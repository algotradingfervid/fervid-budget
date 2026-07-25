# Phase 3 — Payment Linking, Reservation & Settlement · Spec

**Parent:** `2026-07-25-payment-requests-overview.md`. **Depends on:** Phase 1 (migration runner + `auth.Manager.Can`/`Scope`), Phase 2 (`payment_requests` table, `store.Request`, `RequestInput`, `ListRequests`, `Request`, request statuses/transitions).

**Covers matrix IDs:** Q4, Q6, L7, L8, L9, L10, L11 (settlement transitions), S1, S2, S3, S4, S5, S6, S7, S9, S10, S11, S12, S13, S14, S15, X3, X5, X6. (S8 stale-processing reminder is **Phase 5** — not built here; the `processing_at` column it needs is added and populated by this phase.)

**Goal:** Put an explicit, atomic reservation → payment → settlement flow in front of the existing ledger so every new payment is created **only** by linking to one approved, actor-reserved request, with exactly one payment per request, a settlement decision that drives the request to *completed* or *partial_review*, and manager accept / raise-concern on partial results — while historical (`request_id IS NULL`) payments stay untouched.

---

## 1. Migration (v3)

Phase 1 registers migration **v1** (permission tables); Phase 2 registers **v2** (request tables); Phase 3 registers **v3** in the same ordered `[]migration` slice in `internal/store/migrations.go`. Migration numbers are contiguous and never reordered (overview §4). `Up(*sql.Tx)`:

1. Guard each column add with `columnExists(tx,"payments",col)` (Phase-1 helper). Add `request_id`, `settlement`, `partial_reason`.
2. `CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_request ON payments(request_id) WHERE request_id IS NOT NULL;` — the DB-level "one payment per request" guarantee (S9).

The migration is idempotent: reopening an already-migrated DB is a no-op (columns already present → skipped; `IF NOT EXISTS` index). Historical payment rows are never rewritten (X6).

## 2. Schema (migration v3)

```sql
ALTER TABLE payments ADD COLUMN request_id INTEGER REFERENCES payment_requests(id); -- NULL for historical rows
ALTER TABLE payments ADD COLUMN settlement TEXT NOT NULL DEFAULT '';                 -- '' (historical) | 'settled' | 'partial'
ALTER TABLE payments ADD COLUMN partial_reason TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_request ON payments(request_id) WHERE request_id IS NOT NULL;
```

`ADD COLUMN … NOT NULL DEFAULT ''` is legal in SQLite because a constant default is supplied; existing rows adopt `''`. `request_id` is nullable with no default so historical rows read back `NULL`.

## 3. Store API (types + methods)

### Type changes
`store.Payment` gains three fields (models.go):
```go
RequestID     *int64  // nil for historical payments
Settlement    string  // '', 'settled', 'partial'
PartialReason string
```
The `Payment(ctx,id)` and `paymentInTx` scans and a new `PaymentForRequest` scan select `py.request_id, COALESCE(py.settlement,''), COALESCE(py.partial_reason,'')`. `ListPayments`/`RecentPayments` are left unchanged (they never surface settlement columns; the zero values are correct).

### New / changed methods on `*Store` (all mutations use `BeginTx`+`defer tx.Rollback()`+`recordAuditTx`+`tx.Commit()`, entity_type `payment_request` for request rows and `payment` for payment rows):

