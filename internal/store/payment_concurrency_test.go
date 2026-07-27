package store

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// Two people in Accounts settling their own requests at the same moment used to
// take the whole application down, not just lose one of the two writes.
//
// RecordPaymentForRequest opened a deferred transaction and then read before it
// wrote. SQLite will not promote a transaction holding a read snapshot into a
// writer while another writer is active, and does not consult the busy handler
// for that upgrade, so one settlement failed instantly with SQLITE_BUSY. Worse,
// in rollback-journal mode the COMMIT itself waited on other readers, and a
// COMMIT that gives up with SQLITE_BUSY leaves its transaction OPEN: database/sql
// has already marked the Tx done, so `defer tx.Rollback()` returns ErrTxDone
// without rolling back, and the connection goes back to the pool still holding
// the write lock. From then on every writer met "database is locked" and every
// BEGIN on that connection met "cannot start a transaction within a transaction"
// — including the write that login performs, so nobody could sign in until the
// process was restarted.
//
// The fix is three-part: WAL so a COMMIT no longer waits on readers, beginWriteTx
// so the transaction is a writer from its first statement, and validating on the
// caller's own transaction instead of a second pooled connection.
//
// This test asserts both halves: every concurrent settlement succeeds, AND the
// database is still writable afterwards.
func TestConcurrentSettlementsAllSucceedAndLeaveTheDatabaseWritable(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)

	const settlers = 8
	ids := make([]int64, settlers)
	actors := make([]User, settlers)

	for i := 0; i < settlers; i++ {
		id := auditRequest(t, s, ctx, req, mgr, headID, 500000)
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
			_, errs[i] = s.RecordPaymentForRequest(ctx, actors[i], ids[i], PaymentInput{
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
