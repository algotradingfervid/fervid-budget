# Design-System Adoption — Revision Spec

**Date:** 2026-07-25
**Status:** Supersedes the phase map in `2026-07-25-payment-requests-overview.md` §2. All other sections of the overview remain authoritative unless amended here.
**Why this exists:** the five phase plans were written before the UI/UX design system existed. They are excellent on the backend and audited to 92/92 requirement coverage, but every UI task targets markup that the approved design replaces. A four-way independent analysis of the plans against the approved mockups also surfaced functional gaps that have nothing to do with styling.

**Authoritative design sources:**
- `docs/superpowers/specs/2026-07-25-payment-requests-uiux-design.md` — the approved UI/UX design
- `mockups/` — 43 approved screens, `mockup.css`, `mockup.js`
- Claude Design project `5a5f30b6-cbc2-4c21-96f6-eb3fafb80d8b` — *Mobile-first payment requests prototype*, confirmed to use the identical token set (`#a83a1d`, `#7f2b16`, `#f3f0eb`, `#fbfaf7`, `#282624`, `#0f633d`, `#b42318`, `#9a5b00`, `#245b8f`, `#191817`). It is a second rendering of the same system, not a competing one.

---

## 1. Functional gaps found (not styling)

These are requirements from the approved design that **no plan task builds**. Each is now assigned an owner phase.

| # | Gap | Approved source | Plan state | Owner |
|---|---|---|---|---|
| G1 | Post-approval cancellation: employee requests → payment freezes → manager accepts (→ Cancelled) or declines (→ Approved) | Round 9–10 flow diagram | absent; only pre-approval `withdrawn` exists | P2 |
| G2 | Manager cancels an approved request with a reason | flow diagram | absent | P2 |
| G3 | Statuses `cancellation_requested`, `cancelled` | flow diagram | absent from the status enum | P2 |
| G4 | **No drafts.** A request exists only once submitted, and takes its number then | Round 10, verbatim: "There are no drafts" | plan implements `draft` status, a "Save draft" button, and `Reraise`→draft | P2 |
| G5 | Vendor master + combobox + permission-gated bank details | UI/UX spec §8 | free-text `vendor_payee` only | **P1V (new)** |
| G6 | Duplicate warning on submit — warns, never blocks | Round 10 | absent | P2 |
| G7 | Urgency reason required when urgent is ticked | user answer; UI/UX §6.1 | `urgent` boolean only | P2 |
| G8 | Self-approval blocked; approver list excludes the requester | user: "self approval not acceptable" | `ManagerID > 0` only | P2 |
| G9 | Default approver per employee | UI/UX §2 Q7 | absent | P2 |
| G10 | Attachment exception reason when the require-attachments flag is on | UI/UX §6.1 | toggle exists, exception reason does not | P2 |
| G11 | Reservation **reassign** (appears in three mockups) | UI/UX §5 | no store method, route, or permission verb | P3 |
| G12 | Release requires a **reason** | `accounts-release-reassign.html` | `ReleaseRequest(…, confirmed, authorized)` has no reason | P3 |
| G13 | Paid amount may never exceed the approved amount | UI/UX §6.3; `payment-entry.html` | unenforced in `RecordPaymentForRequest` | P3 |
| G14 | Distinct terminal state "Completed — partial accepted" | `.pill.completed-partial` | store writes plain `completed` | P3 |
| G15 | Reservation-conflict is a **screen**, not an error page | `accounts-reservation-conflict.html` | `respondError(409, …)` | P3 |
| G16 | Settlement confirmation persists **nothing** until confirmed | UI/UX §6.3 | one click POSTs and writes | P3 |
| G17 | Recoverable **detail** screen | `recoverable-detail.html` | no route at all | P4 |
| G18 | Recoverables ageing, 4 metrics, by-category and by-counterparty rollups | `recoverables-dashboard.html` | 2 metrics, no ageing, no rollups | P4 |
| G19 | In-app notification centre + unread bell badge | `notifications.html` | explicitly out of scope in the P5 spec | **P5** |
| G20 | 12 notification events | `admin-notifications.html` | 6 seeded | P5 |
| G21 | One consolidated Configuration screen | `admin-configuration.html` | settings scattered over 3 phases + 2 half-screens | P2 owns shell |
| G22 | Redraw of the 9 pre-existing screens on the new system | user: "update this entire application" | absent | **P6 (new)** |

