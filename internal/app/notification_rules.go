package app

// The admin notification rules screen (/admin/notifications, behind the
// notification permission): the SMTP sender settings, one email rule per
// workflow event, and a test send. It is separate from the user's own
// notification centre (/notifications, notification_centre.go). In-app
// notifications always go out, so email is the only per-event switch here.

import (
	"net/http"
	"strconv"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

// adminNotifications shows the rules screen.
func (a *App) adminNotifications(w http.ResponseWriter, r *http.Request) {
	a.renderAdminNotificationsPage(w, r, http.StatusOK, PageData{})
}

// renderAdminNotificationsPage loads the event rules and mail settings and
// renders the rules screen with status. pd carries any message (Error or
// Notice, shown in the layout's flash) or a rejected rule draft to reopen.
func (a *App) renderAdminNotificationsPage(w http.ResponseWriter, r *http.Request, status int, pd PageData) {
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
	pd.Title, pd.NotifSettings, pd.MailCfg = "Notification rules", settings, mail
	pd.NotifFields = notify.NotifyFieldNames()
	a.renderStatus(w, r, status, "admin_notifications", pd)
}

// adminNotificationsSMTP saves the mail settings (sender, management
// recipients, base URL for links). The SMTP host must be one the server
// operator allowed in FERVID_SMTP_ALLOWED_HOSTS.
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
	if in.SMTPHost != "" && !notify.AllowedSMTPHost(in.SMTPHost, a.cfg.SMTPAllowedHosts) {
		a.respondError(w, r, http.StatusBadRequest, "SMTP host must be authorized in FERVID_SMTP_ALLOWED_HOSTS by the server operator.", nil)
		return
	}
	if err := a.st.SetMailSettings(r.Context(), auth.CurrentUser(r), in); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/notifications", http.StatusSeeOther)
}

// adminNotificationEventSave saves the email rule for one event. The subject
// and body templates are checked first, so a typo is reported here instead of
// producing a broken email later. A rejected rule reopens its edit sheet with
// the error (400).
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
			// Reopen the edit sheet with what the admin typed and the reason
			// beside it, rather than the stored rule.
			a.renderAdminNotificationsPage(w, r, http.StatusBadRequest, PageData{NotifDraft: &in, NotifDraftError: err.Error()})
			return
		}
	}
	if err := a.st.SetNotificationSetting(r.Context(), auth.CurrentUser(r), in); err != nil {
		if storeErrorStatus(err) == http.StatusBadRequest {
			a.renderAdminNotificationsPage(w, r, http.StatusBadRequest, PageData{NotifDraft: &in, NotifDraftError: friendly(err)})
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/notifications", http.StatusSeeOther)
}

// adminNotificationsTest sends a test email (to test_to, or the admin's own
// address) so the SMTP settings can be checked without waiting for a real
// event. A failed send answers 502 with a message; the details go to the
// server log only.
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
	// "Name <address>" when a sender name is set, as workflow emails use.
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
		a.renderAdminNotificationsPage(w, r, http.StatusBadGateway, PageData{Error: "The test email could not be sent. Check the operator SMTP configuration and server logs."})
		return
	}
	a.renderAdminNotificationsPage(w, r, http.StatusOK, PageData{Notice: "Test email sent to " + to + "."})
}
