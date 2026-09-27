package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func plannerFixture(t *testing.T) (*Store, User, BudgetPlan) {
	t.Helper()
	s := newTestStore(t)
	actor, hid := seedActorAndHead(t, s, context.Background())
	var pid int64
	if err := s.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, hid).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	return s, actor, BudgetPlan{Month: "2026-09", Projects: []PlanProject{{ID: pid, Heads: []PlanHead{{ID: hid, Lines: []PlanLine{{Description: "Base rent", Amount: 12345}, {Description: "Maintenance", Amount: 6789}}}}}}}
}
func plannerRead(t *testing.T, s *Store, month string) BudgetPlan {
	t.Helper()
	p, err := s.BudgetPlan(context.Background(), month)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func plannerSave(t *testing.T, s *Store, u User, p BudgetPlan, edit bool) {
	t.Helper()
	if err := s.SaveBudgetPlan(context.Background(), u, p, edit); err != nil {
		t.Fatal(err)
	}
}
func plannerCounts(t *testing.T, s *Store) []int {
	t.Helper()
	var result []int
	for _, table := range []string{"projects", "heads", "budget_months", "budgets", "budget_lines", "audit_log"} {
		var n int
		if err := s.DB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		result = append(result, n)
	}
	return result
}

func TestBudgetPlannerBlankAndLineRoundTrip(t *testing.T) {
	s, u, p := plannerFixture(t)
	ctx := context.Background()
	blank := plannerRead(t, s, p.Month)
	if blank.Exists || len(blank.Projects) != 0 || blank.Revision == "" {
		t.Fatalf("blank plan unexpectedly populated: %+v", blank)
	}
	p.Projects = append(p.Projects, PlanProject{Name: "  Workshop  ", Heads: []PlanHead{{Name: "  Supplies  ", Lines: []PlanLine{{Description: "  Fasteners  ", Amount: 10001}, {Description: "Zero allocation", Amount: 0}}}}})
	plannerSave(t, s, u, p, false)
	got := plannerRead(t, s, p.Month)
	if !got.Exists || len(got.Projects) != 2 {
		t.Fatalf("saved plan: %+v", got)
	}
	var sum int64
	for _, project := range got.Projects {
		for _, head := range project.Heads {
			var want int64
			for _, line := range head.Lines {
				want += line.Amount
				if line.Description != strings.TrimSpace(line.Description) {
					t.Fatal("untrimmed line")
				}
			}
			b, err := s.Budget(ctx, head.ID, p.Month)
			if err != nil || b.Amount != want {
				t.Fatalf("head aggregate=%+v %v; lines=%d", b, err, want)
			}
			sum += want
		}
	}
	if sum != 29135 {
		t.Fatalf("total %d", sum)
	}
	got.Projects[0].Heads[0].Lines[0].Amount++
	plannerSave(t, s, u, got, true)
	updated := plannerRead(t, s, p.Month)
	if updated.Revision == got.Revision {
		t.Fatal("changed plan did not change revision")
	}
	var mismatch int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM budgets b WHERE amount != (SELECT SUM(amount) FROM budget_lines WHERE budget_id=b.id)`).Scan(&mismatch); err != nil || mismatch != 0 {
		t.Fatalf("aggregate/detail mismatch=%d: %v", mismatch, err)
	}
}

func TestBudgetPlannerFailedValidationRollsBackAllWrites(t *testing.T) {
	tests := []struct {
		name   string
		change func(*BudgetPlan)
	}{
		{"negative later line", func(p *BudgetPlan) { p.Projects[0].Heads[0].Lines[1].Amount = -1 }},
		{"overflow", func(p *BudgetPlan) { p.Projects[0].Heads[0].Lines[1].Amount = math.MaxInt64 }},
		{"plan ceiling", func(p *BudgetPlan) {
			p.Projects[0].Heads[0].Lines = []PlanLine{{"One", 500000000000000}, {"Two", 500000000000000}}
		}},
		{"empty description", func(p *BudgetPlan) { p.Projects[0].Heads[0].Lines[1].Description = "  " }},
		{"long description", func(p *BudgetPlan) { p.Projects[0].Heads[0].Lines[1].Description = strings.Repeat("界", 241) }},
		{"duplicate head", func(p *BudgetPlan) { p.Projects[0].Heads = append(p.Projects[0].Heads, p.Projects[0].Heads[0]) }},
		{"duplicate project", func(p *BudgetPlan) { p.Projects = append(p.Projects, p.Projects[0]) }},
		{"unknown head", func(p *BudgetPlan) { p.Projects[0].Heads[0].ID = 999999 }},
		{"unknown project", func(p *BudgetPlan) { p.Projects[0].ID = 999999 }},
		{"empty lines", func(p *BudgetPlan) { p.Projects[0].Heads[0].Lines = nil }},
		{"too many lines", func(p *BudgetPlan) { p.Projects[0].Heads[0].Lines = make([]PlanLine, 501) }},
		{"duplicate new master", func(p *BudgetPlan) {
			p.Projects = append(p.Projects, PlanProject{Name: "operations", Heads: []PlanHead{{Name: "New", Lines: []PlanLine{{"Line", 1}}}}})
		}},
		{"new masters followed by invalid line", func(p *BudgetPlan) {
			p.Projects = []PlanProject{{Name: "New project", Heads: []PlanHead{{Name: "New head", Lines: []PlanLine{{"Good", 100}, {"Bad", -1}}}}}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, u, p := plannerFixture(t)
			before := plannerCounts(t, s)
			tt.change(&p)
			err := s.SaveBudgetPlan(context.Background(), u, p, false)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("got %v; want validation", err)
			}
			if after := plannerCounts(t, s); !reflect.DeepEqual(before, after) {
				t.Fatalf("failed save changed database: %v -> %v", before, after)
			}
		})
	}
}

func TestBudgetPlannerInvalidEditPreservesExistingLinesAndAudit(t *testing.T) {
	s, u, p := plannerFixture(t)
	plannerSave(t, s, u, p, false)
	before := plannerRead(t, s, p.Month)
	counts := plannerCounts(t, s)
	p = plannerRead(t, s, p.Month)
	p.Projects[0].Heads[0].Lines[0].Amount = 55555
	p.Projects = append(p.Projects, PlanProject{Name: "Will roll back", Heads: []PlanHead{{Name: "Bad", Lines: []PlanLine{{"Negative", -1}}}}})
	if err := s.SaveBudgetPlan(context.Background(), u, p, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("%v", err)
	}
	if after := plannerRead(t, s, p.Month); !reflect.DeepEqual(before, after) {
		t.Fatalf("invalid edit changed saved plan: %+v", after)
	}
	if !reflect.DeepEqual(counts, plannerCounts(t, s)) {
		t.Fatal("invalid edit persisted masters or audit")
	}
}

func TestBudgetPlannerConcurrentEditorsAndDuplicateCreate(t *testing.T) {
	s, u, p := plannerFixture(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; results <- s.SaveBudgetPlan(context.Background(), u, p, false) }()
	}
	close(start)
	wg.Wait()
	close(results)
	success, rejected := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrValidation) {
			rejected++
		} else {
			t.Fatalf("create error: %v", err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("creates succeeded=%d rejected=%d", success, rejected)
	}
	first := plannerRead(t, s, p.Month)
	second := plannerRead(t, s, p.Month)
	first.Projects[0].Heads[0].Lines[0].Amount = 33333
	second.Projects[0].Heads[0].Lines[0].Amount = 44444
	results = make(chan error, 2)
	start = make(chan struct{})
	for _, plan := range []BudgetPlan{first, second} {
		wg.Add(1)
		go func(plan BudgetPlan) {
			defer wg.Done()
			<-start
			results <- s.SaveBudgetPlan(context.Background(), u, plan, true)
		}(plan)
	}
	close(start)
	wg.Wait()
	close(results)
	success, rejected = 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrValidation) {
			rejected++
		} else {
			t.Fatalf("edit error: %v", err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("edits succeeded=%d rejected=%d", success, rejected)
	}
	final := plannerRead(t, s, p.Month)
	if final.Projects[0].Heads[0].Lines[0].Amount != 33333 && final.Projects[0].Heads[0].Lines[0].Amount != 44444 {
		t.Fatal("unexpected saved winner")
	}
	p = plannerRead(t, s, p.Month)
	p.Revision = ""
	if err := s.SaveBudgetPlan(context.Background(), u, p, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing revision accepted: %v", err)
	}
}

func TestBudgetPlannerLockedAndRetired(t *testing.T) {
	s, u, p := plannerFixture(t)
	ctx := context.Background()
	plannerSave(t, s, u, p, false)
	existing := plannerRead(t, s, p.Month)
	hid := existing.Projects[0].Heads[0].ID
	pid := existing.Projects[0].ID
	if _, err := s.UpsertHead(ctx, hid, pid, "Rent", "5", false, 1); err != nil {
		t.Fatal(err)
	}
	retired := plannerRead(t, s, p.Month)
	if !retired.Projects[0].Heads[0].ReadOnly {
		t.Fatal("retired head not marked read-only")
	}
	if err := s.SaveBudgetPlan(ctx, u, existing, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("stale pre-retirement revision accepted: %v", err)
	}
	plannerSave(t, s, u, retired, true)
	retired = plannerRead(t, s, p.Month)
	retired.Projects[0].Heads[0].Lines[0].Amount++
	if err := s.SaveBudgetPlan(ctx, u, retired, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("retired amount editable: %v", err)
	}
	retired = plannerRead(t, s, p.Month)
	retired.Month = "2026-10"
	retired.SourceMonth = p.Month
	if err := s.SaveBudgetPlan(ctx, u, retired, false); !errors.Is(err, ErrValidation) {
		t.Fatalf("retired head copied into new month: %v", err)
	}
	if err := s.LockMonth(ctx, u, p.Month, "Final accounts"); err != nil {
		t.Fatal(err)
	}
	locked := plannerRead(t, s, p.Month)
	if !locked.Locked {
		t.Fatal("locked flag absent")
	}
	if err := s.SaveBudgetPlan(ctx, u, locked, true); !errors.Is(err, ErrLockedMonth) {
		t.Fatalf("locked save: %v", err)
	}
}

func TestBudgetPlannerExistingHeadsCannotBeDroppedOrReparented(t *testing.T) {
	s, u, p := plannerFixture(t)
	plannerSave(t, s, u, p, false)
	ctx := context.Background()
	current := plannerRead(t, s, p.Month)
	current.Projects = []PlanProject{{Name: "Other", Heads: []PlanHead{{Name: "Different", Lines: []PlanLine{{"Line", 1}}}}}}
	if err := s.SaveBudgetPlan(ctx, u, current, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("dropped saved head: %v", err)
	}
	current = plannerRead(t, s, p.Month)
	current.Projects[0].ID = 0
	current.Projects[0].Name = "Other"
	if err := s.SaveBudgetPlan(ctx, u, current, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("reparented existing head: %v", err)
	}
}

func TestBudgetPlannerPermissionsRecheckedFromDatabase(t *testing.T) {
	s, u, p := plannerFixture(t)
	ctx := context.Background()
	id, err := s.CreateUser(ctx, "requester@example.com", "Requester", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	requester, err := s.UserByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveBudgetPlan(ctx, requester, p, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("requester saved: %v", err)
	}
	if _, err := s.DB().Exec(`UPDATE users SET active=0 WHERE id=?`, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveBudgetPlan(ctx, u, p, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("inactive admin saved: %v", err)
	}
	if _, err := s.DB().Exec(`UPDATE users SET active=1 WHERE id=?`, u.ID); err != nil {
		t.Fatal(err)
	}
	// A custom budget editor can use existing masters, but cannot create masters.
	roleID, err := s.CreateRole(ctx, u, "Budget editor", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRolePermissions(ctx, u, roleID, []Grant{{"budget", "view"}, {"budget", "edit"}, {"month", "create"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`DELETE FROM user_roles WHERE user_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, id, roleID); err != nil {
		t.Fatal(err)
	}
	plannerSave(t, s, requester, p, false)
	p = plannerRead(t, s, p.Month)
	p.Projects[0].Heads = append(p.Projects[0].Heads, PlanHead{Name: "Not permitted", Lines: []PlanLine{{"Line", 1}}})
	if err := s.SaveBudgetPlan(ctx, requester, p, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("master creation without permission: %v", err)
	}
}

