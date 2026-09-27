// permissions.go defines the permission vocabulary (which actions exist on
// which resources), the PermissionSet every access check asks, and the reads
// and writes for roles and user-role assignments. The privilege rules those
// writes must pass (role ceiling, last administrator) live in security.go.

package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// PermissionSet is the read side of authorisation: everything that renders the
// shell, the nav, the mobile tab bar and every gated control asks this
// interface, never a role name.
//
// The implementation is dbPermissionSet below, resolved from the roles tables
// by EffectivePermissions. Callers only ever depend on Can and Scope.
type PermissionSet interface {
	// Can reports whether the holder may perform action on resource.
	Can(resource, action string) bool
	// Scope reports the data scope the holder has over resource — "own",
	// "assigned" or ScopeAll. An empty string means no scope at all.
	Scope(resource string) string
}

// ScopeAll is the broadest data scope: the holder sees every row of a resource.
const ScopeAll = "all"

// Grant is a single resource/action pair. Both halves are literal: matching is
// exact and there are no wildcards.
type Grant struct {
	Resource string
	Action   string
}

// Role is a named bundle of grants and data scopes that can be assigned to
// users. System roles (Requester, Manager, Accounts, Admin) are seeded and
// cannot be renamed or deleted.
type Role struct {
	ID          int64
	Name        string
	Description string
	IsSystem    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ScopeGrant limits which rows of a scoped resource (request or payment) a role
// reaches: "own", "assigned" or "all".
type ScopeGrant struct {
	Resource string
	Scope    string
}

// dbPermissionSet is the PermissionSet built from a user's roles in the
// database (or from an explicit grant list, see NewPermissionSet).
type dbPermissionSet struct {
	grants map[string]map[string]struct{} // resource -> set of actions
	scopes map[string]string              // scoped resource -> broadest scope
}

var _ PermissionSet = (*dbPermissionSet)(nil)

// EmptyPermissions is the deny-all set. PermissionSet is an interface, so there
// is no `PermissionSet{}` literal to fall back on: callers that cannot resolve
// permissions return this instead.
func EmptyPermissions() PermissionSet { return &dbPermissionSet{} }

// NewPermissionSet builds a permission set from an explicit grant list. It is
// the constructor for fixtures and for any caller that already knows the exact
// grants; EffectivePermissions is what resolves them from the database.
// Matching is exact — there are no wildcards. "*" was a Casbin artefact, and a
// stale wildcard row is precisely the thing that would grant everything to
// everyone once roles became data.
func NewPermissionSet(grants []Grant, scopes []ScopeGrant) PermissionSet {
	ps := &dbPermissionSet{grants: map[string]map[string]struct{}{}, scopes: map[string]string{}}
	for _, g := range grants {
		ps.add(g.Resource, g.Action)
	}
	for _, sc := range scopes {
		ps.mergeScope(sc.Resource, sc.Scope)
	}
	return ps
}

// AllGrants is every canonical (resource, action) pair — what the Admin role
// holds. Exported so fixtures can build an all-powerful permission set without
// reaching for a wildcard.
func AllGrants() []Grant { return adminGrants() }

// Can reports whether the set holds resource:action.
func (p *dbPermissionSet) Can(resource, action string) bool {
	acts, ok := p.grants[resource]
	if !ok {
		return false
	}
	_, ok = acts[action]
	return ok
}

// Scope returns the broadest data scope the user has for a scoped resource, or
// "" if the resource is not scoped or the user has no scope grant for it.
func (p *dbPermissionSet) Scope(resource string) string {
	return p.scopes[resource]
}

// add records one grant.
func (p *dbPermissionSet) add(resource, action string) {
	if p.grants == nil {
		p.grants = map[string]map[string]struct{}{}
	}
	if p.grants[resource] == nil {
		p.grants[resource] = map[string]struct{}{}
	}
	p.grants[resource][action] = struct{}{}
}

// mergeScope records a data scope, keeping the broadest one seen per resource.
func (p *dbPermissionSet) mergeScope(resource, scope string) {
	if p.scopes == nil {
		p.scopes = map[string]string{}
	}
	if scopeRank(scope) > scopeRank(p.scopes[resource]) {
		p.scopes[resource] = scope
	}
}

// scopeRank orders data scopes from narrowest to broadest: none < own <
// assigned < all.
func scopeRank(scope string) int {
	switch scope {
	case "all":
		return 3
	case "assigned":
		return 2
	case "own":
		return 1
	default:
		return 0
	}
}

// resourceActions is the canonical permission vocabulary: every action that can
// be granted on each resource. It is the single source of truth for grant
// validation, for the Admin grant set, and for the presentation map in
// internal/app/permmap.go. Matching is exact; there are no wildcards.
var resourceActions = map[string][]string{
	"request":              {"view", "create", "edit", "withdraw", "reraise", "comment", "cancel"},
	"approval":             {"approve", "reject", "return", "reassign", "accept_partial", "cancel"},
	"payment":              {"view", "create", "edit", "void", "process", "settle", "mark_partial", "hold"},
	"reservation":          {"reserve", "release", "reassign"},
	"attachment":           {"view", "create"},
	"vendor":               {"view", "create", "edit"},
	"vendor_bank":          {"view", "edit"},
	"project":              {"view", "create", "edit"},
	"head":                 {"view", "create", "edit"},
	"budget":               {"view", "edit"},
	"month":                {"view", "create", "lock"},
	"grid":                 {"view", "export"},
	"report":               {"view", "export"},
	"recoverable_category": {"view", "create", "edit", "delete"},
	"recoverable_report":   {"view", "export"},
	"user":                 {"view", "create", "edit"},
	"role":                 {"view", "create", "edit", "delete"},
	"notification":         {"view", "edit"},
	"config":               {"view", "edit"},
	"audit":                {"view"},
	"backup":               {"view", "create"},
}

// resourceOrder fixes a deterministic display/iteration order for the matrix UI
// and for generating the Admin grant set. Every key of resourceActions must
// appear here exactly once — TestPermissionVocabularyIsCanonical enforces it.
var resourceOrder = []string{
	"request", "approval", "payment", "reservation", "attachment",
	"vendor", "vendor_bank",
	"project", "head", "budget", "month",
	"grid", "report", "recoverable_category", "recoverable_report",
	"user", "role", "notification", "config", "audit", "backup",
}

// scopedResources are the resources that take a data scope.
var scopedResources = map[string]bool{"request": true, "payment": true}

// ValidGrant reports whether resource:action is in the permission vocabulary.
func ValidGrant(resource, action string) bool {
	return slices.Contains(resourceActions[resource], action)
}

// ValidScope reports whether scope ("own", "assigned" or "all") may be set on
// resource; only scoped resources take one.
func ValidScope(resource, scope string) bool {
	if !scopedResources[resource] {
		return false
	}
	return scope == "own" || scope == "assigned" || scope == "all"
}

// scanRole reads one roles row; sql.ErrNoRows becomes ErrNotFound.
func scanRole(scanner rowScanner) (Role, error) {
	var r Role
	var sys int
	err := scanner.Scan(&r.ID, &r.Name, &r.Description, &sys, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return r, ErrNotFound
	}
	r.IsSystem = sys == 1
	return r, err
}

// AllRoles lists every role, system roles first, then by name.
func (s *Store) AllRoles(ctx context.Context) ([]Role, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,description,is_system,created_at,updated_at FROM roles ORDER BY is_system DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Role reads one role, or ErrNotFound.
func (s *Store) Role(ctx context.Context, id int64) (Role, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,description,is_system,created_at,updated_at FROM roles WHERE id=?`, id)
	return scanRole(row)
}

// CreateRole adds an empty, non-system role and returns its id. Grants are set
// afterwards with UpdateRolePermissions.
func (s *Store) CreateRole(ctx context.Context, actor User, name, description string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("%w: role name is required", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO roles(name,description,is_system) VALUES(?,?,0)`, name, strings.TrimSpace(description))
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "create", EntityType: "role", EntityID: &id, Summary: "Created role " + name, After: map[string]any{"id": id, "name": name}}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// UpdateRole renames a role and rewrites its description. System role names are
// immutable: seedSystemRoles and backfillUserRoles both resolve them by name,
// so a rename would make the next migration run seed a duplicate.
func (s *Store) UpdateRole(ctx context.Context, actor User, id int64, name, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: role name is required", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireRoleCeiling(ctx, tx, actor.ID, []int64{id}); err != nil {
		return err
	}

	var current string
	var sys int
	err = tx.QueryRowContext(ctx, `SELECT name,is_system FROM roles WHERE id=?`, id).Scan(&current, &sys)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if sys == 1 && !strings.EqualFold(current, name) {
		return fmt.Errorf("%w: system roles cannot be renamed", ErrForbidden)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE roles SET name=?, description=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		name, strings.TrimSpace(description), id); err != nil {
		return classify(err)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "update", EntityType: "role", EntityID: &id, Summary: "Updated role " + name, Before: map[string]any{"name": current}, After: map[string]any{"name": name}}); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteRole removes a non-system role the actor may manage. A role that is
