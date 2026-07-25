# Payment Requests — UI/UX Design

**Date:** 2026-07-25
**Status:** Approved (design), mockups to follow
**Scope:** Whole-application UI/UX redesign of Fervid Budget, mobile-first, adding the payment-request module.

---

## 1. Context

Fervid Budget today is a Go + `html/template` + htmx + SQLite application with eleven screens
(`internal/app/templates.go`), two hardcoded Casbin roles (`internal/auth/auth.go:56-69`), and a
desktop-first variance grid as its home page. Money already flows one way: someone with
`payment:create` types a payment into a form and it lands against a head.

The workflow for the new module was settled across ten rounds of discussion with the client
(recorded in `docs/payment-requests-design.html`). This document covers only the **UI/UX**: what
screens exist, how they behave, how they adapt to a phone, and how the design absorbs the modules
the client already knows are coming.

**The job of this redesign:** every rupee that leaves the company starts as a request that a named
person approved, and anyone can see on their phone what is waiting on them.

---

## 2. Decisions taken in this round

| # | Question | Decision |
|---|---|---|
| 1 | Redesign scope | Whole app — existing 11 screens redrawn plus the new module |
| 2 | Visual language | Keep the existing Fervid identity, extend it. No new look |
| 3 | Home screen | Role-aware dashboard; variance grid becomes its own nav item |
| 4 | Free payment entry | Removed. Payments start from an approved request. Config flag can re-enable direct entry later, off by default |
| 5 | Committed column | **Not built now.** Layout slot reserved in the grid so it can be switched on without redesign |
| 6 | Vendor master | Build it, detailed (§8) |
| 7 | Approver selection | Admin sets a default manager per employee; employee may pick another eligible approver; **self-approval blocked** |
| 8 | Approval levels | Single level. UI reserves room for a second stage |
| 9 | Recoverables dashboard | Total outstanding, by category, by counterparty, ageing against expected return date, CSV export |
| 10 | Mobile | **Everything** is mobile-first, all four personas |
| 11 | Mockup format | Multi-file clickable prototype with persona switcher and device toggle |
| 12 | Future modules | Recoverable repayment tracking, purchase orders, vendor bills/GST, multi-level approval, budget forecasting, expense cards, **invoices, payments received, inventory** |

---

## 3. Information architecture

### 3.1 Mobile — bottom tab bar

Five slots, permission-filtered, with a role-specific primary action in the centre.

```
┌───────────────────────────────────────────────┐
│   Home     Requests     ⊕     Payments   More │
└───────────────────────────────────────────────┘
```

| Persona | Centre action |
|---|---|
| Employee | New request |
| Manager | Review approvals |
| Accounts | Record payment |
| Admin | New request |

"More" opens a full-screen sheet holding every permitted item, grouped exactly as the desktop
sidebar. Nothing is reachable on desktop that is unreachable on mobile.

### 3.2 Desktop — sidebar, regrouped into modules

| Group | Items |
|---|---|
| — | Home |
| Requests | My requests · Approvals · Accounts queue · Recoverables |
| Payments | Payments ledger |
| Budget | Variance grid · Budgets · Monthly plans |
| Reports | Monthly · Projects · Heads · Recoverables · Requests |
| Masters | Projects · Heads · Vendors |
| Admin | Users · Roles & permissions · Configuration · Audit · Backups |

### 3.3 Reserved module slots

Rendered in the mockup as disabled "Coming soon" entries so the shape is visible:
**Receivables** (Invoices, Payments received) · **Inventory** (Items, Stock) · **Procurement**
(Purchase orders).

Masters is the designed growth point — Customers and Items sit beside Vendors with no
restructuring. Every future module is a nav group plus its own list/detail pair reusing the same
components.

---

## 4. Status model

Twelve states. One pill style each, used identically on every screen.

| State | Pill | Waiting on |
|---|---|---|
| Awaiting approval | info | Manager |
| Returned for correction | warn | Employee |
| Rejected — final | danger, muted | nobody, read-only |
| Approved — awaiting payment | accent | Accounts |
| On hold | warn, striped | Employee, then Accounts |
| Cancellation requested | warn | Manager |
| Cancelled | neutral, muted | nobody |
| Processing — taken by *name* | accent, striped | assigned accountant |
| Partial — manager review | warn | Manager |
| Partial — under discussion | warn, dotted | Manager and Accounts |
| Completed | success | nobody |
| Completed — partial accepted | success, outlined | nobody |