---

## 2. Decisions taken to resolve conflicts

### D1 — Drafts are removed
`CreateRequest` and `SubmitRequest` collapse into one atomic operation: the request is created **already `pending`**, and `NextRequestNumber` is called in the same transaction. The `draft` status is dropped from the enum and from `canTransition`. `ReraiseRequest` creates a new `pending` request copying the rejected one's fields. Coverage row **L1 changes meaning** from "Draft (private)" to "No draft state exists" and becomes a proof-of-absence test.

### D2 — Roles matrix: presentation grouping over the canonical vocabulary
The mockup shows 9 page rows × 7 fixed action columns. The canonical vocabulary is 17+ resources with ragged action lists. Both survive:

- **Enforcement stays canonical.** `role_permissions` remains one row per `(resource, action)`. Nothing about the permission engine changes.
- **The admin screen renders a presentation map.** A new `internal/app/permmap.go` defines 9 groups × 7 columns; each cell maps to a set of canonical grants.
- Toggling a cell grants or revokes **all** canonical actions behind it. Each row carries an **"Advanced"** disclosure listing the individual canonical actions for fine-grained control, preserving requirement R2.
- A cell whose group has no canonical action for that column renders `—`, not a checkbox.

| Row | Canonical resources |
|---|---|
| Payment requests | `request`, `approval` |
| Payments | `payment` (create/edit/void/settle/mark_partial) |
| Reservations | `reservation` (new: reserve/release/reassign) |
| Recoverables | `recoverable_report`, `recoverable_category` |
| Vendors | `vendor` |
| Vendor bank details | `vendor_bank` |
| Budgets & variance grid | `budget`, `grid`, `month` |
| Reports | `report` |
| Administration | `user`, `role`, `notification`, `config`, `audit`, `backup` |

Columns: View · Create · Edit · Approve · Process · Cancel · Export.

New resources added to the canonical vocabulary: `vendor`{view,create,edit}, `vendor_bank`{view,edit}, `reservation`{reserve,release,reassign}, `config`{view,edit}.

### D3 — Vendor master becomes its own phase, before the request workflow
Sequenced as **P1V**, after P1 (it needs `vendor_bank` permission) and before P2 (so `payment_requests` is created once with `vendor_id`, never migrated twice). `vendor_payee TEXT` is retained alongside `vendor_id` as the payee snapshot for reimbursement and employee-advance requests, where there is no vendor row.

### D4 — Responsiveness is media queries, not a device attribute
`mockup.css` keys 57 rules on `html[data-device="mobile"|"desktop"]`, which is a prototype toggle. Production uses `@media (max-width: 860px)`. `.m-only` / `.d-only` become media-query-driven. Rules that cannot be expressed as a media query are listed in the Phase 0 plan and resolved individually.

### D5 — `.badge` → `.pill` is a single global rename
Done once in Phase 0 across every existing template, not per-phase.

### D6 — Configuration is one screen, owned by Phase 2
`GET/POST /configuration`, permission `config`{view,edit}, one `<fieldset>` per section. Phase 2 lands the shell and the generic `app_settings`-backed save handler; Phases 3, 4 and 5 each append their own fieldset. This removes the planned standalone `/recoverable-categories` page and the SMTP half of `/notifications`.

### D7 — Notification URL split
`/notifications` = the **user's** in-app centre (G19). `/admin/notifications` = the per-event email rules screen.

