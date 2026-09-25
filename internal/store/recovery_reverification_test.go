package store

import (
	"context"
	"errors"
	"testing"
)

func TestRecoveryReplayCannotConfirmDifferentAmount(t *testing.T) {
	ctx := context.Background()
	s, actor, _, id := recoveryFixture(t)
	in := recoveryInput("changed-recovery-confirmation", 200000)
	first, err := s.RecordRecovery(ctx, actor, id, in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.RecordRecovery(ctx, actor, id, in)
	if err != nil || again != first {
		t.Fatalf("identical retry must be idempotent: %d %v", again, err)
	}
	in.Amount = 300000
	in.Reference = "CHANGED-RECEIPT"
	if _, err := s.RecordRecovery(ctx, actor, id, in); !errors.Is(err, ErrValidation) {
		t.Fatalf("edited old confirmation returns %v, falsely confirming changed recovery", err)
	}
	rows, err := s.RecoveryEvents(ctx, actor, id)
	if err != nil || len(rows) != 1 || rows[0].Amount != 200000 {
		t.Fatalf("replay changed history: %+v %v", rows, err)
	}
}