**The "waiting on" line is the signature of this design.** Every status pill is followed by a plain
sentence naming the person or role who owes the next action — `Waiting on you`, `Waiting on Priya
Nair (Accounts)`, `Nothing pending`. It appears on cards, list rows, detail headers, and the
dashboard. It is what people scan on a phone, and it is what turns twelve abstract states into a
question anyone can answer.

Adding a state later means adding one row to the pill table and one "waiting on" sentence. Adding a
second approval stage means two new states and no structural change.

---

## 5. Screen inventory and coverage

43 screens. The mockup ships a coverage page mapping each client decision to its screen.

### Requests (14)

| Screen | Covers |
|---|---|
| `dashboard` | Unified role-aware home, permission-controlled work areas |
| `request-new-type` | Four request types |
| `request-new-form` | Adaptive form, financial treatment, recoverable categories, urgency, attachments |
| `request-duplicate-warning` | Duplicate check warns, never blocks |
| `request-submitted` | Auto-generated number `PR-2026-000123`, no drafts |
| `requests-list` | My requests, search, filters, status |
| `request-detail-employee` | Timeline + conversation, attachments, permitted actions |
| `request-edit` | Edit while awaiting approval, re-notify, reset reminder |
| `request-returned` | Returned for correction, correct and resubmit |
| `request-cancel` | Employee requests cancellation, payment frozen |
| `approvals-list` | Manager queue, no bulk approval |
| `request-detail-manager` | Approve / return / reject with reason, cancel with reason |
| `manager-cancellation-decision` | Manager accepts or declines cancellation |
| `request-on-hold` | Accounts holds, employee clarifies, only Accounts releases |

### Accounts (8)

| Screen | Covers |
|---|---|
| `accounts-queue` | Approved / on hold / processing / partial review tabs |
| `accounts-reservation-conflict` | "Just taken by *name*" |
| `payment-request-picker` | Searchable dropdown: number, requester, payee, project, head, amount |
| `payment-entry` | Reservation banner, one payment per request, cannot exceed approved |
| `payment-settlement-confirm` | Approved / paid / difference, settled vs partial, save only on confirm |
| `payment-partial-review` | Manager accepts and closes, or raises concern |
| `accounts-release-reassign` | Release or reassign with reason, confirmation that no payment was initiated |
| `accounts-stale-processing` | One-day stale reservation reminder |

### Recoverables (3)

`recoverables-dashboard` · `recoverables-list` · `recoverable-detail` — excluded from budget
actuals, category breakdown, counterparty, expected return date ageing, export.

### Admin and masters (7)

`admin-roles` (page-level + action-level + record scope own/assigned/all) · `admin-configuration`
(numbering, attachment mandatory toggle, reminder days, urgency, recoverable categories,
direct-payment flag) · `admin-notifications` (per-event enable, To/CC, include requester/manager/
accounts, subject and body templates) · `admin-users` (default manager per employee, multiple roles)
· `notifications` (in-app centre) · `vendors-list` · `vendor-detail`.

### Existing screens redrawn (9)

`login` · `variance-grid` · `payments-ledger` · `payment-detail` · `budgets` · `monthly-plans` ·
`reports` · `audit` · `backups`.

### Two colour rules established while building

**Urgent is never a tint.** The brand terracotta `#a83a1d` sits close in hue to the danger red
`#b42318`, so a soft-tinted "Urgent" pill reads like the "Approved" pill at a glance. Urgent is
therefore a solid red fill carrying an `!` glyph — distinguishable by shape as well as colour.

**Annotations are not a product colour.** Mockup notes use no fill at all: a dashed rule and a
monospace "Mockup note" label. An earlier purple treatment collided with the purple that means
*recoverable*, which is the one place purple carries meaning.

---

## 6. The three screens that carry the product

### 6.1 New request — one form that morphs

Type chooser first: four large tap targets, each with a one-line description of when to use it.
Then a single scrolling form whose sections appear and disappear by type and by financial
treatment.

- Amount uses a large numeric input with live Indian formatting (`₹1,00,000`) and a spelled-out
  confirmation line underneath. On a phone this is the field people get wrong; it gets the most room.
- Budget expense requires project and head. Recoverable hides them unless the category demands one
  (EMD/PBG require a project; ICD requires a counterparty).
- Attachments are optional by default; when the admin flag is on, submitting without one requires an
  exception reason in the same control.
- Duplicate check fires on blur of amount + payee and renders an inline amber card listing the
  matching requests. It never blocks.
- Sticky bottom bar holds the single primary action.

### 6.2 Request detail — one screen, four audiences

