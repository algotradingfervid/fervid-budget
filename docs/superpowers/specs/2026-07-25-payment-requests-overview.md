# Payment Requests — Overview Spec (Foundation)

**Status:** Approved design → building. **Source of truth for scope:** `docs/payment-requests-design.html` (the reviewed "understanding" document). This overview fixes the data model, migration approach, permission vocabulary, cross-cutting conventions, and the per-phase interface contracts that all phase specs and plans build on. Coverage of every design decision is tracked in `2026-07-25-payment-requests-coverage.md`.

**Currency:** ₹ INR, stored as integer paise (`int64`), formatted via `internal/money`.

---

## 1. Goals & non-goals

**Goal:** Add a request → approval → payment-linking → settlement workflow in front of the existing payment ledger, governed by an admin-managed, per-screen, data-level permission system.

**In scope:** fine-grained roles/permissions; the request lifecycle; treatment-first request types; recoverable payments; one-request→one-payment linking with explicit settlement; per-event email + reminders; a unified role-aware dashboard; mobile-first request form.

**Out of scope (must not be built):** tax/TDS calculation; recoverable *repayment* tracking; refund / return-of-money; forfeiture / write-off conversion; **any direct request-less payment path** (every future payment links to an approved request; pre-existing payments stay as historical records with `request_id IS NULL`).

---

## 2. Phase map

Each phase produces working, tested software and has its own spec + TDD plan. Dependencies flow downward.

| Phase | Deliverable | Depends on |
|---|---|---|
| **1 — RBAC foundation** | Migration runner; `roles`, `role_permissions`, `role_data_scope`, `user_roles`; 4 seeded system roles; DB-driven multi-role enforcement with data scope; admin Roles matrix + create-by-copy; multi-role user assignment. | — |
| **2 — Request workflow** | `payment_requests` (+ attachments, comments); `app_settings` (key/value, with `require_attachments`); auto number; treatment→type form (mobile-first); submit/edit/withdraw/re-raise; manager approve/adjust/return/reject/reassign; conversation thread; unified dashboard + My-requests / Approvals queues. | 1 |
| **3 — Linking & settlement** | `payments.request_id` + settlement columns; atomic reservation (Processing) from both entry points; On-hold; settlement popup ("Payment settled" → Completed, "Partial settlement" → Partial-review); manager accept / raise-concern; **enforce every new payment links to an approved request**; Accounts "To pay" queue + dropdown search. | 1, 2 |
| **4 — Recoverables** | `recoverable_categories` (admin-configurable); recoverable treatment path + per-category field rules; exclusion from budget actuals; Recoverable Payments report; close-on-payment. | 1, 2, 3 |
| **5 — Notifications engine** | extends `app_settings` (SMTP + management keys, admin screen), `notification_settings` per event; SMTP sender + templates; approval email; urgent emails; reminder scheduler (pending > 3 calendar days daily; processing > 1 calendar day); in-app notification surfacing. | 1, 2, 3 |

---

## 3. Data model

All new schema is applied by the migration runner (§4) on **one global monotonic sequence**, one version per phase and no collisions: **v1** = Phase 1 (permission tables + seed 4 system roles + back-fill `user_roles`, all in a single migration), **v2** = Phase 2 (`payment_requests`, `request_attachments`, `request_comments`, `request_number_seq`, `app_settings`), **v3** = Phase 3 (`payments` column adds + partial unique index), **v4** = Phase 4 (`recoverable_categories` + seed), **v5** = Phase 5 (`notification_settings`). Column notes use SQLite types. Money = INTEGER paise. Booleans = INTEGER 0/1. Dates = TEXT `YYYY-MM-DD`; timestamps = DATETIME.

### Phase 1 — permissions
```sql
CREATE TABLE roles (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  is_system INTEGER NOT NULL DEFAULT 0,   -- 1 = seeded, cannot be deleted or renamed
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_roles_name_nocase ON roles(lower(name));

CREATE TABLE role_permissions (         -- one row per granted (resource, action)
  role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  resource TEXT NOT NULL,
  action TEXT NOT NULL,
  PRIMARY KEY(role_id, resource, action)
);

CREATE TABLE role_data_scope (          -- data scope per scoped resource
  role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  resource TEXT NOT NULL,               -- 'request' | 'payment'
  scope TEXT NOT NULL,                  -- 'own' | 'assigned' | 'all'
  PRIMARY KEY(role_id, resource)
);

CREATE TABLE user_roles (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  PRIMARY KEY(user_id, role_id)
);
```
Legacy `users.role` is retained but no longer authoritative; the migration seeds `user_roles` from it. Effective access = union of a user's roles' grants; effective scope for a resource = the **broadest** scope among the user's roles (`all` > `assigned` > `own`).

