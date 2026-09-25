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

## Fix wave 2026-09-25 (coverage re-verification)

The 2026-09-25 coverage re-verification confirmed **60** defects, grouped into eight
clusters, A–H. Each cluster was fixed on its own branch, reviewed independently, and
merged one at a time into the integration branch `integration/fixwave-2026-09-25`, in
the order H, A, B, C, D, E, F, G. The per-cluster files under
`docs/qa/results/fixwave-2026-09-25/` are kept as the detailed record: browser
evidence, tests changed because they pinned the old behaviour, and the verification runs.
Defect ids are per cluster, so `copy-1`, `test-1` and `recoverables-1` each name
different defects in different clusters, while `audit-2` is one defect that both F and G
fixed (see the integration notes).

| Cluster | Defect | Status | How it was fixed | Tests |
|---|---|---|---|---|
| H | ux-mobile-1 | **Fixed** | Below 860px `--page-gutter` was `0px`; it is now `16px`, so `.page-inner` insets every block, and the two full-width bands use `var(--page-gutter)` instead of a hand-written 14px. An `.action-bar` button whose label names a person ("Save and notify …") now wraps at phone width. | `ux.spec.ts` › "every navigable screen is reachable, fits, and is accessible" (new gutter check at 1280 and 393) |
| H | rbac-11 | **Fixed** | The phone half is the same gutter fix. On desktop `.page-inner` gains `padding-top: var(--page-gutter)`, so a page that opens on a card no longer draws it along the top of the window; an opening banner or grid header cancels the padding and stays flush. | `ux.spec.ts` › "request detail and the deactivation warning keep the gutter at 390 and 1440"; the nav walk's top-inset and flush-band checks |
| H | deactivation-1 | **Fixed** | `.checkline` had `white-space: nowrap`; it is now `white-space: normal; max-width: 100%`, so the deactivation confirmation wraps inside the column. | `ux.spec.ts` › "request detail and the deactivation warning keep the gutter at 390 and 1440" |
| A | rbac-1 | **Fixed** | The Advanced boxes are the grant set: `rolesSave` expands a ticked `cell` only when none of its boxes is ticked (the no-JavaScript path), so unticking one box under a ticked cell revokes that grant. `initRoleMatrix` syncs cell and boxes both ways and draws a half-held cell as `indeterminate`. | `TestRolesSaveHonoursAnUntickedAdvancedBoxUnderATickedCell`; e2e `rbac-1 — an Advanced box unticked under a ticked cell is revoked, and the cell shows the partial state` |
| A | rbac-2 | **Fixed** | An empty or unknown scope now fails closed everywhere a scope is read in SQL — `requestWhere`, `LinkablePaymentRequests`, `ListPayments` and `paymentScopeReaches` — the way `recoverableScope` already did; `/payments/{id}` for an unlinked payment checks it too. | `TestEmptyScopeReadsNoRequestsOrPayments`, `TestRequestScopeNoneShowsNoRowsAnywhere`, `TestPaymentScopeNoneShowsNoLedgerRows`; e2e `rbac-2 — a request scope of None lists and exports nothing, as the detail page already 404s` |
| A | rbac-4 | **Fixed** | `initRoleMatrix` moves the `is-on` class to the checked scope pill on `change`, on the desktop table and the phone accordion alike. | e2e `rbac-4 — the scope pill highlight follows the selection` |
| A | rbac-5 | **Fixed** | Both cell renderings mark a partially held cell `data-partial="1"`, drawn as `indeterminate`, and the accordion badge counts grants rather than cells ("4 of 15", not "0 of 7"). | `TestRolesSaveHonoursAnUntickedAdvancedBoxUnderATickedCell`, `TestBuildPermMatrixMarksGrantedPartialAndUnavailableCells` (updated); e2e `rbac-5 — a row held only through Advanced shows partial cells and counts its grants` |
| A | rbac-6 | **Fixed** | The delete sheet for a role somebody holds explains that it cannot be deleted yet, offers "Open Users" and has no submit button; the pill and note use `plural` ("1 user"). | `TestRoleDeleteSheetExplainsAHeldRole`; e2e `rbac-6 — the delete sheet for a held role explains itself instead of dead-ending on a 403` |
| B | rbac-8 | **Fixed** | Only the request's current approver (with `approval:reassign`) or a holder of `user:edit` may reassign, and nobody may reassign to themselves. Review fix-up: `ReassignRequest` takes `asAdministrator` and re-checks the current approver inside the write transaction, with the UPDATE also matching `manager_id`, so a former approver racing an administrator is refused. | `TestOnlyTheCurrentApproverOrAnAdministratorMayReassignAnApproval`, `TestApproverReassignmentRoute` (updated), `TestReassignmentIsRefusedToAnApproverTheRequestHasAlreadyLeft` |
| B | rbac-9 | **Fixed** | `store.RequestThread` resolves the ids in a `manager_id` change to names (`nameUsersInChanges`), so the chip reads "Approver Mona Manager → Max Manager". | `TestHistoryNamesTheApproversAChangeMovedBetween` |
| B | rbac-10 | **Fixed** | The edit form's banner and submit button follow the chosen approver through `data-follows-select` and `initFollowSelects`. | `TestEditFormLabelsFollowTheChosenApprover` |
| B | deactivation-2 | **Fixed** | `ReassignRequest` accepts every status the deactivation warning lists (pending, returned, cancellation_requested, partial_review), and the warning names the "Reassign approval" control. Review fix-up: the `approval_reassigned` notice now reads "{{number}} was reassigned to you as its approver"; migration **v13** (`UpReassignedNotificationWording`) carries it onto installed databases only where the row still reads as v11 wrote it. | `TestReassignmentRescuesEveryStatusTheDeactivationWarningLists`, `TestMigrationV13RewordsTheReassignmentNoticeWithoutTouchingAnAdminsEdit` |
| B | history-1 | **Fixed** | The eight payment-side audit summaries start with the actor's name ("Aarav Accounts lifted the hold"), like the request-side ones. Review fix-up: TC-G-063 now matches `'reserved the request for processing'`, the new wording in full. | `TestDetailThreadNamesTheActorForAccountsEvents`, `TestTrailBodyDropsTheActorNameTheHeadAlreadyCarries`; TC-G-063 in `tests/e2e/audit-g-information-flow.spec.ts` |
| B | copy-1 | **Fixed** | `statusPhrase` names a status with its article in plain words, and all eight transition refusals use it ("an approved request cannot be returned"). | `TestIllegalTransitionRefusalsReadAsSentences` |
| B | copy-2 | **Fixed** | Same change: "an approved request cannot be withdrawn". | `TestIllegalTransitionRefusalsReadAsSentences` |
| B | hold-1 | **Fixed** | New route `POST /requests/{id}/attachments` lets the requester add a document without changing the request. Review fix-up: it is open on hold only — the handler checks `req.OnHold` and `store.AddHeldRequestAttachment` re-reads the row inside the write transaction, so a hold lifted between page load and upload refuses too. | `TestRequesterCanAddADocumentWhileTheRequestIsOnHold`, `TestHeldRequestAttachmentIsOpenExactlyAsWideAsTheHold` |
| C | form-1 / recoverables-1 | **Fixed** | The store's `recoverable` type has its own chooser card ("Deposit or guarantee") and form with a free "Paid to"; only the employee advance forces payee = requester and fixes its category, and `validateRequestInput` enforces both. Review fix-up: a legacy employee advance filed under a deposit category now carries its notice on the returned screen too, whose buttons read "Resubmit as an Employee advance", so the reclassification is stated before the press. | `TestValidateRequestInputPerType`, `TestEmployeeAdvancePayeeIsForcedAndADepositPayeeIsFree`, `TestDepositIsItsOwnTypeAndPaysTheCounterparty`, `TestDepositPayeeIsOnTheEditFormAndSurvivesTheReturnedScreen`, `TestATypeWithNoCardIsRefusedRatherThanSilentlyBounced`, `TestRequestNewTypeChooser`, `TestRecoverableCategoryPickerRendersTheLiveCategories`, `TestLegacyEmployeeAdvanceUnderADepositCategoryStaysEditable`, `TestLegacyDepositReturnedScreenSaysWhatResubmittingDoes`, `TestEditFormWarningsCarryTheirOwnHeadings`, `TestRetiringTheEmployeeAdvanceCategoryIsNotMistakenForALegacyRow` |
| C | form-3 | **Fixed** | `offerRetiredRefs` puts a project or head retired since the request was raised back into the edit form, marked "(retired)", with a banner; saving it is refused with "that budget head has been retired" instead of "project and head are required". | `TestEditFormOffersARetiredProjectBackAndSaysWhy` |
| C | form-7 | **Fixed** | An appended block in `fervid-ds.css` sets `align-content: start` on fields and makes `.m-half` fields span the one-column grid at ≤860px, so controls line up. | None automated; browser measurements in the cluster file |
| C | form-6 | **Fixed** | Coverage rows T10, T11 and T12 now cite tests that prove the whole requirement, and the missing tests were written. | `TestCompulsoryAttachmentsAreEnforcedOnSubmit`, `TestEmployeeAdvancePayeeIsForcedAndADepositPayeeIsFree`, `TestRetiredProjectIsHiddenFromNewRequestsAndKeptOnHistory` |
| C | recoverables-7 | **Fixed** | Every recoverable category may link a project; it is required only where the category's `RequiresProject` says so, and a retired project is refused. | `TestEveryRecoverableCategoryMayLinkAProject` |
| C | recoverables-5 | **Fixed** | Each recoverable category row on `/configuration` has a name input, a Requires select and a Save button; the code stays fixed as the stable identity. | `TestRecoverableCategoriesAreEditableInPlace` |
| C | recoverables-10 | **Fixed** (removed) | The dead `allow_direct_payments` toggle is gone from `configSections`, with a note that every payment starts from an approved request; the seeded row stays, unread. | `TestConfigurationScreenReadsAndWritesAppSettings` |
| C | recoverables-9 | **Fixed** | `TestNoForfeitureRoute` also reflects over `*store.Store`'s methods, so coverage row X4's "no store method" claim is true. | `TestNoForfeitureRoute` |
| C | tests-1 | **Fixed** | A test pins that the F-E-07 hint says the employee advance fills in the payee, never the counterparty. | `TestConfigurationHintNamesThePayeeNotTheCounterparty` |
| D | settlement-1 | **Fixed** | The four settlement notifications name the amount actually paid (`{{paid_amount}}`, `{{paid_on}}`) beside the approved figure. Migration **v14** (`UpPaidAmountTemplates`) rewrites only rows still on the old seeded default. | `TestSettlementNotificationsNameTheAmountActuallyPaid`, `TestMigrationV14RewritesOnlyTheUneditedSettlementTemplates` |
| D | settlement-2 | **Fixed** | `conflictCause` has an `unclaimed` cause: the screen says nobody holds the request, shows the true pill and offers "Take it for processing". Still 409, as TC-A-102 pins. | `TestUnclaimedEntryScreenSaysNobodyHoldsItAndOffersToTake` |
| D | settlement-3 | **Fixed** | Partial reviews appear in an Approvals tab, a Home metric and area, the nav badge and the needs-me count for a holder of `approval:accept_partial`. | `TestManagerHomeAndApprovalsQueueListPartialReviews`, `TestNeedsMeCountsPartialReviewsForTheManagerAndOpenConcernsForTheHolder` |
| D | settlement-4 | **Fixed** | Request detail offers "Record payment" (`POST /requests/{id}/record-payment`) to a holder of `reservation:reserve` on an approved, unclaimed request not on hold. | `TestRequestDetailOffersRecordPaymentToAccounts` |
| D | settlement-5 | **Fixed** | The "Payment saved" banner is tied to a one-shot outcome cookie (`fervid_payment_outcome`), so a refresh or bookmark never replays it; the read-only card says the payment can no longer be edited on every visit. | `TestPaymentSavedBannerAppearsOnlyAfterTheConfirmingPost`; TC-D-084 (updated) |
| D | settlement-6 | **Fixed** | On the htmx path the settle sheet's ✕ and "Go back" carry `data-close`, so the sheet closes in place and the typed values survive. | `TestSettlementSheetClosersDismissInPlaceOnTheHtmxPath`; TC-D-062 (extended) |
| D | settlement-7 | **Fixed** | Request rows carry `ProcessingByName`; `statusPill` reads "With Accounts — taken by {name}" and `waitingOn` names the holder. | `TestProcessingRequestNamesTheHolderEverywhere`, `TestRequestRowsCarryTheHolderName`, `TestWaitingOnNamesWhoeverOwesTheNextAction` |
| D | settlement-8 | **Fixed** | A `concern_open` flag (migration **v15**) marks a raised concern until the holding accountant answers or the manager accepts; the pill reads "Partial — under discussion" and the screens say whose turn it is. The status is untouched. | `TestConcernDisplaysAsUnderDiscussionUntilAccountsAnswers`, `TestRaiseConcernMarksTheRequestUnderDiscussionUntilAccountsAnswers` |
| D | queue-1 | **Fixed** | The stale-reservation screen names the holder to a non-holder and, when the reader cannot act, replaces "Pick one" with who can. | `TestStaleScreenTellsANonHolderWhoCanAct` |
| D | queue-2 | **Fixed** | "Payment settled →" is gated on `payment:settle`, like the preview and the write; a role without it sees why. | `TestEntryScreenWithholdsSettleFromAPayerWithoutTheVerb` |
| D | partial-1 | **Fixed** | The partial review says "Still owed to the payee", its comment prompt follows the reader's seat, and an accepted shortfall reads "Balance written off". | `TestPartialScreensNameThePayeeAndStopOwingAfterAcceptance`, `TestPartialReviewScreenAndManagerDecision` (updated) |
| E | notify-1 | **Fixed** | Email and in-app now share one routing rule, `routesToHolder`: the seven reservation events go to the accountant holding the reservation, falling back to the group only when nobody holds it. | `TestRunRemindersNotifiesTheAssignedAccountant`, `TestNotifyResolvesRecipientsForTheAuditEvents` (both gained email assertions) |
| E | notify-2 | **Fixed** | `renderInAppBody` drops lines whose field came out empty; the centre keeps line breaks and shows the row's own link as link text. | `TestNotifyInAppBodyDropsEmptyFieldLinesAndKeepsLineBreaks`, `TestRenderInAppBody`, `TestNotificationCentreRendersBodyLinesAndLink`, `TestNotifBodyOnlyLinksTheRowsOwnTarget` |
| E | notify-3 | **Fixed** | `buildRFC822` Q-encodes a non-ASCII Subject and From name per RFC 2047. | `TestBuildRFC822EncodesNonASCIIHeaders` |
| E | ux-2 | **Fixed** | The rules screen's duplicate callouts are gone; a rule refused for an unknown field reopens its sheet with the typed values and the reason. | `TestAdminNotificationsShowsEachResultOnceAndKeepsARefusedRule` |
| E | test-1 | **Fixed** | A missing test: management recipients are copied on the `request_approved` email, and mutation-checked. | `TestNotifyCopiesManagementOnTheApprovalEmail` |
| E | urgent-1 | **Fixed** | `LinkablePaymentRequests` selects `urgent` and sorts it first on every tab but Paid; the queue, dashboard and picker show the Urgent pill. | `TestAccountsQueueMarksAndSurfacesUrgentRequests` |
| E | notifications-1 | **Fixed** | On a returned request, the full edit form's "Save and notify {approver}" sends `submit_action=resubmit`, so one press saves, resubmits and fires `request_edited`, as the label promises. | `TestFullEditOfAReturnedRequestResubmitsAndNotifies` |
| E | copy-1 | **Fixed** | The Configuration note reads "Reminders are counted in days, not working hours", and three manual pages match. | `TestReminderCopyReadsTheConfiguredThresholds` (extended) |
| F | recoverables-2 | **Fixed** | Payment readers return the linked request's `Treatment`; `ListPayments` takes `ExcludeRecoverable` for the grid's Recent Payments, and a head-less recoverable is labelled "Recoverable", never "/". | `TestListPaymentsCanLeaveOutRecoverablesAndCarriesTreatment`, `TestRecoverablePaymentStaysOffTheGridAndIsNamedNotSlashed` |
| F | grid-2 | **Fixed** | Same root cause and fix as recoverables-2: the grid panel no longer lists recoverables. | as recoverables-2 |
| F | recoverables-1 | **Fixed** | `RecoverableRollups` and the register's `ageing=overdue` filter count a row overdue only once paid, so rollups add up to the Total and the tile. | `TestOverdueRollupAndRegisterFilterIgnoreMoneyThatNeverLeft` |
| F | recoverables-6 | **Fixed** | Empty tfoot spacer cells on `/recoverables/list` and `/vendors` are `.d-only`, so the phone total is a compact card. | `TestTCardsTotalRowsCarryNoEmptyMobileCells` |
| F | grid-1 | **Fixed** | Recent Payments renders only with `payment:view`, instead of claiming the month is empty. | `TestGridHidesThePaymentsPanelRatherThanClaimingItIsEmpty`, `TestGridIsGatedOnGridViewAndItsPaymentPanelOnPaymentView` (updated) |
| F | grid-pill-1 | **Fixed** | `.pill.on-track` is green and uses `var(--track)`, matching the legend and the bar. | e2e in `tests/e2e/grid-reports.spec.ts` |
| F | grid-htmx-1 | **Fixed** | The grid filters swap over htmx with a pushed URL and still work without JavaScript. Review fix-ups: `hx-sync="this:queue last"` plus a dedupe so one gesture sends one request with no abort errors (TC-G-042), and `hx-history="false"` so Back and Forward re-render the rows and controls together. | `TestGridFiltersSwapOverHTMXAndStillWorkWithoutIt`; e2e "grid-htmx-1: search then Enter logs no errors…"; TC-G-042 (unchanged) |
| F | ui-1 | **Fixed** | Head counts pluralise on the grid and `/months`, `td.actions-cell` stays a table cell, and a refused Add user keeps the drawer open with the input (but not the password). | `TestGridAndMonthsPluraliseHeadCounts`, `TestAddUserValidationKeepsTheDrawerAndTheInput` |
| F | audit-2 | **Fixed** | `SetBudgets` skips an unchanged head and names the change ("Operations / Utilities 2026-08: ₹0.00 → ₹1,000.00", or "set to ₹X"). Review fix-up: on the first save of an empty month, a head with no budget submitted at ₹0 is neither written nor audited. | `TestSetBudgetsAuditsOnlyChangedBudgetsWithAReadableSummary` |
| G | backup-1 | **Fixed** | `RestoreBackup` moves the target's `-wal` and `-shm` aside with the old database, so SQLite cannot replay the pre-restore WAL over the restored file. | `TestRestoreBackupOverDatabaseWithLiveWALServesTheBackup` |
| G | backup-2 | **Fixed** | New `store.DailyBackup`, started by `cmd/server`, makes one backup a day after `FERVID_BACKUP_HOUR` (default 2) and prunes. | `TestDailyBackupRunsOncePerDayAfterTheConfiguredHour`, `TestDailyBackupCountsOnlyBackupsMadeAfterTheHourToday`, `TestDailyBackupPrunesWithTheRetentionRules`, `TestDailyBackupSchedulerRunsOnStartAndStopsOnCancel`, `TestLoadDefaultsAndEnvironmentOverrides` (extended) |
| G | backup-3 | **Fixed** | `PruneBackups` keeps 30 days plus the newest backup of each of the previous `FERVID_BACKUP_KEEP_MONTHS` (12) months, ageing a backup by the timestamp in its name. | `TestPruneBackupsKeepsThirtyDaysAndTheNewestBackupOfTwelveMonths`, `TestPruneBackupsAgesByTheTimestampInTheName` |
| G | audit-1 | **Fixed** | `Store.AuditPage` filters the whole log in SQL, including a from/to date range, and `/audit` pages 100 rows at a time with the scope applied before paging. | `TestAuditLogFiltersEveryRowAndPages`, `TestAuditLogFiltersByDate` |
| G | audit-2 | **Fixed** | Fixed here too; the merged `SetBudgets` keeps F's wording and G's ordering, so an unknown head still fails the batch. See the integration notes. | `TestSetBudgetsRefusesAnUnknownHeadEvenAtZero`, `TestSetBudgetsIsAtomic` |
| G | docs-1 | **Fixed** | The design notes' §14 is rewritten against the code (no Authboss, Casbin or JSON seed), and `docs/OPERATIONS.md` and the Backups manual page describe the daily job, retention and WAL handling. | `TestSeedCreatesTheAdminAndThreeSampleProjects` |
| G | test-1 | **Fixed** (for the promises that hold) | Tests added for restore over a WAL, daily scheduling, monthly retention, seed content and "Paid on" defaulting to today. The Accounts `/export.csv` item was refuted: RT-21 already asserts the 403. | the backup and seed tests above, `TestRecordPaymentPaidOnDefaultsToToday` |

