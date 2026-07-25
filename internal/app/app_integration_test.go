package app

import (
	"bytes"
	"context"
	"html/template"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/config"
	"fervidbudget/internal/store"
)

const testAdminPassword = "AdminPass1234"

type appTestServer struct {
	t      *testing.T
	ctx    context.Context
	cfg    config.Config
	st     *store.Store
	http   *http.Server
	server *httptest.Server
	client *http.Client
}

func newAppTestServer(t *testing.T) *appTestServer {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		DBPath:         filepath.Join(dir, "fervid.db"),
		AttachmentDir:  filepath.Join(dir, "attachments"),
		BackupDir:      filepath.Join(dir, "backups"),
		SessionKey:     "test-session-key-that-is-long-enough",
		AdminEmail:     "admin@example.test",
		AdminName:      "Test Admin",
		AdminPassword:  testAdminPassword,
		BackupKeepDays: 1,
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	httpServer, err := New(cfg, st)
	if err != nil {
		_ = st.Close()
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpServer.Handler)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(func() {
		server.Close()
		_ = st.Close()
	})
	return &appTestServer{t: t, ctx: context.Background(), cfg: cfg, st: st, http: httpServer, server: server, client: client}
}

func (s *appTestServer) request(method, path string, body io.Reader, contentType string) *http.Response {
	s.t.Helper()
	req, err := http.NewRequest(method, s.server.URL+path, body)
	if err != nil {
		s.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	return resp
}

func responseBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func requireStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body := responseBody(t, resp)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, want, body)
	}
}

func (s *appTestServer) login(email, password string) {
	s.t.Helper()
	form := url.Values{"email": {email}, "password": {password}}
	resp := s.request(http.MethodPost, "/login", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		s.t.Fatalf("login status = %d, want 303; body: %s", resp.StatusCode, body)
	}
}

func (s *appTestServer) csrf() string {
	s.t.Helper()
	u, _ := url.Parse(s.server.URL)
	for _, c := range s.client.Jar.Cookies(u) {
		if c.Name == "fervid_csrf" {
			return c.Value
		}
	}
	s.t.Fatal("CSRF cookie was not set")
	return ""
}

