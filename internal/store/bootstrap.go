package store

import (
	"context"
	"strings"
)

// HasUsers checks all accounts, including disabled accounts. A restored system
// must never gain a new bootstrap administrator just because none is active.
func (s *Store) HasUsers(ctx context.Context) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists)
	return exists, err
}

// BootstrapAdminIfEmpty creates the first administrator only. The emptiness
// check is part of the INSERT, the transaction's first database operation, so
// concurrent startups cannot each add an administrator under different names.
// Its role assignment commits together with the account.
func (s *Store) BootstrapAdminIfEmpty(ctx context.Context, email, name, hash string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if err := validateUserFields(email, name, "admin"); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO users(email,name,password_hash,role,active)
		SELECT ?,?,?,'admin',1 WHERE NOT EXISTS(SELECT 1 FROM users)`, email, name, hash)
	if err != nil {
		return classify(err)
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if err := assignDefaultRoleTx(ctx, tx, id, "admin"); err != nil {
		return err
	}
	return tx.Commit()
}
