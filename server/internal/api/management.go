package api

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"personal-cloud/server/internal/systemdisk"
)

type AppSettings struct {
	CloudName        string `json:"cloudName"`
	MaxUploadMB      int    `json:"maxUploadMB"`
	LogRetentionDays int    `json:"logRetentionDays"`
}

func defaultAppSettings() AppSettings {
	return AppSettings{CloudName: "Personal Cloud", MaxUploadMB: 5120, LogRetentionDays: 30}
}

func (s *Server) getSettings() (AppSettings, error) {
	settings := defaultAppSettings()
	rows, err := s.db.Query(`SELECT key, value FROM app_settings WHERE key IN ('cloud_name', 'max_upload_mb', 'log_retention_days')`)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return settings, err
		}
		switch key {
		case "cloud_name":
			if strings.TrimSpace(value) != "" {
				settings.CloudName = value
			}
		case "max_upload_mb":
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 1 {
				settings.MaxUploadMB = parsed
			}
		case "log_retention_days":
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 1 {
				settings.LogRetentionDays = parsed
			}
		}
	}
	return settings, rows.Err()
}

func (s *Server) getSettingsAPI(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	settings, err := s.getSettings()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func validateCloudName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("название облака не может быть пустым")
	}
	if len([]rune(value)) > 50 {
		return "", fmt.Errorf("название облака не должно превышать 50 символов")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("название облака содержит недопустимые символы")
		}
	}
	return value, nil
}

