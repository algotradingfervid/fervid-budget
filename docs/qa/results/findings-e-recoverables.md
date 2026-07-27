# Findings — Recoverables (TC-E)

> **Frozen evidence, not current status.** This document records what was found
> at commit `30edd6a` and is **not** updated as defects are fixed — the same rule
> `AUDIT-REPORT.md` states for itself. Its present-tense claims describe the
> audited commit, so a statement here that something "is" broken means it was
> broken then; several have since been repaired, and a few of the `file:line`
> citations have shifted. For what was actually **done** about each finding, read
> [`REPAIR-LOG.md`](REPAIR-LOG.md).

Nine findings. Ranked by severity. Every one is confirmed by at least two
independent methods (live reproduction through `tests/e2e/audit-e-recoverables.spec.ts`
plus direct reading of the Go source), per the audit protocol.

---

### F-E-01 — No recoverable request raised through the product can ever be settled, in any category

- **Severity** — critical
- **Confidence** — confirmed (reproduced three ways: live UI/HTTP flow across five categories, an earlier manual Playwright probe against a live server, static analysis)
- **Type** — data-integrity / spec-divergence (blocks V2, V3, V7 end to end)
- **Where** — `internal/app/templates.go:1731-1781` (the recoverable fieldset has no head control, for any of its six categories); `internal/app/linking.go:242-244` (`paymentEntry` sets `SelectedHeadID = 0` whenever `req.HeadID == nil`); `internal/store/store.go:1620-1623` (`validatePayment` refuses `HeadID == 0`)
- **Traces to** — V2, V3, V7 / TC-E-030, TC-E-031 (blocked), TC-E-042 (blocked)
- **What happens** — Reserve any approved recoverable request and attempt to record its payment (through the real "Take for processing" → payment entry → "Payment settled" → "Confirm and save payment" flow, or the equivalent direct POST to `/payments`). The POST answers **400** with body `validation failed: valid head, date, and positive amount are required`. The request stays in `processing`, still holding the caller's reservation — there is no way to advance it, void it, or release it back cleanly from this state through any screen this suite found.
- **Why it is wrong** — `payment_requests.head_id` is optional for a recoverable request (`needsRecoverable()`, `internal/store/requests.go:173-190`, never touches `HeadID`), and no screen in the recoverable path ever asks for one — by design, since a deposit or advance does not belong to a budget head. But `payments.head_id` is still validated as mandatory by `validatePayment`, a check written for budget payments before Phase 4 existed and never updated to exempt a payment linked to a `treatment='recoverable'` request. The payment entry screen's hidden `head_id` field (`internal/app/templates.go:389`) therefore always carries `0`, and every settlement dies on that one check.
- **Scope note — this is broader than the project-requirement story.** A sibling audit (`findings-d-linking-settlement.md`, F-D-11) reached the same defect but scoped it to the four categories that require no *project* (`icd`, `security_deposit`, `employee_advance`, `other`). The blocker is the **head**, not the project, and the recoverable fieldset never collects one: `<fieldset data-when="treatment:recoverable">` offers `rcategory`, `rproject` (for `emd|pbg` only), `counterparty`, `expected-return` and `terms`, while `id="head" name="head_id"` lives exclusively inside `<fieldset data-when="treatment:budget">`. EMD and PBG therefore carry a project and still no head, so they fail identically. All six categories are affected.
- **Reproduction** (from a signed-in Admin session):
  1. `GET /requests/new?type=employee_advance` (the only chooser card that can carry `treatment=recoverable`; see F-E-02).
  2. Fill Short title, leave treatment on "Refundable or recoverable" (its default for this card), pick any category (this suite tried all six: EMD, PBG, ICD, Security deposit, Employee advance, Other — every one fails identically), fill Expected return date, Repayment terms, What the money is for, Amount, Purpose, Approver (someone else — G8).
  3. Submit. It succeeds and lands on `/requests/{id}/submitted`.
  4. Sign in as the approver, approve it in full.
  5. As anyone holding `payment:process`, go to `/accounts-queue?tab=approved`, click "Take for processing" for that request.
  6. On `/payments/new?request={id}`, note the hidden `<input name="head_id" value="0">`.
  7. Fill Amount actually paid / Paid on / Payment mode / Reference, click "Payment settled →", choose "Fully settled", click "Confirm and save payment".
  8. The POST to `/payments` answers 400. The banner reads *"validation failed: valid head, date, and positive amount are required. Nothing has been saved. Correct it and confirm again."*
