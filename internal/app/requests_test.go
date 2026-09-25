package app

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// assignRole grants a Phase-1 seeded role to a user by name, replacing whatever
// the legacy-role back-fill gave them.
func (s *appTestServer) assignRole(userID int64, roleName string) {
	s.t.Helper()
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		s.t.Fatal(err)
	}
	var roleID int64
	for _, r := range roles {
		if strings.EqualFold(r.Name, roleName) {
			roleID = r.ID
		}
	}
	if roleID == 0 {
		s.t.Fatalf("seeded role %q not found", roleName)
	}
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.SetUserRoles(s.ctx, admin, userID, []int64{roleID}); err != nil {
		s.t.Fatal(err)
	}
}

// seedRequester creates an active user holding only the Requester role and
// returns their store record.
func (s *appTestServer) seedRequester(email, name, password string) store.User {
	s.t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.t.Fatal(err)
	}
	id, err := s.st.CreateUser(s.ctx, email, name, hash, "data_entry", true)
	if err != nil {
		s.t.Fatal(err)
	}
	s.assignRole(id, "Requester")
	u, err := s.st.UserByID(s.ctx, id)
	if err != nil {
		s.t.Fatal(err)
	}
	return u
}

// postFormHX is postForm with the header htmx sets on every request it makes,
// so a fragment endpoint is exercised the way the browser will reach it.
func (s *appTestServer) postFormHX(path string, form url.Values) *http.Response {
	s.t.Helper()
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf", s.csrf())
	req, err := http.NewRequest(http.MethodPost, s.server.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	return resp
}

// grantAlso adds a second role carrying extra grants to a user who already
// holds one. Phase 1's starter roles were never given request:cancel or
// approval:cancel — both verbs entered the canonical vocabulary with this
// phase's cancellation flow, and nothing back-filled the seeded Requester and
// Manager roles with them. In the product an administrator grants them on the
// Roles screen; the fixture does the same thing directly.
func (s *appTestServer) grantAlso(userID int64, roleName string, grants []store.Grant, scopes []store.ScopeGrant) {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
	roleID, err := s.st.CreateRole(s.ctx, admin, roleName, "")
	if err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.UpdateRolePermissions(s.ctx, admin, roleID, grants, scopes); err != nil {
		s.t.Fatal(err)
	}
	held, err := s.st.UserRoles(s.ctx, userID)
	if err != nil {
		s.t.Fatal(err)
	}
	ids := []int64{roleID}
	for _, role := range held {
		ids = append(ids, role.ID)
	}
	if err := s.st.SetUserRoles(s.ctx, admin, userID, ids); err != nil {
		s.t.Fatal(err)
	}
}

// seedSecondApprover returns a user id that holds approval:approve and is never
// the logged-in requester, so G8 cannot get in a fixture's way.
func seedSecondApprover(t *testing.T, s *appTestServer) int64 {
	t.Helper()
	hash, err := auth.HashPassword("ApproverPass123")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.st.CreateUser(s.ctx, "kavita@example.test", "Kavita Rao", hash, "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	s.assignRole(id, "Manager")
	return id
}

// between returns the markup of one element, so an assertion about a select can
// be made about that select rather than about the whole page.
func between(t *testing.T, body, open, close string) string {
	t.Helper()
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatalf("markup %q not found", open)
	}
	rest := body[start:]
	end := strings.Index(rest, close)
	if end < 0 {
		t.Fatalf("markup %q has no %q", open, close)
	}
	return rest[:end]
}

// probeApp builds an App over the test server's store without going through
// New, so the pure plumbing helpers can be exercised directly.
func (s *appTestServer) probeApp() *App {
	s.t.Helper()
	manager, err := auth.New(s.cfg, s.st)
	if err != nil {
		s.t.Fatal(err)
	}
	return &App{cfg: s.cfg, st: s.st, auth: manager, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// requestInput is the single reader of the request form. Everything the adaptive
// form can reveal has to arrive here, because the store — not the browser — is
// what decides whether a field was allowed to be filled in.
func TestRequestInputReadsEveryFieldTheAdaptiveFormCanReveal(t *testing.T) {
	form := url.Values{
		"treatment": {"recoverable"}, "type": {"employee_advance"},
		"recoverable_category": {"icd"}, "project_id": {"3"}, "head_id": {"7"},
		"vendor_id": {"11"}, "vendor_payee": {"Ignored"}, "short_title": {"Site mobilisation"},
		"purpose": {"survey crew"}, "needed_by": {"2026-08-01"}, "invoice_no": {"SE/1"},
		"invoice_date": {"2026-07-18"}, "expense_date": {"2026-07-17"},
		"advance_reason": {"cash for the crew"}, "counterparty": {"Anand Steel"},
		"expected_return_date": {"2026-11-30"}, "repayment_notes": {"refund on completion"},
		"urgent": {"on"}, "urgency_reason": {"supply stops Monday"},
		"attachment_exception_reason": {"invoice arrives Monday"},
		"manager_id":                  {"5"}, "amount": {"₹1,00,000.50"},
	}
	r := httptest.NewRequest(http.MethodPost, "/requests", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	in, err := requestInput(r)
	if err != nil {
		t.Fatal(err)
	}
	if in.Amount != 10000050 {
		t.Fatalf("amount = %d, want 10000050 paise (grouped input must parse)", in.Amount)
	}
	for _, tc := range []struct{ name, got, want string }{
		{"treatment", in.Treatment, "recoverable"},
		{"type", in.Type, "employee_advance"},
		{"recoverable_category", in.RecoverableCategory, "icd"},
		{"short_title", in.ShortTitle, "Site mobilisation"},
		{"purpose", in.Purpose, "survey crew"},
		{"needed_by", in.NeededBy, "2026-08-01"},
		{"invoice_no", in.InvoiceNo, "SE/1"},
		{"invoice_date", in.InvoiceDate, "2026-07-18"},
		{"expense_date", in.ExpenseDate, "2026-07-17"},
		{"advance_reason", in.AdvanceReason, "cash for the crew"},
		{"counterparty", in.Counterparty, "Anand Steel"},
		{"expected_return_date", in.ExpectedReturnDate, "2026-11-30"},
		{"repayment_notes", in.RepaymentNotes, "refund on completion"},
		{"urgency_reason", in.UrgencyReason, "supply stops Monday"},
		{"attachment_exception_reason", in.AttachmentExceptionReason, "invoice arrives Monday"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if !in.Urgent {
		t.Error("urgent checkbox was not read")
	}
	if in.ProjectID != 3 || in.HeadID != 7 || in.VendorID != 11 || in.ManagerID != 5 {
		t.Errorf("ids = %d/%d/%d/%d, want 3/7/11/5", in.ProjectID, in.HeadID, in.VendorID, in.ManagerID)
	}
	// RequesterID never comes from the form: the store stamps it from the actor,
	// which is what makes the self-approval rule (G8) unforgeable.
	if in.RequesterID != 0 {
		t.Errorf("requester_id = %d, want 0 — it must come from the session, never the form", in.RequesterID)
	}

	// A blank or unparseable amount is a validation error, not a silent zero.
	blank := httptest.NewRequest(http.MethodPost, "/requests", strings.NewReader("type=reimbursement"))
	blank.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := blank.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if _, err := requestInput(blank); err == nil {
		t.Fatal("a missing amount must be rejected")
	}
}

// The URL may narrow a caller's scope, never widen it. Q5/R6.
func TestScopeNarrowsFromTheURLButNeverWidens(t *testing.T) {
	s := newAppTestServer(t)
	a := s.probeApp()
	requester := s.seedRequester("scoped@example.test", "Scoped", "RequesterPass123")
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name      string
		user      store.User
		requested string
		want      string
	}{
		{"requester holds own", requester, "", "own"},
		{"requester cannot ask for all", requester, "all", "own"},
		{"requester cannot ask for assigned", requester, "assigned", "own"},
		{"admin holds all", admin, "", "all"},
		{"admin may narrow to assigned", admin, "assigned", "assigned"},
		{"admin may narrow to own", admin, "own", "own"},
		{"a nonsense scope is ignored", admin, "everything", "all"},
	} {
		if got := a.effectiveScope(tc.user, tc.requested); got != tc.want {
			t.Errorf("%s: effectiveScope(%q) = %q, want %q", tc.name, tc.requested, got, tc.want)
		}
	}
}

func TestCanViewRequestFollowsTheScope(t *testing.T) {
	me := store.User{ID: 7}
	mine := store.Request{RequesterID: 7, ManagerID: 9}
	assigned := store.Request{RequesterID: 3, ManagerID: 7}
	stranger := store.Request{RequesterID: 3, ManagerID: 9}

	for _, tc := range []struct {
		scope string
		req   store.Request
		want  bool
	}{
		{"all", stranger, true},
		{"assigned", assigned, true},
		{"assigned", mine, true},
		{"assigned", stranger, false},
		{"own", mine, true},
		{"own", assigned, false},
		{"", mine, false},
	} {
		if got := canViewRequest(tc.scope, me, tc.req); got != tc.want {
			t.Errorf("canViewRequest(%q, requester=%d manager=%d) = %v, want %v",
				tc.scope, tc.req.RequesterID, tc.req.ManagerID, got, tc.want)
		}
	}
}

func TestRequestExportIsScopedAndCarriesThePhase2Columns(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Exp")
	mgrID := seedSecondApprover(t, s)
	rhea := s.seedRequester("rhea@example.test", "Rhea", "RequesterPass123")
	if _, err := s.st.CreateRequest(s.ctx, rhea, store.RequestInput{Treatment: "budget",
		Type: "reimbursement", ShortTitle: "Lunch with the client", ProjectID: 1, HeadID: headID,
		Amount: 100000, Purpose: "client lunch", ExpenseDate: "2026-07-21", ManagerID: mgrID}); err != nil {
		t.Fatal(err)
	}

	s.login("rhea@example.test", "RequesterPass123")
	resp := s.request(http.MethodGet, "/requests/export.csv?scope=own", nil, "")
	requireStatus(t, resp, http.StatusOK)
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Fatalf("content type = %q, want text/csv", got)
	}
	body := responseBody(t, resp)
	if !strings.HasPrefix(body, "Number,Status,Type,Title,Amount,Approved amount,Payee,Requester,Approver,Created") {
		t.Fatalf("request export header unexpected: %s", body)
	}
	if !strings.Contains(body, "Lunch with the client") || !strings.Contains(body, "Kavita Rao") {
		t.Fatalf("the requester's own row is missing from their export: %s", body)
	}

	// Q5: another requester exports nothing of it, even asking for scope=all.
	s.seedRequester("olga@example.test", "Olga", "OtherPass1234")
	s.login("olga@example.test", "OtherPass1234")
	other := responseBody(t, s.request(http.MethodGet, "/requests/export.csv?scope=all", nil, ""))
	if strings.Contains(other, "Lunch with the client") {
		t.Fatalf("scope=all widened a requester's export: %s", other)
	}
}

// A16: picking a type is a navigation, not a form control. GET /requests/new is
// step 1 of 2 and lists the four types as links; there is no <select name="type">
// anywhere in the application.
func TestRequestNewTypeChooser(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/requests/new", nil, ""))

	if !strings.Contains(body, `class="type-grid"`) {
		t.Fatal("the chooser does not use the .type-grid layout")
	}
	// Five, one per store type: the deposit or guarantee got its card in the
	// 2026-09-25 fix wave (form-1), because the employee advance — the only card
	// that could carry the recoverable treatment — pays the requester.
	if n := strings.Count(body, `class="type-card"`); n != 5 {
		t.Fatalf("the chooser renders %d type cards, want 5", n)
	}
	for _, want := range []string{
		`href="/requests/new?type=vendor_invoice"`,
		`href="/requests/new?type=vendor_advance"`,
		`href="/requests/new?type=reimbursement"`,
		`href="/requests/new?type=employee_advance"`,
		`href="/requests/new?type=recoverable"`,
		"Vendor invoice payment", "Vendor advance", "Reimbursement", "Employee advance", "Deposit or guarantee",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("type card %q missing", want)
		}
	}
	// A16: the type is never a form control.
	if strings.Contains(body, `name="type"`) {
		t.Fatal("the chooser rendered a type form control; the type is a route parameter")
	}
	// D1: the screen says so, because the whole flow depends on it.
	if !strings.Contains(strings.ToLower(body), "nothing is saved until you submit") {
		t.Fatal("the chooser must state that nothing is saved until submit")
	}
	// The step-1 screen keeps its own heading on every device — the ux sweep
	// requires a visible h1 at 390px as well as 1440px.
	if !strings.Contains(body, "<h1>") {
		t.Fatal("the chooser has no h1")
	}

	// An unknown type falls back to the chooser rather than rendering a broken form.
	body = responseBody(t, s.request(http.MethodGet, "/requests/new?type=mystery", nil, ""))
	if !strings.Contains(body, `class="type-grid"`) {
		t.Fatal("an unknown type must fall back to the chooser")
	}
}

// The step-2 form is built for one type, and the treatment half of it is
// rendered by the server on every change (A16). Nothing here is decided by the
// browser: the same rules run again in validateRequestInput.
func TestRequestFormIsAdaptiveAndTypeIsNeverAControl(t *testing.T) {
	s := newAppTestServer(t)
	s.seedHead("Form")
	mgrID := seedSecondApprover(t, s)
	s.seedVendor(store.VendorInput{Name: "Sundaram Electricals Pvt Ltd", VendorType: "company", Status: "active"})
	s.login(s.cfg.AdminEmail, testAdminPassword)

	form := responseBody(t, s.request(http.MethodGet, "/requests/new?type=vendor_invoice", nil, ""))

	// T3: bank details live on the vendor record, never on a request screen.
	for _, banned := range []string{"bank account", "ifsc", "copy previous", "save draft", "save as draft"} {
		if strings.Contains(strings.ToLower(form), banned) {
			t.Fatalf("the request form exposes %q", banned)
		}
	}
	// A16: the type arrives as a route parameter and leaves as a hidden input.
	if strings.Contains(form, `<select id="rtype"`) || strings.Contains(form, `<select name="type"`) {
		t.Fatal("the form rendered a type selector; the type is a route parameter")
	}
	for _, want := range []string{
		`<input type="hidden" name="type" value="vendor_invoice">`,
		`money-field"`, `class="money-wrap"`, `class="in-words"`, `class="combo"`,
		`class="uploader"`, `class="action-bar"`, `name="short_title"`, `name="urgency_reason"`,
		`name="invoice_no"`, `name="invoice_date"`, `hx-get="/requests/new/fields"`,
		`data-when="treatment:budget"`, `data-when="urgent:on"`, "<h1>",
		`action="/requests"`, "Submit request",
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("the form is missing %q", want)
		}
	}
	// The recoverable treatment is only offered where the store accepts it.
	// store.validateRequestInput refuses it for a vendor invoice, a vendor
	// advance and a reimbursement, so presenting the choice on those types means
	// rejecting the requester after they have filled in the fields it reveals.
	// A vendor invoice states the treatment; an employee advance chooses it.
	if strings.Contains(form, `value="recoverable"`) {
		t.Fatal("the vendor invoice form offered a recoverable treatment the store refuses")
	}
	if !strings.Contains(form, `<input type="hidden" name="treatment" value="budget">`) {
		t.Fatal("the vendor invoice form must still submit a treatment")
	}
	advanceForm := responseBody(t, s.request(http.MethodGet, "/requests/new?type=employee_advance", nil, ""))
	for _, want := range []string{`class="choice"`, `value="recoverable"`, `value="budget"`} {
		if !strings.Contains(advanceForm, want) {
			t.Fatalf("the employee advance form is missing %q; it is the one type that may be recoverable", want)
		}
	}

	// G8: the requester is never in their own approver list, and somebody else is.
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	approverSelect := between(t, form, `<select id="approver"`, `</select>`)
	if strings.Contains(approverSelect, `value="`+strconvFormat(admin.ID)+`"`) {
		t.Fatalf("the requester appears in their own approver select: %s", approverSelect)
	}
	if !strings.Contains(approverSelect, `value="`+strconvFormat(mgrID)+`"`) {
		t.Fatalf("no approver is offered at all: %s", approverSelect)
	}
	// A field the browser must never be trusted to require is not marked
	// required either: a hidden required control makes the whole form
	// unsubmittable in Chrome, and the store is the authority regardless.
	if strings.Contains(form, `name="urgency_reason" required`) {
		t.Fatal("a conditionally hidden field carries the required attribute")
	}

	// Each type gets its own fieldsets and nobody else's.
	reimb := responseBody(t, s.request(http.MethodGet, "/requests/new?type=reimbursement", nil, ""))
	if !strings.Contains(reimb, `name="expense_date"`) {
		t.Fatal("reimbursement does not ask for the expense date")
	}
	for _, absent := range []string{`name="invoice_no"`, `class="combo"`} {
		if strings.Contains(reimb, absent) {
			t.Fatalf("reimbursement rendered %q, which belongs to a vendor invoice", absent)
		}
	}
	advance := responseBody(t, s.request(http.MethodGet, "/requests/new?type=employee_advance", nil, ""))
	if !strings.Contains(advance, `name="advance_reason"`) {
		t.Fatal("employee advance does not ask what the money is for")
	}
	// An employee advance opens on the recoverable treatment, so its fields are
	// the recoverable ones from the first render.
	if !strings.Contains(advance, `name="repayment_notes"`) {
		t.Fatal("employee advance did not open on the recoverable treatment")
	}

	// The fields fragment swaps on treatment, and the server decides which
	// fieldset exists — that is why the rules cannot drift from the store's.
	frag := responseBody(t, s.htmxGet("/requests/new/fields?type=vendor_invoice&treatment=recoverable&recoverable_category=icd"))
	if !strings.Contains(frag, "fieldset") {
		t.Fatalf("the fragment rendered nothing useful: %s", frag)
	}
	if !strings.Contains(frag, `name="counterparty"`) {
		t.Fatalf("ICD did not reveal the counterparty field: %s", frag)
	}
	if strings.Contains(frag, `name="head_id"`) {
		t.Fatalf("a recoverable request must not ask for a budget head: %s", frag)
	}
	frag = responseBody(t, s.htmxGet("/requests/new/fields?type=vendor_invoice&treatment=recoverable&recoverable_category=emd"))
	if !strings.Contains(frag, `name="project_id"`) {
		t.Fatal("EMD did not reveal the related-project field")
	}
	if strings.Contains(frag, `name="counterparty"`) {
		t.Fatal("EMD revealed the counterparty field, which belongs to ICD")
	}
	frag = responseBody(t, s.htmxGet("/requests/new/fields?type=vendor_invoice&treatment=budget"))
	if !strings.Contains(frag, `name="head_id"`) || !strings.Contains(frag, `name="project_id"`) {
		t.Fatalf("a budget expense must ask for a project and head: %s", frag)
	}
}

// G10: when attachments are compulsory the form asks for a written reason
// instead of blocking, because documents are sometimes genuinely unavailable.
func TestCompulsoryAttachmentsAskForAReasonRatherThanBlocking(t *testing.T) {
	s := newAppTestServer(t)
	s.seedHead("Att")
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.SetAppSetting(s.ctx, admin, "require_attachments", "1"); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	form := responseBody(t, s.request(http.MethodGet, "/requests/new?type=reimbursement", nil, ""))
	if !strings.Contains(form, `name="attachment_exception_reason"`) {
		t.Fatal("attachments are compulsory but the form never asks why one is missing")
	}
	if !strings.Contains(strings.ToLower(form), "never blocks you") {
		t.Fatal("the form does not say that a missing document is not a block")
	}
}

// A15/A16: an htmx fragment is a fragment — no shell, no document.
func TestRenderPartialOmitsTheShell(t *testing.T) {
	s := newAppTestServer(t)
	s.seedHead("Frag")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	full := responseBody(t, s.request(http.MethodGet, "/requests/new?type=vendor_invoice", nil, ""))
	if !strings.Contains(full, "<aside") {
		t.Fatal("the full page is missing the shell")
	}
	frag := responseBody(t, s.htmxGet("/requests/new/fields?type=vendor_invoice&treatment=budget"))
	for _, forbidden := range []string{"<aside", "<!doctype", "<html", `class="appshell"`, `class="tabbar"`, `class="m-topbar"`} {
		if strings.Contains(strings.ToLower(frag), forbidden) {
			t.Fatalf("the fragment carried %q: %s", forbidden, frag)
		}
	}
	if !strings.Contains(frag, "fieldset") {
		t.Fatalf("the fragment rendered nothing useful: %s", frag)
	}
}

// D1: there is exactly one submit button and exactly one POST. The request is
// created, numbered and already pending when that POST returns.
func TestRequesterCreatesAndSubmitsInOnePost(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("One")
	mgrID := seedSecondApprover(t, s)
	s.seedRequester("rhea2@example.test", "Rhea Two", "RequesterPass123")
	s.login("rhea2@example.test", "RequesterPass123")

	resp := s.postForm("/requests", url.Values{
		"type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Hyderabad site visit"},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)}, "amount": {"1,000.00"},
		"purpose": {"flight and hotel"}, "expense_date": {"2026-07-17"},
		"manager_id": {strconvFormat(mgrID)},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	loc := resp.Header.Get("Location")
	if !strings.HasSuffix(loc, "/submitted") {
		t.Fatalf("redirect = %q, want the submitted confirmation", loc)
	}
	_ = responseBody(t, resp)

	list, err := s.st.ListRequests(s.ctx, store.RequestListOptions{Scope: "all"})
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %#v, %v", list, err)
	}
	if list[0].Status != "pending" || list[0].Number == "" || list[0].SubmittedAt == nil {
		t.Fatalf("request after one POST = %+v; D1 wants it created, numbered and pending", list[0])
	}
	// The comma-grouped amount the money field writes back parses correctly.
	if list[0].Amount != 100000 {
		t.Fatalf("amount = %d, want 100000 paise", list[0].Amount)
	}

	body := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(list[0].ID)+"/submitted", nil, ""))
	for _, want := range []string{`class="banner good"`, `class="req-head"`, `class="thread"`,
		`class="pill awaiting"`, `class="waiting"`, list[0].Number, "Kavita Rao",
		`href="/requests/new"`, "<h1>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the confirmation screen is missing %q", want)
		}
	}

	// A failed submit re-renders the form with the message and keeps nothing.
	bad := s.postForm("/requests", url.Values{
		"type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {""},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)}, "amount": {"1000.00"},
		"purpose": {"x"}, "expense_date": {"2026-07-17"}, "manager_id": {strconvFormat(mgrID)},
	})
	if bad.StatusCode == http.StatusSeeOther {
		t.Fatal("a request with no short title was accepted")
	}
	badBody := responseBody(t, bad)
	if !strings.Contains(badBody, "short title") {
		t.Fatalf("the error was not shown on the form: %s", badBody)
	}
	// …and what was typed survives, so nobody retypes a form to fix one field.
	if !strings.Contains(badBody, "flight") && !strings.Contains(badBody, ">x<") {
		t.Fatalf("the rejected form lost the purpose the requester typed: %s", badBody)
	}
	again, _ := s.st.ListRequests(s.ctx, store.RequestListOptions{Scope: "all"})
	if len(again) != 1 {
		t.Fatalf("a failed submit created %d extra rows", len(again)-1)
	}
}

