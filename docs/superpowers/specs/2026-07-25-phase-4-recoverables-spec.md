# Phase 4 — Recoverable Payments · Spec

**Parent:** `2026-07-25-payment-requests-overview.md`. **Depends on:** Phase 1 (migration runner + `auth.Manager.Can`/`RequirePermission` new vocabulary), Phase 2 (`payment_requests`, `RequestInput`, request validation, `treatment`/`type`/`recoverable_category_id`), Phase 3 (`payments.request_id`, `payments.settlement`, `RecordPaymentForRequest`, settlement path). **Covers matrix IDs:** T1 (recoverable branch), T9 (recoverable branch), V1, V2, V3, V4, V5, V6, V7, V8, X2, X4.

**Goal:** Add the recoverable-payment path on top of the request workflow: an admin-configurable `recoverable_categories` table with per-category field rules, validation that enforces those rules on `treatment='recoverable'` requests, **exclusion of recoverable payments from budget actuals** (variance grid and reports), a dedicated **Recoverable Payments report** with CSV export behind its own `recoverable_report` resource, and confirmation that a recoverable request **closes on its outgoing payment while retaining its recoverable classification and expected-return information**. Recoverable *repayment* tracking and forfeiture/write-off are explicitly out of scope.

**Currency:** ₹ INR integer paise (`int64`), formatted via `internal/money`.

---

## 1. Migration v4 (recoverable_categories) — consumes the Phase 1 runner (C1)

Phase 1 introduced `internal/store/migrations.go`: an ordered slice `migrations []migration` where `type migration struct { Version int; Name string; Up func(*sql.Tx) error }`, run by `migrate(db *sql.DB)` inside `store.Open` after `db.Exec(schemaSQL)`. `migrate` reads `PRAGMA user_version`, executes each pending migration's `Up` inside a transaction, then sets `PRAGMA user_version = Version`. Helper `columnExists(tx,table,col) (bool,error)` guards column adds.

Phase 4 **appends one entry** `{Version: 4, Name: "recoverable_categories", Up: upRecoverableCategories}` to the `migrations` slice (Phase 1 registered v1, Phase 2 v2, Phase 3 v3; numbers are contiguous and never reordered). `upRecoverableCategories` creates the table with `CREATE TABLE IF NOT EXISTS`, creates the case-insensitive unique name index, and seeds the six default categories with `INSERT … ON CONFLICT(name index) DO NOTHING` so re-running (or opening an already-migrated DB) is a no-op.

## 2. Schema (migration v4) — exactly overview §3 (P4)

```sql
CREATE TABLE IF NOT EXISTS recoverable_categories (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  requires_project INTEGER NOT NULL DEFAULT 0,
  requires_counterparty INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_recoverable_categories_name_nocase
  ON recoverable_categories(lower(name));
```

**Seeded defaults** (`active=1`, in `sort_order`):

| sort | name | requires_project | requires_counterparty |
|---|---|---|---|
| 1 | Employee advance | 0 | 0 |
| 2 | EMD | 1 | 0 |
| 3 | PBG | 1 | 0 |
| 4 | ICD | 0 | 1 |
| 5 | Security deposit | 0 | 0 |
| 6 | Other | 0 | 0 |

`payment_requests.recoverable_category_id` is the **FK target Phase 2 declared as deferred-safe**: Phase 2 created the column (`REFERENCES recoverable_categories(id)`, nullable) but it stays NULL until this Version 4 migration creates and seeds `recoverable_categories`. It is populated only for `treatment='recoverable'` requests.

## 3. Store API (types + methods)

New file `internal/store/recoverables.go`; new types added to `internal/store/models.go`.

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

type RecoverableRow struct {
    Number             string // PR-YYYY-NNNNNN
    Category           string
    Counterparty       string // ICD/deposit counterparty, or employee (requester) name
    Project            string // "" when not linked to a real project (V8)
    Amount             int64  // outgoing paise
    PaidOn             string // YYYY-MM-DD
    ExpectedReturnDate string
    RepaymentNotes     string
    Status             string // request status, e.g. "completed"
    Requester          string
}

