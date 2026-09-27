// installments.go supports paying one approved request in several payments:
// the migration that allows many payments per request, reading a request's
// payment history, and finding a payment by its form submission key so a
// double-submitted form does not pay twice.
package store

import (
	"context"
	"database/sql"
)

// upInstallments is the migration that allows several payments per request:
// it adds payments.submission_key (unique when set) and turns the
// one-payment-per-request index into a plain index. Existing payments and
// settlements are kept as they are.
func upInstallments(tx *sql.Tx) error {
	has, err := columnExists(tx, "payments", "submission_key")
	if err != nil {
		return err
	}
	if !has {
		if _, err = tx.Exec(`ALTER TABLE payments ADD COLUMN submission_key TEXT`); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`DROP INDEX IF EXISTS idx_payments_request;
 CREATE INDEX IF NOT EXISTS idx_payments_request ON payments(request_id) WHERE request_id IS NOT NULL;
 CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_submission ON payments(submission_key) WHERE submission_key IS NOT NULL;`)
	return err
}

// RequestPayments returns every payment recorded against a request, voided
// ones included, oldest first. It reads the ids and closes that cursor before
// loading each payment through Payment, so only one query is open at a time.
func (s *Store) RequestPayments(ctx context.Context, requestID int64) ([]Payment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM payments WHERE request_id=? ORDER BY id`, requestID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var out []Payment
	for _, id := range ids {
		p, err := s.Payment(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// PaymentBySubmission returns the payment this actor already recorded on the
// request with this submission key, or ErrNotFound (also for a blank key). A
// resubmitted payment form uses it to show the first result instead of paying
// twice.
func (s *Store) PaymentBySubmission(ctx context.Context, requestID, actorID int64, key string) (Payment, error) {
	if key == "" {
		return Payment{}, ErrNotFound
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM payments WHERE request_id=? AND entered_by=? AND submission_key=?`, requestID, actorID, key).Scan(&id)
	if err == sql.ErrNoRows {
		return Payment{}, ErrNotFound
	}
	if err != nil {
		return Payment{}, err
	}
	return s.Payment(ctx, id)
}
