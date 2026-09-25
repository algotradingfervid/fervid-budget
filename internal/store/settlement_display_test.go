package store

import (
	"context"
	"strings"
	"testing"
)

// The 2026-09-25 fix wave, cluster D (settlement). Each test here reproduces
// one of the wave's defects at the store layer before the fix and pins the
// behaviour after it.

// settlementDisplayFixture takes one request to partial_review the way the
// product does — reserved by the accountant, paid short, marked partial — and
// returns the people involved.
func settlementDisplayFixture(t *testing.T, s *Store, ctx context.Context, seq int) (acc, req, mgr User, headID, id int64) {
	t.Helper()
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	var err error
	if mgr, err = s.UserByID(ctx, mgrID); err != nil {
		t.Fatal(err)
	}
	id = seedApprovedRequest(t, s, ctx, seq, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, acc, id, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 300000, VendorPayee: "Acme"}, "partial", "short pay", nil); err != nil {
		t.Fatal(err)
	}
	return acc, req, mgr, headID, id
}

// settlement-8: a concern is a question to Accounts, and the request has to
// say so until Accounts answers. The status never changes (owner decision —
// no new state in the machine); the row carries a flag the display reads.
func TestRaiseConcernMarksTheRequestUnderDiscussionUntilAccountsAnswers(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgr, _, id := settlementDisplayFixture(t, s, ctx, 1)

	before, err := s.Request(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if before.ConcernOpen {
		t.Fatalf("a fresh partial review has no open concern")
	}
	if err := s.RaiseConcern(ctx, mgr, id, "Why was the balance withheld?"); err != nil {
		t.Fatal(err)
	}
	after, err := s.Request(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "partial_review" || !after.ConcernOpen {
		t.Fatalf("after a concern: status=%q concern_open=%v, want partial_review with the concern open", after.Status, after.ConcernOpen)
	}
	// The manager talking to themselves, or the requester chiming in, does not
	// answer the question: it was asked of Accounts.
	if _, err := s.AddRequestComment(ctx, mgr, id, "Still waiting."); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRequestComment(ctx, req, id, "I can wait."); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Request(ctx, id); !r.ConcernOpen {
		t.Fatalf("a comment from the manager or the requester must not close the concern")
	}
	// The accountant who recorded the shortfall answers, and the display returns
	// to the ordinary partial review.
	if _, err := s.AddRequestComment(ctx, acc, id, "The vendor agreed to the retention."); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Request(ctx, id); r.ConcernOpen {
		t.Fatalf("the holder's reply must close the concern")
	}
	// A second concern reopens it; accepting the shortfall ends the discussion.
	if err := s.RaiseConcern(ctx, mgr, id, "Show me the agreement."); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Request(ctx, id); !r.ConcernOpen {
		t.Fatalf("a second concern must reopen the discussion")
	}
	if err := s.AcceptPartial(ctx, mgr, id, ""); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Request(ctx, id); r.ConcernOpen || r.Status != "completed_partial" {
		t.Fatalf("accepting closes the request and the concern: status=%q concern_open=%v", r.Status, r.ConcernOpen)
	}
}

// settlement-3 / settlement-8: "needs me" is the one predicate the design turns
// on. A partial review is the manager's to decide, and an open concern is the
// holder's to answer, so both belong in it.
func TestNeedsMeCountsPartialReviewsForTheManagerAndOpenConcernsForTheHolder(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, _, mgr, _, id := settlementDisplayFixture(t, s, ctx, 1)

	needsMe := func(viewer int64) int {
		n, err := s.CountRequests(ctx, RequestListOptions{Scope: "all", ViewerID: viewer, Bucket: "needs-me"})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := needsMe(mgr.ID); got != 1 {
		t.Fatalf("manager's needs-me with a partial review waiting = %d, want 1", got)
	}
	if got := needsMe(acc.ID); got != 0 {
		t.Fatalf("holder's needs-me with nothing asked of them = %d, want 0", got)
	}
	if err := s.RaiseConcern(ctx, mgr, id, "Why short?"); err != nil {
		t.Fatal(err)
	}
	if got := needsMe(acc.ID); got != 1 {
		t.Fatalf("holder's needs-me with an open concern = %d, want 1", got)
	}
	if got := needsMe(mgr.ID); got != 1 {
		t.Fatalf("the manager still owes the decision while the concern is open: needs-me = %d, want 1", got)
	}
	if _, err := s.AddRequestComment(ctx, acc, id, "Answered."); err != nil {
		t.Fatal(err)
	}
	if got := needsMe(acc.ID); got != 0 {
		t.Fatalf("holder's needs-me after answering = %d, want 0", got)
	}
	// The row is reachable through the assigned scope with the status named,
	// which is what the approvals queue's tab and the dashboard area ask for.
	list, err := s.ListRequests(ctx, RequestListOptions{Scope: "assigned", ViewerID: mgr.ID, Statuses: []string{"partial_review"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != id {
		t.Fatalf("assigned partial_review list = %+v, want the one review", list)
	}
}

// settlement-7: every screen that reads a Request has to be able to name who
// holds it, so the holder's name rides on the row itself rather than on one
// queue query.
func TestRequestRowsCarryTheHolderName(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	id := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	r, err := s.Request(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.ProcessingByName != "" {
		t.Fatalf("unreserved request names a holder: %q", r.ProcessingByName)
	}
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	if r, _ = s.Request(ctx, id); r.ProcessingByName != acc.Name {
		t.Fatalf("Request().ProcessingByName = %q, want %q", r.ProcessingByName, acc.Name)
	}
	list, err := s.ListRequests(ctx, RequestListOptions{Scope: "all", Statuses: []string{"processing"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ProcessingByName != acc.Name {
		t.Fatalf("ListRequests holder name = %+v, want %q", list, acc.Name)
	}
}

// settlement-1: the four settlement templates said {{amount}} — the requested
// figure — where they meant the amount paid. The seeded defaults now say
// {{paid_amount}}, and migration v13 carries the correction onto an installed
// database without touching a template an administrator has edited.
func TestMigrationV13RewritesOnlyTheUneditedSettlementTemplates(t *testing.T) {
	s := newTestStore(t)
	for _, event := range []string{"payment_settled", "payment_partial_review", "payment_partial_accepted", "payment_partial_concern"} {
		var subject, body string
		if err := s.DB().QueryRow(`SELECT subject_template, body_template FROM notification_settings WHERE event=?`, event).Scan(&subject, &body); err != nil {
			t.Fatalf("%s: %v", event, err)
		}
		if strings.Contains(body, "{{amount}}") || strings.Contains(subject, "{{amount}}") {
			t.Fatalf("%s still fills the paid figure from the requested amount:\n%s\n%s", event, subject, body)
		}
		if !strings.Contains(body, "{{paid_amount}}") {
			t.Fatalf("%s body never names the amount paid:\n%s", event, body)
		}
	}
	// An installed database: two rows still on the old default, one the admin
	// rewrote. Only the two are corrected.
	for event, old := range oldSettlementTemplates {
		if _, err := s.DB().Exec(`UPDATE notification_settings SET subject_template=?, body_template=? WHERE event=?`, old.Subject, old.Body, event); err != nil {
			t.Fatal(err)
		}
	}
	custom := "{{approver}} accepted {{number}} — our own wording"
	if _, err := s.DB().Exec(`UPDATE notification_settings SET body_template=? WHERE event='payment_partial_accepted'`, custom); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := UpPaidAmountTemplates(tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("UpPaidAmountTemplates: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"payment_settled", "payment_partial_review", "payment_partial_concern"} {
		var body string
		if err := s.DB().QueryRow(`SELECT body_template FROM notification_settings WHERE event=?`, event).Scan(&body); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, "{{paid_amount}}") {
			t.Fatalf("%s was left on the old default after v13:\n%s", event, body)
		}
	}
	var kept string
	if err := s.DB().QueryRow(`SELECT body_template FROM notification_settings WHERE event='payment_partial_accepted'`).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != custom {
		t.Fatalf("v13 overwrote an administrator's own wording: %q", kept)
	}
}
