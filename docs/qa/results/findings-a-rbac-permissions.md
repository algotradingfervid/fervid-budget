# Findings — permission and authorisation enforcement (area A)

> **Frozen evidence, not current status.** This document records what was found
> at commit `30edd6a` and is **not** updated as defects are fixed — the same rule
> `AUDIT-REPORT.md` states for itself. Its present-tense claims describe the
> audited commit, so a statement here that something "is" broken means it was
> broken then; several have since been repaired, and a few of the `file:line`
> citations have shifted. For what was actually **done** about each finding, read
> [`REPAIR-LOG.md`](REPAIR-LOG.md).

**Source.** `tests/e2e/audit-a-rbac-permissions.spec.ts` — 124 executed tests,
475 route × caller matrix cells, run on port 4301 against a fresh database:
**`124 passed (16.4s)`, 0 failed.** Test cases:
`docs/qa/test-cases/TC-A-rbac-permissions.md`.

**Headline.** The route-permission matrix itself is sound. All 475 cells matched
the outcome derived from `systemRoleDefaults` before the run; every anonymous
call redirects to `/login` rather than leaking a status; every state-changing
POST refuses a missing, a forged **and** a cross-session CSRF token; no route is
gated on a pair outside the canonical vocabulary; ownership rules layered above
the gate hold even against an administrator holding all 66 grants; and a
permission change is live on the very next request with no re-login.

**Attachments are the exception, and they are the whole of the serious risk in
this area.** Neither reading nor writing an attachment has any ownership check,
the Requester role holds both verbs, and — because one download route serves two
tables with independent id sequences — the product's own Download button on a
requester's own invoice returns another user's bank advice.

| ID | Claim | Severity | Confidence |
|---|---|---|---|
| F-A-01 | `GET /attachments/{id}` has no ownership check; a Requester reads every payment's bank advice | **critical** | confirmed |
| F-A-05 | One download route serves two tables, so a request document's Download link returns an unrelated payment attachment | **critical** | confirmed |
| F-A-03 | `POST /payments/{id}/attachments` has no ownership check; a Requester plants documents on any payment, including an immutable one | **high** | confirmed |
| F-A-02 | `GET /grid` is gated on a session alone; `grid:view` is never checked and the screen carries ledger data | **high** | confirmed |
| F-A-04 | The `payment` data scope is declared, grantable and enforced nowhere | **medium** | confirmed |
| F-A-06 | Nine granted verbs are enforced nowhere; `approval:reassign` is a capability with no door | **medium** | confirmed |
| F-A-08 | A request can be routed to an "approver" who holds no `approval:approve`, and then nobody can decide it | **medium** | confirmed |
| F-A-11 | A 12-character letters-only password answers 500, not 400, on both create and reset | **medium** | confirmed |
| F-A-07 | `user:create` renders the ＋ Add user control while `POST /users` demands `user:edit` | **low** | confirmed |
| F-A-09 | `friendly()` has no `ErrForbidden` branch, so an authorisation refusal reads "Something went wrong" | **low** | confirmed |
| F-A-10 | `POST /login` is the one state-changing POST outside `withCSRF`, and outside the body cap | **informational** | confirmed |

---

### F-A-01 — `GET /attachments/{id}` has no ownership check; the lowest-privilege role reads every payment's bank advice

- **Severity** — critical
- **Confidence** — confirmed (reproduced two ways: the matrix cell and a standalone escalation probe, plus the server log showing the bytes served; and the code path read end to end)
- **Type** — permission / information-flow
- **Where** — `internal/app/app.go:881-911` (`attachmentDownload`); `internal/store/store.go:1351-1360` (`AttachmentByID`, whose own comment reads *"Authorization is intentionally left to the app's permission layer"* — and the app layer supplies none); route registration `internal/app/app.go:393`
- **Traces to** — `RT-20` / `PM-20` / `OS-21` / `PE-01`; TC-A-M14, TC-A-92, TC-A-94; coverage `R6`, `Q5`
- **What happens** — `attachmentDownload` resolves the id, checks that the stored
  path sits inside `AttachmentDir`, checks that the file exists, and streams it.
  It never asks which payment the attachment belongs to, which request that
  payment settles, or whether the caller's `request` data scope reaches it. The
  only gate is the route's `attachment:view`, and the seeded **Requester** role
  holds `attachment:view` (`internal/store/migrations.go:363`).
- **Why it is wrong** — `Q5`/`R6` and `OS-01` say a caller reads only the requests
  its data scope reaches, and the product enforces that carefully everywhere
  else: `loadViewableRequest` refuses a foreign request
  (`internal/app/requests.go:236`), and `paymentDetail` explicitly re-checks the
  *request's* scope before rendering a linked payment, with the comment *"or
  `payment:view` becomes a way around Q5/R6"* (`internal/app/app.go:757-762`).
  The attachment route is the hole that same comment was written to close. A bank
  advice is the most sensitive artefact in the payment file: account numbers,
  UTRs, payee identity.