// A16.4: hidden is not validation. A hand-rolled POST that fills a field the
// form would never have shown is still refused by the store's own rules.
func TestAHandRolledPostCannotEscapeTheTypeRules(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Forge")
	mgrID := seedSecondApprover(t, s)
	vendorID := s.seedVendor(store.VendorInput{Name: "Anand Steel Traders", VendorType: "company", Status: "active"})
	s.seedRequester("forger@example.test", "Forger", "RequesterPass123")
	s.login("forger@example.test", "RequesterPass123")

	// A vendor invoice is a budget expense. The form never offers it any other
	// way, and posting past the form does not change that.
	resp := s.postForm("/requests", url.Values{
		"type": {"vendor_invoice"}, "treatment": {"recoverable"},
		"recoverable_category": {"icd"}, "short_title": {"Sneaky"},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)},
		"amount": {"1000.00"}, "purpose": {"x"}, "manager_id": {strconvFormat(mgrID)},
		"invoice_no": {"A/1"}, "invoice_date": {"2026-07-18"},
		"vendor_id": {strconvFormat(vendorID)},
	})
	if resp.StatusCode == http.StatusSeeOther {
		t.Fatal("a vendor invoice was accepted with recoverable treatment; the server is not authoritative")
	}
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)

	// G8: nor can a requester route a request to themselves.
	me, err := s.st.UserByEmail(s.ctx, "forger@example.test")
	if err != nil {
		t.Fatal(err)
	}
	self := s.postForm("/requests", url.Values{
		"type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Self"},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)}, "amount": {"100.00"},
		"purpose": {"x"}, "expense_date": {"2026-07-17"}, "manager_id": {strconvFormat(me.ID)},
	})
	requireStatus(t, self, http.StatusBadRequest)
	if body := responseBody(t, self); !strings.Contains(strings.ToLower(body), "cannot approve your own request") {
		t.Fatalf("self-approval was not refused in so many words: %s", body)
	}

	all, _ := s.st.ListRequests(s.ctx, store.RequestListOptions{Scope: "all"})
	if len(all) != 0 {
		t.Fatalf("%d requests exist; every one of those posts should have been refused", len(all))
	}
}

