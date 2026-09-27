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

func seedActorAndHead(t *testing.T, s *Store, ctx context.Context) (User, int64) {
	t.Helper()
	userID, err := s.CreateUser(ctx, "qa@example.com", "QA User", "hash", "admin", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	actor, err := s.UserByID(ctx, userID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	projectID, err := s.UpsertProject(ctx, 0, "Operations", true, 1)
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	headID, err := s.UpsertHead(ctx, 0, projectID, "Rent", "5", true, 1)
	if err != nil {
		t.Fatalf("UpsertHead: %v", err)
	}
	return actor, headID
}

// seedRequestParty creates the users, project, and head that request fixtures
// share, returning an Accounts actor, a requester, a manager id, and a head id.
func seedRequestParty(t *testing.T, s *Store, ctx context.Context) (accountant User, requester User, managerID, headID int64) {
	t.Helper()
	accID, err := s.CreateUser(ctx, "accounts@example.com", "Accounts", "hash", "admin", true)
	if err != nil {
		t.Fatalf("create accountant: %v", err)
	}
	reqID, err := s.CreateUser(ctx, "requester@example.com", "Requester", "hash", "data_entry", true)
	if err != nil {
		t.Fatalf("create requester: %v", err)
	}
	mgrID, err := s.CreateUser(ctx, "manager@example.com", "Manager", "hash", "admin", true)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	projectID, err := s.UpsertProject(ctx, 0, "Operations", true, 1)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	hid, err := s.UpsertHead(ctx, 0, projectID, "Rent", "5", true, 1)
	if err != nil {
		t.Fatalf("create head: %v", err)
	}
	if accountant, err = s.UserByID(ctx, accID); err != nil {
		t.Fatal(err)
	}
	if requester, err = s.UserByID(ctx, reqID); err != nil {
		t.Fatal(err)
	}
	return accountant, requester, mgrID, hid
}

// seedApprovedRequest inserts an approved payment_requests row directly (its
// schema is fixed by overview §3) so Phase-3 store tests do not depend on the
// Phase-2 request methods. seq keeps the request number unique within a test.
func seedApprovedRequest(t *testing.T, s *Store, ctx context.Context, seq int, requesterID, managerID, headID, amount, approved int64) int64 {
	t.Helper()
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

func requestStatus(t *testing.T, s *Store, ctx context.Context, id int64) string {
	t.Helper()
	var status string
	if err := s.DB().QueryRowContext(ctx, `SELECT status FROM payment_requests WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatalf("request status: %v", err)
	}
	return status
}

func requestProcessingBy(t *testing.T, s *Store, ctx context.Context, id int64) *int64 {
	t.Helper()
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

func seedRequestActors(t *testing.T, s *Store, ctx context.Context) (requester User, manager User, headID int64) {
	t.Helper()
	rid, err := s.CreateUser(ctx, "req@example.com", "Rhea Requester", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	mid, err := s.CreateUser(ctx, "mgr@example.com", "Manav Manager", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	requester, _ = s.UserByID(ctx, rid)
	manager, _ = s.UserByID(ctx, mid)
	projectID, err := s.UpsertProject(ctx, 0, "Operations", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	headID, err = s.UpsertHead(ctx, 0, projectID, "Rent", "5", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	return requester, manager, headID
}

// seedTestVendor inserts a Phase-1V vendor row directly; Phase 2 only needs its id.
func seedTestVendor(t *testing.T, s *Store, ctx context.Context, name string) int64 {
	t.Helper()
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
func grantApprovalPermission(t *testing.T, s *Store, ctx context.Context, userID int64) {
	t.Helper()
	var roleID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM roles WHERE lower(name)='manager'`).Scan(&roleID); err != nil {
		t.Fatalf("seeded Manager role missing: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES(?,?) ON CONFLICT DO NOTHING`, userID, roleID); err != nil {
		t.Fatal(err)
	}
}

func newRoleActor(t *testing.T, s *Store, ctx context.Context) User {
	t.Helper()
	id, err := s.CreateUser(ctx, "roleadmin@example.com", "Role Admin", "hash", "admin", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u, err := s.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	return u
}

func vendorActor(t *testing.T, s *Store, ctx context.Context) User {
	t.Helper()
	id, err := s.CreateUser(ctx, "vendor-admin@example.com", "Vendor Admin", "hash", "admin", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u, err := s.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	return u
}

// seedVendorRequest inserts a payment_requests row shaped the way the request
// form makes one for a vendor: vendor_id names the payee and vendor_payee is
// left empty. Reading the snapshot on such a row is the trap PROGRESS.md
// records, and it is exactly the trap a test about vendor totals must not fall
// into — a fixture that fills vendor_payee would make the payee-name fallback
// look like the vendor_id join working.
func seedVendorRequest(t *testing.T, s *Store, ctx context.Context, seq int, status string, requesterID, managerID, headID, vendorID, amount int64) int64 {
	t.Helper()
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

func newAccountsUser(t *testing.T, s *Store, ctx context.Context, email string) User {
	t.Helper()
	uid, err := s.CreateUser(ctx, email, fmt.Sprintf("Accounts %s", email), "hash", "data_entry", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u, err := s.UserByID(ctx, uid)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	return u
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