type RecoverableReportOptions struct {
    From       string // YYYY-MM, inclusive (filters on payment paid_on month)
    To         string // YYYY-MM, inclusive
    CategoryID int64  // 0 = all categories
    Query      string // matches number/counterparty/project/requester
}
```

Methods on `*Store`:

- `ListRecoverableCategories(ctx context.Context, activeOnly bool) ([]RecoverableCategory, error)` — ordered by `sort_order, name`; `activeOnly` filters `active=1` (form pickers pass `true`, admin screen passes `false`).
- `UpsertRecoverableCategory(ctx context.Context, actor User, id int64, name string, requiresProject, requiresCounterparty, active bool, sortOrder int) (int64, error)` — validates non-empty name; unique case-insensitive name (`ErrDuplicate` via `classify`); audited via `recordAuditTx` (`entity_type="recoverable_category"`, action create/update). No hard delete — deactivation via `active=0` (a category may be referenced by historical requests, mirroring project/head retirement).
- `RecoverableReport(ctx context.Context, opts RecoverableReportOptions) ([]RecoverableRow, error)` — see §5.
- `validateRecoverable(in RequestInput, cat RecoverableCategory) error` — package-private; see §4. Called by Phase 2's `CreateRequest`/`UpdateRequest`.

`Grid` and `Report` are **modified** (not new) to exclude recoverable payments — see §5.

## 4. Recoverable request validation (extends Phase 2, per overview §6)

When a request has `treatment='recoverable'`, Phase 2's `CreateRequest`/`UpdateRequest` resolves the category (`Request(...)` path loads `recoverable_category_id → RecoverableCategory`) and calls `validateRecoverable(in, cat)`. Rules (each returns `fmt.Errorf("%w: …", ErrValidation)`):

| Rule | Condition | Covers |
|---|---|---|
| Category required | `in.RecoverableCategoryID == 0` → error | V1, T1 |
| Project required for EMD/PBG | `cat.RequiresProject && in.ProjectID == 0` → error | V5 |
| Counterparty required for ICD | `cat.RequiresCounterparty && trim(in.Counterparty) == ""` → error | V5 |
| Expected return date required | `!validDate(in.ExpectedReturnDate)` → error | V6 |
| Repayment notes required | `trim(in.RepaymentNotes) == ""` → error | V6 |
| Employee advance: project optional, requester auto-recorded | `in.Type == "employee_advance"` uses the seeded *Employee advance* category (`RequiresProject=0`, so project stays optional); the create path auto-fills `counterparty = requester name` when blank | T9, V5 |

`vendor_invoice`/`vendor_advance`/`reimbursement` and `employee_advance` **budget** requests keep their Phase-2 rules unchanged (project+head required, etc.); Phase 4 only adds the `treatment='recoverable'` branch. Treatment-first-then-type UI (T1) is the Phase-2 form; Phase 4 enforces the recoverable side server-side.

## 5. Exclusion from budget actuals + Recoverable report (V2, V3, V8)

**Rule:** a payment whose linked request is recoverable (`payment_requests.treatment='recoverable'`, joined via `payments.request_id`) is money that leaves the company temporarily; it is **not** a budget expense and must never inflate grid/report actuals. Historical payments (`request_id IS NULL`) and payments linked to `treatment='budget'` requests are counted exactly as before.

**`Store.Grid` SQL change** — the actual-sum `CASE` and the "head has activity" `EXISTS` both add a `LEFT JOIN payment_requests` and filter `COALESCE(pr.treatment,'') <> 'recoverable'`:

```sql
COALESCE(SUM(CASE WHEN py.voided_at IS NULL
                   AND COALESCE(pr.treatment,'') <> 'recoverable'
                  THEN py.amount ELSE 0 END),0)
...
LEFT JOIN payment_requests pr ON pr.id = py.request_id
...
OR EXISTS(SELECT 1 FROM payments px
          LEFT JOIN payment_requests pxr ON pxr.id = px.request_id
          WHERE px.head_id=h.id AND substr(px.paid_on,1,7)=?
            AND COALESCE(pxr.treatment,'') <> 'recoverable')
```

`Report` iterates `Grid` per month, so it inherits the exclusion with no additional SQL; a report-level test still asserts the exclusion end to end.

**`RecoverableReport`** surfaces the excluded money separately. It joins recoverable requests to their (single, non-voided) linked payment:

```sql
SELECT pr.number, COALESCE(rc.name,''), COALESCE(pr.counterparty,''), COALESCE(p.name,''),
       py.amount, py.paid_on, COALESCE(pr.expected_return_date,''),
       COALESCE(pr.repayment_notes,''), pr.status, COALESCE(u.name,'')
FROM payment_requests pr
JOIN payments py ON py.request_id = pr.id AND py.voided_at IS NULL
LEFT JOIN recoverable_categories rc ON rc.id = pr.recoverable_category_id
LEFT JOIN projects p ON p.id = pr.project_id
LEFT JOIN users u ON u.id = pr.requester_id
WHERE pr.treatment='recoverable'
  AND substr(py.paid_on,1,7) BETWEEN ? AND ?
