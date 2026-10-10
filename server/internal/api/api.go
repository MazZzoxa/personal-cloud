package api

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"personal-cloud/server/internal/auth"
	"personal-cloud/server/internal/chat"
	"personal-cloud/server/internal/network"
	"personal-cloud/server/internal/storage"
)

type Server struct {
	db             *sql.DB
	store          *storage.Store
	auth           *auth.Service
	chat           *chat.Service
	chatHub        *chat.Hub
	webDir         string
	version        string
	trustLocalhost bool
	chatRoot       string
	dataDir        string
}

func New(db *sql.DB, store *storage.Store, authService *auth.Service, webDir, version, chatRoot string, trustLocalhost bool) http.Handler {
	s := &Server{
		db:             db,
		store:          store,
		auth:           authService,
		chat:           chat.NewService(db, chatRoot),
		chatHub:        chat.NewHub(),
		webDir:         webDir,
		version:        version,
		trustLocalhost: trustLocalhost,
		chatRoot:       chatRoot,
		dataDir:        filepath.Dir(chatRoot),
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/auth/me", s.authMe)
	mux.HandleFunc("GET /api/network", s.networkInfo)
	mux.HandleFunc("POST /api/auth/pair", s.authPair)
	mux.HandleFunc("POST /api/auth/logout", s.authLogout)
	mux.HandleFunc("GET /api/devices", s.listDevices)
	mux.HandleFunc("POST /api/devices/pairing", s.createPairingCode)
	mux.HandleFunc("GET /api/storage", s.storageInfo)
	mux.HandleFunc("GET /api/settings", s.getSettingsAPI)
	mux.HandleFunc("PATCH /api/settings", s.updateSettingsAPI)
	mux.HandleFunc("GET /api/logs", s.listLogs)
	mux.HandleFunc("DELETE /api/logs", s.clearLogs)
	mux.HandleFunc("PATCH /api/devices/{id}", s.renameDevice)
	mux.HandleFunc("DELETE /api/devices/{id}", s.revokeDevice)
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("GET /api/files", s.listFiles)
	mux.HandleFunc("POST /api/files", s.uploadFile)
	mux.HandleFunc("GET /api/files/download", s.downloadFile)
	mux.HandleFunc("GET /api/files/preview", s.previewFile)
	mux.HandleFunc("GET /api/files/preview/text", s.previewText)
	mux.HandleFunc("POST /api/files/download-bulk", s.downloadFilesArchive)
	mux.HandleFunc("DELETE /api/files", s.deleteFile)
	mux.HandleFunc("POST /api/folders", s.createFolder)
	mux.HandleFunc("PATCH /api/files/rename", s.renameFile)
	mux.HandleFunc("POST /api/files/move", s.moveFile)
	mux.HandleFunc("POST /api/files/copy", s.copyFile)
	mux.HandleFunc("GET /api/search", s.search)
	mux.HandleFunc("GET /api/chat/messages", s.listChatMessages)
	mux.HandleFunc("POST /api/chat/messages", s.createChatMessage)
	mux.HandleFunc("PATCH /api/chat/messages/{id}", s.updateChatMessage)
	mux.HandleFunc("DELETE /api/chat/messages/{id}", s.deleteChatMessage)
	mux.HandleFunc("POST /api/chat/delete", s.deleteChatMessages)
	mux.HandleFunc("POST /api/chat/download", s.downloadSelectedChat)
	mux.HandleFunc("GET /api/chat/ws", s.chatWebSocket)
	mux.HandleFunc("GET /api/chat/attachments/{id}", s.downloadChatAttachment)

	mux.Handle("/", s.withWebApp())
	return s.withHeaders(s.withOriginCheck(s.withAuth(s.logRequests(mux))))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": s.version})
}

func (s *Server) networkInfo(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	routes := network.Routes(requestPort(r), requestScheme(r))
	current := "unknown"
	host := requestHost(r)
	if host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
		current = "host"
	} else if strings.HasSuffix(strings.ToLower(host), ".ts.net") {
		current = "tailscale"
	} else {
		for _, route := range routes {
			if route.Address == host {
				current = route.Kind
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"current": current,
		"routes":  routes,
	})
}

func requestScheme(r *http.Request) string {
	if isSecureRequest(r) {
		return "https"
	}
	return "http"
}

func requestHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.Host)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(r.Host, "[]")
}