- **Reproduction** — from a signed-in state:
  1. As an accountant (Accounts role), take an approved request from
     `/accounts-queue`, record its payment, and upload a bank advice on
     `/payments/{payId}`. Note the id in the Download link, `/attachments/{N}`.
  2. Sign in as a user holding **only** the Requester role, who raised nothing
     and can see none of this: `GET /requests/{that request}` correctly answers
     403 *"You do not have permission to view this request."*
  3. `GET /attachments/{N}` as that Requester.
  4. Observed: **200** and the file bytes. Ids are small sequential integers, so
     the whole `payment_attachments` table is enumerable by walking 1, 2, 3…
- **Impact** — every signed-in user of the product can read every bank advice,
  invoice and proof-of-payment document ever uploaded, for every project and
  every vendor, regardless of role. Horizontal privilege escalation available to
  the least privileged account, needing nothing but a browser and a loop. Rated
  critical rather than high because F-A-05 means the leak is also delivered by
  the product's own UI, to users who are not attacking anything.
- **Evidence** — TC-A-92 asserts `probe.status === 200` for a Requester fetching
  an attachment uploaded by another user, and that the body contains that file's
  own content. TC-A-M14 shows the cell at 200 for C-req, C-acc and C-adm and 403
  for C-mgr — the **only** thing between a reader and every attachment is whether
  their role happens to carry `attachment:view`. Server log for the run:
  `{"path":"/attachments/1","status":200,"bytes":52}`.
- **Suggested direction** — resolve the attachment's payment, then its request,
  and apply the same `canViewRequest(a.auth.Scope(u,"request"), u, req)` check
  `paymentDetail` already runs; answer 404 rather than 403 so the route is not an
  enumeration oracle.

---

### F-A-05 — one download route serves two tables, so a request document's Download link returns an unrelated payment attachment

- **Severity** — critical
- **Confidence** — confirmed (reproduced through the UI link and by reading both SQL statements; the served bytes were compared against the direct probe)
- **Type** — data-integrity / information-flow
- **Where** — `internal/store/store.go:1353` (`AttachmentByID` selects `FROM payment_attachments`) versus `internal/app/templates.go:2393` (`request_detail` renders `{{range .RequestAtts}} … href="/attachments/{{.ID}}"`) and `internal/app/templates.go:297-307` (`payment_detail` does the same for `.RequestAtts`). Request documents live in `request_attachments` (`internal/store/requests.go:332`, `requests.go:1051`); payment documents in `payment_attachments`.
- **Traces to** — TC-A-94; coverage `T10`, `Q5`, `R6`. **No row in `docs/qa/uml/05-route-permission-matrix.md` covers this class** — it was found by probing.
- **What happens** — there is exactly one download route, `GET /attachments/{id}`,
  and it reads `payment_attachments`. Two screens hand it ids taken from
  `request_attachments`. The two tables are separate with separate autoincrement
  sequences, so the id a requester clicks means something different at the other
  end: either no such row (404) or **a different document belonging to a
  payment**. Both sequences start at 1, so the collision begins at the first row
  of each table — it is the default case, not an edge case.
- **Why it is wrong** — two ways. First, correctness: a requester can never
  download the invoice they themselves uploaded, which is the entire purpose of
  the Documents section on `request_detail` and of `T10`'s attachment policy.
  Second, and far worse, confidentiality: where the id spaces overlap, the link on
  *my* invoice serves *somebody else's* bank advice, with the recipient believing
  it is their own file, under their own filename in the page. That converts
  F-A-01 from "a hand-typed URL leaks documents" into "the product hands you
  someone else's bank details when you click Download on your own receipt".
- **Reproduction** — from a signed-in state:
  1. As an accountant, record a payment and upload `bank-advice.txt` on it. It
     becomes `payment_attachments` row 1.
  2. As a Requester who cannot see that request, raise a request and attach
     `invoice-mine.txt` through `/requests/{id}/edit`. It becomes
     `request_attachments` row 1.
  3. Open `/requests/{id}`. The Documents section lists `invoice-mine.txt` with a
     Download button pointing at `/attachments/1`.
  4. Click it.
  5. Observed: **200**, and the response is `bank-advice.txt` — the accountant's
     file, on a payment this requester is refused at `/payments/{id}`.
- **Impact** — every request document in the product is undownloadable through
  the UI, and every click on one serves a stranger's payment proof instead. It
  affects the ordinary, non-adversarial user, which makes it far more likely to be
  exercised than F-A-01 and is why both are rated critical.
