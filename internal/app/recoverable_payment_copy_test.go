package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestRecoverableNoPaymentCopyMatchesRequestLifecycle(t *testing.T) {
	for _, tc := range []struct {
		status, want string
		approved     bool
	}{
		{"pending", "Awaiting approval. No payment has been recorded.", false},
		{"returned", "Returned for correction. No payment has been recorded.", false},
		{"rejected", "This request was rejected. No payment was recorded.", false},
		{"cancelled", "This request was cancelled. No payment was recorded.", false},
		{"withdrawn", "This request was withdrawn. No payment was recorded.", false},
		{"approved", "Approved but not yet paid.", true},
		{"processing", "With Accounts for payment. No payment has been recorded.", true},
		{"cancellation_requested", "Cancellation requested. No payment has been recorded.", false},
		{"completed", "No payment has been recorded for this request.", true},
	} {
		t.Run(tc.status, func(t *testing.T) {
			s := newAppTestServer(t)
			_, h := s.seedHead("Lifecycle copy")
			id := seedRecoverable(t, s, h, "PR-NOPAY-"+tc.status, "other", "Test counterparty", "2027-12-31", "", 500000)
			if _, err := s.st.DB().Exec(`UPDATE payment_requests SET status=? WHERE id=?`, tc.status, id); err != nil {
				t.Fatal(err)
			}
			if !tc.approved {
				if _, err := s.st.DB().Exec(`UPDATE payment_requests SET approved_by=NULL,approved_amount=NULL,approved_at=NULL WHERE id=?`, id); err != nil {
					t.Fatal(err)
				}
			}
			s.login(s.cfg.AdminEmail, testAdminPassword)
			resp := s.request(http.MethodGet, fmt.Sprintf("/recoverables/%d", id), nil, "")
			requireStatus(t, resp, http.StatusOK)
			body := responseBody(t, resp)
			start, end := strings.Index(body, "<h2>Latest payment</h2>"), strings.Index(body, `<section id="recovery-history">`)
			if start < 0 || end <= start {
				t.Fatal("missing payment section")
			}
			payment := body[start:end]
			if !strings.Contains(payment, tc.want) {
				t.Fatalf("%s copy = %s; want %q", tc.status, payment, tc.want)
			}
			if tc.status != "approved" && strings.Contains(payment, "Approved but not yet paid") {
				t.Fatalf("%s falsely presented as approved", tc.status)
			}
			if strings.Contains(payment, "Amount paid") || strings.Contains(body, `id="record-recovery"`) {
				t.Fatal("unpaid record offered a paid amount or recovery action")
			}
		})
	}
}
