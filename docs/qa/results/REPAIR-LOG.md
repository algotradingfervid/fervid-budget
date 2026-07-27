# Repair log — what happened to each audit finding

`AUDIT-REPORT.md` is a **snapshot of commit `30edd6a`**, taken under the
instruction *report defects, do not change application code*. That instruction was
then reversed — *fix all the issues* — so the report's prose stays as the historical
record of what was found, and this file records what was **done about it**.

Read this file for status. Read the report for evidence.

**Wave structure and file ownership:** `FIX-PLAN.md`.
**Verification gate for every wave:** `go build ./... && go vet ./... && go test -count=1 ./...`,
`npm run typecheck`, the shipped suite at **163 passed / 49 skipped / 0 failed**, and
the owning area's audit spec on its own port.

---

## Decisions taken during the repair

Recorded because each closes a question the audit left open, and a later reader
will otherwise re-open it.

| # | Question | Decision | Why |
|---|---|---|---|
| 1 | How should a recoverable payment get a head? | **The head becomes optional.** Migration **v8** rebuilds `payments` with `head_id INTEGER REFERENCES heads(id)` nullable; `validatePayment` requires a head only for a budget-treatment payment. | *The user's call.* The grid and the monthly report already exclude recoverables **by treatment**, so a recoverable's head was never meaningful to any reader. The alternative — collect a head on the recoverable fieldset — would have made every accountant choose a value nothing consumes. |
| 2 | `calendarDaysBetween` — use it or delete it? (F-F-08) | **Deleted.** The thresholds are elapsed days, and the copy must say "days", not "calendar days". | A calendar boundary needs a timezone this app does not have, and under calendar semantics `reminder_repeat_days` degrades into nightly spam. |
| 3 | Can a request stay `on_hold` while `cancellation_requested`? (F-C-07) | **No.** The ask suspends the flag but keeps `hold_reason`; a decline restores the flag from it. | Leaving `on_hold=1` would break the *`on_hold` implies `approved`* invariant that `PROGRESS.md` records as a build decision and `linking_test.go` pins. This preserves L7's "only Accounts lifts" without breaking the invariant. |
| 4 | Is F-A-03 (no ownership check on attachment **upload**) still a hole? | **Unreachable, not fixed-by-removal — so it was fixed anyway.** | F-D-08's fix means every payment this product can create is linked, and `paymentCreate` refuses `request_id=0`, so the route cannot be reached. The check was added regardless, because the hole returns the day a free-standing payment becomes creatable. `TC-A-123` says so in a comment. |
| 5 | `payments.head_id` rebuild — why not the documented 12-step recipe? | The migration stashes `payment_attachments`, empties it, does create/copy/drop/rename, then restores the rows with their ids. | `PRAGMA foreign_keys` rides on the DSN and cannot change inside a transaction. `PRAGMA defer_foreign_keys` is **not** sufficient alone: SQLite tracks deferred violations as a counter only DML adjusts, so the violations booked when `DROP TABLE` implicitly deletes the parent rows are never cancelled by the later DDL rename and `COMMIT` still fails 787. Renaming the old table aside fails differently — this driver rewrites the child's `REFERENCES` clause even under `legacy_alter_table`. |

---

## Wave 1 — store foundations and one CSS fix · `633997b`

