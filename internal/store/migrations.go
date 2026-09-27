// migrations.go upgrades an existing database one step at a time. Read the
// comment on migrations before adding one.

package store

import (
	"context"
	"database/sql"
	"fmt"
)

// migration is one upgrade step: Up moves the database to Version.
type migration struct {
	Version int
	Name    string
	Up      func(*sql.Tx) error
}

// migrations upgrade an existing database one step at a time. The schema in
// schema.go is version 0; each entry below moves the database to its Version.
// migrate runs every entry newer than the database's PRAGMA user_version, each
// in its own transaction, and records the new version in the same transaction.
//
// To add a migration:
//
//  1. Append {Version: <last+1>, Name: "...", Up: upSomething}. Never renumber,
//     reorder or edit a shipped entry: installed databases have already run it.
//  2. Make Up safe to run twice: CREATE ... IF NOT EXISTS, addColumnIfMissing
//     for a new column, INSERT ... ON CONFLICT DO NOTHING for seed rows.
//  3. After v1, never overwrite rows an administrator can edit (role grants,
//     notification rules). Only add new rows, or update rows that still hold
//     exactly the old default.
//
// What each version did:
//
//	v1  roles, role grants, data scopes and user roles; the four starter roles
//	v2  vendor master
//	v3  payment requests, attachments, comments, app settings
//	v4  link a payment to the request it settles
//	v5  give the Accounts role reserve and release
//	v6  recoverable categories
//	v7  notification rules and in-app notifications
//	v8  allow payments without a budget head (table rebuild)
//	v9  nine more notification rules
//	v10 payments.vendor_id
//	v11 notification rules for reassignment and approver cancellation
//	v12 let the Manager role open request attachments
//	v13 reword the reassignment notification
//	v14 settlement notifications show the amount actually paid
//	v15 payment_requests.concern_open flag
//	v16 recovery events
//	v17 request installments
//	v18 password reset tokens and throttle
//	v19 session revocation (users.session_version)
//	v20 budget lines
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
	// v2: the vendor master. Bank columns live on the vendor row; they are kept
	// from the wrong eyes by the store never selecting them for a caller without
	// vendor_bank:view (see vendors.go). payments.vendor_payee stays as the payee
	// snapshot for payments with no vendor row; v10 adds vendor_id beside it.
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
	// v3: the request workflow. There are no drafts: status has no DEFAULT and a
	// CHECK forbids 'draft'. vendor_id points at the vendor master, and
	// vendor_payee is the payee snapshot for requests with no vendor row.
	//
	// The SQL comment on recoverable_category_id below is shipped text and is
	// left as it was. Its last sentence is out of date: the recoverable
	// categories table and the back-fill come from v6, not v5.
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
			// v1 already adds this column; the guard makes adding it here too
			// harmless, and covers a database that skipped v1's copy.
			return addDefaultApproverColumn(tx)
		},
	},
	// v4: link a payment to the request it settles. Older payments keep
	// request_id NULL. The partial unique index allows at most one payment per
	// request.
	{
		Version: 4,
		Name:    "payments_request_linking",
		Up: func(tx *sql.Tx) error {
			adds := []struct{ col, def string }{
				{"request_id", `INTEGER REFERENCES payment_requests(id)`},
				{"settlement", `TEXT NOT NULL DEFAULT ''`},
				{"partial_reason", `TEXT NOT NULL DEFAULT ''`},
			}
			for _, a := range adds {
				if err := addColumnIfMissing(tx, "payments", a.col, a.def); err != nil {
					return err
				}
			}
			_, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_request ON payments(request_id) WHERE request_id IS NOT NULL`)
			return err
		},
	},
	// v5: give the Accounts role reservation:reserve and reservation:release on
	// databases v1 seeded before those actions existed. It adds the two grants
	// and nothing else, so an administrator's changes to the role survive.
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
	// v6: the recoverable categories table that
	// payment_requests.recoverable_category_id points at, and the back-fill of
	// that column for existing requests.
	{
		Version: 6,
		Name:    "recoverable_categories",
		Up:      upRecoverableCategories,
	},
	// v7: notification_settings (the twelve admin-editable event rules) and the
	// in-app notifications table behind the bell.
	{
		Version: 7,
		Name:    "notifications",
		Up:      upNotifications,
	},
	// v8: a recoverable request has no budget head, so the payment that settles
	// it cannot carry a NOT NULL head_id. SQLite cannot drop NOT NULL in place,
	// so the table is rebuilt (see upPaymentsHeadNullable).
	{
		Version: 8,
		Name:    "payments_head_nullable",
		Up:      upPaymentsHeadNullable,
	},
	{
		// v9: rules for nine more workflow events. Adds rows only, so an
		// administrator's edits to v7's twelve survive.
		Version: 9,
		Name:    "notification_events_audit",
		Up:      upAuditNotificationEvents,
	},
	// v10: record which vendor each payment went to.
	{
		Version: 10,
		Name:    "payments_vendor_id",
		Up:      upPaymentsVendorID,
	},
	// v11: notify the new approver on reassignment, and the requester when an
	// approver cancels their request.
	{
		Version: 11,
		Name:    "notification_events_reassignment",
		Up:      upReassignmentNotificationEvents,
	},
	// v12: let the Manager role open the attachments on requests it approves.
	{
		Version: 12,
		Name:    "approver_can_view_attachments",
		Up:      upApproverAttachmentView,
	},
	// v13: the reassignment notice no longer says "needs your approval", which
	// was wrong for most statuses a request can be reassigned in.
	{
		Version: 13,
		Name:    "notification_reassigned_wording",
		Up:      upReassignedNotificationWording,
	},
	// v14: the four settlement notices used {{amount}} (the amount requested)
	// where they meant the amount paid. Rewrites only rules still on the old
	// default text.
	{
		Version: 14,
		Name:    "notification_paid_amount",
		Up:      upPaidAmountTemplates,
	},
	// v15: concern_open marks a partial payment the approver raised a concern
	// about that Accounts has not answered yet. It is a flag, not a status.
	{
		Version: 15,
		Name:    "request_concern_open",
		Up:      upRequestConcernOpen,
	},
	{Version: 16, Name: "recovery_events", Up: upRecoveryEvents},
	{Version: 17, Name: "request_installments", Up: upInstallments},
	{Version: 18, Name: "password_reset", Up: upPasswordReset},
	{Version: 19, Name: "session_revocation", Up: upSessionRevocation},
	{Version: 20, Name: "budget_lines", Up: upBudgetLines},
}

// upRequestConcernOpen adds payment_requests.concern_open.
func upRequestConcernOpen(tx *sql.Tx) error {
	return addColumnIfMissing(tx, "payment_requests", "concern_open", "INTEGER NOT NULL DEFAULT 0")
}

// upApproverAttachmentView grants attachment:view to the system Manager role,
// so an approver can open the invoice on the request they are deciding.
// Custom roles are left alone. It does nothing if the grant is already there
// or the Manager role has been renamed or removed.
func upApproverAttachmentView(tx *sql.Tx) error {
	var roleID int64
	err := tx.QueryRow(`SELECT id FROM roles WHERE lower(name)='manager' AND is_system=1`).Scan(&roleID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`INSERT INTO role_permissions(role_id,resource,action) VALUES(?,'attachment','view')
		 ON CONFLICT(role_id,resource,action) DO NOTHING`, roleID)
	return err
}

// upPaymentsVendorID records which vendor each payment went to. It adds
// payments.vendor_id (adding a column that defaults to NULL is allowed with
// foreign keys on, so no rebuild is needed), back-fills it from the request
// each payment settles, touching only rows still NULL, and indexes it for
// vendorPaidThisYear. Payments that settle no request stay NULL and are
// matched by payee name instead. Every step is safe to re-run.
func upPaymentsVendorID(tx *sql.Tx) error {
	if err := addColumnIfMissing(tx, "payments", "vendor_id", "INTEGER REFERENCES vendors(id)"); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE payments
		SET vendor_id = (SELECT r.vendor_id FROM payment_requests r WHERE r.id = payments.request_id)
		WHERE vendor_id IS NULL AND request_id IS NOT NULL`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_payments_vendor ON payments(vendor_id)`)
	return err
}

// upPaymentsHeadNullable rebuilds payments so head_id is nullable, preserving
// every row (including historical payments with request_id NULL), every column
// v4 added, and every index, including the partial unique index
// idx_payments_request that allows at most one payment per request.
//
// Three constraints shape it:
//
//   - PRAGMA foreign_keys rides on the DSN (store.Open) and cannot change
//     inside a transaction, and the migration runner wraps each Up in one.
//     payment_attachments.payment_id references payments, so the rebuild must
//     happen with enforcement live. PRAGMA defer_foreign_keys CAN be set
//     inside a transaction — it postpones any transient violation to COMMIT
//     and resets itself there — and is kept on as the belt. It is not enough
//     on its own, though: SQLite tracks deferred violations as a counter that
//     only DML adjusts, so the violations counted when DROP TABLE implicitly
//     deletes the referenced rows are never cancelled by the later RENAME
//     (DDL), and COMMIT would still refuse. The braces are therefore to stash
//     the payment_attachments rows, empty that table so the DROP sees no
//     child reference and the counter never moves, and restore the rows —
//     ids included — once the rebuilt table is back under its own name.
//
//   - The order is create-new → copy → drop-old → rename-new. Renaming the
//     old table aside first would not work: ALTER TABLE RENAME rewrites the
//     REFERENCES clauses in child tables (verified against this driver, which
//     does so even under PRAGMA legacy_alter_table=ON), so
//     payment_attachments would end up pointing at the temporary name.
//
//   - Columns are copied explicitly by name, never SELECT *, so the copy
//     cannot silently reorder if the source schema drifts.
//
// Guarded by columnNotNull so a re-run against an already-rebuilt table is a
// no-op.
func upPaymentsHeadNullable(tx *sql.Tx) error {
	notNull, err := columnNotNull(tx, "payments", "head_id")
	if err != nil {
		return err
	}
	if !notNull {
		return nil // already rebuilt; re-running is a no-op
	}
	if _, err := tx.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
CREATE TABLE payment_attachments_v8_stash AS
  SELECT id,payment_id,original_name,stored_path,mime_type,size_bytes,uploaded_by,created_at
  FROM payment_attachments;
DELETE FROM payment_attachments;
CREATE TABLE payments_v8 (
  id INTEGER PRIMARY KEY,
  head_id INTEGER REFERENCES heads(id),
  paid_on TEXT NOT NULL,
  amount INTEGER NOT NULL,
  vendor_payee TEXT,
  payment_mode TEXT,
  invoice_no TEXT,
  reference_no TEXT,
  remarks TEXT,
  entered_by INTEGER NOT NULL REFERENCES users(id),
  updated_by INTEGER REFERENCES users(id),
  voided_by INTEGER REFERENCES users(id),
  void_reason TEXT,
  voided_at DATETIME,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  request_id INTEGER REFERENCES payment_requests(id),
  settlement TEXT NOT NULL DEFAULT '',
  partial_reason TEXT NOT NULL DEFAULT ''
);
INSERT INTO payments_v8
  (id,head_id,paid_on,amount,vendor_payee,payment_mode,invoice_no,reference_no,remarks,
   entered_by,updated_by,voided_by,void_reason,voided_at,created_at,updated_at,
   request_id,settlement,partial_reason)
  SELECT id,head_id,paid_on,amount,vendor_payee,payment_mode,invoice_no,reference_no,remarks,
   entered_by,updated_by,voided_by,void_reason,voided_at,created_at,updated_at,
   request_id,settlement,partial_reason
  FROM payments;
DROP TABLE payments;
ALTER TABLE payments_v8 RENAME TO payments;
INSERT INTO payment_attachments
  (id,payment_id,original_name,stored_path,mime_type,size_bytes,uploaded_by,created_at)
  SELECT id,payment_id,original_name,stored_path,mime_type,size_bytes,uploaded_by,created_at
  FROM payment_attachments_v8_stash;
DROP TABLE payment_attachments_v8_stash;
CREATE INDEX IF NOT EXISTS idx_payments_head ON payments(head_id);
CREATE INDEX IF NOT EXISTS idx_payments_paid_on ON payments(paid_on);
CREATE INDEX IF NOT EXISTS idx_payments_voided ON payments(voided_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_request ON payments(request_id) WHERE request_id IS NOT NULL;
`); err != nil {
		return err
	}
	return nil
}

