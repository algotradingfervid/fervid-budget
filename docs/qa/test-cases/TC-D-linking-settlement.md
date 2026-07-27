# TC-D — Payment linking, reservation and settlement

Executable test cases for everything a request does between **`approved`** and
**`completed`**: the two reservation entry points, the atomicity of the
reservation, release and reassign, the hold, the stale nudge, the pure
settlement preview, the settlement outcomes, the immutability of the payment,
and the features the design forbids.

**Out of scope, owned by siblings.** Raising a request and the approval decision
(`TC-B`), platform/RBAC administration (`TC-A`), recoverables arithmetic and
notification delivery (`TC-C`). Where a case below touches one of those it does
so only as a precondition.

| | |
|---|---|
| **Spec** | `tests/e2e/audit-d-linking-settlement.spec.ts` |
| **Use cases** | `docs/qa/use-cases/UC-C-settlement-recoverables-notifications.md` (`UC-C-01`–`UC-C-29`, `UC-C-37`) |
| **Coverage matrix** | `docs/superpowers/specs/2026-07-25-payment-requests-coverage.md` |
| **Findings** | `docs/qa/results/findings-d-linking-settlement.md` |
| **Run** | `FERVID_E2E_PORT=4304 npx playwright test tests/e2e/audit-d-linking-settlement.spec.ts --project=chromium --reporter=line` |
| **Mobile run** | the same with `--project=mobile-chrome` (only `TC-D-092`/`TC-D-093` execute there; every other case is skipped by design) |

---

## 1. How to read this document

### 1.1 Conventions

| ID | Convention |
|---|---|
| CD1 | **99 test cases, 77 Playwright tests.** Where one test covers several cases its title carries every ID (`TC-D-026/027/028`). Each case is still a row here with its own expected result, because each is a separate claim about the product. |
| CD2 | **Every expected result was derived from the Go code and the specs before the spec file was run**, and is written as what the product *should* do — not as what it does. Eleven cases fail on purpose; each is annotated `test.fail()` in the spec and carries a finding ID. Each annotated test was additionally run **without** its annotation, so the recorded failure is the one the case is about rather than an earlier assertion. |
| CD3 | A gated route refuses an **anonymous** caller with **303 → /login** and an **authenticated but unauthorised** one with **403**. `withCSRF` answers **403** to a missing or forged token. An **unrouted POST answers 405**, not 404. |
| CD4 | `requestStatusText` (`internal/app/requests.go:988`) spells `processing` as **"With Accounts"**. Assertions read that sentence, never the enum. |
| CD5 | Money is `int64` paise; `money.FormatPaise` already prints `₹`. The entry form's amount field is filled by `amountValue`, so it shows `4,200.00` and the read-only approved field shows `₹4,200.00`. |
| CD6 | A refusal that reaches the store is rendered by `respondStoreError` (error page) or `settlementError` (the confirmation sheet, re-carrying every typed value). `ErrValidation` keeps the store's sentence; `ErrForbidden` is flattened to a generic one (finding F-D-02), so cases about a *state* conflict assert the status and the state, not the words. |
| CD7 | **Database state is asserted through the app's own reading of it**: `processing_by` through the queue's `.pill.processing`, the reservation screen's `.reserve-bar`, and the `audit_log` rows at `GET /audit?entity=payment_request&id={id}&action=process` — which is written inside the same transaction as the reservation UPDATE. No test opens the SQLite file. |
| CD8 | Types: `functional` · `permission` · `negative` · `boundary` · `concurrency` · `information-flow` · `regression` · `proof-of-absence` · `accessibility`. |
| CD9 | Verdicts: `PASS` · `FAIL (product defect)` · `FAIL (test defect — fixed)` · `BLOCKED` · `NOT RUN`. A `FAIL (product defect)` row is green in CI because the test is annotated `test.fail()`; the assertion inside is unweakened, so fixing the defect turns the test red with "passed unexpectedly". |

### 1.2 The shared corpus

Four approved requests, built once and reused by every read-only case, because
each costs a dozen navigations through the real screens (G8 means a second
signed-in person must approve every one):

| Fixture | Shape | Used for |
|---|---|---|
| `open` | approved · unclaimed · payee `Acme Supplies {runId}` · ₹7,431.00 | the takeable row, every search facet |
| `onHold` | approved · `on_hold=1` with a reason | L7 |
| `taken` | `processing`, reserved by a single-role Accounts subject | every non-holder probe |
| `done` | settled in full, therefore `completed` | L10, X3, X6 |

Plus four signed-in subjects holding **exactly** one role each (`Accounts`,
`Manager`, `Requester`, and none at all) via `asRole`, which unticks every box
before ticking the one asked for — a fresh user otherwise silently holds
**Accounts** (`internal/store/migrations.go:501`).

---

## 2. Index

