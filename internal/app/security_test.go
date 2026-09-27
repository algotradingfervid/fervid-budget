package app

// Permanent regression checks derived from the original nine exploit probes.
// These assertions require the attacks to be blocked.
import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"html/template"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/config"
	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

func TestSecurityReplayLegacyPaymentScopeBypass(t *testing.T) {
	s := newAppTestServer(t)
	admin, head := s.seedHead("security scope")
	id, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{HeadID: head, PaidOn: "2026-06-03", Amount: 4200, VendorPayee: "PRIVATE-PAYEE-SECURITY"})
	if err != nil {
		t.Fatal(err)
	}
	s.seedProbeUser("scoped@example.test", "Scoped", "ScopedPass123", "Scoped editor", []store.Grant{{Resource: "payment", Action: "view"}, {Resource: "payment", Action: "edit"}, {Resource: "payment", Action: "void"}}, []store.ScopeGrant{{Resource: "payment", Scope: "own"}})
	s.login("scoped@example.test", "ScopedPass123")
	denied := s.request("GET", fmt.Sprintf("/payments/%d", id), nil, "")
	requireStatus(t, denied, 404)
	denied.Body.Close()
	edit := s.request("GET", fmt.Sprintf("/payments/%d/edit", id), nil, "")
	requireStatus(t, edit, 404)
	if strings.Contains(responseBody(t, edit), "PRIVATE-PAYEE-SECURITY") {
		t.Fatal("edit exposed another user payment")
	}
	changed := s.postForm(fmt.Sprintf("/payments/%d/edit", id), url.Values{"head_id": {fmt.Sprint(head)}, "paid_on": {"2026-06-03"}, "amount": {"99.00"}, "vendor_payee": {"ALTERED-BY-OTHER-USER"}, "payment_mode": {"cash"}})
	requireStatus(t, changed, 404)
	changed.Body.Close()
	p, err := s.st.Payment(s.ctx, id)
	if err != nil || p.Amount != 4200 {
		t.Fatalf("unauthorized mutation: %v %+v", err, p)
	}
	voided := s.postForm(fmt.Sprintf("/payments/%d/void", id), url.Values{"reason": {"Unauthorized scope probe"}})
	requireStatus(t, voided, 404)
	voided.Body.Close()
	p, err = s.st.Payment(s.ctx, id)
	if err != nil || p.VoidedAt != nil {
		t.Fatal("unauthorized void persisted")
	}
}

func TestSecurityReplayAuditPaymentDisclosure(t *testing.T) {
	s := newAppTestServer(t)
	admin, head := s.seedHead("audit scope")
	id, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{HeadID: head, PaidOn: "2026-06-03", Amount: 4200, VendorPayee: "AUDIT-PRIVATE-PAYEE", ReferenceNo: "AUDIT-PRIVATE-REFERENCE"})
	if err != nil {
		t.Fatal(err)
	}
	s.seedProbeUser("audit@example.test", "Audit reader", "AuditPass12345", "Scoped audit", []store.Grant{{Resource: "audit", Action: "view"}, {Resource: "payment", Action: "view"}}, []store.ScopeGrant{{Resource: "payment", Scope: "own"}, {Resource: "request", Scope: "own"}})
	s.login("audit@example.test", "AuditPass12345")
	denied := s.request("GET", fmt.Sprintf("/payments/%d", id), nil, "")
	requireStatus(t, denied, 404)
	denied.Body.Close()
	res := s.request("GET", fmt.Sprintf("/audit?entity=payment&id=%d", id), nil, "")
	requireStatus(t, res, 200)
	body := responseBody(t, res)
	if strings.Contains(body, "AUDIT-PRIVATE-PAYEE") || strings.Contains(body, "AUDIT-PRIVATE-REFERENCE") {
		t.Fatal("audit disclosed private fields")
	}
}

type reviewNoMail struct{}

func (reviewNoMail) Send(_ context.Context, _ notify.Message) error { return nil }

