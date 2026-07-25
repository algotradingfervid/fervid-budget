package store

import (
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
