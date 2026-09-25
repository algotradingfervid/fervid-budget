package store

import (
	"context"
	"errors"
	"testing"
)

func TestBackdatedRecoveryCannotSpendFutureInstallmentOrInvalidateLaterEvent(t *testing.T) {
	ctx := context.Background()
	s, u, headID, id := recoveryFixture(t)
	// Initial ₹7,000 January payout plus ₹5,000 in March.
	payRecoverable(t, s, ctx, u, headID, id, "2026-03-01", 500000)
	in := recoveryInput("history-before-later-installment", 800000)
	if _, err := s.RecordRecovery(ctx, u, id, in); !errors.Is(err, ErrValidation) {
		t.Fatalf("February recovery borrowed March payout: %v", err)
	}
	in = recoveryInput("history-february-full-return", 700000)
	if _, err := s.RecordRecovery(ctx, u, id, in); err != nil {
		t.Fatal(err)
	}
	// Overall balance is ₹5,000 and January itself had ₹7,000 available.
	// But inserting January ₹1,000 would make February's already-recorded
	// return exceed everything paid by then. The later-event guard must reject it.
	in = recoveryInput("history-invalidate-february-event", 100000)
	in.OccurredOn = "2026-01-15"
	if _, err := s.RecordRecovery(ctx, u, id, in); !errors.Is(err, ErrValidation) {
		t.Fatalf("backdate invalidated a later running balance: %v", err)
	}
	rows, err := s.RecoveryEvents(ctx, u, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Amount != 700000 {
		t.Fatalf("refused backdate changed history: %+v", rows)
	}
	in = recoveryInput("history-valid-march-recovery", 500000)
	in.OccurredOn = "2026-03-01"
	if _, err := s.RecordRecovery(ctx, u, id, in); err != nil {
		t.Fatal(err)
	}
	report, err := s.RecoverableReport(ctx, RecoverableReportOptions{Viewer: RecoverableViewerAll()})
	if err != nil {
		t.Fatal(err)
	}
	if len(report) != 1 || report[0].Amount != 0 || report[0].RecoveredAmount != 1200000 {
		t.Fatalf("unexpected reconciled balance: %+v", report)
	}
}
