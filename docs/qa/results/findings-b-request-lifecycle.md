# Findings — B · the payment-request lifecycle

From the audit of raising a request and everything the requester can still do to
it. Suite: `tests/e2e/audit-b-request-lifecycle.spec.ts` · cases:
`docs/qa/test-cases/TC-B-request-lifecycle.md`.

**Run:** `FERVID_E2E_PORT=4302 npx playwright test tests/e2e/audit-b-request-lifecycle.spec.ts --project=chromium --reporter=line`
→ **`1 skipped` / `98 passed` / 0 failed**, stable over two consecutive runs.
99 cases declared: 84 genuine passes, 14 carrying a confirmed defect under
`test.fail()`, 1 under `test.fixme()` because its race is intermittent.

No application code was changed. Every finding below was reproduced at least
twice — through the suite and again either with `curl` against a throwaway
server on a clean database, or by reading the code path that should have
prevented it.

| ID | Severity | Type | One line |
|---|---|---|---|
| F-B-11 | **critical** | information-flow | `GET /attachments/{id}` makes no ownership check at all, so a Requester can read the bank advice on any payment in the system. |
| F-B-09 | high | data-integrity | Every request document is undownloadable: the screens link a `request_attachments` id to a route that reads `payment_attachments`. |
| F-B-01 | high | validation | Any amount over about ₹9.22 × 10¹⁶ is silently clamped to int64 max instead of refused. |
| F-B-10 | high | data-integrity | Two people submitting at the same instant: one gets a 500 `SQLITE_BUSY` and loses the whole form. |
| F-B-16 | high | data-integrity | The request list and its CSV export stop at 200 rows in silence, while the tab beside them counts every one. |
| F-B-06 | medium | validation | A forged `vendor_id`, `manager_id`, `project_id` or `head_id` answers 500 and loses the typed form. |
| F-B-13 | medium | UX | The edit screen's vendor combobox can never return a result, so the vendor on a pending request cannot be changed. |
| F-B-05 | medium | data-integrity | A head from another project is accepted; the request then names one project and is charged to another's head. |
| F-B-15 | medium | data-integrity | The recoverable fields are stored and displayed on a budget expense. |
| F-B-03 | medium | validation | A head that is inactive — or was never active — is accepted when named directly in the POST. |
| F-B-07 | medium | validation | An inactive vendor is accepted the same way. |
| F-B-02 | medium | UX | Every refusal of the `recoverable` type reports the wrong reason and throws the requester's form away. |
| F-B-17 | medium | spec-divergence | An admin-added recoverable category is enforced but can never be selected, and the htmx fragment silently substitutes a different one. |
| F-B-04 | low | data-integrity | Invoice and advance-reason columns are stored and displayed on a reimbursement. |
| F-B-18 | low | UX | The list is sorted newest-first while the screen says it is sorted by who is holding requests up. |
| F-B-14 | informational | validation | No date is ever compared with today. |
| F-B-08 | informational | UX | Validation messages reach the user with a `validation failed: ` sentinel and, in one case, the wrong article. |
| F-B-12 | informational | test-fidelity | htmx silently drops a request whose element it has just replaced — a trap for anyone writing tests against the adaptive form. |

---

### F-B-11 — `GET /attachments/{id}` makes no ownership check, so any Requester can read any payment's proof

