// reminders.go finds requests that need a reminder, records that one was sent,
// and reads the reminder thresholds. The scheduler that sends them lives in
// internal/notify.

package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
)

// collectRequests drains a requestSelect result set and closes it.
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

// Reminder thresholds are admin-set (Configuration, Reminders and ageing) and
// the current time is passed in, so the scheduler is deterministic under test.
//
// A "day" is an elapsed 24-hour period, not a calendar day:
//   - the app has no timezone setting, and a UTC midnight boundary would fall
//     at 05:30 for users in India;
//   - "repeat every 1 day" would otherwise re-send at every midnight, even for
//     a reminder sent at 23:55;
//   - comparing stored timestamps against the passed-in time keeps the queries
//     simple and testable.

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

// withDefaults replaces any value that is not positive with its default.
func (t ReminderThresholds) withDefaults() ReminderThresholds {
	if t.PendingAfterDays <= 0 {
		t.PendingAfterDays = defaultPendingAfterDays
	}
	if t.RepeatEveryDays <= 0 {
		t.RepeatEveryDays = defaultRepeatEveryDays
	}
	if t.StaleAfterDays <= 0 {
		t.StaleAfterDays = defaultStaleAfterDays
	}
	return t
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
	th = th.withDefaults()
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
	th = th.withDefaults()
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

// ReminderAudit describes a sent reminder for the request's audit trail, which
// the request screen shows as "Who has been told". Label names which reminder
// it was; Recipients are the people it was actually delivered to, as reported
// by the sender.
type ReminderAudit struct {
	Label      string
	Recipients []string
}

// reminderActor is the actor_name on a reminder's audit row. actor_id stays NULL
// because no person did this — the scheduler did — and the trail renders the
// name in front of the verb, so it may not be blank.
const reminderActor = "Fervid Budget"

// MarkReminderSent records that a reminder went out. The timestamp stops the
// next tick sending the same one again inside the repeat window; the audit row
// records who was told.
//
// Both writes are one transaction, and a request that is not there is refused
// rather than silently audited: an audit row asserting a reminder for a row that
// does not exist is worse than no row.
func (s *Store) MarkReminderSent(ctx context.Context, requestID int64, now time.Time, rec ReminderAudit) error {
	tx, err := s.beginWriteTx(ctx)
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

// ResetReminder clears the timer so the wait starts again. Only tests call it:
// the request writes that hand a request to a new person clear
// reminder_last_sent themselves, inside their own transaction.
func (s *Store) ResetReminder(ctx context.Context, requestID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET reminder_last_sent=NULL WHERE id=?`, requestID)
	return classify(err)
}
