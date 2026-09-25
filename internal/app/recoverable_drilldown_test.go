package app

import (
	"fervidbudget/internal/store"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestRecoverableDashboardDrilldownsMatchOutstandingPopulation(t *testing.T) {
	s := newAppTestServer(t)
	admin, h := s.seedHead("Recovery drilldown")
	outstanding := seedRecoverable(t, s, h, "PR-DRILL-PAID", "icd", "Same Counterparty", "2027-06-01", "2026-02-01", 700000)
	seedRecoverable(t, s, h, "PR-DRILL-UNPAID", "icd", "Same Counterparty", "2027-06-01", "", 800000)
	recovered := seedRecoverable(t, s, h, "PR-DRILL-RECOVERED", "icd", "Same Counterparty", "2027-06-01", "2026-02-01", 300000)
	for i, item := range []struct{ id, amount int64 }{{outstanding, 200000}, {recovered, 300000}} {
		if _, err := s.st.RecordRecovery(s.ctx, admin, item.id, store.RecoveryInput{Kind: "return", Amount: item.amount, OccurredOn: "2026-02-02", Reference: fmt.Sprintf("DRILL-%d", i), Note: "Bank return evidenced by the statement", Token: fmt.Sprintf("drilldown-recovery-token-%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	dashboard := responseBody(t, s.request(http.MethodGet, "/recoverables", nil, ""))
	link := func(label, value string) string {
		t.Helper()
		matches := regexp.MustCompile(`data-label="` + label + `"><a href="([^"]+)">` + value + `</a>`).FindStringSubmatch(dashboard)
		if len(matches) != 2 {
			t.Fatalf("missing %s drilldown", label)
		}
		return html.UnescapeString(matches[1])
	}
	count := func(body string) int {
		return len(regexp.MustCompile(`<a href="/recoverables/[0-9]+">`).FindAllString(body, -1))
	}
	for _, href := range []string{link("Category", "ICD"), link("Counterparty", "Same Counterparty")} {
		body := responseBody(t, s.request(http.MethodGet, href, nil, ""))
		if count(body) != 1 {
			t.Fatalf("dashboard count 1 links to %d records at %s; unpaid/reconciled must not widen the drilldown", count(body), href)
		}
		if !strings.Contains(body, "PR-DRILL-PAID") || strings.Contains(body, "PR-DRILL-UNPAID") || strings.Contains(body, "PR-DRILL-RECOVERED") {
			t.Fatal("wrong drilldown population")
		}
		if !strings.Contains(body, `data-label="Outstanding">₹5,000.00</td>`) {
			t.Fatal("wrong outstanding amount")
		}
	}
	var cat int64
	if err := s.st.DB().QueryRow(`SELECT id FROM recoverable_categories WHERE code='icd'`).Scan(&cat); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		filter string
		want   int
	}{{"", 3}, {"&ageing=unpaid", 1}, {"&ageing=recovered", 1}, {"&ageing=outstanding", 1}} {
		body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/recoverables/list?category=%d%s", cat, tc.filter), nil, ""))
		if count(body) != tc.want {
			t.Fatalf("view %q got%d want%d", tc.filter, count(body), tc.want)
		}
	}
}