### Phase 2 — requests
```sql
CREATE TABLE payment_requests (
  id INTEGER PRIMARY KEY,
  number TEXT NOT NULL UNIQUE,               -- PR-YYYY-NNNNNN
  status TEXT NOT NULL DEFAULT 'draft',       -- see §6 statuses
  treatment TEXT NOT NULL,                    -- 'budget' | 'recoverable'
  type TEXT NOT NULL,                         -- see §6 types
  recoverable_category_id INTEGER REFERENCES recoverable_categories(id), -- Phase 4; nullable
  project_id INTEGER REFERENCES projects(id), -- nullable per type rules
  head_id INTEGER REFERENCES heads(id),       -- nullable per type rules
  amount INTEGER NOT NULL,                     -- requested paise
  purpose TEXT NOT NULL,
  needed_by TEXT,                              -- YYYY-MM-DD, nullable
  vendor_payee TEXT NOT NULL DEFAULT '',
  counterparty TEXT NOT NULL DEFAULT '',       -- recoverable ICD etc.
  expected_return_date TEXT,                   -- recoverable, nullable
  repayment_notes TEXT NOT NULL DEFAULT '',
  urgent INTEGER NOT NULL DEFAULT 0,
  requester_id INTEGER NOT NULL REFERENCES users(id),
  manager_id INTEGER NOT NULL REFERENCES users(id),      -- current approver
  approved_amount INTEGER,                     -- set on approval
  approved_by INTEGER REFERENCES users(id),
  approved_at DATETIME,
  decision_reason TEXT NOT NULL DEFAULT '',    -- return/reject/reassign reason
  on_hold INTEGER NOT NULL DEFAULT 0,
  hold_reason TEXT NOT NULL DEFAULT '',
  processing_by INTEGER REFERENCES users(id),  -- reservation (Phase 3)
  processing_at DATETIME,
  reminder_last_sent DATETIME,                 -- Phase 5 scheduler bookkeeping
  submitted_at DATETIME,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_requests_status ON payment_requests(status);
CREATE INDEX idx_requests_manager ON payment_requests(manager_id, status);
CREATE INDEX idx_requests_requester ON payment_requests(requester_id, status);

CREATE TABLE request_attachments (
  id INTEGER PRIMARY KEY,
  request_id INTEGER NOT NULL REFERENCES payment_requests(id),
  original_name TEXT NOT NULL, stored_path TEXT NOT NULL,
  mime_type TEXT, size_bytes INTEGER NOT NULL,
  uploaded_by INTEGER NOT NULL REFERENCES users(id),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_request_attachments_request ON request_attachments(request_id);

CREATE TABLE request_comments (
  id INTEGER PRIMARY KEY,
  request_id INTEGER NOT NULL REFERENCES payment_requests(id),
  author_id INTEGER NOT NULL REFERENCES users(id),
  body TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_request_comments_request ON request_comments(request_id);

CREATE TABLE request_number_seq (            -- per-year monotonic counter
  year TEXT PRIMARY KEY, last INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '');
-- Introduced here (Phase 2 v2) with the `require_attachments` key and the
-- AppSetting/SetAppSetting accessors. Phase 5 (v5) extends it with the SMTP +
-- management keys and the admin settings screen — no new table, just new keys.
```
Status/edit **history** is recorded in the existing `audit_log` with `entity_type='payment_request'`. The conversation is `request_comments`.

The Go `store.Request` struct includes the reservation/scheduler fields that map to the columns above: `ProcessingBy *int64`, `ProcessingAt *time.Time`, `ReminderLastSent *time.Time`, `SubmittedAt *time.Time` (all nullable → pointers). Phase 2's `requestSelect` selects `r.processing_by, r.processing_at, r.reminder_last_sent, r.submitted_at` and `scanRequest` scans them as pointers, so the columns are populated from Phase 2 onward even though `processing_by`/`processing_at` are only written by Phase 3 and `reminder_last_sent` by Phase 5.

### Phase 3 — linking & settlement (columns added to `payments`)
```sql
ALTER TABLE payments ADD COLUMN request_id INTEGER REFERENCES payment_requests(id); -- NULL for historical
ALTER TABLE payments ADD COLUMN settlement TEXT NOT NULL DEFAULT '';  -- 'settled' | 'partial'
ALTER TABLE payments ADD COLUMN partial_reason TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX idx_payments_request ON payments(request_id) WHERE request_id IS NOT NULL; -- one payment per request
```

