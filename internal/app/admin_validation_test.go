package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAdminDefaultApproversFilterAndRejectIneligibleWithLegacyGuidance(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
	admin, _ := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	requester := s.seedRoleUser("default.employee@qa.test", "Default Employee", "DefaultEmployee123", "Requester")
	invalid := s.seedRoleUser("invalid.approver@qa.test", "Not An Approver", "InvalidApprover123", "Requester")
	manager := s.seedRoleUser("valid.approver@qa.test", "Eligible Approver", "ValidApprover123", "Manager")
	body := responseBody(t, s.request("GET", "/users", nil, ""))
	options := strings.Split(strings.Split(body, `<template id="approver-options">`)[1], `</template>`)[0]
	if strings.Contains(options, invalid.Name) || !strings.Contains(options, manager.Name) {
		t.Fatalf("wrong default approver options: %s", options)
	}
	roles, _ := s.st.UserRoles(s.ctx, requester.ID)
	form := url.Values{"id": {strconvFormat(requester.ID)}, "name": {"Should not rename"}, "role": {"data_entry"}, "active": {"on"}, "role_ids": {strconvFormat(roles[0].ID)}, "default_approver_id": {strconvFormat(invalid.ID)}}
	res := s.postForm("/users", form)
	requireStatus(t, res, http.StatusBadRequest)
	res.Body.Close()
	after, _ := s.st.UserByID(s.ctx, requester.ID)
	if after.Name != requester.Name || after.DefaultApproverID != 0 {
		t.Fatal("invalid save changed profile")
	}
	// Represent a historical default, or a formerly eligible approver whose
	// grant was revoked after their assignment.
	if _, err := s.st.DB().Exec(`UPDATE users SET default_approver_id=? WHERE id=?`, invalid.ID, requester.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.st.SetAppSetting(s.ctx, admin, "allow_approver_choice", "0"); err != nil {
		t.Fatal(err)
	}
	s.login(requester.Email, "DefaultEmployee123")
	body = responseBody(t, s.request("GET", "/requests/new?type=reimbursement", nil, ""))
	mustContain(t, "legacy routing", body, "Your default approver is unavailable.", "Ask an administrator", `type="submit" disabled>Submit request`)
	if err := s.st.SetUserDefaultApprover(s.ctx, admin, requester.ID, manager.ID); err != nil {
		t.Fatal(err)
	}
	body = responseBody(t, s.request("GET", "/requests/new?type=reimbursement", nil, ""))
	if strings.Contains(body, "Your default approver is unavailable.") || strings.Contains(body, `type="submit" disabled>Submit request`) {
		t.Fatal("valid routing remains blocked")
	}
}

func TestDuplicateUserEmailIsSpecificAndRetainsCreateForm(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
	existing := s.seedRoleUser("duplicate.email@qa.test", "Existing Person", "ExistingPassword123", "Requester")
	roles, _ := s.st.UserRoles(s.ctx, existing.ID)
	form := url.Values{"name": {"Different Person"}, "email": {" DUPLICATE.EMAIL@QA.TEST "}, "active": {"on"}, "password": {"DuplicatePassword123"}, "role_ids": {strconvFormat(roles[0].ID)}}
	res := s.postForm("/users", form)
	requireStatus(t, res, http.StatusBadRequest)
	body := responseBody(t, res)
	mustContain(t, "duplicate email response", body, "A user with this email address already exists.", "Different Person", `id="user-new" data-open-on-load`)
	if strings.Contains(body, "A record with this name already exists.") || strings.Contains(body, "DuplicatePassword123") {
		t.Fatal("wrong duplicate message or echoed password")
	}
	var n int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM users WHERE email=?`, existing.Email).Scan(&n); err != nil || n != 1 {
		t.Fatalf("duplicate row written n=%d err=%v", n, err)
	}
}

func TestInvalidNotificationRecipientsKeepDrawerAndInput(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
	before, _ := s.st.NotificationSetting(s.ctx, "request_submitted")
	for _, tc := range []struct{ field, value, label string }{{"to_recipients", "not-an-email", "Always also send To"}, {"cc_recipients", "valid@example.test, broken", "Always copy (Cc)"}} {
		form := url.Values{"include_manager": {"on"}, "subject_template": {"Retain my edited subject"}, "body_template": {"Body {{number}}"}, tc.field: {tc.value}}
		res := s.postForm("/admin/notifications/events/request_submitted", form)
		requireStatus(t, res, http.StatusBadRequest)
		body := responseBody(t, res)
		mustContain(t, "invalid recipients response", body, tc.label+" contains an invalid email address", tc.value, "Retain my edited subject", `id="ev-request_submitted" hidden data-reopen`)
		after, _ := s.st.NotificationSetting(s.ctx, "request_submitted")
		if after.SubjectTemplate != before.SubjectTemplate || after.ToRecipients != before.ToRecipients || after.CcRecipients != before.CcRecipients {
			t.Fatal("invalid notification save changed row")
		}
	}
}

// A rejected Add user form comes back on the users page with the drawer open,
// an inline error, and the typed name, email and roles still in place, so
// nothing has to be retyped. The password is deliberately never echoed into HTML.
func TestAddUserValidationKeepsTheDrawerAndTheInput(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
	resp := s.postForm("/users", url.Values{"email": {"nora@example.test"}, "name": {"Nora Test"}, "password": {"NoraPassword123"}, "active": {"on"}})
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	drawer := cut(body, `id="user-new"`, "</form>")
	if drawer == "" || strings.HasPrefix(strings.TrimSpace(strings.SplitN(drawer, ">", 2)[0]), `id="user-new" hidden`) || strings.Contains(strings.SplitN(drawer, ">", 2)[0], "hidden") {
		t.Fatalf("the Add user drawer is not open on the validation answer: %s", strings.SplitN(drawer, ">", 2)[0])
	}
	mustContain(t, "the reopened drawer", drawer, "Choose at least one role for the new user.", `value="nora@example.test"`, `value="Nora Test"`)
	if strings.Contains(body, "NoraPassword123") {
		t.Fatal("the password was echoed back into the page")
	}
	if strings.Contains(body, "error-code") {
		t.Fatal("the full-page error screen answered a form validation error")
	}

	// A role that was ticked stays ticked when the password is what is wrong.
	roleID := s.roleIDByName(t, "Requester")
	resp = s.postForm("/users", url.Values{"email": {"nora@example.test"}, "name": {"Nora Test"}, "password": {"short"}, "role_ids": {roleID}})
	requireStatus(t, resp, http.StatusBadRequest)
	drawer = cut(responseBody(t, resp), `id="user-new"`, "</form>")
	if !strings.Contains(drawer, fmt.Sprintf(`value="%s" checked`, roleID)) {
		t.Fatal("the ticked role was lost on a password error")
	}
}
