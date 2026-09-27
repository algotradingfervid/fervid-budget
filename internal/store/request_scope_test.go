package store

import (
	"context"
	"errors"
	"testing"
)

// scopeFixture seeds a viewer and three requests that stand in different
// relations to them: one they raised, one routed to them as approver, and one
// they have nothing to do with.
type scopeFixture struct {
	s                        *Store
	viewer, requester, other User
	headID                   int64
	raised, routed, stranger int64
}

func newScopeFixture(t *testing.T) scopeFixture {
	t.Helper()
	ctx := context.Background()
	s := newTestStore(t)
	requester, viewer, headID := seedRequestActors(t, s, ctx)
	otherID, err := s.CreateUser(ctx, "other-approver@example.com", "Other Approver", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.UserByID(ctx, otherID)
	return scopeFixture{
		s: s, viewer: viewer, requester: requester, other: other, headID: headID,
		raised:   seedApprovedRequest(t, s, ctx, 1, viewer.ID, other.ID, headID, 1000, 1000),
		routed:   seedApprovedRequest(t, s, ctx, 2, requester.ID, viewer.ID, headID, 2000, 2000),
		stranger: seedApprovedRequest(t, s, ctx, 3, requester.ID, other.ID, headID, 3000, 3000),
	}
}

// The request data scope is one rule written twice, once in Go and once in
// SQL. This table is the contract for both: every scope, against a request the
// viewer raised, one routed to them and one that is neither, and the two forms
// must give the same answer as each other and as the table.
func TestRequestScopeGoAndSQLAgree(t *testing.T) {
	ctx := context.Background()
	f := newScopeFixture(t)
	rows := map[string]int64{"raised": f.raised, "routed": f.routed, "stranger": f.stranger}
	for _, tc := range []struct {
		scope                    string
		raised, routed, stranger bool
	}{
		{ScopeAll, true, true, true},
		{ScopeAssigned, true, true, false},
		{ScopeOwn, true, false, false},
		{"", false, false, false},
		{"nonsense", false, false, false},
	} {
		want := map[string]bool{"raised": tc.raised, "routed": tc.routed, "stranger": tc.stranger}
		for name, id := range rows {
			req, err := f.s.Request(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if got := RequestInScope(tc.scope, f.viewer.ID, req); got != want[name] {
				t.Errorf("RequestInScope(%q, %s) = %v, want %v", tc.scope, name, got, want[name])
			}
			predicate, args := requestScopeSQL("r", tc.scope, f.viewer.ID)
			var n int
			if err := f.s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_requests r WHERE r.id=? AND `+predicate, append([]any{id}, args...)...).Scan(&n); err != nil {
				t.Fatalf("scope %q SQL: %v", tc.scope, err)
			}
			if (n == 1) != want[name] {
				t.Errorf("requestScopeSQL(%q, %s) matched %d rows, want %v", tc.scope, name, n, want[name])
			}
		}
	}
}

// The approvals queue is a filter inside the scope, never a scope of its own:
// an approver with the assigned scope sees the requests they raised in their
// list, but those requests are not waiting on them and stay out of the queue.
func TestRoutedToViewerAndHeldByViewerFilterInsideTheScope(t *testing.T) {
	ctx := context.Background()
	f := newScopeFixture(t)
	ids := func(opts RequestListOptions) map[int64]bool {
		t.Helper()
		opts.ViewerID = f.viewer.ID
		opts.Bucket = "all"
		list, err := f.s.ListRequests(ctx, opts)
		if err != nil {
			t.Fatal(err)
		}
		n, err := f.s.CountRequests(ctx, opts)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(list) {
			t.Fatalf("CountRequests = %d but ListRequests returned %d rows for %+v", n, len(list), opts)
		}
		out := map[int64]bool{}
		for _, r := range list {
			out[r.ID] = true
		}
		return out
	}

	list := ids(RequestListOptions{Scope: ScopeAssigned})
	if !list[f.raised] || !list[f.routed] || list[f.stranger] || len(list) != 2 {
		t.Fatalf("assigned scope list = %v, want the raised and the routed request", list)
	}
	queue := ids(RequestListOptions{Scope: ScopeAssigned, RoutedToViewer: true})
	if queue[f.raised] || !queue[f.routed] || len(queue) != 1 {
		t.Fatalf("approval queue = %v, want only the routed request", queue)
	}
	// A scope that does not reach the routed request hides it from the queue too.
	if got := ids(RequestListOptions{Scope: ScopeOwn, RoutedToViewer: true}); len(got) != 0 {
		t.Fatalf("own scope approval queue = %v, want nothing", got)
	}

	// The viewer holds the stranger request for payment. The held filter finds it
	// inside a scope that reaches it and not inside one that does not.
	if _, err := f.s.DB().ExecContext(ctx, `UPDATE payment_requests SET status='processing', processing_by=? WHERE id=?`, f.viewer.ID, f.stranger); err != nil {
		t.Fatal(err)
	}
	held := ids(RequestListOptions{Scope: ScopeAll, HeldByViewer: true})
	if !held[f.stranger] || len(held) != 1 {
		t.Fatalf("held within all = %v, want only the held request", held)
	}
	if got := ids(RequestListOptions{Scope: ScopeAssigned, HeldByViewer: true}); len(got) != 0 {
		t.Fatalf("held within assigned = %v, want nothing", got)
	}
}

// The store's own write-time checks answer a hidden request the same way as a
// missing one, so a request id cannot be confirmed to exist by trying to act on
// it. Only a request the actor can see may answer "forbidden".
func TestHiddenRequestIsNotFoundInWriteTimeChecks(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, requester, mgrID, headID := seedRequestParty(t, s, ctx)
	id := seedApprovedRequest(t, s, ctx, 1, requester.ID, mgrID, headID, 500000, 500000)
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET treatment='recoverable', type='employee_advance' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}

	outsiderID, err := s.CreateUser(ctx, "outsider@example.com", "Outsider", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	outsider, _ := s.UserByID(ctx, outsiderID)
	roleID, err := s.CreateRole(ctx, acc, "Own-scoped accounts", "")
	if err != nil {
		t.Fatal(err)
	}
	grants := []Grant{{"reservation", "reserve"}, {"payment", "create"}, {"recoverable_report", "view"}}
	if err := s.UpdateRolePermissions(ctx, acc, roleID, grants, []ScopeGrant{{"request", ScopeOwn}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM user_roles WHERE user_id=?`, outsider.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, outsider.ID, roleID); err != nil {
		t.Fatal(err)
	}

	const missing = 987654
	for name, target := range map[string]int64{"hidden": id, "missing": missing} {
		if err := s.ReserveRequest(ctx, outsider, target); !errors.Is(err, ErrNotFound) {
			t.Errorf("ReserveRequest on a %s request = %v, want ErrNotFound", name, err)
		}
		if _, err := s.RecoveryEvents(ctx, outsider, target); !errors.Is(err, ErrNotFound) {
			t.Errorf("RecoveryEvents on a %s request = %v, want ErrNotFound", name, err)
		}
	}
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatalf("the accountant who can see the request still reserves it: %v", err)
	}

	// Visible but not permitted is still a refusal, not a disappearance: once the
	// request is the outsider's own they can see it, but without the recoverable
	// grant they may not read its recovery history.
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET requester_id=? WHERE id=?`, outsider.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id=? AND resource='recoverable_report'`, roleID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecoveryEvents(ctx, outsider, id); !errors.Is(err, ErrForbidden) {
		t.Fatalf("RecoveryEvents without the grant = %v, want ErrForbidden", err)
	}
}
