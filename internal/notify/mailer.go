package notify

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"

	"fervidbudget/internal/store"
)

type Message struct {
	From    string
	To      []string
	Cc      []string
	Subject string
	Body    string
}

type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// SMTPMailer is the real transport. Host, port, username and from-address are
// admin-editable data in app_settings; the password is not — it comes from the
// environment via config.SMTPPassword and is never persisted or logged.
type SMTPMailer struct {
	st       *store.Store
	password string
	// sendMail is injectable so the message the transport would put on the wire
	// can be asserted without opening a socket.
	sendMail func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

func NewSMTPMailer(st *store.Store, password string) *SMTPMailer {
	return &SMTPMailer{st: st, password: password, sendMail: smtp.SendMail}
}

func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	app, err := m.st.GetMailSettings(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(app.SMTPHost) == "" {
		return fmt.Errorf("smtp host is not configured")
	}
	port := app.SMTPPort
	if port == 0 {
		port = 587
	}
	recipients := append(append([]string{}, msg.To...), msg.Cc...)
	if len(recipients) == 0 {
		return nil
	}
	var auth smtp.Auth
	if strings.TrimSpace(app.SMTPUsername) != "" {
		auth = smtp.PlainAuth("", app.SMTPUsername, m.password, app.SMTPHost)
	}
	addr := fmt.Sprintf("%s:%d", app.SMTPHost, port)
	return m.sendMail(addr, auth, envelopeAddr(app.SMTPFromAddr, msg.From), recipients, []byte(buildRFC822(msg)))
}

func buildRFC822(msg Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", msg.From)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(msg.To, ", "))
	if len(msg.Cc) > 0 {
		fmt.Fprintf(&b, "Cc: %s\r\n", strings.Join(msg.Cc, ", "))
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", msg.Subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	// UTF-8 matters: every amount in this product carries a ₹.
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(msg.Body)
	return b.String()
}

// envelopeAddr is the bare address the SMTP conversation uses, which is not the
// same as the display From header ("Fervid <noreply@x>").
func envelopeAddr(fromAddr, fromHeader string) string {
	if a := strings.TrimSpace(fromAddr); a != "" {
		return a
	}
	if i := strings.LastIndex(fromHeader, "<"); i >= 0 {
		if j := strings.Index(fromHeader[i:], ">"); j > 0 {
			return fromHeader[i+1 : i+j]
		}
	}
	return fromHeader
}
