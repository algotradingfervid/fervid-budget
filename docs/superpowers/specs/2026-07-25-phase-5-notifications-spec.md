# Phase 5 — Notifications Engine, Reminders & Email Config · Spec

**Parent:** `2026-07-25-payment-requests-overview.md`. **Covers matrix IDs:** N1, N2, N3, N4, N5, N6, N8, S8, T10 (runtime admin attachments-mandatory toggle), and the email half of A8. **Depends on:** Phase 1 (migration runner + `auth.Manager.Can`/`Scope` + `role_permissions`/`user_roles` + `.Perms.Can` permission-driven nav), Phase 2 (`store.Request`, request submit/approve/edit events, `RequesterID`/`ManagerID`/`SubmittedAt`, the `app_settings` key/value table + `AppSetting`/`SetAppSetting` accessors from the `v2` migration, and a `SubmitRequest` that reads the `require_attachments` key fresh via `AppSetting(ctx,"require_attachments")=="1"`), Phase 3 (`processing_at`, the `processing` state).

**Goal:** Add an **email layer** on top of the in-app queues Phase 2/3 already surface: a new `internal/notify` package with a `Mailer` interface, an SMTP implementation whose password comes only from the environment, and a `Service` that resolves recipients, renders per-event templates, and sends. Add a `notification_settings` (per-event config) table with store accessors (the `app_settings` key/value table already exists from Phase 2's `v2` migration and is **consumed** here — SMTP host/port/username/from, the management recipient list, and the runtime `require_attachments` toggle are stored as `app_settings` keys via `SetAppSetting`), an admin `/notifications` screen behind `RequirePermission("notification", …)`, and a deterministic reminder engine (`RunReminders(ctx, now time.Time)`) driven by a scheduler goroutine that stops on shutdown. Urgent is a **flag only** — it changes who is emailed and when, never who may approve.

---

## 1. Dependencies consumed & scope boundary

**Consumed from earlier phases (exact names this phase relies on):**
- Phase 1: `func migrate(db *sql.DB) error` + the ordered `[]migration{Version int, Name string, Up func(*sql.Tx) error}` slice in `internal/store/migrations.go`; `columnExists(tx,table,col)`; tables `role_permissions(role_id,resource,action)`, `user_roles(user_id,role_id)`; `auth.Manager.Can(u,resource,action) bool`; canonical `notification`{view,edit} vocabulary (overview §5).
- Phase 2: `store.Request` with at least `ID int64`, `Number string`, `Status string`, `Treatment string`, `Type string`, `Amount int64`, `ApprovedAmount *int64`, `Purpose string`, `VendorPayee string`, `Project string`, `Head string`, `RequesterID int64`, `ManagerID int64`, `SubmittedAt *time.Time`, `Urgent bool`; store mutations `CreateRequest`, `SubmitRequest`, `ApproveRequest`, `UpdateRequest` (edit-while-pending). The Phase-2 approve/submit/edit handlers are the hook points for `Service.Notify`. **Also from Phase 2 (`v2` migration):** the `app_settings(key,value)` table and its `AppSetting(ctx,key)`/`SetAppSetting(ctx,actor,key,value)` accessors — Phase 5 consumes both (it does **not** create `app_settings`). Phase 2's `SubmitRequest` enforces attachments by reading the `require_attachments` key fresh each call (`AppSetting(ctx,"require_attachments")=="1"`), so Phase 5 drives enforcement purely by writing that key (no separate enforcement-flag method exists).
- Phase 3: `store.Request.ProcessingAt *time.Time` and the `processing` status; `ReserveRequest`.

**Owned by this phase:** the `reminder_last_sent DATETIME` column on `payment_requests` (declared in the Phase-2 schema as "Phase 5 scheduler bookkeeping") — Phase 5 provides its only writers (`MarkReminderSent`, `ResetReminder`).

**Out of scope (do not build):** a persistent in-app `notifications` table (in-app surfacing is the Phase-2/3 queues — N1); SMS/push; per-recipient delivery receipts; retry/queue durability (a failed send is logged, not persisted); storing any secret in the DB.

**N1 coordination note:** in-app notification is already delivered by the Phase-2/3 work queues (the pending-approval and processing lists) — Phase 5 adds **only** a configurable **email** layer on top of them. Email is opt-in per event (`email_enabled`); a disabled event stays in-app-only, so the queues remain the source of truth (verified by the email opt-out test `TestNotifyDisabledEventSendsNoEmail`).

---

## 2. Schema (migration `v5`)

Registered as the next contiguous entry appended to the Phase-1 migration slice (`{Version: 5, Name: "notifications", Up: upNotifications}` — the canonical numbering is `v1`=Phase 1, `v2`=Phase 2, `v3`=Phase 3, `v4`=Phase 4, `v5`=Phase 5). **Phase 5's `v5` migration creates the `notification_settings` table only.** The `app_settings` key/value table already exists from Phase 2's `v2` migration; Phase 5 does **not** create it — it consumes it (SMTP host/port/username/from, `management_recipients`, and the runtime `require_attachments` toggle are stored as `app_settings` keys via the Phase-2 `SetAppSetting` accessor). The new table uses `CREATE TABLE IF NOT EXISTS`; the migration is idempotent and re-running an already-migrated DB is a no-op.

```sql
-- app_settings (key/value) is created by Phase 2's v2 migration and reused here — NOT created by v5.
-- Keys consumed by Phase 5 via AppSetting/SetAppSetting:
--   smtp_host, smtp_port, smtp_username, smtp_from_name, smtp_from_addr,
--   management_recipients, require_attachments.

CREATE TABLE IF NOT EXISTS notification_settings (
  event             TEXT PRIMARY KEY,               -- see §5.1 event constants
  email_enabled     INTEGER NOT NULL DEFAULT 0,
  to_recipients     TEXT NOT NULL DEFAULT '',
  cc_recipients     TEXT NOT NULL DEFAULT '',
  include_requester INTEGER NOT NULL DEFAULT 0,
  include_manager   INTEGER NOT NULL DEFAULT 0,
  include_accounts  INTEGER NOT NULL DEFAULT 0,
  subject_template  TEXT NOT NULL DEFAULT '',
  body_template     TEXT NOT NULL DEFAULT ''
);
```

`upNotifications` also **seeds one row per event** (`INSERT … ON CONFLICT(event) DO NOTHING`) so the admin screen always has a full matrix. Seeded defaults (all `email_enabled=0` — email is opt-in; in-app remains the default per N1):

| event | include flags (default) | default subject | default body (excerpt) |
|---|---|---|---|
| `request_approved` | requester, accounts | `Request {{.Number}} approved` | `{{.Number}} for {{money .ApprovedAmount}} ({{.Project}} / {{.Head}}, payee {{.Payee}}) was approved.` |
| `request_submitted_urgent` | manager | `URGENT: {{.Number}} needs approval` | `Urgent request {{.Number}} for {{money .Amount}} is awaiting your approval.` |
| `request_approved_urgent` | accounts | `URGENT: {{.Number}} approved — pay now` | `Urgent request {{.Number}} for {{money .ApprovedAmount}} was approved and is ready to pay.` |
| `request_edited` | manager | `Request {{.Number}} was edited` | `Pending request {{.Number}} was edited and re-sent for your approval.` |
| `reminder_pending` | manager | `Reminder: {{.Number}} awaiting approval` | `Request {{.Number}} for {{money .Amount}} has been pending since {{.SubmittedOn}}.` |
| `reminder_processing_stale` | accounts | `Reminder: {{.Number}} still processing` | `Request {{.Number}} has been in processing since {{.ProcessingOn}} with no settlement.` |

The management recipient list (`app_settings.management_recipients`) is **not** a per-event column; it is appended to `Cc` at send time for the two approval events `request_approved` and `request_approved_urgent` (§5.3), satisfying N3 ("approval email → Accounts + requester + management list") and the post-approval half of N6.

---

## 3. Config (`internal/config/config.go`)

Add exactly one field, the env-only secret. All other SMTP settings (host/port/username/from/management list) are admin-editable in `app_settings` and therefore live in the DB, never in `Config`.

```go
type Config struct {
    // …existing fields…
    SMTPPassword string // FERVID_SMTP_PASSWORD — env only, never persisted (N8)
}
```

Loaded via the existing helper: `SMTPPassword: env("FERVID_SMTP_PASSWORD", "")`. `Config` has no `smtp_password` accessor beyond this field, and nothing writes it to `app_settings`.

---

## 4. Store API

All new store code lives in `internal/store/notifications.go` (accessors) and `internal/store/reminders.go` (scheduler queries). Mutating accessors follow the house style (`ctx, actor User`, `BeginTx`+`defer Rollback`+`Commit`, `recordAuditTx`); the two reminder-bookkeeping writers are un-audited system operations (no actor — the scheduler) and are documented as the deliberate exception.

**`app_settings` is Phase 2's:** the `app_settings` table and its key/value accessors `AppSetting(ctx,key)`/`SetAppSetting(ctx,actor,key,value)` are provided by Phase 2 (`v2`). Phase 5's `GetAppSettings`/`SetAppSettings` are **typed convenience wrappers** that read/write the individual `smtp_*` + `management_recipients` keys through `AppSetting`/`SetAppSetting` — Phase 5 never creates `app_settings` and never writes it with raw SQL. The `require_attachments` toggle is a separate `app_settings` key read/written directly via `AppSetting`/`SetAppSetting` (§7).

### 4.1 Types

```go
type AppSettings struct {
    SMTPHost             string
    SMTPPort             int
    SMTPUsername         string
    SMTPFromName         string
    SMTPFromAddr         string
    ManagementRecipients string // raw comma-separated list
}

type NotificationSetting struct {
    Event            string
    EmailEnabled     bool
    ToRecipients     string
    CcRecipients     string
    IncludeRequester bool
    IncludeManager   bool
    IncludeAccounts  bool
    SubjectTemplate  string
    BodyTemplate     string
}
```

### 4.2 Methods

```go
// app_settings (key/value) — table + AppSetting/SetAppSetting are Phase 2's (v2);
// these are typed wrappers that read/write the smtp_* + management_recipients keys via SetAppSetting.
func (s *Store) GetAppSettings(ctx context.Context) (AppSettings, error)
func (s *Store) SetAppSettings(ctx context.Context, actor User, in AppSettings) error

// notification_settings
func (s *Store) NotificationSetting(ctx context.Context, event string) (NotificationSetting, error) // ErrNotFound if unseeded
func (s *Store) AllNotificationSettings(ctx context.Context) ([]NotificationSetting, error)          // ordered by event
func (s *Store) SetNotificationSetting(ctx context.Context, actor User, in NotificationSetting) error // upsert, audited

// recipient expansion — accounts = users whose union of roles grants (payment, process)
func (s *Store) UsersWithPermission(ctx context.Context, resource, action string) ([]User, error)

// reminder engine (clock injected)
func (s *Store) RequestsPendingReminder(ctx context.Context, now time.Time) ([]Request, error)
func (s *Store) RequestsStaleProcessing(ctx context.Context, now time.Time) ([]Request, error)
func (s *Store) MarkReminderSent(ctx context.Context, requestID int64, now time.Time) error // un-audited bookkeeping
func (s *Store) ResetReminder(ctx context.Context, requestID int64) error                   // sets reminder_last_sent=NULL (A8 reset)
```

**`UsersWithPermission`** (drives `include_accounts`): joins `users → user_roles → role_permissions` and returns active users granted the `(resource, action)` grant:
```sql
SELECT DISTINCT u.id,u.email,u.name,u.password_hash,u.role,u.active,u.created_at,u.updated_at
FROM users u
JOIN user_roles ur ON ur.user_id=u.id
JOIN role_permissions rp ON rp.role_id=ur.role_id
WHERE rp.resource=? AND rp.action=? AND u.active=1
ORDER BY u.name;
```

**Calendar-day semantics** (deterministic, timezone-stable — all timestamps read as UTC):
```go
// calendarDaysBetween returns the number of whole calendar-date boundaries from a to b (b>=a).
func calendarDaysBetween(a, b time.Time) int {
    da := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
    db := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
    return int(db.Sub(da).Hours() / 24)
}
```
- `RequestsPendingReminder`: `status='pending'`, `submitted_at NOT NULL`, `calendarDaysBetween(submitted_at, now) >= 3`, and `(reminder_last_sent IS NULL OR calendarDaysBetween(reminder_last_sent, now) >= 1)` (daily throttle — at most one pending reminder per calendar day per request).
- `RequestsStaleProcessing`: `status='processing'`, `processing_at NOT NULL`, `calendarDaysBetween(processing_at, now) >= 1`, same daily throttle.

Both load candidate rows by status via SQL, then apply the calendar-day predicates in Go against the injected `now` (avoids driver-specific SQLite date arithmetic and keeps the clock a single injected value). Each returns fully-populated `store.Request` rows (including `RequesterID`, `ManagerID`, `SubmittedAt`, `ProcessingAt`, `ReminderLastSent`).

---

## 5. `internal/notify` package

Files: `mailer.go` (interface + Message + SMTP impl), `service.go` (Service + Notify + recipients + templates), `reminders.go` (RunReminders + Scheduler), `events.go` (event constants).

### 5.1 Event constants (`events.go`)
```go
const (
    EventRequestApproved         = "request_approved"
    EventRequestSubmittedUrgent  = "request_submitted_urgent"
    EventRequestApprovedUrgent   = "request_approved_urgent"
    EventRequestEdited           = "request_edited"
    EventReminderPending         = "reminder_pending"
    EventReminderProcessingStale = "reminder_processing_stale"
)
```

### 5.2 Mailer (`mailer.go`)
```go
type Message struct {
    From    string   // "Name <addr>" or bare addr
    To      []string
    Cc      []string
    Subject string
    Body    string   // plain text
}

type Mailer interface {
    Send(ctx context.Context, msg Message) error
}
```

**SMTP implementation** — password from env only, host/port/username loaded from `app_settings` at send time (so admin edits take effect without a restart). `sendMail` is an injectable function field defaulting to `smtp.SendMail`, enabling a unit test without a live server.
```go
type SMTPMailer struct {
    st       *store.Store
    password string // config.SMTPPassword (FERVID_SMTP_PASSWORD)
    sendMail func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}
func NewSMTPMailer(st *store.Store, password string) *SMTPMailer
func (m *SMTPMailer) Send(ctx context.Context, msg Message) error
```
`Send` loads `AppSettings`, requires `SMTPHost != ""` (else `errors.New("smtp host not configured")`), builds `addr = host:port` (port defaults to `587` when unset), builds `auth = smtp.PlainAuth("", username, m.password, host)` when `username != ""` else `nil`, assembles an RFC-5322 message with CRLF line endings:
```
From: <From>\r\nTo: <To joined by ", ">\r\nCc: <Cc joined>\r\nSubject: <Subject>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n<Body>
```
and calls `m.sendMail(addr, auth, envelopeFrom, append(To, Cc...), msg)` where `envelopeFrom` is the bare address parsed from `AppSettings.SMTPFromAddr`. The password is never logged and never read from or written to the DB (N8).

### 5.3 Service & recipient resolution (`service.go`)
```go
type Service struct {
    st     *store.Store
    mailer Mailer
}
func NewService(st *store.Store, mailer Mailer) *Service
func (s *Service) Notify(ctx context.Context, event string, req store.Request) error // overview §8 signature
```

`Notify`:
1. `cfg, err := st.NotificationSetting(ctx, event)` — `ErrNotFound` ⇒ return `nil` (unknown/unconfigured event; in-app only, N1).
2. `!cfg.EmailEnabled` ⇒ return `nil` (email opt-out; in-app queues remain the source of truth — N1).
3. Load `app, _ := st.GetAppSettings(ctx)`; resolve requester/manager via `st.UserByID`; if `cfg.IncludeAccounts`, `accounts := emails(st.UsersWithPermission(ctx,"payment","process"))`.
4. Build the render view and resolve recipients (below); if `len(To)==0 && len(Cc)==0` ⇒ return `nil`.
5. Render `Subject`/`Body` from templates (§5.4); `Send` via `s.mailer`.

**Recipient resolver** (pure, unit-tested directly):
```go
func resolveRecipients(event string, cfg store.NotificationSetting, app store.AppSettings, v RequestView, accounts []string) (to, cc []string)
```
- `to` = split(`cfg.ToRecipients`) + (requester if `IncludeRequester`) + (manager if `IncludeManager`) + accounts (if `IncludeAccounts`).
- `cc` = split(`cfg.CcRecipients`) + management list (split(`app.ManagementRecipients`)) **iff** `event ∈ {request_approved, request_approved_urgent}`.
- Both lists are trimmed, blanks dropped, lower-cased, and de-duplicated; an address already in `to` is removed from `cc`.

`RequestView` decouples email templates from store-struct churn and is built from `store.Request` + resolved users:
```go
type RequestView struct {
    Number, Treatment, Type, Project, Head, Payee, Purpose, Status string
    Amount, ApprovedAmount int64
    RequesterName, RequesterEmail, ManagerName, ManagerEmail string
    SubmittedOn, ProcessingOn string // "2006-01-02" or "" 
}
```

### 5.4 Template rendering (`service.go`)
`text/template` (not `html/template` — bodies are plain-text email, no HTML escaping of ₹) with a small FuncMap so authors can format money:
```go
func renderTemplate(tmpl string, v RequestView) (string, error) {
    t, err := template.New("n").Option("missingkey=zero").Funcs(template.FuncMap{
        "money": money.FormatPaise,
        "short": money.FormatShort,
    }).Parse(tmpl)
    if err != nil { return "", err }
    var b strings.Builder
    if err := t.Execute(&b, v); err != nil { return "", err }
    return b.String(), nil
}
```
Templates reference `{{.Number}}`, `{{money .Amount}}`, `{{money .ApprovedAmount}}`, `{{.Project}}`, `{{.Head}}`, `{{.Payee}}` (N-events render request fields: number, amount via money, project/head, payee).

---

## 6. Reminders & scheduler (`reminders.go`) — clock injection

```go
func (s *Service) RunReminders(ctx context.Context, now time.Time) error
func (s *Service) Scheduler(ctx context.Context, interval time.Duration, now func() time.Time)
```

`RunReminders` (deterministic — `now` is injected, never `time.Now()` inside):
- For `EventReminderPending`: if that event is enabled, for each `st.RequestsPendingReminder(ctx, now)` call `Notify(ctx, EventReminderPending, req)` then `st.MarkReminderSent(ctx, req.ID, now)` (daily throttle bookkeeping).
- For `EventReminderProcessingStale` (S8): same loop over `st.RequestsStaleProcessing(ctx, now)`.
- A disabled reminder event short-circuits (no send, no bookkeeping).

`Scheduler` runs `RunReminders(ctx, now())` once immediately, then on every `interval` tick, and returns when `ctx.Done()` — this is the goroutine body wired in `cmd/server`. It is directly testable with a small interval + a cancelable context + a `fakeMailer`.

**`cmd/server/main.go` wiring:** after `srv, err := app.New(cfg, db)` and before the serve goroutine, build a scheduler-side service and launch it under the existing shutdown context:
```go
notifier := notify.NewService(db, notify.NewSMTPMailer(db, cfg.SMTPPassword))
go notifier.Scheduler(ctx, 24*time.Hour, func() time.Time { return time.Now().UTC() })
```
`ctx` is the `signal.NotifyContext` already in `main`; on SIGINT/SIGTERM the scheduler returns, so shutdown stays graceful. (`app.New` stays `New(cfg, st) (*http.Server, error)` — it builds its own handler-side `a.notify` internally, so the httptest harness is unchanged.)

---

## 7. UI / routes (`internal/app`)

App gains a `notify *notify.Service` field, constructed inside `app.New` from `notify.NewSMTPMailer(st, cfg.SMTPPassword)` + `notify.NewService`. New handlers in `internal/app/notifications.go`, template block `"notifications"` appended to `internal/app/templates.go`, funcs added to the FuncMap as needed.

Routes (all behind the Phase-1 gate; POSTs behind `withCSRF`):
- `GET /notifications` — `RequirePermission("notification","view")`: renders the SMTP/app-settings form (host, port, username, from name, from addr, management recipients, **and a `require_attachments` toggle** — "make an attachment mandatory before a request can be submitted", T10) **and** the per-event matrix (enable checkbox, To, Cc, include-requester/manager/accounts checkboxes, subject template, body template). The SMTP form shows a **read-only note** — "Password is read from `FERVID_SMTP_PASSWORD`; it is never stored." — and has **no password input** (N8). The toggle's rendered state is read from the `app_settings` `require_attachments` key via `AppSetting`.
- `POST /notifications/smtp` — `RequirePermission("notification","edit")`: `SetAppSettings` for the SMTP/management keys, and persists the `require_attachments` toggle via `SetAppSetting(ctx, actor, "require_attachments", value)` (`value` is `"1"` when on, `"0"` when off). No runtime mirror call is needed — Phase-2 `SubmitRequest` reads that key fresh via `AppSetting`, so the toggle takes effect immediately (T10 — the runtime, `app_settings`-backed toggle that drives Phase 2's attachment-mandatory enforcement).
- `POST /notifications/events/{event}` — `RequirePermission("notification","edit")`: `SetNotificationSetting` for one event row.

Nav: a "Notifications" link is added to the Settings group, gated by `{{if .Perms.Can "notification" "view"}}…{{end}}` (Phase-1 permission-driven nav); `(.Perms.Can "notification" "view")` is also added to the Settings-group `{{if or …}}` visibility condition so the group shows for notification-only admins.

---

## 8. Event wiring (hooks into Phase 2/3 handlers)

Phase 5 adds fire-and-forget `Notify` calls at the existing Phase-2/3 decision points (errors logged, never blocking the user's request):
- Phase-2 **approve** handler → `a.notify.Notify(ctx, notify.EventRequestApproved, req)`; if `req.Urgent`, also `notify.EventRequestApprovedUrgent`.
- Phase-2 **submit** handler → if `req.Urgent`, `notify.Notify(ctx, notify.EventRequestSubmittedUrgent, req)` (immediate manager email — the pre-approval half of N6).
- Phase-2 **edit-while-pending** handler → `a.notify.Notify(ctx, notify.EventRequestEdited, req)` (A8 email) and `a.st.ResetReminder(ctx, req.ID)` (A8 reminder reset, so the pending reminder can fire again).

**Urgent authority invariant:** urgent changes only notification routing/timing. The approve path still enforces the unchanged Phase-2 approval rules; a test asserts a non-manager cannot approve an urgent request and that `ApproveRequest` behaves identically for urgent and non-urgent requests (N6 "no authority").

---

## 9. Acceptance criteria (P5 "done")

- Opening a freshly-migrated DB creates `notification_settings` with one seeded row per event (the `app_settings` table is already present from Phase 2's `v2` migration — `v5` does not re-create it); re-opening is a no-op (idempotent migration).
- Enabling `request_approved` and approving a request sends exactly one email whose recipients are the requester + all Accounts users (To) and the management list (Cc), with subject/body rendered from the templates and amounts formatted by `internal/money`.
- Urgent submit emails the manager immediately; urgent approve emails Accounts + management; approval authority is unchanged for urgent requests.
- `RunReminders` with an injected `now` emails the manager once per calendar day for requests pending ≥ 3 calendar days, and nudges Accounts for requests processing ≥ 1 calendar day; a second call on the same `now` sends nothing; a call on the next calendar day sends again.
- Editing a pending request emails the (possibly new) manager and clears `reminder_last_sent`.
- The SMTP password is read only from `FERVID_SMTP_PASSWORD`; no `app_settings` row ever holds a password; the `/notifications` page has no password input.
- The `/notifications` screen exposes a `require_attachments` toggle stored in the `app_settings` `require_attachments` key; enabling it persists `"1"` and makes attachment submission mandatory (Phase-2 `SubmitRequest` reads the key fresh via `AppSetting`), and disabling it persists `"0"` and relaxes enforcement — T10 (the runtime, `app_settings`-backed toggle Phase 2 deferred to this phase).
- A non-`notification`-permitted session gets 403 on `GET /notifications` and both POSTs by URL.
- `make test-race` and `make test-cover` green.

---

## 10. Test plan (TDD)

- Store unit: `internal/store/notifications_test.go` (app_settings + notification_settings round-trip; `UsersWithPermission`), `internal/store/reminders_test.go` (calendar-day candidate queries + `MarkReminderSent`/`ResetReminder` with fixed `now`), `internal/store/migrations_test.go` additions (v5 tables + idempotency).
- Notify unit: `internal/notify/mailer_test.go` (SMTP header/envelope/auth via injected `sendMail`; password-from-env; `fakeMailer`), `internal/notify/service_test.go` (recipient resolution incl. management-Cc rule and de-dup; disabled event = no send; template rendering with `money`), `internal/notify/reminders_test.go` (deterministic `RunReminders` with fixed clocks + `fakeMailer`; daily throttle; `Scheduler` runs on start and stops on cancel).
- App integration: `internal/app/app_integration_test.go` additions (`/notifications` renders matrix + password note; per-event save; RBAC 403 by URL; no password persisted; the `require_attachments` toggle persists to `app_settings` and drives Phase-2 submit enforcement — T10, `TestRequireAttachmentsTogglePersistsAndDrivesSubmitEnforcement`).

Every behaviour is red→green→commit per the plan.

---

## 11. Assumptions & risks

- **Migration number:** the canonical sequence is `v1`=Phase 1 … `v5`=Phase 5, so Phase 5 registers `Version: 5` only. `app_settings` is owned by Phase 2's `v2` migration; Phase 5's `v5` creates `notification_settings` only and never re-creates `app_settings`. If a phase registers multiple migrations, use the next free integer — the runner requires only that versions are contiguous and never reordered.
- **`request_edited` event (A8):** §6's canonical event list does not name an edit event; A8 nonetheless requires an email re-notify on edit. This spec **extends** §6 with `request_edited` (manager, opt-in). If the design review prefers reusing an existing event, remap the edit hook accordingly — the recipient/template machinery is unchanged.
- **Accounts = `Can(payment, process)`:** "include accounts" is resolved as active users whose roles grant `(payment, process)` (the Accounts starter role). If the org splits Accounts across roles, all such users are included — matching "expand include-flags to actual users by role".
- **Management list placement:** the management recipient list is Cc'd on the two approval events only; encoded as a small event allow-list, not a per-event column. Documented so audit can confirm N3/N6 without a hidden global Cc on every event.
- **`store.Request` shape:** assumes Phase 2 exposes `Project`/`Head` name fields and `RequesterID`/`ManagerID`, and Phase 3 exposes `ProcessingAt`. If names are IDs-only, templates fall back to IDs and the recipient resolver still works (it uses IDs → `UserByID`).
- **Delivery durability:** a send failure is logged and dropped (no retry queue) — acceptable for this internal tool; flagged in case durability is later required.
</content>
</invoke>
