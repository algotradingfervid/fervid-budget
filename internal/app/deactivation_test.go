package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// F-G-024: retiring a head with unfinished requests against it warns, names
// them, and saves only once the administrator confirms. The owner chose
// warn-and-confirm over a hard block (2026-09-25).
func TestDeactivatingAHeadWithOpenRequestsAsksForConfirmation(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Retire")
	requester := s.seedRequester("retire.req@example.test", "Retire Requester", "RetirePass12345")
	s.seedPendingRequest(951, requester.ID, admin.ID, headID, 120000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
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
	s.login(s.cfg.AdminEmail, testAdminPassword)

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
