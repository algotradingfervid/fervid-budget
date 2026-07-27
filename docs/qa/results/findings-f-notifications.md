# Findings — TC-F Notifications & Reminders

Eight findings, ranked by severity. Confidence is "confirmed" only where reproduced two
independent ways (a live Playwright assertion plus a direct code citation, or two code paths);
"probable" where the code is unambiguous but no live reproduction was attempted; nothing here is
"unverified" — anything I could not confirm two ways is written up as a `NOT RUN` test case instead
of a finding.

---

### F-F-01 — The admin notification-rules screen is unusable by mouse click for eleven of its twelve event sheets, and for its own SMTP form

- **Severity** — critical
- **Confidence** — confirmed (reproduced two ways)
- **Type** — UX / rendering defect (not a permission or data-integrity bug)
- **Where** — `internal/app/templates.go:3347` (missing `hidden`); `web/static/fervid-ds.css:3714-3722` (no `.overlay[hidden]` companion rule)
- **Traces to** — N2, TC-F-030–035, TC-F-037, TC-F-047
- **What happens** — every one of the twelve per-event edit sheets is rendered as
  `<div class="overlay" id="ev-{{.Event}}">` with **no `hidden` attribute**, unlike every other
  `.overlay` in the product (`approve-sheet`, `return-sheet`, `reject-sheet`, `hold-sheet` all carry
  `hidden`, `templates.go:2206/2224/2235/2459`). `.overlay` itself is
  `position:fixed; inset:0; z-index:50; display:grid` with no `[hidden]` override anywhere in the
  stylesheet. On any visit to `/admin/notifications`, all twelve sheets are therefore laid out
  full-viewport simultaneously, and — because later DOM siblings paint on top of earlier ones at
  equal `z-index` — only the **last** one (`reminder_stale_reservation`, sort_order 12) is reachable
  by a pointer. Every other event's "Edit" button, and even the SMTP form's own "Save email
  settings" button (which sits further up the same page, also beneath the stack), is permanently
  covered.
- **Why it is wrong** — CSS cascade origin outranks specificity: an author-stylesheet rule
  (`.overlay{display:grid}`) always beats the user-agent stylesheet's `[hidden]{display:none}`,
  regardless of selector specificity, unless the author adds an explicit override. Every sibling
  `.overlay` in the app gets this right by carrying `hidden` in the markup and letting JS
  (`openDialog`/`closeDialog`, `fervid-app.js:116-138`) toggle it; only these twelve, added for
  Phase 5 (G20), were never given it.
- **Reproduction** —
  1. Sign in as Admin, `GET /admin/notifications`.
  2. In a real browser, click the "Edit" button on any row **other than** "Stale reservation" (the
     last row) — e.g. `[data-open="ev-request_returned"]`.
  3. The click never lands: Chromium's own hit-test at that screen position resolves to
     `#ev-reminder_stale_reservation`, not the target.
  4. Independently: run `document.querySelectorAll('.overlay').forEach(el => console.log(el.id,
     el.hidden, getComputedStyle(el).display))` on a freshly-loaded `/admin/notifications`. Every
     one of the twelve reports `hidden: false, display: "grid"`.
  This suite's own `test.fail()` at TC-F-047 pins reproduction (3); the diagnostic script used to
  produce (4) is not part of the deliverable but its output is quoted verbatim here.
- **Impact** — an administrator cannot, by mouse, change the email settings, recipients, or
  templates for eleven of the twelve events, and cannot save the SMTP form at all. The only sheet
  that opens is "Stale reservation." A keyboard user tabbing directly to a specific button and
  pressing Enter is **not** affected the same way (Enter on a focused button is not a
  coordinate-based hit-test), so this is a pointer-specific, not a total, outage — but for the
  overwhelming majority of admins this makes the whole per-event configuration surface of Phase 5
  non-functional.
