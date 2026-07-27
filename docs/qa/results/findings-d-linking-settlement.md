# Findings — D · payment linking, reservation and settlement

Fourteen findings from the audit of everything between `approved` and `completed`.
Eleven test cases in `tests/e2e/audit-d-linking-settlement.spec.ts` are annotated
`test.fail()`, one per defect, so the suite is green while every assertion stays
exactly as strong as written; the day one is fixed its test goes red with
"passed unexpectedly".

**Relationship to `UC-C`.** The sibling use-case document
(`docs/qa/use-cases/UC-C-settlement-recoverables-notifications.md` §4) reached six
of these by reading the code. Where that is so it is said in the finding, with its
`SD-`/`DV-` id, and this document adds the **executed reproduction**. Nothing here
contradicts it.

**Every annotated test was also run with its `test.fail()` removed**, so the
recorded failure is the one the case is about and not an earlier assertion. The
verbatim output is quoted under *Evidence*.

Run record: chromium **77 passed, 0 failed**; mobile-chrome **2 passed, 75
skipped, 0 failed**; `npm run typecheck` clean.

| ID | Severity | Confidence | Type | One line | Pinned by |
|---|---|---|---|---|---|
| F-D-11 | **critical** | confirmed | data-integrity | A recoverable request whose category requires no project can never be paid, and is stranded in `processing` for ever | TC-D-096 |
| F-D-01 | high | confirmed | data-integrity | `POST /payments` writes the client's `head_id`, `vendor_payee` and `invoice_no` without comparing them to the request | TC-D-058 |
| F-D-10 | high | confirmed | permission | The settlement *write* is gated more weakly than its *pure preview*; `payment:mark_partial` gates nothing at all | TC-D-097/098 |
| F-D-02 | medium | confirmed | information-flow | Every *state* conflict in this flow is reported as something it is not | TC-D-023 |
| F-D-03 | medium | confirmed | permission | The stale-reservation nudge offers a non-holding accountant a choice that answers 403 | TC-D-044 |
| F-D-04 | medium | confirmed | validation | The queue and picker search the amount as a paise integer, so the figure a person can see never matches | TC-D-013 |
| F-D-07 | medium | confirmed | UX | The Accounts queue has no search control at all above 860 px | TC-D-094 |
| F-D-12 | medium | confirmed | information-flow | The release screen promises the requester and approver are notified; release, reassign and unhold fire no event | TC-D-099 |
| F-D-06 | low | confirmed | validation | A payment may be dated arbitrarily far in the future | TC-D-090 |
| F-D-08 | low | confirmed | data-integrity | A linked, immutable payment still accepts a new attachment by route | TC-D-095 |
| F-D-09 | low | confirmed | test-fidelity | `fixtures.ts`'s "KNOWN GAP" about blank payees is stale, and three shipped specs repeat it | TC-D-057 |
| F-D-05 | informational | confirmed | accessibility | The processing-note field's accessible name is `Processing note optional` | TC-D-054 |
| F-D-13 | informational | confirmed | spec-divergence | `PROGRESS.md` still lists four design-system classes as having no CSS rule; all four have one | — (read) |
| F-D-14 | informational | confirmed | spec-divergence | The store's fifth request type, `recoverable`, has no card and no form — the URL falls back to the chooser | — (observed) |

---

### F-D-11 — A recoverable whose category requires no project can never be paid

