# TC-C — the approver's decisions and the cancellation flow

**Suite** `tests/e2e/audit-c-approvals-cancellation.spec.ts`
**Findings** [`findings-c-approvals-cancellation.md`](../results/findings-c-approvals-cancellation.md) — eight,
ids `F-C-01`…`F-C-08`. Each is named in the row that found it.
**Use cases** [`UC-B-requests-approvals.md`](../use-cases/UC-B-requests-approvals.md) — this document uses UC-B's
IDs, its actor codes (AC1–AC5), its selector vocabulary (SV1–SV31) and its conventions (CV1–CV7).
**Coverage matrix** `docs/superpowers/specs/2026-07-25-payment-requests-coverage.md`

```
FERVID_E2E_PORT=4303 npx playwright test tests/e2e/audit-c-approvals-cancellation.spec.ts \
  --project=chromium --reporter=line
```

**Result of the recorded run — `94 passed (10.9s)`, 0 failed, fresh server and fresh database.**
Three of the 94 are `test.fail()`-annotated: they assert what the design requires, they fail because the
product does not do it, Playwright counts an expected failure as a pass, and the day any of the three is
fixed the suite goes red with *"passed unexpectedly"*. Nothing was weakened to reach green. Verified with
the JSON reporter that all three really are failing rather than quietly passing:

```
expected   expectedStatus=failed   TC-C-113B — approving a request whose cancellation is undecided …
expected   expectedStatus=failed   TC-C-123  — a refused resubmission must not leave the edit applied …
expected   expectedStatus=failed   TC-C-134  — the loser of a decision race must be refused, not …
```

**The eleven live statuses.** The Phase-2 enum in `internal/store/requests.go:86–89` lists seven and its
comment says Phase 3 appends more; the shipped set is **eleven** — `pending`, `returned`, `approved`,
`rejected`, `withdrawn`, `cancellation_requested`, `cancelled`, `processing`, `partial_review`,
`completed`, `completed_partial`. `draft` is not merely unused but **unrepresentable**:
`status TEXT NOT NULL CHECK (status <> 'draft')` (`internal/store/migrations.go:156`). The legality grid in
§1.8 is built on the real eleven, minus `draft`, which no route can attempt (see F-C-08).

---

## 0. Scope, and what this document deliberately does not cover

| ID | In scope | Out of scope (owner) |
|---|---|---|
| SC1 | The approvals queue: who reaches it, what it filters, whether its counts are honest | Raising a request — the type chooser, the adaptive form, per-type validation (UC-B-01…UC-B-15) |
| SC2 | Approve, approve-for-less, and every adversarial approved amount | Reservation, payment entry, settlement, partial review (UC-C-*) |
| SC3 | Who may decide: G8, the assigned-manager rule, the route gates | Notifications and reminders (UC-C-*) |
| SC4 | Return and reject: the text requirement, editability, terminality | The recoverable register |
| SC5 | Reassignment — as a proof of absence | |
| SC6 | A6: no bulk approval | |
| SC7 | The cancellation flow: all five routes, both actors, and the hold interaction | |
| SC8 | The legal-transition grid, exhaustively, driven over HTTP | |
| SC9 | Edit-while-pending rerouting, from the approver's side | |
| SC10 | Concurrency: two decisions on one request | |

**The cast.** Built once per run by the file-level `beforeAll`, every subject holding **exactly** the roles
named — `audit-support.createUserWithExactRoles`, because a "Manager" made the `fixtures.ts` way is
Accounts + Manager and every refusal you expected becomes a 200.

| ID | Subject | Roles | Why it exists |
|---|---|---|---|
| CA1 | `requester` | `[Requester]` (AC1) | raises every request; `request` scope `own` |
| CA2 | `outsider` | `[Requester]` (AC1) | holds `request:cancel` but owns nothing — proves a verb is not a row |
| CA3 | `mgrA` | `[Manager]` (AC2) | the approver every request is sent to |
| CA4 | `mgrB` | `[Manager]` (AC2) | an approver it was **not** sent to |
| CA5 | `accounts` | `[Accounts]` (AC3) | holds `payment:hold`, holds no approval verb |
| CA6 | `nobody` | `[]` | signed in, permitted nothing |
| CA7 | `dual` | `[Requester, Manager]` | the sharpest G8 probe: may raise **and** may approve |
| CA8 | `boss` | seeded `admin@fervid.local` (AC4) | all 66 grants, and nobody's approver |

---

## 1. Test cases

`traces to` cites the UC-B use case and the coverage-matrix IDs. `type` is one of
`functional · permission · negative · boundary · concurrency · information-flow · regression ·
proof-of-absence`. Every **expected result** was written from the spec and the code before the suite was
run; **actual result** and **verdict** were filled in afterwards.

### 1.1 The approvals queue

| TC ID | Title | Traces to | Type | Pri | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-C-001 | Anonymous caller is redirected off `/approvals` | UC-B-25 · A5, R6 | permission | high | none | `GET /approvals` with no cookies, redirects not followed | 3xx (**303** See Other, CV1) with `Location` containing `/login`; never a 200 | 303 → `/login` | PASS |
| TC-C-002 | A Requester is refused the queue | UC-B-25 · A5, R6 | permission | critical | CA1 signed in | `GET /approvals` | **403** — the route is gated on `approval:approve`, which AC1 does not hold | 403 | PASS |
| TC-C-003 | An Accounts user is refused the queue | UC-B-25 · A5, R6 | permission | critical | CA5 signed in | `GET /approvals` | **403** — AC3 holds every payment verb and no approval verb | 403 | PASS |
| TC-C-004 | A role-less user is refused the queue | UC-B-25 · R6 | permission | high | CA6 signed in | `GET /approvals` | **403** | 403 | PASS |
| TC-C-005 | The queue filters on `manager_id`, not on the caller's `request` scope | UC-B-25 · A5, R3 | information-flow | critical | one pending request to CA3, one to CA4 | CA3 opens `/approvals?bucket=to-approve&q=<title>` for each; then `GET /requests/{other id}` | CA3's queue holds their own row (1 card) and **not** CA4's (0 cards) although Manager carries `request` scope `all`; the same row is nevertheless **readable** at 200 — the queue is a design choice, not a missing grant (`internal/app/requests.go:896`) | 1 / 0 / 200 | PASS |
| TC-C-006 | Dashboard metric = queue tab count = rows shown | UC-B-32 · A5 | information-flow | high | ≥2 pending requests to CA3 | read `.metric-value` under *"Awaiting my approval"* on `/`, then the **To approve** tab `.n` and the `.req-card` count on `/approvals` | all three equal, and ≥ 2 — both numbers come from the same `CountRequests(scope=assigned, statuses=[pending])` (`dashboard.go:84`, `requests.go:896`) | equal, ≥2 | PASS |
| TC-C-007 | A request moves between the three tabs as its status changes | UC-B-25, UC-B-30 · A5, L6, L11 | functional | high | approved request to CA3 | check the three buckets after approve, after the requester's cancellation ask, and after CA3 accepts | approved ⇒ 0 in *To approve*, 1 in *Decided*; asked ⇒ 1 in *Cancellations*, 0 in *Decided*; accepted ⇒ 0 in *Cancellations*, 1 in *Decided* | as expected | PASS |
| TC-C-008 | An unknown `?bucket=` falls back to *To approve* | UC-B-25 | negative | low | CA3 signed in | `GET /approvals?bucket=not-a-tab` | the tab carrying `aria-current="page"` is *To approve* — `knownApprovalTab` rejects the value rather than emptying the list (`requests.go:889–891`) | *To approve* | PASS |

