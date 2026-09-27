package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// A deposit or guarantee (EMD, PBG, ICD, security deposit) is its own request
// type, paid to the counterparty or authority the requester names. An employee
// advance always pays the requester, so it cannot carry a deposit.
func TestDepositIsItsOwnTypeAndPaysTheCounterparty(t *testing.T) {
	s := newAppTestServer(t)
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	approver := seedSecondApprover(t, s)
	s.login("rhea@example.test", "RheaPass12345")

	// Step 1: the chooser offers the deposit as a card of its own.
	chooser := s.get("/requests/new")
	if !strings.Contains(chooser, `href="/requests/new?type=recoverable"`) || !strings.Contains(chooser, "Deposit or guarantee") {
		t.Fatalf("the chooser has no card for a deposit or guarantee: %s", firstLines(chooser))
	}

	// Step 2: its form fixes the recoverable treatment, offers every category
	// except Employee advance, and asks who is paid.
	form := s.get("/requests/new?type=recoverable")
	mustContain(t, "the deposit form", form,
		`<input type="hidden" name="treatment" value="recoverable">`,
		`id="rcategory"`, `value="emd"`, `value="icd"`, `value="security_deposit"`,
		`name="vendor_payee"`, "Who is paid",
	)
	if strings.Contains(form, `value="employee_advance"`) {
		t.Fatal("the deposit form offers the Employee advance category, which would make the payee the requester")
	}
	if strings.Contains(form, `name="head_id"`) || strings.Contains(form, `name="advance_reason"`) {
		t.Fatal("the deposit form asks for a budget head or an advance reason, neither of which a deposit has")
	}

	// An inter-corporate deposit, raised through the form's POST.
	resp := s.postForm("/requests", url.Values{
		"type": {"recoverable"}, "treatment": {"recoverable"}, "recoverable_category": {"icd"},
		"short_title": {"ICD to Meridian"}, "amount": {"10,00,000"}, "purpose": {"inter-corporate deposit"},
		"counterparty": {"Meridian Holdings Pvt Ltd"}, "vendor_payee": {"Meridian Holdings Pvt Ltd"},
		"expected_return_date": {"2027-03-31"}, "repayment_notes": {"returned at maturity with interest"},
		"manager_id": {strconvFormat(approver)},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	loc := resp.Header.Get("Location")
	_ = responseBody(t, resp)
	var id int64
	if _, err := fmt.Sscanf(loc, "/requests/%d/submitted", &id); err != nil {
		t.Fatalf("redirected to %q, want the submitted screen", loc)
	}
	detail := s.get(fmt.Sprintf("/requests/%d", id))
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
		"manager_id": {strconvFormat(approver)},
	})
	requireStatus(t, refused, http.StatusBadRequest)
	if body := responseBody(t, refused); !strings.Contains(body, "say who receives the money") || !strings.Contains(body, `value="Ridge Metro EMD"`) {
		t.Fatalf("a payee-less deposit was not refused for its payee with the typing kept: %s", firstLines(body))
	}

	// The employee advance states its category rather than offering the list,
	// and the store refuses any other category under that type.
	advance := s.get("/requests/new?type=employee_advance")
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
		"manager_id": {strconvFormat(approver)},
	})
	requireStatus(t, forged, http.StatusBadRequest)
	if body := responseBody(t, forged); !strings.Contains(body, "Employee advance category") {
		t.Fatalf("an ICD forged under the employee advance type was not refused for its category: %s", firstLines(body))
	}
}