// G6: the duplicate check is a read. It renders 200 whether or not anything
// matched, POST /requests neither calls it nor consults it, and no result it
// can produce is capable of refusing a submit.
func TestDuplicateCheckWarnsAndNeverBlocks(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Dup")
	mgrID := seedSecondApprover(t, s)
	vendorID := s.seedVendor(store.VendorInput{Name: "Sundaram Electricals Pvt Ltd", VendorType: "company", Status: "active"})
	requester := s.seedRequester("dup@example.test", "Dup Requester", "RequesterPass123")

	existing, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "vendor_invoice", ShortTitle: "June switchgear", ProjectID: 1, HeadID: headID,
		Amount: 10000000, Purpose: "panels", ManagerID: mgrID, VendorID: vendorID,
		InvoiceNo: "SE/26-27/1102", InvoiceDate: "2026-06-28"})
	if err != nil {
		t.Fatal(err)
	}
	orig, err := s.st.Request(s.ctx, existing)
	if err != nil {
		t.Fatal(err)
	}

	s.login("dup@example.test", "RequesterPass123")
	body := responseBody(t, s.postFormHX("/requests/duplicate-check", url.Values{
		"type": {"vendor_invoice"}, "vendor_id": {strconvFormat(vendorID)},
		"amount": {"1,00,000.00"}, "invoice_no": {"SE/26-27/1184"}}))
	if !strings.Contains(body, `class="banner warn"`) {
		t.Fatalf("the duplicate warning is not a .banner.warn: %s", body)
	}
	if !strings.Contains(body, orig.Number) {
		t.Fatalf("the existing request is not listed: %s", body)
	}
	if strings.Contains(body, "<aside") || strings.Contains(strings.ToLower(body), "<!doctype") {
		t.Fatalf("the duplicate fragment carried the shell: %s", body)
	}
	// The wording must not promise a block.
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "you can still") && !strings.Contains(lower, "check before you submit") {
		t.Fatalf("the warning does not say the submit may still proceed: %s", body)
	}

	// Nothing similar: 200 and an empty fragment, never a 4xx.
	empty := s.postFormHX("/requests/duplicate-check", url.Values{"type": {"vendor_invoice"},
		"vendor_id": {strconvFormat(vendorID)}, "amount": {"3.00"}})
	requireStatus(t, empty, http.StatusOK)
	if got := strings.TrimSpace(responseBody(t, empty)); got != "" {
		t.Fatalf("no-match fragment = %q, want empty", got)
	}

	// G6: the submit goes through anyway, and creates a second request.
	resp := s.postForm("/requests", url.Values{
		"type": {"vendor_invoice"}, "treatment": {"budget"}, "short_title": {"July switchgear"},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)}, "amount": {"1,00,000.00"},
		"purpose": {"panels"}, "vendor_id": {strconvFormat(vendorID)},
		"invoice_no": {"SE/26-27/1184"}, "invoice_date": {"2026-07-18"},
		"manager_id": {strconvFormat(mgrID)},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	all, _ := s.st.ListRequests(s.ctx, store.RequestListOptions{Scope: "all"})
	if len(all) != 2 {
		t.Fatalf("the duplicate warning blocked the submit: %d requests exist, want 2", len(all))
	}

	// The form carries the wiring that asks for the check, and somewhere to put it.
	form := responseBody(t, s.request(http.MethodGet, "/requests/new?type=vendor_invoice", nil, ""))
	for _, want := range []string{`hx-post="/requests/duplicate-check"`, `id="dup-check"`} {
		if !strings.Contains(form, want) {
			t.Fatalf("the form is missing the duplicate-check wiring %q", want)
		}
	}
}

