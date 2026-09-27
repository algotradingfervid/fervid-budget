package app

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

func TestRoleCreateValidationPreservesDialogAndDoesNotCreateDuplicate(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	before, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	selected := before[len(before)-1].ID
	for _, name := range []string{"Accounts", "  accounts  ", "   "} {
		resp := s.postForm("/roles/new", url.Values{
			"name": {name}, "description": {"Keep <script>alert(1)</script> & my description"},
			"selected_role_id": {strconvFormat(selected)},
		})
		requireStatus(t, resp, http.StatusBadRequest)
		body := responseBody(t, resp)
		for _, want := range []string{
			`id="role-new" data-open-on-load`, `value="` + name + `"`,
			`aria-invalid="true" aria-describedby="nr-error" data-dialog-initial-focus`,
			`value="Keep &lt;script&gt;alert(1)&lt;/script&gt; &amp; my description"`,
			`name="selected_role_id" value="` + strconvFormat(selected) + `"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("refused create lacks %q", want)
			}
		}
		if name != "   " && !strings.Contains(body, "A role with this name already exists. Choose a different name.") {
			t.Error("missing actionable duplicate-name error")
		}
		after, err := s.st.AllRoles(s.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) {
			t.Fatal("invalid role create changed role count")
		}
	}
	// A corrected retry remains a normal create, with no stale validation state.
	resp := s.postForm("/roles/new", url.Values{"name": {"Recovered role"}, "description": {"Retained description"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	after, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 {
		t.Fatal("corrected role was not created exactly once")
	}
}

func TestApprovalValidationPreservesDialogAndNeverCommitsInvalidAmounts(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Approval validation")
	managerID := seedSecondApprover(t, s)
	requester := s.seedRequester("validation@example.invalid", "Validation requester", "RequesterPass123")
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{
		Treatment: "budget", Type: "reimbursement", ShortTitle: "Contextual approval",
		ProjectID: 1, HeadID: headID, Amount: 840397, Purpose: "Retain approval context",
		ExpenseDate: "2026-07-17", ManagerID: managerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeThread, err := s.st.RequestThread(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	s.login("kavita@example.test", "ApproverPass123")
	endpoint := "/requests/" + strconvFormat(id) + "/approve"
	note := "Retain <approval> & explanation"
	for _, amount := range []string{"8403.98", "0", "-1.00", "not money", ""} {
		resp := s.postForm(endpoint, url.Values{"approved_amount": {amount}, "note": {note}})
		requireStatus(t, resp, http.StatusBadRequest)
		body := responseBody(t, resp)
		for _, want := range []string{
			`id="approve-sheet" data-open-on-load`, `id="ap-amount"`, `value="` + amount + `"`,
			`aria-invalid="true" aria-describedby="ap-error" data-dialog-initial-focus`,
			`Retain &lt;approval&gt; &amp; explanation</textarea>`, `id="ap-error"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("amount %q: refused approval lacks %q", amount, want)
			}
		}
		got, err := s.st.Request(s.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "pending" || got.ApprovedAmount != nil {
			t.Fatalf("invalid amount %q changed request: %+v", amount, got)
		}
		thread, err := s.st.RequestThread(s.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(thread) != len(beforeThread) {
			t.Fatal("invalid approval appended history")
		}
	}
	// An unassigned approver cannot use validation to expose an actionable dialog.
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.postForm(endpoint, url.Values{"approved_amount": {"not money"}, "note": {note}})
	requireStatus(t, resp, http.StatusNotFound)
	if strings.Contains(responseBody(t, resp), `id="approve-sheet"`) {
		t.Fatal("unassigned approver received approval editor")
	}

	s.login("kavita@example.test", "ApproverPass123")
	resp = s.postForm(endpoint, url.Values{"approved_amount": {"8403.97"}, "note": {note}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	got, err := s.st.Request(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "approved" || got.ApprovedAmount == nil || *got.ApprovedAmount != 840397 {
		t.Fatal("corrected valid approval did not commit")
	}
}
