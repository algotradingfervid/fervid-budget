# TC-B — Raising a payment request, and everything the requester can do to it

Executed test cases for **UC-B §1–§4 and §6** (`docs/qa/use-cases/UC-B-requests-approvals.md`),
restricted to the requester's half of the lifecycle: the adaptive form, the whole
`POST /requests` validation surface, numbering, attachments, comments, the
requester's own verbs, and the scope boundary around them. **The manager's
decision is out of scope** and belongs to `TC-C-approvals-cancellation.md`;
where a case needs an approved, returned or rejected request it puts the
manager's POST in directly rather than driving the approval screens.

| | |
|---|---|
| Spec | `tests/e2e/audit-b-request-lifecycle.spec.ts` |
| Findings | `docs/qa/results/findings-b-request-lifecycle.md` |
| Use cases | `docs/qa/use-cases/UC-B-requests-approvals.md` (UC-B-01…24, 33, 37–42) |
| Coverage matrix | `docs/superpowers/specs/2026-07-25-payment-requests-coverage.md` |
| Run command | `FERVID_E2E_PORT=4302 npx playwright test tests/e2e/audit-b-request-lifecycle.spec.ts --project=chromium --reporter=line` |
| Result | **99 declared · 98 executed · 84 PASS · 14 FAIL (product defect), each annotated `it.fail()` · 1 SKIPPED (`it.fixme()`, an intermittent race) · 0 BLOCKED** |

Every `expected result` below was written from `internal/store/requests.go`,
`internal/app/requests.go`, `internal/app/templates.go` and UC-B **before** the
suite was run. `actual result` and `verdict` were filled in afterwards.

---

## 0. Conventions

| ID | Convention |
|---|---|
| TCV1 | Statuses follow UC-B CV5: `ErrValidation`/`ErrDuplicate`→**400**, `ErrForbidden`→**403**, `ErrNotFound`→**404**, anything else→**500**. A successful `POST /requests` is **303 → /requests/{id}/submitted**. |
| TCV2 | A "refusal message" is the text of `div.alert.error` on the re-rendered form (`internal/app/templates.go:48`). It carries the sentinel prefix `validation failed: ` (UC-B DS4); the quoted expectations below name the tail only. |
| TCV3 | "the valid body for *type*" means the minimum accepted `POST /requests` body for that type from §1, with a run-unique `short_title`, `purpose` and `invoice_no`. A cell is *omitted* by deleting the key, not by sending an empty string, unless the case says otherwise. |
| TCV4 | Field labels are UC-B §0.2 (SV1–SV31), quoted character for character from the templates. The recoverable fieldset is addressed by id (`#rcategory`, `#expected-return`, `#rproject`, `#counterparty`, `#terms`) as SV4 permits; everything else uses `getByLabel`. |
| TCV5 | Subjects are built with `asRole` (`tests/e2e/audit-support.ts:116`) so each holds **exactly** the roles named — the Accounts role that `assignDefaultRoleTx` grants every new user is unticked first. |
| TCV6 | `capturePageErrors` is attached to every form-driven case; any `pageerror`, `console.error` or HTTP ≥ 500 during it fails the case. |
| TCV7 | Both htmx-driven selects (`#rcategory`, `#project`) live **inside** the fragment they replace. Every case that changes one twice waits for the server's `selected` **attribute** and for htmx to stop settling before touching it again — see F-B-12. |

### Preconditions

