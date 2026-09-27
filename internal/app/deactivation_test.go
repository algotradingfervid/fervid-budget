package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// F-G-024: retiring a head with unfinished requests against it warns, names
// them, and saves only once the administrator confirms. The owner chose
// warn-and-confirm over a hard block (2026-09-25).
func TestDeactivatingAHeadWithOpenRequestsAsksForConfirmation(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Retire")
	requester := s.seedRequester("retire.req@example.test", "Retire Requester", "RetirePass12345")
	s.seedPendingRequest(951, requester.ID, admin.ID, headID, 120000)
	s.loginAdmin()

	projectID := projectOf(t, s, headID)
	form := func() url.Values {
		return url.Values{
			"id": {fmt.Sprint(headID)}, "project_id": {fmt.Sprint(projectID)},
			"name": {"Rent Retire"}, "due_day": {"5"}, "sort_order": {"1"},
		}
	}

	resp := s.postForm("/heads", form())
	body := responseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unconfirmed deactivation status = %d, want 200 with a confirmation page", resp.StatusCode)
	}
	for _, want := range []string{"PR-2026-000951", `name="confirm"`, `action="/heads"`} {
		if !strings.Contains(body, want) {
			t.Errorf("confirmation page lacks %q", want)
		}
	}
	if !s.headActive(headID) {
		t.Fatal("the head was deactivated before anyone confirmed")
	}

	confirmed := form()
	confirmed.Set("confirm", "on")
	requireStatus(t, s.postForm("/heads", confirmed), http.StatusSeeOther)
	if s.headActive(headID) {
		t.Fatal("the confirmed deactivation did not save")
	}

	// Nothing open against it: no question to ask.
	_, quiet := s.seedHead("Quiet")
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, quiet).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, s.postForm("/heads", url.Values{
		"id": {fmt.Sprint(quiet)}, "project_id": {fmt.Sprint(projectID)},
		"name": {"Rent Quiet"}, "due_day": {"5"}, "sort_order": {"1"},
	}), http.StatusSeeOther)
	if s.headActive(quiet) {
		t.Fatal("a head with nothing open should deactivate without a question")
	}
}

// F-G-025: deactivating an approver with requests waiting on them warns, lists
// those requests, and carries the rest of the edit through the confirmation.
func TestDeactivatingAnApproverWithWaitingRequestsAsksForConfirmation(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Leaver")
	requester := s.seedRequester("leaver.req@example.test", "Leaver Requester", "LeaverPass12345")
	approver := s.seedRequester("leaver.apr@example.test", "Leaving Approver", "ApproverPass12345")
	s.assignRole(approver.ID, "Manager")
	s.seedPendingRequest(961, requester.ID, approver.ID, headID, 90000)
	s.loginAdmin()

	const newPassword = "FreshPassword98765"
	form := url.Values{
		"id": {fmt.Sprint(approver.ID)}, "email": {approver.Email}, "name": {"Leaving Approver Renamed"},
		"role": {"data_entry"}, "default_approver_id": {"0"}, "password": {newPassword},
		"role_ids": {s.roleIDByName(t, "Requester"), s.roleIDByName(t, "Manager")},
	}
	resp := s.postForm("/users", form)
	body := responseBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unconfirmed deactivation status = %d, want 200 with a confirmation page", resp.StatusCode)
	}
	for _, want := range []string{"PR-2026-000961", `name="confirm"`, `action="/users"`, "Leaving Approver Renamed", "reassign"} {
		if !strings.Contains(body, want) {
			t.Errorf("confirmation page lacks %q", want)
		}
	}
	if strings.Contains(body, newPassword) {
		t.Error("the confirmation page echoed the new password into the markup")
	}
	if u, err := s.st.UserByID(s.ctx, approver.ID); err != nil || !u.Active {
		t.Fatalf("the approver was deactivated before anyone confirmed (err %v)", err)
	}

	form.Set("confirm", "on")
	requireStatus(t, s.postForm("/users", form), http.StatusSeeOther)
	u, err := s.st.UserByID(s.ctx, approver.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Active || u.Name != "Leaving Approver Renamed" {
		t.Fatalf("after confirming: active=%v name=%q, want inactive and renamed", u.Active, u.Name)
	}
}

func (s *appTestServer) headActive(id int64) bool {
	s.t.Helper()
	var active int
	if err := s.st.DB().QueryRow(`SELECT active FROM heads WHERE id=?`, id).Scan(&active); err != nil {
		s.t.Fatal(err)
	}
	return active == 1
}

