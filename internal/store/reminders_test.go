package store

import (
	"context"
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

func TestCalendarDaysBetweenCountsDateBoundaries(t *testing.T) {
	a := time.Date(2026, 7, 20, 23, 0, 0, 0, time.UTC)
	b := time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC)
	if got := calendarDaysBetween(a, b); got != 3 {
		t.Fatalf("calendarDaysBetween = %d, want 3", got)
	}
	// Two hours apart across midnight is one calendar day, not zero.
	if got := calendarDaysBetween(a, time.Date(2026, 7, 21, 1, 0, 0, 0, time.UTC)); got != 1 {
		t.Fatalf("across midnight = %d, want 1", got)
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
	if err := s.MarkReminderSent(ctx, id, now); err != nil {
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
