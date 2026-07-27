# TC-E — Recoverables

Scope: recoverable categories (CRUD, per-category field rules), the recoverable
request path, exclusion of recoverable money from budget actuals, the
recoverables register/dashboard/detail, close-on-payment, project linkage, and
proof of absence for repayment/forfeiture tracking. Settlement mechanics,
general request raising and notifications belong to sibling documents.

**UC-C does not exist yet.** `docs/qa/use-cases/UC-C-settlement-recoverables-notifications.md`
— the document that would own this area — has not been written by the time
this suite was authored (checked immediately before writing this file: only
`UC-A-platform-rbac-admin.md` and `UC-B-requests-approvals.md` exist under
`docs/qa/use-cases/`). Every "traces to" cell below therefore cites the
coverage-matrix IDs from `docs/superpowers/specs/2026-07-25-payment-requests-coverage.md`
only (`V1`–`V8`, `X2`, `X4`, `R6`, `D3`), plus `F-E-nn` for this document's own
findings (`docs/qa/results/findings-e-recoverables.md`).

**Ground truth used to write every expected result**, cited once here rather
than in every row:

- Categories, rules and CRUD: `internal/store/recoverables.go`,
  `internal/store/requests.go:107-247`, `internal/app/configuration.go`,
  `internal/app/templates.go:3119-3166`.
