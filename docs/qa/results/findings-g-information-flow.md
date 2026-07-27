# Findings — G · information-flow integrity

Thirty-four findings from the information-flow audit. Every one was reproduced
either twice through the product or once through the product and once against the
code that should have prevented it; the one that could not be reproduced at all is
marked `unverified` and says so.

**What did not go wrong.** Recorded first, because it is the more surprising half
of the result. The money trail is sound: a rupee figure typed into `Amount`
arrives unchanged at eighteen consecutive hops including two CSVs, with exactly
one `₹` at each, for a lakh-grouped value, for a paise-precision value, and for
the adjusted case where approved ≠ requested and paid ≠ approved. The grid follows
the **paid** figure and the request keeps the **approved** one, exactly as IF12
and IF4 require. The payee reaches every screen that shows one, including the
payment detail's `<h1>` — the "KNOWN GAP" comment on
`fixtures.createApprovedRequest` is **stale**, closed by
`internal/app/linking.go:260` and `internal/app/templates.go:390`. Audit
before/after images are genuine pre- and post-images, and no refused mutation of
any kind left an audit row behind. `/requests/export.csv` applies the caller's
data scope correctly. Those are the questions this audit existed to ask, and the
answer to most of them is yes.

**Severity ranking.**

| ID | Severity | One line |
|---|---|---|
| F-G-016 | critical | `/recoverables/list` and its CSV apply no data scope, and the CSV discloses more than its own screen |
| F-G-033 | critical | Retiring a project makes its heads' own Save button reassign them to a different project |
| F-G-001 | high | A recoverable raised through the UI can never be paid — the form offers no head and the store requires one |
| F-G-003 | high | The `payment` data scope is declared, editable, seeded and never read |
| F-G-017 | high | The audit log discloses the full text of every request to anyone holding `audit:view` |
| F-G-018 | high | The dashboard's accounts area hardwires scope `all` and lists requests the caller cannot open |
| F-G-028 | high | An unbudgeted month cannot be saved at all: the budgets screen refuses the values it rendered |
| F-G-032 | high | `/grid` is `RequireLogin` only, so a subject with no role reads every budget, actual, payee and payment link |
| F-G-034 | high | `POST /users` spans three transactions, so a refused save still commits the rename |
| F-G-035 | high | A request edit whose resubmit fails still writes the edit and audits it as done |
| F-G-022 | high | A role assigned to users is deleted with no pre-check, silently stripping it from its holders |
| F-G-024 | high | Deactivating a head permanently strands its approved requests, with no warning |
| F-G-025 | high | Deactivating an approver strands their pending approvals with no route to reassign them |
| F-G-027 | high | A request aimed at an id that does not exist answers 500, not a 4xx |
| F-G-029 | high | The grid's own filter form posts to `/`, so the grid cannot be filtered and a lock is never confirmed |
| F-G-002 | medium | 403-vs-404 lets a requester enumerate which request ids exist |
| F-G-004 | medium | The audit log's Entity filter cannot reach `payment_request`, so the whole workflow is unfilterable |
| F-G-006 | medium | The "My open requests" tile omits `returned`, which the bucket it links to includes |
| F-G-007 | medium | Two screens carry the label "Approved, unclaimed" and show different numbers |
| F-G-009 | medium | Vendor "Paid this year" is an exact-name match, so a rename silently zeroes it |
| F-G-013 | medium | Two nav badges are declared and no query ever populates them |
| F-G-014 | medium | `/requests` silently ignores `?status=` while its own CSV honours it |
| F-G-019 | medium | Locking a month lands the operator on the dashboard, which confirms nothing |
| F-G-020 | medium | `/payments/new` shows no lock affordance, so the whole form is filled in first |
| F-G-021 | medium | Requests are not lock-aware: one may be raised and approved into a locked month |
| F-G-023 | medium | Deleting a system role reports "you do not have permission" to the holder of every permission |
| F-G-030 | medium | Every CSV amount column is a formatted currency string, not a number |
| F-G-036 | medium | A 12-character letters-only password answers 500, not 400 |
| F-G-005 | low | Workflow actions render as raw column values on `/audit` |
| F-G-010 | low | Vendor "Open requests" is always zero |
| F-G-012 | low | The recoverables by-category drill-through cannot reproduce its own count |
| F-G-026 | low | A locked-month budget save answers 400 where every other locked-month refusal answers 409 |
| F-G-008 | informational | The accounts-queue Approved badge understates the rows beneath it |
| F-G-037 | informational | The notification centre's 100-row limit against an unbounded count (**unverified**) |

Three observations that are **not** defects are recorded at the end.

---

### F-G-016 — `/recoverables/list` and its CSV apply no data scope, and the CSV discloses more than its own screen

