package chat

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	MaxMessageLength         = 32 * 1024
	MaxAttachmentSize        = 100 * 1024 * 1024
	MaxAttachmentsPerMessage = 8
)

type Attachment struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
}

type Message struct {
	ID          int64        `json:"id"`
	Body        string       `json:"body"`
	CreatedAt   string       `json:"createdAt"`
	Attachments []Attachment `json:"attachments"`
}

type FileUpload struct {
	Name     string
	MimeType string
	Size     int64
	Path     string
}

type Service struct {
	db   *sql.DB
	root string
}

func NewService(db *sql.DB, root string) *Service {
	return &Service{db: db, root: root}
}

func (s *Service) TempDir() string {
	return filepath.Join(s.root, ".incoming")
}

func (s *Service) List(ctx context.Context, limit int, before int64) ([]Message, bool, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, body, created_at
FROM chat_messages
WHERE (? = 0 OR id < ?)
ORDER BY id DESC
LIMIT ?`, before, before, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	messages := make([]Message, 0, limit+1)
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.ID, &message.Body, &message.CreatedAt); err != nil {
			return nil, false, err
		}
		message.Attachments = []Attachment{}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}

	for index := range messages {
		attachments, err := s.loadAttachments(ctx, messages[index].ID)
		if err != nil {
			return nil, false, err
		}
		messages[index].Attachments = attachments
	}

	sort.Slice(messages, func(i, j int) bool {
		return messages[i].ID < messages[j].ID
	})
	return messages, hasMore, nil
}

func (s *Service) CreateText(ctx context.Context, body string) (Message, error) {
	return s.create(ctx, body, nil)
}

func (s *Service) CreateFromFiles(ctx context.Context, body string, uploads []FileUpload) (Message, error) {
	return s.create(ctx, body, uploads)
}

func (s *Service) create(ctx context.Context, body string, uploads []FileUpload) (Message, error) {
	body = strings.TrimSpace(body)
	if len(body) > MaxMessageLength {
		return Message{}, fmt.Errorf("message is too long")
	}
	if len(uploads) > MaxAttachmentsPerMessage {
		return Message{}, fmt.Errorf("too many attachments")
	}
	if body == "" && len(uploads) == 0 {
		return Message{}, errors.New("message or attachment is required")
	}

	for index := range uploads {
		if uploads[index].Size < 0 || uploads[index].Size > MaxAttachmentSize {
			return Message{}, fmt.Errorf("attachment %q is too large", uploads[index].Name)
		}
		if uploads[index].Path == "" {
			return Message{}, fmt.Errorf("attachment %q has no temporary file", uploads[index].Name)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, err
	}

	cleanup := func() {
		_ = tx.Rollback()
		for _, upload := range uploads {
			_ = os.Remove(upload.Path)
		}
	}

	result, err := tx.ExecContext(ctx, `INSERT INTO chat_messages (body, created_at) VALUES (?, ?)`, body, now)
	if err != nil {
		cleanup()
		return Message{}, err
	}
	messageID, err := result.LastInsertId()
	if err != nil {
		cleanup()
		return Message{}, err
	}

	message := Message{ID: messageID, Body: body, CreatedAt: now, Attachments: []Attachment{}}
	finalized := make([]string, 0, len(uploads))
	for _, upload := range uploads {
		name, err := cleanFilename(upload.Name)
		if err != nil {
			cleanup()
			for _, path := range finalized {
				_ = os.Remove(path)
			}
			return Message{}, err
		}
		mimeType := normalizeMime(upload.MimeType, name)
		storedName := fmt.Sprintf("%d-%s-%s", messageID, randomToken(), name)
		finalPath := filepath.Join(s.root, storedName)
		if err := os.Rename(upload.Path, finalPath); err != nil {
			cleanup()
			for _, path := range finalized {
				_ = os.Remove(path)
			}
			return Message{}, err
		}
		finalized = append(finalized, finalPath)

		attachmentResult, err := tx.ExecContext(ctx, `
INSERT INTO chat_attachments (message_id, name, storage_name, mime_type, size, created_at)
VALUES (?, ?, ?, ?, ?, ?)`,
			messageID, name, storedName, mimeType, upload.Size, now)
		if err != nil {
			cleanup()
			for _, path := range finalized {
				_ = os.Remove(path)
			}
			return Message{}, err
		}
		attachmentID, err := attachmentResult.LastInsertId()
		if err != nil {
			cleanup()
			for _, path := range finalized {
				_ = os.Remove(path)
			}
			return Message{}, err
		}
		message.Attachments = append(message.Attachments, Attachment{
			ID:       attachmentID,
			Name:     name,
			MimeType: mimeType,
			Size:     upload.Size,
			URL:      fmt.Sprintf("/api/chat/attachments/%d", attachmentID),
		})
	}

	if err := tx.Commit(); err != nil {
		for _, path := range finalized {
			_ = os.Remove(path)
		}
		return Message{}, err
	}

	return message, nil
}

func (s *Service) GetAttachment(ctx context.Context, id int64) (Attachment, string, error) {
	if id <= 0 {
		return Attachment{}, "", errors.New("invalid attachment id")
	}
	var attachment Attachment
	var storageName string
	if err := s.db.QueryRowContext(ctx, `
SELECT id, name, storage_name, mime_type, size
FROM chat_attachments
WHERE id = ?`, id).Scan(&attachment.ID, &attachment.Name, &storageName, &attachment.MimeType, &attachment.Size); err != nil {
		return Attachment{}, "", err
	}
	attachment.URL = fmt.Sprintf("/api/chat/attachments/%d", attachment.ID)
	if strings.Contains(storageName, "/") || strings.Contains(storageName, "\\") || storageName == "." || storageName == ".." {
		return Attachment{}, "", errors.New("invalid stored attachment path")
	}
	return attachment, filepath.Join(s.root, storageName), nil
}

func (s *Service) loadAttachments(ctx context.Context, messageID int64) ([]Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, mime_type, size
FROM chat_attachments
WHERE message_id = ?
ORDER BY id ASC`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	attachments := make([]Attachment, 0)
	for rows.Next() {
		var attachment Attachment
		if err := rows.Scan(&attachment.ID, &attachment.Name, &attachment.MimeType, &attachment.Size); err != nil {
			return nil, err
		}
		attachment.URL = fmt.Sprintf("/api/chat/attachments/%d", attachment.ID)
		attachments = append(attachments, attachment)
	}
	return attachments, rows.Err()
}