```go
// ReserveRequest atomically moves an approved, unclaimed, not-on-hold request to
// 'processing' reserved by actor. Returns ErrForbidden when RowsAffected()==0
// (already taken, on hold, or not approved) — the "just taken" case (S5).
func (s *Store) ReserveRequest(ctx context.Context, actor User, id int64) error

// ReleaseRequest returns a 'processing' request to 'approved'. confirmed must be
// true ("no payment initiated" — S7). Permitted for the assignee
// (processing_by==actor) or an authorized caller (authorized==true, i.e. handler
// resolved payment scope 'all'). Clears processing_by/processing_at.
func (s *Store) ReleaseRequest(ctx context.Context, actor User, id int64, confirmed, authorized bool) error

// RecordPaymentForRequest writes the single linked payment for a request the
// actor currently holds in 'processing', then transitions it: settlement
// "settled" → 'completed' (even if in.Amount < approved_amount, S10); "partial"
// → 'partial_review' (partialReason required, S11). The payment row is immutable
// thereafter (S12). One-payment-per-request is enforced by idx_payments_request
// and by the status guard (S9). Nothing is written unless the whole tx commits
// (S13). Returns the new payment id.
func (s *Store) RecordPaymentForRequest(ctx context.Context, actor User, requestID int64, in PaymentInput, settlement, partialReason string, attachment *AttachmentInput) (int64, error)

// AcceptPartial moves a 'partial_review' request to 'completed' (S11, L11).
func (s *Store) AcceptPartial(ctx context.Context, actor User, id int64) error

// RaiseConcern keeps a 'partial_review' request in 'partial_review' and appends a
// conversation comment (S11). comment is required.
func (s *Store) RaiseConcern(ctx context.Context, actor User, id int64, comment string) error

// HoldRequest sets on_hold=1 + hold_reason on an approved, not-already-held
// request (L7). reason required.
func (s *Store) HoldRequest(ctx context.Context, actor User, id int64, reason string) error

// UnholdRequest clears on_hold/hold_reason on a held request (L7). The route gate
// (payment.hold) is what restricts lifting to Accounts.
func (s *Store) UnholdRequest(ctx context.Context, actor User, id int64) error

// LinkablePaymentRequests lists approved · unclaimed (processing_by IS NULL) ·
// not-on-hold requests within the caller's scope (S3), searchable by request
// number / requester / payee / project / head / amount (S4).
func (s *Store) LinkablePaymentRequests(ctx context.Context, opts RequestListOptions) ([]Request, error)

// PaymentForRequest returns the single payment linked to a request, or
// ErrNotFound. Powers the requester's outcome view (Q4).
func (s *Store) PaymentForRequest(ctx context.Context, requestID int64) (Payment, error)
```

### Gating the legacy create path (S15 / X5)
`RecordPaymentForRequest` becomes the **only** user-reachable way to create a payment. `CreatePayment` / `CreatePaymentWithAttachment` remain in the package **solely** as seed/import helpers that mint historical (`request_id IS NULL`) rows; they are removed from every HTTP route. The `POST /payments` handler (`paymentCreate`) refuses any submission that does not carry a valid request reserved by the caller. `UpdatePaymentWithAttachment` and `VoidPayment` refuse when `before.RequestID != nil` (S12), leaving historical rows editable/voidable as before (X6).

## 4. Concurrency & atomicity (the crux — S2, S5, S9, S13)

- **Reservation** is a single conditional statement:
  ```sql
  UPDATE payment_requests
  SET status='processing', processing_by=?, processing_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
  WHERE id=? AND status='approved' AND processing_by IS NULL AND on_hold=0
  ```
  Under SQLite's single-writer locking (`busy_timeout=5000`, `PRAGMA foreign_keys=ON`), each single-statement `UPDATE` is atomic and serialized. The first committer flips `status` to `processing`; every later caller's `WHERE` matches 0 rows. `ReserveRequest` reads `RowsAffected()`; `0` ⇒ `ErrForbidden`. This is provable by racing N goroutines on one request and asserting **exactly one** returns `nil` and the rest `ErrForbidden`, with `processing_by` equal to the winner (S5). The test runs under `-race`.
- **Both entry points reserve through the same method.** The request-detail "Record payment" button and the payment-form request-picker both POST to `/requests/{id}/record-payment`, which calls `ReserveRequest`. There is exactly one reservation code path, so the concurrency guarantee holds for both (S1, S2).
- **Settlement is transactional.** `RecordPaymentForRequest` inserts the payment row and transitions the request inside one `BeginTx`. The request-status re-check in the UPDATE `WHERE status='processing' AND processing_by=?` makes the settlement fail-closed if the reservation was lost. The payment therefore never exists in a "recorded but request not advanced" limbo (S13). The re-record attempt is refused by the status guard, and a duplicate `request_id` is rejected by `idx_payments_request` (S9).

## 5. Enforcement, permissions & entry points

