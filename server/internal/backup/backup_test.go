package backup

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"personal-cloud/server/internal/database"
)

type fixture struct {
	svc        *Service
	db         *sql.DB
	storageDir string
	chatDir    string
	backupDir  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	f := &fixture{
		storageDir: filepath.Join(root, "storage"),
		chatDir:    filepath.Join(dataDir, "chat-attachments"),
		backupDir:  filepath.Join(dataDir, "backups"),
	}
	for _, dir := range []string{f.storageDir, f.chatDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "cloud.db")+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Initialize(db); err != nil {
		t.Fatal(err)
	}
	f.db = db
	f.svc = NewService(db, f.storageDir, f.chatDir, dataDir, "test")
	return f
}

func (f *fixture) write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (f *fixture) count(t *testing.T, query string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) setting(t *testing.T, key string) string {
	t.Helper()
	var value string
	if err := f.db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCreateVerifyRestoreRoundTrip(t *testing.T) {
	f := newFixture(t)
	f.write(t, filepath.Join(f.storageDir, "docs", "a.txt"), "hello")
	if err := os.MkdirAll(filepath.Join(f.storageDir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.write(t, filepath.Join(f.chatDir, "abc.bin"), "chat")
	f.exec(t, `INSERT INTO chat_messages (body, created_at) VALUES ('hi', '2026-01-01T00:00:00Z')`)
	f.exec(t, `INSERT INTO chat_attachments (message_id, name, storage_name, mime_type, size, created_at) VALUES (1, 'x.bin', 'abc.bin', 'application/octet-stream', 4, '2026-01-01T00:00:00Z')`)
	f.exec(t, `INSERT INTO devices (name, kind, token_hash, created_at, last_seen_at) VALUES ('Phone', 'paired', 'hash', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	f.exec(t, `UPDATE app_settings SET value = 'Before' WHERE key = 'cloud_name'`)

	name, err := f.svc.create(f.backupDir, KindManual)
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(f.backupDir, name)
	if err := f.svc.verify(full); err != nil {
		t.Fatalf("verify: %v", err)
	}

	items, err := List(f.backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Valid || items[0].StorageFiles != 1 || items[0].ChatFiles != 1 || items[0].FolderCount != 2 {
		t.Fatalf("unexpected list: %#v", items)
	}

	// The snapshot inside the archive must not carry any device tokens.
	zr, err := zip.OpenReader(full)
	if err != nil {
		t.Fatal(err)
	}
	var snapshotPath = filepath.Join(t.TempDir(), "snapshot.db")
	for _, file := range zr.File {
		if file.Name != dbEntry {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(snapshotPath, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = zr.Close()
	snap, err := sql.Open("sqlite", snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	var devices int
	if err := snap.QueryRow(`SELECT COUNT(*) FROM devices`).Scan(&devices); err != nil {
		t.Fatal(err)
	}
	_ = snap.Close()
	if devices != 0 {
		t.Fatalf("snapshot contains %d devices, want 0", devices)
	}

	// Change everything, then restore.
	f.write(t, filepath.Join(f.storageDir, "docs", "a.txt"), "changed")
	f.write(t, filepath.Join(f.storageDir, "new.txt"), "new")
	if err := os.Remove(filepath.Join(f.chatDir, "abc.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.storageDir, "empty")); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `DELETE FROM chat_messages`)
	f.exec(t, `UPDATE app_settings SET value = 'After' WHERE key = 'cloud_name'`)
	f.exec(t, `INSERT INTO app_settings (key, value) VALUES ('backup_keep', '3')`)

	if err := f.svc.restore(f.backupDir, full); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if got := readFile(t, filepath.Join(f.storageDir, "docs", "a.txt")); got != "hello" {
		t.Fatalf("a.txt = %q, want hello", got)
	}
	if _, err := os.Stat(filepath.Join(f.storageDir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should be gone after restore, stat err = %v", err)
	}
	if info, err := os.Stat(filepath.Join(f.storageDir, "empty")); err != nil || !info.IsDir() {
		t.Fatalf("empty folder was not restored: %v", err)
	}
	if got := readFile(t, filepath.Join(f.chatDir, "abc.bin")); got != "chat" {
		t.Fatalf("chat attachment = %q, want chat", got)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM chat_messages WHERE body = 'hi'`); n != 1 {
		t.Fatalf("chat message count = %d, want 1", n)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM chat_attachments`); n != 1 {
		t.Fatalf("chat attachment rows = %d, want 1", n)
	}
	if got := f.setting(t, "cloud_name"); got != "Before" {
		t.Fatalf("cloud_name = %q, want Before", got)
	}
	if got := f.setting(t, "backup_keep"); got != "3" {
		t.Fatalf("backup_keep must survive a restore, got %q", got)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM devices`); n != 1 {
		t.Fatalf("devices must be untouched by a restore, got %d", n)
	}

	items, err = List(f.backupDir)
	if err != nil {
		t.Fatal(err)
	}
	preRestore := 0
	for _, item := range items {
		if item.Kind == KindPreRestore {
			preRestore++
		}
	}
	if preRestore != 1 {
		t.Fatalf("expected one safety backup, got %d (%#v)", preRestore, items)
	}
	leftovers, _ := filepath.Glob(f.storageDir + ".*")
	if len(leftovers) != 0 {
		t.Fatalf("temporary folders left behind: %v", leftovers)
	}
}

// tamper copies a backup but replaces the content of one entry.
func tamper(t *testing.T, src, dst, entry, content string) {
	t.Helper()
	zr, err := zip.OpenReader(src)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	if err := zw.SetComment(zr.Comment); err != nil {
		t.Fatal(err)
	}
	for _, file := range zr.File {
		writer, err := zw.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == entry {
			if _, err := writer.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(writer, reader); err != nil {
			t.Fatal(err)
		}
		_ = reader.Close()
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptedBackupIsRejectedAndLiveDataKept(t *testing.T) {
	f := newFixture(t)
	f.write(t, filepath.Join(f.storageDir, "a.txt"), "hello")
	name, err := f.svc.create(f.backupDir, KindManual)
	if err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(t.TempDir(), "broken.zip")
	tamper(t, filepath.Join(f.backupDir, name), broken, "storage/a.txt", "HELLO")

	err = f.svc.verify(broken)
	if err == nil || !strings.Contains(err.Error(), "повреждён") {
		t.Fatalf("verify should report corruption, got %v", err)
	}

	f.write(t, filepath.Join(f.storageDir, "a.txt"), "live")
	if err := f.svc.restore(f.backupDir, broken); err == nil {
		t.Fatal("restore of a corrupted backup must fail")
	}
	if got := readFile(t, filepath.Join(f.storageDir, "a.txt")); got != "live" {
		t.Fatalf("live data changed by a failed restore: %q", got)
	}
	leftovers, _ := filepath.Glob(f.storageDir + ".*")
	if len(leftovers) != 0 {
		t.Fatalf("temporary folders left behind: %v", leftovers)
	}
}

func TestSafeRel(t *testing.T) {
	good := []string{"a.txt", "dir/file.txt", "папка/файл.txt"}
	bad := []string{"", "/abs", "../x", "a/../b", "a//b", "a/./b", "a/..", "."}
	for _, value := range good {
		if _, err := safeRel(value); err != nil {
			t.Errorf("safeRel(%q) rejected: %v", value, err)
		}
	}
	for _, value := range bad {
		if _, err := safeRel(value); err == nil {
			t.Errorf("safeRel(%q) accepted", value)
		}
	}
}

func TestResolveRejectsUnsafeNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"../../etc/passwd", "backup.zip", "personal-cloud-backup-20260101-120000-manual.zip/../x"} {
		if _, err := Resolve(dir, name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Resolve(%q) = %v, want ErrInvalidName", name, err)
		}
	}
	if _, err := Resolve(dir, "personal-cloud-backup-20260101-120000-manual.zip"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing backup: got %v, want ErrNotFound", err)
	}
}

func TestPruneKeepsNewestAutoOnly(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(f.backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	names := []string{
		"personal-cloud-backup-20260101-120000-auto.zip",
		"personal-cloud-backup-20260102-120000-auto.zip",
		"personal-cloud-backup-20260103-120000-auto.zip",
		"personal-cloud-backup-20260101-120000-manual.zip",
	}
	for _, name := range names {
		f.write(t, filepath.Join(f.backupDir, name), "x")
	}
	f.svc.prune(f.backupDir, KindAuto, 2)
	for name, want := range map[string]bool{
		names[0]: false, names[1]: true, names[2]: true, names[3]: true,
	} {
		_, err := os.Stat(filepath.Join(f.backupDir, name))
		if (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", name, err == nil, want)
		}
	}
}

func TestImport(t *testing.T) {
	f := newFixture(t)
	f.write(t, filepath.Join(f.storageDir, "a.txt"), "hello")
	name, err := f.svc.create(f.backupDir, KindManual)
	if err != nil {
		t.Fatal(err)
	}
	data := readFile(t, filepath.Join(f.backupDir, name))

	target := filepath.Join(t.TempDir(), "other")
	info, err := f.svc.Import(target, strings.NewReader(data))
	if err != nil {
		t.Fatalf("import of a valid backup failed: %v", err)
	}
	if info.Kind != KindImported || !info.Valid {
		t.Fatalf("unexpected imported info: %#v", info)
	}

	if _, err := f.svc.Import(target, bytes.NewReader([]byte("not a zip"))); !errors.Is(err, ErrInvalidBackup) {
		t.Fatalf("import of garbage = %v, want ErrInvalidBackup", err)
	}
	entries, _ := os.ReadDir(target)
	if len(entries) != 1 {
		t.Fatalf("rejected import left files behind: %v", entries)
	}
}