| ID | Precondition |
|---|---|
| P1 | The worker world: an exactly-**Requester** subject signed in (UC-B AC1); a second exactly-Requester subject (*the stranger*); two exactly-**Manager** subjects (AC2); a subject with **no role at all**; two active vendors and one inactive vendor; one head created **inactive** under Operations; the seeded Operations / People / Growth projects and their heads; an admin session (AC4). |
| P2 | P1, plus a `pending` request the Requester raised through `POST /requests`. |
| P3 | P2, plus the Manager has `POST /requests/{id}/return`ed it with a comment. |
| P4 | P2, plus the Manager has `POST /requests/{id}/approve`d it for the full amount. |
| P5 | P2, plus the Manager has `POST /requests/{id}/reject`ed it with a reason. |
| P6 | P1, plus `require_attachments` switched **on** through the real `/configuration` form (restored to off in the case's `finally`). |
| P7 | P1, plus an approved request that an exactly-**Accounts** subject has settled, with a proof uploaded on the resulting payment. |

---

## 1. The consolidated required / optional / forbidden / forced matrix

Derived from `validateRequestInput` (`internal/store/requests.go:127–247`),
`forcesRequesterPayee` (`:251`), `validateAttachmentPolicy` (`:533`) and
`Store.recoverableRules` (`internal/store/recoverables.go:119`). It agrees with
UC-B §1 (VA1–VA8, VT1–VT10, VC1–VC7, VG1–VG5); the right-hand column is the
case that exercises the cell.

### 1.1 Always, every type and treatment

| ID | Field | Rule | Refusal (tail) | Case |
|---|---|---|---|---|
| M1 | `amount` | `> 0` paise after `money.ParsePaise` | `enter the amount you are requesting` (handler) | TC-B-011…018 |
| M2 | `short_title` | non-blank after trim | `a short title is required` | TC-B-019 |
| M3 | `purpose` | non-blank after trim | `purpose is required` | TC-B-020 |
| M4 | `manager_id` | `> 0` | `choose an approver` | TC-B-021 |
| M5 | `manager_id ≠ requester_id` | G8, structural | `you cannot approve your own request` | TC-B-022 |
| M6 | `treatment` | `budget` \| `recoverable` | `treatment must be budget or recoverable` | TC-B-023 |
| M7 | `type` | one of the five in `requestTypes` | 400 *“That is not a kind of request this system raises.”* | TC-B-024 |
| M8 | `needed_by`, `expected_return_date`, `invoice_date`, `expense_date` | if present, `YYYY-MM-DD` | `<label> is invalid` | TC-B-025, TC-B-030 |

### 1.2 Per type × treatment

`req` required · `opt` optional, stored · `ign` read and **still written** but
never validated · `forced` overwritten whatever was posted · `—` the whole
combination refused before any field is read.

| ID | type | treatment | project | head | vendor | payee | invoice_no | invoice_date | expense_date | advance_reason | category | return date | terms | counterparty | Cases |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| MT1 | `vendor_invoice` | `budget` | req | req | req | ign | req | req | ign | ign | ign | ign | ign | ign | TC-B-001, 026–030, 055 |
| MT2 | `vendor_invoice` | `recoverable` | — | — | — | — | — | — | — | — | — | — | — | — | TC-B-048 |
| MT3 | `vendor_advance` | `budget` | req | req | req | ign | ign | ign | ign | req | ign | ign | ign | ign | TC-B-002, 031 |
| MT4 | `vendor_advance` | `recoverable` | — | — | — | — | — | — | — | — | — | — | — | — | TC-B-049 |
| MT5 | `reimbursement` | `budget` | req | req | **forced 0** | **forced = requester** | ign | ign | req | ign | ign | ign | ign | ign | TC-B-003, 032, 033, 040, 054, 092 |
| MT6 | `reimbursement` | `recoverable` | — | — | — | — | — | — | — | — | — | — | — | — | TC-B-050 |
| MT7 | `employee_advance` | `budget` | req | req | **forced 0** | **forced = requester** | ign | ign | ign | req | ign | ign | ign | ign | TC-B-005, 034, 053 |
| MT8 | `employee_advance` | `recoverable` | per category | opt | **forced 0** | **forced = requester** | ign | ign | ign | req | req | req | req | per category | TC-B-004, 034–036, 041, 052 |
| MT9 | `recoverable` | `recoverable` | per category | opt | opt | opt | ign | ign | ign | ign | req | req | req | per category | TC-B-006, 037–039, 043, 091 |
| MT10 | `recoverable` | `budget` | — | — | — | — | — | — | — | — | — | — | — | — | TC-B-051 |

Ordering note carried over from UC-B §1: for `employee_advance` the
`advance_reason` check runs **before** the treatment branch
(`internal/store/requests.go:233`), so a recoverable advance with no reason
reports *“say what the money is for”* and never the recoverable errors — the
reason TC-B-034 asserts that message on both treatments.

### 1.3 Recoverable category rules (rows of `recoverable_categories`)

| ID | code | requires project | requires counterparty | Case |
|---|---|---|---|---|
| MC1 | `employee_advance` | no | no | TC-B-004 |
| MC2 | `emd` | **yes** | no | TC-B-038, TC-B-008 |
| MC3 | `pbg` | **yes** | no | TC-B-008 |
| MC4 | `icd` | no | **yes** | TC-B-039, TC-B-008 |
| MC5 | `security_deposit` | no | **yes** | TC-B-008 |
| MC6 | `other` | no | no | TC-B-008 |
| MC7 | unknown code | — | — | TC-B-037 |

Every recoverable treatment additionally needs a known active category, a valid
`expected_return_date` and non-blank `repayment_notes`
(`internal/store/requests.go:173–191`) — TC-B-035, TC-B-036, TC-B-037.

### 1.4 Configuration-driven

| ID | Rule | Case |
|---|---|---|
| MG1 | `require_attachments=1` and no file ⇒ `attachment_exception_reason` non-blank; never a hard block (G10) | TC-B-083 |
| MG2 | Referential cells that are **not** enforced: an inactive head, an inactive vendor, a head from another project, a non-existent vendor or approver | TC-B-046, 056, 058, 060 |

---

## 2. Case index

`type`: F functional · P permission · N negative · B boundary · CC concurrency ·
IF information-flow · PA proof-of-absence · R regression.
`priority`: 1 highest.

| TC | Title | Traces to | type | pri | Verdict |
|---|---|---|---|---|---|
| TC-B-001 | A vendor invoice is raised on the budget treatment through the form | UC-B-01/02/05 · T1,T2,T6,L2,C3,C4 | F | 1 | PASS |
| TC-B-002 | A vendor advance is raised on the budget treatment through the form | UC-B-06 · T1,T2,T7,L2 | F | 1 | PASS |
| TC-B-003 | A reimbursement is raised and its payee is the requester | UC-B-07 · T8,T11,L2 | F | 1 | PASS |
| TC-B-004 | An employee advance opens recoverable and already categorised | UC-B-09 · T9,V1,V5,V6 | F | 1 | PASS |
| TC-B-005 | An employee advance switched to budget asks for project and head | UC-B-08 · T9,V1 | F | 1 | PASS |
| TC-B-006 | The `recoverable` type has no card and no form, yet the POST accepts it | UC-B-10 · T2,T9,V1,DV1 | PA | 2 | PASS |
| TC-B-007 | The treatment radio swaps `#form-fields`, and the fieldsets are alternatives | UC-B-03 · T1,T2 | F | 1 | PASS |
| TC-B-008 | The category select swaps the fieldset and keeps what was typed | UC-B-03 · T9,V5 | F | 2 | PASS |
| TC-B-009 | Choosing a project narrows the heads to that project's own | UC-B-02/03 · T12 | F | 2 | PASS |
| TC-B-010 | `GET /requests/new/fields` answers a bare fragment and needs `request:create` | UC-B-03 · T1,R6 | P | 2 | PASS |
| TC-B-011 | An absent amount is refused | UC-B-05 · VA1/M1 | N | 1 | PASS |
| TC-B-012 | An amount of zero is refused | UC-B-05 · VA1/M1 | B | 1 | PASS |
| TC-B-013 | A negative amount is refused | UC-B-05 · VA1/M1 | B | 1 | PASS |
| TC-B-014 | A whitespace-only amount is refused | UC-B-05 · VA1/M1 | B | 2 | PASS |
| TC-B-015 | A non-numeric amount is refused | UC-B-05 · VA1/M1 | B | 2 | PASS |
| TC-B-016 | An amount with grouping separators and a rupee sign is accepted | UC-B-05 · C3 | B | 1 | PASS |
| TC-B-017 | A very large amount that fits int64 paise round-trips exactly | UC-B-05 · C3 | B | 2 | PASS |
| TC-B-018 | An amount whose paise overflow int64 is refused | UC-B-05 · C3,VA1 | B | 1 | **FAIL (product defect) — F-B-01** |
| TC-B-019 | A blank short title is refused | UC-B-05 · VA2/M2 | N | 1 | PASS |
| TC-B-020 | A blank purpose is refused | UC-B-05 · VA3/M3 | N | 1 | PASS |
| TC-B-021 | No approver is refused | UC-B-05 · VA4/M4,A1 | N | 1 | PASS |
| TC-B-022 | Naming yourself as the approver is refused (G8) | UC-B-33 · VA5/M5 | P | 1 | PASS |
| TC-B-023 | A treatment that is neither budget nor recoverable is refused | UC-B-40 · VA6/M6 | N | 1 | PASS |
| TC-B-024 | An unknown request type is refused | UC-B-40 · VA7/M7 | N | 1 | PASS |
| TC-B-025 | A malformed needed-by date is refused | UC-B-05 · VA8/M8 | B | 2 | PASS |
| TC-B-026 | A vendor invoice without a project is refused | UC-B-05 · VT1/MT1 | N | 1 | PASS |
| TC-B-027 | A vendor invoice without a head is refused | UC-B-05 · VT1/MT1 | N | 1 | PASS |
| TC-B-028 | A vendor invoice without a vendor is refused | UC-B-05 · VT1/MT1,T6 | N | 1 | PASS |
| TC-B-029 | A vendor invoice without an invoice number is refused | UC-B-05 · VT1/MT1,T6 | N | 1 | PASS |
| TC-B-030 | A vendor invoice without a valid invoice date is refused | UC-B-05 · VT1/MT1,M8 | N | 1 | PASS |
| TC-B-031 | A vendor advance without a reason is refused | UC-B-06 · VT3/MT3,T7 | N | 1 | PASS |
| TC-B-032 | A reimbursement without an expense date is refused | UC-B-07 · VT5/MT5,T8 | N | 1 | PASS |
| TC-B-033 | A reimbursement without a project and head is refused | UC-B-07 · VT5/MT5 | N | 1 | PASS |
| TC-B-034 | An employee advance without a reason is refused on either treatment | UC-B-08/09 · VT7,VT8 | N | 1 | PASS |
| TC-B-035 | A recoverable advance without an expected return date is refused | UC-B-09 · VT8,V6 | N | 1 | PASS |
| TC-B-036 | A recoverable advance without repayment terms is refused | UC-B-09 · VT8,V6 | N | 1 | PASS |
| TC-B-037 | A recoverable with an unknown category is refused | UC-B-10 · VC7/MC7,V4 | N | 1 | PASS |
| TC-B-038 | An EMD recoverable without a project is refused | UC-B-10 · VC2/MC2,V5 | N | 1 | PASS |
| TC-B-039 | An ICD recoverable without a counterparty is refused, with one accepted | UC-B-10 · VC4/MC4,V5,V6 | N | 1 | PASS |
| TC-B-040 | A reimbursement payee is forced to the requester even when the POST names a vendor | UC-B-07/40 · T11,VT5 | P | 1 | PASS |
| TC-B-041 | An employee advance payee is forced to the requester | UC-B-09/40 · T11,VT8 | P | 1 | PASS |
| TC-B-042 | A needed-by date in the past is accepted | UC-B-05 · VA8/M8 | B | 3 | PASS |
| TC-B-043 | An expected return date before today is accepted | UC-B-10 · VA8/M8,V6 | B | 3 | PASS |
| TC-B-044 | A purpose at length round-trips | UC-B-05 · VA3/M3 | B | 3 | PASS |
| TC-B-045 | HTML and script in text fields render escaped, never executed | UC-B-16 · R6 | IF | 1 | PASS |
| TC-B-046 | An inactive head named in the POST is refused | UC-B-02 · T12,MG2 | N | 2 | **FAIL (product defect) — F-B-03** |
| TC-B-047 | A request keeps showing a head that was retired after it was raised | UC-B-05/23 · T12 | R | 2 | PASS |
| TC-B-048 | A vendor invoice posted as recoverable is refused | UC-B-40 · VT2/MT2,L11 | N | 1 | PASS |
| TC-B-049 | A vendor advance posted as recoverable is refused | UC-B-40 · VT4/MT4,L11 | N | 1 | PASS |
| TC-B-050 | A reimbursement posted as recoverable is refused | UC-B-40 · VT6/MT6,L11 | N | 1 | PASS |
| TC-B-051 | The recoverable type posted as a budget expense is refused | UC-B-40 · VT10/MT10,L11 | N | 1 | PASS |
| TC-B-052 | A recoverable employee advance carrying only the budget fieldset is refused | UC-B-40 · VT8/MT8,V5 | N | 1 | PASS |
| TC-B-053 | A budget employee advance carrying only the recoverable fieldset is refused | UC-B-40 · VT7/MT7 | N | 1 | PASS |
| TC-B-054 | A reimbursement posted with the whole vendor fieldset stores no vendor | UC-B-40 · VT5/MT5,T11 | N | 1 | PASS |
| TC-B-055 | A vendor invoice posted with the reimbursement fieldset is refused | UC-B-40 · VT1/MT1 | N | 1 | PASS |
| TC-B-056 | A head belonging to another project is refused | UC-B-40 · DV6,MG2 | N | 2 | **FAIL (product defect) — F-B-05** |
| TC-B-057 | Choosing a vendor writes the hidden id, and the visible text is never read | UC-B-11 · T6,T7 | F | 1 | PASS |
| TC-B-058 | A `vendor_id` naming no vendor is refused, not answered with a server error | UC-B-41 · DS1,MG2 | N | 1 | **FAIL (product defect) — F-B-06** |
| TC-B-059 | A `vendor_id` naming a real vendor the requester never searched for is accepted | UC-B-41 · T6 | N | 2 | PASS |
| TC-B-060 | A `vendor_id` naming an inactive vendor is refused | UC-B-41 · T6,MG2 | N | 2 | **FAIL (product defect) — F-B-07** |
| TC-B-061 | A Requester without `vendor:view` gets the plain select and cannot search | UC-B-11 · T6,R6 | P | 1 | PASS |
| TC-B-062 | Three raises take three consecutive numbers | UC-B-05/15 · C4,L2 | F | 1 | PASS |
| TC-B-063 | Two concurrent raises from two browsers both succeed, with consecutive numbers | UC-B-05 · C4,L2 | CC | 1 | **FAIL (product defect) — F-B-10**, intermittent · annotated `it.fixme()` |
| TC-B-064 | A refused submit consumes no number | UC-B-05 · C4 | B | 2 | PASS |
| TC-B-065 | The confirmation screen shows the number and who has it | UC-B-15 · L2,C4,N1 | F | 1 | PASS |
| TC-B-066 | No route creates, saves or lists a draft | UC-B-15 · L1,D5 | PA | 1 | PASS |
| TC-B-067 | A pending request cannot be submitted a second time | UC-B-15/19 · L1,L11 | PA | 1 | PASS |
| TC-B-068 | A returned request is corrected and resubmitted, keeping its number | UC-B-19 · Q1,L3,L11 | F | 1 | PASS |
| TC-B-069 | Editing while pending reroutes, re-notifies and resets the reminder clock | UC-B-18 · A8,N1,C2 | F | 1 | PASS |
| TC-B-070 | A pending request is withdrawn | UC-B-20 · Q2,L5,L11 | F | 1 | PASS |
| TC-B-071 | An approved request cannot be withdrawn | UC-B-37 · Q2,L6,L11 | N | 1 | PASS |
| TC-B-072 | A rejected request is re-raised as a new pending request | UC-B-21 · Q3,L4,C4 | F | 1 | PASS |
| TC-B-073 | A withdrawn request cannot be re-raised | UC-B-39 · Q3,L5,L11 | N | 1 | PASS |
| TC-B-074 | A rejected request cannot be edited | UC-B-38 · L4,Q1,L11 | N | 1 | PASS |
| TC-B-075 | Asking for cancellation of an approved request freezes payment | UC-B-22 · L6,L11 (G1) | F | 1 | PASS |
| TC-B-076 | Asking for cancellation of a pending request is refused | UC-B-22 · L11 | N | 1 | PASS |
| TC-B-077 | A stranger cannot edit, withdraw, re-raise or ask to cancel another request | UC-B-42 · Q5,R6 | P | 1 | PASS |
| TC-B-078 | The approver cannot edit the request they were sent | UC-B-18 · R6,A4 | P | 1 | PASS |
| TC-B-079 | A comment persists and everyone who may view the request sees it | UC-B-17 · N7,C2 | F | 1 | PASS |
| TC-B-080 | A stranger cannot comment on someone else's request | UC-B-42 · N7,R6 | P | 1 | PASS |
| TC-B-081 | An empty comment is refused | UC-B-17 · N7 | N | 2 | PASS |
| TC-B-082 | A document uploaded with the request lands on the request | UC-B-12 · T10,N7,C2 | F | 1 | PASS |
| TC-B-083 | `require_attachments` makes a document or a written reason mandatory | UC-B-13 · T10,VG3/MG1 | F | 1 | PASS |
| TC-B-084 | A requester can download the document on their own request | UC-B-12 · T10 | F | 1 | **FAIL (product defect) — F-B-09** |
| TC-B-085 | `/attachments/{id}` obeys the row scope its request obeys | UC-B-12/42 · R6,Q5 | IF | 1 | **FAIL (product defect) — F-B-11** |
| TC-B-086 | The list holds only the requester's own requests | UC-B-23 · Q5,R3,R6 | P | 1 | PASS |
| TC-B-087 | The CSV export is scoped, and `?scope=all` cannot widen it | UC-B-24 · D3,Q5,C3 | P | 1 | PASS |
| TC-B-088 | Another requester's request by direct id is refused | UC-B-42 · Q5,R6 | P | 1 | PASS |
| TC-B-089 | Anonymous and no-role callers are refused the form and the POST | UC-B-42 · R6,D2 | P | 1 | PASS |
| TC-B-090 | `POST /requests` without a valid CSRF token is refused | UC-B CV4 | P | 1 | PASS |
| TC-B-091 | A refused recoverable-type submit says why and keeps what was typed | UC-B-10 · DV1 | N | 2 | **FAIL (product defect) — F-B-02** |
| TC-B-092 | A field belonging to another type is neither stored nor shown | UC-B-40 · DS3 | N | 3 | **FAIL (product defect) — F-B-04** |
| TC-B-093 | A race never mints a duplicate number, whatever it does to the loser | UC-B-05 · C4 | CC | 1 | PASS |
| TC-B-094 | The vendor combobox on the edit screen can search | UC-B-11/18 · T6,T7 | F | 1 | **FAIL (product defect) — F-B-13** |
| TC-B-095 | A forged manager, project or head id is refused, not answered with a server error | UC-B-41 · DS1,MG2 | N | 1 | **FAIL (product defect) — F-B-06** |
| TC-B-096 | The recoverable fieldset posted on a budget request is neither stored nor shown | UC-B-40 · DS3,V1 | N | 2 | **FAIL (product defect) — F-B-15** |
| TC-B-097 | An admin-added recoverable category is enforced on submit and stored as itself | UC-B-09/10 · V4,V5 | F | 2 | PASS |
| TC-B-098 | An admin-added recoverable category is offered on the form and survives a swap | UC-B-03/09 · V4,DV3 | F | 2 | **FAIL (product defect) — F-B-17** |
| TC-B-099 | A list of more than 200 requests is never silently truncated | UC-B-23/24 · D3,D4,DS8 | B | 1 | **FAIL (product defect) — F-B-16** |

---

## 3. Case detail

### A · every type and treatment, through the real form

**TC-B-001** · P1 · Open `/requests/new`, click *Vendor invoice payment*, confirm no `type` control exists, leave the treatment on *Budget expense*, fill Short title, Project = Operations, Head = Operations / Office Rent, Vendor, Amount = 100000, Invoice number, Invoice date, Purpose, Approver; Submit request; open the request.
*Expected*: the chooser offers exactly 4 cards; `input[name=type]` = `vendor_invoice` and no `select[name=type]` exists (A16); choosing a project leaves only the placeholder among the non-Operations head options; `.in-words` says "lakh"; the submit lands on `/requests/{id}/submitted` with a `PR-YYYY-NNNNNN` number and the pill *Awaiting approval*; the detail shows the treatment pill *Budget expense*, a **Vendor** row naming the chosen vendor, and *Vendor invoice payment* in `.rh-meta`; no console error, no 5xx.
*Actual*: exactly as expected. · **PASS**

**TC-B-002** · P1 · Same flow for *Vendor advance*: assert the invoice fields are absent, fill Reason for the advance, submit, open the request.
*Expected*: `getByLabel('Invoice number')` resolves 0 elements; the submit succeeds; the advance reason appears on the detail; treatment pill *Budget expense*.
*Actual*: as expected. · **PASS**

**TC-B-003** · P1 · `/requests/new?type=reimbursement`: read `#paid-to`, then fill and submit.
*Expected*: `#paid-to` shows the requester's own name, carries **no `name` attribute** (so it posts nothing) and there is no vendor control at all; the detail's payee row is labelled **Paid to** and names the requester (T11).
*Actual*: as expected. · **PASS**

**TC-B-004** · P1 · `/requests/new?type=employee_advance`, fill the recoverable fieldset, submit.
*Expected*: the form opens with *Refundable or recoverable* checked and `#rcategory` = `employee_advance` (`internal/app/requests.go:89–92`); no `head_id` control exists, because the two fieldsets are alternatives; the detail's treatment pill reads *Recoverable · Employee advance* and the payee is the requester.
*Actual*: as expected. · **PASS**

**TC-B-005** · P1 · Same screen, check *Budget expense*, fill Project/Head, submit.
*Expected*: the swap replaces the recoverable fieldset with the budget one — `head_id` appears, `#rcategory` disappears; the detail's pill reads *Budget expense* and names Operations.
*Actual*: as expected. · **PASS**

**TC-B-006** · P1 · Look for a `recoverable` card on `/requests/new`; open `/requests/new?type=recoverable`; then `POST /requests` with the valid `recoverable` body.
*Expected*: no chooser card exists, and an unknown `?type=` falls back to the chooser heading *What are you asking to be paid?* (`internal/app/requests.go:74–77`); the POST nevertheless **succeeds**, because `store.requestTypes` carries five types and `requestTypeOptions` only four (UC-B DV1); the created request shows *Recoverable · EMD — earnest money deposit* and `.rh-meta` prints the raw enum `recoverable`, since `typeLabel` has no label for it.
*Actual*: as expected, including the raw enum on screen. Recorded as F-B-02. · **PASS**

**TC-B-007** · P1 · On the vendor-invoice form, toggle the treatment radio recoverable → budget, counting `GET /requests/new/fields` requests.
*Expected*: each toggle asks the server for the fieldset; recoverable renders `repayment_notes` and **no** `head_id`; budget renders `head_id` and no `repayment_notes`; the first swap URL carries `treatment=recoverable`. A hidden-not-removed head would post a stale `head_id`, which is why the count of `head_id` controls must be 0 and not merely invisible.
*Actual*: 2 swaps, both fieldsets exclusive. · **PASS**

**TC-B-008** · P1 · On the employee-advance form type into `#terms` and `#expected-return`, then pick `emd`, `icd`, `security_deposit`, `other` in turn, waiting each time for the server's `selected` attribute and for htmx to settle (TCV7).
*Expected*: `emd` reveals `#rproject` and no counterparty (MC2); `icd` and `security_deposit` reveal `#counterparty` and no project (MC4, MC5); `other` reveals neither (MC6); what was typed survives every swap.
*Actual*: as expected once the case waits for the applied swap. The first draft polled only the click and was flaky 1-in-3 — see F-B-12. · **PASS**

**TC-B-009** · P1 · Count the head options, choose People, then Operations.
*Expected*: every head is offered until a project is chosen; after People only the placeholder is not a `People /` head and no `Operations /` head remains; after Operations the reverse.
*Actual*: as expected. · **PASS**

**TC-B-010** · P1 · `GET /requests/new/fields?type=employee_advance&treatment=recoverable&recoverable_category=icd` as the Requester, then as the no-role subject.
*Expected*: 200, a bare partial (no `<!doctype html>`), containing `name="counterparty"` and **not** `name="head_id"`; the no-role caller gets 403 because the route is gated on `request:create`.
*Actual*: as expected. · **PASS**

### B · the required, forced and optional matrix

Unless stated, each case POSTs the valid body for its type with exactly one cell changed, and asserts **400** plus the refusal tail from §1.

| TC | Change | Expected refusal | Actual | Verdict |
|---|---|---|---|---|
| TC-B-011 | `amount` omitted | 400 · `enter the amount you are requesting` | as expected | PASS |
| TC-B-012 | `amount=0` | 400 · same (ParsePaise rejects non-positive) | as expected | PASS |
| TC-B-013 | `amount=-500` | 400 · same | as expected | PASS |
| TC-B-014 | `amount="   "` | 400 · same | as expected | PASS |
| TC-B-015 | `amount="one hundred"` | 400 · same | as expected | PASS |
| TC-B-019 | `short_title="   "` | 400 · `a short title is required` | as expected | PASS |
| TC-B-020 | `purpose=" \n "` | 400 · `purpose is required` | as expected | PASS |
| TC-B-021 | `manager_id` omitted | 400 · `choose an approver` | as expected | PASS |
| TC-B-023 | `treatment=capex` | 400 · `treatment must be budget or recoverable` | as expected | PASS |
| TC-B-025 | `needed_by=31/07/2026` | 400 · `required-by date is invalid` | as expected | PASS |
| TC-B-026 | `project_id` omitted | 400 · `project and head are required` | as expected | PASS |
| TC-B-027 | `head_id` omitted | 400 · same | as expected | PASS |
| TC-B-028 | `vendor_id` omitted | 400 · `choose a vendor from the vendor master` | as expected | PASS |
| TC-B-029 | `invoice_no="  "` | 400 · `the invoice number is required` | as expected | PASS |
| TC-B-031 | `advance_reason=" "` | 400 · `say what the advance is for` | as expected | PASS |
| TC-B-032 | `expense_date` omitted | 400 · `the expense date is required` | as expected | PASS |
| TC-B-033 | `project_id`+`head_id` omitted | 400 · `project and head are required` | as expected | PASS |
| TC-B-035 | `expected_return_date` omitted | 400 · `expected return date is required for recoverables` | as expected | PASS |
| TC-B-036 | `repayment_notes="   "` | 400 · `repayment or refund terms are required` | as expected | PASS |

**TC-B-016** · P1 · `amount=₹ 1,00,000.50`.
*Expected*: accepted — `money.ParsePaise` strips the symbol and the Indian grouping — and stored as 10000050 paise, rendered `₹1,00,000.50` with **exactly one** `₹` (C3; `money.FormatPaise` already carries it).
*Actual*: as expected; one symbol. · **PASS**

**TC-B-017** · P1 · `amount=99999999999`.
*Expected*: accepted and rendered `₹99,99,99,99,999.00` — 9 999 999 999 900 paise, well inside int64.
*Actual*: as expected. · **PASS**

**TC-B-018** · P1 · `amount=1e300`.
*Expected*: refused with 400. `money.ParsePaise` documents that *“non-numeric, empty and non-positive amounts are rejected”* and guards `IsNaN`/`IsInf`, so a value that cannot be represented in int64 paise must not become one.
*Actual*: **303 → /requests/{id}/submitted.** The request was created for `₹92,23,37,20,36,85,47,758.07` — int64 max — because the float-to-int64 conversion saturates on this platform and the `paise <= 0` guard never fires. Reproduced with curl: `9e18`, `1e19` and `1e300` all store the identical clamped value. · **FAIL (product defect) — F-B-01**, annotated `it.fail()`.

**TC-B-022** · P1 · Read the approver `<select>` on the form, then POST with `manager_id` = the requester's own id.
*Expected*: the requester's own option is structurally absent (`ListApprovers` excludes them, `internal/store/requests.go:551`); the POST is refused 400 · `you cannot approve your own request` (G8, VA5).
*Actual*: as expected. · **PASS**

**TC-B-024** · P1 · `type=bribe`.
*Expected*: 400. `validateRequestInput` rejects the type, and `renderRejectedRequestForm` cannot find a label for it, so the answer is the error page *“That is not a kind of request this system raises.”*
*Actual*: as expected. · **PASS**

**TC-B-030** · P1 · `invoice_date` omitted, then `invoice_date=2026-13-45`.
*Expected*: 400 · `the invoice date is required`, then 400 · `invoice date is invalid` — the format check (M8) runs before the per-type check.
*Actual*: as expected. · **PASS**

**TC-B-034** · P1 · `advance_reason` omitted, once on the recoverable treatment and once on budget.
*Expected*: both 400 · `say what the money is for`. The reason check precedes the treatment branch, so the recoverable case never reports a recoverable error.
*Actual*: as expected. · **PASS**

**TC-B-037** · P1 · `recoverable_category=slush_fund`.
*Expected*: 400 — the code is not a row of `recoverable_categories`. (The *message* the requester gets is F-B-02, proved separately by TC-B-091.)
*Actual*: 400. · **PASS**

**TC-B-038** · P1 · `emd` with `project_id` omitted. *Expected*: 400 (MC2). *Actual*: 400. · **PASS**

**TC-B-039** · P1 · `icd` with no counterparty, then the same with one.
*Expected*: 400 first (MC4); then accepted, and the counterparty appears on the detail (V6).
*Actual*: as expected. · **PASS**

**TC-B-040** · P1 · A reimbursement POSTed with `vendor_id` = a real vendor **and** `vendor_payee=Cayman Holdings Ltd`.
*Expected*: accepted, but `CreateRequest` clears `VendorID` and overwrites `VendorPayee` with the actor's name (`internal/store/requests.go:367–370`), so the detail shows a **Paid to** row naming the requester and neither the vendor nor the posted payee anywhere.
*Actual*: as expected — both forged values discarded. · **PASS**

**TC-B-041** · P1 · The same for an employee advance, with another user's name as `vendor_payee`. *Expected/Actual*: forced to the requester. · **PASS**

**TC-B-042** · P1 · `needed_by=2019-01-01`.
*Expected*: **accepted**. `validateRequestInput` checks the format of `needed_by` and nothing else — there is no past-date rule anywhere in the store. The detail renders *1 January 2019*.
*Actual*: as expected. Recorded as informational F-B-14. · **PASS**

**TC-B-043** · P1 · `expected_return_date=2019-06-30` on a recoverable.
*Expected*: accepted for the same reason; the detail renders *30 June 2019*.
*Actual*: as expected. · **PASS**

**TC-B-044** · P1 · A purpose of ~19 000 characters bracketed by `Head.` and `Tail.`.
*Expected*: accepted (no length rule; the body cap is 21 MiB) and rendered whole — the `dd` starts `Head.` and ends `Tail.`
*Actual*: as expected. · **PASS**

**TC-B-045** · P1 · `short_title` = `<script>window.__pwned=1</script><b>bold</b> …`, `purpose` = `<img src=x onerror="window.__pwned=2"> and <iframe src="/"></iframe>`; open the detail with `capturePageErrors`.
*Expected*: `h1` contains the markup as **text**; zero `h1 script`/`h1 b` elements; zero `main img`/`main iframe`; `window.__pwned` undefined; no console error. `html/template` escapes every interpolation.
*Actual*: as expected — nothing parsed, nothing ran. · **PASS**

**TC-B-046** · P1 · POST a vendor invoice naming the head that was created inactive.
*Expected*: 400. The form never offers it (`ListHeads(ctx, true)` is active-only), and T12 says an inactive head belongs to history, not to a new request.
*Actual*: **303 — accepted.** `needsProjectHead` checks only that both ids are positive; the foreign key is satisfied because the row exists. Reproduced with curl on a clean database. · **FAIL (product defect) — F-B-03**, annotated `it.fail()`.

**TC-B-047** · P1 · Create an **active** head under Operations, raise a request against it, retire the head through `POST /heads`, reload the request and the new-request form.
*Expected*: the request still names the retired head (the detail reads it through a join that ignores `active`); the new-request form no longer offers it, nor the head that was never active (T12).
*Actual*: as expected. · **PASS**

### C · hidden is not validation

The browser cannot send any of these bodies — the budget and recoverable
fieldsets are alternatives, and the type-specific fieldsets are rendered only for
their own type. Each case is therefore a hand-rolled POST, and each proves the
server re-decides (`internal/app/requests.go:298` reads every field
unconditionally; the store is the only gate).

| TC | Body | Expected | Actual | Verdict |
|---|---|---|---|---|
| TC-B-048 | `vendor_invoice` + `treatment=recoverable` + the whole recoverable fieldset | 400 · `a vendor invoice is a budget expense` | as expected | PASS |
| TC-B-049 | `vendor_advance` + recoverable | 400 · `a vendor advance is a budget expense` | as expected | PASS |
| TC-B-050 | `reimbursement` + recoverable | 400 · `reimbursement is a budget expense` | as expected | PASS |
| TC-B-051 | `recoverable` + `treatment=budget` + `head_id` | 400 (rule fires; message is F-B-02) | 400 | PASS |
| TC-B-052 | recoverable `employee_advance` with the **budget** fieldset only | 400 · `choose a recoverable category` | as expected | PASS |
| TC-B-053 | budget `employee_advance` with the **recoverable** fieldset only | 400 · `project and head are required` | as expected | PASS |
| TC-B-055 | `vendor_invoice` with the **reimbursement** fieldset (`expense_date`, no vendor, no invoice) | 400 · `choose a vendor from the vendor master` | as expected | PASS |

**TC-B-054** · P1 · A reimbursement POSTed with `vendor_id`, `invoice_no`, `invoice_date` and `advance_reason` — the entire vendor fieldset it never renders.
*Expected*: accepted, with the vendor dropped, the payee forced to the requester, and no **Vendor** row on the detail (MT5).
*Actual*: as expected. What it does with the remaining foreign fields is TC-B-092. · **PASS**

**TC-B-056** · P1 · `project_id` = Operations, `head_id` = People / Payroll.
*Expected*: 400. The form only ever offers the chosen project's own heads, so a mismatched pair can only be forged.
*Actual*: **303 — accepted**, and the detail then renders *Project: People · Head: Office Rent* for the curl reproduction of the same shape: a request that claims one project and is charged to another project's head. Confirms UC-B DV6 with an observable consequence. · **FAIL (product defect) — F-B-05**, annotated `it.fail()`.

### D · the vendor control

**TC-B-057** · P1 · As the admin (who holds `vendor:view`, so the real combobox renders): type part of the vendor name, click the option, read `#vendor-id`. Then as the Requester POST `vendor_id` = the real id with `q=Some Other Company`.
*Expected*: the option click writes the vendor's id into `#vendor-id`; the visible box is `name="q"` — a search term, never the payee; the POST stores the vendor the **id** names and nothing the text said.
*Actual*: as expected. · **PASS**

**TC-B-058** · P1 · `vendor_id=99999999`.
*Expected*: 400 with a message. `needsVendor` is the only vendor rule, so the id reaches the INSERT; a well-behaved system turns the referential failure into a validation refusal.
*Actual*: **500**, error page, whole form lost. The log says `constraint failed: FOREIGN KEY constraint failed (787)`; `classify` (`internal/store/store.go:1712`) maps only `UNIQUE`, so everything else falls through to 500. Reproduced with curl for `manager_id=99999` too. Confirms UC-B DS1. · **FAIL (product defect) — F-B-06**, annotated `it.fail()`.

**TC-B-059** · P1 · `vendor_id` = a real active vendor the requester never searched for.
*Expected*: **accepted**. The code guarantees existence, never selection: there is no per-requester vendor visibility and no CSRF-bound choice token. Asserted as the contract, not as a defect.
*Actual*: accepted; the detail names that vendor. · **PASS**

**TC-B-060** · P1 · `vendor_id` = the inactive vendor.
*Expected*: 400 — `vendorChoices` offers active vendors only, so an inactive payee is out of the product's own vocabulary.
*Actual*: **303 — accepted**; the payee renders normally. Reproduced with curl. · **FAIL (product defect) — F-B-07**, annotated `it.fail()`.

**TC-B-061** · P1 · `GET /vendors/search` as the Requester; then raise a full vendor invoice on their degraded form.
*Expected*: 403 on the search route (it is gated on `vendor:view`, which Requester does not hold); the form gives a plain `<select id="vendor" name="vendor_id">` listing active vendors and **no** `.combo-input`; the inactive vendor is not among the options; the request submits successfully. This is the documented degradation at `internal/app/requests.go:284` and `internal/app/templates.go:1896`.
*Actual*: as expected — the degraded path is fully usable. · **PASS**

### E · numbering

**TC-B-062** · P1 · Raise three requests, read each number.
*Expected*: all match `PR-YYYY-NNNNNN`, the year segment is the calendar year (`number_year_mode=calendar`), the three are distinct, and each sequence is exactly one more than the last.
*Actual*: as expected. · **PASS**

**TC-B-063** · P1 · `Promise.all` of one `POST /requests` from the Requester's browser and one from the stranger's.
*Expected*: both succeed with two consecutive numbers. `NextRequestNumber` reserves inside the caller's transaction, and `busy_timeout(5000)` is set on every pooled connection (`internal/store/store.go:36`) precisely so concurrent writers wait their turn.
*Actual*: one **500** `database is locked (5) (SQLITE_BUSY)` — on 5 of 6 full runs and 3 of 3 isolated runs, but not on every run, so the case is annotated `it.fixme()` rather than `it.fail()`: a run where the race is won would turn a `test.fail()` red with "expected to fail, but passed". `CreateRequest` opens a **deferred** transaction and reads (`requestNumberYear`, `internal/store/requests.go:406`) before it writes, and SQLite refuses that upgrade without ever calling the busy handler — which is why `ReserveRequest`, whose first statement is the UPDATE (`internal/store/store.go:736`), is safe and this is not. Reproduced a second way with a parallel curl burst. · **FAIL (product defect) — F-B-10**, annotated `it.fail()`.

**TC-B-064** · P1 · Read a baseline number; submit one body that fails validation and one that fails referentially (`vendor_id=99999999`); raise a valid one and read its number.
*Expected*: the sequence advances by exactly 1 across the whole case — validation runs before the transaction opens, and the referential failure rolls back the transaction that reserved the number.
*Actual*: exactly 1. No number is burned even by the 500. · **PASS**

**TC-B-093** · P1 · Three concurrent raises from two browsers; collect whatever succeeded.
*Expected*: at least one succeeds; every refusal is ≥ 400 (never a redirect with nothing behind it); every number that was handed out is unique. The integrity half of the contract must hold even while the availability half (F-B-10) does not.
*Actual*: as expected — no duplicate number under any race observed. · **PASS**

### F · the single-POST contract (decision D1)

**TC-B-065** · P2 · Open `/requests/{id}/submitted`.
*Expected*: the number, the pill *Awaiting approval*, the good banner naming the approver, and the waiting line naming the approver.
*Actual*: as expected. · **PASS**

**TC-B-066** · P2 · POST `/requests/draft`, `/requests/new/draft`, `/requests/1/submit`, `/requests/1/save-draft`, `/requests/1/copy`; count the submit buttons on the form; `GET /requests?bucket=draft`.
*Expected*: every unrouted POST answers 404 **or 405** (`routes()` registers a catch-all `GET /`, so the path matches and the method does not); the form has exactly one submit; an unknown bucket is a filter, not an error, and lists nothing. `draft` is unrepresentable — `payment_requests.status` carries `CHECK (status <> 'draft')` (`internal/store/migrations.go:155`). Also covers D5 (`/copy` is not built).
*Actual*: as expected. · **PASS**

**TC-B-067** · P2 · `POST /requests/{id}/edit` with `submit_action=resubmit` on a **pending** request.
*Expected*: 400 · `a pending request cannot be submitted`. `SubmitRequest` is the only remaining edge into `pending` and `canTransition("pending","pending")` is false, so there is no second submit.
*Actual*: as expected. · **PASS**

### G · what the requester may still do

**TC-B-068** · P3 · Open the request, correct the Purpose, press *Resubmit for approval*.
*Expected*: the returned status shows the manager's words in a warn banner; the same URL serves a correction form rather than the read-only detail (`showsReturnedCorrection`); after resubmitting the status is *Awaiting approval*, **the number is unchanged**, and the new purpose is on the detail.
*Actual*: as expected. · **PASS**

**TC-B-069** · P2 · Confirm the request is in Manager A's `/approvals`; open `/requests/{id}/edit`, change the Amount and the Approver to Manager B, *Save and notify*; then check both queues, Manager B's `/notifications`, the thread and the audit snapshot.
*Expected*: the info banner promises history + re-notify + reminder reset; the thread's `.tl-change` names **Amount** and **Approver**; the request leaves A's queue and enters B's (reroute); B's notification centre carries the number and *edited and re-sent* (`request_edited`, `IncludeManager`, in-app always fires — G19); and the `update` audit row's *after* snapshot carries `"ReminderLastSent": null` (`reminder_last_sent=NULL`, `internal/store/requests.go:622`). Covers A8 end to end.
*Actual*: all four held. · **PASS**

**TC-B-070** · P2 · Press *Withdraw* on the detail.
*Expected*: status *Withdrawn*, waiting line *Withdrawn by the requester*, and the control is gone afterwards.
*Actual*: as expected. · **PASS**

**TC-B-071** · P4 · Read the action bar, then POST `/requests/{id}/withdraw` anyway.
*Expected*: the locked banner is shown, no *Withdraw* control is rendered, and the store refuses the POST with 400 · `approved request cannot be withdrawn` — an approved request is cancelled by asking, never withdrawn (G1).
*Actual*: as expected. The message reads *“a approved request cannot be withdrawn”* — the article is wrong, recorded as F-B-08. · **PASS**

**TC-B-072** · P5 · Read the rejected banner, press *Raise it again*, then re-read the source.
*Expected*: the reject reason is in a bad banner; the re-raise lands on the **new** request's confirmation with a different id and a strictly later number (D1: a copy, not a reopening); the source stays rejected and final.
*Actual*: as expected. · **PASS**

**TC-B-073** · P2 → withdrawn · Confirm no *Raise it again* control, then POST `/requests/{id}/reraise`.
*Expected*: 400 · `only a rejected request can be re-raised`. `ReraiseRequest` requires `status == "rejected"` exactly — withdrawn is not re-raisable, which is what the code actually allows.
*Actual*: as expected. · **PASS**

**TC-B-074** · P5 · Read the action bar; `GET /requests/{id}/edit`; `POST /requests/{id}/edit`.
*Expected*: no *Edit request* link; the GET answers **400** with *“A rejected request is final”* (`loadEditableRequest` refuses the status before the store would); the POST also 400.
*Actual*: as expected. · **PASS**

**TC-B-075** · P4 · *Request cancellation* → fill Reason → *Send cancellation request*; then POST a blank reason, then ask again.
*Expected*: the cancel screen warns *Payment freezes the moment you ask*; after sending, the pill is `cancelreq` and a warn banner says *Payment is frozen* and quotes the reason; a blank reason is 400; a second ask is 400 (`cancellation_requested` has no edge to itself).
*Actual*: as expected. · **PASS**

**TC-B-076** · P2 · Read the action bar; `GET /requests/{id}/cancel`; POST `cancel-request`.
*Expected*: no link; the GET answers 400 with *“withdraw it instead”* (`requestCancelForm` refuses any status but `approved`); the POST answers 400 · `cannot be sent for cancellation`.
*Actual*: as expected. · **PASS**

**TC-B-077** · P2 · As the second exactly-Requester subject: GET the request, its edit and cancel screens; POST edit, withdraw, reraise, cancel-request.
*Expected*: **403 on all seven** — the Requester scope is `own`, so `canViewRequest` refuses the row before any verb is considered (Q5/R6); and the request is still pending afterwards.
*Actual*: 403 on all seven; nothing changed. · **PASS**

**TC-B-078** · P2 · As the Manager the request was sent to: read the detail, then GET and POST the edit route.
*Expected*: the waiting line reads *Waiting on you* but there is no *Edit request* link, and both edit calls are 403 — a Manager holds `request:view`/`comment` and no `request:edit` at all; an approver who wants a change returns the request.
*Actual*: as expected. · **PASS**

### H · the conversation

**TC-B-079** · P2 · Post a comment as the requester; read it as the manager.
*Expected*: the comment lands on the thread as `li.is-comment.is-me` for its author, naming them; the manager sees the same body but not marked as theirs; the section head promises *Everyone who can see this request sees this whole stream* (N7).
*Actual*: as expected. · **PASS**

**TC-B-080** · P2 · `POST /requests/{id}/comment` as the stranger.
*Expected*: 403 — `requestComment` calls `loadViewableRequest` first — and the body never appears on the thread.
*Actual*: as expected. · **PASS**

**TC-B-081** · P2 · `body="   "`.
*Expected*: 400 · `comment cannot be empty`.
*Actual*: as expected. · **PASS**

### I · documents

**TC-B-082** · P1 · Raise a reimbursement with a PDF chosen in the uploader; open the request.
*Expected*: the attachment appears as a `.file-row` naming the file with a `PDF` badge, and it joins the merged thread (`RequestThread` folds attachments in). One POST carries the row, its number and the document (D1).
*Actual*: as expected. · **PASS**

**TC-B-083** · P6 · With `require_attachments` on: POST with neither file nor reason; POST with a reason; read the form; raise one with a file. Restore the setting in `finally`.
*Expected*: the bare POST is 400 · `attach a supporting document, or say why you cannot`; a written reason is accepted (G10 — it never hard-blocks) and the detail carries the info banner *No document was attached* quoting it; the form shows the exception field and drops the *optional* badge; a real file is accepted with no reason. The setting is driven through the real `/configuration` form so no other setting is blanked (`configurationSave` writes every registered toggle).
*Actual*: all four held; the setting was restored. · **PASS**

**TC-B-084** · P1 · Raise a request with a document, read the Download link the detail renders, follow it as the owner.
*Expected*: 200, serving the requester's own file. The screen offers the link, so the link must work.
*Actual*: **404.** `request_detail` links a `request_attachments` row id to `GET /attachments/{id}`, and that route reads `payment_attachments` (`internal/store/store.go:1355`) — a different table with a sequence of its own. Every request attachment is therefore undownloadable, and worse, an id that happens to exist in the payment table serves an **unrelated payment's file**. · **FAIL (product defect) — F-B-09**, annotated `it.fail()`.

**TC-B-085** · P7 · Confirm the Requester is refused the request (403) and the payment (403) behind an Accounts-uploaded proof, then `GET` the proof's `/attachments/{id}`.
*Expected*: not 200. `payment_detail` justifies its own request-scope check with *“or payment:view becomes a way around Q5/R6”*; the same reasoning must hold for the file on it.
*Actual*: **200 — the file is served.** `attachmentDownload` (`internal/app/app.go:881`) checks only that the stored path is inside the attachment directory and that the file exists; it never asks who is calling. `attachment:view`, which the seeded **Requester** role holds, is enough to read the bank advice on any payment in the system by walking ids. · **FAIL (product defect) — F-B-11**, annotated `it.fail()`.

### J · scope, and the doors that must stay shut

**TC-B-086** · P1 · Raise one request as each Requester; open `/requests?bucket=all`, then `&scope=all`.
*Expected*: the heading reads *My requests*, the list holds the caller's own number and not the other's; `?scope=all` cannot widen a narrow scope (`effectiveScope` only narrows).
*Actual*: as expected. · **PASS**

**TC-B-087** · P1 · `GET /requests/export.csv?bucket=all&scope=all` as the Requester.
*Expected*: 200 (a Requester holds `request:view`, so the export is theirs — D3); the header row names the columns; their own number is present and the other requester's absent; money carries exactly one `₹` per cell and never `₹₹` (C3).
*Actual*: as expected. · **PASS**

**TC-B-088** · P1 · The stranger's request id on `/requests/{id}`, `/submitted`, `/edit`, `/cancel`; then a non-existent id.
*Expected*: 403 on all four; **404** for the missing id. The two must not be swapped — though the distinction itself leaks existence (UC-B DS14).
*Actual*: as expected. · **PASS**

**TC-B-089** · P1 · Anonymous `GET /requests`, `/requests/new`, `/requests/new?type=…`; then the no-role subject on the same and on `POST /requests`; then the Manager on the form and the POST.
*Expected*: anonymous gets a 3xx to `/login` (**303**, not 302); the no-role subject 403 on all three; the Manager 403 on both, holding no `request:create`.
*Actual*: as expected. · **PASS**

**TC-B-090** · P1 · `POST /requests` with the `csrf` field omitted, then forged; then a forged token on withdraw, comment and edit.
*Expected*: 403 every time, with *“form session expired”* — `withCSRF` refuses a missing **or** wrong token (CV4).
*Actual*: as expected. · **PASS**

### Annotated defect cases

**TC-B-091** · P1 · POST the `recoverable` body with `repayment_notes` omitted.
*Expected*: 400, a message naming the rule (`repayment or refund terms are required`), and the form re-rendered with what was typed still in it — the promise `renderRejectedRequestForm` exists to keep.
*Actual*: 400 with *“That is not a kind of request this system raises.”* and the whole form gone. `renderRejectedRequestForm` looks the type up in `requestTypeLabels`, which has no `recoverable` entry, so **every** refusal of that type is misreported. · **FAIL (product defect) — F-B-02**, annotated `it.fail()`.

**TC-B-092** · P1 · A reimbursement POSTed with `invoice_no`, `invoice_date` and `advance_reason`.
*Expected*: those columns are neither stored nor displayed — a reimbursement has no invoice.
*Actual*: both the invoice number and the advance reason are stored verbatim and printed on the detail's `<dl>`. `CreateRequest` writes every column it is handed and `request_detail` prints any non-empty one. Extends UC-B DS3 from the recoverable columns to the invoice and advance columns. · **FAIL (product defect) — F-B-04**, annotated `it.fail()`.

### Cases added to confirm or refute sibling-audit claims

**TC-B-094** · P2 (raised by the admin, who holds `vendor:view`) · Open `/requests/{id}/edit`, read the vendor control, type part of the vendor's name into it.
*Expected*: the edit screen renders the combobox for a caller with `vendor:view`; its input carries `name="q"`, because htmx sends the triggering input's own name/value pair with `hx-get`; typing a vendor name offers that vendor as a `#vendor-options .co[data-id]` option to click.
*Actual*: the combobox renders, but its input has **no `name` at all** (`internal/app/templates.go:2486`, against `:1902` on the create form). htmx therefore asks `GET /vendors/search` with no `q`, `SearchVendors("")` returns nil (`internal/store/vendors.go:337`), and the fragment renders only its *“Type a name, GSTIN or city”* placeholder — for every keystroke. The vendor on a pending request cannot be changed. Confirmed a third way over HTTP: `GET /vendors/search` with no query returns exactly that placeholder. · **FAIL (product defect) — F-B-13**, annotated `it.fail()`.

**TC-B-095** · P1 · POST the valid vendor-invoice body with `manager_id`, then `project_id`, then `head_id` set to `99999999`.
*Expected*: 400 each time. These ids are submitted values; a referential failure caused by a submitted value is a validation failure (CV5).
*Actual*: **500** for all three, exactly as for `vendor_id` (TC-B-058). The log carries `constraint failed: FOREIGN KEY constraint failed (787)` once per attempt. `validateRequestInput` checks only that each id is positive, and `classify` maps only `UNIQUE`. · **FAIL (product defect) — F-B-06**, annotated `it.fail()`.

**TC-B-096** · P1 · POST a **budget** `vendor_invoice` carrying `counterparty`, `expected_return_date` and `repayment_notes` — the whole recoverable fieldset the browser can never send alongside the budget one.
*Expected*: accepted (the treatment rules are satisfied) but with those three columns neither stored nor displayed — the treatment decides which fields the request has.
*Actual*: all three are stored and the detail renders **Counterparty**, **Expected return** and **Repayment terms** under a pill reading *Budget expense*. `CreateRequest` writes every column it is handed (`internal/store/requests.go:414–427`) and `request_detail` prints each non-empty one (`internal/app/templates.go:2355–2357`). Reproduced with curl on a clean database. Confirms a sibling audit's DS3. · **FAIL (product defect) — F-B-15**, annotated `it.fail()`.

**TC-B-097** · P1 · As the admin, add a recoverable category *Retention deposit …* requiring a counterparty; confirm it is on `/configuration`; then POST a `recoverable` request with its code and no counterparty, and again with one.
*Expected*: V4's promise — the rule the admin chose is enforced from the table with no code change. The first POST is refused 400; the second is accepted and the request carries **that** category, not a substituted one.
*Actual*: exactly as expected. `requestCreate` reads `recoverable_category` straight off the form, so the rewriting `normalizeRecoverableCategory` does is never reached on the submit path — **this refutes the "silently filed under EMD" half of the sibling claim.** The detail's pill renders the raw code (*Recoverable · retention_deposit_…*), because `recoverableCategoryLabels` is a hardcoded map. · **PASS**

**TC-B-098** · P1 · Add another category; read the request form's `#rcategory` options; then ask the htmx fragment for that code.
*Expected*: the form offers every active category, and the fragment keeps the category it was given and reveals the field its rule requires.
*Actual*: the select still offers **exactly the six seeded codes** (`internal/app/templates.go:1738–1743`), so the new category can never be picked; and the fragment answers with `employee_advance` selected and no `name="counterparty"` — `normalizeRecoverableCategory` (`internal/app/requests.go:132`) silently substituted both the category and the reveal its rule depends on. Confirms a sibling audit's DV3, narrowed to the UI. · **FAIL (product defect) — F-B-17**, annotated `it.fail()`.

**TC-B-099** · P1, plus a dedicated Requester subject so 205 rows disturb no other case · Raise 205 requests; read the **All** tab count against the rendered card count; then take the CSV export.
*Expected*: the list renders every row the tab beside it counts — the counts are computed from the same options as the rows *“so a tab can never promise a number the list does not show”* (`internal/app/requests.go:482`) — and the export carries every row it counted, or says that it did not.
*Actual*: the tab reads **214**, the list renders **200** cards, the sub-line reads *“200 shown”*, and the CSV carries **200** data rows. `ListRequests` defaults `Limit` to 200 (`internal/store/requests.go:1272`) and the export takes it (`internal/app/requests.go:928`), while `CountRequests` has no limit at all. There is no pagination control anywhere on the page. Reproduced with curl. Confirms and extends a sibling audit's DS8 — the silently-truncated **export** is the serious half. · **FAIL (product defect) — F-B-16**, annotated `it.fail()`.

---

## 4. Result summary

| | Count |
|---|---|
| Declared | **99** |
| Executed | **98** |
| PASS | 84 |
| FAIL (product defect), annotated `it.fail()` | 14 |
| SKIPPED, annotated `it.fixme()` — an intermittent race | 1 (TC-B-063) |
| FAIL (test defect — fixed) | 0 remaining (4 found and fixed during the pass: TC-B-008, TC-B-009, TC-B-079, TC-B-093) |
| BLOCKED | 0 |
| NOT RUN | 0 |

Final run, twice consecutively: **`1 skipped` · `98 passed` · 0 failed**. The
fourteen annotated cases are counted as passing *because they failed as
declared*. The day any of them is fixed Playwright reports **“passed
unexpectedly”**, which is the signal the annotation exists for — TC-B-063 earned
its `it.fixme()` by doing exactly that on one run.

Annotated cases, in severity order: TC-B-085 (F-B-11, critical) · TC-B-084
(F-B-09, high) · TC-B-018 (F-B-01, high) · TC-B-063 (F-B-10, high, `fixme`) ·
TC-B-099 (F-B-16, high) · TC-B-058 and TC-B-095 (F-B-06, medium) · TC-B-094
(F-B-13, medium) · TC-B-056 (F-B-05, medium) · TC-B-096 (F-B-15, medium) ·
TC-B-046 (F-B-03, medium) · TC-B-060 (F-B-07, medium) · TC-B-091 (F-B-02,
medium) · TC-B-098 (F-B-17, medium) · TC-B-092 (F-B-04, low).

Test defects found and fixed, worth knowing about:

- **TC-B-008 / TC-B-009** — both htmx-driven selects live inside the fragment
  they replace. Acting on one while the previous swap is still settling
  dispatches the change into a node htmx then detaches, and htmx drops a request
  from a detached element **silently**. Waiting for the server's `selected`
  attribute plus `.htmx-request, .htmx-settling, .htmx-swapping` at zero makes it
  deterministic (5/5 after, 1/3 before). Recorded as F-B-12 for the next author.
- **TC-B-079** — `.section-head` occurs twice on `request_detail` (Attachments,
  History). Scope it by its own `h2`.
- **TC-B-093** — two of its three racing submits belong to the same browser, and
  the case then read all three numbers inside one `Promise.all`. A second `goto`
  on a page still navigating aborts the first (`net::ERR_ABORTED`). API requests
  may race on one page; navigations may not — so the raises stay concurrent and
  the reads are now sequential.

## 5. Coverage matrix IDs exercised

| ID | Where |
|---|---|
| T1 | TC-B-001, 002, 007, 010 |
| T2 | TC-B-001…006, 011–056 (the whole of §1) |
| T5 | TC-B-011–025, 042–044 |
| T6 | TC-B-001, 028–030, 057–061, 094 |
| T7 | TC-B-002, 031, 057 |
| T8 | TC-B-003, 032, 033 |
| T9 | TC-B-004, 005, 008, 034–036, 052, 053 |
| T10 | TC-B-082, 083, 084 |
| T11 | TC-B-003, 040, 041, 054 |
| T12 | TC-B-009, 046, 047 |
| A1 | TC-B-021, 065 |
| A8 | TC-B-069 |
| Q1 | TC-B-068, 074 |
| Q2 | TC-B-070, 071, 075 |
| Q3 | TC-B-072, 073 |
| Q5 | TC-B-077, 080, 085, 086, 087, 088 |
| L1 | TC-B-066, 067 (proof-of-absence) |
| L2 | TC-B-001…006, 062, 065 |
| L3 | TC-B-068 |
| L4 | TC-B-072, 074 |
| L5 | TC-B-070, 073 |
| L6 | TC-B-071, 075 |
| L11 | TC-B-048–053, 067, 071, 073, 076 |
| V1 | TC-B-004, 005, 006 |
| V4 | TC-B-037, 097, 098 |
| V5 | TC-B-008, 038, 039, 052 |
| V6 | TC-B-035, 036, 039, 043 |
| N1 | TC-B-065, 069 |
| N7 | TC-B-079, 080, 081, 082 |
| C2 | TC-B-069, 082 |
| C3 | TC-B-016, 017, 018, 087 |
| C4 | TC-B-062, 063, 064, 072, 093 |
| D2 | TC-B-089 |
| D3 | TC-B-087, 099 |
| D4 | TC-B-099 |
| D5 | TC-B-066 |
| R3 / R6 | TC-B-010, 045, 061, 077, 078, 085, 088, 089 |
| **D1 (decision)** | TC-B-065, 066, 067, 072, 082 |

Deliberately **not** covered here (they belong to a sibling): A2–A7 (the
manager's decisions), Q4/Q6 and L7–L10 and every `S*` (settlement), V2/V3/V7/V8
(the recoverable register), N2–N6/N8 (email and reminders), X1–X6.

## 6. Gaps and what could not be tested

| # | Gap | Why |
|---|---|---|
| G-1 | The reminder clock reset in TC-B-069 is asserted through the audit `after` snapshot, not by observing a suppressed reminder. | `reminder_last_sent` has no UI surface, and the scheduler is started by `cmd/server` on an hourly tick with no route to trigger it. Proving a *suppressed* reminder needs the Go suite or a clock injection point neither of which exists over HTTP. Partially covered, honestly labelled. |
| G-2 | The urgency rules (VG1, VG2) are not exercised. | They are configuration-driven and belong to whoever owns `/configuration`; TC-B-083 already changes one setting and restoring it is the risky part of a shared database. Not a scope claim — a deliberate hand-off. |
| G-3 | The 20 MiB upload cap (VG5) is not exercised. | Pushing 20 MiB through the harness on every run costs more than the assertion is worth; the mismatch between the advertised 10 MB and the enforced 20 MiB is already recorded as UC-B DV5. |
| G-4 | No mobile-chrome run. | Nothing in this area has a 390 px question that `requests.spec.ts` and `request-workflow.spec.ts` do not already sweep (they run the full quality bar — no sideways scroll, visible `h1`, tap targets, axe — on both projects). |
| G-5 | `POST /requests` was not probed with a body over the 21 MiB cap. | The 413 path is generic middleware (`withCSRF`), not request-specific. |
