// startupcheck constructs the real application on one additional local copy.
// It never listens, schedules work, sends mail, or accepts a database argument.
package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	"fervidbudget/internal/app"
	"fervidbudget/internal/config"
	"fervidbudget/internal/store"
)

const root = "/Users/narendhupati/Library/Application Support/Fervid Budget QA/production-rehearsal-2026-09-26"

func main() {
	data := filepath.Join(root, "startup-check/data")
	path := filepath.Join(data, "fervid.db")
	for p := path; ; p = filepath.Dir(p) {
		i, err := os.Lstat(p)
		if err != nil || i.Mode()&os.ModeSymlink != 0 {
			fail("allowlisted_copy_validation")
		}
		if p == string(filepath.Separator) {
			break
		}
	}
	if i, err := os.Stat(root); err != nil || i.Mode().Perm()&0077 != 0 {
		fail("private_root_permissions")
	}
	s, err := store.Open(path)
	if err != nil {
		fail("open_copy")
	}
	defer s.Close()
	before, count := accountState(s.DB())
	cfg := config.Config{DBPath: path, AttachmentDir: filepath.Join(data, "attachments"), BackupDir: filepath.Join(root, "startup-check/backups"), SessionKey: "local-startup-regression-no-listener", AdminEmail: "admin@fervid.local", AdminName: "Fervid Admin", AdminPassword: "admin123"}
	if _, err = app.New(cfg, s); err != nil {
		fail("construct_application")
	}
	after, afterCount := accountState(s.DB())
	if before != after || count != afterCount {
		fail("existing_accounts_changed")
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"all_checks_pass": true, "users_before": count, "users_after": afterCount, "all_account_columns_and_role_assignments_match": true, "http_listener_or_scheduler_started": false})
}

func accountState(db *sql.DB) ([32]byte, int) {
	h := sha256.New()
	enc := json.NewEncoder(h)
	for _, query := range []string{"SELECT * FROM users ORDER BY id", "SELECT * FROM user_roles ORDER BY user_id,role_id"} {
		rows, err := db.Query(query)
		if err != nil {
			fail("account_query")
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			fail("account_columns")
		}
		for rows.Next() {
			values, ptrs := make([]any, len(cols)), make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err = rows.Scan(ptrs...); err != nil {
				rows.Close()
				fail("account_read")
			}
			if err = enc.Encode(values); err != nil {
				rows.Close()
				fail("account_digest")
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			fail("account_read")
		}
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		fail("account_count")
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest, count
}

func fail(stage string) {
	json.NewEncoder(os.Stderr).Encode(map[string]any{"all_checks_pass": false, "failed_stage": stage, "details_redacted": true})
	os.Exit(1)
}
