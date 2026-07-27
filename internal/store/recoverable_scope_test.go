package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The recoverables register is a second view over payment_requests, and F-G-016 /
// F-E-03 found that it asked nobody who was reading it: recoverable_report:view
// alone returned every counterparty, amount, requester and repayment note in the
// company, including rows the same caller is refused on /requests/{id}.
//
// These tests pin the SQL half of the fix, for all four readers. They matter
// disproportionately because the defect is invisible through the shipped roles —
// Accounts and Admin both hold request=all, so every seeded holder of the report
// permission legitimately sees everything, and only a custom narrow role can
// observe the difference. That is precisely why it survived the original build.

// seedTwoPartyRecoverables plants one paid recoverable for each of two requesters
// and returns them. B's counterparty is the disclosure canary: nothing A can read
// may ever name it.
func seedTwoPartyRecoverables(t *testing.T, s *Store, ctx context.Context) (a, b User, headID int64) {
	t.Helper()
	a, headID = seedActorAndHead(t, s, ctx)

	bID, err := s.CreateUser(ctx, "second@example.com", "Second Requester", "hash", "admin", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	b, err = s.UserByID(ctx, bID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}

	// A's two rows: one paid, one still awaiting payment.
	r1 := seedRecoverableRequestOnly(t, s, ctx, a, headID, 0, "PR-2026-000901", "completed", "emd", "Alpha Counterparty", "2026-11-30", 500000, false)
	payRecoverable(t, s, ctx, a, headID, r1, "2026-05-04", 500000)
	seedRecoverableRequestOnly(t, s, ctx, a, headID, 0, "PR-2026-000902", "approved", "icd", "Alpha Counterparty", "2026-12-31", 250000, false)

	// B's row, raised and managed by B, so it is outside every one of A's scopes.
	r3 := seedRecoverableRequestOnly(t, s, ctx, b, headID, 0, "PR-2026-000903", "completed", "pbg", "Bravo Counterparty", "2026-10-15", 900000, false)
	payRecoverable(t, s, ctx, b, headID, r3, "2026-05-06", 900000)
	return a, b, headID
}

func TestRecoverableReportAppliesTheRequestDataScope(t *testing.T) { // R3, R6 · F-G-016/F-E-03
	ctx := context.Background()
	s := newTestStore(t)
	a, b, _ := seedTwoPartyRecoverables(t, s, ctx)
	asOf := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	numbers := func(v RecoverableViewer) []string {
		t.Helper()
		rows, err := s.RecoverableReport(ctx, RecoverableReportOptions{AsOf: asOf, Viewer: v})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, r.Number)
		}
		return out
	}

	if got := numbers(RecoverableViewerAll()); len(got) != 3 {
		t.Fatalf("all = %v, want all three rows", got)
	}
	if got := numbers(RecoverableViewer{Scope: "own", ViewerID: a.ID}); strings.Join(got, ",") != "PR-2026-000902,PR-2026-000901" && strings.Join(got, ",") != "PR-2026-000901,PR-2026-000902" {
		t.Fatalf("own(A) = %v, want exactly A's two rows", got)
	}
	if got := numbers(RecoverableViewer{Scope: "own", ViewerID: b.ID}); len(got) != 1 || got[0] != "PR-2026-000903" {
		t.Fatalf("own(B) = %v, want only B's row", got)
	}
	// "assigned" admits the rows you manage AND the rows you raised, because that is
	// what canViewRequest admits and the register must not disagree with the detail
	// screen about whether a row exists.
	if got := numbers(RecoverableViewer{Scope: "assigned", ViewerID: b.ID}); len(got) != 1 || got[0] != "PR-2026-000903" {
		t.Fatalf("assigned(B) = %v, want only B's row", got)
	}
	// The zero value is the whole safety property of RecoverableViewer: a caller who
	// forgets it gets an empty screen, never someone else's money.
	if got := numbers(RecoverableViewer{}); len(got) != 0 {
		t.Fatalf("zero viewer = %v, want no rows at all", got)
	}
	if got := numbers(RecoverableViewer{Scope: "nonsense", ViewerID: a.ID}); len(got) != 0 {
		t.Fatalf("unknown scope = %v, want no rows at all", got)
	}
}

