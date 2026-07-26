# Fervid Budget — build progress

**Last updated:** 2026-07-26
**Branch:** `main` (all work committed here; nothing pushed)

Resume by reading this file, then the phase plan named under "Next up".

---

## What this project is doing

Adopting the approved mockup design system (`mockups/`, 43 screens) across the
whole application and building the payment-requests module on top of it, in
phases. The design system, specs and every phase plan are committed.

**Source of truth, in order:**

1. `docs/superpowers/specs/2026-07-25-design-system-adoption-spec.md` — the 22
   gaps (G1–G22) and 10 decisions (D1–D10) that govern every phase.
2. `docs/superpowers/plans/2026-07-25-phase-*.md` — one plan per phase, each
   with a Status block per task recording its commit.
3. `mockups/screens/*.html` — the approved design. Screens must match these.

---

## Phase status

| Phase | Scope | Status |
|---|---|---|
| 0 | Design system + app shell | **Done** (20/20) |
| 1 | RBAC foundation | **Done** (13/13) |
| 1V | Vendor master | **Done** (8/8) |
| 2 | Request workflow + Configuration | **Done** (33/33) |
| 3 | Linking + settlement | Store done (1–10). Screens 11–19 and e2e 20–21 outstanding |
| 4 | Recoverables | Not started (13 tasks) |
| 5 | Notifications | Not started (12 tasks) |
| 6 | Redraw the 11 original screens | Not started (9 tasks) |

**Migration sequence** (one monotonic line, `PRAGMA user_version`):
v1 permissions · v2 vendors · v3 requests · v4 payments-linking ·
**v5 recoverable categories** (Phase 4) · **v6 notification settings** (Phase 5).

---

## ⚠ The Playwright suite is RED — 17 failures, expected, read this first

`go build`, `go vet` and `go test ./...` are **green** (all five packages).
`shell.spec.ts` + `ux.spec.ts` — the structural and UI/UX gates — are **green on
both projects, 44 passed**, including `/accounts-queue` and the rebuilt
`/payments/new` at 390px and 1440px.

The **full** Playwright suite is `17 failed / 126 passed`. Every failure has one
cause and none is a regression:

Phase 3's Global Constraints retire the free-standing payment path by design —
"`CreatePayment` … removed from every HTTP route. `RecordPaymentForRequest` is
the only user-reachable creation method." Task 11 made `POST /payments` refuse a
request-less payment and Task 13 turned `/payments/new` into the request picker.
`tests/e2e/fixtures.ts::createPayment` still drives the old free-entry form, so
it and everything depending on it fails.

Failing: `core-workflows.spec.ts` (2) and `regression-issues.spec.ts` (15) —
ISS-003/004/005/006/017/023/024/025/026/028/029/030 and the upload smoke.

**These broke at Tasks 11 and 13, not 14** — reverting Task 14 does not recover
them. Fixing them is **Task 20**, which rewrites the e2e journey for the
reserve → pay flow. Three cannot be fixed before their screens exist:
ISS-006 needs Task 15's `settlementError` sheet for field retention;
ISS-023/024/025 need Task 16's linked payment detail. **ISS-030 needs a product
decision, not a rewrite** — it tests "Save and add another", a control the plan
removes by design.

Do not "fix" these by restoring the free-entry route; that would undo the phase.

---

## Next up

**Phase 3, Task 15** — `docs/superpowers/plans/2026-07-25-phase-3-linking-settlement-plan.md`.

Phase 3 detail:

| Task | Screen | State |
|---|---|---|
| 1–10 | Store layer | Done |
| 11 | Reservation entry + conflict screen | Done — `19a71c5` |
| 12 | Accounts queue at `/accounts-queue` | Done — `ef8b9cd` |
| 13 | Request picker | Done — `68cf8bf` |
| 14 | Payment entry | Done — `4b9a6a8` |
| 15 | Settlement preview — **pure, persists nothing** (D8) | **Resume here** |
| 16 | Settlement submit + payment detail + trail | Not started |
| 17 | Partial review | Not started |
| 18 | Release / reassign | Not started |
| 19 | Hold, unhold, stale reservation | Not started |
| 20–21 | e2e accounts journey at 390px; proof-of-absence (no refund path) | Not started |