### 1.2 Approving, and approving for an adjusted amount

| TC ID | Title | Traces to | Type | Pri | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-C-010 | The assigned manager approves the full amount through the sheet | UC-B-26 · A2, L6, C3 | functional | critical | pending ₹18,400 to CA3 | open the request, assert *Waiting on you*, press `/^Approve /`, assert **Amount approved** pre-filled `18,400.00` (SV18), press **Approve request** | 303 → `/approvals`; status *Approved — awaiting payment*; the **Approved** row of the detail `.dl` reads `₹18,400.00` | as expected | PASS |
| TC-C-011 | An approver may approve **less** than was asked for | UC-B-26 (26.a) · A2 | functional | critical | pending ₹18,400 to CA3 | in the sheet set 12000, fill `/^Note/`, approve | **Amount** row still `₹18,400.00` and **Approved** row `₹12,000.00` — the request and the decision are two different numbers | as expected | PASS |
| TC-C-012 | `approved_amount`, `approved_by` and `approved_at` are written, audited against `entity_type='payment_request'` | UC-B-26 · C2, C3 | information-flow | critical | request approved for less | CA8 opens `/audit?entity=payment_request&id={id}` | one row: entity *Payment Request*, action `approve`, summary *"&lt;approver&gt; approved request …"* with `₹12,000.00`; the after-JSON carries `"ApprovedBy": <id>`, `"ApprovedAt": "20…"`, `"ApprovedAmount": 1200000` (paise) | all present | PASS |
| TC-C-013 | The requester reads the outcome on their own request | UC-B-16, UC-B-26 · Q4, L6 | information-flow | critical | request approved for less | CA1 opens `/requests/{id}` | pill *Approved — awaiting payment*; `.banner.locked` *"Approved requests are locked"*; **Edit request** gone; the approval on the thread | as expected | PASS |
| TC-C-014 | An adjusted amount **greater** than requested is accepted | UC-B-26 (26.c) · A2 | boundary | high | pending ₹18,400 to CA3 | `POST /approve` with `approved_amount=25000` | Accepted (303). `ApproveRequest` checks only `> 0` (`store/requests.go:675`); nothing caps it, and `approvedOf` then makes ₹25,000 the ceiling Accounts may pay. Recorded as **F-C-01** — a gap in the specification, not a violation of it | 303; **Amount** ₹18,400.00, **Approved** ₹25,000.00 | PASS (finding F-C-01) |
| TC-C-014B | The raised ceiling is real money: Accounts pays above the request and it completes | UC-B-26 (26.c), UC-C-* · A2, L10 | functional | high | pending ₹18,400 to CA3 | approve for ₹25,000; CA5 takes it for processing and settles ₹25,000 | G13 caps a payment at `approvedOf(req)` (`store.go:900–910`), which is the approved amount — so the over-approval **authorises** the larger payment. Expected: the payment is accepted and the request completes | 303; status *Completed*; the outcome panel reads ₹25,000.00 and *"confirmed settled"* | PASS (finding F-C-01) |
| TC-C-015 | An approved amount of **zero** is refused | UC-B-26 (26.e2) · A2 | negative | high | pending to CA3 | `POST /approve` `approved_amount=0` | **400** *"Enter the amount you are approving."*; status unchanged | 400 | PASS |
| TC-C-016 | A **negative** approved amount is refused | UC-B-26 (26.e2) | negative | high | pending to CA3 | `approved_amount=-500` | **400**, same message; status unchanged | 400 | PASS |
| TC-C-017 | A **non-numeric** approved amount is refused | UC-B-26 (26.e1) | negative | high | pending to CA3 | `approved_amount=twelve thousand` | **400**, same message; status unchanged | 400 | PASS |
| TC-C-018 | An **absent** `approved_amount` is refused | UC-B-26 (26.e1) | negative | high | pending to CA3 | POST with the field omitted | **400**, same message; status unchanged | 400 | PASS |
| TC-C-019 | A **whitespace-only** approved amount is refused | UC-B-26 (26.e1) | negative | high | pending to CA3 | `approved_amount="   "` | **400** — `ParsePaise` trims first, so spaces are the empty case; status unchanged | 400 | PASS |
| TC-C-020 | A grouped Indian amount is understood | UC-B-26 · C3 | boundary | medium | pending ₹200,000 to CA3 | `approved_amount=1,00,000` | 303; **Approved** row `₹1,00,000.00` — one lakh, not one hundred | as expected | PASS |
| TC-C-021 | One paise is a legal approved amount | UC-B-26 · C3 | boundary | medium | pending to CA3 | `approved_amount=0.01` | 303; **Approved** row `₹0.01` — the guard is `> 0`, so the smallest positive amount passes and is not rounded away | ₹0.01 | PASS |
| TC-C-022 | An approval with a missing or forged CSRF token is refused | UC-B-26 (26.e7) · CV4 | permission | high | pending to CA3 | POST with no `csrf`, then with `csrf=not-a-real-token` | **403** both times; status still *Awaiting approval* | 403, 403 | PASS |

### 1.3 Who may decide — G8 and the assigned-manager rule