- **Evidence** — Playwright's own actionability diagnostic (quoted above) and the `hidden:false`
  read on all twelve elements.
- **Suggested direction** — add `hidden` to the twelve sheet `<div>`s in `admin_notifications`
  (`templates.go:3347`), matching every other `.overlay` in the codebase. No CSS change is needed if
  that one attribute is restored, since `openDialog`/`closeDialog` already toggle it correctly.

---

### F-F-06 — Six real workflow actions notify nobody: withdraw, re-raise, release, unhold, accept-partial, and a cancellation decision

- **Severity** — high
- **Confidence** — confirmed (reproduced two ways — live assertion plus exhaustive grep of every `a.fire(` call site)
- **Type** — spec-divergence / coverage gap (not a bug in an existing rule — no event covers these at all)
- **Where** — `internal/app/requests.go:748-754` (withdraw), `:759-766` (reraise), `:850-858`
  (cancellation decide); `internal/app/linking.go:623-635` (release), `:710-720` (unhold),
  `:477-484` (accept-partial); confirmed absent from the complete list of `a.fire(` call sites in
  the package (nine, naming exactly the twelve seeded events between them)
- **Traces to** — N1, G20, TC-F-051–056
- **What happens** — driving each of these six actions for real (withdrawing a pending request,
  re-raising a rejected one, releasing a reservation, lifting a hold, accepting a partial payment,
  and declining a pending cancellation) adds **zero** rows to the notification list of the person
  who arguably needs to know: the manager for withdraw/reraise, the requester for
  release/unhold/accept-partial/cancellation-decide. `raise-concern` (approval:accept_partial's
  sibling action) has the identical gap by the same code reading, though it was not separately
  driven live.
- **Why it is wrong** — none of the twelve seeded events (`migrations_notifications.go`) names any
  of these transitions, and `a.fire(` is called from exactly nine call sites in `internal/app`,
  none inside any of these six handlers. This is not "an event failed to fire" (Section 8 of the
  test-case document found the twelve seeded events completely consistent with what actually fires)
  — it is that the product's own notification vocabulary never covers these transitions at all,
  even though each one changes what a specific other person needs to do next (a manager whose queue
  item vanished on withdrawal has no way to know why; a requester whose reservation was released has
  no way to know the invoice is unclaimed again).
- **Reproduction** — for each action: raise/approve a request to the right precondition status,
  capture the target person's notification count, perform the action via its real POST route with a
  valid CSRF token, re-read the count. TC-F-051 through TC-F-056 do exactly this and all pass
  (the "before equals after" assertion is true, which is what makes the gap real rather than a
  flaky test).
- **Impact** — a manager or requester left silently unaware of a state change that took a request
  out of their queue or off their plate. Lower-consequence than a security defect, but a real
  day-to-day usability gap on a feature (notifications) whose whole purpose is "tell people what
  they need to know."
- **Evidence** — TC-F-051 through TC-F-056 (unread/row counts unchanged before/after each action).
- **Suggested direction** — decide, per action, whether it needs a seeded event (most likely
  candidates: a `request_withdrawn`/`request_cancelled_outright` pair told to the manager, and a
  `reservation_released`/`request_unheld` pair told to the requester); this is a product-design
  decision, not a one-line fix, since it changes the seeded-event count from twelve.

---

### F-F-05 — The notification centre has no discoverable entry point on desktop

