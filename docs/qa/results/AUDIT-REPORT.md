# Fervid Budget — QA audit report

**Date:** 2026-07-27 · **Commit audited:** `30edd6a` (all 8 build phases complete)
**Method:** 16 agents. Nine modelled the shipped system from source; seven wrote
and executed browser test suites against isolated servers on ports 4301–4307.

**Instruction governing this pass:** report defects, do not change application
code. Nothing under `internal/`, `cmd/` or `web/` was modified. Every defect
below is recorded as an honest, still-failing assertion annotated `test.fail()`
(deterministic) or `test.fixme()` (flaky or blocked), so the suite is green while
the defect stays pinned — and the day one is fixed, its test goes red with
"passed unexpectedly".

---

## The short version

The **permission matrix is fundamentally sound.** All 475 route × caller cells
matched the outcome derived from the seeded role definitions before the run.
Anonymous callers are redirected, never answered. Every state-changing POST but
one refuses a missing, forged **and** cross-session CSRF token. Ownership rules
layered above the route gate hold even against an administrator holding all 66
grants — no self-approval, no deciding another manager's request, no accepting a
partial settlement you do not manage. A permission change takes effect on the
very next request. The reservation lock is genuinely atomic across five races.
The money trail is exact at 18 consecutive hops, with one `₹` at each.

Against that, the audit found **five critical defects**, and the most severe is
not a security hole but a feature that cannot work at all:

1. **No recoverable payment can ever be recorded, in any of the six categories.**
2. **Any signed-in user can download every attachment in the system.**
3. **One download route serves two tables, so clicking Download on your own
   invoice can hand you a stranger's bank advice.**
4. **The recoverables register and its CSV apply no data scope.**
5. **Retiring a project makes its heads' own Save button move them to a
   different project.**

The recurring theme is narrow: **`payments.head_id`, attachment ownership, and
a family of read paths that were never given the scope check their siblings
have.** Where the codebase reasoned explicitly about a boundary it got it right;
the failures are all places where a check was never written rather than written
wrongly.

---

## What was executed

| Area | Spec | Tests | Result | Findings |
|---|---|---|---|---|
| A · RBAC & permission enforcement | `audit-a-rbac-permissions.spec.ts` | 124 | 124 passed, 0 failed | 11 |
| B · Request lifecycle & validation | `audit-b-request-lifecycle.spec.ts` | 99 | 98 passed, 1 skipped, 0 failed | 18 |
| C · Approvals & cancellation | `audit-c-approvals-cancellation.spec.ts` | 94 | 94 passed, 0 failed | 8 |
| D · Linking, reservation & settlement | `audit-d-linking-settlement.spec.ts` | 77 | 77 passed, 0 failed | 14 |
| E · Recoverables | `audit-e-recoverables.spec.ts` | 52 | 50 passed, 2 skipped, 0 failed | 9 |
| F · Notifications & reminders | `audit-f-notifications.spec.ts` | 58 | 58 passed, 0 failed | 8 |
| G · Information-flow integrity | `audit-g-information-flow.spec.ts` | 56 | 56 passed, 0 failed | 34 |
| H · The UI at data volume | `audit-h-scale.spec.ts` | 3 | 3 passed, 0 failed | 1 |
| — · Harness self-test | `audit-smoke.spec.ts` | 4 | 4 passed, 0 failed | — |

**564 new test cases** on top of the 163 already shipping. Roughly **37 are
annotated** as pinning a confirmed defect; the rest genuinely pass.

**Each area runs against its own server and its own database.** That is not
tidiness — it is a correctness requirement discovered the hard way. The first
attempt to run everything through one server broke the *shipped* suite:
`ux.spec.ts` failed with `/roles: scrolls sideways by 806px`, because the audit
had created a few custom roles and the roles matrix grows a column per role. The
audit builds a world in order to interrogate it — custom roles, dozens of users,
214 requests for the pagination case — so areas contaminate each other's counts
and contaminate the shipped specs' assumptions.