- Migration v6 (not v4 — the phase-4 plan's numbering is stale):
  `internal/store/migrations.go:313-320`.
- Seeded categories and rules (`internal/store/recoverables.go:21-31`,
  cross-checked against `internal/store/requests.go:116-123`): Employee
  advance (neither), EMD (project), PBG (project), ICD (counterparty),
  **Security deposit (counterparty — the phase-4 plan text says "nothing",
  which is wrong; `TestMigrationV6SeedsRecoverableCategories` and
  `TestSeededCategoriesMatchPhase2Rules` in `internal/store/recoverables_test.go`
  pin the counterparty requirement)**, Other (neither).
- Routes and permission gates: `internal/app/app.go:400-406,516-520`,
  `internal/store/permissions.go:164-165,181`,
  `internal/store/migrations.go:351-401` (role grants),
  cross-checked against the sibling route matrix
  `docs/qa/uml/05-route-permission-matrix.md` (RT-26–29, RT-92, RT-94,
  PM-26–29, PM-94), which independently derives the same gates.
- Grid/report exclusion SQL: `internal/store/store.go:1380-1400` (Grid),
  `internal/store/recoverables.go:250-258` (`recoverableBaseWhere`,
  `recoverableAmount`).
- Templates: `internal/app/templates.go:3429-3643` (dashboard, list, detail),
  `internal/app/templates.go:1729-1805` (recoverable request fieldset).
- **A defect discovered while building this suite governs several expected
  results below: F-E-01.** The recoverable fieldset never collects a head
  (`internal/app/templates.go:1731-1781` has no head control for any of the
  six categories), so `payments/new`'s hidden `head_id` defaults to `0`
  (`internal/app/linking.go:243-244`), and `validatePayment`
  (`internal/store/store.go:1620-1623`) refuses `HeadID == 0`. **No recoverable
  request raised through the real screens can be settled.** This is proven
  live in TC-E-030 and detailed in `findings-e-recoverables.md` (F-E-01,
  critical). Every test case whose precondition is "a *paid* recoverable
  request" is marked `BLOCKED` and cross-references the Go store test that
  proves the same logic at the unit level instead.

---

## Summary table

| TC ID | Title | Traces to | Type | Priority | Verdict |
|---|---|---|---|---|---|
| TC-E-001 | Admin creates a recoverable category through the real screen | V4 | functional | high | PASS |
| TC-E-002 | Renaming a category preserves its code; existing request and register still resolve to it | V4 | regression | high | PASS |
| TC-E-003 | Reordering changes list order everywhere the order is read | V4 | functional | medium | PASS |
| TC-E-004 | Deactivating blocks new requests but leaves history and the list filter intact | V4 | functional | high | PASS |
| TC-E-005 | Duplicate name differing only by case is refused | V4 | negative/boundary | medium | PASS |
| TC-E-006 | "In use" count matches the number of requests naming the category | V4 | functional | medium | PASS |
| TC-E-007 | No delete action exists for a category, in use or not | V4 | proof-of-absence | high | PASS |
| TC-E-008 | The pre-Phase-4 standalone `/recoverable-categories` screen is gone | V4 | proof-of-absence | low | PASS |
| TC-E-009 | Requester, Manager, Accounts refused `POST /configuration/recoverable-categories`; Admin succeeds | V4, R6 | permission | high | PASS |
| TC-E-010 | A newly admin-created category is never selectable on the real request form | V4 | regression | medium | FAIL (product defect) |
| TC-E-011 | A deactivated seed category is still offered on the request form and only refused after submit | V4, V5 | regression | medium | FAIL (product defect) |
| TC-E-012 | EMD without a project is refused | V5 | negative | high | PASS |
| TC-E-013 | EMD with a project is accepted | V5 | functional | high | PASS |
| TC-E-014 | PBG without a project is refused | V5 | negative | high | PASS |
| TC-E-015 | PBG with a project is accepted | V5 | functional | high | PASS |
| TC-E-016 | ICD without a counterparty is refused | V5 | negative | high | PASS |
| TC-E-017 | ICD with a counterparty is accepted | V5 | functional | high | PASS |
| TC-E-018 | Security deposit without a counterparty is refused (pins the plan-vs-code divergence) | V5 | negative | high | PASS |
| TC-E-019 | Security deposit with a counterparty is accepted | V5 | functional | high | PASS |
| TC-E-020 | Employee advance needs neither project nor counterparty; payee auto-records the requester | V5, V6 | functional | high | PASS |
| TC-E-021 | Other needs neither project nor counterparty | V5 | functional | medium | PASS |
| TC-E-022 | Expected return date is required for every recoverable category | V6 | negative/boundary | high | PASS |
| TC-E-023 | Repayment/refund terms is required for every recoverable category | V6 | negative/boundary | high | PASS |
| TC-E-024 | A brand-new category's own "project" rule is enforced | V4, V5 | functional | high | PASS |
| TC-E-025 | Same new category's own "counterparty" rule is enforced | V4, V5 | functional | high | PASS |
| TC-E-026 | Same new category accepted once both are supplied — rules really come from the table | V4, V5 | functional | high | PASS |
| TC-E-027 | Baseline: grid and per-head report agree on the actual for a project/head before any change | V2, V3 | functional | high | PASS |
| TC-E-028 | A settled budget payment raises the grid actual by exactly the paid amount | V2 | functional | high | PASS |
| TC-E-029 | The same rise is reflected in the per-head report and both CSV exports | V2, D3 | functional | high | PASS |
| TC-E-030 | **A recoverable request raised through the real screens can be reserved, paid and closed** | V2, V3, V7 | functional | critical | FAIL (product defect) |
| TC-E-031 | Grid/report stay unchanged after a *paid* recoverable — blocked by F-E-01 | V2, V3 | functional | high | BLOCKED |
| TC-E-032 | An approved-but-unpaid recoverable contributes nothing to grid or report | V2 | functional | high | PASS |
| TC-E-033 | That unpaid recoverable is present in the register and the dashboard's outstanding totals | V3 | functional | high | PASS |
| TC-E-034 | `/recoverables/list.csv` total agrees with the on-screen list total | V3, D3 | functional | medium | PASS |
| TC-E-035 | Dashboard renders its metric strip, rollups and the two explanatory banners | V2, V3 | functional | medium | PASS |
| TC-E-036 | An unpaid recoverable never reads as overdue, however old its expected return date | V3 | boundary | high | PASS |
| TC-E-036b | The dashboard's "Past expected return" tile applies the same "unpaid can't be overdue" rule as the row | V3 | functional | medium | FAIL (product defect) |
| TC-E-037 | `/recoverables/list.csv` reaches the export handler, not the `{id}` detail handler | V3 | regression | high | PASS |
| TC-E-038 | `/recoverables/{id}` for a non-recoverable request answers 404 | V3 | negative | medium | PASS |
| TC-E-039 | `ageing=unpaid` narrows the register correctly | V3 | functional | low | PASS |
| TC-E-040 | Dashboard Outstanding metric and category rollup agree with the list's own footer total | V3 | functional | medium | PASS |
| TC-E-041 | `.metric-foot` now carries a real CSS rule (informational; previously a Phase-0 gap) | — | regression | low | PASS |
| TC-E-042 | Category and expected-return survive settlement — blocked by F-E-01 | V7 | regression | high | BLOCKED |
| TC-E-043 | Before settlement, the detail screen already shows category and expected return correctly | V6, V7 | functional | medium | PASS |
| TC-E-044 | An EMD linked to a real project shows in that project's register row while absent from its grid actuals | V8, V2 | functional | high | PASS |
| TC-E-045 | A recoverable with no project reads "Not project linked" everywhere | V8 | functional | low | PASS |
| TC-E-046 | No repayment route (GET) | X2 | proof-of-absence | high | PASS |
| TC-E-047 | No repayment route (POST) | X2 | proof-of-absence | high | PASS |
| TC-E-048 | No forfeiture/write-off route (GET) | X4 | proof-of-absence | high | PASS |
| TC-E-049 | No forfeiture/write-off route (POST) | X4 | proof-of-absence | high | PASS |
| TC-E-050 | No repayment or forfeiture control appears in any recoverable screen's markup | X2, X4 | proof-of-absence | medium | PASS |
| TC-E-051 | Requester refused `recoverable_report:view` on all three view routes | R6 | permission | high | PASS |
| TC-E-052 | Manager refused `recoverable_report:view` on all three view routes | R6 | permission | high | PASS |
| TC-E-053 | Accounts granted `recoverable_report:view` on all three view routes | R6 | permission | high | PASS |
| TC-E-054 | Requester/Manager refused `recoverable_report:export`; Accounts granted it | R6, D3 | permission | high | PASS |
| TC-E-055 | Admin holds every recoverable verb; anonymous refused all four routes | R6 | permission | medium | PASS |

56 test cases (55 planned plus TC-E-036b, added when TC-E-036 turned up a second,
narrower defect worth its own pinned assertion). 52 executed to a real pass/fail
(50 PASS, 2 `test.fail()` proofs of confirmed defects — F-E-01 and F-E-05 — which
report as **passed** because the harness expects them to fail), and 2 are
genuinely `BLOCKED` by F-E-01 (Playwright counterpart `test.fixme()`, not
deleted). Final run: `FERVID_E2E_PORT=4305 npx playwright test
tests/e2e/audit-e-recoverables.spec.ts --project=chromium --reporter=line` →
**0 failed, 2 skipped, 50 passed**.

---

## Detail

### Section 1 — Category CRUD (V4)

#### TC-E-001 — Admin creates a recoverable category through the real screen
**Traces to:** V4. **Type:** functional. **Priority:** high.
**Preconditions:** signed in as Admin (holds `recoverable_category:edit`, the
only verb the one mutating route checks — `app.go:520`).
**Steps:** Open `/configuration`. In the "Recoverable categories" fieldset,
fill "New category" = `Retention money «runId»`, "Must also capture" =
"Related project", submit.
**Expected:** 303 → `/configuration`; the new row appears in the table with
"Requires" = "Related project", "Active" = on, "In use" = 0
(`recoverables.go:184-227` inserts with a derived code
`retention_money_<runid-ish>`; `Requires()` at `recoverables.go:83-94` renders
the phrase).
**Actual:** Matches. **Verdict:** PASS.

#### TC-E-002 — Renaming a category preserves its code; existing request and register still resolve to it
**Traces to:** V4. **Type:** regression. **Priority:** high.
**Preconditions:** the category from TC-E-001; one request filed against it
(via `probePost /requests`, since the request form cannot select a
non-seeded category — see TC-E-010).
**Steps:** (1) Note the category's `code` is stable by construction — it is
derived once at INSERT and the `UPDATE` in `UpsertRecoverableCategory`
deliberately excludes it from the `SET` list (`recoverables.go:211-216`). (2)
File a recoverable request naming the category's code. (3) Rename the category
(same `id`, new `name`) by resubmitting its per-row form with a different
`name` value — the real screen's own per-row form only auto-submits the
`active` checkbox, so this step uses `probePost` against the same route with
the same field shape the row's hidden inputs already carry (`templates.go:3135-3145`),
which is the same server code path a rename would use if the UI exposed one.
(4) Reload `/recoverables/list` and the request's own `/recoverables/{id}`.
**Expected:** the request still resolves to the category (its
`recoverable_category_id` is untouched — nothing in `UpsertRecoverableCategory`
touches child rows), and both screens now show the **new** name, because they
join on `recoverable_category_id` and read `rc.name` live
(`recoverables.go:288-298`, `:130-172`).
**Actual:** Matches. **Verdict:** PASS.

#### TC-E-003 — Reordering changes list order everywhere the order is read
**Traces to:** V4. **Type:** functional. **Priority:** medium.
**Steps:** Create two categories with `sort_order` 50 and 51 (`probePost`,
since the real "add" form never sends a `sort_order` — see notes below).
Reload `/configuration` and `/recoverables/list`'s category filter.
**Expected:** `ListRecoverableCategories` orders `ORDER BY sort_order,name`
(`recoverables.go:167`), so the 50-row precedes the 51-row in both places. A
resubmission with the two `sort_order` values swapped reverses the order in
both places on the next load.
**Actual:** Matches.
**Note (defect, low severity, folded into F-E-02):** the real "Add category"
form (`templates.go:3150-3163`) has no `sort_order` field at all — every
category created through the screen lands at `sort_order=0` regardless of how
many already exist, and the per-row toggle-active form only round-trips the
*existing* `sort_order` via a hidden input. There is **no way to reorder
categories through the UI**; `sort_order` is fully write-only from the
screen's perspective. This test proves the store honours whatever value is
sent, using the same route a UI control would use if one existed.
**Verdict:** PASS (store behaviour); see F-E-02 for the missing UI affordance.

#### TC-E-004 — Deactivating blocks new requests but leaves history and the list filter intact
**Traces to:** V4. **Type:** functional. **Priority:** high.
**Preconditions:** signed in as Admin.
**Steps:** (1) File a recoverable request against the seeded "PBG" category
(project supplied) and note it succeeds. (2) On `/configuration`, uncheck
PBG's "Active" box (auto-submits, `templates.go:3142`). (3) Attempt to file a
**second** PBG recoverable request through `/requests/new?type=employee_advance`
with category `pbg` selected. (4) Reload `/recoverables/list` and open its
category filter `<select>`.
**Expected:** step 1's request is unaffected (nothing about `active` touches
existing rows). Step 3 is refused with "choose a recoverable category" —
`recoverableRules` builds its map from `ListRecoverableCategories(ctx, true)`
(`recoverables.go:119-129`), which excludes inactive rows, so the lookup in
`needsRecoverable()` (`requests.go:174-177`) misses and returns the generic
"choose a recoverable category" error even though a category named "PBG" is
visibly selected on the form (see TC-E-011). Step 4: PBG **still appears** in
the filter, because `recoverablesList` calls
`ListRecoverableCategories(r.Context(), false)` (`recoverables.go:81`,
`activeOnly=false`) — deactivation must not make historical PBG rows
unfilterable.
**Actual:** Matches exactly, including the "still appears in the filter"
half, which is easy to mis-predict as a bug but is deliberate (historical
reporting must not lose a filter for money already recorded under it).
**Verdict:** PASS.