| TC ID | Title | Traces to | Type | Pri | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-C-030 | A person who may approve never finds their own name in the approver list | UC-B-33 · G8, A1 | functional | critical | CA7 signed in | `GET /requests/new?type=reimbursement` | no `<option>` labelled with CA7's own name — `ListApprovers` excludes the caller (`store/requests.go:551–557`) — while other approvers are offered | absent / present | PASS |
| TC-C-031 | A hand-rolled POST naming yourself as approver is refused | UC-B-33 · G8, R6 | negative | critical | CA7 signed in | `POST /requests` with `manager_id` = own id | **400** *"you cannot approve your own request — choose another approver"* — `validateRequestInput` re-enforces what the form never offered | 400, message present | PASS |
| TC-C-032 | G8 binds an administrator holding all 66 grants | UC-B-33 · G8, R6 | negative | critical | CA8 signed in | `POST /requests` with `manager_id` = the admin's own id | **400**, same sentence — self-approval is a rule about people, not about permissions | 400, message present | PASS |
| TC-C-033 | An administrator cannot approve a request assigned to somebody else | UC-B-34 · A5, R6 | permission | critical | pending to CA3 | CA8 `POST /approve` | **403** — `before.ManagerID != actor.ID` (`store/requests.go:687`). An admin holds the verb and is not that person | 403; status unchanged | PASS |
| TC-C-034 | A Manager cannot approve a request assigned to a different manager | UC-B-34 · A5 | permission | critical | pending to CA3 | CA4 `POST /approve` | **403**; status unchanged | 403 | PASS |
| TC-C-035 | A Manager cannot return or reject another manager's request | UC-B-34 · A3, A4 | permission | critical | pending to CA3 | CA4 `POST /return`, `POST /reject` | **403** both — `decideRequest` applies the same manager test | 403, 403 | PASS |
| TC-C-036 | An Accounts user is refused approve, return and reject at the route | UC-B-26 (26.e6) · R6 | permission | critical | pending to CA3 | CA5 posts all three | **403** each — three separate gates (`approval:approve/return/reject`), none held by AC3 | 403 ×3 | PASS |
| TC-C-037 | The requester, and a role-less user, cannot approve over HTTP | UC-B-33 · R6 | permission | critical | pending to CA3 | CA1 then CA6 `POST /approve` | **403** both | 403, 403 | PASS |
| TC-C-038 | The wrong manager is offered no decision controls on the screen | UC-B-34 · A5 | information-flow | high | pending to CA3 | CA4 opens `/requests/{id}` | no approve/return/reject opener, no `.waiting.you`, and the waiting line names CA3 — the action bar gates on permission **and** on the reader's seat on the row | as expected | PASS |

### 1.4 Return and reject

| TC ID | Title | Traces to | Type | Pri | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-C-040 | Returning with no comment is refused | UC-B-27, UC-B-36 · A4 | negative | critical | pending to CA3 | `POST /return` `comment=""` | **400** *"a comment is required to return a request"*; status still *Awaiting approval* | 400, message present | PASS |
| TC-C-041 | Returning with a whitespace-only comment is refused | UC-B-36 · A4 | negative | critical | pending to CA3 | `comment="   \t  "` | **400**, same sentence — `decideRequest` trims before testing (`store/requests.go:715`) | 400 | PASS |
| TC-C-042 | A returned request goes back to the requester and becomes editable | UC-B-19, UC-B-27 · A4, L3, Q1 | functional | critical | pending to CA3 | CA3 presses **Return for correction**, fills **What needs correcting** (SV19), returns; CA1 opens the request | 303 → `/approvals`; CA1 sees `.pill.returned`, a `.banner.warn` quoting the words verbatim, and the same URL now renders the correction form with **Resubmit for approval** | as expected | PASS |
| TC-C-043 | A returned request leaves the queue and comes back on resubmission, keeping its number | UC-B-19 · Q1, L3, C4 | functional | high | returned request | read the number, `POST /edit` with `submit_action=resubmit` | the row is absent from *To approve* while returned and present after resubmission; status *Awaiting approval*; **the number is unchanged** — a return is not a new request | absent → present, number identical | PASS |
| TC-C-044 | The return reason reaches the audit trail as well as the screen | UC-B-27 · C2 | information-flow | high | pending to CA3 | return with a distinctive reason; read the thread and `/audit` | the reason is on the shared thread and in the audit summary under action `return` | present in both | PASS |
| TC-C-045 | Rejecting with no reason is refused | UC-B-28, UC-B-36 · A3 | negative | critical | pending to CA3 | `POST /reject` `reason=""` | **400** *"a reason is required to reject a request"*; status unchanged | 400, message present | PASS |
| TC-C-046 | Rejecting with a whitespace-only reason is refused | UC-B-36 · A3 | negative | critical | pending to CA3 | `reason="\n \t "` | **400**, same sentence | 400 | PASS |
| TC-C-047 | A rejected request is final and says so with the reason | UC-B-28 · A3, L4 | functional | critical | pending to CA3 | CA3 presses **Reject** (exact — the substring also matches *Reject permanently*), fills **Reason for rejection** (SV20), rejects; CA1 opens it | 303 → `/approvals`; pill *Rejected — final*; `.banner.bad` naming the approver and quoting the reason | as expected | PASS |
| TC-C-048 | A rejected request is read-only for its requester | UC-B-38 · L4, Q3 | negative | critical | rejected request | CA1: assert no Edit / Withdraw / Request-cancellation control; `GET /edit`; `POST /edit`; `POST /withdraw`; `POST /cancel-request` | no controls; `GET /edit` **400** *"A rejected request is final — raise a new one."*; all three POSTs refused | as expected | PASS |
| TC-C-049 | A rejected request refuses every approver decision too | UC-B-38 · L4, L11 | negative | critical | rejected request | CA3 posts approve, return, reject, cancel, cancellation-decide | all refused (400/403); status still *Rejected — final* | all refused | PASS |
| TC-C-050 | The rejection reason is in the audit trail under the `reject` action | UC-B-28 · C2 | information-flow | high | pending to CA3 | reject with a distinctive reason; CA8 reads `/audit` | action `reject`, the reason in the summary, entity *Payment Request* | present | PASS |
| TC-C-051 | Re-raising a rejected request creates a new one and leaves the original rejected | UC-B-21 · Q3, C4, L4 | functional | high | rejected request | `POST /reraise` | 303 to a **different** id; the copy is already `pending` with a **new** number; the original is still *Rejected — final* | as expected | PASS |
| TC-C-052 | A withdrawn request is terminal for both actors | UC-B-20, UC-B-39 · Q2, L5, L11 | negative | high | pending to CA3, withdrawn by CA1 | assert the pill; CA3 posts approve/return/reject; CA1 posts reraise | pill *Withdrawn*; all four refused; status unchanged — `withdrawn` has no outgoing edge, and re-raise is only for `rejected` | all refused | PASS |

### 1.5 Reassignment — proof of absence

