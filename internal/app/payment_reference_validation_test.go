package app

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestLinkedPaymentReferencesRequireMeaningfulTextForEveryMode(t *testing.T) {
	s := newAppTestServer(t)
	admin, head := s.seedHead("Reference validation")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	for i, mode := range []string{"neft", "rtgs", "upi", "cheque", "cash", "card", "dd"} {
		t.Run(mode, func(t *testing.T) {
			id := s.seedApprovedRequest(i+1, admin.ID, admin.ID, head, 101)
			requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", id), url.Values{}), http.StatusSeeOther)
			form := url.Values{"request_id": {strconvFormat(id)}, "amount": {"1.01"}, "paid_on": {"2026-06-15"}, "payment_mode": {mode}, "settlement": {"settled"}, "reference_no": {"   "}}
			resp := s.postForm("/payments", form)
			body := responseBody(t, resp)
			if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "spaces alone are not a reference") {
				t.Fatalf("status%d missing reference refusal: %s", resp.StatusCode, body)
			}
			rows, err := s.st.RequestPayments(s.ctx, id)
			if err != nil || len(rows) != 0 {
				t.Fatalf("rejected reference wrote a payment: %v %v", rows, err)
			}
			form.Set("reference_no", "  REF-"+mode+"  ")
			requireStatus(t, s.postForm("/payments", form), http.StatusSeeOther)
			rows, err = s.st.RequestPayments(s.ctx, id)
			if err != nil || len(rows) != 1 || rows[0].ReferenceNo != "REF-"+mode {
				t.Fatalf("corrected reference not trimmed/persisted: %v %v", rows, err)
			}
		})
	}
}
