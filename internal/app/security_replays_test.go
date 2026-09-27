package app

// Permanent regression checks derived from the original nine exploit probes.
// These assertions require the attacks to be blocked.
import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

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
	s.login(s.cfg.AdminEmail, testAdminPassword)
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
	var project int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, head).Scan(&project); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"type": {"reimbursement"}, "treatment": {"budget"}, "project_id": {fmt.Sprint(project)}, "head_id": {fmt.Sprint(head)}, "manager_id": {fmt.Sprint(admin.ID)}, "amount": {"10"}, "short_title": {"=1+1"}, "purpose": {"Security formula probe"}, "expense_date": {"2026-06-03"}, "attachment_exception_reason": {"Synthetic test"}}
	created := s.postForm("/requests", form)
	requireStatus(t, created, 303)
	created.Body.Close()
	s.login(s.cfg.AdminEmail, testAdminPassword)
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
	s.login(s.cfg.AdminEmail, testAdminPassword)
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
