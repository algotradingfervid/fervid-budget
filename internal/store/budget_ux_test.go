package store

import (
	"context"
	"testing"
)

func TestUXGridNeutralStatusAndFilteredTotalScope(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, head := seedActorAndHead(t, s, ctx)
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
	actor, head := seedActorAndHead(t, s, ctx)
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
