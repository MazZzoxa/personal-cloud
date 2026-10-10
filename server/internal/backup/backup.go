// Package backup implements Personal Cloud backups (v0.8.0).
//
// A backup is a single ZIP archive:
//
//	manifest.json            list of every payload file with size and SHA-256
//	database/cloud.db        consistent SQLite snapshot (devices are removed)
//	storage/<path>           the user's files
//	chat-attachments/<name>  chat attachments
//
// A short JSON summary is also stored as the ZIP comment, so listing backups
// only reads the central directory instead of parsing the whole manifest.
package backup

import (
	"archive/zip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"personal-cloud/server/internal/systemdisk"
)

const (
	FormatVersion = 1
	appID         = "personal-cloud"

	manifestName  = "manifest.json"
	dbEntry       = "database/cloud.db"
	storagePrefix = "storage/"
	chatPrefix    = "chat-attachments/"
	namePrefix    = "personal-cloud-backup-"

	KindManual     = "manual"
	KindAuto       = "auto"
	KindPreRestore = "pre-restore"
	KindImported   = "imported"

	// How many safety copies made before a restore are kept.
	preRestoreKeep = 2

	maxManifestBytes = 256 << 20
	minFreeMargin    = 64 << 20
)

var (
	ErrBusy          = errors.New("другая операция резервного копирования уже выполняется")
	ErrNotFound      = errors.New("резервная копия не найдена")
	ErrInvalidName   = errors.New("недопустимое имя резервной копии")
	ErrInvalidBackup = errors.New("файл не является резервной копией Personal Cloud")

	nameRe = regexp.MustCompile(`^personal-cloud-backup-\d{8}-\d{6}-(manual|auto|pre-restore|imported)(-\d+)?\.zip$`)
)

// Item describes one payload file inside the archive.
type Item struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Summary is the compact description stored in the ZIP comment.
type Summary struct {
	Format       int    `json:"format"`
	App          string `json:"app"`
	Version      string `json:"version"`
	CreatedAt    string `json:"createdAt"`
	Kind         string `json:"kind"`
	StorageFiles int    `json:"storageFiles"`
	ChatFiles    int    `json:"chatFiles"`
	FolderCount  int    `json:"folderCount"`
	DataBytes    int64  `json:"dataBytes"`
}

// Manifest is the full description stored as manifest.json.
type Manifest struct {
	Summary
	Database Item     `json:"database"`
	Files    []Item   `json:"files"`
	Folders  []string `json:"folders"`
}

// Info is one row of the backup list shown in the web interface.
type Info struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Size         int64  `json:"size"`
	ModifiedAt   string `json:"modifiedAt"`
	CreatedAt    string `json:"createdAt"`
	Version      string `json:"version"`
	StorageFiles int    `json:"storageFiles"`
	ChatFiles    int    `json:"chatFiles"`
	FolderCount  int    `json:"folderCount"`
	DataBytes    int64  `json:"dataBytes"`
	Valid        bool   `json:"valid"`
	Error        string `json:"error,omitempty"`
}