func requestPort(r *http.Request) int {
	if _, port, err := net.SplitHostPort(r.Host); err == nil {
		if value, err := strconv.Atoi(port); err == nil && value > 0 && value < 65536 {
			return value
		}
	}
	if isSecureRequest(r) {
		return 443
	}
	return 8080
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	settings, err := s.getSettings()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM files WHERE kind = 'file'`).Scan(&count)
	var messages int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM chat_messages`).Scan(&messages)
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     settings.CloudName,
		"version":  s.version,
		"files":    count,
		"messages": messages,
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

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("q"))
	category := strings.ToLower(strings.TrimSpace(q.Get("type")))
	if category == "all" {
		category = ""
	}
	if !storage.ValidCategory(category) {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("неизвестный тип файлов"))
		return
	}

	modifiedDays := 0
	switch q.Get("modified") {
	case "", "any":
	case "day":
		modifiedDays = 1
	case "week":
		modifiedDays = 7
	case "month":
		modifiedDays = 30
	case "year":
		modifiedDays = 365
	default:
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("неизвестный период изменения"))
		return
	}

	sortKey := q.Get("sort")
	switch sortKey {
	case "", "relevance", "name", "size", "modified", "type":
	default:
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("неизвестный порядок сортировки"))
		return
	}
	order := q.Get("order")
	if order != "" && order != "asc" && order != "desc" {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("неизвестное направление сортировки"))
		return
	}

	limit := 0
	if value := q.Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			errorJSON(w, http.StatusBadRequest, fmt.Errorf("некорректный лимит результатов"))
			return
		}
		limit = parsed
	}
	content := q.Get("content") == "1" || q.Get("content") == "true"

	result, err := s.store.Search(storage.SearchOptions{
		Query:    query,
		Path:     q.Get("path"),
		Category: category,
		Modified: modifiedDays,
		Sort:     sortKey,
		Order:    order,
		Content:  content,
		Limit:    limit,
	})
	if err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"query":     query,
		"items":     result.Items,
		"terms":     result.Terms,
		"total":     result.Total,
		"truncated": result.Truncated,
		"partial":   result.Partial,
		"content":   content,
	})
}

func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request) {
	multipartReader, err := r.MultipartReader()
	if err != nil {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("invalid multipart form: %w", err))
		return
	}

	settings, err := s.getSettings()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	maxBytes := int64(settings.MaxUploadMB) * 1024 * 1024
	path := r.URL.Query().Get("path")
	for {
		part, err := multipartReader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			errorJSON(w, http.StatusBadRequest, fmt.Errorf("invalid multipart form: %w", err))
			return
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		entry, err := s.store.SaveLimit(path, part, part.FileName(), part.Header.Get("Content-Type"), maxBytes)
		if errors.Is(err, storage.ErrFileTooLarge) {
			errorJSON(w, http.StatusRequestEntityTooLarge, fmt.Errorf("файл превышает установленный лимит %d МБ", settings.MaxUploadMB))
			return
		}
		_ = part.Close()
		if err != nil {
			errorJSON(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, entry)
		return
	}
	errorJSON(w, http.StatusBadRequest, fmt.Errorf("file is required"))
}

func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	file, info, clean, err := s.store.Open(path)
	if err != nil {
		errorJSON(w, http.StatusNotFound, err)
		return
	}
	defer file.Close()

	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(info.Name())))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filepath.Base(clean)))
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, filepath.Base(clean), info.ModTime(), file)
}

func (s *Server) downloadFilesArchive(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Paths []string `json:"paths"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	if len(payload.Paths) < 2 {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("at least two paths are required"))
		return
	}

	archiveEntries, err := s.store.ArchiveEntries(payload.Paths)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	if len(archiveEntries) == 0 {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("no files to download"))
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="personal-cloud.zip"`)

	zw := zip.NewWriter(w)
	if err := s.store.WriteArchive(zw, archiveEntries); err != nil {
		_ = zw.Close()
		return
	}
	if err := zw.Close(); err != nil {
		return
	}
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
	if err := decodeJSON(r, &payload); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	entry, err := s.store.CreateFolder(payload.Path)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func (s *Server) renameFile(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	entry, err := s.store.Rename(payload.Path, payload.Name)
	if err != nil {
		errorJSON(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) moveFile(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Path        string `json:"path"`
		Destination string `json:"destination"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	entry, err := s.store.Move(payload.Path, payload.Destination)
	if err != nil {
		errorJSON(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) copyFile(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Path        string `json:"path"`
		Destination string `json:"destination"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	entry, err := s.store.Copy(payload.Path, payload.Destination)
	if err != nil {
		errorJSON(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, entry)
}

func decodeJSON(r *http.Request, target any) error {
	body := io.LimitReader(r.Body, 1<<20)
	defer r.Body.Close()
	if err := json.NewDecoder(body).Decode(target); err != nil {
		return fmt.Errorf("invalid JSON")
	}
	return nil
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
