package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestInstallmentsCumulativeCeilingRetryAndHistory(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, requester, mgr, head := seedRequestParty(t, s, ctx)
	id := seedApprovedRequest(t, s, ctx, 1, requester.ID, mgr, head, 1200000, 1200000)
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	first := PaymentInput{PaidOn: "2026-06-15", Amount: 700000, SubmissionKey: "first-confirm"}
	if _, err := historicalSettlement(s, ctx, acc, id, first, "settled", "", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("silent short settlement: %v", err)
	}
	if _, err := historicalSettlement(s, ctx, acc, id, first, "installment", "Payment 1 of 2", nil); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Request(ctx, id)
	if r.Status != "approved" || r.PaidAmount != 700000 || r.ProcessingBy != nil {
		t.Fatalf("first installment: %+v", r)
	}
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	if _, err := historicalSettlement(s, ctx, acc, id, first, "installment", "Payment 1 of 2", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("replayed confirmation: %v", err)
	}
	stalePaid := int64(0)
	stale := PaymentInput{PaidOn: "2026-06-16", Amount: 300000, SubmissionKey: "stale-window", ExpectedPaid: &stalePaid}
	if _, err := historicalSettlement(s, ctx, acc, id, stale, "installment", "", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("stale payment window: %v", err)
	}
	over := PaymentInput{PaidOn: "2026-06-16", Amount: 500001, SubmissionKey: "over"}
	if _, err := historicalSettlement(s, ctx, acc, id, over, "settled", "", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("cumulative overpay: %v", err)
	}
	second := PaymentInput{PaidOn: "2026-06-16", Amount: 500000, SubmissionKey: "second-confirm"}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := historicalSettlement(s, ctx, acc, id, second, "settled", "", nil)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("concurrent success count=%d", success)
	}
	r, _ = s.Request(ctx, id)
	history, err := s.RequestPayments(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.PaidAmount != 1200000 || r.Status != "completed" || len(history) != 2 {
		t.Fatalf("final=%+v history=%d", r, len(history))
	}
	if history[0].Amount != 700000 || history[1].Amount != 500000 {
		t.Fatalf("history changed: %+v", history)
	}
}

func TestDuplicateInvoiceRequiresReasonAndEditCannotBypass(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	requester, mgr, head := seedRequestActors(t, s, ctx)
	vendor := seedTestVendor(t, s, ctx, "Invoice Test")
	in := RequestInput{Type: "vendor_invoice", Treatment: "budget", ProjectID: 1, HeadID: head, VendorID: vendor, ShortTitle: "Test", Purpose: "Invoice test", Amount: 220000, ManagerID: mgr.ID, InvoiceNo: " INV-123 ", InvoiceDate: "2026-06-15"}
	if _, err := s.CreateRequest(ctx, requester, in); err != nil {
		t.Fatal(err)
	}
	in.InvoiceNo = "inv-123"
	for _, reason := range []string{"", "short"} {
		in.DuplicateReason = reason
		if _, err := s.CreateRequest(ctx, requester, in); !errors.Is(err, ErrValidation) {
			t.Fatalf("override=%q err=%v", reason, err)
		}
	}
	in.DuplicateReason = "Legitimate replacement; original is under dispute"
	id, err := s.CreateRequest(ctx, requester, in)
	if err != nil {
		t.Fatal(err)
	}
	audit, err := s.Audit(ctx, "payment_request", id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(audit[0].AfterJSON, in.DuplicateReason) {
		t.Fatal("override reason absent from audit")
	}
	in.InvoiceNo = "different"
	in.DuplicateReason = ""
	editable, err := s.CreateRequest(ctx, requester, in)
	if err != nil {
		t.Fatal(err)
	}
	in.InvoiceNo = "INV-123"
	if err := s.EditRequest(ctx, requester, editable, RequestEdit{Input: in}); !errors.Is(err, ErrValidation) {
		t.Fatalf("edit bypass=%v", err)
	}
	in.InvoiceDate = "2027-06-15"
	if _, err := s.CreateRequest(ctx, requester, in); err != nil {
		t.Fatalf("legitimate later invoice year: %v", err)
	}
}

func TestPartialReviewCanContinueWithoutNewApproval(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, requester, mgrID, head := seedRequestParty(t, s, ctx)
	mgr, _ := s.UserByID(ctx, mgrID)
	id := seedApprovedRequest(t, s, ctx, 1, requester.ID, mgrID, head, 1200000, 1200000)
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	if _, err := historicalSettlement(s, ctx, acc, id, PaymentInput{PaidOn: "2026-06-15", Amount: 700000}, "partial", "Balance disputed", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ContinuePartial(ctx, requester, id, "Next week"); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	if err := s.ContinuePartial(ctx, mgr, id, "Pay balance next week"); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Request(ctx, id)
	if r.Status != "approved" || r.PaidAmount != 700000 || *r.ApprovedAmount != 1200000 {
		t.Fatalf("continued=%+v", r)
	}
}