// A reimbursement carries no vendor row, so its payee is the person raising it.
// The check has to know that, or half the request types never get checked.
func TestDuplicateCheckKnowsAReimbursementPaysItsRequester(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Self")
	mgrID := seedSecondApprover(t, s)
	requester := s.seedRequester("claimer@example.test", "Claire Claimer", "RequesterPass123")
	if _, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "reimbursement", ShortTitle: "Hyderabad flights", ProjectID: 1, HeadID: headID,
		Amount: 1840000, Purpose: "travel", ExpenseDate: "2026-07-17", ManagerID: mgrID}); err != nil {
		t.Fatal(err)
	}

	s.login("claimer@example.test", "RequesterPass123")
	body := responseBody(t, s.postFormHX("/requests/duplicate-check", url.Values{
		"type": {"reimbursement"}, "amount": {"18,400.00"}}))
	if !strings.Contains(body, "Hyderabad flights") {
		t.Fatalf("a repeat reimbursement went unnoticed: %q", body)
	}
}

// The "waiting on" line is the signature element of the design: one plain
// sentence naming who owes the next action, computed once so the list, the
// approvals queue and the detail head can never disagree.
func TestWaitingOnNamesWhoeverOwesTheNextAction(t *testing.T) {
	const me, them = int64(7), int64(9)
	req := func(status string, requester, manager int64) store.Request {
		return store.Request{Status: status, RequesterID: requester, ManagerID: manager,
			RequesterName: "Rhea", ManagerName: "Kavita"}
	}
	for _, tc := range []struct {
		name      string
		req       store.Request
		wantText  string
		wantClass string
	}{
		{"pending on me", req("pending", them, me), "Waiting on you", "you"},
		{"pending on someone else", req("pending", me, them), "Waiting on Kavita", ""},
		{"returned to me", req("returned", me, them), "Waiting on you", "you"},
		{"returned to someone else", req("returned", them, me), "Waiting on Rhea", ""},
		{"approved", req("approved", me, them), "Waiting on Accounts", ""},
		{"cancellation on me", req("cancellation_requested", them, me), "Waiting on you", "you"},
		{"rejected", req("rejected", me, them), "Closed. Raise a new request if needed", "closed"},
		{"withdrawn", req("withdrawn", me, them), "Withdrawn by the requester", "closed"},
		{"cancelled", req("cancelled", me, them), "Cancelled. Nothing can be paid against it", "closed"},
	} {
		got := waitingOn(tc.req, me)
		if got.Text != tc.wantText || got.Class != tc.wantClass {
			t.Errorf("%s: waitingOn = %q/%q, want %q/%q", tc.name, got.Text, got.Class, tc.wantText, tc.wantClass)
		}
	}
}

func TestRequestsListRendersCardsTabsAndWaitingLine(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("List")
	mgrID := seedSecondApprover(t, s)
	mgr, err := s.st.UserByID(s.ctx, mgrID)
	if err != nil {
		t.Fatal(err)
	}
	requester := s.seedRequester("lister@example.test", "Lister", "RequesterPass123")
	mk := func(title string) int64 {
		id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
			Type: "reimbursement", ShortTitle: title, ProjectID: 1, HeadID: headID, Amount: 1000,
			Purpose: "p", ExpenseDate: "2026-07-17", ManagerID: mgrID})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	mk("Still waiting")
	returned := mk("Sent back to me")
	if err := s.st.ReturnRequest(s.ctx, mgr, returned, "attach the receipt"); err != nil {
		t.Fatal(err)
	}

	s.login("lister@example.test", "RequesterPass123")
	body := responseBody(t, s.request(http.MethodGet, "/requests", nil, ""))
	for _, want := range []string{
		`class="req-list"`, `class="req-card`, `class="rc-no"`, `class="rc-amt"`,
		`class="rc-title"`, `class="rc-meta"`, `class="rc-foot"`,
		`class="segmented"`, `class="m-filters"`, `id="filter-sheet"`, `class="overlay"`,
		"Still waiting", "Sent back to me", "<h1>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the requests list is missing %q", want)
		}
	}
	// D5: the design system removed .badge entirely.
	if strings.Contains(body, `class="badge`) {
		t.Fatal("the list still renders .badge; the design system uses .pill")
	}
	// The returned request says it is waiting on the viewer; the pending one is not.
	if !strings.Contains(body, `class="waiting you"`) {
		t.Fatalf("no 'waiting on you' line for the returned request: %s", body)
	}
	if !strings.Contains(body, "Waiting on Kavita Rao") {
		t.Fatal("the pending request does not name its approver")
	}
	// The tabs carry counts that come from the same SQL as the rows.
	if !strings.Contains(body, `href="/requests?bucket=needs-me`) {
		t.Fatal("the Needs me tab is missing")
	}
	needsMe := responseBody(t, s.request(http.MethodGet, "/requests?bucket=needs-me", nil, ""))
	if !strings.Contains(needsMe, "Sent back to me") || strings.Contains(needsMe, "Still waiting") {
		t.Fatalf("the Needs me bucket listed the wrong requests: %s", needsMe)
	}
	closed := responseBody(t, s.request(http.MethodGet, "/requests?bucket=closed", nil, ""))
	if strings.Contains(closed, "Still waiting") || strings.Contains(closed, "Sent back to me") {
		t.Fatal("an open request was listed under Closed")
	}
	if !strings.Contains(closed, `class="empty"`) {
		t.Fatal("an empty bucket says nothing at all")
	}

	// Q5: the list is scoped. Another requester sees none of it.
	s.seedRequester("notlister@example.test", "Not Lister", "OtherPass1234")
	s.login("notlister@example.test", "OtherPass1234")
	other := responseBody(t, s.request(http.MethodGet, "/requests", nil, ""))
	if strings.Contains(other, "Still waiting") {
		t.Fatal("a requester can see another person's requests in the list")
	}
}

