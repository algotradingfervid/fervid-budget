package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "fervid-budget-test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	return s
}

// seedUser creates an active user and returns the row as the store reads it
// back. legacyRole fills the old users.role column ("admin" or "data_entry"):
// it decides which seeded role a new user starts with, and the last-admin guard
// still counts it, so fixtures keep choosing it explicitly.
func seedUser(t *testing.T, s *Store, email, name, legacyRole string) User {
	t.Helper()
	ctx := t.Context()
	id, err := s.CreateUser(ctx, email, name, "hash", legacyRole, true)
	if err != nil {
		t.Fatalf("CreateUser %s: %v", email, err)
	}
	u, err := s.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("UserByID %s: %v", email, err)
	}
	return u
}

// seedHead creates an active project holding one active head (due on the 5th,
// first in sort order) and returns the head id.
func seedHead(t *testing.T, s *Store, project, head string) int64 {
	t.Helper()
	ctx := t.Context()
	projectID, err := s.UpsertProject(ctx, 0, project, true, 1)
	if err != nil {
		t.Fatalf("UpsertProject %s: %v", project, err)
	}
	headID, err := s.UpsertHead(ctx, 0, projectID, head, "5", true, 1)
	if err != nil {
		t.Fatalf("UpsertHead %s: %v", head, err)
	}
	return headID
}

// sqlExpr is a raw SQL expression placed into a requestRow instead of a bound
// value, for example sqlNow.
type sqlExpr string

// sqlNow writes the database's CURRENT_TIMESTAMP, the way the store stamps rows.
const sqlNow = sqlExpr("CURRENT_TIMESTAMP")

// requestRow is a payment_requests row a test writes straight to the table,
// keyed by column name. Only the listed columns are written, so the schema's
// defaults fill everything else exactly as they would for the real insert.
type requestRow map[string]any

