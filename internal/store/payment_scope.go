package store

import (
	"context"
	"database/sql"
)

// A reservation does not preserve visibility after a role or activity change.
// Check the current row scope on the writer transaction, so permission edits
// cannot race a previously opened payment confirmation.
func paymentRequestScopeTx(ctx context.Context, tx *sql.Tx, actorID, requestID int64) error {
	var allowed int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_requests r JOIN users u ON u.id=? AND u.active=1
 WHERE r.id=? AND EXISTS (
 SELECT 1 FROM user_roles ur JOIN role_data_scope ds ON ds.role_id=ur.role_id
 WHERE ur.user_id=u.id AND ds.resource='request' AND
 (ds.scope='all' OR (ds.scope='own' AND r.requester_id=u.id) OR
 (ds.scope='assigned' AND (r.manager_id=u.id OR r.requester_id=u.id))))`, actorID, requestID).Scan(&allowed)
	if err != nil {
		return err
	}
	if allowed == 0 {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_requests WHERE id=?`, requestID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
		return ErrForbidden
	}
	return nil
}
