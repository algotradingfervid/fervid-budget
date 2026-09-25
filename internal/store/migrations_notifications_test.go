package store

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// applyAuditNotificationEvents runs migration v9's Up by hand.
//
// It is called rather than relied upon because the migrations slice lives in
// migrations.go, which this file's owner does not edit: the registration is a
// one-line append made separately. Applying the Up directly is exactly what the
// migrator would do, so these tests pin the behaviour either way round.
func applyAuditNotificationEvents(t *testing.T, s *Store) {
	t.Helper()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := UpAuditNotificationEvents(tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("UpAuditNotificationEvents: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// auditEventKeys is the nine events of F-F-06, in the order they are seeded.
// Spelled out as literals rather than read off the slice, because these strings
// are the contract with internal/notify's event constants and with the handlers
// that fire them — a rename must break a test, not silently stop delivering.
var auditEventKeys = []string{
	"request_withdrawn",
	"request_reraised",
	"request_unheld",
	"reservation_released",
	"reservation_reassigned",
	"payment_partial_accepted",
	"payment_partial_concern",
	"request_cancellation_accepted",
	"request_cancellation_declined",
}

// The {{token}} vocabulary a seeded template may use. This is notify's
// NotifyFieldNames(); a store test cannot import notify, because notify imports
// store, so the list is repeated here and a typo in a seeded template fails
// below instead of rendering an error into an email nobody can debug.
var notifyTokenVocabulary = map[string]bool{
	"number": true, "amount": true, "approved_amount": true, "payee": true,
	"requester": true, "approver": true, "project": true, "head": true,
	"purpose": true, "status": true, "needed_by": true, "submitted_on": true,
	"processing_on": true, "link": true, "paid_amount": true, "paid_on": true,
}

var seededTokenPattern = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

func TestMigrationV9SeedsTheNineMissingEvents(t *testing.T) { // F-F-06
	s := newTestStore(t)
	applyAuditNotificationEvents(t, s)

	// The catalogue is counted, not assumed, and the literal is updated only when
	// a migration deliberately adds to it — v11 added the last two, so a change
	// here has to be argued for rather than absorbed.
	want := len(defaultNotificationSettings) + len(auditNotificationSettings) + len(reassignmentNotificationSettings)
	if want != 23 {
		t.Fatalf("event catalogue = %d, want 23 (v7's twelve, v9's nine, v11's two)", want)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM notification_settings`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("seeded events = %d, want %d", count, want)
	}

	// Who each new event is addressed to. This is the half of the F-F-06 contract
	// that lives in data; internal/notify's TestNotifyResolvesRecipientsForThe
	// AuditEvents owns the other half — given these flags, which user ids get a
	// row. Both are literal, so a change to either has to be deliberate.
	wantFlags := map[string][3]bool{ // {requester, manager, accounts}
		"request_withdrawn":             {false, true, false},
		"request_reraised":              {false, true, false},
		"request_unheld":                {true, false, false},
		"reservation_released":          {true, true, false},
		"reservation_reassigned":        {true, true, true},
		"payment_partial_accepted":      {true, false, true},
		"payment_partial_concern":       {false, false, true},
		"request_cancellation_accepted": {true, false, true},
		"request_cancellation_declined": {true, false, true},
	}

	for i, event := range auditEventKeys {
		var label, audience, subject, body string
		var email, req, mgr, acct, order int
		if err := s.DB().QueryRow(`SELECT label,audience,email_enabled,include_requester,include_manager,include_accounts,subject_template,body_template,sort_order
			FROM notification_settings WHERE event=?`, event).
			Scan(&label, &audience, &email, &req, &mgr, &acct, &subject, &body, &order); err != nil {
			t.Fatalf("event %q missing: %v", event, err)
		}
		if got, want := [3]bool{req == 1, mgr == 1, acct == 1}, wantFlags[event]; got != want {
			t.Fatalf("event %q audience flags {requester,manager,accounts} = %v, want %v", event, got, want)
		}
		// Label and Audience are seeded presentation: the admin screen renders
		// them and never hardcodes copy per event, so an event with neither
		// describes itself as nothing.
		if label == "" || audience == "" {
			t.Fatalf("event %q seeded with label=%q audience=%q", event, label, audience)
		}
		if subject == "" || body == "" {
			t.Fatalf("event %q seeded with an empty template", event)
		}
		// email_enabled defaults to 0 for every seeded event: a fresh install
		// delivers in-app rows only and email is opt-in per event.
		if email != 0 {
			t.Fatalf("event %q ships with email on; email is opt-in", event)
		}
		// An event addressed to nobody delivers nothing, which is the F-F-06
		// defect all over again.
		if req+mgr+acct == 0 {
			t.Fatalf("event %q has no audience flag set, so it would notify nobody", event)
		}
		if wantOrder := len(defaultNotificationSettings) + i + 1; order != wantOrder {
			t.Fatalf("event %q sort_order = %d, want %d — v9's events list after v7's", event, order, wantOrder)
		}
		// G20: the admin-facing vocabulary is {{token}}, never Go template syntax.
		if strings.Contains(subject+body, "{{.") {
			t.Fatalf("event %q uses Go template syntax: %q / %q", event, subject, body)
		}
		for _, m := range seededTokenPattern.FindAllStringSubmatch(subject+body, -1) {
			if key := strings.TrimSpace(m[1]); !notifyTokenVocabulary[key] {
				t.Fatalf("event %q uses unknown token %q; renderTemplate would refuse it", event, key)
			}
		}
	}
}

// Every seeded event must land in a filter bucket the .segmented strip on
// /notifications actually queries, or its rows are invisible under every tab but
// "All". kind is derived once at write time from the event, so this is the only
// definition there is.
func TestEverySeededEventLandsInAKnownNotificationBucket(t *testing.T) { // F-F-06
	all := append(append([]NotificationSetting(nil), defaultNotificationSettings...), auditNotificationSettings...)
	for _, n := range all {
		switch notificationKind(n.Event) {
		case "activity", "mention", "reminder":
		default:
			t.Fatalf("event %q maps to kind %q, which no filter queries", n.Event, notificationKind(n.Event))
		}
	}
	// The one new event that is a mention: the approver's written concern is
	// addressed to the accountant and waits on their answer.
	if got := notificationKind("payment_partial_concern"); got != "mention" {
		t.Fatalf("payment_partial_concern kind = %q, want mention", got)
	}
	// The eight that report a settled fact and ask nothing.
	for _, event := range []string{"request_withdrawn", "request_reraised", "request_unheld",
		"reservation_released", "reservation_reassigned", "payment_partial_accepted",
		"request_cancellation_accepted", "request_cancellation_declined"} {
		if got := notificationKind(event); got != "activity" {
			t.Fatalf("%s kind = %q, want activity", event, got)
		}
	}
}

// PROGRESS.md, "Seeding only happens in v1": a migration that re-seeds resets
// every deliberate change an administrator has made. v9 adds rows and never
// touches one, so it is safe to run twice — which is what makes it safe to
// re-apply after a user_version reset.
func TestMigrationV9IsIdempotentAndKeepsAdministratorEdits(t *testing.T) { // F-F-06
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)
	applyAuditNotificationEvents(t, s)

	// An administrator configures two rows: one of v7's twelve and one of v9's
	// nine, so both halves of the catalogue are covered.
	edits := []NotificationSetting{
		{Event: "request_approved", EmailEnabled: true, ToRecipients: "ap@fervid.test",
			CcRecipients: "board@fervid.test", IncludeRequester: true, IncludeManager: true,
			SubjectTemplate: "OUR WORDING {{number}}", BodyTemplate: "our body {{link}}"},
		{Event: "reservation_released", EmailEnabled: true, ToRecipients: "queue@fervid.test",
			IncludeRequester: true, IncludeAccounts: true,
			SubjectTemplate: "{{number}} is free again", BodyTemplate: "ours {{link}}"},
	}
	for _, in := range edits {
		if err := s.SetNotificationSetting(ctx, actor, in); err != nil {
			t.Fatalf("SetNotificationSetting(%s): %v", in.Event, err)
		}
	}

	// Re-apply twice more. A DO UPDATE upsert, or a re-seed, would flatten the
	// edits above back to the shipped defaults.
	applyAuditNotificationEvents(t, s)
	applyAuditNotificationEvents(t, s)

	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM notification_settings`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if want := len(defaultNotificationSettings) + len(auditNotificationSettings) + len(reassignmentNotificationSettings); count != want {
		t.Fatalf("events after three applications = %d, want %d", count, want)
	}
	for _, in := range edits {
		got, err := s.NotificationSetting(ctx, in.Event)
		if err != nil {
			t.Fatal(err)
		}
		if !got.EmailEnabled || got.ToRecipients != in.ToRecipients || got.CcRecipients != in.CcRecipients ||
			got.SubjectTemplate != in.SubjectTemplate || got.BodyTemplate != in.BodyTemplate ||
			got.IncludeRequester != in.IncludeRequester || got.IncludeManager != in.IncludeManager ||
			got.IncludeAccounts != in.IncludeAccounts {
			t.Fatalf("event %q was reset by a re-run: %+v", in.Event, got)
		}
	}
	// Sort order is not disturbed either, so the admin screen does not reorder
	// itself on an upgrade.
	rows, err := s.AllNotificationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range auditEventKeys {
		if got := rows[len(defaultNotificationSettings)+i].Event; got != want {
			t.Fatalf("row %d = %q, want %q", len(defaultNotificationSettings)+i, got, want)
		}
	}
}

// An installed database that has already taken v9 must survive it arriving a
// second time on a partially-seeded table — the shape a half-applied upgrade
// leaves behind.
func TestMigrationV9FillsGapsWithoutDuplicating(t *testing.T) { // F-F-06
	s := newTestStore(t)
	// Simulate a database where only some of the nine landed.
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := seedNotificationSettings(tx, auditNotificationSettings[:3], len(defaultNotificationSettings)); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	applyAuditNotificationEvents(t, s)

	for _, event := range auditEventKeys {
		var n int
		if err := s.DB().QueryRow(`SELECT COUNT(*) FROM notification_settings WHERE event=?`, event).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("event %q rows = %d, want exactly 1", event, n)
		}
	}
}

// v13 rewords the approval_reassigned notice. Since fix wave B a request is
// reassignable while returned, awaiting a cancellation decision or in partial
// review, and "needs your approval" is wrong for all three (deactivation-2
// review). The seed carries the new wording for a fresh database; v13 carries
// it onto an installed one — and only where the row still reads exactly as v11
// wrote it, so an administrator's own wording is never overwritten.
func TestMigrationV13RewordsTheReassignmentNoticeWithoutTouchingAnAdminsEdit(t *testing.T) {
	s := newTestStore(t)
	read := func() (subject, body string) {
		t.Helper()
		if err := s.DB().QueryRow(`SELECT subject_template, body_template FROM notification_settings WHERE event='approval_reassigned'`).Scan(&subject, &body); err != nil {
			t.Fatal(err)
		}
		return subject, body
	}
	want := reassignmentNotificationSettings[0]
	if want.Event != "approval_reassigned" {
		t.Fatalf("reassignmentNotificationSettings[0] is %q", want.Event)
	}
	for _, bad := range []string{"needs your approval", "for approval"} {
		if strings.Contains(want.SubjectTemplate, bad) || strings.Contains(want.BodyTemplate, bad) {
			t.Fatalf("the seeded approval_reassigned notice still says %q, which is wrong for a returned, frozen or partial-review request", bad)
		}
	}
	if subject, body := read(); subject != want.SubjectTemplate || body != want.BodyTemplate {
		t.Fatalf("fresh database: subject=%q body=%q, want the seed", subject, body)
	}

	// An installed database that still carries v11's wording is brought forward.
	if _, err := s.DB().Exec(`UPDATE notification_settings SET subject_template=?, body_template=? WHERE event='approval_reassigned'`,
		v11ReassignedSubject, v11ReassignedBody); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`PRAGMA user_version = 12`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.DB()); err != nil {
		t.Fatalf("v13: %v", err)
	}
	if subject, body := read(); subject != want.SubjectTemplate || body != want.BodyTemplate {
		t.Fatalf("after v13: subject=%q body=%q, want the new wording", subject, body)
	}

	// An administrator's own wording is theirs, whichever half they changed.
	for _, edit := range []struct{ subject, body string }{
		{"Please look at {{number}}", v11ReassignedBody},
		{v11ReassignedSubject, "{{number}} is yours now: {{link}}"},
	} {
		if _, err := s.DB().Exec(`UPDATE notification_settings SET subject_template=?, body_template=? WHERE event='approval_reassigned'`, edit.subject, edit.body); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB().Exec(`PRAGMA user_version = 12`); err != nil {
			t.Fatal(err)
		}
		if err := migrate(s.DB()); err != nil {
			t.Fatalf("re-running v13: %v", err)
		}
		if subject, body := read(); subject != edit.subject || body != edit.body {
			t.Fatalf("v13 overwrote an administrator's edit: subject=%q body=%q", subject, body)
		}
	}
}
