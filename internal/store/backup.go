// backup.go creates, prunes and restores backups.
//
// A backup is a folder backup-YYYYMMDD-HHMMSS (with a -NN suffix if that name
// is taken) holding fervid.db, a consistent copy of the database made with
// VACUUM INTO; attachments/, a copy of the attachment folder; and
// manifest.json, which records where the attachments originally lived. It is
// built in a ".tmp" folder and renamed when complete, so a crash never leaves
// a backup that looks finished. Every file and folder is created owner-only.

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	backupDBName       = "fervid.db"       // the database file inside a backup
	backupAttachDir    = "attachments"     // the attachment folder inside a backup
	backupManifestFile = "manifest.json"   // the manifest inside a backup
	backupStampLayout  = "20060102-150405" // the time in a backup-<stamp> folder name
)

// BackupOptions says what to back up and where. Now overrides the clock that
// names the backup; nil means time.Now.
type BackupOptions struct {
	DBPath        string
	AttachmentDir string
	BackupDir     string
	Now           func() time.Time
}

// BackupInfo describes a finished backup: its folder, the database and
// attachment copies inside it, and the time it was stamped with.
type BackupInfo struct {
	Path          string
	DBPath        string
	AttachmentDir string
	CreatedAt     time.Time
}

// RestoreOptions names the backup folder to restore and the live database
// file and attachment folder it replaces.
type RestoreOptions struct {
	BackupPath    string
	DBPath        string
	AttachmentDir string
}

// Backup is CreateBackup with positional arguments and the real clock.
func Backup(ctx context.Context, dbPath, attachmentDir, backupDir string) (BackupInfo, error) {
	return CreateBackup(ctx, BackupOptions{
		DBPath:        dbPath,
		AttachmentDir: attachmentDir,
		BackupDir:     backupDir,
	})
}

// CreateBackup writes a new backup folder under opts.BackupDir and returns its
// paths. A missing attachment folder is backed up as an empty one. On any
// failure the half-built ".tmp" folder is removed.
func CreateBackup(ctx context.Context, opts BackupOptions) (BackupInfo, error) {
	if opts.DBPath == "" || opts.BackupDir == "" {
		return BackupInfo{}, fmt.Errorf("%w: database path and backup directory are required", ErrValidation)
	}
	if _, err := os.Stat(opts.DBPath); err != nil {
		return BackupInfo{}, err
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	createdAt := now()
	if err := os.MkdirAll(opts.BackupDir, 0700); err != nil {
		return BackupInfo{}, err
	}

	finalPath := uniqueBackupPath(opts.BackupDir, createdAt)
	tempPath := finalPath + ".tmp"
	if err := os.RemoveAll(tempPath); err != nil {
		return BackupInfo{}, err
	}
	if err := os.MkdirAll(tempPath, 0700); err != nil {
		return BackupInfo{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tempPath)
		}
	}()

	dbBackupPath := filepath.Join(tempPath, backupDBName)
	if err := backupSQLite(ctx, opts.DBPath, dbBackupPath); err != nil {
		return BackupInfo{}, err
	}
	attachmentBackupPath := filepath.Join(tempPath, backupAttachDir)
	if err := copyTree(ctx, opts.AttachmentDir, attachmentBackupPath); err != nil {
		return BackupInfo{}, err
	}
	sourceAttachmentAbs, err := filepath.Abs(opts.AttachmentDir)
	if err != nil {
		return BackupInfo{}, err
	}
	if err := writeBackupManifest(tempPath, backupManifest{
		CreatedAt:           createdAt.Format(time.RFC3339),
		DatabaseFile:        backupDBName,
		AttachmentDir:       backupAttachDir,
		SourceDBPath:        opts.DBPath,
		SourceAttachment:    opts.AttachmentDir,
		SourceAttachmentAbs: sourceAttachmentAbs,
	}); err != nil {
		return BackupInfo{}, err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return BackupInfo{}, err
	}
	cleanup = false

	return BackupInfo{
		Path:          finalPath,
		DBPath:        filepath.Join(finalPath, backupDBName),
		AttachmentDir: filepath.Join(finalPath, backupAttachDir),
		CreatedAt:     createdAt,
	}, nil
}

// Restore is RestoreBackup with positional arguments.
func Restore(ctx context.Context, backupPath, dbPath, attachmentDir string) error {
	return RestoreBackup(ctx, RestoreOptions{
		BackupPath:    backupPath,
		DBPath:        dbPath,
		AttachmentDir: attachmentDir,
	})
}

