package store

import (
	"context"
	"errors"
	"testing"
)

// Decision D9: `users.default_approver_id` belongs to Phase 1. SetUserDefaultApprover
// is the canonical writer and User.DefaultApproverID (int64, 0 = none) the canonical
// reader — Phase 2 must not ship a second pair of accessors over the same column,
// because that is how the two drift apart. This test pins the contract Phase 2's
// request form actually consumes: the stored default is readable off the loaded
// user, and it is always one of the options ListApprovers offers.
func TestDefaultApproverPreselectsAnOfferedApprover(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	admin := mgr
	grantApprovalPermission(t, s, ctx, mgr.ID)

	// Unset by default: 0 means "no default", and the form pre-selects nothing.
	u, err := s.UserByID(ctx, req.ID)
	if err != nil || u.DefaultApproverID != 0 {
		t.Fatalf("DefaultApproverID = %d, %v; want 0, nil", u.DefaultApproverID, err)
	}
	if err := s.SetUserDefaultApprover(ctx, admin, req.ID, mgr.ID); err != nil {
		t.Fatalf("SetUserDefaultApprover: %v", err)
	}
	u, err = s.UserByID(ctx, req.ID)
	if err != nil || u.DefaultApproverID != mgr.ID {
		t.Fatalf("DefaultApproverID = %d, %v; want %d", u.DefaultApproverID, err, mgr.ID)
	}

	// G9 + G8: whatever is pre-selected must be a legal option, i.e. it must be
	// in the very list the approver control is built from, and never the
	// requester themselves.
	approvers, err := s.ListApprovers(ctx, req.ID)
	if err != nil {
		t.Fatalf("ListApprovers: %v", err)
	}
	offered := false
	for _, a := range approvers {
		if a.ID == req.ID {
			t.Fatal("the requester appears in their own approver list")
		}
		offered = offered || a.ID == u.DefaultApproverID
	}
	if !offered {
		t.Fatalf("the default approver %d is not among the offered approvers %#v", u.DefaultApproverID, approvers)
	}

	// And a request routed to that default approver is accepted end to end.
	if _, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "Site travel", ProjectID: 1, HeadID: headID, Amount: 4500, Purpose: "travel",
		ExpenseDate: "2026-07-21", ManagerID: u.DefaultApproverID}); err != nil {
		t.Fatalf("CreateRequest routed to the default approver: %v", err)
	}

	// A person can never be their own default approver (G8), and a rejection
	// never moves the stored value.
	if err := s.SetUserDefaultApprover(ctx, admin, req.ID, req.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("self as default approver = %v, want ErrValidation", err)
	}
	u, _ = s.UserByID(ctx, req.ID)
	if u.DefaultApproverID != mgr.ID {
		t.Fatalf("a rejected update changed the stored approver to %d", u.DefaultApproverID)
	}

	// A nominee without approval:approve is neither offered nor accepted as a default.
	nobodyID, err := s.CreateUser(ctx, "nobody@example.com", "No Body", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserDefaultApprover(ctx, admin, req.ID, nobodyID); !errors.Is(err, ErrValidation) {
		t.Fatalf("ineligible default accepted: %v", err)
	}
	fresh, err := s.ListApprovers(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range fresh {
		if a.ID == nobodyID {
			t.Fatal("a user without approval:approve was offered as an approver")
		}
	}

	// 0 clears it.
	if err := s.SetUserDefaultApprover(ctx, admin, req.ID, 0); err != nil {
		t.Fatal(err)
	}
	u, _ = s.UserByID(ctx, req.ID)
	if u.DefaultApproverID != 0 {
		t.Fatalf("cleared default = %d, want 0", u.DefaultApproverID)
	}

	audit, err := s.Audit(ctx, "user", req.ID, 5)
	if err != nil || len(audit) == 0 {
		t.Fatalf("audit = %#v, %v; want default-approver entries", audit, err)
	}
}
