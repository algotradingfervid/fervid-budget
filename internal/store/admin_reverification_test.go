package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReverifyAdminChangeRevokesOutstandingReset(t *testing.T) {
	for _, mode := range []string{"legacy password", "profile password", "deactivate then reactivate"} {
		t.Run(mode, func(t *testing.T) {
			s := newTestStore(t)
			ctx := t.Context()
			actor := newRoleActor(t, s, ctx)
			id, err := s.CreateUser(ctx, "reset-target@example.invalid", "Reset Target", "oldhash", "data_entry", true)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			if err = s.CreatePasswordReset(ctx, id, "outstanding", now); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "legacy password":
				err = s.UpdateUser(ctx, id, "Reset Target", "data_entry", true, "admin-newhash")
			case "profile password":
				err = s.SaveUser(ctx, actor, UserSaveInput{ID: id, Name: "Reset Target", Role: "data_entry", Active: true, PasswordHash: "admin-newhash"})
			default:
				err = s.SaveUser(ctx, actor, UserSaveInput{ID: id, Name: "Reset Target", Role: "data_entry", Active: false})
				if err == nil {
					err = s.SaveUser(ctx, actor, UserSaveInput{ID: id, Name: "Reset Target", Role: "data_entry", Active: true})
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = s.ConsumePasswordReset(ctx, "outstanding", "attacker-chosen", now); !errors.Is(err, ErrNotFound) {
				t.Fatalf("stale reset must not override administrator action; got %v", err)
			}
		})
	}
}

func TestReverifyRestoreRebasesBothAttachmentTablesAndRejectsTraversal(t *testing.T) {
	for _, mode := range []string{"absolute", "legacy relative", "outside root"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			sourceDB := filepath.Join(root, "source.db")
			attachments := filepath.Join(root, "attachments")
			if err := os.MkdirAll(filepath.Join(attachments, "nested"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(attachments, "nested", "proof.txt"), []byte("proof"), 0600); err != nil {
				t.Fatal(err)
			}
			original := filepath.Join(attachments, "nested", "proof.txt")
			if mode == "legacy relative" {
				original = "attachments/nested/proof.txt"
			}
			if mode == "outside root" {
				original = filepath.Join(root, "outside.txt")
			}
			db, err := sql.Open("sqlite", sourceDB)
			if err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"payment_attachments", "request_attachments"} {
				if _, err = db.Exec(`CREATE TABLE `+table+` (id INTEGER PRIMARY KEY,stored_path TEXT); INSERT INTO `+table+`(stored_path) VALUES(?)`, original); err != nil {
					t.Fatal(err)
				}
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			info, err := CreateBackup(t.Context(), BackupOptions{DBPath: sourceDB, AttachmentDir: attachments, BackupDir: filepath.Join(root, "backups")})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "legacy relative" {
				if err = os.Remove(filepath.Join(info.Path, "manifest.json")); err != nil {
					t.Fatal(err)
				}
				if err = writeBackupManifest(info.Path, backupManifest{SourceAttachment: "attachments", DatabaseFile: "fervid.db", AttachmentDir: "attachments"}); err != nil {
					t.Fatal(err)
				}
			}
			targetDB := filepath.Join(root, "target.db")
			targetAttachments := filepath.Join(root, "target-attachments")
			if err = os.WriteFile(targetDB, []byte("live database unchanged on failure"), 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(targetAttachments, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(targetAttachments, "keep.txt"), []byte("live file"), 0600); err != nil {
				t.Fatal(err)
			}
			err = Restore(t.Context(), info.Path, targetDB, targetAttachments)
			if mode == "outside root" {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("unsafe attachment accepted: %v", err)
				}
				data, e := os.ReadFile(targetDB)
				if e != nil || string(data) != "live database unchanged on failure" {
					t.Fatal("unsafe restore changed live DB")
				}
				if _, e = os.Stat(filepath.Join(targetAttachments, "keep.txt")); e != nil {
					t.Fatal("unsafe restore changed live attachments")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			target, err := sql.Open("sqlite", targetDB)
			if err != nil {
				t.Fatal(err)
			}
			defer target.Close()
			originalDB, err := sql.Open("sqlite", info.DBPath)
			if err != nil {
				t.Fatal(err)
			}
			defer originalDB.Close()
			for _, table := range []string{"payment_attachments", "request_attachments"} {
				var stored, untouched string
				if err = target.QueryRow(`SELECT stored_path FROM ` + table + ` WHERE id=1`).Scan(&stored); err != nil {
					t.Fatal(err)
				}
				if stored != filepath.Join(targetAttachments, "nested", "proof.txt") {
					t.Fatalf("%s rebase=%q", table, stored)
				}
				if err = originalDB.QueryRow(`SELECT stored_path FROM ` + table + ` WHERE id=1`).Scan(&untouched); err != nil {
					t.Fatal(err)
				}
				if untouched != original {
					t.Fatal("immutable backup modified")
				}
			}
		})
	}
}
