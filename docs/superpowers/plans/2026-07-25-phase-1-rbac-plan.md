# Phase 1 — RBAC Foundation · TDD Implementation Plan

**Parent spec:** `docs/superpowers/specs/2026-07-25-phase-1-rbac-spec.md`
**Overview:** `docs/superpowers/specs/2026-07-25-payment-requests-overview.md`
**Covers matrix IDs:** R1–R9, C1, G8 (foundation), G9 (+ the `own/assigned/all` scope mechanism consumed by Q5/D2 in P2).

---

## Amendment log

Amended 2026-07-25 against `docs/superpowers/specs/2026-07-25-design-system-adoption-spec.md` — §4 "Phase 1" and decisions **D2**, **D4**, **D5**, plus gaps **G8**/**G9**. Everything not listed below is the plan as originally written and audited. **Task count after this amendment: 13.**

| # | Change | Source |
|---|---|---|
| A1 | **Tasks 1 and 2 are already implemented and committed** — `ff76441` (migration runner + v1 `roles`) and `3abd750` (v1 `role_permissions`, `role_data_scope`, `user_roles`). Their bodies are preserved verbatim as the record of what shipped. Do not re-execute them. | repo history |
| A2 | Migration numbering: **Phase 1 still owns exactly one migration, v1 — unchanged.** What shifted is everything after it: v2 is now Phase 1V (`vendors`), v3 Phase 2, v4 Phase 3, v5 Phase 4, v6 Phase 5. | spec §3, D3 |
| A3 | Task 3 `resourceActions` gains `vendor`{view, create, edit}, `vendor_bank`{view, edit}, `reservation`{reserve, release, reassign}, `config`{view, edit}. `resourceOrder` and the vocabulary table in Global Constraints grow with them, and `TestPermissionVocabularyIsCanonical` asserts every new pair. | D2 |
| A4 | **New Task 10** — `internal/app/permmap.go`: the 9 page-row × 7 action-column presentation map over the canonical vocabulary, with `expandCells`, `buildPermMatrix` and a unit test proving every canonical `(resource, action)` pair is reachable through exactly one cell and appears in exactly one row's **Advanced** list. Enforcement is untouched: `role_permissions` stays one row per `(resource, action)` and the permission engine does not change. | D2 |
| A5 | Task 10 → **Task 11**, rebuilt against `mockups/screens/admin-roles.html`: `.segmented` role switcher with `.n` "custom" badges, name/description in a `.card`, two matrices from one dataset (desktop `table.perm-table`, mobile `.perm-acc`), `.perm-scope` 3-state pill group with an explicit `None`, sticky `.action-bar`. The `.split` + `<aside class="card">` role list and the scope `<select>` are gone. | §4 Phase 1, D2 |
| A6 | Task 11 → **Task 12**, rebuilt against `mockups/screens/admin-users.html`: `table.t-cards` with `data-label` on every cell, `.t-lead` on the name, `.pill.neutral.no-dot` role chips, editing inside an `.overlay > .sheet`. Adds `users.default_approver_id` (**G9**) with a store rule that a user can never be their own default approver (**G8** foundation; Phase 2 consumes the column to pre-select the approver). | §4 Phase 1, G8, G9 |
| A7 | Task 12 → **Task 13**, cut down. Phase 0 owns the app shell, the `NavGroup`/`NavItem`/`TabBar` model, `buildShell`/`resolveTabs` and `PageData.Shell`/`.Perms`; Task 13 no longer invents any of it. It now only (a) swaps the Phase 0 stub `PermissionSet` for the DB-backed one and re-points every nav/badge gate onto the canonical vocabulary, and (b) keeps the server-side URL-enforcement tests. | §4 Phase 1, D4 |
| A8 | **Interface ownership.** `internal/store/permissions.go` already exists: Phase 0 Task 12 created it with the `PermissionSet` **interface** (`Can(resource, action) bool`, `Scope(resource) string`), the `Grant` struct, `const ScopeAll` and the temporary Casbin-backed `staticPermissionSet`/`NewPermissionSet`. Phase 1 Tasks 3 and 7 **append to that file and implement that same interface** — they never redeclare `PermissionSet` or `Grant`, and Task 7 returns the interface, not a competing struct. | D2, Phase 0 plan §Self-review |
| A9 | `.badge` is retired globally by Phase 0 (**D5**): every template this phase touches emits `.pill`, and the Task 11 render test fails if `class="badge` reappears. Responsiveness is media-query driven (**D4**) — no `data-device` attribute anywhere. | D4, D5 |
| A10 | Task 3 `resourceActions` gains `request`{cancel} and `approval`{cancel} — the two pairs Phase 2's cancellation routes are gated on. The vocabulary table in Global Constraints and `TestPermissionVocabularyIsCanonical` grow with them; the pair total goes **64 → 66**. Resource count is unchanged at 21. | Phase 2 plan (`6220ab2`) |

**Deliberately not amended.** The seeded system-role grants in Task 8 keep exactly the actions they had. Phase 1V grants Accounts the new `vendor`/`vendor_bank` verbs and Phase 3 the `reservation` verbs, each in the phase that builds the screens behind them; seeding them here would hand out access to routes that do not exist yet. Admin still receives every action automatically, because `adminGrants()` iterates `resourceOrder` — the four new resources are included the moment Task 3 lands.

**Gotcha carried over from A1/A2.** The runner applies each version exactly once. Any database already stamped `PRAGMA user_version = 1` by `ff76441`/`3abd750` will **not** pick up the rest of v1 (Task 8's seeding and back-fill, Task 12's `default_approver_id`). Before running the remaining tasks against an existing database, re-stamp it (`PRAGMA user_version = 0`) or recreate it — the whole of v1 is written to be idempotent (`CREATE TABLE IF NOT EXISTS`, upsert-by-name seeding, `columnExists`-guarded column adds) precisely so that is safe. Do **not** split the additions into a v2: that number belongs to Phase 1V.

---

## Goal

Replace the two hard-coded Casbin roles (`admin`, `data_entry`) with an admin-managed, DB-driven, per-screen permission system: multiple roles per user (effective access = union), a data scope (`own`/`assigned`/`all`) per scoped resource, strictly server-side enforcement on routes and data, a Roles matrix admin UI with create-by-copy and delete, and multi-role user assignment. Ship the versioned migration runner that every later phase builds on.

## Architecture

- **Migration runner** (`internal/store/migrations.go`): an ordered `[]migration{Version,Name,Up}` slice driven by `PRAGMA user_version`. `store.Open` execs the unchanged v0 baseline `schemaSQL`, then calls `migrate(db)`. Phase 1 owns **exactly one** migration, **v1** (permission tables + seed 4 system roles + back-fill `user_roles`, all in a single `Up`). Later phases append v2–v6 on the same global monotonic sequence — v2 is Phase 1V's `vendors` (amendment A2). Within Phase 1 the single v1 is built incrementally across tasks: Task 1 opens v1 with the `roles` table (so the runner has a real first migration), Task 2 completes the permission tables, Task 8 folds the seeding + back-fill into the same v1 `Up`, and Task 12 adds the guarded `users.default_approver_id` column to it.
- **Permission model** (`internal/store/permissions.go` — the file already exists, created by Phase 0 Task 12): tables `roles`, `role_permissions`, `role_data_scope`, `user_roles`; a package-level canonical vocabulary (`resourceActions`, `scopedResources`, `resourceOrder`) mirroring overview §5 as amended by D2; new types `Role`, `ScopeGrant` and the concrete `dbPermissionSet`, which **implements the `PermissionSet` interface Phase 0 declared** (`Can`, `Scope`) — `PermissionSet` and `Grant` are never redeclared here; CRUD + assignment methods; `EffectivePermissions` computes the union of a user's roles' grants and the broadest scope per resource and returns it as a `PermissionSet`.
- **Auth engine** (`internal/auth/auth.go`): the static Casbin enforcer is removed. `Manager.Can`/`Permissions` already exist (Phase 0 Task 12, Casbin-backed); this phase replaces their bodies so they, plus a new `Scope`/`permsFor`, delegate to `store.EffectivePermissions`. `RequirePermission(resource, action, next)` keeps its signature but consults the DB engine.
- **Presentation map** (`internal/app/permmap.go`): the 9 page rows × 7 action columns the approved roles screen draws, each cell a *set* of canonical grants, plus the per-row **Advanced** enumeration that preserves fine-grained control (R2). Presentation only — enforcement never consults it (D2).
- **Handlers/templates** (`internal/app`): new Roles admin screen (`.segmented` switcher + desktop `perm-table` / mobile `perm-acc` matrices, create/copy/delete in sheets), users screen rebuilt as `table.t-cards` with role assignment and the default approver inside an `.overlay > .sheet`. All existing route action strings are migrated from the old `read/write/update` verbs to the canonical §5 vocabulary (`view/create/edit/...`), and so are the nav and badge gates Phase 0 wrote against the interim Casbin verbs. The shell itself is Phase 0's — this phase populates it, it does not build it.
- **Legacy bridge:** `users.role` is retained but non-authoritative. `CreateUser` now writes a default `user_roles` row (admin→Admin, else→Accounts) so every user — including the bootstrap admin and users created in tests/e2e — resolves to a concrete permission set. `EffectivePermissions` is a pure union over `user_roles`.

## Tech Stack

- Go 1.25 (`go.mod` module `fervidbudget`); `net/http` method+pattern routing (Go 1.22+); `html/template` server rendering.
- `modernc.org/sqlite` (pure-Go driver); SQLite `PRAGMA user_version` migrations.
- Auth: bespoke HMAC session cookies + CSRF (unchanged); **Casbin removed** from the auth path (the `github.com/casbin/casbin/v2` dependency may remain in `go.mod`; do not run `go mod tidy` as part of this phase).
- Tests: Go `testing` + `net/http/httptest`; Playwright (`tests/e2e`) for browser e2e. `make test`, `make test-race`, `make test-cover`, `make test-e2e`.

## Global Constraints

Copied verbatim from overview §7 (all phases obey):

- **Store methods:** `func (s *Store) X(ctx, actor User, …)`; mutations use `s.db.BeginTx(ctx,nil)` + `defer tx.Rollback()` + `tx.Commit()`; every mutation writes `recordAuditTx(ctx, tx, AuditInput{…, Before, After})`.
- **Errors:** reuse `ErrNotFound, ErrForbidden, ErrValidation, ErrDuplicate, ErrLockedMonth, ErrInactiveHead`; wrap validation as `fmt.Errorf("%w: message", ErrValidation)`; map DB errors via `classify`.
- **Handlers:** register in `App.routes` behind `a.auth.RequirePermission(resource, action, …)`; POST handlers wrapped with `a.withCSRF`; render via templates in `internal/app/templates.go`; user-facing errors via `a.respondError`/`friendly`.
- **Permission gate signature (Phase 1):** `func (m *Manager) RequirePermission(resource, action string, next http.Handler) http.Handler` (unchanged shape; new engine behind it). New helper `func (m *Manager) Can(u store.User, resource, action string) bool` and `func (m *Manager) Scope(u store.User, resource string) string`.
- **Templates:** server-rendered HTML in the existing design system (`web/static/fervid-ds.css`); nav is role-aware (driven by `Can`); request form is mobile-first.
- **Testing (TDD, required):** Go unit tests via `newTestStore(t)`; HTTP/integration via `newAppTestServer(t)` (`httptest`); browser e2e in `tests/e2e/*.spec.ts` using the Playwright fixtures. Every task is red→green→commit. Run `make test-race` and `make test-cover`; e2e via `make test-e2e`.
- **Clock injection:** reminder/settlement timing logic takes an injected `now func() time.Time` (or `time.Time`) so tests are deterministic — never call `time.Now()` inside tested branches directly. *(No timing logic in Phase 1; noted for continuity.)*
- **Commits:** one per task, conventional messages.

**Canonical permission vocabulary (overview §5, extended by adoption-spec D2) — single source of truth for this phase.** 21 resources, 66 `(resource, action)` pairs. The four resources marked ★ are the D2 additions; they are declared here so the roles matrix and Phases 1V–5 have a stable vocabulary from day one, even though the screens behind them land later. The two actions marked † are the Phase 2 cancellation-flow additions, declared here for the same reason: the routes behind them are gated on a vocabulary this phase owns.

| Resource | Actions |
|---|---|
| `request` | view, create, edit, withdraw, reraise, comment, † cancel |
| `approval` | approve, reject, return, reassign, accept_partial, † cancel |
| `payment` | view, create, edit, void, process, settle, mark_partial, hold |
| ★ `reservation` | reserve, release, reassign |
| `attachment` | view, create |
| ★ `vendor` | view, create, edit |
| ★ `vendor_bank` | view, edit |
| `project` | view, create, edit |
| `head` | view, create, edit |
| `budget` | view, edit |
| `month` | view, create, lock |
| `grid` | view, export |
| `report` | view, export |
| `recoverable_category` | view, create, edit, delete |
| `recoverable_report` | view, export |
| `user` | view, create, edit |
| `role` | view, create, edit, delete |
| `notification` | view, edit |
| ★ `config` | view, edit |
| `audit` | view |
| `backup` | view, create |

Scoped resources: `request`, `payment` — scope ∈ `own | assigned | all`; effective scope = broadest across the user's roles (`all` > `assigned` > `own`).

**The matrix is presentation, the vocabulary is enforcement.** The approved roles screen draws 9 page rows × 7 fixed action columns; this table is 21 resources with ragged action lists. `internal/app/permmap.go` (Task 10) is the only bridge between them, and its unit test is what guarantees the bridge is total and unambiguous. Nothing in `internal/store` or `internal/auth` ever imports it.

Every commit message ends with:

```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
```

---

## Task 1 — Migration runner (C1)

> **Status: DONE** — shipped as commit `ff76441`. Body kept verbatim as the record of what was built; do not re-execute (amendment A1).

**Files**
- Create: `internal/store/migrations.go`
- Modify: `internal/store/store.go` (wire `migrate` into `Open`)
- Test: `internal/store/migrations_test.go`

**Interfaces**
- Produces: `type migration struct { Version int; Name string; Up func(*sql.Tx) error }`; `var migrations []migration`; `func migrate(db *sql.DB) error`; `func columnExists(tx *sql.Tx, table, col string) (bool, error)`.
- Consumes: `schemaSQL` (unchanged v0 baseline), `database/sql`.

### Step 1 — write the failing test

Add to `internal/store/migrations_test.go`:

```go
package store

import (
	"database/sql"
	"testing"
)

func TestMigrateAppliesAndIsIdempotent(t *testing.T) {
	s := newTestStore(t) // Open already runs migrate once

	var version int
	if err := s.DB().QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if want := migrations[len(migrations)-1].Version; version != want {
		t.Fatalf("user_version = %d, want %d (latest migration)", version, want)
	}

	// Re-running migrate against an already-migrated DB is a no-op and must not error.
	if err := migrate(s.DB()); err != nil {
		t.Fatalf("re-running migrate: %v", err)
	}
}

func TestColumnExistsReportsSchemaShape(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	got, err := columnExists(tx, "users", "role")
	if err != nil || !got {
		t.Fatalf("columnExists(users, role) = %v, %v; want true, nil", got, err)
	}
	got, err = columnExists(tx, "users", "does_not_exist")
	if err != nil || got {
		t.Fatalf("columnExists(users, does_not_exist) = %v, %v; want false, nil", got, err)
	}
}

var _ = sql.ErrNoRows // keep database/sql imported for later tests in this file
```

### Step 2 — run it (expect failure)

```
go test ./internal/store/ -run 'TestMigrate|TestColumnExists' -v
```

Expected: compile failure — `undefined: migrations`, `undefined: migrate`, `undefined: columnExists`.

### Step 3 — minimal implementation

Create `internal/store/migrations.go`:

```go
package store

import (
	"database/sql"
	"fmt"
)

type migration struct {
	Version int
	Name    string
	Up      func(*sql.Tx) error
}

// migrations is ordered and append-only. Versions are contiguous and never
// reordered; each phase appends its own. v0 is the baseline schemaSQL. Phase 1
// owns exactly one migration, v1: this task opens it with the roles table so the
// runner has a real first migration to exercise; Task 2 adds the remaining
// permission tables and Task 8 folds seeding + back-fill into the same v1 Up.
var migrations = []migration{
	{
		Version: 1,
		Name:    "permission schema",
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS roles (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  is_system INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_name_nocase ON roles(lower(name));
`)
			return err
		},
	},
}

// migrate applies every registered migration whose Version is greater than the
// database's current PRAGMA user_version, each inside its own transaction, then
// stamps user_version. Re-running against a migrated DB is a no-op.
func migrate(db *sql.DB) error {
	var current int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if err := m.Up(tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d (%s): %w", m.Version, m.Name, err)
		}
		// PRAGMA cannot be parameterized; the version is a trusted integer literal.
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, m.Version)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("stamp user_version %d: %w", m.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// columnExists guards additive column migrations so a re-run is safe even if
// user_version is out of sync with the physical schema.
func columnExists(tx *sql.Tx, table, col string) (bool, error) {
	rows, err := tx.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
}
```

Wire into `Open` in `internal/store/store.go` — after the existing `schemaSQL` exec:

```go
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
```

### Step 4 — run to pass

```
go test ./internal/store/ -run 'TestMigrate|TestColumnExists' -v
```

Expected: PASS. `TestMigrateAppliesAndIsIdempotent` asserts `user_version == migrations[len(migrations)-1].Version`, i.e. `1`, because Task 1 already registers v1 (the `roles` table). The `migrations` slice is non-empty from the outset, so the generic `migrations[len-1].Version` form is correct as written and needs no later revision — no empty-slice guard or temporary `assert 0` hack.

### Step 5 — commit

```
git add internal/store/migrations.go internal/store/migrations_test.go internal/store/store.go
git commit -m "feat(store): add PRAGMA user_version migration runner + open v1

Introduces an ordered migration slice, migrate(db), and a columnExists
guard, wired into store.Open after the v0 baseline schema. Registers the
single Phase 1 migration v1, opening it with the roles table and its
case-insensitive unique index. Idempotent by design so re-opening a
migrated database is a no-op.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2 — Migration v1: complete the permission schema (R1–R9)

> **Status: DONE** — shipped as commit `3abd750`. Body kept verbatim; do not re-execute (amendment A1). Note the runner has therefore already stamped `user_version = 1` on every existing database: see the gotcha in the amendment log before running Tasks 8 and 12.

**Files**
- Modify: `internal/store/migrations.go` (extend the v1 `Up` opened in Task 1)
- Test: `internal/store/migrations_test.go`

**Interfaces**
- Produces: tables `role_permissions`, `role_data_scope`, `user_roles` added to migration v1 (`roles` was created by Task 1). No new migration version — all four permission tables live in the single v1 (overview §3 P1).
- Consumes: the Task 1 runner and its v1 `roles` table.

### Step 1 — write the failing test

Append to `internal/store/migrations_test.go`:

```go
func TestMigrationV1CreatesPermissionTables(t *testing.T) {
	s := newTestStore(t)
	for _, table := range []string{"roles", "role_permissions", "role_data_scope", "user_roles"} {
		var name string
		if err := s.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name); err != nil {
			t.Fatalf("table %q missing after migrate: %v", table, err)
		}
	}
	// Case-insensitive unique role name index exists.
	var idx string
	if err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_roles_name_nocase'`,
	).Scan(&idx); err != nil {
		t.Fatalf("idx_roles_name_nocase missing: %v", err)
	}
}
```

### Step 2 — run it (expect failure)

```
go test ./internal/store/ -run TestMigrationV1CreatesPermissionTables -v
```

Expected: FAIL — `table "role_permissions" missing after migrate` (Task 1 already created `roles` + the index; the three dependent tables are still absent).

### Step 3 — minimal implementation

Extend the v1 `Up` opened in Task 1 (do **not** add a new migration entry) so its single SQL exec also creates the three tables that reference `roles`/`users`. The v1 migration in `internal/store/migrations.go` now reads:

```go
var migrations = []migration{
	{
		Version: 1,
		Name:    "permission schema",
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS roles (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  is_system INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_name_nocase ON roles(lower(name));

CREATE TABLE IF NOT EXISTS role_permissions (
  role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  resource TEXT NOT NULL,
  action TEXT NOT NULL,
  PRIMARY KEY(role_id, resource, action)
);

CREATE TABLE IF NOT EXISTS role_data_scope (
  role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  resource TEXT NOT NULL,
  scope TEXT NOT NULL,
  PRIMARY KEY(role_id, resource)
);

CREATE TABLE IF NOT EXISTS user_roles (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  PRIMARY KEY(user_id, role_id)
);
`)
			return err
		},
	},
}
```

`TestMigrateAppliesAndIsIdempotent` already uses the generic `migrations[len(migrations)-1].Version` form and still asserts `1` (the version is unchanged — v1 simply creates more tables now), so no test revision is required here.

### Step 4 — run to pass

```
go test ./internal/store/ -run 'TestMigrationV1|TestMigrate' -v
```

Expected: PASS.

### Step 5 — commit

```
git add internal/store/migrations.go internal/store/migrations_test.go
git commit -m "feat(store): complete v1 permission schema (permissions/scope/user_roles)

Extends the single Phase 1 migration v1 to add role_permissions,
role_data_scope and user_roles alongside the roles table from Task 1
(overview §3 P1), with cascading foreign keys. No new migration version.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3 — Role types, canonical vocabulary, and role CRUD (R2, R8, R9)

> **Status: DONE** — shipped as commit `09971cf`; the vocabulary was extended to 66 pairs by `1bb7d05` (amendment A10). Body kept as the record of what was built.

**Files**
- Modify: `internal/store/permissions.go` — **the file already exists.** Phase 0 Task 12 created it with the `PermissionSet` interface (`Can(resource, action) bool`, `Scope(resource) string`), the `Grant` struct, `const ScopeAll = "all"` and the temporary Casbin-backed `staticPermissionSet`/`NewPermissionSet`. This task **appends** to it and must not redeclare `PermissionSet` or `Grant` (amendment A8). `staticPermissionSet`/`NewPermissionSet` stay until Task 13 deletes them.
- Test: `internal/store/permissions_test.go`

**Interfaces**
- Produces (types): `Role{ID,Name,Description string,IsSystem bool,CreatedAt,UpdatedAt time.Time}`, `ScopeGrant{Resource,Scope string}`, and the concrete `dbPermissionSet` (unexported maps) satisfying the existing `PermissionSet` interface; `func EmptyPermissions() PermissionSet` for the deny-all case, since an interface has no `PermissionSet{}` literal.
- Reuses unchanged from Phase 0: `type PermissionSet interface`, `type Grant struct{Resource, Action string}`, `const ScopeAll`.
- Produces (vocabulary): `var resourceActions map[string][]string`, `var resourceOrder []string`, `var scopedResources map[string]bool`; `func ValidGrant(resource, action string) bool`, `func ValidScope(resource, scope string) bool`.
- Produces (methods): `AllRoles(ctx) ([]Role, error)`, `Role(ctx, id int64) (Role, error)`, `CreateRole(ctx, actor User, name, description string) (int64, error)`, `DeleteRole(ctx, actor User, id int64) error`.
- Consumes: `classify`, `recordAuditTx`, `ErrValidation`, `ErrDuplicate`, `ErrForbidden`, `ErrNotFound`.

### Step 1 — write the failing test

Create `internal/store/permissions_test.go`:

```go
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
```

### Step 2 — run it (expect failure)

```
go test ./internal/store/ -run 'TestCreateRoleAndListAndDeleteCustomRole|TestPermissionVocabularyIsCanonical' -v
```

Expected: compile failure — `undefined: ValidGrant`, `undefined: CreateRole`, etc.

### Step 3 — minimal implementation

Append to the existing `internal/store/permissions.go` (Phase 0 created the file; its `package store` clause, `PermissionSet` interface, `Grant` struct, `ScopeAll` const and `staticPermissionSet`/`NewPermissionSet` stay exactly where they are). Add the imports the file does not yet have:

```go
import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

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
```

### Step 4 — run to pass

```
go test ./internal/store/ -run 'TestCreateRoleAndListAndDeleteCustomRole|TestPermissionVocabularyIsCanonical' -v
```

Expected: PASS. (System-role deletion protection is proven in Task 8, once system roles are seeded.)

### Step 5 — commit

```
git add internal/store/permissions.go internal/store/permissions_test.go
git commit -m "feat(store): role types, canonical vocabulary, and role CRUD

Adds Role/ScopeGrant and dbPermissionSet — which implements the existing
store.PermissionSet interface rather than replacing it — the overview §5
resource/action allow-list extended with vendor, vendor_bank, reservation
and config (adoption-spec D2), ValidGrant/ValidScope, and
AllRoles/Role/CreateRole/DeleteRole with case-insensitive name uniqueness
and audit.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4 — `RolePermissions` read + `UpdateRolePermissions` write with validation (R2, R3, R8)

> **Status: DONE** — shipped as commit `d21f280`. Body kept as the record of what was built.

**Files**
- Modify: `internal/store/permissions.go`
- Test: `internal/store/permissions_test.go`

**Interfaces**
- Produces: `RolePermissions(ctx, id int64) ([]Grant, []ScopeGrant, error)`, `UpdateRolePermissions(ctx, actor User, roleID int64, grants []Grant, scopes []ScopeGrant) error`.
- Consumes: `ValidGrant`, `ValidScope`, `Role`, `recordAuditTx`.

### Step 1 — write the failing test

Append to `internal/store/permissions_test.go`:

```go
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
```

### Step 2 — run it (expect failure)

```
go test ./internal/store/ -run TestUpdateRolePermissionsPersistsAndValidates -v
```

Expected: compile failure — `undefined: (*Store).RolePermissions`, `UpdateRolePermissions`.

### Step 3 — minimal implementation

Append to `internal/store/permissions.go`:

```go
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
```

### Step 4 — run to pass

```
go test ./internal/store/ -run TestUpdateRolePermissionsPersistsAndValidates -v
```

Expected: PASS.

### Step 5 — commit

```
git add internal/store/permissions.go internal/store/permissions_test.go
git commit -m "feat(store): read and replace role grants + data scopes

RolePermissions reads a role's grants and scopes; UpdateRolePermissions
atomically replaces both sets after validating every entry against the
canonical vocabulary and rejecting unknown or unscoped values.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5 — `CopyRole` (R5)

> **Status: DONE** — shipped as commit `b223e49`. Body kept as the record of what was built.

**Files**
- Modify: `internal/store/permissions.go`
- Test: `internal/store/permissions_test.go`

**Interfaces**
- Produces: `CopyRole(ctx, actor User, srcID int64, name string) (int64, error)`.
- Consumes: `RolePermissions`, `CreateRole`-style INSERT, `recordAuditTx`.

### Step 1 — write the failing test

Append to `internal/store/permissions_test.go`:

```go
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
```

### Step 2 — run it (expect failure)

```
go test ./internal/store/ -run TestCopyRoleDuplicatesGrantsAndScopes -v
```

Expected: compile failure — `undefined: (*Store).CopyRole`.

### Step 3 — minimal implementation

Append to `internal/store/permissions.go`:

```go
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
```

### Step 4 — run to pass

```
go test ./internal/store/ -run TestCopyRoleDuplicatesGrantsAndScopes -v
```

Expected: PASS.

### Step 5 — commit

```
git add internal/store/permissions.go internal/store/permissions_test.go
git commit -m "feat(store): create role by copying an existing role (R5)

CopyRole clones a source role's grants and scopes into a new non-system
role, rejecting duplicate names and missing sources.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 6 — `SetUserRoles` / `UserRoles` (R4 assignment)

**Files**
- Modify: `internal/store/permissions.go`
- Test: `internal/store/permissions_test.go`

**Interfaces**
- Produces: `SetUserRoles(ctx, actor User, userID int64, roleIDs []int64) error`, `UserRoles(ctx, userID int64) ([]Role, error)`.
- Consumes: `roles`/`user_roles`, `recordAuditTx`.

### Step 1 — write the failing test

Append to `internal/store/permissions_test.go`:

```go
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
```

### Step 2 — run it (expect failure)

```
go test ./internal/store/ -run TestSetUserRolesReplacesAssignment -v
```

Expected: compile failure — `undefined: (*Store).SetUserRoles`, `UserRoles`.

### Step 3 — minimal implementation

Append to `internal/store/permissions.go`:

```go
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
```

### Step 4 — run to pass

```
go test ./internal/store/ -run TestSetUserRolesReplacesAssignment -v
```

Expected: PASS.

### Step 5 — commit

```
git add internal/store/permissions.go internal/store/permissions_test.go
git commit -m "feat(store): assign and read multiple roles per user (R4)

SetUserRoles atomically replaces a user's role set after validating every
role id; UserRoles reads the current assignment.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 7 — `EffectivePermissions`: union of grants + broadest scope (R3, R4; scope mechanism for Q5/D2)

**Files**
- Modify: `internal/store/permissions.go`
- Test: `internal/store/permissions_test.go`

**Interfaces**
- Produces: `EffectivePermissions(ctx, userID int64) (PermissionSet, error)` — the return type is the `PermissionSet` **interface** Phase 0 declared (amendment A8); the value behind it is the `dbPermissionSet` from Task 3. Nothing outside this file names the concrete type, so Task 13 can delete the Phase 0 stub without touching a single caller.
- Consumes: `user_roles`, `role_permissions`, `role_data_scope`, `dbPermissionSet.add`/`mergeScope`.

### Step 1 — write the failing test

Append to `internal/store/permissions_test.go`:

```go
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
```

### Step 2 — run it (expect failure)

```
go test ./internal/store/ -run TestEffectivePermissionsUnionAndBroadestScope -v
```

Expected: compile failure — `undefined: (*Store).EffectivePermissions`.

### Step 3 — minimal implementation

Append to `internal/store/permissions.go`:

```go
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
```

### Step 4 — run to pass

```
go test ./internal/store/ -run TestEffectivePermissionsUnionAndBroadestScope -v
```

Expected: PASS.

### Step 5 — commit

```
git add internal/store/permissions.go internal/store/permissions_test.go
git commit -m "feat(store): resolve effective permissions as union + broadest scope

EffectivePermissions unions grants across a user's roles and keeps the
broadest data scope per scoped resource (all>assigned>own), the mechanism
P2/P3 row-filtering consumes.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 8 — Extend migration v1: seed 4 system roles + back-fill `user_roles`; CreateUser default role (R7, R8, R9)

**Files**
- Modify: `internal/store/migrations.go` (extend the v1 `Up` to seed + back-fill; add `seedSystemRoles`, `backfillUserRoles`, `systemRoleDefaults`)
- Modify: `internal/store/store.go` (`CreateUser` writes a default `user_roles` row)
- Test: `internal/store/permissions_test.go`, `internal/store/migrations_test.go`

**Interfaces**
- Produces: `func seedSystemRoles(tx *sql.Tx) error`, `func backfillUserRoles(tx *sql.Tx) error`, `var systemRoleDefaults []systemRoleDef`, `func assignDefaultRoleTx(ctx, tx, userID int64, legacyRole string) error`.
- Consumes: `resourceActions`, `resourceOrder`, `Grant`, `ScopeGrant`.

### Step 1 — write the failing test

Append to `internal/store/permissions_test.go`:

```go
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
```

Also append to `internal/store/migrations_test.go`:

```go
func TestMigrationBackfillsExistingUsers(t *testing.T) {
	// Simulate a legacy database: create the store, then insert a user row
	// directly WITHOUT user_roles (as if it predated the RBAC migration),
	// clear user_roles, reset user_version to 0 so the single v1 re-applies its
	// seed + back-fill (v1 is idempotent — tables use IF NOT EXISTS and
	// seedSystemRoles upserts by name), and re-run migrate.
	s := newTestStore(t)
	if _, err := s.DB().Exec(`INSERT INTO users(email,name,password_hash,role,active) VALUES('legacy@example.com','Legacy','hash','admin',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`DELETE FROM user_roles`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.DB()); err != nil {
		t.Fatalf("re-migrate legacy DB: %v", err)
	}
	var id int64
	if err := s.DB().QueryRow(`SELECT id FROM users WHERE email='legacy@example.com'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	roles, err := s.UserRoles(context.Background(), id)
	if err != nil || len(roles) != 1 || roles[0].Name != "Admin" {
		t.Fatalf("legacy admin back-fill = %+v, %v; want [Admin]", roles, err)
	}
}
```

*(Add `import "context"` to `migrations_test.go` if not already present.)*

### Step 2 — run it (expect failure)

```
go test ./internal/store/ -run 'TestSeedSystemRolesMatchDefaults|TestDeleteRoleRejectsSystemRole|TestCreateUserAssignsDefaultRole|TestMigrationBackfillsExistingUsers' -v
```

Expected: FAIL — no system roles seeded; `UserRoles` empty for new users; `DeleteRole` on Admin currently succeeds.

### Step 3 — minimal implementation

Append v2 + helpers to `internal/store/migrations.go`:

```go
type systemRoleDef struct {
	Name        string
	Description string
	Grants      []Grant
	Scopes      []ScopeGrant
}

// systemRoleDefaults are the seeded starter roles (overview §5). Admin holds
// every action in the canonical vocabulary.
var systemRoleDefaults = []systemRoleDef{
	{
		Name:        "Requester",
		Description: "Raise and manage your own payment requests.",
		Grants: []Grant{
			{"request", "view"}, {"request", "create"}, {"request", "edit"},
			{"request", "withdraw"}, {"request", "reraise"}, {"request", "comment"},
			{"attachment", "view"}, {"attachment", "create"},
		},
		Scopes: []ScopeGrant{{"request", "own"}},
	},
	{
		Name:        "Manager",
		Description: "Review and decide on payment requests.",
		Grants: []Grant{
			{"request", "view"}, {"request", "comment"},
			{"approval", "approve"}, {"approval", "reject"}, {"approval", "return"},
			{"approval", "reassign"}, {"approval", "accept_partial"},
			{"grid", "view"}, {"report", "view"},
		},
		Scopes: []ScopeGrant{{"request", "all"}},
	},
	{
		Name:        "Accounts",
		Description: "Process approved requests and record payments.",
		Grants: []Grant{
			{"request", "view"}, {"request", "comment"},
			{"payment", "view"}, {"payment", "create"}, {"payment", "edit"}, {"payment", "void"},
			{"payment", "process"}, {"payment", "settle"}, {"payment", "mark_partial"}, {"payment", "hold"},
			{"attachment", "view"}, {"attachment", "create"},
			{"grid", "view"}, {"report", "view"}, {"report", "export"},
			{"recoverable_report", "view"}, {"recoverable_report", "export"},
		},
		Scopes: []ScopeGrant{{"request", "all"}, {"payment", "all"}},
	},
	{
		Name:        "Admin",
		Description: "Full administrative access.",
		Grants:      adminGrants(),
		Scopes:      []ScopeGrant{{"request", "all"}, {"payment", "all"}},
	},
}

func adminGrants() []Grant {
	var out []Grant
	for _, res := range resourceOrder {
		for _, act := range resourceActions[res] {
			out = append(out, Grant{res, act})
		}
	}
	return out
}

// seedSystemRoles is idempotent: it upserts each system role by name and resets
// its grants and scopes to the canonical defaults.
func seedSystemRoles(tx *sql.Tx) error {
	for _, def := range systemRoleDefaults {
		var roleID int64
		err := tx.QueryRow(`SELECT id FROM roles WHERE lower(name)=lower(?)`, def.Name).Scan(&roleID)
		switch err {
		case sql.ErrNoRows:
			res, insErr := tx.Exec(`INSERT INTO roles(name,description,is_system) VALUES(?,?,1)`, def.Name, def.Description)
			if insErr != nil {
				return insErr
			}
			if roleID, err = res.LastInsertId(); err != nil {
				return err
			}
		case nil:
			if _, err := tx.Exec(`UPDATE roles SET is_system=1, description=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, def.Description, roleID); err != nil {
				return err
			}
		default:
			return err
		}
		if _, err := tx.Exec(`DELETE FROM role_permissions WHERE role_id=?`, roleID); err != nil {
			return err
		}
		for _, g := range def.Grants {
			if _, err := tx.Exec(`INSERT INTO role_permissions(role_id,resource,action) VALUES(?,?,?)`, roleID, g.Resource, g.Action); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`DELETE FROM role_data_scope WHERE role_id=?`, roleID); err != nil {
			return err
		}
		for _, sc := range def.Scopes {
			if _, err := tx.Exec(`INSERT INTO role_data_scope(role_id,resource,scope) VALUES(?,?,?)`, roleID, sc.Resource, sc.Scope); err != nil {
				return err
			}
		}
	}
	return nil
}

// backfillUserRoles maps every existing user's legacy users.role to a system
// role (admin->Admin, everything else->Accounts) and inserts user_roles rows.
func backfillUserRoles(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id, role FROM users`)
	if err != nil {
		return err
	}
	type u struct {
		id   int64
		role string
	}
	var users []u
	for rows.Next() {
		var one u
		if err := rows.Scan(&one.id, &one.role); err != nil {
			rows.Close()
			return err
		}
		users = append(users, one)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, one := range users {
		name := "Accounts"
		if one.role == "admin" {
			name = "Admin"
		}
		var roleID int64
		if err := tx.QueryRow(`SELECT id FROM roles WHERE lower(name)=lower(?)`, name).Scan(&roleID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO user_roles(user_id,role_id) VALUES(?,?) ON CONFLICT(user_id,role_id) DO NOTHING`, one.id, roleID); err != nil {
			return err
		}
	}
	return nil
}
```

Fold seeding + back-fill into the **existing v1 migration** (do **not** add a v2). Extend the v1 `Up` (from Tasks 1–2) so, after creating the four tables, it seeds the system roles and back-fills `user_roles`:

```go
var migrations = []migration{
	{
		Version: 1,
		Name:    "permission schema",
		Up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS roles ( /* … unchanged from Task 2 … */ );
CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_name_nocase ON roles(lower(name));
CREATE TABLE IF NOT EXISTS role_permissions ( /* … */ );
CREATE TABLE IF NOT EXISTS role_data_scope ( /* … */ );
CREATE TABLE IF NOT EXISTS user_roles ( /* … */ );
`); err != nil {
				return err
			}
			if err := seedSystemRoles(tx); err != nil {
				return err
			}
			return backfillUserRoles(tx)
		},
	},
}
```

Phase 1 still registers exactly one migration (v1); seeding is idempotent (upsert by name) and the tables use `IF NOT EXISTS`, so re-applying v1 is safe.

Add `assignDefaultRoleTx` in `internal/store/migrations.go` (or `permissions.go`):

```go
// assignDefaultRoleTx gives a freshly created user a user_roles row derived from
// its legacy role, so users created after the RBAC migration still resolve to a
// concrete permission set. Tolerant if system roles are somehow absent.
func assignDefaultRoleTx(ctx context.Context, tx *sql.Tx, userID int64, legacyRole string) error {
	name := "Accounts"
	if legacyRole == "admin" {
		name = "Admin"
	}
	var roleID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE lower(name)=lower(?)`, name).Scan(&roleID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES(?,?) ON CONFLICT(user_id,role_id) DO NOTHING`, userID, roleID)
	return err
}
```

*(This requires `context` and `database/sql` imports in the file it lives in — `migrations.go` already imports `database/sql`; add `context`.)*

Rewrite `CreateUser` in `internal/store/store.go` to wrap the insert + default-role assignment in one transaction:

```go
func (s *Store) CreateUser(ctx context.Context, email, name, hash, role string, active bool) (int64, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if err := validateUserFields(email, name, role); err != nil {
		return 0, err
	}
	if role == "" {
		role = "data_entry"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO users(email,name,password_hash,role,active) VALUES(?,?,?,?,?)`,
		email, name, hash, role, boolInt(active))
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := assignDefaultRoleTx(ctx, tx, id, role); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}
```

### Step 4 — run to pass

```
go test ./internal/store/ -run 'TestSeedSystemRolesMatchDefaults|TestDeleteRoleRejectsSystemRole|TestCreateUserAssignsDefaultRole|TestMigrationBackfillsExistingUsers' -v
go test ./internal/store/ -v
```

Expected: PASS across the whole store package (the existing store/backup tests still pass — `CreateUser` remains transactional and does not touch `audit_log`, so the `abortAuditAction` triggers in `store_test.go` are unaffected).

### Step 5 — commit

```
git add internal/store/migrations.go internal/store/store.go internal/store/permissions_test.go internal/store/migrations_test.go
git commit -m "feat(store): seed 4 system roles, back-fill user_roles, default-assign on create

