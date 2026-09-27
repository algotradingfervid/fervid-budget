package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"fervidbudget/internal/store"
)

// countLoads wraps the manager's database read so a test can see how many times
// the permission tables were actually queried, and with which context.
func countLoads(manager *Manager) (*atomic.Int64, *atomic.Value) {
	var loads atomic.Int64
	var lastCtx atomic.Value
	inner := manager.loadPerms
	manager.loadPerms = func(ctx context.Context, userID int64) (store.PermissionSet, error) {
		loads.Add(1)
		lastCtx.Store(ctxHolder{ctx})
		return inner(ctx, userID)
	}
	return &loads, &lastCtx
}

type ctxMarker struct{}

// ctxHolder gives atomic.Value one concrete type to hold.
type ctxHolder struct{ ctx context.Context }

// serve runs one request through the session middleware and hands the handler
// the request exactly as the application's handlers see it.
func serve(t *testing.T, manager *Manager, user store.User, handler func(r *http.Request)) {
	t.Helper()
	request := authenticatedRequest(t, manager, user, http.MethodGet, "/")
	request = request.WithContext(context.WithValue(request.Context(), ctxMarker{}, "request"))
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(r)
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(httptest.NewRecorder(), request)
}

func TestPermissionLookupsAreCachedForOneRequest(t *testing.T) {
	manager, st := newTestManager(t)
	admin := createTestUser(t, st, "cache-admin@example.test", "admin", true)
	entry := createTestUser(t, st, "cache-entry@example.test", "data_entry", true)
	loads, lastCtx := countLoads(manager)

	serve(t, manager, admin, func(r *http.Request) {
		current := CurrentUser(r)
		for range 5 {
			if !manager.Can(current, "role", "edit") {
				t.Fatal("admin should edit roles")
			}
		}
		if got := manager.Scope(current, "payment"); got != store.ScopeAll {
			t.Fatalf("admin payment scope = %q", got)
		}
		if !manager.Permissions(current).Can("user", "edit") {
			t.Fatal("admin permission set should allow user:edit")
		}
		if got := loads.Load(); got != 1 {
			t.Fatalf("seven lookups for the signed-in user ran %d database reads, want 1", got)
		}
		if held, _ := lastCtx.Load().(ctxHolder); held.ctx == nil || held.ctx.Value(ctxMarker{}) != "request" {
			t.Fatal("the permission query did not run on the request's context")
		}

		// Asking about somebody else (a user loaded from the database, as the
		// reservation screens do) must answer with their own permissions, never
		// the signed-in user's cached set.
		if manager.Can(entry, "role", "view") {
			t.Fatal("accounts user answered with the admin's permissions")
		}
		if !manager.Can(entry, "payment", "create") {
			t.Fatal("accounts user should create payments")
		}
		if got := loads.Load(); got != 3 {
			t.Fatalf("two lookups for another user ran %d reads in total, want 3 (never cached)", got)
		}
		if !manager.Can(current, "role", "edit") {
			t.Fatal("signed-in answer changed after asking about another user")
		}
		if got := loads.Load(); got != 3 {
			t.Fatalf("returning to the signed-in user re-read the database: %d reads", got)
		}
	})
}

func TestPermissionChangeAppliesOnTheNextRequest(t *testing.T) {
	manager, st := newTestManager(t)
	admin := createTestUser(t, st, "fresh-admin@example.test", "admin", true)
	entry := createTestUser(t, st, "fresh-entry@example.test", "data_entry", true)
	loads, _ := countLoads(manager)

	serve(t, manager, entry, func(r *http.Request) {
		if manager.Can(CurrentUser(r), "role", "view") {
			t.Fatal("accounts must not view roles before the grant")
		}
	})

	roles, err := st.AllRoles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var accountsID int64
	for _, role := range roles {
		if role.Name == "Accounts" {
			accountsID = role.ID
		}
	}
	if accountsID == 0 {
		t.Fatal("seeded Accounts role not found")
	}
	grants, scopes, err := st.RolePermissions(t.Context(), accountsID)
	if err != nil {
		t.Fatal(err)
	}
	grants = append(grants, store.Grant{Resource: "role", Action: "view"})
	if err := st.UpdateRolePermissions(t.Context(), admin, accountsID, grants, scopes); err != nil {
		t.Fatal(err)
	}

	before := loads.Load()
	serve(t, manager, entry, func(r *http.Request) {
		if !manager.Can(CurrentUser(r), "role", "view") {
			t.Fatal("the new grant did not apply on the next request")
		}
	})
	if got := loads.Load() - before; got != 1 {
		t.Fatalf("the next request ran %d reads, want one fresh read", got)
	}
}

func TestPermissionCacheIgnoredOnceTheRequestIsOver(t *testing.T) {
	manager, st := newTestManager(t)
	admin := createTestUser(t, st, "late-admin@example.test", "admin", true)
	loads, _ := countLoads(manager)

	ctx, cancel := context.WithCancel(context.Background())
	escaped := admin.WithRequestCache(newPermissionCache(ctx))
	manager.Can(escaped, "role", "edit")
	cancel()
	// A user value that outlives its request reads fresh, on a live context,
	// instead of failing on the finished request's cancelled one.
	if !manager.Can(escaped, "role", "edit") {
		t.Fatal("a user value that outlived its request was denied")
	}
	if got := loads.Load(); got != 2 {
		t.Fatalf("reads = %d, want 2", got)
	}
}
