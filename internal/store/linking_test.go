package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestPaymentRequestIndexAllowsInstallmentsAndNullHistoricals(t *testing.T) {
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
	if err := linked("2026-06-16"); err != nil {
		t.Fatalf("installment insert: %v", err)
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

func TestReleaseRequestRequiresConfirmReasonAndAuthority(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	otherID, err := s.CreateUser(ctx, "other@example.com", "Other", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.UserByID(ctx, otherID)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	const reason = "Vendor bank details need confirming before I can transfer."
	// No confirm → validation, still processing (S7).
	if err := s.ReleaseRequest(ctx, acc, reqID, reason, false, false); !errors.Is(err, ErrValidation) {
		t.Fatalf("release without confirm = %v, want ErrValidation", err)
	}
	// G12: no reason → validation, still processing, nothing recorded.
	if err := s.ReleaseRequest(ctx, acc, reqID, "   ", true, false); !errors.Is(err, ErrValidation) {
		t.Fatalf("release without reason = %v, want ErrValidation", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "processing" {
		t.Fatalf("status after refused release = %q, want processing", got)
	}
	// Non-assignee, non-authorized → forbidden (S6).
	if err := s.ReleaseRequest(ctx, other, reqID, reason, true, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-assignee release = %v, want ErrForbidden", err)
	}
	// Assignee with confirm + reason → back to approved, unclaimed.
	if err := s.ReleaseRequest(ctx, acc, reqID, reason, true, false); err != nil {
		t.Fatalf("assignee release: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "approved" {
		t.Fatalf("status after release = %q, want approved", got)
	}
	if pb := requestProcessingBy(t, s, ctx, reqID); pb != nil {
		t.Fatalf("processing_by not cleared: %v", pb)
	}
	// G12: the reason is in the audit trail, which is what the release .thread renders.
	var trail int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE entity_type='payment_request' AND entity_id=? AND action='release' AND summary LIKE ?`, reqID, "%"+reason+"%").Scan(&trail); err != nil {
		t.Fatal(err)
	}
	if trail != 1 {
		t.Fatalf("release reason not in the audit trail: %d matching entries", trail)
	}
	// Authorized caller (reservation:release granted broadly) may release someone else's hold.
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseRequest(ctx, other, reqID, "Reserved over a day, freeing it for the queue.", true, true); err != nil {
		t.Fatalf("authorized release: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "approved" {
		t.Fatalf("status after authorized release = %q, want approved", got)
	}
}

func TestReassignReservationRequiresPermissionReasonAndActiveTarget(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	deepakID, err := s.CreateUser(ctx, "deepak@example.com", "Deepak Menon", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	const reason = "Going on leave, Deepak picks it up."
	// Without the reassign permission → forbidden, reservation untouched.
	if err := s.ReassignReservation(ctx, acc, reqID, deepakID, reason, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unauthorized reassign = %v, want ErrForbidden", err)
	}
	if pb := requestProcessingBy(t, s, ctx, reqID); pb == nil || *pb != acc.ID {
		t.Fatalf("processing_by moved without permission: %v", pb)
	}
	// Reason is required (same rule as release).
	if err := s.ReassignReservation(ctx, acc, reqID, deepakID, "  ", true); !errors.Is(err, ErrValidation) {
		t.Fatalf("reassign without reason = %v, want ErrValidation", err)
	}
	// Reassigning to the current holder is a no-op the user should not be offered.
	if err := s.ReassignReservation(ctx, acc, reqID, acc.ID, reason, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("reassign to self = %v, want ErrValidation", err)
	}
	// Unknown target user → not found, nothing changed.
	if err := s.ReassignReservation(ctx, acc, reqID, 987654, reason, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reassign to unknown user = %v, want ErrNotFound", err)
	}
	// Authorized, with a reason, to a real colleague → still processing, new holder.
	if err := s.ReassignReservation(ctx, acc, reqID, deepakID, reason, true); err != nil {
		t.Fatalf("reassign: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "processing" {
		t.Fatalf("status after reassign = %q, want processing (it never returns to the open queue)", got)
	}
	if pb := requestProcessingBy(t, s, ctx, reqID); pb == nil || *pb != deepakID {
		t.Fatalf("processing_by = %v, want %d", pb, deepakID)
	}
	var trail int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE entity_type='payment_request' AND entity_id=? AND action='reassign' AND summary LIKE ?`, reqID, "%"+reason+"%").Scan(&trail); err != nil {
		t.Fatal(err)
	}
	if trail != 1 {
		t.Fatalf("reassign reason not in the audit trail: %d matching entries", trail)
	}
	// An approved (unreserved) request cannot be reassigned — there is nothing to move.
	other := seedApprovedRequest(t, s, ctx, 2, req.ID, mgrID, headID, 100000, 100000)
	if err := s.ReassignReservation(ctx, acc, other, deepakID, reason, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reassign of an unreserved request = %v, want ErrForbidden", err)
	}
}

func TestRecordPaymentSettledCompletesEvenWhenUnderApproved(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	payID, err := historicalSettlement(s, ctx, acc, reqID, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 400000, VendorPayee: "Acme Landlord"}, "settled", "Agreed deduction documented", nil)
	if err != nil {
		t.Fatalf("record settled: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "completed" {
		t.Fatalf("status = %q, want completed (S10)", got)
	}
	p, err := s.PaymentForRequest(ctx, reqID)
	if err != nil || p.ID != payID || p.RequestID == nil || *p.RequestID != reqID || p.Settlement != "settled" {
		t.Fatalf("linked payment = %+v, err=%v", p, err)
	}
	// One payment per request: a second settlement is refused (S9).
	if _, err := historicalSettlement(s, ctx, acc, reqID, PaymentInput{HeadID: headID, PaidOn: "2026-06-16", Amount: 100000, VendorPayee: "Acme"}, "settled", "", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("second settlement = %v, want ErrForbidden", err)
	}
}

func TestRecordPaymentPartialNeedsReasonAndRoutesToReview(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	in := PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 300000, VendorPayee: "Acme Landlord"}
	// Partial without reason → validation; nothing written; still processing (S13).
	if _, err := historicalSettlement(s, ctx, acc, reqID, in, "partial", "", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("partial without reason = %v, want ErrValidation", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "processing" {
		t.Fatalf("status after rejected partial = %q, want processing", got)
	}
	if _, err := s.PaymentForRequest(ctx, reqID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("payment written despite rejected settlement: %v", err)
	}
	// Partial with reason → partial_review (L9).
	if _, err := historicalSettlement(s, ctx, acc, reqID, in, "partial", "Balance pending vendor confirmation", nil); err != nil {
		t.Fatalf("record partial: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "partial_review" {
		t.Fatalf("status = %q, want partial_review", got)
	}
	p, err := s.PaymentForRequest(ctx, reqID)
	if err != nil || p.Settlement != "partial" || p.PartialReason != "Balance pending vendor confirmation" {
		t.Fatalf("linked partial payment = %+v, err=%v", p, err)
	}
}

func TestRecordPaymentRequiresActorReservation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	otherID, err := s.CreateUser(ctx, "other@example.com", "Other", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.UserByID(ctx, otherID)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	in := PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 500000, VendorPayee: "Acme Landlord"}
	// Not reserved at all → forbidden, no payment (store-level X5).
	if _, err := historicalSettlement(s, ctx, acc, reqID, in, "settled", "", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("record without reservation = %v, want ErrForbidden", err)
	}
	if got := paymentCount(t, s); got != 0 {
		t.Fatalf("payment written without reservation: %d", got)
	}
	// Reserved by acc; a different accountant may not record it.
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	if _, err := historicalSettlement(s, ctx, other, reqID, in, "settled", "", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("record by non-assignee = %v, want ErrForbidden", err)
	}
}

// TestRecordPaymentRejectsOverpayment is the G13 proof: the mockup's read-only
// approved field and .banner.bad both promise that paid can never exceed
// approved. Rejection must leave the request reserved and no payment written, so
// the accountant can correct the figure without re-reserving.
func TestRecordPaymentRejectsOverpayment(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 480000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	over := PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 480001, VendorPayee: "Acme Landlord"}
	for _, settlement := range []string{"settled", "partial"} {
		_, err := historicalSettlement(s, ctx, acc, reqID, over, settlement, "reason", nil)
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("overpayment (%s) = %v, want ErrValidation", settlement, err)
		}
		if !strings.Contains(err.Error(), "cancel") {
			t.Fatalf("overpayment error must name the remedy (cancel and raise a new request), got %q", err)
		}
	}
	if got := paymentCount(t, s); got != 0 {
		t.Fatalf("overpayment wrote %d payment rows (G13 broken)", got)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "processing" {
		t.Fatalf("status after refused overpayment = %q, want processing (reservation must survive)", got)
	}
	// The approved amount itself is allowed; it is > that is refused.
	exact := PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 480000, VendorPayee: "Acme Landlord"}
	if _, err := historicalSettlement(s, ctx, acc, reqID, exact, "settled", "", nil); err != nil {
		t.Fatalf("payment of exactly the approved amount: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "completed" {
		t.Fatalf("status = %q, want completed", got)
	}
}

func TestLinkedPaymentIsImmutable(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	payID, err := historicalSettlement(s, ctx, acc, reqID, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 500000, VendorPayee: "Acme Landlord"}, "settled", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePayment(ctx, acc, payID, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 111, VendorPayee: "Changed"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("edit linked payment = %v, want ErrValidation", err)
	}
	if err := s.VoidPayment(ctx, acc, payID, "oops"); !errors.Is(err, ErrValidation) {
		t.Fatalf("void linked payment = %v, want ErrValidation", err)
	}
	p, err := s.Payment(ctx, payID)
	if err != nil || p.Amount != 500000 || p.VoidedAt != nil {
		t.Fatalf("linked payment mutated: %+v, %v", p, err)
	}
}

func TestAcceptPartialAndRaiseConcern(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	// Two requests, both taken to partial_review.
	settleToReview := func(seq int) int64 {
		id := seedApprovedRequest(t, s, ctx, seq, req.ID, mgrID, headID, 500000, 500000)
		if err := s.ReserveRequest(ctx, acc, id); err != nil {
			t.Fatal(err)
		}
		if _, err := historicalSettlement(s, ctx, acc, id, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 300000, VendorPayee: "Acme"}, "partial", "short pay", nil); err != nil {
			t.Fatal(err)
		}
		return id
	}
	mgr, _ := s.UserByID(ctx, mgrID)
	accepted := settleToReview(1)
	if err := s.AcceptPartial(ctx, mgr, accepted, "Balance will be invoiced separately."); err != nil {
		t.Fatalf("accept partial: %v", err)
	}
	// G14: a distinguishable terminal state, not plain 'completed'. The ledger and
	// the .pill.completed-partial both depend on being able to tell them apart.
	if got := requestStatus(t, s, ctx, accepted); got != "completed_partial" {
		t.Fatalf("accepted status = %q, want completed_partial (G14)", got)
	}
	// Accepting a non-partial_review request is rejected (L11).
	if err := s.AcceptPartial(ctx, mgr, accepted, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("accept completed_partial = %v, want ErrForbidden", err)
	}
	// The optional note reaches the trail the manager and Accounts both read.
	var noted int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE entity_type='payment_request' AND entity_id=? AND action='accept_partial' AND summary LIKE ?`, accepted, "%invoiced separately%").Scan(&noted); err != nil {
		t.Fatal(err)
	}
	if noted != 1 {
		t.Fatalf("accept note not in the audit trail: %d matching entries", noted)
	}
	// A clean settlement stays plain 'completed' — the two states never merge.
	clean := seedApprovedRequest(t, s, ctx, 3, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, clean); err != nil {
		t.Fatal(err)
	}
	if _, err := historicalSettlement(s, ctx, acc, clean, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 500000, VendorPayee: "Acme"}, "settled", "", nil); err != nil {
		t.Fatal(err)
	}
	if got := requestStatus(t, s, ctx, clean); got != "completed" {
		t.Fatalf("clean settlement status = %q, want completed", got)
	}

	concerned := settleToReview(2)
	if err := s.RaiseConcern(ctx, mgr, concerned, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty concern = %v, want ErrValidation", err)
	}
	if err := s.RaiseConcern(ctx, mgr, concerned, "Please confirm the balance timeline"); err != nil {
		t.Fatalf("raise concern: %v", err)
	}
	if got := requestStatus(t, s, ctx, concerned); got != "partial_review" {
		t.Fatalf("concerned status = %q, want partial_review", got)
	}
	var comments int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM request_comments WHERE request_id=? AND body=?`, concerned, "Please confirm the balance timeline").Scan(&comments); err != nil {
		t.Fatal(err)
	}
	if comments != 1 {
		t.Fatalf("concern comment not persisted: %d", comments)
	}
	// A concern writes a comment and an audit row whose summary is that same
	// comment, so the merged thread has to print the manager's words once.
	thread, err := s.RequestThread(ctx, concerned)
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	printed := 0
	for _, e := range thread {
		if strings.Contains(e.Title+e.Body, "Please confirm the balance timeline") {
			printed++
		}
	}
	if printed != 1 {
		t.Fatalf("the concern appears %d times in the thread, want 1", printed)
	}

	// Both answers belong to the request's own manager. Holding the verb is not
	// holding the request: ApproveRequest and decideRequest refuse a stranger
	// here, and a balance written off for good is the last place to relax it.
	notMine := settleToReview(4)
	if err := s.AcceptPartial(ctx, acc, notMine, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("accept by someone who is not the manager = %v, want ErrForbidden", err)
	}
	if err := s.RaiseConcern(ctx, acc, notMine, "Not mine to judge"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("concern by someone who is not the manager = %v, want ErrForbidden", err)
	}
	if got := requestStatus(t, s, ctx, notMine); got != "partial_review" {
		t.Fatalf("status after an outsider's decision = %q, want partial_review", got)
	}
	var strayed int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM request_comments WHERE request_id=?`, notMine).Scan(&strayed); err != nil {
		t.Fatal(err)
	}
	if strayed != 0 {
		t.Fatalf("a refused concern still wrote %d comment(s)", strayed)
	}
}

func TestHoldUnholdPreserveApprovedFieldsAndAllowComments(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 480000)

	if err := s.HoldRequest(ctx, acc, reqID, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("hold without reason = %v, want ErrValidation", err)
	}
	if err := s.HoldRequest(ctx, acc, reqID, "await vendor GST"); err != nil {
		t.Fatalf("hold: %v", err)
	}
	var onHold int
	var holdReason string
	var amount, approved int64
	if err := s.DB().QueryRowContext(ctx, `SELECT on_hold,hold_reason,amount,approved_amount FROM payment_requests WHERE id=?`, reqID).Scan(&onHold, &holdReason, &amount, &approved); err != nil {
		t.Fatal(err)
	}
	if onHold != 1 || holdReason != "await vendor GST" || amount != 500000 || approved != 480000 {
		t.Fatalf("hold changed approved fields: on_hold=%d reason=%q amount=%d approved=%d", onHold, holdReason, amount, approved)
	}
	// Q6: while on hold the requester may still add a comment; no field changes.
	// (Phase 2's AddRequestComment returns the new comment id alongside its error.)
	if _, err := s.AddRequestComment(ctx, req, reqID, "Attaching the corrected invoice"); err != nil {
		t.Fatalf("comment while on hold: %v", err)
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT amount,approved_amount FROM payment_requests WHERE id=?`, reqID).Scan(&amount, &approved); err != nil {
		t.Fatal(err)
	}
	if amount != 500000 || approved != 480000 {
		t.Fatalf("comment changed approved fields: amount=%d approved=%d", amount, approved)
	}
	// A held request cannot be reserved (L7).
	if err := s.ReserveRequest(ctx, acc, reqID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reserve held = %v, want ErrForbidden", err)
	}
	// Unhold restores availability.
	if err := s.UnholdRequest(ctx, acc, reqID); err != nil {
		t.Fatalf("unhold: %v", err)
	}
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatalf("reserve after unhold: %v", err)
	}
}

func TestLinkablePaymentRequestsFilterAndSearch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	linkable := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000) // approved, unclaimed, not held
	held := seedApprovedRequest(t, s, ctx, 2, req.ID, mgrID, headID, 600000, 600000)     // on hold → not takeable
	reserved := seedApprovedRequest(t, s, ctx, 3, req.ID, mgrID, headID, 700000, 700000) // reserved → not takeable
	pending := seedApprovedRequest(t, s, ctx, 4, req.ID, mgrID, headID, 800000, 800000)  // not approved → invisible here
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET on_hold=1 WHERE id=?`, held); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, acc, reserved); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET status='pending' WHERE id=?`, pending); err != nil {
		t.Fatal(err)
	}

	all, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID})
	if err != nil {
		t.Fatalf("linkable: %v", err)
	}
	if len(all.Available) != 1 || all.Available[0].ID != linkable {
		t.Fatalf("takeable set = %+v, want only request %d (S3)", all.Available, linkable)
	}
	// The taken/held rows are visible but never takeable — this is what the
	// picker renders as .co.is-taken instead of silently hiding them.
	if len(all.Unavailable) != 2 {
		t.Fatalf("unavailable set = %+v, want the held and the reserved request", all.Unavailable)
	}
	for _, r := range all.Unavailable {
		if r.ID == pending {
			t.Fatal("a non-approved request leaked into the queue")
		}
	}

	// Search narrows both sets, by number, payee and amount (S4).
	byNumber, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID, Query: "PR-2026-000001"})
	if err != nil || len(byNumber.Available) != 1 || byNumber.Available[0].ID != linkable {
		t.Fatalf("search by number = %+v, err=%v", byNumber.Available, err)
	}
	byPayee, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID, Query: "Acme Landlord"})
	if err != nil || len(byPayee.Available) != 1 {
		t.Fatalf("search by payee = %+v, err=%v", byPayee.Available, err)
	}
	byAmount, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID, Query: "500000"})
	if err != nil || len(byAmount.Available) != 1 {
		t.Fatalf("search by amount = %+v, err=%v", byAmount.Available, err)
	}
	none, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID, Query: "nonexistent-xyz"})
	if err != nil || len(none.Available) != 0 || len(none.Unavailable) != 0 {
		t.Fatalf("search miss = %+v, err=%v", none, err)
	}
}

