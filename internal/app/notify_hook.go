package app

// The hook workflow handlers call after a request changes, to send the
// matching in-app and email notifications.

import (
	"net/http"

	"fervidbudget/internal/notify"
)

// fire sends the notifications for event on a request that has already been
// saved. Failures are logged, never returned: the workflow action itself
// succeeded, and a mail outage must not make it look as if it failed. The
// in-app row is written before any email is tried (see notify.Service.Notify),
// so it survives a mail failure. An urgent request also raises the urgent
// event when it is submitted, re-raised or approved.
func (a *App) fire(r *http.Request, event string, reqID int64) {
	if a.notify == nil || reqID == 0 {
		return
	}
	ctx := r.Context()
	req, err := a.st.Request(ctx, reqID)
	if err != nil {
		a.log.WarnContext(ctx, "notification skipped: request unreadable",
			"request_id", requestID(r), "event", event, "payment_request_id", reqID, "error", err)
		return
	}
	if err := a.notify.Notify(ctx, event, req); err != nil {
		a.log.WarnContext(ctx, "notification not delivered",
			"request_id", requestID(r), "event", event, "payment_request", req.Number, "error", err)
	}
	// An urgent request raises its own event on top of the ordinary one, at both
	// send points (a re-raise is a new submission); notify resolves the
	// recipients from the request's status.
	if req.Urgent && (event == notify.EventRequestSubmitted || event == notify.EventRequestReraised || event == notify.EventRequestApproved) {
		if err := a.notify.Notify(ctx, notify.EventRequestUrgent, req); err != nil {
			a.log.WarnContext(ctx, "urgent notification not delivered",
				"request_id", requestID(r), "payment_request", req.Number, "error", err)
		}
	}
}
