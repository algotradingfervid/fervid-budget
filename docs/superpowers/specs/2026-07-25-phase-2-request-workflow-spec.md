# Phase 2 — Request Workflow · Spec

**Parent:** `2026-07-25-payment-requests-overview.md`. **Depends on:** Phase 1 (migration runner `migrate`/`columnExists` + `migrations` slice; `auth.Manager.Can`, `auth.Manager.Scope`, `store.EffectivePermissions`; the four seeded roles). **Covers matrix IDs:** T1–T12, A1–A8 (in-app; email is P5), Q1, Q2, Q3, Q5, L1–L6, L11, N1, N7, D1, D2, D3, D4, D5, C2, C3, C4, X1.

**Goal:** Put a request → approval workflow in front of the payment ledger: a mobile-first, treatment-first request form with per-type required-field enforcement; an auto request number `PR-YYYY-NNNNNN` that is unique and monotonic per year; the requester lifecycle (draft, edit/resubmit, submit, withdraw, re-raise); the manager lifecycle (approve with adjustable amount, return with comment, reject with reason, admin reassign); a per-request conversation thread; and one unified, permission-gated dashboard with scoped queues and counts. Recoverable *enforcement/exclusion* is Phase 4 — Phase 2 captures the recoverable fields and validates the budget-expense types fully.

---

## 1. Schema deltas (migration v2)

Phase 1 built the runner. Phase 2 appends migration **v2** (`{Version: 2, Name: "requests", Up: ...}`) to the `migrations` slice in `internal/store/migrations.go`. `Up` creates the five Phase-2 tables and indexes exactly as overview §3 (Phase 2), each with `CREATE TABLE IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS` so re-runs are safe:

- `payment_requests` — all columns of overview §3 P2 (id, number UNIQUE, status DEFAULT 'draft', treatment, type, recoverable_category_id, project_id, head_id, amount, purpose, needed_by, vendor_payee, counterparty, expected_return_date, repayment_notes, urgent, requester_id, manager_id, approved_amount, approved_by, approved_at, decision_reason, on_hold, hold_reason, processing_by, processing_at, reminder_last_sent, submitted_at, created_at, updated_at) plus `idx_requests_status`, `idx_requests_manager`, `idx_requests_requester`.
- `request_attachments` (+ `idx_request_attachments_request`).
- `request_comments` (+ `idx_request_comments_request`).
- `request_number_seq(year TEXT PRIMARY KEY, last INTEGER NOT NULL DEFAULT 0)`.
- `app_settings(key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '')` — the runtime key/value settings store; migration v2 also seeds `require_attachments='0'`. The `AppSetting`/`SetAppSetting` accessors are introduced here (Phase 2, v2 migration); Phase 5 later adds the admin UI toggle that flips `require_attachments`.

`payment_requests.recoverable_category_id` keeps its `REFERENCES recoverable_categories(id)` clause even though `recoverable_categories` is a Phase-4 table. SQLite defers FK resolution — the referenced table need not exist at `CREATE TABLE` time and NULL foreign keys are never enforced — so the column exists, stays NULL, and introduces no Phase-4 dependency until Phase 4 seeds `recoverable_categories`. Status/edit **history** lives in the existing `audit_log` with `entity_type='payment_request'`; the conversation lives in `request_comments`.

---

## 2. Store API (types + methods)

New types in `internal/store/models.go`:

```go
type Request struct {
	ID                    int64
	Number                string
	Status                string
	Treatment             string
	Type                  string
	RecoverableCategoryID *int64
	ProjectID             *int64
	Project               string // joined name; retained even when project inactive
	HeadID                *int64
	Head                  string // joined name; retained even when head inactive
	Amount                int64
	Purpose               string
	NeededBy              string // YYYY-MM-DD, "" if unset
	VendorPayee           string
	Counterparty          string
	ExpectedReturnDate    string // YYYY-MM-DD, "" if unset
	RepaymentNotes        string
	Urgent                bool
	RequesterID           int64
	RequesterName         string
	ManagerID             int64
	ManagerName           string
	ApprovedAmount        *int64
	ApprovedBy            *int64
	ApprovedByName        string
	ApprovedAt            *time.Time
	DecisionReason        string
	OnHold                bool
	HoldReason            string
	ProcessingBy          *int64
	ProcessingAt          *time.Time
	ReminderLastSent      *time.Time
	SubmittedAt           *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type RequestInput struct {
	Treatment             string
	Type                  string
	ProjectID             int64 // 0 = none
	HeadID                int64 // 0 = none
	Amount                int64
	Purpose               string
	NeededBy              string
	VendorPayee           string
	Counterparty          string
	ExpectedReturnDate    string
	RepaymentNotes        string
	Urgent                bool
	ManagerID             int64
	RecoverableCategoryID int64 // 0 = none (captured; Phase 4 validates)
}

type RequestListOptions struct {
	Scope    string // "own" | "assigned" | "all"
	ViewerID int64
	Status   string // "" or "all" = any status; otherwise exact status
	Query    string
	Limit    int
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
```

