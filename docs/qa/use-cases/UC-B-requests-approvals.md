# UC-B — Payment requests & the approval workflow

Industry-standard use-case specifications for the payment-request lifecycle
(raising, editing, withdrawing, re-raising, cancelling) and the manager approval
workflow, derived from the Go code that ships on `main`.

**Sibling documents.** `UC-A-platform-rbac-admin.md` owns platform, RBAC and
administration (`UC-A-*`). `UC-C-settlement-recoverables-notifications.md` owns
reservation, payment linking, settlement, holds, the recoverable register and
notification delivery (`UC-C-*`). This document references those areas but never
specifies them.

**Source of truth.** Every load-bearing statement below cites `file:line` in the
shipped code. Where the code contradicts a spec or a doc comment, the code is
recorded as what ships and the divergence is called out in
§ *Divergences and suspected defects*.

---

## 0. Reading this document

### 0.1 Conventions

| ID | Convention |
|---|---|
| CV1 | **Route** is method + path exactly as registered in `internal/app/app.go` `routes()` (`internal/app/app.go:362`). An unrouted POST answers **405**, not 404, because `GET /` is a registered catch-all (`internal/app/app.go:376`). |
| CV2 | **Permission gate** is the `(resource, action)` pair passed to `RequirePermission` (`internal/auth/auth.go:140`), plus any handler-level ownership test. Holding a verb is never the same as being allowed to touch a given row. |
| CV3 | Money is `int64` paise (`internal/money`). `money.FormatPaise` already prints `₹`; a `.money-field` input is filled by `amountValue` (`internal/app/requests.go:1053`) so the `₹` is not doubled. |
| CV4 | Every mutating POST is wrapped in `withCSRF` (`internal/app/app.go:554`), which parses the body (multipart when the content type says so), enforces a 21 MiB body cap (`internal/app/http_errors.go:22`) and rejects a bad token with **403** *“Your form session expired. Refresh the page and try again.”* |
| CV5 | A store error is mapped to HTTP by `storeErrorStatus` (`internal/app/http_errors.go:199`): `ErrNotFound`→404, `ErrForbidden`→403, `ErrValidation`/`ErrDuplicate`/`ErrInactiveHead`→400, anything else→500. |
| CV6 | Field labels are quoted **character for character** from `internal/app/templates.go`. The red asterisk is `<span class="req" aria-hidden="true">*</span>`, so it is **not** part of the accessible name. |
| CV7 | Row ids in every table in this document are stable and citable by later documents. |

### 0.2 Selector vocabulary for the Playwright author

Verified against the current templates, not against fixture comments.

| ID | Screen | Control | Selector that works | Note |
|---|---|---|---|---|
| SV1 | `request_form` | Short title | `getByLabel('Short title')` | `templates.go:1836` |
| SV2 | `request_form` | Treatment | `getByRole('radio', { name: /Budget expense/ })` / `/Refundable or recoverable/` | The group heading is a `<span class="flabel">`, **not** a label — `getByLabel('How should this be treated')` fails. `templates.go:1841–1852` |
| SV3 | `request_form_fields` (budget) | Project, Head | `#project`, `#head` or `getByLabel('Project')`, `getByLabel('Head')` | `templates.go:1787`, `1796` |
| SV4 | `request_form_fields` (recoverable) | Category, Expected return date, Related project, Counterparty company, Repayment or refund terms | `getByLabel('Category')`, `getByLabel('Expected return date')`, `getByLabel('Related project')`, `getByLabel('Counterparty company')`, `getByLabel('Repayment or refund terms')` | `templates.go:1735`, `1748`, `1753`, `1763`, `1768`. “Related project” and “Project” never co-exist — the two fieldsets are alternatives. |
| SV5 | `request_form` | Amount, Needed by | `getByLabel('Amount')`, `getByLabel('Needed by')` | `templates.go:1863`, `1869` |
| SV6 | `request_form` | Urgent | `getByLabel('Mark this urgent')` | `templates.go:1875`. On **`request_edit`** the same checkbox reads **“Marked urgent”** (`templates.go:2570`) — different string, same field. |
| SV7 | `request_form` | Why is it urgent | `getByLabel('Why is it urgent')` | Rendered only when `urgency_mode == "reason"`; starts `hidden` and is revealed by `data-when="urgent:on"`. `templates.go:1879–1882` |
| SV8 | `request_form` | Vendor | `#vendor` (combo input) + assert `#vendor-id` non-empty | `templates.go:1902`, `1917`. Pick with `#vendor-options .co` (full page) — the fragment root is `.combo-list` (`templates.go:1365`). |
| SV9 | `request_form` | Invoice number / Invoice date | `getByLabel('Invoice number')`, `getByLabel('Invoice date')` | `templates.go:1929`, `1934` |
| SV10 | `request_form` | Reason for the advance | `getByLabel('Reason for the advance')` | `templates.go:1939` (vendor_advance) |
| SV11 | `request_form` | Paid to (read-only), Expense date | `getByLabel('Paid to')`, `getByLabel('Expense date')` | `templates.go:1952`, `1957` (reimbursement) |
| SV12 | `request_form` | What the money is for | `getByLabel('What the money is for')` | `templates.go:1974` (employee_advance) |
| SV13 | `request_form` | Purpose | `getByLabel('Purpose')` | `templates.go:1985` |
| SV14 | `request_form` | Supporting document | `input[name="attachment"]` **or** `getByLabel('Add invoice, receipt or proof')` | `templates.go:1993–1995`. Unlike the payment screens (`templates.go:429`, `548`) the **request** file input is *not* `hidden` and *does* carry an explicit `for=` label, so `getByLabel` resolves here. `input[name="attachment"]` is still the stable choice. |
| SV15 | `request_form` | Attachment exception | `getByLabel('If you cannot attach a document, say why')` | Rendered only when `require_attachments == "1"`. `templates.go:2001` |
| SV16 | `request_form` | Approver | `getByLabel('Approver')` | `templates.go:2014` |
| SV17 | `request_form` | Submit | `getByRole('button', { name: 'Submit request' })` | `templates.go:2038` |
| SV18 | approve sheet | Amount approved, Note | `#approve-sheet` → `getByLabel('Amount approved')`, `getByLabel(/^Note/)` | `templates.go:2212`, `2217`. **The `optional` badge is not `aria-hidden`**, so the accessible name is literally `"Note optional"` — an exact `getByLabel('Note')` fails. Same trap on `#cx-note` (`templates.go:2882`). |
| SV19 | return sheet | What needs correcting | `#return-sheet` → `getByLabel('What needs correcting')` | `templates.go:2229` |
| SV20 | reject sheet | Reason for rejection | `#reject-sheet` → `getByLabel('Reason for rejection')` | `templates.go:2241` |
| SV21 | `request_returned` | Correction fields | `getByLabel('Short title')`, `'Invoice number'`, `'Invoice date'`, `'Expense date'`, `'Reason for the advance'`, `'Amount'`, `'Purpose'`, `'Attach the corrected document'` | `templates.go:2719–2744` |
| SV22 | `request_returned` | Buttons | `'Save corrections'`, `'Resubmit for approval'` | `templates.go:2754`, `2755` |
| SV23 | `request_edit` | Buttons | `'Discard changes'` (link), `/^Save and notify/` | `templates.go:2658`, `2659` |
| SV24 | `request_thread` | Comment | `getByLabel('Add a comment')`, then `'Post comment'` | `templates.go:2192`, `2194` |
| SV25 | `request_cancel` | Reason | `getByLabel('Reason')`, then `'Send cancellation request'` | `templates.go:2800`, `2816` |
| SV26 | `request_cancellation` | Decline / accept / outright | `#decline-sheet`→`getByLabel('Why it should still be paid')`; `#accept-sheet`→`getByLabel(/^Note/)`; `#outright-sheet`→`getByLabel('Reason')` | `templates.go:2894`, `2882`, `2905` |
| SV27 | `requests` | Toolbar vs filter sheet | **Scope every filter selector.** `page.locator('.toolbar').getByLabel('Type')` or `page.locator('#filter-sheet').getByLabel('Type')` | `Type`/`Treatment`/`Search` exist twice in the DOM — `#ty`/`#tr`/`#q` in `.toolbar` (`templates.go:2129–2138`) and `#fs-ty`/`#fs-tr`/`#fs-q` in the hidden `#filter-sheet` (`templates.go:2079–2092`). An unscoped `getByLabel` is a strict-mode violation. |
| SV28 | `requests` / `approvals` | Mobile search | `getByLabel('Search requests')` / `getByLabel('Search approvals')` (`aria-label`) | `templates.go:2145`, `2945` |
| SV29 | `request_detail` | Action bar | `getByRole('link', { name: 'Edit request' })`, `'Withdraw'`, `'Request cancellation'`, `'Raise it again'`, `'Reject'`, `'Return for correction'`, `/^Approve /`, `'Cancel with reason'`, `'Decide the cancellation'` | `templates.go:2413–2435` |
| SV30 | any | Status pill / waiting line | `.rh-status .pill`, `.waiting`, `.waiting.you` | `templates.go:2266–2267` |
| SV31 | fixture claim to ignore | `tests/e2e/fixtures.ts:178–188` “KNOWN GAP — payee … does not reach the payment” | **Stale.** Fixed at `fca6939`; `internal/app/linking.go:260` now writes `VendorPayee: req.Vendor`. Do not reproduce the comment. |

### 0.3 Actors

| ID | Actor | Seeded grants (`internal/store/migrations.go:352–404`) |
|---|---|---|
| AC1 | **Requester** | `request:view/create/edit/withdraw/reraise/comment/cancel`, `attachment:view/create`; scope `request=own` |
| AC2 | **Manager** (approver) | `request:view/comment`, `approval:approve/reject/return/reassign/accept_partial/cancel`, `grid:view`, `report:view`; scope `request=all` |
| AC3 | **Accounts** | `request:view/comment`, the `payment:*` set, `reservation:reserve/release`, …; scope `request=all`, `payment=all` |
| AC4 | **Admin** | every canonical pair (`adminGrants()`, `internal/store/migrations.go:404`); scope `request=all`, `payment=all` |
| AC5 | **System** | the reminder scheduler and `notify.Service` — specified in `UC-C-*` |

---

## 1. The consolidated per-type validation matrix

Derived from **`validateRequestInput`** — the real name of the validator —
`internal/store/requests.go:127–247`, plus `forcesRequesterPayee`
(`:251`), `validateUrgency` (`:513`), `validateAttachmentPolicy` (`:533`) and the
category rule set loaded by `Store.recoverableRules`
(`internal/store/recoverables.go:119`).

**Legend.** `req` = required, non-empty / positive / a valid `YYYY-MM-DD`
(`validDate`, `internal/store/store.go:1651`) · `opt` = optional, stored as given
· `ign` = read from the form but not validated and **still written to the row** ·
`forced=X` = overwritten by the store whatever was posted · `—` = the whole
combination is refused before any field is looked at.

Always-captured, every type and treatment (`internal/store/requests.go:128–150`):

| ID | Field | Rule | Message on failure |
|---|---|---|---|
| VA1 | `amount` | `> 0` paise | `validation failed: a positive amount is required` |
| VA2 | `short_title` | non-blank | `… a short title is required — it is what your approver sees in their list` |
| VA3 | `purpose` | non-blank | `… purpose is required` |
| VA4 | `manager_id` | `> 0` | `… choose an approver` |
| VA5 | `manager_id != requester_id` | **G8**, structural | `… you cannot approve your own request — choose another approver` |
| VA6 | `treatment` | `budget` \| `recoverable` | `… treatment must be budget or recoverable` |
| VA7 | `type` | one of the five in `requestTypes` (`:78`) | `… unknown request type` |
| VA8 | `needed_by`, `expected_return_date`, `invoice_date`, `expense_date` | if present, must parse `YYYY-MM-DD` | `… <label> is invalid` |

Per type × treatment:

| ID | type | treatment | project_id | head_id | vendor_id | vendor_payee | invoice_no | invoice_date | expense_date | advance_reason | recoverable_category | expected_return_date | repayment_notes | counterparty |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| VT1 | `vendor_invoice` | `budget` | **req** | **req** | **req** | `ign` (left empty; display payee resolves from `vendor_id`) | **req** | **req** | `ign` | `ign` | `ign` | `ign` | `ign` | `ign` |
| VT2 | `vendor_invoice` | `recoverable` | — | — | — | — | — | — | — | — | — | — | — | — |
| VT3 | `vendor_advance` | `budget` | **req** | **req** | **req** | `ign` | `ign` | `ign` | `ign` | **req** | `ign` | `ign` | `ign` | `ign` |
| VT4 | `vendor_advance` | `recoverable` | — | — | — | — | — | — | — | — | — | — | — | — |
| VT5 | `reimbursement` | `budget` | **req** | **req** | `forced=0` | `forced=<requester name>` | `ign` | `ign` | **req** | `ign` | `ign` | `ign` | `ign` | `ign` |
| VT6 | `reimbursement` | `recoverable` | — | — | — | — | — | — | — | — | — | — | — | — |
| VT7 | `employee_advance` | `budget` | **req** | **req** | `forced=0` | `forced=<requester name>` | `ign` | `ign` | `ign` | **req** | `ign` | `ign` | `ign` | `ign` |
| VT8 | `employee_advance` | `recoverable` | per category (VC\*) | `opt` | `forced=0` | `forced=<requester name>` | `ign` | `ign` | `ign` | **req** | **req** (known active code) | **req** | **req** | per category (VC\*) |
| VT9 | `recoverable` | `recoverable` | per category (VC\*) | `opt` | `opt` | `opt` | `ign` | `ign` | `ign` | `ign` | **req** | **req** | **req** | per category (VC\*) |
| VT10 | `recoverable` | `budget` | — | — | — | — | — | — | — | — | — | — | — | — |

Refusal messages for the `—` rows: `… a vendor invoice is a budget expense`
(`:195`), `… a vendor advance is a budget expense` (`:211`), `… reimbursement is
a budget expense` (`:225`), `… recoverable type requires recoverable treatment`
(`:242`).

Ordering note for the test author: for `employee_advance` the `advance_reason`
check runs **before** the treatment branch (`:233`), so a recoverable employee
advance with no reason fails on *“say what the money is for”* and never reports
the recoverable errors.

**Recoverable category rules** — since Phase 4 these come from
`recoverable_categories` **rows** (`internal/store/recoverables.go:119–129`),
active rows only, keyed on **`code`**, which is the stable identity and is never
in the `UPDATE … SET` list (`internal/store/recoverables.go:211–213`). The
built-in map at `internal/store/requests.go:116–123` survives only as the v6 seed
and as the fixture the pure validator tests use.

| ID | code | Seeded name | requires_project | requires_counterparty | Additional message |
|---|---|---|---|---|---|
| VC1 | `employee_advance` | Employee advance | no | no | — |
| VC2 | `emd` | EMD | **yes** | no | `… this recoverable category always belongs to a project` |
| VC3 | `pbg` | PBG | **yes** | no | as VC2 |
| VC4 | `icd` | ICD | no | **yes** | `… this recoverable category needs a counterparty company` |
| VC5 | `security_deposit` | Security deposit | no | **yes** | as VC4 |
| VC6 | `other` | Other | no | no | — |
| VC7 | *any admin-added row* | admin-chosen | admin-chosen | admin-chosen | enforced; **but not selectable — see DV3** |

Every recoverable treatment additionally requires a **known active category code**
(`… choose a recoverable category`), a valid `expected_return_date`
(`… expected return date is required for recoverables`) and non-blank
`repayment_notes` (`… repayment or refund terms are required for recoverables`)
— `internal/store/requests.go:173–191`.

**Configuration-driven rules**, applied after `validateRequestInput`:

| ID | Rule | Code | Default |
|---|---|---|---|
| VG1 | `urgent` + `urgency_mode=reason` ⇒ `urgency_reason` non-blank | `internal/store/requests.go:513–528` | `reason` (`migrations.go:245`) |
| VG2 | `urgent` + `urgency_mode=disabled` ⇒ refused, *“urgent requests are switched off”* | same | — |
| VG3 | `require_attachments=1` and zero attachments ⇒ `attachment_exception_reason` non-blank, *“attach a supporting document, or say why you cannot”* — **never a hard block (G10)** | `internal/store/requests.go:533–546` | `0` (`migrations.go:243`) |
| VG4 | `allow_approver_choice=0` ⇒ the Approver `<select>` renders `disabled` and a hidden `manager_id` carries the default | `templates.go:2015–2019` | `1` (`migrations.go:246`) |
| VG5 | Upload cap: 20 MiB per file, *“files must be 20 MiB or smaller”*; the advertised cap on screen is `attachment_max_mb` = 10 | `internal/app/app.go:948`, `internal/app/http_errors.go:23`, `templates.go:1994`, `migrations.go:244` |  |

---

## 2. Raising a request

### UC-B-01 — Choose what kind of payment is being asked for

| | |
|---|---|
| **Goal** | “I need money paid out and I want to start the right form for it.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | The reader presses **＋ New request** on the dashboard (`templates.go:3188`) or on the requests list (`templates.go:2123`), or opens `/requests/new` directly. |
| **Route(s)** | `GET /requests/new` (no `?type=`, or an unrecognised one) |
| **Permission gate** | `request:create` (`app.go:443`) |
| **Coverage IDs** | T1, T2, D5 |
| **Priority** | critical |

**Preconditions**
1. A session exists; `RequireLogin` would otherwise redirect to `/login` (`auth.go:87`).
2. The caller holds `request:create`.

**Postconditions (success)** — the chooser is rendered; **nothing is written**. There is no draft row, no number, no audit entry (D1, `requests.go:20–33`).

**Postconditions (failure)** — nothing written either way; this is a pure read.

**Main success scenario**
1. Actor navigates to `/requests/new`.
2. System resolves `?type=` against `requestTypeLabels`; absent or unknown ⇒ chooser (`requests.go:72–76`).
3. System renders `request_new_type`: eyebrow **“New request · step 1 of 2”**, `h1` **“What are you asking to be paid?”**, sub **“Pick a type. The form only asks for what that type needs. Nothing is saved until you submit.”** (`templates.go:1684–1686`).
4. System renders exactly **four** `.type-card` links from `requestTypeOptions` (`requests.go:42–55`), in this order and with these titles and pills:
   - **Vendor invoice payment** — “Needs invoice number and date”
   - **Vendor advance** — “Needs a reason for the advance”
   - **Reimbursement** — “Paid to you · needs the expense date”
   - **Employee advance** — “Usually recoverable” (pill class `recoverable`)
5. Actor clicks a card; the browser navigates to `/requests/new?type=<key>` — the card is an `<a>`, not a form (`templates.go:1693`).

**Alternate flows**
- **01.a** (step 2) `?type=` is one of the four keys ⇒ UC-B-02.
- **01.b** Actor presses **Cancel** (`templates.go:1688`) ⇒ `GET /`.

**Exception flows**
- **01.e1** `?type=recoverable`: `recoverable` is a valid store type (`store/requests.go:78–81`) but has **no card and no label**, so the chooser is re-rendered silently. See UC-B-10 and **DV1**.
- **01.e2** Caller lacks `request:create` ⇒ **403** error page *“You do not have permission to perform this action.”* (`auth.go:143`).
- **01.e3** `POST /requests/new` ⇒ **405** (CV1).

**Business rules**
- BR-01.1 The type vocabulary exists in exactly one place; adding a type means adding a `requestTypeOption` — `internal/app/requests.go:37–55`.
- BR-01.2 The type is a route parameter, never a form control (A16) — `internal/app/requests.go:29–33`.
- BR-01.3 No “copy a previous request” control exists anywhere on this screen (D5).

**Data touched** — none. Reads only the in-process `requestTypeOptions` slice.

**Non-functional / UX notes** — at 390px `.type-grid` stacks to one column and the tab bar is present; the `h1` is visible (no `page-banner d-only`). Cards are links ⇒ Enter activates. Four cards, so there is no empty state.

**Open questions** — none.

---

### UC-B-02 — Open the adaptive form for a chosen type

| | |
|---|---|
| **Goal** | “Show me only the fields my kind of request needs.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | A `.type-card` on UC-B-01, or a direct `?type=` URL. |
| **Route(s)** | `GET /requests/new?type={vendor_invoice\|vendor_advance\|reimbursement\|employee_advance}` |
| **Permission gate** | `request:create` |
| **Coverage IDs** | T1, T2, T3, T4, T5, T12, A1 |
| **Priority** | critical |

**Preconditions**
1. Session with `request:create`.
2. At least one active project **and** one active head exist, otherwise the budget fieldset offers only placeholders.
3. At least one other active user holds `approval:approve`, or the Approver `<select>` has only *“Choose an approver”* (`ListApprovers`, `store/requests.go:551–571`).

**Postconditions (success)** — the form is rendered with the type in a hidden input; nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**
1. Actor arrives at `/requests/new?type=vendor_invoice`.
2. System looks the type up, loads **active** projects and heads, the approver list, and `app_settings` (`requests.go:78`, `359–384`).
3. System pre-selects the actor’s `default_approver_id` when it is non-zero (G9, `requests.go:379–382`).
4. System defaults `Treatment` to `budget`; for `employee_advance` only, it defaults to `recoverable` with category `employee_advance` (`requests.go:84–92`).
5. System loads the vendor fallback list — active vendors, limit 500 — for `vendor_invoice`/`vendor_advance` only (`requests.go:286–292`).
6. System renders `request_form`: eyebrow **“New request · step 2 of 2”**, `h1` = the type label, sub **“One form that changes with what you pick. Nothing is saved until you submit.”**, and a **“← Change type”** link back to `/requests/new` (`templates.go:1821–1825`).
7. Fieldsets render in this fixed order: **What is this for** → `#form-fields` → **Amount and timing** → (**Vendor and invoice** \| **Vendor and advance** \| **Your expense** \| **Advance details**) → **Purpose and documents** → **Who approves it**, closed by one `.action-bar` with **Submit request** (`templates.go:1832–2039`).

**Alternate flows**
- **02.a** `type=reimbursement` ⇒ the **Your expense** fieldset with read-only **Paid to** = `{{.User.Name}}` and **Expense date** (`templates.go:1948–1961`).
- **02.b** `type=employee_advance` ⇒ **Advance details** with read-only **Paid to** and **What the money is for**; the form opens on the recoverable treatment (`templates.go:1964–1979`).
- **02.c** `urgency_mode=disabled` ⇒ the whole urgency block is absent (`templates.go:1873`).
- **02.d** `urgency_mode=free` ⇒ the checkbox renders, **Why is it urgent** does not (`templates.go:1878`).
- **02.e** `require_attachments=1` ⇒ **Supporting document** carries `*` instead of the `optional` badge, and the exception field appears (`templates.go:1989`, `1999–2006`).
- **02.f** `allow_approver_choice=0` ⇒ the `<select>` is `disabled` and a hidden `manager_id` carries the value (VG4).
- **02.g** Caller lacks `vendor:view` ⇒ the vendor control is a plain `<select id="vendor" name="vendor_id">` rather than the combobox (`templates.go:1919–1925`).

**Exception flows**
- **02.e1** Unknown `?type=` ⇒ chooser, not an error (UC-B-01.e1).
- **02.e2** A store read fails ⇒ `respondStoreError` (CV5).

**Business rules**
- BR-02.1 **No bank, account, IFSC or UPI field appears on any request screen**; bank details live on the vendor record behind `vendor_bank` (T3) — `internal/app/templates.go:1815–1816`, `1918`.
- BR-02.2 Only **active** projects and heads are offered on a new request; a historical request keeps showing its own (T12) — `internal/app/requests.go:362–369` with `activeOnly=true`, versus the `LEFT JOIN` names in `requestSelect` (`store/requests.go:267–268`).
- BR-02.3 The requester’s own name is **structurally absent** from the approver list (G8) — `store/requests.go:551–557` (`u.id<>?`).
- BR-02.4 Nothing inside `#form-fields` carries the HTML `required` attribute; every asterisk there is backed by `aria-required` — a `required` control that `data-when` has hidden makes the form unsubmittable in Chrome — `internal/app/templates.go:1725–1728`.

**Data touched** — reads `projects`, `heads`, `users` (+ `user_roles`, `role_permissions`), `app_settings`, `vendors`. Writes nothing.

**Non-functional / UX notes** — one column at 390px (`.span-*` collapse, `m-half` pairs); the `.action-bar` is sticky above the tab bar. The `.ab-note` *“Submitting sends it to your approver and creates the request number.”* is `d-only`, so it is desktop-only — do not assert it on the phone project. First focusable control is **Short title**.

**Open questions** — none.

---

### UC-B-03 — Swap the treatment-dependent middle of the form

| | |
|---|---|
| **Goal** | “When I say this is refundable, ask me refundable questions instead of budget ones.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | `change` on the treatment radios, on **Category**, or on **Project** (`templates.go:1843`, `1737`, `1789`). |
| **Route(s)** | `GET /requests/new/fields?type=…&treatment=…&recoverable_category=…&project_id=…&head_id=…&counterparty=…&expected_return_date=…&repayment_notes=…` |
| **Permission gate** | `request:create` (`app.go:444`) |
| **Coverage IDs** | T1, T2, T9, V5 |
| **Priority** | high |

**Preconditions**
1. Session with `request:create`.
2. The browser runs htmx (`hx-include="closest form"`, `hx-target="#form-fields"`).

**Postconditions (success)** — `#form-fields` contains the fieldset for the posted treatment, with everything the requester had already typed still in it. Nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**
1. Actor checks **Refundable or recoverable**.
2. Browser issues the `GET` with the whole form serialised.
3. System resolves the treatment: anything other than the literal `recoverable` is `budget` (`requests.go:114–118`).
4. System normalises the category through `normalizeRecoverableCategory`: a known code survives; otherwise `employee_advance` for that type, else `emd` (`requests.go:132–140`).
5. System re-reads `project_id`, `head_id`, `counterparty`, `expected_return_date`, `repayment_notes` from the query so the swap costs the requester nothing (`requests.go:121–125`).
6. System renders **`request_form_fields`** without the shell (`renderPartial`, `app.go:536`), as legend **“Recoverable details”** with **Category**, **Expected return date**, **Repayment or refund terms**, plus the brand banner *“This will not touch budget actuals”* (`templates.go:1731–1781`).
7. htmx replaces `#form-fields`; the client `data-when` pass is instant local feedback only (`fervid-app.js:195–211`).

