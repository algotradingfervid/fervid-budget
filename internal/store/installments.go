package store

import (
	"context"
	"database/sql"
)

// upInstallments preserves every historical payment and settlement. Only the
// one-payment index is relaxed; existing writeoffs are never reopened.
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

// RequestPayments returns the complete append-only payment history, oldest first.
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
