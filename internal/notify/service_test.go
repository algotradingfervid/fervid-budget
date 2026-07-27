package notify

import (
	"context"
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