// A15: the manager queue is its own screen, with its own tabs and its own
// empty state. Folding it into /requests?scope=assigned made the segmented
// tabs mean two things at once.
func TestApprovalsQueueIsItsOwnScreen(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Appr")
	mgrID := seedSecondApprover(t, s)
	mgr, err := s.st.UserByID(s.ctx, mgrID)
	if err != nil {
		t.Fatal(err)
	}
	requester := s.seedRequester("appreq@example.test", "Appr Requester", "RequesterPass123")
	mk := func(title string) int64 {
		id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
			Type: "reimbursement", ShortTitle: title, ProjectID: 1, HeadID: headID, Amount: 18400,
			Purpose: "p", ExpenseDate: "2026-07-17", ManagerID: mgrID})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	mk("Travel reimbursement")
	frozen := mk("Binding wire advance")
	if err := s.st.ApproveRequest(s.ctx, mgr, frozen, 18400, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.st.RequestCancellation(s.ctx, requester, frozen, "order withdrawn"); err != nil {
		t.Fatal(err)
	}

	s.login("kavita@example.test", "ApproverPass123")
	body := responseBody(t, s.request(http.MethodGet, "/approvals", nil, ""))
	for _, want := range []string{`class="segmented"`, `class="req-list"`, `class="req-card`,
		"Travel reimbursement", `class="waiting you"`, "<h1>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("approvals queue is missing %q", want)
		}
	}
	// A6: no bulk approval anywhere on the screen.
	if strings.Contains(body, `type="checkbox"`) || strings.Contains(strings.ToLower(body), "approve selected") {
		t.Fatal("the approvals queue offers bulk approval")
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("the approvals queue still renders .badge")
	}
	// The cancellation tab is a first-class part of the queue (G1).
	if !strings.Contains(body, `href="/approvals?bucket=cancellations`) {
		t.Fatal("no cancellations tab")
	}
	cancel := responseBody(t, s.request(http.MethodGet, "/approvals?bucket=cancellations", nil, ""))
	if !strings.Contains(cancel, "Binding wire advance") || strings.Contains(cancel, "Travel reimbursement") {
		t.Fatalf("the cancellations tab listed the wrong requests: %s", cancel)
	}

	// A requester without approval:approve cannot reach the queue at all.
	s.login("appreq@example.test", "RequesterPass123")
	requireStatus(t, s.request(http.MethodGet, "/approvals", nil, ""), http.StatusForbidden)
}

// A19: the two detail mockups are one page. Only the action bar changes, and it
// changes on permission, never on a role name and never on which URL you came
// from. History and conversation are a single merged .thread.
func TestRequestDetailIsOneScreenWithPermissionGatedActions(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Detail")
	mgrID := seedSecondApprover(t, s)
	mgr, err := s.st.UserByID(s.ctx, mgrID)
	if err != nil {
		t.Fatal(err)
	}
	vendorID := s.seedVendor(store.VendorInput{Name: "Meridian Facility Services",
		VendorType: "company", Status: "active"})
	requester := s.seedRequester("sneha@example.test", "Sneha Pillai", "RequesterPass123")
	s.grantAlso(requester.ID, "Requester who may cancel",
		[]store.Grant{{Resource: "request", Action: "cancel"}},
		[]store.ScopeGrant{{Resource: "request", Scope: "own"}})
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "vendor_invoice", ShortTitle: "July housekeeping", ProjectID: 1, HeadID: headID,
		Amount: 23500000, Purpose: "monthly contract", ManagerID: mgrID, VendorID: vendorID,
		InvoiceNo: "MFS/26-27/0912", InvoiceDate: "2026-07-22"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.AddRequestComment(s.ctx, requester, id, "Same as June."); err != nil {
		t.Fatal(err)
	}

	// --- The approver sees the decision controls. ---
	s.login("kavita@example.test", "ApproverPass123")
	body := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, ""))
	for _, want := range []string{
		`class="req-head"`, `class="rh-no"`, `class="rh-amt"`, `class="rh-status"`,
		`class="pill awaiting"`, `class="waiting you"`, `class="dl"`, `class="thread"`,
		`class="comment-box"`, `class="action-bar"`,
		`data-open="approve-sheet"`, `data-open="return-sheet"`, `data-open="reject-sheet"`,
		`id="approve-sheet"`, `class="overlay"`, `class="sheet"`,
		"MFS/26-27/0912", "Meridian Facility Services", "<h1>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("approver detail view is missing %q", want)
		}
	}
	// A19: one merged stream, not two lists.
	if strings.Count(body, `class="thread"`) != 1 {
		t.Fatal("the detail page renders more than one thread")
	}
	if strings.Contains(body, "<h2>Conversation</h2>") && strings.Contains(body, "<h2>History</h2>") {
		t.Fatal("history and conversation are still two separate lists")
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("the detail page still renders .badge")
	}
	// The submit event and the comment are both in the one stream.
	if !strings.Contains(body, "submitted request") || !strings.Contains(body, "Same as June.") {
		t.Fatalf("the thread is missing an event or a comment: %s", body)
	}
	// money.FormatPaise already carries the rupee sign, so the approve sheet's
	// .money-field must not print a second one behind its own .cur prefix.
	if strings.Contains(body, `value="₹`) {
		t.Fatal("a money-field input carries a second rupee sign")
	}

	// --- The requester sees the same page, with a different action bar. ---
	s.login("sneha@example.test", "RequesterPass123")
	body = responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, ""))
	if strings.Contains(body, `data-open="approve-sheet"`) {
		t.Fatal("the requester is offered an approve control on their own request")
	}
	for _, want := range []string{`class="req-head"`, `class="thread"`,
		`href="/requests/` + strconvFormat(id) + `/edit"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("requester detail view is missing %q", want)
		}
	}

	// --- Approved: the requester is locked out of editing and offered cancellation. ---
	if err := s.st.ApproveRequest(s.ctx, mgr, id, 23500000, "ok"); err != nil {
		t.Fatal(err)
	}
	body = responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, ""))
	if strings.Contains(body, `href="/requests/`+strconvFormat(id)+`/edit"`) {
		t.Fatal("an approved request still offers an edit link")
	}
	for _, want := range []string{`class="banner locked"`, `href="/requests/` + strconvFormat(id) + `/cancel"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("approved detail view is missing %q", want)
		}
	}

	// --- The three shipped screens that already link here now resolve. ---
	for _, path := range []string{"/requests/" + strconvFormat(id), "/requests/" + strconvFormat(id) + "/submitted"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusOK)
		_ = responseBody(t, resp)
	}

	// --- A third party in neither seat cannot read it at all (Q5/R6). ---
	// 404, not 403: an out-of-scope row answers exactly as a non-existent one
	// does, or the status code is an existence oracle for the whole id space
	// (F-G-002). TestOutOfScopeRequestIsIndistinguishableFromAMissingOne pins the
	// pair.
	s.seedRequester("nosy@example.test", "Nosy Parker", "OtherPass1234")
	s.login("nosy@example.test", "OtherPass1234")
	requireStatus(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, ""), http.StatusNotFound)
}

