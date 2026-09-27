package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

const staleDecision = "this request changed; reload and review it before deciding"

// Cancelling outright and deciding a requested cancellation are bound to the
// request revision the approver saw, exactly as approve/return/reject are: a
// change since the cancellation screen was drawn refuses the decision with the
// same message, and a fresh screen lets it through.
func TestCancellationDecisionsAreBoundToTheReviewedRevision(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Cancel revision")
	mgrID := seedSecondApprover(t, s)
	mgr, err := s.st.UserByID(s.ctx, mgrID)
	if err != nil {
		t.Fatal(err)
	}
	s.grantAlso(mgrID, "Approver who may cancel",
		[]store.Grant{{Resource: "approval", Action: "cancel"}},
		[]store.ScopeGrant{{Resource: "request", Scope: "all"}})
	requester := s.seedRequester("cancel-revision@example.test", "Revision Requester", "RequesterPass123")
	approved := func(title string) int64 {
		id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget", Type: "reimbursement",
			ShortTitle: title, ProjectID: 1, HeadID: headID, Amount: 10000, Purpose: "taxi", ManagerID: mgrID, ExpenseDate: "2026-09-26"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.st.ApproveRequest(s.ctx, mgr, id, 10000, ""); err != nil {
			t.Fatal(err)
		}
		return id
	}
	s.login("kavita@example.test", "ApproverPass123")
	screenRevision := func(id int64) string {
		return requestRevisionFromPage(t, s.get(fmt.Sprintf("/requests/%d/cancellation", id)))
	}
	// changeUnderneath is somebody else acting on the request after the
	// approver's screen was drawn.
	changeUnderneath := func(id int64, before string) {
		if _, err := s.st.AddRequestComment(s.ctx, requester, id, "One more detail"); err != nil {
			t.Fatal(err)
		}
		req, err := s.st.Request(s.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if now, _ := s.st.RequestEditRevision(s.ctx, req); now == before {
			t.Fatal("the comment did not move the request revision; the test proves nothing")
		}
	}
	refused := func(path string, form url.Values, id int64, wantStatus string) {
		t.Helper()
		resp := s.postForm(path, form)
		requireStatus(t, resp, http.StatusBadRequest)
		if body := responseBody(t, resp); !strings.Contains(body, staleDecision) {
			t.Fatalf("%s refusal did not explain the stale screen:\n%s", path, body)
		}
		if got, _ := s.st.Request(s.ctx, id); got.Status != wantStatus {
			t.Fatalf("%s: a stale decision changed the status to %q", path, got.Status)
		}
	}

	// --- Outright cancellation. ---
	outright := approved("Outright")
	cancelPath := fmt.Sprintf("/requests/%d/cancel", outright)
	seen := screenRevision(outright)
	changeUnderneath(outright, seen)
	refused(cancelPath, url.Values{"reason": {"Budget pulled"}, "revision": {seen}}, outright, "approved")
	refused(cancelPath, url.Values{"reason": {"Budget pulled"}, "revision": {""}}, outright, "approved")
	requireStatus(t, s.postForm(cancelPath, url.Values{"reason": {"Budget pulled"}, "revision": {screenRevision(outright)}}), http.StatusSeeOther)
	if got, _ := s.st.Request(s.ctx, outright); got.Status != "cancelled" {
		t.Fatalf("fresh outright cancellation left status %q", got.Status)
	}

	// --- Deciding a requested cancellation: decline, then accept. ---
	asked := approved("Asked")
	decidePath := fmt.Sprintf("/requests/%d/cancellation", asked)
	if err := s.st.RequestCancellation(s.ctx, requester, asked, "Order withdrawn"); err != nil {
		t.Fatal(err)
	}
	seen = screenRevision(asked)
	changeUnderneath(asked, seen)
	refused(decidePath, url.Values{"decision": {"decline"}, "note": {"Still needed"}, "revision": {seen}}, asked, "cancellation_requested")
	refused(decidePath, url.Values{"decision": {"accept"}, "revision": {seen}}, asked, "cancellation_requested")
	requireStatus(t, s.postForm(decidePath, url.Values{"decision": {"decline"}, "note": {"Still needed"}, "revision": {screenRevision(asked)}}), http.StatusSeeOther)
	if got, _ := s.st.Request(s.ctx, asked); got.Status != "approved" {
		t.Fatalf("fresh decline left status %q", got.Status)
	}
	if err := s.st.RequestCancellation(s.ctx, requester, asked, "Order withdrawn again"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, s.postForm(decidePath, url.Values{"decision": {"accept"}, "revision": {screenRevision(asked)}}), http.StatusSeeOther)
	if got, _ := s.st.Request(s.ctx, asked); got.Status != "cancelled" {
		t.Fatalf("fresh accept left status %q", got.Status)
	}

	// Every form that posts these decisions carries the revision.
	pending := approved("Forms")
	if err := s.st.RequestCancellation(s.ctx, requester, pending, "Check forms"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{pending, approved("Forms outright")} {
		body := s.get(fmt.Sprintf("/requests/%d/cancellation", id))
		forms := strings.Count(body, `<form class="sheet" method="post" action="/requests/`)
		if forms == 0 || strings.Count(body, `name="revision" value="`) < forms {
			t.Fatalf("request %d: %d decision forms but not all carry the revision", id, forms)
		}
	}
}
