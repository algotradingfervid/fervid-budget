# Findings — C: the approver's decisions and the cancellation flow

**Suite** `tests/e2e/audit-c-approvals-cancellation.spec.ts` — 94 executed cases, **`94 passed (10.9s)`, 0
failed** on a fresh server and a fresh database
(`FERVID_E2E_PORT=4303 npx playwright test tests/e2e/audit-c-approvals-cancellation.spec.ts --project=chromium --reporter=line`).
**Test cases** [`TC-C-approvals-cancellation.md`](../test-cases/TC-C-approvals-cancellation.md)
**Use cases** [`UC-B-requests-approvals.md`](../use-cases/UC-B-requests-approvals.md)

Eight findings. Three are represented in the suite by `test.fail()`-annotated tests that assert the
**correct** behaviour and are expected to fail, so the suite is green today and goes red with *"passed
unexpectedly"* the day any of them is fixed. Verified with the JSON reporter that all three genuinely fail
(`status=expected`, `expectedStatus=failed`) rather than quietly passing. No assertion was removed,
loosened or wrapped in a `catch`, and no application code was touched.

| ID | Severity | Type | One line | Test |
|---|---|---|---|---|
| F-C-01 | **high** | validation · spec-divergence | an approver may approve **more** than was asked for, and Accounts can then pay the larger figure and close the request | TC-C-014, 014B |
| F-C-03 | **high** | state-machine · permission | `POST /requests/{id}/approve` unfreezes a request whose cancellation nobody has decided — no reason, no `cancel_decline`, amount rewritten | TC-C-113B |
| F-C-05 | **high** | data-integrity · UX | the loser of two simultaneous decisions gets a **500** `SQLITE_BUSY`, not a refusal | TC-C-134 |
| F-C-07 | **high** | state-machine · information-flow | a declined cancellation destroys Accounts' hold and nobody is told — L7's *"only Accounts lifts"* is broken by two other actors | TC-C-097B, 098 |
| F-C-04 | medium | data-integrity | a refused resubmission has already committed the edit, including the approver reroute | TC-C-123 |
| F-C-02 | medium | spec-divergence | `approval:reassign` is granted to Manager and Admin and no route consumes it; A7 is unshipped but marked verified | TC-C-060, 060B, 061, 062 |
| F-C-06 | informational | UX | the audit screen's **Entity** and **Action** filters omit every request event | TC-C-012 |
| F-C-08 | low | spec-divergence | eleven live statuses, not nine; `draft` is unrepresentable; `partial_review → completed` has no writer | TC-C-119A/B/C |

---

## F-C-01 — an approver may approve more than was requested, and that money can be paid

- **Severity** — high
- **Confidence** — confirmed (reproduced two ways: accepted through the approve sheet and through a direct
  POST, then driven end to end until the larger amount was actually paid and the request closed)
