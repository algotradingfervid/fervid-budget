package app

import (
	"bytes"
	"context"
	"errors"
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
	"reflect"
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
	for _, hidden := range []string{"Monthly plans", "href=\"/projects\"", "href=\"/users\"", "href=\"/audit\"", "href=\"/roles\""} {
		if strings.Contains(body, hidden) {
			t.Fatalf("data-entry navigation exposes %q", hidden)
		}
	}
	// …and still offers everything the Accounts role does allow. The labels are
	// the Phase 0 shell's, which this task does not change.
	for _, shown := range []string{"Payments ledger", "Variance grid", "Reports"} {
		if !strings.Contains(body, shown) {
			t.Fatalf("data entry navigation is missing %q", shown)
		}
	}
	for _, path := range []string{"/projects", "/heads", "/users", "/audit", "/budgets", "/months", "/roles"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusForbidden)
		_ = responseBody(t, resp)
	}
}

func TestMonthNormalizationAndReportRangeExport(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/grid?month=not-a-month", nil, ""))
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

// Retargeted for Phase 3: free-standing payment entry is retired, so the same
// retention behaviour is now exercised on the linked path — reserve a request,
// post a malformed amount, and get every typed value back. The behaviour is
// unchanged; only the linkage is added.
func TestPaymentErrorRetainsInputAndUsesHumanModes(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Retention")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{
		"request_id":   {strconvFormat(reqID)},
		"head_id":      {strconvFormat(headID)},
		"paid_on":      {"2026-04-11"},
		"amount":       {"not-money"},
		"vendor_payee": {"Aster Stores"},
		"payment_mode": {"bank_transfer"},
		"invoice_no":   {"INV-42"},
		"reference_no": {"REF-42"},
		"remarks":      {"retain this note"},
		"settlement":   {"settled"},
	}
	resp := s.postForm("/payments", form)
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	if !strings.Contains(body, "invalid amount") {
		t.Fatalf("malformed amount was not explained: %s", body)
	}
	// Nothing was written, and the reservation survives so the accountant can
	// simply correct the figure.
	var payments int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&payments); err != nil {
		t.Fatal(err)
	}
	if payments != 0 {
		t.Fatalf("a rejected settlement wrote %d payments", payments)
	}
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status after a rejected settlement = %q, want processing", got)
	}
	// Task 15 re-renders the confirmation sheet rather than an error page, so
	// every typed value comes back. The mode is spelled for a person in the
	// summary and kept verbatim in the field that reposts it — the old
	// `value="bank_transfer" selected` assertion belonged to the free-entry
	// <select>, which this phase retired.
	for _, expected := range []string{
		"not-money", "Aster Stores", "INV-42", "REF-42", "retain this note",
		`value="bank_transfer"`, ">Bank transfer<",
		`class="overlay"`, `class="sheet"`, "Nothing has been saved",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("the settlement sheet did not retain/display %q:\n%s", expected, body)
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

	// The admin's centre action is Approve, not Pay: approval:approve is the
	// first candidate in centreActions and Phase 2 built /approvals, so the
	// fall-through that used to land on Pay no longer happens.
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

// appScreens is every route the product renders today. The shell rewrite must
// leave all of them serving a complete page.
var appScreens = []string{
	"/", "/payments", "/payments/new", "/budgets", "/months",
	"/reports/monthly", "/projects", "/heads", "/users", "/audit", "/backups",
}

func TestEveryScreenRendersTheAppShell(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	for _, screen := range appScreens {
		resp := s.request(http.MethodGet, screen, nil, "")
		requireStatus(t, resp, http.StatusOK)
		body := responseBody(t, resp)
		for _, want := range []string{
			`<div class="appshell">`,
			`<aside class="sidebar">`,
			`<div class="side-brand">`,
			`<nav class="side-nav"`,
			`<div class="side-user">`,
			`<header class="m-topbar">`,
			`<main class="page">`,
			`<div class="page-inner">`,
			`<nav class="tabbar"`,
			`<div class="more-sheet"`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s is missing shell markup %q", screen, want)
			}
		}
		if !strings.Contains(body, `<div class="sgroup">Admin</div>`) {
			t.Fatalf("%s did not render the permission-driven nav groups", screen)
		}
	}
}

