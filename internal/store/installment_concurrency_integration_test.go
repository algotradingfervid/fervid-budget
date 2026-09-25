package store

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestConcurrentInstallmentsAndStaleConfirmationPreserveCeiling(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, requester, managerID, headID := seedRequestParty(t, s, ctx)
	id := seedApprovedRequest(t, s, ctx, 1, requester.ID, managerID, headID, 1200000, 1200000)
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	first := PaymentInput{PaidOn: "2026-06-15", Amount: 700000, SubmissionKey: "integration-first-confirmation"}
	if _, err := s.RecordPaymentForRequest(ctx, acc, id, first, "installment", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	// Replaying the old form after reserving again must not consume the balance.
	if _, err := s.RecordPaymentForRequest(ctx, acc, id, first, "installment", "", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("old token replay = %v", err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, key := range []string{"integration-race-a", "integration-race-b"} {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			<-start
			_, errs[i] = s.RecordPaymentForRequest(ctx, acc, id, PaymentInput{PaidOn: "2026-06-16", Amount: 400000, SubmissionKey: key}, "installment", "", nil)
		}(i, key)
	}
	close(start)
	wg.Wait()
	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrValidation) {
			t.Fatalf("unexpected concurrency error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful racing installments=%d, want1", successes)
	}
	r, err := s.Request(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.PaidAmount != 1100000 || r.Status != "approved" {
		t.Fatalf("paid=%d status=%s", r.PaidAmount, r.Status)
	}
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, acc, id, PaymentInput{PaidOn: "2026-06-17", Amount: 100001, SubmissionKey: "integration-over"}, "settled", "", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("overpayment=%v", err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, acc, id, PaymentInput{PaidOn: "2026-06-17", Amount: 100000, SubmissionKey: "integration-final"}, "settled", "", nil); err != nil {
		t.Fatal(err)
	}
	history, err := s.RequestPayments(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("payment history count=%d, want3", len(history))
	}
	r, err = s.Request(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.PaidAmount != 1200000 || r.Status != "completed" {
		t.Fatalf("paid=%d status=%s", r.PaidAmount, r.Status)
	}
}
