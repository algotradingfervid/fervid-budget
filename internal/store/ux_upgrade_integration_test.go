package store

import (
	"context"
	"testing"
)

func TestUXUpgradeFromVersion15PreservesHistoricalWriteoffAndPayment(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, requester, managerID, headID := seedRequestParty(t, s, ctx)
	manager, err := s.UserByID(ctx, managerID)
	if err != nil {
		t.Fatal(err)
	}
	id := seedApprovedRequest(t, s, ctx, 1, requester.ID, managerID, headID, 1200000, 1200000)
	if err := s.ReserveRequest(ctx, acc, id); err != nil {
		t.Fatal(err)
	}
	payID, err := s.RecordPaymentForRequest(ctx, acc, id, PaymentInput{PaidOn: "2026-06-15", Amount: 700000}, "partial", "Legacy accepted shortfall", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptPartial(ctx, manager, id, "Legacy decision"); err != nil {
		t.Fatal(err)
	}
	// Reconstruct the exact pre-upgrade tables/index shape in this disposable DB.
	// It contains a real completed_partial request and its immutable payment.
	for _, sql := range []string{
		`DROP TABLE recovery_events`,
		`DROP TABLE password_resets`,
		`DROP TABLE password_reset_throttle`,
		`DROP INDEX idx_payment_submission`,
		`DROP INDEX idx_payments_request`,
		`ALTER TABLE payments DROP COLUMN submission_key`,
		`CREATE UNIQUE INDEX idx_payments_request ON payments(request_id) WHERE request_id IS NOT NULL`,
		`PRAGMA user_version=15`,
	} {
		if _, err := s.DB().Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrate(s.DB()); err != nil {
		t.Fatalf("version15 upgrade: %v", err)
	}
	if err := migrate(s.DB()); err != nil {
		t.Fatalf("second upgrade: %v", err)
	}
	req, err := s.Request(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if req.Status != "completed_partial" || req.PaidAmount != 700000 {
		t.Fatalf("historical writeoff reopened/changed: %s paid=%d", req.Status, req.PaidAmount)
	}
	pay, err := s.Payment(ctx, payID)
	if err != nil {
		t.Fatal(err)
	}
	if pay.Amount != 700000 || pay.PartialReason != "Legacy accepted shortfall" || pay.Settlement != "partial" {
		t.Fatalf("historical payment changed: %+v", pay)
	}
	if err := s.ContinuePartial(ctx, manager, id, "Accidentally reopen"); err == nil {
		t.Fatal("terminal historical writeoff reopened")
	}
	var recoveryCount int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM recovery_events`).Scan(&recoveryCount); err != nil {
		t.Fatal(err)
	}
	if recoveryCount != 0 {
		t.Fatal("upgrade invented recoveries")
	}
	rows, err := s.DB().Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("upgrade damaged foreign keys")
	}
}