### D8 — Settlement uses a preview endpoint that writes nothing
`POST /requests/{id}/settlement-preview` is pure: it re-checks the reservation, computes approved / paid / difference, and renders the confirmation sheet fragment. Delivered by htmx into the live form so the file input is uploaded exactly once on the final POST. The same template renders as a full page for the no-JS path. Only `POST /payments` writes.

### D9 — Phase 1 owns `users.default_approver_id`
*Added 2026-07-25, after the per-phase amendments surfaced the clash.* §1 assigns G9 to Phase 2, but Phase 1 ships first and its `admin-users.html` renders the default-approver field — a field with no column behind it. The column, `User.DefaultApproverID int64` (0 = none) and `SetUserDefaultApprover(ctx, actor, userID, approverID) error` therefore all land in **Phase 1 migration v1**. Phase 2 keeps a `columnExists`-guarded `ALTER` so its plan still runs standalone, and consumes Phase 1's accessors instead of declaring a second writer over the same column.

### D10 — The canonical vocabulary is 66 pairs, not 64
*Added 2026-07-25.* D2's new-resource list (`vendor`, `vendor_bank`, `reservation`, `config`) omits the two verbs Phase 2's cancellation flow registers its routes behind. `request`{cancel} and `approval`{cancel} join `resourceActions`, taking the canonical vocabulary to **21 resources / 66 pairs**. Phase 1's exact-list and total assertions are the enforcement point; no later phase may invent a verb outside that table.

---

## 3. Revised phase map

| Phase | Deliverable | Depends on |
|---|---|---|
| **0 — Design system & app shell** | Port the mockup component layer into `fervid-ds.css`; media-query responsiveness; `badge`→`pill`; permission-driven nav model (groups, badges, coming-soon); mobile top bar, bottom tab bar, More sheet; shared partials (status pill, waiting-on line, thread, request card, sheet/overlay, money field, combobox, filter sheet); the JS behaviours. | — |
| **1 — RBAC foundation** | Migration runner; role tables; seeded roles; DB-driven enforcement with data scope; roles matrix (D2) and multi-role users on the new system. | 0 |
| **1V — Vendor master** | `vendors` table; CRUD; search endpoint for the combobox; `vendor_bank` gating; list + detail screens. | 0, 1 |
| **2 — Request workflow** | Requests (no drafts, D1); adaptive form; cancellation flow (G1–G3); duplicate warning; urgency reason; self-approval block; default approver; queues; dashboard; **Configuration screen shell**. | 0, 1, 1V |
| **3 — Linking & settlement** | Reservation + reassign + release-with-reason; conflict screen; settlement preview and confirmation; partial review with the distinct accepted state; hold; payment detail. | 0, 1, 2 |
| **4 — Recoverables** | Categories as a Configuration fieldset; validation; budget exclusion; dashboard with ageing, metrics and rollups; recoverables list; **recoverable detail**. | 0, 1, 2, 3 |
| **5 — Notifications** | SMTP; 12 events; templates; reminders with injected clock; **in-app notification centre + unread bell**; admin rules screen. | 0, 1, 2, 3 |
| **6 — Existing screens redrawn** | Variance grid (mobile accordion), payments ledger, budgets, monthly plans, reports, projects, heads, audit, backups, login. | 0, and each screen's owning phase |

---

## 4. Per-phase amendments

Applied as targeted edits to the existing plan files; the backend task bodies are kept as written unless listed here.

### Phase 1
- Tasks 1–9 unchanged; they are pure backend and may run **in parallel with Phase 0**.
- Task 3 `resourceActions` gains `vendor`, `vendor_bank`, `reservation`, `config` (D2).
- Task 10 rebuilt against `admin-roles.html`: `.segmented` role switcher, `.card`/`.card-head`/`.card-body`, desktop `.perm-table` + mobile `.perm-acc`, `.perm-scope` 3-state pill group with a `None` state, sticky `.action-bar`. Consumes `internal/app/permmap.go`.
- Task 11 rebuilt against `admin-users.html`: `table.t-cards` with `data-label`, role chips as `.pill.neutral.no-dot`, editing in an `.overlay > .sheet`. Adds the default-approver field (G9).
- Task 12 reduced to *populating* the Phase 0 nav model from `.Perms`; it no longer invents the shell.