func TestShellMarksTheActiveNavItemAndSkipsRoutesTheUserCannotReach(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/payments", nil, ""))
	if !strings.Contains(body, `<a class="active" href="/payments" aria-current="page">`) {
		t.Fatalf("payments ledger is not marked active: %s", body)
	}
	if !strings.Contains(body, `<a class="soon" aria-disabled="true">`) {
		t.Fatal("coming-soon nav entries are missing their disabled treatment")
	}
	if !strings.Contains(body, `<span class="n">Soon</span>`) {
		t.Fatal("coming-soon nav entries are missing their pill")
	}
	// The screens later phases add are announced, not linked: their nav entries
	// are present so the structure is stable, but they render without an href
	// so nobody can click through to a route that does not exist yet.
	// Phase 5 built the last one, so unbuiltPrefixes is now empty and every
	// navigable screen resolves. The Soon mechanism itself is still exercised by
	// the four "Coming soon" product announcements above, which carry no href.
	// …and a screen that has been built is a link, which is the other half of
	// the same switch: Phase 2 removed /requests, /approvals and /configuration
	// from unbuiltPrefixes as it built each of them, and Phase 3 removed
	// /accounts-queue.
	for _, href := range []string{`href="/requests"`, `href="/approvals"`, `href="/configuration"`, `href="/accounts-queue"`, `href="/recoverables"`, `href="/admin/notifications"`} {
		if !strings.Contains(body, href) {
			t.Fatalf("%s is built but the nav still announces it as Soon", href)
		}
	}

	hash, err := auth.HashPassword("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.CreateUser(s.ctx, "shell-nav@example.test", "Nav Entry", hash, "data_entry", true); err != nil {
		t.Fatal(err)
	}
	entry := newAppTestClient(t, s)
	entry.login("shell-nav@example.test", "EntryPassword123")
	body = responseBody(t, entry.request(http.MethodGet, "/", nil, ""))
	if !strings.Contains(body, `<aside class="sidebar">`) {
		t.Fatal("data-entry user lost the app shell")
	}
	for _, forbidden := range []string{`href="/users"`, `href="/audit"`, `href="/backups"`, `<div class="sgroup">Admin</div>`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("data-entry shell exposes %q", forbidden)
		}
	}
}

func TestChromeNoneRendersWithoutTheAppShell(t *testing.T) {
	s := newAppTestServer(t)
	login := responseBody(t, s.request(http.MethodGet, "/login", nil, ""))
	if strings.Contains(login, "<aside") || strings.Contains(login, "tabbar") {
		t.Fatalf("login page rendered app chrome: %s", login)
	}

	s.login(s.cfg.AdminEmail, testAdminPassword)
	req, err := http.NewRequest(http.MethodGet, s.server.URL+"/payments", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, http.StatusOK)
	fragment := responseBody(t, resp)
	for _, forbidden := range []string{"<aside", `class="appshell"`, `class="tabbar"`, `class="m-topbar"`, `class="more-sheet"`} {
		if strings.Contains(fragment, forbidden) {
			t.Fatalf("htmx fragment rendered %q", forbidden)
		}
	}
}

func TestTemplatesNeverCompareRoleNames(t *testing.T) {
	source, err := os.ReadFile("templates.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{`eq .User.Role`, `eq $.User.Role`, `.User.Role "`} {
		if count := strings.Count(string(source), banned); count != 0 {
			t.Fatalf("templates.go still compares role names: %d occurrences of %q", count, banned)
		}
	}
}

// Decision D5: the design system standardises on .pill. .badge survives in the
// stylesheet as a safety net for one release, but no template may emit it.
func TestNoTemplateUsesLegacyBadgeClass(t *testing.T) {
	source, err := os.ReadFile("templates.go")
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(source), `class="badge`); count != 0 {
		t.Fatalf(`templates.go still emits class="badge" %d times; the design system uses .pill`, count)
	}
}

// /budgets, /projects and /heads edit rows in place: every cell holds a bare
// <input form="head-12"> or <select> whose column header labels it on screen
// but not programmatically, so a screen reader announces "edit text, blank"
// once per row with nothing to tell the rows apart. Each control therefore
// names its field *and* its row. Controls that already sit inside a visible
// <label> — the Active checkbox, every field of the create form at the top of
// each screen — are deliberately left alone: an aria-label there would replace
// the visible text rather than supply the missing one.
func TestInlineRowEditorsNameTheirFieldAndRow(t *testing.T) {
	s := newAppTestServer(t)
	s.seedHead("Alpha")
	s.login(s.cfg.AdminEmail, testAdminPassword)

	for _, tc := range []struct {
		path string
		want []string
	}{
		{"/heads", []string{
			`aria-label="Project for Rent Alpha"`,
			`aria-label="Head name for Rent Alpha"`,
			`aria-label="Due day for Rent Alpha"`,
			`aria-label="Sort order for Rent Alpha"`,
		}},
		// The row control mirrors its column header ("Name"), not the create
		// form's "Project name" label above it: getByLabel matches on substring,
		// so reusing that phrase would make every existing "Project name"
		// selector ambiguous the moment a project exists.
		{"/projects", []string{
			`aria-label="Name for Operations Alpha"`,
			`aria-label="Sort order for Operations Alpha"`,
		}},
		{"/budgets", []string{
			`aria-label="Budget for Operations Alpha / Rent Alpha"`,
		}},
		// The payments table is the one region on any screen that scrolls
		// sideways without a focusable child, so it needs a tab stop of its own
		// and a name saying what scrolls.
		{"/payments", []string{
			`class="table-wrap payments-table" tabindex="0" role="region" aria-label="Payments table, scrollable"`,
		}},
	} {
		body := responseBody(t, s.request(http.MethodGet, tc.path, nil, ""))
		for _, want := range tc.want {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s does not render %s", tc.path, want)
			}
		}
	}

	for _, path := range []string{"/heads", "/projects"} {
		if body := responseBody(t, s.request(http.MethodGet, path, nil, "")); strings.Contains(body, `aria-label="Active`) {
			t.Errorf("GET %s overrides the visible Active label with an aria-label", path)
		}
	}
}