Then Phase 4 (13 tasks), Phase 5 (12 tasks), Phase 6 (9 tasks).

**Note on the plan's route naming:** Task 12 says it rebuilds `/requests/to-pay`,
but the nav and mockups use `/accounts-queue`, which is what was built. Expect
the same drift in later task text.

---

## Standing rules for every phase

These are learned, not theoretical. Each one has already cost a debugging pass.

- **`unbuiltPrefixes` in `internal/app/nav.go` is the single switch** for screens
  with no route yet. The sidebar renders them as Soon announcements and the tab
  bar skips them. A phase that builds a screen **deletes its line**;
  `TestEveryLinkedNavItemResolves` and `TestTabBarNeverLinksToAnUnbuiltRoute`
  fail until it does. Removing a line flips nav expectations in
  `internal/app/nav_test.go` — the comments there say what to change.
- **Never `git add -A`.** The tree carries unrelated dirty files (see below) and
  parallel agents share one index. Commit with pathspec form:
  `git commit -m "..." -- <explicit paths>`.
- **The canonical permission vocabulary is 21 resources / 66 pairs**, declared
  once in `internal/store/permissions.go`. No phase may invent a verb; a test
  pins the count.
- **No template may compare a role name** (`TestTemplatesNeverCompareRoleNames`)
  **or emit `class="badge"`** (`TestNoTemplateUsesLegacyBadgeClass`). Gate with
  `.Perms.Can "resource" "action"`.
- **Do not invent CSS.** Use `web/static/fervid-ds.css`. A missing class is a
  Phase 0 defect — report it.
- **Money is `int64` paise** via `internal/money`.

### Traps that have already bitten

- **Forward foreign keys break every write.** `store.Open` sets
  `PRAGMA foreign_keys=ON`, and SQLite then rejects *every* write to a child
  table whose parent table does not exist yet — even a NULL value. This is why
  `payment_requests.recoverable_category_id` is a plain INTEGER with no
  `REFERENCES`: Phase 4's v5 owns promoting it. Do not "fix" it early.
- **PRAGMAs are per-connection.** They ride on the DSN in `store.Open`, not a
  `db.Exec` after opening — otherwise only the first pooled connection gets
  them and foreign keys are silently off everywhere else.
  `TestForeignKeysEnforcedOnEveryPooledConnection` pins this.
- **`CURRENT_TIMESTAMP` is second-resolution.** A burst of rows written in one
  test shares a timestamp, so any ordering must tie-break on `id`. A Phase 2
  test passed only by luck before this was fixed.
- **`money.FormatPaise` already includes `₹`** — use `amountValue` inside a
  `.money-field`, or it renders `₹ ₹1,00,000.00`.
- **`statusText` is taken** by the variance grid; the request helper is `reqStatus`.
- **`hidden` fieldsets still submit.** Render alternatives as alternatives.
- **`required` on a `data-when`-hidden field makes the form unsubmittable** in
  Chrome ("not focusable"). Use `aria-required`; keep asterisks `aria-hidden`.
- **`page-banner d-only` leaves a phone with no visible `h1`** and fails the UX
  sweep. Do not use it.

---

## The UI/UX gate

Both suites must be green on **both** Playwright projects before a phase is done.

```
go build ./... && go vet ./... && go test -count=1 ./...
npx playwright test --reporter=line
```

- `tests/e2e/shell.spec.ts` — structural, on a fixed `ROUTES` list. **Add every
  new route.**
- `tests/e2e/ux.spec.ts` — quality, and it **discovers its routes from the nav**,
  so new screens are swept automatically. Fails on horizontal scroll, a missing
  `h1`, wrong chrome per device, tap targets under 40px, controls trapped under
  the tab bar, any WCAG 2 A/AA violation, and invisible keyboard focus.
  **Do not weaken it to pass — fix the screen.**