// Job is the state of the single background operation.
type Job struct {
	Op         string `json:"op"`    // backup | restore | verify
	State      string `json:"state"` // idle | running | done | error
	Phase      string `json:"phase"`
	Name       string `json:"name"`
	Done       int64  `json:"done"`
	Total      int64  `json:"total"`
	Error      string `json:"error,omitempty"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

// Service creates, verifies and restores backups. Only one operation runs at a time.
type Service struct {
	db         *sql.DB
	storageDir string
	chatDir    string
	dataDir    string
	version    string

	mu  sync.Mutex
	job Job
}

func NewService(db *sql.DB, storageDir, chatDir, dataDir, version string) *Service {
	return &Service{
		db:         db,
		storageDir: storageDir,
		chatDir:    chatDir,
		dataDir:    dataDir,
		version:    version,
		job:        Job{State: "idle"},
	}
}

// DefaultDir is used when no custom backup folder is configured.
func (s *Service) DefaultDir() string { return filepath.Join(s.dataDir, "backups") }

// ---------------------------------------------------------------- job state

func (s *Service) Status() Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.job
}

// Restoring is true while a restore is running; the API rejects writes then.
func (s *Service) Restoring() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.job.State == "running" && s.job.Op == "restore"
}

func (s *Service) begin(op, name, phase string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job.State == "running" {
		return ErrBusy
	}
	s.job = Job{Op: op, State: "running", Phase: phase, Name: name, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	return nil
}

func (s *Service) setPhase(phase string, total int64) {
	s.mu.Lock()
	s.job.Phase = phase
	s.job.Done = 0
	s.job.Total = total
	s.mu.Unlock()
}

func (s *Service) addDone(n int64) {
	s.mu.Lock()
	s.job.Done += n
	s.mu.Unlock()
}

func (s *Service) finish(name string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name != "" {
		s.job.Name = name
	}
	s.job.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		s.job.State = "error"
		s.job.Error = err.Error()
		return
	}
	s.job.State = "done"
	s.job.Phase = "Готово"
	if s.job.Total > 0 {
		s.job.Done = s.job.Total
	}
}

type progressWriter struct{ s *Service }

func (w progressWriter) Write(p []byte) (int, error) {
	w.s.addDone(int64(len(p)))
	return len(p), nil
}

// ------------------------------------------------------- background starters

// StartBackup starts creating a backup in the background.
func (s *Service) StartBackup(dir, kind string, keep int, onDone func(name string, err error)) error {
	if err := s.begin("backup", "", "Подготовка"); err != nil {
		return err
	}
	go func() {
		name, err := s.create(dir, kind)
		if err == nil && kind == KindAuto {
			s.prune(dir, KindAuto, keep)
		}
		s.finish(name, err)
		if onDone != nil {
			onDone(name, err)
		}
	}()
	return nil
}

// StartVerify checks every file of a backup against the checksums in its manifest.
func (s *Service) StartVerify(dir, name string, onDone func(err error)) error {
	full, err := Resolve(dir, name)
	if err != nil {
		return err
	}
	if err := s.begin("verify", name, "Проверка"); err != nil {
		return err
	}
	go func() {
		err := s.verify(full)
		s.finish(name, err)
		if onDone != nil {
			onDone(err)
		}
	}()
	return nil
}

// StartRestore replaces the current files and database content with a backup.
func (s *Service) StartRestore(dir, name string, onDone func(err error)) error {
	full, err := Resolve(dir, name)
	if err != nil {
		return err
	}
	if err := s.begin("restore", name, "Подготовка"); err != nil {
		return err
	}
	go func() {
		err := s.restore(dir, full)
		s.finish(name, err)
		if onDone != nil {
			onDone(err)
		}
	}()
	return nil
}

// ------------------------------------------------------------------- create

type sourceFile struct {
	name string // path inside the archive
	abs  string
}

type plan struct {
	files        []sourceFile
	folders      []string
	total        int64
	storageFiles int
	chatFiles    int
}

func skipTemporary(name string) bool {
	return name == ".incoming" || strings.HasPrefix(name, ".upload-")
}

func collect(root, prefix string, into *plan, countStorage bool) error {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		if skipTemporary(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if countStorage {
				into.folders = append(into.folders, rel)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil // symlinks and special files are not backed up
		}
		into.files = append(into.files, sourceFile{name: prefix + rel, abs: current})
		into.total += info.Size()
		if countStorage {
			into.storageFiles++
		} else {
			into.chatFiles++
		}
		return nil
	})
}

func (s *Service) scan() (*plan, error) {
	p := &plan{}
	if err := collect(s.storageDir, storagePrefix, p, true); err != nil {
		return nil, fmt.Errorf("чтение хранилища: %w", err)
	}
	if err := collect(s.chatDir, chatPrefix, p, false); err != nil {
		return nil, fmt.Errorf("чтение вложений чата: %w", err)
	}
	return p, nil
}

var storedExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".heic": true, ".avif": true,
	".mp3": true, ".m4a": true, ".aac": true, ".ogg": true, ".opus": true, ".flac": true,
	".mp4": true, ".mkv": true, ".mov": true, ".avi": true, ".webm": true, ".m4v": true,
	".zip": true, ".7z": true, ".rar": true, ".gz": true, ".xz": true, ".bz2": true, ".zst": true,
	".docx": true, ".xlsx": true, ".pptx": true, ".jar": true, ".apk": true,
}

func methodFor(name string) uint16 {
	if storedExtensions[strings.ToLower(path.Ext(name))] {
		return zip.Store
	}
	return zip.Deflate
}

func (s *Service) addFile(zw *zip.Writer, name, abs string) (Item, error) {
	file, err := os.Open(abs)
	if err != nil {
		return Item{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Item{}, err
	}
	header := &zip.FileHeader{Name: name, Method: methodFor(name), Modified: info.ModTime()}
	writer, err := zw.CreateHeader(header)
	if err != nil {
		return Item{}, err
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(writer, hash, progressWriter{s}), file)
	if err != nil {
		return Item{}, err
	}
	return Item{Path: name, Size: written, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

// snapshotDB writes a consistent copy of the live database and strips the
// device registry from it: backups never carry access tokens.
func (s *Service) snapshotDB(target string) error {
	quoted := strings.ReplaceAll(target, "'", "''")
	if _, err := s.db.Exec("VACUUM INTO '" + quoted + "'"); err != nil {
		return fmt.Errorf("снимок базы данных: %w", err)
	}
	snap, err := sql.Open("sqlite", target)
	if err != nil {
		return fmt.Errorf("открытие снимка базы данных: %w", err)
	}
	defer snap.Close()
	for _, statement := range []string{`DELETE FROM pairing_codes`, `DELETE FROM devices`} {
		if _, err := snap.Exec(statement); err != nil {
			return fmt.Errorf("очистка снимка базы данных: %w", err)
		}
	}
	var mode string
	if err := snap.QueryRow(`PRAGMA journal_mode = DELETE`).Scan(&mode); err != nil {
		return fmt.Errorf("настройка снимка базы данных: %w", err)
	}
	if _, err := snap.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("сжатие снимка базы данных: %w", err)
	}
	return nil
}

func uniqueName(dir, kind string) string {
	base := namePrefix + time.Now().Format("20060102-150405") + "-" + kind
	name := base + ".zip"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			return name
		}
		name = fmt.Sprintf("%s-%d.zip", base, i)
	}
}

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d Б", value)
	}
	div, exp := uint64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cБ", float64(value)/float64(div), []rune("КМГТ")[exp])
}

// create builds a backup archive in dir and returns its file name.
func (s *Service) create(dir, kind string) (name string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("не удалось создать папку резервных копий: %w", err)
	}

	s.setPhase("Подсчёт файлов", 0)
	p, err := s.scan()
	if err != nil {
		return "", err
	}
	if _, free, spaceErr := systemdisk.Space(dir); spaceErr == nil && free < uint64(p.total)+minFreeMargin {
		return "", fmt.Errorf("недостаточно свободного места в папке резервных копий: нужно около %s, свободно %s",
			formatBytes(uint64(p.total)+minFreeMargin), formatBytes(free))
	}

	workDir, err := os.MkdirTemp(dir, ".creating-")
	if err != nil {
		return "", fmt.Errorf("не удалось создать временную папку: %w", err)
	}
	defer os.RemoveAll(workDir)

	s.setPhase("Снимок базы данных", 0)
	snapshot := filepath.Join(workDir, "cloud.db")
	if err := s.snapshotDB(snapshot); err != nil {
		return "", err
	}
	snapInfo, err := os.Stat(snapshot)
	if err != nil {
		return "", err
	}

	tempArchive := filepath.Join(workDir, "backup.zip")
	out, err := os.Create(tempArchive)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(out)
	closed := false
	defer func() {
		if !closed {
			_ = zw.Close()
			_ = out.Close()
		}
	}()

	s.setPhase("Копирование файлов", p.total+snapInfo.Size())
	manifest := Manifest{Folders: p.folders}
	if manifest.Folders == nil {
		manifest.Folders = []string{}
	}
	dbItem, err := s.addFile(zw, dbEntry, snapshot)
	if err != nil {
		return "", fmt.Errorf("запись базы данных: %w", err)
	}
	manifest.Database = dbItem
	manifest.Files = make([]Item, 0, len(p.files))
	dataBytes := dbItem.Size
	for _, source := range p.files {
		item, err := s.addFile(zw, source.name, source.abs)
		if err != nil {
			return "", fmt.Errorf("копирование %q: %w", source.name, err)
		}
		manifest.Files = append(manifest.Files, item)
		dataBytes += item.Size
	}

	manifest.Summary = Summary{
		Format:       FormatVersion,
		App:          appID,
		Version:      s.version,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		Kind:         kind,
		StorageFiles: p.storageFiles,
		ChatFiles:    p.chatFiles,
		FolderCount:  len(p.folders),
		DataBytes:    dataBytes,
	}

	s.setPhase("Завершение", 0)
	manifestWriter, err := zw.CreateHeader(&zip.FileHeader{Name: manifestName, Method: zip.Deflate, Modified: time.Now()})
	if err != nil {
		return "", err
	}
	if err := json.NewEncoder(manifestWriter).Encode(manifest); err != nil {
		return "", err
	}
	comment, err := json.Marshal(manifest.Summary)
	if err != nil {
		return "", err
	}
	if err := zw.SetComment(string(comment)); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	if err := out.Sync(); err != nil {
		return "", err
	}
	closed = true
	if err := out.Close(); err != nil {
		return "", err
	}

	name = uniqueName(dir, kind)
	if err := moveFile(tempArchive, filepath.Join(dir, name)); err != nil {
		return "", fmt.Errorf("не удалось сохранить резервную копию: %w", err)
	}
	if kind == KindPreRestore {
		s.prune(dir, KindPreRestore, preRestoreKeep)
	}
	return name, nil
}

// moveFile renames, falling back to copy when source and target are on different volumes.
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(to)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(to)
		return err
	}
	return os.Remove(from)
}

// ---------------------------------------------------- list / names / delete

// KindFromName extracts the kind from a backup file name ("" if invalid).
func KindFromName(name string) string {
	match := nameRe.FindStringSubmatch(name)
	if match == nil {
		return ""
	}
	return match[1]
}

// Resolve validates a backup name coming from the API and returns its full path.
func Resolve(dir, name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", ErrInvalidName
	}
	full := filepath.Join(dir, name)
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return full, nil
}

func readSummary(zr *zip.Reader) (Summary, error) {
	var summary Summary
	if err := json.Unmarshal([]byte(zr.Comment), &summary); err != nil {
		return summary, ErrInvalidBackup
	}
	if summary.App != appID {
		return summary, ErrInvalidBackup
	}
	if summary.Format != FormatVersion {
		return summary, fmt.Errorf("неподдерживаемый формат резервной копии (%d)", summary.Format)
	}
	return summary, nil
}

// List returns the backups in dir, newest first. A missing folder is not an error.
func List(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Info{}, nil
		}
		return nil, err
	}
	items := make([]Info, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !nameRe.MatchString(entry.Name()) {
			continue
		}
		fileInfo, err := entry.Info()
		if err != nil {
			continue
		}
		info := Info{
			Name:       entry.Name(),
			Kind:       KindFromName(entry.Name()),
			Size:       fileInfo.Size(),
			ModifiedAt: fileInfo.ModTime().UTC().Format(time.RFC3339),
		}
		zr, err := zip.OpenReader(filepath.Join(dir, entry.Name()))
		if err != nil {
			info.Error = "архив повреждён или не завершён"
			items = append(items, info)
			continue
		}
		summary, sumErr := readSummary(&zr.Reader)
		_ = zr.Close()
		if sumErr != nil {
			info.Error = sumErr.Error()
			items = append(items, info)
			continue
		}
		info.Valid = true
		info.CreatedAt = summary.CreatedAt
		info.Version = summary.Version
		info.StorageFiles = summary.StorageFiles
		info.ChatFiles = summary.ChatFiles
		info.FolderCount = summary.FolderCount
		info.DataBytes = summary.DataBytes
		items = append(items, info)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name > items[j].Name })
	return items, nil
}

// NewestModTime returns the modification time of the newest regular
// (non pre-restore) backup file, used by the scheduler.
func NewestModTime(dir string) (time.Time, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}, false
	}
	var newest time.Time
	found := false
	for _, entry := range entries {
		kind := KindFromName(entry.Name())
		if entry.IsDir() || kind == "" || kind == KindPreRestore {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if !found || info.ModTime().After(newest) {
			newest = info.ModTime()
			found = true
		}
	}
	return newest, found
}

// Delete removes one backup file.
func (s *Service) Delete(dir, name string) error {
	full, err := Resolve(dir, name)
	if err != nil {
		return err
	}
	s.mu.Lock()
	busy := s.job.State == "running" && s.job.Name == name
	s.mu.Unlock()
	if busy {
		return ErrBusy
	}
	return os.Remove(full)
}

// prune removes the oldest backups of one kind beyond keep (best effort).
func (s *Service) prune(dir, kind string, keep int) {
	if keep < 1 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && KindFromName(entry.Name()) == kind {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for i := keep; i < len(names); i++ {
		_ = os.Remove(filepath.Join(dir, names[i]))
	}
}

// Import stores an uploaded backup archive in dir after a quick structural check.
func (s *Service) Import(dir string, src io.Reader) (Info, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Info{}, fmt.Errorf("не удалось создать папку резервных копий: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".import-*.tmp")
	if err != nil {
		return Info{}, err
	}
	tempName := temp.Name()
	cleanup := func() { _ = os.Remove(tempName) }
	if _, err := io.Copy(temp, src); err != nil {
		_ = temp.Close()
		cleanup()
		return Info{}, err
	}
	if err := temp.Close(); err != nil {
		cleanup()
		return Info{}, err
	}

	zr, err := zip.OpenReader(tempName)
	if err != nil {
		cleanup()
		return Info{}, ErrInvalidBackup
	}
	_, sumErr := readSummary(&zr.Reader)
	hasManifest := false
	for _, file := range zr.File {
		if file.Name == manifestName {
			hasManifest = true
			break
		}
	}
	_ = zr.Close()
	if sumErr != nil {
		cleanup()
		return Info{}, sumErr
	}
	if !hasManifest {
		cleanup()
		return Info{}, ErrInvalidBackup
	}

	name := uniqueName(dir, KindImported)
	if err := os.Rename(tempName, filepath.Join(dir, name)); err != nil {
		cleanup()
		return Info{}, err
	}
	items, err := List(dir)
	if err != nil {
		return Info{}, err
	}
	for _, item := range items {
		if item.Name == name {
			return item, nil
		}
	}
	return Info{Name: name, Kind: KindImported}, nil
}

// ------------------------------------------------------- manifest / verify

func openBackup(full string) (*zip.ReadCloser, Manifest, error) {
	var manifest Manifest
	zr, err := zip.OpenReader(full)
	if err != nil {
		return nil, manifest, fmt.Errorf("архив повреждён или не завершён: %w", err)
	}
	if _, err := readSummary(&zr.Reader); err != nil {
		_ = zr.Close()
		return nil, manifest, err
	}
	var entry *zip.File
	for _, file := range zr.File {
		if file.Name == manifestName {
			entry = file
			break
		}
	}
	if entry == nil {
		_ = zr.Close()
		return nil, manifest, ErrInvalidBackup
	}
	reader, err := entry.Open()
	if err != nil {
		_ = zr.Close()
		return nil, manifest, err
	}
	err = json.NewDecoder(io.LimitReader(reader, maxManifestBytes)).Decode(&manifest)
	_ = reader.Close()
	if err != nil {
		_ = zr.Close()
		return nil, manifest, fmt.Errorf("не удалось прочитать список файлов копии: %w", err)
	}
	if manifest.App != appID || manifest.Format != FormatVersion {
		_ = zr.Close()
		return nil, manifest, ErrInvalidBackup
	}
	return zr, manifest, nil
}

// safeRel validates a slash-separated relative path taken from an archive.
func safeRel(rel string) (string, error) {
	bad := fmt.Errorf("небезопасный путь в архиве: %q", rel)
	if rel == "" || strings.ContainsRune(rel, 0) || strings.HasPrefix(rel, "/") {
		return "", bad
	}
	if runtime.GOOS == "windows" && strings.Contains(rel, "\\") {
		return "", bad
	}
	if path.Clean(rel) != rel {
		return "", bad
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return "", bad
		}
		if runtime.GOOS == "windows" && strings.Contains(part, ":") {
			return "", bad
		}
	}
	return rel, nil
}

// splitItemPath maps an archive path to its payload root and relative path.
func splitItemPath(name string) (prefix, rel string, err error) {
	switch {
	case strings.HasPrefix(name, storagePrefix):
		prefix = storagePrefix
	case strings.HasPrefix(name, chatPrefix):
		prefix = chatPrefix
	default:
		return "", "", fmt.Errorf("неизвестная запись в архиве: %q", name)
	}
	rel, err = safeRel(strings.TrimPrefix(name, prefix))
	return prefix, rel, err
}

func indexEntries(zr *zip.ReadCloser) map[string]*zip.File {
	index := make(map[string]*zip.File, len(zr.File))
	for _, file := range zr.File {
		index[file.Name] = file
	}
	return index
}

// copyChecked streams one archive entry to dst (may be nil) while verifying size and SHA-256.
func (s *Service) copyChecked(file *zip.File, item Item, dst io.Writer) error {
	reader, err := file.Open()
	if err != nil {
		return fmt.Errorf("%s: %w", item.Path, err)
	}
	defer reader.Close()
	hash := sha256.New()
	writers := []io.Writer{hash, progressWriter{s}}
	if dst != nil {
		writers = append(writers, dst)
	}
	written, err := io.Copy(io.MultiWriter(writers...), reader)
	if err != nil {
		return fmt.Errorf("%s: %w", item.Path, err)
	}
	if written != item.Size || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		return fmt.Errorf("файл повреждён: %s", item.Path)
	}
	return nil
}

func (s *Service) verify(full string) error {
	s.setPhase("Чтение архива", 0)
	zr, manifest, err := openBackup(full)
	if err != nil {
		return err
	}
	defer zr.Close()
	index := indexEntries(zr)

	total := manifest.Database.Size
	for _, item := range manifest.Files {
		total += item.Size
	}
	s.setPhase("Проверка контрольных сумм", total)

	if manifest.Database.Path != dbEntry {
		return ErrInvalidBackup
	}
	entry, ok := index[dbEntry]
	if !ok {
		return fmt.Errorf("в архиве нет базы данных")
	}
	if err := s.copyChecked(entry, manifest.Database, nil); err != nil {
		return err
	}
	for _, item := range manifest.Files {
		if _, _, err := splitItemPath(item.Path); err != nil {
			return err
		}
		entry, ok := index[item.Path]
		if !ok {
			return fmt.Errorf("в архиве нет файла: %s", item.Path)
		}
		if err := s.copyChecked(entry, item, nil); err != nil {
			return err
		}
	}
	return nil
}
