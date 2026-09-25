package store

import (
	"context"
	"errors"
	"testing"
)

func TestFullBalanceCannotEnterShortfallReview(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgr, head := seedRequestParty(t, s, ctx)
	id := seedApprovedRequest(t, s, ctx, 1, req.ID, mgr, head, 1200000, 1200000)
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, acc, id, PaymentInput{PaidOn: "2026-06-15", Amount: 700000}, "installment", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	_, err := s.RecordPaymentForRequest(ctx, acc, id, PaymentInput{PaidOn: "2026-06-16", Amount: 500000}, "partial", "Disputed remaining balance", nil)
	if !errors.Is(err, ErrValidation) {
		r, _ := s.Request(ctx, id)
		t.Fatalf("full balance partial returned %v with state=%s paid=%d; want refusal", err, r.Status, r.PaidAmount)
	}
}

func TestContinuedShortfallThenAcceptedDeductionKeepsCumulativeAccounting(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, head := seedRequestParty(t, s, ctx)
	mgr, err := s.UserByID(ctx, mgrID)
	if err != nil {
		t.Fatal(err)
	}
	id := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, head, 1200000, 1200000)
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, acc, id, PaymentInput{PaidOn: "2026-06-15", Amount: 700000}, "partial", "Only part delivered", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ContinuePartial(ctx, mgr, id, "Pay balance after inspection"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, acc, id, PaymentInput{PaidOn: "2026-06-16", Amount: 300000}, "partial", "Final two thousand remains disputed", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptPartial(ctx, mgr, id, "Accept documented reduction of two thousand"); err != nil {
		t.Fatal(err)
	}
	r, err := s.Request(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "completed_partial" || r.PaidAmount != 1000000 || *r.ApprovedAmount != 1200000 {
		t.Fatalf("wrong final obligation: %+v", r)
	}
	grid, err := s.Grid(ctx, "2026-06", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var actual int64
	for _, row := range grid.Rows {
		if row.HeadID == head {
			actual = row.Actual
		}
	}
	if actual != 1000000 {
		t.Fatalf("budget actual=%d, want only payouts1000000", actual)
	}
	history, err := s.RequestPayments(ctx, id)
	if err != nil || len(history) != 2 {
		t.Fatalf("history %v error%v", history, err)
	}
	if err := s.ContinuePartial(ctx, mgr, id, "Try reopening accepted deduction"); !errors.Is(err, ErrValidation) {
		t.Fatalf("accepted deduction reopened: %v", err)
	}
}
