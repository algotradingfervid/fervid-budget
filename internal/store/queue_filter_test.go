package store

import (
	"context"
	"testing"
)

func TestApprovedQueueExcludesHeldAndReservedButPickerKeepsThem(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	accounts, requester, managerID, headID := seedRequestParty(t, s, ctx)
	open := seedApprovedRequest(t, s, ctx, 1, requester.ID, managerID, headID, 10000, 10000)
	held := seedApprovedRequest(t, s, ctx, 2, requester.ID, managerID, headID, 20000, 20000)
	reserved := seedApprovedRequest(t, s, ctx, 3, requester.ID, managerID, headID, 30000, 30000)
	if err := s.HoldRequest(ctx, accounts, held, "Waiting for documents"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, accounts, reserved); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, status, query string
		want                []int64
	}{
		{"approved", "approved", "", []int64{open}},
		{"approved search excludes hold", "approved", "PR-2026-000002", nil},
		{"approved search excludes reservation", "approved", "PR-2026-000003", nil},
		{"approved search finds available", "approved", "PR-2026-000001", []int64{open}},
		{"held remains discoverable", "hold", "PR-2026-000002", []int64{held}},
		{"processing remains discoverable", "processing", "PR-2026-000003", []int64{reserved}},
		{"picker retains pool", "", "", []int64{open, held, reserved}},
		{"picker searches reservation", "", "PR-2026-000003", []int64{reserved}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: ScopeAll, ViewerID: accounts.ID, Status: tc.status, Query: tc.query})
			if err != nil {
				t.Fatal(err)
			}
			if got.Counts.Approved != 1 || got.Counts.Hold != 1 || got.Counts.Processing != 1 || got.Counts.ApprovedAmount != 10000 {
				t.Fatalf("scope counts changed across filter: %+v", got.Counts)
			}
			rows := append(got.Available, got.Unavailable...)
			if len(rows) != len(tc.want) {
				t.Fatalf("got %d rows, want IDs %v: %+v", len(rows), tc.want, rows)
			}
			for _, id := range tc.want {
				found := false
				for _, r := range rows {
					if r.ID == id {
						found = true
					}
				}
				if !found {
					t.Errorf("missing request %d", id)
				}
			}
			if tc.status == "approved" && len(got.Unavailable) != 0 {
				t.Fatalf("approved queue offers unavailable rows: %+v", got.Unavailable)
			}
		})
	}
}