`playwright.config.ts` therefore excludes `audit-*.spec.ts` and `make test-e2e`
is unchanged at its documented **163 passed / 49 skipped / 0 failed**. The audit
runs separately via `make test-audit` (`scripts/run-audit.sh`), one port and one
database per area. Area H exists because of that accident, and it turned the
accident into a finding.

Model documents: `docs/qa/uml/01`–`06` and `docs/qa/use-cases/UC-A`/`UC-B`/`UC-C`
(133 use cases). Test cases: `docs/qa/test-cases/TC-A`–`TC-G`. Per-area findings:
`findings-a`…`findings-g` in this directory.

---

## Critical

### C1 · No recoverable payment can ever be recorded, in any category
`F-E-01` · `F-D-11` · `F-G-001` — three agents, independently.

The recoverable fieldset never collects a **head**. `<fieldset
data-when="treatment:recoverable">` (`internal/app/templates.go:1731`) offers the
category, a project for `emd|pbg` only, counterparty, expected return and terms;
`id="head" name="head_id"` lives exclusively inside `<fieldset
data-when="treatment:budget">`. So `payment_requests.head_id` is always NULL for
a recoverable. `paymentEntry` then emits a hidden `head_id=0`
(`internal/app/linking.go:242`) with no selector on the screen, `payments.head_id`
is `NOT NULL` (`internal/store/schema.go:60`), and `validatePayment` refuses
`HeadID == 0` (`internal/store/store.go:1621`).

Every settlement answers **400 `validation failed: valid head, date, and positive
amount are required`**, for every amount, date and mode. The request is left in
`processing`, still holding the accountant's reservation, and the register shows
it awaiting payment for ever. Verified live for all six categories:
`{"emd":400,"pbg":400,"icd":400,"security_deposit":400,"employee_advance":400,"other":400}`.

**Scope correction.** Area D scoped this to the four categories requiring no
project. The blocker is the head, not the project, so EMD and PBG fail
identically — confirmed by reading the fieldset structure directly. **All six.**

Phase 4's whole deliverable — the recoverables register — can therefore never
contain a paid row produced by the product. Requirements V2, V3 and V7 are
untestable through the UI, which is why the E suite marks two cases `test.fixme`
and cites the Go tests instead. **No Go test catches this** because every one
seeds the payment with a hand-supplied `head_id`, the one shape the real form
cannot produce (`internal/store/recoverables_test.go:802`).

### C2 · `GET /attachments/{id}` performs no ownership check
`F-A-01` · `F-B-11` — verified independently by me from source.

`attachmentDownload` (`internal/app/app.go:881`) resolves the id, checks the path
is inside the attachment directory, checks the file exists, and streams it. It
never asks which payment the attachment belongs to or whether the caller's data
scope reaches it. The only gate is `attachment:view` — which the seeded
**Requester** role holds (`internal/store/migrations.go:363`).

Live: a Requester refused the request (403) and the payment (403) received
**200 and the file bytes** for the bank advice attached to that payment. Ids are
small sequential integers, so the whole table is enumerable by walking 1, 2, 3.

The codebase closes this exact hole elsewhere and says so: `paymentDetail`
re-checks the *request's* scope with the comment *"or `payment:view` becomes a way
around Q5/R6"* (`internal/app/app.go:757`). The attachment route is the hole that
comment was written about.

### C3 · One download route serves two tables
`F-A-05` · `F-B-09` — found by probing, not predicted by any model document.

`AttachmentByID` selects `FROM payment_attachments` (`internal/store/store.go:1353`).
Templates render **request** attachments — `RequestAtts`, from
`request_attachments` — through the same `/attachments/{id}` URL at
`internal/app/templates.go:297`, `:307` and `:2393`. The two tables have
independent autoincrement sequences and both start at 1, so the collision is the
default case, not an edge case.