**Alternate flows**
- **03.a** Category `emd` or `pbg` ⇒ **Related project** appears with hint *“EMD and PBG always belong to a project.”* (`templates.go:1751–1760`).
- **03.b** Category `icd` or `security_deposit` ⇒ **Counterparty company** appears, placeholder *“Company receiving the deposit”* (`templates.go:1761–1766`).
- **03.c** Treatment back to **Budget expense** ⇒ legend **“Charge it to”** with **Project** and **Head** (`templates.go:1783–1803`).
- **03.d** Changing **Project** re-renders the head `<select>` filtered to that project’s heads, option text `{{.Project}} / {{.Name}}` (`templates.go:1799`).

**Exception flows**
- **03.e1** An unknown `recoverable_category` silently becomes `emd` (or `employee_advance`). For an **admin-added** category this rewrites the requester’s choice — see **DV3**.
- **03.e2** Fragment render failure ⇒ **500** *“That section could not be rendered.”* (`app.go:544`).

**Business rules**
- BR-03.1 Budget and recoverable are rendered as **alternatives, never as two fieldsets with one hidden** — a hidden control still submits, and leaving the budget `project_id` in the document during a recoverable request would post two values and let the stale one win — `internal/app/templates.go:1718–1723`.
- BR-03.2 The conditional-field rules exist once, in Go; `hidden` is not validation and every reveal is re-enforced by `validateRequestInput` — `internal/app/requests.go:100–104`.
- BR-03.3 The fragment is a `GET` and writes nothing.

**Data touched** — reads `projects`, `heads`, `users`, `app_settings`.

**Non-functional / UX notes** — at 390px the swapped fields are `span-6 m-half` pairs. Focus is **not** moved by the swap, so the radio keeps focus (assert with `toBeFocused`). Empty project list ⇒ only the *“Choose a project”* placeholder.

**Open questions** — the head `<select>` is filtered client-of-server-side by project, but nothing rejects a `head_id` belonging to a different project on submit (**DV6**).

---

### UC-B-04 — Be warned about a possible duplicate, and submit anyway

| | |
|---|---|
| **Goal** | “Tell me if I am about to ask for the same thing twice, but do not stop me.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | `blur` on **Amount** or on **Invoice number** (`templates.go:1865`, `1931`). |
| **Route(s)** | `POST /requests/duplicate-check` |
| **Permission gate** | `request:create` + CSRF (`app.go:445`) |
| **Coverage IDs** | T2, G6 |
| **Priority** | high |

**Preconditions**
1. Session with `request:create`.
2. The form carries a payee identity: a non-zero `vendor_id`, **or** a type whose payee is the requester (`SimilarRequests` returns nothing otherwise, `store/requests.go:1318–1320`).

**Postconditions (success)** — `#dup-check` holds either nothing or the advisory banner. **No row is written and no submit is gated.**

**Postconditions (failure)** — the failure is logged and swallowed; `#dup-check` is left empty (`requests.go:266–269`).

**Main success scenario**
1. Actor types an amount and tabs out.
2. Browser posts the serialised form to `/requests/duplicate-check`.
3. System parses the amount, reads `vendor_id`, and for `reimbursement`/`employee_advance` substitutes the signed-in user’s name as the payee (`requests.go:251–258`).
4. System queries `SimilarRequests`: not this id, status not in `withdrawn/rejected/cancelled`, created within the window (default **30 days**), same vendor id **or** same lower-cased display payee, **and** amount within ±1 % (floor 100 paise) **or** the identical invoice number, newest first, limit 5 (`store/requests.go:1310–1341`).
5. Zero matches ⇒ empty 200 body; `#dup-check` stays empty (`requests.go:270–272`).
6. One or more matches ⇒ `request_duplicates` renders a `.banner.warn` headed **“A similar request already exists”** (singular) or **“N similar requests already exist”**, with sub *“Same payee and a close amount in the last 30 days. Check before you submit — you can still go ahead.”* and a `.req-card` per match (`templates.go:2970–2991`).
7. Actor finishes the form and presses **Submit request**; the submit succeeds regardless.

**Alternate flows**
- **04.a** On the **edit** screen the field would carry `request_id` so the row excludes itself (`ExcludeID`, `requests.go:261`) — note that `request_edit` wires **no** duplicate check, so this path is unexercised in the product.

**Exception flows**
- **04.e1** Missing/invalid CSRF ⇒ **403** (CV4) — the fragment target then receives an error page fragment.
- **04.e2** The query errors ⇒ logged as `duplicate check failed`, empty body, submit unaffected.

**Business rules**
- BR-04.1 A duplicate warning **never** blocks submission; `POST /requests` neither calls this endpoint nor consults its result (G6) — `internal/app/requests.go:243–248`, `store/requests.go:1306–1309`.
- BR-04.2 The tolerance is ±1 % with a 100-paise floor, so *“₹1,00,000 again”* is caught and *“₹2,500”* is not — `store/requests.go:1321–1325`.
- BR-04.3 Closed requests are never offered as duplicates — `store/requests.go:1328`.

**Data touched** — reads `payment_requests` joined to `projects`, `heads`, `vendors`, `users`.

**Non-functional / UX notes** — `#dup-check` is `aria-live="polite"`, so the warning is announced without stealing focus (`templates.go:2032`). At 390px the matched cards stack. Empty state is genuinely empty markup — assert `toBeEmpty()`, not a message.

**Open questions** — the window is fixed at 30 days in code (`opt.Days` default) while the banner text also says 30; there is no `app_settings` key for it.

---

### UC-B-05 — Raise a vendor invoice payment request

| | |
|---|---|
| **Goal** | “I have an invoice from a vendor and it needs paying.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | Manager (AC2) as recipient; `notify.Service` (AC5) |
| **Scope / level** | system · user-goal |
| **Trigger** | **Submit request** on `/requests/new?type=vendor_invoice`. |
| **Route(s)** | `POST /requests` |
| **Permission gate** | `request:create` + CSRF (`app.go:446`) |
| **Coverage IDs** | T1, T2, T3, T5, T6, T12, A1, L1, L2, L11, C2, C3, C4, D1 |
| **Priority** | critical |

**Preconditions**
1. Session with `request:create`.
2. An active project and one of its active heads exist.
3. An active vendor exists in the vendor master.
4. Another active user holds `approval:approve`.
5. Fields per **VT1** are filled: Short title, Project, Head, Vendor (as an id), Amount, Invoice number, Invoice date, Purpose, Approver.

**Postconditions (success)**
1. Exactly one `payment_requests` row exists with `status='pending'`, `treatment='budget'`, `type='vendor_invoice'`, `submitted_at=CURRENT_TIMESTAMP`, `requester_id=<actor>`, `manager_id=<chosen>` (`store/requests.go:414–427`).
2. `number` is `<prefix>-<year>-<NNNNNN>` — default `PR-2026-000001` — reserved inside the same transaction from `request_number_seq` (`store/requests.go:52–76`).
3. Any staged file exists as a `request_attachments` row in that same transaction (`store/requests.go:435–440`).
4. One `audit_log` row: action `submit`, entity `payment_request`, summary *“<name> submitted request <number> for ₹X”*, `after` carrying `number`, `type`, `amount`, `manager_id` (`store/requests.go:442–447`).
5. `notify.EventRequestSubmitted` is fired, plus `EventRequestUrgent` when `urgent` (`requests.go:172`, `notifications.go:46–51`).
6. The browser is at `GET /requests/{id}/submitted` via **303** (`requests.go:173`).

**Postconditions (failure)** — **nothing** is written: the row, its number, its `submitted_at` and its attachment commit together or not at all, and the staged file is deleted behind them (`requests.go:161–170`, `store/requests.go:401–451`).

**Main success scenario**
1. Actor fills **Short title**.
2. Actor leaves **Budget expense** checked.
3. Actor selects a **Project**; the head list narrows (UC-B-03.d).
4. Actor selects a **Head**.
5. Actor types into **Vendor** and picks a result; `#vendor-id` receives the id (UC-B-11).
6. Actor fills **Amount**; the `.in-words` line renders `money.InWords`; the duplicate check may fire (UC-B-04).
7. Actor fills **Invoice number** and **Invoice date**.
8. Actor fills **Purpose**.
9. Actor selects an **Approver** (pre-selected when a default exists, rendered as `<name> (your default)`, `templates.go:2017`).
10. Actor presses **Submit request**.
11. System reads every field unconditionally (`requestInput`, `requests.go:298–328`), stages the upload, then calls `CreateRequest`.
12. System forces nothing for this type, validates per **VT1**, resolves the recoverable category link to `NULL` (budget), applies VG1–VG3, writes the row, the number, the attachment and the audit line in one transaction, fires the event and redirects.

**Alternate flows**
- **05.a** No file attached and `require_attachments=0` ⇒ accepted with no attachment.
- **05.b** **Needed by** filled ⇒ stored and shown on the confirmation as *“· needed by <long date>”* (`templates.go:3015`).
- **05.c** **Mark this urgent** checked ⇒ UC-B-14.
- **05.d** No JavaScript ⇒ the `<noscript>` `<select id="vendor-plain" name="vendor_id">` posts the id instead; it is rendered *before* the hidden input so it wins as the first `vendor_id` in the body (`templates.go:1908–1917`).

**Exception flows**
- **05.e1** Amount unparseable ⇒ `requestInput` returns *“validation failed: enter the amount you are requesting”*; the form re-renders at **400** with everything still typed in (`requests.go:322–327`, `179–198`).
- **05.e2** Any VA/VT1 rule fails ⇒ **400**, `request_form` again, `.alert.error` banner at the top of the page (`templates.go:48`), all values preserved via `requestFromInput` (`requests.go:202–216`).
- **05.e3** No vendor chosen (`vendor_id` absent or 0) ⇒ *“choose a vendor from the vendor master”*.
- **05.e4** Treatment forged to `recoverable` ⇒ *“a vendor invoice is a budget expense”* (**VT2**).
- **05.e5** `manager_id` == requester ⇒ **G8** refusal (UC-B-33).
- **05.e6** File larger than 20 MiB ⇒ **400** and the partial file is removed (VG5).
- **05.e7** Whole body over 21 MiB ⇒ **413** *“The submitted form is too large.”* (`app.go:565`).
- **05.e8** `vendor_id` pointing at no vendor row ⇒ **500**, not 400 (UC-B-41, **DS1**).

**Business rules**
- BR-05.1 **D1 — there are no drafts.** Creating, numbering and submitting are one atomic POST; a request exists only once it has been sent — `internal/app/requests.go:22–27`, `internal/store/migrations.go:155–156` (`CHECK (status <> 'draft')`).
- BR-05.2 The number is reserved inside the caller’s transaction and is monotonic per year (C4) — `internal/store/requests.go:65–75`.
- BR-05.3 `vendor_payee` is left empty for this type; the display payee is `COALESCE(NULLIF(v.name,''), r.vendor_payee)` and screens must read `.Vendor` — `internal/store/requests.go:257`.
- BR-05.4 The visible combobox text is never trusted; the hidden id is what the server reads — `internal/app/templates.go:1897–1899`.

**Data touched** — writes `payment_requests`, `request_number_seq`, `request_attachments`, `audit_log`, `notifications` (via `UC-C-*`). Reads `projects`, `heads`, `vendors`, `users`, `app_settings`, `recoverable_categories`.

**Non-functional / UX notes** — single column at 390px; sticky action bar; `.in-words` updates live from `fervid-app.js`. On a validation failure the page re-renders at HTTP 400 with the banner as the first thing in `.page-inner`, so focus lands at the top of the document — the failing field is **not** focused.

**Open questions** — the user-visible message keeps the sentinel prefix, e.g. *“validation failed: the invoice number is required”* (`friendly`, `app.go:1633`). Intentional or leakage is unresolved (**DS4**).

---

### UC-B-06 — Raise a vendor advance request

| | |
|---|---|
| **Goal** | “Pay this vendor before any invoice exists — a deposit against an order.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | Manager (AC2) |
| **Scope / level** | system · user-goal |
| **Trigger** | **Submit request** on `/requests/new?type=vendor_advance`. |
| **Route(s)** | `POST /requests` |
| **Permission gate** | `request:create` + CSRF |
| **Coverage IDs** | T1, T2, T5, T7, T12, A1, L2, C2, C3, C4, D1 |
| **Priority** | critical |

**Preconditions**
1. As UC-B-05 preconditions 1–4.
2. Fields per **VT3**: Short title, Project, Head, Vendor, Amount, **Reason for the advance**, Purpose, Approver. There is no invoice number and no invoice date.

**Postconditions (success)** — as UC-B-05, with `type='vendor_advance'`, `advance_reason` non-blank, `invoice_no=''` and `invoice_date NULL`.

**Postconditions (failure)** — nothing written; staged file removed.

**Main success scenario**
1. Actor opens `/requests/new?type=vendor_advance`; `h1` reads **“Vendor advance”**.
2. Fieldset legend reads **“Vendor and advance”** (`templates.go:1891`).
3. Actor fills **Short title**, **Project**, **Head**, **Vendor**, **Amount**.
4. Actor fills **Reason for the advance** (`templates.go:1939`).
5. Actor fills **Purpose**, selects an **Approver**, presses **Submit request**.
6. System validates per **VT3** and commits exactly as UC-B-05.

**Alternate flows**
- **06.a** Invoice fields are absent from the DOM for this type; a hand-rolled `invoice_no` is stored but never required and never displayed unless non-empty (`templates.go:2351`).

**Exception flows**
- **06.e1** Blank **Reason for the advance** ⇒ **400** *“say what the advance is for”* (`store/requests.go:219–221`).
- **06.e2** Treatment forged to `recoverable` ⇒ *“a vendor advance is a budget expense”* (**VT4**).
- **06.e3** Missing project or head ⇒ *“project and head are required for this type”*.
- **06.e4** Missing vendor ⇒ *“choose a vendor from the vendor master”*.

**Business rules**
- BR-06.1 A vendor advance is always a budget expense — `internal/store/requests.go:209–212`. The **recoverable** counterpart of “money we expect back from a vendor” is the `recoverable` treatment on a different type, not this one.
- BR-06.2 `advance_reason` is shared with `employee_advance` but carries a different label per screen: **“Reason for the advance”** here, **“What the money is for”** there (`templates.go:1939` vs `1974`).

**Data touched** — as UC-B-05.

**Non-functional / UX notes** — the **Vendor** field is `span-6` and the reason `span-6`; both are full width at 390px. Empty vendor master ⇒ only *“Choose a vendor”*.

**Open questions** — none.

---

### UC-B-07 — Raise a reimbursement (payee forced to me)

| | |
|---|---|
| **Goal** | “I spent my own money for the company and want it back.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | Manager (AC2) |
| **Scope / level** | system · user-goal |
| **Trigger** | **Submit request** on `/requests/new?type=reimbursement`. |
| **Route(s)** | `POST /requests` |
| **Permission gate** | `request:create` + CSRF |
| **Coverage IDs** | T1, T2, T5, T8, T11, T12, A1, L2, C2, C3 |
| **Priority** | critical |

**Preconditions**
1. Session with `request:create`.
2. An active project and one of its active heads exist.
3. Another active user holds `approval:approve`.
4. Fields per **VT5**: Short title, Project, Head, Amount, **Expense date**, Purpose, Approver. No vendor control is rendered.

**Postconditions (success)**
1. `payment_requests` row with `type='reimbursement'`, `treatment='budget'`, `vendor_id NULL`, **`vendor_payee` = the actor’s `users.name`** (`store/requests.go:367–370`).
2. `expense_date` stored as a valid ISO date; `status='pending'`; number assigned; audit `submit` row written.
3. Redirect **303** to `/requests/{id}/submitted`.

**Postconditions (failure)** — nothing written.

**Main success scenario**
1. Actor opens `/requests/new?type=reimbursement`; `h1` reads **“Reimbursement”**.
2. Fieldset **“Your expense”** shows read-only **Paid to** = the actor’s name with hint *“Reimbursements always pay the person raising them.”* (`templates.go:1948–1955`).
3. Actor fills **Short title**, **Project**, **Head**, **Amount**, **Expense date**, **Purpose**, **Approver**.
4. Actor presses **Submit request**.
5. System sets `VendorID=0` and `VendorPayee=actor.Name` **before** validation, so nothing the form said about a payee can survive (`store/requests.go:367–370`).
6. System validates per **VT5** and commits.

**Alternate flows**
- **07.a** A hand-rolled `vendor_id` and `vendor_payee` are both overwritten — the forcing is unconditional for this type.
- **07.b** The duplicate check substitutes the actor’s name as the payee, so repeat reimbursements are caught (`requests.go:254–258`).

**Exception flows**
- **07.e1** Blank or malformed **Expense date** ⇒ **400** *“the expense date is required”* (`store/requests.go:229–231`).
- **07.e2** Treatment forged to `recoverable` ⇒ *“reimbursement is a budget expense”* (**VT6**).
- **07.e3** Missing project or head ⇒ *“project and head are required for this type”*.

**Business rules**
- BR-07.1 **T11 / T8** — the payee of a reimbursement is the logged-in employee, forced by the store, not by the form — `internal/store/requests.go:249–253`, `365–370`.
- BR-07.2 The `Paid to` input carries **no `name`**, so it posts nothing at all (`templates.go:1953`).
- BR-07.3 The read-only field shows `.User.Name` on the create form but `.Request2.RequesterName` on the edit form (`templates.go:2601`) — correct in both cases, different bindings.

**Data touched** — writes `payment_requests` (incl. `vendor_payee`), `request_number_seq`, `audit_log`. Reads `projects`, `heads`, `users`, `app_settings`.

**Non-functional / UX notes** — **Paid to** is `readonly`, not `disabled`, so it stays in the tab order and is announced. Two `m-half` fields become full width at 390px.

**Open questions** — none.

---

### UC-B-08 — Raise an employee advance as a budget expense

| | |
|---|---|
| **Goal** | “Give me money up front for organisation spending, and just charge it to the budget.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | Manager (AC2) |
| **Scope / level** | system · user-goal |
| **Trigger** | **Submit request** on `/requests/new?type=employee_advance` after switching the treatment to **Budget expense**. |
| **Route(s)** | `POST /requests` |
| **Permission gate** | `request:create` + CSRF |
| **Coverage IDs** | T1, T2, T5, T9, T11, T12, A1, L2, V1 |
| **Priority** | high |

**Preconditions**
1. Session with `request:create`.
2. An active project and one of its active heads exist.
3. Another active user holds `approval:approve`.
4. Fields per **VT7**: Short title, **Project**, **Head**, Amount, **What the money is for**, Purpose, Approver.

**Postconditions (success)**
1. `payment_requests` row with `type='employee_advance'`, `treatment='budget'`, `project_id`/`head_id` set, `vendor_id NULL`, `vendor_payee` = the actor’s name.
2. `recoverable_category_id` is **NULL** — `recoverableCategoryLink` returns nil for a budget treatment (`store/recoverables.go:134–137`).
3. `status='pending'`, number assigned, audit `submit` row written, redirect **303**.

**Postconditions (failure)** — nothing written.

**Main success scenario**
1. Actor opens `/requests/new?type=employee_advance`. The form opens on **Refundable or recoverable** with **Category** pre-set to *Employee advance* (`requests.go:88–92`).
2. Actor checks **Budget expense**.
3. htmx swaps `#form-fields` to the **“Charge it to”** fieldset (UC-B-03.c); the recoverable controls leave the document entirely.
4. Actor selects **Project** and **Head**.
5. Actor fills **Short title**, **Amount**, **What the money is for**, **Purpose**, **Approver**.
6. Actor presses **Submit request**.
7. System checks `advance_reason` first, then takes the `budget` branch and requires project + head (`store/requests.go:232–238`).

**Alternate flows**
- **08.a** Actor leaves the treatment on recoverable ⇒ UC-B-09.
- **08.b** `recoverable_category` still posted (stale hidden value) while `treatment=budget` ⇒ the **code text is stored** but the id link is NULL; the card and the confirmation gate the recoverable pill on `treatment`, so nothing is mislabelled (`templates.go:2057`, `3015`). See **DS3** for the fields that *are* leaked.

**Exception flows**
- **08.e1** Blank **What the money is for** ⇒ **400** *“say what the money is for”* — checked before the treatment branch (`store/requests.go:233–235`).
- **08.e2** Missing project or head ⇒ *“project and head are required for this type”*.

**Business rules**
- BR-08.1 **T9** — an employee advance is refundable→recoverable, non-refundable→budget, and the requester chooses; the type does not decide — `internal/app/requests.go:86–92`, `internal/store/requests.go:232–239`.
- BR-08.2 The payee is forced to the requester on **both** treatments (`forcesRequesterPayee`, `store/requests.go:251–253`).

**Data touched** — as UC-B-07 plus `recoverable_categories` (read).

**Non-functional / UX notes** — the treatment radio is the only control on the screen whose change re-renders a third of the form; at 390px the swap is a visible content jump — the sticky action bar keeps **Submit request** reachable throughout.

**Open questions** — none.

---

### UC-B-09 — Raise an employee advance on the recoverable treatment

| | |
|---|---|
| **Goal** | “Advance me money the company expects to see accounted for, and keep it out of budget actuals.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | Manager (AC2) |
| **Scope / level** | system · user-goal |
| **Trigger** | **Submit request** on `/requests/new?type=employee_advance` with the default treatment left alone. |
| **Route(s)** | `POST /requests` |
| **Permission gate** | `request:create` + CSRF |
| **Coverage IDs** | T1, T2, T5, T9, T11, A1, L2, V1, V4, V5, V6, V8 |
| **Priority** | critical |

**Preconditions**
1. Session with `request:create`.
2. At least one **active** row in `recoverable_categories` (seeded by v6, `store/recoverables.go:37–61`).
3. Another active user holds `approval:approve`.
4. Fields per **VT8**: Short title, **Category**, **Expected return date**, **Repayment or refund terms**, Amount, **What the money is for**, Purpose, Approver — plus **Related project** or **Counterparty company** when the category demands it.

**Postconditions (success)**
1. `payment_requests` row with `treatment='recoverable'`, `type='employee_advance'`, `recoverable_category=<code>`, `recoverable_category_id=<row id>` resolved by code (`store/recoverables.go:134–146`), `expected_return_date` a valid ISO date, `repayment_notes` non-blank, `vendor_payee` = the actor’s name.
2. The request is visible in the recoverable register (specified in `UC-C-*`) and excluded from budget actuals (V2).
3. `status='pending'`, number assigned, audit `submit` row written, redirect **303**.

**Postconditions (failure)** — nothing written; staged file removed.

**Main success scenario**
1. Actor opens `/requests/new?type=employee_advance`; the form is already on **Refundable or recoverable** with **Category** = *Employee advance*.
2. `#form-fields` shows legend **“Recoverable details”** and the brand banner *“This will not touch budget actuals — It appears in Recoverable payments instead.”* (`templates.go:1772–1778`).
3. Actor fills **Expected return date**.
4. Actor fills **Repayment or refund terms**.
5. Actor fills **Short title**, **Amount**, **What the money is for**, **Purpose**, **Approver**.
6. Actor presses **Submit request**.
7. System validates `advance_reason` first, then `needsRecoverable`: the category must be a known **active** code, the return date must parse, the terms must be non-blank, and the category’s own two flags are applied (`store/requests.go:173–191`, `239`).
8. System resolves the code to `recoverable_category_id` and commits.

**Alternate flows**
- **09.a** Category switched to **EMD — earnest money deposit** or **PBG — performance bank guarantee** ⇒ **Related project** appears and becomes required (**VC2/VC3**); the request may therefore link to a real project (V8).
- **09.b** Category switched to **ICD — inter-corporate deposit** or **Security deposit** ⇒ **Counterparty company** appears and becomes required (**VC4/VC5**).
- **09.c** Category **Other** ⇒ neither extra field; only the three universal recoverable fields.
- **09.d** An admin deactivates a category between render and submit ⇒ the option is still in the DOM but validation refuses it: *“choose a recoverable category”*. This is the Active switch working (`store/recoverables.go:117–121`).

**Exception flows**
- **09.e1** Unknown or inactive `recoverable_category` ⇒ **400** *“choose a recoverable category”*.
- **09.e2** Blank/invalid **Expected return date** ⇒ **400** *“expected return date is required for recoverables”*.
- **09.e3** Blank **Repayment or refund terms** ⇒ **400** *“repayment or refund terms are required for recoverables”*.
- **09.e4** `emd`/`pbg` with no project ⇒ **400** *“this recoverable category always belongs to a project”*.
- **09.e5** `icd`/`security_deposit` with a blank counterparty ⇒ **400** *“this recoverable category needs a counterparty company”*.
- **09.e6** An admin-added category chosen through the htmx swap is silently rewritten to `emd`/`employee_advance` before it ever reaches the select — **DV3**.

**Business rules**
- BR-09.1 **V4** — the rule set is read from `recoverable_categories` rows, not from a hardcoded map, so a category an admin adds is enforced without a code change — `internal/store/recoverables.go:115–129`.
- BR-09.2 A category’s stable identity is its **`code`**, never its name; `code` is excluded from the update statement so renaming never orphans requests — `internal/store/recoverables.go:96–113`, `211–213`.
- BR-09.3 Only **active** categories are offered to the validator, so deactivating one stops new requests naming it — `internal/store/recoverables.go:117–121`.
- BR-09.4 `recoverable_category_id` is what every Phase-4 register query joins on; the code alone is not enough — `internal/store/recoverables.go:131–133`.

**Data touched** — writes `payment_requests` (`treatment`, `recoverable_category`, `recoverable_category_id`, `expected_return_date`, `repayment_notes`, `counterparty`, `vendor_payee`), `request_number_seq`, `audit_log`. Reads `recoverable_categories`, `projects`, `heads`, `users`, `app_settings`.

**Non-functional / UX notes** — the category `<select>` and the return date are `span-6 m-half`, so they pair on a tablet and stack at 390px. The `.hint` under **Category** reads *“Categories are maintained by your administrator.”* Each conditional field is `aria-required="true"` and carries **no** `required` attribute (BR-02.4).

**Open questions** — the option text in the template (*“EMD — earnest money deposit”*) and the seeded row name (*“EMD”*) differ, so one category has two display names depending on the screen (**DV4**).

---

### UC-B-10 — Raise a `recoverable`-type request (no UI path)