Extends the single migration v1 to seed Requester/Manager/Accounts/Admin
with the overview §5 default grants and scopes and map existing users.role
into user_roles. CreateUser now writes a default user_roles row so new and
bootstrap users resolve to a concrete permission set. System roles are
delete-protected.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 9 — Auth engine rewrite: DB-driven `Can`/`Scope`/`RequirePermission` (R1, R6 foundation)

**Files**
- Modify: `internal/auth/auth.go` (remove Casbin; add DB engine)
- Modify: `internal/auth/auth_test.go` (rewrite the permission test to canonical vocabulary)
- Modify: `internal/app/app.go` (migrate every **existing** route's action string to the canonical vocabulary; the Roles routes are registered in Task 11 with their handlers)

*(Scope note: this task rewrites the auth engine and re-labels the existing routes' actions. It does **not** register the Roles routes — their handlers do not exist yet (Task 11) — and it does **not** modify `TestDataEntryHasRestrictedNavigationAndRoutes`, which depends on the nav gates re-pointed in Task 13 and would be red here; that test change lands in Task 13. The nav/badge gates Phase 0 wrote against the interim Casbin verbs — `request:approve`, `payment:read`, `budget:read`, `payment_attachment:create` — are also left alone here and swept in Task 13, where the stub `PermissionSet` is deleted.)*

**Interfaces**
- Produces: `func (m *Manager) permsFor(u store.User) (store.PermissionSet, error)`, `func (m *Manager) Scope(u store.User, resource string) string`; **rewrites the bodies** of `func (m *Manager) Can(u store.User, resource, action string) bool` and `func (m *Manager) Permissions(u store.User) store.PermissionSet`, which Phase 0 Task 12 already added against Casbin — their signatures are unchanged, so no caller moves; unchanged `RequirePermission(resource, action string, next http.Handler) http.Handler`.
- Consumes: `store.EffectivePermissions`, `store.PermissionSet`, `store.EmptyPermissions`.

### Step 1 — write the failing test

Replace `TestSessionMiddlewareAndPermissions` in `internal/auth/auth_test.go` with a canonical-vocabulary version and add direct `Can` assertions:

```go
func TestSessionMiddlewareAndPermissions(t *testing.T) {
	manager, st := newTestManager(t)
	admin := createTestUser(t, st, "admin@example.test", "admin", true)     // -> Admin role
	entry := createTestUser(t, st, "entry@example.test", "data_entry", true) // -> Accounts role

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentUser(r).ID == 0 {
			t.Fatal("authenticated user was not stored in the request context")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	for _, testCase := range []struct {
		name       string
		user       store.User
		object     string
		action     string
		wantStatus int
	}{
		{name: "admin manages users", user: admin, object: "user", action: "edit", wantStatus: http.StatusNoContent},
		{name: "accounts creates payment", user: entry, object: "payment", action: "create", wantStatus: http.StatusNoContent},
		{name: "accounts denied users", user: entry, object: "user", action: "view", wantStatus: http.StatusForbidden},
		{name: "accounts denied roles", user: entry, object: "role", action: "view", wantStatus: http.StatusForbidden},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := authenticatedRequest(t, manager, testCase.user, http.MethodGet, "/")
			response := httptest.NewRecorder()
			manager.Middleware(manager.RequirePermission(testCase.object, testCase.action, okHandler)).ServeHTTP(response, request)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, testCase.wantStatus)
			}
		})
	}

	if !manager.Can(admin, "role", "create") {
		t.Fatal("admin should manage roles")
	}
	if manager.Can(entry, "role", "view") {
		t.Fatal("accounts must not view roles")
	}
	if got := manager.Scope(entry, "payment"); got != "all" {
		t.Fatalf("accounts payment scope = %q, want all", got)
	}

	response := httptest.NewRecorder()
	manager.Middleware(manager.RequireLogin(okHandler)).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous request = %d %q, want login redirect", response.Code, response.Header().Get("Location"))
	}
}
```

### Step 2 — run it (expect failure)

```
go test ./internal/auth/ -run TestSessionMiddlewareAndPermissions -v
```

Expected: FAIL — old Casbin engine denies `user/edit`/`payment/create` under new vocabulary; `manager.Can`/`manager.Scope` undefined.

### Step 3 — minimal implementation

Rewrite `internal/auth/auth.go`. Remove the Casbin imports and `enforcer` field; replace `New` and `RequirePermission`, add `permsFor`/`Permissions`/`Can`/`Scope`:

```go
import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fervidbudget/internal/config"
	"fervidbudget/internal/store"

	"golang.org/x/crypto/bcrypt"
)

type Manager struct {
	cfg     config.Config
	store   *store.Store
	onError func(http.ResponseWriter, *http.Request, int, string)
}

func New(cfg config.Config, st *store.Store) (*Manager, error) {
	return &Manager{cfg: cfg, store: st}, nil
}

// permsFor loads the caller's effective permission set. Correctness-first: it
// queries per request (a short-TTL cache keyed by user id is a later option).
func (m *Manager) permsFor(u store.User) (store.PermissionSet, error) {
	if u.ID == 0 {
		return store.EmptyPermissions(), nil
	}
	return m.store.EffectivePermissions(context.Background(), u.ID)
}

// Permissions returns the caller's effective permission set for template use;
// on error it returns an empty (deny-all) set. store.PermissionSet is an
// interface, so the deny-all value comes from store.EmptyPermissions() rather
// than a struct literal.
func (m *Manager) Permissions(u store.User) store.PermissionSet {
	ps, err := m.permsFor(u)
	if err != nil {
		return store.EmptyPermissions()
	}
	return ps
}

func (m *Manager) Can(u store.User, resource, action string) bool {
	ps, err := m.permsFor(u)
	if err != nil {
		return false
	}
	return ps.Can(resource, action)
}

func (m *Manager) Scope(u store.User, resource string) string {
	ps, err := m.permsFor(u)
	if err != nil {
		return ""
	}
	return ps.Scope(resource)
}

func (m *Manager) RequirePermission(resource, action string, next http.Handler) http.Handler {
	return m.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.Can(CurrentUser(r), resource, action) {
			m.writeError(w, r, http.StatusForbidden, "You do not have permission to perform this action.")
			return
		}
		next.ServeHTTP(w, r)
	}))
}
```

Migrate every **existing** route action string in `internal/app/app.go` `routes()` to the canonical vocabulary. Do **not** add the Roles routes here — their handlers (`rolesPage`/`rolesSave`/`roleCreate`/`roleCopy`/`roleDelete`) do not exist yet and are registered together with their definitions in Task 11. Replace the route block with:

```go
	mux.Handle("GET /months", a.auth.RequirePermission("month", "view", http.HandlerFunc(a.months)))
	mux.Handle("POST /months", a.auth.RequirePermission("month", "create", http.HandlerFunc(a.withCSRF(a.monthCreate))))
	mux.Handle("GET /payments/new", a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.paymentForm)))
	mux.Handle("GET /payments", a.auth.RequirePermission("payment", "view", http.HandlerFunc(a.payments)))
	mux.Handle("POST /payments", a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.withCSRF(a.paymentCreate))))
	mux.Handle("GET /payments/{id}", a.auth.RequirePermission("payment", "view", http.HandlerFunc(a.paymentDetail)))
	mux.Handle("GET /payments/{id}/edit", a.auth.RequirePermission("payment", "edit", http.HandlerFunc(a.paymentEditForm)))
	mux.Handle("POST /payments/{id}/edit", a.auth.RequirePermission("payment", "edit", http.HandlerFunc(a.withCSRF(a.paymentEdit))))
	mux.Handle("POST /payments/{id}/void", a.auth.RequirePermission("payment", "void", http.HandlerFunc(a.withCSRF(a.paymentVoid))))
	mux.Handle("POST /payments/{id}/attachments", a.auth.RequirePermission("attachment", "create", http.HandlerFunc(a.withCSRF(a.attachmentUpload))))
	mux.Handle("GET /attachments/{id}", a.auth.RequirePermission("attachment", "view", http.HandlerFunc(a.attachmentDownload)))
	mux.Handle("GET /export.csv", a.auth.RequirePermission("grid", "export", http.HandlerFunc(a.exportGrid)))
	mux.Handle("GET /reports/monthly", a.auth.RequirePermission("report", "view", http.HandlerFunc(a.report)))
	mux.Handle("GET /reports/projects", a.auth.RequirePermission("report", "view", http.HandlerFunc(a.report)))
	mux.Handle("GET /reports/heads", a.auth.RequirePermission("report", "view", http.HandlerFunc(a.report)))
	mux.Handle("GET /reports/ytd.csv", a.auth.RequirePermission("report", "export", http.HandlerFunc(a.exportYTD)))
	mux.Handle("GET /budgets", a.auth.RequirePermission("budget", "view", http.HandlerFunc(a.budgets)))
	mux.Handle("POST /budgets", a.auth.RequirePermission("budget", "edit", http.HandlerFunc(a.withCSRF(a.budgetSave))))
	mux.Handle("POST /months/{month}/lock", a.auth.RequirePermission("month", "lock", http.HandlerFunc(a.withCSRF(a.lockMonth))))
	mux.Handle("POST /months/{month}/unlock", a.auth.RequirePermission("month", "lock", http.HandlerFunc(a.withCSRF(a.unlockMonth))))
	mux.Handle("GET /projects", a.auth.RequirePermission("project", "view", http.HandlerFunc(a.projects)))
	mux.Handle("POST /projects", a.auth.RequirePermission("project", "edit", http.HandlerFunc(a.withCSRF(a.projectSave))))
	mux.Handle("GET /heads", a.auth.RequirePermission("head", "view", http.HandlerFunc(a.heads)))
	mux.Handle("POST /heads", a.auth.RequirePermission("head", "edit", http.HandlerFunc(a.withCSRF(a.headSave))))
	mux.Handle("GET /users", a.auth.RequirePermission("user", "view", http.HandlerFunc(a.users)))
	mux.Handle("POST /users", a.auth.RequirePermission("user", "edit", http.HandlerFunc(a.withCSRF(a.userSave))))
	mux.Handle("GET /audit", a.auth.RequirePermission("audit", "view", http.HandlerFunc(a.auditLog)))
	mux.Handle("GET /backups", a.auth.RequirePermission("backup", "view", http.HandlerFunc(a.backups)))
	mux.Handle("POST /backups", a.auth.RequirePermission("backup", "create", http.HandlerFunc(a.withCSRF(a.backupCreate))))
```

The `/roles` routes are intentionally absent: they are registered in Task 11 next to the handlers that serve them, so this task never references an undefined handler and the app package keeps compiling.

`TestDataEntryHasRestrictedNavigationAndRoutes` is **not** touched here — the Phase 0 shell still gates its items on the interim Casbin verbs, so the sidebar it asserts is only correct once Task 13 re-points `navSpec` onto the canonical vocabulary. Its rewrite and commit are deferred to Task 13, where it goes green.

### Step 4 — run to pass

```
go test ./internal/auth/ -run TestSessionMiddlewareAndPermissions -v
go build ./...
```

Expected: auth test PASS; the app package builds with the route/vocabulary changes (no Roles routes yet, so no undefined-handler references). The `TestDataEntryHasRestrictedNavigationAndRoutes` rewrite is deferred to Task 13, so this task commits no knowingly-red test.

### Step 5 — commit

```
git add internal/auth/auth.go internal/auth/auth_test.go internal/app/app.go
git commit -m "refactor(auth): replace Casbin with DB-driven permission engine

Manager.Can/Scope/permsFor delegate to store.EffectivePermissions and
RequirePermission consults them; the Phase 0 Can/Permissions signatures are
unchanged, only their bodies. Every existing route action string migrates to
the canonical view/create/edit vocabulary. The Roles routes and their
handlers land together in Task 11. Auth permission test rewritten to the new
vocabulary.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 10 — `internal/app/permmap.go`: the 9 × 7 presentation map over the canonical vocabulary (D2, R2)

> **Status: DONE** — shipped as commit `48704ad`. Body kept as the record of what was planned; three points of it were stale and the shipped code differs:
>
> 1. **The `Cells` map below orphans two grants.** It was written against the 64-pair vocabulary, before amendment A10 added `request:cancel` and `approval:cancel`. Both belong to the Payment requests row, and the shipped `requests` row carries all three cancellation verbs in its `cancel` cell: `request:withdraw`, `request:cancel`, `approval:cancel`. Transcribed literally, the totality test fails with `cells map 64 grants, the canonical vocabulary has 66`.
> 2. **`requests.Advanced` is 15 entries, not 13** (request 7 + approval 6 + attachment 2), for the same reason. The assertion in Step 1 was updated.
> 3. **The three `store` accessors were not added.** `internal/store/permissions.go` was owned by the concurrent Tasks 6–9 work, so `ResourceOrder`/`ResourceActions`/`IsScopedResource` do not exist. The shipped map is self-contained in `internal/app`: `permGroupDef.actionsFor` derives a row's canonical actions from its own cells (so `permGroups` stays the single place a grant is written down), and `isScopedResource` wraps the already-exported `store.ValidScope(resource, store.ScopeAll)`. The totality proof is unchanged in strength — the test checks every mapped grant with `store.ValidGrant` (map ⊆ vocabulary) and that the map holds exactly 66 distinct grants across 21 resources, and a subset of equal size is the whole set. **Task 11 still needs `store.ResourceOrder`/`IsScopedResource`** (plan lines ~2713 and ~3081): either add the accessors then, or iterate `permGroups`.

The approved roles screen draws **9 page rows × 7 fixed action columns**. The canonical vocabulary is **21 resources with ragged action lists**. Decision D2 keeps both: enforcement stays canonical (`role_permissions` remains one row per `(resource, action)`, and the permission engine from Tasks 3–7 does not change by one line), while the admin screen renders a presentation map in which **each cell is a set of canonical grants**. Toggling a cell grants or revokes every canonical action behind it; each row also carries an **Advanced** disclosure listing those actions individually, which is what preserves R2's fine-grained control. A cell whose row has no canonical action for that column is *unavailable* and renders `—`, not a checkbox.

This task builds only the map and its guarantees. Task 11 renders it.

**Files**
- Create: `internal/app/permmap.go`
- Modify: `internal/store/permissions.go` (three exported accessors, because the vocabulary vars are unexported)
- Test: `internal/app/permmap_test.go`

**Interfaces**
- Produces: `type permColumn struct{Key, Label string}` + `var permColumns []permColumn` (7, fixed order); `type permGroupDef struct{Key, Label string; Resources []string; Cells map[string][]store.Grant}` + `var permGroups []permGroupDef` (9); view-model `permRow`/`permCell`/`permAdvanced`; `func permGroup(key string) (permGroupDef, bool)`; `func expandCells(cells []string) []store.Grant`; `func buildPermMatrix(grants []store.Grant, scopes []store.ScopeGrant) []permRow`.
- Produces (in `store`): `func ResourceOrder() []string`, `func ResourceActions(resource string) []string`, `func IsScopedResource(resource string) bool`.
- Consumes: `store.Grant`, `store.ScopeGrant`, `store.ValidGrant`, `entityText`.
- **Not** consumed by: `internal/store`, `internal/auth`. The map is presentation; nothing that enforces anything may import it.

Add the accessors to `internal/store/permissions.go` first — the map cannot be validated against a vocabulary it cannot read:

```go
// ResourceOrder returns the canonical resources in display order.
func ResourceOrder() []string { return append([]string(nil), resourceOrder...) }

// ResourceActions returns the canonical actions for a resource.
func ResourceActions(resource string) []string {
	return append([]string(nil), resourceActions[resource]...)
}

// IsScopedResource reports whether a resource carries a data scope.
func IsScopedResource(resource string) bool { return scopedResources[resource] }
```

### Step 1 — write the failing test

Create `internal/app/permmap_test.go`:

```go
package app

import (
	"testing"

	"fervidbudget/internal/store"
)

// This is the contract between the drawn matrix and the enforced vocabulary.
// If a canonical pair were reachable from nowhere, an admin could never grant
// it; if it were reachable from two cells, ticking one and clearing the other
// would silently disagree about the same permission. Neither is allowed.
func TestPermMapCoversCanonicalVocabularyExactlyOnce(t *testing.T) {
	if len(permGroups) != 9 {
		t.Fatalf("permGroups = %d rows, want the 9 the approved design draws", len(permGroups))
	}
	wantColumns := []string{"view", "create", "edit", "approve", "process", "cancel", "export"}
	if len(permColumns) != len(wantColumns) {
		t.Fatalf("permColumns = %d, want %d", len(permColumns), len(wantColumns))
	}
	for i, want := range wantColumns {
		if permColumns[i].Key != want {
			t.Fatalf("permColumns[%d] = %q, want %q", i, permColumns[i].Key, want)
		}
	}

	// 1. The rows partition the canonical resources.
	owner := map[string]string{}
	for _, group := range permGroups {
		for _, resource := range group.Resources {
			if len(store.ResourceActions(resource)) == 0 {
				t.Fatalf("row %q claims %q, which is not in the canonical vocabulary", group.Key, resource)
			}
			if prev, dup := owner[resource]; dup {
				t.Fatalf("resource %q is owned by both row %q and row %q", resource, prev, group.Key)
			}
			owner[resource] = group.Key
		}
	}
	for _, resource := range store.ResourceOrder() {
		if _, ok := owner[resource]; !ok {
			t.Fatalf("canonical resource %q is orphaned: no matrix row owns it", resource)
		}
	}

	// 2. Every cell grant is canonical, belongs to its own row, and is mapped by
	//    exactly one cell in the whole matrix.
	known := map[string]bool{}
	for _, column := range permColumns {
		known[column.Key] = true
	}
	cellOf := map[store.Grant]string{}
	for _, group := range permGroups {
		rowResources := map[string]bool{}
		for _, resource := range group.Resources {
			rowResources[resource] = true
		}
		for key := range group.Cells {
			if !known[key] {
				t.Fatalf("row %q declares unknown column %q", group.Key, key)
			}
		}
		for _, column := range permColumns {
			for _, grant := range group.Cells[column.Key] {
				if !store.ValidGrant(grant.Resource, grant.Action) {
					t.Fatalf("cell %s:%s maps %s:%s, which is not canonical", group.Key, column.Key, grant.Resource, grant.Action)
				}
				if !rowResources[grant.Resource] {
					t.Fatalf("cell %s:%s maps %s:%s from a resource the row does not own", group.Key, column.Key, grant.Resource, grant.Action)
				}
				if prev, dup := cellOf[grant]; dup {
					t.Fatalf("%s:%s is mapped twice: by %s and by %s:%s", grant.Resource, grant.Action, prev, group.Key, column.Key)
				}
				cellOf[grant] = group.Key + ":" + column.Key
			}
		}
	}

	// 3. Nothing orphaned: every canonical pair sits in exactly one row's
	//    Advanced list, which is the disclosure that preserves R2.
	advanced := map[store.Grant]int{}
	for _, row := range buildPermMatrix(nil, nil) {
		for _, entry := range row.Advanced {
			advanced[store.Grant{Resource: entry.Resource, Action: entry.Action}]++
		}
	}
	total := 0
	for _, resource := range store.ResourceOrder() {
		for _, action := range store.ResourceActions(resource) {
			total++
			grant := store.Grant{Resource: resource, Action: action}
			if advanced[grant] != 1 {
				t.Fatalf("%s:%s appears %d times across the Advanced lists, want exactly 1", resource, action, advanced[grant])
			}
			if _, ok := cellOf[grant]; !ok {
				t.Fatalf("%s:%s is reachable from no cell", resource, action)
			}
		}
	}
	if len(cellOf) != total {
		t.Fatalf("cells map %d grants, the canonical vocabulary has %d", len(cellOf), total)
	}
	if len(advanced) != total {
		t.Fatalf("Advanced lists cover %d grants, the canonical vocabulary has %d", len(advanced), total)
	}
}

func TestExpandCellsGrantsEveryCanonicalActionBehindTheCell(t *testing.T) {
	grants := expandCells([]string{"requests:approve"})
	want := map[string]bool{"approve": true, "reject": true, "return": true, "reassign": true, "accept_partial": true}
	if len(grants) != len(want) {
		t.Fatalf("expandCells(requests:approve) = %v, want all %d approval actions", grants, len(want))
	}
	for _, g := range grants {
		if g.Resource != "approval" || !want[g.Action] {
			t.Fatalf("expandCells produced %s:%s, which is outside the cell", g.Resource, g.Action)
		}
	}
	// Duplicates collapse; an unavailable cell, an unknown row and a malformed
	// value all expand to nothing rather than to a bogus grant.
	if got := expandCells([]string{"vendors:view", "vendors:view"}); len(got) != 1 {
		t.Fatalf("expandCells deduplication = %v, want one grant", got)
	}
	if got := expandCells([]string{"requests:process", "reservations:approve", "nonsense", "nope:view"}); len(got) != 0 {
		t.Fatalf("expandCells on empty/unknown cells = %v, want none", got)
	}
}

func TestBuildPermMatrixMarksGrantedPartialAndUnavailableCells(t *testing.T) {
	held := []store.Grant{
		{Resource: "request", Action: "view"},
		{Resource: "attachment", Action: "view"},
		{Resource: "approval", Action: "approve"},
	}
	var requests permRow
	for _, row := range buildPermMatrix(held, []store.ScopeGrant{{Resource: "request", Scope: "all"}}) {
		if row.Key == "requests" {
			requests = row
		}
	}
	cells := map[string]permCell{}
	for _, cell := range requests.Cells {
		cells[cell.Column] = cell
	}
	if len(requests.Cells) != len(permColumns) {
		t.Fatalf("row rendered %d cells, want one per column (%d)", len(requests.Cells), len(permColumns))
	}
	if !cells["view"].Granted || cells["view"].Partial {
		t.Fatalf("view cell = %+v, want fully granted (request:view + attachment:view)", cells["view"])
	}
	if cells["approve"].Granted || !cells["approve"].Partial {
		t.Fatalf("approve cell = %+v, want partial (1 of the 5 approval actions)", cells["approve"])
	}
	if cells["process"].Available {
		t.Fatalf("process cell = %+v, want unavailable so the template renders an em dash", cells["process"])
	}
	if cells["create"].Granted || cells["create"].Partial {
		t.Fatalf("create cell = %+v, want untouched", cells["create"])
	}
	if requests.Held != 1 || requests.Total != len(permColumns) {
		t.Fatalf("row badge = %d of %d, want 1 of %d", requests.Held, requests.Total, len(permColumns))
	}
	if !requests.Scoped || requests.ScopeResource != "request" || requests.Scope != "all" {
		t.Fatalf("row scope = %q on %q (scoped=%v), want all on request", requests.Scope, requests.ScopeResource, requests.Scoped)
	}
	if got := len(requests.Advanced); got != 13 {
		t.Fatalf("Advanced list = %d entries, want 13 (request 6 + approval 5 + attachment 2)", got)
	}
	// Advanced entries carry the cell that also covers them, so the template can
	// keep the two in sync and the reader can see why a cell is partial.
	for _, entry := range requests.Advanced {
		if entry.Resource == "approval" && entry.Cell != "requests:approve" {
			t.Fatalf("advanced entry %s:%s reports cell %q, want requests:approve", entry.Resource, entry.Action, entry.Cell)
		}
		if entry.Resource == "request" && entry.Action == "view" && !entry.Granted {
			t.Fatal("advanced entry request:view should be granted")
		}
	}
	// An unscoped row offers no scope control at all: the template renders the
	// static "None" pill the mockup shows on Administration.
	for _, row := range buildPermMatrix(nil, nil) {
		if row.Key == "administration" && row.Scoped {
			t.Fatal("Administration must not be data-scoped")
		}
	}
}
```

### Step 2 — run it (expect failure)

```
go test ./internal/app/ -run 'TestPermMap|TestExpandCells|TestBuildPermMatrix' -v
```

Expected: compile failure — `undefined: permGroups`, `undefined: permColumns`, `undefined: expandCells`, `undefined: buildPermMatrix`, and `undefined: store.ResourceOrder` until the accessors above are added.

### Step 3 — minimal implementation

Create `internal/app/permmap.go`:

```go
package app

import (
	"strings"

	"fervidbudget/internal/store"
)

// The roles screen draws 9 page rows × 7 action columns; the vocabulary
// underneath is 21 resources with ragged action lists. This file is the only
// place the two meet (adoption-spec D2).
//
// Enforcement never consults it: role_permissions stays one row per
// (resource, action) and internal/store and internal/auth do not import this
// package. A cell is a SET of canonical grants — toggling it grants or revokes
// all of them — and every row carries an Advanced disclosure enumerating its
// individual canonical actions, which is what keeps R2's fine-grained control.
// A cell with no canonical action for its column is unavailable and renders an
// em dash rather than a checkbox.
//
// Three canonical resources are not named in the D2 row table, because the
// approved screen draws only nine rows. They are placed where the approved
// screens put their controls, and permmap_test.go asserts the placement is
// total and unambiguous:
//
//	attachment      -> Payment requests. The uploader appears on the request
//	                   form and on payment entry; one grant governs both.
//	project, head   -> Budgets & variance grid. Projects and heads are the two
//	                   axes of the grid and of every budget row — they are
//	                   masters of the budget, not of administration.

type permColumn struct {
	Key   string
	Label string
}

// permColumns is the fixed column set of the approved matrix, in order.
var permColumns = []permColumn{
	{Key: "view", Label: "View"},
	{Key: "create", Label: "Create"},
	{Key: "edit", Label: "Edit"},
	{Key: "approve", Label: "Approve"},
	{Key: "process", Label: "Process"},
	{Key: "cancel", Label: "Cancel"},
	{Key: "export", Label: "Export"},
}

type permGroupDef struct {
	Key       string
	Label     string
	Resources []string
	Cells     map[string][]store.Grant
}

// permGroups is the 9-row presentation map. Every canonical resource is owned
// by exactly one row and every canonical action by at most one cell.
var permGroups = []permGroupDef{
	{
		Key:       "requests",
		Label:     "Payment requests",
		Resources: []string{"request", "approval", "attachment"},
		Cells: map[string][]store.Grant{
			"view":   {{Resource: "request", Action: "view"}, {Resource: "attachment", Action: "view"}},
			"create": {{Resource: "request", Action: "create"}, {Resource: "request", Action: "reraise"}, {Resource: "attachment", Action: "create"}},
			"edit":   {{Resource: "request", Action: "edit"}, {Resource: "request", Action: "comment"}},
			"approve": {
				{Resource: "approval", Action: "approve"}, {Resource: "approval", Action: "reject"},
				{Resource: "approval", Action: "return"}, {Resource: "approval", Action: "reassign"},
				{Resource: "approval", Action: "accept_partial"},
			},
			"cancel": {{Resource: "request", Action: "withdraw"}},
		},
	},
	{
		Key:       "payments",
		Label:     "Payments",
		Resources: []string{"payment"},
		Cells: map[string][]store.Grant{
			"view":   {{Resource: "payment", Action: "view"}},
			"create": {{Resource: "payment", Action: "create"}},
			"edit":   {{Resource: "payment", Action: "edit"}},
			"process": {
				{Resource: "payment", Action: "process"}, {Resource: "payment", Action: "settle"},
				{Resource: "payment", Action: "mark_partial"}, {Resource: "payment", Action: "hold"},
			},
			"cancel": {{Resource: "payment", Action: "void"}},
		},
	},
	{
		Key:       "reservations",
		Label:     "Reservations",
		Resources: []string{"reservation"},
		Cells: map[string][]store.Grant{
			"create": {{Resource: "reservation", Action: "reserve"}},
			"edit":   {{Resource: "reservation", Action: "reassign"}},
			"cancel": {{Resource: "reservation", Action: "release"}},
		},
	},
	{
		Key:       "recoverables",
		Label:     "Recoverables",
		Resources: []string{"recoverable_report", "recoverable_category"},
		Cells: map[string][]store.Grant{
			"view":   {{Resource: "recoverable_report", Action: "view"}, {Resource: "recoverable_category", Action: "view"}},
			"create": {{Resource: "recoverable_category", Action: "create"}},
			"edit":   {{Resource: "recoverable_category", Action: "edit"}},
			"cancel": {{Resource: "recoverable_category", Action: "delete"}},
			"export": {{Resource: "recoverable_report", Action: "export"}},
		},
	},
	{
		Key:       "vendors",
		Label:     "Vendors",
		Resources: []string{"vendor"},
		Cells: map[string][]store.Grant{
			"view":   {{Resource: "vendor", Action: "view"}},
			"create": {{Resource: "vendor", Action: "create"}},
			"edit":   {{Resource: "vendor", Action: "edit"}},
		},
	},
	{
		Key:       "vendor-bank",
		Label:     "Vendor bank details",
		Resources: []string{"vendor_bank"},
		Cells: map[string][]store.Grant{
			"view": {{Resource: "vendor_bank", Action: "view"}},
			"edit": {{Resource: "vendor_bank", Action: "edit"}},
		},
	},
	{
		Key:       "budgets",
		Label:     "Budgets & variance grid",
		Resources: []string{"budget", "grid", "month", "project", "head"},
		Cells: map[string][]store.Grant{
			"view": {
				{Resource: "budget", Action: "view"}, {Resource: "grid", Action: "view"},
				{Resource: "month", Action: "view"}, {Resource: "project", Action: "view"},
				{Resource: "head", Action: "view"},
			},
			"create":  {{Resource: "month", Action: "create"}, {Resource: "project", Action: "create"}, {Resource: "head", Action: "create"}},
			"edit":    {{Resource: "budget", Action: "edit"}, {Resource: "project", Action: "edit"}, {Resource: "head", Action: "edit"}},
			"process": {{Resource: "month", Action: "lock"}},
			"export":  {{Resource: "grid", Action: "export"}},
		},
	},
	{
		Key:       "reports",
		Label:     "Reports",
		Resources: []string{"report"},
		Cells: map[string][]store.Grant{
			"view":   {{Resource: "report", Action: "view"}},
			"export": {{Resource: "report", Action: "export"}},
		},
	},
	{
		Key:       "administration",
		Label:     "Administration",
		Resources: []string{"user", "role", "notification", "config", "audit", "backup"},
		Cells: map[string][]store.Grant{
			"view": {
				{Resource: "user", Action: "view"}, {Resource: "role", Action: "view"},
				{Resource: "notification", Action: "view"}, {Resource: "config", Action: "view"},
				{Resource: "audit", Action: "view"}, {Resource: "backup", Action: "view"},
			},
			"create": {{Resource: "user", Action: "create"}, {Resource: "role", Action: "create"}, {Resource: "backup", Action: "create"}},
			"edit": {
				{Resource: "user", Action: "edit"}, {Resource: "role", Action: "edit"},
				{Resource: "notification", Action: "edit"}, {Resource: "config", Action: "edit"},
			},
			"cancel": {{Resource: "role", Action: "delete"}},
		},
	},
}

// permAdvanced is one canonical action inside a row's Advanced disclosure.
// Cell is the "<group>:<column>" value of the cell that also covers it, so the
// screen can keep the two in step; it is empty for an action no cell reaches.
type permAdvanced struct {
	Resource string
	Action   string
	Label    string
	Cell     string
	Granted  bool
}

type permCell struct {
	Column    string
	Label     string
	Value     string // "<group>:<column>" — the submitted form value
	Available bool   // false renders an em dash instead of a checkbox
	Granted   bool   // every canonical action behind the cell is held
	Partial   bool   // some, but not all, are held
}

type permRow struct {
	Key           string
	Label         string
	Cells         []permCell
	Advanced      []permAdvanced
	Held          int    // fully granted cells — the mobile "N of 7" badge
	Total         int    // len(permColumns)
	Scoped        bool   // the row owns a data-scoped resource
	ScopeResource string // which one; the form field is "scope_" + this
	Scope         string // "", "own", "assigned" or "all"
}

func permGroup(key string) (permGroupDef, bool) {
	for _, group := range permGroups {
		if group.Key == key {
			return group, true
		}
	}
	return permGroupDef{}, false
}

// expandCells turns submitted "<group>:<column>" cell values into the full set
// of canonical grants behind them, deduplicated. Unknown rows, unknown columns
// and cells with no canonical action expand to nothing, so a hand-crafted POST
// can never invent a grant.
func expandCells(cells []string) []store.Grant {
	var out []store.Grant
	seen := map[store.Grant]bool{}
	for _, raw := range cells {
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			continue
		}
		group, ok := permGroup(parts[0])
		if !ok {
			continue
		}
		for _, grant := range group.Cells[parts[1]] {
			if seen[grant] {
				continue
			}
			seen[grant] = true
			out = append(out, grant)
		}
	}
	return out
}

// buildPermMatrix renders one role's stored grants and scopes as the drawn
// matrix. Both the desktop table and the mobile accordion are built from this
// one result, so they can never disagree.
func buildPermMatrix(grants []store.Grant, scopes []store.ScopeGrant) []permRow {
	held := map[store.Grant]bool{}
	for _, grant := range grants {
		held[grant] = true
	}
	scope := map[string]string{}
	for _, sc := range scopes {
		scope[sc.Resource] = sc.Scope
	}

	rows := make([]permRow, 0, len(permGroups))
	for _, group := range permGroups {
		row := permRow{Key: group.Key, Label: group.Label, Total: len(permColumns)}
		columnOf := map[store.Grant]string{}
		for _, column := range permColumns {
			behind := group.Cells[column.Key]
			cell := permCell{
				Column:    column.Key,
				Label:     column.Label,
				Value:     group.Key + ":" + column.Key,
				Available: len(behind) > 0,
			}
			got := 0
			for _, grant := range behind {
				columnOf[grant] = column.Key
				if held[grant] {
					got++
				}
			}
			cell.Granted = cell.Available && got == len(behind)
			cell.Partial = got > 0 && !cell.Granted
			if cell.Granted {
				row.Held++
			}
			row.Cells = append(row.Cells, cell)
		}
		for _, resource := range group.Resources {
			for _, action := range store.ResourceActions(resource) {
				grant := store.Grant{Resource: resource, Action: action}
				cell := ""
				if column, ok := columnOf[grant]; ok {
					cell = group.Key + ":" + column
				}
				row.Advanced = append(row.Advanced, permAdvanced{
					Resource: resource,
					Action:   action,
					Label:    entityText(resource) + " · " + strings.ReplaceAll(action, "_", " "),
					Cell:     cell,
					Granted:  held[grant],
				})
			}
			if store.IsScopedResource(resource) {
				row.Scoped = true
				row.ScopeResource = resource
				row.Scope = scope[resource]
			}
		}
		rows = append(rows, row)
	}
	return rows
}
```

### Step 4 — run to pass

```
go test ./internal/app/ -run 'TestPermMap|TestExpandCells|TestBuildPermMatrix' -v
go test ./internal/store/ -run TestPermissionVocabularyIsCanonical -v
```

Expected: PASS. The coverage test is deliberately brittle in one direction only — adding a resource to `resourceActions` without giving it a row fails immediately, which is exactly the guard Phases 1V–5 need when they extend the vocabulary.

### Step 5 — commit

```
git add internal/app/permmap.go internal/app/permmap_test.go internal/store/permissions.go
git commit -m "feat(app): map the roles matrix onto the canonical vocabulary (D2)

permmap.go defines the 9 page rows x 7 action columns the approved design
draws, each cell a set of canonical grants, plus expandCells and
buildPermMatrix for the screen. Enforcement is untouched: role_permissions
stays one row per (resource, action) and nothing under internal/store or
internal/auth imports this map. A unit test proves the bridge is total and
unambiguous — every canonical pair is reachable through exactly one cell and
listed in exactly one row's Advanced disclosure.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 11 — Roles admin screen on the design system: matrix editor, create / copy / delete (R1, R2, R3, R5, R9 UI; D2)

Rebuilt against `mockups/screens/admin-roles.html`. The screen is: a `.segmented` role switcher whose custom roles carry an `.n` "custom" badge; the role's name and description in a `.card` whose `.card-head` carries a `.pill.good` "N users"; **two renderings of one dataset** — a desktop `table.perm-table` inside `.table-wrap.d-only` and a mobile `.perm-acc.m-only` of `.pa-item`/`.pa-head`/`.pa-body` — data scope as a 3-state `.perm-scope` pill group with an explicit `None`; and a sticky `.action-bar` with an `.ab-note.d-only`. Every button is `.btn` plus a modifier. The old `.split` + `<aside class="card">` role list, the `<select>` scope control and the `.badge` element are gone.

**Two matrices, one form, no double submission.** Both renderings live inside the same `<form>`, so both would post. The desktop table is authoritative without JavaScript (it is inside `.table-wrap`, which scrolls horizontally on a phone), and the mobile accordion's inputs are therefore rendered with `disabled` in the HTML. `fervid-app.js` swaps `disabled` between the two on `matchMedia('(max-width: 860px)')`, so exactly one rendering ever contributes to a POST.

**Save semantics.** `grants = expand(submitted "cell" values) ∪ submitted "perm" values`. A cell contributes all of its canonical actions; an Advanced checkbox contributes exactly one. The union is order-independent and idempotent, and it is what makes a *partially* granted cell expressible. `UpdateRolePermissions` (Task 4) then replaces the role's whole grant set, so anything not submitted is revoked.

**Files**
- Modify: `internal/app/app.go` (real `rolesPage`/`rolesSave`/`roleCreate`/`roleCopy`/`roleDelete`; **register the `/roles` routes in `routes()` now that the handlers exist**)
- Modify: `internal/app/templates.go` (rebuilt `"roles"` template)
- Modify: `internal/store/permissions.go` (`UpdateRole` — the `.card` fields have to save somewhere)
- Modify: `web/static/fervid-app.js` (matrix hand-over + cell/Advanced sync)
- Test: `internal/app/app_integration_test.go`, `internal/store/permissions_test.go`

**Interfaces**
- Consumes: `store.AllRoles`, `store.Role`, `store.RolePermissions`, `store.CreateRole`, `store.CopyRole`, `store.UpdateRolePermissions`, `store.DeleteRole`, `store.ResourceOrder`/`IsScopedResource`, and `permColumns`/`buildPermMatrix`/`expandCells` from Task 10.
- Produces (store): `UpdateRole(ctx, actor User, id int64, name, description string) error`.
- Produces (`PageData` fields): `Roles []store.Role`, `Role store.Role`, `PermColumns []permColumn`, `PermMatrix []permRow`, `RoleUserCounts map[int64]int`.

The view-model is Task 10's (`permRow`/`permCell`/`permAdvanced`); the old `matrixResource`/`matrixAction`/`buildRoleMatrix` types are not built at all — `buildPermMatrix` replaces them.

**Dependencies on Phase 0.** `.segmented`, `.card`/`.card-head`/`.card-body`, `.form-grid`/`.field.span-*`, `.pill`, `.checkline`, `.table-wrap`, `.perm-table`/`.perm-acc`/`.perm-scope`, `.action-bar`/`.ab-note`, `.overlay`/`.sheet` and `.m-only`/`.d-only` are all ported by Phase 0 Tasks 6–10; this task adds no component CSS. One exception: `.m-half` is one of the `html[data-device]` rules D4 converts to a media query. Begin Step 3 with `grep -c 'm-half' web/static/fervid-ds.css`; if it returns `0`, append this single rule inside the existing `@media (max-width: 860px)` block, which is where D4 requires it to live:

```css
  .form-grid > .field.m-half { grid-column: span 6; }
```

### Step 1 — write the failing test

Append to `internal/app/app_integration_test.go`:

```go
func TestRolesAdminScreenRendersApprovedMatrix(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/roles", nil, ""))

	// The approved chrome: segmented switcher, role card, both matrices, the
	// scope pill group and the sticky action bar.
	for _, want := range []string{
		`class="segmented"`, `class="card"`, `class="card-head"`, `class="pill good"`,
		`class="form-grid"`, `class="field span-4 m-half"`,
		`class="table-wrap d-only"`, `class="perm-table"`,
		`class="perm-acc m-only"`, `class="pa-head"`, `class="chev"`, `class="pa-body"`,
		`class="perm-scope"`, `class="action-bar"`, `class="ab-note d-only"`,
		`class="btn primary"`, `class="btn outline"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/roles is missing the approved markup %q", want)
		}
	}
	// Every seeded role is a segment; custom roles are badged.
	for _, want := range []string{"Requester", "Manager", "Accounts", "Admin"} {
		if !strings.Contains(body, want) {
			t.Fatalf("/roles page missing role %q", want)
		}
	}
	// All nine rows, by their design labels.
	for _, want := range []string{
		"Payment requests", "Payments", "Reservations", "Recoverables", "Vendors",
		"Vendor bank details", "Budgets &amp; variance grid", "Reports", "Administration",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/roles matrix missing row %q", want)
		}
	}
	// Cells, the Advanced disclosure, and an em dash where a row has no
	// canonical action for a column (Reservations has no View).
	for _, want := range []string{`name="cell"`, `name="perm"`, "Advanced", "—"} {
		if !strings.Contains(body, want) {
			t.Fatalf("/roles matrix missing %q", want)
		}
	}
	// Only one of the two renderings may post: the mobile inputs ship disabled.
	if !strings.Contains(body, `name="cell" disabled`) {
		t.Fatal("mobile accordion inputs must be rendered disabled so only one matrix submits")
	}
	// D5: .badge is retired.
	if strings.Contains(body, `class="badge`) {
		t.Fatal("/roles still renders the retired .badge class")
	}
}