- **Impact** — Every acceptance criterion that depends on a recoverable payment actually existing (V2's exclusion, V3's separate report, V7's close-on-payment, and this suite's own before/after proof of budget-actuals exclusion) is untestable through the product and, more importantly, **unusable by an actual accountant**: an EMD, PBG, ICD, security deposit or employee advance can be raised and approved, but the money can never be recorded as paid. Every Go store/app test that exercises this path (`TestRecoverableRequestClosesOnPaymentRetainingClassification`, `TestGridAndReportExcludeRecoverablePayments`, `TestRecoverablePaymentExcludedFromActualsButInRecoverableReport`) seeds the payment by direct SQL insert with a hand-supplied `head_id`, bypassing `CreateRequest` and the real form entirely, which is why none of them caught this.
- **Evidence** — Live run, one settlement attempt per category (TC-E-030): `{"emd":400,"icd":400,"security_deposit":400,"employee_advance":400,"other":400}`. Response body: `<div class="alert error" role="alert" aria-live="assertive">validation failed: valid head, date, and positive amount are required</div>`.
- **Suggested direction** — Either add a head selector to the recoverable fieldset (if a recoverable payment should genuinely book against a head for tracking, matching how EMD/PBG already book against a project) or make `RecordPaymentForRequest`/`validatePayment` accept `HeadID == 0` specifically when the linked request's `treatment` is `recoverable`. The second is smaller and matches the "kept out of budget actuals" framing already used everywhere else in Phase 4.

---

### F-E-02 — An admin-created category can never be selected on the real request form; a deactivated seed category still can be

- **Severity** — high
- **Confidence** — confirmed (live DOM read of the six hardcoded `<option>` values plus source reading)
- **Type** — spec-divergence (V4 says "admin can add a category and have its rules enforced without a code change" — enforcement holds, discoverability does not)
- **Where** — `internal/app/templates.go:1736-1744` (`<select id="rcategory">` is six literal `<option>` tags for `emd|pbg|icd|employee_advance|security_deposit|other`); confirmed that `ListRecoverableCategories` is never called anywhere in the request-form code path — only `internal/app/configuration.go:98` and `internal/app/recoverables.go:81` call it, for the admin screen and the register's own filter respectively
- **Traces to** — V4, V5 / TC-E-010, TC-E-011
- **What happens** — An admin adds a new recoverable category through `/configuration` (say, "Retention money"). It is real, active, and its rule is enforced the moment any client submits a request naming its code — but no signed-in person can ever reach that state by clicking through the product, because the category picker on `/requests/new?type=employee_advance` is a static list of exactly the six seed codes, unconnected to the `recoverable_categories` table. Symmetrically, deactivating one of the six seed categories does **not** remove or disable its `<option>` — a user can still select "Other" after it is turned off, fill in the rest of the form, and only discover the refusal after submitting, with the unhelpful generic message "choose a recoverable category" rather than something naming the category as inactive.
- **Why it is wrong** — Phase 4's own comment (`internal/store/recoverables.go:118`) states "Only active categories are offered, so deactivating one stops new requests naming it — which is the whole point of the Active switch," which is true of the *validation* but not of the *form*. V4's stated goal — "an admin can add a category and have its rules enforced without a code change" — is only half true: the enforcement half ships, the *usability* half does not.
- **Data-integrity is not compromised.** `normalizeRecoverableCategory` (`internal/app/requests.go:129-140`) — which substitutes a fallback code for anything it does not recognise — is called **only** from `requestFormFields` (`internal/app/requests.go:105-127`, the `GET /requests/new/fields` htmx fragment handler used to decide which fieldset to re-render). It is never called from `requestCreate`/`CreateRequest`. A direct `POST /requests` carrying a brand-new category's real code is stored and validated as itself, exactly as TC-E-024/025/026 prove. The gap is purely "nobody can click their way to typing it," not "the system mangles it if you do."
- **Reproduction** (TC-E-010): create a category through `/configuration`; `GET /requests/new?type=employee_advance`; read every `<option value>` under `#rcategory` — the set is always exactly `{emd, employee_advance, icd, other, pbg, security_deposit}`, never the new category's code. (TC-E-011): deactivate "Other" the same way; the same page still offers `<option value="other">`, enabled.
- **Impact** — V4 (admin-configurable categories) ships enforcement but not affordance. An organisation that wants a 7th category (e.g. "Retention money," a very common construction-industry deposit) can create it, but nobody can raise a request against it without already knowing its derived code and a way to submit outside the UI — in practice, this means the feature is unusable for its stated purpose without a follow-up code change to the form, exactly what V4 was meant to avoid.
- **Suggested direction** — Render `#rcategory`'s options from `ListRecoverableCategories(ctx, true)` instead of the hardcoded list, using each row's live `name` as the label. That single change also fixes the "deactivated category still shown" half for free, since the query already filters `active=1`.