// RestoreBackup replaces the live database and attachment folder with a
// backup's. Everything is first copied beside the live files (".restore-new-"),
// and attachment paths stored in the copied database are rewritten to the live
// attachment folder. Only then are the live files moved aside
// (".restore-old-") and the copies moved in. If a step fails the originals are
// put back. The application must be stopped while this runs.
func RestoreBackup(ctx context.Context, opts RestoreOptions) error {
	if opts.BackupPath == "" || opts.DBPath == "" || opts.AttachmentDir == "" {
		return fmt.Errorf("%w: backup path, database path, and attachment directory are required", ErrValidation)
	}
	backupDB := filepath.Join(opts.BackupPath, backupDBName)
	backupAttachments := filepath.Join(opts.BackupPath, backupAttachDir)
	if _, err := os.Stat(backupDB); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.DBPath), 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.AttachmentDir), 0700); err != nil {
		return err
	}

	stamp := time.Now().Format(backupStampLayout)
	newDB := opts.DBPath + ".restore-new-" + stamp
	newAttachments := opts.AttachmentDir + ".restore-new-" + stamp
	if err := os.RemoveAll(newDB); err != nil {
		return err
	}
	if err := os.RemoveAll(newAttachments); err != nil {
		return err
	}
	if err := copyFile(ctx, backupDB, newDB); err != nil {
		return err
	}
	if err := copyTree(ctx, backupAttachments, newAttachments); err != nil {
		return errors.Join(err, os.Remove(newDB))
	}

	// Relocation changes the directory serving files. Rewrite metadata only in
	// this private staging copy; the immutable backup and the live DB are untouched.
	if err := rebaseRestoredAttachments(ctx, newDB, opts.BackupPath, opts.AttachmentDir); err != nil {
		return errors.Join(err, os.Remove(newDB), os.RemoveAll(newAttachments))
	}

	// The -wal and -shm files belong to the database being replaced. Left beside
	// the restored file, SQLite would replay that WAL into it on the next open —
	// after an unclean stop the WAL holds most of the live data — so the restore
	// would silently serve the old database. They move aside with it and come
	// back with it on rollback.
	sidecarsOld, err := moveDBSidecars(opts.DBPath, opts.DBPath+".restore-old-"+stamp)
	if err != nil {
		_ = os.Remove(newDB)
		_ = os.RemoveAll(newAttachments)
		return err
	}
	dbOld, err := replacePath(opts.DBPath, newDB, stamp)
	if err != nil {
		_ = os.Remove(newDB)
		_ = os.RemoveAll(newAttachments)
		return errors.Join(err, restoreDBSidecars(opts.DBPath, sidecarsOld))
	}
	attachmentsOld, err := replacePath(opts.AttachmentDir, newAttachments, stamp)
	if err != nil {
		rollbackErr := rollbackReplace(opts.DBPath, dbOld)
		sidecarErr := restoreDBSidecars(opts.DBPath, sidecarsOld)
		cleanupErr := os.RemoveAll(newAttachments)
		return errors.Join(err, rollbackErr, sidecarErr, cleanupErr)
	}
	cleanupErrs := []error{os.RemoveAll(dbOld), os.RemoveAll(attachmentsOld)}
	for _, old := range sidecarsOld {
		cleanupErrs = append(cleanupErrs, os.RemoveAll(old))
	}
	return errors.Join(cleanupErrs...)
}

// dbSidecarSuffixes are SQLite's write-ahead log and shared-memory files,
// which sit beside the database file.
var dbSidecarSuffixes = []string{"-wal", "-shm"}

// moveDBSidecars renames dbPath's -wal and -shm files to sit beside oldBase and
// returns the suffix → moved path of each one that existed.
func moveDBSidecars(dbPath, oldBase string) (map[string]string, error) {
	moved := map[string]string{}
	for _, suffix := range dbSidecarSuffixes {
		src := dbPath + suffix
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, errors.Join(err, restoreDBSidecars(dbPath, moved))
		}
		dst := oldBase + suffix
		if err := os.RemoveAll(dst); err != nil {
			return nil, errors.Join(err, restoreDBSidecars(dbPath, moved))
		}
		if err := os.Rename(src, dst); err != nil {
			return nil, errors.Join(err, restoreDBSidecars(dbPath, moved))
		}
		moved[suffix] = dst
	}
	return moved, nil
}