- **Severity** — critical
- **Confidence** — confirmed (reproduced two ways: through the UI with a custom role, and by reading the store function's signature, which takes no viewer)
- **Type** — information-flow
- **Where** — `internal/store/recoverables.go` (`RecoverableReport`, no viewer parameter); `internal/app/recoverables.go:95-126` (`exportRecoverable`); `internal/app/app.go:404-405` (both routes gated on `recoverable_report` alone)
- **Traces to** — UC-C-33, UC-C-34 · R3, R6, V3, D3 · TC-G-054, TC-G-058
- **What happens** — A caller holding `recoverable_report:view` reads the register for every requester in the system, and one holding `recoverable_report:export` downloads it in a single request — regardless of their `request` data scope. A subject whose scope is `own`, and who is correctly refused 403 on `/requests/{id}` for the very same request, nonetheless reads that request's number, category, counterparty, project and amount off `/recoverables/list`. The CSV goes further: `exportRecoverable` writes eleven columns including **Requester** and **Repayment Notes**, neither of which the on-screen register renders at all.
- **Why it is wrong** — R3 requires a data scope per resource and R6 requires permissions to govern data server-side. `canViewRequest` (`internal/app/requests.go:332-343`) is the rule for reading a request row, and Q5 says a requester sees only their own. The register is a second view over `payment_requests` that never asks. This is exactly the place the brief predicted scope filtering would be forgotten — and it is the one of the four CSVs where it matters, because the other three have no per-row owner to filter by.
- **Reproduction** —
  1. As admin, create a role holding `recoverable_report:view`, `recoverable_report:export` and `request:view`, with `scope_request=own`.
  2. Create a user holding **only** that role (untick the Accounts role the create sheet assigns by default).
  3. As a different requester, raise a recoverable (`/requests/new?type=employee_advance`, ICD category, a distinctive counterparty) and have it approved.
  4. Sign in as the subject from step 2. `GET /requests/{id}` → 403.
  5. `GET /recoverables/list` → 200, and the row is there with its number, counterparty and amount.
  6. `GET /recoverables/list.csv` → the same row plus the requester's name and their repayment terms verbatim.
- **Impact** — Every recoverable in the company — counterparty, amount, expected return date, repayment terms and the name of the person who raised it — is readable and downloadable by anyone an administrator grants the recoverables report to. Today the seed grants it to Accounts and Admin, both of whom hold `request=all`, so nothing is currently exposed; the moment an administrator builds a narrower role — which R1 exists to let them do — the leak opens with no warning.
- **Evidence** — `F-G-016: the register lists a request this caller is forbidden to open` → the number was present. `F-G-016: the CSV names the requester, which the on-screen register does not even show` → present. The register's `<thead>` carries no Requester column, confirming the CSV over-discloses relative to its own screen.
- **Suggested direction** — Give `RecoverableReportOptions` a `Scope`/`ViewerID` pair and apply it in the same `WHERE` clause `ListRequests` uses, so the register and the request list cannot disagree about who may see a row. Until then, treat `recoverable_report` as implying `request=all` and say so on the roles screen.

---

### F-G-033 — Retiring a project makes its heads' own Save button reassign them to a different project

- **Severity** — critical
- **Confidence** — confirmed (reproduced through the UI; the cause is two mismatched arguments on adjacent lines)
- **Type** — data-integrity
- **Where** — `internal/app/app.go:1093-1105` — `heads` calls `ListHeads(ctx, false)` (all heads) and `ListProjects(ctx, true)` (active projects only); the row select is rendered at `internal/app/templates.go:1176`
- **Traces to** — UC-A-28, UC-A-29 · T12 · TC-G-087
- **What happens** — A head whose project has been retired is still listed on `/heads` (correctly — T12 keeps history visible), but its `<select name="project_id">` is populated from active projects only, so it contains no option for the head's own project. The browser then selects the first option. The row's Save button posts that select. Pressing it — with no edit of any kind — moves the head to a project nobody chose.
- **Why it is wrong** — A control that displays a value the record does not hold is lying, and a Save that changes data the operator did not touch is corruption rather than a mistake. T12 says inactive projects and heads are hidden from *new* entries and shown on historical ones; it does not say the historical view may rewrite them. The grid groups payments by the head's project, so a reassignment silently moves every historical payment under that head into a different project's variance.
- **Reproduction** —
  1. As admin, `POST /projects` with a new name → note its id from the row's hidden `input[name="id"]`.
  2. `POST /heads` with that `project_id` and a new head name.
  3. On `/heads`, confirm the head's select shows the new project.
  4. `POST /projects` with the same id and name but **no** `active` field → the project is retired, with no warning that a head hangs off it.
  5. Reload `/heads`. The head is still listed. Its select contains no option for its own project, and its selected value is now some other project's id.
  6. Press that row's **Save**. Change nothing else.
  7. Reload `/heads`. The head is now under the project the browser had preselected.
- **Impact** — One click on a control the operator had no reason to distrust silently relocates a spend head, and with it every payment ever recorded against it, into another project's budget variance. There is no confirmation, no audit of intent, and nothing on screen that would make the operator suspect anything happened.
- **Evidence** — `F-G-033: the row offers no option for the head's own project` → no option matched. `F-G-033: pressing Save with no edit reassigned the head to a project nobody chose` → the post-save `project_id` differed from the original and equalled the value the browser had preselected.
- **Suggested direction** — `heads` should load the same project set it lists heads from (`ListProjects(ctx, false)`), rendering a retired project as a disabled-but-selected option. That is one argument, and it makes the control honest.

---

### F-G-001 — A recoverable raised through the UI can never be paid

- **Severity** — high
- **Confidence** — confirmed (reproduced through the UI end to end; the store's own test covers the same path only by seeding `head_id` directly, which is why it has never been seen)
- **Type** — state-machine
- **Where** — `internal/app/templates.go:1730-1781` (the recoverable fieldset offers project and counterparty, never a head); `internal/store/store.go:1621` (`validatePayment` refuses `in.HeadID == 0`); `internal/app/linking.go:244-247` (the entry screen posts `head_id` from `req.HeadID`, which is nil)
- **Traces to** — UC-C-31, UC-C-36 · V1, V6, V7, L10 · TC-G-038
- **What happens** — A recoverable request is raised, approved, offered in the accounts queue with a "Take for processing" button, reserved, and its entry form filled in — and the confirm is refused with "valid head, date, and positive amount are required". No payment is ever created. The recoverable then sits in the register's "Not yet paid" ageing bucket permanently, because V7's "closes on payment" can never happen.
- **Why it is wrong** — V7 says a recoverable closes on payment and retains its classification; the register has a `PaidOn` column and an "Awaiting payment" state, and the accounts queue renders recoverables with their own pill and offers them for processing. Every screen in the chain promises the payment is possible. `payments.head_id` is `NOT NULL` and the recoverable form never asks for a head, so it is not.
- **Reproduction** —
  1. `/requests/new?type=employee_advance` (the only UI route to a recoverable — `requestTypeOptions` offers four types and `recoverable` is not one of them, so `?type=recoverable` falls back to the chooser).
  2. Fill the short title; the treatment is already "Refundable or recoverable". Set the category to ICD, fill the counterparty, the expected return date, the terms, the amount, "What the money is for" and the purpose. Choose an approver. Submit.
  3. The request detail has **no** Head row.
  4. Approve it as that approver.
  5. `/accounts-queue?tab=approved` → the row is there, labelled "Recoverable", with a "Take for processing" button. Press it.
  6. The entry form's hidden `head_id` is `0`. Fill in the amount, date, mode and reference. Press "Payment settled →", choose "Fully settled", press "Confirm and save payment".
  7. 400. URL is `/payments`. No payment exists. `/recoverables/list?ageing=unpaid` still lists it.
- **Impact** — The whole recoverables feature is read-only in practice. Deposits, EMDs, bank guarantees and employee advances can be requested and approved but never recorded as paid, so the register's outstanding total counts money that may well have left the bank, and its ageing is meaningless. The Go tests cannot see it because `seedRecoverablePayment` writes `head_id` directly.
- **Evidence** — `F-G-001: the settlement is refused, so no payment exists` → URL `/payments`; the sheet's error banner contained "valid head". `F-G-001: it sits in "Not yet paid" with no reachable way out` → the row was present under `?ageing=unpaid`.
- **Suggested direction** — Either the recoverable fieldset gains a head select (recoverables already carry a project for EMD/PBG, so a head is a small extension), or `payments.head_id` becomes nullable for a recoverable and the grid's treatment filter carries the exclusion on its own. The first is smaller; the second is closer to what a deposit actually is.

---

### F-G-003 — The `payment` data scope is declared, editable, seeded and never read

- **Severity** — high
- **Confidence** — confirmed (reproduced through the UI with a custom role; and by grep — nothing calls `Scope(u, "payment")`)
- **Type** — permission
- **Where** — `internal/store/permissions.go:185` declares `payment` as a scoped resource; `internal/store/migrations.go:394,400` seeds `payment=all` for Accounts and Admin; the roles screen renders and stores its radios (`internal/app/app.go:1306-1315`); `ListPayments` (`internal/store/store.go:1256`) takes no viewer
- **Traces to** — UC-A-13 · R3 · TC-G-053
- **What happens** — An administrator sets a role's payment scope to "own". The setting is stored and re-rendered correctly. It changes nothing: `/payments` shows every payment in the system, including the payee and the name of whoever entered it, and the ledger search finds another accountant's payment by their own processing note.
- **Why it is wrong** — R3 is "data scope per resource (own/assigned/all)". The vocabulary declares the resource as scoped and the UI offers the control, so an administrator has every reason to believe they have narrowed access. They have not. The payment *detail* is protected — but only incidentally, because `paymentDetail` re-checks the **request** scope behind it (`internal/app/app.go:759`), which is a different rule that happens to cover the same ground.
- **Reproduction** —
  1. Create a role holding `payment:view` and `request:view`, with `scope_payment=own` and `scope_request=own`.
  2. Create a user holding only that role.
  3. As admin, settle a payment with a distinctive amount and processing note.
  4. Confirm on `/roles?role={id}` that the `own` radio for payment is checked.
  5. As the subject, `GET /payments?month={that month}` → 200, and the payment is listed with its amount, payee and "Entered by".
  6. `GET /payments?month={m}&q={the admin's processing note}` → the same row.
- **Impact** — An administrator who narrows payment visibility gets no narrowing and no warning. Anyone with `payment:view` reads the whole ledger. The blast radius is bounded today because the ledger row carries no purpose or remarks column, but it does carry every amount, payee and reference in the company.
- **Evidence** — `F-G-003: payment scope "own" is never read, so the ledger shows a payment somebody else entered` → `₹13,100.00` present. `F-G-003: and who entered it` → "Fervid Admin" present.
- **Suggested direction** — Either give `PaymentListOptions` a `Scope`/`ViewerID` pair filtered on `entered_by`, or remove `payment` from the scoped resources so the roles screen stops offering a control that does nothing. The second is a smaller change and an honest one.

---

### F-G-017 — The audit log discloses the full text of every request to anyone holding `audit:view`

- **Severity** — high
- **Confidence** — confirmed (reproduced through the UI with a custom role; `Store.Audit`'s signature has no viewer)
- **Type** — information-flow
- **Where** — `internal/store/store.go:1591-1603` (`Audit` takes `entityType`, `entityID`, `limit` and no viewer); `internal/app/app.go:1364-1389` (`auditLog` filters only on entity, action and actor name)
- **Traces to** — UC-A-40 · R3, R6, C2 · TC-G-055
- **What happens** — `before_json` and `after_json` for a `payment_request` row are the entire `Request` struct, serialised (`internal/store/requests.go:705-709` passes `before` and `after` whole). `/audit` renders both in a `<details>` disclosure. A caller whose `request` scope is `own`, and who is correctly refused 403 on `/requests/{id}`, reads that request's number, short title, purpose, requester name, manager name, amount in paise, approved amount and every date off the audit log instead.
- **Why it is wrong** — C2 requires an audit history for request mutations; it does not require that history to be a bypass for Q5 and R6. Every other read path over `payment_requests` asks `canViewRequest`. This one does not, and it carries strictly more data than the screens that do.
- **Reproduction** —
  1. Create a role holding `audit:view` and `request:view`, with `scope_request=own`. Create a user holding only it.
  2. As a different requester, raise a request with a distinctive purpose, and have a manager approve it (an approve row carries the whole struct in Before and After).
  3. As the subject, `GET /requests/{id}` → 403.
  4. `GET /audit?entity=payment_request&actor={the approver's name}` → 200, and the row's disclosure contains the request number, the purpose verbatim, the requester's name and the amount in paise.
- **Impact** — Latent today: `audit:view` is granted to Admin alone in the seed, and an admin holds `request=all` anyway. But the audit log is exactly the permission a reviewer or an auditor would be given without payment or request access, and the grant that reads "may review who changed financial records" silently confers "may read the full text of every request ever raised".
- **Evidence** — `F-G-017: before_json carries the whole Request struct, purpose included` → the purpose string was present in the response body, as were the requester's name and `6400000`.
- **Suggested direction** — Either scope `/audit` by the caller's `request` scope for `payment_request` rows, or stop serialising whole structs into `before_json`/`after_json` and record the changed columns only — which `diffRequestAudit` already knows how to derive for the thread.

---

### F-G-018 — The dashboard's accounts area hardwires scope `all` and lists requests the caller cannot open

- **Severity** — high
- **Confidence** — confirmed (reproduced through the UI with a custom role; the literal is on one line)
- **Type** — information-flow
- **Where** — `internal/app/dashboard.go:96` — `store.RequestListOptions{Scope: "all", Statuses: []string{"approved"}}`, where every other area on the same screen either narrows (`"own"`, `"assigned"`) or would have asked `effectiveScope`
- **Traces to** — UC-B-32 · R3, R6, D2, Q5 · TC-G-056
- **What happens** — The "Approved and unclaimed" work area, gated on `payment:process`, is built with the scope string `"all"` written into the call rather than resolved from the caller. Its tile count and its four listed rows are therefore company-wide for anyone holding that verb, whatever their `request` scope says. Each row carries the request number, its short title and its amount, and links to a detail page the same caller is refused.
- **Why it is wrong** — D2 says only permitted areas appear and R6 says permissions govern data server-side. The accounts **queue** — the same dataset, one screen away — does it correctly: `accountsQueue` passes `a.auth.Scope(u, "request")` (`internal/app/linking.go:159`). Two screens over one dataset, one of which forgot to ask.
- **Reproduction** —
  1. Create a role holding `payment:process` and `request:view`, with `scope_request=own`. Create a user holding only it.
  2. As a different requester, raise a request with a distinctive title, and have it approved.
  3. As the subject, `GET /requests/{id}` → 403.
  4. `GET /` → 200, and the request's number, short title and amount are on the page.
  5. `GET /accounts-queue?tab=approved` → 200, and the row is correctly **absent**.
- **Impact** — Anyone holding `payment:process` reads the number, title and amount of every approved request in the company from the home page, even when their data scope was deliberately narrowed. The dashboard is the first screen after login, so the disclosure is unavoidable rather than something the user has to go looking for.
- **Evidence** — `F-G-018: the accounts work area hardwires Scope:"all", so it lists a request the caller cannot open` → the number, the title and `₹55,500.00` were all present in the dashboard body, and absent from the queue.
- **Suggested direction** — Replace the literal with `a.effectiveScope(u, "")`, which is the helper the requests list already uses and which cannot widen.

---

### F-G-028 — An unbudgeted month cannot be saved at all: the budgets screen refuses the values it rendered

- **Severity** — high
- **Confidence** — confirmed (reproduced through the UI three ways: unchanged, one field filled, and reducing an existing budget)
- **Type** — validation
- **Where** — `internal/app/templates.go:1160` renders `value="{{money .Budget}}"` for every head, so an unbudgeted head shows `₹0.00`; `internal/money/money.go:31-33` rejects `paise <= 0`; `internal/app/app.go:1025-1030` sets `parseErr` and abandons the **whole batch** when any one field fails
- **Traces to** — UC-A-32, UC-A-33 · C3 · TC-G-041
- **What happens** — Open `/budgets?month={any month the seed did not budget}`. Every field reads `₹0.00`. Press "Save Budgets". 400, "invalid budget amount for one or more heads". Fill one head in and press Save: still 400, because the other eight are still `₹0.00` and the batch is rejected whole. There is no incremental path and no way to enter the first budget of a month through this screen. The same rule means an existing budget can never be reduced to zero.
- **Why it is wrong** — A screen must not refuse the values it just rendered. `money.ParsePaise` rejects zero because it is the right rule for a *payment* amount — you cannot pay nothing — but a budget of zero is a real and meaningful figure, and "this head has no budget this month" is the state the screen displays by default.
- **Reproduction** —
  1. As admin, `GET /budgets?month=2028-06`. Every `input[name^="budget_"]` holds `₹0.00`.
  2. `POST /budgets` with `month=2028-06` and every one of those fields at `₹0.00` — i.e. exactly what the form would send. → 400, "invalid budget amount for one or more heads".
  3. Repeat with one field set to `25,00,000.00` and the rest unchanged. → 400. Reload: the field is `₹0.00` again; nothing was written.
  4. `GET /budgets?month=2026-06` (seeded, so every figure is real). POST it back with one head changed to `0.00`. → 400, and the old figure stands.
- **Impact** — The monthly budgeting workflow is unreachable through the UI for any month that is not already fully budgeted, and for any month in which a head was added after the budgets were set. The seed sidesteps it entirely by calling `SetBudget` directly, which is why it has never been noticed. An operator opening next month for the first time cannot enter a single figure.
- **Evidence** — `F-G-028: saving the values the screen rendered is refused` → 400, body contained "invalid budget amount". `F-G-028: one head at a time does not work either` → 400. `F-G-028: an existing budget can never be reduced to zero` → 400.
- **Suggested direction** — `budgetSave` should treat a zero as a zero: parse with a budget-specific parser that accepts `>= 0`, and skip empty fields rather than failing the batch. `SetBudgets` already validates `Amount < 0` separately (`internal/store/store.go:333`), which suggests zero was always meant to be legal there.

---

### F-G-032 — `/grid` is `RequireLogin` only, so a subject with no role reads every budget, actual, payee and payment link

- **Severity** — high
- **Confidence** — confirmed (reproduced through the UI with a role-less subject; the route registration is one line)
- **Type** — permission
- **Where** — `internal/app/app.go:377` — `mux.Handle("GET /grid", a.auth.RequireLogin(...))`, against `:394` where the CSV of the same data is `RequirePermission("grid","export")`
- **Traces to** — UC-A-42, UC-A-43 · R6, D2 · TC-G-057
- **What happens** — A user holding no role at all — which is what an administrator gets by unticking every box — is refused `/requests`, `/payments`, `/reports/monthly`, `/audit`, `/users` and `/vendors`, and served `/grid` with a 200. That page carries every project and head, every budgeted figure, every actual, and a "Recent Payments" panel listing amounts, payees and `href="/payments/{id}"` links. The linked detail is correctly 403, and `/export.csv` of the identical data is correctly 403.
- **Why it is wrong** — R6 requires permissions to govern data server-side, and `grid:view` exists in the vocabulary precisely to gate this screen. Gating the download of a dataset while serving the dataset itself is not a defence. The inconsistency is the tell: the same numbers are protected in one representation and open in the other.
- **Reproduction** —
  1. As admin, create a user and untick **every** `role_ids` checkbox (a new user is given Accounts by default, so this step is essential).
  2. Sign in as them. `GET /requests`, `/payments`, `/reports/monthly`, `/audit`, `/users`, `/vendors` → 403 each.
  3. `GET /grid?month=2026-06` → 200. The payroll budget `₹8,50,000.00` is on the page, as is every other head.
  4. Settle a payment in another month as admin, then `GET /grid?month={that month}` as the role-less user → the exact amount and the payee are on the page, and the Recent Payments rows link to `/payments/{id}`.
  5. `GET /export.csv?month={that month}` → 403.
- **Impact** — The company's whole budget plan and its month-by-month spend, plus the payee and amount of every recent payment, are readable by any authenticated account regardless of what it was granted. For a finance application that is the central confidential dataset.
- **Evidence** — `F-G-032: /grid is RequireLogin only` → 200. `F-G-032: including the exact amount of a payment they hold no permission to see` → `₹66,000.00` present. `F-G-032: and every budget figure` → `₹8,50,000.00` present. The CSV of the same month → 403.
- **Suggested direction** — Gate `GET /grid` on `grid:view`, which every role that should reach it already holds (Manager, Accounts, Admin). Note that a Requester holds no `grid:view`, so this is a deliberate decision about whether requesters see the company plan, not a mechanical change — but the current state is not a decision, it is an omission.

---

### F-G-034 — `POST /users` spans three transactions, so a refused save still commits the rename

- **Severity** — high
- **Confidence** — confirmed (reproduced through the UI; the three calls are on adjacent lines with no envelope)
- **Type** — data-integrity
- **Where** — `internal/app/app.go:1197` (`UpdateUser`), `:1213` (`SetUserRoles`), `:1217` (`SetUserDefaultApprover`) — three independent store calls, each opening and committing its own transaction
- **Traces to** — UC-A-20 · R4 · TC-G-088
- **What happens** — One form, one submit, three transactions. A failure in the second leaves the first committed. `SetUserRoles` validates that every role id exists inside its own transaction (`internal/store/permissions.go:471-479`), so a save that renames a user *and* assigns a role that does not exist returns 400 — after the rename has already been written.
- **Why it is wrong** — The operator is shown a refusal and reasonably concludes nothing happened. Half of what they asked for happened. R4 makes role assignment the thing that governs access, so a half-applied user save is a half-applied access change.
- **Reproduction** —
  1. Create a user holding exactly the Requester role. Note that `/users` shows one role pill for them.
  2. Read their id from the Edit button's `data-open="user-{id}"` attribute.
  3. `POST /users` with `id={that id}`, `email={theirs}`, `name=Renamed By A Failure`, `role=data_entry`, `active=on`, `role_ids=99999999`, `default_approver_id=0`.
  4. 400, "role 99999999 does not exist".
  5. Reload `/users`. The Name cell reads "Renamed By A Failure". The Roles cell still reads "Requester".
- **Impact** — Any user save that trips the role validation, the default-approver guard or a unique constraint leaves the database in a state neither the operator nor the audit log describes. The audit row is written last (`internal/app/app.go:1227`), so the partial change is not audited at all.
- **Evidence** — `F-G-034: UpdateUser committed before SetUserRoles failed, so the rename stuck` → the row showed the new name. `F-G-034: while the role assignment the same POST asked for did not happen` → the pill was unchanged.
- **Suggested direction** — Add a store method that takes the whole user save — profile, roles and default approver — and does it in one transaction, the way `CreateRequest` already handles a request and its attachments together.

---

### F-G-035 — A request edit whose resubmit fails still writes the edit and audits it as done

- **Severity** — high
- **Confidence** — confirmed (reproduced through the UI; the three calls are on adjacent lines)
- **Type** — data-integrity
- **Where** — `internal/app/requests.go:657-665` — `UpdateRequest`, then `AddRequestAttachment`, then `SubmitRequest`, three transactions, one form
- **Traces to** — UC-B-18, UC-B-19 · A8, C2 · TC-G-089
- **What happens** — The requester corrects a request and resubmits. `UpdateRequest` commits the correction and writes an `update` audit row. `SubmitRequest` then fails, and the requester is returned to the correction form with an error. The correction is in the database and in the history; the requester has been told it failed.
- **Why it is wrong** — Same class as F-G-034 and worse in one respect: the audit log records the edit as a completed mutation, so the history asserts a change the caller was told had not happened. C2's whole value is that the trail is what actually occurred.
- **Reproduction** —
  1. As a Requester, raise a pending reimbursement for `2000.00`.
  2. `POST /requests/{id}/edit` with the same fields, `amount=31000.00`, and `submit_action=resubmit`. (A `pending` request has no `pending` edge in `legalTransitions`, `internal/store/requests.go:91-101`, so `SubmitRequest` refuses.)
  3. 400, "a pending request cannot be submitted".
  4. `GET /requests/{id}` → the head amount reads `₹31,000.00`.
  5. `GET /audit?entity=payment_request&id={id}` → an `update` row is present.
- **Impact** — A requester who resubmits at the wrong moment, or whose attachment upload fails, sees a refusal and does not know their figures changed. The approver sees the new figures with no indication anything went wrong. The reachable trigger through the shipped UI is the attachment path — with "Require attachments" on and no file attached, `SubmitRequest`'s policy check fails after `UpdateRequest` has committed.
- **Evidence** — `F-G-035: the edit committed even though the POST that carried it was refused` → `₹31,000.00`. `F-G-035: the audit records the edit as done, while the caller saw a 400` → an `update` row was present.
- **Suggested direction** — Give the store one `UpdateAndResubmit` entry point that does all three inside a transaction, or reorder so `SubmitRequest`'s preconditions are checked before `UpdateRequest` writes.

---

### F-G-022 — A role assigned to users is deleted with no pre-check, silently stripping it from its holders

- **Severity** — high
- **Confidence** — confirmed (reproduced through the UI, including the holder's next request)
- **Type** — data-integrity
- **Where** — `internal/store/migrations.go:69` — `user_roles.role_id … ON DELETE CASCADE`; `DeleteRole` in `internal/store/permissions.go` has no assignment check; `roleDelete` (`internal/app/app.go:1356-1362`) redirects on success
- **Traces to** — UC-A-14, UC-A-15 · R9, R1 · TC-G-080
- **What happens** — Deleting a custom role that is currently assigned to users succeeds with a clean 303. The cascade removes every `user_roles` row. The role pill disappears from every holder, and each holder loses the permissions it carried on their very next request. Nothing warns the administrator, and the roles screen shows the holder count immediately above the Delete button it does not consult.
- **Why it is wrong** — R9 permits deleting a non-system role; it does not say the deletion should be silent about its consequences. The screen already knows the number (`{{index .RoleUserCounts .Role.ID}} users`), so the information needed for a confirmation is on the page and unused.
- **Reproduction** —
  1. Create a custom role holding `payment:view`. Assign it as a user's only role.
  2. As that user, `GET /payments` → 200.
  3. As admin, `POST /roles/{id}/delete` → 303, no warning.
  4. `/users` → the role pill is gone from that user's Roles cell.
  5. As that user, `GET /payments` → 403.
- **Impact** — An administrator tidying up roles can revoke access for an arbitrary number of people with one click and no indication of how many. In the worst case a role held by everyone who processes payments is removed and the queue stops working, with nothing on screen to connect cause and effect.
- **Evidence** — `F-G-022: an assigned role is deleted with a clean 303 and no warning at all` → 303. `F-G-022: the holder silently loses the permission on their very next request` → 403.
- **Suggested direction** — Refuse the delete with a named count when the role is assigned, or require an explicit "reassign these N users first" step. The count is already computed for the screen.

---

### F-G-024 — Deactivating a head permanently strands its approved requests, with no warning

- **Severity** — high
- **Confidence** — confirmed (reproduced through the UI end to end)
- **Type** — data-integrity
- **Where** — `headSave` (`internal/app/app.go:1107-1111`) has no check for live requests; `validatePayment` (`internal/store/store.go:1632-1639`) then refuses an inactive head
- **Traces to** — UC-A-29, UC-A-34 · T12 · TC-G-081
- **What happens** — A head with an approved, unpaid request against it is deactivated with a clean 303. The request keeps its head name for history, which is correct. It can then never be paid: `validatePayment` requires the head to be active, so the settlement is refused with "project or head is inactive". The refusal itself is coherent — a 400 with a sentence, not a 500 — but nothing at deactivation time said this would happen.
- **Why it is wrong** — T12 says inactive heads are hidden from new entries and retained on historical ones. An approved-but-unpaid request is neither: it is a live obligation. Retiring the head converts it into an obligation with no way to discharge it and no route to move it.
- **Reproduction** —
  1. Create a head under Operations. Raise a vendor invoice against it and have it approved.
  2. `POST /heads` with that head's `id`, its name, and **no** `active` field → 303, no warning.
  3. `GET /requests/{id}` → the Head row still names it.
  4. Take the request for processing, fill the entry form, confirm → 400, "project or head is inactive". No payment.
- **Impact** — An administrator retiring a spend head at month end can silently render every approved request against it unpayable. The only remedy is to reactivate the head, which is not discoverable from the refusal message.
- **Evidence** — `F-G-024: a head with an approved request against it is deactivated with no check and no warning` → 303. `F-G-024: the request is stranded, and the app says why in a sentence rather than 500ing` → the sheet's error banner matched `/inactive/i`; no 5xx was recorded.
- **Suggested direction** — Warn on deactivation with the count of live requests, the way F-G-022 should for roles. Alternatively let `validatePayment` accept an inactive head when the payment is settling a request that was approved while it was active — the request already carries the head id.

---

### F-G-025 — Deactivating an approver strands their pending approvals with no route to reassign them

- **Severity** — high
- **Confidence** — confirmed (reproduced through the UI, including the failed sign-in and the absence of a reassignment route)
- **Type** — state-machine
- **Where** — `userSave` (`internal/app/app.go:1182-1199`) has no check for pending approvals; `ReassignRequest` exists in the store (`internal/store/requests.go:759`) and is on no HTTP route — the only registered reassign is `POST /requests/{id}/reassign`, which is `reservation:reassign` and moves a *reservation*
- **Traces to** — UC-A-20, UC-B-31 · A7 · TC-G-083
- **What happens** — A manager with pending approvals is deactivated with no check. The requests still name them and still show "Awaiting" for ever. They cannot sign in to clear the queue. No route reassigns a pending approval to anyone else. Every screen that touches the request continues to render — nothing 500s — so the requests simply sit there.
- **Why it is wrong** — A7 is "Admin reassign (reason + history)", which UC-B-31 already records as unreachable. This is the consequence of that gap meeting an ordinary administrative action: deactivating a departing employee.
- **Reproduction** —
  1. Create a Manager subject. As someone else, raise a request routed to them.
  2. On `/users`, open their edit sheet, untick **Active**, save.
  3. `/users` → their Status cell reads "Inactive".
  4. `GET /requests/{id}` → the Approver row still names them; the status is still "Awaiting".
  5. Try to sign in as them → back to `/login`.
  6. `POST /requests/{id}/reassign-approval` and `/approval-reassign` → 404/405. There is no such route.
- **Impact** — Every request routed to a departing employee is frozen at the moment their account is closed. The requester cannot withdraw an approved request, and nobody can approve, return or reject a pending one. The only remedy is to reactivate the account.
- **Evidence** — `F-G-025: the approver is deactivated with no check for the approvals waiting on them` → "Inactive". `F-G-025: a deactivated approver cannot sign in to clear their queue` → `/login`. Both probe paths returned 404/405. Four screens all rendered below 500.
- **Suggested direction** — Put `ReassignRequest` on a route gated on a new `approval:reassign` (or on `approval:reject`, which an admin already holds), and warn on deactivation with the pending count.

---

### F-G-027 — A request aimed at an id that does not exist answers 500, not a 4xx

- **Severity** — high
- **Confidence** — confirmed (reproduced two ways: through Playwright, and with `curl` against a scratch server on a separate port and database — all four id combinations answered 500, and the log carried `constraint failed: FOREIGN KEY constraint failed (787)`)
- **Type** — validation
- **Where** — `classify` (`internal/store/store.go:1712-1720`) recognises `UNIQUE` and nothing else; `storeErrorStatus` (`internal/app/http_errors.go:199-212`) returns 500 for an unclassified error; `validateRequestInput` (`internal/store/requests.go:161-172`) checks `> 0` but never existence
- **Traces to** — UC-B-41 · R6 · TC-G-084 (annotated `test.fail()`)
- **What happens** — `PRAGMA foreign_keys=ON` rides on the DSN and correctly prevents the write, so no orphan row is created — the integrity guarantee holds. But the FK violation is never classified, so the requester gets a 500 error page reading "Something went wrong while processing your request." No driver text leaks, which is right; the status code and the message are wrong.
- **Why it is wrong** — A caller supplying a bad id has made a client error, and R6's "permissions govern data server-side" implies the server distinguishes a refusal from a fault. A 500 also means the error is logged as a server fault, so a client hammering bad ids pollutes the operational signal.
- **Reproduction** —
  1. As admin, note a real vendor id from the request form's combobox.
  2. `POST /requests` with `type=vendor_invoice`, `treatment=budget`, a valid `manager_id`, valid text fields, and `project_id=99999999`, `head_id=99999999`, `vendor_id={the real one}`. → 500.
  3. Repeat with `project_id=1`, `head_id=99999999` → 500. And with `project_id=1`, `head_id=1`, `vendor_id=99999999` → 500.
  4. The server log carries `constraint failed: FOREIGN KEY constraint failed (787)` for each.
- **Impact** — Bounded: reaching it needs a hand-built POST, because the form's selects only offer real ids. The consequence is a misleading error page, a mis-logged fault, and — contrast with `validatePayment`, which checks the head exists *before* its transaction and returns a 400 with a sentence (TC-G-086) — an inconsistency in how the same class of mistake is handled on two adjacent paths.
- **Evidence** — Playwright: `F-G-027: a project that does not exist must be a 4xx the requester can act on, not a 500 — got 500`. curl: four probes, `HTTP 500` each, body `<p>Something went wrong while processing your request.</p>`, and no `FOREIGN KEY` text in the response.
- **Suggested direction** — Add a `FOREIGN KEY` branch to `classify` returning `ErrValidation` (SQLite's message is stable), or have `validateRequestInput`'s callers resolve project, head and vendor first — which `recoverableCategoryLink` already does for the category.

---

### F-G-029 — The grid's own filter form posts to `/`, so the grid cannot be filtered

- **Severity** — high
- **Confidence** — confirmed (reproduced through the UI for the filter form, the lock redirect, and two of the links)
- **Type** — UX
- **Where** — `internal/app/templates.go:136` — `<form class="toolbar" method="get" action="/" aria-label="Grid filters">`; `:1152` (months "Grid" action `href="/?month={{.Month}}"`); `:1159` (budgets "View grid"); `lockMonth`/`unlockMonth` redirect to `/?month=` and `GET /` is `a.dashboard` (`internal/app/app.go:375`), which reads no `month` parameter
- **Traces to** — UC-A-31, UC-A-42 · TC-G-042, TC-G-070
- **What happens** — `/` was the variance grid before Phase 4 made it the dashboard, and one form and three links still point there. Submitting the grid's own Month / Status / Search toolbar navigates to the dashboard, which ignores every one of those parameters — so the grid's filters are unreachable from the grid.
- **Why it is wrong** — A filter that navigates away from the thing it filters is not a filter. PROGRESS.md:380-383 records the lock half of this as a known pre-existing issue and proposes `/grid?month=` for both; the filter form and the two links are the rest of the same family and are not recorded there.
- **Reproduction** —
  1. `GET /grid?month=2026-06`. The filter form's `action` is `/`.
  2. Type "Office" into its search field and press **Apply**. The URL becomes `/?…`, the heading reads "Good day, …", and there is no `table.matrix.grid` on the page.
  3. `GET /?month=2026-06&q=Office&status=over` → 200, and the body contains neither "2026-06" nor a grid.
  4. `/budgets?month=2026-06` → the "View grid" link is `/?month=2026-06`. `/months` → the "Grid" action is `/?month=…`. Following it delivers no grid.
- **Impact** — The variance grid — the application's headline screen — cannot be filtered by month, status or search text through its own controls.
- **Evidence** — `F-G-029: the grid filter form posts to /, which is the dashboard` → `action="/"`. `F-G-029: the filtered grid is nowhere on the page the filter delivered` → 0 grid tables. `F-G-029: the "Grid" action does not deliver a grid` → 0 grid tables.
- **Suggested direction** — Change all four to `/grid`. It is a one-token change in each place, and `/grid` already reads `month`, `status` and `q`.

---

### F-G-002 — 403-vs-404 lets a requester enumerate which request ids exist

- **Severity** — medium
- **Confidence** — confirmed (reproduced through the UI; the ordering is explicit in the helper)
- **Type** — information-flow
- **Where** — `internal/app/requests.go:229-240` — `loadViewableRequest` calls `a.st.Request(...)` and only then `canViewRequest`
- **Traces to** — UC-B-42 · R6, Q5 · TC-G-051
- **What happens** — `GET /requests/{id}` for an id that exists but is out of scope answers **403**. For an id that does not exist it answers **404**. The two are trivially distinguishable, so a requester can walk the id space and learn exactly which request ids exist, and by extension how many requests the company raises and at what rate.
- **Why it is wrong** — Q5 and R6 confine a requester to their own rows. The existence of a row is itself information about those rows, and a scope check that fires after the read leaks it through the status code. The refusal body is clean — TC-G-050 confirms no field of the request is echoed — so this is the whole of the leak, but it is a real one.
- **Reproduction** —
  1. Create two exactly-one-role Requester subjects. As the second, raise a request; note its id.
  2. As the first, `GET /requests/{that id}` → 403.
  3. `GET /requests/99999999` → 404.
- **Impact** — Low individually, cumulative in aggregate: an insider can enumerate the request id space and infer volume and growth. It also holds on `/requests/{id}/edit`, `/submitted`, `/partial-review` and `/reservation`, all of which route through the same helper.
- **Evidence** — `F-G-002: 403 and 404 differ, so the status code is an existence oracle for every request id` → 403 and 404 respectively.
- **Suggested direction** — Answer 404 for an out-of-scope row as well, so the two cases are indistinguishable. The audit log still records the attempt.

---

### F-G-004 — The audit log's Entity filter cannot reach `payment_request`

- **Severity** — medium
- **Confidence** — confirmed (reproduced through the UI; the option list is a literal)
- **Type** — UX
- **Where** — `internal/app/templates.go:3262` — the Entity `<select>` offers payment, budget, budget_month, month_lock, project, head, user, report and backup; the Action `<select>` offers ten actions
- **Traces to** — UC-A-40 · C2 · TC-G-027
- **What happens** — Every request, approval, reservation and settlement writes `entity_type='payment_request'`, and there is no option for it. Nor for `vendor`, `role`, `notification`, `recoverable_category` or `mail_settings`. The Action list likewise omits `submit`, `approve`, `reject`, `return`, `withdraw`, `reraise`, `process`, `settle`, `mark_partial`, `accept_partial`, `hold`, `unhold`, `reassign` and `cancel` — the entire Phase-2 and Phase-3 vocabulary.
- **Why it is wrong** — C2 requires an audit history for request mutations, and the screen exists to make that history reviewable. The rows are written correctly and reachable by hand-typing `?entity=payment_request`, but the only interface offered cannot ask for them. `/audit` is also capped at the newest 1000 rows of the selected entity *before* filtering (`internal/app/app.go:1368`), so on a busy database the workflow rows are the ones pushed out.
- **Reproduction** — `GET /audit`. Read the Entity and Action option lists. No option contains "request"; no option contains "approv".
- **Impact** — An auditor cannot review the payment-request workflow from the audit screen, which is the screen whose stated purpose is "Review who changed financial records, when, and why".
- **Evidence** — `F-G-004: no Entity option reaches payment_request` → no match. `F-G-004: no Action option reaches approve either` → no match. The rows themselves were present via the query string.
- **Suggested direction** — Build both option lists from the entity and action strings the store actually writes, rather than from a hand-maintained literal.

---

### F-G-006 — The "My open requests" tile omits `returned`, which the bucket it links to includes

- **Severity** — medium
- **Confidence** — confirmed (reproduced through the UI; both predicates read directly)
- **Type** — information-flow
- **Where** — `internal/app/dashboard.go:77` counts `Statuses: {"pending","approved","cancellation_requested"}`; the tile links to `/requests?bucket=open`, whose bucket predicate additionally includes `returned`
- **Traces to** — UC-B-32 · D1, D4 · TC-G-030
- **What happens** — A requester with one pending and one returned request sees "My open requests: 1" and, on clicking it, two rows. The tile and its destination are two different queries over the same data.
- **Why it is wrong** — D4 is "queues show counts". A count that does not match what clicking it shows is worse than no count, because the reader has no way to know which is right. The requests list itself gets this right — `a.requests` computes every tab count from the same options object with only the bucket changed, and comments that "a tab can never promise a number the list does not show". The dashboard builds a second, different options object.
- **Reproduction** —
  1. As a Requester, raise two reimbursements. Have a Manager return one.
  2. `GET /` → "My open requests" reads 1.
  3. Click it. `/requests?bucket=open` shows 2 cards.
- **Impact** — A requester chasing their own work is told the wrong number on the first screen they see. The same tile is where they judge whether anything is outstanding.
- **Evidence** — `F-G-006: "My open requests" omits returned, which /requests?bucket=open includes` → tile 1, bucket 2. The needs-me tile and its bucket did agree, which isolates the cause to the status set.
- **Suggested direction** — Have the dashboard call `CountRequests` with `Bucket: "open"` — the same bucket the tile links to — instead of an explicit status list.

---

### F-G-007 — Two screens carry the label "Approved, unclaimed" and show different numbers

- **Severity** — medium
- **Confidence** — confirmed (reproduced through the UI, before and after placing a hold)
- **Type** — information-flow
- **Where** — `internal/app/dashboard.go:96` counts `Statuses: {"approved"}` with no `on_hold` test; `internal/store/store.go:1113` counts `status='approved' AND processing_by IS NULL AND on_hold=0`
- **Traces to** — UC-B-32, UC-C-01, UC-C-13 · D4, L7 · TC-G-031
- **What happens** — With nothing on hold the two agree. Place a hold on one approved request — which leaves `status='approved'` by design — and the accounts queue's metric drops by one while the dashboard's stays put. Two tiles, the same words, different numbers.
- **Why it is wrong** — `on_hold=1` implies `status='approved'`, which is exactly why a status-only count is wrong. L7 makes a held request unavailable to Accounts; a tile that still counts it as "unclaimed" is describing work that cannot be claimed. The dashboard's count also omits the `processing_by IS NULL` test, so a reserved request stays in it too.
- **Reproduction** —
  1. Create two approved requests. Read the "Approved, unclaimed" tile on `/` and the metric of the same name on `/accounts-queue`. They agree.
  2. `POST /requests/{one id}/hold` with a reason.
  3. Read both again. The queue's is one lower; the dashboard's is unchanged.
- **Impact** — An accountant reading the home page is told there is work waiting that the queue will not offer them. With several holds outstanding the two numbers drift steadily apart.
- **Evidence** — `F-G-007: the dashboard tile has no on_hold test, so it still counts the held request` → dashboard unchanged, queue −1. `F-G-007: two screens carrying the identical label … must not show different numbers` → they did.
- **Suggested direction** — Have the dashboard's accounts area read `LinkableCounts.Approved` from `LinkablePaymentRequests`, which is the number the queue shows and already spans the caller's scope — fixing F-G-018 at the same time.

---

### F-G-009 — Vendor "Paid this year" is an exact-name match, so a rename silently zeroes it

- **Severity** — medium
- **Confidence** — confirmed (reproduced through the UI; the rename is the decisive test)
- **Type** — data-integrity
- **Where** — `internal/store/vendors.go` — the figure joins `payments.vendor_payee` to `vendors.name` because `payments` has no `vendor_id` column; PROGRESS.md:384-387 records it
- **Traces to** — UC-A-35, UC-A-38 · TC-G-035
- **What happens** — A vendor with one `₹9,100.00` payment shows `₹9,100.00`. Rename the vendor — the same row, the payment untouched — and the figure becomes `₹0.00`. The payment still exists and still shows the old payee in the ledger.
- **Why it is wrong** — The link between a payment and a vendor is `requests.vendor_id` → `vendors.id`, and the report resolves it by string equality on a denormalised snapshot instead. Any rename, any trailing-space difference, any reimbursement that happens to share a name breaks or corrupts the total.
- **Reproduction** —
  1. Create a vendor. Raise, approve and settle a request against it for `9100.00`, dated in the current calendar year (the SQL filters `substr(paid_on,1,4)` against the server's year).
  2. `/vendors?q={name}` → "Paid this year" reads `₹9,100.00`.
  3. Open the vendor, change its name, save.
  4. The ledger row is unchanged and still shows the old payee.
  5. `/vendors?q={new name}` → "Paid this year" reads `₹0.00`.
- **Impact** — Vendor spend totals are unreliable, and unreliable in a direction that under-reports. An administrator correcting a vendor's legal name silently erases its payment history from the vendor master.
- **Evidence** — `the vendor total picks the payment up by payee name` → `₹9,100.00`. `F-G-009: a rename silently orphans every payment made to the old name` → `₹0.00`.
- **Suggested direction** — Add `payments.vendor_id`, populated from the request at settlement (`RecordPaymentForRequest` already has the request in hand), and join on it. PROGRESS.md proposes the same.

---

### F-G-013 — Two nav badges are declared and no query ever populates them

- **Severity** — medium
- **Confidence** — confirmed
- **Type** — UX
- **Where** — `internal/app/nav.go:62-63` declares `Badge: "approvals"` and `Badge: "accounts_queue"`; `internal/store/badges.go:32-46` has three specs and neither of those keys
- **Traces to** — UC-A-08 · D4 · TC-G-039
- **What happens** — A manager with pending approvals sees no badge on the Approvals nav item. The two counts a person most needs at a glance are the two that were declared and never built. `BadgeCounts` also memoises for 15 seconds (`badges.go:14`), so the three badges that do work are stale for that window after a mutation.
- **Why it is wrong** — D4 is "queues show counts". The declaration says the design intended these two.
- **Reproduction** — As a Manager with ≥1 request in "To approve", read the nav's `/approvals` link. It carries no `.n` or `.badge` element.
- **Impact** — Managers and accountants have to open a queue to discover whether it has work in it, which is exactly what a badge exists to prevent.
- **Evidence** — `F-G-013: nav.go declares Badge:"approvals" and badges.go has no spec for it` → 0 badge elements with the queue non-empty.
- **Suggested direction** — Add two `badgeSpec` entries. Both are one scalar sub-select, and the pattern is already established.

---

### F-G-014 — `/requests` silently ignores `?status=` while its own CSV honours it

- **Severity** — medium
- **Confidence** — confirmed
- **Type** — information-flow
- **Where** — `internal/app/requests.go:474-476` builds `RequestListOptions` with `Bucket` and never reads `status`; `:930` (`requestsExport`) reads `Status: q.Get("status")`
- **Traces to** — UC-B-23, UC-B-24 · D3, R6 · TC-G-040
- **What happens** — `/requests?status=rejected` shows the default open bucket including pending requests. `/requests/export.csv?status=rejected` — the same query string — correctly returns nothing. The screen and its own export describe two different sets for one URL. The bare CSV also has no `bucket` default where the screen defaults to `open`, so `/requests/export.csv` with no parameters exports every status while `/requests` shows only open ones.
- **Why it is wrong** — D3 is "export lists/reports by permission", and the reasonable reading is that the export is the list. A shared URL that produces one set on screen and another in the file is a correctness problem for anyone reconciling the two. It is **not** a confidentiality problem: both apply `effectiveScope`.
- **Reproduction** —
  1. As a Requester with one pending request, `GET /requests?status=rejected` → the pending request is listed.
  2. `GET /requests/export.csv?status=rejected` → the request is absent.
  3. `GET /requests/export.csv` → the request is present.
- **Impact** — A filter that appears to be ignored, and an export that silently means something different from the screen it sits on.
- **Evidence** — `F-G-014: the screen ignores ?status= and shows the pending request anyway` → present. `F-G-014: the CSV for the same URL applies the filter` → absent.
- **Suggested direction** — Have `a.requests` read `status` into `RequestListOptions.Status` as the export does, and give the export the same `bucket` default the screen has.

---

### F-G-019 / F-G-020 / F-G-021 / F-G-026 — the locked-month family

Grouped because they are one workflow and were found in one pass (TC-G-070).

**F-G-019 — locking a month gives the operator no confirmation.** *Medium ·
confirmed.* `lockMonth` and `unlockMonth` redirect to `/?month={month}`, and
`GET /` is the dashboard, which reads no `month` parameter. The landing page
contains neither the month nor the reason typed. `/grid?month={month}` does show
both. Same root cause as F-G-029; PROGRESS.md:380-383 records it and proposes
`/grid?month=`. **Impact:** an operator may lock twice or believe the lock failed.
**Evidence:** the redirect Location was `/?month=2027-10`; the landing body
contained neither the reason nor the month string.

**F-G-020 — `/payments/new` shows no lock affordance.** *Medium · confirmed.* The
grid renders "Locked" in place of its Add buttons and `/budgets` renders a locked
banner, but the payment entry screen carries no lock indication at all. An
accountant fills in the amount, the date, the mode, the reference and the note,
presses through the settlement sheet and only then learns the month is closed.
The reservation does survive, so the work is not lost. **Evidence:**
`F-G-020: /payments/new shows no lock warning` → zero `.locked` elements with the
month locked.

**F-G-021 — requests are not lock-aware.** *Medium · confirmed.*
`internal/store/requests.go` contains no `IsLocked` call, so a request may be
raised **and approved** into a locked month with no warning at either step. It
becomes an approved obligation nobody can pay until the month is reopened.
Whether requests should be lock-aware is a spec question — the lock is documented
as a ledger lock — but the current behaviour lets an approval commit to a month
that is closed. **Evidence:** a request was created and approved into a locked
month; the settlement was then refused.

**F-G-026 — one condition, two status codes.** *Low · confirmed.* A locked-month
budget save answers **400**, because `budgetSave` renders its own error page for
every failure (`internal/app/app.go:1034-1043`) instead of routing through
`respondStoreError`, which maps `ErrLockedMonth` to **409**
(`internal/app/http_errors.go:189-190`). The user-visible message is correct
either way. **Evidence:** `F-G-026: a locked-month budget save answers 400 where
every other locked-month refusal answers 409` → 400, body contained "month is
locked".

The lock itself works: a settlement dated into a locked month is refused and
writes nothing, a budget save is refused, and after unlocking the identical
settlement succeeds with the figure intact.

---

### F-G-023 — Deleting a system role reports the wrong reason

- **Severity** — medium
- **Confidence** — confirmed
- **Type** — UX
- **Where** — `DeleteRole` returns an `ErrForbidden`-wrapped "system roles cannot be deleted"; `respondStoreError` (`internal/app/http_errors.go:191-192`) replaces the message with "You do not have permission to perform this action." for every `ErrForbidden`
- **Traces to** — UC-A-15 · R9 · TC-G-080
- **What happens** — An administrator — who holds every one of the 66 grants — tries to delete the Requester role and is told they lack permission. The real reason, which the store wrote, is discarded.
- **Why it is wrong** — R9 is "delete non-system; system roles protected". The protection works; its explanation does not survive the error mapping. Telling the holder of every permission that they lack permission sends them looking in the wrong place.
- **Reproduction** — As admin, `POST /roles/{a seeded role id}/delete` → 403, body contains "do not have permission" and not "system role".
- **Impact** — Misdirected troubleshooting on an access-control screen.
- **Evidence** — `F-G-023: the store says "system roles cannot be deleted" and the page says the admin lacks permission` → the body contained the generic message only.
- **Suggested direction** — Have `respondStoreError` use `friendly(err)` for `ErrForbidden` when the wrapped error carries a message, as it already does for `ErrValidation`.

---

### F-G-030 — Every CSV amount column is a formatted currency string, not a number

- **Severity** — medium
- **Confidence** — confirmed (all four exports)
- **Type** — UX
- **Where** — `internal/app/app.go:1453` and `:1485`, `internal/app/requests.go:943`, `internal/app/recoverables.go:115` — all four write `money.FormatPaise(...)`
- **Traces to** — UC-A-43, UC-A-45, UC-B-24, UC-C-34 · C3, D3 · TC-G-043
- **What happens** — Every amount cell is `"₹1,50,000.00"` — quoted, because the Indian grouping commas would otherwise break the field. No plain-number form of the figure appears anywhere in the file. `Number("₹1,50,000.00")` is `NaN`, so a spreadsheet or script has to strip a currency symbol and re-parse grouping before it can sum a column.
- **Why it is wrong** — The figure survives intact, which is what the money-trail cases assert, and for a human reading the file the formatting is a courtesy. But an export is the hop where the data leaves the application, and this one leaves as a rendered report. The variance percentage is a formatted string too (`"12.3%"`).
- **Reproduction** — Settle a payment for `150000.00`. Fetch `/export.csv?month={m}` — the Actual cell is `"₹1,50,000.00"` and no line contains `150000`. `Number()` of the cell is `NaN`. The same holds for `/reports/ytd.csv`, `/requests/export.csv` and `/recoverables/list.csv`.
- **Impact** — Anyone importing the export into a spreadsheet or a reconciliation script must clean every money column first. Also worth noting: three of the four exports omit `charset=utf-8` from their `Content-Type`, and `₹` is multi-byte, so a client that guesses an 8-bit encoding renders mojibake.
- **Evidence** — `F-G-030: {export} writes the amount as a quoted, symbol-bearing, comma-grouped string` → present in all three exports with rows. `F-G-030: Number("₹1,50,000.00") is NaN` → true.
- **Suggested direction** — Emit paise, or rupees to two decimals with no symbol and no grouping, and let the reader format. If the human-readable form is wanted, emit both columns. Add `charset=utf-8` to the three that lack it.

---

### F-G-036 — A 12-character letters-only password answers 500

- **Severity** — medium
- **Confidence** — confirmed (reproduced through the UI, including the one-digit control case)
- **Type** — validation
- **Where** — `validatePassword` (`internal/app/app.go:1555-1560`) checks length only; `auth.HashPassword` → `auth.ValidatePassword` (`internal/auth/auth.go:37-58`) additionally requires a letter **and** a digit
- **Traces to** — UC-A-19, UC-A-22 · TC-G-090 (annotated `test.fail()`), TC-G-091
- **What happens** — Creating or updating a user with `abcdefghijkl` passes the app-layer length check, reaches `HashPassword`, and fails there. The response is a 500 error page reading "The password could not be secured." The same password with one character replaced by a digit succeeds with a 303. No user is created by the failure, because the hash is computed before `CreateUser`.
- **Why it is wrong** — The application enforces a rule it does not state and reports breaking it as a server fault. `validatePassword` exists precisely to catch this at the boundary and returns the right shape (400 with a message) for the length rule; it just does not know about the other two.
- **Reproduction** —
  1. `POST /users` with `id=0`, a fresh email, a name, `role=data_entry`, `active=on`, `password=abcdefghijkl` → 500, "could not be secured".
  2. `/users` → no user with that email exists.
  3. Repeat with `password=abcdefghijk1` → 303.
- **Impact** — An administrator setting or resetting a password gets an error page implying the system is broken and no indication of what to change. The 500 is also logged as a server fault.
- **Evidence** — `F-G-036: the app-layer length check passes and auth.HashPassword then fails, so the response is a 500` → 500. `one digit is the whole difference between 303 and 500` → 303.
- **Suggested direction** — Have `validatePassword` call `auth.ValidatePassword` and return its message, so one rule set is enforced in one place and reported as a 400. The form's `minlength="12"` should gain a matching hint.

---

### F-G-005 — Workflow actions render as raw column values on `/audit`

- **Severity** — low
- **Confidence** — confirmed
- **Type** — UX
- **Where** — `internal/app/app.go:1882-1907` — `actionText` maps ten actions and returns the identifier unchanged for everything else
- **Traces to** — UC-A-40 · C2 · TC-G-027
- **What happens** — The Action pill reads `approve`, `submit`, `process`, `settle`, `mark_partial`, `accept_partial` — lowercase identifiers — beside `Created`, `Updated`, `Voided` and `Exported`, which are spelled for a reader. The inconsistency on one screen is what makes it read as unfinished rather than as a convention.
- **Why it is wrong** — The same mapping exists and is complete on the request thread (`auditPhrase`, `internal/app/linking.go:897`), so the vocabulary a reader needs is already written down elsewhere in the codebase.
- **Reproduction** — `GET /audit?entity=payment_request&actor={any approver}`. The Action column reads `approve`. `GET /audit?entity=user` on the same database reads `Created` / `Updated`.
- **Impact** — Cosmetic, on an evidence screen where legibility is the point.
- **Evidence** — `F-G-005: the Action pill prints the raw identifier "approve", where an audited "update" reads "Updated"` → `approve`. The contrast case found `Created`/`Updated` on the `user` entity.
- **Suggested direction** — Extend `actionText` from `auditPhrase`'s vocabulary, or have `/audit` use `auditPhrase` directly.

---

### F-G-010 — Vendor "Open requests" is always zero

- **Severity** — low
- **Confidence** — confirmed
- **Type** — data-integrity
- **Where** — `internal/store/vendors.go` — no query populates `Vendor.OpenRequests`; rendered at `internal/app/templates.go:1339`. PROGRESS.md:388 records it
- **Traces to** — UC-A-35 · TC-G-036
- **What happens** — The vendor list has an "Open requests" column and a footer total. Both are always `0`, including with a live pending request against the vendor.
- **Why it is wrong** — A column that renders Go's zero value is worse than an absent column: it asserts that there are no open requests.
- **Reproduction** — Create a vendor, raise a pending vendor invoice against it, `/vendors?q={name}` → the cell reads `0` and the footer reads `0`.
- **Impact** — An administrator deactivating a vendor consults exactly this column to see what it would strand, and is told nothing is outstanding.
- **Evidence** — `F-G-010: the vendor list promises an open-request count and no query ever fills it in` → `0` with a pending request present.
- **Suggested direction** — Either build the join, or remove the column until it exists.

---

### F-G-012 — The recoverables by-category drill-through cannot reproduce its own count

- **Severity** — low
- **Confidence** — confirmed
- **Type** — UX
- **Where** — `internal/app/templates.go:3459` links a category row to `/recoverables/list?q={{urlquery .Label}}`, where the list's own control is `?category={id}` (`:3514`) and `q` searches number, counterparty, project and requester
- **Traces to** — UC-C-33, UC-C-34 · TC-G-037
- **What happens** — Clicking "ICD — inter-corporate deposit" (count 1) searches the free-text field for that label, which appears in none of the columns `q` covers, and lands on a different number of rows.
- **Why it is wrong** — A drill-through that cannot reproduce the number it was clicked from is a broken link with a 200 status. The dashboard, list and CSV otherwise agree exactly, which is what makes this stand out.
- **Reproduction** — Raise and approve an ICD recoverable. `/recoverables` → note the by-category row's Count. Click the category link. `/recoverables/list?q=…` shows a different count.
- **Impact** — Minor navigational annoyance on a report screen.
- **Evidence** — `F-G-012: the by-category row promises {n} and its own link finds {m}` → the two differed.
- **Suggested direction** — Link to `?category={{.ID}}`, which the list already honours. The rollup would need to carry the category id.

---

### F-G-008 — The accounts-queue Approved badge understates the rows beneath it

- **Severity** — informational
- **Confidence** — confirmed
- **Type** — UX
- **Where** — `internal/store/store.go:1112-1114` (the badge is the strict takeable set) against `:1153` (the tab's rows are `status IN ('approved','processing')`)
- **Traces to** — UC-C-01 · S3, D4 · TC-G-032
- **What happens** — The Approved tab's badge counts approved · unclaimed · not-on-hold, and its rows include reserved and held requests too. The other four badges match their rows exactly. The store comments the reason: the picker needs to render `.co.is-taken` rows rather than silently hide a request someone is already paying.
- **Why it is wrong** — It probably is not. Recorded because the audit was asked to check strip-versus-tab agreement and this is the one place they differ, and because the rationale is a store-layer comment rather than anything on screen.
- **Reproduction** — Create two approved requests, reserve one, open `/accounts-queue?tab=approved`. The badge equals the number of "Take for processing" buttons and is less than the number of rows.
- **Impact** — A reader may think the tab is showing more than it counted. Mild.
- **Evidence** — `F-G-008: the Approved tab renders reserved rows its own badge excludes` → rows > badge; badge == takeable buttons.
- **Suggested direction** — If it stays, label it "Approved, unclaimed" like the metric strip does, so the number and the rows are visibly answering different questions.

---

### F-G-037 — The notification centre's row limit against an unbounded count

- **Severity** — informational
- **Confidence** — **unverified** (read from code; not exercised)
- **Type** — information-flow
- **Where** — `internal/store/inapp.go:100-104` (`ListNotifications` defaults to `LIMIT 100`) against `:134-142` (`NotificationCounts` has no limit)
- **Traces to** — UC-C-38, UC-C-39 · N1, G19 · TC-G-033 (adjacent; not covered)
- **What happens** — A user with more than 100 notifications would see an "All" count higher than the number of rows the centre lists.
- **Why it is wrong** — The same class as F-G-006: a count that does not match its own list.
- **Reproduction** — Would need 101 notifications for one user, which is minutes of setup for a divergence no operator will realistically meet. Not attempted.
- **Impact** — Negligible in practice.
- **Evidence** — None. Marked unverified accordingly.
- **Suggested direction** — Either paginate the centre or cap the count at the same limit, so the two cannot disagree.

---

## Observations that are not defects

**O-1 — a replayed request POST creates a second request.** By design. G6 is
explicit that legitimate repeat payments exist — the same rent, the same monthly
retainer — and the duplicate check is advisory: `requestDuplicateCheck` is a read
that answers 200 either way, and `POST /requests` neither calls it nor consults
it. TC-G-060 confirms two identical submissions produce two separately numbered
requests, which satisfies C4. There is no client-side double-submit guard either,
so a double click has the same effect. Recorded so a future reader does not
mistake it for a gap.

**O-2 — the settlement replay is genuinely safe.** `paymentCreate`
(`internal/app/app.go:713-716`) turns a failed insert into a redirect to the
payment that already exists, and `idx_payments_request` is the backstop. A double
confirm, a Back-button resubmit and a replayed POST all land on the same payment.
TC-G-062 confirms all three, and TC-G-063 confirms the same for a double reserve,
a double release and a double mark-all-read — with the losing accountant in a
reservation race getting IF7's stated **409** and the conflict screen.

**O-3 — the `fixtures.createApprovedRequest` "KNOWN GAP" comment is stale.** It
records that the payee does not reach the payment, that the queue's Payee column,
the entry screen's payee, the payment row's `vendor_payee` and the payment
detail's `<h1>` are all blank for a request raised through the real form. None of
that is true now: `paymentEntry` sets `VendorPayee: req.Vendor`
(`internal/app/linking.go:257-260`) and `payment_form` posts it as a hidden input
(`internal/app/templates.go:390`). TC-G-010 asserts the payee at eight consecutive
hops including the `<h1>`. PROGRESS.md's note about a fix at commit `fca6939` is
the accurate one; the fixture comment should be deleted, which is a change for
whoever owns `fixtures.ts` and not for this audit.
