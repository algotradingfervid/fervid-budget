package notify

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"fervidbudget/internal/store"
)

func TestRenderTemplateSubstitutesTheTokenVocabulary(t *testing.T) { // G20
	v := RequestView{
		Number: "PR-1", Amount: 123456, ApprovedAmount: 100000, Payee: "Acme",
		ManagerName: "Kavita Rao", Project: "Ops", Head: "Rent", NeededBy: "2026-08-01",
		Link: "https://budget.test/requests/1",
	}
	out, err := renderTemplate("{{number}}: {{amount}} to {{payee}}", v)
	if err != nil {
		t.Fatal(err)
	}
	if out != "PR-1: ₹1,234.56 to Acme" {
		t.Fatalf("render = %q, want PR-1: ₹1,234.56 to Acme", out)
	}
	full, err := renderTemplate("{{approver}} {{project}} {{head}} {{needed_by}} {{link}} {{approved_amount}}", v)
	if err != nil {
		t.Fatal(err)
	}
	if full != "Kavita Rao Ops Rent 2026-08-01 https://budget.test/requests/1 ₹1,000.00" {
		t.Fatalf("render = %q", full)
	}
}

// A typo must fail loudly at save time, not emit an empty string into an email
// nobody can debug afterwards.
func TestRenderTemplateRejectsUnknownToken(t *testing.T) { // G20
	if _, err := renderTemplate("Hello {{recipient}}", RequestView{}); err == nil {
		t.Fatal("unknown token accepted")
	}
	if err := ValidateTemplate("{{number}} is fine"); err != nil {
		t.Fatalf("valid template rejected: %v", err)
	}
	if err := ValidateTemplate("{{nope}}"); err == nil {
		t.Fatal("ValidateTemplate accepted an unknown token")
	}
}

// Admin-editable text must never reach the Go template engine.
func TestRenderTemplateDoesNotExecuteGoTemplates(t *testing.T) { // G20
	// A Go action would be executed by text/template; here it is just an
	// unknown token, which is refused.
	if _, err := renderTemplate(`{{.Number}}`, RequestView{Number: "PR-1"}); err == nil {
		t.Fatal("Go template syntax was accepted; admin text can reach the engine")
	}
}

func TestResolveRecipientsForUrgentDependsOnStatus(t *testing.T) {
	app := store.MailSettings{ManagementRecipients: "boss@test"}
	cfg := store.NotificationSetting{Event: EventRequestUrgent, IncludeManager: true, IncludeAccounts: true}
	accounts := []string{"acct@test"}

	pending := RequestView{Status: "pending", ManagerEmail: "mgr@test"}
	to, cc := resolveRecipients(EventRequestUrgent, cfg, app, pending, accounts)
	if len(to) != 1 || to[0] != "mgr@test" {
		t.Fatalf("pre-approval urgent To = %v, want the approver only", to)
	}
	if len(cc) != 0 {
		t.Fatalf("pre-approval urgent Cc = %v, want management not copied yet", cc)
	}

	approved := RequestView{Status: "approved", ManagerEmail: "mgr@test"}
	to, cc = resolveRecipients(EventRequestUrgent, cfg, app, approved, accounts)
	if len(to) != 1 || to[0] != "acct@test" {
		t.Fatalf("post-approval urgent To = %v, want Accounts", to)
	}
	if len(cc) != 1 || cc[0] != "boss@test" {
		t.Fatalf("post-approval urgent Cc = %v, want the management list", cc)
	}
}

// An address in both To and Cc would deliver twice and read as a mistake.
func TestResolveRecipientsDedupesAcrossToAndCc(t *testing.T) {
	app := store.MailSettings{ManagementRecipients: "Boss@Test"}
	cfg := store.NotificationSetting{Event: EventRequestApproved, IncludeRequester: true,
		ToRecipients: "boss@test", CcRecipients: "boss@test, ops@test"}
	to, cc := resolveRecipients(EventRequestApproved, cfg, app, RequestView{RequesterEmail: "req@test"}, nil)
	if len(to) != 2 {
		t.Fatalf("To = %v, want the fixed address plus the requester", to)
	}
	for _, e := range cc {
		if e == "boss@test" {
			t.Fatalf("boss@test appears in both To and Cc: %v / %v", to, cc)
		}
	}
	if len(cc) != 1 || cc[0] != "ops@test" {
		t.Fatalf("Cc = %v, want only ops@test", cc)
	}
}

