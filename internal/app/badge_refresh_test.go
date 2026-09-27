package app

import (
	"fervidbudget/internal/store"
	"fmt"
	"net/http"
	"net/url"
	"testing"
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
	resp := s.postForm("/payments", url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-23"}, "amount": {"1.01"}, "settlement": {"settled"}, "reference_no": {"BADGE-101"}, "payment_mode": {"cash"}})
	requireStatus(t, resp, http.StatusSeeOther)
	body := s.get(resp.Header.Get("Location"))
	mustContain(t, "fresh receipt ledger badge", body, `Payments ledger<span class="n">`+strconvFormat(int64(before["my_payments"]+1))+`</span>`)
}
