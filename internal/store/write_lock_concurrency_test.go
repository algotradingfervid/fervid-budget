package store

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// Every write transaction takes SQLite's write lock at BEGIN (the DSN says
// _txlock=immediate), so a second writer waits on busy_timeout instead of
// failing. Before that rule, a transaction that read first and wrote second was
// promoted to a writer only if nobody else was writing; SQLite does not consult
// the busy handler for that promotion, so the loser failed straight away with
// "database is locked". These tests race exactly such read-then-write methods.

// Many wrong passwords for one account at the same moment must all be counted:
// RecordFailedLogin reads the attempt count and then writes it back.
func TestConcurrentFailedLoginsAreAllCounted(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	const email = "victim@example.com"
	if _, err := s.CreateUser(ctx, email, "Victim", "hash", "data_entry", true); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	const attempts = 24
	start := make(chan struct{})
	errs := make([]error, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = s.RecordFailedLogin(ctx, email)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("failed login %d was not recorded: %v", i, err)
		}
	}

	var count int
	var locked bool
	if err := s.DB().QueryRowContext(ctx, `SELECT attempt_count, locked IS NOT NULL FROM users WHERE email=?`, email).Scan(&count, &locked); err != nil {
		t.Fatalf("read attempt count: %v", err)
	}
	if count != attempts {
		t.Errorf("attempt_count = %d, want %d: a concurrent increment was lost", count, attempts)
	}
	if !locked {
		t.Error("account is not locked after five or more failures")
	}
}

// Releasing a reservation and reassigning it race on the same request. Exactly
// one may win; the other must be told why in plain words (a wrapped
// ErrForbidden), never with a driver error, and the request must end in the
// winner's state.
func TestReleaseAndReassignRaceHasOneWinner(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s)
	holder := newAccountsUser(t, s, "holder@example.com")
	successor := newAccountsUser(t, s, "successor@example.com")

	for round := 0; round < 10; round++ {
		id := auditRequest(t, s, req, mgr, headID, 100000)
		if err := s.ApproveRequest(ctx, mgr, id, 100000, ""); err != nil {
			t.Fatalf("ApproveRequest: %v", err)
		}
		if err := s.ReserveRequest(ctx, holder, id); err != nil {
			t.Fatalf("ReserveRequest: %v", err)
		}

		start := make(chan struct{})
		var releaseErr, reassignErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			releaseErr = s.ReleaseRequest(ctx, holder, id, "no payment was started", true, false)
		}()
		go func() {
			defer wg.Done()
			<-start
			reassignErr = s.ReassignReservation(ctx, mgr, id, successor.ID, "handing over", true)
		}()
		close(start)
		wg.Wait()

		got, err := s.Request(ctx, id)
		if err != nil {
			t.Fatalf("Request: %v", err)
		}
		switch {
		case releaseErr == nil && reassignErr == nil:
			t.Fatalf("round %d: both release and reassign succeeded", round)
		case releaseErr == nil:
			if !errors.Is(reassignErr, ErrForbidden) {
				t.Fatalf("round %d: reassign lost with %v, want a wrapped ErrForbidden", round, reassignErr)
			}
			if got.Status != "approved" || got.ProcessingBy != nil {
				t.Fatalf("round %d: release won but request is %s held by %v", round, got.Status, got.ProcessingBy)
			}
		case reassignErr == nil:
			if !errors.Is(releaseErr, ErrForbidden) {
				t.Fatalf("round %d: release lost with %v, want a wrapped ErrForbidden", round, releaseErr)
			}
			if got.Status != "processing" || got.ProcessingBy == nil || *got.ProcessingBy != successor.ID {
				t.Fatalf("round %d: reassign won but request is %s held by %v", round, got.Status, got.ProcessingBy)
			}
		default:
			t.Fatalf("round %d: neither operation succeeded: release %v; reassign %v", round, releaseErr, reassignErr)
		}
	}
}