func TestNotifyWritesInAppRowsAndSendsEmailWhenEnabled(t *testing.T) { // G19
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	must(t, st.SetMailSettings(ctx, actor, store.MailSettings{
		SMTPHost: "smtp.test", SMTPFromName: "Fervid", SMTPFromAddr: "no@reply.test",
		BaseURL: "https://budget.test",
	}))
	requester := mustUser(t, st, "req@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "mgr@test", "Manav", "admin")
	reqID := insertRequest(t, st, "PR-2026-000001", "pending", requester, manager, time.Now().UTC(), nil)

	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventRequestSubmitted, EmailEnabled: true, IncludeManager: true,
		SubjectTemplate: "{{number}} needs approval", BodyTemplate: "{{amount}} to {{payee}} — {{link}}",
	})

	mailer := &fakeMailer{}
	svc := NewService(st, mailer)
	req, err := st.Request(ctx, reqID)
	must(t, err)
	must(t, svc.Notify(ctx, EventRequestSubmitted, req))

	msgs := mailer.messages()
	if len(msgs) != 1 {
		t.Fatalf("emails sent = %d, want 1", len(msgs))
	}
	contains(t, msgs[0].Subject, "PR-2026-000001 needs approval")
	contains(t, msgs[0].Body, "₹5,000.00")
	contains(t, msgs[0].Body, "https://budget.test/requests/")
	contains(t, msgs[0].From, "Fervid <no@reply.test>")
	if len(msgs[0].To) != 1 || msgs[0].To[0] != "mgr@test" {
		t.Fatalf("To = %v, want the approver", msgs[0].To)
	}

	rows, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: manager})
	must(t, err)
	if len(rows) != 1 {
		t.Fatalf("in-app rows for the approver = %d, want 1", len(rows))
	}
	if rows[0].Href != "/requests/"+itoa(reqID) {
		t.Fatalf("in-app href = %q", rows[0].Href)
	}
	// The requester was not addressed, so has nothing.
	rows, err = st.ListNotifications(ctx, store.NotificationFilter{UserID: requester})
	must(t, err)
	if len(rows) != 0 {
		t.Fatalf("requester got %d rows for an approver-only event", len(rows))
	}
}

// N1/G19: disabling an event turns off email, never the in-app record.
func TestNotifyDisabledEventStillWritesInAppButSendsNoEmail(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	requester := mustUser(t, st, "req2@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "mgr2@test", "Manav", "admin")
	reqID := insertRequest(t, st, "PR-2026-000002", "pending", requester, manager, time.Now().UTC(), nil)

	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventRequestSubmitted, EmailEnabled: false, IncludeManager: true,
		SubjectTemplate: "{{number}} needs approval", BodyTemplate: "body",
	})

	mailer := &fakeMailer{}
	svc := NewService(st, mailer)
	req, err := st.Request(ctx, reqID)
	must(t, err)
	must(t, svc.Notify(ctx, EventRequestSubmitted, req))

	if n := len(mailer.messages()); n != 0 {
		t.Fatalf("emails sent = %d, want 0 for a disabled event", n)
	}
	rows, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: manager})
	must(t, err)
	if len(rows) != 1 {
		t.Fatalf("in-app rows = %d — in-app must fire even when email is off", len(rows))
	}
}

// A broken template must not cost the recipient their in-app record.
func TestNotifyKeepsInAppRowWhenTemplateIsBroken(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	requester := mustUser(t, st, "req3@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "mgr3@test", "Manav", "admin")
	reqID := insertRequest(t, st, "PR-2026-000003", "pending", requester, manager, time.Now().UTC(), nil)

	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventRequestSubmitted, EmailEnabled: true, IncludeManager: true,
		SubjectTemplate: "{{typo_here}}", BodyTemplate: "body",
	})
	mailer := &fakeMailer{}
	svc := NewService(st, mailer)
	req, err := st.Request(ctx, reqID)
	must(t, err)
	err = svc.Notify(ctx, EventRequestSubmitted, req)
	if err == nil {
		t.Fatal("a broken subject template should surface as an error")
	}
	rows, lerr := st.ListNotifications(ctx, store.NotificationFilter{UserID: manager})
	must(t, lerr)
	if len(rows) != 1 {
		t.Fatalf("in-app rows = %d, want 1 — the row is written before email is attempted", len(rows))
	}
	if !strings.Contains(rows[0].Title, "PR-2026-000003") {
		t.Fatalf("fallback title = %q, want it to name the request", rows[0].Title)
	}
}

