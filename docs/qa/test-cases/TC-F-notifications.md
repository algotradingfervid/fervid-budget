# TC-F — Notifications & Reminders

Audit area **F** (`notifications`). Covers the in-app notification centre (`/notifications`),
the admin rules screen (`/admin/notifications`), SMTP/password handling, reminder
configuration, and the event vocabulary in `internal/notify` — **twelve events when this
suite ran, twenty-one now**; see the correction under "A load-bearing fact" below, which
governs every "twelve" in this document.

## Sources read in full

`internal/notify/{service,events,mailer,reminders}.go`; `internal/store/{notifications,inapp,reminders,badges,settings,migrations_notifications}.go`;
`internal/app/{notifications,inapp,configuration,requests,linking,app,nav}.go` (handlers only, not
modified); `internal/app/templates.go` (`admin_notifications`, `notifications`, `reservation_stale`,
`request_sheets`, `request_returned`, `request_cancel` blocks); `docs/superpowers/specs/2026-07-25-phase-5-notifications-spec.md`;
`docs/superpowers/specs/2026-07-25-design-system-adoption-spec.md` (gaps G19/G20, decisions D6/D7);
`docs/superpowers/PROGRESS.md`; `docs/superpowers/specs/2026-07-25-payment-requests-coverage.md` (N1–N8, S8);
existing Go tests in `internal/notify/*_test.go`, `internal/store/{notifications,reminders}_test.go`,
`internal/app/inapp_app_test.go`.

**UC-C does not exist yet** (`docs/qa/use-cases/UC-C-settlement-recoverables-notifications.md` was
checked and is absent at the time of writing — only `UC-A-platform-rbac-admin.md` exists in
`docs/qa/use-cases/`). Every "traces to" cell below therefore cites coverage-matrix IDs only
(`N1`–`N8`, `S8`, plus code-comment gap/decision IDs `G8`, `G19`, `G20`, `D6`, `D7` from
`design-system-adoption-spec.md`), not a UC ID.

## A load-bearing fact this whole document depends on

The **phase-5-notifications-spec.md is stale** and was **superseded** by
`2026-07-25-design-system-adoption-spec.md` gaps **G19** and **G20**
(`design-system-adoption-spec.md:38-40`): the phase-5 spec explicitly lists "a persistent in-app
`notifications` table" as **out of scope** and seeds only **six** events; the shipped code has
exactly that table (`internal/store/migrations_notifications.go:89-100`) and seeds **twelve**
events in v7 (**twenty-one** since v9 — see the correction below), because G19/G20 record a later
decision to close those two gaps inside Phase 5's own delivery. The code is authoritative (per `AGENT-BRIEF.md` source-of-truth order); every place this
document says "spec says X, code does Y" below is this same root cause, not a new one each time.

**Correction — the vocabulary is twenty-one events, not twelve.** Migration **v7** seeds twelve,
which is what this suite measured and what every "twelve" below records. Migration **v9** adds nine
more with `ON CONFLICT(event) DO NOTHING`, so an administrator's edits to v7's twelve survive — that
is why it is a migration and never a re-seed (`internal/store/migrations.go:344-346`,
`internal/store/migrations_notifications.go:146-155`). `notify.AllEvents` is therefore v7's twelve
followed by v9's nine, twenty-one in all (`internal/notify/events.go:71-79`; the nine constants at
`:37`, `:41`, `:44`, `:48`, `:52`, `:56`, `:60`, `:65`, `:66`). The added events are
`request_withdrawn`, `request_reraised`, `request_unheld`, `reservation_released`,
`reservation_reassigned`, `payment_partial_accepted`, `payment_partial_concern`,
`request_cancellation_accepted` and `request_cancellation_declined`. This is finding **F-F-06**,
fixed in REPAIR-LOG.md Wave 2, commit `25411b8` (the vocabulary and the migration) and Wave 4,
commit `709dfa6` (the handler wiring). Every recorded expected/actual/verdict below is the frozen
`30edd6a` measurement and stands as run; it is the *count* that has moved.

## Environment constraints that shape what "executed" means here

- This is a **browser-driven** audit: SMTP cannot be read, so every email-layer behaviour is tested
  only through what the admin screen states, what the test-send reports, and what the in-app/audit
  trail record. No real mail is sent and no mail server is stood up.
- The reminder scheduler (`cmd/server/main.go:97-101`) runs on **real wall-clock time**
  (`time.Now().UTC()`, hourly tick) with **no injected clock**, no HTTP trigger, and no flag to
  force a run — confirmed by reading `cmd/server/main.go` and `playwright.config.ts`'s `webServer`
  command (no `FERVID_*` var overrides the clock). Calendar-day thresholds (≥1 day minimum) cannot
  be produced inside a Playwright run. Every reminder-firing case is therefore marked
  **`NOT RUN — Go-level only`** with the covering Go test named, per the audit brief's explicit
  permission to do so honestly rather than invent an untestable browser case.

---

## Summary table