| TC ID | Title | Traces to | Type | Pri | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-C-060 | A Manager holding `approval:reassign` has no route to use it | UC-B-31 · A7 (DV7/DS11) | proof-of-absence | high | pending to CA3 | CA3 `POST /requests/{id}/reassign` with `to_user_id`, `reason`, `confirm=on` | **403** — the only registered path of that shape is gated on **`reservation:reassign`** (`app.go:466`), which Manager does not hold; the **Approver** row is unchanged. `approval:reassign` is a granted verb no route consumes: **F-C-02** | 403; Approver unchanged | PASS (finding F-C-02) |
| TC-C-060B | No route of any other plausible name reaches the approval reassignment | UC-B-31 (31.e2) · A7, CV1 | proof-of-absence | high | pending to CA3 | CA3 **and** CA8 each POST `/requests/{id}/approval-reassign`, `/reassign-approver`, `/change-approver`, `/approvals/{id}/reassign` | **405** (accepting 404) for all eight probes — proof of absence has to rule out the paths a reader would guess at, not only the one the spec named; the **Approver** row is untouched throughout | 405 ×8; Approver unchanged | PASS (finding F-C-02) |
| TC-C-061 | The reservation reassign route cannot move an approver either | UC-B-31 (31.e1) · A7 | proof-of-absence | high | pending to CA3 | CA8 (who does hold `reservation:reassign`) posts: no target; no `confirm`; then `to_user_id`=CA4 with `confirm=on` | **400** *"Choose who should take this reservation."*; **400** *"Confirm that no payment has been initiated."*; **400** *"That person cannot work the Accounts queue."* — every branch is about a reservation. Approver unchanged | as expected | PASS |
| TC-C-062 | No screen offers an approver a way to hand the decision on | UC-B-31 · A7 | proof-of-absence | medium | pending to CA3 | CA3 opens `/requests/{id}` | the bar offers Approve, Return for correction and Reject and nothing else; no `reassign` link or form anywhere on the page | as expected | PASS |
| TC-C-063 | The only rerouting the product ships is the requester's own edit | UC-B-18.a, UC-B-31 · A7, A8 | functional | high | pending to CA3 | CA1 `POST /edit` with `submit_action=save` and `manager_id`=CA4 | 303; the **Approver** row on the detail reads CA4 | as expected | PASS |

### 1.6 No bulk approval (A6)

| TC ID | Title | Traces to | Type | Pri | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-C-070 | There is no bulk-approve route | UC-B-35 · A6 | proof-of-absence | high | CA3 signed in | POST to `/approvals/bulk-approve`, `/approvals/approve-all`, `/requests/bulk-approve`, `/requests/approve`, `/approvals` | **405** (accepting 404) for each — `GET /` is a registered catch-all, so the path matches and the method does not (CV1) | 405 ×5 | PASS |
| TC-C-071 | The queue offers no selection and says why | UC-B-35 · A6 | proof-of-absence | medium | ≥1 pending to CA3 | CA3 opens `/approvals`; inspect `main.page` | no checkbox; no button whose name matches `/approve/i`; the screen states *"There is no bulk approval, by design"* | as expected | PASS |

### 1.7 The cancellation flow — five routes, two actors

The five routes and who owns them (CV2):

| ID | Route | Gate | Owner | Additional handler test |
|---|---|---|---|---|
| CR1 | `GET /requests/{id}/cancel` | `request:cancel` | requester (AC1) | must be the row's requester; status must be `approved` |
| CR2 | `POST /requests/{id}/cancel-request` | `request:cancel` | requester (AC1) | must be the row's requester; `approved → cancellation_requested` |
| CR3 | `POST /requests/{id}/cancel` | `approval:cancel` | approver (AC2) | must be the row's `manager_id` |
| CR4 | `GET /requests/{id}/cancellation` | `approval:cancel` | approver (AC2) | must be the row's `manager_id`; **no status test** |
| CR5 | `POST /requests/{id}/cancellation` | `approval:cancel` | approver (AC2) | must be the row's `manager_id`; status must be `cancellation_requested` |