### Phase 4 — recoverables
```sql
CREATE TABLE recoverable_categories (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  requires_project INTEGER NOT NULL DEFAULT 0,
  requires_counterparty INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_recoverable_categories_name_nocase ON recoverable_categories(lower(name));
```
Seeded: Employee advance (proj optional), EMD (proj req), PBG (proj req), ICD (counterparty req), Security deposit, Other.

### Phase 5 — notifications
```sql
-- app_settings already exists (introduced in Phase 2, v2). Phase 5 does NOT
-- create it; it adds these keys and the admin settings screen:
--   smtp_host, smtp_port, smtp_username, smtp_from_name, smtp_from_addr, management_recipients

CREATE TABLE notification_settings (
  event TEXT PRIMARY KEY,                 -- see §6 notification events
  email_enabled INTEGER NOT NULL DEFAULT 0,
  to_recipients TEXT NOT NULL DEFAULT '', cc_recipients TEXT NOT NULL DEFAULT '',
  include_requester INTEGER NOT NULL DEFAULT 0,
  include_manager INTEGER NOT NULL DEFAULT 0,
  include_accounts INTEGER NOT NULL DEFAULT 0,
  subject_template TEXT NOT NULL DEFAULT '',
  body_template TEXT NOT NULL DEFAULT ''
);
```
SMTP password is read from env `FERVID_SMTP_PASSWORD` (never stored in DB), consistent with the "no secrets in DB/URL" rule.

---

## 4. Migration strategy (cross-cutting, built first in Phase 1)

The app has **no migration runner** today — `store.Open` execs the idempotent `schemaSQL`. We add a versioned runner without disturbing the baseline:

- Keep `schemaSQL` as the **v0 baseline** (unchanged).
- Add `internal/store/migrations.go` with an ordered `[]migration{version int, name string, up func(*sql.Tx) error}` slice.
- In `Open`, after `schemaSQL`, run `migrate(db)`: read `PRAGMA user_version`; for each migration with `version > current`, run its `up` inside a transaction and set `PRAGMA user_version = version`.
- Column adds guard with a `columnExists(tx, table, col)` helper (reads `PRAGMA table_info`) so re-runs are safe even if `user_version` is out of sync.
- New tables in migrations use `CREATE TABLE IF NOT EXISTS`.
- Each phase appends **exactly one** migration on a single global monotonic sequence; versions are contiguous and never reordered. The canonical mapping is fixed:
  - **v1** — Phase 1: permission tables + seed 4 system roles + back-fill `user_roles` (all in one migration).
  - **v2** — Phase 2: `payment_requests`, `request_attachments`, `request_comments`, `request_number_seq`, `app_settings`.
  - **v3** — Phase 3: `payments` column adds (`request_id`, `settlement`, `partial_reason`) + the partial unique index.
  - **v4** — Phase 4: `recoverable_categories` + seed.
  - **v5** — Phase 5: `notification_settings` (and the SMTP/management keys added to the existing `app_settings`).

**Contract:** `func migrate(db *sql.DB) error`; helper `func columnExists(tx *sql.Tx, table, col string) (bool, error)`.

---

## 5. Permission vocabulary (canonical — Phase 1 defines, all phases consume)

Enforcement is **server-side on the data**, not menu-hiding. A handler wraps `RequirePermission(resource, action)`; scoped list/detail queries additionally filter by the caller's effective scope.

**Resources & actions:**

| Resource | Actions |
|---|---|
| `request` | view, create, edit, withdraw, reraise, comment |
| `approval` | approve, reject, return, reassign, accept_partial |
| `payment` | view, create, edit, void, process, settle, mark_partial, hold |
| `attachment` | view, create |
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
| `audit` | view |
| `backup` | view, create |

**Scoped resources:** `request`, `payment` carry a data scope `own | assigned | all`.
- `own` — requester_id = caller (requests) / entered_by = caller (payments).
- `assigned` — manager_id = caller (requests) / processing_by = caller (payments).
- `all` — no row filter.

