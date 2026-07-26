package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"html/template"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

func TestHTTPErrorStatusContract(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("HTTP errors")
	paymentID, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{
		HeadID:      headID,
		PaidOn:      "2026-04-15",
		Amount:      100,
		VendorPayee: "Status tests",
		PaymentMode: "cash",
	})
	if err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)

	invalid := url.Values{"project_id": {"0"}, "name": {"Invalid head"}, "due_day": {"32"}}
	resp := s.postForm("/heads", invalid)
	requireStatus(t, resp, http.StatusBadRequest)
	if !strings.Contains(responseBody(t, resp), "validation") {
		t.Fatal("400 response does not explain a validation failure")
	}

	// CSRF failures must be rejected before an admin action reaches its handler.
	resp = s.request(http.MethodPost, "/budgets", strings.NewReader("month=2026-04"), "application/x-www-form-urlencoded")
	requireStatus(t, resp, http.StatusForbidden)
	if !strings.Contains(responseBody(t, resp), "form session expired") {
		t.Fatal("403 response did not give a safe CSRF recovery message")
	}

	resp = s.request(http.MethodGet, "/attachments/999999", nil, "")
	requireStatus(t, resp, http.StatusNotFound)
	_ = responseBody(t, resp)

	// The maximum accepted request body is deliberately exercised through the
	// real multipart endpoint. A rejected upload must return 413 and leave no
	// partial file behind.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("csrf", s.csrf()); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("attachment", "too-large.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, io.LimitReader(strings.NewReader(strings.Repeat("x", 1)), 1)); err != nil {
		t.Fatal(err)
	}
	// A 21 MiB file exceeds the request cap once multipart framing is included.
	if _, err := part.Write(bytes.Repeat([]byte("x"), 21<<20)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	resp = s.request(http.MethodPost, "/payments/"+strconvFormat(paymentID)+"/attachments", &body, mw.FormDataContentType())
	requireStatus(t, resp, http.StatusRequestEntityTooLarge)
	_ = responseBody(t, resp)
	atts, err := s.st.Attachments(s.ctx, paymentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 0 {
		t.Fatalf("oversized upload persisted %d attachment rows", len(atts))
	}
	entries, err := os.ReadDir(s.cfg.AttachmentDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("oversized upload left %d files behind", len(entries))
	}
}

func TestServerUsesFiniteTimeoutsAndIssuesRequestIDs(t *testing.T) {
	s := newAppTestServer(t)
	if s.http.ReadHeaderTimeout <= 0 || s.http.ReadTimeout <= 0 || s.http.WriteTimeout <= 0 || s.http.IdleTimeout <= 0 {
		t.Fatalf("server timeouts must all be finite: header=%s read=%s write=%s idle=%s", s.http.ReadHeaderTimeout, s.http.ReadTimeout, s.http.WriteTimeout, s.http.IdleTimeout)
	}
	if s.http.ReadHeaderTimeout > s.http.ReadTimeout {
		t.Fatalf("read-header timeout (%s) must not exceed full read timeout (%s)", s.http.ReadHeaderTimeout, s.http.ReadTimeout)
	}
	if s.http.IdleTimeout < time.Second {
		t.Fatalf("idle timeout (%s) is implausibly short", s.http.IdleTimeout)
	}

	first := s.request(http.MethodGet, "/login", nil, "")
	requireStatus(t, first, http.StatusOK)
	firstID := first.Header.Get("X-Request-ID")
	_ = responseBody(t, first)
	if firstID == "" {
		t.Fatal("successful response is missing X-Request-ID")
	}
	second := s.request(http.MethodGet, "/login", nil, "")
	requireStatus(t, second, http.StatusOK)
	secondID := second.Header.Get("X-Request-ID")
	_ = responseBody(t, second)
	if secondID == "" || secondID == firstID {
		t.Fatalf("request IDs must be non-empty and unique: first=%q second=%q", firstID, secondID)
	}

	missing := s.request(http.MethodGet, "/attachments/999999", nil, "")
	requireStatus(t, missing, http.StatusSeeOther) // unauthenticated responses still carry correlation IDs.
	if got := missing.Header.Get("X-Request-ID"); got == "" {
		t.Fatal("error/redirect response is missing X-Request-ID")
	}
	_ = responseBody(t, missing)
}

func TestPanicRecoveryReturnsSafeErrorAndEmitsStructuredLogs(t *testing.T) {
	s := newAppTestServer(t)
	var logs bytes.Buffer
	am, err := auth.New(s.cfg, s.st)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{
		cfg:  s.cfg,
		st:   s.st,
		auth: am,
		tpl:  template.Must(template.New("base").Parse(`{{define "error_page"}}<main><p>{{.Error}}</p><p>Request ID: {{.RequestID}}</p></main>{{end}}`)),
		log:  slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	h := a.httpObservability(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("database password: must-not-reach-client")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://budget.test/panic", nil).WithContext(context.Background())
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
	requestID := rec.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("recovered panic response is missing X-Request-ID")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Something went wrong") || !strings.Contains(body, requestID) {
		t.Fatalf("panic error body is not safe/correlated: %s", body)
	}
	if strings.Contains(body, "database password") || strings.Contains(body, "must-not-reach-client") {
		t.Fatalf("panic body leaked internal detail: %s", body)
	}

	entries := decodeJSONLogLines(t, logs.String())
	var panicLog, completion map[string]any
	for _, entry := range entries {
		switch entry["msg"] {
		case "http panic recovered":
			panicLog = entry
		case "http request completed":
			completion = entry
		}
	}
	if panicLog == nil || completion == nil {
		t.Fatalf("expected panic and completion JSON events, got %v", entries)
	}
	for name, entry := range map[string]map[string]any{"panic": panicLog, "completion": completion} {
		if entry["request_id"] != requestID || entry["method"] != http.MethodGet || entry["path"] != "/panic" {
			t.Fatalf("%s log has incomplete request context: %v", name, entry)
		}
	}
	if completion["status"] != float64(http.StatusInternalServerError) {
		t.Fatalf("completion status = %#v, want 500", completion["status"])
	}
}

func decodeJSONLogLines(t *testing.T, raw string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("expected structured log entries")
	}
	entries := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}