func TestBudgetPlannerLegacyCopyAndDetailedCopy(t *testing.T) {
	s, u, p := plannerFixture(t)
	ctx := context.Background()
	hid := p.Projects[0].Heads[0].ID
	if err := s.SetBudget(ctx, u, hid, "2026-08", 76543); err != nil {
		t.Fatal(err)
	}
	legacy := plannerRead(t, s, "2026-08")
	if lines := legacy.Projects[0].Heads[0].Lines; len(lines) != 1 || lines[0].Description != "Monthly allocation" || lines[0].Amount != 76543 {
		t.Fatalf("legacy fallback: %+v", lines)
	}
	legacy.Month = "2026-09"
	legacy.SourceMonth = "2026-08"
	legacy.Projects[0].Heads[0].Lines = append(legacy.Projects[0].Heads[0].Lines, PlanLine{"Extra", 111})
	plannerSave(t, s, u, legacy, false)
	if err := s.CreateMonthPlan(ctx, u, "2026-10", "2026-09"); err != nil {
		t.Fatal(err)
	}
	copy := plannerRead(t, s, "2026-10")
	if copy.SourceMonth != "2026-09" || !reflect.DeepEqual(copy.Projects, plannerRead(t, s, "2026-09").Projects) {
		t.Fatalf("copy lost lines: %+v", copy)
	}
	copy.Projects[0].Heads[0].Lines[0].Amount = 100
	plannerSave(t, s, u, copy, true)
	if source := plannerRead(t, s, "2026-09"); source.Projects[0].Heads[0].Lines[0].Amount != 76543 {
		t.Fatal("editing copy altered source")
	}
	if err := s.SetBudget(ctx, u, hid, "2026-09", 1); !errors.Is(err, ErrValidation) {
		t.Fatalf("legacy aggregate edit desynchronized details: %v", err)
	}
}