### Phase 1V (new plan file)
`vendors` migration (v2, pushing later versions up by one), store CRUD + search, `vendor_bank` gating, `vendors-list.html` and `vendor-detail.html`.

### Phase 2
- Migration renumbered v3. Schema: `vendor_id` FK; drop `draft` from the status default; add `cancellation_requested`/`cancelled`; add `urgency_reason`, `attachment_exception_reason`, `short_title`, `invoice_no`, `invoice_date`, `expense_date`, `advance_reason`, `cancel_reason`.
- Task 3 validation gains: per-type conditional rules, urgency reason, self-approval rejection, EMD/PBG project, ICD counterparty.
- Tasks 4+5 merge into one atomic create-and-submit (D1).
- New tasks: cancellation flow (G1–G3), duplicate check endpoint (G6), default approver (G9), Configuration screen shell (D6).
- Task 13 split into per-screen tasks matching `request-new-type`, `request-new-form` (+ htmx `requestFormFields` partial), `request-submitted`, `requests-list`, `approvals-list`, `request-detail`, `request-edit`, `request-returned`.
- Task 14 dashboard rebuilt against `dashboard.html` with `.work-areas`.
- Task 15 Playwright selectors updated off `.badge` / `getByLabel('Type')`.

### Phase 3
- Migration renumbered v4.
- `ReleaseRequest` signature gains `reason string` (G12). New `ReassignRequest` for reservations (G11).
- `RecordPaymentForRequest` enforces paid ≤ approved (G13) and writes `completed_partial` on accept (G14).
- `LinkablePaymentRequests` gains a status filter, counts, and an "unavailable/taken" result set for the queue tabs and the picker's `.co.is-taken` rows.
- New tasks: settlement preview endpoint (D8), reservation-conflict screen (G15), release/reassign screen, payment-detail screen, accounts queue with metrics and `.segmented` tabs.

### Phase 4
- Migration renumbered v5. Task 7 retargeted from a standalone page to a Configuration fieldset (D6).
- `RecoverableRow` gains ageing; `RecoverableReport` gains ordering, an ageing filter, and GROUP BY rollups for the by-category and by-counterparty tables.
- New task: recoverable detail screen (G17).

### Phase 5
- Migration renumbered v6. Seed 12 events (G20). Template vocabulary switches to `{{number}} {{amount}} {{payee}} {{approver}} {{project}} {{head}} {{needed_by}} {{link}}`.
- Admin screen moves to `/admin/notifications` (D7) and adopts the `.t-cards` + `.overlay > .sheet` template editor.
- Reminder thresholds move from hardcoded constants to `app_settings` keys surfaced in Configuration.
- New task set: `notifications` table, unread counter, mark-read route, shell bell badge, and the notification centre screen (G19).

### Phase 6 (new plan file)
One task per screen, each a template rewrite plus a rendering test: login, variance grid, payments ledger, budgets, monthly plans, reports, projects, heads, audit, backups.

---

## 5. Coverage matrix changes

`2026-07-25-payment-requests-coverage.md` is extended, not replaced:
- **L1 inverts** — "Draft (private)" becomes "No draft state exists" (proof-of-absence).
- **New IDs** for G1–G22, each bound to a named test in its owning phase.
- **New section "Design system"** with one row per shared component asserting it renders on at least one screen, plus a row asserting no template references `.badge`.

## 6. Definition of done

Unchanged from the overview §10, plus: every screen in `mockups/screens/` has a corresponding rendered route, and a Playwright test walks the employee → manager → accounts journey on a 390 px viewport.
