# Fervid Budget — build progress

**Last updated:** 2026-07-27
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
| 3 | Linking + settlement | **Done** (21/21) |
| 4 | Recoverables | Not started (13 tasks) |
| 5 | Notifications | Not started (12 tasks) |
| 6 | Redraw the 11 original screens | Not started (9 tasks) |

**Migration sequence** (one monotonic line, `PRAGMA user_version`):
v1 permissions · v2 vendors · v3 requests · v4 payments-linking ·
**v5 accounts reservation grants** (Phase 3) ·
**v6 recoverable categories** (Phase 4) · **v7 notification settings** (Phase 5).

> ⚠ **Phase 3 took v5.** The plans for Phases 4 and 5 still say v5 and v6. They
> mean "the next two"; migrations are append-only and are never renumbered.

---

## Both suites are green

```
go build ./... && go vet ./... && go test -count=1 ./...   # all five packages ok
make test-race                                             # ok, no race reports
make test-cover                                            # app 72.1%  auth 92.2%
                                                           # config 90.9%  money 100%  store 73.0%
npm run typecheck                                          # clean
npx playwright test                                        # 145 passed, 39 skipped, 0 failed
```

The 39 skips are structural and pre-existing: the `mobile-chrome` copies of the
three chromium-only spec files (`baseline`, `components`, `linking-settlement`),
skipped by their own guards. `regression-issues.spec.ts` is excluded from mobile
by `playwright.config.ts` `testIgnore` and contributes no skips.

The suite was 17 failed / 126 passed at the start of this session. Every failure
had one cause — `fixtures.ts::createPayment` drove the free-entry payment form
that Phase 3 retires by design — and all 17 are now repaired onto the real
reserve → settle flow.

---

## Next up

**Phase 4 — recoverables**, `docs/superpowers/plans/2026-07-25-phase-4-recoverables-plan.md`
(13 tasks). Its migration is **v6**, not the v5 the plan text says.

Phase 3 detail, for reference:

| Task | Scope | Commit |
|---|---|---|
| 1–10 | Store layer | earlier session |
| 11–14 | Conflict screen, queue, picker, entry | `19a71c5` `ef8b9cd` `68cf8bf` `4b9a6a8` |
| 15 | Settlement preview (pure, D8) | `c9cc62a` |
| 16 | Settlement submit, payment detail, trail | `3c30370` |
| 17 | Partial review + two sheets | `6a0b15e` |
| 18 | Release / reassign | `3ebb8dd` |
| 19 | Hold, unhold, stale reservation | `3daa98d` |
| 20 | e2e fixtures, 390 px journey, 17 repairs | `5f06f52` `7bdeaa3` `3596e2f` `a02045d` `980f3ba` |
| 21 | Proof-of-absence: no refund path | `6aadbb2` |
| — | Blank-payee fix across the flow | `fca6939` |

---

## Standing rules for every phase

These are learned, not theoretical. Each one has already cost a debugging pass.

- **`unbuiltPrefixes` in `internal/app/nav.go` is the single switch** for screens
  with no route yet. The sidebar renders them as Soon announcements and the tab
  bar skips them. A phase that builds a screen **deletes its line**;
  `TestEveryLinkedNavItemResolves` and `TestTabBarNeverLinksToAnUnbuiltRoute`
  fail until it does. It currently holds `/recoverables` (Phase 4) and
  `/admin/notifications` (Phase 5).
- **Never `git add -A`.** Parallel agents share one index. Commit with pathspec
  form: `git commit -m "..." -- <explicit paths>`.
- **The canonical permission vocabulary is 21 resources / 66 pairs**, declared
  once in `internal/store/permissions.go`. No phase may invent a verb; a test
  pins the count.
- **No template may compare a role name** (`TestTemplatesNeverCompareRoleNames`)
  **or emit `class="badge"`** (`TestNoTemplateUsesLegacyBadgeClass`). Gate with
  `.Perms.Can "resource" "action"`.
- **Do not invent CSS.** Use `web/static/fervid-ds.css`. A missing class is a
  Phase 0 defect — report it. Worth running after any template work:
  extract every `class="…"` you added and grep each token against the stylesheet.
  An invented class renders as nothing and no test catches it.
