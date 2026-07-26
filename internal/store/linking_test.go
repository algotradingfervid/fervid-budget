package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
)

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

func TestMigrationV4AddsLinkingColumnsAndIndex(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	for _, col := range []string{"request_id", "settlement", "partial_reason"} {
		var found int
		if err := s.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('payments') WHERE name=?`, col).Scan(&found); err != nil {
			t.Fatal(err)
		}
		if found != 1 {
			t.Fatalf("payments.%s missing after migration", col)
		}
	}
	var idx int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_payments_request'`).Scan(&idx); err != nil {
		t.Fatal(err)
	}
	if idx != 1 {
		t.Fatal("idx_payments_request missing after migration")
	}
	_ = ctx
}

func TestPaymentRequestIndexRejectsDuplicateLinkButAllowsNullHistoricals(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	linked := func(paidOn string) error {
		_, err := s.DB().ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,entered_by,request_id,settlement) VALUES(?,?,?,?,?,'settled')`, headID, paidOn, 1000, acc.ID, reqID)
		return err
	}
	if err := linked("2026-06-15"); err != nil {
		t.Fatalf("first link insert: %v", err)
	}
	if err := linked("2026-06-16"); err == nil {
		t.Fatal("second payment linked to the same request was allowed (S9 broken)")
	}
	for _, d := range []string{"2026-06-17", "2026-06-18"} {
		if _, err := s.DB().ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,entered_by) VALUES(?,?,?,?)`, headID, d, 2000, acc.ID); err != nil {
			t.Fatalf("historical NULL-request insert %s: %v", d, err)
		}
	}
}

func TestMigrationV4IsIdempotentAndLeavesHistoricalPaymentsUntouched(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := dir + "/fervid.db"
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	actor, headID := seedActorAndHead(t, s, ctx)
	payID, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-05-10", Amount: 7777})
	if err != nil {
		t.Fatalf("historical payment: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Re-open the same database: migrate() must be a no-op, not an error.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	var reqID sql.NullInt64
	var settlement string
	if err := s2.DB().QueryRowContext(ctx, `SELECT request_id, settlement FROM payments WHERE id=?`, payID).Scan(&reqID, &settlement); err != nil {
		t.Fatalf("read historical payment: %v", err)
	}
	if reqID.Valid || settlement != "" {
		t.Fatalf("historical payment changed by migration: request_id=%v settlement=%q", reqID, settlement)
	}
	_ = errors.Is
}

func TestPaymentForRequestAndHistoricalFields(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	payID, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-05-11", Amount: 3210})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Payment(ctx, payID)
	if err != nil {
		t.Fatal(err)
	}
	if p.RequestID != nil || p.Settlement != "" || p.PartialReason != "" {
		t.Fatalf("historical payment carries linkage fields: %+v", p)
	}
	if _, err := s.PaymentForRequest(ctx, 424242); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PaymentForRequest(unlinked) = %v, want ErrNotFound", err)
	}
}

func TestReserveRequestMovesApprovedToProcessing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "processing" {
		t.Fatalf("status = %q, want processing", got)
	}
	if pb := requestProcessingBy(t, s, ctx, reqID); pb == nil || *pb != acc.ID {
		t.Fatalf("processing_by = %v, want %d", pb, acc.ID)
	}
	// Illegal source states are rejected (L11): a second reserve, and reserving a
	// non-approved (e.g. still-pending) request, both fail.
	if err := s.ReserveRequest(ctx, acc, reqID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("double reserve = %v, want ErrForbidden", err)
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET status='pending' WHERE id=?`, reqID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, acc, reqID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reserve pending = %v, want ErrForbidden", err)
	}
}

func TestReserveRequestSkipsOnHoldRequests(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET on_hold=1 WHERE id=?`, reqID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, acc, reqID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reserve on-hold = %v, want ErrForbidden", err)
	}
}

// TestReserveRequestIsAtomicUnderConcurrency is the S5 proof: many accountants
// race to reserve one request; exactly one wins and the rest get ErrForbidden.
func TestReserveRequestIsAtomicUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)

	const racers = 8
	accts := make([]User, racers)
	for i := range accts {
		id, err := s.CreateUser(ctx, fmt.Sprintf("racer%d@example.com", i), fmt.Sprintf("Racer %d", i), "hash", "admin", true)
		if err != nil {
			t.Fatal(err)
		}
		if accts[i], err = s.UserByID(ctx, id); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = s.ReserveRequest(ctx, accts[i], reqID)
		}(i)
	}
	close(start)
	wg.Wait()

	winner := -1
	for i, err := range errs {
		switch {
		case err == nil:
			if winner != -1 {
				t.Fatalf("more than one winner: %d and %d", winner, i)
			}
			winner = i
		case errors.Is(err, ErrForbidden):
		default:
			t.Fatalf("racer %d unexpected error: %v", i, err)
		}
	}
	if winner == -1 {
		t.Fatal("no racer won the reservation")
	}
	if got := requestStatus(t, s, ctx, reqID); got != "processing" {
		t.Fatalf("final status = %q, want processing", got)
	}
	if pb := requestProcessingBy(t, s, ctx, reqID); pb == nil || *pb != accts[winner].ID {
		t.Fatalf("processing_by = %v, want winner %d", pb, accts[winner].ID)
	}
}
