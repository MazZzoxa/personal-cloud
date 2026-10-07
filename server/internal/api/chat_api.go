package api

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"personal-cloud/server/internal/chat"
)

func (s *Server) listChatMessages(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)

	messages, hasMore, err := s.chat.List(r.Context(), limit, before)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":   messages,
		"hasMore": hasMore,
	})
}

func (s *Server) createChatMessage(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(strings.ToLower(contentType), "multipart/form-data") {
		s.createMultipartChatMessage(w, r)
		return
	}

	var payload struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	message, err := s.chat.CreateText(r.Context(), payload.Body)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	s.chatHub.Broadcast(chat.Event{Type: "message", Message: &message})
	writeJSON(w, http.StatusCreated, message)
}

func (s *Server) createMultipartChatMessage(w http.ResponseWriter, r *http.Request) {
	reader, err := r.MultipartReader()
	if err != nil {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("invalid multipart form: %w", err))
		return
	}

	body := ""
	uploads := make([]chat.FileUpload, 0, chat.MaxAttachmentsPerMessage)
	cleanup := func() {
		for _, upload := range uploads {
			_ = os.Remove(upload.Path)
		}
	}
	defer func() {
		cleanup()
	}()

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			errorJSON(w, http.StatusBadRequest, fmt.Errorf("invalid multipart form: %w", err))
			return
		}

		if part.FileName() == "" {
			if part.FormName() == "body" {
				value, readErr := io.ReadAll(io.LimitReader(part, chat.MaxMessageLength+1))
				if readErr != nil {
					errorJSON(w, http.StatusBadRequest, readErr)
					return
				}
				body = string(value)
			}
			_ = part.Close()
			continue
		}

		if len(uploads) >= chat.MaxAttachmentsPerMessage {
			errorJSON(w, http.StatusBadRequest, fmt.Errorf("maximum %d attachments per message", chat.MaxAttachmentsPerMessage))
			return
		}

		name := part.FileName()
		mimeType := part.Header.Get("Content-Type")
		tempPath, size, copyErr := s.chat.CopyToTemp(r.Context(), part)
		_ = part.Close()
		if copyErr != nil {
			errorJSON(w, http.StatusBadRequest, copyErr)
			return
		}
		uploads = append(uploads, chat.FileUpload{
			Name:     name,
			MimeType: mimeType,
			Size:     size,
			Path:     tempPath,
		})
	}

	message, err := s.chat.CreateFromFiles(r.Context(), body, uploads)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}

	// Files are moved out of the temporary directory by CreateFromFiles.
	uploads = nil
	s.chatHub.Broadcast(chat.Event{Type: "message", Message: &message})
	writeJSON(w, http.StatusCreated, message)
}

func (s *Server) updateChatMessage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("invalid message id"))
		return
	}
	var payload struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	message, err := s.chat.UpdateText(r.Context(), id, payload.Body)
	if err != nil {
		status := http.StatusBadRequest
		if err == sql.ErrNoRows {
			status = http.StatusNotFound
		}
		errorJSON(w, status, err)
		return
	}
	s.chatHub.Broadcast(chat.Event{Type: "message_updated", Message: &message})
	writeJSON(w, http.StatusOK, message)
}

func (s *Server) deleteChatMessage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("invalid message id"))
		return
	}
	if err := s.chat.Delete(r.Context(), id); err != nil {
		status := http.StatusBadRequest
		if err == sql.ErrNoRows {
			status = http.StatusNotFound
		}
		errorJSON(w, status, err)
		return
	}
	s.chatHub.Broadcast(chat.Event{Type: "message_deleted", MessageID: id})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteChatMessages(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	if len(payload.IDs) == 0 || len(payload.IDs) > 100 {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("select between 1 and 100 messages"))
		return
	}
	deleted, err := s.chat.DeleteMany(r.Context(), payload.IDs)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	for _, id := range deleted {
		s.chatHub.Broadcast(chat.Event{Type: "message_deleted", MessageID: id})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) downloadSelectedChat(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	if len(payload.IDs) == 0 || len(payload.IDs) > 100 {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("select between 1 and 100 messages"))
		return
	}
	messages, err := s.chat.ListByIDs(r.Context(), payload.IDs)
	if err != nil {
		if err == sql.ErrNoRows {
			errorJSON(w, http.StatusNotFound, err)
			return
		}
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	if len(messages) == 0 {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("no messages to download"))
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="personal-cloud-chat.zip"`)
	zw := zip.NewWriter(w)
	defer zw.Close()

	textFile, err := zw.Create("messages.txt")
	if err != nil {
		return
	}
	for _, message := range messages {
		if _, err := fmt.Fprintf(textFile, "[%s] Вы:\n%s\n\n", message.CreatedAt, message.Body); err != nil {
			return
		}
	}

	for _, message := range messages {
		for _, attachment := range message.Attachments {
			_, path, err := s.chat.GetAttachment(r.Context(), attachment.ID)
			if err != nil {
				continue
			}
			file, err := os.Open(path)
			if err != nil {
				continue
			}
			name := fmt.Sprintf("attachments/%d-%s", message.ID, filepath.Base(attachment.Name))
			entry, err := zw.Create(name)
			if err == nil {
				_, _ = io.Copy(entry, file)
			}
			_ = file.Close()
		}
	}
}

func (s *Server) chatWebSocket(w http.ResponseWriter, r *http.Request) {
	s.chatHub.ServeWS(w, r, func(_ *chat.Client, payload []byte) error {
		var incoming struct {
			Type string `json:"type"`
			Body string `json:"body"`
		}
		if err := json.Unmarshal(payload, &incoming); err != nil {
			return fmt.Errorf("invalid WebSocket message")
		}
		if incoming.Type != "message" {
			return nil
		}

		message, err := s.chat.CreateText(r.Context(), incoming.Body)
		if err != nil {
			return err
		}
		s.chatHub.Broadcast(chat.Event{Type: "message", Message: &message})
		return nil
	})
}

func isInlineImageMime(contentType string) bool {
	switch strings.ToLower(contentType) {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/avif":
		return true
	default:
		return false
	}
}

func (s *Server) downloadChatAttachment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("invalid attachment id"))
		return
	}

	attachment, path, err := s.chat.GetAttachment(r.Context(), id)
	if err != nil {
		errorJSON(w, http.StatusNotFound, err)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		errorJSON(w, http.StatusNotFound, err)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		errorJSON(w, http.StatusNotFound, err)
		return
	}
	defer file.Close()

	contentType := attachment.MimeType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	dispositionType := "attachment"
	if isInlineImageMime(contentType) {
		dispositionType = "inline"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(dispositionType, map[string]string{"filename": attachment.Name}))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, attachment.Name, info.ModTime(), file)
}
