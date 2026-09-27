package store

import "testing"

func TestBadgeInvalidationRefreshesCountsWithoutWaitingForTTL(t *testing.T) {
	s, ctx, actor, headID := newBadgeStore(t)
	perms := NewPermissionSet(AllGrants(), nil)
	first, err := s.BadgeCounts(ctx, actor.ID, perms)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-04-10", Amount: 101, VendorPayee: "Fresh badge", PaymentMode: "cash"}); err != nil {
		t.Fatal(err)
	}
	s.InvalidateBadgeCounts()
	fresh, err := s.BadgeCounts(ctx, actor.ID, perms)
	if err != nil || fresh["my_payments"] != first["my_payments"]+1 {
		t.Fatalf("stale badge: before=%v after=%v err=%v", first, fresh, err)
	}
}