| Finding | Outcome |
|---|---|
| F-D-11 · F-E-01 · F-G-001 **critical** | **Fixed.** Migration v8, per decision 1. Four payment readers had inner joins on `heads` that would have silently dropped a NULL-head row — a 404 on the very payment the 303 redirects to — and are now `LEFT JOIN` + `COALESCE`. |
| F-D-01 **high** | **Fixed.** `RecordPaymentForRequest` derives `head_id`, `vendor_payee` and `invoice_no` from the request row it already has open, so a forged form value cannot charge an approved payment to a head nobody approved. |
| F-B-01 **high** | **Fixed.** `money.ParsePaise` refuses an int64 overflow instead of saturating; `1e300` no longer becomes a request for ₹92,23,37,20,36,85,47,758.07. |
| F-B-06 · F-G-027 medium | **Fixed.** `classify` maps a FOREIGN KEY violation to `ErrValidation` — a forged id is a 400 with the form intact, not a 500. |
| F-A-04 · F-G-003 medium | **Fixed** (filter here, wired in Wave 3). `PaymentListOptions` gains `Scope`/`ViewerID` and filters on `entered_by`. |
| F-D-02 medium | **Fixed, store half.** `ReserveRequest` returns three distinguishable sentinels. Handler half in Wave 4. |
| F-D-08 low | **Fixed.** `AddAttachment` refuses a payment whose `request_id` is set, matching edit and void. |
| F-A-01 · F-A-05 low-level | **Fixed.** `RequestAttachmentByID` and `AttachmentWithPayment` added, so Wave 3 could check ownership from the right table. |
| F-D-06 low | **Implemented but inert.** `validatePayment` refuses a `paid_on` after `in.Now` — and no caller sets `Now` yet, so the guard does not fire. Deliberate: nine fixtures in `regression-issues.spec.ts` book payments in 2027–2029, and moving them is Wave 5's job. Until then the code is present and tested at store level but not enforced through the UI. |
| F-H-01 **high** | **Fixed.** `.segmented` carries `max-width: 100%; overflow-x: auto` with `flex-shrink: 0` items, so the roles strip scrolls in its own box the way `.table-wrap` already does. Both TC-H-001 (1440px) and TC-H-003 (390px) were failing; both pass. |

**Spec pass 1 · `350c108`** — nine `test.fail()` annotations retired (TC-D-058, TC-D-095,
TC-D-096, TC-E-030, TC-B-018, TC-B-058, TC-B-095, TC-G-084, TC-H-001/003), and
TC-E-031 and TC-E-042 came back from `fixme`: both needed a *paid* recoverable,
which v8 made possible for the first time.

---

## Wave 2 — request/recoverable logic, and the notification vocabulary · `25411b8`

| Finding | Outcome |
|---|---|
| F-C-01 **high** | **Fixed.** An approver could approve **more** than was requested and the larger figure became the payment ceiling — ₹25,000 approved on a ₹18,400 request, paid in full, closed as Completed. `ApproveRequest` caps at the requested amount. |
| F-C-03 **high** | **Fixed.** Approving was legal from `cancellation_requested`. Now requires `pending`; `legalTransitions` keeps the edge for `DecideCancellation` alone, commented as such. |
| F-C-07 **high** | **Fixed** per decision 3. |
| F-B-10 · F-C-05 **high** | **Fixed.** `beginWriteTx` takes the write lock with the transaction's **first** statement — the per-call-site `BEGIN IMMEDIATE` that modernc's DSN-only `_txlock` cannot express — and every UPDATE is conditional on the state it expects with a `RowsAffected` check returning `ErrRequestRaced` (a 400). The plan named four writers; TC-C-134 races accept-vs-decline too, so the whole family moved. Both new tests were verified to **fail against the unfixed code under `-race`**, not merely to pass against the fixed. |
| F-B-16 **high** | **Fixed, store half.** `ListRequestsPage` returns `Total`/`Offset`/`Limit`/`Truncated`; `RequestsUnlimited` for the CSV. Rendering is Wave 4. |
| F-A-08 medium | **Fixed.** A request could be routed to somebody holding no `approval:approve`, after which nobody could decide it. Refused on create, update and reassign. |
| F-B-05 · F-B-03 · F-B-07 medium | **Fixed.** A head from another project, an inactive head and an inactive vendor were all accepted when named directly in the POST. |
| F-B-15 · F-B-04 low | **Fixed.** Columns a shape does not own are cleared, so a crafted POST can no longer make a budget expense render Counterparty and Repayment terms. |
| F-E-05 medium | **Fixed.** `RecoverableMetrics` applies the `paid <> ''` guard its own `recoverableAgeing` already had, so unpaid money is not counted overdue. |
| F-E-02 · F-B-17 **high** | **Fixed, store half.** `ListRecoverableCategories(ctx, activeOnly)`. Rendering is Wave 4. |
| F-B-18 low | **Fixed.** Ordering is `urgent DESC, created_at, id` — longest-waiting first, as the screen says. |
| F-D-04 medium | **Fixed.** The linkable-request search normalises the needle like `ParsePaise` and also matches on paise, so `7,431.00` finds ₹7,431.00. LIKE metacharacters are escaped. |
| F-F-06 **high** | **Fixed, vocabulary half.** Nine new `notify.Event*` constants; migration **v9** adds their `notification_settings` rows with `ON CONFLICT(event) DO NOTHING`, so an administrator's edits to v7's twelve survive. That is why it is a migration and never a re-seed. Handler wiring is Wave 4. |
| F-F-08 informational | **Resolved by deletion** per decision 2. |
| F-F-02 medium | **Fixed.** `MarkReminderSent` writes an audit row, so "a reminder went out" has a witness. |

