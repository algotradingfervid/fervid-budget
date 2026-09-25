package store

import (
	"database/sql"
	"fmt"
)

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
		SubjectTemplate: "{{number}} has been paid — {{paid_amount}} to {{payee}}",
		BodyTemplate:    "{{number}} was paid {{paid_amount}} to {{payee}} and Accounts confirmed it as fully settled ({{approved_amount}} was approved). The request is now complete.\n\nOpen it: {{link}}"},
	{Event: "payment_partial_review", Label: "Partial payment sent for review", Audience: "Approver", IncludeManager: true,
		SubjectTemplate: "{{number}} was partly paid — your review is needed",
		BodyTemplate:    "{{number}} was approved for {{approved_amount}} but only {{paid_amount}} was paid to {{payee}}. Accept the difference or raise a concern.\n\nOpen it: {{link}}"},
	{Event: "reminder_pending", Label: "Pending reminder", Audience: "Whoever it is waiting on", IncludeManager: true,
		SubjectTemplate: "Reminder: {{number}} is still waiting on you",
		BodyTemplate:    "{{number}} for {{amount}} has been pending since {{submitted_on}}.\n\nOpen it: {{link}}"},
	{Event: "reminder_stale_reservation", Label: "Stale reservation", Audience: "Assigned accountant", IncludeAccounts: true,
		SubjectTemplate: "Reminder: {{number}} is still reserved and unpaid",
		BodyTemplate:    "{{number}} has been in processing since {{processing_on}} with no settlement recorded.\n\nOpen it: {{link}}"},
}

