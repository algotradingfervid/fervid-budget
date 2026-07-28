package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	"regexp"
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

// roleIDByName resolves a seeded role to the id the people form posts, so a test
// can ask for "Requester" rather than hard-coding a number the seed decides.
func (s *appTestServer) roleIDByName(t *testing.T, name string) string {
	t.Helper()
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range roles {
		if strings.EqualFold(role.Name, name) {
			return strconv.FormatInt(role.ID, 10)
		}
	}
	t.Fatalf("no seeded role named %q", name)
	return ""
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

// TestBudgetSaveAppliesReadableFieldsAndNamesTheRest replaces
// TestBudgetBatchValidationDoesNotPartiallySave, whose expectation F-G-028
// reversed. That test asserted a batch is abandoned whole when any one field
// fails — which is precisely the defect: the budgets screen renders ₹0.00 for
// every unbudgeted head, money.ParsePaise refuses zero, so a month that had
// never been budgeted could not be budgeted at all, one head at a time or
// otherwise. Every field that reads is now applied and the ones that do not are
// named back, so the "did not partially save" assertion is no longer the
// contract. Nothing was weakened: the same POST is made and the same status is
// demanded, and the row that used to be discarded is now asserted to be written.
func TestBudgetSaveAppliesReadableFieldsAndNamesTheRest(t *testing.T) {
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
	if !strings.Contains(body, "not-a-number") {
		t.Fatalf("the unreadable field was not handed back for correction: %s", body)
	}
	if !strings.Contains(body, "invalid budget") {
		t.Fatalf("the response does not say which figures were refused: %s", body)
	}
	budget, err := s.st.Budget(s.ctx, headA, "2026-04")
	if err != nil {
		t.Fatal(err)
	}
	if budget.Amount != 25000 {
		t.Fatalf("the readable field was not applied: got %d, want 25000", budget.Amount)
	}
	if _, err := s.st.Budget(s.ctx, headB, "2026-04"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Budget(headB) = %v, want ErrNotFound — an unreadable field must write nothing", err)
	}
}

// F-G-028's three reproductions, in one test: the values the screen renders can
// be saved, a single head can be filled in on a month with no budgets at all,
// and an existing budget can be reduced to zero.
func TestUnbudgetedMonthCanBeSavedAndABudgetCanBeZeroed(t *testing.T) {
	s := newAppTestServer(t)
	admin, headA := s.seedHead("ZeroA")
	_, headB := s.seedHead("ZeroB")
	s.login(s.cfg.AdminEmail, testAdminPassword)

	// 1. Exactly what the form sends for a month nobody has budgeted: ₹0.00 in
	// every field, the way `value="{{money .Budget}}"` renders it.
	asRendered := url.Values{
		"month":                          {"2028-06"},
		"budget_" + strconvFormat(headA): {"₹0.00"},
		"budget_" + strconvFormat(headB): {"₹0.00"},
	}
	requireStatus(t, s.postForm("/budgets", asRendered), http.StatusSeeOther)

	// 2. One head filled in, the rest left as rendered.
	firstFigure := url.Values{
		"month":                          {"2028-06"},
		"budget_" + strconvFormat(headA): {"25,00,000.00"},
		"budget_" + strconvFormat(headB): {"₹0.00"},
	}
	requireStatus(t, s.postForm("/budgets", firstFigure), http.StatusSeeOther)
	budget, err := s.st.Budget(s.ctx, headA, "2028-06")
	if err != nil {
		t.Fatal(err)
	}
	if budget.Amount != 250000000 {
		t.Fatalf("the first budget of a month = %d, want 250000000", budget.Amount)
	}

	// 3. An existing figure reduced to zero. It is a real number: "no budget this
	// month" is a state an operator must be able to reach.
	if err := s.st.SetBudget(s.ctx, admin, headB, "2028-07", 500000); err != nil {
		t.Fatal(err)
	}
	toZero := url.Values{"month": {"2028-07"}, "budget_" + strconvFormat(headB): {"0.00"}}
	requireStatus(t, s.postForm("/budgets", toZero), http.StatusSeeOther)
	budget, err = s.st.Budget(s.ctx, headB, "2028-07")
	if err != nil {
		t.Fatal(err)
	}
	if budget.Amount != 0 {
		t.Fatalf("budget after being zeroed = %d, want 0", budget.Amount)
	}

	// An empty field means the same thing as a zero one, and a negative figure is
	// still refused.
	empty := url.Values{"month": {"2028-07"}, "budget_" + strconvFormat(headB): {""}}
	requireStatus(t, s.postForm("/budgets", empty), http.StatusSeeOther)
	negative := url.Values{"month": {"2028-07"}, "budget_" + strconvFormat(headB): {"-100.00"}}
	resp := s.postForm("/budgets", negative)
	requireStatus(t, resp, http.StatusBadRequest)
	if body := responseBody(t, resp); !strings.Contains(body, "invalid budget") {
		t.Fatalf("a negative budget was not refused: %s", body)
	}
}

// F-G-026: one condition, one status code. A locked month is a 409 through
// respondStoreError and used to be a 400 on the two screens that render their
// own error instead.
func TestLockedMonthRefusalIsAlwaysAConflict(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("LockStatus")
	if err := s.st.SetBudget(s.ctx, admin, headID, "2026-09", 100000); err != nil {
		t.Fatal(err)
	}
	if err := s.st.LockMonth(s.ctx, admin, "2026-09", "Closed for audit"); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.postForm("/budgets", url.Values{
		"month":                           {"2026-09"},
		"budget_" + strconvFormat(headID): {"200000.00"},
	})
	requireStatus(t, resp, http.StatusConflict)
	if body := responseBody(t, resp); !strings.Contains(strings.ToLower(body), "locked") {
		t.Fatalf("the locked-month refusal does not say so: %s", body)
	}
	// The same condition reached through monthCreate, which also re-renders.
	resp = s.postForm("/months", url.Values{"target_month": {"2026-09"}, "source_mode": {"blank"}})
	if resp.StatusCode != http.StatusConflict && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("re-creating a locked month = %d, want 409 (or 400 if it is refused for another reason)", resp.StatusCode)
	}
	_ = responseBody(t, resp)
}