// The Accounts group resolves to whoever holds payment:process.
func TestNotifyResolvesAccountsGroup(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	requester := mustUser(t, st, "req4@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "mgr4@test", "Manav", "admin")
	acct := mustUser(t, st, "acct4@test", "Anil", "data_entry")
	grantAccounts(t, st, acct)
	reqID := insertRequest(t, st, "PR-2026-000004", "approved", requester, manager, time.Now().UTC(), nil)

	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventRequestApproved, EmailEnabled: true, IncludeRequester: true, IncludeAccounts: true,
		SubjectTemplate: "{{number}} approved", BodyTemplate: "b",
	})
	mailer := &fakeMailer{}
	svc := NewService(st, mailer)
	req, err := st.Request(ctx, reqID)
	must(t, err)
	must(t, svc.Notify(ctx, EventRequestApproved, req))

	msgs := mailer.messages()
	if len(msgs) != 1 {
		t.Fatalf("emails = %d", len(msgs))
	}
	joined := strings.Join(msgs[0].To, ",")
	contains(t, joined, "req4@test")
	contains(t, joined, "acct4@test")
	for _, uid := range []int64{requester, acct} {
		rows, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: uid})
		must(t, err)
		if len(rows) != 1 {
			t.Fatalf("user %d in-app rows = %d, want 1", uid, len(rows))
		}
	}
}

// N3: the approval email goes To the requester and Accounts and is copied (Cc)
// to the management list. The list is set only in MailSettings — not in the
// event's own To or Cc — so the Cc below can only have come from the N3 rule
// (test-1: no earlier test pinned it).
func TestNotifyCopiesManagementOnTheApprovalEmail(t *testing.T) { // N3
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	must(t, st.SetMailSettings(ctx, actor, store.MailSettings{
		SMTPHost: "smtp.test", SMTPFromAddr: "no@reply.test",
		ManagementRecipients: "Boss@T.test; cfo@t.test",
	}))
	requester := mustUser(t, st, "mc-req@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "mc-mgr@test", "Manav", "admin")
	reqID := insertRequest(t, st, "PR-2026-000970", "approved", requester, manager, time.Now().UTC(), nil)
	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventRequestApproved, EmailEnabled: true, IncludeRequester: true, IncludeAccounts: true,
		SubjectTemplate: "{{number}} approved", BodyTemplate: "b",
	})
	mailer := &fakeMailer{}
	svc := NewService(st, mailer)
	req, err := st.Request(ctx, reqID)
	must(t, err)
	must(t, svc.Notify(ctx, EventRequestApproved, req))

	msgs := mailer.messages()
	if len(msgs) != 1 {
		t.Fatalf("emails = %d, want 1", len(msgs))
	}
	if got := strings.Join(msgs[0].Cc, ","); got != "boss@t.test,cfo@t.test" {
		t.Fatalf("approval Cc = %q, want the management list boss@t.test,cfo@t.test", got)
	}
	contains(t, strings.Join(msgs[0].To, ","), "mc-req@test")
	for _, e := range msgs[0].To {
		if e == "boss@t.test" || e == "cfo@t.test" {
			t.Fatalf("management is in To %v, want it only in Cc", msgs[0].To)
		}
	}
}

// notify-2: the in-app body is the email body minus what only makes sense on
// paper. Its line breaks are kept (the centre renders them), and a line whose
// template field came out empty is dropped rather than shown as a bare label —
// a request raised with no Needed-by date used to read "Needed by:" with
// nothing after it.
func TestNotifyInAppBodyDropsEmptyFieldLinesAndKeepsLineBreaks(t *testing.T) { // N1
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	requester := mustUser(t, st, "ib-req@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "ib-mgr@test", "Manav", "admin")
	reqID := insertRequest(t, st, "PR-2026-000980", "pending", requester, manager, time.Now().UTC(), nil)
	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventRequestSubmitted, IncludeManager: true,
		SubjectTemplate: "{{number}} needs your approval",
		// The seeded default body (migrations_notifications.go).
		BodyTemplate: "{{requester}} raised {{number}} for {{amount}} to {{payee}}.\n\nProject: {{project}} / {{head}}\nNeeded by: {{needed_by}}\n\nOpen it: {{link}}",
	})
	mailer := &fakeMailer{}
	svc := NewService(st, mailer)
	req, err := st.Request(ctx, reqID)
	must(t, err)
	req.Project, req.Head = "Operations", "Office Rent"
	must(t, svc.Notify(ctx, EventRequestSubmitted, req))

	rows, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: manager})
	must(t, err)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	want := "Rhea raised PR-2026-000980 for ₹5,000.00 to Acme.\n\nProject: Operations / Office Rent\n\nOpen it: /requests/" + itoa(reqID)
	if rows[0].Body != want {
		t.Fatalf("in-app body =\n%q\nwant\n%q", rows[0].Body, want)
	}
}

