package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"personal-cloud/server/internal/backup"
	"personal-cloud/server/internal/systemdisk"
)

const (
	defaultBackupKeep  = 7
	maxBackupKeep      = 100
	maxBackupIntervalH = 24 * 30
)

type backupSettings struct {
	Dir           string `json:"dir"`           // custom backup folder; empty = default
	Keep          int    `json:"keep"`          // automatic backups to keep
	IntervalHours int    `json:"intervalHours"` // 0 = automatic backups are off
}

func (s *Server) getBackupSettings() (backupSettings, error) {
	cfg := backupSettings{Keep: defaultBackupKeep}
	rows, err := s.db.Query(`SELECT key, value FROM app_settings WHERE key IN ('backup_dir', 'backup_keep', 'backup_interval_hours')`)
	if err != nil {
		return cfg, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return cfg, err
		}
		switch key {
		case "backup_dir":
			cfg.Dir = strings.TrimSpace(value)
		case "backup_keep":
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 1 && parsed <= maxBackupKeep {
				cfg.Keep = parsed
			}
		case "backup_interval_hours":
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 && parsed <= maxBackupIntervalH {
				cfg.IntervalHours = parsed
			}
		}
	}
	return cfg, rows.Err()
}

func (s *Server) backupDirFor(cfg backupSettings) string {
	if cfg.Dir != "" {
		return cfg.Dir
	}
	return s.backup.DefaultDir()
}

// usableBackupDir returns the configured backup folder and refuses to continue
// when a custom folder is missing (for example an unplugged external drive):
// creating it silently could put the "backup" on the system disk.
func (s *Server) usableBackupDir() (string, backupSettings, error) {
	cfg, err := s.getBackupSettings()
	if err != nil {
		return "", cfg, err
	}
	dir := s.backupDirFor(cfg)
	if cfg.Dir != "" {
		if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
			return "", cfg, fmt.Errorf("папка резервных копий недоступна: %s. Подключите диск или выберите другую папку в настройках копирования", dir)
		}
	}
	return dir, cfg, nil
}

func backupErrorStatus(err error) int {
	switch {
	case errors.Is(err, backup.ErrBusy):
		return http.StatusConflict
	case errors.Is(err, backup.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, backup.ErrInvalidName), errors.Is(err, backup.ErrInvalidBackup):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// auditSystem records an event that was not caused by a single HTTP request.
func (s *Server) auditSystem(level, event string, status int) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(ctx, `INSERT INTO audit_logs (created_at, level, event, method, path, status, device_id, device_name, ip) VALUES (?, ?, ?, 'SYSTEM', 'backup', ?, NULL, 'Система', '')`,
		time.Now().UTC().Format(time.RFC3339Nano), level, event, status)
}

func (s *Server) auditBackupResult(okEvent, failEvent, name string, err error) {
	if err != nil {
		message := err.Error()
		if runes := []rune(message); len(runes) > 300 {
			message = string(runes[:300]) + "…"
		}
		s.auditSystem("error", failEvent+": "+message, http.StatusInternalServerError)
		return
	}
	if name != "" {
		okEvent += " (" + name + ")"
	}
	s.auditSystem("info", okEvent, http.StatusOK)
}

// ------------------------------------------------------------------ handlers

// GET /api/backups
func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	cfg, err := s.getBackupSettings()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	dir := s.backupDirFor(cfg)
	dirAvailable := true
	probe := dir
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		dirAvailable = cfg.Dir == "" // the default folder is created on first use
		probe = s.dataDir
	}
	items, err := backup.List(dir)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	_, free, diskErr := systemdisk.Space(probe)
	writeJSON(w, http.StatusOK, map[string]any{
		"items":         items,
		"dir":           dir,
		"defaultDir":    s.backup.DefaultDir(),
		"settings":      cfg,
		"job":           s.backup.Status(),
		"dirAvailable":  dirAvailable,
		"diskAvailable": diskErr == nil,
		"diskFreeBytes": free,
	})
}

// GET /api/backups/status
func (s *Server) backupStatus(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	writeJSON(w, http.StatusOK, s.backup.Status())
}