// still assigned to anyone is refused.
func (s *Store) DeleteRole(ctx context.Context, actor User, id int64) error {
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireRoleCeiling(ctx, tx, actor.ID, []int64{id}); err != nil {
		return err
	}
	var name string
	var sys int
	err = tx.QueryRowContext(ctx, `SELECT name,is_system FROM roles WHERE id=?`, id).Scan(&name, &sys)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if sys == 1 {
		return fmt.Errorf("%w: system roles cannot be deleted", ErrForbidden)
	}
	// Refuse to delete a role that is still assigned: user_roles cascades on
	// delete, so it would silently strip the role's permissions from every holder.
	var holders int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_roles WHERE role_id=?`, id).Scan(&holders); err != nil {
		return err
	}
	if holders > 0 {
		return fmt.Errorf("%w: %s is assigned to %d %s — reassign them before deleting the role",
			ErrForbidden, name, holders, pluralUsers(holders))
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM roles WHERE id=?`, id); err != nil {
		return classify(err)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "delete", EntityType: "role", EntityID: &id, Summary: "Deleted role " + name, Before: map[string]any{"id": id, "name": name}}); err != nil {
		return err
	}
	return tx.Commit()
}

// pluralUsers returns "user" or "users" for n.
func pluralUsers(n int) string {
	if n == 1 {
		return "user"
	}
	return "users"
}

