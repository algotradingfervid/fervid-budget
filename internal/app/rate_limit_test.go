package app

import (
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/config"
	"fervidbudget/internal/store"
)

// The login limiter reads its per-client and global caps from the
// configuration, so test suites can raise them while production keeps 30/300.
func TestLoginLimiterUsesConfiguredLimits(t *testing.T) {
	post := func(h http.Handler, ip string) int {
		r := httptest.NewRequest("POST", "http://example.test/login", nil)
		r.RemoteAddr = ip + ":4567"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })

	perClient := &App{cfg: config.Config{LoginLimitPerIP: 3, LoginLimitGlobal: 1000}}
	h := perClient.securityBoundary(ok)
	for i := 0; i < 3; i++ {
		if code := post(h, "192.0.2.1"); code != 204 {
			t.Fatalf("attempt %d from one client = %d, want 204", i+1, code)
		}
	}
	if code := post(h, "192.0.2.1"); code != 429 {
		t.Fatalf("attempt over the per-client limit = %d, want 429", code)
	}
	if code := post(h, "192.0.2.2"); code != 204 {
		t.Fatalf("another client after one hit its limit = %d, want 204", code)
	}

	overall := &App{cfg: config.Config{LoginLimitPerIP: 1000, LoginLimitGlobal: 5}}
	h = overall.securityBoundary(ok)
	for i := 0; i < 5; i++ {
		if code := post(h, fmt.Sprintf("192.0.2.%d", 10+i)); code != 204 {
			t.Fatalf("attempt %d overall = %d, want 204", i+1, code)
		}
	}
	if code := post(h, "192.0.2.99"); code != 429 {
		t.Fatalf("attempt over the global limit = %d, want 429", code)
	}

	generous := &App{cfg: config.Config{LoginLimitPerIP: 1000, LoginLimitGlobal: 5000}}
	h = generous.securityBoundary(ok)
	for i := 0; i < 200; i++ {
		if code := post(h, "127.0.0.1"); code != 204 {
			t.Fatalf("attempt %d with a raised limit = %d, want 204", i+1, code)
		}
	}

	// A Config built without the limits keeps the production defaults.
	if perIP, global := (&App{}).loginLimits(); perIP != 30 || global != 300 {
		t.Fatalf("default limits = %d/%d, want 30/300", perIP, global)
	}
}

// Behind a trusted reverse proxy every connection comes from the proxy, so the
// reset throttle must key on the forwarded client address. One client using
// up its allowance must not lock out a different client.
func TestPasswordResetThrottleKeysOnForwardedClient(t *testing.T) {
	s := newAppTestServer(t)
	cfg := s.cfg
	cfg.TrustedProxies = []string{"127.0.0.1"}
	am, err := auth.New(cfg, s.st)
	if err != nil {
		t.Fatal(err)
	}
	mailer := &resetTestMailer{}
	a := &App{cfg: cfg, st: s.st, auth: am, log: slog.Default(), mailer: mailer,
		tpl: template.Must(template.New("test").Parse(`{{define "login_help"}}{{.Notice}}{{end}}`))}
	actor, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.SetMailSettings(s.ctx, actor, store.MailSettings{SMTPHost: "localhost", SMTPFromAddr: "noreply@example.test", BaseURL: "https://budget.example.test"}); err != nil {
		t.Fatal(err)
	}
	send := func(client, email string) {
		form := url.Values{"email": {email}}
		req := httptest.NewRequest("POST", "/login/help", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "127.0.0.1:40000"
		req.Header.Set("X-Forwarded-For", client)
		a.passwordResetRequest(httptest.NewRecorder(), req)
	}

	// The first client uses up its allowance on unknown addresses, then asks
	// for a real account: that request is throttled, so no email goes out.
	for i := 0; i < 6; i++ {
		send("203.0.113.10", fmt.Sprintf("nobody%d@example.test", i))
	}
	send("203.0.113.10", s.cfg.AdminEmail)
	if len(mailer.messages) != 0 {
		t.Fatalf("throttled client still got %d reset email(s)", len(mailer.messages))
	}

	// A different client behind the same proxy has its own allowance.
	send("203.0.113.20", s.cfg.AdminEmail)
	if len(mailer.messages) != 1 {
		t.Fatalf("second client behind the proxy got %d reset emails, want 1 (shared throttle bucket)", len(mailer.messages))
	}
}
