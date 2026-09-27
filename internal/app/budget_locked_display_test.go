package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestLockedPlannerConflictDisplaysStoredPlan(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	p := plannerFixture("2026-04")
	requireStatus(t, s.postForm("/budgets/plan", plannerForm(t, p, "create")), http.StatusSeeOther)
	stored, err := s.st.BudgetPlan(s.ctx, p.Month)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.st.LockMonth(s.ctx, admin, p.Month, "Final review"); err != nil {
		t.Fatal(err)
	}
	stored.Projects[0].Heads[0].Lines[0].Amount = 55555
	stored.Projects[0].Heads[0].Lines[0].Description = "UNSAVED ATTEMPT"
	resp := s.postForm("/budgets/plan", plannerForm(t, stored, "edit"))
	requireStatus(t, resp, http.StatusConflict)
	body := responseBody(t, resp)
	const marker = `<script type="application/json" id="budget-planner-data">`
	_, raw, ok := strings.Cut(body, marker)
	if !ok {
		t.Fatal("missing planner configuration")
	}
	raw, _, _ = strings.Cut(raw, "</script>")
	var cfg plannerConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Draft.Locked || cfg.Draft.Projects[0].Heads[0].Lines[0].Amount != 1000050 || cfg.Draft.Projects[0].Heads[0].Lines[0].Description != "Rent" {
		t.Fatalf("locked view showed rejected draft: %+v", cfg.Draft)
	}
	if !strings.Contains(cfg.Error, "Your changes were not saved") {
		t.Fatal("missing recovery explanation")
	}
	final, err := s.st.BudgetPlan(s.ctx, p.Month)
	if err != nil || final.Projects[0].Heads[0].Lines[0].Amount != 1000050 {
		t.Fatalf("stored budget changed: %+v %v", final, err)
	}
}