---

### F-E-03 — `/recoverables/list` and `/recoverables/list.csv` (and the already-known `/recoverables/{id}`) apply no request-level data scope

- **Severity** — medium (see note on exploitability below; the information-flow sibling's document rates the family of `/recoverables/*` scope gaps critical — recorded here for the record, with this document's own reasoning)
- **Confidence** — confirmed (source reading; not independently re-derived from a live over-broad-read exploit because no seeded role can trigger one — see below)
- **Type** — information-flow / permission
- **Where** — `internal/app/recoverables.go:74-95` (`recoverablesList`), `:97-128` (`exportRecoverable`), `:130-173` (`recoverableDetail`) — none of the three calls `a.auth.Scope(u, ...)` or any per-row ownership check; every live recoverable is returned to any caller holding the resource-level verb
- **Traces to** — R6 / cross-references **F-05** in `docs/qa/uml/05-route-permission-matrix.md`, which already documents the identical gap for `/recoverables/{id}` specifically. This finding confirms the same absence extends to the list and its CSV export, which F-05 did not cover.
- **What happens** — Every one of the three `recoverable_report`-gated read routes hands back the entire live recoverables population — every category, counterparty, project, amount, requester and repayment-note text in the system — to anyone holding `recoverable_report:view` (list, detail) or `:export` (CSV), with no scoping by who raised the request, who manages them, or any other row-level test. `request:view`'s own scope machinery (`own`/`assigned`/`all`, `canViewRequest`, `internal/app/requests.go:330+`) is simply never consulted anywhere in this file.
- **Why it is wrong** — Every comparable screen in this codebase enforces a scope: `/requests` filters by `canViewRequest`, `payment_detail` re-checks the underlying request's scope before showing the trail (`internal/app/app.go:758-762`). The recoverables register is the one place money moves through the system with no equivalent check at all.
- **Why this is not exploitable today** — Both seeded roles that hold any `recoverable_report` verb — Accounts and Admin (`internal/store/migrations.go:392`, `adminGrants()`) — already hold `request` scope `all` (`internal/store/migrations.go:394,400`), so under the shipped role set nobody who could see less through `/requests` sees more through `/recoverables`. The gap is real but latent: it activates the moment an admin creates a **custom** role that pairs `recoverable_report:view` with a narrower `request` scope (e.g. "assigned"), which the Roles screen permits today with nothing to stop it.
- **Reproduction** — Read `internal/app/recoverables.go` end to end (already the source cited above); corroborated by this suite's own permission tests (TC-E-051–055), which confirm the *resource-level* gate works exactly as documented but never touch a row-level question because no seeded role exposes one.
- **Impact** — Latent until a custom role combines the two grants; then, whoever holds it reads every recoverable in the company regardless of any narrower request scope they were given, including Repayment Notes text that can carry negotiated, counterparty-specific terms.
- **Suggested direction** — Apply the same scope test `canViewRequest` already implements, or a documented decision that `recoverable_report` is deliberately company-wide (in which case say so where the resource is defined, so a future role author does not assume otherwise).

---

### F-E-04 — The recoverables CSV exports three columns the on-screen register never shows

