# Phase 3 — Payment Linking, Reservation & Settlement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

## Amendment log

Amended 2026-07-25 against `docs/superpowers/specs/2026-07-25-design-system-adoption-spec.md` §4 → **Phase 3**. Backend task bodies not listed below are unchanged from the original plan.

| Decision | Change | Where |
|---|---|---|
| spec §3 phase map | Migration renumbered **v3 → v4** (Phase 1 = v1, Phase 1V vendors = v2, Phase 2 requests = v3). | Architecture, Global Constraints, Task 1 |
| **G12** | `ReleaseRequest` gains a **required `reason string`**, recorded in the audit trail. | Task 4 |
| **G11** | Reservation **reassign** existed in no store method, route or permission verb yet appears in three mockups. Adds the `reservation` resource (`reserve`/`release`/`reassign`), the `ReassignReservation` store method (reason required, reassign permission required) and the screen. | Tasks 5, 11, 18 |
| **G13** | `RecordPaymentForRequest` rejects **paid > approved** with `ErrValidation`; the remedy is cancelling and raising a new request. | Task 6 |
| **G14** | Manager acceptance of a partial writes the distinct terminal state **`completed_partial`** ("Completed — partial accepted", `.pill.completed-partial`), not plain `completed`. | Task 8 |
| queue + picker data | `LinkablePaymentRequests` gains a **status filter**, **per-status counts**, metric totals and a second **"unavailable/taken" result set**; rows expose `ProcessingBy`/`ProcessingAt`/`ProcessingByName`. Without these, four of the queue's five `.segmented` tabs, all four `.metric-strip` metrics and the picker's `.co.is-taken` rows are unbuildable. | Task 10 |
| **G15** | Reservation conflict becomes a **rendered 409 screen** via `renderStatus`, not `respondError`. | Task 11 |
| **D8** | New **pure** `POST /requests/{id}/settlement-preview` — no `BeginTx`, no attachment staging. Only `POST /payments` writes. | Task 15 |
| **D5** | No template added or edited in this phase may emit `.badge`; statuses are `.pill`. | Global Constraints |
| spec §1, §4 | Every screen is rebuilt on the Phase 0 component layer. New UI tasks: accounts queue (12), request picker (13), payment entry (14), settlement preview (15), settlement submit + payment detail (16), partial review (17), release/reassign (18), hold + stale reservation (19). | Tasks 12–19 |

**Task count: 14 → 21.** Old Task 10 splits into 11–14; old Task 11 into 15–16; old Task 12 into 17–19; old Tasks 13 and 14 become 20 and 21. Tasks 1–3, 7 and 9 are unchanged apart from renumbering.

**Contradictions recorded, not silently resolved:**

1. **Phase 0 does not enumerate four classes this phase needs.** Phase 0 Tasks 6–11 port `mockup.css` by line range, and those ranges omit `.metric-strip`/`.metric` (391–411), `.segmented` (436–453) and `.reserve-bar` (625–641), and scope `.a-list` to `.area` (1019–1037) although `accounts-reservation-conflict.html` and `accounts-stale-processing.html` use `.a-list` directly inside `.card`. Handled by the pre-flight check in Global Constraints rather than by redefining Phase 0's components here.
2. **`payment-request-picker.html` promises a Configuration toggle this phase does not build.** Its `.banner.info` ends "An administrator can re-enable direct entry in Configuration if you ever need it." D6 gives the Configuration screen to Phase 2 and lets Phases 3–5 append fieldsets, but spec §4 → Phase 3 lists no fieldset for this phase. Task 13 renders the banner **without that final sentence** — shipping a promise of a control that does not exist is worse than omitting it — and the "Legacy path retirement" constraint below stands unweakened. If the toggle is genuinely wanted, it needs a spec decision and its own task, not a silent addition here.
3. **`completed_partial` is a new status in Phase 2's vocabulary.** Phase 2 owns `canTransition`; Task 8 extends it (`partial_review → completed_partial`) rather than duplicating the state machine here.

**Implementation notes recorded while executing Tasks 1–10 (2026-07-26) — the plan text below is wrong on these points:**

- **Task 3's concurrency proof could not pass against `store.Open` as it stood, for a reason the plan does not mention.** `Open` configured SQLite with `db.Exec("PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;")`, but PRAGMA state is **per connection** and `database/sql` opens further connections on demand, so only the first connection was ever configured. The eight racing reservations therefore failed with `database is locked (5) (SQLITE_BUSY)` instead of `ErrForbidden`, and — more seriously — foreign keys were silently **off** on every connection but one. `Open` now passes the pragmas as DSN `_pragma` query parameters (`path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"`), which modernc.org/sqlite applies to each new connection; without a `file:` prefix it strips the query before opening, so paths containing spaces still work. Verified separately that the race test does its job: with the `WHERE status='approved' AND processing_by IS NULL AND on_hold=0` guard removed it reports *"more than one winner: 0 and 1"*.
- **Task 5's `active` column exists**, so the deactivated-target check ships as written. **Task 9's `AddRequestComment` returns `(int64, error)`**, not `error`; the test calls it as `if _, err := …`.
- **Task 8's `canTransition` snippet is written for the wrong type.** `legalTransitions` is `map[string]map[string]bool`, not `map[string][]string`; the edge ships as `"partial_review": {"completed": true, "completed_partial": true}`. Phase 2's exhaustive `TestCanTransition` still passes untouched, because `partial_review` is not in its status list. `requestStatuses` (an unused map) was left alone — nothing reads it, and no task asked for it.
- **Task 10's `LinkablePaymentRequests` did not exist**; it is new code, not a rewrite. Three things in the task are wrong:
  1. **Staleness cannot be counted while scanning the returned rows.** The plan's own test calls the method with the default (approved) tab and expects `StaleReservations == 1`, but that tab's query returns no `processing` rows at all, so the row-loop counter is always 0 there — and the Approved tab is exactly where `accounts-queue.html` renders the "1 open over a day" banner. The count moved into the aggregate pass with an injected cutoff (`now-StaleReservation`, formatted as SQLite's `CURRENT_TIMESTAMP` text), which is the fallback the task's own closing note describes. `julianday('now')` is still never used.
  2. **The default tab must return the taken rows too.** The plan filters `""`/`"approved"` to `status='approved' AND processing_by IS NULL AND on_hold=0`, which makes `Unavailable` permanently empty — yet the plan's test requires the held and the reserved request in it, and the picker's `.co.is-taken` rows exist for precisely that. The default tab now selects `status IN ('approved','processing')`; takeability is still re-derived per row, so no tab can offer a request that is not free.
  3. **The `Processing` tab counts every reservation, not just the caller's.** The plan's test asserts `c.Processing == 2` (own only) while the plan's own SQL counts 3, and `accounts-queue.html` reads "Processing 5" against "Reserved by you 2" and "Reserved by others 3" — 2+3. Counting only one's own would also contradict the rows the same call returns under `Status:"processing"`, which include Deepak's. The SQL is kept; **that one assertion in the plan's test was corrected to 3**, with the reason recorded in the test.

---

**Goal:** Put an atomic reservation → payment → settlement flow in front of the existing ledger so every new payment is created only by linking to one approved, actor-reserved request, with exactly one payment per request and an explicit settlement that drives the request to *completed*, *completed_partial* or *partial_review* — rendered on the approved design system.

**Architecture:** Add migration **v4** (payments `request_id`/`settlement`/`partial_reason` + partial unique index). Add store methods that mutate `payment_requests`/`payments` inside one transaction each, using conditional `UPDATE … WHERE` for every state transition so illegal/racing moves fail closed. Re-point the HTTP payment-creation path at `RecordPaymentForRequest`; retire the free-standing create from all routes. Settlement is a two-step: a **pure** preview endpoint renders the confirmation sheet and writes nothing, and only the final `POST /payments` persists. Ten approved screens — accounts queue, request picker, payment entry, settlement confirmation, payment detail, partial review, reservation conflict, release/reassign, stale reservation, request on hold — are built on the Phase 0 component layer.

**Tech Stack:** Go 1.x, `database/sql` + `modernc.org/sqlite`, `html/template`, `net/http` (Go 1.22 routing), Playwright (TS). No new dependencies.

## Global Constraints

- **Currency:** ₹ INR stored as integer **paise (`int64`)**, formatted via `internal/money` (`FormatPaise`, `ParsePaise`). Never float money.
- **Store mutations:** `func (s *Store) X(ctx, actor User, …)`; each mutation uses `s.db.BeginTx(ctx,nil)` + `defer tx.Rollback()` + `tx.Commit()` and writes `recordAuditTx(ctx, tx, AuditInput{…})` — entity_type `payment_request` for request rows, `payment` for payment rows.
- **Errors:** reuse `ErrNotFound, ErrForbidden, ErrValidation, ErrDuplicate, ErrLockedMonth, ErrInactiveHead`; validation wrapped as `fmt.Errorf("%w: message", ErrValidation)`; DB errors via `classify` (UNIQUE → `ErrDuplicate`).
- **Migrations:** append to the ordered `[]migration` in `internal/store/migrations.go`; numbers are contiguous and never reordered. Phase 1 = v1, Phase 1V (vendors) = v2, Phase 2 (requests) = v3, **Phase 3 = v4**. Column adds guard with `columnExists(tx,table,col)`; indexes use `IF NOT EXISTS`.
- **Handlers:** register in `App.routes` behind `a.auth.RequirePermission(resource, action, next)` with the overview §5 vocabulary as extended by spec D2; POST handlers wrapped with `a.withCSRF`; render via `internal/app/templates.go`; user-facing errors via `a.respondError` / `a.respondStoreError` / `friendly` — **except** where this plan names a rendered screen (Task 11 conflict, Task 15 preview), which use `a.renderStatus(w, r, status, name, data)`.
- **Consumed from Phase 1:** `migration{Version,Name,Up}` slice + `columnExists`; `auth.Manager.Can(u,resource,action) bool`, `auth.Manager.Scope(u,resource) string`, `RequirePermission(resource,action,next)`; the canonical vocabulary extended by spec D2 with **`reservation`{`reserve`,`release`,`reassign`}** (Phase 1 Task 3 `resourceActions`). Phase 3 is the only consumer of that resource.
- **Consumed from Phase 2:** table `payment_requests` (overview §3), `request_comments`; `store.Request` exposing `ID, Number, Status, Treatment, Type, Amount, Purpose, NeededBy, ApprovedAmount *int64, ApprovedByName, ApprovedAt *time.Time, VendorPayee, Project, Head, RequesterID, RequesterName, ManagerID, ManagerName, OnHold bool, HoldReason, ProcessingBy *int64, ProcessingAt *time.Time, HeadID *int64`; `Request(ctx,id) (Request,error)`; `ListRequests(ctx,RequestListOptions) ([]Request,error)`; `RequestListOptions{Scope,ViewerID,Status,Query,Limit}`; `AddRequestComment(ctx,actor,requestID,body) error`; `canTransition(from,to string) bool`; the `requestDetail` handler and `request_detail` template; and `PageData` fields `Requests []store.Request`, `Request2 store.Request`, `Comments`, `RequestAtts`, `Counts map[string]int` — Phase 3 adds no duplicate of these.
- **Consumed from Phase 0 (design system):** the component layer in `web/static/fervid-ds.css`, the behaviours in `web/static/fervid-app.js`, and `PageData.Shell` populated inside `renderStatus` (skipped when `HX-Request` is present, which is what makes htmx fragments possible). This phase **renders with, and never redefines**: `.page-banner`, `.metric-strip`/`.metric`, `.segmented` + `.n`, `.banner` tones (`info|warn|bad|good|brand`) with `.b-ico`/`.b-actions`, `.pill` statuses including `.pill.completed-partial` and `.pill.no-dot`, `.waiting`/`.you`/`.done`, `.req-head`/`.rh-top`/`.rh-no`/`.rh-amt`/`.rh-meta`/`.rh-status`, `.card`/`.card-head`/`.card-body`, `.dl`, `.compare`/`.cmp-row`/`.diff`/`.match`, `.overlay > .sheet` with `.sh-head`/`.sh-sub`/`.sh-body`/`.sh-foot`/`.sh-close`, `.choice` with `.outcome.good`/`.outcome.warn`, `.money-field`/`.money-wrap`/`.cur`/`.in-words`, `.combo`/`.combo-input`/`.combo-caret`/`.combo-list`/`.co`/`.co-main`/`.co-amt`/`.co.is-taken`, `.uploader`/`.file-row`, `.reserve-bar`/`.rb-dot`/`.rb-meta`/`.rb-actions`, `.a-list`/`.al-main`/`.al-amt`, `.thread`/`.tl-dot`/`.tl-head`/`.tl-body`/`.tl-change`, `.comment-box`/`.cb-actions`, `table.t-cards` + `td[data-label]` + `td.t-lead` + `.t-sub`, `.toolbar`/`.m-filters`/`.m-search`/`.filter-btn`, `.action-bar`/`.ab-note`, `.form-grid`/`.span-*`/`.field`/`.req`/`.opt`/`.hint`/`.flabel`, `.stack-8/12/16`, `.row-end`, `.num`, `.empty`, `.section-head`, `.d-only`/`.m-only`.
- **Phase 0 pre-flight — run once before Task 11 and fix before continuing:**
  ```bash
  for c in 'metric-strip' 'segmented' 'reserve-bar' 'is-taken' 'completed-partial' '^\.a-list'; do
    grep -q -- "$c" web/static/fervid-ds.css || echo "MISSING $c"
  done
  ```
  Must print nothing. Phase 0's line-range port list omits `.metric-strip`/`.metric` (`mockups/mockup.css:391-411`), `.segmented` (`:436-453`) and `.reserve-bar` (`:625-641`), and scopes `.a-list` to `.area` (`:1019-1037`) while two Phase-3 mockups place `.a-list` directly inside `.card`. If anything is reported missing, port those exact ranges from `mockups/mockup.css` verbatim — dropping the `.area ` prefix from the `.a-list` rules and converting `html[data-device="mobile"]` selectors to the `@media (max-width: 860px)` block per D4 — in one commit `feat(css): port queue, segmented, reserve-bar and a-list components`. Do not invent substitutes.
- **UI conventions (spec §1, D5, and the approved mockups):** every screen this phase adds or edits reproduces its mockup in `mockups/screens/`. Statuses are `.pill` — `grep -n 'class="badge' internal/app/templates.go` must stay at zero. Confirmations are `.overlay > .sheet`; a `<details>` disclosure is **not** an acceptable substitute for a sheet, and the original plan's `<details class="inline-danger">` blocks are removed wherever they appear. Tables are `table.t-cards` with `data-label` on every `<td>` so they restack below 860 px. Reveals use the Phase 0 `[data-when="name:value"]` contract and are **re-enforced server-side** — `hidden` is not validation.
- **Legacy path retirement:** `CreatePayment`/`CreatePaymentWithAttachment` stay only as seed/import helpers producing historical (`request_id IS NULL`) rows; they are removed from every HTTP route. `RecordPaymentForRequest` is the only user-reachable creation method. No configuration flag re-enables free entry in this phase.
- **Testing (TDD, required):** store units via `newTestStore(t)`; HTTP via `newAppTestServer(t)`; e2e in `tests/e2e/*.spec.ts`. Each task is red→green→commit. Verify with `make test-race`, `make test-cover`, `make test-e2e`. One commit per task, conventional messages. Stage only the files the task touched, by explicit path — the tree carries unrelated modified files, so never `git add -A`.

---

### Task 1: Migration v4 — payments linking columns + partial unique index

**Files:**
- Modify: `internal/store/migrations.go` (append migration v4)
- Create: `internal/store/linking_test.go` (shared Phase-3 store test helpers + migration tests)

**Interfaces:**
- Consumes: Phase-1 `migration{Version int, Name string, Up func(*sql.Tx) error}` slice and `columnExists(tx *sql.Tx, table, col string) (bool, error)`; table `payment_requests` (Phase 2).
- Produces: `payments.request_id INTEGER` (nullable), `payments.settlement TEXT NOT NULL DEFAULT ''`, `payments.partial_reason TEXT NOT NULL DEFAULT ''`, unique index `idx_payments_request` (partial, `WHERE request_id IS NOT NULL`); test helpers `seedRequestParty`, `seedApprovedRequest`, `requestStatus`, `requestProcessingBy`.

- [x] **Step 1: Write the failing test** (create `internal/store/linking_test.go`)

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

// seedRequestParty creates the users, project, and head that request fixtures
// share, returning an Accounts actor, a requester, a manager id, and a head id.
func seedRequestParty(t *testing.T, s *Store, ctx context.Context) (accountant User, requester User, managerID, headID int64) {
	t.Helper()
	accID, err := s.CreateUser(ctx, "accounts@example.com", "Accounts", "hash", "admin", true)
	if err != nil {
		t.Fatalf("create accountant: %v", err)
	}
	reqID, err := s.CreateUser(ctx, "requester@example.com", "Requester", "hash", "data_entry", true)
	if err != nil {
		t.Fatalf("create requester: %v", err)
	}
	mgrID, err := s.CreateUser(ctx, "manager@example.com", "Manager", "hash", "admin", true)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	projectID, err := s.UpsertProject(ctx, 0, "Operations", true, 1)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	hid, err := s.UpsertHead(ctx, 0, projectID, "Rent", "5", true, 1)
	if err != nil {
		t.Fatalf("create head: %v", err)
	}
	if accountant, err = s.UserByID(ctx, accID); err != nil {
		t.Fatal(err)
	}
	if requester, err = s.UserByID(ctx, reqID); err != nil {
		t.Fatal(err)
	}
	return accountant, requester, mgrID, hid
}

// seedApprovedRequest inserts an approved payment_requests row directly (its
// schema is fixed by overview §3) so Phase-3 store tests do not depend on the
// Phase-2 request methods. seq keeps the request number unique within a test.
func seedApprovedRequest(t *testing.T, s *Store, ctx context.Context, seq int, requesterID, managerID, headID, amount, approved int64) int64 {
	t.Helper()
	var projectID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatalf("head project: %v", err)
	}
	res, err := s.DB().ExecContext(ctx, `INSERT INTO payment_requests
		(number,status,treatment,type,project_id,head_id,amount,purpose,vendor_payee,requester_id,manager_id,approved_amount,approved_by,approved_at)
		VALUES(?,'approved','budget','vendor_invoice',?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), projectID, headID, amount, "Office rent", "Acme Landlord", requesterID, managerID, approved, managerID)
	if err != nil {
		t.Fatalf("seed approved request: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func requestStatus(t *testing.T, s *Store, ctx context.Context, id int64) string {
	t.Helper()
	var status string
	if err := s.DB().QueryRowContext(ctx, `SELECT status FROM payment_requests WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatalf("request status: %v", err)
	}
	return status
}

func requestProcessingBy(t *testing.T, s *Store, ctx context.Context, id int64) *int64 {
	t.Helper()
	var pb sql.NullInt64
	if err := s.DB().QueryRowContext(ctx, `SELECT processing_by FROM payment_requests WHERE id=?`, id).Scan(&pb); err != nil {
		t.Fatalf("processing_by: %v", err)
	}
	if pb.Valid {
		v := pb.Int64
		return &v
	}
	return nil
}

func TestMigrationV4AddsLinkingColumnsAndIndex(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	for _, col := range []string{"request_id", "settlement", "partial_reason"} {
		var found int
		if err := s.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('payments') WHERE name=?`, col).Scan(&found); err != nil {
			t.Fatal(err)
		}
		if found != 1 {
			t.Fatalf("payments.%s missing after migration", col)
		}
	}
	var idx int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_payments_request'`).Scan(&idx); err != nil {
		t.Fatal(err)
	}
	if idx != 1 {
		t.Fatal("idx_payments_request missing after migration")
	}
	_ = ctx
}

func TestPaymentRequestIndexRejectsDuplicateLinkButAllowsNullHistoricals(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	linked := func(paidOn string) error {
		_, err := s.DB().ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,entered_by,request_id,settlement) VALUES(?,?,?,?,?,'settled')`, headID, paidOn, 1000, acc.ID, reqID)
		return err
	}
	if err := linked("2026-06-15"); err != nil {
		t.Fatalf("first link insert: %v", err)
	}
	if err := linked("2026-06-16"); err == nil {
		t.Fatal("second payment linked to the same request was allowed (S9 broken)")
	}
	for _, d := range []string{"2026-06-17", "2026-06-18"} {
		if _, err := s.DB().ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,entered_by) VALUES(?,?,?,?)`, headID, d, 2000, acc.ID); err != nil {
			t.Fatalf("historical NULL-request insert %s: %v", d, err)
		}
	}
}

func TestMigrationV4IsIdempotentAndLeavesHistoricalPaymentsUntouched(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := dir + "/fervid.db"
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	actor, headID := seedActorAndHead(t, s, ctx)
	payID, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-05-10", Amount: 7777})
	if err != nil {
		t.Fatalf("historical payment: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Re-open the same database: migrate() must be a no-op, not an error.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	var reqID sql.NullInt64
	var settlement string
	if err := s2.DB().QueryRowContext(ctx, `SELECT request_id, settlement FROM payments WHERE id=?`, payID).Scan(&reqID, &settlement); err != nil {
		t.Fatalf("read historical payment: %v", err)
	}
	if reqID.Valid || settlement != "" {
		t.Fatalf("historical payment changed by migration: request_id=%v settlement=%q", reqID, settlement)
	}
	_ = errors.Is
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestMigrationV4AddsLinkingColumnsAndIndex|TestPaymentRequestIndexRejectsDuplicateLinkButAllowsNullHistoricals|TestMigrationV4IsIdempotent' -v`
Expected: FAIL — `pragma_table_info('payments')` returns 0 for the new columns and `idx_payments_request` is absent, so `TestMigrationV4AddsLinkingColumnsAndIndex` fails ("payments.request_id missing after migration"); the duplicate-link insert succeeds (no index) so that test fails too.

- [x] **Step 3: Write minimal implementation** (append v4 to `internal/store/migrations.go`; the `migrations` slice and helpers already exist from Phase 1/2)

```go
{
	Version: 4,
	Name:    "payments_request_linking",
	Up: func(tx *sql.Tx) error {
		adds := []struct{ col, ddl string }{
			{"request_id", `ALTER TABLE payments ADD COLUMN request_id INTEGER REFERENCES payment_requests(id)`},
			{"settlement", `ALTER TABLE payments ADD COLUMN settlement TEXT NOT NULL DEFAULT ''`},
			{"partial_reason", `ALTER TABLE payments ADD COLUMN partial_reason TEXT NOT NULL DEFAULT ''`},
		}
		for _, a := range adds {
			exists, err := columnExists(tx, "payments", a.col)
			if err != nil {
				return err
			}
			if exists {
				continue
			}
			if _, err := tx.Exec(a.ddl); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_request ON payments(request_id) WHERE request_id IS NOT NULL`)
		return err
	},
},
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestMigrationV4AddsLinkingColumnsAndIndex|TestPaymentRequestIndexRejectsDuplicateLinkButAllowsNullHistoricals|TestMigrationV4IsIdempotent' -v`
Expected: PASS (3 tests).

- [x] **Step 5: Commit**

```bash
git add internal/store/migrations.go internal/store/linking_test.go
git commit -m "feat(store): add payments request linking columns and one-per-request index (migration v4)"
```

---

### Task 2: Payment model fields + scans + PaymentForRequest

**Files:**
- Modify: `internal/store/models.go` (Payment struct)
- Modify: `internal/store/store.go` (`Payment`, `paymentInTx` scans; add `PaymentForRequest`)
- Modify: `internal/store/linking_test.go` (add test)

**Interfaces:**
- Consumes: migration v4 columns (Task 1); `seedRequestParty`, `seedActorAndHead`.
- Produces: `Payment.RequestID *int64`, `Payment.Settlement string`, `Payment.PartialReason string`; `func (s *Store) PaymentForRequest(ctx context.Context, requestID int64) (Payment, error)`.

- [x] **Step 1: Write the failing test** (append to `internal/store/linking_test.go`)

```go
func TestPaymentForRequestAndHistoricalFields(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	payID, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-05-11", Amount: 3210})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Payment(ctx, payID)
	if err != nil {
		t.Fatal(err)
	}
	if p.RequestID != nil || p.Settlement != "" || p.PartialReason != "" {
		t.Fatalf("historical payment carries linkage fields: %+v", p)
	}
	if _, err := s.PaymentForRequest(ctx, 424242); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PaymentForRequest(unlinked) = %v, want ErrNotFound", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestPaymentForRequestAndHistoricalFields -v`
Expected: FAIL — build error `p.RequestID undefined (type store.Payment has no field or method RequestID)` and `s.PaymentForRequest undefined`; run reports `FAIL fervidbudget/internal/store [build failed]`.

- [x] **Step 3: Write minimal implementation**

In `internal/store/models.go`, add to the `Payment` struct (after `Remarks`):

```go
	RequestID     *int64
	Settlement    string
	PartialReason string
```

In `internal/store/store.go`, change the `Payment(ctx,id)` SELECT/Scan to include the new columns (append to the column list before `FROM` and to the `Scan` args):

