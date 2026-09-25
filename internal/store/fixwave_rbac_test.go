package store

import (
	"context"
	"testing"
)

// Fixwave 2026-09-25, rbac-2: an empty data scope — the role editor's "None",
// stored as no role_data_scope row — used to leave the request list, the
// request export, the payment queue and the payment ledger unrestricted, while
// the detail pages refused the same rows. Every scoped read now fails closed
// on anything other than own, assigned or all, the way recoverableScope does.
func TestEmptyScopeReadsNoRequestsOrPayments(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if _, err := s.CreatePayment(ctx, acc, PaymentInput{HeadID: headID, PaidOn: "2026-05-05", Amount: 1100, VendorPayee: "Mine"}); err != nil {
		t.Fatal(err)
	}

	for _, scope := range []string{"", "nonsense"} {
		list, err := s.ListRequests(ctx, RequestListOptions{Scope: scope, ViewerID: acc.ID, Bucket: "all"})
		if err != nil || len(list) != 0 {
			t.Fatalf("ListRequests(scope %q) = %d rows, %v; want none", scope, len(list), err)
		}
		n, err := s.CountRequests(ctx, RequestListOptions{Scope: scope, ViewerID: acc.ID, Bucket: "all"})
		if err != nil || n != 0 {
			t.Fatalf("CountRequests(scope %q) = %d, %v; want 0", scope, n, err)
		}
		page, err := s.ListRequestsPage(ctx, RequestPageOptions{RequestListOptions: RequestListOptions{Scope: scope, ViewerID: acc.ID, Bucket: "all"}})
		if err != nil || len(page.Requests) != 0 {
			t.Fatalf("ListRequestsPage(scope %q) = %d rows, %v; want none", scope, len(page.Requests), err)
		}
		set, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: scope, ViewerID: acc.ID})
		if err != nil || len(set.Available) != 0 || set.Counts.Approved != 0 {
			t.Fatalf("LinkablePaymentRequests(scope %q) = %d rows / %d approved, %v; want none", scope, len(set.Available), set.Counts.Approved, err)
		}
		payments, err := s.ListPayments(ctx, PaymentListOptions{Scope: scope, ViewerID: acc.ID, Month: "2026-05"})
		if err != nil || len(payments) != 0 {
			t.Fatalf("ListPayments(scope %q) = %d rows, %v; want none", scope, len(payments), err)
		}
	}

	// And "all" is still everything, so nothing was locked shut.
	list, err := s.ListRequests(ctx, RequestListOptions{Scope: ScopeAll, ViewerID: acc.ID, Bucket: "all"})
	if err != nil || len(list) != 1 {
		t.Fatalf("ListRequests(all) = %d rows, %v; want 1", len(list), err)
	}
	payments, err := s.ListPayments(ctx, PaymentListOptions{Scope: ScopeAll, ViewerID: acc.ID, Month: "2026-05"})
	if err != nil || len(payments) != 1 {
		t.Fatalf("ListPayments(all) = %d rows, %v; want 1", len(payments), err)
	}
}
