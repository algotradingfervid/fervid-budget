package store

import (
	"context"
	"errors"
	"testing"
)

// recoverables-2 / grid-2. A recoverable payment is a deposit, not budget
// spend, so the variance grid's Recent Payments panel must not list it; and
// every payment row carries its request's treatment so a screen can name a
// head-less recoverable instead of printing an empty "Project / Head".
func TestListPaymentsCanLeaveOutRecoverablesAndCarriesTreatment(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s)
	budgetPay, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-09-10", Amount: 12345, VendorPayee: "Landlord", PaymentMode: "cash"})
	if err != nil {
		t.Fatal(err)
	}
	// Shaped as the real write path leaves it since v8: the request is linked to
	// a project but carries no head, and neither does its payment.
	req := insertRequestRow(t, s, requestRow{
		"number": "PR-2026-000801", "status": "completed", "treatment": "recoverable", "type": "employee_advance",
		"recoverable_category": "emd", "recoverable_category_id": recoverableCategoryID(t, s, "emd"),
		"project_id": projectOfHead(t, s, headID), "head_id": nil, "amount": 250000, "approved_amount": 250000,
		"purpose": "Deposit", "counterparty": "Ridge Metro", "expected_return_date": "2027-01-31", "repayment_notes": "Refund on close",
		"requester_id": actor.ID, "manager_id": actor.ID, "submitted_at": sqlNow,
	})
	res, err := s.DB().ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,entered_by,request_id,settlement) VALUES(NULL,'2026-09-25',250000,'Ridge Metro',?,?,'settled')`, actor.ID, req)
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
	actor, rent := seedActorAndHead(t, s)
	projectID := projectOfHead(t, s, rent)
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
	actor, rent := seedActorAndHead(t, s)
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

func TestUXGridNeutralStatusAndFilteredTotalScope(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, head := seedActorAndHead(t, s)
	zero, err := s.Grid(ctx, "2026-04", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if zero.NoActivity != 1 || zero.NotPaid != 0 || zero.Rows[0].Status != "no-activity" {
		t.Fatalf("zero activity=%+v", zero)
	}
	if err = s.SetBudget(ctx, actor, head, "2026-04", 10000); err != nil {
		t.Fatal(err)
	}
	planned, err := s.Grid(ctx, "2026-04", "not-paid", "")
	if err != nil {
		t.Fatal(err)
	}
	if planned.NotPaid != 1 || planned.NoActivity != 0 || planned.Total.Budget != 10000 || !planned.Filtered || planned.ScopeLabel != "Filtered subtotal" {
		t.Fatalf("planned=%+v", planned)
	}
	none, err := s.Grid(ctx, "2026-04", "", "missing-name")
	if err != nil {
		t.Fatal(err)
	}
	if len(none.Rows) != 0 || none.Total.Budget != 0 || !none.Filtered {
		t.Fatalf("filter=%+v", none)
	}
}

func TestUXReportStableIDsAndPaymentHeadScope(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, head := seedActorAndHead(t, s)
	if err := s.SetBudget(ctx, actor, head, "2026-04", 10000); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Report(ctx, "2026-04", "2026-04", "heads")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].HeadID != head || rows[0].ProjectID == 0 {
		t.Fatalf("report IDs=%+v", rows)
	}
	id, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: head, PaidOn: "2026-04-01", Amount: 10000, VendorPayee: "Fixture", PaymentMode: "cash"})
	if err != nil {
		t.Fatal(err)
	}
	payments, err := s.ListPayments(ctx, PaymentListOptions{Month: "2026-04", HeadID: head, ProjectID: rows[0].ProjectID, Scope: ScopeAll, ExcludeRecoverable: true})
	if err != nil || len(payments) != 1 || payments[0].ID != id {
		t.Fatalf("payments=%+v err=%v", payments, err)
	}
	wrong, err := s.ListPayments(ctx, PaymentListOptions{Month: "2026-04", HeadID: head + 100, Scope: ScopeAll})
	if err != nil || len(wrong) != 0 {
		t.Fatalf("wrong head filter=%+v err=%v", wrong, err)
	}
}