Live: clicking Download on a Requester's own `invoice-….txt` returned the
accountant's `bank-advice-….txt`. The server log line is byte-identical to the
direct probe of the payment attachment — the same file, reached two ways.

Two defects in one: every request document is undownloadable, **and** the
product hands ordinary non-adversarial users someone else's payment proof under
their own filename. That is why C2 is rated critical rather than high — the leak
arrives by a click, not only a typed URL.

### C4 · The recoverables register and its CSV apply no data scope
`F-G-016` · `F-E-03`

`recoverablesList`, `exportRecoverable` and `recoverableDetail`
(`internal/app/recoverables.go:74`, `:97`, `:130`) never call `Scope` and never
apply a per-row ownership test. Every live recoverable — category, counterparty,
project, amount, requester, repayment-note text — goes to any holder of
`recoverable_report:view`/`:export`. The CSV additionally exports **Status,
Requester and Repayment Notes**, three columns its own screen does not show.

Latent under the seeded roles, because both holders (Accounts, Admin) also carry
`request` scope `all`. It activates the moment an administrator builds a custom
role pairing `recoverable_report:view` with a narrower request scope — which the
Roles screen permits today with nothing to stop it. Every comparable screen in
the codebase scopes; this is the one place money moves with no equivalent check.

### C5 · Retiring a project silently reassigns its heads
`F-G-033`

`/heads` loads **active** projects only for the row selects while listing **all**
heads. A head belonging to a retired project therefore has a `<select>` with no
matching option, the browser selects the first one, and pressing that row's own
**Save** moves the head to a different project. Silent data corruption from a
button whose only apparent job is to save what is on screen.

---

## High