func TestSecurityReplayNotificationScopeDisclosure(t *testing.T) {
	s := newAppTestServer(t)
	admin, head := s.seedHead("notification scope")
	id := s.seedPendingRequest(1, admin.ID, admin.ID, head, 1234500)
	u := s.seedProbeUser("notify@example.test", "Scoped accounts", "NotifyPass123", "Scoped accounts", []store.Grant{{Resource: "payment", Action: "process"}, {Resource: "request", Action: "view"}}, []store.ScopeGrant{{Resource: "request", Scope: "own"}})
	_, err := s.st.DB().Exec(`UPDATE payment_requests SET vendor_payee='NOTIFICATION-PRIVATE-PAYEE',status='approved',approved_amount=amount WHERE id=?`, id)
	if err != nil {
		t.Fatal(err)
	}
	req, err := s.st.Request(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = notify.NewService(s.st, reviewNoMail{}).Notify(s.ctx, notify.EventRequestApproved, req); err != nil {
		t.Fatal(err)
	}
	s.login(u.Email, "NotifyPass123")
	denied := s.request("GET", fmt.Sprintf("/requests/%d", id), nil, "")
	requireStatus(t, denied, 404)
	denied.Body.Close()
	body := responseBody(t, s.request("GET", "/notifications", nil, ""))
	if strings.Contains(body, "NOTIFICATION-PRIVATE-PAYEE") {
		t.Fatal("notification leaked")
	}
}

func TestSecurityReplayLogoutReplay(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
	base, _ := url.Parse(s.server.URL)
	var stolen *http.Cookie
	for _, c := range s.client.Jar.Cookies(base) {
		if c.Name == "fervid_session" {
			stolen = c
		}
	}
	if stolen == nil {
		t.Fatal("no session")
	}
	out := s.postForm("/logout", nil)
	requireStatus(t, out, 303)
	out.Body.Close()
	req, _ := http.NewRequest("GET", s.server.URL+"/users", nil)
	req.AddCookie(stolen)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	replay, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, replay, 303)
	replay.Body.Close()
}

func TestSecurityReplayCrossSiteLogin(t *testing.T) {
	s := newAppTestServer(t)
	form := url.Values{"email": {s.cfg.AdminEmail}, "password": {testAdminPassword}}
	req, _ := http.NewRequest("POST", s.server.URL+"/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://attacker.invalid")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, res, 403)
	res.Body.Close()
	page := s.request("GET", "/users", nil, "")
	requireStatus(t, page, 303)
	page.Body.Close()
}

func TestSecurityReplayCSVFormula(t *testing.T) {
	s := newAppTestServer(t)
	admin, head := s.seedHead("CSV")
	u := s.seedRequester("csv@example.test", "CSV requester", "CsvPass12345")
	s.login(u.Email, "CsvPass12345")
	project := projectOf(t, s, head)
	form := url.Values{"type": {"reimbursement"}, "treatment": {"budget"}, "project_id": {fmt.Sprint(project)}, "head_id": {fmt.Sprint(head)}, "manager_id": {fmt.Sprint(admin.ID)}, "amount": {"10"}, "short_title": {"=1+1"}, "purpose": {"Security formula probe"}, "expense_date": {"2026-06-03"}, "attachment_exception_reason": {"Synthetic test"}}
	created := s.postForm("/requests", form)
	requireStatus(t, created, 303)
	created.Body.Close()
	s.loginAdmin()
	res := s.request("GET", "/requests/export.csv?scope=all&bucket=all", nil, "")
	requireStatus(t, res, 200)
	rows, err := csv.NewReader(strings.NewReader(responseBody(t, res))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if len(r) > 3 && r[3] == "'=1+1" {
			found = true
		}
	}
	if !found {
		t.Fatal("CSV did not escape the formula")
	}
}

