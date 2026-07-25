# Phase 4 — Recoverable Payments Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

## Amendment log

Amended 2026-07-25 against `docs/superpowers/specs/2026-07-25-design-system-adoption-spec.md` §4 "Phase 4". Backend task bodies written before the design system existed are preserved verbatim; everything below is the delta.

| Ref | Change |
|---|---|
| §3 phase map | Migration renumbered **v4 → v5**. The sequence is now v1 RBAC, v2 vendors (Phase 1V), v3 requests (Phase 2), v4 payment-linking (Phase 3), **v5 recoverable categories**. |
| **D5** | Every `.badge` in this plan's templates becomes `.pill`. Phase 0 performs the global rename; no Phase-4 template may reintroduce `.badge`. |
| **D6** | The standalone `GET/POST /recoverable-categories` screen is **deleted**. Categories become one `<fieldset>` appended to the Phase-2-owned Configuration screen (`GET/POST /configuration`, permission `config`{view,edit}) — Task 11. The fieldset renders a `.t-cards` table (Category / Requires / Active / In use) plus a `.form-grid` add-row. "Requires" is one derived descriptive column, never raw `requires_project` / `requires_counterparty` checkboxes. |
| **G17** | New Task 10 — the recoverable **detail** screen `GET /recoverables/{id}`: `.req-head`, an overdue `.banner.warn`, `.card` + `.dl` detail blocks, the `.thread` timeline, a `.comment-box` and an `.action-bar`. No task in the original plan built it. |
| **G18** | `RecoverableRow` gains ageing (`DaysToReturn`, `Overdue`, `HasReturnDate`, `AgeingLabel`, `AgeingTone`) and `RecoverableReportOptions` gains `Counterparty`, `Ageing`, `Order` and an injected `AsOf` clock — Task 6. New Task 7 adds `RecoverableMetrics` (Outstanding · Past expected return · Due in 30 days · Paid out this month) and `RecoverableRollups` (the by-category and by-counterparty GROUP BY tables). New Task 8 renders the dashboard. |
| Routes | The family moves under one prefix: `GET /recoverables` (dashboard), `GET /recoverables/list` and `GET /recoverables/list.csv` (list + export, formerly `/reports/recoverable[.csv]`), `GET /recoverables/{id}` (detail). |
| UI contract | `table.t-cards` with `data-label` on **every** `<td>` (the mobile card-restack contract), `.pill.recoverable` for categories, `.pill.bad`/`.neutral`/`.approved`/`.hold` for ageing, `.m-filters` for the mobile filter strip, `<tfoot>` totals, and the two explainer banners — `.banner.brand` "Kept out of budget actuals on purpose" and `.banner.locked` for the out-of-scope note. |
| Task count | 9 → 13. Old Task 3→4, 4→5, 5→6, 6→9, 7→11, 8→12, 9→13. Tasks 3, 7, 8 and 10 are new. |

Two things the mockups show that the data model cannot supply are resolved here rather than invented at execution time:

- `recoverables-dashboard.html`'s by-counterparty **Type** column ("Government body", "Client", "Landlord", …) has no field behind it anywhere in the schema, and Phase 1V's `vendors` table does not cover employees or tender authorities. It is replaced by a **Categories** column — the distinct recoverable categories that counterparty holds, from `GROUP_CONCAT(DISTINCT rc.name)`.
- `admin-configuration.html`'s per-row **Requires** wording ("Employee, filled automatically") is sample copy keyed to the six seeded rows. The production column is derived from the two flags by `RecoverableCategory.Requires()` so it stays correct for categories an admin adds later; the employee auto-fill is a request-*type* rule (`type='employee_advance'` copies the requester into `counterparty`), already enforced in Task 4, not a category flag.

---

**Goal:** Add the recoverable-payment path: an admin-configurable `recoverable_categories` table (seeded) surfaced as a Configuration fieldset, per-category request validation, exclusion of recoverable payments from budget actuals, an aged recoverables register (dashboard → list → detail) with CSV export, and a regression guard that a recoverable request closes on payment while keeping its classification.

**Architecture:** A Phase-1-runner migration (v5) creates and seeds `recoverable_categories`. Store gains category CRUD plus usage counts, a `validateRecoverable` hook wired into Phase 2's request validation, a modified `Grid` (which `Report` reuses) that filters out payments whose linked request is recoverable, an aged `RecoverableReport` join query driven by an injected `AsOf` clock, and two GROUP BY rollups plus a four-metric summary. App gains four read/export routes (`/recoverables`, `/recoverables/list`, `/recoverables/list.csv`, `/recoverables/{id}`) and one `<fieldset>` appended to Phase 2's Configuration screen, all behind Phase-1 `RequirePermission`. Every screen is assembled from the Phase-0 component layer — no new markup is invented. Repayment tracking and forfeiture are deliberately not built.

**Tech Stack:** Go (`net/http` std ServeMux, `html/template`), `modernc.org/sqlite`, `internal/money`, existing `internal/store` + `internal/app` + `internal/auth`.

## Global Constraints

Copied verbatim from `2026-07-25-payment-requests-overview.md` and `2026-07-25-phase-4-recoverables-spec.md`. Every task implicitly includes these.

- **Currency:** ₹ INR stored as integer paise (`int64`), formatted via `internal/money` (`money.FormatPaise`).
- **Migration numbering:** contiguous, never reordered (v1=Phase 1, v2=Phase 1V vendors, v3=Phase 2 requests, v4=Phase 3 payment linking). Phase 4 registers exactly **migration v5** (`{Version: 5, Name: "recoverable_categories", Up: upRecoverableCategories}`) appended after Phase 3's v4. `recoverable_categories` is the FK target of `payment_requests.recoverable_category_id`, which Phase 2 declared as a deferred-safe FK (nullable, stays NULL until this v5 migration creates and seeds the table). New tables use `CREATE TABLE IF NOT EXISTS`; seeds are idempotent.
- **Store methods:** signature `func (s *Store) X(ctx, actor User, …)`; mutations use `s.db.BeginTx(ctx,nil)` + `defer tx.Rollback()` + `tx.Commit()`; every mutation writes `recordAuditTx(ctx, tx, AuditInput{…})`.
- **Errors:** reuse `ErrNotFound, ErrForbidden, ErrValidation, ErrDuplicate, ErrLockedMonth, ErrInactiveHead`; wrap validation as `fmt.Errorf("%w: message", ErrValidation)`; map DB errors via `classify`.
- **Handlers:** register in `App.routes` behind `a.auth.RequirePermission(resource, action, next)` (Phase 1 new vocabulary); POST handlers wrapped with `a.withCSRF`; render via templates in `internal/app/templates.go`; user-facing errors via `a.respondStoreError`/`a.respondError`.
- **Permission vocabulary (Phase 1, consumed):** `recoverable_category`{view, create, edit, delete}, `recoverable_report`{view, export}. Server-side enforcement (403 by URL), not menu-hiding.
- **Recoverable exclusion rule:** a payment whose linked request has `payment_requests.treatment='recoverable'` (joined via `payments.request_id`) is **never** summed into grid/report actuals. Historical (`request_id IS NULL`) and `treatment='budget'` payments count as before.
- **Out of scope (must remain absent):** recoverable *repayment* tracking (X2) and forfeiture/write-off — no columns, routes, or store methods.
- **Design system (Phase 0, consumed):** every screen is assembled from the ported component layer in `web/static/fervid-ds.css`. Phase 4 adds **no new CSS**. Classes used: `.page-banner`, `.banner.brand` / `.banner.warn` / `.banner.locked` with `.b-ico`, `.metric-strip` / `.metric` / `.metric-label` / `.metric-value` / `.metric-foot` / `.metric.warn`, `.section-head` / `.more-link`, `.toolbar` / `.field`, `.m-filters` / `.m-search` / `.filter-btn`, `table.t-cards` with `td.t-lead` and `td.c` and `<tfoot>`, `.bad-num`, `.pill` (+ `.recoverable`, `.bad`, `.neutral`, `.approved`, `.hold`, `.completed`, `.no-dot`), `.card` / `.card-head`, `.dl`, `.req-head` / `.rh-top` / `.rh-no` / `.rh-amt` / `.rh-meta` / `.rh-status`, `.thread` / `.tl-dot` / `.tl-head` / `.tl-body`, `.comment-box` / `.cb-actions`, `.action-bar` / `.ab-note`, `.form-grid` / `.field.span-N` / `.m-half`, `.checkline`, `.hint`, `.row-end`, `.empty`.
- **`.t-cards` contract:** every `<td>` in a `table.t-cards` carries a `data-label` attribute, including `<tfoot>` cells (`data-label=""` where the column has no mobile label). Below 860 px the CSS restacks rows into cards and reads that attribute for the field name — a missing `data-label` renders an unlabelled value on mobile. This is asserted by test, not by eye.
- **No `.badge`:** Phase 0 (D5) renamed `.badge` to `.pill` across every template. Phase-4 templates must not reintroduce it; `grep -c 'class="badge' internal/app/templates.go` stays at `0`.
- **Ageing is clock-injected:** every ageing computation takes an explicit `AsOf time.Time`; `time.Now()` is called only at the HTTP boundary. Tests pass fixed clocks so "25 days overdue" is deterministic.
- **Testing:** Go unit via `newTestStore(t)`; HTTP integration via `newAppTestServer(t)`. Every task is red→green→commit. Run `make test-race` and `make test-cover`.
- **Commits:** one per task, conventional messages. Stage only the files the task touched, by explicit path — the tree contains unrelated modified files, so never `git add -A`.

### Consumed contracts (from Phases 0–3; exact signatures relied upon)

Phase 2/3 specs are not yet written; these come from overview §8 + the §3 schema. If a neighboring phase used different field names, adapt at execution (see Assumptions/Risks in the return note).

- **Phase 0 (design system & shell):** `web/static/fervid-ds.css` carries the full component layer listed under Global Constraints; `internal/app/nav.go` defines `Shell`/`NavGroup`/`NavItem` and `PageData.Shell`; `buildShell(perms)` filters items by `perms.Can(resource, action)`. Phase 4 contributes **one** nav item — `{Key: "recoverables", Label: "Recoverables", Href: "/recoverables", Icon: "⟲", Resource: "recoverable_report", Action: "view"}` — to `navSpec`'s Reports group. It never renders `<aside>` or a tab bar itself.
- **Phase 1:** `internal/store/migrations.go` with `type migration struct { Version int; Name string; Up func(*sql.Tx) error }` and package var `migrations []migration`; `func columnExists(tx *sql.Tx, table, col string) (bool,error)`; `migrate` invoked in `store.Open`. `auth.Manager`: `RequirePermission(resource, action string, next http.Handler) http.Handler`. Permission-driven nav renders via `.Perms.Can "resource" "action"` — a `Can` method on `PageData.Perms` bound to the current user; there is no global `can` template FuncMap.
- **Phase 2 (requests):** table `payment_requests` (overview §3) with columns incl. `treatment`, `type`, `recoverable_category_id`, `project_id`, `head_id`, `counterparty`, `expected_return_date`, `repayment_notes`, `requester_id`, `manager_id`, `status`, `number`, `amount`, `approved_amount`, `approved_by`, `approved_at`, `processing_by`, `processing_at`, `submitted_at`. Type `RequestInput` with fields `Treatment, Type string; RecoverableCategoryID int64; ProjectID, HeadID int64; Amount int64; Purpose string; NeededBy string; VendorPayee, Counterparty string; ExpectedReturnDate, RepaymentNotes string; Urgent bool; ManagerID int64`. Method `CreateRequest(ctx, actor User, in RequestInput) (int64, error)` (and `UpdateRequest`) whose validation Phase 4 extends. Type `store.Request` (full field list in the Phase-2 plan) exposing at least `ID, Number, Status, Treatment, Type, RecoverableCategoryID *int64, Project, Head, Amount, Purpose, Counterparty, ExpectedReturnDate, RepaymentNotes, RequesterName, ApprovedAmount *int64, OnHold, SubmittedAt`; `Request(ctx, id) (Request, error)`; `RequestComments(ctx, requestID) ([]RequestComment, error)`; `RequestAttachments(ctx, requestID) ([]RequestAttachment, error)`; route `POST /requests/{id}/comment` behind `request`{comment}; PageData fields `Request2 store.Request`, `Comments []store.RequestComment`, `Audit []store.AuditEntry`.
- **Phase 2 (Configuration screen, D6):** `GET /configuration` renders the `configuration` template behind `RequirePermission("config","view")`; `POST /configuration` saves behind `config`{edit} and is wrapped in `a.withCSRF`. The screen is one `<form>` of `<fieldset>` blocks, each with a `<legend>` and a `.form-grid` body, closed by a shared sticky `.action-bar`. `PageData.Config` carries the generic `app_settings`-backed values. Phases 3, 4 and 5 each **append one fieldset** to that template and each add their own handler branch; none of them registers a route. Phase 4 appends the "Recoverable categories" fieldset (Task 11) and a dedicated sub-form `POST /configuration/recoverable-categories` so a category save never round-trips the whole configuration form.
- **Phase 3 (payments):** `payments.request_id INTEGER` (NULL for historical), `payments.settlement TEXT`, `payments.partial_reason TEXT`; `RecordPaymentForRequest(ctx, actor User, requestID int64, in PaymentInput, settlement string, partialReason string, attachment *AttachmentInput) (int64, error)` which, with `settlement="settled"`, sets the request `status='completed'`; `PaymentForRequest(ctx, requestID int64) (Payment, error)` returning `ErrNotFound` when the request has no linked payment, with `Payment` exposing `PaidOn, Amount, PaymentMode, ReferenceNo, EnteredByName`.
- **Existing store (pre-Phase-1):** `Audit(ctx, entityType string, entityID int64, limit int) ([]AuditEntry, error)` — the source of the `.thread` timeline on the detail screen.

---

### Task 1: Migration v5 — `recoverable_categories` table + seed

**Files:**
- Create: `internal/store/recoverables.go`
- Modify: `internal/store/migrations.go` (append v5 to the `migrations` slice)
- Test: `internal/store/recoverables_test.go`

**Interfaces:**
- Consumes: Phase 1 `migration` struct + `migrations` slice + `migrate` in `store.Open`.
- Produces: `func upRecoverableCategories(tx *sql.Tx) error`; table `recoverable_categories`; six seeded rows; `PRAGMA user_version` advances to ≥ 5.
- Note: `recoverable_categories` (created here in Version 5) is the FK target that Phase 2's `payment_requests.recoverable_category_id` declared as deferred-safe — the column is nullable and stays NULL until this v5 migration creates and seeds the table.

- [ ] **Step 1: Write the failing tests**

Create `internal/store/recoverables_test.go` (Task 2 will add `context` and `errors` to this import block when its tests need them):

```go
package store

import (
	"path/filepath"
	"testing"
)

func TestMigrationV5SeedsRecoverableCategories(t *testing.T) {
	s := newTestStore(t)
	var version int
	if err := s.DB().QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version < 5 {
		t.Fatalf("user_version = %d, want >= 5", version)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM recoverable_categories`).Scan(&count); err != nil {
		t.Fatalf("recoverable_categories table missing: %v", err)
	}
	if count != 6 {
		t.Fatalf("seeded categories = %d, want 6", count)
	}
	checks := []struct {
		name     string
		proj, cp bool
	}{
		{"Employee advance", false, false}, {"EMD", true, false}, {"PBG", true, false},
		{"ICD", false, true}, {"Security deposit", false, false}, {"Other", false, false},
	}
	for _, c := range checks {
		var proj, cp int
		err := s.DB().QueryRow(`SELECT requires_project, requires_counterparty FROM recoverable_categories WHERE name=?`, c.name).Scan(&proj, &cp)
		if err != nil {
			t.Fatalf("category %q missing: %v", c.name, err)
		}
		if (proj == 1) != c.proj || (cp == 1) != c.cp {
			t.Fatalf("%s flags = proj:%d cp:%d, want proj:%v cp:%v", c.name, proj, cp, c.proj, c.cp)
		}
	}
}