- **Severity** — critical
- **Confidence** — confirmed (reproduced two ways: the suite, and the handler's own code path)
- **Type** — information-flow
- **Where** — `internal/app/app.go:881–911` (`attachmentDownload`); route at `internal/app/app.go:393`
- **Traces to** — UC-B-12, UC-B-42 · R6, Q5 · TC-B-085
- **What happens** — `attachmentDownload` resolves the id, checks the stored path
  is inside `AttachmentDir`, checks the file exists, and serves it. It never asks
  who is calling. The route's only gate is `attachment:view`, which the seeded
  **Requester** role holds (`internal/store/migrations.go:352`). A Requester who
  is refused `/requests/{id}` with 403 and `/payments/{id}` with 403 is served
  the proof attached to that same payment with **200**.
- **Why it is wrong** — the payment detail handler refuses a caller whose request
  scope does not reach the row, and its own comment says why: *"Reading it
  therefore obeys the same row scope /requests/{id} obeys, or payment:view
  becomes a way around Q5/R6"* (`internal/app/app.go:755–762`). The file on that
  payment carries the same information — payee, amount, bank details on a
  transfer advice — and is not protected at all. R6 requires permissions to
  govern data server-side; Q5 says a requester sees only their own.
- **Reproduction**
  1. Sign in as an administrator and raise a request; have a Manager approve it.
  2. As an **Accounts** user, reserve it and record the payment, then upload a
     file on `/payments/{payId}` (*Attachments* → Upload).
  3. Read the `href` of its Download link — `/attachments/{n}`.
  4. Sign in as a user holding **exactly** the Requester role, who did not raise
     the request. Confirm `GET /requests/{id}` → 403 and `GET /payments/{payId}` → 403.
  5. `GET /attachments/{n}` → **200**, and the file is returned.
- **Impact** — every attachment in the database is readable by every signed-in
  user who holds `attachment:view`, by walking small integers. On this product
  that means bank advices, invoices and receipts belonging to other people's
  requests and payments. It is also an enumeration oracle: 200 versus 404 tells
  the caller exactly how many attachments exist.
- **Evidence** — `TC-B-085`: *“attachmentDownload must obey the same row scope
  /requests/{id} obeys; got 200 · Expected: not 200”*.
- **Suggested direction** — resolve the attachment's owning entity and apply the
  same `canViewRequest` test the payment detail already applies; for a request
  attachment, the request's own scope check.

### F-B-09 — every request document is undownloadable, because the link resolves against the wrong table

- **Severity** — high
- **Confidence** — confirmed (the suite, plus the two SQL statements involved)
- **Type** — data-integrity
- **Where** — `internal/app/templates.go:2393` and `:307` render
  `/attachments/{{.ID}}` for a `RequestAttachment`; `internal/store/store.go:1353–1361`
  (`AttachmentByID`) selects from `payment_attachments`
- **Traces to** — UC-B-12 · T10 · TC-B-084
- **What happens** — request attachments live in `request_attachments`
  (`internal/store/migrations.go:208`) with an autoincrement of their own; payment
  attachments live in `payment_attachments` (`internal/store/schema.go:77`) with
  another. `GET /attachments/{id}` reads only the payment table. The Download
  button the request detail renders therefore answers **404** whenever no payment
  attachment happens to share that id — and, when one does, serves **that
  payment's file instead**.
- **Why it is wrong** — the screen offers a control; a rendered control has to
  work. Worse, combined with F-B-11 the collision is not merely broken but
  actively wrong: a requester pressing Download on their own invoice can be
  handed a stranger's bank advice.
- **Reproduction**
  1. Raise any request with a document attached in the *Supporting document*
     uploader.
  2. Open `/requests/{id}` and press **Download** on the file row.
  3. **404 — “The requested attachment was not found.”** The file is on disk and
     the row is in `request_attachments`.
- **Impact** — T10's whole point is that supporting documents reach the approver
  and Accounts. Nobody can open one from a request screen. The two Phase-3
  screens that list request attachments beside payment ones
  (`internal/app/templates.go:303–309`) have the same bug and the same collision
  risk.
- **Evidence** — `TC-B-084`: *“the link the screen renders must serve the
  requester's own document; got 404 · Expected: 200”*.
- **Suggested direction** — either give request attachments their own route, or
  make `AttachmentByID` look in both tables and return which entity it found, so
  the scope check of F-B-11 has something to check against.

### F-B-01 — an amount too large for int64 paise is silently clamped instead of refused

- **Severity** — high
- **Confidence** — confirmed (the suite, plus three `curl` submissions on a clean database)
- **Type** — validation
- **Where** — `internal/money/money.go:29–33` (`ParsePaise`)
- **Traces to** — UC-B-05 · VA1, C3 · TC-B-018
- **What happens** — `ParsePaise` parses the input as a `float64`, multiplies by
  100 and converts to `int64`. On this platform that conversion **saturates**, so
  the result is `9223372036854775807` — a large positive number. The guard
  `if paise <= 0` is designed to catch a wrapped value and never fires. The
  request is created for **₹92,23,37,20,36,85,47,758.07**.
- **Why it is wrong** — `ParsePaise`'s own contract is *"Non-numeric, empty and
  non-positive amounts are rejected"*, and it explicitly rejects `NaN` and `Inf`
  two lines earlier. An input it cannot represent must not become a different
  number silently. C3 requires the currency to be `int64` paise throughout, which
  it is — but the value stored bears no relation to the value submitted.
- **Reproduction** (`curl`, clean database, admin session)
  1. `POST /requests` with `type=reimbursement`, a valid body and `amount=1e300`.
  2. **303 → /requests/1/submitted.**
  3. `GET /requests/1` → `₹92,23,37,20,36,85,47,758.07`.
  4. Repeat with `amount=9e18` and `amount=1e19`: all three store the identical
     clamped value, and the CSV export prints it too.
- **Impact** — a pasted or fat-fingered scientific-notation value creates an
  approvable request for a nonsense amount. `ApproveRequest` accepts any positive
  amount, so it can be approved and reach the Accounts queue; the dashboard
  metric strip and every total that sums amounts are then wrong by ~9.2 × 10¹⁸.
  Related, from the same parser: `1_000` is accepted as ₹1,000.00 because Go's
  `ParseFloat` allows underscores — harmless, but the same laxity.
- **Evidence** — `TC-B-018`: *“int64 overflow: expected 400, got 303 →
  /requests/9/submitted”*.
- **Suggested direction** — bound the parsed float before converting (reject
  anything whose paise value exceeds `math.MaxInt64`, and probably a much lower
  business ceiling), and reject scientific notation outright.

### F-B-10 — two simultaneous submits: one answers 500 and loses the form

- **Severity** — high
- **Confidence** — confirmed (5 of 6 full suite runs, 3 of 3 isolated runs, and once with a parallel `curl` burst)
- **Type** — data-integrity
- **Where** — `internal/store/requests.go:401–413` (`CreateRequest` opens a
  deferred transaction, then reads, then writes); the same shape in
  `ReraiseRequest` (`:810–826`)
- **Traces to** — UC-B-05 · C4 · TC-B-063, TC-B-093
- **What happens** — `CreateRequest` calls `BeginTx` (deferred), then
  `requestNumberYear` → `settingInTx`, which is a **SELECT**, and only then
  `NextRequestNumber`, which writes. SQLite will not upgrade a transaction that
  already holds a read snapshot into a writer while another writer is active, and
  for that specific case it does **not** call the busy handler — so
  `busy_timeout(5000)` never applies. The loser gets
  `database is locked (5) (SQLITE_BUSY)`, which `classify` does not recognise, so
  it becomes a **500** error page.
- **Why it is wrong** — `store.Open`'s own comment says the busy timeout is there
  because it *"is what makes concurrent reservations (ReserveRequest) fail with
  SQLITE_BUSY instead of waiting their turn"* (`internal/store/store.go:27–35`).
  That reasoning holds for `ReserveRequest`, whose **first** statement is the
  UPDATE (`internal/store/store.go:736`), and does not hold here.
- **Reproduction**
  1. Sign two users in, in two browsers.
  2. `POST /requests` from both at the same instant (`Promise.all`).
  3. One answers 303; the other answers **500** with
     `"error":"database is locked (5) (SQLITE_BUSY)"` in the server log.
  4. Also reproducible with `seq 1 10 | xargs -P 10 curl … /requests`, which
     logged the same error once.
- **Impact** — the busier the organisation, the more often somebody loses a
  filled-in request form to a generic *“Something went wrong”*. Nothing is
  corrupted — the transaction rolls back and no number is burned (TC-B-064, and
  TC-B-093 confirms no number is ever handed out twice) — but the work is gone.
  Annotated `test.fixme()` rather than `test.fail()` precisely because the loser
  sometimes wins.
- **Evidence** — `TC-B-063`: *“the second concurrent submit failed: 500”*, three
  isolated runs out of three.
- **Suggested direction** — begin the numbering transaction as a writer
  (`BEGIN IMMEDIATE`), or read the numbering settings before opening it. Mapping
  `SQLITE_BUSY` to a retry in `classify` would also stop it surfacing as a 500.

### F-B-16 — the list and its CSV export stop at 200 rows in silence

- **Severity** — high
- **Confidence** — confirmed (the suite, plus 214 requests created over `curl`)
- **Type** — data-integrity
- **Where** — `internal/store/requests.go:1271–1274` (`ListRequests` defaults
  `Limit` to 200); `internal/app/requests.go:928` (the export calls it);
  `internal/store/requests.go:1296` (`CountRequests` has no limit)
- **Traces to** — UC-B-23, UC-B-24 · D3, D4 · TC-B-099
- **What happens** — with 214 requests in scope, the **All** tab renders `214`
  and the list below it renders **200** cards. `GET /requests/export.csv` returns
  **200 data rows**. There is no pagination control, no "load more", and no
  warning anywhere on the page.
- **Why it is wrong** — D4 says queues show counts, and the counts are computed
  from the same options as the rows *"so a tab can never promise a number the
  list does not show"* (`internal/app/requests.go:482–483`) — but the count query
  has no LIMIT and the row query does. D3 makes the export a permission-gated
  feature; an export used for reconciliation that drops 14 rows without saying so
  is worse than no export.
- **Reproduction**
  1. Raise 214 requests (any type).
  2. `GET /requests?bucket=all` → the *All* tab reads **214**, the sub-line reads
     *“200 shown”*, and `.req-card` occurs **200** times.
  3. `GET /requests/export.csv?bucket=all` → 1 header row + **200** data rows.
  4. `grep -ciE 'page=|paginat|Next page|Load more'` over the page → **0**.
- **Impact** — an organisation past 200 open requests cannot see or export all of
  them, and has no signal that anything is missing. The same 200 applies to the
  approvals queue, which is built from `ListRequests` too.
- **Evidence** — `TC-B-099`: *“the All tab promises 214 and the list renders 200
  — a tab must never promise a number the list does not show”*.
- **Suggested direction** — page the list, and let the export stream every row
  (or refuse and say so). Confirms and extends a sibling audit's **DS8**: the
  export half is the serious half.

### F-B-06 — a forged foreign key answers 500 and loses the typed form

- **Severity** — medium
- **Confidence** — confirmed (the suite for `vendor_id`; `curl` for all four)
- **Type** — validation
- **Where** — `internal/store/store.go:1712–1720` (`classify` maps only
  `UNIQUE`); the unchecked ids are read at `internal/app/requests.go:303–306`
- **Traces to** — UC-B-41 · TC-B-058, TC-B-095
- **What happens** — `validateRequestInput` checks that `vendor_id`,
  `manager_id`, `project_id` and `head_id` are **positive**, never that they name
  a row. The INSERT then fails on the foreign key, `classify` passes the error
  through unrecognised, and `storeErrorStatus` maps it to **500**. The requester
  gets the error page and everything they typed is gone — the opposite of what
  `renderRejectedRequestForm` exists for.
- **Why it is wrong** — CV5's contract is that a bad input is a 400. A referential
  failure caused by a submitted value is a validation failure, not a server
  failure. It also produces a spurious 5xx in any monitoring.
- **Reproduction** — `POST /requests` with an otherwise valid body and, in turn,
  `vendor_id=999999`, `manager_id=999999`, `project_id=999999`, `head_id=999999`.
  All four answer **500**; the log carries
  `constraint failed: FOREIGN KEY constraint failed (787)` four times.
- **Impact** — reachable by anyone who can craft a form post, and by the product
  itself: F-B-03 and F-B-07 show the same fields are not checked for *state*
  either, so the difference between "accepted wrongly" and "500" is only whether
  the row happens to exist.
- **Evidence** — `TC-B-058`: *“a forged vendor id must be a validation refusal,
  not a server error; got 500”*.
- **Suggested direction** — validate each id against its table inside
  `CreateRequest`/`UpdateRequest`, or teach `classify` to map
  `FOREIGN KEY constraint failed` to `ErrValidation` with a field name. Confirms
  a sibling audit's **DS1**.

### F-B-13 — the edit screen's vendor combobox can never return a result

- **Severity** — medium
- **Confidence** — confirmed (the suite drives it in a browser; also confirmed over HTTP and in the template)
- **Type** — UX
- **Where** — `internal/app/templates.go:2486` (`request_vendor_field`'s
  `combo-input` has no `name`), against `:1902` on the create form which carries
  `name="q"`; `internal/store/vendors.go:337–339` returns nil for an empty needle
- **Traces to** — UC-B-11, UC-B-18 · T6, T7 · TC-B-094
- **What happens** — htmx sends the triggering input's own name/value pair with
  `hx-get`. The edit screen's box has no name, so it sends nothing;
  `vendorSearch` reads `q=""`, `SearchVendors("")` returns nil, and the fragment
  renders only its *“Type a name, GSTIN or city”* placeholder row — for every
  keystroke, forever. `#vendor-options` never contains a `[data-id]` option, so
  there is nothing to click and `#vendor-id` can never be rewritten.
- **Why it is wrong** — the create form's identical control works. On a pending
  `vendor_invoice` the vendor is therefore **unchangeable**: the only way to
  correct a wrong payee is to withdraw and raise again.
- **Reproduction**
  1. As a user holding `vendor:view` (an administrator), raise a `vendor_invoice`.
  2. Open `/requests/{id}/edit` and type any part of the vendor's name into the
     Vendor box.
  3. Only *“Type a name, GSTIN or city · Only active vendors can be picked.”*
     appears. `GET /vendors/search` with no `q` returns exactly that.
- **Impact** — one of the two fields most likely to need correcting cannot be
  corrected. A requester without `vendor:view` is unaffected: they get the plain
  `<select>`, which works (TC-B-061) — so the bug hits the more privileged user.
- **Evidence** — `TC-B-094`: *“htmx sends the triggering input own name/value
  pair, so the box has to be named q · Expected: "q" Received: null”*.
- **Suggested direction** — add `name="q"` to the edit screen's `combo-input`, as
  the create form has. Confirms a sibling audit's **DS2**.

### F-B-05 — a head belonging to another project is accepted

- **Severity** — medium
- **Confidence** — confirmed (the suite, plus `curl` showing the rendered result)
- **Type** — data-integrity
- **Where** — `internal/store/requests.go:161–166` (`needsProjectHead` checks
  only that both ids are positive)
- **Traces to** — UC-B-40 · DV6 · TC-B-056
- **What happens** — the form narrows the head `<select>` to the chosen project's
  own heads (`internal/app/templates.go:1799`), so a mismatched pair can only be
  forged — and nothing on the server rejects it. The request is created and its
  detail then reads **Project: People · Head: Office Rent**.
- **Why it is wrong** — a head belongs to exactly one project
  (`heads.project_id`), and every budget and variance figure is keyed on the head.
  A request that claims one project and is charged to another project's head
  misattributes the spend the moment it is paid, and the variance grid and the
  request disagree about where the money went.
- **Reproduction** — `POST /requests` with `project_id` = Operations and
  `head_id` = a People head. **303**; `GET /requests/{id}` shows the pair. With
  `project_id` = People and `head_id` = Operations / Office Rent the detail reads
  *Project: People · Head: Office Rent*.
- **Impact** — silent misattribution of budget actuals, discoverable only by
  comparing the request against the head's real project.
- **Evidence** — `TC-B-056`: *“a head from a different project: expected 400, got
  303 → /requests/19/submitted”*.
- **Suggested direction** — one extra check in `needsProjectHead`: the head's
  `project_id` must equal the submitted `project_id`. Confirms a sibling audit's
  **DV6**, with the observable consequence attached.

### F-B-15 — the recoverable fields are stored and displayed on a budget expense

- **Severity** — medium
- **Confidence** — confirmed (the suite, plus `curl` showing the rendered rows)
- **Type** — data-integrity
- **Where** — `internal/store/requests.go:414–427` (`CreateRequest` inserts
  `counterparty`, `expected_return_date`, `repayment_notes` and
  `recoverable_category` unconditionally); `internal/app/templates.go:2355–2357`
  prints each when non-empty
- **Traces to** — UC-B-40 · DS3 · TC-B-096
- **What happens** — the form renders the budget and recoverable fieldsets as
  alternatives, so a browser cannot send both. A crafted `POST /requests` for a
  `budget` `vendor_invoice` carrying the recoverable fields is accepted, stores
  them, and the detail then renders **Counterparty**, **Expected return** and
  **Repayment terms** under a pill that says *Budget expense*.
- **Why it is wrong** — the file's own contract is that *"`hidden` is not
  validation: every reveal is re-enforced by store.validateRequestInput"*
  (`internal/app/requests.go:30–33`). The treatment rules are re-enforced; the
  **fields** are not scrubbed. V1 and V2 turn on treatment being the single
  answer to "does this touch budget actuals"; a budget row carrying refund terms
  invites the opposite reading.
- **Reproduction** — `POST /requests` with `type=vendor_invoice`,
  `treatment=budget`, a valid budget body **and** `counterparty=…`,
  `expected_return_date=2027-03-31`, `repayment_notes=…`. **303**; the detail
  lists all three rows beside *Budget expense*.
- **Impact** — a request can be made to read as recoverable to a human while
  behaving as a budget expense to every query, or the reverse. The recoverable
  register keys on `recoverable_category_id`, which stays NULL here, so the rows
  are decorative — which is exactly what makes them misleading.
- **Evidence** — `TC-B-096`: *“a budget expense has no counterparty · Expected:
  false Received: true”*.
- **Suggested direction** — clear the fields the treatment does not own before
  the INSERT, the way `forcesRequesterPayee` already clears `vendor_id`. Confirms
  a sibling audit's **DS3**.

### F-B-03 — an inactive head is accepted when named directly in the POST

- **Severity** — medium
- **Confidence** — confirmed (the suite, plus `curl` on a clean database)
- **Type** — validation
- **Where** — `internal/store/requests.go:161–166`; the form's own list is
  active-only via `ListHeads(ctx, true)` (`internal/app/requests.go:366`)
- **Traces to** — UC-B-02 · T12 · TC-B-046
- **What happens** — a head created inactive, or retired after the fact, is never
  offered on the new-request form (T12, confirmed by TC-B-047) but is accepted
  when its id is posted. The request is created and renders the retired head
  normally.
- **Why it is wrong** — T12 draws the line at "hidden from new, shown on
  historical". Hiding is a presentation decision; here it is the *only*
  enforcement, so the rule holds for a browser and not for a request.
- **Reproduction** — create a head with `active` unchecked, note its id from
  `/heads`, then `POST /requests` naming it as `head_id`. **303**;
  `GET /requests/{id}` shows *Head: Probe Retired*.
- **Impact** — spend can be booked against a head an administrator has
  deliberately closed, which is how a closed cost centre quietly reopens.
- **Evidence** — `TC-B-046`: *“an inactive head named directly in the POST:
  expected 400, got 303”*.
- **Suggested direction** — check the head (and its project) is active in
  `needsProjectHead`, alongside the pairing check of F-B-05.

### F-B-07 — an inactive vendor is accepted when named directly in the POST

- **Severity** — medium
- **Confidence** — confirmed (the suite, plus `curl` on a clean database)
- **Type** — validation
- **Where** — `internal/store/requests.go:167–172` (`needsVendor`);
  `vendorChoices` offers active vendors only (`internal/app/requests.go:290`)
- **Traces to** — UC-B-41 · T6 · TC-B-060
- **What happens** — the same hole as F-B-03, one table over. An inactive vendor
  is absent from both the combobox results and the plain select, and accepted by
  the POST; the payee then renders normally on every downstream screen.
- **Why it is wrong** — `status='inactive'` on a vendor is the product's way of
  saying "do not pay these people any more". The vendor master is described as
  the authority for the payee (*"choose a vendor from the vendor master"*), and
  the authority is not consulted about status.
- **Reproduction** — create a vendor with `status=inactive`, then `POST /requests`
  with `type=vendor_invoice` naming it as `vendor_id`. **303**; the payee shows
  *Probe Dead Vendor*.
- **Impact** — a retired or blocked vendor can be paid through a crafted request,
  and the request looks entirely ordinary to its approver and to Accounts.
- **Evidence** — `TC-B-060`: *“an inactive vendor named directly in the POST:
  expected 400, got 303”*.
- **Suggested direction** — have `needsVendor` resolve the vendor and require
  `status='active'`.

### F-B-02 — every refusal of the `recoverable` type reports the wrong reason and throws the form away

- **Severity** — medium
- **Confidence** — confirmed (reproduced two ways: the suite, and the label lookup)
- **Type** — UX
- **Where** — `internal/app/requests.go:179–184` (`renderRejectedRequestForm`
  looks the type up in `requestTypeLabels`); `requestTypeOptions` at `:42–55` has
  four entries, `store.requestTypes` at `internal/store/requests.go:78` has five
- **Traces to** — UC-B-10 · DV1 · TC-B-006, TC-B-091, TC-B-051, TC-B-037
- **What happens** — the store accepts a fifth type, `recoverable`, that the
  chooser and the form do not know. Creation works. But when validation refuses
  such a submit, `renderRejectedRequestForm` cannot find a label, so instead of
  re-rendering the form with the real message it answers **400 “That is not a
  kind of request this system raises.”** — a sentence about the wrong problem —
  and the entire form is lost. The type also renders as the raw enum
  `recoverable` in `.rh-meta`, and its category as the raw code when the category
  is not one of the six hardcoded labels.
- **Why it is wrong** — the message contradicts what just happened (the system
  *did* raise that kind of request, seconds earlier, in TC-B-006), and it is
  emitted for every rule in the recoverable set: a missing category, a missing
  return date, missing terms, a wrong treatment. The rules themselves fire
  correctly (400 in all cases) — only the reporting is broken.
- **Reproduction**
  1. `POST /requests` with `type=recoverable`, `treatment=recoverable`,
     `recoverable_category=emd`, a project, a return date and terms → **303**, created.
  2. Repeat with `repayment_notes` omitted → **400 “That is not a kind of request
     this system raises.”** rather than *“repayment or refund terms are required
     for recoverables”*, and with no form to correct.
- **Impact** — the `recoverable` type has no UI path, so in practice this bites
  integrations and anybody following the overview spec, which lists five types
  (`docs/superpowers/specs/2026-07-25-payment-requests-overview.md:251`). It is a
  latent trap the moment a fifth card is added.
- **Evidence** — `TC-B-091`: *“the requester must be told which rule refused them
  · Expected substring: "repayment or refund terms are required" · Received:
  "That is not a kind of request this system raises."”*
- **Suggested direction** — decide whether `recoverable` is a type users may
  raise. If it is, give it a card, a label and a category label; if it is not,
  drop it from `store.requestTypes` so the validator refuses it first and the
  message is honest either way.

### F-B-17 — an admin-added recoverable category is enforced but can never be selected

- **Severity** — medium
- **Confidence** — confirmed (the suite, plus `curl`: added a category and used it)
- **Type** — spec-divergence
- **Where** — `internal/app/templates.go:1738–1743` hardcodes the six seeded
  options; `internal/app/requests.go:1026–1033` hardcodes their labels;
  `normalizeRecoverableCategory` (`:132–140`) rewrites anything else
- **Traces to** — UC-B-09, UC-B-10 · V4, DV3 · TC-B-097, TC-B-098
- **What happens** — this refines the claim rather than confirming it whole.
  - **Enforcement works.** A category added on `/configuration` gets a code
    (`categoryCode`, `internal/store/recoverables.go:99`), and a submit carrying
    that code is stored **as itself** and has the admin's chosen rule enforced —
    omitting the counterparty a new `requires_counterparty` category demands
    answers **400**. `requestCreate` reads the raw form value, so there is **no
    silent refiling under EMD on submit**. That half of the sibling claim is
    **refuted**.
  - **Selection does not.** The `<select>` never offers the new category, so no
    requester can pick it through the UI.
  - **The htmx fragment substitutes silently.** Asking
    `GET /requests/new/fields?type=employee_advance&treatment=recoverable&recoverable_category=<new code>`
    returns the select with `employee_advance` selected and **no counterparty
    field** — `normalizeRecoverableCategory` replaced the category and the reveal
    that its rule depends on. So if the option ever existed, one swap would
    silently lose it.
  - **Display does not.** The detail's pill renders the raw code
    (*Recoverable · retention_deposit*), because the label map is hardcoded.
- **Why it is wrong** — V4 promises *"an admin can add a category and have its
  rules enforced without a code change"*, and the code comment at
  `internal/store/requests.go:109–115` says the table is the authority. Two of the
  four halves need a code change.
- **Reproduction**
  1. `/configuration` → *Recoverable categories* → **New category** =
     “Retention deposit”, *Must also capture* = Counterparty company → **Add category**.
  2. Open `/requests/new?type=employee_advance`: the Category select still offers
     exactly the six seeded options.
  3. `POST /requests` with `recoverable_category=retention_deposit` and no
     counterparty → **400** (the rule is enforced). With a counterparty → 303, and
     the detail reads *Recoverable · retention_deposit*.
  4. `GET /requests/new/fields?...&recoverable_category=retention_deposit` →
     `employee_advance` selected, no `name="counterparty"` in the fragment.
- **Impact** — an administrator can create a category that nobody can use and
  that renders as a machine code where it is used. Not a data-integrity defect on
  the submit path; a broken feature.
- **Evidence** — `TC-B-098`: *“the form must offer every active category ·
  Expected: 1 Received: 0”*. `TC-B-097` passes, which is what refutes the
  misfiling half.
- **Suggested direction** — render the options and labels from
  `ListRecoverableCategories(ctx, true)`, and make
  `normalizeRecoverableCategory` accept any active code.

### F-B-04 — invoice and advance columns are stored and displayed on a reimbursement

- **Severity** — low
- **Confidence** — confirmed (the suite)
- **Type** — data-integrity
- **Where** — `internal/store/requests.go:414–427`;
  `internal/app/templates.go:2351–2354`
- **Traces to** — UC-B-40 · DS3 · TC-B-092
- **What happens** — the same mechanism as F-B-15 in the other direction. A
  crafted reimbursement carrying `invoice_no`, `invoice_date` and
  `advance_reason` stores all three, and the detail prints **Invoice number**,
  **Invoice date** and **Advance reason** on a request type that has none of
  them. The vendor *is* correctly dropped and the payee correctly forced
  (TC-B-054), which is what makes the omission stand out.
- **Why it is wrong** — the type is supposed to decide which fields exist (T2).
  An invoice number on a reimbursement will be read as a real invoice by whoever
  approves it.
- **Reproduction** — `POST /requests` with `type=reimbursement`, a valid body and
  `invoice_no=FORGED-1`, `invoice_date=2026-07-01`,
  `advance_reason=Not a field this type has.` → 303; both appear on the detail.
- **Impact** — cosmetic in isolation; combined with the duplicate check, which
  matches on `invoice_no` (`internal/store/requests.go:1331`), a forged invoice
  number on a reimbursement can also collide with a genuine vendor invoice's
  duplicate warning.
- **Evidence** — `TC-B-092`: *“a reimbursement has no invoice number · Expected:
  false Received: true”*.
- **Suggested direction** — the same scrub as F-B-15, keyed on type rather than
  treatment.

### F-B-18 — the list is newest-first while the screen says it is sorted by who is holding requests up

- **Severity** — low
- **Confidence** — confirmed (`curl`: two requests raised a second apart)
- **Type** — UX
- **Where** — `internal/store/requests.go:1276–1278`
  (`ORDER BY r.urgent DESC, r.created_at DESC, r.id DESC`);
  `internal/app/templates.go:2119` (*“sorted by who is holding them up”*)
- **Traces to** — UC-B-23 · DV2 · verified during this pass, not asserted by a case
- **What happens** — the comment above the query says *"the list is sorted by who
  has been kept waiting longest"* and the screen says *"sorted by who is holding
  them up"*. The query sorts urgent first, then **newest** first.
- **Why it is wrong** — "kept waiting longest" is `created_at ASC`. As written,
  the request that has been waiting longest is last, or on page two once F-B-16
  truncates.
- **Reproduction** — raise *SORT-OLDEST*, wait a second, raise *SORT-NEWEST*, open
  `/requests?bucket=open`: *SORT-NEWEST* is first.
- **Impact** — the queue's ordering is the opposite of what its own sub-line
  promises, so the oldest neglected request is the hardest one to find.
- **Evidence** — the two cards' rendered order, above; no assertion in this
  suite, because the list's ordering belongs to UC-B-23's owner.
- **Suggested direction** — pick one and make the other match. Confirms a sibling
  audit's **DV2**.

### F-B-14 — no date is ever compared with today

- **Severity** — informational
- **Confidence** — confirmed (the suite)
- **Type** — validation
- **Where** — `internal/store/requests.go:151–160` (format only)
- **Traces to** — UC-B-05, UC-B-10 · VA8 · TC-B-042, TC-B-043
- **What happens** — `needed_by=2019-01-01` and, on a recoverable,
  `expected_return_date=2019-06-30` are both accepted and rendered (*1 January
  2019*, *30 June 2019*). Only the `YYYY-MM-DD` format is checked.
- **Why it is wrong** — arguably it is not: neither field has a stated rule, and
  a back-dated `needed_by` is a legitimate way to say "this is already late".
  Recorded because both cases were written expecting a refusal, and because an
  `expected_return_date` in the past will age straight into the recoverable
  register as overdue on the day it is created.
- **Reproduction** — as above.
- **Impact** — the recoverables ageing view inherits dates that were never
  plausible.
- **Evidence** — `TC-B-042`, `TC-B-043`, both PASS against the code's actual
  contract.
- **Suggested direction** — if a rule is wanted, it belongs in
  `validateRequestInput` beside the format check; otherwise state in the spec
  that past dates are allowed, so the next auditor does not write this case again.

### F-B-08 — validation messages carry a sentinel prefix, and one carries the wrong article

- **Severity** — informational
- **Confidence** — confirmed (the suite)
- **Type** — UX
- **Where** — `internal/app/app.go:1636–1637` (`friendly` returns `err.Error()`
  for `ErrValidation`); `internal/store/models.go:12`
  (`ErrValidation = errors.New("validation failed")`);
  `internal/store/requests.go:659` and `:470`, `:614`, `:696`, `:732`
- **Traces to** — UC-B DS4 · TC-B-071
- **What happens** — every validation banner reads
  *“validation failed: the invoice number is required”*. And because the status
  is interpolated with a fixed article, an approved request reports
  *“a approved request cannot be withdrawn”*.
- **Why it is wrong** — `validation failed:` is an internal sentinel, not a
  sentence for a person; and the article is simply wrong in front of every
  vowel-initial status (`approved`, `on_hold`).
- **Reproduction** — `POST /requests/{id}/withdraw` on an approved request; read
  the error page.
- **Impact** — cosmetic, on the screen a person sees at their most frustrated.
- **Evidence** — `TC-B-071`: *Received string: "validation failed: a approved
  request cannot be withdrawn"*.
- **Suggested direction** — have `friendly` strip the sentinel, and phrase the
  transition messages without a leading article (*“an approved request cannot
  be…”* → *“This request is approved and cannot be withdrawn.”*).

### F-B-12 — htmx silently drops a request from an element it has just replaced

- **Severity** — informational
- **Confidence** — confirmed (5 isolated runs before and after the fix)
- **Type** — test-fidelity
- **Where** — `internal/app/templates.go:1736–1737` and `:1788–1789` — both
  controls that trigger the swap live inside `#form-fields`, the fragment the
  swap replaces
- **Traces to** — UC-B-03 · TC-B-008, TC-B-009
- **What happens** — `#rcategory` and `#project` each carry
  `hx-get="/requests/new/fields" hx-target="#form-fields"`, and each is itself
  inside `#form-fields`. Changing one twice in quick succession loses the second
  change entirely: htmx will not issue a request for an element that is no longer
  in the document, and it reports nothing when it declines. Two of this suite's
  cases were flaky (1 in 3) until they waited for the swap to have been
  **applied** — the server echoes the choice as a `selected` **attribute**, which
  `selectOption` never sets — and for
  `.htmx-request, .htmx-settling, .htmx-swapping` to be empty. 5/5 stable after.
- **Why it is wrong** — it is not a product defect: a human cannot change a
  `<select>` twice inside one settle window, and every manual dispatch on the
  swapped-in node fires correctly. It is recorded because it cost a debugging
  pass and will cost the next author one: the failure mode is a *missing* network
  request with no console error, which reads exactly like a server-side bug.
- **Reproduction** — pick `emd` then immediately `icd` on `#rcategory` without
  waiting for the first fragment to land: the second change produces no request
  and the fieldset stays on `emd`.
- **Impact** — test flakiness only.
- **Evidence** — the diagnostic run: *“DBG lost swap for icd … element state
  {listener:[…], value:"icd"} … DBG manual dispatch recovered it”*.
- **Suggested direction** — nothing in the product. For test authors, the
  `swapSettled` helper in `audit-b-request-lifecycle.spec.ts` is the pattern; a
  comment there points here.

---

## Sibling claims, confirmed or refuted

| Claim | Verdict | Where |
|---|---|---|
| The edit screen's vendor combobox cannot search — its `combo-input` has no `name`, so htmx sends no `q` | **CONFIRMED** three ways: the template, `GET /vendors/search` with no `q` returning only the placeholder row, and driving the field in a browser | F-B-13 · TC-B-094 |
| A forged foreign key answers 500, not 400 — `vendor_id`, `manager_id`, `project_id`, `head_id` | **CONFIRMED for all four**; each logs `FOREIGN KEY constraint failed (787)` and renders the 500 page | F-B-06 · TC-B-058, TC-B-095 |
| Recoverable-only columns are written regardless of treatment, so a crafted budget POST renders Counterparty / Expected return / Repayment terms | **CONFIRMED**; all three rows render under a *Budget expense* pill | F-B-15 · TC-B-096 |
| An admin-added recoverable category can never be selected, and a submit carrying it is silently filed under EMD | **SPLIT — the UI gap is confirmed, the data-integrity half is REFUTED.** The category is never offered and renders as its raw code, but a submit carrying it is stored as itself and its rule is enforced: `normalizeRecoverableCategory` is reached only by the htmx fragment, never by `requestCreate`. The fragment *does* substitute silently, which is the real hazard | F-B-17 · TC-B-097 (passes), TC-B-098 |
| `?type=recoverable` silently re-renders the chooser because `requestTypeOptions` has four types while the store accepts five | **CONFIRMED**; the chooser renders 4 cards and `?type=recoverable` falls back to its `<h1>`. The consequence worth fixing is the broken refusal path, not the fallback | F-B-02 · TC-B-006, TC-B-091 |
| The list and its CSV export have a silent 200-row cap, and the list's sort is `created_at DESC` while the screen says "kept waiting longest" | **CONFIRMED, both halves.** With 214 in scope the tab reads 214, the list renders 200, the CSV carries 200 data rows and there is no pagination control; and *SORT-NEWEST* renders above *SORT-OLDEST* | F-B-16, F-B-18 · TC-B-099 |

## Divergences recorded, not defects

| ID | Claim | Spec / doc | Code |
|---|---|---|---|
| DVB1 | Coverage row **L1 “Draft (private)”** | `docs/superpowers/specs/2026-07-25-payment-requests-coverage.md` still names a draft state | Decision **D1** removed it; `payment_requests.status` carries `CHECK (status <> 'draft')` (`internal/store/migrations.go:155`) and no route produces one (TC-B-066). The coverage row's own restatement — *“L1 changes meaning … and becomes a proof-of-absence test”* (`2026-07-25-design-system-adoption-spec.md:47`) — has not been carried back into the matrix. |
| DVB2 | The fixtures' “KNOWN GAP — payee does not reach the payment” | `tests/e2e/fixtures.ts:178–188` | **Stale.** `internal/app/linking.go:260` writes `VendorPayee: req.Vendor`. Independently confirms a sibling audit's DV8/SV31. Do not propagate the comment. |
| DVB3 | A vendor id names a vendor the requester chose | implied by the combobox design | The server guarantees only that the vendor **exists** — there is no per-requester vendor visibility and no bound choice token, so any active vendor id is accepted (TC-B-059). Asserted as the contract, not as a defect. |

## Not run, and why

| # | Case | Reason |
|---|---|---|
| NR-1 | A *suppressed* reminder after an edit resets `reminder_last_sent` | The column has no UI surface and the scheduler is started by `cmd/server` on an hourly tick with no route to trigger it. TC-B-069 proves the reset through the `update` audit row's `after` snapshot (`"ReminderLastSent": null`) and proves the reroute and the re-notification directly — the suppression itself needs the Go suite or a clock seam that does not exist over HTTP. Partial, and labelled as such. |
| NR-2 | The urgency rules (VG1 `urgency_mode=reason`, VG2 `disabled`) | Configuration-driven and owned by whoever audits `/configuration`. This suite already mutates one setting (TC-B-083) and restoring it is the risky part on a shared database; a deliberate hand-off rather than an oversight. |
| NR-3 | The 20 MiB upload cap, and the 10 MB the screen advertises | Pushing 20 MiB through the harness on every run costs more than the assertion is worth. The mismatch is already recorded as a sibling audit's DV5. |
| NR-4 | `POST /requests` with a body over the 21 MiB cap | The 413 path is generic `withCSRF` middleware, not request-specific. |
| NR-5 | A mobile-chrome pass | Nothing in this area has a 390 px question that `requests.spec.ts` and `request-workflow.spec.ts` do not already sweep on both projects with the full quality bar. |
