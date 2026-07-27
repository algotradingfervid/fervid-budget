package store

import (
	"context"
	"database/sql"
	"fmt"
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

// Role and ScopeGrant are the Phase 1 permission types consumed by
// internal/auth and every later phase (overview §8). PermissionSet (an
// interface) and Grant are already declared in this file by Phase 0 — do not
// redeclare them.
type Role struct {
	ID          int64
	Name        string
	Description string
	IsSystem    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ScopeGrant struct {
	Resource string
	Scope    string
}

// dbPermissionSet is the effective, resolved permission view for one user,
// computed from the roles tables. It implements the PermissionSet interface
// Phase 0 declared, so the shell, the nav, the tab bar and every gated control
// keep asking Can and Scope exactly as they already do — only the answer's
// source changes.
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

func (p *dbPermissionSet) add(resource, action string) {
	if p.grants == nil {
		p.grants = map[string]map[string]struct{}{}
	}
	if p.grants[resource] == nil {
		p.grants[resource] = map[string]struct{}{}
	}
	p.grants[resource][action] = struct{}{}
}

func (p *dbPermissionSet) mergeScope(resource, scope string) {
	if p.scopes == nil {
		p.scopes = map[string]string{}
	}
	if scopeRank(scope) > scopeRank(p.scopes[resource]) {
		p.scopes[resource] = scope
	}
}

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

// resourceActions is the canonical permission vocabulary (overview §5, extended
// by adoption-spec D2). It is the single source of truth for grant validation,
// for the Admin grant set, and for the presentation map in
// internal/app/permmap.go. 21 resources, 66 (resource, action) pairs.
//
// vendor, vendor_bank, reservation and config are declared here even though
// Phases 1V and 3 build the screens behind them: the roles matrix has to be
// able to grant them from day one, and a vocabulary that grows per phase would
// make every earlier role definition incomplete. request:cancel and
// approval:cancel are here for the same reason: Phase 2's cancellation flow
// gates its routes on them.
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

var scopedResources = map[string]bool{"request": true, "payment": true}

func ValidGrant(resource, action string) bool {
	acts, ok := resourceActions[resource]
	if !ok {
		return false
	}
	for _, a := range acts {
		if a == action {
			return true
		}
	}
	return false
}

func ValidScope(resource, scope string) bool {
	if !scopedResources[resource] {
		return false
	}
	return scope == "own" || scope == "assigned" || scope == "all"
}

func scanRole(scanner interface{ Scan(...any) error }) (Role, error) {
	var r Role
	var sys int
	err := scanner.Scan(&r.ID, &r.Name, &r.Description, &sys, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return r, ErrNotFound
	}
	r.IsSystem = sys == 1
	return r, err
}

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

func (s *Store) Role(ctx context.Context, id int64) (Role, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,description,is_system,created_at,updated_at FROM roles WHERE id=?`, id)
	return scanRole(row)
}

func (s *Store) CreateRole(ctx context.Context, actor User, name, description string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("%w: role name is required", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

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

func (s *Store) DeleteRole(ctx context.Context, actor User, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
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
	// user_roles.role_id cascades on delete, so deleting an assigned role used to
	// strip it from every holder in silence — each of whom lost the permissions it
	// carried on their very next request, with nothing on screen connecting cause
	// to effect (F-G-022). The holder count is already on the roles screen,
	// immediately above the Delete button; this is the check that consults it.
	// R9 permits deleting a non-system role; it does not permit doing so blind.
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

func pluralUsers(n int) string {
	if n == 1 {
		return "user"
	}
	return "users"
}

func (s *Store) RolePermissions(ctx context.Context, id int64) ([]Grant, []ScopeGrant, error) {
	grantRows, err := s.db.QueryContext(ctx, `SELECT resource,action FROM role_permissions WHERE role_id=? ORDER BY resource,action`, id)
	if err != nil {
		return nil, nil, err
	}
	defer grantRows.Close()
	var grants []Grant
	for grantRows.Next() {
		var g Grant
		if err := grantRows.Scan(&g.Resource, &g.Action); err != nil {
			return nil, nil, err
		}
		grants = append(grants, g)
	}
	if err := grantRows.Err(); err != nil {
		return nil, nil, err
	}

	scopeRows, err := s.db.QueryContext(ctx, `SELECT resource,scope FROM role_data_scope WHERE role_id=? ORDER BY resource`, id)
	if err != nil {
		return nil, nil, err
	}
	defer scopeRows.Close()
	var scopes []ScopeGrant
	for scopeRows.Next() {
		var sc ScopeGrant
		if err := scopeRows.Scan(&sc.Resource, &sc.Scope); err != nil {
			return nil, nil, err
		}
		scopes = append(scopes, sc)
	}
	return grants, scopes, scopeRows.Err()
}

// UpdateRolePermissions replaces a role's entire grant + scope set atomically.
// Every grant and scope is validated against the canonical vocabulary first.
func (s *Store) UpdateRolePermissions(ctx context.Context, actor User, roleID int64, grants []Grant, scopes []ScopeGrant) error {
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var name string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM roles WHERE id=?`, roleID).Scan(&name); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
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
	if _, err := tx.ExecContext(ctx, `UPDATE roles SET updated_at=CURRENT_TIMESTAMP WHERE id=?`, roleID); err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "update", EntityType: "role", EntityID: &roleID, Summary: "Updated permissions for role " + name, After: map[string]any{"grants": len(grants), "scopes": len(scopes)}}); err != nil {
		return err
	}
	return tx.Commit()
}