- **Severity** — critical
- **Confidence** — confirmed (all four affected categories reproduced end to end through the real screens)
- **Type** — data-integrity
- **Where** — `internal/store/schema.go:60` (`payments.head_id INTEGER NOT NULL`), `internal/store/store.go:1621-1623` (`validatePayment` refuses `HeadID == 0`), `internal/app/linking.go:244-247` (`paymentEntry` computes `headID = 0` when the request has none), `internal/app/templates.go:389` (posts it as a hidden input), `internal/app/templates.go:1751-1766` (the form offers a project only for `emd`/`pbg`), `internal/store/recoverables.go:25-30` (four of the six seeded categories require no project)
- **Traces to** — UC-C-31, UC-C-36 · V8, S15, L10 · TC-D-096. Same defect as `UC-C` **SD-07**, reached there by reading; this is the executed reproduction across all four categories.
- **What happens** — A recoverable category that requires no project gives the request no project, and therefore no head — the form renders no head control at all for `icd`, `security_deposit`, `employee_advance` or `other`. The request is approved normally and reserved normally. The entry screen then posts `head_id=0`, `payments.head_id` is `NOT NULL`, and `validatePayment` refuses a zero head, so the settlement answers **400 `validation failed: valid head, date, and positive amount are required`** — for every amount, every date and every mode. The entry screen offers no head selector, so there is nothing the accountant can do. The reservation is not released by the refusal, so the request sits in `processing` holding it, and the recoverables register shows it as awaiting payment for ever.
- **Why it is wrong** — V8 makes the project *optional* on a recoverable and the seeded rules act on that: only `emd` and `pbg` require one. Approving a request is a promise that it can be paid. Here the category — a classification choice made by the requester on the form — silently decides whether the request is payable at all, and the failure surfaces only at the last step, to a third person, as a validation error about a field they were never shown. Nothing in the product can recover the request: it cannot be settled, and releasing the reservation only returns it to the queue for the next accountant to hit the same wall.
- **Reproduction**
  1. Sign in as an administrator. Open `/requests/new?type=employee_advance`.
  2. Fill a short title. Choose **Refundable or recoverable**. Choose **ICD — inter-corporate deposit** (or Security deposit, Employee advance, Other). Note that no project and no head control is rendered.
  3. Fill the expected return date, the counterparty (ICD/security deposit only), the repayment terms, ₹5,000.00, "What the money is for", the purpose, and an approver. Submit.
  4. As that approver, approve it for ₹5,000.00.
  5. As an accountant, `POST /requests/{id}/record-payment` (or take it from the queue). Open `/payments/new?request={id}` and read the hidden `head_id` — it is `0`.
  6. Fill the form and confirm the settlement. **400**, with the message above. Repeat with any values: the answer never changes.
  7. Re-read `/requests/{id}`: still `With Accounts`, still reserved.
- **Impact** — Every recoverable payment in four of the six seeded categories — inter-corporate deposits, security deposits, employee advances and "other" — cannot be recorded at all. Money that has genuinely left the bank can never be entered, the request never closes, the reservation is never freed, and the recoverables register (the whole point of Phase 4) is permanently wrong. This is the most severe defect this audit found.
- **Evidence** — TC-D-096, run without its annotation, reported every category in one array:
  ```
  icd: 400 validation failed: valid head, date, and positive amount are required
  security_deposit: 400 validation failed: valid head, date, and positive amount are required
  employee_advance: 400 validation failed: valid head, date, and positive amount are required
  other: 400 validation failed: valid head, date, and positive amount are required
  ```
  Its earlier, passing assertions record the mechanism: `input[name="head_id"]` has value `0`, `select[name="head_id"], #head` has count 0, and the status pill after each refusal is `With Accounts`.
- **Why no existing test catches it** — the Go twin `TestRecoverableRequestClosesOnPaymentRetainingClassification` seeds `project_id = nil` but passes a real `HeadID` explicitly (`internal/store/recoverables_test.go:802-814`), which is the one shape the real form cannot produce.
- **Suggested direction** — Decide where a project-less recoverable is charged and make it representable: either let `payments.head_id` be nullable for a recoverable payment (it is already excluded from budget actuals), or give the entry screen a head selector when the request has none, gated so a budget-treatment request can never use it.

---

### F-D-01 — `POST /payments` trusts the client for the head, the payee and the invoice

- **Severity** — high
- **Confidence** — confirmed (reproduced through HTTP in TC-D-058, and read in the store)
- **Type** — data-integrity
- **Where** — `internal/app/app.go:1499-1508` (`paymentInput`), `internal/store/store.go:892-913` (`RecordPaymentForRequest`), `internal/store/store.go:1620-1635` (`validatePayment`), `internal/app/templates.go:389-391` and `:448`, `internal/app/linking.go:316` (`settlementFields`)
- **Traces to** — UC-C-06, UC-C-28 · S14, S13 · TC-D-058. Same defect as `UC-C` **SD-05**; this is the executed reproduction.
- **What happens** — The entry screen carries the request's head, payee and invoice as **hidden inputs**, `hx-include` names all three on the preview POST, and `settlementFields` carries them through the no-JS path. `paymentInput` reads them straight back out of the form, and `RecordPaymentForRequest` re-reads only `status`, `processing_by`, `amount` and `approved_amount` from the request before inserting the form's values. `validatePayment` checks that the head exists and is active — never that it is *this request's* head. A settlement posted with `head_id` pointing at another project's head is accepted, and the money is charged there.
- **Why it is wrong** — The approval is the control: a manager approves an amount **against a project and head**. S14 prefills the payee, project and head *from the request* precisely so the accountant does not choose them, and the screen offers no control to change any of them — so the server treats as input what the design treats as a fact. The consequence is silent: the request screen goes on showing `Operations / Office Rent` while the payment, the variance grid and the monthly report attribute the money to `People / Payroll`. The payee facet has a sharper edge — the payment detail renders `<a href="/vendors/{{.Request2.VendorID}}">{{.Payment.VendorPayee}}</a>` (`templates.go:282`), so a tampered name is shown as a link to the *real* vendor.
- **Reproduction**
  1. Raise and approve a `vendor_invoice` request for ₹4,400.00 against **Operations / Office Rent**.
  2. Take it for processing.
  3. Read the hidden `head_id` off `/payments/new?request={id}` (call it `H_ok`) and any other active head's id from the request form's `#head` select (`H_other`).
  4. `POST /payments` with a valid `csrf`, `request_id={id}`, `amount=4400.00`, `paid_on=2026-07-20`, `payment_mode=bank_transfer`, `reference_no=UTR-TAMPER`, `settlement=settled`, **`head_id=H_other`**, **`vendor_payee=NOT THE APPROVED PAYEE`**.
  5. Follow the 303 and read `.rh-meta` and the `<h1>` on the payment.