// restoreDBSidecars moves sidecars moved by moveDBSidecars back beside dbPath.
func restoreDBSidecars(dbPath string, moved map[string]string) error {
	var errs []error
	for suffix, old := range moved {
		errs = append(errs, os.Rename(old, dbPath+suffix))
	}
	return errors.Join(errs...)
}

// PruneBackups applies the retention policy: every backup younger than keepDays is
// kept, and so is the newest backup of each of the keepMonths calendar months
// before the current one. Everything else named backup-* is removed. A backup's
// age is the timestamp in its folder name (the time CreateBackup stamped on it),
// which survives a copy to another disk; a folder whose name carries no time
// falls back to its mtime and is never a month's backup.
func PruneBackups(backupDir string, keepDays, keepMonths int, now time.Time) error {
	if backupDir == "" || keepDays <= 0 {
		return nil
	}
	dayCutoff := now.AddDate(0, 0, -keepDays)
	firstMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -keepMonths, 0)
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return err
	}
	type candidate struct {
		name     string
		at       time.Time
		fromName bool
	}
	var backups []candidate
	newestOfMonth := map[string]candidate{}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "backup-") {
			continue
		}
		c := candidate{name: entry.Name()}
		c.at, c.fromName = backupTimeFromName(entry.Name(), now.Location())
		if !c.fromName {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			c.at = info.ModTime()
		}
		backups = append(backups, c)
		if c.fromName && keepMonths > 0 && !c.at.Before(firstMonth) {
			month := c.at.Format("2006-01")
			if cur, ok := newestOfMonth[month]; !ok || c.at.After(cur.at) || (c.at.Equal(cur.at) && c.name > cur.name) {
				newestOfMonth[month] = c
			}
		}
	}
	for _, c := range backups {
		if !c.at.Before(dayCutoff) {
			continue
		}
		if c.fromName && newestOfMonth[c.at.Format("2006-01")].name == c.name {
			continue
		}
		if err := os.RemoveAll(filepath.Join(backupDir, c.name)); err != nil {
			return err
		}
	}
	return nil
}

// backupTimeFromName reads the time from a folder named by uniqueBackupPath:
// backup-YYYYMMDD-HHMMSS, optionally followed by a -NN collision suffix.
func backupTimeFromName(name string, loc *time.Location) (time.Time, bool) {
	const layout = backupStampLayout
	rest := strings.TrimPrefix(name, "backup-")
	if len(rest) < len(layout) {
		return time.Time{}, false
	}
	if tail := rest[len(layout):]; tail != "" && !strings.HasPrefix(tail, "-") {
		return time.Time{}, false
	}
	at, err := time.ParseInLocation(layout, rest[:len(layout)], loc)
	if err != nil {
		return time.Time{}, false
	}
	return at, true
}

// backupManifest is manifest.json. SourceAttachment and SourceAttachmentAbs
// record where the attachments lived when the backup was taken, so a restore
// can rewrite stored attachment paths to the new location.
type backupManifest struct {
	CreatedAt           string `json:"created_at"`
	DatabaseFile        string `json:"database_file"`
	AttachmentDir       string `json:"attachment_dir"`
	SourceDBPath        string `json:"source_db_path"`
	SourceAttachment    string `json:"source_attachment_dir"`
	SourceAttachmentAbs string `json:"source_attachment_absolute,omitempty"`
}

// backupSQLite writes a consistent copy of the database at sourcePath to
// destPath, owner-only. A partial copy is removed on failure.
func backupSQLite(ctx context.Context, sourcePath, destPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0700); err != nil {
		return err
	}
	// VACUUM INTO asks SQLite to create a transactionally consistent database
	// image. A plain file copy can miss pages that are still in a WAL file.
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(destPath)
		}
	}()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return err
	}
	_, execErr := db.ExecContext(ctx, "VACUUM INTO ?", destPath)
	closeErr := db.Close()
	if execErr != nil {
		return errors.Join(execErr, closeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Chmod(destPath, 0600); err != nil {
		return err
	}
	complete = true
	return nil
}

// writeBackupManifest writes manifest.json into dir; the file must not exist.
func writeBackupManifest(dir string, manifest backupManifest) error {
	path := filepath.Join(dir, backupManifestFile)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	encodeErr := enc.Encode(manifest)
	closeErr := f.Close()
	if encodeErr != nil || closeErr != nil {
		_ = os.Remove(path)
	}
	return errors.Join(encodeErr, closeErr)
}

// uniqueBackupPath names a new backup folder for time t, adding -01, -02 ...
// when a folder with that name already exists.
func uniqueBackupPath(backupDir string, t time.Time) string {
	base := filepath.Join(backupDir, "backup-"+t.Format(backupStampLayout))
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s-%02d", base, i)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

// copyTree copies the folder src to dst (which must not contain the files
// yet). An empty or missing src produces an empty dst. Links and special files
// are refused rather than followed.
func copyTree(ctx context.Context, src, dst string) error {
	if src == "" {
		return os.MkdirAll(dst, 0700)
	}
	info, err := os.Lstat(src)
	if os.IsNotExist(err) {
		return os.MkdirAll(dst, 0700)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: attachment source must be a directory", ErrValidation)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: backups cannot include links or special files", ErrValidation)
		}
		return copyFile(ctx, path, target)
	})
}

