package storage

import (
	"database/sql"
	"encoding/json"
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

type Entry struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mimeType"`
	UpdatedAt string `json:"updatedAt"`
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
		kind := "file"
		if item.IsDir() {
			kind = "folder"
		}
		mimeType := ""
		if !item.IsDir() {
			mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(item.Name())))
			if mimeType == "" {
				mimeType = detectMime(filepath.Join(abs, item.Name()))
			}
		}
		id, _ := s.lookupID(itemRel)
		entries = append(entries, Entry{
			ID:        id,
			Name:      item.Name(),
			Path:      itemRel,
			Kind:      kind,
			Size:      itemInfo.Size(),
			MimeType:  mimeType,
			UpdatedAt: itemInfo.ModTime().Format(time.RFC3339),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind == "folder"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

func (s *Store) Save(rel string, src io.Reader, filename, contentType string) (Entry, error) {
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

	size, copyErr := io.Copy(temp, src)
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

	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.upsertFile(clean, filename, "file", size, contentType, now); err != nil {
		return Entry{}, err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return Entry{}, err
	}
	id, _ := s.lookupID(clean)
	return Entry{ID: id, Name: filename, Path: clean, Kind: "file", Size: info.Size(), MimeType: contentType, UpdatedAt: info.ModTime().Format(time.RFC3339)}, nil
}

func (s *Store) CreateFolder(rel string) (Entry, error) {
	abs, clean, err := s.safePath(rel)
	if err != nil {
		return Entry{}, err
	}
	if clean == "" {
		return Entry{}, fmt.Errorf("folder path is required")
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return Entry{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	name := filepath.Base(clean)
	if err := s.upsertFile(clean, name, "folder", 0, "", now); err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Entry{}, err
	}
	id, _ := s.lookupID(clean)
	return Entry{ID: id, Name: name, Path: clean, Kind: "folder", UpdatedAt: info.ModTime().Format(time.RFC3339)}, nil
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
		file.Close()
		return nil, nil, "", err
	}
	if info.IsDir() {
		file.Close()
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

func (s *Store) upsertFile(path, name, kind string, size int64, mimeType, now string) error {
	_, err := s.db.Exec(`
INSERT INTO files(path, name, kind, size, mime_type, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(path) DO UPDATE SET
    name=excluded.name,
    kind=excluded.kind,
    size=excluded.size,
    mime_type=excluded.mime_type,
    updated_at=excluded.updated_at
`, path, name, kind, size, mimeType, now, now)
	return err
}

func (s *Store) lookupID(path string) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM files WHERE path = ?`, path).Scan(&id)
	return id, err
}

func detectMime(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer file.Close()
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	return http.DetectContentType(buf[:n])
}

func (e Entry) JSON() []byte {
	value, _ := json.Marshal(e)
	return value
}