Methods on `*Store` (exact signatures — the §8 Phase-2 contract; all mutations use `BeginTx`+`defer Rollback`+`recordAuditTx`+`Commit`):

```go
func NextRequestNumber(tx *sql.Tx, year string) (string, error)
func (s *Store) CreateRequest(ctx context.Context, actor User, in RequestInput) (int64, error)
func (s *Store) UpdateRequest(ctx context.Context, actor User, id int64, in RequestInput) error
func (s *Store) SubmitRequest(ctx context.Context, actor User, id int64) error
func (s *Store) WithdrawRequest(ctx context.Context, actor User, id int64) error
func (s *Store) ApproveRequest(ctx context.Context, actor User, id, approvedAmount int64, note string) error
func (s *Store) ReturnRequest(ctx context.Context, actor User, id int64, comment string) error
func (s *Store) RejectRequest(ctx context.Context, actor User, id int64, reason string) error
func (s *Store) ReassignRequest(ctx context.Context, actor User, id, newManagerID int64, reason string) error
func (s *Store) ReraiseRequest(ctx context.Context, actor User, id int64) (int64, error)
func (s *Store) AddRequestComment(ctx context.Context, actor User, requestID int64, body string) (int64, error)
func (s *Store) RequestComments(ctx context.Context, requestID int64) ([]RequestComment, error)
func (s *Store) AddRequestAttachment(ctx context.Context, actor User, requestID int64, in AttachmentInput) (int64, error)
func (s *Store) RequestAttachments(ctx context.Context, requestID int64) ([]RequestAttachment, error)
func (s *Store) Request(ctx context.Context, id int64) (Request, error)
func (s *Store) ListRequests(ctx context.Context, opts RequestListOptions) ([]Request, error)
func (s *Store) CountRequests(ctx context.Context, opts RequestListOptions) (int, error)
func (s *Store) AppSetting(ctx context.Context, key string) (string, error)          // runtime settings read (T10 flag)
func (s *Store) SetAppSetting(ctx context.Context, actor User, key, value string) error // upsert + audit
```

**Attachment-required flag (T10):** the `require_attachments` setting lives in `app_settings` (default `'0'`, seeded by migration v2) and is read via `AppSetting(ctx, "require_attachments")` — `"1"` means mandatory. It is enforced only at **submit** (a draft may be saved without a file); `SubmitRequest` reads the setting and rejects a fileless submit when it is `"1"`. `SetAppSetting` (introduced here) writes the value with an audit row; the admin UI toggle that flips it lands in Phase 5. No `config.Config` flag is involved.

---

## 3. Treatment → type → required-field rules (overview §6, validated in `validateRequestInput`)

Always captured (every type): `amount > 0`, `purpose` non-empty, `manager_id` chosen (A1), `treatment ∈ {budget, recoverable}`, `type ∈ {vendor_invoice, vendor_advance, reimbursement, employee_advance, recoverable}`, optional `urgent` flag (T5); `needed_by`/`expected_return_date` when present must be `YYYY-MM-DD`.

| Type | Treatment | Required fields (P2 enforced) | Payee rule | Notes |
|---|---|---|---|---|
| `vendor_invoice` | budget | project + head; `vendor_payee` | free | invoice no/date via `vendor_payee`/reference capture; attachment optional (config-gated) — T6 |
| `vendor_advance` | budget | project + head; `vendor_payee` | free | reason carried in `purpose` — T7 |
| `reimbursement` | budget | project + head | **forced = requester** | payee overwritten with requester on create/update — T8, T11 |
| `employee_advance` | budget | project + head | **forced = requester** | non-refundable path — T9 (budget half) |
| `employee_advance` | recoverable | `expected_return_date` + `repayment_notes` (project optional) | **forced = requester** | requester auto-recorded; category=Employee advance captured (`recoverable_category_id`), per-category enforcement is P4 — T9 (capture) |
| `recoverable` | recoverable | `expected_return_date` + `repayment_notes`; `counterparty` captured | free | deposits/loans (EMD/PBG/ICD/security); category + EMD/PBG→project + ICD→counterparty enforcement is **Phase 4** — T9/V-series capture only |

