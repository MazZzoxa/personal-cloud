package api

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"personal-cloud/server/internal/storage"
)

// previewFile serves an image, video, audio file or PDF inline so the browser
// can display it. Only types from an allow-list are served; everything else
// must be downloaded. Range requests are supported for seeking in media.
func (s *Server) previewFile(w http.ResponseWriter, r *http.Request) {
	file, info, clean, err := s.store.Open(r.URL.Query().Get("path"))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		errorJSON(w, status, err)
		return
	}
	defer file.Close()

	name := filepath.Base(clean)
	contentType := storage.PreviewContentType(name)
	if contentType == "" {
		errorJSON(w, http.StatusUnsupportedMediaType, fmt.Errorf("предпросмотр этого типа файлов недоступен"))
		return
	}

	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	h.Set("Accept-Ranges", "bytes")
	h.Set("Cache-Control", "private, no-cache")
	// The preview is embedded by the app itself, so allow same-origin framing
	// (the global default is DENY).
	h.Set("X-Frame-Options", "SAMEORIGIN")
	if contentType != "application/pdf" {
		// A sandboxed, script-less context: an SVG opened directly in a tab
		// can never run scripts with access to the cloud. Chrome refuses to
		// show PDFs inside a sandbox, so PDFs rely on nosniff alone.
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	}
	http.ServeContent(w, r, name, info.ModTime(), file)
}

// previewText returns the beginning of a text file as JSON, converted to UTF-8.
func (s *Server) previewText(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	limit := int64(0)
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 1 {
			errorJSON(w, http.StatusBadRequest, fmt.Errorf("некорректный лимит"))
			return
		}
		if parsed > storage.MaxTextPreviewBytes {
			parsed = storage.MaxTextPreviewBytes
		}
		limit = parsed
	}

	preview, err := s.store.ReadText(r.URL.Query().Get("path"), limit)
	switch {
	case errors.Is(err, storage.ErrNotText):
		errorJSON(w, http.StatusUnsupportedMediaType, fmt.Errorf("файл не является текстовым"))
		return
	case errors.Is(err, os.ErrNotExist):
		errorJSON(w, http.StatusNotFound, err)
		return
	case err != nil:
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}
