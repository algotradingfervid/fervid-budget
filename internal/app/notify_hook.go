package app

import (
	"fervidbudget/internal/notify"
	"net/http"
)

// fire delivers one event for a request that has already been written.
//
// It logs and swallows failures on purpose: the workflow action succeeded, and
// an SMTP outage or a broken admin template must not make the approver believe
// their approval failed. The in-app row is written before any email is
// attempted (see notify.Service.Notify), so the record survives a mail failure.
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
	// send points; notify resolves the recipients from the request's status.
	if req.Urgent && (event == notify.EventRequestSubmitted || event == notify.EventRequestApproved) {
		if err := a.notify.Notify(ctx, notify.EventRequestUrgent, req); err != nil {
			a.log.WarnContext(ctx, "urgent notification not delivered",
				"request_id", requestID(r), "payment_request", req.Number, "error", err)
		}
	}
}
