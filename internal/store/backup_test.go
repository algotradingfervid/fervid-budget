package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateBackupUsesConsistentSQLiteSnapshotAndCopiesAttachments(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "live", "fervid.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.DB().Exec(`PRAGMA journal_mode=WAL; CREATE TABLE snapshot_check (value TEXT); INSERT INTO snapshot_check(value) VALUES ('committed');`); err != nil {
		t.Fatalf("seed WAL database: %v", err)
	}
	attachments := filepath.Join(root, "attachments")
	if err := os.MkdirAll(filepath.Join(attachments, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attachments, "nested", "receipt.txt"), []byte("receipt bytes"), 0640); err != nil {
		t.Fatal(err)
	}

	createdAt := time.Date(2026, 7, 23, 10, 11, 12, 0, time.UTC)
	info, err := CreateBackup(ctx, BackupOptions{
		DBPath: dbPath, AttachmentDir: attachments, BackupDir: filepath.Join(root, "backups"), Now: func() time.Time { return createdAt },
	})
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if got, want := filepath.Base(info.Path), "backup-20260723-101112"; got != want {
		t.Fatalf("backup directory = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(info.Path, "manifest.json")); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	gotAttachment, err := os.ReadFile(filepath.Join(info.AttachmentDir, "nested", "receipt.txt"))
	if err != nil || string(gotAttachment) != "receipt bytes" {
		t.Fatalf("backup attachment = %q, %v", gotAttachment, err)
	}

	backupDB, err := sql.Open("sqlite", info.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer backupDB.Close()
	var integrity, value string
	if err := backupDB.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("backup integrity = %q, %v", integrity, err)
	}
	if err := backupDB.QueryRow(`SELECT value FROM snapshot_check`).Scan(&value); err != nil || value != "committed" {
		t.Fatalf("snapshot value = %q, %v", value, err)
	}
}

func TestCreateBackupCleansTemporaryDirectoryOnFailureAndCancellation(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "live.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(root, "backups")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = CreateBackup(ctx, BackupOptions{DBPath: dbPath, BackupDir: backupDir, Now: func() time.Time { return time.Date(2026, 7, 23, 1, 2, 3, 0, time.UTC) }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled backup error = %v", err)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") || strings.HasPrefix(entry.Name(), "backup-") {
			t.Fatalf("partial backup left behind: %s", entry.Name())
		}
	}
}

func TestRestoreBackupStagesAndReplacesDatabaseAndAttachments(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	sourceDB := filepath.Join(root, "source.db")
	source, err := Open(sourceDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.DB().Exec(`CREATE TABLE restored_check (value TEXT); INSERT INTO restored_check(value) VALUES ('from backup');`); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	sourceAttachments := filepath.Join(root, "source-attachments")
	if err := os.MkdirAll(sourceAttachments, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceAttachments, "proof.txt"), []byte("backup proof"), 0644); err != nil {
		t.Fatal(err)
	}
	info, err := CreateBackup(ctx, BackupOptions{DBPath: sourceDB, AttachmentDir: sourceAttachments, BackupDir: filepath.Join(root, "backups")})
	if err != nil {
		t.Fatal(err)
	}

	liveDB := filepath.Join(root, "live", "fervid.db")
	live, err := Open(liveDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.DB().Exec(`CREATE TABLE old_check (value TEXT); INSERT INTO old_check(value) VALUES ('old');`); err != nil {
		t.Fatal(err)
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	liveAttachments := filepath.Join(root, "live-attachments")
	if err := os.MkdirAll(liveAttachments, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(liveAttachments, "old.txt"), []byte("old proof"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := RestoreBackup(ctx, RestoreOptions{BackupPath: info.Path, DBPath: liveDB, AttachmentDir: liveAttachments}); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	restored, err := sql.Open("sqlite", liveDB)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var value string
	if err := restored.QueryRow(`SELECT value FROM restored_check`).Scan(&value); err != nil || value != "from backup" {
		t.Fatalf("restored database = %q, %v", value, err)
	}
	var oldTable int
	if err := restored.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='old_check'`).Scan(&oldTable); err != nil || oldTable != 0 {
		t.Fatalf("old database survived restore: %d, %v", oldTable, err)
	}
	proof, err := os.ReadFile(filepath.Join(liveAttachments, "proof.txt"))
	if err != nil || string(proof) != "backup proof" {
		t.Fatalf("restored attachment = %q, %v", proof, err)
	}
	if _, err := os.Stat(filepath.Join(liveAttachments, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old attachment should be replaced, err=%v", err)
	}
}

func TestRestoreBackupFailureLeavesLivePathsUntouched(t *testing.T) {
	root := t.TempDir()
	liveDB := filepath.Join(root, "live.db")
	if err := os.WriteFile(liveDB, []byte("live database marker"), 0600); err != nil {
		t.Fatal(err)
	}
	liveAttachments := filepath.Join(root, "attachments")
	if err := os.MkdirAll(liveAttachments, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(liveAttachments, "live.txt"), []byte("live attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(root, "broken-backup")
	if err := os.MkdirAll(backupPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := RestoreBackup(context.Background(), RestoreOptions{BackupPath: backupPath, DBPath: liveDB, AttachmentDir: liveAttachments}); err == nil {
		t.Fatal("RestoreBackup succeeded with missing backup database")
	}
	gotDB, err := os.ReadFile(liveDB)
	if err != nil || string(gotDB) != "live database marker" {
		t.Fatalf("live DB changed on failed restore: %q, %v", gotDB, err)
	}
	gotAttachment, err := os.ReadFile(filepath.Join(liveAttachments, "live.txt"))
	if err != nil || string(gotAttachment) != "live attachment" {
		t.Fatalf("live attachment changed on failed restore: %q, %v", gotAttachment, err)
	}
}