func TestBudgetPlannerThreeMonthPaymentsGridAndReports(t *testing.T) {
	s, u, p := plannerFixture(t)
	ctx := context.Background()
	hid := p.Projects[0].Heads[0].ID
	var expectedBudget, expectedActual int64
	for i, month := range []string{"2026-01", "2026-02", "2026-03"} {
		p.Month = month
		p.Projects[0].Heads[0].Lines = []PlanLine{{"Rent", int64(10000 + i*1000)}, {"Maintenance", 1234}}
		plannerSave(t, s, u, p, false)
		payment := int64(4000 + i*500)
		if _, err := s.CreatePayment(ctx, u, PaymentInput{HeadID: hid, PaidOn: month + "-05", Amount: payment, VendorPayee: "Budget vendor"}); err != nil {
			t.Fatal(err)
		}
		seedRecoverablePayment(t, s, ctx, u, hid, 0, fmt.Sprintf("PR-2026-%06d", i+1), month+"-06", 90000)
		grid, err := s.Grid(ctx, month, "", "")
		if err != nil {
			t.Fatal(err)
		}
		budget := int64(11234 + i*1000)
		if grid.Total.Budget != budget || grid.Total.Actual != payment || grid.Total.Variance != budget-payment {
			t.Fatalf("%s grid=%+v; want budget=%d actual=%d", month, grid.Total, budget, payment)
		}
		expectedBudget += budget
		expectedActual += payment
	}
	for _, group := range []string{"heads", "projects"} {
		report, err := s.Report(ctx, "2026-01", "2026-03", group)
		if err != nil {
			t.Fatal(err)
		}
		var budget, actual int64
		for _, row := range report {
			budget += row.Budget
			actual += row.Actual
		}
		if budget != expectedBudget || actual != expectedActual {
			t.Fatalf("%s report budget=%d actual=%d; want %d/%d", group, budget, actual, expectedBudget, expectedActual)
		}
	}
}

