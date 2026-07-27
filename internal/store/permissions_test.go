package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func newRoleActor(t *testing.T, s *Store, ctx context.Context) User {
	t.Helper()
	id, err := s.CreateUser(ctx, "roleadmin@example.com", "Role Admin", "hash", "admin", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u, err := s.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	return u
}

func TestCreateRoleAndListAndDeleteCustomRole(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor := newRoleActor(t, s, ctx)

	id, err := s.CreateRole(ctx, actor, "Auditor", "Read-only reviewer")
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if _, err := s.CreateRole(ctx, actor, "auditor", "dupe"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("case-insensitive duplicate role = %v, want %v", err, ErrDuplicate)
	}
	if _, err := s.CreateRole(ctx, actor, "   ", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank role name = %v, want %v", err, ErrValidation)
	}

	role, err := s.Role(ctx, id)
	if err != nil || role.Name != "Auditor" || role.IsSystem {
		t.Fatalf("Role = %+v, %v", role, err)
	}

	if err := s.DeleteRole(ctx, actor, id); err != nil {
		t.Fatalf("DeleteRole custom: %v", err)
	}
	if _, err := s.Role(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Role after delete = %v, want %v", err, ErrNotFound)
	}
}

func TestPermissionVocabularyIsCanonical(t *testing.T) {
	if !ValidGrant("payment", "settle") || ValidGrant("payment", "read") {
		t.Fatal("payment vocabulary does not match overview §5")
	}
	if !ValidScope("request", "own") || ValidScope("budget", "all") || ValidScope("request", "everything") {
		t.Fatal("scope vocabulary does not match overview §5")
	}
	// Every resource in resourceOrder has a non-empty action list.
	for _, res := range resourceOrder {
		if len(resourceActions[res]) == 0 {
			t.Fatalf("resource %q has no actions", res)
		}
	}
	// resourceOrder is the iteration order for the matrix UI and for
	// adminGrants(); a resource missing from it would be silently unreachable.
	if len(resourceOrder) != len(resourceActions) {
		t.Fatalf("resourceOrder lists %d resources, resourceActions has %d", len(resourceOrder), len(resourceActions))
	}
	inOrder := map[string]bool{}
	for _, res := range resourceOrder {
		if inOrder[res] {
			t.Fatalf("resource %q appears twice in resourceOrder", res)
		}
		inOrder[res] = true
	}
	for res := range resourceActions {
		if !inOrder[res] {
			t.Fatalf("resource %q is missing from resourceOrder", res)
		}
	}

	// The four resources the design-system adoption spec adds (D2): the roles
	// matrix and Phases 1V-5 are written against them, so they must exist here
	// before any of those screens can be gated.
	for _, g := range []Grant{
		{"vendor", "view"}, {"vendor", "create"}, {"vendor", "edit"},
		{"vendor_bank", "view"}, {"vendor_bank", "edit"},
		{"reservation", "reserve"}, {"reservation", "release"}, {"reservation", "reassign"},
		{"config", "view"}, {"config", "edit"},
	} {
		if !ValidGrant(g.Resource, g.Action) {
			t.Fatalf("canonical grant %s:%s is missing from the vocabulary", g.Resource, g.Action)
		}
	}
	// The cancellation flow Phase 2 adds gates its routes on these two pairs;
	// the vocabulary has to carry them before those routes can be registered.
	for _, g := range []Grant{
		{"request", "cancel"}, {"approval", "cancel"},
	} {
		if !ValidGrant(g.Resource, g.Action) {
			t.Fatalf("canonical grant %s:%s is missing from the vocabulary", g.Resource, g.Action)
		}
	}
	// …and nothing beyond them: the lists are exact, not a prefix.
	for _, g := range []Grant{
		{"vendor", "delete"}, {"vendor_bank", "create"},
		{"reservation", "view"}, {"config", "create"},
		{"request", "approve"}, {"approval", "view"},
	} {
		if ValidGrant(g.Resource, g.Action) {
			t.Fatalf("vocabulary admits %s:%s, which is not canonical", g.Resource, g.Action)
		}
	}
	// The new resources are not data-scoped: scope stays request + payment.
	for _, res := range []string{"vendor", "vendor_bank", "reservation", "config"} {
		if ValidScope(res, "all") {
			t.Fatalf("resource %q must not be data-scoped", res)
		}
	}
	if want := 66; countGrants() != want {
		t.Fatalf("vocabulary holds %d (resource, action) pairs, want %d", countGrants(), want)
	}
}

func countGrants() int {
	total := 0
	for _, acts := range resourceActions {
		total += len(acts)
	}
	return total
}

// The Phase 0 stub matched "*" because Casbin's admin policy was a wildcard.
// Once roles are data a stale wildcard row would grant everything to everyone,
// which is precisely the failure mode this phase exists to remove: matching is
// exact, and AllGrants is how a caller asks for everything.
func TestNewPermissionSetMatchesExactlyAndAllGrantsIsTheWholeVocabulary(t *testing.T) {
	wildcard := NewPermissionSet([]Grant{{Resource: "*", Action: "*"}}, nil)
	if wildcard.Can("payment", "view") || wildcard.Scope("request") != "" {
		t.Fatalf("a wildcard grant still opens the permission set: %+v", wildcard)
	}
	if got := len(AllGrants()); got != countGrants() {
		t.Fatalf("AllGrants() = %d pairs, want the whole vocabulary (%d)", got, countGrants())
	}
	every := NewPermissionSet(AllGrants(), []ScopeGrant{{"request", "all"}})
	if !every.Can("role", "delete") || !every.Can("backup", "create") {
		t.Fatal("AllGrants() does not produce an all-powerful permission set")
	}
	if every.Scope("request") != "all" {
		t.Fatalf("scopes were dropped: request = %q", every.Scope("request"))
	}
	if every.Can("payment", "read") {
		t.Fatal("an exact-matching set admits a non-canonical action")
	}
}

func TestUpdateRolePermissionsPersistsAndValidates(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor := newRoleActor(t, s, ctx)
	id, err := s.CreateRole(ctx, actor, "Approvers", "")
	if err != nil {
		t.Fatal(err)
	}

	grants := []Grant{{"request", "view"}, {"request", "comment"}, {"approval", "approve"}}
	scopes := []ScopeGrant{{"request", "all"}}
	if err := s.UpdateRolePermissions(ctx, actor, id, grants, scopes); err != nil {
		t.Fatalf("UpdateRolePermissions: %v", err)
	}
	gotGrants, gotScopes, err := s.RolePermissions(ctx, id)
	if err != nil {
		t.Fatalf("RolePermissions: %v", err)
	}
	if len(gotGrants) != 3 || len(gotScopes) != 1 || gotScopes[0].Scope != "all" {
		t.Fatalf("persisted grants=%v scopes=%v", gotGrants, gotScopes)
	}

	// Replacing the set fully overwrites the previous grants (no accumulation).
	if err := s.UpdateRolePermissions(ctx, actor, id, []Grant{{"grid", "view"}}, nil); err != nil {
		t.Fatal(err)
	}
	gotGrants, gotScopes, _ = s.RolePermissions(ctx, id)
	if len(gotGrants) != 1 || gotGrants[0] != (Grant{"grid", "view"}) || len(gotScopes) != 0 {
		t.Fatalf("overwrite failed: grants=%v scopes=%v", gotGrants, gotScopes)
	}

	// Unknown resource/action and bad scope are rejected against the vocabulary.
	if err := s.UpdateRolePermissions(ctx, actor, id, []Grant{{"payment", "read"}}, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown action = %v, want %v", err, ErrValidation)
	}
	if err := s.UpdateRolePermissions(ctx, actor, id, nil, []ScopeGrant{{"budget", "all"}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unscoped resource scope = %v, want %v", err, ErrValidation)
	}
	if err := s.UpdateRolePermissions(ctx, actor, id, nil, []ScopeGrant{{"request", "everything"}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad scope value = %v, want %v", err, ErrValidation)
	}
}

func TestCopyRoleDuplicatesGrantsAndScopes(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor := newRoleActor(t, s, ctx)

	srcID, err := s.CreateRole(ctx, actor, "Template", "source")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRolePermissions(ctx, actor, srcID,
		[]Grant{{"request", "view"}, {"approval", "approve"}},
		[]ScopeGrant{{"request", "all"}}); err != nil {
		t.Fatal(err)
	}

	copyID, err := s.CopyRole(ctx, actor, srcID, "Template Copy")
	if err != nil {
		t.Fatalf("CopyRole: %v", err)
	}
	if copyID == srcID {
		t.Fatal("copy reused the source id")
	}
	grants, scopes, err := s.RolePermissions(ctx, copyID)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 2 || len(scopes) != 1 || scopes[0].Scope != "all" {
		t.Fatalf("copied grants=%v scopes=%v", grants, scopes)
	}
	copied, err := s.Role(ctx, copyID)
	if err != nil || copied.IsSystem {
		t.Fatalf("copied role = %+v, %v (must not be a system role)", copied, err)
	}

	if _, err := s.CopyRole(ctx, actor, srcID, "Template"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("copy into existing name = %v, want %v", err, ErrDuplicate)
	}
	if _, err := s.CopyRole(ctx, actor, 999999, "Ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("copy missing source = %v, want %v", err, ErrNotFound)
	}
}

func TestSetUserRolesReplacesAssignment(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor := newRoleActor(t, s, ctx)

	uid, err := s.CreateUser(ctx, "member@example.com", "Member", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	r1, _ := s.CreateRole(ctx, actor, "Alpha", "")
	r2, _ := s.CreateRole(ctx, actor, "Beta", "")

	if err := s.SetUserRoles(ctx, actor, uid, []int64{r1, r2}); err != nil {
		t.Fatalf("SetUserRoles: %v", err)
	}
	roles, err := s.UserRoles(ctx, uid)
	if err != nil || len(roles) != 2 {
		t.Fatalf("UserRoles = %+v, %v; want 2 roles", roles, err)
	}

	// Replacing the assignment fully overwrites the previous set.
	if err := s.SetUserRoles(ctx, actor, uid, []int64{r2}); err != nil {
		t.Fatal(err)
	}
	roles, _ = s.UserRoles(ctx, uid)
	if len(roles) != 1 || roles[0].ID != r2 {
		t.Fatalf("post-replace roles = %+v; want only Beta", roles)
	}

	// Unknown role ids are rejected as validation errors.
	if err := s.SetUserRoles(ctx, actor, uid, []int64{r2, 999999}); !errors.Is(err, ErrValidation) {
		t.Fatalf("assign unknown role = %v, want %v", err, ErrValidation)
	}
}

func TestEffectivePermissionsUnionAndBroadestScope(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor := newRoleActor(t, s, ctx)

	uid, err := s.CreateUser(ctx, "union@example.com", "Union User", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	// Role A: can view requests scoped to own; Role B: can approve, request scope all.
	roleA, _ := s.CreateRole(ctx, actor, "ViewOwn", "")
	if err := s.UpdateRolePermissions(ctx, actor, roleA, []Grant{{"request", "view"}}, []ScopeGrant{{"request", "own"}}); err != nil {
		t.Fatal(err)
	}
	roleB, _ := s.CreateRole(ctx, actor, "ApproveAll", "")
	if err := s.UpdateRolePermissions(ctx, actor, roleB, []Grant{{"approval", "approve"}}, []ScopeGrant{{"request", "all"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserRoles(ctx, actor, uid, []int64{roleA, roleB}); err != nil {
		t.Fatal(err)
	}

	ps, err := s.EffectivePermissions(ctx, uid)
	if err != nil {
		t.Fatalf("EffectivePermissions: %v", err)
	}
	if !ps.Can("request", "view") || !ps.Can("approval", "approve") {
		t.Fatalf("union grants missing: %+v", ps)
	}
	if ps.Can("payment", "create") {
		t.Fatal("granted a permission no role holds")
	}
	if got := ps.Scope("request"); got != "all" {
		t.Fatalf("effective request scope = %q, want broadest 'all'", got)
	}
	if got := ps.Scope("payment"); got != "" {
		t.Fatalf("unset scope = %q, want empty string", got)
	}
}

func TestSeedSystemRolesMatchDefaults(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	roles, err := s.AllRoles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Role{}
	for _, r := range roles {
		byName[r.Name] = r
	}
	for _, want := range []string{"Requester", "Manager", "Accounts", "Admin"} {
		r, ok := byName[want]
		if !ok {
			t.Fatalf("system role %q not seeded", want)
		}
		if !r.IsSystem {
			t.Fatalf("role %q must be is_system", want)
		}
	}

	// Admin has every resource+action in the vocabulary and scope 'all'.
	admin := byName["Admin"]
	grants, scopes, err := s.RolePermissions(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, acts := range resourceActions {
		total += len(acts)
	}
	if len(grants) != total {
		t.Fatalf("Admin grants = %d, want every action %d", len(grants), total)
	}
	scopeAll := 0
	for _, sc := range scopes {
		if sc.Scope == "all" {
			scopeAll++
		}
	}
	if scopeAll != 2 {
		t.Fatalf("Admin scopes = %+v, want request=all and payment=all", scopes)
	}

	// Requester default: request scope own, no approval permissions.
	req := byName["Requester"]
	rp, rs, _ := s.RolePermissions(ctx, req.ID)
	hasApprove := false
	for _, g := range rp {
		if g.Resource == "approval" {
			hasApprove = true
		}
	}
	if hasApprove {
		t.Fatal("Requester must not hold approval permissions")
	}
	if len(rs) != 1 || rs[0].Resource != "request" || rs[0].Scope != "own" {
		t.Fatalf("Requester scope = %+v, want request=own", rs)
	}
}

func TestDeleteRoleRejectsSystemRole(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor := newRoleActor(t, s, ctx)
	roles, _ := s.AllRoles(ctx)
	var adminID int64
	for _, r := range roles {
		if r.Name == "Admin" {
			adminID = r.ID
		}
	}
	if err := s.DeleteRole(ctx, actor, adminID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("delete system role = %v, want %v", err, ErrForbidden)
	}
}

func TestUpdateRoleRenamesCustomRolesAndProtectsSystemNames(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor := newRoleActor(t, s, ctx)

	id, err := s.CreateRole(ctx, actor, "Reviewer", "first")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRole(ctx, actor, id, "Senior reviewer", "second"); err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	role, err := s.Role(ctx, id)
	if err != nil || role.Name != "Senior reviewer" || role.Description != "second" {
		t.Fatalf("Role = %+v, %v", role, err)
	}
	if err := s.UpdateRole(ctx, actor, id, "   ", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank name = %v, want %v", err, ErrValidation)
	}

	// A system role's description is editable; its name is not — seedSystemRoles
	// and backfillUserRoles both resolve system roles by name, so a rename would
	// duplicate them on the next migration run.
	var adminID int64
	roles, _ := s.AllRoles(ctx)
	for _, r := range roles {
		if r.Name == "Admin" {
			adminID = r.ID
		}
	}
	if err := s.UpdateRole(ctx, actor, adminID, "Admin", "Full access, reworded"); err != nil {
		t.Fatalf("editing a system role description: %v", err)
	}
	if err := s.UpdateRole(ctx, actor, adminID, "Superuser", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("renaming a system role = %v, want %v", err, ErrForbidden)
	}
	if _, err := s.CreateRole(ctx, actor, "Auditor", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRole(ctx, actor, id, "auditor", ""); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("rename onto an existing name = %v, want %v", err, ErrDuplicate)
	}
}

func TestSetUserDefaultApproverRejectsSelfAndUnknownUsers(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor := newRoleActor(t, s, ctx)

	uid, err := s.CreateUser(ctx, "employee@example.com", "Employee", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	approverID, err := s.CreateUser(ctx, "approver@example.com", "Approver", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	inactiveID, err := s.CreateUser(ctx, "gone@example.com", "Gone", "hash", "data_entry", false)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetUserDefaultApprover(ctx, actor, uid, approverID); err != nil {
		t.Fatalf("SetUserDefaultApprover: %v", err)
	}
	u, err := s.UserByID(ctx, uid)
	if err != nil || u.DefaultApproverID != approverID {
		t.Fatalf("DefaultApproverID = %d, want %d (%v)", u.DefaultApproverID, approverID, err)
	}

	// Self-approval is impossible: a user can never be their own default.
	if err := s.SetUserDefaultApprover(ctx, actor, uid, uid); !errors.Is(err, ErrValidation) {
		t.Fatalf("self as default approver = %v, want %v", err, ErrValidation)
	}
	if err := s.SetUserDefaultApprover(ctx, actor, uid, 999999); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown approver = %v, want %v", err, ErrValidation)
	}
	if err := s.SetUserDefaultApprover(ctx, actor, uid, inactiveID); !errors.Is(err, ErrValidation) {
		t.Fatalf("inactive approver = %v, want %v", err, ErrValidation)
	}
	// None of the rejections may have moved the stored value.
	u, _ = s.UserByID(ctx, uid)
	if u.DefaultApproverID != approverID {
		t.Fatalf("a rejected update changed the stored approver to %d", u.DefaultApproverID)
	}
	// 0 clears it.
	if err := s.SetUserDefaultApprover(ctx, actor, uid, 0); err != nil {
		t.Fatalf("clearing the default approver: %v", err)
	}
	u, _ = s.UserByID(ctx, uid)
	if u.DefaultApproverID != 0 {
		t.Fatalf("DefaultApproverID = %d after clearing, want 0", u.DefaultApproverID)
	}
}

func TestCreateUserAssignsDefaultRole(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	adminID, err := s.CreateUser(ctx, "boss@example.com", "Boss", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := s.UserRoles(ctx, adminID)
	if err != nil || len(roles) != 1 || roles[0].Name != "Admin" {
		t.Fatalf("admin default roles = %+v, %v; want [Admin]", roles, err)
	}
	ps, _ := s.EffectivePermissions(ctx, adminID)
	if !ps.Can("role", "create") || !ps.Can("user", "edit") {
		t.Fatal("bootstrap-style admin lacks administrative permissions")
	}

	entryID, err := s.CreateUser(ctx, "clerk@example.com", "Clerk", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	roles, _ = s.UserRoles(ctx, entryID)
	if len(roles) != 1 || roles[0].Name != "Accounts" {
		t.Fatalf("data_entry default roles = %+v; want [Accounts]", roles)
	}
}

// F-G-022 — user_roles.role_id cascades, so deleting an assigned role stripped it
// from every holder in silence. F-G-023 — and the refusal has to say something
// true, which is what the app layer renders through friendly().
func TestDeleteRoleRefusesARoleSomebodyHolds(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor := newRoleActor(t, s, ctx)
	roleID, err := s.CreateRole(ctx, actor, "Held", "")
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	holder, err := s.CreateUser(ctx, "holder@example.com", "Holder", "hash", "data_entry", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.SetUserRoles(ctx, actor, holder, []int64{roleID}); err != nil {
		t.Fatalf("SetUserRoles: %v", err)
	}

	err = s.DeleteRole(ctx, actor, roleID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("DeleteRole on an assigned role = %v, want ErrForbidden", err)
	}
	if !strings.Contains(err.Error(), "assigned to 1 user") {
		t.Fatalf("the refusal does not name the holders: %v", err)
	}
	if _, err := s.Role(ctx, roleID); err != nil {
		t.Fatalf("the refused delete removed the role: %v", err)
	}
	// The grant the holder had is still theirs.
	roles, err := s.UserRoles(ctx, holder)
	if err != nil || len(roles) != 1 {
		t.Fatalf("UserRoles after a refused delete = %+v, %v", roles, err)
	}

	// Released, the same role deletes as before — nothing was locked shut.
	if err := s.SetUserRoles(ctx, actor, holder, nil); err != nil {
		t.Fatalf("SetUserRoles(nil): %v", err)
	}
	if err := s.DeleteRole(ctx, actor, roleID); err != nil {
		t.Fatalf("DeleteRole on an unheld role: %v", err)
	}
	if _, err := s.Role(ctx, roleID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Role after delete = %v, want ErrNotFound", err)
	}
}
