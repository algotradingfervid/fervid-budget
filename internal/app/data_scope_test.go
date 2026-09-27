package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// An approver holding the "assigned" request scope sees the requests they
// raised themselves in the request list, because the scope reaches them, but
// their approval queue lists only the requests routed to them as approver.
func TestAssignedApproverSeesOwnRequestInListButNotInApprovalQueue(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Assigned scope")
	approver := s.seedProbeUser("assigned-approver@example.test", "Assigned Approver", "ApproverPass1234", "Assigned approver",
		[]store.Grant{{Resource: "request", Action: "view"}, {Resource: "request", Action: "create"}, {Resource: "approval", Action: "approve"}},
		[]store.ScopeGrant{{Resource: "request", Scope: store.ScopeAssigned}})
	number := func(seq int) string { return fmt.Sprintf("PR-2026-%06d", seq) }
	raisedID, raised := s.seedPendingRequest(101, approver.ID, admin.ID, headID, 250000), number(101)
	routedID, routed := s.seedPendingRequest(102, admin.ID, approver.ID, headID, 250000), number(102)
	s.seedPendingRequest(103, admin.ID, admin.ID, headID, 250000)
	stranger := number(103)

	s.login("assigned-approver@example.test", "ApproverPass1234")
	list := s.get("/requests?bucket=open")
	if !strings.Contains(list, raised) || !strings.Contains(list, routed) || strings.Contains(list, stranger) {
		t.Fatalf("assigned-scope list should show the raised and routed requests only:\n%s", list)
	}
	for _, id := range []int64{raisedID, routedID} {
		requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", id), nil, ""), http.StatusOK)
	}

	queue := s.get("/approvals")
	if !strings.Contains(queue, routed) {
		t.Fatalf("approval queue misses the request routed to the approver:\n%s", queue)
	}
	if strings.Contains(queue, raised) {
		t.Fatalf("approval queue lists the approver's own raised request:\n%s", queue)
	}
}

// A request outside the caller's data scope is answered 404 everywhere — on the
// detail page, on the register, and on every state-changing POST, including the
// ones whose check runs inside the store — exactly like a request that does not
// exist. A distinguishable refusal would let anybody walk the id space.
func TestOutOfScopeRequestAnswersNotFoundOnReadsAndWrites(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Hidden request")
	id := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET treatment='recoverable', type='employee_advance', recoverable_category='employee_advance' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	s.seedProbeUser("own-scoped@example.test", "Own Scoped", "OwnScopedPass1234", "Own-scoped everything",
		[]store.Grant{
			{Resource: "request", Action: "view"}, {Resource: "approval", Action: "approve"}, {Resource: "approval", Action: "reject"},
			{Resource: "reservation", Action: "reserve"}, {Resource: "payment", Action: "create"},
			{Resource: "recoverable_report", Action: "view"},
		},
		[]store.ScopeGrant{{Resource: "request", Scope: store.ScopeOwn}})
	s.login("own-scoped@example.test", "OwnScopedPass1234")

	const missing = 987654
	recovery := url.Values{"kind": {"return"}, "amount": {"100"}, "occurred_on": {"2026-02-02"}, "reference": {"UTR-1"}, "note": {"Proof"}, "token": {"hidden-request-probe-token"}}
	for _, target := range []int64{id, missing} {
		for _, path := range []string{"/requests/%d", "/recoverables/%d"} {
			resp := s.request(http.MethodGet, fmt.Sprintf(path, target), nil, "")
			requireStatus(t, resp, http.StatusNotFound)
			_ = responseBody(t, resp)
		}
		for path, form := range map[string]url.Values{
			"/requests/%d/approve":        {},
			"/requests/%d/reject":         {"comment": {"No"}},
			"/requests/%d/record-payment": {},
			"/recoverables/%d/events":     recovery,
		} {
			resp := s.submitScreen(fmt.Sprintf(path, target), form)
			requireStatus(t, resp, http.StatusNotFound)
			_ = responseBody(t, resp)
		}
	}
	if got := requestStatusApp(t, s, id); got != "approved" {
		t.Fatalf("a hidden request was changed by an out-of-scope caller: status %q", got)
	}
}

// F-G-002 — an out-of-scope request answered 403 while a missing one answered
// 404, so the status code enumerated the id space. The two are now identical.
func TestOutOfScopeRequestIsIndistinguishableFromAMissingOne(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Oracle")
	owner := s.seedRequester("owner@example.test", "Owner Rao", "OwnerPass12345")
	s.seedRequester("stranger@example.test", "Stranger Rao", "StrangerPass1234")
	id := s.seedApprovedRequest(941, owner.ID, admin.ID, headID, 100000)

	s.login("stranger@example.test", "StrangerPass1234")
	existing := s.request(http.MethodGet, fmt.Sprintf("/requests/%d", id), nil, "")
	missing := s.request(http.MethodGet, "/requests/99999999", nil, "")
	if existing.StatusCode != missing.StatusCode {
		t.Fatalf("an existing out-of-scope request answers %d and a missing one %d — the status code is an existence oracle",
			existing.StatusCode, missing.StatusCode)
	}
	if existing.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", existing.StatusCode)
	}
	if body := responseBody(t, existing); strings.Contains(body, "Office rent") {
		t.Fatal("the refusal echoes a field of the request it refused")
	}
	_ = responseBody(t, missing)
}