| TC | Title | Traces to | Type | Priority | Verdict |
|---|---|---|---|---|---|
| TC-D-001 | The queue reserves with a submit button in a POST form, never a link | UC-C-01, UC-C-03 · S1 | functional | high | PASS |
| TC-D-002 | Entry point one: the queue hands the reservation to the entry screen | UC-C-03 · S1, S2, L8 | functional | critical | PASS |
| TC-D-003 | Entry point two: the payment picker reserves the same way | UC-C-04 · S1, S3 | functional | critical | PASS |
| TC-D-004 | Only approved, unclaimed, not-on-hold requests are takeable | UC-C-04 · S3 | functional | critical | PASS |
| TC-D-005 | An on-hold request is read-only in the picker and states its reason | UC-C-04, UC-C-16 · S3, L7 | information-flow | high | PASS |
| TC-D-006 | A reserved request names its holder and offers no way to take it | UC-C-04, UC-C-05 · S3 | information-flow | high | PASS |
| TC-D-007 | A completed request drops out of the linkable list | UC-C-04 · L10, S9 | functional | high | PASS |
| TC-D-008 | The picker search finds a request by number | UC-C-04 · S4 | functional | medium | PASS |
| TC-D-009 | The picker search finds a request by requester | UC-C-04 · S4 | functional | medium | PASS |
| TC-D-010 | The picker search finds a request by payee | UC-C-04 · S4 | functional | high | PASS |
| TC-D-011 | The picker search finds a request by project | UC-C-04 · S4 | functional | medium | PASS |
| TC-D-012 | The picker search finds a request by head, and excludes another head | UC-C-04 · S4 | functional | medium | PASS |
| TC-D-013 | The amount search matches the amount a person can see | UC-C-02, UC-C-04 · S4 | functional | medium | **FAIL (product defect)** — F-D-04 |
| TC-D-014 | The queue search narrows the rows without moving the counts | UC-C-02 · S4, D4 | functional | medium | PASS |
| TC-D-015 | The queue and the picker are gated on the verbs they need | UC-C-01, UC-C-04 · S3, D2 | permission | critical | PASS |
| TC-D-016 | An unknown queue tab is refused rather than silently defaulted | UC-C-01 · S3 | negative | low | PASS |
| TC-D-017 | Two accountants click at once: one wins, one gets the conflict screen | UC-C-03, UC-C-05 · S2, S5 | concurrency | critical | PASS |
| TC-D-018 | Four concurrent HTTP reservations: exactly one 303 and one 409 each time | UC-C-03, UC-C-05 · S2, S5 | concurrency | critical | PASS |
| TC-D-019 | The audit records exactly one reservation, naming the winner | UC-C-03 · S2, S5, L8 | concurrency | critical | PASS |
| TC-D-020 | Reserving a completed request is refused | UC-C-03 · S9, L10 | negative | high | PASS |
| TC-D-021 | The reservation POST refuses a missing and a forged CSRF token | UC-C-03 · S1 | negative | high | PASS |
| TC-D-022 | A Manager and a Requester cannot reserve at all | UC-C-03 · S1, D2 | permission | critical | PASS |
| TC-D-023 | Reserving an on-hold request says it is on hold | UC-C-16 · L7, S3 | information-flow | medium | **FAIL (product defect)** — F-D-02 |
| TC-D-024 | Nothing expires a reservation | UC-C-17, UC-C-20 · S6 | functional | critical | PASS |
| TC-D-025 | The release screen belongs to the holder and states the reservation | UC-C-17 · S6, S7 | functional | high | PASS |
| TC-D-026 | An unconfirmed release does not release | UC-C-17 · S7 | negative | critical | PASS |
| TC-D-027 | A release with no reason, or a whitespace reason, is refused | UC-C-17 · S6 | negative | high | PASS |
| TC-D-028 | The holder releases with both, and the request returns to the queue | UC-C-17 · S6, S7 | functional | critical | PASS |
| TC-D-029 | A second accountant cannot open the release screen | UC-C-19 · S6 | permission | high | PASS |
| TC-D-030 | A second accountant cannot release somebody else's reservation | UC-C-19 · S6 | permission | critical | PASS |
| TC-D-031 | A Manager holds neither reservation verb | UC-C-17, UC-C-18 · S6, D2 | permission | high | PASS |
| TC-D-032 | An Accounts user cannot reassign — that verb is Admin-only | UC-C-18 · S6, D2 | permission | high | PASS |
| TC-D-033 | An Admin may release a reservation that is not theirs | UC-C-19 · S6 | permission | high | PASS |
| TC-D-034 | Reassign moves the reservation to the named colleague | UC-C-18 · S6 | functional | high | PASS |
| TC-D-035 | The trail records the handover with the reason given | UC-C-18 · S6 | information-flow | medium | PASS |
| TC-D-036 | A reassignment with no target is refused | UC-C-18 · S6 | negative | medium | PASS |
| TC-D-037 | A reassignment without the confirmation is refused | UC-C-18 · S7 | negative | high | PASS |
| TC-D-038 | A reassignment to somebody who cannot work the queue is refused | UC-C-18 · S6 | negative | high | PASS |
| TC-D-039 | A reassignment with no reason is refused | UC-C-18 · S6 | negative | medium | PASS |
| TC-D-040 | The reservation screens refuse a request nobody has reserved | UC-C-17, UC-C-18 · S6 | negative | medium | PASS |
| TC-D-041 | The stale nudge is gated on payment:process | UC-C-20 · Q6, S8 | permission | high | PASS |
| TC-D-042 | The holder is offered three live choices and no dead ends | UC-C-20 · Q6, S6 | functional | high | PASS |
| TC-D-043 | An administrator is offered release, reassign and hold | UC-C-20 · Q6, S6 | permission | medium | PASS |
| TC-D-044 | A non-holding accountant is offered no choice that answers 403 | UC-C-20 · Q6 | permission | medium | **FAIL (product defect)** — F-D-03 |
| TC-D-045 | The nudge refuses a request that is not reserved | UC-C-20 · Q6 | negative | low | PASS |
| TC-D-046 | Accounts holds an approved request, and on_hold=1 implies approved | UC-C-13 · L7 | functional | critical | PASS |
| TC-D-047 | A hold with no reason, or a whitespace reason, is refused | UC-C-13 · L7 | negative | high | PASS |
| TC-D-048 | payment:hold is one grant both ways; a Manager holds neither | UC-C-13, UC-C-15 · L7, D2 | permission | high | PASS |
| TC-D-049 | An on-hold request cannot be reserved and is takeable from neither list | UC-C-16 · L7, S3 | functional | critical | PASS |
| TC-D-050 | The requester answers while on hold and not one field changes | UC-C-14 · Q6, L7 | information-flow | high | PASS |
| TC-D-051 | Unholding returns the request to the takeable queue | UC-C-15 · L7 | functional | high | PASS |
| TC-D-052 | Unholding a request that is not on hold is refused | UC-C-15 · L7 | negative | low | PASS |
| TC-D-053 | A reserved request cannot be put on hold | UC-C-13 · L7 | negative | medium | PASS |
| TC-D-054 | The entry screen carries the six labels the design names | UC-C-06 · S14 | accessibility | high | PASS |
| TC-D-055 | The payee, head and invoice arrive as hidden inputs from the request | UC-C-06 · S14 | information-flow | critical | PASS |
| TC-D-056 | The amount is prefilled with what was approved, and the date with today | UC-C-06 · S14 | functional | medium | PASS |
| TC-D-057 | The payee survives queue → entry → payment row → payment detail h1 | UC-C-06, UC-C-21 · S14 | regression | high | PASS |
| TC-D-058 | A tampered head_id and vendor_payee are ignored or refused | UC-C-06, UC-C-28 · S14, S13 | negative | high | **FAIL (product defect)** — F-D-01 |
| TC-D-059 | Somebody else's entry screen is the conflict screen, not a 403 | UC-C-05 · S5 | information-flow | high | PASS |
| TC-D-060 | The entry screen refuses an unreserved request and an unknown id | UC-C-06, UC-C-26 · S15 | negative | high | PASS |
| TC-D-061 | Previewing, then abandoning, records nothing at all | UC-C-07 · S13 (D8) | functional | critical | PASS |
| TC-D-062 | The sheet swaps into #settle-mount without changing the URL | UC-C-07 · S13 | functional | high | PASS |
| TC-D-063 | The preview is gated on payment:settle and refuses a non-holder | UC-C-07 · S13, S5 | permission | high | PASS |
| TC-D-064 | A malformed amount comes back as the sheet, not an error page | UC-C-28 · S13 | negative | medium | PASS |
| TC-D-065 | Paid less than approved, settled, completes the request | UC-C-08, UC-C-29 · S10, Q4, L10 | functional | critical | PASS |
| TC-D-066 | Paid exactly the approved amount completes with a matching difference | UC-C-08 · S10 | boundary | high | PASS |
| TC-D-067 | Paid one paise more than approved is refused and writes nothing | UC-C-08, UC-C-28 · S10 (G13) | boundary | critical | PASS |
| TC-D-068 | A partial settlement with no reason is refused | UC-C-09 · S11, L9 | negative | high | PASS |
| TC-D-069 | A partial settlement routes to partial_review and reaches the manager | UC-C-09 · S11, L9, L11 | functional | critical | PASS |
| TC-D-070 | The manager accepts and closes it as completed_partial | UC-C-10 · S11, L11 | functional | critical | PASS |
| TC-D-071 | The accepted note joins the history | UC-C-10 · S11 | information-flow | medium | PASS |
| TC-D-072 | An empty concern is refused | UC-C-11 · S11 | negative | medium | PASS |
| TC-D-073 | A concern keeps the request in review with the words in the trail | UC-C-11 · S11 | functional | high | PASS |
| TC-D-074 | The partial decision belongs to the request's own manager | UC-C-12 · S11, R6 | permission | critical | PASS |
| TC-D-075 | The partial review redirects when there is nothing to review | UC-C-10 · S11 | negative | low | PASS |
| TC-D-076 | A second settlement creates no second payment | UC-C-27 · S9 | negative | critical | PASS |
| TC-D-077 | A linked payment cannot be edited | UC-C-23 · S12 | negative | critical | PASS |
| TC-D-078 | A linked payment cannot be voided | UC-C-23 · S12 | negative | critical | PASS |
| TC-D-079 | A payment cannot be created without a request | UC-C-25 · X5, S15 | negative | critical | PASS |
| TC-D-080 | A payment cannot be created against an unreserved request | UC-C-26 · S15 | negative | critical | PASS |
| TC-D-081 | A payment cannot be created by somebody who does not hold it | UC-C-26 · S15, S2 | permission | critical | PASS |
| TC-D-082 | Every payment this product can create is linked and offers only View | UC-C-22 · X6, S12 | proof-of-absence | medium | PASS |
| TC-D-083 | There is no refund, reversal or return-of-money route | UC-C-37 · X3 | proof-of-absence | high | PASS |
| TC-D-084 | The payment detail says refunds are out of scope and offers no control | UC-C-21, UC-C-37 · X3 | proof-of-absence | medium | PASS |
| TC-D-085 | A settlement value outside settled/partial is refused | UC-C-28 · S13 | negative | high | PASS |
| TC-D-086 | A partial with no reason is refused at the POST | UC-C-28 · S11 | negative | high | PASS |
| TC-D-087 | Zero, negative, non-numeric, empty and infinite amounts are refused | UC-C-28 · S13 | boundary | high | PASS |
| TC-D-088 | A malformed paid_on is refused | UC-C-28 · S13 | boundary | medium | PASS |
| TC-D-089 | A paid_on in a locked month is refused with 409 | UC-C-28 · S13 | negative | high | PASS |
| TC-D-090 | A payment cannot be dated in the future | UC-C-28 · S13 | boundary | low | **FAIL (product defect)** — F-D-06 |
| TC-D-091 | The settlement POST and the preview refuse a bad CSRF token | UC-C-08 · S13 | negative | high | PASS |
| TC-D-092 | [mobile] Reserve, preview and settle at 390 px with no sideways scroll | UC-C-03, UC-C-06, UC-C-08 · S1, S10 | functional | critical | PASS |
| TC-D-093 | [mobile] The conflict screen and the release screen fit a phone | UC-C-05, UC-C-17 · S5, S6 | functional | high | PASS |
| TC-D-094 | The queue offers a search control at desktop width | UC-C-02 · S4 | functional | medium | **FAIL (product defect)** — F-D-07 |
| TC-D-095 | A linked payment accepts no new attachment either | UC-C-23 · S12 | negative | low | **FAIL (product defect)** — F-D-08 |
| TC-D-096 | A recoverable whose category needs no project can still be paid | UC-C-31, UC-C-36 · V8, S15, L10 | functional | critical | **FAIL (product defect)** — F-D-11 |
| TC-D-097 | A settlement write requires `payment:settle` | UC-C-08 · S10, S13 | permission | high | **FAIL (product defect)** — F-D-10 |
| TC-D-098 | A partial settlement requires `payment:mark_partial` | UC-C-09 · S11 | permission | high | **FAIL (product defect)** — F-D-10 |
| TC-D-099 | Releasing a reservation notifies the requester and the approver | UC-C-17 · S6, N1 | information-flow | medium | **FAIL (product defect)** — F-D-12 |

