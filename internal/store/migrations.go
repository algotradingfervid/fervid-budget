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
	// Phase 1V owns exactly one migration, v2: the vendor master. Bank columns
	// live on the same row as the rest of the vendor — they are attributes of
	// the vendor, not a separate entity — and are kept from the wrong eyes by
	// the store never selecting them for a caller without vendor_bank:view
	// (see vendors.go). Splitting them into a second table would move the
	// secret without adding a boundary, because the same code would join it.
	//
	// payments.vendor_payee is deliberately untouched: it is the payee snapshot
	// for reimbursements and employee advances, where no vendor row exists, and
	// it keeps historical payments readable. Phase 2 adds vendor_id alongside
	// it rather than replacing it.
	{
		Version: 2,
		Name:    "vendors",
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS vendors (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  vendor_type TEXT NOT NULL DEFAULT 'company',   -- company | proprietor | individual
  status TEXT NOT NULL DEFAULT 'active',         -- active | inactive
  categories TEXT NOT NULL DEFAULT '',
  gstin TEXT NOT NULL DEFAULT '',
  pan TEXT NOT NULL DEFAULT '',
  msme_udyam TEXT NOT NULL DEFAULT '',
  tds_section TEXT NOT NULL DEFAULT '',
  tds_rate TEXT NOT NULL DEFAULT '',
  contact_person TEXT NOT NULL DEFAULT '',
  phone TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  address TEXT NOT NULL DEFAULT '',
  city TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT '',
  state_code TEXT NOT NULL DEFAULT '',
  bank_account_name TEXT NOT NULL DEFAULT '',
  bank_account_number TEXT NOT NULL DEFAULT '',
  bank_ifsc TEXT NOT NULL DEFAULT '',
  bank_name TEXT NOT NULL DEFAULT '',
  bank_branch TEXT NOT NULL DEFAULT '',
  upi_id TEXT NOT NULL DEFAULT '',
  default_payment_mode TEXT NOT NULL DEFAULT '',
  payment_terms_days INTEGER NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_vendors_name_nocase ON vendors(lower(name));
CREATE INDEX IF NOT EXISTS idx_vendors_status ON vendors(status);`)
			return err
		},
	},
	// Phase 2 owns exactly one migration, v3: the request workflow. Two schema
	// decisions are load-bearing and must not be softened later.
	//
	// D1 — there are no drafts. `status` carries no DEFAULT, so every writer has
	// to name a status, and CHECK (status <> 'draft') makes a draft row
	// unrepresentable for the life of the table.
	//
	// A2 — `vendor_id` references the Phase-1V vendor master, while
	// `vendor_payee` survives beside it as the payee snapshot for reimbursements
	// and employee advances, which have no vendor row at all.
	{
		Version: 3,
		Name:    "requests",
		Up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS payment_requests (
  id INTEGER PRIMARY KEY,
  number TEXT NOT NULL UNIQUE,
  -- D1: there are no drafts. status has no DEFAULT, so every writer must name
  -- one, and the CHECK makes 'draft' unrepresentable for the life of the table.
  status TEXT NOT NULL CHECK (status <> 'draft'),
  treatment TEXT NOT NULL,
  type TEXT NOT NULL,
  recoverable_category TEXT NOT NULL DEFAULT '',
  -- A21: the category *code* lives in recoverable_category and drives the
  -- Phase-2 rules; recoverable_category_id stays NULL until Phase 4 creates
  -- recoverable_categories and back-fills it. The column deliberately carries
  -- no REFERENCES clause: with PRAGMA foreign_keys=ON (store.Open sets it),
  -- SQLite rejects *every* write to a child table whose parent table does not
  -- exist yet — even when the value is NULL — so a forward reference here would
  -- make CreateRequest fail until Phase 4 ships. Phase 4's v5 migration owns the
  -- link.
  recoverable_category_id INTEGER,
  project_id INTEGER REFERENCES projects(id),
  head_id INTEGER REFERENCES heads(id),
  vendor_id INTEGER REFERENCES vendors(id),
  vendor_payee TEXT NOT NULL DEFAULT '',
  short_title TEXT NOT NULL DEFAULT '',
  amount INTEGER NOT NULL,
  purpose TEXT NOT NULL,
  needed_by TEXT,
  invoice_no TEXT NOT NULL DEFAULT '',
  invoice_date TEXT,
  expense_date TEXT,
  advance_reason TEXT NOT NULL DEFAULT '',
  counterparty TEXT NOT NULL DEFAULT '',
  expected_return_date TEXT,
  repayment_notes TEXT NOT NULL DEFAULT '',
  urgent INTEGER NOT NULL DEFAULT 0,
  urgency_reason TEXT NOT NULL DEFAULT '',
  attachment_exception_reason TEXT NOT NULL DEFAULT '',
  requester_id INTEGER NOT NULL REFERENCES users(id),
  manager_id INTEGER NOT NULL REFERENCES users(id),
  approved_amount INTEGER,
  approved_by INTEGER REFERENCES users(id),
  approved_at DATETIME,
  decision_reason TEXT NOT NULL DEFAULT '',
  cancel_reason TEXT NOT NULL DEFAULT '',
  on_hold INTEGER NOT NULL DEFAULT 0,
  hold_reason TEXT NOT NULL DEFAULT '',
  processing_by INTEGER REFERENCES users(id),
  processing_at DATETIME,
  reminder_last_sent DATETIME,
  submitted_at DATETIME,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_requests_status ON payment_requests(status);
CREATE INDEX IF NOT EXISTS idx_requests_manager ON payment_requests(manager_id, status);
CREATE INDEX IF NOT EXISTS idx_requests_requester ON payment_requests(requester_id, status);
CREATE INDEX IF NOT EXISTS idx_requests_vendor ON payment_requests(vendor_id, created_at);

CREATE TABLE IF NOT EXISTS request_attachments (
  id INTEGER PRIMARY KEY,
  request_id INTEGER NOT NULL REFERENCES payment_requests(id),
  original_name TEXT NOT NULL,
  stored_path TEXT NOT NULL,
  mime_type TEXT,
  size_bytes INTEGER NOT NULL,
  uploaded_by INTEGER NOT NULL REFERENCES users(id),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_request_attachments_request ON request_attachments(request_id);

CREATE TABLE IF NOT EXISTS request_comments (
  id INTEGER PRIMARY KEY,
  request_id INTEGER NOT NULL REFERENCES payment_requests(id),
  author_id INTEGER NOT NULL REFERENCES users(id),
  body TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_request_comments_request ON request_comments(request_id);

CREATE TABLE IF NOT EXISTS request_number_seq (
  year TEXT PRIMARY KEY,
  last INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS app_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);
-- D6: the Phase-2 half of the Configuration screen. Phases 3-5 append keys.
INSERT INTO app_settings(key,value) VALUES
  ('number_prefix','PR'),
  ('number_year_mode','calendar'),
  ('number_width','6'),
  ('require_attachments','0'),
  ('attachment_max_mb','10'),
  ('urgency_mode','reason'),
  ('allow_approver_choice','1'),
  ('allow_direct_payments','0'),
  ('payment_modes','NEFT, RTGS, UPI, Cheque, Cash, Card, DD')
ON CONFLICT(key) DO NOTHING;
`); err != nil {
				return err
			}
			// G9: default approver per employee. Phase 1's v1 already adds this
			// column; the columnExists guard makes the double ownership harmless
			// and keeps this migration runnable against a database that skipped
			// it. Do not convert it to an unguarded ALTER — that fails hard.
			return addDefaultApproverColumn(tx)
		},
	},
	// Phase 3 owns exactly one migration, v4: it links a payment to the request
	// it settles. Three additive columns and one partial unique index, so the
	// historical ledger is untouched — every pre-Phase-3 payment keeps
	// request_id NULL and stays editable and voidable (X6).
	//
	// S9 — exactly one payment per request. The index is partial
	// (WHERE request_id IS NOT NULL) because SQLite treats NULLs as distinct
	// anyway; stating it keeps the intent readable and the index small.
	{
		Version: 4,
		Name:    "payments_request_linking",
		Up: func(tx *sql.Tx) error {
			adds := []struct{ col, ddl string }{
				{"request_id", `ALTER TABLE payments ADD COLUMN request_id INTEGER REFERENCES payment_requests(id)`},
				{"settlement", `ALTER TABLE payments ADD COLUMN settlement TEXT NOT NULL DEFAULT ''`},
				{"partial_reason", `ALTER TABLE payments ADD COLUMN partial_reason TEXT NOT NULL DEFAULT ''`},
			}
			for _, a := range adds {
				exists, err := columnExists(tx, "payments", a.col)
				if err != nil {
					return err
				}
				if exists {
					continue
				}
				if _, err := tx.Exec(a.ddl); err != nil {
					return err
				}
			}
			_, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_request ON payments(request_id) WHERE request_id IS NOT NULL`)
			return err
		},
	},
	// The reservation verbs entered the vocabulary with Phase 3, after v1 had
	// already seeded the starter roles on every existing database — and
	// seedSystemRoles only runs inside v1. Without this, an installed system
	// upgrades to a phase whose whole flow the Accounts role cannot start.
	//
	// It adds the two grants and nothing else: a re-seed would also reset every
	// deliberate change an administrator has made to a system role.
	{
		Version: 5,
		Name:    "accounts_reservation_grants",
		Up: func(tx *sql.Tx) error {
			for _, action := range []string{"reserve", "release"} {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO role_permissions(role_id,resource,action)
					SELECT id,'reservation',? FROM roles WHERE is_system=1 AND lower(name)='accounts'`, action); err != nil {
					return err
				}
			}
			return nil
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
			// G1: after approval it is too late to withdraw, so the requester
			// asks for cancellation and an approver decides.
			{"request", "cancel"},
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
			// G1 decides a requester's cancellation; G2 cancels an approved
			// request outright, with a reason.
			{"approval", "cancel"},
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
			// Phase 3 put taking a request out of the queue on its own verbs.
			// Accounts is the role that takes work; reassign — taking work off
			// somebody else — stays with an administrator.
			{"reservation", "reserve"}, {"reservation", "release"},
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
