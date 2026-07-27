# Fervid Budget — build progress

**Last updated:** 2026-07-27 (Phases 4, 5 and 6 complete — all phases done)
**Branch:** `main` (all work committed here; nothing pushed)

Resume by reading this file, then the phase plan named under "Next up". "All
phases done" is a build-plan status, not a defect-free claim — a QA audit ran
the same day and a repair is in progress; see "Next up" and
`docs/qa/results/REPAIR-LOG.md` before trusting any screen as correct rather
than merely built.

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
| 4 | Recoverables | **Done** (13/13) |
| 5 | Notifications | **Done** (12/12) |
| 6 | Redraw the 11 original screens | **Done** (9/9) |

**Migration sequence** (one monotonic line, `PRAGMA user_version`):
v1 permissions · v2 vendors · v3 requests · v4 payments-linking ·
**v5 accounts reservation grants** (Phase 3) ·
**v6 recoverable categories** (Phase 4) · **v7 notification settings** (Phase 5) ·
**v8 `payments.head_id` nullable** · **v9 nine more notification events** ·
**v10 `payments.vendor_id`**
(the last three from the 2026-07-27 QA audit repair — `docs/qa/results/REPAIR-LOG.md`).

> ⚠ **Phase 3 took v5.** The Phase 5 plan still says v6; it means "the next
> one", which is **v7**. Migrations are append-only and are never renumbered.
> The line stopped at v7 when the eight build phases finished; the QA audit
> repair has since carried it to **v10**, and it may grow further as repair
> waves land — do not hardcode an upper bound anywhere. The authority is the
> `migrations` slice in `internal/store/migrations.go`, whose header comment
> carries this same map.

---

## The suites, as they stood when the build phases closed