#### TC-E-005 — Duplicate name differing only by case is refused
**Traces to:** V4. **Type:** negative/boundary. **Priority:** medium.
**Steps:** Create "Deposit Probe «runId»"; then attempt to create
"deposit probe «RUNID»" (same text, different case).
**Expected:** the second is refused. `idx_recoverable_categories_name_nocase`
is a case-insensitive unique index (`recoverables.go:50`), and
`UpsertRecoverableCategory` surfaces the constraint via `classify(err)` →
`ErrDuplicate` → HTTP 400 (`http_errors.go` `storeErrorStatus` mapping cited
in `UC-B`'s CV5).
**Actual:** Matches (400, category not created twice).
**Verdict:** PASS.

#### TC-E-006 — "In use" count matches the number of requests naming the category
**Traces to:** V4. **Type:** functional. **Priority:** medium.
**Steps:** Create a fresh category. File 2 recoverable requests naming it
(`probePost`). Reload `/configuration`.
**Expected:** its "In use" cell reads `2` —
`ListRecoverableCategoriesWithUsage`'s correlated subquery
`(SELECT COUNT(*) FROM payment_requests pr WHERE pr.recoverable_category_id=c.id)`
(`recoverables.go:230-232`) counts by the FK id, not the code, so it is exact
regardless of status.
**Actual:** Matches. **Verdict:** PASS.

#### TC-E-007 — No delete action exists for a category, in use or not
**Traces to:** V4. **Type:** proof-of-absence. **Priority:** high.
**Steps:** (1) Inspect the Configuration screen's Recoverable-categories
fieldset markup for any delete control. (2) `probePost` a plausible delete
route: `POST /configuration/recoverable-categories/{id}/delete`. (3) As Admin,
resubmit the per-row form for an **in-use** category (PBG) with its existing
values unchanged.
**Expected:** (1) no delete button/link/form exists anywhere in the fieldset
— the only per-row control is the auto-submitting `active` checkbox
(`templates.go:3135-3145`); the only mutating store method is
`UpsertRecoverableCategory`, which only ever `INSERT`s or `UPDATE`s
(`recoverables.go:184-227`) — there is no `DELETE FROM recoverable_categories`
anywhere in the codebase. (2) 404 or 405 (no such route is registered — the
catch-all `GET /` matches on path, not method). (3) succeeds normally (an
in-use category can always be re-saved; nothing about usage blocks a save).
Note also that `recoverable_category:delete` **is** a valid permission grant
(`permissions.go:164`) and can be assigned to a role from the Roles screen's
"Recoverables" row/"Cancel" column (`internal/app/permmap.go:106-117`), but no
route or handler ever checks it — it is a dead grant.
**Actual:** Matches. **Verdict:** PASS. (Dead-grant observation recorded as
F-E-03, informational.)

#### TC-E-008 — The pre-Phase-4 standalone `/recoverable-categories` screen is gone
**Traces to:** V4. **Type:** proof-of-absence. **Priority:** low.
**Steps:** `GET /recoverable-categories`; `POST /recoverable-categories`.
**Expected:** both 404 (Phase 4/D6 replaced the standalone page with the
Configuration fieldset; no route named `/recoverable-categories` is
registered at all, so even the catch-all's method-mismatch 405 does not
apply to the GET — the exact path segment is absent). Mirrors
`TestStandaloneRecoverableCategoriesScreenIsGone`.
**Actual:** Matches (404/404). **Verdict:** PASS.

#### TC-E-009 — Requester, Manager, Accounts refused `POST /configuration/recoverable-categories`; Admin succeeds
**Traces to:** V4, R6. **Type:** permission. **Priority:** high.
**Preconditions:** `asRole` subjects holding exactly Requester, Manager,
Accounts, Admin (no other roles — the `asRole` trap this suite avoids).
**Steps:** each subject `probePost`s `/configuration/recoverable-categories`
with a valid name and CSRF token.
**Expected:** Requester, Manager and Accounts → 403 (none of the three seeded
roles hold `recoverable_category:edit` — `migrations.go:352-395` lists their
full grants and it is absent from all three). Admin → 303 → `/configuration`
(holds every grant via `adminGrants()`, `migrations.go:404-412`).
**Actual:** Matches for all four. **Verdict:** PASS.

#### TC-E-010 — A newly admin-created category is never selectable on the real request form
**Traces to:** V4. **Type:** regression. **Priority:** medium.
**Steps:** Create category "Escrow probe «runId»" (Admin, via `/configuration`).
Sign in as a Requester and open `/requests/new?type=employee_advance`.
**Expected (written before running, from the spec's intent for V4 — "an admin
can add a category and have its rules enforced without a code change"):** the
new category appears as a `<option>` in the "Category" `<select>` so it can
actually be chosen.
**Actual:** it does **not**. The `<select id="rcategory">`
(`templates.go:1736-1744`) is six hardcoded `<option>` tags for exactly the
seed codes (`emd`,`pbg`,`icd`,`employee_advance`,`security_deposit`,`other`);
`ListRecoverableCategories` is never called anywhere in the request-form code
path (only `internal/app/configuration.go:98` and
`internal/app/recoverables.go:81` call it). An admin-added category is
enforceable (proven in TC-E-024–026 via `probePost`) but **unreachable by
anyone clicking through the product** — it can only be used by a caller who
already knows its code and can craft a raw POST.
**Verdict:** FAIL (product defect) — see F-E-02 (high).

#### TC-E-011 — A deactivated seed category is still offered on the request form and only refused after submit
**Traces to:** V4, V5. **Type:** regression. **Priority:** medium.
**Steps:** As Admin, deactivate "Other" on `/configuration`. Open
`/requests/new?type=employee_advance` and inspect the Category `<select>`.
**Expected (written before running):** since Phase 4's own comment says
"Only active categories are offered, so deactivating one stops new requests
naming it" (`recoverables.go:118`), the option should either be removed or
disabled.
**Actual:** "Other" is still a selectable, enabled `<option value="other">` —
the select is the same hardcoded six regardless of `active`. Selecting it and
submitting is refused only server-side, with the generic "choose a recoverable
category" message, which reads oddly next to a form that shows "Other"
plainly selected.
**Verdict:** FAIL (product defect) — same root cause as TC-E-010, folded into
F-E-02 (high).

---

### Section 2 — Per-category field rules (V5, V6), exhaustive

Every case in this section is driven through the real screen:
`/requests/new?type=employee_advance` (the only card that can carry
`treatment=recoverable` — see F-E-02 discussion above for why; `vendor_invoice`,
`vendor_advance` and `reimbursement` are hard-forced to `treatment=budget` by
`validateRequestInput`, `requests.go:194,210,223`). None of the recoverable
fields carry the HTML `required` attribute by design (`templates.go:1725-1727`
explains why — a `data-when`-hidden required field makes Chrome refuse to
submit at all), so every omission below is a genuine round trip to the server,
not a browser-blocked submission.

#### TC-E-012 / 013 — EMD requires a project
**Traces to:** V5. **Type:** negative / functional. **Priority:** high.
**Steps:** category = EMD, leave "Related project" unselected, fill return
date + terms + advance reason + purpose + amount + approver, submit.
**Expected:** refused, "this recoverable category always belongs to a
project" (`requests.go:184-186`). Repeating with a project selected succeeds
and lands on `/requests/{id}/submitted`.
**Actual:** matches both ways. **Verdict:** PASS / PASS.

#### TC-E-014 / 015 — PBG requires a project
**Traces to:** V5. Same shape as TC-E-012/013, category = PBG.
**Actual:** matches. **Verdict:** PASS / PASS.

#### TC-E-016 / 017 — ICD requires a counterparty
**Traces to:** V5. **Steps:** category = ICD, leave "Counterparty company"
blank vs. filled.
**Expected:** refused with "this recoverable category needs a counterparty
company" (`requests.go:187-189`) when blank; accepted when filled.
**Actual:** matches. **Verdict:** PASS / PASS.

#### TC-E-018 / 019 — Security deposit requires a counterparty (pins the plan-vs-code divergence)
**Traces to:** V5. **Priority:** high — this is the one the phase-4 plan text
gets wrong (spec table at `2026-07-25-phase-4-recoverables-spec.md:41` lists
`requires_counterparty=0` for Security deposit; the seed
(`recoverables.go:29`) and the Phase-2 rule map
(`requests.go:121`) both say `1`, and
`TestMigrationV6SeedsRecoverableCategories`/`TestSeededCategoriesMatchPhase2Rules`
pin it).
**Steps:** category = Security deposit, leave "Counterparty company" blank vs.
filled.
**Expected:** refused when blank, accepted when filled — same rule as ICD.
**Actual:** matches; the code, not the stale spec table, is what ships.
**Verdict:** PASS / PASS.

#### TC-E-020 — Employee advance needs neither; payee auto-records the requester
**Traces to:** V5, V6. **Type:** functional. **Priority:** high.
**Steps:** category = Employee advance (the default for this form type), no
project, no counterparty field is even rendered
(`templates.go:1751,1761` guard on `emd|pbg` / `icd|security_deposit`), fill
return date, terms, "what the money is for", purpose, amount, approver, submit.
**Expected:** accepted. Separately, `forcesRequesterPayee("employee_advance")`
(`requests.go:249-253`) is `true` regardless of treatment, so `VendorPayee` is
set to the requester's own name at `CreateRequest` (`requests.go:367-369`) —
**this is the payee, not the `Counterparty` field**; `RecoverableRow.Counterparty`
is left however the (absent, for this category) form field left it. The
Configuration screen's own hint text ("An employee advance fills the
counterparty in from the requester automatically") is therefore imprecise —
it is the *payee*, not `counterparty`, that is auto-filled. Verified by
opening `/requests/{id}` and reading the payee line.
**Actual:** request accepted; payee shows the requester's name; `Counterparty`
on `/recoverables/{id}` reads "Not recorded" (as expected, since nothing fills
it for this category).
**Verdict:** PASS (behaviour); the hint-text imprecision is recorded as F-E-04
(informational).

#### TC-E-021 — Other needs neither
**Traces to:** V5. Same shape, category = Other.
**Actual:** matches. **Verdict:** PASS.

#### TC-E-022 — Expected return date required for every category
**Traces to:** V6. **Type:** negative/boundary. **Priority:** high.
**Steps:** category = Other (simplest — no extra field), leave "Expected
return date" blank, fill everything else, submit.
**Expected:** refused, "expected return date is required for recoverables"
(`requests.go:178-180`, gated by `validDate`, so a malformed date is refused
identically).
**Actual:** matches. **Verdict:** PASS.

#### TC-E-023 — Repayment/refund terms required for every category
**Traces to:** V6. **Steps:** category = Other, leave "Repayment or refund
terms" blank (and, separately, whitespace-only), fill everything else.
**Expected:** both refused, "repayment or refund terms are required for
recoverables" (`requests.go:181-183`, `strings.TrimSpace`).
**Actual:** matches for both blank and whitespace-only.
**Verdict:** PASS.

#### TC-E-024 / 025 / 026 — A brand-new category's own rule is honoured (proves rules come from the table)
**Traces to:** V4, V5. **Type:** functional. **Priority:** high.
**Preconditions:** Admin creates "Retention combo «runId»" with "Must also
capture" = "Related project and counterparty company" (`requires=both` →
`requires_project=1, requires_counterparty=1`, `configuration.go:113-127`).
Since this category cannot be reached through the request-form `<select>`
(F-E-02), all three sub-cases use `probePost /requests` with a manager id read
live from a real approver `<select>` so no id is hardcoded. `type=employee_advance`
is used rather than the fifth, UI-unreachable `type=recoverable`
(`requests.go:78-81,240-244`): a *rejected* `type=recoverable` submission is
itself masked — `renderRejectedRequestForm` (`requests.go:180-184`) looks up
`requestTypeLabels[in.Type]` before it can re-render the form, that map only
has the four chooser types, and the miss produces the unrelated 400 "That is
not a kind of request this system raises." instead of the real validation
message this test needs to read. Discovered while running this exact test
(first attempt failed with that message instead of the expected one) —
recorded as F-E-08 (low). `type=employee_advance` sidesteps it (it *is* in
that map) while still exercising `treatment=recoverable` and the new
category's rules identically; it needs one extra field, `advance_reason`.
**Steps/Expected:** (024) omit `project_id` → refused, "this recoverable
category always belongs to a project". (025) supply `project_id`, omit
`counterparty` → refused, "this recoverable category needs a counterparty
company". (026) supply both → accepted, and the resulting request's
`recoverable_category_id` resolves to the new row (checked via
`/recoverables/{id}` showing "Retention combo «runId»" as the Category).
**Actual:** matches in all three sub-cases — this is the strongest possible
proof that `validateRequestInput`'s rule set is genuinely read from
`recoverable_categories` (`recoverableRules`, `recoverables.go:119-129`) and
not from the old hardcoded `recoverableCategoryRules` map, because this
category was never in that map.
**Verdict:** PASS / PASS / PASS.

---

### Section 3 — Exclusion from budget actuals (V2, V3), before/after

#### TC-E-027 — Baseline: grid and per-head report agree before any change
**Traces to:** V2, V3. **Type:** functional. **Priority:** high.
**Note on route choice:** `/reports/monthly` aggregates to one company-wide
row per month with no project/head breakdown
(`templates.go:3273`, `{{if ne .Mode "monthly"}}<th>Project</th>{{end}}`), so
a *per-head* figure is read from `/reports/heads` — the sibling tab in the
same Reports area and the same `report:view` gate — which is what this test
does; `/reports/monthly`'s own company-wide total is also read as a second,
coarser corroboration.
**Steps:** for the current month, read the Actual cell for Project =
"Operations", Head = "Office Rent" on `/grid` and on `/reports/heads`.
**Expected:** the two figures are numerically identical — both derive from
the same `Store.Grid`/`Store.Report` actual-sum expression
(`store.go:1380-1478`), so they can never legitimately disagree.
**Actual:** matches (both read the same paise value once parsed). **Verdict:** PASS.

#### TC-E-028 — A settled budget payment raises the grid actual by exactly the paid amount
**Traces to:** V2. **Type:** functional. **Priority:** high.
**Steps:** using `createApprovedRequest`/`settlePayment` (fixtures.ts) raise,
approve and settle a `vendor_invoice` request for ₹700.00 against
Operations/Office Rent, paid today. Re-read the grid Actual for that head.
**Expected:** new Actual = baseline (TC-E-027) + ₹700.00 exactly — the
non-recoverable branch of the `CASE` in `Grid`'s SQL
(`store.go:1387-1388`) is unconditional for a `treatment='budget'` payment.
**Actual:** matches exactly (delta = 70000 paise). **Verdict:** PASS.

#### TC-E-029 — The same rise is reflected in the per-head report and both CSV exports
**Traces to:** V2, D3. **Type:** functional. **Priority:** high.
**Steps:** re-read `/reports/heads` for the same head; fetch
`/export.csv?month=<M>` and `/reports/ytd.csv?from=<M>&to=<M>` and locate the
Operations/Office Rent row in each.
**Expected:** all three agree with the on-screen grid figure from TC-E-028 —
`Report` iterates `Grid` per month (`Report`'s doc comment,
`recoverables.go` §5 of the phase-4 spec) so it inherits the same numbers, and
`exportGrid`/`exportYTD` (`app.go:1440-1497`) format directly from `Grid`/
`Report` with no separate arithmetic.
**Actual:** matches across all three sources. **Verdict:** PASS.

#### TC-E-030 — A recoverable request raised through the real screens can be reserved, paid and closed
**Traces to:** V2, V3, V7. **Type:** functional. **Priority:** critical.
**This test asserts the *correct*, intended behaviour and is wrapped in
`test.fail()`, per the harness's convention for a confirmed, deterministic
product defect** — the day this is fixed, the assertion starts passing and
Playwright reports "passed unexpectedly", which is the signal to remove the
`test.fail()` wrapper.
**Steps:** for five categories spanning every rule shape — EMD (project), ICD
(counterparty), Security deposit (counterparty), Employee advance (neither),
Other (neither) — raise the recoverable request through
`/requests/new?type=employee_advance`, approve it, reserve it
(`POST /requests/{id}/record-payment`, the same route "Take for processing"
uses) and post the settlement to `/payments` with exactly the fields the real
confirmation sheet sends.
**Expected:** every one of the five settles and answers 302/303 → `/payments/{id}`,
exactly like a budget request — the rule a category enforces at *creation*
(project vs counterparty vs neither) should have no bearing on whether its
payment can be *recorded*.
**Actual:** all five answer **400**, body `validation failed: valid head,
date, and positive amount are required`:
`{"emd":400,"icd":400,"security_deposit":400,"employee_advance":400,"other":400}`.
Root cause: the recoverable fieldset never renders a head control for any of
its six categories (`templates.go:1731-1781`); `paymentEntry` therefore
computes `SelectedHeadID = 0` whenever `req.HeadID == nil`
(`linking.go:242-244`), which every recoverable request's `HeadID` always is
regardless of category (`needsRecoverable()` never touches `HeadID`,
`requests.go:173-190`); and `validatePayment` refuses `HeadID == 0`
(`store.go:1620-1623`). **No recoverable request raised through the product,
in any category, can ever be settled.**
**Verdict:** FAIL (product defect) — F-E-01, critical. Reproduced three ways:
driving the full UI flow through reservation and settlement for all five
categories (this test), an earlier manual Playwright probe against a live
server that captured the exact `alert.error`/`banner.bad` text, and static
analysis of the three functions cited above.

#### TC-E-031 — Grid/report stay unchanged after a *paid* recoverable — BLOCKED by F-E-01
**Traces to:** V2, V3. **Type:** functional. **Priority:** high.
**Status: BLOCKED.** This is the "sharpest test" the brief asks for — raise
and settle a recoverable against the same project/head as TC-E-028 and prove
the grid/report actuals are *unchanged* — but it cannot be produced end to end
through the product while F-E-01 stands: there is no way to reach a state
where a recoverable payment exists at all through the UI. The Playwright
counterpart is `test.fixme()`, citing F-E-01, so it neither reports a false
pass nor silently disappears.
**What is verified instead:** the SQL that would enforce this is read
directly (`store.go:1387-1398`, filtering `COALESCE(pr.treatment,'') <>
'recoverable'` in both the actual-sum `CASE` and the "head has activity"
`EXISTS`) and is exercised by two Go store tests that seed the payment
directly (bypassing the broken form path):
`TestGridAndReportExcludeRecoverablePayments` and
`TestRecoverablePaymentExcludedFromActualsButInRecoverableReport`
(`internal/store/recoverables_test.go:470,516`). Both assert the exact
delta-zero property this test would assert. That is unit-level, not
end-to-end, evidence — it proves the logic is right, not that a user can ever
reach it.
**Verdict:** BLOCKED (reason: F-E-01).

#### TC-E-032 — An approved-but-unpaid recoverable contributes nothing to grid or report
**Traces to:** V2. **Type:** functional. **Priority:** high.
**Steps:** raise and approve (do not attempt to pay) an EMD recoverable
against Operations/Office Rent. Re-read the grid/report Actual for that head.
**Expected:** unchanged from the post-TC-E-028 figure — an approved-but-unpaid
request has no payment row at all, so it cannot appear in any actual sum
regardless of treatment.
**Actual:** matches (delta = 0). **Verdict:** PASS. (Necessarily a weaker
proof than TC-E-031 would have been — see that entry.)

#### TC-E-033 — That unpaid recoverable is present in the register and the dashboard's outstanding totals
**Traces to:** V3. **Type:** functional. **Priority:** high.
**Steps:** open `/recoverables/list` and `/recoverables`.
**Expected:** the request from TC-E-032 appears in the list (status
"Awaiting payment") and is counted in the dashboard's "Outstanding"
amount/count (`RecoverableMetrics` counts every live recoverable, paid or not
— `recoverables.go:391-410`).
**Actual:** matches. **Verdict:** PASS.

#### TC-E-034 — `/recoverables/list.csv` total agrees with the on-screen list total
**Traces to:** V3, D3. **Type:** functional. **Priority:** medium.
**Steps:** read the `<tfoot>` total on `/recoverables/list`; fetch
`/recoverables/list.csv` with the same filters and sum the Amount column.
**Expected:** equal — both are `sum(RecoverableReport(...).Amount)` over the
same rows (`recoverables.go:87-89`, `exportRecoverable`,
`app.go`/`recoverables.go:97-128`).
**Actual:** matches. **Verdict:** PASS.

---

### Section 4 — Register, dashboard, detail

#### TC-E-035 — Dashboard renders its metric strip, rollups and the two explanatory banners
**Traces to:** V2, V3. **Type:** functional. **Priority:** medium.
**Steps:** open `/recoverables` as Admin/Accounts.
**Expected:** "Kept out of budget actuals on purpose" banner (brand),
"Tracking the money coming back is not in this version" banner (locked),
four `.metric` tiles (Outstanding, Past expected return, Due in 30 days, Paid
out this month), a By-category and a By-counterparty table
(`templates.go:3429-3493`).
**Actual:** matches. **Verdict:** PASS.

#### TC-E-036 — An unpaid recoverable never reads as overdue, however old its expected return date
**Traces to:** V3. **Type:** boundary. **Priority:** high.
**Steps:** raise and approve (do not pay) a recoverable with expected return
date far in the past (e.g. 2020-01-01). Open `/recoverables/list`.
**Expected:** its ageing pill reads "Awaiting payment" with tone `approved`,
never "N days overdue"/tone `bad` — `recoverableAgeing` checks `paidOn == ""`
**before** comparing dates (`recoverables.go:264-279`): "money that never
left cannot be overdue" is enforced by evaluation order, not a separate flag.
**Actual:** matches exactly. **Verdict:** PASS.

While writing this case, reading `RecoverableMetrics`'s SQL
(`recoverables.go:391-406`) showed its `OverdueAmount`/`OverdueCount` sum by
`expected_return_date < today` over the whole live population, with **no**
`paid_on <> ''` guard — unlike `recoverableAgeing`, which checks `paidOn == ""`
**first** (`recoverables.go:264-266`). That means the very row this test just
proved reads "Awaiting payment" at the row level should still be counted as
overdue in the dashboard's own "Past expected return" tile. That claim gets
its own pinned test rather than being folded in here, so a fix to one is never
mistaken for a fix to the other — see TC-E-036b.

#### TC-E-036b — The dashboard's "Past expected return" tile applies the same "unpaid can't be overdue" rule as the row
**Traces to:** V3. **Type:** functional. **Priority:** medium.
**This test asserts the *correct* behaviour and is wrapped in `test.fail()`**,
per the same convention as TC-E-030.
**Steps:** read the dashboard's "Past expected return" tile count; raise and
approve (do not pay) another far-past-due "Other" recoverable; re-read the
tile.
**Expected:** the count is unchanged — an unpaid recoverable is not overdue by
the same reasoning TC-E-036 already established at the row level, so the
aggregate that summarises those rows should agree with them.
**Actual:** the count rises by exactly 1. Root cause quoted above under
TC-E-036: `recoverables.go:391-406` has no `paid_on` guard.
**Verdict:** FAIL (product defect) — F-E-05, medium. Reproduced twice: live,
via this test, and by reading the SQL directly.

#### TC-E-037 — `/recoverables/list.csv` reaches the export handler, not the `{id}` detail handler
**Traces to:** V3. **Type:** regression. **Priority:** high.
**Steps:** `GET /recoverables/list.csv` with no filters.
**Expected:** 200, `Content-Type: text/csv`, body starts with the CSV header
row — Go 1.22+'s `ServeMux` prefers the more specific literal pattern over
`{id}` (`app.go:400-402` comment), so this must never fall through to
`recoverableDetail`'s "not a recoverable payment" 404.
**Actual:** matches. **Verdict:** PASS.

#### TC-E-038 — `/recoverables/{id}` for a non-recoverable request answers 404
**Traces to:** V3. **Type:** negative. **Priority:** medium.
**Steps:** create an ordinary budget `vendor_invoice` request; `GET
/recoverables/{that id}`.
**Expected:** 404, "That request is not a recoverable payment."
(`recoverables.go:138-141`).
**Actual:** matches. **Verdict:** PASS.

#### TC-E-039 — `ageing=unpaid` narrows the register correctly
**Traces to:** V3. **Type:** functional. **Priority:** low.
**Steps:** with at least one unpaid and one (pre-existing, from other runs)
row present, load `/recoverables/list?ageing=unpaid`.
**Expected:** every visible row has "Paid on" = "Not yet paid"
(`recoverables.go:334-336`, `py.id IS NULL`).
**Actual:** matches. **Verdict:** PASS.

#### TC-E-040 — Dashboard Outstanding metric and category rollup agree with the list's own footer total
**Traces to:** V3. **Type:** functional. **Priority:** medium.
**Steps:** compare `/recoverables`'s Outstanding amount/count and its
By-category table's row for "EMD" against `/recoverables/list`'s own totals
and category-filtered footer.
**Expected:** equal — `RecoverableMetrics`, `RecoverableRollups` and
`RecoverableReport` all share `recoverableBaseWhere`/`recoverableAmount`
(`recoverables.go:250-258`), so they cannot legitimately drift.
**Actual:** matches. **Verdict:** PASS.

#### TC-E-041 — `.metric-foot` now carries a real CSS rule
**Traces to:** — (design-system hygiene, flagged by the brief). **Type:**
regression. **Priority:** low.
**Steps:** on `/recoverables`, read the computed style of a `.metric-foot`
element (e.g. the outstanding-count caption).
**Expected (per PROGRESS.md's "Known gaps" section, which is what the brief
points at):** unstyled — `color`/`font-size` at browser defaults.
**Actual:** `web/static/fervid-ds.css:367-373` **does** define `.metric-foot`
(`display:block; margin-top:4px; color:var(--fb-muted); font-size:11.5px;
line-height:1.3`), and the computed style in the browser matches (11.5px, a
muted colour, not the default black/16px). Git history confirms the fix
landed in commit `a80e18f` ("fix(css): add the four rules the mockups use but
Phase 0 never delivered"), which post-dates the "Known gaps" section in
`docs/superpowers/PROGRESS.md:368-375` — that section is now **stale
documentation**, not a live defect.
**Verdict:** PASS (the CSS rule exists and applies); the stale doc note is
recorded as F-E-06 (informational, doc-only).

---

### Section 5 — Close on payment (V7)

#### TC-E-042 — Category and expected-return survive settlement — BLOCKED by F-E-01
**Traces to:** V7. **Type:** regression. **Priority:** high.
**Status: BLOCKED**, for the same reason as TC-E-031: settling any recoverable
request through the product is impossible while F-E-01 stands. Playwright
counterpart is `test.fixme()`.
**What is verified instead:** `TestRecoverableRequestClosesOnPaymentRetainingClassification`
(`internal/store/recoverables_test.go:793-844`) inserts a `processing`
recoverable request directly (with a real `head_id`, sidestepping the form) and
calls `RecordPaymentForRequest` directly, then asserts `status='completed'`,
`treatment='recoverable'`, category id, expected-return date and repayment
notes are all retained, and that the grid actual is still 0. That test passes
today — the retention *logic* is correct — but, as with TC-E-031, no user can
reach the state it starts from.
**Verdict:** BLOCKED (reason: F-E-01).

#### TC-E-043 — Before settlement, the detail screen already shows category and expected return correctly
**Traces to:** V6, V7. **Type:** functional. **Priority:** medium.
**Steps:** raise and approve an ICD recoverable (counterparty supplied); open
`/recoverables/{id}`.
**Expected:** "Recoverable details" card shows Category = "ICD", Counterparty
= the supplied company, Expected return = the supplied date, Refund terms =
the supplied text, and the Payment card reads "Approved but not yet paid. The
money has not left, so nothing is outstanding against a counterparty yet."
(`templates.go:3606-3608`).
**Actual:** matches. **Verdict:** PASS.

---

### Section 6 — Linked project (V8)

#### TC-E-044 — An EMD linked to a real project shows in that project's register row while absent from its grid actuals
**Traces to:** V8, V2. **Type:** functional. **Priority:** high.
**Steps:** raise and approve an EMD recoverable against project "Operations"
(no head — recoverables never carry one, see F-E-01). Open
`/recoverables/list`; open `/grid` for Operations' heads.
**Expected:** the register row shows Project = "Operations"
(`RecoverableReport`'s `LEFT JOIN projects` — `recoverables.go:296`,
`r.Project`); none of Operations' head rows in `/grid` change, because no
payment exists yet for this request (approved-but-unpaid, same caveat as
TC-E-032/044 — a genuinely *paid* linked recoverable cannot be produced while
F-E-01 stands).
**Actual:** matches. **Verdict:** PASS.

#### TC-E-045 — A recoverable with no project reads "Not project linked" everywhere
**Traces to:** V8. **Type:** functional. **Priority:** low.
**Steps:** raise and approve an ICD recoverable (ICD never requires a
project). Open `/recoverables/list` and `/recoverables/{id}`.
**Expected:** list shows "Not project linked" (`templates.go:3541`); detail
shows "Not project linked" for "Related project" (`templates.go:3588`).
**Actual:** matches. **Verdict:** PASS.

---

### Section 7 — Proof of absence (X2, X4)

#### TC-E-046 / 047 — No recoverable repayment route
**Traces to:** X2. **Type:** proof-of-absence. **Priority:** high.
**Steps:** `GET` and `POST` `/recoverables/1/repay`, `/recoverables/list/repay`,
`/configuration/recoverable-categories/1/repay`.
**Expected:** GET → 404 for all three (unregistered paths). POST → 404 or 405
(the catch-all `GET /` matches path, not method, for anything under
`/recoverables/1/...` that happens to collide with a registered prefix; none
of these three do, so 404 is actually what all three POSTs return too — both
are accepted per the harness convention).
**Actual:** all 404. **Verdict:** PASS / PASS.

#### TC-E-048 / 049 — No forfeiture/write-off route
**Traces to:** X4. Same shape, paths `/recoverables/1/forfeit`,
`/recoverables/1/write-off`, `/recoverables/list/forfeit`.
**Actual:** all 404. **Verdict:** PASS / PASS.

#### TC-E-050 — No repayment or forfeiture control appears in any recoverable screen's markup
**Traces to:** X2, X4. **Type:** proof-of-absence. **Priority:** medium.
**Steps:** search the rendered HTML of `/recoverables`, `/recoverables/list`
and `/recoverables/{id}` for any `<button>`, `<a class="btn"...>` or `<form
action=...>` whose visible text or action matches
`/repay|forfeit|write.?off/i`.
**Expected:** none — the only occurrences of those words anywhere in
`internal/app/templates.go` are descriptive/disclaiming prose: the dashboard's
own "locked" banner ("Repayments, forfeitures, and converting a lost deposit
into an expense are deliberately out of scope", `templates.go:3490`), the
detail screen's overdue-banner copy ("chasing the money... happen outside this
version", `templates.go:3577`) and its action-bar note ("Recording the refund
is out of scope for this version", `templates.go:3636`) — none inside an
actionable element.
**Actual:** matches; zero actionable matches, three descriptive matches (all
accounted for above). **Verdict:** PASS.

---

### Section 8 — Permissions

#### TC-E-051 — Requester refused `recoverable_report:view` on all three view routes
**Traces to:** R6. **Type:** permission. **Priority:** high.
**Steps:** `asRole(['Requester'])`; `probeGet` `/recoverables`,
`/recoverables/list`, `/recoverables/{a real id}`.
**Expected:** 403 on all three — Requester's grants
(`migrations.go:352-364`) include no `recoverable_report` resource at all.
**Actual:** matches. **Verdict:** PASS.

#### TC-E-052 — Manager refused `recoverable_report:view` on all three view routes
**Traces to:** R6. Same shape, `asRole(['Manager'])`.
**Expected:** 403 — Manager's grants (`migrations.go:365-378`) hold `grid:view`
and `report:view` but no `recoverable_report` resource.
**Actual:** matches. **Verdict:** PASS.

#### TC-E-053 — Accounts granted `recoverable_report:view` on all three view routes
**Traces to:** R6. `asRole(['Accounts'])`.
**Expected:** 200 on all three (`migrations.go:392`:
`{"recoverable_report","view"}`).
**Actual:** matches. **Verdict:** PASS.

#### TC-E-054 — Requester/Manager refused `recoverable_report:export`; Accounts granted it
**Traces to:** R6, D3. **Priority:** high.
**Steps:** `probeGet /recoverables/list.csv` as Requester, Manager, Accounts.
**Expected:** 403, 403, 200 — only Accounts and Admin hold `recoverable_report:export`
(`migrations.go:392`, `adminGrants()`).
**Actual:** matches. **Verdict:** PASS.

#### TC-E-055 — Admin holds every recoverable verb; anonymous refused all four routes
**Traces to:** R6. **Steps:** `asRole(['Admin'])` on all four routes (200
each); `probeAnonymous` on all four (303 → `/login`).
**Actual:** matches. **Verdict:** PASS.
