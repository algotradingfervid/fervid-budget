package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type BackupRow struct{ Name, CreatedAt, Status, RestoreCommand string }

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

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