// The decisions the detail page's sheets post, through HTTP rather than through
// the store, so the routes, the permission gates and the form field names are
// all exercised the way a browser reaches them.
func TestRequestDecisionsThroughTheDetailScreen(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Decide")
	mgrID := seedSecondApprover(t, s)
	requester := s.seedRequester("deciding@example.test", "Deciding Requester", "RequesterPass123")
	mk := func(title string) int64 {
		id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
			Type: "reimbursement", ShortTitle: title, ProjectID: 1, HeadID: headID, Amount: 500000,
			Purpose: "p", ExpenseDate: "2026-07-17", ManagerID: mgrID})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	approve, returned, rejected, withdrawn := mk("To approve"), mk("To return"), mk("To reject"), mk("To withdraw")

	// The requester withdraws their own; nobody else's controls are offered.
	s.login("deciding@example.test", "RequesterPass123")
	resp := s.postForm("/requests/"+strconvFormat(withdrawn)+"/withdraw", url.Values{})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if got, _ := s.st.Request(s.ctx, withdrawn); got.Status != "withdrawn" {
		t.Fatalf("withdraw left status %q", got.Status)
	}
	// A comment from the requester lands on the shared thread.
	resp = s.postForm("/requests/"+strconvFormat(approve)+"/comment", url.Values{"body": {"Receipts attached."}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	s.login("kavita@example.test", "ApproverPass123")
	// An adjusted approval is the point of the amount field in the sheet.
	resp = s.postForm("/requests/"+strconvFormat(approve)+"/approve",
		url.Values{"approved_amount": {"4,000.00"}, "note": {"Cut the cab fare"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	got, _ := s.st.Request(s.ctx, approve)
	if got.Status != "approved" || got.ApprovedAmount == nil || *got.ApprovedAmount != 400000 {
		t.Fatalf("approval = %q / %v, want approved at 400000", got.Status, got.ApprovedAmount)
	}
	// Return and reject both demand words, and refuse without them.
	if resp := s.postForm("/requests/"+strconvFormat(returned)+"/return", url.Values{"comment": {""}}); resp.StatusCode == http.StatusSeeOther {
		t.Fatal("a return with no comment was accepted")
	}
	resp = s.postForm("/requests/"+strconvFormat(returned)+"/return", url.Values{"comment": {"Attach the receipt"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if resp := s.postForm("/requests/"+strconvFormat(rejected)+"/reject", url.Values{"reason": {""}}); resp.StatusCode == http.StatusSeeOther {
		t.Fatal("a rejection with no reason was accepted")
	}
	resp = s.postForm("/requests/"+strconvFormat(rejected)+"/reject", url.Values{"reason": {"Not budgeted"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if got, _ := s.st.Request(s.ctx, rejected); got.Status != "rejected" {
		t.Fatalf("reject left status %q", got.Status)
	}

	// A rejected request is raised again as a new pending one (D1: never a draft).
	s.login("deciding@example.test", "RequesterPass123")
	resp = s.postForm("/requests/"+strconvFormat(rejected)+"/reraise", url.Values{})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	fresh, _ := s.st.ListRequests(s.ctx, store.RequestListOptions{Scope: "own", ViewerID: requester.ID,
		Statuses: []string{"pending"}})
	if len(fresh) != 1 || fresh[0].ID == rejected {
		t.Fatalf("re-raise did not produce one new pending request: %+v", fresh)
	}
}

// The edit screen's job is to promise, before the person commits, the three
// things the store already does: record the change, tell the approver again,
// and restart the reminder clock. Changing the approver moves the request.
func TestRequestEditScreenAndReroute(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Edit")
	mgrID := seedSecondApprover(t, s)
	other := seedThirdApprover(t, s)
	requester := s.seedRequester("arun@example.test", "Arun Mehta", "RequesterPass123")
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "reimbursement", ShortTitle: "Hyderabad site visit", ProjectID: 1, HeadID: headID,
		Amount: 1690000, Purpose: "flight and hotel", ExpenseDate: "2026-07-17", ManagerID: mgrID})
	if err != nil {
		t.Fatal(err)
	}

	s.login("arun@example.test", "RequesterPass123")
	body := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id)+"/edit", nil, ""))
	for _, want := range []string{`class="banner info"`, ` money-field"`, `class="in-words"`,
		`class="action-bar"`, `name="manager_id"`, `value="16,900.00"`, "restarts", "<h1>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("edit screen is missing %q", want)
		}
	}
	// money.FormatPaise carries the rupee sign; the .money-field draws its own.
	if strings.Contains(body, `value="₹`) {
		t.Fatal("the amount field carries a second rupee sign")
	}
	// A16: the type is still not editable — it is what the request is.
	if strings.Contains(body, `name="type"`) && !strings.Contains(body, `type="hidden" name="type"`) {
		t.Fatal("the edit screen offers a type control rather than carrying the route parameter")
	}
	// Nobody else may open somebody's edit form, whatever they may read.
	s.login("kavita@example.test", "ApproverPass123")
	requireStatus(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id)+"/edit", nil, ""), http.StatusForbidden)

	s.login("arun@example.test", "RequesterPass123")
	resp := s.postForm("/requests/"+strconvFormat(id)+"/edit", url.Values{
		"type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Hyderabad site visit"},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)}, "amount": {"18,400.00"},
		"purpose": {"flight, hotel and cabs"}, "expense_date": {"2026-07-17"},
		"manager_id": {strconvFormat(other)},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	got, _ := s.st.Request(s.ctx, id)
	if got.Amount != 1840000 || got.ManagerID != other {
		t.Fatalf("edit not applied: amount=%d manager=%d", got.Amount, got.ManagerID)
	}
	if got.ReminderLastSent != nil {
		t.Fatal("the reminder clock was not restarted")
	}
	// The change is on the merged thread with a before and an after.
	thread, _ := s.st.RequestThread(s.ctx, id)
	var sawDiff bool
	for _, e := range thread {
		if e.Action == "update" && len(e.Changes) > 0 {
			sawDiff = true
		}
	}
	if !sawDiff {
		t.Fatal("the edit is not visible as a change in the thread")
	}

	// A rejected edit puts back what was typed rather than what is stored.
	resp = s.postForm("/requests/"+strconvFormat(id)+"/edit", url.Values{
		"type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Renamed in the browser"},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)}, "amount": {"19,000.00"},
		"purpose": {""}, "expense_date": {"2026-07-17"}, "manager_id": {strconvFormat(other)},
	})
	requireStatus(t, resp, http.StatusBadRequest)
	rejected := responseBody(t, resp)
	if !strings.Contains(rejected, "Renamed in the browser") || !strings.Contains(rejected, `value="19,000.00"`) {
		t.Fatalf("a rejected edit lost the typing: %s", rejected)
	}
}

func seedThirdApprover(t *testing.T, s *appTestServer) int64 {
	t.Helper()
	hash, err := auth.HashPassword("ApproverPass456")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.st.CreateUser(s.ctx, "rakesh@example.test", "Rakesh Iyer", hash, "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	s.assignRole(id, "Manager")
	return id
}

// Returned is not rejected. A returned request keeps its number and its
// history — the requester corrects it and sends it again — so the correction
// form lives on the same URL as the detail, above the same thread.
func TestReturnedRequestScreenCorrectsAndResubmits(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Ret")
	mgrID := seedSecondApprover(t, s)
	mgr, err := s.st.UserByID(s.ctx, mgrID)
	if err != nil {
		t.Fatal(err)
	}
	vendorID := s.seedVendor(store.VendorInput{Name: "Kaveri Logistics", VendorType: "company", Status: "active"})
	requester := s.seedRequester("ret@example.test", "Ret Requester", "RequesterPass123")
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "vendor_invoice", ShortTitle: "July freight", ProjectID: 1, HeadID: headID,
		Amount: 6450000, Purpose: "freight", ManagerID: mgrID, VendorID: vendorID,
		InvoiceNo: "KL/2026/0788", InvoiceDate: "2026-06-28"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.ReturnRequest(s.ctx, mgr, id, "The invoice attached is the June one."); err != nil {
		t.Fatal(err)
	}
	before, _ := s.st.Request(s.ctx, id)

	s.login("ret@example.test", "RequesterPass123")
	body := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, ""))
	for _, want := range []string{
		`class="pill returned"`, `class="banner warn"`, "The invoice attached is the June one.",
		`class="thread"`, `action="/requests/` + strconvFormat(id) + `/edit"`,
		"Resubmit for approval", before.Number, "<h1>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("returned screen is missing %q", want)
		}
	}
	// One sticky action bar, not one per form: two would be two competing
	// primary actions at the bottom of a phone screen.
	if n := strings.Count(body, `class="action-bar"`); n != 1 {
		t.Fatalf("the returned screen renders %d action bars, want 1", n)
	}

	// The approver looking at the same URL gets the read-only detail, not a
	// correction form they could never submit.
	s.login("kavita@example.test", "ApproverPass123")
	managerView := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, ""))
	if strings.Contains(managerView, "Resubmit for approval") {
		t.Fatal("the approver is offered the requester's correction form")
	}

	// Correct and resubmit in one press: the number survives and the status
	// goes back to pending.
	s.login("ret@example.test", "RequesterPass123")
	resp := s.postForm("/requests/"+strconvFormat(id)+"/edit", url.Values{
		"type": {"vendor_invoice"}, "treatment": {"budget"}, "short_title": {"July freight"},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)}, "amount": {"64,500.00"},
		"purpose": {"freight"}, "vendor_id": {strconvFormat(vendorID)},
		"invoice_no": {"KL/2026/0812"}, "invoice_date": {"2026-07-21"},
		"manager_id": {strconvFormat(mgrID)}, "submit_action": {"resubmit"},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	after, _ := s.st.Request(s.ctx, id)
	if after.Status != "pending" || after.Number != before.Number || after.InvoiceNo != "KL/2026/0812" {
		t.Fatalf("after correction and resubmit = %+v", after)
	}
}

