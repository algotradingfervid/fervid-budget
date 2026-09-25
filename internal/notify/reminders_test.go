package notify

import (
	"context"
	"strings"
	"testing"
	"time"

	"fervidbudget/internal/store"
)

func TestRunRemindersFiresOnceThenRespectsTheCadence(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	requester := mustUser(t, st, "rr-req@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "rr-mgr@test", "Manav", "admin")
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	reqID := insertRequest(t, st, "PR-2026-000501", "pending", requester, manager, now.AddDate(0, 0, -4), nil)

	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventReminderPending, EmailEnabled: true, IncludeManager: true,
		SubjectTemplate: "Reminder: {{number}}", BodyTemplate: "pending since {{submitted_on}}",
	})

	mailer := &fakeMailer{}
	svc := NewService(st, mailer)
	must(t, svc.RunReminders(ctx, now))

	msgs := mailer.messages()
	if len(msgs) != 1 {
		t.Fatalf("reminder emails = %d, want 1", len(msgs))
	}
	contains(t, msgs[0].Subject, "PR-2026-000501")

	rows, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: manager, Scope: "reminders"})
	must(t, err)
	if len(rows) != 1 {
		t.Fatalf("in-app reminder rows = %d, want 1", len(rows))
	}

	// Running again the same day must not repeat it.
	must(t, svc.RunReminders(ctx, now))
	if n := len(mailer.messages()); n != 1 {
		t.Fatalf("reminder emails after a second run = %d, want 1 (cadence not respected)", n)
	}

	// A day later the repeat window has passed.
	must(t, svc.RunReminders(ctx, now.AddDate(0, 0, 1)))
	if n := len(mailer.messages()); n != 2 {
		t.Fatalf("reminder emails the next day = %d, want 2", n)
	}
	_ = reqID
}

// The stale-reservation reminder goes to whoever holds the reservation.
func TestRunRemindersNotifiesTheAssignedAccountant(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	requester := mustUser(t, st, "sr-req@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "sr-mgr@test", "Manav", "admin")
	acct := mustUser(t, st, "sr-acct@test", "Anil", "data_entry")
	other := mustUser(t, st, "sr-other@test", "Other", "data_entry")
	grantAccounts(t, st, acct, other)
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	processing := now.AddDate(0, 0, -3)
	reqID := insertRequest(t, st, "PR-2026-000601", "processing", requester, manager, now.AddDate(0, 0, -6), &processing)
	if _, err := st.DB().ExecContext(ctx, `UPDATE payment_requests SET processing_by=? WHERE id=?`, acct, reqID); err != nil {
		t.Fatal(err)
	}

	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventReminderStaleReservation, EmailEnabled: true, IncludeAccounts: true,
		SubjectTemplate: "Still reserved: {{number}}", BodyTemplate: "since {{processing_on}}",
	})

	mailer := &fakeMailer{}
	svc := NewService(st, mailer)
	must(t, svc.RunReminders(ctx, now))

	rows, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: acct, Scope: "reminders"})
	must(t, err)
	if len(rows) != 1 {
		t.Fatalf("assigned accountant in-app rows = %d, want 1", len(rows))
	}
	// The other accountant is not chased about someone else's reservation.
	rows, err = st.ListNotifications(ctx, store.NotificationFilter{UserID: other})
	must(t, err)
	if len(rows) != 0 {
		t.Fatalf("an unrelated accountant got %d rows", len(rows))
	}
	// The email goes to the same one person the in-app row does. It used to go
	// to everyone holding payment:process, so the unrelated accountant was
	// emailed about a reservation they had no in-app row for (notify-1).
	msgs := mailer.messages()
	if len(msgs) != 1 {
		t.Fatalf("stale-reservation emails = %d, want 1", len(msgs))
	}
	if got := strings.Join(msgs[0].To, ","); got != "sr-acct@test" || len(msgs[0].Cc) != 0 {
		t.Fatalf("stale-reservation email To = %q Cc = %v, want only the holder sr-acct@test", got, msgs[0].Cc)
	}
}

