package store

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"
)

// DailyBackup is the automated daily backup of §10. The server runs it in
// serve mode beside the reminder scheduler, in its own goroutine: the backup
// is a VACUUM INTO read under WAL, so requests keep reading and writing while
// it runs.
type DailyBackup struct {
	DBPath        string
	AttachmentDir string
	BackupDir     string
	// Hour is the local hour (0-23) at or after which the day's backup is due.
	Hour       int
	KeepDays   int
	KeepMonths int
	Log        *slog.Logger
}

// RunIfDue makes today's backup once now has reached today's Hour, unless a
// backup stamped at or after that moment already exists — an earlier tick, the
// run before a restart, or an admin's Create Backup. That makes it safe to call
// as often as the scheduler likes: at most one automated backup a day.
func (d DailyBackup) RunIfDue(ctx context.Context, now time.Time) (BackupInfo, bool, error) {
	due := time.Date(now.Year(), now.Month(), now.Day(), d.Hour, 0, 0, 0, now.Location())
	if now.Before(due) {
		return BackupInfo{}, false, nil
	}
	made, err := d.backupSince(due)
	if err != nil || made {
		return BackupInfo{}, false, err
	}
	info, err := CreateBackup(ctx, BackupOptions{
		DBPath: d.DBPath, AttachmentDir: d.AttachmentDir, BackupDir: d.BackupDir,
		Now: func() time.Time { return now },
	})
	if err != nil {
		return BackupInfo{}, false, err
	}
	d.logger().InfoContext(ctx, "daily backup created", "backup_path", info.Path)
	if err := PruneBackups(d.BackupDir, d.KeepDays, d.KeepMonths, now); err != nil {
		d.logger().WarnContext(ctx, "daily backup created but pruning failed", "backup_path", info.Path, "error", err)
	}
	return info, true, nil
}

// backupSince reports whether a finished backup stamped at or after t exists.
func (d DailyBackup) backupSince(t time.Time) (bool, error) {
	entries, err := os.ReadDir(d.BackupDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !strings.HasPrefix(name, "backup-") || strings.HasSuffix(name, ".tmp") {
			continue
		}
		if at, ok := backupTimeFromName(name, t.Location()); ok && !at.Before(t) {
			return true, nil
		}
	}
	return false, nil
}

// Scheduler checks once immediately, so a server started after the hour
// catches up on the day's backup, then on every interval tick, and returns
// when ctx is cancelled. A failed backup is logged and retried on the next
// tick. now is injected so the loop is deterministic under test.
func (d DailyBackup) Scheduler(ctx context.Context, interval time.Duration, now func() time.Time) {
	d.runLogged(ctx, now())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runLogged(ctx, now())
		}
	}
}

func (d DailyBackup) runLogged(ctx context.Context, now time.Time) {
	if _, _, err := d.RunIfDue(ctx, now); err != nil && ctx.Err() == nil {
		d.logger().ErrorContext(ctx, "daily backup failed", "error", err)
	}
}

func (d DailyBackup) logger() *slog.Logger {
	if d.Log != nil {
		return d.Log
	}
	return slog.Default()
}