**Totals** — 99 cases · 88 `PASS` · 11 `FAIL (product defect)` · 0 `BLOCKED` · 0 `NOT RUN`.
Two things the brief asks for could not be executed as written and are recorded in
§4 rather than dropped.

---

## 3. Detail

Preconditions are additive to: *the seeded database, an administrator signed in,
and the shared corpus of §1.2*. "Probe" means a request sent through a signed-in
context without following redirects, so a refusal is read as its own status.

### 3.1 The two reservation entry points and the linkable set (S1, S3, S4, L10)

| TC | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|
| TC-D-001 | `open` is approved and unclaimed | 1 GET the approved tab filtered to its number. 2 Read the Action cell. | The cell holds exactly one submit button named `Take for processing` inside `<form method="post" action="/requests/{id}/record-payment">` with a `csrf` input, and no link of that name. Reserving is a mutation; a GET link would let a crawler take a request. | As expected. | PASS |
| TC-D-002 | a fresh approved request | 1 Click `Take for processing`. | 303 to `/payments/new?request={id}`; the entry screen shows `.reserve-bar` reading `Reserved by you` and `nobody else can process this request`. | As expected. | PASS |
| TC-D-003 | a fresh approved request | 1 GET `/payments/new?q={number}`. 2 Click the takeable `.co`. | The picker's `h1` is `Which approved request is this for?`; the row is a `button.co` inside a POST form to `record-payment`; clicking it lands on the entry screen reserved by the caller. Both entry points reserve through the same route. | As expected. | PASS |
| TC-D-004 | `open`, `onHold`, `taken` | 1 GET `/payments/new/options?q=` for each. | Only `open` carries `action="/requests/{id}/record-payment"`. `onHold` and `taken` render `class="co is-taken" href="/requests/{id}"` — readable, not takeable. | As expected. | PASS |
| TC-D-005 | `onHold` | 1 GET the picker fragment for it. 2 GET the approved tab for it. | The picker row reads `On hold —` followed by the hold reason verbatim; the queue offers no `Take for processing`. | As expected. | PASS |
| TC-D-006 | `taken` | 1 GET the picker fragment for it. | The row names the holder (`Reserved by …`) and says `you cannot take this one`. | As expected. | PASS |
| TC-D-007 | `done` (settled) | 1 GET the picker fragment. 2 GET the approved tab. 3 GET the paid tab. | The picker does not contain the number at all and says `No approved requests match`; the approved tab offers no button; the Paid tab shows the row with a `Completed` pill. L10: a settled request has left the reservable set for good. | As expected. | PASS |
| TC-D-008 | `open` | 1 Search the picker by request number. | The row is returned. | As expected. | PASS |
| TC-D-009 | `open` | 1 Search by `Fervid Admin`. 2 Search by a requester that does not exist. | The row is returned for the requester; the nonsense search answers `No approved requests match` rather than listing everything. | As expected. | PASS |
| TC-D-010 | `open`, payee `Acme Supplies {runId}` | 1 Search by that payee. | The row is returned **and prints that payee** — the search and the display read the same column (`COALESCE(NULLIF(v.name,''),r.vendor_payee,'')`). | As expected. | PASS |
| TC-D-011 | `open` charged to Operations | 1 Search `Operations`. | The row is returned. | As expected. | PASS |
| TC-D-012 | `open` charged to Office Rent | 1 Search `Office Rent`. 2 Search `Staff Welfare`. | Returned for its own head; **excluded** for a head it is not charged to — otherwise the search is not filtering. | As expected. | PASS |
| TC-D-013 | `open` is ₹7,431.00 | 1 Search `743100` (paise). 2 Search `7,431.00` (as displayed). | Both find the row: the hint under the box promises "request number, requester, payee, project, head or amount", and the only amount a person can see is the formatted one. | Paise matched; `7,431.00` returned `No approved requests match`. The column is searched as `CAST(r.amount AS TEXT)`. | **FAIL (product defect)** F-D-04 |
| TC-D-014 | at least two rows in the approved tab | 1 Read the four metric values. 2 Re-GET with `q={number}`. 3 Read them again. | The table narrows to one row and the four counts are byte-identical: they span the caller's whole scope, never the page or the search. | As expected. | PASS |
| TC-D-015 | the Manager, Requester and no-role subjects | 1 Probe `/accounts-queue`, `/payments/new`, `/payments/new/options` as each. 2 As Accounts. 3 Anonymously. | 403 for all three subjects on all three routes; 200 for Accounts on the queue and the picker; 3xx to `/login` anonymously. | As expected. | PASS |
| TC-D-016 | — | 1 Probe `/accounts-queue?tab=everything`. | 400 reading `That queue tab does not exist.` — the tab is validated before the store is asked, so an invented tab cannot reach the store's own default branch. | As expected. | PASS |
| TC-D-094 | desktop viewport (asserted `> 860 px`) | 1 GET the approved tab. 2 Look for the search box. | A search control is on the screen. S4 is a requirement of this screen, and an accountant with a queue of hundreds cannot use a control that is not rendered. | `#queue-q` exists but is `display:none`: the queue ships only `<form class="m-filters">`, which `fervid-ds.css:3853` hides outside the 860 px media query. Requests, approvals, vendors and recoverables all ship a `.toolbar` beside theirs. | **FAIL (product defect)** F-D-07 |

