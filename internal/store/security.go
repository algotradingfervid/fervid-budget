// security.go holds the privilege rules that every role and user write must
// pass, plus the shared readers they are built on.
//
// The central idea is the "role ceiling": someone who is not an administrator
// may only create, edit, copy or assign a role whose every permission and data
// scope they already hold themselves, so nobody can hand out more than they
// have. The built-in Admin role is out of reach for non-administrators
// altogether, and the system always keeps at least one active administrator.
//
// The file also holds the payment write-scope check (paymentWriteAccess), the
// request read check used for notification recipients (CanViewRequest), and
// the database triggers that cap money values (installFinancialLimits).

package store

import (
	"context"
	"database/sql"
	"fmt"
)

// adminRoleSQL is the rule for the built-in administrator role: the system role
// named exactly "Admin". That role can never lose its user and role management
// permissions, can never be edited or assigned by a non-administrator, and must
// always have at least one active holder. The roles table must be aliased r.
const adminRoleSQL = `r.is_system=1 AND r.name='Admin'`

// adminMember reports whether user id is active and holds the built-in Admin role.
func adminMember(ctx context.Context, q querier, id int64) (bool, error) {
	var yes bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id WHERE u.id=? AND u.active=1 AND `+adminRoleSQL+`)`, id).Scan(&yes)
	return yes, err
}

// isAdminRole reports whether roleID is the built-in Admin role. A role id that
// does not exist returns sql.ErrNoRows.
func isAdminRole(ctx context.Context, q querier, roleID int64) (bool, error) {
	var yes bool
	err := q.QueryRowContext(ctx, `SELECT `+adminRoleSQL+` FROM roles r WHERE r.id=?`, roleID).Scan(&yes)
	return yes, err
}

// eachStringPair runs a query that returns two text columns and calls fn for
// every row, in the order the query returns them.
func eachStringPair(ctx context.Context, q querier, query string, arg any, fn func(a, b string)) error {
	rows, err := q.QueryContext(ctx, query, arg)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			return err
		}
		fn(a, b)
	}
	return rows.Err()
}

// roleGrants loads one role's grants and data scopes, ordered by resource (and
// action). Both slices are nil when the role has none.
func roleGrants(ctx context.Context, q querier, roleID int64) ([]Grant, []ScopeGrant, error) {
	var grants []Grant
	if err := eachStringPair(ctx, q, `SELECT resource,action FROM role_permissions WHERE role_id=? ORDER BY resource,action`, roleID,
		func(resource, action string) { grants = append(grants, Grant{resource, action}) }); err != nil {
		return nil, nil, err
	}
	var scopes []ScopeGrant
	if err := eachStringPair(ctx, q, `SELECT resource,scope FROM role_data_scope WHERE role_id=? ORDER BY resource`, roleID,
		func(resource, scope string) { scopes = append(scopes, ScopeGrant{resource, scope}) }); err != nil {
		return nil, nil, err
	}
	return grants, scopes, nil
}

// within reports whether ps already covers every grant and data scope: each
// grant is held, and no scope is broader than the one ps has for that resource.
// This is the role ceiling test.
func within(ps PermissionSet, grants []Grant, scopes []ScopeGrant) bool {
	for _, g := range grants {
		if !ps.Can(g.Resource, g.Action) {
			return false
		}
	}
	for _, sc := range scopes {
		if scopeRank(sc.Scope) > scopeRank(ps.Scope(sc.Resource)) {
			return false
		}
	}
	return true
}

// loadPermissions builds a user's permission set from every role they hold:
// the union of the grants, and the broadest data scope per resource. With
// activeOnly set, a deactivated user gets an empty set.
func loadPermissions(ctx context.Context, q querier, userID int64, activeOnly bool) (*dbPermissionSet, error) {
	ps := &dbPermissionSet{grants: map[string]map[string]struct{}{}, scopes: map[string]string{}}
	from, where := `user_roles ur`, `ur.user_id=?`
	if activeOnly {
		from, where = `users u JOIN user_roles ur ON ur.user_id=u.id`, `u.id=? AND u.active=1`
	}
	if err := eachStringPair(ctx, q, `SELECT rp.resource,rp.action FROM `+from+` JOIN role_permissions rp ON rp.role_id=ur.role_id WHERE `+where, userID, ps.add); err != nil {
		return ps, err
	}
	err := eachStringPair(ctx, q, `SELECT ds.resource,ds.scope FROM `+from+` JOIN role_data_scope ds ON ds.role_id=ur.role_id WHERE `+where, userID, ps.mergeScope)
	return ps, err
}

// effectivePermissionsQ returns the permissions of an active user; a deactivated
// user holds none. It runs on q, so a write can check the actor inside its own
// transaction.
func effectivePermissionsQ(ctx context.Context, q querier, id int64) (PermissionSet, error) {
	return loadPermissions(ctx, q, id, true)
}

// requireRoleCeiling returns nil only when the actor may manage every role in
// roleIDs. Unknown role ids are a validation error, checked first for everyone.
// Administrators may then manage any role. Anyone else may never manage the
// built-in Admin role, and may manage another role only when its grants and
// scopes are all within their own (the role ceiling); otherwise ErrForbidden.
func requireRoleCeiling(ctx context.Context, q querier, actorID int64, roleIDs []int64) error {
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
		protected, err := isAdminRole(ctx, q, id)
		if err != nil {
			return err
		}
		if protected {
			return ErrForbidden
		}
		grants, scopes, err := roleGrants(ctx, q, id)
		if err != nil {
			return err
		}
		if !within(ps, grants, scopes) {
			return ErrForbidden
		}
	}
	return nil
}

// userRoleIDs lists the role ids assigned to a user.
func userRoleIDs(ctx context.Context, q querier, id int64) ([]int64, error) {
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

// protectUserChange enforces the rules for changing a user's roles or active
// flag. The actor's role ceiling must cover both the roles the user has now
// and the roles they are being given. And if the change would take the Admin
// role away from an active administrator (by demotion or deactivation), it is
// refused when the actor is doing it to themselves, or when nobody else would
// be left as an active administrator.
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
			yes, err := isAdminRole(ctx, tx, id)
			if err != nil {
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
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT u.id) FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id WHERE u.active=1 AND u.id<>? AND `+adminRoleSQL, userID).Scan(&others); err != nil {
			return err
		}
		if others == 0 {
			return fmt.Errorf("%w: at least one active administrator is required", ErrValidation)
		}
	}
	return nil
}