```go
func (s *Store) Payment(ctx context.Context, id int64) (Payment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT py.id,py.head_id,h.project_id,p.name,h.name,py.paid_on,py.amount,
		COALESCE(py.vendor_payee,''),COALESCE(py.payment_mode,''),COALESCE(py.invoice_no,''),COALESCE(py.reference_no,''),COALESCE(py.remarks,''),
		py.entered_by,u.name,py.updated_by,py.voided_by,COALESCE(py.void_reason,''),py.voided_at,py.created_at,py.updated_at,
		py.request_id,COALESCE(py.settlement,''),COALESCE(py.partial_reason,'')
		FROM payments py JOIN heads h ON h.id=py.head_id JOIN projects p ON p.id=h.project_id JOIN users u ON u.id=py.entered_by WHERE py.id=?`, id)
	var p Payment
	err := row.Scan(&p.ID, &p.HeadID, &p.ProjectID, &p.Project, &p.Head, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.EnteredByName, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt, &p.RequestID, &p.Settlement, &p.PartialReason)
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	return p, err
}
```

Change `paymentInTx` similarly (it is used by the immutability guard in Task 7):

```go
func paymentInTx(ctx context.Context, tx *sql.Tx, id int64) (Payment, error) {
	var p Payment
	err := tx.QueryRowContext(ctx, `SELECT id,head_id,paid_on,amount,COALESCE(vendor_payee,''),COALESCE(payment_mode,''),COALESCE(invoice_no,''),COALESCE(reference_no,''),COALESCE(remarks,''),entered_by,updated_by,voided_by,COALESCE(void_reason,''),voided_at,created_at,updated_at,request_id,COALESCE(settlement,''),COALESCE(partial_reason,'') FROM payments WHERE id=?`, id).
		Scan(&p.ID, &p.HeadID, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt, &p.RequestID, &p.Settlement, &p.PartialReason)
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	return p, err
}
```

Add `PaymentForRequest` (next to `Payment`):

```go
// PaymentForRequest returns the single payment linked to a request, or
// ErrNotFound when none exists yet. It powers the requester's outcome view (Q4).
func (s *Store) PaymentForRequest(ctx context.Context, requestID int64) (Payment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT py.id,py.head_id,h.project_id,p.name,h.name,py.paid_on,py.amount,
		COALESCE(py.vendor_payee,''),COALESCE(py.payment_mode,''),COALESCE(py.invoice_no,''),COALESCE(py.reference_no,''),COALESCE(py.remarks,''),
		py.entered_by,u.name,py.updated_by,py.voided_by,COALESCE(py.void_reason,''),py.voided_at,py.created_at,py.updated_at,
		py.request_id,COALESCE(py.settlement,''),COALESCE(py.partial_reason,'')
		FROM payments py JOIN heads h ON h.id=py.head_id JOIN projects p ON p.id=h.project_id JOIN users u ON u.id=py.entered_by WHERE py.request_id=?`, requestID)
	var p Payment
	err := row.Scan(&p.ID, &p.HeadID, &p.ProjectID, &p.Project, &p.Head, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.EnteredByName, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt, &p.RequestID, &p.Settlement, &p.PartialReason)
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	return p, err
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestPaymentForRequestAndHistoricalFields -v`
Expected: PASS. Also run `go test ./internal/store/ -run TestPaymentCreateEditVoidAndAudit -v` — Expected: PASS (existing payment scans still work with the widened SELECT).

- [x] **Step 5: Commit**

```bash
git add internal/store/models.go internal/store/store.go internal/store/linking_test.go
git commit -m "feat(store): expose payment linkage fields and PaymentForRequest"
```

---

### Task 3: ReserveRequest — atomic reservation + concurrency proof (S2, S5, L8, L11)

**Files:**
- Modify: `internal/store/store.go` (add `ReserveRequest`)
- Modify: `internal/store/linking_test.go` (add tests; add `"sync"` to imports)

**Interfaces:**
- Consumes: `payment_requests` (Phase 2); `seedRequestParty`, `seedApprovedRequest`, `requestStatus`, `requestProcessingBy`.
- Produces: `func (s *Store) ReserveRequest(ctx context.Context, actor User, id int64) error`.

- [x] **Step 1: Write the failing test** (add `"sync"` to the `import` block of `linking_test.go`, then append)

```go
func TestReserveRequestMovesApprovedToProcessing(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "processing" {
		t.Fatalf("status = %q, want processing", got)
	}
	if pb := requestProcessingBy(t, s, ctx, reqID); pb == nil || *pb != acc.ID {
		t.Fatalf("processing_by = %v, want %d", pb, acc.ID)
	}
	// Illegal source states are rejected (L11): a second reserve, and reserving a
	// non-approved (e.g. still-pending) request, both fail.
	if err := s.ReserveRequest(ctx, acc, reqID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("double reserve = %v, want ErrForbidden", err)
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET status='pending' WHERE id=?`, reqID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, acc, reqID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reserve pending = %v, want ErrForbidden", err)
	}
}

func TestReserveRequestSkipsOnHoldRequests(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET on_hold=1 WHERE id=?`, reqID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, acc, reqID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reserve on-hold = %v, want ErrForbidden", err)
	}
}

// TestReserveRequestIsAtomicUnderConcurrency is the S5 proof: many accountants
// race to reserve one request; exactly one wins and the rest get ErrForbidden.
func TestReserveRequestIsAtomicUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)

	const racers = 8
	accts := make([]User, racers)
	for i := range accts {
		id, err := s.CreateUser(ctx, fmt.Sprintf("racer%d@example.com", i), fmt.Sprintf("Racer %d", i), "hash", "admin", true)
		if err != nil {
			t.Fatal(err)
		}
		if accts[i], err = s.UserByID(ctx, id); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = s.ReserveRequest(ctx, accts[i], reqID)
		}(i)
	}
	close(start)
	wg.Wait()

	winner := -1
	for i, err := range errs {
		switch {
		case err == nil:
			if winner != -1 {
				t.Fatalf("more than one winner: %d and %d", winner, i)
			}
			winner = i
		case errors.Is(err, ErrForbidden):
		default:
			t.Fatalf("racer %d unexpected error: %v", i, err)
		}
	}
	if winner == -1 {
		t.Fatal("no racer won the reservation")
	}
	if got := requestStatus(t, s, ctx, reqID); got != "processing" {
		t.Fatalf("final status = %q, want processing", got)
	}
	if pb := requestProcessingBy(t, s, ctx, reqID); pb == nil || *pb != accts[winner].ID {
		t.Fatalf("processing_by = %v, want winner %d", pb, accts[winner].ID)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestReserveRequest' -v`
Expected: FAIL — build error `s.ReserveRequest undefined (type *Store has no field or method ReserveRequest)`; `FAIL fervidbudget/internal/store [build failed]`.

- [x] **Step 3: Write minimal implementation** (add to `internal/store/store.go`)

```go
// ReserveRequest atomically moves an approved, unclaimed, not-on-hold request to
// 'processing' reserved by actor. The single conditional UPDATE is the
// concurrency guarantee: only the first committer matches, so a losing caller
// sees RowsAffected()==0 and is told the request is unavailable (S2, S5, L8).
func (s *Store) ReserveRequest(ctx context.Context, actor User, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests
		SET status='processing', processing_by=?, processing_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='approved' AND processing_by IS NULL AND on_hold=0`, actor.ID, id)
	if err != nil {
		return classify(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: request is not available to process", ErrForbidden)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "process", EntityType: "payment_request", EntityID: &id, Summary: "Reserved request for processing", After: map[string]any{"processing_by": actor.ID}}); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [x] **Step 4: Run test to verify it passes (including the race detector)**

Run: `go test ./internal/store/ -run 'TestReserveRequest' -v`
Expected: PASS (4 tests).
Run: `go test -race ./internal/store/ -run TestReserveRequestIsAtomicUnderConcurrency -v`
Expected: PASS with no data-race report.

- [x] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/linking_test.go
git commit -m "feat(store): atomic ReserveRequest with concurrency test (approved to processing)"
```

---

### Task 4: ReleaseRequest — confirm + authority + **required reason** (S6, S7, **G12**)

**Amended (G12):** `accounts-release-reassign.html` makes the reason a required field and its `.thread` renders every release reason ("Going on leave, someone else should take this."). The signature therefore gains `reason string`, validated before any transaction opens and recorded in the audit summary.

**Files:**
- Modify: `internal/store/store.go` (add `ReleaseRequest`)
- Modify: `internal/store/linking_test.go` (add test)

**Interfaces:**
- Consumes: `ReserveRequest` (Task 3); helpers.
- Produces: `func (s *Store) ReleaseRequest(ctx context.Context, actor User, id int64, reason string, confirmed, authorized bool) error`.

- [x] **Step 1: Write the failing test**

```go
func TestReleaseRequestRequiresConfirmReasonAndAuthority(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	otherID, err := s.CreateUser(ctx, "other@example.com", "Other", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.UserByID(ctx, otherID)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	const reason = "Vendor bank details need confirming before I can transfer."
	// No confirm → validation, still processing (S7).
	if err := s.ReleaseRequest(ctx, acc, reqID, reason, false, false); !errors.Is(err, ErrValidation) {
		t.Fatalf("release without confirm = %v, want ErrValidation", err)
	}
	// G12: no reason → validation, still processing, nothing recorded.
	if err := s.ReleaseRequest(ctx, acc, reqID, "   ", true, false); !errors.Is(err, ErrValidation) {
		t.Fatalf("release without reason = %v, want ErrValidation", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "processing" {
		t.Fatalf("status after refused release = %q, want processing", got)
	}
	// Non-assignee, non-authorized → forbidden (S6).
	if err := s.ReleaseRequest(ctx, other, reqID, reason, true, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-assignee release = %v, want ErrForbidden", err)
	}
	// Assignee with confirm + reason → back to approved, unclaimed.
	if err := s.ReleaseRequest(ctx, acc, reqID, reason, true, false); err != nil {
		t.Fatalf("assignee release: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "approved" {
		t.Fatalf("status after release = %q, want approved", got)
	}
	if pb := requestProcessingBy(t, s, ctx, reqID); pb != nil {
		t.Fatalf("processing_by not cleared: %v", pb)
	}
	// G12: the reason is in the audit trail, which is what the release .thread renders.
	var trail int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE entity_type='payment_request' AND entity_id=? AND action='release' AND summary LIKE ?`, reqID, "%"+reason+"%").Scan(&trail); err != nil {
		t.Fatal(err)
	}
	if trail != 1 {
		t.Fatalf("release reason not in the audit trail: %d matching entries", trail)
	}
	// Authorized caller (reservation:release granted broadly) may release someone else's hold.
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseRequest(ctx, other, reqID, "Reserved over a day, freeing it for the queue.", true, true); err != nil {
		t.Fatalf("authorized release: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "approved" {
		t.Fatalf("status after authorized release = %q, want approved", got)
	}
}
```

> If the audit table name in `recordAuditTx` differs from `audit_log`, match it in the trail assertion; the store logic is unaffected.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestReleaseRequestRequiresConfirmReasonAndAuthority -v`
Expected: FAIL — `s.ReleaseRequest undefined`; `FAIL fervidbudget/internal/store [build failed]`.

- [x] **Step 3: Write minimal implementation** (add to `internal/store/store.go`)

```go
// ReleaseRequest returns a 'processing' request to 'approved'. confirmed must be
// true ("no payment initiated" — S7) and reason is required (G12): the release is
// visible to the requester and the approver, and the reservation .thread renders
// it. Only the assignee, or an authorized caller (handler resolved
// reservation:release beyond their own reservations), may release (S6). There is
// no auto-release; this is the only path back to approved.
func (s *Store) ReleaseRequest(ctx context.Context, actor User, id int64, reason string, confirmed, authorized bool) error {
	if !confirmed {
		return fmt.Errorf("%w: confirm that no payment was initiated before releasing", ErrValidation)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a reason is required so the requester and approver know why", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var processingBy sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT status, processing_by FROM payment_requests WHERE id=?`, id).Scan(&status, &processingBy); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if status != "processing" {
		return fmt.Errorf("%w: only a processing request can be released", ErrForbidden)
	}
	if !authorized && (!processingBy.Valid || processingBy.Int64 != actor.ID) {
		return fmt.Errorf("%w: only the assignee may release this request", ErrForbidden)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='approved', processing_by=NULL, processing_at=NULL, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='processing'`, id); err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "release", EntityType: "payment_request", EntityID: &id, Summary: "Released reservation: " + reason, Before: map[string]any{"processing_by": processingBy.Int64}}); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestReleaseRequestRequiresConfirmReasonAndAuthority -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/linking_test.go
git commit -m "feat(store): ReleaseRequest with confirm, required reason and assignee/authorized checks"
```

---

### Task 5: ReassignReservation — hand a reservation to a colleague (**G11**)

**New task (G11).** Reassign appears in `accounts-release-reassign.html` (the second `.choice` option, "Needs reassign permission"), `accounts-reservation-conflict.html` (the `.a-list` entry "Ask for it to be reassigned") and `accounts-stale-processing.html` (the admin `.action-bar` "Reassign with reason"), yet no store method, route or permission verb existed. The request stays in `processing`; only `processing_by` moves. `authorized` is the handler-resolved `reservation:reassign` grant, mirroring `ReleaseRequest`'s `authorized` parameter — the store fails closed when it is false, so the check survives a mis-registered route.

**Files:**
- Modify: `internal/store/store.go` (add `ReassignReservation`)
- Modify: `internal/store/linking_test.go` (add test)

**Interfaces:**
- Consumes: `ReserveRequest` (Task 3); helpers; `UserByID`.
- Produces: `func (s *Store) ReassignReservation(ctx context.Context, actor User, id, toUserID int64, reason string, authorized bool) error`.

- [x] **Step 1: Write the failing test**

```go
func TestReassignReservationRequiresPermissionReasonAndActiveTarget(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	deepakID, err := s.CreateUser(ctx, "deepak@example.com", "Deepak Menon", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	const reason = "Going on leave, Deepak picks it up."
	// Without the reassign permission → forbidden, reservation untouched.
	if err := s.ReassignReservation(ctx, acc, reqID, deepakID, reason, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unauthorized reassign = %v, want ErrForbidden", err)
	}
	if pb := requestProcessingBy(t, s, ctx, reqID); pb == nil || *pb != acc.ID {
		t.Fatalf("processing_by moved without permission: %v", pb)
	}
	// Reason is required (same rule as release).
	if err := s.ReassignReservation(ctx, acc, reqID, deepakID, "  ", true); !errors.Is(err, ErrValidation) {
		t.Fatalf("reassign without reason = %v, want ErrValidation", err)
	}
	// Reassigning to the current holder is a no-op the user should not be offered.
	if err := s.ReassignReservation(ctx, acc, reqID, acc.ID, reason, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("reassign to self = %v, want ErrValidation", err)
	}
	// Unknown target user → not found, nothing changed.
	if err := s.ReassignReservation(ctx, acc, reqID, 987654, reason, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reassign to unknown user = %v, want ErrNotFound", err)
	}
	// Authorized, with a reason, to a real colleague → still processing, new holder.
	if err := s.ReassignReservation(ctx, acc, reqID, deepakID, reason, true); err != nil {
		t.Fatalf("reassign: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "processing" {
		t.Fatalf("status after reassign = %q, want processing (it never returns to the open queue)", got)
	}
	if pb := requestProcessingBy(t, s, ctx, reqID); pb == nil || *pb != deepakID {
		t.Fatalf("processing_by = %v, want %d", pb, deepakID)
	}
	var trail int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE entity_type='payment_request' AND entity_id=? AND action='reassign' AND summary LIKE ?`, reqID, "%"+reason+"%").Scan(&trail); err != nil {
		t.Fatal(err)
	}
	if trail != 1 {
		t.Fatalf("reassign reason not in the audit trail: %d matching entries", trail)
	}
	// An approved (unreserved) request cannot be reassigned — there is nothing to move.
	other := seedApprovedRequest(t, s, ctx, 2, req.ID, mgrID, headID, 100000, 100000)
	if err := s.ReassignReservation(ctx, acc, other, deepakID, reason, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reassign of an unreserved request = %v, want ErrForbidden", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestReassignReservationRequiresPermissionReasonAndActiveTarget -v`
Expected: FAIL — `s.ReassignReservation undefined (type *Store has no field or method ReassignReservation)`; `FAIL fervidbudget/internal/store [build failed]`.

- [x] **Step 3: Write minimal implementation** (add to `internal/store/store.go`, next to `ReleaseRequest`)

```go
// ReassignReservation hands a reservation to another user without ever returning
// the request to the open queue (G11): status stays 'processing' and only
// processing_by moves, so no third party can slip in between. authorized is the
// handler-resolved reservation:reassign grant; the store fails closed without it.
// reason is required and is what the reservation .thread renders.
func (s *Store) ReassignReservation(ctx context.Context, actor User, id, toUserID int64, reason string, authorized bool) error {
	if !authorized {
		return fmt.Errorf("%w: reassigning someone else's reservation needs the reassign permission", ErrForbidden)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a reason is required when a reservation changes hands", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var processingBy sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT status, processing_by FROM payment_requests WHERE id=?`, id).Scan(&status, &processingBy); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if status != "processing" || !processingBy.Valid {
		return fmt.Errorf("%w: only a reserved request can be reassigned", ErrForbidden)
	}
	if processingBy.Int64 == toUserID {
		return fmt.Errorf("%w: that person already holds this reservation", ErrValidation)
	}
	var toName string
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT name, active FROM users WHERE id=?`, toUserID).Scan(&toName, &active); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if active == 0 {
		return fmt.Errorf("%w: that user is deactivated", ErrValidation)
	}
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests
		SET processing_by=?, processing_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='processing' AND processing_by=?`, toUserID, id, processingBy.Int64)
	if err != nil {
		return classify(err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("%w: the reservation changed while you were deciding", ErrForbidden)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "reassign", EntityType: "payment_request", EntityID: &id,
		Summary: "Reassigned reservation to " + toName + ": " + reason,
		Before:  map[string]any{"processing_by": processingBy.Int64},
		After:   map[string]any{"processing_by": toUserID}}); err != nil {
		return err
	}
	return tx.Commit()
}
```

> If `users` has no `active` column in this codebase, drop the `active` scan and its check; every other assertion is unaffected.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestReassignReservationRequiresPermissionReasonAndActiveTarget -v`
Expected: PASS. Then `go test -race ./internal/store/ -run 'TestReserveRequest|TestReassignReservation'` — Expected: `ok`, no race report (reassign and reserve contend on the same conditional UPDATE).

- [x] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/linking_test.go
git commit -m "feat(store): ReassignReservation with reassign permission and required reason"
```

---

### Task 6: RecordPaymentForRequest — settlement transitions + **paid ≤ approved** (S9, S10, S13, L9, L10, L11, **G13**)

**Amended (G13):** `payment-entry.html` states the rule twice — the read-only approved field carries "A payment can never exceed this. Overpayment means cancelling and raising a new request", and the live difference banner turns `.banner.bad` with "more than approved — not allowed". The store had no such check. The ceiling is read on the same `SELECT` that validates the reservation and rejected with `ErrValidation` before any `INSERT`, so the deferred rollback leaves no payment row and the reservation survives — the accountant fixes the figure without re-reserving. The comparison is against `approved_amount` when set, falling back to `amount` for the (defensive) case of a request reaching `processing` without one. The error names the remedy, because the mockup does: cancel the request and raise a new one.

**Files:**
- Modify: `internal/store/store.go` (add `RecordPaymentForRequest`)
- Modify: `internal/store/linking_test.go` (add tests)

**Interfaces:**
- Consumes: `ReserveRequest` (Task 3); `Payment`/`PaymentForRequest` (Task 2); `validatePayment`, `paymentFromInput`, `addAttachmentTx`, `recordAuditTx`, `money.FormatPaise`.
- Produces: `func (s *Store) RecordPaymentForRequest(ctx context.Context, actor User, requestID int64, in PaymentInput, settlement, partialReason string, attachment *AttachmentInput) (int64, error)`.

- [x] **Step 1: Write the failing test**

```go
func TestRecordPaymentSettledCompletesEvenWhenUnderApproved(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	payID, err := s.RecordPaymentForRequest(ctx, acc, reqID, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 400000, VendorPayee: "Acme Landlord"}, "settled", "", nil)
	if err != nil {
		t.Fatalf("record settled: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "completed" {
		t.Fatalf("status = %q, want completed (S10)", got)
	}
	p, err := s.PaymentForRequest(ctx, reqID)
	if err != nil || p.ID != payID || p.RequestID == nil || *p.RequestID != reqID || p.Settlement != "settled" {
		t.Fatalf("linked payment = %+v, err=%v", p, err)
	}
	// One payment per request: a second settlement is refused (S9).
	if _, err := s.RecordPaymentForRequest(ctx, acc, reqID, PaymentInput{HeadID: headID, PaidOn: "2026-06-16", Amount: 100000, VendorPayee: "Acme"}, "settled", "", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("second settlement = %v, want ErrForbidden", err)
	}
}

func TestRecordPaymentPartialNeedsReasonAndRoutesToReview(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	in := PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 300000, VendorPayee: "Acme Landlord"}
	// Partial without reason → validation; nothing written; still processing (S13).
	if _, err := s.RecordPaymentForRequest(ctx, acc, reqID, in, "partial", "", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("partial without reason = %v, want ErrValidation", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "processing" {
		t.Fatalf("status after rejected partial = %q, want processing", got)
	}
	if _, err := s.PaymentForRequest(ctx, reqID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("payment written despite rejected settlement: %v", err)
	}
	// Partial with reason → partial_review (L9).
	if _, err := s.RecordPaymentForRequest(ctx, acc, reqID, in, "partial", "Balance pending vendor confirmation", nil); err != nil {
		t.Fatalf("record partial: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "partial_review" {
		t.Fatalf("status = %q, want partial_review", got)
	}
	p, err := s.PaymentForRequest(ctx, reqID)
	if err != nil || p.Settlement != "partial" || p.PartialReason != "Balance pending vendor confirmation" {
		t.Fatalf("linked partial payment = %+v, err=%v", p, err)
	}
}

func TestRecordPaymentRequiresActorReservation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	otherID, err := s.CreateUser(ctx, "other@example.com", "Other", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.UserByID(ctx, otherID)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	in := PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 500000, VendorPayee: "Acme Landlord"}
	// Not reserved at all → forbidden, no payment (store-level X5).
	if _, err := s.RecordPaymentForRequest(ctx, acc, reqID, in, "settled", "", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("record without reservation = %v, want ErrForbidden", err)
	}
	if got := paymentCount(t, s); got != 0 {
		t.Fatalf("payment written without reservation: %d", got)
	}
	// Reserved by acc; a different accountant may not record it.
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, other, reqID, in, "settled", "", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("record by non-assignee = %v, want ErrForbidden", err)
	}
}

// TestRecordPaymentRejectsOverpayment is the G13 proof: the mockup's read-only
// approved field and .banner.bad both promise that paid can never exceed
// approved. Rejection must leave the request reserved and no payment written, so
// the accountant can correct the figure without re-reserving.
func TestRecordPaymentRejectsOverpayment(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 480000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	over := PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 480001, VendorPayee: "Acme Landlord"}
	for _, settlement := range []string{"settled", "partial"} {
		_, err := s.RecordPaymentForRequest(ctx, acc, reqID, over, settlement, "reason", nil)
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("overpayment (%s) = %v, want ErrValidation", settlement, err)
		}
		if !strings.Contains(err.Error(), "cancel") {
			t.Fatalf("overpayment error must name the remedy (cancel and raise a new request), got %q", err)
		}
	}
	if got := paymentCount(t, s); got != 0 {
		t.Fatalf("overpayment wrote %d payment rows (G13 broken)", got)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "processing" {
		t.Fatalf("status after refused overpayment = %q, want processing (reservation must survive)", got)
	}
	// The approved amount itself is allowed; it is > that is refused.
	exact := PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 480000, VendorPayee: "Acme Landlord"}
	if _, err := s.RecordPaymentForRequest(ctx, acc, reqID, exact, "settled", "", nil); err != nil {
		t.Fatalf("payment of exactly the approved amount: %v", err)
	}
	if got := requestStatus(t, s, ctx, reqID); got != "completed" {
		t.Fatalf("status = %q, want completed", got)
	}
}
```

> `TestRecordPaymentRejectsOverpayment` needs `"strings"` in `linking_test.go`'s import block; add it with this task.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestRecordPayment' -v`
Expected: FAIL — `s.RecordPaymentForRequest undefined`; `FAIL fervidbudget/internal/store [build failed]`.

- [x] **Step 3: Write minimal implementation** (add to `internal/store/store.go`)

```go
// RecordPaymentForRequest writes the single linked payment for a request the
// actor currently holds in 'processing', then transitions it in the same
// transaction: "settled" → 'completed' (even if paid < approved, S10); "partial"
// → 'partial_review' (partialReason required, L9). Paid may never exceed the
// approved amount (G13). The payment and the request move together or not at all
// (S13); a re-record is refused by the status guard and idx_payments_request (S9).
func (s *Store) RecordPaymentForRequest(ctx context.Context, actor User, requestID int64, in PaymentInput, settlement, partialReason string, attachment *AttachmentInput) (int64, error) {
	settlement = strings.TrimSpace(settlement)
	partialReason = strings.TrimSpace(partialReason)
	if settlement != "settled" && settlement != "partial" {
		return 0, fmt.Errorf("%w: choose payment settled or partial settlement", ErrValidation)
	}
	if settlement == "partial" && partialReason == "" {
		return 0, fmt.Errorf("%w: a reason is required for a partial settlement", ErrValidation)
	}
	if err := s.validatePayment(ctx, in); err != nil {
		return 0, err
	}
	if attachment != nil {
		if err := validateAttachment(*attachment); err != nil {
			return 0, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var status string
	var processingBy sql.NullInt64
	var requested int64
	var approved sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT status, processing_by, amount, approved_amount FROM payment_requests WHERE id=?`, requestID).Scan(&status, &processingBy, &requested, &approved); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if status != "processing" || !processingBy.Valid || processingBy.Int64 != actor.ID {
		return 0, fmt.Errorf("%w: reserve this request before recording its payment", ErrForbidden)
	}
	// G13: the approved amount is a hard ceiling. Paying more is not a settlement
	// decision, it is a different obligation — cancel and raise a new request.
	ceiling := requested
	if approved.Valid {
		ceiling = approved.Int64
	}
	if in.Amount > ceiling {
		return 0, fmt.Errorf("%w: %s is more than the approved %s — to pay more, cancel this request and raise a new one",
			ErrValidation, money.FormatPaise(in.Amount), money.FormatPaise(ceiling))
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,payment_mode,invoice_no,reference_no,remarks,entered_by,request_id,settlement,partial_reason)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.HeadID, in.PaidOn, in.Amount, in.VendorPayee, in.PaymentMode, in.InvoiceNo, in.ReferenceNo, in.Remarks, actor.ID, requestID, settlement, partialReason)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	newStatus := "completed"
	if settlement == "partial" {
		newStatus = "partial_review"
	}
	upd, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='processing' AND processing_by=?`, newStatus, requestID, actor.ID)
	if err != nil {
		return 0, err
	}
	if n, err := upd.RowsAffected(); err != nil {
		return 0, err
	} else if n == 0 {
		return 0, fmt.Errorf("%w: reservation was lost before settlement", ErrForbidden)
	}
	after := paymentFromInput(id, actor, in)
	after.RequestID = &requestID
	after.Settlement = settlement
	after.PartialReason = partialReason
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "create", EntityType: "payment", EntityID: &id, Summary: "Recorded payment " + money.FormatPaise(in.Amount), After: after}); err != nil {
		return 0, err
	}
	reqAction, reqSummary := "settle", "Settled request as completed"
	if settlement == "partial" {
		reqAction, reqSummary = "mark_partial", "Partial settlement: "+partialReason
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: reqAction, EntityType: "payment_request", EntityID: &requestID, Summary: reqSummary, After: map[string]any{"status": newStatus, "payment_id": id}}); err != nil {
		return 0, err
	}
	if attachment != nil {
		if _, err := addAttachmentTx(ctx, tx, actor, id, *attachment); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestRecordPayment' -v`
Expected: PASS (4 tests, including `TestRecordPaymentRejectsOverpayment`).

- [x] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/linking_test.go
git commit -m "feat(store): RecordPaymentForRequest with settlement transitions and paid-not-over-approved guard"
```

---

### Task 7: Linked-payment immutability (S12)

**Files:**
- Modify: `internal/store/store.go` (`UpdatePaymentWithAttachment`, `VoidPayment`)
- Modify: `internal/store/linking_test.go` (add test)

**Interfaces:**
- Consumes: `paymentInTx` (now returns `RequestID`, Task 2); `RecordPaymentForRequest` (Task 6).
- Produces: `UpdatePaymentWithAttachment`/`VoidPayment` reject linked payments with `ErrValidation`.

- [x] **Step 1: Write the failing test**

```go
func TestLinkedPaymentIsImmutable(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	payID, err := s.RecordPaymentForRequest(ctx, acc, reqID, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 500000, VendorPayee: "Acme Landlord"}, "settled", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePayment(ctx, acc, payID, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 111, VendorPayee: "Changed"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("edit linked payment = %v, want ErrValidation", err)
	}
	if err := s.VoidPayment(ctx, acc, payID, "oops"); !errors.Is(err, ErrValidation) {
		t.Fatalf("void linked payment = %v, want ErrValidation", err)
	}
	p, err := s.Payment(ctx, payID)
	if err != nil || p.Amount != 500000 || p.VoidedAt != nil {
		t.Fatalf("linked payment mutated: %+v, %v", p, err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestLinkedPaymentIsImmutable -v`
Expected: FAIL — `UpdatePayment` succeeds (or `VoidPayment` succeeds) instead of returning `ErrValidation`; test fails at the first assertion.

- [x] **Step 3: Write minimal implementation**

In `UpdatePaymentWithAttachment`, immediately after `before, err := paymentInTx(ctx, tx, id)` (and its error check), before the `VoidedAt` check, insert:

```go
	if before.RequestID != nil {
		return fmt.Errorf("%w: a payment linked to a request cannot be edited", ErrValidation)
	}
```

In `VoidPayment`, immediately after `before, err := paymentInTx(ctx, tx, id)` (and its error check), before the `VoidedAt` check, insert:

```go
	if before.RequestID != nil {
		return fmt.Errorf("%w: a payment linked to a request cannot be voided", ErrValidation)
	}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestLinkedPaymentIsImmutable -v`
Expected: PASS. Also run `go test ./internal/store/ -run TestPaymentCreateEditVoidAndAudit -v` — Expected: PASS (historical `request_id IS NULL` payments remain editable/voidable, X6).

- [x] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/linking_test.go
git commit -m "feat(store): make request-linked payments immutable"
```

---

### Task 8: AcceptPartial → **completed_partial** + RaiseConcern (S11, L11, **G14**)

**Amended (G14):** `payment-partial-review.html` says the request "closes as **Completed — partial accepted**", `mockup.css:476` gives that state its own `.pill.completed-partial` (outlined green, not the solid `.pill.completed`), and `payments-ledger.html` renders it as a distinct row status. A manager accepting ₹60,000 against ₹95,000 approved is materially different from a payment that closed clean, and the ledger must be able to tell them apart forever. `AcceptPartial` therefore writes `completed_partial`, not `completed`. The sheet's optional note is carried through as `note string` because the mockup's close sheet has one.

**Phase 2 coupling:** `completed_partial` is a new value in the request status vocabulary that Phase 2 owns. Extend Phase 2's `canTransition` table with `partial_review → completed_partial` (terminal, no outgoing edges) rather than duplicating the state machine here, and add the same value to whatever status→label/pill mapping Phase 2 renders. Everything that treats `completed` as terminal must treat `completed_partial` the same way — in particular `LinkablePaymentRequests` (Task 10) counts both under the queue's "Paid" tab.

**Files:**
- Modify: `internal/store/store.go` (add `AcceptPartial`, `RaiseConcern`)
- Modify: `internal/store/requests.go` (Phase 2 `canTransition`: add `partial_review → completed_partial`)
- Modify: `internal/store/linking_test.go` (add test)

**Interfaces:**
- Consumes: `RecordPaymentForRequest` (Task 6); `request_comments` (Phase 2); `canTransition` (Phase 2).
- Produces: `func (s *Store) AcceptPartial(ctx context.Context, actor User, id int64, note string) error`; `func (s *Store) RaiseConcern(ctx context.Context, actor User, id int64, comment string) error`.

- [x] **Step 1: Write the failing test**

```go
func TestAcceptPartialAndRaiseConcern(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	// Two requests, both taken to partial_review.
	settleToReview := func(seq int) int64 {
		id := seedApprovedRequest(t, s, ctx, seq, req.ID, mgrID, headID, 500000, 500000)
		if err := s.ReserveRequest(ctx, acc, id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.RecordPaymentForRequest(ctx, acc, id, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 300000, VendorPayee: "Acme"}, "partial", "short pay", nil); err != nil {
			t.Fatal(err)
		}
		return id
	}
	mgr, _ := s.UserByID(ctx, mgrID)
	accepted := settleToReview(1)
	if err := s.AcceptPartial(ctx, mgr, accepted, "Balance will be invoiced separately."); err != nil {
		t.Fatalf("accept partial: %v", err)
	}
	// G14: a distinguishable terminal state, not plain 'completed'. The ledger and
	// the .pill.completed-partial both depend on being able to tell them apart.
	if got := requestStatus(t, s, ctx, accepted); got != "completed_partial" {
		t.Fatalf("accepted status = %q, want completed_partial (G14)", got)
	}
	// Accepting a non-partial_review request is rejected (L11).
	if err := s.AcceptPartial(ctx, mgr, accepted, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("accept completed_partial = %v, want ErrForbidden", err)
	}
	// The optional note reaches the trail the manager and Accounts both read.
	var noted int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE entity_type='payment_request' AND entity_id=? AND action='accept_partial' AND summary LIKE ?`, accepted, "%invoiced separately%").Scan(&noted); err != nil {
		t.Fatal(err)
	}
	if noted != 1 {
		t.Fatalf("accept note not in the audit trail: %d matching entries", noted)
	}
	// A clean settlement stays plain 'completed' — the two states never merge.
	clean := seedApprovedRequest(t, s, ctx, 3, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, clean); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, acc, clean, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 500000, VendorPayee: "Acme"}, "settled", "", nil); err != nil {
		t.Fatal(err)
	}
	if got := requestStatus(t, s, ctx, clean); got != "completed" {
		t.Fatalf("clean settlement status = %q, want completed", got)
	}

	concerned := settleToReview(2)
	if err := s.RaiseConcern(ctx, mgr, concerned, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty concern = %v, want ErrValidation", err)
	}
	if err := s.RaiseConcern(ctx, mgr, concerned, "Please confirm the balance timeline"); err != nil {
		t.Fatalf("raise concern: %v", err)
	}
	if got := requestStatus(t, s, ctx, concerned); got != "partial_review" {
		t.Fatalf("concerned status = %q, want partial_review", got)
	}
	var comments int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM request_comments WHERE request_id=? AND body=?`, concerned, "Please confirm the balance timeline").Scan(&comments); err != nil {
		t.Fatal(err)
	}
	if comments != 1 {
		t.Fatalf("concern comment not persisted: %d", comments)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestAcceptPartialAndRaiseConcern -v`
Expected: FAIL — `s.AcceptPartial undefined` / `s.RaiseConcern undefined`; `FAIL fervidbudget/internal/store [build failed]`.

- [x] **Step 3: Write minimal implementation** (add to `internal/store/store.go`)

```go
// AcceptPartial closes a 'partial_review' request as 'completed_partial' — a
// terminal state distinct from a clean 'completed' (S11, G14). The difference is
// permanent and visible: the ledger renders .pill.completed-partial, and anyone
// reading the request later can see that a balance was written off rather than
// paid. note is optional and joins the trail the requester reads.
func (s *Store) AcceptPartial(ctx context.Context, actor User, id int64, note string) error {
	note = strings.TrimSpace(note)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='completed_partial', updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='partial_review'`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("%w: only a partial-review request can be accepted", ErrForbidden)
	}
	summary := "Accepted partial settlement — completed, partial accepted"
	if note != "" {
		summary += ": " + note
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "accept_partial", EntityType: "payment_request", EntityID: &id, Summary: summary, After: map[string]any{"status": "completed_partial"}}); err != nil {
		return err
	}
	return tx.Commit()
}

