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
	if !strings.HasPrefix(body, "Number,Status,Type,Title,Amount,Payee,Requester,Approver,Created") {
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
	if n := strings.Count(body, `class="type-card"`); n != 4 {
		t.Fatalf("the chooser renders %d type cards, want 4", n)
	}
	for _, want := range []string{
		`href="/requests/new?type=vendor_invoice"`,
		`href="/requests/new?type=vendor_advance"`,
		`href="/requests/new?type=reimbursement"`,
		`href="/requests/new?type=employee_advance"`,
		"Vendor invoice payment", "Vendor advance", "Reimbursement", "Employee advance",
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
		`class="choice"`, `money-field"`, `class="money-wrap"`, `class="in-words"`, `class="combo"`,
		`class="uploader"`, `class="action-bar"`, `name="short_title"`, `name="urgency_reason"`,
		`name="invoice_no"`, `name="invoice_date"`, `hx-get="/requests/new/fields"`,
		`data-when="treatment:budget"`, `data-when="urgent:on"`, "<h1>",
		`action="/requests"`, "Submit request",
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("the form is missing %q", want)
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
