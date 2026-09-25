package store

import (
	"context"
	"errors"
	"testing"
)

func TestInstallmentCannotBeCancelledAndHidden(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, requester, managerID, headID := seedRequestParty(t, s, ctx)
	manager, err := s.UserByID(ctx, managerID)
	if err != nil {
		t.Fatal(err)
	}
	id := seedApprovedRequest(t, s, ctx, 1, requester.ID, managerID, headID, 1200000, 1200000)
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, acc, id, PaymentInput{PaidOn: "2026-06-15", Amount: 700000, SubmissionKey: "integration-cancel-first"}, "installment", "", nil); err != nil {
		t.Fatal(err)
	}
	for name, action := range map[string]func() error{
		"requester asks":  func() error { return s.RequestCancellation(ctx, requester, id, "Cancel the balance") },
		"manager cancels": func() error { return s.CancelRequest(ctx, manager, id, "Cancel the balance") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(); !errors.Is(err, ErrValidation) {
				t.Fatalf("got %v, want validation refusal", err)
			}
		})
	}
	r, err := s.Request(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "approved" || r.PaidAmount != 700000 {
		t.Fatalf("request altered: status=%s paid=%d", r.Status, r.PaidAmount)
	}
	// A stale/imported cancellation decision also must not erase paid history.
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='cancellation_requested' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideCancellation(ctx, manager, id, true, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("accept after payout = %v", err)
	}
	// Declining is safe and must provide a recovery route out of the old state.
	if err := s.DecideCancellation(ctx, manager, id, false, "Payment exists; retain remaining obligation"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, acc, id, PaymentInput{PaidOn: "2026-06-16", Amount: 500000, SubmissionKey: "integration-cancel-second"}, "settled", "", nil); err != nil {
		t.Fatal(err)
	}
}
