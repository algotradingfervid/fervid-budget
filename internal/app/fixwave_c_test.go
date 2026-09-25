package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// The 2026-09-25 fix wave, cluster C (request types). Every test below pins one
// defect id from docs/qa/results/fixwave-2026-09-25/C-request-types.md, so a
// regression points straight at the finding.

// form-1 / recoverables-1 — a deposit or guarantee (EMD, PBG, ICD, security
// deposit) is its own request type, paid to the counterparty or authority the
// requester names. It used to be raisable only as an employee advance, which
// always pays the requester, so a ₹10 lakh inter-corporate deposit was recorded
// as "Paid to Rhea Requester".
func TestDepositIsItsOwnTypeAndPaysTheCounterparty(t *testing.T) {
	s := newAppTestServer(t)
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	approver := seedSecondApprover(t, s)
	s.login("rhea@example.test", "RheaPass12345")

	// Step 1: the chooser offers the deposit as a card of its own.
	chooser := responseBody(t, s.request(http.MethodGet, "/requests/new", nil, ""))
	if !strings.Contains(chooser, `href="/requests/new?type=recoverable"`) || !strings.Contains(chooser, "Deposit or guarantee") {
		t.Fatalf("the chooser has no card for a deposit or guarantee: %s", firstLines(chooser))
	}

	// Step 2: its form fixes the recoverable treatment, offers every category
	// except Employee advance, and asks who is paid.
	form := responseBody(t, s.request(http.MethodGet, "/requests/new?type=recoverable", nil, ""))
	for _, want := range []string{
		`<input type="hidden" name="treatment" value="recoverable">`,
		`id="rcategory"`, `value="emd"`, `value="icd"`, `value="security_deposit"`,
		`name="vendor_payee"`, "Who is paid",
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("the deposit form is missing %q: %s", want, firstLines(form))
		}
	}
	if strings.Contains(form, `value="employee_advance"`) {
		t.Fatal("the deposit form offers the Employee advance category, which would make the payee the requester")
	}
	if strings.Contains(form, `name="head_id"`) || strings.Contains(form, `name="advance_reason"`) {
		t.Fatal("the deposit form asks for a budget head or an advance reason, neither of which a deposit has")
	}

	// The ICD from the finding, raised through the form's POST.
	resp := s.postForm("/requests", url.Values{
		"type": {"recoverable"}, "treatment": {"recoverable"}, "recoverable_category": {"icd"},
		"short_title": {"ICD to Meridian"}, "amount": {"10,00,000"}, "purpose": {"inter-corporate deposit"},
		"counterparty": {"Meridian Holdings Pvt Ltd"}, "vendor_payee": {"Meridian Holdings Pvt Ltd"},
		"expected_return_date": {"2027-03-31"}, "repayment_notes": {"returned at maturity with interest"},
		"manager_id": {itoa64(approver)},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	loc := resp.Header.Get("Location")
	_ = responseBody(t, resp)
	var id int64
	if _, err := fmt.Sscanf(loc, "/requests/%d/submitted", &id); err != nil {
		t.Fatalf("redirected to %q, want the submitted screen", loc)
	}
	detail := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", id), nil, ""))
	if !strings.Contains(detail, "Deposit or guarantee") {
		t.Fatalf("the request is not labelled as a deposit: %s", firstLines(detail))
	}
	paidTo := between(t, detail, "<dt>Paid to</dt>", "</dd>")
	if !strings.Contains(paidTo, "Meridian Holdings Pvt Ltd") || strings.Contains(paidTo, requester.Name) {
		t.Fatalf("Paid to = %q, want the counterparty and not the requester", paidTo)
	}
	req, err := s.st.Request(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if req.Type != "recoverable" || req.VendorPayee != "Meridian Holdings Pvt Ltd" || req.Vendor != "Meridian Holdings Pvt Ltd" {
		t.Fatalf("stored as type=%s payee=%q vendor=%q", req.Type, req.VendorPayee, req.Vendor)
	}

	// A deposit that names nobody is refused for that reason, with the form back.
	refused := s.postForm("/requests", url.Values{
		"type": {"recoverable"}, "treatment": {"recoverable"}, "recoverable_category": {"emd"},
		"short_title": {"Ridge Metro EMD"}, "amount": {"2,50,000"}, "purpose": {"tender"},
		"project_id": {"1"}, "expected_return_date": {"2027-03-31"}, "repayment_notes": {"on award"},
		"manager_id": {itoa64(approver)},
	})
	requireStatus(t, refused, http.StatusBadRequest)
	if body := responseBody(t, refused); !strings.Contains(body, "say who receives the money") || !strings.Contains(body, `value="Ridge Metro EMD"`) {
		t.Fatalf("a payee-less deposit was not refused for its payee with the typing kept: %s", firstLines(body))
	}

	// The employee advance states its category rather than offering the list,
	// and the store refuses any other category under that type.
	advance := responseBody(t, s.request(http.MethodGet, "/requests/new?type=employee_advance", nil, ""))
	if !strings.Contains(advance, `<input type="hidden" name="recoverable_category" value="employee_advance">`) || strings.Contains(advance, `id="rcategory"`) {
		t.Fatalf("the employee advance form still lets the requester pick a deposit category: %s", firstLines(advance))
	}
	if !strings.Contains(advance, "An employee advance always pays the person raising it.") {
		t.Fatal("the employee advance form no longer says who it pays")
	}
	forged := s.postForm("/requests", url.Values{
		"type": {"employee_advance"}, "treatment": {"recoverable"}, "recoverable_category": {"icd"},
		"short_title": {"ICD as an advance"}, "amount": {"10,00,000"}, "purpose": {"deposit"},
		"advance_reason": {"deposit"}, "counterparty": {"Meridian Holdings Pvt Ltd"},
		"expected_return_date": {"2027-03-31"}, "repayment_notes": {"at maturity"},
		"manager_id": {itoa64(approver)},
	})
	requireStatus(t, forged, http.StatusBadRequest)
	if body := responseBody(t, forged); !strings.Contains(body, "Employee advance category") {
		t.Fatalf("an ICD forged under the employee advance type was not refused for its category: %s", firstLines(body))
	}
}

// form-1 — the deposit's payee is editable where the rest of the request is,
// and survives the returned screen, which posts only what it shows.
func TestDepositPayeeIsOnTheEditFormAndSurvivesTheReturnedScreen(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Ridge")
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	approver := seedSecondApprover(t, s)
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{
		Treatment: "recoverable", Type: "recoverable", ShortTitle: "Ridge Metro EMD",
		RecoverableCategory: "emd", ProjectID: projectID, Amount: 25000000, Purpose: "tender",
		VendorPayee: "Ridge Metro Rail Corporation", ExpectedReturnDate: "2027-03-31",
		RepaymentNotes: "on award", ManagerID: approver,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.login("rhea@example.test", "RheaPass12345")
	edit := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/edit", id), nil, ""))
	if !strings.Contains(edit, `name="vendor_payee" value="Ridge Metro Rail Corporation"`) {
		t.Fatalf("the edit form does not carry the deposit's payee: %s", firstLines(edit))
	}
	resp := s.postForm(fmt.Sprintf("/requests/%d/edit", id), url.Values{
		"type": {"recoverable"}, "treatment": {"recoverable"}, "recoverable_category": {"emd"},
		"short_title": {"Ridge Metro EMD"}, "amount": {"2,50,000"}, "purpose": {"tender, corrected"},
		"project_id": {itoa64(projectID)}, "vendor_payee": {"Ridge Metro Rail Corporation Ltd"},
		"expected_return_date": {"2027-03-31"}, "repayment_notes": {"on award"},
		"manager_id": {itoa64(approver)},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	req, err := s.st.Request(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if req.VendorPayee != "Ridge Metro Rail Corporation Ltd" {
		t.Fatalf("payee after edit = %q", req.VendorPayee)
	}

	// Returned: the correction screen posts the payee it does not show.
	if err := s.st.ReturnRequest(s.ctx, mustUser(t, s, approver), id, "attach the tender notice"); err != nil {
		t.Fatal(err)
	}
	returned := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", id), nil, ""))
	if !strings.Contains(returned, `<input type="hidden" name="vendor_payee" value="Ridge Metro Rail Corporation Ltd">`) {
		t.Fatalf("the returned screen drops the payee, so a resubmit would be refused: %s", firstLines(returned))
	}
}

// form-3 (T12) — a project or head retired after a request was raised is shown
// on the edit form, marked, with the retirement explained; the historical
// values are not silently blanked into "Choose a project" and a save is refused
// for the retirement, not for a missing project.
func TestEditFormOffersARetiredProjectBackAndSaysWhy(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Legacy")
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	approver := seedSecondApprover(t, s)
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{
		Treatment: "budget", Type: "reimbursement", ShortTitle: "Legacy plant visit",
		ProjectID: projectID, HeadID: headID, Amount: 120000, Purpose: "Original purpose",
		ExpenseDate: "2026-07-01", ManagerID: approver,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.UpsertProject(s.ctx, projectID, "Operations Legacy", false, 1); err != nil {
		t.Fatal(err)
	}
	_ = admin

	s.login("rhea@example.test", "RheaPass12345")
	edit := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/edit", id), nil, ""))
	projectSelect := between(t, edit, `<select id="project"`, `</select>`)
	if !strings.Contains(projectSelect, fmt.Sprintf(`<option value="%d" selected>Operations Legacy (retired)</option>`, projectID)) {
		t.Fatalf("the retired project is not offered back, marked and selected: %s", projectSelect)
	}
	headSelect := between(t, edit, `<select id="head"`, `</select>`)
	if !strings.Contains(headSelect, fmt.Sprintf(`<option value="%d" selected>Operations Legacy / Rent Legacy (retired)</option>`, headID)) {
		t.Fatalf("the head under the retired project is not offered back, marked and selected: %s", headSelect)
	}
	if !strings.Contains(edit, "retired after it was raised") || !strings.Contains(edit, "Operations Legacy") {
		t.Fatalf("the edit form does not explain the retirement: %s", firstLines(edit))
	}

	// Saving with the historical ids is refused for the retirement, in words.
	resp := s.postForm(fmt.Sprintf("/requests/%d/edit", id), url.Values{
		"type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Legacy plant visit"},
		"amount": {"1,200"}, "purpose": {"Corrected purpose"}, "expense_date": {"2026-07-01"},
		"project_id": {itoa64(projectID)}, "head_id": {itoa64(headID)}, "manager_id": {itoa64(approver)},
	})
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	if strings.Contains(body, "project and head are required") {
		t.Fatal("the refusal claims the request has no project and head; it has both, they were retired")
	}
	if !strings.Contains(body, "has been retired") {
		t.Fatalf("the refusal does not name the retirement: %s", firstLines(body))
	}
	// And the refused form still shows the historical values, not blanks.
	if !strings.Contains(body, "Operations Legacy (retired)") {
		t.Fatalf("the refused form blanked the retired project: %s", firstLines(body))
	}
}

// form-6 (T10) — with attachments compulsory, a submit with no document and no
// reason is refused, and a written reason unblocks it. The cited render-only
// test proves the form asks; this proves the store enforces it on the real POST.
func TestCompulsoryAttachmentsAreEnforcedOnSubmit(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Att")
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	approver := seedSecondApprover(t, s)
	if err := s.st.SetAppSetting(s.ctx, admin, "require_attachments", "1"); err != nil {
		t.Fatal(err)
	}
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	_ = requester
	s.login("rhea@example.test", "RheaPass12345")
	body := func(reason string) url.Values {
		return url.Values{
			"type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Team lunch"},
			"amount": {"500"}, "purpose": {"team lunch"}, "expense_date": {"2026-07-21"},
			"project_id": {itoa64(projectID)}, "head_id": {itoa64(headID)}, "manager_id": {itoa64(approver)},
			"attachment_exception_reason": {reason},
		}
	}
	refused := s.postForm("/requests", body(""))
	requireStatus(t, refused, http.StatusBadRequest)
	if page := responseBody(t, refused); !strings.Contains(page, "say why you cannot") {
		t.Fatalf("a fileless, reasonless submit was not refused for the attachment policy: %s", firstLines(page))
	}
	accepted := s.postForm("/requests", body("Receipt arrives with the card statement on Monday"))
	requireStatus(t, accepted, http.StatusSeeOther)
	_ = responseBody(t, accepted)
}

// form-6 (T12) — a retired PROJECT (not only a head) disappears from the
// new-request form, and the request raised against it keeps showing its name.
func TestRetiredProjectIsHiddenFromNewRequestsAndKeptOnHistory(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Legacy")
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	approver := seedSecondApprover(t, s)
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{
		Treatment: "budget", Type: "reimbursement", ShortTitle: "Legacy plant visit",
		ProjectID: projectID, HeadID: headID, Amount: 120000, Purpose: "site visit",
		ExpenseDate: "2026-07-01", ManagerID: approver,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.login("rhea@example.test", "RheaPass12345")
	before := responseBody(t, s.request(http.MethodGet, "/requests/new?type=vendor_invoice", nil, ""))
	if !strings.Contains(between(t, before, `<select id="project"`, `</select>`), "Operations Legacy") {
		t.Fatal("the live project is not offered on the new-request form, so the retirement below proves nothing")
	}
	if _, err := s.st.UpsertProject(s.ctx, projectID, "Operations Legacy", false, 1); err != nil {
		t.Fatal(err)
	}
	after := responseBody(t, s.request(http.MethodGet, "/requests/new?type=vendor_invoice", nil, ""))
	if strings.Contains(between(t, after, `<select id="project"`, `</select>`), "Operations Legacy") {
		t.Fatal("a retired project is still offered for new requests")
	}
	if strings.Contains(between(t, after, `<select id="head"`, `</select>`), "Rent Legacy") {
		t.Fatal("a head under a retired project is still offered for new requests")
	}
	detail := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", id), nil, ""))
	if !strings.Contains(detail, "<dd>Operations Legacy</dd>") || !strings.Contains(detail, "<dd>Rent Legacy</dd>") {
		t.Fatalf("the historical request lost its retired project or head name: %s", firstLines(detail))
	}
}

// recoverables-7 (V8) — every recoverable category may link a project; only the
// ones that require one insist on it. An ICD or a security deposit used to have
// no project field at all, so it read "Not project linked" with no way to say
// otherwise.
func TestEveryRecoverableCategoryMayLinkAProject(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Link")
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	approver := seedSecondApprover(t, s)
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	_ = requester
	s.login("rhea@example.test", "RheaPass12345")

	for _, tc := range []struct {
		code     string
		required bool
	}{{"emd", true}, {"pbg", true}, {"icd", false}, {"security_deposit", false}, {"other", false}} {
		frag := responseBody(t, s.htmxGet("/requests/new/fields?type=recoverable&treatment=recoverable&recoverable_category="+tc.code))
		if !strings.Contains(frag, `id="rproject"`) {
			t.Fatalf("%s: no project field at all", tc.code)
		}
		if got := strings.Contains(between(t, frag, `<select id="rproject"`, ">"), `aria-required="true"`); got != tc.required {
			t.Fatalf("%s: project required = %v, want %v", tc.code, got, tc.required)
		}
	}
	// The employee advance's own fragment offers the optional link too.
	if frag := responseBody(t, s.htmxGet("/requests/new/fields?type=employee_advance&treatment=recoverable")); !strings.Contains(frag, `id="rproject"`) {
		t.Fatalf("an employee advance cannot be linked to a project: %s", firstLines(frag))
	}

	resp := s.postForm("/requests", url.Values{
		"type": {"recoverable"}, "treatment": {"recoverable"}, "recoverable_category": {"icd"},
		"short_title": {"ICD to Meridian"}, "amount": {"10,00,000"}, "purpose": {"deposit"},
		"counterparty": {"Meridian Holdings Pvt Ltd"}, "vendor_payee": {"Meridian Holdings Pvt Ltd"},
		"project_id": {itoa64(projectID)}, "expected_return_date": {"2027-03-31"}, "repayment_notes": {"at maturity"},
		"manager_id": {itoa64(approver)},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	loc := resp.Header.Get("Location")
	_ = responseBody(t, resp)
	var id int64
	if _, err := fmt.Sscanf(loc, "/requests/%d/submitted", &id); err != nil {
		t.Fatalf("redirected to %q", loc)
	}
	detail := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", id), nil, ""))
	if !strings.Contains(detail, "<dd>Operations Link</dd>") {
		t.Fatalf("the linked project is not on the request: %s", firstLines(detail))
	}
	// A link to a retired project is refused as such, like a retired head.
	if _, err := s.st.UpsertProject(s.ctx, projectID, "Operations Link", false, 1); err != nil {
		t.Fatal(err)
	}
	refused := s.postForm("/requests", url.Values{
		"type": {"recoverable"}, "treatment": {"recoverable"}, "recoverable_category": {"other"},
		"short_title": {"Misc"}, "amount": {"1,000"}, "purpose": {"misc"}, "vendor_payee": {"Someone"},
		"project_id": {itoa64(projectID)}, "expected_return_date": {"2027-03-31"}, "repayment_notes": {"n/a"},
		"manager_id": {itoa64(approver)},
	})
	requireStatus(t, refused, http.StatusBadRequest)
	if body := responseBody(t, refused); !strings.Contains(body, "project has been retired") {
		t.Fatalf("a link to a retired project was not refused as such: %s", firstLines(body))
	}
}

// recoverables-5 (V4) — an existing category's name and rule are editable on the
// Configuration screen, in the row, audited. They used to be hidden inputs, so
// an in-use category's rule could only change by delete-and-recreate, which the
// store refuses while any request names it.
func TestRecoverableCategoriesAreEditableInPlace(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	cats, err := s.st.ListRecoverableCategories(s.ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var emd store.RecoverableCategory
	for _, c := range cats {
		if c.Code == "emd" {
			emd = c
		}
	}
	if emd.ID == 0 {
		t.Fatal("the seed no longer carries EMD")
	}
	page := responseBody(t, s.request(http.MethodGet, "/configuration", nil, ""))
	for _, want := range []string{
		fmt.Sprintf(`<input form="rc-%d" name="name" aria-label="Name for EMD" value="EMD" required>`, emd.ID),
		fmt.Sprintf(`<select form="rc-%d" name="requires" aria-label="Requires for EMD">`, emd.ID),
		fmt.Sprintf(`<button form="rc-%d" class="btn small outline" type="submit">Save<span class="sr-only"> EMD</span></button>`, emd.ID),
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the category row has no visible edit control %q: %s", want, firstLines(page))
		}
	}
	if strings.Contains(page, `type="hidden" name="name"`) || strings.Contains(page, `type="hidden" name="requires"`) {
		t.Fatal("the name or the rule still travels as a hidden input")
	}

	resp := s.postForm("/configuration/recoverable-categories", url.Values{
		"id": {itoa64(emd.ID)}, "name": {"Earnest money"}, "requires": {"both"},
		"sort_order": {"2"}, "active": {"on"},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	cats, err = s.st.ListRecoverableCategories(s.ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var after store.RecoverableCategory
	for _, c := range cats {
		if c.ID == emd.ID {
			after = c
		}
	}
	if after.Name != "Earnest money" || !after.RequiresProject || !after.RequiresCounterparty || !after.Active || after.Code != "emd" {
		t.Fatalf("after edit = %+v; want renamed, both rules, active, code unchanged", after)
	}
	audit, err := s.st.Audit(s.ctx, "recoverable_category", emd.ID, 5)
	if err != nil || len(audit) == 0 || audit[0].Action != "update" {
		t.Fatalf("the edit was not audited: %#v, %v", audit, err)
	}
	// The new rule reaches the form: EMD now also asks for a counterparty.
	frag := responseBody(t, s.htmxGet("/requests/new/fields?type=recoverable&treatment=recoverable&recoverable_category=emd"))
	if !strings.Contains(frag, `name="counterparty"`) || !strings.Contains(frag, "Earnest money") {
		t.Fatalf("the edited rule and name did not reach the request form: %s", firstLines(frag))
	}
}

// tests-1 (F-E-07) — the Configuration hint says the employee advance fills in
// the PAYEE, not the counterparty. The fix landed without a test, so the copy
// could slip back.
func TestConfigurationHintNamesThePayeeNotTheCounterparty(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	page := responseBody(t, s.request(http.MethodGet, "/configuration", nil, ""))
	if !strings.Contains(page, "fills the <b>payee</b> in from the requester automatically") {
		t.Fatalf("the hint no longer says the payee is filled in: %s", firstLines(page))
	}
	if strings.Contains(page, "fills the counterparty") || !strings.Contains(page, "does not fill in the counterparty") {
		t.Fatalf("the hint claims the counterparty is filled in: %s", firstLines(page))
	}
}

func mustUser(t *testing.T, s *appTestServer, id int64) store.User {
	t.Helper()
	u, err := s.st.UserByID(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
