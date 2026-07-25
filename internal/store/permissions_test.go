package store

import (
	"context"
	"errors"
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
	// …and nothing beyond them: the lists are exact, not a prefix.
	for _, g := range []Grant{
		{"vendor", "delete"}, {"vendor_bank", "create"},
		{"reservation", "view"}, {"config", "create"},
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
	if want := 64; countGrants() != want {
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