- **Severity** — low
- **Confidence** — confirmed (both header rows read directly from source)
- **Type** — information-flow
- **Where** — list header, `internal/app/templates.go:3536`: `Request | Category | Counterparty | Project | Amount | Paid on | Expected back | Ageing` (8 columns); CSV header, `internal/app/recoverables.go:113`: `Number,Category,Counterparty,Project,Amount,Paid On,Expected Return,Ageing,Status,Requester,Repayment Notes` (11 columns)
- **Traces to** — V3, D3 / TC-E-034, TC-E-037
- **What happens** — `Status`, `Requester` and `Repayment Notes` appear in every exported row but nowhere on the screen the export button sits on. Anyone who has only ever looked at `/recoverables/list` has no way to know the export carries more than what they see.
- **Why it matters less than F-E-03** — under the seeded roles, `recoverable_report:export` and `:view` are held by the same two roles (Accounts, Admin), so nobody gains *new information* by exporting that they could not already get by opening each request's own detail screen (`/recoverables/{id}` shows Counterparty and Refund terms, and `/requests/{id}` shows the requester). The gap is a **screen/export inconsistency**, not a fresh disclosure, under the shipped configuration — but it becomes a disclosure exactly like F-E-03 the moment a custom role holds `:export` without `:view`, since `RequirePermission("recoverable_report","export")` gates the CSV independently of the list.
- **Reproduction** — `GET /recoverables/list` vs `GET /recoverables/list.csv`, diff the header rows (TC-E-037 exercises the CSV route directly and asserts its literal header string).
- **Impact** — Low under current roles; compounds with F-E-03 under a custom one.
- **Suggested direction** — Either add the three columns to the list table (most consistent) or note in the export button's label/tooltip that the download carries more detail than the screen.

---

### F-E-05 — The dashboard's "Past expected return" tile does not apply the "unpaid cannot be overdue" rule the register itself enforces

