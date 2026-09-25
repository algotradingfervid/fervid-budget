package app

import (
	"encoding/csv"
	"fervidbudget/internal/store"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func reportAnchor(t *testing.T, body, text string) string {
	t.Helper()
	re := regexp.MustCompile(`<a[^>]*href="([^"]+)"[^>]*>` + regexp.QuoteMeta(text) + `</a>`)
	m := re.FindStringSubmatch(body)
	if len(m) != 2 {
		t.Fatalf("missing report anchor %q", text)
	}
	return html.UnescapeString(m[1])
}

func TestReportCurrentMonthKeepsSelectedScopeAndBack(t *testing.T) {
	s := newAppTestServer(t)
	actor, head := s.seedHead("KeepScope")
	_, other := s.seedHead("OtherScope")
	for id, amount := range map[int64]int64{head: 12300, other: 87600} {
		if err := s.st.SetBudget(s.ctx, actor, id, "2026-04", amount); err != nil {
			t.Fatal(err)
		}
	}
	var project int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, head).Scan(&project); err != nil {
		t.Fatal(err)
	}
	if err := s.st.SetBudget(s.ctx, actor, other, currentMonthForTest(), 87600); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	origin := "/reports/heads?from=2026-01&to=2026-06"
	for _, headID := range []int64{0, head} {
		t.Run(fmt.Sprint(headID), func(t *testing.T) {
			scope := url.Values{"from": {"2026-04"}, "to": {"2026-04"}, "project_id": {fmt.Sprint(project)}, "back": {origin}}
			mode := "projects"
			if headID != 0 {
				mode = "heads"
				scope.Set("head_id", fmt.Sprint(headID))
			}
			page := responseBody(t, s.request(http.MethodGet, "/reports/"+mode+"?"+scope.Encode(), nil, ""))
			current := reportAnchor(t, page, "Current month")
			t.Logf("Rendered Current month href: %s", current)
			moved := responseBody(t, s.request(http.MethodGet, current, nil, ""))
			t.Logf("Following Current month: unrelated project visible=%v; selected context visible=%v", strings.Contains(cut(moved, "<tbody>", "</tbody>"), "OtherScope"), strings.Contains(moved, "Showing: "))
			u, err := url.Parse(current)
			if err != nil {
				t.Fatal(err)
			}
			q := u.Query()
			if q.Get("project_id") != fmt.Sprint(project) || q.Get("back") != origin || (headID > 0 && q.Get("head_id") != fmt.Sprint(headID)) {
				t.Fatalf("Current month silently broadens selected report: %s", current)
			}
			if q.Get("from") != currentMonthForTest() || q.Get("to") != currentMonthForTest() {
				t.Fatalf("date shortcut=%s", current)
			}
			if !strings.Contains(moved, "Showing: ") || !strings.Contains(moved, "KeepScope") {
				t.Fatal("zero-activity current period loses selected context")
			}
			if strings.Contains(cut(moved, "<tbody>", "</tbody>"), "OtherScope") {
				t.Fatal("current month expanded to unrelated project")
			}
			if reportAnchor(t, moved, "← Back to source report") != origin {
				t.Fatal("source scope was lost")
			}
		})
	}
	if _, err := s.st.UpsertHead(s.ctx, head, project, "Retired scope", "5", false, 0); err != nil {
		t.Fatal(err)
	}
	retiredPath := fmt.Sprintf("/reports/heads?from=%s&to=%s&project_id=%d&head_id=%d", currentMonthForTest(), currentMonthForTest(), project, head)
	retired := responseBody(t, s.request(http.MethodGet, retiredPath, nil, ""))
	if !strings.Contains(retired, "Showing:") || !strings.Contains(retired, "Retired scope") {
		t.Fatal("retired head loses scope label in an empty period")
	}

}