func TestMigrationV5IsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotent.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := Open(path) // re-open an already-migrated database
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	var count int
	if err := s2.DB().QueryRow(`SELECT COUNT(*) FROM recoverable_categories`).Scan(&count); err != nil {
		t.Fatalf("count after re-open: %v", err)
	}
	if count != 6 {
		t.Fatalf("after re-open categories = %d, want 6 (no duplicates)", count)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestMigrationV5' -v`
Expected: FAIL — `no such table: recoverable_categories` (migration v5 not yet registered).

- [ ] **Step 3: Create the migration up-function**

Create `internal/store/recoverables.go`:

```go
package store

import "database/sql"

// upRecoverableCategories is migration v5: it creates the admin-configurable
// recoverable_categories table and seeds the six default categories. It is
// idempotent — re-running on a migrated database inserts nothing new.
func upRecoverableCategories(tx *sql.Tx) error {
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS recoverable_categories (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  requires_project INTEGER NOT NULL DEFAULT 0,
  requires_counterparty INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_recoverable_categories_name_nocase ON recoverable_categories(lower(name));`); err != nil {
		return err
	}
	seed := []struct {
		name             string
		proj, cp, sort   int
	}{
		{"Employee advance", 0, 0, 1},
		{"EMD", 1, 0, 2},
		{"PBG", 1, 0, 3},
		{"ICD", 0, 1, 4},
		{"Security deposit", 0, 0, 5},
		{"Other", 0, 0, 6},
	}
	for _, c := range seed {
		if _, err := tx.Exec(`INSERT INTO recoverable_categories(name,requires_project,requires_counterparty,active,sort_order)
			SELECT ?,?,?,1,? WHERE NOT EXISTS(SELECT 1 FROM recoverable_categories WHERE lower(name)=lower(?))`,
			c.name, c.proj, c.cp, c.sort, c.name); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Register migration v5**

In `internal/store/migrations.go`, append the v5 entry to the `migrations` slice literal, immediately after the Phase 3 (v4) entry:

```go
var migrations = []migration{
	// … v1 permissions (Phase 1), v2 vendors (Phase 1V), v3 requests (Phase 2), v4 payment linking (Phase 3) …
	{Version: 5, Name: "recoverable_categories", Up: upRecoverableCategories},
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestMigrationV5' -v`
Expected: PASS — `--- PASS: TestMigrationV5SeedsRecoverableCategories`, `--- PASS: TestMigrationV5IsIdempotent`, `ok  	fervidbudget/internal/store`.

- [ ] **Step 6: Commit**

```bash
git add internal/store/recoverables.go internal/store/migrations.go internal/store/recoverables_test.go
git commit -m "feat(store): add recoverable_categories migration v5 with seeded defaults"
```

---

### Task 2: `RecoverableCategory` type + CRUD store methods

**Files:**
- Modify: `internal/store/models.go` (add `RecoverableCategory`)
- Modify: `internal/store/recoverables.go` (add `ListRecoverableCategories`, `UpsertRecoverableCategory`)
- Test: `internal/store/recoverables_test.go`

**Interfaces:**
- Consumes: `boolInt`, `classify`, `recordAuditTx`, `ErrValidation`, `ErrDuplicate`, `AuditInput`, `User` (all existing in `internal/store`).
- Produces:
  - `type RecoverableCategory struct { ID int64; Name string; RequiresProject, RequiresCounterparty, Active bool; SortOrder int; CreatedAt time.Time }`
  - `func (s *Store) ListRecoverableCategories(ctx context.Context, activeOnly bool) ([]RecoverableCategory, error)`
  - `func (s *Store) UpsertRecoverableCategory(ctx context.Context, actor User, id int64, name string, requiresProject, requiresCounterparty, active bool, sortOrder int) (int64, error)`

- [ ] **Step 1: Write the failing test**

Update the `internal/store/recoverables_test.go` import block to add `"context"` and `"errors"` (now `"context"`, `"errors"`, `"path/filepath"`, `"testing"`), then append:

```go
func TestRecoverableCategoryCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)

	id, err := s.UpsertRecoverableCategory(ctx, actor, 0, "Retention money", true, false, true, 7)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == 0 {
		t.Fatal("expected a new category id")
	}
	cats, err := s.ListRecoverableCategories(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var found *RecoverableCategory
	for i := range cats {
		if cats[i].ID == id {
			found = &cats[i]
		}
	}
	if found == nil || !found.RequiresProject || found.RequiresCounterparty || !found.Active {
		t.Fatalf("stored category = %+v", found)
	}
	if _, err := s.UpsertRecoverableCategory(ctx, actor, 0, "retention money", false, false, true, 8); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate name = %v, want %v", err, ErrDuplicate)
	}
	if _, err := s.UpsertRecoverableCategory(ctx, actor, id, "Retention money", true, false, false, 7); err != nil {
		t.Fatalf("update: %v", err)
	}
	active, err := s.ListRecoverableCategories(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range active {
		if c.ID == id {
			t.Fatal("deactivated category still listed as active")
		}
	}
	if _, err := s.UpsertRecoverableCategory(ctx, actor, 0, "  ", false, false, true, 9); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank name = %v, want %v", err, ErrValidation)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store/ -run TestRecoverableCategoryCRUD -v`
Expected: FAIL (build) — `undefined: (*Store).UpsertRecoverableCategory`, `undefined: (*Store).ListRecoverableCategories`, `undefined: RecoverableCategory`.

- [ ] **Step 3: Add the `RecoverableCategory` type**

In `internal/store/models.go`, after the `Head` struct, add:

```go
type RecoverableCategory struct {
	ID                   int64
	Name                 string
	RequiresProject      bool
	RequiresCounterparty bool
	Active               bool
	SortOrder            int
	CreatedAt            time.Time
}
```

- [ ] **Step 4: Implement the CRUD methods**

In `internal/store/recoverables.go`, update the import block and add the methods:

```go
import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)
```

```go
func (s *Store) ListRecoverableCategories(ctx context.Context, activeOnly bool) ([]RecoverableCategory, error) {
	q := `SELECT id,name,requires_project,requires_counterparty,active,sort_order,created_at FROM recoverable_categories`
	if activeOnly {
		q += ` WHERE active=1`
	}
	q += ` ORDER BY sort_order,name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecoverableCategory
	for rows.Next() {
		var c RecoverableCategory
		var rp, rc, active int
		if err := rows.Scan(&c.ID, &c.Name, &rp, &rc, &active, &c.SortOrder, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.RequiresProject, c.RequiresCounterparty, c.Active = rp == 1, rc == 1, active == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) UpsertRecoverableCategory(ctx context.Context, actor User, id int64, name string, requiresProject, requiresCounterparty, active bool, sortOrder int) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("%w: recoverable category name is required", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var savedID int64
	action := "update"
	if id == 0 {
		res, err := tx.ExecContext(ctx, `INSERT INTO recoverable_categories(name,requires_project,requires_counterparty,active,sort_order) VALUES(?,?,?,?,?)`,
			name, boolInt(requiresProject), boolInt(requiresCounterparty), boolInt(active), sortOrder)
		if err != nil {
			return 0, classify(err)
		}
		if savedID, err = res.LastInsertId(); err != nil {
			return 0, err
		}
		action = "create"
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE recoverable_categories SET name=?, requires_project=?, requires_counterparty=?, active=?, sort_order=? WHERE id=?`,
			name, boolInt(requiresProject), boolInt(requiresCounterparty), boolInt(active), sortOrder, id); err != nil {
			return 0, classify(err)
		}
		savedID = id
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: action, EntityType: "recoverable_category", EntityID: &savedID, Summary: "Saved recoverable category " + name}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return savedID, nil
}
```

Note: `database/sql` is still needed by `upRecoverableCategories`; keep it in the import block.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/store/ -run TestRecoverableCategoryCRUD -v`
Expected: PASS — `--- PASS: TestRecoverableCategoryCRUD`, `ok  	fervidbudget/internal/store`.

- [ ] **Step 6: Commit**

```bash
git add internal/store/models.go internal/store/recoverables.go internal/store/recoverables_test.go
git commit -m "feat(store): add recoverable category CRUD (List/Upsert) with audit"
```

---

### Task 3: Category usage counts + the derived "Requires" label (D6)

The Configuration fieldset (Task 11) shows one descriptive **Requires** column and an **In use** count. Neither is derivable in a template from the two booleans without embedding presentation logic in HTML, and the count needs its own query. Both land in the store, tested, before any screen consumes them.

**Files:**
- Modify: `internal/store/models.go` (add `RecoverableCategoryUsage`)
- Modify: `internal/store/recoverables.go` (add `RecoverableCategory.Requires`, `ListRecoverableCategoriesWithUsage`)
- Test: `internal/store/recoverables_test.go`

**Interfaces:**
- Consumes: `RecoverableCategory` (Task 2); `payment_requests.recoverable_category_id` (Phase 2).
- Produces:
  - `func (c RecoverableCategory) Requires() string` — "Nothing extra" · "Related project" · "Counterparty company" · "Related project and counterparty company"
  - `type RecoverableCategoryUsage struct { RecoverableCategory; InUse int }`
  - `func (s *Store) ListRecoverableCategoriesWithUsage(ctx context.Context) ([]RecoverableCategoryUsage, error)` — every category, active or not, in `sort_order,name` order, each with the number of `payment_requests` rows referencing it.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/recoverables_test.go`:

```go
func TestRecoverableCategoryRequiresLabel(t *testing.T) {
	cases := []struct {
		name     string
		cat      RecoverableCategory
		want     string
	}{
		{"neither", RecoverableCategory{Name: "Other"}, "Nothing extra"},
		{"project", RecoverableCategory{Name: "EMD", RequiresProject: true}, "Related project"},
		{"counterparty", RecoverableCategory{Name: "ICD", RequiresCounterparty: true}, "Counterparty company"},
		{"both", RecoverableCategory{Name: "Retention", RequiresProject: true, RequiresCounterparty: true}, "Related project and counterparty company"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cat.Requires(); got != c.want {
				t.Fatalf("Requires() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestListRecoverableCategoriesWithUsageCountsRequests(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	var emdID, icdID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM recoverable_categories WHERE name='EMD'`).Scan(&emdID); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM recoverable_categories WHERE name='ICD'`).Scan(&icdID); err != nil {
		t.Fatal(err)
	}
	for i, catID := range []int64{emdID, emdID, icdID} {
		if _, err := s.DB().ExecContext(ctx, `INSERT INTO payment_requests
			(number,status,treatment,type,recoverable_category_id,head_id,amount,purpose,counterparty,expected_return_date,repayment_notes,requester_id,manager_id,submitted_at)
			VALUES(?, 'pending','recoverable','recoverable',?,?,?, 'Deposit','Counterparty','2027-03-31','Refund later',?,?,CURRENT_TIMESTAMP)`,
			fmt.Sprintf("PR-2026-%06d", 300+i), catID, headID, int64(100000), actor.ID, actor.ID); err != nil {
			t.Fatal(err)
		}
	}
	usage, err := s.ListRecoverableCategoriesWithUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 6 {
		t.Fatalf("categories = %d, want the 6 seeded", len(usage))
	}
	counts := map[string]int{}
	for _, u := range usage {
		counts[u.Name] = u.InUse
	}
	if counts["EMD"] != 2 {
		t.Fatalf("EMD in use = %d, want 2", counts["EMD"])
	}
	if counts["ICD"] != 1 {
		t.Fatalf("ICD in use = %d, want 1", counts["ICD"])
	}
	if counts["Other"] != 0 {
		t.Fatalf("Other in use = %d, want 0", counts["Other"])
	}
	if usage[0].Name != "Employee advance" {
		t.Fatalf("first row = %q, want the lowest sort_order (Employee advance)", usage[0].Name)
	}
	if usage[1].Requires() != "Related project" {
		t.Fatalf("EMD Requires() = %q, want Related project", usage[1].Requires())
	}
}
```

Add `"fmt"` to the `internal/store/recoverables_test.go` import block (now `"context"`, `"errors"`, `"fmt"`, `"path/filepath"`, `"testing"`).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestRecoverableCategoryRequiresLabel|TestListRecoverableCategoriesWithUsageCountsRequests' -v`
Expected: FAIL (build) — `c.cat.Requires undefined`, `s.ListRecoverableCategoriesWithUsage undefined`, `undefined: RecoverableCategoryUsage`.

- [ ] **Step 3: Add the usage type**

In `internal/store/models.go`, immediately after `RecoverableCategory`, add:

```go
// RecoverableCategoryUsage is a category plus the number of payment requests that
// reference it. Used by the Configuration screen's "In use" column so an admin can
// see what deactivating a category would strand.
type RecoverableCategoryUsage struct {
	RecoverableCategory
	InUse int
}
```

- [ ] **Step 4: Implement the label and the usage query**

In `internal/store/recoverables.go`, add:

```go
// Requires renders the two per-category field rules as one phrase for the
// Configuration screen. Presentation lives here, not in the template, so a
// category an admin adds later describes itself correctly with no template edit.
func (c RecoverableCategory) Requires() string {
	switch {
	case c.RequiresProject && c.RequiresCounterparty:
		return "Related project and counterparty company"
	case c.RequiresProject:
		return "Related project"
	case c.RequiresCounterparty:
		return "Counterparty company"
	default:
		return "Nothing extra"
	}
}

func (s *Store) ListRecoverableCategoriesWithUsage(ctx context.Context) ([]RecoverableCategoryUsage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.name,c.requires_project,c.requires_counterparty,c.active,c.sort_order,c.created_at,
		(SELECT COUNT(*) FROM payment_requests pr WHERE pr.recoverable_category_id=c.id)
		FROM recoverable_categories c ORDER BY c.sort_order,c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecoverableCategoryUsage
	for rows.Next() {
		var u RecoverableCategoryUsage
		var rp, rc, active int
		if err := rows.Scan(&u.ID, &u.Name, &rp, &rc, &active, &u.SortOrder, &u.CreatedAt, &u.InUse); err != nil {
			return nil, err
		}
		u.RequiresProject, u.RequiresCounterparty, u.Active = rp == 1, rc == 1, active == 1
		out = append(out, u)
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestRecoverableCategoryRequiresLabel|TestListRecoverableCategoriesWithUsageCountsRequests' -v`
Expected: PASS — all four `TestRecoverableCategoryRequiresLabel/*` subtests PASS and `--- PASS: TestListRecoverableCategoriesWithUsageCountsRequests`.

- [ ] **Step 6: Commit**

```bash
git add internal/store/models.go internal/store/recoverables.go internal/store/recoverables_test.go
git commit -m "feat(store): add recoverable category usage counts and derived Requires label"
```

---

### Task 4: Recoverable request validation (`validateRecoverable` + wiring)

**Files:**
- Modify: `internal/store/recoverables.go` (add `validateRecoverable`, `recoverableCategory` helper)
- Modify: `internal/store/requests.go` (Phase 2 — call the hook in `CreateRequest`/`UpdateRequest`)
- Test: `internal/store/recoverables_test.go`

**Interfaces:**
- Consumes: `RequestInput` (Phase 2), `RecoverableCategory` (Task 2), `validDate`, `ErrValidation`, `ErrNotFound`.
- Produces:
  - `func validateRecoverable(in RequestInput, cat RecoverableCategory) error`
  - `func (s *Store) recoverableCategory(ctx context.Context, id int64) (RecoverableCategory, error)`
  - A validation branch in `CreateRequest`/`UpdateRequest` invoked when `in.Treatment == "recoverable"`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/recoverables_test.go`:

```go
func TestValidateRecoverable(t *testing.T) {
	emd := RecoverableCategory{ID: 2, Name: "EMD", RequiresProject: true}
	icd := RecoverableCategory{ID: 4, Name: "ICD", RequiresCounterparty: true}
	emp := RecoverableCategory{ID: 1, Name: "Employee advance"}
	base := func() RequestInput {
		return RequestInput{
			Treatment: "recoverable", Type: "recoverable",
			RecoverableCategoryID: 2, ProjectID: 10,
			ExpectedReturnDate: "2027-03-31", RepaymentNotes: "Refund at project close",
		}
	}
	t.Run("valid_baseline", func(t *testing.T) { // T1: recoverable branch accepted when all rules met
		if err := validateRecoverable(base(), emd); err != nil {
			t.Fatalf("valid EMD rejected: %v", err)
		}
	})
	t.Run("missing_category", func(t *testing.T) { // V1
		in := base()
		in.RecoverableCategoryID = 0
		if err := validateRecoverable(in, emd); !errors.Is(err, ErrValidation) {
			t.Fatalf("missing category = %v", err)
		}
	})
	t.Run("emd_requires_project", func(t *testing.T) { // V5
		in := base()
		in.ProjectID = 0
		if err := validateRecoverable(in, emd); !errors.Is(err, ErrValidation) {
			t.Fatalf("EMD without project = %v", err)
		}
	})
	t.Run("icd_requires_counterparty", func(t *testing.T) { // V5
		in := base()
		in.RecoverableCategoryID, in.ProjectID, in.Counterparty = 4, 0, ""
		if err := validateRecoverable(in, icd); !errors.Is(err, ErrValidation) {
			t.Fatalf("ICD without counterparty = %v", err)
		}
	})
	t.Run("requires_return_date_and_notes", func(t *testing.T) { // V6
		in := base()
		in.ExpectedReturnDate = ""
		if err := validateRecoverable(in, emd); !errors.Is(err, ErrValidation) {
			t.Fatalf("missing return date = %v", err)
		}
		in = base()
		in.RepaymentNotes = "   "
		if err := validateRecoverable(in, emd); !errors.Is(err, ErrValidation) {
			t.Fatalf("missing repayment notes = %v", err)
		}
	})
	t.Run("employee_advance_project_optional", func(t *testing.T) { // T9
		in := base()
		in.Type, in.RecoverableCategoryID, in.ProjectID = "employee_advance", 1, 0
		if err := validateRecoverable(in, emp); err != nil {
			t.Fatalf("employee advance without project rejected: %v", err)
		}
	})
}

func TestCreateRecoverableRequestEnforcesCategoryRules(t *testing.T) { // V1 end-to-end
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)
	var emdID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM recoverable_categories WHERE name='EMD'`).Scan(&emdID); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateRequest(ctx, actor, RequestInput{
		Treatment: "recoverable", Type: "recoverable", RecoverableCategoryID: emdID,
		Amount: 500000, Purpose: "Tender EMD", ExpectedReturnDate: "2027-03-31",
		RepaymentNotes: "Refund on award", ManagerID: actor.ID, // EMD without a project
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("EMD without project via CreateRequest = %v, want validation error", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestValidateRecoverable|TestCreateRecoverableRequestEnforcesCategoryRules' -v`
Expected: FAIL (build) — `undefined: validateRecoverable`.

- [ ] **Step 3: Implement `validateRecoverable` and the category loader**

In `internal/store/recoverables.go` add the following (the import block already has `context`, `database/sql`, `fmt`, `strings` from Task 2 — no import change needed):

```go
// validateRecoverable enforces the per-category field rules for a request whose
// treatment is "recoverable" (overview §6). cat is the resolved category.
func validateRecoverable(in RequestInput, cat RecoverableCategory) error {
	if in.RecoverableCategoryID == 0 {
		return fmt.Errorf("%w: a recoverable category is required", ErrValidation)
	}
	if cat.RequiresProject && in.ProjectID == 0 {
		return fmt.Errorf("%w: this recoverable category requires a project", ErrValidation)
	}
	if cat.RequiresCounterparty && strings.TrimSpace(in.Counterparty) == "" {
		return fmt.Errorf("%w: this recoverable category requires a counterparty", ErrValidation)
	}
	if !validDate(in.ExpectedReturnDate) {
		return fmt.Errorf("%w: an expected return date is required", ErrValidation)
	}
	if strings.TrimSpace(in.RepaymentNotes) == "" {
		return fmt.Errorf("%w: repayment notes are required", ErrValidation)
	}
	return nil
}

func (s *Store) recoverableCategory(ctx context.Context, id int64) (RecoverableCategory, error) {
	if id == 0 {
		return RecoverableCategory{}, fmt.Errorf("%w: a recoverable category is required", ErrValidation)
	}
	var c RecoverableCategory
	var rp, rc, active int
	err := s.db.QueryRowContext(ctx, `SELECT id,name,requires_project,requires_counterparty,active,sort_order,created_at FROM recoverable_categories WHERE id=?`, id).
		Scan(&c.ID, &c.Name, &rp, &rc, &active, &c.SortOrder, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.RequiresProject, c.RequiresCounterparty, c.Active = rp == 1, rc == 1, active == 1
	return c, nil
}
```

- [ ] **Step 4: Wire the hook into Phase 2 request validation**

In `internal/store/requests.go`, inside `CreateRequest` (and `UpdateRequest`), immediately after the base field validation and before the row is inserted, add:

```go
if in.Treatment == "recoverable" {
	cat, err := s.recoverableCategory(ctx, in.RecoverableCategoryID)
	if err != nil {
		return 0, err
	}
	if in.Type == "employee_advance" && strings.TrimSpace(in.Counterparty) == "" {
		in.Counterparty = actor.Name // requester auto-recorded (overview §6)
	}
	if err := validateRecoverable(in, cat); err != nil {
		return 0, err
	}
}
```

(Ensure `strings` is imported in `requests.go`. `UpdateRequest` returns `error` not `(int64,error)` — use `return err` there instead of `return 0, err`.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestValidateRecoverable|TestCreateRecoverableRequestEnforcesCategoryRules' -v`
Expected: PASS — all `TestValidateRecoverable/*` subtests PASS and `--- PASS: TestCreateRecoverableRequestEnforcesCategoryRules`.

- [ ] **Step 6: Commit**

```bash
git add internal/store/recoverables.go internal/store/requests.go internal/store/recoverables_test.go
git commit -m "feat(store): enforce per-category recoverable request validation"
```

---

### Task 5: Exclude recoverable payments from Grid + Report actuals

**Files:**
- Modify: `internal/store/store.go` (`Grid` query only)
- Test: `internal/store/recoverables_test.go`

**Interfaces:**
- Consumes: `Grid`, `Report`, `CreatePayment`, `SetBudget`; `payment_requests` + `payments.request_id` (Phases 2/3).
- Produces: modified `Grid` SQL; a package-private test helper `seedRecoverablePayment`.

- [ ] **Step 1: Write the failing test + helper**

Append to `internal/store/recoverables_test.go`:

```go
// seedRecoverablePayment inserts a completed recoverable request and its single
// linked (settled, non-voided) payment directly, exercising the Grid/Report and
// RecoverableReport joins without depending on the Phase 2/3 Go API. projectID
// of 0 leaves the recoverable unlinked from any real project.
func seedRecoverablePayment(t *testing.T, s *Store, ctx context.Context, actor User, headID, projectID int64, number, paidOn string, amount int64) (int64, int64) {
	t.Helper()
	var catID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM recoverable_categories WHERE name='Security deposit'`).Scan(&catID); err != nil {
		t.Fatalf("lookup category: %v", err)
	}
	var proj any
	if projectID != 0 {
		proj = projectID
	}
	res, err := s.DB().ExecContext(ctx, `INSERT INTO payment_requests
		(number,status,treatment,type,recoverable_category_id,project_id,head_id,amount,purpose,counterparty,expected_return_date,repayment_notes,requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?, 'completed','recoverable','recoverable',?,?,?,?, 'Security deposit','Acme Landlord','2027-12-31','Refund at lease end',?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		number, catID, proj, headID, amount, actor.ID, actor.ID, amount, actor.ID)
	if err != nil {
		t.Fatalf("insert recoverable request: %v", err)
	}
	reqID, _ := res.LastInsertId()
	res2, err := s.DB().ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,entered_by,request_id,settlement) VALUES(?,?,?,?,?,?, 'settled')`,
		headID, paidOn, amount, "Acme Landlord", actor.ID, reqID)
	if err != nil {
		t.Fatalf("insert recoverable payment: %v", err)
	}
	payID, _ := res2.LastInsertId()
	return reqID, payID
}

func TestGridAndReportExcludeRecoverablePayments(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	if err := s.SetBudget(ctx, actor, headID, "2026-08", 1000000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-08-05", Amount: 300000, VendorPayee: "Budget Vendor"}); err != nil {
		t.Fatal(err)
	}
	seedRecoverablePayment(t, s, ctx, actor, headID, 0, "PR-2026-000001", "2026-08-06", 700000)

	grid, err := s.Grid(ctx, "2026-08", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if grid.Total.Actual != 300000 {
		t.Fatalf("grid actual = %d, want 300000 (recoverable excluded)", grid.Total.Actual)
	}
	rows, err := s.Report(ctx, "2026-08", "2026-08", "heads")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Actual != 300000 {
		t.Fatalf("report rows = %+v, want single row with actual 300000", rows)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store/ -run TestGridAndReportExcludeRecoverablePayments -v`
Expected: FAIL — `grid actual = 1000000, want 300000` (the recoverable payment is currently summed into actuals).

- [ ] **Step 3: Modify the `Grid` query**

In `internal/store/store.go`, replace the query inside `Grid` (currently `SELECT p.id,…ORDER BY p.sort_order,p.name,h.sort_order,h.name`) with the version below. Two changes: the actual `SUM(CASE …)` and the head-activity `EXISTS` both exclude recoverable-linked payments via a `payment_requests` join/filter.

```go
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.name,h.id,h.name,h.active,p.active,COALESCE(h.due_day,''),COALESCE(b.amount,0),
		COALESCE(SUM(CASE WHEN py.voided_at IS NULL AND COALESCE(pr.treatment,'') <> 'recoverable' THEN py.amount ELSE 0 END),0)
		FROM heads h JOIN projects p ON p.id=h.project_id
		LEFT JOIN budgets b ON b.head_id=h.id AND b.month=?
		LEFT JOIN payments py ON py.head_id=h.id AND substr(py.paid_on,1,7)=?
		LEFT JOIN payment_requests pr ON pr.id=py.request_id
		WHERE (h.active=1 AND p.active=1)
			OR b.id IS NOT NULL
			OR EXISTS(SELECT 1 FROM payments px
				LEFT JOIN payment_requests pxr ON pxr.id=px.request_id
				WHERE px.head_id=h.id AND substr(px.paid_on,1,7)=?
					AND COALESCE(pxr.treatment,'') <> 'recoverable')
		GROUP BY p.id,p.name,h.id,h.name,h.active,p.active,h.due_day,b.amount
		ORDER BY p.sort_order,p.name,h.sort_order,h.name`, month, month, month)
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/store/ -run TestGridAndReportExcludeRecoverablePayments -v`
Expected: PASS — `--- PASS: TestGridAndReportExcludeRecoverablePayments`.

- [ ] **Step 5: Verify no regression in existing grid/report tests**

Run: `go test ./internal/store/ -run 'TestHistoricalGridAndReportsIncludeRetiredHeads|TestCreateMonthPlanCopiesOnlyActiveHeads' -v`
Expected: PASS (historical `request_id IS NULL` payments still count — the `COALESCE(pr.treatment,'')<>'recoverable'` filter keeps them).

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/recoverables_test.go
git commit -m "feat(store): exclude recoverable payments from grid and report actuals"
```

---

### Task 6: `RecoverableReport` — aged register, ordering and ageing filter (G18)

The original query INNER JOINed `payments`, so a recoverable that is approved but not yet paid could not appear at all. `recoverables-list.html` shows exactly such a row ("Not yet paid"), and the dashboard's Outstanding metric counts it. The register therefore LEFT JOINs payments and is scoped by request status, not by payment existence.

**Files:**
- Modify: `internal/store/models.go` (add `RecoverableRow`, `RecoverableReportOptions`)
- Modify: `internal/store/recoverables.go` (add `RecoverableReport`, `recoverableAgeing`)
- Test: `internal/store/recoverables_test.go`

**Interfaces:**
- Consumes: `payment_requests`, `payments`, `recoverable_categories`, `projects`, `users`; `validMonth`; `seedRecoverablePayment` (Task 5).
- Produces:
  - `type RecoverableRow struct { RequestID int64; Number, Category, Counterparty, Project string; Amount int64; PaidOn, ExpectedReturnDate, RepaymentNotes, Status, Requester string; OnHold, HasReturnDate, Overdue bool; DaysToReturn int; AgeingLabel, AgeingTone string }`
  - `type RecoverableReportOptions struct { From, To string; CategoryID int64; Counterparty, Query, Ageing, Order string; AsOf time.Time }`
  - `func (s *Store) RecoverableReport(ctx context.Context, opts RecoverableReportOptions) ([]RecoverableRow, error)`
  - `func recoverableAgeing(paidOn, expectedReturn string, onHold bool, days int) (label, tone string, overdue bool)`

**Semantics fixed here (the screens and Task 7 depend on all four):**

1. **Base set** — `treatment='recoverable'` and `status NOT IN ('rejected','cancelled','withdrawn')`. A request that died before any money left is not outstanding money. Any further terminal-without-payment status Phase 2 introduces must be added to this list.
2. **Payment join** — `LEFT JOIN payments py ON py.request_id=pr.id AND py.voided_at IS NULL`. `Amount` is the paid amount when paid, otherwise the approved amount, otherwise the requested amount. `PaidOn` is `""` when unpaid.
3. **Date range** — `From`/`To` filter on `paid_on` and are **optional**. When both are blank there is no date restriction and unpaid rows are included; when a range is given, only rows paid inside it appear (unpaid rows have no `paid_on` and are excluded). The register screens pass no range; the CSV export and the month-scoped tests pass one.
4. **Ageing** — measured in whole calendar days from `AsOf` to `expected_return_date` via `julianday`. `AsOf` is required for determinism; a zero value falls back to `time.Now().UTC()` at the single point where the option struct is normalised.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/recoverables_test.go`:

```go
func TestRecoverablePaymentExcludedFromActualsButInRecoverableReport(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	if err := s.SetBudget(ctx, actor, headID, "2026-09", 1000000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-09-03", Amount: 250000, VendorPayee: "Budget Vendor"}); err != nil {
		t.Fatal(err)
	}
	seedRecoverablePayment(t, s, ctx, actor, headID, 0, "PR-2026-000009", "2026-09-04", 800000)

	grid, err := s.Grid(ctx, "2026-09", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if grid.Total.Actual != 250000 {
		t.Fatalf("grid actual = %d, want 250000 (recoverable excluded)", grid.Total.Actual)
	}
	report, err := s.Report(ctx, "2026-09", "2026-09", "heads")
	if err != nil {
		t.Fatal(err)
	}
	if len(report) != 1 || report[0].Actual != 250000 {
		t.Fatalf("report actual = %+v, want 250000", report)
	}
	rec, err := s.RecoverableReport(ctx, RecoverableReportOptions{From: "2026-09", To: "2026-09"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec) != 1 {
		t.Fatalf("recoverable report rows = %d, want 1", len(rec))
	}
	if rec[0].Amount != 800000 || rec[0].Category != "Security deposit" || rec[0].Counterparty != "Acme Landlord" || rec[0].Status != "completed" {
		t.Fatalf("recoverable row = %+v", rec[0])
	}
}

func TestRecoverableReportShowsLinkedProject(t *testing.T) { // V8
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	var projectID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	seedRecoverablePayment(t, s, ctx, actor, headID, projectID, "PR-2026-000021", "2026-10-01", 500000)
	rec, err := s.RecoverableReport(ctx, RecoverableReportOptions{From: "2026-10", To: "2026-10"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec) != 1 || rec[0].Project == "" {
		t.Fatalf("recoverable row missing linked project: %+v", rec)
	}
}

// seedRecoverableRequestOnly inserts a recoverable request with no linked payment,
// so the register can be tested for rows that are approved but not yet paid.
func seedRecoverableRequestOnly(t *testing.T, s *Store, ctx context.Context, actor User, headID int64, number, status, category, counterparty, expectedReturn string, amount int64, onHold bool) int64 {
	t.Helper()
	var catID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM recoverable_categories WHERE name=?`, category).Scan(&catID); err != nil {
		t.Fatalf("lookup category %q: %v", category, err)
	}
	res, err := s.DB().ExecContext(ctx, `INSERT INTO payment_requests
		(number,status,treatment,type,recoverable_category_id,head_id,amount,approved_amount,purpose,counterparty,expected_return_date,repayment_notes,requester_id,manager_id,on_hold,submitted_at)
		VALUES(?,?, 'recoverable','recoverable',?,?,?,?, 'Deposit',?,?, 'Refund on close',?,?,?,CURRENT_TIMESTAMP)`,
		number, status, catID, headID, amount, amount, counterparty, expectedReturn, actor.ID, actor.ID, boolInt(onHold))
	if err != nil {
		t.Fatalf("insert recoverable request %s: %v", number, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestRecoverableAgeingLabels(t *testing.T) {
	cases := []struct {
		name                  string
		paidOn, expected      string
		onHold                bool
		days                  int
		wantLabel, wantTone   string
		wantOverdue           bool
	}{
		{"on_hold_wins", "2026-02-12", "2026-06-30", true, -25, "On hold", "hold", false},
		{"unpaid", "", "2026-11-30", false, 128, "Awaiting payment", "approved", false},
		{"no_fixed_date", "2026-01-30", "", false, 0, "No fixed date", "neutral", false},
		{"overdue", "2026-02-12", "2026-06-30", false, -25, "25 days overdue", "bad", true},
		{"due_today", "2026-02-12", "2026-07-25", false, 0, "Due today", "neutral", false},
		{"future", "2026-04-03", "2027-01-15", false, 174, "174 days to go", "neutral", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			label, tone, overdue := recoverableAgeing(c.paidOn, c.expected, c.onHold, c.days)
			if label != c.wantLabel || tone != c.wantTone || overdue != c.wantOverdue {
				t.Fatalf("recoverableAgeing = (%q,%q,%v), want (%q,%q,%v)", label, tone, overdue, c.wantLabel, c.wantTone, c.wantOverdue)
			}
		})
	}
}

func TestRecoverableReportIncludesUnpaidAndExcludesDeadRequests(t *testing.T) { // G18
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	seedRecoverablePayment(t, s, ctx, actor, headID, 0, "PR-2026-000200", "2026-05-10", 400000)
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000201", "approved", "EMD", "Ridge Metro tender authority", "2026-11-30", 200000, false)
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000202", "rejected", "EMD", "Nobody", "2026-11-30", 999999, false)
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000203", "cancelled", "ICD", "Nobody", "2026-11-30", 999999, false)

	asOf := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	rows, err := s.RecoverableReport(ctx, RecoverableReportOptions{AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("register rows = %d, want 2 (paid + unpaid; rejected and cancelled excluded)", len(rows))
	}
	byNumber := map[string]RecoverableRow{}
	for _, r := range rows {
		byNumber[r.Number] = r
	}
	unpaid, ok := byNumber["PR-2026-000201"]
	if !ok {
		t.Fatalf("unpaid recoverable missing from the register: %+v", rows)
	}
	if unpaid.PaidOn != "" || unpaid.Amount != 200000 || unpaid.AgeingTone != "approved" {
		t.Fatalf("unpaid row = %+v, want empty PaidOn, approved amount, approved tone", unpaid)
	}
	if unpaid.RequestID == 0 {
		t.Fatal("RecoverableRow.RequestID must be populated so the list can link to the detail screen")
	}
	// A date range still scopes to paid rows only.
	scoped, err := s.RecoverableReport(ctx, RecoverableReportOptions{From: "2026-05", To: "2026-05", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 1 || scoped[0].Number != "PR-2026-000200" {
		t.Fatalf("range-scoped rows = %+v, want only the May payment", scoped)
	}
}

func TestRecoverableReportAgeingFilterAndOrdering(t *testing.T) { // G18
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	asOf := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000301", "completed", "EMD", "Coastal Power", "2026-06-30", 200000, false)  // 25 days overdue
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000302", "completed", "PBG", "Ridge Metro", "2026-08-10", 300000, false)    // due in 16 days
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000303", "completed", "ICD", "Harith Infra", "2027-01-15", 100000, false)   // later
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000304", "completed", "Security deposit", "Whitefield", "", 65000, false)   // no fixed date

	all, err := s.RecoverableReport(ctx, RecoverableReportOptions{AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("rows = %d, want 4", len(all))
	}
	if all[0].Number != "PR-2026-000301" || !all[0].Overdue || all[0].DaysToReturn != -25 {
		t.Fatalf("default order must put the overdue row first with DaysToReturn -25: %+v", all[0])
	}
	if all[1].Number != "PR-2026-000302" || all[3].Number != "PR-2026-000304" {
		t.Fatalf("default order = %s,%s,%s,%s; want soonest expected return first and no-date last",
			all[0].Number, all[1].Number, all[2].Number, all[3].Number)
	}

	overdue, err := s.RecoverableReport(ctx, RecoverableReportOptions{Ageing: "overdue", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(overdue) != 1 || overdue[0].Number != "PR-2026-000301" {
		t.Fatalf("ageing=overdue rows = %+v, want only PR-2026-000301", overdue)
	}
	due30, err := s.RecoverableReport(ctx, RecoverableReportOptions{Ageing: "due30", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(due30) != 1 || due30[0].Number != "PR-2026-000302" {
		t.Fatalf("ageing=due30 rows = %+v, want only PR-2026-000302", due30)
	}
	later, err := s.RecoverableReport(ctx, RecoverableReportOptions{Ageing: "later", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(later) != 2 {
		t.Fatalf("ageing=later rows = %d, want 2 (the 2027 date and the undated one)", len(later))
	}

	byAmount, err := s.RecoverableReport(ctx, RecoverableReportOptions{Order: "amount", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if byAmount[0].Number != "PR-2026-000302" || byAmount[0].Amount != 300000 {
		t.Fatalf("order=amount first row = %+v, want the ₹3,000 row", byAmount[0])
	}
	byCounterparty, err := s.RecoverableReport(ctx, RecoverableReportOptions{Counterparty: "Harith Infra", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(byCounterparty) != 1 || byCounterparty[0].Number != "PR-2026-000303" {
		t.Fatalf("counterparty filter = %+v, want only PR-2026-000303", byCounterparty)
	}
}
```

Add `"time"` to the `internal/store/recoverables_test.go` import block (now `"context"`, `"errors"`, `"fmt"`, `"path/filepath"`, `"testing"`, `"time"`).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestRecoverablePaymentExcludedFromActualsButInRecoverableReport|TestRecoverableReportShowsLinkedProject|TestRecoverableAgeingLabels|TestRecoverableReportIncludesUnpaidAndExcludesDeadRequests|TestRecoverableReportAgeingFilterAndOrdering' -v`
Expected: FAIL (build) — `undefined: (*Store).RecoverableReport`, `undefined: RecoverableReportOptions`, `undefined: RecoverableRow`, `undefined: recoverableAgeing`.

- [ ] **Step 3: Add the report types**

In `internal/store/models.go`, after `ReportRow`, add:

```go
// RecoverableRow is one line of the outstanding-recoverables register. Ageing
// fields are computed against RecoverableReportOptions.AsOf, never time.Now(),
// so both the screens and their tests are deterministic.
type RecoverableRow struct {
	RequestID          int64
	Number             string
	Category           string
	Counterparty       string
	Project            string
	Amount             int64
	PaidOn             string // "" when approved but not yet paid
	ExpectedReturnDate string
	RepaymentNotes     string
	Status             string
	Requester          string
	OnHold             bool
	HasReturnDate      bool
	Overdue            bool
	DaysToReturn       int    // whole calendar days from AsOf; negative when overdue
	AgeingLabel        string // "25 days overdue", "174 days to go", "Awaiting payment", …
	AgeingTone         string // pill modifier: "bad" | "neutral" | "approved" | "hold"
}

type RecoverableReportOptions struct {
	From         string // optional YYYY-MM; filters on paid_on
	To           string // optional YYYY-MM
	CategoryID   int64
	Counterparty string
	Query        string
	Ageing       string    // "" | "overdue" | "due30" | "later" | "unpaid"
	Order        string    // "" (overdue first, then soonest return) | "amount" | "paid_on" | "number"
	AsOf         time.Time // zero → time.Now().UTC() at normalisation
}
```

- [ ] **Step 4: Implement `RecoverableReport` and the ageing helper**

In `internal/store/recoverables.go`, add `"time"` to the import block and add:

```go
// recoverableAgeing turns a row's payment state and its distance from the expected
// return date into the pill text and pill modifier the register renders. days is
// whole calendar days from "now" to the expected return date (negative = overdue).
func recoverableAgeing(paidOn, expectedReturn string, onHold bool, days int) (label, tone string, overdue bool) {
	switch {
	case onHold:
		return "On hold", "hold", false
	case paidOn == "":
		return "Awaiting payment", "approved", false
	case expectedReturn == "":
		return "No fixed date", "neutral", false
	case days < 0:
		return fmt.Sprintf("%d days overdue", -days), "bad", true
	case days == 0:
		return "Due today", "neutral", false
	default:
		return fmt.Sprintf("%d days to go", days), "neutral", false
	}
}

func (s *Store) RecoverableReport(ctx context.Context, opts RecoverableReportOptions) ([]RecoverableRow, error) {
	asOf := opts.AsOf
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	today := asOf.UTC().Format("2006-01-02")

	// Dead requests never held money, so they are not outstanding recoverables.
	q := `SELECT pr.id, pr.number, COALESCE(rc.name,''), COALESCE(pr.counterparty,''), COALESCE(p.name,''),
		COALESCE(py.amount, COALESCE(pr.approved_amount, pr.amount)) AS amt,
		COALESCE(py.paid_on,''), COALESCE(pr.expected_return_date,''), COALESCE(pr.repayment_notes,''),
		pr.status, COALESCE(u.name,''), COALESCE(pr.on_hold,0),
		CAST(julianday(COALESCE(NULLIF(pr.expected_return_date,''),?)) - julianday(?) AS INTEGER) AS days
		FROM payment_requests pr
		LEFT JOIN payments py ON py.request_id=pr.id AND py.voided_at IS NULL
		LEFT JOIN recoverable_categories rc ON rc.id=pr.recoverable_category_id
		LEFT JOIN projects p ON p.id=pr.project_id
		LEFT JOIN users u ON u.id=pr.requester_id
		WHERE pr.treatment='recoverable' AND pr.status NOT IN ('rejected','cancelled','withdrawn')`
	args := []any{today, today}

	if validMonth(opts.From) || validMonth(opts.To) {
		from, to := opts.From, opts.To
		if !validMonth(from) {
			from = to
		}
		if !validMonth(to) {
			to = from
		}
		if to < from {
			from, to = to, from
		}
		q += ` AND substr(COALESCE(py.paid_on,''),1,7) BETWEEN ? AND ?`
		args = append(args, from, to)
	}
	if opts.CategoryID > 0 {
		q += ` AND pr.recoverable_category_id=?`
		args = append(args, opts.CategoryID)
	}
	if cp := strings.TrimSpace(opts.Counterparty); cp != "" {
		q += ` AND lower(COALESCE(pr.counterparty,''))=lower(?)`
		args = append(args, cp)
	}
	switch opts.Ageing {
	case "overdue":
		q += ` AND COALESCE(pr.expected_return_date,'') <> '' AND pr.expected_return_date < ?`
		args = append(args, today)
	case "due30":
		q += ` AND COALESCE(pr.expected_return_date,'') <> '' AND pr.expected_return_date >= ?
			AND julianday(pr.expected_return_date) - julianday(?) <= 30`
		args = append(args, today, today)
	case "later":
		q += ` AND (COALESCE(pr.expected_return_date,'') = '' OR julianday(pr.expected_return_date) - julianday(?) > 30)`
		args = append(args, today)
	case "unpaid":
		q += ` AND py.id IS NULL`
	}
	search := strings.ToLower(strings.TrimSpace(opts.Query))
	if search != "" {
		q += ` AND (lower(pr.number) LIKE ? ESCAPE '\' OR lower(COALESCE(pr.counterparty,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(p.name,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(u.name,'')) LIKE ? ESCAPE '\')`
		search = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
		needle := "%" + search + "%"
		args = append(args, needle, needle, needle, needle)
	}
	switch opts.Order {
	case "amount":
		q += ` ORDER BY amt DESC, pr.number`
	case "paid_on":
		q += ` ORDER BY COALESCE(py.paid_on,'') DESC, pr.number`
	case "number":
		q += ` ORDER BY pr.number`
	default:
		// Overdue first, then the soonest expected return; undated rows sort last.
		q += ` ORDER BY CASE WHEN COALESCE(pr.expected_return_date,'') <> '' AND pr.expected_return_date < ? THEN 0 ELSE 1 END,
			COALESCE(NULLIF(pr.expected_return_date,''),'9999-12-31'), pr.number`
		args = append(args, today)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecoverableRow
	for rows.Next() {
		var r RecoverableRow
		var onHold int
		if err := rows.Scan(&r.RequestID, &r.Number, &r.Category, &r.Counterparty, &r.Project, &r.Amount,
			&r.PaidOn, &r.ExpectedReturnDate, &r.RepaymentNotes, &r.Status, &r.Requester, &onHold, &r.DaysToReturn); err != nil {
			return nil, err
		}
		r.OnHold = onHold == 1
		r.HasReturnDate = r.ExpectedReturnDate != ""
		if !r.HasReturnDate {
			r.DaysToReturn = 0
		}
		r.AgeingLabel, r.AgeingTone, r.Overdue = recoverableAgeing(r.PaidOn, r.ExpectedReturnDate, r.OnHold, r.DaysToReturn)
		out = append(out, r)
	}
	return out, rows.Err()
}
```

The `COALESCE(NULLIF(pr.expected_return_date,''),?)` in the `days` expression substitutes today's date for an undated row so `julianday` never returns NULL into an `int` scan; the Go side then zeroes `DaysToReturn` and `recoverableAgeing` renders "No fixed date" instead.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestRecoverablePaymentExcludedFromActualsButInRecoverableReport|TestRecoverableReportShowsLinkedProject|TestRecoverableAgeingLabels|TestRecoverableReportIncludesUnpaidAndExcludesDeadRequests|TestRecoverableReportAgeingFilterAndOrdering' -v`
Expected: PASS — all five tests PASS, including every `TestRecoverableAgeingLabels/*` subtest.

- [ ] **Step 6: Commit**

```bash
git add internal/store/models.go internal/store/recoverables.go internal/store/recoverables_test.go
git commit -m "feat(store): add aged RecoverableReport with ageing filter and ordering"
```

---

### Task 7: `RecoverableMetrics` + `RecoverableRollups` (G18)

`recoverables-dashboard.html` shows four metrics and two GROUP BY tables. None of them can be derived from a page of `RecoverableRow`s without re-implementing the base-set rules in Go, so both are store queries sharing Task 6's WHERE clause.

**Files:**
- Modify: `internal/store/models.go` (add `RecoverableMetrics`, `RecoverableRollup`)
- Modify: `internal/store/recoverables.go` (add `recoverableBaseWhere`, `RecoverableMetrics`, `RecoverableRollups`)
- Test: `internal/store/recoverables_test.go`

**Interfaces:**
- Consumes: the base set defined in Task 6; `seedRecoverablePayment` (Task 5), `seedRecoverableRequestOnly` (Task 6).
- Produces:
  - `type RecoverableMetrics struct { OutstandingAmount int64; OutstandingCount int; OverdueAmount int64; OverdueCount int; DueIn30Amount int64; DueIn30Count int; PaidThisMonthAmount int64; PaidThisMonthCount int }`
  - `type RecoverableRollup struct { Label, Detail string; Count int; Outstanding, Overdue int64; Oldest, ExpectedBack string }`
  - `func (s *Store) RecoverableMetrics(ctx context.Context, asOf time.Time) (RecoverableMetrics, error)`
  - `func (s *Store) RecoverableRollups(ctx context.Context, by string, asOf time.Time) ([]RecoverableRollup, error)` — `by` is `"category"` or `"counterparty"`; anything else is `ErrValidation`.

`RecoverableRollup.Detail` is empty for category rows and carries the distinct category names for counterparty rows — the replacement for the mockup's unmodelled "Type" column (see the Amendment log).

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/recoverables_test.go`:

```go
func TestRecoverableMetricsFourDashboardNumbers(t *testing.T) { // G18
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	asOf := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	// Paid this month, due far out.
	req1 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000401", "completed", "PBG", "Ridge Metro", "2027-01-15", 300000, false)
	payRecoverable(t, s, ctx, actor, headID, req1, "2026-07-05", 300000)
	// Paid earlier, now overdue.
	req2 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000402", "completed", "EMD", "Coastal Power", "2026-06-30", 200000, false)
	payRecoverable(t, s, ctx, actor, headID, req2, "2026-02-12", 200000)
	// Paid earlier, due inside 30 days.
	req3 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000403", "completed", "ICD", "Harith Infra", "2026-08-10", 100000, false)
	payRecoverable(t, s, ctx, actor, headID, req3, "2026-03-01", 100000)
	// Approved, unpaid: outstanding but nothing paid out.
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000404", "approved", "EMD", "Ridge Metro", "2026-11-30", 50000, false)
	// Rejected: invisible everywhere.
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000405", "rejected", "EMD", "Nobody", "2026-01-01", 999999, false)

	m, err := s.RecoverableMetrics(ctx, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if m.OutstandingAmount != 650000 || m.OutstandingCount != 4 {
		t.Fatalf("outstanding = %d over %d rows, want 650000 over 4", m.OutstandingAmount, m.OutstandingCount)
	}
	if m.OverdueAmount != 200000 || m.OverdueCount != 1 {
		t.Fatalf("past expected return = %d over %d, want 200000 over 1", m.OverdueAmount, m.OverdueCount)
	}
	if m.DueIn30Amount != 100000 || m.DueIn30Count != 1 {
		t.Fatalf("due in 30 days = %d over %d, want 100000 over 1", m.DueIn30Amount, m.DueIn30Count)
	}
	if m.PaidThisMonthAmount != 300000 || m.PaidThisMonthCount != 1 {
		t.Fatalf("paid out this month = %d over %d, want 300000 over 1", m.PaidThisMonthAmount, m.PaidThisMonthCount)
	}
}

func TestRecoverableRollupsByCategoryAndCounterparty(t *testing.T) { // G18
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	asOf := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	req1 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000501", "completed", "EMD", "Ridge Metro", "2026-06-30", 200000, false)
	payRecoverable(t, s, ctx, actor, headID, req1, "2026-02-12", 200000)
	req2 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000502", "completed", "EMD", "Coastal Power", "2026-12-31", 400000, false)
	payRecoverable(t, s, ctx, actor, headID, req2, "2026-04-03", 400000)
	req3 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, "PR-2026-000503", "completed", "PBG", "Ridge Metro", "2026-11-30", 300000, false)
	payRecoverable(t, s, ctx, actor, headID, req3, "2026-05-20", 300000)

	cats, err := s.RecoverableRollups(ctx, "category", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 2 {
		t.Fatalf("category rollups = %d, want 2 (EMD, PBG)", len(cats))
	}
	if cats[0].Label != "EMD" || cats[0].Count != 2 || cats[0].Outstanding != 600000 {
		t.Fatalf("EMD rollup = %+v, want 2 items totalling 600000, largest first", cats[0])
	}
	if cats[0].Overdue != 200000 {
		t.Fatalf("EMD overdue = %d, want 200000", cats[0].Overdue)
	}
	if cats[0].Oldest != "2026-02-12" {
		t.Fatalf("EMD oldest paid_on = %q, want 2026-02-12", cats[0].Oldest)
	}

	cps, err := s.RecoverableRollups(ctx, "counterparty", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(cps) != 2 {
		t.Fatalf("counterparty rollups = %d, want 2", len(cps))
	}
	if cps[0].Label != "Ridge Metro" || cps[0].Count != 2 || cps[0].Outstanding != 500000 {
		t.Fatalf("Ridge Metro rollup = %+v, want 2 items totalling 500000", cps[0])
	}
	if !strings.Contains(cps[0].Detail, "EMD") || !strings.Contains(cps[0].Detail, "PBG") {
		t.Fatalf("counterparty Detail = %q, want the distinct categories", cps[0].Detail)
	}
	if cps[0].ExpectedBack != "2026-06-30" {
		t.Fatalf("Ridge Metro expected back = %q, want the earliest date 2026-06-30", cps[0].ExpectedBack)
	}

	if _, err := s.RecoverableRollups(ctx, "project", asOf); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown rollup dimension = %v, want ErrValidation", err)
	}
}
```

Add `"strings"` to the `internal/store/recoverables_test.go` import block, and add the payment helper next to `seedRecoverableRequestOnly`:

```go
// payRecoverable links a settled, non-voided payment to an existing recoverable request.
func payRecoverable(t *testing.T, s *Store, ctx context.Context, actor User, headID, requestID int64, paidOn string, amount int64) int64 {
	t.Helper()
	res, err := s.DB().ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,entered_by,request_id,settlement) VALUES(?,?,?,?,?,?, 'settled')`,
		headID, paidOn, amount, "Counterparty", actor.ID, requestID)
	if err != nil {
		t.Fatalf("insert payment for request %d: %v", requestID, err)
	}
	id, _ := res.LastInsertId()
	return id
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestRecoverableMetricsFourDashboardNumbers|TestRecoverableRollupsByCategoryAndCounterparty' -v`
Expected: FAIL (build) — `s.RecoverableMetrics undefined`, `s.RecoverableRollups undefined`, `undefined: RecoverableRollup`.

- [ ] **Step 3: Add the metric and rollup types**

In `internal/store/models.go`, after `RecoverableReportOptions`, add:

```go
// RecoverableMetrics is the four-number strip on the recoverables dashboard.
// Outstanding counts every live recoverable, paid or not; PaidThisMonth counts
// only money that actually left in the calendar month containing asOf.
type RecoverableMetrics struct {
	OutstandingAmount   int64
	OutstandingCount    int
	OverdueAmount       int64
	OverdueCount        int
	DueIn30Amount       int64
	DueIn30Count        int
	PaidThisMonthAmount int64
	PaidThisMonthCount  int
}

// RecoverableRollup is one grouped row of the dashboard's by-category or
// by-counterparty table. Detail is empty for category rows and lists the distinct
// categories for counterparty rows.
type RecoverableRollup struct {
	Label        string
	Detail       string
	Count        int
	Outstanding  int64
	Overdue      int64
	Oldest       string // earliest paid_on in the group; "" when nothing is paid yet
	ExpectedBack string // earliest expected_return_date in the group
}
```

- [ ] **Step 4: Implement the metrics and rollups**

In `internal/store/recoverables.go`, add:

```go
// recoverableBaseWhere is the single definition of "a live recoverable", shared by
// RecoverableReport, RecoverableMetrics and RecoverableRollups so the dashboard
// totals can never drift from the list underneath them.
const recoverableBaseWhere = `pr.treatment='recoverable' AND pr.status NOT IN ('rejected','cancelled','withdrawn')`

func (s *Store) RecoverableMetrics(ctx context.Context, asOf time.Time) (RecoverableMetrics, error) {
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	today := asOf.UTC().Format("2006-01-02")
	month := asOf.UTC().Format("2006-01")
	var m RecoverableMetrics
	err := s.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(amt),0), COUNT(*),
		COALESCE(SUM(CASE WHEN exp <> '' AND exp < ? THEN amt ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN exp <> '' AND exp < ? THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN exp <> '' AND exp >= ? AND julianday(exp)-julianday(?) <= 30 THEN amt ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN exp <> '' AND exp >= ? AND julianday(exp)-julianday(?) <= 30 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN substr(paid,1,7)=? THEN amt ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN substr(paid,1,7)=? THEN 1 ELSE 0 END),0)
		FROM (SELECT COALESCE(py.amount, COALESCE(pr.approved_amount, pr.amount)) AS amt,
			COALESCE(pr.expected_return_date,'') AS exp, COALESCE(py.paid_on,'') AS paid
			FROM payment_requests pr
			LEFT JOIN payments py ON py.request_id=pr.id AND py.voided_at IS NULL
			WHERE `+recoverableBaseWhere+`)`,
		today, today, today, today, today, today, month, month).
		Scan(&m.OutstandingAmount, &m.OutstandingCount, &m.OverdueAmount, &m.OverdueCount,
			&m.DueIn30Amount, &m.DueIn30Count, &m.PaidThisMonthAmount, &m.PaidThisMonthCount)
	if err != nil {
		return RecoverableMetrics{}, err
	}
	return m, nil
}

func (s *Store) RecoverableRollups(ctx context.Context, by string, asOf time.Time) ([]RecoverableRollup, error) {
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	today := asOf.UTC().Format("2006-01-02")
	var label, detail string
	switch by {
	case "category":
		label = `COALESCE(rc.name,'Uncategorised')`
		detail = `''`
	case "counterparty":
		label = `COALESCE(NULLIF(pr.counterparty,''),'Not recorded')`
		detail = `COALESCE(GROUP_CONCAT(DISTINCT rc.name),'')`
	default:
		return nil, fmt.Errorf("%w: unknown recoverable rollup dimension %q", ErrValidation, by)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+label+` AS grp, `+detail+`,
		COUNT(*), COALESCE(SUM(COALESCE(py.amount, COALESCE(pr.approved_amount, pr.amount))),0),
		COALESCE(SUM(CASE WHEN COALESCE(pr.expected_return_date,'') <> '' AND pr.expected_return_date < ?
			THEN COALESCE(py.amount, COALESCE(pr.approved_amount, pr.amount)) ELSE 0 END),0),
		COALESCE(MIN(NULLIF(COALESCE(py.paid_on,''),'')),''),
		COALESCE(MIN(NULLIF(COALESCE(pr.expected_return_date,''),'')),'')
		FROM payment_requests pr
		LEFT JOIN payments py ON py.request_id=pr.id AND py.voided_at IS NULL
		LEFT JOIN recoverable_categories rc ON rc.id=pr.recoverable_category_id
		WHERE `+recoverableBaseWhere+`
		GROUP BY grp
		ORDER BY 4 DESC, grp`, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecoverableRollup
	for rows.Next() {
		var r RecoverableRollup
		if err := rows.Scan(&r.Label, &r.Detail, &r.Count, &r.Outstanding, &r.Overdue, &r.Oldest, &r.ExpectedBack); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

Then replace the inline `WHERE pr.treatment='recoverable' AND pr.status NOT IN ('rejected','cancelled','withdrawn')` in Task 6's `RecoverableReport` with `WHERE `+recoverableBaseWhere so the three queries share one definition. Re-run Task 6's tests to confirm the substitution is behaviour-neutral.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestRecoverableMetricsFourDashboardNumbers|TestRecoverableRollupsByCategoryAndCounterparty|TestRecoverableReport' -v`
Expected: PASS — the two new tests plus every Task-6 `TestRecoverableReport*` test still pass after the shared-WHERE substitution.

- [ ] **Step 6: Commit**

```bash
git add internal/store/models.go internal/store/recoverables.go internal/store/recoverables_test.go
git commit -m "feat(store): add recoverable dashboard metrics and category/counterparty rollups"
```

---

### Task 8: Recoverables dashboard screen `GET /recoverables` (G18)

**Files:**
- Modify: `internal/app/app.go` (PageData fields, route, `recoverablesDashboard` handler)
- Modify: `internal/app/nav.go` (one `navSpec` entry)
- Modify: `internal/app/templates.go` (`recoverables_dashboard` template)
- Test: `internal/app/recoverables_app_test.go`

**Interfaces:**
- Consumes: `store.RecoverableMetrics`, `store.RecoverableRollups` (Task 7); `RequirePermission("recoverable_report","view")`; `money.FormatPaise` / `money.FormatShort` via the existing `money` / `short` template funcs; Phase-0 `Shell` and `navSpec`.
- Produces: route `GET /recoverables`; handler `recoverablesDashboard`; template `recoverables_dashboard`; PageData fields `RecMetrics store.RecoverableMetrics`, `ByCategory []store.RecoverableRollup`, `ByCounterparty []store.RecoverableRollup`.

Rendered from `mockups/screens/recoverables-dashboard.html`: a `.banner.brand` explainer, a four-tile `.metric-strip` (the second tile carries `.warn`), two `.section-head` + `table.t-cards` rollups each closed by a `<tfoot>` total, and a closing `.banner.locked` restating that repayment tracking is out of scope.

- [ ] **Step 1: Write the failing integration test**

Create `internal/app/recoverables_app_test.go` (Tasks 9 and 11 will add `net/url` to this import block when their tests need it):

```go
package app

import (
	"net/http"
	"strings"
	"testing"

	"fervidbudget/internal/auth"
)

// seedRecoverable inserts a completed recoverable request + its settled payment
// via the store's DB handle, so app tests can render the register.
func seedRecoverable(t *testing.T, s *appTestServer, headID int64, number, category, counterparty, expectedReturn, paidOn string, amount int64) int64 {
	t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	var catID int64
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT id FROM recoverable_categories WHERE name=?`, category).Scan(&catID); err != nil {
		t.Fatal(err)
	}
	res, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO payment_requests
		(number,status,treatment,type,recoverable_category_id,head_id,amount,purpose,counterparty,expected_return_date,repayment_notes,requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?, 'completed','recoverable','recoverable',?,?,?, 'Inter-corporate deposit',?,?, 'Recover on maturity',?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		number, catID, headID, amount, counterparty, expectedReturn, admin.ID, admin.ID, amount, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	reqID, _ := res.LastInsertId()
	if paidOn != "" {
		if _, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,entered_by,request_id,settlement) VALUES(?,?,?,?,?,?, 'settled')`,
			headID, paidOn, amount, counterparty, admin.ID, reqID); err != nil {
			t.Fatal(err)
		}
	}
	return reqID
}

func TestRecoverablesDashboardRendersMetricsAndRollups(t *testing.T) { // G18
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recoverable")
	seedRecoverable(t, s, headID, "PR-2026-000101", "ICD", "Beacon Infra Ltd", "2027-09-30", "2026-08-15", 900000)
	seedRecoverable(t, s, headID, "PR-2026-000102", "EMD", "Ridge Metro tender authority", "2026-01-31", "2026-01-05", 200000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/recoverables", nil, ""))
	for _, want := range []string{
		"Recoverable payments",
		"Kept out of budget actuals on purpose",     // .banner.brand explainer
		"Tracking the money coming back is not in this version", // .banner.locked
		"Outstanding", "Past expected return", "Due in 30 days", "Paid out this month",
		"By category", "By counterparty",
		"Beacon Infra Ltd", "Ridge Metro tender authority",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("recoverables dashboard missing %q", want)
		}
	}
	for _, want := range []string{`class="metric-strip"`, `class="metric warn"`, `class="banner brand"`, `class="banner locked"`, `class="t-cards"`, "<tfoot>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("recoverables dashboard missing design-system markup %q", want)
		}
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("templates must use .pill, never .badge (D5)")
	}
	assertTCardsLabelled(t, body)
}

// assertTCardsLabelled fails when any <td> inside a table.t-cards lacks a
// data-label attribute. Below 860 px the CSS restacks rows into cards and reads
// that attribute for the field name, so a missing one renders an unlabelled value.
func assertTCardsLabelled(t *testing.T, body string) {
	t.Helper()
	for _, chunk := range strings.Split(body, `class="t-cards"`)[1:] {
		table := chunk
		if end := strings.Index(table, "</table>"); end >= 0 {
			table = table[:end]
		}
		for _, cell := range strings.Split(table, "<td")[1:] {
			head := cell
			if gt := strings.Index(head, ">"); gt >= 0 {
				head = head[:gt]
			}
			if !strings.Contains(head, "data-label=") {
				t.Fatalf("t-cards <td%s> has no data-label (mobile card restack contract)", head)
			}
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/app/ -run TestRecoverablesDashboardRendersMetricsAndRollups -v`
Expected: FAIL — `/recoverables` returns 404 (route not registered) so every `strings.Contains` assertion fails.

- [ ] **Step 3: Add PageData fields**

In `internal/app/app.go`, add to the `PageData` struct (after `Reports []store.ReportRow`):

```go
	RecMetrics     store.RecoverableMetrics
	ByCategory     []store.RecoverableRollup
	ByCounterparty []store.RecoverableRollup
```

- [ ] **Step 4: Add the handler**

In `internal/app/app.go`, after `exportYTD`, add:

```go
func (a *App) recoverablesDashboard(w http.ResponseWriter, r *http.Request) {
	asOf := time.Now().UTC() // the only clock call on this path; the store takes it as a parameter
	metrics, err := a.st.RecoverableMetrics(r.Context(), asOf)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	byCat, err := a.st.RecoverableRollups(r.Context(), "category", asOf)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	byCp, err := a.st.RecoverableRollups(r.Context(), "counterparty", asOf)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "recoverables_dashboard", PageData{
		Title: "Recoverable payments", RecMetrics: metrics, ByCategory: byCat, ByCounterparty: byCp,
	})
}
```

- [ ] **Step 5: Register the route and the nav item**

In `internal/app/app.go` `routes`, after the `/reports/ytd.csv` line, add:

```go
	mux.Handle("GET /recoverables", a.auth.RequirePermission("recoverable_report", "view", http.HandlerFunc(a.recoverablesDashboard)))
```

In `internal/app/nav.go`, add one entry to `navSpec`'s Reports group (Phase 0 filters it by permission; no role string is involved):

```go
	{Key: "recoverables", Label: "Recoverables", Href: "/recoverables", Icon: "⟲", Resource: "recoverable_report", Action: "view"},
```

- [ ] **Step 6: Add the template**

In `internal/app/templates.go`, add the `recoverables_dashboard` template before the closing backtick:

```html
{{define "recoverables_dashboard"}}
{{template "top" .}}
<section class="page-banner d-only"><div><div class="eyebrow">Money we expect back</div><h1>Recoverable payments</h1><p class="sub">{{money .RecMetrics.OutstandingAmount}} outstanding across {{.RecMetrics.OutstandingCount}} payments · none of it counts as budget spend</p></div><div class="pb-actions"><a class="btn outline" href="/recoverables/list.csv">⤓ Export CSV</a></div></section>
<div class="banner brand"><span class="b-ico">↩</span><div><b>Kept out of budget actuals on purpose</b><p>A deposit is not an expense. These payments never appear in the variance grid or in project spend — they live here until the money comes back.</p></div></div>
<div class="metric-strip">
  <div class="metric"><span class="metric-label">Outstanding</span><span class="metric-value">{{short .RecMetrics.OutstandingAmount}}</span><span class="metric-foot">{{.RecMetrics.OutstandingCount}} payments</span></div>
  <div class="metric warn"><span class="metric-label">Past expected return</span><span class="metric-value">{{short .RecMetrics.OverdueAmount}}</span><span class="metric-foot">{{.RecMetrics.OverdueCount}} payments overdue</span></div>
  <div class="metric"><span class="metric-label">Due in 30 days</span><span class="metric-value">{{short .RecMetrics.DueIn30Amount}}</span><span class="metric-foot">{{.RecMetrics.DueIn30Count}} payments</span></div>
  <div class="metric"><span class="metric-label">Paid out this month</span><span class="metric-value">{{short .RecMetrics.PaidThisMonthAmount}}</span><span class="metric-foot">{{.RecMetrics.PaidThisMonthCount}} payments</span></div>
</div>
<div class="section-head"><h2>By category</h2><a class="more-link" href="/recoverables/list">Full list →</a></div>
<div class="table-wrap"><table class="t-cards">
<thead><tr><th>Category</th><th class="c">Count</th><th class="num">Outstanding</th><th class="num">Overdue</th><th>Oldest</th></tr></thead>
<tbody>{{range .ByCategory}}<tr>
  <td class="t-lead" data-label="Category"><a href="/recoverables/list?category_name={{urlquery .Label}}">{{.Label}}</a></td>
  <td class="c" data-label="Count">{{.Count}}</td>
  <td class="num" data-label="Outstanding">{{money .Outstanding}}</td>
  <td class="num{{if .Overdue}} bad-num{{end}}" data-label="Overdue">{{if .Overdue}}{{money .Overdue}}{{else}}—{{end}}</td>
  <td data-label="Oldest">{{if .Oldest}}{{.Oldest}}{{else}}Not yet paid{{end}}</td>
</tr>{{else}}<tr><td colspan="5" class="empty" data-label="">No recoverable payments yet.</td></tr>{{end}}</tbody>
<tfoot><tr><td data-label="">Total</td><td class="c" data-label="Count">{{.RecMetrics.OutstandingCount}}</td><td class="num" data-label="Outstanding">{{money .RecMetrics.OutstandingAmount}}</td><td class="num" data-label="Overdue">{{money .RecMetrics.OverdueAmount}}</td><td data-label="">—</td></tr></tfoot>
</table></div>
<div class="section-head"><h2>By counterparty</h2></div>
<div class="table-wrap"><table class="t-cards">
<thead><tr><th>Counterparty</th><th>Categories</th><th class="c">Items</th><th class="num">Outstanding</th><th>Expected back</th></tr></thead>
<tbody>{{range .ByCounterparty}}<tr>
  <td class="t-lead" data-label="Counterparty"><a href="/recoverables/list?counterparty={{urlquery .Label}}">{{.Label}}</a></td>
  <td data-label="Categories">{{if .Detail}}{{.Detail}}{{else}}—{{end}}</td>
  <td class="c" data-label="Items">{{.Count}}</td>
  <td class="num" data-label="Outstanding">{{money .Outstanding}}</td>
  <td data-label="Expected back">{{if .ExpectedBack}}{{.ExpectedBack}}{{else}}No fixed date{{end}}</td>
</tr>{{else}}<tr><td colspan="5" class="empty" data-label="">No counterparties yet.</td></tr>{{end}}</tbody>
</table></div>
<div class="banner locked"><span class="b-ico">i</span><div><b>Tracking the money coming back is not in this version</b><p>A recoverable closes when its payment is made. Repayments, forfeitures, and converting a lost deposit into an expense are deliberately out of scope — they need an accounting adjustment process, not an edit to a completed payment.</p></div></div>
{{template "bottom" .}}
{{end}}
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `go test ./internal/app/ -run TestRecoverablesDashboardRendersMetricsAndRollups -v`
Expected: PASS — `--- PASS: TestRecoverablesDashboardRendersMetricsAndRollups`.

- [ ] **Step 8: Commit**

```bash
git add internal/app/app.go internal/app/nav.go internal/app/templates.go internal/app/recoverables_app_test.go
git commit -m "feat(app): add recoverables dashboard with ageing metrics and rollups"
```

---

### Task 9: Recoverables list screen + CSV export

**Files:**
- Modify: `internal/app/app.go` (PageData fields, routes, `recoverablesList` + `exportRecoverable` handlers)
- Modify: `internal/app/templates.go` (`recoverables_list` template)
- Test: `internal/app/recoverables_app_test.go`

**Interfaces:**
- Consumes: `store.RecoverableReport`, `store.ListRecoverableCategories`, `store.RecoverableRow`, `store.RecoverableReportOptions`; `RequirePermission("recoverable_report", …)`; `money.FormatPaise`; helpers `queryDefault`, `parseID`.
- Produces: routes `GET /recoverables/list`, `GET /recoverables/list.csv`; handlers `recoverablesList`, `exportRecoverable`; template `recoverables_list`; PageData fields `Recoverables []store.RecoverableRow`, `RecoverableTotal int64`, `Categories []store.RecoverableCategory`, `CategoryID int64`, `Ageing string`.

**Routing note:** `GET /recoverables/list.csv` and `GET /recoverables/{id}` (Task 10) coexist safely. Go 1.22+ `ServeMux` gives the more specific pattern precedence, and a literal final segment is more specific than a wildcard, so `/recoverables/list.csv` never reaches the detail handler and `parseID("list.csv")` is never called. Task 10's test asserts this explicitly.

Rendered from `mockups/screens/recoverables-list.html`: a desktop `.toolbar` of Search / Category / Ageing, the mobile `.m-filters` strip, one `table.t-cards` whose Category cell is a `.pill.recoverable` and whose Ageing cell is a `.pill` in the row's own tone, and a `<tfoot>` carrying the shown-count and the amount total.

- [ ] **Step 1: Write the failing integration tests**

Append to `internal/app/recoverables_app_test.go`:

```go
func TestRecoverablesListScreenAndExport(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recoverable")
	seedRecoverable(t, s, headID, "PR-2026-000101", "ICD", "Beacon Infra Ltd", "2027-09-30", "2026-08-15", 900000)
	seedRecoverable(t, s, headID, "PR-2026-000102", "EMD", "Ridge Metro tender authority", "2026-01-31", "2026-01-05", 200000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/recoverables/list", nil, ""))
	for _, want := range []string{"All recoverables", "Beacon Infra Ltd", "PR-2026-000101", "Ridge Metro tender authority"} {
		if !strings.Contains(body, want) {
			t.Fatalf("recoverables list missing %q", want)
		}
	}
	for _, want := range []string{`class="t-cards"`, `class="m-filters"`, `class="pill recoverable"`, "<tfoot>", `name="ageing"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("recoverables list missing design-system markup %q", want)
		}
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("templates must use .pill, never .badge (D5)")
	}
	if !strings.Contains(body, `class="pill bad"`) {
		t.Fatal("an overdue row must render its ageing as a .pill.bad")
	}
	assertTCardsLabelled(t, body)

	// The ageing filter narrows the register.
	overdue := responseBody(t, s.request(http.MethodGet, "/recoverables/list?ageing=overdue", nil, ""))
	if !strings.Contains(overdue, "PR-2026-000102") || strings.Contains(overdue, "PR-2026-000101") {
		t.Fatal("ageing=overdue must keep only the row past its expected return date")
	}

	resp := s.request(http.MethodGet, "/recoverables/list.csv?from=2026-08&to=2026-08", nil, "")
	requireStatus(t, resp, http.StatusOK)
	csv := responseBody(t, resp)
	if !strings.HasPrefix(csv, "Number,Category,Counterparty,Project,Amount,Paid On,Expected Return,Ageing,Status,Requester,Repayment Notes") {
		t.Fatalf("recoverable CSV header unexpected: %s", csv)
	}
	if !strings.Contains(csv, "Beacon Infra Ltd") {
		t.Fatalf("recoverable CSV missing data row: %s", csv)
	}
}

func TestRecoverablesListViewableByAccountsRole(t *testing.T) {
	s := newAppTestServer(t)
	hash, err := auth.HashPassword("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	// data_entry maps to the seeded Accounts role, which holds recoverable_report{view,export}
	if _, err := s.st.CreateUser(s.ctx, "accounts@example.test", "Accounts User", hash, "data_entry", true); err != nil {
		t.Fatal(err)
	}
	s.login("accounts@example.test", "EntryPassword123")
	for _, path := range []string{"/recoverables", "/recoverables/list"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusOK)
		_ = responseBody(t, resp)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ -run 'TestRecoverablesListScreenAndExport|TestRecoverablesListViewableByAccountsRole' -v`
Expected: FAIL — `/recoverables/list` returns 404 (route not registered) so the status/body assertions fail.

- [ ] **Step 3: Add PageData fields**

In `internal/app/app.go`, add to the `PageData` struct (after the Task-8 fields):

```go
	Recoverables     []store.RecoverableRow
	RecoverableTotal int64
	Categories       []store.RecoverableCategory
	CategoryID       int64
	Ageing           string
```

- [ ] **Step 4: Add the handlers**

In `internal/app/app.go`, after `recoverablesDashboard`, add:

```go
// recoverableListOptions builds the shared query options for the list screen and
// its CSV export. From/To stay optional: with no range the register shows every
// live recoverable including ones approved but not yet paid.
func (a *App) recoverableListOptions(r *http.Request) store.RecoverableReportOptions {
	q := r.URL.Query()
	ageing := q.Get("ageing")
	switch ageing {
	case "overdue", "due30", "later", "unpaid": // allow-list; anything else means "all"
	default:
		ageing = ""
	}
	return store.RecoverableReportOptions{
		From:         q.Get("from"),
		To:           q.Get("to"),
		CategoryID:   parseID(q.Get("category")),
		Counterparty: q.Get("counterparty"),
		Query:        q.Get("q"),
		Ageing:       ageing,
		Order:        q.Get("order"),
		AsOf:         time.Now().UTC(),
	}
}

func (a *App) recoverablesList(w http.ResponseWriter, r *http.Request) {
	opts := a.recoverableListOptions(r)
	rows, err := a.st.RecoverableReport(r.Context(), opts)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	cats, err := a.st.ListRecoverableCategories(r.Context(), false)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	var total int64
	for _, row := range rows {
		total += row.Amount
	}
	a.render(w, r, "recoverables_list", PageData{
		Title: "All recoverables", Recoverables: rows, RecoverableTotal: total,
		Categories: cats, CategoryID: opts.CategoryID, Ageing: opts.Ageing,
		From: opts.From, To: opts.To, Query: opts.Query,
	})
}

func (a *App) exportRecoverable(w http.ResponseWriter, r *http.Request) {
	opts := a.recoverableListOptions(r)
	rows, err := a.st.RecoverableReport(r.Context(), opts)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	u := auth.CurrentUser(r)
	scope := "all live recoverables"
	if opts.From != "" || opts.To != "" {
		scope = opts.From + " to " + opts.To
	}
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "export", EntityType: "recoverable_report", Summary: "Exported recoverable payments · " + scope})
	var body bytes.Buffer
	cw := csv.NewWriter(&body)
	_ = cw.Write([]string{"Number", "Category", "Counterparty", "Project", "Amount", "Paid On", "Expected Return", "Ageing", "Status", "Requester", "Repayment Notes"})
	for _, row := range rows {
		_ = cw.Write([]string{row.Number, row.Category, row.Counterparty, row.Project, money.FormatPaise(row.Amount), row.PaidOn, row.ExpectedReturnDate, row.AgeingLabel, row.Status, row.Requester, row.RepaymentNotes})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The export could not be generated.", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="recoverables.csv"`)
	if _, err := w.Write(body.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "csv response write failed", "request_id", requestID(r), "error", err)
	}
}
```

- [ ] **Step 5: Register the routes**

In `internal/app/app.go` `routes`, immediately after the `GET /recoverables` line added in Task 8:

```go
	mux.Handle("GET /recoverables/list", a.auth.RequirePermission("recoverable_report", "view", http.HandlerFunc(a.recoverablesList)))
	mux.Handle("GET /recoverables/list.csv", a.auth.RequirePermission("recoverable_report", "export", http.HandlerFunc(a.exportRecoverable)))
```

- [ ] **Step 6: Add the template**

In `internal/app/templates.go`, add the `recoverables_list` template before the closing backtick:

```html
{{define "recoverables_list"}}
{{template "top" .}}
<section class="page-banner d-only"><div><div class="eyebrow">Recoverable payments</div><h1>All recoverables</h1><p class="sub">Aged against the expected return date · overdue in red</p></div><div class="pb-actions"><a class="btn outline" href="/recoverables/list.csv?category={{.CategoryID}}&ageing={{.Ageing}}&q={{urlquery .Query}}">⤓ Export CSV</a></div></section>
<form class="toolbar" method="get" action="/recoverables/list">
  <div class="field search"><label for="q">Search</label><input id="q" name="q" value="{{.Query}}" placeholder="Counterparty, project, request number…"></div>
  <div class="field"><label for="cat">Category</label><select id="cat" name="category"><option value="0">All categories</option>{{range .Categories}}<option value="{{.ID}}" {{if eq $.CategoryID .ID}}selected{{end}}>{{.Name}}</option>{{end}}</select></div>
  <div class="field"><label for="age">Ageing</label><select id="age" name="ageing"><option value="">All</option><option value="overdue" {{if eq .Ageing "overdue"}}selected{{end}}>Overdue</option><option value="due30" {{if eq .Ageing "due30"}}selected{{end}}>Due in 30 days</option><option value="later" {{if eq .Ageing "later"}}selected{{end}}>Due later</option><option value="unpaid" {{if eq .Ageing "unpaid"}}selected{{end}}>Not yet paid</option></select></div>
  <span class="row-end"></span><button class="btn">Apply</button>
</form>
<div class="m-filters"><span class="m-search"><input name="q" form="rec-filters" value="{{.Query}}" placeholder="Search recoverables…" aria-label="Search"></span><button class="btn filter-btn" data-open="rec-filter-sheet">Filters</button></div>
<div class="table-wrap"><table class="t-cards">
<thead><tr><th>Request</th><th>Category</th><th>Counterparty</th><th>Project</th><th class="num">Amount</th><th>Paid on</th><th>Expected back</th><th>Ageing</th></tr></thead>
<tbody>{{range .Recoverables}}<tr>
  <td class="t-lead" data-label="Request"><a href="/recoverables/{{.RequestID}}">{{.Number}}</a></td>
  <td data-label="Category"><span class="pill recoverable">{{.Category}}</span></td>
  <td data-label="Counterparty">{{if .Counterparty}}{{.Counterparty}}{{else}}Not recorded{{end}}</td>
  <td data-label="Project">{{if .Project}}{{.Project}}{{else}}Not project linked{{end}}</td>
  <td class="num" data-label="Amount">{{money .Amount}}</td>
  <td data-label="Paid on">{{if .PaidOn}}{{.PaidOn}}{{else}}Not yet paid{{end}}</td>
  <td data-label="Expected back">{{if .HasReturnDate}}{{.ExpectedReturnDate}}{{else}}No fixed date{{end}}</td>
  <td data-label="Ageing"><span class="pill {{.AgeingTone}}">{{.AgeingLabel}}</span></td>
</tr>{{else}}<tr><td colspan="8" class="empty" data-label="">No recoverable payments match these filters.</td></tr>{{end}}</tbody>
<tfoot><tr><td data-label="">{{len .Recoverables}} shown</td><td data-label=""></td><td data-label=""></td><td data-label=""></td><td class="num" data-label="Amount">{{money .RecoverableTotal}}</td><td data-label=""></td><td data-label=""></td><td data-label=""></td></tr></tfoot>
</table></div>
{{template "bottom" .}}
{{end}}
```

The mobile `.m-filters` strip reuses the Phase-0 filter sheet: `data-open="rec-filter-sheet"` opens an `.overlay > .sheet` containing the same `category` / `ageing` selects, so no filter is desktop-only. The nav entry was added in Task 8 — there is no second nav link.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/app/ -run 'TestRecoverablesListScreenAndExport|TestRecoverablesListViewableByAccountsRole' -v`
Expected: PASS — both tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/recoverables_app_test.go
git commit -m "feat(app): add recoverables list screen with ageing filter and CSV export"
```

---

### Task 10: Recoverable detail screen `GET /recoverables/{id}` (G17)

No task in the original plan built this screen; `mockups/screens/recoverable-detail.html` is the approved design and nothing rendered it.

**Files:**
- Modify: `internal/app/app.go` (route, `recoverableDetail` handler, PageData fields)
- Modify: `internal/app/templates.go` (`recoverable_detail` template)
- Test: `internal/app/recoverables_app_test.go`

**Interfaces:**
- Consumes: `store.Request` + `Request(ctx,id)`, `store.RequestComments`, `store.Audit` (the `.thread` source), `store.PaymentForRequest` (Phase 3), `store.RecoverableReport` with `Query` set to the request number (for the ageing fields); `RequirePermission("recoverable_report","view")`; Phase-2 route `POST /requests/{id}/comment` for the `.comment-box`.
- Produces: route `GET /recoverables/{id}`; handler `recoverableDetail`; template `recoverable_detail`; PageData fields `Recoverable store.RecoverableRow`, `RecPayment store.Payment`, `HasPayment bool` (reusing the Phase-2 fields `Request2`, `Comments`, `Audit`).

Blocks, in the mockup's order: `.req-head` (number · amount · title · meta · `.rh-status` pills), an overdue `.banner.warn`, a `.card` + `.card-head` + `.dl` "Recoverable details", a `.section-head` + `.card` + `.dl` "Payment" block, a `.section-head` + `.thread` history, a `.comment-box` posting to Phase 2's comment route, and a closing `.action-bar`.

- [ ] **Step 1: Write the failing integration tests**

Append to `internal/app/recoverables_app_test.go`:

```go
func TestRecoverableDetailScreen(t *testing.T) { // G17
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recoverable")
	reqID := seedRecoverable(t, s, headID, "PR-2026-000097", "EMD", "Coastal Power Utilities Ltd", "2026-06-30", "2026-02-12", 200000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/recoverables/%d", reqID), nil, ""))
	for _, want := range []string{
		"PR-2026-000097", "Coastal Power Utilities Ltd", "EMD",
		"Past its expected return date",  // the overdue banner copy
		"Recoverable details", "Payment", "History and conversation",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("recoverable detail missing %q", want)
		}
	}
	for _, want := range []string{`class="req-head"`, `class="banner warn"`, `class="dl"`, `class="thread"`, `class="comment-box"`, `class="action-bar"`, `class="pill recoverable"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("recoverable detail missing design-system markup %q", want)
		}
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("templates must use .pill, never .badge (D5)")
	}
	// The comment box posts to Phase 2's existing conversation route, not a new one.
	if !strings.Contains(body, fmt.Sprintf(`action="/requests/%d/comment"`, reqID)) {
		t.Fatal("comment box must post to the Phase-2 request comment route")
	}
	// A non-recoverable request is not reachable through this screen.
	var budgetID int64
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO payment_requests(number,status,treatment,type,head_id,amount,purpose,requester_id,manager_id,submitted_at)
		VALUES('PR-2026-000098','pending','budget','vendor_invoice',?,?, 'Rent',?,?,CURRENT_TIMESTAMP)`, headID, int64(100000), admin.ID, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	budgetID, _ = res.LastInsertId()
	resp := s.request(http.MethodGet, fmt.Sprintf("/recoverables/%d", budgetID), nil, "")
	requireStatus(t, resp, http.StatusNotFound)
	_ = responseBody(t, resp)
}

func TestRecoverableListCSVRouteBeatsDetailWildcard(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	// /recoverables/list.csv is a literal pattern and must win over /recoverables/{id}.
	resp := s.request(http.MethodGet, "/recoverables/list.csv", nil, "")
	requireStatus(t, resp, http.StatusOK)
	csv := responseBody(t, resp)
	if !strings.HasPrefix(csv, "Number,Category,") {
		t.Fatalf("/recoverables/list.csv was routed to the detail handler: %s", csv)
	}
}
```

Add `"fmt"` to the `internal/app/recoverables_app_test.go` import block (now `"fmt"`, `"net/http"`, `"strings"`, `"testing"`, plus `"fervidbudget/internal/auth"`).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ -run 'TestRecoverableDetailScreen|TestRecoverableListCSVRouteBeatsDetailWildcard' -v`
Expected: FAIL — `/recoverables/{id}` returns 404 (route not registered); the CSV test passes only once Task 9's route exists.

- [ ] **Step 3: Add PageData fields**

In `internal/app/app.go`, add to the `PageData` struct (after the Task-9 fields):

```go
	Recoverable store.RecoverableRow
	RecPayment  store.Payment
	HasPayment  bool
```

- [ ] **Step 4: Add the handler**

In `internal/app/app.go`, after `exportRecoverable`, add:

```go
func (a *App) recoverableDetail(w http.ResponseWriter, r *http.Request) {
	id := parseID(r.PathValue("id"))
	req, err := a.st.Request(r.Context(), id)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// This screen is the recoverables register, not a general request viewer.
	if req.Treatment != "recoverable" {
		a.respondError(w, r, http.StatusNotFound, "That request is not a recoverable payment.", nil)
		return
	}
	// Reuse the register query so ageing on the detail screen and in the list can
	// never disagree; the number is unique so it returns exactly this row.
	rows, err := a.st.RecoverableReport(r.Context(), store.RecoverableReportOptions{Query: req.Number, AsOf: time.Now().UTC()})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	var row store.RecoverableRow
	for _, candidate := range rows {
		if candidate.RequestID == req.ID {
			row = candidate
		}
	}
	pay, payErr := a.st.PaymentForRequest(r.Context(), req.ID)
	if payErr != nil && !errors.Is(payErr, store.ErrNotFound) {
		a.respondStoreError(w, r, payErr)
		return
	}
	comments, err := a.st.RequestComments(r.Context(), req.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	audit, err := a.st.Audit(r.Context(), "payment_request", req.ID, 50)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "recoverable_detail", PageData{
		Title: "Recoverable " + req.Number, Request2: req, Recoverable: row,
		RecPayment: pay, HasPayment: payErr == nil, Comments: comments, Audit: audit,
	})
}
```

- [ ] **Step 5: Register the route**

In `internal/app/app.go` `routes`, after the `/recoverables/list.csv` line:

```go
	mux.Handle("GET /recoverables/{id}", a.auth.RequirePermission("recoverable_report", "view", http.HandlerFunc(a.recoverableDetail)))
```

- [ ] **Step 6: Add the template**

In `internal/app/templates.go`, add the `recoverable_detail` template before the closing backtick:

```html
{{define "recoverable_detail"}}
{{template "top" .}}
<div class="req-head">
  <div class="rh-top"><span class="rh-no">{{.Request2.Number}}</span><span class="rh-amt">{{money .Recoverable.Amount}}</span></div>
  <h1>{{.Request2.Purpose}}</h1>
  <p class="rh-meta">Raised by {{.Request2.RequesterName}}{{if .Recoverable.PaidOn}} · paid {{.Recoverable.PaidOn}}{{end}} · <span class="pill recoverable">Recoverable · {{.Recoverable.Category}}</span></p>
  <div class="rh-status"><span class="pill {{.Request2.Status}}">{{.Request2.Status}}</span><span class="pill {{.Recoverable.AgeingTone}}">{{.Recoverable.AgeingLabel}}</span></div>
</div>
{{if .Recoverable.Overdue}}<div class="banner warn"><span class="b-ico">◷</span><div><b>Past its expected return date</b><p>Expected {{.Recoverable.ExpectedReturnDate}}. This is a reporting flag only — chasing the money and recording its return happen outside this version.</p></div></div>{{end}}
<div class="card">
  <div class="card-head"><h2>Recoverable details</h2><span class="pill recoverable no-dot">Not in budget actuals</span></div>
  <dl class="dl">
    <div><dt>Amount</dt><dd class="big">{{money .Recoverable.Amount}}</dd></div>
    <div><dt>Category</dt><dd>{{.Recoverable.Category}}</dd></div>
    <div><dt>Counterparty</dt><dd>{{if .Request2.Counterparty}}{{.Request2.Counterparty}}{{else}}Not recorded{{end}}</dd></div>
    <div><dt>Related project</dt><dd>{{if .Request2.Project}}{{.Request2.Project}}{{else}}Not project linked{{end}}</dd></div>
    <div><dt>Expected return</dt><dd>{{if .Recoverable.HasReturnDate}}{{.Recoverable.ExpectedReturnDate}}{{else}}No fixed date{{end}}</dd></div>
    <div><dt>Ageing</dt><dd>{{.Recoverable.AgeingLabel}}</dd></div>
    <div style="grid-column:1/-1"><dt>Refund terms</dt><dd>{{.Request2.RepaymentNotes}}</dd></div>
  </dl>
</div>
<div class="section-head"><h2>Payment</h2></div>
<div class="card">
{{if .HasPayment}}
  <dl class="dl">
    <div><dt>Paid on</dt><dd>{{.RecPayment.PaidOn}}</dd></div>
    <div><dt>Amount paid</dt><dd class="big">{{money .RecPayment.Amount}}</dd></div>
    <div><dt>Mode</dt><dd>{{if .RecPayment.PaymentMode}}{{.RecPayment.PaymentMode}}{{else}}Not recorded{{end}}</dd></div>
    <div><dt>Reference</dt><dd class="num">{{if .RecPayment.ReferenceNo}}{{.RecPayment.ReferenceNo}}{{else}}Not recorded{{end}}</dd></div>
    <div><dt>Recorded by</dt><dd>{{.RecPayment.EnteredByName}}</dd></div>
    <div><dt>Payee</dt><dd>{{.RecPayment.VendorPayee}}</dd></div>
  </dl>
{{else}}
  <p class="empty">Approved but not yet paid. The money has not left, so nothing is outstanding against a counterparty yet.</p>
{{end}}
</div>
<div class="section-head"><h2>History and conversation</h2></div>
<ol class="thread">
{{range .Audit}}<li><span class="tl-dot brand">·</span><div class="tl-head"><b>{{.ActorName}} {{.Action}}</b><time>{{.CreatedAt.Format "02 Jan 2006, 15:04"}}</time></div><div class="tl-body">{{.Summary}}</div></li>{{end}}
{{range .Comments}}<li class="is-comment"><span class="tl-dot">{{initials .AuthorName}}</span><div class="tl-head"><b>{{.AuthorName}}</b><time>{{.CreatedAt.Format "02 Jan 2006, 15:04"}}</time></div><div class="tl-body"><p>{{.Body}}</p></div></li>{{end}}
</ol>
{{if .Perms.Can "request" "comment"}}
<form class="comment-box" method="post" action="/requests/{{.Request2.ID}}/comment">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <label for="cmt" class="flabel">Add a comment</label>
  <textarea id="cmt" name="body" placeholder="Record what you heard from the counterparty." required></textarea>
  <div class="cb-actions"><span class="row-end"></span><button class="btn primary small">Post comment</button></div>
</form>
{{end}}
<div class="action-bar">
  <span class="ab-note d-only">Recording the refund is out of scope for this version.</span>
  <span class="row-end"></span>
  <a class="btn outline" href="/recoverables/list">Back to list</a>
  {{if .Perms.Can "recoverable_report" "export"}}<a class="btn" href="/recoverables/list.csv?q={{urlquery .Request2.Number}}">⤓ Export this record</a>{{end}}
</div>
{{template "bottom" .}}
{{end}}
```

`initials` is the existing avatar-initials template func used by the Phase-2 thread partial; if Phase 2 named it differently, use that name rather than adding a second one.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/app/ -run 'TestRecoverableDetailScreen|TestRecoverableListCSVRouteBeatsDetailWildcard' -v`
Expected: PASS — both tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/recoverables_app_test.go
git commit -m "feat(app): add recoverable detail screen with ageing banner and thread"
```

---

### Task 11: Recoverable categories as a Configuration fieldset (D6)

The standalone `/recoverable-categories` screen is deleted. Categories become one `<fieldset>` inside the single Configuration screen Phase 2 owns, so an administrator configures the request module in one place instead of hunting three screens.

**Files:**
- Modify: `internal/app/configuration.go` (Phase 2 — extend `configuration` handler; add `recoverableCategorySave`)
- Modify: `internal/app/app.go` (register the sub-form route; add the PageData field)
- Modify: `internal/app/templates.go` (append one `<fieldset>` to the Phase-2 `configuration` template)
- Test: `internal/app/recoverables_app_test.go`

**Interfaces:**
- Consumes: Phase 2's `GET /configuration` handler and `configuration` template (see Consumed contracts); `store.ListRecoverableCategoriesWithUsage` and `RecoverableCategory.Requires()` (Task 3); `store.UpsertRecoverableCategory` (Task 2); `RequirePermission("recoverable_category", …)`; `withCSRF`.
- Produces: route `POST /configuration/recoverable-categories`; handler `recoverableCategorySave`; PageData field `CategoryUsage []store.RecoverableCategoryUsage`; one appended `<fieldset>`.

**Why a sub-form and not the main Save button:** the Configuration screen's `.action-bar` saves scalar `app_settings` values. Categories are rows, and adding one must not require re-posting every other fieldset. The fieldset therefore contains its own `<form>` targets — the same pattern the Phase-2 Configuration shell uses for any row-shaped section — while the fieldset itself stays visually inside the one screen.

**Permission note:** the *screen* is gated on `config`{view}; the *category mutation* stays gated on `recoverable_category`{edit}, the Phase-1 vocabulary the store audit records against. A user with `config`{view} but not `recoverable_category`{edit} sees the table read-only: the add-row and the Active checkboxes render only inside `{{if .Perms.Can "recoverable_category" "edit"}}`.

- [ ] **Step 1: Write the failing integration tests**

Add `"net/url"` to the `internal/app/recoverables_app_test.go` import block, then append:

```go
func TestConfigurationRecoverableCategoriesFieldset(t *testing.T) { // D6, V4
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recoverable")
	seedRecoverable(t, s, headID, "PR-2026-000701", "EMD", "Ridge Metro tender authority", "2026-11-30", "", 200000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/configuration", nil, ""))
	for _, want := range []string{"Recoverable categories", "EMD", "ICD", "Related project", "Counterparty company", "Nothing extra"} {
		if !strings.Contains(body, want) {
			t.Fatalf("configuration screen missing %q", want)
		}
	}
	// One descriptive Requires column, never the raw flags.
	for _, forbidden := range []string{`name="requires_project"`, `name="requires_counterparty"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("configuration must not expose the raw flag input %s (D6)", forbidden)
		}
	}
	// The "In use" count is rendered from the store, not guessed.
	if !strings.Contains(body, `data-label="In use"`) {
		t.Fatal("configuration fieldset must render an In use column")
	}
	if strings.Contains(body, "/recoverable-categories") {
		t.Fatal("the standalone /recoverable-categories screen must not exist or be linked (D6)")
	}
	assertTCardsLabelled(t, body)

	form := url.Values{"name": {"Retention money"}, "requires": {"project"}, "active": {"on"}, "sort_order": {"7"}}
	resp := s.postForm("/configuration/recoverable-categories", form)
	requireStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != "/configuration" {
		t.Fatalf("save redirect = %q, want /configuration", loc)
	}
	_ = responseBody(t, resp)
	cats, err := s.st.ListRecoverableCategories(s.ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range cats {
		if c.Name == "Retention money" && c.RequiresProject && !c.RequiresCounterparty {
			found = true
		}
	}
	if !found {
		t.Fatal("new recoverable category was not persisted with the flags implied by requires=project")
	}
}

func TestStandaloneRecoverableCategoriesScreenIsGone(t *testing.T) { // D6 proof of absence
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.request(http.MethodGet, "/recoverable-categories", nil, "")
	requireStatus(t, resp, http.StatusNotFound)
	_ = responseBody(t, resp)
	post := s.postForm("/recoverable-categories", url.Values{"name": {"Nope"}})
	if post.StatusCode != http.StatusNotFound {
		t.Fatalf("POST /recoverable-categories = %d, want 404 (categories live on /configuration)", post.StatusCode)
	}
	_ = responseBody(t, post)
}

func TestRecoverableCategorySaveRequiresPermission(t *testing.T) {
	s := newAppTestServer(t)
	hash, err := auth.HashPassword("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.CreateUser(s.ctx, "accounts2@example.test", "Accounts User", hash, "data_entry", true); err != nil {
		t.Fatal(err)
	}
	s.login("accounts2@example.test", "EntryPassword123")
	// Accounts holds no recoverable_category permission → 403 by URL
	resp := s.postForm("/configuration/recoverable-categories", url.Values{"name": {"Nope"}})
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ -run 'TestConfigurationRecoverableCategoriesFieldset|TestStandaloneRecoverableCategoriesScreenIsGone|TestRecoverableCategorySaveRequiresPermission' -v`
Expected: FAIL — the Configuration screen renders without a Recoverable categories fieldset, and `POST /configuration/recoverable-categories` returns 404.

- [ ] **Step 3: Add the PageData field and extend the Configuration handler**

In `internal/app/app.go`, add to `PageData`:

```go
	CategoryUsage []store.RecoverableCategoryUsage
```

In `internal/app/configuration.go` (Phase 2's file), inside the existing `configuration` GET handler and before it renders, add the Phase-4 branch:

```go
	// Phase 4 fieldset: categories with their usage counts.
	catUsage, err := a.st.ListRecoverableCategoriesWithUsage(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data.CategoryUsage = catUsage
```

- [ ] **Step 4: Add the save handler**

In `internal/app/configuration.go`, add:

```go
// recoverableCategorySave persists one row of the Configuration screen's
// Recoverable categories fieldset. The form carries a single "requires" select
// rather than two raw booleans (D6); this is the only place that mapping exists.
func (a *App) recoverableCategorySave(w http.ResponseWriter, r *http.Request) {
	var requiresProject, requiresCounterparty bool
	switch r.FormValue("requires") {
	case "project":
		requiresProject = true
	case "counterparty":
		requiresCounterparty = true
	case "both":
		requiresProject, requiresCounterparty = true, true
	case "none", "":
		// nothing extra
	default:
		a.respondError(w, r, http.StatusBadRequest, "That category requirement is not recognised.", nil)
		return
	}
	sortOrder, _ := strconv.Atoi(r.FormValue("sort_order"))
	_, err := a.st.UpsertRecoverableCategory(r.Context(), auth.CurrentUser(r), parseID(r.FormValue("id")),
		r.FormValue("name"), requiresProject, requiresCounterparty, r.FormValue("active") == "on", sortOrder)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/configuration", http.StatusSeeOther)
}
```

- [ ] **Step 5: Register the route**

In `internal/app/app.go` `routes`, next to Phase 2's `POST /configuration` line, add:

```go
	mux.Handle("POST /configuration/recoverable-categories", a.auth.RequirePermission("recoverable_category", "edit", http.HandlerFunc(a.withCSRF(a.recoverableCategorySave))))
```

There is **no** `GET /recoverable-categories` and no `POST /recoverable-categories`. If the earlier draft of this plan was already executed, delete both registrations, both handlers and the `recoverable_categories` template, and delete the Settings nav link that pointed at them.

- [ ] **Step 6: Append the fieldset to the Configuration template**

In `internal/app/templates.go`, inside the Phase-2 `configuration` template's `<form>`, append this fieldset after the Reminders fieldset and before the closing `.action-bar`:

```html
  <fieldset>
    <legend>Recoverable categories</legend>
    <div class="table-wrap" style="margin-bottom:10px"><table class="t-cards">
      <thead><tr><th>Category</th><th>Requires</th><th class="c">Active</th><th class="c">In use</th></tr></thead>
      <tbody>{{range .CategoryUsage}}<tr>
        <td class="t-lead" data-label="Category">{{.Name}}</td>
        <td data-label="Requires">{{.Requires}}</td>
        <td class="c" data-label="Active">{{if $.Perms.Can "recoverable_category" "edit"}}<form id="rc-{{.ID}}" method="post" action="/configuration/recoverable-categories"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="name" value="{{.Name}}"><input type="hidden" name="requires" value="{{requiresKey .RecoverableCategory}}"><input type="hidden" name="sort_order" value="{{.SortOrder}}"></form><input form="rc-{{.ID}}" type="checkbox" name="active" {{check .Active}} onchange="this.form.submit()"><noscript><button form="rc-{{.ID}}" class="btn small outline">Save</button></noscript>{{else}}{{if .Active}}<span class="pill good no-dot">On</span>{{else}}<span class="pill neutral no-dot">Off</span>{{end}}{{end}}</td>
        <td class="c" data-label="In use">{{.InUse}}</td>
      </tr>{{else}}<tr><td colspan="4" class="empty" data-label="">No recoverable categories yet.</td></tr>{{end}}</tbody>
    </table></div>
    {{if .Perms.Can "recoverable_category" "edit"}}
    <form method="post" action="/configuration/recoverable-categories"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="active" value="on">
      <div class="form-grid">
        <div class="field span-5"><label for="nc-name">New category</label><input id="nc-name" name="name" placeholder="e.g. Retention deposit" required></div>
        <div class="field span-4 m-half"><label for="nc-req">Must also capture</label><select id="nc-req" name="requires"><option value="none">Nothing extra</option><option value="project">Related project</option><option value="counterparty">Counterparty company</option><option value="both">Related project and counterparty company</option></select></div>
        <div class="field span-3 m-half" style="align-self:end"><button class="btn">Add category</button></div>
      </div>
    </form>
    {{end}}
    <p class="hint" style="margin:10px 0 0">All recoverables always capture the counterparty, the reason, an expected return date and the refund terms. These rules only add what the category needs on top. An employee advance fills the counterparty in from the requester automatically — that is a request-type rule, not a category setting.</p>
  </fieldset>
```

Add one template func alongside the existing `check`/`boolText` helpers, so the hidden `requires` field round-trips a row without re-deriving the mapping in HTML:

```go
	"requiresKey": func(c store.RecoverableCategory) string {
		switch {
		case c.RequiresProject && c.RequiresCounterparty:
			return "both"
		case c.RequiresProject:
			return "project"
		case c.RequiresCounterparty:
			return "counterparty"
		default:
			return "none"
		}
	},
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/app/ -run 'TestConfigurationRecoverableCategoriesFieldset|TestStandaloneRecoverableCategoriesScreenIsGone|TestRecoverableCategorySaveRequiresPermission' -v`
Expected: PASS — all three tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/app/app.go internal/app/configuration.go internal/app/templates.go internal/app/recoverables_app_test.go
git commit -m "feat(app): move recoverable categories into the Configuration screen"
```

---

### Task 12: Close-on-payment retains recoverable classification (V7 regression guard)

**Files:**
- Test: `internal/store/recoverables_test.go` (test only — verifies Phase 3 settlement interplay)

**Interfaces:**
- Consumes: Phase 3 `RecordPaymentForRequest(ctx, actor, requestID, in PaymentInput, settlement, partialReason string, attachment *AttachmentInput) (int64,error)`; `payment_requests` columns; `RecoverableReport` (Task 6).
- Produces: no production code; a regression test.

- [ ] **Step 1: Write the failing test**

Append to `internal/store/recoverables_test.go`:

```go
func TestRecoverableRequestClosesOnPaymentRetainingClassification(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	var catID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM recoverable_categories WHERE name='EMD'`).Scan(&catID); err != nil {
		t.Fatal(err)
	}
	// An approved + reserved (processing) recoverable request, ready to settle.
	res, err := s.DB().ExecContext(ctx, `INSERT INTO payment_requests
		(number,status,treatment,type,recoverable_category_id,project_id,head_id,amount,purpose,expected_return_date,repayment_notes,requester_id,manager_id,approved_amount,approved_by,approved_at,processing_by,processing_at,submitted_at)
		VALUES('PR-2026-000031','processing','recoverable','recoverable',?,?,?,?, 'Tender EMD','2027-06-30','Refund on award',?,?,?,?,CURRENT_TIMESTAMP,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		catID, nil, headID, 500000, actor.ID, actor.ID, 500000, actor.ID, actor.ID)
	if err != nil {
		t.Fatalf("seed processing request: %v", err)
	}
	reqID, _ := res.LastInsertId()

	if _, err := s.RecordPaymentForRequest(ctx, actor, reqID, PaymentInput{HeadID: headID, PaidOn: "2026-08-20", Amount: 500000, VendorPayee: "State PWD"}, "settled", "", nil); err != nil {
		t.Fatalf("RecordPaymentForRequest: %v", err)
	}

	var status, treatment, expReturn, notes string
	var gotCat int64
	if err := s.DB().QueryRowContext(ctx, `SELECT status,treatment,COALESCE(expected_return_date,''),repayment_notes,recoverable_category_id FROM payment_requests WHERE id=?`, reqID).
		Scan(&status, &treatment, &expReturn, &notes, &gotCat); err != nil {
		t.Fatal(err)
	}
	if status != "completed" {
		t.Fatalf("status = %q, want completed", status)
	}
	if treatment != "recoverable" || gotCat != catID || expReturn != "2027-06-30" || notes != "Refund on award" {
		t.Fatalf("recoverable classification not retained: treatment=%s cat=%d return=%s notes=%s", treatment, gotCat, expReturn, notes)
	}
	rec, err := s.RecoverableReport(ctx, RecoverableReportOptions{From: "2026-08", To: "2026-08"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec) != 1 || rec[0].Status != "completed" || rec[0].Amount != 500000 {
		t.Fatalf("recoverable report after settle = %+v", rec)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails (or passes as a guard)**

Run: `go test ./internal/store/ -run TestRecoverableRequestClosesOnPaymentRetainingClassification -v`
Expected before Phase 3 is present: FAIL (build) — `undefined: (*Store).RecordPaymentForRequest`. If Phase 3 is already merged and correct, this test PASSES immediately and stands as the regression guard (the intended steady state). If it FAILS with a non-`completed` status or lost classification, that is a Phase-3 bug to fix before proceeding.

- [ ] **Step 3: Confirm the guard passes**

Run: `go test ./internal/store/ -run TestRecoverableRequestClosesOnPaymentRetainingClassification -v`
Expected: PASS — `--- PASS: TestRecoverableRequestClosesOnPaymentRetainingClassification`.

- [ ] **Step 4: Commit**

```bash
git add internal/store/recoverables_test.go
git commit -m "test(store): guard recoverable classification survives settlement close"
```

---

### Task 13: Proof-of-absence — no recoverable repayment tracking (X2) + no forfeiture/write-off (X4)

**Files:**
- Test: `internal/app/recoverables_app_test.go` (test only)

**Interfaces:**
- Consumes: `newAppTestServer`, `requireStatus`, `s.postForm`.
- Produces: proof-of-absence tests asserting no repayment route (X2) and no forfeiture/write-off route (X4) exist.

- [ ] **Step 1: Write the test**

Append to `internal/app/recoverables_app_test.go`:

```go
func TestNoRecoverableRepaymentTracking(t *testing.T) { // X2
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	// No repayment surface exists: plausible repayment routes must be unrouted (404).
	for _, path := range []string{"/recoverables/1/repay", "/recoverables/list/repay", "/configuration/recoverable-categories/1/repay"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusNotFound)
		_ = responseBody(t, resp)
	}
	resp := s.postForm("/recoverables/1/repay", url.Values{"amount": {"100000"}})
	if resp.StatusCode != http.StatusNotFound {
		body := responseBody(t, resp)
		t.Fatalf("repayment POST status = %d, want 404 (no repayment path exists); body: %s", resp.StatusCode, body)
	}
	_ = responseBody(t, resp)
}

func TestNoForfeitureRoute(t *testing.T) { // X4
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	// Forfeiture / write-off is out of scope: no route or store method converts a
	// recoverable into a budget expense, so plausible forfeiture paths are unrouted (404).
	for _, path := range []string{"/recoverables/1/forfeit", "/recoverables/1/write-off", "/recoverables/list/forfeit"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusNotFound)
		_ = responseBody(t, resp)
	}
	resp := s.postForm("/recoverables/1/forfeit", url.Values{"reason": {"defaulted"}})
	if resp.StatusCode != http.StatusNotFound {
		body := responseBody(t, resp)
		t.Fatalf("forfeiture POST status = %d, want 404 (no forfeiture/write-off path exists); body: %s", resp.StatusCode, body)
	}
	_ = responseBody(t, resp)
}
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `go test ./internal/app/ -run 'TestNoRecoverableRepaymentTracking|TestNoForfeitureRoute' -v`
Expected: PASS — every candidate repayment and forfeiture/write-off path returns 404, proving neither repayment tracking (X2) nor forfeiture conversion (X4) is built.

- [ ] **Step 3: Commit**

```bash
git add internal/app/recoverables_app_test.go
git commit -m "test(app): assert no recoverable repayment (X2) or forfeiture/write-off (X4) exists"
```

---

### Final verification

- [ ] **Design-system conformance**

Run: `grep -c 'class="badge' internal/app/templates.go`
Expected: `0` (D5 — every status chip is a `.pill`).

Run: `grep -c 'recoverable-categories' internal/app/app.go`
Expected: `1` — only the `POST /configuration/recoverable-categories` registration. Any second hit means the deleted standalone screen survived.

Run: `git diff --stat -- web/static/fervid-ds.css`
Expected: empty — Phase 4 adds no CSS; every class it uses was ported in Phase 0.

- [ ] **Run the full suite with the race detector**

Run: `make test-race`
Expected: PASS — `ok  	fervidbudget/internal/store`, `ok  	fervidbudget/internal/app`, `ok  	fervidbudget/internal/auth`, etc.

- [ ] **Run coverage**

Run: `make test-cover`
Expected: PASS — coverage profile written to `output/coverage.out`.

- [ ] **Vet**

Run: `make vet`
Expected: no output (clean).

---

## Coverage

Every Phase-4 requirement ID from `2026-07-25-payment-requests-coverage.md` mapped to the task and the passing test that proves it.

| ID | Requirement | Task | Test name |
|---|---|---|---|
| T1 | Treatment-first; recoverable branch enforced | Task 4 | `TestValidateRecoverable/valid_baseline` |
| T9 | Employee advance refundable → recoverable (project optional) | Task 4 | `TestValidateRecoverable/employee_advance_project_optional` |
| V1 | Budget vs recoverable classification enforced | Task 4 | `TestValidateRecoverable/missing_category`, `TestCreateRecoverableRequestEnforcesCategoryRules` |
| V2 | Recoverable excluded from budget actuals | Task 5 | `TestGridAndReportExcludeRecoverablePayments` |
| V3 | Separate recoverable payments register/total | Task 6, Task 9 | `TestRecoverablePaymentExcludedFromActualsButInRecoverableReport`, `TestRecoverablesListScreenAndExport` |
| V4 | Admin-configurable recoverable categories | Task 1, Task 2, Task 3, Task 11 | `TestMigrationV5SeedsRecoverableCategories`, `TestRecoverableCategoryCRUD`, `TestListRecoverableCategoriesWithUsageCountsRequests`, `TestConfigurationRecoverableCategoriesFieldset` |
| V5 | Category rules (EMD/PBG→project; ICD→counterparty; emp adv→auto employee) | Task 4 | `TestValidateRecoverable/emd_requires_project`, `TestValidateRecoverable/icd_requires_counterparty` |
| V6 | Records counterparty/employee, expected return date, repayment notes | Task 4, Task 6 | `TestValidateRecoverable/requires_return_date_and_notes`, `TestRecoverablePaymentExcludedFromActualsButInRecoverableReport` |
| V7 | Recoverable closes on payment; retains classification + return info | Task 12 | `TestRecoverableRequestClosesOnPaymentRetainingClassification` |
| V8 | Recoverable may link to a real project | Task 6 | `TestRecoverableReportShowsLinkedProject` |
| X2 | No recoverable repayment tracking (proof of absence) | Task 13 | `TestNoRecoverableRepaymentTracking` |
| X4 | No forfeiture / write-off conversion (proof of absence) | Task 13 | `TestNoForfeitureRoute` |
| **G17** | Recoverable detail screen exists and renders the approved blocks | Task 10 | `TestRecoverableDetailScreen` |
| **G18** | Ageing, four dashboard metrics, by-category and by-counterparty rollups | Task 6, Task 7, Task 8 | `TestRecoverableAgeingLabels`, `TestRecoverableReportAgeingFilterAndOrdering`, `TestRecoverableReportIncludesUnpaidAndExcludesDeadRequests`, `TestRecoverableMetricsFourDashboardNumbers`, `TestRecoverableRollupsByCategoryAndCounterparty`, `TestRecoverablesDashboardRendersMetricsAndRollups` |
| **D6** | Categories live in Configuration; the standalone screen is gone | Task 11 | `TestConfigurationRecoverableCategoriesFieldset`, `TestStandaloneRecoverableCategoriesScreenIsGone` |
| **D5** | No template reintroduces `.badge` | Tasks 8–11 | the `class="badge` assertion inside `TestRecoverablesDashboardRendersMetricsAndRollups`, `TestRecoverablesListScreenAndExport`, `TestRecoverableDetailScreen` |
| Design system | Every `.t-cards` cell carries `data-label` (mobile card restack) | Tasks 8, 9, 11 | `assertTCardsLabelled` inside each screen test |
</content>
