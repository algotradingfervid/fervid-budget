package store

import (
	"context"
	"errors"
	"testing"
)

func TestPaymentReferenceRejectsWhitespaceWithoutWriting(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, requester, manager, head := seedRequestParty(t, s, ctx)
	for i, mode := range []string{"neft", "rtgs", "upi", "cheque", "card", "dd", "bank_transfer", "cash"} {
		t.Run(mode, func(t *testing.T) {
			id := seedApprovedRequest(t, s, ctx, i+1, requester.ID, manager, head, 101, 101)
			if err := s.ReserveRequest(ctx, acc, id); err != nil {
				t.Fatal(err)
			}
			in := PaymentInput{PaidOn: "2026-06-15", Amount: 101, PaymentMode: mode, ReferenceNo: " \t\n "}
			if _, err := historicalSettlement(s, ctx, acc, id, in, "settled", "", nil); !errors.Is(err, ErrValidation) {
				t.Fatalf("blank reference accepted: %v", err)
			}
			rows, err := s.RequestPayments(ctx, id)
			if err != nil || len(rows) != 0 {
				t.Fatalf("rejection wrote payments: %v %v", rows, err)
			}
			in.ReferenceNo = "BANK-101-" + mode
			if _, err := historicalSettlement(s, ctx, acc, id, in, "settled", "", nil); err != nil {
				t.Fatalf("corrected reference rejected: %v", err)
			}
		})
	}
}