func TestRenderInAppBody(t *testing.T) {
	v := RequestView{Number: "PR-1", Payee: "Acme", Link: "/requests/1"}
	cases := []struct{ tmpl, want string }{
		// A field on its own line that is empty takes the line with it, and
		// the blank lines around it collapse to one paragraph break.
		{"{{number}} to {{payee}}.\n\n{{purpose}}\nNeeded by: {{needed_by}}\n\nOpen it: {{link}}", "PR-1 to Acme.\n\nOpen it: /requests/1"},
		// Admin text with no tokens is kept as written.
		{"Hello\nthere", "Hello\nthere"},
		// Every line empty leaves no body at all, not whitespace.
		{"Needed by: {{needed_by}}", ""},
	}
	for _, tc := range cases {
		got, err := renderInAppBody(tc.tmpl, v)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("renderInAppBody(%q) = %q, want %q", tc.tmpl, got, tc.want)
		}
	}
	if _, err := renderInAppBody("{{nope}}", v); err == nil {
		t.Fatal("unknown token accepted")
	}
}

// An unknown event is a no-op, not an error: nothing is configured for it.
func TestNotifyUnknownEventIsANoOp(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	mailer := &fakeMailer{}
	svc := NewService(st, mailer)
	if err := svc.Notify(ctx, "not_an_event", store.Request{}); err != nil {
		t.Fatalf("unknown event = %v, want nil", err)
	}
	if n := len(mailer.messages()); n != 0 {
		t.Fatalf("emails = %d, want 0", n)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// F-F-06: the nine events the audit found missing must reach the person who
// needs to know, and — for the events about one specific reservation — must not
// chase the whole Accounts group about work that belongs to one accountant.
//
// The audience flags below are the seeded ones; internal/store's
// TestMigrationV9SeedsTheNineMissingEvents pins that the migration really seeds
// these, and this test pins what they resolve to.
func TestNotifyResolvesRecipientsForTheAuditEvents(t *testing.T) { // F-F-06
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	requester := mustUser(t, st, "ae-req@test", "Rhea Requester", "data_entry")
	manager := mustUser(t, st, "ae-mgr@test", "Manav Manager", "admin")
	holder := mustUser(t, st, "ae-holder@test", "Anil Holder", "data_entry")
	otherAcct := mustUser(t, st, "ae-other@test", "Other Accountant", "data_entry")
	grantAccounts(t, st, holder, otherAcct)
	emailOf := map[int64]string{requester: "ae-req@test", manager: "ae-mgr@test",
		holder: "ae-holder@test", otherAcct: "ae-other@test"}

	cases := []struct {
		event    string
		status   string
		holding  bool // processing_by is set to holder
		cfg      store.NotificationSetting
		want     []int64
		notWant  []int64
		wantKind string
	}{
		{event: EventRequestWithdrawn, status: "withdrawn",
			cfg:      store.NotificationSetting{IncludeManager: true},
			want:     []int64{manager},
			notWant:  []int64{requester, holder, otherAcct},
			wantKind: "activity"},
		{event: EventRequestReraised, status: "pending",
			cfg:      store.NotificationSetting{IncludeManager: true},
			want:     []int64{manager},
			notWant:  []int64{requester, holder, otherAcct},
			wantKind: "activity"},
		{event: EventRequestUnheld, status: "approved",
			cfg:      store.NotificationSetting{IncludeRequester: true},
			want:     []int64{requester},
			notWant:  []int64{manager, holder, otherAcct},
			wantKind: "activity"},
		// Release nulls processing_by, so there is no assignee left to address —
		// and the release screen states that the requester and the approver are
		// both notified, so both are.
		{event: EventReservationReleased, status: "approved",
			cfg:      store.NotificationSetting{IncludeRequester: true, IncludeManager: true},
			want:     []int64{requester, manager},
			notWant:  []int64{holder, otherAcct},
			wantKind: "activity"},
		// Reassignment moves processing_by, so "the assigned accountant" is
		// already the new holder by the time this fires — and the accountant who
		// is not involved hears nothing.
		{event: EventReservationReassigned, status: "processing", holding: true,
			cfg:      store.NotificationSetting{IncludeRequester: true, IncludeManager: true, IncludeAccounts: true},
			want:     []int64{requester, manager, holder},
			notWant:  []int64{otherAcct},
			wantKind: "activity"},
		{event: EventPaymentPartialAccepted, status: "completed_partial", holding: true,
			cfg:      store.NotificationSetting{IncludeRequester: true, IncludeAccounts: true},
			want:     []int64{requester, holder},
			notWant:  []int64{manager, otherAcct},
			wantKind: "activity"},
		// A concern is written words addressed to the accountant, waiting on
		// their answer — the same shape as request_returned, so it files under
		// "mention" and shows up on that tab.
		{event: EventPaymentPartialConcern, status: "partial_review", holding: true,
			cfg:      store.NotificationSetting{IncludeAccounts: true},
			want:     []int64{holder},
			notWant:  []int64{requester, manager, otherAcct},
			wantKind: "mention"},
		{event: EventCancellationAccepted, status: "cancelled", holding: true,
			cfg:      store.NotificationSetting{IncludeRequester: true, IncludeAccounts: true},
			want:     []int64{requester, holder},
			notWant:  []int64{manager, otherAcct},
			wantKind: "activity"},
		{event: EventCancellationDeclined, status: "approved", holding: true,
			cfg:      store.NotificationSetting{IncludeRequester: true, IncludeAccounts: true},
			want:     []int64{requester, holder},
			notWant:  []int64{manager, otherAcct},
			wantKind: "activity"},
	}

	for i, tc := range cases {
		t.Run(tc.event, func(t *testing.T) {
			number := "PR-2026-0009" + strconv.Itoa(10+i)
			processing := time.Now().UTC().Add(-48 * time.Hour)
			var processingAt *time.Time
			if tc.holding {
				processingAt = &processing
			}
			reqID := insertRequest(t, st, number, tc.status, requester, manager, time.Now().UTC().Add(-72*time.Hour), processingAt)
			if tc.holding {
				if _, err := st.DB().ExecContext(ctx, `UPDATE payment_requests SET processing_by=? WHERE id=?`, holder, reqID); err != nil {
					t.Fatal(err)
				}
			}
			cfg := tc.cfg
			cfg.Event = tc.event
			cfg.SubjectTemplate = "{{number}} — " + tc.event
			cfg.BodyTemplate = "{{amount}} to {{payee}}. Open it: {{link}}"
			// Email on, so the email audience is checked against the in-app one:
			// the two must reach the same people (notify-1).
			cfg.EmailEnabled = true
			enableEvent(t, st, actor, cfg)

			mailer := &fakeMailer{}
			svc := NewService(st, mailer)
			req, err := st.Request(ctx, reqID)
			must(t, err)
			must(t, svc.Notify(ctx, tc.event, req))

			msgs := mailer.messages()
			if len(msgs) != 1 {
				t.Fatalf("%s: emails = %d, want 1", tc.event, len(msgs))
			}
			to := map[string]bool{}
			for _, e := range append(append([]string{}, msgs[0].To...), msgs[0].Cc...) {
				to[e] = true
			}
			for _, uid := range tc.want {
				if !to[emailOf[uid]] {
					t.Fatalf("%s: email To %v is missing %s", tc.event, msgs[0].To, emailOf[uid])
				}
			}
			for _, uid := range tc.notWant {
				if to[emailOf[uid]] {
					t.Fatalf("%s: email To %v includes %s, who has no in-app row for it", tc.event, msgs[0].To, emailOf[uid])
				}
			}

			for _, uid := range tc.want {
				rows, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: uid, Limit: 200})
				must(t, err)
				var got *store.Notification
				for j := range rows {
					if rows[j].Event == tc.event && rows[j].RequestID != nil && *rows[j].RequestID == reqID {
						got = &rows[j]
					}
				}
				if got == nil {
					t.Fatalf("%s: user %d got no row", tc.event, uid)
				}
				if got.Kind != tc.wantKind {
					t.Fatalf("%s: kind = %q, want %q — a wrong kind hides the row under every .segmented tab but All",
						tc.event, got.Kind, tc.wantKind)
				}
				contains(t, got.Title, number)
				if got.Href != "/requests/"+itoa(reqID) {
					t.Fatalf("%s: href = %q", tc.event, got.Href)
				}
			}
			for _, uid := range tc.notWant {
				rows, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: uid, Limit: 200})
				must(t, err)
				for _, row := range rows {
					if row.Event == tc.event && row.RequestID != nil && *row.RequestID == reqID {
						t.Fatalf("%s: user %d was notified and should not have been", tc.event, uid)
					}
				}
			}
		})
	}
}