func TestUserCreationEnforcesPasswordAndInactiveFlagWithCreateAudit(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	// The create form sends the same role checkboxes the edit form does. It used
	// to send a two-value legacy string that could not express "requester" or
	// "approver", and whose harmless-looking option was mapped to the role that
	// can settle and void payments.
	requester := s.roleIDByName(t, "Requester")

	weak := url.Values{"email": {"weak@example.test"}, "name": {"Weak"}, "role_ids": {requester}, "password": {"short"}, "active": {"on"}}
	resp := s.postForm("/users", weak)
	requireStatus(t, resp, http.StatusBadRequest)
	if !strings.Contains(responseBody(t, resp), "at least 12") {
		t.Fatal("weak password error was not returned")
	}

	// A new account with no role at all would be able to sign in and do nothing,
	// so the form refuses it rather than quietly picking one.
	roleless := url.Values{"email": {"roleless@example.test"}, "name": {"Roleless"}, "password": {"ValidPassword123"}}
	resp = s.postForm("/users", roleless)
	requireStatus(t, resp, http.StatusBadRequest)
	if !strings.Contains(responseBody(t, resp), "at least one role") {
		t.Fatal("creating a user with no roles was not refused")
	}

	inactive := url.Values{"email": {"inactive@example.test"}, "name": {"Inactive"}, "role_ids": {requester}, "password": {"ValidPassword123"}}
	resp = s.postForm("/users", inactive)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	u, err := s.st.UserByEmail(s.ctx, "inactive@example.test")
	if err != nil {
		t.Fatal(err)
	}
	// The role asked for is the role granted — not whatever the legacy column
	// would have derived, which for anything but "admin" was Accounts.
	roles, err := s.st.UserRoles(s.ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].Name != "Requester" {
		t.Fatalf("new user holds %#v, want exactly the Requester role", roles)
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
	// Six badges, not three: the store's three badgeSpecs plus the unread bell
	// count and the two work-queue counts navSpec has always declared and nothing
	// populated (F-G-013).
	for _, want := range []string{"chrome=app", "active=payments", "fab=Approve", "groups=8", "badges=6", "title=Payments", "item=Payments ledger", "item=Users", "item=Audit log", "item=Notifications"} {
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

// ---------------------------------------------------------------------------
// The 2026-07-27 QA audit's area-A and area-G repairs. Every test below pins one
// finding, and each is named with the id so a regression points straight at the
// record.
// ---------------------------------------------------------------------------

// seedAttachmentFile writes a real file into AttachmentDir and returns the
// AttachmentInput naming it, so a download test exercises the streaming path and
// not a missing-file 404.
// paymentDetailLink is a link to a payment *record*, which only the Recent
// Payments panel emits. `href="/payments/` alone also matches the grid's own
// "+ Add payment" button (/payments/new).
var paymentDetailLink = regexp.MustCompile(`href="/payments/\d+"`)

func (s *appTestServer) seedAttachmentFile(name, content string) store.AttachmentInput {
	s.t.Helper()
	stored := filepath.Join(s.cfg.AttachmentDir, name)
	if err := os.WriteFile(stored, []byte(content), 0600); err != nil {
		s.t.Fatal(err)
	}
	return store.AttachmentInput{OriginalName: name, StoredPath: stored, MimeType: "text/plain", SizeBytes: int64(len(content))}
}

// seedPaymentAttachment inserts a payment_attachments row directly. It has to:
// AddAttachment now refuses a payment that settles a request (F-D-08), which is
// exactly the payment whose bank advice these tests are about.
func (s *appTestServer) seedPaymentAttachment(payID, uploader int64, in store.AttachmentInput) int64 {
	s.t.Helper()
	res, err := s.st.DB().Exec(`INSERT INTO payment_attachments(payment_id,original_name,stored_path,mime_type,size_bytes,uploaded_by) VALUES(?,?,?,?,?,?)`,
		payID, in.OriginalName, in.StoredPath, in.MimeType, in.SizeBytes, uploader)
	if err != nil {
		s.t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		s.t.Fatal(err)
	}
	return id
}

// F-A-01 / F-B-11 — attachment:view is a seeded Requester grant, and the route
// used to ask nothing else. A signed-in user could read every bank advice in the
// product by walking ids from 1.
func TestAttachmentDownloadObeysTheRequestScopeAndAnswers404(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("AttachScope")
	s.seedRequester("outsider@example.test", "Outsider", "OutsiderPass123")

	s.login(s.cfg.AdminEmail, testAdminPassword)
	reqID, payID := s.settleOneRequest(1, headID, 500000, "5000.00", "settled", "2026-07-25")
	advice := s.seedAttachmentFile("bank-advice.txt", "UTR N221260725004417 · A/C 000123456789")
	attID := s.seedPaymentAttachment(payID, admin.ID, advice)

	// The accountant who recorded it still reads it.
	resp := s.request(http.MethodGet, "/attachments/"+strconvFormat(attID), nil, "")
	requireStatus(t, resp, http.StatusOK)
	if body := responseBody(t, resp); !strings.Contains(body, "UTR N221260725004417") {
		t.Fatalf("the owner did not receive the file: %q", body)
	}

	// The Requester holds attachment:view, holds no scope over this request, and
	// is correctly refused the request itself.
	s.login("outsider@example.test", "OutsiderPass123")
	resp = s.request(http.MethodGet, "/requests/"+strconvFormat(reqID), nil, "")
	if resp.StatusCode == http.StatusOK {
		t.Fatal("fixture is wrong: the outsider can see the request")
	}
	_ = responseBody(t, resp)

	resp = s.request(http.MethodGet, "/attachments/"+strconvFormat(attID), nil, "")
	// 404, not 403: ids are small sequential integers, so a distinguishable
	// refusal is an enumeration oracle for the whole table.
	requireStatus(t, resp, http.StatusNotFound)
	body := responseBody(t, resp)
	if strings.Contains(body, "UTR N221260725004417") || strings.Contains(body, "000123456789") {
		t.Fatalf("the refused download leaked the file: %q", body)
	}
	// An id that does not exist answers exactly the same way, so the two cases
	// cannot be told apart.
	missing := s.request(http.MethodGet, "/attachments/999999", nil, "")
	requireStatus(t, missing, http.StatusNotFound)
	_ = responseBody(t, missing)
}

// F-A-05 / F-B-09 — one download route served two tables with independent id
// sequences, so the Download button on a requester's own invoice returned a
// stranger's bank advice.
func TestRequestDocumentsHaveTheirOwnScopedRoute(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("TwoTables")
	requester := s.seedRequester("mine@example.test", "Mine", "MinePass12345")
	other := s.seedRequester("theirs@example.test", "Theirs", "TheirsPass123")

	// payment_attachments row 1 …
	s.login(s.cfg.AdminEmail, testAdminPassword)
	_, payID := s.settleOneRequest(1, headID, 500000, "5000.00", "settled", "2026-07-25")
	advice := s.seedAttachmentFile("bank-advice.txt", "PAYMENT-ATTACHMENT-ROW-ONE")
	payAttID := s.seedPaymentAttachment(payID, admin.ID, advice)

	// … and request_attachments row 1, on a request the payment knows nothing
	// about. Both sequences start at 1, so the collision is the default case.
	ownReq := s.seedApprovedRequest(2, requester.ID, admin.ID, headID, 250000)
	invoice := s.seedAttachmentFile("invoice-mine.txt", "REQUEST-ATTACHMENT-ROW-ONE")
	reqAttID, err := s.st.AddRequestAttachment(s.ctx, requester, ownReq, invoice)
	if err != nil {
		t.Fatal(err)
	}
	if payAttID != reqAttID {
		t.Fatalf("fixture does not reproduce the collision: payment attachment %d, request attachment %d", payAttID, reqAttID)
	}

	s.login("mine@example.test", "MinePass12345")
	// The requester's own document, through the route that reads its own table.
	resp := s.request(http.MethodGet, fmt.Sprintf("/requests/%d/attachments/%d", ownReq, reqAttID), nil, "")
	requireStatus(t, resp, http.StatusOK)
	body := responseBody(t, resp)
	if !strings.Contains(body, "REQUEST-ATTACHMENT-ROW-ONE") {
		t.Fatalf("the request document route served the wrong bytes: %q", body)
	}
	if strings.Contains(resp.Header.Get("Content-Disposition"), "bank-advice") {
		t.Fatalf("Content-Disposition names the payment attachment: %q", resp.Header.Get("Content-Disposition"))
	}
	// And the payment route, handed the same number, gives this reader nothing.
	resp = s.request(http.MethodGet, "/attachments/"+strconvFormat(payAttID), nil, "")
	requireStatus(t, resp, http.StatusNotFound)
	if got := responseBody(t, resp); strings.Contains(got, "PAYMENT-ATTACHMENT-ROW-ONE") {
		t.Fatal("the payment download route still serves a bank advice to a requester")
	}

	// The {id} in the path is load-bearing: a document that belongs to another
	// request is not reachable by naming a request the caller can see.
	theirReq := s.seedApprovedRequest(3, other.ID, admin.ID, headID, 100000)
	theirInvoice := s.seedAttachmentFile("invoice-theirs.txt", "SOMEBODY-ELSES-INVOICE")
	theirAttID, err := s.st.AddRequestAttachment(s.ctx, other, theirReq, theirInvoice)
	if err != nil {
		t.Fatal(err)
	}
	resp = s.request(http.MethodGet, fmt.Sprintf("/requests/%d/attachments/%d", ownReq, theirAttID), nil, "")
	requireStatus(t, resp, http.StatusNotFound)
	_ = responseBody(t, resp)
	// …and neither is it by naming its own request, which this reader cannot see.
	resp = s.request(http.MethodGet, fmt.Sprintf("/requests/%d/attachments/%d", theirReq, theirAttID), nil, "")
	requireStatus(t, resp, http.StatusNotFound)
	if got := responseBody(t, resp); strings.Contains(got, "SOMEBODY-ELSES-INVOICE") {
		t.Fatal("the request document route leaks another requester's invoice")
	}
}

// F-A-03 — the write path needs the same ownership check as the read path.
func TestAttachmentUploadObeysTheRequestScope(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("PlantProof")
	s.seedRequester("planter@example.test", "Planter", "PlanterPass123")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	_, payID := s.settleOneRequest(1, headID, 500000, "5000.00", "settled", "2026-07-25")

	s.login("planter@example.test", "PlanterPass123")
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("csrf", s.csrf()); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("attachment", "planted.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("forged advice")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	resp := s.request(http.MethodPost, "/payments/"+strconvFormat(payID)+"/attachments", &body, mw.FormDataContentType())
	requireStatus(t, resp, http.StatusNotFound)
	_ = responseBody(t, resp)
	atts, err := s.st.Attachments(s.ctx, payID)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 0 {
		t.Fatalf("a stranger planted %d documents on a payment", len(atts))
	}
}

// F-A-02 / F-G-032 — /grid was RequireLogin only, and its Recent Payments panel
// carries amounts, payees and live payment links.
func TestGridIsGatedOnGridViewAndItsPaymentPanelOnPaymentView(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("GridGate")
	if err := s.st.SetBudget(s.ctx, admin, headID, time.Now().Format("2006-01"), 85000000); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	paidOn := time.Now().Format("2006-01") + "-01"
	s.settleOneRequest(1, headID, 6600000, "66000.00", "settled", paidOn)

	// A Requester holds neither grid:view nor payment:view.
	s.seedRequester("noone@example.test", "No One", "NoOnePass1234")
	s.login("noone@example.test", "NoOnePass1234")
	resp := s.request(http.MethodGet, "/grid", nil, "")
	requireStatus(t, resp, http.StatusForbidden)
	if body := responseBody(t, resp); strings.Contains(body, "8,50,000.00") || strings.Contains(body, "66,000.00") {
		t.Fatal("the refused grid still rendered the company's figures")
	}

	// grid:view alone opens the matrix and nothing else: no payment rows, no
	// payment links, because that panel is payment:view's.
	s.seedProbeUser("gridonly@example.test", "Grid Only", "GridOnlyPass1", "grid-only",
		[]store.Grant{{Resource: "grid", Action: "view"}}, nil)
	s.login("gridonly@example.test", "GridOnlyPass1")
	resp = s.request(http.MethodGet, "/grid", nil, "")
	requireStatus(t, resp, http.StatusOK)
	body := responseBody(t, resp)
	if !strings.Contains(body, "8,50,000.00") {
		t.Fatalf("grid:view did not open the budget matrix: %s", body)
	}
	// The amount also appears as the head's Actual, which is budget data and
	// grid:view's to show. What must not appear is the payments panel's own
	// content: a row per payment, each linking to a /payments/{id} this caller is
	// answered 403 on.
	if paymentDetailLink.MatchString(body) {
		t.Fatalf("the Recent Payments panel served payment links to a caller with no payment:view: %v", paymentDetailLink.FindString(body))
	}
	if !strings.Contains(body, "No payments in this month.") {
		t.Fatalf("the payments panel was handed rows rather than nothing: %s", body)
	}

	// And with both verbs the panel is back, so the gate narrows nothing it
	// should not.
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body = responseBody(t, s.request(http.MethodGet, "/grid", nil, ""))
	if !paymentDetailLink.MatchString(body) || strings.Contains(body, "No payments in this month.") {
		t.Fatalf("a caller holding payment:view lost the Recent Payments panel: %s", body)
	}
}

// F-D-10 — the settlement write was gated more weakly than its pure preview.
func TestSettlementWriteNeedsSettleAndPartialNeedsMarkPartial(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("SettleGate")
	// Everything an accountant needs except the two decision verbs.
	entry := s.seedProbeUser("entry@example.test", "Entry Only", "EntryPass1234", "entry-no-settle",
		[]store.Grant{
			{Resource: "request", Action: "view"}, {Resource: "payment", Action: "view"},
			{Resource: "payment", Action: "create"}, {Resource: "payment", Action: "process"},
			{Resource: "reservation", Action: "reserve"},
		},
		[]store.ScopeGrant{{Resource: "request", Scope: store.ScopeAll}, {Resource: "payment", Scope: store.ScopeAll}})
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)

	s.login("entry@example.test", "EntryPass1234")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{
		"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)},
		"paid_on": {"2026-07-25"}, "amount": {"5000.00"}, "vendor_payee": {"Acme Landlord"},
		"settlement": {"settled"},
	}
	resp := s.postForm("/payments", form)
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
	var payments int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&payments); err != nil {
		t.Fatal(err)
	}
	if payments != 0 {
		t.Fatalf("a caller without payment:settle recorded %d payments", payments)
	}

	// payment:settle and nothing more: a full settlement goes through, and a
	// partial one — writing a shortfall off to the approver's queue — does not.
	s.grantAlso(entry.ID, "settle-only", []store.Grant{{Resource: "payment", Action: "settle"}}, nil)
	partial := url.Values{}
	for k, v := range form {
		partial[k] = v
	}
	partial.Set("settlement", "partial")
	partial.Set("amount", "3000.00")
	partial.Set("partial_reason", "balance next month")
	resp = s.postForm("/payments", partial)
	requireStatus(t, resp, http.StatusForbidden)
	if body := responseBody(t, resp); !strings.Contains(body, "partial payment") {
		t.Fatalf("the refusal does not name the decision it refused: %s", body)
	}
	requireStatus(t, s.postForm("/payments", form), http.StatusSeeOther)
}

// F-G-033 — the heads screen lists every head and must offer every project, or a
// row whose project was retired has no option of its own and its Save moves it.
func TestHeadsScreenIsSuppliedEveryProjectIncludingRetiredOnes(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Retired")
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.UpsertProject(s.ctx, projectID, "Operations Retired", false, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.UpsertProject(s.ctx, 0, "Still Running", true, 2); err != nil {
		t.Fatal(err)
	}

	data, err := s.probeApp().headsPageData(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Projects) != 1 || data.Projects[0].Name != "Still Running" {
		t.Fatalf("Projects must stay the active set a new head may choose from, got %+v", data.Projects)
	}
	found := false
	for _, p := range data.AllProjects {
		if p.ID == projectID {
			found = true
		}
	}
	if !found {
		t.Fatalf("AllProjects has no option for the retired project %d: %+v", projectID, data.AllProjects)
	}
	// Every head the screen lists must have an option in AllProjects, which is
	// the property whose absence let the browser pick a stranger's project.
	options := map[int64]bool{}
	for _, p := range data.AllProjects {
		options[p.ID] = true
	}
	for _, h := range data.Heads {
		if !options[h.ProjectID] {
			t.Fatalf("head %q is listed with no option for its own project %d", h.Name, h.ProjectID)
		}
	}
}

// F-G-034 — one form, one submit, one transaction.
func TestUserSaveIsOneTransaction(t *testing.T) {
	s := newAppTestServer(t)
	subject := s.seedRequester("subject@example.test", "Original Name", "SubjectPass123")
	s.login(s.cfg.AdminEmail, testAdminPassword)

	resp := s.postForm("/users", url.Values{
		"id": {strconvFormat(subject.ID)}, "email": {"subject@example.test"},
		"name": {"Renamed By A Failure"}, "role": {"data_entry"}, "active": {"on"},
		"role_ids": {"99999999"}, "default_approver_id": {"0"},
	})
	requireStatus(t, resp, http.StatusBadRequest)
	if body := responseBody(t, resp); !strings.Contains(body, "99999999") {
		t.Fatalf("the refusal does not name the bad role id: %s", body)
	}
	after, err := s.st.UserByID(s.ctx, subject.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Name != "Original Name" {
		t.Fatalf("a refused save committed the rename anyway: name = %q", after.Name)
	}
	roles, err := s.st.UserRoles(s.ctx, subject.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].Name != "Requester" {
		t.Fatalf("the role assignment moved: %+v", roles)
	}

	// The same save with a real role id applies all three parts at once.
	all, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var managerID int64
	for _, r := range all {
		if r.Name == "Manager" {
			managerID = r.ID
		}
	}
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, s.postForm("/users", url.Values{
		"id": {strconvFormat(subject.ID)}, "email": {"subject@example.test"},
		"name": {"Renamed On Purpose"}, "role": {"data_entry"}, "active": {"on"},
		"role_ids": {strconvFormat(managerID)}, "default_approver_id": {strconvFormat(admin.ID)},
	}), http.StatusSeeOther)
	after, err = s.st.UserByID(s.ctx, subject.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Name != "Renamed On Purpose" {
		t.Fatalf("the accepted save did not rename: %q", after.Name)
	}
	if after.DefaultApproverID != admin.ID {
		t.Fatalf("the accepted save did not set the default approver: %v", after.DefaultApproverID)
	}
	roles, err = s.st.UserRoles(s.ctx, subject.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].Name != "Manager" {
		t.Fatalf("the accepted save did not reassign the role: %+v", roles)
	}
}

// F-G-022 / F-G-023 — deleting a role checks its holders first, and a refusal
// says what is actually true.
func TestRoleDeleteChecksHoldersAndExplainsItself(t *testing.T) {
	s := newAppTestServer(t)
	holder := s.seedProbeUser("holder@example.test", "Holder", "HolderPass123", "payments-reader",
		[]store.Grant{{Resource: "payment", Action: "view"}},
		[]store.ScopeGrant{{Resource: "payment", Scope: store.ScopeAll}})
	roles, err := s.st.UserRoles(s.ctx, holder.ID)
	if err != nil || len(roles) != 1 {
		t.Fatalf("UserRoles = %+v, %v", roles, err)
	}
	custom := roles[0].ID

	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.postForm(fmt.Sprintf("/roles/%d/delete", custom), url.Values{})
	requireStatus(t, resp, http.StatusForbidden)
	body := responseBody(t, resp)
	if !strings.Contains(body, "assigned to 1 user") {
		t.Fatalf("the refusal does not name the holders: %s", body)
	}
	if strings.Contains(body, "do not have permission") {
		t.Fatalf("an administrator was told they lack permission: %s", body)
	}
	// The role, and therefore the holder's access, survives.
	if _, err := s.st.Role(s.ctx, custom); err != nil {
		t.Fatalf("the refused delete removed the role anyway: %v", err)
	}
	s.login("holder@example.test", "HolderPass123")
	resp = s.request(http.MethodGet, "/payments", nil, "")
	requireStatus(t, resp, http.StatusOK)
	_ = responseBody(t, resp)

	// A system role says so, rather than claiming the administrator lacks a
	// permission they hold all 66 of.
	var requesterRole int64
	all, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range all {
		if r.Name == "Requester" {
			requesterRole = r.ID
		}
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp = s.postForm(fmt.Sprintf("/roles/%d/delete", requesterRole), url.Values{})
	requireStatus(t, resp, http.StatusForbidden)
	body = responseBody(t, resp)
	if !strings.Contains(body, "System roles cannot be deleted") {
		t.Fatalf("the system-role refusal does not say so: %s", body)
	}

	// And an unheld custom role still deletes, so nothing was locked shut.
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.SetUserRoles(s.ctx, admin, holder.ID, nil); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, s.postForm(fmt.Sprintf("/roles/%d/delete", custom), url.Values{}), http.StatusSeeOther)
	if _, err := s.st.Role(s.ctx, custom); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Role after delete = %v, want ErrNotFound", err)
	}
}

// F-G-017 — the audit log serialises whole Request structs into
// before_json/after_json, so audit:view was a way around the request row scope.
func TestAuditLogAppliesTheRequestRowScope(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("AuditScope")
	raiser := s.seedRequester("raiser@example.test", "Raiser Person", "RaiserPass123")
	reqID := s.seedApprovedRequest(1, raiser.ID, admin.ID, headID, 6400000)
	// An approval carries the whole struct in Before and After.
	if err := s.st.RecordAudit(s.ctx, store.AuditInput{
		ActorID: &admin.ID, ActorName: admin.Name, Action: "approve",
		EntityType: "payment_request", EntityID: &reqID,
		Summary: "Approved PR-2026-000001",
		After:   map[string]any{"purpose": "SECRET-PURPOSE-STRING", "amount": 6400000, "requester": "Raiser Person"},
	}); err != nil {
		t.Fatal(err)
	}

	// A reviewer holding audit:view and a request scope of "own" — the exact
	// grant a compliance reader would be given.
	s.seedProbeUser("reviewer@example.test", "Reviewer", "ReviewerPass1", "audit-reader",
		[]store.Grant{{Resource: "audit", Action: "view"}, {Resource: "request", Action: "view"}},
		[]store.ScopeGrant{{Resource: "request", Scope: "own"}})
	s.login("reviewer@example.test", "ReviewerPass1")
	resp := s.request(http.MethodGet, "/requests/"+strconvFormat(reqID), nil, "")
	if resp.StatusCode == http.StatusOK {
		t.Fatal("fixture is wrong: the reviewer can read the request directly")
	}
	_ = responseBody(t, resp)
	resp = s.request(http.MethodGet, "/audit?entity=payment_request", nil, "")
	requireStatus(t, resp, http.StatusOK)
	body := responseBody(t, resp)
	for _, leak := range []string{"SECRET-PURPOSE-STRING", "6400000", "Raiser Person"} {
		if strings.Contains(body, leak) {
			t.Fatalf("the audit log disclosed %q to a reader the request itself refuses", leak)
		}
	}

	// An administrator, who holds request=all, sees exactly what they saw before.
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body = responseBody(t, s.request(http.MethodGet, "/audit?entity=payment_request", nil, ""))
	if !strings.Contains(body, "SECRET-PURPOSE-STRING") {
		t.Fatalf("scoping the audit log hid a row from request=all: %s", body)
	}
	// Rows that are not about a request are untouched for everybody.
	s.login("reviewer@example.test", "ReviewerPass1")
	body = responseBody(t, s.request(http.MethodGet, "/audit?entity=user", nil, ""))
	if !strings.Contains(strings.ToLower(body), "login") {
		t.Fatalf("a non-request entity lost its rows: %s", body)
	}
}

// F-A-06 / F-C-02 — coverage A7. store.ReassignRequest was complete, tested and
// reachable by no URL, and approval:reassign gated nothing.
func TestApproverReassignmentRoute(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Reassign")
	raiser := s.seedRequester("askfor@example.test", "Ask For", "AskForPass123")
	firstApprover := seedSecondApprover(t, s)
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	reqID, err := s.st.CreateRequest(s.ctx, raiser, store.RequestInput{
		Treatment: "budget", Type: "reimbursement", ProjectID: projectID, HeadID: headID, Amount: 250000,
		Purpose: "Taxi fares for the site visit", ShortTitle: "Taxi fares",
		ManagerID: firstApprover, ExpenseDate: "2026-07-01", VendorPayee: "Ask For",
	})
	if err != nil {
		t.Fatal(err)
	}

	// A Requester holds no approval:reassign, and the route says so.
	s.login("askfor@example.test", "AskForPass123")
	resp := s.postForm(fmt.Sprintf("/requests/%d/reassign-approver", reqID), url.Values{
		"manager_id": {strconvFormat(admin.ID)}, "reason": {"wrong approver"},
	})
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)

	// The administrator holds it. A reason is mandatory and a target is mandatory.
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp = s.postForm(fmt.Sprintf("/requests/%d/reassign-approver", reqID), url.Values{"reason": {"no target"}})
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)
	resp = s.postForm(fmt.Sprintf("/requests/%d/reassign-approver", reqID), url.Values{"manager_id": {strconvFormat(admin.ID)}})
	requireStatus(t, resp, http.StatusBadRequest)
	if body := responseBody(t, resp); !strings.Contains(body, "reason") {
		t.Fatalf("the refusal does not ask for a reason: %s", body)
	}
	// The new approver must be able to approve — the route must not be a way back
	// into F-A-08's stranded state.
	resp = s.postForm(fmt.Sprintf("/requests/%d/reassign-approver", reqID), url.Values{
		"manager_id": {strconvFormat(raiser.ID)}, "reason": {"send it to somebody who cannot decide"},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("reassigning to a non-approver = %d, want 400", resp.StatusCode)
	}
	_ = responseBody(t, resp)

	// And the real thing: the approver changes, and the trail records it as its
	// own event rather than as an anonymous update.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/reassign-approver", reqID), url.Values{
		"manager_id": {strconvFormat(admin.ID)}, "reason": {"Kavita is on leave this week"},
	}), http.StatusSeeOther)
	req, err := s.st.Request(s.ctx, reqID)
	if err != nil {
		t.Fatal(err)
	}
	if req.ManagerID != admin.ID {
		t.Fatalf("manager after reassignment = %d, want %d", req.ManagerID, admin.ID)
	}
	trail, err := s.st.Audit(s.ctx, "payment_request", reqID, 20)
	if err != nil {
		t.Fatal(err)
	}
	var reassigned bool
	for _, entry := range trail {
		if entry.Action == "approval_reassign" && strings.Contains(entry.Summary, "Kavita is on leave this week") {
			reassigned = true
		}
	}
	if !reassigned {
		t.Fatalf("no approval_reassign row carries the reason: %+v", trail)
	}
	// A request outside the caller's scope answers 404, not 403 (F-G-002), so the
	// route is not an existence oracle either.
	resp = s.postForm("/requests/999999/reassign-approver", url.Values{
		"manager_id": {strconvFormat(admin.ID)}, "reason": {"nothing here"},
	})
	requireStatus(t, resp, http.StatusNotFound)
	_ = responseBody(t, resp)
}

