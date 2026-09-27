package store

import (
	"context"
	"database/sql"
)

// paymentRequestScopeTx re-checks, inside the writer transaction, that the actor
// may still see the request they are about to reserve, settle or review. Holding
// a reservation or knowing the id does not preserve visibility after a role or
// activity change, and reading the permissions in the same transaction means a
// permission edit cannot race a payment form that was opened earlier.
//
// A request the actor may not see answers ErrNotFound, exactly as a request that
// does not exist, so the id space cannot be probed.
func paymentRequestScopeTx(ctx context.Context, tx *sql.Tx, actorID, requestID int64) error {
	_, _, err := requestVisibleTx(ctx, tx, actorID, requestID)
	return err
}