// POST /api/backups
func (s *Server) createBackup(w http.ResponseWriter, r *http.Request) {
	dir, cfg, err := s.usableBackupDir()
	if err != nil {
		errorJSON(w, http.StatusConflict, err)
		return
	}
	err = s.backup.StartBackup(dir, backup.KindManual, cfg.Keep, func(name string, err error) {
		s.auditBackupResult("Резервная копия создана", "Не удалось создать резервную копию", name, err)
	})
	if err != nil {
		errorJSON(w, backupErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusAccepted, s.backup.Status())
}

// POST /api/backups/import — multipart upload of a backup archive (field "file").
func (s *Server) importBackup(w http.ResponseWriter, r *http.Request) {
	dir, _, err := s.usableBackupDir()
	if err != nil {
		errorJSON(w, http.StatusConflict, err)
		return
	}
	reader, err := r.MultipartReader()
	if err != nil {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("invalid multipart form: %w", err))
		return
	}
	for {
		part, err := reader.NextPart()
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
		info, err := s.backup.Import(dir, part)
		_ = part.Close()
		if err != nil {
			errorJSON(w, backupErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusCreated, info)
		return
	}
	errorJSON(w, http.StatusBadRequest, fmt.Errorf("file is required"))
}

// GET /api/backups/{name}/download
func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	dir, _, err := s.usableBackupDir()
	if err != nil {
		errorJSON(w, http.StatusConflict, err)
		return
	}
	name := r.PathValue("name")
	full, err := backup.Resolve(dir, name)
	if err != nil {
		errorJSON(w, backupErrorStatus(err), err)
		return
	}
	file, err := os.Open(full)
	if err != nil {
		errorJSON(w, http.StatusNotFound, backup.ErrNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, name))
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, name, info.ModTime(), file)
}

