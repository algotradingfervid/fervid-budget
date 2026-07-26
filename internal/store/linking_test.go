package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
	payID, err := s.RecordPaymentForRequest(ctx, acc, reqID, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 400000, VendorPayee: "Acme Landlord"}, "settled", "", nil)
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
	if _, err := s.RecordPaymentForRequest(ctx, acc, reqID, PaymentInput{HeadID: headID, PaidOn: "2026-06-16", Amount: 100000, VendorPayee: "Acme"}, "settled", "", nil); !errors.Is(err, ErrForbidden) {
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
	if _, err := s.RecordPaymentForRequest(ctx, acc, reqID, in, "partial", "", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("partial without reason = %v, want ErrValidation", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "processing" {
		t.Fatalf("status after rejected partial = %q, want processing", got)
	}
	if _, err := s.PaymentForRequest(ctx, reqID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("payment written despite rejected settlement: %v", err)
	}
	// Partial with reason → partial_review (L9).
	if _, err := s.RecordPaymentForRequest(ctx, acc, reqID, in, "partial", "Balance pending vendor confirmation", nil); err != nil {
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
	if _, err := s.RecordPaymentForRequest(ctx, acc, reqID, in, "settled", "", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("record without reservation = %v, want ErrForbidden", err)
	}
	if got := paymentCount(t, s); got != 0 {
		t.Fatalf("payment written without reservation: %d", got)
	}
	// Reserved by acc; a different accountant may not record it.
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, other, reqID, in, "settled", "", nil); !errors.Is(err, ErrForbidden) {
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
		_, err := s.RecordPaymentForRequest(ctx, acc, reqID, over, settlement, "reason", nil)
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
	if _, err := s.RecordPaymentForRequest(ctx, acc, reqID, exact, "settled", "", nil); err != nil {
		t.Fatalf("payment of exactly the approved amount: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "completed" {
		t.Fatalf("status = %q, want completed", got)
	}
}
