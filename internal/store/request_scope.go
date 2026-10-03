// request_scope.go defines who may see a payment request (the request data
// scope), once in Go and once in SQL, plus the in-transaction check that
// write paths use.
package store

import (
	"context"
	"database/sql"
)

// The request data scope decides which payment requests a person may see. It is
// one rule, written here once in Go and once in SQL, and every screen, list,
// count, export, notification and write check that asks "may this person see
// this request?" goes through one of the two. The two forms are tested against
// each other so they cannot drift.
//
//   - ScopeAll:      every request in the company.
//   - ScopeAssigned: requests routed to the person for approval (they are the
//     manager on the row) plus the requests they raised themselves. An approver
//     is also a requester, and hiding their own requests behind a second role
//     helps nobody.
//   - ScopeOwn:      only the requests the person raised.
//   - anything else: nothing. The role editor's "None" is stored as no scope row
//     at all, so an empty or unrecognised scope fails closed.
//
// Note that the scope is about seeing. "Which requests are waiting on me as an
// approver?" is a different question, answered by RequestListOptions.RoutedToViewer,
// and never by widening or narrowing a scope word.
const (
	ScopeAssigned = "assigned"
	ScopeOwn      = "own"
)

// RequestInScope reports whether a viewer holding the given request data scope
// may see request r. It is the Go form of the rule described above.
func RequestInScope(scope string, viewerID int64, r Request) bool {
	switch scope {
	case ScopeAll:
		return true
	case ScopeAssigned:
		return r.ManagerID == viewerID || r.RequesterID == viewerID
	case ScopeOwn:
		return r.RequesterID == viewerID
	default:
		return false
	}
}

// requestScopeSQL is the SQL form of RequestInScope: a predicate over the
// payment_requests row aliased as alias, with its bound arguments. It is never
// empty — an unrestricted scope yields the constant 1 and a refused scope the
// constant 0 — so a caller can always write `AND ` + predicate.
func requestScopeSQL(alias, scope string, viewerID int64) (string, []any) {
	switch scope {
	case ScopeAll:
		return `1`, nil
	case ScopeAssigned:
		return `(` + alias + `.manager_id=? OR ` + alias + `.requester_id=?)`, []any{viewerID, viewerID}
	case ScopeOwn:
		return alias + `.requester_id=?`, []any{viewerID}
	default:
		return `0`, nil
	}
}

// requestVisibleTx reads request requestID inside tx and answers ErrNotFound
// unless the actor, with the roles and activity they hold at this very moment,
// may see it. A missing row and a hidden row are deliberately indistinguishable
// so that request ids cannot be probed. Callers layer their own conditions —
// grants, the request's treatment, who holds the reservation — on the returned
// request and permission set.
//
// The permissions are read inside the same transaction as the write that
// follows, so a role change cannot race a form that was opened earlier.
func requestVisibleTx(ctx context.Context, tx *sql.Tx, actorID, requestID int64) (Request, PermissionSet, error) {
	req, err := requestInTx(ctx, tx, requestID)
	if err != nil {
		return Request{}, nil, err
	}
	ps, err := effectivePermissionsQ(ctx, tx, actorID)
	if err != nil {
		return Request{}, nil, err
	}
	if !RequestInScope(ps.Scope("request"), actorID, req) {
		return Request{}, nil, ErrNotFound
	}
	return req, ps, nil
}
