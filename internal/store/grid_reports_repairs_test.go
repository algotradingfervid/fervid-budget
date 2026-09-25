package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// recoverables-1 (F-E-05, second half). The dashboard tile learned that money
// which never left cannot be overdue; the By-category/By-counterparty rollup and
// the register's ageing=overdue filter did not, so an approved-but-unpaid row
// with a past return date showed red in its category row while the Total row
// and the tile beneath it read ₹0.00.
func TestOverdueRollupAndRegisterFilterIgnoreMoneyThatNeverLeft(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	asOf := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)

	paid := seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000701", "completed", "emd", "Coastal Power", "2026-06-30", 200000, false)
	payRecoverable(t, s, ctx, actor, headID, paid, "2026-02-12", 200000)
	unpaid := seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000702", "approved", "emd", "Ridge Metro", "2026-01-15", 250000, false)

	m, err := s.RecoverableMetrics(ctx, asOf, RecoverableViewerAll())
	if err != nil {
		t.Fatal(err)
	}
	for _, by := range []string{"category", "counterparty"} {
		rolls, err := s.RecoverableRollups(ctx, by, asOf, RecoverableViewerAll())
		if err != nil {
			t.Fatal(err)
		}
		var overdue int64
		for _, r := range rolls {
			overdue += r.Overdue
			if r.Label == "Ridge Metro" && r.Overdue != 0 {
				t.Fatalf("the unpaid counterparty row is overdue by %d; money that never left cannot be overdue", r.Overdue)
			}
		}
		if overdue != m.OverdueAmount {
			t.Fatalf("%s rollup rows add up to %d overdue while the tile and Total row say %d", by, overdue, m.OverdueAmount)
		}
	}

	rows, err := s.RecoverableReport(ctx, RecoverableReportOptions{Ageing: "overdue", AsOf: asOf, Viewer: RecoverableViewerAll()})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.RequestID == unpaid {
			t.Fatalf("ageing=overdue lists the unpaid row, labelled %q", r.AgeingLabel)
		}
	}
	if len(rows) != m.OverdueCount {
		t.Fatalf("ageing=overdue lists %d rows and the tile it is linked from counts %d", len(rows), m.OverdueCount)
	}
}

