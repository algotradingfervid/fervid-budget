# UC-C — Payment linking & settlement, recoverables, notifications

**Scope.** The third of three use-case documents. It owns the coverage-matrix
sections **Linking, processing & settlement** (`S1`–`S15`), **Recoverables**
(`V1`–`V8`), **Notifications, reminders & conversation** (`N1`–`N8`) and **Scope
boundaries** (`X1`–`X6`), plus the lifecycle rows those flows own (`L7`–`L11`)
and the two requester rows they close (`Q4`, `Q6`). Siblings own `UC-A-*`
(platform, RBAC, administration) and `UC-B-*` (requests and approvals); this
document references those IDs but never redefines them.

**Source of truth.** Every load-bearing claim below is cited to `file:line` in
the code as committed on `main`. Where the code contradicts a spec or a document,
the code is recorded as what ships and the divergence is listed in
[§4 Divergences](#4-divergences-found-in-the-code) rather than "corrected".

**Audience.** A test engineer writing Playwright and Go tests from these steps.
Control text, field labels, selectors and HTTP status codes are given verbatim
because they are the test's contract, not decoration.

---

## Contents

1. [Route and permission reference](#1-route-and-permission-reference)
2. [Actors and standing facts](#2-actors-and-standing-facts)
3. [Use cases](#3-use-cases)
   - [Linking and settlement — UC-C-01 … UC-C-29](#31-linking-and-settlement)
   - [Recoverables — UC-C-30 … UC-C-37](#32-recoverables)
   - [Notifications — UC-C-38 … UC-C-45](#33-notifications-reminders-and-conversation)
4. [Divergences found in the code](#4-divergences-found-in-the-code)
5. [Traceability](#5-traceability)

---

## 1. Route and permission reference

Every route this document exercises, with the gate `routes()` actually registers.
`RT-*` ids are stable and cited by the use cases.

| RT | Method + path | Handler | Gate (middleware) | Handler-level check | Cited |
|---|---|---|---|---|---|
| RT-01 | `GET /accounts-queue` | `accountsQueue` | `payment:process` | tab must be one of five | `app.go:476`, `linking.go:141` |
| RT-02 | `GET /payments/new` | `paymentForm` | `payment:create` | `?request=` → must be held by caller | `app.go:384`, `app.go:655` |
| RT-03 | `GET /payments/new/options` | `paymentPickerOptions` | `payment:create` | — | `app.go:385`, `linking.go:266` |
| RT-04 | `POST /requests/{id}/record-payment` | `requestRecordPayment` | `reservation:reserve` | atomic reserve; 409 on loss | `app.go:451`, `linking.go:82` |
| RT-05 | `POST /requests/{id}/settlement-preview` | `settlementPreview` | `payment:settle` | must be held by caller; **writes nothing** | `app.go:454`, `linking.go:280` |
| RT-06 | `POST /payments` | `paymentCreate` | `payment:create` **+ `payment:settle`** | `request_id` required; `payment:mark_partial` when `settlement=partial`; store re-checks reservation | `app.go:446-447`, `app.go:822-837` |
| RT-07 | `GET /payments/{id}` | `paymentDetail` | `payment:view` | linked → also request row-scope | `app.go:388`, `app.go:759` |
| RT-08 | `GET /payments` | `payments` | `payment:view` | — | `app.go:386`, `app.go:669` |
| RT-09 | `GET /payments/{id}/edit` | `paymentEditForm` | `payment:edit` | linked → 303 to `/payments/{id}` | `app.go:389`, `app.go:803` |
| RT-10 | `POST /payments/{id}/edit` | `paymentEdit` | `payment:edit` | store refuses a linked payment | `app.go:390`, `store.go:609` |
| RT-11 | `POST /payments/{id}/void` | `paymentVoid` | `payment:void` | store refuses a linked payment | `app.go:391`, `store.go:649` |
| RT-12 | `GET /requests/{id}/reservation` | `reservationForm` | **session only** | holder+release, or reassign | `app.go:464`, `linking.go:521` |
| RT-13 | `POST /requests/{id}/release` | `requestRelease` | `reservation:release` | reason + confirm; assignee or authorized | `app.go:465`, `linking.go:623` |
| RT-14 | `POST /requests/{id}/reassign` | `requestReassign` | `reservation:reassign` | target + confirm + target can work queue | `app.go:466`, `linking.go:642` |
| RT-15 | `GET /requests/{id}/reservation/stale` | `requestStale` | `payment:process` | 409 off `processing` | `app.go:470`, `linking.go:730` |
| RT-16 | `POST /requests/{id}/hold` | `requestHold` | `payment:hold` | reason required; approved + not held | `app.go:474`, `linking.go:694` |
| RT-17 | `POST /requests/{id}/unhold` | `requestUnhold` | `payment:hold` | must be on hold | `app.go:475`, `linking.go:710` |
| RT-18 | `GET /requests/{id}/partial-review` | `requestPartialReview` | `request:view` | 303 off `partial_review`; 404 with no payment | `app.go:482`, `linking.go:377` |
| RT-19 | `POST /requests/{id}/accept-partial` | `requestAcceptPartial` | `approval:accept_partial` | **must be the request's own manager** | `app.go:483`, `store.go:980` |
| RT-20 | `POST /requests/{id}/raise-concern` | `requestRaiseConcern` | `approval:accept_partial` | own manager; comment required | `app.go:484`, `store.go:1021` |
| RT-21 | `POST /requests/{id}/comment` | `requestComment` | `request:comment` | any viewable status, incl. on hold | `app.go:491`, `requests.go:768` |
| RT-22 | `GET /recoverables` | `recoverablesDashboard` | `recoverable_report:view` | **summary aggregates scoped to caller, fixed F-G-016/F-E-03** | `app.go:403`, `recoverables.go:117-120` |
| RT-23 | `GET /recoverables/list` | `recoverablesList` | `recoverable_report:view` | filter allow-lists; **rows scoped to caller's request scope, fixed F-G-016** | `app.go:404`, `recoverables.go:48`, `recoverables.go:117-120` |
| RT-24 | `GET /recoverables/list.csv` | `exportRecoverable` | `recoverable_report:export` | audits `export`; **same scope as RT-23** | `app.go:405`, `recoverables.go:97` |
| RT-25 | `GET /recoverables/{id}` | `recoverableDetail` | `recoverable_report:view` | 404 unless `treatment='recoverable'`; **also 404 outside the caller's request scope, fixed F-G-016/F-G-002** | `app.go:406`, `recoverables.go:138`, `recoverables.go:195-198` |
| RT-26 | `POST /configuration/recoverable-categories` | `recoverableCategorySave` | `recoverable_category:edit` | `requires` allow-list | `app.go:520`, `configuration.go:113` |
| RT-27 | `GET /configuration` | `configuration` | `config:view` | — | `app.go:516` |
| RT-28 | `POST /configuration` | `configurationSave` | `config:edit` | only registered keys written | `app.go:517`, `configuration.go:142` |
| RT-29 | `GET /notifications` | `notificationCentre` | **session only** | rows scoped to caller by SQL | `app.go:411`, `inapp.go:33` |
| RT-30 | `POST /notifications/read` | `notificationsMarkAllRead` | **session only** | scoped by `user_id` | `app.go:412`, `inapp.go:54` |
| RT-31 | `GET /notifications/{id}/open` | `notificationOpen` | **session only** | 404 if not yours; **no CSRF** | `app.go:413`, `inapp.go:73` |
| RT-32 | `GET /admin/notifications` | `adminNotifications` | `notification:view` | — | `app.go:417` |
| RT-33 | `POST /admin/notifications/smtp` | `adminNotificationsSMTP` | `notification:edit` | no password field exists | `app.go:418`, `notifications.go:75` |
| RT-34 | `POST /admin/notifications/events/{event}` | `adminNotificationEventSave` | `notification:edit` | template token validation | `app.go:419`, `notifications.go:96` |
| RT-35 | `POST /admin/notifications/test` | `adminNotificationsTest` | `notification:edit` | 502 on transport error | `app.go:420`, `notifications.go:125` |
| RT-36 | `GET /grid` | `grid` | session only | recoverables excluded from actuals | `app.go:377`, `store.go:1388` |
| RT-37 | `GET /reports/monthly` | `report` | `report:view` | built on `Grid`, so same exclusion | `app.go:395`, `store.go:1493` |

---

## 2. Actors and standing facts

| AF | Fact | Cited |
|---|---|---|
| AF-01 | The permission vocabulary is 21 resources / 66 pairs. Linking uses `payment:{view,create,edit,void,process,settle,mark_partial,hold}`, `reservation:{reserve,release,reassign}`, `approval:accept_partial`, `recoverable_report:{view,export}`, `recoverable_category:{view,create,edit,delete}`, `notification:{view,edit}`. | `permissions.go:150-172` |
| AF-02 | The seeded **Accounts** role holds `payment:process/settle/mark_partial/hold`, `reservation:reserve`, `reservation:release`, `recoverable_report:view/export` — and **not** `reservation:reassign`. | `migrations.go:380-395` |
| AF-03 | The seeded **Manager** role holds `approval:accept_partial` and `request:view` scope `all`, and **no** `payment:*`, **no** `reservation:*`, **no** `recoverable_report:view`. A manager therefore cannot open `/accounts-queue` or `/recoverables`. | `migrations.go:365-378` |
| AF-04 | **Admin** holds every pair, including `reservation:reassign` — but is not automatically anybody's manager, which is what `UC-C-12` turns on. | `migrations.go:396-401`, `store.go:980` |
| AF-05 | Money is `int64` paise; `money.FormatPaise` already emits `₹`. Money inputs use `amountValue` inside `.money-field`, never `money`. | `money.go:47`, `templates.go:403` |
| AF-06 | An unrouted **POST** answers **405**, not 404, because `routes()` registers a catch-all `GET /`. Proof-of-absence tests must accept either. | `app.go:376`, `app_integration_test.go:1537-1543` |
| AF-07 | `on_hold=1` implies `status='approved'`. `HoldRequest`'s `WHERE` demands it and `activeHold` re-asks the question for display. | `store.go:1047`, `linking.go:808` |
| AF-08 | `store.StaleReservation` is a **24-hour** constant. The screens print elapsed hours (`26 h`) so the label reads like "26 hours"; the threshold is 24. The scheduler uses a *separate*, admin-configurable `reminder_stale_days` (default 1). | `models.go:410`, `reminders.go:46`, `linking.go:783-800` |
| AF-09 | `store.Request.Vendor` is the display payee (`COALESCE(NULLIF(v.name,''),r.vendor_payee)`). Every Phase-3 screen reads `.Vendor`, and the entry form copies it into the payment's hidden `vendor_payee`. | `requests.go:257`, `linking.go:260`, `templates.go:390` |
| AF-10 | The settlement sheet is swapped into `#settle-mount` by htmx, so **the URL does not change**; its "Go back" is an `<a>` and its close control `.sh-close` is also an `<a>`. | `templates.go:439`, `templates.go:470`, `templates.go:516` |
| AF-11 | The payment-entry file input is `hidden` with **no accessible name** — reach it as `input[name="attachment"]`, never `getByLabel('Attachment')`. | `templates.go:429`, `fixtures.ts:297-300` |
| AF-12 | "Take for processing" is a **submit button inside a POST form**, not a link, because reserving mutates. | `templates.go:722`, `fixtures.ts:262-265` |
| AF-13 | `.metric-foot` **now has a rule** (`fervid-ds.css:367`), added by Phase 6 commit `a80e18f`, together with `.metric.warn`, `.metric.good` and `.btn.approve`. `PROGRESS.md`'s "Known gaps" entry is stale. | `fervid-ds.css:364-405`, `PROGRESS.md:368-375` |
| AF-14 | The unread bell lives in `.m-topbar .m-icon .dot`, which is `display:none` above 860 px. At audit time there was no `/notifications` entry in `navSpec`, so **on desktop the notification centre had no link and no unread indicator** — SD-08. **Fixed in Wave 4 (F-F-05):** `navSpec` now carries `{Key: "notifications", Label: "Notifications", Href: "/notifications", Icon: "✉", Badge: "notifications"}` and the shell fills that badge from `UnreadNotificationCount` (`nav.go:61`, `nav.go:162-169`). The bell's mobile-only CSS is unchanged; the desktop entry is a second door, not a change to the first. See REPAIR-LOG.md Wave 4. | `templates.go:74`, `fervid-ds.css:3881-3884`, `fervid-ds.css:4398`, `nav.go:57-61`, `nav.go:162-169` |
| AF-15 | `initMoneyFields` stamps `data-money-bound="1"` on a money input once it owns it. Filling before that lands in a different field. | `fervid-app.js:536`, `regression-issues.spec.ts:68-71` |
| AF-16 | The payment-detail mockup's `Bank` row (`HDFC ····4471 · IFSC …`) is **not built** — the `dl.dl` on `payment_detail` has no such row. | `templates.go:277-287`, `PROGRESS.md:376-379` |

Actors used throughout: **Accountant** (Accounts role), **Second accountant**
(same role, different session), **Manager** (the request's own `manager_id`),
**Requester**, **Administrator**, **Scheduler** (the in-process ticker,
`main.go:99-102`).

---

## 3. Use cases

### 3.1 Linking and settlement

#### UC-C-01 — Open the Accounts payment queue

| | |
|---|---|
| **Goal** | "Show me every approved request I am allowed to pay, and how much is waiting." |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar "Accounts queue", or the dashboard's Accounts work area |
| **Route(s)** | `GET /accounts-queue` (RT-01) |
| **Permission gate** | `payment:process`; rows narrowed by `auth.Scope(user,"request")` |
| **Coverage IDs** | S3, D4 |
| **Priority** | critical |

**Preconditions**

1. The caller holds `payment:process` (AF-02).
2. At least one request exists in `approved`, `processing`, on hold, `partial_review` or a completed state within the caller's request scope.
3. `unbuiltPrefixes` is empty, so the nav entry resolves (`nav.go:250`).

**Postconditions (success)** — a `200` page titled `Payment queue`; no rows written; no audit entry (this is a read).

**Postconditions (failure)** — nothing written; a caller without `payment:process` gets `403` from `RequirePermission` and never reaches `accountsQueue`.

**Main success scenario**

1. Accountant clicks **Accounts queue** in the sidebar (`nav.go:63`).
2. System serves `/accounts-queue` with no `tab`, so `accountsQueue` defaults to `queueTabs[0].Key == "approved"` (`linking.go:143-145`).
3. System runs one aggregate count pass over the caller's whole scope, independent of the tab and the search (`store.go:1112-1127`).
4. System renders `<h1>Payment queue</h1>` and a `.sub` reading `{n} approved and unclaimed · {₹} · one request, one payment` (`templates.go:669-670`).
5. System renders a `.metric-strip` of exactly **four** `.metric` tiles, in order: `Approved, unclaimed`, `Reserved by you`, `Reserved by others`, `On hold`. Each has `.metric-label`, `.metric-value` and `.metric-foot` (`templates.go:677-682`).
6. System renders a `.segmented` strip of exactly **five** anchors: `Approved`, `Processing`, `On hold`, `Partial review`, `Paid`, each with a `<span class="n">` count and `href="/accounts-queue?tab={key}"` (`templates.go:684-691`).
7. System renders one `table.t-cards` with the head `Request · Payee · Project / head · Amount · Needed by · Status · Action` (`templates.go:712`).
8. Accountant reads a takeable row: `.t-lead` links `/requests/{id}`, `.t-sub` reads `{requester} · approved {date}`, and the action cell carries the **Take for processing** submit button (`templates.go:716-722`).

**Alternate flows**

- **01.a (from step 2) — an explicit tab.** `?tab=processing|hold|partial_review|paid` selects the matching store filter (`linking.go:158-164`, `store.go:1148-1166`). `hold` demands `on_hold=1 AND status='approved'` (`store.go:1161`) — belt and braces for AF-07.
- **01.b (from step 5) — the `Reserved by you` foot.** With stale reservations present it reads `{n} open over a day`; otherwise `All recent` (`templates.go:679`).
- **01.c (from step 7) — the `Approved` tab shows taken rows too.** The store selects `status IN ('approved','processing')` for that tab and splits the result into `Available` and `Unavailable`, so a request somebody else holds is rendered rather than hidden (`store.go:1152-1153`, `models.go:436-443`).
- **01.d (from step 8) — no `reservation:reserve`.** The action cell degrades to `<a class="btn small outline" href="/requests/{id}">View</a>` (`templates.go:722`).
- **01.e (from step 8) — a recoverable row.** The `Project / head` cell renders `<span class="pill recoverable">Recoverable</span>` instead of the project path (`templates.go:718`).
- **01.f (from step 5) — stale banner.** When `Counts.StaleReservations > 0` a `.banner.brand` appears above the table with a **Review** link to `?tab=processing` (`templates.go:699-708`).

**Exception flows**

- **01.e1 — unknown tab.** `GET /accounts-queue?tab=nope` → `400` error page reading `That queue tab does not exist.` (`linking.go:152-155`). The tab is validated against `queueTabs` before the store is asked, so an invented tab can never reach `LinkablePaymentRequests`' own `default:` branch.
- **01.e2 — no `payment:process`.** `403` from the middleware; the sidebar entry is not rendered either (`nav.go:63`, `navItemVisible`).
- **01.e3 — empty tab.** One row: `<td colspan="7" class="empty">Nothing in this tab right now.</td>` (`templates.go:755`).

**Business rules**

- The takeable set is exactly *approved · unclaimed · not on hold*: `status='approved' AND processing_by IS NULL AND on_hold=0` (`store.go:1113`).
- Counts always span the caller's whole scope, never the page or the search, so typing does not move the numbers (`store.go:1109-1111`, `models.go:421-423`).
- Ordering is `approved_at, id` — `id` tie-breaks because `CURRENT_TIMESTAMP` is second-resolution (`store.go:1179-1181`).
- Scope narrowing: `own` → `requester_id=?`, `assigned` → `manager_id=?`, `all` → unrestricted (`store.go:1101-1107`).

**Data touched** — reads `payment_requests` (`status, processing_by, processing_at, on_hold, hold_reason, approved_amount, amount, approved_at, treatment, needed_by`), `projects.name`, `heads.name`, `vendors.name`, `users.name`.

**Non-functional / UX notes** — at 390 px `table.t-cards` becomes `display:block` cards but the `<tr>` elements survive, so a `tr`-scoped locator still resolves (`linking-settlement.spec.ts:42-45`). Every cell carries `data-label` for the restack. The `.page-banner` is unconditional (not `d-only`), so a phone keeps a visible `h1` (`templates.go:658-660`). `ux.spec.ts` crawls this route on both devices.

**Open questions** — the nav label is `Accounts queue`, the `h1` is `Payment queue`, and the Phase-3 spec calls it the "To pay" queue at `/requests/to-pay` (`phase-3 spec:119`). Three names, one screen; no doc names which is canonical.

---

#### UC-C-02 — Search the payment queue

| | |
|---|---|
| **Goal** | "Find the one approved request I am looking for without scrolling." |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | Typing in the queue's search box |
| **Route(s)** | `GET /accounts-queue?tab={tab}&q={text}` (RT-01) |
| **Permission gate** | `payment:process` |
| **Coverage IDs** | S4 |
| **Priority** | high |

**Preconditions**

1. UC-C-01 preconditions hold.
2. At least two rows exist in the chosen tab so a narrowing is observable.

**Postconditions (success)** — the table shows only matching rows; the `.metric-strip` and `.segmented` counts are **unchanged**; nothing written.

**Postconditions (failure)** — nothing written; on a non-match the counts still stand.

**Main success scenario**

1. Accountant types into `#queue-q`, whose `aria-label` is `Search the payment queue` and whose placeholder is `Number, payee, project…` (`templates.go:695`).
2. Accountant presses the **Search** submit button in `form.m-filters` (`templates.go:696`).
3. System issues `GET /accounts-queue` carrying the hidden `tab` and the `q` (`templates.go:693-697`).
4. System lowercases and trims the term, escapes `\`, `%` and `_`, and matches it with `LIKE … ESCAPE '\'` against six columns: `r.number`, `u.name` (requester), the **display** payee `COALESCE(NULLIF(v.name,''),r.vendor_payee,'')`, `p.name` (project), `h.name` (head) and `CAST(r.amount AS TEXT)` (`store.go:1171-1178`).
5. System re-renders the table narrowed, with the same counts above it.

**Alternate flows**

- **02.a (step 4) — search by amount.** The needle matches `CAST(r.amount AS TEXT)`, i.e. the raw **paise** integer. Searching `5000.00` finds nothing; searching `500000` finds a ₹5,000.00 request (`store.go:1174`).
- **02.b (step 4) — payee.** The payee is searched exactly as displayed, so typing a vendor's name finds the row that shows that name (`store.go:1131-1134`, AF-09).
- **02.c (step 2) — no JavaScript.** The form is a plain `GET`; there is no htmx on the queue search, so it degrades by construction.

**Exception flows**

- **02.e1 — no match.** `<td colspan="7" class="empty">Nothing in this tab right now.</td>`, counts unchanged (`templates.go:755`).
- **02.e2 — wildcard injection.** `q=%25_` (URL-encoded `%_`) is escaped to literals, so it matches only rows genuinely containing `%_` (`store.go:1175-1176`). The sibling proof for the ledger is `ISS-026` (`regression-issues.spec.ts:545`).

**Business rules**

- Search narrows rows only; counts are computed before and independently of `opts.Query` (`store.go:1109-1123`).
- The escape replacer is `\`→`\\`, `%`→`\%`, `_`→`\_`, applied once (`store.go:1175`).

**Data touched** — read-only, same columns as UC-C-01.

**Non-functional / UX notes** — at 390 px `.m-filters` is the visible filter row; the desktop `.toolbar` idiom is not used on this screen. The search input has an explicit `aria-label` because the visible label is absent.

**Open questions** — none.

---

#### UC-C-03 — Take a request for processing from the queue

| | |
|---|---|
| **Goal** | "Claim this approved request so nobody else pays it while I do." |
| **Primary actor** | Accountant |
| **Supporting actors** | Requester, Manager (both later notified of the outcome) |
| **Scope / level** | system · user-goal |
| **Trigger** | Pressing **Take for processing** on a queue row |
| **Route(s)** | `POST /requests/{id}/record-payment` → `303 /payments/new?request={id}` (RT-04) |
| **Permission gate** | `reservation:reserve`, wrapped in `withCSRF` |
| **Coverage IDs** | S1, S2, L8 |
| **Priority** | critical |

**Preconditions**

1. The request is `status='approved'`.
2. `processing_by IS NULL`.
3. `on_hold=0`.
4. The caller holds `reservation:reserve` (AF-02).
5. A valid CSRF token is present (`app.go:451`).

**Postconditions (success)**

1. `payment_requests.status='processing'`.
2. `processing_by = caller.id`, `processing_at = CURRENT_TIMESTAMP`, `updated_at` bumped.
3. One `audit_log` row: `action='process'`, `entity_type='payment_request'`, `entity_id={id}`, `summary='Reserved request for processing'`, `after_json` carrying `processing_by` (`store.go:749`).
4. The caller is redirected `303` to `/payments/new?request={id}` (`linking.go:98`).
5. The row leaves the `Approved` tab's `Available` set and appears in `Processing`.

**Postconditions (failure)** — `status`, `processing_by`, `processing_at` unchanged; no audit row; no payment row.

**Main success scenario**

1. Accountant opens `/accounts-queue?tab=approved` (UC-C-01).
2. Accountant locates the row — for a test, `page.locator('tr').filter({ has: page.locator('a[href="/requests/{id}"]') })` (`fixtures.ts:288`).
3. Accountant presses **Take for processing** (a `<button class="btn small primary" type="submit">` inside `<form method="post" action="/requests/{id}/record-payment">`, AF-12).
4. System validates CSRF, then calls `ReserveRequest` (`linking.go:85`).
5. System executes one conditional `UPDATE payment_requests SET status='processing', processing_by=?, processing_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='approved' AND processing_by IS NULL AND on_hold=0` inside a transaction (`store.go:736-738`).
6. System reads `RowsAffected()`; `1` means the caller won.
7. System writes the `process` audit row and commits (`store.go:749-752`).
8. System redirects `303` to `/payments/new?request={id}`.
9. Accountant lands on the entry screen and sees `.reserve-bar` reading **Reserved by you** (UC-C-06).

**Alternate flows**

- **03.a (step 1) — the picker entry point.** The same POST is reached from `/payments/new`'s dropdown; see UC-C-04. Both entry points call the one `ReserveRequest`, which is what makes the concurrency guarantee identical on both paths (`phase-3 spec:102`, `linking.go:82`).
- **03.b (step 1) — the stale nudge.** A reservation past 24 h is resumed from `/requests/{id}/reservation/stale`, not from the form; see UC-C-20 (`templates.go:749`).

**Exception flows**

- **03.e1 — already taken (the race).** `RowsAffected()==0` → `ErrForbidden` "request is not available to process" (`store.go:746-748`). The handler reloads the request and renders the **conflict screen at HTTP 409**, not an error page — see UC-C-05 (`linking.go:86-93`).
- **03.e2 — on hold.** The `WHERE` clause's `on_hold=0` fails, so the same `RowsAffected()==0` path fires and the loser sees the conflict screen naming nobody (holder falls back to `Someone else`, `linking.go:60-62`). See UC-C-16.
- **03.e3 — not approved.** Same path: a `pending`, `rejected`, `completed` or `cancellation_requested` request matches zero rows.
- **03.e4 — no `reservation:reserve`.** `403` from the middleware. The queue never rendered the button (alternate 01.d), so this is only reachable by a hand-rolled POST.
- **03.e5 — missing or stale CSRF.** `withCSRF` refuses before the handler runs (`app.go:451`, `app.go:554`).
- **03.e6 — unknown id.** `ReserveRequest` matches zero rows → `ErrForbidden` → the handler's reload calls `Request(ctx,id)`, which returns `ErrNotFound` → `404` error page (`linking.go:87-90`).

**Business rules**

- Reservation is a **single conditional UPDATE**, which SQLite serialises; the first committer flips `status` and every later caller matches zero rows (`store.go:736-748`).
- There is exactly one reservation code path, shared by both entry points (`linking.go:82`, `templates.go:575`, `templates.go:722`).
- Reserving is gated on `reservation:reserve` rather than `payment:create`, because taking work out of the queue is a different act from recording money leaving the bank (`app.go:448-451`, `linking.go:79-81`).
- Nothing is ever auto-released; this transition is reversed only by UC-C-17 or UC-C-18 (`models.go:408-410`).

**Data touched** — writes `payment_requests.status/processing_by/processing_at/updated_at`, inserts `audit_log`.

**Non-functional / UX notes** — a POST cannot be a link; the control must be a button so a crawler or a prefetch cannot reserve. At 390 px the action cell is `class="c" data-label="Action"` and the button keeps a ≥40 px tap target (enforced by `ux.spec.ts`).

**Open questions** — none.

---

#### UC-C-04 — Pick a request from the payment picker

| | |
|---|---|
| **Goal** | "I want to record a payment; show me which approved requests I may pay." |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar/tab-bar "Pay", or `＋ Record a payment` on the queue |
| **Route(s)** | `GET /payments/new` (RT-02), `GET /payments/new/options` (RT-03), then `POST /requests/{id}/record-payment` (RT-04) |
| **Permission gate** | `payment:create` to read; `reservation:reserve` to take |
| **Coverage IDs** | S1, S3, S4, S15, X5 |
| **Priority** | critical |

**Preconditions**

1. The caller holds `payment:create` (`app.go:384`).
2. No `?request=` is present, so `paymentForm` takes the picker branch (`app.go:656-665`).

**Postconditions (success)** — a `200` page titled `Record a payment`; on choosing a row, the postconditions of UC-C-03 apply. Nothing is written by the read itself.

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. Accountant opens `/payments/new`.
2. System sees `parseID(r.URL.Query().Get("request")) == 0` and renders `payment_pick_request` (`app.go:656-664`).
3. System renders `<h1>Which approved request is this for?</h1>` with the `.sub` `Every payment belongs to exactly one approved request.` (`templates.go:605-606`).
4. System renders a `.banner.info` reading **Free payment entry has been removed** / `Payments recorded before this module remain in the ledger as history. New money out starts here.` (`templates.go:613-619`).
5. Accountant types into the combobox `#picker` (label **Search approved requests**, hint `Search by request number, requester, payee, project, head or amount.`) (`templates.go:621-630`).
6. System (htmx) fires `hx-get="/payments/new/options"` on `keyup changed delay:250ms, search` and swaps `#picker-list` `outerHTML` (`templates.go:624-626`).
7. System returns `payment_pick_options`: a `div.combo-list#picker-list` with `role="listbox"` and `aria-label="Approved requests"` (`templates.go:568`).
8. Accountant presses a takeable row — a `<button class="co" type="submit">` inside `<form method="post" action="/requests/{id}/record-payment">` (`templates.go:574-577`).
9. System reserves and redirects exactly as UC-C-03 steps 4–8.

**Alternate flows**

- **04.a (step 5) — no JavaScript.** The wrapper is `<form class="field" method="get" action="/payments/new">`, so pressing Enter re-renders the whole page from the same template with `?q=` (`templates.go:621`, `linking.go:171-185`).
- **04.b (step 7) — a taken request.** `Linkable.Unavailable` rows render as `<a class="co is-taken" href="/requests/{id}">` whose `<small>` reads `On hold — {reason}`, `Reserved by {name} at {hh:mm} — you cannot take this one`, or `{status} — you cannot take this one` (`templates.go:582-587`). The row is deliberately shown rather than hidden (`models.go:436-438`).
- **04.c (step 8) — no `reservation:reserve`.** The takeable row degrades to the same read-only `a.co.is-taken` the loser gets, not to a button that would 403 (`templates.go:578-580`).
- **04.d — "Recently paid by you".** Below the picker, a `table.t-cards` with head `Request · Payee · Paid · Date · Status` lists the caller's last five linked payments, each `.t-lead` linking `/payments/{paymentID}` (`templates.go:634-650`, `linking.go:192-226`).
- **04.e (step 3) — the queue shortcut.** `.pb-actions` carries `<a class="btn outline" href="/accounts-queue">Open the queue</a>` when the caller holds `payment:process` (`templates.go:609`).

**Exception flows**

- **04.e1 — no takeable and no visible rows.** `<div class="co"><span class="co-main"><b>No approved requests match</b><small>Clear the search, or check the queue.</small></span></div>` (`templates.go:589`).
- **04.e2 — no payments recorded yet.** `<td colspan="5" class="empty">You have not recorded a payment yet.</td>` (`templates.go:647`).
- **04.e3 — no `payment:create`.** `403` from the middleware on both RT-02 and RT-03.
- **04.e4 — the picker offers no free-standing entry at all.** There is no "enter a payment without a request" control anywhere on this screen; that is the X5 guarantee made visible (`templates.go:600-652`, `phase-3 spec:123`).

**Business rules**

- The picker's list is `LinkablePaymentRequests` with `Limit: 20` (`linking.go:171-178`) — the queue is unlimited, the picker is capped.
- Selecting a request is a POST, because a GET must never mutate (`templates.go:556-559`).
- The mockup's sentence "An administrator can re-enable direct entry in Configuration if you ever need it" is deliberately **not shipped** — no such control exists (`templates.go:563-565`). Note that `configSections` does carry an `allow_direct_payments` toggle whose hint says "Off" (`configuration.go:69-71`) — see divergence DV-06.

**Data touched** — reads `payment_requests`, `payments` (via `ListPayments`), `projects`, `heads`, `vendors`, `users`.

**Non-functional / UX notes** — the combobox is a real `role="listbox"`; the caret is `aria-hidden`. At 390 px each `.co` is a full-width tap target. `shell.spec.ts` includes `/payments/new` in `ROUTES` on both devices (`shell.spec.ts:13`).

**Open questions** — `recentPaidByActor` re-reads every candidate payment with `Payment(ctx,id)` because of a comment claiming `ListPayments` does not select the linkage columns (`linking.go:209-210`). It does (`store.go:1263`). See divergence DV-05.

---

#### UC-C-05 — Lose the reservation race and meet the conflict screen

| | |
|---|---|
| **Goal** | "Somebody beat me to it — tell me who, tell me nothing was saved, and tell me what I can do instead." |
| **Primary actor** | Second accountant (the loser) |
| **Supporting actors** | Accountant (the winner) |
| **Scope / level** | system · user-goal |
| **Trigger** | Pressing **Take for processing**, or opening `/payments/new?request={id}`, on a request another session already holds |
| **Route(s)** | `POST /requests/{id}/record-payment` → **409** `reservation_conflict`; `GET /payments/new?request={id}` → **409** `reservation_conflict`; `POST /requests/{id}/settlement-preview` → **409** |
| **Permission gate** | RT-04 `reservation:reserve` / RT-02 `payment:create` / RT-05 `payment:settle` |
| **Coverage IDs** | S5, S2 |
| **Priority** | critical |

**Preconditions**

1. Two signed-in sessions, both holding `reservation:reserve` and `payment:create`. Playwright's `secondPage` fixture creates the second accountant through the real Users screens (`fixtures.ts:25-35`).
2. One approved, unclaimed, not-on-hold request visible to both.

**Postconditions (success — i.e. the conflict is correctly reported)**

1. Exactly **one** session holds the reservation; `processing_by` equals the winner's id.
2. Exactly **one** `audit_log` `process` row exists for the request.
3. The loser receives HTTP **409** with a rendered `reservation_conflict` page — **not** the `error_page` template, so `.error-code` is absent (`linking-settlement.spec.ts:116-118`).
4. No payment row exists.

**Postconditions (failure)** — under no circumstance may two `process` audit rows exist for one request, nor `processing_by` change hands without a `reassign` row.

**Main success scenario**

1. Accountant A opens `/accounts-queue?tab=approved` and presses **Take for processing**; A lands on `/payments/new?request={id}` (UC-C-03).
2. Second accountant B, whose queue page was already open, presses **Take for processing** on the same row — or simply opens `/payments/new?request={id}` in a tab left open.
3. System's `ReserveRequest` matches zero rows for B and returns `ErrForbidden` (`store.go:746-748`); on the `GET` path `paymentEntry`'s `heldByCaller` is false (`linking.go:240-243`).
4. System reloads the request, resolves the holder's name through `withHolderName` → `UserByID` (`linking.go:41-53`), logs a `reservation conflict` warning (`linking.go:64-65`), and renders `reservation_conflict` with **HTTP 409** (`linking.go:66-70`).
5. B reads a `.banner.bad` whose bold line is `{Holder} took this request before you` and whose paragraph is `{Number} is now reserved by {Holder}. Nothing you typed has been saved, and no payment was created.` (`templates.go:772-778`).
6. B reads the `.req-head`: `.rh-no` the number, `.rh-amt` the approved amount, `h1` the payee (and short title), `.rh-status` a `<span class="pill processing">Processing — {Holder}</span>` and a `.waiting` line `Reserved {date}` (`templates.go:780-788`).
7. B reads the `What you can do` card — an `.a-list` of the three things a loser may actually do (`templates.go:790-797`).
8. B presses **Back to queue** in the `.action-bar` and picks a different request (`templates.go:799-803`).

**Alternate flows**

- **05.a (step 7) — B holds `reservation:reassign`.** A second `.a-list` entry appears: **Ask for it to be reassigned** / `{Holder} is told and must confirm no payment was started`, linking `/requests/{id}/reservation` (`templates.go:794`). Gated, because offering a control the reader cannot use leaks it. The seeded Accounts role does **not** hold reassign (AF-02), so an ordinary accountant does not see it.
- **05.b (step 7) — read-only.** **Open the request read-only** / `You can see it and comment, but not pay it`, linking `/requests/{id}` (`templates.go:795`).
- **05.c (step 8) — pick another.** `.action-bar` also carries `<a class="btn outline" href="/payments/new">Pick another request</a>` (`templates.go:801`).
- **05.d — losing it mid-settlement.** If the reservation is lost between the entry screen and the preview, `settlementPreview`'s own `heldByCaller` check funnels into the same screen (`linking.go:288-293`). If it is lost between the preview and the confirm, `RecordPaymentForRequest`'s `WHERE status='processing' AND processing_by=?` fails and the store returns `ErrForbidden` "reservation was lost before settlement" (`store.go:925-933`), which `settlementError` re-renders inside the sheet at `403` (`linking.go:341-364`).

**Exception flows**

- **05.e1 — the holder's user row is unreadable.** `withHolderName` logs a warning and leaves `ProcessingByName` empty; the screen says `Someone else` (`linking.go:44-52`, `linking.go:59-62`).
- **05.e2 — the request vanished between the failed reserve and the reload.** `respondStoreError` → `404` error page (`linking.go:87-90`).
- **05.e3 — N-way race.** Racing N goroutines on one request must yield exactly one `nil` and `N-1` `ErrForbidden`, with `processing_by` equal to the winner, under `-race` (`phase-3 spec:101`).

**Business rules**

- Losing the race is a **screen, not an error** (G15), and `reservationConflict` is the single place it is presented, so the queue, the picker and the entry form say the same words (`linking.go:55-71`).
- The status carried is `http.StatusConflict` — 409 (`linking.go:66`). Assert the status, not just the copy.
- `heldByCaller` is the one question every settlement screen asks first: `status=="processing" && *ProcessingBy == userID` (`linking.go:75-77`).

**Data touched** — reads `payment_requests`, `users`; writes nothing.

**Non-functional / UX notes** — the loser's queue row shows `Reserved by {name}` in a `.pill.processing`, and their action cell is a read-only `View` unless they hold reassign (`templates.go:738`, `templates.go:750-751`). The conflict screen keeps full app chrome — only `error_page` is chrome-less (`http_errors.go:121`).

**Open questions** — none.

---

#### UC-C-06 — Read the payment entry screen prefilled from the request

| | |
|---|---|
| **Goal** | "Show me what was approved and let me type only what I actually paid." |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Landing after UC-C-03 / UC-C-04, or **Resume** on a `processing` row |
| **Route(s)** | `GET /payments/new?request={id}` (RT-02) |
| **Permission gate** | `payment:create`; plus `heldByCaller` |
| **Coverage IDs** | S14, S13 |
| **Priority** | critical |

**Preconditions**

1. The request is `status='processing'`.
2. `processing_by = caller.id`.
3. The caller holds `payment:create`.

**Postconditions (success)** — a `200` page titled `Record payment`; **nothing written** — the primary button opens the settlement step, it does not save (`templates.go:349-351`).

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. Accountant is on `/payments/new?request={id}`.
2. System loads the request, confirms `heldByCaller`, and renders `payment_form` (`linking.go:233-263`).
3. System renders the `.page-banner`: eyebrow `Accounts · payment entry`, `<h1>Record the payment</h1>`, `.sub` `{Number} · {Vendor} · one request, one payment` (`templates.go:360-366`).
4. System renders `.reserve-bar`: a `.rb-dot`, the bold **Reserved by you**, and `.rb-meta` `since {HH:MM} · nobody else can process this request` (`templates.go:368-373`).
5. System renders the **What was approved** card: `dl.dl` rows `Approved amount` (`dd.big`), `Approved by`, `Payee`, `Charge to`, `Purpose`, plus a `Open the request →` link (`templates.go:375-384`).
6. System renders `<form method="post" action="/payments" enctype="multipart/form-data">` carrying four hidden inputs copied from the request: `request_id`, `head_id` (from `SelectedHeadID`), `vendor_payee` (the **display** payee `.Request2.Vendor`), `invoice_no` (`templates.go:386-391`).
7. System renders the **Payment** fieldset:
   - `#approved`, label **Approved amount**, `readonly`, `value` = `money(approvedOf)`, `data-approved` = the raw paise, hint `A payment can never exceed this. Overpayment means cancelling and raising a new request.` (`templates.go:395-400`).
   - `#amount`, label **Amount actually paid**, `name="amount"`, `inputmode="decimal"`, `required`, prefilled with `amountValue(approvedOf)`, inside `.money-field > .money-wrap` whose `.cur` carries the `₹` (`templates.go:401-405`).
   - `#diff-banner` with `#diff-text` reading `Matches the approved amount exactly` (`templates.go:406-412`).
   - `#paid_on`, label **Paid on**, `type="date"`, `required`, prefilled with today (`templates.go:413`, `linking.go:256`).
   - `#payment_mode`, label **Payment mode**, `required`, options `Choose…` plus the six modes `bank_transfer, cheque, upi, cash, card, other` (`templates.go:414-419`, `linking.go:993-995`).
   - `#reference_no`, label **Transaction / UTR reference**, `required` (`templates.go:420`).
8. System renders the **Proof and notes** fieldset: a `label.uploader` wrapping a `hidden` `<input type="file" name="attachment" accept=".pdf,.jpg,.jpeg,.png">` with **no accessible name**, and `#remarks`, label **Processing note** (optional) (`templates.go:424-437`).
9. System renders the empty `<div id="settle-mount"></div>` and the `.action-bar` with note `Nothing is saved until you confirm on the next step.` and the primary button **Payment settled →** (`templates.go:439-450`).
10. Accountant overwrites `#amount`; `initDifferenceBanner` recomputes `#diff-banner`'s class and `#diff-text`'s words live (`fervid-app.js:498-527`).

**Alternate flows**

- **06.a (step 10) — paid less than approved.** `#diff-banner` becomes `class="banner warn"` and `#diff-text` reads `₹{n} less than approved` (`fervid-app.js:512-518`).
- **06.b (step 10) — paid more than approved.** `class="banner bad"`, text `₹{n} more than approved — not allowed`. Display only; the store is the enforcement (`fervid-app.js:519`, `store.go:901-910`).
- **06.c (step 4) — release from here.** With `reservation:release` the reserve bar carries `<a class="btn small outline" href="/requests/{id}/reservation">Release</a>` and the action bar carries `<a class="btn outline" …>Cancel and release</a>` (`templates.go:372`, `templates.go:444`).
- **06.d (step 9) — no JavaScript.** The button's `formaction`/`formmethod`/`formenctype` attributes post the preview as `application/x-www-form-urlencoded`, so the file bytes cannot travel with it; the confirmation page re-offers the upload (`templates.go:446`, `templates.go:546-549`).

**Exception flows**

- **06.e1 — the request is not held by the caller.** The conflict screen at 409, not a 403 — the reader needs to know who has it (`linking.go:240-243`, UC-C-05).
- **06.e2 — unknown id.** `Request()` returns `ErrNotFound` → `404` error page (`linking.go:235-239`).
- **06.e3 — no `payment:create`.** `403` from the middleware, even while holding the reservation.

**Business rules**

- Prefill is: amount = `approvedOf(req)` (approved amount, falling back to the requested amount), payee = `req.Vendor`, head = `req.HeadID`, invoice = `req.InvoiceNo`, paid-on = today (`linking.go:244-262`, `linking.go:816-821`).
- The payee written is the **display** payee, never the raw `vendor_payee` snapshot: a `vendor_invoice` names its payee with `vendor_id` and leaves the snapshot empty, so the snapshot would write a payment with nobody to pay (`linking.go:257-260`, AF-09).
- Nothing on this screen writes. The only writer in the flow is `POST /payments` (`linking.go:30-32`).
- The `₹` lives in `.money-field`'s `.cur` prefix, so the input uses `amountValue`, not `money` (`templates.go:355-357`).

**Data touched** — reads `payment_requests` and its joins; writes nothing.

**Non-functional / UX notes** — a test must wait for `data-money-bound="1"` on **Amount actually paid** before filling it (AF-15). At 390 px `.span-4.m-half` fields pair up and `.action-bar` is sticky above the tab bar. `required` is used here (not `aria-required`) because none of these fields is inside a `data-when`-hidden container.

**Open questions** — none. The entry screen still offers no control for project/head, payee or invoice, and a recoverable request with no project still submits `head_id=0`, but this is no longer a defect: `RecordPaymentForRequest` (F-D-01, `store.go:1110-1114`) ignores the form's `head_id`/`vendor_payee`/`invoice_no` entirely and re-derives all three from the request row it already holds open, and `payments.head_id` is nullable since migration v8 (`migrations.go:453`). A zero head_id is stored as SQL NULL, never as a foreign key to nothing (`store.go:1128-1130`). See UC-C-36 exception 36.e1 and the correction on divergence DV-01/SD-07.

---

#### UC-C-07 — Preview the settlement without writing anything

| | |
|---|---|
| **Goal** | "Before I commit, show me approved against paid and ask me what kind of settlement this is." |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | Pressing **Payment settled →** on the entry screen |
| **Route(s)** | `POST /requests/{id}/settlement-preview` (RT-05) |
| **Permission gate** | `payment:settle`, `withCSRF`; plus `heldByCaller` |
| **Coverage IDs** | S13 (D8) |
| **Priority** | critical |

**Preconditions**

1. UC-C-06 preconditions hold and the caller still holds the reservation.
2. `#amount` parses as money (`money.ParsePaise`).
3. The caller holds `payment:settle` (`app.go:454`).

**Postconditions (success)**

1. **No row of any table is written. No transaction is opened.** `settlementPreview` holds no `BeginTx` (`linking.go:275-309`).
2. The sheet is delivered — as `settlement_sheet` when `HX-Request` is present, as the full-page `settlement_confirm` otherwise (`linking.go:327-335`).
3. HTTP `200`.

**Postconditions (failure)** — still nothing written.

**Main success scenario**

1. Accountant fills the four required fields and presses **Payment settled →**.
2. htmx posts `#amount, #paid_on, #payment_mode, #reference_no, #remarks, [name=csrf], [name=head_id], [name=vendor_payee], [name=invoice_no]` to `/requests/{id}/settlement-preview` and targets `#settle-mount` with `innerHTML` (`templates.go:447-449`).
3. System re-checks `heldByCaller`, parses the amount, computes `Approved`, `Paid`, `Difference = approved - paid` and `Match` (`linking.go:290-308`).
4. System snapshots the eight text fields into `Settlement.Fields` via `settlementFields` — deliberately **not** the file input (`linking.go:314-322`).
5. System renders `settlement_sheet` into `#settle-mount`, so **the URL does not change** (AF-10).
6. Accountant sees `.overlay#settle-sheet > .sheet[role=dialog][aria-modal=true][aria-labelledby=settle-title]` already visible (`templates.go:466-467`).
7. Accountant reads `.sh-head`: `<h2 id="settle-title">Confirm the payment</h2>` and `.sh-sub` `{Number} · {Vendor}`; the close control is `<a class="sh-close" href="/payments/new?request={id}" aria-label="Close">✕</a>` (`templates.go:468-471`).
8. Accountant reads the `.compare` block: `.cmp-row` `Approved`, `.cmp-row` `Actually paid`, then either `.cmp-row.match` `Difference / ₹0.00` or `.cmp-row.diff` `Difference / ₹{n} lower ⚠` (`templates.go:474-482`).
9. Accountant reads the prompt: `This matches the approved amount. Confirm to close the request.` when matched, else `You paid less than was approved. Which is it?` (`templates.go:485`).
10. Accountant reads the `.choice` of two radios (UC-C-08 step 3).
11. Accountant reads the `.banner.info` **Confirming saves the payment** / `It cannot be edited or cancelled afterwards. The request accepts no further payment — any balance needs a fresh request.` (`templates.go:507-513`).
12. Accountant presses **Confirm and save payment** — a submit with `formaction="/payments" formmethod="post"` (`templates.go:518`) — continuing into UC-C-08 or UC-C-09.

**Alternate flows**

- **07.a (step 2) — no JavaScript.** The plain submit posts the same fields as `x-www-form-urlencoded`, and `renderSettlement` picks `settlement_confirm`: a full page with a `.reserve-bar`, a **Payment about to be saved** card carrying a `<span class="pill neutral no-dot">Not saved yet</span>`, an `.uploader` that re-offers the file, every snapshotted field as a hidden input, and the same sheet markup inside a real `<form … enctype="multipart/form-data">` (`templates.go:524-553`).
- **07.b (step 12) — back out.** `.sh-foot` carries `<a class="btn outline" href="/payments/new?request={id}">Go back</a>` — an **anchor, not a button** (AF-10, `templates.go:516`).

**Exception flows**

- **07.e1 — unparseable amount.** `settlementError` re-renders the sheet with `Nothing has been saved. Correct it and confirm again.` in a `.banner.bad`, echoing the characters typed (`linking.go:294-298`, `linking.go:341-364`, `templates.go:473`). Status is `400` (`storeErrorStatus` on `ErrValidation`).
- **07.e2 — reservation lost in the meantime.** 409 conflict screen (`linking.go:288-293`).
- **07.e3 — no `payment:settle`.** `403` from the middleware — **and now from the writer too.** At audit time `POST /payments` was gated on `payment:create` alone, so this reader could skip the preview and post the settlement anyway (divergence DV-04, UC-C-28 alternate 28.b). **Fixed in Wave 3 (F-D-10):** `POST /payments` is wrapped in both gates, `payment:create` then `payment:settle` (`app.go:446-447`). See REPAIR-LOG.md Wave 3.
- **07.e4 — CSRF absent.** `withCSRF` refuses (`app.go:454`).

**Business rules**

- **D8: the preview is pure.** No `BeginTx`, no attachment staging, no writes of any kind. That is what lets the sheet say "Not saved yet" honestly (`linking.go:275-279`).
- One markup source, two deliveries, so the htmx fragment and the no-JS page can never disagree about what the confirmation says (`linking.go:324-326`).
- The file input is deliberately absent from `settlementFields`: it lives in the live form on the htmx path and is re-offered on the no-JS page, so the bytes are transmitted exactly once, by the final multipart POST (`linking.go:311-313`, `templates.go:351-354`).

**Data touched** — reads `payment_requests`; writes nothing.

**Non-functional / UX notes** — at 390 px the sheet shares the viewport with the tab bar; `linking-settlement.spec.ts` asserts zero horizontal overflow across the whole journey (`linking-settlement.spec.ts:83-85`). The sheet arrives **already visible** (no `hidden` attribute) because htmx swapped it in; the `data-open`/`data-close` sheets elsewhere start `hidden` (`templates.go:466` vs `templates.go:911`).

**Open questions** — none.

---

#### UC-C-08 — Settle the payment in full, even when paid is less than approved

| | |
|---|---|
| **Goal** | "This discharges the obligation — close the request." |
| **Primary actor** | Accountant |
| **Supporting actors** | Requester, Manager (notified) |
| **Scope / level** | system · user-goal |
| **Trigger** | Choosing **Fully settled** and pressing **Confirm and save payment** |
| **Route(s)** | `POST /payments` → `303 /payments/{paymentID}` (RT-06) |
| **Permission gate** | `payment:create` **+ `payment:settle`** (F-D-10, Wave 3), `withCSRF`; store re-checks the reservation |
| **Coverage IDs** | S10, S13, L10, L11, Q4, S9, S15 |
| **Priority** | critical |

**Preconditions**

1. The request is `processing` and `processing_by = caller.id`.
2. `settlement` posts as `settled`.
3. `amount > 0`, `paid_on` is a valid date, and `paid_on`'s month is not locked. `head_id` must resolve to an **active** head under an **active** project only when the request's own treatment is not `recoverable`; since migration v8 `payments.head_id` is nullable and `validatePayment` takes a `headOptional` flag that is true for a recoverable settlement, so a recoverable with no head skips this check entirely (`migrations.go:453`, `store.go:1922-1957`, caller at `store.go:1115`).
4. `amount <= approvedOf(request)` (G13, `store.go:901-910`).
5. No payment already links to this request (`idx_payments_request`, `phase-3 spec:16`).

**Postconditions (success)**

1. One `payments` row: `request_id={id}`, `settlement='settled'`, `partial_reason=''`, `entered_by=caller.id`, plus head/date/amount/payee/mode/invoice/reference/remarks (`store.go:911-913`).
2. `payment_requests.status='completed'`, `updated_at` bumped — **regardless of whether `amount < approved_amount`** (`store.go:921-925`).
3. Two `audit_log` rows, both inside the same transaction: `action='create'`, `entity_type='payment'`, `summary='Recorded payment ₹{n}'`; and `action='settle'`, `entity_type='payment_request'`, `summary='Settled request as completed'`, `after_json` carrying `{status, payment_id}` (`store.go:938-947`).
4. If a file was attached, one `payment_attachments` row and its own `attach` audit row, in the same transaction (`store.go:948-952`).
5. `notify.EventPaymentSettled` fires for the request (`app.go:723-724`).
6. `303` to `/payments/{paymentID}`.
7. The request has left every takeable set and appears only under the queue's `Paid` tab (`store.go:1164-1165`).

**Postconditions (failure)** — **no** `payments` row, **no** status change, **no** audit row, **no** attachment row. The insert and the transition commit together or not at all (`store.go:883-956`).

**Main success scenario**

1. Accountant is on the sheet from UC-C-07 with `Paid` below `Approved`.
2. Accountant reads the two `.choice` labels:
   - `input[type=radio][name=settlement][value=settled]` — **Fully settled** / `The obligation is discharged. Deductions such as TDS or retention were handled outside this system.` / `<span class="outcome good">Completed</span>` (`templates.go:487-491`).
   - `input[type=radio][name=settlement][value=partial]` — **Partial payment** / `A balance is genuinely still owed to the payee.` / `<span class="outcome warn">Manager review</span>` (`templates.go:492-496`).
3. `settled` is pre-checked whenever `Settlement.Settlement != "partial"` (`templates.go:488`).
4. Accountant confirms the `settled` radio is checked and presses **Confirm and save payment**.
5. System validates CSRF, reads `request_id` from the form, and refuses immediately if it is zero (`app.go:692-696`).
6. System builds `PaymentInput` from `head_id, paid_on, vendor_payee, payment_mode, invoice_no, reference_no, remarks, amount` via the `paymentInput` helper (`app.go:1988-1997`).
7. System stages any uploaded attachment to disk (`app.go:700-704`).
8. System calls `RecordPaymentForRequest`, which validates the settlement word, re-reads `status/processing_by/treatment/head_id/vendor(display)/invoice_no/amount/approved_amount` from the request row itself and **overwrites** `in.HeadID`/`in.VendorPayee`/`in.InvoiceNo` with those values — the form's copies of these three fields, built in step 6, are discarded rather than trusted (F-D-01, `store.go:1110-1114`) — validates the payment, opens one transaction, enforces the reservation, enforces the ceiling, inserts the payment, transitions the request, writes both audit rows, adds the attachment, and commits (`store.go:866-956`).
9. System fires `EventPaymentSettled` (`app.go:723-724`) and redirects `303` to `/payments/{paymentID}`.
10. Accountant reads the payment detail (UC-C-21): a `.banner.good` `Payment saved. The request is completed.`, a `.pill.completed`, and a `.compare .cmp-row.match` reading `Difference · confirmed settled by Accounts` (`templates.go:243-249`, `templates.go:271`).

**Alternate flows**

- **08.a (step 4) — paid exactly equals approved.** Identical outcome; the sheet's `.cmp-row.match` shows `₹0.00` and the prompt reads `This matches the approved amount. Confirm to close the request.` (`templates.go:478`, `templates.go:485`).
- **08.b (step 4) — paid strictly less, still settled.** This is the S10 case: the store applies **no** `amount >= approved` check on the settled branch (`store.go:921-925`). The detail screen's `.cmp-row.match` still names the difference, labelled `confirmed settled by Accounts`, so an agreed shortfall never reads like money still owed (`templates.go:270-272`).
- **08.c (step 7) — with proof.** `input[name="attachment"]` is set before the preview on the htmx path, or on the confirmation page without JS; either way the bytes travel once, on this POST (`templates.go:429`, `templates.go:548`).
- **08.d (step 9) — double confirm.** A back-button or double-tap resubmit fails the status guard, but `paymentCreate` then looks the request's payment up and redirects to it, so a double confirm is not a failure (`app.go:711-716`).

**Exception flows**

- **08.e1 — no `settlement` value, or an unrecognised one.** `ErrValidation` "choose payment settled or partial settlement" → `settlementError` re-renders the sheet at `400` with the figures intact (`store.go:869-871`, `linking.go:341-364`).
- **08.e2 — paid above approved (tampered amount).** `ErrValidation` `"{paid} is more than the approved {ceiling} — to pay more, cancel this request and raise a new one"` at `400` (`store.go:901-910`). See UC-C-28.
- **08.e3 — the month is locked.** `validatePayment` returns `ErrLockedMonth` → `409` (`store.go:1624-1626`, `http_errors.go:189-190`) and `settlementError` routes it back into the sheet because 409 < 500 (`linking.go:342-345`).
- **08.e4 — inactive head or project.** `ErrInactiveHead` → `400` (`store.go:1627-1634`).
- **08.e5 — `head_id` is 0 on a non-recoverable request.** `ErrValidation` "valid head, date, and positive amount are required" → `400`. **Not reachable for a recoverable** since migration v8 / `store.go:1922-1923` (`headOptional` is true when `treatment=="recoverable"`); the divergence this used to cite (DV-01) is fixed — see the correction there.
- **08.e6 — reservation lost.** `ErrForbidden` "reserve this request before recording its payment" (pre-insert) or "reservation was lost before settlement" (post-insert guard) → `403`, nothing written (`store.go:898-900`, `store.go:925-933`).
- **08.e7 — a payment already links to this request.** Refused by the status guard (the request is no longer `processing`) and, at the database level, by `idx_payments_request`. See UC-C-27.
- **08.e8 — attachment rejected.** `validateAttachment` failure returns before the transaction; the staged file is removed (`store.go:878-882`, `app.go:710`).

**Business rules**

- **S10:** `settlement == "settled"` → `completed`, with no comparison against the approved amount (`store.go:921-925`).
- **G13:** paid may never **exceed** approved. Overpaying is a different obligation, not a settlement decision (`store.go:901-910`).
- **S13:** the payment row and the request transition are one transaction; the payment never exists in a "recorded but request not advanced" limbo (`store.go:883-956`).
- **S9:** one request, one payment — the unique partial index plus the `processing` status guard (`phase-3 spec:16`, `store.go:925`).
- **S15/X5:** `request_id` is mandatory; `CreatePayment`/`CreatePaymentWithAttachment` survive only as seed/import helpers and are on no route (`app.go:687-696`, `phase-3 spec:91`).
- **C2:** every mutation records audit; the request-side action word is `settle` (`store.go:941`).

**Data touched** — writes `payments`, `payment_requests.status/updated_at`, `audit_log` ×2 (+1 per attachment), `payment_attachments`, `notifications` (one row per resolved recipient).

**Non-functional / UX notes** — `settlePayment` in `fixtures.ts:273-315` is the canonical driver: fill four fields → **Payment settled →** → `.overlay .sheet` → check `input[name="settlement"][value="settled"]` → **Confirm and save payment** → `/payments/{id}`. All three of Paid on, Payment mode and Transaction / UTR reference are `required` and live in the **same** `<form>` as the confirm button, so leaving any blank makes the browser silently refuse the confirm — the sheet opens and nothing happens (`linking-settlement.spec.ts:52-56`).

**Open questions** — none.

---

#### UC-C-09 — Record a partial settlement and send it to the manager

| | |
|---|---|
| **Goal** | "A balance is genuinely still owed — I cannot close this, so a manager must decide." |
| **Primary actor** | Accountant |
| **Supporting actors** | Manager (the request's own `manager_id`) |
| **Scope / level** | system · user-goal |
| **Trigger** | Choosing **Partial payment** in the settlement sheet |
| **Route(s)** | `POST /payments` → `303 /payments/{paymentID}` (RT-06) |
| **Permission gate** | `payment:create` **+ `payment:settle`**, and **`payment:mark_partial`** because `settlement=partial` (F-D-10, Wave 3), `withCSRF`; store re-checks the reservation |
| **Coverage IDs** | S11, S13, L9, L11 |
| **Priority** | critical |

**Preconditions**

1. UC-C-08 preconditions 1 and 3–5 hold.
2. `settlement` posts as `partial`.
3. `partial_reason` is non-blank after trimming (`store.go:872-874`).

**Postconditions (success)**

1. One `payments` row with `settlement='partial'` and `partial_reason` stored verbatim (`store.go:911-913`).
2. `payment_requests.status='partial_review'` (`store.go:921-924`).
3. Two audit rows: the payment's `create`, and the request's `action='mark_partial'`, `summary='Partial settlement: {reason}'` (`store.go:938-945`).
4. `notify.EventPaymentPartialReview` fires (`app.go:725-726`).
5. `303` to `/payments/{paymentID}`.
6. The request appears under the queue's `Partial review` tab and on the manager's `/requests/{id}/partial-review`.

**Postconditions (failure)** — nothing written; the request stays `processing` and the reservation stays with the caller.

**Main success scenario**

1. Accountant is on the sheet with `Paid` below `Approved`.
2. Accountant checks `input[name="settlement"][value="partial"]`.
3. The `[data-when="settlement:partial"]` field un-hides, revealing `#partial_reason`, label **Why only part was paid**, whose placeholder is `{ManagerName} reads this when deciding whether to close it.` (`templates.go:502-505`, `fervid-app.js:196-197`).
4. Accountant types the reason and presses **Confirm and save payment**.
5. System validates the reason server-side and, inside one transaction, inserts the payment, sets the status to `partial_review`, and writes both audit rows (`store.go:866-947`).
6. System fires `EventPaymentPartialReview` and redirects to the payment.
7. Accountant reads `.banner.good` `Payment saved. The request is with the manager.` and a `.compare .cmp-row.diff` reading `Still owed to the payee` (`templates.go:246`, `templates.go:268-269`).

**Alternate flows**

- **09.a (step 3) — pre-checked partial.** After a rejected confirm, `settlementError` carries `Settlement.Settlement == "partial"` forward, so the radio is re-checked and the field starts visible (`templates.go:493`, `templates.go:502`, `linking.go:360-363`).
- **09.b (step 2) — partial with `Paid == Approved`.** Nothing forbids it; the store cares only about the word and the reason. The sheet's prompt still reads `This matches the approved amount…`, which is then misleading — see open questions.

**Exception flows**

- **09.e1 — blank reason.** `ErrValidation` "a reason is required for a partial settlement" → the sheet re-renders at `400` with the amount, the choice and the (empty) reason preserved (`store.go:872-874`, `linking.go:341-364`).
- **09.e2 — `hidden` is not validation.** `#partial_reason` carries `aria-required="true"`, not `required`, because a `required` control inside a hidden container makes the form unsubmittable in Chrome. A hand-rolled POST with `settlement=partial` and no reason is refused server-side by 09.e1 (`templates.go:500-505`).
- **09.e3 through 09.e8** — identical to UC-C-08's e2–e8.

**Business rules**

- `partial` → `partial_review`, and the reason is mandatory (`store.go:872-874`, `store.go:921-924`).
- The request-side audit word is `mark_partial`, distinct from `settle`, so the trail reads differently (`store.go:941-943`, `linking.go:937`).
- Accounts can close a request that settles; the one outcome it cannot close is money still owed, so that request stops and a person decides (`linking.go:367-371`).
- The two settlement outcomes fire **different** events because they ask different people for different things (`app.go:720-727`).

**Data touched** — as UC-C-08, with `settlement='partial'`, `partial_reason` set, and `status='partial_review'`.

**Non-functional / UX notes** — `fixtures.ts:306-310` drives the partial branch by checking the radio then filling `getByLabel('Why only part was paid')`. `linking-settlement.spec.ts:70` asserts `[data-when="settlement:partial"]` is **hidden** before the radio is touched — a useful negative anchor.

**Open questions** — a partial settlement where `paid == approved` is accepted and leaves `Still owed to the payee ₹0.00` on the detail screen. Nothing in the code forbids it and no rule names it as illegal.

---

#### UC-C-10 — Manager accepts the partial and closes the request

| | |
|---|---|
| **Goal** | "The shortfall is acceptable — write the balance off and close this." |
| **Primary actor** | Manager (the request's own `manager_id`) |
| **Supporting actors** | Accountant, Requester |
| **Scope / level** | system · user-goal |
| **Trigger** | Pressing **Accept and close** in the partial review's `#close-sheet` |
| **Route(s)** | `GET /requests/{id}/partial-review` (RT-18), `POST /requests/{id}/accept-partial` → `303 /requests/{id}` (RT-19) |
| **Permission gate** | RT-18 `request:view`; RT-19 `approval:accept_partial` **and** `manager_id == actor.id` |
| **Coverage IDs** | S11, L11 |
| **Priority** | critical |

**Preconditions**

1. `payment_requests.status='partial_review'`.
2. Exactly one payment links to the request (`PaymentForRequest` resolves).
3. The caller **is** `req.ManagerID` (`store.go:980-982`).
4. The caller holds `approval:accept_partial` (AF-03 — the seeded Manager role does).
5. The caller's request scope reaches the row (`loadViewableRequest`, `requests.go:229-241`).

**Postconditions (success)**

1. `payment_requests.status='completed_partial'` — a terminal state deliberately distinct from a clean `completed` (`store.go:983`, `linking.go:474-476`).
2. One `audit_log` row: `action='accept_partial'`, `summary='Accepted partial settlement — completed, partial accepted'`, with `: {note}` appended when a note was given (`store.go:992-996`).
3. `303` to `/requests/{id}`.
4. The payment row is **untouched** — nothing on this screen can amend it (`linking.go:369-371`).

**Postconditions (failure)** — status stays `partial_review`; no audit row; no comment.

**Main success scenario**

1. Manager arrives from the notification, or from the request detail's **Decide the partial payment** button (rendered only for the manager holding the verb, `templates.go:2439-2441`), or from the Accounts queue's `Partial review` tab **View** link (`templates.go:742`).
2. System loads the request through `loadViewableRequest`, confirms `status=='partial_review'`, loads the linked payment, its attachments, the comments and the two-entity merged trail (`linking.go:377-425`).
3. System renders `.req-head` with the number, the approved amount, `h1` `{Vendor} — {ShortTitle}`, and a `.rh-status` carrying the status pill plus the shared `waitingOn` line (`templates.go:825-836`).
4. Manager reads the `.compare`: `Approved`, `Paid on {date}`, and `.cmp-row.diff` **Still owed to the vendor** (`templates.go:838-842`).
5. Manager reads the `.banner.warn` `{EnteredByName} marked this a genuine partial payment` quoting the reason (`templates.go:844-850`).
6. Manager reads **The payment that was recorded** card, headed by `<span class="pill neutral no-dot">Cannot be edited</span>`, listing `Amount paid`, `Paid on`, `Mode`, `Reference`, `Recorded by` and `Proof` (`templates.go:852-862`).
7. Manager reads **History and conversation** — one `ol.thread` interleaving the request's audit, the payment's audit and the comments, oldest first (`templates.go:867-884`, `linking.go:450-472`).
8. Manager presses **Accept and close** in the `.action-bar` (`button[data-open="close-sheet"]`, `templates.go:908`).
9. System (client-side) opens `.overlay#close-sheet` (`fervid-app.js:401-406`).
10. Manager reads `<h2>Accept {₹paid} and close?</h2>` and `.sh-sub` `{Number} · {₹diff} will never be paid against this request`, a second `.compare` whose third row is **Written off from this request**, and the hint `The request closes as Completed — partial accepted. If the balance is still due later, {Requester} raises a new request for {₹diff}.` (`templates.go:914-921`).
11. Manager optionally fills `#cl-note`, label **Note** (optional), placeholder `Recorded in the history and visible to everyone.` (`templates.go:922`).
12. Manager presses the sheet's **Accept and close** submit (`templates.go:924`).
13. System calls `AcceptPartial`, which re-reads the request in the transaction, refuses anyone who is not the manager, updates `WHERE id=? AND status='partial_review'`, writes the audit row and commits (`store.go:964-999`).
14. System redirects `303` to `/requests/{id}`, where the status pill now reads the `completed_partial` text.

**Alternate flows**

- **10.a (step 8) — ask first.** A `.comment-box` above the bar posts to `/requests/{id}/comment` carrying `<input type="hidden" name="return_to" value="partial-review">`, so a reply asked mid-decision comes back to the decision (`templates.go:886-895`, `requests.go:778-784`).
- **10.b (step 8) — dispute instead.** **Raise a concern** opens `#concern-sheet`; see UC-C-11.
- **10.c (step 12) — no note.** `note` is optional; the summary is then the bare sentence (`store.go:992-995`).

**Exception flows**

- **10.e1 — not the request's manager.** `AcceptPartial` returns bare `ErrForbidden` → `403` error page `You do not have permission to perform this action.` The `.action-bar` never rendered the sheets for this reader either (`store.go:980-982`, `templates.go:901`). See UC-C-12.
- **10.e2 — wrong status.** `RowsAffected()==0` → `ErrForbidden` "only a partial-review request can be accepted" → `403` (`store.go:987-991`).
- **10.e3 — opening the screen off `partial_review`.** `requestPartialReview` redirects `303` to `/requests/{id}` rather than rendering a page every sentence of which would be false (`linking.go:382-389`).
- **10.e4 — no payment recorded.** `404` `No payment has been recorded against this request yet, so there is nothing to review.` (`linking.go:393-398`).
- **10.e5 — outside the caller's request scope.** `404` `The requested record was not found.` **Corrected since Wave 3 (F-G-002, commit `1fac147`): this answered `403` `You do not have permission to view this request.` at the time of this audit.** A row outside the caller's scope now answers exactly as a row that does not exist, so the status code is no longer an existence oracle (`requests.go:295-308`).
- **10.e6 — no `approval:accept_partial`.** `403` from the middleware on RT-19; the read route RT-18 still succeeds, and the bar degrades — see alternate 11.c.

**Business rules**

- **The decision belongs to the request's own manager**, not to everyone holding the verb. `ApproveRequest` and `decideRequest` refuse a non-manager, and writing a balance off for good is the last place to relax that (`store.go:971-982`).
- `completed_partial` is permanent and visible: the ledger renders `.pill.completed-partial` so anyone reading later can see a balance was written off rather than paid (`store.go:959-963`, `templates.go:645`, `templates.go:735`).
- Neither decision moves money and neither can amend the payment (S12) (`linking.go:369-371`).
- The action bar is chosen **server-side** on the very permission the routes are gated by; the mockup's `data-for`/`data-not-for` persona attributes are prototype scaffolding (`templates.go:819-822`).

**Data touched** — writes `payment_requests.status/updated_at`, `audit_log`, `notifications`; reads `payments`, `payment_attachments`, `request_comments`, `audit_log` (both entity types).

**Correction (Waves 2/4)** — at the time of this audit accepting a partial fired nothing. `notify.EventPaymentPartialAccepted` was added by migration v9 (Wave 2, commit `25411b8`) and wired in Wave 4, so a fifth success postcondition now holds: one `notifications` row per resolved recipient — the requester learns the balance is never coming, the assigned accountant learns to stop chasing it (`events.go:55-56`, call site `linking.go:554`). F-F-06.

**Non-functional / UX notes** — the sheets are the manager's alone, so a spec must sign in as `approverFor(runId)`, never as the admin (`fixtures.ts:133-143`). The accept button is a plain `.btn.primary`, not the mockup's green `.btn.approve` — that class existed with no rule when the screen was built (`templates.go:906-908`); it now has one (AF-13), so the mockup's colour could be restored.

**Open questions** — `AcceptPartial` writes the manager's note into the audit summary only; it does not append a `request_comments` row, so the note appears in the trail as an event body rather than as a comment. Deliberate or not is not recorded.

---

#### UC-C-11 — Manager raises a concern about the partial payment

| | |
|---|---|
| **Goal** | "This shortfall is not acceptable — put my objection on the record and keep the request open." |
| **Primary actor** | Manager (the request's own `manager_id`) |
| **Supporting actors** | Accountant |
| **Scope / level** | system · user-goal |
| **Trigger** | Pressing **Raise a concern** on the partial review |
| **Route(s)** | `POST /requests/{id}/raise-concern` → `303 /requests/{id}/partial-review` (RT-20) |
| **Permission gate** | `approval:accept_partial` **and** `manager_id == actor.id` |
| **Coverage IDs** | S11 |
| **Priority** | high |

**Preconditions**

1. `status='partial_review'`.
2. The caller is `req.ManagerID` and holds `approval:accept_partial`.
3. `comment` is non-blank after trimming (`store.go:1005-1007`).

**Postconditions (success)**

1. One `request_comments` row authored by the manager (`store.go:1027`).
2. One `audit_log` row: `action='concern'`, `summary='Raised concern: {comment}'` (`store.go:1030`).
3. `status` stays `partial_review` — the money has already left, and this reverses nothing (`linking.go:486-487`).
4. `303` back to `/requests/{id}/partial-review`.

**Postconditions (failure)** — no comment row, no audit row, status unchanged.

**Main success scenario**

1. Manager presses **Raise a concern** (`button.btn.outline[data-open="concern-sheet"]`, `templates.go:905`).
2. System opens `.overlay#concern-sheet` (`templates.go:928-938`).
3. Manager reads `<h2>Raise a concern</h2>` and `.sh-sub` `The request stays open as Partial — under discussion`.
4. Manager fills `#cn-reason`, label **What is wrong** (required), placeholder `Accounts can reply in the conversation, but the recorded payment cannot be changed.` (`templates.go:933`).
5. Manager reads the `.banner.warn` **This does not reverse anything** / `The {₹paid} has left the bank. Raising a concern keeps the request open so the two of you can agree what happens next.` (`templates.go:934`).
6. Manager presses **Raise concern** (`templates.go:936`).
7. System calls `RaiseConcern`, which re-reads the request, refuses a non-manager, refuses any status but `partial_review`, inserts the comment, writes the `concern` audit row and commits (`store.go:1004-1033`).
8. System redirects `303` to the same review screen, where the concern now appears in the thread.

**Alternate flows**

- **11.a (step 8) — the trail renders the words once.** `partialTrail` drops the `comment`, `attach` and `concern` **audit** rows because the conversation already renders those sentences in full — `RaiseConcern` copies the whole comment into its summary (`linking.go:444-458`).
- **11.b (step 8) — decide later.** The request remains in `partial_review`, so the manager may return and accept (UC-C-10) at any time; `legalTransitions` allows `partial_review → completed | completed_partial` and nothing else (`requests.go:100`).
- **11.c — a reader who is not the decider.** The `{{else}}` branch of the action bar renders `<span class="ab-note d-only">{ManagerName} decides…</span>` plus a **Back** link, and appends `. You can still comment.` only when the reader actually holds `request:comment` (`templates.go:939-948`).

**Exception flows**

- **11.e1 — blank comment.** `ErrValidation` "a concern comment is required" → `400` error page (`store.go:1005-1007`). Note this handler does **not** re-render the sheet; it goes to `respondStoreError` (`linking.go:490-493`).
- **11.e2 — not the manager.** bare `ErrForbidden` → `403` (`store.go:1021-1023`).
- **11.e3 — wrong status.** `ErrForbidden` "only a partial-review request can receive a concern" → `403` (`store.go:1024-1026`).

**Business rules**

- Disputing a shortfall answers to the same owner as accepting it: the manager the request was routed to, not everybody who happens to hold the verb (`store.go:1018-1023`).
- A concern is a line in the trail rather than a chat message, which is why it is a sheet and not an inline disclosure (`templates.go:814-817`).
- `#cn-reason` uses `required` (not `aria-required`) because the concern sheet has no `data-when` reveal — the field is always visible once the sheet opens.

**Data touched** — writes `request_comments`, `audit_log`, `notifications`.

**Correction (Waves 2/4)** — at the time of this audit a concern fired nothing, so the accountant it is addressed to learned of it only by reloading. `notify.EventPaymentPartialConcern` was added by migration v9 (Wave 2, commit `25411b8`) and wired in Wave 4: a fifth success postcondition now holds, one `notifications` row per resolved recipient, filed under `mention` rather than `activity` because it waits on the accountant's answer (`events.go:60`, call site `linking.go:567`). F-F-06.

**Non-functional / UX notes** — the sheet starts `hidden` and is opened by `data-open`; without JavaScript it is unreachable. That is a genuine no-JS gap on this screen, unlike the settlement sheet which has a full-page fallback.

**Open questions** — with JavaScript disabled neither `#close-sheet` nor `#concern-sheet` can be opened, so the manager has no way to decide a partial settlement. No fallback is documented.

---

#### UC-C-12 — Accept a partial on a request you do not manage, while holding the verb

| | |
|---|---|
| **Goal** | (adversarial) "I am an administrator with every grant — can I close somebody else's partial settlement?" |
| **Primary actor** | Administrator |
| **Supporting actors** | Manager (the legitimate decider) |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | A hand-rolled `POST /requests/{id}/accept-partial` |
| **Route(s)** | RT-19, RT-20 |
| **Permission gate** | `approval:accept_partial` **passes**; the store's ownership check **refuses** |
| **Coverage IDs** | S11, R6 |
| **Priority** | critical |

**Preconditions**

1. `status='partial_review'` with a linked payment.
2. The caller holds `approval:accept_partial` — an admin holds every pair (AF-04).
3. `req.ManagerID != caller.ID`.
4. A valid CSRF token (read from the `fervid_csrf` cookie when the screen carries no form, as `ISS-025` does — `regression-issues.spec.ts:521-525`).

**Postconditions (success — i.e. correctly refused)**

1. HTTP **403**, body `You do not have permission to perform this action.` (`http_errors.go:191-192`).
2. `status` still `partial_review`.
3. No `accept_partial` audit row, no `concern` audit row, no comment row.

**Postconditions (failure)** — a `completed_partial` status written by a non-manager would be the defect.

**Main success scenario**

1. Administrator opens `/requests/{id}/partial-review`. The route's gate is only `request:view`, so the page renders (`app.go:482`).
2. System evaluates the action-bar condition `and (eq .User.ID .Request2.ManagerID) (.Perms.Can "approval" "accept_partial")` — the first half is false, so **neither sheet and neither button is rendered**; the reader gets the `{{else}}` bar naming the manager instead (`templates.go:901`, `templates.go:939-948`).
3. Administrator posts to `/requests/{id}/accept-partial` by hand with a valid CSRF token.
4. `RequirePermission("approval","accept_partial")` passes.
5. `AcceptPartial` opens a transaction, re-reads the request with `requestInTx`, compares `before.ManagerID != actor.ID`, and returns bare `ErrForbidden` before touching a single row (`store.go:976-982`).
6. System answers `403`; the deferred `tx.Rollback()` discards the transaction.

**Alternate flows**

- **12.a — the same test against `raise-concern`.** Identical: the ownership check sits in `RaiseConcern` at `store.go:1021-1023`, and it runs **before** the status check, so a non-manager gets 403 even on a wrong-status request.
- **12.b — the legitimate manager, same request.** Succeeds; UC-C-10.

**Exception flows**

- **12.e1 — no CSRF.** `withCSRF` refuses before the gate, which would mask the check under test. A test must carry a real token.

**Business rules**

- Holding `approval:accept_partial` says a person **may** accept a shortfall; it never says **whose** (`store.go:971-975`).
- The check is in the store, not only the template, so a hand-rolled POST meets the same rule as the screen — hiding a control hides nothing from a script (`templates.go:819-822`).
- This is the concrete form of the standing rule recorded in `PROGRESS.md:356-357` and the shared brief.

**Data touched** — reads `payment_requests`; writes nothing.

**Non-functional / UX notes** — the template gate and the store gate must be tested **separately**: the first proves nothing is offered, the second proves nothing is accepted. `fixtures.ts:325` warns that the sheets belong to the manager, not to an admin holding every grant.

**Open questions** — none.

---

#### UC-C-13 — Put an approved request on hold

| | |
|---|---|
| **Goal** | "I cannot pay this until the requester answers a question — pause it without rejecting it." |
| **Primary actor** | Accountant |
| **Supporting actors** | Requester (told, and asked to answer) |
| **Scope / level** | system · user-goal |
| **Trigger** | Pressing **Put on hold** on the request detail |
| **Route(s)** | `POST /requests/{id}/hold` → `303 /requests/{id}` (RT-16) |
| **Permission gate** | `payment:hold`, `withCSRF`; plus `loadViewableRequest` row scope |
| **Coverage IDs** | L7, N1 |
| **Priority** | high |

**Preconditions**

1. `status='approved'`.
2. `on_hold=0`.
3. The caller holds `payment:hold` (AF-02) and `request:view` scope reaching the row.
4. `reason` is non-blank after trimming (`store.go:1038-1041`).

**Postconditions (success)**

1. `on_hold=1`, `hold_reason={reason}`, `updated_at` bumped. **`status` stays `approved`** (AF-07, `store.go:1047`).
2. One `audit_log` row: `action='hold'`, `summary='On hold: {reason}'` (`store.go:1056`).
3. `notify.EventRequestOnHold` fires, whose seeded audience is the **Requester** (`linking.go:703`, `migrations_notifications.go:48-50`).
4. `303` to `/requests/{id}`.
5. The request leaves the queue's takeable set (`on_hold=0` fails) and appears under the `On hold` tab (`store.go:1113`, `store.go:1161`).

**Postconditions (failure)** — `on_hold`, `hold_reason`, `status` unchanged; no audit row; no notification.

**Main success scenario**

1. Accountant opens `/requests/{id}` for an approved, unheld request.
2. System renders the `.action-bar` with `<button class="btn outline" type="button" data-open="hold-sheet">Put on hold</button>`, gated on `not $held`, `status == "approved"` and `payment:hold` (`templates.go:2449-2451`).
3. Accountant presses it; `.overlay#hold-sheet` opens (`templates.go:2458-2470`).
4. Accountant reads `<h2>Put {Number} on hold</h2>` and `.sh-sub` `Payment is blocked until Accounts lifts it`.
5. Accountant fills `#hold-reason`, label **What do you need from the requester** (required), placeholder `They see this exactly as you write it.`, and reads the hint `{Requester} is asked to answer. Nobody in Accounts can reserve or pay this until you release it.` (`templates.go:2464-2465`).
6. Accountant presses the sheet's **Put on hold** submit (`templates.go:2467`).
7. System calls `HoldRequest`, whose conditional `UPDATE … WHERE id=? AND status='approved' AND on_hold=0` is the whole guard (`store.go:1047`).
8. System writes the `hold` audit row, commits, fires the event and redirects.
9. Accountant re-reads the request and sees the `.banner.warn` **This request is on hold** quoting the reason, plus `Payment is blocked until Accounts lifts the hold.` (`templates.go:2302-2311`).

**Alternate flows**

- **13.a (step 9) — the requester's reading.** The "Approved requests are locked" banner is suppressed while a hold is active, because stacking it would tell the requester the approver is deciding when Accounts is holding it (`templates.go:2283-2297`).
- **13.b (step 2) — the pill.** `request_head` renders `<span class="pill hold">On hold</span>` in place of the status pill while `activeHold` is true, because `status` is still `approved` (`templates.go:2261-2266`).
- **13.c — from the stale nudge.** `reservation_stale` offers **Put it on hold** / `If you are waiting on the requester for something — release the reservation first`, gated on `payment:hold` and linking to the **reservation** screen, because a hold pauses an *approved* request and this screen only exists while it is reserved (`templates.go:1117-1121`).

**Exception flows**

- **13.e1 — blank reason.** `ErrValidation` "a hold reason is required" → `400` error page (`store.go:1038-1041`, `linking.go:699-702`).
- **13.e2 — already on hold, or not approved.** `RowsAffected()==0` → `ErrForbidden` "only an approved, not-already-held request can be held" → `403` (`store.go:1051-1055`).
- **13.e3 — the request is `processing`.** Same as 13.e2: a reserved request must be released first (the rule alternate 13.c's sub-line states).
- **13.e4 — no `payment:hold`.** `403` from the middleware; the button was not rendered.
- **13.e5 — outside the caller's request scope.** `404` `The requested record was not found.` — **`403` until Wave 3 (F-G-002, commit `1fac147`)**; `loadViewableRequest` now answers a scope refusal exactly as it answers a missing row (`linking.go:695-698`, `requests.go:295-308`).

**Business rules**

- **L7:** the request stays `approved`, so the queue's own availability test takes it out of the takeable set without inventing a status for it (`linking.go:686-688`).
- A hold is the one pause in the flow that is not a decision, so it is a **state of the request screen** rather than a screen of its own (`linking.go:680-685`).
- The reason is mandatory in the **store**, so an empty one is refused whether it arrives from the sheet or from a script (`linking.go:691-693`).
- `hidden` fieldsets still submit and `hidden` is not validation — every reveal is re-enforced server-side (`PROGRESS.md:275-276`).

**Data touched** — writes `payment_requests.on_hold/hold_reason/updated_at`, `audit_log`, `notifications`.

**Non-functional / UX notes** — the `.action-bar` on the request detail is a single sticky bar at phone width; the hold note joins it rather than bringing a second bar (`templates.go:2403-2405`). The sheet requires JavaScript to open — same gap as UC-C-11.

**Open questions** — none.

---

#### UC-C-14 — Requester adds a clarification while the request is on hold

| | |
|---|---|
| **Goal** | "Answer the question Accounts asked, without being able to change what was approved." |
| **Primary actor** | Requester |
| **Supporting actors** | Accountant |
| **Scope / level** | system · user-goal |
| **Trigger** | Posting in the conversation box on an on-hold request |
| **Route(s)** | `POST /requests/{id}/comment` → `303 /requests/{id}` (RT-21) |
| **Permission gate** | `request:comment`; plus `loadViewableRequest` row scope |
| **Coverage IDs** | Q6, N7, L7 |
| **Priority** | high |

**Preconditions**

1. `status='approved'` and `on_hold=1`.
2. The caller holds `request:comment` (the seeded Requester role does, `migrations.go:355-361`).
3. The caller's request scope reaches the row — `own` suffices for the requester.
4. `body` is non-blank after trimming (`store.go:1002-1004`).

**Postconditions (success)**

1. One `request_comments` row.
2. One `audit_log` row: `action='comment'`, `summary='Commented on request'`, `after_json` carrying `comment_id` (`store.go:1022-1025`).
3. **No field of `payment_requests` changes** — not `amount`, not `approved_amount`, not `project_id`, not `head_id`, not `status`, not `on_hold`, not `hold_reason` (`store.go:1001-1031` writes only the comment and the audit row).
4. `303` to `/requests/{id}`, where the comment appears in the `ol.thread`.

**Postconditions (failure)** — no comment, no audit row, no field change.

**Main success scenario**

1. Requester opens `/requests/{id}` and reads the `.banner.warn` **This request is on hold** with the quoted reason (`templates.go:2302-2311`).
2. Requester scrolls to `request_thread`'s `.comment-box`, whose label is **Add a comment** and whose textarea placeholder is `Anyone who can see this request will see your comment.` (`templates.go:2189-2196`).
3. Requester types the answer and presses **Post comment** (`templates.go:2193`).
4. System resolves the request through `loadViewableRequest`, then calls `AddRequestComment`, which re-reads the request inside the transaction only to prove it exists — there is no status test at all (`requests.go:768-776`, `store.go:1011-1013`).
5. System inserts the comment and the audit row, commits, and redirects `303`.
6. Requester sees their words as an `li.is-comment.is-me` in the thread (`templates.go:2177`).

**Alternate flows**

- **14.a — attaching a document instead.** The same rule holds: an on-hold request accepts the Phase-2 conversation routes and Phase 3 adds **no** field-editing path for approved or held requests (`phase-3 spec:130`).
- **14.b — the accountant replying.** Accounts holds `request:comment` too (`migrations.go:383`), so the thread is genuinely two-way.
- **14.c — the queue's affordance.** An on-hold row's action cell is `<a class="btn small" href="/requests/{id}">Read reply</a>`, which is the accountant's way back into the conversation (`templates.go:741`).

**Exception flows**

- **14.e1 — blank body.** `ErrValidation` "comment cannot be empty" → `400`. The browser also refuses: the textarea is `required` (`store.go:1002-1004`, `templates.go:2193`).
- **14.e2 — attempting to edit fields instead.** `/requests/{id}/edit` refuses: `loadEditableRequest` demands `status` be `pending` or `returned` and answers `400` `This request can no longer be edited.` otherwise (`requests.go:596-599`). The detail screen does not render an **Edit request** button for an approved request either (`templates.go:2413-2415`).
- **14.e3 — no `request:comment`.** `403` from the middleware; the `.comment-box` was not rendered (`templates.go:2189`).
- **14.e4 — outside scope.** `404` `The requested record was not found.` — **`403` until Wave 3 (F-G-002, commit `1fac147`)** (`requests.go:295-308`).

**Business rules**

- **Q6:** an on-hold request accepts requester comments and attachments **without changing approved fields** (`phase-3 spec:130`, `phase-3 spec:163`).
- `AddRequestComment` applies no status filter, which is precisely what makes the on-hold conversation possible; the protection against field change is that no field-editing route accepts an approved request.
- **N7:** the whole stream is visible to everyone who can see the request — `request_thread`'s own sub-line says so (`templates.go:2173`).

**Data touched** — writes `request_comments`, `audit_log`; reads `payment_requests`.

**Non-functional / UX notes** — the thread is one chronological stream of events, comments and attachments, not three lists (`models.go:465-467`). At 390 px `.comment-box` sits above the sticky action bar; `ux.spec.ts` fails on a control trapped under the tab bar.

**Open questions** — none.

---

#### UC-C-15 — Lift the hold

| | |
|---|---|
| **Goal** | "The question is answered — put this back in the queue." |
| **Primary actor** | Accountant |
| **Supporting actors** | Requester |
| **Scope / level** | system · user-goal |
| **Trigger** | Pressing **Release hold** on the request detail |
| **Route(s)** | `POST /requests/{id}/unhold` → `303 /requests/{id}` (RT-17) |
| **Permission gate** | `payment:hold`, `withCSRF`; plus row scope |
| **Coverage IDs** | L7 |
| **Priority** | high |

**Preconditions**

1. `on_hold=1`.
2. The caller holds `payment:hold`.
3. The caller's request scope reaches the row.

**Postconditions (success)**

1. `on_hold=0`, `hold_reason=''`, `updated_at` bumped (`store.go:1070`).
2. One `audit_log` row: `action='unhold'`, `summary='Hold lifted'` (`store.go:1079`).
3. `303` to `/requests/{id}`.
4. The request is takeable again and returns to the `Approved` tab.

**Postconditions (failure)** — `on_hold` and `hold_reason` unchanged; no audit row.

**Main success scenario**

1. Accountant opens the on-hold request and reads the requester's reply in the thread (UC-C-14).
2. System renders two controls, both gated on `$held` **and** `payment:hold`: `<a class="btn outline" href="/requests/{id}">Keep on hold</a>` and a form posting `/requests/{id}/unhold` with the submit **Release hold** (`templates.go:2445-2448`).
3. System also renders the note `Releasing returns it to Approved — awaiting payment.` for a reader who may lift it (`templates.go:2405`).
4. Accountant presses **Release hold**.
5. System calls `UnholdRequest`, whose `UPDATE … WHERE id=? AND on_hold=1` is the guard (`store.go:1070`).
6. System writes the `unhold` audit row and redirects.
7. Accountant returns to `/accounts-queue?tab=approved` and finds the row takeable again.

**Alternate flows**

- **15.a (step 2) — decline to lift.** **Keep on hold** is a link back to the same screen, not a POST: the accountant read the answer and decided it was not enough, and that decision writes nothing (`templates.go:2442-2446`).
- **15.b — no `payment:hold`.** The bar renders the note `Only Accounts can take this off hold.` instead of the two controls (`templates.go:2405`).
- **15.c — the audit tone.** `unhold` is deliberately grouped with the reservation movements (`brand` tone, `◷` glyph) because lifting a hold puts the request back in the takeable queue (`linking.go:850-853`, `linking.go:872-873`).

**Exception flows**

- **15.e1 — not on hold.** `RowsAffected()==0` → `ErrForbidden` "request is not on hold" → `403` (`store.go:1074-1078`).
- **15.e2 — no `payment:hold`.** `403` from the middleware.
- **15.e3 — outside scope.** `404` from `loadViewableRequest` — **`403` until Wave 3 (F-G-002, commit `1fac147`)** (`linking.go:711-714`, `requests.go:295-308`).

**Business rules**

- No reason is required to lift a hold: the hold itself is the thing that needed explaining, and lifting it is answering the question rather than asking a new one (`linking.go:707-709`).
- `payment:hold` is **one** grant for both directions, because the person who can stop a payment is the person who must be able to start it again (`app.go:471-475`).
- **L7:** only `payment:hold` lifts a hold, which is what restricts it to Accounts (`store.go:1062-1063`).
- Note that `UnholdRequest`'s `WHERE` does **not** test `status='approved'`; AF-07 makes that safe on a well-formed row, and the queue's `hold` tab adds the status test as belt and braces (`store.go:1070`, `store.go:1158-1161`).

**Data touched** — writes `payment_requests.on_hold/hold_reason/updated_at`, `audit_log`.

**Non-functional / UX notes** — at the time of this audit, no notification event fired on unhold; only `EventRequestOnHold` existed (`events.go:15`). **Fixed (F-F-06, migration v9, Wave 4 wiring):** `EventRequestUnheld` now exists and fires from `linking.go:803`, so the requester learns the hold was lifted by email/in-app as well as from the request screen.

**Open questions** — none. (Was: is the absence of an "unhold" notification deliberate? Answered by the fix above — it was a gap, not a decision, and it is closed.)

---

#### UC-C-16 — Try to reserve an on-hold request

| | |
|---|---|
| **Goal** | (adversarial) "The queue will not offer this row — can I take it by posting anyway?" |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | A hand-rolled `POST /requests/{id}/record-payment` against a held request |
| **Route(s)** | RT-04 |
| **Permission gate** | `reservation:reserve` **passes**; the store's `WHERE` refuses |
| **Coverage IDs** | S3, L7, S2 |
| **Priority** | high |

**Preconditions**

1. `status='approved'`, `on_hold=1`, `processing_by IS NULL`.
2. The caller holds `reservation:reserve` and a valid CSRF token.

**Postconditions (success — correctly refused)**

1. HTTP **409** with the `reservation_conflict` screen (not `error_page`).
2. `status` still `approved`, `on_hold` still 1, `processing_by` still NULL.
3. No `process` audit row.

**Postconditions (failure)** — a reservation on a held request would defeat L7 entirely: the requester was told payment is blocked.

**Main success scenario**

1. Accountant opens `/accounts-queue?tab=hold` and sees the row with `<span class="pill hold">On hold</span>` and an action cell of **Read reply**, i.e. **no** reserve control (`templates.go:733`, `templates.go:741`).
2. Accountant posts to `/requests/{id}/record-payment` by hand with a valid token.
3. `RequirePermission("reservation","reserve")` passes.
4. `ReserveRequest`'s `WHERE … AND on_hold=0` matches zero rows → `ErrForbidden` (`store.go:736-748`).
5. `requestRecordPayment` reloads the request and calls `reservationConflict` (`linking.go:86-93`).
6. Because `ProcessingBy` is NULL, `withHolderName` returns early and `Holder` falls back to `Someone else` (`linking.go:42-44`, `linking.go:59-62`).
7. System answers **409** with a banner reading `Someone else took this request before you` — factually wrong for a hold, but the state is correctly protected. See open questions.

**Alternate flows**

- **16.a — the picker's rendering.** The same request appears in `Linkable.Unavailable` as an `a.co.is-taken` whose `<small>` reads `On hold — {HoldReason}`, which is the accurate sentence (`templates.go:585`).
- **16.b — release the hold first.** UC-C-15 restores takeability.

**Exception flows**

- **16.e1 — no CSRF.** Refused before the gate.
- **16.e2 — no `reservation:reserve`.** `403` before the store is reached.

**Business rules**

- The takeable set excludes held rows at the SQL level in both the count and the row query (`store.go:1113`, `store.go:1152-1161`), and again in the reservation's own `WHERE` (`store.go:738`) — belt, braces and a third strap.
- The queue never offers a control the store would refuse (`linking.go:22-25`).

**Data touched** — reads `payment_requests`, `users`; writes nothing.

**Non-functional / UX notes** — a test should assert both the 409 **and** that `processing_by` is still NULL; the status alone does not prove the state survived.

**Open questions** — the conflict screen's copy is wrong for this path: nobody took the request, it is on hold. `reservationConflict` is reached for three distinct causes (taken, on hold, not approved) and says "took this request before you" for all three (`linking.go:55-71`). Recorded as suspected defect SD-04.

---

#### UC-C-17 — Release your own reservation

| | |
|---|---|
| **Goal** | "I cannot finish this today — put it back so a colleague can pay it." |
| **Primary actor** | Accountant (the holder) |
| **Supporting actors** | Requester, Manager (both told why the money stopped moving) |
| **Scope / level** | system · user-goal |
| **Trigger** | **Release** on the entry screen, **Cancel and release** on the action bar, or **Release it** on the stale nudge |
| **Route(s)** | `GET /requests/{id}/reservation` (RT-12), `POST /requests/{id}/release` → `303 /accounts-queue` (RT-13) |
| **Permission gate** | RT-12 session only, plus ownership; RT-13 `reservation:release`, `withCSRF` |
| **Coverage IDs** | S6, S7 |
| **Priority** | critical |

**Preconditions**

1. `status='processing'` and `processing_by` is not NULL.
2. The caller either holds the reservation and holds `reservation:release`, or holds `reservation:reassign` (`linking.go:531-538`).
3. `reason` is non-blank (`store.go:765-768`).
4. `confirm` posts as `on` (`store.go:762-764`).

**Postconditions (success)**

1. `status='approved'`, `processing_by=NULL`, `processing_at=NULL`, `updated_at` bumped (`store.go:788`).
2. One `audit_log` row: `action='release'`, `summary='Released reservation: {reason}'`, `before_json` carrying the previous `processing_by` (`store.go:791`).
3. `303` to `/accounts-queue` (`linking.go:634`).
4. The request is takeable by anyone again.

**Postconditions (failure)** — `status`, `processing_by`, `processing_at` unchanged; no audit row.

**Main success scenario**

1. Accountant presses **Release** in the entry screen's `.reserve-bar`, or **Cancel and release** in its action bar — both `<a>` to `/requests/{id}/reservation` (`templates.go:372`, `templates.go:444`).
2. System loads the request through `loadViewableRequest`, confirms `status=='processing'`, resolves `mine`, `mayRelease`, `mayReassign`, and refuses unless `(mine && mayRelease) || mayReassign` (`linking.go:521-538`).
3. System titles the tab **Release reservation** when the reader may release, **Reassign reservation** when they may only reassign (`linking.go:553-558`).
4. System renders the `.page-banner`: eyebrow `Accounts · reservation`, `<h1>Release or reassign this request</h1>`, `.sub` `{Number} · {Vendor} · {₹approved}` (`templates.go:969-975`).
5. System renders `.reserve-bar` reading **Reserved by you** (or `Reserved by {name}`) and `.rb-meta` `since {HH:MM} · {elapsed}` (`templates.go:977-981`).
6. System renders the `.banner.bad` **Confirm no payment has been started** / `Releasing puts the request back in the open queue and anyone in Accounts can take it. If you have already initiated a transfer in the bank portal, do not release — finish recording it.` (`templates.go:983-989`).
7. System renders `form#reservation-form` whose own `action` is the branch this reader may actually take (`/release` when they hold release, else `/reassign`) (`templates.go:991-995`).
8. Accountant leaves the `.choice` radio `action=release` checked — **Release it** / `Back to Approved — awaiting payment. Anyone in Accounts can pick it up.` (`templates.go:1000-1005`).
9. Accountant fills `#reason`, label **Reason** (required), placeholder `Recorded in the history and visible to everyone who can see this request.` (`templates.go:1031-1034`).
10. Accountant ticks `label.checkline > input[type=checkbox][name=confirm][value=on][required]` reading `I confirm no payment has been initiated for this request` (`templates.go:1036`).
11. Accountant presses **Release reservation** — `<button class="btn danger" type="submit" data-when="action:release" style="order:2" formaction="/requests/{id}/release">` (`templates.go:1052`).
12. System calls `ReleaseRequest(actor, id, reason, confirm=="on", Can(reservation,reassign))` (`linking.go:629-630`).
13. Store rejects an unconfirmed or reasonless release, then re-reads `status` and `processing_by`, allows the assignee (or an authorized caller), performs the `UPDATE … WHERE id=? AND status='processing'`, writes the audit row and commits (`store.go:761-794`).
14. System redirects `303` to `/accounts-queue`, where the row is takeable again.

**Alternate flows**

- **17.a (step 11) — keep working instead.** The action bar's first control is `<a class="btn outline" href="/payments/new?request={id}">Keep working on it</a>` when the reader holds the reservation **and** `payment:create`; otherwise `Back to the queue` (`templates.go:1044`).
- **17.b (step 8) — reassign instead.** UC-C-18.
- **17.c (step 5) — the history.** Below the form, an `ol.thread` filtered by `reservationTrail` to the three actions this screen is a history of — `process`, `release`, `reassign` (`linking.go:603-612`, `templates.go:1057-1068`). The filter lives in Go, not the template, because `{{range}}…{{else}}` fires on an empty slice, not on a filter that matched nothing (`linking.go:598-602`).
- **17.d — from the stale nudge.** **Release it** / `Requires a reason and confirming no payment was initiated`, offered when the reader holds `reservation:release` **and** either holds the reservation or holds reassign (`templates.go:1110-1115`).

**Exception flows**

- **17.e1 — no confirmation.** `ErrValidation` "confirm that no payment was initiated before releasing" → `400` error page (`store.go:762-764`). This is the S7 rule: cancel/switch releases **only** after explicit confirmation.
- **17.e2 — blank reason.** `ErrValidation` "a reason is required so the requester and approver know why" → `400` (`store.go:765-768`).
- **17.e3 — not `processing`.** `ErrForbidden` "only a processing request can be released" → `403` (`store.go:782-784`).
- **17.e4 — not the assignee and not authorized.** `ErrForbidden` "only the assignee may release this request" → `403` (`store.go:785-787`). See UC-C-19.
- **17.e5 — reading the screen with neither standing.** `403` `Only the person holding this reservation can release it.` (`linking.go:534-538`).
- **17.e6 — the request is not reserved by anyone.** `409` `This request is not reserved by anyone.` (`linking.go:527-530`).
- **17.e7 — outside the caller's request scope.** `404` from `loadViewableRequest` — holding a reservation verb somewhere is not permission to read this request. **`403` until Wave 3 (F-G-002, commit `1fac147`)**; the refusal is now indistinguishable from a missing row (`linking.go:517-520`, `linking.go:620-622`, `requests.go:295-308`).

**Business rules**

- **S6:** there is no auto-release; this is the only path back to `approved` besides a reassignment (`store.go:755-760`, `models.go:408-410`).
- **S7:** `confirmed` must be true, and it is enforced in the **store**, so the same rule holds whether the release arrives from this screen or from a script (`linking.go:614-617`).
- `authorized` is resolved from `reservation:reassign`, because releasing work that is not yours is taking it off somebody — the verb that grants exactly that (`linking.go:617-618`, `linking.go:630`).
- The route gate is `reservation:release` and the screen's gate is ownership, deliberately different: gating the route on one verb would 403 exactly the reader the queue and the conflict screen link here (`app.go:455-464`).
- **Release is first in the DOM and last on the screen.** HTML makes the first submit in tree order the form's default button and `hidden` does not exempt it; with Reassign first, pressing Enter on the checked "Release it" radio posted to `/reassign` with no target and lost the typed reason on a 400. `style="order:2"` puts the danger button back on the right (`templates.go:1045-1052`).

**Data touched** — writes `payment_requests.status/processing_by/processing_at/updated_at`, `audit_log`.

**Non-functional / UX notes** — `#to_user_id` uses `aria-required`, not `required`, because a required control inside a `data-when`-hidden field makes the whole form unsubmittable in Chrome (`templates.go:1022-1025`). The confirm checkbox *is* `required` because it is never hidden. The history reads oldest-first, unlike the mockup — one product, one direction (`templates.go:963-966`).

**Open questions** — none. At the time of this audit the action-bar note claimed `The requester and the approver are both notified.` (`templates.go:1042`) while no notification event fired on release — `requestRelease` called no `a.fire` and `events.go` had no release event — recorded as suspected defect SD-02. **Fixed (F-F-06):** `notify.EventReservationReleased` was added by migration v9 (Wave 2, commit `25411b8`) and wired in Wave 4; `requestRelease` now fires it, so the screen's promise is true and this use case also writes `notifications` (`events.go:45-48`, call site `linking.go:712`).

---

#### UC-C-18 — Reassign someone else's reservation

| | |
|---|---|
| **Goal** | "This has sat on a colleague's desk too long — hand it to somebody who will finish it, without letting a third party slip in." |
| **Primary actor** | Administrator (holder of `reservation:reassign`) |
| **Supporting actors** | The current holder, the new holder |
| **Scope / level** | system · user-goal |
| **Trigger** | **Reassign** on a `processing` queue row, the conflict screen's **Ask for it to be reassigned**, or the stale nudge's **Hand it to a colleague** |
| **Route(s)** | RT-12, then `POST /requests/{id}/reassign` → `303 /requests/{id}` (RT-14) |
| **Permission gate** | `reservation:reassign`, `withCSRF`; store fails closed without `authorized` |
| **Coverage IDs** | S6, S7 |
| **Priority** | high |

**Preconditions**

1. `status='processing'` and `processing_by` is not NULL.
2. The caller holds `reservation:reassign` — **not** the seeded Accounts role (AF-02).
3. `to_user_id` names an **active** user who is **not** the current holder and who **can work the Accounts queue** (holds `payment:process`) (`linking.go:660-668`, `store.go:826-839`, `linking.go:591-596`).
4. `confirm` posts as `on` (`linking.go:653-656`).
5. `reason` is non-blank (`store.go:806-809`).

**Postconditions (success)**

1. `processing_by = to_user_id`, `processing_at = CURRENT_TIMESTAMP`, `updated_at` bumped. **`status` stays `processing`** — the request never returns to the open queue, so no third party can slip in between (`store.go:840-842`, `store.go:797-800`).
2. One `audit_log` row: `action='reassign'`, `summary='Reassigned reservation to {name}: {reason}'`, with `before_json`/`after_json` carrying the old and new `processing_by` (`store.go:851-855`).
3. `303` to `/requests/{id}` (`linking.go:674`).

**Postconditions (failure)** — `processing_by` unchanged; no audit row; the original holder keeps the work.

**Main success scenario**

1. Administrator opens `/accounts-queue?tab=processing` and presses **Reassign** on a row held by somebody else — rendered only when `ProcessingBy` is set and the reader holds `reservation:reassign` (`templates.go:750`).
2. System renders the reservation screen; because `mayReassign` is true it also loads `ListUsers` and filters it through `reassignCandidates` (`linking.go:539-547`).
3. Administrator selects the second `.choice` radio — **Reassign to someone else** / `Stays in Processing, assigned to the person you choose.` (`templates.go:1006-1011`).
4. The `[data-when="action:reassign"]` field un-hides `#to_user_id`, label **Reassign to**, options `Choose…` plus each candidate's name (`templates.go:1015-1030`).
5. Administrator picks a name, fills **Reason**, ticks the confirm box.
6. Administrator presses **Reassign** — `<button class="btn outline" type="submit" data-when="action:reassign" formaction="/requests/{id}/reassign">` (`templates.go:1053`).
7. System refuses a zero target, refuses a missing confirmation, loads the target user, and refuses a target who cannot work the queue (`linking.go:648-668`).
8. `ReassignReservation` fails closed without `authorized`, requires a reason, re-reads `status`/`processing_by`, refuses handing it to the current holder, refuses a deactivated target, then performs `UPDATE … WHERE id=? AND status='processing' AND processing_by=?` — an optimistic guard against the reservation moving while the administrator was deciding (`store.go:802-850`).
9. System writes the audit row, commits, and redirects to the request.

**Alternate flows**

- **18.a (step 1) — from the conflict screen.** The loser's `.a-list` entry **Ask for it to be reassigned** links here, gated on the same verb (`templates.go:794`).
- **18.b (step 1) — from the stale nudge.** **Hand it to a colleague** / `Stays reserved, assigned to them, with your reason recorded`, plus an `.action-bar` **Reassign with reason** and the note `Administrators can force a reassignment.` (`templates.go:1116`, `templates.go:1138-1144`).
- **18.c (step 3) — a reader who holds only reassign.** The release radio and button are not rendered; the reassign radio is pre-checked and its field is **not** hidden, because without the script the reader would face a form whose only field was invisible (`templates.go:1006-1020`, `templates.go:1053`).
- **18.d (step 2) — an administrator taking it over.** The candidate filter excludes the **holder**, not the caller, so an administrator clearing a colleague's stale reservation may legitimately name themselves (`linking.go:571-574`).

**Exception flows**

- **18.e1 — no target.** `400` `Choose who should take this reservation.` (`linking.go:649-652`).
- **18.e2 — no confirmation.** `400` `Confirm that no payment has been initiated.` — demanded in the handler because `ReassignReservation` has no `confirmed` parameter; the screen asks the same question for both branches, so the same answer is required for both (`linking.go:637-641`, `linking.go:653-656`).
- **18.e3 — target cannot work the queue.** `400` `That person cannot work the Accounts queue.` A reservation parked on somebody without `payment:process` is a request nobody can move: the new holder gets 403 on the queue and on the reservation screen, and anybody else re-reserving gets 409 (`linking.go:576-579`, `linking.go:665-668`).
- **18.e4 — target is the current holder.** `ErrValidation` "that person already holds this reservation" → `400` (`store.go:826-828`).
- **18.e5 — deactivated target.** `ErrValidation` "that user is deactivated" → `400` (`store.go:837-839`).
- **18.e6 — blank reason.** `ErrValidation` "a reason is required when a reservation changes hands" → `400` (`store.go:806-809`).
- **18.e7 — no `reservation:reassign`.** `403` from the middleware; and even if it were bypassed, `ReassignReservation` returns `ErrForbidden` "reassigning someone else's reservation needs the reassign permission" (`store.go:803-805`).
- **18.e8 — the reservation moved mid-decision.** `RowsAffected()==0` → `ErrForbidden` "the reservation changed while you were deciding" → `403` (`store.go:846-850`).
- **18.e9 — not `processing`, or unclaimed.** `ErrForbidden` "only a reserved request can be reassigned" → `403` (`store.go:823-825`).

**Business rules**

- **G11:** a reassignment never returns the request to the open queue; only `processing_by` moves (`store.go:797-800`).
- The select only offers people who can work the queue, and the handler is what makes that a **rule** rather than a courtesy (`linking.go:657-659`).
- Every field the `[data-when]` reveal implies is re-checked server-side, so a hand-rolled POST cannot reassign without a target any more than it can release without a reason (`linking.go:505-508`, `templates.go:956-958`).

**Data touched** — writes `payment_requests.processing_by/processing_at/updated_at`, `audit_log`; reads `users`, `user_roles`, `role_permissions`.

**Non-functional / UX notes** — `reassignCandidates` runs one permission query per user; fine at this user count, but the fix if the table grows is a batched permission read in the store, not a looser filter (`linking.go:571-579`, `PROGRESS.md:393-395`).

**Open questions** — none, and for the same reason as UC-C-17. At the time of this audit the screen promised notification and none was sent (SD-02). **Fixed (F-F-06):** `notify.EventReservationReassigned` was added by migration v9 (Wave 2, commit `25411b8`) and wired in Wave 4; `requestReassign` now fires it and addresses the new assignee personally, since they are the one now expected to pay it (`events.go:49-52`, call site `linking.go:755`). This use case therefore also writes `notifications`.

---

#### UC-C-19 — Release a reservation you do not hold, without `reservation:reassign`

| | |
|---|---|
| **Goal** | (adversarial) "Can I take a request off a colleague using only my own release grant?" |
| **Primary actor** | Second accountant |
| **Supporting actors** | The holder |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | A hand-rolled `POST /requests/{id}/release` |
| **Route(s)** | RT-12 (403), RT-13 (403) |
| **Permission gate** | `reservation:release` **passes**; the store's assignee test refuses |
| **Coverage IDs** | S6 |
| **Priority** | critical |

**Preconditions**

1. `status='processing'`, `processing_by = holder.id`.
2. The caller holds `reservation:release` but **not** `reservation:reassign` — exactly the seeded Accounts role (AF-02).
3. `caller.id != holder.id`.
4. `reason` and `confirm=on` are both supplied, so the refusal under test is the authority test and not a validation.

**Postconditions (success — correctly refused)**

1. HTTP **403**.
2. `status` still `processing`, `processing_by` still the holder.
3. No `release` audit row.

**Postconditions (failure)** — a release by a non-holder without reassign would let one accountant strip another's reservation, which is the single thing a reservation exists to prevent.

**Main success scenario**

1. Second accountant opens `/requests/{id}/reservation`.
2. `reservationForm` computes `mine=false`, `mayRelease=true`, `mayReassign=false`, so `!(mine && mayRelease) && !mayReassign` is true and it answers **403** `Only the person holding this reservation can release it.` (`linking.go:531-538`).
3. Second accountant posts to `/requests/{id}/release` anyway, with a reason and a confirmation.
4. `RequirePermission("reservation","release")` passes.
5. `ReleaseRequest` validates `confirmed` and `reason`, reads `status` and `processing_by`, and evaluates `!authorized && processing_by != actor.ID` — `authorized` is `Can(reservation,reassign)` = false — so it returns `ErrForbidden` "only the assignee may release this request" (`linking.go:630`, `store.go:785-787`).
6. System answers **403**; the transaction rolls back.

**Alternate flows**

- **19.a — the same caller, their own reservation.** Succeeds: UC-C-17.
- **19.b — an administrator, somebody else's reservation.** Succeeds, because `authorized` is true. The stale-nudge screen offers exactly that choice rather than making the administrator infer it from the reassign entry that shares the href (`templates.go:1110-1115`).
- **19.c — the queue's affordance.** A `processing` row held by somebody else offers **View** to an accountant and **Reassign** only to a reassign-holder; there is no release control at all (`templates.go:750-751`).

**Exception flows**

- **19.e1 — validation short-circuits first.** `ReleaseRequest` checks `confirmed` and `reason` **before** it reads the row, so a test omitting either gets a `400` about the missing field and never exercises the authority rule (`store.go:762-768`). Supply both.

**Business rules**

- `authorized` for a release is `reservation:reassign`, not `reservation:release`: taking work off somebody is the reassign verb (`linking.go:617-618`).
- The store fails closed — the handler resolves the grant and passes it, and the store never re-derives it (`store.go:761`, `store.go:785-787`).

**Data touched** — reads `payment_requests`; writes nothing.

**Non-functional / UX notes** — assert the 403 on **both** RT-12 and RT-13: the first proves the screen is not shown, the second proves the write is refused.

**Open questions** — none.

---

#### UC-C-20 — Act on a reservation that has been open too long

| | |
|---|---|
| **Goal** | "This has been sitting on my desk for a day — remind me, and show me the four things I can do." |
| **Primary actor** | Accountant (the holder) |
| **Supporting actors** | Administrator, Scheduler |
| **Scope / level** | system · user-goal |
| **Trigger** | **Resume** on a stale `processing` queue row, or the queue's stale banner |
| **Route(s)** | `GET /requests/{id}/reservation/stale` (RT-15) |
| **Permission gate** | `payment:process`; plus `loadViewableRequest` row scope |
| **Coverage IDs** | S8, Q6, S6 |
| **Priority** | medium |

**Preconditions**

1. `status='processing'` and `processing_by` is not NULL.
2. `time.Since(processing_at) >= store.StaleReservation` (24 h) for the queue to route here (`linking.go:798-800`, AF-08).
3. The caller holds `payment:process` and can read the request.

**Postconditions (success)** — a `200` page titled `Reserved too long`; **nothing written and nothing released** (`linking.go:722-728`).

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. Accountant opens `/accounts-queue?tab=processing`.
2. System renders their own row's status pill as `<span class="pill processing">Reserved by you · {reservedLabel}</span>`, where `reservedLabel` prints the clock time while the reservation is same-day and the elapsed hours (`26 h`) once it is older (`templates.go:737`, `linking.go:780-791`).
3. System renders the action cell as **Resume**, whose href is `/requests/{id}/reservation/stale` when `stale .ProcessingAt` and `/payments/new?request={id}` otherwise (`templates.go:743-749`).
4. Accountant presses **Resume** and lands on the nudge screen.
5. System confirms `status=='processing'`, loads the merged trail, and renders `reservation_stale` (`linking.go:730-751`).
6. Accountant reads the `.banner.warn` `This has been reserved by you for {elapsed}` and `A reminder went out at the one-day mark. Nothing is released automatically — a transfer may already be under way, so only you or an authorised colleague can act.` (`templates.go:1087-1094`).
7. Accountant reads the `.req-head` — number, approved amount, `h1` `{Vendor} — {ShortTitle}`, `.rh-meta` naming the requester, project/head and needed-by, and a `.rh-status` with `<span class="pill processing">Processing — {name}</span>` plus a `.waiting` line (`templates.go:1096-1104`).
8. Accountant reads the **Pick one** card — an `.a-list` of up to four choices with **no default** (`templates.go:1106-1123`).
9. Accountant presses **Carry on and record the payment** / `Opens the payment form with your reservation intact`, gated on holding the reservation **and** `payment:create` (`templates.go:1109`).
10. System serves `/payments/new?request={id}` with the reservation intact (UC-C-06).

**Alternate flows**

- **20.a (step 8) — release it.** UC-C-17 alternate 17.d.
- **20.b (step 8) — hand it over.** UC-C-18 alternate 18.b.
- **20.c (step 8) — put it on hold.** UC-C-13 alternate 13.c.
- **20.d (step 6) — an administrator reading a colleague's stale reservation.** The banner names the holder instead of "you", and the `.action-bar` appears with **Reassign with reason** (`templates.go:1090`, `templates.go:1138-1144`).
- **20.e (step 2) — the queue banner.** With `Counts.StaleReservations > 0`, `.banner.brand` reads `You have {n} requests reserved` / `{m} of them have been open for more than a day. Finish them or release them so someone else can.` with a **Review** link. The banner is an aggregate and cannot say **which** row it means; the row's own `stale` predicate can, which is why the link goes to the tab and the row goes to this screen (`templates.go:699-708`, `linking.go:793-797`).

**Exception flows**

- **20.e1 — not `processing`.** `409` `This request is not reserved by anyone.` — off that status every sentence on the screen is false (`linking.go:735-740`).
- **20.e2 — no `payment:process`.** `403` from the middleware (`app.go:470`).
- **20.e3 — outside scope.** `404` from `loadViewableRequest`: holding `payment:process` is permission to work the queue, not permission to read a request outside the caller's data scope. **`403` until Wave 3 (F-G-002, commit `1fac147`)** (`linking.go:726-729`, `requests.go:295-308`).
- **20.e4 — the "Who has been told" section is empty of reminders.** The `ol.thread` renders `.Audit`, i.e. the request's whole merged audit trail; the reminder writes **no audit row**, so the reminder itself never appears there. See open questions.

**Business rules**

- Nothing is ever released automatically: an automatic release would let a second accountant pay an invoice already moving through a bank portal (`linking.go:722-726`, `templates.go:1074-1078`).
- Every choice is gated server-side on the verb that choice actually needs; a choice the reader could not take is **not shown**, not greyed out (`templates.go:1080-1084`).
- The queue's stale threshold is the fixed 24 h `store.StaleReservation`; the **scheduler's** threshold is the admin-set `reminder_stale_days` (default 1). They are two different numbers that can be configured apart (AF-08).

**Data touched** — reads `payment_requests`, `audit_log`, `users`; writes nothing.

**Non-functional / UX notes** — the whole page is one `.a-list` of choices, so at 390 px each is a full-width row with an `.al-amt` arrow. `ux.spec.ts` does not reach this screen (it is id-parameterised), so its accessibility must be asserted explicitly.

**Open questions** — the banner asserts `A reminder went out at the one-day mark` unconditionally. Whether one did depends on the scheduler having ticked and on `reminder_stale_days`; and `MarkReminderSent` writes no audit row, so the "Who has been told" thread can never evidence it. `PROGRESS.md:390-391` recorded this as "no writer before Phase 5"; Phase 5 added the notification but **not** the trail line. Recorded as suspected defect SD-01.

---

#### UC-C-21 — Read a recorded payment

| | |
|---|---|
| **Goal** | "Show me what was approved, what left the bank, the proof, and the whole story — and offer me nothing that would change it." |
| **Primary actor** | Accountant |
| **Supporting actors** | Requester, Manager |
| **Scope / level** | system · user-goal |
| **Trigger** | Landing after UC-C-08/09, the ledger's **View**, or the request's **View the payment** |
| **Route(s)** | `GET /payments/{id}` (RT-07) |
| **Permission gate** | `payment:view`; **plus** the request's own row scope when the payment is linked |
| **Coverage IDs** | S12, Q4, X6 |
| **Priority** | critical |

**Preconditions**

1. The payment exists.
2. The caller holds `payment:view`.
3. For a linked payment, `canViewRequest(scope, user, request)` is true (`app.go:757-762`).

**Postconditions (success)** — a `200` page titled `Payment · {Number}`; nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. Accountant lands on `/payments/{id}` after confirming.
2. System loads the payment, its attachments, the request behind it, the merged two-entity trail and the request's own attachments (`app.go:737-782`).
3. System renders the `.banner.good` **Payment saved. The request is completed.** (or `…is with the manager.` for `partial_review`) and `{Requester} and {Manager} have been notified. This payment can no longer be edited or cancelled.` (`templates.go:243-249`).
4. System renders `.req-head`: `.rh-no` `PAY-{id} · from {Number}`, `.rh-amt` the amount, `h1` the payee, `.rh-meta` `{Project} / {Head} · paid {date}`, and `.rh-status` carrying the request's status pill and the shared `waitingOn` line (`templates.go:251-263`).
5. System renders the `.compare`: `Approved`, `Paid`, then `.cmp-row.match` `Difference · confirmed settled by Accounts` for a settled payment or `.cmp-row.diff` `Still owed to the payee` for a partial one (`templates.go:265-273`).
6. System renders the **Payment** card headed `<span class="pill neutral no-dot">Read-only</span>`, with `dl.dl` rows `Paid on`, `Mode`, `Reference`, `Recorded by`, `Payee`, `Invoice`, `Settlement`, `Partial reason`, `Processing note` (`templates.go:275-288`).
7. System renders **Proof** — one `.file-row` per payment attachment, plus one per **request** attachment sub-labelled `Invoice from the request`, each with a **Download** link when the reader holds `attachment:view`. **The two links go to two different routes since Wave 3 (F-A-05/F-B-09, commit `1fac147`):** a payment attachment to `GET /attachments/{id}` (`templates.go:304`), a request document to `GET /requests/{requestID}/attachments/{attachmentID}` (`templates.go:319`). At audit time both rendered through `/attachments/{id}`, which reads `payment_attachments` — and both id sequences start at 1, so Download on your own invoice served a stranger's bank advice. Both handlers now check ownership and refuse with **404** (`app.go:1091-1107`, `app.go:1115-1146`, `app.go:1040-1048`). See REPAIR-LOG.md Wave 3.
8. System renders **Full trail, request to payment** — an `ol.thread` over the merged audit, oldest first, each line decorated by `trailAction`/`auditTone`/`auditGlyph`/`auditPhrase` (`templates.go:313-322`, `linking.go:997-1020`).
9. System renders the `.action-bar` with the note `Refunds and reversals are outside this version.`, an **Open the request** link and **Back to ledger** — and **no Edit and no Void** (`templates.go:324-329`).

**Alternate flows**

- **21.a — the `Settlement` row's words.** `Partial — a balance is still owed` or `Fully settled — deductions handled outside this system` (`templates.go:284`).
- **21.b — the payee links to the vendor.** Only when the request carries a `vendor_id` **and** the reader holds `vendor:view` (`templates.go:282`).
- **21.c — a historical, request-less payment.** `.Request2.ID` is zero, so the `{{else}}` branch renders `payment_detail_historical`: the pre-Phase-3 screen, verbatim, with **Edit**, an upload form and a **Void Payment** danger zone (`templates.go:330-345`). See UC-C-24.
- **21.d — the requester's reading.** They read the outcome on the request instead; UC-C-29.

**Exception flows**

- **21.e1 — no `payment:view`.** `403` from the middleware.
- **21.e2 — the caller may see payments but not this request.** `403` `You cannot see the request behind this payment.` — half this screen is the request, so `payment:view` must not become a way around Q5/R6 (`app.go:755-762`).
- **21.e3 — unknown id.** `404` `The requested record was not found.`
- **21.e4 — no attachments on either side.** The whole **Proof** section is omitted (`templates.go:290`, `templates.go:311`).
- **21.e5 — empty trail.** `<li>` with `.tl-body.muted` `No history recorded.` (`templates.go:321`).
- **21.e6 — a **Download** link on a payment the caller cannot see.** `404` `The requested attachment was not found.` **Added in Wave 3 (F-A-01/F-B-11, commit `1fac147`).** At the time of this audit `GET /attachments/{id}` was gated on `attachment:view` alone and streamed any file to any holder of that verb, which the seeded Requester role holds. `attachmentDownload` now resolves the attachment together with its payment and applies `canReadPayment` — the same test 21.e2 applies to the screen — before opening a byte (`app.go:459`, handler `app.go:1091`, refusal at `app.go:1044-1048`).
- **21.e7 — a request document downloaded through the payment route.** `404`. **Added in Wave 3 (F-A-05/F-B-09).** The two id sequences are independent, so one shared route handed id `7` from whichever table it read first; the routes are now split. `GET /attachments/{id}` serves payment documents only, and the **Proof** list's `Invoice from the request` rows link to the request's own route, `/requests/{requestID}/attachments/{attachmentID}` (`app.go:460`, handler `app.go:1115`, link at `templates.go:319`).

**Business rules**

- **S12:** a linked payment offers no mutation control anywhere, because the store refuses to edit or void one and a control the route would reject is a lie (`templates.go:230-238`, `store.go:609-611`, `store.go:649-651`).
- The trail spans two entity types, merged and ordered oldest-first because a trail is a story and a story is told forwards (`linking.go:997-1000`).
- `trailAction` folds the entity into the action once — `create`→`record_payment`, `update`→`amend_payment`, `attach`→`attach_payment` — so "create" on a payment is a different sentence from the same word on a request (`linking.go:823-842`).
- The proof list is both halves: the bank advice Accounts uploaded and the invoice the request came in with, so nobody has to open the request to check what was billed (`templates.go:300-302`). **Since Wave 3 the two halves are served by two routes** — payment documents from `GET /attachments/{id}`, request documents from `GET /requests/{id}/attachments/{attachmentID}` — because one URL over two independent id sequences handed one reader another reader's bank advice, and because `attachment:view` alone is not permission to read a particular file (F-A-01/F-A-05, `app.go:459-460`, `templates.go:304`, `templates.go:319`). See 21.e6/21.e7.

**Data touched** — reads `payments`, `payment_attachments`, `payment_requests`, `request_attachments`, `audit_log` (both entity types), `users`, `projects`, `heads`, `vendors`.

**Non-functional / UX notes** — `ISS-025` is the canonical assertion set: `.card-head .pill` reading `Read-only` visible; zero `Upload` buttons; zero `Void` buttons; zero `Edit` links; zero `input[name="attachment"]` (`regression-issues.spec.ts:497-513`). `ISS-023` proves the proof list's **Download** link streams real bytes (`regression-issues.spec.ts:449-465`).

**Open questions** — the mockup's `Bank` row is not built and needs a spec decision, not an implementation pass: it would put `vendor_bank`-restricted data on a screen no spec authorises for it and needs an account-masking helper with no precedent (AF-16).

---

#### UC-C-22 — Browse the payments ledger

| | |
|---|---|
| **Goal** | "Show me the month's payments, and let me tell a linked one from a historical one." |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar "Payments ledger" |
| **Route(s)** | `GET /payments` (RT-08) |
| **Permission gate** | `payment:view` |
| **Coverage IDs** | X6, S12 |
| **Priority** | high |

**Preconditions**

1. The caller holds `payment:view`.
2. At least one payment exists in the chosen month.

**Postconditions (success)** — a `200` page titled `Payments`; nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. Accountant opens `/payments`; `month` defaults to the current month and `status` to `active` (`app.go:670-671`).
2. System sums the non-voided amounts into `PaymentTotal` and reports the month's lock state (`app.go:678-684`).
3. System renders the `.page-banner` `<h1>Payments</h1>` with `.sub` `{month} · {n} entries · active total {₹}` (`templates.go:218`).
4. System renders the `.toolbar`: `Month` (`type="month"`), `Status` (`Active` / `Removed / voided` / `All`), `Search`, a **Filter** submit and a **Reset** link (`templates.go:219-225`).
5. System renders one `table.t-cards` in a `.table-wrap.payments-table[tabindex=0][role=region]`, head `Date · Project / Head · Amount · Payee · Mode · Reference · Entered by · Status · Actions` (`templates.go:226`).
6. Accountant reads a **linked** payment's row: a **View** link and nothing else (`templates.go:226`, gate `not .RequestID`).
7. Accountant reads a **historical** payment's row: **View**, **Edit**, and a `details.inline-danger` **Remove** disclosure whose form posts `/payments/{id}/void` with a required `Reason` — all conditional on `payment:edit`, `not .RequestID`, the month being unlocked and the row not already voided.

**Alternate flows**

- **22.a (step 6) — a voided row.** `<tr class="is-voided">`, `<span class="pill bad">Removed</span>` and the void reason in place of the action cluster.
- **22.b (step 6) — a locked month.** `<small class="muted">Locked</small>` and the banner's `+ Add payment` replaced by `<span class="pill warn">Locked</span>` (`templates.go:218`).
- **22.c (step 4) — search escaping.** `ListPayments` escapes `\`, `%`, `_` exactly as the queue does; `ISS-026` proves `%_` matches literally (`store.go:1278-1284`, `regression-issues.spec.ts:527-560`).

**Exception flows**

- **22.e1 — no rows.** `<td colspan="9" class="empty">No payments match these filters. <a href="/payments/new">Record a payment</a></td>` (`templates.go:226`).
- **22.e2 — no `payment:view`.** `403`; the nav entry is hidden too (`nav.go:67`).

**Business rules**

- `request_id` travels with every ledger row, precisely so the screen can tell a linked payment from a historical one; a screen that cannot see the difference offers an Edit button the store would refuse (`store.go:1296-1302`).
- **X6:** historical rows keep `request_id IS NULL` and remain editable and voidable exactly as before (`phase-3 spec:91`, `store.go:609-611`).
- The **only** creator of a linked payment is `POST /payments`; nothing in the product creates a historical one, so a fresh install can produce only linked payments (`regression-issues.spec.ts:487-495`).

**Data touched** — reads `payments`, `heads`, `projects`, `users`, `month_locks`.

**Non-functional / UX notes** — a multi-part cell value needs one wrapper element: a restacked `<td>` is a flex row, and a cell holding several children spreads them as separate flex items that overlap and swallow taps. The Reference cell wraps its two parts in a single `<span>` for exactly that reason (`templates.go:226`, `PROGRESS.md:106-110`).

**Open questions** — the banner's `+ Add payment` links `/payments/new?month={month}`, but `paymentForm` ignores `month` entirely (`app.go:655-667`). Harmless, but the parameter is dead.

---

#### UC-C-23 — Try to edit or void a linked payment

| | |
|---|---|
| **Goal** | (adversarial) "The request is closed and the requester was told what was paid — can I rewrite it?" |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | `GET /payments/{id}/edit`, `POST /payments/{id}/edit`, `POST /payments/{id}/void` on a linked payment |
| **Route(s)** | RT-09, RT-10, RT-11 |
| **Permission gate** | `payment:edit` / `payment:void` **pass**; the store refuses |
| **Coverage IDs** | S12 |
| **Priority** | critical |

**Preconditions**

1. A payment with `request_id` not NULL.
2. The caller holds `payment:edit` and `payment:void` (the seeded Accounts role holds both, `migrations.go:384`).
3. A valid CSRF token for the POSTs.

**Postconditions (success — correctly refused)**

1. `GET /payments/{id}/edit` → **303** to `/payments/{id}`, no form served (`app.go:803-806`).
2. `POST /payments/{id}/edit` → **400**, body containing `cannot be edited` (`store.go:609-611`).
3. `POST /payments/{id}/void` → **400**, body containing `cannot be voided` (`store.go:649-651`).
4. Every column of the `payments` row is unchanged; `voided_at` stays NULL; no `update` or `void` audit row.
5. The request's status is unchanged.

**Postconditions (failure)** — an edited amount would silently rewrite what the requester and approver were told was paid; a void would leave the request `completed` with nothing paid.

**Main success scenario**

1. Accountant opens `/payments/{id}` and confirms no Edit link and no Void button exist (UC-C-21).
2. Accountant opens `/payments/{id}/edit` by URL. `paymentEditForm` sees `p.RequestID != nil` and redirects `303` to `/payments/{id}` — serving the form would let somebody fill in a screen whose Save can only ever fail (`app.go:799-806`).
3. Accountant posts to `/payments/{id}/edit` with a changed amount and a valid token.
4. `UpdatePaymentWithAttachment` reads the row in the transaction, sees `before.RequestID != nil`, and returns `ErrValidation` "a payment linked to a request cannot be edited" (`store.go:607-611`).
5. `paymentEdit` maps `ErrValidation` to `400` and, because the status is below 500, re-renders `payment_edit_form` with the message (`app.go:826-842`).
6. Accountant posts to `/payments/{id}/void` with a reason.
7. `VoidPayment` reads the row, sees `before.RequestID != nil`, and returns `ErrValidation` "a payment linked to a request cannot be voided" (`store.go:648-651`).
8. `paymentVoid` sends it to `respondStoreError` → `400` error page (`app.go:849-853`).

**Alternate flows**

- **23.a — the ledger row.** Offers neither control (UC-C-22 step 6); `ISS-025` asserts zero `Edit` links and zero `details.inline-danger` on the row (`regression-issues.spec.ts:504-507`).
- **23.b — attaching more proof after the fact.** `POST /payments/{id}/attachments` is still routed and gated on `attachment:create` (`app.go:452`), and no linked-payment screen renders the form. **Since Wave 1 the store refuses it too:** `AddAttachment` returns a validation error for a payment whose `request_id` is set, matching the edit and void guards (F-D-08, `store.go:1580-1587`), and Wave 3 added an ownership check on the same path (F-A-03, `app.go:1050-1070`). See open questions.

**Exception flows**

- **23.e1 — a voided linked payment.** Unreachable: the void is refused first.
- **23.e2 — no token.** Refused before the gate; the token must be read from the `fervid_csrf` cookie because the detail screen carries no form (`regression-issues.spec.ts:519-525`).

**Business rules**

- **S12:** the guard is `before.RequestID != nil` in both writers, leaving historical rows editable and voidable as before (X6) (`store.go:609-611`, `store.go:649-651`).
- The redirect on the GET is the "no screen whose Save can only fail" rule; the 400 on the POSTs is the enforcement (`app.go:799-806`).

**Data touched** — reads `payments`; writes nothing.

**Non-functional / UX notes** — a complete test asserts three layers: the screen offers nothing, the form route redirects, and both write routes answer 400 with the exact phrases `cannot be edited` / `cannot be voided`.

**Open questions** — none. At the time of this audit, `POST /payments/{id}/attachments` accepted an upload against a **linked** payment: `AddAttachment` had no `RequestID` guard and no screen offered the control, but the route was live and gated only on `attachment:create`. Recorded then as suspected defect SD-03. **Fixed (F-D-08 in Wave 1, F-A-03 in Wave 3):** the store refuses a payment whose `request_id` is set (`store.go:1580-1587`), and the handler resolves the payment and applies the caller's scope before staging anything, answering 404 on refusal (`app.go:1050-1070`, `app.go:1040-1048`). See the correction on SD-03 and `docs/qa/results/REPAIR-LOG.md`.

---

#### UC-C-24 — Edit or void a historical payment

| | |
|---|---|
| **Goal** | "A pre-module ledger row is wrong — correct it, or take it out of actuals." |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | **Edit** or **Remove** on a request-less ledger row |
| **Route(s)** | RT-09, RT-10, RT-11 |
| **Permission gate** | `payment:edit` / `payment:void` |
| **Coverage IDs** | X6 |
| **Priority** | medium |

**Preconditions**

1. A payment with `request_id IS NULL`.
2. `voided_at IS NULL` and the month is not locked (`app.go:812`, `store.go:612-613`, `store.go:652-653`).
3. The caller holds the matching verb.

**Postconditions (success)**

1. Edit: the payment's columns are updated, `updated_by` set, and one `audit_log` `update` row on entity `payment`.
2. Void: `voided_by`, `void_reason`, `voided_at` set, and one `audit_log` `void` row with `summary='Voided payment ₹{n}'` (`store.go:661-671`).
3. A voided payment drops out of actual totals but keeps its trail (`templates.go:343`).

**Postconditions (failure)** — nothing changed.

**Main success scenario**

1. Accountant opens a historical payment's detail; `payment_detail_historical` renders with the `.page-banner` `Payment #{id}` and an **Edit** button when unlocked and unvoided (`templates.go:340`).
2. Accountant presses **Edit**, fills the form and saves; `UpdatePaymentWithAttachment` succeeds because `RequestID` is nil.
3. Accountant instead opens the **Void Payment** danger zone, whose blurb is `Voiding keeps the audit trail but excludes this payment from actual totals.`, fills the required **Reason** and presses **Void** behind a `confirm()` (`templates.go:343`).

**Alternate flows**

- **24.a — from the ledger row.** The `details.inline-danger` **Remove** disclosure carries a `next` field so the operator returns to the filtered list (`templates.go:226`).
- **24.b — voiding twice.** `VoidPayment` returns `nil` early when `voided_at` is already set, so a double submit is idempotent rather than an error (`store.go:652-653`).

**Exception flows**

- **24.e1 — locked month.** The screen renders `<div class="locked">This payment belongs to a locked month and is read-only.</div>` and the controls are withheld (`templates.go:341`).
- **24.e2 — already voided.** `<div class="locked">Voided payment. Reason: {reason}</div>` and no controls (`templates.go:341`).
- **24.e3 — no such payment can be created.** On a fresh install this use case has **no subject**: `POST /payments` refuses without a `request_id`, and the seed creates only projects, heads and budgets. Reaching it needs a row inserted by `CreatePayment` as a seed/import helper (`regression-issues.spec.ts:487-495`, `phase-3 spec:91`).

**Business rules**

- **X6:** migration v4 never rewrites existing payment rows; `request_id` reads back NULL and those rows keep the pre-Phase-3 behaviour (`phase-3 spec:18`, `phase-3 spec:29`).
- The historical detail template is kept **verbatim** rather than redrawn, so the X6 guarantee is visible in the markup (`templates.go:336-338`).

**Data touched** — writes `payments`, `audit_log`, possibly `payment_attachments`.

**Non-functional / UX notes** — this is the only remaining screen in the product with a `.danger-zone` and an `onsubmit="return confirm(...)"`. It is also the only screen still using `dl.details.detail-grid` and `ol.timeline` rather than `dl.dl` and `ol.thread`.

**Open questions** — the historical branch is documented as surviving "until Phase 6 redraws the ledger" (`templates.go:337-338`), and Phase 6 is closed. Whether it is now permanent is not recorded.

---

#### UC-C-25 — Create a payment with no request at all

| | |
|---|---|
| **Goal** | (adversarial) "Free-standing payment entry is gone — is it really?" |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | `POST /payments` with no `request_id` |
| **Route(s)** | RT-06 |
| **Permission gate** | `payment:create` **+ `payment:settle`** pass (both are needed since Wave 3's F-D-10); the handler refuses |
| **Coverage IDs** | S15, X5 |
| **Priority** | critical |

**Preconditions**

1. The caller holds `payment:create` and a valid CSRF token.
2. The form carries a complete, otherwise-valid payment: `head_id`, `paid_on`, `amount`, `payment_mode`, `reference_no`, `settlement=settled`.
3. `request_id` is absent, empty, `0`, or unparseable.

**Postconditions (success — correctly refused)**

1. HTTP **400**, error page reading `Payments must be linked to an approved request.` (`app.go:693-696`).
2. **No** `payments` row. `SELECT COUNT(*) FROM payments` is unchanged.
3. No audit row, no attachment row, no notification.

**Postconditions (failure)** — a request-less payment would break S15, X5, and the exclusion arithmetic that depends on `payments.request_id` joining to `payment_requests.treatment`.

**Main success scenario**

1. Accountant opens `/payments/new` and confirms the screen offers only the picker — no free-standing entry control exists (UC-C-04 exception 04.e4).
2. Accountant posts to `/payments` by hand with every field except `request_id`.
3. `RequirePermission("payment","create")` passes and `withCSRF` passes.
4. `paymentCreate` computes `linkedID := parseID(r.FormValue("request_id"))`, finds `0`, and answers `400` before `paymentInput` is even called (`app.go:691-696`).

**Alternate flows**

- **25.a — `request_id=abc`.** `parseID` yields 0; same refusal.
- **25.b — `request_id=999999` (nonexistent).** Passes the zero test, so `RecordPaymentForRequest` runs and its `SELECT … WHERE id=?` returns no rows → `ErrNotFound` → but `paymentCreate` first tries `PaymentForRequest(linkedID)`, which also fails, so `settlementError` runs; that in turn calls `Request(ctx,linkedID)`, fails, and `respondStoreError` answers **404** (`app.go:707-718`, `linking.go:346-351`).
- **25.c — the store's own helpers.** `CreatePayment` and `CreatePaymentWithAttachment` still exist but are on **no route**; a reflection or grep test over `routes()` is the proof (`phase-3 spec:91`).

**Exception flows**

- **25.e1 — no `payment:create`.** `403`, which does not prove the refusal under test. The caller must hold the verb.

**Business rules**

- **S15/X5:** `paymentCreate` is the only user-reachable way a payment is created, and it creates nothing that is not linked to a request the caller holds (`app.go:687-689`).
- The refusal is a **handler** rule, before any store call, so a malformed post costs nothing.

**Data touched** — none.

**Non-functional / UX notes** — assert the payment count before and after, not only the status: a 400 with a written row would be the worst possible outcome and the status alone cannot see it.

**Open questions** — `configSections` still carries an `allow_direct_payments` toggle whose hint reads "Off. Every new payment starts from an approved request. Turning this on demands a written reason and is flagged in the audit log." (`configuration.go:69-71`). **No code reads that key.** Setting it to `1` changes nothing. Recorded as divergence DV-06.

---

#### UC-C-26 — Create a payment against an approved but unreserved request

| | |
|---|---|
| **Goal** | (adversarial) "The request is approved — can I skip the reservation and just pay it?" |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | `POST /payments` with a `request_id` whose request is `approved`, unclaimed |
| **Route(s)** | RT-06 |
| **Permission gate** | `payment:create` **+ `payment:settle`** pass (both are needed since Wave 3's F-D-10); the store refuses |
| **Coverage IDs** | S15, S2, X5 |
| **Priority** | critical |

**Preconditions**

1. The request is `status='approved'`, `processing_by IS NULL`.
2. The caller holds `payment:create` and a valid token.
3. Every other payment field is valid.

**Postconditions (success — correctly refused)**

1. HTTP **403**, message `reserve this request before recording its payment` (`store.go:898-900`).
2. No `payments` row; the request stays `approved` and unclaimed.

**Postconditions (failure)** — a payment without a reservation defeats S2 entirely: two accountants could pay the same invoice.

**Main success scenario**

1. Accountant posts to `/payments` with a valid `request_id` for an approved, unclaimed request.
2. `paymentCreate` passes the zero test and builds the input (`app.go:692-704`).
3. `RecordPaymentForRequest` validates the settlement word and the payment, opens the transaction, and reads `status, processing_by, amount, approved_amount` (`store.go:867-897`).
4. Store evaluates `status != "processing" || !processingBy.Valid || processingBy.Int64 != actor.ID` — the first two clauses are both true — and returns `ErrForbidden` (`store.go:898-900`).
5. `paymentCreate` removes the staged attachment, tries `PaymentForRequest` (which fails, so this is not a double-confirm), and calls `settlementError` (`app.go:709-718`).
6. `settlementError` sees `403`, which is below 500, so it re-renders the settlement sheet with the message rather than an error page (`linking.go:341-364`).

**Alternate flows**

- **26.a — reserved by somebody else.** The third clause fails; same 403. The screens route that case to the 409 conflict page instead (UC-C-05), but a hand-rolled POST straight to `/payments` gets the store's 403.
- **26.b — the request is on hold.** `status` is `approved`, so the same first clause fails; 403.
- **26.c — the request is `pending` / `rejected` / `completed`.** Same 403.
- **26.d — reserve first, then pay.** Succeeds; UC-C-03 then UC-C-08.

**Exception flows**

- **26.e1 — validation runs first.** `validatePayment` and the settlement-word check run **before** the transaction, so a test with a bad amount or a missing `settlement` gets a 400 and never reaches the reservation rule (`store.go:869-882`). Post a fully valid body.

**Business rules**

- The reservation is re-checked **inside** the settlement transaction, so it is not a screen convenience but the write's precondition (`store.go:898-900`).
- The check is `processing_by == actor.ID`, not merely "reserved": another accountant's reservation is as good as none for this caller.

**Data touched** — reads `payment_requests`; writes nothing (the staged attachment file is deleted).

**Non-functional / UX notes** — a test must assert the row count and the request's `status`/`processing_by` afterwards; the 403 alone does not prove the transaction rolled back.

**Open questions** — none.

---

#### UC-C-27 — Record a second payment against the same request

| | |
|---|---|
| **Goal** | (adversarial) "One request, one payment — what happens on the second?" |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | `POST /payments` twice for one `request_id` |
| **Route(s)** | RT-06 |
| **Permission gate** | `payment:create` **+ `payment:settle`** (F-D-10, Wave 3); store status guard + unique index |
| **Coverage IDs** | S9, S13 |
| **Priority** | critical |

**Preconditions**

1. One payment already links to the request; the request is `completed`, `completed_partial` or `partial_review`.
2. The caller holds `payment:create` and a valid token.

**Postconditions (success — correctly refused)**

1. `SELECT COUNT(*) FROM payments WHERE request_id={id}` is still exactly **1**.
2. The request's status is unchanged.
3. The caller is redirected **303** to `/payments/{existingID}` — a double confirm is not a failure (`app.go:711-716`).

**Postconditions (failure)** — two payments against one request would double-count actuals and make the `Approved` / `Paid` comparison on every screen meaningless.

**Main success scenario**

1. Accountant completes UC-C-08 and lands on `/payments/{id}`.
2. Accountant presses the browser Back button twice and re-submits the confirmation (or double-taps **Confirm and save payment**).
3. `RecordPaymentForRequest` opens the transaction and reads the request: `status` is now `completed`, so `status != "processing"` and the store returns `ErrForbidden` "reserve this request before recording its payment" **before** the INSERT (`store.go:898-900`).
4. `paymentCreate` calls `PaymentForRequest(linkedID)`, which resolves the existing payment, and redirects `303` to it (`app.go:711-716`).
5. Accountant lands on the same payment they already created; nothing was written.

**Alternate flows**

- **27.a — the database-level guarantee.** Were the status guard ever bypassed, `CREATE UNIQUE INDEX idx_payments_request ON payments(request_id) WHERE request_id IS NOT NULL` rejects the second INSERT, which `classify` maps to `ErrDuplicate` → `400` (`phase-3 spec:16`, `store.go:914-916`).
- **27.b — the request has left the queue.** After settlement the request is in the `Paid` tab, which offers no reserve control, and `LinkablePaymentRequests`' `Available` set can never contain it (`store.go:1113`, `store.go:1164-1165`).
- **27.c — a balance still owed.** The sheet says so in as many words: `The request accepts no further payment — any balance needs a fresh request.` (`templates.go:511`). The accept-partial sheet repeats it: `If the balance is still due later, {Requester} raises a new request for {₹diff}.` (`templates.go:921`).

**Exception flows**

- **27.e1 — a `partial_review` request.** Same path: `status != "processing"`, so the second payment is refused and the caller is bounced to the first payment.
- **27.e2 — the reservation was somehow re-taken.** `ReserveRequest` demands `status='approved'`, which a settled request is not, so re-reserving is impossible (`store.go:738`).

**Business rules**

- **S9** is enforced twice: the status guard in the transaction, and the unique partial index in the schema (`phase-3 spec:103`).
- The double-confirm redirect exists so a back button or a double tap does not look like a failure — the payment this request needed already exists, so go to it (`app.go:711-713`).
- **S13:** nothing is written unless the whole transaction commits, so a refused second attempt leaves no orphan payment row (`store.go:883-956`).

**Data touched** — reads `payment_requests`, `payments`; writes nothing.

**Non-functional / UX notes** — the observable outcome of a double confirm is a **303 to the existing payment**, not an error. A test that asserts a 4xx here will fail; assert the row count and the redirect target instead.

**Open questions** — none.

---

#### UC-C-28 — Tamper with the settlement value or the amount

| | |
|---|---|
| **Goal** | (adversarial) "The radios and the readonly field are client-side — what does the server accept?" |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | A hand-rolled `POST /payments` with a forged `settlement` or `amount` |
| **Route(s)** | RT-06, RT-05 |
| **Permission gate** | `payment:create` **+ `payment:settle`** (F-D-10, Wave 3); store validation |
| **Coverage IDs** | S13, S10, S11 |
| **Priority** | critical |

**Preconditions**

1. The request is `processing` and held by the caller.
2. The caller holds `payment:create` and a valid token.

**Postconditions (success — correctly refused)**

1. Any `settlement` other than exactly `settled` or `partial` (after trimming) → **400**, `choose payment settled or partial settlement`, nothing written (`store.go:867-871`).
2. `settlement=partial` with a blank `partial_reason` → **400**, `a reason is required for a partial settlement` (`store.go:872-874`).
3. `amount` above `approvedOf(request)` → **400**, `{paid} is more than the approved {ceiling} — to pay more, cancel this request and raise a new one` (`store.go:901-910`).
4. `amount <= 0` or a malformed `paid_on` → **400**, `valid head, date, and positive amount are required` (`store.go:1621-1623`).
5. In every case: no `payments` row, no status change, no audit row.

**Postconditions (failure)** — an accepted overpayment would let Accounts create an obligation nobody approved.

**Main success scenario**

1. Accountant reads the approved amount from `#approved`'s `data-approved` attribute — the readonly field is display only (`templates.go:397-398`).
2. Accountant posts `/payments` with `amount` set above that ceiling, `settlement=settled`, and everything else valid.
3. `RecordPaymentForRequest` passes the settlement-word and `validatePayment` checks, opens the transaction, reads `amount` and `approved_amount`, computes `ceiling = approved_amount` (falling back to `amount` when unapproved), and refuses `in.Amount > ceiling` (`store.go:901-910`).
4. `settlementError` re-renders the sheet at 400 with the typed figure echoed back, so the accountant does not lose their work (`linking.go:352-363`).

**Alternate flows**

- **28.a — forged `settlement=SETTLED`.** The comparison is case-sensitive after `TrimSpace`, so `SETTLED` is rejected by 400 (`store.go:867-871`).
- **28.b — skipping the preview entirely.** At audit time `POST /payments` was gated on `payment:create` while `POST /requests/{id}/settlement-preview` needed `payment:settle`, so a caller holding `create` but not `settle` could not open the sheet yet **could** post the settlement directly, and the store accepted it (divergence DV-04). **Fixed in Wave 3 (F-D-10, commit `1fac147`):** `POST /payments` now needs `payment:create` **and** `payment:settle`, and `payment:mark_partial` besides when `settlement=partial` — the last checked in the handler rather than as a route gate, because it applies to one value of one field (`app.go:446-447`, `app.go:833-837`). Skipping the preview no longer skips the verb. See REPAIR-LOG.md Wave 3.
- **28.c — forged `head_id`.** **Fixed since Wave 1 (F-D-01, commit `633997b`).** `RecordPaymentForRequest` never validates the *posted* `head_id` against anything, because it never uses it: it overwrites `in.HeadID` with the request's own `head_id` before `validatePayment` runs (`store.go:1112`). A forged value in the POST body is simply discarded, so there is no "an arbitrary id fails with `ErrInactiveHead`" case to reach — the only head ever checked is the one the manager actually approved against.
- **28.d — forged `vendor_payee` / `invoice_no`.** **Fixed since Wave 1 (F-D-01).** Both are hidden inputs copied from the request for display continuity, but the server overwrites them from the request row the same way as `head_id` (`store.go:1113-1114`) and never stores the posted values verbatim.
- **28.e — the preview with a forged amount.** `settlementPreview` only parses the amount; the ceiling is not checked there, so the sheet will happily display an over-approved figure and the refusal arrives at the confirm (`linking.go:294-308`).

**Exception flows**

- **28.e1 — unparseable amount at the preview.** 400, sheet re-rendered with `Nothing has been saved. Correct it and confirm again.` (`linking.go:294-298`, `templates.go:473`).
- **28.e2 — a 5xx cause.** `settlementError` deliberately routes a server fault to the error page instead of the sheet: re-rendering a sheet over a broken database would be a lie (`linking.go:337-345`).

**Business rules**

- **G13:** the approved amount is a hard ceiling; paying more is a different obligation, not a settlement decision (`store.go:901-903`).
- The client-side `#diff-banner` is display only — it lets the accountant see the refusal coming instead of meeting it after they press confirm (`fervid-app.js:494-497`).
- `hidden` is not validation; every `data-when` reveal is re-enforced server-side (`PROGRESS.md:275-276`, `templates.go:500-501`).

**Data touched** — reads `payment_requests`, `heads`, `projects`, `month_locks`; writes nothing.

**Non-functional / UX notes** — the amount is echoed back **even when it is what was rejected**: a malformed figure parses to zero, and the field still shows the characters the accountant actually entered (`linking.go:352-355`).

**Open questions** — none. This use case originally recorded suspected defect SD-05: `head_id`, `vendor_payee` and `invoice_no` accepted from the form with no comparison against the request. **Fixed in Wave 1 (F-D-01, commit `633997b`, see `docs/qa/results/REPAIR-LOG.md`).** The three fields are never read from the form at all any more — see 28.c/28.d above and the correction on SD-05 in the divergence log.

---

#### UC-C-29 — Requester reads the payment outcome on their request

| | |
|---|---|
| **Goal** | "Was my request paid, how much, and when?" |
| **Primary actor** | Requester |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Opening one's own request after settlement |
| **Route(s)** | `GET /requests/{id}` |
| **Permission gate** | `request:view`, scope `own` suffices |
| **Coverage IDs** | Q4, L10, L9 |
| **Priority** | high |

**Preconditions**

1. A payment links to the request.
2. The caller can view the request (`own` scope reaches their own row).

**Postconditions (success)** — a `200` request-detail page carrying a **Payment outcome** section; nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. Requester opens `/requests/{id}`.
2. `requestDetailData` calls `PaymentForRequest`; `ErrNotFound` is an *answer* here, not a failure, because no payment yet is the ordinary case for most of a request's life (`requests.go:552-562`).
3. System renders `<div class="section-head"><h2>Payment outcome</h2></div>` and a `.compare` block: `Approved`, `Paid on {date}`, then either `.cmp-row.diff` **Still owed to the payee** (partial) or `.cmp-row.match` **Difference · confirmed settled by Accounts** (settled) (`templates.go:2365-2378`).
4. For a partial payment, System also renders a `.banner.warn` `{EnteredByName} marked this a genuine partial payment` quoting the reason (`templates.go:2379-2384`).
5. Requester presses **View the payment** in the `.action-bar`, rendered when a payment exists **and** the reader holds `payment:view` (`templates.go:2410-2412`).

**Alternate flows**

- **29.a — no payment yet.** The whole **Payment outcome** section is omitted (`templates.go:2368`, `templates.go:2385`).
- **29.b — no `payment:view`.** The outcome figures are still shown; only the link to the payment is withheld. The requester learns the outcome without needing ledger access — that is the Q4 point (`templates.go:2410`).
- **29.c — the status pill.** `completed`, `completed_partial` and `partial_review` each render their own pill through `pillClass`/`reqStatus`, and the shared `waitingOn` line says whose move it is (`templates.go:2261-2266`).

**Exception flows**

- **29.e1 — another requester's request.** `404` from `loadViewableRequest`; `own` scope means `requester_id = u.ID`. **`403` until Wave 3 (F-G-002, commit `1fac147`)**, when the scope refusal was made indistinguishable from a missing row (`requests.go:295-308`, `requests.go:399-410`).
- **29.e2 — the store read fails for a reason other than not-found.** `respondStoreError` → 500 (`requests.go:560-561`).

**Business rules**

- **Q4:** the requester reads the outcome on the **request**, not in the ledger (`requests.go:553-555`).
- A settled shortfall is agreed and an unsettled one is still owed, and the two must never read the same — hence two different `.cmp-row` classes and two different labels (`templates.go:2365-2367`).

**Data touched** — reads `payment_requests`, `payments`, `request_comments`, `request_attachments`, `audit_log`.

**Non-functional / UX notes** — the request detail has exactly **one** `.action-bar`, sticky at phone width, so the payment link joins it rather than bringing its own (`templates.go:2407-2409`).

**Open questions** — none.

---

### 3.2 Recoverables

#### UC-C-30 — Administer the recoverable categories

| | |
|---|---|
| **Goal** | "Add, rename, re-scope or retire the kinds of money we expect back, without a code change." |
| **Primary actor** | Administrator |
| **Supporting actors** | Requester (whose form the rules govern) |
| **Scope / level** | system · user-goal |
| **Trigger** | The **Recoverable categories** fieldset on Configuration |
| **Route(s)** | `GET /configuration` (RT-27), `POST /configuration/recoverable-categories` → `303 /configuration` (RT-26) |
| **Permission gate** | RT-27 `config:view`; RT-26 `recoverable_category:edit`, `withCSRF` |
| **Coverage IDs** | V4, V5 |
| **Priority** | high |

**Preconditions**

1. Migration **v6** has run, so `recoverable_categories` exists and is seeded with six rows (`recoverables.go:33-61` — `upRecoverableCategories`'s own comment names it v6; registered as `{Version: 6, Name: "recoverable_categories"}` at `migrations.go:320-321`). **This document said "v7" throughout; that was wrong at the time of the audit and is corrected here — v7 is `notifications`.** Later migrations are present too and none of them touches this table: the chain is **v1–v10** as of this writing (`Version:` at `migrations.go:42/99/150/272/304/320/327/336/344/352`).
2. The caller holds `config:view` to read and `recoverable_category:edit` to write.
3. For a new row, `name` is non-blank and contains at least one letter or digit (`recoverables.go:185-200`).

**Postconditions (success)**

1. **Create:** one `recoverable_categories` row with a derived `code`, the given `name`, `requires_project`, `requires_counterparty`, `active=1` and `sort_order`; one `audit_log` row `action='create'`, `entity_type='recoverable_category'`, `summary='Saved recoverable category {name}'` (`recoverables.go:201-221`).
2. **Update:** `name`, `requires_project`, `requires_counterparty`, `active`, `sort_order` overwritten; **`code` is never rewritten** (`recoverables.go:211-217`).
3. `303` to `/configuration`.
4. `recoverableRules` — which `validateRequestInput` consumes — reflects the change on the **next** request, with no restart (`recoverables.go:115-129`, `requests.go:371-377`).

**Postconditions (failure)** — no row written, no audit row; `code` unchanged in every case.

**Main success scenario**

1. Administrator opens `/configuration`.
2. System loads `ListRecoverableCategoriesWithUsage`, which carries a per-row `InUse` count of `payment_requests.recoverable_category_id` references (`configuration.go:98-103`, `recoverables.go:229-248`).
3. System renders a `<fieldset>` with `<legend>Recoverable categories</legend>` **outside** the main settings form (`templates.go:3128-3129`).
4. System renders one `table.t-cards`, head `Category · Requires · Active · In use` (`templates.go:3131`).
5. Administrator reads each row: the name; the derived **Requires** phrase (`Related project`, `Counterparty company`, `Related project and counterparty company`, or `Nothing extra`); an Active checkbox; and the usage count (`templates.go:3132-3147`, `recoverables.go:83-94`).
6. Administrator adds a category: fills `#nc-name`, label **New category**, placeholder `e.g. Retention deposit` (required); chooses `#nc-req`, label **Must also capture**, from `Nothing extra` / `Related project` / `Counterparty company` / `Related project and counterparty company`; presses **Add category** (`templates.go:3150-3163`).
7. System maps the single `requires` value onto the two booleans — the only place that mapping exists (`configuration.go:113-127`).
8. `UpsertRecoverableCategory` derives the code with `categoryCode(name)` — lowercase, alphanumerics kept, everything else collapsed to single `_`, trimmed — inserts, audits and commits (`recoverables.go:99-113`, `recoverables.go:197-209`).
9. Administrator reads the hint beneath the fieldset: `All recoverables always capture the counterparty, the reason, an expected return date and the refund terms. These rules only add what the category needs on top. An employee advance fills the counterparty in from the requester automatically — that is a request-type rule, not a category setting.` (`templates.go:3165`).

**Alternate flows**

- **30.a (step 5) — retire a category.** Unticking Active submits the row's own `<form id="rc-{id}">` via `onchange="this.form.submit()"`, carrying the id, the existing name, `requiresKey` and the sort order as hidden inputs. A `<noscript>` **Save** button covers the no-JS path (`templates.go:3136-3144`). Deactivation stops **new** requests naming the category, because `recoverableRules` reads active rows only (`recoverables.go:117-121`).
- **30.b (step 5) — no `recoverable_category:edit`.** The Active cell degrades to a read-only `<span class="pill good no-dot">On</span>` / `<span class="pill neutral no-dot">Off</span>`, and the add-form is omitted entirely (`templates.go:3145`, `templates.go:3149`).
- **30.c (step 8) — rename an existing category.** The `UPDATE`'s SET list deliberately omits `code`, so every request already pointing at the row keeps resolving its field rules (`recoverables.go:211-213`).
- **30.d — the six seeded rows.** `employee_advance` (Employee advance, nothing extra), `emd` (EMD, project), `pbg` (PBG, project), `icd` (ICD, counterparty), `security_deposit` (Security deposit, **counterparty**), `other` (Other, nothing extra) (`recoverables.go:21-31`).

**Exception flows**

- **30.e1 — blank name.** `ErrValidation` "recoverable category name is required" → `400` (`recoverables.go:185-188`).
- **30.e2 — a name with no letter or digit** (e.g. `"---"`). `categoryCode` yields `""` → `ErrValidation` "recoverable category name must contain a letter or digit" → `400` (`recoverables.go:197-200`).
- **30.e3 — a duplicate name.** `idx_recoverable_categories_name_nocase` is unique on `lower(name)` → `classify` maps it to `ErrDuplicate` → `400` (`recoverables.go:50`, `recoverables.go:203-205`).
- **30.e4 — a duplicate derived code.** `idx_recoverable_categories_code` is unique; two differently-punctuated names can collapse to one code (e.g. `EMD` and `e.m.d`) → `ErrDuplicate` → `400` (`recoverables.go:49`).
- **30.e5 — an unrecognised `requires` value.** `400` `That category requirement is not recognised.` (`configuration.go:124-127`).
- **30.e6 — posting to `/recoverable-categories` (no `/configuration` prefix).** `404` or `405`; the categories live only on the Configuration path (`recoverables_app_test.go:287-292`, AF-06).
- **30.e7 — no `recoverable_category:edit`.** `403` from the middleware, even for a caller holding `config:edit` — the seeded Accounts role is the concrete example (`recoverables_app_test.go:295-309`).
- **30.e8 — no categories at all.** `<td colspan="4" class="empty">No recoverable categories yet.</td>` (`templates.go:3147`).

**Business rules**

- **`code` is the category's stable identity.** Phase 2 stored the code in `payment_requests.recoverable_category` and keyed its hardcoded rules on it; the table therefore carries `code`, and `UpsertRecoverableCategory` derives it on create and never rewrites it, so renaming a category cannot orphan its requests (`recoverables.go:11-16`, `PROGRESS.md:165-170`).
- **V4:** the rule set is read from the table, not the built-in map, so an admin-added category is usable at once (`recoverables.go:115-118`).
- The built-in `recoverableCategoryRules` map survives only as the migration seed and as the fixture the pure validator's tests use; `TestSeededCategoriesMatchPhase2Rules` fails if the two ever drift (`requests.go:109-123`, `recoverables.go:18-20`).
- The fieldset sits **outside** the main settings form because a `<form>` nested inside another is invalid HTML that the parser silently drops — the row toggles would post nothing at all (`templates.go:3119-3124`, `PROGRESS.md:279-284`).
- **V5 field rules:** EMD and PBG require a project; ICD and security deposit require a counterparty; employee advance and other require neither (`recoverables.go:25-30`, `requests.go:116-123`).
- Migration **v6** back-fills `payment_requests.recoverable_category_id` from the code, because Phase 2 hardcoded it to NULL and every register query joins on it (`backfillRecoverableCategoryIDs`, called from `upRecoverableCategories`: `recoverables.go:60`, `recoverables.go:63-78`; `PROGRESS.md:176-180`). **Recorded as "v7" at audit time; that was a mis-numbering, not a change — see precondition 1.**

**Data touched** — writes `recoverable_categories`, `audit_log`; reads `payment_requests` for the usage count.

**Non-functional / UX notes** — the Active label uses `.sr-only` (never the invented `.vh`) for its accessible name (`templates.go:3142`, `PROGRESS.md:291-293`). At 390 px the table restacks with `data-label` on every cell, including a `data-label=""` on the empty-state row.

**Open questions** — none. At the time of this audit **the request form did not read this table**: `request_form_fields` hardcoded the six seeded `<option>` values, so an admin-added category could never be chosen on the real form and a deactivated one was still offered — `V4` satisfied in the store and the validator but not in the UI, recorded as suspected defect SD-06. **Fixed (F-E-02/F-B-17):** Wave 2 exposed `ListRecoverableCategories(ctx, activeOnly)` and Wave 4a rewrote the select to `{{range .Categories}}<option value="{{.Code}}">` with a `Choose a category` placeholder (`templates.go:1828-1831`). V4 is now delivered end to end. See the correction on SD-06 and `docs/qa/results/REPAIR-LOG.md`.

---

#### UC-C-31 — Raise a recoverable request under its category's field rules

| | |
|---|---|
| **Goal** | "Record money going out that we expect back, with everything we will need to chase it." |
| **Primary actor** | Requester |
| **Supporting actors** | Manager (approves it), Administrator (owns the categories) |
| **Scope / level** | system · user-goal |
| **Trigger** | Choosing **Recoverable** treatment on the new-request form |
| **Route(s)** | `GET /requests/new?type=…`, `GET /requests/new/fields`, `POST /requests` |
| **Permission gate** | `request:create` |
| **Coverage IDs** | V1, V5, V6, V8, T9 |
| **Priority** | critical |

**Preconditions**

1. The caller holds `request:create`.
2. `treatment='recoverable'` and `type` is `recoverable` or `employee_advance` — no other type may be recoverable (`requests.go:192-245`).
3. The chosen `recoverable_category` code exists in the **active** rule set (`requests.go:174-177`).

**Postconditions (success)**

1. One `payment_requests` row with `treatment='recoverable'`, `recoverable_category={code}`, **`recoverable_category_id`** resolved from the code, `expected_return_date`, `repayment_notes`, and `counterparty`/`project_id` as the category demands (`requests.go:414-427`, `recoverables.go:131-147`).
2. `status='pending'`, `submitted_at` set — D1 makes create and submit one POST (`requests.go:419`).
3. One `audit_log` `submit` row on entity `payment_request` (`requests.go:442-447`).
4. The row appears in the recoverables register immediately, with the ageing label `Awaiting payment` because no money has left yet (`recoverables.go:264-270`).

**Postconditions (failure)** — no request row, no audit row.

**Main success scenario**

1. Requester chooses **Recoverable** as the treatment, then the type (T1 order).
2. System swaps `#form-fields` with `request_form_fields`, whose recoverable branch is `<fieldset data-when="treatment:recoverable"><legend>Recoverable details</legend>` (`templates.go:1730-1732`).
3. Requester chooses `#rcategory`, label **Category**, hint `Categories are maintained by your administrator.`; the select carries `hx-get="/requests/new/fields" hx-trigger="change"` so choosing re-renders the fieldset (`templates.go:1734-1746`).
4. Requester fills `#expected-return`, label **Expected return date** (`type="date"`, `aria-required`) (`templates.go:1747-1750`).
5. For **EMD** or **PBG**, System reveals `#rproject`, label **Related project**, hint `EMD and PBG always belong to a project.` (`templates.go:1751-1760`).
6. For **ICD** or **security deposit**, System reveals `#counterparty`, label **Counterparty company**, placeholder `Company receiving the deposit` (`templates.go:1761-1766`).
7. Requester fills `#terms`, label **Repayment or refund terms** (`aria-required`) (`templates.go:1767-1770`).
8. Requester reads the `.banner.brand` **This will not touch budget actuals** / `It appears in Recoverable payments instead.` (`templates.go:1771-1779`).
9. Requester submits. `CreateRequest` loads the rule set from the table, runs the pure `validateRequestInput`, resolves the category id, and writes the row (`requests.go:365-451`).

**Alternate flows**

- **31.a (step 1) — an employee advance.** `type='employee_advance'` may be **either** treatment: `budget` demands a project and head, `recoverable` runs the category rules. That is T9 (`requests.go:232-239`).
- **31.b (step 6) — payee auto-recorded.** For `reimbursement` and `employee_advance`, `forcesRequesterPayee` clears `vendor_id` and sets `vendor_payee = actor.Name` — the **payee** is the requester (`requests.go:249-253`, `requests.go:367-370`).
- **31.c (step 5) — V8, a real project.** A recoverable may link to a real project; `RecoverableReport` selects `COALESCE(p.name,'')` and the register prints `Not project linked` when it is empty (`recoverables.go:288`, `templates.go:3541`).
- **31.d (step 3) — `other`.** Requires neither project nor counterparty, but still requires the return date and the terms (`requests.go:178-183`).

**Exception flows**

- **31.e1 — unknown or inactive category.** `ErrValidation` "choose a recoverable category" → `400` (`requests.go:174-177`).
- **31.e2 — missing expected return date.** `ErrValidation` "expected return date is required for recoverables" → `400`. Note the date is required for **every** recoverable, whatever the category (`requests.go:178-180`).
- **31.e3 — blank repayment terms.** `ErrValidation` "repayment or refund terms are required for recoverables" → `400` (`requests.go:181-183`).
- **31.e4 — EMD/PBG with no project.** `ErrValidation` "this recoverable category always belongs to a project" → `400` (`requests.go:184-186`).
- **31.e5 — ICD/security deposit with no counterparty.** `ErrValidation` "this recoverable category needs a counterparty company" → `400` (`requests.go:187-189`).
- **31.e6 — `type='recoverable'` with `treatment='budget'`.** `ErrValidation` "recoverable type requires recoverable treatment" → `400` (`requests.go:240-243`).
- **31.e7 — `vendor_invoice` / `vendor_advance` / `reimbursement` with recoverable treatment.** Each type's own branch demands `treatment=='budget'` → `400` (`requests.go:194-195`, `requests.go:210-211`, `requests.go:223-224`).
- **31.e8 — a hidden field posted anyway.** `hidden` fieldsets still submit and `hidden` is not validation; `validateRequestInput` is the enforcement and it runs on every path (`requests.go:127`, `PROGRESS.md:275-276`).

**Business rules**

- **V1:** treatment is the classification, and it is a first-class column, not a derived flag (`requests.go:145-147`).
- **V6:** every recoverable records the counterparty (when its category demands it), the expected return date and the refund terms; the requester is recorded as the payee for an employee advance (`requests.go:173-190`, `requests.go:367-370`).
- `validateRequestInput` stays **pure** — the caller supplies the rule set — so the rules can come from the database without the validator reaching for it (`requests.go:125-127`).
- `recoverable_category_id` is populated on **both** write paths (create and edit); Phase 2 hardcoded it NULL and **v6** back-fills the history (`requests.go:378-381`, `requests.go:583`, `recoverables.go:63-78`, `PROGRESS.md:176-180`). (Written as "v7" at audit time — a mis-numbering; see UC-C-30 precondition 1.)
- `payment_requests.recoverable_category_id` is a plain `INTEGER` with **no** `REFERENCES`, because a forward foreign key would have made SQLite reject every write to the table before **v6** created the parent — the reason is spelled out in v3's own schema comment (`migrations.go:163-170`, `PROGRESS.md:261-265`).

**Data touched** — writes `payment_requests` (incl. `treatment`, `recoverable_category`, `recoverable_category_id`, `counterparty`, `expected_return_date`, `repayment_notes`), `request_attachments`, `audit_log`; reads `recoverable_categories`.

**Non-functional / UX notes** — every revealed field uses `aria-required`, never `required`, because a `required` control inside a `data-when`-hidden container makes the form unsubmittable in Chrome; the asterisks are `aria-hidden` (`templates.go:1736`, `PROGRESS.md:277-278`).

**Open questions** — none, as UC-C-30. At the time of this audit the category select was hardcoded, so V4 and this use case's step 3 disagreed about where the option list came from (SD-06). **Fixed in Waves 2/4 (F-E-02/F-B-17):** the select now ranges over the active categories (`templates.go:1828-1831`), which is what step 3 always claimed.

---

#### UC-C-32 — Read the recoverables dashboard

| | |
|---|---|
| **Goal** | "How much money is out there, how much is overdue, and who has it?" |
| **Primary actor** | Accountant (or anyone with `recoverable_report:view`) |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar "Recoverables" |
| **Route(s)** | `GET /recoverables` (RT-22) |
| **Permission gate** | `recoverable_report:view` |
| **Coverage IDs** | V3, V2 |
| **Priority** | high |

**Preconditions**

1. The caller holds `recoverable_report:view` — the seeded Accounts and Admin roles do; **Manager and Requester do not** (AF-02, AF-03).
2. At least one live recoverable exists for the tables to be non-empty.

**Postconditions (success)** — a `200` page titled `Recoverable payments`; nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. Accountant opens `/recoverables`.
2. System takes `asOf := time.Now().UTC()` **once, at the HTTP boundary**, and passes it to all three store reads, so every ageing figure on the page agrees (`recoverables.go:23-39`, `recoverables.go:15-21`).
3. System renders the `.page-banner`: eyebrow `Money we expect back`, `<h1>Recoverable payments</h1>`, `.sub` `{₹} outstanding across {n} payments · none of it counts as budget spend` (`templates.go:3431-3436`).
4. System renders the `.banner.brand` **Kept out of budget actuals on purpose** / `A deposit is not an expense. These payments never appear in the variance grid or in project spend — they live here until the money comes back.` (`templates.go:3440-3446`).
5. System renders a `.metric-strip` of **four** tiles, values formatted with `short` (`₹x.xx L` / `₹x.xx Cr` above ₹1 lakh, else the full figure): `Outstanding`, `Past expected return` (on a `.metric.warn` tile), `Due in 30 days`, `Paid out this month`. Each has a `.metric-foot` count (`templates.go:3448-3453`, `money.go:47-61`).
6. System renders **By category**: `table.t-cards`, head `Category · Count · Outstanding · Overdue · Oldest`, each `.t-lead` linking `/recoverables/list?q={label}`, an overdue cell carrying `.bad-num` when non-zero, and a `<tfoot>` **Total** row using the exact `money` figures (`templates.go:3455-3472`).
7. System renders **By counterparty**: head `Counterparty · Categories · Items · Outstanding · Expected back`, each `.t-lead` linking `/recoverables/list?counterparty={label}` (`templates.go:3474-3484`).
8. System renders the `.banner.locked` **Tracking the money coming back is not in this version** / `A recoverable closes when its payment is made. Repayments, forfeitures, and converting a lost deposit into an expense are deliberately out of scope — they need an accounting adjustment process, not an edit to a completed payment.` (`templates.go:3486-3492`).
9. Accountant presses **⤓ Export CSV** (`.pb-actions`, gated on `recoverable_report:export`) or **Full list →** (`templates.go:3437`, `templates.go:3455`).

**Alternate flows**

- **32.a (step 6) — an uncategorised row.** The category rollup labels it `Uncategorised` (`recoverables.go:421`).
- **32.b (step 7) — no counterparty recorded.** Labelled `Not recorded`; the `Categories` column is a `GROUP_CONCAT(DISTINCT rc.name)` of the categories that counterparty holds (`recoverables.go:424-425`).
- **32.c (step 6) — never paid.** `Oldest` reads `Not yet paid`; `Expected back` reads `No fixed date` (`templates.go:3463`, `templates.go:3482`).
- **32.d (step 5) — no `recoverable_report:export`.** The export button is not rendered.

**Exception flows**

- **32.e1 — no recoverables at all.** `<td colspan="5" class="empty">No recoverable payments yet.</td>` and `No counterparties yet.` (`templates.go:3464`, `templates.go:3483`).
- **32.e2 — no `recoverable_report:view`.** `403` from the middleware; the nav entry is hidden (`nav.go:64`).

**Business rules**

- "A live recoverable" has exactly one definition, shared by the report, the metrics and the rollups so the dashboard totals can never drift from the list underneath them: `treatment='recoverable' AND status NOT IN ('rejected','cancelled','withdrawn')` (`recoverables.go:250-254`).
- **Correction (F-G-016/F-E-03, fixed, Wave 4):** the four tiles, the by-category rollup and the by-counterparty rollup all read through `RecoverableMetrics`/`RecoverableRollups`, which — like the register in UC-C-33 — now apply `recoverableScope(viewer)` (`internal/store/recoverables.go:347-357`, taken as a `RecoverableViewer` parameter at `recoverables.go:488` and `recoverables.go:532`; the viewer is built at `internal/app/recoverables.go:117-120`). At the time of this audit they did not, so a caller with `recoverable_report:view` and a narrower-than-`all` request scope saw company-wide totals; nothing changes for the seeded roles, which both hold `request=all`.
- The money considered at risk is `COALESCE(py.amount, COALESCE(pr.approved_amount, pr.amount))` — what actually left when the payment exists, else what was approved, else what was asked for (`recoverables.go:256-258`).
- The payment join excludes voided rows: `LEFT JOIN payments py ON py.request_id=pr.id AND py.voided_at IS NULL` (`recoverables.go:294`).
- `time.Now()` is called at the HTTP boundary and nowhere below it, so the same request always produces the same ageing and tests can pin "25 days overdue" to a fixed clock (`recoverables.go:19-21`).
- The mockup's by-counterparty "Type" column has no field behind it anywhere in the schema and is replaced by the distinct categories that counterparty holds — real data answering the same question (`templates.go:3422-3427`).

**Data touched** — reads `payment_requests`, `payments`, `recoverable_categories`, `projects`, `users`.

**Non-functional / UX notes** — `.metric-foot` and `.metric.warn` both have rules now (AF-13), so the third line and the warn tint render as designed. The `.page-banner` is deliberately **not** `d-only`: that would leave a phone with no visible `h1` and fail the UX sweep (`templates.go:3427-3428`). `shell.spec.ts` covers `/recoverables` on both devices (`shell.spec.ts:15`).

**Open questions** — the by-category `<tfoot>` **Total** row reuses `RecMetrics.OutstandingCount`/`OutstandingAmount`, which spans the whole live set. Since the rollup is also over the whole live set the two agree today; nothing enforces that they will.

---

#### UC-C-33 — Work the aged recoverables register and open one record

| | |
|---|---|
| **Goal** | "Show me every recoverable aged against its expected return date, filter it, and let me open one." |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | **Full list →** on the dashboard, or a rollup link |
| **Route(s)** | `GET /recoverables/list` (RT-23), `GET /recoverables/{id}` (RT-25) |
| **Permission gate** | `recoverable_report:view` |
| **Coverage IDs** | V3, V6, V8 |
| **Priority** | high |

**Preconditions**

1. The caller holds `recoverable_report:view`.
2. At least one live recoverable exists.

**Postconditions (success)** — a `200` list page titled `All recoverables`, and a `200` detail page titled `Recoverable {Number}`; nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. Accountant opens `/recoverables/list`.
2. System allow-lists the query parameters: `ageing` ∈ {`overdue`,`due30`,`later`,`unpaid`} (anything else means "all"), `order` ∈ {`amount`,`paid_on`,`number`} (`recoverables.go:48-71`).
3. System renders the `.page-banner` `<h1>All recoverables</h1>` with `.sub` `Aged against the expected return date · overdue shown in red` (`templates.go:3503-3510`).
4. System renders the desktop `.toolbar` — `#q` **Search** (placeholder `Counterparty, project, request number…`), `#cat` **Category** (`All categories` plus every category, active or not), `#age` **Ageing** (`All`, `Overdue`, `Due in 30 days`, `Due later`, `Not yet paid`) and an **Apply** button — and the mobile `.m-filters` with a **Filters** submit (`templates.go:3512-3533`).
5. System renders one `table.t-cards`, head `Request · Category · Counterparty · Project · Amount · Paid on · Expected back · Ageing` (`templates.go:3536`).
6. Accountant reads a row: `.t-lead` linking `/recoverables/{requestID}`; the category in a `<span class="pill recoverable">`; `Not recorded` / `Not project linked` / `Not yet paid` / `No fixed date` for empty values; and the ageing pill (`templates.go:3537-3546`).
7. Accountant reads the `<tfoot>`: `{n} shown` and the exact `money` total of the filtered rows (`templates.go:3547-3552`, `recoverables.go:86-89`).
8. Accountant presses a `.t-lead` link and lands on the detail screen.
9. System refuses any request whose `treatment` is not `recoverable`, then re-runs the register query filtered by the request's own number so the ageing here and in the list can never disagree (`recoverables.go:130-154`).
10. System renders `.req-head`: `.rh-no` the number, `.rh-amt` the recoverable amount, `h1` the purpose, `.rh-meta` `Raised by {requester} · paid {date} · <span class="pill recoverable">Recoverable · {Category}</span>`, and `.rh-status` carrying the request status pill plus the ageing pill (`templates.go:3565-3570`).
11. System renders the **Recoverable details** card, headed `<span class="pill recoverable no-dot">Not in budget actuals</span>`, with `dl.dl` rows `Amount`, `Category`, `Counterparty`, `Related project`, `Expected return`, `Ageing` and a full-width `Refund terms` (`templates.go:3582-3593`).
12. System renders the **Payment** card — either the paid figures (`Paid on`, `Amount paid`, `Mode`, `Reference`, `Recorded by`, `Payee`) or the empty state (`templates.go:3595-3609`).
13. System renders **History and conversation** using Phase 2's merged `RequestThread`, so the story reads identically wherever the request is opened (`templates.go:3611-3624`, `recoverables.go:160-168`).
14. Accountant presses **Back to list**, or **⤓ Export this record** (`/recoverables/list.csv?q={number}`) (`templates.go:3635-3640`).

**Alternate flows**

- **33.a (step 6) — the ageing labels.** `On hold` (tone `hold`), `Awaiting payment` (tone `approved`), `No fixed date` (tone `neutral`), `{n} days overdue` (tone `bad`), `Due today` (tone `neutral`), `{n} days to go` (tone `neutral`) (`recoverables.go:264-279`).
- **33.b (step 2) — default ordering.** Overdue first, then the soonest expected return; undated rows sort last via `COALESCE(NULLIF(expected_return_date,''),'9999-12-31')` (`recoverables.go:351-354`).
- **33.c (step 4) — a month range.** `from`/`to` filter on `substr(COALESCE(py.paid_on,''),1,7)`, swapped if reversed and mirrored if only one is given. With no range the register shows **every** live recoverable, including approved-but-unpaid ones (`recoverables.go:301-314`, `recoverables.go:45-47`).
- **33.d (step 11) — overdue.** A `.banner.warn` **Past its expected return date** / `Expected {date}. This is a reporting flag only — chasing the money and recording its return happen outside this version.` (`templates.go:3572-3580`).
- **33.e (step 13) — comment.** A `.comment-box` posting to Phase 2's own `/requests/{id}/comment`, placeholder `Record what you heard from the counterparty.`, gated on `request:comment` (`templates.go:3626-3633`).

**Exception flows**

- **33.e1 — no rows match.** `<td colspan="8" class="empty">No recoverable payments match these filters.</td>` (`templates.go:3546`).
- **33.e2 — a non-recoverable id.** `404` `That request is not a recoverable payment.` — this screen is the register, not a general request viewer (`recoverables.go:137-141`).
- **33.e3 — an unknown id.** `404` `The requested record was not found.` (`recoverables.go:132-135`).
- **33.e4 — no `recoverable_report:view`.** `403` on both routes.
- **33.e5 — `/recoverables/list.csv` vs `/recoverables/{id}`.** The literal final segment wins in Go 1.22+ `ServeMux`, so the export never reaches the detail handler (`app.go:400-406`).
- **33.e6 — an unpaid row asserted as overdue.** Money that never left cannot be overdue: `recoverableAgeing` returns `Awaiting payment` whenever `paid_on` is empty, whatever the return date (`recoverables.go:266-268`, `PROGRESS.md:186-188`).

**Business rules**

- **Correction (F-G-016/F-E-03, Wave 4, critical, fixed):** at the time of this audit, this screen, its CSV and its summary aggregates applied **no data scope at all** — `recoverable_report:view` alone returned every recoverable in the company, including rows the same caller was refused on `/requests/{id}`. Fixed: every read builds its options through `recoverableListOptions`, which sets `Viewer: a.recoverableViewer(r)` (`internal/app/recoverables.go:97`, viewer built at `recoverables.go:117-120`), and the store applies it via `recoverableScope` (`internal/store/recoverables.go:347-357`) in `RecoverableReport` (`recoverables.go:401-403`), `RecoverableMetrics` (`recoverables.go:488`) and `RecoverableRollups` (`recoverables.go:532`) alike — `RecoverableViewer` itself is `{Scope, ViewerID}` at `internal/store/models.go:281-288`, and an unrecognised scope returns `AND 0`, i.e. no rows — so the list, the CSV, the detail (33.e2/33.e3, which also now answer the request's own 404 rather than a blanket 403 — see below) and the dashboard rollups all agree with the caller's `request` scope. Nothing changes for Accounts or Admin, both seeded `request=all` — which is exactly why this went unnoticed originally.
- **33.e3 correction:** an id outside the caller's scope now answers the same `404` as an unknown id (F-G-002, `recoverables.go:195-198`) — the register is not an existence oracle for requests the caller cannot see.
- **V3:** the recoverable register and its total are separate from budget reporting — a different route, a different query and a different total (`recoverables.go:281-382`).
- Ageing is whole calendar days from `asOf` to the expected return date, negative meaning overdue, computed in SQL as `CAST(julianday(...) - julianday(...) AS INTEGER)` (`recoverables.go:292`).
- An undated row substitutes today so `julianday` never scans NULL into an int; the label then says `No fixed date`, not `Due today` (`recoverables.go:372-377`).
- The detail screen reuses the register query rather than computing ageing a second way (`recoverables.go:142-144`).
- The history is Phase 2's merged thread, not a second rendering of the same events (`templates.go:3557-3562`).

**Data touched** — reads `payment_requests`, `payments`, `recoverable_categories`, `projects`, `users`, `request_comments`, `audit_log`.

**Non-functional / UX notes** — both the desktop `.toolbar` and the mobile `.m-filters` are plain GET forms, so filtering works with no JavaScript and the phone is not left with a subset of the filters (`templates.go:3496-3500`). The full-width `Refund terms` row uses `style="grid-column:1/-1"` — the convention — never an invented `.dl-wide` (`templates.go:3591`, `PROGRESS.md:291-293`).

**Open questions** — the list's export link builds `?category=&ageing=&q=` but drops `from`/`to`, so exporting from a month-filtered list silently exports the unfiltered range (`templates.go:3509`).

---

#### UC-C-34 — Export the recoverables register as CSV

| | |
|---|---|
| **Goal** | "Give me the register as a spreadsheet." |
| **Primary actor** | Accountant |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | **⤓ Export CSV** on the dashboard or the list, or **⤓ Export this record** on a detail |
| **Route(s)** | `GET /recoverables/list.csv` (RT-24) |
| **Permission gate** | `recoverable_report:export` |
| **Coverage IDs** | V3, D3 |
| **Priority** | medium |

**Preconditions**

1. The caller holds `recoverable_report:export` (Accounts and Admin do; `migrations.go:392`).
2. The same filters as UC-C-33 apply, read through the shared `recoverableListOptions` (`recoverables.go:98`).

**Postconditions (success)**

1. `Content-Type: text/csv` and `Content-Disposition: attachment; filename="recoverables.csv"` (`recoverables.go:123-124`).
2. A header row `Number, Category, Counterparty, Project, Amount, Paid On, Expected Return, Ageing, Status, Requester, Repayment Notes`, then one row per register row with `Amount` formatted by `money.FormatPaise` (so it carries `₹`) (`recoverables.go:113-117`).
3. One `audit_log` row: `action='export'`, `entity_type='recoverable_report'`, `summary='Exported recoverable payments · {scope}'`, where scope is `all live recoverables` or `{from} to {to}` (`recoverables.go:104-110`).

**Postconditions (failure)** — no audit row is written on a `403`; on a CSV writer failure the audit row has already been written (see open questions).

**Main success scenario**

1. Accountant presses **⤓ Export CSV**.
2. System resolves the same options the list used and re-runs `RecoverableReport` (`recoverables.go:98-103`).
3. System records the export in the audit log **before** writing the body (`recoverables.go:109-110`).
4. System writes the header and rows into a buffer, flushes, sets the two headers and writes the bytes (`recoverables.go:111-127`).
5. Accountant's browser saves `recoverables.csv`.

**Alternate flows**

- **34.a — one record.** The detail screen's **⤓ Export this record** passes `?q={number}`; the number is unique, so exactly one row comes back (`templates.go:3639`).
- **34.b — from a rollup.** A category or counterparty link carries `?q=` / `?counterparty=` through to the export when re-pressed from the list.
- **34.c — an empty result.** A valid CSV with only the header row.

**Exception flows**

- **34.e1 — no `recoverable_report:export`.** `403`; the button is not rendered either (`templates.go:3437`, `templates.go:3509`, `templates.go:3639`).
- **34.e2 — a CSV writer error.** `500` `The export could not be generated.` (`recoverables.go:119-122`).
- **34.e3 — a response write error.** Logged, not surfaced — the headers are already sent (`recoverables.go:125-127`).

**Business rules**

- **D3:** exports are gated on their own `export` action, distinct from `view` (`permissions.go:165`).
- The list and the export share one options builder, so a filtered list and its export can never disagree about the query (`recoverables.go:45-47`).
- **Correction (F-G-016/F-E-03, Wave 4):** that shared builder now also carries the row scope. At the time of this audit the CSV applied none, so `recoverable_report:export` alone downloaded every counterparty, amount, requester and repayment note in the company — including rows the same caller is refused on `/requests/{id}`. `recoverableListOptions` sets `Viewer: a.recoverableViewer(r)` at the one place the screen and the download both pass through, so the file cannot drift from the screen (`internal/app/recoverables.go:97`, `recoverables.go:117-120`, `internal/store/recoverables.go:347-357`). See the same correction under UC-C-33.
- `money.FormatPaise` already includes `₹`, so the CSV's Amount column carries the symbol (AF-05).

**Data touched** — reads the register; writes `audit_log`.

**Non-functional / UX notes** — the whole body is buffered before any header is set, so a mid-serialisation failure can still answer a clean `500` rather than a truncated download (`recoverables.go:111-122`).

**Open questions** — the audit row is written **before** the body succeeds, so a failed export still records as exported (`recoverables.go:109` vs `recoverables.go:119`). Minor, but it makes the audit log's `export` action a record of intent rather than of delivery.

---

#### UC-C-35 — Prove a recoverable payment is absent from budget actuals

| | |
|---|---|
| **Goal** | "A deposit is not an expense. Prove that the money we paid out as a recoverable is nowhere in the variance grid or the monthly report, and is in the recoverable register." |
| **Primary actor** | Accountant / test engineer |
| **Supporting actors** | Manager, Requester |
| **Scope / level** | system · user-goal |
| **Trigger** | Settling a recoverable request, then comparing three screens |
| **Route(s)** | `GET /grid?month=YYYY-MM` (RT-36), `GET /reports/monthly?from=&to=` (RT-37), `GET /recoverables/list` (RT-23) |
| **Permission gate** | `grid:view` (grid is session-only in practice), `report:view`, `recoverable_report:view` |
| **Coverage IDs** | V2, V3 |
| **Priority** | critical |

**Preconditions**

1. A month `M` with a known, non-zero grid `Actual` — the seeded sample data plus at least one **budget** payment gives a baseline that is not zero (`seed.go:163-188`).
2. A head `H` under an active project, with a budget in `M`.
3. One **recoverable** request `R` (treatment `recoverable`) approved for a distinctive amount `A`, with a project and head so it can actually be paid (see exception 35.e1).
4. One **budget** request `B` approved for a distinctive amount `A2`, same head `H`, same month.
5. The caller can read all three screens.

**Postconditions (success)**

1. `payments` contains **two** rows for `M`, both `settlement='settled'`, one with `request_id = R` and one with `request_id = B`.
2. Grid `Actual` for head `H`, for project `P`, and for the `Company total` row each rose by exactly `A2` — **not** by `A2 + A`.
3. The monthly report's `Actual` for period `M` rose by exactly `A2`.
4. The recoverables register contains `R` with `Paid on` set and its `<tfoot>` total including `A`; `B` is absent from it.

**Postconditions (failure)** — a grid `Actual` that rose by `A2 + A` means the exclusion is broken and V2 fails.

**Main success scenario — the exact figures to compare**

1. **Baseline.** Open `GET /grid?month=M`. Record three numbers from the desktop table (`.gridwrap.d-only`): the head row's `Actual` cell for `H`; the `tr.project-row` `Actual` cell for `P`; and the `tfoot tr.total` `Actual` cell beside the `<span class="tlabel">Company total</span>` (`templates.go:123-190`, cell 5 of each row). Also record the `.metric-strip` tile labelled **Actual**, which is `short`-formatted (`₹x.xx L`) and therefore only a coarse check (`templates.go:131`).
2. **Baseline.** Open `GET /reports/monthly?from=M&to=M`. Record the row whose `data-label="Period"` is `M`, cell `data-label="Actual"` (`templates.go:3272`).
3. **Baseline.** Open `GET /recoverables/list`. Record the `<tfoot>` `data-label="Amount"` total and the `{n} shown` count (`templates.go:3547-3552`).
4. **Act.** Settle the **budget** request `B` for `A2` with `paid_on` inside `M` (UC-C-08).
5. **Act.** Settle the **recoverable** request `R` for `A` with `paid_on` inside `M` (UC-C-08 — the settlement flow is identical; nothing on the entry screen or the sheet mentions recoverability).
6. **Assert the grid.** Re-open `GET /grid?month=M`. The head `Actual`, the project `Actual` and the `Company total` `Actual` must each be `baseline + A2` **exactly**. The `Remaining` cell must have fallen by exactly `A2`.
7. **Assert the report.** Re-open `GET /reports/monthly?from=M&to=M`. The `Actual` for `M` must be `baseline + A2` exactly. The monthly report is computed **from** `Grid`, so this is a second reading of the same arithmetic, not an independent one (`store.go:1492-1503`).
8. **Assert the register.** Re-open `GET /recoverables/list`. `R`'s row must be present with `Paid on` = the settlement date, and the `<tfoot>` total must be `baseline + A` exactly. `B` must not appear at all.
9. **Assert the detail.** Open `GET /recoverables/{R}`. The card is headed `<span class="pill recoverable no-dot">Not in budget actuals</span>` and the **Payment** card shows `Amount paid` = `A` (`templates.go:3583`, `templates.go:3598-3605`).

**Alternate flows**

- **35.a (step 6) — the exclusion mechanism.** `Grid`'s actual expression is `SUM(CASE WHEN py.voided_at IS NULL AND COALESCE(pr.treatment,'') <> 'recoverable' THEN py.amount ELSE 0 END)`, joined `LEFT JOIN payment_requests pr ON pr.id=py.request_id`. The `COALESCE` is what keeps **historical** payments — `request_id IS NULL`, so `pr.treatment` is NULL — counting exactly as they always have (`store.go:1384-1392`).
- **35.b (step 6) — the row-visibility clause too.** A head with no budget appears in the grid only if it has a payment in the month; that `EXISTS` sub-query carries the **same** exclusion, so a head whose only payment is recoverable does **not** materialise as an `unbudgeted` row (`store.go:1395-1398`). A stronger form of the test therefore uses a head with **no** budget and **only** a recoverable payment, and asserts the head is absent from the grid entirely.
- **35.c (step 8) — recoverable pill on the queue.** While `R` was approved, its queue row's `Project / head` cell rendered `<span class="pill recoverable">Recoverable</span>` instead of the project path — a visible marker for the tester before settlement (`templates.go:718`).
- **35.d (step 3) — the dashboard's coarser figures.** `/recoverables`' tiles use `short`, so `₹1,00,000.00` displays as `₹1.00 L`. Assert on the `<tfoot>` of the **list**, or on the by-category table's `Outstanding` cell, both of which use `money` (`templates.go:3449`, `templates.go:3461`, `templates.go:3550`).
- **35.e (step 5) — voiding.** A voided payment is excluded from the grid by `py.voided_at IS NULL` and from the recoverable register by the join's own `AND py.voided_at IS NULL`, at which point `recoverableAmount` falls back to the approved amount — so voiding a recoverable payment returns the row to `Awaiting payment` in the register while removing nothing from the grid (it was never there). A linked payment cannot be voided anyway (UC-C-23), so this is reachable only for a historical row.

**Exception flows**

- **35.e1 — a project-less recoverable.** If `R`'s category does not require a project (ICD, security deposit, employee advance, other) the request carries `project_id`/`head_id` NULL, so the entry screen posts `head_id=0`. **Fixed in Wave 1 (migration v8, F-D-11/F-E-01/F-G-001, commit `633997b`) — this used to be suspected defect SD-07 / divergence DV-01, and is no longer one.** `payments.head_id` is nullable (`migrations.go:453`) and `validatePayment` accepts `HeadID==0` when the request's treatment is `recoverable` (`store.go:1922-1923`, caller at `store.go:1115`), so any of the four project-less categories settles in step 5 exactly like a project-bearing one.
- **35.e2 — a locked month.** `validatePayment` returns `ErrLockedMonth` → `409`; unlock `M` first (`store.go:1624-1626`).
- **35.e3 — the mobile grid.** At 390 px the desktop `.gridwrap` is `d-only` and the figures live in the accordion instead. The accordion contract is `.is-open` on a `<button class="acc-head">`, driven by `fervid-app.js`, **not** `<details>/<summary>`; built from `<details>` every project renders permanently collapsed (`PROGRESS.md:101-108`). Assert on the desktop project only, or drive the accordion the way the JS expects.

**Business rules**

- **V2:** a recoverable payment is a deposit, not an expense; it never counts as budget actuals (`store.go:1384-1386`).
- The exclusion is applied in **two** places in `Grid` — the actual sum and the row-visibility `EXISTS` — and nowhere else, because `Report` is built on `Grid` (`store.go:1388`, `store.go:1395-1398`, `store.go:1493`).
- **X6:** the `COALESCE(pr.treatment,'')` keeps historical, request-less payments counting as they always did (`store.go:1385-1386`).
- **V3:** the recoverable total is computed by a different query over a different definition, so the two totals are independent by construction (`recoverables.go:250-258`).

**Data touched** — reads `payments`, `payment_requests`, `heads`, `projects`, `budgets`, `month_locks`, `recoverable_categories`.

**Non-functional / UX notes** — the grid is the widest screen in the product. `baseline.spec.ts` captured `/` as `grid` for several phases and therefore photographed the dashboard instead, which is why the accordion defect above was invisible; the route list now says `/grid` (`PROGRESS.md:96-100`, commit `278918b`).

**Open questions** — none. (SD-07 is fixed — see 35.e1.)

---

#### UC-C-36 — Close a recoverable on payment, retaining its classification

| | |
|---|---|
| **Goal** | "Paying a deposit closes the request, but it must not quietly become an ordinary expense." |
| **Primary actor** | Accountant |
| **Supporting actors** | Requester, Manager |
| **Scope / level** | system · user-goal |
| **Trigger** | Settling a recoverable request |
| **Route(s)** | RT-04 then RT-06, then RT-23 / RT-25 to verify |
| **Permission gate** | `reservation:reserve`, `payment:create` **+ `payment:settle`** (F-D-10, Wave 3) |
| **Coverage IDs** | V7, V1, V6, L10 |
| **Priority** | critical |

**Preconditions**

1. A recoverable request, `status='processing'`, held by the caller. `head_id` need **not** resolve — a recoverable settles with a NULL head since migration v8 (see 36.e1, since fixed).
2. `recoverable_category_id` is populated (it is, on both write paths and after the **v6** back-fill — `requests.go:378-381`, `recoverables.go:71-78`; "v7" here was a mis-numbering, see UC-C-30 precondition 1).
3. `expected_return_date` and `repayment_notes` are set (mandatory for every recoverable — UC-C-31).

**Postconditions (success)**

1. `payment_requests.status='completed'` (or `partial_review` on a partial settlement).
2. **`treatment` is still `recoverable`.**
3. **`recoverable_category_id` is unchanged.**
4. **`expected_return_date` and `repayment_notes` are unchanged.**
5. One `payments` row with `request_id` set.
6. The row remains in the recoverable register, now with `Paid on` set and an ageing label derived from the return date rather than `Awaiting payment`.
7. The grid's `Actual` for the month is **unchanged** (UC-C-35).

**Postconditions (failure)** — any change to `treatment`, `recoverable_category_id`, `expected_return_date` or `repayment_notes` breaks V7: the register would lose the row or lose the information needed to chase the money.

**Main success scenario**

1. Accountant reserves the recoverable request from `/accounts-queue?tab=approved`; the row is marked with the `.pill.recoverable` (UC-C-03, `templates.go:718`).
2. Accountant records the payment and confirms **Fully settled** (UC-C-08).
3. `RecordPaymentForRequest` inserts the payment and executes `UPDATE payment_requests SET status=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='processing' AND processing_by=?` — the SET list touches **only** `status` and `updated_at` (`store.go:925`).
4. Accountant opens `/recoverables/list` and finds the row, now with a real `Paid on` and an ageing pill (`{n} days to go`, `Due today` or `{n} days overdue`).
5. Accountant opens `/recoverables/{id}`: the `.rh-meta` still carries `<span class="pill recoverable">Recoverable · {Category}</span>`, the card is still headed `Not in budget actuals`, and the **Payment** card now shows the figures (`templates.go:3568`, `templates.go:3583`, `templates.go:3597-3605`).
6. Accountant opens `/grid?month=M` and confirms the `Actual` figures did not move (UC-C-35).

**Alternate flows**

- **36.a (step 2) — a partial settlement.** `status='partial_review'`; the register's base filter excludes only `rejected`, `cancelled` and `withdrawn`, so the row stays live and the manager's decision (UC-C-10) moves it to `completed_partial`, which is also live (`recoverables.go:254`).
- **36.b — a rejected, cancelled or withdrawn recoverable.** Excluded from the register: a request that died before any money left never held money, so it is not outstanding (`recoverables.go:252-254`).
- **36.c — approved but not yet paid.** The register shows it with `Not yet paid` and the **Payment** card renders `Approved but not yet paid. The money has not left, so nothing is outstanding against a counterparty yet.` (`templates.go:3606-3608`).
- **36.d — the amount at risk switches source.** Before payment it is `COALESCE(approved_amount, amount)`; after payment it is the payment's own amount, so an under-paid recoverable's exposure drops to what actually left (`recoverables.go:256-258`).

**Exception flows**

- **36.e1 — a project-less recoverable.** ICD, security deposit, employee advance and `other` require no project, so `project_id`/`head_id` are NULL and the entry form posts `head_id=0`. **This was the sharpest live defect in this document — SD-07 / DV-01 — and is fixed as of Wave 1** (migration v8: `payments.head_id` nullable, `migrations.go:453`; `validatePayment` accepts `HeadID==0` for a recoverable, `store.go:1922-1923`, caller `store.go:1115`; commit `633997b`, `docs/qa/results/REPAIR-LOG.md`). The request now settles like any other and moves to `completed`/`completed_partial` in the same step (`recoverables.go:25-30`, `linking.go:244-247`).
- **36.e2 — the queue row for a recoverable hides the head.** The `Project / head` cell renders the `.pill.recoverable` in place of the path, so the accountant cannot see from the queue whether a head is even set (`templates.go:718`).
- **36.e3 — repayment tracking.** Out of scope; see UC-C-37.

**Business rules**

- **V7:** the settlement UPDATE touches only `status` and `updated_at`; the classification and the return information are what keep the row in the register after it closes (`store.go:925`, `recoverables_test.go:790-828`).
- The register's live set includes every non-terminated status, including `completed` and `completed_partial`, which is why a closed recoverable is still visible (`recoverables.go:254`).
- The accountant's screen flow does not branch on `treatment` — the entry form and confirm sheet are identical for a recoverable and a budget payment — but the store now does branch on it once, at `validatePayment`'s `headOptional` argument (`treatment == "recoverable"`, `store.go:1115`), which is what lets a project-less recoverable settle with no head. The budget-actuals exclusion is a separate, later branch in the reporting queries (`store.go:1388`).

**Data touched** — writes `payments`, `payment_requests.status/updated_at`, `audit_log`; reads `recoverable_categories`.

**Non-functional / UX notes** — the register's ageing label flips from `Awaiting payment` to a date-derived label the instant the payment lands, because `recoverableAgeing` switches on `paid_on` being non-empty (`recoverables.go:264-279`). That flip is a good single assertion for V7.

**Open questions** — none. (SD-07 is fixed — see 36.e1.)

---

#### UC-C-37 — Ask the product for a refund, a write-off, or repayment tracking

| | |
|---|---|
| **Goal** | (adversarial) "The money came back / was forfeited / was partly repaid — where do I record that?" |
| **Primary actor** | Administrator |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction (proof of absence) |
| **Trigger** | Guessing at plausible routes |
| **Route(s)** | none — every path below must be unrouted |
| **Permission gate** | n/a |
| **Coverage IDs** | X2, X3, X4 |
| **Priority** | critical |

**Preconditions**

1. The caller is signed in — an unauthenticated request would redirect to `/login` and prove nothing.
2. The caller holds every grant (an admin), so a `403` cannot mask the absence.

**Postconditions (success — correctly absent)**

1. Every `GET` below answers **404** (the catch-all `GET /` renders `notFound`).
2. Every `POST` below answers **404 or 405** — `405` because the catch-all matches the path but not the method (AF-06).
3. No `*Store` method name contains `refund` (case-insensitive), proven by reflection over `reflect.TypeOf(s.st)` (`app_integration_test.go:1551-1559`).
4. No table or column named for refunds, forfeiture or repayment amounts exists — `repayment_notes` is free text about **terms**, not a ledger of repayments (`requests.go:181-183`).

**Postconditions (failure)** — any 2xx/3xx from these paths means scope has silently grown.

**Main success scenario**

1. Administrator issues `POST /payments/1/refund` → 404 or 405 (`app_integration_test.go:1544-1550`).
2. Administrator issues `POST /requests/1/refund` → 404 or 405.
3. Administrator issues `GET /recoverables/1/repay`, `GET /recoverables/list/repay`, `GET /configuration/recoverable-categories/1/repay` → **404** each (`recoverables_app_test.go:315-319`).
4. Administrator issues `POST /recoverables/1/repay` → 404 or 405 (`recoverables_app_test.go:325-330`).
5. Administrator issues `GET /recoverables/1/forfeit`, `GET /recoverables/1/write-off`, `GET /recoverables/list/forfeit` → **404** each (`recoverables_app_test.go:338-342`).
6. Administrator issues `POST /recoverables/1/forfeit` → 404 or 405 (`recoverables_app_test.go:344-349`).
7. Administrator reads the product's own statement of scope: the dashboard's `.banner.locked` **Tracking the money coming back is not in this version** (`templates.go:3486-3492`), the detail's `.ab-note` `Recording the refund is out of scope for this version.` (`templates.go:3636`), the overdue banner's `This is a reporting flag only — chasing the money and recording its return happen outside this version.` (`templates.go:3577`), and the payment detail's `.ab-note` `Refunds and reversals are outside this version.` (`templates.go:325`).

**Alternate flows**

- **37.a — X1, no tax or TDS arithmetic.** No `tax` or `tds` column, table or route exists on the payment side. The settlement sheet says so in words: `Deductions such as TDS or retention were handled outside this system.` (`templates.go:489`), as does the payment detail's `Settlement` row (`templates.go:284`) and the entry form's hint `This system does no tax arithmetic. Record what you did so the trail explains the difference.` (`templates.go:434`). Note that a **vendor** record does carry a `Default TDS rate` field (`templates.go:1440`) — a stored attribute, not a calculation.
- **37.b — the immutability corollary.** X3 holds partly because a recorded payment cannot be edited or voided (UC-C-23): there is no way to express a reversal even by mutating the original (`app_integration_test.go:1533-1535`).

**Exception flows**

- **37.e1 — expecting 404 on a POST.** The router **cannot** produce it: the catch-all `GET /` matches the path and `ServeMux` answers 405 on the method. Do not narrow the assertion (`recoverables_app_test.go:320-324`, AF-06).
- **37.e2 — asserting on an unauthenticated request.** `RequireLogin` redirects to `/login` first, so the test must be signed in.

**Business rules**

- **X2:** no recoverable repayment tracking. `repayment_notes` captures the **terms**, and nothing records instalments (`recoverables.go:113`, `templates.go:3591`).
- **X3:** no refund or return-of-money route, and no store method whose name contains "refund" (`phase-3 spec:142`).
- **X4:** no forfeiture or write-off path converts a recoverable into a budget expense. The one write-off that exists — accepting a partial settlement — writes off a **balance not yet paid** and changes no classification (UC-C-10).
- The stated reason is architectural, not an oversight: these need an accounting adjustment process, not an edit to a completed payment (`templates.go:3490`).

**Data touched** — none.

**Non-functional / UX notes** — the product tells the reader the boundary in four places rather than leaving a dead end; a test may assert the copy as well as the status codes.

**Open questions** — none.

---

### 3.3 Notifications, reminders and conversation

#### UC-C-38 — Read my own notification centre

| | |
|---|---|
| **Goal** | "What has happened that needs me?" |
| **Primary actor** | Any signed-in user |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | The mobile top bar's bell, or `/notifications` directly |
| **Route(s)** | `GET /notifications` (RT-29) |
| **Permission gate** | **authenticated session only** — every row is already scoped to the caller by SQL, so a verb would add nothing |
| **Coverage IDs** | N1 |
| **Priority** | high |

**Preconditions**

1. The caller is signed in.
2. At least one `notifications` row exists with `user_id = caller.id` for the list to be non-empty.

**Postconditions (success)** — a `200` page titled `Notifications`; **nothing written** — reading the centre does not mark anything read.

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. User presses the bell — `<a class="m-icon" href="/notifications" aria-label="Notifications">✉` in `.m-topbar .m-actions`, carrying `<span class="dot">{n}</span>` when `Shell.Unread > 0` (`templates.go:74`).
2. `buildPageShell` has already resolved that count with `UnreadNotificationCount`, and a failure there costs the badge, never the page (`nav.go:149-159`, `inapp.go:148-152`).
3. System validates `scope` against the allow-list `{all, unread, mentions, reminders}`, defaulting to `all` (`inapp.go:18`, `inapp.go:35-38`).
4. System reads the caller's rows and the four counts (`inapp.go:39-48`).
5. System renders the `.page-banner`: eyebrow `Your activity`, `<h1>Notifications</h1>`, `.sub` `{n} unread of {m}` or `Everything here is read` (`templates.go:3387-3392`).
6. System renders the `.segmented` **kind filter** — four anchors, each with a `<span class="n">` count: `All`, `Unread`, `Mentions`, `Reminders`, hrefs `/notifications?scope={key}` (`templates.go:3398-3403`).
7. System renders `.notif-list`, one `<a class="notif">` per row (with `unread` appended while `read_at` is NULL), each holding `<span class="n-ico">{glyph}</span>`, `<span class="n-main"><b>{Title}</b><p>{Body}</p></span>` and a `<time>` (`templates.go:3405-3411`).
8. User reads a row's glyph: `✎` for a mention, `◷` for a reminder, `✓` otherwise (`inapp.go:22-31`).

**Alternate flows**

- **38.a (step 6) — the Mentions bucket.** `kind='mention'`, derived at write time from the event: `request_returned`, `request_on_hold`, `request_rejected` — the events where somebody wrote something addressed to you and is waiting on an answer (`inapp.go:53-64`).
- **38.b (step 6) — the Reminders bucket.** `kind='reminder'`: `reminder_pending`, `reminder_stale_reservation` (`inapp.go:58-59`).
- **38.c (step 6) — everything else is `activity`.** There is no `activity` tab; those rows appear under `All` and `Unread` only (`inapp.go:63`).
- **38.d (step 4) — the default page size.** 100 rows, newest first, ordered `created_at DESC, id DESC` because `CURRENT_TIMESTAMP` is second-resolution and a burst written in one request shares a timestamp (`inapp.go:99-107`).
- **38.e — an in-app row always exists.** `Notify` writes the in-app rows **first and unconditionally**; the email is a second, opt-in step, and a mail failure never costs the user their record (`service.go:36-38`, `service.go:60-67`). That is N1.

**Exception flows**

- **38.e1 — no rows.** A non-link `<div class="notif">` reading **Nothing here yet** / `You will be told when something needs you.` (`templates.go:3412-3417`).
- **38.e2 — an unknown `scope`.** Silently coerced to `all`, not a 400 (`inapp.go:36-38`).
- **38.e3 — not signed in.** `RequireLogin` redirects to `/login`.
- **38.e4 — another user's rows.** Unreachable: every read is `WHERE user_id=?`, so ownership is not a permission check that could be forgotten — it simply matches nothing (`inapp.go:17-22`, `inapp.go:89`).

**Business rules**

- **N1:** in-app is the default channel for **every** event, and it always fires (`service.go:60-61`, `events.go:1-3`).
- `kind` is derived **once**, at write time, from the event, so the filter strip never re-derives it and the two can never disagree. The plan's second copy in `internal/notify` was deliberately dropped (`inapp.go:22-23`, `events.go:29-32`).
- The centre needs no permission verb because the store scopes it (`app.go:408-411`, `inapp.go:12-16`).
- The notification row markup is `<a class="notif">` holding `.n-ico`, `.n-main` and a `<time>`. The plan's `<form>`-wrapping-a-`<button>` shape overflowed the centre by 1011 px once it had rows — caught by `ux.spec.ts`, not by eye (`PROGRESS.md:131-136`).

**Data touched** — reads `notifications`; writes nothing.

**Non-functional / UX notes** — `shell.spec.ts` covers `/notifications` on both devices (`shell.spec.ts:16`). **The bell is mobile-only**: `.m-topbar` is `display:none` above 860 px, and `navSpec` has no `/notifications` entry, so on desktop the centre has no link and no unread indicator at all (AF-14). Recorded as suspected defect SD-08.

**Open questions** — none.

---

#### UC-C-39 — Open a notification, and mark them all read

| | |
|---|---|
| **Goal** | "Take me to what this is about, and clear the badge." |
| **Primary actor** | Any signed-in user |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | Clicking a `.notif` row, or pressing **Mark all read** |
| **Route(s)** | `GET /notifications/{id}/open` → `303 {row.Href}` (RT-31); `POST /notifications/read` → `303 /notifications` (RT-30) |
| **Permission gate** | session only; ownership enforced in SQL. RT-30 carries `withCSRF`; **RT-31 does not** |
| **Coverage IDs** | N1 |
| **Priority** | high |

**Preconditions**

1. The caller is signed in.
2. For RT-31, a `notifications` row with that id **and** `user_id = caller.id`.
3. For RT-30 to render its button, `NotifCounts.Unread > 0`.

**Postconditions (success)**

1. **Open:** `read_at` set on that one row (only if it was NULL), then a `303` to the row's stored `Href` (`inapp.go:156-178`, `inapp.go:86-94`).
2. **Mark all:** `read_at` set on every unread row of the caller, then a `303` to `/notifications`; the bell's `.dot` disappears (`inapp.go:180-183`).
3. No other user's row is touched in either case.

**Postconditions (failure)** — no `read_at` written; the badge count unchanged.

**Main success scenario**

1. User clicks a `.notif` row, whose `href` is `/notifications/{id}/open` (`templates.go:3407`).
2. System reads the row scoped to the caller; a foreign or missing id yields `ErrNotFound` (`inapp.go:76-85`, `inapp.go:126-135`).
3. System marks it read with `UPDATE notifications SET read_at=? WHERE id=? AND user_id=? AND read_at IS NULL` (`inapp.go:157-158`).
4. System takes the destination from the **stored row**, never from the query string, and falls back to `/notifications` unless it begins with `/` (`inapp.go:90-93`).
5. System redirects `303`; the user lands on `/requests/{id}` for a request-scoped notification (`service.go:165-170`).
6. User returns to `/notifications` and presses **Mark all read** — a `<button class="btn outline" type="submit">` inside a POST form in `.pb-actions`, rendered only while something is unread (`templates.go:3393-3395`).
7. System clears every unread `read_at` for the caller and redirects `303`.
8. User reads the `.sub` `Everything here is read` and finds the bell carrying no `.dot`.

**Alternate flows**

- **39.a (step 3) — already read.** `RowsAffected()==0`, so the handler distinguishes "already read" from "not yours": it counts the row, and only a count of zero is `ErrNotFound`. A double click is therefore not an error (`inapp.go:162-177`).
- **39.b (step 4) — a row with no href.** `Href` is `''`, so the destination becomes `/notifications` (`inapp.go:90-93`, `service.go:166-168`).
- **39.c (step 6) — nothing unread.** The button is not rendered; posting anyway succeeds as a no-op (`inapp.go:180-183`).

**Exception flows**

- **39.e1 — somebody else's notification id.** `404` `That notification is not available.` — not yours, or gone; either way there is nothing to show (`inapp.go:77-82`).
- **39.e2 — not signed in.** Redirect to `/login`.
- **39.e3 — RT-30 without a CSRF token.** Refused by `withCSRF` (`app.go:412`).

**Business rules**

- Opening is a **GET** because the approved design makes each row a plain anchor, and an anchor cannot POST. The side effect is a per-user read receipt on the reader's own row — what every mail client does when you open a message — not a destructive action, so nothing here is worth a CSRF token (`inapp.go:63-70`).
- The destination comes from the stored row, so **there is no redirect for an attacker to aim** (`inapp.go:71-72`).
- Every mutation is scoped by `user_id`, so marking another user's row matches nothing rather than relying on a check that could be forgotten (`inapp.go:17-22`, `inapp.go:154-155`).

**Data touched** — writes `notifications.read_at`; reads `notifications`.

**Non-functional / UX notes** — a GET with a side effect means a link prefetcher or a crawler can mark a row read. Acceptable by the reasoning above, but worth knowing when a test asserts read state after a navigation.

**Open questions** — none.

---

#### UC-C-40 — Configure SMTP delivery, with the password from the environment only

| | |
|---|---|
| **Goal** | "Point the system at our mail server, without putting the password in the database." |
| **Primary actor** | Administrator |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | The **Email delivery** fieldset on the notification rules screen |
| **Route(s)** | `GET /admin/notifications` (RT-32), `POST /admin/notifications/smtp` → `303 /admin/notifications` (RT-33) |
| **Permission gate** | RT-32 `notification:view`; RT-33 `notification:edit`, `withCSRF` |
| **Coverage IDs** | N8 |
| **Priority** | critical |

**Preconditions**

1. Migration v7 has run, seeding `notification_settings`; `app_settings` already exists from v3 (`migrations_notifications.go:70-72`). v7 is no longer the last migration: v8, v9 and v10 have landed since (`migrations.go:336`, `:344`, `:352`), and **v9 seeds nine further `notification_settings` rows** with `ON CONFLICT(event) DO NOTHING`, so an administrator's edits to v7's twelve survive (F-F-06, `migrations_notifications.go:132-157`). This screen therefore lists **21** rules, not twelve.
2. The caller holds `notification:view` to read and `notification:edit` to write.
3. `FERVID_SMTP_PASSWORD` is set in the process environment if authentication is needed (`config.go:40`).

**Postconditions (success)**

1. Seven `app_settings` keys written: `smtp_host`, `smtp_port`, `smtp_username`, `smtp_from_name`, `smtp_from_addr`, `management_recipients`, `base_url` — the last with trailing slashes trimmed (`notifications.go:63-73`).
2. **No `app_settings` key ever contains a password**; `MailSettings` has no password field at all (`notifications.go:20-34`).
3. `303` to `/admin/notifications`.
4. One `audit_log` row from `SetAppSettings` (Phase 2's generic writer).

**Postconditions (failure)** — no keys written.

**Main success scenario**

1. Administrator opens `/admin/notifications`.
2. System loads every notification rule and the mail settings (`notifications.go:58-73`).
3. System renders the `.page-banner`: eyebrow `Administration`, `<h1>Notification rules</h1>`, `.sub` `Who is told what, and how. In-app notifications always fire; email is opt-in per event.` (`templates.go:3295-3301`).
4. System renders `<fieldset><legend>Email delivery</legend>` containing a POST form to `/admin/notifications/smtp` with seven fields: `#smtp_host` **SMTP host** (placeholder `smtp.example.com`), `#smtp_port` **Port** (`type="number"`), `#smtp_username` **Username**, `#smtp_from_name` **From name**, `#smtp_from_addr` **From address**, `#base_url` **Base URL for links** (placeholder `https://budget.example.com`), and `#management_recipients` **Management copy list** (placeholder `one@example.com, two@example.com`, hint `Copied on approvals and on urgent requests once they are approved.`) (`templates.go:3306-3319`).
5. Administrator reads the hint: `The SMTP password is read from the FERVID_SMTP_PASSWORD environment variable and is never stored in the database. There is deliberately no field for it here.` (`templates.go:3320`).
6. Administrator fills the fields and presses **Save email settings** (`templates.go:3322`).
7. System parses the port with `strconv.Atoi` (a non-numeric value silently becomes 0) and calls `SetMailSettings`, which trims every value (`notifications.go:76-90`, `notifications.go:63-73`).
8. System redirects `303`; the values round-trip on the next read.

**Alternate flows**

- **40.a (step 7) — port 0.** `SMTPMailer.Send` substitutes **587** when the stored port is 0 (`mailer.go:47-50`).
- **40.b (step 4) — the From header.** `formatFrom` composes `"{Name} <{addr}>"` when both are set, else the bare address; the SMTP **envelope** address is resolved separately by `envelopeAddr`, which prefers the stored address and otherwise extracts the `<…>` from the header (`service.go:320-327`, `mailer.go:78-90`).
- **40.c (step 4) — `base_url`.** It makes `{{link}}` absolute; a relative path is useless in a mail client. Empty means "render the bare path", which is what tests and a not-yet-configured install get (`notifications.go:30-33`, `service.go:187`).
- **40.d — the password's only source.** `config.Load()` reads `FERVID_SMTP_PASSWORD` with an empty default, and it reaches `SMTPMailer` as a constructor argument in both `main.go` and `App` (`config.go:22-25`, `config.go:40`, `main.go:97`, `app.go:203`).

**Exception flows**

- **40.e1 — no `notification:edit`.** `403` on RT-33; the screen's Edit buttons and both forms are gated too (`templates.go:3341`, `templates.go:3345`).
- **40.e2 — no `notification:view`.** `403` on RT-32; the sidebar entry `Notification rules` is hidden (`nav.go:89`).
- **40.e3 — no host configured, but an event fires.** `SMTPMailer.Send` returns `smtp host is not configured`; `a.fire` logs and swallows it, so the workflow action still succeeded and the in-app row still exists (`mailer.go:44-46`, `notifications.go:23-28`).
- **40.e4 — a message with no recipients.** `Send` returns `nil` without opening a socket (`mailer.go:51-54`).

**Business rules**

- **N8:** the SMTP password comes only from the environment and is never persisted or logged. `TestSMTPPasswordLoadsFromEnvOnly` and `TestAppSettingsRoundTripAndNeverStoresPassword` pin it (`coverage:114`, `config_test.go:72-80`).
- There is deliberately **no password field anywhere on this screen** (`templates.go:3288-3290`).
- Authentication is attempted only when a username is set: `smtp.PlainAuth("", username, password, host)` (`mailer.go:55-58`).
- The typed accessors are named `MailSettings`/`GetMailSettings`/`SetMailSettings` because Phase 2 already owns the generic `AppSettings`/`SetAppSettings` pair — the plan's names could not compile (`notifications.go:11-18`, `PROGRESS.md:125-130`).
- The message body is `text/plain; charset=UTF-8`, which matters because every amount carries a `₹` (`mailer.go:71-73`).

**Data touched** — writes `app_settings` (seven keys), `audit_log`; reads the process environment.

**Non-functional / UX notes** — a store-level test should assert that **no** `app_settings` row's value equals the configured password, not merely that no key is named for it. The screen's two forms are siblings inside one `<fieldset>` — never nested — for the reason recorded at UC-C-30 (`templates.go:3306-3331`).

**Open questions** — none.

---

#### UC-C-41 — Configure one event's email rule, recipients and templates

| | |
|---|---|
| **Goal** | "Turn email on for this event, decide who gets it, and write what it says." |
| **Primary actor** | Administrator |
| **Supporting actors** | Every recipient |
| **Scope / level** | system · user-goal |
| **Trigger** | **Edit** on an event row |
| **Route(s)** | `POST /admin/notifications/events/{event}` → `303 /admin/notifications` (RT-34) |
| **Permission gate** | `notification:edit`, `withCSRF` |
| **Coverage IDs** | N2, N8, N3 |
| **Priority** | critical |

**Preconditions**

1. `{event}` is one of the seeded rows — twelve at the time of this audit, 21 since migration v9 (F-F-06) — and `SetNotificationSetting` is an `UPDATE`, not an upsert, so an unknown event is rejected rather than silently creating a rule nothing will ever fire (`notifications.go:114-119`, `notifications.go:139-143`).
2. The caller holds `notification:edit`.
3. Both templates use only known `{{token}}` names (`notifications.go:109-114`).

**Postconditions (success)**

1. Eight columns updated on that row: `email_enabled`, `to_recipients`, `cc_recipients`, `include_requester`, `include_manager`, `include_accounts`, `subject_template`, `body_template` (`notifications.go:129-135`).
2. `label`, `audience` and `sort_order` are **not** in the SET list — they are seeded presentation, not admin input (`notifications.go:116-118`).
3. One `audit_log` row: `action='update'`, `entity_type='notification_setting'`, `summary='Updated notification rule {event}'` (`notifications.go:144-147`).
4. `303` to `/admin/notifications`.

**Postconditions (failure)** — no column changed; no audit row.

**Main success scenario**

1. Administrator reads the events table: `table.t-cards`, head `Event · In-app · Email · Goes to · Fixed To / CC · Edit` (`templates.go:3333-3343`).
2. Each row shows the label with the raw event key in a `.t-sub`; **In-app** is a static `<span class="pill good no-dot">On</span>`; **Email** is `On` or `Off`; **Goes to** is the seeded audience sentence; **Fixed To / CC** shows the two lists or `—`.
3. Administrator presses **Edit** (`button.btn.small.outline[data-open="ev-{event}"]`) (`templates.go:3341`).
4. System opens `.overlay#ev-{event}`, whose `.sh-head` shows the label and the audience (`templates.go:3347-3353`).
5. Administrator ticks `label.checkline` **Also send an email for this event** (`name="email_enabled"`) (`templates.go:3355`).
6. Administrator ticks any of the three **Who it goes to** boxes: **The requester** (`include_requester`), **The approver** (`include_manager`), **The Accounts group** (`include_accounts`) (`templates.go:3356-3360`).
7. Administrator fills `#to-{event}` **Always also send To** and `#cc-{event}` **Always copy (Cc)** (`templates.go:3361-3362`).
8. Administrator fills `#sub-{event}` **Subject** and `#body-{event}` **Message** (`rows="6"`) (`templates.go:3363-3364`).
9. Administrator reads the `.hint` listing the available fields, rendered from `notify.NotifyFieldNames()` as `{{number}}, {{amount}}, …`: `Available fields: … Anything else is rejected when you save, so a typo cannot reach an inbox.` (`templates.go:3365`, `service.go:247-251`).
10. Administrator presses **Save rule** (`templates.go:3370`).
11. System validates both templates with `notify.ValidateTemplate` before storing anything (`notifications.go:109-114`).
12. System writes the row, audits, and redirects.

**Alternate flows**

- **41.a (step 9) — the fourteen tokens.** `number, amount, approved_amount, payee, requester, approver, project, head, purpose, status, needed_by, submitted_on, processing_on, link` — one definition, used both to render and to validate (`service.go:228-251`).
- **41.b (step 6) — the Accounts group.** `include_accounts` resolves to real people through `UsersWithPermission("payment","process")`, so an event addressed to "the Accounts group" finds them **without naming a role anywhere** (`service.go:84-89`, `notifications.go:152-175`).
- **41.c (step 6) — the assigned accountant.** For `request_cancellation_requested` and `reminder_stale_reservation`, the in-app resolver addresses the **person who reserved it** (`req.ProcessingBy`) in preference to the whole Accounts group (`service.go:141-151`).
- **41.d (step 5) — N3, the approval email.** `request_approved` seeds `include_requester` and `include_accounts`; `resolveRecipients` additionally Cc's the **management list** for that one event, so an approval reaches Accounts + requester + management (`migrations_notifications.go:41-43`, `service.go:216-218`).
- **41.e (step 5) — email off.** `Notify` returns straight after the in-app write; no recipient resolution and no send (`service.go:65-68`).
- **41.f (step 2) — no `notification:edit`.** The Edit cell renders `—` and the per-row sheets are not emitted at all (`templates.go:3341`, `templates.go:3345`).

**Exception flows**

- **41.e1 — an unknown token.** `renderTemplate` collects the bad names and `ValidateTemplate` returns `unknown template field(s): {names}`; the handler re-renders the whole screen at **400** with a `.banner.warn` **That did not save** carrying the message (`service.go:258-281`, `notifications.go:110-113`, `templates.go:3303`).
- **41.e2 — an unknown event key.** `RowsAffected()==0` → `ErrNotFound` → `404` (`notifications.go:139-143`).
- **41.e3 — blank event.** `ErrValidation` "an event is required" → `400` (`notifications.go:121-123`).
- **41.e4 — a template that would break at send time.** Cannot happen: the same `renderTemplate` validates on save and renders on send, so a typo fails at the screen instead of silently emitting an empty string into an email nobody can debug (`service.go:253-257`).
- **41.e5 — a broken template on an existing row.** `writeInApp` falls back to `{Label} · {Number}` for the title and an empty body — a broken email template must never cost the user their in-app record (`service.go:91-113`).
- **41.e6 — no events configured at all.** `<td colspan="6" class="empty">No notification events are configured.</td>` (`templates.go:3342`).

**Business rules**

- **N2:** email is configurable **per event**; in-app is not, so the screen shows a static `On` pill rather than a control that would imply otherwise (`notifications.go:19-21`, `templates.go:3379-3383`).
- Templates are `{{token}}` substitution, **not** Go templates: notification text is admin-editable, and an editable string that reaches `text/template` is an execution surface (`service.go:253-257`, `migrations_notifications.go:26-27`).
- Recipient lists split on `,`, `;` and newline, lowercase and trim each address, and are deduped — `cc` is deduped against `to`, so nobody is both (`service.go:283-308`, `service.go:219-221`).
- An event with no resolved recipients sends nothing rather than erroring (`service.go:70-72`).

**Data touched** — writes `notification_settings`, `audit_log`; reads `users`, `user_roles`, `role_permissions`, `app_settings`.

**Non-functional / UX notes** — the `.hint`'s braces are HTML entities (`&#123;&#123;`) so the literal `{{token}}` renders rather than being consumed (`templates.go:3365`). Each sheet's id is `ev-{event}`, i.e. one overlay per event — twelve overlays on the page at the time of this audit, 21 since migration v9 added the F-F-06 events (`templates.go:3535`, ranging over the same settings list the table renders from).

**Open questions** — the sheets require JavaScript (`data-open`); there is no no-JS path to editing a rule.

---

#### UC-C-42 — Send a test email

| | |
|---|---|
| **Goal** | "Prove the SMTP settings work without waiting for a real event." |
| **Primary actor** | Administrator |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | Pressing **Send a test email** |
| **Route(s)** | `POST /admin/notifications/test` (RT-35) |
| **Permission gate** | `notification:edit`, `withCSRF` |
| **Coverage IDs** | N8 |
| **Priority** | medium |

**Preconditions**

1. The caller holds `notification:edit`.
2. `smtp_host` is set, or the send will fail by design.

**Postconditions (success)**

1. One message sent with subject `Fervid Budget test email` and body `This is a test from the notification rules screen. If you are reading it, SMTP is configured correctly.` (`notifications.go:140-144`).
2. HTTP **200**, the screen re-rendered with a `.banner.brand` **Done** / `Test email sent to {to}.` (`notifications.go:149`, `templates.go:3304`).
3. Nothing is written to any table.

**Postconditions (failure)** — HTTP **502**, the screen re-rendered with a `.banner.warn` **That did not save** / `The test email could not be sent: {transport error}` (`notifications.go:146-148`).

**Main success scenario**

1. Administrator fills `#test_to`, label **Send a test email to**, prefilled with their own address (`templates.go:3327`).
2. Administrator presses **Send a test email** (`templates.go:3328`).
3. System trims the address and falls back to the caller's own when blank (`notifications.go:127-130`).
4. System reads the mail settings and composes the From header from the name and address (`notifications.go:131-139`).
5. System calls the mailer directly — not through `Notify`, because there is no event and no request (`notifications.go:140-144`).
6. System re-renders the screen at `200` with the success notice.

**Alternate flows**

- **42.a (step 3) — blank recipient.** Falls back to `user.Email` (`notifications.go:128-130`).
- **42.b (step 4) — no from-name.** The From header is the bare address (`notifications.go:136-139`).

**Exception flows**

- **42.e1 — no host.** `smtp host is not configured` surfaces in the banner at **502** (`mailer.go:44-46`).
- **42.e2 — a refused connection or bad credentials.** The transport error surfaces verbatim at **502**. It is reported rather than swallowed: a test send that silently "succeeds" is worse than no test at all (`notifications.go:122-124`).
- **42.e3 — no `notification:edit`.** `403`; the form is not rendered.

**Business rules**

- The test bypasses `Notify` entirely, so no `notifications` row is written and no event rule is consulted.
- **502** (`StatusBadGateway`) is the deliberate status for an upstream mail failure — distinguishable from a 400 validation failure on the same screen (`notifications.go:146`).

**Data touched** — reads `app_settings`; writes nothing.

**Non-functional / UX notes** — both banners render at the top of the same screen, so a test can assert the pair `(status, banner class, banner text)`.

**Open questions** — the failure banner's bold line reads **That did not save**, which is wrong for a send that saved nothing. The template has one error slot and reuses its heading (`templates.go:3303`).

---

#### UC-C-43 — The twelve events and who each resolves to

> **Correction (post-audit, Wave 2/4, migration v9, commit `25411b8`):** nine more events exist now, closing finding F-F-06 — the six workflow actions that fired nothing (withdraw, re-raise, release, unhold, accept-partial, either cancellation decision), plus reservation-reassignment and the split concern event. **The vocabulary is 21 events, not twelve.** This use case's title, preconditions, table and business rules describe the original twelve as they stood at the time of this audit; they are still accurate as a description of *that* twelve, so the table is kept rather than rewritten. The nine additions are listed in a second table immediately after it. See `internal/notify/events.go:22-67` and `docs/qa/results/REPAIR-LOG.md` (Wave 2 · vocabulary, Wave 4 · wiring).

| | |
|---|---|
| **Goal** | "For each thing that can happen, exactly one rule row, one audience and one pair of templates." |
| **Primary actor** | Administrator (reading), System (firing) |
| **Supporting actors** | Requester, Manager, Accounts group, management list |
| **Scope / level** | system · subfunction |
| **Trigger** | Any workflow mutation that calls `a.fire`, plus the two scheduler batches |
| **Route(s)** | every mutation route; verified on `GET /admin/notifications` (RT-32) |
| **Permission gate** | n/a for firing; `notification:view` to read the table |
| **Coverage IDs** | N1, N2, N3 |
| **Priority** | critical |

**Preconditions**

1. Migration v7 seeded exactly twelve `notification_settings` rows (`migrations_notifications.go:28-68`, `migrations_notifications.go:105-114`). As of migration v9, nine more rows exist (F-F-06) — 21 total.
2. `notify.AllEvents` lists the original twelve, then v9's nine, in the screen's order (`events.go:71-79`).

**Postconditions (success)** — the table lists 21 rows (as of migration v9); each fires from at least one call site; each writes one `notifications` row per resolved recipient.

**Postconditions (failure)** — a row outside the seeded set, a missing row, or an event with no writer. (This read "a thirteenth row" at the time of this audit, when twelve was the whole vocabulary; migration v9 made the seeded set 21, so the test is "matches `notify.AllEvents`", not "is exactly twelve" — `events.go:71-79`.)

**The twelve events**

| EV | Event key | Seeded label | Seeded audience | Seeded include flags | Fired at |
|---|---|---|---|---|---|
| EV-01 | `request_submitted` | Request submitted | Approver | manager | `requests.go:172` |
| EV-02 | `request_edited` | Request edited before approval | Approver | manager | `requests.go:676` |
| EV-03 | `request_returned` | Returned for correction | Requester | requester | `requests.go:735` |
| EV-04 | `request_rejected` | Rejected | Requester | requester | `requests.go:744` |
| EV-05 | `request_approved` | Approved | Requester + Accounts group | requester, accounts | `requests.go:726` |
| EV-06 | `request_urgent` | Urgent request raised | Approver immediately, Accounts on approval | manager, accounts | derived in `fire` (`inapp.go:44-51`) |
| EV-07 | `request_on_hold` | Put on hold | Requester | requester | `linking.go:703` |
| EV-08 | `request_cancellation_requested` | Cancellation requested | Approver + assigned accountant | manager, accounts | `requests.go:824` |
| EV-09 | `payment_settled` | Payment recorded and settled | Requester + approver | requester, manager | `app.go:724` |
| EV-10 | `payment_partial_review` | Partial payment sent for review | Approver | manager | `app.go:726` |
| EV-11 | `reminder_pending` | Pending reminder | Whoever it is waiting on | manager | `reminders.go:19` |
| EV-12 | `reminder_stale_reservation` | Stale reservation | Assigned accountant | accounts | `reminders.go:22` |

**The nine events added by migration v9 (F-F-06, Wave 2 vocabulary / Wave 4 wiring)**

| EV | Event key | Fired at | Closes |
|---|---|---|---|
| EV-13 | `request_withdrawn` | `requests.go:916` | the approver's queue item vanishing with no explanation on a withdraw (UC-C's sibling, request lifecycle) |
| EV-14 | `request_reraised` | `requests.go:932` | the approver never learning a rejected request came back under a new number (D1) |
| EV-15 | `request_unheld` | `linking.go:803` | Accounts lifting a hold silently (see the now-fixed Open Question below and UC-C-15) |
| EV-16 | `reservation_released` | `linking.go:712` | SD-02: the release screen's "both are notified" promise, previously false |
| EV-17 | `reservation_reassigned` | `linking.go:755` | the new assignee not learning a reservation was handed to them (G11) |
| EV-18 | `payment_partial_accepted` | `linking.go:554` | the requester/accountant not learning a shortfall was accepted and the request closed `completed_partial` |
| EV-19 | `payment_partial_concern` | `linking.go:567` | the accountant not learning the approver disputed a shortfall instead |
| EV-20 | `request_cancellation_accepted` | `requests.go:1029` | SD-02-adjacent: neither cancellation decision fired anything before |
| EV-21 | `request_cancellation_declined` | `requests.go:1031` | same, for the decline branch |

Line numbers above are current as of this writing; `internal/app/linking.go` and `internal/app/requests.go` are both under active development in the same repair effort, so re-grep the event constant before citing a line number in a future test.

**Main success scenario**

1. A workflow action commits — say an approval.
2. `a.fire` reloads the request and calls `notify.Notify(ctx, event, req)` (`inapp.go:29-43`).
3. `Notify` loads the event's rule; an **unknown** event is not an error, it is nothing to deliver (`service.go:40-43`).
4. `Notify` loads the mail settings and builds a `RequestView` — display strings only, so a template can never reach into the model (`service.go:23-25`, `service.go:172-199`).
5. `Notify` resolves the Accounts group only when `include_accounts` is set (`service.go:84-89`).
6. `Notify` writes the **in-app rows first, unconditionally** — one per user id from `resolveInAppUsers` (`service.go:60-61`, `service.go:97-128`).
7. If `email_enabled`, `Notify` resolves `to` and `cc`, renders the subject and body, and sends (`service.go:65-81`).
8. Administrator confirms the delivery by reading the recipient's `/notifications` (UC-C-38) and, for email, the transport.

**Alternate flows**

- **43.a (step 6) — the requester's own event.** `include_requester && req.RequesterID != 0` (`service.go:135-137`).
- **43.b (step 6) — the manager's own event.** `include_manager && req.ManagerID != 0`, **except** for `request_urgent` on a request that is no longer `pending` (`service.go:138-140`).
- **43.c (step 6) — Accounts.** `include_accounts`, **except** for `request_urgent` while the request is still `pending`; and for EV-08/EV-12 the assigned accountant is addressed personally when one exists (`service.go:141-151`).
- **43.d (step 6) — dedupe.** The id list is deduped and zeroes dropped, so a requester who is also the manager gets one row, not two (`service.go:153-162`).
- **43.e (step 7) — management Cc.** Added for `request_approved` and for `request_urgent` **after** approval (`service.go:216-218`).
- **43.f (step 4) — the link.** `Link = base_url + /requests/{id}`, or `/requests` when the request has no id (`service.go:165-170`, `service.go:187`).
- **43.g (step 4) — the payee.** `RequestView.Payee` is `req.Vendor`, the display payee, so an email never names a blank payee (`service.go:184`, AF-09).

**Exception flows**

- **43.e1 — an unreadable request.** `a.fire` logs `notification skipped: request unreadable` and returns; the workflow action still succeeded (`inapp.go:34-39`).
- **43.e2 — a delivery failure.** `a.fire` logs `notification not delivered` and swallows it, deliberately: an SMTP outage must not make the approver believe their approval failed (`inapp.go:23-28`, `inapp.go:40-43`).
- **43.e3 — a missing requester or manager user row.** `newRequestView` tolerates `ErrNotFound` and leaves the names blank (`service.go:172-180`).
- **43.e4 — a broken subject template.** The in-app title falls back to `{Label} · {Number}`, or to `{event} · {Number}` when the label is empty; the email send returns an error instead (`service.go:102-109`, `service.go:73-76`).

**Business rules**

- The original twelve events are the G20 set; `AllEvents` now also carries the nine F-F-06 additions, and is the order the admin screen renders — the seeded `sort_order` is what `AllNotificationSettings` orders by (`events.go:6-79`, `notifications.go:95-98`).
- `resolveInAppUsers` mirrors `resolveRecipients` **in user-id space** — the same switches, two spaces — so what an admin ticks governs both channels (`service.go:130-132`).
- "The Accounts group" is resolved by permission (`payment:process`), never by role name; no template compares a role name anywhere in the product (`service.go:88`, `PROGRESS.md:231-233`).
- The plan's `kindFor(event)` classifier in `internal/notify` was dropped: `store.AddNotification` already derives `kind`, and a second copy could only drift (`events.go:29-32`).

**Data touched** — writes `notifications`; reads `notification_settings`, `app_settings`, `users`, `user_roles`, `role_permissions`, `payment_requests`.

**Non-functional / UX notes** — a coverage test should assert that every key in `AllEvents` has a seeded row **and** at least one `a.fire` (or scheduler) call site. At the time of this audit, nine `a.fire` sites plus the derived urgent plus the two scheduler batches accounted for all twelve; the count is larger now that F-F-06 added nine more call sites (see the table above).

**Open questions** — none. At the time of this audit, no event fired on **unhold** (UC-C-15), **release** or **reassign** (UC-C-17, UC-C-18), despite the reservation screen's note promising notification (SD-02). **Fixed** — all three now fire (`EventRequestUnheld`, `EventReservationReleased`, `EventReservationReassigned`; see the nine-event table above and the correction on SD-02).

---

#### UC-C-44 — Urgent submit and urgent approve, conferring no authority

| | |
|---|---|
| **Goal** | "Make sure the right people hear about an urgent request quickly — without letting urgency decide anything." |
| **Primary actor** | Requester (raising it), Manager, Accountant |
| **Supporting actors** | Management list |
| **Scope / level** | system · subfunction |
| **Trigger** | Submitting or approving a request with `urgent=1` |
| **Route(s)** | `POST /requests`, `POST /requests/{id}/approve` |
| **Permission gate** | `request:create` / `approval:approve` — **unchanged by urgency** |
| **Coverage IDs** | N6 |
| **Priority** | high |

**Preconditions**

1. `payment_requests.urgent = 1` (and, when the urgency mode demands it, `urgency_reason` is set — `configuration.go:59-64`).
2. The `request_urgent` rule row exists (EV-06) and, for email, is enabled.

**Postconditions (success)**

1. **On submit:** two events fire — `request_submitted` and `request_urgent`. The urgent one resolves to the **approver only**: `include_manager` applies, and `include_accounts` is suppressed while `status == "pending"` (`inapp.go:44-51`, `service.go:141`, `service.go:212`).
2. **On approval:** two events fire — `request_approved` and `request_urgent`. The urgent one now resolves to **Accounts** and **not** the manager, and the management list is Cc'd (`service.go:138-140`, `service.go:209-218`).
3. Exactly **one** `notification_settings` row backs both send points — `request_urgent` — with one pair of templates (`migrations_notifications.go:44-47`).
4. **The permission set is byte-identical to a non-urgent request.** No grant is added, no gate relaxed, no self-approval allowed.

**Postconditions (failure)** — any change in who may approve, or an urgent notification reaching Accounts before approval.

**Main success scenario**

1. Requester submits an urgent request.
2. `requestCreate` fires `EventRequestSubmitted`; inside `fire`, `req.Urgent && event == EventRequestSubmitted` fires `EventRequestUrgent` as well (`requests.go:172`, `inapp.go:44-51`).
3. `resolveRecipients` computes `urgentPreApproval := event == EventRequestUrgent && v.Status == "pending"` — true — so Accounts is excluded and the manager is included (`service.go:201-214`).
4. Manager receives a subject rendered from `URGENT: {{number}} — {{amount}} to {{payee}}` and a body carrying the purpose and the needed-by date (`migrations_notifications.go:46-47`).
5. Manager approves the request through the ordinary `approval:approve` gate, on the ordinary sheet, with the ordinary G8 self-approval refusal.
6. `requestApprove` fires `EventRequestApproved`; `fire` adds `EventRequestUrgent` again (`requests.go:726`, `inapp.go:46`).
7. `resolveRecipients` now sees `status != "pending"`, so `urgentPreApproval` is false: Accounts **is** included, the manager is **excluded**, and the management list is Cc'd (`service.go:209-218`).
8. Accounts finds the request in the queue exactly as any other approved request — urgency changes no ordering and no gate.

**Alternate flows**

- **44.a — a non-urgent request.** `fire`'s extra call is skipped entirely; only the ordinary event fires (`inapp.go:46`).
- **44.b — urgency disabled.** `urgency_mode=disabled` in Configuration; validation refuses an urgent submission at the request layer (`configuration.go:59-64`).
- **44.c — the Configuration note.** `Urgent requests follow the same approval rules. Urgency changes who is told and how soon, never who may decide.` (`configuration.go:64`).

**Exception flows**

- **44.e1 — expecting a separate urgent screen or queue.** There is none. Urgency is a flag on the request and a second notification event; no route, no tab and no gate keys off it (`grep` of `Urgent` across `app.go`/`linking.go` finds only display and the `fire` branch).
- **44.e2 — expecting urgency to permit self-approval.** G8 is absolute: `validateRequestInput` refuses `ManagerID == RequesterID` whatever roles the requester holds, and the Configuration screen renders it checked-and-disabled — not a setting (`requests.go:140-144`, `templates.go:3096-3100`).
- **44.e3 — the urgent rule disabled for email.** The in-app rows still fire; only the email is suppressed (`service.go:60-67`).

**Business rules**

- **N6:** urgent pre-approval and post-approval emails, and **no** approval authority (`coverage:112`).
- One urgent row, two send points: before approval it is the approver's problem; after approval it is Accounts', and only then does management care (`service.go:201-203`).
- The urgent event raises **on top of** the ordinary one, at both send points; `notify` resolves the recipients from the request's own status rather than from a second rule row (`inapp.go:44-45`).

**Data touched** — writes `notifications`; reads `notification_settings`, `app_settings`, `users`.

**Non-functional / UX notes** — the two send points are distinguished **only** by `req.Status` at the moment `Notify` runs. A test must therefore fire the event with the request in the right status, not merely call `Notify` twice.

**Open questions** — none.

---

#### UC-C-45 — Run the reminder scheduler

| | |
|---|---|
| **Goal** | "Nudge whoever is holding something up, once the wait crosses the configured threshold, and keep nudging on the configured cadence." |
| **Primary actor** | Scheduler (in-process) |
| **Supporting actors** | Manager (pending reminders), Accountant (stale reservations), Administrator (sets the thresholds) |
| **Scope / level** | system · subfunction |
| **Trigger** | Process start, then every hourly tick |
| **Route(s)** | none — an in-process goroutine. Thresholds are set on `POST /configuration` (RT-28) |
| **Permission gate** | n/a; thresholds need `config:edit` |
| **Coverage IDs** | N4, N5, S8 |
| **Priority** | critical |

**Preconditions**

1. `notification_settings` carries the `reminder_pending` and `reminder_stale_reservation` rows (EV-11, EV-12).
2. Thresholds are read per run: `reminder_pending_days` (default 3), `reminder_repeat_days` (default 1), `reminder_stale_days` (default 1) (`reminders.go:43-47`, `reminders.go:64-81`).
3. `now` is **injected** — `Scheduler` takes a `func() time.Time`, and `RunReminders` takes a `time.Time`, so the whole loop is deterministic under test (`reminders.go:13`, `reminders.go:54`).

**Postconditions (success)**

1. For every due request in each batch: one `notifications` row per resolved recipient, an email when the event is enabled, and `payment_requests.reminder_last_sent = now` (`reminders.go:38-47`, `store/reminders.go:131-134`).
2. Nothing else is written — no status change, no release, no audit row.
3. The next tick inside the repeat window sends nothing for the same request.

**Postconditions (failure)** — a request reminded twice inside the cadence, or a request past the threshold never reminded.

**Main success scenario**

1. `main` constructs the service and starts `Scheduler(ctx, time.Hour, func() time.Time { return time.Now().UTC() })` on the **shutdown** context, so Ctrl-C stops it with the server (`main.go:97-102`).
2. `Scheduler` runs `RunReminders` **once immediately**, then on every tick (`reminders.go:54-65`).
3. `RunReminders` reads the thresholds **once per run**, so a Configuration change takes effect on the next tick without a restart (`reminders.go:11-18`).
4. `RunReminders` runs the **pending** batch, then the **stale processing** batch, in that order (`reminders.go:19-23`).
5. Each batch short-circuits only if its event row is absent; there is deliberately **no `EmailEnabled` short-circuit**, because the in-app reminder always fires and `Notify` decides on its own whether an email follows (`reminders.go:27-33`).
6. **Pending batch** — `RequestsPendingReminder` selects `status='pending' AND COALESCE(on_hold,0)=0 AND submitted_at IS NOT NULL AND submitted_at <= now-PendingAfterDays AND (reminder_last_sent IS NULL OR reminder_last_sent <= now-RepeatEveryDays)`, ordered `submitted_at, id` (`store/reminders.go:87-105`).
7. **Stale batch** — `RequestsStaleProcessing` selects `status='processing' AND processing_at IS NOT NULL AND processing_at <= now-StaleAfterDays AND (reminder_last_sent IS NULL OR reminder_last_sent <= now-RepeatEveryDays) AND NOT EXISTS(SELECT 1 FROM payments py WHERE py.request_id=r.id AND py.voided_at IS NULL)`, ordered `processing_at, id` (`store/reminders.go:109-127`).
8. For each request, the batch calls `Notify` and **then** stamps `MarkReminderSent(req.ID, now)` — stamping after the send is what stops the next tick repeating it inside the cadence (`reminders.go:38-47`).
9. Manager receives `Reminder: {{number}} is still waiting on you` / `{{number}} for {{amount}} has been pending since {{submitted_on}}.` (`migrations_notifications.go:62-64`).
10. Accountant receives `Reminder: {{number}} is still reserved and unpaid` / `{{number}} has been in processing since {{processing_on}} with no settlement recorded.` (`migrations_notifications.go:65-67`).

**Alternate flows**

- **45.a (step 3) — admin-set thresholds.** Configuration's **Reminders and ageing** fieldset carries `reminder_pending_days` (**Remind after (days pending)**, hint `…Default 3.`), `reminder_repeat_days` (**Then repeat every (days)**, `…Default 1.`) and `reminder_stale_days` (**Reservation goes stale after (days)**, `…Default 1.`), with the note `Reminders are counted in calendar days, not working hours. A request put on hold is waiting on the requester by design and is never reminded about.` (`configuration.go:79-86`).
- **45.b (step 3) — garbage or blank.** `ReminderThresholds` falls back to the default whenever `Atoi` fails or the value is `<= 0`: a zero wait would remind on every tick and a negative one would never remind at all (`reminders.go:60-81`).
- **45.c (step 7) — the assigned accountant.** `reminder_stale_reservation` seeds `include_accounts`, and `resolveInAppUsers` addresses `req.ProcessingBy` personally when it is set, falling back to the whole Accounts group otherwise (`service.go:141-151`).
- **45.d (step 8) — reroute resets the clock.** `ResetReminder` clears `reminder_last_sent` when a request moves to a new person, since the new owner has not been waited on yet (`store/reminders.go:136-141`).
- **45.e (step 1) — the hourly tick.** Hourly rather than daily because the admin can set the waits to any number of days: an hourly tick makes the first reminder land within an hour of becoming due, and the per-request stamp is what stops it repeating (`main.go:92-96`).
- **45.f — graceful shutdown.** On Ctrl-C the server drains, then waits up to 15 s for the scheduler so a reminder in flight finishes writing before the database closes (`main.go:132-138`).

**Exception flows**

- **45.e1 — a request on hold.** Excluded from the pending batch: it is waiting on the requester by design, and nagging the approver about it is noise (`store/reminders.go:83-86`).
- **45.e2 — a reservation whose payment is already recorded.** Excluded from the stale batch by the `NOT EXISTS` sub-query; a voided payment does **not** exclude it, because the sub-query itself filters `voided_at IS NULL` (`store/reminders.go:121`).
- **45.e3 — a request with `submitted_at IS NULL`.** Excluded; there is no wait to measure (`store/reminders.go:98`).
- **45.e4 — a failure mid-batch.** `runReminderBatch` returns the error, which aborts the **rest of that batch** and, if it was the pending batch, the stale batch too — `Scheduler` discards the error and tries again next tick (`reminders.go:19-22`, `reminders.go:38-48`, `reminders.go:55`).
- **45.e5 — the event row missing.** The batch returns `nil` and does nothing (`reminders.go:29-31`).

**Business rules**

- **N4:** a daily manager reminder after the configured pending wait (default 3 days) (`reminders.go:44`, `store/reminders.go:87-105`).
- **N5 / S8:** a stale-processing nudge after the configured stale wait (default 1 day) (`reminders.go:46`, `store/reminders.go:109-127`).
- **Nothing is released automatically.** The reminder nudges; a person decides (UC-C-20, `models.go:408-410`).
- The old hardcoded 3 and 1 are gone; both are data now (`store/reminders.go:26-33`).
- The clock is a parameter so a test can pin "three days later" without sleeping (`store/reminders.go:28-31`).

**Data touched** — writes `notifications`, `payment_requests.reminder_last_sent`; reads `notification_settings`, `app_settings`, `payment_requests`, `payments`, `users`.

**Non-functional / UX notes** — the queue's own stale marker uses the **fixed** 24-hour `store.StaleReservation`, while this scheduler uses the **configurable** `reminder_stale_days`. Set `reminder_stale_days=4` and the queue still flags a 25-hour reservation as stale and links to a screen whose banner claims a reminder went out at the one-day mark, while no reminder has been sent (AF-08). Recorded as divergence DV-02.

**Open questions** — answered. At the time of this audit `calendarDaysBetween` existed, was documented as the intended semantics ("counts date boundaries crossed, not elapsed hours") and was tested, but **no query used it**; recorded as divergence DV-03. **Wave 2 answered it by deleting the helper** (F-F-08, decision 2 in `docs/qa/results/REPAIR-LOG.md`): a calendar boundary needs a timezone this app does not have, and under calendar semantics `reminder_repeat_days` degrades into nightly spam. Elapsed days are therefore the intended semantics, and the reasoning is recorded at `store/reminders.go:37-56`. The behaviour is unchanged — both batches still compare against `now.AddDate(0,0,-N)`, so a request submitted at 23:00 is reminded at 23:00 three days later, not at midnight (`store/reminders.go:94-95`, `store/reminders.go:116-117`). **One thing did not follow:** the Configuration note at `configuration.go:86` still says "counted in calendar days", which the decision said should read plain "days" — see the correction on DV-03.

---

## 4. Divergences found in the code

### 4.1 Code contradicts a spec or a shipped document

| DV | Claim | Spec / doc says | Code does |
|---|---|---|---|
| DV-01 | A recoverable request with no project can be paid | `V8` treats a project as *optional* on a recoverable, and four of the six seeded categories require none (`recoverables.go:25-30`) | **Fixed in Wave 1, migration v8 (F-D-11/F-E-01/F-G-001, commit `633997b`).** At the time of this audit, `payments.head_id` was `INTEGER NOT NULL REFERENCES heads(id)` and such a request could not be settled at all. As of migration v8, `payments.head_id` is nullable (`migrations.go:453`) and `validatePayment` accepts `HeadID == 0` when the request's treatment is `recoverable` (`store.go:1922-1923`, caller `store.go:1115`). The spec and the code now agree. |
| DV-02 | One stale-reservation threshold | Phase 5 made the wait admin-set data: "The old hardcoded 3 and 1 are gone" (`store/reminders.go:26-33`) | Two thresholds coexist. The queue and the `Resume` link use the fixed `store.StaleReservation = 24 * time.Hour` (`models.go:410`, `linking.go:787`, `store.go:1098`); the scheduler uses `reminder_stale_days` (`reminders.go:46`). Configuring the latter does not move the former. |
| DV-03 | Reminders count calendar days | `calendarDaysBetween` is documented as the semantics and the Configuration note says "Reminders are counted in calendar days, not working hours" (`store/reminders.go:49-58`, `configuration.go:86`) | At the time of this audit: neither query called it, both used `now.AddDate(0,0,-N)` (an elapsed-instant comparison), and `calendarDaysBetween` was referenced only by its own unit test. **Half-resolved in Wave 2 (F-F-08, decision 2 in REPAIR-LOG.md): `calendarDaysBetween` was deleted rather than adopted** — a calendar boundary needs a timezone this app does not have, and under calendar semantics `reminder_repeat_days` degrades into nightly spam (`store/reminders.go:37-56`, which now records the reasoning; the helper is gone, grep finds only comments about its removal). The queries are unchanged and still elapsed-instant. **The other half stands:** `configuration.go:86` still reads "Reminders are counted in calendar days, not working hours", which the decision said should say plain "days". So the divergence is now only between the Configuration copy and the query. |
| DV-04 | The settlement gate | The Phase-3 spec gates `POST /payments` on `payment,create` and the settlement decision behind the preview (`phase-3 spec:112-113`, `phase-3 spec:123`) | **Fixed in Wave 3 (F-D-10, commit `1fac147`).** At the time of this audit: `POST /requests/{id}/settlement-preview` needed `payment:settle` but the **writer** `POST /payments` needed only `payment:create`, so a caller with `create` and not `settle` could not open the sheet yet could post the settlement directly and `RecordPaymentForRequest` accepted it; `payment:mark_partial` was in the vocabulary and gated nothing at all. The write is no longer gated more weakly than its own pure preview: `POST /payments` is wrapped in `payment:create` **and** `payment:settle` (`app.go:446-447`), and `payment:mark_partial` is checked in `paymentCreate` when `settlement=partial` (`app.go:833-837`) — in the handler rather than as a route gate, because it applies to one value of one field. |
| DV-05 | `ListPayments` and the linkage columns | `linking.go:209-210`: "ListPayments does not select the linkage columns — RequestID is always nil on its rows — so the linkage is re-read per candidate" | `ListPayments` **does** select `py.request_id, settlement, partial_reason` and scans them (`store.go:1263`, `store.go:1296-1305`). The comment is stale and `recentPaidByActor` performs an unnecessary `Payment(ctx,id)` per candidate over up to 200 rows. |
| DV-06 | Two Configuration settings nothing reads | `configSections` declares `allow_direct_payments` ("Turning this on demands a written reason and is flagged in the audit log") and `payment_modes` ("Comma separated, in the order the payment form should offer them") (`configuration.go:69-74`) | Neither key is read anywhere outside `configSections` and the v3 seed (`migrations.go:247`). The payment modes are a hardcoded Go slice (`linking.go:993-995`), and no code path consults `allow_direct_payments` — X5 is absolute regardless of its value. |
| DV-07 | The queue's route and name | Phase-3 spec: `/requests/to-pay`, handler `accountsQueue` (`phase-3 spec:119`) | The route is `GET /accounts-queue` (`app.go:476`). `/requests/to-pay` does not exist. The nav calls it **Accounts queue**, the `h1` says **Payment queue**, the spec says **To pay**. |
| DV-08 | `ReleaseRequest`'s signature | Overview/Phase-3 spec: `ReleaseRequest(ctx, actor, id int64, confirmed, authorized bool)` (`phase-3 spec:54`) | `ReleaseRequest(ctx, actor, id int64, reason string, confirmed, authorized bool)` — a mandatory reason was added (G12) (`store.go:761`). |
| DV-09 | `AcceptPartial`'s signature and target state | Phase-3 spec: `AcceptPartial(ctx, actor, id) error`, moving to `'completed'` (`phase-3 spec:65-66`) | `AcceptPartial(ctx, actor, id, note string) error`, moving to **`completed_partial`** — a terminal state deliberately distinct from a clean `completed` (G14) (`store.go:964`, `store.go:983`). |
| DV-10 | `LinkablePaymentRequests`' return type | Phase-3 spec: `([]Request, error)` with `RequestListOptions` (`phase-3 spec:83`) | `(LinkableSet, error)` with `LinkableOptions`, splitting `Available` from `Unavailable` and carrying nine counts (`store.go:1090`, `models.go:412-443`). |
| DV-11 | `.metric-foot` has no CSS rule | `PROGRESS.md:368-375` lists `.btn.approve`, `.metric.warn`, `.metric.good` and `.metric-foot` as having **no rule**, "so today that third line in every metric tile renders unstyled" | All four have rules, added by Phase 6 (`fervid-ds.css:364-405`). `PROGRESS.md`'s own Phase-6 detail table records the fix at commit `a80e18f` (`PROGRESS.md:116`), so the Known-gaps entry contradicts the same file three sections earlier. |
| DV-12 | `unbuiltPrefixes` holds `/admin/notifications` | `PROGRESS.md:229-230`: "It now holds exactly one line, `/admin/notifications` (Phase 5)" | `var unbuiltPrefixes []string` — empty (`nav.go:250`). Every navigable screen resolves. |
| DV-13 | The blank-payee gap across the settlement flow | `fixtures.ts:178-188` carries a "KNOWN GAP" saying every Phase-3 screen reads `Request.VendorPayee`, so "the queue's Payee column, the entry screen's payee, the payment row's `vendor_payee` and the payment detail's `<h1>` are all blank" | Fixed at commit `fca6939`. `LinkablePaymentRequests` selects `COALESCE(NULLIF(v.name,''),r.vendor_payee,'')` as `Vendor` (`store.go:1136`), every screen reads `.Vendor` (`templates.go:717`, `templates.go:364`, `templates.go:584`), and the entry form copies it into the payment (`templates.go:390`, `linking.go:260`). The fixture comment is stale; `ISS-026`'s comment repeats it (`regression-issues.spec.ts:530-536`). |
| DV-14 | Migration count | The coverage spec's audit footer said "single monotonic migration sequence v1–v5" (`coverage:145`) at the time of this audit | At the time of this audit: v1–v7 (v1 permissions, v2 vendors, v3 requests, v4 payments-linking, v5 accounts reservation grants, v6 recoverable categories, v7 notification settings, `PROGRESS.md:39-48`). **Stale now, too.** Three repair-wave migrations have run since: v8 (`migrations.go:336`, `payments.head_id` nullable, F-D-11/F-E-01/F-G-001), v9 (`migrations.go:344`, nine new notification events, F-F-06) and v10 (`migrations.go:352`, `payments.vendor_id`, F-G-009/F-G-010). The chain is **v1–v10** as of this writing (`internal/store/migrations.go`, `Version:` declared at lines 42/99/150/272/304/320/327/336/344/352; the roster comment is `migrations.go:20-29`). |
| DV-15 | `store.RecoverableCategory` and `recoverableCategoryRules` | The Phase-4 plan expected `RequestInput.RecoverableCategoryID` | Phase 2 identifies a category by its **code**, so the table carries a `code` column that is the stable identity and `UpsertRecoverableCategory` never rewrites it (`recoverables.go:11-16`, `recoverables.go:211-213`, `PROGRESS.md:165-170`). Recorded here because the brief asks the `code` rule to be explicit. |

### 4.2 Suspected defects, ranked

Ranked by the harm a real user would suffer. **At the time of this audit, none of these was fixed** — they were findings, per the shared brief that instructed *report, do not repair*. That instruction was later reversed; four repair waves have since run (`docs/qa/results/REPAIR-LOG.md`), and several entries below are now marked fixed inline rather than removed, so the finding stays on record.

| SD | Severity | Defect | Concrete failure scenario | Evidence |
|---|---|---|---|---|
| SD-07 | **critical** — **FIXED, Wave 1** | A recoverable request whose category requires no project can never be paid | At the time of this audit: an administrator approves an ICD for ₹5,00,000 (category `icd`, counterparty required, project not). An accountant takes it from the queue, fills the entry form, presses **Payment settled →**, chooses **Fully settled**, and presses **Confirm and save payment**. The store answered `400` `valid head, date, and positive amount are required`, because the hidden `head_id` was `0` and `payments.head_id` was `NOT NULL`. **Fixed by migration v8 (F-D-11/F-E-01/F-G-001, commit `633997b`, decision 1 in REPAIR-LOG.md): `payments.head_id` is now nullable and `validatePayment` accepts a zero head for a recoverable settlement.** All four affected categories (`icd`, `security_deposit`, `employee_advance`, `other`) now settle normally. | `migrations.go:453` — the `head_id INTEGER REFERENCES heads(id)` line of `upPaymentsHeadNullable`'s `CREATE TABLE payments_v8`, with no `NOT NULL`; `store.go:1922-1923` (`validatePayment`'s `headOptional` guard), `store.go:1115` (caller passes `treatment=="recoverable"`), `recoverables.go:25-30`. Note `migrations.go:173` is a *different* `head_id` — `payment_requests`', from v3 — and is not the one this finding is about. |
| SD-06 | **high** — **FIXED, Waves 2/4** | The request form's recoverable-category list is hardcoded, so V4 is not delivered in the UI | At the time of this audit: an administrator adds "Retention deposit" on Configuration and it saves, appears in the categories table, and is enforced by the validator; a requester then opens the new-request form, chooses **Recoverable**, and the **Category** select offers only the six seeded options. Symmetrically, deactivating `icd` left it in the dropdown, and choosing it then failed validation with `choose a recoverable category`, which read like the requester's mistake. **Fixed (F-E-02/F-B-17): Wave 2 added `ListRecoverableCategories(ctx, activeOnly)` and Wave 4a rewrote the select to range over it.** | `templates.go:1828-1831` now renders `{{range .Categories}}<option value="{{.Code}}">`, with a `Choose a category` placeholder when none is selected and the hint `Categories are maintained by your administrator.` (`templates.go:1831`). See REPAIR-LOG.md Waves 2 and 4. |
| SD-05 | **high** — **FIXED, Wave 1** | The settlement POST accepts `head_id`, `vendor_payee` and `invoice_no` from the form without comparing them to the request | At the time of this audit: an accountant reserves request `PR-2026-000042` (approved against *Operations / Office Rent*) and posts `/payments` with `head_id` pointing at *Growth / Marketing* instead; the store validated only that the head was active and never checked it belonged to the request. **Fixed (F-D-01, commit `633997b`): `RecordPaymentForRequest` no longer reads `head_id`, `vendor_payee` or `invoice_no` from the form at all** — it re-reads all three from the request row it already holds open and overwrites whatever the form sent, before `validatePayment` ever runs. A forged value in the POST body is simply discarded. | `store.go:1110-1115` (the re-read and overwrite); `app.go:1988-1997` (`paymentInput`) still parses the form fields into `PaymentInput`, but they are replaced before use. |
| SD-02 | medium — **FIXED, Waves 2/4** | The reservation screen promises notifications that are never sent | At the time of this audit: an accountant releases a reservation, reads `The requester and the approver are both notified.` on the action bar, and tells the requester so, but no email and no in-app row was created. **Fixed.** Migration v9 (Wave 2, commit `25411b8`) declared `EventReservationReleased`, `EventReservationReassigned` and `EventRequestUnheld`; Wave 4 wired them. `requestRelease` now fires `notify.EventReservationReleased`, `requestReassign`(-the reservation kind) fires `notify.EventReservationReassigned`, and unhold fires `notify.EventRequestUnheld`. | `internal/notify/events.go:44-52`; call sites `internal/app/linking.go:712` (released), `:755` (reassigned), `:803` (unheld) — line numbers as of this writing, verify against current source before citing further. |
| SD-01 | medium — **the "no audit row" half is FIXED, Wave 2; the two-threshold mismatch is not** | The stale-reservation screen asserts a reminder that may not have been sent, and (at the time of this audit) could never evidence it | An administrator sets `reminder_stale_days=4`. A reservation crosses 25 hours, so the queue flags it stale (fixed 24 h, DV-02, still true) and links to the nudge screen, whose banner states `A reminder went out at the one-day mark.` No reminder has been sent, and none will be for three more days — **this half stands; DV-02 is not one of the four repair waves' fixes.** The audit-trail half is fixed: `MarkReminderSent` now writes a `remind` audit row (`Action: "remind", EntityType: "payment_request"`) with the reminder label and recipients (F-F-02, commit `25411b8`), so a genuine reminder now does appear in "Who has been told". | `linking.go:787-800` vs `reminders.go:46` (threshold mismatch, unchanged); `store/reminders.go:171-200` (`MarkReminderSent` now writes the audit row via `recordAuditTx`, fixed). |
| SD-08 | medium — **FIXED, Wave 4** | The notification centre is unreachable on desktop, and the unread count invisible | At the time of this audit: a manager signs in on a laptop, is sent a `payment_partial_review` notification, and has no way to see it, because `.m-topbar` is `display:none` above 860 px and `navSpec` had no `/notifications` entry. **Fixed (F-F-05).** `navSpec` now carries a top-level `notifications` item (`Href: "/notifications"`, no `Resource` gate — every row the screen returns is already scoped to the signed-in user by the store), with `Badge: "notifications"` for the unread count injected in `buildPageShell`. | `nav.go:53-61`. |
| SD-04 | low — **FIXED, Waves 1/4** | The reservation-conflict screen gives the wrong reason for two of its three causes | At the time of this audit: an accountant posts a reserve for a request that is **on hold** (or not approved), `ReserveRequest` returned the same `ErrForbidden` for all three causes, and the screen read `Someone else took this request before you` about a request nobody had reserved. **Fixed (F-D-02): Wave 1 gave `ReserveRequest` three distinguishable sentinels and Wave 4 taught the screen to branch on them.** `linking.go:61-65` declares `conflictTaken` / `conflictHold` / `conflictNotApproved`; `conflictCause` (`linking.go:109-123`) reads the sentinel **and** the row together, because they can disagree — `RecordPaymentForRequest` leaves `processing_by` set on a completed request, so the row wins on "does anybody hold it". The page title is now `Already taken` / `On hold` / `Not available to process` (`linking.go:80-92`). | `store.go:890-891` (`ErrAlreadyReserved`), `store.go:937`, `linking.go:55-92`, `linking.go:109-123`. |
| SD-03 | low — **FIXED, Waves 1/3** | A linked payment still accepts new attachments by route | At the time of this audit: `POST /payments/{id}/attachments` was gated only on `attachment:create` and `AddAttachment` had no `RequestID` guard, so a hand-rolled POST could add proof to an immutable payment — the one mutation S12 did not close. **Fixed twice over.** Wave 1 (F-D-08) made `AddAttachment` refuse a payment whose `request_id` is set, matching the edit and void guards (`store.go:1580-1587`). Wave 3 (F-A-03, decision 4 in REPAIR-LOG.md) added the ownership check on the write path as well — `attachmentUpload` resolves the payment and applies `canReadPayment` before staging anything (`app.go:1050-1070`) — kept even though F-D-08 makes it unreachable today, because the hole returns the day a free-standing payment becomes creatable. Both refusals are **404**, not 403, so the route is not an id-enumeration oracle (`app.go:1040-1048`). | `store.go:1580-1587`, `app.go:1050-1070`, `app.go:1040-1048`. |
| SD-09 | low | Two Configuration settings do nothing | An administrator ticks **Allow direct payments without a request** and saves. Nothing changes: X5 is unconditional. Likewise editing **Payment modes offered** leaves the entry form's six modes untouched. Both are recorded to the audit log as if they had an effect. | `configuration.go:69-74`; no reader outside `configSections` and `migrations.go:247`; `linking.go:993-995` hardcodes the modes. |
| SD-10 | low | Neither partial-review decision, nor a hold, is reachable without JavaScript | With scripting off, `#close-sheet`, `#concern-sheet` and `#hold-sheet` all start `hidden` and are opened only by `data-open`, so a manager cannot decide a partial settlement and an accountant cannot place a hold. The settlement sheet, by contrast, has a full-page `settlement_confirm` fallback, and the release screen's `data-when` reveal degrades safely. | `templates.go:911`, `templates.go:928`, `templates.go:2459`, `fervid-app.js:401-406`; contrast `linking.go:327-335`. |

### 4.3 Unverified

| UV | Item | Why |
|---|---|---|
| UV-01 | That an email actually reaches an inbox | No live SMTP server was contacted. Everything below `SMTPMailer.sendMail` is asserted only through the injectable seam (`mailer.go:30-33`). |
| UV-02 | Behaviour under real SQLite write contention across processes | The atomicity argument is read from the single conditional `UPDATE` plus `busy_timeout=5000` (`store.go:736-738`, `phase-3 spec:101`). I did not run the `-race` goroutine test. |
| UV-03 | The exact rendered figures in UC-C-35 | The comparison procedure is derived from the queries and templates, not from an executed run. A tester must record the real baseline numbers. |
| UV-04 | Whether `EventRequestUrgent`'s post-approval send reaches a *different* Accounts set than `EventRequestApproved` | Both resolve through `UsersWithPermission("payment","process")`, so they should be identical; not exercised (`service.go:88`). |
| UV-05 | That no `app_settings` row ever holds the SMTP password | Asserted from the absence of a password field in `MailSettings` and in the screen (`notifications.go:20-34`, `templates.go:3320`), and from the named test in the coverage matrix (`coverage:114`). I did not run the test. |
| UV-06 | Mobile rendering of the recoverables register and the notification centre at 390 px | Both are in `shell.spec.ts`'s `ROUTES` on both devices, which asserts no overflow and one chrome, but neither is in a flow spec. Restack behaviour of the eight-column register table is unverified by eye. |

---

## 5. Traceability

Every use case, the coverage-matrix IDs it satisfies, the routes it exercises and its primary actor.

| UC ID | Coverage matrix IDs | Routes | Primary actor |
|---|---|---|---|
| UC-C-01 | S3, D4 | `GET /accounts-queue` | Accountant |
| UC-C-02 | S4 | `GET /accounts-queue?q=` | Accountant |
| UC-C-03 | S1, S2, L8 | `POST /requests/{id}/record-payment` | Accountant |
| UC-C-04 | S1, S3, S4, S15, X5 | `GET /payments/new`, `GET /payments/new/options`, `POST /requests/{id}/record-payment` | Accountant |
| UC-C-05 | S5, S2 | `POST /requests/{id}/record-payment`, `GET /payments/new?request=`, `POST /requests/{id}/settlement-preview` | Second accountant |
| UC-C-06 | S14, S13 | `GET /payments/new?request={id}` | Accountant |
| UC-C-07 | S13 (D8) | `POST /requests/{id}/settlement-preview` | Accountant |
| UC-C-08 | S10, S13, S9, S15, L10, L11, Q4 | `POST /payments` | Accountant |
| UC-C-09 | S11, S13, L9, L11 | `POST /payments` | Accountant |
| UC-C-10 | S11, L11 | `GET /requests/{id}/partial-review`, `POST /requests/{id}/accept-partial` | Manager |
| UC-C-11 | S11 | `POST /requests/{id}/raise-concern` | Manager |
| UC-C-12 | S11, R6 | `POST /requests/{id}/accept-partial`, `POST /requests/{id}/raise-concern` | Administrator |
| UC-C-13 | L7, N1 | `POST /requests/{id}/hold` | Accountant |
| UC-C-14 | Q6, N7, L7 | `POST /requests/{id}/comment` | Requester |
| UC-C-15 | L7 | `POST /requests/{id}/unhold` | Accountant |
| UC-C-16 | S3, L7, S2 | `POST /requests/{id}/record-payment` | Accountant |
| UC-C-17 | S6, S7 | `GET /requests/{id}/reservation`, `POST /requests/{id}/release` | Accountant (holder) |
| UC-C-18 | S6, S7 | `GET /requests/{id}/reservation`, `POST /requests/{id}/reassign` | Administrator |
| UC-C-19 | S6 | `GET /requests/{id}/reservation`, `POST /requests/{id}/release` | Second accountant |
| UC-C-20 | S8, Q6, S6 | `GET /requests/{id}/reservation/stale` | Accountant (holder) |
| UC-C-21 | S12, Q4, X6 | `GET /payments/{id}` | Accountant |
| UC-C-22 | X6, S12 | `GET /payments` | Accountant |
| UC-C-23 | S12 | `GET /payments/{id}/edit`, `POST /payments/{id}/edit`, `POST /payments/{id}/void` | Accountant |
| UC-C-24 | X6 | `GET /payments/{id}/edit`, `POST /payments/{id}/edit`, `POST /payments/{id}/void` | Accountant |
| UC-C-25 | S15, X5 | `POST /payments` | Accountant |
| UC-C-26 | S15, S2, X5 | `POST /payments` | Accountant |
| UC-C-27 | S9, S13 | `POST /payments` | Accountant |
| UC-C-28 | S13, S10, S11 | `POST /payments`, `POST /requests/{id}/settlement-preview` | Accountant |
| UC-C-29 | Q4, L10, L9 | `GET /requests/{id}` | Requester |
| UC-C-30 | V4, V5 | `GET /configuration`, `POST /configuration/recoverable-categories` | Administrator |
| UC-C-31 | V1, V5, V6, V8, T9 | `GET /requests/new`, `GET /requests/new/fields`, `POST /requests` | Requester |
| UC-C-32 | V3, V2 | `GET /recoverables` | Accountant |
| UC-C-33 | V3, V6, V8 | `GET /recoverables/list`, `GET /recoverables/{id}` | Accountant |
| UC-C-34 | V3, D3 | `GET /recoverables/list.csv` | Accountant |
| UC-C-35 | V2, V3 | `GET /grid`, `GET /reports/monthly`, `GET /recoverables/list`, `GET /recoverables/{id}` | Accountant |
| UC-C-36 | V7, V1, V6, L10 | `POST /requests/{id}/record-payment`, `POST /payments`, `GET /recoverables/{id}` | Accountant |
| UC-C-37 | X2, X3, X4, **X1** (alt 37.a) | none — every path must be unrouted | Administrator |
| UC-C-38 | N1 | `GET /notifications` | Any signed-in user |
| UC-C-39 | N1 | `GET /notifications/{id}/open`, `POST /notifications/read` | Any signed-in user |
| UC-C-40 | N8 | `GET /admin/notifications`, `POST /admin/notifications/smtp` | Administrator |
| UC-C-41 | N2, N8, N3 | `POST /admin/notifications/events/{event}` | Administrator |
| UC-C-42 | N8 | `POST /admin/notifications/test` | Administrator |
| UC-C-43 | N1, N2, N3 | every mutation route; `GET /admin/notifications` | Administrator / System |
| UC-C-44 | N6 | `POST /requests`, `POST /requests/{id}/approve` | Requester, Manager |
| UC-C-45 | N4, N5, S8 | none (scheduler); `POST /configuration` | Scheduler |

### 5.1 Coverage confirmation

Every ID in the four sections this document owns is covered. Read the other way round:

| Section | ID | Covered by |
|---|---|---|
| Linking | S1 | UC-C-03, UC-C-04 |
| Linking | S2 | UC-C-03, UC-C-05, UC-C-16, UC-C-26 |
| Linking | S3 | UC-C-01, UC-C-04, UC-C-16 |
| Linking | S4 | UC-C-02, UC-C-04 |
| Linking | S5 | UC-C-05 |
| Linking | S6 | UC-C-17, UC-C-18, UC-C-19, UC-C-20 |
| Linking | S7 | UC-C-17, UC-C-18 |
| Linking | S8 | UC-C-20, UC-C-45 |
| Linking | S9 | UC-C-08, UC-C-27 |
| Linking | S10 | UC-C-08, UC-C-28 |
| Linking | S11 | UC-C-09, UC-C-10, UC-C-11, UC-C-12, UC-C-28 |
| Linking | S12 | UC-C-21, UC-C-22, UC-C-23 |
| Linking | S13 | UC-C-07, UC-C-08, UC-C-09, UC-C-27, UC-C-28 |
| Linking | S14 | UC-C-06 |
| Linking | S15 | UC-C-04, UC-C-08, UC-C-25, UC-C-26 |
| Recoverables | V1 | UC-C-31, UC-C-36 |
| Recoverables | V2 | UC-C-32, UC-C-35 |
| Recoverables | V3 | UC-C-32, UC-C-33, UC-C-34, UC-C-35 |
| Recoverables | V4 | UC-C-30 (SD-06 — "not delivered in the UI" — is fixed; the form's select now reads the table, `templates.go:1828-1831`) |
| Recoverables | V5 | UC-C-30, UC-C-31 |
| Recoverables | V6 | UC-C-31, UC-C-33, UC-C-36 |
| Recoverables | V7 | UC-C-36 |
| Recoverables | V8 | UC-C-31, UC-C-33 |
| Notifications | N1 | UC-C-13, UC-C-38, UC-C-39, UC-C-43 |
| Notifications | N2 | UC-C-41, UC-C-43 |
| Notifications | N3 | UC-C-41, UC-C-43 |
| Notifications | N4 | UC-C-45 |
| Notifications | N5 | UC-C-45 |
| Notifications | N6 | UC-C-44 |
| Notifications | N7 | UC-C-14 |
| Notifications | N8 | UC-C-40, UC-C-41, UC-C-42 |
| Scope boundaries | X1 | UC-C-37 alt 37.a (also owned by `UC-B-*` on the request side) |
| Scope boundaries | X2 | UC-C-37 |
| Scope boundaries | X3 | UC-C-37 |
| Scope boundaries | X4 | UC-C-37 |
| Scope boundaries | X5 | UC-C-04, UC-C-08, UC-C-25, UC-C-26 |
| Scope boundaries | X6 | UC-C-21, UC-C-22, UC-C-24, UC-C-35 |

Lifecycle and requester rows this document also closes: **L7** (UC-C-13/14/15/16), **L8** (UC-C-03), **L9** (UC-C-09, UC-C-29), **L10** (UC-C-08, UC-C-29, UC-C-36), **L11** (UC-C-08, UC-C-09, UC-C-10, UC-C-11), **Q4** (UC-C-08, UC-C-21, UC-C-29), **Q6** (UC-C-14, UC-C-20), **D3** (UC-C-34), **D4** (UC-C-01).

IDs referenced but **owned elsewhere**: `R1`–`R9`, `T1`–`T12`, `A1`–`A8`, `Q1`–`Q3`, `Q5`, `L1`–`L6`, `D1`, `D2`, `D5`, `C1`–`C4` (`UC-A-*`, `UC-B-*`).