ORDER BY py.paid_on DESC, pr.number
```

The `LEFT JOIN projects` renders `Project=""` when the recoverable is not tied to a real project and the project name when it is (V8). Optional `CategoryID`/`Query` filters append `AND` clauses.

## 6. Close-on-payment retains classification (V7)

No new code — a **regression guard**. Phase 3's `RecordPaymentForRequest(…, settlement="settled", …)` moves the request to `status='completed'` (overview §6). Phase 4 asserts that after settlement of a recoverable request, `treatment` stays `'recoverable'`, `recoverable_category_id`, `expected_return_date`, and `repayment_notes` are unchanged, and the paid row appears in `RecoverableReport` — proving the classification and return info survive settlement.

## 7. UI / routes (permission-driven)

All new routes register in `App.routes` behind Phase 1's `RequirePermission(resource, action, next)` (new vocabulary); POSTs wrap `a.withCSRF`. Nav links render only when `.Perms.Can "resource" "view"` is true for the session (Phase 1 exposed `PageData.Perms` with a `Can` method for permission-driven nav; there is no global `can` template function).

- `GET /reports/recoverable` → `RequirePermission("recoverable_report","view")` → `recoverableReport` handler → `recoverable_report` template (metric strip: total recoverable outgoing; table of `RecoverableRow`; From/To/category filters).
- `GET /reports/recoverable.csv` → `RequirePermission("recoverable_report","export")` → `exportRecoverable` handler (CSV: `Number,Category,Counterparty,Project,Amount,Paid On,Expected Return,Status,Requester,Repayment Notes`; audited `entity_type="recoverable_report", action="export"`).
- `GET /recoverable-categories` → `RequirePermission("recoverable_category","view")` → `recoverableCategories` handler → `recoverable_categories` template (add form + editable rows: name, requires-project, requires-counterparty, active, order — mirrors the Projects screen).
- `POST /recoverable-categories` → `RequirePermission("recoverable_category","edit")` + `withCSRF` → `recoverableCategorySave` (`UpsertRecoverableCategory`, audited).

Seeded roles (overview §5): **Accounts** holds `recoverable_report`{view,export}; **Admin** holds `recoverable_category`{view,create,edit,delete} and everything else. A user without the resource is blocked server-side (403 by URL), not merely menu-hidden.

## 8. Out of scope (must remain absent) — X2 + X4 (forfeiture)

- **X2 — recoverable repayment tracking:** no `repaid`/`balance` columns, no repayment route or store method, and `RecoverableRow` carries no "amount repaid"/"outstanding" field. Proven by a proof-of-absence test (a plausible repayment route returns 404) and this note.
- **Forfeiture / write-off conversion (X4):** no route or store method that converts a recoverable into a budget expense. Proven by a proof-of-absence test (a plausible forfeiture/write-off route returns 404) and this note.

The report is read-only reconciliation; recoverables show as `completed` with their expected-return date, and follow-up recovery is a manual/offline process in this release.

## 9. Acceptance criteria (P4 done when all hold)

- Opening a fresh DB creates `recoverable_categories` at `user_version ≥ 4` with the six seeded rows and correct flags; re-opening is idempotent (still six rows).
- A `treatment='recoverable'` request is rejected without a category; EMD/PBG without a project; ICD without a counterparty; any recoverable without an expected-return date or repayment notes; an employee-advance recoverable with no project but with return date + notes is accepted.
- A recoverable payment is **absent** from `Grid` and `Report` actuals but **present** in `RecoverableReport` — asserted in one test.
- Admin creates/edits a recoverable category through `/recoverable-categories`; a user lacking `recoverable_category` view is 403 by URL. Accounts can view/export `/reports/recoverable`; a user lacking `recoverable_report` view is 403.
- Settling a recoverable request sets status `completed` while `treatment`/category/expected-return/notes are retained (V7).
- No repayment route or method exists (X2); no forfeiture/write-off route or method exists (X4).
- `make test-race` and `make test-cover` green.

## 10. Test plan (TDD)

Store unit tests in `internal/store/recoverables_test.go` (migration/seed, CRUD, validation, Grid/Report exclusion + RecoverableReport combined assertion, close-on-payment). App integration tests in `internal/app/recoverables_app_test.go` (report screen + CSV, categories CRUD screen, permission 403s, X2 + X4 proof-of-absence) using `newAppTestServer`. Each behaviour is a red→green→commit cycle in `2026-07-25-phase-4-recoverables-plan.md`. Every covered ID maps to a named passing test in that plan's Coverage table.
</content>
</invoke>
