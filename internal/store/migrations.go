package store

import (
	"database/sql"
	"fmt"
)

type migration struct {
	Version int
	Name    string
	Up      func(*sql.Tx) error
}

// migrations is ordered and append-only. Versions are contiguous and never
// reordered; each phase appends its own. v0 is the baseline schemaSQL.
//
// Reserved global sequence (one monotonic line across all phases):
//
//	v1 — permissions (roles, role_permissions, role_data_scope, user_roles)
//	v2 — vendors
//	v3 — requests
//	v4 — payments linking + settlement
//	v5 — recoverable categories
//	v6 — notification settings
//
// Phase 1 owns exactly one migration, v1: it creates all four permission tables
// (overview §3 P1); a later task folds seeding of the system roles and the
// user_roles back-fill into the same v1 Up.
var migrations = []migration{
	{
		Version: 1,
		Name:    "permission schema",
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS roles (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  is_system INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_name_nocase ON roles(lower(name));

CREATE TABLE IF NOT EXISTS role_permissions (
  role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  resource TEXT NOT NULL,
  action TEXT NOT NULL,
  PRIMARY KEY(role_id, resource, action)
);

CREATE TABLE IF NOT EXISTS role_data_scope (
  role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  resource TEXT NOT NULL,
  scope TEXT NOT NULL,
  PRIMARY KEY(role_id, resource)
);

CREATE TABLE IF NOT EXISTS user_roles (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  PRIMARY KEY(user_id, role_id)
);
`)
			return err
		},
	},
}

// migrate applies every registered migration whose Version is greater than the
// database's current PRAGMA user_version, each inside its own transaction, then
// stamps user_version. Re-running against a migrated DB is a no-op.
func migrate(db *sql.DB) error {
	var current int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if err := m.Up(tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d (%s): %w", m.Version, m.Name, err)
		}
		// PRAGMA cannot be parameterized; the version is a trusted integer literal.
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, m.Version)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("stamp user_version %d: %w", m.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// columnExists guards additive column migrations so a re-run is safe even if
// user_version is out of sync with the physical schema.
func columnExists(tx *sql.Tx, table, col string) (bool, error) {
	rows, err := tx.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
}