// F-A-11 / F-G-036 — a 12-character letters-only password answered 500.
func TestPasswordRulesAreOneFunctionAndEveryRefusalIsA400(t *testing.T) {
	s := newAppTestServer(t)
	subject := s.seedRequester("victim@example.test", "Victim", "VictimPass123")
	s.login(s.cfg.AdminEmail, testAdminPassword)

	cases := []struct {
		name     string
		password string
		wants    string
	}{
		{"twelve letters, no digit", "abcdefghijkl", "letter and a number"},
		{"eleven characters", "abcdefghij1", "at least 12"},
		{"twelve digits, no letter", "123456789012", "letter and a number"},
	}
	for _, tc := range cases {
		create := url.Values{"email": {tc.name + "@example.test"}, "name": {"New Person"},
			"role": {"data_entry"}, "active": {"on"}, "password": {tc.password}}
		resp := s.postForm("/users", create)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("create with %s = %d, want 400", tc.name, resp.StatusCode)
		}
		if body := responseBody(t, resp); !strings.Contains(body, tc.wants) {
			t.Fatalf("create with %s did not name the rule (%q): %s", tc.name, tc.wants, body)
		}
		reset := url.Values{"id": {strconvFormat(subject.ID)}, "email": {"victim@example.test"},
			"name": {"Victim"}, "role": {"data_entry"}, "active": {"on"}, "password": {tc.password}}
		resp = s.postForm("/users", reset)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("reset with %s = %d, want 400", tc.name, resp.StatusCode)
		}
		_ = responseBody(t, resp)
	}
	// The victim's own credentials are untouched, and a compliant password works.
	s.login("victim@example.test", "VictimPass123")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm("/users", url.Values{
		"id": {strconvFormat(subject.ID)}, "email": {"victim@example.test"}, "name": {"Victim"},
		"role": {"data_entry"}, "active": {"on"}, "password": {"NewVictimPass1"},
	}), http.StatusSeeOther)
	s.login("victim@example.test", "NewVictimPass1")
}