// RaiseConcern keeps a 'partial_review' request in review and appends a
// conversation comment (S11). comment is required.
func (s *Store) RaiseConcern(ctx context.Context, actor User, id int64, comment string) error {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return fmt.Errorf("%w: a concern comment is required", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM payment_requests WHERE id=?`, id).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if status != "partial_review" {
		return fmt.Errorf("%w: only a partial-review request can receive a concern", ErrForbidden)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO request_comments(request_id,author_id,body) VALUES(?,?,?)`, id, actor.ID, comment); err != nil {
		return classify(err)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "concern", EntityType: "payment_request", EntityID: &id, Summary: "Raised concern: " + comment}); err != nil {
		return err
	}
	return tx.Commit()
}
```

Also extend Phase 2's `canTransition` (`internal/store/requests.go`) so the new terminal state is part of the one state machine:

```go
	// Phase 3 (G14): a manager-accepted partial closes distinctly from a clean pay.
	"partial_review": {"completed", "completed_partial"},
	// "completed_partial" has no outgoing edges — terminal, like "completed".
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestAcceptPartialAndRaiseConcern|TestCanTransition' -v`
Expected: PASS. `TestCanTransition` is Phase 2's table test; it must still pass with the new edge, and no existing edge may be removed to make it do so.

- [x] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/requests.go internal/store/linking_test.go
git commit -m "feat(store): accept partial into the distinct completed_partial terminal state"
```

---

### Task 9: HoldRequest / UnholdRequest + on-hold comment invariant (L7, Q6)

**Files:**
- Modify: `internal/store/store.go` (add `HoldRequest`, `UnholdRequest`)
- Modify: `internal/store/linking_test.go` (add test)

**Interfaces:**
- Consumes: `payment_requests` (Phase 2); `AddRequestComment` (Phase 2).
- Produces: `func (s *Store) HoldRequest(ctx context.Context, actor User, id int64, reason string) error`; `func (s *Store) UnholdRequest(ctx context.Context, actor User, id int64) error`.

- [x] **Step 1: Write the failing test**

```go
func TestHoldUnholdPreserveApprovedFieldsAndAllowComments(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 480000)

	if err := s.HoldRequest(ctx, acc, reqID, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("hold without reason = %v, want ErrValidation", err)
	}
	if err := s.HoldRequest(ctx, acc, reqID, "await vendor GST"); err != nil {
		t.Fatalf("hold: %v", err)
	}
	var onHold int
	var holdReason string
	var amount, approved int64
	if err := s.DB().QueryRowContext(ctx, `SELECT on_hold,hold_reason,amount,approved_amount FROM payment_requests WHERE id=?`, reqID).Scan(&onHold, &holdReason, &amount, &approved); err != nil {
		t.Fatal(err)
	}
	if onHold != 1 || holdReason != "await vendor GST" || amount != 500000 || approved != 480000 {
		t.Fatalf("hold changed approved fields: on_hold=%d reason=%q amount=%d approved=%d", onHold, holdReason, amount, approved)
	}
	// Q6: while on hold the requester may still add a comment; no field changes.
	if err := s.AddRequestComment(ctx, req, reqID, "Attaching the corrected invoice"); err != nil {
		t.Fatalf("comment while on hold: %v", err)
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT amount,approved_amount FROM payment_requests WHERE id=?`, reqID).Scan(&amount, &approved); err != nil {
		t.Fatal(err)
	}
	if amount != 500000 || approved != 480000 {
		t.Fatalf("comment changed approved fields: amount=%d approved=%d", amount, approved)
	}
	// A held request cannot be reserved (L7).
	if err := s.ReserveRequest(ctx, acc, reqID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reserve held = %v, want ErrForbidden", err)
	}
	// Unhold restores availability.
	if err := s.UnholdRequest(ctx, acc, reqID); err != nil {
		t.Fatalf("unhold: %v", err)
	}
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatalf("reserve after unhold: %v", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestHoldUnholdPreserveApprovedFieldsAndAllowComments -v`
Expected: FAIL — `s.HoldRequest undefined` / `s.UnholdRequest undefined`; `FAIL fervidbudget/internal/store [build failed]`.

- [x] **Step 3: Write minimal implementation** (add to `internal/store/store.go`)

```go
// HoldRequest pauses an approved, not-already-held request (L7). reason required.
func (s *Store) HoldRequest(ctx context.Context, actor User, id int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a hold reason is required", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET on_hold=1, hold_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='approved' AND on_hold=0`, reason, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("%w: only an approved, not-already-held request can be held", ErrForbidden)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "hold", EntityType: "payment_request", EntityID: &id, Summary: "On hold: " + reason}); err != nil {
		return err
	}
	return tx.Commit()
}

// UnholdRequest lifts a hold. The route gate (payment.hold) restricts this to
// Accounts (L7).
func (s *Store) UnholdRequest(ctx context.Context, actor User, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET on_hold=0, hold_reason='', updated_at=CURRENT_TIMESTAMP WHERE id=? AND on_hold=1`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("%w: request is not on hold", ErrForbidden)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "unhold", EntityType: "payment_request", EntityID: &id, Summary: "Hold lifted"}); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestHoldUnholdPreserveApprovedFieldsAndAllowComments -v`
Expected: PASS.

> If Phase 2's `AddRequestComment` signature differs from `AddRequestComment(ctx, actor User, requestID int64, body string) error`, adjust the call in the test to match; the store logic is unaffected.

- [x] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/linking_test.go
git commit -m "feat(store): HoldRequest and UnholdRequest preserving approved fields"
```

---

### Task 10: LinkablePaymentRequests — filter, search, **tabs, counts and the taken set** (S3, S4)

**Amended.** The original returned one flat `[]Request` of takeable requests. `accounts-queue.html` needs far more from the same query and the original shape cannot express any of it: a five-tab `.segmented` bar (Approved 7 · Processing 5 · On hold 1 · Partial review 1 · Paid 42) with a count per tab, a four-metric `.metric-strip` (approved-and-unclaimed with its ₹ total, reserved-by-you with a stale sub-count, reserved-by-others, on hold), rows that render "Reserved by you · 14:02", "Reserved by you · 26 h" and "Reserved by Deepak M", and — in `payment-request-picker.html` — a fourth `.co.is-taken` row for a request the caller may see but not take. So the method returns a `LinkableSet`: the takeable rows, the visible-but-taken rows, and the counts. One query feeds both slices; the counts come from a second, aggregate-only query so paging the rows never distorts the tabs.

Staleness ("1 open over a day", "26 h") is computed in Go from `ProcessingAt` against an injectable `Now`, per the overview's clock-injection rule — never from `julianday('now')`, which no test can control.

**Files:**
- Modify: `internal/store/models.go` (add `Request.ProcessingByName`; add `LinkableOptions`, `LinkableSet`, `LinkableCounts`)
- Modify: `internal/store/store.go` (rewrite `LinkablePaymentRequests`)
- Modify: `internal/store/linking_test.go` (add tests)

**Interfaces:**
- Consumes: `payment_requests` (Phase 2); `store.Request` (Phase 2); `ReserveRequest`, `HoldRequest`, `RecordPaymentForRequest`, `AcceptPartial` (Tasks 3, 6, 8, 9).
- Produces:
  - `Request.ProcessingByName string` (joined from `users`, `""` when unreserved) — consumed by the queue, the picker and the conflict screen.
  - `type LinkableOptions struct { Scope string; ViewerID int64; Status string; Query string; Limit int; Now time.Time }` — `Status` is `""`/`"approved"` (default, takeable only), `"processing"`, `"hold"`, `"partial_review"` or `"paid"`; `Now` zero means `time.Now()`.
  - `type LinkableCounts struct { Approved, Processing, Hold, PartialReview, Paid, ReservedByMe, ReservedByOthers, StaleReservations int; ApprovedAmount int64 }`
  - `type LinkableSet struct { Available []Request; Unavailable []Request; Counts LinkableCounts }`
  - `func (s *Store) LinkablePaymentRequests(ctx context.Context, opts LinkableOptions) (LinkableSet, error)`
  - `const StaleReservation = 24 * time.Hour`

- [x] **Step 1: Write the failing test**

```go
func TestLinkablePaymentRequestsFilterAndSearch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	linkable := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)  // approved, unclaimed, not held
	held := seedApprovedRequest(t, s, ctx, 2, req.ID, mgrID, headID, 600000, 600000)      // on hold → not takeable
	reserved := seedApprovedRequest(t, s, ctx, 3, req.ID, mgrID, headID, 700000, 700000)  // reserved → not takeable
	pending := seedApprovedRequest(t, s, ctx, 4, req.ID, mgrID, headID, 800000, 800000)   // not approved → invisible here
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET on_hold=1 WHERE id=?`, held); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, acc, reserved); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET status='pending' WHERE id=?`, pending); err != nil {
		t.Fatal(err)
	}

	all, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID})
	if err != nil {
		t.Fatalf("linkable: %v", err)
	}
	if len(all.Available) != 1 || all.Available[0].ID != linkable {
		t.Fatalf("takeable set = %+v, want only request %d (S3)", all.Available, linkable)
	}
	// The taken/held rows are visible but never takeable — this is what the
	// picker renders as .co.is-taken instead of silently hiding them.
	if len(all.Unavailable) != 2 {
		t.Fatalf("unavailable set = %+v, want the held and the reserved request", all.Unavailable)
	}
	for _, r := range all.Unavailable {
		if r.ID == pending {
			t.Fatal("a non-approved request leaked into the queue")
		}
	}

	// Search narrows both sets, by number, payee and amount (S4).
	byNumber, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID, Query: "PR-2026-000001"})
	if err != nil || len(byNumber.Available) != 1 || byNumber.Available[0].ID != linkable {
		t.Fatalf("search by number = %+v, err=%v", byNumber.Available, err)
	}
	byPayee, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID, Query: "Acme Landlord"})
	if err != nil || len(byPayee.Available) != 1 {
		t.Fatalf("search by payee = %+v, err=%v", byPayee.Available, err)
	}
	byAmount, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID, Query: "500000"})
	if err != nil || len(byAmount.Available) != 1 {
		t.Fatalf("search by amount = %+v, err=%v", byAmount.Available, err)
	}
	none, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID, Query: "nonexistent-xyz"})
	if err != nil || len(none.Available) != 0 || len(none.Unavailable) != 0 {
		t.Fatalf("search miss = %+v, err=%v", none, err)
	}
}

