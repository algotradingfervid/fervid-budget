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
	s.loginAdmin()
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
		resp := s.submitScreen(endpoint, url.Values{"approved_amount": {amount}, "note": {note}})
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
	s.loginAdmin()
	resp := s.submitScreen(endpoint, url.Values{"approved_amount": {"not money"}, "note": {note}})
	requireStatus(t, resp, http.StatusNotFound)
	if strings.Contains(responseBody(t, resp), `id="approve-sheet"`) {
		t.Fatal("unassigned approver received approval editor")
	}

	s.login("kavita@example.test", "ApproverPass123")
	resp = s.submitScreen(endpoint, url.Values{"approved_amount": {"8403.97"}, "note": {note}})
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

// F-B-08 — every validation banner opened with the internal sentinel
// "validation failed: ", which is not a sentence for a person.
func TestValidationMessagesLoseTheSentinelPrefix(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
	resp := s.postForm("/heads", url.Values{"project_id": {"0"}, "name": {""}, "due_day": {"5"}})
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	if strings.Contains(body, "validation failed") {
		t.Fatalf("the banner still carries the sentinel: %s", firstLines(body))
	}
	if !strings.Contains(body, "project and head name are required") {
		t.Fatalf("the banner lost the rule with the sentinel: %s", firstLines(body))
	}
}

// F-E-08 / F-B-02 — a refused `type=recoverable` submission looked up a UI label
// it has none of and answered "That is not a kind of request this system raises."
// about a type the store had accepted seconds earlier, throwing the form away.
func TestARefusedUnlabelledTypeKeepsItsRealReason(t *testing.T) {
	s := newAppTestServer(t)
	admin := s.admin()
	approver := seedSecondApprover(t, s)
	_ = admin
	s.loginAdmin()
	resp := s.postForm("/requests", url.Values{
		"type": {"recoverable"}, "treatment": {"recoverable"},
		"recoverable_category": {"pbg"}, "short_title": {"Bank guarantee"},
		"amount": {"1,00,000.00"}, "purpose": {"guarantee against the tender"},
		"expected_return_date": {"2027-01-01"}, "repayment_notes": {"Released on completion"},
		"manager_id": {strconvFormat(approver)},
	})
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	if strings.Contains(body, "not a kind of request this system raises") {
		t.Fatalf("the refusal is about the wrong problem: %s", firstLines(body))
	}
	if !strings.Contains(body, "belongs to a project") {
		t.Fatalf("the rule that actually refused it is not reported: %s", firstLines(body))
	}
}

// F-D-14 — `/requests/new?type=recoverable` used to answer the chooser with no
// explanation at all, and then (F-D-14's first reconciliation) a 400 on the
// premise that "recoverable" was a treatment every card carried. It was not:
// only the employee advance carried the radio, and that type pays the
// requester, so the store's fifth type has its own card now (form-1). A ?type=
// naming nothing is still a 400 on the chooser with the reason above it.
func TestATypeWithNoCardIsRefusedRatherThanSilentlyBounced(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()

	// No ?type= at all is step 1 of 2, unchanged and not an error.
	chooser := s.request(http.MethodGet, "/requests/new", nil, "")
	requireStatus(t, chooser, http.StatusOK)
	if body := responseBody(t, chooser); strings.Contains(body, "alert error") {
		t.Fatalf("the plain chooser reports an error: %s", firstLines(body))
	}

	resp := s.request(http.MethodGet, "/requests/new?type=mystery", nil, "")
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	if !strings.Contains(body, "not a request type this system raises") {
		t.Fatalf("?type=mystery was bounced without an explanation: %s", firstLines(body))
	}
	// It is still the chooser: the reader lands on the cards they can actually
	// use, with the reason above them.
	if !strings.Contains(body, `class="type-grid"`) || strings.Count(body, `class="type-card"`) != 5 {
		t.Fatalf("?type=mystery did not render the five-card chooser: %s", firstLines(body))
	}

	// Every store type has a card, so the two vocabularies cannot drift apart
	// again without this failing.
	if _, offered := requestTypeLabels["recoverable"]; !offered {
		t.Fatal("the store's recoverable type has no chooser card; a deposit can only be raised as an employee advance again")
	}
	form := s.request(http.MethodGet, "/requests/new?type=recoverable", nil, "")
	requireStatus(t, form, http.StatusOK)
	if body := responseBody(t, form); !strings.Contains(body, `name="vendor_payee"`) || !strings.Contains(body, `<input type="hidden" name="treatment" value="recoverable">`) {
		t.Fatalf("the deposit form does not ask who is paid on a fixed recoverable treatment: %s", firstLines(body))
	}
}
