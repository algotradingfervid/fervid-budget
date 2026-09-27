package app

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/config"
	"fervidbudget/internal/store"
)

func startupFixture(t *testing.T) (config.Config, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{DBPath: filepath.Join(dir, "fervid.db"), AttachmentDir: filepath.Join(dir, "attachments"), BackupDir: filepath.Join(dir, "backups"), SessionKey: "synthetic-startup-test-session-key", AdminEmail: "bootstrap@example.test", AdminName: "Bootstrap Admin", AdminPassword: "BootstrapPass123"}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return cfg, st
}

func startupRows(t *testing.T, db *sql.DB, query string) [][]any {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]any
	for rows.Next() {
		row, ptrs := make([]any, len(cols)), make([]any, len(cols))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStartupBootstrapsOnlyEmptyUserDatabase(t *testing.T) {
	cfg, st := startupFixture(t)
	if _, err := New(cfg, st); err != nil {
		t.Fatal(err)
	}
	u, err := st.UserByEmail(context.Background(), cfg.AdminEmail)
	if err != nil || !u.Active || u.Role != "admin" || !auth.CheckPassword(u.PasswordHash, cfg.AdminPassword) {
		t.Fatal("empty installation did not bootstrap a usable administrator")
	}
	var roles int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=? AND r.name='Admin'`, u.ID).Scan(&roles); err != nil || roles != 1 {
		t.Fatal("bootstrap account lacks its Admin role")
	}
	// Changed configuration on the next startup must not add a second account.
	cfg.AdminEmail, cfg.AdminPassword = "changed@example.test", "DifferentPass456"
	if _, err := New(cfg, st); err != nil {
		t.Fatal(err)
	}
	if rows := startupRows(t, st.DB(), "SELECT id FROM users"); len(rows) != 1 {
		t.Fatal("restart added a second bootstrap user")
	}
}

func TestStartupPreservesExistingAccountsAndRoles(t *testing.T) {
	for _, sameEmail := range []bool{false, true} {
		t.Run(map[bool]string{false: "configured_email_absent", true: "configured_email_already_disabled_nonadmin"}[sameEmail], func(t *testing.T) {
			cfg, st := startupFixture(t)
			email := "restored@example.test"
			if sameEmail {
				email = cfg.AdminEmail
			}
			var roleID int64
			if err := st.DB().QueryRow(`SELECT id FROM roles WHERE name='Requester'`).Scan(&roleID); err != nil {
				t.Fatal(err)
			}
			hash, err := auth.HashPassword("ExistingPass987")
			if err != nil {
				t.Fatal(err)
			}
			id, err := st.CreateUserWithRoles(context.Background(), email, "Existing Account", hash, "data_entry", false, []int64{roleID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.DB().Exec(`UPDATE users SET attempt_count=3,last_attempt=CURRENT_TIMESTAMP,locked=CURRENT_TIMESTAMP,session_version=4 WHERE id=?`, id); err != nil {
				t.Fatal(err)
			}
			beforeUsers := startupRows(t, st.DB(), "SELECT * FROM users ORDER BY id")
			beforeRoles := startupRows(t, st.DB(), "SELECT * FROM user_roles ORDER BY user_id,role_id")
			// Bootstrap credentials are irrelevant for an existing installation,
			// so even absent/invalid bootstrap values must not prevent startup.
			cfg.AdminPassword = ""
			if _, err := New(cfg, st); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(beforeUsers, startupRows(t, st.DB(), "SELECT * FROM users ORDER BY id")) {
				t.Fatal("startup changed restored account data")
			}
			if !reflect.DeepEqual(beforeRoles, startupRows(t, st.DB(), "SELECT * FROM user_roles ORDER BY user_id,role_id")) {
				t.Fatal("startup changed restored role assignments")
			}
		})
	}
}

func TestConcurrentStartupCreatesOneBootstrapAdministrator(t *testing.T) {
	cfg, st := startupFixture(t)
	start := make(chan struct{})
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	for _, email := range []string{"first@example.test", "second@example.test"} {
		cfg := cfg
		cfg.AdminEmail = email
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, err := New(cfg, st); errors <- err }()
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(startupRows(t, st.DB(), "SELECT id FROM users")) != 1 || len(startupRows(t, st.DB(), "SELECT * FROM user_roles")) != 1 {
		t.Fatal("concurrent startup created multiple accounts or lost its role assignment")
	}
}
