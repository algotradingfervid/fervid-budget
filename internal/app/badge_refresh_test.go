package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

func TestPaymentReceiptImmediatelyRefreshesLedgerBadge(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Immediate badge")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 101)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	before, err := s.st.BadgeCounts(s.ctx, admin.ID, store.NewPermissionSet(store.AllGrants(), nil))
	if err != nil {
		t.Fatal(err)
	}
	resp := s.postPayment(url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-23"}, "amount": {"1.01"}, "settlement": {"settled"}, "reference_no": {"BADGE-101"}, "payment_mode": {"cash"}})
	requireStatus(t, resp, http.StatusSeeOther)
	body := s.get(resp.Header.Get("Location"))
	mustContain(t, "fresh receipt ledger badge", body, `Payments ledger<span class="n">`+strconvFormat(int64(before["my_payments"]+1))+`</span>`)
}

// F-G-013 — navSpec declared Badge:"approvals" and Badge:"accounts_queue" and no
// query ever populated them, so the two counts a person most needs at a glance
// were the two that were never built.
func TestTheTwoDeclaredWorkQueueBadgesArePopulated(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Badges")
	requester := s.seedRequester("badge@example.test", "Badge Requester", "BadgePass12345")
	s.seedPendingRequest(1501, requester.ID, admin.ID, headID, 100000)
	s.seedApprovedRequest(1502, requester.ID, admin.ID, headID, 110000)

	s.loginAdmin()
	body := s.get("/")
	for _, want := range []string{`href="/approvals"`, `href="/accounts-queue"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("the nav item %s is missing", want)
		}
	}
	// One pending approval and one takeable request, so both badges read 1.
	if strings.Count(body, `<span class="n">1</span>`) < 2 {
		t.Fatalf("the approvals and accounts-queue badges are not populated: %s", firstLines(body))
	}
}