- **Severity** — high
- **Confidence** — confirmed (reproduced two ways)
- **Type** — UX / spec-divergence (G19)
- **Where** — `internal/app/nav.go:47-` (`navSpec`, no `/notifications` item); `internal/app/templates.go:3179-3189` (dashboard `pb-actions`, no notifications button); the only link anywhere is `templates.go:74` inside `.m-topbar`, which is `display:none` above 860px (`fervid-ds.css:3881-3884`, switched on only inside `@media (max-width:860px)` at `:4135`)
- **Traces to** — G19, TC-F-049
- **What happens** — at any viewport 861px or wider (essentially every desktop and most tablets),
  there is no button, link, or icon anywhere in the shell that leads to `/notifications`. The
  sidebar (`shell_sidebar`, built entirely from `navSpec`) has an item for the **admin** rules
  screen (`notif-admin` → `/admin/notifications`) but none for the user's own centre. The dashboard,
  which the approved mockup (`mockups/screens/dashboard.html:19`) shows with a "Notifications"
  button carrying an unread-count pill right next to "+ New request," ships with only the "+ New
  request" button. The route itself is not gated — typing the URL directly still returns 200 with
  the caller's own rows — so this is a discoverability gap, not an authorisation one.
- **Why it is wrong** — G19 ("in-app notification centre + unread bell badge") was added
  specifically to close a gap between the approved design and the original Phase 5 plan
  (`design-system-adoption-spec.md:38`); shipping it reachable only below an 860px breakpoint is an
  incomplete resolution of that same gap.
- **Reproduction** — sign in, resize to 1440x900, load `/`: the sidebar has no
  `a[href="/notifications"]`, and no visible element anywhere links to it; `.m-topbar` is
  `display:none`. Resize to 390x844: the bell (`.m-icon .dot`) appears and is the unread count.
  `GET /notifications` directly still 200s at either width.
- **Impact** — every desktop user — the majority of this product's real usage, given it is a
  finance back-office tool — has no way to discover their own notification centre exists, let alone
  reach it, unless someone tells them the URL.
- **Evidence** — TC-F-049 (sidebar has zero matches; no visible element matches at 1440x900; direct
  `GET /notifications` is 200).
- **Suggested direction** — add a sidebar nav item (unconditional — G19's own comment says it needs
  no permission verb) or a dashboard button matching the approved mockup, whichever the design
  review prefers; either is a small, well-scoped fix.

---

### F-F-02 — The stale-reservation screen's "a reminder went out" claim is decoupled from reality in two ways