// paymentWriteAccess allows the actor to change payment p when their payment
// data scope is "all", or when it is "own" or "assigned" and they entered the
// payment themselves. Otherwise it returns ErrNotFound, so a payment the actor
// may not touch is not even revealed to exist.
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

// CanViewRequest answers whether a person may read request r on a screen that
// exists to show requests: they need the request:view grant and their request
// data scope has to reach the row (RequestInScope). Notification recipients and
// inbox reads use it. HTTP handlers check the grant through the route and the
// scope through the same predicate.
func CanViewRequest(ps PermissionSet, uid int64, r Request) bool {
	if ps == nil || !ps.Can("request", "view") {
		return false
	}
	return RequestInScope(ps.Scope("request"), uid, r)
}

// UserCanViewRequest is CanViewRequest for a user id: it loads the user's
// current permissions (none if deactivated) and answers false on any error.
func (s *Store) UserCanViewRequest(ctx context.Context, id int64, r Request) bool {
	ps, err := effectivePermissionsQ(ctx, s.db, id)
	return err == nil && CanViewRequest(ps, id, r)
}

// installFinancialLimits bounds all future financial writes, including imports
// and planner submissions, with INSERT and UPDATE triggers on every table that
// stores an amount. 9e14 paise per table is below float64's exact-integer range
// and leaves ample headroom for cross-table totals and the bounded 120-month
// reporting window. If existing data already breaks a limit it refuses to start
// rather than changing anything.
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