func TestReportMultiMonthTotalsCSVLabelAndPaymentScopes(t *testing.T) {
	s := newAppTestServer(t)
	actor, head := s.seedHead("VisibleReport")
	_, other := s.seedHead("ExcludedReport")
	for _, month := range []string{"2026-03", "2026-04"} {
		for id, amount := range map[int64]int64{head: 100000, other: 900000} {
			if err := s.st.SetBudget(s.ctx, actor, id, month, amount); err != nil {
				t.Fatal(err)
			}
		}
	}
	owned := s.seedProbeUser("own-report@example.test", "Own report", "ReportScopedPass1", "own-report", []store.Grant{{Resource: "report", Action: "view"}, {Resource: "payment", Action: "view"}}, []store.ScopeGrant{{Resource: "payment", Scope: "own"}})
	for _, fixture := range []struct {
		actor       store.User
		head        int64
		date, payee string
		amount      int64
	}{{owned, head, "2026-03-10", "Own March", 20000}, {owned, head, "2026-04-10", "Own April", 30000}, {actor, head, "2026-04-11", "Other viewer payment", 40000}, {actor, other, "2026-04-12", "Other head payment", 80000}} {
		if _, err := s.st.CreatePayment(s.ctx, fixture.actor, store.PaymentInput{HeadID: fixture.head, PaidOn: fixture.date, Amount: fixture.amount, VendorPayee: fixture.payee, PaymentMode: "cash"}); err != nil {
			t.Fatal(err)
		}
	}
	var project int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, head).Scan(&project); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/reports/heads?from=2026-03&to=2026-04&project_id=%d&head_id=%d", project, head)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, path, nil, ""))
	for _, want := range []string{"Own March", "Own April", "Other viewer payment", "₹1,000.00", "Export full heads report CSV"} {
		if !strings.Contains(body, want) {
			t.Errorf("admin report missing %s", want)
		}
	}
	for label, value := range map[string]string{"Budget": "₹2,000.00", "Actual": "₹900.00", "Remaining": "₹1,100.00", "Used": "45%"} {
		metric := cut(body, `class="metric-label">`+label+`</span>`, `</div>`)
		if !strings.Contains(metric, value) {
			t.Errorf("rendered %s metric = %s, want %s", label, metric, value)
		}
	}
	if strings.Contains(body, "Other head payment") {
		t.Fatal("head context exposed unrelated payment")
	}
	rows, err := s.st.Report(s.ctx, "2026-03", "2026-04", "heads")
	if err != nil {
		t.Fatal(err)
	}
	var scoped []store.ReportRow
	for _, r := range rows {
		if r.HeadID == head {
			scoped = append(scoped, r)
		}
	}
	summary := summarizeReports(scoped)
	if summary.Budget != 200000 || summary.Actual != 90000 || summary.Variance != 110000 {
		t.Fatalf("multi-month arithmetic=%+v", summary)
	}
	// Following the expressly labelled full export must actually return full scope.
	export := reportAnchor(t, body, "Export full heads report CSV")
	csvBody := responseBody(t, s.request(http.MethodGet, export, nil, ""))
	records, err := csv.NewReader(strings.NewReader(csvBody)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 5 || !strings.Contains(csvBody, "ExcludedReport") {
		t.Fatalf("full CSV scope unexpected rows=%d body=%s", len(records), csvBody)
	}
	s.login(owned.Email, "ReportScopedPass1")
	limited := responseBody(t, s.request(http.MethodGet, path, nil, ""))
	for _, want := range []string{"Own March", "Own April", "Restricted payment access may show fewer records"} {
		if !strings.Contains(limited, want) {
			t.Errorf("own report missing %s", want)
		}
	}
	for _, secret := range []string{"Other viewer payment", "Other head payment"} {
		if strings.Contains(limited, secret) {
			t.Errorf("own scope leaked %s", secret)
		}
	}
	if strings.Contains(limited, "Export full heads report CSV") {
		t.Fatal("export action offered without permission")
	}
	s.seedProbeUser("no-pay-scope@example.test", "No scope", "NoScopePass123", "no-pay-scope", []store.Grant{{Resource: "report", Action: "view"}, {Resource: "payment", Action: "view"}}, nil)
	s.login("no-pay-scope@example.test", "NoScopePass123")
	none := responseBody(t, s.request(http.MethodGet, path, nil, ""))
	if strings.Contains(none, "Own March") || strings.Contains(none, "Other viewer payment") {
		t.Fatal("missing payment scope did not fail closed")
	}
	empty := responseBody(t, s.request(http.MethodGet, "/reports/heads?from=2099-01&to=2099-01", nil, ""))
	if strings.Contains(empty, `href="/budgets">Set a budget`) {
		t.Fatal("empty report offers an inaccessible budget action")
	}

}

func TestGridPaymentActivityDoesNotDeclareOpenBalancesSettled(t *testing.T) {
	s := newAppTestServer(t)
	actor, head := s.seedHead("OutstandingBalances")
	month := currentMonthForTest()
	if err := s.st.SetBudget(s.ctx, actor, head, month, 1000000); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	installment, _ := s.settleOneRequest(501, head, 1200000, "7000.00", "installment", month+"-01")
	kept, _ := s.settleOneRequest(502, head, 60000, "100.00", "partial", month+"-01")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/accept-partial", kept), url.Values{"decision": {"continue"}, "note": {"Pay the remaining 500 on next transfer"}}), http.StatusSeeOther)
	s.settleOneRequest(503, head, 200000, "2000.00", "settled", month+"-01")
	for id, wantRemaining := range map[int64]int64{installment: 500000, kept: 50000} {
		req, err := s.st.Request(s.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if req.Status != "approved" || approvedOf(req)-req.PaidAmount != wantRemaining {
			t.Fatalf("request %d status=%s remaining=%d", id, req.Status, approvedOf(req)-req.PaidAmount)
		}
	}
	body := responseBody(t, s.request(http.MethodGet, "/grid?month="+month, nil, ""))
	row := cut(body, `<tr class="head`, `</tr>`)
	t.Logf("Grid head row with ₹5,000 + ₹500 still payable: %s", row)
	for _, want := range []string{"₹10,000.00", "₹9,100.00", "₹900.00", "Under budget", "Payment recorded"} {
		if !strings.Contains(row, want) {
			t.Errorf("grid row missing %q", want)
		}
	}
	if strings.Contains(row, ">Settled<") || strings.Contains(row, `duepill settled`) {
		t.Fatal("payment activity falsely declares open request balances settled")
	}
	for _, status := range []string{"under", "on-track", "over", "unbudgeted"} {
		if dueText("5", month, status) != "Payment recorded" {
			t.Errorf("%s claims settlement from budget utilization", status)
		}
	}
}
