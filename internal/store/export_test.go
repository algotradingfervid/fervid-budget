// export_test.go holds store methods that only the tests in this package use,
// so production code cannot call them.

package store

import "context"

// ResetLoginAttempts clears a user's failed-login counter and lock by id.
func (s *Store) ResetLoginAttempts(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET attempt_count=0,last_attempt=NULL,locked=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=?`, userID)
	return classify(err)
}

// RecentPayments returns the newest non-voided payments, newest first.
func (s *Store) RecentPayments(ctx context.Context, limit int) ([]Payment, error) {
	rows, err := s.db.QueryContext(ctx, paymentSelect+` WHERE py.voided_at IS NULL ORDER BY py.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
