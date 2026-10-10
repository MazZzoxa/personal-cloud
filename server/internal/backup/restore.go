package backup

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"personal-cloud/server/internal/systemdisk"
)

// Settings that describe where and how backups are made belong to this
// installation, not to the data set, so a restore never overwrites them.
const keepSettingsClause = `key NOT IN ('backup_dir', 'backup_keep', 'backup_interval_hours')`

// restore replaces the storage folder, chat attachments and database content
// with the contents of the backup at full. dir is the backup folder, used for
// the safety copy that is made before anything is touched.
//
// Order of work, chosen so that a failure leaves the live data intact:
//  1. extract everything into temporary folders next to the live ones and
//     verify every checksum (nothing live is touched yet);
//  2. create a safety backup of the current state;
//  3. swap the folders (renames, reversible);
//  4. replace the database content in one transaction; if that fails the
//     folder swap is undone.
func (s *Service) restore(dir, full string) error {
	s.setPhase("Чтение архива", 0)
	zr, manifest, err := openBackup(full)
	if err != nil {
		return err
	}
	defer zr.Close()
	if manifest.Database.Path != dbEntry {
		return ErrInvalidBackup
	}

	stamp := time.Now().Format("20060102-150405")
	storageTmp := s.storageDir + ".restoring-" + stamp
	chatTmp := s.chatDir + ".restoring-" + stamp
	storageOld := s.storageDir + ".replaced-" + stamp
	chatOld := s.chatDir + ".replaced-" + stamp
	dbTmp := filepath.Join(s.dataDir, ".restore-"+stamp+".db")

	committed := false
	defer func() {
		_ = os.Remove(dbTmp)
		if !committed {
			_ = os.RemoveAll(storageTmp)
			_ = os.RemoveAll(chatTmp)
		}
	}()

	if free, ok := freeSpace(filepath.Dir(s.storageDir)); ok && free < uint64(manifest.DataBytes)+minFreeMargin {
		return fmt.Errorf("недостаточно свободного места для восстановления: нужно около %s, свободно %s",
			formatBytes(uint64(manifest.DataBytes)+minFreeMargin), formatBytes(free))
	}

	if err := s.extract(zr, manifest, storageTmp, chatTmp, dbTmp); err != nil {
		return err
	}

	s.setPhase("Страховочная копия текущих данных", 0)
	if _, err := s.create(dir, KindPreRestore); err != nil {
		return fmt.Errorf("не удалось создать страховочную копию: %w", err)
	}

	s.setPhase("Замена файлов", 0)
	undoStorage, err := swapDir(s.storageDir, storageTmp, storageOld)
	if err != nil {
		return fmt.Errorf("не удалось заменить папку хранилища (закройте активные загрузки и повторите): %w", err)
	}
	undoChat, err := swapDir(s.chatDir, chatTmp, chatOld)
	if err != nil {
		undoStorage()
		return fmt.Errorf("не удалось заменить папку вложений чата: %w", err)
	}

	s.setPhase("Восстановление базы данных", 0)
	if err := s.restoreDatabase(dbTmp); err != nil {
		undoChat()
		undoStorage()
		return err
	}
	committed = true

	s.setPhase("Очистка", 0)
	_ = os.RemoveAll(storageOld)
	_ = os.RemoveAll(chatOld)
	return nil
}

func freeSpace(path string) (uint64, bool) {
	total, free, err := systemdisk.Space(path)
	if err != nil || total == 0 {
		return 0, false
	}
	return free, true
}

// swapDir puts replacement in place of current, keeping the previous folder as old.
// The returned function reverts the swap.
func swapDir(current, replacement, old string) (func(), error) {
	if err := os.Rename(current, old); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// Nothing to replace: just move the new folder into place.
		if err := os.Rename(replacement, current); err != nil {
			return nil, err
		}
		return func() { _ = os.Rename(current, replacement) }, nil
	}
	if err := os.Rename(replacement, current); err != nil {
		_ = os.Rename(old, current)
		return nil, err
	}
	return func() {
		_ = os.Rename(current, replacement)
		_ = os.Rename(old, current)
	}, nil
}

// extract unpacks and verifies the archive into temporary locations.
func (s *Service) extract(zr *zip.ReadCloser, manifest Manifest, storageTmp, chatTmp, dbTmp string) error {
	index := indexEntries(zr)

	total := manifest.Database.Size
	for _, item := range manifest.Files {
		total += item.Size
	}
	s.setPhase("Распаковка и проверка файлов", total)

	for _, base := range []string{storageTmp, chatTmp} {
		_ = os.RemoveAll(base)
		if err := os.MkdirAll(base, 0o755); err != nil {
			return fmt.Errorf("не удалось создать временную папку: %w", err)
		}
	}
	for _, folder := range manifest.Folders {
		rel, err := safeRel(folder)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(storageTmp, filepath.FromSlash(rel)), 0o755); err != nil {
			return err
		}
	}

	entry, ok := index[dbEntry]
	if !ok {
		return fmt.Errorf("в архиве нет базы данных")
	}
	if err := s.extractOne(entry, manifest.Database, dbTmp); err != nil {
		return err
	}

	for _, item := range manifest.Files {
		prefix, rel, err := splitItemPath(item.Path)
		if err != nil {
			return err
		}
		entry, ok := index[item.Path]
		if !ok {
			return fmt.Errorf("в архиве нет файла: %s", item.Path)
		}
		base := storageTmp
		if prefix == chatPrefix {
			base = chatTmp
		}
		target := filepath.Join(base, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := s.extractOne(entry, item, target); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) extractOne(entry *zip.File, item Item, target string) error {
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("%s: %w", item.Path, err)
	}
	copyErr := s.copyChecked(entry, item, out)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if !entry.Modified.IsZero() {
		_ = os.Chtimes(target, entry.Modified, entry.Modified)
	}
	return nil
}

// restoreDatabase copies the data tables from the snapshot into the live
// database in a single transaction. Devices, pairing codes and the audit log
// are deliberately left alone: access rights stay as they are now and the
// restore itself is recorded in the journal.
func (s *Service) restoreDatabase(snapshot string) error {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("подключение к базе данных: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS snap`, snapshot); err != nil {
		return fmt.Errorf("не удалось открыть базу данных из копии: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), `DETACH DATABASE snap`) }()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("начало транзакции: %w", err)
	}
	defer tx.Rollback()

	statements := []string{
		`DELETE FROM chat_attachments`,
		`DELETE FROM chat_messages`,
		`DELETE FROM files`,
		`INSERT INTO files (id, path, name, kind, size, mime_type, created_at, updated_at)
		 SELECT id, path, name, kind, size, mime_type, created_at, updated_at FROM snap.files`,
		`INSERT INTO chat_messages (id, body, created_at)
		 SELECT id, body, created_at FROM snap.chat_messages`,
		`INSERT INTO chat_attachments (id, message_id, name, storage_name, mime_type, size, created_at)
		 SELECT id, message_id, name, storage_name, mime_type, size, created_at FROM snap.chat_attachments`,
		`DELETE FROM app_settings WHERE ` + keepSettingsClause,
		`INSERT OR REPLACE INTO app_settings (key, value)
		 SELECT key, value FROM snap.app_settings WHERE ` + keepSettingsClause,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("восстановление базы данных: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("завершение восстановления базы данных: %w", err)
	}
	return nil
}