Server-side on the data, not menu-hiding (overview §5, R6). Route gates use the Phase-1 vocabulary via `RequirePermission(resource, action, next)`; scoped list queries pass `auth.Scope(user,"request")` into `RequestListOptions.Scope`.

| Route | Method | Handler | Permission gate | Store call |
|---|---|---|---|---|
| `/requests/{id}/record-payment` | POST | `requestRecordPayment` | `payment`,`process` | `ReserveRequest` → 303 `/payments/new?request={id}` |
| `/payments/new` | GET | `paymentForm` | `payment`,`create` | prefilled form if `?request=` reserved by caller; else request-picker |
| `/payments` | POST | `paymentCreate` | `payment`,`create` | `RecordPaymentForRequest` (refuse if no reserved request → X5) |
| `/requests/{id}/release` | POST | `requestRelease` | `payment`,`process` | `ReleaseRequest(confirmed, authorized)` |
| `/requests/{id}/hold` | POST | `requestHold` | `payment`,`hold` | `HoldRequest` |
| `/requests/{id}/unhold` | POST | `requestUnhold` | `payment`,`hold` | `UnholdRequest` |
| `/requests/{id}/accept-partial` | POST | `requestAcceptPartial` | `approval`,`accept_partial` | `AcceptPartial` |
| `/requests/{id}/raise-concern` | POST | `requestRaiseConcern` | `approval`,`accept_partial` | `RaiseConcern` |
| `/requests/to-pay` | GET | `accountsQueue` | `payment`,`process` | `LinkablePaymentRequests` (+ counts, D4) |

All POST handlers are wrapped with `a.withCSRF`. `requestRelease` resolves `authorized := a.auth.Scope(user,"payment")=="all"` and passes it to `ReleaseRequest`. On a `ReserveRequest` `ErrForbidden`, `requestRecordPayment` reloads the request and renders "Just taken by {name}" (S5) by looking up `Request(ctx,id).ProcessingBy` → `UserByID`.

The `paymentForm` handler, given `?request={id}`, loads the request, confirms `Status=="processing"` and `ProcessingBy==caller`, and **prefills** project/head (locked select), vendor/payee, and amount = `approved_amount` (falling back to `amount`) — S14. With no valid reserved request it renders the request-picker (a `LinkablePaymentRequests` dropdown that POSTs the chosen id to `/requests/{id}/record-payment`) and offers **no** free-standing entry — S3, X5. The Save action opens the settlement popup ("Payment settled" / "Partial settlement"); only the confirmed popup submits `POST /payments` with `settlement` (+ `partial_reason`) — S13.

## 6. UI

- **Request detail** (Phase-2 `request_detail` template, extended here): shows the current lifecycle state; a "Record payment" button when the request is approved·unclaimed·not-on-hold; hold/unhold and release controls (permission-driven); an **Outcome** block (Q4) rendering the linked payment's amount, paid-on date, and — for `partial_review` — the partial reason, from `PaymentForRequest`; and manager **Accept partial** / **Raise concern** controls when `partial_review`.
- **Accounts "To pay" queue** (`/requests/to-pay`): the linkable list with a search box (number/requester/payee/project/head/amount) and a count badge (D4); each row's "Record payment" button posts to the reservation route.
- **Payment form**: request-picker dropdown (no reserved request) or prefilled read-only linkage + settlement popup (reserved request). Mobile-first, existing design system.
- On-hold requests still accept requester comments/attachments via the Phase-2 conversation routes without changing approved fields (Q6) — Phase 3 adds no field-editing path for approved/held requests.

## 7. Consumed contracts

- **Phase 1:** `migration{Version,Name,Up}` slice + `columnExists(tx,table,col)`; `auth.Manager.Can(u,resource,action) bool`, `auth.Manager.Scope(u,resource) string`; `RequirePermission(resource,action,next)`.
- **Phase 2:** table `payment_requests` (columns per overview §3, incl. `status`, `on_hold`, `hold_reason`, `processing_by`, `processing_at`, `approved_amount`, `amount`, `requester_id`, `manager_id`, `project_id`, `head_id`, `number`); `request_comments`; `store.Request` exposing at least `ID, Number, Status, Amount, ApprovedAmount *int64, VendorPayee, Project, Head, RequesterID, RequesterName, ManagerID, OnHold bool, ProcessingBy *int64, ProcessingAt *time.Time`; `Request(ctx,id) (Request,error)`; `ListRequests(ctx,RequestListOptions) ([]Request,error)` honouring `RequestListOptions{Scope,ViewerID,Status,Query}`; `AddRequestComment(ctx,actor,requestID,body) error`.

