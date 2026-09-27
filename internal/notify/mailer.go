package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

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
	st           *store.Store
	password     string
	allowedHosts []string
	// sendMail is injectable so the message the transport would put on the wire
	// can be asserted without opening a socket.
	sendMail func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

func NewSMTPMailer(st *store.Store, password string, allowedHosts ...string) *SMTPMailer {
	return &SMTPMailer{st: st, password: password, allowedHosts: allowedHosts}
}

func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	app, err := m.st.GetMailSettings(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(app.SMTPHost) == "" {
		return fmt.Errorf("smtp host is not configured")
	}
	if !AllowedSMTPHost(app.SMTPHost, m.allowedHosts) {
		return fmt.Errorf("SMTP host is not in the operator allowlist")
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
	if m.sendMail != nil {
		return m.sendMail(addr, auth, envelopeAddr(app.SMTPFromAddr, msg.From), recipients, []byte(buildRFC822(msg)))
	}
	return sendTLSMail(ctx, app.SMTPHost, port, auth, envelopeAddr(app.SMTPFromAddr, msg.From), recipients, []byte(buildRFC822(msg)))
}

func buildRFC822(msg Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", encodeFromHeader(msg.From))
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(msg.To, ", "))
	if len(msg.Cc) > 0 {
		fmt.Fprintf(&b, "Cc: %s\r\n", strings.Join(msg.Cc, ", "))
	}
	// Header text must be ASCII unless the server negotiated SMTPUTF8, which
	// net/smtp does only when the server offers it — and every default subject
	// carries a ₹ or an em dash. An RFC 2047 encoded-word is readable by every
	// MTA and client; QEncoding leaves an all-ASCII subject untouched (notify-3).
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", msg.Subject))
	b.WriteString("MIME-Version: 1.0\r\n")
	// UTF-8 matters: every amount in this product carries a ₹.
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(msg.Body)
	return b.String()
}

// encodeFromHeader encodes a non-ASCII display name ("Fervid Bügets <x@y>") as
// an RFC 2047 encoded-word, for the same reason the Subject is. An all-ASCII
// From, or one that does not parse as an address, is written as it was.
func encodeFromHeader(from string) string {
	ascii := true
	for i := 0; i < len(from); i++ {
		if from[i] > 0x7e {
			ascii = false
			break
		}
	}
	if ascii {
		return from
	}
	addr, err := mail.ParseAddress(from)
	if err != nil {
		return from
	}
	return addr.String() // net/mail Q-encodes a non-ASCII name
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

// Only an operator may authorize the destination to which the SMTP secret is sent.
func AllowedSMTPHost(host string, allowed []string) bool {
	if strings.ContainsAny(host, "\r\n") {
		return false
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || strings.ContainsAny(host, "/\\:@\r\n") {
		return false
	}
	for _, v := range allowed {
		if host == strings.ToLower(strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}
func sendTLSMail(ctx context.Context, host string, port int, auth smtp.Auth, from string, to []string, msg []byte) error {
	if port != 587 && port != 465 {
		return fmt.Errorf("SMTP port must be 587 or 465")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("SMTP resolution failed")
	}
	if len(ips) == 0 {
		return fmt.Errorf("SMTP resolution failed")
	}
	for _, ip := range ips {
		if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
			return fmt.Errorf("SMTP destination is not public")
		}
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(ips[0].IP.String(), fmt.Sprint(port)))
	if err != nil {
		return fmt.Errorf("SMTP connection failed")
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { rawConn.Close() })
	defer stop()
	tc := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if port == 465 {
		secured := tls.Client(conn, tc)
		if err := secured.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("SMTP TLS failed")
		}
		conn = secured
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("SMTP greeting failed")
	}
	defer client.Close()
	if port == 587 {
		if err := client.StartTLS(tc); err != nil {
			return fmt.Errorf("SMTP requires verified TLS")
		}
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed")
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("SMTP sender refused")
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("SMTP recipient refused")
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP data refused")
	}
	if _, err = w.Write(msg); err != nil {
		return fmt.Errorf("SMTP delivery failed")
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("SMTP delivery failed")
	}
	return client.Quit()
}
