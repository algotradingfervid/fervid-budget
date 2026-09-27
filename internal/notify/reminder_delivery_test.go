package notify

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net/smtp"
	"strings"
	"sync"
	"testing"
	"time"

	"fervidbudget/internal/store"
)

// reminderSMTPFixture sets up one overdue pending request whose reminder
// emails its manager, with SMTP settings pointing at smtp.example.test.
func reminderSMTPFixture(t *testing.T) (*store.Store, int64, time.Time) {
	t.Helper()
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	requester := mustUser(t, st, "rd-req@test", "Rhea", "data_entry")
	manager := mustUser(t, st, "rd-mgr@test", "Manav", "admin")
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	reqID := insertRequest(t, st, "PR-2026-000701", "pending", requester, manager, now.AddDate(0, 0, -4), nil)
	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventReminderPending, EmailEnabled: true, IncludeManager: true,
		SubjectTemplate: "Reminder: {{number}}", BodyTemplate: "pending since {{submitted_on}}",
	})
	must(t, st.SetMailSettings(ctx, actor, store.MailSettings{
		SMTPHost: "smtp.example.test", SMTPPort: 587,
		SMTPFromName: "Fervid", SMTPFromAddr: "noreply@example.test",
	}))
	return st, reqID, now
}

func reminderLastSent(t *testing.T, st *store.Store, reqID int64) sql.NullTime {
	t.Helper()
	var sent sql.NullTime
	must(t, st.DB().QueryRow(`SELECT reminder_last_sent FROM payment_requests WHERE id=?`, reqID).Scan(&sent))
	return sent
}

// A reminder sent through the real SMTP transport, with the configured host on
// the operator allowlist, goes out once and the request is stamped as
// reminded, so the next tick does not send it again.
func TestReminderThroughAllowlistedSMTPSendsOnceAndIsMarkedSent(t *testing.T) {
	ctx := context.Background()
	st, reqID, now := reminderSMTPFixture(t)

	var mu sync.Mutex
	var sent []string
	mailer := NewSMTPMailer(st, "", []string{"smtp.example.test"})
	mailer.sendMail = func(addr string, _ smtp.Auth, _ string, to []string, msg []byte) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, addr+" "+strings.Join(to, ",")+"\n"+string(msg))
		return nil
	}
	svc := NewService(st, mailer)

	must(t, svc.RunReminders(ctx, now))
	must(t, svc.RunReminders(ctx, now.Add(time.Hour)))

	if len(sent) != 1 {
		t.Fatalf("reminder emails sent = %d, want exactly 1", len(sent))
	}
	contains(t, sent[0], "smtp.example.test:587 rd-mgr@test")
	contains(t, sent[0], "PR-2026-000701")
	if got := reminderLastSent(t, st, reqID); !got.Valid {
		t.Fatal("reminder was sent but the request was not marked as reminded")
	}
	if n := remindRowCount(t, st, reqID); n != 1 {
		t.Fatalf("remind audit rows = %d, want 1", n)
	}
}

// When delivery fails (here: the host is not on the allowlist) the request is
// not stamped, and the scheduler logs the failure instead of dropping it.
func TestSchedulerLogsAFailedReminderRun(t *testing.T) {
	st, reqID, now := reminderSMTPFixture(t)
	svc := NewService(st, NewSMTPMailer(st, "", nil))

	var buf bytes.Buffer
	var mu sync.Mutex
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{mu: &mu, w: &buf}, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Scheduler(ctx, time.Hour, func() time.Time { return now })
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		logged := buf.String()
		mu.Unlock()
		if strings.Contains(logged, "reminder run failed") {
			contains(t, logged, "allowlist")
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatalf("scheduler did not log the failed reminder run; log so far: %q", logged)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	<-done
	if got := reminderLastSent(t, st, reqID); got.Valid {
		t.Fatal("request was marked as reminded although the email was refused")
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
