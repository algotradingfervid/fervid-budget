package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
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
//
// A "day" here is an elapsed 24-hour period, not a calendar-date boundary
// (F-F-08). The audit found a `calendarDaysBetween` helper documented as the
// semantics, unit-tested, and called from nowhere in production, while both
// queries below did elapsed-time arithmetic. Elapsed days won, and the helper
// is gone, for three reasons:
//
//  1. A calendar-day boundary is only meaningful in a timezone, and this app has
//     no timezone setting to name one. The DB stores UTC, so a UTC boundary
//     would roll over at 05:30 for the finance team this product is built for —
//     a reminder "after 3 calendar days" firing at 5:30 in the morning is not
//     what anybody meant.
//  2. RepeatEveryDays is unusable under calendar semantics: a reminder sent at
//     23:55 would be "a day old" five minutes later and re-sent at midnight,
//     every night. All three thresholds are one admin control group, so they
//     have to read the same way.
//  3. The queries take an injected now and compare stored timestamps, which is
//     what makes the scheduler deterministic under test. Date-boundary
//     arithmetic in SQL means date(…, 'localtime') and the server's zone, which
//     is neither injectable nor the organisation's.
//
// So the product copy should say "3 days", not "3 calendar days".

// ReminderThresholds is how long to wait before the first reminder, how often to
// repeat it, and how long a reservation may sit before it is stale. Every value
// is a count of elapsed 24-hour days — see the note above.
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

// ReminderAudit is the witness half of MarkReminderSent (F-F-02).
//
// The stale-reservation screen tells an accountant "a reminder went out", and
// the "Who has been told" thread printed directly underneath it is built from
// the audit log — so a reminder that wrote no audit row could never be
// corroborated by the one list on the page that exists to corroborate it.
//
// Label names which reminder went out, because both reminders land on the same
// request's trail and two rows reading "Reminder sent" tell a reader nothing.
// Recipients is who was told, which is the literal question that section asks;
// the caller passes the people it actually delivered to rather than resolving
// them again, so the row cannot claim somebody the delivery missed.
type ReminderAudit struct {
	Label      string
	Recipients []string
}

// reminderActor is the actor_name on a reminder's audit row. actor_id stays NULL
// because no person did this — the scheduler did — and the trail renders the
// name in front of the verb, so it may not be blank.
const reminderActor = "Fervid Budget"

// MarkReminderSent records that a reminder went out. The timestamp is what stops
// the next tick sending the same one again inside the repeat window; the audit
// row is what lets anyone check that it happened at all (F-F-02).
//
// Both writes are one transaction, and a request that is not there is refused
// rather than silently audited: an audit row asserting a reminder for a row that
// does not exist is worse than no row.
func (s *Store) MarkReminderSent(ctx context.Context, requestID int64, now time.Time, rec ReminderAudit) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET reminder_last_sent=? WHERE id=?`, now.UTC(), requestID)
	if err != nil {
		return classify(err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	label := strings.TrimSpace(rec.Label)
	if label == "" {
		label = "Reminder"
	}
	summary := label + " sent"
	if len(rec.Recipients) > 0 {
		summary += " to " + strings.Join(rec.Recipients, ", ")
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorName: reminderActor,
		Action: "remind", EntityType: "payment_request", EntityID: &requestID,
		Summary: summary,
		After:   map[string]any{"reminder": label, "recipients": rec.Recipients, "sent_at": now.UTC()}}); err != nil {
		return err
	}
	return tx.Commit()
}

// ResetReminder clears the timer so the wait starts again — used when a request
// moves to a new person, since the new owner has not been waited on yet.
func (s *Store) ResetReminder(ctx context.Context, requestID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET reminder_last_sent=NULL WHERE id=?`, requestID)
	return classify(err)
}