- **Impact** — Anybody who may work the Accounts queue (the seeded **Accounts** role, and Admin) can move an approved payment onto a budget head nobody approved and rename its payee, with no screen anywhere showing the mismatch. That defeats the budget control the variance grid exists to provide, and it is invisible to the requester, the approver and the trail — the audit's `after` blob records the tampered values as if they were the request's.
- **Evidence** — TC-D-058: status `303`; `.rh-meta` rendered `People / Payroll · paid 20 July 2026` where the request says `Operations / Office Rent`; the payment's `<h1>` read `NOT THE APPROVED PAYEE …`.
- **Suggested direction** — Derive all three from the request inside `RecordPaymentForRequest`, which already has the row open in its transaction, and ignore whatever the form sent. If the fields must keep travelling through the form for the no-JS confirmation, compare and refuse on mismatch rather than trusting.

---

### F-D-10 — The settlement write is gated more weakly than its pure preview

- **Severity** — high
- **Confidence** — confirmed (reproduced with a purpose-built role in TC-D-097/098)
- **Type** — permission
- **Where** — `internal/app/app.go:387` (`POST /payments` → `payment:create`), `internal/app/app.go:454` (`POST /requests/{id}/settlement-preview` → `payment:settle`), `internal/store/store.go:866-957` (`RecordPaymentForRequest` checks the reservation and nothing else), `internal/store/permissions.go:153` (`payment:mark_partial` is in the vocabulary)
- **Traces to** — UC-C-07, UC-C-08, UC-C-09 · S10, S11, S13 · TC-D-097, TC-D-098. Recorded as `UC-C` **DV-04**, unverified there; this is the executed reproduction.
- **What happens** — The read-only preview requires `payment:settle`. The write that records the payment and closes the request requires only `payment:create`. A subject holding `payment:view`, `payment:create`, `reservation:reserve` and `request:view` — and neither `payment:settle` nor `payment:mark_partial` — is refused **403** on the preview and answered **303** on both writes: it settled one request in full and routed another to `partial_review` as a partial settlement.
- **Why it is wrong** — The gate is on the wrong side of the write. The preview persists nothing (D8), so gating it and not the writer inverts the protection. `payment:settle` and `payment:mark_partial` exist in the canonical vocabulary to name exactly these two decisions, and `mark_partial` gates no route anywhere in the product. Nothing ships broken today because both seeded roles that hold `create` also hold `settle` — the risk is an administrator building a "payment entry, no settlement decisions" role and getting one that can still settle and still write off a shortfall to the manager's queue.
- **Reproduction**
  1. As an administrator, `POST /roles/new` with a name, then `POST /roles` with `role_id`, `name`, `scope_request=all` and four `perm` fields: `payment:view`, `payment:create`, `reservation:reserve`, `request:view`.
  2. Create a user, then `POST /users` with their `id`, `name`, `role=data_entry`, `active=on` and a single `role_ids` naming the new role (this replaces the default Accounts assignment).
  3. Sign in as them. Reserve two approved requests.
  4. `POST /requests/{id}/settlement-preview` → **403**.
  5. `POST /payments` with `settlement=settled` → **303**, payment recorded, request `completed`.
  6. `POST /payments` with `settlement=partial` and a reason → **303**, request `partial_review`.
- **Impact** — Administrators designing least-privilege roles. No seeded configuration is affected, which is why this is high rather than critical.
- **Evidence** — TC-D-097/098, run without its annotation: the preview assertion passed at `403`, then `expect([settled.status, partial.status]).toEqual([403, 403])` received `[303, 303]`.
- **Suggested direction** — Gate `POST /payments` on `payment:settle` in addition to `payment:create`, and branch on `payment:mark_partial` inside the handler when `settlement=partial`; or, if the two verbs are not wanted, remove them from the vocabulary and drop the gate from the preview so the two agree.

---

### F-D-02 — Every state conflict in this flow is reported as something it is not

