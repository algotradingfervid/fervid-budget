package notify

import (
	"context"
	"mime"
	"net/mail"
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
	m := NewSMTPMailer(st, "s3cr3t-from-env", "smtp.example.test")
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
	m := NewSMTPMailer(st, "", "smtp.example.test")
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
	m := NewSMTPMailer(st, "", "smtp.example.test")
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
	m := NewSMTPMailer(st, "", "smtp.example.test")
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

// Header text must be ASCII unless the server negotiated SMTPUTF8, and
// net/smtp only does that when the server offers it. Every default subject
// carries a ₹ or an em dash, so the Subject and a non-ASCII From display name
// go out as RFC 2047 encoded-words any MTA can carry (notify-3).
func TestBuildRFC822EncodesNonASCIIHeaders(t *testing.T) {
	subject := "PR-2026-000002 needs your approval — ₹2,250.50 to Rhea Requester"
	raw := buildRFC822(Message{From: "Fervid Bügets <noreply@x.test>", To: []string{"a@x.test"},
		Subject: subject, Body: "Body ₹1.00"})
	head, body, ok := strings.Cut(raw, "\r\n\r\n")
	if !ok {
		t.Fatalf("no header/body separator:\n%s", raw)
	}
	for i := 0; i < len(head); i++ {
		if head[i] > 0x7e {
			t.Fatalf("header carries a raw 8-bit byte at %d:\n%s", i, head)
		}
	}
	var gotSubject, gotFrom string
	for _, line := range strings.Split(head, "\r\n") {
		if v, ok := strings.CutPrefix(line, "Subject: "); ok {
			gotSubject = v
		}
		if v, ok := strings.CutPrefix(line, "From: "); ok {
			gotFrom = v
		}
	}
	if s, err := new(mime.WordDecoder).DecodeHeader(gotSubject); err != nil || s != subject {
		t.Fatalf("decoded Subject = %q (%v), want %q", s, err, subject)
	}
	if a, err := mail.ParseAddress(gotFrom); err != nil || a.Name != "Fervid Bügets" || a.Address != "noreply@x.test" {
		t.Fatalf("From %q parses to %+v (%v)", gotFrom, a, err)
	}
	if body != "Body ₹1.00" {
		t.Fatalf("body = %q, want it untouched", body)
	}
	// ASCII headers stay exactly as they were.
	plain := buildRFC822(Message{From: "Fervid <noreply@x.test>", To: []string{"a@x.test"}, Subject: "Hi"})
	for _, want := range []string{"From: Fervid <noreply@x.test>\r\n", "Subject: Hi\r\n"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("ASCII header changed; missing %q in:\n%s", want, plain)
		}
	}
}
