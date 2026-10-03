package app

// Rows for the Backups screen: each backup folder's creation time, whether
// its files look complete, and the exact command that restores it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupRow is one backup folder as the Backups screen lists it.
type BackupRow struct{ Name, CreatedAt, Status, RestoreCommand string }

// shellQuote wraps value in single quotes for a POSIX shell, escaping any
// single quotes inside it.
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// backupRows describes each named backup folder. The time comes from its
// manifest.json; the status says "Files present" only when that manifest is
// valid and both fervid.db and the attachments folder exist (the backup has
// not been test-restored). RestoreCommand is a copy-paste shell command.
func (a *App) backupRows(names []string) []BackupRow {
	rows := []BackupRow{}
	for _, name := range names {
		row := BackupRow{Name: name, CreatedAt: "Unknown — inspect manifest", Status: "Needs inspection"}
		manifestValid := false
		path := filepath.Join(a.cfg.BackupDir, name)
		var manifest struct {
			CreatedAt string `json:"created_at"`
		}
		if contents, err := os.ReadFile(filepath.Join(path, "manifest.json")); err == nil && json.Unmarshal(contents, &manifest) == nil {
			if created, err := time.Parse(time.RFC3339, manifest.CreatedAt); err == nil {
				row.CreatedAt = created.Local().Format("02 Jan 2006, 15:04 MST")
				manifestValid = true
			}
		}
		db, de := os.Stat(filepath.Join(path, "fervid.db"))
		at, ae := os.Stat(filepath.Join(path, "attachments"))
		if manifestValid && de == nil && ae == nil && !db.IsDir() && at.IsDir() {
			row.Status = "Files present · not restore-tested"
		}
		row.RestoreCommand = "FERVID_DB=" + shellQuote(a.cfg.DBPath) + " FERVID_ATTACHMENT_DIR=" + shellQuote(a.cfg.AttachmentDir) + " go run ./cmd/server --restore " + shellQuote(path)
		rows = append(rows, row)
	}
	return rows
}
