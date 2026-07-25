package store

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
