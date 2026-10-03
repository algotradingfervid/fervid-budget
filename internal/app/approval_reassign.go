package app

// Handing a pending request's approval on to a different approver.

import (
	"fmt"
	"net/http"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/notify"
)

// requestReassignApprover hands a pending request to a different approver,
// with a reason, recorded in the request's history as "Approval reassigned".
//
// The route needs approval:reassign and the request must be in the caller's
// request scope. mayReassignApprover then allows only the request's own
// approver, or a holder of user:edit rescuing a request whose approver cannot
// act; otherwise any manager could take another manager's request and approve
// it themselves. The store also refuses reassigning a request to yourself or
// to its own requester. The new approver is notified.
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
	// The store checks again, inside its write transaction, that the caller is
	// still the request's approver, unless the caller holds user:edit.
	if err := a.st.ReassignRequest(r.Context(), u, req.ID, to, r.FormValue("reason"), a.auth.Can(u, "user", "edit")); err != nil {
		if status, fixable := userError(err); fixable {
			a.respondError(w, r, status, friendly(err), err)
		} else {
			a.respondStoreError(w, r, err)
		}
		return
	}
	// Notify after the write, so the message goes to the new approver.
	a.fire(r, notify.EventApproverReassigned, req.ID)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
}