func TestRolesAdminCreateEditCopyDelete(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	// Create a custom role.
	resp := s.postForm("/roles/new", url.Values{"name": {"Reviewer"}, "description": {"read only"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var reviewerID int64
	for _, r := range roles {
		if r.Name == "Reviewer" {
			reviewerID = r.ID
		}
	}
	if reviewerID == 0 {
		t.Fatal("Reviewer role was not created")
	}

	// Save through the matrix: one cell expands to every canonical action behind
	// it, one Advanced checkbox adds a single refinement, and the name and
	// description in the card are persisted alongside.
	save := url.Values{
		"role_id":       {strconvFormat(reviewerID)},
		"name":          {"Reviewer"},
		"description":   {"Reviews and approves"},
		"cell":          {"requests:approve", "reports:view"},
		"perm":          {"request:view"},
		"scope_request": {"all"},
	}
	resp = s.postForm("/roles", save)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	grants, scopes, err := s.st.RolePermissions(s.ctx, reviewerID)
	if err != nil {
		t.Fatal(err)
	}
	held := map[string]bool{}
	for _, g := range grants {
		held[g.Resource+":"+g.Action] = true
	}
	for _, want := range []string{
		"approval:approve", "approval:reject", "approval:return", "approval:reassign",
		"approval:accept_partial", "report:view", "request:view",
	} {
		if !held[want] {
			t.Fatalf("cell expansion did not grant %s (got %v)", want, grants)
		}
	}
	if len(grants) != 7 {
		t.Fatalf("saved %d grants, want exactly the 7 the cells and refinement cover: %v", len(grants), grants)
	}
	if len(scopes) != 1 || scopes[0].Resource != "request" || scopes[0].Scope != "all" {
		t.Fatalf("saved scopes = %v, want request=all", scopes)
	}
	role, err := s.st.Role(s.ctx, reviewerID)
	if err != nil || role.Description != "Reviews and approves" {
		t.Fatalf("role card did not save: %+v, %v", role, err)
	}

	// Clearing a cell revokes everything behind it: the same POST without
	// requests:approve leaves only what is still submitted.
	save.Del("cell")
	save.Add("cell", "reports:view")
	resp = s.postForm("/roles", save)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	grants, _, err = s.st.RolePermissions(s.ctx, reviewerID)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range grants {
		if g.Resource == "approval" {
			t.Fatalf("clearing the Approve cell left %s:%s behind", g.Resource, g.Action)
		}
	}
	if len(grants) != 2 {
		t.Fatalf("after clearing the cell: %v, want report:view + request:view", grants)
	}

	// A hand-crafted POST cannot invent a grant outside the vocabulary.
	resp = s.postForm("/roles", url.Values{
		"role_id": {strconvFormat(reviewerID)},
		"name":    {"Reviewer"},
		"perm":    {"payment:read"},
	})
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)

	// Copy the role.
	resp = s.postForm("/roles/"+strconvFormat(reviewerID)+"/copy", url.Values{"name": {"Reviewer Copy"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	// Delete the custom role; deleting a system role is rejected.
	resp = s.postForm("/roles/"+strconvFormat(reviewerID)+"/delete", nil)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if _, err := s.st.Role(s.ctx, reviewerID); !errorsIsNotFound(err) {
		t.Fatalf("Reviewer still present after delete: %v", err)
	}
	var adminID int64
	for _, r := range roles {
		if r.Name == "Admin" {
			adminID = r.ID
		}
	}
	resp = s.postForm("/roles/"+strconvFormat(adminID)+"/delete", nil)
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
}

func errorsIsNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }
```

*(Add `"errors"` to the app test imports if not already present.)*

And, because the role card must save, append the `UpdateRole` test to `internal/store/permissions_test.go`:

```go
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
```

### Step 2 — run it (expect failure)

```
go test ./internal/app/ -run 'TestRolesAdminScreenRendersApprovedMatrix|TestRolesAdminCreateEditCopyDelete' -v
go test ./internal/store/ -run TestUpdateRoleRenamesCustomRoles -v
```

Expected: FAIL — the `/roles` routes are not registered yet (Task 9 deliberately left them out), so requests 404; the handlers, the `"roles"` template and `store.UpdateRole` do not exist.

### Step 3 — minimal implementation

Add `UpdateRole` to `internal/store/permissions.go`:

```go
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
```

Add the handlers to `internal/app/app.go`. The view-model comes from Task 10 — there is no `buildRoleMatrix` here:

```go
func (a *App) rolesPage(w http.ResponseWriter, r *http.Request) {
	roles, err := a.st.AllRoles(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	selected := parseID(r.URL.Query().Get("role"))
	if selected == 0 && len(roles) > 0 {
		selected = roles[0].ID
	}
	role, err := a.st.Role(r.Context(), selected)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.respondStoreError(w, r, err)
		return
	}
	grants, scopes, err := a.st.RolePermissions(r.Context(), selected)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	counts := map[int64]int{}
	users, err := a.st.ListUsers(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	for _, u := range users {
		urs, err := a.st.UserRoles(r.Context(), u.ID)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		for _, ur := range urs {
			counts[ur.ID]++
		}
	}
	a.render(w, r, "roles", PageData{
		Title:          "Roles",
		Roles:          roles,
		Role:           role,
		PermColumns:    permColumns,
		PermMatrix:     buildPermMatrix(grants, scopes),
		RoleUserCounts: counts,
	})
}

// rolesSave persists the matrix. Grants are the union of the cells that were
// ticked — each expanding to every canonical action behind it — and the
// individual Advanced checkboxes. The union is order-independent, and it is
// what lets a cell be only partially granted. UpdateRolePermissions then
// replaces the role's whole grant set, so anything absent here is revoked.
func (a *App) rolesSave(w http.ResponseWriter, r *http.Request) {
	roleID := parseID(r.FormValue("role_id"))
	grants := expandCells(r.Form["cell"])
	seen := map[store.Grant]bool{}
	for _, g := range grants {
		seen[g] = true
	}
	for _, raw := range r.Form["perm"] {
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			continue
		}
		g := store.Grant{Resource: parts[0], Action: parts[1]}
		if seen[g] {
			continue
		}
		seen[g] = true
		grants = append(grants, g)
	}
	// The two matrices carry two scope radio groups. They must not share a name:
	// same-name radios are one group across the whole form, so enabling the
	// mobile set would silently clear the desktop selection. The desktop group
	// wins when it is enabled; the mobile group is the fallback. An empty value
	// is the explicit "None" state and stores no scope row at all.
	var scopes []store.ScopeGrant
	for _, res := range store.ResourceOrder() {
		if !store.IsScopedResource(res) {
			continue
		}
		v := strings.TrimSpace(r.FormValue("scope_" + res))
		if v == "" {
			v = strings.TrimSpace(r.FormValue("m_scope_" + res))
		}
		if v != "" {
			scopes = append(scopes, store.ScopeGrant{Resource: res, Scope: v})
		}
	}
	// Screen the grants before writing anything. UpdateRole and
	// UpdateRolePermissions are two transactions, so a grant rejected by the
	// second would otherwise leave the first one's rename committed. Every grant
	// is re-validated inside UpdateRolePermissions as well — this is a guard
	// against a half-applied save, not the security boundary.
	for _, g := range grants {
		if !store.ValidGrant(g.Resource, g.Action) {
			a.respondError(w, r, http.StatusBadRequest, "That permission does not exist.", nil)
			return
		}
	}
	if err := a.st.UpdateRole(r.Context(), auth.CurrentUser(r), roleID, r.FormValue("name"), r.FormValue("description")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if err := a.st.UpdateRolePermissions(r.Context(), auth.CurrentUser(r), roleID, grants, scopes); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles?role="+strconvFormatID(roleID), http.StatusSeeOther)
}

func (a *App) roleCreate(w http.ResponseWriter, r *http.Request) {
	id, err := a.st.CreateRole(r.Context(), auth.CurrentUser(r), r.FormValue("name"), r.FormValue("description"))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles?role="+strconvFormatID(id), http.StatusSeeOther)
}

func (a *App) roleCopy(w http.ResponseWriter, r *http.Request) {
	id, err := a.st.CopyRole(r.Context(), auth.CurrentUser(r), pathID(r), r.FormValue("name"))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles?role="+strconvFormatID(id), http.StatusSeeOther)
}

func (a *App) roleDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.st.DeleteRole(r.Context(), auth.CurrentUser(r), pathID(r)); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles", http.StatusSeeOther)
}

func strconvFormatID(id int64) string { return strconv.FormatInt(id, 10) }
```

Now that the handlers exist, register the Roles routes in `internal/app/app.go` `routes()` (these were deliberately omitted from Task 9). Add alongside the other admin routes:

```go
	mux.Handle("GET /roles", a.auth.RequirePermission("role", "view", http.HandlerFunc(a.rolesPage)))
	mux.Handle("POST /roles", a.auth.RequirePermission("role", "edit", http.HandlerFunc(a.withCSRF(a.rolesSave))))
	mux.Handle("POST /roles/new", a.auth.RequirePermission("role", "create", http.HandlerFunc(a.withCSRF(a.roleCreate))))
	mux.Handle("POST /roles/{id}/copy", a.auth.RequirePermission("role", "create", http.HandlerFunc(a.withCSRF(a.roleCopy))))
	mux.Handle("POST /roles/{id}/delete", a.auth.RequirePermission("role", "delete", http.HandlerFunc(a.withCSRF(a.roleDelete))))
```

Add the `PageData` fields in `internal/app/app.go`:

```go
	Roles          []store.Role
	Role           store.Role
	PermColumns    []permColumn
	PermMatrix     []permRow
	RoleUserCounts map[int64]int
	AllRoles       []store.Role             // used by Task 12
	UserRoleIDs    map[int64]map[int64]bool // used by Task 12
	Approvers      []store.User             // used by Task 12
	ApproverNames  map[int64]string         // used by Task 12
```

Replace the `"roles"` template in `internal/app/templates.go` with the approved screen. Two `{{define}}` helpers keep the twin matrices honest — `permscope` for the desktop group, `mpermscope` for the mobile one (different `name`, same state):

```
{{define "permscope"}}
<span class="perm-scope">
  <label class="{{if eq .Scope ""}}is-on{{end}}"><input type="radio" name="scope_{{.ScopeResource}}" value="" {{if eq .Scope ""}}checked{{end}}> None</label>
  <label class="{{if eq .Scope "own"}}is-on{{end}}"><input type="radio" name="scope_{{.ScopeResource}}" value="own" {{if eq .Scope "own"}}checked{{end}}> Own</label>
  <label class="{{if eq .Scope "assigned"}}is-on{{end}}"><input type="radio" name="scope_{{.ScopeResource}}" value="assigned" {{if eq .Scope "assigned"}}checked{{end}}> Assigned</label>
  <label class="{{if eq .Scope "all"}}is-on{{end}}"><input type="radio" name="scope_{{.ScopeResource}}" value="all" {{if eq .Scope "all"}}checked{{end}}> All</label>
</span>
{{end}}

{{define "mpermscope"}}
<span class="perm-scope">
  <label class="{{if eq .Scope ""}}is-on{{end}}"><input type="radio" name="m_scope_{{.ScopeResource}}" value="" disabled {{if eq .Scope ""}}checked{{end}}> None</label>
  <label class="{{if eq .Scope "own"}}is-on{{end}}"><input type="radio" name="m_scope_{{.ScopeResource}}" value="own" disabled {{if eq .Scope "own"}}checked{{end}}> Own</label>
  <label class="{{if eq .Scope "assigned"}}is-on{{end}}"><input type="radio" name="m_scope_{{.ScopeResource}}" value="assigned" disabled {{if eq .Scope "assigned"}}checked{{end}}> Assigned</label>
  <label class="{{if eq .Scope "all"}}is-on{{end}}"><input type="radio" name="m_scope_{{.ScopeResource}}" value="all" disabled {{if eq .Scope "all"}}checked{{end}}> All</label>
</span>
{{end}}

{{define "roles"}}
{{template "top" .}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">Access control</div>
    <h1>Roles &amp; permissions</h1>
    <p class="sub">Roles are data, not code. Create them, copy them, and set what each one may do and see.</p>
  </div>
  <div class="pb-actions">
    {{if .Perms.Can "role" "create"}}<button class="btn outline" type="button" data-open="role-copy">Copy this role</button>
    <button class="btn primary" type="button" data-open="role-new">＋ New role</button>{{end}}
  </div>
</section>

<div class="segmented">
  {{range .Roles}}<a class="{{if eq .ID $.Role.ID}}is-active{{end}}" href="/roles?role={{.ID}}">{{.Name}}{{if not .IsSystem}} <span class="n">custom</span>{{end}}</a>{{end}}
</div>

<form id="role-form" method="post" action="/roles">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <input type="hidden" name="role_id" value="{{.Role.ID}}">

  <div class="card">
    <div class="card-head">
      <h2>{{.Role.Name}}</h2>
      <span class="pill good">{{index .RoleUserCounts .Role.ID}} users</span>
      {{if and (not .Role.IsSystem) (.Perms.Can "role" "delete")}}<button class="btn small outline" type="button" data-open="role-delete">Delete role</button>{{end}}
    </div>
    <div class="card-body">
      <div class="form-grid">
        <div class="field span-4 m-half"><label for="rname">Role name</label><input id="rname" name="name" value="{{.Role.Name}}" required {{if .Role.IsSystem}}readonly{{end}}></div>
        <div class="field span-8"><label for="rdesc">Description</label><input id="rdesc" name="description" value="{{.Role.Description}}"></div>
      </div>
    </div>
  </div>

  <div class="table-wrap d-only">
    <table class="perm-table">
      <thead>
        <tr><th>Page</th>{{range .PermColumns}}<th class="c">{{.Label}}</th>{{end}}<th>Records it can see</th></tr>
      </thead>
      <tbody>
        {{range .PermMatrix}}
        <tr>
          <td class="perm-row-head">{{.Label}}</td>
          {{range .Cells}}<td class="c">{{if .Available}}<input type="checkbox" name="cell" value="{{.Value}}" data-cell="{{.Value}}" aria-label="{{.Label}}" {{if .Granted}}checked{{end}}>{{else}}<span class="muted">—</span>{{end}}</td>{{end}}
          <td>{{if .Scoped}}{{template "permscope" .}}{{else}}<span class="perm-scope"><label class="is-on">None</label></span>{{end}}</td>
        </tr>
        <tr class="perm-advanced">
          <td colspan="9">
            <details>
              <summary>Advanced — every permission behind this row</summary>
              <div class="row">{{range .Advanced}}<label class="checkline"><input type="checkbox" name="perm" value="{{.Resource}}:{{.Action}}" data-cell="{{.Cell}}" {{if .Granted}}checked{{end}}> {{.Label}}</label>{{end}}</div>
            </details>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>

  <div class="perm-acc m-only">
    {{range .PermMatrix}}
    <div class="pa-item">
      <button class="pa-head" type="button"><span class="chev">›</span><b>{{.Label}}</b><span class="n">{{.Held}} of {{.Total}}</span></button>
      <div class="pa-body">
        {{range .Cells}}{{if .Available}}<label class="checkline"><input type="checkbox" name="cell" disabled value="{{.Value}}" data-cell="{{.Value}}" {{if .Granted}}checked{{end}}> {{.Label}}</label>{{end}}{{end}}
        <details>
          <summary>Advanced</summary>
          {{range .Advanced}}<label class="checkline"><input type="checkbox" name="perm" disabled value="{{.Resource}}:{{.Action}}" data-cell="{{.Cell}}" {{if .Granted}}checked{{end}}> {{.Label}}</label>{{end}}
        </details>
        {{if .Scoped}}<div class="field"><span class="flabel">Records it can see</span>{{template "mpermscope" .}}</div>{{end}}
      </div>
    </div>
    {{end}}
  </div>

  <div class="action-bar">
    <span class="ab-note d-only">Changes apply to all {{index .RoleUserCounts .Role.ID}} users holding this role.</span>
    <span class="row-end"></span>
    <a class="btn outline" href="/roles?role={{.Role.ID}}">Discard</a>
    <button class="btn primary" type="submit">Save role</button>
  </div>
</form>

<div class="overlay" id="role-new" hidden>
  <div class="sheet">
    <form method="post" action="/roles/new">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="sh-head"><div><h2>New role</h2><p class="sh-sub">It starts with no permissions at all.</p></div><button class="sh-close" type="button" data-close="role-new">✕</button></div>
      <div class="sh-body stack-12">
        <div class="field"><label for="nr-name">Role name</label><input id="nr-name" name="name" required></div>
        <div class="field"><label for="nr-desc">Description</label><input id="nr-desc" name="description"></div>
      </div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="role-new">Cancel</button><span class="row-end"></span><button class="btn primary" type="submit">Create role</button></div>
    </form>
  </div>
</div>

<div class="overlay" id="role-copy" hidden>
  <div class="sheet">
    <form method="post" action="/roles/{{.Role.ID}}/copy">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="sh-head"><div><h2>Copy {{.Role.Name}}</h2><p class="sh-sub">The copy starts with the same permissions and data scope.</p></div><button class="sh-close" type="button" data-close="role-copy">✕</button></div>
      <div class="sh-body stack-12">
        <div class="field"><label for="cr-name">New role name</label><input id="cr-name" name="name" required></div>
      </div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="role-copy">Cancel</button><span class="row-end"></span><button class="btn primary" type="submit">Copy role</button></div>
    </form>
  </div>
</div>

{{if not .Role.IsSystem}}
<div class="overlay" id="role-delete" hidden>
  <div class="sheet">
    <form method="post" action="/roles/{{.Role.ID}}/delete">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="sh-head"><div><h2>Delete {{.Role.Name}}?</h2><p class="sh-sub">{{index .RoleUserCounts .Role.ID}} users hold this role and will lose everything it grants.</p></div><button class="sh-close" type="button" data-close="role-delete">✕</button></div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="role-delete">Cancel</button><span class="row-end"></span><button class="btn danger" type="submit">Delete role</button></div>
    </form>
  </div>
</div>
{{end}}
{{template "bottom" .}}
{{end}}
```

Notes on the markup:

- Every checkbox is a *cell* (`name="cell"`, value `<group>:<column>`) or an *Advanced* refinement (`name="perm"`, value `<resource>:<action>`). Both carry `data-cell` so the JS can keep them in step.
- The mobile accordion's inputs ship `disabled`; `fervid-app.js` hands over below 860 px. Without JavaScript the desktop table is authoritative at every width and scrolls inside `.table-wrap`.
- An unavailable cell renders `—` inside a `<span class="muted">`, never a disabled checkbox — a disabled checkbox reads as "off", which is a different claim.
- The three sheets sit outside `#role-form`: HTML forbids nested forms, and the create/copy/delete posts must not carry the matrix.
- System roles render their name `readonly`; `store.UpdateRole` rejects the rename server-side as well, because `readonly` is not validation.

Append the two rules the real radios need to `web/static/fervid-ds.css` (the mockup's scope pills are static labels; production needs inputs that a keyboard can reach):

```css
.perm-scope input { position: absolute; width: 1px; height: 1px; opacity: 0; }
.perm-scope label:has(input:focus-visible) { box-shadow: 0 0 0 2px var(--brand); }
```

Add the matrix hand-over and the cell/Advanced sync to `web/static/fervid-app.js`:

```js
// Roles matrix. Both renderings live in one form, so exactly one may submit:
// the desktop table is authoritative without JS, and below 860px we hand over
// to the accordion.
(function () {
  const form = document.getElementById('role-form');
  if (!form) return;
  const desktop = form.querySelector('.perm-table');
  const mobile = form.querySelector('.perm-acc');
  if (!desktop || !mobile) return;

  const narrow = window.matchMedia('(max-width: 860px)');
  const setDisabled = function (root, off) {
    root.querySelectorAll('input').forEach(function (input) { input.disabled = off; });
  };
  const handOver = function () {
    setDisabled(desktop, narrow.matches);
    setDisabled(mobile, !narrow.matches);
  };
  narrow.addEventListener('change', handOver);
  handOver();

  // A cell owns every canonical action behind it: toggling it toggles the
  // Advanced checkboxes it covers, so clearing a cell really does revoke.
  form.addEventListener('change', function (event) {
    const cell = event.target;
    if (!cell.matches('input[name="cell"]')) return;
    const covered = form.querySelectorAll('input[name="perm"][data-cell="' + CSS.escape(cell.value) + '"]');
    covered.forEach(function (box) { box.checked = cell.checked; });
  });
})();
```

### Step 4 — run to pass

```
go test ./internal/app/ -run 'TestRolesAdminScreenRendersApprovedMatrix|TestRolesAdminCreateEditCopyDelete' -v
go test ./internal/store/ -run TestUpdateRoleRenamesCustomRoles -v
```

Expected: PASS. Then check the screen by eye at 1440 px and 390 px: the desktop table scrolls inside `.table-wrap` rather than pushing the page sideways, the accordion replaces it below 860 px, and the action bar stays stuck to the bottom.

### Step 5 — commit

```
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go internal/store/permissions.go internal/store/permissions_test.go web/static/fervid-ds.css web/static/fervid-app.js
git commit -m "feat(app): roles admin screen on the approved design system

GET /roles renders the segmented role switcher, the role card, and the
9x7 permission matrix twice from one dataset — a desktop perm-table and a
mobile perm-acc, only one of which can ever submit. Cells expand to every
canonical action behind them, an Advanced disclosure keeps per-action
control, and data scope is a 3-state pill group with an explicit None.
UpdateRole saves the card and refuses to rename a system role. POST
create/save/copy/delete round it out; system roles stay delete-guarded.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 12 — Users screen on the design system: multi-role assignment + default approver (R4 UI; G9, G8 foundation)

Rebuilt against `mockups/screens/admin-users.html`. The screen is a `table.t-cards` — `data-label` on **every** cell so it restacks into labelled cards below 860 px, `.t-lead` on the name — with each user's roles shown as `.pill.neutral.no-dot` chips. Editing moves out of the table and into an `.overlay > .sheet` (`.sh-head` / `.sh-body` / `.sh-foot`) whose body carries the role `.checkline` list, the password reset and the active toggle. The inline per-row `<input>` grid is gone.

The screen also lands the **default approver** (G9): a `default_approver_id` on `users` that Phase 2 reads to pre-select the approver on a new request. Self-approval is impossible, so a user can never be their own default — enforced in the store, not just omitted from the `<select>` (G8's foundation; Phase 2 enforces the same rule on the request itself).

**Files**
- Modify: `internal/store/migrations.go` (extend the v1 `Up` with the `default_approver_id` column, guarded by `columnExists`)
- Modify: `internal/store/models.go` (`User.DefaultApproverID int64`), `internal/store/store.go` (`scanUser`, `ListUsers`, `UserByID`, `UserByEmail` select the new column)
- Modify: `internal/store/permissions.go` (`SetUserDefaultApprover`)
- Modify: `internal/app/app.go` (`users` handler loads roles + approver candidates; `userSave` persists `SetUserRoles` and the default approver)
- Modify: `internal/app/templates.go` (rebuilt `"users"` template)
- Test: `internal/app/app_integration_test.go`, `internal/store/permissions_test.go`

**Interfaces**
- Consumes: `store.AllRoles`, `store.UserRoles`, `store.SetUserRoles`, `store.ListUsers`.
- Produces (store): `SetUserDefaultApprover(ctx, actor User, userID, approverID int64) error`; `User.DefaultApproverID int64` (0 = none).
- Produces (app): `PageData.AllRoles`, `PageData.UserRoleIDs`, `PageData.Approvers`, `PageData.ApproverNames` populated by `users`; `userSave` calls `SetUserRoles` and `SetUserDefaultApprover` on the update path.

**Migration note.** The column belongs to Phase 1's single v1 migration (amendment A2 — v2 is Phase 1V's `vendors`). `columnExists` was built in Task 1 for exactly this and is otherwise unused. Re-read the amendment-log gotcha first: a database already stamped `user_version = 1` will not re-run v1.

### Step 1 — write the failing test

Append to `internal/app/app_integration_test.go`:

```go
func TestUsersScreenAssignsMultipleRoles(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	hash, err := auth.HashPassword("MemberPassword123")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := s.st.CreateUser(s.ctx, "member@example.test", "Member", hash, "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var requesterID, managerID int64
	for _, r := range roles {
		switch r.Name {
		case "Requester":
			requesterID = r.ID
		case "Manager":
			managerID = r.ID
		}
	}

	// The users page is the approved t-cards table with a role chip per role and
	// an editing sheet holding the role checklines.
	body := responseBody(t, s.request(http.MethodGet, "/users", nil, ""))
	for _, want := range []string{
		`class="t-cards"`, `class="t-lead" data-label="Name"`, `data-label="Email"`,
		`data-label="Roles"`, `data-label="Default approver"`, `data-label="Status"`,
		`class="pill neutral no-dot"`, `class="overlay"`, `class="sheet"`,
		`class="sh-head"`, `class="sh-body`, `class="sh-foot"`, `class="checkline"`,
		"role_ids", "default_approver_id", "Requester", "Manager",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("users page missing the approved markup %q", want)
		}
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("/users still renders the retired .badge class")
	}

	// Assign Requester + Manager to the member and give them a default approver.
	form := url.Values{
		"id":                  {strconvFormat(uid)},
		"name":                {"Member"},
		"email":               {"member@example.test"},
		"role":                {"data_entry"},
		"active":              {"on"},
		"role_ids":            {strconvFormat(requesterID), strconvFormat(managerID)},
		"default_approver_id": {strconvFormat(adminID)},
	}
	resp := s.postForm("/users", form)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	got, err := s.st.UserRoles(s.ctx, uid)
	if err != nil || len(got) != 2 {
		t.Fatalf("UserRoles after save = %+v, %v; want 2 roles", got, err)
	}
	// Union access: the member can now both create requests and approve.
	ps, _ := s.st.EffectivePermissions(s.ctx, uid)
	if !ps.Can("request", "create") || !ps.Can("approval", "approve") {
		t.Fatalf("union of Requester+Manager not effective: %+v", ps)
	}
	saved, err := s.st.UserByID(s.ctx, uid)
	if err != nil || saved.DefaultApproverID != adminID {
		t.Fatalf("default approver = %d, want %d (%v)", saved.DefaultApproverID, adminID, err)
	}

	// The member's own sheet must not offer the member as their own approver,
	// and a hand-crafted POST that tries it is rejected.
	if strings.Contains(body, `value="`+strconvFormat(uid)+`" data-approver-for="`+strconvFormat(uid)+`"`) {
		t.Fatal("the approver list offers a user as their own approver")
	}
	form.Set("default_approver_id", strconvFormat(uid))
	resp = s.postForm("/users", form)
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)
	saved, _ = s.st.UserByID(s.ctx, uid)
	if saved.DefaultApproverID != adminID {
		t.Fatalf("rejected self-approval still changed the stored value to %d", saved.DefaultApproverID)
	}
}
```

`adminID` is the bootstrap admin; fetch it at the top of the test alongside the other roles:

```go
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	adminID := admin.ID
```

Append the store-level rule to `internal/store/permissions_test.go`:

```go
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
```

And the schema assertion to `internal/store/migrations_test.go`:

```go
func TestMigrationV1AddsDefaultApproverColumn(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	got, err := columnExists(tx, "users", "default_approver_id")
	if err != nil || !got {
		t.Fatalf("columnExists(users, default_approver_id) = %v, %v; want true, nil", got, err)
	}
}
```

### Step 2 — run it (expect failure)

```
go test ./internal/app/ -run TestUsersScreenAssignsMultipleRoles -v
go test ./internal/store/ -run 'TestSetUserDefaultApprover|TestMigrationV1AddsDefaultApproverColumn' -v
```

Expected: FAIL — no `default_approver_id` column, no `SetUserDefaultApprover`, and the users page has neither role checkboxes nor the sheet.

### Step 3 — minimal implementation

**a. Schema.** Extend the v1 `Up` in `internal/store/migrations.go` one last time — after `seedSystemRoles` and `backfillUserRoles`:

```go
			if err := backfillUserRoles(tx); err != nil {
				return err
			}
			return addDefaultApproverColumn(tx)
```

```go
// addDefaultApproverColumn is additive and guarded by columnExists, so
// re-applying v1 to a database that already has the column is a no-op (G9).
// Phase 2 reads this column to pre-select the approver on a new request.
func addDefaultApproverColumn(tx *sql.Tx) error {
	has, err := columnExists(tx, "users", "default_approver_id")
	if err != nil || has {
		return err
	}
	_, err = tx.Exec(`ALTER TABLE users ADD COLUMN default_approver_id INTEGER REFERENCES users(id)`)
	return err
}
```

**b. Model.** Add `DefaultApproverID int64` to `User` in `internal/store/models.go` (0 means none), extend `scanUser` in `internal/store/store.go`, and add the column to the three `SELECT`s that feed it (`UserByEmail`, `UserByID`, `ListUsers`):

```go
func scanUser(scanner interface{ Scan(...any) error }) (User, error) {
	var u User
	var active int
	var approver sql.NullInt64
	err := scanner.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &active, &u.CreatedAt, &u.UpdatedAt, &approver)
	if err == sql.ErrNoRows {
		return u, ErrNotFound
	}
	u.Active = active == 1
	u.DefaultApproverID = approver.Int64
	return u, err
}
```

```sql
SELECT id,email,name,password_hash,role,active,created_at,updated_at,default_approver_id FROM users …
```

**c. The rule.** Append to `internal/store/permissions.go`:

```go
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
```

**d. Handler.** Extend `users` in `internal/app/app.go`:

```go
func (a *App) users(w http.ResponseWriter, r *http.Request) {
	users, err := a.st.ListUsers(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	roles, err := a.st.AllRoles(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	assigned := map[int64]map[int64]bool{}
	names := map[int64]string{}
	var approvers []store.User
	for _, u := range users {
		names[u.ID] = u.Name
		if u.Active {
			approvers = append(approvers, u)
		}
		urs, err := a.st.UserRoles(r.Context(), u.ID)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		set := map[int64]bool{}
		for _, ur := range urs {
			set[ur.ID] = true
		}
		assigned[u.ID] = set
	}
	a.render(w, r, "users", PageData{
		Title:         "Users",
		Users:         users,
		AllRoles:      roles,
		UserRoleIDs:   assigned,
		Approvers:     approvers,
		ApproverNames: names,
	})
}
```

In `userSave`, after a successful update (the `id != 0` branch), persist the role checkboxes and the default approver. Insert before the final `a.recordAudit(...)`/redirect, after the `if err != nil { a.respondStoreError(...) }` check on the create/update result. The create path (`id == 0`) relies on `CreateUser`'s default-role assignment (Task 8); both of these only apply to existing users:

```go
	if id != 0 {
		var roleIDs []int64
		for _, raw := range r.Form["role_ids"] {
			if v := parseID(raw); v != 0 {
				roleIDs = append(roleIDs, v)
			}
		}
		if err := a.st.SetUserRoles(r.Context(), auth.CurrentUser(r), id, roleIDs); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		if err := a.st.SetUserDefaultApprover(r.Context(), auth.CurrentUser(r), id, parseID(r.FormValue("default_approver_id"))); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
```

**e. Template.** Replace the `"users"` table and per-row inputs in `internal/app/templates.go` with the approved `t-cards` table plus one editing sheet per user:

```
{{define "users"}}
{{template "top" .}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">Access</div>
    <h1>Users</h1>
    <p class="sub">A person can hold several roles at once.</p>
  </div>
  <div class="pb-actions">{{if .Perms.Can "user" "create"}}<button class="btn primary" type="button" data-open="user-new">＋ Add user</button>{{end}}</div>
</section>

<div class="table-wrap">
  <table class="t-cards">
    <thead><tr><th>Name</th><th>Email</th><th>Roles</th><th>Default approver</th><th class="c">Status</th><th class="c">Edit</th></tr></thead>
    <tbody>
      {{range .Users}}
      <tr>
        <td class="t-lead" data-label="Name">{{.Name}}</td>
        <td data-label="Email">{{.Email}}</td>
        <td data-label="Roles">{{$uid := .ID}}{{range $.AllRoles}}{{if index (index $.UserRoleIDs $uid) .ID}}<span class="pill neutral no-dot">{{.Name}}</span> {{end}}{{end}}</td>
        <td data-label="Default approver">{{if .DefaultApproverID}}{{index $.ApproverNames .DefaultApproverID}}{{else}}—{{end}}</td>
        <td class="c" data-label="Status"><span class="pill {{if .Active}}good{{else}}neutral{{end}}">{{boolText .Active}}</span></td>
        <td class="c" data-label=""><button class="btn small outline" type="button" data-open="user-{{.ID}}">Edit</button></td>
      </tr>
      {{else}}
      <tr><td colspan="6" class="empty">No users yet.</td></tr>
      {{end}}
    </tbody>
  </table>
</div>

{{range .Users}}
<div class="overlay" id="user-{{.ID}}" hidden>
  <div class="sheet">
    <form method="post" action="/users">
      <input type="hidden" name="csrf" value="{{$.CSRF}}">
      <input type="hidden" name="id" value="{{.ID}}">
      <input type="hidden" name="email" value="{{.Email}}">
      <div class="sh-head"><div><h2>{{.Name}}</h2><p class="sh-sub">{{.Email}}</p></div><button class="sh-close" type="button" data-close="user-{{.ID}}">✕</button></div>
      <div class="sh-body stack-12">
        <div class="field"><label for="u-name-{{.ID}}">Name</label><input id="u-name-{{.ID}}" name="name" value="{{.Name}}" required></div>
        <div class="field"><span class="flabel">Roles</span>
          <div class="stack-8">{{$uid := .ID}}{{range $.AllRoles}}<label class="checkline"><input type="checkbox" name="role_ids" value="{{.ID}}" {{if index (index $.UserRoleIDs $uid) .ID}}checked{{end}}> {{.Name}}{{if .Description}} — {{.Description}}{{end}}</label>{{end}}</div>
        </div>
        <div class="field"><label for="u-apr-{{.ID}}">Default approver for their own requests</label>
          <select id="u-apr-{{.ID}}" name="default_approver_id">
            <option value="0">None</option>
            {{$self := .ID}}{{$chosen := .DefaultApproverID}}{{range $.Approvers}}{{if ne .ID $self}}<option value="{{.ID}}" data-approver-for="{{$self}}" {{if eq .ID $chosen}}selected{{end}}>{{.Name}}</option>{{end}}{{end}}
          </select>
          <span class="hint">Self-approval is not allowed, so they never appear in their own list.</span>
        </div>
        <div class="field"><label for="u-pw-{{.ID}}">Reset password</label><input id="u-pw-{{.ID}}" name="password" type="password" minlength="12" placeholder="Leave blank to keep the current one"></div>
        <input type="hidden" name="role" value="{{.Role}}">
        <label class="checkline"><input type="checkbox" name="active" {{check .Active}}> Active</label>
      </div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="user-{{.ID}}">Cancel</button><span class="row-end"></span><button class="btn primary" type="submit">Save user</button></div>
    </form>
  </div>
</div>
{{end}}

<div class="overlay" id="user-new" hidden>
  <div class="sheet">
    <form class="setup-form" method="post" action="/users">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="sh-head"><div><h2>Add user</h2><p class="sh-sub">They can be given more roles once they exist.</p></div><button class="sh-close" type="button" data-close="user-new">✕</button></div>
      <div class="sh-body stack-12">
        <div class="field"><label for="nu-email">Email</label><input id="nu-email" name="email" type="email" required></div>
        <div class="field"><label for="nu-name">Name</label><input id="nu-name" name="name" required></div>
        <div class="field"><label for="nu-role">Role</label><select id="nu-role" name="role"><option value="data_entry">Data entry</option><option value="admin">Admin</option></select></div>
        <div class="field"><label for="nu-pw">Password</label><input id="nu-pw" name="password" type="password" minlength="12" required></div>
        <label class="checkline"><input type="checkbox" name="active" checked> Active</label>
      </div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="user-new">Cancel</button><span class="row-end"></span><button class="btn primary" type="submit">Add User</button></div>
    </form>
  </div>
</div>
{{template "bottom" .}}
{{end}}
```

Notes on the markup:

- Every `<td>` carries `data-label`, including the trailing action cell (`data-label=""`), because `table.t-cards` uses that attribute to caption each field once the table restacks below 860 px.
- The create form keeps its **legacy Role `<select>` labelled "Role" and its "Add User" button** — Playwright fixtures (`createDataEntryUser`) and `regression-issues.spec.ts` drive `getByLabel('Role').selectOption(...)` and `getByRole('button', { name: 'Add User' })`. It has moved into a sheet, so those specs now click "＋ Add user" first; update that one line in the fixture, nothing else.
- The legacy `users.role` value rides along as a hidden input on the edit sheet: it is still what `UpdateUser` validates and what `RequireAnotherActiveAdmin` guards, and Phase 1 keeps it non-authoritative rather than removing it.
- The approver `<option>` list omits the user themselves; `SetUserDefaultApprover` rejects it again server-side.

### Step 4 — run to pass

```
go test ./internal/app/ -run 'TestUsersScreenAssignsMultipleRoles|TestUserCreationEnforcesPasswordAndInactiveFlagWithCreateAudit' -v
go test ./internal/store/ -v
```

Expected: PASS. The create-user flow and its audit entry are unchanged; multi-role assignment and the default approver persist; the whole store package is green with the new column.

### Step 5 — commit

```
git add internal/store/migrations.go internal/store/models.go internal/store/store.go internal/store/permissions.go internal/store/permissions_test.go internal/store/migrations_test.go internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): users screen on the design system with roles and default approver

The users table becomes a t-cards table with data-label on every cell and
role chips; editing moves into an overlay sheet carrying the role
checklines. Adds users.default_approver_id (G9) to migration v1, guarded by
columnExists, with SetUserDefaultApprover refusing to let anyone be their
own approver (G8) — enforced in the store, not only hidden from the list.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 13 — Retire the stub `PermissionSet` and prove server-side URL enforcement (R6; D2/Q5 mechanism)

**Reduced in scope by amendment A7.** Phase 0 builds the shell: `Shell`/`NavGroup`/`NavItem`/`TabBar`, `navSpec`, `buildShell`, `resolveTabs`, `PageData.Shell`, `PageData.Perms` populated in `renderStatus`, and the `top`/`bottom` templates that render them. **None of that is rebuilt here.** What is left is the hand-over: Phase 0 gates every nav item, tab and badge on the interim Casbin verbs (`request:approve`, `payment:read`, `budget:read`, `payment_attachment:create`, `recoverable:read`, `notification_rule:read`, …), and those pairs do not exist in the canonical vocabulary. Point the real engine at them unchanged and the whole sidebar silently empties. So this task:

1. re-points every gate onto the canonical vocabulary, with a guard test that fails if any gate drifts off it again;
2. deletes the Phase 0 stub (`staticPermissionSet`/`NewPermissionSet`) now that `EffectivePermissions` supplies the real thing behind the same interface (amendment A8);
3. keeps the R6 proof: a Requester-only session gets 403 on admin routes **by URL**, not by menu-hiding.

**Files**
- Modify: `internal/app/nav.go` (`navSpec`, `centreActions`, `resolveTabs` — resources and actions only; no structural change)
- Modify: `internal/store/badges.go` (the three badge specs' `Resource`/`Action`)
- Modify: `internal/store/permissions.go` (delete `staticPermissionSet`; re-implement `NewPermissionSet` over `dbPermissionSet`; export `AllGrants`)
- Modify: `internal/app/nav_test.go`, `internal/store/badges_test.go` (their fixtures move onto the exact-matching constructor)
- Modify: `internal/app/app_integration_test.go` (Requester-only 403 test **and** the `TestDataEntryHasRestrictedNavigationAndRoutes` rewrite deferred from Task 9)
- Create (optional): `tests/e2e/permissions.spec.ts`

**Interfaces**
- Consumes: `auth.Manager.Permissions(u)`, `store.EffectivePermissions`, `store.ValidGrant`.
- Produces: no new types. `PageData.Perms` and the shell already exist and are Phase 0's.

**The vocabulary hand-over, in full:**

| Gate | Phase 0 (interim) | Canonical |
|---|---|---|
| nav `approvals` | `request:approve` | `approval:approve` |
| nav `accounts-queue` | `request:pay` | `payment:process` |
| nav `recoverables` | `recoverable:read` | `recoverable_report:view` |
| nav `payments` | `payment:read` | `payment:view` |
| nav `variance-grid` | `grid:read` | `grid:view` |
| nav `budgets` | `budget:read` | `budget:view` |
| nav `monthly-plans` | `budget:read` | `month:view` |
| nav `reports` | `report:read` | `report:view` |
| nav `vendors` | `vendor:read` | `vendor:view` |
| nav `projects` / `heads` | `project:read` / `head:read` | `project:view` / `head:view` |
| nav `users` / `roles` | `user:read` / `role:read` | `user:view` / `role:view` |
| nav `configuration` | `config:read` | `config:view` |
| nav `notif-admin` | `notification_rule:read` | `notification:view` |
| nav `audit` / `backups` | `audit:read` / `backup:read` | `audit:view` / `backup:view` |
| tab bar centre action | `request:approve` | `approval:approve` |
| tab bar right slot | `payment:read` | `payment:view` |
| badge `my_payments` | `payment:read` | `payment:view` |
| badge `receipts_missing` | `payment_attachment:create` | `attachment:create` |
| badge `open_months` | `budget:read` | `month:view` |

`request:create` and `payment:create`, already canonical, are left alone.

### Step 1 — write the failing test

First the guard, which is what stops this from ever silently regressing. Append to `internal/app/nav_test.go`:

```go
func TestNavAndTabGatesUseTheCanonicalVocabulary(t *testing.T) {
	for _, group := range navSpec {
		for _, item := range group.Items {
			if item.Resource == "" && item.Action == "" {
				continue // available to every signed-in user
			}
			if !store.ValidGrant(item.Resource, item.Action) {
				t.Fatalf("nav item %q gates on %s:%s, which is not in the canonical vocabulary", item.Key, item.Resource, item.Action)
			}
		}
	}
	for _, candidate := range centreActions {
		if !store.ValidGrant(candidate.Resource, candidate.Action) {
			t.Fatalf("tab bar centre action %q gates on %s:%s, which is not canonical", candidate.Tab.Key, candidate.Resource, candidate.Action)
		}
	}
}
```

And to `internal/store/badges_test.go`:

```go
func TestBadgeSpecsUseTheCanonicalVocabulary(t *testing.T) {
	for _, spec := range badgeSpecs {
		if !ValidGrant(spec.Resource, spec.Action) {
			t.Fatalf("badge %q gates on %s:%s, which is not in the canonical vocabulary", spec.Key, spec.Resource, spec.Action)
		}
	}
}
```

Then rewrite `TestDataEntryHasRestrictedNavigationAndRoutes` in `internal/app/app_integration_test.go` — this modification was deferred from Task 9 because the shell only gates correctly once the hand-over above lands, so it is red until Step 3 goes green here. A data_entry user now resolves to the Accounts role: Accounts may view payments/reports but not admin screens, so the forbidden-route list stays valid:

```go
func TestDataEntryHasRestrictedNavigationAndRoutes(t *testing.T) {
	s := newAppTestServer(t)
	hash, err := auth.HashPassword("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.CreateUser(s.ctx, "entry@example.test", "Entry User", hash, "data_entry", true); err != nil {
		t.Fatal(err)
	}
	s.login("entry@example.test", "EntryPassword123")
	body := responseBody(t, s.request(http.MethodGet, "/", nil, ""))
	for _, hidden := range []string{"Monthly plans", "href=\"/projects\"", "href=\"/users\"", "href=\"/audit\"", "href=\"/roles\""} {
		if strings.Contains(body, hidden) {
			t.Fatalf("data-entry navigation exposes %q", hidden)
		}
	}
	// …and still offers everything the Accounts role does allow. The labels are
	// the Phase 0 shell's, which this task does not change.
	for _, shown := range []string{"Payments ledger", "Variance grid", "Reports"} {
		if !strings.Contains(body, shown) {
			t.Fatalf("data entry navigation is missing %q", shown)
		}
	}
	for _, path := range []string{"/projects", "/heads", "/users", "/audit", "/budgets", "/months", "/roles"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusForbidden)
		_ = responseBody(t, resp)
	}
}
```

Then append the definitive R6 test to `internal/app/app_integration_test.go`:

```go
func TestRequesterOnlySessionForbiddenFromAdminRoutesByURL(t *testing.T) {
	s := newAppTestServer(t)

	// Create a user and give them ONLY the Requester role (override the default
	// Accounts assignment) so we test a least-privilege session.
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("RequesterPass123")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := s.st.CreateUser(s.ctx, "requester@example.test", "Requester User", hash, "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var requesterID int64
	for _, r := range roles {
		if r.Name == "Requester" {
			requesterID = r.ID
		}
	}
	if err := s.st.SetUserRoles(s.ctx, admin, uid, []int64{requesterID}); err != nil {
		t.Fatal(err)
	}

	s.login("requester@example.test", "RequesterPass123")

	// GET admin/screen routes are blocked by URL (server-side, not menu-hiding).
	for _, path := range []string{"/roles", "/users", "/audit", "/projects", "/heads", "/budgets", "/months", "/payments", "/reports/monthly"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusForbidden)
		_ = responseBody(t, resp)
	}

	// An admin POST is blocked even with a valid CSRF token.
	resp := s.postForm("/roles/new", url.Values{"name": {"Sneaky"}})
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
	resp = s.postForm("/users", url.Values{"id": {"1"}, "name": {"x"}, "email": {s.cfg.AdminEmail}, "role": {"admin"}, "active": {"on"}})
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)

	// The nav for a Requester exposes none of the admin destinations.
	body := responseBody(t, s.request(http.MethodGet, "/", nil, ""))
	for _, hidden := range []string{"href=\"/roles\"", "href=\"/users\"", "href=\"/audit\"", "href=\"/projects\"", "href=\"/budgets\"", "Monthly plans"} {
		if strings.Contains(body, hidden) {
			t.Fatalf("Requester nav exposes %q", hidden)
		}
	}
}
```

### Step 2 — run it (expect failure)

```
go test ./internal/app/ -run 'TestNavAndTabGatesUseTheCanonicalVocabulary|TestRequesterOnlySessionForbiddenFromAdminRoutesByURL' -v
go test ./internal/store/ -run TestBadgeSpecsUseTheCanonicalVocabulary -v
```

Expected: FAIL — the guard tests report `request:approve`, `payment:read`, `budget:read` and friends as non-canonical, and with the DB engine now answering, an Accounts user's sidebar has lost the items those gates control.

### Step 3 — minimal implementation

**a. Re-point the gates.** Apply the hand-over table above to `navSpec`, `centreActions` and `resolveTabs` in `internal/app/nav.go`, and to `badgeSpecs` in `internal/store/badges.go`. Resources and actions only — no item is added, removed or reordered, and no structure changes:

```go
	{Title: "Requests", Items: []NavItem{
		{Key: "requests-list", Label: "My requests", Href: "/requests", Icon: "▤"},
		{Key: "approvals", Label: "Approvals", Href: "/approvals", Icon: "✓", Resource: "approval", Action: "approve", Badge: "approvals"},
		{Key: "accounts-queue", Label: "Accounts queue", Href: "/accounts-queue", Icon: "₹", Resource: "payment", Action: "process", Badge: "accounts_queue"},
		{Key: "recoverables", Label: "Recoverables", Href: "/recoverables", Icon: "↩", Resource: "recoverable_report", Action: "view"},
	}},
```

```go
	if can(perms, "payment", "view") {
		tabs.Right = append(tabs.Right, TabItem{Key: "payments", Label: "Payments", Href: "/payments", Icon: "▦"})
	} else {
		tabs.Right = append(tabs.Right, TabItem{Key: "variance-grid", Label: "Budget", Href: "/grid", Icon: "▥"})
	}
```

**b. Retire the stub.** In `internal/store/permissions.go`, delete `staticPermissionSet` and re-implement `NewPermissionSet` over the real type. The wildcard matching goes with it: `"*"` was a Casbin artefact, and a stale wildcard row is precisely the thing that would grant everything to everyone once roles became data.

```go
// NewPermissionSet builds a permission set from an explicit grant list. It is
// the constructor for fixtures and for any caller that already knows the exact
// grants; EffectivePermissions is what resolves them from the database. Matching
// is exact — there are no wildcards.
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
```

Update the two fixture call sites: `internal/app/nav_test.go` builds its admin set as `store.NewPermissionSet(store.AllGrants(), nil)` and its data-entry set from the explicit canonical grants the Accounts role holds; `internal/store/badges_test.go` does the same for the badge specs it exercises. Both stay unit tests — no database is required to test nav filtering.

**c. Nothing else.** `PageData.Perms`, `renderStatus` populating it, the sidebar, the tab bar, the More sheet and the `{{if .Perms.Can}}` gates that replaced `{{if eq .User.Role "admin"}}` are all Phase 0 Tasks 16–17 and are already in place. This task adds no template markup. If `grep -c 'eq .User.Role' internal/app/templates.go` is not `0` when you get here, that is a Phase 0 defect — fix it there, not with a second copy of the nav.

### Step 4 — run to pass

```
go test ./internal/app/ -run 'TestNavAndTabGates|TestRequesterOnlySessionForbiddenFromAdminRoutesByURL|TestDataEntryHasRestrictedNavigationAndRoutes' -v
go test ./internal/app/ -v
go test ./...
grep -rn 'NewPermissionSet(\[\]store.Grant{{Resource: "\*"' internal/ ; grep -rcn 'staticPermissionSet' internal/store/permissions.go
make test-race
make test-cover
```

Expected: PASS across the whole module — including `TestDataEntryHasRestrictedNavigationAndRoutes`, which was rewritten in Step 1 and goes green now that every gate speaks the canonical vocabulary. Both greps must come back empty/`0`: no wildcard permission fixture and no stub implementation survive. The pre-existing admin tests still pass because Admin holds every canonical action, so its nav and controls render exactly as before.

Optional e2e (`tests/e2e/permissions.spec.ts`) mirroring the design-system fixtures:

```ts
import { test, expect, login } from './fixtures';

test('requester-only session is denied admin routes by URL', async ({ page, runId }) => {
  await login(page, 'admin@fervid.local', 'admin123');
  // Create a user, then (in the app) assign only the Requester role via /users.
  const email = `${runId}-req@example.test`;
  await page.goto('/users');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Name').fill(`Requester ${runId}`);
  await page.getByLabel('Role').selectOption('data_entry');
  await page.getByLabel('Password').fill('StrongTestPassword!42');
  await page.getByRole('button', { name: 'Add User' }).click();
  await expect(page).toHaveURL(/\/users$/);

  await login(page, email, 'StrongTestPassword!42');
  await page.goto('/roles');
  await expect(page.getByRole('alert')).toContainText(/forbidden|permission/i);
  await expect(page.locator('.side-nav a[href="/users"]')).toHaveCount(0);
});
```

*(Run with `make test-e2e`. The user is created through the "＋ Add user" sheet from Task 12, then narrowed to Requester-only via the role checklines in that user's edit sheet — the seeded Accounts default would otherwise still let them reach payments.)*

### Step 5 — commit

```
git add internal/app/nav.go internal/app/nav_test.go internal/store/badges.go internal/store/badges_test.go internal/store/permissions.go internal/app/app_integration_test.go tests/e2e/permissions.spec.ts
git commit -m "feat(app): move every permission gate onto the DB engine (R6)

The Phase 0 shell gated nav items, tab bar and badges on interim Casbin
verbs; they now use the canonical vocabulary, with guard tests that fail if
any gate drifts off it. The stub staticPermissionSet is deleted and its
wildcard matching with it — NewPermissionSet builds the real type and
matches exactly. Integration tests prove a Requester-only session gets 403
on /roles, /users and admin POSTs by URL, and the deferred
TestDataEntryHasRestrictedNavigationAndRoutes rewrite goes green here.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Verification (phase acceptance)

Run the full gates before declaring Phase 1 done (overview §9 P1):

```
go vet ./...
make test-race
make test-cover
make test-e2e
```

All green; the four seeded roles reproduce the understanding-document behaviour; a Requester-only session cannot reach another user's data or any admin route by URL.

Plus the design-system gates this amendment adds:

```
grep -rn 'class="badge' internal/app/templates.go        # nothing on /roles or /users (D5)
grep -rn 'data-device' internal/app/templates.go web/static/fervid-ds.css   # nothing (D4)
```

and the two screens reviewed by eye at 1440 px and 390 px against `mockups/screens/admin-roles.html` and `mockups/screens/admin-users.html`: no horizontal page scroll, the roles matrix hands over to the accordion below 860 px, and the users table restacks into labelled cards.

---

## Coverage

Every Phase 1 matrix ID mapped to the task that implements it and the test that proves it.

| ID | Requirement | Task | Proving test |
|---|---|---|---|
| R1 | Admin-managed per-screen permissions, not hard-coded | 4, 9, 11 | `TestUpdateRolePermissionsPersistsAndValidates` (store) · `TestRolesAdminCreateEditCopyDelete` (app) · `TestSessionMiddlewareAndPermissions` (auth) |
| R2 | Granular actions per resource | 3, 4, 10 | `TestPermissionVocabularyIsCanonical` · `TestUpdateRolePermissionsPersistsAndValidates` · `TestPermMapCoversCanonicalVocabularyExactlyOnce` (the Advanced disclosure reaches every action a cell groups) |
| R3 | Data scope per resource (own/assigned/all) | 4, 7, 11 | `TestUpdateRolePermissionsPersistsAndValidates` · `TestEffectivePermissionsUnionAndBroadestScope` · `TestRolesAdminCreateEditCopyDelete` (the 3-state pill group with an explicit None) |
| R4 | Multiple roles per user; access = union | 6, 7, 12 | `TestSetUserRolesReplacesAssignment` · `TestEffectivePermissionsUnionAndBroadestScope` · `TestUsersScreenAssignsMultipleRoles` |
| R5 | Create role by copying an existing one | 5, 11 | `TestCopyRoleDuplicatesGrantsAndScopes` · `TestRolesAdminCreateEditCopyDelete` |
| R6 | Permissions govern data server-side (URL blocked, not menu-hide) | 9, 13 | `TestRequesterOnlySessionForbiddenFromAdminRoutesByURL` · `TestDataEntryHasRestrictedNavigationAndRoutes` |
| R7 | 4 starter roles seeded; Admin combines with any role | 8 | `TestSeedSystemRolesMatchDefaults` · `TestCreateUserAssignsDefaultRole` · `TestMigrationBackfillsExistingUsers` |
| R8 | Separate permission sets across all resources (§5 vocabulary) | 3, 8 | `TestPermissionVocabularyIsCanonical` · `TestSeedSystemRolesMatchDefaults` |
| R9 | Delete non-system roles; system roles protected | 3, 8, 11 | `TestCreateRoleAndListAndDeleteCustomRole` · `TestDeleteRoleRejectsSystemRole` · `TestUpdateRoleRenamesCustomRolesAndProtectsSystemNames` |
| C1 | Versioned migration runner | 1, 2 | `TestMigrateAppliesAndIsIdempotent` · `TestColumnExistsReportsSchemaShape` · `TestMigrationV1CreatesPermissionTables` |
| D2 | Roles matrix reconciles 9×7 presentation with the canonical vocabulary; enforcement unchanged | 3, 10, 11 | `TestPermMapCoversCanonicalVocabularyExactlyOnce` (nothing orphaned, nothing double-mapped) · `TestExpandCellsGrantsEveryCanonicalActionBehindTheCell` · `TestBuildPermMatrixMarksGrantedPartialAndUnavailableCells` (`—` for an unavailable cell) · `TestRolesAdminScreenRendersApprovedMatrix` |
| G9 | Default approver per employee | 12 | `TestSetUserDefaultApproverRejectsSelfAndUnknownUsers` · `TestUsersScreenAssignsMultipleRoles` · `TestMigrationV1AddsDefaultApproverColumn` |
| G8 (foundation) | Self-approval impossible — a user is never their own default approver | 12 | `TestSetUserDefaultApproverRejectsSelfAndUnknownUsers` · the rejected-POST assertion in `TestUsersScreenAssignsMultipleRoles` |
| D5 | `.badge` retired on every screen this phase touches | 11, 12 | `class="badge` assertions in `TestRolesAdminScreenRendersApprovedMatrix` and `TestUsersScreenAssignsMultipleRoles` |
| Q5/D2 mechanism | `own/assigned/all` scope resolution + permission-driven areas (consumed in P2) | 7, 9, 13 | `TestEffectivePermissionsUnionAndBroadestScope` (broadest scope) · `manager.Scope` assertion in `TestSessionMiddlewareAndPermissions` · nav assertions in `TestRequesterOnlySessionForbiddenFromAdminRoutesByURL` · `TestNavAndTabGatesUseTheCanonicalVocabulary` |
