package store

import (
	"context"
	"database/sql"
	"testing"
)

func TestMigrateAppliesAndIsIdempotent(t *testing.T) {
	s := newTestStore(t) // Open already runs migrate once

	var version int
	if err := s.DB().QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if want := migrations[len(migrations)-1].Version; version != want {
		t.Fatalf("user_version = %d, want %d (latest migration)", version, want)
	}

	// Re-running migrate against an already-migrated DB is a no-op and must not error.
	if err := migrate(s.DB()); err != nil {
		t.Fatalf("re-running migrate: %v", err)
	}
}

func TestColumnExistsReportsSchemaShape(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	got, err := columnExists(tx, "users", "role")
	if err != nil || !got {
		t.Fatalf("columnExists(users, role) = %v, %v; want true, nil", got, err)
	}
	got, err = columnExists(tx, "users", "does_not_exist")
	if err != nil || got {
		t.Fatalf("columnExists(users, does_not_exist) = %v, %v; want false, nil", got, err)
	}
}

func TestMigrationV1CreatesPermissionTables(t *testing.T) {
	s := newTestStore(t)
	for _, table := range []string{"roles", "role_permissions", "role_data_scope", "user_roles"} {
		var name string
		if err := s.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name); err != nil {
			t.Fatalf("table %q missing after migrate: %v", table, err)
		}
	}
	// Case-insensitive unique role name index exists.
	var idx string
	if err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_roles_name_nocase'`,
	).Scan(&idx); err != nil {
		t.Fatalf("idx_roles_name_nocase missing: %v", err)
	}
}

var _ = sql.ErrNoRows // keep database/sql imported for later tests in this file

func TestMigrationBackfillsExistingUsers(t *testing.T) {
	// Simulate a legacy database: create the store, then insert a user row
	// directly WITHOUT user_roles (as if it predated the RBAC migration),
	// clear user_roles, reset user_version to 0 so the single v1 re-applies its
	// seed + back-fill (v1 is idempotent — tables use IF NOT EXISTS and
	// seedSystemRoles upserts by name), and re-run migrate.
	s := newTestStore(t)
	if _, err := s.DB().Exec(`INSERT INTO users(email,name,password_hash,role,active) VALUES('legacy@example.com','Legacy','hash','admin',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`DELETE FROM user_roles`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.DB()); err != nil {
		t.Fatalf("re-migrate legacy DB: %v", err)
	}
	var id int64
	if err := s.DB().QueryRow(`SELECT id FROM users WHERE email='legacy@example.com'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	roles, err := s.UserRoles(context.Background(), id)
	if err != nil || len(roles) != 1 || roles[0].Name != "Admin" {
		t.Fatalf("legacy admin back-fill = %+v, %v; want [Admin]", roles, err)
	}
}