> ⚠ **"Both suites are green" — this section's old heading — is now two of three,
> and these figures are the pre-audit run.**
> This section was written when the build phases closed, and it predates the
> **audit suite** (`make test-audit`, `tests/e2e/audit-*.spec.ts`), which
> `playwright.config.ts` deliberately excludes from `make test-e2e` — so the
> `npx playwright test` line below has never covered it. That third suite is
> **not green: areas A and G are red on purpose**, because twenty-nine
> assertions there recorded defective behaviour as truth and now fail since the
> product improved (`docs/qa/results/REPAIR-LOG.md`, "Expectations that were
> wrong, not the product"). The numbers below were also measured before the four
> repair waves and have not been re-measured here — treat them as the record of
> what the build phases achieved, not as current status. `REPAIR-LOG.md` is
> current status.

```
go build ./... && go vet ./... && go test -count=1 ./...   # all five packages ok
make test-race                                             # ok, no race reports
make test-cover                                            # app 72.5%  auth 92.2%  notify 86.8%
                                                           # config 90.9%  money 100%  store 74.1%
npm run typecheck                                          # clean
npx playwright test                                        # 163 passed, 49 skipped, 0 failed
make test-audit                                            # NOT covered by the line above;
                                                           # areas A and G currently red — see REPAIR-LOG.md
```

The 49 skips are structural, and the count breaks down exactly:
`baseline` 32 + `components` 15 + `linking-settlement` 2. Each is the
**mobile-chrome copy** of a chromium-only file, skipping itself via
`test.skip(project.name !== 'chromium', …)`; nothing is skipped on desktop and
nothing is skipped because it fails. `regression-issues.spec.ts` uses a
different mechanism — `playwright.config.ts` `testIgnore` drops it from the
mobile project entirely, so it is never collected and contributes 0 skips.

Six of the ten spec files run on **both** devices, including
`core-workflows.spec.ts` (the 390 px reserve → settle journey), `shell.spec.ts`
and `ux.spec.ts`. Mobile coverage of the real flows is therefore genuine; the
three single-run files do work that is not device-dependent.

Phases 4 and 5 added 8 shell tests between them (`/recoverables`,
`/recoverables/list`, `/notifications`, `/admin/notifications`, on both
devices). Phase 6 added five capture routes to `baseline.spec.ts`, which is
chromium-only — hence 10 more skipped mobile copies and the 39 → 49 move.

---

## Next up

**Every phase in the build plan is complete** — that framing predates the audit
below and describes the plan, not the product's current defect state.
`unbuiltPrefixes` is empty: every screen the navigation knows about resolves to
a real route. But "complete" is not "correct": a QA audit on 2026-07-27 ran 564
new test cases against commit `30edd6a` across 8 areas and raised 103 area-level
findings (`docs/qa/results/AUDIT-REPORT.md:57-67`, the "What was executed"
table) before cross-area dedup — no single document states one deduplicated
total, and the coverage matrix's unrelated **"92 / 92 VERIFIED"** requirements
count (`docs/superpowers/specs/2026-07-25-payment-requests-coverage.md`, itself
now corrected — see that file) is not that number either, in case a future
reader conflates the two. A four-wave repair is under way. Read
`docs/qa/results/REPAIR-LOG.md` for what is actually fixed, in progress, or
still open before treating any phase as done in the sense of defect-free.

The open items worth a future session, beyond the repair itself, are under
"Known gaps" below. The largest is that **production has still never run any
of this** — see `docs/superpowers/PROGRESS.md` → Known gaps and `deploy.sh`.

### Where the Phase 6 plan was wrong

Phase 6's own tasks were mostly already done — Phase 0 had performed the
`.badge` → `.pill` rename and restyled login — so the real work was the nine
tables and the grid accordion. Three defects surfaced that no plan step named:

- **`baseline.spec.ts` had stopped capturing the variance grid.** Its route list
  said `{slug: 'grid', path: '/'}`, which was correct until Phase 2 made `/` the
  dashboard. For several phases `grid-*.png` has been a picture of the
  dashboard, so the widest screen in the app had no visual record at all.
- **The accordion contract is `.is-open` on a `<button class="acc-head">`,** not
  `<details>/<summary>`. `fervid-app.js` already implements the toggle and
  republishes the state through `aria-expanded`/`aria-controls`; built from
  `<details>`, every project rendered permanently collapsed and the mobile grid
  showed no heads. The mislabelled capture above is why that was invisible.
- **A restacked `<td>` is a flex row of label and value.** A cell holding
  several children spreads them as separate flex items which overlap and
  swallow taps — Playwright caught it as "Reference intercepts pointer events"
  on the payments View link. Multi-part cell values each need one wrapper.

Phase 6 detail:

| Task | Scope | Commit |
|---|---|---|
| — | The four missing CSS rules (Phase 0 defect) | `a80e18f` |
| 1–2 | `.badge` → `.pill`, login | done in Phase 0 |
| 3–7 | Nine tables restacked + grid accordion | `cd88c8d` |
| 8 | Chrome-less error page, empty-state actions | `0a17fb1` |
| 9 | Visual capture fixed; accordion on the JS contract | `278918b` |

### Where the Phase 5 plan was wrong

- **Migration is v7**, not the v6 the plan says.
- **`SetAppSettings` was already taken.** Phase 2 owns
  `AppSettings(ctx) map[string]string` and
  `SetAppSettings(ctx, actor, map[string]string)`, so the plan's
  `SetAppSettings(ctx, actor, AppSettings)` could not compile. The typed wrapper
  over that same table is `MailSettings` / `GetMailSettings` / `SetMailSettings`.
- **The plan's `kindFor` in `internal/notify` was dropped.** `store.AddNotification`
  already derives `kind` from the event at write time; a second copy could only
  drift from the one the `.segmented` filter queries.
- **The notification row markup in the plan does not lay out.** Built as a
  `<form>` wrapping a `<button>`, the centre overflowed by 1011 px once it had
  rows — caught by `ux.spec.ts`, not by eye. The approved design is
  `<a class="notif">` holding `.n-ico`, `.n-main` and a `<time>`.

**A defect found on the way:** `.gitignore`'s first line was an unanchored
`server`, meant for the built binary. It also matched the **`cmd/server`
directory**, so `cmd/server/main.go` had never been committed and a fresh clone
could not build at all. The rule is now `/server`.

Phase 5 detail:

| Task | Scope | Commit |
|---|---|---|
| 1 | Env-only SMTP password | `e97b950` |
| 2 | Migration v7, twelve events, in-app table | `0c6c0ed` |
| 3 | Mail settings, rule accessors, permission lookup | `aa43695` |
| 4 | In-app channel, scoped reads, mark-read | `3ca35c5` |
| 5 | Reminder queries on injected clock + thresholds | `a182e04` |
| 6 | Configuration "Reminders and ageing" fieldset | `1db18e8` |
| 7 | notify package: Mailer, SMTPMailer, harness | `1268073` |
| 8 | Service.Notify: in-app always, email opt-in | `b1520cb` |
| 9 | RunReminders + Scheduler | `072a8be` |
| 10 | Notification centre + bell count | `5ade0cf` |
| 11 | Admin rules screen + event hooks | `411f231` |
| 12 | Scheduler on shutdown ctx + .gitignore fix | `4307271` |
| — | Notification row rebuilt on the approved markup | `5b4182c` |

### Where the Phase 4 plan was wrong

The plan was written before Phases 2 and 3 existed and its "Consumed contracts"
section says to adapt at execution. Four things needed it, and the third was a
real defect:

- **`RequestInput` has no `RecoverableCategoryID`.** Phase 2 identifies a
  category by a *code* (`emd`, `icd`, …) in `RecoverableCategory`. The category
  table therefore carries a `code` column, which is the stable identity —
  `UpsertRecoverableCategory` derives it on create and never rewrites it, so
  renaming a category cannot orphan its requests.
- **Phase 2 already enforced the per-category rules** through a hardcoded
  `recoverableCategoryRules` map, and its own comment said Phase 4 would replace
  it with table rows. `validateRequestInput` now takes the rule set as a
  parameter and stays pure; the store supplies it from the active categories.
- **`payment_requests.recoverable_category_id` was always NULL.** Phase 2
  hardcodes it in the INSERT, and every Phase-4 register query joins on it, so
  the register would have shown a blank category for every request the real form
  has ever produced. The plan's tests could not catch it — they `INSERT` the id
  directly, the one shape `CreateRequest` never wrote. v6 back-fills existing
  rows and both write paths now populate it.
- **Seed flags: `security_deposit` requires a counterparty.** The plan seeds it
  requiring nothing, which would have silently weakened validation Phase 2 ships
  and tests. `TestSeededCategoriesMatchPhase2Rules` now fails if the seed and
  the built-in map ever drift.

Two smaller corrections: the plan's ageing/ordering test seeded *unpaid* rows and
asserted they read as overdue, contradicting the ageing spec its own label test
pins (money that never left cannot be overdue); and its absence guards demanded
404 on POST, which this router cannot produce — see `TestNoRefundRoute`.

Phase 4 detail:

| Task | Scope | Commit |
|---|---|---|
| 1 | Migration v6 + seed + back-fill | `09b7f3b` |
| 2–3 | Category CRUD, usage counts, Requires label | `60b6392` |
| 4 | Rules from the table; category id linked on write | `897da43` |
| 5 | Recoverables excluded from grid/report actuals | `941d40e` |
| 6–7 | Aged register, metrics, rollups | `45aa35b` |
| 8–10 | Dashboard, list + CSV, detail | `7afb60f` |
| 11–13 | Configuration fieldset, V7 guard, X2/X4 absence | `20160e4` |

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
  fail until it does. **It is now empty** (`internal/app/nav.go:307`) — every
  screen the navigation knows about resolves to a real route. This paragraph used
  to say it held one line for `/admin/notifications`; Phase 5 built that screen
  and emptied it. Deleting a line also flips the nav guard tests that assert the
  screen is *not* linked — updating those is part of the same task, not a
  regression.
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
- **A `<form>` inside another `<form>` is invalid HTML** and the parser drops
  the inner one silently — its controls post nothing. The Configuration screen
  is one big settings form, so a section needing its own POST (Phase 4's
  recoverable categories) goes *outside* it. A bare `<fieldset>` outside a form
  is valid and reads as the same section.
- **An unrouted POST answers 405, not 404.** `routes()` registers a catch-all
  `GET /`, so the path matches and the method does not. Proof-of-absence tests
  must accept either; `TestNoRefundRoute` documents this.
- **Verify every class you add against the stylesheet.** Phase 4 caught two this
  way: an invented `.dl-wide` (the convention is `style="grid-column:1/-1"`) and
  `.vh` (the convention is `.sr-only`). Neither renders, and no test sees it.
- **A read-then-write transaction races.** `BeginTx → read → write` lets a
  second writer's SQLite transaction race the first, and `_pragma=busy_timeout`
  on the DSN does **not** apply to the read-to-write upgrade, so the loser gets
  an instant `500 SQLITE_BUSY` instead of waiting its turn (found in the audit
  as F-B-10/F-C-05, `docs/qa/results/REPAIR-LOG.md` Wave 2). Take the write lock
  with the transaction's **first** statement instead — `beginWriteTx`
  (`internal/store/requests.go:16-45`) does this with a no-op `UPDATE …
  WHERE 1=0` — then guard the real write with the state it expects and check
  `RowsAffected`. Any new writer added to the request or recoverable flow
  should open through `beginWriteTx`, not a bare `BeginTx`.
- **A migration cannot drop `NOT NULL` in place, and `PRAGMA foreign_keys`
  cannot be toggled inside the migration's own transaction** — it rides on the
  DSN (see the pooled-connection trap above). `PRAGMA defer_foreign_keys` alone
  is not a substitute: SQLite counts deferred violations as a running total that
  only DML adjusts, so the violations an implicit `DROP TABLE` books against a
  child table are never cancelled by a later rename, and `COMMIT` still fails.
  The working recipe — stash the dependent table's rows, empty it, rebuild the
  parent with the new column definition, copy rows back with their original
  ids, restore the dependent table's rows — is `upPaymentsHeadNullable`
  (`internal/store/migrations.go:435`, migration v8). Renaming the old table
  aside and swapping in the new one under the old name does not work either:
  this driver rewrites a child table's `REFERENCES` clause even under
  `legacy_alter_table`.
- **If a screen's copy says somebody is told, grep for the `a.fire` that tells
  them.** Nothing binds the sentence to the send, so the two drift silently: the
  reservation screen promised *"The requester and the approver are both
  notified"* while release, reassign and unhold fired nothing (finding F-D-12).
  Wave 4 fixed those three — and Wave 3's new approver-reassignment sheet
  reintroduced the same defect one screen over, promising *"The new approver is
  told"* from a handler with no `a.fire` and no event in the vocabulary. Adding a
  route that changes who owes the next action means adding its event to
  `notify.AllEvents` **and** firing it, or not writing the promise. Both live
  instances are recorded under "Still open" in
  `docs/qa/results/REPAIR-LOG.md`.

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
  approved. **This paragraph used to say a hold was dropped, permanently, the
  moment a requester asked for cancellation — that was finding F-C-07, not a
  decision, and it broke requirement L7 ("On hold — only Accounts lifts"): a
  manager declining the cancellation returned the request to `approved` with
  the accountant's hold silently gone.** Fixed in REPAIR-LOG Wave 2 (commit
  `25411b8`, decision 3): `RequestCancellation` clears `on_hold` but keeps
  `hold_reason` (`internal/store/requests.go:1268-1286`); `DecideCancellation`
  restores `on_hold` from `hold_reason` on a decline and clears both on accept
  (`requests.go:1310-1333`). The hold event and its reason survive in the audit
  trail either way.