### 3.2 Atomicity (S2, S5)

| TC | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|
| TC-D-017 | one approved request, two signed-in accountants on the queue | 1 Click `Take for processing` on both pages inside one `Promise.all`. | Exactly one page is on `/payments/new?request={id}` with `Reserved by you`. The other shows `.banner.bad` reading `… took this request before you` and `no payment was created`, has no `.error-code` and no `.reserve-bar`. G15: losing the race is a screen. | As expected, on every run. | PASS |
| TC-D-018 | four fresh approved requests | 1 For each: fire two concurrent `POST /requests/{id}/record-payment` from two contexts. | Every round yields exactly `[303, 409]`. The 303's `Location` is the entry screen; the 409's body is the conflict screen. Repeated four times to catch a lock that only usually holds. | Four rounds, `[303, 409]` each. | PASS |
| TC-D-019 | as TC-D-018 | 2 Probe `/audit?entity=payment_request&id={id}&action=process`. 3 Read the queue's Processing pill. | Exactly **one** audit row — `processing_by` moved once — and the name on it is the same person the queue's `.pill.processing` names. The audit write shares the reservation's transaction, so one row is one reservation. | One row per round, matching the queue's holder. | PASS |
| TC-D-020 | `done` | 1 Probe the reserve POST. | 409, no reservation. A completed request cannot be re-reserved. | 409 through the conflict screen (whose wording is F-D-02's second instance). | PASS |
| TC-D-021 | a fresh approved request | 1 Probe the reserve POST with no `csrf`, then with a forged one. 2 Re-check the queue. | 403 both times and the request is still unclaimed. | As expected. | PASS |
| TC-D-022 | Manager and Requester subjects | 1 Probe the reserve POST as each. | 403 — `reservation:reserve` belongs to Accounts and Admin only — and the request is untouched. | As expected. | PASS |
| TC-D-023 | `onHold` | 1 Probe the reserve POST. | 409, and the screen names the real cause: the request is **on hold**. It must not report a reservation that does not exist. | 409, but the body reads `Someone else took this request before you` and `is now reserved by Someone else`, with a `.pill.processing` on a request whose status is `approved`. | **FAIL (product defect)** F-D-02 |

### 3.3 No auto-release, release and reassign (S6, S7, G11, G12)

| TC | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|
| TC-D-024 | a request reserved through the queue | 1 Reload the entry screen. 2 Let a second accountant lose the race. 3 Post an over-approved settlement (refused). 4 Navigate away to the dashboard and the queue, then back. 5 Re-check the approved tab. | The reservation is intact at every step (`Reserved by you`) and the request is not takeable by anybody else. There is no auto-release: `ReleaseRequest` and `ReassignReservation` are the only writers of `processing_by`, and the reminder scheduler only notifies. | As expected. | PASS |
| TC-D-025 | holder's own reservation | 1 GET `/requests/{id}/reservation`. | 200; `h1` `Release or reassign this request`; `Reserved by you`; a required `Reason`; the checkbox `I confirm no payment has been initiated for this request`; a `Release reservation` submit. | As expected. | PASS |
| TC-D-026 | a reserved request | 1 Probe the release POST with a reason and **no** `confirm`. 2 Re-check takeability. | 400 reading `confirm that no payment was initiated`, and the request is still reserved. S7 exists because the alternative is two people paying one invoice. | As expected. | PASS |
| TC-D-027 | as above | 1 Probe with `confirm` and no reason. 2 Probe with a whitespace reason. | 400 both times reading `a reason is required`; still reserved. G12: the requester and approver are told why the money stopped. | As expected. | PASS |
| TC-D-028 | as above | 1 Probe with both. 2 Re-check the queue and the request. | 303 to `/accounts-queue`; the request is takeable again and reads `Approved — awaiting payment`. | As expected. | PASS |
| TC-D-029 | `taken`, plus a second Accounts subject | 1 Probe GET `/requests/{id}/reservation` as them. | 403 reading `Only the person holding this reservation can release it.` The screen is gated by ownership, not by a verb. | As expected. | PASS |
| TC-D-030 | as above | 1 Probe the release POST as them. 2 Confirm the holder still holds it. | 403; the holder's entry screen still answers 200. | As expected. | PASS |
| TC-D-031 | Manager subject, `taken` | 1 Probe release. 2 Probe reassign. | 403 both — a Manager holds no reservation verb at all. | As expected. | PASS |
| TC-D-032 | the holder, `taken` | 1 Probe reassign as the holder. 2 Open their reservation screen. | 403, and the screen renders no `#to_user_id` at all: `reservation:reassign` is held by **Admin alone** among the seeded roles. | As expected. | PASS |
| TC-D-033 | a request reserved by the second accountant | 1 GET the reservation screen as the Admin. 2 Probe the release POST with reason and confirm. | 200 naming the holder; 303 and the request is takeable again. Taking work off somebody is the reassign verb, and the store honours it for release. | As expected. | PASS |
| TC-D-034 | as above, plus an Accounts reassign target | 1 Choose the `Reassign to someone else` radio. 2 Post reassign with target, reason and confirm. 3 Check both accountants' entry screens. | The `[data-when]` reveal shows `#to_user_id`, which offers the queue-capable target; 303 to the request; the new holder gets 200 and the **old** holder now gets the 409 conflict screen. The request never returns to the open queue (G11). | As expected. | PASS |
| TC-D-035 | as above | 1 Re-open the reservation screen. | The history holds exactly one `reserved it for processing`, exactly one `reassigned the reservation`, and the reason verbatim. | As expected. | PASS |
| TC-D-036 | a reserved request, Admin | 1 Probe reassign with reason and confirm but no `to_user_id`. | 400 `Choose who should take this reservation.` | As expected. | PASS |
| TC-D-037 | as above | 1 Probe with a target and reason but no `confirm`. | 400 `Confirm that no payment has been initiated.` The screen asks one question for both branches, so both branches require the answer. | As expected. | PASS |
| TC-D-038 | as above, plus a Manager subject | 1 Confirm the select does not offer the Manager. 2 Probe reassign naming them anyway. | The option is absent, and the POST is refused 400 `That person cannot work the Accounts queue.` A reservation parked on somebody without `payment:process` is a request nobody can move. | As expected. | PASS |
| TC-D-039 | as above | 1 Probe with target and confirm but no reason. 2 Confirm the original holder still holds it. | 400 `a reason is required`; after all four refusals the reservation has not moved. | As expected. | PASS |
| TC-D-040 | `open` (never reserved) | 1 GET the reservation screen. 2 Probe reassign. | 409 `This request is not reserved by anyone.`; the POST answers 403. | As expected. | PASS |

### 3.4 The stale nudge (Q6, S8)

| TC | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|
| TC-D-041 | `taken` | 1 Probe `/requests/{id}/reservation/stale` as the holder, the Admin, a Manager, a Requester, a no-role user and anonymously. | 200 · 200 · 403 · 403 · 403 · 3xx→`/login`. The route is gated on `payment:process`, which is what puts a person in front of reserved rows. | As expected. | PASS |
| TC-D-042 | as above, as the holder | 1 Read the banner. 2 Count and read the `.a-list` choices. 3 Probe every href. | The banner says the reservation is theirs and that **nothing is released automatically**. Exactly three choices — `Carry on and record the payment`, `Release it`, `Put it on hold` — and each answers 200 for them. Reassign is not offered because they do not hold it. | As expected. | PASS |
| TC-D-043 | as above, as the Admin | 1 Same. | Three choices — `Release it`, `Hand it to a colleague`, `Put it on hold` — no `Carry on` (the reservation is not theirs), plus a `Reassign with reason` action bar. Each answers 200. Every choice is gated again on the verb that choice needs. | As expected. | PASS |
| TC-D-044 | as above, as a **non-holding** Accounts subject | 1 Probe every href the screen offers them. | Every offered choice answers 200. The template's own rule is that a choice the reader cannot take is not shown at all. | One choice is offered (`Put it on hold`) and it answers **403**: it links to the release screen, which refuses a caller who is neither the holder nor able to reassign. | **FAIL (product defect)** F-D-03 |
| TC-D-045 | `open` | 1 Probe the nudge. | 409 `This request is not reserved by anyone.` — off `processing` there is no holder to name and no elapsed time to count. | As expected. | PASS |

### 3.5 Hold and unhold (L7, Q6)

| TC | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|
| TC-D-046 | a fresh approved request, an Accounts subject | 1 Open the request. 2 `Put on hold`. 3 Fill the reason and submit. 4 GET the queue's hold tab. | The pill reads `On hold`; the banner quotes the reason and says payment is blocked until Accounts lifts it; the row appears in the hold tab — whose query is `on_hold=1 AND status='approved'`, which **is** the proof that `on_hold=1` implies `approved`. | As expected. | PASS |
| TC-D-047 | a fresh approved request | 1 Probe hold with `reason=""`. 2 With whitespace. 3 Re-check takeability. | 400 `a hold reason is required` both times; the request is still payable. | As expected. | PASS |
| TC-D-048 | Manager and Requester subjects | 1 Probe hold as the Manager. 2 Probe unhold as the Manager. 3 Probe hold as the Requester. 4 Read the Manager's view of an on-hold request. | 403 on all three — `payment:hold` is one grant covering both directions and Accounts holds it. The Manager's screen offers no `Release hold` and says `Only Accounts can take this off hold.` | As expected. | PASS |
| TC-D-049 | `onHold` | 1 Probe the reserve POST as Accounts. 2 Check the queue. 3 Check the picker. 4 Read the request. | 409; no `Take for processing`; no POST form in the picker; the request still reads `On hold`. | As expected. | PASS |
| TC-D-050 | a fresh approved request whose requester is the signed-in user | 1 Snapshot the whole Details list. 2 Hold it as Accounts. 3 Re-snapshot. 4 Post a comment as the requester. 5 Re-snapshot. | The Details list is byte-identical after the hold and after the comment; the comment appears in the thread; the hold is still on. Q6: a clarification changes no field. | As expected. | PASS |
| TC-D-051 | a held request | 1 `Release hold`. 2 Re-check the queue. | The pill returns to `Approved — awaiting payment` and the request is takeable again. | As expected. | PASS |
| TC-D-052 | as above, hold already lifted | 1 Probe unhold again. 2 Check the hold tab. | 403; the row is not in the hold tab. | As expected. | PASS |
| TC-D-053 | `taken` | 1 Probe hold. 2 Read the holder's own view. | Refused (403) and the reservation is untouched; the `Put on hold` control is not rendered on a reserved request. A hold pauses an *approved* request. | As expected. The refusal text is the generic permission sentence, not the state — F-D-02's third instance. | PASS |

### 3.6 The entry screen and its prefill (S14)

| TC | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|
| TC-D-054 | a request reserved by the caller | 1 Read each label element. 2 Read the attachment input. | The labels are exactly `Approved amount`, `Amount actually paid`, `Paid on`, `Payment mode`, `Transaction / UTR reference`, `Processing note`; each required one appends an `aria-hidden` asterisk so the accessible name is the bare string. The attachment input is `hidden`, carries no `aria-label`, and is reachable only as `input[name="attachment"]`; its visible affordance is `label.uploader` reading `Add the bank advice`. | Five labels exact. The sixth is `Processing note optional`: the `.opt` chip is **not** `aria-hidden`, so it joins the accessible name (F-D-05, informational). | PASS |
| TC-D-055 | as above, payee `Hidden Fields Vendor {runId}` | 1 Read the hidden inputs. 2 Read the "What was approved" card. | `request_id` is the request; `head_id` is the request's own head id; `vendor_payee` is the request's **display** payee; `invoice_no` is the request's invoice. The card names the payee and `Operations / Office Rent`. The accountant never chooses any of them. | As expected. | PASS |
| TC-D-056 | as above | 1 Read the two money fields and the date. | `Amount actually paid` is prefilled `4,200.00` (the approved amount), the read-only `Approved amount` shows `₹4,200.00`, and `Paid on` holds today's date. | As expected. | PASS |
| TC-D-057 | a request raised through the real form with payee `Payee Walk {runId}` | 1 Read the queue's Payee column. 2 Reserve and read the entry screen and its hidden payee. 3 Settle in full. 4 Read the payment detail `h1`. 5 Search the ledger by the payee. | The payee is that exact name at all five places, and the ledger row is findable by it. A `vendor_invoice` has no `vendor_payee` snapshot, so every screen must read the display payee. | As expected at all five. `fixtures.ts:178-188`'s "KNOWN GAP" is stale — the fix at `fca6939` holds (F-D-09, informational). | PASS |
| TC-D-058 | a request reserved by the caller, approved against Operations / Office Rent | 1 Read the honest hidden values. 2 POST `/payments` with `head_id` = People / Payroll and a `vendor_payee` of the poster's choosing. 3 Open the payment. | The tampered snapshot is either ignored (the payment is charged to the approved head and the approved payee) or refused. The approval names where the money is charged; the settlement screen is not a place to change it. | 303. The payment's `.rh-meta` renders `People / Payroll · paid 20 July 2026` where the approval named `Operations / Office Rent`, and the `<h1>` reads `NOT THE APPROVED PAYEE …`. `paymentInput` reads both off the form and `RecordPaymentForRequest` never compares them with the request. | **FAIL (product defect)** F-D-01 |
| TC-D-059 | `taken` | 1 Probe `/payments/new?request={id}`. | 409 on a rendered screen naming the holder and offering `Go back to the queue`, with no `.error-code`. G15: this is a screen about a lost race, not an error. | As expected. | PASS |
| TC-D-060 | `open` | 1 Probe the entry screen for an unreserved request. 2 For id 99999999. 3 For `request=not-a-number`. | 409 · 404 · 200 with the picker (an unparseable id falls back to "choose a request" rather than erroring). | As expected. | PASS |

### 3.7 The settlement preview is pure (S13, D8)

| TC | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|
| TC-D-061 | a request reserved by the caller | 1 Snapshot status, the presence of a payment outcome, and the ledger for the reference. 2 POST the settlement preview. 3 Re-snapshot. 4 Walk away to the queue and the dashboard. 5 Re-snapshot. | The preview answers 200 with `Not saved yet`, `Confirming saves the payment` and every typed value carried forward as a hidden input — and the three-part snapshot is identical before, after and after abandoning. No BeginTx, no attachment staging, no writes: only `POST /payments` writes. | As expected. | PASS |
| TC-D-062 | as above, entry form filled | 1 Click `Payment settled →`. 2 Compare the URL. 3 Locate the sheet and its footer. 4 Click `Go back`. | The URL does not change; the sheet is inside `#settle-mount`; its banner says confirming saves the payment and that it cannot be edited afterwards; `Go back` is an `<a href="/payments/new?request={id}">` (a button there would submit the form it sits in); after clicking it the reservation is intact. | As expected. | PASS |
| TC-D-063 | `taken` | 1 Probe the preview as a Manager. 2 As the Admin, who is not the holder. | 403 (no `payment:settle`) · 409 with the conflict screen. Losing the reservation between entry and confirmation is a screen, not an error. | As expected. | PASS |
| TC-D-064 | a request reserved by the caller | 1 POST the preview with `amount=not-money`. | 400 rendering the sheet with `enter a valid amount` and `Nothing has been saved`, not the error page. | As expected. | PASS |

### 3.8 Settlement outcomes (S10, S11)

| TC | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|
| TC-D-065 | approved ₹6,000.00, reserved | 1 Enter ₹4,500.00. 2 Open the sheet. 3 Choose `Fully settled`. 4 Confirm. 5 Read the request. | `#diff-banner` warns client-side; the sheet shows the ₹1,500.00 shortfall and `Completed` as the outcome; the payment page shows `.pill.completed`; the **request** reads `Completed` and `Difference · confirmed settled by Accounts`, and never `Still owed`. S10: settled completes it even under the approved figure. | As expected. | PASS |
| TC-D-066 | approved ₹6,100.00 | 1 Settle the exact amount. | `.pill.completed` and a `.cmp-row.match` of `₹0.00`; the request is `Completed`. | As expected. | PASS |
| TC-D-067 | approved ₹6,200.00, reserved | 1 POST ₹6,200.01. 2 Re-read the request and the ledger. | 400 `… more than the approved …` and `cancel this request and raise a new one`; the request is still `With Accounts` and the ledger has no row. G13: the ceiling is hard, and one paise over is over. | As expected. | PASS |
| TC-D-068 | approved ₹6,300.00, reserved | 1 POST `settlement=partial` with no reason. 2 With whitespace. | 400 `a reason is required for a partial settlement` both times; the request is still reserved. L9: the manager reads that reason when deciding. | As expected. | PASS |
| TC-D-069 | approved ₹6,400.00, reserved | 1 Enter ₹4,000.00. 2 Choose `Partial payment` in the sheet. 3 Fill the reason. 4 Confirm. 5 Sign in as the request's own manager. | The sheet's partial branch shows `Manager review` and reveals the reason only once partial is chosen; the payment shows `.pill.partial`; the request reads `Partial — manager review`; the manager's own request screen offers `Decide the partial payment`, and the review shows the ₹2,400.00 still owed, the quoted reason, and `Cannot be edited`. | As expected. | PASS |
| TC-D-070 | as above | 6 `Accept and close`, with a note. | The sheet restates the ₹2,400.00 written off; after confirming the request is `Completed — partial accepted` (`.pill.completed-partial`) — a terminal state deliberately distinct from a clean `completed` (G14). | As expected. | PASS |
| TC-D-071 | as above | 7 Read the history. | The manager's note appears in the trail. | As expected. | PASS |
| TC-D-072 | a partial-review request, as its manager | 1 Probe raise-concern with a whitespace comment. | 400 `a concern comment is required`. | As expected. | PASS |
| TC-D-073 | as above | 2 Open the review, `Raise a concern`, fill it, submit. | The sheet warns that this reverses nothing; the redirect returns to `/partial-review`; the request is still `Partial — manager review`; the words are in the **conversation** and the decision is still open. | As expected. | PASS |
| TC-D-074 | a partial-review request whose manager is somebody else, as the Admin (who holds `approval:accept_partial` and every other verb) | 1 Probe accept-partial. 2 Probe raise-concern. 3 Open the review screen. | 403 both. The screen renders no `Accept and close` button and says the manager decides. Holding the verb says a person may accept a shortfall, never **whose**. | As expected. | PASS |
| TC-D-075 | `done` (completed) and `open` (approved) | 1 Probe `/requests/{id}/partial-review` for each. | 303 to the request both times: off `partial_review` every sentence on that screen is false. | As expected. | PASS |

### 3.9 One request, one payment, never edited (S9, S12, S15, X5, X6)

| TC | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|
| TC-D-076 | approved ₹7,100.00, reserved | 1 POST the settlement. 2 POST it again with a different amount and reference. 3 Search the ledger for both references. | The first is 303 to `/payments/{id}`. The second creates **nothing**: the request has left `processing` and `idx_payments_request` is unique on `request_id`. Exactly one row for the first reference, none for the second. | As expected — the second POST answers 303 to the payment that already exists, which is the deliberate double-confirm branch, so the index is never reached over HTTP. | PASS |
| TC-D-077 | a settled payment | 1 Look for Edit. 2 GET `/payments/{id}/edit`. 3 POST it with valid values. | No control; the GET is a 303 back to the payment; the POST is 400 `a payment linked to a request cannot be edited`. Editing it would rewrite what the requester was told was paid. | As expected. | PASS |
| TC-D-078 | as above | 4 POST `/payments/{id}/void` with a reason. 5 Re-read the payment. | 400 `a payment linked to a request cannot be voided` — voiding would leave the request `completed` with nothing paid — and the figures are untouched. | As expected. | PASS |
| TC-D-079 | — | 1 POST `/payments` with every field but `request_id`. | 400 `Payments must be linked to an approved request.` X5: free-standing entry is gone. | As expected. | PASS |
| TC-D-080 | an approved, unreserved request | 1 POST `/payments` naming it. | 403 and nothing written. S15: approved is not enough; a payment needs a reservation the caller holds. | As expected (the message is the generic one — F-D-02). | PASS |
| TC-D-081 | `taken` (held by somebody else) | 1 POST `/payments` naming it. 2 Search the ledger. | 403 and nothing written. | As expected. | PASS |
| TC-D-082 | every payment this run recorded | 1 GET `/payments?month=2026-07&status=all`. 2 Inspect every row's actions. | Every row offers exactly one action, `View`, and no `Remove` disclosure: every payment this product can create is linked, and a linked payment is immutable. Edit and Void survive for historical `request_id IS NULL` rows only. | As expected. | PASS |
| TC-D-095 | a settled payment | 1 Confirm no screen offers an attachment form. 2 POST a real multipart file to `/payments/{id}/attachments`. | Refused. A payment the screen calls read-only must refuse a new document as firmly as it refuses an edit. | 303 — the file is attached. The route is gated only on `attachment:create` and `store.AddAttachment` has no `RequestID` guard, unlike `UpdatePaymentWithAttachment` and `VoidPayment`. | **FAIL (product defect)** F-D-08 |

### 3.10 Proof of absence (X3)

| TC | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|
| TC-D-083 | `done` | 1 POST and GET six refund-shaped paths: `/requests/{id}/refund`, `…/reverse`, `…/return-money`, `/payments/1/refund`, `/payments/1/reverse`, `/refunds`. | Every POST answers **405** (or 404) and every GET 404. `routes()` registers a catch-all `GET /`, so an unrouted POST matches the path and not the method. There is no store method behind any of them. | 405 on every POST, 404 on every GET. | PASS |
| TC-D-084 | `done` | 1 Follow the request's `View the payment` link. 2 Read the action bar. | The bar says `Refunds and reversals are outside this version.`; there is no Refund or Reverse control of any kind; the banner says the payment can no longer be edited or cancelled. | As expected. | PASS |

### 3.11 Tampering with the settlement POST

| TC | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|
| TC-D-085 | approved ₹8,100.00, reserved | 1 POST with `settlement` in turn `""`, `completed`, `SETTLED`, `part`, `refund`. | 400 each, reading `choose payment settled or partial settlement`. The vocabulary is two words and the store is case-sensitive. | As expected. | PASS |
| TC-D-086 | as above | (see TC-D-068 for the sheet path) 1 POST `partial` with no reason. | 400 `a reason is required for a partial settlement`. | As expected. | PASS |
| TC-D-087 | as above | 1 POST amounts `0`, `0.00`, `-100.00`, `not-money`, `""`, `"  "`, `1e400`. 2 Re-read the request and the ledger. | 400 for all seven; the request is still reserved and nothing is in the ledger. `money.ParsePaise` rejects non-positive, non-numeric, empty and infinite input. | As expected — twelve refusals in one test, then the reservation intact. | PASS |
| TC-D-088 | approved ₹8,200.00, reserved | 1 POST `paid_on` in turn `""`, `2026-13-45`, `20-07-2026`, `yesterday`. | 400 each, reading `valid head, date, and positive amount are required`. | As expected. | PASS |
| TC-D-089 | as above | 1 Lock `2025-03` with a reason. 2 POST with `paid_on=2025-03-15`. 3 Unlock. | **409** (not 400) reading `This month is locked` — the figures are fine, the period is closed — and nothing written. | As expected. | PASS |
| TC-D-090 | approved ₹8,300.00, reserved | 1 POST `paid_on=2029-12-31`. | 400. Money cannot have left the bank in 2029, and the payment lands in that month's actuals. | 303 — accepted. `validatePayment` checks the shape of the date and the month lock and nothing else; the date input carries no `max`. | **FAIL (product defect)** F-D-06 |
| TC-D-091 | approved ₹8,400.00, reserved | 1 POST `/payments` with no `csrf`, then a forged one. 2 The same against the preview. 3 Search the ledger. | 403 on all four; nothing written. | As expected. | PASS |

### 3.12 Settlements the product cannot complete, and the gate that is not there

| TC | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|
| TC-D-096 | for each of the four recoverable categories that require **no** project — `icd`, `security_deposit`, `employee_advance`, `other` — an `employee_advance`-type request raised with recoverable treatment through the real form and approved | 1 Confirm the form offers no project and no head for that category. 2 Reserve it. 3 Read the entry screen's hidden `head_id` and look for a head selector. 4 POST the settlement in full. 5 Read the request's status afterwards. All four categories are driven before any assertion, so one run reports all four. | An approved recoverable request is payable. Its category deciding whether it belongs to a project cannot decide whether it can be paid at all. | The form offers no project and no head; the entry screen posts `head_id=0` and offers no selector; the settlement answers **400 `validation failed: valid head, date, and positive amount are required`** for **all four** categories, and the request is left at `With Accounts` — reserved, unpayable, for ever. | **FAIL (product defect)** F-D-11 |
| TC-D-097 | a purpose-built role holding exactly `payment:view`, `payment:create`, `reservation:reserve`, `request:view` with request scope `all`, and a user holding only that role; two approved requests reserved by them | 1 POST the settlement **preview** (which writes nothing). 2 POST `/payments` with `settlement=settled`. | The preview is refused 403 for want of `payment:settle` — and so is the write, which is the act `payment:settle` names. | The preview is 403. The **write is 303**: the payment is recorded and the request completed by a caller who may not open the confirmation sheet. | **FAIL (product defect)** F-D-10 |
| TC-D-098 | as above | 3 POST `/payments` with `settlement=partial` and a reason. | Refused 403: `payment:mark_partial` exists in the vocabulary for exactly this decision. | 303 — the partial is recorded and the request routed to the manager. `payment:mark_partial` gates no route in the product. | **FAIL (product defect)** F-D-10 |
| TC-D-099 | a request reserved by the caller, whose approver is a separate signed-in user | 1 Read the release screen's action-bar note. 2 Count both people's notification rows. 3 Release with a reason and the confirmation. 4 Count again. | The note says *"The requester and the approver are both notified."* — so both counts rise by one. | The note is there and the release succeeds; both counts rise by **0**. `requestRelease` fires no event, and `internal/notify/events.go` declares none for release, reassign or unhold. | **FAIL (product defect)** F-D-12 |

### 3.13 The journey on a phone (390 px — a shipped guarantee)

Both cases run on `--project=chromium` **and** `--project=mobile-chrome`, and
both set the viewport to exactly 390 × 850 so the width is the same on either.

| TC | Preconditions | Steps | Expected result | Actual result | Verdict |
|---|---|---|---|---|---|
| TC-D-092 | a fresh approved request, 390 px | 1 Open the queue. 2 Take for processing. 3 Fill the entry form. 4 Open the sheet. 5 Confirm. 6 Measure `scrollWidth - clientWidth` at each of the four screens. | Four metric tiles and five segmented tabs survive the restack; the reservation, the sheet and the payment all behave as on the desktop; the payment offers no Edit or Void; no screen scrolls sideways; and the browser logs no console error, page error or 5xx. | As expected, on both projects. | PASS |
| TC-D-093 | as above plus a second accountant, 390 px | 1 Reserve as the first. 2 Navigate the second to the entry screen. 3 Measure. 4 Release from the first, with reason and confirmation. 5 Measure. | The second gets a rendered 409 naming the winner with a way back to the queue; neither the conflict screen nor the release screen scrolls sideways; the release succeeds and lands on the queue. | As expected, on both projects. | PASS |

---

## 4. What could not be executed, and why

| # | Item the brief asks for | Why not, and what stands in its place |
|---|---|---|
| 1 | **X6 — "historical payments keep `request_id IS NULL` and are unaffected."** | This environment contains **no** request-less payment and cannot be made to contain one: `store.CreatePayment` is off every HTTP route (X5) and the e2e seed creates no payments. TC-D-082 asserts the half that is testable — every payment the product *can* create is linked, and offers only `View`. The other half is owned by the Go test `TestMigrationV3IsIdempotentAndLeavesHistoricalPaymentsUntouched`, and by `UC-C-24`, which specifies the historical branch. Recorded rather than dropped. |
| 2 | **A second payment refused *by the partial unique index*.** | Over HTTP the index is never reached: `RecordPaymentForRequest`'s status guard refuses first, and `paymentCreate` turns that refusal into a 303 to the payment that already exists (the deliberate double-confirm branch). TC-D-076 therefore asserts the guarantee — no second payment exists — rather than the mechanism. The index itself is pinned by the Go test named in coverage row S9. |
| 3 | A reservation genuinely **26 hours old**, so the nudge screen's own staleness banner and the queue's `Resume → stale` link fire on elapsed time. | Ageing a row needs a direct `UPDATE payment_requests SET processing_at=…`, and this audit writes no SQL. The screen is reachable at any age (`requestStale` gates on `status='processing'` only), so TC-D-041 to TC-D-045 exercise its gating, its choices and its refusals in full; the 24-hour arithmetic is covered by the Go tests `TestStaleReservationScreenOffersTheFourChoices` and `TestTheQueueLinksAStaleReservationToTheNudgeScreen`. |
| 4 | ~~`DV-04` — a caller holding `payment:create` but not `payment:settle` can post a settlement.~~ | **Now executed.** No seeded role separates the two, so TC-D-097/098 builds the role the question is about through the roles screen's own `perm` field and assigns it as the subject's only role. Confirmed: the preview 403s and both writes succeed. |

---

## 5. Run record

| Run | Command | Result |
|---|---|---|
| chromium | `FERVID_E2E_PORT=4304 npx playwright test tests/e2e/audit-d-linking-settlement.spec.ts --project=chromium --reporter=line` | **77 passed (1.6m)**, 0 failed. Eleven of the 99 cases are `FAIL (product defect)`, each represented by a `test.fail()` test that Playwright counts as passing because it failed as declared. |
| mobile-chrome | the same with `--project=mobile-chrome` | **2 passed, 75 skipped**, 0 failed. Only `TC-D-092`/`TC-D-093` execute there; the rest are skipped by an explicit `test.skip` per describe, because a list assertion or a server refusal is not a layout question. |
| typecheck | `npm run typecheck` | clean |

Each annotated test was additionally run with its `test.fail()` removed, and the
failing assertion recorded, so no annotated case is hiding an unrelated failure:

| Case | The assertion that failed, verbatim |
|---|---|
| TC-D-013 | `the amount every screen displays must be searchable: the hint promises "or amount"` — `Expected substring: "PR-2026-…"` in an empty result |
| TC-D-023 | `the accountant has to be told the real reason payment is blocked` — `Expected: true, Received: false` |
| TC-D-044 | `every choice the nudge screen offers must answer for the reader it is offered to: /requests/15/reservation` — `Expected: 200, Received: 403` |
| TC-D-058 | `the payment must be charged to the head the approval named, not one the poster chose` — `Received string: "People / Payroll · paid 20 July 2026"` |
| TC-D-090 | `money cannot have left the bank in 2029, so the ledger must refuse the date` — `Expected: 400, Received: 303` |
| TC-D-094 | `S4: an accountant with a queue of hundreds needs the search on the screen they work from` — `Expected: visible, Received: hidden` |
| TC-D-095 | `S12: a payment the screen calls read-only must refuse a new document as firmly as it refuses an edit` — `Expected: 400, Received: 303` |
| TC-D-096 | `a recoverable request that was approved must be payable …` — all four categories: `400 validation failed: valid head, date, and positive amount are required` |
| TC-D-097/098 | `settling and marking a partial are the two decisions payment:settle and payment:mark_partial exist for …` — `Expected [403, 403], Received [303, 303]` |
| TC-D-099 | `both people the screen names must actually be told, in app or by email` — `Expected [1, 1], Received [0, 0]` |
