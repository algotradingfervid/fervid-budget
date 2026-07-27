package notify

import (
	"context"
	"net/smtp"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

func TestSMTPMailerBuildsMessageAndUsesEnvPassword(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	must(t, st.SetMailSettings(ctx, actor, store.MailSettings{
		SMTPHost: "smtp.example.test", SMTPPort: 2525, SMTPUsername: "mailer@example.test",
		SMTPFromName: "Fervid", SMTPFromAddr: "noreply@example.test",
	}))
	m := NewSMTPMailer(st, "s3cr3t-from-env")
	var gotAddr, gotFrom string
	var gotAuth smtp.Auth
	var gotTo []string
	var gotMsg []byte
	m.sendMail = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr, gotAuth, gotFrom, gotTo, gotMsg = addr, a, from, to, msg
		return nil
	}
	err := m.Send(ctx, Message{From: "Fervid <noreply@example.test>", To: []string{"a@x.test"}, Cc: []string{"b@y.test"}, Subject: "Hi", Body: "Body ₹1.00"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAddr != "smtp.example.test:2525" {
		t.Fatalf("addr = %q, want smtp.example.test:2525", gotAddr)
	}
	if gotFrom != "noreply@example.test" {
		t.Fatalf("envelope from = %q, want noreply@example.test", gotFrom)
	}
	if gotAuth == nil {
		t.Fatal("expected PLAIN auth when username is configured")
	}
	if len(gotTo) != 2 {
		t.Fatalf("recipients = %v, want To+Cc", gotTo)
	}
	s := string(gotMsg)
	for _, want := range []string{"From: Fervid <noreply@example.test>", "To: a@x.test", "Cc: b@y.test", "Subject: Hi", "Body ₹1.00", "charset=UTF-8"} {
		if !strings.Contains(s, want) {
			t.Fatalf("message missing %q:\n%s", want, s)
		}
	}
}

func TestSMTPMailerRequiresHost(t *testing.T) {
	st := openTestStore(t)
	m := NewSMTPMailer(st, "")
	err := m.Send(context.Background(), Message{To: []string{"a@x.test"}, Subject: "x", Body: "y"})
	if err == nil {
		t.Fatal("expected error when smtp host is not configured")
	}
}

// With no username there is nothing to authenticate with, and passing a nil-safe
// PLAIN auth to a server that does not want it fails the conversation.
func TestSMTPMailerOmitsAuthWithoutUsername(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	must(t, st.SetMailSettings(ctx, actor, store.MailSettings{SMTPHost: "smtp.example.test"}))
	m := NewSMTPMailer(st, "")
	var gotAuth smtp.Auth
	var gotAddr string
	m.sendMail = func(addr string, a smtp.Auth, _ string, _ []string, _ []byte) error {
		gotAddr, gotAuth = addr, a
		return nil
	}
	must(t, m.Send(ctx, Message{From: "f@x.test", To: []string{"a@x.test"}, Subject: "x", Body: "y"}))
	if gotAuth != nil {
		t.Fatal("auth must be omitted when no username is configured")
	}
	if gotAddr != "smtp.example.test:587" {
		t.Fatalf("addr = %q, want the default port 587", gotAddr)
	}
}

// A message with no recipients is a no-op, not an SMTP round trip.
func TestSMTPMailerSkipsEmptyRecipients(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	must(t, st.SetMailSettings(ctx, actor, store.MailSettings{SMTPHost: "smtp.example.test"}))
	m := NewSMTPMailer(st, "")
	called := false
	m.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		called = true
		return nil
	}
	must(t, m.Send(ctx, Message{From: "f@x.test", Subject: "x", Body: "y"}))
	if called {
		t.Fatal("sent a message with no recipients")
	}
}