// CopyRole creates a new, non-system role named `name` whose grants and scopes
// are copied from srcID.
func (s *Store) CopyRole(ctx context.Context, actor User, srcID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("%w: role name is required", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
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
func (s *Store) SetUserRoles(ctx context.Context, actor User, userID int64, roleIDs []int64) error {
	unique := map[int64]struct{}{}
	for _, id := range roleIDs {
		unique[id] = struct{}{}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

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
// A user can never be their own default approver — self-approval is not
// acceptable, and the rule lives here rather than in the <select> because a
// hidden option is not validation. approverID 0 clears the field.
func (s *Store) SetUserDefaultApprover(ctx context.Context, actor User, userID, approverID int64) error {
	if approverID != 0 && approverID == userID {
		return fmt.Errorf("%w: a user cannot be their own default approver", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
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
	if approverID != 0 {
		var active int
		err := tx.QueryRowContext(ctx, `SELECT active FROM users WHERE id=?`, approverID).Scan(&active)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%w: the chosen approver does not exist", ErrValidation)
		}
		if err != nil {
			return err
		}
		if active != 1 {
			return fmt.Errorf("%w: the chosen approver is not an active user", ErrValidation)
		}
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
// resource (all > assigned > own).
func (s *Store) EffectivePermissions(ctx context.Context, userID int64) (PermissionSet, error) {
	ps := &dbPermissionSet{grants: map[string]map[string]struct{}{}, scopes: map[string]string{}}
	if userID == 0 {
		return ps, nil
	}
	grantRows, err := s.db.QueryContext(ctx, `SELECT rp.resource, rp.action
		FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id
		WHERE ur.user_id=?`, userID)
	if err != nil {
		return ps, err
	}
	defer grantRows.Close()
	for grantRows.Next() {
		var resource, action string
		if err := grantRows.Scan(&resource, &action); err != nil {
			return ps, err
		}
		ps.add(resource, action)
	}
	if err := grantRows.Err(); err != nil {
		return ps, err
	}

	scopeRows, err := s.db.QueryContext(ctx, `SELECT rds.resource, rds.scope
		FROM user_roles ur JOIN role_data_scope rds ON rds.role_id=ur.role_id
		WHERE ur.user_id=?`, userID)
	if err != nil {
		return ps, err
	}
	defer scopeRows.Close()
	for scopeRows.Next() {
		var resource, scope string
		if err := scopeRows.Scan(&resource, &scope); err != nil {
			return ps, err
		}
		ps.mergeScope(resource, scope)
	}
	return ps, scopeRows.Err()
}
