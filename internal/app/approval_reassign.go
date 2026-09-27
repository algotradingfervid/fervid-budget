package app

// Handing a pending request's approval on to a different approver.

import (
	"fmt"
	"net/http"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/notify"
)

// requestReassignApprover hands a pending request to a different approver, with
// a reason, and records it as its own `approval_reassign` event (coverage A7).
//
// store.ReassignRequest has always carried every rule this needs — pending only,
// a real approver at the other end, G8, a mandatory reason, its own audit action
// — and had no HTTP caller at all, so `approval:reassign` was a grant an
// administrator could give and revoke with nothing behind it (F-A-06/F-C-02).
// This is also the repair for a request routed to somebody who cannot approve
// (F-A-08) and for a stranded approval (F-G-025).
//
// Who may do it: the route asks for `approval:reassign`, the caller's request
// data scope must reach the row, and then mayReassignApprover decides — the
// request's own approver handing it on, or a holder of user:edit rescuing one
// whose approver cannot act. The gate used to stop at the scope, and the seeded
// Manager role holds both approval:reassign and request scope `all`, so any
// manager could take another manager's request for themselves and approve it
// (rbac-8). The store refuses the self-assignment as well, whoever the actor is.
func (a *App) requestReassignApprover(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	if !a.mayReassignApprover(u, req) {
		a.respondError(w, r, http.StatusForbidden,
			"Only the approver this request was sent to, or an administrator, can reassign it.", nil)
		return
	}
	to := parseID(r.FormValue("manager_id"))
	if to == 0 {
		a.respondError(w, r, http.StatusBadRequest, "Choose the approver this request should go to.", nil)
		return
	}
	if strings.TrimSpace(r.FormValue("reason")) == "" {
		a.respondError(w, r, http.StatusBadRequest, "Give a reason for the reassignment.", nil)
		return
	}
	// The store re-checks "is this still the approver" on the row inside its
	// write transaction, because req above was read before it; only the
	// admin-level grant mayReassignApprover honours lets that check be skipped.
	if err := a.st.ReassignRequest(r.Context(), u, req.ID, to, r.FormValue("reason"), a.auth.Can(u, "user", "edit")); err != nil {
		status := storeErrorStatus(err)
		if status >= http.StatusInternalServerError {
			a.respondStoreError(w, r, err)
			return
		}
		a.respondError(w, r, status, friendly(err), err)
		return
	}
	// The sheet says "The new approver is told". Fired after the store call, so
	// the notification addresses the manager_id ReassignRequest has just written
	// rather than the one it replaced.
	a.fire(r, notify.EventApproverReassigned, req.ID)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
}