---

## Known gaps and open items

> **A QA audit ran on 2026-07-27** against commit `30edd6a` and raised 103
> area-level findings (see "Next up" above for why that number, not "92", is
> the defensible one), five of them critical. **`docs/qa/results/AUDIT-REPORT.md`
> supersedes this section** for anything it covers, and
> `docs/qa/results/REPAIR-LOG.md` tracks what has actually been fixed since —
> `FIX-PLAN.md` is the wave layout, not current status. The audit also corrected
> several claims made *in this file* — see its "Documentation that is now
> wrong" table. Corrections applied below.

- ~~**Design system:** `.btn.approve`, `.metric.warn`, `.metric.good` and
  `.metric-foot` have no rule.~~ **Fixed in Phase 6** by commit `a80e18f`, which
  this file's own Phase-6 detail table records. All four have rules today —
  `web/static/fervid-ds.css:367`, `:378`, `:387`, `:397`. The 2026-07-27 QA audit
  verified this by reading `getComputedStyle` on the live screen; see
  `docs/qa/results/findings-e-recoverables.md` F-E-09 and
  `findings-d-linking-settlement.md` F-D-13. Kept here struck through because two
  independent agents wasted a pass on the stale claim.
- **The payment-detail mockup's `Bank` row** (`HDFC ····4471 · IFSC …`) is not
  built. It needs `vendor_bank`-restricted data on a screen no spec authorises
  for it, plus an account-masking helper with no precedent. Needs a spec
  decision, not an implementation pass.