| ID | Finding |
|---|---|
| `F-D-01` | `POST /payments` writes the client's `head_id`, `vendor_payee` and `invoice_no` without comparing them to the request. A forged head charges an approved payment to a head nobody approved, and the grid, the report and the audit `after` blob all follow the tampered value while the request screen still shows the approved one. |
| `F-A-03` | `POST /payments/{id}/attachments` also checks the grant and not the actor. A Requester plants a file on any payment — including one the screen calls read-only and which refuses an edit from its own accountant — and the trail records it as ordinary business under the intruder's name. |
| `F-A-02` · `F-G-032` | `GET /grid` is `RequireLogin` only. `grid:view` exists, is seeded to three roles and gates nothing. Any signed-in user — including one with **no role at all** — reads every budget and actual plus a Recent Payments table with amounts, payees and live payment links, while `/payments` answers them 403. Its CSV *is* gated on `grid:export`. |
| `F-D-10` | The settlement **write** is gated more weakly than its **pure preview**: preview needs `payment:settle`, the writer needs only `payment:create`, and `payment:mark_partial` gates nothing anywhere. A purpose-built role settled a request in full and marked another partial while being refused 403 on the preview. |
| `F-C-01` | An approver may approve **more** than was requested, and that larger figure becomes the payment ceiling — ₹25,000 approved on a ₹18,400 request, then paid in full and closed as Completed. |
| `F-C-03` | `POST /approve` on a request in `cancellation_requested` unfreezes it: amount rewritten, `approved_at` re-stamped, no decline record, and the stale `cancel_reason` retained but rendered nowhere — so the override is invisible on screen. |
| `F-C-07` | A **declined** cancellation destroys Accounts' hold. `on_hold` is cleared on the ask and restored by nothing, so the request returns to `approved` genuinely re-reservable with the hold question unanswered — and **no event fires on either cancellation decision**, so nobody is told. Breaks requirement L7, "On hold (only Accounts lifts)". |
| `F-B-10` · `F-C-05` | Two simultaneous submits, or two simultaneous decisions, give the loser **500 `SQLITE_BUSY`** in ~4 ms and lose their form. `CreateRequest` and every decision writer read before they write inside a deferred transaction, so `busy_timeout` cannot apply. Integrity holds — no double-write. Contrast `ReserveRequest`, which writes first and is correct across five races. |
| `F-B-01` | `amount=1e300`, `9e18` and `1e19` are silently clamped to int64 max and created as **₹92,23,37,20,36,85,47,758.07**. `ParsePaise`'s non-positive guard never fires because the float conversion saturates first. |
| `F-B-16` | With 214 requests the **All** tab reads 214, the list renders 200, and the CSV export carries 200 rows — no warning, no pagination anywhere. |
| `F-E-02` | An admin-added recoverable category can never be selected: `#rcategory` is six hardcoded options, unconnected to the table. A deactivated seed category is still offered. Enforcement ships; the affordance V4 promises does not. Data integrity is *not* compromised — the fallback substitution is only on the htmx fragment path. |
| `F-G-028` | An unbudgeted month cannot be saved at all: the screen renders `₹0.00` for unbudgeted heads, `ParsePaise` rejects zero, and `budgetSave` refuses the whole batch. An existing budget can never be reduced to zero either. |
| `F-F-01` | All twelve per-event sheets on `/admin/notifications` are missing `hidden`, unlike every other overlay in the app, so all twelve render full-viewport at once and only the last is clickable. |
| `F-F-05` | The notification centre is unreachable on desktop: the only link anywhere is the `.m-topbar` bell, `display:none` above 860 px, and `navSpec` has no entry. The route works if typed. |
| `F-F-06` | Six actions fire **no notification at all** — withdraw, re-raise, release, unhold, accept-partial and either cancellation decision. The approver of a re-raised request is never told it exists. |
| `F-D-12` | The reservation screen states *"The requester and the approver are both notified"*; release, reassign and unhold fire nothing. Both counts moved by zero. |
| `F-G-017` | `/audit` discloses the full `Request` struct to any holder of `audit:view`. |
| `F-G-018` | The dashboard's accounts area hardwires `Scope:"all"`. |
| `F-G-034` · `F-G-035` · `F-C-04` | `POST /users` spans three transactions and `POST /requests/{id}/edit` spans three more, with no envelope: a refused save still commits the rename, and a refused resubmit still writes the edit **and audits it as done**. |
| `F-G-022` · `F-G-024` · `F-G-025` | An assigned role is deleted with no pre-check; deactivating a head strands its approved requests; deactivating an approver strands their approvals — with no reassignment route to recover (see below). |
| `F-A-06` · `F-C-02` | **Coverage requirement A7 is not shipped.** `store.ReassignRequest` is complete and unit-tested but has no HTTP caller; `approval:reassign` is seeded to Manager and Admin and gates nothing; `POST /requests/{id}/reassign` is the *reservation* handler. The coverage matrix marks A7 verified, citing a store-level test that never touches HTTP. Nine granted verbs in total are enforced nowhere. |
| `F-A-04` · `F-G-003` | The `payment` data scope is declared in `scopedResources`, drawn on the roles screen, saved, round-tripped — and never read. A role set to `payment=own` receives the whole ledger. |
| `F-B-06` · `F-G-027` | A forged `vendor_id`, `manager_id`, `project_id` or `head_id` answers **500** and loses the typed form: `classify` maps only UNIQUE violations, so `FOREIGN KEY constraint failed (787)` reaches the error page. |
| `F-G-029` | The variance grid's own filter form posts to `/`, which is now the dashboard, so month/status/search are unreachable from the grid. Same root cause as `lockMonth`/`unlockMonth` redirecting to `/?month=` with no confirmation, the budgets "View grid" link, the months "Grid" action and payment-edit's "Back to grid". |
| `F-H-01` | **The roles matrix scrolls the page sideways once a handful of roles exist** — by 806 px at 1440 px, and by 3024 px at 390 px. It violates the project's own `ux.spec.ts` gate, which has been passing for eight phases only because the test database never held more than the four seeded roles. Every other wide table in the design system sits in a `.table-wrap` scroll container; this one does not, so the sidebar and tab bar slide away with the content. The screen through which all permission administration happens degrades exactly as the permission model gets used. |

---

