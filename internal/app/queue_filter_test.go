package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestAccountsQueueApprovedTabAndSearchMatchItsCount(t *testing.T) {
	s := newAppTestServer(t)
	accounts, head := s.seedHead("queue filter")
	requester := s.seedColleague("queue-requester@example.test", "Requester", "RequesterPass123")
	open := s.seedApprovedRequest(1, requester.ID, accounts.ID, head, 10000)
	held := s.seedApprovedRequest(2, requester.ID, accounts.ID, head, 20000)
	reserved := s.seedApprovedRequest(3, requester.ID, accounts.ID, head, 30000)
	if err := s.st.HoldRequest(s.ctx, accounts, held, "Documents pending"); err != nil {
		t.Fatal(err)
	}
	if err := s.st.ReserveRequest(s.ctx, accounts, reserved); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	for _, tc := range []struct {
		path string
		want []int64
	}{
		{"/accounts-queue", []int64{open}},
		{"/accounts-queue?tab=approved", []int64{open}},
		{"/accounts-queue?tab=approved&q=PR-2026-000002", nil},
		{"/accounts-queue?tab=approved&q=PR-2026-000003", nil},
		{"/accounts-queue?tab=approved&q=PR-2026-000001", []int64{open}},
		{"/accounts-queue?tab=hold&q=PR-2026-000002", []int64{held}},
		{"/accounts-queue?tab=processing&q=PR-2026-000003", []int64{reserved}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			resp := s.request(http.MethodGet, tc.path, nil, "")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
			body := responseBody(t, resp)
			if !strings.Contains(body, `Approved, unclaimed <span class="n">1</span>`) {
				t.Fatal("approved tab count no longer describes one unclaimed request")
			}
			start := strings.Index(body, "<tbody>")
			end := strings.Index(body, "</tbody>")
			if start < 0 || end < start {
				t.Fatal("queue table missing")
			}
			rows := body[start:end]
			if got := strings.Count(rows, `data-label="Request"`); got != len(tc.want) {
				t.Fatalf("rendered %d request rows, want %v", got, tc.want)
			}
			for _, id := range tc.want {
				if !strings.Contains(rows, fmt.Sprintf(`href="/requests/%d"`, id)) {
					t.Errorf("missing expected request %d", id)
				}
			}
		})
	}
	// The record-payment picker must still show a reservation instead of hiding a
	// possible duplicate when Accounts searches for an invoice already being paid.
	body := responseBody(t, s.request(http.MethodGet, "/payments/new?q=PR-2026-000003", nil, ""))
	if !strings.Contains(body, "PR-2026-000003") || !strings.Contains(body, "is-taken") {
		t.Fatal("payment picker lost the reserved request warning")
	}
}