// The .segmented filter on /notifications queries `kind`, which
// store.AddNotification derives from the event at write time — there is no
// second copy in this package to drift from it. So a new event's rows really are
// reachable under the tab they belong to, not only under "All".
func TestNewEventsAreReachableUnderTheirFilterTab(t *testing.T) { // F-F-06
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	requester := mustUser(t, st, "fb-req@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "fb-mgr@test", "Manav", "admin")
	holder := mustUser(t, st, "fb-holder@test", "Anil", "data_entry")
	grantAccounts(t, st, holder)
	processing := time.Now().UTC().Add(-48 * time.Hour)
	reqID := insertRequest(t, st, "PR-2026-000950", "partial_review", requester, manager, processing, &processing)
	if _, err := st.DB().ExecContext(ctx, `UPDATE payment_requests SET processing_by=? WHERE id=?`, holder, reqID); err != nil {
		t.Fatal(err)
	}
	enableEvent(t, st, actor, store.NotificationSetting{Event: EventPaymentPartialConcern, IncludeAccounts: true,
		SubjectTemplate: "concern on {{number}}", BodyTemplate: "b {{link}}"})
	enableEvent(t, st, actor, store.NotificationSetting{Event: EventReservationReleased, IncludeRequester: true,
		SubjectTemplate: "released {{number}}", BodyTemplate: "b {{link}}"})

	svc := NewService(st, &fakeMailer{})
	req, err := st.Request(ctx, reqID)
	must(t, err)
	must(t, svc.Notify(ctx, EventPaymentPartialConcern, req))
	must(t, svc.Notify(ctx, EventReservationReleased, req))

	mentions, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: holder, Scope: "mentions"})
	must(t, err)
	if len(mentions) != 1 || mentions[0].Event != EventPaymentPartialConcern {
		t.Fatalf("Mentions tab for the accountant = %+v, want the partial concern", mentions)
	}
	// The requester's release row is activity: it reports a settled fact and asks
	// nothing, so it must not be filed as a mention or a reminder.
	for _, scope := range []string{"mentions", "reminders"} {
		rows, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: requester, Scope: scope})
		must(t, err)
		if len(rows) != 0 {
			t.Fatalf("release row appears under %q = %+v, want it under activity only", scope, rows)
		}
	}
	counts, err := st.NotificationCounts(ctx, requester)
	must(t, err)
	if counts.All != 1 || counts.Unread != 1 {
		t.Fatalf("requester counts = %+v, want one unread row", counts)
	}
}

