package storage

import (
	"archive/zip"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Store struct {
	root string
	db   *sql.DB
}

var ErrFileTooLarge = errors.New("file exceeds the configured upload limit")

// Root returns the configured storage root. It is intended for local management
// metrics and is not exposed directly through the HTTP API.
func (s *Store) Root() string { return s.root }

type Entry struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mimeType"`
	Extension string `json:"extension"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type SearchOptions struct {
	Query string
	Path  string
}

type ArchiveEntry struct {
	Path string
	Name string
	Info os.FileInfo
}

func New(root string, db *sql.DB) *Store {
	return &Store{root: root, db: db}
}

func (s *Store) List(rel string) ([]Entry, error) {
	abs, clean, err := s.safePath(rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory")
	}

	items, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		itemRel := filepath.ToSlash(filepath.Join(clean, item.Name()))
		itemInfo, err := item.Info()
		if err != nil {
			return nil, err
		}
		entry, err := s.entryFromInfo(itemRel, itemInfo)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind == "folder"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

func (s *Store) Search(options SearchOptions) ([]Entry, error) {
	query := strings.TrimSpace(strings.ToLower(options.Query))
	if query == "" {
		return []Entry{}, nil
	}

	root := s.root
	prefix := ""
	if strings.TrimSpace(options.Path) != "" {
		abs, clean, err := s.safePath(options.Path)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("search path is not a directory")
		}
		root = abs
		prefix = clean
	}

	entries := make([]Entry, 0)
	err := filepath.WalkDir(root, func(path string, dirEntry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if strings.HasPrefix(dirEntry.Name(), ".upload-") {
			if dirEntry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relToStore, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		relToStore = filepath.ToSlash(relToStore)
		if prefix != "" && !strings.HasPrefix(relToStore, prefix+"/") && relToStore != prefix {
			return nil
		}

		info, err := dirEntry.Info()
		if err != nil {
			return err
		}
		entry, err := s.entryFromInfo(relToStore, info)
		if err != nil {
			return err
		}

		searchText := strings.ToLower(strings.Join([]string{
			entry.Name,
			entry.Path,
			entry.MimeType,
			entry.Extension,
		}, " "))
		if strings.Contains(searchText, query) {
			entries = append(entries, entry)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind == "folder"
		}
		return strings.ToLower(entries[i].Path) < strings.ToLower(entries[j].Path)
	})
	return entries, nil
}

// ArchiveEntries validates selected paths and expands folders into files/directories
// that can be streamed into a ZIP archive without exposing paths outside storage.
func (s *Store) ArchiveEntries(paths []string) ([]ArchiveEntry, error) {
	seen := make(map[string]struct{})
	result := make([]ArchiveEntry, 0)

	for _, rel := range paths {
		abs, clean, err := s.safePath(rel)
		if err != nil {
			return nil, err
		}
		if clean == "" {
			return nil, fmt.Errorf("cannot download storage root")
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}

		add := func(path, name string, fileInfo os.FileInfo) {
			if _, exists := seen[name]; exists {
				return
			}
			seen[name] = struct{}{}
			result = append(result, ArchiveEntry{Path: path, Name: name, Info: fileInfo})
		}

		if !info.IsDir() {
			add(abs, filepath.ToSlash(clean), info)
			continue
		}

		err = filepath.WalkDir(abs, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path != abs && strings.HasPrefix(entry.Name(), ".upload-") {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			fileInfo, err := entry.Info()
			if err != nil {
				return err
			}
			relPath, err := filepath.Rel(s.root, path)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(relPath)
			if fileInfo.IsDir() {
				name += "/"
			}
			add(path, name, fileInfo)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

// WriteArchive streams validated archive entries into the provided ZIP writer.
func (s *Store) WriteArchive(writer *zip.Writer, entries []ArchiveEntry) error {
	for _, entry := range entries {
		header, err := zip.FileInfoHeader(entry.Info)
		if err != nil {
			return err
		}
		header.Name = entry.Name
		if !entry.Info.IsDir() {
			header.Method = zip.Deflate
		}

		part, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if entry.Info.IsDir() {
			continue
		}

		file, err := os.Open(entry.Path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(part, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func (s *Store) Save(rel string, src io.Reader, filename, contentType string) (Entry, error) {
	return s.SaveLimit(rel, src, filename, contentType, 0)
}

// SaveLimit writes to a temporary file and atomically replaces the destination
// only after the stream is fully read and the optional size limit is satisfied.
// A zero limit means unlimited (legacy behavior).
func (s *Store) SaveLimit(rel string, src io.Reader, filename, contentType string, maxBytes int64) (Entry, error) {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == "" || filename == ".." {
		return Entry{}, fmt.Errorf("invalid filename")
	}
	rel = filepath.ToSlash(filepath.Join(rel, filename))
	abs, clean, err := s.safePath(rel)
	if err != nil {
		return Entry{}, err
	}

	parent := filepath.Dir(abs)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Entry{}, err
	}

	temp, err := os.CreateTemp(parent, ".upload-*")
	if err != nil {
		return Entry{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	var size int64
	var copyErr error
	if maxBytes > 0 {
		size, copyErr = io.Copy(temp, io.LimitReader(src, maxBytes+1))
		if copyErr == nil && size > maxBytes {
			copyErr = ErrFileTooLarge
		}
	} else {
		size, copyErr = io.Copy(temp, src)
	}
	closeErr := temp.Close()
	if copyErr != nil {
		return Entry{}, copyErr
	}
	if closeErr != nil {
		return Entry{}, closeErr
	}

	if err := os.Rename(tempPath, abs); err != nil {
		return Entry{}, err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return Entry{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.upsertFile(clean, filename, "file", size, normalizeMime(contentType, filename, abs), now, now); err != nil {
		return Entry{}, err
	}
	return s.entryFromInfo(clean, info)
}

func (s *Store) CreateFolder(rel string) (Entry, error) {
	abs, clean, err := s.safePath(rel)
	if err != nil {
		return Entry{}, err
	}
	if clean == "" {
		return Entry{}, fmt.Errorf("folder path is required")
	}
	if _, err := os.Stat(abs); err == nil {
		return Entry{}, fmt.Errorf("destination already exists")
	} else if !os.IsNotExist(err) {
		return Entry{}, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Entry{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.upsertFile(clean, filepath.Base(clean), "folder", 0, "", now, info.ModTime().UTC().Format(time.RFC3339)); err != nil {
		return Entry{}, err
	}
	return s.entryFromInfo(clean, info)
}

func (s *Store) Rename(rel, newName string) (Entry, error) {
	abs, clean, err := s.safePath(rel)
	if err != nil {
		return Entry{}, err
	}
	if clean == "" {
		return Entry{}, fmt.Errorf("cannot rename storage root")
	}
	newName = strings.TrimSpace(newName)
	if newName == "" || newName == "." || newName == ".." || filepath.Base(filepath.FromSlash(newName)) != newName || strings.ContainsAny(newName, `/\\`) {
		return Entry{}, fmt.Errorf("invalid new name")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Entry{}, err
	}

	parent := filepath.Dir(clean)
	targetRel := filepath.ToSlash(filepath.Join(parent, newName))
	targetAbs, targetClean, err := s.safePath(targetRel)
	if err != nil {
		return Entry{}, err
	}
	if strings.EqualFold(clean, targetClean) || targetClean == clean {
		return s.entryFromInfo(clean, info)
	}
	if _, err := os.Stat(targetAbs); err == nil {
		return Entry{}, fmt.Errorf("destination already exists")
	} else if !os.IsNotExist(err) {
		return Entry{}, err
	}
	if err := os.Rename(abs, targetAbs); err != nil {
		return Entry{}, err
	}
	if err := s.rewritePathMetadata(clean, targetClean); err != nil {
		return Entry{}, err
	}
	targetInfo, err := os.Stat(targetAbs)
	if err != nil {
		return Entry{}, err
	}
	return s.entryFromInfo(targetClean, targetInfo)
}

func (s *Store) Move(rel, destination string) (Entry, error) {
	sourceAbs, sourceClean, err := s.safePath(rel)
	if err != nil {
		return Entry{}, err
	}
	if sourceClean == "" {
		return Entry{}, fmt.Errorf("cannot move storage root")
	}
	destinationAbs, destinationClean, err := s.safePath(destination)
	if err != nil {
		return Entry{}, err
	}
	destinationInfo, err := os.Stat(destinationAbs)
	if err != nil {
		return Entry{}, err
	}
	if !destinationInfo.IsDir() {
		return Entry{}, fmt.Errorf("destination is not a folder")
	}
	sourceInfo, err := os.Stat(sourceAbs)
	if err != nil {
		return Entry{}, err
	}

	name := filepath.Base(sourceClean)
	targetRel := filepath.ToSlash(filepath.Join(destinationClean, name))
	targetAbs, targetClean, err := s.safePath(targetRel)
	if err != nil {
		return Entry{}, err
	}
	if targetClean == sourceClean {
		return s.entryFromInfo(sourceClean, sourceInfo)
	}
	if sourceInfo.IsDir() {
		if strings.HasPrefix(targetClean, sourceClean+"/") {
			return Entry{}, fmt.Errorf("cannot move a folder into itself")
		}
	}
	if _, err := os.Stat(targetAbs); err == nil {
		return Entry{}, fmt.Errorf("destination already contains an item with this name")
	} else if !os.IsNotExist(err) {
		return Entry{}, err
	}

	if err := os.Rename(sourceAbs, targetAbs); err != nil {
		return Entry{}, err
	}
	if err := s.rewritePathMetadata(sourceClean, targetClean); err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(targetAbs)
	if err != nil {
		return Entry{}, err
	}
	return s.entryFromInfo(targetClean, info)
}

func (s *Store) Copy(rel, destination string) (Entry, error) {
	sourceAbs, sourceClean, err := s.safePath(rel)
	if err != nil {
		return Entry{}, err
	}
	if sourceClean == "" {
		return Entry{}, fmt.Errorf("cannot copy storage root")
	}
	destinationAbs, destinationClean, err := s.safePath(destination)
	if err != nil {
		return Entry{}, err
	}
	destinationInfo, err := os.Stat(destinationAbs)
	if err != nil {
		return Entry{}, err
	}
	if !destinationInfo.IsDir() {
		return Entry{}, fmt.Errorf("destination is not a folder")
	}

	name := filepath.Base(sourceClean)
	targetRel := filepath.ToSlash(filepath.Join(destinationClean, name))
	targetAbs, targetClean, err := s.safePath(targetRel)
	if err != nil {
		return Entry{}, err
	}
	if sourceInfo, statErr := os.Stat(sourceAbs); statErr == nil && sourceInfo.IsDir() {
		if targetClean == sourceClean || strings.HasPrefix(targetClean, sourceClean+"/") {
			return Entry{}, fmt.Errorf("cannot copy a folder into itself")
		}
	}
	if _, err := os.Stat(targetAbs); err == nil {
		return Entry{}, fmt.Errorf("destination already contains an item with this name")
	} else if !os.IsNotExist(err) {
		return Entry{}, err
	}

	if err := copyPath(sourceAbs, targetAbs); err != nil {
		_ = os.RemoveAll(targetAbs)
		return Entry{}, err
	}
	if err := s.indexTree(targetAbs); err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(targetAbs)
	if err != nil {
		return Entry{}, err
	}
	return s.entryFromInfo(targetClean, info)
}

func (s *Store) Delete(rel string) error {
	abs, clean, err := s.safePath(rel)
	if err != nil {
		return err
	}
	if clean == "" {
		return fmt.Errorf("cannot delete storage root")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.RemoveAll(abs); err != nil {
			return err
		}
		_, err = s.db.Exec(`DELETE FROM files WHERE path = ? OR path LIKE ?`, clean, clean+"/%")
		return err
	}
	if err := os.Remove(abs); err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM files WHERE path = ?`, clean)
	return err
}

