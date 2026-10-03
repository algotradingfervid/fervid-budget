# Architecture

A map of the code for somebody about to change it. Every statement here was
checked against the code; file names are given so you can read on. For running
the app see the [README](../README.md); for operating it see
[OPERATIONS.md](OPERATIONS.md).

## Contents

1. [The shape of the program](#the-shape-of-the-program)
2. [Packages](#packages)
3. [How a web request is handled](#how-a-web-request-is-handled)
4. [Permissions and visibility](#permissions-and-visibility)
5. [The payment request lifecycle](#the-payment-request-lifecycle)
6. [Data model](#data-model)
7. [Transactions and concurrency](#transactions-and-concurrency)
8. [Templates and front end](#templates-and-front-end)
9. [Notifications and reminders](#notifications-and-reminders)
10. [Backups](#backups)
11. [Testing](#testing)
12. [Where to start for common changes](#where-to-start-for-common-changes)
13. [Glossary](#glossary)

## The shape of the program

One Go binary (`cmd/server`) and one SQLite file. There is no separate front-end
build: pages are rendered on the server with `html/template` and htmx swaps
parts of them. The layers are strict:

```
browser ──HTTP──> internal/app  (routes, middleware, handlers, templates)
                      │
                      ├──> internal/auth    (session, CSRF, permission gates)
                      ├──> internal/notify  (in-app + email, after a change is saved)
                      ▼
                  internal/store  (every SQL statement and business rule)
                      │
                      ▼
                  SQLite file (WAL mode) + attachment folder on disk
```

Handlers never write SQL, and the store never knows about HTTP. The store
re-checks permissions inside its own transactions, so a rule cannot be skipped
by calling a store method from a new handler.

`cmd/server/main.go` does one of three things, chosen by flag:

| Command | Does |
|---|---|
| `server --restore <dir>` | Restore database and attachments from a backup folder, then exit |
| `server --backup` | Make one backup, prune old ones, then exit |
| `server [--seed]` | Serve until SIGINT/SIGTERM. `--seed` first creates the admin and sample data on an empty database |

While serving, two background goroutines share the shutdown signal: the
**reminder job** (every hour) and the **daily backup** (checks every ten
minutes, makes at most one backup a day).

## Packages

| Package | Owns |
|---|---|
| `cmd/server` | Flags, JSON logging, start-up and graceful shutdown, the two background jobs |
| `internal/app` | The web layer. `app.go` builds the server and middleware chain; `routes.go` is the route table; one file per screen (`requests.go`, `payments.go`, `budgets.go`, `users.go`, `roles.go`, …); `render.go` and `page_data.go` fill and render pages; `templates/*.html` hold the HTML |
| `internal/store` | The only code that talks to SQLite: schema (`schema.go`), migrations (`migrations.go`, `migrations_notifications.go`), and one file per business area (`request_transitions.go`, `accounts_queue.go`, `payments.go`, `budgets.go`, `permissions.go`, `audit.go`, `backup.go`, …). Errors are the sentinels in `models.go` (`ErrNotFound`, `ErrForbidden`, `ErrValidation`, `ErrLockedMonth`, …) |
| `internal/auth` | Signed session cookie, password hashing, CSRF tokens, `RequireLogin` / `RequirePermission` wrappers, the per-request permission cache, safe `?next=` paths |
| `internal/notify` | Notification events, recipient rules, in-app rows, the SMTP mailer (host allowlist), the reminder job |
| `internal/config` | Reads `FERVID_*` environment variables with safe defaults; creates private data folders |
| `internal/money` | Amounts as whole **paise** in an `int64` (1 rupee = 100 paise), parsing and Indian formatting (lakh, crore) |
| `internal/password` | The password policy (12–72 bytes, a letter and a digit) |

Each package has a package comment; `go doc ./internal/<name>` prints it.

## How a web request is handled

The middleware chain is built in `internal/app/app.go` (`build`). From the
outside in:

1. **`httpObservability`** (`middleware.go`): gives the request an id
   (`X-Request-ID`, also printed on error pages), logs one line when it
   finishes, and turns a panic into the generic error page.
2. **`securityBoundary`** (`security.go`): sets security headers (CSP, frame
   denial, no-store, HSTS when cookies are secure). For anything but GET, HEAD
   and OPTIONS it refuses cross-site form posts (403), and rate-limits posts to
   `/login` and its sub-pages (429): per client address first, then a global
   allowance.
3. **`auth.Manager.Middleware`**: reads the session cookie and puts the
   signed-in user, with a fresh per-request permission cache, on the request.
4. **`refreshBadgesAfterMutation`** (`middleware.go`): after any non-GET
   request, clears the cached counts shown on the navigation badges.
5. **The route table** (`routes.go`, a standard `http.ServeMux`). Each route is
   registered through a small helper that states its gate:
   - `read(pattern, resource, action, h)`: a GET needing one permission.
   - `write(...)`: a POST needing one permission, then `withCSRF`.
   - `signedIn` / `signedInForm`: only a session; the handler checks the rest.
6. **`withCSRF`** (`handler_wrappers.go`), on every form POST except sign-in:
   caps the body size, parses the form, checks the CSRF token (403), and for any
   `/requests/{id}/…` path answers **404 unless the caller may see that
   request**. Handlers under `/requests/{id}` can therefore assume visibility.
7. **The handler** reads the form, calls one store method, and on success
   calls `fire` (`notify_hook.go`) to send notifications, then redirects or
   renders.
8. **The store method** opens a transaction, re-reads the rows, re-checks who
   is acting and whether the change is allowed, writes the change and its audit
   row, and commits.
9. **Rendering** (`render.go`): `renderStatus` adds what every page needs (user,
   CSRF token, permissions, navigation) and renders into a buffer, so a template
   error becomes a clean 500 instead of half a page. An htmx request
   (`HX-Request` header) gets only the fragment.

Store errors become HTTP statuses in one place, `storeErrorStatus` in
`render.go`: not found 404, locked month 409, forbidden 403, validation /
duplicate / inactive head 400, anything else 500.

After sign-in the user is sent to the page they were trying to open (`?next=`),
but only if it is a known page they may open, including the specific record in
the URL (`login_redirect.go`). Otherwise they land on Home with a note that does
not name the page.

## Permissions and visibility

There are no hard-coded role checks. Every check asks a **permission set**
(`store.PermissionSet`, in `internal/store/permissions.go`) two questions:

- **`Can(resource, action)`**: a grant such as `request:create`,
  `approval:approve`, `payment:settle`, `month:lock`. The full vocabulary is
  `resourceActions` in `permissions.go`; matching is exact, with no wildcards.
- **`Scope(resource)`**: the data scope for the two scoped resources,
  `request` and `payment`: `own`, `assigned` or `all`. No scope means nothing.

A **role** is a named bundle of grants and scopes (`roles`, `role_permissions`,
`role_data_scope` tables). A user may hold several roles; grants are added
together and the broadest scope wins (`own` < `assigned` < `all`). Four system
roles are created by the first migration: Requester (request scope `own`),
Manager (`all`), Accounts (`all` for requests and payments) and Admin (every
grant, `all`). Roles are copied and edited on the Roles screen. Somebody who is
not an administrator can only grant permissions and scopes they hold
themselves, and the last administrator cannot be removed
(`internal/store/security.go`).

**Who may see a request** is one rule, written once in Go (`RequestInScope`)
and once in SQL (`requestScopeSQL`) in `internal/store/request_scope.go`, and
tested against each other:

| Scope | Sees |
|---|---|
| `all` | Every request |
| `assigned` | Requests routed to them for approval **or** raised by them |
| `own` | Only requests they raised |
| none / unknown | Nothing (fails closed) |

"Which requests are waiting for *my* decision" is a separate filter
(`RequestListOptions.RoutedToViewer`), never a scope.

**A hidden request answers 404, exactly like a missing one**, on every page and
form, so request ids cannot be probed. A payment linked to a request is visible
to whoever may see that request; a payment without a request needs
`payment:view` and the payment scope (`userReadsPayment` in
`internal/app/login_redirect.go`).

Checks happen in three places:

1. **The route gate** (`routes.go`): the permission the URL needs.
2. **The handler**, for rules that depend on the form or the record (for
   example "create or edit" on `POST /users`, or "partial payment needs
   `payment:mark_partial`").
3. **The store, inside the write transaction**: who the actor is on this row
   (only the request's approver may decide it, never the requester), the
   current status, and visibility read fresh in the same transaction
   (`requestVisibleTx`), so a role change cannot race a form opened earlier.

Permission sets are read at most once per request per user (the cache in
`internal/auth/auth.go`), so a role change applies on the very next request.

Other protections worth knowing: sessions are signed with the user's password
hash and session version, so changing a password or signing out ends every
session of that user; failed sign-ins all get the same "Invalid email or
password" and do **not** lock the account (the per-source rate limit above does
the throttling).

## The payment request lifecycle

A request exists only once it is submitted; there are no drafts. Statuses
(`payment_requests.status`), from `internal/store/request_transitions.go` and
`accounts_queue.go`:

```
                 ┌──────────── return ───────────┐
                 ▼                               │
  raise ──> pending ──approve──> approved ──reserve──> processing ──record payment──> completed
             │  ▲                 │   ▲  ▲               │   │
             │  └─ resubmit ─ returned │  │ release /    │   └─ short, needs review ─> partial_review
             │                     │   │  │ instalment   │                               │
             ├─ reject ──> rejected│   │  └──────────────┘          accept shortfall ────┼─> completed_partial
             │   (re-raise makes   │   │                            continue balance ────┘   (back to approved)
             │    a new request)   │   └── decline ──┐
             └─ withdraw ─> withdrawn                 │
                                   ├─ ask to cancel ─> cancellation_requested ── accept ──> cancelled
                                   └─ cancel outright ─────────────────────────────────────> cancelled
```

| From | To | Who / how | Store method |
|---|---|---|---|
| — | `pending` | Requester raises it (no drafts) | `CreateRequest` in `requests.go` |
| `pending` | `approved` | The request's approver; amount may be lowered, never raised; refused if the required-by month is locked | `ApproveRequest` |
| `pending` | `returned` | The approver, with a comment | `ReturnRequest` |
| `returned` | `pending` | Requester corrects and resubmits | `EditRequest` / `SubmitRequest` |
| `pending` | `rejected` | The approver, with a reason (final) | `RejectRequest` |
| `rejected` | new `pending` request | Requester re-raises: a **new** request with a new number | `ReraiseRequest` |
| `pending` | `withdrawn` | Requester (final) | `WithdrawRequest` |
| `approved` | `processing` | An accountant reserves it from the queue (not while on hold) | `ReserveRequest` |
| `processing` | `approved` | Reservation released, or an instalment leaves a balance | `ReleaseRequest`, `RecordPaymentForRequest` |
| `processing` | `completed` | Payment recorded as fully settled | `RecordPaymentForRequest` |
| `processing` | `partial_review` | Payment recorded as short; goes back to the approver | `RecordPaymentForRequest` |
| `partial_review` | `completed_partial` | Approver accepts the shortfall (final) | `AcceptPartial` |
| `partial_review` | `approved` | Approver asks for the rest to be paid | `ContinuePartial` |
| `approved` | `cancellation_requested` | Requester asks to cancel | `RequestCancellation` |
| `cancellation_requested` | `cancelled` / `approved` | Approver accepts / declines | `DecideCancellation` |
| `approved` | `cancelled` | Approver cancels outright, with a reason | `CancelRequest` |

Other facts about the lifecycle:

- **On hold** is not a status. It is the `on_hold` flag on an `approved`
  request, set and cleared by Accounts (`HoldRequest`, `UnholdRequest`). A held
  request cannot be reserved.
- **Stale decisions are refused.** Approve, return, reject and the cancellation
  decisions carry the request's *revision* (a hash of the row plus its newest
  audit id, `RequestEditRevision` in `requests.go`). If the request changed after
  the approver opened it, the decision is refused with "this request changed;
  reload and review it before deciding".
- **Races lose cleanly.** Every transition's `UPDATE` repeats the status it just
  read in its `WHERE`; if no row changed, the caller gets "somebody else changed
  this request a moment ago" (`ErrRequestRaced`).
- **Payments against a request** can never exceed the approved amount, cannot
  be dated in the future, before the day of approval, or in a locked month.
- Once any money is paid, the request can no longer be cancelled.

The QA state diagrams in [`docs/qa/uml/02-state-machines.md`](qa/uml/02-state-machines.md)
draw the same machine in more detail.

## Data model

Money is always an integer number of **paise**. Database triggers
(`installFinancialLimits` in `internal/store/security.go`) refuse negative or
absurd amounts on every table that stores one.

The main tables (the original ones are in `internal/store/schema.go`; later
columns and tables come from `migrations.go` and the migration functions it
lists):

| Table | Holds |
|---|---|
| `users`, `roles`, `user_roles`, `role_permissions`, `role_data_scope` | People and what they may do |
| `projects`, `heads` | Projects and the expense heads inside them |
| `budgets`, `budget_lines`, `budget_months` | Monthly budget per head (with optional line items); which months are open for planning |
| `month_locks` | Closed months: see *Month lock* in the glossary |
| `payment_requests` | Every request: type, treatment (`budget` or `recoverable`), amounts, requester, approver (`manager_id`), status, hold and reservation fields |
| `request_comments`, `request_attachments` | The conversation and documents on a request |
| `payments`, `payment_attachments` | Money actually paid, usually linked to a request (`request_id`) |
| `vendors` | Vendor master, including bank details |
| `recoverable_categories`, `recovery_events` | Kinds of recoverable spend; money coming back against a recoverable request |
| `notifications`, `notification_settings` | In-app notification rows; the admin's rule per event |
| `app_settings` | Admin configuration (numbering, reminder waits, payment modes, mail settings, …) |
| `audit_log` | One row for every change, written in the same transaction as the change |
| `request_number_seq` | Request numbering per year |
| `password_resets`, `password_reset_throttle` | Emailed reset tokens (hashed) and their rate limit |

**Migrations.** `store.Open` runs `schemaSQL` (every statement `IF NOT EXISTS`)
and then `migrate`, which applies each entry in the `migrations` list newer than
the database's `PRAGMA user_version`, one transaction each. Never edit or
renumber a shipped migration; append a new one (the comment above `migrations`
in `migrations.go` has the rules).

## Transactions and concurrency

SQLite allows one writer at a time. The store has a single rule for this
(`internal/store/tx.go`):

- The connection string sets `_txlock=immediate`, so every transaction that may
  write takes the write lock at `BEGIN`. Two writers queue on the 5-second
  `busy_timeout` instead of the second failing half-way.
- **`beginWriteTx`** opens a transaction that may write; **`beginReadTx`**
  opens one marked read-only, which never takes the write lock. Always use one of
  the two, never `db.BeginTx` directly.
- The database runs in WAL mode, so readers and the writer do not block each
  other, and backups (`VACUUM INTO`) run while the app is in use.

Inside a write transaction a store method re-reads the row, checks the rules,
writes with a guarded `UPDATE`, writes the audit row, and commits. Notifications
are sent by the handler **after** the commit, and a mail failure is logged, not
returned, so it never undoes or hides a saved change.

## Templates and front end

- **HTML** lives in `internal/app/templates/*.html`, one file per feature, and is
  compiled into the binary with `embed.FS` (`internal/app/templates.go`). A
  syntax error stops the server at start-up.
- Each `{{define "name"}}` is a page or a partial. Full pages start with
  `{{template "top" .}}` and end with `{{template "bottom" .}}` (both in
  `shell.html`, with the sidebar, mobile tab bar and flash messages). Shared
  partials such as `head_common`, `nav_item` and `sheet_foot` are there too.
- The functions templates may call are in `internal/app/template_funcs.go`
  (money formatting, status and label text, `dict` for passing several values to
  a partial, …). Label tables (statuses, payment modes, …) are in
  `display_text.go`.
- Every page gets a `PageData` value (`page_data.go`).
- **Static files** are in `web/static/` and only the named files in
  `publicStatic` (`security.go`) are served. The folder is found relative to the
  working directory, so run the server from the repository root.
- **JavaScript** is small and progressive: htmx (pinned, with script evaluation
  disabled) plus `fervid-app.js` and the budget planner's `budget-planner.js`.
  The Content Security Policy allows only same-origin scripts, so there are no
  inline scripts or event handlers.

## Notifications and reminders

`internal/notify`. Each workflow event (`events.go`: `request_submitted`,
`request_approved`, `payment_settled`, `reminder_pending`, …) has a row in
`notification_settings` where an admin chooses who is told (requester,
approver, Accounts, extra addresses), whether email goes out, and the subject
and body with `{{token}}` placeholders.

- A handler calls `fire(r, event, requestID)` after its change is committed.
  `notify.Service.Notify` works out the recipients, **always** writes an in-app
  row for each (shown in the Notifications centre), and sends email
  only if the admin switched email on for that event. An urgent request also
  raises `request_urgent` when submitted and approved.
- Notification reads are filtered by current request visibility, so losing
  access to a request also hides its old notifications.
- Email goes through one SMTP mailer (`mailer.go`) that only talks to hosts in
  `FERVID_SMTP_ALLOWED_HOSTS`, over verified TLS. Host and sender are admin
  settings; the password comes only from `FERVID_SMTP_PASSWORD`.
- **The reminder job** (`reminders.go`) runs every hour on the same service and
  mailer. It reminds approvers about requests pending longer than the configured
  wait, and accountants about reservations held too long. Each request is stamped
  when reminded, so it is not reminded again until the wait passes once more.

## Backups

`internal/store/backup.go` and `backup_schedule.go`. A backup is a folder
`backup-YYYYMMDD-HHMMSS/` holding a `VACUUM INTO` copy of the database, a copy
of the attachments and a `manifest.json`. They are made by the daily job, by an
admin on the Backups screen, or by `server --backup`; `server --restore <dir>`
restores one (stop the app first). Retention keeps every backup younger than
`FERVID_BACKUP_KEEP_DAYS` plus the newest of each of the last
`FERVID_BACKUP_KEEP_MONTHS` months. Details: [OPERATIONS.md](OPERATIONS.md).

## Testing

| Layer | Where | Run with |
|---|---|---|
| Go unit and integration tests | `*_test.go` next to the code. Store tests use temporary databases; app tests drive the real handlers and middleware in-process | `make test` (`make test-race` for the race detector) |
| Shipped browser suite | `tests/e2e/*.spec.ts` except `audit-*`; `playwright.config.ts` starts its own freshly seeded server and throwaway database | `make test-e2e` |
| QA audit suite | `tests/e2e/audit-*.spec.ts`; `scripts/run-audit.sh` gives each area its own server and database | `make test-audit` |
| Type check of the test code | `tsconfig.json` | `make typecheck` |
| User manual build | `tests/manual/`; checks every screenshot and link exists | `make manual` (needs your own server on :8080) |

The two browser suites **must not share a database**: the audit suite builds a
large world (custom roles, dozens of users, hundreds of requests) that breaks the
shipped suite's assumptions. Test servers raise the sign-in rate limits
(`FERVID_LOGIN_LIMIT_PER_IP`, `FERVID_LOGIN_LIMIT_GLOBAL`) because every test
signs in from the same address.

A bug fix comes with a test that fails before the fix and passes after it.

## Where to start for common changes

- **A new screen:** add the route in `routes.go` with its permission, a handler
  file in `internal/app`, a `{{define}}` in a template under `templates/`, and
  the store method it needs. Add it to the navigation in `nav.go` if it belongs
  in the menu.
- **A new permission:** add the action to `resourceActions` (and a new resource
  to `resourceOrder`) in `internal/store/permissions.go`, its label in
  `internal/app/permmap.go`, and a migration if existing roles should receive it.
- **A new request rule or transition:** `internal/store/request_transitions.go`
  (or `accounts_queue.go` for Accounts' steps), following the pattern at the top
  of that file; then the handler and the manual.
- **A new notification event:** a constant in `internal/notify/events.go`
  (and its recipients in `notify`), a migration adding its
  `notification_settings` row (the original rules are
  `defaultNotificationSettings` in `internal/store/migrations_notifications.go`),
  and a `fire` call in the handler after the change.
- **A database change:** append a migration to `internal/store/migrations.go`.

## Glossary

| Term | Meaning |
|---|---|
| **Request** | A payment request: somebody asking for money to be paid, which an approver decides and Accounts pays. Numbered from a prefix, year and sequence (for example `PR-2026-000123`; the format is set in Configuration). |
| **Project** | A piece of work or cost centre that budgets are grouped under. |
| **Head** | An expense head: one spending line inside a project (for example "Travel"). Budgets and budget-treated requests are booked against a head. |
| **Budget** | The amount planned for one head in one month. The variance grid compares it with what was paid. |
| **Approver** | The person a request is routed to for a decision (`manager_id`). Never the requester. |
| **Reservation** | An accountant claiming an approved request from the payment queue (status `processing`), so two people do not pay it twice. It can be released or handed to someone else. |
| **Hold** | Accounts parking an approved request (the `on_hold` flag) until the requester supplies something. Nobody can reserve it meanwhile. |
| **Settlement** | Recording the payment for a request: fully settled (`completed`), an instalment with a balance left (back to `approved`), or short (`partial_review`). |
| **Partial review** | A short payment waiting for the approver to accept the shortfall (`completed_partial`) or ask for the balance to be paid. |
| **Recoverable** | Money paid out that should come back (deposits, guarantees, advances). Raised with the `recoverable` treatment, has no head, and is tracked in the recoverables register until recovery events account for it. |
| **Month lock** | An administrator closing a month (with a reason). Payments and recovery events dated in a locked month cannot be recorded, edited or voided, its budgets cannot change, and requests needed that month cannot be approved. |
| **Data scope** | How far a permission reaches: `own`, `assigned` or `all` (see [Permissions](#permissions-and-visibility)). |
| **Paise** | One hundredth of a rupee. Every amount in the code and database is a whole number of paise. |
| **Audit log** | The permanent record of who changed what, written with every change. |