// The management recipient list is Cc-on-email-only and never receives an in-app
// row, because there is no user id to address one to. The nine new events follow
// that convention rather than inventing another: none of them copies management
// on its own, and an organisation that wants them to sets the event's own
// cc_recipients.
func TestAuditEventsDoNotCopyManagementByThemselves(t *testing.T) { // F-F-06
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	must(t, st.SetMailSettings(ctx, actor, store.MailSettings{
		SMTPHost: "smtp.test", SMTPFromAddr: "no@reply.test",
		ManagementRecipients: "boss@fervid.test", BaseURL: "https://budget.test",
	}))
	requester := mustUser(t, st, "mg-req@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "mg-mgr@test", "Manav", "admin")
	reqID := insertRequest(t, st, "PR-2026-000960", "approved", requester, manager, time.Now().UTC(), nil)

	mailer := &fakeMailer{}
	svc := NewService(st, mailer)
	for _, event := range []string{EventRequestWithdrawn, EventRequestReraised, EventRequestUnheld,
		EventReservationReleased, EventReservationReassigned, EventPaymentPartialAccepted,
		EventPaymentPartialConcern, EventCancellationAccepted, EventCancellationDeclined} {
		enableEvent(t, st, actor, store.NotificationSetting{Event: event, EmailEnabled: true,
			IncludeRequester: true, IncludeManager: true,
			SubjectTemplate: "{{number}}", BodyTemplate: "b {{link}}"})
		req, err := st.Request(ctx, reqID)
		must(t, err)
		must(t, svc.Notify(ctx, event, req))
	}
	for _, m := range mailer.messages() {
		for _, cc := range m.Cc {
			if cc == "boss@fervid.test" {
				t.Fatalf("event copied management on its own: %q Cc %v", m.Subject, m.Cc)
			}
		}
	}
	// And management never gets an in-app row, since it is an address, not a user.
	var rows int
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications n
		JOIN users u ON u.id=n.user_id WHERE u.email='boss@fervid.test'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("in-app rows for the management list = %d, want 0", rows)
	}
}