// POST /api/backups/{name}/verify
func (s *Server) verifyBackup(w http.ResponseWriter, r *http.Request) {
	dir, _, err := s.usableBackupDir()
	if err != nil {
		errorJSON(w, http.StatusConflict, err)
		return
	}
	name := r.PathValue("name")
	err = s.backup.StartVerify(dir, name, func(err error) {
		if err != nil {
			s.auditBackupResult("", "Проверка резервной копии "+name+" не пройдена", "", err)
		}
	})
	if err != nil {
		errorJSON(w, backupErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusAccepted, s.backup.Status())
}

// POST /api/backups/{name}/restore
func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSON(r, &payload); err != nil || !payload.Confirm {
		errorJSON(w, http.StatusBadRequest, fmt.Errorf("требуется подтверждение восстановления"))
		return
	}
	dir, _, err := s.usableBackupDir()
	if err != nil {
		errorJSON(w, http.StatusConflict, err)
		return
	}
	name := r.PathValue("name")
	err = s.backup.StartRestore(dir, name, func(err error) {
		s.auditBackupResult("Данные восстановлены из резервной копии", "Не удалось восстановить данные из резервной копии", name, err)
	})
	if err != nil {
		errorJSON(w, backupErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusAccepted, s.backup.Status())
}

// DELETE /api/backups/{name}
func (s *Server) deleteBackup(w http.ResponseWriter, r *http.Request) {
	dir, _, err := s.usableBackupDir()
	if err != nil {
		errorJSON(w, http.StatusConflict, err)
		return
	}
	if err := s.backup.Delete(dir, r.PathValue("name")); err != nil {
		errorJSON(w, backupErrorStatus(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ------------------------------------------------------------------ settings

func within(path, parent string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absParent, absPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (s *Server) validateBackupDir(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("укажите полный путь к папке, например D:\\Backups")
	}
	clean := filepath.Clean(value)
	for _, protected := range []string{s.store.Root(), s.chatRoot} {
		if within(clean, protected) {
			return "", fmt.Errorf("папка резервных копий не должна находиться внутри хранилища или вложений чата")
		}
	}
	if err := os.MkdirAll(clean, 0o755); err != nil {
		return "", fmt.Errorf("не удалось создать папку: %w", err)
	}
	probe, err := os.CreateTemp(clean, ".write-test-*")
	if err != nil {
		return "", fmt.Errorf("в эту папку нельзя записывать: %w", err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return clean, nil
}

// GET /api/backups/settings
func (s *Server) getBackupSettingsAPI(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	cfg, err := s.getBackupSettings()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// PATCH /api/backups/settings
func (s *Server) updateBackupSettingsAPI(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var input struct {
		Dir           *string `json:"dir"`
		Keep          *int    `json:"keep"`
		IntervalHours *int    `json:"intervalHours"`
	}
	if err := decodeJSON(r, &input); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	cfg, err := s.getBackupSettings()
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	if input.Dir != nil && strings.TrimSpace(*input.Dir) != cfg.Dir {
		next, err := s.validateBackupDir(*input.Dir)
		if err != nil {
			errorJSON(w, http.StatusBadRequest, err)
			return
		}
		cfg.Dir = next
	}
	if input.Keep != nil {
		if *input.Keep < 1 || *input.Keep > maxBackupKeep {
			errorJSON(w, http.StatusBadRequest, fmt.Errorf("количество хранимых копий должно быть от 1 до %d", maxBackupKeep))
			return
		}
		cfg.Keep = *input.Keep
	}
	if input.IntervalHours != nil {
		if *input.IntervalHours < 0 || *input.IntervalHours > maxBackupIntervalH {
			errorJSON(w, http.StatusBadRequest, fmt.Errorf("интервал автоматического копирования должен быть от 0 до %d часов", maxBackupIntervalH))
			return
		}
		cfg.IntervalHours = *input.IntervalHours
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	for key, value := range map[string]string{
		"backup_dir":            cfg.Dir,
		"backup_keep":           strconv.Itoa(cfg.Keep),
		"backup_interval_hours": strconv.Itoa(cfg.IntervalHours),
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
	writeJSON(w, http.StatusOK, cfg)
}

// --------------------------------------------------- maintenance / scheduler

// withMaintenance rejects almost everything while a restore replaces the data.
func (s *Server) withMaintenance(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && s.backup.Restoring() && !allowedDuringRestore(r) {
			noStore(w)
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": "Идёт восстановление из резервной копии. Подождите завершения.",
				"code":  "restoring",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowedDuringRestore(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/health", "/api/auth/me", "/api/network", "/api/info", "/api/backups/status":
		return true
	}
	return false
}

// startBackupScheduler makes automatic backups when an interval is configured.
func (s *Server) startBackupScheduler() {
	go func() {
		time.Sleep(45 * time.Second)
		var lastAttempt time.Time
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			lastAttempt = s.runScheduledBackup(lastAttempt)
			<-ticker.C
		}
	}()
}

func (s *Server) runScheduledBackup(lastAttempt time.Time) time.Time {
	cfg, err := s.getBackupSettings()
	if err != nil || cfg.IntervalHours <= 0 {
		return lastAttempt
	}
	// Failed or skipped attempts are not retried every minute.
	if time.Since(lastAttempt) < 15*time.Minute {
		return lastAttempt
	}
	if s.backup.Restoring() || s.backup.Status().State == "running" {
		return lastAttempt
	}
	dir := s.backupDirFor(cfg)
	if cfg.Dir != "" {
		if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
			// Only look again in about an hour: 45 minutes ahead + 15 minute delay.
			s.auditSystem("warning", "Автоматическая копия пропущена: папка резервных копий недоступна", http.StatusServiceUnavailable)
			return time.Now().Add(45 * time.Minute)
		}
	}
	interval := time.Duration(cfg.IntervalHours) * time.Hour
	if newest, ok := backup.NewestModTime(dir); ok && time.Since(newest) < interval {
		return lastAttempt
	}
	err = s.backup.StartBackup(dir, backup.KindAuto, cfg.Keep, func(name string, err error) {
		s.auditBackupResult("Автоматическая резервная копия создана", "Не удалось создать автоматическую резервную копию", name, err)
	})
	if err != nil {
		return lastAttempt
	}
	return time.Now()
}