func (s *Server) updateSettingsAPI(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var input struct {
		CloudName        *string `json:"cloudName"`
		MaxUploadMB      *int    `json:"maxUploadMB"`
		LogRetentionDays *int    `json:"logRetentionDays"`
	}
	if err := decodeJSON(r, &input); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	settings, err := s.getSettings()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	if input.CloudName != nil {
		settings.CloudName, err = validateCloudName(*input.CloudName)
		if err != nil {
			errorJSON(w, http.StatusBadRequest, err)
			return
		}
	}
	if input.MaxUploadMB != nil {
		if *input.MaxUploadMB < 1 || *input.MaxUploadMB > 51200 {
			errorJSON(w, http.StatusBadRequest, fmt.Errorf("лимит загрузки должен быть от 1 до 51200 МБ"))
			return
		}
		settings.MaxUploadMB = *input.MaxUploadMB
	}
	if input.LogRetentionDays != nil {
		if *input.LogRetentionDays < 1 || *input.LogRetentionDays > 365 {
			errorJSON(w, http.StatusBadRequest, fmt.Errorf("срок хранения журналов должен быть от 1 до 365 дней"))
			return
		}
		settings.LogRetentionDays = *input.LogRetentionDays
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	for key, value := range map[string]string{
		"cloud_name":         settings.CloudName,
		"max_upload_mb":      strconv.Itoa(settings.MaxUploadMB),
		"log_retention_days": strconv.Itoa(settings.LogRetentionDays),
	} {
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO app_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			_ = tx.Rollback()
			errorJSON(w, http.StatusInternalServerError, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) storageInfo(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	storageBytes, fileCount, folderCount, err := directoryStats(s.store.Root())
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	chatBytes, _, _, err := directoryStats(s.chatRoot)
	if err != nil && !os.IsNotExist(err) {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	if os.IsNotExist(err) {
		chatBytes = 0
	}

	var databaseBytes uint64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, statErr := os.Stat(filepath.Join(s.dataDir, "cloud.db"+suffix))
		if statErr == nil {
			databaseBytes += uint64(max64(0, info.Size()))
		}
	}

	totalBytes, freeBytes, diskErr := systemdisk.Space(s.store.Root())
	writeJSON(w, http.StatusOK, map[string]any{
		"fileCount":       fileCount,
		"folderCount":     folderCount,
		"storageBytes":    storageBytes,
		"chatBytes":       chatBytes,
		"databaseBytes":   databaseBytes,
		"totalCloudBytes": storageBytes + chatBytes + databaseBytes,
		"diskTotalBytes":  totalBytes,
		"diskFreeBytes":   freeBytes,
		"diskAvailable":   diskErr == nil,
	})
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func directoryStats(root string) (bytes uint64, files, folders int64, err error) {
	if root == "" {
		return 0, 0, 0, nil
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.IsDir() && (entry.Name() == ".incoming" || strings.HasPrefix(entry.Name(), ".upload-")) {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			folders++
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".upload-") {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		files++
		if info.Size() > 0 {
			bytes += uint64(info.Size())
		}
		return nil
	})
	return
}

type auditLog struct {
	ID         int64  `json:"id"`
	CreatedAt  string `json:"createdAt"`
	Level      string `json:"level"`
	Event      string `json:"event"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Status     int    `json:"status"`
	DeviceName string `json:"deviceName"`
	IP         string `json:"ip"`
}

func (s *Server) listLogs(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	level := r.URL.Query().Get("level")
	if level != "info" && level != "warning" && level != "error" {
		level = ""
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	like := "%" + query + "%"
	rows, err := s.db.QueryContext(r.Context(), `
SELECT id, created_at, level, event, method, path, status, device_name, ip
FROM audit_logs
WHERE (? = 0 OR id < ?)
  AND (? = '' OR level = ?)
  AND (? = '' OR event LIKE ? OR path LIKE ? OR device_name LIKE ? OR ip LIKE ?)
ORDER BY id DESC LIMIT ?`, before, before, level, level, query, like, like, like, like, limit+1)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	items := make([]auditLog, 0, limit+1)
	for rows.Next() {
		var item auditLog
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.Level, &item.Event, &item.Method, &item.Path, &item.Status, &item.DeviceName, &item.IP); err != nil {
			errorJSON(w, http.StatusInternalServerError, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": hasMore})
}

func (s *Server) clearLogs(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM audit_logs`); err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func shouldAudit(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	switch r.Method {
	case http.MethodGet:
		return strings.HasPrefix(r.URL.Path, "/api/backups/") && strings.HasSuffix(r.URL.Path, "/download")
	case http.MethodPost:
		return r.URL.Path == "/api/backups" || r.URL.Path == "/api/backups/import" ||
			(strings.HasPrefix(r.URL.Path, "/api/backups/") && strings.HasSuffix(r.URL.Path, "/restore")) ||
			r.URL.Path == "/api/auth/pair" || r.URL.Path == "/api/auth/logout" ||
			r.URL.Path == "/api/devices/pairing" || r.URL.Path == "/api/files" || r.URL.Path == "/api/folders" ||
			r.URL.Path == "/api/files/move" || r.URL.Path == "/api/files/copy" ||
			r.URL.Path == "/api/chat/messages" || r.URL.Path == "/api/chat/delete"
	case http.MethodPatch:
		return r.URL.Path == "/api/files/rename" || strings.HasPrefix(r.URL.Path, "/api/devices/") || strings.HasPrefix(r.URL.Path, "/api/chat/messages/") || r.URL.Path == "/api/settings" || r.URL.Path == "/api/backups/settings"
	case http.MethodDelete:
		return r.URL.Path == "/api/files" || strings.HasPrefix(r.URL.Path, "/api/devices/") || strings.HasPrefix(r.URL.Path, "/api/chat/messages/") || r.URL.Path == "/api/logs" || strings.HasPrefix(r.URL.Path, "/api/backups/")
	}
	return false
}

func auditEvent(method, route string, status int) string {
	var event string
	switch {
	case route == "POST /api/auth/pair":
		event = "Подключение устройства"
	case route == "POST /api/auth/logout":
		event = "Выход устройства"
	case route == "POST /api/devices/pairing":
		event = "Создание кода подключения"
	case strings.HasPrefix(route, "PATCH /api/devices/"):
		event = "Переименование устройства"
	case strings.HasPrefix(route, "DELETE /api/devices/"):
		event = "Отзыв доступа устройства"
	case route == "POST /api/files":
		event = "Загрузка файла"
	case route == "DELETE /api/files":
		event = "Удаление файла или папки"
	case route == "POST /api/folders":
		event = "Создание папки"
	case route == "PATCH /api/files/rename":
		event = "Переименование файла или папки"
	case route == "POST /api/files/move":
		event = "Перемещение файла или папки"
	case route == "POST /api/files/copy":
		event = "Копирование файла или папки"
	case route == "POST /api/chat/messages":
		event = "Отправка сообщения в чате"
	case route == "PATCH /api/chat/messages/{id}":
		event = "Изменение сообщения в чате"
	case route == "DELETE /api/chat/messages/{id}" || route == "POST /api/chat/delete":
		event = "Удаление сообщения в чате"
	case route == "PATCH /api/settings":
		event = "Изменение настроек облака"
	case route == "DELETE /api/logs":
		event = "Очистка журнала событий"
	case route == "POST /api/backups":
		event = "Создание резервной копии"
	case route == "POST /api/backups/import":
		event = "Загрузка резервной копии"
	case route == "POST /api/backups/{name}/restore":
		event = "Восстановление из резервной копии"
	case route == "PATCH /api/backups/settings":
		event = "Изменение настроек резервного копирования"
	case route == "DELETE /api/backups/{name}":
		event = "Удаление резервной копии"
	case route == "GET /api/backups/{name}/download":
		event = "Скачивание резервной копии"
	default:
		event = method + " " + route
	}
	if status >= 500 {
		return event + " — ошибка сервера"
	}
	if status >= 400 {
		return event + " — действие не выполнено"
	}
	return event
}

func auditRoute(r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	path := r.URL.Path
	for _, prefix := range []string{"/api/devices/", "/api/chat/messages/"} {
		if strings.HasPrefix(path, prefix) {
			return prefix + "{id}"
		}
	}
	return path
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !shouldAudit(r) {
			next.ServeHTTP(w, r)
			return
		}
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		route := auditRoute(r)
		event := auditEvent(r.Method, route, status)
		level := "info"
		if status >= 500 {
			level = "error"
		} else if status >= 400 {
			level = "warning"
		}
		device, _ := deviceFromContext(r.Context())
		deviceID := any(nil)
		deviceName := ""
		if device.ID > 0 {
			deviceID = device.ID
			deviceName = device.Name
		}
		// Use an independent short-lived context: a client may disconnect just after
		// the response, but that should not discard the audit event.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = s.db.ExecContext(ctx, `INSERT INTO audit_logs (created_at, level, event, method, path, status, device_id, device_name, ip) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			time.Now().UTC().Format(time.RFC3339Nano), level, event, r.Method, route, status, deviceID, deviceName, clientIP(r))
		settings, err := s.getSettings()
		if err == nil && settings.LogRetentionDays > 0 {
			cutoff := time.Now().UTC().Add(-time.Duration(settings.LogRetentionDays) * 24 * time.Hour).Format(time.RFC3339Nano)
			_, _ = s.db.ExecContext(ctx, `DELETE FROM audit_logs WHERE created_at < ?`, cutoff)
		}
	})
}
