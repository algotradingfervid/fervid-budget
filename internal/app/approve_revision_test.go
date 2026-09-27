package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// Approving is bound to the request revision the approver saw, like return,
// reject and the cancellation decisions: a stale or missing revision is refused
// with the stale-screen message and leaves the request pending, and the
// revision from a fresh screen lets the approval through.
func TestApproveIsBoundToTheReviewedRevision(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Approve revision")
	mgrID := seedSecondApprover(t, s)
	requester := s.seedRequester("approve-revision@example.test", "Approve Requester", "RequesterPass123")
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "Approve revision", ProjectID: 1, HeadID: headID, Amount: 10000, Purpose: "taxi", ManagerID: mgrID, ExpenseDate: "2026-09-26"})
	if err != nil {
		t.Fatal(err)
	}
	s.login("kavita@example.test", "ApproverPass123")
	path := fmt.Sprintf("/requests/%d/approve", id)
	screen := func() string {
		return requestRevisionFromPage(t, responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", id), nil, "")))
	}
	seen := screen()
	if _, err := s.st.AddRequestComment(s.ctx, requester, id, "One more detail"); err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{seen, ""} {
		resp := s.postForm(path, url.Values{"approved_amount": {"100.00"}, "revision": {revision}})
		requireStatus(t, resp, http.StatusBadRequest)
		if body := responseBody(t, resp); !strings.Contains(body, staleDecision) {
			t.Fatalf("revision %q: refusal did not explain the stale screen:\n%s", revision, body)
		}
		if got, _ := s.st.Request(s.ctx, id); got.Status == "approved" {
			t.Fatalf("revision %q: a stale approval went through", revision)
		}
	}
	requireStatus(t, s.postForm(path, url.Values{"approved_amount": {"100.00"}, "revision": {screen()}}), http.StatusSeeOther)
	if got, _ := s.st.Request(s.ctx, id); got.Status != "approved" {
		t.Fatalf("fresh approval left status %q", got.Status)
	}
}