// TestLinkablePaymentRequestsTabsCountsAndReserver builds one of every state the
// accounts queue can show and asserts each .segmented tab, each .metric-strip
// metric and the reserver identity the rows print. Without these the queue is
// unbuildable.
func TestLinkablePaymentRequestsTabsCountsAndReserver(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	deepakID, err := s.CreateUser(ctx, "deepak@example.com", "Deepak Menon", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	deepak, _ := s.UserByID(ctx, deepakID)
	mgr, _ := s.UserByID(ctx, mgrID)

	open1 := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	open2 := seedApprovedRequest(t, s, ctx, 2, req.ID, mgrID, headID, 300000, 300000)
	mine := seedApprovedRequest(t, s, ctx, 3, req.ID, mgrID, headID, 100000, 100000)
	stale := seedApprovedRequest(t, s, ctx, 4, req.ID, mgrID, headID, 78000, 78000)
	theirs := seedApprovedRequest(t, s, ctx, 5, req.ID, mgrID, headID, 33500, 33500)
	onHold := seedApprovedRequest(t, s, ctx, 6, req.ID, mgrID, headID, 25000, 25000)
	inReview := seedApprovedRequest(t, s, ctx, 7, req.ID, mgrID, headID, 95000, 95000)
	paidClean := seedApprovedRequest(t, s, ctx, 8, req.ID, mgrID, headID, 41300, 41300)
	paidPartial := seedApprovedRequest(t, s, ctx, 9, req.ID, mgrID, headID, 60000, 60000)

	for _, id := range []int64{mine, stale, inReview, paidClean, paidPartial} {
		if err := s.ReserveRequest(ctx, acc, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ReserveRequest(ctx, deepak, theirs); err != nil {
		t.Fatal(err)
	}
	if err := s.HoldRequest(ctx, acc, onHold, "waiting on the requester"); err != nil {
		t.Fatal(err)
	}
	// One reservation is 26 hours old; the queue banner counts it as stale.
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET processing_at=? WHERE id=?`,
		time.Date(2026, 7, 24, 12, 40, 0, 0, time.UTC).Format("2006-01-02 15:04:05"), stale); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, acc, inReview, PaymentInput{HeadID: headID, PaidOn: "2026-07-23", Amount: 60000, VendorPayee: "Nova"}, "partial", "700 of 1000 copies delivered", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, acc, paidClean, PaymentInput{HeadID: headID, PaidOn: "2026-07-24", Amount: 41300, VendorPayee: "Nova"}, "settled", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordPaymentForRequest(ctx, acc, paidPartial, PaymentInput{HeadID: headID, PaidOn: "2026-07-23", Amount: 60000, VendorPayee: "Nova"}, "partial", "balance later", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptPartial(ctx, mgr, paidPartial, ""); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 25, 14, 40, 0, 0, time.UTC)
	set, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID, Now: now})
	if err != nil {
		t.Fatalf("linkable: %v", err)
	}
	c := set.Counts
	// .segmented tabs.
	if c.Approved != 2 {
		t.Fatalf("Approved tab = %d, want 2", c.Approved)
	}
	if c.Processing != 2 { // mine + stale; theirs is another's, counted separately below
		t.Fatalf("Processing tab = %d, want 2", c.Processing)
	}
	if c.Hold != 1 {
		t.Fatalf("On hold tab = %d, want 1", c.Hold)
	}
	if c.PartialReview != 1 {
		t.Fatalf("Partial review tab = %d, want 1", c.PartialReview)
	}
	if c.Paid != 2 { // completed + completed_partial both count as paid (G14)
		t.Fatalf("Paid tab = %d, want 2 (completed and completed_partial)", c.Paid)
	}
	// .metric-strip metrics.
	if c.ApprovedAmount != 800000 {
		t.Fatalf("approved total = %d, want 800000 (₹8,000.00)", c.ApprovedAmount)
	}
	if c.ReservedByMe != 2 {
		t.Fatalf("reserved by me = %d, want 2", c.ReservedByMe)
	}
	if c.ReservedByOthers != 1 {
		t.Fatalf("reserved by others = %d, want 1", c.ReservedByOthers)
	}
	if c.StaleReservations != 1 {
		t.Fatalf("stale reservations = %d, want 1 (the 26-hour-old one)", c.StaleReservations)
	}

	// Rows carry who holds the reservation and since when, which is what
	// "Reserved by you · 14:02" and "Reserved by Deepak M" render from.
	byStatus, err := s.LinkablePaymentRequests(ctx, LinkableOptions{Scope: "all", ViewerID: acc.ID, Status: "processing", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	found := map[int64]Request{}
	for _, r := range append(append([]Request{}, byStatus.Available...), byStatus.Unavailable...) {
		found[r.ID] = r
	}
	if r, ok := found[theirs]; !ok || r.ProcessingByName != "Deepak Menon" {
		t.Fatalf("reserved-by-others row = %+v, want ProcessingByName Deepak Menon", r)
	}
	if r, ok := found[theirs]; !ok || r.ProcessingAt == nil {
		t.Fatalf("reserved row has no ProcessingAt; the queue cannot print the reserved-at time")
	}
	// A row someone else holds is never offered as takeable.
	for _, r := range byStatus.Available {
		if r.ID == theirs {
			t.Fatal("a request reserved by another accountant was offered as takeable")
		}
	}
	if _, ok := found[open1]; ok {
		t.Fatalf("status=processing returned the approved request %d", open1)
	}
	_ = open2
}
```

> `TestLinkablePaymentRequestsTabsCountsAndReserver` needs `"time"` in `linking_test.go`'s import block; add it with this task.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestLinkablePaymentRequests -v`
Expected: FAIL — `undefined: LinkableOptions` / `undefined: LinkableSet` and `s.LinkablePaymentRequests undefined`; `FAIL fervidbudget/internal/store [build failed]`.

- [x] **Step 3: Write minimal implementation**

In `internal/store/models.go`, add `ProcessingByName string` to `Request` (immediately after `ProcessingAt`) and the Phase-3 queue types:

```go
// StaleReservation is how long a reservation may sit before the queue nudges.
// Nothing is ever released automatically — a bank transfer may be under way.
const StaleReservation = 24 * time.Hour

type LinkableOptions struct {
	Scope    string // "own" | "assigned" | "all", from auth.Scope
	ViewerID int64
	Status   string // "" or "approved" (takeable) | "processing" | "hold" | "partial_review" | "paid"
	Query    string
	Limit    int
	Now      time.Time // injected clock; zero means time.Now()
}

// LinkableCounts feeds the queue's .segmented tabs and .metric-strip. It is
// always computed over the caller's whole scope, never over the current page or
// the current search, so the tab numbers do not move as you type.
type LinkableCounts struct {
	Approved          int
	Processing        int
	Hold              int
	PartialReview     int
	Paid              int
	ReservedByMe      int
	ReservedByOthers  int
	StaleReservations int
	ApprovedAmount    int64
}

// LinkableSet splits what the caller may act on from what they may only see.
// Unavailable is deliberately not hidden: the picker renders it as .co.is-taken
// so an accountant learns the request exists and who holds it.
type LinkableSet struct {
	Available   []Request
	Unavailable []Request
	Counts      LinkableCounts
}
```

In `internal/store/store.go`, replace `LinkablePaymentRequests`:

```go
// LinkablePaymentRequests powers the Accounts queue and the request picker.
// Available is the S3 set — approved · unclaimed · not on hold — the only rows
// that may be reserved. Unavailable is everything else the tab asked for that
// the caller may see but not take. Both are searchable by number / requester /
// payee / project / head / amount (S4). Counts always span the caller's scope.
func (s *Store) LinkablePaymentRequests(ctx context.Context, opts LinkableOptions) (LinkableSet, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	var out LinkableSet

	scopeSQL, scopeArgs := "", []any(nil)
	switch opts.Scope {
	case "own":
		scopeSQL, scopeArgs = ` AND r.requester_id=?`, []any{opts.ViewerID}
	case "assigned":
		scopeSQL, scopeArgs = ` AND r.manager_id=?`, []any{opts.ViewerID}
	}

	// Counts first: one aggregate pass over the scope, independent of the tab,
	// the search and the limit.
	countQ := `SELECT
		COALESCE(SUM(CASE WHEN r.status='approved' AND r.processing_by IS NULL AND r.on_hold=0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='approved' AND r.processing_by IS NULL AND r.on_hold=0 THEN COALESCE(r.approved_amount, r.amount) ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='processing' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.on_hold=1 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='partial_review' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status IN ('completed','completed_partial') THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='processing' AND r.processing_by=? THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='processing' AND r.processing_by IS NOT NULL AND r.processing_by<>? THEN 1 ELSE 0 END),0)
		FROM payment_requests r WHERE 1=1` + scopeSQL
	countArgs := append([]any{opts.ViewerID, opts.ViewerID}, scopeArgs...)
	if err := s.db.QueryRowContext(ctx, countQ, countArgs...).Scan(
		&out.Counts.Approved, &out.Counts.ApprovedAmount, &out.Counts.Processing,
		&out.Counts.Hold, &out.Counts.PartialReview, &out.Counts.Paid,
		&out.Counts.ReservedByMe, &out.Counts.ReservedByOthers); err != nil {
		return out, err
	}

	q := `SELECT r.id, r.number, r.status, r.amount, r.approved_amount, COALESCE(r.vendor_payee,''),
		COALESCE(p.name,''), COALESCE(h.name,''), r.requester_id, COALESCE(u.name,''), r.manager_id,
		r.on_hold, COALESCE(r.hold_reason,''), r.processing_by, COALESCE(pu.name,''), r.processing_at,
		r.head_id, COALESCE(r.needed_by,''), r.treatment, r.type, r.approved_at
		FROM payment_requests r
		LEFT JOIN projects p ON p.id=r.project_id
		LEFT JOIN heads h ON h.id=r.head_id
		LEFT JOIN users pu ON pu.id=r.processing_by
		JOIN users u ON u.id=r.requester_id
		WHERE `
	var args []any
	switch opts.Status {
	case "", "approved":
		q += `r.status='approved' AND r.processing_by IS NULL AND r.on_hold=0`
	case "processing":
		q += `r.status='processing'`
	case "hold":
		q += `r.on_hold=1`
	case "partial_review":
		q += `r.status='partial_review'`
	case "paid":
		q += `r.status IN ('completed','completed_partial')`
	default:
		return out, fmt.Errorf("%w: unknown queue tab %q", ErrValidation, opts.Status)
	}
	q += scopeSQL
	args = append(args, scopeArgs...)
	if search := strings.ToLower(strings.TrimSpace(opts.Query)); search != "" {
		q += ` AND (lower(r.number) LIKE ? ESCAPE '\' OR lower(u.name) LIKE ? ESCAPE '\' OR lower(COALESCE(r.vendor_payee,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(p.name,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(h.name,'')) LIKE ? ESCAPE '\' OR CAST(r.amount AS TEXT) LIKE ? ESCAPE '\')`
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
		needle := "%" + esc + "%"
		args = append(args, needle, needle, needle, needle, needle, needle)
	}
	q += ` ORDER BY r.approved_at, r.id`
	if opts.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, opts.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Request
		var approved, processingBy, headID sql.NullInt64
		var processingAt, approvedAt sql.NullTime
		var onHold int
		if err := rows.Scan(&r.ID, &r.Number, &r.Status, &r.Amount, &approved, &r.VendorPayee,
			&r.Project, &r.Head, &r.RequesterID, &r.RequesterName, &r.ManagerID,
			&onHold, &r.HoldReason, &processingBy, &r.ProcessingByName, &processingAt,
			&headID, &r.NeededBy, &r.Treatment, &r.Type, &approvedAt); err != nil {
			return out, err
		}
		if approved.Valid {
			v := approved.Int64
			r.ApprovedAmount = &v
		}
		if processingBy.Valid {
			v := processingBy.Int64
			r.ProcessingBy = &v
		}
		if headID.Valid {
			v := headID.Int64
			r.HeadID = &v
		}
		if processingAt.Valid {
			v := processingAt.Time
			r.ProcessingAt = &v
			if r.Status == "processing" && processingBy.Valid && processingBy.Int64 == opts.ViewerID && now.Sub(v) >= StaleReservation {
				out.Counts.StaleReservations++
			}
		}
		if approvedAt.Valid {
			v := approvedAt.Time
			r.ApprovedAt = &v
		}
		r.OnHold = onHold == 1
		if r.Status == "approved" && !processingBy.Valid && !r.OnHold {
			out.Available = append(out.Available, r)
		} else {
			out.Unavailable = append(out.Unavailable, r)
		}
	}
	return out, rows.Err()
}
```

> Two notes. **Staleness** is counted only over rows the query returned, which is correct for every tab the mockup shows the banner on (Approved and Processing both include the caller's reservations); if a later screen needs it on a tab that excludes them, move the count into the aggregate pass with an injected cutoff parameter rather than reintroducing `julianday('now')`. **Availability** is re-derived from the row itself, not from the tab, so the `processing`/`hold` tabs can never hand the UI a "Take for processing" button.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestLinkablePaymentRequests -v`
Expected: PASS (2 tests). Then run the whole store suite: `go test -race ./internal/store/` — Expected: `ok`.

- [x] **Step 5: Commit**

```bash
git add internal/store/models.go internal/store/store.go internal/store/linking_test.go
git commit -m "feat(store): LinkablePaymentRequests with tab filter, counts and the taken set"
```

---

### Task 11: HTTP — reservation entry points, permission verbs, and the **reservation-conflict screen** (S1, S2, S14, S15/X5, **G11**, **G15**)

**Amended.** Two changes to the original task. First, reservation moves onto its own permission verbs: spec D2 adds `reservation`{`reserve`,`release`,`reassign`} to the canonical vocabulary precisely so that "may take work" and "may take work off someone else" stop being the same grant. Second, **G15**: losing the race is not an error, it is a screen. `accounts-reservation-conflict.html` names the winner, states in as many words that nothing was saved, shows the request with `.pill.processing` and a `.waiting` timestamp, offers an `.a-list` of three next actions and closes with an `.action-bar`. `respondError(409, …)` renders none of that, so this task adds a `reservation_conflict` template rendered through `renderStatus(409, …)`.

The picker, queue and entry screens are Tasks 12–14; this task lands the routes and the conflict screen they all funnel into.

**Files:**
- Modify: `internal/app/app.go` (routes; `requestRecordPayment`, `paymentCreate`; `PageData` Phase-3 fields)
- Modify: `internal/app/templates.go` (new `reservation_conflict`)
- Modify: `internal/app/app_integration_test.go` (add helpers + tests; add `"fmt"` import; retarget `TestPaymentErrorRetainsInputAndUsesHumanModes` — see note)

**Interfaces:**
- Consumes: `ReserveRequest`, `RecordPaymentForRequest`, `LinkablePaymentRequests`, `Request` (with `ProcessingByName`, Task 10), `auth.Manager.Can`/`Scope`, `renderStatus`.
- Produces: routes `POST /requests/{id}/record-payment` (gated `reservation:reserve`), reworked `POST /payments`; `PageData` fields `Linkable store.LinkableSet`, `Tab string` (Phase 2 already provides `Requests`, `Request2`, `Comments`, `RequestAtts`, `Counts` — do not redeclare them).

- [ ] **Step 1: Write the failing test** (add to `internal/app/app_integration_test.go`; add `"fmt"` to its imports)

```go
func (s *appTestServer) seedApprovedRequest(seq int, requesterID, managerID, headID, amount int64) int64 {
	s.t.Helper()
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		s.t.Fatal(err)
	}
	res, err := s.st.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,project_id,head_id,amount,purpose,vendor_payee,requester_id,manager_id,approved_amount,approved_by,approved_at)
		VALUES(?,'approved','budget','vendor_invoice',?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), projectID, headID, amount, "Office rent", "Acme Landlord", requesterID, managerID, amount, managerID)
	if err != nil {
		s.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func requestStatusApp(t *testing.T, s *appTestServer, id int64) string {
	t.Helper()
	var st string
	if err := s.st.DB().QueryRow(`SELECT status FROM payment_requests WHERE id=?`, id).Scan(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestReservationEntryPointAndPrefill(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Reserve")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	resp := s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{})
	requireStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != fmt.Sprintf("/payments/new?request=%d", reqID) {
		t.Fatalf("reserve redirect = %q", loc)
	}
	_ = responseBody(t, resp)
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status after reserve = %q, want processing", got)
	}
	// Prefilled form carries the approved amount and payee (S14).
	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/payments/new?request=%d", reqID), nil, ""))
	if !strings.Contains(body, "Acme Landlord") || !strings.Contains(body, "5,000.00") {
		t.Fatalf("prefilled form missing request data: %s", body)
	}
	if !strings.Contains(body, fmt.Sprintf(`name="request_id" value="%d"`, reqID)) {
		t.Fatalf("prefilled form missing request linkage: %s", body)
	}
}

// TestReservationConflictRendersScreenNotErrorPage is the G15 proof.
func TestReservationConflictRendersScreenNotErrorPage(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Taken")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 3350000)
	// Someone already reserved it.
	if err := s.st.ReserveRequest(s.ctx, admin, reqID); err != nil {
		t.Fatal(err)
	}
	// A second accountant tries via the button.
	hash, _ := auth.HashPassword("SecondPass1234")
	if _, err := s.st.CreateUser(s.ctx, "second@example.test", "Second Acct", hash, "admin", true); err != nil {
		t.Fatal(err)
	}
	s.login("second@example.test", "SecondPass1234")
	resp := s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{})
	requireStatus(t, resp, http.StatusConflict)
	body := responseBody(t, resp)

	// It is a screen, not the generic error page.
	if strings.Contains(body, `class="error-page"`) || strings.Contains(body, "Request ID:") {
		t.Fatalf("conflict rendered the generic error page: %s", body)
	}
	for _, want := range []string{
		`class="banner bad"`,               // names the winner, says nothing was saved
		"Test Admin took this request",     // who won
		"Nothing you typed has been saved", // and that no payment was created
		`class="req-head"`,                 // the request in context
		`class="pill processing"`,          // its state
		`class="waiting"`,                  // reserved-at line
		`class="a-list"`,                   // what you can do next
		"Go back to the queue",
		"Ask for it to be reassigned",
		"Open the request read-only",
		`class="action-bar"`,
		"PR-2026-000001",
		"33,500.00",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("conflict screen missing %q:\n%s", want, body)
		}
	}
	// The reservation is untouched by the loser.
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status = %q, want processing", got)
	}
}

func TestPaymentCreateRequiresReservedRequest(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("X5")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	// No request_id at all → refused.
	form := url.Values{"head_id": {strconvFormat(headID)}, "paid_on": {"2026-06-15"}, "amount": {"100.00"}, "settlement": {"settled"}}
	resp := s.postForm("/payments", form)
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)
	// A request the caller has NOT reserved → refused.
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	form.Set("request_id", strconvFormat(reqID))
	resp = s.postForm("/payments", form)
	if resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("unreserved link accepted: %d", resp.StatusCode)
	}
	_ = responseBody(t, resp)
	var n int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("request-less/unreserved payment wrote %d rows (X5 broken)", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run 'TestReservationEntryPointAndPrefill|TestReservationConflictRendersScreenNotErrorPage|TestPaymentCreateRequiresReservedRequest' -v`
Expected: FAIL — no `POST /requests/{id}/record-payment` route (404 instead of 303); no `reservation_conflict` template, so the conflict assertions fail on the generic error page; `paymentCreate` still creates free-standing payments so a row is written.

- [ ] **Step 3: Write minimal implementation**

In `internal/app/app.go`, add the Phase-3 fields to `PageData` (Phase 2 already supplies `Requests`, `Request2`, `Comments`, `RequestAtts`, `Counts`):

```go
	Linkable    store.LinkableSet
	Tab         string
	RecentPaid  []store.PaidRequestRow
	Settlement  SettlementPreview
	ReserveMine bool
```

In `App.routes`, register the reservation entry point on its own verb:

```go
	mux.Handle("POST /requests/{id}/record-payment", a.auth.RequirePermission("reservation", "reserve", http.HandlerFunc(a.withCSRF(a.requestRecordPayment))))
```

Add `requestRecordPayment` and the shared conflict responder, and rework `paymentCreate`:

```go
// reservationConflict renders G15: the losing accountant gets a screen naming the
// winner, not an error page. It is the single place a lost race is presented, so
// the queue, the picker and the payment form all say the same thing.
func (a *App) reservationConflict(w http.ResponseWriter, r *http.Request, req store.Request, cause error) {
	holder := "someone else"
	if req.ProcessingByName != "" {
		holder = req.ProcessingByName
	}
	a.log.WarnContext(r.Context(), "reservation conflict", "request_id", requestID(r), "request", req.ID, "holder", holder, "error", cause)
	a.renderStatus(w, r, http.StatusConflict, "reservation_conflict", PageData{
		Title:    "Already taken",
		Request2: req,
		Error:    holder + " took this request before you.",
	})
}

func (a *App) requestRecordPayment(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	u := auth.CurrentUser(r)
	if err := a.st.ReserveRequest(r.Context(), u, id); err != nil {
		if errors.Is(err, store.ErrForbidden) {
			req, rerr := a.st.Request(r.Context(), id)
			if rerr != nil {
				a.respondStoreError(w, r, rerr)
				return
			}
			a.reservationConflict(w, r, req, err)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/new?request=%d", id), http.StatusSeeOther)
}

func (a *App) paymentCreate(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	requestID := parseID(r.FormValue("request_id"))
	if requestID == 0 {
		a.respondError(w, r, http.StatusBadRequest, "Payments must be linked to an approved request.", nil)
		return
	}
	in, err := paymentInput(r)
	settlement := r.FormValue("settlement")
	partialReason := r.FormValue("partial_reason")
	var attachment *store.AttachmentInput
	var attachmentPath string
	if err == nil {
		attachment, attachmentPath, err = a.stageUploadedAttachment(r)
	}
	var payID int64
	if err == nil {
		payID, err = a.st.RecordPaymentForRequest(r.Context(), u, requestID, in, settlement, partialReason, attachment)
	}
	if err != nil {
		removeStagedAttachment(a.log, r, attachmentPath)
		// A double-confirm (back button, double tap) must not look like a failure:
		// the payment this request needed already exists, so go to it.
		if pay, perr := a.st.PaymentForRequest(r.Context(), requestID); perr == nil {
			http.Redirect(w, r, fmt.Sprintf("/payments/%d", pay.ID), http.StatusSeeOther)
			return
		}
		a.settlementError(w, r, requestID, in, r.FormValue("amount"), settlement, partialReason, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", payID), http.StatusSeeOther)
}
```

> `settlementError` re-renders the settlement sheet rather than a full error page; it is written in Task 15, which owns the sheet template. Until then, stub it as `a.respondStoreError(w, r, err)` so this task compiles and its three tests pass, and replace the stub in Task 15 — that swap is asserted there.

In `internal/app/templates.go`, add the conflict screen. It reproduces `mockups/screens/accounts-reservation-conflict.html`:

```html
{{define "reservation_conflict"}}
{{template "top" .}}
<div class="banner bad">
  <span class="b-ico">✕</span>
  <div>
    <b>{{.Request2.ProcessingByName}} took this request before you</b>
    <p>{{.Request2.Number}} is now reserved by {{.Request2.ProcessingByName}}. Nothing you typed has been saved, and no payment was created.</p>
  </div>
</div>

<div class="req-head">
  <div class="rh-top"><span class="rh-no">{{.Request2.Number}}</span><span class="rh-amt">{{money .Request2.Amount}}</span></div>
  <h1>{{.Request2.VendorPayee}}{{if .Request2.Purpose}} — {{.Request2.Purpose}}{{end}}</h1>
  <p class="rh-meta">{{.Request2.Project}} / {{.Request2.Head}}{{if .Request2.ApprovedAt}} · approved {{datep .Request2.ApprovedAt}}{{end}}</p>
  <div class="rh-status">
    <span class="pill processing">Processing — {{.Request2.ProcessingByName}}</span>
    <span class="waiting">Reserved {{datep .Request2.ProcessingAt}}</span>
  </div>
</div>

<div class="card">
  <div class="card-head"><h2>What you can do</h2></div>
  <div class="a-list">
    <a href="/requests/to-pay"><span class="al-main"><b>Go back to the queue</b><small>Other approved requests are unclaimed</small></span><span class="al-amt">→</span></a>
    {{if .Perms.Can "reservation" "reassign"}}<a href="/requests/{{.Request2.ID}}/reservation"><span class="al-main"><b>Ask for it to be reassigned</b><small>{{.Request2.ProcessingByName}} is told and must confirm no payment was started</small></span><span class="al-amt">→</span></a>{{end}}
    <a href="/requests/{{.Request2.ID}}"><span class="al-main"><b>Open the request read-only</b><small>You can see it and comment, but not pay it</small></span><span class="al-amt">→</span></a>
  </div>
</div>

<div class="action-bar">
  <span class="row-end"></span>
  <a class="btn outline" href="/payments/new">Pick another request</a>
  <a class="btn primary" href="/requests/to-pay">Back to queue</a>
</div>
{{template "bottom" .}}
{{end}}
```

> The mockup shows the reassign entry unconditionally with the note "Needs reassign permission". Client-side hiding of a permitted-only action leaks it (Phase 0 constraint), so the entry is gated with `.Perms.Can "reservation" "reassign"` and the note drops to the sub-line for those who do have it. The route it points at is built in Task 18.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run 'TestReservationEntryPointAndPrefill|TestReservationConflictRendersScreenNotErrorPage|TestPaymentCreateRequiresReservedRequest' -v`
Expected: PASS (3 tests). `TestReservationEntryPointAndPrefill`'s prefill assertions need Task 14's payment-entry screen; run it again at the end of Task 14 and expect it still green.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): reservation entry point on the reservation verb and the conflict screen"
```

---

### Task 12: Accounts queue screen — metrics, `.segmented` tabs and `t-cards` rows

**New task.** Rebuilds `/requests/to-pay` against `mockups/screens/accounts-queue.html`: a `.page-banner`, a four-metric `.metric-strip`, a five-tab `.segmented` bar with counts, the `.m-filters` search row, the stale-reservation `.banner.brand`, and `table.t-cards` rows whose action changes with state — "Take for processing" when takeable, "Resume" when yours, "Reassign" when someone else's, "Read reply" when held, "View" in partial review.

**Files:**
- Modify: `internal/app/app.go` (`accountsQueue`; route; two template helpers)
- Modify: `internal/app/templates.go` (new `accounts_queue`)
- Modify: `internal/app/app_integration_test.go` (add tests)

**Interfaces:**
- Consumes: `LinkablePaymentRequests` (Task 10), `auth.Scope`, `Perms.Can`.
- Produces: route `GET /requests/to-pay` (gated `payment:process`); template helpers `hhmm(t *time.Time) string` ("14:02"), `since(t *time.Time) string` ("26 h", "41 m") and `reservedLabel(t *time.Time) string`, which is the clock time while the reservation is same-day and the elapsed time once it is older — exactly the "· 14:02" / "· 26 h" split the mockup shows.

- [ ] **Step 1: Write the failing test**

```go
func TestAccountsQueueRendersMetricsTabsAndRowActions(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Queue")
	open := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 23500000)
	mine := s.seedApprovedRequest(2, admin.ID, admin.ID, headID, 10000000)
	held := s.seedApprovedRequest(3, admin.ID, admin.ID, headID, 2500000)
	if err := s.st.ReserveRequest(s.ctx, admin, mine); err != nil {
		t.Fatal(err)
	}
	if err := s.st.HoldRequest(s.ctx, admin, held, "await vendor GST"); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/requests/to-pay", nil, ""))
	for _, want := range []string{
		`class="metric-strip"`,
		"Approved, unclaimed",
		"Reserved by you",
		"Reserved by others",
		"On hold",
		`class="segmented"`,
		"Approved <span class=\"n\">1</span>",
		"Processing <span class=\"n\">1</span>",
		"On hold <span class=\"n\">1</span>",
		"Partial review <span class=\"n\">0</span>",
		"Paid <span class=\"n\">0</span>",
		`class="m-filters"`,
		`<table class="t-cards">`,
		`data-label="Payee"`,
		"Take for processing",
		"PR-2026-000001",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("queue missing %q:\n%s", want, body)
		}
	}
	// A reserved-by-you row offers Resume, never a second Take.
	if strings.Count(body, "Take for processing") != 1 {
		t.Fatalf("Take offered for a non-takeable row:\n%s", body)
	}
	// Design system, not the legacy markup.
	if strings.Contains(body, `class="badge`) {
		t.Fatalf("queue still renders .badge (D5):\n%s", body)
	}

	// The Processing tab shows the reserved row and hides the open one.
	proc := responseBody(t, s.request(http.MethodGet, "/requests/to-pay?tab=processing", nil, ""))
	if !strings.Contains(proc, "PR-2026-000002") || strings.Contains(proc, "PR-2026-000001") {
		t.Fatalf("processing tab wrong:\n%s", proc)
	}
	if !strings.Contains(proc, "Resume") {
		t.Fatalf("processing tab has no Resume action:\n%s", proc)
	}
	// The hold tab shows the held row, which is never takeable.
	hold := responseBody(t, s.request(http.MethodGet, "/requests/to-pay?tab=hold", nil, ""))
	if !strings.Contains(hold, "PR-2026-000003") || strings.Contains(hold, "Take for processing") {
		t.Fatalf("hold tab wrong:\n%s", hold)
	}
	// An unknown tab is rejected, not silently treated as Approved.
	requireStatus(t, s.request(http.MethodGet, "/requests/to-pay?tab=nonsense", nil, ""), http.StatusBadRequest)
	_ = open
}

func TestAccountsQueueSearchNarrowsRows(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("QueueSearch")
	s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.seedApprovedRequest(2, admin.ID, admin.ID, headID, 600000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/requests/to-pay?q=PR-2026-000002", nil, ""))
	if !strings.Contains(body, "PR-2026-000002") || strings.Contains(body, "PR-2026-000001") {
		t.Fatalf("queue search did not narrow:\n%s", body)
	}
	// Counts are scope-wide, not search-scoped: the tab still says 2.
	if !strings.Contains(body, "Approved <span class=\"n\">2</span>") {
		t.Fatalf("search distorted the tab counts:\n%s", body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run 'TestAccountsQueue' -v`
Expected: FAIL — `GET /requests/to-pay` is unregistered, so both tests get the 404 page and fail on the first missing marker (`class="metric-strip"`).

- [ ] **Step 3: Write minimal implementation**

In `internal/app/app.go`, register the route and add the handler:

```go
	mux.Handle("GET /requests/to-pay", a.auth.RequirePermission("payment", "process", http.HandlerFunc(a.accountsQueue)))
```

```go
var queueTabs = []struct{ Key, Label string }{
	{"approved", "Approved"}, {"processing", "Processing"}, {"hold", "On hold"},
	{"partial_review", "Partial review"}, {"paid", "Paid"},
}

func (a *App) accountsQueue(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "approved"
	}
	known := false
	for _, t := range queueTabs {
		if t.Key == tab {
			known = true
		}
	}
	if !known {
		a.respondError(w, r, http.StatusBadRequest, "That queue tab does not exist.", nil)
		return
	}
	set, err := a.st.LinkablePaymentRequests(r.Context(), store.LinkableOptions{
		Scope: a.auth.Scope(u, "request"), ViewerID: u.ID, Status: tab, Query: r.URL.Query().Get("q"),
	})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "accounts_queue", PageData{Title: "Payment queue", Linkable: set, Tab: tab, Query: r.URL.Query().Get("q")})
}
```

Add the two display helpers to the `template.FuncMap` in `New` (display only; money and state stay server-side):

```go
		"hhmm": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("15:04")
		},
		"since": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			d := time.Since(*t)
			if d < time.Hour {
				return fmt.Sprintf("%d m", int(d.Minutes()))
			}
			return fmt.Sprintf("%d h", int(d.Hours()))
		},
		// reservedLabel: the clock time while the reservation is same-day, the
		// elapsed time once it is older. "· 14:02" answers "when did I start?";
		// "· 26 h" answers "how long has this been sitting?".
		"reservedLabel": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			if time.Since(*t) < 24*time.Hour {
				return t.Format("15:04")
			}
			return fmt.Sprintf("%d h", int(time.Since(*t).Hours()))
		},
