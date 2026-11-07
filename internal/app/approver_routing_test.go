package app

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"fervidbudget/internal/notify"
)

// approverFieldError is the per-field message the request forms show on the
// approver control when the store refuses a non-default approver.
const approverFieldError = "Requests must go to your default approver."

// With "Let the employee choose a different approver" off, the real POSTs
// enforce it: a hand-made create or edit naming anybody but the requester's
// default approver is refused with the message on the approver field, and the
// edit form, like the create form, offers no choice.
func TestApproverChoiceOffIsEnforcedOnTheRequestForms(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Routing")
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	kavita := seedSecondApprover(t, s)
	rakesh := seedThirdApprover(t, s)
	if err := s.st.SetUserDefaultApprover(s.ctx, admin, requester.ID, kavita); err != nil {
		t.Fatal(err)
	}
	if err := s.st.SetAppSetting(s.ctx, admin, "allow_approver_choice", "0"); err != nil {
		t.Fatal(err)
	}
	form := func(managerID int64) url.Values {
		return url.Values{
			"type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Team lunch"},
			"amount": {"500"}, "purpose": {"team lunch"}, "expense_date": {"2026-07-21"},
			"project_id": {strconvFormat(projectOf(t, s, headID))}, "head_id": {strconvFormat(headID)},
			"manager_id": {strconvFormat(managerID)},
		}
	}
	s.login("rhea@example.test", "RheaPass12345")

	refused := s.postForm("/requests", form(rakesh))
	requireStatus(t, refused, http.StatusBadRequest)
	page := responseBody(t, refused)
	mustContain(t, "refused create", page,
		`<span class="field-error-message" id="error-approver">`+approverFieldError+`</span>`,
		`href="#approver">`+approverFieldError)
	var n int
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT COUNT(*) FROM payment_requests WHERE manager_id=?`, rakesh).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a request routed to a non-default approver was saved (%d rows)", n)
	}

	accepted := s.postForm("/requests", form(kavita))
	requireStatus(t, accepted, http.StatusSeeOther)
	_ = responseBody(t, accepted)
	var id int64
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT id FROM payment_requests WHERE requester_id=?`, requester.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}

	// The edit form disables the select and posts the request's approver.
	edit := s.get(fmt.Sprintf("/requests/%d/edit", id))
	mustContain(t, "edit form with choice off", edit,
		`<select id="apr" name="manager_id" required disabled`,
		fmt.Sprintf(`<input type="hidden" name="manager_id" value="%d">`, kavita))

	resp := s.submitScreen(fmt.Sprintf("/requests/%d/edit", id), form(rakesh))
	requireStatus(t, resp, http.StatusBadRequest)
	mustContain(t, "refused edit", responseBody(t, resp),
		`<span class="field-error-message" id="error-approver">`+approverFieldError+`</span>`)
	var managerID int64
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT manager_id FROM payment_requests WHERE id=?`, id).Scan(&managerID); err != nil {
		t.Fatal(err)
	}
	if managerID != kavita {
		t.Fatalf("the refused edit moved the request to %d", managerID)
	}

	// With choice on again, the edit form's select is live.
	if err := s.st.SetAppSetting(s.ctx, admin, "allow_approver_choice", "1"); err != nil {
		t.Fatal(err)
	}
	edit = s.get(fmt.Sprintf("/requests/%d/edit", id))
	mustNotContain(t, "edit form with choice on", edit, `required disabled`, `<input type="hidden" name="manager_id"`)
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/edit", id), form(rakesh)), http.StatusSeeOther)
}

// Re-raising through the real route is refused, with the message a new
// request would get, once the request's head has been retired; nothing new is
// written.
func TestReraiseRouteRefusesARetiredHead(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Reraise retired")
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	rejected := s.seedPendingRequest(931, requester.ID, admin.ID, headID, 130000)
	s.loginAdmin()
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/reject", rejected),
		url.Values{"reason": {"Wrong month"}}), http.StatusSeeOther)
	if _, err := s.st.DB().ExecContext(s.ctx, `UPDATE heads SET active=0 WHERE id=?`, headID); err != nil {
		t.Fatal(err)
	}

	s.login("rhea@example.test", "RheaPass12345")
	resp := s.postForm(fmt.Sprintf("/requests/%d/reraise", rejected), url.Values{})
	requireStatus(t, resp, http.StatusBadRequest)
	mustContain(t, "refused re-raise", responseBody(t, resp), "that budget head has been retired")
	var n int
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT COUNT(*) FROM payment_requests`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("refused re-raise left %d requests, want only the rejected one", n)
	}
}

// A re-raised urgent request is a new submission, so it raises the urgent
// event for its approver just as raising it the first time did.
func TestReraisedUrgentRequestRaisesTheUrgentEvent(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Reraise urgent")
	requester := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass12345")
	rejected := s.seedPendingRequest(941, requester.ID, admin.ID, headID, 130000)
	if _, err := s.st.DB().ExecContext(s.ctx, `UPDATE payment_requests SET urgent=1, urgency_reason='travel tomorrow' WHERE id=?`, rejected); err != nil {
		t.Fatal(err)
	}
	s.loginAdmin()
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/reject", rejected),
		url.Values{"reason": {"Wrong month"}}), http.StatusSeeOther)
	if hasEvent(notificationTitles(t, s, admin.ID), notify.EventRequestUrgent) {
		t.Fatal("fixture already carries an urgent notification")
	}

	s.login("rhea@example.test", "RheaPass12345")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/reraise", rejected), url.Values{}), http.StatusSeeOther)
	if !hasEvent(notificationTitles(t, s, admin.ID), notify.EventRequestUrgent) {
		t.Fatal("re-raising an urgent request did not raise the urgent event")
	}
}