// auditNotificationSettings are the nine events the 2026-07-27 audit found
// missing (F-F-06): six workflow actions notified nobody at all, and three more
// sat in the same gap. They are seeded by migration v9 rather than being
// appended to defaultNotificationSettings, because v7 has already run on every
// installed database and a seed only ever runs once — see PROGRESS.md, "Seeding
// only happens in v1".
//
// Two conventions from the original twelve are followed deliberately rather
// than reinvented:
//
//   - email_enabled is left at its 0 default, so a fresh install delivers in-app
//     rows only and email stays opt-in per event.
//   - the management recipient list is not touched. It is Cc-on-email-only for
//     request_approved and the post-approval urgent case, and it never receives
//     an in-app row because there is no user id to address one to. An
//     organisation that wants management copied on any of these sets the event's
//     own cc_recipients, which is the supported per-event control.
var auditNotificationSettings = []NotificationSetting{
	{Event: "request_withdrawn", Label: "Withdrawn by the requester", Audience: "Approver", IncludeManager: true,
		SubjectTemplate: "{{requester}} withdrew {{number}} — no approval needed",
		BodyTemplate:    "{{requester}} withdrew {{number}} ({{amount}} to {{payee}}) before it was decided. It has left your queue and needs nothing further from you.\n\nOpen it: {{link}}"},
	{Event: "request_reraised", Label: "Raised again after a rejection", Audience: "Approver", IncludeManager: true,
		SubjectTemplate: "{{number}} needs your approval — raised again after a rejection",
		BodyTemplate:    "{{requester}} raised {{number}} again after an earlier rejection. It is {{amount}} to {{payee}}.\n\nProject: {{project}} / {{head}}\nNeeded by: {{needed_by}}\n\nOpen it: {{link}}"},
	{Event: "request_unheld", Label: "Hold lifted", Audience: "Requester", IncludeRequester: true,
		SubjectTemplate: "{{number}} is off hold and back in the payment queue",
		BodyTemplate:    "Accounts lifted the hold on {{number}}. It is queued to be paid again and nothing more is needed from you.\n\nOpen it: {{link}}"},
	{Event: "reservation_released", Label: "Reservation released", Audience: "Requester + approver",
		IncludeRequester: true, IncludeManager: true,
		SubjectTemplate: "{{number}} is unclaimed again — the reservation was released",
		BodyTemplate:    "The accountant who had taken {{number}} ({{amount}} to {{payee}}) for processing released it. It is approved and back in the open queue for someone to pick up.\n\nOpen it: {{link}}"},
	{Event: "reservation_reassigned", Label: "Reservation handed to someone else", Audience: "Requester + approver + the new assignee",
		IncludeRequester: true, IncludeManager: true, IncludeAccounts: true,
		SubjectTemplate: "{{number}} is now being processed by someone else",
		BodyTemplate:    "{{number}} ({{amount}} to {{payee}}) stayed reserved and changed hands, so it never went back to the open queue.\n\nOpen it: {{link}}"},
	{Event: "payment_partial_accepted", Label: "Partial payment accepted", Audience: "Requester + assigned accountant",
		IncludeRequester: true, IncludeAccounts: true,
		SubjectTemplate: "{{number}} is closed — the shortfall was accepted",
		BodyTemplate:    "{{approver}} accepted that {{number}} was paid {{paid_amount}} against {{approved_amount}} approved. The balance will not be paid and the request is closed.\n\nOpen it: {{link}}"},
	{Event: "payment_partial_concern", Label: "Concern raised about a partial payment", Audience: "Assigned accountant", IncludeAccounts: true,
		SubjectTemplate: "{{approver}} raised a concern about the partial payment on {{number}}",
		BodyTemplate:    "{{approver}} is not satisfied with the shortfall on {{number}}: {{paid_amount}} paid against {{approved_amount}} approved. The request stays in review until the concern is answered.\n\nOpen it: {{link}}"},
	{Event: "request_cancellation_accepted", Label: "Cancellation accepted", Audience: "Requester + assigned accountant",
		IncludeRequester: true, IncludeAccounts: true,
		SubjectTemplate: "{{number}} was cancelled — the payment will not be made",
		BodyTemplate:    "{{approver}} accepted the ask to cancel {{number}} ({{approved_amount}} to {{payee}}). Nothing further will be paid against it.\n\nOpen it: {{link}}"},
	{Event: "request_cancellation_declined", Label: "Cancellation declined", Audience: "Requester + assigned accountant",
		IncludeRequester: true, IncludeAccounts: true,
		SubjectTemplate: "{{number}} stands — the cancellation was declined",
		BodyTemplate:    "{{approver}} declined the ask to cancel {{number}}. It is approved again for {{approved_amount}} to {{payee}} and the payment is no longer frozen.\n\nOpen it: {{link}}"},
}

// oldSettlementTemplates are the four settlement rules exactly as v7 and v9
// seeded them, before settlement-1 (2026-09-25) found that every one of them
// filled the amount *paid* from {{amount}} — the amount *requested*. "Approved
// for ₹12,000 but only ₹12,345.67 was paid" was the sentence an approver read
// about an ₹8,000 payment. The map is what lets UpPaidAmountTemplates tell a
// row still on the seeded default from one an administrator rewrote.
var oldSettlementTemplates = map[string]struct{ Subject, Body string }{
	"payment_settled": {
		Subject: "{{number}} has been paid — {{amount}} to {{payee}}",
		Body:    "{{number}} was paid in full: {{amount}} to {{payee}}. The request is now complete.\n\nOpen it: {{link}}"},
	"payment_partial_review": {
		Subject: "{{number}} was partly paid — your review is needed",
		Body:    "{{number}} was approved for {{approved_amount}} but only {{amount}} was paid to {{payee}}. Accept the difference or raise a concern.\n\nOpen it: {{link}}"},
	"payment_partial_accepted": {
		Subject: "{{number}} is closed — the shortfall was accepted",
		Body:    "{{approver}} accepted that {{number}} was paid {{amount}} against {{approved_amount}} approved. The balance will not be paid and the request is closed.\n\nOpen it: {{link}}"},
	"payment_partial_concern": {
		Subject: "{{approver}} raised a concern about the partial payment on {{number}}",
		Body:    "{{approver}} is not satisfied with the shortfall on {{number}}: {{amount}} paid against {{approved_amount}} approved. The request stays in review until the concern is answered.\n\nOpen it: {{link}}"},
}