- **Severity** — medium
- **Confidence** — confirmed (three separate instances observed)
- **Type** — information-flow
- **Where** — `internal/store/store.go:736-748` (`ReserveRequest` returns one `ErrForbidden` for three causes), `internal/app/linking.go:82-99` (`requestRecordPayment` funnels every `ErrForbidden` into the conflict screen), `internal/app/linking.go:58-71` (`reservationConflict` defaults the holder to `Someone else`), `internal/app/templates.go:775-785`, `internal/app/http_errors.go:191-192`, `internal/app/app.go:1628-1640` (`friendly` has no `ErrForbidden` branch), `internal/app/linking.go:341-364` (`settlementError`)
- **Traces to** — UC-C-16, UC-C-13, UC-C-26 · L7, S3, S15 · TC-D-023, and observed in TC-D-020, TC-D-053, TC-D-080. Extends `UC-C` **SD-04** from one instance to three.
- **What happens** — Three different refusals arrive as the wrong sentence.
  1. **Reserving an on-hold request** → `409` on the conflict screen reading *"Someone else took this request before you"*, *"{Number} is now reserved by Someone else"*, with a `.pill.processing` reading *"Processing — Someone else"* — on a request whose status is `approved` and whose `processing_by` is `NULL`. The same wording appears for a **completed** request (TC-D-020) and for one frozen by a cancellation request.
  2. **Holding a reserved request** → `403` reading *"You do not have permission to perform this action."* The caller has the permission; the request is in the wrong state. `respondStoreError` maps every `ErrForbidden` to that sentence.
  3. **Settling a request you have not reserved** → `403` rendering the settlement confirmation with *"Something went wrong while processing your request."*, because `friendly()` has no `ErrForbidden` case and falls to its default.
- **Why it is wrong** — G15's point is that a refusal in this flow is a *screen that tells the accountant what happened and what to do next*. Two of the three tell them something untrue and the third tells them nothing. The picker already gets case 1 right in the same codebase — it renders `On hold — {HoldReason}` (`templates.go:585`) — so the information exists and only the reserve path discards it.
- **Reproduction**
  1. Put an approved request on hold with a reason.
  2. `POST /requests/{id}/record-payment` (or click the row from a stale picker tab). The banner names a holder that does not exist.
  3. Take a different request for processing, then `POST /requests/{id}/hold` on it: the answer is a permission error for an action you are permitted to take.
- **Impact** — An accountant is told a colleague took a request nobody has, so the natural next step is to go and ask them; the real answer — "the requester owes us an answer" — is one click away on the request, and the screen points away from it. A permission error for a state conflict sends the same person to an administrator for a grant they already hold.
- **Evidence** — TC-D-023: `expect(body.toLowerCase().includes('on hold')).toBe(true)` — `Expected: true, Received: false`, against a body containing `Someone else took this request before you`. TC-D-020, TC-D-053 and TC-D-080 record the other statuses and bodies.
- **Suggested direction** — Have `ReserveRequest` distinguish its three causes (one `SELECT status, processing_by, on_hold` before the UPDATE would name it) and let `reservationConflict` render the on-hold and completed cases in their own words. Separately, give `friendly()` an `ErrForbidden` branch so a state conflict carries its sentence to the reader the way `ErrValidation` does.

---

### F-D-03 — The stale nudge offers a non-holding accountant a choice that answers 403