- **Evidence** — TC-A-94 asserts (a) the `Content-Disposition` filename returned
  for the requester's own document id is **not** that document's name, and (b) in
  the collision case the status is 200 and the filename is the accountant's
  `bank-advice-….txt`. The run's server log shows
  `{"path":"/attachments/1","status":200,"bytes":52}` for TC-A-94, byte-identical
  to the same line emitted for TC-A-92's direct probe of the payment attachment —
  the same file, reached two ways. The two SQL statements name different tables at
  `internal/store/store.go:1353` and `internal/store/requests.go:332`.
- **Suggested direction** — give request documents their own route,
  `GET /requests/{id}/attachments/{attachmentId}`, scoped by `canViewRequest`;
  or give `AttachmentByID` a kind discriminator and the templates a distinct
  prefix. Either way the fix should carry the ownership check F-A-01 asks for,
  because it is the same boundary.

---

### F-A-03 — `POST /payments/{id}/attachments` has no ownership check either; a Requester can plant a document on any payment

- **Severity** — high
- **Confidence** — confirmed (the upload succeeded, the proof list grew by exactly one, the audit trail names the Requester, and the same payment refuses an edit from its own accountant)
- **Type** — permission / data-integrity
- **Where** — `internal/app/app.go:861-879` (`attachmentUpload`); `internal/store/store.go` `AddAttachment` checks existence, not-voided and month-unlocked only; route registration `internal/app/app.go:392`
- **Traces to** — `RT-19` / `PM-19` / `OS-21` / `OS-16` / `PE-02`; TC-A-M13, TC-A-123; coverage `S12`, `C2`
- **What happens** — the route asks for `attachment:create`, which the seeded
  Requester role holds, and the handler passes the path id straight to
  `AddAttachment`. Nothing asks whose payment it is.
