package store

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// backup-3: §10 asks for at least 30 daily and 12 monthly backups. Pruning used
// to apply one 30-day cutoff, so every monthly backup was deleted with the rest.
func TestPruneBackupsKeepsThirtyDaysAndTheNewestBackupOfTwelveMonths(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.Local)
	for day := 0; day < 400; day++ {
		at := time.Date(2026, 9, 25, 2, 0, 0, 0, time.Local).AddDate(0, 0, -day)
		mkBackupDir(t, dir, "backup-"+at.Format("20060102-150405"), at)
	}
	// A second backup on the last day of a month: only the newer one of that
	// day is that month's backup.
	mkBackupDir(t, dir, "backup-20260331-230000", time.Date(2026, 3, 31, 23, 0, 0, 0, time.Local))
	// Folders that are not backups are never touched, whatever their age.
	mkBackupDir(t, dir, "notes", now.AddDate(-3, 0, 0))

	if err := PruneBackups(dir, 30, 12, now); err != nil {
		t.Fatalf("PruneBackups: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	sort.Strings(got)

	want := []string{"notes"}
	for day := 0; day < 30; day++ { // 2026-08-27 .. 2026-09-25
		want = append(want, "backup-"+time.Date(2026, 9, 25, 2, 0, 0, 0, time.Local).AddDate(0, 0, -day).Format("20060102-150405"))
	}
	// The newest backup of each of the twelve months before September 2026.
	// August's (08-31) is already inside the 30 days.
	for _, name := range []string{
		"backup-20250930-020000", "backup-20251031-020000", "backup-20251130-020000", "backup-20251231-020000",
		"backup-20260131-020000", "backup-20260228-020000", "backup-20260331-230000", "backup-20260430-020000",
		"backup-20260531-020000", "backup-20260630-020000", "backup-20260731-020000",
	} {
		want = append(want, name)
	}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("kept %d folders, want %d:\n got %v\nwant %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kept folders differ at %d: got %q want %q\n got %v", i, got[i], want[i], got)
		}
	}
}

// The folder name is the backup's creation time. A backup folder copied to a
// new disk gets a fresh mtime, and must still age out by its name; a folder
// whose name carries no time falls back to its mtime.
func TestPruneBackupsAgesByTheTimestampInTheName(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.Local)
	mkBackupDir(t, dir, "backup-20250101-020000", now)                 // old name, fresh mtime
	mkBackupDir(t, dir, "backup-20260920-020000-01", now)              // uniqueBackupPath suffix
	mkBackupDir(t, dir, "backup-manual-copy", now.AddDate(0, -6, 0))   // no time in name, old mtime
	mkBackupDir(t, dir, "backup-manual-recent", now.AddDate(0, 0, -1)) // no time in name, recent mtime
	if err := PruneBackups(dir, 30, 12, now); err != nil {
		t.Fatal(err)
	}
	for name, kept := range map[string]bool{
		"backup-20250101-020000":    false,
		"backup-20260920-020000-01": true,
		"backup-manual-copy":        false,
		"backup-manual-recent":      true,
	} {
		_, err := os.Stat(filepath.Join(dir, name))
		if kept && err != nil {
			t.Errorf("%s was pruned, want kept: %v", name, err)
		}
		if !kept && !os.IsNotExist(err) {
			t.Errorf("%s was kept, want pruned (err=%v)", name, err)
		}
	}
}

func mkBackupDir(t *testing.T, dir, name string, mtime time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}
