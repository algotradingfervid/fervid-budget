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
// Phase 0 supplies the implementation below, a snapshot of the current Casbin
// role policies. Phase 1 adds the DB-backed permission engine (roles,
// permissions and data scopes stored in SQLite) behind this same interface, so
// callers never change: they keep depending on Can and Scope only.
type PermissionSet interface {
	// Can reports whether the holder may perform action on resource.
	Can(resource, action string) bool
	// Scope reports the data scope the holder has over resource: ScopeAll
	// today, narrower values ("project", "own") once Phase 1 lands. An empty
	// string means no access at all.
	Scope(resource string) string
}

// ScopeAll is the only scope Phase 0 can express: a holder either sees every
// row of a resource or none of it.
const ScopeAll = "all"

// Grant is a single resource/action pair. "*" in either position matches
// anything, mirroring the wildcard policy the admin role carries.
type Grant struct {
	Resource string
	Action   string
}

type staticPermissionSet struct {
	grants []Grant
}

// NewPermissionSet snapshots grants into an immutable permission set. The
// snapshot is safe to hand to a template and to call repeatedly without
// re-entering the policy engine.
func NewPermissionSet(grants []Grant) PermissionSet {
	snapshot := make([]Grant, len(grants))
	copy(snapshot, grants)
	return staticPermissionSet{grants: snapshot}
}

func (p staticPermissionSet) Can(resource, action string) bool {
	if resource == "" || action == "" {
		return false
	}
	for _, g := range p.grants {
		if (g.Resource == "*" || g.Resource == resource) && (g.Action == "*" || g.Action == action) {
			return true
		}
	}
	return false
}

func (p staticPermissionSet) Scope(resource string) string {
	if resource == "" {
		return ""
	}
	for _, g := range p.grants {
		if g.Resource == "*" || g.Resource == resource {
			return ScopeAll
		}
	}
	return ""
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
// internal/app/permmap.go. 21 resources, 64 (resource, action) pairs.
//
// vendor, vendor_bank, reservation and config are declared here even though
// Phases 1V and 3 build the screens behind them: the roles matrix has to be
// able to grant them from day one, and a vocabulary that grows per phase would
// make every earlier role definition incomplete.
var resourceActions = map[string][]string{
	"request":              {"view", "create", "edit", "withdraw", "reraise", "comment"},
	"approval":             {"approve", "reject", "return", "reassign", "accept_partial"},
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM roles WHERE id=?`, id); err != nil {
		return classify(err)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "delete", EntityType: "role", EntityID: &id, Summary: "Deleted role " + name, Before: map[string]any{"id": id, "name": name}}); err != nil {
		return err
	}
	return tx.Commit()
}
