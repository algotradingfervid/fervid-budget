package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func seedReminderRequest(t *testing.T, s *Store, ctx context.Context, number, status string) int64 {
	t.Helper()
	reqUser, err := s.CreateUser(ctx, number+"-req@test", "Req", "h", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := s.CreateUser(ctx, number+"-mgr@test", "Mgr", "h", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO payment_requests(number,status,treatment,type,amount,purpose,requester_id,manager_id,submitted_at)
		VALUES(?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		number, status, "budget", "vendor_invoice", int64(500000), "Rent", reqUser, mgr)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// F-F-08: the thresholds are elapsed 24-hour days, not calendar-date
// boundaries. TestCalendarDaysBetweenCountsDateBoundaries used to live here and
// exercised a helper nothing in production ever called; the helper is gone and
// this test replaces it by pinning the semantics that actually run, so the two
// can no longer disagree.
//
// A request submitted at 23:50 has not "waited a day" ten minutes later at
// 00:00, and — the decisive case — a reminder sent at 23:55 is not repeatable
// five minutes later at midnight.
func TestReminderThresholdsCountElapsedDaysNotCalendarBoundaries(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id := seedReminderRequest(t, s, ctx, "PR-2026-000020", "pending")
	th := ReminderThresholds{PendingAfterDays: 1, RepeatEveryDays: 1, StaleAfterDays: 1}

	submitted := time.Date(2026, 7, 20, 23, 50, 0, 0, time.UTC)
	if _, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET submitted_at=? WHERE id=?`, submitted, id); err != nil {
		t.Fatal(err)
	}
	// Ten minutes later, one date boundary has been crossed. Not a day.
	justPastMidnight := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	if due, err := s.RequestsPendingReminder(ctx, justPastMidnight, th); err != nil {
		t.Fatal(err)
	} else if len(due) != 0 {
		t.Fatalf("due ten minutes after submission = %d, want 0 — a date boundary is not an elapsed day", len(due))
	}
	// A full 24 hours later it is due.
	if due, err := s.RequestsPendingReminder(ctx, submitted.Add(24*time.Hour), th); err != nil {
		t.Fatal(err)
	} else if len(due) != 1 {
		t.Fatalf("due after 24 elapsed hours = %d, want 1", len(due))
	}

	// The repeat cadence reads the same way: a reminder sent at 23:55 is not
	// repeatable at midnight, which is the case that makes calendar-day
	// semantics unusable for this threshold.
	sent := time.Date(2026, 7, 24, 23, 55, 0, 0, time.UTC)
	if err := s.MarkReminderSent(ctx, id, sent, ReminderAudit{Label: "Pending-approval reminder"}); err != nil {
		t.Fatal(err)
	}
	if due, _ := s.RequestsPendingReminder(ctx, time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC), th); len(due) != 0 {
		t.Fatalf("repeatable five minutes later = %d, want 0", len(due))
	}
	if due, _ := s.RequestsPendingReminder(ctx, sent.Add(24*time.Hour), th); len(due) != 1 {
		t.Fatalf("repeatable after 24 elapsed hours = %d, want 1", len(due))
	}
}

// F-F-02: "a reminder went out" needs a witness. The stale-reservation screen's
// "Who has been told" thread is built from the audit log, so the reminder has to
// leave a row there — naming which reminder and who was told, since both
// reminders land on the same request's trail.
func TestMarkReminderSentWritesAnAuditRow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id := seedReminderRequest(t, s, ctx, "PR-2026-000030", "processing")
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)

	if err := s.MarkReminderSent(ctx, id, now, ReminderAudit{
		Label: "Stale-reservation reminder", Recipients: []string{"Anil Kumar"},
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Audit(ctx, "payment_request", id, 50)
	if err != nil {
		t.Fatal(err)
	}
	var found []AuditEntry
	for _, e := range entries {
		if e.Action == "remind" {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("remind audit rows = %d, want 1", len(found))
	}
	got := found[0]
	if got.ActorID != nil {
		t.Fatalf("actor_id = %v, want NULL — no person sent this, the scheduler did", *got.ActorID)
	}
	if got.ActorName == "" {
		t.Fatal("actor_name is blank; the trail renders it in front of the verb")
	}
	if !strings.Contains(got.Summary, "Stale-reservation reminder") {
		t.Fatalf("summary = %q, want it to name which reminder went out", got.Summary)
	}
	if !strings.Contains(got.Summary, "Anil Kumar") {
		t.Fatalf("summary = %q, want it to name who was told", got.Summary)
	}

	// A second reminder is a second row: the trail is a history, not a flag.
	if err := s.MarkReminderSent(ctx, id, now.AddDate(0, 0, 1), ReminderAudit{Label: "Stale-reservation reminder"}); err != nil {
		t.Fatal(err)
	}
	entries, err = s.Audit(ctx, "payment_request", id, 50)
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, e := range entries {
		if e.Action == "remind" {
			rows++
		}
	}
	if rows != 2 {
		t.Fatalf("remind audit rows after a second reminder = %d, want 2", rows)
	}

	// A reminder for a request that is not there writes nothing at all.
	if err := s.MarkReminderSent(ctx, 987654, now, ReminderAudit{Label: "Pending-approval reminder"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("MarkReminderSent on a missing request = %v, want ErrNotFound", err)
	}
	var stray int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE action='remind' AND entity_id=987654`).Scan(&stray); err != nil {
		t.Fatal(err)
	}
	if stray != 0 {
		t.Fatalf("audit rows for a missing request = %d, want 0", stray)
	}
}

func TestReminderThresholdsDefaultsAndOverrides(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)
	th, err := s.ReminderThresholds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if th.PendingAfterDays != 3 || th.RepeatEveryDays != 1 || th.StaleAfterDays != 1 {
		t.Fatalf("defaults = %+v, want 3/1/1", th)
	}
	for k, v := range map[string]string{"reminder_pending_days": "5", "reminder_repeat_days": "2", "reminder_stale_days": "4"} {
		if err := s.SetAppSetting(ctx, actor, k, v); err != nil {
			t.Fatal(err)
		}
	}
	th, err = s.ReminderThresholds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if th.PendingAfterDays != 5 || th.RepeatEveryDays != 2 || th.StaleAfterDays != 4 {
		t.Fatalf("configured = %+v, want 5/2/4", th)
	}
	// Garbage never disables reminders; it falls back to the default.
	for _, bad := range []string{"not a number", "", "0", "-3"} {
		if err := s.SetAppSetting(ctx, actor, "reminder_pending_days", bad); err != nil {
			t.Fatal(err)
		}
		if th, _ := s.ReminderThresholds(ctx); th.PendingAfterDays != 3 {
			t.Fatalf("value %q gave %d, want the 3-day default", bad, th.PendingAfterDays)
		}
	}
}

func TestRequestsPendingReminderHonoursConfiguredThreshold(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id := seedReminderRequest(t, s, ctx, "PR-2026-000001", "pending")
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	th := ReminderThresholds{PendingAfterDays: 3, RepeatEveryDays: 1, StaleAfterDays: 1}
	if _, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET submitted_at=? WHERE id=?`, now.AddDate(0, 0, -4), id); err != nil {
		t.Fatal(err)
	}
	due, err := s.RequestsPendingReminder(ctx, now, th)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("pending reminders = %d, want 1", len(due))
	}
	// A longer configured wait suppresses the same request.
	if due, _ := s.RequestsPendingReminder(ctx, now, ReminderThresholds{PendingAfterDays: 7, RepeatEveryDays: 1, StaleAfterDays: 1}); len(due) != 0 {
		t.Fatalf("7-day threshold reminders = %d, want 0 (submitted 4 days ago)", len(due))
	}

	// Once reminded, the repeat cadence suppresses it until the window passes.
	if err := s.MarkReminderSent(ctx, id, now, ReminderAudit{Label: "Pending-approval reminder"}); err != nil {
		t.Fatal(err)
	}
	if due, _ := s.RequestsPendingReminder(ctx, now, th); len(due) != 0 {
		t.Fatalf("reminded today still due = %d, want 0", len(due))
	}
	tomorrow := now.AddDate(0, 0, 1)
	if due, _ := s.RequestsPendingReminder(ctx, tomorrow, th); len(due) != 1 {
		t.Fatalf("due again after the repeat window = %d, want 1", len(due))
	}

	// Resetting the timer makes it eligible again immediately.
	if err := s.ResetReminder(ctx, id); err != nil {
		t.Fatal(err)
	}
	if due, _ := s.RequestsPendingReminder(ctx, now, th); len(due) != 1 {
		t.Fatalf("after ResetReminder = %d, want 1", len(due))
	}

	// A request on hold is waiting on the requester by design: nagging the
	// approver about it is noise.
	if _, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET on_hold=1 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if due, _ := s.RequestsPendingReminder(ctx, now, th); len(due) != 0 {
		t.Fatalf("on-hold request reminded = %d, want 0", len(due))
	}
}

func TestRequestsStaleProcessingSkipsSettledAndHonoursThreshold(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	th := ReminderThresholds{PendingAfterDays: 3, RepeatEveryDays: 1, StaleAfterDays: 1}

	stale := seedReminderRequest(t, s, ctx, "PR-2026-000010", "processing")
	if _, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET processing_at=?, processing_by=? WHERE id=?`,
		now.AddDate(0, 0, -3), actor.ID, stale); err != nil {
		t.Fatal(err)
	}
	paid := seedReminderRequest(t, s, ctx, "PR-2026-000011", "processing")
	if _, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET processing_at=?, processing_by=? WHERE id=?`,
		now.AddDate(0, 0, -3), actor.ID, paid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,entered_by,request_id,settlement) VALUES(?,?,?,?,?,?, 'settled')`,
		headID, "2026-07-24", int64(500000), "Someone", actor.ID, paid); err != nil {
		t.Fatal(err)
	}

	rows, err := s.RequestsStaleProcessing(ctx, now, th)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != stale {
		t.Fatalf("stale reservations = %+v, want only the unpaid one", rows)
	}
	// A longer configured wait suppresses it.
	if rows, _ := s.RequestsStaleProcessing(ctx, now, ReminderThresholds{StaleAfterDays: 10, RepeatEveryDays: 1}); len(rows) != 0 {
		t.Fatalf("10-day threshold = %d rows, want 0", len(rows))
	}
}