// insertRequestRow writes row into payment_requests and returns the new id.
// Tests that need a request in a state the workflow reaches only after several
// steps use it to plant the end state directly.
func insertRequestRow(t *testing.T, s *Store, row requestRow) int64 {
	t.Helper()
	cols := make([]string, 0, len(row))
	for col := range row {
		cols = append(cols, col)
	}
	sort.Strings(cols)
	marks := make([]string, len(cols))
	args := make([]any, 0, len(cols))
	for i, col := range cols {
		if expr, ok := row[col].(sqlExpr); ok {
			marks[i] = string(expr)
			continue
		}
		marks[i] = "?"
		args = append(args, row[col])
	}
	res, err := s.DB().ExecContext(t.Context(), `INSERT INTO payment_requests(`+strings.Join(cols, ",")+`) VALUES(`+strings.Join(marks, ",")+`)`, args...)
	if err != nil {
		t.Fatalf("insert request %v: %v", row["number"], err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// projectOfHead returns the project a head belongs to.
func projectOfHead(t *testing.T, s *Store, headID int64) int64 {
	t.Helper()
	var projectID int64
	if err := s.DB().QueryRowContext(t.Context(), `SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatalf("project of head %d: %v", headID, err)
	}
	return projectID
}

// recoverableCategoryID returns the id of a seeded recoverable category.
func recoverableCategoryID(t *testing.T, s *Store, code string) int64 {
	t.Helper()
	var id int64
	if err := s.DB().QueryRowContext(t.Context(), `SELECT id FROM recoverable_categories WHERE code=?`, code).Scan(&id); err != nil {
		t.Fatalf("recoverable category %q: %v", code, err)
	}
	return id
}

// seedActorAndHead creates one admin-legacy user (qa@example.com) and the
// Operations / Rent head most store tests post against.
func seedActorAndHead(t *testing.T, s *Store) (User, int64) {
	t.Helper()
	actor := seedUser(t, s, "qa@example.com", "QA User", "admin")
	return actor, seedHead(t, s, "Operations", "Rent")
}

// seedRequestParty creates the people a linked request needs and the
// Operations / Rent head: an Accounts actor, a requester, and a manager id.
func seedRequestParty(t *testing.T, s *Store) (accountant User, requester User, managerID, headID int64) {
	t.Helper()
	accountant = seedUser(t, s, "accounts@example.com", "Accounts", "admin")
	requester = seedUser(t, s, "requester@example.com", "Requester", "data_entry")
	manager := seedUser(t, s, "manager@example.com", "Manager", "admin")
	return accountant, requester, manager.ID, seedHead(t, s, "Operations", "Rent")
}

// seedApprovedRequest plants an approved vendor-invoice request directly, so
// payment tests do not depend on the request workflow. seq keeps the request
// number unique within a test.
func seedApprovedRequest(t *testing.T, s *Store, seq int, requesterID, managerID, headID, amount, approved int64) int64 {
	t.Helper()
	return insertRequestRow(t, s, requestRow{
		"number": fmt.Sprintf("PR-2026-%06d", seq), "status": "approved", "treatment": "budget", "type": "vendor_invoice",
		"project_id": projectOfHead(t, s, headID), "head_id": headID, "amount": amount,
		"purpose": "Office rent", "vendor_payee": "Acme Landlord", "requester_id": requesterID, "manager_id": managerID,
		"approved_amount": approved, "approved_by": managerID, "approved_at": sqlNow,
	})
}

func requestStatus(t *testing.T, s *Store, id int64) string {
	t.Helper()
	ctx := t.Context()
	var status string
	if err := s.DB().QueryRowContext(ctx, `SELECT status FROM payment_requests WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatalf("request status: %v", err)
	}
	return status
}

func requestProcessingBy(t *testing.T, s *Store, id int64) *int64 {
	t.Helper()
	ctx := t.Context()
	var pb sql.NullInt64
	if err := s.DB().QueryRowContext(ctx, `SELECT processing_by FROM payment_requests WHERE id=?`, id).Scan(&pb); err != nil {
		t.Fatalf("processing_by: %v", err)
	}
	if pb.Valid {
		v := pb.Int64
		return &v
	}
	return nil
}

// seedRequestActors creates a requester, an admin-legacy manager, and the
// Operations / Rent head.
func seedRequestActors(t *testing.T, s *Store) (requester User, manager User, headID int64) {
	t.Helper()
	requester = seedUser(t, s, "req@example.com", "Rhea Requester", "data_entry")
	manager = seedUser(t, s, "mgr@example.com", "Manav Manager", "admin")
	return requester, manager, seedHead(t, s, "Operations", "Rent")
}

// seedTestVendor inserts a Phase-1V vendor row directly; Phase 2 only needs its id.
func seedTestVendor(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	ctx := t.Context()
	res, err := s.DB().ExecContext(ctx, `INSERT INTO vendors(name,vendor_type,status,gstin) VALUES(?,'company','active','29AABCS1429B1ZQ')`, name)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// grantApprovalPermission gives a user a role carrying approval:approve.
func grantApprovalPermission(t *testing.T, s *Store, userID int64) {
	t.Helper()
	ctx := t.Context()
	var roleID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM roles WHERE lower(name)='manager'`).Scan(&roleID); err != nil {
		t.Fatalf("seeded Manager role missing: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES(?,?) ON CONFLICT DO NOTHING`, userID, roleID); err != nil {
		t.Fatal(err)
	}
}

// newRoleActor is the admin-legacy user the role and permission tests act as.
func newRoleActor(t *testing.T, s *Store) User {
	t.Helper()
	return seedUser(t, s, "roleadmin@example.com", "Role Admin", "admin")
}

// vendorActor is the admin-legacy user the vendor tests act as.
func vendorActor(t *testing.T, s *Store) User {
	t.Helper()
	return seedUser(t, s, "vendor-admin@example.com", "Vendor Admin", "admin")
}

// seedVendorRequest plants a request shaped the way the request form makes one
// for a vendor: vendor_id names the payee and vendor_payee is left empty. A
// fixture that filled vendor_payee would let a payee-name fallback pass for the
// vendor_id join that the vendor-totals tests are meant to check.
func seedVendorRequest(t *testing.T, s *Store, seq int, status string, requesterID, managerID, headID, vendorID, amount int64) int64 {
	t.Helper()
	return insertRequestRow(t, s, requestRow{
		"number": fmt.Sprintf("PR-2026-%06d", seq), "status": status, "treatment": "budget", "type": "vendor_invoice",
		"project_id": projectOfHead(t, s, headID), "head_id": headID, "vendor_id": vendorID, "amount": amount,
		"purpose": "Switchgear", "invoice_no": "INV-1", "requester_id": requesterID, "manager_id": managerID,
		"approved_amount": amount, "approved_by": managerID, "approved_at": sqlNow, "submitted_at": sqlNow,
	})
}

func seedVendorNamed(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	res, err := s.DB().Exec(`INSERT INTO vendors(name,vendor_type,status) VALUES(?,'company','active')`, name)
	if err != nil {
		t.Fatalf("seed vendor %q: %v", name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// newAccountsUser creates a data-entry-legacy user named after their email.
func newAccountsUser(t *testing.T, s *Store, email string) User {
	t.Helper()
	return seedUser(t, s, email, fmt.Sprintf("Accounts %s", email), "data_entry")
}

// backdateApproval moves a request's approval to the start of paidOn when it is
// later than that. The store refuses a payment dated before the approval, so a
// fixture that settles on a fixed past date must first make the approval
// precede it. Dates that are not a plain YYYY-MM-DD are left alone.
func backdateApproval(ctx context.Context, db *sql.DB, requestID int64, paidOn string) error {
	if len(paidOn) != 10 {
		return nil
	}
	start := paidOn + " 00:00:00"
	_, err := db.ExecContext(ctx, `UPDATE payment_requests SET approved_at=? WHERE id=? AND approved_at>?`, start, requestID, start)
	return err
}

// historicalSettlement records a payment on a fixed past date, back-dating the
// approval first (see backdateApproval). Tests about the payment-date rule
// itself call RecordPaymentForRequest directly instead.
func historicalSettlement(ctx context.Context, s *Store, actor User, id int64, in PaymentInput, settlement, reason string, attachment *AttachmentInput) (int64, error) {
	if err := backdateApproval(ctx, s.DB(), id, in.PaidOn); err != nil {
		return 0, err
	}
	return s.RecordPaymentForRequest(ctx, actor, id, in, settlement, reason, attachment)
}