// UpPaidAmountTemplates is migration v14 (v13 on the settlement branch; renumbered
// at integration behind UpReassignedNotificationWording).
//
// It rewrites a settlement rule's subject and body only when both still read
// exactly as they were seeded, so an administrator's own wording — even wording
// that happens to use {{amount}} on purpose — is never touched. The replacement
// text is the current seeded default, so a fresh database and a migrated one
// end up identical. Re-runnable: a row already rewritten no longer matches.
func UpPaidAmountTemplates(tx *sql.Tx) error {
	current := map[string]NotificationSetting{}
	for _, n := range defaultNotificationSettings {
		current[n.Event] = n
	}
	for _, n := range auditNotificationSettings {
		current[n.Event] = n
	}
	for event, old := range oldSettlementTemplates {
		want, ok := current[event]
		if !ok {
			return fmt.Errorf("migration v14: %s has no seeded default", event)
		}
		if _, err := tx.Exec(`UPDATE notification_settings SET subject_template=?, body_template=?
			WHERE event=? AND subject_template=? AND body_template=?`,
			want.SubjectTemplate, want.BodyTemplate, event, old.Subject, old.Body); err != nil {
			return err
		}
	}
	return nil
}

// reassignmentNotificationSettings are the two events migration v11 adds.
//
// They are the audit's repair auditing itself: both were found while checking
// the QA documents against the code four waves had just written, and both are
// the fault F-F-06 was about — a transition that changes what a specific other
// person must do next, firing nothing.
//
// The first is the sharper of the two, because the product was not merely
// silent, it was wrong: the reassignment sheet says "The new approver is told",
// and the route Wave 3 built to make reassignment reachable at all fired
// nothing. A screen that states a notification happens is a promise, and this
// is what keeps it.
//
// Same conventions as v9's nine: email off by default so delivery stays opt-in
// per event, and the management recipient list untouched.
var reassignmentNotificationSettings = []NotificationSetting{
	// Not "needs your approval": a request can be handed on while it is
	// returned, awaiting a cancellation decision or in partial review as well as
	// while pending, and what the new approver is being asked for differs in
	// each. The wording names what is true in all four — they are its approver
	// now — and the body says what that can mean (deactivation-2 review). v13
	// carries this onto databases v11 already seeded.
	{Event: "approval_reassigned", Label: "Sent to a different approver", Audience: "The new approver", IncludeManager: true,
		SubjectTemplate: "{{number}} was reassigned to you as its approver",
		BodyTemplate:    "{{number}} ({{amount}} to {{payee}}) was reassigned to you. You are its approver now: whatever it is waiting on from its approver — the approval itself, a cancellation request or a partial-settlement review — is yours to decide, and if it was returned for correction it comes to you once it is resubmitted.\n\nRequested by: {{requester}}\nProject: {{project}} / {{head}}\nNeeded by: {{needed_by}}\n\nOpen it: {{link}}"},
	{Event: "request_cancelled", Label: "Cancelled by the approver", Audience: "Requester", IncludeRequester: true,
		SubjectTemplate: "{{number}} was cancelled by {{approver}}",
		BodyTemplate:    "{{approver}} cancelled {{number}} ({{amount}} to {{payee}}). Nothing will be paid against it, and it needs nothing further from you.\n\nOpen it: {{link}}"},
}

// The approval_reassigned wording v11 seeded, kept so v13 can recognise a row
// nobody has edited.
const (
	v11ReassignedSubject = "{{number}} needs your approval — it was reassigned to you"
	v11ReassignedBody    = "{{number}} ({{amount}} to {{payee}}) was moved to you for approval.\n\nRequested by: {{requester}}\nProject: {{project}} / {{head}}\nNeeded by: {{needed_by}}\n\nOpen it: {{link}}"
)