| TC ID | Title | Traces to | Type | Pri | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-C-080 | The requester's cancel routes are refused to the approver | UC-B-22 · G1, R6 | permission | critical | approved request to CA3 | CA3 hits CR1 and CR2 | **403** both — Manager holds `approval:cancel`, never `request:cancel` | 403 ×2 | PASS |
| TC-C-081 | The approver's cancellation routes are refused to the requester | UC-B-29, UC-B-30 · G1, G2, R6 | permission | critical | approved request | CA1 hits CR3, CR4, CR5 | **403** each — Requester holds `request:cancel`, never `approval:cancel` | 403 ×3 | PASS |
| TC-C-082 | An Accounts user is refused all five | UC-B-29, UC-B-30 · R6 | permission | high | approved request | CA5 hits CR1–CR5 | **403** ×5 — AC3 holds neither cancel verb | 403 ×5 | PASS |
| TC-C-083 | A role-less user is refused all five | R6 | permission | high | approved request | CA6 hits CR1–CR5 | **403** ×5 | 403 ×5 | PASS |
| TC-C-084 | An anonymous caller is redirected off all five | R6, CV1 | permission | high | approved request | no cookies; GET and POST as appropriate | 3xx → `/login` for each — `RequirePermission` wraps `RequireLogin`, which fires before `withCSRF` is ever consulted | 303 → `/login` ×5 | PASS |
| TC-C-085 | Holding `request:cancel` is not permission to cancel somebody else's request | UC-B-42 · Q5, R3 | permission | critical | approved request raised by CA1 | CA2 hits CR1 then CR2 | **403** on the screen (data scope `own` does not reach the row) and **403** on the POST (`RequestCancellation` re-checks the requester); the request is untouched | 403, 403 | PASS |
| TC-C-086 | Only the approver the request was sent to may decide its cancellation | UC-B-34, UC-B-30 · G1 | permission | critical | cancellation pending on CA3's request | CA4 hits CR4, CR5, CR3 | **403** each; the screen says *"Only the approver this request was sent to can decide its cancellation"*; status still *Cancellation requested* | as expected | PASS |
| TC-C-087 | The requester asks, and payment freezes at that moment | UC-B-22 · G1, G3, C2 | functional | critical | approved request | CA1 follows **Request cancellation**, fills **Reason** (SV25), sends | the ask screen names *"approved by &lt;approver&gt; on &lt;date&gt;"* — where `approved_at` surfaces to the requester; after sending, `.pill.cancelreq`, a `.banner.warn` *"Payment is frozen"* quoting the reason, and an audit row `cancel_request` | as expected | PASS |
| TC-C-088 | A cancellation ask with an empty or whitespace-only reason is refused | UC-B-22 · G1 | negative | high | approved request | CR2 with `""` then `"    "` | **400** *"say why it should be cancelled"* both; status still *Approved — awaiting payment* | 400, 400 | PASS |
| TC-C-089 | The ask screen is offered only for an approved request | UC-B-22, UC-B-37 · L11 | negative | high | one pending, one rejected | CA1 hits CR1 on each | **400** on the pending one with *"withdraw it instead"*; **400** on the rejected one — the screen is never rendered to somebody whose submit would bounce | 400, 400 | PASS |
| TC-C-090 | The approver's decision screen shows the ask and offers both answers | UC-B-30 · G1 | functional | high | cancellation pending | CA3 opens CR4 | a card *"Why &lt;requester&gt; wants it cancelled"* quoting the reason; both **Decline — keep it live** and **Cancel the request** visible | as expected | PASS |
| TC-C-091 | Declining a cancellation demands a written reason | UC-B-30 · G1 | negative | critical | cancellation pending | CR5 `decision=decline` with `""` then `" \t "` | **400** *"say why it should still be paid"* both; the request stays frozen | 400, 400 | PASS |
| TC-C-092 | Declining unfreezes the request and is recorded | UC-B-30 · G1, L6, C2 | functional | critical | cancellation pending | CA3 opens CR4, presses **Decline — keep it live**, fills **Why it should still be paid** (SV26), confirms | 303 → `/approvals?bucket=cancellations`; CA1 sees *Approved — awaiting payment*, `.banner.locked` back, **no** `.banner.warn`; audit action `cancel_decline` carrying the reason | as expected | PASS |
| TC-C-093 | Accepting closes the request permanently, and the note is optional | UC-B-30 · G1, G3 | functional | critical | cancellation pending | CR5 `decision=accept`, `note=""` | 303; status *Cancelled*; the waiting line reads *"Cancelled. Nothing can be paid against it"*; audit summary *"at the requester's asking"* — accepting lands under the same `cancel` action as an outright cancel (BR-30.1), and only declining requires words | as expected | PASS |
| TC-C-094 | The approver may cancel an approved request outright, with a reason | UC-B-29 · G2, C2 | functional | critical | approved request | CA3 follows **Cancel with reason** → CR4 → the `#outright-sheet` → fills **Reason** → **Cancel request** | status *Cancelled*; the reason is in the audit trail and on the requester's own history | as expected | PASS |
| TC-C-095 | An outright cancellation with no reason is refused | UC-B-29 · G2 | negative | high | approved request | CR3 `reason="   "` | **400** *"a reason is required to cancel a request"*; status unchanged | 400 | PASS |
| TC-C-096 | An outright cancellation is also legal while a cancellation is pending | UC-B-29 · G2, L11 | boundary | medium | cancellation pending | CR3 with a reason | Accepted (303) and the request is *Cancelled* — `legalTransitions` carries `cancellation_requested → cancelled` and `CancelRequest` does not additionally insist on `approved`, so this route reaches the same end as accepting | 303; *Cancelled* | PASS |
| TC-C-097 | **A hold placed before a cancellation ask is dropped, and declining returns the request with no hold** | UC-B-22, UC-B-30 · L7, G1, C2 | functional | critical | approved request | CA5 `POST /hold` with a reason → CA1 CR2 → CA3 CR5 `decline` | after the hold: pill *On hold* and a banner quoting the question. After the ask: pill *Cancellation requested* and **no** *On hold* pill — `on_hold=1` implies `status='approved'`, so every exit from approved clears it. After the decline: *Approved — awaiting payment*, **no** *On hold* pill, no hold banner, `.banner.locked` back. **The hold event and its reason survive** on the thread and in the audit trail under action `hold` | exactly as expected — code and PROGRESS.md agree | PASS |
| TC-C-098 | After that decline the request is **immediately re-reservable** | UC-B-30 · L7, L8 | functional | critical | hold → ask → decline, as TC-C-097 | CA5 opens `/accounts-queue?tab=approved&q=<number>`, then actually `POST /requests/{id}/record-payment` | the row is present **and** carries **Take for processing**; and the reservation itself succeeds — `ReserveRequest`'s conditional UPDATE requires `on_hold=0`, so a successful reserve is the only proof the hold is genuinely gone rather than merely unrendered. Status becomes *With Accounts*, ready to pay, with the accountant's question never answered | present, takeable, reserved (303 → *With Accounts*) | PASS (finding F-C-07) |
| TC-C-097B | **Nobody is told when a cancellation is decided, and the accountant loses their hold in silence** | UC-B-30 · L7, N-events, UC-B DS10 | information-flow | critical | approved request | CA5 holds it with a question; count CA5's notices for this number; CA1 asks to cancel; recount; CA3 declines; recount both CA5's and CA1's | **Control:** `request_cancellation_requested` carries `IncludeAccounts` (`migrations_notifications.go:51–52`), so the ask **must** raise CA5's count — without that the silence proves nothing. **Then:** the decline adds nothing for CA5 and nothing for CA1 — there is no event for either cancellation decision at all (`notify/events.go:9–21` stops at `EventCancellationRequested`) and neither decision handler calls `a.fire` (`app/requests.go:850–867`). The hold banner and pill are gone from every screen; the reason survives only in the audit trail | control +1 for CA5; decline +0 for CA5 and +0 for CA1; banner and pill absent; reason present only in `/audit` | PASS (finding F-C-07) |
| TC-C-099 | A hold cannot be placed while a cancellation is pending | UC-B-22 · L7, L11 | negative | high | cancellation pending | CA5 `POST /hold` | **403** — `HoldRequest`'s conditional UPDATE requires `status='approved' AND on_hold=0` and matches no row; status still *Cancellation requested* and no hold on it. (The store's sentence is replaced by the generic `ErrForbidden` message, CV5, so the observable truth is the status and the unchanged state) | 403; unchanged | PASS |

### 1.8 Legal transitions, exhaustively

The grid is built from the guard code, not from a document: `legalTransitions`
(`internal/store/requests.go:91–101`) plus each writer's own status test. Nine actions × eight
from-states; each test builds one request genuinely in that state and fires **every illegal action** at
it over its real HTTP route, then re-reads the status pill.

| ID | Action | Route | Actor | Legal from |
|---|---|---|---|---|
| AX1 | approve | `POST /requests/{id}/approve` | CA3 | `pending` **(and `cancellation_requested` — see F-C-03)** |
| AX2 | return | `POST /requests/{id}/return` | CA3 | `pending` |
| AX3 | reject | `POST /requests/{id}/reject` | CA3 | `pending` |
| AX4 | withdraw | `POST /requests/{id}/withdraw` | CA1 | `pending` |
| AX5 | resubmit | `POST /requests/{id}/edit` (`submit_action=resubmit`) | CA1 | `returned` |
| AX6 | cancel-request | `POST /requests/{id}/cancel-request` | CA1 | `approved` |
| AX7 | cancel-outright | `POST /requests/{id}/cancel` | CA3 | `approved`, `cancellation_requested` |
| AX8 | decide-cancellation | `POST /requests/{id}/cancellation` | CA3 | `cancellation_requested` |
| AX9 | reraise | `POST /requests/{id}/reraise` | CA1 | `rejected` |