- **Type** — validation · spec-divergence
- **Where** — `internal/store/requests.go:675` (the only guard: `approvedAmount <= 0`),
  `internal/app/linking.go:816` (`approvedOf`), `internal/store/store.go:900–910` (G13's ceiling)
- **Traces to** — UC-B-26 alternate flow 26.c and its open question · coverage A2, L10 · TC-C-014, TC-C-014B

**What happens.** A request for ₹18,400 is approved for ₹25,000. The POST succeeds. The detail screen then
shows **Amount** ₹18,400.00 beside **Approved** ₹25,000.00. Accounts takes the request for processing and
records a payment of **₹25,000** — which is accepted, because G13 caps a payment at `approvedOf(request)`
and that is now ₹25,000. The request closes as `completed` and the requester's own outcome panel reports
₹25,000.00 with *"Difference · confirmed settled by Accounts"*.

**Why it is wrong.** Nothing in the specification forbids the increase, which is why this is reported as a
gap rather than a violation: coverage row **A2** says only *"Approve; may adjust amount"*. But every other
statement about the control is one-directional — the approve sheet's own hint reads *"You may approve a
smaller amount than was asked for."* — and G13 treats the approved amount as the number that authorises a
payment: *"the approved amount is a hard ceiling. Paying more is not a settlement decision, it is a
different obligation — cancel and raise a new request"* (`store.go:901–903`). The one control described as
a reduction is therefore the only place in the product where a single person raises a payment ceiling above
what anybody asked for, and G13's guard is defeated by the same person it is meant to constrain. UC-B-26
records the same open question independently; this finding adds the proof that the consequence is money,
not bookkeeping.

**Reproduction**
1. Raise a request for ₹18,400 to Manager M.
2. As M, open the request, press **Approve ₹18,400.00**, set **Amount approved** to `25000`, approve.
3. Read the detail screen: **Amount** ₹18,400.00, **Approved** ₹25,000.00.
4. As an Accounts user, `/accounts-queue?tab=approved` → **Take for processing** → enter
   **Amount actually paid** `25000` → **Payment settled →** → **Confirm and save payment**.
5. The payment is accepted and the request reads *Completed*.

**Impact.** An approver, acting alone, can authorise and cause the payment of more money than the requester
asked for, with nothing in the audit trail distinguishing it from an ordinary approval and nothing on any
screen calling attention to the increase. The figure is visible, so it is not concealed — but no second
person is asked, and the outcome panel actively reassures the reader that the difference was *confirmed
settled*.

**Evidence** TC-C-014: `POST /approve approved_amount=25000` → 303; **Approved** row `₹25,000.00` against
an **Amount** row of `₹18,400.00`. TC-C-014B: payment of ₹25,000 accepted; status *Completed*; outcome panel
contains `₹25,000.00` and *"confirmed settled"*.

**Suggested direction.** Decide the rule and enforce it in `ApproveRequest`: either cap the approved amount
at the requested amount, or keep the increase and make it explicit — its own audit action, and a
confirmation in the sheet naming the increase.

---

## F-C-03 — the approve route overrules an undecided cancellation request

- **Severity** — high
- **Confidence** — confirmed (reproduced through the HTTP route, and again by reading the guard the route
  shares with the decline path; the damage is visible in four independent places — the status pill, the
  audit trail, the stored `cancel_reason` and the requester's own screen)
- **Type** — state-machine · permission
- **Where** — `internal/store/requests.go:97` (the edge), `:695` (the reuse), `internal/app/app.go:494`
  (the gate) — against `internal/app/app.go:505` and `internal/store/requests.go:934`
- **Traces to** — UC-B-26 precondition 1 and 26.e5 · UC-B-30 · coverage G1, L11 · TC-C-113B

**What happens.** A requester asks for an approved request to be cancelled; the request moves to
`cancellation_requested` and payment freezes. The assigned approver then posts to
`POST /requests/{id}/approve` instead of to `POST /requests/{id}/cancellation`. It is accepted:
**303 → /approvals**. The request returns to `approved`, `approved_amount` is rewritten to whatever the POST
carried and `approved_at` is re-stamped, the audit trail records an ordinary `approve` and **no
`cancel_decline`**, and `cancel_reason` is left behind on the now-approved row. Because `request_detail`
renders the cancellation banner only while the status is `cancellation_requested`, that stale reason is
displayed nowhere: the requester sees a plain *Approved — awaiting payment* request with no trace that
their cancellation ask existed, was overruled, or why. Accounts can then reserve and pay it.

**Why it is wrong.** `legalTransitions` gives `cancellation_requested` two outgoing edges, `cancelled` and
`approved` (`requests.go:97`). The second exists solely so `DecideCancellation(accept=false)` can decline,
and that path is deliberately expensive: it is gated on `approval:cancel`, it **requires** a written note
(`requests.go:911–913`, *"say why it should still be paid"*), and it records its own audit action
`cancel_decline` — because, in the code's own words, *"the requester and Accounts both read it"*.
`ApproveRequest` tests the same edge with the same helper and therefore inherits it while enforcing none of
those three protections. UC-B-26's precondition 1 states *"Status is `pending` — the only status with a
legal edge to `approved`"*, which is not what the table says, and 26.e5 is wrong for this one status. G1
promises the requester that payment stays frozen *until the approver decides*; a decision that leaves no
record is not a decision.

There is also a permission dimension, **derived from the route table rather than separately reproduced**:
unfreezing is gated on `approval:cancel` at `app.go:505` and on `approval:approve` at `app.go:494`. Both
seeded roles that hold either verb hold both, so no seeded role exploits this — but a custom role granted
only the **Approve** matrix cell (`permmap.go:67–71`, which contains `approve` and not `cancel`) would be
able to overrule cancellations it is not authorised to decide.

**Reproduction**
1. Sign in as a Requester and raise a request, choosing Manager M as the approver.
2. As M, approve it.
3. As the requester, open the request, follow **Request cancellation**, give a reason, send. The pill reads
   *Cancellation requested* and the banner reads *Payment is frozen*.
4. As M, `POST /requests/{id}/approve` with a valid `csrf` and `approved_amount=9999`. The screen does not
   offer this — the approve sheet renders only while `pending` — so post it directly.
5. Reload the request as the requester.

**Impact.** The approver-side freeze that G1 exists to guarantee can be lifted without the written reason
the design demands, without the audit action that makes it reviewable, and while silently re-setting the
approved amount. A requester who asked for a payment to be stopped is never told it was refused, and the
trail an auditor would read says only *"approved request … for ₹9,999.00"*. A stale `cancel_reason` is left
on an `approved` row — the exact class of dangling state the code is careful to prevent for `on_hold`.

**Evidence** — audit before/after JSON for one request, read at `/audit?entity=payment_request&id=…`:

```
… Payment Request  cancel_request  "… asked for cancellation of PR-2026-000001: The trip is off.. Payment frozen."
      before  "Status": "approved",                "CancelReason": ""
      after   "Status": "cancellation_requested",  "CancelReason": "The trip is off."
… Payment Request  approve         "… approved request PR-2026-000001 for ₹9,999.00"
      before  "Status": "cancellation_requested",  "CancelReason": "The trip is off."
      after   "Status": "approved",                "CancelReason": "The trip is off."     ← stale
```

Test output, all four soft assertions failing in one run: `a frozen request must not be unfrozen through
the approve route — Expected: not 303 … Received: 303`; `the cancellation is still nobody's decision …
Expected: "Cancellation requested", Received: "Approved — awaiting payment"`; `and unfreezing is a
cancel_decline … Expected substring: "cancel_decline"` not found; `and the requester can still see what
happened to the cancellation they asked for … Expected substring: "The trip is off."` not found.

**Suggested direction.** Give `ApproveRequest` its own status test (`before.Status != "pending"`) instead of
borrowing `canTransition`, so the `cancellation_requested → approved` edge belongs to `DecideCancellation`
alone. If re-approval from a frozen state is genuinely wanted, route it through the decline path so the
reason and the `cancel_decline` action are captured.

---

## F-C-05 — the loser of two simultaneous decisions gets a 500, not a refusal

- **Severity** — high
- **Confidence** — confirmed (three different race pairs, 6/6 across two runs; the cause is in the server
  log and the shape is visible in the code)
- **Type** — data-integrity · UX
- **Where** — `internal/store/requests.go:674–712` (`ApproveRequest`), `:714–749` (`decideRequest`),
  `:909–958` (`DecideCancellation`), `:962–999` (`CancelRequest`); DSN at `internal/store/store.go:36`;
  contrast `internal/store/store.go:736` (`ReserveRequest`)
- **Traces to** — UC-B-26, UC-B-28, UC-B-30 · coverage L11 · TC-C-130, 131, 133, 134

**What happens.** Two sessions of the same approver decide the same request at the same moment — approve vs
approve, approve vs reject, or accept vs decline a cancellation. Exactly one commits, which is correct. The
other is answered **500 Internal Server Error**, with `database is locked (5) (SQLITE_BUSY)` in the log,
about **4 ms** after the request arrived.

**Why it is wrong.** Every decision writer in `internal/store/requests.go` has the shape `BeginTx` →
`requestInTx` (a read) → `UPDATE` → `recordAuditTx` → `Commit`. Two such transactions both take a read lock
and then both need to upgrade it. SQLite does not invoke the busy handler for an upgrade that can never
succeed — retrying would deadlock — so it returns `SQLITE_BUSY` immediately, which is why
`_pragma=busy_timeout(5000)` on the DSN does not help and the failure lands in milliseconds. `classify`
maps only `UNIQUE` violations, so the error falls through `storeErrorStatus`'s default to **500**
(`internal/app/http_errors.go:199–211`).

The store already contains the shape that is safe and says so in a comment: `ReserveRequest` is *"a single
conditional UPDATE … only the first committer matches, so a losing caller sees `RowsAffected()==0` and is
told the request is unavailable"* (`store.go:726–748`). `HoldRequest` and `UnholdRequest` use it too. The
approval writers do not.

**Reproduction**
1. Raise a request to Manager M and sign M in twice, in two browser contexts.
2. From both sessions, `POST /requests/{id}/approve` simultaneously with different `approved_amount` values
   (`Promise.all`, or two `curl` calls backgrounded with `&`).
3. One answers 303. The other renders the 500 error page.

**Impact.** Two people sharing an approver account, one person with the request open in two tabs, or a
double-submitted form all land a manager on an internal-error page for a race the system handled correctly
underneath. Nothing tells them the request was already decided, so the natural next action is to retry.
Data integrity is intact — TC-C-130/131/133 assert exactly one commit, one audit row and one final state,
and those assertions pass — so this is an error-surface defect, not a corruption one. It also means every
concurrent-decision path in the product logs at `ERROR` level, which makes real errors harder to find.

**Evidence**

```
{"level":"ERROR","msg":"request failed","method":"POST","path":"/requests/1/approve",
 "status":500,"error":"database is locked (5) (SQLITE_BUSY)"}
{"level":"ERROR","msg":"request failed","method":"POST","path":"/requests/2/reject",
 "status":500,"error":"database is locked (5) (SQLITE_BUSY)"}
{"level":"ERROR","msg":"request failed","method":"POST","path":"/requests/3/cancellation",
 "status":500,"error":"database is locked (5) (SQLITE_BUSY)"}
```

**Suggested direction.** Either make the decision writers conditional updates in the `ReserveRequest`
mould — `UPDATE … WHERE id=? AND status='pending'`, with `RowsAffected()==0` meaning "already decided" — or
open these transactions as immediate/exclusive so the second waits its turn under `busy_timeout`. At a
minimum, `classify` should map `SQLITE_BUSY` to a conflict the handler can phrase for a person.

---

## F-C-07 — a declined cancellation destroys Accounts' hold, and nobody is told

- **Severity** — high
- **Confidence** — confirmed (reproduced two ways: the request is genuinely re-reservable, not merely shown
  as available; and the notification silence is measured against a control that proves the routing works)
- **Type** — state-machine · information-flow
- **Where** — `internal/store/requests.go:889` (`RequestCancellation` clears the hold), `:941`
  (`DecideCancellation` clears it on both branches and restores it on neither),
  `internal/notify/events.go:9–21` (no event for either decision),
  `internal/app/requests.go:850–867` (neither decision handler calls `a.fire`)
- **Traces to** — coverage **L7** *"On hold (only Accounts lifts)"*, G1 · UC-B-30 BR-30.3 ·
  UC-B **DS10** (the notification half, found independently) · `docs/superpowers/PROGRESS.md:358–361` ·
  TC-C-097, 097B, 098

**What happens.** Accounts puts an approved request on hold with a question — *"Which head should this
hit?"* — which blocks payment until Accounts lifts it. The requester then asks for the request to be
cancelled: the hold is cleared. The approver declines the cancellation: the request returns to `approved`
**with no hold**. It is immediately reservable again — the reservation POST succeeds, which is the only
proof that `on_hold` really is `0`, since `ReserveRequest`'s conditional UPDATE requires it — and the
request is payable with the accountant's question never answered. Meanwhile:

- the hold banner and the *On hold* pill are gone from every screen; the question survives only in the
  audit trail, which no queue links to;
- **no notification fires for either cancellation decision.** There is no event for it at all — the
  vocabulary stops at `EventCancellationRequested` — and neither `requestCancellationDecide` nor
  `requestCancelOutright` calls `a.fire`. So the accountant who was told *"…asked to cancel PR-…, the
  payment is frozen until you accept or decline"* is never told it was declined, and never told their hold
  is gone.

**Why it is wrong.** Coverage row **L7** reads *"On hold (only Accounts lifts)"*. Here two people who are
not Accounts have lifted it between them: the requester by asking to cancel, the approver by declining.
`PROGRESS.md:358–361` documents the *mechanism* — a hold is cleared on every exit from `approved` so that
`on_hold=1` implies `status='approved'` — and that part is a defensible invariant, correctly implemented,
and is **not** what this finding is about. What is documented nowhere is that the hold is not **restored**
on the way back to `approved`, that the request becomes payable in the same instant, and that the one team
whose block was removed is not informed. The invariant was chosen to keep a column tidy; its cost is the
guarantee L7 makes to Accounts.

**Reproduction**
1. Raise a request to Manager M; M approves it.
2. As an Accounts user, open the request and **Put on hold** with the reason *"Which head should this
   hit?"*. The pill reads *On hold* and payment is blocked.
3. As the requester, **Request cancellation** with any reason. The pill becomes *Cancellation requested*
   and the *On hold* pill is gone.
4. As M, open `/requests/{id}/cancellation` and **Decline — keep it live** with a reason.
5. The request reads *Approved — awaiting payment* with no hold. As the Accounts user, open
   `/accounts-queue?tab=approved` — it is there with **Take for processing** — and take it. The reservation
   succeeds.
6. Check `/notifications` as the Accounts user and as the requester: neither has anything about the
   decline.

**Impact.** An accountant's blocking question can be erased by two other people, without their knowledge,
and the payment they were holding becomes payable. If the hold existed because the head was wrong or a
document was missing, the request is now payable with that unresolved — and the only record of the question
is a row in an audit log that no queue links to. The absent notification makes the loss undetectable in
practice: nothing prompts the accountant to look again.

**Evidence** TC-C-098: `POST /requests/{id}/record-payment` after the decline → **303**, status becomes
*With Accounts*. TC-C-097B: the accountant's notice count for the request rises by 1 on the ask (the
control, proving `request_cancellation_requested` does reach Accounts) and by **0** on the decline; the
requester's count is likewise unchanged by the decline; the hold banner is absent and no pill records that
a hold was ever placed; the reason is present only in `/audit`.

**Suggested direction.** Two independent fixes, either of which closes most of the hole: restore
`on_hold`/`hold_reason` when a declined cancellation returns the request to `approved` (the snapshot needed
is already in the audit row), and add a notification event for both cancellation decisions with Accounts on
the routing list. If the hold genuinely should not survive, the decline should at least tell Accounts that
their hold was dropped and why.

---

## F-C-04 — a refused resubmission has already committed the edit

- **Severity** — medium
- **Confidence** — confirmed (the refusal and the applied change observed in the same request cycle; the
  cause is the two-transaction sequence in the handler)
- **Type** — data-integrity
- **Where** — `internal/app/requests.go:645–678` (`requestEdit`: `UpdateRequest`, then `SubmitRequest`),
  `internal/store/requests.go:469` (`canTransition(pending, pending)` is false)
- **Traces to** — UC-B-18, UC-B-19 · UC-B **DS6** (same root cause, different trigger) · coverage A8 ·
  TC-C-123

**What happens.** `POST /requests/{id}/edit` with `submit_action=resubmit` on a request that is still
`pending` answers **400** *"validation failed: a pending request cannot be submitted"* — and the edit has
already been saved. In the recorded run the approver moved from Manager A to Manager B and the short title
changed, both persisted, behind a page that says the operation failed and re-renders the posted values as
if nothing had been written.

**Why it is wrong.** `requestEdit` calls `UpdateRequest`, which commits its own transaction, and only then
`SubmitRequest`, which refuses because `returned → pending` is the only edge into `pending` after creation.
The handler then renders the correction screen with the error, so the reader is told the whole operation
failed. A refusal that applied half its work is worse than either outcome on its own: the user believes
nothing changed, and the request has silently moved to a different approver. UC-B records the same root
cause as **DS6** through a different trigger (a returned request failing the attachment policy on
resubmit); this is the pending-status trigger, which additionally shows that the approver reroute is what
leaks.

**Reproduction**
1. Raise a request to Manager A. Leave it `pending`.
2. `POST /requests/{id}/edit` with a full valid body, `manager_id` = Manager B, a changed `short_title`,
   and `submit_action=resubmit`.
3. The response is 400 with *"a pending request cannot be submitted"*.
4. Reload `/requests/{id}`: the **Approver** row reads Manager B and the title is the new one.

**Impact.** Anyone who reaches the resubmit button from a pending state — a stale returned screen, a
back-button resubmission, a scripted client — changes the request while being told they did not. The
approver reroute is the damaging part: the original approver loses the row from their queue and nobody was
told the change succeeded.

**Evidence** TC-C-123: status 400 with the expected sentence, then
`a refused submission must not have rerouted it — Expected: "cmga …", Received: "cmgb …"` and
`nor renamed it — Expected: "AC-1 …", Received: "AC-1 … corrected"`.

**Suggested direction.** Put the update and the submission in one store call (or one transaction) so a
refused submission rolls the correction back; or make the handler validate the transition before writing
anything.

---

## F-C-02 — `approval:reassign` is a granted permission that no route consumes (A7)

- **Severity** — medium
- **Confidence** — confirmed (the route table read end to end; the path the spec names probed from both a
  Manager and an Admin session; and every other plausible path probed and answered 405)
- **Type** — spec-divergence
- **Where** — `internal/store/requests.go:759` (`ReassignRequest`, no HTTP caller),
  `internal/store/permissions.go:152` (the verb), `internal/store/migrations.go:371` (granted to Manager),
  `internal/app/permmap.go:69` (wired into the **Approve** matrix cell),
  `internal/app/app.go:466` (the path the Phase-2 spec names, registered on `reservation:reassign`)
- **Traces to** — UC-B-31 · UC-B **DV7 / DS11** (found independently) · coverage A7 · TC-C-060, 060B,
  061, 062, 063

**What happens.** Coverage row **A7** — *"Admin reassign (reason + history)"* — has no end-to-end path.
`store.ReassignRequest` implements every rule the spec asks for (pending-only, positive target,
G8-for-reassignment, mandatory reason, its own `approval_reassign` audit action) and nothing calls it. The
verb `approval:reassign` is seeded on Manager and Admin and rendered in the roles matrix, so an
administrator can grant and revoke a permission that does nothing. The one path of the right shape,
`POST /requests/{id}/reassign`, is registered on **`reservation:reassign`** and handles payment-reservation
handover: a Manager holding `approval:reassign` gets **403** there, and an Admin who does hold
`reservation:reassign` reaches a handler whose every refusal is about reservations — *"Choose who should
take this reservation."*, *"Confirm that no payment has been initiated."*, *"That person cannot work the
Accounts queue."* No other plausible path exists either: `/approval-reassign`, `/reassign-approver`,
`/change-approver` and `/approvals/{id}/reassign` all answer **405** for both a Manager and the
administrator.

**Why it is wrong.** The Phase-2 route table maps `POST /requests/{id}/reassign` → `approval/reassign` →
`requestReassign` for A7 (`docs/superpowers/specs/2026-07-25-phase-2-request-workflow-spec.md:206`); the
code maps that path to a different resource and a different behaviour. Worse for anybody reading the
project's own records, the coverage matrix marks A7 **verified**, citing the Go unit test
`P2·T10 TestReassignAndReraise` — a store-level test that exercises `ReassignRequest` directly and never
touches an HTTP route, so it passes while the feature is unreachable. (Coverage row **L1**, *"Draft
(private)"*, has the same shape of problem — see F-C-08.) The practical consequence is that an approver who
should not decide a request — conflict of interest, absence, wrong department — has no way to hand it on.
The only rerouting the product ships belongs to the **requester** (TC-C-063), and it records the change as
an `update` diff on `manager_id` rather than as `approval_reassign`.

**Reproduction**
1. As a Manager holding exactly the Manager role, `POST /requests/{id}/reassign` on a pending request
   assigned to you, with `to_user_id`, `reason` and `confirm=on` ⇒ **403**.
2. As the administrator, the same POST ⇒ **400** *"That person cannot work the Accounts queue."*
3. `POST /requests/{id}/approval-reassign` (or `/reassign-approver`, `/change-approver`,
   `/approvals/{id}/reassign`), as either ⇒ **405**.
4. Open `/requests/{id}` as the approver: the bar offers Approve, Return for correction and Reject only,
   and no `reassign` link or form appears anywhere on the page.

**Impact.** A documented requirement is unmet while the coverage matrix records it as verified, and the
roles screen advertises a control that changes nothing — which quietly weakens trust in the rest of the
matrix. Operationally, a request sent to the wrong approver must be corrected by the requester or
abandoned.

**Evidence** TC-C-060: 403 with the **Approver** row unchanged. TC-C-060B: 405 on all eight probes.
TC-C-061: the three reservation-flavoured refusal messages. TC-C-062: no reassignment control on the page.

**Suggested direction.** Either register a route for `store.ReassignRequest` behind `approval:reassign`
with a screen for it, or remove the verb from the vocabulary and the matrix and record A7 as satisfied by
the requester's approver change. Leaving a granted permission with no effect is the worst of the three.
Either way, the coverage matrix's citation for A7 should name an end-to-end test, not a store test.

---

## F-C-06 — the audit screen cannot be filtered to request events

- **Severity** — informational
- **Confidence** — confirmed (the `<select>` read in the template; the trail reachable only by editing the
  URL)
- **Type** — UX
- **Where** — `internal/app/templates.go:3262` (the **Entity** and **Action** selects), against
  `internal/app/app.go:1364–1368` (the handler, which accepts any value)
- **Traces to** — coverage C2 · TC-C-012, TC-C-044, TC-C-050

**What happens.** `/audit` offers Entity options `payment`, `budget`, `budget_month`, `month_lock`,
`project`, `head`, `user`, `report`, `backup` — and no `payment_request`, although that is the entity type
every request mutation is filed under and the one C2 is about. The rows are visible under **All**, and
`/audit?entity=payment_request&id=123` works when typed by hand, but neither is reachable from the screen.
The **Action** filter has the same gap: it offers none of `submit`, `approve`, `return`, `reject`,
`withdraw`, `reraise`, `cancel`, `cancel_request`, `cancel_decline`, `hold`, `unhold`, `approval_reassign`,
`settle` or `mark_partial`.

**Reproduction** Sign in as the administrator, open `/audit`, and try to list only the approval decisions,
or only one request's history.

**Impact.** An auditor reviewing one request's history has to read an undifferentiated 200-row list or know
to hand-edit the query string. The data is all there; only the way in is missing. Every request-trail
assertion in this suite had to reach the rows through `/audit?entity=payment_request&id={id}` for exactly
this reason.

**Suggested direction.** Add `payment_request` to the Entity list and drive both selects from the
vocabularies that already exist in code — `actionText` and `auditPhrase` know every action name.

---

## F-C-08 — the documented status enum and transition table have drifted from the shipped ones

- **Severity** — low
- **Confidence** — confirmed (the eleven statuses driven or read from code; the dead edge established by
  finding every writer; `draft` by the schema constraint)
- **Type** — spec-divergence
- **Where** — `internal/store/requests.go:86–89` (the seven-item enum and its comment), `:91–101`
  (`legalTransitions`), `internal/store/migrations.go:156` (`CHECK (status <> 'draft')`),
  `internal/store/store.go:925` and `:983` (the only two writers of a completed state)
- **Traces to** — coverage L1, L9, L10, L11 · `docs/superpowers/specs/2026-07-25-payment-requests-overview.md:248`
  · TC-C-119A, 119B, 119C

**What happens.** Three divergences, all in the same area:

1. **Eleven live statuses, not nine.** The overview spec's transition table describes nine and does not
   mention `cancellation_requested`, `cancelled` or `completed_partial`. The shipped set is `pending`,
   `returned`, `approved`, `rejected`, `withdrawn`, `cancellation_requested`, `cancelled`, `processing`,
   `partial_review`, `completed`, `completed_partial`. The `requestStatuses` map lists only the first
   seven; its own comment says Phase 3 appends the rest, and it also names `on_hold`, which is a column
   flag and not a status.
2. **`draft` is unrepresentable, not merely unused.** `status TEXT NOT NULL CHECK (status <> 'draft')`
   makes it impossible for the life of the table, yet coverage row **L1** still names *"Draft (private)"*
   as a state and marks it verified by `TestCreateRequestDraftAssignsNumberAndForcesPayee` — a test whose
   name says draft and whose body asserts that a request is created already `pending`.
3. **`legalTransitions["partial_review"]["completed"]` has no writer.** `RecordPaymentForRequest` writes
   `completed` only `WHERE status='processing'` (`store.go:925`), and `AcceptPartial` writes only
   `completed_partial` (`store.go:983`). So the edge is dead: nothing can move a request from
   `partial_review` to `completed`, which TC-C-119C confirms by driving the accept and observing
   *Completed — partial accepted*.

**Why it is wrong.** A transition table that describes states the product does not have, omits three it
does, and carries an edge no code can traverse is not a usable specification of the state machine — and it
is the document a reader would reach for before changing any of this. `on_hold` appearing in a list of
statuses invites exactly the confusion `activeHold` exists to prevent.

**Reproduction** Drive each of `processing`, `partial_review` and `completed_partial` and read the pill
(TC-C-119A/B/C); grep for writers of `status='completed'`; read `migrations.go:156`.

**Impact.** Documentation only — no user-visible defect. It is reported because two coverage rows (A7 here
and L1) are marked verified on the strength of tests that do not exercise what the row claims, and because
the dead edge is the kind of thing a later change would rely on.

**Suggested direction.** Regenerate the enum comment and the overview's transition list from
`legalTransitions` plus the writers, drop the dead `partial_review → completed` edge, remove `on_hold` from
any list of statuses, and re-point coverage rows L1 and A7 at what actually verifies them.

---

## Confirmations of other agents' findings, and things that are **not** defects

Recorded so the coordinator does not double-count, and so a future reader does not re-investigate.

| Claim | Verdict from this pass |
|---|---|
| UC-B **DV7 / DS11** — `store.ReassignRequest` is unreachable dead code | **Confirmed independently, through routes.** Reported here as F-C-02, extended with the 405 sweep over four other plausible paths and with the observation that the coverage matrix marks A7 verified on a store-level test. |
| UC-B **DS6** — a failed resubmit leaves the corrections saved | **Confirmed, by a second trigger** (pending rather than returned). F-C-04. |
| UC-B **DS10** — no notification fires for a withdrawal, an outright cancellation, or either cancellation decision | **Confirmed empirically**, against a control that proves the routing works. Folded into F-C-07, which is about what that silence costs Accounts; the general claim remains DS10's. |
| UC-B **DS9** — the approvals queue has no tab for requests the manager returned | **Confirmed.** `approvalTabs` covers `pending`, `cancellation_requested`, and `approved`/`rejected`/`cancelled` only, so a returned request is in none of them. TC-C-043 asserts exactly that (0 cards while returned, 1 after resubmission). Not re-raised as a finding of mine. |
| UC-B **DS4** — validation messages reach the user with the `validation failed:` sentinel prefix | **Confirmed.** Every 400 body this suite asserts on contains it — e.g. *"validation failed: a comment is required to return a request"*. Not re-raised. |
| UC-B **DS13** — a refused decision replaces the page with the error page rather than re-opening the sheet | **Confirmed** by TC-C-040/041/045/046, which land on `error_page`. Not re-raised. |
| UC-B-26 precondition 1 — *"`pending` is the only status with a legal edge to `approved`"* | **Refuted.** `cancellation_requested → approved` is also legal, and `ApproveRequest` uses it. F-C-03. |
| "A declined cancellation erases the pending cancellation" | **Partly refuted, in the detail.** The *status* is overwritten; `cancel_reason` is **not** erased — it is left on the approved row and rendered nowhere. That stale column is part of what makes F-C-03 undetectable from a screen. |
| `PROGRESS.md:358–361` — a hold placed before a cancellation ask is dropped, so a decline returns the request to approved with no hold, while the hold event and reason survive in the audit trail | **Confirmed exactly, in both directions** (TC-C-097). The documented mechanism is correctly implemented and is **not itself a defect**. What is not documented — the request becomes immediately payable, L7's *"only Accounts lifts"* is broken, and nobody is told — is F-C-07. |
| `POST /requests/{id}/cancel` (outright) is accepted from `cancellation_requested`, not only from `approved` | **Not a defect.** `legalTransitions` carries the edge deliberately and the outcome — `cancelled`, with a mandatory reason — is the same one accepting the request would reach. Recorded as boundary case TC-C-096. |
| `GET /requests/{id}/cancellation` has no status guard and answers 200 on a closed request | **Not a defect.** The template degrades correctly: on anything that is neither `approved` nor `cancellation_requested` it renders no decision sheet at all and offers only **Back to the request**. Asserted by TC-C-118. |
| Accepting a cancellation needs no note while declining does | **Not a defect.** Deliberate and consistent: the accept sheet labels the field *Note optional*, the decline sheet marks its field required, and `DecideCancellation` demands the note only on the decline branch — because that is the branch the requester and Accounts have to read. Asserted by TC-C-091 and TC-C-093. |
| The approvals queue shows only requests assigned to the caller although Manager carries `request` scope `all` | **Not a defect.** Deliberate: `approvals` hard-codes `Scope: "assigned"` (`app/requests.go:896`) because the queue answers "what is mine to decide". TC-C-005 asserts both halves — 0 cards in the queue **and** 200 reading the other manager's request — so the filter cannot be mistaken for a missing grant. |
