package store

import (
	"context"
	"database/sql"
	"strings"
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

func TestMigrationV1AddsDefaultApproverColumn(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	got, err := columnExists(tx, "users", "default_approver_id")
	if err != nil || !got {
		t.Fatalf("columnExists(users, default_approver_id) = %v, %v; want true, nil", got, err)
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

func TestMigrationV2CreatesVendors(t *testing.T) {
	s := newTestStore(t)
	for _, col := range []string{"id", "name", "display_name", "vendor_type", "status",
		"categories", "gstin", "pan", "msme_udyam", "tds_section", "tds_rate",
		"contact_person", "phone", "email", "address", "city", "state", "state_code",
		"bank_account_name", "bank_account_number", "bank_ifsc", "bank_name",
		"bank_branch", "upi_id", "default_payment_mode", "payment_terms_days",
		"notes", "created_at", "updated_at"} {
		var n int
		if err := s.DB().QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('vendors') WHERE name = ?`, col,
		).Scan(&n); err != nil || n != 1 {
			t.Fatalf("vendors.%s missing (n=%d err=%v)", col, n, err)
		}
	}
	if _, err := s.DB().Exec(
		`INSERT INTO vendors(name, vendor_type, status) VALUES('Acme','company','active')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO vendors(name, vendor_type, status) VALUES('acme','company','active')`); err == nil {
		t.Fatal("duplicate vendor name accepted; case-insensitive unique index missing")
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

// The reservation verbs entered the vocabulary with Phase 3, long after v1 had
// seeded the starter roles on every installed database — and seedSystemRoles
// runs only inside v1. Without the v5 back-fill, an upgraded system has an
// Accounts role that cannot take a request out of the queue, which is the first
// step of every payment this phase records.
func TestMigrationBackfillsAccountsReservationGrants(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	var roleID int64
	if err := s.DB().QueryRow(`SELECT id FROM roles WHERE lower(name)='accounts'`).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	grants, _, err := s.RolePermissions(ctx, roleID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []Grant{{"reservation", "reserve"}, {"reservation", "release"}} {
		if !hasGrant(grants, want) {
			t.Fatalf("seeded Accounts role is missing %+v: %+v", want, grants)
		}
	}

	// Simulate the installed database: the grants are absent and the schema is
	// one version behind. Re-running migrate must put them back without
	// resetting anything else about the role.
	if _, err := s.DB().Exec(`DELETE FROM role_permissions WHERE role_id=? AND resource='reservation'`, roleID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`INSERT OR IGNORE INTO role_permissions(role_id,resource,action) VALUES(?,'vendor','view')`, roleID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`PRAGMA user_version = 4`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.DB()); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	grants, _, err = s.RolePermissions(ctx, roleID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []Grant{{"reservation", "reserve"}, {"reservation", "release"}, {"vendor", "view"}} {
		if !hasGrant(grants, want) {
			t.Fatalf("after the back-fill the Accounts role is missing %+v: %+v", want, grants)
		}
	}
}

func hasGrant(grants []Grant, want Grant) bool {
	for _, g := range grants {
		if g == want {
			return true
		}
	}
	return false
}

// TestMigrationV8RebuildsPaymentsPreservingRowsIndexesAndForeignKeys simulates
// an installed v7 database — payments with head_id NOT NULL, real historical
// rows (request_id NULL), a linked settlement, and a child payment_attachments
// row — and proves the v8 rebuild: head_id loses NOT NULL, every row survives
// with its id, the attachment still resolves, the partial unique index still
// enforces one-payment-per-request, every index is recreated, and both foreign
// keys still bite. The child row is the important part: PRAGMA foreign_keys
// rides on the DSN and cannot change inside the migration's transaction, so v8
// leans on PRAGMA defer_foreign_keys — this test fails if that mechanism ever
// stops carrying the child rows across the DROP/RENAME.
func TestMigrationV8RebuildsPaymentsPreservingRowsIndexesAndForeignKeys(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)

	// Reconstruct the exact pre-v8 shape: schemaSQL's table plus the three v4
	// columns and all four indexes. payment_attachments is empty at this
	// point, so the DROP is legal even with foreign keys enforced.
	if _, err := s.DB().Exec(`
DROP TABLE payments;
CREATE TABLE payments (
  id INTEGER PRIMARY KEY,
  head_id INTEGER NOT NULL REFERENCES heads(id),
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
CREATE INDEX idx_payments_head ON payments(head_id);
CREATE INDEX idx_payments_paid_on ON payments(paid_on);
CREATE INDEX idx_payments_voided ON payments(voided_at);
CREATE UNIQUE INDEX idx_payments_request ON payments(request_id) WHERE request_id IS NOT NULL;
`); err != nil {
		t.Fatalf("reconstruct pre-v8 payments: %v", err)
	}
	if _, err := s.DB().Exec(`INSERT INTO payments(id,head_id,paid_on,amount,vendor_payee,entered_by) VALUES(1,?,?,?,?,?)`,
		headID, "2025-05-10", 7777, "Historical Vendor", acc.ID); err != nil {
		t.Fatalf("seed historical payment: %v", err)
	}
	if _, err := s.DB().Exec(`INSERT INTO payments(id,head_id,paid_on,amount,vendor_payee,entered_by,request_id,settlement) VALUES(2,?,?,?,?,?,?, 'settled')`,
		headID, "2026-06-15", 500000, "Acme Landlord", acc.ID, reqID); err != nil {
		t.Fatalf("seed linked payment: %v", err)
	}
	if _, err := s.DB().Exec(`INSERT INTO payment_attachments(payment_id,original_name,stored_path,size_bytes,uploaded_by) VALUES(2,'advice.pdf','/tmp/advice.pdf',9,?)`, acc.ID); err != nil {
		t.Fatalf("seed child attachment: %v", err)
	}
	if _, err := s.DB().Exec(`PRAGMA user_version = 7`); err != nil {
		t.Fatal(err)
	}

	if err := migrate(s.DB()); err != nil {
		t.Fatalf("v8 rebuild: %v", err)
	}

	// head_id is nullable now.
	var notnull int
	if err := s.DB().QueryRow(`SELECT "notnull" FROM pragma_table_info('payments') WHERE name='head_id'`).Scan(&notnull); err != nil {
		t.Fatal(err)
	}
	if notnull != 0 {
		t.Fatalf("payments.head_id notnull = %d after v8, want 0", notnull)
	}
	// Every row survived with its id and its linkage.
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("payments rows after rebuild = %d, want 2", count)
	}
	var amount int64
	var reqRef sql.NullInt64
	var settlement string
	if err := s.DB().QueryRow(`SELECT amount, request_id, settlement FROM payments WHERE id=1`).Scan(&amount, &reqRef, &settlement); err != nil {
		t.Fatal(err)
	}
	if amount != 7777 || reqRef.Valid || settlement != "" {
		t.Fatalf("historical payment changed by rebuild: amount=%d request_id=%v settlement=%q", amount, reqRef, settlement)
	}
	if err := s.DB().QueryRow(`SELECT amount, request_id, settlement FROM payments WHERE id=2`).Scan(&amount, &reqRef, &settlement); err != nil {
		t.Fatal(err)
	}
	if amount != 500000 || !reqRef.Valid || reqRef.Int64 != reqID || settlement != "settled" {
		t.Fatalf("linked payment changed by rebuild: amount=%d request_id=%v settlement=%q", amount, reqRef, settlement)
	}
	// The child row still resolves through its foreign key.
	var joined int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM payment_attachments pa JOIN payments p ON p.id=pa.payment_id WHERE pa.payment_id=2`).Scan(&joined); err != nil {
		t.Fatal(err)
	}
	if joined != 1 {
		t.Fatalf("attachment orphaned by rebuild: joined=%d, want 1", joined)
	}
	// Every index was recreated.
	for _, idx := range []string{"idx_payments_head", "idx_payments_paid_on", "idx_payments_voided", "idx_payments_request"} {
		var n int
		if err := s.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=? AND tbl_name='payments'`, idx).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("index %s missing after rebuild", idx)
		}
	}
	// S9 still holds: a second payment on the same request is refused by the
	// partial unique index …
	if _, err := s.DB().Exec(`INSERT INTO payments(head_id,paid_on,amount,entered_by,request_id,settlement) VALUES(?,?,?,?,?, 'settled')`,
		headID, "2026-06-16", 1000, acc.ID, reqID); err == nil {
		t.Fatal("second payment linked to the same request was allowed after rebuild (S9 broken)")
	}
	// … while NULL request_id historicals still multiply freely.
	if _, err := s.DB().Exec(`INSERT INTO payments(head_id,paid_on,amount,entered_by) VALUES(?,?,?,?)`, headID, "2026-06-17", 2000, acc.ID); err != nil {
		t.Fatalf("historical NULL-request insert after rebuild: %v", err)
	}
	// The point of the rebuild: a NULL head is now representable …
	if _, err := s.DB().Exec(`INSERT INTO payments(head_id,paid_on,amount,entered_by) VALUES(NULL,?,?,?)`, "2026-06-18", 3000, acc.ID); err != nil {
		t.Fatalf("NULL head_id insert after rebuild: %v", err)
	}
	// … and both foreign keys still bite on every pooled connection.
	if _, err := s.DB().Exec(`INSERT INTO payments(head_id,paid_on,amount,entered_by) VALUES(999999,?,?,?)`, "2026-06-19", 4000, acc.ID); err == nil {
		t.Fatal("dangling head_id accepted after rebuild; the foreign key was lost")
	}
	if _, err := s.DB().Exec(`INSERT INTO payment_attachments(payment_id,original_name,stored_path,size_bytes,uploaded_by) VALUES(999999,'x','/tmp/x',1,?)`, acc.ID); err == nil {
		t.Fatal("dangling payment_id accepted after rebuild; the child foreign key was lost")
	}

	// Idempotence: re-running v8 against the already-rebuilt table is a no-op.
	if _, err := s.DB().Exec(`PRAGMA user_version = 7`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.DB()); err != nil {
		t.Fatalf("re-running v8: %v", err)
	}
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("payments rows after idempotent re-run = %d, want 4", count)
	}
}

func TestMigrationV7CreatesNotificationTablesAndSeedsTwelveEvents(t *testing.T) { // G19, G20
	s := newTestStore(t)
	// v7 creates notification_settings and notifications; app_settings is Phase 2's (v3).
	for _, table := range []string{"notification_settings", "notifications"} {
		var name string
		if err := s.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("table %q missing: %v", table, err)
		}
	}
	// Scoped to v7's own sort_order block, because notification_settings is
	// append-only across migrations: v9 seeds nine more events (F-F-06) at
	// sort_order 13-21, and a count of the whole table would make this test about
	// the size of the catalogue rather than about what v7 does.
	// TestMigrationV9SeedsTheNineMissingEvents owns the total.
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM notification_settings WHERE sort_order<=?`,
		len(defaultNotificationSettings)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 12 {
		t.Fatalf("v7-seeded events = %d, want 12", count)
	}
	want := []string{
		"request_submitted", "request_edited", "request_returned", "request_rejected",
		"request_approved", "request_urgent", "request_on_hold", "request_cancellation_requested",
		"payment_settled", "payment_partial_review", "reminder_pending", "reminder_stale_reservation",
	}
	for _, event := range want {
		var subject, body string
		if err := s.DB().QueryRow(`SELECT subject_template,body_template FROM notification_settings WHERE event=?`, event).Scan(&subject, &body); err != nil {
			t.Fatalf("event %q missing: %v", event, err)
		}
		if subject == "" || body == "" {
			t.Fatalf("event %q seeded with an empty template", event)
		}
		// G20: the admin-facing vocabulary is {{token}}, never Go template syntax.
		if strings.Contains(subject+body, "{{.") || strings.Contains(subject+body, "{{money") {
			t.Fatalf("event %q still uses Go template syntax: %q / %q", event, subject, body)
		}
	}
	// The unread-count index the shell bell depends on exists.
	var idx string
	if err := s.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_notifications_user_unread'`).Scan(&idx); err != nil {
		t.Fatalf("unread index missing: %v", err)
	}
	var version int
	if err := s.DB().QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 7 {
		t.Fatalf("user_version = %d, want >= 7", version)
	}
}

// TestMigrationV10AddsAndBackfillsPaymentsVendorID simulates an installed v9
// database — payments with no vendor_id, one payment linked to a vendor request
// and one free-standing historical payment — and proves v10: the column arrives
// with its foreign key live, the linked payment inherits the vendor from the
// request it settles, the unlinked one is left NULL because nothing can say who
// it was paid to, the index exists, and a second run changes nothing.
//
// It is deliberately NOT a table rebuild, which is the whole reason this test is
// short next to v8's. `ALTER TABLE … ADD COLUMN … REFERENCES` is legal with
// PRAGMA foreign_keys on precisely because the new column defaults to NULL, so
// none of v8's stash-and-restore machinery is needed. The assertions at the end
// are what prove the shortcut is sound rather than merely quiet.
func TestMigrationV10AddsAndBackfillsPaymentsVendorID(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, requester, mgrID, headID := seedRequestParty(t, s, ctx)
	vendorID := seedVendorNamed(t, s, "Sundaram Electricals Pvt Ltd")
	reqID := seedVendorRequest(t, s, ctx, 7, "approved", requester.ID, mgrID, headID, vendorID, 500000)

	// Reconstruct the pre-v10 shape: v8's rebuilt table, minus vendor_id.
	// payment_attachments is empty on a fresh store, so the DROP is legal with
	// foreign keys enforced.
	if _, err := s.DB().Exec(`
DROP TABLE payments;
CREATE TABLE payments (
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
CREATE INDEX idx_payments_head ON payments(head_id);
CREATE INDEX idx_payments_paid_on ON payments(paid_on);
CREATE INDEX idx_payments_voided ON payments(voided_at);
CREATE UNIQUE INDEX idx_payments_request ON payments(request_id) WHERE request_id IS NOT NULL;
`); err != nil {
		t.Fatalf("reconstruct pre-v10 payments: %v", err)
	}
	if _, err := s.DB().Exec(`INSERT INTO payments(id,head_id,paid_on,amount,vendor_payee,entered_by,request_id,settlement)
		VALUES(1,?,?,?,?,?,?,'settled')`, headID, "2025-06-15", 500000, "Sundaram Electricals Pvt Ltd", acc.ID, reqID); err != nil {
		t.Fatalf("seed linked payment: %v", err)
	}
	if _, err := s.DB().Exec(`INSERT INTO payments(id,head_id,paid_on,amount,vendor_payee,entered_by)
		VALUES(2,?,?,?,?,?)`, headID, "2025-05-10", 7777, "Somebody Historical", acc.ID); err != nil {
		t.Fatalf("seed historical payment: %v", err)
	}
	if _, err := s.DB().Exec(`PRAGMA user_version = 9`); err != nil {
		t.Fatal(err)
	}

	if err := migrate(s.DB()); err != nil {
		t.Fatalf("v10: %v", err)
	}

	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('payments') WHERE name='vendor_id'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("payments.vendor_id missing after v10 (n=%d err=%v)", n, err)
	}
	var linked sql.NullInt64
	if err := s.DB().QueryRow(`SELECT vendor_id FROM payments WHERE id=1`).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if !linked.Valid || linked.Int64 != vendorID {
		t.Fatalf("linked payment vendor_id = %v, want %d — the back-fill did not follow request_id", linked, vendorID)
	}
	var historical sql.NullInt64
	if err := s.DB().QueryRow(`SELECT vendor_id FROM payments WHERE id=2`).Scan(&historical); err != nil {
		t.Fatal(err)
	}
	if historical.Valid {
		t.Fatalf("historical payment vendor_id = %v, want NULL — a payment that settles no request has no vendor to inherit", historical)
	}
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_payments_vendor' AND tbl_name='payments'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("idx_payments_vendor missing after v10 (n=%d err=%v)", n, err)
	}
	// The foreign key really is live on the added column, on a pooled connection.
	if _, err := s.DB().Exec(`INSERT INTO payments(head_id,paid_on,amount,entered_by,vendor_id) VALUES(?,?,?,?,999999)`,
		headID, "2025-06-20", 100, acc.ID); err == nil {
		t.Fatal("dangling vendor_id accepted; ADD COLUMN … REFERENCES did not carry the constraint")
	}
	// And NULL stays representable, which is what makes the whole approach legal.
	if _, err := s.DB().Exec(`INSERT INTO payments(head_id,paid_on,amount,entered_by) VALUES(?,?,?,?)`,
		headID, "2025-06-21", 100, acc.ID); err != nil {
		t.Fatalf("NULL vendor_id refused: %v", err)
	}

	// Idempotence: re-running v10 adds nothing and overwrites nothing. The
	// deliberate value on the historical row is the interesting part — the
	// back-fill only ever fills NULLs, so a later correction survives.
	if _, err := s.DB().Exec(`UPDATE payments SET vendor_id=? WHERE id=2`, vendorID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`PRAGMA user_version = 9`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.DB()); err != nil {
		t.Fatalf("re-running v10: %v", err)
	}
	if err := s.DB().QueryRow(`SELECT vendor_id FROM payments WHERE id=2`).Scan(&historical); err != nil {
		t.Fatal(err)
	}
	if !historical.Valid || historical.Int64 != vendorID {
		t.Fatalf("re-running v10 discarded a deliberate vendor_id: %v", historical)
	}
}