// RolePermissions returns one role's grants and data scopes, ordered by
// resource and action.
func (s *Store) RolePermissions(ctx context.Context, id int64) ([]Grant, []ScopeGrant, error) {
	return roleGrants(ctx, s.db, id)
}

// UpdateRolePermissions replaces a role's entire grant and scope set
// atomically and audits the before and after. The checks run in this order:
// every grant and scope must be in the vocabulary (validateGrants), the actor
// must be allowed to make this edit (requireRolePermissionEdit), and the role
// must exist.
func (s *Store) UpdateRolePermissions(ctx context.Context, actor User, roleID int64, grants []Grant, scopes []ScopeGrant) error {
	if err := validateGrants(grants, scopes); err != nil {
		return err
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireRolePermissionEdit(ctx, tx, actor.ID, roleID, grants, scopes); err != nil {
		return err
	}

	var name string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM roles WHERE id=?`, roleID).Scan(&name); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	oldGrants, oldScopes, err := roleGrants(ctx, tx, roleID)
	if err != nil {
		return err
	}
	before := roleGrantAudit(name, oldGrants, oldScopes)
	after := roleGrantAudit(name, grants, scopes)
	if err := replaceRoleGrantsTx(ctx, tx, roleID, grants, scopes); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE roles SET updated_at=CURRENT_TIMESTAMP WHERE id=?`, roleID); err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "update", EntityType: "role", EntityID: &roleID, Summary: "Updated permissions for role " + name, Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

// validateGrants checks every grant and scope against the vocabulary.
func validateGrants(grants []Grant, scopes []ScopeGrant) error {
	for _, g := range grants {
		if !ValidGrant(g.Resource, g.Action) {
			return fmt.Errorf("%w: unknown permission %s:%s", ErrValidation, g.Resource, g.Action)
		}
	}
	for _, sc := range scopes {
		if !ValidScope(sc.Resource, sc.Scope) {
			return fmt.Errorf("%w: invalid data scope %s=%s", ErrValidation, sc.Resource, sc.Scope)
		}
	}
	return nil
}

// requireRolePermissionEdit decides whether actorID may give roleID exactly
// these grants and scopes. An administrator may edit any role. Anyone else must
// pass the role ceiling for the role as it is now, may not edit a system role
// at all, and may only hand out grants and scopes they hold themselves.
// Whoever the actor is, the built-in Admin role must keep every user and role
// management permission, so nobody can lock the system out of administration.
func requireRolePermissionEdit(ctx context.Context, tx *sql.Tx, actorID, roleID int64, grants []Grant, scopes []ScopeGrant) error {
	admin, err := adminMember(ctx, tx, actorID)
	if err != nil {
		return err
	}
	if !admin {
		if err := requireRoleCeiling(ctx, tx, actorID, []int64{roleID}); err != nil {
			return err
		}
		var system bool
		if err := tx.QueryRowContext(ctx, `SELECT is_system FROM roles WHERE id=?`, roleID).Scan(&system); err != nil {
			return err
		}
		if system {
			return ErrForbidden
		}
		ps, err := effectivePermissionsQ(ctx, tx, actorID)
		if err != nil {
			return err
		}
		if !within(ps, grants, scopes) {
			return ErrForbidden
		}
	}
	protected, err := isAdminRole(ctx, tx, roleID)
	if err != nil {
		return err
	}
	if protected && !keepsAdminManagement(grants, scopes) {
		return fmt.Errorf("%w: administrator management permissions cannot be removed", ErrValidation)
	}
	return nil
}

// keepsAdminManagement reports whether the grants still include every user and
// role management permission in the vocabulary.
func keepsAdminManagement(grants []Grant, scopes []ScopeGrant) bool {
	next := NewPermissionSet(grants, scopes)
	for _, g := range adminGrants() {
		if (g.Resource == "user" || g.Resource == "role") && !next.Can(g.Resource, g.Action) {
			return false
		}
	}
	return true
}

// roleGrantAudit is the before/after shape recorded when a role's permissions
// change: the role name, its grants as sorted "resource:action" strings (an
// empty list, never null) and its data scopes as a resource-to-scope map. The
// audit screen parses exactly this shape.
func roleGrantAudit(name string, grants []Grant, scopes []ScopeGrant) map[string]any {
	perms := make([]string, 0, len(grants))
	for _, g := range grants {
		perms = append(perms, g.Resource+":"+g.Action)
	}
	sort.Strings(perms)
	dataScopes := map[string]string{}
	for _, sc := range scopes {
		dataScopes[sc.Resource] = sc.Scope
	}
	return map[string]any{"role": name, "permissions": perms, "data_scopes": dataScopes}
}

// replaceRoleGrantsTx overwrites a role's grants and data scopes inside tx.
func replaceRoleGrantsTx(ctx context.Context, tx *sql.Tx, roleID int64, grants []Grant, scopes []ScopeGrant) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id=?`, roleID); err != nil {
		return err
	}
	for _, g := range grants {
		if _, err := tx.ExecContext(ctx, `INSERT INTO role_permissions(role_id,resource,action) VALUES(?,?,?)`, roleID, g.Resource, g.Action); err != nil {
			return classify(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM role_data_scope WHERE role_id=?`, roleID); err != nil {
		return err
	}
	for _, sc := range scopes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO role_data_scope(role_id,resource,scope) VALUES(?,?,?)`, roleID, sc.Resource, sc.Scope); err != nil {
			return classify(err)
		}
	}
	return nil
}

// CopyRole creates a new, non-system role named `name` whose grants and scopes
// are copied from srcID.
func (s *Store) CopyRole(ctx context.Context, actor User, srcID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("%w: role name is required", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var srcName string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM roles WHERE id=?`, srcID).Scan(&srcName); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if err := requireRoleCeiling(ctx, tx, actor.ID, []int64{srcID}); err != nil {
		return 0, err
	}

	res, err := tx.ExecContext(ctx, `INSERT INTO roles(name,description,is_system) SELECT ?, description, 0 FROM roles WHERE id=?`, name, srcID)
	if err != nil {
		return 0, classify(err)
	}
	newID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO role_permissions(role_id,resource,action) SELECT ?, resource, action FROM role_permissions WHERE role_id=?`, newID, srcID); err != nil {
		return 0, classify(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO role_data_scope(role_id,resource,scope) SELECT ?, resource, scope FROM role_data_scope WHERE role_id=?`, newID, srcID); err != nil {
		return 0, classify(err)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "create", EntityType: "role", EntityID: &newID, Summary: "Copied role " + srcName + " to " + name, After: map[string]any{"id": newID, "name": name, "source": srcName}}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newID, nil
}