func TestBudgetPlannerLegacyCopyDoesNotOverwriteEqualAmountTargetDetail(t *testing.T) {
	s, u, p := plannerFixture(t)
	ctx := context.Background()
	plannerSave(t, s, u, p, false)
	head := p.Projects[0].Heads[0]
	if err := s.SetBudget(ctx, u, head.ID, "2026-10", 19134); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMonthPlan(ctx, u, "2026-10", p.Month); err != nil {
		t.Fatal(err)
	}
	target := plannerRead(t, s, "2026-10")
	if lines := target.Projects[0].Heads[0].Lines; len(lines) != 1 || lines[0].Description != "Monthly allocation" || lines[0].Amount != 19134 {
		t.Fatalf("existing target had source detail injected: %+v", lines)
	}
	target.Projects[0].Heads[0].Lines = []PlanLine{{"Target-only detail", 19134}}
	plannerSave(t, s, u, target, true)
	if err := s.CreateMonthPlan(ctx, u, "2026-10", p.Month); err != nil {
		t.Fatal(err)
	}
	if got := plannerRead(t, s, "2026-10").Projects[0].Heads[0].Lines; len(got) != 1 || got[0].Description != "Target-only detail" {
		t.Fatalf("existing detail changed: %+v", got)
	}
}

func TestBudgetPlannerLegacyCopyAuditFailureRollsBackMonthAndLines(t *testing.T) {
	s, u, p := plannerFixture(t)
	ctx := context.Background()
	plannerSave(t, s, u, p, false)
	before := plannerCounts(t, s)
	if _, err := s.DB().Exec(`CREATE TRIGGER reject_month_audit BEFORE INSERT ON audit_log WHEN NEW.entity_type='budget_month' BEGIN SELECT RAISE(ABORT,'audit deliberately unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMonthPlan(ctx, u, "2026-10", p.Month); err == nil {
		t.Fatal("copy succeeded when audit failed")
	}
	if after := plannerCounts(t, s); !reflect.DeepEqual(before, after) {
		t.Fatalf("audit failure persisted a partial copy: %v -> %v", before, after)
	}
	if copy := plannerRead(t, s, "2026-10"); copy.Exists || len(copy.Projects) != 0 {
		t.Fatalf("failed copy persisted: %+v", copy)
	}
	if source := plannerRead(t, s, p.Month); len(source.Projects[0].Heads[0].Lines) != 2 {
		t.Fatal("failed copy changed source")
	}
	// The new planner also commits its month, masters, lines and audit together.
	p.Month = "2026-11"
	if err := s.SaveBudgetPlan(ctx, u, p, false); err == nil {
		t.Fatal("planner succeeded when audit failed")
	}
	if after := plannerCounts(t, s); !reflect.DeepEqual(before, after) {
		t.Fatalf("planner audit failure persisted changes: %v -> %v", before, after)
	}
}