// A deposit's payee is editable wherever the rest of the request is, and
// survives the returned screen, which posts only what it shows.
func TestDepositPayeeIsOnTheEditFormAndSurvivesTheReturnedScreen(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Ridge")
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	approver := seedSecondApprover(t, s)
	projectID := projectOf(t, s, headID)
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
	edit := s.get(fmt.Sprintf("/requests/%d/edit", id))
	if !strings.Contains(edit, `name="vendor_payee" value="Ridge Metro Rail Corporation"`) {
		t.Fatalf("the edit form does not carry the deposit's payee: %s", firstLines(edit))
	}
	resp := s.submitScreen(fmt.Sprintf("/requests/%d/edit", id), url.Values{
		"type": {"recoverable"}, "treatment": {"recoverable"}, "recoverable_category": {"emd"},
		"short_title": {"Ridge Metro EMD"}, "amount": {"2,50,000"}, "purpose": {"tender, corrected"},
		"project_id": {strconvFormat(projectID)}, "vendor_payee": {"Ridge Metro Rail Corporation Ltd"},
		"expected_return_date": {"2027-03-31"}, "repayment_notes": {"on award"},
		"manager_id": {strconvFormat(approver)},
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
	returned := s.get(fmt.Sprintf("/requests/%d", id))
	if !strings.Contains(returned, `<input type="hidden" name="vendor_payee" value="Ridge Metro Rail Corporation Ltd">`) {
		t.Fatalf("the returned screen drops the payee, so a resubmit would be refused: %s", firstLines(returned))
	}
}

// An employee advance recorded under a deposit category before deposits were
// their own type still opens on the edit form: its category is offered back as
// Employee advance and the banner says so.
func TestLegacyEmployeeAdvanceUnderADepositCategoryStaysEditable(t *testing.T) {
	s := newAppTestServer(t)
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	approver := seedSecondApprover(t, s)
	id := seedLegacyDepositAsEmployeeAdvance(t, s, 900, requester, approver)
	s.login("rhea@example.test", "RheaPass12345")
	edit := s.get(fmt.Sprintf("/requests/%d/edit", id))
	if !strings.Contains(edit, `<input type="hidden" name="recoverable_category" value="employee_advance">`) {
		t.Fatalf("the legacy row is not offered back as an Employee advance: %s", firstLines(edit))
	}
	if !strings.Contains(edit, "before deposits and guarantees became their own request type") || !strings.Contains(edit, "ICD — inter-corporate deposit") {
		t.Fatalf("the edit form does not explain the reclassification: %s", firstLines(edit))
	}
	// And the row itself still reads as it was recorded.
	detail := s.get(fmt.Sprintf("/requests/%d", id))
	if !strings.Contains(detail, "ICD — inter-corporate deposit") || !strings.Contains(detail, "Meridian Holdings Pvt Ltd") {
		t.Fatalf("the legacy row no longer reads as recorded: %s", firstLines(detail))
	}
}

// The Configuration hint says the employee advance fills in the PAYEE, not the
// counterparty.
func TestConfigurationHintNamesThePayeeNotTheCounterparty(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
	page := s.get("/configuration")
	if !strings.Contains(page, "fills the <b>payee</b> in from the requester automatically") {
		t.Fatalf("the hint no longer says the payee is filled in: %s", firstLines(page))
	}
	if strings.Contains(page, "fills the counterparty") || !strings.Contains(page, "does not fill in the counterparty") {
		t.Fatalf("the hint claims the counterparty is filled in: %s", firstLines(page))
	}
}

// seedLegacyDepositAsEmployeeAdvance writes an employee advance recorded under
// a deposit category before deposits were their own request type. Only raw SQL
// can write it now, which is the point.
func seedLegacyDepositAsEmployeeAdvance(t *testing.T, s *appTestServer, seq int, requester store.User, approver int64) int64 {
	t.Helper()
	return s.insertRequest(t, map[string]any{
		"number": requestNumber(seq), "status": "pending", "treatment": "recoverable", "type": "employee_advance",
		"recoverable_category": "icd", "recoverable_category_id": categoryIDByCode(t, s, "icd"),
		"amount": int64(100000000), "purpose": "deposit", "short_title": "ICD to Meridian", "advance_reason": "deposit",
		"counterparty": "Meridian Holdings Pvt Ltd", "expected_return_date": "2027-03-31", "repayment_notes": "at maturity",
		"vendor_payee": requester.Name, "requester_id": requester.ID, "manager_id": approver,
		"submitted_at": sqlNow,
	})
}

// Resubmitting a returned legacy deposit-as-employee-advance files it under the
// Employee advance category, so the returned screen says so before the press:
// it carries the same notice as the full edit form, its buttons name what they
// do, and the notice survives a refused attempt.
func TestLegacyDepositReturnedScreenSaysWhatResubmittingDoes(t *testing.T) {
	s := newAppTestServer(t)
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	approver := seedSecondApprover(t, s)
	id := seedLegacyDepositAsEmployeeAdvance(t, s, 901, requester, approver)
	if err := s.st.ReturnRequest(s.ctx, mustUser(t, s, approver), id, "please fix"); err != nil {
		t.Fatal(err)
	}
	s.login("rhea@example.test", "RheaPass12345")

	returned := s.get(fmt.Sprintf("/requests/%d", id))
	mustContain(t, "the returned screen", returned,
		"Recorded before deposits and guarantees were their own request type",
		"ICD — inter-corporate deposit",
		"Meridian Holdings Pvt Ltd",
		`href="/requests/new?type=recoverable"`,
		">Resubmit as an Employee advance<",
		">Save as an Employee advance<",
	)
	if strings.Contains(returned, "since been retired") {
		t.Fatal("the returned screen titles the legacy notice with a retirement it does not have")
	}

	// A refused attempt keeps the notice: the re-rendered screen is built from
	// the posted values, which already say Employee advance.
	base := url.Values{
		"type": {"employee_advance"}, "treatment": {"recoverable"}, "recoverable_category": {"employee_advance"},
		"short_title": {"ICD to Meridian"}, "purpose": {"deposit"}, "advance_reason": {"deposit"},
		"counterparty": {"Meridian Holdings Pvt Ltd"}, "expected_return_date": {"2027-03-31"},
		"repayment_notes": {"at maturity"}, "manager_id": {strconvFormat(approver)}, "submit_action": {"resubmit"},
	}
	refused := url.Values{}
	for k, v := range base {
		refused[k] = v
	}
	refused.Set("amount", "0")
	resp := s.submitScreen(fmt.Sprintf("/requests/%d/edit", id), refused)
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	if !strings.Contains(body, "Recorded before deposits and guarantees were their own request type") || !strings.Contains(body, ">Resubmit as an Employee advance<") {
		t.Fatalf("the refused returned screen lost the legacy notice: %s", firstLines(body))
	}
	after, err := s.st.Request(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.RecoverableCategory != "icd" || after.Status != "returned" {
		t.Fatalf("a refused resubmit changed the row: %+v", after)
	}

	// The stated consequence, when the requester takes it.
	base.Set("amount", "10,00,000")
	resp = s.submitScreen(fmt.Sprintf("/requests/%d/edit", id), base)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	after, err = s.st.Request(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.RecoverableCategory != "employee_advance" || after.Status != "pending" {
		t.Fatalf("after the stated resubmit = %+v", after)
	}
}

// Each edit-form warning carries its own heading rather than one shared
// heading about retirement, and a request that is both legacy and on a retired
// project shows both warnings, separately.
func TestEditFormWarningsCarryTheirOwnHeadings(t *testing.T) {
	s := newAppTestServer(t)
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	approver := seedSecondApprover(t, s)
	id := seedLegacyDepositAsEmployeeAdvance(t, s, 901, requester, approver)
	s.login("rhea@example.test", "RheaPass12345")

	edit := s.get(fmt.Sprintf("/requests/%d/edit", id))
	if !strings.Contains(edit, "<b>Recorded before deposits and guarantees were their own request type</b>") {
		t.Fatalf("the legacy warning has no heading of its own: %s", firstLines(edit))
	}
	if strings.Contains(edit, "since been retired") {
		t.Fatal("the legacy warning is titled with a retirement the request does not have")
	}

	// Now retire the project the legacy row is linked to: two warnings, two headings.
	admin := s.admin()
	projectID, err := s.st.UpsertProject(s.ctx, 0, "Legacy Plant", true, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET project_id=? WHERE id=?`, projectID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.UpsertProject(s.ctx, projectID, "Legacy Plant", false, 5); err != nil {
		t.Fatal(err)
	}
	_ = admin
	both := s.get(fmt.Sprintf("/requests/%d/edit", id))
	if !strings.Contains(both, "<b>Recorded before deposits and guarantees were their own request type</b>") ||
		!strings.Contains(both, "<b>This request charges something that has since been retired</b>") {
		t.Fatalf("the two warnings do not each carry their heading: %s", firstLines(both))
	}
	if n := strings.Count(both, `class="banner warn"`); n != 2 {
		t.Fatalf("%d warn banners, want one per warning", n)
	}
}