**Seeded system roles (defaults):**
- **Requester:** `request`{view(own),create,edit,withdraw,reraise,comment}, `attachment`{view,create}.
- **Manager:** `request`{view(all),comment}, `approval`{approve,reject,return,reassign,accept_partial}, `grid`{view}, `report`{view}.
- **Accounts:** `request`{view(all),comment}, `payment`{view(all),create,edit,void,process,settle,mark_partial,hold}, `attachment`{view,create}, `grid`{view}, `report`{view,export}, `recoverable_report`{view,export}.
- **Admin:** every resource+action, scope `all` (and the only role that manages `role`, `user`, `notification`, `recoverable_category`, `backup`, `audit`).

---

## 6. Canonical enumerations

**Request statuses:** `draft`, `pending`, `returned`, `rejected`, `withdrawn`, `approved`, `processing`, `partial_review`, `completed`. (`on_hold` is a boolean flag on an `approved` request, not a separate status.)

**Legal transitions** (enforced in store): draft→pending; pending→{returned,rejected,withdrawn,approved}; returned→pending; approved↔(on_hold flag); approved→processing; processing→approved (release); processing→{completed,partial_review}; partial_review→completed. `rejected`/`withdrawn`/`completed` are terminal.

**Treatments:** `budget`, `recoverable`.
**Types:** `vendor_invoice`, `vendor_advance`, `reimbursement`, `employee_advance`, `recoverable`.
**Per-type field rules** (validated in store, Phase 2/4):
- `vendor_invoice` (budget): project+head required; vendor required; invoice_no/date via reference fields; attachment optional (config-gated).
- `vendor_advance` (budget): project+head required; vendor required; reason in purpose.
- `reimbursement` (budget): project+head required; payee forced = requester.
- `employee_advance`: if `treatment=budget` → project+head required (non-refundable); if `treatment=recoverable` → category=Employee advance, project optional, requester auto-recorded, expected_return_date+repayment_notes required.
- `recoverable` deposit/loan: category required; EMD/PBG→project required; ICD→counterparty required; expected_return_date+repayment_notes required; excluded from budget actuals.

**Settlement actions (Phase 3):** `settle` → request `completed` (even if paid < approved); `mark_partial` → request `partial_review`. A linked payment is immutable after it is recorded.

**Notification events (Phase 5):** `request_approved` (Accounts + requester + management list), `request_submitted_urgent`, `request_approved_urgent`. Reminders: `reminder_pending` (daily after 3 calendar days), `reminder_processing_stale` (after 1 calendar day). Non-email events surface in in-app queues only.

---

## 7. Cross-cutting conventions (all phases obey)

- **Store methods:** `func (s *Store) X(ctx, actor User, …)`; mutations use `s.db.BeginTx(ctx,nil)` + `defer tx.Rollback()` + `tx.Commit()`; every mutation writes `recordAuditTx(ctx, tx, AuditInput{…, Before, After})`.
- **Errors:** reuse `ErrNotFound, ErrForbidden, ErrValidation, ErrDuplicate, ErrLockedMonth, ErrInactiveHead`; wrap validation as `fmt.Errorf("%w: message", ErrValidation)`; map DB errors via `classify`.
- **Handlers:** register in `App.routes` behind `a.auth.RequirePermission(resource, action, …)`; POST handlers wrapped with `a.withCSRF`; render via templates in `internal/app/templates.go`; user-facing errors via `a.respondError`/`friendly`.
- **Permission gate signature (Phase 1):** `func (m *Manager) RequirePermission(resource, action string, next http.Handler) http.Handler` (unchanged shape; new engine behind it). New helper `func (m *Manager) Can(u store.User, resource, action string) bool` and `func (m *Manager) Scope(u store.User, resource string) string`.
- **Templates:** server-rendered HTML in the existing design system (`web/static/fervid-ds.css`); nav and permission-gated UI are **permission-driven** via `.Perms.Can "resource" "action"` — a method on `PageData.Perms` (`store.PermissionSet`) populated on **every** render (there is no global `can` FuncMap); request form is mobile-first.
- **Testing (TDD, required):** Go unit tests via `newTestStore(t)`; HTTP/integration via `newAppTestServer(t)` (`httptest`); browser e2e in `tests/e2e/*.spec.ts` using the Playwright fixtures. Every task is red→green→commit. Run `make test-race` and `make test-cover`; e2e via `make test-e2e`.
- **Clock injection:** reminder/settlement timing logic takes an injected `now func() time.Time` (or `time.Time`) so tests are deterministic — never call `time.Now()` inside tested branches directly.
- **Commits:** one per task, conventional messages.

---

## 8. Cross-phase interface contracts (so phase plans interlock)

Names later phases depend on. Phase specs must produce these exact signatures.