### Integration notes

- **Migrations.** B's `notification_reassigned_wording` stays **v13**. D's
  `notification_paid_amount` was written as v13 on its branch and is now **v14**, and
  D's `request_concern_open` is now **v15** (was v14). Every cluster file that says
  "v13" for the paid-amount rewrite means v14 in the merged tree; the store test is
  `TestMigrationV14RewritesOnlyTheUneditedSettlementTemplates`.
- **audit-2 was fixed by both F and G in `SetBudgets`.** The merged implementation
  keeps F's wording (`Project / Head YYYY-MM: set to ₹X` for a new budget,
  `… ₹A → ₹B` for a change), F's rule that untouched heads submitted at ₹0 with no
  budget are neither written nor audited, and G's ordering: the head lookup runs before
  the skip, so an unknown head still fails the batch, with the message
  `unknown budget head`. G's duplicate test
  `TestSetBudgetsAuditsOnlyChangedBudgetsAndNamesHeadAndMonth` was removed; F's
  `TestSetBudgetsAuditsOnlyChangedBudgetsWithAReadableSummary` and the new
  `TestSetBudgetsRefusesAnUnknownHeadEvenAtZero` cover it.
- F's `TestListPaymentsCanLeaveOutRecoverablesAndCarriesTreatment` now passes
  `Scope: ScopeAll`, because A's rbac-2 made an empty payment scope fail closed.