| TC ID | Title | Traces to | Type | Pri | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-C-110 | Every illegal action against a **pending** request is refused | UC-B-*, L2, L11 | negative | critical | pending to CA3 | fire AX5, AX6, AX7, AX8, AX9 | all refused (400/403); status still *Awaiting approval* | 5 refusals | PASS |
| TC-C-111 | Every illegal action against a **returned** request is refused | L3, L11 | negative | critical | returned | fire AX1–AX4, AX6–AX9 | all refused; status still *Returned for correction* | 8 refusals | PASS |
| TC-C-112 | Every illegal action against an **approved** request is refused | L6, L11 | negative | critical | approved | fire AX1–AX5, AX8, AX9 | all refused; status still *Approved — awaiting payment* | 7 refusals | PASS |
| TC-C-113 | Every illegal action against a **cancellation_requested** request is refused | G1, G3, L11 | negative | critical | cancellation pending | fire AX2–AX6, AX9 (AX1 is excluded and owned by TC-C-113B) | all refused; status still *Cancellation requested* | 6 refusals | PASS |
| TC-C-113B | **Approving a request whose cancellation is undecided must be refused** | UC-B-26 (26.e5 / precondition 1), UC-B-30 · G1, L11 | negative | critical | cancellation pending | CA3 `POST /approve` `approved_amount=9999` | **Required:** refusal; status held at *Cancellation requested*; any unfreeze recorded as `cancel_decline` with a written reason; and the requester still able to see what became of the cancellation they asked for. **Actual:** accepted, and all four requirements broken — see §2.4 | 303 → `/approvals`; status became *Approved — awaiting payment*; audit has no `cancel_decline`; `approved_amount` rewritten to ₹9,999.00 and `approved_at` re-stamped; `cancel_reason` left stale on the approved row and rendered nowhere, so the requester's screen shows a plain approved request | **FAIL (product defect)** — F-C-03, `test.fail()` |
| TC-C-114 | Every illegal action against a **rejected** request is refused | L4, L11 | negative | critical | rejected | fire all nine | all refused; status still *Rejected — final* | 9 refusals | PASS |
| TC-C-115 | Every illegal action against a **withdrawn** request is refused | L5, L11 | negative | critical | withdrawn | fire all nine | all refused; status still *Withdrawn* | 9 refusals | PASS |
| TC-C-116 | Every illegal action against a **cancelled** request is refused | G3, L11 | negative | critical | cancelled outright | fire all nine | all refused; status still *Cancelled* | 9 refusals | PASS |
| TC-C-117 | A **completed** request is terminal: nothing in the grid moves it | L10, L11 | negative | critical | approved, then reserved and settled in full by CA5 (`fixtures.settlePayment`) | fire all nine | pill *Completed*; all nine refused; still *Completed* | 9 refusals | PASS |
| TC-C-119A | Every action in the grid is refused against a **processing** request | L8, L11 | negative | critical | approved, then CA5 `POST /record-payment` (one POST: `ReserveRequest`) | fire all nine | pill *With Accounts*; all nine refused; still *With Accounts* — `processing` has no outgoing edge in `legalTransitions` at all | 9 refusals | PASS |
| TC-C-119B | Every action in the grid is refused against a **partial_review** request | L9, L11 | negative | critical | approved ₹18,400, then CA5 settles ₹10,000 as a partial with a reason | fire all nine | pill *Partial — manager review*; all nine refused; unchanged | 9 refusals | PASS |
| TC-C-119C | Every action in the grid is refused against a **completed_partial** request, and accept-partial never writes `completed` | L11, G14 | negative | critical | partial_review, then CA3 `POST /accept-partial` | the accept yields **`completed_partial`** (*Completed — partial accepted*), not `completed` — `AcceptPartial` writes only that state (`store.go:983`), so the `partial_review → completed` edge in `legalTransitions` (`requests.go:100`) has **no writer**. Then all nine refused; unchanged | *Completed — partial accepted*; 9 refusals | PASS (finding F-C-08) |
| TC-C-118 | The cancellation screen degrades rather than lying on a closed request | UC-B-29, UC-B-30 | information-flow | medium | rejected, withdrawn and cancelled requests | CA3 opens CR4 on each | 200 with only **Back to the request** — `requestCancellationForm` carries no status guard, so it must offer no decision at all: no `#accept-sheet`, no `#decline-sheet`, no `#outright-sheet` | as expected ×3 | PASS |

### 1.9 Edit-while-pending rerouting, from the approver's side

| TC ID | Title | Traces to | Type | Pri | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-C-120 | After a reroute the original approver cannot decide and the new one can | UC-B-18.a · A8, A5 | functional | critical | pending to CA3 | CA1 opens `/requests/{id}/edit`, picks CA4 in **Approver**, presses `/^Save and notify/` (SV23); then CA3 and CA4 each `POST /approve` | CA3's queue drops to 0 cards, CA4's rises to 1; CA3 is **403**; CA4 succeeds (303) and the request is *Approved — awaiting payment* | as expected | PASS |
| TC-C-121 | The reroute is written into the history as an approver change | UC-B-18.a · A8, C2 | information-flow | high | pending to CA3 | reroute by edit; read the thread and `/audit` | `.thread .tl-change` reads **Approver** (`threadField("manager_id")`); the audit row reads *Updated* with summary *"… edited request …"* | as expected | PASS |
| TC-C-122 | The two approvers see two different screens for the same request | UC-B-18.a · A8, A5 | information-flow | high | request rerouted CA3 → CA4 | both open `/requests/{id}` | CA3: no `.waiting.you`, no approve opener, and the waiting line names CA4. CA4: `.waiting.you` visible and exactly one approve opener | as expected | PASS |
| TC-C-123 | **A refused resubmission must not leave the edit applied** | UC-B-18, UC-B-19 (DS6) · A8 | negative | high | pending to CA3 | CA1 `POST /edit` with `submit_action=resubmit`, a changed title and `manager_id`=CA4 | **Required:** 400 *"a pending request cannot be submitted"* **and nothing written** — a refusal must not apply half its work. **Actual:** the 400 is correct and the edit was committed anyway | 400 with the right message; **Approver** had moved to CA4 and the title had changed. `requestEdit` commits `UpdateRequest` and only then calls `SubmitRequest` (`app/requests.go:658–665`) | **FAIL (product defect)** — F-C-04, `test.fail()` |

### 1.10 Concurrency

