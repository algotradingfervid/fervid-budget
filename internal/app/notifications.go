package app

import (
	"net/http"
	"strconv"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

// The admin notification rules screen (D7).
//
// /admin/notifications is the rule editor; /notifications is the user's own
// centre. They are two different screens with two different audiences, which is
// why the rule editor is the one behind a permission verb.
//
// In-app delivery is not configurable here: it always fires, so the screen
// shows a static "On" pill rather than a control that would imply otherwise.
// Email is the only per-event switch.

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

func (a *App) adminNotifications(w http.ResponseWriter, r *http.Request) {
	a.renderAdminNotifications(w, r, http.StatusOK, "", "")
}

func (a *App) renderAdminNotifications(w http.ResponseWriter, r *http.Request, status int, errMsg, notice string) {
	settings, err := a.st.AllNotificationSettings(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	mail, err := a.st.GetMailSettings(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.renderStatus(w, r, status, "admin_notifications", PageData{
		Title: "Notification rules", NotifSettings: settings, MailCfg: mail,
		NotifFields: notify.NotifyFieldNames(), Error: errMsg, Notice: notice,
	})
}

func (a *App) adminNotificationsSMTP(w http.ResponseWriter, r *http.Request) {
	port, _ := strconv.Atoi(r.FormValue("smtp_port"))
	in := store.MailSettings{
		SMTPHost:             r.FormValue("smtp_host"),
		SMTPPort:             port,
		SMTPUsername:         r.FormValue("smtp_username"),
		SMTPFromName:         r.FormValue("smtp_from_name"),
		SMTPFromAddr:         r.FormValue("smtp_from_addr"),
		ManagementRecipients: r.FormValue("management_recipients"),
		BaseURL:              r.FormValue("base_url"),
	}
	if err := a.st.SetMailSettings(r.Context(), auth.CurrentUser(r), in); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/notifications", http.StatusSeeOther)
}

// adminNotificationEventSave writes one event rule. Templates are validated
// before they are stored, so a typo fails here rather than silently emitting an
// empty string into an email nobody can debug later.
func (a *App) adminNotificationEventSave(w http.ResponseWriter, r *http.Request) {
	event := r.PathValue("event")
	in := store.NotificationSetting{
		Event:            event,
		EmailEnabled:     r.FormValue("email_enabled") == "on",
		ToRecipients:     r.FormValue("to_recipients"),
		CcRecipients:     r.FormValue("cc_recipients"),
		IncludeRequester: r.FormValue("include_requester") == "on",
		IncludeManager:   r.FormValue("include_manager") == "on",
		IncludeAccounts:  r.FormValue("include_accounts") == "on",
		SubjectTemplate:  r.FormValue("subject_template"),
		BodyTemplate:     r.FormValue("body_template"),
	}
	for _, tmpl := range []string{in.SubjectTemplate, in.BodyTemplate} {
		if err := notify.ValidateTemplate(tmpl); err != nil {
			a.renderAdminNotifications(w, r, http.StatusBadRequest, err.Error(), "")
			return
		}
	}
	if err := a.st.SetNotificationSetting(r.Context(), auth.CurrentUser(r), in); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/notifications", http.StatusSeeOther)
}

// adminNotificationsTest proves the SMTP settings work without waiting for a
// real event. It reports the transport error rather than swallowing it — a test
// send that silently "succeeds" is worse than no test at all.
func (a *App) adminNotificationsTest(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	to := strings.TrimSpace(r.FormValue("test_to"))
	if to == "" {
		to = user.Email
	}
	mail, err := a.st.GetMailSettings(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	from := strings.TrimSpace(mail.SMTPFromAddr)
	if name := strings.TrimSpace(mail.SMTPFromName); name != "" && from != "" {
		from = name + " <" + from + ">"
	}
	err = a.mailer.Send(r.Context(), notify.Message{
		From: from, To: []string{to},
		Subject: "Fervid Budget test email",
		Body:    "This is a test from the notification rules screen. If you are reading it, SMTP is configured correctly.",
	})
	if err != nil {
		a.renderAdminNotifications(w, r, http.StatusBadGateway, "The test email could not be sent: "+err.Error(), "")
		return
	}
	a.renderAdminNotifications(w, r, http.StatusOK, "", "Test email sent to "+to+".")
}