- **Minor issues the reviewers reported, fixed at integration:**
  - the `PaymentListOptions.Scope` comment now says the ledger fails closed;
  - the reservation-conflict "taken" screen and the reservation-stale, request-cancel,
    request-submitted, similar-request-card, recoverables-register and
    recoverable-detail pills all go through `statusPill`/`rowPill`, so they read
    "With Accounts — taken by {name}" and "Partial — under discussion"
    (`TestLesserScreensShareTheStatusPill`, and the extended
    `TestUnclaimedEntryScreenSaysNobodyHoldsItAndOffersToTake`);
  - `/audit` with an offset past the end lands on the last real page
    (`TestAuditLogFiltersEveryRowAndPages`, `TestPageAuditEntriesClampsAnOffsetPastTheEnd`);
  - a flash `.alert` rendered ahead of a `.page-banner`/`.gridhead` now opens the page
    as a full-width strip with the band flush beneath it (`ux.spec.ts` › "a flash ahead
    of the page banner opens the page as a strip").
- **Already covered by B's fix-up, and only verified at integration:** rbac-8's
  in-transaction "current approver" check (`requests_test.go`, the former-approver
  stale-read case), hold-1's hold-only attachment route
  (`TestRequesterCanAddADocumentWhileTheRequestIsOnHold`), and the
  `approval_reassigned` wording
  (`TestMigrationV13RewordsTheReassignmentNoticeWithoutTouchingAnAdminsEdit`).

---

## Still open

Nothing rated critical or high. What remains, each verified against source rather than inferred from a green test:

| Finding | Why it is still open |
|---|---|
| F-G-011 | `/requests/export.csv` has no approved-amount column. |
| F-G-024 | A head with an approved request against it can be deactivated with no check and no warning. The resulting refusal is coherent — a 400 with a sentence, never a 5xx. |

### Closed since this table was written

| Finding | How it was closed |
|---|---|
| F-G-020, remaining edge | **Fixed.** The payment form warned only about the month it *opens* on; a locked month picked in "Paid on" afterwards was met at the confirmation. `#paid_on` now re-asks `GET /payments/lock-status?paid_on=` on `change` (gated `payment:create`, like `/payments/new`) and swaps `#lock-banner` for the fragment `payment_lock_banner`, which the form also renders on load — one template, one sentence. The handler reads `store.MonthIsLocked`, so a failed read is a store error rather than a silent "open"; an empty or malformed date answers the empty wrapper. The banner moved into the Payment fieldset, directly above "Paid on", so it is on screen beside the field at phone width too. Proved by `TestPaymentLockStatusFollowsTheDateField` (red first: 404, no route) and the e2e case *"F-G-020 warns about a locked month as soon as Paid on names it"* in `tests/e2e/regression-issues.spec.ts`. |
| F-G-025, open half | **Fixed** in fix wave 2026-09-25 (cluster B, deactivation-2): the deactivation warning lists the approvals waiting on the person, every status it lists (pending, returned, cancellation requested, partial review) can now be reassigned, and its copy names the "Reassign approval" control; cluster H's deactivation-1 made its confirmation wrap at phone width. Proved by `TestReassignmentRescuesEveryStatusTheDeactivationWarningLists`. |
| Coverage matrix **V6**, **A8** re-notify half | **Pinned**, and the coverage matrix now marks both rows Verified. A8's re-notify half is proved by `TestEditingAPendingRequestReNotifiesTheApprover` (a pending edit notifies the assigned approver, mutation-checked), and fix wave 2026-09-25 (cluster E, notifications-1) added the returned-request path, `TestFullEditOfAReturnedRequestResubmitsAndNotifies`. V6 is proved by `TestRecoverableCreateRecordsCounterpartyReturnDateAndNotes`, which cluster C kept green after deposits gained a payee. |

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
