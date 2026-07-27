package store

import "database/sql"

// NotificationSetting is one admin-configurable event rule.
//
// Label is the human sentence the admin screen shows and Audience is the
// plain-English "Goes to" column. Both are seeded, so the screen never
// hardcodes copy per event and an event added later describes itself.
type NotificationSetting struct {
	Event            string
	Label            string
	Audience         string
	EmailEnabled     bool
	ToRecipients     string
	CcRecipients     string
	IncludeRequester bool
	IncludeManager   bool
	IncludeAccounts  bool
	SubjectTemplate  string
	BodyTemplate     string
}

// defaultNotificationSettings are the twelve events of G20, in the order
// admin-notifications.html lists them. Templates use the {{token}} vocabulary
// substituted by internal/notify — never Go template syntax, because these
// strings are admin-editable and must never reach the Go template engine.
var defaultNotificationSettings = []NotificationSetting{
	{Event: "request_submitted", Label: "Request submitted", Audience: "Approver", IncludeManager: true,
		SubjectTemplate: "{{number}} needs your approval — {{amount}} to {{payee}}",
		BodyTemplate:    "{{requester}} raised {{number}} for {{amount}} to {{payee}}.\n\nProject: {{project}} / {{head}}\nNeeded by: {{needed_by}}\n\nOpen it: {{link}}"},
	{Event: "request_edited", Label: "Request edited before approval", Audience: "Approver", IncludeManager: true,
		SubjectTemplate: "{{number}} was edited and re-sent for approval",
		BodyTemplate:    "{{requester}} edited {{number}} and sent it back for your approval. It is now {{amount}} to {{payee}}.\n\nOpen it: {{link}}"},
	{Event: "request_returned", Label: "Returned for correction", Audience: "Requester", IncludeRequester: true,
		SubjectTemplate: "{{number}} was returned for correction",
		BodyTemplate:    "{{approver}} returned {{number}} for correction.\n\nOpen it: {{link}}"},
	{Event: "request_rejected", Label: "Rejected", Audience: "Requester", IncludeRequester: true,
		SubjectTemplate: "{{number}} was rejected",
		BodyTemplate:    "{{approver}} rejected {{number}} for {{amount}}. This is final.\n\nOpen it: {{link}}"},
	{Event: "request_approved", Label: "Approved", Audience: "Requester + Accounts group", IncludeRequester: true, IncludeAccounts: true,
		SubjectTemplate: "{{number}} approved — {{approved_amount}} to {{payee}}",
		BodyTemplate:    "{{approver}} approved {{number}} for {{approved_amount}} to {{payee}}.\n\nProject: {{project}} / {{head}}\nNeeded by: {{needed_by}}\n\nOpen it: {{link}}"},
	{Event: "request_urgent", Label: "Urgent request raised", Audience: "Approver immediately, Accounts on approval",
		IncludeManager: true, IncludeAccounts: true,
		SubjectTemplate: "URGENT: {{number}} — {{amount}} to {{payee}}",
		BodyTemplate:    "{{number}} is marked urgent.\n\n{{purpose}}\nNeeded by: {{needed_by}}\n\nOpen it: {{link}}"},
	{Event: "request_on_hold", Label: "Put on hold", Audience: "Requester", IncludeRequester: true,
		SubjectTemplate: "{{number}} was put on hold",
		BodyTemplate:    "Accounts put {{number}} on hold and needs more information before paying.\n\nOpen it: {{link}}"},
	{Event: "request_cancellation_requested", Label: "Cancellation requested", Audience: "Approver + assigned accountant",
		IncludeManager: true, IncludeAccounts: true,
		SubjectTemplate: "{{requester}} asked to cancel {{number}}",
		BodyTemplate:    "{{requester}} asked to cancel {{number}} ({{approved_amount}} to {{payee}}). The payment is frozen until you accept or decline.\n\nOpen it: {{link}}"},
	{Event: "payment_settled", Label: "Payment recorded and settled", Audience: "Requester + approver",
		IncludeRequester: true, IncludeManager: true,
		SubjectTemplate: "{{number}} has been paid — {{amount}} to {{payee}}",
		BodyTemplate:    "{{number}} was paid in full: {{amount}} to {{payee}}. The request is now complete.\n\nOpen it: {{link}}"},
	{Event: "payment_partial_review", Label: "Partial payment sent for review", Audience: "Approver", IncludeManager: true,
		SubjectTemplate: "{{number}} was partly paid — your review is needed",
		BodyTemplate:    "{{number}} was approved for {{approved_amount}} but only {{amount}} was paid to {{payee}}. Accept the difference or raise a concern.\n\nOpen it: {{link}}"},
	{Event: "reminder_pending", Label: "Pending reminder", Audience: "Whoever it is waiting on", IncludeManager: true,
		SubjectTemplate: "Reminder: {{number}} is still waiting on you",
		BodyTemplate:    "{{number}} for {{amount}} has been pending since {{submitted_on}}.\n\nOpen it: {{link}}"},
	{Event: "reminder_stale_reservation", Label: "Stale reservation", Audience: "Assigned accountant", IncludeAccounts: true,
		SubjectTemplate: "Reminder: {{number}} is still reserved and unpaid",
		BodyTemplate:    "{{number}} has been in processing since {{processing_on}} with no settlement recorded.\n\nOpen it: {{link}}"},
}

// upNotifications creates notification_settings and the in-app notifications
// table. app_settings (key/value) is created by Phase 2's v3 migration and is
// deliberately NOT re-created here.
func upNotifications(tx *sql.Tx) error {
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS notification_settings (
  event             TEXT PRIMARY KEY,
  label             TEXT NOT NULL DEFAULT '',
  audience          TEXT NOT NULL DEFAULT '',
  email_enabled     INTEGER NOT NULL DEFAULT 0,
  to_recipients     TEXT NOT NULL DEFAULT '',
  cc_recipients     TEXT NOT NULL DEFAULT '',
  include_requester INTEGER NOT NULL DEFAULT 0,
  include_manager   INTEGER NOT NULL DEFAULT 0,
  include_accounts  INTEGER NOT NULL DEFAULT 0,
  subject_template  TEXT NOT NULL DEFAULT '',
  body_template     TEXT NOT NULL DEFAULT '',
  sort_order        INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS notifications (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id),
  event      TEXT NOT NULL,
  kind       TEXT NOT NULL DEFAULT 'activity',
  request_id INTEGER,
  title      TEXT NOT NULL,
  body       TEXT NOT NULL DEFAULT '',
  href       TEXT NOT NULL DEFAULT '',
  read_at    DATETIME,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_notifications_user_unread ON notifications(user_id, read_at, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_request ON notifications(request_id);`); err != nil {
		return err
	}
	for i, n := range defaultNotificationSettings {
		if _, err := tx.Exec(`INSERT INTO notification_settings
			(event,label,audience,email_enabled,to_recipients,cc_recipients,include_requester,include_manager,include_accounts,subject_template,body_template,sort_order)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(event) DO NOTHING`,
			n.Event, n.Label, n.Audience, boolInt(n.EmailEnabled), n.ToRecipients, n.CcRecipients,
			boolInt(n.IncludeRequester), boolInt(n.IncludeManager), boolInt(n.IncludeAccounts),
			n.SubjectTemplate, n.BodyTemplate, i+1); err != nil {
			return err
		}
	}
	return nil
}