## Medium and below

Recorded in full in the per-area findings documents. The themes:

- **Wrong message for the right refusal.** Every *state* conflict in the
  settlement flow is reported as something it is not (`F-D-02`): reserving an
  on-hold request says *"Someone else took this request before you… reserved by
  Someone else"* on a request nobody holds; holding a reserved request answers
  *"You do not have permission"* to somebody who has it; settling without a
  reservation says *"Something went wrong"* because `friendly()` has no
  `ErrForbidden` branch (`F-A-09`). The picker gets the first case right in the
  same codebase, so the information exists and only this path discards it.
- **Searches that cannot match what is on screen.** The queue and picker search
  the amount as a **paise integer** (`F-D-04`), so `7,431.00` never finds
  ₹7,431.00 while `743100` does. The Accounts queue has **no search control at
  all above 860 px** (`F-D-07`) — it ships only the mobile filter row, where every
  other list in the app ships both.
- **Screens that disagree about the same number** (`F-G-007`, `F-G-006`,
  `F-G-009`, `F-E-05`): two screens label a figure "Approved, unclaimed" and show
  different numbers; "My open requests" omits `returned`; vendor "Paid this year"
  is zeroed by a rename because it matches by payee *name*; the recoverables
  dashboard counts unpaid money as overdue while its own register correctly says
  "Awaiting payment".
- **Validation in the wrong layer.** A 12-character letters-only password answers
  **500** *"The password could not be secured"* on both create and reset
  (`F-A-11`, `F-G-036`), because the app checks length only and `auth.HashPassword`
  then demands a digit. A payment may be dated arbitrarily far into the future
  (`F-D-06`). An inactive head, an inactive vendor, and a head from another
  project are all accepted when named in the POST (`F-B-03`, `F-B-07`, `F-B-05`).
- **Recoverable-only columns on budget requests.** `counterparty`,
  `expected_return_date` and `repayment_notes` are stored *and rendered* on a
  Budget expense (`F-B-15`).
- **Enumeration and disclosure edges.** 403-vs-404 lets a caller learn which
  request ids exist (`F-G-002`); CSV amount cells carry `₹` and Indian grouping so
  `Number()` yields `NaN` (`F-G-030`); `/reports/ytd.csv` always exports head-level
  rows whatever tab it was pressed from (`F-G-031`).
- **Dead vocabulary and dead settings.** `recoverable_category:delete` has no
  route or store method (`F-E-06`); `payment_modes` and `allow_direct_payments` are
  admin-editable settings nothing reads; two nav badges are declared and never
  populated (`F-G-013`); `calendarDaysBetween` is documented, unit-tested and has
  no production caller, so "calendar days" in requirements N4/S8 is really
  elapsed-day arithmetic (`F-F-08`); two different stale-reservation clocks
  disagree (`F-F-02`, `F-D-…`).
- **The vestigial Casbin dependency.** `go.mod` still requires
  `github.com/casbin/casbin/v2`; no file imports it.

---

## Documentation that is now wrong

The audit corrected several claims in the project's own records. These matter
because a future session reads them as fact:

| Claim | Reality |
|---|---|
| `PROGRESS.md` — `.metric-foot`, `.btn.approve`, `.metric.warn`, `.metric.good` have no CSS rule | All four have rules (`fervid-ds.css:367/378/387/397`), added by commit `a80e18f`, which the same file records a hundred lines earlier |
| `PROGRESS.md` — `unbuiltPrefixes` holds one line | It is empty (`nav.go:250`) |
| `PROGRESS.md` — production will run v1→v5 on first deploy | The chain runs through **v7** |
| `fixtures.ts:178` — "KNOWN GAP", the payee is blank throughout the settlement flow | **Stale.** Fixed at `fca6939`; verified at all five hops. Three shipped specs still steer around a defect that no longer exists, costing coverage |
| Overview spec — 17 resources / 54 pairs | 21 resources / 66 pairs |
| Overview spec — nine request statuses including `draft` | **Eleven** live statuses; `draft` is forbidden by `CHECK (status <> 'draft')`. The spec omits `cancellation_requested`, `cancelled` and `completed_partial`, and the entire cancellation sub-machine |
| Overview spec — `partial_review → completed` | No writer performs it; `AcceptPartial` writes `completed_partial`. A dead edge in the transition table |
| Coverage matrix — "92 / 92 VERIFIED" | Requirement **A7 has no HTTP surface**; **L1 (Draft)** is unreachable by construction; and roughly six of the cited test names do not exist in the repo. The requirement IDs are sound; the verification column is not |