// TestLinkablePaymentRequestsTabsCountsAndReserver builds one of every state the
// accounts queue can show and asserts each .segmented tab, each .metric-strip
// metric and the reserver identity the rows print. Without these the queue is
// unbuildable.
func TestLinkablePaymentRequestsTabsCountsAndReserver(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	deepakID, err := s.CreateUser(ctx, "deepak@example.com", "Deepak Menon", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	deepak, _ := s.UserByID(ctx, deepakID)
	mgr, _ := s.UserByID(ctx, mgrID)

	open1 := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	open2 := seedApprovedRequest(t, s, ctx, 2, req.ID, mgrID, headID, 300000, 300000)
	mine := seedApprovedRequest(t, s, ctx, 3, req.ID, mgrID, headID, 100000, 100000)
	stale := seedApprovedRequest(t, s, ctx, 4, req.ID, mgrID, headID, 78000, 78000)
	theirs := seedApprovedRequest(t, s, ctx, 5, req.ID, mgrID, headID, 33500, 33500)
	onHold := seedApprovedRequest(t, s, ctx, 6, req.ID, mgrID, headID, 25000, 25000)
	inReview := seedApprovedRequest(t, s, ctx, 7, req.ID, mgrID, headID, 95000, 95000)
	paidClean := seedApprovedRequest(t, s, ctx, 8, req.ID, mgrID, headID, 41300, 41300)
	// An accepted shortfall needs a real unpaid balance.
	paidPartial := seedApprovedRequest(t, s, ctx, 9, req.ID, mgrID, headID, 70000, 70000)

	for _, id := range []int64{mine, stale, inReview, paidClean, paidPartial} {
		if err := s.ReserveRequest(ctx, acc, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ReserveRequest(ctx, deepak, theirs); err != nil {
		t.Fatal(err)
	}
	if err := s.HoldRequest(ctx, acc, onHold, "waiting on the requester"); err != nil {
		t.Fatal(err)
	}
	// One reservation is 26 hours old; the queue banner counts it as stale.
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET processing_at=? WHERE id=?`,
		time.Date(2026, 7, 24, 12, 40, 0, 0, time.UTC).Format("2006-01-02 15:04:05"), stale); err != nil {
		t.Fatal(err)
	}
	if _, err := historicalSettlement(s, ctx, acc, inReview, PaymentInput{HeadID: headID, PaidOn: "2026-07-23", Amount: 60000, VendorPayee: "Nova"}, "partial", "700 of 1000 copies delivered", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := historicalSettlement(s, ctx, acc, paidClean, PaymentInput{HeadID: headID, PaidOn: "2026-07-24", Amount: 41300, VendorPayee: "Nova"}, "settled", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := historicalSettlement(s, ctx, acc, paidPartial, PaymentInput{HeadID: headID, PaidOn: "2026-07-23", Amount: 60000, VendorPayee: "Nova"}, "partial", "balance later", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptPartial(ctx, mgr, paidPartial, ""); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 25, 14, 40, 0, 0, time.UTC)
	set, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID, Now: now})
	if err != nil {
		t.Fatalf("linkable: %v", err)
	}
	c := set.Counts
	// .segmented tabs.
	if c.Approved != 2 {
		t.Fatalf("Approved tab = %d, want 2", c.Approved)
	}
	// mine + stale + theirs. The plan's draft asserted 2 here (own reservations
	// only), but accounts-queue.html reads "Processing 5" against "Reserved by
	// you 2" and "Reserved by others 3" — the tab counts every reservation, and
	// the reserved-by-you / reserved-by-others metrics split it. Counting only
	// one's own would also contradict the rows: the same method returns all
	// three under Status:"processing" a few lines below.
	if c.Processing != 3 {
		t.Fatalf("Processing tab = %d, want 3 (mine + stale + Deepak's)", c.Processing)
	}
	if c.Hold != 1 {
		t.Fatalf("On hold tab = %d, want 1", c.Hold)
	}
	if c.PartialReview != 1 {
		t.Fatalf("Partial review tab = %d, want 1", c.PartialReview)
	}
	if c.Paid != 2 { // completed + completed_partial both count as paid (G14)
		t.Fatalf("Paid tab = %d, want 2 (completed and completed_partial)", c.Paid)
	}
	// .metric-strip metrics.
	if c.ApprovedAmount != 800000 {
		t.Fatalf("approved total = %d, want 800000 (₹8,000.00)", c.ApprovedAmount)
	}
	if c.ReservedByMe != 2 {
		t.Fatalf("reserved by me = %d, want 2", c.ReservedByMe)
	}
	if c.ReservedByOthers != 1 {
		t.Fatalf("reserved by others = %d, want 1", c.ReservedByOthers)
	}
	if c.StaleReservations != 1 {
		t.Fatalf("stale reservations = %d, want 1 (the 26-hour-old one)", c.StaleReservations)
	}

	// Rows carry who holds the reservation and since when, which is what
	// "Reserved by you · 14:02" and "Reserved by Deepak M" render from.
	byStatus, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID, Status: "processing", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	found := map[int64]Request{}
	for _, r := range append(append([]Request{}, byStatus.Available...), byStatus.Unavailable...) {
		found[r.ID] = r
	}
	if r, ok := found[theirs]; !ok || r.ProcessingByName != "Deepak Menon" {
		t.Fatalf("reserved-by-others row = %+v, want ProcessingByName Deepak Menon", r)
	}
	if r, ok := found[theirs]; !ok || r.ProcessingAt == nil {
		t.Fatalf("reserved row has no ProcessingAt; the queue cannot print the reserved-at time")
	}
	// A row someone else holds is never offered as takeable.
	for _, r := range byStatus.Available {
		if r.ID == theirs {
			t.Fatal("a request reserved by another accountant was offered as takeable")
		}
	}
	if _, ok := found[open1]; ok {
		t.Fatalf("status=processing returned the approved request %d", open1)
	}
	_ = open2
}
