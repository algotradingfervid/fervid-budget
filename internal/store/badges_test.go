package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func newBadgeStore(t *testing.T) (*Store, context.Context, User, int64) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "badges.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := t.Context()
	actorID, err := s.CreateUser(ctx, "badges@example.test", "Badge Actor", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := s.UserByID(ctx, actorID)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := s.UpsertProject(ctx, 0, "Badge Project", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	headID, err := s.UpsertHead(ctx, 0, projectID, "Badge Head", "5", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	return s, ctx, actor, headID
}

func badgeKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestBadgeCountsRunOnlyThePermittedSubSelects(t *testing.T) {
	s, ctx, actor, headID := newBadgeStore(t)
	for i := 0; i < 2; i++ {
		if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-04-10", Amount: 1000, VendorPayee: "Badge Vendor", PaymentMode: "cash"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetBudget(ctx, actor, headID, "2026-04", 50000); err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		name     string
		perms    PermissionSet
		wantKeys []string
		want     map[string]int
	}{
		{
			name:     "wildcard runs every sub-select",
			perms:    NewPermissionSet([]Grant{{Resource: "*", Action: "*"}}),
			wantKeys: []string{"my_payments", "open_months", "receipts_missing"},
			want:     map[string]int{"my_payments": 2, "open_months": 1, "receipts_missing": 2},
		},
		{
			name:     "payment view only counts payments",
			perms:    NewPermissionSet([]Grant{{Resource: "payment", Action: "view"}}),
			wantKeys: []string{"my_payments"},
			want:     map[string]int{"my_payments": 2},
		},
		{
			name:     "attachment creator only counts missing receipts",
			perms:    NewPermissionSet([]Grant{{Resource: "attachment", Action: "create"}}),
			wantKeys: []string{"receipts_missing"},
			want:     map[string]int{"receipts_missing": 2},
		},
		{
			name:     "no grants queries nothing",
			perms:    NewPermissionSet(nil),
			wantKeys: []string{},
		},
		{
			name:     "nil permission set queries nothing",
			perms:    nil,
			wantKeys: []string{},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			counts, err := s.BadgeCounts(ctx, actor.ID, testCase.perms)
			if err != nil {
				t.Fatal(err)
			}
			if got := badgeKeys(counts); strings.Join(got, "|") != strings.Join(testCase.wantKeys, "|") {
				t.Fatalf("badge keys = %v, want %v", got, testCase.wantKeys)
			}
			for key, want := range testCase.want {
				if counts[key] != want {
					t.Fatalf("counts[%q] = %d, want %d", key, counts[key], want)
				}
			}
		})
	}
}

func TestBadgeCountsAreMemoisedPerUserForTheCacheWindow(t *testing.T) {
	s, ctx, actor, headID := newBadgeStore(t)
	perms := NewPermissionSet([]Grant{{Resource: "*", Action: "*"}})
	for i := 0; i < 2; i++ {
		if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-04-10", Amount: 1000, VendorPayee: "Cache Vendor", PaymentMode: "cash"}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.BadgeCounts(ctx, actor.ID, perms)
	if err != nil {
		t.Fatal(err)
	}
	if first["my_payments"] != 2 {
		t.Fatalf("first call my_payments = %d, want 2", first["my_payments"])
	}

	// A third payment lands. Inside the memo window the counts must not be
	// re-queried, which is only observable as the stale value coming back.
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-04-11", Amount: 2000, VendorPayee: "Cache Vendor", PaymentMode: "cash"}); err != nil {
		t.Fatal(err)
	}
	cached, err := s.BadgeCounts(ctx, actor.ID, perms)
	if err != nil {
		t.Fatal(err)
	}
	if cached["my_payments"] != 2 {
		t.Fatalf("cached call my_payments = %d, want the memoised 2", cached["my_payments"])
	}

	// Mutating the returned map must not corrupt the memo.
	cached["my_payments"] = 99
	again, err := s.BadgeCounts(ctx, actor.ID, perms)
	if err != nil {
		t.Fatal(err)
	}
	if again["my_payments"] != 2 {
		t.Fatalf("memo was corrupted by a caller: my_payments = %d, want 2", again["my_payments"])
	}

	// A different user does not read another user's memo.
	otherID, err := s.CreateUser(ctx, "other@example.test", "Other", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.BadgeCounts(ctx, otherID, perms)
	if err != nil {
		t.Fatal(err)
	}
	if other["my_payments"] != 0 {
		t.Fatalf("other user my_payments = %d, want 0", other["my_payments"])
	}

	restore := badgeCacheTTL
	badgeCacheTTL = 0
	t.Cleanup(func() { badgeCacheTTL = restore })
	fresh, err := s.BadgeCounts(ctx, actor.ID, perms)
	if err != nil {
		t.Fatal(err)
	}
	if fresh["my_payments"] != 3 {
		t.Fatalf("expired memo my_payments = %d, want 3", fresh["my_payments"])
	}
	if badgeCacheTTL != 0 || restore != 15*time.Second {
		t.Fatalf("badge memo window = %s, want 15s", restore)
	}
}

func TestBadgeCountsAreSafeUnderConcurrentReaders(t *testing.T) {
	s, ctx, actor, headID := newBadgeStore(t)
	perms := NewPermissionSet([]Grant{{Resource: "*", Action: "*"}})
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-04-10", Amount: 1000, VendorPayee: "Race Vendor", PaymentMode: "cash"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counts, err := s.BadgeCounts(ctx, actor.ID, perms)
			if err != nil {
				errs <- err
				return
			}
			if counts["my_payments"] != 1 {
				errs <- fmt.Errorf("concurrent my_payments = %d, want 1", counts["my_payments"])
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
