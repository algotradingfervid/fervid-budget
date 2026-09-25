package store

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func dailyBackupFixture(t *testing.T) (DailyBackup, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, "fervid.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	var logs bytes.Buffer
	return DailyBackup{
		DBPath:        dbPath,
		AttachmentDir: filepath.Join(root, "attachments"),
		BackupDir:     filepath.Join(root, "backups"),
		Hour:          2,
		KeepDays:      30,
		KeepMonths:    12,
		Log:           slog.New(slog.NewTextHandler(&logs, nil)),
	}, &logs
}

func backupNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// backup-2: §10 promises an automated daily backup. The job makes one backup
// per day once the configured hour has passed, and a second check the same day
// — another tick, or a restart — does nothing.
func TestDailyBackupRunsOncePerDayAfterTheConfiguredHour(t *testing.T) {
	ctx := context.Background()
	job, logs := dailyBackupFixture(t)
	day := func(d, h, m int) time.Time { return time.Date(2026, 9, d, h, m, 0, 0, time.Local) }

	if _, made, err := job.RunIfDue(ctx, day(25, 1, 59)); err != nil || made {
		t.Fatalf("before the hour: made=%v err=%v", made, err)
	}
	if got := backupNames(t, job.BackupDir); len(got) != 0 {
		t.Fatalf("backup made before the configured hour: %v", got)
	}
	info, made, err := job.RunIfDue(ctx, day(25, 2, 0))
	if err != nil || !made {
		t.Fatalf("at the hour: made=%v err=%v", made, err)
	}
	if filepath.Base(info.Path) != "backup-20260925-020000" {
		t.Fatalf("daily backup path = %s", info.Path)
	}
	for _, at := range []time.Time{day(25, 2, 5), day(25, 23, 59)} {
		if _, made, err := job.RunIfDue(ctx, at); err != nil || made {
			t.Fatalf("second check the same day at %s: made=%v err=%v", at, made, err)
		}
	}
	if _, made, err := job.RunIfDue(ctx, day(26, 3, 0)); err != nil || !made {
		t.Fatalf("next day: made=%v err=%v", made, err)
	}
	if got := backupNames(t, job.BackupDir); strings.Join(got, ",") != "backup-20260925-020000,backup-20260926-030000" {
		t.Fatalf("backups = %v", got)
	}
	if !strings.Contains(logs.String(), "daily backup created") {
		t.Fatalf("daily backup was not logged: %s", logs.String())
	}
}

// A backup an admin made earlier the same day but before the hour does not
// stand in for the daily one; one made after the hour does.
func TestDailyBackupCountsOnlyBackupsMadeAfterTheHourToday(t *testing.T) {
	ctx := context.Background()
	job, _ := dailyBackupFixture(t)
	if _, err := CreateBackup(ctx, BackupOptions{DBPath: job.DBPath, AttachmentDir: job.AttachmentDir, BackupDir: job.BackupDir,
		Now: func() time.Time { return time.Date(2026, 9, 25, 1, 0, 0, 0, time.Local) }}); err != nil {
		t.Fatal(err)
	}
	if _, made, err := job.RunIfDue(ctx, time.Date(2026, 9, 25, 9, 0, 0, 0, time.Local)); err != nil || !made {
		t.Fatalf("a pre-hour manual backup suppressed the daily one: made=%v err=%v", made, err)
	}
	if _, err := CreateBackup(ctx, BackupOptions{DBPath: job.DBPath, AttachmentDir: job.AttachmentDir, BackupDir: job.BackupDir,
		Now: func() time.Time { return time.Date(2026, 9, 26, 4, 0, 0, 0, time.Local) }}); err != nil {
		t.Fatal(err)
	}
	if _, made, err := job.RunIfDue(ctx, time.Date(2026, 9, 26, 9, 0, 0, 0, time.Local)); err != nil || made {
		t.Fatalf("a post-hour manual backup should count as the day's backup: made=%v err=%v", made, err)
	}
}

// The daily run prunes with the same retention as a manual backup.
func TestDailyBackupPrunesWithTheRetentionRules(t *testing.T) {
	ctx := context.Background()
	job, _ := dailyBackupFixture(t)
	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.Local)
	mkBackupDir(t, job.BackupDir, "backup-20260101-020000", now)
	mkBackupDir(t, job.BackupDir, "backup-20260102-020000", now)
	if _, made, err := job.RunIfDue(ctx, now); err != nil || !made {
		t.Fatalf("made=%v err=%v", made, err)
	}
	if got := backupNames(t, job.BackupDir); strings.Join(got, ",") != "backup-20260102-020000,backup-20260925-030000" {
		t.Fatalf("after the daily prune: %v", got)
	}
}

// The scheduler checks once on start, like the reminder scheduler, so a server
// restarted after the hour catches up, and it stops when ctx is cancelled.
func TestDailyBackupSchedulerRunsOnStartAndStopsOnCancel(t *testing.T) {
	job, _ := dailyBackupFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		job.Scheduler(ctx, time.Hour, func() time.Time { return time.Date(2026, 9, 25, 8, 0, 0, 0, time.Local) })
	}()
	deadline := time.Now().Add(5 * time.Second)
	for strings.Join(backupNames(t, job.BackupDir), ",") != "backup-20260925-080000" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop on cancel")
	}
	if got := backupNames(t, job.BackupDir); strings.Join(got, ",") != "backup-20260925-080000" {
		t.Fatalf("scheduler start-up backups = %v", got)
	}
}