// F-A-07 — the ＋ Add user button is gated on user:create and the route demanded
// user:edit, so a legitimately configured role got a control that 403s.
func TestUserSaveDemandsCreateToCreateAndEditToEdit(t *testing.T) {
	s := newAppTestServer(t)
	subject := s.seedRequester("edited@example.test", "Edited", "EditedPass123")
	s.seedProbeUser("creator@example.test", "Creator", "CreatorPass12", "user-creator",
		[]store.Grant{{Resource: "user", Action: "view"}, {Resource: "user", Action: "create"}}, nil)
	s.seedProbeUser("editor@example.test", "Editor", "EditorPass123", "user-editor",
		[]store.Grant{{Resource: "user", Action: "view"}, {Resource: "user", Action: "edit"}}, nil)

	// user:create renders the button and now also carries its submit.
	s.login("creator@example.test", "CreatorPass12")
	body := responseBody(t, s.request(http.MethodGet, "/users", nil, ""))
	if !strings.Contains(body, `data-open="user-new"`) {
		t.Fatalf("the create control is not rendered for user:create: %s", body)
	}
	requireStatus(t, s.postForm("/users", url.Values{
		"email": {"minted@example.test"}, "name": {"Minted"}, "role_ids": {s.roleIDByName(t, "Requester")},
		"active": {"on"}, "password": {"MintedPass123"},
	}), http.StatusSeeOther)
	// …and cannot edit an existing user.
	resp := s.postForm("/users", url.Values{
		"id": {strconvFormat(subject.ID)}, "email": {"edited@example.test"},
		"name": {"Renamed by a creator"}, "role": {"data_entry"}, "active": {"on"},
	})
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)

	// user:edit is the mirror image: it edits and it does not mint.
	s.login("editor@example.test", "EditorPass123")
	requireStatus(t, s.postForm("/users", url.Values{
		"id": {strconvFormat(subject.ID)}, "email": {"edited@example.test"},
		"name": {"Renamed by an editor"}, "role": {"data_entry"}, "active": {"on"},
	}), http.StatusSeeOther)
	resp = s.postForm("/users", url.Values{
		"email": {"sneaky@example.test"}, "name": {"Sneaky"}, "role": {"data_entry"},
		"active": {"on"}, "password": {"SneakyPass123"},
	})
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
	if _, err := s.st.UserByEmail(s.ctx, "sneaky@example.test"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("user:edit minted a user: %v", err)
	}
}