- **Severity** — medium
- **Confidence** — confirmed
- **Type** — permission
- **Where** — `internal/app/templates.go:1121` (the `Put it on hold` entry, gated on `payment:hold` alone), `internal/app/linking.go:521-538` (`reservationForm` refuses anybody who is neither the holder-with-release nor a reassigner), `internal/app/templates.go:1080-1084` (the template's own stated rule)
- **Traces to** — UC-C-20 · Q6 · TC-D-044
- **What happens** — `GET /requests/{id}/reservation/stale` is gated on `payment:process`, so **any** accountant reaches it, not only the holder. For a non-holding accountant three of the four choices are correctly withheld and the fourth — *"Put it on hold"* — is offered, because it is gated only on `payment:hold`, which every accountant holds. It links to `/requests/{id}/reservation`, which answers **403** for a caller who neither holds the reservation nor may reassign. The screen offers exactly one thing to do and it is a dead end.
- **Why it is wrong** — The template states the rule in its own comment: *"A choice the reader could not take is not shown greyed out — it is not shown."* The Go test that guards this screen (`TestStaleScreenOffersFourChoicesThatAllLandSomewhereUsable`) checks the hrefs as an **administrator**, who can reach all of them, so the one reader for whom the rule breaks is the one nobody tested.
- **Reproduction**
  1. Create two users holding exactly the **Accounts** role.
  2. As the first, take an approved request for processing.
  3. As the second, open `/requests/{id}/reservation/stale`: 200, with one choice.
  4. Follow it: **403** *"Only the person holding this reservation can release it."*
- **Impact** — An accountant sent to the nudge screen for a colleague's overdue reservation is offered one action and refused when they take it. The screen exists to route a stalled payment to somebody who can move it; for this reader it routes them into a wall.
- **Evidence** — TC-D-044: `every choice the nudge screen offers must answer for the reader it is offered to: /requests/15/reservation` — `Expected: 200, Received: 403`.
- **Suggested direction** — Gate the hold entry the way the release entry is gated — on being able to act on *this* reservation (`ReserveMine` or `reservation:reassign`) as well as on `payment:hold` — or point it at a screen the reader can use.

---

### F-D-04 — The amount search matches paise, so the figure a person can see never matches

- **Severity** — medium
- **Confidence** — confirmed
- **Type** — validation
- **Where** — `internal/store/store.go:1174` (`CAST(r.amount AS TEXT) LIKE ?`), `internal/app/templates.go:629` (the hint), `internal/app/templates.go:695` (the queue's placeholder), `internal/money` (money is `int64` paise)
- **Traces to** — UC-C-02, UC-C-04 · S4 · TC-D-013
- **What happens** — S4 requires the linkable list to be searchable by amount, and the picker's hint says so: *"Search by request number, requester, payee, project, head or amount."* The query compares the search text against `CAST(r.amount AS TEXT)`, which is the **paise** integer. For a request of ₹7,431.00 the row is found by `743100` (and by `7431` as a substring) and **not** by `7,431.00` or `7431.00` — the only two forms of the amount that appear anywhere on screen.
- **Why it is wrong** — Every rendering of money goes through `money.FormatPaise`, which produces `₹7,431.00`. A search box beside a column of `₹7,431.00` that answers "No approved requests match" when you type `7,431.00` is not a search by amount. The substring behaviour also produces false positives: `7431` matches ₹74.31 and ₹743.10 as readily as ₹7,431.00.
- **Reproduction**
  1. Approve a request for exactly ₹7,431.00.
  2. Open `/payments/new`, type `7,431.00` into "Search approved requests" → `No approved requests match`.
  3. Type `743100` → the row appears.
- **Impact** — Every accountant who searches the queue or the picker by the amount on the invoice in their hand. The workaround (multiply by 100 and drop the separators) is not discoverable from the hint.
- **Evidence** — TC-D-013: `pickerOptions('743100')` contained the request number; `pickerOptions('7,431.00')` returned the empty-result fragment.
- **Suggested direction** — Normalise the needle the way `money.ParsePaise` normalises input — strip `₹`, commas and whitespace — and if what remains parses as a decimal, match `r.amount` on the paise value rather than on its text.

---

### F-D-07 — The Accounts queue has no search control above 860 px

- **Severity** — medium
- **Confidence** — confirmed
- **Type** — UX
- **Where** — `internal/app/templates.go:693-697` (the queue's only filter form is `class="m-filters"`), `web/static/fervid-ds.css:3853` (`.m-filters { display: none; }`), `:4322` (it becomes `display:flex` only inside `@media (max-width: 860px)`)
- **Traces to** — UC-C-02 · S4 · TC-D-094
- **What happens** — The queue renders exactly one filter form and it is the mobile one, so at desktop width the search box and its Search button are `display:none`. An accountant on a laptop has no way to search the queue except by typing `?q=` into the address bar. The server-side search works perfectly — this is purely the missing control.
- **Why it is wrong** — S4 is a requirement of this screen and `UC-C-02` specifies it. Every other searchable list ships **both** renderings and hides whichever does not belong: requests (`templates.go:2127` + `:2143`), approvals (`:2936` + `:2943`), vendors (`:1300` + `:1321`), recoverables (`:3512` + `:3528`). The queue is the only one that ships the mobile form alone, and the CSS comment at `:4334` explains why that is a mistake — the desktop `.toolbar` "only steps aside once a mobile filter row exists to replace it", which assumes there is a `.toolbar` to step aside.
- **Reproduction**
  1. Sign in as an accountant on a window wider than 860 px and open `/accounts-queue`: no search input.
  2. Narrow the window below 860 px: it appears.
  3. Confirm the feature exists: `/accounts-queue?tab=approved&q=PR-2026-000004`.
- **Impact** — Every desktop accountant, on the one screen whose job is to find the request matching the invoice in their hand, on a table with no pagination.
- **Evidence** — TC-D-094: `#queue-q` — `Expected: visible, Received: hidden`, at a viewport asserted wider than 860 px. (It is also why TC-D-014 drives the search through the query string.)
- **Suggested direction** — Add the `.toolbar` twin the other four lists have, or drop `.m-filters` from this form so the single form is visible at both widths.

---

### F-D-12 — The release screen promises notifications that are never sent

- **Severity** — medium
- **Confidence** — confirmed
- **Type** — information-flow
- **Where** — `internal/app/templates.go:1042` (*"The requester and the approver are both notified."*), `internal/app/linking.go:623-635` (`requestRelease` — no `a.fire`), `internal/app/linking.go:642-675` (`requestReassign` — none), `internal/app/linking.go:710-720` (`requestUnhold` — none, while `requestHold` at `:694-705` does fire), `internal/notify/events.go:8-21` (no release, reassign or unhold event exists)
- **Traces to** — UC-C-17, UC-C-18 · S6, N1 · TC-D-099. Same defect as `UC-C` **SD-02**; reproduced here because the promise is made on this audit's screen.
- **What happens** — The release/reassign screen's action bar states that the requester and the approver are both notified. Releasing succeeds (303) and neither person's notification centre gains a row: the count for both goes from *n* to *n*. No email is queued either — there is no event to queue.
- **Why it is wrong** — The sentence is a promise made to the person taking the action, who will reasonably assume the requester now knows their payment has stopped. Holding a request *does* fire `EventRequestOnHold`; releasing one — the same kind of interruption from the requester's point of view — fires nothing, and lifting a hold fires nothing either, so the requester is never told the pause is over.
- **Reproduction**
  1. Reserve an approved request as an accountant.
  2. Open `/requests/{id}/reservation` and read the action bar.
  3. Note the row count at `/notifications?scope=all` for both the requester and the approver.
  4. Release with a reason and the confirmation.
  5. Re-count: unchanged for both.
- **Impact** — The requester and the approver are not told their payment stopped, so nobody chases it; the accountant believes they have been told. The screen's own words are the evidence of intent.
- **Evidence** — TC-D-099: `expect([approverDelta, requesterDelta]).toEqual([1, 1])` received `[0, 0]` after a 303 release.
- **Suggested direction** — Either add the three events (`request_released`, `reservation_reassigned`, `request_unheld`) and fire them, or delete the sentence. A promise in the UI with no writer behind it is the worst of the two.

---

### F-D-06 — A payment may be dated arbitrarily far in the future

- **Severity** — low
- **Confidence** — confirmed
- **Type** — validation
- **Where** — `internal/store/store.go:1620-1626` (`validatePayment` checks only that `paid_on` parses, plus the month lock), `internal/app/templates.go:413` (`<input type="date">` with no `max`)
- **Traces to** — UC-C-28 · S13 · TC-D-090
- **What happens** — `POST /payments` with `paid_on=2029-12-31` is accepted: the payment is created, the request closes as `completed`, and the row lands in the ledger and the variance actuals for December 2029.
- **Why it is wrong** — `paid_on` records when money left the bank, so a future date records something that has not happened. No spec forbids it explicitly, which is why this is low — but the same function already refuses a *locked* month, so the store does police the period and simply does not police the direction. A future-dated actual is invisible in the current month's grid, which is where somebody would look for it.
- **Reproduction**
  1. Reserve an approved request.
  2. `POST /payments` with the entry screen's hidden fields, `settlement=settled` and `paid_on=2029-12-31`.
  3. 303 to the payment; `GET /payments?month=2029-12` shows it.
- **Impact** — A typo in the year (`2062` for `2026`) silently removes a real payment from the month it belongs to, and the request looks correctly closed.
- **Evidence** — TC-D-090: `money cannot have left the bank in 2029, so the ledger must refuse the date` — `Expected: 400, Received: 303`.
- **Suggested direction** — Refuse a `paid_on` after today in `validatePayment`, and add `max` to the date input so the browser says so first.

---

### F-D-08 — A linked, immutable payment still accepts a new attachment by route

- **Severity** — low
- **Confidence** — confirmed
- **Type** — data-integrity
- **Where** — `internal/app/app.go:392` (gated on `attachment:create` only), `internal/store/store.go:1310-1334` (`AddAttachment` has no `RequestID` guard); contrast `store.go:609-611` (edit) and `store.go:649-651` (void)
- **Traces to** — UC-C-23 · S12 · TC-D-095. Same defect as `UC-C` **SD-03**; this is the executed reproduction.
- **What happens** — `POST /payments/{id}/attachments` with a real multipart file succeeds against a payment that settles a request: 303, and the file appears in its Proof list. No screen offers the control, so this is reachable only by a hand-rolled request.
- **Why it is wrong** — S12 says a recorded payment is immutable, and the screen says so twice: a `Read-only` pill and a banner reading *"This payment can no longer be edited or cancelled."* Both `UpdatePaymentWithAttachment` and `VoidPayment` refuse a row with a `request_id`; `AddAttachment` is the one mutation that does not. It is low because the addition is audited, additive and arguably legitimate (a bank advice that arrives late) — which is exactly why the decision should be explicit rather than accidental.
- **Reproduction**
  1. Settle a request in full and note the payment id.
  2. Read the `fervid_csrf` cookie.
  3. `POST /payments/{id}/attachments` as `multipart/form-data` with `csrf` and an `attachment` file part.
  4. 303; reload the payment and the file is in **Proof**.
- **Impact** — Anybody holding `attachment:create` (Requester, Accounts, Admin) who will hand-roll a POST can add a document to a payment the product presents as closed. The trail records who did it, so this is a consistency hole rather than a way to hide anything.
- **Evidence** — TC-D-095: `S12: a payment the screen calls read-only must refuse a new document as firmly as it refuses an edit` — `Expected: 400, Received: 303`.
- **Suggested direction** — Decide the rule in one place: refuse when `RequestID != nil` (matching the other two writers), or allow it deliberately and let the linked payment screen offer the upload so the "Read-only" pill stops being a half-truth.

---

### F-D-09 — The "KNOWN GAP" comment about blank payees is stale, in four places

- **Severity** — low
- **Confidence** — confirmed
- **Type** — test-fidelity
- **Where** — `tests/e2e/fixtures.ts:178-188` (the comment), `tests/e2e/core-workflows.spec.ts:96-100` and `:130`, `tests/e2e/regression-issues.spec.ts:530-536`. The fix is commit `fca6939`: `internal/store/store.go:1136`, `internal/app/linking.go:257-261`, `internal/app/templates.go:364`, `:390`, `:584`, `:717`.
- **Traces to** — UC-C-06, UC-C-21 · S14 · TC-D-057. Recorded as `UC-C` **DV-13**; this is the empirical confirmation the task asked for.
- **What happens** — `createApprovedRequest`'s doc comment states that for a request raised through the real form "the queue's Payee column, the entry screen's payee, the payment row's `vendor_payee` and the payment detail's `<h1>` are all blank", and that any spec needing a payee "needs that fixed first". Walked end to end, all five hops carry the vendor's name: the queue's Payee cell, the entry screen's card, the entry screen's hidden `vendor_payee`, the payment detail's `<h1>`, and the ledger row found by searching that name.
- **Why it is wrong** — The comment is the stated reason two shipped tests avoid the payee: `core-workflows.spec.ts` searches the ledger by the processing note "because the payee column … is blank today" and identifies the payment detail by its number "because the h1 is the payee, which is blank today". A stale gap comment costs coverage — the payee is now the most natural handle on those rows and both tests steer around a defect that no longer exists.
- **Reproduction** — TC-D-057: raise and approve a `vendor_invoice` request with payee `Payee Walk X` through the real form, then read all five hops. Every one reads `Payee Walk X`.
- **Impact** — Test authors, and anybody reading `fixtures.ts` to learn what the flow guarantees.
- **Suggested direction** — Replace the KNOWN GAP paragraph with what is now true (the payee is the vendor's name, resolved by `COALESCE(NULLIF(v.name,''), r.vendor_payee)`) and let the two shipped tests assert on it. Those files are outside this audit's remit, so nothing was changed.

---

### F-D-05 — The processing-note field's accessible name is "Processing note optional"

- **Severity** — informational
- **Confidence** — confirmed
- **Type** — accessibility
- **Where** — `internal/app/templates.go:432`; contrast every required label on the same form (`:402`, `:413`, `:420`)
- **Traces to** — UC-C-06 · S14 · TC-D-054
- **What happens** — Required fields append `<span class="req" aria-hidden="true">*</span>`, so their accessible name is the bare label. The optional field appends `<span class="opt">optional</span>` with **no** `aria-hidden`, so a screen reader announces *"Processing note optional"* and `getByLabel('Processing note', { exact: true })` resolves nothing.
- **Why it is wrong** — It is defensible as prose, which is why this is informational. It is worth knowing because it is inconsistent with the `.req` treatment two lines above, and because it is a trap for every test author — the same trap `UC-B` recorded for the approve sheet's `Note optional`.
- **Reproduction** — `GET /payments/new?request={id}` on a reservation you hold; read `label[for="remarks"]`.
- **Impact** — Test authors, and screen-reader users who hear one field's optionality announced and no other field's requiredness.
- **Evidence** — `label[for="remarks"]` has text `Processing note optional`; its `span.opt` carries no `aria-hidden`.
- **Suggested direction** — Pick one convention: mark `.opt` `aria-hidden` and let the absence of `*` mean optional, or drop `aria-hidden` from `.req` so both are announced.

---

### F-D-13 — `PROGRESS.md` still lists four design-system classes as having no CSS rule

- **Severity** — informational
- **Confidence** — confirmed (read both files)
- **Type** — spec-divergence
- **Where** — `docs/superpowers/PROGRESS.md:368-375` versus `web/static/fervid-ds.css:367` (`.metric-foot`), `:378` (`.metric.warn`), `:387` (`.metric.good`), `:397` (`.btn.approve`)
- **Traces to** — the Accounts queue's metric strip (UC-C-01) · TC-D-014 asserts the four tiles render. Same divergence as `UC-C` **DV-11**.
- **What happens** — The Known-gaps section says all four "appear in the mockups but have **no rule**", and singles out `.metric-foot` as "so today that third line in every metric tile renders unstyled". All four have rules, added by Phase 6 under a comment that says so (*"Phase 0 defects, fixed in Phase 6"*), and `PROGRESS.md`'s own Phase-6 detail table records the fix.
- **Why it is wrong** — The same file contradicts itself three sections apart, and the queue's metric strip is the screen the stale claim names.
- **Impact** — Anybody reading `PROGRESS.md` to decide what is still broken.
- **Suggested direction** — Delete the entry; the fix is already recorded a hundred lines earlier.

---

### F-D-14 — The store's fifth request type has no card and no form

- **Severity** — informational
- **Confidence** — confirmed (observed while building TC-D-096)
- **Type** — spec-divergence
- **Where** — `internal/store/requests.go:78-81` (`requestTypes` accepts `recoverable`), `internal/store/requests.go:239-243` (it has its own validation branch), `internal/app/requests.go:42-55` (`requestTypeOptions` has four cards, not five), `internal/app/requests.go:71-77` (an unknown type silently falls back to the chooser)
- **Traces to** — UC-C-31 · V1 · discovered by TC-D-096
- **What happens** — `GET /requests/new?type=recoverable` renders the type chooser, not a form — the type is unreachable from the UI even though the store validates it and the enum accepts it. A recoverable must therefore be raised as an `employee_advance` (or a reimbursement) with recoverable treatment, which is what TC-D-096 does.
- **Why it is wrong** — The comment above `requestTypeOptions` says "Adding a type here is the only way to add one to the product", so the four-card list is deliberate — but the store's fifth type and its validation branch are then dead code, and the fallback is silent: a typed or bookmarked URL shows the chooser with no explanation.
- **Impact** — Nobody today; it is a trap for the next person who reads `requestTypes` and assumes five types ship.
- **Suggested direction** — Either give the type a card, or drop it from `requestTypes` and its validation branch. If the silent fallback stays, say why in a comment on `requestNew`.

---

## Not defects — checked and found correct

Recorded because each was a plausible failure the audit set out to find.

| # | Claim tested | Result |
|---|---|---|
| 1 | Two simultaneous reservations could both succeed, or 500 on `SQLITE_BUSY` | Neither, over five races (one through the UI, four through concurrent HTTP). The single conditional `UPDATE … WHERE status='approved' AND processing_by IS NULL AND on_hold=0` plus `busy_timeout=5000` yields exactly one 303 and one 409, and the audit trail records exactly one reservation each time. |
| 2 | Something expires a reservation | Nothing does. `ReleaseRequest` and `ReassignReservation` are the only writers of `processing_by` after the reserve, and the reminder scheduler only notifies (`internal/notify/reminders.go:38-48`). A reservation survived a reload, a lost race, a refused settlement, and a walk away and back. |
| 3 | The settlement preview writes something | It does not. The request's status, the absence of a payment outcome, and the ledger were identical before the preview, after it, and after abandoning the flow. |
| 4 | `on_hold=1` could coexist with a status other than `approved` | Not reachable from this flow: `HoldRequest`'s `WHERE status='approved' AND on_hold=0` refuses, and the hold tab's own `on_hold=1 AND status='approved'` is what proves the invariant from outside. |
| 5 | A partial-settlement decision could be taken by anybody holding `approval:accept_partial` | No — the store checks `before.ManagerID != actor.ID` in both `AcceptPartial` and `RaiseConcern`; the administrator, who holds every grant, is refused 403 on both and offered neither button. |
| 6 | A second payment could be written against one request | No. The status guard refuses before the unique index is reached, and the handler turns the refusal into a 303 to the payment that already exists — so a double confirm is neither an error nor a second row. |
| 7 | A refund or reversal path might exist unrouted | It does not: six refund-shaped paths answer 405 on POST and 404 on GET, and the payment detail says so in words. |
| 8 | The 390 px journey might have regressed | It has not. Reserve → preview → settle at exactly 390 px on both `chromium` and `mobile-chrome`, with no horizontal overflow on the queue, the entry screen, the sheet or the payment, and no console error, page error or 5xx. |
