package store

import (
	"errors"
	"strings"
	"testing"
)

// routingWorld is the cast of the approver-choice and re-raise tests: a
// requester whose default approver is def, and other, a second person who may
// approve but is not the requester's default.
type routingWorld struct {
	s         *Store
	requester User
	def       User
	other     User
	headID    int64
	projectID int64
}

func newRoutingWorld(t *testing.T) routingWorld {
	t.Helper()
	s := newTestStore(t)
	requester, def, headID := seedRequestActors(t, s)
	other := seedUser(t, s, "other@example.com", "Omar Other", "admin")
	if _, err := s.DB().ExecContext(t.Context(), `UPDATE users SET default_approver_id=? WHERE id=?`, def.ID, requester.ID); err != nil {
		t.Fatal(err)
	}
	return routingWorld{s: s, requester: requester, def: def, other: other, headID: headID, projectID: projectOfHead(t, s, headID)}
}

// setSetting writes one app_settings value straight to the table.
func setSetting(t *testing.T, s *Store, key, value string) {
	t.Helper()
	if _, err := s.DB().ExecContext(t.Context(), `INSERT INTO app_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
		t.Fatal(err)
	}
}

// execSQL runs one statement that plants a state the test needs.
func execSQL(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.DB().ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// reimbursement is a valid budget request routed to managerID.
func (w routingWorld) reimbursement(managerID int64) RequestInput {
	return RequestInput{Treatment: "budget", Type: "reimbursement", ShortTitle: "Site visit",
		ProjectID: w.projectID, HeadID: w.headID, Amount: 120000, Purpose: "site visit",
		ExpenseDate: "2026-07-01", ManagerID: managerID}
}

func (w routingWorld) create(t *testing.T, in RequestInput) int64 {
	t.Helper()
	id, err := w.s.CreateRequest(t.Context(), w.requester, in)
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	return id
}

// rejected raises in and has its approver reject it.
func (w routingWorld) rejected(t *testing.T, in RequestInput) int64 {
	t.Helper()
	id := w.create(t, in)
	approver, err := w.s.UserByID(t.Context(), in.ManagerID)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.s.RejectRequest(t.Context(), approver, id, "not this month"); err != nil {
		t.Fatalf("RejectRequest: %v", err)
	}
	return id
}

func requestCount(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM payment_requests`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// With approver choice off, a new request must go to the requester's default
// approver; a hand-made POST naming anybody else is refused, and so is any
// request from a requester who has no default approver.
func TestApproverChoiceOffCreateNeedsTheDefaultApprover(t *testing.T) {
	w := newRoutingWorld(t)
	ctx := t.Context()
	setSetting(t, w.s, "allow_approver_choice", "0")

	_, err := w.s.CreateRequest(ctx, w.requester, w.reimbursement(w.other.ID))
	if !errors.Is(err, ErrApproverNotDefault) || !errors.Is(err, ErrValidation) {
		t.Fatalf("create routed to a non-default approver = %v, want ErrApproverNotDefault", err)
	}
	if n := requestCount(t, w.s); n != 0 {
		t.Fatalf("refused create left %d requests", n)
	}
	id := w.create(t, w.reimbursement(w.def.ID))
	got, _ := w.s.Request(ctx, id)
	if got.ManagerID != w.def.ID {
		t.Fatalf("approver = %d, want default %d", got.ManagerID, w.def.ID)
	}

	execSQL(t, w.s, `UPDATE users SET default_approver_id=NULL WHERE id=?`, w.requester.ID)
	if _, err := w.s.CreateRequest(ctx, w.requester, w.reimbursement(w.def.ID)); !errors.Is(err, ErrApproverNotDefault) {
		t.Fatalf("create with no default approver = %v, want ErrApproverNotDefault", err)
	}
}

// With approver choice on (the default), any person who may approve can be
// chosen, on create and on edit.
func TestApproverChoiceOnAllowsAnyEligibleApprover(t *testing.T) {
	w := newRoutingWorld(t)
	ctx := t.Context()
	setSetting(t, w.s, "allow_approver_choice", "1")
	id := w.create(t, w.reimbursement(w.other.ID))
	if err := w.s.UpdateRequest(ctx, w.requester, id, w.reimbursement(w.def.ID)); err != nil {
		t.Fatalf("edit to another eligible approver: %v", err)
	}
	if err := w.s.EditRequest(ctx, w.requester, id, RequestEdit{Input: w.reimbursement(w.other.ID)}); err != nil {
		t.Fatalf("EditRequest to another eligible approver: %v", err)
	}
	// Choice on still never allows an approver who cannot approve.
	_, err := w.s.CreateRequest(ctx, w.requester, w.reimbursement(w.requester.ID))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("self-approval with choice on = %v, want ErrValidation", err)
	}
}

// With approver choice off, an edit (save, or save-and-resubmit of a returned
// request) may not move the request to anybody but the default approver.
// Keeping the approver the request already has is not a choice and stays
// allowed, so a request an approver reassigned, or one raised while choice was
// on, can still be corrected.
func TestApproverChoiceOffEditAndResubmitNeedTheDefaultApprover(t *testing.T) {
	w := newRoutingWorld(t)
	ctx := t.Context()
	setSetting(t, w.s, "allow_approver_choice", "0")
	id := w.create(t, w.reimbursement(w.def.ID))

	err := w.s.EditRequest(ctx, w.requester, id, RequestEdit{Input: w.reimbursement(w.other.ID)})
	if !errors.Is(err, ErrApproverNotDefault) {
		t.Fatalf("edit to a non-default approver = %v, want ErrApproverNotDefault", err)
	}
	if err := w.s.UpdateRequest(ctx, w.requester, id, w.reimbursement(w.other.ID)); !errors.Is(err, ErrApproverNotDefault) {
		t.Fatalf("UpdateRequest to a non-default approver = %v, want ErrApproverNotDefault", err)
	}
	if got, _ := w.s.Request(ctx, id); got.ManagerID != w.def.ID {
		t.Fatalf("refused edit moved the request to %d", got.ManagerID)
	}

	// Returned, then corrected and resubmitted in one press.
	if err := w.s.ReturnRequest(ctx, w.def, id, "add the ticket"); err != nil {
		t.Fatalf("ReturnRequest: %v", err)
	}
	err = w.s.EditRequest(ctx, w.requester, id, RequestEdit{Input: w.reimbursement(w.other.ID), Resubmit: true})
	if !errors.Is(err, ErrApproverNotDefault) {
		t.Fatalf("resubmit to a non-default approver = %v, want ErrApproverNotDefault", err)
	}
	if got, _ := w.s.Request(ctx, id); got.Status != "returned" || got.ManagerID != w.def.ID {
		t.Fatalf("refused resubmit changed the request: status %q approver %d", got.Status, got.ManagerID)
	}
	if err := w.s.EditRequest(ctx, w.requester, id, RequestEdit{Input: w.reimbursement(w.def.ID), Resubmit: true}); err != nil {
		t.Fatalf("resubmit to the default approver: %v", err)
	}

	// Raised to other while choice was on; choice then switched off. Keeping
	// other is allowed, moving to a third person who is not the default is not.
	setSetting(t, w.s, "allow_approver_choice", "1")
	kept := w.create(t, w.reimbursement(w.other.ID))
	setSetting(t, w.s, "allow_approver_choice", "0")
	if err := w.s.EditRequest(ctx, w.requester, kept, RequestEdit{Input: w.reimbursement(w.other.ID)}); err != nil {
		t.Fatalf("edit keeping the request's current approver: %v", err)
	}
	third := seedUser(t, w.s, "third@example.com", "Tara Third", "admin")
	if err := w.s.EditRequest(ctx, w.requester, kept, RequestEdit{Input: w.reimbursement(third.ID)}); !errors.Is(err, ErrApproverNotDefault) {
		t.Fatalf("edit to a third non-default approver = %v, want ErrApproverNotDefault", err)
	}
}

// Re-raising with approver choice off needs the default approver too: the new
// request is a new routing decision.
func TestReraiseHonoursApproverChoice(t *testing.T) {
	w := newRoutingWorld(t)
	ctx := t.Context()
	toOther := w.rejected(t, w.reimbursement(w.other.ID))
	toDefault := w.rejected(t, w.reimbursement(w.def.ID))
	setSetting(t, w.s, "allow_approver_choice", "0")
	before := requestCount(t, w.s)
	if _, err := w.s.ReraiseRequest(ctx, w.requester, toOther); !errors.Is(err, ErrApproverNotDefault) {
		t.Fatalf("re-raise to a non-default approver = %v, want ErrApproverNotDefault", err)
	}
	if n := requestCount(t, w.s); n != before {
		t.Fatalf("refused re-raise wrote a request (%d -> %d)", before, n)
	}
	if _, err := w.s.ReraiseRequest(ctx, w.requester, toDefault); err != nil {
		t.Fatalf("re-raise to the default approver: %v", err)
	}
	setSetting(t, w.s, "allow_approver_choice", "1")
	if _, err := w.s.ReraiseRequest(ctx, w.requester, toOther); err != nil {
		t.Fatalf("re-raise to another eligible approver with choice on: %v", err)
	}
}

// A re-raise is validated exactly as a new request with the same fields: for
// each thing that went stale after the rejection, the re-raise is refused with
// the very message CreateRequest gives, and no request is written.
func TestReraiseRefusesWhatANewRequestWouldRefuse(t *testing.T) {
	cases := []struct {
		name  string
		input func(w routingWorld, vendorID int64) RequestInput
		stale func(t *testing.T, w routingWorld, vendorID int64)
		want  string
	}{
		{
			name:  "approver lost approval:approve",
			input: func(w routingWorld, _ int64) RequestInput { return w.reimbursement(w.def.ID) },
			stale: func(t *testing.T, w routingWorld, _ int64) {
				execSQL(t, w.s, `DELETE FROM user_roles WHERE user_id=?`, w.def.ID)
			},
			want: "that person cannot approve requests",
		},
		{
			name:  "approver inactive",
			input: func(w routingWorld, _ int64) RequestInput { return w.reimbursement(w.def.ID) },
			stale: func(t *testing.T, w routingWorld, _ int64) {
				execSQL(t, w.s, `UPDATE users SET active=0 WHERE id=?`, w.def.ID)
			},
			want: "that person cannot approve requests",
		},
		{
			name:  "head's project retired",
			input: func(w routingWorld, _ int64) RequestInput { return w.reimbursement(w.def.ID) },
			stale: func(t *testing.T, w routingWorld, _ int64) {
				execSQL(t, w.s, `UPDATE projects SET active=0 WHERE id=?`, w.projectID)
			},
			want: "that budget head has been retired",
		},
		{
			name: "project retired (recoverable with no head)",
			input: func(w routingWorld, _ int64) RequestInput {
				return RequestInput{Treatment: "recoverable", Type: "recoverable", RecoverableCategory: "emd",
					ShortTitle: "Tender EMD", ProjectID: w.projectID, Amount: 50000, Purpose: "tender deposit",
					ExpectedReturnDate: "2026-12-31", RepaymentNotes: "refund after award",
					VendorPayee: "Tender Authority", ManagerID: w.def.ID}
			},
			stale: func(t *testing.T, w routingWorld, _ int64) {
				execSQL(t, w.s, `UPDATE projects SET active=0 WHERE id=?`, w.projectID)
			},
			want: "that project has been retired",
		},
		{
			name:  "head retired",
			input: func(w routingWorld, _ int64) RequestInput { return w.reimbursement(w.def.ID) },
			stale: func(t *testing.T, w routingWorld, _ int64) {
				execSQL(t, w.s, `UPDATE heads SET active=0 WHERE id=?`, w.headID)
			},
			want: "that budget head has been retired",
		},
		{
			name: "vendor inactive",
			input: func(w routingWorld, vendorID int64) RequestInput {
				return RequestInput{Treatment: "budget", Type: "vendor_advance", ShortTitle: "Booking advance",
					ProjectID: w.projectID, HeadID: w.headID, Amount: 80000, Purpose: "advance",
					VendorID: vendorID, AdvanceReason: "hall booking", ManagerID: w.def.ID}
			},
			stale: func(t *testing.T, w routingWorld, vendorID int64) {
				execSQL(t, w.s, `UPDATE vendors SET status='inactive' WHERE id=?`, vendorID)
			},
			want: "that vendor is no longer active",
		},
		{
			name: "urgency switched off",
			input: func(w routingWorld, _ int64) RequestInput {
				in := w.reimbursement(w.def.ID)
				in.Urgent, in.UrgencyReason = true, "travel tomorrow"
				return in
			},
			stale: func(t *testing.T, w routingWorld, _ int64) {
				setSetting(t, w.s, "urgency_mode", "disabled")
			},
			want: "urgent requests are switched off",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newRoutingWorld(t)
			ctx := t.Context()
			vendorID := seedTestVendor(t, w.s, "Acme Supplies")
			in := tc.input(w, vendorID)
			id := w.rejected(t, in)
			tc.stale(t, w, vendorID)
			before := requestCount(t, w.s)

			_, err := w.s.ReraiseRequest(ctx, w.requester, id)
			if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("re-raise = %v, want ErrValidation containing %q", err, tc.want)
			}
			_, createErr := w.s.CreateRequest(ctx, w.requester, in)
			if createErr == nil || createErr.Error() != err.Error() {
				t.Fatalf("re-raise refusal %q differs from a new request's %v", err, createErr)
			}
			if n := requestCount(t, w.s); n != before {
				t.Fatalf("refused re-raise wrote a request (%d -> %d)", before, n)
			}
			if got, _ := w.s.Request(ctx, id); got.Status != "rejected" {
				t.Fatalf("source status = %q, want rejected", got.Status)
			}
		})
	}
}

