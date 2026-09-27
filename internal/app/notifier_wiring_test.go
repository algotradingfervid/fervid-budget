package app

import (
	"context"
	"strings"
	"testing"

	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

// The service handed to the reminder scheduler is the one the handlers use,
// and its mailer honours the operator's SMTP host allowlist, so reminder
// emails go through the same checks as every other email.
func TestNewWithNotifierSharesTheAppMailerAndAllowlist(t *testing.T) {
	s := newAppTestServer(t)
	a, _, err := build(s.cfg, s.st)
	if err != nil {
		t.Fatal(err)
	}
	_, svc, err := NewWithNotifier(s.cfg, s.st)
	if err != nil {
		t.Fatal(err)
	}
	if svc == nil {
		t.Fatal("NewWithNotifier returned no notification service")
	}

	actor := s.admin()
	// Port 2525 is refused by the transport before any network access, after
	// the allowlist check has passed, so the error tells us which check ran.
	if err := s.st.SetMailSettings(s.ctx, actor, store.MailSettings{SMTPHost: "smtp.example.test", SMTPPort: 2525, SMTPFromAddr: "noreply@example.test"}); err != nil {
		t.Fatal(err)
	}
	err = a.mailer.Send(context.Background(), notify.Message{To: []string{"x@example.test"}, Subject: "s", Body: "b"})
	if err == nil || strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("app mailer error = %v, want the allowlisted host to pass the allowlist", err)
	}
}