// recoverables-2 / grid-2. A recoverable payment is a deposit, not budget
// spend, so the variance grid's Recent Payments panel must not list it; and
// every payment row carries its request's treatment so a screen can name a
// head-less recoverable instead of printing an empty "Project / Head".
func TestListPaymentsCanLeaveOutRecoverablesAndCarriesTreatment(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	budgetPay, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-09-10", Amount: 12345, VendorPayee: "Landlord", PaymentMode: "cash"})
	if err != nil {
		t.Fatal(err)
	}
	// Shaped as the real write path leaves it since v8: the request is linked to
	// a project but carries no head, and neither does its payment.
	res, err := s.DB().ExecContext(ctx, `INSERT INTO payment_requests
		(number,status,treatment,type,recoverable_category,recoverable_category_id,project_id,head_id,amount,approved_amount,
		 purpose,counterparty,expected_return_date,repayment_notes,requester_id,manager_id,submitted_at)
		VALUES('PR-2026-000801','completed','recoverable','employee_advance','emd',(SELECT id FROM recoverable_categories WHERE code='emd'),
		 (SELECT project_id FROM heads WHERE id=?),NULL,250000,250000,'Deposit','Ridge Metro','2027-01-31','Refund on close',?,?,CURRENT_TIMESTAMP)`,
		headID, actor.ID, actor.ID)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := res.LastInsertId()
	res, err = s.DB().ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,entered_by,request_id,settlement) VALUES(NULL,'2026-09-25',250000,'Ridge Metro',?,?,'settled')`, actor.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	recPay, _ := res.LastInsertId()

	all, err := s.ListPayments(ctx, PaymentListOptions{Month: "2026-09", Scope: ScopeAll})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("the ledger lists %d payments, want both", len(all))
	}
	for _, p := range all {
		want := ""
		if p.ID == recPay {
			want = "recoverable"
		}
		if p.Treatment != want {
			t.Fatalf("payment %d treatment = %q, want %q", p.ID, p.Treatment, want)
		}
	}

	budgetOnly, err := s.ListPayments(ctx, PaymentListOptions{Month: "2026-09", Scope: ScopeAll, ExcludeRecoverable: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(budgetOnly) != 1 || budgetOnly[0].ID != budgetPay {
		t.Fatalf("ExcludeRecoverable listed %+v, want only the budget payment %d", budgetOnly, budgetPay)
	}

	one, err := s.Payment(ctx, recPay)
	if err != nil {
		t.Fatal(err)
	}
	if one.Treatment != "recoverable" {
		t.Fatalf("Payment(%d).Treatment = %q, want recoverable", recPay, one.Treatment)
	}
	byReq, err := s.PaymentForRequest(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if byReq.Treatment != "recoverable" {
		t.Fatalf("PaymentForRequest(%d).Treatment = %q, want recoverable", req, byReq.Treatment)
	}
}

// audit-2 (BV36). Saving the budget form used to write an "update" audit row for
// every head on the form, including the ones whose amount did not move, and the
// summary named neither the head nor the month.
func TestSetBudgetsAuditsOnlyChangedBudgetsWithAReadableSummary(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, rent := seedActorAndHead(t, s, ctx)
	var projectID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT project_id FROM heads WHERE id=?`, rent).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	power, err := s.UpsertHead(ctx, 0, projectID, "Power", "10", true, 2)
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		t.Helper()
		var n int
		if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE entity_type='budget'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if err := s.SetBudgets(ctx, actor, "2026-07", []BudgetInput{{HeadID: rent, Amount: 4500000}, {HeadID: power, Amount: 0}}); err != nil {
		t.Fatal(err)
	}
	// Power was submitted at ₹0 with no budget behind it. A head with no budget
	// already reads as ₹0 everywhere, so nothing changed: no row is written and
	// nothing is audited. Only Rent, which the user set, is recorded (audit-2,
	// review: the first save of a month used to log every head on the form).
	if got := count(); got != 1 {
		t.Fatalf("the first save wrote %d budget audit rows, want 1 (only Rent was set)", got)
	}
	if _, err := s.Budget(ctx, power, "2026-07"); err == nil {
		t.Fatal("a head submitted at ₹0 with no budget got a budget row it never had")
	}
	var summary string
	if err := s.DB().QueryRowContext(ctx, `SELECT summary FROM audit_log WHERE entity_type='budget' AND action='create' ORDER BY id LIMIT 1`).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if want := "Operations / Rent 2026-07: set to ₹45,000.00"; summary != want {
		t.Fatalf("create summary = %q, want %q", summary, want)
	}

	// The same form saved again with nothing changed is not a mutation.
	if err := s.SetBudgets(ctx, actor, "2026-07", []BudgetInput{{HeadID: rent, Amount: 4500000}, {HeadID: power, Amount: 0}}); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 1 {
		t.Fatalf("an unchanged save wrote %d new budget audit rows, want 0", got-1)
	}

	// One head changed: exactly one row, naming the head, the month and both amounts.
	if err := s.SetBudgets(ctx, actor, "2026-07", []BudgetInput{{HeadID: rent, Amount: 4500000}, {HeadID: power, Amount: 120000}}); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 2 {
		t.Fatalf("a one-head change wrote %d budget audit rows, want 1", got-1)
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT summary FROM audit_log WHERE entity_type='budget' ORDER BY id DESC LIMIT 1`).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if want := "Operations / Power 2026-07: set to ₹1,200.00"; summary != want {
		t.Fatalf("create summary = %q, want %q", summary, want)
	}

	// Rent changed: an update naming both amounts.
	if err := s.SetBudgets(ctx, actor, "2026-07", []BudgetInput{{HeadID: rent, Amount: 5000000}, {HeadID: power, Amount: 120000}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT summary FROM audit_log WHERE entity_type='budget' ORDER BY id DESC LIMIT 1`).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if want := "Operations / Rent 2026-07: ₹45,000.00 → ₹50,000.00"; summary != want {
		t.Fatalf("update summary = %q, want %q", summary, want)
	}

	b, err := s.Budget(ctx, power, "2026-07")
	if err != nil || b.Amount != 120000 {
		t.Fatalf("the changed budget reads %d (%v), want 120000", b.Amount, err)
	}

	// Clearing a budget that exists is a change, and is audited.
	if err := s.SetBudgets(ctx, actor, "2026-07", []BudgetInput{{HeadID: rent, Amount: 5000000}, {HeadID: power, Amount: 0}}); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 4 {
		t.Fatalf("clearing Power wrote %d budget audit rows, want 1", got-3)
	}
}

// An unknown head submitted at ₹0 with no budget behind it looks exactly like
// the untouched heads SetBudgets now skips — but the head lookup runs before
// the skip (the G branch's ordering, kept at integration), so a forged or stale
// head id still fails the whole batch instead of vanishing quietly.
func TestSetBudgetsRefusesAnUnknownHeadEvenAtZero(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, rent := seedActorAndHead(t, s, ctx)
	err := s.SetBudgets(ctx, actor, "2026-07", []BudgetInput{{HeadID: rent, Amount: 4500000}, {HeadID: 999999, Amount: 0}})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("SetBudgets with an unknown head at ₹0 = %v, want a validation error", err)
	}
	if _, err := s.Budget(ctx, rent, "2026-07"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Rent was written despite the failed batch: %v", err)
	}
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM audit_log WHERE entity_type='budget'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a failed batch wrote %d budget audit rows", n)
	}
}