| TC ID | Title | Traces to | Type | Pri | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|---|---|---|---|
| TC-C-130 | Two sessions of the approver approving at once: exactly one wins | UC-B-26 · A2, L11 | concurrency | critical | pending to CA3; CA3 signed in twice | both sessions `POST /approve` simultaneously with different amounts | exactly one 303; the request is approved **once** — one status, one `approve` audit row, never two | 1 win; one audit row; *Approved — awaiting payment* | PASS |
| TC-C-131 | Approving and rejecting at once: the request lands in exactly one state | UC-B-26, UC-B-28 · L11 | concurrency | critical | pending to CA3; CA3 signed in twice | one session approves, the other rejects, simultaneously | exactly one 303; the final status is *Approved — awaiting payment* **or** *Rejected — final*, never a mixture | 1 win; one decided state | PASS |
| TC-C-132 | Two different managers racing: authorisation does not depend on timing | UC-B-34 · A5 | concurrency | high | pending to CA3 | CA3 and CA4 `POST /approve` simultaneously | the assigned approver wins (303) whatever the interleaving; CA4 is **403** on authorisation rather than on the race; the approved amount is the assigned approver's | 303 / 403; ₹18,400.00 | PASS |
| TC-C-133 | Accepting and declining a cancellation at once resolves to one answer | UC-B-30 · G1, L11 | concurrency | high | cancellation pending; CA3 signed in twice | one accepts, the other declines, simultaneously | exactly one 303; the request is *Cancelled* **or** *Approved — awaiting payment*, never both | 1 win; one state | PASS |
| TC-C-134 | **The loser of a decision race must be refused, not handed a 500** | UC-B-26, UC-B-28, UC-B-30 · L11 | concurrency | high | three races: approve/approve, approve/reject, accept/decline | fire each pair simultaneously and read the loser's status | **Required:** the loser is told no — a 4xx refusal it can act on. **Actual:** **500** every time, `database is locked (5) (SQLITE_BUSY)` in the server log, after ~4 ms | 500 ×3 | **FAIL (product defect)** — F-C-05, `test.fail()` |

---

## 2. Detail for the cases that need it

### 2.1 TC-C-005 — why "the real filter" is the interesting question

`approvals` builds every tab with `Scope: "assigned"` hard-coded (`internal/app/requests.go:896`), and the
comment above it states the intent: *"holding `approval:approve` says you may decide, and the `manager_id`
on the row says which requests are yours to decide."* A Manager's **data** scope is `all`, so the same
person can read every request in the system at `/requests` and at `/requests/{id}`. The test asserts both
halves — 0 cards in the queue **and** 200 on the other manager's request — because asserting only the
first would be satisfied by a missing grant, and only the second by a queue with no filter at all.

### 2.2 TC-C-014 and F-C-01 — what "may adjust" leaves open

Coverage row **A2** reads *"Approve; may adjust amount"*. The approve sheet's hint reads *"You may approve
a smaller amount than was asked for."* `ApproveRequest` enforces only `approvedAmount > 0`. So the
expectation written before the run — derived from the **code**, as the brief requires — was that an
over-approval commits, and it does. What makes it a finding rather than a curiosity is
`approvedOf(req)` (`internal/app/linking.go:816`): the approved amount **is** the ceiling
`RecordPaymentForRequest` measures a payment against under G13. Raising it raises what Accounts may pay,
with no second signature and nothing on the requester's screen calling attention to it.

### 2.3 TC-C-097 — the hold interaction, verified line by line

The brief asked for this one specifically, and the code and `docs/superpowers/PROGRESS.md:358–361` agree
with each other and with the observed behaviour:

1. `HoldRequest` sets `on_hold=1` only `WHERE … status='approved' AND on_hold=0` (`store.go:1047`).
2. `RequestCancellation` writes `status='cancellation_requested', on_hold=0, hold_reason=''` in one
   statement (`store/requests.go:889`), with an eight-line comment explaining that a hold left set would
   outlive its own status.
3. `DecideCancellation` clears the hold on **both** branches (`store/requests.go:941`) — so declining
   returns the request to `approved` with no hold.
4. The reason is not lost: the `hold` audit row and the before/after snapshots are untouched, and the
   thread renders the hold event with tone `warn` and glyph `⏸`.

TC-C-098 is the second half of the proof, and it is where the mechanism stops being neutral. An invariant
about a column is only interesting if it changes what a person can do, so the test goes past the rendered
queue and actually reserves the request: `ReserveRequest`'s conditional UPDATE requires `on_hold=0`, so a
successful reservation is the only proof the hold is genuinely gone. It succeeds. Coverage row **L7** reads
*"On hold (only Accounts lifts)"* — and here two people who are not Accounts have lifted it between them:
the requester by asking, the approver by declining. Nothing restores it on the way back to `approved`, and
TC-C-097B shows nobody is told. That gap is **F-C-07**; steps 1–4 above remain correct and documented, and
are not a defect in themselves.

### 2.4 TC-C-113B and F-C-03 — the edge that lets an approval overrule a cancellation

`legalTransitions` gives `cancellation_requested` two outgoing edges: `cancelled` and `approved`
(`store/requests.go:97`). The second exists so `DecideCancellation(accept=false)` can decline. But
`ApproveRequest` tests the same edge with the same helper (`canTransition(before.Status, "approved")`,
`store/requests.go:695`), so `POST /requests/{id}/approve` also passes it. UC-B-26's precondition 1 —
*"Status is `pending` — the only status with a legal edge to `approved`"* — is therefore incorrect, and so
is 26.e5 for this one status.

### 2.5 TC-C-134 and F-C-05 — why `busy_timeout` does not save the decision writers

Every decision writer in `internal/store/requests.go` follows the same shape: `BeginTx` → `requestInTx`
(a read) → `UPDATE` → `recordAuditTx` → `Commit`. Two of them running at once both take a read lock and
then both need to upgrade to a write lock; SQLite does not invoke the busy handler for an upgrade that can
never succeed, so the loser gets `SQLITE_BUSY` immediately — which is why the failure lands in 4 ms
despite `busy_timeout(5000)` on the DSN (`store.go:36`). `ReserveRequest` is the shape that is safe
(`store.go:736`): a single conditional `UPDATE … WHERE status='approved' AND …`, no prior read, so the
loser simply matches no rows and is told the request is unavailable.

The good news, asserted separately in TC-C-130/131/133 so it cannot be lost behind the annotated test:
the transaction rolls back cleanly. There is **no** double-approval and **no** mixed state.

### 2.6 TC-C-119A/B/C — building the grid on the statuses that actually exist