func (s *Store) Open(rel string) (*os.File, os.FileInfo, string, error) {
	abs, clean, err := s.safePath(rel)
	if err != nil {
		return nil, nil, "", err
	}
	file, err := os.Open(abs)
	if err != nil {
		return nil, nil, "", err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, "", err
	}
	if info.IsDir() {
		_ = file.Close()
		return nil, nil, "", fmt.Errorf("cannot download a folder")
	}
	return file, info, clean, nil
}

func (s *Store) safePath(rel string) (string, string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	rel = strings.TrimPrefix(rel, "/")
	clean := filepath.ToSlash(filepath.Clean(rel))
	if clean == "." {
		clean = ""
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", "", errors.New("invalid path")
	}
	abs := filepath.Join(s.root, filepath.FromSlash(clean))
	rootAbs, err := filepath.Abs(s.root)
	if err != nil {
		return "", "", err
	}
	absReal, err := filepath.Abs(abs)
	if err != nil {
		return "", "", err
	}
	relCheck, err := filepath.Rel(rootAbs, absReal)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(os.PathSeparator)) {
		return "", "", errors.New("invalid path")
	}
	return absReal, clean, nil
}

func (s *Store) entryFromInfo(path string, info os.FileInfo) (Entry, error) {
	kind := "file"
	mimeType := ""
	extension := ""
	if info.IsDir() {
		kind = "folder"
	} else {
		extension = strings.TrimPrefix(strings.ToLower(filepath.Ext(info.Name())), ".")
		mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(info.Name())))
		if mimeType == "" {
			mimeType = detectMime(filepath.Join(s.root, filepath.FromSlash(path)))
		}
	}

	createdAt, _ := s.lookupCreatedAt(path)
	if createdAt == "" {
		createdAt = info.ModTime().UTC().Format(time.RFC3339)
	}
	updatedAt := info.ModTime().UTC().Format(time.RFC3339)
	if err := s.upsertFile(path, info.Name(), kind, info.Size(), mimeType, createdAt, updatedAt); err != nil {
		return Entry{}, err
	}
	id, _ := s.lookupID(path)
	return Entry{
		ID:        id,
		Name:      info.Name(),
		Path:      path,
		Kind:      kind,
		Size:      info.Size(),
		MimeType:  mimeType,
		Extension: extension,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func (s *Store) upsertFile(path, name, kind string, size int64, mimeType, createdAt, updatedAt string) error {
	_, err := s.db.Exec(`
INSERT INTO files(path, name, kind, size, mime_type, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(path) DO UPDATE SET
    name=excluded.name,
    kind=excluded.kind,
    size=excluded.size,
    mime_type=excluded.mime_type,
    updated_at=excluded.updated_at
`, path, name, kind, size, mimeType, createdAt, updatedAt)
	return err
}

func (s *Store) lookupID(path string) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM files WHERE path = ?`, path).Scan(&id)
	return id, err
}

func (s *Store) lookupCreatedAt(path string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT created_at FROM files WHERE path = ?`, path).Scan(&value)
	return value, err
}

