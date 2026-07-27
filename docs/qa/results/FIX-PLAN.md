# Fix plan — repairing the 2026-07-27 audit findings

Companion to `AUDIT-REPORT.md`. Grouped by **file ownership**, because
`internal/app/app.go`, `internal/app/templates.go` and `internal/store/store.go`
are each touched by many findings and two agents editing one of them at the same
time is the fastest way to lose work. A wave's agents own disjoint files; waves
run in order.

**The tests already exist.** Every confirmed defect is pinned by a still-failing
assertion annotated `test.fail()` in `tests/e2e/audit-*.spec.ts`. So a fix is
done when its annotation is **removed** and the test passes on its own port:

```sh
FERVID_E2E_PORT=43NN npx playwright test -c playwright.audit.config.ts \
  tests/e2e/audit-X-....spec.ts --project=chromium --reporter=line
```

Never delete or weaken an assertion. If a test needs changing because the
*expectation* was wrong rather than the code, say so explicitly in the report.

**Gate for every wave:** `go build ./... && go vet ./... && go test -count=1 ./...`,
`npm run typecheck`, `make test-e2e` (must stay 163/49/0), and the owning area's
audit spec.

---

## Decision taken by the user

**The recoverable-payment defect is fixed in the data model.** Migration **v8**
rebuilds `payments` so `head_id` is nullable, and `validatePayment` requires a
head only for a budget-treatment payment. The grid and the monthly report already
exclude recoverables *by treatment* (`internal/store/store.go:1388`,`:1398`), so a
recoverable payment's head was never meaningful. Care needed: `payment_attachments`
references `payments`, so the rebuild must not orphan it, and `PRAGMA foreign_keys`
rides on the DSN and cannot be toggled inside the migration's transaction.

---

## Wave 1 — store foundations and one CSS fix

### 1a · owns `internal/store/migrations.go`, `internal/store/store.go`, `internal/money/money.go`

| Finding | Fix |
|---|---|
| F-D-11 · F-E-01 · F-G-001 **critical** | Migration **v8**: rebuild `payments` with `head_id INTEGER REFERENCES heads(id)` (nullable). `validatePayment` requires a head only when the payment is not settling a recoverable request. |
| F-D-01 **high** | `RecordPaymentForRequest` derives `head_id`, `vendor_payee` and `invoice_no` from the request row it already has open, ignoring whatever the form sent. |
| F-B-01 **high** | `money.ParsePaise` refuses a value that overflows int64 instead of saturating to max. Guard before the `int64` conversion. |
| F-B-06 · F-G-027 medium | `classify` maps a FOREIGN KEY violation to `ErrValidation`, so a forged id is a 400 and not a 500. |
| F-D-02 (part) medium | `ReserveRequest` distinguishes its three refusal causes — already taken, on hold, not approved — with distinct sentinel errors so the handler can name the real one. |
| F-A-04 · F-G-003 medium | `ListPayments` takes a scope + viewer and filters on `entered_by`, mirroring `requestWhere`. |
| F-D-06 low | `validatePayment` refuses a `paid_on` after today. |
| F-D-08 low | `AddAttachment` refuses a payment whose `request_id` is set, matching edit and void. |
| F-A-01 · F-A-05 (store half) | Add `RequestAttachmentByID` so request documents can be resolved from their own table, and a helper that resolves a payment attachment to its payment and request for the ownership check. |

### 1b · owns `web/static/fervid-ds.css`

| Finding | Fix |
|---|---|
| F-H-01 **high** | `/roles` overflows because the `.segmented` role strip grows one link per role and pushes the page wide. Contain it — scroll or wrap within its own box — so the page never scrolls sideways at 1440 px or 390 px. Every other wide surface uses `.table-wrap { overflow: auto }`; match that intent. |

---

## Wave 2 — request/recoverable store logic, and the notification vocabulary

### 2a · owns `internal/store/requests.go`, `internal/store/recoverables.go`