## 8. Acceptance criteria

- Both entry points atomically reserve; a second concurrent caller is rejected ("just taken") — proven by a `-race` goroutine test.
- Settlement popup routes to `completed` (settled, even when paid < approved) vs `partial_review` (partial, reason required); manager accept → completed / raise-concern → stays partial_review with a comment.
- A payment cannot be created without an approved+reserved request the caller holds (store + HTTP proof-of-absence); a linked payment cannot be edited or voided.
- No refund / return-of-money route or store method exists — a proof-of-absence guard (X3).
- Exactly one payment per request (constraint + status guard). Requester sees the payment outcome on their request. On-hold requests accept comments without field change; only `payment.hold` lifts a hold.
- Historical (`request_id IS NULL`) payments are unchanged after migration and remain editable/voidable.
- `make test-race`, `make test-cover`, and `make test-e2e` green.

## 9. Test plan (TDD)

- **Store units** — new `internal/store/linking_test.go`: reservation atomicity (incl. `-race` concurrency), release confirm/authority, settlement transitions, one-payment-per-request, immutability, accept/raise-concern, hold/unhold + Q6, linkable filtering/search, `PaymentForRequest`. Migration/idempotency/X6 in `internal/store/migrations_test.go` (extended). Each behaviour is red→green→commit.
- **App integration** — `internal/app/app_integration_test.go` additions: both reservation entry points + "just taken"; prefill; request-less create refused (X5); settlement submit → completed/partial; immutable linked payment via HTTP; requester outcome (Q4); hold/unhold/release/accept/raise; To-pay queue counts; no refund route or store method (X3).
- **e2e** — new `tests/e2e/linking-settlement.spec.ts`: button and dropdown entry points, settlement popup → completed vs partial, and a linked payment showing no Edit/Void control.

## 10. Coverage (spec → mechanism)

| ID | Delivered by |
|---|---|
| L7 | `HoldRequest`/`UnholdRequest`; `payment.hold` gate |
| L8 | `ReserveRequest` (approved→processing) |
| L9 | `RecordPaymentForRequest` partial → `partial_review` |
| L10 | `RecordPaymentForRequest` settled → `completed`; `LinkablePaymentRequests` excludes non-approved |
| L11 | Conditional `WHERE`-guarded transitions in every mutation reject illegal moves |
| Q4 | `PaymentForRequest` + request-detail Outcome block |
| Q6 | On-hold accepts comments (P2) with no approved-field change; `HoldRequest` preserves fields |
| S1 | Two entry points both POST `/requests/{id}/record-payment` |
| S2 | `ReserveRequest` atomic on either path |
| S3 | `LinkablePaymentRequests` = approved·unclaimed·not-on-hold |
| S4 | `LinkablePaymentRequests` search over number/requester/payee/project/head/amount |
| S5 | `ReserveRequest` `RowsAffected()==0`→`ErrForbidden`; race test; "just taken by X" |
| S6 | `ReleaseRequest` assignee/authorized; no auto-release |
| S7 | `ReleaseRequest` requires `confirmed` |
| S9 | `idx_payments_request` + processing status guard |
| S10 | Settled → completed with no `amount ≥ approved` check |
| S11 | Partial → partial_review; `AcceptPartial`/`RaiseConcern` |
| S12 | `RequestID != nil` guard in `UpdatePaymentWithAttachment`/`VoidPayment` |
| S13 | Payment insert + request transition in one tx |
| S14 | `paymentForm` prefill from request |
| S15/X5 | Legacy create off all routes; `paymentCreate` requires reserved request |
| X3 | No refund / return-of-money route or store method (proof-of-absence guard) |
| X6 | Guarded, idempotent migration; historical `request_id IS NULL` untouched |
