package app

// The backups screen and the "Back up now" button.

import (
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// backups lists the backup folders (named "backup-…") in the backup
// directory, newest first, with where the database and attachments live and
// whom to contact for a restore.
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
	slices.Sort(names)
	slices.Reverse(names)
	a.render(w, r, "backups", PageData{Title: "Backups", Backups: names, BackupRows: a.backupRows(names), BackupLocation: a.cfg.BackupDir, BackupDBPath: a.cfg.DBPath, BackupAttachmentPath: a.cfg.AttachmentDir, RecoveryName: a.cfg.AdminName, RecoveryContact: a.cfg.AdminEmail})
}

// backupCreate makes a backup of the database and attachments, then deletes
// backups older than the configured retention (a failed clean-up is only
// logged). The backup is audited.
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