| | |
|---|---|
| **Goal** | *(as documented)* “Record a pure recoverable — a deposit that is not an advance to anybody in particular.” |
| **Primary actor** | Requester (AC1), by hand-rolled POST only |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | A crafted `POST /requests` with `type=recoverable`. There is **no screen control that produces it.** |
| **Route(s)** | `POST /requests` |
| **Permission gate** | `request:create` + CSRF |
| **Coverage IDs** | T2, T9, V1, V5, V6, L11 |
| **Priority** | medium (integrity) |

**Preconditions**
1. Session with `request:create` and a valid CSRF token.
2. `treatment=recoverable` and a known active `recoverable_category`.
3. Fields per **VT9**.

**Postconditions (success)** — a `payment_requests` row with `type='recoverable'`, `treatment='recoverable'`, `status='pending'` and a number. It is a first-class request from then on: it appears in lists, in the approvals queue and in the recoverable register.

**Postconditions (failure)** — nothing written.

**Main success scenario**
1. Actor posts `type=recoverable`, `treatment=recoverable`, `recoverable_category=emd`, `project_id`, `expected_return_date`, `repayment_notes`, `short_title`, `amount`, `purpose`, `manager_id`, `csrf`.
2. `requestTypes` accepts `recoverable` (`store/requests.go:78–81`), so VA7 passes.
3. `validateRequestInput` takes the `recoverable` branch, requires the recoverable treatment and delegates to `needsRecoverable` (`store/requests.go:240–244`).
4. The row is created and the actor is redirected to `/requests/{id}/submitted`.

**Alternate flows**
- **10.a** The created request is then editable through `/requests/{id}/edit`, which renders the base form only — none of the four type-specific fieldsets matches `recoverable` (`templates.go:2582`, `2597`, `2607`).

**Exception flows**
- **10.e1** `GET /requests/new?type=recoverable` ⇒ the **type chooser**, silently. `requestTypeLabels` has only four keys (`requests.go:60–66`, `73–76`).
- **10.e2** `treatment=budget` ⇒ **400** *“recoverable type requires recoverable treatment”* (**VT10**).
- **10.e3** Any other validation failure ⇒ `renderRejectedRequestForm` cannot find a label for the type and answers **400** *“That is not a kind of request this system raises.”* — the requester never sees which field was wrong (`requests.go:180–183`).
- **10.e4** On the detail screen `typeLabel` falls through to the raw enum, so the page reads **“recoverable”** rather than a sentence (`requests.go:1017–1022`).

**Business rules**
- BR-10.1 The store’s type vocabulary is five values; the product’s is four. `requestTypeOptions` is “the only way to add one to the product” — `internal/app/requests.go:36–37` — and `recoverable` is not in it.
- BR-10.2 The intended user path to a recoverable is the **treatment**, not the type: `employee_advance` + recoverable (UC-B-09).

**Data touched** — `payment_requests`, `request_number_seq`, `audit_log`, `recoverable_categories`.

**Non-functional / UX notes** — not reachable by keyboard or pointer; no screen to assess. A `recoverable`-type row renders a bare enum in `.rh-meta` and in the CSV `Type` column.

**Open questions** — is `recoverable` intended to remain store-only (as the code implies) or is a fifth card missing (as the overview spec’s type list implies)? **DV1.**

---

### UC-B-11 — Pick a vendor from the combobox

| | |
|---|---|
| **Goal** | “Find the vendor by name, GSTIN or city and attach the right one to my request.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | Typing in the **Vendor** box on `request_form`. |
| **Route(s)** | `GET /vendors/search?q=…` (fragment) |
| **Permission gate** | `vendor:view` (`app.go:433`) — and the combobox is only rendered to a caller who holds it (`templates.go:1900`) |
| **Coverage IDs** | T6, T7, T3 |
| **Priority** | critical |

**Preconditions**
1. Session with `request:create` **and** `vendor:view`.
2. The form type is `vendor_invoice` or `vendor_advance`.
3. At least one vendor with `status='active'`.

**Postconditions (success)** — `#vendor-id` (`input[name="vendor_id"][data-combo-value]`) holds the chosen vendor’s numeric id and `#vendor` shows its name. Nothing is written server-side.

**Postconditions (failure)** — `#vendor-id` unchanged; submitting then fails VT1/VT3 with *“choose a vendor from the vendor master”*.

**Main success scenario**
1. Actor types at least one character into **Vendor**.
2. After a 250 ms debounce htmx issues `GET /vendors/search?q=<text>` — the input carries `name="q"`, which is what puts the term in the query string (`templates.go:1902–1904`).
3. System searches **active vendors only**, matching name, display name, GSTIN or city, prefix matches ranked first, limit 10 (`store/vendors.go:335–356`).
4. System renders `vendor_combo_options`: a `.combo-list` with `role="listbox"`, `aria-label="Vendor results"`, and one `<a class="co" role="option" data-id data-name>` per hit (`templates.go:1365–1369`).
5. htmx swaps it into `#vendor-options`.
6. Actor clicks a result. `fervid-app.js` writes `data-id` into `#vendor-id`, writes `data-name` into the visible box, empties the list and prevents the default navigation (`fervid-app.js:386–398`).
7. Actor continues the form; **only the id is submitted as `vendor_id`**.

**Alternate flows**
- **11.a** No match ⇒ a non-clickable `.co` reading **“No vendor matches “<q>””** with *“Only active vendors can be picked.”*
- **11.b** Empty `q` ⇒ `SearchVendors` returns nothing and the list reads **“Type a name, GSTIN or city”** (`store/vendors.go:337–339`).
- **11.c** Caller holds `vendor:create` ⇒ a trailing **“＋ Add a new vendor”** row linking to `/vendors/new` (`templates.go:1368`).
- **11.d** No JavaScript ⇒ the `<noscript>` `<select id="vendor-plain" name="vendor_id">` (label **“Or pick from the list”**) posts the id; the click handler is never installed and the `.co` rows stay ordinary links to `/vendors/{id}` (`templates.go:1908–1914`, `fervid-app.js:383–385`).
- **11.e** Caller lacks `vendor:view` ⇒ no combobox at all; a plain `<select id="vendor" name="vendor_id">` built from `vendorChoices` (active, limit 500) is rendered instead (`requests.go:286–292`, `templates.go:1919–1925`).

**Exception flows**
- **11.e1** The **edit** screen’s copy of this control (`request_vendor_field`) gives its combo input **no `name` attribute** (`templates.go:2486`), so htmx sends no `q`; the list can only ever render the empty-state row. The vendor on a pending request therefore cannot be changed through the combobox — **DS2**.
- **11.e2** A forged `vendor_id` for a non-existent vendor ⇒ **500** (UC-B-41).
- **11.e3** An inactive vendor’s id, forged, is accepted: `validateRequestInput` only checks `VendorID > 0` (`store/requests.go:167–172`).

**Business rules**
- BR-11.1 The visible text is never trusted; the hidden id is what the server reads, and it comes from the vendor master — `internal/app/templates.go:1895–1899`, `fervid-app.js:378–385`.
- BR-11.2 The hidden `vendor_id` is rendered **after** the `<noscript>` select so that with scripting off the select is the first `vendor_id` in the body and therefore wins — `internal/app/templates.go:1915–1917`.
- BR-11.3 Bank details stay on the vendor record and never appear on this form (T3) — hint text at `internal/app/templates.go:1918`.

**Data touched** — reads `vendors`.

**Non-functional / UX notes** — the list is a `role="listbox"` of `role="option"` links; keyboard users tab into each option (there is no arrow-key handler). At 390px the list is full width under the input. `aria-required="true"` on the combo input, never `required` — the control is inside a `data-when`-free fieldset but the same rule is applied for consistency.

**Open questions** — whether `#vendor-options` (page) versus `.combo-list` (fragment root) should both be targeted by tests; both selectors appear in the current suite.

---

### UC-B-12 — Attach a supporting document to a request

| | |
|---|---|
| **Goal** | “Send the invoice or receipt along with the request.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | Choosing a file in **Add invoice, receipt or proof** before submitting, or **Add another document** / **Attach the corrected document** on the edit screens. |
| **Route(s)** | `POST /requests` · `POST /requests/{id}/edit` |
| **Permission gate** | `request:create` / `request:edit` + CSRF. Downloading later needs `attachment:view` **plus** the request's own row scope, on the request's own route — `GET /requests/{id}/attachments/{attachmentID}` (`app.go:460`, handler `app.go:1115`, `templates.go:2510`). See the correction under Postconditions. |
| **Coverage IDs** | T10, N7, C2 |
| **Priority** | high |

**Preconditions**
1. The form is `enctype="multipart/form-data"` (`templates.go:1828`, `2539`, `2695`).
2. The file is at most 20 MiB and the whole body at most 21 MiB.
3. `AttachmentDir` is writable.

**Postconditions (success)**
1. The file is on disk at `<AttachmentDir>/<unixnano>-<basename>`, mode `0600` (`app.go:930–933`).
2. A `request_attachments` row records `original_name`, `stored_path`, `mime_type`, `size_bytes`, `uploaded_by` (`store/requests.go:436–439`).
3. On create, the row is written **in the same transaction** as the request. On edit, it is a separate `AddRequestAttachment` call that also writes an audit row action `attach`, summary *“Uploaded attachment <name>”* (`store/requests.go:1051–1080`).
4. The thread gains an `attachment` entry rendered as `📎 <name> · <size>` with glyph `⇪` (`templates.go:2182`, `requests.go:1086`).
5. The **Attachments** section on the detail screen lists it with a `.f-ico` extension tag and a **Download** button for anyone holding `attachment:view` (`templates.go:2387–2396`).

**Correction (Wave 3, commit `1fac147`)** — at the time of this audit the **Download** button pointed at `GET /attachments/{id}`, one route shared by payment attachments and request documents over two independent id sequences and gated on `attachment:view` alone. Two holes followed: id `7` could resolve to either table (F-A-05/F-B-09), and the verb by itself — a Requester grant — read every request's documents (F-A-01/F-B-11). The routes are now split and both re-check the request the file hangs off: `GET /attachments/{id}` serves **payment** documents only and refuses anybody the payment's own request is not visible to (`app.go:459`, handler `attachmentDownload` at `app.go:1091`), and a request document is served by the request's own route, `GET /requests/{id}/attachments/{attachmentID}`, which checks the attachment belongs to the request in the path (`app.go:460`, handler `requestAttachmentDownload` at `app.go:1115`). The detail screen's link is `/requests/{{$.Request2.ID}}/attachments/{{.ID}}` (`templates.go:2510`).

**Postconditions (failure)** — no `request_attachments` row and **no file left on disk**: `removeStagedAttachment` deletes the staged path on every error path (`requests.go:163`, `667`; `http_errors.go:238–249`).

**Main success scenario**
1. Actor sets a file on `input[name="attachment"]`.
2. Actor presses **Submit request**.
3. `withCSRF` parses the multipart body (32 MiB in-memory threshold `1<<20` plus temp files, `app.go:559`).
4. `stageUploadedAttachment` copies at most 20 MiB + 1 byte, closes, and rejects anything larger (`app.go:913–953`).
5. `CreateRequest` validates the metadata (`validateAttachment`, `store/store.go:1674–1679`) and inserts the row inside the request transaction.

**Alternate flows**
- **12.a** No file chosen ⇒ `http.ErrMissingFile`/`ErrNotMultipart` are **not** errors; the request is created with no attachment (`app.go:915–921`).
- **12.b** A file whose `Filename` is blank is silently ignored (`app.go:925–927`).
- **12.c** Editing adds a **second** attachment; existing ones are listed above the uploader and are not replaced (`templates.go:2624`).
- **12.d** `require_attachments=1` ⇒ UC-B-13.

**Exception flows**
- **12.e1** File > 20 MiB ⇒ **400** *“validation failed: files must be 20 MiB or smaller”*, staged file removed.
- **12.e2** Body > 21 MiB ⇒ **413** *“The submitted form is too large.”*
- **12.e3** Disk write failure ⇒ **500** and the partial file is removed.
- **12.e4** Caller lacks `attachment:view` ⇒ the file row renders with no **Download** control (`templates.go:2510`).
- **12.e5** Caller holds `attachment:view` but the request is outside their data scope ⇒ **404** *“The requested attachment was not found.”* — `requestAttachmentDownload` resolves the attachment, loads its request and applies `canViewRequest` before opening a byte (`app.go:1115`, refusal at `notFoundAttachment`, `app.go:1044–1048`). Added in Wave 3 (F-A-01/F-B-11); at the time of this audit the verb alone was enough.
- **12.e6** Caller substitutes an `{attachmentID}` belonging to a different request ⇒ the same **404**: the handler compares the attachment's `RequestID` with the `{id}` in the path, *“otherwise the path's {id} is decoration and the route is /attachments/{id} again under a longer name”* (`app.go:1115`). Added in Wave 3 (F-A-05/F-B-09).

**Business rules**
- BR-12.1 D1 leaves no earlier moment at which a file could be attached, so the attachment and the request commit together — `internal/store/models.go:389–392`.
- BR-12.2 There is no delete-attachment route; a document, once on a request, stays (no `attachment:delete` in the vocabulary, `store/permissions.go:155`).
- BR-12.3 The advertised size on screen is `attachment_max_mb` (10 by default) while the enforced cap is 20 MiB — the two are independent (**DV5**).

**Data touched** — writes `request_attachments`, `audit_log`; the filesystem under `AttachmentDir`. Reads `app_settings`.

**Non-functional / UX notes** — the uploader is a `.uploader` block with a visible `input type="file"` and an explicit `for="attachment"` label, so it is keyboard reachable and has an accessible name — unlike the payment screens’ hidden uploader. At 390px it is full width. Empty state under **Attachments** on the detail screen is *“No documents attached.”*

**Open questions** — MIME type is taken from the client’s `Content-Type` header with no server-side sniffing and no extension allow-list on the **request** form (the payment uploaders carry `accept=".pdf,.jpg,.jpeg,.png"`; the request ones do not).

---

### UC-B-13 — Submit with attachments compulsory and no document to hand

| | |
|---|---|
| **Goal** | “The rule says attach a document, but the invoice arrives Monday — let me say so.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | Admin (AC4), who turned the rule on |
| **Scope / level** | system · subfunction |
| **Trigger** | Submitting with `require_attachments=1` and no file. |
| **Route(s)** | `POST /requests` · `POST /requests/{id}/edit` with `submit_action=resubmit` |
| **Permission gate** | `request:create` / `request:edit` + CSRF |
| **Coverage IDs** | T10, G10 |
| **Priority** | high |

**Preconditions**
1. `app_settings.require_attachments = '1'` — set on `/configuration` by the toggle **“Require a supporting document on every request”** (`configuration.go:55`).
2. Session with `request:create`.

**Postconditions (success)** — the request is created with zero attachments and a non-blank `attachment_exception_reason`; the detail screen renders the info banner **“No document was attached”** with the reason underneath (`templates.go:2334–2339`).

**Postconditions (failure)** — nothing written; the form returns at **400**.

**Main success scenario**
1. Actor opens the form; **Supporting document** carries `*` rather than the `optional` badge, and a new field **“If you cannot attach a document, say why”** is rendered with hint *“A missing document never blocks you — it asks for this instead, and your approver sees it.”* (`templates.go:1989`, `1999–2005`).
2. Actor leaves the file input empty and fills the exception reason.
3. Actor presses **Submit request**.
4. `attachmentsRequired` reads the setting; `validateAttachmentPolicy(true, 0, reason)` passes because the reason is non-blank (`store/requests.go:389–395`, `533–546`).
5. The request is created as normal.

**Alternate flows**
- **13.a** A file **is** attached ⇒ the policy passes on the count alone and the exception reason is irrelevant (`store/requests.go:534`).
- **13.b** `require_attachments=0` ⇒ neither the asterisk nor the exception field is rendered, and the policy is a no-op.
- **13.c** **Resubmitting a returned request** re-runs the policy against the **stored** attachment count and the **stored** exception reason (`SubmitRequest`, `store/requests.go:472–482`).

**Exception flows**
- **13.e1** No file and a blank reason ⇒ **400** *“validation failed: attach a supporting document, or say why you cannot”*.
- **13.e2** Same on resubmit: a returned request with neither cannot go back to `pending`.
- **13.e3** `request_returned` posts the stored reason as a hidden input (`templates.go:2705`), so a resubmit never blanks it by omission.

**Business rules**
- BR-13.1 **G10 — the system never hard-blocks on a missing document.** It asks for a written reason, because legitimate documents are sometimes genuinely unavailable — `internal/store/requests.go:530–532`.
- BR-13.2 `UpdateRequest` does **not** re-run the attachment policy; only `CreateRequest` and `SubmitRequest` do (`store/requests.go:577–643` has no `validateAttachmentPolicy` call). Saving a correction without resubmitting therefore never trips it.

**Data touched** — reads `app_settings`, `request_attachments`; writes `payment_requests.attachment_exception_reason`.

**Non-functional / UX notes** — the exception input is `aria-required="true"` (never `required`), so the browser does not block the submit and the server owns the message. Placeholder: *“Vendor posts the invoice; it arrives Monday”*. Full width at every breakpoint.

**Open questions** — none.

---

### UC-B-14 — Mark a request urgent

| | |
|---|---|
| **Goal** | “This one cannot wait — tell my approver now.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | Manager (AC2); Accounts (AC3) once approved |
| **Scope / level** | system · subfunction |
| **Trigger** | Checking **Mark this urgent** on the form. |
| **Route(s)** | `POST /requests` |
| **Permission gate** | `request:create` + CSRF |
| **Coverage IDs** | T5, N6 |
| **Priority** | medium |

**Preconditions**
1. `urgency_mode` is `reason` (default) or `free` — not `disabled`.
2. Session with `request:create`.

**Postconditions (success)**
1. `payment_requests.urgent = 1` and, under `reason`, `urgency_reason` non-blank.
2. The request sorts **first** in every list: `ORDER BY r.urgent DESC, …` (`store/requests.go:1278`).
3. A `.pill.urgent` reading **“Urgent”** appears on the card, the head and the dashboard row (`templates.go:2056`, `2260`, `3236`).
4. The detail screen’s **Urgency** row reads *“Urgent — <reason>”* (`templates.go:2360`).
5. `notify.EventRequestUrgent` fires **in addition to** `request_submitted`, and again on approval (`notifications.go:44–51`).

**Postconditions (failure)** — nothing written.

**Main success scenario**
1. Actor checks **Mark this urgent**; hint reads *“Urgent requests follow the same approval rules. They send an immediate email to your approver, and to Accounts once approved.”* (`templates.go:1876`).
2. `data-when="urgent:on"` un-hides **Why is it urgent** client-side (`fervid-app.js:187`, `209`).
3. Actor fills the reason (placeholder *“Supply stops if this is not cleared by Monday”*).
4. Actor submits; `validateUrgency` passes.

**Alternate flows**
- **14.a** `urgency_mode=free` ⇒ no reason field, no reason required (`store/requests.go:520–521`).
- **14.b** Checkbox left clear ⇒ `validateUrgency` returns immediately; the reason, if forged, is still stored but never shown (the **Urgency** row reads *“Normal”*).
- **14.c** On the **edit** screen the same checkbox is labelled **“Marked urgent”** (`templates.go:2570`).

**Exception flows**
- **14.e1** `urgent=on` with a blank reason under `reason` mode ⇒ **400** *“say why this is urgent”*.
- **14.e2** `urgency_mode=disabled` and `urgent=on` forged ⇒ **400** *“urgent requests are switched off”*; the control is not rendered at all in that mode.

**Business rules**
- BR-14.1 **G7/N6 — urgency changes who is told and how soon, never who may decide.** No extra authority attaches — `internal/store/requests.go:511–512`, `internal/app/configuration.go:64`.
- BR-14.2 The rule is configuration-driven and therefore lives **outside** the pure `validateRequestInput` — `internal/store/requests.go:511–513`.

**Data touched** — writes `payment_requests.urgent`, `.urgency_reason`. Reads `app_settings.urgency_mode`.

**Non-functional / UX notes** — the checkbox is a `label.checkline` wrapping the input, so its whole text is the hit area (≥40px). The revealed reason field starts `hidden` in the markup when the box is clear, so a Playwright `toBeVisible` on it must follow the check.

**Open questions** — none.

---

### UC-B-15 — Read the submitted confirmation

| | |
|---|---|
| **Goal** | “I pressed Submit — what happens now, and who has it?” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | The **303** from `POST /requests`, or from `POST /requests/{id}/reraise`. |
| **Route(s)** | `GET /requests/{id}/submitted` |
| **Permission gate** | `request:view` (`app.go:447`) **plus** the row-level scope test in `loadViewableRequest` |
| **Coverage IDs** | L2, D1, C4, N1 |
| **Priority** | high |

**Preconditions**
1. The request exists.
2. The caller’s `request` scope reaches the row: `all`, or `assigned` and they are its manager or requester, or `own` and they raised it (`requests.go:332–343`).

**Postconditions (success)** — a read-only forecast screen. Nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**
1. System loads the request and checks the scope.
2. System renders `request_submitted`: a `.banner.good` **“Submitted. <approver> has been notified.”** with *“You can still edit this request until they act on it. Every edit tells them again and restarts the three-day reminder clock.”* (`templates.go:3000–3007`).
3. The `.req-head` shows the number in `.rh-no`, the amount in `.rh-amt`, the short title as `h1`, and a meta line of type · recoverable category (when recoverable) · project · head · needed-by (`templates.go:3009–3015`).
4. Status pill reads **“Awaiting approval”**; the waiting line reads **“Waiting on <approver>”** (`templates.go:3017–3018`).
5. A three-step **“What happens next”** `.thread` forecasts review → Accounts picks it up → it settles (`templates.go:3022–3042`).
6. The action bar offers **Raise another** (`/requests/new`), **My requests** (`/requests`) and **View this request** (`/requests/{id}`) (`templates.go:3044–3049`).

**Alternate flows**
- **15.a** Reached after a re-raise ⇒ the same screen, for the **new** request with its own number (UC-B-21).
- **15.b** A recoverable request ⇒ the meta line carries `· <category label>`.

**Exception flows**
- **15.e1** Unknown id ⇒ **404** *“The requested record was not found.”*
- **15.e2** Another requester’s id under scope `own` ⇒ **404** *“The requested record was not found.”* (UC-B-42). *(403 “You do not have permission to view this request.” at `30edd6a`; changed by F-G-002, Wave 3, `1fac147` — `loadViewableRequest`, `internal/app/requests.go:295-308`.)*
- **15.e3** No `request:view` ⇒ **403** from the middleware.

**Business rules**
- BR-15.1 The three-step thread is a **forecast, not a history**; the real history lives on the detail screen — `internal/app/templates.go:2993–2997`.
- BR-15.2 The status pill and the waiting sentence are computed by the same helpers the list uses, so the three screens cannot drift (`pillClass`, `requestStatusText`, `waitingOn`).

**Data touched** — reads `payment_requests` (+ joins).

**Non-functional / UX notes** — `h1` is the short title, so the UX sweep’s visible-`h1` rule is satisfied on a phone. Three action-bar controls wrap at 390px inside the sticky bar. No empty state.

**Open questions** — the banner asserts an email was sent even when the `request_submitted` event has email disabled; in-app delivery always fires (`notifications.go:19–21`), so the sentence is true only of the in-app notification.

---

### UC-B-16 — Read one request, whoever I am

| | |
|---|---|
| **Goal** | “Show me everything about this request and only the actions I am actually allowed.” |
| **Primary actor** | Requester (AC1) · Manager (AC2) · Accounts (AC3) · Admin (AC4) |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | A card on `/requests`, a row on `/approvals`, a dashboard row, or a notification link. |
| **Route(s)** | `GET /requests/{id}` |
| **Permission gate** | `request:view` (`app.go:488`) **plus** `canViewRequest` scope test |
| **Coverage IDs** | Q4, Q5, Q6, N7, A5, L3–L7, C2 |
| **Priority** | critical |

**Preconditions**
1. The request exists and the caller’s scope reaches it.

**Postconditions (success)** — one screen, rendered from `request_detail`, whose `.action-bar` contents depend on permission **and** the reader’s seat on this row. Nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**
1. System loads the request (`loadViewableRequest`).
2. If the reader raised it, it is `returned`, and they hold `request:edit`, the system renders **`request_returned`** instead — a correction form, not a read-only page (`requests.go:517–525`, `534–537`) ⇒ UC-B-19.
3. Otherwise the system loads the merged thread, the attachments and any linked payment; `ErrNotFound` on the payment is an answer, not a failure (`requests.go:543–564`).
4. System renders `request_head` — number, amount, short title as `h1`, meta *“<type> · <project> / <head> · raised by <name> on <date>”*, the status pill and the waiting sentence (`templates.go:2251–2270`).
5. System renders state banners, at most the ones that apply: **“Approved requests are locked”** (requester, approved, not held), **“This request is on hold”** (L7), **“Payment is frozen”** (cancellation requested), **“<manager> sent this back”** (returned), **“<manager> rejected this”** (rejected), **“No document was attached”** (`templates.go:2288–2339`).
6. System renders the **Details** card with a treatment pill (**Recoverable · <category>** or **Budget expense**) and a `dl` of Amount, Approved, Needed by, Project, Head, **Vendor** or **Paid to**, GSTIN, Invoice number, Invoice date, Expense date, Advance reason, Counterparty, Expected return, Repayment terms, Requested by, Approver, Urgency, Purpose (`templates.go:2341–2362`).
7. When a payment exists, the system renders **Payment outcome** — Approved beside Paid, and the difference named as *“Still owed to the payee”* (partial) or *“Difference · confirmed settled by Accounts”* (Q4, `templates.go:2368–2385`).
8. System renders **Attachments** and then the merged **History and conversation** thread (UC-B-17).
9. System renders exactly one `.action-bar`, gated per row.

**Alternate flows**
- **16.a** Reader raised it and it is `pending` or `returned` and they hold `request:edit` ⇒ **Edit request** link (`templates.go:2413`).
- **16.b** Reader raised it, `pending`, holds `request:withdraw` ⇒ **Withdraw** submit (`templates.go:2416`).
- **16.c** Reader raised it, `approved`, holds `request:cancel` ⇒ **Request cancellation** link (`templates.go:2419`).
- **16.d** Reader raised it, `rejected`, holds `request:reraise` ⇒ **Raise it again** submit (`templates.go:2422`).
- **16.e** Reader is the row’s manager and it is `pending` ⇒ **Reject**, **Return for correction**, **Approve ₹X**, each gated on its own verb, plus the three sheets (`templates.go:2425–2429`, `2472`).
- **16.f** Reader is the manager, `approved`, holds `approval:cancel` ⇒ **Cancel with reason** (`templates.go:2430`).
- **16.g** Reader is the manager, `cancellation_requested`, holds `approval:cancel` ⇒ **Decide the cancellation** (`templates.go:2433`).
- **16.h** Reader is the manager, `partial_review`, holds `approval:accept_partial` ⇒ **Decide the partial payment** (`UC-C-*`).
- **16.i** Reader holds `payment:hold` ⇒ **Put on hold** / **Release hold** / **Keep on hold** (`UC-C-*`, L7).
- **16.j** A linked payment and `payment:view` ⇒ **View the payment**.
- **16.k** On hold: the requester may still comment and may not change any field (Q6) — the only control offered to them is the comment box.

