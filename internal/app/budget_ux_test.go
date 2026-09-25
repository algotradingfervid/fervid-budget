package app

import (
	"fervidbudget/internal/store"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestUXBudgetPartialSaveLinksRejectedCellsAndKeepsSavedValue(t *testing.T) {
	s := newAppTestServer(t)
	actor, a := s.seedHead("UXValid")
	_, b := s.seedHead("UXBad")
	_, c := s.seedHead("UXNegative")
	if err := s.st.SetBudget(s.ctx, actor, b, "2026-04", 50000); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.postForm("/budgets", url.Values{"month": {"2026-04"}, fmt.Sprintf("budget_%d", a): {"100.50"}, fmt.Sprintf("budget_%d", b): {"abc"}, fmt.Sprintf("budget_%d", c): {"-10"}})
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	if strings.Count(body, `role="alert"`) != 1 {
		t.Fatal("a partial save must announce its result once")
	}
	if !strings.Contains(body, `role="region" aria-labelledby="budget-errors-heading"`) {
		t.Fatal("the linked error summary must have a region label")
	}
	for _, want := range []string{"1 budget amount saved. 2 invalid budget amounts were not saved", fmt.Sprintf(`href="#budget_%d"`, b), fmt.Sprintf(`href="#budget_%d"`, c), fmt.Sprintf(`aria-describedby="budget_error_%d"`, b), fmt.Sprintf(`id="budget_error_%d"`, b), "Not saved:", `class="budget-saved">Saved`, `value="abc"`, `value="-10"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	saved, err := s.st.Budget(s.ctx, a, "2026-04")
	if err != nil || saved.Amount != 10050 {
		t.Fatalf("saved budget=%+v err=%v", saved, err)
	}
	unchanged, err := s.st.Budget(s.ctx, b, "2026-04")
	if err != nil || unchanged.Amount != 50000 {
		t.Fatalf("invalid budget changed=%+v err=%v", unchanged, err)
	}
}

func TestUXGridSeparatesFilteredSubtotalAndFullMonthAndRecoversNoMatch(t *testing.T) {
	s := newAppTestServer(t)
	actor, a := s.seedHead("OfficeRentUX")
	_, b := s.seedHead("UtilitiesUX")
	for id, amount := range map[int64]int64{a: 1000000, b: 250000} {
		if err := s.st.SetBudget(s.ctx, actor, id, "2026-04", amount); err != nil {
			t.Fatal(err)
		}
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/grid?month=2026-04&q=OfficeRentUX", nil, ""))
	for _, want := range []string{"Filtered subtotal", "Showing 1 of 2 heads", "Full month company total: budget ₹12,500.00", "Full month: 2 budgeted heads are unpaid", `href="/grid?month=2026-04"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	noMatch := responseBody(t, s.request(http.MethodGet, "/grid?month=2026-04&q=does-not-match", nil, ""))
	if !strings.Contains(noMatch, "No heads match these filters") || strings.Contains(noMatch, "Add projects and heads to begin") {
		t.Fatal("no match gives setup advice")
	}
	cleared := responseBody(t, s.request(http.MethodGet, "/grid?month=2026-04", nil, ""))
	if !strings.Contains(cleared, "OfficeRentUX") || !strings.Contains(cleared, "UtilitiesUX") {
		t.Fatal("clear did not restore rows")
	}
}

func TestUXZeroActivityDoesNotInventUnpaidObligation(t *testing.T) {
	s := newAppTestServer(t)
	s.seedHead("NeutralUX")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/grid?month=2020-01", nil, ""))
	if !strings.Contains(body, "No activity") || !strings.Contains(body, "Usual day 5") || !strings.Contains(body, "Full month: 0 budgeted heads are unpaid") {
		t.Fatal("zero activity not shown neutrally")
	}
	if strings.Contains(body, `duepill overdue`) {
		t.Fatal("zero activity is presented as overdue")
	}
	if dueText("31", "2026-02", "not-paid") != "Overdue" {
		t.Fatal("last-day planned timing must be supported for short months")
	}
}

func TestUXReportDrilldownCarriesIDsDatesAndSourceScope(t *testing.T) {
	s := newAppTestServer(t)
	actor, head := s.seedHead("DrillUX")
	_, other := s.seedHead("OtherUX")
	for id, amount := range map[int64]int64{head: 200000, other: 800000} {
		if err := s.st.SetBudget(s.ctx, actor, id, "2026-04", amount); err != nil {
			t.Fatal(err)
		}
	}
	pay, err := s.st.CreatePayment(s.ctx, actor, store.PaymentInput{HeadID: head, PaidOn: "2026-04-01", Amount: 100000, VendorPayee: "Drill payee", PaymentMode: "cash"})
	if err != nil {
		t.Fatal(err)
	}
	var project int64
	if err = s.st.DB().QueryRow("SELECT project_id FROM heads WHERE id=?", head).Scan(&project); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	origin := "/reports/heads?from=2026-03&to=2026-06"
	link := reportDetailURL(store.ReportRow{Period: "2026-04", HeadID: head, ProjectID: project}, origin)
	body := responseBody(t, s.request(http.MethodGet, link, nil, ""))
	for _, want := range []string{"Back to source report", html.EscapeString(origin), "Payments behind these actuals", "Drill payee", fmt.Sprintf("Open payment %d", pay), fmt.Sprintf(`value="%d"`, head), "2026-04"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(cut(body, "<tbody>", "</tbody>"), "OtherUX") {
		t.Fatal("project/head drilldown leaked unrelated rows")
	}
	if safeReportBack("https://evil.example/reports/heads") != "" || safeReportBack("//evil.example/reports/heads") != "" {
		t.Fatal("unsafe back destination accepted")
	}
	s.seedProbeUser("reportonly@example.test", "Report only", "ReportOnlyPass1", "report-only", []store.Grant{{Resource: "report", Action: "view"}}, nil)
	s.login("reportonly@example.test", "ReportOnlyPass1")
	limited := responseBody(t, s.request(http.MethodGet, link, nil, ""))
	if strings.Contains(limited, "Drill payee") {
		t.Fatal("report privilege exposed payment data")
	}
}
