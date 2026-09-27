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
	rows, err := s.db.QueryContext(ctx, `SELECT py.id,COALESCE(py.head_id,0),COALESCE(h.project_id,0),COALESCE(p.name,''),COALESCE(h.name,''),py.paid_on,py.amount,
		COALESCE(py.vendor_payee,''),COALESCE(py.payment_mode,''),COALESCE(py.invoice_no,''),COALESCE(py.reference_no,''),COALESCE(py.remarks,''),
		py.entered_by,u.name,py.updated_by,py.voided_by,COALESCE(py.void_reason,''),py.voided_at,py.created_at,py.updated_at
		FROM payments py LEFT JOIN heads h ON h.id=py.head_id LEFT JOIN projects p ON p.id=h.project_id JOIN users u ON u.id=py.entered_by
		WHERE py.voided_at IS NULL
		ORDER BY py.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.ID, &p.HeadID, &p.ProjectID, &p.Project, &p.Head, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.EnteredByName, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