// addDefaultApproverColumn adds users.default_approver_id, the approver
// pre-selected on a user's new requests. Both v1 and v3 call it.
func addDefaultApproverColumn(tx *sql.Tx) error {
	return addColumnIfMissing(tx, "users", "default_approver_id", "INTEGER REFERENCES users(id)")
}

// systemRoleDef is one seeded starter role.
type systemRoleDef struct {
	Name        string
	Description string
	Grants      []Grant
	Scopes      []ScopeGrant
}

// systemRoleDefaults are the seeded starter roles. Admin holds every action in
// the vocabulary.
var systemRoleDefaults = []systemRoleDef{
	{
		Name:        "Requester",
		Description: "Raise and manage your own payment requests.",
		Grants: []Grant{
			{"request", "view"}, {"request", "create"}, {"request", "edit"},
			{"request", "withdraw"}, {"request", "reraise"}, {"request", "comment"},
			// After approval it is too late to withdraw, so the requester asks
			// for cancellation and an approver decides.
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
			// Decides a requester's cancellation, or cancels an approved
			// request outright with a reason.
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
			// Accounts takes work from the queue; reassigning work away from
			// somebody else stays with an administrator.
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

// adminGrants is every (resource, action) pair in the vocabulary, in
// resourceOrder.
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
// its grants and scopes to the canonical defaults. Only v1 calls it; later
// migrations must add grants instead, so an administrator's changes survive.
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

// backfillUserRoles gives every existing user the system role that matches
// their legacy users.role (see systemRoleForLegacy).
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
		var roleID int64
		if err := tx.QueryRow(`SELECT id FROM roles WHERE lower(name)=lower(?)`, systemRoleForLegacy(one.role)).Scan(&roleID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO user_roles(user_id,role_id) VALUES(?,?) ON CONFLICT(user_id,role_id) DO NOTHING`, one.id, roleID); err != nil {
			return err
		}
	}
	return nil
}

// systemRoleForLegacy maps the legacy users.role value to a starter role name:
// "admin" becomes Admin, anything else Accounts.
func systemRoleForLegacy(legacyRole string) string {
	if legacyRole == "admin" {
		return "Admin"
	}
	return "Accounts"
}

// assignDefaultRoleTx gives a freshly created user the starter role matching
// their legacy role (see systemRoleForLegacy), so every user resolves to a
// concrete permission set. It does nothing if that role no longer exists.
func assignDefaultRoleTx(ctx context.Context, tx *sql.Tx, userID int64, legacyRole string) error {
	var roleID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE lower(name)=lower(?)`, systemRoleForLegacy(legacyRole)).Scan(&roleID)
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

// addColumnIfMissing runs ALTER TABLE table ADD COLUMN col def unless the
// column already exists, so the migration is safe to re-run. def is the column
// definition exactly as it would follow the column name, for example
// "INTEGER NOT NULL DEFAULT 0". table, col and def must be literals from the
// migration itself, never user input.
func addColumnIfMissing(tx *sql.Tx, table, col, def string) error {
	has, err := columnExists(tx, table, col)
	if err != nil || has {
		return err
	}
	_, err = tx.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, col, def))
	return err
}

// tableColumn reads PRAGMA table_info for table and reports whether col exists
// and whether it is NOT NULL.
func tableColumn(tx *sql.Tx, table, col string) (found, notNull bool, err error) {
	rows, err := tx.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, false, err
		}
		if name == col {
			return true, notnull == 1, nil
		}
	}
	return false, false, rows.Err()
}

// columnNotNull reports whether table.col carries a NOT NULL constraint, and
// is an error if the column does not exist. It is the re-run guard for rebuild
// migrations (v8): once the rebuild has happened the constraint is gone and
// the migration is a no-op.
func columnNotNull(tx *sql.Tx, table, col string) (bool, error) {
	found, notNull, err := tableColumn(tx, table, col)
	if err != nil {
		return false, err
	}
	if !found {
		return false, fmt.Errorf("column %s.%s not found", table, col)
	}
	return notNull, nil
}

// columnExists reports whether table.col exists. It guards additive column
// migrations (see addColumnIfMissing).
func columnExists(tx *sql.Tx, table, col string) (bool, error) {
	found, _, err := tableColumn(tx, table, col)
	return found, err
}