func TestSecurityReplayLoginEnumeration(t *testing.T) {
	s := newAppTestServer(t)
	for _, email := range []string{s.cfg.AdminEmail, "absent@example.test"} {
		var body string
		for i := 0; i < 6; i++ {
			form := url.Values{"email": {email}, "password": {"WrongPass123"}}
			body = responseBody(t, s.request("POST", "/login", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"))
		}
		locked := strings.Contains(body, "Too many failed attempts")
		if locked || !strings.Contains(body, "Invalid email or password") {
			t.Fatalf("unexpected result for %s", email)
		}
	}
}

func TestSecurityReplayHeadersDefaultsAndCSRF(t *testing.T) {
	t.Setenv("FERVID_ADMIN_PASSWORD", "")
	t.Setenv("FERVID_SECURE_COOKIES", "")
	t.Setenv("FERVID_ADDR", "")
	cfg := config.Load()
	if cfg.AdminPassword != "" || !cfg.SecureCookies || cfg.Addr != "127.0.0.1:8080" {
		t.Fatal("insecure defaults")
	}
	s := newAppTestServer(t)
	s.loginAdmin()
	res := s.request("GET", "/users", nil, "")
	requireStatus(t, res, 200)
	defer res.Body.Close()
	for _, h := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "Cache-Control"} {
		if res.Header.Get(h) == "" {
			t.Fatalf("header %s missing", h)
		}
	}
	m, _ := auth.New(s.cfg, s.st)
	req, _ := http.NewRequest("POST", "http://app.example.test/projects", strings.NewReader("csrf=attacker-chosen"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "fervid_csrf", Value: "attacker-chosen"})
	if m.CheckCSRF(req) {
		t.Fatal("arbitrary matching cookie accepted")
	}
}

func TestSecurityReplayUnauthenticatedAuditAmplification(t *testing.T) {
	s := newAppTestServer(t)
	email := strings.Repeat("a", 256<<10) + "@absent.invalid"
	for i := 0; i < 2; i++ {
		form := url.Values{"email": {email}, "password": {"WrongPass123"}}
		res := s.request("POST", "/login", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
		requireStatus(t, res, 413)
		res.Body.Close()
	}
	var count, total int
	if err := s.st.DB().QueryRow(`SELECT count(*),COALESCE(sum(length(actor_name)),0) FROM audit_log WHERE action='login_failed'`).Scan(&count, &total); err != nil {
		t.Fatal(err)
	}
	if count != 0 || total != 0 {
		t.Fatalf("unexpected storage: %d rows %d bytes", count, total)
	}
}

func TestSecurityBoundaryOriginsLimitsAndHeaders(t *testing.T) {
	a := &App{}
	handler := a.securityBoundary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		origin, site string
		want         int
	}{{"http://example.test", "same-origin", 204}, {"http://attacker.test", "cross-site", 403}, {"http://sibling.example.test", "same-site", 403}, {"null", "cross-site", 403}} {
		r := httptest.NewRequest("POST", "http://example.test/login", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("origin %s: %d", tc.origin, w.Code)
		}
		for _, key := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "Referrer-Policy", "Cache-Control"} {
			if w.Header().Get(key) == "" {
				t.Errorf("missing %s", key)
			}
		}
	}
	for i := 0; i < 31; i++ {
		r := httptest.NewRequest("POST", "http://example.test/login", nil)
		r.RemoteAddr = "192.0.2.20:4567"
		r.Header.Set("X-Forwarded-For", "1.2.3.4")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if i == 30 && w.Code != 429 {
			t.Fatalf("login source limit: %d", w.Code)
		}
	}
	if got := requestBodyLimit(httptest.NewRequest("POST", "/login", nil)); got != 8<<10 {
		t.Fatalf("login cap: %d", got)
	}
	for _, p := range []string{"/", "/_kitchensink.html", "/unexpected-secret.env"} {
		w := httptest.NewRecorder()
		publicStatic(false).ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 404 {
			t.Fatalf("static path %s: %d", p, w.Code)
		}
	}
	// With dev pages switched on, the design-system reference page is served
	// but nothing else slips through the allowlist. The file server reads
	// web/static relative to the working directory, which is the repository
	// root when the server runs and this package's directory under go test.
	t.Chdir("../..")
	for p, want := range map[string]int{"/_kitchensink.html": 200, "/": 404, "/unexpected-secret.env": 404} {
		w := httptest.NewRecorder()
		publicStatic(true).ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != want {
			t.Fatalf("dev static path %s: %d, want %d", p, w.Code, want)
		}
	}
}