---

## Wave 3 — the app layer · `1fac147`

| Finding | Outcome |
|---|---|
| F-A-01 · F-B-11 **critical** | **Fixed.** `GET /attachments/{id}` performed no ownership check at all, and `attachment:view` is a seeded **Requester** grant, so any signed-in user read every bank advice in the system by walking ids from 1. It now resolves the attachment to its payment and applies the same request-scope check `paymentDetail` runs — whose own comment said *"or `payment:view` becomes a way around Q5/R6"*; this route was the hole that comment was written about. Refusal is **404** through the same path as a missing id, so the route is not an enumeration oracle. |
| F-A-05 · F-B-09 **critical** | **Fixed.** One download route served two tables: `AttachmentByID` reads `payment_attachments`, three templates rendered **request** attachments through it, and both id sequences start at 1 — so Download on your own invoice served a stranger's bank advice. Request documents now have `GET /requests/{id}/attachments/{attachmentID}`, which checks the attachment really belongs to the request in the path. |
| F-A-03 **high** | **Fixed** per decision 4. |
| F-A-02 · F-G-032 **high** | **Fixed.** `/grid` was `RequireLogin` only, so a role-less user read every budget and actual plus a Recent Payments table with amounts, payees and live payment links while `/payments` answered them 403. Gated on `grid:view`, with Recent Payments gated separately on `payment:view` **in the handler** rather than trusted to a template. |
| F-A-04 · F-G-003 | **Fixed, wiring half.** Wave 1's filter is now actually passed; without this the roles-screen control still did nothing. |
| F-D-10 **high** | **Fixed.** The settlement write was gated more weakly than its pure preview. `POST /payments` now needs `payment:settle`, and `payment:mark_partial` when `settlement=partial`. |
| F-G-028 **high** | **Fixed.** An unbudgeted month could not be saved at all. Empty and ₹0.00 are zero rather than errors, and one unreadable field no longer discards the batch; the banner names what was left. |
| F-G-033 **critical** | **Fixed, server half.** The heads screen now receives `PageData.AllProjects`. Template half is Wave 4. |
| F-G-034 **high** | **Fixed.** `POST /users` runs its three writes in one `beginWriteTx` envelope (`SaveUser`). |
| F-G-022 · F-G-023 | **Fixed.** Deleting a role pre-checks its holders, and a refused system-role delete says why instead of "you do not have permission". |
| F-G-017 **high** | **Fixed.** `/audit` no longer discloses the full request text to any `audit:view` holder; `auditWithinRequestScope` applies. |
| F-A-06 · F-C-02 medium | **Fixed** — coverage requirement **A7** now has a door. `POST /requests/{id}/reassign-approver` (`manager_id`, `reason`), gated on `approval:reassign`, over the existing tested `store.ReassignRequest`. It is also the recovery path F-A-08 and F-G-025 need. Control rendering is Wave 4. |
| F-A-11 · F-G-036 medium | **Fixed.** `validatePassword` delegates to `auth.ValidatePassword` plus the 12-character minimum, so every rejection is a 400 naming the rule. |
| F-A-07 low | **Fixed.** `POST /users` requires `user:create` when creating and `user:edit` when editing, so the button and the route agree. |
| F-A-09 · F-D-02 low | **Fixed, `friendly` half.** An `ErrForbidden` state conflict carries its own sentence. Naming the three reservation sentinels on the conflict screen is Wave 4. |
| F-G-026 medium | **Fixed.** A locked-month refusal answers 409 everywhere, not 400 on two screens. |
| F-G-004 · F-C-06 medium | **Fixed.** `auditEntities`/`auditActions` cover the request workflow; `actionText`/`actionClass` render the new verbs. Filter `<select>`s are Wave 4. |
| F-A-10 informational | **Fixed.** `POST /login` gets the same body cap every other POST has. |
| F-G-002 medium | **Fixed at the app.go sites.** The main site is `loadViewableRequest` in `requests.go` — Wave 4's file — which must answer 404, not 403. |