The Phase-2 enum lists seven statuses and the overview spec's transition table describes nine; the shipped
product has **eleven**. Three of them are only reachable through Accounts' side of the flow, so they are
reached here with the cheapest legitimate path and then interrogated for legality alone:

| Status | Reached by | Outgoing edges in `legalTransitions` |
|---|---|---|
| `processing` | `POST /requests/{id}/record-payment` — one POST, `ReserveRequest` | **none** |
| `partial_review` | a partial settlement through `fixtures.settlePayment` | `completed`, `completed_partial` |
| `completed_partial` | `POST /requests/{id}/accept-partial` by the row's manager | **none** — terminal, like `completed` |

Two things fall out of it, both reported as **F-C-08**. `AcceptPartial` writes only `completed_partial`
(`store.go:983`), and `RecordPaymentForRequest` writes `completed` only from `processing`
(`store.go:925`) — so `legalTransitions["partial_review"]["completed"]` (`requests.go:100`) has **no
writer at all**. And `draft`, which coverage row **L1** still names as a state, cannot be attempted from
any route: `CHECK (status <> 'draft')` makes it unrepresentable for the life of the table
(`migrations.go:156`).

---

## 3. Traceability

### 3.1 Coverage-matrix IDs exercised

| Coverage ID | Requirement | Test cases |
|---|---|---|
| A2 | Approve; may adjust amount | TC-C-010, 011, 012, 014, 014B, 015–021, 130, 132 |
| A3 | Reject with required reason | TC-C-035, 045, 046, 047, 050, 131 |
| A4 | Return with required comments | TC-C-035, 040, 041, 042, 044 |
| A5 | Managers see all; approve assigned | TC-C-002…005, 033, 034, 038, 120, 122, 132 |
| A6 | No bulk approval | TC-C-070, 071 |
| A7 | Admin reassign (reason + history) | TC-C-060, 060B, 061, 062, 063 — **absence only**; the matrix marks A7 verified by a store-level Go test that never touches HTTP |
| A8 | Edit-while-pending: reroute | TC-C-063, 120, 121, 122, 123 |
| Q1 | Edit & resubmit sent-back | TC-C-042, 043 |
| Q2 | Withdraw pending | TC-C-052 |
| Q3 | Re-raise rejected | TC-C-048, 051 |
| Q4 | See payment outcome | TC-C-013 |
| Q5 | Requester sees only own | TC-C-085 |
| L1 | Draft (private) | **not testable** — `CHECK (status <> 'draft')` makes it unrepresentable (F-C-08) |
| L2 | Pending approval | TC-C-110 |
| L3 | Returned | TC-C-042, 043, 111 |
| L4 | Rejected — final, read-only | TC-C-047, 048, 049, 114 |
| L5 | Withdrawn | TC-C-052, 115 |
| L6 | Approved | TC-C-010, 013, 092, 112 |
| L7 | On hold (**only Accounts lifts** — violated, F-C-07) | TC-C-097, 097B, 098, 099 |
| L8 | Processing (reserved) | TC-C-098, 119A |
| L9 | Partial — manager review | TC-C-119B (legality only) |
| L10 | Completed; drops out of the link list | TC-C-014B, 117 |
| L11a | `completed_partial` is terminal and distinct (G14) | TC-C-119C |
| L11 | Legal transition enforcement | TC-C-110…119C, 130, 131, 133, 134 |
| C2 | Audit history for request mutations | TC-C-012, 044, 050, 087, 092, 093, 094, 121 |
| C3 | Currency INR paise throughout | TC-C-012, 020, 021 |
| C4 | Request number unique, monotonic | TC-C-043, 051 |
| R3 | Data scope per resource | TC-C-005, 085 |
| R6 | Permissions govern data server-side (URL blocked) | TC-C-001…004, 022, 031…037, 080…085 |
| G1 | Post-approval cancellation | TC-C-080…099, 113, 133 |
| G2 | Manager cancels outright with a reason | TC-C-094, 095, 096 |
| G3 | Statuses `cancellation_requested`, `cancelled` | TC-C-087, 093, 116 |
| G8 | Self-approval blocked | TC-C-030, 031, 032, 037 |

### 3.2 Coverage IDs deliberately **not** exercised here

| Coverage ID | Why, and who owns it |
|---|---|
| T1–T12, A1 | Raising a request and per-type validation — UC-B-01…UC-B-15, another agent's suite |
| S1–S15, V1–V8 | Reservation, payment, settlement, partial review, recoverables — UC-C-*. Reached here only as states whose *legality* this suite owns (TC-C-014B, 098, 117, 119A/B/C); nothing about the payment screens is asserted |
| N1–N8 | Notifications and reminders — UC-C-*. The single exception is TC-C-097B, which counts in-app notices only to establish that a **cancellation decision** raises none; the templates, the email path and the reminder scheduler are untouched here |
| R1, R2, R4, R5, R7–R9 | The RBAC administration surface — UC-A |
| Q6 | Commenting while on hold — touched only as far as the hold's own lifecycle |
| A7 (positive path) | `store.ReassignRequest` has no HTTP caller, so its four store-level rules — pending-only, `newManagerID > 0`, G8-for-reassignment, reason required — are **NOT RUN** from a browser. They are covered at unit level by `internal/store/requests_test.go:815–819` |

### 3.3 Verdict summary

| Verdict | Count | Test cases |
|---|---|---|
| PASS | 91 | everything not listed below |
| FAIL (product defect) | 3 | TC-C-113B (F-C-03) · TC-C-123 (F-C-04) · TC-C-134 (F-C-05) — each `test.fail()`-annotated, each verified with the JSON reporter to be genuinely failing |
| FAIL (test defect — fixed) | 0 | four were found and fixed during the pass, all before the recorded run: **(1)** TC-C-121 matched the stored action `update` where the screen renders `actionText` = *Updated*; **(2)** TC-C-063/121/122 used `submit_action=resubmit` to reroute a **pending** request, which is itself refused — that discovery became TC-C-123 and F-C-04; **(3)** TC-C-099 asserted the store's sentence where `respondStoreError` substitutes the generic `ErrForbidden` message; **(4)** TC-C-097B asserted the requester had *zero* notices for the request, when they correctly hold two (approved, on-hold) — the honest assertion is that the decline adds none |
| BLOCKED | 0 | |
| NOT RUN | 0 executed cases | two things cannot be driven from a browser and are recorded rather than dropped: the **positive path of A7** (§3.2) and the **`draft` status** (coverage L1), which `CHECK (status <> 'draft')` makes unrepresentable, so no route can attempt it. Both are reported as findings (F-C-02, F-C-08) rather than as gaps in this suite |

**Total 94 executed cases.** `94 passed (10.9s)`, 0 failed.