| TC ID | Title | Traces to | Type | Priority |
|---|---|---|---|---|
| TC-F-001 | Submitting a request fires `request_submitted` in-app to the assigned manager only | N1 | functional | high |
| TC-F-002 | Returning a pending request fires `request_returned` (mention) in-app to the requester | N1 | functional | high |
| TC-F-003 | Editing and resubmitting a returned request fires `request_edited` in-app to the manager | N1 | functional | medium |
| TC-F-004 | Rejecting a pending request fires `request_rejected` (mention) in-app to the requester | N1 | functional | high |
| TC-F-005 | Approving a request fires `request_approved` to the requester and to every independent Accounts-permission holder | N1, N3 | functional | high |
| TC-F-050 | `request_approved`'s seeded rule never notifies the manager, only the requester and Accounts | N3 | information-flow | medium |
| TC-F-006 | Putting an approved request on hold fires `request_on_hold` (mention) to the requester only | N1 | functional | medium |
| TC-F-007 | Settling a payment in full fires `payment_settled` to both requester and approver | N1 | functional | high |
| TC-F-008 | A partial settlement fires `payment_partial_review` to the approver only, never the requester | N1 | information-flow | high |
| TC-F-009 | Asking to cancel an approved request fires `request_cancellation_requested` to the approver, never the asking requester | N1 | information-flow | medium |
| TC-F-010 | The admin rules screen renders exactly the twelve seeded events, in `notify.AllEvents` order | N1, N2, G20 | regression | high |
| TC-F-010b | Email is disabled by default for every one of the twelve seeded events | N2, N3 | boundary | medium |
| TC-F-011 | `/notifications` never renders another user's rows | N1 | information-flow | high |
| TC-F-012 | `GET /notifications/{id}/open` for another user's id answers 404 and does not mark it read | N1 | permission | critical |
| TC-F-013 | An anonymous `GET /notifications` redirects to `/login` | N1 | permission | medium |
| TC-F-014 | `GET /notifications/{id}/open` for a nonexistent id answers 404 | N1 | negative | low |
| TC-F-015 | A user holding no role at all can still reach `/notifications` | N1, G19 | permission | medium |
| TC-F-016 | The mobile shell's bell dot equals the unread count shown on the notifications page | N1, G19 | functional | medium |
| TC-F-049 | The notification centre has no desktop entry point — see finding F-F-05 | G19 | proof-of-absence | high |
| TC-F-017 | The "All" segmented count equals the number of rows rendered for scope=all | N1 | functional | medium |
| TC-F-018 | The "Mentions" segmented filter shows only mention-kind rows, and its count matches | N1 | functional | medium |
| TC-F-019 | The "Reminders" segmented filter is empty in this environment; bucket counts never exceed "All" | N1, N4, N5 | boundary | low |
| TC-F-020 | Opening one unread notification marks only that row read and navigates to its own stored href | N1 | functional | high |
| TC-F-021 | `POST /notifications/read` marks every one of the caller's unread rows read | N1 | functional | medium |
| TC-F-022 | Unread counts and mark-all-read are scoped per user | N1 | information-flow | high |
| TC-F-023 | `POST /notifications/read` without a CSRF token is refused and changes nothing | N1 | negative | medium |
| TC-F-024 | A Requester is refused `GET /admin/notifications` | N2 | permission | high |
| TC-F-025 | A Manager is refused `GET /admin/notifications` | N2 | permission | high |
| TC-F-026 | An Accounts user is refused `GET /admin/notifications` | N2 | permission | high |
| TC-F-027 | A Requester is refused all three admin notification POSTs | N2 | permission | high |
| TC-F-028 | A Manager is refused all three admin notification POSTs | N2 | permission | high |
| TC-F-029 | An Accounts user is refused all three admin notification POSTs | N2 | permission | high |
| TC-F-030 | Toggling `email_enabled` for an event persists and round-trips | N2 | functional | high |
| TC-F-031 | Recipient lists and include-requester/manager/accounts flags persist and round-trip | N2, N3 | functional | high |
| TC-F-032 | Subject/body templates containing characters needing escaping persist exactly and render safely | N2, G20 | boundary | medium |
| TC-F-033 | Saving a template referencing an unknown token is rejected and the previous value is retained | N2 | negative | medium |
| TC-F-034 | The In-app column always reads "On" regardless of the event's email toggle | N1, N2 | regression | low |
| TC-F-035 | SMTP host/port/username/from-name/from-addr/base URL/management recipients persist and round-trip | N8 | functional | high |
| TC-F-036 | No password field exists on the page, and a posted `smtp_password` is never echoed or audited | N8 | information-flow | critical |
| TC-F-037 | Sending a test email with SMTP unconfigured fails gracefully with a message, not a 500 or a hang | N8 | negative | high |
| TC-F-038 | An urgent request stays pending until its assigned manager decides | N6 | state-machine | high |
| TC-F-039 | An urgent pending request never appears in the Accounts "approved" queue | N6 | permission | high |
| TC-F-040 | A Manager who is not the assigned approver is refused approving an urgent request | N6, G8 | permission | critical |
| TC-F-041 | Submitting an urgent request notifies the assigned manager immediately and not Accounts | N6 | information-flow | high |
| TC-F-042 | Approving the urgent request then notifies Accounts, completing the two-send-point design | N6 | information-flow | high |
| TC-F-043 | The Reminders-and-ageing thresholds on `/configuration` persist and round-trip | N4, N5 | functional | medium |
| TC-F-044 | The "reserved too long" screen renders a reservation-history trail for a live reservation | S8 | functional | medium |
| TC-F-045 | `POST /admin/notifications/events/{event}` with a forged CSRF token is refused, even for an Admin session | N2 | negative | medium |
| TC-F-046 | The admin rules screen never renders `require_attachments` or the reminder-threshold fields (Configuration's own) | N2, D6 | regression | low |
| TC-F-047 | A real pointer click on an event's own "Edit" trigger cannot open its own sheet — deterministic, `test.fail()`, see finding F-F-01 | N2 | proof-of-absence | critical |
| TC-F-048 | The pending-reminder hint on `/requests/new` does not reflect the configured `reminder_pending_days` — deterministic, `test.fail()`, see finding F-F-03 | N4 | proof-of-absence | medium |
| TC-F-051 | Withdrawing a pending request fires no notification to the manager — see finding F-F-06 | N1, G20 | proof-of-absence | high |
| TC-F-052 | Reraising a rejected request fires no notification to the manager — see finding F-F-06 | N1, G20 | proof-of-absence | high |
| TC-F-053 | Releasing a reservation fires no notification to the requester — see finding F-F-06 | N1, G20 | proof-of-absence | high |
| TC-F-054 | Lifting a hold (unhold) fires no notification to the requester — see finding F-F-06 | N1, G20 | proof-of-absence | high |
| TC-F-055 | Accepting a partial payment fires no notification to the requester — see finding F-F-06 | N1, G20 | proof-of-absence | high |
| TC-F-056 | Deciding a cancellation request fires no notification to the requester who asked — see finding F-F-06 | N1, G20 | proof-of-absence | high |
| TC-F-057 | "Save corrections" without resubmitting still fires `request_edited`, wrongly claiming it was re-sent — see finding F-F-07 | N1, A8 | information-flow | medium |

---

## Detail

### Section 1 — the in-app channel is unconditional (N1)

#### TC-F-001 — Submitting a request fires `request_submitted` in-app to the assigned manager only
- **Preconditions:** a fresh Requester-only subject and a fresh Manager-only subject exist; a vendor exists.
- **Steps:** the Requester raises a `vendor_invoice` request naming the Manager as approver.
- **Expected:** `store.AddNotification` (`internal/notify/service.go:97-128`) writes one row for the manager (seeded `request_submitted` rule has `IncludeManager:true` only, `migrations_notifications.go:29-31`); the manager's `/notifications` lists a title containing the request number; the requester has no new row from this event.
- **Actual:** manager's notification list contained "PR-… needs your approval" style title referencing the request number; requester had none from this event.
- **Verdict:** PASS

#### TC-F-002 — Returning a pending request fires `request_returned` (mention) in-app to the requester
- **Preconditions:** the pending request from TC-F-001.
- **Steps:** the manager returns it for correction with a comment.
- **Expected:** one row lands for the requester, `kind='mention'` (`store.notificationKind`, `internal/store/inapp.go:56-65`), since `request_returned` seeds `IncludeRequester:true` only.
- **Actual:** requester's `/notifications?scope=mentions` showed the row; glyph rendered `✎`.
- **Verdict:** PASS

#### TC-F-003 — Editing and resubmitting a returned request fires `request_edited` in-app to the manager
- **Preconditions:** the returned request from TC-F-002.
- **Steps:** the requester opens `/requests/{id}` (renders `request_returned` template), clicks "Resubmit for approval" with the pre-filled fields unchanged.
- **Expected:** `requestEdit` (`internal/app/requests.go:645-677`) fires `EventRequestEdited` unconditionally; seeded rule has `IncludeManager:true` only (`Audience: "Approver"`), so the manager gets a new row and the requester does not.
- **Actual:** manager's list gained a new row titled "... was edited and re-sent for approval"; requester's mention count was unchanged by this step.
- **Verdict:** PASS

#### TC-F-004 — Rejecting a pending request fires `request_rejected` (mention) in-app to the requester
- **Preconditions:** the request from TC-F-003, now pending again.
- **Steps:** the manager rejects it with a reason.
- **Expected:** one mention-kind row for the requester (`IncludeRequester:true` only).
- **Actual:** requester's mentions list gained a second row ("... was rejected").
- **Verdict:** PASS

#### TC-F-005 — Approving a request fires `request_approved` to the requester and to every independent Accounts-permission holder
- **Preconditions:** two fresh Accounts-only subjects (holding only `payment:process`, no other role); a fresh pending request from the same requester/manager pair.
- **Steps:** the manager approves the request; then read each Accounts subject's `/notifications`.
- **Expected:** `accountsUsers` resolves via `st.UsersWithPermission(ctx,"payment","process")` (`internal/notify/service.go:84-89`), which is a live permission query, not a hardcoded account — **both** independently-created Accounts subjects must receive a row, proving group resolution rather than a single recipient; the requester also receives one (`IncludeRequester:true`).
- **Actual:** requester and both Accounts subjects each showed a row titled "... approved — ₹… to …".
- **Verdict:** PASS

#### TC-F-050 — `request_approved`'s seeded rule never notifies the manager, only the requester and Accounts
- **Preconditions:** the manager from TC-F-005 already has a `request_submitted` row for request D (from raising it).
- **Steps:** after TC-F-005's approval, read the manager's own notification titles; filter for ones naming request D's number **and** containing "approved".
- **Expected:** zero matches — the seeded `request_approved` rule sets `IncludeRequester`/`IncludeAccounts` only, no `IncludeManager` (`migrations_notifications.go`) — an approving manager is never told about their own decision.
- **Actual:** zero matches.
- **Verdict:** PASS

#### TC-F-006 — Putting an approved request on hold fires `request_on_hold` (mention) to the requester only
- **Preconditions:** a fresh approved request (not yet reserved); an Accounts subject holding `payment:hold`.
- **Steps:** the Accounts subject opens the approved request and puts it on hold with a reason.
- **Expected:** one mention-kind row for the requester (`IncludeRequester:true` only, `migrations_notifications.go:48-50`); no row for the Accounts actor or the manager.
- **Actual:** requester's mentions list gained a row "... was put on hold".
- **Verdict:** PASS

#### TC-F-007 — Settling a payment in full fires `payment_settled` to both requester and approver
- **Preconditions:** a fresh approved request; an Accounts subject able to reserve and record a payment.
- **Steps:** reserve via "Take for processing", then record a full settlement through `fixtures.settlePayment`.
- **Expected:** `app.go:724` fires `EventPaymentSettled`; seeded rule has `IncludeRequester:true, IncludeManager:true` — both the requester and the request's own manager get a row.
- **Actual:** both requester and manager showed a "... has been paid — ₹… to …" row.
- **Verdict:** PASS

#### TC-F-008 — A partial settlement fires `payment_partial_review` to the approver only, never the requester
- **Preconditions:** a fresh approved request; an Accounts subject.
- **Steps:** reserve, then record a **partial** settlement (`settlement:'partial'`, a reason supplied).
- **Expected:** `app.go:726` fires `EventPaymentPartialReview`; seeded rule has `IncludeManager:true` only (`Audience: "Approver"`, `migrations_notifications.go:59-61`) — the manager gets a row, the requester's count from this event is **zero**. This is a deliberate information-flow assertion: a design that silently told the requester about a shortfall the manager hasn't reviewed yet would be a defect.
- **Actual:** manager received "... was partly paid — your review is needed"; requester's own notification count did not increase from this step.
- **Verdict:** PASS

#### TC-F-009 — Asking to cancel an approved request fires `request_cancellation_requested` to the approver, never the asking requester
- **Preconditions:** a fresh approved, unreserved request.
- **Steps:** the requester (holding `request:cancel`) opens `/requests/{id}/cancel` and posts a reason.
- **Expected:** `requests.go:824` fires `EventCancellationRequested`; seeded rule has `IncludeManager:true` only (`Audience: "Approver + assigned accountant"`) — the manager gets a row; since the request was never reserved, `resolveInAppUsers`'s special-case (`service.go:141-152`) falls back to the full Accounts group, but no Accounts subject was created for this case, so only the manager's row is asserted; the requester's own list gains nothing from this event.
- **Actual:** manager received "... asked to cancel PR-…"; requester's count was unchanged by this step.
- **Verdict:** PASS

#### TC-F-010 — The admin rules screen renders exactly the twelve seeded events, in `notify.AllEvents` order
- **Preconditions:** signed in as Admin.
- **Steps:** `GET /admin/notifications`; collect every `.t-sub` (event slug) in document order.
- **Expected:** exactly `["request_submitted","request_edited","request_returned","request_rejected","request_approved","request_urgent","request_on_hold","request_cancellation_requested","payment_settled","payment_partial_review","reminder_pending","reminder_stale_reservation"]` — the same order `notify.AllEvents` declares (`internal/notify/events.go:23-27`) and `AllNotificationSettings` returns (`ORDER BY sort_order,event`, `internal/store/notifications.go:97-98`, and `sort_order` is seeded `i+1` in that same array order, `migrations_notifications.go:105-114`). This is the highest-value check in the whole document: comparing seeded events (12) against fireable events (traced below) against rendered events (this test) is how a silently-orphaned event would be caught.
- **Actual:** the twelve slugs appeared, in the exact order listed above. Cross-checked against fire sites read in the Go source: `request_submitted` (`requests.go:172`), `request_edited` (`requests.go:676`), `request_returned` (`requests.go:735`), `request_rejected` (`requests.go:744`), `request_approved` (`requests.go:726`), `request_urgent` (`notifications.go:46-51`, fired from inside `fire()` on top of submit/approve when `req.Urgent`), `request_on_hold` (`linking.go:703`), `request_cancellation_requested` (`requests.go:824`), `payment_settled` (`app.go:724`), `payment_partial_review` (`app.go:726`), `reminder_pending` and `reminder_stale_reservation` (both only from `Service.RunReminders` via the hourly `Scheduler`, `reminders.go:19-22`). **All twelve seeded events are fireable, all fireable events are seeded, and all are rendered** — no orphan in either direction. This is a clean bill of health, not a defect, and is reported as such in the findings.
- **Verdict:** PASS
- **Since the run:** the screen renders **twenty-one** rows, not twelve. Migration **v9** seeds nine more (`internal/store/migrations_notifications.go:146-155`) and `notify.AllEvents` lists v7's twelve followed by v9's nine (`internal/notify/events.go:71-79` — the citation `events.go:23-27` above is where `AllEvents` sat at `30edd6a`). The twelve slugs above are still the first twelve, in the same order, because `sort_order` is append-only and v9 adds rows without touching v7's. The no-orphan property still holds in both directions: all nine new events fire — `request_withdrawn` (`internal/app/requests.go:916`), `request_reraised` (`:932`), `request_cancellation_accepted`/`_declined` (`:1029`, `:1031`), `payment_partial_accepted` (`internal/app/linking.go:554`), `payment_partial_concern` (`:567`), `reservation_released` (`:712`), `reservation_reassigned` (`:755`), `request_unheld` (`:803`). F-F-06, fixed REPAIR-LOG.md Wave 2 (`25411b8`) and Wave 4 (`709dfa6`).

#### TC-F-010b — Email is disabled by default for every one of the twelve seeded events
- **Preconditions:** none of TC-F-001–010 touch admin settings; run before Section 4 (which does).
- **Steps:** as Admin, `GET /admin/notifications`; read all twelve "Email" column pills.
- **Expected:** every one reads "Off" — `defaultNotificationSettings` (`migrations_notifications.go`) never sets `EmailEnabled` on any of the twelve struct literals, so it is Go's zero value (`false`/`0`) for all twelve on a freshly-migrated database. Consequence for N3: "approval email to Accounts + requester + management" does not happen on a fresh install until an admin opts `request_approved` in — only the in-app row is unconditional.
- **Actual:** all twelve pills read "Off".
- **Verdict:** PASS
- **Since the run:** there are twenty-one pills, and the rule still holds for all of them. None of the nine `auditNotificationSettings` struct literals sets `EmailEnabled` either (`internal/store/migrations_notifications.go:87-120`), so `seedNotificationSettings` writes `boolInt(false)` = 0 for each (`:132-144`) — the same zero value, by the same route, as v7's twelve.

---

### Section 2 — scoping (every row is the caller's own)

#### TC-F-011 — `/notifications` never renders another user's rows
- **Steps:** as the Requester from Section 1, `GET /notifications?scope=all`; assert the body does not contain any title known to belong only to the Manager's own notifications (e.g. the "needs your approval" / "was edited" titles from TC-F-001/003, which the store never wrote for this requester); as the Manager, confirm the reverse — their list does not contain the requester's mention-only titles.
- **Expected:** the store scopes every read by `user_id` (`internal/store/inapp.go:19-24`), so cross-contamination is structurally impossible; this test is the positive-control complement to TC-F-012.
- **Actual:** each list contained only titles addressed to that user; no cross-over observed.
- **Verdict:** PASS

#### TC-F-012 — `GET /notifications/{id}/open` for another user's id answers 404 and does not mark it read
- **Preconditions:** a notification id known to belong to the Manager (captured from TC-F-001).
- **Steps:** as the Requester (a different, unrelated user), request `GET /notifications/{managerNotifId}/open`.
- **Expected:** `store.Notification` scopes by `id AND user_id` (`internal/store/inapp.go:127-135`), so a foreign id matches nothing and returns `ErrNotFound` → the handler answers **404**, not a redirect to the target `href` — and the row must **not** be marked read (verified by the Manager's unread count being unchanged after the attempt). This is the authorisation test explicitly called out as high-severity if it fails: a 200/redirect here would mean any signed-in user can read and silently mark-read anyone else's notifications by walking ids.
- **Actual:** response was 404; the Manager's own unread count, re-checked immediately after, was unchanged.
- **Verdict:** PASS — no authorisation defect found.

#### TC-F-013 — An anonymous `GET /notifications` redirects to `/login`
- **Steps:** `probeAnonymous(browser, '/notifications', baseURL)`.
- **Expected:** `RequireLogin` refuses with `303 See Other → /login` (per the audit-support facts file, not 302).
- **Actual:** 303 → /login.
- **Verdict:** PASS

#### TC-F-014 — `GET /notifications/{id}/open` for a nonexistent id answers 404
- **Steps:** as any signed-in user, `probeGet('/notifications/999999999/open')`.
- **Expected:** 404 (same `ErrNotFound` path as TC-F-012, id simply does not exist at all).
- **Actual:** 404.
- **Verdict:** PASS

#### TC-F-015 — A user holding no role at all can still reach `/notifications`
- **Preconditions:** `asRole(..., [])` — a subject with zero role grants.
- **Steps:** `GET /notifications`.
- **Expected:** 200. The route is `RequireLogin` only (`app.go:411`) by design (G19 comment, `inapp.go:12-16`): "It needs an authenticated session and no permission verb: every row it can return is scoped to the signed-in user by the store, so there is nothing a permission could add."
- **Actual:** 200, empty-state "Nothing here yet" shown (this user was never addressed by any event).
- **Verdict:** PASS

---

### Section 3 — the bell count, mark-read, and the `.segmented` filter

All of Section 3 shares one dedicated Requester/Manager pair whose full notification history is
built by this document's own test data (Section 1's request chain plus two more small requests
raised solely to give the requester at least one activity-kind row alongside the mention-kind
rows already produced by TC-F-002/004). Counts are asserted **relationally** (row counts vs. the
segmented number, not hand-computed absolute totals) so the check is not fragile to a
miscounted setup step.

#### TC-F-016 — The mobile shell's bell dot equals the unread count shown on the notifications page
- **Preconditions:** the requester has ≥1 unread notification; viewport narrowed to 390×844 so `.m-topbar` (mobile-only, `display:none` above 860px — `fervid-ds.css:3881-3884` vs. `:4398` inside `@media (max-width:860px)`) actually renders.
- **Steps:** load any app page at the narrow viewport; read `.m-topbar .m-icon .dot`; separately read the unread number from `/notifications`'s own sub-header/segmented "Unread" count.
- **Expected:** the two numbers are equal — both are `store.UnreadNotificationCount` read at request time (`nav.go:152-158` for the shell, `NotificationCounts.Unread` for the page).
- **Actual:** both read the same number.
- **Verdict:** PASS

#### TC-F-049 — The notification centre has no desktop entry point
- **Preconditions:** none.
- **Steps:** sign in, resize to 1440×900, load `/`; assert `.sidebar a[href="/notifications"]` has zero matches, and no **visible** element anywhere on the page links to `/notifications` (a plain `a[href="/notifications"]` count would wrongly count the mobile topbar's own anchor, which is always in the DOM and only `display:none` at this width — the visible-only and sidebar-scoped checks are the honest versions); assert `.m-topbar` itself is hidden; separately confirm `GET /notifications` still answers 200 when requested directly.
- **Expected:** `navSpec` (`nav.go`) has no item for `/notifications` (it has one for the *admin* rules screen only); the dashboard template has no notifications button either, unlike the approved mockup (`dashboard.html:19`); the only markup anywhere pointing at `/notifications` is the mobile-only bell. So: zero sidebar matches, zero visible matches, `.m-topbar` hidden, and the route itself still 200s — a discoverability gap, not a permission one. See finding F-F-05.
- **Actual:** exactly as expected — zero sidebar matches, zero visible matches at 1440×900, `.m-topbar` hidden, direct `GET /notifications` 200.
- **Verdict:** PASS (the assertions describe the current, confirmed-defective state honestly; see F-F-05 for why this is a usability finding despite the test passing)

#### TC-F-017 — The "All" segmented count equals the number of rows rendered for scope=all
- **Steps:** `GET /notifications?scope=all`; compare the digit next to "All" against `.notif-list .notif` count.
- **Expected:** equal.
- **Actual:** equal.
- **Verdict:** PASS

#### TC-F-018 — The "Mentions" segmented filter shows only mention-kind rows, and its count matches
- **Steps:** `GET /notifications?scope=mentions`; assert every row's `.n-ico` glyph is `✎` (the mention glyph, `notifGlyph`, `inapp.go:22-30`) and the row count equals the "Mentions" digit; separately confirm `scope=all` contains at least one row whose glyph is **not** `✎` (proving the filter narrows rather than coincidentally matching).
- **Expected:** as above.
- **Actual:** all `scope=mentions` rows showed `✎`; `scope=all` contained non-mention rows too (the activity-kind approvals/settlements).
- **Verdict:** PASS

#### TC-F-019 — The "Reminders" segmented filter is empty in this environment; bucket counts never exceed "All"
- **Steps:** `GET /notifications?scope=reminders`.
- **Expected:** 0 rows, 0 in the "Reminders" digit — no reminder can fire in this environment (see the environment-constraints note above); and, generally, `Mentions + Reminders ≤ All` for any user (the buckets are non-overlapping subsets of the same set of rows, `notificationKind`, `inapp.go:56-65`).
- **Actual:** "Reminders" showed 0; the containment inequality held.
- **Verdict:** PASS

#### TC-F-020 — Opening one unread notification marks only that row read and navigates to its own stored href
- **Steps:** under `scope=unread`, read the first row's title (it names its own request number, e.g. `PR-2026-000131`); click the row; confirm the resulting URL is `/requests/{id}` and that the landed page's own `.rh-no` shows the **same** request number the clicked title named; re-check the unread count.
- **Expected:** `notificationOpen` marks only the opened row (`UPDATE ... WHERE id=? AND user_id=?`, `inapp.go:156-158`) and redirects to `n.Href`, which `requestHref()` always renders as `/requests/{id}` (`service.go:165-170`) — never a query-string-supplied target (`inapp.go:90-94`); the unread count drops by exactly 1.
- **Actual:** landed on `/requests/{id}`; the page's `.rh-no` matched the exact request number named in the clicked row's title (proving it navigated to *that* row's own target, not merely *some* request page); unread count decreased by exactly 1.
- **Verdict:** PASS

#### TC-F-021 — `POST /notifications/read` marks every one of the caller's unread rows read
- **Steps:** with ≥1 unread row remaining, POST `/notifications/read` with a valid CSRF token; reload `/notifications`.
- **Expected:** unread count becomes 0; no row anywhere in `scope=all` carries the `.unread` class.
- **Actual:** unread count 0; no `.unread` class present.
- **Verdict:** PASS

#### TC-F-022 — Unread counts and mark-all-read are scoped per user
- **Steps:** before the requester's mark-all-read in TC-F-021, record the manager's own unread count; after it completes, re-read the manager's count.
- **Expected:** the manager's count is unaffected — `MarkAllNotificationsRead` is `WHERE user_id=?` (`inapp.go:180-183`).
- **Actual:** manager's unread count identical before and after.
- **Verdict:** PASS

#### TC-F-023 — `POST /notifications/read` without a CSRF token is refused and changes nothing
- **Preconditions:** the requester has ≥1 unread row again (a fresh event fired after TC-F-021).
- **Steps:** `probePost('/notifications/read', {}, {csrf:'omit'})`.
- **Expected:** 403 (`withCSRF`, per the audit-support facts file); the unread count is unchanged by the rejected attempt.
- **Actual:** 403; unread count unchanged.
- **Verdict:** PASS

---

### Section 4 — the admin rules screen (N2, N8) — RBAC, the twelve-row matrix, persistence

**The matrix is twenty-one rows now, not twelve** — migration v9's nine events render through the
same `range` as v7's twelve (`internal/notify/events.go:71-79`). Every count in this section is the
`30edd6a` measurement and is left as measured.

**A defect discovered while writing TC-F-030, that changes how TC-F-030–035/037 had to be driven —
see finding F-F-01 for the full writeup.** Every one of the twelve `<div class="overlay"
id="ev-{{.Event}}">` per-event sheets (`templates.go:3347`) is missing the `hidden` attribute every
other `.overlay` in the app carries by default (contrast `approve-sheet`/`return-sheet`/
`reject-sheet`/`hold-sheet`, all `hidden`, `templates.go:2206/2224/2235/2459`). `.overlay` itself is
`position:fixed; inset:0; z-index:50; display:grid` (`fervid-ds.css:3714-3722`) with no
`.overlay[hidden]` companion rule anywhere in the stylesheet, so an author-origin `display:grid`
beats the user-agent stylesheet's `[hidden]{display:none}` regardless of selector specificity (CSS
cascade origin outranks specificity). The practical result, confirmed two independent ways — this
suite's own `.click()` on `[data-open="ev-request_returned"]` timing out with Playwright reporting
`<div class="overlay" id="ev-reminder_stale_reservation">` intercepting the click, and a standalone
diagnostic script reading `getComputedStyle(el).display`/`el.hidden` on all twelve elements on a
fresh, un-interacted page load and finding `hidden:false, display:"grid"` on every one — is that
**all twelve sheets render simultaneously, full-viewport, on every visit to `/admin/notifications`,
and only the last one in DOM order (`reminder_stale_reservation`) is reachable by a pointer.** Every
other Edit button, and the SMTP form's own "Save email settings" button, is permanently covered.
TC-F-030 through TC-F-035 and TC-F-037 below therefore read a sheet's current field values with
`.inputValue()`/`.isChecked()` (DOM property reads Playwright does not require actionability for)
and write through the same POST route the covered button would otherwise submit to
(`probePost`) — proving the backend persistence contract genuinely works, while TC-F-047 is the
dedicated, deliberately-failing reproduction of the fact that a real mouse click cannot reach it.

#### TC-F-024 / TC-F-025 / TC-F-026 — Requester / Manager / Accounts refused `GET /admin/notifications`
- **Steps:** `asRole(...,['Requester'|'Manager'|'Accounts'])`; `probeGet('/admin/notifications')`.
- **Expected:** 403 in every case — `notification:view` is granted to Admin alone among the four seeded roles (`migrations.go:351-402`; none of Requester/Manager/Accounts' grant lists mention `notification`).
- **Actual:** 403 for all three roles.
- **Verdict:** PASS (×3)

#### TC-F-027 / TC-F-028 / TC-F-029 — Requester / Manager / Accounts refused all three admin notification POSTs
- **Steps:** for each role, `probePost('/admin/notifications/smtp', …)`, `probePost('/admin/notifications/events/request_approved', …)`, `probePost('/admin/notifications/test', …)`.
- **Expected:** 403 for all three POSTs, all three roles (`notification:edit`, same Admin-only grant).
- **Actual:** 403 in all nine combinations.
- **Verdict:** PASS (×3, one per role covering all three routes)

#### TC-F-030 — Toggling `email_enabled` for an event persists and round-trips
- **Steps:** as Admin, read `request_returned`'s current `email_enabled` via `.inputValue()`/`.isChecked()` on its (un-hidden-but-buried) sheet; flip it via `probePost /admin/notifications/events/request_returned` with the rest of the row's fields unchanged; reload; re-read the same way, and cross-check the outer table's own "Email" pill (never buried under any overlay).
- **Expected:** the flipped value persists (`SetNotificationSetting`, `internal/store/notifications.go:120-150`, is a real `UPDATE` read back by `AllNotificationSettings`); the table's own Email column agrees.
- **Actual:** the flipped value round-tripped; the table's Email pill matched it exactly.
- **Verdict:** PASS

#### TC-F-031 — Recipient lists and include-requester/manager/accounts flags persist and round-trip
- **Steps:** read the current row, then `probePost` a known `to_recipients`/`cc_recipients` plus `include_manager`/`include_accounts` on, leaving `include_requester` exactly as read; reload; re-read; cross-check the table's own "Fixed To / CC" cell.
- **Expected:** every field reads back exactly what was posted; `include_requester` is unchanged from before (never touched).
- **Actual:** all fields matched; `include_requester` was unchanged; the table cell contained both addresses.
- **Verdict:** PASS

#### TC-F-032 — Subject/body templates containing characters needing escaping persist exactly and render safely
- **Steps:** `probePost` a subject template of `{{number}} & <b>"quoted"</b> — 'ok'` (a valid token plus HTML-special characters); reload; read the input's `.inputValue()`.
- **Expected:** the value round-trips **exactly** (character-for-character) because `html/template` escapes on output but the browser decodes entities back to the literal string on `.value`; the page must not be broken by the raw `<b>`/`"` (i.e. no unescaped injection — `templates.go:3363` interpolates via `{{.SubjectTemplate}}` inside an attribute, which `html/template` auto-escapes for that context).
- **Actual:** `.inputValue()` returned the exact original string; the rest of the page (the SMTP form's own "Save email settings" button, present though also unreachable by click per F-F-01) still rendered normally, confirming no markup breakage.
- **Verdict:** PASS

#### TC-F-033 — Saving a template referencing an unknown token is rejected and the previous value is retained
- **Steps:** `probePost subject_template={{not_a_real_token}}` for `request_returned`, whose current subject is the value TC-F-032 set; read the response and re-read the field.
- **Expected:** `notify.ValidateTemplate` (`service.go:278-281`) rejects it; the handler answers `400` with an error banner (`notifications.go:109-114`) and — critically — does **not** call `SetNotificationSetting`, so re-reading the field shows the **old** subject unchanged.
- **Actual:** 400 with "That did not save" in the body; the previous subject (TC-F-032's escaped string) was unchanged on re-read.
- **Verdict:** PASS

#### TC-F-034 — The In-app column always reads "On" regardless of the event's email toggle
- **Steps:** compare the "In-app" column pill for an event with `email_enabled=on` against one with it off.
- **Expected:** both read the static `<span class="pill good no-dot">On</span>` (`templates.go:3337`) — in-app is not a control, by design (the template's own comment: "a switch that did nothing would be a lie").
- **Actual:** both showed "On" regardless of the Email column's state.
- **Verdict:** PASS

#### TC-F-047 — A real pointer click on an event's own "Edit" trigger cannot open its own sheet
- **Preconditions:** none beyond a signed-in Admin.
- **Steps:** `test.fail()` (this test is expected to fail); `GET /admin/notifications`; click `[data-open="ev-request_returned"]`; assert `#ev-request_returned` becomes visible.
- **Expected (of the underlying bug, not of the assertion):** the click should open exactly that sheet. It cannot: per the F-F-01 writeup above `#ev-request_returned` is permanently covered by `#ev-reminder_stale_reservation`, so this assertion is written to fail deterministically, on purpose, so the day someone adds `hidden` to `templates.go:3347` this test starts reporting "passed unexpectedly" — the correct signal to delete it and close the finding.
- **Actual:** the click timed out after 10s with Playwright's own diagnostic naming `#ev-reminder_stale_reservation` as the intercepting element — exactly the predicted failure. Test result: expected-fail (counted in the green run).
- **Verdict:** PASS (as an expected failure — this is the correct, honest outcome per the audit brief's guidance on deterministic failures)

---

### Section 5 — SMTP settings and the password rule (N8)

#### TC-F-035 — SMTP host/port/username/from-name/from-addr/base URL/management recipients persist and round-trip
- **Steps:** `probePost /admin/notifications/smtp` with all seven `MailSettings` fields set to distinct, runId-tagged values (the "Save email settings" button itself is unreachable by click — F-F-01); reload `/admin/notifications`.
- **Expected:** every field reads back exactly (`GetMailSettings`/`SetMailSettings`, `internal/store/notifications.go:46-73`).
- **Actual:** all seven fields matched after reload.
- **Verdict:** PASS
- **Note on N3's management-list half (structural, not a defect):** `management_recipients` is a raw comma-separated list of email addresses (`resolveRecipients`, `service.go:201-222`, appends it to `cc` for `request_approved`/urgent-post-approval only) — it is never resolved to a `user_id`, so `resolveInAppUsers` (`service.go:133-163`) has no in-app counterpart for it at all and never could: there is no notifications-table row an arbitrary external address could receive. This is expected given the data shape, not a coverage gap the way F-F-06's six actions are — recorded here only because it was asked after directly.

#### TC-F-036 — No password field exists on the page, and a posted `smtp_password` is never echoed or audited
- **Steps:** confirm `input[type="password"]` has zero matches on `/admin/notifications`; POST to `/admin/notifications/smtp` with an extra, unexpected `smtp_password=<marker>` field alongside the legitimate ones; reload the page and separately load `/audit?entity=app_setting` (Admin holds `audit:view`); search both bodies for the marker string.
- **Expected:** zero password inputs; the marker string appears **nowhere** — `store.MailSettings` has no password field at all (`notifications.go:23-34`) and `adminNotificationsSMTP` never calls `r.FormValue("smtp_password")` (`internal/app/notifications.go:75-91`), so the value is dropped before it ever reaches the map that becomes the audit `Before`/`After` JSON (`SetAppSettings`, `internal/store/settings.go:46-82`). The page's own copy naming `FERVID_SMTP_PASSWORD` (the **variable name**, not a secret value) is expected and is not a leak.
- **Actual:** zero `type="password"` inputs; the marker string was absent from both the settings page and the audit log; "FERVID_SMTP_PASSWORD" (the documented env-var name) was present as expected.
- **Verdict:** PASS

#### TC-F-037 — Sending a test email with SMTP unconfigured fails gracefully with a message, not a 500 or a hang
- **Preconditions:** `smtp_host` is explicitly cleared first via `probePost /admin/notifications/smtp` (TC-F-035 set one; the "Save email settings" button itself is unreachable by click — F-F-01).
- **Steps:** `probePost('/admin/notifications/test', {test_to:'x@example.test'})`.
- **Expected:** `SMTPMailer.Send` returns `"smtp host is not configured"` (`mailer.go:44-46`); the handler answers `502` with `"The test email could not be sent: smtp host is not configured"` (`notifications.go:145-148`) — not a 500, and the request completes within Playwright's normal action timeout (proving no hang).
- **Actual:** 502 with exactly that message, well within the timeout.
- **Verdict:** PASS

---

### Section 6 — recipient resolution and urgency confer no authority (N3, N6)

#### TC-F-038 — An urgent request stays pending until its assigned manager decides
- **Steps:** raise a `vendor_invoice` request with "Mark this urgent" checked and a reason filled; read its status immediately after submission.
- **Expected:** status is `pending`, not auto-approved — urgency is a flag that changes notification routing only (`notifications.go:12-21` comment; `phase-5-notifications-spec.md`'s own words: "Urgent is a flag only"); rendered as "Awaiting approval" (`requestStatusText`, `internal/app/requests.go:988-991`).
- **Actual:** status pill read "Awaiting approval".
- **Verdict:** PASS

#### TC-F-039 — An urgent pending request never appears in the Accounts "approved" queue
- **Steps:** as a fresh Accounts-only subject, `GET /accounts-queue?tab=approved`; search for a link to the urgent request from TC-F-038.
- **Expected:** absent — the queue only lists `status='approved'` requests; urgency confers no early payability.
- **Actual:** no row/link for that request id was present.
- **Verdict:** PASS

#### TC-F-040 — A Manager who is not the assigned approver is refused approving an urgent request
- **Steps:** a second, independent Manager-only subject (not the one named as approver) attempts `probePost('/requests/{id}/approve', {approved_amount:'…', note:''})` against the still-pending urgent request from TC-F-038.
- **Expected:** `ApproveRequest` requires `before.ManagerID == actor.ID` (`internal/store/requests.go:687-689`) and returns `ErrForbidden` regardless of urgency → `403` (`storeErrorStatus`, `http_errors.go:205-206`). The UI itself never shows the Approve/Return/Reject buttons to a non-assigned manager (`$mineToDecide`, `templates.go:2400-2428`), so this is exercised at the route level, which is the correct place for a defence a hidden button cannot demonstrate.
- **Actual:** 403.
- **Verdict:** PASS — G8/self-approval-adjacent authority boundary holds under urgency too.

#### TC-F-041 — Submitting an urgent request notifies the assigned manager immediately and not Accounts
- **Preconditions:** the urgent request from TC-F-038 (still pending); a fresh Accounts-only subject who has received no other notification in this run.
- **Steps:** read the assigned manager's `/notifications` and the fresh Accounts subject's `/notifications`.
- **Expected:** `resolveInAppUsers` includes the manager for `EventRequestUrgent` because `req.Status == "pending"` (`service.go:138`, the negated condition only excludes the manager **after** approval); it explicitly excludes Accounts while `req.Status == "pending"` (`service.go:141`) — "before approval it is the approver's problem". The manager's list must contain a title starting "URGENT:"; the Accounts subject's list must not.
- **Actual:** manager had an "URGENT: PR-… — ₹… to …" row; the fresh Accounts subject had none.
- **Verdict:** PASS

#### TC-F-042 — Approving the urgent request then notifies Accounts, completing the two-send-point design
- **Steps:** the correct assigned manager approves the urgent request (via the UI, now legitimately `$mineToDecide`); re-check the same Accounts subject from TC-F-041.
- **Expected:** `fire()` raises `EventRequestUrgent` a second time on approval (`notifications.go:44-51`); now `req.Status != "pending"`, so `resolveInAppUsers` **includes** Accounts and **excludes** the manager for this second firing (`service.go:138,141`) — the Accounts subject gains a new "URGENT:" row.
- **Actual:** the Accounts subject's list gained a new "URGENT: … approved …"-style row after the approval.
- **Verdict:** PASS

---

### Section 7 — reminders and ageing thresholds; the stale-reservation screen (N4, N5, S8)

#### TC-F-043 — The Reminders-and-ageing thresholds on `/configuration` persist and round-trip
- **Steps:** as Admin, set `reminder_pending_days`, `reminder_repeat_days`, `reminder_stale_days` to distinct non-default numbers; save; reload `/configuration`.
- **Expected:** all three values read back exactly (`configurationSave`/`AppSettings`, `configuration.go:79-160`); these are the same keys `store.ReminderThresholds` reads (`internal/store/reminders.go:64-81`).
- **Actual:** all three values round-tripped.
- **Verdict:** PASS

#### TC-F-044 — The "reserved too long" screen renders a reservation-history trail for a live reservation
- **Preconditions:** an approved request reserved (not settled) by an Accounts subject.
- **Steps:** the same Accounts subject opens `GET /requests/{id}/reservation/stale` (gated on `payment:process` only, not on actual elapsed time — `app.go:470`, `linking.go:730-750`) **seconds** after reserving; read the trail and the warning banner text.
- **Expected:** 200; the page renders "Who has been told" with a `<ol class="thread">` history containing at least the "reserved it for processing" line, and never a "reminder" line (`MarkReminderSent` never calls `recordAuditTx`, and `auditPhrase`/`auditGlyph`/`auditTone` have no reminder-related case). The banner's own text — "A reminder went out at the one-day mark" — is **not** conditional on anything (`templates.go:1085-1094` has no `{{if}}` around it), so it must render exactly that sentence even though the reservation is seconds, not a day, old, and no reminder has fired.
- **Actual:** 200; the trail rendered the expected "process" entry and never mentioned "reminder"; the banner rendered "A reminder went out at the one-day mark" verbatim, seconds after reservation.
- **Verdict:** PASS — the assertions describe the current, confirmed-defective state honestly. See finding F-F-02: this screen's own claim is decoupled from reality two separate ways (no real reminder has fired yet it says one did; and even the hardcoded 24h `store.StaleReservation` clock that normally gates reaching this screen is independent of the admin-configurable `reminder_stale_days` the sentence is actually about).

#### TC-F-048 — The pending-reminder hint on `/requests/new` reflects the configured `reminder_pending_days` threshold
- **Steps:** `test.fail()` (deterministic failure, kept on purpose); as Admin, set `reminder_pending_days=7` via `/configuration` (preserving the three unrelated toggle fields exactly as read, since `configurationSave` treats an absent toggle as "off"); as the requester, load `/requests/new?type=vendor_invoice`; read the Reminders `.hint` text.
- **Expected:** the hint should read "...7 calendar days...", reflecting the live setting.
- **Actual:** the hint still read "...three calendar days...", the hardcoded string at `templates.go:2024` — unchanged by the configuration edit. Test result: expected-fail (counted in the green run). See finding F-F-03 — the same hardcoded string (or its "three-day reminder clock" sibling) also appears at `templates.go:2515-2516`, `:2535-2536`, `:3005-3006`, `:3027-3028`.
- **Verdict:** PASS (as an expected failure)

---

### Section 8 — CSRF hygiene and screen boundaries (supporting checks)

#### TC-F-045 — `POST /admin/notifications/events/{event}` with a forged CSRF token is refused, even for an Admin session
- **Steps:** `probePost('/admin/notifications/events/request_approved', {...}, {csrf:'bogus'})` as Admin.
- **Expected:** 403 (`withCSRF`, same rule as every other POST in the app).
- **Actual:** 403.
- **Verdict:** PASS

#### TC-F-046 — The admin rules screen never renders `require_attachments` or the reminder-threshold fields (Configuration's own)
- **Steps:** as Admin, `GET /admin/notifications`; search the body for `name="require_attachments"` and `name="reminder_pending_days"`.
- **Expected:** absent from both — D6 ("Configuration is one screen") and the existing Go test `TestAdminNotificationsScreenSavesAndBlocksUnprivileged` already pin this boundary; this is the browser-level confirmation that the screens have not drifted into overlapping the same `app_settings` keys from two different forms.
- **Actual:** neither field name was present.
- **Verdict:** PASS

---

### Section 9 — coverage gaps in the twelve-event vocabulary (see finding F-F-06)

None of the six actions below ever calls `a.fire(` anywhere in `internal/app`: grepping every
`a.fire(` call site in the package names exactly nine events (the twelve seeded events resolve to
nine call sites once the nested `EventRequestUrgent` call inside `fire()` itself is counted
separately) and no others. Each test drives the real action through its own POST route with a
valid CSRF token and proves the person who arguably should be told receives nothing more than they
already had. This is a genuine, honest outcome — there is no seeded event at all for any of these
six transitions, so nothing here is "an existing rule failing to fire" (Section 1/8 above already
established the twelve seeded events are completely consistent with what fires).

**Every gap in this section is Fixed** — the vocabulary in REPAIR-LOG.md Wave 2, commit `25411b8`,
the wiring in Wave 4, commit `709dfa6` (F-F-06, with F-D-12). Migration **v9** seeds nine new event
rules and `notify.AllEvents` now holds twenty-one (`internal/notify/events.go:71-79`), and each of
the six transitions below fires one of them. The six cases keep their recorded `PASS` verdicts,
because each asserted the observed silence honestly rather than a wish; what is stale is the claim
that no event exists.

#### TC-F-051 — Withdrawing a pending request fires no notification to the manager
- **Preconditions:** a fresh pending request naming `mgr` as approver.
- **Steps:** capture the manager's notification count; the requester posts `/requests/{id}/withdraw` with a valid CSRF token; re-read the manager's count.
- **Expected:** unchanged — `requestWithdraw` (`requests.go:748-754`) never calls `a.fire`.
- **Actual:** unchanged (303 on the withdraw POST; count identical before/after).
- **Verdict:** PASS
- **Since the run:** `requestWithdraw` fires `EventRequestWithdrawn` (`internal/app/requests.go:916`; the constant at `internal/notify/events.go:37`, its rule seeded by v9). The approver whose queue item vanished without a decision is now told.

#### TC-F-052 — Reraising a rejected request fires no notification to the manager (no request_submitted-equivalent)
- **Preconditions:** requestA (Section 1), rejected.
- **Steps:** capture the manager's count; the requester posts `/requests/{id}/reraise`; re-read.
- **Expected:** unchanged — `requestReraise` (`requests.go:759-766`) creates a brand-new pending request (D1) but never calls `a.fire`, so the manager has no way to learn a new request now needs their attention.
- **Actual:** unchanged (303 redirect to the new request's `/submitted` page; manager's count identical before/after).
- **Verdict:** PASS
- **Since the run:** `requestReraise` fires `EventRequestReraised` for the **new** id (`internal/app/requests.go:932`; the constant at `internal/notify/events.go:41`). D1 makes a re-raise a new request with its own number, which is exactly why it needs its own event rather than `request_submitted`.

#### TC-F-053 — Releasing a reservation fires no notification to the requester
- **Preconditions:** requestU (TC-F-044), reserved by `accA`.
- **Steps:** capture the requester's count; `accA` posts `/requests/{id}/release` with a reason and `confirm=on`; re-read.
- **Expected:** unchanged — `requestRelease` (`linking.go:623-635`) never calls `a.fire`.
- **Actual:** unchanged (303 redirect to `/accounts-queue`; requester's count identical before/after).
- **Verdict:** PASS
- **Since the run:** `requestRelease` fires `EventReservationReleased` (`internal/app/linking.go:712`; the constant at `internal/notify/events.go:48`), whose seeded rule carries both `IncludeRequester` and `IncludeManager` — the release screen states in as many words that "the requester and the approver are both notified", so both are. This also closes F-D-12 (TC-D-099).

#### TC-F-054 — Lifting a hold (unhold) fires no notification to the requester
- **Preconditions:** requestE (TC-F-006), on hold.
- **Steps:** capture the requester's count; `accA` posts `/requests/{id}/unhold`; re-read.
- **Expected:** unchanged — `requestUnhold` (`linking.go:710-720`) never calls `a.fire`.
- **Actual:** unchanged (303 redirect to the request page; requester's count identical before/after).
- **Verdict:** PASS
- **Since the run:** `requestUnhold` fires `EventRequestUnheld` (`internal/app/linking.go:803`; the constant at `internal/notify/events.go:44`). `request_on_hold` already told the requester the hold was placed; this is the other half.

#### TC-F-055 — Accepting a partial payment fires no notification to the requester
- **Preconditions:** requestG (TC-F-008), in `partial_review`.
- **Steps:** capture the requester's count; `mgr` posts `/requests/{id}/accept-partial` with a note; re-read.
- **Expected:** unchanged — `requestAcceptPartial` (`linking.go:477-484`) never calls `a.fire`.
- **Actual:** unchanged (303 redirect to the request page; requester's count identical before/after).
- **Verdict:** PASS
- **Since the run:** `requestAcceptPartial` fires `EventPaymentPartialAccepted` (`internal/app/linking.go:554`; the constant at `internal/notify/events.go:56`), and the concern branch fires `EventPaymentPartialConcern` (`linking.go:567`, constant at `events.go:60`). The requester learns the balance is never coming; the assigned accountant learns to stop chasing it.

#### TC-F-056 — Deciding a cancellation request fires no notification to the requester who asked
- **Preconditions:** requestH (TC-F-009), `cancellation_requested`.
- **Steps:** capture the requester's count; `mgr` posts `/requests/{id}/cancellation` with `decision=decline` and a note; re-read.
- **Expected:** unchanged — `requestCancellationDecide` (`requests.go:850-858`) never calls `a.fire`.
- **Actual:** unchanged (303 redirect to `/approvals?bucket=cancellations`; requester's count identical before/after).
- **Verdict:** PASS
- **Since the run:** `requestCancellationDecide` fires whichever half the decision was — `EventCancellationAccepted` or `EventCancellationDeclined` (`internal/app/requests.go:1029`, `:1031`; the constants at `internal/notify/events.go:65-66`). They are two events and not one because the sentences are opposites — *"nothing will be paid"* against *"payment is unfrozen"* — and each is an admin-editable template in its own right. This is also the silence TC-C-097B recorded.

#### TC-F-057 — "Save corrections" without resubmitting still fires `request_edited`, wrongly claiming it was re-sent
- **Preconditions:** a fresh request, returned by `mgr`.
- **Steps:** capture the manager's notification titles; the requester opens the request (renders `request_returned`) and clicks "Save corrections" (`submit_action=save`, deliberately not "Resubmit for approval"); re-read the manager's titles; separately reload the request and confirm its own "sent this back" banner still shows (proving status is still `returned`, nothing was resubmitted).
- **Expected:** the manager's count increases by exactly one, with a new row whose title mentions "edited" — because `a.fire(EventRequestEdited, ...)` at `requests.go:676` sits outside the `submit_action=="resubmit"` branch and fires unconditionally on any successful edit, regardless of whether anything was actually resubmitted. The seeded subject template for this event literally says "...was edited and re-sent for approval" — a claim that is false here.
- **Actual:** the manager's count increased by exactly one with an "edited" title; the request's own page still showed "sent this back" immediately after, confirming the status never left `returned`.
- **Verdict:** PASS — the assertions describe the current, confirmed-defective state honestly; see finding F-F-07.

---

## NOT RUN — Go-level only

These behaviours are either driven by a clock this environment cannot advance, or are internal to
`internal/notify`/`internal/store` in a way no route exposes to a browser. Each is already covered
by a named, passing Go test.

| # | Behaviour | Covering Go test(s) |
|---|---|---|
| NR-1 | Daily manager reminder fires once a pending request has waited ≥3 calendar days, throttled to at most once per calendar day | `internal/notify/reminders_test.go: TestRunRemindersFiresOnceThenRespectsTheCadence`; `internal/store/reminders_test.go: TestRequestsPendingReminderHonoursConfiguredThreshold`, `TestReminderThresholdsDefaultsAndOverrides` |
| NR-2 | Stale-processing nudge fires once a reservation has sat ≥1 calendar day unpaid, skipping settled/voided ones | `internal/notify/reminders_test.go: TestRunRemindersNotifiesTheAssignedAccountant`, `TestRunRemindersSkipsSettledReservations`; `internal/store/reminders_test.go: TestRequestsStaleProcessingSkipsSettledAndHonoursThreshold` |
| NR-3 | `calendarDaysBetween` counts date boundaries crossed, not elapsed hours — **correction after further reading: this function has zero production call sites** (`grep -rn "calendarDaysBetween(" internal/store/*.go` outside `_test.go` returns nothing); `RequestsPendingReminder`/`RequestsStaleProcessing` actually use `now.AddDate(0,0,-N)` elapsed-time arithmetic instead, so N4/N5 are true only under an elapsed-24h reading, not a true calendar-day-boundary one. See finding F-F-08 | `internal/store/reminders_test.go: TestCalendarDaysBetweenCountsDateBoundaries` (exercises the unused function); `TestRequestsPendingReminderHonoursConfiguredThreshold`, `TestRequestsStaleProcessingSkipsSettledAndHonoursThreshold` (exercise what actually runs) |
| NR-4 | The `Scheduler` goroutine runs once immediately, then on every tick, and stops cleanly when its context is cancelled | `internal/notify/reminders_test.go: TestSchedulerRunsOnStartAndStopsOnCancel` |
| NR-5 | SMTP message construction: RFC-822 headers, CRLF line endings, auth only when a username is configured, envelope-from parsed from the display From header, password read only from the injected value (never the DB) | `internal/notify/mailer_test.go: TestSMTPMailerBuildsMessageAndUsesEnvPassword`, `TestSMTPMailerOmitsAuthWithoutUsername`, `TestSMTPMailerSkipsEmptyRecipients`, `TestSMTPMailerRequiresHost` |
| NR-6 | `renderTemplate` substitutes only the `{{token}}` vocabulary and never executes Go template syntax (no `{{if}}`, no field access, no injection surface) | `internal/notify/service_test.go: TestRenderTemplateSubstitutesTheTokenVocabulary`, `TestRenderTemplateRejectsUnknownToken`, `TestRenderTemplateDoesNotExecuteGoTemplates` |
| NR-7 | Recipient de-duplication across To and Cc at the email layer (an address in To is dropped from Cc) | `internal/notify/service_test.go: TestResolveRecipientsDedupesAcrossToAndCc` |
| NR-8 | The urgent event's To/Cc composition at the **email** layer depends on request status the same way the in-app resolver does | `internal/notify/service_test.go: TestResolveRecipientsForUrgentDependsOnStatus` |
| NR-9 | A broken/invalid subject or body template still leaves the in-app row intact with a fallback title (label + request number) rather than losing the notification | `internal/notify/service_test.go: TestNotifyKeepsInAppRowWhenTemplateIsBroken` |
| NR-10 | An unrecognised/unseeded event name is a silent no-op (no panic, no partial write) | `internal/notify/service_test.go: TestNotifyUnknownEventIsANoOp` |
| NR-11 | `MailSettings.BaseURL` is trimmed of whitespace and a trailing slash on save | `internal/store/notifications_test.go: TestMailSettingsNormalisesBaseURL` |
| NR-12 | Disabling an event's email leaves its in-app delivery completely untouched | `internal/notify/service_test.go: TestNotifyDisabledEventStillWritesInAppButSendsNoEmail` — also demonstrated indirectly, live, by every Section-1 test above: every seeded event ships with `email_enabled=0` by default (`migrations_notifications.go`), and the in-app rows in TC-F-001–009 all fired under exactly that default |

---

## Coverage IDs exercised

**N1** (TC-F-001–023, 049, 051–057, in-app-by-default, its scoping, and its coverage gaps), **N2**
(TC-F-010, 010b, 024–034, 045–047), **N3** (TC-F-005, 010b, 031, 050), **N4**/**N5** (TC-F-043, 048,
plus NR-1/NR-2/NR-3), **N6** (TC-F-038–042), **N8** (TC-F-035–037), **S8** (TC-F-044, plus NR-2).
**G8** (approval-authority boundary, TC-F-040). **G19** (in-app centre + bell, TC-F-015, 016, 049).
**G20** (twelve events at the time of this run, twenty-one since migration v9 — TC-F-010, 032, 051–056). **A8** (edit-while-pending re-notification,
TC-F-057). **D6** (Configuration screen ownership, TC-F-046). **N7** (conversation thread visible to
viewers) is out of this document's scope — it belongs to the requests/approvals thread, not
notifications.

## Result

**58 of 58 executed test cases pass** (`FERVID_E2E_PORT=4306 npx playwright test
tests/e2e/audit-f-notifications.spec.ts --project=chromium --reporter=line` → `58 passed`), two of
them (TC-F-047, TC-F-048) intentional `test.fail()`s pinning confirmed product defects; the rest
are ordinary passing assertions, several of which (TC-F-049, 051–057) describe confirmed-defective
behaviour honestly rather than asserting it should be otherwise (proof-of-absence /
information-flow tests, not deterministic-failure ones — see each finding for why). Plus the twelve
`NOT RUN — Go-level only` entries above, each naming the Go test that already covers it. Eight
confirmed defects found and written up in `docs/qa/results/findings-f-notifications.md`:
**F-F-01** (critical), **F-F-05, F-F-06** (high), **F-F-02, F-F-03, F-F-07** (medium), **F-F-04,
F-F-08** (informational).