Identical layout for everyone; **the action bar changes by permission**, not the page.

```
┌────────────────────────────────────────────┐
│ PR-2026-000128        ₹1,00,000            │
│ ● Approved — awaiting payment              │
│ Waiting on Accounts                        │
├────────────────────────────────────────────┤
│ Details · attachments                      │
├────────────────────────────────────────────┤
│ Timeline + conversation (one stream)       │
│  submitted · edited · approved · held ·    │
│  comment · assigned · paid · settled       │
└────────────────────────────────────────────┘
```

Timeline and conversation are **one chronological stream**, not two tabs. System events and human
comments interleave, because the argument about a payment only makes sense next to the events that
caused it. Everyone permitted to view the request sees the whole stream; there are no private
comments.

### 6.3 Payment entry and settlement

Reservation banner pinned at the top (`Reserved by you at 14:02 · Release`). The approved amount is
shown read-only directly above the paid-amount field, and the difference computes live.

*Payment settled* opens a confirmation sheet:

```
Approved    ₹1,00,000
Paid        ₹98,000
Difference  ₹2,000 lower   ⚠

○ Fully settled — deductions handled outside this system   → Completed
○ Partial payment — balance still due                      → Manager review

[ Cancel ]                     [ Confirm and save payment ]
```

Nothing is written until Confirm. This sheet is the only place the Completed-vs-Partial decision is
made, and the system performs no tax arithmetic.

---

## 7. Mobile patterns for dense screens

| Screen | Mobile pattern |
|---|---|
| Variance grid | Project accordion cards; each head shows budget/actual/remaining stacked with a used-% bar. Desktop keeps the sticky-column matrix. Committed column slot reserved, hidden |
| Lists (requests, payments, vendors, audit) | Table markup with `data-label` cells that CSS restacks into cards; filters move into a filter sheet |
| Budgets editor | One head per card, single amount input, sticky Save bar |
| Permission matrix | Expandable row per page instead of a grid |
| Reports | Summary tiles first, table scrolls horizontally in its own container |

One markup per screen serves both devices. Nothing is desktop-only.

---

## 8. Vendor master

**Identity** — name, display/short name, type (company / proprietor / individual), status, category tags
**Statutory** — GSTIN, PAN, MSME/Udyam number, TDS section and default rate (recorded only, never calculated)
**Contact** — contact person, phone, email, billing address, city, state and state code
**Payment**, gated behind `vendor_bank:read` — account name, account number, IFSC, bank and branch, UPI ID, default payment mode, payment terms in days
**Operations** — notes, created/updated audit, tabs listing linked requests and payments

Request forms use a combobox with inline "＋ Add new vendor" for users holding create permission.
This master is the controlled record that keeps bank details off the employee request form.

---

## 9. Design system additions

Appended to `fervid-ds.css` as a new section; no existing class is modified.

Status pill set · waiting-on line · timeline and conversation thread · request card · segmented work
queue tabs · sticky mobile action bar · bottom tab bar and centre action · confirmation sheet ·
searchable combobox · money input with Indian formatting · adaptive fieldset · attachment uploader
with exception reason · reservation banner · notification bell and list · permission matrix · filter
sheet · empty, loading and error states.

Tokens are the current production values (`fervid-ds.css:1221-1242`): paper `#f3f0eb`, surface
`#fbfaf7`, ink `#282624`, brand `#a83a1d`, ok `#0f633d`, bad `#b42318`, sidebar `#191817`, Inter for
UI, Georgia for display, SFMono for figures. The mockups add only the soft status tints the new
components need.

---

## 10. Mockup deliverable

```
mockups/
  index.html      screen directory, flow map, coverage checklist
  mockup.css      production tokens plus new components
  mockup.js       shell, persona switcher, device toggle, tabs, sheets, modals
  screens/        43 files, one per screen
```

Every screen carries a prototype bar with a **persona switcher** (Employee / Manager / Accounts /
Admin) and a **device toggle** (Mobile 390 px / Desktop). Switching persona re-renders the same
screen with that role's navigation and action bar. State travels in the query string so the files
work when opened directly from disk. Links between screens follow the real flow. Data is realistic
Indian business data throughout.

---

## 11. Out of scope

Repayment tracking of recoverables, forfeiture or conversion of EMD/PBG, refunds and reversals,
editing a recorded payment, tax computation, multi-level approval, bulk approval, copy-previous-
request, private internal comments, and drafts. Each is named here so a later reader knows it was
decided rather than forgotten.