| Finding | Fix |
|---|---|
| F-C-01 **high** | `ApproveRequest` refuses an approved amount greater than the requested amount. |
| F-C-03 **high** | Approving is not legal from `cancellation_requested`; the cancellation must be decided first. |
| F-C-07 **high** | A cancellation ask preserves `on_hold`/`hold_reason`, and a declined cancellation restores them, so "only Accounts lifts" holds. |
| F-B-10 · F-C-05 **high** | Remove the read-then-write races: single conditional `UPDATE … WHERE <expected state>` and check `RowsAffected`, the shape `ReserveRequest` already uses correctly. Covers create, approve, return, reject. |
| F-B-16 **high** | The list's 200-row cap becomes explicit: paginate, or return a total plus a truncation flag the screen can show. Silence is the defect. |
| F-A-08 medium | Refuse a `manager_id` that does not hold `approval:approve`, using the query `ListApprovers` already runs. Same on update. |
| F-B-05 medium | Refuse a `head_id` that does not belong to the named `project_id`. |
| F-B-03 · F-B-07 medium | Refuse an inactive head and an inactive vendor named directly in the POST. |
| F-B-15 · F-B-04 low | Clear the columns a treatment/type does not own, so a budget expense cannot carry counterparty/return/repayment and a reimbursement cannot carry invoice fields. |
| F-E-05 medium | `RecoverableMetrics` adds the `paid <> ''` guard its own `recoverableAgeing` applies, so unpaid money is not counted overdue. |
| F-E-02 · F-B-17 **high** (store half) | Expose active categories for the request form to render, so an admin-added category is selectable and a deactivated one is not offered. |
| F-B-18 low | Order the list the way the screen says — longest-waiting first — or change the screen. Pick one and make them agree. |
| F-D-04 medium | Normalise the amount needle like `ParsePaise` (strip `₹`, commas, spaces) and match on paise, so `7,431.00` finds ₹7,431.00. |

### 2b · owns `internal/notify/` (all files)

| Finding | Fix |
|---|---|
| F-F-06 **high** (vocabulary half) | Declare the missing events so handlers have something to fire: withdrawn, re-raised, released, reassigned, unheld, partial accepted, partial concern raised, cancellation decided. Seed their `notification_settings` rows in a migration — remember `seedSystemRoles` runs only in v1, so a new row needs its own migration with `INSERT OR IGNORE`. |
| F-F-08 informational | `calendarDaysBetween` is dead. Either use it in the reminder queries so "calendar days" is true, or delete it and make the copy say elapsed days. Decide and be consistent with F-F-03. |
| F-F-02 medium | `MarkReminderSent` writes an audit row, so "a reminder went out" has a witness. |

---

## Wave 3 — the app-layer hot file (sequential, sole owner)

### 3 · owns `internal/app/app.go`, `internal/store/permissions.go`, `internal/app/permmap.go`

| Finding | Fix |
|---|---|
| F-A-01 · F-B-11 **critical** | `attachmentDownload` resolves the attachment's payment, then its request, and applies the same scope check `paymentDetail` runs. Answer **404**, not 403, so the route is not an enumeration oracle. |
| F-A-05 · F-B-09 **critical** | Request documents get their own route, `GET /requests/{id}/attachments/{attachmentID}`, reading `request_attachments` and scoped by the request. **This URL is the contract with Wave 4a**, which updates the three template hrefs. |
| F-A-03 **high** | `attachmentUpload` applies the same ownership check on the write path. |
| F-A-02 · F-G-032 **high** | Gate `GET /grid` on `grid:view`. Gate the Recent Payments panel on `payment:view` separately — that is the part that leaks beyond budgets. |
| F-D-10 **high** | Gate `POST /payments` on `payment:settle` as well as `payment:create`, and require `payment:mark_partial` when `settlement=partial`. |
| F-G-028 **high** | `budgetSave` accepts zero and does not refuse the whole batch for one unparseable field, so a fresh month is saveable. |
| F-G-033 **critical** | The heads screen offers **all** projects in its row selects, not only active ones, so Save cannot silently move a head. |
| F-G-034 **high** | `POST /users` runs its three writes in one envelope. |
| F-G-022 · F-G-023 **high**/medium | Deleting a role checks its holders first, and a refused system-role delete says why instead of "you do not have permission". |
| F-G-017 **high** | `/audit` stops disclosing the full request text to any `audit:view` holder. |
| F-A-06 · F-C-02 medium | Build the approver-reassignment route on the existing, tested `store.ReassignRequest`, gated on `approval:reassign`. That is coverage requirement **A7**, and it is also the recovery path F-A-08 and F-G-025 need. |
| F-A-11 · F-G-036 medium | `validatePassword` delegates to `auth.ValidatePassword` and adds its 12-character minimum, so every rejection is a 400 naming the rule. |
| F-A-07 low | `POST /users` requires `user:create` when creating and `user:edit` when editing, so the button and the route agree. |
| F-A-09 · F-D-02 (part) low | `friendly` gains an `ErrForbidden` branch so a state conflict carries its own sentence. |
| F-G-026 medium | A locked-month refusal answers 409 everywhere, not 400 on two screens. |
| F-G-004 · F-C-06 medium | The audit screen's Entity and Action filters cover the request workflow. |
| F-A-10 informational | `POST /login` gets the same body cap every other POST has. |
| F-G-002 medium | A request the caller may not see answers the same status whether or not it exists. |

---

## Wave 4 — templates, and the remaining handlers

