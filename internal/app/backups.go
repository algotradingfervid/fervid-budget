package app

// The backups screen and creating a backup.

import (
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

func (a *App) backups(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(a.cfg.BackupDir)
	if err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "Backups could not be listed.", err)
		return
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "backup-") {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	a.render(w, r, "backups", PageData{Title: "Backups", Backups: names, BackupRows: a.backupRows(names), BackupLocation: a.cfg.BackupDir, BackupDBPath: a.cfg.DBPath, BackupAttachmentPath: a.cfg.AttachmentDir, RecoveryName: a.cfg.AdminName, RecoveryContact: a.cfg.AdminEmail})
}

func (a *App) backupCreate(w http.ResponseWriter, r *http.Request) {
	info, err := store.Backup(r.Context(), a.cfg.DBPath, a.cfg.AttachmentDir, a.cfg.BackupDir)
	if err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The backup could not be created.", err)
		return
	}
	if err := store.PruneBackups(a.cfg.BackupDir, a.cfg.BackupKeepDays, a.cfg.BackupKeepMonths, time.Now()); err != nil {
		a.log.WarnContext(r.Context(), "backup pruning failed", "request_id", requestID(r), "error", err)
	}
	u := auth.CurrentUser(r)
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "create", EntityType: "backup", Summary: "Created backup " + info.Path})
	http.Redirect(w, r, "/backups", http.StatusSeeOther)
}