**Phase 1 (auth/permissions) produces:**
- `store`: `AllRoles(ctx) ([]Role, error)`, `Role(ctx,id) (Role, error)`, `CreateRole(ctx,actor,name,desc string) (int64,error)`, `CopyRole(ctx,actor,srcID int64,name string) (int64,error)`, `UpdateRolePermissions(ctx,actor,roleID int64,grants []Grant,scopes []ScopeGrant) error`, `DeleteRole(ctx,actor,id int64) error`, `SetUserRoles(ctx,actor,userID int64,roleIDs []int64) error`, `UserRoles(ctx,userID int64) ([]Role,error)`, `EffectivePermissions(ctx,userID int64) (PermissionSet, error)`.
- types: `Role{ID,Name,Description,IsSystem}`, `Grant{Resource,Action}`, `ScopeGrant{Resource,Scope}`, `PermissionSet` with `Can(resource,action) bool` and `Scope(resource) string`.
- `auth.Manager`: `Can(u,resource,action) bool`, `Scope(u,resource) string`, updated `RequirePermission`.

**Phase 2 (requests) produces:**
- `store`: `CreateRequest(ctx,actor,RequestInput) (int64,error)`, `UpdateRequest`, `SubmitRequest`, `WithdrawRequest`, `ApproveRequest(ctx,actor,id,approvedAmount int64,note string) error`, `ReturnRequest`, `RejectRequest`, `ReassignRequest`, `AddRequestComment`, `Request(ctx,id) (Request,error)`, `ListRequests(ctx,RequestListOptions) ([]Request,error)`, `NextRequestNumber(tx,year) (string,error)`.
- types: `Request`, `RequestInput`, `RequestListOptions{Scope,ViewerID,Status,Query,…}`.

**Phase 3 (linking/settlement) produces:**
- `store`: `ReserveRequest(ctx,actor,id int64) error` (atomic → processing), `ReleaseRequest(ctx,actor,id int64,confirmed,authorized bool) error` (processing → approved; the handler passes `authorized` after checking `auth.Scope(user,"payment")=="all"`, so releasing another user's reservation is gated), `RecordPaymentForRequest(ctx,actor,requestID int64,in PaymentInput,settlement string,partialReason string,attachment *AttachmentInput) (int64,error)`, `AcceptPartial(ctx,actor,id int64) error`, `RaiseConcern(ctx,actor,id int64,comment string) error`, `HoldRequest`/`UnholdRequest`, `LinkablePaymentRequests(ctx,RequestListOptions) ([]Request,error)`.
- Existing `CreatePayment*` path is gated to require an approved, reserved request.

**Phase 4 (recoverables) produces:**
- `store`: `ListRecoverableCategories`, `UpsertRecoverableCategory`, `RecoverableReport(ctx,…) ([]RecoverableRow,error)`; `Grid`/`Report` updated to exclude payments whose request is recoverable.

**Phase 5 (notifications) produces:**
- `internal/notify`: `Mailer` interface `Send(ctx, msg Message) error`; `Service` with `Notify(ctx, event string, req Request)`; `notification_settings`/`app_settings` store accessors; `RunReminders(ctx, now time.Time)` invoked by a scheduler goroutine in `cmd/server`.

---

## 9. Acceptance criteria (per phase, verified before phase is "done")

- **P1:** admin can create/copy/delete non-system roles, toggle any resource+action and data scope, assign multiple roles; a Requester cannot reach another user's request or any admin route by URL (403), proven by integration test; `make test-race` green.
- **P2:** an employee raises each request type with correct required-field enforcement; auto number is unique/monotonic per year; manager approve/adjust/return/reject/reassign works; edit-while-pending re-notifies + reroutes; conversation persists; queues show correct counts by scope.
- **P3:** both entry points atomically reserve (second concurrent caller is rejected); settlement popup routes to completed vs partial_review correctly; partial accept/raise-concern; a payment cannot be created without an approved+reserved request; linked payment immutable.
- **P4:** recoverable requests validated per category; recoverable payments excluded from grid/report actuals and shown in the recoverable report; categories admin-configurable; request closes on payment.
- **P5:** approval email renders from template to resolved recipients; urgent emails fire on submit/approve; reminders fire per schedule with an injected clock; SMTP password only from env.

---

## 10. Definition of done (whole feature)

All five phase plans executed, every coverage-matrix ID mapped to a passing test, `make test-all` green (vet, race, cover, typecheck, e2e), and the four seeded roles reproduce the behaviour described in the understanding document.