func (s *Store) rewritePathMetadata(oldPath, newPath string) error {
	rows, err := s.db.Query(`SELECT name, kind, size, mime_type, created_at, updated_at, path FROM files WHERE path = ? OR path LIKE ? ORDER BY LENGTH(path)`, oldPath, oldPath+"/%")
	if err != nil {
		return err
	}
	defer rows.Close()

	type record struct {
		name, kind, mimeType, createdAt, updatedAt, path string
		size                                             int64
	}
	records := make([]record, 0)
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.name, &r.kind, &r.size, &r.mimeType, &r.createdAt, &r.updatedAt, &r.path); err != nil {
			return err
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM files WHERE path = ? OR path LIKE ?`, oldPath, oldPath+"/%"); err != nil {
		return err
	}
	for _, r := range records {
		newPathForRecord := newPath
		if r.path != oldPath {
			newPathForRecord += strings.TrimPrefix(r.path, oldPath)
		}
		if _, err := tx.Exec(`INSERT INTO files(path, name, kind, size, mime_type, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)`, newPathForRecord, r.name, r.kind, r.size, r.mimeType, r.createdAt, r.updatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) indexTree(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		_, err = s.entryFromInfo(filepath.ToSlash(rel), info)
		return err
	})
}

func copyPath(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
		return os.Chtimes(target, info.ModTime(), info.ModTime())
	}

	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(target, info.ModTime(), info.ModTime())
}

func normalizeMime(contentType, filename, path string) string {
	if strings.TrimSpace(contentType) != "" {
		if mediaType, _, err := mime.ParseMediaType(contentType); err == nil && mediaType != "" {
			return mediaType
		}
	}
	if value := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); value != "" {
		return value
	}
	return detectMime(path)
}

func detectMime(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer file.Close()
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	if n == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(buf[:n])
}