### 4a · owns `internal/app/templates.go`, `internal/app/nav.go`

| Finding | Fix |
|---|---|
| F-E-02 · F-B-17 **high** | Render the recoverable category `<select>` from the active categories (Wave 2a supplies them) instead of six hardcoded options. |
| F-A-05 (template half) | Point the three request-document links at `GET /requests/{id}/attachments/{attachmentID}`. |
| F-F-01 **critical** | Add `hidden` to the twelve per-event sheets and the SMTP form on `/admin/notifications`, matching every other overlay. |
| F-F-05 **high** | Give the notification centre a desktop entry point: a `navSpec` item and/or a dashboard control, so it is not reachable only through a bell that `display:none`s above 860 px. |
| F-D-07 medium | Give the Accounts queue the desktop `.toolbar` search twin the other four lists have. |
| F-G-029 **high** | The grid's filter form posts to `/grid`, not `/`. Same for the budgets "View grid" link, the months "Grid" action and payment-edit's "Back to grid". |
| F-B-13 medium | The edit screen's vendor combobox input gets `name="q"` so htmx sends a query. |
| F-D-05 informational | `.opt` gets `aria-hidden` so a field's accessible name is not "Processing note optional". |
| F-E-07 informational | The Configuration hint names the payee, not the counterparty. |
| F-F-03 medium | The "three calendar days" copy reads the configured threshold instead of hardcoding it, in all four places. |

### 4b · owns `internal/app/linking.go`, `internal/app/requests.go`, `internal/app/recoverables.go`, `internal/app/dashboard.go`, `internal/app/configuration.go`, `internal/app/notifications.go`

| Finding | Fix |
|---|---|
| F-G-016 · F-E-03 **critical** | `recoverablesList`, `exportRecoverable` and `recoverableDetail` apply the request data scope. |
| F-F-06 **high** (wiring half) | Fire the events Wave 2b declared, from withdraw, re-raise, release, reassign, unhold, accept-partial and both cancellation decisions. |
| F-D-12 medium | Release, reassign and unhold notify, so the reservation screen's promise is true. |
| F-F-07 medium | "Save corrections" without resubmitting does not tell the manager it was re-sent. |
| F-G-018 **high** | The dashboard's accounts area uses the caller's scope instead of hardwiring `all`. |
| F-G-035 · F-C-04 **high**/medium | `POST /requests/{id}/edit` commits the edit and the submit together, or neither. |
| F-D-03 medium | The stale nudge only offers "Put it on hold" to a reader who can act on that reservation. |
| F-E-04 low | The recoverables CSV and its screen carry the same columns. |
| F-G-006 · F-G-007 · F-G-008 medium | The dashboard tiles and the queue badges agree with the lists they link to. |
| F-G-014 medium | `/requests` honours `?status=`, as its CSV already does. |
| F-G-019 · F-G-020 · F-G-021 medium | Locking redirects somewhere that confirms it; `/payments/new` shows the lock before the form is filled; a request cannot be approved into a locked month. |
| F-E-08 · F-B-02 medium | A refused `recoverable`-type submission reports the real reason and keeps the form. |
| F-G-012 low | The recoverables by-category drill-through reproduces its own count. |

---

## Wave 5 — the tail, the documents, and full verification

| Finding | Fix |
|---|---|
| F-G-030 medium | CSV amount columns are numbers, not `₹`-formatted strings. |
| F-G-031 medium | `/reports/ytd.csv` exports the level of the tab it was pressed from. |
| F-G-013 medium | Either populate the two declared nav badges or stop declaring them. |
| F-G-009 · F-G-010 low | Vendor "Paid this year" and "Open requests" — both need a join the schema does not have (`payments.vendor_id`). Decide: add the column in a migration, or make the screens honest about what they count. |
| F-B-08 informational | Validation messages lose the `validation failed: ` sentinel. |
| F-C-08 · F-D-14 low | Reconcile the status enum and the request-type list with what ships. |
| F-E-06 low | `recoverable_category:delete` — consume it or remove it from the vocabulary, and update the count test deliberately if removing. |
| F-D-09 · F-D-13 · F-E-09 · F-F-04 | Correct the stale records: `fixtures.ts`'s KNOWN GAP, `PROGRESS.md`'s four CSS classes / `unbuiltPrefixes` / v5-vs-v7, and the coverage matrix's "92 / 92 VERIFIED" where A7 and L1 do not hold. |
| — | Remove every `test.fail()` whose defect is fixed; run both suites and the Go suite; update `AUDIT-REPORT.md` with the outcome per finding. |

---

## Deliberately not fixed

Record here anything a wave decides to leave, with the reason, so the audit
report can say so plainly rather than the finding quietly disappearing.