Budget-expense types are validated **fully** in Phase 2. Recoverable-treatment inputs are **captured and lightly validated** (treatment/type + return date + repayment notes); the category-specific matrix (EMD/PBG→project, ICD→counterparty, budget-actuals exclusion) is deferred to Phase 4. Bank/account details never appear on the form (T3).

---

## 4. Status lifecycle & transitions (overview §6; Phase-2 subset enforced in the store)

**Statuses touched in P2:** `draft`, `pending`, `returned`, `rejected`, `withdrawn`, `approved`. (`processing`, `partial_review`, `completed`, and the `on_hold` flag are Phase 3.)

**Legal transitions enforced by `canTransition(from, to)` (illegal ⇒ `ErrValidation`, L11):**

| From | To | Method |
|---|---|---|
| draft | pending | `SubmitRequest` |
| returned | pending | `SubmitRequest` (resubmit) |
| pending | approved | `ApproveRequest` |
| pending | returned | `ReturnRequest` |
| pending | rejected | `RejectRequest` |
| pending | withdrawn | `WithdrawRequest` |

Terminal (no outbound P2 transition): `rejected`, `withdrawn`. `rejected` is read-only — a new request must be raised via **re-raise** (Q3). `ReassignRequest` and `UpdateRequest`-while-pending and comments do **not** change status.

**Edit rules by status:**
- `draft` — requester edits freely; private to the requester (L1).
- `returned` — requester edits + resubmits (Q1).
- `pending` — requester edits: writes audit history, resets `reminder_last_sent = NULL`, reroutes `manager_id` to the (possibly changed) chosen manager, and leaves a P5 hook for manager re-notification (A8).
- `approved`/`rejected`/`withdrawn` — `UpdateRequest` rejected with `ErrValidation`.

**Manager gating:** managers see **all** requests (scope `all`) but may `approve`/`return`/`reject` only requests whose `manager_id == actor.ID` (A5) — otherwise `ErrForbidden`. `ReassignRequest` requires `approval:reassign` (Admin) and records reason + audit history (A7). **No bulk approval** — approval is per-request from the full detail view (A6).

---

## 5. UI / routes

All new routes register behind Phase-1 `RequirePermission(resource, action, …)`; every POST is wrapped with `a.withCSRF`; pages render from `internal/app/templates.go` in the existing design system; errors via `a.respondStoreError`/`friendly`. Detail/list handlers additionally call `a.auth.Scope(u, "request")` and enforce it.

| Method / path | Permission | Handler | Purpose |
|---|---|---|---|
| `GET /dashboard` | login | `dashboard` | Unified work-area hub, permission-gated, with counts (D1/D2/D4) |
| `GET /requests` | request/view | `requests` | Scoped, filterable, searchable list/queue |
| `GET /requests/export.csv` | request/view | `requestsExport` | CSV of the caller's scoped list (D3) |
| `GET /requests/new` | request/create | `requestForm` | Mobile-first treatment→type form (T1–T8, no bank fields T3) |
| `POST /requests` | request/create | `requestCreate` | Create draft; `submit_action=submit` also submits |
| `GET /requests/{id}` | request/view | `requestDetail` | Fields, conversation, audit timeline, action panel (scope-checked) |
| `GET /requests/{id}/edit` | request/edit | `requestEditForm` | Edit draft/returned/pending |
| `POST /requests/{id}/edit` | request/edit | `requestEdit` | Update (A8/Q1) |
| `POST /requests/{id}/submit` | request/edit | `requestSubmit` | draft/returned → pending (L2) |
| `POST /requests/{id}/withdraw` | request/withdraw | `requestWithdraw` | pending → withdrawn (Q2/L5) |
| `POST /requests/{id}/reraise` | request/reraise | `requestReraise` | fresh draft copy of a rejected request (Q3) |
| `POST /requests/{id}/comment` | request/comment | `requestComment` | Append to conversation (N7) |
| `POST /requests/{id}/attachments` | attachment/create | `requestAttachmentUpload` | Optional file (T10) |
| `POST /requests/{id}/approve` | approval/approve | `requestApprove` | Approve, adjustable amount (A2/L6) |
| `POST /requests/{id}/return` | approval/return | `requestReturn` | Return with required comment (A4/L3) |
| `POST /requests/{id}/reject` | approval/reject | `requestReject` | Reject with required reason (A3/L4) |
| `POST /requests/{id}/reassign` | approval/reassign | `requestReassign` | Admin reassign, reason + history (A7) |

**Nav** gains permission-driven links to "Dashboard" (any logged-in user) and "Requests" (`Can("request","view")`). Templates added: `dashboard`, `requests`, `request_form`, `request_detail`. The form is mobile-first (T4): single-column `form-grid`, treatment radios then type radios, fields grouped in `fieldset`s; **no bank/account fields** (T3). The list is wrapped in `table-wrap`. **No copy-previous control** anywhere and **no bulk-approve control**; there is no `/requests/{id}/copy` and no bulk-approve route (D5/A6 proven by 404 + template-absence assertions).