- **Why it is wrong** — the attachments on a payment *are* the evidence that the
  payment happened as recorded. `partial_review` puts them in front of a manager
  deciding whether to write off a shortfall (`internal/app/linking.go:412-418`),
  and `payment_detail` renders them as "Proof". Evidence any user can add to is
  not evidence. The contrast with `S12` makes it starker: the very same payment
  refuses an edit from the accountant who entered it (400, *"a linked payment is
  immutable"*), so the record is frozen against its owner and open to a stranger.
  It also breaks the same `Q5`/`R6` boundary as F-A-01, in the write direction,
  and writes an `attach` audit row under the Requester's name, so the trail
  records the intrusion as ordinary business.
- **Reproduction** — from a signed-in state:
  1. Note a payment id `P` entered by an accountant against a request a
     particular Requester cannot see.
  2. As that Requester, `POST /payments/P/attachments` with a valid `csrf` field
     and a multipart `attachment` file.
  3. Observed: **303 → /payments/P**.
  4. Sign in as the accountant and open `/payments/P`: the planted file is listed
     under Proof, and the trail reads "Uploaded attachment planted-….txt"
     attributed to the Requester.
  5. As that same accountant, `POST /payments/P/edit` → **400**, the payment is
     immutable.
- **Impact** — any user can attach an arbitrary file to any payment. The obvious
  abuse is planting a forged advice; the quieter one is attaching noise to a
  payment under review to muddy a partial-settlement decision. Combined with
  F-A-01 the attacker can read what is already there first.
- **Evidence** — TC-A-123. The proof-list link count before and after the upload
  differs by exactly one, read from the accountant's own rendering of the page;
  the trail contains the Requester's name; the edit probe returns 400.
- **Suggested direction** — the same fix as F-A-01 applied to the write path:
  resolve payment → request and require the caller's scope to reach it. A tighter
  rule would demand the payment's own reservation or `payment:edit`, since adding
  to the proof of an immutable payment is a change to it.

---

### F-A-02 — `GET /grid` is gated on a session alone; `grid:view` is never checked, and the screen carries payment data

- **Severity** — high
- **Confidence** — confirmed (three ways: the route registration, the sidebar's own hiding of the item, and the leaked table's contents)
- **Type** — permission / information-flow
- **Where** — `internal/app/app.go:377` (`mux.Handle("GET /grid", a.auth.RequireLogin(...))`) versus `internal/app/nav.go:70` (the nav item declares `Resource: "grid", Action: "view"`). The handler also loads the last ten payments unconditionally, `internal/app/app.go:643`
- **Traces to** — `RT-07` / `PM-07` / `GR-43` / `PE-03`; TC-A-M01, TC-A-58, TC-A-62, TC-A-63; UC-A-42; coverage `R6`, `D2`
- **What happens** — `grid:view` exists in the vocabulary, is grantable from the
  roles matrix, and is held by Manager, Accounts and Admin but not by Requester.
  The sidebar honours it: a Requester sees no "Variance grid" entry. The route
  ignores it: `GET /grid` answers 200 to any signed-in caller, including one with
  **no role at all**.
- **Why it is wrong** — `R6` says permissions govern data server-side and a URL
  must be blocked; `D2` says only permitted areas appear. A grant that changes
  only what the menu draws is the exact failure mode this codebase warns itself
  about (`web/static/fervid-app.js:7`). And the screen is not innocuous: it
  renders the whole budget-versus-actual matrix **and** a "Recent Payments" table
  with amounts, payees and live `/payments/{id}` links — for a caller whose
  `/payments` answers 403 two assertions later in the same test. Note the
  asymmetry with `GET /export.csv`, which *is* gated on `grid:export`
  (`app.go:394`): the CSV of the grid is protected and the grid is not.
- **Reproduction** — from a signed-in state:
  1. Sign in as a user holding only the Requester role.
  2. `GET /` — the sidebar has no "Variance grid" item.
  3. `GET /payments` → **403**.
  4. `GET /grid` → **200**, containing "Recent Payments for …", amounts, payees
     and `href="/payments/{id}"` links.
  5. On a phone it is worse: `resolveTabs` (`internal/app/nav.go:228-232`) puts a
     **Budget → /grid** tab in the bottom bar precisely *because* the caller lacks
     `payment:view`, so the tab bar advertises the screen the sidebar hides.
- **Impact** — company-wide budget figures and a payments summary readable by
  every account, including a newly created one with no roles assigned.
- **Evidence** — TC-A-63: `/grid` 200 with "Recent Payments" and payment links;
  `/payments` 403 for the same session; `.sidebar a[href="/grid"]` count 0 and
  `.tabbar a[href="/grid"]` count 1. TC-A-62 shows the same 200 for a role-less
  user.
- **Suggested direction** — gate the route on `grid:view` like every other nav
  target, and decide separately whether the recent-payments panel belongs behind
  `payment:view` — that is the part which leaks beyond budgets.

---

### F-A-04 — the `payment` data scope is declared, grantable, rendered, and enforced nowhere

- **Severity** — medium
- **Confidence** — confirmed (a custom role with `payment=own` sees the whole ledger; and `Scope(u, "payment")` appears in no handler)
- **Type** — permission / spec-divergence
- **Where** — `internal/store/permissions.go:185` (`scopedResources = {"request": true, "payment": true}`); `internal/app/permmap.go:255-268` draws a scope control for every scoped resource, so the roles screen offers Payments · None/Own/Assigned/All; `internal/app/app.go:669-685` (`payments`) calls `ListPayments` with no scope argument, and no file in `internal/app` calls `a.auth.Scope(u, "payment")`
- **Traces to** — `OS-05`; TC-A-69, TC-A-72, TC-A-118; coverage `R3`
- **What happens** — an administrator can build a role with `payment:view` and
  data scope **Own**, save it, and see the choice persist and re-render. It
  changes nothing: the holder receives the entire payments ledger.
- **Why it is wrong** — `R3` promises "data scope per resource
  (own/assigned/all)", and the vocabulary declares `payment` one of the two scoped
  resources. A control that saves, round-trips and does nothing is worse than an
  absent one, because the administrator who set it believes access is restricted.
- **Reproduction** — from a signed-in state as an administrator:
  1. `/roles` → ＋ New role → `payments-own`.
  2. Tick Payments · View. Set that row's "Records it can see" to **Own**. Save.
  3. Create a user, untick every other role, tick `payments-own` only.
  4. Sign in as that user and open `/payments`.
  5. Observed: **200 with every payment in the month**, including payments entered
     by other people. Expected: none, since this user has entered none.
- **Impact** — any attempt to build a restricted accounts role silently grants
  full ledger visibility. Not exploitable through the seeded roles (only Accounts
  and Admin hold `payment:view`, and both carry `payment=all`), so the exposure is
  latent until somebody uses the feature the roles screen advertises.
- **Evidence** — TC-A-72, the suite's one `test.fail()`, with the assertion
  intact: `expect(probe.body.includes('/payments/{payId}'), 'scope payment=own
  must hide a payment this user did not enter (R3) — F-A-04').toBe(false)` fails.
  TC-A-118 separately proves the control round-trips, so the setting is real and
  only its enforcement is missing. The day the scope is wired up, TC-A-72 reports
  "passed unexpectedly".
- **Suggested direction** — either give `PaymentListOptions` a scope + viewer and
  filter on `entered_by`, mirroring `requestWhere`; or remove `payment` from
  `scopedResources` so the matrix stops offering a control that does nothing.

---

### F-A-06 — nine granted verbs are enforced nowhere, and one of them is a capability with no door

- **Severity** — medium
- **Confidence** — confirmed (55 route gates extracted from `routes()` and diffed against the 66-pair vocabulary; the unreachable store method has no caller outside tests; three plausible URLs probed)
- **Type** — spec-divergence
- **Where** — `internal/app/app.go:362-524` names 55 distinct pairs. Unnamed by any route gate: `approval:reassign`, `grid:view`, `head:create`, `payment:mark_partial`, `project:create`, `recoverable_category:{view,create,delete}`, `user:create`, `vendor_bank:{view,edit}`. `vendor_bank:*` **are** enforced below the route, at `internal/store/vendors.go` and `internal/app/templates.go:1458-1474`; the other nine are enforced by nothing. `store.ReassignRequest` (`internal/store/requests.go:759`) has no caller anywhere outside tests.
- **Traces to** — `GR-11`, `GR-20`, `GR-33`, `GR-36`, `GR-43`, `GR-47`, `GR-48`, `GR-50`, `GR-54`; §4.1 of `05-route-permission-matrix.md`; TC-A-119, TC-A-120; coverage `A7`, `R2`, `R8`, `V4`
- **What happens** — the roles matrix draws a checkbox for each of these, saves it
  and re-renders it. Toggling any of the nine changes nothing observable. Three
  are merely folded into a sibling verb by an upsert handler (`project:create`,
  `head:create`, `recoverable_category:create`), which is defensible. Four are
  genuinely misleading:
  - **`approval:reassign`** — granted to Manager and Admin, backed by a complete
    and tested store method, reachable by no URL. Coverage `A7` ("Admin reassign
    with reason + history") therefore has no HTTP surface at all. It is also the
    missing recovery path for F-A-08.
  - **`grid:view`** — see F-A-02.
  - **`payment:mark_partial`** — a partial settlement is performed by
    `POST /payments` behind `payment:create`; revoking `mark_partial` does not
    stop anybody marking a payment partial.
  - **`recoverable_category:delete`** — no delete exists on the HTTP surface at
    all, while `V4` claims "admin-configurable categories".
- **Why it is wrong** — `R2` and `R8` promise granular per-resource control. A
  cell that does nothing is a false statement about the system's behaviour, made
  by the very screen an administrator uses to reason about access.
- **Reproduction** — as an administrator: `/roles` → any role → open Advanced on
  "Payment requests" → untick "Payment requests · approval reassign" → Save →
  reload and confirm it saved. Nothing behaves differently, and no URL exercises
  it: `POST /requests/{id}/reassign-approver`, `…/reassign-manager` and
  `…/approver` all answer **405**. The one real `/reassign` route is the
  reservation's, and it refuses a Manager — who holds `approval:reassign` and not
  `reservation:reassign`.
- **Impact** — misleading administration; an unbuilt feature (`A7`) that looks
  built; and a stranded-request state (F-A-08) with no in-app repair.
- **Evidence** — TC-A-119 pins 55 route gates, 11 pairs unnamed by any gate and 9
  enforced nowhere, and asserts separately that **no** route names a pair outside
  `resourceActions`. TC-A-120 shows the three candidate URLs at 405 and the
  reservation route at 403 for a Manager.
- **Suggested direction** — build the approver-reassignment route
  (`store.ReassignRequest` is ready and `A7` asks for it); for the rest, either
  consume the verb or take the cell off the matrix. Rendering an unenforced grant
  is the one option that should not survive.

---

### F-A-08 — a request can be routed to an "approver" who holds no `approval:approve`, and then nobody can decide it

- **Severity** — medium
- **Confidence** — confirmed (the request was created, both plausible deciders were refused, and only withdrawal remained)
- **Type** — validation / state-machine
- **Where** — `internal/store/requests.go:127-147` (`validateRequestInput` checks `ManagerID > 0` and `ManagerID != RequesterID`, and nothing else about the manager) versus `internal/store/requests.go:551-557` (`ListApprovers` selects only users holding `approval:approve`)
- **Traces to** — TC-A-122; coverage `A1`, `A7`, `GR-08`
- **What happens** — the `<select name="manager_id">` on the request form offers
  only real approvers, but the server re-checks nothing. A posted `manager_id`
  naming a Requester-only user is accepted, and the request is created `pending`
  and routed to them.
- **Why it is wrong** — `A1` makes the requester pick a manager, and every
  decision route is gated on `approval:*` **and** on `manager_id == actor`
  (`OS-08`). Those two rules together mean a request routed to a non-approver can
  be decided by nobody: the named person is refused by the route gate, every real
  manager is refused by the ownership check, and the recovery a designer would
  reach for — reassigning the approver — does not exist as a route (F-A-06). A
  hidden `<select>` option is not validation; the code applies exactly this
  principle explicitly for the default approver
  (`internal/store/permissions.go:513-519`) and not here.
- **Reproduction** — from a signed-in state as a Requester:
  1. Find the numeric user id of another Requester-only account.
  2. `POST /requests` with `type=reimbursement`, a valid project, head, amount and
     expense date, `manager_id=<that id>`, and a valid `csrf`.
  3. Observed: **303 → /requests/{id}/submitted** — created and pending.
  4. As the named "approver": `POST /requests/{id}/approve` → **403** (holds no
     `approval:approve`). As a real Manager → **403** (not this request's
     manager).
  5. `POST /requests/{id}/withdraw` as the raiser → 303. That is the only exit.
- **Impact** — a request parked in `pending`, in no `/approvals` queue, with no
  manager who can act on it. Reachable by a hand-rolled POST, or by a stale form
  page whose approver has since lost the Manager role — so it is reachable without
  malice.
- **Evidence** — TC-A-122, asserting the observed creation, both refusals, and
  that withdrawal is the remaining exit.
- **Suggested direction** — have `validateRequestInput` (or `CreateRequest`)
  reject a `manager_id` that does not hold `approval:approve`, using the query
  `ListApprovers` already runs. Applying it to `UpdateRequest` closes the
  edit-the-approver path too.

---

### F-A-11 — a 12-character letters-only password answers 500, not 400, on both create and reset

- **Severity** — medium
- **Confidence** — confirmed (observed on both paths; the two validators read end to end; and a control case proves the app's own rule reports correctly)
- **Type** — validation
- **Where** — `internal/app/app.go:1555-1560` (`validatePassword` checks length alone: `len(password) < 12`) then `internal/app/app.go:1170-1181` (`userSave` calls `auth.HashPassword`, and reports its failure as `respondError(…, http.StatusInternalServerError, "The password could not be secured.", err)`); the second rule is `internal/auth/auth.go:47-60` (`ValidatePassword` requires at least 8 characters **and** a letter **and** a digit)
- **Traces to** — TC-A-124; UC-A-19, UC-A-22; `RT-86`
- **What happens** — two password rules disagree and the caller pays for it. A
  12-character letters-only password passes the app-layer length check, then fails
  the auth-layer digit check, and the handler classifies that as a server fault:
  HTTP **500** with *"The password could not be secured."* The same happens on the
  reset path.
- **Why it is wrong** — a 500 means "the server broke", and every other rejected
  input in this handler is a 400 with a message naming the rule: an 11-character
  password answers 400 *"password must be at least 12 characters"*. The digit rule
  is real and reasonable; it is simply enforced in the wrong layer and reported in
  the wrong class. An administrator resetting a colleague's password gets a
  server-error page and no idea what to change, and the 500 pollutes error
  monitoring with a routine validation event (`respondError` logs it at
  `slog.LevelError`, `internal/app/http_errors.go:166-174`).
- **Reproduction** — from a signed-in state as an administrator:
  1. `POST /users` with `id=0`, a fresh email, a name, `active=on`, and
     `password=abcdefghijkl` (12 letters, no digit), plus a valid `csrf`.
  2. Observed: **500**, body *"The password could not be secured."*
  3. `POST /users` with `id=<an existing user>` and the same password.
  4. Observed: **500** again.
  5. Control: the same POST with `password=abcdefghij1` (11 characters) →
     **400**, *"password must be at least 12 characters"*.
- **Impact** — no security consequence: it fails closed, and TC-A-124 confirms
  the victim's credentials and live session are untouched by the refused reset.
  The cost is an administrator blocked with a misleading error, and false alarms
  in the error log.
- **Evidence** — TC-A-124: 500 on create, 500 on reset, 400 with the correct
  message on the 11-character control, and `GET /requests` still 200 for the
  victim afterwards.
- **Suggested direction** — have `internal/app.validatePassword` delegate to
  `auth.ValidatePassword` and add its own 12-character minimum on top, so one
  function owns the whole rule and every failure is a 400 naming what is missing.

---

### F-A-07 — `user:create` renders the ＋ Add user control, and `POST /users` demands `user:edit`

- **Severity** — low
- **Confidence** — confirmed (a custom role holding `user:view` + `user:create` sees the control and is refused its submit)
- **Type** — permission / UX
- **Where** — `internal/app/templates.go:1195` (`{{if .Perms.Can "user" "create"}}` renders the button) versus `internal/app/app.go:510` (`POST /users` is gated on `user:edit`; the same handler creates when `id == 0`, `app.go:1185-1190`)
- **Traces to** — `GR-54`; TC-A-121; UC-A-19
- **What happens** — a role holding `user:view` and `user:create` but not
  `user:edit` gets the Users screen with a working ＋ Add user sheet whose submit
  answers 403. Conversely a role holding `user:edit` alone can mint users without
  holding `user:create` at all.
- **Why it is wrong** — it is the exact inverse of the rule the product states for
  itself — *"a control the route would reject is a lie"*
  (`internal/app/app.go:799-802`) — and it is the only place in the area where a
  rendered control 403s; the sidebar's 18 items are all consistent (TC-A-58 …
  TC-A-62).
- **Reproduction** — as an administrator, create a role granting only `user:view`
  and `user:create`; assign it as a user's only role; sign in as them; `/users`
  renders with ＋ Add user; fill the sheet and submit → **403**.
- **Impact** — small: it needs a custom role to reach and it fails closed. The
  cost is a broken screen for a legitimately configured role, plus the misleading
  grant of F-A-06.
- **Evidence** — TC-A-121: the control is `toBeVisible()` and the create POST is
  403.
- **Suggested direction** — split the route (`POST /users` with `id == 0` requires
  `user:create`, otherwise `user:edit`), or gate the button on `user:edit` so the
  screen and the route agree.

---

### F-A-09 — an authorisation refusal on the settlement path is reported as "Something went wrong"

- **Severity** — low
- **Confidence** — confirmed (observed on the response body; and `friendly()` read end to end)
- **Type** — UX
- **Where** — `internal/app/app.go:1628-1641` (`friendly` has branches for `ErrLockedMonth`, `ErrDuplicate`, `ErrInactiveHead` and `ErrValidation`, and falls through to a generic sentence for everything else, `store.ErrForbidden` included); consumed by `settlementError` (`internal/app/linking.go:341-363`) and by `renderRejectedEdit` / `renderRejectedRequestForm`
- **Traces to** — TC-A-102; `OS-15`
- **What happens** — `RecordPaymentForRequest` refuses a caller who does not hold
  the request's reservation with a precise message, *"reserve this request before
  recording its payment"*, wrapped in `ErrForbidden`. `settlementError` re-renders
  the confirmation sheet at 403 with `friendly(err)`, which discards it and prints
  *"Something went wrong while processing your request."*
- **Why it is wrong** — the product is otherwise careful to say why: the ownership
  refusals on editing, cancelling and releasing all reach the reader in words
  (`internal/app/requests.go:593`, `requests.go:804`,
  `internal/app/linking.go:536`). Here the refusal an accountant is most likely to
  meet — the reservation was lost, or the form was stale — is reported as a fault
  rather than as a rule, and the same sentence also covers genuine server faults,
  which hampers diagnosis.
- **Reproduction** — as an accountant holding a reservation on request A,
  `POST /payments` with `request_id` = a different approved request B and
  otherwise valid fields. Observed: 403 whose body reads *"Something went wrong
  while processing your request."* rather than naming the reservation.
- **Impact** — diagnosis and self-service only. The refusal itself is correct.
- **Evidence** — TC-A-102 asserts 403, asserts the body does **not** carry the
  middleware's permission wording (so the store refused, not the gate), and
  asserts it **does** carry the generic sentence. A second reading confirms the
  state: `GET /payments/new?request={B}` answers 409 for the same caller.
- **Suggested direction** — add an `errors.Is(err, store.ErrForbidden)` branch to
  `friendly` returning the wrapped message, the way the `ErrValidation` branch
  already does.

---

### F-A-10 — `POST /login` is the only state-changing POST outside `withCSRF`, and therefore outside the 21 MiB body cap

- **Severity** — informational
- **Confidence** — confirmed (observed 200 with no CSRF refusal; and the registration read directly)
- **Type** — permission
- **Where** — `internal/app/app.go:365` (`mux.HandleFunc("POST /login", a.loginPost)` — every other mutating POST is wrapped in `a.withCSRF`); the body cap and the `ParseForm` guard live inside `withCSRF` (`app.go:556-571`)
- **Traces to** — `RT-03` / `PM-03`; TC-A-83; UC-A-05. Also filed as F-07 in `05-route-permission-matrix.md`
- **What happens** — a POST to `/login` with no `csrf` field is processed
  normally: wrong credentials re-render the login template at 200, correct ones
  sign the caller in.
- **Why it is wrong** — arguably it is not: a login form has no session to protect
  and a pre-session token is commonly omitted. It is recorded because (a) it is
  the single exception to an otherwise uniform rule, so a reader auditing
  `withCSRF` coverage should not have to rediscover it, and (b) the missing body
  cap means `loginPost` will `ParseForm` an unbounded body on an unauthenticated
  endpoint, which every other POST refuses at 21 MiB. Login-CSRF also enables
  session-fixation nuisance.
- **Reproduction** — anonymous context, `POST /login` with
  `email=…&password=wrong` and no `csrf`. Observed 200, the login template with
  its error, and no CSRF refusal anywhere in the body.
- **Impact** — no data exposure. The realistic worry is the unbounded request body.
- **Evidence** — TC-A-83.
- **Suggested direction** — keep the CSRF exemption if it is intentional and say
  so in a comment, but wrap the handler in the same `http.MaxBytesReader` cap
  every other POST already has.

---

## What was verified and is sound

Recorded because a findings list read alone gives a false picture of the area.

| Property | Evidence |
|---|---|
| All 475 route × caller cells matched the outcome derived from `systemRoleDefaults` before the run — nothing more permissive, nothing less | TC-A-M01 … TC-A-M57 |
| An anonymous caller is redirected (303 → `/login`) on all 25 probed paths and never 403s or 200s | TC-A-108 |
| Every state-changing POST refuses a missing, a forged **and** a cross-session CSRF token, across 12 resource families, and the refusal comes from `withCSRF` rather than from the permission gate | TC-A-79 … TC-A-81, with TC-A-82 as the positive control |
| No route is gated on a pair outside `resourceActions`, and a hand-crafted `cell` or `perm` value cannot invent a grant | TC-A-119, TC-A-100 |
| The vocabulary is exactly 21 resources / 66 pairs, the matrix screen offers every pair once, and saving round-trips exactly in both directions | TC-A-115 … TC-A-118 |
| Ownership beats grants: an administrator holding all 66 pairs is still refused editing somebody's request, asking for its cancellation, deciding a cancellation they were not sent, and accepting a partial settlement they do not own | TC-A-78, TC-A-75, TC-A-M34, TC-A-M38, TC-A-M42 |
| G8 holds at both layers — the form refuses a self-approver, and `ApproveRequest` refuses one that reached the row anyway | TC-A-73 |
| `reservation:reassign` is genuinely Admin-only, and it is what authorises releasing work that is not yours | TC-A-M46, TC-A-77 |
| The `request` scope holds on the list, in the CSV export and by direct id; a `?scope=` may only narrow; scopes union to the broadest across roles | TC-A-65 … TC-A-68, TC-A-70 |
| A permission change is live on the very next request, grant *and* revoke, with no re-login and no cached permission set | TC-A-71 |
| A deactivated account's live session stops working immediately | TC-A-114 |
| Another user's notification answers 404, not 403 — no enumeration oracle | TC-A-109 |
| The sidebar's verb and the route's verb agree on all 18 navigable screens; no nav item renders and then 403s | TC-A-58 … TC-A-62 |
| A reservation cannot be handed to somebody who cannot work the queue | TC-A-113 |
| The approved amount is a hard ceiling, a forged `settlement` value is refused, and a swapped `request_id` on the payment form is refused | TC-A-104, TC-A-103, TC-A-102 |

---

## NOT RUN, and why

| Item | Reason |
|---|---|
| `F-05` of `05-route-permission-matrix.md` — `GET /recoverables/{id}` bypasses the request data scope | Real from the code: `recoverableDetail` renders a whole request, its payment and its thread with only `recoverable_report:view` and no `canViewRequest` (`internal/app/recoverables.go:130-172`). **Not exploitable with the seeded roles** — the only holders (Accounts, Admin) also carry `request=all` — so proving it needs a custom role plus a recoverable request, and this pass built neither. Marked NOT RUN rather than claimed. |
| `F-06`, `F-08`, `F-10`, `F-11`, `F-12` of the same document | The conflict/preview scope reads, `request:comment` reach across all requests, the mutating GET on `/notifications/{id}/open`, the `/static/` directory listing and refusal ordering. Out of the probe set this pass built; neither confirmed nor contradicted. |
| `POST /logout` with a **valid** token from a signed-in session | Only the anonymous, tokenless case is asserted (TC-A-84). Exercising the valid case would end a session that later tests depend on, and `workers: 1` gives no isolated slot for it. |
| Every number here is measured against a **fresh** database | `seedSystemRoles` runs inside migration v1 and never re-runs (`internal/store/migrations.go:75`), so an installed database may legitimately hold different grants. The matrix should be re-measured against production grants before being relied on there. |
| `--project=mobile-chrome` | All 124 tests skip by design: nothing in this area is layout-dependent, and building the fixture world twice would double the users for no new information. The guard is `testInfo.project.name`, not `browserName` — Pixel 5 is chromium too. |