---

## Expectations that were wrong, not the product

Kept visible because the honest failure mode of a repair pass is editing a test
until it passes.

| Test | What happened |
|---|---|
| `TC-G-086` | Demanded a forged `head_id` be **refused**. F-D-01 makes the server ignore it and derive the head from the request, which is stronger. Rewritten to assert the payment is booked against the approved head whatever the form sent. |
| `TC-G-038` | Had pinned *"sits in Not yet paid for ever"* as truth, with no `test.fail()`. Now asserts a recoverable settles and closes. |
| `ISS-001` | Asserted the batch atomicity F-G-028 deliberately reversed. The first rewrite still failed because it expected `123.45` while the reloaded input renders `₹123.45` — **my expectation was wrong, not the product.** Now `toHaveValue(/123\.45/)`, plus a restore step so the month stops leaking into ISS-029. |
| `TC-B-085` | Its own `test.fail()` was hiding a broken fixture, so F-B-11's pin could never have detected a fix. The fixture now attaches during the settlement's own multipart POST. |
| `TC-B-063` | Was `fixme` because the SQLITE_BUSY race was non-deterministic — `test.fail()` reported "passed unexpectedly" on the run where the loser happened to win. Wave 2's `beginWriteTx` made it deterministic; now live. |
| `TC-A-123` | Rewritten from *"upload is unchecked"* to a proof of the refusal, per decision 4. |

**Areas A and G are red on purpose right now.** Twenty-nine assertions there
recorded defective behaviour as truth *without* `test.fail()`, so they fail
because the product improved. Each is being rewritten to assert the fixed
behaviour, never deleted.

---

## Wave 4 — templates and the remaining handlers · `709dfa6` · `629925e`

| Finding | Outcome |
|---|---|
| F-G-016 · F-E-03 **critical** | **Fixed — the last critical.** The register was a second view over `payment_requests` that never asked who was reading it, so `recoverable_report:view` alone returned every category, counterparty, amount, requester and repayment note in the company. The rows, the CSV and the detail screen were scoped in `709dfa6`; the **summary aggregates** were still company-wide and were closed in `629925e`, which matters because the counterparty rollup names the other party outright. The predicate lives in SQL — `recoverableScope`, applied by `RecoverableReport` and taken as a parameter by `RecoverableMetrics` and `RecoverableRollups` — because aggregates cannot be filtered row-by-row after the fact. An unrecognised scope returns `AND 0`, so it fails closed. |
| F-A-05 · F-G-033 **critical** (template halves) | **Fixed.** The three request-document links point at `GET /requests/{id}/attachments/{attachmentID}`; the heads row selects range over `AllProjects`, marking retired ones, so a row's own Save cannot move a head. |
| F-G-035 · F-C-04 **high** | **Fixed, and then finished.** Wave 4 hoisted every submit precondition ahead of the write, which closed the observable defect but left a window between three transactions. `629925e` replaced them with `store.EditRequest`: the edit, the document and the transition commit together or not at all. The attachment had to move *inside* that transaction, because the attachment policy counts rows and the document must exist before the submit is validated. |
| F-F-01 **critical** | **Fixed.** The per-event sheets and the SMTP form carry `hidden`, so they stop rendering full-viewport at once with only the last one clickable. |
| F-F-06 · F-D-12 **high** | **Fixed, wiring half.** Nine events fire from withdraw, re-raise, unhold, release, reassign, partial accept, partial concern and both cancellation decisions. |
| F-F-07 · F-F-05 · F-F-03 | **Fixed.** "Save corrections" no longer claims a re-send; the notification centre has a desktop entry; the reminder copy reads the configured threshold and says "days". |
| F-G-018 · F-G-006 · F-G-007 · F-G-008 · F-G-012 | **Fixed.** The dashboard uses the caller's scope, and each tile counts the bucket its own link points at. |
| F-G-002 medium | **Fixed.** `loadViewableRequest` answers 404, so the route is not an existence oracle. |
| F-G-029 · F-G-019 · F-G-020 · F-G-021 | **Fixed**, with one edge left: the grid filter form posts to `/grid`, lock/unlock lands somewhere that confirms it, and a request cannot be approved into a locked month. The payment form warns on the month it **opens** on; a month typed in afterwards is still only caught at submit, because which month is being written is not known until then. |
| F-D-02 · F-D-03 · F-D-05 · F-D-07 | **Fixed.** The conflict screen decides its wording from the row rather than the sentinel — the store reports "already reserved" for a *completed* request whose `processing_by` was never cleared, so the screen was claiming somebody holds a request nobody holds. |
| F-E-02 · F-B-17 · F-E-04 · F-E-07 · F-E-08 · F-B-02 | **Fixed.** The category `<select>` is the table; the CSV and its screen carry the same columns. |
| F-B-16 · F-B-13 · F-B-08 · F-G-014 · F-G-005 · F-G-013 · F-G-030 · F-G-031 | **Fixed.** The 200-row cap is visible with a pager; `?status=` is honoured; CSV amounts are numbers, not `₹`-formatted strings. |