**Exception flows**
- **16.e1** Unknown id ⇒ **404**.
- **16.e2** Out of scope ⇒ **404** *“The requested record was not found.”* *(403 at `30edd6a`; F-G-002, Wave 3, `1fac147` — `requests.go:295-308`.)*
- **16.e3** No `request:view` ⇒ **403** from the middleware.
- **16.e4** A reader who is neither requester nor manager and holds no decision verb sees an action bar containing only `.row-end` — an empty bar, not a missing one.

**Business rules**
- BR-16.1 One screen for every audience; only the action bar differs, and it differs on **permission plus seat**, never on a role name and never on the URL the reader arrived from — `internal/app/requests.go:505–507`, `internal/app/templates.go:2272–2278`.
- BR-16.2 `on_hold=1` implies `status='approved'`, so the hold pill replaces the status pill rather than joining it — `internal/app/templates.go:2261–2266`.
- BR-16.3 Only Accounts lifts a hold — `internal/app/templates.go:2442–2444`.
- BR-16.4 There is exactly **one** action bar, because at phone width it is sticky and a second would sit on top of it — `internal/app/templates.go:2407–2409`.

**Data touched** — reads `payment_requests`, `audit_log`, `request_comments`, `request_attachments`, `payments`, `vendors`, `projects`, `heads`, `users`.

**Non-functional / UX notes** — at 390px the `dl.dl` collapses to one column, the action bar is sticky above the tab bar, and `.ab-note` items are `d-only`. Empty thread renders *“Nothing has happened yet.”*; empty attachments render *“No documents attached.”*

**Open questions** — none.

---

### UC-B-17 — Add a comment to the conversation

| | |
|---|---|
| **Goal** | “Ask a question or answer one, without returning the request.” |
| **Primary actor** | anyone who can see the request and holds `request:comment` |
| **Supporting actors** | every other viewer of the request |
| **Scope / level** | system · subfunction |
| **Trigger** | **Post comment** under the thread. |
| **Route(s)** | `POST /requests/{id}/comment` |
| **Permission gate** | `request:comment` (`app.go:491`) **plus** `loadViewableRequest`’s scope test (`requests.go:769`) |
| **Coverage IDs** | N7, Q6, C2 |
| **Priority** | high |

**Preconditions**
1. The request exists and the caller’s scope reaches it.
2. The caller holds `request:comment` — Requester, Manager, Accounts and Admin all do (`migrations.go:357`, `369`, `383`, and `adminGrants()`).
3. The comment body is non-blank after trimming.

**Postconditions (success)**
1. A `request_comments` row: `request_id`, `author_id`, `body` (`store/requests.go:1014`).
2. An `audit_log` row action `comment`, summary *“Commented on request”*, `after.comment_id` (`store/requests.go:1022–1025`).
3. The comment appears in the merged thread as `Kind="comment"`, headed by the author’s name, with the author’s initials in the `.tl-dot` and `class="is-comment is-me"` for its own author (`templates.go:2177–2181`, `store/requests.go:1188–1190`).
4. The `comment` audit action is **suppressed** from the thread’s event stream so the words appear once, not twice (`store/requests.go:1176`).
5. Redirect **303** to `/requests/{id}`.

**Postconditions (failure)** — no comment row, no audit row.

**Main success scenario**
1. Actor scrolls to **History and conversation**, headed by *“Everyone who can see this request sees this whole stream”* (`templates.go:2171–2173`).
2. Actor types into **Add a comment** (placeholder *“Anyone who can see this request will see your comment.”*).
3. Actor presses **Post comment**.
4. System loads and scope-checks the request, trims the body, writes the comment and the audit row in one transaction, redirects.

**Alternate flows**
- **17.a** The request is on hold ⇒ the comment box is the requester’s only control, and commenting changes no field (Q6).
- **17.b** From the partial-review screen with `return_to=partial-review` and status `partial_review` ⇒ the redirect lands back on `/requests/{id}/partial-review`. The field carries a **token, never a URL**, so nothing a form says can redirect off-site (`requests.go:778–785`).
- **17.c** A caller without `request:comment` ⇒ the whole `form.comment-box` is absent (`templates.go:2189`).

**Exception flows**
- **17.e1** Blank or whitespace-only body ⇒ **400** *“validation failed: comment cannot be empty”*. The `<textarea required>` also blocks it client-side (`templates.go:2193`).
- **17.e2** Out of scope ⇒ **404** before any write *(403 at `30edd6a`; F-G-002, Wave 3, `1fac147` — `requests.go:295-308`)*.
- **17.e3** Unknown id ⇒ **404**.
- **17.e4** No CSRF ⇒ **403** (CV4).

**Business rules**
- BR-17.1 Visibility follows the request: everyone who can see the request sees the whole stream (N7) — there is no private-comment flag anywhere in `request_comments` (`migrations.go:220–227`).
- BR-17.2 The thread is one chronological stream of audit events, comments and uploads, tie-broken on audit `id` because `CURRENT_TIMESTAMP` resolves only to the second — `internal/store/requests.go:1158–1168`.
- BR-17.3 A comment is the alternative to returning a request, which is why the box lives inside the thread — `internal/app/templates.go:2167–2168`.
- BR-17.4 `concern` audit rows are also suppressed, because `RaiseConcern` writes both a comment row and an audit row whose summary is that comment (`store/requests.go:1173–1177`).

**Data touched** — writes `request_comments`, `audit_log`. Reads `payment_requests`, `audit_log`, `request_comments`, `request_attachments`, `users`.

**Non-functional / UX notes** — `<label for="cmt" class="flabel">Add a comment</label>` is a real label, so `getByLabel` resolves. The button is `.btn.primary.small` — verify it still clears the 40px tap floor at 390px. Empty thread state is *“Nothing has happened yet.”*

**Open questions** — commenting is allowed in every status, including `rejected`, `withdrawn` and `cancelled`; nothing closes a dead request’s thread.

---

## 3. Requester actions after submission

### UC-B-18 — Edit a request while it is still pending

| | |
|---|---|
| **Goal** | “I got the amount wrong — fix it before my approver looks, and tell them again.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | Manager (AC2), re-notified; the reminder scheduler (AC5) |
| **Scope / level** | system · user-goal |
| **Trigger** | **Edit request** on the detail screen of a `pending` request. |
| **Route(s)** | `GET /requests/{id}/edit` · `POST /requests/{id}/edit` |
| **Permission gate** | `request:edit` (`app.go:489`, `490`) **plus** `loadEditableRequest`: requester-only and status ∈ {`pending`,`returned`} (`requests.go:587–602`) |
| **Coverage IDs** | A8, Q1, T12, C2, L2 |
| **Priority** | critical |

**Preconditions**
1. The caller raised the request (`req.RequesterID == actor.ID`).
2. Status is `pending`.
3. The caller holds `request:edit`.
4. Every VA/VT rule for the request’s type will still hold after the edit.

**Postconditions (success)**
1. The row is updated across `treatment, type, recoverable_category, recoverable_category_id, project_id, head_id, vendor_id, vendor_payee, short_title, amount, purpose, needed_by, invoice_no, invoice_date, expense_date, advance_reason, counterparty, expected_return_date, repayment_notes, urgent, urgency_reason, attachment_exception_reason, manager_id` (`store/requests.go:618–630`).
2. **`reminder_last_sent` is set to NULL** — the three-day clock restarts (`store/requests.go:622`).
3. `manager_id` is written from the form, so choosing a different approver **reroutes** the request to them.
4. One `audit_log` row action `update`, summary *“<name> edited request <number>”*, with full before/after snapshots; the thread renders the diff as `.tl-change` lines for amount, approved amount, needed by, invoice number, invoice date, expense date, approver, short title, purpose and urgency (`store/requests.go:637–640`, `1097–1098`).
5. `notify.EventRequestEdited` fires (`requests.go:676`).
6. Redirect **303** to `/requests/{id}`.
7. The status stays `pending`; nothing about the edit changes it.

**Postconditions (failure)** — the row is unchanged, `reminder_last_sent` untouched, no audit row, no notification, and any staged file is deleted (`requests.go:666–675`).

**Main success scenario**
1. Actor presses **Edit request**.
2. System refuses anybody who is not the requester and any status outside {`pending`,`returned`} **before** rendering (`requests.go:587–602`).
3. System renders `request_edit`: eyebrow *“<number> · <status text>”*, `h1` **“Edit request”**, sub = the short title, and an info banner **“Editing tells <manager> again”** with *“Every change is recorded in the history, notifies your approver, and restarts the three-day reminder clock. Change the approver and it moves to that person instead.”* (`templates.go:2522–2537`).
4. The type travels as a hidden input and **is not editable**; so does the treatment (`templates.go:2541–2542`).
5. Actor changes **Amount**.
6. Actor presses **Save and notify <manager>**.
7. System reads the whole form, stages any file, calls `UpdateRequest`, adds the attachment, and — because `submit_action` is not `resubmit` — skips `SubmitRequest` (`requests.go:651–665`).
8. System fires `request_edited` and redirects to the detail screen, where the thread shows *Amount ₹18,400.00 → ₹21,500.00*.

**Alternate flows**
- **18.a** Actor changes **Approver** ⇒ the request moves to that person; the hint says so (`templates.go:2650`). The old approver keeps only the audit trail.
- **18.b** Actor adds a document via **Add another document** ⇒ UC-B-12.c.
- **18.c** Status is `returned` ⇒ the same POST route serves UC-B-19, and `renderRejectedEdit` re-renders `request_returned` rather than `request_edit` on failure (`requests.go:690–694`).
- **18.d** Actor presses **Discard changes** ⇒ a plain link back to `/requests/{id}`; nothing is written.

**Exception flows**
- **18.e1** Not the requester ⇒ **403** *“Only the person who raised a request may edit it.”* (`requests.go:593`).
- **18.e2** Status `approved` ⇒ **400** *“This request can no longer be edited. It is approved and locked; ask for it to be cancelled instead.”* (`requests.go:597–599`, `606–607`).
- **18.e3** Status `rejected` ⇒ **400** *“… A rejected request is final — raise a new one.”* (UC-B-38).
- **18.e4** Status `cancellation_requested` ⇒ **400** *“… A cancellation is already pending on it.”*
- **18.e5** Any validation failure ⇒ **400** and `request_edit` re-renders with the posted values overlaid on the stored row by `editedRequest`, so the number, status, names, approved amount and timestamps are never lost (`requests.go:700–711`).
- **18.e6** A store-level race — the row moved out of an editable status between the GET and the POST — ⇒ **400** *“a <status> request cannot be edited”* from `UpdateRequest` (`store/requests.go:613–615`).
- **18.e7** The request’s stored project or head has since been **deactivated** ⇒ the `<select>` cannot render the stored option (both lists are active-only), nothing is selected, `project_id`/`head_id` post empty and the save fails with *“project and head are required for this type”* — **DS5**.

**Business rules**
- BR-18.1 **A8** — an edit while pending records history, re-notifies, resets the reminder clock and reroutes. All four are one statement in `UpdateRequest` — `internal/store/requests.go:616–630`.
- BR-18.2 Only the person who raised a request may correct it; an approver who wants a change **returns** it instead, which is a different verb with a different audit line — `internal/app/requests.go:566–569`.
- BR-18.3 The type is what the request *is* and travels as a hidden input (A16) — `internal/app/templates.go:2518–2519`.
- BR-18.4 `editableStatuses` is `{returned, pending}`; `draft` is gone (D1) — `internal/store/requests.go:573–575`.
- BR-18.5 The attachment policy is **not** re-checked on a plain save (BR-13.2).

**Data touched** — writes `payment_requests` (22 columns + `reminder_last_sent`, `updated_at`), `request_attachments`, `audit_log`, `notifications`. Reads `projects`, `heads`, `users`, `app_settings`, `recoverable_categories`, `vendors`.

**Non-functional / UX notes** — the primary button text is dynamic (**Save and notify <manager name>**), so tests must use `/^Save and notify/`. At 390px the fieldsets stack and the action bar is sticky. First focusable field is **Short title**.

**Open questions** — the edit screen wires no duplicate check, so changing an amount to a look-alike is never flagged.

---

### UC-B-19 — Correct a returned request and resubmit it

| | |
|---|---|
| **Goal** | “My approver sent it back — fix what they asked for and send it again, same number.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | Manager (AC2) |
| **Scope / level** | system · user-goal |
| **Trigger** | Opening `/requests/{id}` on a request the caller raised whose status is `returned`. |
| **Route(s)** | `GET /requests/{id}` (renders the correction screen) · `POST /requests/{id}/edit` |
| **Permission gate** | `request:view` to reach the screen; `request:edit` both to be offered it and to post (`requests.go:534–537`, `app.go:490`) |
| **Coverage IDs** | Q1, L3, L11, A8, T10, C2 |
| **Priority** | critical |

**Preconditions**
1. Status is `returned` and `decision_reason` carries the approver’s words.
2. The caller raised it and holds `request:edit`.
3. If `require_attachments=1`, either an attachment exists or `attachment_exception_reason` is non-blank (VG3).

**Postconditions (success)**
1. The row is updated exactly as UC-B-18, **and then** `SubmitRequest` sets `status='pending'`, `submitted_at=CURRENT_TIMESTAMP`, `reminder_last_sent=NULL` (`store/requests.go:483`).
2. **The number and the whole history are retained** — nothing is copied.
3. Two audit rows: `update` then `submit` with summary *“<name> resubmitted request <number>”* (`store/requests.go:488–491`).
4. `notify.EventRequestEdited` fires (`requests.go:676`).
5. Redirect **303** to `/requests/{id}`; the pill reads **“Awaiting approval”** again.

**Postconditions (failure)** — the row keeps status `returned` and every stored value; no audit rows; staged file removed.