// SetUserRoles replaces a user's entire role assignment atomically. Every role
// id must exist; unknown ids are rejected before any write.
//
// The Users screen saves roles through SaveUser, together with the profile.
// This is the assignment-only write, used by test fixtures that need a user to
// hold a particular role without restating the rest of their profile.
func (s *Store) SetUserRoles(ctx context.Context, actor User, userID int64, roleIDs []int64) error {
	unique := map[int64]struct{}{}
	for _, id := range roleIDs {
		unique[id] = struct{}{}
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT active FROM users WHERE id=?`, userID).Scan(&active); err != nil {
		return err
	}
	if err := protectUserChange(ctx, tx, actor.ID, userID, roleIDs, active); err != nil {
		return err
	}

	for id := range unique {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM roles WHERE id=?`, id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("%w: role %d does not exist", ErrValidation, id)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id=?`, userID); err != nil {
		return err
	}
	for id := range unique {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, userID, id); err != nil {
			return classify(err)
		}
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "update", EntityType: "user", EntityID: &userID, Summary: fmt.Sprintf("Updated role assignment (%d roles)", len(unique)), After: map[string]any{"role_ids": roleIDs}}); err != nil {
		return err
	}
	return tx.Commit()
}

// UserRoles lists the roles assigned to a user, system roles first.
func (s *Store) UserRoles(ctx context.Context, userID int64) ([]Role, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id,r.name,r.description,r.is_system,r.created_at,r.updated_at
		FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=? ORDER BY r.is_system DESC, r.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetUserDefaultApprover records who approves this user's requests by default.
// approverID 0 clears the field. A user can never be their own default
// approver, and the approver must be an active user who can approve requests.
//
// SaveUser applies the same rules through requireDefaultApproverTx. This is
// the field-only write, used by tests that need to set just this column.
func (s *Store) SetUserDefaultApprover(ctx context.Context, actor User, userID, approverID int64) error {
	if approverID != 0 && approverID == userID {
		return fmt.Errorf("%w: a user cannot be their own default approver", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var name string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM users WHERE id=?`, userID).Scan(&name); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	var approver any
	if err := requireDefaultApproverTx(ctx, tx, userID, approverID); err != nil {
		return err
	}
	if approverID != 0 {
		approver = approverID
	}

	if _, err := tx.ExecContext(ctx, `UPDATE users SET default_approver_id=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, approver, userID); err != nil {
		return classify(err)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "update", EntityType: "user", EntityID: &userID, Summary: "Updated default approver for " + name, After: map[string]any{"default_approver_id": approverID}}); err != nil {
		return err
	}
	return tx.Commit()
}

// EffectivePermissions resolves a user's complete permission view: the union of
// every grant across all assigned roles, and the broadest data scope per scoped
// resource (all > assigned > own). User id 0 has no permissions.
//
// Unlike effectivePermissionsQ it does not check that the user is active: it
// answers for the roles the user holds. Signing in and session checks are what
// keep a deactivated user out.
func (s *Store) EffectivePermissions(ctx context.Context, userID int64) (PermissionSet, error) {
	if userID == 0 {
		return &dbPermissionSet{grants: map[string]map[string]struct{}{}, scopes: map[string]string{}}, nil
	}
	return loadPermissions(ctx, s.db, userID, false)
}
