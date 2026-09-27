package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
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

// seedApprovedRequest inserts an approved payment_requests row directly (its
// schema is fixed by overview §3) so Phase-3 store tests do not depend on the
// Phase-2 request methods. seq keeps the request number unique within a test.
func seedApprovedRequest(t *testing.T, s *Store, seq int, requesterID, managerID, headID, amount, approved int64) int64 {
	t.Helper()
	ctx := t.Context()
	var projectID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatalf("head project: %v", err)
	}
	res, err := s.DB().ExecContext(ctx, `INSERT INTO payment_requests
		(number,status,treatment,type,project_id,head_id,amount,purpose,vendor_payee,requester_id,manager_id,approved_amount,approved_by,approved_at)
		VALUES(?,'approved','budget','vendor_invoice',?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), projectID, headID, amount, "Office rent", "Acme Landlord", requesterID, managerID, approved, managerID)
	if err != nil {
		t.Fatalf("seed approved request: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
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

// seedVendorRequest inserts a payment_requests row shaped the way the request
// form makes one for a vendor: vendor_id names the payee and vendor_payee is
// left empty. Reading the snapshot on such a row is the trap PROGRESS.md
// records, and it is exactly the trap a test about vendor totals must not fall
// into — a fixture that fills vendor_payee would make the payee-name fallback
// look like the vendor_id join working.
func seedVendorRequest(t *testing.T, s *Store, seq int, status string, requesterID, managerID, headID, vendorID, amount int64) int64 {
	t.Helper()
	ctx := t.Context()
	var projectID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatalf("head project: %v", err)
	}
	res, err := s.DB().ExecContext(ctx, `INSERT INTO payment_requests
		(number,status,treatment,type,project_id,head_id,vendor_id,amount,purpose,invoice_no,
		 requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?,?,'budget','vendor_invoice',?,?,?,?,'Switchgear','INV-1',?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), status, projectID, headID, vendorID, amount,
		requesterID, managerID, amount, managerID)
	if err != nil {
		t.Fatalf("seed vendor request: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
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

// Existing workflow fixtures use fixed historical payment dates. Model an
// approval that really happened before that historical settlement. Security
// date tests call RecordPaymentForRequest directly and never use this helper.
func historicalSettlement(s *Store, ctx context.Context, actor User, id int64, in PaymentInput, settlement, reason string, attachment *AttachmentInput) (int64, error) {
	if len(in.PaidOn) == 10 {
		if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET approved_at=? WHERE id=? AND approved_at>?`, in.PaidOn+" 00:00:00", id, in.PaidOn+" 00:00:00"); err != nil {
			return 0, err
		}
	}
	return s.RecordPaymentForRequest(ctx, actor, id, in, settlement, reason, attachment)
}