func TestRecoverableMetricsAndRollupsApplyTheRequestDataScope(t *testing.T) { // R3, R6 · F-G-016
	ctx := context.Background()
	s := newTestStore(t)
	a, b, _ := seedTwoPartyRecoverables(t, s, ctx)
	asOf := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	all, err := s.RecoverableMetrics(ctx, asOf, RecoverableViewerAll())
	if err != nil {
		t.Fatal(err)
	}
	if all.OutstandingCount != 3 || all.OutstandingAmount != 1650000 {
		t.Fatalf("all metrics = %d rows / %d paise, want 3 / 1650000", all.OutstandingCount, all.OutstandingAmount)
	}

	// A's own tiles must total A's own money. Summarising a scoped table without the
	// scope discloses exactly what the register hides: how much there is, and how
	// many of them.
	ownA, err := s.RecoverableMetrics(ctx, asOf, RecoverableViewer{Scope: "own", ViewerID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if ownA.OutstandingCount != 2 || ownA.OutstandingAmount != 750000 {
		t.Fatalf("own(A) metrics = %d rows / %d paise, want 2 / 750000", ownA.OutstandingCount, ownA.OutstandingAmount)
	}
	if ownA.PaidThisMonthCount != 0 {
		t.Fatalf("own(A) paid-this-month = %d, want 0 (A's payment is in May, asOf is July)", ownA.PaidThisMonthCount)
	}

	ownB, err := s.RecoverableMetrics(ctx, asOf, RecoverableViewer{Scope: "own", ViewerID: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if ownB.OutstandingCount != 1 || ownB.OutstandingAmount != 900000 {
		t.Fatalf("own(B) metrics = %d rows / %d paise, want 1 / 900000", ownB.OutstandingCount, ownB.OutstandingAmount)
	}

	if empty, err := s.RecoverableMetrics(ctx, asOf, RecoverableViewer{}); err != nil {
		t.Fatal(err)
	} else if empty.OutstandingCount != 0 || empty.OutstandingAmount != 0 {
		t.Fatalf("zero viewer metrics = %+v, want all zeroes", empty)
	}

	// The counterparty rollup is the sharpest disclosure on the summary screen: it
	// names the other party outright. A restricted to their own rows must not learn
	// that "Bravo Counterparty" is anybody this company deals with.
	labels := func(by string, v RecoverableViewer) string {
		t.Helper()
		rows, err := s.RecoverableRollups(ctx, by, asOf, v)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, r.Label)
		}
		return strings.Join(out, "|")
	}

	if got := labels("counterparty", RecoverableViewer{Scope: "own", ViewerID: a.ID}); strings.Contains(got, "Bravo") {
		t.Fatalf("own(A) counterparty rollup = %q, must not name B's counterparty", got)
	}
	if got := labels("counterparty", RecoverableViewerAll()); !strings.Contains(got, "Bravo") || !strings.Contains(got, "Alpha") {
		t.Fatalf("all counterparty rollup = %q, want both parties", got)
	}
	// B's row is the only pbg; A must not see that category grouping at all.
	if got := labels("category", RecoverableViewer{Scope: "own", ViewerID: a.ID}); strings.Contains(strings.ToLower(got), "bank guarantee") {
		t.Fatalf("own(A) category rollup = %q, must not include the category only B's row has", got)
	}
	if got := labels("category", RecoverableViewer{}); got != "" {
		t.Fatalf("zero viewer category rollup = %q, want nothing", got)
	}
	// The dimension check still runs before the scope, so a bad dimension is still
	// a validation error rather than a silently empty result.
	if _, err := s.RecoverableRollups(ctx, "project", asOf, RecoverableViewerAll()); err == nil {
		t.Fatal("RecoverableRollups(project) should still refuse an unknown dimension")
	}
}