// rbac-2: a request scope of None (no role_data_scope row) must mean no rows,
// on the list and the CSV alike, which is what the detail page already says
// with its 404. The same rule applies to the payment ledger and its detail page.
func TestRequestScopeNoneShowsNoRowsAnywhere(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Scope")
	reqID := s.seedPendingRequest(1, admin.ID, admin.ID, headID, 1200)
	s.seedProbeUser("carl@example.test", "Carl", "CarlPass12345", "Viewer",
		[]store.Grant{{Resource: "request", Action: "view"}, {Resource: "grid", Action: "view"}}, nil)

	s.login("carl@example.test", "CarlPass12345")
	body := s.get("/requests?bucket=all")
	if strings.Contains(body, "PR-2026-000001") {
		t.Fatalf("a viewer with no request scope sees the list: %s", body)
	}
	csv := s.get("/requests/export.csv?bucket=all")
	if strings.Contains(csv, "PR-2026-000001") {
		t.Fatalf("a viewer with no request scope gets rows in the export: %s", csv)
	}
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""), http.StatusNotFound)
	// The dashboard's own count of the scope agrees.
	requireStatus(t, s.request(http.MethodGet, "/", nil, ""), http.StatusOK)
}

func TestPaymentScopeNoneShowsNoLedgerRows(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Ledger")
	payID, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{HeadID: headID, PaidOn: "2026-06-03", Amount: 4200, VendorPayee: "Scope Probe Payee"})
	if err != nil {
		t.Fatal(err)
	}
	s.seedProbeUser("dana@example.test", "Dana", "DanaPass12345", "Ledger Viewer",
		[]store.Grant{{Resource: "payment", Action: "view"}}, nil)

	s.login("dana@example.test", "DanaPass12345")
	body := s.get("/payments?month=2026-06&status=all")
	if strings.Contains(body, "Scope Probe Payee") {
		t.Fatalf("a viewer with no payment scope sees the ledger: %s", body)
	}
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/payments/%d", payID), nil, ""), http.StatusNotFound)

	// And a scope of All still shows the row, so the ledger was not locked shut.
	s.seedProbeUser("erin@example.test", "Erin", "ErinPass12345", "Ledger Reader",
		[]store.Grant{{Resource: "payment", Action: "view"}},
		[]store.ScopeGrant{{Resource: "payment", Scope: store.ScopeAll}})
	s.login("erin@example.test", "ErinPass12345")
	body = s.get("/payments?month=2026-06&status=all")
	if !strings.Contains(body, "Scope Probe Payee") {
		t.Fatalf("a viewer with scope All lost the ledger: %s", body)
	}
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/payments/%d", payID), nil, ""), http.StatusOK)
}

func TestPaymentReservationDoesNotSurviveRevokedRequestScope(t *testing.T) {
	s := newAppTestServer(t)
	admin, head := s.seedHead("Revoked payout scope")
	id := s.seedApprovedRequest(1, admin.ID, admin.ID, head, 1200000)
	grants := []store.Grant{{Resource: "payment", Action: "settle"}, {Resource: "reservation", Action: "reserve"}, {Resource: "request", Action: "view"}, {Resource: "payment", Action: "create"}, {Resource: "payment", Action: "process"}, {Resource: "payment", Action: "view"}}
	actor := s.seedProbeUser("scope-payout@example.test", "Scoped Accounts", "ScopePayout123", "Scoped Accounts", grants, []store.ScopeGrant{{Resource: "request", Scope: "all"}, {Resource: "payment", Scope: "all"}})
	roles, err := s.st.UserRoles(s.ctx, actor.ID)
	if err != nil || len(roles) != 1 {
		t.Fatalf("roles=%v error=%v", roles, err)
	}
	s.login(actor.Email, "ScopePayout123")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", id), url.Values{}), http.StatusSeeOther)
	if err := s.st.UpdateRolePermissions(s.ctx, admin, roles[0].ID, grants, []store.ScopeGrant{{Resource: "request", Scope: "own"}, {Resource: "payment", Scope: "all"}}); err != nil {
		t.Fatal(err)
	}
	// Keep the same session: role changes must take effect on the next request.
	for _, path := range []string{fmt.Sprintf("/requests/%d", id), fmt.Sprintf("/payments/new?request=%d", id)} {
		resp := s.request(http.MethodGet, path, nil, "")
		body := responseBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("revoked scope GET %s status=%d body bytes=%d, want404", path, resp.StatusCode, len(body))
		}
	}
	form := url.Values{"request_id": {fmt.Sprint(id)}, "paid_on": {"2026-06-15"}, "amount": {"7000"}, "settlement": {"installment"}, "submission_key": {"revoked-scope-form"}, "reference_no": {"RESTORED-SCOPE-REF"}, "expected_paid": {"0"}}
	preview := s.postForm(fmt.Sprintf("/requests/%d/settlement-preview", id), form)
	requireStatus(t, preview, http.StatusNotFound)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", id), url.Values{}), http.StatusNotFound)
	resp := s.postPayment(form)
	_ = responseBody(t, resp)
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusForbidden {
		t.Errorf("payout after scope revocation status=%d, want403/404", resp.StatusCode)
	}
	var count int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM payments WHERE request_id=?`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("revoked scope still created %d payout", count)
	}
	// Restoring visibility makes the original reservation usable again; the fix
	// must not turn all held payments into a permanent refusal.
	if err := s.st.UpdateRolePermissions(s.ctx, admin, roles[0].ID, grants, []store.ScopeGrant{{Resource: "request", Scope: "all"}, {Resource: "payment", Scope: "all"}}); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, s.postPayment(form), http.StatusSeeOther)

}