// copyFile copies one regular file to dst, which must not exist, creating its
// parent folders. The copy is always owner-only (0600), whatever the source's
// permissions. A partial copy is removed on failure.
func copyFile(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: backup source must be a regular file", ErrValidation)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return errors.Join(copyErr, closeErr)
	}
	if closeErr != nil {
		_ = os.Remove(dst)
	}
	return closeErr
}

// replacePath moves target aside to target.restore-old-<stamp> (if it exists)
// and moves replacement into its place. It returns where the old copy went; if
// the final move fails the old copy is put back.
func replacePath(target, replacement, stamp string) (string, error) {
	oldPath := target + ".restore-old-" + stamp
	if _, err := os.Stat(target); err == nil {
		if err := os.RemoveAll(oldPath); err != nil {
			return "", err
		}
		if err := os.Rename(target, oldPath); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(replacement, target); err != nil {
		return oldPath, errors.Join(err, rollbackReplace(target, oldPath))
	}
	return oldPath, nil
}

// rollbackReplace undoes replacePath: it removes whatever is at target and
// moves the old copy back.
func rollbackReplace(target, oldPath string) error {
	if oldPath == "" {
		return nil
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if _, err := os.Stat(oldPath); err == nil {
		return os.Rename(oldPath, target)
	}
	return nil
}

// rebaseRestoredAttachments rewrites every stored attachment path in the
// database copy at dbPath so it points under targetDir, keeping each file's
// path relative to the attachment folder recorded in the backup's manifest.
// A path outside that folder is a validation error, which stops the restore
// before anything live is touched.
func rebaseRestoredAttachments(ctx context.Context, dbPath, backupPath, targetDir string) error {
	contents, err := os.ReadFile(filepath.Join(backupPath, backupManifestFile))
	if err != nil {
		return fmt.Errorf("read restore manifest: %w", err)
	}
	var manifest backupManifest
	if err = json.Unmarshal(contents, &manifest); err != nil {
		return fmt.Errorf("read restore manifest: %w", err)
	}
	targetRoot, err := filepath.Abs(targetDir)
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	// Ensure rebased metadata is in the file that will be moved, not a WAL sidecar.
	if _, err = db.ExecContext(ctx, `PRAGMA journal_mode=DELETE`); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"payment_attachments", "request_attachments"} {
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,stored_path FROM `+table)
		if err != nil {
			return err
		}
		type update struct {
			id   int64
			path string
		}
		updates := []update{}
		for rows.Next() {
			var id int64
			var stored string
			if err = rows.Scan(&id, &stored); err != nil {
				rows.Close()
				return err
			}
			relative, matched := manifest.relativeToSourceRoot(stored)
			if !matched {
				rows.Close()
				return fmt.Errorf("%w: attachment %d in %s is outside the recorded backup attachment root", ErrValidation, id, table)
			}
			updates = append(updates, update{id, filepath.Join(targetRoot, relative)})
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, u := range updates {
			if _, err = tx.ExecContext(ctx, `UPDATE `+table+` SET stored_path=? WHERE id=?`, u.path, u.id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// relativeToSourceRoot returns stored's path relative to the attachment folder
// recorded in the manifest (tried as written, then as an absolute path), or
// false if it lies outside that folder. It never guesses from a file name.
func (m backupManifest) relativeToSourceRoot(stored string) (string, bool) {
	for _, root := range []string{m.SourceAttachment, m.SourceAttachmentAbs} {
		if root == "" || filepath.IsAbs(root) != filepath.IsAbs(stored) {
			continue
		}
		rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(stored))
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel) {
			return rel, true
		}
	}
	return "", false
}
