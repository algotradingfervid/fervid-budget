# Phase 2 — Request Workflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

## Amendment log

Amended **2026-07-25** against `docs/superpowers/specs/2026-07-25-design-system-adoption-spec.md` §4 "Per-phase amendments → Phase 2". Every change below carries the adoption-spec decision or gap ID that mandates it. Nothing else in the plan was altered: backend task bodies and their test code are carried over verbatim where no amendment touches them.

| # | Change | Why |
|---|---|---|
| A1 | Migration renumbered **v2 → v3**; the global sequence is now v1 permissions (P1), v2 vendors (P1V), v3 requests (P2), v4 payments-linking (P3), v5 recoverable categories (P4), v6 notification settings (P5). | D3 — the vendor master became its own phase and took v2. |
| A2 | `payment_requests` gains `vendor_id INTEGER REFERENCES vendors(id)`; `vendor_payee TEXT` is **retained** as the payee snapshot for reimbursement and employee-advance requests, where no vendor row exists. | D3, G5, and the P1V "Downstream contract for Phase 2". |
| A3 | `status` loses its `DEFAULT 'draft'` and gains `CHECK (status <> 'draft')`. The writer must always name a status, and no row can ever hold `draft`. | **D1** — there are no drafts. |
| A4 | New columns: `urgency_reason`, `attachment_exception_reason`, `short_title`, `invoice_no`, `invoice_date`, `expense_date`, `advance_reason`, `cancel_reason`. | G7, G10, and the field list in `request-new-form.html` / `request-detail-*.html`. |
| A5 | Statuses `cancellation_requested` and `cancelled` added to the status enum and to `canTransition`; `draft` removed from both. | G3, D1. |
| A6 | **Old Tasks 4 and 5 merged into one atomic create-and-submit** (new Task 5). `CreateRequest` writes the row already `pending`, calls `NextRequestNumber` in the same transaction, stamps `submitted_at`, and inserts the staged attachments. `SubmitRequest` survives only for `returned → pending` resubmission. The "Save draft" button and `Reraise`→draft are deleted; `ReraiseRequest` now creates a new **pending** request. | **D1**. |
| A7 | Coverage row **L1 inverts** from "Draft (private to employee)" to "No draft state exists", proved by `TestNoDraftStateExists`. | D1, adoption spec §5. |
| A8 | New task: **cancellation flow** (new Task 15 store, new Task 29 screens) — employee asks, payment freezes, manager accepts or declines, manager may cancel outright with a reason. | G1, G2, G3. |
| A9 | New task: **duplicate-check endpoint** (new Task 18 store, new Task 23 endpoint) — fires on amount + payee blur, renders a `.banner.warn` fragment of matches over the last 30 days, and never blocks submission. | G6. |
| A10 | New task: **self-approval block** (new Task 8) — the approver list excludes the requester and the store rejects `ManagerID == RequesterID` with `ErrValidation`. | G8. |
| A11 | New task: **default approver per user** (new Task 9) — `users.default_approver_id`, pre-selected in the form, overridable. | G9. |
| A12 | New task: **urgency reason required when urgent is set** (new Task 6), driven by the `urgency_mode` setting. | G7. |
| A13 | New task: **attachment exception reason** (new Task 7) — when `require_attachments` is on and no file is attached, the submit asks for a reason instead of blocking. | G10. |
| A14 | New task: **Configuration screen shell** (new Task 30) — `GET/POST /configuration`, permission `config`{view,edit}, one `<fieldset>` per section, generic `app_settings`-backed save. Phase 2 owns the shell plus the Numbering, Attachments, Urgency, Approvals and Payments sections; Phases 3–5 append their own fieldsets. | D6, G21. |
| A15 | **Old Task 13 split into nine per-screen tasks** (new Tasks 19–29): shared plumbing, request-new-type, request-new-form (+ the htmx `requestFormFields` partial and a `renderPartial` that omits the shell), request-submitted, requests-list, approvals-list on its own route `GET /approvals`, request-detail, request-edit, request-returned. | Adoption spec §4 Phase 2. |
| A16 | **Adaptive form:** request type is a **route parameter** (`GET /requests/new?type=…`), never a `<select>`. Treatment and recoverable category swap a fragment through htmx `hx-get="/requests/new/fields"`; the same fieldsets carry `data-when` for the no-JS path. Server-side validation stays authoritative — `hidden` is not validation. | UI/UX design; Phase 0 global constraint. |
| A17 | **Old Task 14 dashboard rebuilt** against `dashboard.html` using `.work-areas > section.area` with `.a-head`/`.a-list`/`.a-foot`, not a bare `.metric-strip`. | Adoption spec §4 Phase 2. |
| A18 | **Old Task 15 Playwright selectors updated** — the spec pinned `.badge` (renamed to `.pill` by D5) and `getByLabel('Type')` (the type `<select>` is gone under A16). | D5, A16. |
| A19 | Every UI task now renders the Phase 0 component classes — `.req-card`, `.pill`, `.waiting`, `.thread`, `.money-field` + `.in-words`, `.choice`, `.combo`, `.action-bar`, `.overlay > .sheet`, `table.t-cards` with `data-label`, `.segmented`, `.m-filters` + filter sheet. History and conversation are **one merged chronological `.thread`**, not two lists. | Phase 0; UI/UX §9. |
| A20 | Task 2 `NextRequestNumber` now reads `number_prefix` / `number_width` / `number_year_mode` from `app_settings`. Its pinned signature is unchanged. | D6 — the Configuration Numbering fieldset has to control something real. |
| A21 | `payment_requests` also gains `recoverable_category TEXT` (the category **code**). This is one column beyond the adoption spec's literal list, and it is unavoidable: §4 requires Phase 2 validation to enforce "EMD/PBG project, ICD counterparty", which needs the category identity, and `recoverable_categories` is a **Phase 4** table. Phase 4's v5 migration back-fills `recoverable_category_id` from this code and replaces the Phase-2 `recoverableCategoryRules` map with rows from its table. | G-none; forced by adoption spec §4 Phase 2 "Task 3 validation gains … EMD/PBG project, ICD counterparty". |

**Cross-phase notes raised by this amendment — both now RESOLVED (2026-07-25):**

- **Cancel verbs — accepted, handed to Phase 1.** Phase 1 Task 3's `resourceActions` adds `request`{cancel} and `approval`{cancel} alongside the `vendor`/`vendor_bank`/`reservation`/`config` entries D2 assigns it. The canonical total goes 64 → **66** pairs; Phase 1's vocabulary tests and Global Constraints table were updated with it. Phase 2's cancellation routes register behind those two verbs and must not invent their own.

- **`users.default_approver_id` — Phase 1 owns it.** Phase 1 Task 12 creates the column in migration **v1** and ships `User.DefaultApproverID int64` (0 = none) plus `SetUserDefaultApprover(ctx, actor, userID, approverID) error`, which already rejects self, unknown and inactive approvers. Phase 1 ships first and its `admin-users.html` renders the field, so the column must exist by then. Consequences for this phase:
  - Task 1 **keeps** its `columnExists`-guarded `ALTER TABLE` verbatim. It becomes a no-op on any database Phase 1 has migrated, and it keeps this plan runnable in isolation. The guard is what makes double ownership harmless — do not remove it, and do not convert it to an unguarded `ALTER`, which would fail hard.
  - Task 9 must **not** declare `SetDefaultApprover` or a `DefaultApprover(ctx, userID)` reader. `SetUserDefaultApprover` is the canonical writer and `User.DefaultApproverID` the canonical read — a second pair of methods over one column is how the two drift apart. Where Task 9's code calls its own names, call Phase 1's instead; the request form's pre-selection reads `User.DefaultApproverID` off the already-loaded user.
  - Task 9's `scanUser` change is still required if Phase 2 is implemented standalone, but on the real branch Phase 1 Task 12 has already appended `default_approver_id` to the three user SELECT lists. Check `scanUser` before editing it; appending the column twice is a scan-arity panic, not a compile error.

---

**Goal:** Build the request → approval → cancellation workflow (type-first adaptive form, atomic create-and-submit with no drafts, auto request numbers, requester + manager lifecycles, one merged history-and-conversation thread, permission-gated queues and dashboard, and the Configuration screen shell) on the Phase 0 design system.

**Architecture:** Store-first. A new `internal/store/requests.go` holds the `Request` domain (types in `models.go`, tables via migration **v3** appended to the `migrations` slice). Every mutation follows the house pattern — `BeginTx` + `defer tx.Rollback()` + `recordAuditTx(entity_type="payment_request")` + `tx.Commit()`. HTTP handlers in `internal/app/app.go` register behind Phase-1 `RequirePermission(resource,action)` + `withCSRF`, resolve data scope via `auth.Scope`, and render templates in `internal/app/templates.go` built **only** from Phase 0 component classes. htmx fragments render through `renderPartial`, which omits the shell.

**Tech Stack:** Go 1.22+ (`net/http` ServeMux), `modernc.org/sqlite`, `html/template`, htmx, `internal/money`, Playwright (e2e).

## Global Constraints

- **Depends on Phase 0, Phase 1 and Phase 1V.** Phase 0 supplies `fervid-ds.css` (every component class used below), `fervid-app.js` (`[data-when]`, overlay, money field, accordion), the `Shell`/nav model, `money.InWords`, comma-tolerant `money.ParsePaise`, and `HX-Request` shell skipping. Phase 1 supplies the permission engine. Phase 1V supplies `vendors`, `SearchVendors` and `GET /vendors/search`. **Do not re-specify or re-invent any of them.**
- Migration version **v3**. The reserved global sequence is v1 permissions, v2 vendors, **v3 requests**, v4 payments-linking, v5 recoverable categories, v6 notification settings.
- **There are no drafts (D1).** A request exists only once submitted and takes its number at that moment. No `draft` status, no "Save draft" control, no `SaveDraft`-shaped store method. `ReraiseRequest` produces a new **pending** request.
- Currency is ₹ INR stored as integer paise (`int64`), formatted with `internal/money`. Booleans are INTEGER 0/1; dates are TEXT `YYYY-MM-DD`; timestamps are DATETIME.
- Consume Phase 1 verbatim: `auth.Manager.Can(u store.User, resource, action string) bool`, `auth.Manager.Scope(u store.User, resource string) string`, `auth.Manager.Permissions(u store.User) store.PermissionSet`, `store.EffectivePermissions(ctx, userID int64) (PermissionSet, error)`, the migration runner (`migrations` slice of `migration{Version int; Name string; Up func(*sql.Tx) error}`, `migrate(db *sql.DB) error`, `columnExists(tx *sql.Tx, table, col string) (bool, error)`), and the four seeded roles (`Requester`, `Manager`, `Accounts`, `Admin`).
- Produce these exact signatures: `NextRequestNumber(tx,year)`, `CreateRequest`, `UpdateRequest`, `SubmitRequest`, `WithdrawRequest`, `ApproveRequest(ctx,actor,id,approvedAmount int64,note string) error`, `ReturnRequest`, `RejectRequest`, `ReassignRequest`, `ReraiseRequest`, `RequestCancellation`, `DecideCancellation`, `CancelRequest`, `AddRequestComment`, `RequestThread`, `SimilarRequests`, `ListApprovers`, `SetDefaultApprover`, `Request(ctx,id) (Request,error)`, `ListRequests(ctx,RequestListOptions) ([]Request,error)`; plus types `Request`, `RequestInput`, `RequestListOptions`, `ThreadEntry`, `SimilarRequestOptions`.
- Errors reuse `store.ErrNotFound|ErrForbidden|ErrValidation|ErrDuplicate`; wrap validation as `fmt.Errorf("%w: message", ErrValidation)`; map DB errors via `classify`.
- Store mutations: `func (s *Store) X(ctx, actor User, …)`; `s.db.BeginTx(ctx,nil)` + `defer tx.Rollback()` + `tx.Commit()`; every mutation writes `recordAuditTx(ctx, tx, AuditInput{…, EntityType:"payment_request", Before, After})`. The audit table **is** the thread's event stream — write a summary a human would want to read.
- Handlers register in `App.routes` behind `RequirePermission`; POSTs behind `a.withCSRF`; render via `a.render`/`a.renderStatus`, fragments via `a.renderPartial`; user errors via `a.respondStoreError`/`friendly`.
- **`hidden` is not validation.** Every `data-when` reveal and every htmx-swapped fieldset must be re-enforced in `validateRequestInput`. A hand-rolled POST that fills a hidden field must be rejected by the store, not by the browser.
- **No bank or account fields on any request screen.** Bank details live on the vendor record behind `vendor_bank` (Phase 1V).
- TDD required: Go unit via `newTestStore(t)`; HTTP via `newAppTestServer(t)`; e2e via `tests/e2e/*.spec.ts`. Each task is red → green → commit. Run `make test-race` and `make test-cover` (Go) / `make test-e2e` (browser).
- Commits: stage only the files you touched, by explicit path. The tree contains unrelated modified files — never `git add -A`.
- Do **not** build copy-previous-request; do **not** build bulk approval; do **not** build recoverable budget-exclusion (Phase 4). Phase 2 captures recoverable fields only.

## Screens owned by this phase

| Mockup | Route | Task |
|---|---|---|
| `dashboard.html` | `GET /dashboard` | 31 |
| `request-new-type.html` | `GET /requests/new` | 20 |
| `request-new-form.html` | `GET /requests/new?type=…` (+ `GET /requests/new/fields`) | 21 |
| `request-duplicate-warning.html` | `POST /requests/duplicate-check` fragment | 23 |
| `request-submitted.html` | `GET /requests/{id}/submitted` | 22 |
| `requests-list.html` | `GET /requests` | 24 |
| `approvals-list.html` | `GET /approvals` | 25 |
| `request-detail-employee.html`, `request-detail-manager.html` | `GET /requests/{id}` | 26 |
| `request-edit.html` | `GET /requests/{id}/edit` | 27 |
| `request-returned.html` | `GET /requests/{id}` when `status == "returned"` | 28 |
| `request-cancel.html` | `GET /requests/{id}/cancel` | 29 |
| `manager-cancellation-decision.html` | `GET /requests/{id}/cancellation` | 29 |
| `admin-configuration.html` | `GET/POST /configuration` | 30 |

---

### Task 1: Migration v3 — request tables, vendor link, no-draft schema

**Files:**
- Modify: `internal/store/migrations.go` (append `{Version: 3, ...}` to the `migrations` slice; v1 is Phase 1, v2 is Phase 1V)
- Test: `internal/store/requests_schema_test.go`

**Interfaces:**
- Consumes: Phase-1 `migrations []migration`, `migrate(db)`, `columnExists`, `Open` (runs `migrate` after `schemaSQL`); Phase-1V `vendors`.
- Produces: tables `payment_requests`, `request_attachments`, `request_comments`, `request_number_seq`, `app_settings` present after `store.Open`, at `PRAGMA user_version = 3`; `users.default_approver_id`; `app_settings` seeded with the Phase-2 configuration defaults.

- [ ] **Step 1: Write the failing test**

```go
package store

import "testing"

func TestMigrationV3CreatesRequestTables(t *testing.T) {
	s := newTestStore(t)
	for _, table := range []string{"payment_requests", "request_attachments", "request_comments", "request_number_seq", "app_settings"} {
		var name string
		if err := s.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("table %q missing after migrate: %v", table, err)
		}
	}
	var version int
	if err := s.DB().QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 3 {
		t.Fatalf("user_version = %d, want >= 3", version)
	}
	// A2/A4/A21: the design-system columns exist.
	for _, col := range []string{"vendor_id", "vendor_payee", "short_title", "invoice_no", "invoice_date",
		"expense_date", "advance_reason", "urgency_reason", "attachment_exception_reason",
		"cancel_reason", "recoverable_category", "recoverable_category_id"} {
		var n int
		if err := s.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('payment_requests') WHERE name=?`, col).Scan(&n); err != nil || n != 1 {
			t.Fatalf("payment_requests.%s missing (n=%d err=%v)", col, n, err)
		}
	}
	// G9: every user may carry a default approver.
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='default_approver_id'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("users.default_approver_id missing (n=%d err=%v)", n, err)
	}
	// D1: status has no default and 'draft' is refused by the schema itself.
	if _, err := s.DB().Exec(`INSERT INTO payment_requests(number,treatment,type,amount,purpose,requester_id,manager_id) VALUES('PR-NODEF','budget','vendor_invoice',1,'p',1,1)`); err == nil {
		t.Fatal("status accepted a default; D1 requires the writer to name a status")
	}
	if _, err := s.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,amount,purpose,requester_id,manager_id) VALUES('PR-DRAFT','draft','budget','vendor_invoice',1,'p',1,1)`); err == nil {
		t.Fatal("status 'draft' was accepted; D1 removed drafts entirely")
	}
	// number is UNIQUE
	if _, err := s.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,amount,purpose,requester_id,manager_id) VALUES('PR-DUP','pending','budget','vendor_invoice',1,'p',1,1)`); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if _, err := s.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,amount,purpose,requester_id,manager_id) VALUES('PR-DUP','pending','budget','vendor_invoice',1,'p',1,1)`); err == nil {
		t.Fatal("duplicate request number was accepted; UNIQUE(number) missing")
	}
	// D6: the Configuration screen's Phase-2 keys are seeded.
	for key, want := range map[string]string{
		"number_prefix": "PR", "number_year_mode": "calendar", "number_width": "6",
		"require_attachments": "0", "attachment_max_mb": "10",
		"urgency_mode": "reason", "allow_approver_choice": "1",
		"allow_direct_payments": "0",
	} {
		var got string
		if err := s.DB().QueryRow(`SELECT value FROM app_settings WHERE key=?`, key).Scan(&got); err != nil {
			t.Fatalf("app_settings[%q] missing: %v", key, err)
		}
		if got != want {
			t.Fatalf("app_settings[%q] = %q, want %q", key, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestMigrationV3CreatesRequestTables -v`
Expected: FAIL — `table "payment_requests" missing after migrate`.

- [ ] **Step 3: Write minimal implementation**

Append this migration to the `migrations` slice in `internal/store/migrations.go` (after Phase 1's `{Version: 1, ...}` and Phase 1V's `{Version: 2, ...}`; numbers are contiguous and never reordered):

```go
	{
		Version: 3,
		Name:    "requests",
		Up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS payment_requests (
  id INTEGER PRIMARY KEY,
  number TEXT NOT NULL UNIQUE,
  -- D1: there are no drafts. status has no DEFAULT, so every writer must name
  -- one, and the CHECK makes 'draft' unrepresentable for the life of the table.
  status TEXT NOT NULL CHECK (status <> 'draft'),
  treatment TEXT NOT NULL,
  type TEXT NOT NULL,
  recoverable_category TEXT NOT NULL DEFAULT '',
  recoverable_category_id INTEGER REFERENCES recoverable_categories(id),
  project_id INTEGER REFERENCES projects(id),
  head_id INTEGER REFERENCES heads(id),
  vendor_id INTEGER REFERENCES vendors(id),
  vendor_payee TEXT NOT NULL DEFAULT '',
  short_title TEXT NOT NULL DEFAULT '',
  amount INTEGER NOT NULL,
  purpose TEXT NOT NULL,
  needed_by TEXT,
  invoice_no TEXT NOT NULL DEFAULT '',
  invoice_date TEXT,
  expense_date TEXT,
  advance_reason TEXT NOT NULL DEFAULT '',
  counterparty TEXT NOT NULL DEFAULT '',
  expected_return_date TEXT,
  repayment_notes TEXT NOT NULL DEFAULT '',
  urgent INTEGER NOT NULL DEFAULT 0,
  urgency_reason TEXT NOT NULL DEFAULT '',
  attachment_exception_reason TEXT NOT NULL DEFAULT '',
  requester_id INTEGER NOT NULL REFERENCES users(id),
  manager_id INTEGER NOT NULL REFERENCES users(id),
  approved_amount INTEGER,
  approved_by INTEGER REFERENCES users(id),
  approved_at DATETIME,
  decision_reason TEXT NOT NULL DEFAULT '',
  cancel_reason TEXT NOT NULL DEFAULT '',
  on_hold INTEGER NOT NULL DEFAULT 0,
  hold_reason TEXT NOT NULL DEFAULT '',
  processing_by INTEGER REFERENCES users(id),
  processing_at DATETIME,
  reminder_last_sent DATETIME,
  submitted_at DATETIME,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_requests_status ON payment_requests(status);
CREATE INDEX IF NOT EXISTS idx_requests_manager ON payment_requests(manager_id, status);
CREATE INDEX IF NOT EXISTS idx_requests_requester ON payment_requests(requester_id, status);
CREATE INDEX IF NOT EXISTS idx_requests_vendor ON payment_requests(vendor_id, created_at);

CREATE TABLE IF NOT EXISTS request_attachments (
  id INTEGER PRIMARY KEY,
  request_id INTEGER NOT NULL REFERENCES payment_requests(id),
  original_name TEXT NOT NULL,
  stored_path TEXT NOT NULL,
  mime_type TEXT,
  size_bytes INTEGER NOT NULL,
  uploaded_by INTEGER NOT NULL REFERENCES users(id),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_request_attachments_request ON request_attachments(request_id);

CREATE TABLE IF NOT EXISTS request_comments (
  id INTEGER PRIMARY KEY,
  request_id INTEGER NOT NULL REFERENCES payment_requests(id),
  author_id INTEGER NOT NULL REFERENCES users(id),
  body TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_request_comments_request ON request_comments(request_id);

CREATE TABLE IF NOT EXISTS request_number_seq (
  year TEXT PRIMARY KEY,
  last INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS app_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);
-- D6: the Phase-2 half of the Configuration screen. Phases 3-5 append keys.
INSERT INTO app_settings(key,value) VALUES
  ('number_prefix','PR'),
  ('number_year_mode','calendar'),
  ('number_width','6'),
  ('require_attachments','0'),
  ('attachment_max_mb','10'),
  ('urgency_mode','reason'),
  ('allow_approver_choice','1'),
  ('allow_direct_payments','0'),
  ('payment_modes','NEFT, RTGS, UPI, Cheque, Cash, Card, DD')
ON CONFLICT(key) DO NOTHING;
`); err != nil {
				return err
			}
			// G9: default approver per employee. Additive column, guarded so a
			// re-run is safe even if user_version drifted from the physical schema.
			exists, err := columnExists(tx, "users", "default_approver_id")
			if err != nil {
				return err
			}
			if !exists {
				if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN default_approver_id INTEGER REFERENCES users(id)`); err != nil {
					return err
				}
			}
			return nil
		},
	},
```

> **B6 — deferred FK note:** `recoverable_category_id INTEGER REFERENCES recoverable_categories(id)` keeps the `REFERENCES` clause even though `recoverable_categories` is a Phase-4 table. SQLite defers FK resolution — the parent table need not exist at `CREATE TABLE` time and NULL foreign keys are never enforced — so the column exists, stays NULL until Phase 4 seeds `recoverable_categories`, and introduces no Phase-4 dependency here. `recoverable_category TEXT` carries the category **code** meanwhile (A21); Phase 4's v5 migration back-fills the id from it.
>
> **vendor_id note:** `vendors` really does exist by now — Phase 1V's v2 runs first. The FK is live, not deferred.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestMigrationV3CreatesRequestTables -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/migrations.go internal/store/requests_schema_test.go
git commit -m "feat(store): migration v3 creates request tables with vendor link and no draft state"
```

---

### Task 2: Auto request number (`PR-YYYY-NNNNNN`, monotonic per year, configurable)

**Files:**
- Create: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `*sql.Tx`, `request_number_seq` and the numbering settings in `app_settings` (Task 1).
- Produces: `func NextRequestNumber(tx *sql.Tx, year string) (string, error)` — returns `<prefix>-<year>-NNNNNN`, incrementing `request_number_seq.last` for that year atomically inside the caller's transaction; `func requestNumberYear(tx *sql.Tx, now time.Time) (string, error)` — resolves the year segment from `number_year_mode`.

**Amendment (A20):** the signature is unchanged, but the prefix, the zero-pad width and the year segment now come from `app_settings`, so the Configuration screen's Numbering fieldset (Task 30) governs something real instead of being decorative. Both reads happen on the caller's `tx`, so a number reserved inside a transaction always uses the format in force at that instant.

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"strings"
	"testing"
	"time"
)

func TestNextRequestNumberIsMonotonicPerYear(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	got := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		n, err := NextRequestNumber(tx, "2026")
		if err != nil {
			t.Fatalf("NextRequestNumber: %v", err)
		}
		got = append(got, n)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	want := []string{"PR-2026-000001", "PR-2026-000002", "PR-2026-000003"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("number[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	tx2, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback()
	n, err := NextRequestNumber(tx2, "2027")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(n, "PR-2027-000001") {
		t.Fatalf("new-year number = %q, want PR-2027-000001", n)
	}
}

// A20/D6: the Configuration numbering fieldset actually drives the format.
func TestNextRequestNumberHonoursConfiguredFormat(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DB().Exec(`UPDATE app_settings SET value='REQ' WHERE key='number_prefix'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE app_settings SET value='4' WHERE key='number_width'`); err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	n, err := NextRequestNumber(tx, "2026")
	if err != nil {
		t.Fatal(err)
	}
	if n != "REQ-2026-0001" {
		t.Fatalf("configured number = %q, want REQ-2026-0001", n)
	}
}

func TestRequestNumberYearSegmentModes(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	march := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	april := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if y, err := requestNumberYear(tx, march); err != nil || y != "2026" {
		t.Fatalf("calendar year = %q, %v; want 2026", y, err)
	}
	if _, err := tx.Exec(`UPDATE app_settings SET value='financial' WHERE key='number_year_mode'`); err != nil {
		t.Fatal(err)
	}
	if y, err := requestNumberYear(tx, march); err != nil || y != "2025-26" {
		t.Fatalf("financial year (March) = %q, %v; want 2025-26", y, err)
	}
	if y, err := requestNumberYear(tx, april); err != nil || y != "2026-27" {
		t.Fatalf("financial year (April) = %q, %v; want 2026-27", y, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestNextRequestNumber|TestRequestNumberYearSegmentModes' -v`
Expected: FAIL — `undefined: NextRequestNumber`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/store/requests.go`. **Import only what this task uses** — Go fails the build on an unused import, and the file grows an import per task: `context` and `fervidbudget/internal/money` arrive with Task 5, `encoding/json` and `sort` with Task 16.

```go
package store

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// settingInTx reads a runtime setting on the caller's transaction, falling back
// to def when the key is absent. Numbering must see the format that is in force
// at the instant the number is reserved, so it reads inside the same tx.
func settingInTx(tx *sql.Tx, key, def string) (string, error) {
	var v string
	err := tx.QueryRow(`SELECT value FROM app_settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows || (err == nil && strings.TrimSpace(v) == "") {
		return def, nil
	}
	return v, err
}

// requestNumberYear resolves the year segment of a request number from the
// number_year_mode setting: "calendar" (2026), "financial" (2025-26, April
// start) or "none" (empty segment).
func requestNumberYear(tx *sql.Tx, now time.Time) (string, error) {
	mode, err := settingInTx(tx, "number_year_mode", "calendar")
	if err != nil {
		return "", err
	}
	switch mode {
	case "none":
		return "", nil
	case "financial":
		start := now.Year()
		if now.Month() < time.April {
			start--
		}
		return fmt.Sprintf("%d-%02d", start, (start+1)%100), nil
	default:
		return now.Format("2006"), nil
	}
}

// NextRequestNumber reserves the next monotonic request number for a year
// inside the caller's transaction and returns it as <prefix>-<year>-NNNNNN.
func NextRequestNumber(tx *sql.Tx, year string) (string, error) {
	prefix, err := settingInTx(tx, "number_prefix", "PR")
	if err != nil {
		return "", err
	}
	widthText, err := settingInTx(tx, "number_width", "6")
	if err != nil {
		return "", err
	}
	width, err := strconv.Atoi(strings.TrimSpace(widthText))
	if err != nil || width < 1 || width > 12 {
		width = 6
	}
	if _, err := tx.Exec(`INSERT INTO request_number_seq(year,last) VALUES(?,0) ON CONFLICT(year) DO NOTHING`, year); err != nil {
		return "", err
	}
	var last int64
	if err := tx.QueryRow(`UPDATE request_number_seq SET last=last+1 WHERE year=? RETURNING last`, year).Scan(&last); err != nil {
		return "", err
	}
	if year == "" {
		return fmt.Sprintf("%s-%0*d", prefix, width, last), nil
	}
	return fmt.Sprintf("%s-%s-%0*d", prefix, year, width, last), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestNextRequestNumber|TestRequestNumberYearSegmentModes' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): monotonic per-year request numbering driven by app_settings"
```

---
### Task 3: Request types, per-type field validation, status enum without `draft`

**Files:**
- Modify: `internal/store/models.go` (add `Request`, `RequestInput`, `RequestListOptions`, `RequestComment`, `RequestAttachment`, `ThreadEntry`, `ThreadChange`)
- Modify: `internal/store/requests.go` (add `validateRequestInput`, the status enum, transition helpers)
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `ErrValidation`, `validDate` (store.go).
- Produces: `func validateRequestInput(in RequestInput) error`; `var requestTypes`, `var requestStatuses`, `var recoverableCategoryRules`, `func canTransition(from, to string) bool`; the request struct types.

**Amendments applied here:** A2 (`VendorID` replaces free-text payee for vendor types), A4 (short title, invoice number/date, expense date, advance reason), A5 (`cancellation_requested`, `cancelled` in; `draft` out), A21 (`RecoverableCategory` code drives the EMD/PBG project and ICD counterparty rules). Urgency reason (Task 6) and the self-approval rejection (Task 8) are deliberately **not** here — each is its own red-green task.

- [ ] **Step 1: Write the failing test**

```go
func TestValidateRequestInputPerType(t *testing.T) {
	base := func(mut func(*RequestInput)) RequestInput {
		in := RequestInput{
			Treatment: "budget", Type: "vendor_invoice", ShortTitle: "July switchgear",
			ProjectID: 1, HeadID: 2, Amount: 1000, Purpose: "buy", ManagerID: 7,
			VendorID: 3, InvoiceNo: "SE/26-27/1184", InvoiceDate: "2026-07-18",
		}
		if mut != nil {
			mut(&in)
		}
		return in
	}
	cases := []struct {
		name string
		in   RequestInput
		ok   bool
	}{
		{"vendor_invoice ok", base(nil), true},
		{"vendor_invoice needs project", base(func(i *RequestInput) { i.ProjectID = 0 }), false},
		{"vendor_invoice needs a vendor row, not free text", base(func(i *RequestInput) { i.VendorID = 0; i.VendorPayee = "Acme" }), false},
		{"vendor_invoice needs invoice number", base(func(i *RequestInput) { i.InvoiceNo = "" }), false},
		{"vendor_invoice needs invoice date", base(func(i *RequestInput) { i.InvoiceDate = "" }), false},
		{"vendor_invoice rejects a malformed invoice date", base(func(i *RequestInput) { i.InvoiceDate = "18-07-2026" }), false},
		{"short title is required", base(func(i *RequestInput) { i.ShortTitle = "  " }), false},
		{"vendor_advance ok", base(func(i *RequestInput) {
			i.Type, i.InvoiceNo, i.InvoiceDate = "vendor_advance", "", ""
			i.AdvanceReason = "40% booking against PO-2026-0417"
		}), true},
		{"vendor_advance needs a reason", base(func(i *RequestInput) {
			i.Type, i.InvoiceNo, i.InvoiceDate = "vendor_advance", "", ""
		}), false},
		{"vendor_advance needs a vendor", base(func(i *RequestInput) {
			i.Type, i.InvoiceNo, i.InvoiceDate, i.VendorID = "vendor_advance", "", "", 0
			i.AdvanceReason = "booking"
		}), false},
		{"reimbursement ok without a vendor", base(func(i *RequestInput) {
			i.Type, i.VendorID, i.InvoiceNo, i.InvoiceDate = "reimbursement", 0, "", ""
			i.ExpenseDate = "2026-07-21"
		}), true},
		{"reimbursement needs the expense date", base(func(i *RequestInput) {
			i.Type, i.VendorID, i.InvoiceNo, i.InvoiceDate = "reimbursement", 0, "", ""
		}), false},
		{"employee_advance budget needs head", base(func(i *RequestInput) {
			i.Type, i.VendorID, i.InvoiceNo, i.InvoiceDate, i.HeadID = "employee_advance", 0, "", "", 0
			i.AdvanceReason = "site mobilisation cash"
		}), false},
		{"employee_advance recoverable needs return date", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "employee_advance", ShortTitle: "Site cash",
				Amount: 1000, Purpose: "buy", ManagerID: 7, AdvanceReason: "site cash",
				RecoverableCategory: "employee_advance", RepaymentNotes: "monthly"}
		}), false},
		{"employee_advance recoverable ok", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "employee_advance", ShortTitle: "Site cash",
				Amount: 1000, Purpose: "buy", ManagerID: 7, AdvanceReason: "site cash",
				RecoverableCategory: "employee_advance", ExpectedReturnDate: "2026-12-01", RepaymentNotes: "monthly"}
		}), true},
		{"recoverable EMD ok with a project", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "Ridge Metro EMD",
				Amount: 1000, Purpose: "tender", ManagerID: 7, RecoverableCategory: "emd", ProjectID: 4,
				ExpectedReturnDate: "2026-12-01", RepaymentNotes: "on tender close"}
		}), true},
		{"recoverable EMD without a project is rejected", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "Ridge Metro EMD",
				Amount: 1000, Purpose: "tender", ManagerID: 7, RecoverableCategory: "emd",
				ExpectedReturnDate: "2026-12-01", RepaymentNotes: "on tender close"}
		}), false},
		{"recoverable ICD without a counterparty is rejected", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "ICD to Meridian",
				Amount: 1000, Purpose: "deposit", ManagerID: 7, RecoverableCategory: "icd",
				ExpectedReturnDate: "2026-12-01", RepaymentNotes: "on maturity"}
		}), false},
		{"recoverable ICD with a counterparty ok", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "ICD to Meridian",
				Amount: 1000, Purpose: "deposit", ManagerID: 7, RecoverableCategory: "icd",
				Counterparty: "Meridian Holdings Pvt Ltd", ExpectedReturnDate: "2026-12-01", RepaymentNotes: "on maturity"}
		}), true},
		{"unknown recoverable category", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "Mystery",
				Amount: 1000, Purpose: "x", ManagerID: 7, RecoverableCategory: "mystery",
				ExpectedReturnDate: "2026-12-01", RepaymentNotes: "n"}
		}), false},
		{"unknown type", base(func(i *RequestInput) { i.Type = "mystery" }), false},
		{"no manager", base(func(i *RequestInput) { i.ManagerID = 0 }), false},
		{"no amount", base(func(i *RequestInput) { i.Amount = 0 }), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRequestInput(c.in)
			if c.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("want validation error, got nil")
			}
		})
	}
}

// A5/D1: 'draft' is gone; cancellation_requested and cancelled are in.
func TestCanTransition(t *testing.T) {
	legal := map[[2]string]bool{
		{"returned", "pending"}: true,
		{"pending", "approved"}: true, {"pending", "returned"}: true,
		{"pending", "rejected"}: true, {"pending", "withdrawn"}: true,
		{"approved", "cancellation_requested"}: true, {"approved", "cancelled"}: true,
		{"cancellation_requested", "cancelled"}: true, {"cancellation_requested", "approved"}: true,
	}
	all := []string{"draft", "pending", "returned", "rejected", "withdrawn", "approved",
		"cancellation_requested", "cancelled"}
	for _, from := range all {
		for _, to := range all {
			want := legal[[2]string{from, to}]
			if got := canTransition(from, to); got != want {
				t.Fatalf("canTransition(%q,%q)=%v want %v", from, to, got, want)
			}
		}
	}
	// L1 (inverted): 'draft' is not a status this system knows.
	if requestStatuses["draft"] {
		t.Fatal("draft is present in the status enum; D1 removed it")
	}
	for _, s := range []string{"pending", "returned", "rejected", "withdrawn", "approved",
		"cancellation_requested", "cancelled"} {
		if !requestStatuses[s] {
			t.Fatalf("status %q missing from the enum", s)
		}
	}
}
```

Add the struct types to `internal/store/models.go`:

```go
type Request struct {
	ID                        int64
	Number                    string
	Status                    string
	Treatment                 string
	Type                      string
	RecoverableCategory       string // code: emd|pbg|icd|employee_advance|security_deposit|other
	RecoverableCategoryID     *int64 // linked by Phase 4
	ProjectID                 *int64
	Project                   string // joined name; retained even when project inactive
	HeadID                    *int64
	Head                      string // joined name; retained even when head inactive
	VendorID                  *int64
	Vendor                    string // COALESCE(vendors.name, vendor_payee) — the display payee
	VendorGSTIN               string
	VendorPayee               string // snapshot; the only payee for reimbursement / employee advance
	ShortTitle                string
	Amount                    int64
	Purpose                   string
	NeededBy                  string // YYYY-MM-DD, "" if unset
	InvoiceNo                 string
	InvoiceDate               string // YYYY-MM-DD, "" if unset
	ExpenseDate               string // YYYY-MM-DD, "" if unset
	AdvanceReason             string
	Counterparty              string
	ExpectedReturnDate        string // YYYY-MM-DD, "" if unset
	RepaymentNotes            string
	Urgent                    bool
	UrgencyReason             string
	AttachmentExceptionReason string
	RequesterID               int64
	RequesterName             string
	ManagerID                 int64
	ManagerName               string
	ApprovedAmount            *int64
	ApprovedBy                *int64
	ApprovedByName            string
	ApprovedAt                *time.Time
	DecisionReason            string
	CancelReason              string
	OnHold                    bool
	HoldReason                string
	ProcessingBy              *int64
	ProcessingAt              *time.Time
	ReminderLastSent          *time.Time
	SubmittedAt               *time.Time
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
}

type RequestInput struct {
	Treatment                 string
	Type                      string
	RecoverableCategory       string
	ProjectID                 int64
	HeadID                    int64
	VendorID                  int64
	VendorPayee               string
	ShortTitle                string
	Amount                    int64
	Purpose                   string
	NeededBy                  string
	InvoiceNo                 string
	InvoiceDate               string
	ExpenseDate               string
	AdvanceReason             string
	Counterparty              string
	ExpectedReturnDate        string
	RepaymentNotes            string
	Urgent                    bool
	UrgencyReason             string
	AttachmentExceptionReason string
	ManagerID                 int64
	// RequesterID is filled by the store from the actor, never from the form.
	// It exists so validateRequestInput can reject self-approval (G8).
	RequesterID int64
	// Attachments are staged by the handler and written in the same transaction
	// as the request itself. D1 removed drafts, so there is no earlier moment at
	// which a file could be attached.
	Attachments []AttachmentInput
}

type RequestListOptions struct {
	Scope     string // "own" | "assigned" | "all"
	ViewerID  int64
	Status    string   // "" or "all" = any status; otherwise exact status
	Statuses  []string // optional explicit set; wins over Status when non-empty
	Bucket    string   // "" | "open" | "closed" | "needs-me" | "all"
	Type      string
	Treatment string
	ProjectID int64
	Query     string
	Limit     int
}

type RequestComment struct {
	ID         int64
	RequestID  int64
	AuthorID   int64
	AuthorName string
	Body       string
	CreatedAt  time.Time
}

type RequestAttachment struct {
	ID           int64
	RequestID    int64
	OriginalName string
	StoredPath   string
	MimeType     string
	SizeBytes    int64
	UploadedBy   int64
	CreatedAt    time.Time
}

// ThreadEntry is one line of the merged history-and-conversation stream that
// `.thread` renders. Events, comments and attachments are one chronological
// list, not three (UI/UX §9; request-detail-employee.html).
type ThreadEntry struct {
	Kind      string // "event" | "comment" | "attachment"
	Action    string // audit action for events; "" for comments
	ActorID   int64
	ActorName string
	Initials  string
	Title     string
	Body      string
	FileName  string
	FileSize  int64
	Changes   []ThreadChange
	CreatedAt time.Time
}

type ThreadChange struct {
	Field string
	Was   string
	Now   string
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestValidateRequestInputPerType|TestCanTransition' -v`
Expected: FAIL — `undefined: validateRequestInput` / `undefined: canTransition`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/requests.go`:

```go
var requestTypes = map[string]bool{
	"vendor_invoice": true, "vendor_advance": true, "reimbursement": true,
	"employee_advance": true, "recoverable": true,
}

// requestStatuses is the Phase-2 status enum. 'draft' is deliberately absent —
// D1: a request exists only once submitted. Phase 3 appends on_hold,
// processing, completed and completed_partial.
var requestStatuses = map[string]bool{
	"pending": true, "returned": true, "approved": true, "rejected": true,
	"withdrawn": true, "cancellation_requested": true, "cancelled": true,
}

var legalTransitions = map[string]map[string]bool{
	"returned": {"pending": true},
	"pending":  {"approved": true, "returned": true, "rejected": true, "withdrawn": true},
	// G1/G2: post-approval cancellation. An approved request may be frozen by
	// the requester (cancellation_requested) or cancelled outright by a manager.
	"approved":               {"cancellation_requested": true, "cancelled": true},
	"cancellation_requested": {"cancelled": true, "approved": true},
}

func canTransition(from, to string) bool {
	return legalTransitions[from][to]
}

// recoverableCategoryRules is the Phase-2 view of the recoverable categories.
// Phase 4 creates `recoverable_categories` and replaces this map with rows from
// it (requires_project / requires_counterparty) without changing call sites.
var recoverableCategoryRules = map[string]struct{ RequiresProject, RequiresCounterparty bool }{
	"emd":              {RequiresProject: true},
	"pbg":              {RequiresProject: true},
	"icd":              {RequiresCounterparty: true},
	"employee_advance": {},
	"security_deposit": {RequiresCounterparty: true},
	"other":            {},
}

func validateRequestInput(in RequestInput) error {
	if in.Amount <= 0 {
		return fmt.Errorf("%w: a positive amount is required", ErrValidation)
	}
	if strings.TrimSpace(in.ShortTitle) == "" {
		return fmt.Errorf("%w: a short title is required — it is what your approver sees in their list", ErrValidation)
	}
	if strings.TrimSpace(in.Purpose) == "" {
		return fmt.Errorf("%w: purpose is required", ErrValidation)
	}
	if in.ManagerID <= 0 {
		return fmt.Errorf("%w: choose an approver", ErrValidation)
	}
	if in.Treatment != "budget" && in.Treatment != "recoverable" {
		return fmt.Errorf("%w: treatment must be budget or recoverable", ErrValidation)
	}
	if !requestTypes[in.Type] {
		return fmt.Errorf("%w: unknown request type", ErrValidation)
	}
	for _, d := range []struct{ label, value string }{
		{"required-by date", in.NeededBy},
		{"expected return date", in.ExpectedReturnDate},
		{"invoice date", in.InvoiceDate},
		{"expense date", in.ExpenseDate},
	} {
		if d.value != "" && !validDate(d.value) {
			return fmt.Errorf("%w: %s is invalid", ErrValidation, d.label)
		}
	}
	needsProjectHead := func() error {
		if in.ProjectID <= 0 || in.HeadID <= 0 {
			return fmt.Errorf("%w: project and head are required for this type", ErrValidation)
		}
		return nil
	}
	needsVendor := func() error {
		if in.VendorID <= 0 {
			return fmt.Errorf("%w: choose a vendor from the vendor master", ErrValidation)
		}
		return nil
	}
	needsRecoverable := func() error {
		rule, ok := recoverableCategoryRules[in.RecoverableCategory]
		if !ok {
			return fmt.Errorf("%w: choose a recoverable category", ErrValidation)
		}
		if !validDate(in.ExpectedReturnDate) {
			return fmt.Errorf("%w: expected return date is required for recoverables", ErrValidation)
		}
		if strings.TrimSpace(in.RepaymentNotes) == "" {
			return fmt.Errorf("%w: repayment or refund terms are required for recoverables", ErrValidation)
		}
		if rule.RequiresProject && in.ProjectID <= 0 {
			return fmt.Errorf("%w: this recoverable category always belongs to a project", ErrValidation)
		}
		if rule.RequiresCounterparty && strings.TrimSpace(in.Counterparty) == "" {
			return fmt.Errorf("%w: this recoverable category needs a counterparty company", ErrValidation)
		}
		return nil
	}
	switch in.Type {
	case "vendor_invoice":
		if in.Treatment != "budget" {
			return fmt.Errorf("%w: a vendor invoice is a budget expense", ErrValidation)
		}
		if err := needsProjectHead(); err != nil {
			return err
		}
		if err := needsVendor(); err != nil {
			return err
		}
		if strings.TrimSpace(in.InvoiceNo) == "" {
			return fmt.Errorf("%w: the invoice number is required", ErrValidation)
		}
		if !validDate(in.InvoiceDate) {
			return fmt.Errorf("%w: the invoice date is required", ErrValidation)
		}
	case "vendor_advance":
		if in.Treatment != "budget" {
			return fmt.Errorf("%w: a vendor advance is a budget expense", ErrValidation)
		}
		if err := needsProjectHead(); err != nil {
			return err
		}
		if err := needsVendor(); err != nil {
			return err
		}
		if strings.TrimSpace(in.AdvanceReason) == "" {
			return fmt.Errorf("%w: say what the advance is for", ErrValidation)
		}
	case "reimbursement":
		if in.Treatment != "budget" {
			return fmt.Errorf("%w: reimbursement is a budget expense", ErrValidation)
		}
		if err := needsProjectHead(); err != nil {
			return err
		}
		if !validDate(in.ExpenseDate) {
			return fmt.Errorf("%w: the expense date is required", ErrValidation)
		}
	case "employee_advance":
		if strings.TrimSpace(in.AdvanceReason) == "" {
			return fmt.Errorf("%w: say what the money is for", ErrValidation)
		}
		if in.Treatment == "budget" {
			return needsProjectHead()
		}
		return needsRecoverable()
	case "recoverable":
		if in.Treatment != "recoverable" {
			return fmt.Errorf("%w: recoverable type requires recoverable treatment", ErrValidation)
		}
		return needsRecoverable()
	}
	return nil
}

// forcesRequesterPayee reports whether the payee must equal the requester.
// These types never carry a vendor row — the payee snapshot is the only payee.
func forcesRequesterPayee(t string) bool {
	return t == "reimbursement" || t == "employee_advance"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestValidateRequestInputPerType|TestCanTransition' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/models.go internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): request types, per-type validation, draft-free status enum"
```

---

### Task 4: `app_settings` accessors (`AppSetting` / `SetAppSetting` / `AppSettings`)

**Files:**
- Create: `internal/store/settings.go` (`AppSetting`, `AppSettings`, `SetAppSetting`, `SetAppSettings`)
- Test: `internal/store/settings_test.go`

**Interfaces:**
- Consumes: the `app_settings` table + the Phase-2 defaults seeded by Task 1, `recordAuditTx`, `classify`.
- Produces: `func (s *Store) AppSetting(ctx context.Context, key string) (string, error)` (returns `""` when the key is absent); `func (s *Store) AppSettings(ctx context.Context) (map[string]string, error)`; `func (s *Store) SetAppSetting(ctx context.Context, actor User, key, value string) error`; `func (s *Store) SetAppSettings(ctx context.Context, actor User, values map[string]string) error` (one transaction, one audit row — the Configuration screen saves a whole form at once, D6).

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"context"
	"testing"
)

func TestAppSettingRoundTripAndDefault(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _, _ := seedRequestActors(t, s, ctx)

	// Migration v3 seeds require_attachments='0'.
	v, err := s.AppSetting(ctx, "require_attachments")
	if err != nil {
		t.Fatalf("AppSetting: %v", err)
	}
	if v != "0" {
		t.Fatalf("default require_attachments = %q, want 0", v)
	}
	// An absent key returns "" (no error).
	if v, err := s.AppSetting(ctx, "does_not_exist"); err != nil || v != "" {
		t.Fatalf("absent key = %q, %v; want \"\", nil", v, err)
	}
	// SetAppSetting upserts the value.
	if err := s.SetAppSetting(ctx, actor, "require_attachments", "1"); err != nil {
		t.Fatalf("SetAppSetting: %v", err)
	}
	if v, _ := s.AppSetting(ctx, "require_attachments"); v != "1" {
		t.Fatalf("after set = %q, want 1", v)
	}
}

// D6: the Configuration screen saves a whole form atomically.
func TestSetAppSettingsIsAtomicAndAudited(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _, _ := seedRequestActors(t, s, ctx)

	if err := s.SetAppSettings(ctx, actor, map[string]string{
		"number_prefix": "REQ", "urgency_mode": "free", "require_attachments": "1",
	}); err != nil {
		t.Fatalf("SetAppSettings: %v", err)
	}
	all, err := s.AppSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"number_prefix": "REQ", "urgency_mode": "free", "require_attachments": "1"} {
		if all[k] != want {
			t.Fatalf("AppSettings[%q] = %q, want %q", k, all[k], want)
		}
	}
	audit, err := s.Audit(ctx, "app_setting", 0, 5)
	if err != nil || len(audit) == 0 {
		t.Fatalf("audit = %#v, %v; want a configuration entry", audit, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestAppSetting|TestSetAppSettings' -v`
Expected: FAIL — `s.AppSetting undefined` / `s.SetAppSettings undefined`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/store/settings.go`:

```go
package store

import (
	"context"
	"database/sql"
	"sort"
	"strings"
)

// AppSetting returns the value of a runtime setting, or "" if it is unset.
func (s *Store) AppSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// AppSettings returns every runtime setting. The Configuration screen renders
// from this one map (D6).
func (s *Store) AppSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetAppSetting upserts a single runtime setting and records an audit row.
func (s *Store) SetAppSetting(ctx context.Context, actor User, key, value string) error {
	return s.SetAppSettings(ctx, actor, map[string]string{key: value})
}

// SetAppSettings upserts a batch of settings in one transaction with one audit
// row, so a Configuration save is all-or-nothing (house pattern).
func (s *Store) SetAppSettings(ctx context.Context, actor User, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	before := map[string]any{}
	after := map[string]any{}
	for _, k := range keys {
		var prev string
		switch err := tx.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key=?`, k).Scan(&prev); err {
		case nil, sql.ErrNoRows:
		default:
			return err
		}
		before[k] = prev
		after[k] = values[k]
		if _, err := tx.ExecContext(ctx, `INSERT INTO app_settings(key,value) VALUES(?,?)
 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, values[k]); err != nil {
			return classify(err)
		}
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "settings",
		EntityType: "app_setting", Summary: "Updated configuration: " + strings.Join(keys, ", "),
		Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestAppSetting|TestSetAppSettings' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/settings.go internal/store/settings_test.go
git commit -m "feat(store): app_settings accessors with atomic batch save"
```

---

### Task 5: `CreateRequest` — one atomic create-and-submit (D1), plus `Request` and `SubmitRequest`

**Files:**
- Modify: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `NextRequestNumber`/`requestNumberYear` (Task 2), `validateRequestInput`/`forcesRequesterPayee` (Task 3), `recordAuditTx`, `classify`, `boolInt`, `validateAttachment`.
- Produces: `func (s *Store) CreateRequest(ctx, actor User, in RequestInput) (int64, error)` — creates the request **already `pending`**, reserves the number in the **same transaction**, stamps `submitted_at`, writes the staged attachments and one `submit` audit row; `func (s *Store) Request(ctx, id int64) (Request, error)`; `func (s *Store) SubmitRequest(ctx, actor User, id int64) error` — **only** `returned → pending`, for the correct-and-resubmit flow; package helpers `scanRequest`, `requestInTx`, `nullableID`, `nullableText`.

**This task is the merge of the old Tasks 4 and 5 (amendment A6, decision D1).** There is no `draft` state, no separate "create then submit" pair, and no window in which an unnumbered request exists. `SubmitRequest` survives with one job: resubmitting a request the manager returned for correction (`request-returned.html`).

- [ ] **Step 1: Write the failing test**

```go
func TestCreateRequestIsAtomicCreateAndSubmit(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)

	id, err := s.CreateRequest(ctx, req, RequestInput{
		Treatment: "budget", Type: "reimbursement", ShortTitle: "Team lunch",
		ProjectID: 1, HeadID: headID, Amount: 50000, Purpose: "team lunch",
		ExpenseDate: "2026-07-21", ManagerID: mgr.ID, VendorPayee: "ignored",
		Urgent: true, UrgencyReason: "card bill due Monday",
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	got, err := s.Request(ctx, id)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	// D1: created already pending, numbered, and stamped in one operation.
	if got.Status != "pending" {
		t.Fatalf("status = %q, want pending (D1: there are no drafts)", got.Status)
	}
	if got.SubmittedAt == nil {
		t.Fatal("submitted_at not stamped; create and submit are one operation")
	}
	wantPrefix := "PR-" + time.Now().UTC().Format("2006") + "-"
	if !strings.HasPrefix(got.Number, wantPrefix) {
		t.Fatalf("number = %q, want prefix %q", got.Number, wantPrefix)
	}
	if got.VendorPayee != req.Name || got.Vendor != req.Name {
		t.Fatalf("payee = %q/%q, want requester %q (forced)", got.VendorPayee, got.Vendor, req.Name)
	}
	if got.RequesterID != req.ID || got.ManagerID != mgr.ID {
		t.Fatalf("requester/manager = %d/%d, want %d/%d", got.RequesterID, got.ManagerID, req.ID, mgr.ID)
	}
	// T5: the urgent flag and its reason round-trip.
	if !got.Urgent || got.UrgencyReason == "" {
		t.Fatalf("urgent=%v reason=%q; both must round-trip", got.Urgent, got.UrgencyReason)
	}
	// C3: amount is stored/round-tripped as int64 paise and formats via money.FormatPaise.
	if got.Amount != 50000 || money.FormatPaise(got.Amount) != "₹500.00" {
		t.Fatalf("amount = %d / %q, want 50000 / ₹500.00", got.Amount, money.FormatPaise(got.Amount))
	}
	audit, err := s.Audit(ctx, "payment_request", id, 5)
	if err != nil || len(audit) == 0 || audit[0].Action != "submit" {
		t.Fatalf("audit = %#v, %v; want a submit entry on payment_request", audit, err)
	}
}

// D1 proof of absence: coverage row L1 inverted. No draft state exists anywhere.
func TestNoDraftStateExists(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)

	if requestStatuses["draft"] {
		t.Fatal("draft is in the status enum")
	}
	for _, to := range []string{"pending", "approved", "returned", "rejected", "withdrawn"} {
		if canTransition("draft", to) || canTransition(to, "draft") {
			t.Fatalf("a transition to or from draft exists (%q)", to)
		}
	}
	if _, err := s.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,amount,purpose,requester_id,manager_id) VALUES('PR-X','draft','budget','vendor_invoice',1,'p',1,1)`); err == nil {
		t.Fatal("the schema accepted status='draft'")
	}
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "t", ProjectID: 1, HeadID: headID, Amount: 100, Purpose: "p",
		ExpenseDate: "2026-07-21", ManagerID: mgr.ID})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Request(ctx, id)
	if got.Status == "draft" || got.SubmittedAt == nil || got.Number == "" {
		t.Fatalf("a newly created request must be numbered and pending: %+v", got)
	}
}

func TestCreateRequestRejectsInvalidType(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	_, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "t", HeadID: headID, Amount: 1, Purpose: "x", ManagerID: mgr.ID,
		VendorID: 1, InvoiceNo: "A/1", InvoiceDate: "2026-07-01"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("missing project err = %v, want ErrValidation", err)
	}
	// Nothing partial is left behind when validation fails.
	list, _ := s.ListRequests(ctx, RequestListOptions{Scope: "all"})
	if len(list) != 0 {
		t.Fatalf("a failed create left %d rows behind", len(list))
	}
}

// A staged attachment is written in the same transaction as the request; D1
// left no earlier moment at which a file could be attached.
func TestCreateRequestWritesStagedAttachments(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Sundaram Electricals Pvt Ltd")
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "July switchgear", ProjectID: 1, HeadID: headID, Amount: 100000, Purpose: "panels",
		ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: "SE/26-27/1184", InvoiceDate: "2026-07-18",
		Attachments: []AttachmentInput{{OriginalName: "inv.pdf", StoredPath: "/tmp/inv.pdf", MimeType: "application/pdf", SizeBytes: 12}},
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	atts, err := s.RequestAttachments(ctx, id)
	if err != nil || len(atts) != 1 || atts[0].OriginalName != "inv.pdf" {
		t.Fatalf("attachments = %#v, %v", atts, err)
	}
	got, _ := s.Request(ctx, id)
	if got.Vendor != "Sundaram Electricals Pvt Ltd" {
		t.Fatalf("vendor name not joined: %q", got.Vendor)
	}
}

// The only surviving use of SubmitRequest: correct and resubmit (returned -> pending).
func TestSubmitRequestResubmitsAReturnedRequest(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "t", ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "p",
		ExpenseDate: "2026-07-21", ManagerID: mgr.ID})
	if err != nil {
		t.Fatal(err)
	}
	// A pending request cannot be "submitted" again — it already is.
	if err := s.SubmitRequest(ctx, req, id); !errors.Is(err, ErrValidation) {
		t.Fatalf("double submit = %v, want ErrValidation", err)
	}
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='returned' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitRequest(ctx, req, id); err != nil {
		t.Fatalf("resubmit returned: %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.Status != "pending" || got.SubmittedAt == nil || got.ReminderLastSent != nil {
		t.Fatalf("after resubmit = %+v", got)
	}
}

// T12: retired project/head disappears from new selection but the historical
// request keeps showing its name.
func TestRequestRetainsHistoricalProjectHead(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "Inv", ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "inv",
		ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: "A/1", InvoiceDate: "2026-07-01"})
	if err != nil {
		t.Fatal(err)
	}
	// Retire the head.
	if _, err := s.UpsertHead(ctx, headID, 1, "Rent", "5", false, 1); err != nil {
		t.Fatal(err)
	}
	// New selection lists no active heads now...
	active, err := s.ListHeads(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range active {
		if h.ID == headID {
			t.Fatal("retired head still offered for new requests")
		}
	}
	// ...but the historical request still shows the head + project name.
	got, err := s.Request(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Head != "Rent" || got.Project != "Operations" {
		t.Fatalf("historical names lost: project=%q head=%q", got.Project, got.Head)
	}
}

func seedRequestActors(t *testing.T, s *Store, ctx context.Context) (requester User, manager User, headID int64) {
	t.Helper()
	rid, err := s.CreateUser(ctx, "req@example.com", "Rhea Requester", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	mid, err := s.CreateUser(ctx, "mgr@example.com", "Manav Manager", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	requester, _ = s.UserByID(ctx, rid)
	manager, _ = s.UserByID(ctx, mid)
	projectID, err := s.UpsertProject(ctx, 0, "Operations", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	headID, err = s.UpsertHead(ctx, 0, projectID, "Rent", "5", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	return requester, manager, headID
}

// seedTestVendor inserts a Phase-1V vendor row directly; Phase 2 only needs its id.
func seedTestVendor(t *testing.T, s *Store, ctx context.Context, name string) int64 {
	t.Helper()
	res, err := s.DB().ExecContext(ctx, `INSERT INTO vendors(name,vendor_type,status,gstin) VALUES(?,'company','active','29AABCS1429B1ZQ')`, name)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
```

> The C3 assertion calls `money.FormatPaise`, so add `"fervidbudget/internal/money"` to the imports of `internal/store/requests_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestCreateRequest|TestNoDraftStateExists|TestSubmitRequestResubmits' -v`
Expected: FAIL — `s.CreateRequest undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/requests.go`:

```go
const requestSelect = `SELECT r.id,r.number,r.status,r.treatment,r.type,r.recoverable_category,r.recoverable_category_id,
 r.project_id,COALESCE(p.name,''),r.head_id,COALESCE(h.name,''),
 r.vendor_id,COALESCE(NULLIF(v.name,''),r.vendor_payee),COALESCE(v.gstin,''),r.vendor_payee,r.short_title,
 r.amount,r.purpose,COALESCE(r.needed_by,''),
 r.invoice_no,COALESCE(r.invoice_date,''),COALESCE(r.expense_date,''),r.advance_reason,
 r.counterparty,COALESCE(r.expected_return_date,''),r.repayment_notes,
 r.urgent,r.urgency_reason,r.attachment_exception_reason,
 r.requester_id,COALESCE(ru.name,''),r.manager_id,COALESCE(mu.name,''),
 r.approved_amount,r.approved_by,COALESCE(au.name,''),r.approved_at,
 r.decision_reason,r.cancel_reason,r.on_hold,r.hold_reason,r.processing_by,r.processing_at,
 r.reminder_last_sent,r.submitted_at,r.created_at,r.updated_at
FROM payment_requests r
LEFT JOIN projects p ON p.id=r.project_id
LEFT JOIN heads h ON h.id=r.head_id
LEFT JOIN vendors v ON v.id=r.vendor_id
JOIN users ru ON ru.id=r.requester_id
JOIN users mu ON mu.id=r.manager_id
LEFT JOIN users au ON au.id=r.approved_by`

func scanRequest(sc interface{ Scan(...any) error }) (Request, error) {
	var r Request
	var catID, projID, headID, vendorID, approvedAmt, approvedBy, processingBy sql.NullInt64
	var approvedAt, reminder, submitted, processingAt sql.NullTime
	var urgent, onHold int
	err := sc.Scan(&r.ID, &r.Number, &r.Status, &r.Treatment, &r.Type, &r.RecoverableCategory, &catID,
		&projID, &r.Project, &headID, &r.Head,
		&vendorID, &r.Vendor, &r.VendorGSTIN, &r.VendorPayee, &r.ShortTitle,
		&r.Amount, &r.Purpose, &r.NeededBy,
		&r.InvoiceNo, &r.InvoiceDate, &r.ExpenseDate, &r.AdvanceReason,
		&r.Counterparty, &r.ExpectedReturnDate, &r.RepaymentNotes,
		&urgent, &r.UrgencyReason, &r.AttachmentExceptionReason,
		&r.RequesterID, &r.RequesterName, &r.ManagerID, &r.ManagerName,
		&approvedAmt, &approvedBy, &r.ApprovedByName, &approvedAt,
		&r.DecisionReason, &r.CancelReason, &onHold, &r.HoldReason, &processingBy, &processingAt,
		&reminder, &submitted, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.Urgent = urgent == 1
	r.OnHold = onHold == 1
	for _, p := range []struct {
		src sql.NullInt64
		dst **int64
	}{{catID, &r.RecoverableCategoryID}, {projID, &r.ProjectID}, {headID, &r.HeadID},
		{vendorID, &r.VendorID}, {approvedAmt, &r.ApprovedAmount}, {approvedBy, &r.ApprovedBy},
		{processingBy, &r.ProcessingBy}} {
		if p.src.Valid {
			v := p.src.Int64
			*p.dst = &v
		}
	}
	for _, p := range []struct {
		src sql.NullTime
		dst **time.Time
	}{{approvedAt, &r.ApprovedAt}, {processingAt, &r.ProcessingAt},
		{reminder, &r.ReminderLastSent}, {submitted, &r.SubmittedAt}} {
		if p.src.Valid {
			v := p.src.Time
			*p.dst = &v
		}
	}
	return r, nil
}

func (s *Store) Request(ctx context.Context, id int64) (Request, error) {
	return scanRequest(s.db.QueryRowContext(ctx, requestSelect+` WHERE r.id=?`, id))
}

func requestInTx(ctx context.Context, tx *sql.Tx, id int64) (Request, error) {
	return scanRequest(tx.QueryRowContext(ctx, requestSelect+` WHERE r.id=?`, id))
}

func nullableID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

func nullableText(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// CreateRequest creates a request that is already submitted. D1: there are no
// drafts — the row, its number and its submission timestamp are written in one
// transaction, together with any file the requester staged on the form.
func (s *Store) CreateRequest(ctx context.Context, actor User, in RequestInput) (int64, error) {
	in.RequesterID = actor.ID
	if forcesRequesterPayee(in.Type) {
		in.VendorID = 0
		in.VendorPayee = actor.Name
	}
	if err := validateRequestInput(in); err != nil {
		return 0, err
	}
	for _, att := range in.Attachments {
		if err := validateAttachment(att); err != nil {
			return 0, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	year, err := requestNumberYear(tx, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	number, err := NextRequestNumber(tx, year)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO payment_requests
 (number,status,treatment,type,recoverable_category,recoverable_category_id,project_id,head_id,
  vendor_id,vendor_payee,short_title,amount,purpose,needed_by,invoice_no,invoice_date,expense_date,
  advance_reason,counterparty,expected_return_date,repayment_notes,urgent,urgency_reason,
  attachment_exception_reason,requester_id,manager_id,submitted_at)
 VALUES(?,'pending',?,?,?,NULL,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		number, in.Treatment, in.Type, in.RecoverableCategory,
		nullableID(in.ProjectID), nullableID(in.HeadID),
		nullableID(in.VendorID), in.VendorPayee, strings.TrimSpace(in.ShortTitle),
		in.Amount, in.Purpose, nullableText(in.NeededBy), in.InvoiceNo,
		nullableText(in.InvoiceDate), nullableText(in.ExpenseDate), in.AdvanceReason,
		in.Counterparty, nullableText(in.ExpectedReturnDate), in.RepaymentNotes,
		boolInt(in.Urgent), in.UrgencyReason, in.AttachmentExceptionReason,
		actor.ID, in.ManagerID)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, att := range in.Attachments {
		if _, err := tx.ExecContext(ctx, `INSERT INTO request_attachments(request_id,original_name,stored_path,mime_type,size_bytes,uploaded_by) VALUES(?,?,?,?,?,?)`,
			id, att.OriginalName, att.StoredPath, att.MimeType, att.SizeBytes, actor.ID); err != nil {
			return 0, classify(err)
		}
	}
	// P5 hook: notify in.ManagerID here.
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "submit", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " submitted request " + number + " for " + money.FormatPaise(in.Amount),
		After:   map[string]any{"number": number, "type": in.Type, "amount": in.Amount, "manager_id": in.ManagerID}}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// SubmitRequest resubmits a request the approver returned for correction. It is
// the only remaining transition into `pending` after creation (D1).
func (s *Store) SubmitRequest(ctx context.Context, actor User, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.RequesterID != actor.ID {
		return ErrForbidden
	}
	if !canTransition(before.Status, "pending") {
		return fmt.Errorf("%w: a %s request cannot be submitted", ErrValidation, before.Status)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='pending', submitted_at=CURRENT_TIMESTAMP, reminder_last_sent=NULL, updated_at=CURRENT_TIMESTAMP WHERE id=?`, id); err != nil {
		return err
	}
	after := before
	after.Status = "pending"
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "submit", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " resubmitted request " + before.Number, Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestCreateRequest|TestNoDraftStateExists|TestSubmitRequestResubmits|TestRequestRetainsHistorical' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): atomic create-and-submit with no draft state (D1)"
```

---
### Task 6: Urgency reason required when `urgent` is set (G7)

**Files:**
- Modify: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `settingInTx` (Task 2), `AppSetting` (Task 4), `CreateRequest` (Task 5).
- Produces: `func validateUrgency(in RequestInput, mode string) error`; `func (s *Store) urgencyMode(ctx context.Context) (string, error)`; both wired into `CreateRequest` (and, in Task 10, `UpdateRequest`).

**Why a setting and not a constant:** `admin-configuration.html` offers Urgency = *Requires a reason* (the seeded default) · *Free to mark, no reason* · *Disabled*. Hard-coding the rule would make the Configuration control a lie (D6).

- [ ] **Step 1: Write the failing test**

```go
func TestUrgencyReasonIsRequiredWhenUrgent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	mk := func(urgent bool, reason string) error {
		_, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "reimbursement",
			ShortTitle: "Travel", ProjectID: 1, HeadID: headID, Amount: 18400, Purpose: "site visit",
			ExpenseDate: "2026-07-17", ManagerID: mgr.ID, Urgent: urgent, UrgencyReason: reason})
		return err
	}
	// Default mode is "reason" (seeded by migration v3).
	if err := mk(true, "   "); !errors.Is(err, ErrValidation) {
		t.Fatalf("urgent without a reason = %v, want ErrValidation", err)
	}
	if err := mk(true, "Personal card bill is due on 29 July"); err != nil {
		t.Fatalf("urgent with a reason: %v", err)
	}
	// Not urgent needs no reason.
	if err := mk(false, ""); err != nil {
		t.Fatalf("non-urgent: %v", err)
	}

	// "free": urgent is allowed with no reason.
	if err := s.SetAppSetting(ctx, mgr, "urgency_mode", "free"); err != nil {
		t.Fatal(err)
	}
	if err := mk(true, ""); err != nil {
		t.Fatalf("urgency_mode=free rejected a reasonless urgent request: %v", err)
	}

	// "disabled": urgent may not be set at all.
	if err := s.SetAppSetting(ctx, mgr, "urgency_mode", "disabled"); err != nil {
		t.Fatal(err)
	}
	if err := mk(true, "still urgent"); !errors.Is(err, ErrValidation) {
		t.Fatalf("urgency_mode=disabled accepted an urgent request = %v, want ErrValidation", err)
	}
	if err := mk(false, ""); err != nil {
		t.Fatalf("urgency_mode=disabled rejected a normal request: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestUrgencyReasonIsRequiredWhenUrgent -v`
Expected: FAIL — the reasonless urgent request is accepted (`want ErrValidation`).

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/requests.go`:

```go
// urgencyMode returns the configured urgency policy: "reason" (default),
// "free" or "disabled" (admin-configuration.html · Payments · Urgency).
func (s *Store) urgencyMode(ctx context.Context) (string, error) {
	mode, err := s.AppSetting(ctx, "urgency_mode")
	if err != nil {
		return "", err
	}
	switch mode {
	case "free", "disabled", "reason":
		return mode, nil
	default:
		return "reason", nil
	}
}

// validateUrgency enforces G7. It is separate from validateRequestInput because
// the rule is configuration-driven and validateRequestInput stays pure.
func validateUrgency(in RequestInput, mode string) error {
	if !in.Urgent {
		return nil
	}
	switch mode {
	case "disabled":
		return fmt.Errorf("%w: urgent requests are switched off", ErrValidation)
	case "free":
		return nil
	default:
		if strings.TrimSpace(in.UrgencyReason) == "" {
			return fmt.Errorf("%w: say why this is urgent", ErrValidation)
		}
	}
	return nil
}
```

In `CreateRequest`, insert the check immediately after `validateRequestInput`:

```go
	if err := validateRequestInput(in); err != nil {
		return 0, err
	}
	mode, err := s.urgencyMode(ctx)
	if err != nil {
		return 0, err
	}
	if err := validateUrgency(in, mode); err != nil {
		return 0, err
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestUrgencyReasonIsRequiredWhenUrgent -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): require a reason when a request is marked urgent"
```

---

### Task 7: Attachment exception reason instead of a hard block (G10)

**Files:**
- Modify: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `AppSetting` (Task 4), `CreateRequest`/`SubmitRequest` (Task 5).
- Produces: `func validateAttachmentPolicy(required bool, attachmentCount int, exceptionReason string) error`, wired into `CreateRequest` (counts `in.Attachments`) and `SubmitRequest` (counts stored rows).

**The rule, verbatim from `request-new-form.html`:** "Your administrator can make attachments compulsory. When that is on, submitting without one asks you for a reason instead of blocking you." So `require_attachments=1` + no file + no reason → `ErrValidation`; `require_attachments=1` + no file + a reason → **accepted**, and the reason is stored in `attachment_exception_reason` where the approver can see it.

- [ ] **Step 1: Write the failing test**

```go
func TestAttachmentPolicyAsksForAReasonInsteadOfBlocking(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Kaveri Logistics")
	mk := func(in RequestInput) (int64, error) {
		in.Treatment, in.Type = "budget", "vendor_invoice"
		in.ShortTitle, in.ProjectID, in.HeadID = "Freight", 1, headID
		in.Amount, in.Purpose, in.ManagerID = 64500, "freight", mgr.ID
		in.VendorID, in.InvoiceNo, in.InvoiceDate = vendorID, "KL/2026/0788", "2026-07-21"
		return s.CreateRequest(ctx, req, in)
	}
	// Off by default: no file, no reason, accepted.
	if _, err := mk(RequestInput{}); err != nil {
		t.Fatalf("attachments optional: %v", err)
	}
	if err := s.SetAppSetting(ctx, mgr, "require_attachments", "1"); err != nil {
		t.Fatal(err)
	}
	// On + no file + no reason -> rejected.
	if _, err := mk(RequestInput{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("required attachment missing = %v, want ErrValidation", err)
	}
	// On + no file + a reason -> accepted, and the reason is visible on the request.
	id, err := mk(RequestInput{AttachmentExceptionReason: "Vendor sends the invoice by post; it arrives Monday"})
	if err != nil {
		t.Fatalf("exception reason must unblock the submit, got %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.AttachmentExceptionReason == "" {
		t.Fatal("the exception reason was not stored where the approver can read it")
	}
	// On + a file -> accepted, no reason needed.
	if _, err := mk(RequestInput{Attachments: []AttachmentInput{{OriginalName: "inv.pdf", StoredPath: "/tmp/inv.pdf", MimeType: "application/pdf", SizeBytes: 9}}}); err != nil {
		t.Fatalf("attached file rejected: %v", err)
	}

	// Resubmitting a returned request obeys the same rule against stored files.
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='returned' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitRequest(ctx, req, id); err != nil {
		t.Fatalf("resubmit with a stored exception reason: %v", err)
	}
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='returned', attachment_exception_reason='' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitRequest(ctx, req, id); !errors.Is(err, ErrValidation) {
		t.Fatalf("resubmit without file or reason = %v, want ErrValidation", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestAttachmentPolicy -v`
Expected: FAIL — the fileless, reasonless create is accepted.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/requests.go`:

```go
// validateAttachmentPolicy implements G10. When attachments are compulsory the
// system never hard-blocks: it asks for a written reason, because legitimate
// documents are sometimes genuinely unavailable.
func validateAttachmentPolicy(required bool, attachmentCount int, exceptionReason string) error {
	if !required || attachmentCount > 0 {
		return nil
	}
	if strings.TrimSpace(exceptionReason) == "" {
		return fmt.Errorf("%w: attach a supporting document, or say why you cannot", ErrValidation)
	}
	return nil
}

func (s *Store) attachmentsRequired(ctx context.Context) (bool, error) {
	v, err := s.AppSetting(ctx, "require_attachments")
	return v == "1", err
}
```

In `CreateRequest`, after the urgency check:

```go
	required, err := s.attachmentsRequired(ctx)
	if err != nil {
		return 0, err
	}
	if err := validateAttachmentPolicy(required, len(in.Attachments), in.AttachmentExceptionReason); err != nil {
		return 0, err
	}
```

In `SubmitRequest`, after the transition check:

```go
	required, err := s.attachmentsRequired(ctx)
	if err != nil {
		return err
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_attachments WHERE request_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if err := validateAttachmentPolicy(required, n, before.AttachmentExceptionReason); err != nil {
		return err
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestAttachmentPolicy -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): attachment exception reason instead of a hard submit block"
```

---

### Task 8: Self-approval is impossible (G8)

**Files:**
- Modify: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `validateRequestInput` (Task 3), `user_roles`/`role_permissions` (Phase 1).
- Produces: the `ManagerID == RequesterID` rejection inside `validateRequestInput`; `func (s *Store) ListApprovers(ctx context.Context, excludeUserID int64) ([]User, error)` — active users who hold `approval:approve`, **never** including `excludeUserID`. The request form's approver `<select>` is built only from this list, so the requester's own name is never in it (`request-new-form.html`: "You cannot approve your own request. Your own name is never in this list.").

**Defence in depth:** the list excludes the requester (UI), the store rejects the input (Task 8), and `ApproveRequest` refuses when the actor is the requester even if a row somehow reached that state (Task 12). `admin-configuration.html` renders "Block self-approval" as *checked and disabled* — it is not configurable.

- [ ] **Step 1: Write the failing test**

```go
func TestSelfApprovalIsRejectedAndNeverOffered(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)

	// The store refuses to route a request to its own requester.
	_, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "Lunch", ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "p",
		ExpenseDate: "2026-07-21", ManagerID: req.ID})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("self-approval create = %v, want ErrValidation", err)
	}
	// Pure-input form of the same rule.
	if err := validateRequestInput(RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "t", ProjectID: 1, HeadID: 2, Amount: 1, Purpose: "p",
		ExpenseDate: "2026-07-21", ManagerID: 9, RequesterID: 9}); !errors.Is(err, ErrValidation) {
		t.Fatalf("validateRequestInput self-approval = %v, want ErrValidation", err)
	}

	// The approver list never contains the requester.
	grantApprovalPermission(t, s, ctx, req.ID)
	grantApprovalPermission(t, s, ctx, mgr.ID)
	approvers, err := s.ListApprovers(ctx, req.ID)
	if err != nil {
		t.Fatalf("ListApprovers: %v", err)
	}
	if len(approvers) != 1 || approvers[0].ID != mgr.ID {
		t.Fatalf("approvers = %#v, want only the manager", approvers)
	}
	for _, a := range approvers {
		if a.ID == req.ID {
			t.Fatal("the requester appears in their own approver list")
		}
	}
}

// grantApprovalPermission gives a user a role carrying approval:approve.
func grantApprovalPermission(t *testing.T, s *Store, ctx context.Context, userID int64) {
	t.Helper()
	var roleID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM roles WHERE lower(name)='manager'`).Scan(&roleID); err != nil {
		t.Fatalf("seeded Manager role missing: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES(?,?) ON CONFLICT DO NOTHING`, userID, roleID); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSelfApprovalIsRejected -v`
Expected: FAIL — the self-routed create succeeds and `s.ListApprovers` is undefined.

- [ ] **Step 3: Write minimal implementation**

In `validateRequestInput`, immediately after the `in.ManagerID <= 0` check:

```go
	// G8: self-approval is never acceptable, whatever roles the requester holds.
	// admin-configuration.html renders this as checked-and-disabled: not a setting.
	if in.RequesterID > 0 && in.ManagerID == in.RequesterID {
		return fmt.Errorf("%w: you cannot approve your own request — choose another approver", ErrValidation)
	}
```

Append to `internal/store/requests.go`:

```go
// ListApprovers returns the active users who may approve a request, never
// including excludeUserID. The request form's approver control is built from
// exactly this list, so a requester's own name is never selectable (G8).
func (s *Store) ListApprovers(ctx context.Context, excludeUserID int64) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT u.id,u.email,u.name,u.password_hash,u.role,u.active,u.created_at,u.updated_at,u.default_approver_id
 FROM users u
 JOIN user_roles ur ON ur.user_id=u.id
 JOIN role_permissions rp ON rp.role_id=ur.role_id
 WHERE u.active=1 AND rp.resource='approval' AND rp.action='approve' AND u.id<>?
 ORDER BY u.name`, excludeUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
```

> `scanUser` gains the `default_approver_id` column in Task 9. Land Task 9's `scanUser`/`User` change first if you are implementing these out of order — the two tasks share that one scan list.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestSelfApprovalIsRejected -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): block self-approval and exclude the requester from the approver list"
```

---

### Task 9: Default approver per user (G9)

**Files:**
- Modify: `internal/store/models.go` (`User.DefaultApproverID`)
- Modify: `internal/store/store.go` (`scanUser` + the three user SELECT lists)
- Create: `internal/store/approvers.go` (`SetDefaultApprover`, `DefaultApprover`)
- Test: `internal/store/approvers_test.go`

**Interfaces:**
- Consumes: `users.default_approver_id` (Task 1), `ListApprovers` (Task 8), `recordAuditTx`.
- Produces: `User.DefaultApproverID *int64`; `func (s *Store) SetDefaultApprover(ctx context.Context, actor User, userID, approverID int64) error` (0 clears it); `func (s *Store) DefaultApprover(ctx context.Context, userID int64) (int64, error)`. The request form pre-selects this approver and lets the requester override it (`request-new-form.html`: "Kavita Rao — Manager, Operations (your default)"). `admin-configuration.html` · Approvals · "Let the employee choose a different approver" (`allow_approver_choice`) decides whether the override is offered.

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"context"
	"errors"
	"testing"
)

func TestDefaultApproverRoundTripAndGuards(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, _ := seedRequestActors(t, s, ctx)
	admin := mgr
	grantApprovalPermission(t, s, ctx, mgr.ID)

	// Unset by default.
	if id, err := s.DefaultApprover(ctx, req.ID); err != nil || id != 0 {
		t.Fatalf("DefaultApprover = %d, %v; want 0, nil", id, err)
	}
	if err := s.SetDefaultApprover(ctx, admin, req.ID, mgr.ID); err != nil {
		t.Fatalf("SetDefaultApprover: %v", err)
	}
	if id, _ := s.DefaultApprover(ctx, req.ID); id != mgr.ID {
		t.Fatalf("DefaultApprover = %d, want %d", id, mgr.ID)
	}
	u, err := s.UserByID(ctx, req.ID)
	if err != nil || u.DefaultApproverID == nil || *u.DefaultApproverID != mgr.ID {
		t.Fatalf("User.DefaultApproverID = %v, %v", u.DefaultApproverID, err)
	}
	// G8 again: a person can never be their own default approver.
	if err := s.SetDefaultApprover(ctx, admin, req.ID, req.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("self as default approver = %v, want ErrValidation", err)
	}
	// The nominee must actually be able to approve.
	nobodyID, _ := s.CreateUser(ctx, "nobody@example.com", "No Body", "hash", "data_entry", true)
	if err := s.SetDefaultApprover(ctx, admin, req.ID, nobodyID); !errors.Is(err, ErrValidation) {
		t.Fatalf("non-approver as default = %v, want ErrValidation", err)
	}
	// 0 clears it.
	if err := s.SetDefaultApprover(ctx, admin, req.ID, 0); err != nil {
		t.Fatal(err)
	}
	if id, _ := s.DefaultApprover(ctx, req.ID); id != 0 {
		t.Fatalf("cleared default = %d, want 0", id)
	}
	audit, err := s.Audit(ctx, "user", req.ID, 5)
	if err != nil || len(audit) == 0 {
		t.Fatalf("audit = %#v, %v; want default-approver entries", audit, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestDefaultApproverRoundTrip -v`
Expected: FAIL — `s.DefaultApprover undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `User` in `internal/store/models.go`:

```go
	// DefaultApproverID pre-selects this person's approver on the request form
	// (G9). nil means "no default"; the requester picks from ListApprovers.
	DefaultApproverID *int64
```

In `internal/store/store.go`, append `default_approver_id` to the three user SELECT lists (`UserByEmail`, `UserByID`, `ListUsers`) and extend `scanUser`:

```go
func scanUser(scanner interface{ Scan(...any) error }) (User, error) {
	var u User
	var active int
	var defaultApprover sql.NullInt64
	err := scanner.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &active,
		&u.CreatedAt, &u.UpdatedAt, &defaultApprover)
	if err == sql.ErrNoRows {
		return u, ErrNotFound
	}
	u.Active = active == 1
	if defaultApprover.Valid {
		v := defaultApprover.Int64
		u.DefaultApproverID = &v
	}
	return u, err
}
```

Create `internal/store/approvers.go`:

```go
package store

import (
	"context"
	"database/sql"
	"fmt"
)

// DefaultApprover returns the user's pre-selected approver, or 0 when unset.
func (s *Store) DefaultApprover(ctx context.Context, userID int64) (int64, error) {
	var id sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT default_approver_id FROM users WHERE id=?`, userID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if !id.Valid {
		return 0, nil
	}
	return id.Int64, nil
}

// SetDefaultApprover nominates the approver a person's requests default to.
// approverID 0 clears it. The nominee must hold approval:approve and can never
// be the person themselves (G8).
func (s *Store) SetDefaultApprover(ctx context.Context, actor User, userID, approverID int64) error {
	if userID <= 0 {
		return fmt.Errorf("%w: unknown user", ErrValidation)
	}
	if approverID == userID {
		return fmt.Errorf("%w: a person cannot be their own approver", ErrValidation)
	}
	if approverID > 0 {
		approvers, err := s.ListApprovers(ctx, userID)
		if err != nil {
			return err
		}
		ok := false
		for _, a := range approvers {
			ok = ok || a.ID == approverID
		}
		if !ok {
			return fmt.Errorf("%w: that person cannot approve requests", ErrValidation)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET default_approver_id=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		nullableID(approverID), userID); err != nil {
		return classify(err)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "update", EntityType: "user", EntityID: &userID,
		Summary: "Set default approver", After: map[string]any{"default_approver_id": approverID}}); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestDefaultApproverRoundTrip -v`
Then: `go test ./internal/store/ -run TestUser -v` (the widened scan list must not have broken the existing user tests).
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/models.go internal/store/store.go internal/store/approvers.go internal/store/approvers_test.go
git commit -m "feat(store): default approver per user"
```

---
### Task 10: `UpdateRequest` (returned/pending edit; reroute + reminder reset)

**Files:**
- Modify: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `validateRequestInput`, `validateUrgency`, `forcesRequesterPayee`, `requestInTx`, `recordAuditTx`.
- Produces: `func (s *Store) UpdateRequest(ctx, actor User, id int64, in RequestInput) error` — editable in **returned** and **pending** only (D1 removed `draft`); pending edits reset `reminder_last_sent` and reroute `manager_id`.

- [ ] **Step 1: Write the failing test**

```go
func TestUpdateRequestPendingReroutesAndResetsReminder(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	newMgrID, err := s.CreateUser(ctx, "mgr2@example.com", "Second Manager", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	base := RequestInput{Treatment: "budget", Type: "vendor_advance", ShortTitle: "Advance",
		ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "advance", ManagerID: mgr.ID,
		VendorID: vendorID, AdvanceReason: "40% booking"}
	id, err := s.CreateRequest(ctx, req, base)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a reminder having been sent.
	if _, err := s.DB().Exec(`UPDATE payment_requests SET reminder_last_sent=CURRENT_TIMESTAMP WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	// Requester edits while pending, choosing a different approver.
	edited := base
	edited.Amount, edited.Purpose, edited.ManagerID = 2500, "advance revised", newMgrID
	if err := s.UpdateRequest(ctx, req, id, edited); err != nil {
		t.Fatalf("UpdateRequest pending: %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.Amount != 2500 || got.ManagerID != newMgrID {
		t.Fatalf("edit not applied: amount=%d manager=%d", got.Amount, got.ManagerID)
	}
	if got.ReminderLastSent != nil {
		t.Fatalf("reminder timer not reset: %v", got.ReminderLastSent)
	}
	if got.Status != "pending" {
		t.Fatalf("status changed on edit: %q", got.Status)
	}
	// A returned request is editable too — that is the correction flow.
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='returned' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRequest(ctx, req, id, edited); err != nil {
		t.Fatalf("UpdateRequest returned: %v", err)
	}
}

func TestUpdateRequestRejectedAfterApproval(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	in := RequestInput{Treatment: "budget", Type: "vendor_advance", ShortTitle: "Advance",
		ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "advance", ManagerID: mgr.ID,
		VendorID: vendorID, AdvanceReason: "booking"}
	id, _ := s.CreateRequest(ctx, req, in)
	// Drive it to approved directly (ApproveRequest arrives in Task 12) to prove edits are then blocked.
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='approved' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	in.Amount = 999
	if err := s.UpdateRequest(ctx, req, id, in); !errors.Is(err, ErrValidation) {
		t.Fatalf("editing approved request = %v, want ErrValidation", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestUpdateRequest -v`
Expected: FAIL — `s.UpdateRequest undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/requests.go`:

```go
// editableStatuses: D1 removed 'draft'. A request may be corrected while it is
// still pending, or after the approver returned it.
var editableStatuses = map[string]bool{"returned": true, "pending": true}

func (s *Store) UpdateRequest(ctx context.Context, actor User, id int64, in RequestInput) error {
	in.RequesterID = actor.ID
	if forcesRequesterPayee(in.Type) {
		in.VendorID = 0
		in.VendorPayee = actor.Name
	}
	if err := validateRequestInput(in); err != nil {
		return err
	}
	mode, err := s.urgencyMode(ctx)
	if err != nil {
		return err
	}
	if err := validateUrgency(in, mode); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.RequesterID != actor.ID {
		return ErrForbidden
	}
	if !editableStatuses[before.Status] {
		return fmt.Errorf("%w: a %s request cannot be edited", ErrValidation, before.Status)
	}
	// Pending edits reset the reminder timer and reroute to the chosen approver.
	// P5 hook: re-notify the (possibly new) approver here.
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET
 treatment=?, type=?, recoverable_category=?, project_id=?, head_id=?, vendor_id=?, vendor_payee=?,
 short_title=?, amount=?, purpose=?, needed_by=?, invoice_no=?, invoice_date=?, expense_date=?,
 advance_reason=?, counterparty=?, expected_return_date=?, repayment_notes=?, urgent=?,
 urgency_reason=?, attachment_exception_reason=?, manager_id=?, reminder_last_sent=NULL,
 updated_at=CURRENT_TIMESTAMP
 WHERE id=?`,
		in.Treatment, in.Type, in.RecoverableCategory, nullableID(in.ProjectID), nullableID(in.HeadID),
		nullableID(in.VendorID), in.VendorPayee, strings.TrimSpace(in.ShortTitle),
		in.Amount, in.Purpose, nullableText(in.NeededBy), in.InvoiceNo,
		nullableText(in.InvoiceDate), nullableText(in.ExpenseDate), in.AdvanceReason,
		in.Counterparty, nullableText(in.ExpectedReturnDate), in.RepaymentNotes,
		boolInt(in.Urgent), in.UrgencyReason, in.AttachmentExceptionReason, in.ManagerID, id); err != nil {
		return classify(err)
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "update", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " edited request " + before.Number, Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestUpdateRequest -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): UpdateRequest edit with reroute and reminder reset"
```

---

### Task 11: `WithdrawRequest`

**Files:**
- Modify: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `canTransition`, `requestInTx`, `recordAuditTx`.
- Produces: `func (s *Store) WithdrawRequest(ctx, actor User, id int64) error` (pending → withdrawn; requester only).

**Amendment:** the old test opened by proving a *draft* could not be withdrawn. There are no drafts, so the negative leg now proves the boundary that actually matters: an **approved** request cannot be withdrawn unilaterally — the requester must ask for cancellation instead (G1). That is the same assertion in the world D1 created.

- [ ] **Step 1: Write the failing test**

```go
func TestWithdrawRequestFromPending(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	mk := func() int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_advance",
			ShortTitle: "Advance", ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "advance",
			ManagerID: mgr.ID, VendorID: vendorID, AdvanceReason: "booking"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := mk()
	if err := s.WithdrawRequest(ctx, req, id); err != nil {
		t.Fatalf("WithdrawRequest: %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.Status != "withdrawn" {
		t.Fatalf("status = %q, want withdrawn", got.Status)
	}
	// Withdrawn is terminal.
	if err := s.WithdrawRequest(ctx, req, id); !errors.Is(err, ErrValidation) {
		t.Fatalf("double withdraw = %v, want ErrValidation", err)
	}
	// G1: an approved request cannot be withdrawn — cancellation is a decision
	// the approver makes, not something the requester does alone.
	other := mk()
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='approved' WHERE id=?`, other); err != nil {
		t.Fatal(err)
	}
	if err := s.WithdrawRequest(ctx, req, other); !errors.Is(err, ErrValidation) {
		t.Fatalf("withdraw approved = %v, want ErrValidation", err)
	}
	// Only the requester may withdraw.
	third := mk()
	if err := s.WithdrawRequest(ctx, mgr, third); !errors.Is(err, ErrForbidden) {
		t.Fatalf("withdraw by a non-requester = %v, want ErrForbidden", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestWithdrawRequestFromPending -v`
Expected: FAIL — `s.WithdrawRequest undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/requests.go`:

```go
func (s *Store) WithdrawRequest(ctx context.Context, actor User, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.RequesterID != actor.ID {
		return ErrForbidden
	}
	if !canTransition(before.Status, "withdrawn") {
		return fmt.Errorf("%w: a %s request cannot be withdrawn", ErrValidation, before.Status)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='withdrawn', updated_at=CURRENT_TIMESTAMP WHERE id=?`, id); err != nil {
		return err
	}
	after := before
	after.Status = "withdrawn"
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "withdraw", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " withdrew request " + before.Number, Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestWithdrawRequestFromPending -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): WithdrawRequest pending to withdrawn"
```

---

### Task 12: `ApproveRequest` (adjustable amount, assigned-approver only, never the requester)

**Files:**
- Modify: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `canTransition`, `requestInTx`, `recordAuditTx`, `money.FormatPaise`.
- Produces: `func (s *Store) ApproveRequest(ctx, actor User, id, approvedAmount int64, note string) error` (pending → approved, sets `approved_amount`/`approved_by`/`approved_at`; only `manager_id == actor.ID`, and **never** when `actor.ID == requester_id`, G8).

- [ ] **Step 1: Write the failing test**

```go
func TestApproveRequestAdjustsAmountAndIsAssignedOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	otherID, _ := s.CreateUser(ctx, "other@example.com", "Other Manager", "hash", "admin", true)
	other, _ := s.UserByID(ctx, otherID)
	mk := func(amount int64, purpose string) int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
			ShortTitle: purpose, ProjectID: 1, HeadID: headID, Amount: amount, Purpose: purpose,
			ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: "A/" + purpose, InvoiceDate: "2026-07-18"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := mk(100000, "inv")

	// A non-assigned manager cannot approve.
	if err := s.ApproveRequest(ctx, other, id, 100000, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-assigned approve = %v, want ErrForbidden", err)
	}
	// Assigned manager approves with an adjusted amount.
	if err := s.ApproveRequest(ctx, mgr, id, 90000, "approved for 90k"); err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.Status != "approved" || got.ApprovedAmount == nil || *got.ApprovedAmount != 90000 {
		t.Fatalf("approved = %+v", got)
	}
	if got.ApprovedBy == nil || *got.ApprovedBy != mgr.ID || got.ApprovedAt == nil {
		t.Fatalf("approval metadata = %+v", got)
	}
	// Zero/negative approved amount is rejected.
	id2 := mk(5000, "inv2")
	if err := s.ApproveRequest(ctx, mgr, id2, 0, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("zero approved amount = %v, want ErrValidation", err)
	}
	// G8, defence in depth: even a row that somehow routed to its own requester
	// cannot be self-approved.
	if _, err := s.DB().Exec(`UPDATE payment_requests SET manager_id=requester_id WHERE id=?`, id2); err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveRequest(ctx, req, id2, 5000, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("self-approval at approve time = %v, want ErrForbidden", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestApproveRequestAdjusts -v`
Expected: FAIL — `s.ApproveRequest undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/requests.go`:

```go
func (s *Store) ApproveRequest(ctx context.Context, actor User, id, approvedAmount int64, note string) error {
	if approvedAmount <= 0 {
		return fmt.Errorf("%w: approved amount must be positive", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	// G8: last line of defence. The form never offers it, validateRequestInput
	// rejects it, and this refuses it even if a row reached that state.
	if before.RequesterID == actor.ID {
		return ErrForbidden
	}
	if !canTransition(before.Status, "approved") {
		return fmt.Errorf("%w: a %s request cannot be approved", ErrValidation, before.Status)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='approved', approved_amount=?, approved_by=?, approved_at=CURRENT_TIMESTAMP, decision_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, approvedAmount, actor.ID, strings.TrimSpace(note), id); err != nil {
		return err
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "approve", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " approved request " + before.Number + " for " + money.FormatPaise(approvedAmount),
		Before:  before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestApproveRequestAdjusts -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): ApproveRequest with adjustable amount, assigned-only"
```

---

### Task 13: `ReturnRequest` + `RejectRequest` (required text)

**Files:**
- Modify: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `canTransition`, `requestInTx`, `recordAuditTx`.
- Produces: `func (s *Store) ReturnRequest(ctx, actor User, id int64, comment string) error` (pending → returned; comment required); `func (s *Store) RejectRequest(ctx, actor User, id int64, reason string) error` (pending → rejected, terminal; reason required). Both assigned-approver only.

- [ ] **Step 1: Write the failing test**

```go
func TestReturnAndRejectRequireTextAndAreAssignedOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	mk := func() int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_advance",
			ShortTitle: "Advance", ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "advance",
			ManagerID: mgr.ID, VendorID: vendorID, AdvanceReason: "booking"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	rid := mk()
	if err := s.ReturnRequest(ctx, mgr, rid, "  "); !errors.Is(err, ErrValidation) {
		t.Fatalf("return without comment = %v, want ErrValidation", err)
	}
	if err := s.ReturnRequest(ctx, mgr, rid, "please attach the quote"); err != nil {
		t.Fatalf("ReturnRequest: %v", err)
	}
	got, _ := s.Request(ctx, rid)
	if got.Status != "returned" || got.DecisionReason != "please attach the quote" {
		t.Fatalf("returned = %+v", got)
	}
	// Returned can be resubmitted (returned -> pending), keeping its number.
	if err := s.SubmitRequest(ctx, req, rid); err != nil {
		t.Fatalf("resubmit returned: %v", err)
	}
	after, _ := s.Request(ctx, rid)
	if after.Number != got.Number {
		t.Fatalf("resubmission changed the number: %q -> %q", got.Number, after.Number)
	}

	jid := mk()
	if err := s.RejectRequest(ctx, mgr, jid, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("reject without reason = %v, want ErrValidation", err)
	}
	if err := s.RejectRequest(ctx, mgr, jid, "duplicate of PR-1"); err != nil {
		t.Fatalf("RejectRequest: %v", err)
	}
	got, _ = s.Request(ctx, jid)
	if got.Status != "rejected" {
		t.Fatalf("status = %q, want rejected", got.Status)
	}
	// Rejected is terminal — cannot resubmit.
	if err := s.SubmitRequest(ctx, req, jid); !errors.Is(err, ErrValidation) {
		t.Fatalf("resubmit rejected = %v, want ErrValidation", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestReturnAndReject -v`
Expected: FAIL — `s.ReturnRequest undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/requests.go`:

```go
func (s *Store) decideRequest(ctx context.Context, actor User, id int64, to, action, reason, missingMsg string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: %s", ErrValidation, missingMsg)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	if !canTransition(before.Status, to) {
		return fmt.Errorf("%w: a %s request cannot be %s", ErrValidation, before.Status, to)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status=?, decision_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, to, reason, id); err != nil {
		return err
	}
	after := before
	after.Status, after.DecisionReason = to, reason
	summary := map[string]string{
		"returned": " returned request ",
		"rejected": " rejected request ",
	}[to]
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: action, EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + summary + before.Number + ": " + reason, Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReturnRequest(ctx context.Context, actor User, id int64, comment string) error {
	return s.decideRequest(ctx, actor, id, "returned", "return", comment, "a comment is required to return a request")
}

func (s *Store) RejectRequest(ctx context.Context, actor User, id int64, reason string) error {
	return s.decideRequest(ctx, actor, id, "rejected", "reject", reason, "a reason is required to reject a request")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestReturnAndReject -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): ReturnRequest and RejectRequest with required text"
```

---

### Task 14: `ReassignRequest` + `ReraiseRequest` (a re-raise is a new **pending** request)

**Files:**
- Modify: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `requestInTx`, `NextRequestNumber`, `requestNumberYear`, `recordAuditTx`. Permission (`approval:reassign`, `request:reraise`) is enforced at the route layer (Task 19).
- Produces: `func (s *Store) ReassignRequest(ctx, actor User, id, newManagerID int64, reason string) error` (pending only; reason + history; reroutes `manager_id`, resets reminder); `func (s *Store) ReraiseRequest(ctx, actor User, id int64) (int64, error)` — a fresh **pending** copy of a rejected request with a new number and its own `submitted_at`.

**Amendment (A6/D1):** the old implementation produced a `draft`. There is no draft state, so a re-raise is simply a new submitted request that copies the rejected one's fields. The requester edits it afterwards through the normal pending-edit path if they need to.

- [ ] **Step 1: Write the failing test**

```go
func TestReassignAndReraise(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	admin, _ := s.UserByID(ctx, mgr.ID) // acts as admin reassigner
	newMgrID, _ := s.CreateUser(ctx, "cover@example.com", "Cover Manager", "hash", "admin", true)
	mk := func(managerID, amount int64) int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_advance",
			ShortTitle: "Advance", ProjectID: 1, HeadID: headID, Amount: amount, Purpose: "advance",
			ManagerID: managerID, VendorID: vendorID, AdvanceReason: "booking"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := mk(mgr.ID, 1000)

	if err := s.ReassignRequest(ctx, admin, id, newMgrID, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("reassign without reason = %v, want ErrValidation", err)
	}
	if err := s.ReassignRequest(ctx, admin, id, newMgrID, "manager on leave"); err != nil {
		t.Fatalf("ReassignRequest: %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.ManagerID != newMgrID || got.Status != "pending" {
		t.Fatalf("after reassign manager=%d status=%q", got.ManagerID, got.Status)
	}
	audit, _ := s.Audit(ctx, "payment_request", id, 10)
	var reassigned bool
	for _, a := range audit {
		reassigned = reassigned || a.Action == "reassign"
	}
	if !reassigned {
		t.Fatal("reassign not recorded in history")
	}

	// D1: re-raising a rejected request creates a new PENDING request, not a draft.
	rid := mk(newMgrID, 4000)
	newMgr, _ := s.UserByID(ctx, newMgrID)
	if err := s.RejectRequest(ctx, newMgr, rid, "out of budget"); err != nil {
		t.Fatal(err)
	}
	copyID, err := s.ReraiseRequest(ctx, req, rid)
	if err != nil {
		t.Fatalf("ReraiseRequest: %v", err)
	}
	if copyID == rid {
		t.Fatal("re-raise did not create a new request")
	}
	orig, _ := s.Request(ctx, rid)
	fresh, _ := s.Request(ctx, copyID)
	if fresh.Status != "pending" {
		t.Fatalf("re-raised status = %q, want pending (D1: no drafts)", fresh.Status)
	}
	if fresh.SubmittedAt == nil {
		t.Fatal("re-raised request was not submitted")
	}
	if fresh.Amount != 4000 || fresh.Number == orig.Number {
		t.Fatalf("fresh copy = %+v (orig number %s)", fresh, orig.Number)
	}
	// Only the requester may re-raise, and only a rejected request.
	if _, err := s.ReraiseRequest(ctx, newMgr, rid); !errors.Is(err, ErrForbidden) {
		t.Fatalf("re-raise by a non-requester = %v, want ErrForbidden", err)
	}
	if _, err := s.ReraiseRequest(ctx, req, copyID); !errors.Is(err, ErrValidation) {
		t.Fatalf("re-raise of a pending request = %v, want ErrValidation", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestReassignAndReraise -v`
Expected: FAIL — `s.ReassignRequest undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/requests.go`:

```go
func (s *Store) ReassignRequest(ctx context.Context, actor User, id, newManagerID int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a reason is required to reassign", ErrValidation)
	}
	if newManagerID <= 0 {
		return fmt.Errorf("%w: choose an approver to reassign to", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.Status != "pending" {
		return fmt.Errorf("%w: only a pending request can be reassigned", ErrValidation)
	}
	// G8 holds for reassignment too.
	if newManagerID == before.RequesterID {
		return fmt.Errorf("%w: a request cannot be reassigned to its own requester", ErrValidation)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET manager_id=?, decision_reason=?, reminder_last_sent=NULL, updated_at=CURRENT_TIMESTAMP WHERE id=?`, newManagerID, reason, id); err != nil {
		return classify(err)
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "reassign", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " reassigned request " + before.Number + " to " + after.ManagerName + ": " + reason,
		Before:  before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

// ReraiseRequest copies a rejected request into a new one. D1: the copy is
// created already pending with its own number — there is no draft to land in.
func (s *Store) ReraiseRequest(ctx context.Context, actor User, id int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	src, err := requestInTx(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	if src.RequesterID != actor.ID {
		return 0, ErrForbidden
	}
	if src.Status != "rejected" {
		return 0, fmt.Errorf("%w: only a rejected request can be re-raised", ErrValidation)
	}
	year, err := requestNumberYear(tx, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	number, err := NextRequestNumber(tx, year)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO payment_requests
 (number,status,treatment,type,recoverable_category,recoverable_category_id,project_id,head_id,
  vendor_id,vendor_payee,short_title,amount,purpose,needed_by,invoice_no,invoice_date,expense_date,
  advance_reason,counterparty,expected_return_date,repayment_notes,urgent,urgency_reason,
  attachment_exception_reason,requester_id,manager_id,submitted_at)
 SELECT ?, 'pending', treatment, type, recoverable_category, recoverable_category_id, project_id, head_id,
  vendor_id, vendor_payee, short_title, amount, purpose, needed_by, invoice_no, invoice_date, expense_date,
  advance_reason, counterparty, expected_return_date, repayment_notes, urgent, urgency_reason,
  attachment_exception_reason, requester_id, manager_id, CURRENT_TIMESTAMP
 FROM payment_requests WHERE id=?`, number, id)
	if err != nil {
		return 0, classify(err)
	}
	newID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "reraise", EntityType: "payment_request", EntityID: &newID,
		Summary: actor.Name + " re-raised " + src.Number + " as " + number,
		After:   map[string]any{"source": src.Number, "number": number}}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newID, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestReassignAndReraise -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): ReassignRequest and ReraiseRequest into a new pending request"
```

---
### Task 15: Post-approval cancellation flow (G1, G2, G3)

**Files:**
- Modify: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `canTransition` (Task 3, which already carries `approved → cancellation_requested|cancelled` and `cancellation_requested → cancelled|approved`), `requestInTx`, `recordAuditTx`.
- Produces:
  - `func (s *Store) RequestCancellation(ctx, actor User, id int64, reason string) error` — requester only, `approved → cancellation_requested`, reason required. **This is the freeze.**
  - `func (s *Store) DecideCancellation(ctx, actor User, id int64, accept bool, note string) error` — assigned approver only, `cancellation_requested → cancelled` (accept, note optional) or `→ approved` (decline, note **required**).
  - `func (s *Store) CancelRequest(ctx, actor User, id int64, reason string) error` — G2: the approver cancels an approved (or cancellation-requested) request outright, reason required.

**How the payment freeze works — read this before Phase 3.** There is no separate "frozen" flag. Phase 3's reservation and payment paths key on `status = 'approved'`; `cancellation_requested` is not `approved`, so a frozen request simply cannot be reserved or paid, and an existing reservation is surfaced to the accountant by the same status. Phase 3 must therefore filter its queue on `approved` **only** — never on "not rejected". That is asserted here, in this phase, by `TestCancellationFreezesTheApprovedState`.

- [ ] **Step 1: Write the failing test**

```go
func TestCancellationRequestAcceptAndDecline(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Anand Steel Traders")
	mkApproved := func() int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_advance",
			ShortTitle: "Binding wire order", ProjectID: 1, HeadID: headID, Amount: 47000,
			Purpose: "advance", ManagerID: mgr.ID, VendorID: vendorID, AdvanceReason: "40% booking"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ApproveRequest(ctx, mgr, id, 47000, ""); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// --- Employee asks; payment freezes. ---
	id := mkApproved()
	if err := s.RequestCancellation(ctx, req, id, "  "); !errors.Is(err, ErrValidation) {
		t.Fatalf("cancellation without a reason = %v, want ErrValidation", err)
	}
	if err := s.RequestCancellation(ctx, mgr, id, "not mine to cancel"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cancellation asked by a non-requester = %v, want ErrForbidden", err)
	}
	if err := s.RequestCancellation(ctx, req, id, "Site cancelled the order"); err != nil {
		t.Fatalf("RequestCancellation: %v", err)
	}
	got, _ := s.Request(ctx, id)
	if got.Status != "cancellation_requested" || got.CancelReason != "Site cancelled the order" {
		t.Fatalf("after asking = %+v", got)
	}

	// --- Manager declines: it goes back to approved and Accounts may proceed. ---
	if err := s.DecideCancellation(ctx, mgr, id, false, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("decline without a reason = %v, want ErrValidation", err)
	}
	if err := s.DecideCancellation(ctx, req, id, false, "keep it live"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("decision by a non-approver = %v, want ErrForbidden", err)
	}
	if err := s.DecideCancellation(ctx, mgr, id, false, "Vendor already dispatched; we owe them"); err != nil {
		t.Fatalf("DecideCancellation decline: %v", err)
	}
	got, _ = s.Request(ctx, id)
	if got.Status != "approved" {
		t.Fatalf("declined cancellation left status %q, want approved", got.Status)
	}

	// --- Manager accepts: the request is cancelled and closed. ---
	if err := s.RequestCancellation(ctx, req, id, "Order withdrawn by the site"); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideCancellation(ctx, mgr, id, true, "Agreed, no cancellation charge"); err != nil {
		t.Fatalf("DecideCancellation accept: %v", err)
	}
	got, _ = s.Request(ctx, id)
	if got.Status != "cancelled" {
		t.Fatalf("accepted cancellation left status %q, want cancelled", got.Status)
	}
	// Cancelled is terminal.
	if err := s.RequestCancellation(ctx, req, id, "again"); !errors.Is(err, ErrValidation) {
		t.Fatalf("cancellation of a cancelled request = %v, want ErrValidation", err)
	}

	// --- G2: the approver may cancel outright, with a reason. ---
	direct := mkApproved()
	if err := s.CancelRequest(ctx, mgr, direct, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("outright cancel without a reason = %v, want ErrValidation", err)
	}
	if err := s.CancelRequest(ctx, req, direct, "not my call"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outright cancel by the requester = %v, want ErrForbidden", err)
	}
	if err := s.CancelRequest(ctx, mgr, direct, "Budget pulled for the quarter"); err != nil {
		t.Fatalf("CancelRequest: %v", err)
	}
	got, _ = s.Request(ctx, direct)
	if got.Status != "cancelled" || got.CancelReason != "Budget pulled for the quarter" {
		t.Fatalf("outright cancel = %+v", got)
	}

	// Every step is on the audit trail, which is what `.thread` renders.
	audit, _ := s.Audit(ctx, "payment_request", id, 20)
	want := map[string]bool{"cancel_request": false, "cancel_decline": false, "cancel": false}
	for _, a := range audit {
		if _, ok := want[a.Action]; ok {
			want[a.Action] = true
		}
	}
	for action, seen := range want {
		if !seen {
			t.Fatalf("audit action %q missing from the thread", action)
		}
	}
}

// The freeze is the status itself: a frozen request is not `approved`, so no
// Phase-3 query that filters on `approved` can pick it up.
func TestCancellationFreezesTheApprovedState(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Anand Steel Traders")
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_advance",
		ShortTitle: "Wire", ProjectID: 1, HeadID: headID, Amount: 47000, Purpose: "advance",
		ManagerID: mgr.ID, VendorID: vendorID, AdvanceReason: "booking"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveRequest(ctx, mgr, id, 47000, ""); err != nil {
		t.Fatal(err)
	}
	payable, _ := s.ListRequests(ctx, RequestListOptions{Scope: "all", Status: "approved"})
	if len(payable) != 1 {
		t.Fatalf("approved queue = %d, want 1", len(payable))
	}
	if err := s.RequestCancellation(ctx, req, id, "order withdrawn"); err != nil {
		t.Fatal(err)
	}
	payable, _ = s.ListRequests(ctx, RequestListOptions{Scope: "all", Status: "approved"})
	if len(payable) != 0 {
		t.Fatalf("a frozen request is still in the payable queue: %#v", payable)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestCancellation -v`
Expected: FAIL — `s.RequestCancellation undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/requests.go`:

```go
// RequestCancellation is G1: the requester asks for an approved request to be
// cancelled. Payment freezes the moment this succeeds, because the request is
// no longer in the `approved` state that Accounts reserves from.
func (s *Store) RequestCancellation(ctx context.Context, actor User, id int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: say why it should be cancelled", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.RequesterID != actor.ID {
		return ErrForbidden
	}
	if !canTransition(before.Status, "cancellation_requested") {
		return fmt.Errorf("%w: a %s request cannot be sent for cancellation", ErrValidation, before.Status)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='cancellation_requested', cancel_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, reason, id); err != nil {
		return err
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	// P5 hook: notify the approver, and any accountant holding a reservation.
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "cancel_request", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " asked for cancellation of " + before.Number + ": " + reason + ". Payment frozen.",
		Before:  before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

// DecideCancellation is the approver's answer to G1. Accepting closes the
// request permanently; declining unfreezes it back to approved and requires a
// written reason, because Accounts and the requester both read it.
func (s *Store) DecideCancellation(ctx context.Context, actor User, id int64, accept bool, note string) error {
	note = strings.TrimSpace(note)
	if !accept && note == "" {
		return fmt.Errorf("%w: say why it should still be paid", ErrValidation)
	}
	to, action := "cancelled", "cancel_accept"
	if !accept {
		to, action = "approved", "cancel_decline"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	if before.Status != "cancellation_requested" || !canTransition(before.Status, to) {
		return fmt.Errorf("%w: there is no cancellation to decide on a %s request", ErrValidation, before.Status)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status=?, decision_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, to, note, id); err != nil {
		return err
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	summary := actor.Name + " cancelled " + before.Number + " at the requester's asking"
	if !accept {
		summary = actor.Name + " declined the cancellation of " + before.Number + ": " + note + ". Payment unfrozen."
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: action, EntityType: "payment_request", EntityID: &id,
		Summary: summary, Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

// CancelRequest is G2: the approver cancels an approved request outright,
// without the requester having asked. A reason is always required.
func (s *Store) CancelRequest(ctx context.Context, actor User, id int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a reason is required to cancel a request", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	if !canTransition(before.Status, "cancelled") {
		return fmt.Errorf("%w: a %s request cannot be cancelled", ErrValidation, before.Status)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='cancelled', cancel_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, reason, id); err != nil {
		return err
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "cancel", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " cancelled request " + before.Number + ": " + reason,
		Before:  before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestCancellation -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): post-approval cancellation request, decision and outright cancel"
```

---

### Task 16: Comments, attachments, and the merged history-and-conversation thread

**Files:**
- Modify: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `validateAttachment` (store.go), `recordAuditTx`, `requestInTx`, `Audit`.
- Produces: `AddRequestComment(ctx, actor, requestID int64, body string) (int64, error)`, `RequestComments(ctx, requestID int64) ([]RequestComment, error)`, `AddRequestAttachment(ctx, actor, requestID int64, in AttachmentInput) (int64, error)`, `RequestAttachments(ctx, requestID int64) ([]RequestAttachment, error)`, and **`RequestThread(ctx, requestID int64) ([]ThreadEntry, error)`**.

**Amendment (A19):** `request-detail-employee.html` renders **one** stream headed "History and conversation", with events, comments and attachments interleaved in time order and a single note: "Everyone who can see this request sees this whole stream." The old plan rendered a `Conversation` list and a separate `History` list. `RequestThread` merges them in the store so no template has to sort three slices together.

- [ ] **Step 1: Write the failing test**

```go
func TestRequestCommentsAndAttachments(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "Inv", ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: "inv",
		ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: "A/1", InvoiceDate: "2026-07-18"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.AddRequestComment(ctx, req, id, "  "); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty comment = %v, want ErrValidation", err)
	}
	if _, err := s.AddRequestComment(ctx, req, id, "here is the context"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRequestComment(ctx, mgr, id, "thanks, approved shortly"); err != nil {
		t.Fatal(err)
	}
	comments, err := s.RequestComments(ctx, id)
	if err != nil || len(comments) != 2 {
		t.Fatalf("comments = %#v, %v", comments, err)
	}
	if comments[0].AuthorName != req.Name || comments[1].AuthorName != mgr.Name {
		t.Fatalf("comment authors = %q,%q", comments[0].AuthorName, comments[1].AuthorName)
	}

	if _, err := s.AddRequestAttachment(ctx, req, id, AttachmentInput{OriginalName: "quote.pdf", StoredPath: "/tmp/quote.pdf", MimeType: "application/pdf", SizeBytes: 12}); err != nil {
		t.Fatal(err)
	}
	atts, err := s.RequestAttachments(ctx, id)
	if err != nil || len(atts) != 1 || atts[0].OriginalName != "quote.pdf" {
		t.Fatalf("attachments = %#v, %v", atts, err)
	}
}

// A19: history and conversation are ONE chronological stream.
func TestRequestThreadMergesEventsCommentsAndFiles(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "Inv", ProjectID: 1, HeadID: headID, Amount: 96000, Purpose: "inv",
		ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: "SE/26-27/1180", InvoiceDate: "2026-07-18"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRequestComment(ctx, req, id, "Vendor reissued the invoice."); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddRequestAttachment(ctx, req, id, AttachmentInput{OriginalName: "SE-26-27-1184.pdf", StoredPath: "/tmp/a.pdf", MimeType: "application/pdf", SizeBytes: 214000}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRequest(ctx, req, id, RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "Inv", ProjectID: 1, HeadID: headID, Amount: 100000, Purpose: "inv",
		ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: "SE/26-27/1184", InvoiceDate: "2026-07-18"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveRequest(ctx, mgr, id, 100000, "Panel count matches."); err != nil {
		t.Fatal(err)
	}

	thread, err := s.RequestThread(ctx, id)
	if err != nil {
		t.Fatalf("RequestThread: %v", err)
	}
	if len(thread) < 5 {
		t.Fatalf("thread has %d entries, want the submit, comment, attachment, edit and approval", len(thread))
	}
	kinds := map[string]int{}
	for _, e := range thread {
		kinds[e.Kind]++
	}
	for _, k := range []string{"event", "comment", "attachment"} {
		if kinds[k] == 0 {
			t.Fatalf("thread has no %q entries: %#v", k, thread)
		}
	}
	// Chronological, oldest first — the same order `.thread` renders.
	for i := 1; i < len(thread); i++ {
		if thread[i].CreatedAt.Before(thread[i-1].CreatedAt) {
			t.Fatalf("thread is not chronological at %d", i)
		}
	}
	if thread[0].Kind != "event" || thread[0].Action != "submit" {
		t.Fatalf("thread starts with %#v, want the submit event", thread[0])
	}
	// The edit entry carries a field-level diff for `.tl-change`.
	var sawChange bool
	for _, e := range thread {
		if e.Action == "update" && len(e.Changes) > 0 {
			sawChange = true
			for _, c := range e.Changes {
				if c.Field == "amount" && (c.Was == "" || c.Now == "") {
					t.Fatalf("amount change has no before/after: %#v", c)
				}
			}
		}
	}
	if !sawChange {
		t.Fatal("the edit entry carries no field-level changes")
	}
	// Comments carry initials for the `.tl-dot` avatar.
	for _, e := range thread {
		if e.Kind == "comment" && e.Initials == "" {
			t.Fatalf("comment entry without initials: %#v", e)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestRequestCommentsAndAttachments|TestRequestThreadMerges' -v`
Expected: FAIL — `s.AddRequestComment undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/requests.go`:

```go
func (s *Store) AddRequestComment(ctx context.Context, actor User, requestID int64, body string) (int64, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return 0, fmt.Errorf("%w: comment cannot be empty", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := requestInTx(ctx, tx, requestID); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO request_comments(request_id,author_id,body) VALUES(?,?,?)`, requestID, actor.ID, body)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "comment", EntityType: "payment_request", EntityID: &requestID,
		Summary: "Commented on request", After: map[string]any{"comment_id": id}}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) RequestComments(ctx context.Context, requestID int64) ([]RequestComment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.request_id,c.author_id,COALESCE(u.name,''),c.body,c.created_at
 FROM request_comments c JOIN users u ON u.id=c.author_id WHERE c.request_id=? ORDER BY c.created_at, c.id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RequestComment
	for rows.Next() {
		var c RequestComment
		if err := rows.Scan(&c.ID, &c.RequestID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) AddRequestAttachment(ctx context.Context, actor User, requestID int64, in AttachmentInput) (int64, error) {
	if err := validateAttachment(in); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := requestInTx(ctx, tx, requestID); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO request_attachments(request_id,original_name,stored_path,mime_type,size_bytes,uploaded_by) VALUES(?,?,?,?,?,?)`, requestID, in.OriginalName, in.StoredPath, in.MimeType, in.SizeBytes, actor.ID)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "attach", EntityType: "payment_request", EntityID: &requestID,
		Summary: "Uploaded attachment " + in.OriginalName, After: map[string]any{"id": id, "name": in.OriginalName}}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) RequestAttachments(ctx context.Context, requestID int64) ([]RequestAttachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,request_id,original_name,stored_path,COALESCE(mime_type,''),size_bytes,uploaded_by,created_at FROM request_attachments WHERE request_id=? ORDER BY created_at, id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RequestAttachment
	for rows.Next() {
		var a RequestAttachment
		if err := rows.Scan(&a.ID, &a.RequestID, &a.OriginalName, &a.StoredPath, &a.MimeType, &a.SizeBytes, &a.UploadedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// initials renders "Arun Mehta" as "AM" for the `.tl-dot` avatar.
func initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "?"
	}
	out := strings.ToUpper(parts[0][:1])
	if len(parts) > 1 {
		out += strings.ToUpper(parts[len(parts)-1][:1])
	}
	return out
}

// threadDiffFields are the fields an edit reports in `.tl-change`. Everything
// else changes too rarely, or too noisily, to be worth a line in the story.
var threadDiffFields = []string{"amount", "approved_amount", "needed_by", "invoice_no",
	"invoice_date", "expense_date", "manager_id", "short_title", "purpose", "urgent"}

func diffRequestAudit(beforeJSON, afterJSON string) []ThreadChange {
	var before, after map[string]any
	if json.Unmarshal([]byte(beforeJSON), &before) != nil || json.Unmarshal([]byte(afterJSON), &after) != nil {
		return nil
	}
	var out []ThreadChange
	for _, f := range threadDiffFields {
		key := auditFieldKey(f)
		was, now := fmt.Sprint(before[key]), fmt.Sprint(after[key])
		if before[key] == nil && after[key] == nil {
			continue
		}
		if was != now {
			out = append(out, ThreadChange{Field: f, Was: was, Now: now})
		}
	}
	return out
}

// auditFieldKey maps a column name to the key recordAuditTx serialised, which
// is the Go field name on Request (Amount, NeededBy, …).
func auditFieldKey(column string) string {
	parts := strings.Split(column, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	key := strings.Join(parts, "")
	if key == "InvoiceNo" {
		return "InvoiceNo"
	}
	if key == "ManagerId" {
		return "ManagerID"
	}
	if key == "ApprovedAmount" {
		return "ApprovedAmount"
	}
	return key
}

// RequestThread merges the audit trail, the conversation and the file uploads
// into one chronological stream — the single "History and conversation" list
// the design renders as `.thread` (UI/UX §9).
func (s *Store) RequestThread(ctx context.Context, requestID int64) ([]ThreadEntry, error) {
	audit, err := s.Audit(ctx, "payment_request", requestID, 200)
	if err != nil {
		return nil, err
	}
	comments, err := s.RequestComments(ctx, requestID)
	if err != nil {
		return nil, err
	}
	atts, err := s.RequestAttachments(ctx, requestID)
	if err != nil {
		return nil, err
	}
	out := make([]ThreadEntry, 0, len(audit)+len(comments)+len(atts))
	for _, a := range audit {
		// The comment and attach events are rendered by their own richer
		// entries below; keeping both would double every line.
		if a.Action == "comment" || a.Action == "attach" {
			continue
		}
		var actorID int64
		if a.ActorID != nil {
			actorID = *a.ActorID
		}
		out = append(out, ThreadEntry{Kind: "event", Action: a.Action, ActorID: actorID,
			ActorName: a.ActorName, Initials: initials(a.ActorName), Title: a.Summary,
			Changes: diffRequestAudit(a.BeforeJSON, a.AfterJSON), CreatedAt: a.CreatedAt})
	}
	for _, c := range comments {
		out = append(out, ThreadEntry{Kind: "comment", ActorID: c.AuthorID, ActorName: c.AuthorName,
			Initials: initials(c.AuthorName), Body: c.Body, CreatedAt: c.CreatedAt})
	}
	for _, a := range atts {
		out = append(out, ThreadEntry{Kind: "attachment", ActorID: a.UploadedBy, Title: "Attachment added",
			FileName: a.OriginalName, FileSize: a.SizeBytes, CreatedAt: a.CreatedAt})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
```

> Add `"encoding/json"` and `"sort"` to the imports of `internal/store/requests.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestRequestCommentsAndAttachments|TestRequestThreadMerges' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): merged history-and-conversation thread with attachments"
```

---

### Task 17: `ListRequests` + `CountRequests` (scope, buckets, filters)

**Files:**
- Modify: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `requestSelect`, `scanRequest`.
- Produces: `func (s *Store) ListRequests(ctx, opts RequestListOptions) ([]Request, error)`; `func (s *Store) CountRequests(ctx, opts RequestListOptions) (int, error)`; `var requestBuckets map[string][]string`.

**Amendment (A19):** `requests-list.html` and `approvals-list.html` both open on a `.segmented` tab bar — Open / Needs me / Closed / All, and To approve / Cancellations / Decided. Those tabs are `Bucket` values, resolved in SQL here rather than by filtering in Go, so the counts on the tabs and the rows under them can never disagree. `Type`, `Treatment` and `ProjectID` back the toolbar and the mobile filter sheet.

- [ ] **Step 1: Write the failing test**

```go
func TestListRequestsByScopeAndCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	otherID, _ := s.CreateUser(ctx, "someoneelse@example.com", "Someone Else", "hash", "data_entry", true)
	other, _ := s.UserByID(ctx, otherID)
	mk := func(actor User, amount int64, purpose string) int64 {
		id, err := s.CreateRequest(ctx, actor, RequestInput{Treatment: "budget", Type: "vendor_advance",
			ShortTitle: purpose, ProjectID: 1, HeadID: headID, Amount: amount, Purpose: purpose,
			ManagerID: mgr.ID, VendorID: vendorID, AdvanceReason: "booking"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	own := mk(req, 1000, "mine")
	foreign := mk(other, 2000, "theirs")

	ownList, err := s.ListRequests(ctx, RequestListOptions{Scope: "own", ViewerID: req.ID})
	if err != nil || len(ownList) != 1 || ownList[0].ID != own {
		t.Fatalf("own scope = %#v, %v", ownList, err)
	}
	assigned, _ := s.ListRequests(ctx, RequestListOptions{Scope: "assigned", ViewerID: mgr.ID, Status: "pending"})
	if len(assigned) != 2 {
		t.Fatalf("assigned pending count = %d, want 2", len(assigned))
	}
	all, _ := s.ListRequests(ctx, RequestListOptions{Scope: "all"})
	if len(all) != 2 {
		t.Fatalf("all scope = %d, want 2", len(all))
	}
	found, _ := s.ListRequests(ctx, RequestListOptions{Scope: "all", Query: "theirs"})
	if len(found) != 1 || found[0].ID != foreign {
		t.Fatalf("query filter = %#v", found)
	}
	n, err := s.CountRequests(ctx, RequestListOptions{Scope: "assigned", ViewerID: mgr.ID, Status: "pending"})
	if err != nil || n != 2 {
		t.Fatalf("CountRequests = %d, %v, want 2", n, err)
	}
	// Search matches the vendor name through the join, not just the snapshot.
	byVendor, _ := s.ListRequests(ctx, RequestListOptions{Scope: "all", Query: "acme"})
	if len(byVendor) != 2 {
		t.Fatalf("vendor-name search = %d, want 2", len(byVendor))
	}
}

// A19: the `.segmented` tabs are SQL buckets, so tab counts and tab contents
// can never disagree.
func TestListRequestsBuckets(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	vendorID := seedTestVendor(t, s, ctx, "Acme Supplies")
	mk := func(purpose string) int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_advance",
			ShortTitle: purpose, ProjectID: 1, HeadID: headID, Amount: 1000, Purpose: purpose,
			ManagerID: mgr.ID, VendorID: vendorID, AdvanceReason: "booking"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	pending := mk("pending one")
	returned := mk("returned one")
	rejected := mk("rejected one")
	if err := s.ReturnRequest(ctx, mgr, returned, "fix the invoice"); err != nil {
		t.Fatal(err)
	}
	if err := s.RejectRequest(ctx, mgr, rejected, "no budget"); err != nil {
		t.Fatal(err)
	}

	open, _ := s.CountRequests(ctx, RequestListOptions{Scope: "own", ViewerID: req.ID, Bucket: "open"})
	if open != 2 {
		t.Fatalf("open bucket = %d, want 2 (pending + returned)", open)
	}
	closed, _ := s.CountRequests(ctx, RequestListOptions{Scope: "own", ViewerID: req.ID, Bucket: "closed"})
	if closed != 1 {
		t.Fatalf("closed bucket = %d, want 1", closed)
	}
	// "Needs me" as the requester: the returned one is waiting on them.
	mine, _ := s.ListRequests(ctx, RequestListOptions{Scope: "own", ViewerID: req.ID, Bucket: "needs-me"})
	if len(mine) != 1 || mine[0].ID != returned {
		t.Fatalf("requester needs-me = %#v, want the returned request", mine)
	}
	// "Needs me" as the approver: the pending one is waiting on them.
	theirs, _ := s.ListRequests(ctx, RequestListOptions{Scope: "assigned", ViewerID: mgr.ID, Bucket: "needs-me"})
	if len(theirs) != 1 || theirs[0].ID != pending {
		t.Fatalf("approver needs-me = %#v, want the pending request", theirs)
	}
	// Toolbar filters.
	byType, _ := s.CountRequests(ctx, RequestListOptions{Scope: "all", Type: "vendor_advance"})
	if byType != 3 {
		t.Fatalf("type filter = %d, want 3", byType)
	}
	byTreatment, _ := s.CountRequests(ctx, RequestListOptions{Scope: "all", Treatment: "recoverable"})
	if byTreatment != 0 {
		t.Fatalf("treatment filter = %d, want 0", byTreatment)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestListRequestsByScope|TestListRequestsBuckets' -v`
Expected: FAIL — `s.ListRequests undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/store/requests.go`:

```go
// requestBuckets back the `.segmented` tabs. "needs-me" is scope-dependent and
// handled separately in requestWhere.
var requestBuckets = map[string][]string{
	"open":   {"pending", "returned", "approved", "cancellation_requested"},
	"closed": {"rejected", "withdrawn", "cancelled"},
}

func requestWhere(opts RequestListOptions) (string, []any) {
	var where []string
	var args []any
	switch opts.Scope {
	case "own":
		where = append(where, `r.requester_id=?`)
		args = append(args, opts.ViewerID)
	case "assigned":
		where = append(where, `r.manager_id=?`)
		args = append(args, opts.ViewerID)
	}
	placeholders := func(n int) string {
		return strings.TrimSuffix(strings.Repeat("?,", n), ",")
	}
	switch {
	case len(opts.Statuses) > 0:
		where = append(where, `r.status IN (`+placeholders(len(opts.Statuses))+`)`)
		for _, st := range opts.Statuses {
			args = append(args, st)
		}
	case opts.Bucket == "needs-me":
		// The one line the whole design turns on: who owes the next action.
		where = append(where, `((r.requester_id=? AND r.status='returned')
 OR (r.manager_id=? AND r.status IN ('pending','cancellation_requested')))`)
		args = append(args, opts.ViewerID, opts.ViewerID)
	case opts.Bucket != "" && opts.Bucket != "all":
		statuses := requestBuckets[opts.Bucket]
		if len(statuses) == 0 {
			statuses = []string{opts.Bucket}
		}
		where = append(where, `r.status IN (`+placeholders(len(statuses))+`)`)
		for _, st := range statuses {
			args = append(args, st)
		}
	case opts.Status != "" && opts.Status != "all":
		where = append(where, `r.status=?`)
		args = append(args, opts.Status)
	}
	if opts.Type != "" {
		where = append(where, `r.type=?`)
		args = append(args, opts.Type)
	}
	if opts.Treatment != "" {
		where = append(where, `r.treatment=?`)
		args = append(args, opts.Treatment)
	}
	if opts.ProjectID > 0 {
		where = append(where, `r.project_id=?`)
		args = append(args, opts.ProjectID)
	}
	if q := strings.ToLower(strings.TrimSpace(opts.Query)); q != "" {
		where = append(where, `(lower(r.number) LIKE ? ESCAPE '\' OR lower(r.purpose) LIKE ? ESCAPE '\'
 OR lower(r.short_title) LIKE ? ESCAPE '\' OR lower(r.invoice_no) LIKE ? ESCAPE '\'
 OR lower(COALESCE(v.name,r.vendor_payee)) LIKE ? ESCAPE '\' OR lower(ru.name) LIKE ? ESCAPE '\')`)
		q = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
		needle := "%" + q + "%"
		args = append(args, needle, needle, needle, needle, needle, needle)
	}
	clause := ""
	if len(where) > 0 {
		clause = ` WHERE ` + strings.Join(where, ` AND `)
	}
	return clause, args
}

func (s *Store) ListRequests(ctx context.Context, opts RequestListOptions) ([]Request, error) {
	if opts.Limit <= 0 {
		opts.Limit = 200
	}
	clause, args := requestWhere(opts)
	// Urgent first, then oldest first: the list is sorted by who has been kept
	// waiting longest, which is what requests-list.html promises.
	q := requestSelect + clause + ` ORDER BY r.urgent DESC, r.created_at DESC, r.id DESC LIMIT ?`
	args = append(args, opts.Limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CountRequests(ctx context.Context, opts RequestListOptions) (int, error) {
	clause, args := requestWhere(opts)
	q := `SELECT COUNT(*) FROM payment_requests r
 JOIN users ru ON ru.id=r.requester_id
 LEFT JOIN vendors v ON v.id=r.vendor_id` + clause
	var n int
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestListRequestsByScope|TestListRequestsBuckets' -v`
Then run the whole store suite: `go test ./internal/store/ -race`
Expected: PASS (every request store test green).

- [ ] **Step 5: Commit**

```bash
git add internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): ListRequests and CountRequests by scope, bucket and filters"
```

---

### Task 18: `SimilarRequests` — the duplicate check that never blocks (G6, store half)

**Files:**
- Modify: `internal/store/requests.go`
- Test: `internal/store/requests_test.go`

**Interfaces:**
- Consumes: `requestSelect`, `scanRequest`.
- Produces: `type SimilarRequestOptions`; `func (s *Store) SimilarRequests(ctx context.Context, opt SimilarRequestOptions) ([]Request, error)`.

**The rule, from `request-duplicate-warning.html`:** "The check compares payee, invoice reference, amount, project and head over the last 30 days," and "A duplicate warning never blocks submission. Legitimate repeat payments exist — the same rent, the same monthly retainer. The system points, the person decides." So this method is a **read**. It has no power to refuse anything, and no caller may treat an empty result as permission or a non-empty result as a denial.

- [ ] **Step 1: Write the failing test**

```go
func TestSimilarRequestsFindsRecentNearDuplicatesOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	sundaram := seedTestVendor(t, s, ctx, "Sundaram Electricals Pvt Ltd")
	anand := seedTestVendor(t, s, ctx, "Anand Steel Traders")
	mk := func(vendorID, amount int64, invoice string) int64 {
		id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "vendor_invoice",
			ShortTitle: "Switchgear", ProjectID: 1, HeadID: headID, Amount: amount, Purpose: "panels",
			ManagerID: mgr.ID, VendorID: vendorID, InvoiceNo: invoice, InvoiceDate: "2026-07-18"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	recentA := mk(sundaram, 10000000, "SE/26-27/1102")
	recentB := mk(sundaram, 10000000, "SE/26-27/1184")
	stale := mk(sundaram, 10000000, "SE/26-27/0901")
	_ = mk(anand, 10000000, "AS/26-27/0417")  // different vendor
	_ = mk(sundaram, 250000, "SE/26-27/1200") // same vendor, nowhere near the amount
	gone := mk(sundaram, 10000000, "SE/26-27/1300")
	if _, err := s.DB().Exec(`UPDATE payment_requests SET created_at=datetime('now','-31 days') WHERE id=?`, stale); err != nil {
		t.Fatal(err)
	}
	// A withdrawn request is not a duplicate of anything.
	if err := s.WithdrawRequest(ctx, req, gone); err != nil {
		t.Fatal(err)
	}

	got, err := s.SimilarRequests(ctx, SimilarRequestOptions{
		VendorID: sundaram, Amount: 10000000, InvoiceNo: "SE/26-27/1184",
	})
	if err != nil {
		t.Fatalf("SimilarRequests: %v", err)
	}
	ids := map[int64]bool{}
	for _, r := range got {
		ids[r.ID] = true
	}
	if !ids[recentA] || !ids[recentB] {
		t.Fatalf("near-duplicates missed: %#v", got)
	}
	if ids[stale] {
		t.Fatal("a request older than 30 days was reported as a duplicate")
	}
	if ids[gone] {
		t.Fatal("a withdrawn request was reported as a duplicate")
	}
	if len(got) != 2 {
		t.Fatalf("similar = %d, want exactly the two recent Sundaram requests", len(got))
	}
	// ExcludeID keeps an edit from flagging itself.
	got, _ = s.SimilarRequests(ctx, SimilarRequestOptions{VendorID: sundaram, Amount: 10000000, ExcludeID: recentB})
	for _, r := range got {
		if r.ID == recentB {
			t.Fatal("a request was reported as its own duplicate")
		}
	}
	// A free-text payee (reimbursement, employee advance) matches too.
	byPayee, _ := s.SimilarRequests(ctx, SimilarRequestOptions{Payee: "sundaram electricals pvt ltd", Amount: 10000000})
	if len(byPayee) != 2 {
		t.Fatalf("payee-name match = %d, want 2", len(byPayee))
	}
	// Nothing similar returns an empty slice, never an error.
	none, err := s.SimilarRequests(ctx, SimilarRequestOptions{VendorID: anand, Amount: 1})
	if err != nil || len(none) != 0 {
		t.Fatalf("no matches = %#v, %v; want empty, nil", none, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSimilarRequests -v`
Expected: FAIL — `undefined: SimilarRequestOptions`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/store/models.go`:

```go
// SimilarRequestOptions drives the duplicate check. It is advisory: the result
// is shown to the person and never gates the submit (G6).
type SimilarRequestOptions struct {
	ExcludeID int64
	VendorID  int64
	Payee     string
	Amount    int64
	InvoiceNo string
	Days      int // default 30
	Limit     int // default 5
}
```

Append to `internal/store/requests.go`:

```go
// SimilarRequests reports recent requests that look like the one being raised:
// the same payee, and either a near-identical amount or the very same invoice
// reference, inside the configured window. It is a warning, not a gate — callers
// must never refuse a submit on the strength of a non-empty result (G6).
func (s *Store) SimilarRequests(ctx context.Context, opt SimilarRequestOptions) ([]Request, error) {
	if opt.Days <= 0 {
		opt.Days = 30
	}
	if opt.Limit <= 0 {
		opt.Limit = 5
	}
	payee := strings.ToLower(strings.TrimSpace(opt.Payee))
	if opt.VendorID <= 0 && payee == "" {
		return nil, nil
	}
	// ±1% of the amount, so "₹1,00,000 again" is caught but "₹2,500" is not.
	tolerance := opt.Amount / 100
	if tolerance < 100 {
		tolerance = 100
	}
	invoice := strings.ToLower(strings.TrimSpace(opt.InvoiceNo))
	q := requestSelect + ` WHERE r.id<>?
 AND r.status NOT IN ('withdrawn','rejected','cancelled')
 AND r.created_at >= datetime('now', ?)
 AND (( ? > 0 AND r.vendor_id = ? ) OR ( ? <> '' AND lower(COALESCE(v.name, r.vendor_payee)) = ? ))
 AND (( ? > 0 AND abs(r.amount - ?) <= ? ) OR ( ? <> '' AND lower(r.invoice_no) = ? ))
 ORDER BY r.created_at DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q,
		opt.ExcludeID,
		fmt.Sprintf("-%d days", opt.Days),
		opt.VendorID, opt.VendorID,
		payee, payee,
		opt.Amount, opt.Amount, tolerance,
		invoice, invoice,
		opt.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestSimilarRequests -v`
Then: `go test ./internal/store/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/models.go internal/store/requests.go internal/store/requests_test.go
git commit -m "feat(store): advisory duplicate detection over the last 30 days"
```

---
### Task 19: Route table, `PageData`, the request form parser, and `renderPartial`

**Files:**
- Modify: `internal/app/app.go` (routes, `PageData` fields, `requestInput`, `canViewRequest`, `effectiveScope`, `renderPartial`, `deref` func)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `auth.Manager.Can`/`Scope`/`Permissions`, `RequirePermission`, the Phase-2 store methods, `a.withCSRF`, `a.respondStoreError`, `a.tpl`.
- Produces: every `/requests`, `/approvals` and `/configuration` route; `func requestInput(r *http.Request) (store.RequestInput, error)`; `func canViewRequest(scope string, u store.User, req store.Request) bool`; `func (a *App) effectiveScope(u store.User, requested string) string`; `func (a *App) renderPartial(w, r, name string, data PageData)`.

**This task is the shared plumbing the nine screen tasks (20–29) sit on.** It lands the routes and the parsing; each screen task then lands one template and its test. `renderPartial` is the render side of Phase 0 Task 16's `HX-Request` contract: Phase 0 skips building the shell, this skips rendering `top`/`bottom`.

**Permission verbs used.** `request`{view, create, edit, withdraw, reraise, comment, cancel}, `approval`{approve, return, reject, reassign, cancel}, `attachment`{create}, `config`{view, edit}. `request:cancel` and `approval:cancel` must be added to Phase 1 Task 3's `resourceActions` — see the cross-phase note in the amendment log.

- [ ] **Step 1: Write the failing test**

```go
// assignRole grants a Phase-1 seeded role to a user by name.
func (s *appTestServer) assignRole(userID int64, roleName string) {
	s.t.Helper()
	roles, err := s.st.AllRoles(s.ctx)
	if err != nil {
		s.t.Fatal(err)
	}
	var roleID int64
	for _, r := range roles {
		if strings.EqualFold(r.Name, roleName) {
			roleID = r.ID
		}
	}
	if roleID == 0 {
		s.t.Fatalf("seeded role %q not found", roleName)
	}
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.SetUserRoles(s.ctx, admin, userID, []int64{roleID}); err != nil {
		s.t.Fatal(err)
	}
}

func TestRequestRoutesAreRegisteredAndScoped(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Req")
	hash, _ := auth.HashPassword("RequesterPass123")
	rid, err := s.st.CreateUser(s.ctx, "rhea@example.test", "Rhea", hash, "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	s.assignRole(rid, "Requester")
	requester, _ := s.st.UserByID(s.ctx, rid)
	mgr, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	reqID, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "reimbursement", ShortTitle: "Lunch", ProjectID: 1, HeadID: headID, Amount: 1000,
		Purpose: "p", ExpenseDate: "2026-07-21", ManagerID: mgr.ID})
	if err != nil {
		t.Fatal(err)
	}

	s.login("rhea@example.test", "RequesterPass123")
	for _, path := range []string{"/requests", "/requests/new", "/requests/new?type=reimbursement",
		"/requests/" + strconvFormat(reqID), "/requests/export.csv?scope=own"} {
		resp := s.request(http.MethodGet, path, nil, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
		}
		_ = responseBody(t, resp)
	}
	// The export is real CSV with the Phase-2 column set.
	exp := s.request(http.MethodGet, "/requests/export.csv?scope=own", nil, "")
	if got := responseBody(t, exp); !strings.HasPrefix(got, "Number,Status,Type,Title,Amount,Payee,Requester,Approver,Created") {
		t.Fatalf("request export header unexpected: %s", got)
	}

	// A foreign requester is forbidden by URL (Q5/R6).
	hash2, _ := auth.HashPassword("OtherPass1234")
	oid, _ := s.st.CreateUser(s.ctx, "olga@example.test", "Olga", hash2, "data_entry", true)
	s.assignRole(oid, "Requester")
	s.login("olga@example.test", "OtherPass1234")
	resp := s.request(http.MethodGet, "/requests/"+strconvFormat(reqID), nil, "")
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)

	// A6/D5: no bulk-approve and no copy endpoint exist. `/requests/bulk-approve`
	// collides path-wise with `GET /requests/{id}`, so Go 1.22's mux answers 405;
	// `/requests/{id}/copy` matches no pattern at all, so it answers 404.
	bulk := s.postForm("/requests/bulk-approve", url.Values{})
	if bulk.StatusCode != http.StatusNotFound && bulk.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("bulk-approve status = %d, want 404/405 (endpoint must not exist)", bulk.StatusCode)
	}
	_ = responseBody(t, bulk)
	requireStatus(t, s.postForm("/requests/"+strconvFormat(reqID)+"/copy", url.Values{}), http.StatusNotFound)
}

// A15/A16: an htmx fragment renders without the shell.
func TestRenderPartialOmitsTheShell(t *testing.T) {
	s := newAppTestServer(t)
	s.seedHead("Frag")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	full := responseBody(t, s.request(http.MethodGet, "/requests/new?type=vendor_invoice", nil, ""))
	if !strings.Contains(full, "<aside") {
		t.Fatal("the full page is missing the shell")
	}
	frag := responseBody(t, s.request(http.MethodGet, "/requests/new/fields?type=vendor_invoice&treatment=budget", nil, "", "HX-Request", "true"))
	if strings.Contains(frag, "<aside") || strings.Contains(frag, "<!doctype") {
		t.Fatalf("the fragment carried the shell: %s", frag)
	}
	if !strings.Contains(frag, "fieldset") {
		t.Fatalf("the fragment rendered nothing useful: %s", frag)
	}
}

// X1: nothing tax/TDS-related may leak into the schema or the route table.
func TestNoTaxColumnsInSchema(t *testing.T) {
	s := newAppTestServer(t)
	db := s.st.DB()
	bad := func(s string) bool {
		s = strings.ToLower(s)
		return strings.Contains(s, "tax") || strings.Contains(s, "tds")
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	for _, tbl := range tables {
		// vendors.tds_section / tds_rate are Phase-1V statutory vendor master
		// fields, not request-workflow tax handling; they are out of scope here.
		if tbl == "vendors" {
			continue
		}
		if bad(tbl) {
			t.Fatalf("table name contains tax/tds: %q", tbl)
		}
		cols, err := db.Query(`SELECT name FROM pragma_table_info(?)`, tbl)
		if err != nil {
			t.Fatal(err)
		}
		for cols.Next() {
			var col string
			if err := cols.Scan(&col); err != nil {
				t.Fatal(err)
			}
			if bad(col) {
				cols.Close()
				t.Fatalf("column %q in table %q contains tax/tds", col, tbl)
			}
		}
		cols.Close()
	}
	// No /tax route exists.
	s.login(s.cfg.AdminEmail, testAdminPassword)
	if resp := s.request(http.MethodGet, "/tax", nil, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /tax = %d, want 404 (no tax route may exist)", resp.StatusCode)
	}
}
```

> `s.request` gains a trailing variadic `header ...string` pair list so the fragment test can send `HX-Request: true`. If the existing helper does not take one, add it — it is two lines and every later fragment test needs it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run 'TestRequestRoutesAreRegistered|TestRenderPartialOmits|TestNoTaxColumnsInSchema' -v`
Expected: FAIL — 404 on `/requests/new` (routes not registered).

- [ ] **Step 3: Write minimal implementation**

Add a `deref` helper to the `template.FuncMap` in `New` (nullable `*int64` columns are compared/formatted in the request templates and `html/template` cannot compare a pointer to an int), alongside the display helpers the design needs:

```go
		"deref": func(p *int64) int64 {
			if p == nil {
				return 0
			}
			return *p
		},
		// The pill modifier for a status: `pending` renders `.pill.awaiting`,
		// `cancellation_requested` renders `.pill.cancelreq`. Phase 0 owns the CSS.
		"pillClass": pillClass,
		"statusText": statusText,
		"inWords":    money.InWords,
		"initials":   initialsOf,
		"dateLong":   func(s string) string { return formatLongDate(s) },
```

Add `PageData` fields:

```go
	Requests    []store.Request
	Request2    store.Request
	Thread      []store.ThreadEntry
	RequestAtts []store.RequestAttachment
	Approvers   []store.User
	Similar     []store.Request
	Settings    map[string]string
	Scope       string
	Bucket      string
	FormType    string
	TypeFilter  string
	Treatment   string
	Counts      map[string]int
	Areas       []WorkArea
	// Vendors backs the <noscript> fallback on the request form. Phase 1V's
	// vendor screens may already have added this field — do not declare it twice.
	Vendors []store.Vendor
```

Register routes in `App.routes`:

```go
	mux.Handle("GET /dashboard", a.auth.RequireLogin(http.HandlerFunc(a.dashboard)))
	mux.Handle("GET /requests", a.auth.RequirePermission("request", "view", http.HandlerFunc(a.requests)))
	mux.Handle("GET /approvals", a.auth.RequirePermission("approval", "approve", http.HandlerFunc(a.approvals)))
	mux.Handle("GET /requests/export.csv", a.auth.RequirePermission("request", "view", http.HandlerFunc(a.requestsExport)))
	mux.Handle("GET /requests/new", a.auth.RequirePermission("request", "create", http.HandlerFunc(a.requestNew)))
	mux.Handle("GET /requests/new/fields", a.auth.RequirePermission("request", "create", http.HandlerFunc(a.requestFormFields)))
	mux.Handle("POST /requests/duplicate-check", a.auth.RequirePermission("request", "create", http.HandlerFunc(a.withCSRF(a.requestDuplicateCheck))))
	mux.Handle("POST /requests", a.auth.RequirePermission("request", "create", http.HandlerFunc(a.withCSRF(a.requestCreate))))
	mux.Handle("GET /requests/{id}", a.auth.RequirePermission("request", "view", http.HandlerFunc(a.requestDetail)))
	mux.Handle("GET /requests/{id}/submitted", a.auth.RequirePermission("request", "view", http.HandlerFunc(a.requestSubmitted)))
	mux.Handle("GET /requests/{id}/edit", a.auth.RequirePermission("request", "edit", http.HandlerFunc(a.requestEditForm)))
	mux.Handle("POST /requests/{id}/edit", a.auth.RequirePermission("request", "edit", http.HandlerFunc(a.withCSRF(a.requestEdit))))
	mux.Handle("POST /requests/{id}/submit", a.auth.RequirePermission("request", "edit", http.HandlerFunc(a.withCSRF(a.requestSubmit))))
	mux.Handle("POST /requests/{id}/withdraw", a.auth.RequirePermission("request", "withdraw", http.HandlerFunc(a.withCSRF(a.requestWithdraw))))
	mux.Handle("POST /requests/{id}/reraise", a.auth.RequirePermission("request", "reraise", http.HandlerFunc(a.withCSRF(a.requestReraise))))
	mux.Handle("POST /requests/{id}/comment", a.auth.RequirePermission("request", "comment", http.HandlerFunc(a.withCSRF(a.requestComment))))
	mux.Handle("POST /requests/{id}/attachments", a.auth.RequirePermission("attachment", "create", http.HandlerFunc(a.withCSRF(a.requestAttachmentUpload))))
	mux.Handle("POST /requests/{id}/approve", a.auth.RequirePermission("approval", "approve", http.HandlerFunc(a.withCSRF(a.requestApprove))))
	mux.Handle("POST /requests/{id}/return", a.auth.RequirePermission("approval", "return", http.HandlerFunc(a.withCSRF(a.requestReturn))))
	mux.Handle("POST /requests/{id}/reject", a.auth.RequirePermission("approval", "reject", http.HandlerFunc(a.withCSRF(a.requestReject))))
	mux.Handle("POST /requests/{id}/reassign", a.auth.RequirePermission("approval", "reassign", http.HandlerFunc(a.withCSRF(a.requestReassign))))
	// G1/G2: cancellation.
	mux.Handle("GET /requests/{id}/cancel", a.auth.RequirePermission("request", "cancel", http.HandlerFunc(a.requestCancelForm)))
	mux.Handle("POST /requests/{id}/cancel-request", a.auth.RequirePermission("request", "cancel", http.HandlerFunc(a.withCSRF(a.requestCancelAsk))))
	mux.Handle("GET /requests/{id}/cancellation", a.auth.RequirePermission("approval", "cancel", http.HandlerFunc(a.requestCancellationForm)))
	mux.Handle("POST /requests/{id}/cancellation", a.auth.RequirePermission("approval", "cancel", http.HandlerFunc(a.withCSRF(a.requestCancellationDecide))))
	mux.Handle("POST /requests/{id}/cancel", a.auth.RequirePermission("approval", "cancel", http.HandlerFunc(a.withCSRF(a.requestCancelOutright))))
	// D6: Configuration.
	mux.Handle("GET /configuration", a.auth.RequirePermission("config", "view", http.HandlerFunc(a.configuration)))
	mux.Handle("POST /configuration", a.auth.RequirePermission("config", "edit", http.HandlerFunc(a.withCSRF(a.configurationSave))))
```

Add the shared handlers and helpers to `internal/app/app.go`:

```go
// renderPartial executes one template without the shell. Every htmx fragment
// goes through it. Phase 0 Task 16 already skips shell CONSTRUCTION when
// HX-Request is present; this is the rendering half of the same contract.
func (a *App) renderPartial(w http.ResponseWriter, r *http.Request, name string, data PageData) {
	var buf bytes.Buffer
	if err := a.tpl.ExecuteTemplate(&buf, name, data); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "That section could not be rendered.", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(buf.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "fragment write failed", "request_id", requestID(r), "error", err)
	}
}

// pillClass maps a stored status onto the Phase 0 `.pill` modifier.
func pillClass(status string) string {
	switch status {
	case "pending":
		return "awaiting"
	case "cancellation_requested":
		return "cancelreq"
	case "withdrawn":
		return "cancelled"
	default:
		return status
	}
}

// statusText is the sentence a person reads, not the enum value.
func statusText(status string) string {
	switch status {
	case "pending":
		return "Awaiting approval"
	case "returned":
		return "Returned for correction"
	case "approved":
		return "Approved — awaiting payment"
	case "rejected":
		return "Rejected — final"
	case "withdrawn":
		return "Withdrawn"
	case "cancellation_requested":
		return "Cancellation requested"
	case "cancelled":
		return "Cancelled"
	default:
		return status
	}
}

func initialsOf(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "?"
	}
	out := strings.ToUpper(parts[0][:1])
	if len(parts) > 1 {
		out += strings.ToUpper(parts[len(parts)-1][:1])
	}
	return out
}

func formatLongDate(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("2 January 2006")
}

func requestInput(r *http.Request) (store.RequestInput, error) {
	in := store.RequestInput{
		Treatment:                 r.FormValue("treatment"),
		Type:                      r.FormValue("type"),
		RecoverableCategory:       r.FormValue("recoverable_category"),
		ProjectID:                 parseID(r.FormValue("project_id")),
		HeadID:                    parseID(r.FormValue("head_id")),
		VendorID:                  parseID(r.FormValue("vendor_id")),
		VendorPayee:               r.FormValue("vendor_payee"),
		ShortTitle:                r.FormValue("short_title"),
		Purpose:                   r.FormValue("purpose"),
		NeededBy:                  r.FormValue("needed_by"),
		InvoiceNo:                 r.FormValue("invoice_no"),
		InvoiceDate:               r.FormValue("invoice_date"),
		ExpenseDate:               r.FormValue("expense_date"),
		AdvanceReason:             r.FormValue("advance_reason"),
		Counterparty:              r.FormValue("counterparty"),
		ExpectedReturnDate:        r.FormValue("expected_return_date"),
		RepaymentNotes:            r.FormValue("repayment_notes"),
		Urgent:                    r.FormValue("urgent") == "on",
		UrgencyReason:             r.FormValue("urgency_reason"),
		AttachmentExceptionReason: r.FormValue("attachment_exception_reason"),
		ManagerID:                 parseID(r.FormValue("manager_id")),
	}
	amount, err := money.ParsePaise(r.FormValue("amount"))
	in.Amount = amount
	if err != nil {
		return in, fmt.Errorf("%w: invalid amount; enter a valid request amount", store.ErrValidation)
	}
	return in, nil
}

func canViewRequest(scope string, u store.User, req store.Request) bool {
	switch scope {
	case "all":
		return true
	case "assigned":
		return req.ManagerID == u.ID || req.RequesterID == u.ID
	case "own":
		return req.RequesterID == u.ID
	default:
		return false
	}
}

func (a *App) effectiveScope(u store.User, requested string) string {
	scope := a.auth.Scope(u, "request")
	rank := map[string]int{"own": 1, "assigned": 2, "all": 3}
	if rank[requested] > 0 && rank[requested] < rank[scope] {
		return requested
	}
	return scope
}

func (a *App) loadViewableRequest(w http.ResponseWriter, r *http.Request) (store.Request, bool) {
	u := auth.CurrentUser(r)
	req, err := a.st.Request(r.Context(), pathID(r))
	if err != nil {
		a.respondStoreError(w, r, err)
		return store.Request{}, false
	}
	if !canViewRequest(a.auth.Scope(u, "request"), u, req) {
		a.respondError(w, r, http.StatusForbidden, "You do not have permission to view this request.", nil)
		return store.Request{}, false
	}
	return req, true
}

// requestFormData assembles everything the adaptive form needs. The approver
// list comes from ListApprovers, so the requester's own name is structurally
// absent (G8), and the default approver is pre-selected (G9).
func (a *App) requestFormData(r *http.Request, title string) (PageData, error) {
	u := auth.CurrentUser(r)
	ctx := r.Context()
	projects, err := a.st.ListProjects(ctx, true)
	if err != nil {
		return PageData{}, err
	}
	heads, err := a.st.ListHeads(ctx, true)
	if err != nil {
		return PageData{}, err
	}
	approvers, err := a.st.ListApprovers(ctx, u.ID)
	if err != nil {
		return PageData{}, err
	}
	settings, err := a.st.AppSettings(ctx)
	if err != nil {
		return PageData{}, err
	}
	data := PageData{Title: title, Projects: projects, Heads: heads, Approvers: approvers, Settings: settings}
	if u.DefaultApproverID != nil {
		data.Request2.ManagerID = *u.DefaultApproverID
	}
	return data, nil
}

// Mutating handlers. Each is a thin shell over one store method: the store owns
// every rule, and the handler owns only the redirect.
func (a *App) requestEdit(w http.ResponseWriter, r *http.Request) {
	in, err := requestInput(r)
	if err == nil {
		err = a.st.UpdateRequest(r.Context(), auth.CurrentUser(r), pathID(r), in)
	}
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", pathID(r)), http.StatusSeeOther)
}

func (a *App) requestSubmit(w http.ResponseWriter, r *http.Request) {
	if err := a.st.SubmitRequest(r.Context(), auth.CurrentUser(r), pathID(r)); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", pathID(r)), http.StatusSeeOther)
}

func (a *App) requestWithdraw(w http.ResponseWriter, r *http.Request) {
	if err := a.st.WithdrawRequest(r.Context(), auth.CurrentUser(r), pathID(r)); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", pathID(r)), http.StatusSeeOther)
}

func (a *App) requestReraise(w http.ResponseWriter, r *http.Request) {
	id, err := a.st.ReraiseRequest(r.Context(), auth.CurrentUser(r), pathID(r))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// D1: the copy is already submitted, so it lands on its confirmation screen.
	http.Redirect(w, r, fmt.Sprintf("/requests/%d/submitted", id), http.StatusSeeOther)
}

func (a *App) requestComment(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	if _, err := a.st.AddRequestComment(r.Context(), auth.CurrentUser(r), req.ID, r.FormValue("body")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
}

func (a *App) requestAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	attachment, path, err := a.stageUploadedAttachment(r)
	if err != nil || attachment == nil {
		removeStagedAttachment(a.log, r, path)
		a.respondError(w, r, http.StatusBadRequest, "Choose a file to upload.", err)
		return
	}
	if _, err := a.st.AddRequestAttachment(r.Context(), auth.CurrentUser(r), pathID(r), *attachment); err != nil {
		removeStagedAttachment(a.log, r, path)
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", pathID(r)), http.StatusSeeOther)
}

func (a *App) requestApprove(w http.ResponseWriter, r *http.Request) {
	amount, err := money.ParsePaise(r.FormValue("approved_amount"))
	if err == nil {
		err = a.st.ApproveRequest(r.Context(), auth.CurrentUser(r), pathID(r), amount, r.FormValue("note"))
	}
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/approvals", http.StatusSeeOther)
}

func (a *App) requestReturn(w http.ResponseWriter, r *http.Request) {
	if err := a.st.ReturnRequest(r.Context(), auth.CurrentUser(r), pathID(r), r.FormValue("comment")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/approvals", http.StatusSeeOther)
}

func (a *App) requestReject(w http.ResponseWriter, r *http.Request) {
	if err := a.st.RejectRequest(r.Context(), auth.CurrentUser(r), pathID(r), r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/approvals", http.StatusSeeOther)
}

func (a *App) requestReassign(w http.ResponseWriter, r *http.Request) {
	if err := a.st.ReassignRequest(r.Context(), auth.CurrentUser(r), pathID(r), parseID(r.FormValue("manager_id")), r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", pathID(r)), http.StatusSeeOther)
}

func (a *App) requestsExport(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	scope := a.effectiveScope(u, r.URL.Query().Get("scope"))
	list, err := a.st.ListRequests(r.Context(), store.RequestListOptions{Scope: scope, ViewerID: u.ID,
		Status: r.URL.Query().Get("status"), Bucket: r.URL.Query().Get("bucket"), Query: r.URL.Query().Get("q")})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "export", EntityType: "payment_request", Summary: "Exported request list"})
	var body bytes.Buffer
	cw := csv.NewWriter(&body)
	_ = cw.Write([]string{"Number", "Status", "Type", "Title", "Amount", "Payee", "Requester", "Approver", "Created"})
	for _, req := range list {
		_ = cw.Write([]string{req.Number, req.Status, req.Type, req.ShortTitle,
			money.FormatPaise(req.Amount), req.Vendor, req.RequesterName, req.ManagerName,
			req.CreatedAt.Format("2006-01-02")})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The export could not be generated.", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="requests.csv"`)
	if _, err := w.Write(body.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "csv response write failed", "request_id", requestID(r), "error", err)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run 'TestRequestRoutesAreRegistered|TestRenderPartialOmits|TestNoTaxColumnsInSchema' -v`
Expected: PASS once Tasks 20–29 have supplied the templates; run this task's test again at the end of Task 29 if you implement strictly in order.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/app_integration_test.go
git commit -m "feat(app): request routes, form parsing and shell-less fragment rendering"
```

---

### Task 20: Screen — `request-new-type.html` (type is a route parameter, not a select)

**Files:**
- Modify: `internal/app/app.go` (`requestNew`), `internal/app/templates.go` (`request_new_type`)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `requestFormData` (Task 19).
- Produces: `func (a *App) requestNew(w, r)` — with no `?type=` it renders the chooser, with one it renders the form (Task 21); template `request_new_type`.

**A16, the decision this screen exists to enforce:** picking a type is a **navigation**, not a form control. `GET /requests/new` is step 1 of 2 and lists four `.type-card` links; `GET /requests/new?type=vendor_invoice` is step 2. There is no `<select name="type">` anywhere in the application, which is exactly why the Playwright selector `getByLabel('Type')` had to change (A18).

- [ ] **Step 1: Write the failing test**

```go
func TestRequestNewTypeChooser(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/requests/new", nil, ""))

	if !strings.Contains(body, `class="type-grid"`) {
		t.Fatal("the chooser does not use the .type-grid layout")
	}
	for _, want := range []string{
		`href="/requests/new?type=vendor_invoice"`,
		`href="/requests/new?type=vendor_advance"`,
		`href="/requests/new?type=reimbursement"`,
		`href="/requests/new?type=employee_advance"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("type card link %q missing", want)
		}
	}
	// A16: the type is never a form control.
	if strings.Contains(body, `name="type"`) {
		t.Fatal("the chooser rendered a type form control; the type is a route parameter")
	}
	// D1: the screen says so, because the whole flow depends on it.
	if !strings.Contains(strings.ToLower(body), "nothing is saved until you submit") {
		t.Fatal("the chooser must state that nothing is saved until submit")
	}
	// An unknown type falls back to the chooser rather than rendering a broken form.
	body = responseBody(t, s.request(http.MethodGet, "/requests/new?type=mystery", nil, ""))
	if !strings.Contains(body, `class="type-grid"`) {
		t.Fatal("an unknown type must fall back to the chooser")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestRequestNewTypeChooser -v`
Expected: FAIL — no `type-grid` in the body.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/app/app.go`:

```go
// requestTypeLabels is the vocabulary of request-new-type.html. It is the only
// place a type is named for a human.
var requestTypeLabels = map[string]string{
	"vendor_invoice":   "Vendor invoice payment",
	"vendor_advance":   "Vendor advance",
	"reimbursement":    "Reimbursement",
	"employee_advance": "Employee advance",
}

func (a *App) requestNew(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("type")
	if _, ok := requestTypeLabels[kind]; !ok {
		a.render(w, r, "request_new_type", PageData{Title: "New request"})
		return
	}
	data, err := a.requestFormData(r, requestTypeLabels[kind])
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data.FormType = kind
	data.Request2.Type = kind
	data.Request2.Treatment = "budget"
	if kind == "employee_advance" {
		data.Request2.Treatment = "recoverable"
		data.Request2.RecoverableCategory = "employee_advance"
	}
	a.render(w, r, "request_form", data)
}
```

Add to `internal/app/templates.go`:

```html
{{define "request_new_type"}}
{{template "top" .}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">New request · step 1 of 2</div>
    <h1>What are you asking to be paid?</h1>
    <p class="sub">Pick a type. The form only asks for what that type needs. Nothing is saved until you submit.</p>
  </div>
  <div class="pb-actions"><a class="btn outline" href="/dashboard">Cancel</a></div>
</section>

<div class="type-grid">
  <a class="type-card" href="/requests/new?type=vendor_invoice">
    <span class="tc-ico">▤</span>
    <b>Vendor invoice payment</b>
    <p>You have an invoice from a vendor and it needs paying.</p>
    <span class="tc-tag pill neutral no-dot">Needs invoice number and date</span>
  </a>
  <a class="type-card" href="/requests/new?type=vendor_advance">
    <span class="tc-ico">◷</span>
    <b>Vendor advance</b>
    <p>Money to a vendor before any invoice exists — a deposit against an order.</p>
    <span class="tc-tag pill neutral no-dot">Needs a reason for the advance</span>
  </a>
  <a class="type-card" href="/requests/new?type=reimbursement">
    <span class="tc-ico">↺</span>
    <b>Reimbursement</b>
    <p>You already spent your own money for the company and want it back.</p>
    <span class="tc-tag pill neutral no-dot">Paid to you · needs the expense date</span>
  </a>
  <a class="type-card" href="/requests/new?type=employee_advance">
    <span class="tc-ico">₹</span>
    <b>Employee advance</b>
    <p>Money to you up front for organisation spending you are about to make.</p>
    <span class="tc-tag pill recoverable">Usually recoverable</span>
  </a>
</div>

<div class="banner info" style="margin-top:16px">
  <span class="b-ico">?</span>
  <div>
    <b>Not sure which one?</b>
    <p>If the money leaves the company and never comes back, it is an expense. If it is a deposit, a
      guarantee, or something you will repay, pick the type that fits and mark it recoverable on the
      next screen.</p>
  </div>
</div>
{{template "bottom" .}}
{{end}}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run TestRequestNewTypeChooser -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): request type chooser screen"
```

---
### Task 21: Screen — `request-new-form.html`, the adaptive form and its htmx fields partial

**Files:**
- Modify: `internal/app/app.go` (`requestFormFields`), `internal/app/templates.go` (`request_form`, `request_form_fields`), `web/static/fervid-app.js` (combobox selection)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `requestFormData`, `renderPartial` (Task 19); Phase 1V `GET /vendors/search`; Phase 0 `.choice`, `.combo`, `.money-field`, `.uploader`, `.action-bar`, `[data-when]`, `money.InWords`.
- Produces: `func (a *App) requestFormFields(w, r)` serving `GET /requests/new/fields`; templates `request_form` and `request_form_fields`.

**A16 — how "adaptive" is built, and what it is not.**
1. **Type is fixed by the route.** The form renders the fieldsets for `?type=…` and nothing else. There is no type control to change.
2. **Treatment and recoverable category swap a fragment.** Both carry `hx-get="/requests/new/fields"` with `hx-include="closest form"` and `hx-target="#form-fields"`, so the server decides which fieldsets exist. This is why the rules can never drift between the browser and Go: there is only one copy of them.
3. **The same fieldsets carry `data-when`** so the no-JS path reveals and hides them locally (Phase 0 Task 18 owns that behaviour).
4. **`hidden` is not validation.** A hand-rolled POST that fills a hidden field is rejected by `validateRequestInput`, which is asserted below.

- [ ] **Step 1: Write the failing test**

```go
func TestRequestFormIsAdaptiveAndServerAuthoritative(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Form")
	s.login(s.cfg.AdminEmail, testAdminPassword)

	form := responseBody(t, s.request(http.MethodGet, "/requests/new?type=vendor_invoice", nil, ""))
	// T3: no bank fields, ever.
	if strings.Contains(strings.ToLower(form), "bank account") || strings.Contains(strings.ToLower(form), "ifsc") {
		t.Fatal("request form exposes bank details")
	}
	// D5: no copy-previous control.
	if strings.Contains(strings.ToLower(form), "copy previous") {
		t.Fatal("copy-previous control must not exist")
	}
	// D1: no draft control.
	if strings.Contains(strings.ToLower(form), "save draft") {
		t.Fatal("a Save draft control exists; D1 removed drafts")
	}
	// A16: the type is not a control on this page.
	if strings.Contains(form, `<select id="rtype"`) || strings.Contains(form, `name="type" class`) {
		t.Fatal("the form rendered a type selector; the type is a route parameter")
	}
	// A19: the Phase 0 components are the markup.
	for _, want := range []string{
		`class="choice"`, `class="money-field`, `class="in-words"`, `class="combo"`,
		`class="uploader"`, `class="action-bar"`, `name="short_title"`, `name="urgency_reason"`,
		`name="invoice_no"`, `name="invoice_date"`, `hx-get="/requests/new/fields"`,
		`data-when="treatment:budget"`, `data-when="urgent:on"`,
	} {
		if !strings.Contains(form, want) {
			t.Fatalf("form is missing %q", want)
		}
	}
	// G8: the requester is never in their own approver list.
	admin, _ := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if strings.Contains(form, `<option value="`+strconvFormat(admin.ID)+`"`) {
		t.Fatal("the requester appears in their own approver select")
	}

	// The fields fragment swaps on treatment and renders no shell.
	frag := responseBody(t, s.request(http.MethodGet, "/requests/new/fields?type=vendor_invoice&treatment=recoverable&recoverable_category=icd", nil, "", "HX-Request", "true"))
	if strings.Contains(frag, "<aside") {
		t.Fatal("the fields fragment carried the shell")
	}
	if !strings.Contains(frag, `name="counterparty"`) {
		t.Fatalf("ICD did not reveal the counterparty field: %s", frag)
	}
	if strings.Contains(frag, `name="head_id"`) {
		t.Fatalf("a recoverable request must not ask for a budget head: %s", frag)
	}
	frag = responseBody(t, s.request(http.MethodGet, "/requests/new/fields?type=vendor_invoice&treatment=recoverable&recoverable_category=emd", nil, "", "HX-Request", "true"))
	if !strings.Contains(frag, `name="project_id"`) {
		t.Fatal("EMD did not reveal the related-project field")
	}

	// A16.4: hidden is not validation. A hand-rolled POST that fills a field the
	// UI would have hidden is still rejected by the store's rules.
	admin, _ = s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	mgrID := seedSecondApprover(t, s)
	resp := s.postForm("/requests", url.Values{
		"type": {"vendor_invoice"}, "treatment": {"recoverable"},
		"recoverable_category": {"icd"}, "short_title": {"Sneaky"},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)},
		"amount": {"1000.00"}, "purpose": {"x"}, "manager_id": {strconvFormat(mgrID)},
		"invoice_no": {"A/1"}, "invoice_date": {"2026-07-18"}, "vendor_id": {"1"},
	})
	if resp.StatusCode == http.StatusSeeOther {
		t.Fatal("a vendor invoice was accepted with recoverable treatment; the server is not authoritative")
	}
	_ = responseBody(t, resp)
	_ = admin
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestRequestFormIsAdaptive -v`
Expected: FAIL — `request_form` is not defined.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/app/app.go`:

```go
// requestFormFields serves the treatment-dependent half of the form. The
// browser asks for it on every treatment or category change, so there is
// exactly one copy of the conditional-field rules and it lives in Go.
func (a *App) requestFormFields(w http.ResponseWriter, r *http.Request) {
	data, err := a.requestFormData(r, "")
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data.FormType = r.URL.Query().Get("type")
	data.Request2.Type = data.FormType
	data.Request2.Treatment = r.URL.Query().Get("treatment")
	if data.Request2.Treatment != "recoverable" {
		data.Request2.Treatment = "budget"
	}
	data.Request2.RecoverableCategory = r.URL.Query().Get("recoverable_category")
	data.Request2.ProjectID = optionalID(parseID(r.URL.Query().Get("project_id")))
	data.Request2.HeadID = optionalID(parseID(r.URL.Query().Get("head_id")))
	a.renderPartial(w, r, "request_form_fields", data)
}

func optionalID(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}
```

Add to `internal/app/templates.go`:

```html
{{define "request_form_fields"}}
<fieldset data-when="treatment:budget" {{if eq .Request2.Treatment "recoverable"}}hidden{{end}}>
  <legend>Charge it to</legend>
  <div class="form-grid">
    <div class="field span-6 m-half">
      <label for="project">Project <span class="req">*</span></label>
      <select id="project" name="project_id"
              hx-get="/requests/new/fields" hx-include="closest form" hx-target="#form-fields" hx-trigger="change">
        <option value="">Choose a project</option>
        {{range .Projects}}<option value="{{.ID}}" {{if eq (deref $.Request2.ProjectID) .ID}}selected{{end}}>{{.Name}}</option>{{end}}
      </select>
    </div>
    <div class="field span-6 m-half">
      <label for="head">Head <span class="req">*</span></label>
      <select id="head" name="head_id">
        <option value="">Choose a head</option>
        {{range .Heads}}{{if or (not (deref $.Request2.ProjectID)) (eq .ProjectID (deref $.Request2.ProjectID))}}<option value="{{.ID}}" {{if eq (deref $.Request2.HeadID) .ID}}selected{{end}}>{{.Project}} / {{.Name}}</option>{{end}}{{end}}
      </select>
    </div>
  </div>
</fieldset>

<fieldset data-when="treatment:recoverable" {{if ne .Request2.Treatment "recoverable"}}hidden{{end}}>
  <legend>Recoverable details</legend>
  <div class="form-grid">
    <div class="field span-6 m-half">
      <label for="rcategory">Category <span class="req">*</span></label>
      <select id="rcategory" name="recoverable_category"
              hx-get="/requests/new/fields" hx-include="closest form" hx-target="#form-fields" hx-trigger="change">
        <option value="emd" {{select .Request2.RecoverableCategory "emd"}}>EMD — earnest money deposit</option>
        <option value="pbg" {{select .Request2.RecoverableCategory "pbg"}}>PBG — performance bank guarantee</option>
        <option value="icd" {{select .Request2.RecoverableCategory "icd"}}>ICD — inter-corporate deposit</option>
        <option value="employee_advance" {{select .Request2.RecoverableCategory "employee_advance"}}>Employee advance</option>
        <option value="security_deposit" {{select .Request2.RecoverableCategory "security_deposit"}}>Security deposit</option>
        <option value="other" {{select .Request2.RecoverableCategory "other"}}>Other</option>
      </select>
      <span class="hint">Categories are maintained by your administrator.</span>
    </div>
    <div class="field span-6 m-half">
      <label for="expected-return">Expected return date <span class="req">*</span></label>
      <input id="expected-return" type="date" name="expected_return_date" value="{{.Request2.ExpectedReturnDate}}">
    </div>
    {{if or (eq .Request2.RecoverableCategory "emd") (eq .Request2.RecoverableCategory "pbg")}}
    <div class="field span-6 m-half" data-when="recoverable_category:emd|pbg">
      <label for="rproject">Related project <span class="req">*</span></label>
      <select id="rproject" name="project_id">
        <option value="">Choose a project</option>
        {{range .Projects}}<option value="{{.ID}}" {{if eq (deref $.Request2.ProjectID) .ID}}selected{{end}}>{{.Name}}</option>{{end}}
      </select>
      <span class="hint">EMD and PBG always belong to a project.</span>
    </div>
    {{end}}
    {{if or (eq .Request2.RecoverableCategory "icd") (eq .Request2.RecoverableCategory "security_deposit")}}
    <div class="field span-6 m-half" data-when="recoverable_category:icd|security_deposit">
      <label for="counterparty">Counterparty company <span class="req">*</span></label>
      <input id="counterparty" name="counterparty" value="{{.Request2.Counterparty}}" placeholder="Company receiving the deposit">
    </div>
    {{end}}
    <div class="field span-12">
      <label for="terms">Repayment or refund terms <span class="req">*</span></label>
      <textarea id="terms" name="repayment_notes">{{.Request2.RepaymentNotes}}</textarea>
    </div>
    <div class="field span-12">
      <div class="banner brand" style="margin:0">
        <span class="b-ico">↩</span>
        <div>
          <b>This will not touch budget actuals</b>
          <p>It appears in Recoverable payments instead.</p>
        </div>
      </div>
    </div>
  </div>
</fieldset>
{{end}}

{{define "request_form"}}
{{template "top" .}}
{{if .Error}}<div class="banner bad"><span class="b-ico">!</span><div><b>{{.Error}}</b></div></div>{{end}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">{{if .Request2.ID}}{{.Request2.Number}}{{else}}New request · step 2 of 2{{end}}</div>
    <h1>{{.Title}}</h1>
    <p class="sub">One form that changes with what you pick. Nothing is saved until you submit.</p>
  </div>
  <div class="pb-actions"><a class="btn outline" href="/requests/new">← Change type</a></div>
</section>

<form method="post" enctype="multipart/form-data" action="{{if .Request2.ID}}/requests/{{.Request2.ID}}/edit{{else}}/requests{{end}}">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <input type="hidden" name="type" value="{{.FormType}}">

  <fieldset>
    <legend>What is this for</legend>
    <div class="form-grid">
      <div class="field span-12">
        <label for="short-title">Short title <span class="req">*</span></label>
        <input id="short-title" name="short_title" value="{{.Request2.ShortTitle}}" required>
        <span class="hint">What your approver will see in their approval list.</span>
      </div>
      <div class="field span-12">
        <span class="flabel">How should this be treated <span class="req">*</span></span>
        <div class="choice"
             hx-get="/requests/new/fields" hx-include="closest form" hx-target="#form-fields" hx-trigger="change">
          <label>
            <input type="radio" name="treatment" value="budget" {{if ne .Request2.Treatment "recoverable"}}checked{{end}}>
            <span><b>Budget expense</b><small>Money spent and gone. Counts against a project and head.</small></span>
          </label>
          <label>
            <input type="radio" name="treatment" value="recoverable" {{if eq .Request2.Treatment "recoverable"}}checked{{end}}>
            <span><b>Refundable or recoverable</b><small>A deposit, guarantee, loan or advance you expect back. Kept out of budget actuals.</small></span>
          </label>
        </div>
      </div>
    </div>
  </fieldset>

  <div id="form-fields">{{template "request_form_fields" .}}</div>

  <fieldset>
    <legend>Amount and timing</legend>
    <div class="form-grid">
      <div class="field span-6 money-field">
        <label for="amount">Amount <span class="req">*</span></label>
        <span class="money-wrap"><span class="cur">₹</span><input id="amount" name="amount" inputmode="decimal"
          value="{{if .Request2.Amount}}{{money .Request2.Amount}}{{end}}" required
          hx-post="/requests/duplicate-check" hx-include="closest form" hx-target="#dup-check" hx-trigger="blur"></span>
        <span class="in-words">{{if .Request2.Amount}}{{inWords .Request2.Amount}}{{end}}</span>
      </div>
      <div class="field span-6 m-half">
        <label for="needed-by">Needed by <span class="req">*</span></label>
        <input id="needed-by" type="date" name="needed_by" value="{{.Request2.NeededBy}}">
      </div>
      {{if ne (index .Settings "urgency_mode") "disabled"}}
      <div class="field span-12">
        <label class="checkline"><input type="checkbox" name="urgent" {{check .Request2.Urgent}}> Mark this urgent</label>
        <span class="hint">Urgent requests follow the same approval rules. They send an immediate email to your approver, and to Accounts once approved.</span>
      </div>
      {{if eq (index .Settings "urgency_mode") "reason"}}
      <div class="field span-12" data-when="urgent:on" {{if not .Request2.Urgent}}hidden{{end}}>
        <label for="urgency-reason">Why is it urgent <span class="req">*</span></label>
        <input id="urgency-reason" name="urgency_reason" value="{{.Request2.UrgencyReason}}"
               placeholder="Supply stops if this is not cleared by Monday">
      </div>
      {{end}}
      {{end}}
    </div>
  </fieldset>

  {{if or (eq .FormType "vendor_invoice") (eq .FormType "vendor_advance")}}
  <fieldset>
    <legend>Vendor and {{if eq .FormType "vendor_invoice"}}invoice{{else}}advance{{end}}</legend>
    <div class="form-grid">
      <div class="field span-6">
        <label for="vendor">Vendor <span class="req">*</span></label>
        <span class="combo">
          <input class="combo-input" id="vendor" autocomplete="off" value="{{.Request2.Vendor}}"
                 hx-get="/vendors/search" hx-trigger="keyup changed delay:250ms" hx-target="#vendor-options">
          <span class="combo-caret">▾</span>
          <span class="combo-list" id="vendor-options"></span>
        </span>
        <input type="hidden" name="vendor_id" id="vendor-id" value="{{deref .Request2.VendorID}}">
        <noscript>
          <select name="vendor_id">
            <option value="">Choose a vendor</option>
            {{range .Vendors}}<option value="{{.ID}}" {{if eq (deref $.Request2.VendorID) .ID}}selected{{end}}>{{.Name}}</option>{{end}}
          </select>
        </noscript>
        <span class="hint">Type to search the vendor master. Bank details stay in the vendor record — never on this form.</span>
      </div>
      {{if eq .FormType "vendor_invoice"}}
      <div class="field span-3 m-half">
        <label for="invoice-no">Invoice number <span class="req">*</span></label>
        <input id="invoice-no" name="invoice_no" value="{{.Request2.InvoiceNo}}" required
               hx-post="/requests/duplicate-check" hx-include="closest form" hx-target="#dup-check" hx-trigger="blur">
      </div>
      <div class="field span-3 m-half">
        <label for="invoice-date">Invoice date <span class="req">*</span></label>
        <input id="invoice-date" type="date" name="invoice_date" value="{{.Request2.InvoiceDate}}" required>
      </div>
      {{else}}
      <div class="field span-6">
        <label for="advance-reason">Reason for the advance <span class="req">*</span></label>
        <input id="advance-reason" name="advance_reason" value="{{.Request2.AdvanceReason}}" required>
      </div>
      {{end}}
    </div>
  </fieldset>
  {{end}}

  {{if eq .FormType "reimbursement"}}
  <fieldset>
    <legend>Your expense</legend>
    <div class="form-grid">
      <div class="field span-6 m-half">
        <label for="paid-to">Paid to</label>
        <input id="paid-to" value="{{.User.Name}}" readonly>
        <span class="hint">Reimbursements always pay the person raising them.</span>
      </div>
      <div class="field span-6 m-half">
        <label for="expense-date">Expense date <span class="req">*</span></label>
        <input id="expense-date" type="date" name="expense_date" value="{{.Request2.ExpenseDate}}" required>
      </div>
    </div>
  </fieldset>
  {{end}}

  {{if eq .FormType "employee_advance"}}
  <fieldset>
    <legend>Advance details</legend>
    <div class="form-grid">
      <div class="field span-6 m-half"><label for="adv-to">Paid to</label><input id="adv-to" value="{{.User.Name}}" readonly></div>
      <div class="field span-12">
        <label for="adv-reason">What the money is for <span class="req">*</span></label>
        <input id="adv-reason" name="advance_reason" value="{{.Request2.AdvanceReason}}" required>
      </div>
    </div>
  </fieldset>
  {{end}}

  <fieldset>
    <legend>Purpose and documents</legend>
    <div class="form-grid">
      <div class="field span-12">
        <label for="purpose">Purpose <span class="req">*</span></label>
        <textarea id="purpose" name="purpose" required>{{.Request2.Purpose}}</textarea>
      </div>
      <div class="field span-12">
        <span class="flabel">Supporting document {{if eq (index .Settings "require_attachments") "1"}}<span class="req">*</span>{{else}}<span class="opt">optional</span>{{end}}</span>
        <div class="stack-8">
          <label class="uploader">
            <div class="up-ico">⇪</div>
            <b>Add invoice, receipt or proof</b>
            <small>PDF, JPG or PNG up to {{index .Settings "attachment_max_mb"}} MB</small>
            <input type="file" name="attachment">
          </label>
        </div>
      </div>
      {{if eq (index .Settings "require_attachments") "1"}}
      <div class="field span-12">
        <label for="att-exception">If you cannot attach a document, say why <span class="req">*</span></label>
        <input id="att-exception" name="attachment_exception_reason" value="{{.Request2.AttachmentExceptionReason}}"
               placeholder="Vendor posts the invoice; it arrives Monday">
        <span class="hint">A missing document never blocks you — it asks for this instead, and your approver sees it.</span>
      </div>
      {{end}}
    </div>
  </fieldset>

  <fieldset>
    <legend>Who approves it</legend>
    <div class="form-grid">
      <div class="field span-6">
        <label for="approver">Approver <span class="req">*</span></label>
        <select id="approver" name="manager_id" required {{if eq (index .Settings "allow_approver_choice") "0"}}disabled{{end}}>
          <option value="">Choose an approver</option>
          {{range .Approvers}}<option value="{{.ID}}" {{if eq $.Request2.ManagerID .ID}}selected{{end}}>{{.Name}}{{if eq $.Request2.ManagerID .ID}} (your default){{end}}</option>{{end}}
        </select>
        {{if eq (index .Settings "allow_approver_choice") "0"}}<input type="hidden" name="manager_id" value="{{.Request2.ManagerID}}">{{end}}
        <span class="hint">You cannot approve your own request. Your own name is never in this list.</span>
      </div>
      <div class="field span-6">
        <span class="flabel">Reminders</span>
        <p class="hint" style="margin:4px 0 0">If nothing happens for three calendar days, this request starts sending a daily reminder to whoever it is waiting on.</p>
      </div>
    </div>
  </fieldset>

  <div id="dup-check">{{if .Similar}}{{template "request_duplicates" .}}{{end}}</div>

  <div class="action-bar">
    <span class="ab-note d-only">Submitting sends it to your approver and creates the request number.</span>
    <span class="row-end"></span>
    <a class="btn outline" href="/dashboard">Cancel</a>
    <button class="btn primary" type="submit">{{if .Request2.ID}}Save changes{{else}}Submit request{{end}}</button>
  </div>
</form>
{{template "bottom" .}}
{{end}}
```

`requestFormData` also loads `data.Vendors` for the `<noscript>` fallback:

```go
	vendors, err := a.st.ListVendors(ctx, store.VendorListOptions{Status: "active", Limit: 500}, a.auth.Permissions(u))
	if err != nil {
		return PageData{}, err
	}
	data.Vendors = vendors
```

Add the combobox selection behaviour to `web/static/fervid-app.js` (Phase 0 owns the file; this is the one behaviour Phase 1V's fragment needs and Phase 0 did not specify):

```js
// Vendor combobox: clicking a result writes its id into the hidden input the
// form actually posts, and its name into the visible box. The hidden id is what
// the server validates — the visible text is never trusted.
document.addEventListener('click', (e) => {
  const row = e.target.closest('.combo-list .co[data-id]');
  if (!row) return;
  const combo = row.closest('.combo');
  const input = combo && combo.querySelector('.combo-input');
  const hidden = document.getElementById('vendor-id');
  if (input) input.value = row.dataset.name || '';
  if (hidden) hidden.value = row.dataset.id;
  const list = combo && combo.querySelector('.combo-list');
  if (list) list.innerHTML = '';
});
```

> Phase 1V Task 7's `.co` rows must therefore carry `data-id` and `data-name`. That is an additive change to a fragment that phase already owns; note it when implementing 1V.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run TestRequestFormIsAdaptive -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go web/static/fervid-app.js
git commit -m "feat(app): adaptive request form with htmx fields partial and vendor combobox"
```

---

### Task 22: `POST /requests` and the `request-submitted.html` screen

**Files:**
- Modify: `internal/app/app.go` (`requestCreate`, `requestSubmitted`, `requestEditForm`), `internal/app/templates.go` (`request_submitted`)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `store.CreateRequest` (Task 5), `stageUploadedAttachment`, `requestFormData`.
- Produces: `func (a *App) requestCreate(w, r)` — one POST that stages the file, creates and submits, and redirects to the confirmation; `func (a *App) requestSubmitted(w, r)`; `func (a *App) requestEditForm(w, r)`; template `request_submitted`.

**D1 in the handler:** there is exactly one submit button and exactly one POST. The staged upload is handed to `CreateRequest` as `in.Attachments`, so the file and the row commit together — if validation fails, the staged file is removed and nothing exists. The old `submit_action=save|submit` pair is gone.

- [ ] **Step 1: Write the failing test**

```go
func TestRequesterCreatesAndSubmitsInOnePost(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Req")
	hash, _ := auth.HashPassword("RequesterPass123")
	rid, err := s.st.CreateUser(s.ctx, "rhea2@example.test", "Rhea Two", hash, "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	s.assignRole(rid, "Requester")
	mgrID := seedSecondApprover(t, s)
	s.login("rhea2@example.test", "RequesterPass123")

	resp := s.postForm("/requests", url.Values{
		"type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Hyderabad site visit"},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)}, "amount": {"1,000.00"},
		"purpose": {"flight and hotel"}, "expense_date": {"2026-07-17"},
		"manager_id": {strconvFormat(mgrID)},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	loc := resp.Header.Get("Location")
	if !strings.HasSuffix(loc, "/submitted") {
		t.Fatalf("redirect = %q, want the submitted confirmation", loc)
	}
	_ = responseBody(t, resp)

	list, err := s.st.ListRequests(s.ctx, store.RequestListOptions{Scope: "all"})
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %#v, %v", list, err)
	}
	// D1: created, numbered and pending in one POST.
	if list[0].Status != "pending" || list[0].Number == "" || list[0].SubmittedAt == nil {
		t.Fatalf("request after one POST = %+v", list[0])
	}
	// The comma-grouped amount the money field writes back parses correctly.
	if list[0].Amount != 100000 {
		t.Fatalf("amount = %d, want 100000 paise", list[0].Amount)
	}

	body := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(list[0].ID)+"/submitted", nil, ""))
	for _, want := range []string{`class="banner good"`, `class="req-head"`, `class="thread"`,
		`class="pill awaiting"`, `class="waiting"`, list[0].Number} {
		if !strings.Contains(body, want) {
			t.Fatalf("confirmation screen missing %q", want)
		}
	}
	if !strings.Contains(body, `href="/requests/new"`) {
		t.Fatal("confirmation screen has no 'raise another' path")
	}

	// A failed submit re-renders the form with the message and keeps nothing.
	bad := s.postForm("/requests", url.Values{
		"type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {""},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)}, "amount": {"1000.00"},
		"purpose": {"x"}, "expense_date": {"2026-07-17"}, "manager_id": {strconvFormat(mgrID)},
	})
	if bad.StatusCode == http.StatusSeeOther {
		t.Fatal("a request with no short title was accepted")
	}
	badBody := responseBody(t, bad)
	if !strings.Contains(badBody, "short title") {
		t.Fatalf("the error was not shown on the form: %s", badBody)
	}
	again, _ := s.st.ListRequests(s.ctx, store.RequestListOptions{Scope: "all"})
	if len(again) != 1 {
		t.Fatalf("a failed submit created %d extra rows", len(again)-1)
	}
}

// seedSecondApprover returns a user id that holds approval:approve and is not
// the logged-in requester, so G8 never gets in the fixture's way.
func seedSecondApprover(t *testing.T, s *appTestServer) int64 {
	t.Helper()
	hash, _ := auth.HashPassword("ApproverPass123")
	id, err := s.st.CreateUser(s.ctx, "kavita@example.test", "Kavita Rao", hash, "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	s.assignRole(id, "Manager")
	return id
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestRequesterCreatesAndSubmitsInOnePost -v`
Expected: FAIL — `POST /requests` does not redirect to `/submitted`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/app/app.go`:

```go
// requestCreate is the whole of D1 in one handler: stage the file, create the
// request already pending, redirect to the confirmation. There is no draft to
// save and no second submit step.
func (a *App) requestCreate(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	in, err := requestInput(r)
	var stagedPath string
	if err == nil {
		var attachment *store.AttachmentInput
		attachment, stagedPath, err = a.stageUploadedAttachment(r)
		if err == nil && attachment != nil {
			in.Attachments = []store.AttachmentInput{*attachment}
		}
	}
	var id int64
	if err == nil {
		id, err = a.st.CreateRequest(r.Context(), u, in)
	}
	if err != nil {
		// Nothing was written, so nothing must be left on disk either.
		removeStagedAttachment(a.log, r, stagedPath)
		status := storeErrorStatus(err)
		if status >= http.StatusInternalServerError {
			a.respondStoreError(w, r, err)
			return
		}
		data, dErr := a.requestFormData(r, requestTypeLabels[in.Type])
		if dErr != nil {
			a.respondStoreError(w, r, dErr)
			return
		}
		data.Error = friendly(err)
		data.FormType = in.Type
		data.Request2 = requestFromInput(in)
		a.renderStatus(w, r, status, "request_form", data)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d/submitted", id), http.StatusSeeOther)
}

// requestFromInput re-renders a rejected form without losing what was typed.
func requestFromInput(in store.RequestInput) store.Request {
	req := store.Request{
		Treatment: in.Treatment, Type: in.Type, RecoverableCategory: in.RecoverableCategory,
		VendorPayee: in.VendorPayee, ShortTitle: in.ShortTitle, Amount: in.Amount,
		Purpose: in.Purpose, NeededBy: in.NeededBy, InvoiceNo: in.InvoiceNo,
		InvoiceDate: in.InvoiceDate, ExpenseDate: in.ExpenseDate, AdvanceReason: in.AdvanceReason,
		Counterparty: in.Counterparty, ExpectedReturnDate: in.ExpectedReturnDate,
		RepaymentNotes: in.RepaymentNotes, Urgent: in.Urgent, UrgencyReason: in.UrgencyReason,
		AttachmentExceptionReason: in.AttachmentExceptionReason, ManagerID: in.ManagerID,
	}
	req.ProjectID = optionalID(in.ProjectID)
	req.HeadID = optionalID(in.HeadID)
	req.VendorID = optionalID(in.VendorID)
	return req
}

func (a *App) requestSubmitted(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	a.render(w, r, "request_submitted", PageData{Title: "Request submitted", Request2: req})
}

func (a *App) requestEditForm(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	data, err := a.requestFormData(r, "Edit request")
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data.Request2 = req
	data.FormType = req.Type
	a.render(w, r, "request_edit", data)
}
```

Add to `internal/app/templates.go`:

```html
{{define "request_submitted"}}
{{template "top" .}}
<div class="banner good">
  <span class="b-ico">✓</span>
  <div>
    <b>Submitted. {{.Request2.ManagerName}} has been notified.</b>
    <p>You can still edit this request until they act on it. Every edit tells them again and restarts
      the three-day reminder clock.</p>
  </div>
</div>

<div class="req-head">
  <div class="rh-top">
    <span class="rh-no">{{.Request2.Number}}</span>
    <span class="rh-amt">{{money .Request2.Amount}}</span>
  </div>
  <h1>{{.Request2.ShortTitle}}</h1>
  <p class="rh-meta">{{typeLabel .Request2.Type}}{{if .Request2.Project}} · {{.Request2.Project}}{{end}}{{if .Request2.Head}} · {{.Request2.Head}}{{end}}{{if .Request2.NeededBy}} · needed by {{dateLong .Request2.NeededBy}}{{end}}</p>
  <div class="rh-status">
    <span class="pill {{pillClass .Request2.Status}}">{{statusText .Request2.Status}}</span>
    <span class="waiting">Waiting on {{.Request2.ManagerName}}</span>
  </div>
</div>

<div class="section-head"><h2>What happens next</h2></div>
<ol class="thread">
  <li>
    <span class="tl-dot brand">1</span>
    <div class="tl-head"><b>{{.Request2.ManagerName}} reviews it</b></div>
    <div class="tl-body">They can approve, return it to you for a correction, or reject it. A reminder
      goes out daily if nothing happens after three calendar days.</div>
  </li>
  <li>
    <span class="tl-dot">2</span>
    <div class="tl-head"><b>Accounts picks it up</b></div>
    <div class="tl-body">Once approved, an accountant reserves the request and records the payment
      against it. Nobody else can process it while it is reserved.</div>
  </li>
  <li>
    <span class="tl-dot">3</span>
    <div class="tl-head"><b>It settles</b></div>
    <div class="tl-body">Accounts confirms the payment covers the obligation and the request closes.
      You are notified at every step.</div>
  </li>
</ol>

<div class="action-bar">
  <span class="row-end"></span>
  <a class="btn outline" href="/requests/new">Raise another</a>
  <a class="btn" href="/requests">My requests</a>
  <a class="btn primary" href="/requests/{{.Request2.ID}}">View this request</a>
</div>
{{template "bottom" .}}
{{end}}
```

Add a `typeLabel` template func alongside `pillClass`:

```go
		"typeLabel": func(t string) string {
			if label, ok := requestTypeLabels[t]; ok {
				return label
			}
			return t
		},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run TestRequesterCreatesAndSubmitsInOnePost -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): one-post create-and-submit with the submitted confirmation screen"
```

---

### Task 23: Duplicate-check endpoint — warns, never blocks (G6)

**Files:**
- Modify: `internal/app/app.go` (`requestDuplicateCheck`), `internal/app/templates.go` (`request_duplicates`)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `store.SimilarRequests` (Task 18), `renderPartial` (Task 19).
- Produces: `func (a *App) requestDuplicateCheck(w, r)` serving `POST /requests/duplicate-check`; template `request_duplicates` — a `.banner.warn` listing matches, with `.req-card` rows.

**The invariant this task exists to protect:** the endpoint is a **read**. It renders a warning into `#dup-check` and returns 200 whether or not anything matched. `POST /requests` never calls it, never consults it, and cannot be made to fail because of it. `request-duplicate-warning.html` states the rule outright: "A duplicate warning never blocks submission. Legitimate repeat payments exist — the same rent, the same monthly retainer. The system points, the person decides."

- [ ] **Step 1: Write the failing test**

```go
func TestDuplicateCheckWarnsAndNeverBlocks(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Dup")
	mgrID := seedSecondApprover(t, s)
	vendorID := seedAppVendor(t, s, "Sundaram Electricals Pvt Ltd")
	hash, _ := auth.HashPassword("RequesterPass123")
	rid, _ := s.st.CreateUser(s.ctx, "dup@example.test", "Dup Requester", hash, "data_entry", true)
	s.assignRole(rid, "Requester")
	requester, _ := s.st.UserByID(s.ctx, rid)

	existing, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "vendor_invoice", ShortTitle: "June switchgear", ProjectID: 1, HeadID: headID,
		Amount: 10000000, Purpose: "panels", ManagerID: mgrID, VendorID: vendorID,
		InvoiceNo: "SE/26-27/1102", InvoiceDate: "2026-06-28"})
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := s.st.Request(s.ctx, existing)

	s.login("dup@example.test", "RequesterPass123")
	form := url.Values{"type": {"vendor_invoice"}, "vendor_id": {strconvFormat(vendorID)},
		"amount": {"1,00,000.00"}, "invoice_no": {"SE/26-27/1184"}}
	body := responseBody(t, s.postFormHX("/requests/duplicate-check", form))
	if !strings.Contains(body, `class="banner warn"`) {
		t.Fatalf("duplicate warning is not a .banner.warn: %s", body)
	}
	if !strings.Contains(body, orig.Number) {
		t.Fatalf("the existing request is not listed: %s", body)
	}
	if strings.Contains(body, "<aside") {
		t.Fatal("the duplicate fragment carried the shell")
	}
	// The wording must not promise a block.
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "you can still") && !strings.Contains(lower, "check before you submit") {
		t.Fatalf("the warning does not say the submit may still proceed: %s", body)
	}

	// Nothing similar: 200 and an empty fragment, never a 4xx.
	empty := s.postFormHX("/requests/duplicate-check", url.Values{"type": {"vendor_invoice"},
		"vendor_id": {strconvFormat(vendorID)}, "amount": {"3.00"}})
	requireStatus(t, empty, http.StatusOK)
	if got := strings.TrimSpace(responseBody(t, empty)); got != "" {
		t.Fatalf("no-match fragment = %q, want empty", got)
	}

	// G6: the submit goes through anyway, and creates a second request.
	resp := s.postForm("/requests", url.Values{
		"type": {"vendor_invoice"}, "treatment": {"budget"}, "short_title": {"July switchgear"},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)}, "amount": {"1,00,000.00"},
		"purpose": {"panels"}, "vendor_id": {strconvFormat(vendorID)},
		"invoice_no": {"SE/26-27/1184"}, "invoice_date": {"2026-07-18"},
		"manager_id": {strconvFormat(mgrID)},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	all, _ := s.st.ListRequests(s.ctx, store.RequestListOptions{Scope: "all"})
	if len(all) != 2 {
		t.Fatalf("the duplicate warning blocked the submit: %d requests exist, want 2", len(all))
	}
}

// seedAppVendor inserts a Phase-1V vendor for HTTP-level fixtures.
func seedAppVendor(t *testing.T, s *appTestServer, name string) int64 {
	t.Helper()
	res, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO vendors(name,vendor_type,status) VALUES(?,'company','active')`, name)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
```

> `s.postFormHX` is `postForm` with `HX-Request: true` set — add it beside `postForm` in the test helpers.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestDuplicateCheckWarns -v`
Expected: FAIL — 404 on `/requests/duplicate-check`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/app/app.go`:

```go
// requestDuplicateCheck renders the advisory duplicate warning. It is a read:
// it renders 200 whether or not anything matched, and POST /requests neither
// calls it nor consults its result. The system points; the person decides (G6).
func (a *App) requestDuplicateCheck(w http.ResponseWriter, r *http.Request) {
	amount, _ := money.ParsePaise(r.FormValue("amount"))
	similar, err := a.st.SimilarRequests(r.Context(), store.SimilarRequestOptions{
		ExcludeID: parseID(r.FormValue("request_id")),
		VendorID:  parseID(r.FormValue("vendor_id")),
		Payee:     r.FormValue("vendor_payee"),
		Amount:    amount,
		InvoiceNo: r.FormValue("invoice_no"),
	})
	if err != nil {
		// Even a failed check must not stand between a person and their submit.
		a.log.ErrorContext(r.Context(), "duplicate check failed", "request_id", requestID(r), "error", err)
		return
	}
	if len(similar) == 0 {
		return
	}
	a.renderPartial(w, r, "request_duplicates", PageData{Similar: similar})
}
```

Add to `internal/app/templates.go`:

```html
{{define "request_duplicates"}}
<div class="banner warn">
  <span class="b-ico">⚠</span>
  <div>
    <b>{{if eq (len .Similar) 1}}A similar request already exists{{else}}{{len .Similar}} similar requests already exist{{end}}</b>
    <p>Same payee and a close amount in the last 30 days. Check before you submit — you can still go ahead.</p>
    <div class="req-list" style="margin-top:8px">
      {{range .Similar}}
      <a class="req-card" href="/requests/{{.ID}}">
        <span class="rc-top"><span class="rc-no">{{.Number}}</span><span class="rc-amt">{{money .Amount}}</span></span>
        <span class="rc-title">{{.ShortTitle}}</span>
        <span class="rc-meta">{{.Vendor}}{{if .InvoiceNo}} · invoice {{.InvoiceNo}}{{end}} · raised {{date .CreatedAt}}</span>
        <span class="rc-foot">
          <span class="pill {{pillClass .Status}}">{{statusText .Status}}</span>
          <span class="waiting">{{if eq .Status "approved"}}Waiting on Accounts{{else}}Waiting on {{.ManagerName}}{{end}}</span>
        </span>
      </a>
      {{end}}
    </div>
  </div>
</div>
{{end}}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run TestDuplicateCheckWarns -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): advisory duplicate-check fragment that never blocks a submit"
```

---
### Task 24: Screen — `requests-list.html` (`.segmented` tabs, `.req-card`, mobile filter sheet)

**Files:**
- Modify: `internal/app/app.go` (`requests`, `waitingOn`), `internal/app/templates.go` (`requests`, `request_card`, `request_filter_sheet`)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `store.ListRequests`/`CountRequests` with `Bucket` (Task 17), `effectiveScope` (Task 19).
- Produces: `func (a *App) requests(w, r)`; `func waitingOn(req store.Request, viewerID int64) Waiting`; templates `requests`, `request_card` (shared with `approvals`), `request_filter_sheet`.

**A19 — the "waiting on" line is the signature element of this design.** Every card ends with a plain sentence naming who owes the next action, and it says "you" in brand colour when that is the viewer. It is computed once, in `waitingOn`, and reused by the list, the approvals queue and the detail head, so the three can never disagree.

- [ ] **Step 1: Write the failing test**

```go
func TestRequestsListRendersCardsTabsAndWaitingLine(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("List")
	mgrID := seedSecondApprover(t, s)
	mgr, _ := s.st.UserByID(s.ctx, mgrID)
	hash, _ := auth.HashPassword("RequesterPass123")
	rid, _ := s.st.CreateUser(s.ctx, "lister@example.test", "Lister", hash, "data_entry", true)
	s.assignRole(rid, "Requester")
	requester, _ := s.st.UserByID(s.ctx, rid)
	mk := func(title string) int64 {
		id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
			Type: "reimbursement", ShortTitle: title, ProjectID: 1, HeadID: headID, Amount: 1000,
			Purpose: "p", ExpenseDate: "2026-07-17", ManagerID: mgrID})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	open := mk("Still waiting")
	returned := mk("Sent back to me")
	if err := s.st.ReturnRequest(s.ctx, mgr, returned, "attach the receipt"); err != nil {
		t.Fatal(err)
	}

	s.login("lister@example.test", "RequesterPass123")
	body := responseBody(t, s.request(http.MethodGet, "/requests", nil, ""))
	for _, want := range []string{
		`class="req-list"`, `class="req-card`, `class="rc-no"`, `class="rc-amt"`,
		`class="rc-title"`, `class="rc-meta"`, `class="rc-foot"`,
		`class="segmented"`, `class="m-filters"`, `id="filter-sheet"`, `class="overlay"`,
		"Still waiting", "Sent back to me",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("requests list is missing %q", want)
		}
	}
	// D5: the design removed .badge entirely.
	if strings.Contains(body, `class="badge`) {
		t.Fatal("the list still renders .badge; the design system uses .pill")
	}
	// The returned request says it is waiting on the viewer.
	if !strings.Contains(body, `class="waiting you"`) {
		t.Fatal("no 'waiting on you' line for the returned request")
	}
	// The tabs carry counts that come from the same SQL as the rows.
	if !strings.Contains(body, `href="/requests?bucket=needs-me"`) {
		t.Fatal("the Needs me tab is missing")
	}
	needsMe := responseBody(t, s.request(http.MethodGet, "/requests?bucket=needs-me", nil, ""))
	if !strings.Contains(needsMe, "Sent back to me") || strings.Contains(needsMe, "Still waiting") {
		t.Fatalf("the Needs me bucket listed the wrong requests: %s", needsMe)
	}
	_ = open

	// Q5: the list is scoped. Another requester sees none of it.
	hash2, _ := auth.HashPassword("OtherPass1234")
	oid, _ := s.st.CreateUser(s.ctx, "notlister@example.test", "Not Lister", hash2, "data_entry", true)
	s.assignRole(oid, "Requester")
	s.login("notlister@example.test", "OtherPass1234")
	other := responseBody(t, s.request(http.MethodGet, "/requests", nil, ""))
	if strings.Contains(other, "Still waiting") {
		t.Fatal("a requester can see another person's requests in the list")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestRequestsListRendersCards -v`
Expected: FAIL — `requests` template undefined.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/app/app.go`:

```go
// Waiting is the "waiting on" line: one sentence naming who owes the next
// action. Class is the Phase 0 modifier — "you", "done", "closed" or "".
type Waiting struct {
	Text  string
	Class string
}

func waitingOn(req store.Request, viewerID int64) Waiting {
	switch req.Status {
	case "pending":
		if req.ManagerID == viewerID {
			return Waiting{Text: "Waiting on you", Class: "you"}
		}
		return Waiting{Text: "Waiting on " + req.ManagerName}
	case "returned":
		if req.RequesterID == viewerID {
			return Waiting{Text: "Waiting on you", Class: "you"}
		}
		return Waiting{Text: "Waiting on " + req.RequesterName}
	case "approved":
		return Waiting{Text: "Waiting on Accounts"}
	case "cancellation_requested":
		if req.ManagerID == viewerID {
			return Waiting{Text: "Waiting on you", Class: "you"}
		}
		return Waiting{Text: "Waiting on " + req.ManagerName}
	case "rejected":
		return Waiting{Text: "Closed. Raise a new request if needed", Class: "closed"}
	case "withdrawn":
		return Waiting{Text: "Withdrawn by the requester", Class: "closed"}
	case "cancelled":
		return Waiting{Text: "Cancelled. Nothing can be paid against it", Class: "closed"}
	default:
		return Waiting{}
	}
}

var requestTabs = []struct{ Key, Label string }{
	{"open", "Open"}, {"needs-me", "Needs me"}, {"closed", "Closed"}, {"all", "All"},
}

func (a *App) requests(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	q := r.URL.Query()
	scope := a.effectiveScope(u, q.Get("scope"))
	bucket := q.Get("bucket")
	if bucket == "" {
		bucket = "open"
	}
	opts := store.RequestListOptions{Scope: scope, ViewerID: u.ID, Bucket: bucket,
		Type: q.Get("type"), Treatment: q.Get("treatment"), ProjectID: parseID(q.Get("project_id")),
		Query: q.Get("q")}
	list, err := a.st.ListRequests(r.Context(), opts)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	counts := map[string]int{}
	for _, tab := range requestTabs {
		c := opts
		c.Bucket = tab.Key
		n, err := a.st.CountRequests(r.Context(), c)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		counts[tab.Key] = n
	}
	projects, err := a.st.ListProjects(r.Context(), true)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "requests", PageData{Title: "Requests", Requests: list, Scope: scope,
		Bucket: bucket, TypeFilter: q.Get("type"), Treatment: q.Get("treatment"),
		Query: q.Get("q"), Counts: counts, Projects: projects})
}
```

Register `waitingOn` and the tab list in the `FuncMap`:

```go
		"waitingOn":   waitingOn,
		"requestTabs": func() any { return requestTabs },
```

Add to `internal/app/templates.go`:

```html
{{define "request_card"}}
{{$r := .Req}}
<a class="req-card{{if $r.Urgent}} is-urgent{{end}}{{if eq $r.RequesterID .ViewerID}} is-mine{{end}}" href="/requests/{{$r.ID}}">
  <span class="rc-top"><span class="rc-no">{{$r.Number}}</span><span class="rc-amt">{{money $r.Amount}}</span></span>
  <span class="rc-title">{{$r.ShortTitle}}</span>
  <span class="rc-meta">
    {{if $r.Urgent}}<span class="pill urgent">Urgent</span> {{end}}
    {{if eq $r.Treatment "recoverable"}}<span class="pill recoverable">Recoverable{{if $r.RecoverableCategory}} · {{$r.RecoverableCategory}}{{end}}</span> {{end}}
    {{typeLabel $r.Type}}{{if $r.Project}} · {{$r.Project}} / {{$r.Head}}{{end}}{{if $r.InvoiceNo}} · invoice {{$r.InvoiceNo}}{{end}}
  </span>
  <span class="rc-foot">
    <span class="pill {{pillClass $r.Status}}">{{statusText $r.Status}}</span>
    {{$w := waitingOn $r .ViewerID}}<span class="waiting {{$w.Class}}">{{$w.Text}}</span>
  </span>
</a>
{{end}}

{{define "request_filter_sheet"}}
<div class="overlay" id="filter-sheet" hidden>
  <form class="sheet" method="get" action="/requests">
    <div class="sh-head">
      <div><h2>Filters</h2><p class="sh-sub">Narrow the list</p></div>
      <button class="sh-close" type="button" data-close="filter-sheet" aria-label="Close">✕</button>
    </div>
    <div class="sh-body stack-12">
      <input type="hidden" name="bucket" value="{{.Bucket}}">
      <div class="field"><label for="fs-ty">Type</label><select id="fs-ty" name="type">
        <option value="">Any type</option>
        <option value="vendor_invoice" {{select .TypeFilter "vendor_invoice"}}>Vendor invoice</option>
        <option value="vendor_advance" {{select .TypeFilter "vendor_advance"}}>Vendor advance</option>
        <option value="reimbursement" {{select .TypeFilter "reimbursement"}}>Reimbursement</option>
        <option value="employee_advance" {{select .TypeFilter "employee_advance"}}>Employee advance</option>
      </select></div>
      <div class="field"><label for="fs-tr">Treatment</label><select id="fs-tr" name="treatment">
        <option value="">Any</option>
        <option value="budget" {{select .Treatment "budget"}}>Budget expense</option>
        <option value="recoverable" {{select .Treatment "recoverable"}}>Recoverable</option>
      </select></div>
      <div class="field"><label for="fs-pr">Project</label><select id="fs-pr" name="project_id">
        <option value="">All projects</option>
        {{range .Projects}}<option value="{{.ID}}">{{.Name}}</option>{{end}}
      </select></div>
      <div class="field"><label for="fs-q">Search</label><input id="fs-q" name="q" value="{{.Query}}"></div>
    </div>
    <div class="sh-foot">
      <a class="btn outline" href="/requests">Clear all</a>
      <span class="row-end"></span>
      <button class="btn primary" type="submit">Show requests</button>
    </div>
  </form>
</div>
{{end}}

{{define "requests"}}
{{template "top" .}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">Requests</div>
    <h1>{{if eq .Scope "own"}}My requests{{else}}All requests{{end}}</h1>
    <p class="sub">{{len .Requests}} shown · sorted by who is holding them up</p>
  </div>
  <div class="pb-actions">
    <a class="btn outline" href="/requests/export.csv?scope={{.Scope}}&bucket={{.Bucket}}&q={{.Query}}">⤓ Export CSV</a>
    <a class="btn primary" href="/requests/new">＋ New request</a>
  </div>
</section>

<form class="toolbar d-only" method="get" action="/requests">
  <input type="hidden" name="bucket" value="{{.Bucket}}">
  <div class="field search"><label for="q">Search</label><input id="q" name="q" value="{{.Query}}" placeholder="Number, payee, invoice, purpose…"></div>
  <div class="field"><label for="ty">Type</label><select id="ty" name="type">
    <option value="">Any type</option>
    <option value="vendor_invoice" {{select .TypeFilter "vendor_invoice"}}>Vendor invoice</option>
    <option value="vendor_advance" {{select .TypeFilter "vendor_advance"}}>Vendor advance</option>
    <option value="reimbursement" {{select .TypeFilter "reimbursement"}}>Reimbursement</option>
    <option value="employee_advance" {{select .TypeFilter "employee_advance"}}>Employee advance</option>
  </select></div>
  <div class="field"><label for="tr">Treatment</label><select id="tr" name="treatment">
    <option value="">Any</option>
    <option value="budget" {{select .Treatment "budget"}}>Budget expense</option>
    <option value="recoverable" {{select .Treatment "recoverable"}}>Recoverable</option>
  </select></div>
  <span class="row-end"></span>
  <button class="btn">Apply</button>
</form>

<div class="m-filters">
  <span class="m-search"><form method="get" action="/requests"><input type="hidden" name="bucket" value="{{.Bucket}}"><input name="q" value="{{.Query}}" placeholder="Search requests…" aria-label="Search requests"></form></span>
  <button class="btn filter-btn" type="button" data-open="filter-sheet">Filters</button>
</div>

<div class="segmented" role="tablist" style="margin-bottom:12px">
  {{range requestTabs}}<a class="{{if eq $.Bucket .Key}}is-active{{end}}" role="tab"
    {{if eq $.Bucket .Key}}aria-selected="true"{{end}}
    href="/requests?bucket={{.Key}}&q={{$.Query}}">{{.Label}} <span class="n">{{index $.Counts .Key}}</span></a>{{end}}
</div>

<div class="req-list">
  {{range .Requests}}{{template "request_card" (card . $.User.ID)}}{{else}}<p class="empty">No requests match.</p>{{end}}
</div>

{{template "request_filter_sheet" .}}
{{template "bottom" .}}
{{end}}
```

> **Why the `card` wrapper exists.** `{{template "request_card" .}}` inside a `{{range}}` rebinds `.` to the request, so `$.User.ID` inside the card would resolve against the *card's* dot, not the page, and `html/template` would not report it — it would silently render the wrong "waiting on" line. `card` bundles both:
>
> ```go
> 		"card": func(r store.Request, viewerID int64) any {
> 			return struct {
> 				Req      store.Request
> 				ViewerID int64
> 			}{r, viewerID}
> 		},
> ```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run TestRequestsListRendersCards -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): requests list with segmented tabs, request cards and filter sheet"
```

---

### Task 25: Screen — `approvals-list.html` on its own route `GET /approvals`

**Files:**
- Modify: `internal/app/app.go` (`approvals`), `internal/app/templates.go` (`approvals`)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `store.ListRequests` with `Scope: "assigned"`, `request_card` (Task 24).
- Produces: `func (a *App) approvals(w, r)`; template `approvals`.

**Why its own route (A15):** the manager queue is a different screen with different tabs — To approve · Cancellations · Decided — and a different empty state. Folding it into `/requests?scope=assigned` made the segmented tabs mean two things at once. `.tabbar`'s centre action for `approval:approve` holders already points at `/approvals` (Phase 0 Task 14), so this route was already promised.

- [ ] **Step 1: Write the failing test**

```go
func TestApprovalsQueueIsItsOwnScreen(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Appr")
	mgrID := seedSecondApprover(t, s)
	mgr, _ := s.st.UserByID(s.ctx, mgrID)
	hash, _ := auth.HashPassword("RequesterPass123")
	rid, _ := s.st.CreateUser(s.ctx, "appreq@example.test", "Appr Requester", hash, "data_entry", true)
	s.assignRole(rid, "Requester")
	requester, _ := s.st.UserByID(s.ctx, rid)
	mk := func(title string) int64 {
		id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
			Type: "reimbursement", ShortTitle: title, ProjectID: 1, HeadID: headID, Amount: 18400,
			Purpose: "p", ExpenseDate: "2026-07-17", ManagerID: mgrID})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	waiting := mk("Travel reimbursement")
	frozen := mk("Binding wire advance")
	if err := s.st.ApproveRequest(s.ctx, mgr, frozen, 18400, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.st.RequestCancellation(s.ctx, requester, frozen, "order withdrawn"); err != nil {
		t.Fatal(err)
	}

	s.login("kavita@example.test", "ApproverPass123")
	body := responseBody(t, s.request(http.MethodGet, "/approvals", nil, ""))
	for _, want := range []string{`class="segmented"`, `class="req-list"`, `class="req-card`,
		"Travel reimbursement", `class="waiting you"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("approvals queue is missing %q", want)
		}
	}
	// A6: no bulk approval anywhere on the screen.
	if strings.Contains(body, `type="checkbox"`) || strings.Contains(strings.ToLower(body), "approve selected") {
		t.Fatal("the approvals queue offers bulk approval")
	}
	// The cancellation tab is a first-class part of the queue (G1).
	if !strings.Contains(body, `href="/approvals?bucket=cancellations"`) {
		t.Fatal("no cancellations tab")
	}
	cancel := responseBody(t, s.request(http.MethodGet, "/approvals?bucket=cancellations", nil, ""))
	if !strings.Contains(cancel, "Binding wire advance") || strings.Contains(cancel, "Travel reimbursement") {
		t.Fatalf("the cancellations tab listed the wrong requests: %s", cancel)
	}
	_ = waiting

	// A requester without approval:approve cannot reach the queue at all.
	s.login("appreq@example.test", "RequesterPass123")
	requireStatus(t, s.request(http.MethodGet, "/approvals", nil, ""), http.StatusForbidden)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestApprovalsQueueIsItsOwnScreen -v`
Expected: FAIL — 404 on `/approvals`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/app/app.go`:

```go
var approvalTabs = []struct {
	Key, Label string
	Statuses   []string
}{
	{"to-approve", "To approve", []string{"pending"}},
	{"cancellations", "Cancellations", []string{"cancellation_requested"}},
	{"decided", "Decided", []string{"approved", "rejected", "cancelled"}},
}

func (a *App) approvals(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		bucket = "to-approve"
	}
	counts := map[string]int{}
	var list []store.Request
	for _, tab := range approvalTabs {
		opts := store.RequestListOptions{Scope: "assigned", ViewerID: u.ID, Statuses: tab.Statuses,
			Query: r.URL.Query().Get("q")}
		n, err := a.st.CountRequests(r.Context(), opts)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		counts[tab.Key] = n
		if tab.Key == bucket {
			list, err = a.st.ListRequests(r.Context(), opts)
			if err != nil {
				a.respondStoreError(w, r, err)
				return
			}
		}
	}
	a.render(w, r, "approvals", PageData{Title: "Approvals", Requests: list, Bucket: bucket,
		Counts: counts, Query: r.URL.Query().Get("q")})
}
```

Register `"approvalTabs": func() any { return approvalTabs }` in the `FuncMap`.

Add to `internal/app/templates.go`:

```html
{{define "approvals"}}
{{template "top" .}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">Manager queue</div>
    <h1>Waiting on you</h1>
    <p class="sub">{{index .Counts "to-approve"}} to approve · {{index .Counts "cancellations"}} cancellation{{if ne (index .Counts "cancellations") 1}}s{{end}} to decide</p>
  </div>
  <div class="pb-actions"><a class="btn outline" href="/requests/export.csv?scope=assigned">⤓ Export CSV</a></div>
</section>

<div class="segmented" role="tablist" style="margin-bottom:12px">
  {{range approvalTabs}}<a class="{{if eq $.Bucket .Key}}is-active{{end}}" role="tab"
    {{if eq $.Bucket .Key}}aria-selected="true"{{end}}
    href="/approvals?bucket={{.Key}}">{{.Label}} <span class="n">{{index $.Counts .Key}}</span></a>{{end}}
</div>

<div class="m-filters">
  <span class="m-search"><form method="get" action="/approvals"><input type="hidden" name="bucket" value="{{.Bucket}}"><input name="q" value="{{.Query}}" placeholder="Search approvals…" aria-label="Search approvals"></form></span>
</div>

<div class="req-list">
  {{range .Requests}}{{template "request_card" (card . $.User.ID)}}{{else}}<p class="empty">Nothing is waiting on you.</p>{{end}}
</div>

<p class="hint">Every approval is a decision made after opening the request. There is no bulk approval,
  by design.</p>
{{template "bottom" .}}
{{end}}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run TestApprovalsQueueIsItsOwnScreen -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): manager approvals queue on its own route"
```

---

### Task 26: Screen — `request-detail-employee.html` / `request-detail-manager.html`

**Files:**
- Modify: `internal/app/app.go` (`requestDetail`), `internal/app/templates.go` (`request_detail`, `request_thread`, `request_sheets`)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `store.Request`, `RequestThread` (Task 16), `RequestAttachments`, `waitingOn` (Task 24), `.Perms` (Phase 0).
- Produces: `func (a *App) requestDetail(w, r)`; templates `request_detail`, `request_thread`, `request_sheets`.

**A19 — one screen, four audiences.** The two mockups are the *same page*; only the `.action-bar` changes, and it changes on **permission**, not on role and not on which URL you arrived from. History and conversation are a single `.thread`. Approve, Return and Reject open an `.overlay > .sheet`; none of them is a bare inline form.

- [ ] **Step 1: Write the failing test**

```go
func TestRequestDetailIsOneScreenWithPermissionGatedActions(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Detail")
	mgrID := seedSecondApprover(t, s)
	mgr, _ := s.st.UserByID(s.ctx, mgrID)
	vendorID := seedAppVendor(t, s, "Meridian Facility Services")
	hash, _ := auth.HashPassword("RequesterPass123")
	rid, _ := s.st.CreateUser(s.ctx, "sneha@example.test", "Sneha Pillai", hash, "data_entry", true)
	s.assignRole(rid, "Requester")
	requester, _ := s.st.UserByID(s.ctx, rid)
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "vendor_invoice", ShortTitle: "July housekeeping", ProjectID: 1, HeadID: headID,
		Amount: 23500000, Purpose: "monthly contract", ManagerID: mgrID, VendorID: vendorID,
		InvoiceNo: "MFS/26-27/0912", InvoiceDate: "2026-07-22"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.AddRequestComment(s.ctx, requester, id, "Same as June."); err != nil {
		t.Fatal(err)
	}

	// --- The approver sees the decision controls. ---
	s.login("kavita@example.test", "ApproverPass123")
	body := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, ""))
	for _, want := range []string{
		`class="req-head"`, `class="rh-no"`, `class="rh-amt"`, `class="rh-status"`,
		`class="pill awaiting"`, `class="waiting you"`, `class="dl"`, `class="thread"`,
		`class="comment-box"`, `class="action-bar"`,
		`data-open="approve-sheet"`, `data-open="return-sheet"`, `data-open="reject-sheet"`,
		`id="approve-sheet"`, `class="overlay"`, `class="sheet"`,
		"MFS/26-27/0912", "Meridian Facility Services",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("approver detail view is missing %q", want)
		}
	}
	// A19: one merged stream, not two lists.
	if strings.Count(body, `class="thread"`) != 1 {
		t.Fatal("the detail page renders more than one thread")
	}
	if strings.Contains(body, "<h2>Conversation</h2>") && strings.Contains(body, "<h2>History</h2>") {
		t.Fatal("history and conversation are still two separate lists")
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("the detail page still renders .badge")
	}
	// The submit event and the comment are both in the one stream.
	if !strings.Contains(body, "submitted request") || !strings.Contains(body, "Same as June.") {
		t.Fatalf("the thread is missing an event or a comment: %s", body)
	}

	// --- The requester sees the same page, with a different action bar. ---
	s.login("sneha@example.test", "RequesterPass123")
	body = responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, ""))
	if strings.Contains(body, `data-open="approve-sheet"`) {
		t.Fatal("the requester is offered an approve control on their own request")
	}
	for _, want := range []string{`class="req-head"`, `class="thread"`, `href="/requests/` + strconvFormat(id) + `/edit"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("requester detail view is missing %q", want)
		}
	}

	// --- Approved: the requester is locked out of editing and offered cancellation. ---
	if err := s.st.ApproveRequest(s.ctx, mgr, id, 23500000, "ok"); err != nil {
		t.Fatal(err)
	}
	body = responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, ""))
	if strings.Contains(body, `href="/requests/`+strconvFormat(id)+`/edit"`) {
		t.Fatal("an approved request still offers an edit link")
	}
	for _, want := range []string{`class="banner locked"`, `href="/requests/` + strconvFormat(id) + `/cancel"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("approved detail view is missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestRequestDetailIsOneScreen -v`
Expected: FAIL — `request_detail` template undefined.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/app/app.go`:

```go
func (a *App) requestDetail(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	thread, err := a.st.RequestThread(r.Context(), req.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	atts, err := a.st.RequestAttachments(r.Context(), req.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	approvers, err := a.st.ListApprovers(r.Context(), req.RequesterID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data := PageData{Title: req.Number, Request2: req, Thread: thread, RequestAtts: atts, Approvers: approvers}
	// request-returned.html is the same request in a different state, so it is a
	// different template rather than a pile of conditionals (Task 28).
	name := "request_detail"
	if req.Status == "returned" && req.RequesterID == auth.CurrentUser(r).ID {
		name = "request_returned"
		formData, err := a.requestFormData(r, req.Number)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		formData.Request2, formData.Thread, formData.RequestAtts = req, thread, atts
		formData.FormType = req.Type
		data = formData
	}
	a.render(w, r, name, data)
}
```

Add to `internal/app/templates.go`:

```html
{{define "request_thread"}}
<div class="section-head">
  <h2>History and conversation</h2>
  <span class="small muted">Everyone who can see this request sees this whole stream</span>
</div>
<ol class="thread">
  {{range .Thread}}
  <li class="{{if eq .Kind "comment"}}is-comment{{if eq .ActorID $.User.ID}} is-me{{end}}{{end}}">
    <span class="tl-dot {{threadDot .}}">{{threadGlyph .}}</span>
    <div class="tl-head"><b>{{if eq .Kind "comment"}}{{.ActorName}}{{else}}{{.Title}}{{end}}</b><time>{{date .CreatedAt}}</time></div>
    <div class="tl-body">
      {{if eq .Kind "comment"}}<p>{{.Body}}</p>{{end}}
      {{if eq .Kind "attachment"}}<span class="tl-file">📎 {{.FileName}} · {{fileSize .FileSize}}</span>{{end}}
      {{if .Changes}}<div class="tl-change">{{range .Changes}}{{.Field}} <span class="was">{{.Was}}</span> → <span class="now">{{.Now}}</span><br>{{end}}</div>{{end}}
    </div>
  </li>
  {{else}}<li><span class="tl-dot">·</span><div class="tl-body muted">Nothing has happened yet.</div></li>{{end}}
</ol>

<form class="comment-box" method="post" action="/requests/{{.Request2.ID}}/comment">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <label for="cmt" class="flabel">Add a comment</label>
  <textarea id="cmt" name="body" placeholder="Anyone who can see this request will see your comment." required></textarea>
  <div class="cb-actions"><span class="row-end"></span><button class="btn primary small" type="submit">Post comment</button></div>
</form>
{{end}}

{{define "request_sheets"}}
<div class="overlay" id="approve-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/approve">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="sh-head"><div><h2>Approve {{money .Request2.Amount}}?</h2><p class="sh-sub">{{.Request2.Number}} · {{.Request2.Vendor}}</p></div><button class="sh-close" type="button" data-close="approve-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="field money-field">
        <label for="ap-amount">Amount approved</label>
        <span class="money-wrap"><span class="cur">₹</span><input id="ap-amount" name="approved_amount" inputmode="decimal" value="{{money .Request2.Amount}}"></span>
        <span class="in-words">{{inWords .Request2.Amount}}</span>
        <span class="hint">You may approve a smaller amount than was asked for.</span>
      </div>
      <div class="field"><label for="ap-note">Note <span class="opt">optional</span></label><textarea id="ap-note" name="note" placeholder="Recorded in the history and visible to everyone."></textarea></div>
      <p class="hint" style="margin:0">Accounts will be able to reserve this immediately. {{.Request2.RequesterName}} can no longer edit it.</p>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="approve-sheet">Cancel</button><span class="row-end"></span><button class="btn approve" type="submit">Approve request</button></div>
  </form>
</div>

<div class="overlay" id="return-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/return">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="sh-head"><div><h2>Return for correction</h2><p class="sh-sub">{{.Request2.RequesterName}} can edit and resubmit. The number and history stay.</p></div><button class="sh-close" type="button" data-close="return-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="field"><label for="rt-reason">What needs correcting <span class="req">*</span></label><textarea id="rt-reason" name="comment" required placeholder="Be specific — this is the whole message they get."></textarea></div>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="return-sheet">Cancel</button><span class="row-end"></span><button class="btn primary" type="submit">Return request</button></div>
  </form>
</div>

<div class="overlay" id="reject-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/reject">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="sh-head"><div><h2>Reject this request?</h2><p class="sh-sub">Rejection is final and read-only. {{.Request2.RequesterName}} would have to raise a new request.</p></div><button class="sh-close" type="button" data-close="reject-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="banner bad" style="margin:0"><span class="b-ico">!</span><div><b>This cannot be undone</b><p>If the request is fixable, return it for correction instead.</p></div></div>
      <div class="field"><label for="rj-reason">Reason for rejection <span class="req">*</span></label><textarea id="rj-reason" name="reason" required></textarea></div>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="reject-sheet">Cancel</button><span class="row-end"></span><button class="btn danger" type="submit">Reject permanently</button></div>
  </form>
</div>
{{end}}

{{define "request_detail"}}
{{template "top" .}}
<div class="req-head">
  <div class="rh-top">
    <span class="rh-no">{{.Request2.Number}}</span>
    <span class="rh-amt">{{money .Request2.Amount}}</span>
  </div>
  <h1>{{.Request2.ShortTitle}}</h1>
  <p class="rh-meta">{{typeLabel .Request2.Type}} · raised by {{.Request2.RequesterName}} on {{date .Request2.CreatedAt}}</p>
  <div class="rh-status">
    {{if .Request2.Urgent}}<span class="pill urgent">Urgent</span>{{end}}
    <span class="pill {{pillClass .Request2.Status}}">{{statusText .Request2.Status}}</span>
    {{$w := waitingOn .Request2 .User.ID}}<span class="waiting {{$w.Class}}">{{$w.Text}}</span>
  </div>
</div>

{{if and (eq .Request2.RequesterID .User.ID) (eq .Request2.Status "approved")}}
<div class="banner locked">
  <span class="b-ico">🔒</span>
  <div>
    <b>Approved requests are locked</b>
    <p>You can no longer edit this. If it should not be paid, ask for it to be cancelled — payment
      freezes while your approver decides.</p>
  </div>
</div>
{{end}}
{{if eq .Request2.Status "cancellation_requested"}}
<div class="banner warn">
  <span class="b-ico">⏸</span>
  <div>
    <b>Payment is frozen</b>
    <p>No accountant can reserve or pay this request until {{.Request2.ManagerName}} decides. Reason given: {{.Request2.CancelReason}}</p>
  </div>
</div>
{{end}}
{{if .Request2.AttachmentExceptionReason}}
<div class="banner info">
  <span class="b-ico">i</span>
  <div><b>No document was attached</b><p>{{.Request2.AttachmentExceptionReason}}</p></div>
</div>
{{end}}

<div class="card">
  <div class="card-head"><h2>Details</h2><span class="pill neutral no-dot">{{if eq .Request2.Treatment "recoverable"}}Recoverable{{else}}Budget expense{{end}}</span></div>
  <dl class="dl">
    <div><dt>Amount</dt><dd class="big">{{money .Request2.Amount}}</dd></div>
    {{if .Request2.ApprovedAmount}}<div><dt>Approved</dt><dd class="big">{{money (deref .Request2.ApprovedAmount)}}</dd></div>{{end}}
    {{if .Request2.NeededBy}}<div><dt>Needed by</dt><dd>{{dateLong .Request2.NeededBy}}</dd></div>{{end}}
    {{if .Request2.Project}}<div><dt>Project</dt><dd>{{.Request2.Project}}</dd></div>{{end}}
    {{if .Request2.Head}}<div><dt>Head</dt><dd>{{.Request2.Head}}</dd></div>{{end}}
    <div><dt>{{if .Request2.VendorID}}Vendor{{else}}Paid to{{end}}</dt><dd>{{if and .Request2.VendorID (.Perms.Can "vendor" "view")}}<a href="/vendors/{{deref .Request2.VendorID}}">{{.Request2.Vendor}}</a>{{else}}{{.Request2.Vendor}}{{end}}</dd></div>
    {{if .Request2.VendorGSTIN}}<div><dt>GSTIN</dt><dd class="num">{{.Request2.VendorGSTIN}}</dd></div>{{end}}
    {{if .Request2.InvoiceNo}}<div><dt>Invoice number</dt><dd class="num">{{.Request2.InvoiceNo}}</dd></div>{{end}}
    {{if .Request2.InvoiceDate}}<div><dt>Invoice date</dt><dd>{{dateLong .Request2.InvoiceDate}}</dd></div>{{end}}
    {{if .Request2.ExpenseDate}}<div><dt>Expense date</dt><dd>{{dateLong .Request2.ExpenseDate}}</dd></div>{{end}}
    {{if .Request2.AdvanceReason}}<div><dt>Advance reason</dt><dd>{{.Request2.AdvanceReason}}</dd></div>{{end}}
    {{if .Request2.Counterparty}}<div><dt>Counterparty</dt><dd>{{.Request2.Counterparty}}</dd></div>{{end}}
    {{if .Request2.ExpectedReturnDate}}<div><dt>Expected return</dt><dd>{{dateLong .Request2.ExpectedReturnDate}}</dd></div>{{end}}
    {{if .Request2.RepaymentNotes}}<div><dt>Repayment terms</dt><dd>{{.Request2.RepaymentNotes}}</dd></div>{{end}}
    <div><dt>Approver</dt><dd>{{.Request2.ManagerName}}</dd></div>
    <div><dt>Urgency</dt><dd>{{if .Request2.Urgent}}Urgent — {{.Request2.UrgencyReason}}{{else}}Normal{{end}}</dd></div>
    <div style="grid-column:1/-1"><dt>Purpose</dt><dd>{{.Request2.Purpose}}</dd></div>
  </dl>
</div>

<div class="section-head"><h2>Attachments</h2><span class="small muted">{{len .RequestAtts}} file{{if ne (len .RequestAtts) 1}}s{{end}}</span></div>
<div class="stack-8">
  {{range .RequestAtts}}
  <div class="file-row">
    <span class="f-ico">{{fileKind .OriginalName}}</span>
    <span><b>{{.OriginalName}}</b><small>{{fileSize .SizeBytes}} · added {{date .CreatedAt}}</small></span>
    <span class="f-actions"><a class="btn small outline" href="/attachments/{{.ID}}">Download</a></span>
  </div>
  {{else}}<p class="empty">No documents attached.</p>{{end}}
</div>

{{template "request_thread" .}}

<div class="action-bar">
  <span class="row-end"></span>
  {{if and (eq .Request2.RequesterID .User.ID) (or (eq .Request2.Status "pending") (eq .Request2.Status "returned"))}}
    <a class="btn outline" href="/requests/{{.Request2.ID}}/edit">Edit request</a>
  {{end}}
  {{if and (eq .Request2.RequesterID .User.ID) (eq .Request2.Status "pending") (.Perms.Can "request" "withdraw")}}
    <form method="post" action="/requests/{{.Request2.ID}}/withdraw"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="btn outline" type="submit">Withdraw</button></form>
  {{end}}
  {{if and (eq .Request2.RequesterID .User.ID) (eq .Request2.Status "approved") (.Perms.Can "request" "cancel")}}
    <a class="btn outline" href="/requests/{{.Request2.ID}}/cancel">Request cancellation</a>
  {{end}}
  {{if and (eq .Request2.RequesterID .User.ID) (eq .Request2.Status "rejected") (.Perms.Can "request" "reraise")}}
    <form method="post" action="/requests/{{.Request2.ID}}/reraise"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="btn primary" type="submit">Raise it again</button></form>
  {{end}}
  {{if and (eq .Request2.ManagerID .User.ID) (eq .Request2.Status "pending")}}
    {{if .Perms.Can "approval" "reject"}}<button class="btn danger outline" type="button" data-open="reject-sheet">Reject</button>{{end}}
    {{if .Perms.Can "approval" "return"}}<button class="btn outline" type="button" data-open="return-sheet">Return for correction</button>{{end}}
    {{if .Perms.Can "approval" "approve"}}<button class="btn approve" type="button" data-open="approve-sheet">Approve {{money .Request2.Amount}}</button>{{end}}
  {{end}}
  {{if and (eq .Request2.ManagerID .User.ID) (eq .Request2.Status "approved") (.Perms.Can "approval" "cancel")}}
    <a class="btn danger outline" href="/requests/{{.Request2.ID}}/cancellation">Cancel with reason</a>
  {{end}}
  {{if and (eq .Request2.Status "cancellation_requested") (.Perms.Can "approval" "cancel")}}
    <a class="btn primary" href="/requests/{{.Request2.ID}}/cancellation">Decide the cancellation</a>
  {{end}}
</div>

{{if and (eq .Request2.ManagerID .User.ID) (eq .Request2.Status "pending")}}{{template "request_sheets" .}}{{end}}
{{template "bottom" .}}
{{end}}
```

Register the two small display funcs:

```go
		"threadDot": func(e store.ThreadEntry) string {
			switch e.Action {
			case "submit", "reraise":
				return "brand"
			case "approve":
				return "ok"
			case "return", "reject", "cancel", "cancel_request", "cancel_accept":
				return "warn"
			default:
				return ""
			}
		},
		"threadGlyph": func(e store.ThreadEntry) string {
			switch e.Kind {
			case "comment":
				return e.Initials
			case "attachment":
				return "⇪"
			}
			switch e.Action {
			case "submit", "reraise":
				return "＋"
			case "approve":
				return "✓"
			case "return":
				return "↩"
			case "reject":
				return "✕"
			case "update":
				return "✎"
			case "cancel", "cancel_request", "cancel_accept":
				return "⏸"
			default:
				return "·"
			}
		},
		"fileKind": func(name string) string {
			ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(name), "."))
			if ext == "" {
				return "FILE"
			}
			return ext
		},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run TestRequestDetailIsOneScreen -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): request detail with merged thread and permission-gated action bar"
```

---
### Task 27: Screen — `request-edit.html`

**Files:**
- Modify: `internal/app/templates.go` (`request_edit`)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `requestEditForm`/`requestEdit` (Tasks 19 and 22), `request_form_fields` (Task 21), `store.UpdateRequest` (Task 10).
- Produces: template `request_edit`.

**What this screen must say, from the mockup:** "Every change is recorded in the history, notifies your approver, and restarts the three-day reminder clock. Change the approver and it moves to that person instead." The store already does all three (Task 10); the screen's job is to promise it before the person commits, and to show what a field *was*.

- [ ] **Step 1: Write the failing test**

```go
func TestRequestEditScreenAndReroute(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Edit")
	mgrID := seedSecondApprover(t, s)
	hash, _ := auth.HashPassword("RequesterPass123")
	rid, _ := s.st.CreateUser(s.ctx, "arun@example.test", "Arun Mehta", hash, "data_entry", true)
	s.assignRole(rid, "Requester")
	requester, _ := s.st.UserByID(s.ctx, rid)
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "reimbursement", ShortTitle: "Hyderabad site visit", ProjectID: 1, HeadID: headID,
		Amount: 1690000, Purpose: "flight and hotel", ExpenseDate: "2026-07-17", ManagerID: mgrID})
	if err != nil {
		t.Fatal(err)
	}
	other := seedThirdApprover(t, s)

	s.login("arun@example.test", "RequesterPass123")
	body := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id)+"/edit", nil, ""))
	for _, want := range []string{`class="banner info"`, `class="money-field`, `class="in-words"`,
		`class="action-bar"`, `name="manager_id"`, `value="₹16,900.00"`, "restarts"} {
		if !strings.Contains(body, want) {
			t.Fatalf("edit screen is missing %q", want)
		}
	}
	// A16: the type is still not editable — it is what the request is.
	if strings.Contains(body, `<select id="rtype"`) {
		t.Fatal("the edit screen offers a type selector")
	}

	resp := s.postForm("/requests/"+strconvFormat(id)+"/edit", url.Values{
		"type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Hyderabad site visit"},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)}, "amount": {"18,400.00"},
		"purpose": {"flight, hotel and cabs"}, "expense_date": {"2026-07-17"},
		"manager_id": {strconvFormat(other)},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	got, _ := s.st.Request(s.ctx, id)
	if got.Amount != 1840000 || got.ManagerID != other {
		t.Fatalf("edit not applied: amount=%d manager=%d", got.Amount, got.ManagerID)
	}
	if got.ReminderLastSent != nil {
		t.Fatal("the reminder clock was not restarted")
	}
	// The change is on the merged thread with a before/after.
	thread, _ := s.st.RequestThread(s.ctx, id)
	var sawDiff bool
	for _, e := range thread {
		if e.Action == "update" && len(e.Changes) > 0 {
			sawDiff = true
		}
	}
	if !sawDiff {
		t.Fatal("the edit is not visible as a change in the thread")
	}
}

func seedThirdApprover(t *testing.T, s *appTestServer) int64 {
	t.Helper()
	hash, _ := auth.HashPassword("ApproverPass456")
	id, err := s.st.CreateUser(s.ctx, "rakesh@example.test", "Rakesh Iyer", hash, "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	s.assignRole(id, "Manager")
	return id
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestRequestEditScreenAndReroute -v`
Expected: FAIL — `request_edit` template undefined.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/app/templates.go`:

```html
{{define "request_edit"}}
{{template "top" .}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">{{.Request2.Number}} · {{statusText .Request2.Status}}</div>
    <h1>Edit request</h1>
    <p class="sub">{{.Request2.ShortTitle}}</p>
  </div>
</section>

{{if .Error}}<div class="banner bad"><span class="b-ico">!</span><div><b>{{.Error}}</b></div></div>{{end}}

<div class="banner info">
  <span class="b-ico">i</span>
  <div>
    <b>Editing tells {{.Request2.ManagerName}} again</b>
    <p>Every change is recorded in the history, notifies your approver, and restarts the three-day
      reminder clock. Change the approver and it moves to that person instead.</p>
  </div>
</div>

<form method="post" enctype="multipart/form-data" action="/requests/{{.Request2.ID}}/edit">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <input type="hidden" name="type" value="{{.FormType}}">
  <input type="hidden" name="treatment" value="{{.Request2.Treatment}}">
  <input type="hidden" name="recoverable_category" value="{{.Request2.RecoverableCategory}}">

  <fieldset>
    <legend>What is this for</legend>
    <div class="form-grid">
      <div class="field span-12">
        <label for="short-title">Short title <span class="req">*</span></label>
        <input id="short-title" name="short_title" value="{{.Request2.ShortTitle}}" required>
      </div>
    </div>
  </fieldset>

  <div id="form-fields">{{template "request_form_fields" .}}</div>

  <fieldset>
    <legend>Amount and timing</legend>
    <div class="form-grid">
      <div class="field span-6 money-field">
        <label for="amount">Amount <span class="req">*</span></label>
        <span class="money-wrap"><span class="cur">₹</span><input id="amount" name="amount" inputmode="decimal" value="{{money .Request2.Amount}}" required></span>
        <span class="in-words">{{inWords .Request2.Amount}}</span>
        <span class="hint">Was {{money .Request2.Amount}} when last saved.</span>
      </div>
      <div class="field span-6 m-half">
        <label for="needed">Needed by</label>
        <input id="needed" type="date" name="needed_by" value="{{.Request2.NeededBy}}">
      </div>
      {{if ne (index .Settings "urgency_mode") "disabled"}}
      <div class="field span-12">
        <label class="checkline"><input type="checkbox" name="urgent" {{check .Request2.Urgent}}> Marked urgent</label>
      </div>
      {{if eq (index .Settings "urgency_mode") "reason"}}
      <div class="field span-12" data-when="urgent:on" {{if not .Request2.Urgent}}hidden{{end}}>
        <label for="ureason">Why is it urgent <span class="req">*</span></label>
        <input id="ureason" name="urgency_reason" value="{{.Request2.UrgencyReason}}">
      </div>
      {{end}}
      {{end}}
    </div>
  </fieldset>

  {{if or (eq .FormType "vendor_invoice") (eq .FormType "vendor_advance")}}
  <fieldset>
    <legend>Vendor and {{if eq .FormType "vendor_invoice"}}invoice{{else}}advance{{end}}</legend>
    <div class="form-grid">
      <div class="field span-6">
        <label for="vendor">Vendor <span class="req">*</span></label>
        <span class="combo">
          <input class="combo-input" id="vendor" autocomplete="off" value="{{.Request2.Vendor}}"
                 hx-get="/vendors/search" hx-trigger="keyup changed delay:250ms" hx-target="#vendor-options">
          <span class="combo-caret">▾</span>
          <span class="combo-list" id="vendor-options"></span>
        </span>
        <input type="hidden" name="vendor_id" id="vendor-id" value="{{deref .Request2.VendorID}}">
      </div>
      {{if eq .FormType "vendor_invoice"}}
      <div class="field span-3 m-half"><label for="inv">Invoice number <span class="req">*</span></label><input id="inv" name="invoice_no" value="{{.Request2.InvoiceNo}}" required></div>
      <div class="field span-3 m-half"><label for="idate">Invoice date <span class="req">*</span></label><input id="idate" type="date" name="invoice_date" value="{{.Request2.InvoiceDate}}" required></div>
      {{else}}
      <div class="field span-6"><label for="areason">Reason for the advance <span class="req">*</span></label><input id="areason" name="advance_reason" value="{{.Request2.AdvanceReason}}" required></div>
      {{end}}
    </div>
  </fieldset>
  {{end}}

  {{if eq .FormType "reimbursement"}}
  <fieldset>
    <legend>Expense</legend>
    <div class="form-grid">
      <div class="field span-6 m-half"><label for="paid">Paid to</label><input id="paid" value="{{.Request2.VendorPayee}}" readonly></div>
      <div class="field span-6 m-half"><label for="edate">Expense date <span class="req">*</span></label><input id="edate" type="date" name="expense_date" value="{{.Request2.ExpenseDate}}" required></div>
    </div>
  </fieldset>
  {{end}}

  {{if eq .FormType "employee_advance"}}
  <fieldset>
    <legend>Advance details</legend>
    <div class="form-grid">
      <div class="field span-12"><label for="areason2">What the money is for <span class="req">*</span></label><input id="areason2" name="advance_reason" value="{{.Request2.AdvanceReason}}" required></div>
    </div>
  </fieldset>
  {{end}}

  <fieldset>
    <legend>Purpose and documents</legend>
    <div class="form-grid">
      <div class="field span-12"><label for="purp">Purpose <span class="req">*</span></label><textarea id="purp" name="purpose" required>{{.Request2.Purpose}}</textarea></div>
      <div class="field span-12">
        <span class="flabel">Documents</span>
        <div class="stack-8">
          {{range .RequestAtts}}<div class="file-row"><span class="f-ico">{{fileKind .OriginalName}}</span><span><b>{{.OriginalName}}</b><small>{{fileSize .SizeBytes}}</small></span></div>{{end}}
          <label class="uploader"><div class="up-ico">⇪</div><b>Add another document</b><small>PDF, JPG or PNG up to {{index .Settings "attachment_max_mb"}} MB</small><input type="file" name="attachment"></label>
        </div>
      </div>
    </div>
  </fieldset>

  <fieldset>
    <legend>Approver</legend>
    <div class="form-grid">
      <div class="field span-6">
        <label for="apr">Approver <span class="req">*</span></label>
        <select id="apr" name="manager_id" required>
          {{range .Approvers}}<option value="{{.ID}}" {{if eq $.Request2.ManagerID .ID}}selected{{end}}>{{.Name}}</option>{{end}}
        </select>
        <span class="hint">Changing this moves the request to the new approver and notifies them.</span>
      </div>
    </div>
  </fieldset>

  <div class="action-bar">
    <span class="ab-note d-only">Every change is recorded in the history.</span>
    <span class="row-end"></span>
    <a class="btn outline" href="/requests/{{.Request2.ID}}">Discard changes</a>
    <button class="btn primary" type="submit">Save and notify {{.Request2.ManagerName}}</button>
  </div>
</form>
{{template "bottom" .}}
{{end}}
```

> `requestEditForm` must also load the attachments so the Documents block lists what is already there — add `data.RequestAtts, err = a.st.RequestAttachments(r.Context(), req.ID)` to it.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run TestRequestEditScreenAndReroute -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): request edit screen with reroute and reminder restart"
```

---

### Task 28: Screen — `request-returned.html` (correct and resubmit)

**Files:**
- Modify: `internal/app/templates.go` (`request_returned`)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `requestDetail`'s returned branch (Task 26), `store.SubmitRequest` (Task 5), `request_form_fields`, `request_thread`.
- Produces: template `request_returned`.

**The distinction this screen exists to make, verbatim from the mockup:** "Returned is not rejected. A returned request keeps its number and its history — you correct it and resubmit. A rejected request is final and read-only; the employee raises a new one." So the correction form is on the *same* URL as the detail, above the same thread, and the primary action resubmits rather than saving.

- [ ] **Step 1: Write the failing test**

```go
func TestReturnedRequestScreenCorrectsAndResubmits(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Ret")
	mgrID := seedSecondApprover(t, s)
	mgr, _ := s.st.UserByID(s.ctx, mgrID)
	vendorID := seedAppVendor(t, s, "Kaveri Logistics")
	hash, _ := auth.HashPassword("RequesterPass123")
	rid, _ := s.st.CreateUser(s.ctx, "ret@example.test", "Ret Requester", hash, "data_entry", true)
	s.assignRole(rid, "Requester")
	requester, _ := s.st.UserByID(s.ctx, rid)
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "vendor_invoice", ShortTitle: "July freight", ProjectID: 1, HeadID: headID,
		Amount: 6450000, Purpose: "freight", ManagerID: mgrID, VendorID: vendorID,
		InvoiceNo: "KL/2026/0788", InvoiceDate: "2026-06-28"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.ReturnRequest(s.ctx, mgr, id, "The invoice attached is the June one."); err != nil {
		t.Fatal(err)
	}

	s.login("ret@example.test", "RequesterPass123")
	body := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, ""))
	for _, want := range []string{
		`class="pill returned"`, `class="banner warn"`, "The invoice attached is the June one.",
		`class="thread"`, `action="/requests/` + strconvFormat(id) + `/edit"`,
		`action="/requests/` + strconvFormat(id) + `/submit"`, "Resubmit for approval",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("returned screen is missing %q", want)
		}
	}
	// It keeps its number — that is the whole difference from a rejection.
	got, _ := s.st.Request(s.ctx, id)
	if !strings.Contains(body, got.Number) {
		t.Fatal("the returned screen does not show the retained request number")
	}

	// Correct, then resubmit; the number survives and the status goes back to pending.
	resp := s.postForm("/requests/"+strconvFormat(id)+"/edit", url.Values{
		"type": {"vendor_invoice"}, "treatment": {"budget"}, "short_title": {"July freight"},
		"project_id": {"1"}, "head_id": {strconvFormat(headID)}, "amount": {"64,500.00"},
		"purpose": {"freight"}, "vendor_id": {strconvFormat(vendorID)},
		"invoice_no": {"KL/2026/0812"}, "invoice_date": {"2026-07-21"},
		"manager_id": {strconvFormat(mgrID)},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	resp = s.postForm("/requests/"+strconvFormat(id)+"/submit", url.Values{})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	after, _ := s.st.Request(s.ctx, id)
	if after.Status != "pending" || after.Number != got.Number || after.InvoiceNo != "KL/2026/0812" {
		t.Fatalf("after correction and resubmit = %+v", after)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestReturnedRequestScreen -v`
Expected: FAIL — `request_returned` template undefined.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/app/templates.go`:

```html
{{define "request_returned"}}
{{template "top" .}}
<div class="req-head">
  <div class="rh-top"><span class="rh-no">{{.Request2.Number}}</span><span class="rh-amt">{{money .Request2.Amount}}</span></div>
  <h1>{{.Request2.ShortTitle}}</h1>
  <p class="rh-meta">{{typeLabel .Request2.Type}}{{if .Request2.Project}} · {{.Request2.Project}} / {{.Request2.Head}}{{end}} · raised {{date .Request2.CreatedAt}}</p>
  <div class="rh-status">
    <span class="pill returned">Returned for correction</span>
    <span class="waiting you">Waiting on you</span>
  </div>
</div>

<div class="banner warn">
  <span class="b-ico">↩</span>
  <div>
    <b>{{.Request2.ManagerName}} sent this back</b>
    <p>“{{.Request2.DecisionReason}}”</p>
  </div>
</div>

<p class="hint">Returned is not rejected. This request keeps its number and its history — correct it
  and send it again.</p>

<form method="post" enctype="multipart/form-data" action="/requests/{{.Request2.ID}}/edit">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <input type="hidden" name="type" value="{{.FormType}}">
  <input type="hidden" name="treatment" value="{{.Request2.Treatment}}">
  <input type="hidden" name="recoverable_category" value="{{.Request2.RecoverableCategory}}">
  <input type="hidden" name="manager_id" value="{{.Request2.ManagerID}}">
  <fieldset>
    <legend>Correct and resubmit</legend>
    <div class="form-grid">
      <div class="field span-12"><label for="rt-title">Short title <span class="req">*</span></label><input id="rt-title" name="short_title" value="{{.Request2.ShortTitle}}" required></div>
      {{if eq .FormType "vendor_invoice"}}
      <div class="field span-4 m-half"><label for="rt-inv">Invoice number <span class="req">*</span></label><input id="rt-inv" name="invoice_no" value="{{.Request2.InvoiceNo}}" required></div>
      <div class="field span-4 m-half"><label for="rt-idate">Invoice date <span class="req">*</span></label><input id="rt-idate" type="date" name="invoice_date" value="{{.Request2.InvoiceDate}}" required></div>
      <input type="hidden" name="vendor_id" value="{{deref .Request2.VendorID}}">
      {{end}}
      {{if eq .FormType "reimbursement"}}
      <div class="field span-4 m-half"><label for="rt-edate">Expense date <span class="req">*</span></label><input id="rt-edate" type="date" name="expense_date" value="{{.Request2.ExpenseDate}}" required></div>
      {{end}}
      {{if or (eq .FormType "vendor_advance") (eq .FormType "employee_advance")}}
      <div class="field span-8"><label for="rt-areason">Reason for the advance <span class="req">*</span></label><input id="rt-areason" name="advance_reason" value="{{.Request2.AdvanceReason}}" required></div>
      {{if eq .FormType "vendor_advance"}}<input type="hidden" name="vendor_id" value="{{deref .Request2.VendorID}}">{{end}}
      {{end}}
      <div class="field span-4 money-field">
        <label for="rt-amt">Amount <span class="req">*</span></label>
        <span class="money-wrap"><span class="cur">₹</span><input id="rt-amt" name="amount" inputmode="decimal" value="{{money .Request2.Amount}}" required></span>
        <span class="in-words">{{inWords .Request2.Amount}}</span>
      </div>
      <input type="hidden" name="project_id" value="{{deref .Request2.ProjectID}}">
      <input type="hidden" name="head_id" value="{{deref .Request2.HeadID}}">
      <div class="field span-12"><label for="rt-purpose">Purpose <span class="req">*</span></label><textarea id="rt-purpose" name="purpose" required>{{.Request2.Purpose}}</textarea></div>
      <div class="field span-12">
        <span class="flabel">Documents</span>
        <div class="stack-8">
          {{range .RequestAtts}}<div class="file-row"><span class="f-ico">{{fileKind .OriginalName}}</span><span><b>{{.OriginalName}}</b><small>{{fileSize .SizeBytes}}</small></span></div>{{end}}
          <label class="uploader"><div class="up-ico">⇪</div><b>Attach the corrected document</b><small>PDF, JPG or PNG up to {{index .Settings "attachment_max_mb"}} MB</small><input type="file" name="attachment"></label>
        </div>
      </div>
    </div>
  </fieldset>
  <div class="action-bar">
    <span class="ab-note d-only">Saving keeps it with you; resubmitting sends it back to {{.Request2.ManagerName}}.</span>
    <span class="row-end"></span>
    <button class="btn outline" type="submit">Save corrections</button>
  </div>
</form>

<form method="post" action="/requests/{{.Request2.ID}}/submit">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <div class="action-bar">
    <span class="row-end"></span>
    <button class="btn primary" type="submit">Resubmit for approval</button>
  </div>
</form>

{{template "request_thread" .}}
{{template "bottom" .}}
{{end}}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run TestReturnedRequestScreen -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): returned-request correction screen"
```

---

### Task 29: Screens — `request-cancel.html` and `manager-cancellation-decision.html`

**Files:**
- Modify: `internal/app/app.go` (`requestCancelForm`, `requestCancelAsk`, `requestCancellationForm`, `requestCancellationDecide`, `requestCancelOutright`), `internal/app/templates.go` (`request_cancel`, `request_cancellation`)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `store.RequestCancellation`/`DecideCancellation`/`CancelRequest` (Task 15), `request_thread` (Task 26).
- Produces: the five cancellation handlers and both templates.

- [ ] **Step 1: Write the failing test**

```go
func TestCancellationScreensEndToEnd(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Cancel")
	mgrID := seedSecondApprover(t, s)
	mgr, _ := s.st.UserByID(s.ctx, mgrID)
	vendorID := seedAppVendor(t, s, "Anand Steel Traders")
	hash, _ := auth.HashPassword("RequesterPass123")
	rid, _ := s.st.CreateUser(s.ctx, "cancelreq@example.test", "Cancel Requester", hash, "data_entry", true)
	s.assignRole(rid, "Requester")
	requester, _ := s.st.UserByID(s.ctx, rid)
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "vendor_advance", ShortTitle: "Binding wire order", ProjectID: 1, HeadID: headID,
		Amount: 4700000, Purpose: "advance", ManagerID: mgrID, VendorID: vendorID,
		AdvanceReason: "40% booking against PO-2026-0417"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.ApproveRequest(s.ctx, mgr, id, 4700000, ""); err != nil {
		t.Fatal(err)
	}

	// --- The employee asks. ---
	s.login("cancelreq@example.test", "RequesterPass123")
	body := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id)+"/cancel", nil, ""))
	for _, want := range []string{`class="banner warn"`, "Payment freezes", `class="req-head"`,
		`name="reason"`, `class="action-bar"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("cancel screen is missing %q", want)
		}
	}
	if resp := s.postForm("/requests/"+strconvFormat(id)+"/cancel-request", url.Values{"reason": {""}}); resp.StatusCode == http.StatusSeeOther {
		t.Fatal("a cancellation with no reason was accepted")
	}
	resp := s.postForm("/requests/"+strconvFormat(id)+"/cancel-request", url.Values{"reason": {"Site cancelled the order"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	frozen, _ := s.st.Request(s.ctx, id)
	if frozen.Status != "cancellation_requested" {
		t.Fatalf("status = %q, want cancellation_requested", frozen.Status)
	}
	// The requester sees the freeze on the detail page.
	detail := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, ""))
	if !strings.Contains(detail, `class="pill cancelreq"`) || !strings.Contains(detail, "Payment is frozen") {
		t.Fatalf("the frozen state is not shown to the requester: %s", detail)
	}

	// --- The approver decides. ---
	s.login("kavita@example.test", "ApproverPass123")
	body = responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(id)+"/cancellation", nil, ""))
	for _, want := range []string{"Site cancelled the order", `data-open="accept-sheet"`,
		`data-open="decline-sheet"`, `class="overlay"`, `class="sheet"`, `class="thread"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("cancellation decision screen is missing %q", want)
		}
	}
	// Decline requires a reason and unfreezes.
	if resp := s.postForm("/requests/"+strconvFormat(id)+"/cancellation", url.Values{"decision": {"decline"}, "note": {""}}); resp.StatusCode == http.StatusSeeOther {
		t.Fatal("a decline with no reason was accepted")
	}
	resp = s.postForm("/requests/"+strconvFormat(id)+"/cancellation", url.Values{"decision": {"decline"}, "note": {"Vendor already dispatched"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if got, _ := s.st.Request(s.ctx, id); got.Status != "approved" {
		t.Fatalf("declined cancellation left status %q, want approved", got.Status)
	}
	// Accept closes it.
	if err := s.st.RequestCancellation(s.ctx, requester, id, "Order withdrawn"); err != nil {
		t.Fatal(err)
	}
	resp = s.postForm("/requests/"+strconvFormat(id)+"/cancellation", url.Values{"decision": {"accept"}, "note": {"Agreed"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if got, _ := s.st.Request(s.ctx, id); got.Status != "cancelled" {
		t.Fatalf("accepted cancellation left status %q, want cancelled", got.Status)
	}

	// --- G2: the approver cancels a different request outright. ---
	other, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "vendor_advance", ShortTitle: "Second order", ProjectID: 1, HeadID: headID,
		Amount: 100000, Purpose: "advance", ManagerID: mgrID, VendorID: vendorID, AdvanceReason: "booking"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.ApproveRequest(s.ctx, mgr, other, 100000, ""); err != nil {
		t.Fatal(err)
	}
	resp = s.postForm("/requests/"+strconvFormat(other)+"/cancel", url.Values{"reason": {"Budget pulled for the quarter"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if got, _ := s.st.Request(s.ctx, other); got.Status != "cancelled" || got.CancelReason == "" {
		t.Fatalf("outright cancel = %+v", got)
	}

	// A requester cannot reach the decision screen.
	s.login("cancelreq@example.test", "RequesterPass123")
	requireStatus(t, s.request(http.MethodGet, "/requests/"+strconvFormat(other)+"/cancellation", nil, ""), http.StatusForbidden)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestCancellationScreensEndToEnd -v`
Expected: FAIL — 404 on `/requests/{id}/cancel`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/app/app.go`:

```go
func (a *App) requestCancelForm(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	a.render(w, r, "request_cancel", PageData{Title: "Request cancellation", Request2: req})
}

func (a *App) requestCancelAsk(w http.ResponseWriter, r *http.Request) {
	if err := a.st.RequestCancellation(r.Context(), auth.CurrentUser(r), pathID(r), r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", pathID(r)), http.StatusSeeOther)
}

func (a *App) requestCancellationForm(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	thread, err := a.st.RequestThread(r.Context(), req.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "request_cancellation", PageData{Title: "Cancellation request", Request2: req, Thread: thread})
}

func (a *App) requestCancellationDecide(w http.ResponseWriter, r *http.Request) {
	accept := r.FormValue("decision") == "accept"
	if err := a.st.DecideCancellation(r.Context(), auth.CurrentUser(r), pathID(r), accept, r.FormValue("note")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/approvals?bucket=cancellations", http.StatusSeeOther)
}

func (a *App) requestCancelOutright(w http.ResponseWriter, r *http.Request) {
	if err := a.st.CancelRequest(r.Context(), auth.CurrentUser(r), pathID(r), r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", pathID(r)), http.StatusSeeOther)
}
```

Add to `internal/app/templates.go`:

```html
{{define "request_cancel"}}
{{template "top" .}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">{{.Request2.Number}} · {{statusText .Request2.Status}}</div>
    <h1>Ask for this to be cancelled</h1>
    <p class="sub">Approved requests cannot be withdrawn on your own. Your approver decides.</p>
  </div>
</section>

<div class="banner warn">
  <span class="b-ico">⏸</span>
  <div>
    <b>Payment freezes the moment you ask</b>
    <p>Accounts cannot reserve or pay this request while a cancellation is pending. If an accountant
      has already reserved it, they are told immediately.</p>
  </div>
</div>

<div class="req-head">
  <div class="rh-top"><span class="rh-no">{{.Request2.Number}}</span><span class="rh-amt">{{money .Request2.Amount}}</span></div>
  <h1>{{.Request2.ShortTitle}}</h1>
  <p class="rh-meta">Approved by {{.Request2.ManagerName}}{{if .Request2.ApprovedAt}} on {{datep .Request2.ApprovedAt}}{{end}}</p>
  <div class="rh-status"><span class="pill {{pillClass .Request2.Status}}">{{statusText .Request2.Status}}</span></div>
</div>

<form method="post" action="/requests/{{.Request2.ID}}/cancel-request">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <fieldset>
    <legend>Why should it be cancelled</legend>
    <div class="form-grid">
      <div class="field span-12">
        <label for="reason">Reason <span class="req">*</span></label>
        <textarea id="reason" name="reason" required placeholder="Say what changed. {{.Request2.ManagerName}} sees exactly this."></textarea>
      </div>
      <div class="field span-12">
        <span class="flabel">What happens next</span>
        <ul class="hint" style="margin:4px 0 0; padding-left:18px">
          <li>{{.Request2.ManagerName}} is notified and the request shows <b>Cancellation requested</b>.</li>
          <li>If they accept, the request is cancelled and closed. Nothing can be paid against it.</li>
          <li>If they decline, it returns to <b>Approved — awaiting payment</b> and Accounts can proceed.</li>
        </ul>
      </div>
    </div>
  </fieldset>
  <div class="action-bar">
    <span class="row-end"></span>
    <a class="btn outline" href="/requests/{{.Request2.ID}}">Never mind</a>
    <button class="btn danger" type="submit">Send cancellation request</button>
  </div>
</form>
{{template "bottom" .}}
{{end}}

{{define "request_cancellation"}}
{{template "top" .}}
<div class="req-head">
  <div class="rh-top"><span class="rh-no">{{.Request2.Number}}</span><span class="rh-amt">{{money .Request2.Amount}}</span></div>
  <h1>{{.Request2.ShortTitle}}</h1>
  <p class="rh-meta">{{typeLabel .Request2.Type}}{{if .Request2.Project}} · {{.Request2.Project}} / {{.Request2.Head}}{{end}}{{if .Request2.ApprovedAt}} · approved {{datep .Request2.ApprovedAt}}{{end}}</p>
  <div class="rh-status">
    <span class="pill {{pillClass .Request2.Status}}">{{statusText .Request2.Status}}</span>
    {{$w := waitingOn .Request2 .User.ID}}<span class="waiting {{$w.Class}}">{{$w.Text}}</span>
  </div>
</div>

{{if eq .Request2.Status "cancellation_requested"}}
<div class="banner warn">
  <span class="b-ico">⏸</span>
  <div>
    <b>Payment is frozen</b>
    <p>No accountant can reserve or pay this request until you decide.</p>
  </div>
</div>

<div class="card">
  <div class="card-head"><h2>Why {{.Request2.RequesterName}} wants it cancelled</h2></div>
  <div class="card-body"><p style="margin:0">“{{.Request2.CancelReason}}”</p></div>
</div>
{{end}}

<div class="card" style="margin-top:12px">
  <div class="card-head"><h2>What you approved</h2></div>
  <dl class="dl">
    <div><dt>Amount</dt><dd class="big">{{if .Request2.ApprovedAmount}}{{money (deref .Request2.ApprovedAmount)}}{{else}}{{money .Request2.Amount}}{{end}}</dd></div>
    {{if .Request2.ApprovedAt}}<div><dt>Approved on</dt><dd>{{datep .Request2.ApprovedAt}}</dd></div>{{end}}
    <div><dt>{{if .Request2.VendorID}}Vendor{{else}}Paid to{{end}}</dt><dd>{{.Request2.Vendor}}</dd></div>
    {{if .Request2.AdvanceReason}}<div><dt>Advance reason</dt><dd>{{.Request2.AdvanceReason}}</dd></div>{{end}}
  </dl>
</div>

{{template "request_thread" .}}

<div class="action-bar">
  <span class="ab-note d-only">{{if eq .Request2.Status "cancellation_requested"}}Declining sends it back to Accounts to pay as approved.{{else}}Cancelling closes the request permanently.{{end}}</span>
  <span class="row-end"></span>
  {{if eq .Request2.Status "cancellation_requested"}}
    <button class="btn outline" type="button" data-open="decline-sheet">Decline — keep it live</button>
    <button class="btn danger" type="button" data-open="accept-sheet">Cancel the request</button>
  {{else}}
    <button class="btn danger" type="button" data-open="outright-sheet">Cancel with reason</button>
  {{end}}
</div>

<div class="overlay" id="accept-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/cancellation">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <input type="hidden" name="decision" value="accept">
    <div class="sh-head"><div><h2>Cancel {{.Request2.Number}}?</h2><p class="sh-sub">{{money .Request2.Amount}} to {{.Request2.Vendor}}</p></div><button class="sh-close" type="button" data-close="accept-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="banner bad" style="margin:0"><span class="b-ico">!</span><div><b>The request closes permanently</b><p>Nothing can be paid against it. A new request is needed if the order comes back.</p></div></div>
      <div class="field"><label for="cx-note">Note <span class="opt">optional</span></label><textarea id="cx-note" name="note" placeholder="Recorded in the history."></textarea></div>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="accept-sheet">Back</button><span class="row-end"></span><button class="btn danger" type="submit">Cancel request</button></div>
  </form>
</div>

<div class="overlay" id="decline-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/cancellation">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <input type="hidden" name="decision" value="decline">
    <div class="sh-head"><div><h2>Decline the cancellation</h2><p class="sh-sub">The request returns to Approved — awaiting payment.</p></div><button class="sh-close" type="button" data-close="decline-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="field"><label for="dc-reason">Why it should still be paid <span class="req">*</span></label><textarea id="dc-reason" name="note" required placeholder="{{.Request2.RequesterName}} and Accounts both see this."></textarea></div>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="decline-sheet">Back</button><span class="row-end"></span><button class="btn primary" type="submit">Decline and unfreeze</button></div>
  </form>
</div>

<div class="overlay" id="outright-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/cancel">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="sh-head"><div><h2>Cancel {{.Request2.Number}}?</h2><p class="sh-sub">{{.Request2.RequesterName}} did not ask for this.</p></div><button class="sh-close" type="button" data-close="outright-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="field"><label for="oc-reason">Reason <span class="req">*</span></label><textarea id="oc-reason" name="reason" required placeholder="{{.Request2.RequesterName}} and Accounts both see this."></textarea></div>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="outright-sheet">Back</button><span class="row-end"></span><button class="btn danger" type="submit">Cancel request</button></div>
  </form>
</div>
{{template "bottom" .}}
{{end}}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run TestCancellationScreensEndToEnd -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): cancellation request and manager cancellation decision screens"
```

---
### Task 30: Configuration screen shell (D6, G21)

**Files:**
- Modify: `internal/app/app.go` (`configuration`, `configurationSave`, `configSections`), `internal/app/templates.go` (`configuration`)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `store.AppSettings`/`SetAppSettings` (Task 4), `RequirePermission("config", …)`.
- Produces: `GET /configuration` and `POST /configuration`; `var configSections []ConfigSection`; template `configuration`.

**D6 — what Phase 2 owns and what it must leave room for.** One screen, one `<form>`, one `<fieldset>` per section, one generic `app_settings`-backed save. Phase 2 lands the shell and the **Numbering, Attachments, Urgency, Approvals and Payments** fieldsets. Phase 3 appends its reservation fieldset, Phase 4 replaces the standalone `/recoverable-categories` page with a Recoverable categories fieldset, and Phase 5 appends Reminders and SMTP. Later phases must therefore only need to append a `ConfigSection` — if a later phase has to touch the handler, this task got the shape wrong.

**Two controls are deliberately not settings.** "Block self-approval" renders checked and `disabled` (G8 is structural, not configurable) and "Second approval above a threshold" renders `disabled` as a stated non-goal. Both are in the mockup; neither is written to `app_settings`.

- [ ] **Step 1: Write the failing test**

```go
func TestConfigurationScreenReadsAndWritesAppSettings(t *testing.T) {
	s := newAppTestServer(t)

	// Permission-gated by URL, not by menu-hiding.
	hash, _ := auth.HashPassword("RequesterPass123")
	rid, _ := s.st.CreateUser(s.ctx, "noconfig@example.test", "No Config", hash, "data_entry", true)
	s.assignRole(rid, "Requester")
	s.login("noconfig@example.test", "RequesterPass123")
	requireStatus(t, s.request(http.MethodGet, "/configuration", nil, ""), http.StatusForbidden)
	requireStatus(t, s.postForm("/configuration", url.Values{"number_prefix": {"HACK"}}), http.StatusForbidden)
	if v, _ := s.st.AppSetting(s.ctx, "number_prefix"); v != "PR" {
		t.Fatalf("an unprivileged POST changed a setting: %q", v)
	}

	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/configuration", nil, ""))
	for _, want := range []string{
		"<legend>Request numbering</legend>", "<legend>Attachments</legend>",
		"<legend>Urgency</legend>", "<legend>Approvals</legend>", "<legend>Payments</legend>",
		`name="number_prefix"`, `name="require_attachments"`, `name="urgency_mode"`,
		`name="allow_approver_choice"`, `name="allow_direct_payments"`, `class="action-bar"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("configuration screen is missing %q", want)
		}
	}
	// The self-approval control exists and cannot be switched off.
	if !strings.Contains(body, "Block self-approval") || !strings.Contains(body, "disabled") {
		t.Fatal("the self-approval control must render checked and disabled")
	}
	if strings.Contains(body, `name="block_self_approval"`) {
		t.Fatal("self-approval was rendered as a writable setting; it is structural")
	}

	// Saving writes every posted key in one transaction.
	resp := s.postForm("/configuration", url.Values{
		"number_prefix": {"REQ"}, "number_year_mode": {"financial"}, "number_width": {"5"},
		"require_attachments": {"on"}, "attachment_max_mb": {"20"},
		"urgency_mode": {"free"}, "allow_approver_choice": {""}, "allow_direct_payments": {""},
		"payment_modes": {"NEFT, UPI"},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	settings, err := s.st.AppSettings(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"number_prefix": "REQ", "number_year_mode": "financial", "number_width": "5",
		"require_attachments": "1", "attachment_max_mb": "20", "urgency_mode": "free",
		"allow_approver_choice": "0", "payment_modes": "NEFT, UPI",
	} {
		if settings[k] != want {
			t.Fatalf("app_settings[%q] = %q, want %q", k, settings[k], want)
		}
	}
	// The change is audited (C2).
	audit, err := s.st.Audit(s.ctx, "app_setting", 0, 5)
	if err != nil || len(audit) == 0 {
		t.Fatalf("configuration save was not audited: %#v, %v", audit, err)
	}
	// And it is real: the next request number uses the new format.
	if !strings.Contains(responseBody(t, s.request(http.MethodGet, "/configuration", nil, "")), `value="REQ"`) {
		t.Fatal("the saved prefix is not shown back")
	}

	// An unknown key posted by hand is ignored, not stored.
	resp = s.postForm("/configuration", url.Values{"number_prefix": {"REQ"}, "smtp_password": {"hunter2"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if v, _ := s.st.AppSetting(s.ctx, "smtp_password"); v != "" {
		t.Fatal("an unregistered key was written from the form")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestConfigurationScreen -v`
Expected: FAIL — 404 on `/configuration`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/app/app.go`:

```go
// ConfigField is one control on the Configuration screen. Kind is "text",
// "number", "toggle" or "select".
type ConfigField struct {
	Key, Label, Hint, Kind string
	Options                []ConfigOption
	Span                   int
}

type ConfigOption struct{ Value, Label string }

// ConfigSection is one <fieldset>. Later phases append to configSections and
// touch nothing else — that is the whole point of D6.
type ConfigSection struct {
	Title  string
	Fields []ConfigField
	Note   string
}

var configSections = []ConfigSection{
	{Title: "Request numbering", Fields: []ConfigField{
		{Key: "number_prefix", Label: "Prefix", Kind: "text", Span: 3},
		{Key: "number_year_mode", Label: "Year segment", Kind: "select", Span: 3, Options: []ConfigOption{
			{Value: "calendar", Label: "Calendar year"}, {Value: "financial", Label: "Financial year"}, {Value: "none", Label: "None"}}},
		{Key: "number_width", Label: "Number width", Kind: "number", Span: 3},
	}},
	{Title: "Attachments", Fields: []ConfigField{
		{Key: "require_attachments", Label: "Require a supporting document on every request", Kind: "toggle", Span: 12,
			Hint: "Turning it on does not block submission — it asks for an exception reason when no document is attached, because legitimate documents are sometimes genuinely unavailable."},
		{Key: "attachment_max_mb", Label: "Maximum file size (MB)", Kind: "number", Span: 6},
	}},
	{Title: "Urgency", Fields: []ConfigField{
		{Key: "urgency_mode", Label: "Marking a request urgent", Kind: "select", Span: 6, Options: []ConfigOption{
			{Value: "reason", Label: "Requires a reason"}, {Value: "free", Label: "Free to mark, no reason"}, {Value: "disabled", Label: "Disabled"}}},
	}},
	{Title: "Approvals", Fields: []ConfigField{
		{Key: "allow_approver_choice", Label: "Let the employee choose a different approver", Kind: "toggle", Span: 12,
			Hint: "Off means everyone must use the default approver set on their user record."},
	}, Note: "Self-approval is blocked always. A person can never approve a request they raised, whatever roles they hold."},
	{Title: "Payments", Fields: []ConfigField{
		{Key: "allow_direct_payments", Label: "Allow direct payments without a request", Kind: "toggle", Span: 12,
			Hint: "Off. Every new payment starts from an approved request. Turning this on demands a written reason and is flagged in the audit log."},
		{Key: "payment_modes", Label: "Payment modes offered", Kind: "text", Span: 12},
	}},
}

func (a *App) configuration(w http.ResponseWriter, r *http.Request) {
	settings, err := a.st.AppSettings(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "configuration", PageData{Title: "Configuration", Settings: settings})
}

// configurationSave writes only keys that a registered ConfigField declares, so
// a hand-rolled POST cannot invent settings.
func (a *App) configurationSave(w http.ResponseWriter, r *http.Request) {
	values := map[string]string{}
	for _, section := range configSections {
		for _, f := range section.Fields {
			switch f.Kind {
			case "toggle":
				values[f.Key] = boolSetting(r.FormValue(f.Key))
			default:
				if _, ok := r.Form[f.Key]; ok {
					values[f.Key] = strings.TrimSpace(r.FormValue(f.Key))
				}
			}
		}
	}
	if err := a.st.SetAppSettings(r.Context(), auth.CurrentUser(r), values); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/configuration", http.StatusSeeOther)
}

func boolSetting(v string) string {
	if v == "on" || v == "1" || v == "true" {
		return "1"
	}
	return "0"
}
```

Register `"configSections": func() any { return configSections }` in the `FuncMap`.

Add to `internal/app/templates.go`:

```html
{{define "configuration"}}
{{template "top" .}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">Administration</div>
    <h1>Configuration</h1>
    <p class="sub">The rules the request module runs on. Everything here is data — no code change needed.</p>
  </div>
</section>

<form method="post" action="/configuration">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  {{range configSections}}
  <fieldset>
    <legend>{{.Title}}</legend>
    <div class="form-grid">
      {{range .Fields}}
      <div class="field span-{{.Span}}{{if lt .Span 12}} m-half{{end}}">
        {{if eq .Kind "toggle"}}
          <label class="checkline"><input type="checkbox" name="{{.Key}}" {{if eq (index $.Settings .Key) "1"}}checked{{end}}> {{.Label}}</label>
        {{else if eq .Kind "select"}}
          <label for="cf-{{.Key}}">{{.Label}}</label>
          <select id="cf-{{.Key}}" name="{{.Key}}">
            {{$cur := index $.Settings .Key}}
            {{range .Options}}<option value="{{.Value}}" {{select $cur .Value}}>{{.Label}}</option>{{end}}
          </select>
        {{else}}
          <label for="cf-{{.Key}}">{{.Label}}</label>
          <input id="cf-{{.Key}}" name="{{.Key}}" {{if eq .Kind "number"}}type="number"{{end}} value="{{index $.Settings .Key}}">
        {{end}}
        {{if .Hint}}<span class="hint">{{.Hint}}</span>{{end}}
      </div>
      {{end}}
      {{if eq .Title "Approvals"}}
      <div class="field span-12">
        <label class="checkline"><input type="checkbox" checked disabled> Block self-approval</label>
        <span class="hint">Always on. A person can never approve a request they raised, whatever roles they hold.</span>
      </div>
      <div class="field span-12">
        <label class="checkline"><input type="checkbox" disabled> Second approval above a threshold</label>
        <span class="hint">Reserved for a future version. The status model already has room for it.</span>
      </div>
      {{end}}
    </div>
    {{if .Note}}<p class="hint" style="margin:10px 0 0">{{.Note}}</p>{{end}}
  </fieldset>
  {{end}}

  <div class="action-bar">
    <span class="ab-note d-only">Every change here is written to the audit log.</span>
    <span class="row-end"></span>
    <a class="btn outline" href="/dashboard">Discard</a>
    <button class="btn primary" type="submit">Save configuration</button>
  </div>
</form>
{{template "bottom" .}}
{{end}}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run TestConfigurationScreen -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): configuration screen shell with app_settings-backed sections"
```

---

### Task 31: Screen — `dashboard.html` with `.work-areas`

**Files:**
- Modify: `internal/app/app.go` (`dashboard`, `WorkArea`), `internal/app/templates.go` (`dashboard`)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `auth.Manager.Can`, `store.CountRequests`, `store.ListRequests`, `waitingOn`.
- Produces: `type WorkArea`; `func (a *App) dashboard(w, r)`; template `dashboard`.

**A17 — what changed and why.** The old dashboard was a `.metric-strip` of four counts. `dashboard.html` is a `.metric-strip` **plus** `.work-areas > section.area`, where each area has an `.a-head` (icon, title, count), an `.a-list` of the actual rows a person can click straight into, and an `.a-foot` link to the full queue. The metric strip alone tells you a number; the work area hands you the thing to do. Areas are permission-gated exactly as before, and the strip stays — it is not either/or.

- [ ] **Step 1: Write the failing test**

```go
func TestDashboardShowsGatedWorkAreasWithCounts(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Dash")
	mgrID := seedSecondApprover(t, s)
	mgr, _ := s.st.UserByID(s.ctx, mgrID)
	hash, _ := auth.HashPassword("RequesterPass123")
	rid, _ := s.st.CreateUser(s.ctx, "dashreq@example.test", "Dash Req", hash, "data_entry", true)
	s.assignRole(rid, "Requester")
	requester, _ := s.st.UserByID(s.ctx, rid)
	waiting, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "reimbursement", ShortTitle: "Team lunch", ProjectID: 1, HeadID: headID, Amount: 1000,
		Purpose: "p", ExpenseDate: "2026-07-17", ManagerID: mgrID})
	if err != nil {
		t.Fatal(err)
	}
	sentBack, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "reimbursement", ShortTitle: "Cab receipts", ProjectID: 1, HeadID: headID, Amount: 2000,
		Purpose: "p", ExpenseDate: "2026-07-18", ManagerID: mgrID})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.ReturnRequest(s.ctx, mgr, sentBack, "attach the receipt"); err != nil {
		t.Fatal(err)
	}

	// --- Requester. ---
	s.login("dashreq@example.test", "RequesterPass123")
	body := responseBody(t, s.request(http.MethodGet, "/dashboard", nil, ""))
	for _, want := range []string{
		`class="work-areas"`, `class="area"`, `class="a-head"`, `class="a-list"`, `class="a-foot"`,
		`class="metric-strip"`, "Needs your action", "Cab receipts", `class="pill returned"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("requester dashboard is missing %q", want)
		}
	}
	if strings.Contains(body, "Administration") {
		t.Fatal("requester dashboard must not show Administration")
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("the dashboard still renders .badge")
	}
	// The area rows link straight to the thing to do.
	if !strings.Contains(body, `href="/requests/`+strconvFormat(sentBack)+`"`) {
		t.Fatal("the needs-action area does not link to the request")
	}
	_ = waiting

	// --- Approver sees their own areas. ---
	s.login("kavita@example.test", "ApproverPass123")
	body = responseBody(t, s.request(http.MethodGet, "/dashboard", nil, ""))
	for _, want := range []string{"Awaiting your approval", "Team lunch", `href="/approvals"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("approver dashboard is missing %q", want)
		}
	}

	// --- Admin sees Configuration. ---
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body = responseBody(t, s.request(http.MethodGet, "/dashboard", nil, ""))
	for _, want := range []string{"Configuration", `href="/configuration"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin dashboard is missing %q", want)
		}
	}
}

// D2: a Requester-only session is permission-gated out of admin routes by URL,
// complementing the dashboard work-area gating.
func TestRequesterOnlySessionForbiddenFromAdminRoutesByURL(t *testing.T) {
	s := newAppTestServer(t)
	hash, _ := auth.HashPassword("RequesterPass123")
	rid, _ := s.st.CreateUser(s.ctx, "gated@example.test", "Gated Req", hash, "data_entry", true)
	s.assignRole(rid, "Requester")
	s.login("gated@example.test", "RequesterPass123")
	for _, path := range []string{"/roles", "/users", "/audit", "/configuration", "/approvals"} {
		resp := s.request(http.MethodGet, path, nil, "")
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("GET %s as requester = %d, want 403", path, resp.StatusCode)
		}
		_ = responseBody(t, resp)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run 'TestDashboardShowsGatedWorkAreas|TestRequesterOnlySessionForbiddenFromAdminRoutesByURL' -v`
Expected: FAIL — dashboard body has no `.work-areas`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/app/app.go`:

```go
// WorkArea is one `.area` block: a heading, the rows a person can act on now,
// and a link to the full queue behind them.
type WorkArea struct {
	Key      string
	Icon     string
	Title    string
	Count    int
	Requests []store.Request
	FootText string
	FootHref string
	Links    []NavLink // used by the Configuration area, which has no requests
}

type NavLink struct{ Label, Sub, Href string }

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	ctx := r.Context()
	counts := map[string]int{}
	var areas []WorkArea

	area := func(key, icon, title, footText, footHref string, opts store.RequestListOptions) error {
		opts.ViewerID = u.ID
		opts.Limit = 4
		list, err := a.st.ListRequests(ctx, opts)
		if err != nil {
			return err
		}
		n, err := a.st.CountRequests(ctx, opts)
		if err != nil {
			return err
		}
		counts[key] = n
		if n == 0 {
			return nil
		}
		areas = append(areas, WorkArea{Key: key, Icon: icon, Title: title, Count: n,
			Requests: list, FootText: footText, FootHref: footHref})
		return nil
	}

	if a.auth.Can(u, "request", "create") {
		if err := area("needs-action", "!", "Needs your action", "Open my requests →", "/requests?bucket=needs-me",
			store.RequestListOptions{Scope: "own", Bucket: "needs-me"}); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		if err := area("in-progress", "▤", "In progress", "See all my requests →", "/requests?bucket=open",
			store.RequestListOptions{Scope: "own", Statuses: []string{"pending", "approved"}}); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
	if a.auth.Can(u, "approval", "approve") {
		if err := area("approvals", "✓", "Awaiting your approval", "Open approvals queue →", "/approvals",
			store.RequestListOptions{Scope: "assigned", Statuses: []string{"pending"}}); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		if err := area("decisions", "◐", "Decisions only you can make", "Review all →", "/approvals?bucket=cancellations",
			store.RequestListOptions{Scope: "assigned", Statuses: []string{"cancellation_requested"}}); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
	if a.auth.Can(u, "payment", "process") {
		if err := area("accounts", "₹", "Approved and unclaimed", "Open the queue →", "/requests?bucket=open&status=approved",
			store.RequestListOptions{Scope: "all", Statuses: []string{"approved"}}); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
	if a.auth.Can(u, "config", "view") {
		links := []NavLink{{Label: "Configuration", Sub: "Numbering, attachments, urgency, approvals", Href: "/configuration"}}
		if a.auth.Can(u, "role", "view") {
			links = append(links, NavLink{Label: "Roles & permissions", Sub: "Who can do what", Href: "/roles"})
		}
		if a.auth.Can(u, "user", "view") {
			links = append(links, NavLink{Label: "Users", Sub: "People and default approvers", Href: "/users"})
		}
		if a.auth.Can(u, "vendor", "view") {
			links = append(links, NavLink{Label: "Vendors", Sub: "The vendor master", Href: "/vendors"})
		}
		areas = append(areas, WorkArea{Key: "admin", Icon: "⚙", Title: "Configuration", Links: links})
	}

	a.render(w, r, "dashboard", PageData{Title: "Dashboard", Areas: areas, Counts: counts})
}
```

Add to `internal/app/templates.go`:

```html
{{define "dashboard"}}
{{template "top" .}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">Home</div>
    <h1>Good day, {{.User.Name}}</h1>
    <p class="sub">Everything below is waiting on someone. The ones marked <em>you</em> are yours.</p>
  </div>
  <div class="pb-actions">
    {{if .Perms.Can "request" "create"}}<a class="btn primary" href="/requests/new">＋ New request</a>{{end}}
  </div>
</section>

<div class="greet m-only">
  <h1>Good day, {{.User.Name}}</h1>
</div>

<div class="metric-strip">
  {{if .Perms.Can "request" "create"}}
  <a class="metric{{if index .Counts "needs-action"}} warn{{end}}" href="/requests?bucket=needs-me">
    <span class="metric-label">Needs my action</span>
    <span class="metric-value" data-count="needs-action">{{index .Counts "needs-action"}}</span>
  </a>
  <a class="metric" href="/requests?bucket=open">
    <span class="metric-label">My open requests</span>
    <span class="metric-value" data-count="in-progress">{{index .Counts "in-progress"}}</span>
  </a>
  {{end}}
  {{if .Perms.Can "approval" "approve"}}
  <a class="metric warn" href="/approvals">
    <span class="metric-label">Awaiting my approval</span>
    <span class="metric-value" data-count="approvals">{{index .Counts "approvals"}}</span>
  </a>
  <a class="metric" href="/approvals?bucket=cancellations">
    <span class="metric-label">Cancellation requests</span>
    <span class="metric-value" data-count="decisions">{{index .Counts "decisions"}}</span>
  </a>
  {{end}}
  {{if .Perms.Can "payment" "process"}}
  <a class="metric" href="/requests?status=approved">
    <span class="metric-label">Approved, unclaimed</span>
    <span class="metric-value" data-count="accounts">{{index .Counts "accounts"}}</span>
  </a>
  {{end}}
</div>

<div class="work-areas">
  {{range .Areas}}
  <section class="area">
    <div class="a-head"><span class="a-ico">{{.Icon}}</span><h2>{{.Title}}</h2>{{if .Count}}<span class="n">{{.Count}}</span>{{end}}</div>
    <div class="a-list">
      {{range .Requests}}
      <a href="/requests/{{.ID}}">
        <span class="al-main"><b>{{.Number}} · {{.ShortTitle}}</b>
          <small>
            {{if .Urgent}}<span class="pill urgent">Urgent</span> {{end}}
            <span class="pill {{pillClass .Status}}">{{statusText .Status}}</span>
            {{$w := waitingOn . $.User.ID}}<span class="waiting {{$w.Class}}">{{$w.Text}}</span>
          </small></span>
        <span class="al-amt">{{money .Amount}}</span>
      </a>
      {{end}}
      {{range .Links}}
      <a href="{{.Href}}"><span class="al-main"><b>{{.Label}}</b><small>{{.Sub}}</small></span><span class="al-amt">→</span></a>
      {{end}}
    </div>
    {{if .FootHref}}<a class="a-foot" href="{{.FootHref}}">{{.FootText}}</a>{{end}}
  </section>
  {{else}}
  <section class="area">
    <div class="a-head"><span class="a-ico">✓</span><h2>Nothing is waiting on you</h2></div>
    <div class="a-list"><p class="empty">When something needs you, it appears here.</p></div>
  </section>
  {{end}}
</div>
{{template "bottom" .}}
{{end}}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run 'TestDashboardShowsGatedWorkAreas|TestRequesterOnlySessionForbidden' -v`
Then: `make test-race && make test-cover`
Expected: PASS; full Go suite green.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): dashboard rebuilt on work areas"
```

---

### Task 32: End-to-end browser walk-through (selectors updated for the design system)

**Files:**
- Modify: `internal/app/app_integration_test.go` (approval-through-HTTP test)
- Create: `tests/e2e/requests.spec.ts`
- Test: the two files above

**Interfaces:**
- Consumes: every Phase-2 route; Playwright fixtures (`test`, `expect`, `login`, `admin`).
- Produces: HTTP proof that a manager approves from the detail page and adjusts the amount; a browser walk-through of raise → submit → approve → cancel, on desktop and at 390 px.

**A18 — what the old spec pinned and why it had to change.** The previous Playwright spec asserted `page.locator('.badge')` and drove the form with `getByLabel('Type')`. D5 renamed `.badge` to `.pill` everywhere, and A16 replaced the type `<select>` with a route parameter, so both selectors now match nothing. The replacements below use the design's real anatomy: `.pill`, `.type-card`, `.req-card`, `.action-bar`, `.overlay .sheet`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/app/app_integration_test.go`:

```go
func TestManagerApprovesFromDetailPageAdjustingAmount(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Approve")
	mgrID := seedSecondApprover(t, s)
	vendorID := seedAppVendor(t, s, "Acme Supplies")
	hash, _ := auth.HashPassword("RequesterPass123")
	rid, _ := s.st.CreateUser(s.ctx, "amy@example.test", "Amy", hash, "data_entry", true)
	s.assignRole(rid, "Requester")
	requester, _ := s.st.UserByID(s.ctx, rid)
	reqID, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget",
		Type: "vendor_invoice", ShortTitle: "Server rack", ProjectID: 1, HeadID: headID,
		Amount: 100000, Purpose: "server", ManagerID: mgrID, VendorID: vendorID,
		InvoiceNo: "AC/26-27/0001", InvoiceDate: "2026-07-18"})
	if err != nil {
		t.Fatal(err)
	}

	s.login("kavita@example.test", "ApproverPass123")
	// The detail page exposes the decision sheet for the assigned approver.
	body := responseBody(t, s.request(http.MethodGet, "/requests/"+strconvFormat(reqID), nil, ""))
	if !strings.Contains(body, `action="/requests/`+strconvFormat(reqID)+`/approve"`) {
		t.Fatal("assigned approver cannot see the approve control")
	}
	resp := s.postForm("/requests/"+strconvFormat(reqID)+"/approve", url.Values{"approved_amount": {"900.00"}, "note": {"trim"}})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	got, _ := s.st.Request(s.ctx, reqID)
	if got.Status != "approved" || got.ApprovedAmount == nil || *got.ApprovedAmount != 90000 {
		t.Fatalf("approved = %+v", got)
	}
}
```

Create `tests/e2e/requests.spec.ts`:

```ts
import { test, expect, capturePageErrors, login, admin } from './fixtures';

test.describe('payment requests', () => {
  test('an employee raises a vendor invoice and the manager approves it', async ({ page }) => {
    const errors = capturePageErrors(page);
    await login(page, admin.email, admin.password);

    // Masters the form needs.
    await page.goto('/projects');
    await page.getByLabel('Project name').fill('Ops E2E');
    await page.getByRole('button', { name: 'Add Project' }).click();
    await page.goto('/heads');
    await page.getByLabel('Project').selectOption({ index: 0 });
    await page.getByLabel('Head name').fill('Hardware');
    await page.getByRole('button', { name: 'Add Head' }).click();
    await page.goto('/vendors/new');
    await page.getByLabel('Name').fill('Sundaram Electricals Pvt Ltd');
    await page.getByRole('button', { name: /Save/ }).click();

    // A16: the type is chosen by navigating, not by a select.
    await page.goto('/requests/new');
    await expect(page.locator('.type-grid .type-card')).toHaveCount(4);
    await page.locator('.type-card', { hasText: 'Vendor invoice payment' }).click();
    await expect(page).toHaveURL(/type=vendor_invoice/);

    await page.getByLabel('Short title').fill('July switchgear supply');
    await page.getByLabel('Project', { exact: true }).selectOption({ index: 1 });
    await page.getByLabel('Head', { exact: true }).selectOption({ index: 1 });
    await page.getByLabel('Amount').fill('100000');
    // The money field spells the amount back (Phase 0 Task 19).
    await page.getByLabel('Amount').blur();
    await expect(page.locator('.money-field .in-words')).toContainText(/lakh/i);
    await page.getByLabel('Vendor', { exact: true }).fill('Sundaram');
    await page.locator('.combo-list .co').first().click();
    await page.getByLabel('Invoice number').fill('SE/26-27/1184');
    await page.getByLabel('Invoice date').fill('2026-07-18');
    await page.getByLabel('Purpose').fill('11kV switchgear panels');
    await page.getByLabel('Approver').selectOption({ index: 1 });
    await page.locator('.action-bar').getByRole('button', { name: 'Submit request' }).click();

    // D1: it is numbered and pending straight away — there was never a draft.
    await expect(page.locator('.banner.good')).toContainText(/Submitted/);
    await expect(page.locator('.req-head .rh-no')).toContainText(/^PR-/);
    await expect(page.locator('.req-head .pill')).toContainText('Awaiting approval');
  });

  test('the approver approves from the sheet and the status pill follows', async ({ page }) => {
    const errors = capturePageErrors(page);
    await login(page, admin.email, admin.password);
    await page.goto('/approvals');
    await page.locator('.req-card').first().click();

    await expect(page.locator('.req-head .pill')).toContainText('Awaiting approval');
    await page.locator('.action-bar').getByRole('button', { name: /^Approve/ }).click();
    await expect(page.locator('#approve-sheet')).toBeVisible();
    await page.locator('#approve-sheet').getByRole('button', { name: 'Approve request' }).click();

    await page.goto('/requests');
    await expect(page.locator('.req-card .pill').first()).toContainText('Approved');
    expect(errors).toEqual([]);
  });

  test('the whole journey works on a 390px viewport', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 850 });
    const errors = capturePageErrors(page);
    await login(page, admin.email, admin.password);

    await page.goto('/requests');
    // The mobile chrome is present and does not swallow the content.
    await expect(page.locator('.tabbar')).toBeVisible();
    await expect(page.locator('.m-filters')).toBeVisible();
    await page.locator('.filter-btn').click();
    await expect(page.locator('#filter-sheet .sheet')).toBeVisible();
    await page.locator('#filter-sheet [data-close="filter-sheet"]').first().click();
    await expect(page.locator('#filter-sheet .sheet')).toBeHidden();

    // No horizontal overflow anywhere on the journey.
    for (const path of ['/dashboard', '/requests', '/requests/new', '/approvals']) {
      await page.goto(path);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
      expect(overflow, `horizontal overflow at ${path}`).toBe(false);
    }
    expect(errors).toEqual([]);
  });

  test('a duplicate warning appears and does not block the submit', async ({ page }) => {
    const errors = capturePageErrors(page);
    await login(page, admin.email, admin.password);
    await page.goto('/requests/new?type=vendor_invoice');
    await page.getByLabel('Short title').fill('July switchgear supply again');
    await page.getByLabel('Project', { exact: true }).selectOption({ index: 1 });
    await page.getByLabel('Head', { exact: true }).selectOption({ index: 1 });
    await page.getByLabel('Vendor', { exact: true }).fill('Sundaram');
    await page.locator('.combo-list .co').first().click();
    await page.getByLabel('Amount').fill('100000');
    await page.getByLabel('Amount').blur();
    await expect(page.locator('#dup-check .banner.warn')).toBeVisible();

    await page.getByLabel('Invoice number').fill('SE/26-27/1190');
    await page.getByLabel('Invoice date').fill('2026-07-19');
    await page.getByLabel('Purpose').fill('second batch');
    await page.getByLabel('Approver').selectOption({ index: 1 });
    await page.locator('.action-bar').getByRole('button', { name: 'Submit request' }).click();
    // G6: warned, not blocked.
    await expect(page.locator('.banner.good')).toContainText(/Submitted/);
    expect(errors).toEqual([]);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/app/ -run TestManagerApprovesFromDetailPage -v`
Expected: PASS already if Tasks 19–31 are done — if the decision sheet or the approve route is missing, FAIL. Then:
Run: `make test-e2e` (or `npx playwright test tests/e2e/requests.spec.ts`)
Expected: FAIL on the first run wherever a label or a class does not match; iterate until green.

- [ ] **Step 3: Write minimal implementation**

No new production code should be required if Tasks 19–31 are correct. Where the browser run reveals a gap, fix the **template**, not the test: the labels the spec drives (`Short title`, `Project`, `Head`, `Amount`, `Vendor`, `Invoice number`, `Invoice date`, `Purpose`, `Approver`) are the labels the mockups use, and the classes (`.type-card`, `.req-card`, `.pill`, `.money-field .in-words`, `.action-bar`, `.overlay .sheet`, `.tabbar`, `.m-filters`) are the Phase 0 component names. If a selector does not match, the markup has drifted from the design system.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/app/ -run TestManagerApprovesFromDetailPage -v`
Run: `make test-e2e`
Expected: PASS both.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app_integration_test.go tests/e2e/requests.spec.ts
git commit -m "test: manager approval HTTP and request workflow e2e on the design system"
```

---

### Task 33: Full-suite verification

**Files:** none (verification only).

- [ ] **Step 1: Run the whole gate**

Run: `make test-race`
Expected: `ok  fervidbudget/internal/store` and `ok  fervidbudget/internal/app` (no failures, no data races).

- [ ] **Step 2: Coverage + vet + typecheck + e2e**

Run: `make test-all`
Expected: `go vet` clean; race + cover green; `npm run typecheck` clean; Playwright suite (including `requests.spec.ts`) green.

- [ ] **Step 3: Design-system conformance sweep**

```bash
# D5: no template may reference the old .badge class.
grep -c 'class="badge' internal/app/templates.go            # expect 0
# D1: the word never reaches a screen, and is never written as a status.
grep -ic 'draft' internal/app/templates.go                  # expect 0
grep -c '"draft"' internal/store/requests.go                # expect 0 — no status literal, only comments explaining its absence
# A16: the request type only ever appears as a hidden input carrying the route
# parameter — request_form, request_edit and request_returned, three in all.
grep -c 'type="hidden" name="type"' internal/app/templates.go  # expect 3
grep -c '<select id="rtype"' internal/app/templates.go         # expect 0
# Every Phase-2 mockup has a rendered route (definition-of-done §6).
for name in request_new_type request_form request_submitted requests approvals \
            request_detail request_edit request_returned request_cancel \
            request_cancellation configuration dashboard; do
  grep -q "{{define \"$name\"}}" internal/app/templates.go || echo "MISSING template: $name"
done
```

- [ ] **Step 4: Commit (if any incidental fixups were needed)**

```bash
git add internal/store internal/app web/static tests/e2e
git commit -m "chore: phase-2 request workflow full-suite green"
```

---

## Coverage

| ID | Requirement | Task | Test name |
|---|---|---|---|
| T1 | Type first (a route), then the fields that type needs | 20, 21 | `TestRequestNewTypeChooser` / `TestRequestFormIsAdaptiveAndServerAuthoritative` |
| T2 | Type decides required fields + payee | 3 | `TestValidateRequestInputPerType` |
| T3 | Bank details excluded from every request screen | 21 | `TestRequestFormIsAdaptiveAndServerAuthoritative` (no bank/IFSC) |
| T4 | Mobile-first request form on the design system | 21, 32 | `TestRequestFormIsAdaptiveAndServerAuthoritative` / `requests.spec.ts` 390 px |
| T5 | Always-captured fields incl. urgent + reason | 5, 6 | `TestCreateRequestIsAtomicCreateAndSubmit` / `TestUrgencyReasonIsRequiredWhenUrgent` |
| T6 | Vendor-invoice fields (vendor row, invoice no + date) | 3 | `TestValidateRequestInputPerType` |
| T7 | Vendor-advance fields (vendor row + advance reason) | 3 | `TestValidateRequestInputPerType` |
| T8 | Reimbursement fields (payee = self, expense date) | 3, 5 | `TestValidateRequestInputPerType` / `TestCreateRequestIsAtomicCreateAndSubmit` |
| T9 | Employee advance, budget or recoverable | 3 | `TestValidateRequestInputPerType` |
| T10 | Attachments optional; admin toggle to mandatory | 4, 7 | `TestAppSettingRoundTripAndDefault` / `TestAttachmentPolicyAsksForAReasonInsteadOfBlocking` |
| T11 | Reimbursement / employee advance payee = the requester | 5 | `TestCreateRequestIsAtomicCreateAndSubmit` |
| T12 | Inactive projects/heads hidden from new, shown on historical | 5 | `TestRequestRetainsHistoricalProjectHead` |
| A1 | Requester picks the approver | 8, 21 | `TestSelfApprovalIsRejectedAndNeverOffered` / `TestRequestFormIsAdaptiveAndServerAuthoritative` |
| A2 | Approve; may adjust the amount | 12, 32 | `TestApproveRequestAdjustsAmountAndIsAssignedOnly` / `TestManagerApprovesFromDetailPageAdjustingAmount` |
| A3 | Reject with a required reason | 13, 26 | `TestReturnAndRejectRequireTextAndAreAssignedOnly` / `TestRequestDetailIsOneScreenWithPermissionGatedActions` |
| A4 | Return with a required comment | 13, 28 | `TestReturnAndRejectRequireTextAndAreAssignedOnly` / `TestReturnedRequestScreenCorrectsAndResubmits` |
| A5 | Managers see all; approve only what is assigned | 12, 17, 25 | `TestApproveRequestAdjustsAmountAndIsAssignedOnly` / `TestListRequestsByScopeAndCount` / `TestApprovalsQueueIsItsOwnScreen` |
| A6 | No bulk approval | 19, 25 | `TestRequestRoutesAreRegisteredAndScoped` (404/405) / `TestApprovalsQueueIsItsOwnScreen` (no checkboxes) |
| A7 | Admin reassign (reason + history) | 14 | `TestReassignAndReraise` |
| A8 | Edit-while-pending: history + reroute + reminder reset | 10, 27 | `TestUpdateRequestPendingReroutesAndResetsReminder` / `TestRequestEditScreenAndReroute` |
| Q1 | Edit and resubmit a returned request, keeping its number | 13, 28 | `TestReturnAndRejectRequireTextAndAreAssignedOnly` / `TestReturnedRequestScreenCorrectsAndResubmits` |
| Q2 | Withdraw a pending request | 11 | `TestWithdrawRequestFromPending` |
| Q3 | Re-raise a rejected request as a fresh **pending** one | 14 | `TestReassignAndReraise` |
| Q5 | Requester sees only their own requests | 17, 19, 24 | `TestListRequestsByScopeAndCount` / `TestRequestRoutesAreRegisteredAndScoped` (403) / `TestRequestsListRendersCardsTabsAndWaitingLine` |
| **L1** | **No draft state exists** (inverted by D1; proof of absence) | 1, 3, 5 | `TestMigrationV3CreatesRequestTables` (CHECK rejects `draft`) / `TestCanTransition` / `TestNoDraftStateExists` |
| L2 | Pending approval | 5 | `TestCreateRequestIsAtomicCreateAndSubmit` |
| L3 | Returned | 13 | `TestReturnAndRejectRequireTextAndAreAssignedOnly` |
| L4 | Rejected — final, read-only | 13 | `TestReturnAndRejectRequireTextAndAreAssignedOnly` (resubmit fails) |
| L5 | Withdrawn | 11 | `TestWithdrawRequestFromPending` |
| L6 | Approved (to pay) | 12 | `TestApproveRequestAdjustsAmountAndIsAssignedOnly` |
| L11 | Legal transition enforcement | 3, 5 | `TestCanTransition` / `TestSubmitRequestResubmitsAReturnedRequest` (double submit) |
| N1 | In-app work queues by default | 31 | `TestDashboardShowsGatedWorkAreasWithCounts` |
| N7 | Conversation visible to every viewer | 16, 26 | `TestRequestThreadMergesEventsCommentsAndFiles` / `TestRequestDetailIsOneScreenWithPermissionGatedActions` |
| D1 | One unified dashboard with work areas | 31 | `TestDashboardShowsGatedWorkAreasWithCounts` |
| D2 | Dashboard areas permission-gated; admin routes 403 by URL | 31 | `TestRequesterOnlySessionForbiddenFromAdminRoutesByURL` |
| D3 | Export lists to CSV by permission | 19 | `TestRequestRoutesAreRegisteredAndScoped` (header + scope) |
| D4 | Queues show counts | 24, 25, 31 | `TestRequestsListRendersCardsTabsAndWaitingLine` / `TestApprovalsQueueIsItsOwnScreen` / `TestDashboardShowsGatedWorkAreasWithCounts` |
| D5 | Copy-previous-request NOT built (negative) | 19, 21 | `TestRequestRoutesAreRegisteredAndScoped` (`/copy` 404) / `TestRequestFormIsAdaptiveAndServerAuthoritative` |
| C2 | Audit history for every request mutation | 5, 14, 15, 30 | `TestCreateRequestIsAtomicCreateAndSubmit` / `TestReassignAndReraise` / `TestCancellationRequestAcceptAndDecline` / `TestConfigurationScreenReadsAndWritesAppSettings` |
| C3 | Currency as int64 paise; formats via `money.FormatPaise` | 5 | `TestCreateRequestIsAtomicCreateAndSubmit` |
| C4 | Request number auto, unique, monotonic per year | 1, 2 | `TestMigrationV3CreatesRequestTables` (UNIQUE) / `TestNextRequestNumberIsMonotonicPerYear` |
| X1 | No tax/TDS columns, tables or routes (negative) | 19 | `TestNoTaxColumnsInSchema` |
| **G1** | Post-approval cancellation: employee asks, payment freezes, approver decides | 15, 29 | `TestCancellationRequestAcceptAndDecline` / `TestCancellationFreezesTheApprovedState` / `TestCancellationScreensEndToEnd` |
| **G2** | Approver cancels an approved request outright, with a reason | 15, 29 | `TestCancellationRequestAcceptAndDecline` / `TestCancellationScreensEndToEnd` |
| **G3** | Statuses `cancellation_requested` and `cancelled` | 1, 3 | `TestMigrationV3CreatesRequestTables` / `TestCanTransition` |
| **G4** | No drafts | 1, 3, 5 | `TestNoDraftStateExists` |
| **G5** | Vendor master consumed: `vendor_id` FK, payee snapshot retained | 1, 3, 5 | `TestMigrationV3CreatesRequestTables` / `TestValidateRequestInputPerType` / `TestCreateRequestWritesStagedAttachments` |
| **G6** | Duplicate warning that never blocks | 18, 23 | `TestSimilarRequestsFindsRecentNearDuplicatesOnly` / `TestDuplicateCheckWarnsAndNeverBlocks` |
| **G7** | Urgency reason required when urgent is ticked | 6 | `TestUrgencyReasonIsRequiredWhenUrgent` |
| **G8** | Self-approval blocked; approver list excludes the requester | 8, 12, 21 | `TestSelfApprovalIsRejectedAndNeverOffered` / `TestApproveRequestAdjustsAmountAndIsAssignedOnly` / `TestRequestFormIsAdaptiveAndServerAuthoritative` |
| **G9** | Default approver per employee, pre-selected and overridable | 9, 21 | `TestDefaultApproverRoundTripAndGuards` / `TestRequestFormIsAdaptiveAndServerAuthoritative` |
| **G10** | Attachment exception reason when the flag is on | 7 | `TestAttachmentPolicyAsksForAReasonInsteadOfBlocking` |
| **G21** | One consolidated Configuration screen (shell + Phase-2 sections) | 30 | `TestConfigurationScreenReadsAndWritesAppSettings` |
| **DS1** | `.req-card`, `.pill`, `.waiting` render; no `.badge` survives | 24, 26 | `TestRequestsListRendersCardsTabsAndWaitingLine` / `TestRequestDetailIsOneScreenWithPermissionGatedActions` |
| **DS2** | History and conversation are one merged `.thread` | 16, 26 | `TestRequestThreadMergesEventsCommentsAndFiles` / `TestRequestDetailIsOneScreenWithPermissionGatedActions` |
| **DS3** | `.overlay > .sheet` for approve / return / reject / cancellation | 26, 29 | `TestRequestDetailIsOneScreenWithPermissionGatedActions` / `TestCancellationScreensEndToEnd` |
| **DS4** | `.segmented` tabs and `.m-filters` + filter sheet | 24, 25, 32 | `TestRequestsListRendersCardsTabsAndWaitingLine` / `TestApprovalsQueueIsItsOwnScreen` / `requests.spec.ts` |
| **DS5** | `.work-areas` dashboard, not a bare metric strip | 31 | `TestDashboardShowsGatedWorkAreasWithCounts` |
| **DS6** | htmx fragments render without the shell | 19, 21, 23 | `TestRenderPartialOmitsTheShell` / `TestRequestFormIsAdaptiveAndServerAuthoritative` / `TestDuplicateCheckWarnsAndNeverBlocks` |

---

## Self-review

- **Amendment coverage.** Every row of the amendment log lands in a numbered task: A1–A5 in Task 1, A6 in Task 5 (with 10, 11 and 14 following it through), A7 in the coverage table's L1 row, A8 in 15 and 29, A9 in 18 and 23, A10 in 8, A11 in 9, A12 in 6, A13 in 7, A14 in 30, A15 in 19–29, A16 in 20 and 21, A17 in 31, A18 in 32, A19 across 24–29 and 31, A20 in 2, A21 in 1 and 3.
- **Task count.** 33 tasks: 18 store (1–18), 14 app (19–32) and one verification gate (33). The old plan had 17.
- **Type consistency.** `Request`, `RequestInput`, `RequestListOptions`, `ThreadEntry`, `ThreadChange` and `SimilarRequestOptions` are defined once (Tasks 3, 16, 18) and consumed unchanged thereafter. `PermissionSet`, `Shell` and `.Perms` come from Phase 0/1 and are never redefined here. `Waiting`, `WorkArea`, `ConfigSection` and `ConfigField` are app-layer view types and never cross into the store.
- **Dependency order.** 1 → 2 → 3 → 4 → 5 is a hard chain. 6, 7, 8 and 9 each extend 5 and can run in parallel with one another. 10–18 depend on 5 only. 19 must precede 20–31. 32 depends on everything. Nothing in 1–18 touches `internal/app`, so the store and the screens can be worked by different agents once 19 has landed the route table.
- **What Phase 3 must honour, decided here.** The payment freeze is the `cancellation_requested` status, not a flag: every Phase-3 reservation and payment query must filter on `status = 'approved'` exactly, never on "not rejected". `TestCancellationFreezesTheApprovedState` asserts it from this side.
- **What Phase 4 must honour, decided here.** `recoverable_category TEXT` holds the code; `recoverable_category_id` stays NULL until Phase 4's v5 migration back-fills it and replaces `recoverableCategoryRules` with rows from `recoverable_categories`. The Recoverable-categories fieldset appends to `configSections` rather than restoring the standalone page.
- **Resolved (2026-07-25).** `users.default_approver_id` is owned by **Phase 1** (migration v1, Task 12), which also ships `User.DefaultApproverID` and `SetUserDefaultApprover`. This phase's `columnExists`-guarded `ALTER` stays as a no-op safety net so the plan remains runnable standalone, and Task 9 calls Phase 1's accessors rather than declaring its own. See the resolved cross-phase notes at the top of this plan.