func (s *Service) Get(ctx context.Context, id int64) (Message, error) {
	if id <= 0 {
		return Message{}, errors.New("invalid message id")
	}
	var message Message
	if err := s.db.QueryRowContext(ctx, `
SELECT id, body, created_at
FROM chat_messages
WHERE id = ?`, id).Scan(&message.ID, &message.Body, &message.CreatedAt); err != nil {
		return Message{}, err
	}
	attachments, err := s.loadAttachments(ctx, message.ID)
	if err != nil {
		return Message{}, err
	}
	message.Attachments = attachments
	return message, nil
}

func (s *Service) UpdateText(ctx context.Context, id int64, body string) (Message, error) {
	body = strings.TrimSpace(body)
	if id <= 0 {
		return Message{}, errors.New("invalid message id")
	}
	if body == "" {
		return Message{}, errors.New("message cannot be empty")
	}
	if len(body) > MaxMessageLength {
		return Message{}, fmt.Errorf("message is too long")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE chat_messages SET body = ? WHERE id = ?`, body, id)
	if err != nil {
		return Message{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Message{}, err
	}
	if count == 0 {
		return Message{}, sql.ErrNoRows
	}
	return s.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New("invalid message id")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT storage_name
FROM chat_attachments
WHERE message_id = ?`, id)
	if err != nil {
		return err
	}
	var storageNames []string
	for rows.Next() {
		var storageName string
		if err := rows.Scan(&storageName); err != nil {
			rows.Close()
			return err
		}
		storageNames = append(storageNames, storageName)
	}
	if err := rows.Close(); err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM chat_messages WHERE id = ?`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	for _, storageName := range storageNames {
		if storageName == "" || strings.Contains(storageName, "/") || strings.Contains(storageName, "\\") || storageName == "." || storageName == ".." {
			continue
		}
		_ = os.Remove(filepath.Join(s.root, storageName))
	}
	return nil
}

// DeleteMany removes several messages in one transaction and returns the ids that existed.
func (s *Service) DeleteMany(ctx context.Context, ids []int64) ([]int64, error) {
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return []int64{}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var storageNames []string
	deleted := make([]int64, 0, len(unique))
	for _, id := range unique {
		rows, err := tx.QueryContext(ctx, `SELECT storage_name FROM chat_attachments WHERE message_id = ?`, id)
		if err != nil {
			return nil, err
		}
		var names []string
		for rows.Next() {
			var storageName string
			if err := rows.Scan(&storageName); err != nil {
				rows.Close()
				return nil, err
			}
			names = append(names, storageName)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM chat_attachments WHERE message_id = ?`, id); err != nil {
			return nil, err
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM chat_messages WHERE id = ?`, id)
		if err != nil {
			return nil, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if count > 0 {
			deleted = append(deleted, id)
			storageNames = append(storageNames, names...)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for _, storageName := range storageNames {
		if storageName == "" || strings.Contains(storageName, "/") || strings.Contains(storageName, "\\") || storageName == "." || storageName == ".." {
			continue
		}
		_ = os.Remove(filepath.Join(s.root, storageName))
	}
	return deleted, nil
}

func (s *Service) ListByIDs(ctx context.Context, ids []int64) ([]Message, error) {
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return []Message{}, nil
	}

	messages := make([]Message, 0, len(unique))
	for _, id := range unique {
		message, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].ID < messages[j].ID })
	return messages, nil
}

func cleanFilename(value string) (string, error) {
	value = strings.TrimSpace(value)
	value = filepath.Base(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\") {
		return "", fmt.Errorf("invalid attachment name")
	}
	if len(value) > 160 {
		ext := filepath.Ext(value)
		base := strings.TrimSuffix(value, ext)
		maxBase := 160 - len(ext)
		if maxBase < 1 {
			return "", fmt.Errorf("attachment name is too long")
		}
		value = base[:maxBase] + ext
	}
	return value, nil
}

func normalizeMime(value, filename string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		if semicolon := strings.IndexByte(value, ';'); semicolon >= 0 {
			value = strings.TrimSpace(value[:semicolon])
		}
		if value != "" {
			return value
		}
	}
	if detected := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); detected != "" {
		return detected
	}
	return "application/octet-stream"
}

func randomToken() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err == nil {
		return hex.EncodeToString(buffer)
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// CopyToTemp streams an uploaded file into the chat temp directory and enforces the per-file limit.
func (s *Service) CopyToTemp(ctx context.Context, src io.Reader) (string, int64, error) {
	if err := os.MkdirAll(s.TempDir(), 0o755); err != nil {
		return "", 0, err
	}
	file, err := os.CreateTemp(s.TempDir(), ".upload-*")
	if err != nil {
		return "", 0, err
	}
	path := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(path)
	}

	limited := io.LimitReader(src, MaxAttachmentSize+1)
	size, err := io.Copy(file, limited)
	if err != nil {
		cleanup()
		return "", 0, err
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return "", 0, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", 0, err
	}
	if size > MaxAttachmentSize {
		_ = os.Remove(path)
		return "", 0, fmt.Errorf("attachment exceeds the %d MB limit", MaxAttachmentSize/(1024*1024))
	}
	return path, size, nil
}