func (s *appTestServer) postForm(path string, form url.Values) *http.Response {
	s.t.Helper()
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf", s.csrf())
	return s.request(http.MethodPost, path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
}

func (s *appTestServer) seedHead(name string) (store.User, int64) {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
	projectID, err := s.st.UpsertProject(s.ctx, 0, "Operations "+name, true, 1)
	if err != nil {
		s.t.Fatal(err)
	}
	headID, err := s.st.UpsertHead(s.ctx, 0, projectID, "Rent "+name, "5", true, 1)
	if err != nil {
		s.t.Fatal(err)
	}
	return admin, headID
}

func TestLoginPageIsSafeAndErrorsAreAnnounced(t *testing.T) {
	s := newAppTestServer(t)
	body := responseBody(t, s.request(http.MethodGet, "/login", nil, ""))
	if strings.Contains(body, testAdminPassword) || strings.Contains(body, s.cfg.AdminEmail) {
		t.Fatal("login page exposes bootstrap credentials")
	}
	if !strings.Contains(body, `autocomplete="current-password"`) {
		t.Fatal("login page is missing password autocomplete metadata")
	}

	form := url.Values{"email": {s.cfg.AdminEmail}, "password": {"wrong"}}
	body = responseBody(t, s.request(http.MethodPost, "/login", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"))
	if !strings.Contains(body, `role="alert"`) || !strings.Contains(body, `aria-live="assertive"`) {
		t.Fatal("login errors must be exposed to assistive technology")
	}
}

func TestLoginLockoutAndFailedAttemptsAreAudited(t *testing.T) {
	s := newAppTestServer(t)
	for i := 0; i < 5; i++ {
		form := url.Values{"email": {s.cfg.AdminEmail}, "password": {"incorrect"}}
		resp := s.request(http.MethodPost, "/login", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
		requireStatus(t, resp, http.StatusOK)
		_ = responseBody(t, resp)
	}
	form := url.Values{"email": {s.cfg.AdminEmail}, "password": {testAdminPassword}}
	body := responseBody(t, s.request(http.MethodPost, "/login", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"))
	if !strings.Contains(body, "Too many failed attempts") {
		t.Fatalf("locked login response did not explain lockout: %s", body)
	}
	locked, _, err := s.st.LoginLocked(s.ctx, s.cfg.AdminEmail)
	if err != nil || !locked {
		t.Fatalf("LoginLocked = %v, %v; want locked account", locked, err)
	}
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	audit, err := s.st.Audit(s.ctx, "user", admin.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	failed := 0
	for _, entry := range audit {
		if entry.Action == "login_failed" {
			failed++
		}
	}
	if failed < 5 {
		t.Fatalf("failed-login audit entries = %d, want at least 5", failed)
	}
}

func TestDataEntryHasRestrictedNavigationAndRoutes(t *testing.T) {
	s := newAppTestServer(t)
	hash, err := auth.HashPassword("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.CreateUser(s.ctx, "entry@example.test", "Entry User", hash, "data_entry", true); err != nil {
		t.Fatal(err)
	}
	s.login("entry@example.test", "EntryPassword123")
	body := responseBody(t, s.request(http.MethodGet, "/", nil, ""))
	for _, hidden := range []string{"Monthly plans", "href=\"/projects\"", "href=\"/users\"", "href=\"/audit\""} {
		if strings.Contains(body, hidden) {
			t.Fatalf("data-entry navigation exposes %q", hidden)
		}
	}
	if !strings.Contains(body, "Add payment") || !strings.Contains(body, "Reports") {
		t.Fatal("data entry user is missing allowed navigation")
	}
	for _, path := range []string{"/projects", "/heads", "/users", "/audit", "/budgets", "/months"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusForbidden)
		_ = responseBody(t, resp)
	}
}

func TestMonthNormalizationAndReportRangeExport(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/?month=not-a-month", nil, ""))
	current := time.Now().Format("2006-01")
	if !strings.Contains(body, `name="month" value="`+current+`"`) {
		t.Fatalf("invalid grid month was not normalized to %s", current)
	}
	body = responseBody(t, s.request(http.MethodGet, "/reports/heads?from=2026-05&to=2026-03", nil, ""))
	if !strings.Contains(body, "2026-03 to 2026-05") || !strings.Contains(body, "ytd.csv?from=2026-03&to=2026-05") {
		t.Fatalf("reversed report range was not reflected in controls/export: %s", body)
	}
	resp := s.request(http.MethodGet, "/reports/ytd.csv?from=2026-03&to=2026-05", nil, "")
	requireStatus(t, resp, http.StatusOK)
	csv := responseBody(t, resp)
	if !strings.HasPrefix(csv, "Period,Project,Head,Budget,Actual,Variance,Variance %") {
		t.Fatalf("report CSV has unexpected header: %s", csv)
	}
}

func TestPaymentErrorRetainsInputAndUsesHumanModes(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Retention")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	form := url.Values{
		"head_id":      {"" + strconvFormat(headID)},
		"paid_on":      {"2026-04-11"},
		"amount":       {"not-money"},
		"vendor_payee": {"Aster Stores"},
		"payment_mode": {"bank_transfer"},
		"invoice_no":   {"INV-42"},
		"reference_no": {"REF-42"},
		"remarks":      {"retain this note"},
	}
	resp := s.postForm("/payments", form)
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	for _, expected := range []string{"not-money", "Aster Stores", "INV-42", "REF-42", "retain this note", `value="bank_transfer" selected`, ">Bank transfer<"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("payment form did not retain/display %q", expected)
		}
	}
}

func TestLockedPaymentsAreReadOnlyAndRejectAttachmentUpload(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Locked")
	paymentID, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{HeadID: headID, PaidOn: "2026-04-12", Amount: 12345, VendorPayee: "Lock Test", PaymentMode: "cash"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.LockMonth(s.ctx, admin, "2026-04", "test lock"); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/payments/"+strconvFormat(paymentID)+"/edit", nil, ""))
	if !strings.Contains(body, "read-only") || !strings.Contains(body, "fieldset class=\"payment-section payment-core\" disabled") {
		t.Fatalf("locked payment edit form remains editable: %s", body)
	}
	body = responseBody(t, s.request(http.MethodGet, "/payments/"+strconvFormat(paymentID), nil, ""))
	if strings.Contains(body, "name=\"attachment\" required") {
		t.Fatal("locked payment detail exposes attachment upload")
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("csrf", s.csrf()); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("attachment", "receipt.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("receipt"))
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	resp := s.request(http.MethodPost, "/payments/"+strconvFormat(paymentID)+"/attachments", &buf, mw.FormDataContentType())
	if resp.StatusCode < http.StatusBadRequest {
		body := responseBody(t, resp)
		t.Fatalf("locked attachment upload status = %d; body %s", resp.StatusCode, body)
	}
	_ = responseBody(t, resp)
	atts, err := s.st.Attachments(s.ctx, paymentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 0 {
		t.Fatalf("locked payment has %d attachments after rejected upload", len(atts))
	}
}

func TestBudgetBatchValidationDoesNotPartiallySave(t *testing.T) {
	s := newAppTestServer(t)
	admin, headA := s.seedHead("BudgetA")
	_, headB := s.seedHead("BudgetB")
	if err := s.st.SetBudget(s.ctx, admin, headA, "2026-04", 10000); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	form := url.Values{
		"month":                          {"2026-04"},
		"budget_" + strconvFormat(headA): {"250.00"},
		"budget_" + strconvFormat(headB): {"not-a-number"},
	}
	resp := s.postForm("/budgets", form)
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	if !strings.Contains(body, "not-a-number") || !strings.Contains(strings.ToLower(body), "invalid budget") {
		t.Fatalf("budget validation response does not preserve the invalid row and safe error: %s", body)
	}
	budget, err := s.st.Budget(s.ctx, headA, "2026-04")
	if err != nil {
		t.Fatal(err)
	}
	if budget.Amount != 10000 {
		t.Fatalf("valid row was partially saved: got %d, want 10000", budget.Amount)
	}
}

func TestUserCreationEnforcesPasswordAndInactiveFlagWithCreateAudit(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	weak := url.Values{"email": {"weak@example.test"}, "name": {"Weak"}, "role": {"data_entry"}, "password": {"short"}, "active": {"on"}}
	resp := s.postForm("/users", weak)
	requireStatus(t, resp, http.StatusBadRequest)
	if !strings.Contains(responseBody(t, resp), "at least 12") {
		t.Fatal("weak password error was not returned")
	}

	inactive := url.Values{"email": {"inactive@example.test"}, "name": {"Inactive"}, "role": {"data_entry"}, "password": {"ValidPassword123"}}
	resp = s.postForm("/users", inactive)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	u, err := s.st.UserByEmail(s.ctx, "inactive@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if u.Active {
		t.Fatal("unchecked active box created an active user")
	}
	audit, err := s.st.Audit(s.ctx, "user", u.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) == 0 || audit[0].Action != "create" {
		t.Fatalf("user creation audit = %#v, want create action", audit)
	}
}

func TestAttachmentDownloadAuditFilterAndResponsiveTables(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Attachment")
	paymentID, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{HeadID: headID, PaidOn: "2026-04-14", Amount: 999, VendorPayee: "Proof", PaymentMode: "upi"})
	if err != nil {
		t.Fatal(err)
	}
	stored := filepath.Join(s.cfg.AttachmentDir, "receipt.txt")
	if err := os.WriteFile(stored, []byte("downloaded receipt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.st.AddAttachment(s.ctx, admin, paymentID, "receipt.txt", stored, "text/plain", 18); err != nil {
		t.Fatal(err)
	}
	atts, err := s.st.Attachments(s.ctx, paymentID)
	if err != nil || len(atts) != 1 {
		t.Fatalf("attachments = %#v, %v", atts, err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.request(http.MethodGet, "/attachments/"+strconvFormat(atts[0].ID), nil, "")
	requireStatus(t, resp, http.StatusOK)
	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, "receipt.txt") {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if body := responseBody(t, resp); body != "downloaded receipt" {
		t.Fatalf("attachment content = %q", body)
	}
	body := responseBody(t, s.request(http.MethodGet, "/audit?entity=payment&action=attach", nil, ""))
	if !strings.Contains(body, "Uploaded attachment receipt.txt") || !strings.Contains(body, "Attached") {
		t.Fatalf("attachment audit filter did not render expected entry: %s", body)
	}
	for _, path := range []string{"/payments", "/projects", "/reports/heads"} {
		body = responseBody(t, s.request(http.MethodGet, path, nil, ""))
		if !strings.Contains(body, "table-wrap") {
			t.Fatalf("%s is missing responsive table wrapper", path)
		}
	}
}

// shellProbe renders PageData.Shell instead of the real templates so the shell
// contract can be asserted at its choke point, renderStatus, without depending
// on the markup rewrite that Task 17 owns.
const shellProbe = `chrome={{.Shell.Chrome}} active={{.Shell.Active}} fab={{.Shell.Tabs.Fab.Label}} groups={{len .Shell.Groups}} badges={{len .Shell.Badges}} title={{.Shell.Title}}` +
	`{{range .Shell.Groups}}{{range .Items}} item={{.Label}}{{end}}{{end}}`

func (s *appTestServer) shellProbeHandler(t *testing.T) (http.Handler, *auth.Manager) {
	t.Helper()
	manager, err := auth.New(s.cfg, s.st)
	if err != nil {
		t.Fatal(err)
	}
	probe := &App{
		cfg:  s.cfg,
		st:   s.st,
		auth: manager,
		log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		tpl:  template.Must(template.New("shell_probe").Parse(shellProbe)),
	}
	return manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probe.renderStatus(w, r, http.StatusOK, "shell_probe", PageData{Title: "Payments"})
	})), manager
}

func renderAs(t *testing.T, handler http.Handler, manager *auth.Manager, user store.User, path string, headers map[string]string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if user.ID != 0 {
		login := httptest.NewRecorder()
		manager.Login(login, httptest.NewRequest(http.MethodGet, "/", nil), user)
		for _, cookie := range login.Result().Cookies() {
			request.AddCookie(cookie)
		}
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("shell probe status = %d, want 200", response.Code)
	}
	return response.Body.String()
}

func TestShellIsBuiltFromPermissionsAndSkippedForHTMXFragments(t *testing.T) {
	s := newAppTestServer(t)
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	entryID, err := s.st.CreateUser(s.ctx, "shell-entry@example.test", "Entry User", hash, "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.st.UserByID(s.ctx, entryID)
	if err != nil {
		t.Fatal(err)
	}
	handler, manager := s.shellProbeHandler(t)

	adminShell := renderAs(t, handler, manager, admin, "/payments", nil)
	for _, want := range []string{"chrome=app", "active=payments", "fab=Approve", "groups=8", "badges=3", "title=Payments", "item=Payments ledger", "item=Users", "item=Audit log"} {
		if !strings.Contains(adminShell, want) {
			t.Fatalf("admin shell missing %q: %s", want, adminShell)
		}
	}

	entryShell := renderAs(t, handler, manager, entry, "/payments", nil)
	for _, want := range []string{"chrome=app", "active=payments", "fab=Pay", "item=Payments ledger", "item=Variance grid"} {
		if !strings.Contains(entryShell, want) {
			t.Fatalf("data_entry shell missing %q: %s", want, entryShell)
		}
	}
	for _, forbidden := range []string{"item=Users", "item=Audit log", "item=Budgets", "item=Monthly plans"} {
		if strings.Contains(entryShell, forbidden) {
			t.Fatalf("data_entry shell leaks %q: %s", forbidden, entryShell)
		}
	}

	fragment := renderAs(t, handler, manager, admin, "/payments", map[string]string{"HX-Request": "true"})
	for _, want := range []string{"chrome=none", "groups=0", "badges=0"} {
		if !strings.Contains(fragment, want) {
			t.Fatalf("htmx fragment built a shell anyway (%q missing): %s", want, fragment)
		}
	}
	if strings.Contains(fragment, "item=") {
		t.Fatalf("htmx fragment carries nav items: %s", fragment)
	}

	anonymous := renderAs(t, handler, manager, store.User{}, "/payments", nil)
	if !strings.Contains(anonymous, "chrome=none") || strings.Contains(anonymous, "item=") {
		t.Fatalf("signed-out request built an app shell: %s", anonymous)
	}
}

func TestShellActiveKeyTracksTheRequestPath(t *testing.T) {
	for _, testCase := range []struct{ path, want string }{
		{path: "/", want: "dashboard"},
		{path: "/grid", want: "variance-grid"},
		{path: "/payments", want: "payments"},
		{path: "/payments/42/edit", want: "payments"},
		{path: "/reports/monthly", want: "reports"},
		{path: "/backups", want: "backups"},
		{path: "/login", want: ""},
	} {
		if got := activeNavKey(testCase.path); got != testCase.want {
			t.Fatalf("activeNavKey(%q) = %q, want %q", testCase.path, got, testCase.want)
		}
	}
}

func strconvFormat(id int64) string {
	return strconv.FormatInt(id, 10)
}
