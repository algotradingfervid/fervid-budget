// This file is the reminder job. Every hour it reminds approvers about
// requests that have waited too long for a decision, and accountants about
// reservations they have held too long without paying. The waits are admin
// settings; each request is stamped when reminded so it is not reminded again
// until the wait has passed once more.

package notify

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"fervidbudget/internal/store"
)

// RunReminders makes one reminder pass: pending-approval reminders first,
// then stale-reservation reminders. It reads the admin-set waits at the start
// of every pass, so a Configuration change takes effect on the next tick
// without a restart. The first error stops the pass.
func (s *Service) RunReminders(ctx context.Context, now time.Time) error {
	now = now.UTC()
	th, err := s.st.ReminderThresholds(ctx)
	if err != nil {
		return err
	}
	if err := s.runReminderBatch(ctx, now, th, EventReminderPending, s.st.RequestsPendingReminder); err != nil {
		return err
	}
	return s.runReminderBatch(ctx, now, th, EventReminderStaleReservation, s.st.RequestsStaleProcessing)
}

// runReminderBatch sends one kind of reminder (event) for every request load
// returns as due, and stamps each request as reminded, recording who was told
// on its history. If the event has no rule row it does nothing. A failed send
// returns before the stamp, so that request is tried again next pass.
func (s *Service) runReminderBatch(ctx context.Context, now time.Time, th store.ReminderThresholds, event string,
	load func(context.Context, time.Time, store.ReminderThresholds) ([]store.Request, error)) error {
	// There is deliberately no EmailEnabled short-circuit: the in-app reminder
	// always fires, and Notify decides on its own whether an email follows.
	if _, err := s.st.NotificationSetting(ctx, event); errors.Is(err, store.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	reqs, err := load(ctx, now, th)
	if err != nil {
		return err
	}
	for _, req := range reqs {
		told, err := s.notify(ctx, event, req)
		if err != nil {
			return err
		}
		// Stamping after the send is what stops the next tick repeating it
		// inside the configured wait. It also puts the reminder on the
		// request's own history, so the stale-reservation screen's "a reminder
		// went out" can be checked against the history shown beneath it.
		if err := s.st.MarkReminderSent(ctx, req.ID, now, store.ReminderAudit{
			Label: reminderLabel(event), Recipients: told,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Scheduler runs reminders once immediately, then on every interval tick, and
// returns when ctx is cancelled. now is injected so the whole loop is
// deterministic under test.
func (s *Service) Scheduler(ctx context.Context, interval time.Duration, now func() time.Time) {
	s.runRemindersLogged(ctx, now())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runRemindersLogged(ctx, now())
		}
	}
}

// runRemindersLogged runs one reminder pass and logs a failure. A failed email
// stops the pass before the request is stamped as reminded, so the same
// reminder is retried on the next tick; the log line is how an operator finds
// out why reminders keep repeating or never arrive.
func (s *Service) runRemindersLogged(ctx context.Context, now time.Time) {
	if err := s.RunReminders(ctx, now); err != nil && ctx.Err() == nil {
		slog.ErrorContext(ctx, "reminder run failed", "error", err)
	}
}
