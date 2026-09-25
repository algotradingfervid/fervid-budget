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

const backupDBName = "fervid.db"

type BackupOptions struct {
	DBPath        string
	AttachmentDir string
	BackupDir     string
	Now           func() time.Time
}

type BackupInfo struct {
	Path          string
	DBPath        string
	AttachmentDir string
	CreatedAt     time.Time
}

type RestoreOptions struct {
	BackupPath    string
	DBPath        string
	AttachmentDir string
}

func Backup(ctx context.Context, dbPath, attachmentDir, backupDir string) (BackupInfo, error) {
	return CreateBackup(ctx, BackupOptions{
		DBPath:        dbPath,
		AttachmentDir: attachmentDir,
		BackupDir:     backupDir,
	})
}

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
	if err := os.MkdirAll(opts.BackupDir, 0755); err != nil {
		return BackupInfo{}, err
	}

	finalPath := uniqueBackupPath(opts.BackupDir, createdAt)
	tempPath := finalPath + ".tmp"
	if err := os.RemoveAll(tempPath); err != nil {
		return BackupInfo{}, err
	}
	if err := os.MkdirAll(tempPath, 0755); err != nil {
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
	attachmentBackupPath := filepath.Join(tempPath, "attachments")
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
		AttachmentDir:       "attachments",
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
		AttachmentDir: filepath.Join(finalPath, "attachments"),
		CreatedAt:     createdAt,
	}, nil
}

func Restore(ctx context.Context, backupPath, dbPath, attachmentDir string) error {
	return RestoreBackup(ctx, RestoreOptions{
		BackupPath:    backupPath,
		DBPath:        dbPath,
		AttachmentDir: attachmentDir,
	})
}

func RestoreBackup(ctx context.Context, opts RestoreOptions) error {
	if opts.BackupPath == "" || opts.DBPath == "" || opts.AttachmentDir == "" {
		return fmt.Errorf("%w: backup path, database path, and attachment directory are required", ErrValidation)
	}
	backupDB := filepath.Join(opts.BackupPath, backupDBName)
	backupAttachments := filepath.Join(opts.BackupPath, "attachments")
	if _, err := os.Stat(backupDB); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.DBPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(opts.AttachmentDir), 0755); err != nil {
		return err
	}

	stamp := time.Now().Format("20060102-150405")
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

func restoreDBSidecars(dbPath string, moved map[string]string) error {
	var errs []error
	for suffix, old := range moved {
		errs = append(errs, os.Rename(old, dbPath+suffix))
	}
	return errors.Join(errs...)
}

// PruneBackups applies the §10 retention: every backup younger than keepDays is
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
	const layout = "20060102-150405"
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

type backupManifest struct {
	CreatedAt           string `json:"created_at"`
	DatabaseFile        string `json:"database_file"`
	AttachmentDir       string `json:"attachment_dir"`
	SourceDBPath        string `json:"source_db_path"`
	SourceAttachment    string `json:"source_attachment_dir"`
	SourceAttachmentAbs string `json:"source_attachment_absolute,omitempty"`
}

func backupSQLite(ctx context.Context, sourcePath, destPath string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
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
	complete = true
	return nil
}

func writeBackupManifest(dir string, manifest backupManifest) error {
	path := filepath.Join(dir, "manifest.json")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
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

func uniqueBackupPath(backupDir string, t time.Time) string {
	base := filepath.Join(backupDir, "backup-"+t.Format("20060102-150405"))
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

func copyTree(ctx context.Context, src, dst string) error {
	if src == "" {
		return os.MkdirAll(dst, 0755)
	}
	info, err := os.Stat(src)
	if os.IsNotExist(err) {
		return os.MkdirAll(dst, 0755)
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
		if err := checkContext(ctx); err != nil {
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
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if d.Type()&fs.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
		}
		return copyFileWithMode(ctx, path, target, info.Mode().Perm())
	})
}

func copyFile(ctx context.Context, src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return copyFileWithMode(ctx, src, dst, info.Mode().Perm())
}

func copyFileWithMode(ctx context.Context, src, dst string, mode fs.FileMode) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
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

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// rebaseRestoredAttachments preserves the same relative filename under the
// manifest's recorded source root. It never guesses from a basename or accepts
// a traversal path. An ambiguous/malformed backup fails before live replacement.
func rebaseRestoredAttachments(ctx context.Context, dbPath, backupPath, targetDir string) error {
	contents, err := os.ReadFile(filepath.Join(backupPath, "manifest.json"))
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
			relative := ""
			matched := false
			for _, root := range []string{manifest.SourceAttachment, manifest.SourceAttachmentAbs} {
				if root == "" || filepath.IsAbs(root) != filepath.IsAbs(stored) {
					continue
				}
				rel, e := filepath.Rel(filepath.Clean(root), filepath.Clean(stored))
				if e == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel) {
					relative = rel
					matched = true
					break
				}
			}
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