**Main success scenario**
1. Actor opens `/requests/{id}`.
2. `showsReturnedCorrection` is true, so the system renders **`request_returned`** rather than `request_detail` (`requests.go:517–525`).
3. The screen shows `request_head`, a `.banner.warn` **“<manager> sent this back”** quoting `decision_reason`, and the hint *“Returned is not rejected. This request keeps its number and its history — correct it and send it again.”* (`templates.go:2684–2693`).
4. The **Correct and resubmit** fieldset offers only the fields an approver is likely to have queried: **Short title**, then per type **Invoice number**/**Invoice date** (vendor_invoice), **Expense date** (reimbursement), **Reason for the advance** (vendor_advance or employee_advance), then **Amount**, **Purpose** and **Attach the corrected document** (`templates.go:2716–2748`).
5. Every field the store validates but the screen does not show travels as a **hidden input** — `manager_id`, `project_id`, `head_id`, `needed_by`, `urgent`, `urgency_reason`, `attachment_exception_reason`, the four recoverable fields when recoverable, and `vendor_id` for vendor types (`templates.go:2699–2714`).
6. Actor edits **Purpose** and presses **Resubmit for approval**.
7. System runs `UpdateRequest`, then — because `submit_action == "resubmit"` — `SubmitRequest` (`requests.go:663–665`).

**Alternate flows**
- **19.a** Actor presses **Save corrections** (`submit_action=save`) ⇒ the update lands, the status stays `returned`, and the request keeps waiting on the requester. The `.ab-note` says so: *“Saving keeps it with you; resubmitting sends it back to <manager>.”* (`templates.go:2752`).
- **19.b** A reader who did **not** raise it, or who lacks `request:edit`, sees the ordinary `request_detail` with the same *“sent this back”* banner and no form (`templates.go:2322–2327`).
- **19.c** `/requests/{id}/edit` also works on a returned request and renders the fuller `request_edit`; both routes post to the same handler.

**Exception flows**
- **19.e1** Blank required field ⇒ **400**, `request_returned` re-rendered by `renderRejectedEdit` with the message and the typing intact (`requests.go:690–694`).
- **19.e2** `require_attachments=1`, no attachment, blank stored exception reason ⇒ **400** *“attach a supporting document, or say why you cannot”* from `SubmitRequest`. Note the `update` has already been rolled back independently — the two calls are separate transactions, so a failed resubmit **leaves the corrections saved** (`requests.go:658–665`). See **DS6**.
- **19.e3** Status is not `returned` (or `pending`) ⇒ `canTransition(status,"pending")` is false ⇒ **400** *“a <status> request cannot be submitted”* (`store/requests.go:469–471`).
- **19.e4** Not the requester ⇒ **403** from `loadEditableRequest`, and `SubmitRequest` refuses again with `ErrForbidden` (`store/requests.go:466–468`).

**Business rules**
- BR-19.1 “Returned is not rejected. A returned request keeps its number and its history — you correct it and resubmit. A rejected request is final and read-only.” — `internal/app/templates.go:2665–2670`.
- BR-19.2 Correcting and resubmitting are **one press**, because two buttons over two forms would be two sticky action bars on a phone, and saving-then-forgetting is how a returned request sits for a week — `internal/app/templates.go:2672–2675`.
- BR-19.3 A field missing from the body is a field the store reads as **cleared**, which is why the hidden inputs exist — `internal/app/templates.go:2677–2679`.
- BR-19.4 `returned → pending` is the only remaining transition into `pending` after creation (D1) — `internal/store/requests.go:92`, `454–455`.

**Data touched** — writes `payment_requests` (fields + `status`, `submitted_at`, `reminder_last_sent`), `request_attachments`, `audit_log`. Reads the same set as UC-B-18.

**Non-functional / UX notes** — the correction form sits **above** the thread, so the approver’s words and the fields are on one screen at 390px. Two submit buttons share one `name="submit_action"`; a keyboard Enter inside a text field triggers the **first** one — **Save corrections** — not the resubmit. Assert deliberately.

**Open questions** — DS6: whether a failed resubmit should also roll back the corrections.

---

### UC-B-20 — Withdraw a pending request

| | |
|---|---|
| **Goal** | “Never mind — take this out of my approver’s queue.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | **Withdraw** on the detail screen of a `pending` request. |
| **Route(s)** | `POST /requests/{id}/withdraw` |
| **Permission gate** | `request:withdraw` (`app.go:492`) **plus** requester-only in the store (`store/requests.go:655–657`) |
| **Coverage IDs** | Q2, L5, L11, C2 |
| **Priority** | high |

**Preconditions**
1. The caller raised the request.
2. Status is `pending` — the only status with a legal edge to `withdrawn` (`store/requests.go:93`).
3. The caller holds `request:withdraw`.

**Postconditions (success)**
1. `status='withdrawn'`, `updated_at` refreshed (`store/requests.go:661`).
2. One `audit_log` row action `withdraw`, summary *“<name> withdrew request <number>”*, with before/after (`store/requests.go:666–669`).
3. The request leaves the **Open** bucket and joins **Closed** (`requestBuckets`, `store/requests.go:1201–1204`); it also leaves the approver’s **To approve** tab.
4. The waiting line reads **“Withdrawn by the requester”** with class `closed`; the pill uses the `cancelled` modifier (`requests.go:429–430`, `967–971`).
5. It is excluded from future duplicate checks (`store/requests.go:1328`).
6. Redirect **303** to `/requests/{id}`.
7. **Added since Waves 2/4 (F-F-06):** one `notifications` row per resolved recipient. `requestWithdraw` fires `notify.EventRequestWithdrawn` — *“the approver's queue item has just disappeared without a decision, so they are told why”* (`internal/app/requests.go:916`; the constant at `internal/notify/events.go:37`, seeded by migration v9). At `30edd6a` a withdrawal notified nobody, recorded as **DS10**. `Data touched` below therefore also includes `notifications`.

**Postconditions (failure)** — status and every field unchanged; no audit row, no notification.

**Main success scenario**
1. Actor opens their `pending` request.
2. Actor presses **Withdraw** — a `<form method="post">` inline in the action bar, so this is a submit button, not a link (`templates.go:2417`).
3. System loads the row in a transaction, confirms the actor raised it and that `pending → withdrawn` is legal, writes the status and the audit row, commits.
4. System redirects to the detail screen, which now offers the requester **no** actions.

**Alternate flows**
- **20.a** A withdrawn request cannot be re-raised — only a `rejected` one can (UC-B-39). The requester raises a fresh request instead.

**Exception flows**
- **20.e1** Status `approved` ⇒ **400** *“a approved request cannot be withdrawn”*; the control is not rendered in that state, and **Request cancellation** is offered instead (UC-B-37).
- **20.e2** Status `returned`, `rejected`, `cancelled`, `withdrawn`, `processing`, `partial_review`, `completed*` ⇒ **400** *“a <status> request cannot be withdrawn”*.
- **20.e3** Not the requester ⇒ **403** *“You do not have permission to perform this action.”* (`ErrForbidden` → CV5).
- **20.e4** No `request:withdraw` ⇒ **403** from the middleware.
- **20.e5** Unknown id ⇒ **404**.
- **20.e6** No CSRF ⇒ **403**.

**Business rules**
- BR-20.1 Withdrawal is the requester’s own act only while nobody has acted; after approval it is too late and the requester must **ask** (G1) — `internal/store/migrations.go:358–360`.
- BR-20.2 `withdrawn` is terminal: `legalTransitions` gives it no outgoing edges — `internal/store/requests.go:91–101`.
- BR-20.3 The screen never offers a control whose submit the store would refuse — the button is gated on `$mine` **and** `pending` **and** the verb (`internal/app/templates.go:2416`).

**Data touched** — writes `payment_requests.status`, `.updated_at`, `audit_log`, `notifications` (the last since Wave 4 — see postcondition 7).

**Non-functional / UX notes** — no confirmation dialog: withdrawal is reversible in practice (raise again) so the design does not interrupt. The button is `.btn.outline` inside the sticky bar; at 390px it shares the bar with **Edit request**. Focus after the redirect lands at the top of the detail document.

**Open questions** — none.

---

### UC-B-21 — Re-raise a rejected request

| | |
|---|---|
| **Goal** | “It was rejected but the need is real — start again from what I already wrote.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | Manager (AC2), who receives the new request |
| **Scope / level** | system · user-goal |
| **Trigger** | **Raise it again** on the detail screen of a `rejected` request. |
| **Route(s)** | `POST /requests/{id}/reraise` |
| **Permission gate** | `request:reraise` (`app.go:493`) **plus** requester-only in the store (`store/requests.go:815–817`) |
| **Coverage IDs** | Q3, L4, D1, C4, C2 |
| **Priority** | high |

**Preconditions**
1. The caller raised the source request.
2. The source status is exactly `rejected`.
3. The caller holds `request:reraise`.

**Postconditions (success)**
1. A **new** `payment_requests` row, created already `pending`, with **its own number** from `request_number_seq` and `submitted_at=CURRENT_TIMESTAMP` (`store/requests.go:829–838`).
2. Every substantive column is copied by `INSERT … SELECT`: treatment, type, both category columns, project, head, vendor, payee, title, amount, purpose, needed_by, invoice fields, expense date, advance reason, counterparty, return date, repayment notes, urgency, attachment exception reason, requester and **manager** (`store/requests.go:830–838`).
3. The **source row is untouched** — it stays `rejected` and read-only.
4. One `audit_log` row on the **new** id, action `reraise`, summary *“<name> re-raised <old number> as <new number>”*, `after.source`/`after.number` (`store/requests.go:846–850`).
5. Redirect **303** to `/requests/{newID}/submitted`.
6. **Added since Waves 2/4 (F-F-06):** one `notifications` row per resolved recipient, fired for the **new** id, not the rejected one — D1 makes a re-raise a fresh pending request with its own number, so without this the approver never learned the second attempt existed (`notify.EventRequestReraised`, `internal/app/requests.go:932`; the constant at `internal/notify/events.go:41`, seeded by migration v9). At `30edd6a` a re-raise notified nobody.

**Postconditions (failure)** — no new row, no number consumed beyond the transaction rollback, source unchanged, no notification.

**Main success scenario**
1. Actor opens the rejected request; a `.banner.bad` reads **“<manager> rejected this”** with the reason.
2. Actor presses **Raise it again** (a `.btn.primary` submit inside an inline form, `templates.go:2423`).
3. System copies the row, numbers it, audits it and redirects to the confirmation for the **new** request (UC-B-15.a).

**Alternate flows**
- **21.a** Attachments are **not** copied — `request_attachments` rows point at the old id and the new request starts with none. If `require_attachments=1`, the copy still succeeds because `ReraiseRequest` runs no attachment policy (`store/requests.go:805–856`). See **DS7**.
- **21.b** Comments are not copied either; the new request starts with one `reraise` thread entry (glyph `＋`, tone `brand`, `requests.go:1070`, `1093`).

**Exception flows**
- **21.e1** Source status is `withdrawn` ⇒ **400** *“only a rejected request can be re-raised”* (UC-B-39).
- **21.e2** Any other status ⇒ same **400**.
- **21.e3** Not the requester ⇒ **403**.
- **21.e4** No `request:reraise` ⇒ **403** from the middleware. Note the roles matrix bundles `request:reraise` into the **Create** cell (`permmap.go:65`), so it travels with `request:create`.
- **21.e5** Unknown id ⇒ **404**.

**Business rules**
- BR-21.1 **D1** — a re-raise is a fresh **pending** request with its own number, never a draft copy — `internal/store/requests.go:803–804`, `internal/app/requests.go:756–758`.
- BR-21.2 `rejected` is final and read-only; raising again is the only forward path (L4) — `internal/app/requests.go:610–611`.
- BR-21.3 The copy retains the original approver, so a rejection by one manager is re-sent to the same manager unless the requester then edits it.

**Data touched** — writes `payment_requests` (new row), `request_number_seq`, `audit_log`, `notifications` (the last since Wave 4 — see postcondition 6). Reads the source row.

**Non-functional / UX notes** — the button is the screen’s only primary action in that state; at 390px it sits alone in the sticky bar. No confirmation step — the design treats a new pending request as cheap and reversible (withdraw).

**Open questions** — DS7: whether a re-raise under `require_attachments=1` should demand a document, since it produces a pending request with none.

---

### UC-B-22 — Ask for an approved request to be cancelled

| | |
|---|---|
| **Goal** | “It is approved but it should not be paid — freeze it and let my approver decide.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | Manager (AC2) decides; Accounts (AC3) is blocked meanwhile |
| **Scope / level** | system · user-goal |
| **Trigger** | **Request cancellation** on the detail screen of an `approved` request. |
| **Route(s)** | `GET /requests/{id}/cancel` · `POST /requests/{id}/cancel-request` |
| **Permission gate** | `request:cancel` (`app.go:501`, `502`) **plus** requester-only and `approved`-only in the handler (`requests.go:802–814`) and requester-only again in the store (`store/requests.go:875–877`) |
| **Coverage IDs** | Q2, L6, L11, C2, G1 |
| **Priority** | critical |

**Preconditions**
1. The caller raised the request.
2. Status is exactly `approved` — the only state this route accepts.
3. The caller holds `request:cancel`.
4. A non-blank reason.

**Postconditions (success)**
1. `status='cancellation_requested'`, `cancel_reason=<reason>`, **`on_hold=0` and `hold_reason=''`** (`store/requests.go:889`).
2. **Payment is frozen from that instant**: the request is no longer `approved`, so Accounts cannot reserve it (`store/requests.go:858–860`).
3. One `audit_log` row action `cancel_request`, summary *“<name> asked for cancellation of <number>: <reason>. Payment frozen.”*, with before/after (`store/requests.go:897–900`).
4. `notify.EventCancellationRequested` fires (`requests.go:824`).
5. The detail screen shows a `.banner.warn` **“Payment is frozen”** naming the approver and quoting the reason; the pill uses the `cancelreq` modifier reading **“Cancellation requested”** (`templates.go:2312–2321`, `requests.go:967–971`, `1000–1001`).
6. The request appears in the approver’s **Cancellations** tab and in the *“Decisions only you can make”* dashboard area.
7. Redirect **303** to `/requests/{id}`.

**Postconditions (failure)** — status stays `approved`, `cancel_reason` unchanged, any hold intact, no audit row.

**Main success scenario**
1. Actor presses **Request cancellation**.
2. System refuses anybody who is not the requester (**403**) and any status but `approved` (**400**) **before rendering**, so the screen is never shown to somebody whose submit would bounce (`requests.go:796–814`).
3. System renders `request_cancel`: eyebrow *“<number> · Approved — awaiting payment”*, `h1` **“Ask for this to be cancelled”**, sub *“Approved requests cannot be withdrawn on your own. Your approver decides.”* (`templates.go:2771–2776`).
4. A `.banner.warn` **“Payment freezes the moment you ask”** warns *“Accounts cannot reserve or pay this request while a cancellation is pending. If an accountant has already reserved it, they are told immediately.”* (`templates.go:2779–2786`).
5. A summary `.req-head` shows number, amount, short title and *“approved by <manager> on <date>”*.
6. Actor fills **Reason** (placeholder *“Say what changed. <manager> sees exactly this.”*).
7. A **What happens next** list states the three outcomes: notified and shown as **Cancellation requested**; accepted ⇒ cancelled and closed, nothing payable; declined ⇒ back to **Approved — awaiting payment** (`templates.go:2803–2810`).
8. Actor presses **Send cancellation request** (`.btn.danger`).
9. System writes the status, the reason, clears any hold, audits, notifies, redirects.

**Alternate flows**
- **22.a** Actor presses **Never mind** ⇒ link back to `/requests/{id}`; nothing written.
- **22.b** The request was on hold when the ask lands ⇒ the hold is cleared, because a hold only ever describes an `approved` request (L7) and the audit before/after preserves the lost reason (`store/requests.go:881–888`).

**Exception flows**
- **22.e1** Blank or whitespace reason ⇒ **400** *“say why it should be cancelled”*; `<textarea required>` also blocks it client-side (`store/requests.go:862–865`, `templates.go:2801`).
- **22.e2** Status `pending` ⇒ **400** *“Only an approved request is cancelled this way. It is still awaiting approval; withdraw it instead.”* (`requests.go:810–813`, `612–613`).
- **22.e3** Status already `cancellation_requested` ⇒ **400** *“… A cancellation is already pending on it.”*
- **22.e4** Not the requester — including an **Admin holding every verb** ⇒ **403** *“Only the person who raised a request may ask for it to be cancelled.”* (`requests.go:802–805`).
- **22.e5** A race that moved the row out of `approved` between GET and POST ⇒ **400** *“a <status> request cannot be sent for cancellation”* (`store/requests.go:878–880`).

**Business rules**
- BR-22.1 **G1** — an approved request cannot be withdrawn on the requester’s own say-so; they ask, payment freezes at that moment, and the approver decides — `internal/app/requests.go:788–791`.
- BR-22.2 Asking is the requester’s act, so anybody else is refused **at the handler** rather than at the store, and the screen is never rendered to them — `internal/app/requests.go:793–796`.
- BR-22.3 `on_hold=1` implies `status='approved'`; every exit from `approved` clears the hold — `internal/store/requests.go:881–889`.
- BR-22.4 Legal edges from `approved` are exactly `cancellation_requested` and `cancelled` — `internal/store/requests.go:96`.

**Data touched** — writes `payment_requests.status`, `.cancel_reason`, `.on_hold`, `.hold_reason`, `.updated_at`; `audit_log`; `notifications`.

**Non-functional / UX notes** — `getByLabel('Reason')` is unambiguous on this screen (one field). The **What happens next** block is a `ul.hint`, not a label. At 390px the danger button is the sticky bar’s primary. No empty state.

**Open questions** — the banner promises a reserving accountant *“are told immediately”*; whether that notification exists is specified in `UC-C-*`, and the store’s only note is a `P5 hook` comment (`store/requests.go:896`).

---

## 4. Finding and exporting requests

### UC-B-23 — Browse the request list

| | |
|---|---|
| **Goal** | “Show me the requests I am allowed to see, newest and most urgent first, and let me narrow them.” |
| **Primary actor** | Requester (AC1) · Manager (AC2) · Accounts (AC3) · Admin (AC4) |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | **Requests** in the nav, **My requests** on the confirmation, or a dashboard metric tile. |
| **Route(s)** | `GET /requests?bucket=&scope=&type=&treatment=&project_id=&q=` |
| **Permission gate** | `request:view` (`app.go:441`); the **row set** is bounded by the caller’s `request` data scope |
| **Coverage IDs** | Q5, A5, R3, R6, D3, D4, T12 |
| **Priority** | critical |

**Preconditions**
1. Session with `request:view`.
2. A `request` scope of `own`, `assigned` or `all` — with none, `requestWhere` adds no owner clause but the middleware has already refused the caller (`requests.go:332–343`, `store/requests.go:1209–1216`).

**Postconditions (success)** — a list of at most **200** rows plus four tab counts. Nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**
1. Actor opens `/requests`.
2. System resolves the effective scope: the URL may **narrow** it and never widen it, ranked `own < assigned < all` (`requests.go:347–354`).
3. System defaults `bucket` to `open` (`requests.go:471–473`).
4. System lists the rows and then counts **every** tab from the same SQL with only the bucket changed, so a tab can never promise a number the list does not show (`requests.go:484–494`).
5. System renders `requests`: eyebrow **“Requests”**, `h1` **“My requests”** when the scope is `own`, otherwise **“All requests”**, sub *“<n> shown · sorted by who is holding them up”* (`templates.go:2117–2119`).
6. Header actions: **⤓ Export CSV** carrying the current scope, bucket and query; **＋ New request** for anyone holding `request:create` (`templates.go:2122–2123`).
7. The desktop `.toolbar` offers **Search** (placeholder *“Number, payee, invoice, purpose…”*), **Type** (*Any type* + the four labels) and **Treatment** (*Any* / *Budget expense* / *Recoverable*), closed by **Apply**; the phone gets `.m-filters` with an `aria-label="Search requests"` box and a **Filters** button opening `#filter-sheet` (`templates.go:2127–2147`).
8. The `.segmented` tabs are **links, each its own URL**: **Open**, **Needs me**, **Closed**, **All**, each with its count (`requests.go:463–465`, `templates.go:2149–2153`).
9. Each row is a `request_card` linking to `/requests/{id}`: number, amount, short title, an **Urgent** pill, a **Recoverable · <category>** pill, the type label, project / head, `· invoice <no>`, the status pill and the waiting sentence (`templates.go:2050–2065`).

**Alternate flows**
- **23.a** `bucket=open` ⇒ `pending`, `returned`, `approved`, `cancellation_requested` (`store/requests.go:1202`).
- **23.b** `bucket=closed` ⇒ `rejected`, `withdrawn`, `cancelled` (`store/requests.go:1203`).
- **23.c** `bucket=needs-me` ⇒ the one line the design turns on: `(requester_id=me AND status='returned') OR (manager_id=me AND status IN ('pending','cancellation_requested'))` (`store/requests.go:1226–1230`).
- **23.d** `bucket=all` ⇒ no status clause.
- **23.e** An unknown bucket is treated as a literal status (`store/requests.go:1231–1235`).
- **23.f** `?scope=own` used by a Manager or Admin ⇒ narrowed to their own requests; `?scope=all` used by a Requester ⇒ **ignored**, they stay on `own`.
- **23.g** `q=` searches number, purpose, short title, invoice number, **display payee** (`COALESCE(v.name, r.vendor_payee)`) and requester name, all lower-cased and LIKE-escaped (`store/requests.go:1256–1263`).
- **23.h** The mobile filter sheet is a plain GET form, so a phone produces the same shareable URL a desktop toolbar would (`templates.go:2067–2069`).

**Exception flows**
- **23.e1** No `request:view` ⇒ **403**.
- **23.e2** Nothing matches ⇒ `.empty` *“No requests match. Try another tab, or clear the filters.”* (`templates.go:2156`).
- **23.e3** A Requester never sees another person’s row, whatever the query string (Q5) — the scope clause is SQL, not markup.

**Business rules**
- BR-23.1 A URL may narrow a caller’s scope and never widen it — `internal/app/requests.go:345–354`.
- BR-23.2 Tab counts come from the same query as the rows — `internal/app/requests.go:482–494`.
- BR-23.3 The `.segmented` tabs are links so a view can be bookmarked and sent — `internal/app/templates.go:2106–2109`.
- BR-23.4 `waitingOn` is computed once, in one place, so the list, the queue and the detail head cannot drift — `internal/app/requests.go:388–390`.
- BR-23.5 An active hold answers **before** the status, so a held request reads *“Waiting on <requester> to clarify”* rather than *“Waiting on Accounts”* — `internal/app/requests.go:398–413`.
- BR-23.6 The default page size is 200 (`store/requests.go:1272–1274`); there is **no pagination control** on the screen.

**Data touched** — reads `payment_requests`, `projects`, `heads`, `vendors`, `users`.

**Non-functional / UX notes** — at 390px `.toolbar` is hidden and `.m-filters` shows; `#filter-sheet` is a modal with focus trapped and Escape closing (`fervid-app.js:116–147`). **SV27 applies**: `Type`, `Treatment` and `Search` labels exist twice in the DOM. Empty state as 23.e2.

**Open questions** — `ListRequests` orders `r.urgent DESC, r.created_at DESC, r.id DESC`, i.e. **newest first**, while both the code comment and the screen’s sub-line promise *“who has been kept waiting longest”* (**DV2**).

---

### UC-B-24 — Export the request list as CSV

| | |
|---|---|
| **Goal** | “Give me this list as a spreadsheet.” |
| **Primary actor** | Requester (AC1) · Manager (AC2) · Accounts (AC3) · Admin (AC4) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | **⤓ Export CSV** on `/requests` or on `/approvals`. |
| **Route(s)** | `GET /requests/export.csv?scope=&status=&bucket=&type=&treatment=&project_id=&q=` |
| **Permission gate** | `request:view` (`app.go:442`) — **not** a separate export verb |
| **Coverage IDs** | D3, Q5, R6, C2, C3 |
| **Priority** | medium |

**Preconditions**
1. Session with `request:view`.
2. The filters in the query string are the ones the screen was showing.

**Postconditions (success)**
1. A `text/csv; charset=utf-8` response with `Content-Disposition: attachment; filename="requests.csv"` (`requests.go:951–952`).
2. Header row exactly: `Number, Status, Type, Title, Amount, Payee, Requester, Approver, Created` (`requests.go:940`).
3. One row per request, with the **human** status (`requestStatusText`) and type (`typeLabel`), the amount as `money.FormatPaise` (so it carries `₹`), the **display payee** `req.Vendor`, and the creation date as `YYYY-MM-DD` (`requests.go:942–944`).
4. One `audit_log` row action `export`, entity `payment_request`, summary *“Exported request list”* (`requests.go:936–937`).
5. The row set is **scope-bounded exactly as the list is** — a Requester exports only their own.

**Postconditions (failure)** — no file; the audit row may already exist because it is written **before** the CSV is generated (`requests.go:936` precedes `939`).

**Main success scenario**
1. Actor presses **⤓ Export CSV**.
2. System resolves the effective scope with the same narrowing rule as UC-B-23.
3. System lists the rows (limit 200), records the audit row, writes the CSV and streams it.

**Alternate flows**
- **24.a** From `/approvals` the link hardcodes `scope=assigned` plus the current query, so a manager exports their own queue (`templates.go:2932`).
- **24.b** `status=` is honoured here but not on the list screen (`requests.go:930` reads it; `requests.go:474–476` does not).

**Exception flows**
- **24.e1** No `request:view` ⇒ **403**; the button is still rendered unconditionally on `/requests` (`templates.go:2122` has no `.Perms.Can` guard), but the caller could not have reached the screen.
- **24.e2** CSV writer error ⇒ **500** *“The export could not be generated.”*
- **24.e3** A partial write failure after the headers are sent is logged only (`requests.go:953–955`).

**Business rules**
- BR-24.1 Export is gated by the same verb that gates reading, and bounded by the same scope (D3/R6) — `internal/app/app.go:442`, `internal/app/requests.go:928–931`.
- BR-24.2 The export is audited, because taking data out of the system is an event (C2).
- BR-24.3 Money in the CSV carries `₹` — `money.FormatPaise` (C3).

**Data touched** — reads `payment_requests` (+ joins); writes `audit_log`.

**Non-functional / UX notes** — the export is a plain `<a>` download, so on a phone it hands off to the OS. Nothing announces success. The 200-row limit is silent — an export can be **incomplete with no warning** (**DS8**).

**Open questions** — DS8: whether the export should lift the 200-row cap.

---

## 5. Manager actions

### UC-B-25 — Work the approvals queue

| | |
|---|---|
| **Goal** | “What is mine to decide?” |
| **Primary actor** | Manager (AC2) · Admin (AC4) |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | **Approvals** in the nav, or the *“Awaiting my approval”* dashboard tile. |
| **Route(s)** | `GET /approvals?bucket={to-approve\|cancellations\|decided}&q=` |
| **Permission gate** | `approval:approve` (`app.go:508`) — the verb that lets a person **decide**, not the one that lets them read |
| **Coverage IDs** | A5, A6, D4, Q5, L2, L6, G1 |
| **Priority** | critical |

**Preconditions**
1. Session with `approval:approve`.
2. Requests exist with `manager_id` equal to the caller.

**Postconditions (success)** — the queue and its three counts. Nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**
1. Actor opens `/approvals`.
2. System forces the scope to **`assigned`**, always: holding `approval:approve` says you may decide, and `manager_id` says which rows are yours (`requests.go:884–886`, `896`).
3. System defaults the bucket to the first tab when it is unknown (`requests.go:889–892`).
4. System counts **all three** tabs with the same query and lists only the selected one (`requests.go:895–911`).
5. System renders `approvals`: eyebrow **“Manager queue”**, `h1` **“Waiting on you”**, sub *“<n> to approve · <m> cancellation(s) to decide”* (`templates.go:2927–2929`), plus **⤓ Export CSV** scoped to `assigned`.
6. Toolbar: **Search** with placeholder *“Number, payee, invoice, requester…”* and **Apply**; the phone gets an `aria-label="Search approvals"` box and a **Search** submit (`templates.go:2936–2947`).
7. Tabs, each its own URL with a count: **To approve** (`pending`), **Cancellations** (`cancellation_requested`), **Decided** (`approved`, `rejected`, `cancelled`) (`requests.go:877–881`).
8. Each row is the same `request_card` the list uses; the waiting line reads **“Waiting on you”** with class `you` for these rows (`requests.go:396–397`, `414–419`).
9. A closing hint states the rule: *“Every approval is a decision made after opening the request. There is no bulk approval, by design.”* (`templates.go:2959–2960`).

**Alternate flows**
- **25.a** `bucket=cancellations` ⇒ only `cancellation_requested`; each row leads to UC-B-30.
- **25.b** `bucket=decided` ⇒ the caller’s past decisions plus cancellations.
- **25.c** `q=` narrows every tab’s count as well as the rows (`requests.go:896–898`).

**Exception flows**
- **25.e1** No `approval:approve` ⇒ **403**; the nav entry is absent too.
- **25.e2** Nothing assigned ⇒ `.empty` *“Nothing is waiting on you here.”* (`templates.go:2956`).
- **25.e3** There is **no checkbox column and no “approve selected” button** anywhere on the screen (A6) — `templates.go:2920–2922`.
- **25.e4** An Admin holding every verb sees only rows where **they** are the `manager_id`, because the scope is hardcoded — not every pending request in the system.

**Business rules**
- BR-25.1 The queue is its own screen rather than a scope of `/requests`, because it answers a different question and its tabs are statuses, not buckets — `internal/app/requests.go:869–871`.
- BR-25.2 Scope is always `assigned`; there is no override — `internal/app/requests.go:896`.
- BR-25.3 **A6 — no bulk approval by design** — `internal/app/requests.go:884–886`.
- BR-25.4 Managers see **all** requests on `/requests` (scope `all`) but may decide only their own assignments (A5) — `internal/store/migrations.go:377`.

**Data touched** — reads `payment_requests` (+ joins).

**Non-functional / UX notes** — at 390px cards stack and the `.segmented` tabs scroll horizontally inside their own container. The three tab counts are the D4 assertion point. **Returned**, **withdrawn**, **processing**, **partial_review** and **completed*** rows appear in **no** approvals tab (**DS9**).

**Open questions** — DS9: a request the manager returned disappears from their queue entirely, with no “awaiting the requester” tab.

---

### UC-B-26 — Approve a request, optionally for less

| | |
|---|---|
| **Goal** | “I agree to this — possibly at a smaller amount — so Accounts can pay it.” |
| **Primary actor** | Manager (AC2) |
| **Supporting actors** | Requester (AC1); Accounts (AC3); `notify.Service` (AC5) |
| **Scope / level** | system · user-goal |
| **Trigger** | **Approve ₹X** on the detail screen of a `pending` request assigned to the caller. |
| **Route(s)** | `POST /requests/{id}/approve` |
| **Permission gate** | `approval:approve` (`app.go:494`) **plus** `manager_id == actor` and `requester_id != actor` in the store (`store/requests.go:687–694`) |
| **Coverage IDs** | A2, A5, L6, L11, C2, C3, N3 |
| **Priority** | critical |

**Preconditions**
1. Status is `pending` — the only status with a legal edge to `approved` (`store/requests.go:93`).
2. `manager_id` equals the caller.
3. The caller did **not** raise it (G8).
4. The approved amount is positive.

**Postconditions (success)**
1. `status='approved'`, `approved_amount=<amount>`, `approved_by=<actor>`, `approved_at=CURRENT_TIMESTAMP`, `decision_reason=<trimmed note>` (`store/requests.go:698`).
2. One `audit_log` row action `approve`, summary *“<name> approved request <number> for ₹X”*, with before/after; the thread renders it with tone `ok` and glyph `✓` (`store/requests.go:705–709`, `requests.go:1071`, `1088`).
3. `notify.EventRequestApproved` fires, plus `EventRequestUrgent` when urgent (`requests.go:726`, `notifications.go:46`).
4. The requester’s screen gains the `.banner.locked` **“Approved requests are locked”** and loses **Edit request**; **Request cancellation** appears instead (`templates.go:2288–2297`, `2413`, `2419`).
5. Accounts can reserve it (specified in `UC-C-*`); the *“Approved and unclaimed”* dashboard area counts it.
6. Redirect **303** to `/approvals` — the row has left **To approve** and joined **Decided**.

**Postconditions (failure)** — status, amounts and timestamps unchanged; no audit row; no notification.

**Main success scenario**
1. Actor opens the request from the queue; the waiting line reads **“Waiting on you”**.
2. Actor presses **Approve ₹18,400.00** — the button text carries the amount (`templates.go:2428`), so tests must match `/^Approve /`.
3. `#approve-sheet` opens as a modal, headed **“Approve ₹X?”** with sub *“<number> · <display payee>”* (`templates.go:2209`).
4. **Amount approved** is pre-filled with `amountValue(.Amount)` — e.g. `21,500.00`, no `₹` — with hint *“You may approve a smaller amount than was asked for.”* (`templates.go:2212–2215`).
5. Actor overwrites it with a smaller figure.
6. Actor optionally fills **Note optional** (placeholder *“Recorded in the history and visible to everyone.”*).
7. The sheet warns *“Accounts will be able to reserve this immediately. <requester> can no longer edit it.”* (`templates.go:2218`).
8. Actor presses **Approve request**.
9. System parses the amount, calls `ApproveRequest`, which re-checks the manager, re-checks G8, checks the transition, writes the row and the audit line, commits, notifies and redirects.

**Alternate flows**
- **26.a** Amount left as pre-filled ⇒ approved in full; `approvedOf` then equals the requested amount for the settlement comparison (`UC-C-*`).
- **26.b** Note left blank ⇒ `decision_reason` is `''`; the detail screen shows no decision banner for an approval.
- **26.c** The approved amount may be **larger** than requested — nothing caps it (`store/requests.go:675–677` only requires `> 0`).

**Exception flows**
- **26.e1** Blank or unparseable amount ⇒ **400** *“Enter the amount you are approving.”* (`requests.go:717–720`). The input is `required`, so the browser blocks the empty case first.
- **26.e2** Zero or negative ⇒ **400** *“approved amount must be positive”* (`store/requests.go:675–677`).
- **26.e3** Not the row’s manager ⇒ **403** (UC-B-34).
- **26.e4** The caller raised it ⇒ **403** (UC-B-33).
- **26.e5** Status not `pending` ⇒ **400** *“a <status> request cannot be approved”*.
- **26.e6** No `approval:approve` ⇒ **403** from the middleware; the button is not rendered either.
- **26.e7** No CSRF ⇒ **403**.

**Business rules**
- BR-26.1 **A2** — the approver may approve less than was asked for; the amount is editable in the sheet — `internal/app/requests.go:713–715`.
- BR-26.2 **G8 last line of defence** — the form never offers it, the validator rejects it, and `ApproveRequest` refuses it even if a row reached that state — `internal/store/requests.go:690–694`.
- BR-26.3 The decision belongs to the row’s `manager_id`, not to anyone holding the verb — `internal/store/requests.go:687–689`.
- BR-26.4 The sheet uses `amountValue`, not `money`, because `.money-field` draws its own `₹` — `internal/app/templates.go:2202–2204`.
- BR-26.5 Approval is a decision taken **after opening the request** (A6).

**Data touched** — writes `payment_requests.status`, `.approved_amount`, `.approved_by`, `.approved_at`, `.decision_reason`, `.updated_at`; `audit_log`; `notifications`.

**Non-functional / UX notes** — the sheet is a real modal: first focusable control receives focus on open, Tab cycles inside it, Escape closes, and focus returns to the opener (`fervid-app.js:116–147`). **SV18 applies** — the Note label’s accessible name is `"Note optional"`. At 390px the sheet fills the viewport and its footer buttons clear the tab bar.

**Open questions** — nothing prevents approving **more** than requested; whether that is intended is unresolved.

---

### UC-B-27 — Return a request for correction

| | |
|---|---|
| **Goal** | “This is fixable — send it back with exactly what needs changing.” |
| **Primary actor** | Manager (AC2) |
| **Supporting actors** | Requester (AC1) |
| **Scope / level** | system · user-goal |
| **Trigger** | **Return for correction** on the detail screen of a `pending` request assigned to the caller. |
| **Route(s)** | `POST /requests/{id}/return` |
| **Permission gate** | `approval:return` (`app.go:495`) **plus** `manager_id == actor` (`store/requests.go:728–730`) |
| **Coverage IDs** | A4, Q1, L3, L11, C2 |
| **Priority** | critical |

**Preconditions**
1. Status is `pending`.
2. `manager_id` equals the caller.
3. A non-blank comment.

**Postconditions (success)**
1. `status='returned'`, `decision_reason=<trimmed comment>` (`store/requests.go:734`).
2. One `audit_log` row action `return`, summary *“<name> returned request <number>: <comment>”*, tone `warn`, glyph `↩` (`store/requests.go:743–746`, `requests.go:1074`, `1089`).
3. `notify.EventRequestReturned` fires (`requests.go:735`).
4. **The number and the history are retained.**
5. The requester’s `/requests/{id}` now renders the correction screen (UC-B-19); everyone else sees the `.banner.warn` **“<manager> sent this back”**.
6. The row appears in the requester’s **Needs me** bucket and leaves the approver’s **To approve** tab.
7. Redirect **303** to `/approvals`.

**Postconditions (failure)** — status stays `pending`, `decision_reason` unchanged, no audit row, no notification.

**Main success scenario**
1. Actor presses **Return for correction**.
2. `#return-sheet` opens, headed **“Return for correction”** with sub *“<requester> can edit and resubmit. The number and history stay.”* (`templates.go:2227`).
3. Actor fills **What needs correcting** (placeholder *“Be specific — this is the whole message they get.”*).
4. Actor presses **Return request**.
5. `decideRequest` trims the comment, refuses an empty one, re-checks the manager, checks `pending → returned`, writes status and reason, audits, commits.

**Alternate flows**
- **27.a** A returned request can be returned again after a resubmit; `returned → pending → returned` is a legal cycle (`store/requests.go:92–93`).

**Exception flows**
- **27.e1** Blank or whitespace comment ⇒ **400** *“a comment is required to return a request”* (`store/requests.go:715–718`, `752`). The `<textarea required>` blocks the empty case client-side (`templates.go:2229`) — a **whitespace-only** comment is the case that reaches the store. See UC-B-36.
- **27.e2** Not the row’s manager ⇒ **403**.
- **27.e3** Status not `pending` ⇒ **400** *“a <status> request cannot be returned”*.
- **27.e4** No `approval:return` ⇒ **403**; the button is not rendered.

**Business rules**
- BR-27.1 **A4** — returning demands words; the comment is the whole message the requester gets — `internal/store/requests.go:751–753`.
- BR-27.2 Returning and rejecting are one code path with two words and two target statuses — `internal/store/requests.go:714–757`.
- BR-27.3 Returning is the approver’s alternative to editing somebody else’s request (BR-18.2).
- BR-27.4 The reason lands in `decision_reason`, the same column an approval note and a rejection reason use, so only one of them is ever meaningful at a time.

**Data touched** — writes `payment_requests.status`, `.decision_reason`, `.updated_at`; `audit_log`; `notifications`.

**Non-functional / UX notes** — modal focus behaviour as UC-B-26. `getByLabel('What needs correcting')` is unique on the page. At 390px the sheet’s footer holds **Cancel** and **Return request**.

**Open questions** — none.

---

### UC-B-28 — Reject a request permanently

| | |
|---|---|
| **Goal** | “This should not happen at all — close it with a reason.” |
| **Primary actor** | Manager (AC2) |
| **Supporting actors** | Requester (AC1) |
| **Scope / level** | system · user-goal |
| **Trigger** | **Reject** on the detail screen of a `pending` request assigned to the caller. |
| **Route(s)** | `POST /requests/{id}/reject` |
| **Permission gate** | `approval:reject` (`app.go:496`) **plus** `manager_id == actor` |
| **Coverage IDs** | A3, L4, L11, Q3, C2 |
| **Priority** | critical |

**Preconditions**
1. Status is `pending`.
2. `manager_id` equals the caller.
3. A non-blank reason.

**Postconditions (success)**
1. `status='rejected'`, `decision_reason=<trimmed reason>`.
2. One `audit_log` row action `reject`, summary *“<name> rejected request <number>: <reason>”*, tone `bad`, glyph `✕`.
3. `notify.EventRequestRejected` fires (`requests.go:744`).
4. **`rejected` is terminal and read-only** — no outgoing edge in `legalTransitions` (`store/requests.go:91–101`). The pill reads **“Rejected — final”** and the waiting line **“Closed. Raise a new request if needed”** with class `closed` (`requests.go:996–997`, `427–428`).
5. The only forward path is **Raise it again** (UC-B-21).
6. The row leaves **Open** for **Closed** and is excluded from duplicate checks.
7. Redirect **303** to `/approvals`.

**Postconditions (failure)** — nothing changed.

**Main success scenario**
1. Actor presses **Reject** (`.btn.danger.outline`).
2. `#reject-sheet` opens, headed **“Reject this request?”** with sub *“Rejection is final and read-only. <requester> would have to raise a new request.”* (`templates.go:2238`).
3. A `.banner.bad` warns **“This cannot be undone”** — *“If the request is fixable, return it for correction instead.”* (`templates.go:2240`).
4. Actor fills **Reason for rejection**.
5. Actor presses **Reject permanently** (`.btn.danger`).
6. `decideRequest` runs exactly as UC-B-27 with target `rejected`.

**Alternate flows**
- **28.a** The requester later re-raises; the new request carries the same approver and its own number (UC-B-21).

**Exception flows**
- **28.e1** Blank or whitespace reason ⇒ **400** *“a reason is required to reject a request”* (`store/requests.go:756`). See UC-B-36.
- **28.e2** Not the row’s manager ⇒ **403**.
- **28.e3** Status not `pending` ⇒ **400** *“a <status> request cannot be rejected”*.
- **28.e4** No `approval:reject` ⇒ **403**; the button is not rendered.
- **28.e5** Editing a rejected request afterwards ⇒ **400** (UC-B-38).

**Business rules**
- BR-28.1 **A3** — rejection demands a reason — `internal/store/requests.go:755–757`.
- BR-28.2 **L4** — rejected is final and read-only; `reqStatusExplain` says so in every refusal — `internal/app/requests.go:610–611`.
- BR-28.3 The screen offers **Return for correction** beside **Reject** so the cheaper answer is one click away (BR-27.3).

**Data touched** — as UC-B-27.

**Non-functional / UX notes** — the destructive action is `.btn.danger` and is the sheet’s primary; the warning banner precedes the field. Modal focus behaviour as UC-B-26.

**Open questions** — none.

---

### UC-B-29 — Cancel an approved request outright

| | |
|---|---|
| **Goal** | “I approved this but circumstances changed — stop it being paid, and say why.” |
| **Primary actor** | Manager (AC2) |
| **Supporting actors** | Requester (AC1); Accounts (AC3) |
| **Scope / level** | system · user-goal |
| **Trigger** | **Cancel with reason** on the detail screen, or on `/requests/{id}/cancellation`, of an `approved` request assigned to the caller. |
| **Route(s)** | `GET /requests/{id}/cancellation` (the screen) · `POST /requests/{id}/cancel` |
| **Permission gate** | `approval:cancel` (`app.go:503`, `504`) **plus** `manager_id == actor` in the handler (`requests.go:837–841`) and in the store (`store/requests.go:976–978`) |
| **Coverage IDs** | L6, L11, C2, G2 |
| **Priority** | high |

**Preconditions**
1. Status is `approved`.
2. `manager_id` equals the caller.
3. A non-blank reason.
4. The caller holds `approval:cancel`.

**Postconditions (success)**
1. `status='cancelled'`, `cancel_reason=<reason>`, **`on_hold=0`, `hold_reason=''`** (`store/requests.go:985`).
2. One `audit_log` row action `cancel`, summary *“<name> cancelled request <number>: <reason>”*, tone `warn`, glyph `⏸` (`store/requests.go:992–995`, `requests.go:1074`, `1101`).
3. `cancelled` is terminal; the pill reads **“Cancelled”** and the waiting line **“Cancelled. Nothing can be paid against it”** with class `closed` (`requests.go:1002–1003`, `431–432`).
4. Redirect **303** to `/requests/{id}`.

**Postconditions (failure)** — nothing changed.

**Main success scenario**
1. Actor opens the approved request; the bar offers **Cancel with reason** linking to `/requests/{id}/cancellation` (`templates.go:2430–2432`).
2. System refuses anybody who is not the row’s manager **before rendering**: **403** *“Only the approver this request was sent to can decide its cancellation.”* (`requests.go:837–840`).
3. System renders `request_cancellation` with the request head, a **What you approved** card (approved amount or requested amount, approved-on date, Vendor/Paid to, Advance reason, Purpose) and the thread (`templates.go:2848–2859`).
4. The `.ab-note` reads *“Cancelling closes the request permanently.”* and the only action is **Cancel with reason** (`templates.go:2862`, `2868`).
5. `#outright-sheet` opens, headed **“Cancel <number>?”** with sub *“<requester> did not ask for this.”* (`templates.go:2903`).
6. Actor fills **Reason** (placeholder *“<requester> and Accounts both see this.”*).
7. Actor presses **Cancel request** (`.btn.danger`).
8. `CancelRequest` trims, refuses empty, re-checks the manager, checks `approved → cancelled`, clears the hold, audits, commits.

**Alternate flows**
- **29.a** Status is `cancellation_requested` ⇒ the same screen shows the accept/decline pair instead (UC-B-30).
- **29.b** Any other status ⇒ the screen shows only **Back to the request** (`templates.go:2869–2871`).
- **29.c** The request was on hold ⇒ the hold is cleared, because the hold tab would otherwise keep listing a request no reply can change (`store/requests.go:982–985`).

**Exception flows**
- **29.e1** Blank or whitespace reason ⇒ **400** *“a reason is required to cancel a request”* (`store/requests.go:963–966`).
- **29.e2** Not the row’s manager ⇒ **403**, at the screen and again at the store.
- **29.e3** Status not `approved` ⇒ **400** *“a <status> request cannot be cancelled”*.
- **29.e4** No `approval:cancel` ⇒ **403** from the middleware.

**Business rules**
- BR-29.1 **G2** — an approver may cancel an approved request outright, without having been asked; a reason is always required — `internal/store/requests.go:960–966`.
- BR-29.2 One screen serves both questions, because both ask *“should this still be paid”* — `internal/app/requests.go:828–831`.
- BR-29.3 A cancelled request is dead, so nothing may still be on hold on it — `internal/store/requests.go:982–984`.
- BR-29.4 The roles matrix bundles `request:withdraw`, `request:cancel` and `approval:cancel` into one **Cancel** cell: *“may take a request out of the pipeline”* — `internal/app/permmap.go:72–78`.

**Data touched** — writes `payment_requests.status`, `.cancel_reason`, `.on_hold`, `.hold_reason`, `.updated_at`; `audit_log`.

**Non-functional / UX notes** — `getByLabel('Reason')` inside `#outright-sheet` is unique; the same string appears on `request_cancel` (a different screen). Modal focus behaviour as UC-B-26. No notification event fires for an outright cancel — **still true after the repair waves**: `requestCancelOutright` has no `a.fire` (`internal/app/requests.go:1036-1043`; it was `requests.go:860–867` at the time of this audit). **DS10**.

**Open questions** — DS10, still open for this path. Migration v9 gave withdrawal and both cancellation *decisions* their events (F-F-06, Waves 2/4), but an approver cancelling an approved request outright still fires nothing, so the requester learns of it only by looking (`requests.go:1036-1043`).

---

### UC-B-30 — Decide a requester’s cancellation request

| | |
|---|---|
| **Goal** | “They asked me to cancel it — decide, and unfreeze it either way.” |
| **Primary actor** | Manager (AC2) |
| **Supporting actors** | Requester (AC1); Accounts (AC3) |
| **Scope / level** | system · user-goal |
| **Trigger** | **Decide the cancellation** on the detail screen, the **Cancellations** tab, or the *“Decisions only you can make”* dashboard area. |
| **Route(s)** | `GET /requests/{id}/cancellation` · `POST /requests/{id}/cancellation` |
| **Permission gate** | `approval:cancel` (`app.go:504`, `505`) **plus** `manager_id == actor` in the handler and the store (`requests.go:837`, `store/requests.go:931–933`) |
| **Coverage IDs** | L6, L11, C2, G1, D4 |
| **Priority** | critical |

**Preconditions**
1. Status is `cancellation_requested`.
2. `manager_id` equals the caller.
3. The caller holds `approval:cancel`.
4. **Declining** additionally requires a non-blank note; **accepting** does not.

**Postconditions (success — accept)**
1. `status='cancelled'`, `decision_reason=<note or ''>`, `on_hold=0`, `hold_reason=''` (`store/requests.go:941`).
2. One `audit_log` row action **`cancel`** — the same action as an outright cancel, differing only in summary: *“<name> cancelled <number> at the requester’s asking”* (`store/requests.go:918`, `948`).
3. The request closes permanently; nothing can be paid against it.

**Postconditions (success — decline)**
1. `status='approved'` again, `decision_reason=<note>`, `on_hold=0`, `hold_reason=''`.
2. One `audit_log` row action **`cancel_decline`**, summary *“<name> declined the cancellation of <number>: <note>. Payment unfrozen.”*, tone `warn`, glyph `⏸` (`store/requests.go:920`, `950`).
3. **Payment is unfrozen** — Accounts can reserve it again.

**Postconditions (either, on failure)** — status stays `cancellation_requested`; no audit row.

**Both branches** redirect **303** to `/approvals?bucket=cancellations` (`requests.go:857`).

**Main success scenario (decline)**
1. Actor opens `/requests/{id}/cancellation`.
2. A `.banner.warn` **“Payment is frozen”** states *“No accountant can reserve or pay this request until you decide.”* (`templates.go:2834–2840`).
3. A card headed **“Why <requester> wants it cancelled”** quotes `cancel_reason` (`templates.go:2842–2845`).
4. A card headed **“What you approved”** shows the approved amount, the approved-on date, the payee, the advance reason and the purpose.
5. The thread follows; the `.ab-note` reads *“Declining sends it back to Accounts to pay as approved.”* (`templates.go:2862`).
6. Actor presses **Decline — keep it live**.
7. `#decline-sheet` opens, headed **“Decline the cancellation”** with sub *“The request returns to Approved — awaiting payment.”* (`templates.go:2892`).
8. Actor fills **Why it should still be paid** (placeholder *“<requester> and Accounts both see this.”*).
9. Actor presses **Decline and unfreeze**.
10. `DecideCancellation` runs with `accept=false`: it demands the note, re-checks the manager, confirms the status is `cancellation_requested` and the transition legal, writes `approved`, clears the hold, audits, commits.

**Alternate flows**
- **30.a Accept.** Actor presses **Cancel the request** (`.btn.danger`); `#accept-sheet` opens headed **“Cancel <number>?”** with sub *“₹X to <payee>”*, a `.banner.bad` **“The request closes permanently”** and an **optional** *“Note optional”* field; **Cancel request** posts `decision=accept` (`templates.go:2875–2886`).
- **30.b** The hidden `decision` input is the discriminator: `accept` versus anything else (`requests.go:852`, `templates.go:2878`, `2891`).

**Exception flows**
- **30.e1** Decline with a blank note ⇒ **400** *“say why it should still be paid”* (`store/requests.go:910–913`).
- **30.e2** Accept with a blank note ⇒ allowed; the note is optional on that branch only.
- **30.e3** Status is not `cancellation_requested` ⇒ **400** *“there is no cancellation to decide on a <status> request”* (`store/requests.go:934–936`).
- **30.e4** Not the row’s manager ⇒ **403** at the screen (*“Only the approver this request was sent to can decide its cancellation.”*) and again at the store.
- **30.e5** No `approval:cancel` ⇒ **403** from the middleware.
- **30.e6** No CSRF ⇒ **403**.

**Business rules**
- BR-30.1 Accepting a cancellation **is** cancelling the request, so it lands on the thread under the same `cancel` action; a separate `cancel_accept` would split one event across two names — `internal/store/requests.go:914–918`.
- BR-30.2 Declining requires words because the requester **and** Accounts both read them — `internal/store/requests.go:906–913`.
- BR-30.3 ~~Neither branch may leave a hold set~~ — `internal/store/requests.go:937–941`. **Corrected (F-C-07, Wave 2 `25411b8`, REPAIR-LOG decision 3).** The branches now differ, deliberately. **Accept** clears both columns: nothing may still be "on hold" on a dead row, or the hold tab keeps listing it and offering *"Read reply"* on a request no reply can change. **Decline** returns the request to `approved` — the one status a hold may describe — and *restores* the pause, `on_hold=CASE WHEN COALESCE(hold_reason,'') <> '' THEN 1 ELSE 0 END` (`internal/store/requests.go:1320-1334`, the UPDATE at `:1350`). At `30edd6a` both branches cleared the hold, which cost requirement **L7** *"On hold (only Accounts lifts)"*: a requester asking for cancellation and an approver declining it lifted an accountant's hold between them, with the accountant's question still unanswered. `RequestCancellation` is the other half — it suspends `on_hold` but keeps `hold_reason` precisely so this restore has something to read (`requests.go:1268-1286`).
- BR-30.4 `cancellation_requested` has exactly two legal edges: `cancelled` and back to `approved` — `internal/store/requests.go:97`.
- BR-30.5 **Corrected (F-F-06, Wave 2 vocabulary `25411b8` + Wave 4 wiring).** At `30edd6a` neither branch fired a notification event (`requests.go:850–858` had no `a.fire`) — **DS10**. Both branches fire now, and they are deliberately **two** events rather than one, because "nothing will be paid" and "payment is unfrozen" are opposite sentences and each is an admin-editable template in its own right: `requestCancellationDecide` fires `notify.EventCancellationAccepted` on accept and `notify.EventCancellationDeclined` on decline (`internal/app/requests.go:1029`, `:1031`; the constants at `internal/notify/events.go:65-66`). `Data touched` below therefore also includes `notifications`.

**Data touched** — writes `payment_requests.status`, `.decision_reason`, `.on_hold`, `.hold_reason`, `.updated_at`; `audit_log`; `notifications` (one row per resolved recipient, since Wave 4).

**Non-functional / UX notes** — two sheets on one page; both are `hidden` in the DOM, so scope every selector to `#accept-sheet` or `#decline-sheet`. **SV18 applies** to `#cx-note`. At 390px the two cards stack above the thread and the sticky bar holds both actions.

**Open questions** — none for this use case. DS10 covered four silent paths at `30edd6a`; the two decided here are fixed (BR-30.5). The one still open is the **outright** cancel, UC-B-29 — see its open question.

---

### UC-B-31 — Reassign a pending request to a different approver

**Fixed in the 2026-07-27 audit repair, Wave 3, commit `1fac147`** (`docs/qa/results/REPAIR-LOG.md`: "F-A-06 · F-C-02 medium | Fixed — coverage requirement A7 now has a door."). Everything below described the state at `30edd6a`, where this use case had no route at all; the corrected header, scenario and flows follow, with the `30edd6a` gap kept underneath each so the history is not lost.

| | |
|---|---|
| **Goal** | “This is not my call — hand it to the right approver, with a reason on the record.” |
| **Primary actor** | Admin (AC4) · Manager (AC2) — both hold `approval:reassign` |
| **Supporting actors** | the outgoing and incoming approvers |
| **Scope / level** | system · user-goal |
| **Trigger** | The actor presses a reassignment control naming a new approver and a reason, on a `pending` request they may see. |
| **Route(s)** | `POST /requests/{id}/reassign-approver` (`internal/app/app.go:569`), handler `requestReassignApprover` (`app.go:1615-1647`). *(At `30edd6a`: unrouted — `store.ReassignRequest` had no HTTP caller, and `POST /requests/{id}/reassign` was — and still is — the unrelated **reservation** handoff route, `reservation:reassign` → `requestReassign`, `app.go:466`/`642`, `linking.go:642-674`.)* |
| **Permission gate** | `approval:reassign`, granted to Manager (`internal/store/migrations.go:538`, inside the seed at `:533`) and to Admin, which takes every pair through `adminGrants()` (`migrations.go:564-567`, `migrations.go:571`), now enforced at the route via `a.auth.RequirePermission("approval", "reassign", …)` (`app.go:569`). *(At `30edd6a`: the grant existed and was wired into the roles matrix but consumed by nothing; the seed then sat at `migrations.go:371`/`404`, which is where this row used to cite it.)* |
| **Coverage IDs** | **A7 — verified end-to-end** by `TestApproverReassignmentRoute` (`internal/app/app_integration_test.go:2191`) |
| **Priority** | high |

**Preconditions**
1. The request is within the actor's `request` data scope — enforced by `loadViewableRequest`-equivalent logic inline in the handler; outside scope answers **404**, not 403 (`app.go:1622-1627`, F-G-002).
2. `manager_id` names a user; absent ⇒ 400 "Choose the approver this request should go to." (`app.go:1628-1631`).
3. `reason` is non-blank ⇒ 400 "Give a reason for the reassignment." (`app.go:1633-1635`).
4. Underneath, `store.ReassignRequest` still enforces: a non-blank reason (`store/requests.go:1132-1135`), `newManagerID > 0` (`:1136-1138`), request status exactly `pending` (`:1153-1155`), `newManagerID != requester_id` (G8, `:1157-1159`), and — added by the same repair — that the target actually holds `approval:approve`, refused 400 *“that person cannot approve requests — choose one of the approvers offered”* otherwise (`requireApprover`, `store/requests.go:414-426`, called at `:1141-1143`). That last guard is F-A-08's: reassignment is the recovery path for a request stranded on somebody who cannot decide it, so it must not be a way back into the same hole. **The method moved to `store/requests.go:1131` after the audit; this use case originally cited it at `:759-801`.**

**Postconditions (success)**
1. `manager_id=<new>`, `decision_reason=<reason>`, `reminder_last_sent=NULL`, in one `UPDATE … WHERE id=? AND status='pending'` (`store/requests.go:1160-1161`).
2. One `audit_log` row, action `approval_reassign` — deliberately **not** `reassign`, because reservation reassignment writes that action on the same entity type — summary "\<name\> reassigned request \<number\> to \<new approver\>: \<reason\>" (`store/requests.go:1172-1179`).
3. The row moves into the new approver's queue and out of the old one.
4. 303 redirect to `/requests/{id}` (`app.go:1646`).

**Postconditions (failure)** — unchanged; a refused reassignment leaves `manager_id` and every other field untouched.

**Main success scenario**
1. Admin or Manager opens a pending request within their scope and supplies a new approver and a reason.
2. `POST /requests/{id}/reassign-approver` with `manager_id` and `reason`.
3. The route checks scope, then delegates to `store.ReassignRequest`, which validates and writes the postconditions above.
4. The caller is redirected to `/requests/{id}`, now showing the new approver and the `approval_reassign` trail entry.

*(At `30edd6a` this scenario was **not implementable against the shipped product** — the only way a request changed approver was the requester editing it and choosing a different Approver (UC-B-18.a), which reroutes and re-notifies but records the change as an `update` diff on `manager_id` (`threadField`, `requests.go:1136`) rather than as `approval_reassign`. That path still exists alongside the new one and is still recorded as a plain `update`.)*

**Alternate flows** — none.

**Exception flows**
- **31.e1** A Requester, or anyone without `approval:reassign`, gets 403 (`TestApproverReassignmentRoute`, `app_integration_test.go:2191-2211`).
- **31.e2** `POST /requests/{id}/reassign` (no `-approver` suffix) still reaches the **reservation** handler, unrelated to this use case: it requires the target to be able to work the Accounts queue and refuses otherwise, **400** "That person cannot work the Accounts queue." (`linking.go:664-668`). This is a different feature (handing over a payment reservation), not a stale route — do not conflate the two when testing A7.
- **31.e3** `POST /requests/{id}/approval-reassign` or any other guessed path ⇒ **405** (CV1); the real path is `/reassign-approver`.

**Business rules**
- BR-31.1 The store method exists, is tested at unit level and enforces every rule the spec asks for — `internal/store/requests.go:1131-1183` (`:759-801` at the time of this audit).
- BR-31.2 The two reassignments are deliberately different audit actions so an approver swap does not read as somebody taking over the payment — `internal/store/requests.go:1173-1177`.
- BR-31.3 *(historical, DV7)* The Phase-2 spec route table mapped `POST /requests/{id}/reassign` → `approval/reassign` → `requestReassign` for A7 (`docs/superpowers/specs/2026-07-25-phase-2-request-workflow-spec.md:206`); the shipped `30edd6a` code mapped that exact path to `reservation:reassign` instead. The repair did not change that mapping — it left the ambiguous shared path alone and gave A7 its own path, `/reassign-approver`, rather than resolve the collision on the shared one.

**Data touched** — `payment_requests.manager_id`, `.decision_reason`, `.reminder_last_sent`; `audit_log`.

**Non-functional / UX notes** — `/requests/{id}` renders a **Reassign approval** button, gated on `(eq .Request2.Status "pending") (.Perms.Can "approval" "reassign") .Approvers` (`internal/app/templates.go:2578-2579`), opening a `reassign-approver-sheet` overlay that posts to the route above (`templates.go:2584-2603`). The template comment explains why the gate is the grant and not "is the request's own approver": this route is also the recovery path for a request stuck with a deactivated or now-unqualified approver (F-G-025, F-A-08), so restricting it to the current approver would close that door.

**Open questions** — **DV7 / DS11 are resolved**: A7 now has its own route and its own screen control, rather than being folded into the requester's approver-change path.

---

### UC-B-32 — See my queues and their counts on the dashboard

| | |
|---|---|
| **Goal** | “Tell me at a glance what is waiting on me and hand me the rows.” |
| **Primary actor** | Requester (AC1) · Manager (AC2) · Accounts (AC3) · Admin (AC4) |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Signing in, or **Home** in the nav. |
| **Route(s)** | `GET /{$}` and `GET /dashboard` (`app.go:375`, `381`) |
| **Permission gate** | session only (`RequireLogin`); **every area and every tile is gated on the permission its own queue is** |
| **Coverage IDs** | D1, D2, D4, N1, Q5, A5 |
| **Priority** | critical |

**Preconditions**
1. A session exists.

**Postconditions (success)** — the metric strip, zero or more work areas, nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**
1. Actor lands on `/`.
2. System builds each queue with `area(...)`, which **counts the whole queue** and then lists only its head — at most **4** rows (`dashboard.go:39–68`).
3. `request:create` ⇒ **“Needs your action”** (icon `!`, scope `own`, bucket `needs-me`, foot *“Open my requests →”* to `/requests?bucket=needs-me`) and **“In progress”** (icon `▤`, scope `own`, statuses `pending`+`approved`+`cancellation_requested`, foot *“See all my requests →”*) (`dashboard.go:70–81`).
4. `approval:approve` ⇒ **“Awaiting your approval”** (icon `✓`, scope `assigned`, status `pending`, foot *“Open the approvals queue →”*) and **“Decisions only you can make”** (icon `⏸`, scope `assigned`, status `cancellation_requested`, foot *“Review all →”* to `/approvals?bucket=cancellations`) (`dashboard.go:82–93`).
5. `payment:process` ⇒ **“Approved and unclaimed”** (icon `₹`, scope **all**, status `approved`, foot *“Open all requests →”*) (`dashboard.go:94–100`).
6. Any of `config:view`, `role:view`, `user:view`, `vendor:view` ⇒ an **“Administration”** area of links rather than requests (`dashboard.go:101–103`, `111–127`).
7. System renders `dashboard`: eyebrow **“Home”**, `h1` **“Good day, <name>”**, sub *“Everything below is waiting on someone. The ones marked you are yours.”* (`templates.go:3183–3185`).
8. The `.metric-strip` tiles, each a link to the queue it counts: **Needs my action**, **My open requests** (both behind `request:create`), **Awaiting my approval**, **Cancellation requests** (both behind `approval:approve`), **Approved, unclaimed** (behind `payment:process`), **Budget → Variance grid** (behind `grid:view`) (`templates.go:3192–3225`).
9. Each `.area` renders its icon, title, count badge, up to four rows (number · short title, urgent pill, status pill, waiting sentence, amount) and its foot link (`templates.go:3229–3248`).

**Alternate flows**
- **32.a** A queue whose count is **zero is not rendered as an area at all**; the tile still shows the zero (`dashboard.go:57–59`).
- **32.b** `needs-action` and `approvals` tiles gain the `warn` modifier when their count is non-zero (`templates.go:3194`, `3204`).
- **32.c** No area qualifies ⇒ a single fallback area **“Nothing is waiting on you”** with *“When something needs you, it appears here.”* (`templates.go:3249–3254`).

**Exception flows**
- **32.e1** A caller holding none of the four gating verbs sees only the fallback area and an empty strip (D2).
- **32.e2** A store error in any count ⇒ `respondStoreError` and the whole page is replaced by the error page (`dashboard.go:73–74`).

**Business rules**
- BR-32.1 A count tells you a number; a work area hands you the thing to do — `internal/app/dashboard.go:10–20`.
- BR-32.2 Every area is gated on the same permission its queue is, so nobody is shown work behind a door they cannot open (D2) — `internal/app/dashboard.go:17–19`.
- BR-32.3 The count on the heading is the queue’s **real** size, never the length of what is shown — `internal/app/dashboard.go:48–49`.
- BR-32.4 The **Approved and unclaimed** count uses scope `all` and status `approved` and therefore **includes requests on hold**, whereas the Accounts queue’s takeable set excludes them (L7, specified in `UC-C-*`) — **DS12**.
- BR-32.5 `waitingOn` is called with the reader’s id, so *“Waiting on you”* is literal (`templates.go:3238`).

**Data touched** — reads `payment_requests` (six `CountRequests` calls plus up to five `ListRequests`), `users`, `role_permissions`.

**Non-functional / UX notes** — `h1` is *“Good day, <name>”*, so it is present on a phone. The metric strip scrolls horizontally inside its own container at 390px; tiles are links with a ≥40px hit area. Empty state as 32.c.

**Open questions** — DS12.

---

## 6. Negative and adversarial use cases

These are first-class: each is a rule the product must keep under attack.

### UC-B-33 — Approving my own request is refused (G8)

| | |
|---|---|
| **Goal** | *(adversarial)* “Approve the request I raised myself.” |
| **Primary actor** | Admin (AC4) or anyone holding both `request:create` and `approval:approve` |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | A crafted `POST /requests` with `manager_id` = self, or a crafted `POST /requests/{id}/approve` on a row whose requester is the caller. |
| **Route(s)** | `POST /requests` · `POST /requests/{id}/edit` · `POST /requests/{id}/approve` |
| **Permission gate** | `request:create` / `request:edit` / `approval:approve` — **and G8, which no grant overrides** |
| **Coverage IDs** | A1, A2, L11, R6 |
| **Priority** | critical |

**Preconditions**
1. The caller holds `approval:approve` and `request:create`.
2. The caller’s user id is what would be posted as `manager_id`.

**Postconditions (success of the refusal)** — no request is created with `manager_id == requester_id`, and no `pending` request is approved by its own raiser. **Three independent defences hold.**

**Postconditions (failure)** — n/a: the refusal writes nothing.

**Main success scenario**
1. Actor opens `/requests/new?type=reimbursement` and inspects **Approver**.
2. Their own name is **absent from the list**, because `ListApprovers` excludes `excludeUserID` in SQL (`store/requests.go:551–557`). The hint says so: *“You cannot approve your own request. Your own name is never in this list.”* (`templates.go:2020`).
3. Actor forges `manager_id=<self>` and posts.
4. `validateRequestInput` refuses: **400** *“validation failed: you cannot approve your own request — choose another approver”* (`store/requests.go:142–144`).
5. Actor instead takes an existing request they raised and posts `/requests/{id}/approve`.
6. `ApproveRequest` refuses on the manager check first (`before.ManagerID != actor.ID` ⇒ `ErrForbidden`, `store/requests.go:687–689`), and if the row somehow carried them as manager, the explicit G8 test refuses again: **403** (`store/requests.go:690–694`).

**Alternate flows**
- **33.a** Reassignment: `ReassignRequest` refuses `newManagerID == requester_id` — *“a request cannot be reassigned to its own requester”* (`store/requests.go:779–782`). Reachable since Wave 3 via `POST /requests/{id}/reassign-approver` (UC-B-31).
- **33.b** The Configuration screen renders **“Block self-approval”** checked and **disabled**, carrying **no `name`**, so it cannot be written — G8 is structural, not a setting: `<label class="checkline"><input type="checkbox" checked disabled> Block self-approval</label>` (`templates.go:3098`), with the section note *“Self-approval is blocked always. A person can never approve a request they raised, whatever roles they hold.”* (`configuration.go:68`).

**Exception flows**
- **33.e1** `manager_id=0` or absent ⇒ **400** *“choose an approver”* (VA4) — a different message, so tests must distinguish.
- **33.e2** A request edited to name the editor as approver ⇒ the same **400** from `UpdateRequest` (`store/requests.go:587`).

**Business rules**
- BR-33.1 **G8 — nobody may approve their own request, whatever roles they hold.** Enforced structurally in the approver list, in the validator and in the approve path — `internal/store/requests.go:140–144`, `548–550`, `690–694`.
- BR-33.2 `RequesterID` is filled by the store from the actor and never from the form, precisely so the check cannot be tricked — `internal/store/models.go:386–388`.

**Data touched** — nothing written. Reads `payment_requests`, `users`, `user_roles`, `role_permissions`.

**Non-functional / UX notes** — the refusal renders as `.alert.error` at the top of the re-rendered form (create/edit) or as the full error page (approve). No focus is moved to a field.

**Open questions** — none.

---

### UC-B-34 — Deciding a request assigned to a different approver is refused

| | |
|---|---|
| **Goal** | *(adversarial)* “Approve, return, reject or cancel a request that was sent to somebody else.” |
| **Primary actor** | Manager (AC2) · Admin (AC4) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | A crafted POST to any decision route on a row whose `manager_id` is not the caller. |
| **Route(s)** | `POST /requests/{id}/approve` · `/return` · `/reject` · `/cancel` · `/cancellation`; `GET /requests/{id}/cancellation` |
| **Permission gate** | the decision verb **plus** `manager_id == actor` |
| **Coverage IDs** | A5, R6, L11 |
| **Priority** | critical |

**Preconditions**
1. The request exists and is in a decidable status.
2. The caller holds the verb — an **Admin holds every verb** — but is not the row’s `manager_id`.

**Postconditions (success of the refusal)** — status, amounts, reasons and audit trail all unchanged.

**Postconditions (failure)** — n/a.

**Main success scenario**
1. Actor (an Admin) opens `/requests/{id}` for a request assigned to somebody else.
2. The action bar renders **no** decision controls: every one is gated on `$mineToDecide := eq .Request2.ManagerID .User.ID` (`templates.go:2400`, `2425`, `2430`, `2433`).
3. Actor posts `/requests/{id}/approve` anyway.
4. `ApproveRequest` returns `ErrForbidden` ⇒ **403** *“You do not have permission to perform this action.”* (`store/requests.go:687–689`, CV5).
5. The same holds for `/return` and `/reject` via `decideRequest` (`store/requests.go:728–730`), for `/cancel` via `CancelRequest` (`store/requests.go:976–978`) and for `/cancellation` via `DecideCancellation` (`store/requests.go:931–933`).
6. `GET /requests/{id}/cancellation` refuses **at the screen**: **403** *“Only the approver this request was sent to can decide its cancellation.”* (`requests.go:837–840`).

**Alternate flows**
- **34.a** Reading the request is still allowed for anyone whose scope reaches it — a Manager’s scope is `all` (`migrations.go:377`), so they see it and simply cannot act.
- **34.b** The `/approvals` queue never lists it: scope is hardcoded to `assigned` (BR-25.2).

**Exception flows**
- **34.e1** The partial-settlement decision follows the same rule: it belongs to the request’s **own** manager, not to anyone holding `approval:accept_partial` — an Admin holds every grant but is not the manager (`templates.go:2436–2439`; the decision itself is specified in `UC-C-*`).
- **34.e2** No verb at all ⇒ **403** from the middleware, before any row is read.

**Business rules**
- BR-34.1 **A5** — managers see all requests but may decide only those assigned to them — `docs/superpowers/specs/2026-07-25-phase-2-request-workflow-spec.md:180`, enforced at `internal/store/requests.go:687`, `728`, `931`, `976`.
- BR-34.2 Holding a verb is never the same as being allowed to act on a given row (R6).

**Data touched** — nothing written.

**Non-functional / UX notes** — the refusal is the chrome-less error page (`http_errors.go:121`), which offers its own two ways out rather than a sidebar into an app that just refused.

**Open questions** — none.

---

### UC-B-35 — There is no bulk approval (A6)

| | |
|---|---|
| **Goal** | *(adversarial)* “Approve twenty requests at once.” |
| **Primary actor** | Manager (AC2) · Admin (AC4) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | Looking for a checkbox column on `/approvals`, or posting a guessed bulk endpoint. |
| **Route(s)** | none — `POST /approvals`, `POST /requests/bulk-approve`, `POST /approvals/bulk` are all unrouted |
| **Permission gate** | n/a |
| **Coverage IDs** | A6 |
| **Priority** | high |

**Preconditions**
1. Session with `approval:approve`.
2. Two or more `pending` requests assigned to the caller.

**Postconditions (success of the refusal)** — no status changes; no such route exists.

**Postconditions (failure)** — n/a.

**Main success scenario**
1. Actor opens `/approvals`.
2. There is **no checkbox column, no select-all and no “approve selected” button** in the markup (`templates.go:2920–2922`, `2955–2957`).
3. The screen states the rule in prose: *“Every approval is a decision made after opening the request. There is no bulk approval, by design.”* (`templates.go:2959–2960`).
4. Actor posts `/requests/bulk-approve`.
5. The path matches only the catch-all `GET /` pattern, so the method does not match ⇒ **405 Method Not Allowed** (`app.go:376`; PROGRESS records this as a trap).
6. Actor posts `/approvals`. `GET /approvals` is registered, so again ⇒ **405**.

**Alternate flows**
- **35.a** `GET /requests/bulk-approve` ⇒ **404** from `notFound` *“That page does not exist. It may have moved, or it may not be built yet.”* (`http_errors.go:158–160`).

**Exception flows**
- **35.e1** A test asserting 404 alone is wrong; **accept 404 or 405** (`docs/superpowers/PROGRESS.md:288–290`).

**Business rules**
- BR-35.1 **A6** — every approval is a per-request decision taken after opening the request — `internal/app/requests.go:884–886`, `internal/app/templates.go:2920–2922`.
- BR-35.2 An unrouted POST answers 405, not 404 (CV1).

**Data touched** — nothing.

**Non-functional / UX notes** — proof-of-absence assertions only: count checkboxes on `/approvals` (expect 0) and assert the status code of the guessed POST.

**Open questions** — none.

---

### UC-B-36 — Returning or rejecting without words is refused

| | |
|---|---|
| **Goal** | *(adversarial)* “Send it back, or kill it, without saying why.” |
| **Primary actor** | Manager (AC2) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | Submitting the return or reject sheet with nothing, or with whitespace, in the reason field. |
| **Route(s)** | `POST /requests/{id}/return` · `POST /requests/{id}/reject` |
| **Permission gate** | `approval:return` / `approval:reject` **plus** `manager_id == actor` |
| **Coverage IDs** | A3, A4, L11 |
| **Priority** | high |

**Preconditions**
1. Status `pending`, `manager_id` equals the caller.

**Postconditions (success of the refusal)** — status stays `pending`, `decision_reason` unchanged, no audit row, no notification.

**Postconditions (failure)** — n/a.

**Main success scenario**
1. Actor opens `#return-sheet` and presses **Return request** with the field empty.
2. The browser blocks it: `<textarea id="rt-reason" name="comment" required>` (`templates.go:2229`).
3. Actor types three spaces and presses again; the browser lets it through.
4. `decideRequest` trims the value, finds it empty and returns `ErrValidation` ⇒ **400** *“validation failed: a comment is required to return a request”* (`store/requests.go:715–718`, `752`).
5. Actor repeats on `#reject-sheet` ⇒ **400** *“validation failed: a reason is required to reject a request”* (`store/requests.go:756`).

**Alternate flows**
- **36.a** The same trim-then-refuse pattern guards `RequestCancellation` (*“say why it should be cancelled”*), `DecideCancellation` decline (*“say why it should still be paid”*), `CancelRequest` (*“a reason is required to cancel a request”*), `ReassignRequest` (*“a reason is required to reassign”*) and `AddRequestComment` (*“comment cannot be empty”*).
- **36.b** An approval **note** is genuinely optional — the only reason field in the flow that is (`templates.go:2217`).

**Exception flows**
- **36.e1** The refusal renders as the **full error page**, not as the sheet with an inline message: `requestReturn`/`requestReject` call `respondStoreError` directly (`requests.go:731–736`, `740–745`), so the manager loses the sheet and must reopen it. See **DS13**.

**Business rules**
- BR-36.1 Returning and rejecting demand words, and approving offers an amount, so each gets a moment and a form of its own rather than a bare inline button — `internal/app/templates.go:2199–2201`.
- BR-36.2 Whitespace is not words: every reason is `strings.TrimSpace`d before the emptiness test — `internal/store/requests.go:715`.

**Data touched** — nothing written.

**Non-functional / UX notes** — the `required` attribute is safe here because these fields are **not** inside a `data-when` reveal (BR-02.4). Test the whitespace case, not the empty case, to reach the server rule.

**Open questions** — DS13.

---

### UC-B-37 — Withdrawing an already-approved request is refused

| | |
|---|---|
| **Goal** | *(adversarial)* “Withdraw it after my approver already said yes.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | A crafted `POST /requests/{id}/withdraw` on an `approved` row. |
| **Route(s)** | `POST /requests/{id}/withdraw` |
| **Permission gate** | `request:withdraw` **plus** requester-only **plus** the transition table |
| **Coverage IDs** | Q2, L6, L11, G1 |
| **Priority** | high |

**Preconditions**
1. The caller raised the request.
2. Status is `approved` (or any status other than `pending`).

**Postconditions (success of the refusal)** — `status` stays `approved`, `approved_amount`/`approved_by`/`approved_at` intact, no audit row.

**Postconditions (failure)** — n/a.

**Main success scenario**
1. Actor opens their approved request.
2. The **Withdraw** button is absent — it is gated on `$mine` **and** `pending` (`templates.go:2416`). **Request cancellation** is offered instead (`templates.go:2419`).
3. Actor posts `/requests/{id}/withdraw` by hand.
4. `WithdrawRequest` finds `canTransition("approved","withdrawn")` false ⇒ **400** *“validation failed: a approved request cannot be withdrawn”* (`store/requests.go:658–660`).
5. The correct path is UC-B-22: ask, and the approver decides.

**Alternate flows**
- **37.a** Status `returned` ⇒ the same refusal; a returned request is corrected and resubmitted, or left.
- **37.b** Status `cancellation_requested`, `rejected`, `cancelled`, `withdrawn`, `processing`, `partial_review`, `completed*` ⇒ the same refusal, naming the status.

**Exception flows**
- **37.e1** Not the requester ⇒ **403**, checked **before** the transition test (`store/requests.go:655–657`), so an outsider gets 403 and the owner gets 400 — two distinguishable outcomes.

**Business rules**
- BR-37.1 `pending` is the only status with a legal edge to `withdrawn` — `internal/store/requests.go:93`.
- BR-37.2 **G1** — after approval the requester asks rather than acts, and payment freezes at the moment of asking — `internal/store/requests.go:858–860`.
- BR-37.3 The screen never offers a control whose submit the store would refuse (BR-20.3).

**Data touched** — nothing written.

**Non-functional / UX notes** — the grammatical artefact *“a approved request”* comes from the format string (`store/requests.go:659`) and is user-visible; assert on the substring, not the whole sentence.

**Open questions** — none.

---

### UC-B-38 — Editing a rejected request is refused

| | |
|---|---|
| **Goal** | *(adversarial)* “Fix the rejected one and slip it back through.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | `GET /requests/{id}/edit` or `POST /requests/{id}/edit` on a `rejected` row. |
| **Route(s)** | `GET /requests/{id}/edit` · `POST /requests/{id}/edit` |
| **Permission gate** | `request:edit` **plus** `loadEditableRequest` **plus** `editableStatuses` in the store |
| **Coverage IDs** | L4, Q1, Q3, L11 |
| **Priority** | high |

**Preconditions**
1. The caller raised the request.
2. Status is `rejected`.

**Postconditions (success of the refusal)** — every field and the status unchanged; no audit row.

**Postconditions (failure)** — n/a.

**Main success scenario**
1. Actor opens their rejected request; the bar offers only **Raise it again** — **Edit request** is gated on `pending`/`returned` (`templates.go:2413`).
2. Actor navigates to `/requests/{id}/edit`.
3. `loadEditableRequest` refuses: **400** *“This request can no longer be edited. A rejected request is final — raise a new one.”* (`requests.go:596–599`, `610–611`).
4. Actor posts to the same path; the same handler-level check refuses before any store call.
5. Even if the handler check were bypassed, `UpdateRequest` refuses: `editableStatuses` has no `rejected` ⇒ **400** *“a rejected request cannot be edited”* (`store/requests.go:575`, `613–615`).
6. The correct path is UC-B-21.

**Alternate flows**
- **38.a** Status `approved` ⇒ *“It is approved and locked; ask for it to be cancelled instead.”*
- **38.b** Status `cancellation_requested` ⇒ *“A cancellation is already pending on it.”*
- **38.c** Status `withdrawn`/`cancelled`/`processing`/`completed*` ⇒ the default branch: *“It is <lower-cased status text>.”* (`requests.go:614–616`).

**Exception flows**
- **38.e1** Another person’s rejected request ⇒ **403** *“Only the person who raised a request may edit it.”* — checked **after** the view scope test, so an out-of-scope caller gets **404** *“The requested record was not found.”* instead (`loadEditableRequest` calls `loadViewableRequest` first, `internal/app/requests.go:699-707`). *(At `30edd6a` the scope refusal was 403 “You do not have permission to view this request.”; changed by F-G-002, Wave 3, `1fac147`. The two outcomes are still distinguishable — 403 for a scope-reaching non-owner, 404 for an out-of-scope caller — which is the point: only the second is an existence question.)*
- **38.e2** `SubmitRequest` on a rejected row ⇒ **400** *“a rejected request cannot be submitted”* (`store/requests.go:469–471`).

**Business rules**
- BR-38.1 **L4** — rejected is final and read-only — `internal/store/requests.go:91–101` (no outgoing edge), `internal/app/requests.go:610–611`.
- BR-38.2 Two independent layers refuse it: the handler for an honest screen, the store for a hand-rolled POST — `internal/app/requests.go:584–586`.

**Data touched** — nothing written.

**Non-functional / UX notes** — the refusal is the error page with the explanatory second sentence, which is the only place the product tells the requester what to do instead. Assert on *“raise a new one”*.

**Open questions** — none.

---

### UC-B-39 — Re-raising something that was withdrawn, not rejected, is refused

| | |
|---|---|
| **Goal** | *(adversarial)* “Re-raise the one I withdrew and keep the copy.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | A crafted `POST /requests/{id}/reraise` on a `withdrawn` row. |
| **Route(s)** | `POST /requests/{id}/reraise` |
| **Permission gate** | `request:reraise` **plus** requester-only **plus** an exact `rejected` status test |
| **Coverage IDs** | Q3, L5, L11, C4 |
| **Priority** | high |

**Preconditions**
1. The caller raised the request.
2. Status is `withdrawn`.

**Postconditions (success of the refusal)** — **no new row, and no number consumed**: the whole method runs inside one transaction that is rolled back before `NextRequestNumber` is reached (`store/requests.go:806–820`).

**Postconditions (failure)** — n/a.

**Main success scenario**
1. Actor opens their withdrawn request; the bar offers nothing — **Raise it again** is gated on `rejected` (`templates.go:2422`).
2. Actor posts `/requests/{id}/reraise`.
3. `ReraiseRequest` checks the requester, then `src.Status != "rejected"` ⇒ **400** *“validation failed: only a rejected request can be re-raised”* (`store/requests.go:818–820`).

**Alternate flows**
- **39.a** Status `cancelled` ⇒ the same refusal — an approver-cancelled request is not re-raisable either; the requester raises a fresh one.
- **39.b** Status `pending`/`approved`/`returned` ⇒ the same refusal.

**Exception flows**
- **39.e1** Not the requester ⇒ **403**, checked **before** the status test (`store/requests.go:815–817`).
- **39.e2** No `request:reraise` ⇒ **403** from the middleware.

**Business rules**
- BR-39.1 Re-raise is scoped to `rejected` only, by an explicit status equality rather than by `canTransition` — `internal/store/requests.go:818–820`.
- BR-39.2 The status test precedes number reservation, so a refused re-raise never burns a number and C4’s monotonicity is unaffected.
- BR-39.3 Withdrawn and cancelled are both terminal (`internal/store/requests.go:91–101`).

**Data touched** — nothing written; `request_number_seq` untouched.

**Non-functional / UX notes** — assert the number sequence is unchanged by comparing the next successful request’s number before and after the attempt.

**Open questions** — a requester who withdraws by mistake has no “restore” and must retype the whole request; whether that is intended is unresolved.

---

### UC-B-40 — A concealed `data-when` fieldset is re-enforced server-side

| | |
|---|---|
| **Goal** | *(adversarial)* “Post the fields the form hid from me and see what sticks.” |
| **Primary actor** | Requester (AC1), by crafted POST |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | A `POST /requests` carrying every field the adaptive form is capable of revealing, regardless of the chosen treatment or type. |
| **Route(s)** | `POST /requests` · `POST /requests/{id}/edit` |
| **Permission gate** | `request:create` / `request:edit` + CSRF |
| **Coverage IDs** | T1, T2, T9, V1, V5, L11, R6 |
| **Priority** | critical |

**Preconditions**
1. Session with `request:create`.
2. A body containing, simultaneously: `treatment=budget`, `type=vendor_invoice`, `project_id`, `head_id`, `vendor_id`, `invoice_no`, `invoice_date`, **and** `recoverable_category=icd`, `expected_return_date`, `repayment_notes`, `counterparty`, `expense_date`, `advance_reason`.

**Postconditions (success of the refusal / of the acceptance)**
1. Every rule the concealed fieldset would have imposed is applied by `validateRequestInput`, which reads **every** field unconditionally — `hidden` is not validation (`requests.go:295–297`, `templates.go:1725–1728`).
2. A body that satisfies VT1 **is accepted**, and the recoverable columns are **written anyway**: `CreateRequest` inserts `counterparty`, `expected_return_date`, `repayment_notes` and `recoverable_category` with no treatment test (`store/requests.go:414–427`). `recoverable_category_id` alone is nulled, because `recoverableCategoryLink` short-circuits on a non-recoverable treatment (`store/recoverables.go:134–137`). See **DS3**.
3. A body that contradicts the type is refused with the type’s own message (VT2/VT4/VT6/VT10).

**Postconditions (failure)** — nothing written.

**Main success scenario**
1. Actor posts `treatment=recoverable` with `type=vendor_invoice`.
2. **400** *“a vendor invoice is a budget expense”* — the reveal is re-enforced (`store/requests.go:194–196`).
3. Actor posts `treatment=recoverable`, `type=employee_advance`, `recoverable_category=emd` and **no** `project_id`.
4. **400** *“this recoverable category always belongs to a project”* — even though the browser never showed the field to them (`store/requests.go:184–186`).
5. Actor posts `treatment=recoverable`, `type=employee_advance`, `recoverable_category=icd` and a blank `counterparty`.
6. **400** *“this recoverable category needs a counterparty company”* (`store/requests.go:187–189`).
7. Actor posts `treatment=budget`, `urgent=on` with `urgency_mode=disabled`.
8. **400** *“urgent requests are switched off”* (VG2).

**Alternate flows**
- **40.a** Two `project_id` values in one body: `r.FormValue` returns the **first**, which is why the budget and recoverable fieldsets are rendered as alternatives rather than one-of-two-hidden (BR-03.1).
- **40.b** `q=<free text>` (the combobox’s own field name) is posted and simply ignored — `requestInput` never reads it.
- **40.c** Unknown extra fields are ignored entirely; `requestInput` enumerates the fields it accepts (`requests.go:298–321`).

**Exception flows**
- **40.e1** A `recoverable_category` code that is unknown or inactive ⇒ **400** *“choose a recoverable category”* even on a budget request? **No** — the category is only validated inside `needsRecoverable`, so on a budget treatment a nonsense code passes validation and is stored (**DS3**).
- **40.e2** Forged `head_id` belonging to a different project ⇒ **accepted**; nothing checks the pairing (**DV6**).
- **40.e3** Forged `project_id`/`head_id` for an **inactive** project or head ⇒ **accepted**; requests run no `ErrInactiveHead` check (only payments do, `store/store.go:1625–1638`).

**Business rules**
- BR-40.1 **`hidden` fieldsets still submit, and `hidden` is not validation.** Re-enforce every `data-when` reveal server-side — `docs/superpowers/PROGRESS.md:275–276`, `internal/app/requests.go:29–33`.
- BR-40.2 Every field the adaptive form can reveal is read unconditionally, because a browser that hid a control is no guarantee about the submitter’s browser — `internal/app/requests.go:295–297`.
- BR-40.3 A fieldset the server did not render is one the requester cannot fill in, and the validator refuses it a second time if they forge it anyway — `internal/app/requests.go:100–104`.
- BR-40.4 `required` is never used on a `data-when`-hidden control, so client validation cannot be the enforcement (BR-02.4).

**Data touched** — writes `payment_requests` on the accepted paths; nothing on the refused ones.

**Non-functional / UX notes** — untestable through the UI; drive it with `request.post()` carrying a valid CSRF cookie/field pair, or with a Go handler test. The refusals surface as the re-rendered form at HTTP 400 with the `.alert.error` banner.

**Open questions** — DS3, DV6.

---

### UC-B-41 — Posting a `vendor-id` for a vendor that does not exist

| | |
|---|---|
| **Goal** | *(adversarial)* “Attach a vendor id the master has never heard of.” |
| **Primary actor** | Requester (AC1), by crafted POST |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | `POST /requests` with `vendor_id=999999`. |
| **Route(s)** | `POST /requests` · `POST /requests/{id}/edit` |
| **Permission gate** | `request:create` / `request:edit` + CSRF |
| **Coverage IDs** | T6, T7, R6, C2 |
| **Priority** | high |

**Preconditions**
1. Session with `request:create`.
2. `type=vendor_invoice` or `vendor_advance`, `treatment=budget`, every other VT1/VT3 field valid.
3. `vendor_id` is a positive integer with no matching `vendors.id`.

**Postconditions (success of the refusal)** — no `payment_requests` row, no number consumed beyond the rolled-back transaction, no audit row, staged file removed.

**Postconditions (failure)** — n/a.

**Main success scenario**
1. Actor posts the body.
2. `validateRequestInput` passes: `needsVendor` only asserts `VendorID > 0` (`store/requests.go:167–172`).
3. `CreateRequest` reaches the `INSERT`, which violates `vendor_id INTEGER REFERENCES vendors(id)` under `PRAGMA foreign_keys=ON` (`migrations.go:172`; the pragma rides the DSN in `store.Open`).
4. `classify` maps only `UNIQUE` violations; a foreign-key failure falls through unchanged (`store/store.go:1712–1720`).
5. `storeErrorStatus` therefore returns **500**, and `requestCreate` takes the `>= 500` branch and renders the error page rather than the form (`requests.go:164–171`).
6. Result: **500 “Something went wrong while processing your request.”** — the transaction rolls back, so nothing is written. **DS1.**

**Alternate flows**
- **41.a** `vendor_id=0` or absent ⇒ the clean **400** *“choose a vendor from the vendor master”*.
- **41.b** `vendor_id` of an **inactive** vendor ⇒ **accepted**; only `SearchVendors`/`ListVendors` filter on status, not the validator (`store/vendors.go:350`, `requests.go:290`).
- **41.c** The same 500 shape applies to a forged `manager_id`, `project_id` or `head_id` pointing at no row — all four columns carry `REFERENCES` (`migrations.go:169–188`).

**Exception flows**
- **41.e1** On `POST /requests/{id}/edit` the identical failure surfaces from `UpdateRequest`’s `classify(err)` (`store/requests.go:631`) ⇒ **500** error page.

**Business rules**
- BR-41.1 The hidden vendor id is what the server reads, and it is expected to come from the vendor master — `internal/app/templates.go:1895–1899`. Referential integrity is delegated to SQLite rather than checked in Go.
- BR-41.2 Foreign keys are enforced on **every** pooled connection, because the pragmas ride the DSN — `docs/superpowers/PROGRESS.md:266–269`.
- BR-41.3 `classify` recognises only duplicate-key failures — `internal/store/store.go:1712–1720`.

**Data touched** — nothing written. Reads `vendors` indirectly via the FK check.

**Non-functional / UX notes** — the 500 page is chrome-less and carries the request id for log correlation (`http_errors.go:117–125`, `templates.go:100–104`). Expect **500**, not 400, in the assertion until DS1 is fixed.

**Open questions** — DS1: whether a foreign-key violation should be classified as validation (400) rather than surfacing as a server error.

---

### UC-B-42 — A Requester opening another requester’s request by id

| | |
|---|---|
| **Goal** | *(adversarial)* “Type a different id in the URL and read somebody else’s request.” |
| **Primary actor** | Requester (AC1) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | Editing the id in `/requests/{id}` or in any of its sub-paths. |
| **Route(s)** | `GET /requests/{id}` · `/submitted` · `/edit` · `/cancel` · `/cancellation` · `/partial-review`; `POST /requests/{id}/comment` |
| **Permission gate** | `request:view` (or the sub-route’s verb) **plus** `canViewRequest(scope, user, req)` |
| **Coverage IDs** | Q5, R3, R6 |
| **Priority** | critical |

**Preconditions**
1. The caller’s `request` scope is `own` — the seeded Requester role (`migrations.go:363`).
2. The target request was raised by somebody else.

**Postconditions (success of the refusal)** — **404**, not 403, and **no field of the target request appears in the response body**: the refusal happens before any render (`loadViewableRequest`, `internal/app/requests.go:295-308`). *(Changed from 403 in Wave 3, `1fac147`, F-G-002 — see 42.e1.)*

**Postconditions (failure)** — n/a.

**Main success scenario**
1. Actor opens `/requests/{someone-elses-id}`.
2. `loadViewableRequest` reads the row, then applies `canViewRequest`: scope `own` requires `req.RequesterID == u.ID` (`requests.go:332–343`).
3. **404** *“The requested record was not found.”* — the chrome-less error page, with the real reason (`request %d is outside the caller's data scope`) logged, not rendered (`requests.go:295-308`). *(At `30edd6a` this was 403 **“You do not have permission to view this request.”** — see 42.e1 for why that changed.)*
4. Actor tries `/requests/{id}/submitted`, `/edit`, `/cancel` and `POST /comment`; every one funnels through `loadViewableRequest` and answers the same **404**.
5. Actor tries `/requests?scope=all`; `effectiveScope` refuses to widen and keeps them on `own` (`requests.go:347–354`).
6. Actor tries `/requests/export.csv?scope=all`; the same narrowing applies (`requests.go:929`).

**Alternate flows**
- **42.a** Scope `assigned` (no seeded role uses it for `request`, but the vocabulary supports it) ⇒ visible when the caller is the row’s **manager or** its requester (`requests.go:337`).
- **42.b** Scope `all` (Manager, Accounts, Admin) ⇒ every row is visible; A5 then limits **action**, not sight.
- **42.c** No scope at all ⇒ `canViewRequest` default branch returns false, so every id is refused (`requests.go:340–342`).

**Exception flows**
- **42.e1** A non-existent id ⇒ **404** *“The requested record was not found.”* — **the same status** an out-of-scope id now gets (step 3 above), so the response no longer discloses existence to a caller who cannot see the row. At `30edd6a` an out-of-scope id answered 403, a different status from a non-existent id's 404, which did disclose existence — recorded as **DS14**, fixed F-G-002 Wave 3 (`1fac147`).
- **42.e2** `/requests/{id}/reservation` requires only a session at the route (`app.go:464`); `reservationForm` applies its own ownership rules (specified in `UC-C-*`).
- **42.e3** `/requests/{id}/partial-review` is gated on `request:view` and applies the same scope test.

**Business rules**
- BR-42.1 **Q5/R6** — holding `request:view` somewhere is not the same as being allowed to read a given row — `internal/app/requests.go:226–228`, `330–331`.
- BR-42.2 A URL may narrow a caller’s scope and never widen it — `internal/app/requests.go:345–354`.
- BR-42.3 The scope clause is SQL on the list and a Go predicate on the detail; both derive from the same `PermissionSet.Scope` — `internal/store/requests.go:1209–1216`, `internal/app/requests.go:236`.

**Data touched** — reads `payment_requests` (one row, discarded), `role_permissions`.

**Non-functional / UX notes** — the 404 page is chrome-less and offers its own two ways out. Assert both the status **and** that the response body contains none of the target’s short title, number or amount.

**Open questions** — DS14 (resolved, see above).

---

## 7. Divergences and suspected defects

Findings only — nothing here was changed.

### 7.1 Divergences (code vs. spec or doc)

| ID | Claim | Spec / doc says | Code does |
|---|---|---|---|
| DV1 | `recoverable` is a request type users can raise | *“**Types:** `vendor_invoice`, `vendor_advance`, `reimbursement`, `employee_advance`, `recoverable`.”* (`docs/superpowers/specs/2026-07-25-payment-requests-overview.md:251`) | `requestTypeOptions` has four entries, so the chooser, the labels and the form know only four; `?type=recoverable` silently re-renders the chooser (`internal/app/requests.go:42–55`, `71–76`). The store still accepts it (`internal/store/requests.go:78–81`). |
| DV2 | The request list is *“sorted by who has been kept waiting longest”* | code comment `internal/store/requests.go:1276–1277`; screen sub-line *“sorted by who is holding them up”* (`internal/app/templates.go:2119`) | `ORDER BY r.urgent DESC, r.created_at DESC, r.id DESC` — **newest first** (`internal/store/requests.go:1278`). |
| DV3 | An admin can add a recoverable category and have it used (V4) | *“an admin can add a category and have its rules enforced without a code change (V4)”* (`internal/store/requests.go:109–115`) | Enforcement is table-driven and does work (`internal/store/recoverables.go:119–129`), but the **`<select>` is hardcoded to the six seeded codes** (`internal/app/templates.go:1738–1743`), the display labels are a hardcoded map (`internal/app/requests.go:1026–1033`), and `normalizeRecoverableCategory` rewrites any unrecognised code to `emd`/`employee_advance` (`internal/app/requests.go:132–140`). A new category is therefore enforceable but not selectable, and renders as its raw code. |
| DV4 | One category, one name | `recoverableCategorySeed` names `emd` **“EMD”** (`internal/store/recoverables.go:27`) | The request form option reads **“EMD — earnest money deposit”** and so does `recoverableLabel` (`internal/app/templates.go:1738`, `internal/app/requests.go:1027`), while the Configuration screen and the recoverable register render `rc.name` = “EMD”. |
| DV5 | The advertised attachment cap is the enforced one | screen: *“PDF, JPG or PNG up to {{attachment_max_mb}} MB”*, default **10** (`internal/app/templates.go:1994`, `internal/store/migrations.go:244`) | The server enforces **20 MiB** and the setting is never read by the validator (`internal/app/http_errors.go:23`, `internal/app/app.go:948`). |
| DV6 | A head belongs to the chosen project | the form filters heads by project (`internal/app/templates.go:1799`) | Nothing validates the pairing on submit; `needsProjectHead` checks only that both ids are positive (`internal/store/requests.go:161–166`). |
| DV7 | A7 “Admin reassign (reason + history)” is delivered | *“`POST /requests/{id}/reassign` \| approval/reassign \| `requestReassign` \| Admin reassign, reason + history (A7)”* (`docs/superpowers/specs/2026-07-25-phase-2-request-workflow-spec.md:206`) | **Resolved as of Wave 3, `1fac147`** (`docs/qa/results/REPAIR-LOG.md`) — see UC-B-31. The exact path the spec names, `POST /requests/{id}/reassign`, is still registered on `reservation:reassign` and still handles reservation handover, unrelated to this requirement (`internal/app/app.go:466`, `internal/app/linking.go:642–674`); the repair did not rewire that path. Instead A7 got its own route, `POST /requests/{id}/reassign-approver` (`app.go:569`, handler `app.go:1615-1647`), over the same `store.ReassignRequest` (`internal/store/requests.go:1131`; `:759` at the time of this audit), with a screen control at `internal/app/templates.go:2578-2603`. |
| DV8 | The fixtures’ payee gap | `tests/e2e/fixtures.ts:178–188` “KNOWN GAP … every Phase-3 screen reads `Request.VendorPayee`” | **Fixed** at `fca6939`; `internal/app/linking.go:260` writes `VendorPayee: req.Vendor`. The comment is stale and should not be propagated. |

### 7.2 Suspected defects, ranked

| ID | Severity | Defect | Concrete failure |
|---|---|---|---|
| DS1 | high | A foreign-key violation surfaces as **500**, not 400. `classify` handles only `UNIQUE` (`internal/store/store.go:1712–1720`). | `POST /requests` with `vendor_id=999999` (or a bogus `manager_id`, `project_id`, `head_id`) renders the 500 error page and loses the whole typed form, instead of the 400 re-render with a message (UC-B-41). |
| DS2 | high | The **edit** screen’s vendor combobox cannot search: its `combo-input` has **no `name`** (`internal/app/templates.go:2486`), so htmx sends no `q`, and `SearchVendors("")` returns nothing (`internal/store/vendors.go:337–339`). | On `/requests/{id}/edit` for a `vendor_invoice`, typing any vendor name yields only the *“Type a name, GSTIN or city”* row; the vendor on a pending request cannot be changed. The create form works because its input carries `name="q"` (`internal/app/templates.go:1902`). |
| DS3 | medium | Recoverable columns are written regardless of treatment. `CreateRequest`/`UpdateRequest` insert `counterparty`, `expected_return_date`, `repayment_notes` and `recoverable_category` unconditionally (`internal/store/requests.go:414–427`, `618–630`). | A hand-rolled budget `vendor_invoice` carrying those fields stores them, and `request_detail` then renders **Counterparty**, **Expected return** and **Repayment terms** rows on a budget expense (`internal/app/templates.go:2355–2357`). Nothing scrubs the concealed fieldset. |
| DS4 | medium | Every validation message reaches the user with the sentinel prefix. `friendly` returns `err.Error()` for `ErrValidation` (`internal/app/app.go:1633`), and the errors wrap `errors.New("validation failed")`. | The banner reads *“validation failed: the invoice number is required”*. |
| DS5 | medium | Editing a request whose project or head was since **deactivated** cannot be saved. Both lists are active-only (`internal/app/requests.go:362–369`), so the stored option is absent, nothing is selected and the ids post empty. | Deactivate the project of a pending `vendor_invoice`, open `/requests/{id}/edit`, change the amount, save ⇒ **400** *“project and head are required for this type”* with no way forward. `request_returned` is immune because it posts the ids as hidden inputs (`internal/app/templates.go:2700–2701`). |
| DS6 | medium | A failed resubmit leaves the corrections saved. `UpdateRequest` and `SubmitRequest` are two separate transactions (`internal/app/requests.go:658–665`). | With `require_attachments=1`, no attachment and a blank exception reason, pressing **Resubmit for approval** writes the edits (and the `update` audit row), then fails at **400**; the request stays `returned` with changes already applied. |
| DS7 | medium | A re-raise runs no attachment policy (`internal/store/requests.go:805–856`) and copies no attachments. | With `require_attachments=1`, **Raise it again** produces a `pending` request with zero documents and whatever exception reason the rejected one carried — a path around G10. |
| DS8 | low | `ListRequests` caps at 200 and the CSV export uses it (`internal/store/requests.go:1272–1274`, `internal/app/requests.go:928`). | An organisation with 300 open requests exports 200 rows with no warning and no pagination control anywhere on `/requests`. |
| DS9 | low | The approvals queue has no tab for requests the manager returned. `approvalTabs` covers `pending`, `cancellation_requested`, and `approved`/`rejected`/`cancelled` (`internal/app/requests.go:877–881`). | A returned request vanishes from the approver’s screens entirely; only the requester’s **Needs me** bucket carries it. |
| DS10 | low | **Fixed for three of the four paths, one still open** — F-F-06, Wave 2 vocabulary (`25411b8`) + Wave 4 wiring (`docs/qa/results/REPAIR-LOG.md`). At `30edd6a`, no notification fired for a withdrawal, an outright cancellation, or either cancellation decision. `requestWithdraw` now calls `a.fire(r, notify.EventRequestWithdrawn, …)` (`internal/app/requests.go:916`) and `requestCancellationDecide` fires `EventCancellationAccepted`/`EventCancellationDeclined` (`requests.go:1029`, `1031`) — three of the nine events migration v9 added, bringing the vocabulary from twelve to 21 (`internal/notify/events.go:71-79`). **`requestCancelOutright` still calls no `a.fire`** (`requests.go:1036-1043`), and `notify.AllEvents` has no outright-cancel event, so an approver cancelling an approved request outright still notifies nobody. | Partly superseded. The outright-cancel half stands: see UC-B-29's open question. |
| DS11 | low | **Fixed** (DV7) — `store.ReassignRequest` now has a caller, `requestReassignApprover` (`internal/app/app.go:1615-1647`), over `POST /requests/{id}/reassign-approver`. | Superseded; kept for history. |
| DS12 | low | The dashboard’s **Approved, unclaimed** count uses `status='approved'` with no hold filter (`internal/app/dashboard.go:94–100`), while the Accounts queue’s takeable set excludes held rows (L7). | With one approved request on hold, the tile reads 1 and the Accounts queue offers nothing to take. |
| DS13 | low | A refused return/reject/approve replaces the whole page with the error page rather than re-opening the sheet (`internal/app/requests.go:717–745`). | A manager who types whitespace into **What needs correcting** loses the sheet and must navigate back and reopen it. |
| DS14 | informational | **Fixed** — F-G-002, Wave 3 (`1fac147`). Both a non-existent id and an out-of-scope id now answer **404**: `loadViewableRequest` (`internal/app/requests.go:295-308`) applies `canViewRequest` and answers the same 404 either way, with the real reason logged, not rendered. | Superseded; kept for history. The old citation (`requests.go:231-239`) no longer points at this logic — it moved to `loadViewableRequest`. |

---

## 8. Traceability

### 8.1 Use case → coverage IDs → routes → actor

| UC ID | Coverage matrix IDs | Route(s) | Primary actor |
|---|---|---|---|
| UC-B-01 | T1, T2, D5 | `GET /requests/new` | Requester |
| UC-B-02 | T1, T2, T3, T4, T5, T12, A1 | `GET /requests/new?type=…` | Requester |
| UC-B-03 | T1, T2, T9, V5 | `GET /requests/new/fields` | Requester |
| UC-B-04 | T2 (G6) | `POST /requests/duplicate-check` | Requester |
| UC-B-05 | T1, T2, T3, T5, T6, T12, A1, L1, L2, L11, C2, C3, C4, D1 | `POST /requests` | Requester |
| UC-B-06 | T1, T2, T5, T7, T12, A1, L2, C2, C3, C4, D1 | `POST /requests` | Requester |
| UC-B-07 | T1, T2, T5, T8, T11, T12, A1, L2, C2, C3 | `POST /requests` | Requester |
| UC-B-08 | T1, T2, T5, T9, T11, T12, A1, L2, V1 | `POST /requests` | Requester |
| UC-B-09 | T1, T2, T5, T9, T11, A1, L2, V1, V4, V5, V6, V8 | `POST /requests` | Requester |
| UC-B-10 | T2, T9, V1, V5, V6, L11 | `POST /requests` (no UI path) | Requester (crafted) |
| UC-B-11 | T3, T6, T7 | `GET /vendors/search` | Requester |
| UC-B-12 | T10, N7, C2 | `POST /requests`, `POST /requests/{id}/edit` | Requester |
| UC-B-13 | T10 (G10) | `POST /requests`, `POST /requests/{id}/edit` | Requester |
| UC-B-14 | T5, N6 | `POST /requests` | Requester |
| UC-B-15 | L2, D1, C4, N1 | `GET /requests/{id}/submitted` | Requester |
| UC-B-16 | Q4, Q5, Q6, N7, A5, L3, L4, L5, L6, L7, C2 | `GET /requests/{id}` | all four |
| UC-B-17 | N7, Q6, C2 | `POST /requests/{id}/comment` | any viewer with `request:comment` |
| UC-B-18 | A8, Q1, T12, L2, C2 | `GET`+`POST /requests/{id}/edit` | Requester |
| UC-B-19 | Q1, L3, L11, A8, T10, C2 | `GET /requests/{id}`, `POST /requests/{id}/edit` | Requester |
| UC-B-20 | Q2, L5, L11, C2 | `POST /requests/{id}/withdraw` | Requester |
| UC-B-21 | Q3, L4, D1, C4, C2 | `POST /requests/{id}/reraise` | Requester |
| UC-B-22 | Q2, L6, L11, C2 (G1) | `GET /requests/{id}/cancel`, `POST /requests/{id}/cancel-request` | Requester |
| UC-B-23 | Q5, A5, R3, R6, D3, D4, T12 | `GET /requests` | all four |
| UC-B-24 | D3, Q5, R6, C2, C3 | `GET /requests/export.csv` | all four |
| UC-B-25 | A5, A6, D4, Q5, L2, L6 (G1) | `GET /approvals` | Manager |
| UC-B-26 | A2, A5, L6, L11, C2, C3, N3 | `POST /requests/{id}/approve` | Manager |
| UC-B-27 | A4, Q1, L3, L11, C2 | `POST /requests/{id}/return` | Manager |
| UC-B-28 | A3, L4, L11, Q3, C2 | `POST /requests/{id}/reject` | Manager |
| UC-B-29 | L6, L11, C2 (G2) | `GET /requests/{id}/cancellation`, `POST /requests/{id}/cancel` | Manager |
| UC-B-30 | L6, L11, C2, D4 (G1) | `GET`+`POST /requests/{id}/cancellation` | Manager |
| UC-B-31 | **A7 — reachable since Wave 3** (`1fac147`) | `POST /requests/{id}/reassign-approver` | Admin / Manager |
| UC-B-32 | D1, D2, D4, N1, Q5, A5 | `GET /{$}`, `GET /dashboard` | all four |
| UC-B-33 | A1, A2, L11, R6 (G8) | `POST /requests`, `POST /requests/{id}/edit`, `POST /requests/{id}/approve` | Admin / Manager |
| UC-B-34 | A5, R6, L11 | `POST /requests/{id}/{approve\|return\|reject\|cancel\|cancellation}` | Manager / Admin |
| UC-B-35 | A6 | none (405/404) | Manager / Admin |
| UC-B-36 | A3, A4, L11 | `POST /requests/{id}/return`, `/reject` | Manager |
| UC-B-37 | Q2, L6, L11 (G1) | `POST /requests/{id}/withdraw` | Requester |
| UC-B-38 | L4, Q1, Q3, L11 | `GET`+`POST /requests/{id}/edit` | Requester |
| UC-B-39 | Q3, L5, L11, C4 | `POST /requests/{id}/reraise` | Requester |
| UC-B-40 | T1, T2, T9, V1, V5, L11, R6 | `POST /requests`, `POST /requests/{id}/edit` | Requester (crafted) |
| UC-B-41 | T6, T7, R6, C2 | `POST /requests`, `POST /requests/{id}/edit` | Requester (crafted) |
| UC-B-42 | Q5, R3, R6 | `GET /requests/{id}` and every sub-path | Requester |

### 8.2 Coverage confirmation for this document’s four sections

Every ID in *Request form & types* (`T1–T12`), *Approval & manager* (`A1–A8`),
*Requester actions* (`Q1–Q6`) and *Lifecycle & states* (`L1–L11`) is claimed by at
least one use case above.

| Matrix ID | Requirement | Covered by |
|---|---|---|
| T1 | Treatment first, then type | UC-B-01, UC-B-02, UC-B-03, UC-B-05…09, UC-B-40 |
| T2 | Type decides required fields + payee | UC-B-02, UC-B-04, UC-B-05…10, UC-B-40 · § 1 |
| T3 | Bank details excluded from the form | UC-B-02 (BR-02.1), UC-B-11 (BR-11.3) |
| T4 | Mobile-first form | UC-B-02 (UX notes) |
| T5 | Always-captured fields incl. urgent | UC-B-02, UC-B-05…09, UC-B-14 · VA1–VA8 |
| T6 | Vendor-invoice fields | UC-B-05, UC-B-11, UC-B-41 · VT1 |
| T7 | Vendor-advance fields | UC-B-06, UC-B-11, UC-B-41 · VT3 |
| T8 | Reimbursement fields (payee = self) | UC-B-07 · VT5 |
| T9 | Employee advance: refundable→recoverable, non-refundable→budget | UC-B-03, UC-B-08, UC-B-09, UC-B-10, UC-B-40 · VT7, VT8 |
| T10 | Attachments optional now; admin toggle to require | UC-B-12, UC-B-13 · VG3 |
| T11 | Reimbursement / personal-advance payee = logged-in employee | UC-B-07, UC-B-08, UC-B-09 · VT5, VT7, VT8 |
| T12 | Inactive projects/heads hidden from new, shown on historical | UC-B-02 (BR-02.2), UC-B-05, UC-B-18 (DS5), UC-B-23 |
| A1 | Requester picks the manager | UC-B-02, UC-B-05, UC-B-33 · VA4 |
| A2 | Approve; may adjust the amount | UC-B-26, UC-B-33 |
| A3 | Reject with a required reason | UC-B-28, UC-B-36 |
| A4 | Return with required comments | UC-B-27, UC-B-36 |
| A5 | Managers see all; approve assigned | UC-B-16, UC-B-23, UC-B-25, UC-B-26, UC-B-34 |
| A6 | No bulk approval | UC-B-25, UC-B-35 |
| A7 | Admin reassign (reason + history) | UC-B-31 — `POST /requests/{id}/reassign-approver` since Wave 3 (`1fac147`); DV7/DS11 resolved |
| A8 | Edit-while-pending: history + re-notify + reset + reroute | UC-B-18, UC-B-19 |
| Q1 | Edit & resubmit a sent-back request | UC-B-18, UC-B-19, UC-B-38 |
| Q2 | Withdraw pending | UC-B-20, UC-B-22, UC-B-37 |
| Q3 | Re-raise rejected | UC-B-21, UC-B-28, UC-B-38, UC-B-39 |
| Q4 | See the payment outcome | UC-B-16 step 7 |
| Q5 | Requester sees only their own | UC-B-15, UC-B-16, UC-B-23, UC-B-24, UC-B-32, UC-B-42 |
| Q6 | Add clarification while on hold, no field change | UC-B-16.k, UC-B-17.a |
| L1 | *(“Draft (private)” — D1 removed drafts)* | UC-B-05 (BR-05.1): the absence is the requirement |
| L2 | Pending approval | UC-B-05…09, UC-B-15, UC-B-19, UC-B-25, UC-B-32 |
| L3 | Returned | UC-B-16, UC-B-19, UC-B-27 |
| L4 | Rejected — final, read-only | UC-B-21, UC-B-28, UC-B-38 |
| L5 | Withdrawn | UC-B-20, UC-B-39 |
| L6 | Approved | UC-B-22, UC-B-25, UC-B-26, UC-B-29, UC-B-30, UC-B-37 |
| L7 | On hold (only Accounts lifts) | UC-B-16.i, UC-B-22.b, UC-B-23 (BR-23.5), UC-B-29.c — the hold itself is `UC-C-*` |
| L8 | Processing (reserved) | referenced by UC-B-16, UC-B-20.e2 — owned by `UC-C-*` |
| L9 | Partial — manager review | referenced by UC-B-16.h, UC-B-17.b — owned by `UC-C-*` |
| L10 | Completed; drops out of the link list | referenced by UC-B-16 step 7 — owned by `UC-C-*` |
| L11 | Legal transition enforcement | UC-B-05, UC-B-19…22, UC-B-26…30, UC-B-33, UC-B-34, UC-B-36…40 |

Additional matrix IDs this document also exercises, without owning them:
**R3, R6** (UC-B-23, UC-B-42), **D1–D4** (UC-B-32), **D5** (UC-B-01),
**C2, C3, C4** (UC-B-05, UC-B-21, UC-B-24), **N1** (UC-B-15, UC-B-32),
**N3** (UC-B-26), **N6** (UC-B-14), **N7** (UC-B-12, UC-B-17),
**V1, V4, V5, V6, V8** (UC-B-08, UC-B-09, UC-B-10, UC-B-40),
**T10 / G10** (UC-B-13).