---

## Wave 5 — the tail, the documents, and the spec repair · `4a01506` · `96ba6e1`

| Finding | Outcome |
|---|---|
| F-G-009 · F-G-010 low | **Fixed.** Migration **v10** gives `payments` a `vendor_id`, back-filled from the request each payment settles. "Paid this year" matched payee *text* against the vendor name — v2's own comment promised "Phase 2 adds vendor_id" and Phase 2 never did — and "Open requests" was declared, scanned nowhere, and rendered as a hard `0` that looked like a count. The exact payee match survives as a fallback for historical rows that settle no request. |
| F-D-06 low | **Fixed — the guard now fires.** It had shipped inert: `validatePayment` refused a `paid_on` after `in.Now` and no production caller ever set `Now`, so the rule was enforced against nobody. It defaults to `time.Now().UTC()` now, because forgetting to inject a clock must mean *enforce*, never *skip*. The nine shipped fixtures booking payments in 2027–2029 moved to 2025/2024; those months were only ever private namespaces stopping tests colliding. |
| F-E-06 low | **Fixed.** `recoverable_category:delete` has a door: `POST /configuration/recoverable-categories/{id}/delete`, over `DeleteRecoverableCategory`, which pre-checks usage inside a `beginWriteTx` and refuses by naming the count. |
| F-G-037 informational | **Fixed.** `ListNotificationsPage` returns `Total`/`Truncated` and the centre reads "100 of 105 shown" with a pager. Not theoretical: an area-D test passed alone and failed in a full run purely because the admin had crossed the cap. |
| F-C-08 low | **Fixed.** The status enum listed seven of eleven, on the strength of a comment saying Phase 3 would append the rest; Phase 3 shipped them and never came back for the comment. |
| F-D-14 low | **Fixed at the handler, reported at the store.** `/requests/new?type=` naming no card answers **400** with the chooser and the reason, instead of bouncing the caller silently. A fifth card was rejected deliberately: "recoverable" is a *treatment* on this form, so the card would render a form whose treatment radio had one legal position. |
| — | `UpdateUser`, `SetUserRoles`, `SetUserDefaultApprover` keep their exports and gain comments naming who calls them and what they do not do. `go mod tidy` dropped casbin, doublestar and govaluate — declared, and imported by nothing. |

---

## Still open

Nothing rated critical or high. What remains, each verified against source rather than inferred from a green test:

| Finding | Why it is still open |
|---|---|
| F-G-011 | `/requests/export.csv` has no approved-amount column. |
| F-G-020, remaining edge | The payment form cannot warn about a locked month the accountant types in *after* it renders. Closing it needs the client to re-ask on date change. |
| F-G-024 | A head with an approved request against it can be deactivated with no check and no warning. The resulting refusal is coherent — a 400 with a sentence, never a 5xx. |
| F-G-025, open half | Deactivating a user is not pre-checked against the approvals waiting on them. The *recovery* is now possible (A7's route exists); the *warning* is not. |
| Coverage matrix **V6**, **A8** re-notify half | Marked `UNVERIFIED` rather than ticked: the tests they cited no longer exist and no replacement was found. |

### Found during the Wave 5 documentation pass, not by the audit

Both verified from source while correcting the stale documents. Neither is an
audit finding — the audit could not have raised the first, because Wave 3
introduced it. Recorded here so they cannot quietly disappear.

> **Both are now fixed** (`96ba6e1`). Migration **v11** seeds their rules and the
> two handlers fire them: `notify.EventApproverReassigned` from
> `requestReassignApprover`, `notify.EventRequestCancelled` from
> `requestCancelOutright`. `request_cancelled` files under **mention** rather than
> activity, with `request_rejected`: both end the request against the requester's
> wishes and neither leaves them anything to do — it is the finality that earns the
> stronger filter, not an outstanding action. Proved by
> `TestReassignmentAndOutrightCancellationNotifyTheirSubject`, which was verified
> to fail without the two `a.fire` calls. The catalogue is now **23** events, and
> three pinned count tests were updated deliberately rather than absorbed.
>
> The rows below are left as written, because they are the record of what was
> found and how it was proved — which is the whole point of this file.

| # | What | Evidence |
|---|---|---|
| 1 | **The approver-reassignment sheet promises a notification nothing sends.** The overlay Wave 3 added tells the actor *"The new approver is told, and the reminder clock starts again."* The clock half is true — `ReassignRequest` sets `reminder_last_sent=NULL`. The notification half is not: `requestReassignApprover` contains no `a.fire`, and `notify.AllEvents` has no approver-reassignment event at all. This is a fresh instance of **F-D-12**, the exact defect Wave 4 fixed for release/reassign/unhold — reintroduced one screen over by the A7 repair. | Copy at `internal/app/templates.go:2601`; handler `internal/app/app.go:1615-1647` (no fire); vocabulary `internal/notify/events.go:71-79`. |
| 2 | **F-F-06's cancellation fix covers three paths of four.** Both cancellation *decisions* now fire (`EventCancellationAccepted` / `EventCancellationDeclined`, `internal/app/requests.go:1029`,`:1031`) and the ask already did. An **outright cancel** still fires nothing, and there is no event for it in the vocabulary. The finding is closed for the decision paths and open for this one. | `requestCancelOutright` (`internal/app/requests.go:1036-1043`) — no `a.fire`; `notify.AllEvents` (`internal/notify/events.go:71-79`). |

Both were notification gaps, not data-integrity or permission gaps: nothing was
mis-scoped or mis-written, somebody was simply not told. Each got its own
admin-editable event rather than folding into an existing one, for the reason the
vocabulary already applies: two events exist where the sentences are opposites, and
"you have a request to approve" and "your request was cancelled" are addressed to
different people about different obligations.

### Found during the spec-repair pass

| # | What | Evidence |
|---|---|---|
| 3 | **`/payments/{id}` was an existence oracle where `/requests/{id}` is not.** `paymentDetail` answered **403** for a payment whose request is outside the caller's scope, while a payment that does not exist answers 404 — so the pair distinguished "exists but not yours" from "does not exist", letting a caller walk `payments.id`. That is precisely the shape **F-G-002** was raised about, one route over. Wave 3 fixed the request routes and both attachment routes; this one was never in that finding's scope, so nobody looked at it until **two spec agents reached it independently, from opposite directions**. | `internal/app/app.go:913-915`; contrast `loadViewableRequest` and `attachmentDownload`, both 404. |

> **Fixed.** The refusal is now 404 carrying the same sentence a missing payment
> gets, with the real cause logged. `TestPaymentDetailRefusesTheRequestBehindItOutOfScope`
> additionally asserts the refusal is byte-comparable to a missing id and does not
> name the payee or amount of the payment it is withholding.

## Deliberately not fixed

| What | Why |
|---|---|
| **F-D-14, the store half.** `requestTypes` keeps a fifth type the product's chooser does not offer. | The type is not dead code. `POST /requests` reaches the store with whatever the body carried, and that branch is what gives a hand-rolled `recoverable` submission its real refusal — the category's project rule — instead of an unvalidated write or a refusal for a type the store had just accepted. F-E-08's error message depends on it. The reconciliation was therefore made on the handler side, where the silence actually was. |
| **The over-fetch in the notification pager.** The handler asks for `offset+pageSize` rows and drops the ones already shown. | `store.NotificationFilter` has no `Offset`, and the wave that wrote the handler did not own the store. Correct but wasteful; adding `Offset` would let the handler drop the slice. Recorded so it is a known trade rather than an accident. |
| **Drifted `file:line` citations across `docs/qa/`** outside the seven facts the sweep corrected. | `internal/store/migrations.go` alone moved ~165 lines across v8–v11. Fixing a handful while leaving the rest would imply the rest had been checked. One honest note is better than partial repair that reads as complete. |