- **Money is `int64` paise** via `internal/money`.

### Traps that have already bitten

- **A vendor_invoice has no `vendor_payee`.** The snapshot column is filled only
  for reimbursement and employee advance; a vendor request names its payee with
  `vendor_id`. `store.Request.Vendor` is the display payee
  (`COALESCE(NULLIF(v.name,''), r.vendor_payee)`) and **screens must read that**.
  Reading the raw snapshot showed a blank payee across the whole settlement flow
  and wrote payments with nobody to pay. It survived a green suite because
  `seedApprovedRequest` writes `vendor_payee` directly with `vendor_id` NULL —
  the one shape the real form can never produce. Use `seedVendorRequest` when a
  test needs a request shaped the way the UI makes one.
- **Seeding only happens in v1.** `seedSystemRoles` runs inside migration v1, so
  a later phase that needs a new grant on a *system* role must add its own
  migration — an installed database will never re-run the seed. Use
  `INSERT OR IGNORE` for the grant, never a re-seed: a re-seed resets every
  deliberate change an administrator has made.
- **Forward foreign keys break every write.** `store.Open` sets
  `PRAGMA foreign_keys=ON`, and SQLite then rejects *every* write to a child
  table whose parent table does not exist yet — even a NULL value. This is why
  `payment_requests.recoverable_category_id` is a plain INTEGER with no
  `REFERENCES`: Phase 4 owns promoting it. Do not "fix" it early.
- **PRAGMAs are per-connection.** They ride on the DSN in `store.Open`, not a
  `db.Exec` after opening — otherwise only the first pooled connection gets
  them and foreign keys are silently off everywhere else.
  `TestForeignKeysEnforcedOnEveryPooledConnection` pins this.
- **`CURRENT_TIMESTAMP` is second-resolution.** A burst of rows written in one
  test shares a timestamp, so any ordering must tie-break on `id`.
- **`money.FormatPaise` already includes `₹`** — use `amountValue` inside a
  `.money-field`, or it renders `₹ ₹1,00,000.00`.
- **`statusText` is taken** by the variance grid; the request helper is `reqStatus`.
- **`hidden` fieldsets still submit**, and `hidden` is not validation. Re-enforce
  every `data-when` reveal server-side.
- **`required` on a `data-when`-hidden field makes the form unsubmittable** in
  Chrome ("not focusable"). Use `aria-required`; keep asterisks `aria-hidden`.
- **`page-banner d-only` leaves a phone with no visible `h1`** and fails the UX
  sweep. Do not use it.
- **Format helpers must agree about the clock.** `hhmm` localised while `date`
  and `datep` did not, so one screen printed two different times for one event.

---

## The UI/UX gate

Both suites must be green on **both** Playwright projects before a phase is done.

```
go build ./... && go vet ./... && go test -count=1 ./...
npx playwright test --reporter=line
```

- `tests/e2e/shell.spec.ts` — structural, on a fixed `ROUTES` list. **Add every
  new static route.** Id-parameterised routes cannot go here.
- `tests/e2e/ux.spec.ts` — quality, and it **discovers its routes from the nav**,
  so new screens are swept automatically. Fails on horizontal scroll, a missing
  `h1`, wrong chrome per device, tap targets under 40px, controls trapped under
  the tab bar, any WCAG 2 A/AA violation, and invisible keyboard focus.
  **Do not weaken it to pass — fix the screen.**

**e2e fixtures.** `tests/e2e/fixtures.ts` exports `createApprovedRequest`,
`settlePayment`, `createAccountsUser`, `createApproverUser` and a `secondPage`
context. `createPayment` is gone — it drove the retired free-entry form.
Selector notes that cost an hour each if forgotten:
- the entry screen's labels are `Approved amount`, `Amount actually paid`,
  `Paid on`, `Payment mode`, `Transaction / UTR reference`, `Processing note`;
- the file input is `hidden` with **no accessible name** — use
  `input[name="attachment"]`, never `getByLabel('Attachment')`;