// The page body and the nav must gate on one permission set resolved once per
// request, not on two independently built policies. If the body ever grows its
// own policy again, a control can appear on a screen the nav hides.
func TestPageBodyGatesOnTheRequestPermissionSet(t *testing.T) {
	source, err := os.ReadFile("templates.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"internal/auth", "internal/config"} {
		if strings.Contains(string(source), banned) {
			t.Fatalf("templates.go imports %q; permissions come from PageData.Perms, resolved in renderStatus", banned)
		}
	}

	s := newAppTestServer(t)
	hash, err := auth.HashPassword("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.CreateUser(s.ctx, "entry@example.test", "Entry User", hash, "data_entry", true); err != nil {
		t.Fatal(err)
	}

	// Month Close renders from the variance grid, gated on month_lock:write —
	// the same permission /months/{m}/lock is guarded by. It needs no rows, so
	// it isolates the gate from the data. (Home is the dashboard; the grid
	// moved to /grid when the dashboard shipped.)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	admin := responseBody(t, s.request(http.MethodGet, "/grid", nil, ""))
	if !strings.Contains(admin, "Month Close") {
		t.Fatal("admin cannot see Month Close despite holding month_lock:write")
	}

	entry := newAppTestClient(t, s)
	entry.login("entry@example.test", "EntryPassword123")
	restricted := responseBody(t, entry.request(http.MethodGet, "/grid", nil, ""))
	if strings.Contains(restricted, "Month Close") {
		t.Fatal("data-entry user sees Month Close despite lacking month_lock:write")
	}
}

// newAppTestClient gives a second, independently authenticated client against
// the same server so one test can compare what two roles are shown.
func newAppTestClient(t *testing.T, s *appTestServer) *appTestServer {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	clone := *s
	clone.client = &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &clone
}

func strconvFormat(id int64) string {
	return strconv.FormatInt(id, 10)
}

func TestRolesAdminScreenRendersApprovedMatrix(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/roles", nil, ""))

	// The approved chrome: segmented switcher, role card, both matrices, the
	// scope pill group and the sticky action bar.
	for _, want := range []string{
		`class="segmented"`, `class="card"`, `class="card-head"`, `class="pill good"`,
		`class="form-grid"`, `class="field span-4 m-half"`,
		`class="table-wrap d-only"`, `class="perm-table"`,
		`class="perm-acc m-only"`, `class="pa-head"`, `class="chev"`, `class="pa-body"`,
		`class="perm-scope"`, `class="action-bar"`, `class="ab-note d-only"`,
		`class="btn primary"`, `class="btn outline"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/roles is missing the approved markup %q", want)
		}
	}
	// Every seeded role is a segment; custom roles are badged.
	for _, want := range []string{"Requester", "Manager", "Accounts", "Admin"} {
		if !strings.Contains(body, want) {
			t.Fatalf("/roles page missing role %q", want)
		}
	}
	// All nine rows, by their design labels.
	for _, want := range []string{
		"Payment requests", "Payments", "Reservations", "Recoverables", "Vendors",
		"Vendor bank details", "Budgets &amp; variance grid", "Reports", "Administration",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/roles matrix missing row %q", want)
		}
	}
	// Cells, the Advanced disclosure, and an em dash where a row has no
	// canonical action for a column (Reservations has no View).
	for _, want := range []string{`name="cell"`, `name="perm"`, "Advanced", "—"} {
		if !strings.Contains(body, want) {
			t.Fatalf("/roles matrix missing %q", want)
		}
	}
	// Only one of the two renderings may post: the mobile inputs ship disabled.
	if !strings.Contains(body, `name="cell" disabled`) {
		t.Fatal("mobile accordion inputs must be rendered disabled so only one matrix submits")
	}
	// D5: .badge is retired.
	if strings.Contains(body, `class="badge`) {
		t.Fatal("/roles still renders the retired .badge class")
	}
}

