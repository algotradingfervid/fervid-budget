package store

import (
	"context"
	"encoding/json"
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
	if err := writeBackupManifest(tempPath, backupManifest{
		CreatedAt:        createdAt.Format(time.RFC3339),
		DatabaseFile:     backupDBName,
		AttachmentDir:    "attachments",
		SourceDBPath:     opts.DBPath,
		SourceAttachment: opts.AttachmentDir,
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
		_ = os.Remove(newDB)
		return err
	}

	dbOld, err := replacePath(opts.DBPath, newDB, stamp)
	if err != nil {
		_ = os.Remove(newDB)
		_ = os.RemoveAll(newAttachments)
		return err
	}
	attachmentsOld, err := replacePath(opts.AttachmentDir, newAttachments, stamp)
	if err != nil {
		_ = rollbackReplace(opts.DBPath, dbOld)
		_ = os.RemoveAll(newAttachments)
		return err
	}
	_ = os.RemoveAll(dbOld)
	_ = os.RemoveAll(attachmentsOld)
	return nil
}

func PruneBackups(backupDir string, keepDays int, now time.Time) error {
	if backupDir == "" || keepDays <= 0 {
		return nil
	}
	cutoff := now.AddDate(0, 0, -keepDays)
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "backup-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(filepath.Join(backupDir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

type backupManifest struct {
	CreatedAt        string `json:"created_at"`
	DatabaseFile     string `json:"database_file"`
	AttachmentDir    string `json:"attachment_dir"`
	SourceDBPath     string `json:"source_db_path"`
	SourceAttachment string `json:"source_attachment_dir"`
}

func backupSQLite(ctx context.Context, sourcePath, destPath string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	return copyFile(ctx, sourcePath, destPath)
}

func writeBackupManifest(dir string, manifest backupManifest) error {
	f, err := os.OpenFile(filepath.Join(dir, "manifest.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(manifest)
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
		return copyErr
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
		_ = rollbackReplace(target, oldPath)
		return oldPath, err
	}
	return oldPath, nil
}

func rollbackReplace(target, oldPath string) error {
	if oldPath == "" {
		return nil
	}
	_ = os.RemoveAll(target)
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
