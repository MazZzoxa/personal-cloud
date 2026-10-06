package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"personal-cloud/server/internal/storage"
)

type Server struct {
	db      *sql.DB
	store   *storage.Store
	webDir  string
	version string
}

func New(db *sql.DB, store *storage.Store, webDir, version string) http.Handler {
	s := &Server{db: db, store: store, webDir: webDir, version: version}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("GET /api/files", s.listFiles)
	mux.HandleFunc("POST /api/files", s.uploadFile)
	mux.HandleFunc("GET /api/files/download", s.downloadFile)
	mux.HandleFunc("DELETE /api/files", s.deleteFile)
	mux.HandleFunc("POST /api/folders", s.createFolder)

	mux.Handle("/", s.withWebApp())
	return s.withHeaders(s.logRequests(mux))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": s.version})
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM files WHERE kind = 'file'`).Scan(&count)
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    "Personal Cloud",
		"version": s.version,
		"files":   count,
	})
}

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	entries, err := s.store.List(path)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "items": entries})
}

func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("invalid multipart form: %w", err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("file is required"))
		return
	}
	defer file.Close()

	path := r.URL.Query().Get("path")
	entry, err := s.store.Save(path, file, header.Filename, header.Header.Get("Content-Type"))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	file, info, clean, err := s.store.Open(path)
	if err != nil {
		errorJSON(w, http.StatusNotFound, err)
		return
	}
	defer file.Close()

	contentType := "application/octet-stream"
	if value := r.Header.Get("Accept"); strings.Contains(value, "text/html") {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(clean)))
	http.ServeContent(w, r, filepath.Base(clean), info.ModTime(), file)
}

func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if err := s.store.Delete(path); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Path string `json:"path"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("invalid JSON"))
		return
	}
	entry, err := s.store.CreateFolder(payload.Path)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func (s *Server) withWebApp() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			s.serveIndex(w, r)
			return
		}
		relative := strings.TrimPrefix(filepath.ToSlash(r.URL.Path), "/")
		target := filepath.Join(s.webDir, filepath.FromSlash(relative))
		if s.webDir != "" && fileExists(target) {
			http.ServeFile(w, r, target)
			return
		}
		if s.webDir != "" && fileExists(filepath.Join(s.webDir, "index.html")) {
			s.serveIndex(w, r)
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "Frontend is not built.",
			"hint":  "Run npm install and npm run build inside the web directory.",
		})
	})
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	index := filepath.Join(s.webDir, "index.html")
	if !fileExists(index) {
		writeJSON(w, http.StatusOK, map[string]any{
			"name":    "Personal Cloud",
			"version": s.version,
			"message": "Backend is running. Build the web frontend to use the UI.",
		})
		return
	}
	http.ServeFile(w, r, index)
}

func (s *Server) withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func errorJSON(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