func TestSecurityLogoutRevokesCopiedCookieAndBoundsAudit(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
	u, _ := url.Parse(s.server.URL)
	cookies := s.client.Jar.Cookies(u)
	responseBody(t, s.postForm("/logout", nil))
	r, _ := http.NewRequest("GET", s.server.URL+"/users", nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 303 {
		t.Fatalf("copied cookie remains valid: %d", resp.StatusCode)
	}
	form := url.Values{"email": {strings.Repeat("x", 4000)}, "password": {"wrong"}}
	responseBody(t, s.request("POST", "/login", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"))
	var largest int
	if err = s.st.DB().QueryRow(`SELECT COALESCE(MAX(length(actor_name)),0) FROM audit_log WHERE action='login_failed'`).Scan(&largest); err != nil {
		t.Fatal(err)
	}
	if largest > 254 {
		t.Fatalf("unbounded audit email: %d", largest)
	}
}

func TestSecurityCSVAndAttachmentValidationQuotaAndContainment(t *testing.T) {
	for _, value := range []string{"=SUM(1,2)", " +cmd", "\t@bad", "\rformula", "\uFEFF=bad", "-formula"} {
		if !strings.HasPrefix(csvText(value), "'") {
			t.Fatalf("unsafe CSV: %q", value)
		}
	}
	if csvText("Ordinary vendor") != "Ordinary vendor" {
		t.Fatal("ordinary CSV changed")
	}
	for _, data := range [][]byte{[]byte("%PDF-1.4\nnot a document\n%%EOF"), append([]byte(validTestPDF), []byte("<script>bad()</script>")...)} {
		if _, err := validatedAttachmentType(bytes.NewReader(data), "file.pdf"); err == nil {
			t.Fatal("invalid PDF accepted")
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	payload := append(append([]byte{}, encoded.Bytes()...), []byte("APPENDED-PAYLOAD")...)
	var clean bytes.Buffer
	if _, err := copyAttachment(&clean, bytes.NewReader(payload), "image/png", 1<<20); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(clean.Bytes(), []byte("APPENDED-PAYLOAD")) {
		t.Fatal("image payload survived re-encoding")
	}
	s := newAppTestServer(t)
	a := s.probeApp()
	a.tpl = template.Must(template.New("error_page").Parse(`{{.Error}}`))
	a.cfg.AttachmentQuotaBytes = 1
	body, ct := attachmentMultipart(t, nil, "valid.png", encoded.Bytes())
	r := httptest.NewRequest("POST", "/requests", body)
	r.Header.Set("Content-Type", ct)
	if _, _, err := a.stageUploadedAttachment(r); err == nil {
		t.Fatal("quota bypass")
	}
	if r.MultipartForm != nil {
		r.MultipartForm.RemoveAll()
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("SECRET-OUTSIDE"), 0600)
	link := filepath.Join(a.cfg.AttachmentDir, "escaped")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.serveAttachmentFile(w, httptest.NewRequest("GET", "/attachments/1", nil), 1, link, "file.txt", "text/plain")
	if w.Code != 404 || strings.Contains(w.Body.String(), "SECRET-OUTSIDE") {
		t.Fatalf("symlink escaped: %d", w.Code)
	}
}

func TestSecurityInvalidResetTokenHasNoSideEffects(t *testing.T) {
	s := newAppTestServer(t)
	responseBody(t, s.request("GET", "/login/help", nil, ""))
	before, _ := s.st.UserByEmail(context.Background(), s.cfg.AdminEmail)
	resp := s.postForm("/login/reset", url.Values{"token": {strings.Repeat("a", 64)}, "password": {"NewPassword12345"}, "confirm_password": {"NewPassword12345"}})
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	after, _ := s.st.UserByEmail(context.Background(), s.cfg.AdminEmail)
	if before.PasswordHash != after.PasswordHash || before.SessionVersion != after.SessionVersion {
		t.Fatal("invalid reset changed account")
	}
	// Limiter state expires and cannot grow indefinitely.
	var l authLimiter
	now := time.Now()
	for i := 0; i < 30; i++ {
		if !l.allow("ip", 30, now) {
			t.Fatal("premature limit")
		}
	}
	if l.allow("ip", 30, now) {
		t.Fatal("limit bypass")
	}
	if !l.allow("ip", 30, now.Add(time.Minute)) {
		t.Fatal("limit failed to expire")
	}
}
