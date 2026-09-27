package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/mail"
	"strings"
)

// Validate against the same live grant as ListApprovers, inside the caller's
// write transaction so a user save cannot commit half a profile update.
func requireDefaultApproverTx(ctx context.Context, tx *sql.Tx, userID, approverID int64) error {
	if approverID == 0 {
		return nil
	}
	if userID == approverID {
		return fmt.Errorf("%w: a user cannot be their own default approver", ErrValidation)
	}
	var eligible int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u WHERE u.id=? AND u.active=1 AND EXISTS (
 SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id
 WHERE ur.user_id=u.id AND rp.resource='approval' AND rp.action='approve')`, approverID).Scan(&eligible); err != nil {
		return err
	}
	if eligible == 0 {
		return fmt.Errorf("%w: choose an active default approver with permission to approve requests", ErrValidation)
	}
	return nil
}

// Notification delivery consumes comma-separated bare addresses. Validate that
// exact grammar before storing a rule, including rules whose email is off.
func validateRecipientList(label, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%w: %s must contain comma-separated email addresses on one line", ErrValidation, label)
	}
	for _, part := range strings.Split(value, ",") {
		address := strings.TrimSpace(part)
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Name != "" || parsed.Address != address {
			return fmt.Errorf("%w: %s contains an invalid email address; enter addresses such as name@example.com separated by commas", ErrValidation, label)
		}
	}
	return nil
}