---

## What the audit confirmed is correct

A findings list read alone gives a false picture. Each of these was a plausible
failure the audit set out to find and did not:

- All **475** route × caller cells matched prediction — nothing more permissive,
  nothing less.
- Anonymous callers get **303 → /login** on every gated path, never a status leak.
- CSRF refuses missing, forged **and** cross-session tokens across 12 resource
  families. `POST /login` is the sole exemption (`F-A-10`, informational).
- No route is gated on a pair outside the canonical vocabulary; a hand-crafted
  `perm` value cannot invent a grant.
- **Ownership beats grants.** An administrator holding all 66 pairs is still
  refused editing someone's request, deciding a cancellation they were not sent,
  and accepting a partial settlement they do not manage. G8 holds at both layers.
- The **reservation lock is atomic** across five races (one UI, four concurrent
  HTTP): exactly one 303, one 409, one audit row. Nothing expires a reservation.
- The settlement **preview writes nothing**, confirmed before, after and on
  abandonment.
- `on_hold=1` implies `approved` is not violable from the settlement flow.
- **One payment per request** holds; a double confirm is neither an error nor a
  second row.
- **No refund, write-off or repayment-tracking path exists** — six refund-shaped
  paths answer 405 on POST and 404 on GET.
- The **money trail is exact at 18 hops** for a lakh value, a paise value and the
  adjusted case, with exactly one `₹` at each; the grid follows **paid** and the
  request keeps **approved**.
- **No refused mutation left an audit row**, and Before/After are genuine
  pre/post-images.
- `/requests/export.csv` **does** apply data scope, and `?scope=all` cannot widen it.
- The **390 px journey** still works end to end on both device projects with no
  horizontal overflow and no console error.

---

## Suggested order of repair

Grouped by root cause rather than by severity, because several findings share one:

1. **`payments.head_id`** — C1. Either let it be null for a recoverable payment
   (already excluded from actuals) or give the entry screen a head selector when
   the request has none. One change; unblocks all of Phase 4.
2. **Attachment ownership and routing** — C2 + C3 + `F-A-03` + `F-D-08`. Give
   request documents their own scoped route and apply `canViewRequest` to both the
   read and the write path. One boundary, four findings.
3. **The unscoped read family** — C4 + `F-G-017` + `F-G-018` + `F-05` of the route
   matrix. Decide whether `recoverable_report` and `audit` are deliberately
   company-wide and say so where the resource is declared, or scope them like
   their siblings.
4. **Trust the request, not the form** — `F-D-01`. Derive head, payee and invoice
   inside `RecordPaymentForRequest`, which already has the row open.
5. **Read-then-write transactions** — `F-B-10` + `F-C-05` + `F-G-034` + `F-G-035` +
   `F-C-04`. Single-statement conditional updates where possible, one envelope
   where not.
6. **Active-only selects on screens listing everything** — C5 + `F-B-05` +
   `F-G-024`. The pattern that corrupts data in C5 is the same one that makes an
   edit unsaveable elsewhere.
7. **Say why, not "something went wrong"** — `F-D-02` + `F-A-09` + `F-B-02` +
   `F-E-08`. Give `friendly()` an `ErrForbidden` branch and let
   `ReserveRequest` name its three causes.
8. **Then the documentation table above**, which is cheap and prevents a future
   session chasing fixed bugs.