**Screenshots:** `output/playwright/baseline/` is the frozen pre-redesign record
and nothing in the suite writes to it. Captures go to `current/`
(`FERVID_SHOT_DIR=<name>` for a third set). This was a trap: `baseline.spec.ts`
used to overwrite the very images it was being compared against, and
`make test-all` destroyed them once.

---

## Decisions made during the build (beyond the spec)

- **D9** — Phase 1 owns `users.default_approver_id`; Phase 2 consumes
  `SetUserDefaultApprover` and `User.DefaultApproverID` rather than declaring a
  second writer.
- **D10** — the vocabulary grew to 66 pairs so Phase 2's cancellation routes had
  verbs to register behind (`request:cancel`, `approval:cancel`).
- **Home is the dashboard.** `/` renders `dashboard`; the variance grid has its
  own `/grid`. The nav key for `/` was always `"dashboard"`; both merely
  rendered the grid until the dashboard existed.
- **Seeded roles carry the cancellation verbs** — Requester holds
  `request:cancel`, Manager holds `approval:cancel`. Without this only an Admin
  could use the cancellation screens.
- **htmx is real.** `web/static/htmx.min.js` was a 1,172-byte no-op placeholder
  from the initial commit (`version: "placeholder"`, `ajax` returning
  `Promise.resolve(null)`), so every `hx-*` attribute in the product was inert.
  Genuine **htmx 2.0.6** is now vendored locally and served from `/static/`.
  **No CDN reference** — the self-hosted posture is unchanged. This is a new
  third-party dependency and its hash has not been checked against the official
  registry.

---

## Known gaps and open items

- **Design system:** `.btn.approve`, `.metric.warn`, `.metric.good` and
  `.toolbar .search` appear in the mockups but have **no rule** in
  `web/static/fervid-ds.css`. Fold into Phase 6.
- **Vendor "Paid this year"** totals payments whose `vendor_payee` matches the
  vendor name exactly, because `payments.vendor_id` does not exist until
  Phase 3 links them. It can under-count; it cannot credit the wrong vendor.
- **Vendor "Open requests" is 0 for everyone** until the request/vendor join lands.
- **The mockup's vendor "Export CSV" was not built** — `vendor` has no `export`
  action in the canonical vocabulary, so there is no verb to gate a route on.
- **`ListPayments` does not select `request_id` / `settlement` / `partial_reason`**,
  so `Payment.RequestID` off that call is always nil. Task 16 needs this.
- **`internal/app/linking.go` carries a stub `settlementError`** that only calls
  `respondStoreError`. Task 15 replaces it, and
  `TestPaymentErrorRetainsInputAndUsesHumanModes` regains its field-retention
  assertions at the same moment (it currently proves only the 400, the message,
  and that nothing was written).
- **Symbols Tasks 15–19 assume but which do not exist:** `Store.AuditFor`,
  `store.PaidRequestRow`, `RecentPaymentsByActor`, `fervidMoney` in
  `fervid-app.js`, and a `paymentModes` helper. `ListUsers` takes no
  `activeOnly` argument, though Task 18 calls `ListUsers(ctx, true)`.
  `threadTone`/`threadGlyph` in the plan clash with the existing
  `threadDot`/`threadGlyph`.
- **Production has never run any of this.** It is still on the initial commit at
  `user_version = 0`, so it will run v1→v4 in full on first deploy. Local
  databases that ran a partial v1 need re-stamping or recreating.

## Dirty files that are NOT this work

These were already modified/untracked when the session began and have been left
alone deliberately — they are not part of any phase:

`Makefile`, `internal/store/backup.go`, `internal/store/schema.go`,
`internal/store/store_test.go`, `internal/app/http_safety_test.go`,
`internal/config/config_test.go`, `internal/store/backup_test.go`,
`deploy.sh`, `issues.html`, `docs/payment-requests-design.html`.