func TestRolesAdminCreateEditCopyDelete(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	// Create a custom role.
	resp := s.postForm("/roles/new", url.Values{"name": {"Reviewer"}, "description": {"read only"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var reviewerID int64
	for _, r := range roles {
		if r.Name == "Reviewer" {
			reviewerID = r.ID
		}
	}
	if reviewerID == 0 {
		t.Fatal("Reviewer role was not created")
	}

	// Save through the matrix: one cell expands to every canonical action behind
	// it, one Advanced checkbox adds a single refinement, and the name and
	// description in the card are persisted alongside.
	save := url.Values{
		"role_id":       {strconvFormat(reviewerID)},
		"name":          {"Reviewer"},
		"description":   {"Reviews and approves"},
		"cell":          {"requests:approve", "reports:view"},
		"perm":          {"request:view"},
		"scope_request": {"all"},
	}
	resp = s.postForm("/roles", save)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	grants, scopes, err := s.st.RolePermissions(s.ctx, reviewerID)
	if err != nil {
		t.Fatal(err)
	}
	held := map[string]bool{}
	for _, g := range grants {
		held[g.Resource+":"+g.Action] = true
	}
	for _, want := range []string{
		"approval:approve", "approval:reject", "approval:return", "approval:reassign",
		"approval:accept_partial", "report:view", "request:view",
	} {
		if !held[want] {
			t.Fatalf("cell expansion did not grant %s (got %v)", want, grants)
		}
	}
	if len(grants) != 7 {
		t.Fatalf("saved %d grants, want exactly the 7 the cells and refinement cover: %v", len(grants), grants)
	}
	if len(scopes) != 1 || scopes[0].Resource != "request" || scopes[0].Scope != "all" {
		t.Fatalf("saved scopes = %v, want request=all", scopes)
	}
	role, err := s.st.Role(s.ctx, reviewerID)
	if err != nil || role.Description != "Reviews and approves" {
		t.Fatalf("role card did not save: %+v, %v", role, err)
	}

	// Clearing a cell revokes everything behind it: the same POST without
	// requests:approve leaves only what is still submitted.
	save.Del("cell")
	save.Add("cell", "reports:view")
	resp = s.postForm("/roles", save)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	grants, _, err = s.st.RolePermissions(s.ctx, reviewerID)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		if g.Resource == "approval" {
			t.Fatalf("clearing the Approve cell left %s:%s behind", g.Resource, g.Action)
		}
	}
	if len(grants) != 2 {
		t.Fatalf("after clearing the cell: %v, want report:view + request:view", grants)
	}

	// A hand-crafted POST cannot invent a grant outside the vocabulary.
	resp = s.postForm("/roles", url.Values{
		"role_id": {strconvFormat(reviewerID)},
		"name":    {"Reviewer"},
		"perm":    {"payment:read"},
	})
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)

	// Copy the role.
	resp = s.postForm("/roles/"+strconvFormat(reviewerID)+"/copy", url.Values{"name": {"Reviewer Copy"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	// Delete the custom role; deleting a system role is rejected.
	resp = s.postForm("/roles/"+strconvFormat(reviewerID)+"/delete", nil)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if _, err := s.st.Role(s.ctx, reviewerID); !errorsIsNotFound(err) {
		t.Fatalf("Reviewer still present after delete: %v", err)
	}
	var adminID int64
	for _, r := range roles {
		if r.Name == "Admin" {
			adminID = r.ID
		}
	}
	resp = s.postForm("/roles/"+strconvFormat(adminID)+"/delete", nil)
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
}

func errorsIsNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }

func TestUsersScreenAssignsMultipleRoles(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	adminID := admin.ID

	hash, err := auth.HashPassword("MemberPassword123")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := s.st.CreateUser(s.ctx, "member@example.test", "Member", hash, "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var requesterID, managerID int64
	for _, r := range roles {
		switch r.Name {
		case "Requester":
			requesterID = r.ID
		case "Manager":
			managerID = r.ID
		}
	}

	// The users page is the approved t-cards table with a role chip per role and
	// an editing sheet holding the role checklines.
	body := responseBody(t, s.request(http.MethodGet, "/users", nil, ""))
	for _, want := range []string{
		`class="t-cards"`, `class="t-lead" data-label="Name"`, `data-label="Email"`,
		`data-label="Roles"`, `data-label="Default approver"`, `data-label="Status"`,
		`class="pill neutral no-dot"`, `class="overlay"`, `class="sheet"`,
		`class="sh-head"`, `class="sh-body`, `class="sh-foot"`, `class="checkline"`,
		"role_ids", "default_approver_id", "Requester", "Manager",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("users page missing the approved markup %q", want)
		}
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("/users still renders the retired .badge class")
	}

	// Assign Requester + Manager to the member and give them a default approver.
	form := url.Values{
		"id":                  {strconvFormat(uid)},
		"name":                {"Member"},
		"email":               {"member@example.test"},
		"role":                {"data_entry"},
		"active":              {"on"},
		"role_ids":            {strconvFormat(requesterID), strconvFormat(managerID)},
		"default_approver_id": {strconvFormat(adminID)},
	}
	resp := s.postForm("/users", form)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	got, err := s.st.UserRoles(s.ctx, uid)
	if err != nil || len(got) != 2 {
		t.Fatalf("UserRoles after save = %+v, %v; want 2 roles", got, err)
	}
	// Union access: the member can now both create requests and approve.
	ps, _ := s.st.EffectivePermissions(s.ctx, uid)
	if !ps.Can("request", "create") || !ps.Can("approval", "approve") {
		t.Fatalf("union of Requester+Manager not effective: %+v", ps)
	}
	saved, err := s.st.UserByID(s.ctx, uid)
	if err != nil || saved.DefaultApproverID != adminID {
		t.Fatalf("default approver = %d, want %d (%v)", saved.DefaultApproverID, adminID, err)
	}

	// The member's own sheet must not offer the member as their own approver,
	// and a hand-crafted POST that tries it is rejected.
	if strings.Contains(body, `value="`+strconvFormat(uid)+`" data-approver-for="`+strconvFormat(uid)+`"`) {
		t.Fatal("the approver list offers a user as their own approver")
	}
	form.Set("default_approver_id", strconvFormat(uid))
	resp = s.postForm("/users", form)
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)
	saved, _ = s.st.UserByID(s.ctx, uid)
	if saved.DefaultApproverID != adminID {
		t.Fatalf("rejected self-approval still changed the stored value to %d", saved.DefaultApproverID)
	}
}

// R6: permissions govern the data server-side. A least-privilege session is
// blocked by URL, not by menu-hiding — the menu is only the second assertion.
func TestRequesterOnlySessionForbiddenFromAdminRoutesByURL(t *testing.T) {
	s := newAppTestServer(t)

	// Create a user and give them ONLY the Requester role (override the default
	// Accounts assignment) so we test a least-privilege session.
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("RequesterPass123")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := s.st.CreateUser(s.ctx, "requester@example.test", "Requester User", hash, "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var requesterID int64
	for _, r := range roles {
		if r.Name == "Requester" {
			requesterID = r.ID
		}
	}
	if err := s.st.SetUserRoles(s.ctx, admin, uid, []int64{requesterID}); err != nil {
		t.Fatal(err)
	}

	s.login("requester@example.test", "RequesterPass123")

	// GET admin/screen routes are blocked by URL (server-side, not menu-hiding).
	for _, path := range []string{"/roles", "/users", "/audit", "/projects", "/heads", "/budgets",
		"/months", "/payments", "/reports/monthly", "/configuration", "/approvals"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusForbidden)
		_ = responseBody(t, resp)
	}

	// An admin POST is blocked even with a valid CSRF token.
	resp := s.postForm("/roles/new", url.Values{"name": {"Sneaky"}})
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
	resp = s.postForm("/users", url.Values{"id": {"1"}, "name": {"x"}, "email": {s.cfg.AdminEmail}, "role": {"admin"}, "active": {"on"}})
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)

	// The nav for a Requester exposes none of the admin destinations.
	body := responseBody(t, s.request(http.MethodGet, "/", nil, ""))
	for _, hidden := range []string{"href=\"/roles\"", "href=\"/users\"", "href=\"/audit\"", "href=\"/projects\"", "href=\"/budgets\"", "Monthly plans"} {
		if strings.Contains(body, hidden) {
			t.Fatalf("Requester nav exposes %q", hidden)
		}
	}
}

// seedUserWithGrants creates a signed-in-able user holding exactly the grants
// named — the seeded system roles cannot express "vendor:view but not
// vendor_bank:view", which is the distinction Phase 1V exists to enforce.
func (s *appTestServer) seedUserWithGrants(email, password, roleName string, grants []store.Grant) {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.t.Fatal(err)
	}
	uid, err := s.st.CreateUser(s.ctx, email, roleName+" User", hash, "data_entry", true)
	if err != nil {
		s.t.Fatal(err)
	}
	roleID, err := s.st.CreateRole(s.ctx, admin, roleName, "")
	if err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.UpdateRolePermissions(s.ctx, admin, roleID, grants, nil); err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.SetUserRoles(s.ctx, admin, uid, []int64{roleID}); err != nil {
		s.t.Fatal(err)
	}
}

