package store

import (
	"context"
	"errors"
	"testing"
)

// A month lock that cannot be read is not an unlocked month.
//
// IsLocked used to discard its query error and return false, so every write path
// that asked it — budgets, month plans, payment edit, void, attachment, and the
// approval gate in the app layer — read a failure as "open" and let the write
// through. Under the write contention that this package's own concurrency test
// reproduces, that failure was not hypothetical.
//
// The closed database below is the cheapest way to make the read fail for
// certain. The point is the shape of the answer: an error, not a confident false.
func TestMonthIsLockedReportsFailureInsteadOfAnsweringOpen(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)

	if err := s.LockMonth(ctx, actor, "2026-07", "audit in progress"); err != nil {
		t.Fatalf("LockMonth: %v", err)
	}

	locked, err := s.MonthIsLocked(ctx, "2026-07")
	if err != nil {
		t.Fatalf("MonthIsLocked on a live database: %v", err)
	}
	if !locked {
		t.Fatal("a locked month must read as locked")
	}

	open, err := s.MonthIsLocked(ctx, "2026-06")
	if err != nil {
		t.Fatalf("MonthIsLocked for an unlocked month: %v", err)
	}
	if open {
		t.Fatal("an unlocked month must read as unlocked")
	}

	// Once the database is gone the answer must be an error. The old IsLocked
	// returned false here, which is the bug: silence that reads as permission.
	if err := s.db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := s.MonthIsLocked(ctx, "2026-07"); err == nil {
		t.Fatal("a month lock that cannot be read must report the failure, not answer 'open'")
	}
}

// The same guarantee, one layer down: validatePaymentTx must surface the failure
// rather than treat an unreadable lock as a licence to write.
func TestValidatePaymentSurfacesAnUnreadableMonthLock(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, headID := seedActorAndHead(t, s, ctx)

	in := PaymentInput{HeadID: headID, PaidOn: "2026-07-21", Amount: 1000}
	if err := s.validatePayment(ctx, in, false); err != nil {
		t.Fatalf("a valid payment on a live database: %v", err)
	}

	if err := s.db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := s.validatePayment(ctx, in, false)
	if err == nil {
		t.Fatal("validatePayment accepted a payment whose month lock could not be read")
	}
	if errors.Is(err, ErrValidation) {
		t.Fatalf("the refusal must carry the read failure, not masquerade as invalid input: %v", err)
	}
}
