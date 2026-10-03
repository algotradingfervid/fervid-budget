package store

import (
	"context"
	"testing"
	"time"
)

// A payment may not be dated before its request's approval, and both dates are
// compared as the server's local calendar day. approved_at is stored in UTC, so
// an approval late in the evening west of UTC falls on the next UTC day; the
// rule must still accept a payment made the same local day.
func TestPaymentOnApprovalDayAcceptedInLocalTime(t *testing.T) {
	saved := time.Local
	time.Local = time.FixedZone("UTC-8", -8*60*60)
	t.Cleanup(func() { time.Local = saved })

	s := newTestStore(t)
	ctx := context.Background()
	account, requester, managerID, head := seedRequestParty(t, s)
	id := seedApprovedRequest(t, s, 1, requester.ID, managerID, head, 10000, 10000)
	// Approved at 20:00 on 26 September local time, which is 04:00 on
	// 27 September in UTC.
	if _, err := s.DB().Exec(`UPDATE payment_requests SET approved_at='2026-09-27 04:00:00' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, account, id); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 26, 21, 0, 0, 0, time.Local)
	in := PaymentInput{Amount: 10000, PaidOn: "2026-09-26", PaymentMode: "neft", ReferenceNo: "UTR-LOCAL-1", Now: now}
	if _, err := s.RecordPaymentForRequest(ctx, account, id, in, "settled", "", nil); err != nil {
		t.Fatalf("payment on the local approval day was refused: %v", err)
	}
}