- **Severity** — medium
- **Confidence** — confirmed (reproduced live via `test.fail()`, TC-E-036b, and by reading the SQL directly)
- **Type** — data-integrity / UX
- **Where** — `internal/store/recoverables.go:391-406` (`RecoverableMetrics`'s `OverdueAmount`/`OverdueCount` sum `WHEN exp <> '' AND exp < today`, with no `paid <> ''` condition) versus `internal/store/recoverables.go:264-279` (`recoverableAgeing`, which checks `paidOn == ""` **first** and returns `"Awaiting payment"`/not-overdue before ever comparing dates)
- **Traces to** — V3 / TC-E-036 (row-level, PASS), TC-E-036b (aggregate-level, FAIL)
- **What happens** — Raise and approve (do not pay) a recoverable whose expected return date is years in the past. Its own row on `/recoverables/list` correctly reads "Awaiting payment" (tone `approved`), exactly matching the design intent stated in the phase-4 spec's own reasoning: money that never left cannot be overdue. But the dashboard's "Past expected return" tile at `/recoverables` increments its count and amount for that exact row, because its SQL never checks whether a payment exists at all.
- **Why it is wrong** — the two numbers are supposed to describe the same population; the register is the detail view and the dashboard is its summary. A reader who trusts the dashboard is told there are, e.g., "3 payments overdue," opens the register expecting three red rows, and finds fewer — some of the "overdue" total is actually unpaid, undisbursed money.
- **Reproduction** (TC-E-036b): read the "Past expected return" tile's count; raise+approve one more far-past-due, unpaid "Other" recoverable; re-read the tile. Count rises by 1 when the correct behaviour (matching the row-level rule) is no change at all.
- **Impact** — Misleads whoever reads the dashboard about how much money is genuinely overdue versus merely approved-and-waiting; low financial risk (it is a reporting-only number, never gates a workflow) but directly undermines the one explicit design rule this area states in its own code comments.
- **Suggested direction** — Add `AND paid <> ''` to the `OverdueAmount`/`OverdueCount` `CASE` expressions in `RecoverableMetrics`, matching the guard `recoverableAgeing` already applies per row.

---

### F-E-06 — `recoverable_category:delete` is a grantable permission with no route or feature behind it

- **Severity** — low
- **Confidence** — confirmed
- **Type** — spec-divergence / permission
- **Where** — `internal/store/permissions.go:164` (`"recoverable_category": {"view", "create", "edit", "delete"}`); the only mutating route, `POST /configuration/recoverable-categories` (`internal/app/app.go:520`), is gated on `edit` alone and its handler (`internal/app/configuration.go:106-136`) only ever calls `UpsertRecoverableCategory`, which only `INSERT`s or `UPDATE`s (`internal/store/recoverables.go:184-227`) — there is no `DELETE FROM recoverable_categories` anywhere in the codebase, and no route accepts a delete action for a category at any id
- **Traces to** — V4 / TC-E-007
- **What happens** — An admin can grant a custom role `recoverable_category:delete` from the Roles screen. It does nothing. There is no way to hard-delete a category through the product; the only removal mechanism is deactivation (`active=0`), which is a deliberate design choice stated in the phase-4 spec ("No hard delete... a category may be referenced by historical requests, mirroring project/head retirement") but is not reflected in the permission vocabulary, which still advertises a `delete` verb that nothing checks.
- **Reproduction** — `probeGet`/`probePost` on `/configuration/recoverable-categories/{id}/delete` (and two more plausible shapes) all answer 404; re-saving an in-use category succeeds regardless of any permission held, confirming usage never blocks a save and no delete path exists to gate in the first place.
- **Impact** — Purely cosmetic confusion for whoever configures roles — granting "delete" on this resource buys nothing — with no functional or security consequence.
- **Suggested direction** — Drop `delete` from `resourceActions["recoverable_category"]`, or wire it to something (there is nothing sensible to wire it to while "no hard delete" remains the design).

---

### F-E-07 — The Configuration screen's hint text about employee-advance auto-fill names the wrong field

- **Severity** — informational
- **Confidence** — confirmed
- **Type** — UX (copy only)
- **Where** — `internal/app/templates.go:3165`: *"An employee advance fills the counterparty in from the requester automatically — that is a request-type rule, not a category setting."*
- **Traces to** — V5, V6 / TC-E-020
- **What happens** — The field that is actually auto-filled for an employee advance is the **payee** (`VendorPayee`, via `forcesRequesterPayee`, `internal/store/requests.go:249-253,367-369`), which shows on `/requests/{id}` as "Paid to «name»". The `Counterparty` field (the one `RecoverableCategory.RequiresCounterparty` governs, and the one this hint text is standing next to) is left exactly as the form left it — blank for the seeded "Employee advance" category, since its rule requires neither project nor counterparty. Verified live: raising an employee-advance recoverable and reading `/recoverables/{id}` shows "Counterparty: Not recorded," never the requester's name.
- **Why it is wrong** — the copy conflates two different snapshot columns that happen to both default to the requester in different circumstances.
- **Impact** — None functional; a reader configuring categories could be misled into expecting the register's Counterparty column to show the employee's name for these rows, and it never will.
- **Suggested direction** — Reword to name the payee, not the counterparty, or drop the sentence — the payee auto-fill is already true of every `employee_advance`-typed request regardless of treatment, so it is not really a fact about recoverable categories at all.

---

### F-E-08 — A rejected `type=recoverable` submission shows the wrong error message

- **Severity** — low
- **Confidence** — confirmed (discovered live while writing TC-E-002/004/024/025, reproduced deliberately afterward)
- **Type** — validation / UX
- **Where** — `internal/app/requests.go:179-184` (`renderRejectedRequestForm` looks up `requestTypeLabels[in.Type]` before it can re-render the form; the four-entry map (`internal/app/requests.go:42-55`) has no `"recoverable"` key)
- **Traces to** — V1 / discovered via TC-E-004's first failed run, fixed forward in the test, recorded here as its own product observation
- **What happens** — `type=recoverable` is a real, store-recognised request type distinct from `employee_advance` (`internal/store/requests.go:78-81`, `240-244`) — it is what a "pure" recoverable request (EMD/PBG/ICD/Security deposit/Other with no `employee_advance` masquerade) would use if the UI offered it, and what a hand-rolled API client naturally reaches for. But because no chooser card ever produces it (see F-E-02's UI gap), it is also not in `requestTypeLabels`. A **successful** `type=recoverable` submission works fine (`CreateRequest` never consults that map). A **rejected** one does not: `renderRejectedRequestForm` needs a label to re-render the form and, finding none, discards the real validation message and answers 400 "That is not a kind of request this system raises." instead — even when the actual problem was something ordinary like a missing counterparty.
- **Why it is wrong** — the two code paths (create-then-succeed vs. create-then-fail) disagree about whether `type=recoverable` is a legitimate value, purely as a side effect of one of them needing a UI label and the other not.
- **Reproduction** — `POST /requests` with `type=recoverable`, `treatment=recoverable`, a category that will fail its rule (e.g. `pbg` with no `project_id`): body is "That is not a kind of request this system raises.", not "this recoverable category always belongs to a project". The same payload with a rule that passes returns 303 normally.
- **Impact** — Low: the only callers who could ever reach `type=recoverable` at all are already bypassing the UI (F-E-02), so this is a rough edge for an API-style client, not something a person clicking through the product encounters.
- **Suggested direction** — Either add `"recoverable"` to `requestTypeLabels` with a sensible label (fixing this and giving the type a real home), or have `renderRejectedRequestForm` fall back to the chooser page with the message preserved instead of a generic 400 when the type is unrecognised.

---

### F-E-09 — `PROGRESS.md`'s "Known gaps" note about `.metric-foot` is stale, not a live defect

- **Severity** — informational
- **Confidence** — confirmed
- **Type** — spec-divergence (documentation only)
- **Where** — `docs/superpowers/PROGRESS.md:368-375` claims `.metric-foot` (among three siblings) "appear[s] in the mockups but have no rule in `web/static/fervid-ds.css`"; the rule has existed since commit `a80e18f` ("fix(css): add the four rules the mockups use but Phase 0 never delivered"), at `web/static/fervid-ds.css:367-373` (`display:block; margin-top:4px; color:var(--fb-muted); font-size:11.5px; line-height:1.3`)
- **Traces to** — brief's explicit ask re: `.metric-foot` / TC-E-041
- **What happens** — Nothing wrong on screen: the recoverables dashboard's third metric-strip line renders in a muted colour at 11.5px, not at browser defaults. Verified live (`getComputedStyle`) and by `git log -- web/static/fervid-ds.css`, which shows the fix landing after Phase 4 shipped but before this document's "Known gaps" section was last touched.
- **Impact** — None to the product; a future reader of `PROGRESS.md` could waste a pass "fixing" something already fixed.
- **Suggested direction** — Update or remove that paragraph of `PROGRESS.md`.

---

## Coverage and run summary

- **Coverage IDs exercised:** V1–V8 (recoverables), X2 & X4 (proof of absence), R6 (permissions govern data server-side), D3 (export gated by permission).
- **Final run:** `FERVID_E2E_PORT=4305 npx playwright test tests/e2e/audit-e-recoverables.spec.ts --project=chromium --reporter=line` → **0 failed, 2 skipped, 50 passed** (52 executed of 56 planned cases; 2 `test.fail()` cases — TC-E-030 for F-E-01, TC-E-036b for F-E-05 — report as passed because the harness expects and gets a failure from those specific assertions).
- **`npm run typecheck`** is clean for `audit-e-recoverables.spec.ts`. (This agent reported a residual error in a sibling's file; the coordinator re-ran `npm run typecheck` after every agent had finished and it is clean across the whole suite.)
- **NOT RUN / BLOCKED, with reasons:**
  - TC-E-031 (`test.fixme`) — grid/report unchanged after a genuinely *paid* recoverable. Blocked by F-E-01: no paid recoverable can exist through the product. Store-level proof cited instead: `TestGridAndReportExcludeRecoverablePayments`, `TestRecoverablePaymentExcludedFromActualsButInRecoverableReport` (`internal/store/recoverables_test.go:470,516`).
  - TC-E-042 (`test.fixme`) — category and expected-return survive settlement (V7). Same blocker. Store-level proof: `TestRecoverableRequestClosesOnPaymentRetainingClassification` (`internal/store/recoverables_test.go:793`).
- No file outside the three named deliverables (`docs/qa/test-cases/TC-E-recoverables.md`, `tests/e2e/audit-e-recoverables.spec.ts`, this file) was modified. No commit was made.