- the settlement sheet does not change the URL (htmx swaps it into
  `#settle-mount`), and its "Go back" is an `<a>`, not a button;
- the partial review's sheets belong to the request's **manager**, not to an
  admin holding every grant.

**Screenshots:** `output/playwright/baseline/` is the frozen pre-redesign record
and nothing in the suite writes to it. Captures go to `current/`
(`FERVID_SHOT_DIR=<name>` for a third set). This was a trap: `baseline.spec.ts`
used to overwrite the very images it was being compared against, and
`make test-all` destroyed them once.

---

## Decisions made during the build (beyond the spec)

- **D9** — Phase 1 owns `users.default_approver_id`; Phase 2 consumes
  `SetUserDefaultApprover` and `User.DefaultApproverID`.
- **D10** — the vocabulary grew to 66 pairs so Phase 2's cancellation routes had
  verbs to register behind (`request:cancel`, `approval:cancel`).
- **Home is the dashboard.** `/` renders `dashboard`; the variance grid has its
  own `/grid`.
- **Seeded roles carry the cancellation verbs** — Requester holds
  `request:cancel`, Manager holds `approval:cancel`.
- **htmx is real.** `web/static/htmx.min.js` was a 1,172-byte no-op placeholder
  from the initial commit, so every `hx-*` attribute in the product was inert.
  Genuine **htmx 2.0.6** is now vendored locally and served from `/static/`.
  **No CDN reference.** Its hash has not been checked against the official
  registry.
- **The reservation screen is gated by ownership, not by one verb.** The queue
  and the conflict screen link to it gated on `reservation:reassign`, so a
  release-only route gate would 403 exactly the reader those links are for.
  `reservationForm` loads through `loadViewableRequest` and refuses unless the
  caller either holds the reservation and may release, or may reassign.
- **A partial-payment decision belongs to the request's own manager**, not to
  anyone holding `approval:accept_partial` — an admin holds every grant but is
  not the manager.
- **`on_hold=1` implies `status='approved'`.** Enforced at every exit from
  approved. Consequence: a hold placed before a cancellation is dropped when the
  requester asks for cancellation, so a manager who *declines* that cancellation
  returns the request to approved with no hold. The hold event and its reason
  survive in the audit trail.

---

## Known gaps and open items

- **Design system:** `.btn.approve`, `.metric.warn` and `.metric.good` appear in
  the mockups but have **no rule** in `web/static/fervid-ds.css`. No template
  uses them today — Phase 3 dropped the dead `approve` token rather than
  inventing CSS — so the green/neutral distinction the partial-review mockup
  draws between the two terminal answers is not yet available. Fold into Phase 6.
- **The payment-detail mockup's `Bank` row** (`HDFC ····4471 · IFSC …`) is not
  built. It needs `vendor_bank`-restricted data on a screen no spec authorises
  for it, plus an account-masking helper with no precedent. Needs a spec
  decision, not an implementation pass.
- **`lockMonth`/`unlockMonth` redirect to `/?month=`**, which was the variance
  grid before `/` became the dashboard. Locking a month now drops the operator on
  a page that shows no confirmation. Pre-existing; `/grid?month=` for both would
  fix it. Phase 6 territory.
- **Vendor "Paid this year"** still matches payments by payee *name* because
  `payments.vendor_id` does not exist. Now that linked payments store the
  vendor's display name this is materially more accurate, but it is still a name
  match and can under-count.
- **Vendor "Open requests" is 0 for everyone** until the request/vendor join lands.
- **The mockup's vendor "Export CSV" was not built** — `vendor` has no `export`
  action in the canonical vocabulary, so there is no verb to gate a route on.
- **The stale-reservation screen's "reminder sent" trail line has no writer** —
  no reminder is actually sent until Phase 5.
- **`reassignCandidates` runs one permission query per user.** Fine at this user
  count; if the user table grows the fix is a batched permission read in the
  store, not a looser filter.
- **Production has never run any of this.** It is still on the initial commit at
  `user_version = 0`, so it will run v1→v5 in full on first deploy. Local
  databases that ran a partial v1 need re-stamping or recreating.
