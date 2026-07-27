package notify

import (
	"context"
	"errors"
	"time"

	"fervidbudget/internal/store"
)

// RunReminders reads the admin-set thresholds once per run, so a Configuration
// change takes effect on the next tick without a restart.
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
		if err := s.Notify(ctx, event, req); err != nil {
			return err
		}
		// Stamping after the send is what stops the next tick repeating it
		// inside the configured cadence.
		if err := s.st.MarkReminderSent(ctx, req.ID, now); err != nil {
			return err
		}
	}
	return nil
}

// Scheduler runs reminders once immediately, then on every interval tick, and
// returns when ctx is cancelled. now is injected so the whole loop is
// deterministic under test.
func (s *Service) Scheduler(ctx context.Context, interval time.Duration, now func() time.Time) {
	_ = s.RunReminders(ctx, now())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.RunReminders(ctx, now())
		}
	}
}
