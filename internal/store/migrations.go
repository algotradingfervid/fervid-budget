package store

import (
	"context"
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
// Phase 1 owns exactly one migration, v1: it creates all four permission
// tables (overview §3 P1), seeds the four system roles and back-fills
// user_roles from the legacy users.role column. The whole Up is idempotent —
// CREATE TABLE IF NOT EXISTS, upsert-by-name seeding and conflict-tolerant
// back-fill — so re-applying it after a user_version reset is safe.
var migrations = []migration{
	{
		Version: 1,
		Name:    "permission schema",
		Up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`
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
`); err != nil {
				return err
			}
			if err := seedSystemRoles(tx); err != nil {
				return err
			}
			if err := backfillUserRoles(tx); err != nil {
				return err
			}
			return addDefaultApproverColumn(tx)
		},
	},
}

// addDefaultApproverColumn is additive and guarded by columnExists, so
// re-applying v1 to a database that already has the column is a no-op (G9).
// Phase 2 reads this column to pre-select the approver on a new request.
func addDefaultApproverColumn(tx *sql.Tx) error {
	has, err := columnExists(tx, "users", "default_approver_id")
	if err != nil || has {
		return err
	}
	_, err = tx.Exec(`ALTER TABLE users ADD COLUMN default_approver_id INTEGER REFERENCES users(id)`)
	return err
}

type systemRoleDef struct {
	Name        string
	Description string
	Grants      []Grant
	Scopes      []ScopeGrant
}

// systemRoleDefaults are the seeded starter roles (overview §5). Admin holds
// every action in the canonical vocabulary.
var systemRoleDefaults = []systemRoleDef{
	{
		Name:        "Requester",
		Description: "Raise and manage your own payment requests.",
		Grants: []Grant{
			{"request", "view"}, {"request", "create"}, {"request", "edit"},
			{"request", "withdraw"}, {"request", "reraise"}, {"request", "comment"},
			{"attachment", "view"}, {"attachment", "create"},
		},
		Scopes: []ScopeGrant{{"request", "own"}},
	},
	{
		Name:        "Manager",
		Description: "Review and decide on payment requests.",
		Grants: []Grant{
			{"request", "view"}, {"request", "comment"},
			{"approval", "approve"}, {"approval", "reject"}, {"approval", "return"},
			{"approval", "reassign"}, {"approval", "accept_partial"},
			{"grid", "view"}, {"report", "view"},
		},
		Scopes: []ScopeGrant{{"request", "all"}},
	},
	{
		Name:        "Accounts",
		Description: "Process approved requests and record payments.",
		Grants: []Grant{
			{"request", "view"}, {"request", "comment"},
			{"payment", "view"}, {"payment", "create"}, {"payment", "edit"}, {"payment", "void"},
			{"payment", "process"}, {"payment", "settle"}, {"payment", "mark_partial"}, {"payment", "hold"},
			{"attachment", "view"}, {"attachment", "create"},
			{"grid", "view"}, {"report", "view"}, {"report", "export"},
			{"recoverable_report", "view"}, {"recoverable_report", "export"},
		},
		Scopes: []ScopeGrant{{"request", "all"}, {"payment", "all"}},
	},
	{
		Name:        "Admin",
		Description: "Full administrative access.",
		Grants:      adminGrants(),
		Scopes:      []ScopeGrant{{"request", "all"}, {"payment", "all"}},
	},
}

func adminGrants() []Grant {
	var out []Grant
	for _, res := range resourceOrder {
		for _, act := range resourceActions[res] {
			out = append(out, Grant{res, act})
		}
	}
	return out
}

// seedSystemRoles is idempotent: it upserts each system role by name and resets
// its grants and scopes to the canonical defaults.
func seedSystemRoles(tx *sql.Tx) error {
	for _, def := range systemRoleDefaults {
		var roleID int64
		err := tx.QueryRow(`SELECT id FROM roles WHERE lower(name)=lower(?)`, def.Name).Scan(&roleID)
		switch err {
		case sql.ErrNoRows:
			res, insErr := tx.Exec(`INSERT INTO roles(name,description,is_system) VALUES(?,?,1)`, def.Name, def.Description)
			if insErr != nil {
				return insErr
			}
			if roleID, err = res.LastInsertId(); err != nil {
				return err
			}
		case nil:
			if _, err := tx.Exec(`UPDATE roles SET is_system=1, description=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, def.Description, roleID); err != nil {
				return err
			}
		default:
			return err
		}
		if _, err := tx.Exec(`DELETE FROM role_permissions WHERE role_id=?`, roleID); err != nil {
			return err
		}
		for _, g := range def.Grants {
			if _, err := tx.Exec(`INSERT INTO role_permissions(role_id,resource,action) VALUES(?,?,?)`, roleID, g.Resource, g.Action); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`DELETE FROM role_data_scope WHERE role_id=?`, roleID); err != nil {
			return err
		}
		for _, sc := range def.Scopes {
			if _, err := tx.Exec(`INSERT INTO role_data_scope(role_id,resource,scope) VALUES(?,?,?)`, roleID, sc.Resource, sc.Scope); err != nil {
				return err
			}
		}
	}
	return nil
}

// backfillUserRoles maps every existing user's legacy users.role to a system
// role (admin->Admin, everything else->Accounts) and inserts user_roles rows.
func backfillUserRoles(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id, role FROM users`)
	if err != nil {
		return err
	}
	type u struct {
		id   int64
		role string
	}
	var users []u
	for rows.Next() {
		var one u
		if err := rows.Scan(&one.id, &one.role); err != nil {
			rows.Close()
			return err
		}
		users = append(users, one)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, one := range users {
		name := "Accounts"
		if one.role == "admin" {
			name = "Admin"
		}
		var roleID int64
		if err := tx.QueryRow(`SELECT id FROM roles WHERE lower(name)=lower(?)`, name).Scan(&roleID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO user_roles(user_id,role_id) VALUES(?,?) ON CONFLICT(user_id,role_id) DO NOTHING`, one.id, roleID); err != nil {
			return err
		}
	}
	return nil
}

// assignDefaultRoleTx gives a freshly created user a user_roles row derived from
// its legacy role, so users created after the RBAC migration still resolve to a
// concrete permission set. Tolerant if system roles are somehow absent.
func assignDefaultRoleTx(ctx context.Context, tx *sql.Tx, userID int64, legacyRole string) error {
	name := "Accounts"
	if legacyRole == "admin" {
		name = "Admin"
	}
	var roleID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE lower(name)=lower(?)`, name).Scan(&roleID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES(?,?) ON CONFLICT(user_id,role_id) DO NOTHING`, userID, roleID)
	return err
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