// UpReassignedNotificationWording is migration v13: the approval_reassigned
// notice stops saying "needs your approval".
//
// Fix wave B made a request reassignable while returned, awaiting a
// cancellation decision or in partial review — every status the deactivation
// warning promises can be handed on — and the notice v11 seeded was written
// for the pending case alone: it told the new approver a returned request
// needed their approval when it was waiting on the requester, and a frozen one
// when it was waiting on a cancellation decision. The seed now carries wording
// that is true in all four states; this migration brings an installed database
// to the same text.
//
// Only where the row still reads exactly as v11 wrote it. The template columns
// are an administrator's to edit, and seedNotificationSettings' whole contract
// is that a migration never resets what they typed — so a row whose subject or
// body differs from the v11 default is theirs and is left alone. Re-runnable
// by construction: once rewritten, the WHERE matches nothing.
func UpReassignedNotificationWording(tx *sql.Tx) error {
	_, err := tx.Exec(`UPDATE notification_settings SET subject_template=?, body_template=?
		WHERE event=? AND subject_template=? AND body_template=?`,
		reassignmentNotificationSettings[0].SubjectTemplate, reassignmentNotificationSettings[0].BodyTemplate,
		"approval_reassigned", v11ReassignedSubject, v11ReassignedBody)
	return err
}

// UpReassignmentNotificationEvents is migration v11.
//
// Rows only, ON CONFLICT DO NOTHING, so it is idempotent by construction and an
// administrator's edits to the twenty-one events already seeded are untouched.
// The sort order continues where v9 stopped, which is why the offset is the two
// earlier sets added together rather than a literal.
func UpReassignmentNotificationEvents(tx *sql.Tx) error {
	return seedNotificationSettings(tx, reassignmentNotificationSettings,
		len(defaultNotificationSettings)+len(auditNotificationSettings))
}

// seedNotificationSettings inserts event rules without ever overwriting one.
//
// ON CONFLICT DO NOTHING, never DO UPDATE: an installed database has an
// administrator's edits in these columns — their own recipients, their own
// wording, email switched on — and a re-seed would silently reset every one of
// them. That makes the whole function safe to run twice, which is what lets a
// migration be re-applied after a user_version reset.
//
// startOrder is the sort_order already in use, so a later migration's events
// land after the earlier ones on the admin screen instead of interleaving.
func seedNotificationSettings(tx *sql.Tx, rows []NotificationSetting, startOrder int) error {
	for i, n := range rows {
		if _, err := tx.Exec(`INSERT INTO notification_settings
			(event,label,audience,email_enabled,to_recipients,cc_recipients,include_requester,include_manager,include_accounts,subject_template,body_template,sort_order)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(event) DO NOTHING`,
			n.Event, n.Label, n.Audience, boolInt(n.EmailEnabled), n.ToRecipients, n.CcRecipients,
			boolInt(n.IncludeRequester), boolInt(n.IncludeManager), boolInt(n.IncludeAccounts),
			n.SubjectTemplate, n.BodyTemplate, startOrder+i+1); err != nil {
			return err
		}
	}
	return nil
}

// UpAuditNotificationEvents is migration v9: the nine event rules of F-F-06.
//
// It adds rows and nothing else — no schema change, no re-seed of the twelve
// v7 already wrote — so it is idempotent by construction and an administrator's
// existing settings are untouched whether it runs once or ten times.
//
// Exported because the migrations slice lives in migrations.go and this file
// owns the notification vocabulary; register it there as
// {Version: 9, Name: "notification_events_audit", Up: UpAuditNotificationEvents}.
func UpAuditNotificationEvents(tx *sql.Tx) error {
	return seedNotificationSettings(tx, auditNotificationSettings, len(defaultNotificationSettings))
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
	return seedNotificationSettings(tx, defaultNotificationSettings, 0)
}