// A settled reservation is not stale, however long ago it was reserved.
func TestRunRemindersSkipsSettledReservations(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	requester := mustUser(t, st, "ss-req@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "ss-mgr@test", "Manav", "admin")
	acct := mustUser(t, st, "ss-acct@test", "Anil", "data_entry")
	grantAccounts(t, st, acct)
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	processing := now.AddDate(0, 0, -5)
	reqID := insertRequest(t, st, "PR-2026-000701", "processing", requester, manager, now.AddDate(0, 0, -9), &processing)
	if _, err := st.DB().ExecContext(ctx, `UPDATE payment_requests SET processing_by=? WHERE id=?`, acct, reqID); err != nil {
		t.Fatal(err)
	}
	// Give it a head to hang the payment off, then settle it.
	projectID, err := st.UpsertProject(ctx, 0, "Ops", true, 1)
	must(t, err)
	headID, err := st.UpsertHead(ctx, 0, projectID, "Rent", "5", true, 1)
	must(t, err)
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,entered_by,request_id,settlement)
		VALUES(?,?,?,?,?,?, 'settled')`, headID, "2026-07-22", int64(500000), "Acme", acct, reqID); err != nil {
		t.Fatal(err)
	}

	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventReminderStaleReservation, EmailEnabled: true, IncludeAccounts: true,
		SubjectTemplate: "Still reserved: {{number}}", BodyTemplate: "b",
	})
	mailer := &fakeMailer{}
	svc := NewService(st, mailer)
	must(t, svc.RunReminders(ctx, now))
	if n := len(mailer.messages()); n != 0 {
		t.Fatalf("reminders for a settled reservation = %d, want 0", n)
	}
}

func TestSchedulerRunsOnStartAndStopsOnCancel(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	requester := mustUser(t, st, "sc-req@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "sc-mgr@test", "Manav", "admin")
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	insertRequest(t, st, "PR-2026-000801", "pending", requester, manager, now.AddDate(0, 0, -4), nil)
	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventReminderPending, EmailEnabled: true, IncludeManager: true,
		SubjectTemplate: "Reminder: {{number}}", BodyTemplate: "b",
	})
	fm := &fakeMailer{}
	svc := NewService(st, fm)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		svc.Scheduler(runCtx, time.Hour, func() time.Time { return now })
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for len(fm.messages()) == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatal("scheduler did not run on start")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop on cancel")
	}
}

// F-F-02: the stale-reservation screen tells an accountant "a reminder went
// out", and the "Who has been told" thread printed under it is built from the
// audit log. So every reminder the scheduler sends has to leave a row there,
// naming which reminder it was and who it actually reached.
func TestRunRemindersLeavesAnAuditRowNamingWhoWasTold(t *testing.T) { // F-F-02
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	requester := mustUser(t, st, "ra-req@test", "Rhea Requester", "data_entry")
	manager := mustUser(t, st, "ra-mgr@test", "Manav Manager", "admin")
	acct := mustUser(t, st, "ra-acct@test", "Anil Holder", "data_entry")
	grantAccounts(t, st, acct)
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)

	pending := insertRequest(t, st, "PR-2026-000901", "pending", requester, manager, now.AddDate(0, 0, -4), nil)
	processing := now.AddDate(0, 0, -3)
	stale := insertRequest(t, st, "PR-2026-000902", "processing", requester, manager, now.AddDate(0, 0, -6), &processing)
	if _, err := st.DB().ExecContext(ctx, `UPDATE payment_requests SET processing_by=? WHERE id=?`, acct, stale); err != nil {
		t.Fatal(err)
	}
	enableEvent(t, st, actor, store.NotificationSetting{Event: EventReminderPending, IncludeManager: true,
		SubjectTemplate: "Reminder: {{number}}", BodyTemplate: "b {{link}}"})
	enableEvent(t, st, actor, store.NotificationSetting{Event: EventReminderStaleReservation, IncludeAccounts: true,
		SubjectTemplate: "Still reserved: {{number}}", BodyTemplate: "b {{link}}"})

	svc := NewService(st, &fakeMailer{})
	must(t, svc.RunReminders(ctx, now))

	// The pending reminder names the approver it chased.
	summary := remindSummary(t, st, pending)
	contains(t, summary, "Pending-approval reminder")
	contains(t, summary, "Manav Manager")

	// The stale one names the accountant holding the reservation — and says which
	// reminder it was, so a reader on the stale screen is not shown a pending
	// reminder and told it was the one-day nudge.
	summary = remindSummary(t, st, stale)
	contains(t, summary, "Stale-reservation reminder")
	contains(t, summary, "Anil Holder")
	if strings.Contains(summary, "Pending-approval") {
		t.Fatalf("stale reminder row reads %q", summary)
	}

	// The cadence still governs: a second run the same day repeats neither the
	// notification nor the audit row.
	must(t, svc.RunReminders(ctx, now))
	if n := remindRowCount(t, st, stale); n != 1 {
		t.Fatalf("remind audit rows after a second same-day run = %d, want 1", n)
	}
	// A day later there is a second reminder and a second row: the trail is a
	// history, which is what makes "when did we last chase this" answerable.
	must(t, svc.RunReminders(ctx, now.AddDate(0, 0, 1)))
	if n := remindRowCount(t, st, stale); n != 2 {
		t.Fatalf("remind audit rows the next day = %d, want 2", n)
	}
}

func remindEntries(t *testing.T, st *store.Store, requestID int64) []store.AuditEntry {
	t.Helper()
	entries, err := st.Audit(context.Background(), "payment_request", requestID, 100)
	must(t, err)
	var out []store.AuditEntry
	for _, e := range entries {
		if e.Action == "remind" {
			out = append(out, e)
		}
	}
	return out
}

func remindRowCount(t *testing.T, st *store.Store, requestID int64) int {
	t.Helper()
	return len(remindEntries(t, st, requestID))
}

func remindSummary(t *testing.T, st *store.Store, requestID int64) string {
	t.Helper()
	rows := remindEntries(t, st, requestID)
	if len(rows) != 1 {
		t.Fatalf("remind audit rows for request %d = %d, want 1", requestID, len(rows))
	}
	return rows[0].Summary
}
