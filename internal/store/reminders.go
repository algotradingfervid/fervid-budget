package store

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

// collectRequests drains a requestSelect result set. The scheduler's two
// queries are the only list reads outside requests.go, so the loop lives here
// rather than being duplicated in each.
func collectRequests(rows *sql.Rows) ([]Request, error) {
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Reminder scheduling.
//
// Two things are deliberately parameters rather than constants:
//
//   - now, so a test can pin "three days later" without sleeping, and so the
//     whole scheduler is deterministic.
//   - the thresholds, which are admin-set data (Configuration → Reminders and
//     ageing). The old hardcoded 3 and 1 are gone.

// ReminderThresholds is how long to wait before the first reminder, how often
// to repeat it, and how long a reservation may sit before it is stale.
type ReminderThresholds struct {
	PendingAfterDays int
	RepeatEveryDays  int
	StaleAfterDays   int
}

const (
	defaultPendingAfterDays = 3
	defaultRepeatEveryDays  = 1
	defaultStaleAfterDays   = 1
)

// calendarDaysBetween counts date boundaries crossed, not elapsed hours. A
// request submitted at 23:00 on the 20th has "waited a day" at 01:00 on the
// 21st, which is what a person means and what the screens say.
func calendarDaysBetween(a, b time.Time) int {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	aDay := time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC)
	bDay := time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC)
	return int(bDay.Sub(aDay).Hours() / 24)
}

// ReminderThresholds reads the three admin-set keys, falling back to the
// defaults when a key is unset, blank or not a positive integer. Garbage must
// never silently disable reminders — a zero wait would remind on every tick and
// a negative one would never remind at all.
func (s *Store) ReminderThresholds(ctx context.Context) (ReminderThresholds, error) {
	all, err := s.AppSettings(ctx)
	if err != nil {
		return ReminderThresholds{}, err
	}
	read := func(key string, fallback int) int {
		n, err := strconv.Atoi(all[key])
		if err != nil || n <= 0 {
			return fallback
		}
		return n
	}
	return ReminderThresholds{
		PendingAfterDays: read("reminder_pending_days", defaultPendingAfterDays),
		RepeatEveryDays:  read("reminder_repeat_days", defaultRepeatEveryDays),
		StaleAfterDays:   read("reminder_stale_days", defaultStaleAfterDays),
	}, nil
}

// RequestsPendingReminder returns the pending requests that have waited longer
// than the configured threshold and have not been reminded within the repeat
// cadence. A request on hold is excluded: it is waiting on the requester by
// design, and nagging the approver about it is noise.
func (s *Store) RequestsPendingReminder(ctx context.Context, now time.Time, th ReminderThresholds) ([]Request, error) {
	if th.PendingAfterDays <= 0 {
		th.PendingAfterDays = defaultPendingAfterDays
	}
	if th.RepeatEveryDays <= 0 {
		th.RepeatEveryDays = defaultRepeatEveryDays
	}
	firstDue := now.UTC().AddDate(0, 0, -th.PendingAfterDays)
	repeatDue := now.UTC().AddDate(0, 0, -th.RepeatEveryDays)
	rows, err := s.db.QueryContext(ctx, requestSelect+`
		WHERE r.status='pending' AND COALESCE(r.on_hold,0)=0
		  AND r.submitted_at IS NOT NULL AND r.submitted_at <= ?
		  AND (r.reminder_last_sent IS NULL OR r.reminder_last_sent <= ?)
		ORDER BY r.submitted_at, r.id`, firstDue, repeatDue)
	if err != nil {
		return nil, err
	}
	return collectRequests(rows)
}

// RequestsStaleProcessing returns requests reserved by an accountant that have
// sat in processing past the configured threshold with no settlement recorded.
func (s *Store) RequestsStaleProcessing(ctx context.Context, now time.Time, th ReminderThresholds) ([]Request, error) {
	if th.StaleAfterDays <= 0 {
		th.StaleAfterDays = defaultStaleAfterDays
	}
	if th.RepeatEveryDays <= 0 {
		th.RepeatEveryDays = defaultRepeatEveryDays
	}
	staleBefore := now.UTC().AddDate(0, 0, -th.StaleAfterDays)
	repeatDue := now.UTC().AddDate(0, 0, -th.RepeatEveryDays)
	rows, err := s.db.QueryContext(ctx, requestSelect+`
		WHERE r.status='processing' AND r.processing_at IS NOT NULL AND r.processing_at <= ?
		  AND (r.reminder_last_sent IS NULL OR r.reminder_last_sent <= ?)
		  AND NOT EXISTS(SELECT 1 FROM payments py WHERE py.request_id=r.id AND py.voided_at IS NULL)
		ORDER BY r.processing_at, r.id`, staleBefore, repeatDue)
	if err != nil {
		return nil, err
	}
	return collectRequests(rows)
}

// MarkReminderSent records that a reminder went out, which is what stops the
// next tick sending the same one again inside the repeat window.
func (s *Store) MarkReminderSent(ctx context.Context, requestID int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET reminder_last_sent=? WHERE id=?`, now.UTC(), requestID)
	return classify(err)
}

// ResetReminder clears the timer so the wait starts again — used when a request
// moves to a new person, since the new owner has not been waited on yet.
func (s *Store) ResetReminder(ctx context.Context, requestID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET reminder_last_sent=NULL WHERE id=?`, requestID)
	return classify(err)
}