```

In `internal/app/templates.go`, add the queue, reproducing `accounts-queue.html`:

```html
{{define "accounts_queue"}}
{{template "top" .}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">Accounts</div>
    <h1>Payment queue</h1>
    <p class="sub">{{.Linkable.Counts.Approved}} approved and unclaimed · {{money .Linkable.Counts.ApprovedAmount}} · one request, one payment</p>
  </div>
  <div class="pb-actions">
    <a class="btn primary" href="/payments/new">＋ Record a payment</a>
  </div>
</section>

<div class="metric-strip">
  <div class="metric"><span class="metric-label">Approved, unclaimed</span><span class="metric-value">{{.Linkable.Counts.Approved}}</span><span class="metric-foot">{{money .Linkable.Counts.ApprovedAmount}}</span></div>
  <div class="metric warn"><span class="metric-label">Reserved by you</span><span class="metric-value">{{.Linkable.Counts.ReservedByMe}}</span><span class="metric-foot">{{if .Linkable.Counts.StaleReservations}}{{.Linkable.Counts.StaleReservations}} open over a day{{else}}All recent{{end}}</span></div>
  <div class="metric"><span class="metric-label">Reserved by others</span><span class="metric-value">{{.Linkable.Counts.ReservedByOthers}}</span><span class="metric-foot">Visible, not yours</span></div>
  <div class="metric"><span class="metric-label">On hold</span><span class="metric-value">{{.Linkable.Counts.Hold}}</span><span class="metric-foot">Waiting on the requester</span></div>
</div>

<div class="segmented">
  <a {{if eq .Tab "approved"}}class="is-active"{{end}} href="/requests/to-pay?tab=approved">Approved <span class="n">{{.Linkable.Counts.Approved}}</span></a>
  <a {{if eq .Tab "processing"}}class="is-active"{{end}} href="/requests/to-pay?tab=processing">Processing <span class="n">{{.Linkable.Counts.Processing}}</span></a>
  <a {{if eq .Tab "hold"}}class="is-active"{{end}} href="/requests/to-pay?tab=hold">On hold <span class="n">{{.Linkable.Counts.Hold}}</span></a>
  <a {{if eq .Tab "partial_review"}}class="is-active"{{end}} href="/requests/to-pay?tab=partial_review">Partial review <span class="n">{{.Linkable.Counts.PartialReview}}</span></a>
  <a {{if eq .Tab "paid"}}class="is-active"{{end}} href="/requests/to-pay?tab=paid">Paid <span class="n">{{.Linkable.Counts.Paid}}</span></a>
</div>

<form class="m-filters" method="get" action="/requests/to-pay">
  <input type="hidden" name="tab" value="{{.Tab}}">
  <span class="m-search"><input name="q" value="{{.Query}}" placeholder="Number, payee, project…" aria-label="Search queue"></span>
  <button class="btn filter-btn">Search</button>
</form>

{{if .Linkable.Counts.StaleReservations}}
<div class="banner brand">
  <span class="b-ico">◷</span>
  <div>
    <b>You have {{.Linkable.Counts.ReservedByMe}} requests reserved</b>
    <p>{{.Linkable.Counts.StaleReservations}} has been open for more than a day. Finish it or release it so someone else can.</p>
  </div>
  <span class="b-actions"><a class="btn small outline" href="/requests/to-pay?tab=processing">Review</a></span>
</div>
{{end}}

<div class="table-wrap">
  <table class="t-cards">
    <thead><tr><th>Request</th><th>Payee</th><th>Project / head</th><th class="num">Amount</th><th>Needed by</th><th>Status</th><th class="c">Action</th></tr></thead>
    <tbody>
      {{range .Linkable.Available}}
      <tr>
        <td class="t-lead" data-label="Request"><a href="/requests/{{.ID}}">{{.Number}}</a> <span class="t-sub">{{.RequesterName}} · approved {{datep .ApprovedAt}}</span></td>
        <td data-label="Payee">{{.VendorPayee}}</td>
        <td data-label="Project / head">{{if eq .Treatment "recoverable"}}<span class="pill recoverable">Recoverable</span>{{else}}{{.Project}} / {{.Head}}{{end}}</td>
        <td class="num" data-label="Amount">{{if .ApprovedAmount}}{{money (deref .ApprovedAmount)}}{{else}}{{money .Amount}}{{end}}</td>
        <td data-label="Needed by">{{if .NeededBy}}{{.NeededBy}}{{else}}—{{end}}</td>
        <td data-label="Status"><span class="pill approved">Approved</span></td>
        <td class="c" data-label=""><form method="post" action="/requests/{{.ID}}/record-payment"><input type="hidden" name="csrf" value="{{$.CSRF}}"><button class="btn small primary">Take for processing</button></form></td>
      </tr>
      {{end}}
      {{range .Linkable.Unavailable}}
      <tr>
        <td class="t-lead" data-label="Request"><a href="/requests/{{.ID}}">{{.Number}}</a> <span class="t-sub">{{.RequesterName}} · approved {{datep .ApprovedAt}}</span></td>
        <td data-label="Payee">{{.VendorPayee}}</td>
        <td data-label="Project / head">{{if eq .Treatment "recoverable"}}<span class="pill recoverable">Recoverable</span>{{else}}{{.Project}} / {{.Head}}{{end}}</td>
        <td class="num" data-label="Amount">{{if .ApprovedAmount}}{{money (deref .ApprovedAmount)}}{{else}}{{money .Amount}}{{end}}</td>
        <td data-label="Needed by">{{if .NeededBy}}{{.NeededBy}}{{else}}—{{end}}</td>
        <td data-label="Status">
          {{if .OnHold}}<span class="pill hold">On hold</span>
          {{else if eq .Status "partial_review"}}<span class="pill partial">Partial — manager review</span>
          {{else if eq .Status "completed_partial"}}<span class="pill completed-partial">Completed — partial accepted</span>
          {{else if eq .Status "completed"}}<span class="pill completed">Completed</span>
          {{else if and .ProcessingBy (eq (deref .ProcessingBy) $.User.ID)}}<span class="pill processing">Reserved by you · {{reservedLabel .ProcessingAt}}</span>
          {{else}}<span class="pill processing">Reserved by {{.ProcessingByName}}</span>{{end}}
        </td>
        <td class="c" data-label="">
          {{if .OnHold}}<a class="btn small" href="/requests/{{.ID}}">Read reply</a>
          {{else if eq .Status "partial_review"}}<a class="btn small outline" href="/requests/{{.ID}}/partial-review">View</a>
          {{else if and .ProcessingBy (eq (deref .ProcessingBy) $.User.ID)}}<a class="btn small" href="/payments/new?request={{.ID}}">Resume</a>
          {{else if and .ProcessingBy ($.Perms.Can "reservation" "reassign")}}<a class="btn small outline" href="/requests/{{.ID}}/reservation">Reassign</a>
          {{else}}<a class="btn small outline" href="/requests/{{.ID}}">View</a>{{end}}
        </td>
      </tr>
      {{end}}
      {{if and (not .Linkable.Available) (not .Linkable.Unavailable)}}<tr><td colspan="7" class="empty">Nothing in this tab right now.</td></tr>{{end}}
    </tbody>
  </table>
</div>
{{template "bottom" .}}
{{end}}
```

> `deref` is the Phase 2 helper for `*int64`. The "Reserved by you · 14:02 / · 26 h" split follows the mockup: within the day the clock time is more useful, past it the elapsed time is; both come from `ProcessingAt`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run 'TestAccountsQueue' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): accounts payment queue with metrics, tabs and card table"
```

---

### Task 13: Request picker — `.combo` + `.combo-list` with taken rows

**New task.** Rebuilds the no-request branch of `GET /payments/new` against `mockups/screens/payment-request-picker.html`: the "Free payment entry has been removed" `.banner.info`, a `.combo` search field, a `.combo-list` of `.co` rows (payee, requester, project/head, invoice, needed-by, amount), `.co.is-taken` rows for requests reserved by someone else, and a "Recently paid by you" `t-cards` table. The list is delivered by htmx as the accountant types and re-rendered on plain form submit for the no-JS path.

**Files:**
- Modify: `internal/store/store.go`, `internal/store/models.go` (add `PaidRequestRow`, `RecentPaymentsByActor`)
- Modify: `internal/store/linking_test.go` (add test)
- Modify: `internal/app/app.go` (`paymentForm` picker branch; `paymentPickerOptions`; route)
- Modify: `internal/app/templates.go` (new `payment_pick_request`, `payment_pick_options`)
- Modify: `internal/app/app_integration_test.go` (add test)

**Interfaces:**
- Consumes: `LinkablePaymentRequests` (Task 10), `auth.Scope`.
- Produces: `type PaidRequestRow struct { PaymentID, RequestID int64; Number, Payee, PaidOn, Settlement, Status string; Amount int64 }`; `func (s *Store) RecentPaymentsByActor(ctx context.Context, actorID int64, limit int) ([]PaidRequestRow, error)`; route `GET /payments/new/options` (gated `payment:create`) returning the `.combo-list` fragment.

- [ ] **Step 1: Write the failing test**

```go
// store: internal/store/linking_test.go
func TestRecentPaymentsByActorReturnsLinkedRowsNewestFirst(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	pay := func(seq int, amount int64, on, settlement, reason string) {
		id := seedApprovedRequest(t, s, ctx, seq, req.ID, mgrID, headID, 500000, 500000)
		if err := s.ReserveRequest(ctx, acc, id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.RecordPaymentForRequest(ctx, acc, id, PaymentInput{HeadID: headID, PaidOn: on, Amount: amount, VendorPayee: "Nova Print Works"}, settlement, reason, nil); err != nil {
			t.Fatal(err)
		}
	}
	pay(1, 41300, "2026-07-24", "settled", "")
	pay(2, 118000, "2026-07-24", "settled", "")
	pay(3, 60000, "2026-07-23", "partial", "balance later")

	rows, err := s.RecentPaymentsByActor(ctx, acc.ID, 2)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("limit ignored: %d rows", len(rows))
	}
	if rows[0].Number == "" || rows[0].Payee != "Nova Print Works" || rows[0].Amount == 0 || rows[0].Status == "" {
		t.Fatalf("row is not renderable: %+v", rows[0])
	}
	// Someone else's payments never appear.
	other, err := s.CreateUser(ctx, "other@example.com", "Other", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := s.RecentPaymentsByActor(ctx, other, 5)
	if err != nil || len(mine) != 0 {
		t.Fatalf("another actor's rows leaked: %+v, err=%v", mine, err)
	}
}
```

```go
// app: internal/app/app_integration_test.go
func TestPaymentPickerRendersComboAndTakenRows(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Picker")
	open := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 10000000)
	taken := s.seedApprovedRequest(2, admin.ID, admin.ID, headID, 3350000)
	hash, _ := auth.HashPassword("DeepakPass1234")
	deepakID, err := s.st.CreateUser(s.ctx, "deepak@example.test", "Deepak Menon", hash, "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	deepak, _ := s.st.UserByID(s.ctx, deepakID)
	if err := s.st.ReserveRequest(s.ctx, deepak, taken); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/payments/new", nil, ""))
	for _, want := range []string{
		`class="banner info"`,
		"Free payment entry has been removed",
		`class="combo"`,
		`class="combo-input"`,
		`class="combo-list"`,
		`class="co"`,
		"PR-2026-000001",
		`class="co-amt"`,
		`class="co is-taken"`,
		"Reserved by Deepak Menon",
		"Recently paid by you",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("picker missing %q:\n%s", want, body)
		}
	}
	// A taken row must not be postable: no reservation form inside it.
	takenRow := body[strings.Index(body, `class="co is-taken"`):]
	if i := strings.Index(takenRow, "</a>"); i > 0 && strings.Contains(takenRow[:i], "/record-payment") {
		t.Fatalf("taken row offers a reservation action:\n%s", takenRow[:i])
	}
	// htmx fragment: the list only, no shell.
	frag := responseBody(t, s.request(http.MethodGet, "/payments/new/options?q=PR-2026-000001", nil, "", "HX-Request", "true"))
	if !strings.Contains(frag, `class="combo-list"`) {
		t.Fatalf("options fragment missing the list:\n%s", frag)
	}
	if strings.Contains(frag, "<aside") || strings.Contains(frag, "<!doctype") {
		t.Fatalf("options fragment rendered the whole shell:\n%s", frag)
	}
	if strings.Contains(frag, "PR-2026-000002") {
		t.Fatalf("options fragment ignored the search:\n%s", frag)
	}
	_ = open
}
```

> `s.request` gains an optional trailing `header, value …` variadic if it does not already accept one; that is a test-helper change only.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestRecentPaymentsByActor -v && go test ./internal/app/ -run TestPaymentPickerRendersComboAndTakenRows -v`
Expected: FAIL — `s.RecentPaymentsByActor undefined` (`[build failed]`); then the picker test fails on `class="combo"` because `/payments/new` still renders the legacy table picker, and `/payments/new/options` 404s.

- [ ] **Step 3: Write minimal implementation**

In `internal/store/models.go`:

```go
// PaidRequestRow is one line of "Recently paid by you": enough to render the row
// and link to the payment, without loading a Payment and a Request per line.
type PaidRequestRow struct {
	PaymentID  int64
	RequestID  int64
	Number     string
	Payee      string
	Amount     int64
	PaidOn     string
	Settlement string
	Status     string // the request's status: completed | completed_partial | partial_review
}
```

In `internal/store/store.go`:

```go
func (s *Store) RecentPaymentsByActor(ctx context.Context, actorID int64, limit int) ([]PaidRequestRow, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.QueryContext(ctx, `SELECT py.id, r.id, r.number, COALESCE(py.vendor_payee,''), py.amount, py.paid_on, COALESCE(py.settlement,''), r.status
		FROM payments py JOIN payment_requests r ON r.id=py.request_id
		WHERE py.entered_by=? AND py.request_id IS NOT NULL
		ORDER BY py.created_at DESC, py.id DESC LIMIT ?`, actorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PaidRequestRow
	for rows.Next() {
		var p PaidRequestRow
		if err := rows.Scan(&p.PaymentID, &p.RequestID, &p.Number, &p.Payee, &p.Amount, &p.PaidOn, &p.Settlement, &p.Status); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
```

In `internal/app/app.go`, register the fragment route and split `paymentForm`:

```go
	mux.Handle("GET /payments/new/options", a.auth.RequirePermission("payment", "create", http.HandlerFunc(a.paymentPickerOptions)))
```

```go
func (a *App) pickerData(r *http.Request) (PageData, error) {
	u := auth.CurrentUser(r)
	q := r.URL.Query().Get("q")
	set, err := a.st.LinkablePaymentRequests(r.Context(), store.LinkableOptions{
		Scope: a.auth.Scope(u, "request"), ViewerID: u.ID, Query: q, Limit: 20,
	})
	if err != nil {
		return PageData{}, err
	}
	recent, err := a.st.RecentPaymentsByActor(r.Context(), u.ID, 5)
	if err != nil {
		return PageData{}, err
	}
	return PageData{Title: "Record a payment", Linkable: set, RecentPaid: recent, Query: q}, nil
}

func (a *App) paymentPickerOptions(w http.ResponseWriter, r *http.Request) {
	data, err := a.pickerData(r)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "payment_pick_options", data)
}
```

and in `paymentForm`, replace the old no-request branch with:

```go
	if requestID == 0 {
		data, err := a.pickerData(r)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		a.render(w, r, "payment_pick_request", data)
		return
	}
```

In `internal/app/templates.go`, replace the legacy `payment_pick_request` with the two design-system templates:

```html
{{define "payment_pick_options"}}
<div class="combo-list" id="picker-list">
  {{range .Linkable.Available}}
  <form method="post" action="/requests/{{.ID}}/record-payment"><input type="hidden" name="csrf" value="{{$.CSRF}}">
    <button class="co" type="submit">
      <span class="co-main"><b>{{.Number}} · {{.VendorPayee}}</b>
        <small>{{.RequesterName}} · {{.Project}} / {{.Head}}{{if .NeededBy}} · needed {{.NeededBy}}{{end}}</small></span>
      <span class="co-amt">{{if .ApprovedAmount}}{{money (deref .ApprovedAmount)}}{{else}}{{money .Amount}}{{end}}</span>
    </button>
  </form>
  {{end}}
  {{range .Linkable.Unavailable}}
  <a class="co is-taken" href="/requests/{{.ID}}" aria-disabled="true">
    <span class="co-main"><b>{{.Number}} · {{.VendorPayee}}</b>
      <small>{{if .OnHold}}On hold — {{.HoldReason}}{{else}}Reserved by {{.ProcessingByName}} at {{hhmm .ProcessingAt}} — you cannot take this one{{end}}</small></span>
    <span class="co-amt">{{if .ApprovedAmount}}{{money (deref .ApprovedAmount)}}{{else}}{{money .Amount}}{{end}}</span>
  </a>
  {{end}}
  {{if and (not .Linkable.Available) (not .Linkable.Unavailable)}}<div class="co"><span class="co-main"><b>No approved requests match</b><small>Clear the search, or check the queue.</small></span></div>{{end}}
</div>
{{end}}

{{define "payment_pick_request"}}
{{template "top" .}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">Accounts · new payment</div>
    <h1>Which approved request is this for?</h1>
    <p class="sub">Every payment belongs to exactly one approved request.</p>
  </div>
</section>

<div class="banner info">
  <span class="b-ico">i</span>
  <div>
    <b>Free payment entry has been removed</b>
    <p>Payments recorded before this module remain in the ledger as history. New money out starts here.</p>
  </div>
</div>

<form class="field" method="get" action="/payments/new" style="margin-bottom:4px">
  <label for="picker">Search approved requests</label>
  <span class="combo">
    <input class="combo-input" id="picker" name="q" value="{{.Query}}" autocomplete="off"
           hx-get="/payments/new/options" hx-trigger="keyup changed delay:250ms, search"
           hx-target="#picker-list" hx-swap="outerHTML">
    <span class="combo-caret">▾</span>
  </span>
  <span class="hint">Search by request number, requester, payee, project, head or amount.</span>
</form>

{{template "payment_pick_options" .}}

<div class="section-head"><h2>Recently paid by you</h2></div>
<div class="table-wrap">
  <table class="t-cards">
    <thead><tr><th>Request</th><th>Payee</th><th class="num">Paid</th><th>Date</th><th>Status</th></tr></thead>
    <tbody>
      {{range .RecentPaid}}
      <tr>
        <td class="t-lead" data-label="Request"><a href="/payments/{{.PaymentID}}">{{.Number}}</a></td>
        <td data-label="Payee">{{.Payee}}</td>
        <td class="num" data-label="Paid">{{money .Amount}}</td>
        <td data-label="Date">{{.PaidOn}}</td>
        <td data-label="Status">{{if eq .Status "partial_review"}}<span class="pill partial">Partial — manager review</span>{{else if eq .Status "completed_partial"}}<span class="pill completed-partial">Completed — partial accepted</span>{{else}}<span class="pill completed">Completed</span>{{end}}</td>
      </tr>
      {{else}}<tr><td colspan="5" class="empty">You have not recorded a payment yet.</td></tr>{{end}}
    </tbody>
  </table>
</div>
{{template "bottom" .}}
{{end}}
```

> Three deliberate points. The takeable `.co` is a **submit button inside a POST form**, not a link, because selecting one reserves the request — a GET must never mutate. The taken `.co.is-taken` links to the read-only request instead, which is the only thing the loser may do. The search input degrades to a plain GET on `/payments/new` when htmx is absent, re-rendering the same list from the same template.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestRecentPaymentsByActor -v && go test ./internal/app/ -run TestPaymentPickerRendersComboAndTakenRows -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/store/models.go internal/store/store.go internal/store/linking_test.go internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): request picker as a combobox with taken rows and recent payments"
```

---

### Task 14: Payment entry screen — reserve bar, approved card, money field, live difference

**New task.** Rebuilds the with-request branch of `GET /payments/new` against `mockups/screens/payment-entry.html`: the `.reserve-bar` ("Reserved by you · since 14:02 · nobody else can process this request" with a Release action), the "What was approved" `.card` + `.dl`, a read-only approved-amount field carrying the G13 rule in its `.hint`, the `.money-field` with `.in-words`, the live difference `.banner` that switches `good`/`warn`/`bad`, the `.uploader`, and an `.action-bar` whose primary button opens the settlement step. Nothing on this screen writes.

**Files:**
- Modify: `internal/app/app.go` (`paymentForm` request branch)
- Modify: `internal/app/templates.go` (rewrite `payment_form`)
- Modify: `web/static/fervid-app.js` (bind the difference banner to the Phase 0 money field)
- Modify: `internal/app/app_integration_test.go` (add test; retarget `TestPaymentErrorRetainsInputAndUsesHumanModes`)

**Interfaces:**
- Consumes: `Request` (Phase 2), `ListHeads`, Phase 0 `.money-field` behaviour.
- Produces: the `payment_form` screen; no new routes.

- [ ] **Step 1: Write the failing test**

```go
func TestPaymentEntryScreenShowsReservationApprovalAndRule(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Entry")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 10000000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/payments/new?request=%d", reqID), nil, ""))
	for _, want := range []string{
		`class="reserve-bar"`,
		"Reserved by you",
		`class="rb-actions"`,
		"What was approved",
		`class="dl"`,
		"A payment can never exceed this",
		`class="field span-6 money-field"`,
		`class="in-words"`,
		`id="diff-banner"`,
		`class="uploader"`,
		`class="action-bar"`,
		"Nothing is saved until you confirm",
		`name="request_id" value="`,
		"1,00,000.00",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("payment entry missing %q:\n%s", want, body)
		}
	}
	// A <details> is not an acceptable settlement control (design system rule).
	if strings.Contains(body, "<details") {
		t.Fatalf("payment entry still uses a <details> disclosure:\n%s", body)
	}
	// The form uploads once, on the final POST.
	if !strings.Contains(body, `enctype="multipart/form-data"`) || !strings.Contains(body, `action="/payments"`) {
		t.Fatalf("payment form is not the single multipart POST to /payments:\n%s", body)
	}
}

func TestPaymentEntryRefusesSomeoneElsesReservation(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("NotMine")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	hash, _ := auth.HashPassword("DeepakPass1234")
	deepakID, err := s.st.CreateUser(s.ctx, "deepak@example.test", "Deepak Menon", hash, "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	deepak, _ := s.st.UserByID(s.ctx, deepakID)
	if err := s.st.ReserveRequest(s.ctx, deepak, reqID); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.request(http.MethodGet, fmt.Sprintf("/payments/new?request=%d", reqID), nil, "")
	requireStatus(t, resp, http.StatusConflict)
	body := responseBody(t, resp)
	if !strings.Contains(body, "Deepak Menon took this request") || !strings.Contains(body, `class="a-list"`) {
		t.Fatalf("opening someone else's reservation must land on the conflict screen (G15):\n%s", body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run 'TestPaymentEntryScreen|TestPaymentEntryRefusesSomeoneElsesReservation' -v`
Expected: FAIL — the legacy `payment_form` has no `.reserve-bar`, no approved card and no difference banner; the second test gets a plain 409 error page instead of the conflict screen.

- [ ] **Step 3: Write minimal implementation**

In `internal/app/app.go`, the request branch of `paymentForm` routes a foreign reservation to the shared conflict screen instead of `respondError`:

```go
	req, err := a.st.Request(r.Context(), requestID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if req.Status != "processing" || req.ProcessingBy == nil || *req.ProcessingBy != u.ID {
		a.reservationConflict(w, r, req, nil)
		return
	}
	heads, err := a.st.ListHeads(r.Context(), true)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	amount := req.Amount
	if req.ApprovedAmount != nil {
		amount = *req.ApprovedAmount
	}
	headID := int64(0)
	if req.HeadID != nil {
		headID = *req.HeadID
	}
	a.render(w, r, "payment_form", PageData{
		Title:          "Record payment",
		Heads:          heads,
		SelectedHeadID: headID,
		Request2:       req,
		ReserveMine:    true,
		Payment:        store.Payment{HeadID: headID, PaidOn: time.Now().Format("2006-01-02"), Amount: amount, VendorPayee: req.VendorPayee},
	})
```

In `internal/app/templates.go`, rewrite `payment_form` (the linked screen; the free-standing branch is gone with the legacy path):

```html
{{define "payment_form"}}
{{template "top" .}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">Accounts · payment entry</div>
    <h1>Record the payment</h1>
    <p class="sub">{{.Request2.Number}} · {{.Request2.VendorPayee}} · one request, one payment</p>
  </div>
</section>

<div class="reserve-bar">
  <span class="rb-dot"></span>
  <b>Reserved by you</b>
  <span class="rb-meta">since {{hhmm .Request2.ProcessingAt}} · nobody else can process this request</span>
  <span class="rb-actions"><a class="btn small outline" href="/requests/{{.Request2.ID}}/reservation">Release</a></span>
</div>

<div class="card" style="margin-bottom:14px">
  <div class="card-head"><h2>What was approved</h2><a class="small" href="/requests/{{.Request2.ID}}">Open the request →</a></div>
  <dl class="dl">
    <div><dt>Approved amount</dt><dd class="big">{{if .Request2.ApprovedAmount}}{{money (deref .Request2.ApprovedAmount)}}{{else}}{{money .Request2.Amount}}{{end}}</dd></div>
    <div><dt>Approved by</dt><dd>{{.Request2.ApprovedByName}}{{if .Request2.ApprovedAt}} · {{datep .Request2.ApprovedAt}}{{end}}</dd></div>
    <div><dt>Payee</dt><dd>{{.Request2.VendorPayee}}</dd></div>
    <div><dt>Charge to</dt><dd>{{.Request2.Project}} / {{.Request2.Head}}</dd></div>
    <div style="grid-column:1/-1"><dt>Purpose</dt><dd>{{.Request2.Purpose}}</dd></div>
  </dl>
</div>

<form method="post" action="/payments" enctype="multipart/form-data">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <input type="hidden" name="request_id" value="{{.Request2.ID}}">
  <input type="hidden" name="head_id" value="{{.SelectedHeadID}}">
  {{if .Error}}<div class="banner bad"><span class="b-ico">✕</span><div><b>{{.Error}}</b></div></div>{{end}}
  <fieldset>
    <legend>Payment</legend>
    <div class="form-grid">
      <div class="field span-6">
        <label for="approved">Approved amount</label>
        <input id="approved" value="{{if .Request2.ApprovedAmount}}{{money (deref .Request2.ApprovedAmount)}}{{else}}{{money .Request2.Amount}}{{end}}" readonly
               data-approved="{{if .Request2.ApprovedAmount}}{{deref .Request2.ApprovedAmount}}{{else}}{{.Request2.Amount}}{{end}}">
        <span class="hint">A payment can never exceed this. Overpayment means cancelling and raising a new request.</span>
      </div>
      <div class="field span-6 money-field">
        <label for="amount">Amount actually paid <span class="req">*</span></label>
        <span class="money-wrap"><span class="cur">₹</span><input id="amount" name="amount" inputmode="decimal" value="{{if .PaymentAmount}}{{.PaymentAmount}}{{else}}{{money .Payment.Amount}}{{end}}" required></span>
        <span class="in-words"></span>
      </div>
      <div class="field span-12">
        <div class="banner good" id="diff-banner" style="margin:0">
          <span class="b-ico">✓</span>
          <div><b id="diff-text">Matches the approved amount exactly</b>
            <p>You will be asked whether this settles the obligation in full or leaves a balance due.</p></div>
        </div>
      </div>
      <div class="field span-4 m-half"><label for="paid_on">Paid on <span class="req">*</span></label><input id="paid_on" name="paid_on" type="date" value="{{.Payment.PaidOn}}" required></div>
      <div class="field span-4 m-half"><label for="payment_mode">Payment mode <span class="req">*</span></label>
        <select id="payment_mode" name="payment_mode" required>
          <option value="">Choose…</option>
          {{range paymentModes}}<option value="{{.}}" {{select . $.Payment.PaymentMode}}>{{.}}</option>{{end}}
        </select>
      </div>
      <div class="field span-4"><label for="reference_no">Transaction / UTR reference <span class="req">*</span></label><input id="reference_no" name="reference_no" class="num" value="{{.Payment.ReferenceNo}}" required></div>
      <input type="hidden" name="vendor_payee" value="{{.Request2.VendorPayee}}">
    </div>
  </fieldset>

  <fieldset>
    <legend>Proof and notes</legend>
    <div class="form-grid">
      <div class="field span-12">
        <span class="flabel">Payment advice or proof</span>
        <label class="uploader"><div class="up-ico">⇪</div><b>Add the bank advice</b><small>PDF, JPG or PNG up to 10 MB</small><input type="file" name="attachment" accept=".pdf,.jpg,.jpeg,.png" hidden></label>
      </div>
      <div class="field span-12">
        <label for="remarks">Processing note <span class="opt">optional</span></label>
        <textarea id="remarks" name="remarks">{{.Payment.Remarks}}</textarea>
        <span class="hint">This system does no tax arithmetic. Record what you did so the trail explains the difference.</span>
      </div>
    </div>
  </fieldset>

  <div id="settle-mount"></div>

  <div class="action-bar">
    <span class="ab-note d-only">Nothing is saved until you confirm on the next step.</span>
    <span class="row-end"></span>
    <a class="btn outline" href="/requests/{{.Request2.ID}}/reservation">Cancel and release</a>
    <button class="btn primary" type="submit"
            formaction="/requests/{{.Request2.ID}}/settlement-preview" formmethod="post" formenctype="application/x-www-form-urlencoded"
            hx-post="/requests/{{.Request2.ID}}/settlement-preview"
            hx-include="#amount, #paid_on, #payment_mode, #reference_no, #remarks, [name=csrf]"
            hx-target="#settle-mount" hx-swap="innerHTML">Payment settled →</button>
  </div>
</form>
{{template "bottom" .}}
{{end}}
```

> `formenctype="application/x-www-form-urlencoded"` on the preview button is what keeps D8 honest without JavaScript: the preview never receives the file bytes, so it cannot stage them. `hx-include` does the same for the htmx path by naming the text fields explicitly. Either way the file is transmitted exactly once, by the final multipart POST to `/payments`. `paymentModes` is the existing mode list helper; keep whatever the current form uses.

In `web/static/fervid-app.js`, extend the Phase 0 money-field behaviour so the difference banner tracks the input (display only — G13 is enforced in the store, and the banner is not validation):

```js
// Live difference against the approved ceiling. Mirrors the store's G13 rule so
// the accountant sees the refusal before submitting, never instead of it.
document.querySelectorAll("[data-approved]").forEach(function (approvedEl) {
  var approved = parseInt(approvedEl.dataset.approved, 10) || 0;
  var paid = document.getElementById("amount");
  var banner = document.getElementById("diff-banner");
  var text = document.getElementById("diff-text");
  if (!paid || !banner || !text) return;
  function sync() {
    var v = Math.round((parseFloat(String(paid.value).replace(/[^\d.]/g, "")) || 0) * 100);
    var d = approved - v;
    banner.className = "banner " + (d === 0 ? "good" : d > 0 ? "warn" : "bad");
    text.textContent = d === 0 ? "Matches the approved amount exactly"
      : d > 0 ? fervidMoney(d) + " less than approved"
      : fervidMoney(-d) + " more than approved — not allowed";
  }
  paid.addEventListener("input", sync);
  paid.addEventListener("blur", sync);
  sync();
});
```

> `fervidMoney` is the Phase 0 grouping helper the money field already uses; reuse it rather than adding a second formatter.

**Existing-test note:** `TestPaymentErrorRetainsInputAndUsesHumanModes` and the e2e `createPayment` fixture drive the retired free-standing path. Retarget the Go test to reserve a seeded request first and post with `request_id` + `settlement=settled` + a malformed `amount`, asserting the re-rendered `Record payment` screen retains the input and shows the `invalid amount` error. The retention behaviour is unchanged; only the linkage is added. Do not delete the test.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run 'TestPaymentEntryScreen|TestPaymentEntryRefusesSomeoneElsesReservation|TestReservationEntryPointAndPrefill|TestPaymentErrorRetainsInputAndUsesHumanModes' -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go web/static/fervid-app.js internal/app/app_integration_test.go
git commit -m "feat(app): payment entry screen with reserve bar, approved card and live difference"
```

---

### Task 15: Settlement preview — a pure endpoint that persists nothing (**D8**, **G16**)

**New task (D8).** Today one click posts and writes. The approved flow has a confirmation step in between, and the whole point of `payment-settlement-confirm.html` is that the payment does not exist while it is on screen (`.pill.neutral.no-dot` reads "Not saved yet"). So `POST /requests/{id}/settlement-preview` is **pure**: no `BeginTx`, no attachment staging, no writes of any kind. It re-checks that the caller still holds the reservation, computes approved / paid / difference, and renders the `.overlay > .sheet`.

The sheet contains: a `.compare` block with `.cmp-row` for approved and paid plus a third row that is `.diff` when paid < approved and `.match` when equal; the `.choice` radio pair carrying `.outcome.good` "Completed" and `.outcome.warn` "Manager review"; the `data-when="settlement:partial"` reason textarea; and the immutability `.banner.info`. It is delivered by htmx into `#settle-mount` inside the live form, so the file input is uploaded exactly once by the final POST. The same sheet template renders inside a full page for the no-JS path. Errors from `POST /payments` re-render the sheet, never a full error page.

**Files:**
- Modify: `internal/app/app.go` (`settlementPreview`, `settlementError`, `SettlementPreview` type; route)
- Modify: `internal/app/templates.go` (new `settlement_sheet`, `settlement_confirm`)
- Modify: `internal/app/app_integration_test.go` (add tests)

**Interfaces:**
- Consumes: `Request` (Phase 2), `paymentInput`, `Perms`, `renderStatus` (shell skipped on `HX-Request`, Phase 0 Task 16).
- Produces: route `POST /requests/{id}/settlement-preview` (gated `payment:settle`, wrapped in `withCSRF`); `type SettlementPreview struct { Approved, Paid, Difference int64; Match bool; Settlement, PartialReason string; Fields map[string]string }`.

- [ ] **Step 1: Write the failing test**

```go
func TestSettlementPreviewWritesNothingAndRendersTheSheet(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Preview")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 10000000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	form := url.Values{"amount": {"98000.00"}, "paid_on": {"2026-07-25"}, "payment_mode": {"Bank transfer"}, "reference_no": {"N221260725004417"}}
	resp := s.postForm(fmt.Sprintf("/requests/%d/settlement-preview", reqID), form)
	requireStatus(t, resp, http.StatusOK)
	body := responseBody(t, resp)
	for _, want := range []string{
		`class="overlay"`, `class="sheet"`, `class="sh-head"`, `class="sh-body`, `class="sh-foot"`,
		`class="compare"`, `class="cmp-row"`, `class="cmp-row diff"`,
		"1,00,000.00", "98,000.00", "2,000.00",
		`class="choice"`,
		`class="outcome good"`, "Completed",
		`class="outcome warn"`, "Manager review",
		`data-when="settlement:partial"`,
		`class="banner info"`, "cannot be edited or cancelled",
		`value="settled"`, `value="partial"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("settlement sheet missing %q:\n%s", want, body)
		}
	}
	// D8/G16: nothing was persisted, and the request is still merely reserved.
	var payments int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&payments); err != nil {
		t.Fatal(err)
	}
	if payments != 0 {
		t.Fatalf("the preview wrote %d payment rows (D8 broken)", payments)
	}
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status after preview = %q, want processing", got)
	}
	var atts int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM attachments`).Scan(&atts); err != nil {
		t.Fatal(err)
	}
	if atts != 0 {
		t.Fatalf("the preview staged %d attachments (D8 broken)", atts)
	}
}

func TestSettlementPreviewMatchesRowAndNoJSFullPage(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("PreviewExact")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"amount": {"5000.00"}, "paid_on": {"2026-07-25"}, "payment_mode": {"Bank transfer"}, "reference_no": {"N1"}}

	// No-JS: a whole page, shell and all, wrapping the same sheet.
	page := responseBody(t, s.postForm(fmt.Sprintf("/requests/%d/settlement-preview", reqID), form))
	if !strings.Contains(page, `class="cmp-row match"`) || strings.Contains(page, `class="cmp-row diff"`) {
		t.Fatalf("equal amounts must render .match, not .diff:\n%s", page)
	}
	if !strings.Contains(page, "<aside") {
		t.Fatalf("the no-JS path must render the full page:\n%s", page)
	}
	// The no-JS confirmation carries every field forward so the confirm posts once.
	for _, want := range []string{`name="amount"`, `name="paid_on"`, `name="payment_mode"`, `name="reference_no"`, `name="request_id"`, `enctype="multipart/form-data"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("no-JS confirmation missing %q:\n%s", want, page)
		}
	}

	// htmx: the fragment only.
	frag := responseBody(t, s.postFormWithHeader(fmt.Sprintf("/requests/%d/settlement-preview", reqID), form, "HX-Request", "true"))
	if !strings.Contains(frag, `class="overlay"`) {
		t.Fatalf("htmx fragment missing the sheet:\n%s", frag)
	}
	if strings.Contains(frag, "<aside") || strings.Contains(frag, "<!doctype") {
		t.Fatalf("htmx fragment rendered the shell:\n%s", frag)
	}
}

func TestSettlementPreviewRefusesWhenTheReservationIsGone(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("PreviewLost")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	// An authorised colleague takes it away mid-flow.
	if err := s.st.ReleaseRequest(s.ctx, admin, reqID, "needed elsewhere", true, true); err != nil {
		t.Fatal(err)
	}
	resp := s.postForm(fmt.Sprintf("/requests/%d/settlement-preview", reqID), url.Values{"amount": {"5000.00"}, "paid_on": {"2026-07-25"}})
	requireStatus(t, resp, http.StatusConflict)
	if body := responseBody(t, resp); !strings.Contains(body, `class="a-list"`) {
		t.Fatalf("a lost reservation must land on the conflict screen:\n%s", body)
	}
}
```

> `postFormWithHeader` is a one-line test helper over the existing `postForm`; add it beside the others.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run 'TestSettlementPreview' -v`
Expected: FAIL — `POST /requests/{id}/settlement-preview` is unregistered, so all three get 404 and fail on `class="overlay"`.

- [ ] **Step 3: Write minimal implementation**

In `internal/app/app.go`:

```go
	mux.Handle("POST /requests/{id}/settlement-preview", a.auth.RequirePermission("payment", "settle", http.HandlerFunc(a.withCSRF(a.settlementPreview))))
```

```go
// SettlementPreview is a view model only. It is computed, rendered and thrown
// away; nothing here is persisted (D8).
type SettlementPreview struct {
	Approved      int64
	Paid          int64
	Difference    int64 // approved - paid; > 0 means under-paid
	Match         bool
	Settlement    string
	PartialReason string
	Fields        map[string]string // every entry-form field, carried to the confirm POST
}

// settlementPreview is pure: no BeginTx, no attachment staging, no writes. It
// re-checks the reservation, computes the comparison and renders the sheet. The
// only thing that writes is POST /payments.
func (a *App) settlementPreview(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	id := pathID(r)
	req, err := a.st.Request(r.Context(), id)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if req.Status != "processing" || req.ProcessingBy == nil || *req.ProcessingBy != u.ID {
		a.reservationConflict(w, r, req, nil)
		return
	}
	paid, perr := money.ParsePaise(r.FormValue("amount"))
	if perr != nil {
		a.settlementError(w, r, id, store.PaymentInput{}, r.FormValue("amount"), "", "", fmt.Errorf("%w: enter a valid amount", store.ErrValidation))
		return
	}
	approved := req.Amount
	if req.ApprovedAmount != nil {
		approved = *req.ApprovedAmount
	}
	a.renderSettlement(w, r, http.StatusOK, req, SettlementPreview{
		Approved:   approved,
		Paid:       paid,
		Difference: approved - paid,
		Match:      approved == paid,
		Settlement: "settled",
		Fields:     settlementFields(r),
	}, "")
}

// settlementFields snapshots the entry form so the confirmation can post every
// value in one request. The file input is deliberately absent — it lives in the
// live form on the htmx path, and is re-offered on the no-JS confirmation.
func settlementFields(r *http.Request) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"amount", "paid_on", "payment_mode", "reference_no", "invoice_no", "remarks", "vendor_payee", "head_id"} {
		if v := r.FormValue(k); v != "" {
			out[k] = v
		}
	}
	return out
}

func (a *App) renderSettlement(w http.ResponseWriter, r *http.Request, status int, req store.Request, p SettlementPreview, errMsg string) {
	name := "settlement_confirm"
	if r.Header.Get("HX-Request") == "true" {
		name = "settlement_sheet"
	}
	a.renderStatus(w, r, status, name, PageData{Title: "Confirm the payment", Request2: req, Settlement: p, Error: errMsg})
}

// settlementError re-renders the sheet with the message in place, so a rejected
// settlement never throws the accountant onto an error page and never loses the
// figures they typed. Replaces the Task 11 stub.
func (a *App) settlementError(w http.ResponseWriter, r *http.Request, requestID int64, in store.PaymentInput, rawAmount, settlement, partialReason string, cause error) {
	status := storeErrorStatus(cause)
	if status >= http.StatusInternalServerError {
		a.respondStoreError(w, r, cause)
		return
	}
	req, rerr := a.st.Request(r.Context(), requestID)
	if rerr != nil {
		a.respondStoreError(w, r, rerr)
		return
	}
	approved := req.Amount
	if req.ApprovedAmount != nil {
		approved = *req.ApprovedAmount
	}
	paid, _ := money.ParsePaise(rawAmount)
	a.renderSettlement(w, r, status, req, SettlementPreview{
		Approved: approved, Paid: paid, Difference: approved - paid, Match: approved == paid,
		Settlement: settlement, PartialReason: partialReason, Fields: settlementFields(r),
	}, friendly(cause))
}
```

In `internal/app/templates.go`, add the sheet and its full-page wrapper — one markup source, two deliveries:

```html
{{define "settlement_sheet"}}
<div class="overlay" id="settle-sheet">
  <div class="sheet">
    <div class="sh-head">
      <div><h2>Confirm the payment</h2><p class="sh-sub">{{.Request2.Number}} · {{.Request2.VendorPayee}}</p></div>
      <a class="sh-close" href="/payments/new?request={{.Request2.ID}}" aria-label="Close">✕</a>
    </div>
    <div class="sh-body stack-12">
      {{if .Error}}<div class="banner bad" style="margin:0"><span class="b-ico">✕</span><div><b>{{.Error}}</b><p>Nothing has been saved. Correct it and confirm again.</p></div></div>{{end}}
      <div class="compare">
        <div class="cmp-row"><span class="l">Approved</span><span class="v">{{money .Settlement.Approved}}</span></div>
        <div class="cmp-row"><span class="l">Actually paid</span><span class="v">{{money .Settlement.Paid}}</span></div>
        {{if .Settlement.Match}}
        <div class="cmp-row match"><span class="l">Difference</span><span class="v">{{money 0}}</span></div>
        {{else}}
        <div class="cmp-row diff"><span class="l">Difference</span><span class="v">{{money .Settlement.Difference}} lower ⚠</span></div>
        {{end}}
      </div>

      <div>
        <span class="flabel" style="margin-bottom:6px">{{if .Settlement.Match}}This matches the approved amount. Confirm to close the request.{{else}}You paid less than was approved. Which is it?{{end}}</span>
        <div class="choice">
          <label>
            <input type="radio" name="settlement" value="settled" {{if ne .Settlement.Settlement "partial"}}checked{{end}}>
            <span><b>Fully settled</b><small>The obligation is discharged. Deductions such as TDS or retention were handled outside this system.</small></span>
            <span class="outcome good">Completed</span>
          </label>
          <label>
            <input type="radio" name="settlement" value="partial" {{if eq .Settlement.Settlement "partial"}}checked{{end}}>
            <span><b>Partial payment</b><small>A balance is genuinely still owed to the payee.</small></span>
            <span class="outcome warn">Manager review</span>
          </label>
        </div>
      </div>

      <div class="field" data-when="settlement:partial" {{if ne .Settlement.Settlement "partial"}}hidden{{end}}>
        <label for="partial_reason">Why only part was paid <span class="req">*</span></label>
        <textarea id="partial_reason" name="partial_reason" placeholder="{{.Request2.ManagerName}} reads this when deciding whether to close it.">{{.Settlement.PartialReason}}</textarea>
      </div>

      <div class="banner info" style="margin:0">
        <span class="b-ico">i</span>
        <div>
          <b>Confirming saves the payment</b>
          <p>It cannot be edited or cancelled afterwards. The request accepts no further payment — any balance needs a fresh request.</p>
        </div>
      </div>
    </div>
    <div class="sh-foot">
      <a class="btn outline" href="/payments/new?request={{.Request2.ID}}">Go back</a>
      <span class="row-end"></span>
      <button class="btn primary" type="submit" formaction="/payments" formmethod="post">Confirm and save payment</button>
    </div>
  </div>
</div>
{{end}}

{{define "settlement_confirm"}}
{{template "top" .}}
<div class="reserve-bar">
  <span class="rb-dot"></span>
  <b>Reserved by you</b>
  <span class="rb-meta">since {{hhmm .Request2.ProcessingAt}}</span>
</div>

<div class="card">
  <div class="card-head"><h2>Payment about to be saved</h2><span class="pill neutral no-dot">Not saved yet</span></div>
  <dl class="dl">
    <div><dt>Paid on</dt><dd>{{index .Settlement.Fields "paid_on"}}</dd></div>
    <div><dt>Mode</dt><dd>{{index .Settlement.Fields "payment_mode"}}</dd></div>
    <div><dt>Reference</dt><dd class="num">{{index .Settlement.Fields "reference_no"}}</dd></div>
    <div style="grid-column:1/-1"><dt>Processing note</dt><dd>{{index .Settlement.Fields "remarks"}}</dd></div>
  </dl>
</div>

<form method="post" action="/payments" enctype="multipart/form-data">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <input type="hidden" name="request_id" value="{{.Request2.ID}}">
  {{range $k, $v := .Settlement.Fields}}<input type="hidden" name="{{$k}}" value="{{$v}}">{{end}}
  <div class="field">
    <span class="flabel">Payment advice or proof <span class="opt">optional</span></span>
    <label class="uploader"><div class="up-ico">⇪</div><b>Attach the bank advice</b><small>Attach it here — it is uploaded once, when you confirm.</small><input type="file" name="attachment" accept=".pdf,.jpg,.jpeg,.png" hidden></label>
  </div>
  {{template "settlement_sheet" .}}
</form>
{{template "bottom" .}}
{{end}}
```

Finally, replace the Task 11 stub: `paymentCreate`'s error branch now calls `a.settlementError(...)` as written there.

> The `.uploader` appears on the no-JS confirmation rather than the entry page because the preview button posts URL-encoded and therefore cannot carry the file. On the htmx path the sheet is injected into the live entry form, whose own file input posts with the confirm — one upload, one POST, either way.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run 'TestSettlementPreview' -v`
Expected: PASS (3 tests). Then `grep -n 'func (a \*App) settlementPreview' -A 40 internal/app/app.go | grep -c 'BeginTx\|stageUploadedAttachment'` — Expected: `0`, the mechanical proof that the preview persists nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): pure settlement preview endpoint rendering the confirmation sheet"
```

---

### Task 16: Settlement submit, payment detail screen and the full trail (S10, S11, S12, S13, Q4)

**Amended (was Task 11).** The settlement POST now lands on the payment, not back on the request, because `payment-detail.html` is the approved destination: a `.banner.good` confirming the save and the notifications, a `.req-head` reading "PAY-… · from PR-…" with `.pill.completed`, a `.compare` block whose third row is `.match` "Difference · confirmed settled by Accounts", a read-only `.card` + `.dl`, the `.file-row` proof list, and an `ol.thread` carrying the whole trail from submission to settlement. The request-detail outcome block stays, retargeted to `.compare` and linking to the payment.

**Files:**
- Modify: `internal/app/app.go` (`paymentDetail` loads the request, the approved amount and the merged trail; `requestDetail` loads `PaymentForRequest`)
- Modify: `internal/app/templates.go` (rewrite `payment_detail`; outcome block on `request_detail`)
- Modify: `internal/app/app_integration_test.go` (add tests)

**Interfaces:**
- Consumes: `RecordPaymentForRequest`, `PaymentForRequest`, `Request`, `AuditFor` (the Phase 2 audit listing used by `request_detail`).
- Produces: the `payment_detail` screen; the request-detail outcome section.

- [ ] **Step 1: Write the failing test**

```go
func TestSettlementFlowCompletesAndShowsPaymentDetail(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Settle")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 10000000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	// Settle for less than approved (S10).
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-25"}, "amount": {"98000.00"}, "vendor_payee": {"Acme Landlord"}, "settlement": {"settled"}, "reference_no": {"N221260725004417"}}
	resp := s.postForm("/payments", form)
	requireStatus(t, resp, http.StatusSeeOther)
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/payments/") {
		t.Fatalf("settlement redirect = %q, want the payment detail screen", loc)
	}
	_ = responseBody(t, resp)
	if got := requestStatusApp(t, s, reqID); got != "completed" {
		t.Fatalf("status = %q, want completed", got)
	}

	body := responseBody(t, s.request(http.MethodGet, loc, nil, ""))
	for _, want := range []string{
		`class="banner good"`, "The request is completed",
		`class="req-head"`, "from PR-2026-000001",
		`class="pill completed"`, `class="waiting done"`,
		`class="compare"`, `class="cmp-row match"`, "confirmed settled by Accounts",
		"1,00,000.00", "98,000.00", "2,000.00",
		`class="pill neutral no-dot"`, "Read-only",
		`<ol class="thread">`, `class="tl-dot`, "reserved it for processing", "recorded a payment",
		"Full trail, request to payment",
		`class="action-bar"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("payment detail missing %q:\n%s", want, body)
		}
	}
	// S12: no edit or void control on a linked payment, and the route refuses too.
	if strings.Contains(body, "/edit") || strings.Contains(body, "/void") {
		t.Fatalf("linked payment offers a mutation control:\n%s", body)
	}
	var payID int64
	if err := s.st.DB().QueryRow(`SELECT id FROM payments WHERE request_id=?`, reqID).Scan(&payID); err != nil {
		t.Fatal(err)
	}
	if resp := s.postForm(fmt.Sprintf("/payments/%d/void", payID), url.Values{"reason": {"nope"}}); resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("linked payment void accepted: %d", resp.StatusCode)
	}

	// Q4: the request page shows the outcome and links to the payment.
	reqBody := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""))
	if !strings.Contains(reqBody, `class="compare"`) || !strings.Contains(reqBody, "98,000.00") || !strings.Contains(reqBody, loc) {
		t.Fatalf("request outcome missing the comparison or the payment link:\n%s", reqBody)
	}
}

func TestDoubleConfirmLandsOnTheExistingPayment(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Double")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-25"}, "amount": {"5000.00"}, "vendor_payee": {"Acme"}, "settlement": {"settled"}, "reference_no": {"N1"}}
	first := s.postForm("/payments", form)
	requireStatus(t, first, http.StatusSeeOther)
	_ = responseBody(t, first)
	// The accountant taps Confirm twice. The second must not look like a failure.
	second := s.postForm("/payments", form)
	requireStatus(t, second, http.StatusSeeOther)
	if second.Header.Get("Location") != first.Header.Get("Location") {
		t.Fatalf("double confirm went to %q, want the existing payment %q", second.Header.Get("Location"), first.Header.Get("Location"))
	}
	_ = responseBody(t, second)
	var n int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM payments WHERE request_id=?`, reqID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("double confirm created %d payments (S9 broken)", n)
	}
}

func TestPartialSettlementRoutesToReview(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Partial")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-06-15"}, "amount": {"3000.00"}, "vendor_payee": {"Acme Landlord"}, "settlement": {"partial"}, "partial_reason": {"balance later"}, "reference_no": {"N2"}}
	requireStatus(t, s.postForm("/payments", form), http.StatusSeeOther)
	if got := requestStatusApp(t, s, reqID); got != "partial_review" {
		t.Fatalf("status = %q, want partial_review", got)
	}
	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""))
	if !strings.Contains(body, "balance later") || !strings.Contains(body, `class="pill partial"`) {
		t.Fatalf("partial outcome missing from the request:\n%s", body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run 'TestSettlementFlowCompletesAndShowsPaymentDetail|TestDoubleConfirmLandsOnTheExistingPayment|TestPartialSettlementRoutesToReview' -v`
Expected: FAIL — `payment_detail` is the legacy template with no `.compare`, no `.thread` and no linkage line; the request detail has no outcome block.

- [ ] **Step 3: Write minimal implementation**

In `internal/app/app.go`, extend `paymentDetail` to load the originating request and the merged trail when the payment is linked:

```go
	if pay.RequestID != nil {
		req, rerr := a.st.Request(r.Context(), *pay.RequestID)
		if rerr != nil {
			a.respondStoreError(w, r, rerr)
			return
		}
		data.Request2 = req
		// The trail the mockup shows spans both entities: the request from
		// submission to approval, then the payment from reservation to settlement.
		trail, terr := a.st.AuditFor(r.Context(), map[string][]int64{
			"payment_request": {req.ID},
			"payment":         {pay.ID},
		})
		if terr != nil {
			a.respondStoreError(w, r, terr)
			return
		}
		data.Audit = trail
	}
```

> If Phase 2's audit accessor has a different shape, call it twice and merge by `CreatedAt` rather than adding a new store method; the template only needs an ordered `[]store.AuditEntry`.

In the Phase-2 `requestDetail` handler, load the linked payment (unchanged from the original plan):

```go
	if pay, perr := a.st.PaymentForRequest(r.Context(), req.ID); perr == nil {
		data.Payment = pay
	} else if !errors.Is(perr, store.ErrNotFound) {
		a.respondStoreError(w, r, perr)
		return
	}
```

In `internal/app/templates.go`, rewrite `payment_detail` for the linked case (historical payments keep the existing markup in the `{{else}}` branch until Phase 6 redraws the ledger):

```html
{{define "payment_detail"}}
{{template "top" .}}
{{if .Request2.ID}}
<div class="banner good">
  <span class="b-ico">✓</span>
  <div>
    <b>Payment saved. The request is {{if eq .Request2.Status "partial_review"}}with the manager{{else}}completed{{end}}.</b>
    <p>{{.Request2.RequesterName}} and {{.Request2.ManagerName}} have been notified. This payment can no longer be edited or cancelled.</p>
  </div>
</div>

<div class="req-head">
  <div class="rh-top"><span class="rh-no">PAY-{{.Payment.ID}} · from {{.Request2.Number}}</span><span class="rh-amt">{{money .Payment.Amount}}</span></div>
  <h1>{{.Payment.VendorPayee}}</h1>
  <p class="rh-meta">{{.Payment.Project}} / {{.Payment.Head}} · paid {{.Payment.PaidOn}}</p>
  <div class="rh-status">
    {{if eq .Request2.Status "partial_review"}}<span class="pill partial">Partial — manager review</span><span class="waiting">Waiting on {{.Request2.ManagerName}}</span>
    {{else if eq .Request2.Status "completed_partial"}}<span class="pill completed-partial">Completed — partial accepted</span><span class="waiting done">Nothing pending</span>
    {{else}}<span class="pill completed">Completed</span><span class="waiting done">Nothing pending</span>{{end}}
  </div>
</div>

<div class="compare" style="margin-bottom:14px">
  <div class="cmp-row"><span class="l">Approved</span><span class="v">{{if .Request2.ApprovedAmount}}{{money (deref .Request2.ApprovedAmount)}}{{else}}{{money .Request2.Amount}}{{end}}</span></div>
  <div class="cmp-row"><span class="l">Paid</span><span class="v">{{money .Payment.Amount}}</span></div>
  {{if eq .Payment.Settlement "partial"}}
  <div class="cmp-row diff"><span class="l">Still owed to the payee</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>
  {{else}}
  <div class="cmp-row match"><span class="l">Difference · confirmed settled by Accounts</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>
  {{end}}
</div>

<div class="card">
  <div class="card-head"><h2>Payment</h2><span class="pill neutral no-dot">Read-only</span></div>
  <dl class="dl">
    <div><dt>Paid on</dt><dd>{{.Payment.PaidOn}}</dd></div>
    <div><dt>Mode</dt><dd>{{.Payment.PaymentMode}}</dd></div>
    <div><dt>Reference</dt><dd class="num">{{.Payment.ReferenceNo}}</dd></div>
    <div><dt>Recorded by</dt><dd>{{.Payment.EnteredByName}} at {{date .Payment.CreatedAt}}</dd></div>
    <div><dt>Payee</dt><dd>{{.Payment.VendorPayee}}</dd></div>
    <div><dt>Settlement</dt><dd>{{if eq .Payment.Settlement "partial"}}Partial — a balance is still owed{{else}}Fully settled — deductions handled outside this system{{end}}</dd></div>
    {{if .Payment.PartialReason}}<div style="grid-column:1/-1"><dt>Partial reason</dt><dd>{{.Payment.PartialReason}}</dd></div>{{end}}
    {{if .Payment.Remarks}}<div style="grid-column:1/-1"><dt>Processing note</dt><dd>{{.Payment.Remarks}}</dd></div>{{end}}
  </dl>
</div>

{{if .Attachments}}
<div class="section-head"><h2>Proof</h2></div>
<div class="stack-8">
  {{range .Attachments}}<div class="file-row"><span class="f-ico">{{.Kind}}</span><span><b>{{.OriginalName}}</b><small>{{.SizeLabel}}</small></span><span class="f-actions"><a class="btn small outline" href="/attachments/{{.ID}}">Download</a></span></div>{{end}}
</div>
{{end}}

<div class="section-head"><h2>Full trail, request to payment</h2></div>
<ol class="thread">
  {{range .Audit}}
  <li><span class="tl-dot {{threadTone .Action}}">{{threadGlyph .Action}}</span>
    <div class="tl-head"><b>{{.ActorName}} — {{.Action}}</b><time>{{date .CreatedAt}}</time></div>
    <div class="tl-body">{{.Summary}}</div></li>
  {{else}}<li><div class="tl-body muted">No history recorded.</div></li>{{end}}
</ol>

<div class="action-bar">
  <span class="ab-note d-only">Refunds and reversals are outside this version.</span>
  <span class="row-end"></span>
  <a class="btn outline" href="/requests/{{.Request2.ID}}">Open the request</a>
  <a class="btn" href="/payments">Back to ledger</a>
</div>
{{else}}
{{/* Historical, request-less payment: the pre-Phase-3 detail markup, still editable (X6). */}}
{{template "payment_detail_historical" .}}
{{end}}
{{template "bottom" .}}
{{end}}
```

Add three small template helpers to the `FuncMap` — `sub(a, b int64) int64`, `approvedOf(r store.Request) int64` (approved amount or amount), and `threadTone`/`threadGlyph` mapping an audit action to the mockup's `.tl-dot` modifier (`brand` for submit/reserve, `ok` for approve/settle, `warn` for hold/partial, empty otherwise) and glyph (`＋ ✓ ◷ ₹ ⏸ ✎`). Move the pre-existing detail markup into `payment_detail_historical` unchanged.

Add the outcome block to the Phase-2 `request_detail` template, on the design system:

```html
{{if .Payment.ID}}
<div class="section-head"><h2>Payment outcome</h2></div>
<div class="compare" style="margin-bottom:14px">
  <div class="cmp-row"><span class="l">Approved</span><span class="v">{{money (approvedOf .Request2)}}</span></div>
  <div class="cmp-row"><span class="l">Paid on {{.Payment.PaidOn}}</span><span class="v">{{money .Payment.Amount}}</span></div>
  {{if eq .Payment.Settlement "partial"}}<div class="cmp-row diff"><span class="l">Still owed to the payee</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>
  {{else}}<div class="cmp-row match"><span class="l">Difference · confirmed settled by Accounts</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>{{end}}
</div>
{{if .Payment.PartialReason}}<div class="banner warn"><span class="b-ico">i</span><div><b>{{.Payment.EnteredByName}} marked this a genuine partial payment</b><p>{{.Payment.PartialReason}}</p></div></div>{{end}}
<div class="action-bar"><span class="row-end"></span><a class="btn outline" href="/payments/{{.Payment.ID}}">View the payment</a></div>
{{end}}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run 'TestSettlementFlowCompletesAndShowsPaymentDetail|TestDoubleConfirmLandsOnTheExistingPayment|TestPartialSettlementRoutesToReview' -v`
Expected: PASS (3 tests). Then `go test ./internal/app/` — Expected: `ok`, with the historical-payment detail tests still green.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): settlement submit, payment detail screen and the full request-to-payment trail"
```

---

### Task 17: Partial review screen — two `.overlay > .sheet` confirmations (S11, **G14**)

**New task** (routes were part of old Task 12). Builds `mockups/screens/payment-partial-review.html`: `.req-head` with `.pill.partial` and a `.waiting.you`; the `.compare` block ending in "Still owed to the vendor"; the accountant's reason in a `.banner.warn`; the immutable payment `.card` with `.pill.neutral.no-dot` "Cannot be edited"; the `ol.thread`; the `.comment-box`; an `.action-bar` for the manager only; and **two sheets** — "Accept ₹X and close?" (its own `.compare`, the "closes as Completed — partial accepted" hint and an optional note) and "Raise a concern" (required reason plus the "This does not reverse anything" `.banner.warn`).

**Files:**
- Modify: `internal/app/app.go` (routes; `requestPartialReview`, `requestAcceptPartial`, `requestRaiseConcern`)
- Modify: `internal/app/templates.go` (new `partial_review`)
- Modify: `internal/app/app_integration_test.go` (add tests)

**Interfaces:**
- Consumes: `AcceptPartial` (now with `note`), `RaiseConcern`, `PaymentForRequest`, `Request`, comments + audit from Phase 2.
- Produces: routes `GET /requests/{id}/partial-review` (gated `request:view`), `POST /requests/{id}/accept-partial` and `POST /requests/{id}/raise-concern` (both gated `approval:accept_partial`).

- [ ] **Step 1: Write the failing test**

```go
func TestPartialReviewScreenAndManagerDecision(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Review")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 9500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-23"}, "amount": {"60000.00"}, "vendor_payee": {"Nova Print Works"}, "settlement": {"partial"}, "partial_reason": {"Vendor delivered 700 of the 1,000 copies."}, "reference_no": {"N9912"}}
	requireStatus(t, s.postForm("/payments", form), http.StatusSeeOther)

	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/partial-review", reqID), nil, ""))
	for _, want := range []string{
		`class="req-head"`, `class="pill partial"`, `class="waiting you"`,
		`class="compare"`, "Still owed to the vendor", "35,000.00",
		`class="banner warn"`, "Vendor delivered 700 of the 1,000 copies.",
		`class="pill neutral no-dot"`, "Cannot be edited",
		`<ol class="thread">`, `class="comment-box"`,
		`id="close-sheet"`, "Accept and close", "Completed — partial accepted",
		`id="concern-sheet"`, "Raise a concern", "This does not reverse anything",
		`class="overlay"`, `class="sheet"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("partial review missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<details") {
		t.Fatalf("partial review uses a <details> instead of a sheet:\n%s", body)
	}
	if strings.Count(body, `class="overlay"`) != 2 {
		t.Fatalf("expected two sheets (accept and concern):\n%s", body)
	}

	// Raising a concern keeps it open and records the comment.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/raise-concern", reqID), url.Values{"comment": {"Confirm the balance timeline"}}), http.StatusSeeOther)
	if got := requestStatusApp(t, s, reqID); got != "partial_review" {
		t.Fatalf("status after concern = %q, want partial_review", got)
	}
	// Accepting closes it into the distinct terminal state (G14).
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/accept-partial", reqID), url.Values{"note": {"Balance invoiced separately."}}), http.StatusSeeOther)
	if got := requestStatusApp(t, s, reqID); got != "completed_partial" {
		t.Fatalf("status after accept = %q, want completed_partial (G14)", got)
	}
	after := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""))
	if !strings.Contains(after, `class="pill completed-partial"`) || !strings.Contains(after, "Completed — partial accepted") {
		t.Fatalf("accepted request does not render .pill.completed-partial:\n%s", after)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestPartialReviewScreenAndManagerDecision -v`
Expected: FAIL — `GET /requests/{id}/partial-review` 404s; the accept and concern routes are unregistered.

- [ ] **Step 3: Write minimal implementation**

Routes:

```go
	mux.Handle("GET /requests/{id}/partial-review", a.auth.RequirePermission("request", "view", http.HandlerFunc(a.requestPartialReview)))
	mux.Handle("POST /requests/{id}/accept-partial", a.auth.RequirePermission("approval", "accept_partial", http.HandlerFunc(a.withCSRF(a.requestAcceptPartial))))
	mux.Handle("POST /requests/{id}/raise-concern", a.auth.RequirePermission("approval", "accept_partial", http.HandlerFunc(a.withCSRF(a.requestRaiseConcern))))
```

Handlers:

```go
func (a *App) requestPartialReview(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r) // Phase 2 helper: enforces data scope
	if !ok {
		return
	}
	pay, err := a.st.PaymentForRequest(r.Context(), req.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	comments, cerr := a.st.RequestComments(r.Context(), req.ID)
	if cerr != nil {
		a.respondStoreError(w, r, cerr)
		return
	}
	trail, terr := a.st.AuditFor(r.Context(), map[string][]int64{"payment_request": {req.ID}, "payment": {pay.ID}})
	if terr != nil {
		a.respondStoreError(w, r, terr)
		return
	}
	a.render(w, r, "partial_review", PageData{Title: "Partial payment " + req.Number, Request2: req, Payment: pay, Comments: comments, Audit: trail})
}

func (a *App) requestAcceptPartial(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := a.st.AcceptPartial(r.Context(), auth.CurrentUser(r), id, r.FormValue("note")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", id), http.StatusSeeOther)
}

func (a *App) requestRaiseConcern(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := a.st.RaiseConcern(r.Context(), auth.CurrentUser(r), id, r.FormValue("comment")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d/partial-review", id), http.StatusSeeOther)
}
```

Template (abridged to the structural spine; every class below is required by the test):

```html
{{define "partial_review"}}
{{template "top" .}}
<div class="req-head">
  <div class="rh-top"><span class="rh-no">{{.Request2.Number}}</span><span class="rh-amt">{{money (approvedOf .Request2)}}</span></div>
  <h1>{{.Request2.VendorPayee}} — {{.Request2.Purpose}}</h1>
  <p class="rh-meta">Raised by {{.Request2.RequesterName}} · {{.Request2.Project}} / {{.Request2.Head}}{{if .Request2.ApprovedAt}} · approved {{datep .Request2.ApprovedAt}}{{end}}</p>
  <div class="rh-status">
    <span class="pill partial">Partial — manager review</span>
    {{if eq .User.ID .Request2.ManagerID}}<span class="waiting you">Waiting on you</span>{{else}}<span class="waiting">Waiting on {{.Request2.ManagerName}}</span>{{end}}
  </div>
</div>

<div class="compare" style="margin-bottom:14px">
  <div class="cmp-row"><span class="l">Approved</span><span class="v">{{money (approvedOf .Request2)}}</span></div>
  <div class="cmp-row"><span class="l">Paid on {{.Payment.PaidOn}}</span><span class="v">{{money .Payment.Amount}}</span></div>
  <div class="cmp-row diff"><span class="l">Still owed to the vendor</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>
</div>

<div class="banner warn">
  <span class="b-ico">i</span>
  <div><b>{{.Payment.EnteredByName}} marked this a genuine partial payment</b><p>“{{.Payment.PartialReason}}”</p></div>
</div>

<div class="card">
  <div class="card-head"><h2>The payment that was recorded</h2><span class="pill neutral no-dot">Cannot be edited</span></div>
  <dl class="dl">
    <div><dt>Amount paid</dt><dd class="big">{{money .Payment.Amount}}</dd></div>
    <div><dt>Paid on</dt><dd>{{.Payment.PaidOn}}</dd></div>
    <div><dt>Mode</dt><dd>{{.Payment.PaymentMode}}</dd></div>
    <div><dt>Reference</dt><dd class="num">{{.Payment.ReferenceNo}}</dd></div>
    <div><dt>Recorded by</dt><dd>{{.Payment.EnteredByName}}</dd></div>
  </dl>
</div>

<div class="section-head"><h2>History and conversation</h2></div>
<ol class="thread">
  {{range .Audit}}<li><span class="tl-dot {{threadTone .Action}}">{{threadGlyph .Action}}</span><div class="tl-head"><b>{{.ActorName}} — {{.Action}}</b><time>{{date .CreatedAt}}</time></div><div class="tl-body">{{.Summary}}</div></li>{{end}}
  {{range .Comments}}<li class="is-comment"><span class="tl-dot">{{initials .AuthorName}}</span><div class="tl-head"><b>{{.AuthorName}}</b><time>{{date .CreatedAt}}</time></div><div class="tl-body"><p>{{.Body}}</p></div></li>{{end}}
</ol>

<form class="comment-box" method="post" action="/requests/{{.Request2.ID}}/comments">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <label for="cmt" class="flabel">Reply to Accounts</label>
  <textarea id="cmt" name="body" placeholder="Ask {{.Payment.EnteredByName}} something before you decide."></textarea>
  <div class="cb-actions"><span class="row-end"></span><button class="btn primary small">Post comment</button></div>
</form>

{{if .Perms.Can "approval" "accept_partial"}}
<div class="action-bar">
  <span class="ab-note d-only">No further payment can be attached either way.</span>
  <span class="row-end"></span>
  <button class="btn outline" data-open="concern-sheet">Raise a concern</button>
  <button class="btn approve" data-open="close-sheet">Accept and close</button>
</div>

<div class="overlay" id="close-sheet" hidden>
  <div class="sheet">
    <div class="sh-head"><div><h2>Accept {{money .Payment.Amount}} and close?</h2><p class="sh-sub">{{.Request2.Number}} · {{money (sub (approvedOf .Request2) .Payment.Amount)}} will never be paid against this request</p></div><button class="sh-close" data-close="close-sheet" aria-label="Close">✕</button></div>
    <form method="post" action="/requests/{{.Request2.ID}}/accept-partial">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="sh-body stack-12">
        <div class="compare">
          <div class="cmp-row"><span class="l">Approved</span><span class="v">{{money (approvedOf .Request2)}}</span></div>
          <div class="cmp-row"><span class="l">Paid and accepted</span><span class="v">{{money .Payment.Amount}}</span></div>
          <div class="cmp-row diff"><span class="l">Written off from this request</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>
        </div>
        <p class="hint" style="margin:0">The request closes as <b>Completed — partial accepted</b>. If the balance is still due later, {{.Request2.RequesterName}} raises a new request.</p>
        <div class="field"><label for="cl-note">Note <span class="opt">optional</span></label><textarea id="cl-note" name="note"></textarea></div>
      </div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="close-sheet">Back</button><span class="row-end"></span><button class="btn approve">Accept and close</button></div>
    </form>
  </div>
</div>

<div class="overlay" id="concern-sheet" hidden>
  <div class="sheet">
    <div class="sh-head"><div><h2>Raise a concern</h2><p class="sh-sub">The request stays open as Partial — under discussion</p></div><button class="sh-close" data-close="concern-sheet" aria-label="Close">✕</button></div>
    <form method="post" action="/requests/{{.Request2.ID}}/raise-concern">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="sh-body stack-12">
        <div class="field"><label for="cn-reason">What is wrong <span class="req">*</span></label><textarea id="cn-reason" name="comment" required placeholder="Accounts can reply in the conversation, but the recorded payment cannot be changed."></textarea></div>
        <div class="banner warn" style="margin:0"><span class="b-ico">i</span><div><b>This does not reverse anything</b><p>The {{money .Payment.Amount}} has left the bank. Raising a concern keeps the request open so the two of you can agree what happens next.</p></div></div>
      </div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="concern-sheet">Back</button><span class="row-end"></span><button class="btn primary">Raise concern</button></div>
    </form>
  </div>
</div>
{{else}}
<div class="action-bar">
  <span class="ab-note d-only">{{.Request2.ManagerName}} decides. You can still comment.</span>
  <span class="row-end"></span>
  <a class="btn outline" href="/requests/{{.Request2.ID}}">Back</a>
</div>
{{end}}
{{template "bottom" .}}
{{end}}
```

> `initials` is a small `FuncMap` helper for the comment `.tl-dot`. The mockup's persona `data-for`/`data-not-for` attributes are prototype scaffolding: the two action bars are chosen server-side by `.Perms.Can`, never hidden client-side.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run TestPartialReviewScreenAndManagerDecision -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): partial review screen with accept and concern sheets"
```

---

### Task 18: Release / reassign screen (S6, S7, **G11**, **G12**)

**New task.** Builds `mockups/screens/accounts-release-reassign.html` as one screen with one `.choice`: release, or reassign to a named colleague. The `.reserve-bar` states who holds it and for how long; the `.banner.bad` demands confirmation that no payment has started; the reassign target `<select>` is revealed by `data-when="action:reassign"` **and re-enforced server-side**; the reason is required for both branches; the confirmation checkbox maps to `confirmed`; and a `.thread` shows the reservation history.

**Files:**
- Modify: `internal/app/app.go` (routes; `reservationForm`, `requestRelease`, `requestReassign`)
- Modify: `internal/app/templates.go` (new `reservation_form`)
- Modify: `internal/app/app_integration_test.go` (add tests)

**Interfaces:**
- Consumes: `ReleaseRequest` (Task 4, with reason), `ReassignReservation` (Task 5), `ListUsers`, `auth.Manager.Can`.
- Produces: routes `GET /requests/{id}/reservation` (gated `reservation:release`), `POST /requests/{id}/release` (gated `reservation:release`), `POST /requests/{id}/reassign` (gated `reservation:reassign`).

- [ ] **Step 1: Write the failing test**

```go
func TestReservationScreenReleaseAndReassign(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Release")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 10000000)
	hash, _ := auth.HashPassword("DeepakPass1234")
	deepakID, err := s.st.CreateUser(s.ctx, "deepak@example.test", "Deepak Menon", hash, "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/reservation", reqID), nil, ""))
	for _, want := range []string{
		`class="reserve-bar"`, "Reserved by you",
		`class="banner bad"`, "Confirm no payment has been started",
		`class="choice"`, `value="release"`, `value="reassign"`,
		`data-when="action:reassign"`, "Deepak Menon",
		`name="reason"`, `class="checkline"`,
		`class="action-bar"`, `<ol class="thread">`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("reservation screen missing %q:\n%s", want, body)
		}
	}

	// G12: release without a reason is refused and the reservation survives.
	resp := s.postForm(fmt.Sprintf("/requests/%d/release", reqID), url.Values{"action": {"release"}, "confirm": {"on"}})
	if resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("release without a reason accepted: %d", resp.StatusCode)
	}
	_ = responseBody(t, resp)
	// S7: confirmation is still required.
	resp = s.postForm(fmt.Sprintf("/requests/%d/release", reqID), url.Values{"action": {"release"}, "reason": {"Bank details unconfirmed"}})
	if resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("release without confirmation accepted: %d", resp.StatusCode)
	}
	_ = responseBody(t, resp)
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status after refused releases = %q, want processing", got)
	}

	// G11: reassign keeps it in processing and moves the holder.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/reassign", reqID), url.Values{
		"action": {"reassign"}, "to_user_id": {strconvFormat(deepakID)}, "reason": {"Going on leave"}, "confirm": {"on"},
	}), http.StatusSeeOther)
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status after reassign = %q, want processing", got)
	}
	var holder int64
	if err := s.st.DB().QueryRow(`SELECT processing_by FROM payment_requests WHERE id=?`, reqID).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if holder != deepakID {
		t.Fatalf("processing_by = %d, want %d", holder, deepakID)
	}
}

// The reassign target is revealed by data-when; hidden is not validation.
func TestReassignRefusesWithoutATargetServerSide(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("ReassignGuard")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	resp := s.postForm(fmt.Sprintf("/requests/%d/reassign", reqID), url.Values{"action": {"reassign"}, "reason": {"someone else"}, "confirm": {"on"}})
	if resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("reassign with no target accepted: %d", resp.StatusCode)
	}
	_ = responseBody(t, resp)
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status = %q, want processing", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run 'TestReservationScreenReleaseAndReassign|TestReassignRefusesWithoutATargetServerSide' -v`
Expected: FAIL — `GET /requests/{id}/reservation` and `POST /requests/{id}/reassign` 404; the release POST is unregistered.

- [ ] **Step 3: Write minimal implementation**

```go
	mux.Handle("GET /requests/{id}/reservation", a.auth.RequirePermission("reservation", "release", http.HandlerFunc(a.reservationForm)))
	mux.Handle("POST /requests/{id}/release", a.auth.RequirePermission("reservation", "release", http.HandlerFunc(a.withCSRF(a.requestRelease))))
	mux.Handle("POST /requests/{id}/reassign", a.auth.RequirePermission("reservation", "reassign", http.HandlerFunc(a.withCSRF(a.requestReassign))))
```

```go
func (a *App) reservationForm(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	req, err := a.st.Request(r.Context(), pathID(r))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if req.Status != "processing" {
		a.respondError(w, r, http.StatusConflict, "This request is not reserved by anyone.", nil)
		return
	}
	var users []store.User
	if a.auth.Can(u, "reservation", "reassign") {
		if users, err = a.st.ListUsers(r.Context(), true); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
	trail, terr := a.st.AuditFor(r.Context(), map[string][]int64{"payment_request": {req.ID}})
	if terr != nil {
		a.respondStoreError(w, r, terr)
		return
	}
	a.render(w, r, "reservation_form", PageData{
		Title: "Release reservation", Request2: req, Users: users, Audit: trail,
		ReserveMine: req.ProcessingBy != nil && *req.ProcessingBy == u.ID,
	})
}

func (a *App) requestRelease(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	u := auth.CurrentUser(r)
	err := a.st.ReleaseRequest(r.Context(), u, id, r.FormValue("reason"), r.FormValue("confirm") == "on", a.auth.Can(u, "reservation", "reassign"))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/requests/to-pay", http.StatusSeeOther)
}

func (a *App) requestReassign(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	u := auth.CurrentUser(r)
	// data-when reveals the target field; the handler is what enforces it.
	to := parseID(r.FormValue("to_user_id"))
	if to == 0 {
		a.respondError(w, r, http.StatusBadRequest, "Choose who should take this reservation.", nil)
		return
	}
	if r.FormValue("confirm") != "on" {
		a.respondError(w, r, http.StatusBadRequest, "Confirm that no payment has been initiated.", nil)
		return
	}
	if err := a.st.ReassignReservation(r.Context(), u, id, to, r.FormValue("reason"), a.auth.Can(u, "reservation", "reassign")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", id), http.StatusSeeOther)
}
```

```html
{{define "reservation_form"}}
{{template "top" .}}
<section class="page-banner d-only">
  <div>
    <div class="eyebrow">Accounts · reservation</div>
    <h1>Release or reassign this request</h1>
    <p class="sub">{{.Request2.Number}} · {{.Request2.VendorPayee}} · {{money (approvedOf .Request2)}}</p>
  </div>
</section>

<div class="reserve-bar">
  <span class="rb-dot"></span>
  <b>{{if .ReserveMine}}Reserved by you{{else}}Reserved by {{.Request2.ProcessingByName}}{{end}}</b>
  <span class="rb-meta">since {{hhmm .Request2.ProcessingAt}} · {{since .Request2.ProcessingAt}}</span>
</div>

<div class="banner bad">
  <span class="b-ico">!</span>
  <div>
    <b>Confirm no payment has been started</b>
    <p>Releasing puts the request back in the open queue and anyone in Accounts can take it. If you have already initiated a transfer in the bank portal, do not release — finish recording it.</p>
  </div>
</div>

<form method="post" action="/requests/{{.Request2.ID}}/release" id="reservation-form">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <fieldset>
    <legend>What do you want to do</legend>
    <div class="choice">
      <label><input type="radio" name="action" value="release" checked>
        <span><b>Release it</b><small>Back to Approved — awaiting payment. Anyone in Accounts can pick it up.</small></span></label>
      {{if .Perms.Can "reservation" "reassign"}}
      <label><input type="radio" name="action" value="reassign">
        <span><b>Reassign to someone else</b><small>Stays in Processing, assigned to the person you choose.</small></span></label>
      {{end}}
    </div>

    <div class="form-grid" style="margin-top:13px">
      {{if .Perms.Can "reservation" "reassign"}}
      <div class="field span-6" data-when="action:reassign" hidden>
        <label for="to_user_id">Reassign to <span class="req">*</span></label>
        <select id="to_user_id" name="to_user_id"><option value="">Choose…</option>{{range .Users}}{{if ne .ID $.User.ID}}<option value="{{.ID}}">{{.Name}}</option>{{end}}{{end}}</select>
      </div>
      {{end}}
      <div class="field span-12">
        <label for="reason">Reason <span class="req">*</span></label>
        <textarea id="reason" name="reason" required placeholder="Recorded in the history and visible to everyone who can see this request."></textarea>
      </div>
      <div class="field span-12">
        <label class="checkline"><input type="checkbox" name="confirm" value="on" required> I confirm no payment has been initiated for this request</label>
      </div>
    </div>
  </fieldset>

  <div class="action-bar">
    <span class="ab-note d-only">The requester and the approver are both notified.</span>
    <span class="row-end"></span>
    <a class="btn outline" href="/payments/new?request={{.Request2.ID}}">Keep working on it</a>
    {{if .Perms.Can "reservation" "reassign"}}<button class="btn outline" data-when="action:reassign" hidden formaction="/requests/{{.Request2.ID}}/reassign">Reassign</button>{{end}}
    <button class="btn danger" data-when="action:release" formaction="/requests/{{.Request2.ID}}/release">Release reservation</button>
  </div>
</form>

<div class="section-head"><h2>Reservation history</h2></div>
<ol class="thread">
  {{range .Audit}}{{if or (eq .Action "process") (eq .Action "release") (eq .Action "reassign")}}
  <li><span class="tl-dot {{threadTone .Action}}">◷</span><div class="tl-head"><b>{{.ActorName}} — {{.Action}}</b><time>{{date .CreatedAt}}</time></div><div class="tl-body">{{.Summary}}</div></li>
  {{end}}{{else}}<li><div class="tl-body muted">No reservation history yet.</div></li>{{end}}
</ol>
{{template "bottom" .}}
{{end}}
```

> One form, two `formaction` targets chosen by the same `[data-when]` contract that reveals the target select. Both handlers re-validate everything the reveal implies, so a hand-crafted POST cannot reassign without a target or release without a reason.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run 'TestReservationScreenReleaseAndReassign|TestReassignRefusesWithoutATargetServerSide' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): release and reassign reservation screen with required reason"
```

---

### Task 19: Hold, unhold, and the stale-reservation screen (L7, Q6)

**New task** (routes were part of old Task 12). Two screens. `request-on-hold.html` is a state of the Phase-2 request detail: `.pill.hold`, the accountant's question in a `.banner.warn`, a `.comment-box` for the requester's answer, and an Accounts-only `.action-bar` with "Keep on hold" / "Release hold". `accounts-stale-processing.html` is the 26-hour nudge: a `.banner.warn`, the `.req-head`, an `.a-list` of four choices, a "Who has been told" `.thread`, and an admin-only reassign `.action-bar`.

**Files:**
- Modify: `internal/app/app.go` (routes; `requestHold`, `requestUnhold`, `requestStale`)
- Modify: `internal/app/templates.go` (hold block on `request_detail`; new `reservation_stale`)
- Modify: `internal/app/app_integration_test.go` (add tests)

**Interfaces:**
- Consumes: `HoldRequest`, `UnholdRequest` (Task 9); `LinkablePaymentRequests` counts (Task 10).
- Produces: routes `POST /requests/{id}/hold`, `POST /requests/{id}/unhold` (gated `payment:hold`), `GET /requests/{id}/reservation/stale` (gated `payment:process`).

- [ ] **Step 1: Write the failing test**

```go
func TestHoldAndUnholdMoveTheRequestThroughTheQueue(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Hold")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 2500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/requests/to-pay", nil, ""))
	if !strings.Contains(body, "PR-2026-000001") {
		t.Fatalf("approved request missing from the queue:\n%s", body)
	}
	// Hold takes it out of the takeable tab (L7) and onto the hold tab.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", reqID), url.Values{"reason": {"Which site is this for?"}}), http.StatusSeeOther)
	body = responseBody(t, s.request(http.MethodGet, "/requests/to-pay", nil, ""))
	if strings.Contains(body, "Take for processing") {
		t.Fatalf("held request is still takeable:\n%s", body)
	}
	held := responseBody(t, s.request(http.MethodGet, "/requests/to-pay?tab=hold", nil, ""))
	if !strings.Contains(held, "PR-2026-000001") {
		t.Fatalf("held request missing from the hold tab:\n%s", held)
	}
	// The request screen explains the hold and offers the Accounts-only release.
	detail := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""))
	for _, want := range []string{`class="pill hold"`, `class="banner warn"`, "Which site is this for?", "Release hold", `class="comment-box"`} {
		if !strings.Contains(detail, want) {
			t.Fatalf("on-hold request detail missing %q:\n%s", want, detail)
		}
	}
	// Unhold restores it.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/unhold", reqID), url.Values{}), http.StatusSeeOther)
	body = responseBody(t, s.request(http.MethodGet, "/requests/to-pay", nil, ""))
	if !strings.Contains(body, "Take for processing") {
		t.Fatalf("unheld request is not takeable again:\n%s", body)
	}
}