// A rejected request that is still valid re-raises as a new pending request
// with its own number, its fields copied and the approver unchanged.
func TestReraiseOfAValidRequestStillWorks(t *testing.T) {
	w := newRoutingWorld(t)
	ctx := t.Context()
	setSetting(t, w.s, "allow_approver_choice", "0")
	in := w.reimbursement(w.def.ID)
	in.Urgent, in.UrgencyReason = true, "travel tomorrow"
	id := w.rejected(t, in)
	newID, err := w.s.ReraiseRequest(ctx, w.requester, id)
	if err != nil {
		t.Fatalf("ReraiseRequest: %v", err)
	}
	orig, _ := w.s.Request(ctx, id)
	fresh, err := w.s.Request(ctx, newID)
	if err != nil {
		t.Fatal(err)
	}
	if newID == id || fresh.Number == orig.Number || fresh.Number == "" {
		t.Fatalf("re-raise did not make a new numbered request: %q from %q", fresh.Number, orig.Number)
	}
	if fresh.Status != "pending" || fresh.ManagerID != w.def.ID || fresh.Amount != in.Amount ||
		!fresh.Urgent || fresh.UrgencyReason != in.UrgencyReason || fresh.ShortTitle != in.ShortTitle {
		t.Fatalf("re-raised copy = %+v", fresh)
	}
	if orig.Status != "rejected" {
		t.Fatalf("source status = %q, want rejected", orig.Status)
	}
}
