# Phase 1 — RBAC Foundation · Spec

**Parent:** `2026-07-25-payment-requests-overview.md`. **Covers matrix IDs:** R1–R9, C1, plus the scope infrastructure for Q5/D2 (consumed in P2).

**Goal:** Replace the two hard-coded Casbin roles with an admin-managed, DB-driven, per-screen permission system: multiple roles per user (access = union), a data scope (own/assigned/all) per scoped resource, server-side enforcement, and a Roles matrix admin UI with create-by-copy and delete.

## 1. Migration runner (C1)
Add `internal/store/migrations.go`: ordered `[]migration{Version int, Name string, Up func(*sql.Tx) error}`. `store.Open` calls `migrate(db)` after `db.Exec(schemaSQL)`. `migrate` reads `PRAGMA user_version`, runs each pending migration's `Up` in a transaction, then `PRAGMA user_version = Version`. Helper `columnExists(tx,table,col) (bool,error)` guards column adds. Baseline `schemaSQL` unchanged (== user_version 0). Phase 1 registers migration v1 (permission tables + seed).

## 2. Schema (migration v1)
Tables `roles`, `role_permissions`, `role_data_scope`, `user_roles` exactly as overview §3 (P1). Seed the four system roles (`is_system=1`) with the default grants/scopes in overview §5, and populate `user_roles` from existing `users.role` (admin→Admin role; data_entry→a role granting the legacy payment/report/grid actions, mapped to the new `Accounts` starter for parity). `users.role` retained, non-authoritative.

## 3. Store API (types + methods)
```go
type Role struct { ID int64; Name, Description string; IsSystem bool; CreatedAt, UpdatedAt time.Time }
type Grant struct { Resource, Action string }
type ScopeGrant struct { Resource, Scope string } // Scope in {own,assigned,all}
type PermissionSet struct { /* internal maps */ }
func (p PermissionSet) Can(resource, action string) bool
func (p PermissionSet) Scope(resource string) string // "" if resource not scoped/absent
```
Methods on `*Store` (all audited where they mutate):
`AllRoles(ctx)`, `Role(ctx,id)`, `RolePermissions(ctx,id) ([]Grant,[]ScopeGrant,error)`, `CreateRole(ctx,actor,name,desc)`, `CopyRole(ctx,actor,srcID,name)`, `UpdateRolePermissions(ctx,actor,roleID,[]Grant,[]ScopeGrant)`, `DeleteRole(ctx,actor,id)` (rejects `ErrForbidden`/`ErrValidation` on system roles), `SetUserRoles(ctx,actor,userID,[]int64)`, `UserRoles(ctx,userID)`, `EffectivePermissions(ctx,userID) (PermissionSet,error)`.

Validation: role name required + unique (case-insensitive, `ErrDuplicate`); cannot delete/rename `is_system`; scope value must be one of the three; unknown resource/action rejected against the canonical vocabulary (a package-level allow-list mirroring overview §5).

## 4. Auth engine
`auth.Manager` drops the static Casbin policy list. New:
- `func (m *Manager) permsFor(u store.User) (store.PermissionSet, error)` — loads `EffectivePermissions` (short TTL cache keyed by user id acceptable; correctness first — may query per request).
- `func (m *Manager) Can(u store.User, resource, action string) bool`
- `func (m *Manager) Scope(u store.User, resource string) string`
- `RequirePermission(resource, action string, next)` uses `Can`; on deny → 403 via existing error handler.
Admin bootstrap (`EnsureUser` admin) must resolve to the Admin role so the first login can administer.

## 5. Data-scope enforcement
Provide `PermissionSet.Scope(resource)` and expose it to handlers via `auth.Scope`. P1 delivers the mechanism + tests proving: (a) admin-only routes (`/roles`, `/users`) 403 for non-permitted roles; (b) `Scope` resolves union correctly (all>assigned>own). Actual row-filtering by scope lands with the `request`/`payment` list queries in P2/P3, which must call `auth.Scope` and pass it into `ListRequests`/`ListPayments`.

## 6. UI / routes
- `GET /roles` — list roles (count of users), matrix editor for the selected role: rows = resources, columns = actions (from vocabulary), checkboxes; scoped resources show a scope selector (own/assigned/all). `POST /roles` upsert grants+scope. `POST /roles/new` (create), `POST /roles/{id}/copy`, `POST /roles/{id}/delete`.
- `GET /users` (existing) extended: multi-role checkboxes per user; `POST /users` persists `SetUserRoles`.
- Nav becomes permission-driven: each link shown only if `Can(view)` for its resource; Settings group requires `role`/`user`/etc. `view`.
- All new routes behind `RequirePermission("role", …)` / `RequirePermission("user", …)`; POSTs behind `withCSRF`.

## 7. Acceptance criteria
- Admin creates a role, copies a role, edits any resource+action + data scope, deletes a non-system role (system roles rejected), assigns ≥2 roles to a user; union access verified.
- Integration test: a Requester-only session gets 403 on `/roles`, `/users`, and any admin POST, by URL (proves R6 server-side).
- `EffectivePermissions` union + broadest-scope verified by unit test.
- Migration idempotent: opening an already-migrated DB is a no-op; opening a legacy DB seeds roles and back-fills `user_roles`.
- `make test-race` and `make test-cover` green.

## 8. Test plan (TDD)
Store unit (`internal/store/permissions_test.go`, `migrations_test.go`), auth unit (`internal/auth/auth_test.go` additions), app integration (`internal/app/app_integration_test.go` additions), optional e2e (`tests/e2e/permissions.spec.ts`). Each behaviour is a red→green→commit cycle in the plan.
