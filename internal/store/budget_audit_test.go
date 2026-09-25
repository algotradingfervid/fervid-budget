package store

import (
	"context"
	"testing"
)

// audit-2: saving the budget form wrote an audit row for every head it
// carried, changed or not, each reading only "Saved budget ₹N". A no-op save
// now writes nothing, and a real change names the head, the month and the
// amounts on both sides.
func TestSetBudgetsAuditsOnlyChangedBudgetsAndNamesHeadAndMonth(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, rent := seedActorAndHead(t, s, ctx)
	var projectID int64
	if err := s.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, rent).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	power, err := s.UpsertHead(ctx, 0, projectID, "Power", "5", true, 2)
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		t.Helper()
		var n int
		if err := s.DB().QueryRow(`SELECT COUNT(*) FROM audit_log WHERE entity_type='budget'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// First save: Rent gets a budget, Power is submitted as the ₹0.00 the
	// screen shows for an unbudgeted head — no change, so no row.
	if err := s.SetBudgets(ctx, actor, "2026-07", []BudgetInput{{HeadID: rent, Amount: 4500000}, {HeadID: power, Amount: 0}}); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 1 {
		t.Fatalf("first save wrote %d budget audit rows, want 1 (only Rent changed)", got)
	}
	trail := newestBudgetAudit(t, s)
	if want := "Operations / Rent 2026-07: ₹0.00 → ₹45,000.00"; trail[0].Summary != want || trail[0].Action != "create" {
		t.Fatalf("create row = %q (%s), want %q (create)", trail[0].Summary, trail[0].Action, want)
	}

	// The same form saved again, unchanged: nothing is written.
	if err := s.SetBudgets(ctx, actor, "2026-07", []BudgetInput{{HeadID: rent, Amount: 4500000}, {HeadID: power, Amount: 0}}); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 1 {
		t.Fatalf("an unchanged save wrote %d more budget audit rows", got-1)
	}

	// One real change among unchanged rows writes exactly one update row.
	if err := s.SetBudgets(ctx, actor, "2026-07", []BudgetInput{{HeadID: rent, Amount: 5000000}, {HeadID: power, Amount: 0}}); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 2 {
		t.Fatalf("one change wrote %d budget audit rows, want 1", got-1)
	}
	trail = newestBudgetAudit(t, s)
	if want := "Operations / Rent 2026-07: ₹45,000.00 → ₹50,000.00"; trail[0].Summary != want || trail[0].Action != "update" {
		t.Fatalf("update row = %q (%s), want %q (update)", trail[0].Summary, trail[0].Action, want)
	}
	b, err := s.Budget(ctx, rent, "2026-07")
	if err != nil || b.Amount != 5000000 {
		t.Fatalf("Rent budget = %d, %v", b.Amount, err)
	}
}

// newestBudgetAudit returns the newest budget audit row first by id: several
// rows share a created_at second, so time alone does not order them.
func newestBudgetAudit(t *testing.T, s *Store) []AuditEntry {
	t.Helper()
	var e AuditEntry
	if err := s.DB().QueryRow(`SELECT action,summary FROM audit_log WHERE entity_type='budget' ORDER BY id DESC LIMIT 1`).Scan(&e.Action, &e.Summary); err != nil {
		t.Fatal(err)
	}
	return []AuditEntry{e}
}