func (s *appTestServer) seedVendor(in store.VendorInput) int64 {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
	id, err := s.st.CreateVendor(s.ctx, admin, in)
	if err != nil {
		s.t.Fatalf("CreateVendor(%s): %v", in.Name, err)
	}
	return id
}

func TestVendorsListRendersTheApprovedScreenAndIsPermissionGated(t *testing.T) {
	s := newAppTestServer(t)
	s.seedVendor(store.VendorInput{
		Name: "Sundaram Electricals Pvt Ltd", DisplayName: "Sundaram Elec",
		VendorType: "company", Status: "active", Categories: "Materials, Switchgear",
		GSTIN: "29AABCS1429B1ZQ", City: "Bengaluru",
	})
	s.seedVendor(store.VendorInput{
		Name: "Nova Print Works", VendorType: "company", Status: "active",
		Categories: "Printing", City: "Chennai",
	})
	s.login(s.cfg.AdminEmail, testAdminPassword)

	resp := s.request(http.MethodGet, "/vendors", nil, "")
	requireStatus(t, resp, http.StatusOK)
	body := responseBody(t, resp)

	for _, want := range []string{
		`class="page-banner"`, `class="pb-actions"`,
		`class="banner warn"`, `class="b-ico"`, `class="b-actions"`,
		`class="toolbar"`, `class="m-filters"`, `class="m-search"`, `class="btn filter-btn"`,
		`class="table-wrap"`, `class="t-cards"`,
		`class="t-lead" data-label="Vendor"`, `class="t-sub"`,
		`data-label="Type"`, `data-label="GSTIN"`, `data-label="City"`,
		`data-label="Paid this year"`, `data-label="Open requests"`, `data-label="Status"`,
		`class="pill good"`, `<tfoot>`,
		"Sundaram Electricals Pvt Ltd", "Nova Print Works",
		// Categories are stored comma-separated and read back as a · chain.
		"Materials · Switchgear",
		// A vendor with no GSTIN says so rather than rendering an empty cell.
		`class="pill warn no-dot"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/vendors is missing the approved markup %q", want)
		}
	}
	// One vendor has no GSTIN, so the warning banner counts exactly one.
	if !strings.Contains(body, "1 vendor has no GSTIN") {
		t.Fatalf("/vendors did not report the missing-GSTIN gap: %s", body)
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("/vendors renders the retired .badge class")
	}

	// The nav link that has pointed at a 404 since Phase 1 now resolves.
	if !strings.Contains(body, `<a class="active" href="/vendors" aria-current="page">`) {
		t.Fatal("/vendors does not mark its own nav item active")
	}

	// Filters narrow the list rather than being decorative.
	filtered := responseBody(t, s.request(http.MethodGet, "/vendors?q=chennai", nil, ""))
	if strings.Contains(filtered, "Sundaram Electricals Pvt Ltd") || !strings.Contains(filtered, "Nova Print Works") {
		t.Fatal("/vendors?q= did not filter the list")
	}

	// R6: the route is gated server-side, not by hiding the menu entry.
	s.seedUserWithGrants("no-vendor@example.test", "NoVendorPass123", "No Vendors", []store.Grant{{Resource: "request", Action: "view"}})
	blocked := newAppTestClient(t, s)
	blocked.login("no-vendor@example.test", "NoVendorPass123")
	denied := blocked.request(http.MethodGet, "/vendors", nil, "")
	requireStatus(t, denied, http.StatusForbidden)
	if strings.Contains(responseBody(t, denied), "Sundaram Electricals Pvt Ltd") {
		t.Fatal("a 403 response still leaked vendor data")
	}
}

// allVendorPerms is the fixture's all-seeing reader: assertions about what was
// STORED must not be filtered by the same gate they are checking.
func allVendorPerms() store.PermissionSet {
	return store.NewPermissionSet(store.AllGrants(), nil)
}

const testVendorIFSC = "HDFC0000521"
const testVendorAccount = "50200041294471"

func (s *appTestServer) seedVendorWithBank(name string) int64 {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
	id, err := s.st.CreateVendor(s.ctx, admin, store.VendorInput{
		Name: name, DisplayName: "Sundaram Elec", VendorType: "company", Status: "active",
		Categories: "Materials, Switchgear", GSTIN: "29AABCS1429B1ZQ", PAN: "AABCS1429B",
		ContactPerson: "R. Subramanian", City: "Bengaluru",
		Bank: &store.VendorBank{
			AccountName: "Sundaram Electricals Private Limited", AccountNumber: testVendorAccount,
			IFSC: testVendorIFSC, BankName: "HDFC Bank", Branch: "Peenya",
			DefaultPaymentMode: "bank_transfer", PaymentTermsDays: 30,
		},
	})
	if err != nil {
		s.t.Fatalf("CreateVendor: %v", err)
	}
	return id
}

// The requirement Phase 1V exists for: a caller without vendor_bank:view must
// not receive the bank block, and the proof is that the stored IFSC appears
// nowhere in the response — not that a template hid it.
func TestVendorDetailGatesBankDetailsOnTheDataNotTheMarkup(t *testing.T) {
	s := newAppTestServer(t)
	id := s.seedVendorWithBank("Sundaram Electricals Pvt Ltd")
	path := "/vendors/" + strconvFormat(id)

	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.request(http.MethodGet, path, nil, "")
	requireStatus(t, resp, http.StatusOK)
	body := responseBody(t, resp)
	for _, want := range []string{
		`class="segmented"`, `<fieldset`, `<legend>Identity</legend>`,
		`<legend>Statutory</legend>`, `<legend>Contact</legend>`, `<legend>Notes</legend>`,
		`class="form-grid"`, `class="field span-6"`, `class="field span-3 m-half"`,
		`class="action-bar"`, `class="ab-note d-only"`, `class="row-end"`,
		"bank_ifsc", testVendorIFSC, testVendorAccount,
		"Save vendor",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("vendor detail is missing %q for an admin", want)
		}
	}

	// vendor:view only — no bank permission at all.
	s.seedUserWithGrants("vendor-viewer@example.test", "ViewerPass1234", "Vendor Viewer",
		[]store.Grant{{Resource: "vendor", Action: "view"}})
	viewer := newAppTestClient(t, s)
	viewer.login("vendor-viewer@example.test", "ViewerPass1234")
	viewerBody := responseBody(t, viewer.request(http.MethodGet, path, nil, ""))
	for _, forbidden := range []string{"bank_ifsc", testVendorIFSC, testVendorAccount,
		"bank_account_number", "HDFC Bank"} {
		if strings.Contains(viewerBody, forbidden) {
			t.Fatalf("a caller without vendor_bank:view received %q", forbidden)
		}
	}
	for _, want := range []string{`class="banner locked"`, "You do not have permission to see bank details"} {
		if !strings.Contains(viewerBody, want) {
			t.Fatalf("vendor detail is missing %q for a caller without vendor_bank:view", want)
		}
	}
	// Without vendor:edit the action bar offers a way back, not a way to save.
	if strings.Contains(viewerBody, "Save vendor") {
		t.Fatal("a caller without vendor:edit was offered Save vendor")
	}
	if !strings.Contains(viewerBody, "Back to vendors") {
		t.Fatal("a read-only caller was not offered a way back to the list")
	}
	// The rest of the record is still theirs to read.
	if !strings.Contains(viewerBody, "29AABCS1429B1ZQ") {
		t.Fatal("the non-bank part of the record was withheld too")
	}

	// vendor_bank:view without :edit — the values are readable, not writable.
	s.seedUserWithGrants("bank-reader@example.test", "ReaderPass1234", "Bank Reader",
		[]store.Grant{{Resource: "vendor", Action: "view"}, {Resource: "vendor_bank", Action: "view"}})
	reader := newAppTestClient(t, s)
	reader.login("bank-reader@example.test", "ReaderPass1234")
	readerBody := responseBody(t, reader.request(http.MethodGet, path, nil, ""))
	if !strings.Contains(readerBody, testVendorIFSC) {
		t.Fatal("a caller holding vendor_bank:view could not see the IFSC")
	}
	if !strings.Contains(readerBody, `name="bank_ifsc" value="`+testVendorIFSC+`" disabled`) {
		t.Fatalf("bank fields are not read-only for a caller without vendor_bank:edit: %s", readerBody)
	}
}

func TestVendorCreateAndUpdateThroughTheScreens(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	blank := responseBody(t, s.request(http.MethodGet, "/vendors/new", nil, ""))
	for _, want := range []string{`class="form-grid"`, `name="name"`, `name="vendor_type"`, "Save vendor"} {
		if !strings.Contains(blank, want) {
			t.Fatalf("/vendors/new is missing %q", want)
		}
	}

	form := url.Values{
		"name": {"Meridian Facility Services"}, "display_name": {"Meridian"},
		"vendor_type": {"company"}, "status": {"active"}, "categories": {"Services, Housekeeping"},
		"gstin": {"29AACCM7781L1ZR"}, "city": {"Bengaluru"}, "contact_person": {"A. Rao"},
		"bank_account_name": {"Meridian Facility Services"}, "bank_ifsc": {"ICIC0000042"},
		"payment_terms_days": {"30"},
	}
	resp := s.postForm("/vendors", form)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	all, err := s.st.ListVendors(s.ctx, store.VendorListOptions{Status: "all"}, allVendorPerms())
	if err != nil || len(all) != 1 {
		t.Fatalf("ListVendors after create = %d rows, %v; want 1", len(all), err)
	}
	created := all[0]
	if created.Name != "Meridian Facility Services" || created.Bank == nil || created.Bank.IFSC != "ICIC0000042" {
		t.Fatalf("create did not store the submitted record: %+v", created)
	}

	// A blank name is rejected and the submitted values survive the round trip.
	bad := url.Values{"name": {"  "}, "city": {"Mysuru"}}
	resp = s.postForm("/vendors", bad)
	requireStatus(t, resp, http.StatusBadRequest)
	if body := responseBody(t, resp); !strings.Contains(body, "vendor name is required") || !strings.Contains(body, "Mysuru") {
		t.Fatalf("a rejected create lost its error or its input: %s", body)
	}

	// Update the contact person.
	form.Set("contact_person", "B. Rao")
	resp = s.postForm("/vendors/"+strconvFormat(created.ID), form)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	updated, _ := s.st.Vendor(s.ctx, created.ID, allVendorPerms())
	if updated.ContactPerson != "B. Rao" {
		t.Fatalf("update did not apply: %+v", updated)
	}
}

// A hand-crafted POST is the real test of a write gate: the screen never sends
// these fields, so only a deliberate request carries them.
func TestVendorUpdateIgnoresBankFieldsFromACallerWithoutBankEdit(t *testing.T) {
	s := newAppTestServer(t)
	id := s.seedVendorWithBank("Sundaram Electricals Pvt Ltd")
	s.seedUserWithGrants("vendor-editor@example.test", "EditorPass1234", "Vendor Editor",
		[]store.Grant{{Resource: "vendor", Action: "view"}, {Resource: "vendor", Action: "edit"}})

	editor := newAppTestClient(t, s)
	editor.login("vendor-editor@example.test", "EditorPass1234")
	// Prime the CSRF cookie by loading the screen first.
	_ = responseBody(t, editor.request(http.MethodGet, "/vendors/"+strconvFormat(id), nil, ""))

	resp := editor.postForm("/vendors/"+strconvFormat(id), url.Values{
		"name": {"Sundaram Electricals Pvt Ltd"}, "vendor_type": {"company"}, "status": {"active"},
		"contact_person": {"Legitimate Edit"},
		"bank_ifsc":      {"EVIL0000001"}, "bank_account_number": {"0000000000"},
		"bank_account_name": {"Attacker"},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	after, err := s.st.Vendor(s.ctx, id, allVendorPerms())
	if err != nil {
		t.Fatal(err)
	}
	if after.ContactPerson != "Legitimate Edit" {
		t.Fatalf("the permitted part of the edit was dropped: %+v", after)
	}
	if after.Bank == nil || after.Bank.IFSC != testVendorIFSC || after.Bank.AccountNumber != testVendorAccount {
		t.Fatalf("a caller without vendor_bank:edit rewrote the bank block: %+v", after.Bank)
	}
}

func (s *appTestServer) htmxGet(path string) *http.Response {
	s.t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.server.URL+path, nil)
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	return resp
}

func TestVendorSearchFragmentFeedsTheCombobox(t *testing.T) {
	s := newAppTestServer(t)
	s.seedVendor(store.VendorInput{Name: "Sundaram Electricals Pvt Ltd", VendorType: "company",
		Status: "active", GSTIN: "29AABCS1429B1ZQ", City: "Bengaluru"})
	s.seedVendor(store.VendorInput{Name: "Sundaram Switchgear LLP", VendorType: "company",
		Status: "active", City: "Hosur"})
	s.seedVendor(store.VendorInput{Name: "Meridian Facility Services", VendorType: "company",
		Status: "active", City: "Bengaluru"})
	s.login(s.cfg.AdminEmail, testAdminPassword)

	resp := s.htmxGet("/vendors/search?q=sund")
	requireStatus(t, resp, http.StatusOK)
	body := responseBody(t, resp)
	for _, want := range []string{
		`class="combo-list"`, `class="co"`, `class="co-main"`,
		"Sundaram Electricals Pvt Ltd", "Sundaram Switchgear LLP",
		"29AABCS1429B1ZQ", "Bengaluru",
		// vendor:create is held by the admin, so the add row is offered.
		`class="co co-add"`, "Add a new vendor",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the combobox fragment is missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Meridian Facility Services") {
		t.Fatal("the combobox fragment ignored the query")
	}
	// A fragment is a fragment: no shell, no document.
	for _, forbidden := range []string{"<aside", "<!doctype", "<html", `class="appshell"`, `class="tabbar"`, `class="m-topbar"`} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("the combobox fragment rendered %q", forbidden)
		}
	}
	// It is a picker, not a record: bank details are never in it.
	if strings.Contains(body, testVendorIFSC) {
		t.Fatal("the combobox fragment carried a bank detail")
	}

	// Without vendor:create there is nothing to add with, so no add row.
	s.seedUserWithGrants("combo-viewer@example.test", "ComboPass1234", "Combo Viewer",
		[]store.Grant{{Resource: "vendor", Action: "view"}})
	viewer := newAppTestClient(t, s)
	viewer.login("combo-viewer@example.test", "ComboPass1234")
	viewerBody := responseBody(t, viewer.htmxGet("/vendors/search?q=sund"))
	if !strings.Contains(viewerBody, "Sundaram Electricals Pvt Ltd") {
		t.Fatal("a caller with vendor:view could not search vendors")
	}
	if strings.Contains(viewerBody, "co-add") {
		t.Fatal("a caller without vendor:create was offered the add-a-vendor row")
	}

	// And no vendor permission at all is a 403, not an empty list.
	s.seedUserWithGrants("combo-blocked@example.test", "BlockedPass1234", "Combo Blocked",
		[]store.Grant{{Resource: "request", Action: "view"}})
	blocked := newAppTestClient(t, s)
	blocked.login("combo-blocked@example.test", "BlockedPass1234")
	denied := blocked.htmxGet("/vendors/search?q=sund")
	requireStatus(t, denied, http.StatusForbidden)
	if strings.Contains(responseBody(t, denied), "Sundaram") {
		t.Fatal("a forbidden search still returned vendor names")
	}

	// An empty query renders the empty state rather than the whole master.
	empty := responseBody(t, s.htmxGet("/vendors/search?q="))
	if strings.Contains(empty, "Sundaram") {
		t.Fatalf("a blank query listed vendors: %s", empty)
	}
}

func TestNoRefundRoute(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	// X3: money leaves only through reserve → record-payment, and a recorded
	// payment is immutable (S12). There is deliberately no refund / return-of-money
	// endpoint, so both plausible refund routes must be unrouted.
	//
	// 404 *or* 405 is the correct expectation, exactly as in the sibling guard
	// TestNoBulkApproveOrCopyEndpointExists (A6/D5): routes() registers a
	// catch-all "GET /" for the not-found page, so an unrouted POST matches that
	// pattern on path but not on method, and net/http's ServeMux answers 405
	// rather than 404. Both mean "no such endpoint"; if a refund route were ever
	// registered the status would become a 2xx/3xx/4xx from the handler and this
	// bites. Do not narrow this to 404 — the router cannot produce it here.
	for _, path := range []string{"/payments/1/refund", "/requests/1/refund"} {
		resp := s.postForm(path, url.Values{})
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s = %d, want 404/405 (no refund route may exist, X3)", path, resp.StatusCode)
		}
		_ = responseBody(t, resp)
	}
	// And no refund method may exist on the store either. s.st is declared as
	// *store.Store, so reflect.TypeOf sees the pointer type and enumerates the
	// pointer-receiver methods — which is every method the store has.
	st := reflect.TypeOf(s.st)
	for i := 0; i < st.NumMethod(); i++ {
		if name := st.Method(i).Name; strings.Contains(strings.ToLower(name), "refund") {
			t.Fatalf("unexpected refund store method %q (X3): payments are immutable; refunds are out of scope", name)
		}
	}
}