// G1, G2, G3: an approved request cannot be withdrawn on your own. The
// requester asks, payment freezes at that moment, and the approver accepts or
// declines — or cancels outright with a reason, without being asked.
func TestCancellationScreensEndToEnd(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Cancel")
	mgrID := seedSecondApprover(t, s)
	mgr, err := s.st.UserByID(s.ctx, mgrID)
	if err != nil {
		t.Fatal(err)
	}
	// Neither seeded role carries the cancellation verbs: request:cancel and
	// approval:cancel entered the vocabulary with this flow and Phase 1's
	// starter roles were never back-filled. An administrator grants them on the
	// Roles screen; this does the same thing directly.
	s.grantAlso(mgrID, "Approver who may cancel",
		[]store.Grant{{Resource: "approval", Action: "cancel"}},
		[]store.ScopeGrant{{Resource: "request", Scope: "all"}})
	vendorID := s.seedVendor(store.VendorInput{Name: "Anand Steel Traders", VendorType: "company", Status: "active"})
	requester := s.seedRequester("cancelreq@example.test", "Cancel Requester", "RequesterPass123")
	s.grantAlso(requester.ID, "Requester who may ask for cancellation",
		[]store.Grant{{Resource: "request", Action: "cancel"}},
		[]store.ScopeGrant{{Resource: "request", Scope: "own"}})
	mk := func(title string, amount int64) int64 {
		id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
			Type: "vendor_advance", ShortTitle: title, ProjectID: 1, HeadID: headID, Amount: amount,
			Purpose: "advance", ManagerID: mgrID, VendorID: vendorID,
			AdvanceReason: "40% booking against PO-2026-0417"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.st.ApproveRequest(s.ctx, mgr, id, amount, ""); err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := mk("Binding wire order", 4700000)

	// --- The employee asks. ---
	s.login("cancelreq@example.test", "RequesterPass123")
	body := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id)+"/cancel", nil, ""))
	for _, want := range []string{`class="banner warn"`, "Payment freezes", `class="req-head"`,
		`name="reason"`, `class="action-bar"`, "<h1>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("cancel screen is missing %q", want)
		}
	}
	if resp := s.postForm("/requests/"+strconvFormat(id)+"/cancel-request", url.Values{"reason": {""}}); resp.StatusCode == http.StatusSeeOther {
		t.Fatal("a cancellation with no reason was accepted")
	}
	resp := s.postForm("/requests/"+strconvFormat(id)+"/cancel-request", url.Values{"reason": {"Site cancelled the order"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if frozen, _ := s.st.Request(s.ctx, id); frozen.Status != "cancellation_requested" {
		t.Fatalf("status = %q, want cancellation_requested", frozen.Status)
	}
	// The requester sees the freeze on the detail page.
	detail := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, ""))
	if !strings.Contains(detail, `class="pill cancelreq"`) || !strings.Contains(detail, "Payment is frozen") {
		t.Fatalf("the frozen state is not shown to the requester: %s", detail)
	}
	// The requester cannot decide their own cancellation.
	requireStatus(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id)+"/cancellation", nil, ""), http.StatusForbidden)

	// --- The approver decides. ---
	s.login("kavita@example.test", "ApproverPass123")
	body = responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id)+"/cancellation", nil, ""))
	for _, want := range []string{"Site cancelled the order", `data-open="accept-sheet"`,
		`data-open="decline-sheet"`, `class="overlay"`, `class="sheet"`, `class="thread"`, "<h1>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("cancellation decision screen is missing %q", want)
		}
	}
	// Declining requires a reason and unfreezes.
	if resp := s.postForm("/requests/"+strconvFormat(id)+"/cancellation", url.Values{"decision": {"decline"}, "note": {""}}); resp.StatusCode == http.StatusSeeOther {
		t.Fatal("a decline with no reason was accepted")
	}
	resp = s.postForm("/requests/"+strconvFormat(id)+"/cancellation", url.Values{"decision": {"decline"}, "note": {"Vendor already dispatched"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if got, _ := s.st.Request(s.ctx, id); got.Status != "approved" {
		t.Fatalf("declined cancellation left status %q, want approved", got.Status)
	}
	// Accepting closes it.
	if err := s.st.RequestCancellation(s.ctx, requester, id, "Order withdrawn"); err != nil {
		t.Fatal(err)
	}
	resp = s.postForm("/requests/"+strconvFormat(id)+"/cancellation", url.Values{"decision": {"accept"}, "note": {"Agreed"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if got, _ := s.st.Request(s.ctx, id); got.Status != "cancelled" {
		t.Fatalf("accepted cancellation left status %q, want cancelled", got.Status)
	}

	// --- G2: the approver cancels a different request outright. ---
	other := mk("Second order", 100000)
	outright := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(other)+"/cancellation", nil, ""))
	if !strings.Contains(outright, `data-open="outright-sheet"`) {
		t.Fatal("an approved request offers no outright cancellation to its approver")
	}
	if resp := s.postForm("/requests/"+strconvFormat(other)+"/cancel", url.Values{"reason": {""}}); resp.StatusCode == http.StatusSeeOther {
		t.Fatal("an outright cancellation with no reason was accepted")
	}
	resp = s.postForm("/requests/"+strconvFormat(other)+"/cancel", url.Values{"reason": {"Budget pulled for the quarter"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if got, _ := s.st.Request(s.ctx, other); got.Status != "cancelled" || got.CancelReason == "" {
		t.Fatalf("outright cancel = %+v", got)
	}
}

// D6/G21: one screen, one form, one fieldset per section, one generic
// app_settings-backed save. Phases 3-5 append a ConfigSection and touch nothing
// else — if a later phase has to edit the handler, this task got the shape
// wrong, so the test asserts the shape as much as the behaviour.
func TestConfigurationScreenReadsAndWritesAppSettings(t *testing.T) {
	s := newAppTestServer(t)

	// Permission-gated by URL, not by menu-hiding.
	s.seedRequester("noconfig@example.test", "No Config", "RequesterPass123")
	s.login("noconfig@example.test", "RequesterPass123")
	requireStatus(t, s.request(http.MethodGet, "/configuration", nil, ""), http.StatusForbidden)
	requireStatus(t, s.postForm("/configuration", url.Values{"number_prefix": {"HACK"}}), http.StatusForbidden)
	if v, _ := s.st.AppSetting(s.ctx, "number_prefix"); v != "PR" {
		t.Fatalf("an unprivileged POST changed a setting: %q", v)
	}

	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/configuration", nil, ""))
	for _, want := range []string{
		"<legend>Request numbering</legend>", "<legend>Attachments</legend>",
		"<legend>Urgency</legend>", "<legend>Approvals</legend>", "<legend>Payments</legend>",
		`name="number_prefix"`, `name="require_attachments"`, `name="urgency_mode"`,
		`name="allow_approver_choice"`, `class="action-bar"`,
		"<h1>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("configuration screen is missing %q", want)
		}
	}
	// No "allow direct payments" toggle (recoverables-10): nothing read the
	// key, paymentCreate refuses a request-less payment regardless (X5), and its
	// hint promised a reason and an audit flag that did not exist.
	if strings.Contains(body, `name="allow_direct_payments"`) || strings.Contains(body, "Allow direct payments") {
		t.Fatal("the configuration screen offers a direct-payments toggle that nothing reads")
	}
	// One form of fieldsets closed by one action bar: that is the shape later
	// phases append to.
	if n := strings.Count(body, `class="action-bar"`); n != 1 {
		t.Fatalf("the configuration screen has %d action bars, want 1", n)
	}
	if n := strings.Count(body, `action="/configuration"`); n != 1 {
		t.Fatalf("the configuration screen posts from %d forms, want 1", n)
	}
	// The self-approval control exists and cannot be switched off.
	if !strings.Contains(body, "Block self-approval") || !strings.Contains(body, "disabled") {
		t.Fatal("the self-approval control must render checked and disabled")
	}
	if strings.Contains(body, `name="block_self_approval"`) {
		t.Fatal("self-approval was rendered as a writable setting; it is structural")
	}

	// Saving writes every posted key in one transaction.
	resp := s.postForm("/configuration", url.Values{
		"number_prefix": {"REQ"}, "number_year_mode": {"financial"}, "number_width": {"5"},
		"require_attachments": {"on"}, "attachment_max_mb": {"20"},
		"urgency_mode": {"free"}, "allow_approver_choice": {""},
		"payment_modes": {"NEFT, UPI"},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	settings, err := s.st.AppSettings(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"number_prefix": "REQ", "number_year_mode": "financial", "number_width": "5",
		"require_attachments": "1", "attachment_max_mb": "20", "urgency_mode": "free",
		"allow_approver_choice": "0", "payment_modes": "NEFT, UPI",
	} {
		if settings[k] != want {
			t.Fatalf("app_settings[%q] = %q, want %q", k, settings[k], want)
		}
	}
	// The change is audited (C2).
	audit, err := s.st.Audit(s.ctx, "app_setting", 0, 5)
	if err != nil || len(audit) == 0 {
		t.Fatalf("configuration save was not audited: %#v, %v", audit, err)
	}
	// And it is shown back, so the screen is a reading of the stored rules.
	if !strings.Contains(responseBody(t, s.request(http.MethodGet, "/configuration", nil, "")), `value="REQ"`) {
		t.Fatal("the saved prefix is not shown back")
	}

	// An unknown key posted by hand is ignored, not stored.
	resp = s.postForm("/configuration", url.Values{"number_prefix": {"REQ"}, "smtp_password": {"hunter2"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if v, _ := s.st.AppSetting(s.ctx, "smtp_password"); v != "" {
		t.Fatal("an unregistered key was written from the form")
	}

	// Every field a section declares is one the vocabulary and the store agree
	// on: a Kind the template cannot render is a control nobody can use.
	for _, section := range configSections {
		if section.Title == "" || len(section.Fields) == 0 {
			t.Fatalf("config section %#v has no title or no fields", section)
		}
		for _, f := range section.Fields {
			switch f.Kind {
			case "text", "number", "toggle", "select":
			default:
				t.Fatalf("config field %q declares unknown kind %q", f.Key, f.Kind)
			}
			if f.Kind == "select" && len(f.Options) == 0 {
				t.Fatalf("config select %q offers no options", f.Key)
			}
			if f.Span < 1 || f.Span > 12 {
				t.Fatalf("config field %q spans %d columns", f.Key, f.Span)
			}
		}
	}
}

// A17: the dashboard is a metric strip plus .work-areas. The strip tells you a
// number; the work area hands you the thing to do. Both are gated on the same
// permissions the nav is, so a person is never shown a queue they cannot open.
func TestDashboardShowsGatedWorkAreasWithCounts(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Dash")
	mgrID := seedSecondApprover(t, s)
	mgr, err := s.st.UserByID(s.ctx, mgrID)
	if err != nil {
		t.Fatal(err)
	}
	requester := s.seedRequester("dashreq@example.test", "Dash Req", "RequesterPass123")
	mk := func(title string, amount int64) int64 {
		id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
			Type: "reimbursement", ShortTitle: title, ProjectID: 1, HeadID: headID, Amount: amount,
			Purpose: "p", ExpenseDate: "2026-07-17", ManagerID: mgrID})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	mk("Team lunch", 100000)
	sentBack := mk("Cab receipts", 200000)
	if err := s.st.ReturnRequest(s.ctx, mgr, sentBack, "attach the receipt"); err != nil {
		t.Fatal(err)
	}

	// --- Requester. ---
	s.login("dashreq@example.test", "RequesterPass123")
	body := responseBody(t, s.request(http.MethodGet, "/dashboard", nil, ""))
	for _, want := range []string{
		`class="work-areas"`, `class="area"`, `class="a-head"`, `class="a-list"`, `class="a-foot"`,
		`class="metric-strip"`, "Needs your action", "Cab receipts", `class="pill returned"`, "<h1>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("requester dashboard is missing %q", want)
		}
	}
	if strings.Contains(body, "Administration") {
		t.Fatal("requester dashboard must not show Administration")
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("the dashboard still renders .badge")
	}
	// The area rows link straight to the thing to do.
	if !strings.Contains(body, `href="/requests/`+strconvFormat(sentBack)+`"`) {
		t.Fatal("the needs-action area does not link to the request")
	}

	// --- Approver sees their own areas. ---
	s.login("kavita@example.test", "ApproverPass123")
	body = responseBody(t, s.request(http.MethodGet, "/dashboard", nil, ""))
	for _, want := range []string{"Awaiting your approval", "Team lunch", `href="/approvals"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("approver dashboard is missing %q", want)
		}
	}

	// --- Admin sees the administration area. ---
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body = responseBody(t, s.request(http.MethodGet, "/dashboard", nil, ""))
	for _, want := range []string{"Configuration", `href="/configuration"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin dashboard is missing %q", want)
		}
	}

	// --- Somebody with nothing waiting is told so, not shown an empty page. ---
	s.seedRequester("idle@example.test", "Idle Hands", "OtherPass1234")
	s.login("idle@example.test", "OtherPass1234")
	body = responseBody(t, s.request(http.MethodGet, "/dashboard", nil, ""))
	if !strings.Contains(body, "Nothing is waiting on you") {
		t.Fatalf("an empty dashboard says nothing at all: %s", body)
	}
}

// A6/D5: no bulk-approve and no copy-previous endpoint may exist.
func TestNoBulkApproveOrCopyEndpointExists(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	bulk := s.postForm("/requests/bulk-approve", url.Values{})
	if bulk.StatusCode != http.StatusNotFound && bulk.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("bulk-approve status = %d, want 404/405 (the endpoint must not exist)", bulk.StatusCode)
	}
	_ = responseBody(t, bulk)
	copyResp := s.postForm("/requests/1/copy", url.Values{})
	if copyResp.StatusCode != http.StatusNotFound && copyResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("copy status = %d, want 404/405", copyResp.StatusCode)
	}
	_ = responseBody(t, copyResp)
}

// X1: nothing tax/TDS-related may leak into the schema or the route table.
func TestNoTaxColumnsInSchema(t *testing.T) {
	s := newAppTestServer(t)
	db := s.st.DB()
	bad := func(v string) bool {
		v = strings.ToLower(v)
		return strings.Contains(v, "tax") || strings.Contains(v, "tds")
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	for _, tbl := range tables {
		// vendors.tds_section / tds_rate are Phase-1V statutory vendor master
		// fields, not request-workflow tax handling; they are out of scope here.
		if tbl == "vendors" {
			continue
		}
		if bad(tbl) {
			t.Fatalf("table name contains tax/tds: %q", tbl)
		}
		cols, err := db.Query(`SELECT name FROM pragma_table_info(?)`, tbl)
		if err != nil {
			t.Fatal(err)
		}
		for cols.Next() {
			var col string
			if err := cols.Scan(&col); err != nil {
				t.Fatal(err)
			}
			if bad(col) {
				cols.Close()
				t.Fatalf("column %q in table %q contains tax/tds", col, tbl)
			}
		}
		cols.Close()
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.request(http.MethodGet, "/tax", nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /tax = %d, want 404 (no tax route may exist)", resp.StatusCode)
	}
	_ = responseBody(t, resp)
}