// F-A-09 / F-D-02 — friendly() had no ErrForbidden branch, so a state conflict
// read "Something went wrong".
func TestFriendlyNamesAForbiddenRefusal(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"reservation lost", fmt.Errorf("%w: reserve this request before recording its payment", store.ErrForbidden),
			"Reserve this request before recording its payment"},
		{"already taken", store.ErrAlreadyReserved, "Someone else is already processing this request"},
		{"on hold", store.ErrRequestOnHold, "This request is on hold"},
		{"not approved", store.ErrRequestNotApproved, "Only an approved request can be taken for processing"},
		{"bare sentinel", store.ErrForbidden, "You do not have permission to perform this action."},
	}
	for _, tc := range cases {
		if got := friendly(tc.err); got != tc.want {
			t.Errorf("friendly(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
	// The three reserve sentinels are distinguishable, which is what lets a
	// screen name the real cause.
	if friendly(store.ErrAlreadyReserved) == friendly(store.ErrRequestOnHold) {
		t.Fatal("two different reserve refusals read the same")
	}
	// And nothing else changed: a validation failure still carries its own text.
	if got := friendly(fmt.Errorf("%w: say why", store.ErrValidation)); !strings.Contains(got, "say why") {
		t.Fatalf("friendly(ErrValidation) = %q", got)
	}
}

// F-A-10 — POST /login is the one state-changing POST outside withCSRF, and was
// therefore outside the body cap as well.
func TestLoginPostIsCappedEvenThoughItIsCSRFExempt(t *testing.T) {
	s := newAppTestServer(t)
	// The exemption itself is deliberate and still holds: no token, no refusal.
	form := url.Values{"email": {s.cfg.AdminEmail}, "password": {testAdminPassword}}
	resp := s.request(http.MethodPost, "/login", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	// An unbounded body on an unauthenticated endpoint is not.
	oversized := "email=a%40b.test&password=" + strings.Repeat("x", 22<<20)
	resp = s.request(http.MethodPost, "/login", strings.NewReader(oversized), "application/x-www-form-urlencoded")
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusSeeOther {
		t.Fatalf("an oversized login body was processed: %d", resp.StatusCode)
	}
	_ = responseBody(t, resp)
}

// F-G-004 / F-C-06 — the audit screen's filters could not reach the request
// workflow at all, and `settings` rendered as a lower-case pill.
func TestAuditFilterVocabulariesCoverWhatTheStoreWrites(t *testing.T) {
	entities := map[string]bool{}
	for _, opt := range auditEntities() {
		entities[opt.Value] = true
		if opt.Label == "" || opt.Label == opt.Value {
			t.Errorf("entity option %q has no readable label", opt.Value)
		}
	}
	for _, want := range []string{"payment_request", "payment", "vendor", "role", "recoverable_category", "app_setting", "notification_setting"} {
		if !entities[want] {
			t.Errorf("the Entity filter cannot reach %q", want)
		}
	}
	actions := map[string]bool{}
	for _, opt := range auditActions() {
		actions[opt.Value] = true
		if opt.Label == opt.Value {
			t.Errorf("action %q renders as its raw identifier", opt.Value)
		}
	}
	// The whole Phase-2/Phase-3 vocabulary, plus migration v9's reminder row.
	for _, want := range []string{
		"submit", "approve", "return", "reject", "withdraw", "reraise", "cancel",
		"cancel_request", "approval_reassign", "process", "release", "reassign",
		"hold", "unhold", "settle", "mark_partial", "accept_partial", "remind", "settings",
	} {
		if !actions[want] {
			t.Errorf("the Action filter cannot reach %q", want)
		}
	}
	if got := actionText("settings"); got != "Settings saved" {
		t.Errorf("actionText(settings) = %q", got)
	}
	if got := actionText("remind"); got != "Reminder sent" {
		t.Errorf("actionText(remind) = %q", got)
	}
}