func TestStaleReservationScreenOffersTheFourChoices(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Stale")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 7800000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET processing_at=datetime('now','-26 hours') WHERE id=?`, reqID); err != nil {
		t.Fatal(err)
	}
	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/reservation/stale", reqID), nil, ""))
	for _, want := range []string{
		`class="banner warn"`, "reserved by you for", "26 h",
		`class="req-head"`, `class="pill processing"`, `class="waiting"`,
		`class="a-list"`, "Carry on and record the payment", "Release it", "Hand it to a colleague", "Put it on hold",
		"Who has been told", `<ol class="thread">`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stale reservation screen missing %q:\n%s", want, body)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run 'TestHoldAndUnholdMoveTheRequestThroughTheQueue|TestStaleReservationScreenOffersTheFourChoices' -v`
Expected: FAIL — the hold, unhold and stale routes are unregistered (404 / redirect mismatch).

- [ ] **Step 3: Write minimal implementation**

```go
	mux.Handle("POST /requests/{id}/hold", a.auth.RequirePermission("payment", "hold", http.HandlerFunc(a.withCSRF(a.requestHold))))
	mux.Handle("POST /requests/{id}/unhold", a.auth.RequirePermission("payment", "hold", http.HandlerFunc(a.withCSRF(a.requestUnhold))))
	mux.Handle("GET /requests/{id}/reservation/stale", a.auth.RequirePermission("payment", "process", http.HandlerFunc(a.requestStale)))
```

```go
func (a *App) requestHold(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := a.st.HoldRequest(r.Context(), auth.CurrentUser(r), id, r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", id), http.StatusSeeOther)
}

func (a *App) requestUnhold(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := a.st.UnholdRequest(r.Context(), auth.CurrentUser(r), id); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", id), http.StatusSeeOther)
}

func (a *App) requestStale(w http.ResponseWriter, r *http.Request) {
	req, err := a.st.Request(r.Context(), pathID(r))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	trail, terr := a.st.AuditFor(r.Context(), map[string][]int64{"payment_request": {req.ID}})
	if terr != nil {
		a.respondStoreError(w, r, terr)
		return
	}
	u := auth.CurrentUser(r)
	a.render(w, r, "reservation_stale", PageData{Title: "Reserved too long", Request2: req, Audit: trail,
		ReserveMine: req.ProcessingBy != nil && *req.ProcessingBy == u.ID})
}
```

Add the hold block to the Phase-2 `request_detail` template (the on-hold state of that screen), permission-driven:

```html
{{if .Request2.OnHold}}
<div class="banner warn">
  <span class="b-ico">⏸</span>
  <div><b>This request is on hold</b><p>“{{.Request2.HoldReason}}”</p></div>
</div>
{{if .Perms.Can "payment" "hold"}}
<div class="action-bar">
  <span class="ab-note d-only">Releasing returns it to Approved — awaiting payment.</span>
  <span class="row-end"></span>
  <a class="btn outline" href="/requests/{{.Request2.ID}}">Keep on hold</a>
  <form method="post" action="/requests/{{.Request2.ID}}/unhold"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="btn primary">Release hold</button></form>
</div>
{{else}}
<div class="action-bar"><span class="ab-note d-only">Only Accounts can take this off hold.</span><span class="row-end"></span></div>
{{end}}
{{else if and (eq .Request2.Status "approved") (.Perms.Can "payment" "hold")}}
<div class="action-bar">
  <span class="row-end"></span>
  <button class="btn outline" data-open="hold-sheet">Put on hold</button>
</div>
<div class="overlay" id="hold-sheet" hidden>
  <div class="sheet">
    <div class="sh-head"><div><h2>Put {{.Request2.Number}} on hold</h2><p class="sh-sub">Payment is blocked until Accounts lifts it</p></div><button class="sh-close" data-close="hold-sheet" aria-label="Close">✕</button></div>
    <form method="post" action="/requests/{{.Request2.ID}}/hold">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="sh-body stack-12"><div class="field"><label for="hold-reason">What do you need from the requester <span class="req">*</span></label><textarea id="hold-reason" name="reason" required></textarea></div></div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="hold-sheet">Back</button><span class="row-end"></span><button class="btn primary">Put on hold</button></div>
    </form>
  </div>
</div>
{{end}}
```

And the stale screen:

```html
{{define "reservation_stale"}}
{{template "top" .}}
<div class="banner warn">
  <span class="b-ico">◷</span>
  <div>
    <b>This has been reserved by you for {{since .Request2.ProcessingAt}}</b>
    <p>A reminder went out at the one-day mark. Nothing is released automatically — a transfer may already be under way, so only you or an authorised colleague can act.</p>
  </div>
</div>

<div class="req-head">
  <div class="rh-top"><span class="rh-no">{{.Request2.Number}}</span><span class="rh-amt">{{money (approvedOf .Request2)}}</span></div>
  <h1>{{.Request2.VendorPayee}} — {{.Request2.Purpose}}</h1>
  <p class="rh-meta">Raised by {{.Request2.RequesterName}} · {{.Request2.Project}} / {{.Request2.Head}}{{if .Request2.NeededBy}} · needed by {{.Request2.NeededBy}}{{end}}</p>
  <div class="rh-status">
    <span class="pill processing">Processing — {{.Request2.ProcessingByName}}</span>
    <span class="waiting">{{if .ReserveMine}}Reserved by you{{else}}Reserved by {{.Request2.ProcessingByName}}{{end}} since {{datep .Request2.ProcessingAt}}</span>
  </div>
</div>

<div class="card">
  <div class="card-head"><h2>Pick one</h2></div>
  <div class="a-list">
    {{if .ReserveMine}}<a href="/payments/new?request={{.Request2.ID}}"><span class="al-main"><b>Carry on and record the payment</b><small>Opens the payment form with your reservation intact</small></span><span class="al-amt">→</span></a>{{end}}
    <a href="/requests/{{.Request2.ID}}/reservation"><span class="al-main"><b>Release it</b><small>Requires a reason and confirming no payment was initiated</small></span><span class="al-amt">→</span></a>
    {{if .Perms.Can "reservation" "reassign"}}<a href="/requests/{{.Request2.ID}}/reservation"><span class="al-main"><b>Hand it to a colleague</b><small>Stays reserved, assigned to them, with your reason recorded</small></span><span class="al-amt">→</span></a>{{end}}
    <a href="/requests/{{.Request2.ID}}"><span class="al-main"><b>Put it on hold</b><small>If you are waiting on the requester for something</small></span><span class="al-amt">→</span></a>
  </div>
</div>

<div class="section-head"><h2>Who has been told</h2></div>
<ol class="thread">
  {{range .Audit}}<li><span class="tl-dot {{threadTone .Action}}">{{threadGlyph .Action}}</span><div class="tl-head"><b>{{.ActorName}} — {{.Action}}</b><time>{{date .CreatedAt}}</time></div><div class="tl-body">{{.Summary}}</div></li>{{end}}
</ol>
{{template "bottom" .}}
{{end}}
```

> The mockup's four choices are all present, but "Hand it to a colleague" is permission-gated and "Carry on" only appears for the holder — the mockup's `data-for="admin"` bar is replaced by server-side gating for the same reason as everywhere else.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run 'TestHoldAndUnholdMoveTheRequestThroughTheQueue|TestStaleReservationScreenOffersTheFourChoices' -v`
Expected: PASS (2 tests). Then `go test ./internal/app/` — Expected: `ok` (all app tests).

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): hold, unhold and the stale reservation screen"
```

---

### Task 20: e2e — the accounts journey on a 390 px viewport (S1, S2, S10, S11, S12, D8)

**Amended.** The original spec drove the retired markup ("Linked to request", a `Payment settled` button that posted directly, a redirect back to the request). It is retargeted at the approved screens and, per spec §6, walks the journey on a **390 px** viewport so the mobile chrome, the restacked `t-cards` and the sheets are all exercised.

**Files:**
- Modify: `tests/e2e/fixtures.ts` (add `createApprovedRequest`)
- Create: `tests/e2e/linking-settlement.spec.ts`

**Interfaces:**
- Consumes: Phase-2 request-creation + approval UI (via a fixture helper); the Phase-3 queue, picker, entry screen, settlement sheet and payment detail.

- [ ] **Step 1: Write the failing test** (create `tests/e2e/linking-settlement.spec.ts`)

```ts
import { test, expect, capturePageErrors, createApprovedRequest } from './fixtures';

test.beforeEach(async ({}, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium', 'Linking flow runs once on chromium.');
});

test.describe('payment linking and settlement', () => {
  test('accountant reserves from the queue, confirms in the sheet, and the payment is immutable', async ({ adminPage, runId }) => {
    const errors = capturePageErrors(adminPage);
    await adminPage.setViewportSize({ width: 390, height: 850 });
    const number = await createApprovedRequest(adminPage, runId, '5000.00');

    // Entry point: the queue's "Take for processing" reserves atomically (S1/S2).
    await adminPage.goto('/requests/to-pay');
    await expect(adminPage.locator('.metric-strip .metric')).toHaveCount(4);
    await expect(adminPage.locator('.segmented a')).toHaveCount(5);
    const row = adminPage.locator('tr', { hasText: number });
    await row.getByRole('button', { name: 'Take for processing' }).click();
    await expect(adminPage).toHaveURL(/\/payments\/new\?request=\d+$/);
    await expect(adminPage.locator('.reserve-bar')).toContainText('Reserved by you');

    // Under-paying opens the settlement sheet; nothing is saved yet (D8).
    await adminPage.getByLabel('Amount actually paid').fill('4000.00');
    await expect(adminPage.locator('#diff-banner')).toHaveClass(/warn/);
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    const sheet = adminPage.locator('.overlay .sheet');
    await expect(sheet).toBeVisible();
    await expect(sheet.locator('.cmp-row.diff')).toContainText('1,000.00');
    await expect(sheet.locator('.outcome.good')).toHaveText('Completed');
    await expect(sheet.locator('.outcome.warn')).toHaveText('Manager review');
    // The partial reason only appears once partial is chosen.
    await expect(sheet.locator('[data-when="settlement:partial"]')).toBeHidden();

    // Confirm: one POST, and we land on the payment (S10).
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();
    await expect(adminPage).toHaveURL(/\/payments\/\d+$/);
    await expect(adminPage.locator('.pill.completed')).toBeVisible();
    await expect(adminPage.locator('.compare .cmp-row.match')).toBeVisible();
    await expect(adminPage.locator('ol.thread li')).not.toHaveCount(0);

    // S12: no mutation control anywhere on a linked payment.
    await expect(adminPage.getByRole('link', { name: /Edit/ })).toHaveCount(0);
    await expect(adminPage.getByRole('button', { name: /Void/ })).toHaveCount(0);

    // The page never scrolls sideways at 390 px.
    const overflow = await adminPage.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(0);
    expect(errors).toEqual([]);
  });

  test('a second accountant gets the conflict screen, not an error page', async ({ adminPage, secondPage, runId }) => {
    const number = await createApprovedRequest(adminPage, runId, '2500.00');
    await adminPage.goto('/requests/to-pay');
    await adminPage.locator('tr', { hasText: number }).getByRole('button', { name: 'Take for processing' }).click();
    await expect(adminPage).toHaveURL(/\/payments\/new\?request=\d+$/);

    await secondPage.goto('/requests/to-pay?tab=processing');
    await secondPage.locator('tr', { hasText: number }).getByRole('link', { name: /Reassign|View/ }).click();
    await expect(secondPage.locator('.banner.bad, .reserve-bar')).toBeVisible();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npm run test:e2e -- linking-settlement`
Expected: FAIL — `createApprovedRequest` is not exported from `fixtures.ts` (TypeScript/import error), so the spec cannot run.

- [ ] **Step 3: Write minimal implementation** (add to `tests/e2e/fixtures.ts`)

```ts
// createApprovedRequest drives the Phase-2 request form + approval and returns
// the generated request number. Selectors follow the Phase-2 screens; reconcile
// with the Phase-2 UI labels if they differ.
export async function createApprovedRequest(page: Page, runId: string, amount: string) {
  await page.goto('/requests/new');
  await page.getByRole('link', { name: /Vendor invoice/ }).click();
  await page.getByLabel('Project / Head').selectOption({ index: 1 });
  await page.getByLabel(/Amount/).fill(amount);
  await page.getByLabel('Purpose').fill(`Purpose ${runId}`);
  await page.getByLabel(/Vendor|Payee/).fill(`Vendor ${runId}`);
  await page.getByLabel('Approving manager').selectOption({ index: 1 });
  await page.getByRole('button', { name: /Submit/ }).click();
  const number = (await page.locator('.rh-no, h1').first().innerText()).match(/PR-\d{4}-\d{6}/)?.[0] ?? '';
  await page.getByLabel('Approved amount').fill(amount);
  await page.getByRole('button', { name: /Approve/ }).click();
  return number;
}
```

> `secondPage` is a second authenticated fixture context. If the suite has no such fixture, add one alongside `adminPage` logging in as a separate Accounts user; two contexts are the only way to drive a real reservation race through the browser.

- [ ] **Step 4: Run test to verify it passes**

Run: `npm run typecheck && npm run test:e2e -- linking-settlement`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add tests/e2e/fixtures.ts tests/e2e/linking-settlement.spec.ts
git commit -m "test(e2e): accounts reserve-settle journey and conflict screen at 390 px"
```

---

### Task 21: Proof-of-absence — no refund / return-of-money path (X3)

**Files:**
- Modify: `internal/app/app_integration_test.go` (add `TestNoRefundRoute`; add `"reflect"` to its imports)

**Interfaces:**
- Consumes: the app router (`App.routes`) and `*store.Store` (via `newAppTestServer`).
- Produces: a regression guard proving no refund route and no refund store method exist. Money leaves only through reserve → record-payment, and a recorded payment is immutable (S12); there is deliberately no refund / return-of-money path.

- [ ] **Step 1: Write the test** (append to `internal/app/app_integration_test.go`; add `"reflect"` to its imports)

```go
func TestNoRefundRoute(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	// X3: money leaves only through reserve → record-payment, and a recorded
	// payment is immutable (S12). There is deliberately no refund / return-of-money
	// endpoint, so both plausible refund routes must be unrouted (404).
	for _, path := range []string{"/payments/1/refund", "/requests/1/refund"} {
		resp := s.postForm(path, url.Values{})
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("POST %s = %d, want 404 (no refund route may exist, X3)", path, resp.StatusCode)
		}
		_ = responseBody(t, resp)
	}
	// And no refund method may exist on the store either.
	st := reflect.TypeOf(s.st)
	for i := 0; i < st.NumMethod(); i++ {
		if name := st.Method(i).Name; strings.Contains(strings.ToLower(name), "refund") {
			t.Fatalf("unexpected refund store method %q (X3): payments are immutable; refunds are out of scope", name)
		}
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/app/ -run TestNoRefundRoute -v`
Expected: PASS. This is a proof-of-absence guard — unlike the feature tasks it has no red phase; the correct state is that no refund route or store method exists, and the test locks that in against a future regression.

- [ ] **Step 3: No implementation**

Nothing is built. The guarantee is that no refund route is ever registered in `App.routes` and no `Refund*` method is ever added to `*store.Store`. If a later change adds either, this test turns red.

- [ ] **Step 4: Confirm it still passes**

Run: `go test ./internal/app/ -run TestNoRefundRoute -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app_integration_test.go
git commit -m "test(app): proof-of-absence guard for no refund route or store method (X3)"
```

---

## Final verification

- [ ] Run `make test-race` — Expected: `ok` for `internal/store` and `internal/app` with no race reports (the S5 concurrency test and the reserve/reassign contention test both pass under `-race`).
- [ ] Run `make test-cover` — Expected: coverage profile written, all packages pass.
- [ ] Run `make typecheck && make test-e2e` — Expected: Playwright suites green, including the 390 px journey.
- [ ] **Design-system conformance.** Each command must return what is stated:
  - `grep -c 'class="badge' internal/app/templates.go` → `0` (D5).
  - `grep -c '<details' internal/app/templates.go` → `0` in every template this phase added or edited; sheets replaced them.
  - `grep -c 'eq .User.Role' internal/app/templates.go` → `0`; every gate is `.Perms.Can`.
  - Every screen this phase owns has a route: `/requests/to-pay`, `/payments/new`, `/payments/new?request=`, `/requests/{id}/settlement-preview`, `/payments/{id}`, `/requests/{id}/partial-review`, `/requests/{id}/reservation`, `/requests/{id}/reservation/stale`, and the on-hold state of `/requests/{id}` — ten mockups, ten rendered routes.
- [ ] **D8 proof.** `settlementPreview` contains no `BeginTx`, no `stageUploadedAttachment` and no `Exec`; the only writer is `POST /payments`.
- [ ] Self-review the diff against the coverage table below; confirm no free-standing payment route remains (`grep -n 'CreatePaymentWithAttachment' internal/app/` returns no handler call), that no refund route or store method exists (`grep -rniE 'refund' internal/app internal/store` finds only the `TestNoRefundRoute` guard), and historical payment tests still pass.

## Coverage

| ID | Task | Test name |
|---|---|---|
| L7 | 9, 19 | `TestHoldUnholdPreserveApprovedFieldsAndAllowComments`; `TestHoldAndUnholdMoveTheRequestThroughTheQueue` |
| L8 | 3 | `TestReserveRequestMovesApprovedToProcessing` |
| L9 | 6 | `TestRecordPaymentPartialNeedsReasonAndRoutesToReview` |
| L10 | 6, 10 | `TestRecordPaymentSettledCompletesEvenWhenUnderApproved`; `TestLinkablePaymentRequestsFilterAndSearch` |
| L11 | 3, 6, 8 | `TestReserveRequestMovesApprovedToProcessing`; `TestRecordPaymentPartialNeedsReasonAndRoutesToReview`; `TestAcceptPartialAndRaiseConcern` |
| Q4 | 2, 16 | `TestPaymentForRequestAndHistoricalFields`; `TestSettlementFlowCompletesAndShowsPaymentDetail` |
| Q6 | 9 | `TestHoldUnholdPreserveApprovedFieldsAndAllowComments` |
| S1 | 11, 12, 20 | `TestReservationEntryPointAndPrefill`; `TestAccountsQueueRendersMetricsTabsAndRowActions`; e2e `accountant reserves from the queue…` |
| S2 | 3, 11 | `TestReserveRequestMovesApprovedToProcessing`; `TestReservationEntryPointAndPrefill` |
| S3 | 10, 13 | `TestLinkablePaymentRequestsFilterAndSearch`; `TestPaymentPickerRendersComboAndTakenRows` |
| S4 | 10, 12 | `TestLinkablePaymentRequestsFilterAndSearch`; `TestAccountsQueueSearchNarrowsRows` |
| S5 | 3, 11 | `TestReserveRequestIsAtomicUnderConcurrency` (race); `TestReservationConflictRendersScreenNotErrorPage` |
| S6 | 4, 18 | `TestReleaseRequestRequiresConfirmReasonAndAuthority`; `TestReservationScreenReleaseAndReassign` |
| S7 | 4, 18 | `TestReleaseRequestRequiresConfirmReasonAndAuthority`; `TestReservationScreenReleaseAndReassign` |
| S9 | 1, 6, 16 | `TestPaymentRequestIndexRejectsDuplicateLinkButAllowsNullHistoricals`; `TestRecordPaymentSettledCompletesEvenWhenUnderApproved`; `TestDoubleConfirmLandsOnTheExistingPayment` |
| S10 | 6, 16 | `TestRecordPaymentSettledCompletesEvenWhenUnderApproved`; `TestSettlementFlowCompletesAndShowsPaymentDetail` |
| S11 | 8, 16, 17 | `TestAcceptPartialAndRaiseConcern`; `TestPartialSettlementRoutesToReview`; `TestPartialReviewScreenAndManagerDecision` |
| S12 | 7, 16 | `TestLinkedPaymentIsImmutable`; `TestSettlementFlowCompletesAndShowsPaymentDetail` |
| S13 | 6 | `TestRecordPaymentPartialNeedsReasonAndRoutesToReview` |
| S14 | 11, 14 | `TestReservationEntryPointAndPrefill`; `TestPaymentEntryScreenShowsReservationApprovalAndRule` |
| S15 / X5 | 6, 11 | `TestRecordPaymentRequiresActorReservation`; `TestPaymentCreateRequiresReservedRequest` |
| X3 | 21 | `TestNoRefundRoute` |
| X6 | 1, 7 | `TestMigrationV4IsIdempotentAndLeavesHistoricalPaymentsUntouched`; `TestLinkedPaymentIsImmutable` (historical rows still editable) |
| **G11** | 5, 18 | `TestReassignReservationRequiresPermissionReasonAndActiveTarget`; `TestReservationScreenReleaseAndReassign`, `TestReassignRefusesWithoutATargetServerSide` |
| **G12** | 4, 18 | `TestReleaseRequestRequiresConfirmReasonAndAuthority` (reason in the audit trail); `TestReservationScreenReleaseAndReassign` |
| **G13** | 6, 14 | `TestRecordPaymentRejectsOverpayment`; `TestPaymentEntryScreenShowsReservationApprovalAndRule` (the rule is on screen) |
| **G14** | 8, 12, 17 | `TestAcceptPartialAndRaiseConcern` (writes `completed_partial`); `TestLinkablePaymentRequestsTabsCountsAndReserver` (Paid tab counts both); `TestPartialReviewScreenAndManagerDecision` (`.pill.completed-partial`) |
| **G15** | 11, 14, 15 | `TestReservationConflictRendersScreenNotErrorPage`; `TestPaymentEntryRefusesSomeoneElsesReservation`; `TestSettlementPreviewRefusesWhenTheReservationIsGone` |
| **G16 / D8** | 15, 16 | `TestSettlementPreviewWritesNothingAndRendersTheSheet`; `TestSettlementPreviewMatchesRowAndNoJSFullPage`; `TestSettlementFlowCompletesAndShowsPaymentDetail` |
| **Design system** | 12–19 | `.metric-strip`/`.segmented`/`t-cards`: `TestAccountsQueueRendersMetricsTabsAndRowActions` · `.combo`/`.co.is-taken`: `TestPaymentPickerRendersComboAndTakenRows` · `.reserve-bar`/`.money-field`: `TestPaymentEntryScreenShowsReservationApprovalAndRule` · `.overlay > .sheet`/`.choice`/`.outcome`/`.compare`: `TestSettlementPreviewWritesNothingAndRendersTheSheet`, `TestPartialReviewScreenAndManagerDecision` · `.thread`: `TestSettlementFlowCompletesAndShowsPaymentDetail` · `.a-list`/`.waiting`: `TestStaleReservationScreenOffersTheFourChoices` · no `.badge`, no `<details>`: `TestAccountsQueueRendersMetricsTabsAndRowActions`, `TestPaymentEntryScreenShowsReservationApprovalAndRule`, `TestPartialReviewScreenAndManagerDecision` |