**Inactive projects/heads (T12):** the form's project/head selectors come from `ListProjects(ctx, true)` / `ListHeads(ctx, true)` (active only), so retired options disappear from new requests; `Request`/`requestDetail` join by id and keep showing the historical project/head name after retirement.

---

## 6. Dashboard + queues (D1/D2/D4/N1)

`GET /dashboard` renders up to four work areas, each shown only if the caller's permissions allow, each with a live count from a scoped `CountRequests`:

| Work area | Shown when | Count query | Links to |
|---|---|---|---|
| My requests | `Can("request","create")` | `CountRequests{Scope:"own", ViewerID:u.ID}` | `/requests?scope=own` |
| Manager approvals | `Can("approval","approve")` | `CountRequests{Scope:"assigned", ViewerID:u.ID, Status:"pending"}` | `/requests?scope=assigned&status=pending` |
| Accounts queue (placeholder) | `Can("payment","process")` | `CountRequests{Scope:"all", Status:"approved"}` | `/requests?status=approved` (Phase 3 fills the real queue) |
| Administration | `Can("role","view")` | — (links only) | `/roles`, `/users`, `/audit` |

The list handler resolves its effective scope from `auth.Scope(u,"request")` intersected with the requested `scope` query param (a requester whose scope is `own` can never widen to `all`); rows are filtered by `requester_id` (own) / `manager_id` (assigned) / unfiltered (all). A Requester therefore sees only their own requests (Q5); reaching another user's request by URL is `ErrForbidden`/403.

**In-app notification by default (N1):** Phase 2 delivers the "in-app, by default" notification posture through these permission-gated work-area queues — *My requests*, *Manager approvals* (Approvals), *Accounts queue* (To pay), and the Phase-3 *Partial-review* queue — each surfacing a live count on the dashboard so a user sees their pending work on login without any email. Email and other channels are Phase 5. This is proven by `TestDashboardShowsGatedWorkAreasWithCounts` (see the plan's Coverage table).

---

## 7. Acceptance criteria (P2 done)

- An employee raises each request type with correct required-field enforcement (budget types fully; recoverable fields captured); `vendor_invoice`/`vendor_advance`/`reimbursement`/`employee_advance` reject missing project/head/vendor; reimbursement & employee-advance payee is forced to the requester.
- Auto number `PR-YYYY-NNNNNN` is unique and strictly monotonic per year across concurrent creates.
- Requester can save a draft (private), submit, edit a returned request and resubmit, edit while pending (history logged, reminder reset, manager rerouted), withdraw a pending request, and re-raise a rejected one into a fresh draft.
- Manager can approve (adjusting the approved amount), return with a required comment, reject with a required reason; a manager can only approve requests assigned to them; an admin can reassign with a reason recorded in history.
- Illegal status transitions are rejected.
- The conversation thread persists and is readable by anyone who can view the request.
- The unified dashboard shows only permitted work areas with correct counts; a Requester cannot reach another user's request by URL (403); request lists export to CSV by permission; copy-previous-request and bulk approval are absent.
- Every request mutation writes an `audit_log` row with `entity_type='payment_request'`.
- `make test-race` and `make test-cover` green; e2e `requests.spec.ts` green.

---

## 8. Test plan (TDD)

- **Store unit** — `internal/store/requests_test.go` (+ schema assertions in `internal/store/requests_schema_test.go`): number sequence, per-type validation, create/get, submit + attachment-required flag, update (draft/returned/pending reroute+reminder), withdraw, approve (adjust + assigned-only), return/reject (required text), reassign, re-raise, comments/attachments, list-by-scope + counts, illegal-transition rejection, audit entity type. Uses `newTestStore(t)` and a `seedRequestActors` helper.
- **App integration** — `internal/app/app_integration_test.go` additions using `newAppTestServer(t)`: requester submit flow, scope 403 on another user's request, dashboard work-area gating + counts, CSV export, no-bulk / no-copy absence (404 + body), manager approve via detail page. A `assignRole(t,s,userID,name)` helper assigns Phase-1 roles.
- **e2e** — `tests/e2e/requests.spec.ts`: an employee logs in, raises a `vendor_invoice`, submits; a manager approves; both under the Playwright fixtures, asserting no console/500 errors and axe-clean.

Every task in the plan is red → green → commit and runs `make test-race`/`make test-cover` (Go) or `make test-e2e` (browser).
