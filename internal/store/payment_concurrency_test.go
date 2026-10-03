package store

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// Several people in Accounts settling their own requests at the same moment all
// succeed, and the database is still writable afterwards.
//
// Three things make that hold: WAL, so a COMMIT does not wait on readers; write
// transactions that take the write lock at BEGIN (_txlock=immediate on the DSN),
// because SQLite will not upgrade a read snapshot to a writer while another
// writer is active; and validating on the caller's own transaction rather than a
// second pooled connection. Without them a COMMIT that gives up with SQLITE_BUSY
// can leave its connection holding the write lock, and every later write —
// sign-in included — fails until a restart.
func TestConcurrentSettlementsAllSucceedAndLeaveTheDatabaseWritable(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s)

	const settlers = 8
	ids := make([]int64, settlers)
	actors := make([]User, settlers)

	for i := 0; i < settlers; i++ {
		id := auditRequest(t, s, req, mgr, headID, 500000)
		if err := s.ApproveRequest(ctx, mgr, id, 500000, ""); err != nil {
			t.Fatalf("ApproveRequest: %v", err)
		}
		// A distinct accounts actor per request: reservation is exclusive, and the
		// point here is concurrent settlement, not a fight over one reservation.
		uid, err := s.CreateUser(ctx, accountsEmail(i), "Accounts User", "hash", "data_entry", true)
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		actor, err := s.UserByID(ctx, uid)
		if err != nil {
			t.Fatalf("UserByID: %v", err)
		}
		if err := s.ReserveRequest(ctx, actor, id); err != nil {
			t.Fatalf("ReserveRequest: %v", err)
		}
		ids[i], actors[i] = id, actor
	}

	start := make(chan struct{})
	errs := make([]error, settlers)
	var wg sync.WaitGroup
	for i := 0; i < settlers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = historicalSettlement(ctx, s, actors[i], ids[i], PaymentInput{
				PaidOn:      "2026-07-21",
				Amount:      500000,
				PaymentMode: "bank_transfer",
				ReferenceNo: "UTR-CONCURRENT",
				Now:         time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC),
			}, "settled", "", nil)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("settlement %d failed: %v", i, err)
		}
	}

	// The wedge is the worse half of the bug: a leaked transaction holds the write
	// lock for the life of the process, so this write is the real regression guard.
	// Without a timeout a failure here hangs the suite instead of reporting.
	done := make(chan error, 1)
	go func() {
		_, err := s.CreateUser(ctx, "after-the-storm@example.com", "Later User", "hash", "data_entry", true)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the database is no longer writable after concurrent settlement: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("a write after concurrent settlement blocked for 30s — a transaction was leaked into the pool")
	}

	// Every request must have actually settled, not merely returned nil.
	for i, id := range ids {
		got, err := s.Request(ctx, id)
		if err != nil {
			t.Fatalf("Request: %v", err)
		}
		if got.Status != "completed" {
			t.Errorf("request %d (index %d) status = %q, want completed", id, i, got.Status)
		}
	}
}

func accountsEmail(i int) string {
	return "acct" + strings.Repeat("x", i) + "@example.com"
}
