package store

import (
	"context"
)

// Existing workflow fixtures use fixed historical payment dates. Model an
// approval that really happened before that historical settlement. Security
// date tests call RecordPaymentForRequest directly and never use this helper.
func historicalSettlement(s *Store, ctx context.Context, actor User, id int64, in PaymentInput, settlement, reason string, attachment *AttachmentInput) (int64, error) {
	if len(in.PaidOn) == 10 {
		if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET approved_at=? WHERE id=? AND approved_at>?`, in.PaidOn+" 00:00:00", id, in.PaidOn+" 00:00:00"); err != nil {
			return 0, err
		}
	}
	return s.RecordPaymentForRequest(ctx, actor, id, in, settlement, reason, attachment)
}
