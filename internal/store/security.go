package store

import (
	"context"
	"database/sql"
	"fmt"
)

type securityQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func adminMember(ctx context.Context, q securityQuerier, id int64) (bool, error) {
	var yes bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id WHERE u.id=? AND u.active=1 AND r.is_system=1 AND r.name='Admin')`, id).Scan(&yes)
	return yes, err
}
func (s *Store) IsAdmin(ctx context.Context, id int64) bool {
	ok, err := adminMember(ctx, s.db, id)
	return err == nil && ok
}

func effectivePermissionsQ(ctx context.Context, q securityQuerier, id int64) (PermissionSet, error) {
	ps := &dbPermissionSet{}
	rows, err := q.QueryContext(ctx, `SELECT rp.resource,rp.action FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN role_permissions rp ON rp.role_id=ur.role_id WHERE u.id=? AND u.active=1`, id)
	if err != nil {
		return ps, err
	}
	for rows.Next() {
		var resource, action string
		if err = rows.Scan(&resource, &action); err != nil {
			rows.Close()
			return ps, err
		}
		ps.add(resource, action)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ps, err
	}
	rows, err = q.QueryContext(ctx, `SELECT ds.resource,ds.scope FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN role_data_scope ds ON ds.role_id=ur.role_id WHERE u.id=? AND u.active=1`, id)
	if err != nil {
		return ps, err
	}
	defer rows.Close()
	for rows.Next() {
		var resource, scope string
		if err = rows.Scan(&resource, &scope); err != nil {
			return ps, err
		}
		ps.mergeScope(resource, scope)
	}
	return ps, rows.Err()
}

func requireRoleCeiling(ctx context.Context, q securityQuerier, actorID int64, roleIDs []int64) error {
	admin, err := adminMember(ctx, q, actorID)
	if err != nil {
		return err
	}
	for _, id := range roleIDs {
		var exists bool
		if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM roles WHERE id=?)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: unknown role %d", ErrValidation, id)
		}
	}
	if admin {
		return nil
	}
	ps, err := effectivePermissionsQ(ctx, q, actorID)
	if err != nil {
		return err
	}
	for _, id := range roleIDs {
		var protected bool
		if err = q.QueryRowContext(ctx, `SELECT is_system=1 AND name='Admin' FROM roles WHERE id=?`, id).Scan(&protected); err != nil {
			return err
		}
		if protected {
			return ErrForbidden
		}
		rows, err := q.QueryContext(ctx, `SELECT resource,action FROM role_permissions WHERE role_id=?`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r, a string
			if err = rows.Scan(&r, &a); err != nil {
				rows.Close()
				return err
			}
			if !ps.Can(r, a) {
				rows.Close()
				return ErrForbidden
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		rows, err = q.QueryContext(ctx, `SELECT resource,scope FROM role_data_scope WHERE role_id=?`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r, sc string
			if err = rows.Scan(&r, &sc); err != nil {
				rows.Close()
				return err
			}
			if scopeRank(sc) > scopeRank(ps.Scope(r)) {
				rows.Close()
				return ErrForbidden
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func userRoleIDs(ctx context.Context, q securityQuerier, id int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT role_id FROM user_roles WHERE user_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func protectUserChange(ctx context.Context, tx *sql.Tx, actorID, userID int64, roles []int64, active bool) error {
	before, err := userRoleIDs(ctx, tx, userID)
	if err != nil {
		return err
	}
	if err = requireRoleCeiling(ctx, tx, actorID, append(append([]int64{}, before...), roles...)); err != nil {
		return err
	}
	wasAdmin, err := adminMember(ctx, tx, userID)
	if err != nil {
		return err
	}
	var staysAdmin bool
	if active {
		for _, id := range roles {
			var yes bool
			if err = tx.QueryRowContext(ctx, `SELECT is_system=1 AND name='Admin' FROM roles WHERE id=?`, id).Scan(&yes); err != nil {
				return err
			}
			staysAdmin = staysAdmin || yes
		}
	}
	if wasAdmin && !staysAdmin {
		if actorID == userID {
			return fmt.Errorf("%w: you cannot deactivate or demote your own administrator account", ErrValidation)
		}
		var others int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT u.id) FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id WHERE u.active=1 AND u.id<>? AND r.name='Admin' AND r.is_system=1`, userID).Scan(&others); err != nil {
			return err
		}
		if others == 0 {
			return fmt.Errorf("%w: at least one active administrator is required", ErrValidation)
		}
	}
	return nil
}

func paymentWriteAccess(ctx context.Context, tx *sql.Tx, actorID int64, p Payment) error {
	ps, err := effectivePermissionsQ(ctx, tx, actorID)
	if err != nil {
		return err
	}
	scope := ps.Scope("payment")
	if scope == ScopeAll || ((scope == "own" || scope == "assigned") && p.EnteredBy == actorID) {
		return nil
	}
	return ErrNotFound
}

// CanViewRequest is shared by HTTP, notification recipients and inbox reads.
func CanViewRequest(ps PermissionSet, uid int64, r Request) bool {
	if ps == nil || !ps.Can("request", "view") {
		return false
	}
	switch ps.Scope("request") {
	case ScopeAll:
		return true
	case "own":
		return r.RequesterID == uid
	case "assigned":
		return r.RequesterID == uid || r.ManagerID == uid
	}
	return false
}

func (s *Store) UserCanViewRequest(ctx context.Context, id int64, r Request) bool {
	ps, err := effectivePermissionsQ(ctx, s.db, id)
	return err == nil && CanViewRequest(ps, id, r)
}

// Bound all future financial writes, including imports and planner submissions.
// 9e14 paise per table is below float64's exact-integer range and leaves ample
// headroom for cross-table totals and the bounded 120-month reporting window.
func installFinancialLimits(db *sql.DB) error {
	for _, table := range []string{"payments", "payment_requests", "budgets", "budget_lines", "recovery_events"} {
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`, table).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			continue
		}
		var unsafe bool
		if err := db.QueryRow(fmt.Sprintf(`SELECT COALESCE(MAX(amount),0)>100000000000 OR COALESCE(MIN(amount),0)<0 OR TOTAL(amount)>900000000000000 FROM %s`, table)).Scan(&unsafe); err != nil {
			return err
		}
		if unsafe {
			return fmt.Errorf("financial limits: existing %s values require operator review before starting; no values were changed", table)
		}
		for _, op := range []string{"INSERT", "UPDATE"} {
			statement := fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS security_limit_%s_%s AFTER %s ON %s BEGIN SELECT CASE WHEN NEW.amount<0 OR NEW.amount>100000000000 OR (SELECT TOTAL(amount) FROM %s)>900000000000000 THEN RAISE(ABORT,'financial limit exceeded') END; END`, table, op, op, table, table)
			if _, err := db.Exec(statement); err != nil {
				return err
			}
		}
	}
	return nil
}
