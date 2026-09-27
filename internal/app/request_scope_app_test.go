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
			resp := s.postForm(fmt.Sprintf(path, target), form)
			requireStatus(t, resp, http.StatusNotFound)
			_ = responseBody(t, resp)
		}
	}
	if got := requestStatusApp(t, s, id); got != "approved" {
		t.Fatalf("a hidden request was changed by an out-of-scope caller: status %q", got)
	}
}