// F-G-011: the requests export carries the approved figure beside the
// requested one, so an approved-down request no longer exports only the amount
// nobody approved. A request not yet decided leaves the column empty.
func TestRequestsExportCarriesTheApprovedAmount(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Export")
	requester := s.seedRequester("export.req@example.test", "Export Requester", "ExportPass12345")
	approved := s.seedPendingRequest(971, requester.ID, admin.ID, headID, 120000)
	s.seedPendingRequest(972, requester.ID, admin.ID, headID, 50000)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/approve", approved),
		url.Values{"approved_amount": {"1000.00"}, "reason": {"Capped at the policy rate"}}), http.StatusSeeOther)

	body := s.get("/requests/export.csv?bucket=all")
	if !strings.HasPrefix(body, "Number,Status,Type,Title,Amount,Approved amount,Payee,") {
		t.Fatalf("export header lacks the approved-amount column: %s", strings.SplitN(body, "\n", 2)[0])
	}
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "PR-2026-000971,"):
			if !strings.Contains(line, ",1200.00,1000.00,") {
				t.Errorf("approved-down row = %q, want requested 1200.00 then approved 1000.00", line)
			}
		case strings.HasPrefix(line, "PR-2026-000972,"):
			if !strings.Contains(line, ",500.00,,") {
				t.Errorf("undecided row = %q, want an empty approved amount", line)
			}
		}
	}
}

// F-G-033 — the heads screen listed every head and populated each row's project
// select from the active projects only, so a head on a retired project had no
// option of its own and that row's Save moved it.
func TestHeadRowsOfferTheirOwnRetiredProject(t *testing.T) {
	s := newAppTestServer(t)
	admin := s.admin()
	_ = admin
	retiredID, err := s.st.UpsertProject(s.ctx, 0, "Retired Programme", true, 5)
	if err != nil {
		t.Fatal(err)
	}
	headID, err := s.st.UpsertHead(s.ctx, 0, retiredID, "Stranded head", "5", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.UpsertProject(s.ctx, retiredID, "Retired Programme", false, 5); err != nil {
		t.Fatal(err)
	}

	s.loginAdmin()
	body := s.get("/heads")
	want := fmt.Sprintf(`<option value="%d" selected>Retired Programme (retired)</option>`, retiredID)
	if !strings.Contains(body, want) {
		t.Fatalf("the row offers no option for the head's own retired project: %s", firstLines(body))
	}

	// Saving the row unchanged leaves the head where it is.
	requireStatus(t, s.postForm("/heads", url.Values{
		"id": {strconvFormat(headID)}, "project_id": {strconvFormat(retiredID)},
		"name": {"Stranded head"}, "due_day": {"5"}, "active": {"on"}, "sort_order": {"1"},
	}), http.StatusSeeOther)
	got := projectOf(t, s, headID)
	if got != retiredID {
		t.Fatalf("project_id = %d after an unchanged Save, want %d", got, retiredID)
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
	projectID := projectOf(t, s, headID)
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
	edit := s.get(fmt.Sprintf("/requests/%d/edit", id))
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
		"project_id": {strconvFormat(projectID)}, "head_id": {strconvFormat(headID)}, "manager_id": {strconvFormat(approver)},
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

// form-6 (T12) — a retired PROJECT (not only a head) disappears from the
// new-request form, and the request raised against it keeps showing its name.
func TestRetiredProjectIsHiddenFromNewRequestsAndKeptOnHistory(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Legacy")
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	approver := seedSecondApprover(t, s)
	projectID := projectOf(t, s, headID)
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{
		Treatment: "budget", Type: "reimbursement", ShortTitle: "Legacy plant visit",
		ProjectID: projectID, HeadID: headID, Amount: 120000, Purpose: "site visit",
		ExpenseDate: "2026-07-01", ManagerID: approver,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.login("rhea@example.test", "RheaPass12345")
	before := s.get("/requests/new?type=vendor_invoice")
	if !strings.Contains(between(t, before, `<select id="project"`, `</select>`), "Operations Legacy") {
		t.Fatal("the live project is not offered on the new-request form, so the retirement below proves nothing")
	}
	if _, err := s.st.UpsertProject(s.ctx, projectID, "Operations Legacy", false, 1); err != nil {
		t.Fatal(err)
	}
	after := s.get("/requests/new?type=vendor_invoice")
	if strings.Contains(between(t, after, `<select id="project"`, `</select>`), "Operations Legacy") {
		t.Fatal("a retired project is still offered for new requests")
	}
	if strings.Contains(between(t, after, `<select id="head"`, `</select>`), "Rent Legacy") {
		t.Fatal("a head under a retired project is still offered for new requests")
	}
	detail := s.get(fmt.Sprintf("/requests/%d", id))
	if !strings.Contains(detail, "<dd>Operations Legacy</dd>") || !strings.Contains(detail, "<dd>Rent Legacy</dd>") {
		t.Fatalf("the historical request lost its retired project or head name: %s", firstLines(detail))
	}
}