// settlement-1: every settlement notification states the amount actually
// paid, and the approved figure where it differs. The four seeded templates
// used {{amount}} — the requested figure — so an approver was told "only
// ₹12,345.67 was paid" about an ₹8,000 payment, and "paid in full: ₹50,000"
// about ₹45,000.
func TestSettlementNotificationsNameTheAmountActuallyPaid(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	requester := mustUser(t, st, "req@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "mgr@test", "Manav", "admin")
	accountant := mustUser(t, st, "acc@test", "Asha", "admin")
	acc, err := st.UserByID(ctx, accountant)
	must(t, err)
	// Requested 12,345.67, approved 12,000, paid 8,000 — three different
	// figures, so the wrong one cannot pass by coincidence. A recoverable
	// request, because it is the one shape that needs no budget head.
	reqID := insertRequest(t, st, "PR-2026-000002", "approved", requester, manager, time.Now().UTC(), nil)
	_, err = st.DB().ExecContext(ctx, `UPDATE payment_requests SET amount=1234567, approved_amount=1200000, approved_by=?, approved_at=CURRENT_TIMESTAMP,
		treatment='recoverable', type='employee_advance', recoverable_category='other' WHERE id=?`, manager, reqID)
	must(t, err)
	must(t, st.ReserveRequest(ctx, acc, reqID))
	_, err = st.RecordPaymentForRequest(ctx, acc, reqID, store.PaymentInput{PaidOn: "2026-07-20", Amount: 800000, VendorPayee: "Acme", PaymentMode: "bank_transfer"}, "partial", "Retention held back", nil)
	must(t, err)

	svc := NewService(st, &fakeMailer{})
	req, err := st.Request(ctx, reqID)
	must(t, err)
	for _, event := range []string{EventPaymentPartialReview, EventPaymentPartialConcern, EventPaymentPartialAccepted, EventPaymentSettled} {
		must(t, svc.Notify(ctx, event, req))
	}
	for _, uid := range []int64{manager, requester, accountant} {
		rows, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: uid})
		must(t, err)
		if len(rows) == 0 {
			t.Fatalf("user %d received nothing", uid)
		}
		for _, row := range rows {
			if strings.Contains(row.Body, "12,345.67") || strings.Contains(row.Title, "12,345.67") {
				t.Fatalf("%s tells user %d the requested amount was paid: %q / %q", row.Event, uid, row.Title, row.Body)
			}
			if !strings.Contains(row.Body, "₹8,000.00") {
				t.Fatalf("%s never names the ₹8,000.00 that was paid: %q", row.Event, row.Body)
			}
			if !strings.Contains(row.Body, "₹12,000.00") {
				t.Fatalf("%s never names the ₹12,000.00 approved beside it: %q", row.Event, row.Body)
			}
		}
	}
	// The vocabulary itself: a template may ask for the paid figure by name,
	// and it is listed for the administrator editing the rules.
	out, err := renderTemplate("{{paid_amount}} on {{paid_on}}", RequestView{PaidAmount: 800000, PaidOn: "2026-07-20"})
	must(t, err)
	if out != "₹8,000.00 on 2026-07-20" {
		t.Fatalf("render = %q", out)
	}
	if !slices.Contains(NotifyFieldNames(), "paid_amount") {
		t.Fatalf("the admin sheet's hint does not list paid_amount: %v", NotifyFieldNames())
	}
}
