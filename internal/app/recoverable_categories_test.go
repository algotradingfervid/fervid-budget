package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// F-E-02 / F-B-17 — the category picker was six literal options unconnected to
// the table, so an admin-added category could never be selected and a
// deactivated one was still offered and only refused after the form was filled
// in. F-E-02's data-integrity half is here too: an unknown code is no longer
// silently rewritten to "emd".
func TestRecoverableCategoryPickerRendersTheLiveCategories(t *testing.T) {
	s := newAppTestServer(t)
	admin := s.admin()
	if _, err := s.st.UpsertRecoverableCategory(s.ctx, admin, 0, "Retention money", true, false, true, 9); err != nil {
		t.Fatal(err)
	}
	// Deactivate a seeded one; the picker must stop offering it.
	cats, err := s.st.ListRecoverableCategories(s.ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var otherID int64
	for _, c := range cats {
		if c.Code == "other" {
			otherID = c.ID
		}
	}
	if otherID == 0 {
		t.Fatal("the seed no longer carries an 'other' category")
	}
	if _, err := s.st.UpsertRecoverableCategory(s.ctx, admin, otherID, "Other", false, false, false, 6); err != nil {
		t.Fatal(err)
	}

	s.loginAdmin()
	// The picker lives on the deposit-or-guarantee form since form-1: an employee
	// advance is always in the Employee advance category, so that form states
	// its category rather than offering the list.
	body := s.get("/requests/new?type=recoverable")
	if !strings.Contains(body, `value="retention_money"`) {
		t.Fatalf("an admin-added category is not selectable on the real form: %s", firstLines(body))
	}
	if strings.Contains(body, `value="other"`) {
		t.Fatal("a deactivated category is still offered, and will be refused after the form is filled in")
	}
	if strings.Contains(body, `value="employee_advance"`) {
		t.Fatal("the deposit form offers the Employee advance category, which the store refuses for this type")
	}
	advance := s.get("/requests/new?type=employee_advance")
	if !strings.Contains(advance, `<input type="hidden" name="recoverable_category" value="employee_advance">`) {
		t.Fatalf("the employee advance form does not fix its category: %s", firstLines(advance))
	}
	if strings.Contains(advance, `id="rcategory"`) {
		t.Fatal("the employee advance form still offers a category picker, which is how an EMD was paid to the employee")
	}
	// The new category requires a project, so the form asks for one — the reveal
	// is driven by the category's own flags, not by a hardcoded emd|pbg pair.
	fields := responseBody(t, s.htmxGet("/requests/new/fields?type=recoverable&treatment=recoverable&recoverable_category=retention_money"))
	if !strings.Contains(fields, `id="rproject"`) {
		t.Fatalf("the project field an admin-added category requires is not rendered: %s", firstLines(fields))
	}

	// No silent substitution. An unrecognised code resolves to "nothing chosen"
	// rather than to earnest money deposit.
	unknown := responseBody(t, s.htmxGet("/requests/new/fields?type=recoverable&treatment=recoverable&recoverable_category=made_up"))
	if strings.Contains(unknown, `value="emd" selected`) || strings.Contains(unknown, `value="emd"  selected`) {
		t.Fatal("an unrecognised category is silently rewritten to emd")
	}
	if !strings.Contains(unknown, `<option value="" selected>Choose a category</option>`) {
		t.Fatalf("an unrecognised category does not resolve to 'nothing chosen': %s", firstLines(unknown))
	}
}

func TestNormalizeRecoverableCategoryNeverSubstitutesACode(t *testing.T) {
	cats := []store.RecoverableCategory{
		{Code: "emd", Name: "EMD"}, {Code: "employee_advance", Name: "Employee advance"},
	}
	for _, tc := range []struct{ code, formType, want string }{
		{"emd", "vendor_invoice", "emd"},
		{"", "employee_advance", "employee_advance"},
		{"", "vendor_invoice", ""},
		{"made_up", "vendor_invoice", ""},
		// A deactivated code is not active, so it is not offered and not kept.
		{"other", "employee_advance", ""},
	} {
		if got := normalizeRecoverableCategory(tc.code, tc.formType, cats); got != tc.want {
			t.Errorf("normalizeRecoverableCategory(%q, %q) = %q, want %q", tc.code, tc.formType, got, tc.want)
		}
	}
}

// F-E-06 — recoverable_category:delete has been grantable since Phase 4 with no
// route, no control and no store function behind it. This is the door: the
// control, the verb it is gated on, and the refusal that names the reason rather
// than reporting a permission problem the caller does not have.
func TestRecoverableCategoryDeleteHasADoorAndNamesItsRefusal(t *testing.T) {
	s := newAppTestServer(t)
	admin := s.admin()
	s.loginAdmin()

	// The control exists, in its own form, on the row.
	body := s.get("/configuration")
	if !strings.Contains(body, `action="/configuration/recoverable-categories/`) {
		t.Fatalf("the Configuration screen offers no delete control: %s", firstLines(body))
	}
	if !strings.Contains(body, `<button class="btn small danger" type="submit">Delete`) {
		t.Fatalf("the delete control is not the approved button: %s", firstLines(body))
	}
	// A browser confirm() blocks a whole automated session, and the guard is the
	// server's in any case.
	if strings.Contains(body, "confirm(") {
		t.Fatal("the Configuration screen raises a JS confirm dialog")
	}

	// A category nobody has named goes.
	resp := s.postForm("/configuration/recoverable-categories", url.Values{
		"name": {"Retention deposit"}, "requires": {"none"}, "active": {"on"},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	newID := recoverableCategoryID(t, s, "Retention deposit")
	resp = s.postForm(fmt.Sprintf("/configuration/recoverable-categories/%d/delete", newID), url.Values{})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if id := recoverableCategoryID(t, s, "Retention deposit"); id != 0 {
		t.Fatalf("the category survived its delete (id %d)", id)
	}

	// One a request still points at is refused, with the count, on the screen
	// that prints that same count in its "In use" column.
	approver := seedSecondApprover(t, s)
	s.seedRecoverableRequest(9, admin.ID, approver, "icd", "Harbour Logistics", 500000)
	icdID := recoverableCategoryID(t, s, "ICD")
	if icdID == 0 {
		t.Fatal("the seeded ICD category is missing")
	}
	resp = s.postForm(fmt.Sprintf("/configuration/recoverable-categories/%d/delete", icdID), url.Values{})
	requireStatus(t, resp, http.StatusForbidden)
	refused := responseBody(t, resp)
	if strings.Contains(refused, "do not have permission") {
		t.Fatalf("the refusal reports a permission problem to the holder of every grant: %s", firstLines(refused))
	}
	mustContain(t, "the refusal", refused, "used by 1 request", "deactivate it instead", `data-label="In use"`)
	if recoverableCategoryID(t, s, "ICD") != icdID {
		t.Fatal("a refused delete removed the category anyway")
	}

	// The route is gated on delete, not on edit: a caller who may retire a
	// category by unticking Active may not remove the row.
	s.seedUserWithGrants("cat-editor@example.test", "CatEditorPass1234", "Category Editor", []store.Grant{
		{Resource: "config", Action: "view"},
		{Resource: "recoverable_category", Action: "view"},
		{Resource: "recoverable_category", Action: "edit"},
	})
	editor := newAppTestClient(t, s)
	editor.login("cat-editor@example.test", "CatEditorPass1234")
	screen := editor.get("/configuration")
	if strings.Contains(screen, "/delete") {
		t.Fatal("a caller without recoverable_category:delete is offered the control")
	}
	denied := editor.postForm(fmt.Sprintf("/configuration/recoverable-categories/%d/delete", icdID), url.Values{})
	requireStatus(t, denied, http.StatusForbidden)
	_ = responseBody(t, denied)
}

func recoverableCategoryID(t *testing.T, s *appTestServer, name string) int64 {
	t.Helper()
	rows, err := s.st.ListRecoverableCategoriesWithUsage(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Name == name {
			return r.ID
		}
	}
	return 0
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
	projectID := projectOf(t, s, headID)
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
		"project_id": {strconvFormat(projectID)}, "expected_return_date": {"2027-03-31"}, "repayment_notes": {"at maturity"},
		"manager_id": {strconvFormat(approver)},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	loc := resp.Header.Get("Location")
	_ = responseBody(t, resp)
	var id int64
	if _, err := fmt.Sscanf(loc, "/requests/%d/submitted", &id); err != nil {
		t.Fatalf("redirected to %q", loc)
	}
	detail := s.get(fmt.Sprintf("/requests/%d", id))
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
		"project_id": {strconvFormat(projectID)}, "expected_return_date": {"2027-03-31"}, "repayment_notes": {"n/a"},
		"manager_id": {strconvFormat(approver)},
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
	s.loginAdmin()
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
	page := s.get("/configuration")
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
		"id": {strconvFormat(emd.ID)}, "name": {"Earnest money"}, "requires": {"both"},
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

// form-1, review round 2 — retiring the Employee advance category made
// requestEditData mistake every ordinary employee advance for a legacy deposit,
// and tell its requester it "was recorded under the Employee advance category
// before deposits and guarantees became their own request type".
func TestRetiringTheEmployeeAdvanceCategoryIsNotMistakenForALegacyRow(t *testing.T) {
	s := newAppTestServer(t)
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	approver := seedSecondApprover(t, s)
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{
		Treatment: "recoverable", Type: "employee_advance", RecoverableCategory: "employee_advance",
		ShortTitle: "Site cash", Amount: 500000, Purpose: "site mobilisation", AdvanceReason: "site cash",
		ExpectedReturnDate: "2026-12-31", RepaymentNotes: "settled against bills", ManagerID: approver,
	})
	if err != nil {
		t.Fatal(err)
	}
	admin := s.admin()
	cats, err := s.st.ListRecoverableCategories(s.ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cats {
		if c.Code == "employee_advance" {
			if _, err := s.st.UpsertRecoverableCategory(s.ctx, admin, c.ID, c.Name, false, false, false, c.SortOrder); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.login("rhea@example.test", "RheaPass12345")
	edit := s.get(fmt.Sprintf("/requests/%d/edit", id))
	if strings.Contains(edit, "before deposits and guarantees") {
		t.Fatalf("an ordinary employee advance is called a legacy deposit because its category was retired: %s", firstLines(edit))
	}
	if !strings.Contains(edit, "The Employee advance category has been retired by your administrator") {
		t.Fatalf("the retired category is not named as the reason: %s", firstLines(edit))
	}
}
