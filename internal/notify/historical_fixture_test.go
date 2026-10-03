package notify

import (
	"context"
	"database/sql"
	"fervidbudget/internal/store"
)

// backdateApproval moves a request's approval to the start of paidOn when it is
// later than that. The store refuses a payment dated before the approval, so a
// fixture that settles on a fixed past date must first make the approval
// precede it. Dates that are not a plain YYYY-MM-DD are left alone.
func backdateApproval(ctx context.Context, db *sql.DB, requestID int64, paidOn string) error {
	if len(paidOn) != 10 {
		return nil
	}
	start := paidOn + " 00:00:00"
	_, err := db.ExecContext(ctx, `UPDATE payment_requests SET approved_at=? WHERE id=? AND approved_at>?`, start, requestID, start)
	return err
}

// historicalSettlement records a payment on a fixed past date, back-dating the
// approval first (see backdateApproval). Tests about the payment-date rule
// itself call RecordPaymentForRequest directly instead.
func historicalSettlement(ctx context.Context, s *store.Store, actor store.User, id int64, in store.PaymentInput, settlement, reason string, attachment *store.AttachmentInput) (int64, error) {
	if err := backdateApproval(ctx, s.DB(), id, in.PaidOn); err != nil {
		return 0, err
	}
	return s.RecordPaymentForRequest(ctx, actor, id, in, settlement, reason, attachment)
}
