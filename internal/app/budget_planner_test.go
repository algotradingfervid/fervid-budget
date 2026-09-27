package app

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

func plannerFixture(month string) store.BudgetPlan {
	return store.BudgetPlan{Month: month, Projects: []store.PlanProject{{Name: "Planner Operations", Heads: []store.PlanHead{{Name: "Office Rent", Lines: []store.PlanLine{{Description: "Rent", Amount: 1000050}, {Description: "Maintenance", Amount: 250025}}}}}}}
}
func plannerForm(t *testing.T, p store.BudgetPlan, mode string) url.Values {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return url.Values{"mode": {mode}, "draft": {string(raw)}}
}
func TestBudgetPlannerHTTPCreateReadEditAndLegacyProtection(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	p := plannerFixture("2026-07")
	resp := s.postForm("/budgets/plan", plannerForm(t, p, "create"))
	requireStatus(t, resp, http.StatusSeeOther)
	if resp.Header.Get("Location") != "/budgets/plan?month=2026-07&saved=1" {
		t.Fatal(resp.Header.Get("Location"))
	}
	resp.Body.Close()
	data := s.request(http.MethodGet, "/budgets/plan-data?month=2026-07", nil, "")
	requireStatus(t, data, http.StatusOK)
	var saved store.BudgetPlan
	if err := json.NewDecoder(data.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	data.Body.Close()
	if !saved.Exists || saved.Revision == "" || len(saved.Projects) != 1 || len(saved.Projects[0].Heads[0].Lines) != 2 {
		t.Fatalf("saved=%+v", saved)
	}
	hid := saved.Projects[0].Heads[0].ID
	b, err := s.st.Budget(s.ctx, hid, "2026-07")
	if err != nil || b.Amount != 1250075 {
		t.Fatalf("aggregate=%+v err=%v", b, err)
	}
	saved.Projects[0].Heads[0].Lines[1].Amount = 300025
	requireStatus(t, s.postForm("/budgets/plan", plannerForm(t, saved, "edit")), http.StatusSeeOther)
	requireStatus(t, s.postForm("/budgets/plan", plannerForm(t, saved, "edit")), http.StatusBadRequest)
	body := responseBody(t, s.request(http.MethodGet, "/budgets?month=2026-07", nil, ""))
	if !strings.Contains(body, "Edit lines") || !strings.Contains(body, "Edit budget lines") {
		t.Fatal("legacy aggregate view lacks detailed editor link")
	}
	grid, err := s.st.Grid(s.ctx, "2026-07", "", "")
	if err != nil || grid.Total.Budget != 1300075 {
		t.Fatalf("grid=%+v err=%v", grid.Total, err)
	}
	duplicate := s.postForm("/budgets/plan", plannerForm(t, p, "create"))
	requireStatus(t, duplicate, http.StatusBadRequest)
	if !strings.Contains(responseBody(t, duplicate), "already exists") {
		t.Fatal("duplicate plan is not explained")
	}
}
func TestBudgetPlannerFormValidationRetainsDraftAndEscapesMarkup(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	p := plannerFixture("2026-08")
	p.Projects[0].Name = `</script><img src=x onerror=alert(1)>`
	p.Projects[0].Heads[0].Lines[1].Amount = -1
	resp := s.postForm("/budgets/plan", plannerForm(t, p, "create"))
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	if strings.Contains(body, p.Projects[0].Name) || !strings.Contains(body, `\u003c/script\u003e`) {
		t.Fatal("draft JSON not safely escaped")
	}
	if !strings.Contains(body, `"amount":-1`) {
		t.Fatal("invalid amount was lost")
	}
	var n int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM projects WHERE name=?`, p.Projects[0].Name).Scan(&n); err != nil || n != 0 {
		t.Fatalf("invalid draft left a project: %d %v", n, err)
	}
}
func TestBudgetPlannerAuthorizationSourceAndCSRF(t *testing.T) {
	s := newAppTestServer(t)
	s.seedProbeUser("budget-editor@example.test", "Budget editor", "BudgetEditor123", "budget-editor", []store.Grant{{Resource: "budget", Action: "view"}, {Resource: "budget", Action: "edit"}}, nil)
	s.login("budget-editor@example.test", "BudgetEditor123")
	requireStatus(t, s.request(http.MethodGet, "/budgets/new", nil, ""), http.StatusForbidden)
	requireStatus(t, s.postForm("/budgets/plan", plannerForm(t, plannerFixture("2026-09"), "create")), http.StatusForbidden)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.request(http.MethodGet, "/budgets/plan-data?month=2031-01", nil, ""), http.StatusNotFound)
	requireStatus(t, s.request(http.MethodGet, "/budgets/plan-data?month=bad", nil, ""), http.StatusBadRequest)
	form := plannerForm(t, plannerFixture("2026-09"), "create")
	requireStatus(t, s.request(http.MethodPost, "/budgets/plan", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"), http.StatusForbidden)
	requireStatus(t, s.postForm("/budgets/plan", url.Values{"mode": {"create"}, "draft": {`{"month":"2026-09","projects":[],"unknown":true}`}}), http.StatusBadRequest)
}
func TestNoBudgetUtilisationDoesNotReportZeroPercentSpend(t *testing.T) {
	if usedText(0, 50000) != "No budget" || usedText(0, 0) != "0%" || usedText(10000, 5000) != "50%" {
		t.Fatal("zero denominator or ordinary utilisation displayed incorrectly")
	}
}

func TestLegacyCopyRequiresBudgetPermissions(t *testing.T) {
	s := newAppTestServer(t)
	s.seedProbeUser("month-only@example.test", "Month creator", "MonthCreator123", "month-only", []store.Grant{{Resource: "month", Action: "create"}}, nil)
	s.login("month-only@example.test", "MonthCreator123")
	requireStatus(t, s.postForm("/months", url.Values{"target_month": {"2026-10"}, "source_mode": {"copy"}, "source_month": {"2026-06"}}), http.StatusForbidden)
	var n int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM budget_months WHERE month='2026-10'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("unauthorized copy created month: %d %v", n, err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm("/months", url.Values{"target_month": {"2026-10"}, "source_mode": {"copy"}}), http.StatusBadRequest)
}

func TestBudgetPlannerRejectedDraftUsesCanonicalMetadata(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm("/budgets/plan", plannerForm(t, plannerFixture("2026-07"), "create")), http.StatusSeeOther)
	p, err := s.st.BudgetPlan(s.ctx, "2026-07")
	if err != nil {
		t.Fatal(err)
	}
	oldRevision := p.Revision
	hid := p.Projects[0].Heads[0].ID
	if _, err := s.st.DB().Exec(`UPDATE heads SET active=0 WHERE id=?`, hid); err != nil {
		t.Fatal(err)
	}
	p.Projects[0].Name = "Spoofed project"
	p.Projects[0].Heads[0].Name = "Spoofed head"
	p.SourceMonth = "2000-01"
	p.Projects[0].Heads[0].Lines[0].Amount = 42
	resp := s.postForm("/budgets/plan", plannerForm(t, p, "edit"))
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	if strings.Contains(body, "Spoofed") || strings.Contains(body, `"sourceMonth":"2000-01"`) || !strings.Contains(body, `"readOnly":true`) || !strings.Contains(body, `"revision":"`+oldRevision+`"`) || !strings.Contains(body, `"amount":42`) || !strings.Contains(body, `"savedHeadIds":[`) {
		t.Fatal("rejected draft lost input/revision or retained spoofed persisted metadata")
	}
}