- ~~**`lockMonth`/`unlockMonth` redirect to `/?month=`**, which was the variance
  grid before `/` became the dashboard. Locking a month now drops the operator on
  a page that shows no confirmation. Pre-existing; `/grid?month=` for both would
  fix it. Phase 6 territory.~~ **Fixed** (F-G-019 family, Wave 4): both handlers
  redirect to `/grid?month=` + month (`internal/app/app.go:1387,1396`).
- ~~**Vendor "Paid this year"** still matches payments by payee *name* because
  `payments.vendor_id` does not exist.~~ **Fixed by migration v10** (F-G-009,
  2026-07-27 audit repair): `payments.vendor_id` now exists and is back-filled
  from the request each payment settles; `vendorPaidThisYear` joins on it
  (`internal/store/vendors.go:171`, `migrations.go:29,390,399`). The
  payee-name match survives only as a fallback, for a payment that settles no
  request and so was never linked to a vendor id — history, not the primary path.
- ~~**Vendor "Open requests" is 0 for everyone** until the request/vendor join
  lands.~~ **Fixed** (F-G-010): it now counts `requestBuckets["open"]` per
  vendor — the same status set `/requests?bucket=open` uses, so the tile and the
  list it links to cannot disagree (`internal/store/vendors.go:192`).
- **The mockup's vendor "Export CSV" was not built** — `vendor` has no `export`
  action in the canonical vocabulary, so there is no verb to gate a route on.
- ~~**The stale-reservation screen's "reminder sent" trail line has no writer**
  — no reminder is actually sent until Phase 5.~~ **Resolved twice over:**
  Phase 5 shipped reminders, and the QA audit found the writer itself was still
  missing (F-F-02) — `MarkReminderSent` now writes an audit row on every
  reminder (`internal/store/reminders.go:171`, fixed REPAIR-LOG Wave 2, commit
  `25411b8`).
- **`reassignCandidates` runs one permission query per user.** Fine at this user
  count; if the user table grows the fix is a batched permission read in the
  store, not a looser filter.
- **Production has never run any of this.** It is still on the initial commit at
  `user_version = 0`, so it will run **the whole chain — v1 through the current
  head — in full on first deploy**. Do not trust a hardcoded upper bound here: an
  earlier version of this line said "v1→v5" while the chain had already reached
  v7, and the QA audit's repair has since added v8, v9 and v10 — and may still
  be adding more as later waves land. The authority is the `migrations` slice in
  `internal/store/migrations.go`. Local databases that ran a partial v1 need
  re-stamping or recreating.