- **Severity** — medium
- **Confidence** — confirmed (reproduced two ways)
- **Type** — data-integrity / UX (a screen asserting something the system cannot back up)
- **Where** — `internal/app/templates.go:1091` (unconditional banner text); `internal/store/models.go:410` (`StaleReservation = 24 * time.Hour`, hardcoded); `internal/store/reminders.go:38-46` (`reminder_stale_days`, admin-configurable, same default); `internal/store/reminders.go:131-134` (`MarkReminderSent`, no `recordAuditTx` call); `internal/app/linking.go:848-` (`auditPhrase`/`auditGlyph`/`auditTone`, no reminder-related `case`)
- **Traces to** — S8, N5, TC-F-044
- **What happens** — three separate things compound on one screen:
  1. The "reserved too long" screen (`GET /requests/{id}/reservation/stale`) is reachable the
     instant a reservation is taken — its only gate is `status=='processing' && ProcessingBy !=
     nil` (`linking.go:730-745`), no elapsed-time check — yet its banner (`templates.go:1085-1094`)
     unconditionally states "A reminder went out at the one-day mark." TC-F-044 reaches this
     screen seconds after reserving and the banner still asserts it.
  2. Even where the screen is reached the "normal" way (via the queue's own staleness badge,
     gated on the hardcoded `store.StaleReservation = 24h`), the sentence is about the *scheduler's*
     reminder, which uses the separately admin-configurable `reminder_stale_days` (default 1 day,
     but an admin can set it to anything). If an admin sets it to 3 days, the queue calls a
     24-hour-old reservation "stale" and sends the reader to a screen claiming a reminder already
     went out, when the scheduler will not send one for two more days.
  3. Even when a reminder genuinely has fired, "Who has been told" (the `.thread` trail sourced
     from the audit log) never shows it: `MarkReminderSent` updates `payment_requests` directly with
     no `recordAuditTx` call, and the trail's own vocabulary (`auditPhrase`/`auditGlyph`/`auditTone`)
     has no case for a reminder at all. This is the same gap `docs/superpowers/PROGRESS.md`'s
     "Known gaps" section recorded before Phase 5 ("the stale-reservation screen's 'reminder
     sent' trail line has no writer — no reminder is actually sent until Phase 5") — it is still
     true after Phase 5 shipped the reminders themselves.
- **Why it is wrong** — a screen that tells an accountant "a reminder already went out" should
  either be true or silent; here it can be false in two independent ways and can never be
  corroborated by the very history list sitting directly underneath it.
- **Reproduction** — TC-F-044: reserve a request, go straight to `/requests/{id}/reservation/stale`,
  observe the banner text present and the trail containing no "reminder" line.
- **Impact** — low direct risk (nobody's money moves because of this), but it actively misinforms
  the person deciding whether to chase a stale reservation, and the promised audit trail for
  reminders — the one thing that would let someone check the claim — does not exist.
- **Evidence** — TC-F-044's two assertions (banner text present; trail text never contains
  "reminder").
- **Suggested direction** — make the banner conditional on an actual `reminder_last_sent` value (and
  say when, not "at the one-day mark"), and give `MarkReminderSent` an audited counterpart, or a
  cheap non-audited timeline row, so this screen's own claim becomes checkable against its own
  history list.

---

### F-F-03 — The product's own explanatory copy hardcodes "three calendar days" in four places, independent of the admin-configurable reminder threshold

- **Severity** — medium
- **Confidence** — confirmed (reproduced two ways)
- **Type** — spec-divergence / UX
- **Where** — `internal/app/templates.go:2024` (new-request form hint), `:2515-2516`+`:2535-2536`
  (edit-request banner), `:3005-3006`/`:3027-3028` (returned-request "What happens next" thread)
- **Traces to** — N4, TC-F-048
- **What happens** — Phase 5 turned the pending-reminder wait from a hardcoded constant into
  `app_settings.reminder_pending_days`, editable on `/configuration` (default 3,
  `internal/store/reminders.go:44`). None of the four request-facing sentences that describe this
  wait were updated to read the live setting — they say "three calendar days" / "the three-day
  reminder clock" as literal template text. Changing `reminder_pending_days` to 7 on `/configuration`
  and reloading `/requests/new` still shows "If nothing happens for three calendar days...".
- **Why it is wrong** — the whole point of Phase 5 moving this from a constant to data was so an
  organisation could tune it; the copy explaining the rule to the very people it governs (requesters
  and approvers) was left describing the old constant.
- **Reproduction** — TC-F-048: set `reminder_pending_days=7` via `/configuration`, load
  `/requests/new?type=vendor_invoice`, read the Reminders hint — it still says "three calendar
  days."
- **Impact** — a requester or approver told, in the product's own words, a wait time that is wrong
  the moment an admin changes the setting — a minor but real trust/accuracy issue, compounding
  across four separate screens.
- **Evidence** — TC-F-048 (deterministic `test.fail()`; the hint contains "three calendar days," not
  "7 calendar days").
- **Suggested direction** — parametrise all four sentences off `{{index .Settings
  "reminder_pending_days"}}`, the same map these templates already read for `urgency_mode` and
  `attachment_max_mb`.

---

### F-F-07 — "Save corrections" on a returned request (explicitly not resubmitting) still tells the manager it was "edited and re-sent for approval"

- **Severity** — medium
- **Confidence** — confirmed (reproduced two ways)
- **Type** — information-flow / UX
- **Where** — `internal/app/requests.go:645-677` (`requestEdit`); the `a.fire(r,
  notify.EventRequestEdited, req.ID)` call at `:676` sits **outside** the `if
  r.FormValue("submit_action") == "resubmit"` block at `:663-665`; seeded subject template
  `"{{number}} was edited and re-sent for approval"` (`migrations_notifications.go:32-34`)
- **Traces to** — N1, A8, TC-F-057
- **What happens** — the `request_returned` screen offers two buttons: "Save corrections"
  (`submit_action=save`, leaves the request in `returned` status) and "Resubmit for approval"
  (`submit_action=resubmit`, sends it back to `pending`). `requestEdit` only calls
  `SubmitRequest` — the thing that actually re-sends it — for the resubmit path, but calls
  `a.fire(EventRequestEdited, ...)` unconditionally for both. Clicking "Save corrections" alone
  fires the same "was edited and re-sent for approval" notification to the manager as clicking
  "Resubmit," even though the request demonstrably was not re-sent (its status is still `returned`
  immediately afterward).
- **Why it is wrong** — the notification's own wording ("re-sent for approval") is a factual claim
  that is false exactly in the case the button ("Save corrections," not "Save and resubmit") was
  designed to make: staying put without re-sending.
- **Reproduction** — TC-F-057: raise a request, have it returned, click "Save corrections" (not
  Resubmit) as the requester, and observe the manager's notification count increase by one with a
  title containing "edited," while the request's own page still shows the "sent this back" banner
  (i.e. status is still `returned`).
- **Impact** — a manager reading "re-sent for approval" and expecting to decide again, when nothing
  is actually back in their queue yet — a minor communication defect, not a security one, but a
  believable source of the exact kind of confusion the design's own comment on `requestEdit`
  ("saving and then forgetting to send it back is how a returned request sits for a week with
  nobody waiting on it") was trying to prevent — this makes the opposite mistake possible: believing
  something was sent that was not.
- **Evidence** — TC-F-057 (manager's notification count +1 with an "edited" title; requester's own
  page still shows "sent this back" afterward).
- **Suggested direction** — only fire `EventRequestEdited` (or fire a distinct, correctly-worded
  event) inside the `submit_action == "resubmit"` branch; a plain save while `returned` needs no
  email/in-app row at all, since the manager is already waiting and nothing changed on their side.

---

### F-F-04 — `phase-5-notifications-spec.md` is stale and contradicts the shipped code throughout; the later `design-system-adoption-spec.md` (G19/G20) is the actual authority

- **Severity** — informational
- **Confidence** — confirmed (reproduced two ways — direct text comparison of both spec files against the code)
- **Type** — spec-divergence (documentation only; no product defect)
- **Where** — `docs/superpowers/specs/2026-07-25-phase-5-notifications-spec.md` (whole document) vs. `internal/store/migrations_notifications.go`, `internal/notify/*.go`; resolved by `docs/superpowers/specs/2026-07-25-design-system-adoption-spec.md:38-40,83-87`
- **Traces to** — N1, N2, G19, G20, D6, D7
- **What happens** — the phase-5 spec says: six seeded events (not twelve); migration `v5` (the
  code uses `v7`); explicitly out of scope, "a persistent in-app `notifications` table" (the
  code has exactly that table, `migrations_notifications.go:89-100`); `text/template` with a
  `money`/`short` FuncMap for rendering (the code uses a hand-rolled `{{token}}` substitution that
  deliberately never reaches `text/template`, `service.go:253-274`, precisely to avoid giving
  admin-editable strings a Go-template execution surface); a single `request_approved_urgent`/
  `request_submitted_urgent` pair of events (the code has one `request_urgent` event whose audience
  depends on `req.Status`). None of this is a code defect — the code is newer and is the audited
  authority per this whole exercise's own instructions — but a reader who consulted the phase-5 spec
  in isolation would draw materially wrong conclusions about the product.
- **Why it diverged, not just that it did** — `design-system-adoption-spec.md`'s own gap list
  records the cause precisely: G19 ("In-app notification centre + unread bell badge... explicitly
  out of scope in the P5 spec... Owner: P5") and G20 ("12 notification events... 6 seeded...
  Owner: P5") are dated the same day as the phase-5 spec and assign Phase 5 itself the job of closing
  both gaps — i.e. a later decision superseded the earlier plan within the same phase, and the
  phase-5 spec file was never updated to reflect its own revision.
- **Reproduction** — read `phase-5-notifications-spec.md` sections 2, 5.1, 5.4, 11 ("Out of scope");
  compare each claim against the cited code location; separately read
  `design-system-adoption-spec.md:38-40` and `:83-87` for the resolution.
- **Impact** — none to the running product; a real risk to anyone (human or agent) who plans future
  work from the phase-5 spec file without also reading the adoption spec's amendments.
- **Evidence** — side-by-side text quoted above.
- **Suggested direction** — either mark `phase-5-notifications-spec.md` as superseded at its top (the
  way this document does inline) or fold the G19/G20 resolution back into it; no code change.

---

### F-F-08 — `calendarDaysBetween` is dead code; the reminder queries use elapsed 24-hour arithmetic, not true calendar-day boundaries

- **Severity** — informational (Go-level; not independently browser-testable — see the audit's `NOT RUN` boundary)
- **Confidence** — confirmed (two code paths: the function's zero production call sites, and the actual query logic that replaced it)
- **Type** — spec-divergence
- **Where** — `internal/store/reminders.go:49-58` (`calendarDaysBetween`, defined, documented, unit-tested at `reminders_test.go:32-43`); `internal/store/reminders.go:94-95,116-117` (`RequestsPendingReminder`/`RequestsStaleProcessing`, the only two production call sites that would plausibly use it — neither does)
- **Traces to** — N4, N5
- **What happens** — `calendarDaysBetween`'s own doc comment promises "counts date boundaries
  crossed, not elapsed hours," and it is exercised by its own unit test. Grepping every
  `calendarDaysBetween(` call site in non-test code returns zero matches — its only callers are
  in `reminders_test.go`. The two functions that decide reminder eligibility instead compute
  `now.UTC().AddDate(0, 0, -th.PendingAfterDays)` / `-th.StaleAfterDays` and compare raw timestamps
  (`r.submitted_at <= ?` / `r.processing_at <= ?`) — elapsed-time arithmetic, not calendar-day-
  boundary arithmetic. The two give different answers near a day boundary (e.g. a request submitted
  at 23:50 has, by `calendarDaysBetween`, "waited one calendar day" ten minutes later at 00:00; by
  the actual `AddDate` comparison, it has waited essentially zero elapsed days).
- **Why this matters** — N4/N5 ("daily reminder after 3 calendar days" / "stale nudge after 1
  calendar day") are true only under the elapsed-time reading the code actually implements, not the
  calendar-day reading its own helper function and docs assumed.
- **Reproduction (Go-level only — this is exactly the honest boundary the audit brief asks for)** —
  `grep -rn "calendarDaysBetween(" internal/store/*.go` outside `_test.go` returns nothing;
  `internal/store/reminders_test.go: TestRequestsPendingReminderHonoursConfiguredThreshold` and
  `TestRequestsStaleProcessingSkipsSettledAndHonoursThreshold` exercise the actual `AddDate`-based
  behaviour and pass, while `TestCalendarDaysBetweenCountsDateBoundaries` exercises a function
  nothing in production calls.
- **Impact** — none to correctness of the currently-shipped behaviour (the `AddDate` logic is
  internally consistent and does throttle correctly); the risk is purely documentary — a future
  maintainer reading `calendarDaysBetween`'s comment would reasonably expect calendar-day semantics
  that are not what runs.
- **Evidence** — the grep result (zero non-test call sites) and the two functions' actual query
  bodies, both quoted above.
- **Suggested direction** — either wire `RequestsPendingReminder`/`RequestsStaleProcessing` through
  `calendarDaysBetween` (matching the documented intent) or delete the unused function and its test,
  and correct N4/N5's wording from "calendar days" to "24-hour periods."
