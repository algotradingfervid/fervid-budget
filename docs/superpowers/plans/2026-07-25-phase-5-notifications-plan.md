# Phase 5 — Notifications Engine, Reminders & Email Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

## Amendment log

Amended 2026-07-25 against `docs/superpowers/specs/2026-07-25-design-system-adoption-spec.md` §4 "Phase 5". Backend task bodies written before the design system existed are preserved verbatim; everything below is the delta.

| Ref | Change |
|---|---|
| §3 phase map | Migration renumbered **v5 → v6**. The sequence is now v1 RBAC, v2 vendors, v3 requests, v4 payment-linking, v5 recoverable categories, **v6 notifications**. |
| **G19 — reversed decision** | The original plan's constraint "**N1 coordination:** … this phase does **not** add an in-app `notifications` table" is **withdrawn**. `mockups/screens/notifications.html` is an approved screen and nothing built it. Phase 5 now ships a `notifications` table, a per-user unread counter, mark-read routes, the shell bell badge (Phase 0 already exposes `Shell.Unread`), and the centre screen itself using `.notif-list` / `.notif` / `.notif.unread` / `.n-ico` / `.n-main` plus a `.segmented` filter strip (All · Unread · Mentions · Reminders). New Tasks 4 and 10. |
| **G20** | **12** seeded events, not 6: submitted · edited · returned · rejected · approved · urgent · on hold · cancellation requested · paid/settled · partial review · pending reminder · stale reservation. The two split urgent events (`request_submitted_urgent`, `request_approved_urgent`) collapse into one `request_urgent` row whose recipients are resolved from the request's status, matching the mockup's single "Urgent request raised" line. |
| **G20 (templates)** | The admin-editable placeholder vocabulary becomes `{{number}} {{amount}} {{payee}} {{approver}} {{project}} {{head}} {{needed_by}} {{link}}` (plus `{{requester}} {{purpose}} {{status}} {{approved_amount}} {{submitted_on}} {{processing_on}}`). `renderTemplate` stops executing `text/template` over admin input and becomes a strict token substitution that **errors on an unknown token** — a typo is caught at save time instead of silently rendering nothing, and admin-editable text can no longer reach the Go template engine. |
| **D7** | The admin rules screen moves from `GET /notifications` to `GET /admin/notifications`; `/notifications` is now the user's in-app centre. POST routes move with it: `/admin/notifications/smtp`, `/admin/notifications/events/{event}`, `/admin/notifications/test`. |
| Admin screen | Rebuilt on `mockups/screens/admin-notifications.html`: `table.t-cards` with six columns — Event · In-app · Email · Goes to · Fixed To / CC · Edit — where **In-app** is a static `.pill.good.no-dot` "On" (in-app always fires; only email is opt-in). All editing moves into an `.overlay > .sheet` template editor with `.sh-head` / `.sh-body.stack-12` / `.sh-foot`, `.checkline` include toggles and a `.hint` listing the available fields. A **Send a test email** action is added. |
| **T10 handover** | `require_attachments` **leaves this screen**. It belongs in the Configuration screen's Attachments fieldset, which Phase 2 owns (D6). Task 11 no longer renders the toggle, no longer writes the key, and `TestRequireAttachmentsTogglePersistsAndDrivesSubmitEnforcement` moves to Phase 2 with it. |
| Reminder thresholds | "3 calendar days pending" and "1 day stale" stop being hardcoded. They become `app_settings` keys `reminder_pending_days` (3), `reminder_repeat_days` (1) and `reminder_stale_days` (1), read through `Store.ReminderThresholds` and surfaced in the Configuration screen's **Reminders and ageing** fieldset — appended by Phase 5 (new Task 6) to the Phase-2-owned screen, exactly as Phase 4 appends its categories fieldset. |
| Task count | 9 → 12. Old Task 4→5, 5→7, 6→8, 7→9, 8→11, 9→12. Tasks 4, 6 and 10 are new. |

---

**Goal:** Add the notification layer: an in-app notification centre with an unread bell badge, SMTP-backed per-event emails over the same twelve events, a configurable admin rules screen, and a deterministic reminder scheduler driven by an injected clock and admin-set thresholds.

**Architecture:** A new `internal/notify` package holds a `Mailer` interface, an `SMTPMailer` (transport; password from env, host/port from DB `app_settings`), and a `Service` (`Notify` writes the in-app rows, resolves email recipients, renders templates and sends; `RunReminders(now)` + `Scheduler`). New store tables `notification_settings` and `notifications` (migration `v6`) with accessors; the `app_settings` key/value table is created by Phase 2 (`v3`) and **consumed** here (SMTP keys, management list, base URL, reminder thresholds) via the Phase-2 `AppSetting`/`SetAppSetting` accessors. Reminder queries take an injected `now` and an admin-configured `ReminderThresholds`. Handlers serve the user's centre at `/notifications` behind authentication only, and the admin rules screen at `/admin/notifications` behind `RequirePermission("notification", …)`. `cmd/server` starts the scheduler goroutine under the existing shutdown context.

**Tech Stack:** Go 1.25 stdlib `net/smtp` + `text/template`; SQLite via `modernc.org/sqlite`; `internal/money` for ₹ paise formatting; existing `internal/store` / `internal/auth` / `internal/app` patterns.

## Global Constraints

- Module `fervidbudget`; Go `1.25.0`. Currency is INR stored as integer paise (`int64`), formatted via `internal/money` (`money.FormatPaise`).
- **SMTP password comes only from env `FERVID_SMTP_PASSWORD`** — never stored in the DB, never in `app_settings`, never logged.
- **Clock injection:** reminder timing takes an injected `now time.Time` (or `now func() time.Time`); never call `time.Now()` inside a tested branch. Tests use fixed clocks.
- Store house style: `func (s *Store) X(ctx, actor User, …)`; mutations use `s.db.BeginTx(ctx,nil)` + `defer tx.Rollback()` + `tx.Commit()` and write `recordAuditTx(...)`; reuse `ErrNotFound/ErrValidation/…`; wrap DB errors via `classify`.
- The **admin rules** screen registers behind `a.auth.RequirePermission("notification", "view"|"edit", …)`; the **user's** notification centre requires only an authenticated session, because every row it shows was addressed to that user. All POSTs are wrapped in `a.withCSRF`; errors via `a.respondStoreError`/`respondError`.
- Email bodies are plain text assembled by strict `{{token}}` substitution (no HTML escaping of ₹, and no Go template execution over admin-editable strings).
- Migration versions are contiguous and never reordered; new tables use `CREATE TABLE IF NOT EXISTS`; re-opening a migrated DB is a no-op. Canonical sequence: `v1`=Phase 1, `v2`=Phase 1V, `v3`=Phase 2, `v4`=Phase 3, `v5`=Phase 4, `v6`=Phase 5 — so Phase 5 registers `Version: 6` and creates **`notification_settings` and `notifications`**.
- **`app_settings` is Phase 2's:** the `app_settings(key,value)` table and its `AppSetting`/`SetAppSetting` accessors ship in Phase 2's `v3` migration. Phase 5 does **not** create `app_settings` — it consumes it (SMTP host/port/username/from, `management_recipients`, `base_url`, and the three reminder-threshold keys are stored as `app_settings` keys via `SetAppSetting`). `require_attachments` is **not** Phase 5's: Phase 2 owns that key and renders it in the Configuration screen's Attachments fieldset.
- **N1, restated (G19):** in-app notification is a **real, persisted channel** — every event writes one `notifications` row per recipient and those rows always fire. Email is the additive opt-in layer on top: a disabled event stays in-app-only (kept honest by `TestNotifyDisabledEventStillWritesInAppButSendsNoEmail`). The Phase-2/3 work queues remain the task lists; the centre is the activity feed.
- **Design system (Phase 0, consumed):** both screens are assembled from the ported component layer. Phase 5 adds **no new CSS**. Classes used: `.page-banner`, `.segmented` (+ `.is-active`, `.n`), `.card`, `.notif-list`, `.notif`, `.notif.unread`, `.n-ico`, `.n-main`, `table.t-cards` with `td.t-lead` / `td.c` / `data-label`, `.pill.good.no-dot`, `.overlay`, `.sheet`, `.sh-head`, `.sh-sub`, `.sh-close`, `.sh-body.stack-12`, `.sh-foot`, `.field`, `.flabel`, `.checkline`, `.hint`, `.stack-8`, `.row-end`, `.btn.small.outline`, `.empty`. `.badge` is never used (D5).
- **`.t-cards` contract:** every `<td>` inside a `table.t-cards` carries a `data-label` so the sub-860 px card restack labels each value. Asserted by test.
- **Clock and thresholds:** reminder timing takes both an injected `now` and a `store.ReminderThresholds` value read from `app_settings`. Neither `time.Now()` nor a literal `3`/`1` appears inside a tested branch.
- Every task is red→green→commit. `make test-race` and `make test-cover` stay green.
- **Commits:** stage only the files the task touched, by explicit path — the tree contains unrelated modified files, so never `git add -A`.

### Consumed contracts (from Phases 0–4)

- **Phase 0:** `internal/app/nav.go` defines `Shell` with an `Unread int` field and `PageData.Shell`; the shell is populated once in `renderStatus`. Phase 5 fills `Shell.Unread` (Task 10) and adds one `navSpec` entry for the admin rules screen. The bell itself lives in the Phase-0 `.m-topbar` / sidebar markup and already renders `Shell.Unread` when it is non-zero.
- **Phase 2:** `app_settings(key,value)` with `AppSetting(ctx,key) (string,error)` returning `""` for an unset key and `SetAppSetting(ctx,actor,key,value) error`; the Configuration screen `GET/POST /configuration` behind `config`{view,edit}, one `<fieldset>` per section, to which Phase 5 appends the Reminders fieldset (Task 6); `store.Request` with `ID, Number, Status, Treatment, Type, Project, Head, Amount, ApprovedAmount *int64, Purpose, NeededBy, VendorPayee, RequesterID, ManagerID, Urgent, OnHold, SubmittedAt, ProcessingAt, ReminderLastSent`; `request_comments`. Phase 2 also owns `require_attachments` and the T10 test that proves it.
- **Phase 3:** `ProcessingBy *int64` on `store.Request` — the "assigned accountant" recipient for the cancellation-requested and stale-reservation events.
- **Phase 4:** migration `v5`; Phase 5's entry is appended after it.

---

### Task 1: Config — env-only SMTP password

**Files:**
- Modify: `internal/config/config.go` (add `SMTPPassword` field + loader line)
- Test: `internal/config/config_test.go` (add one test)

**Interfaces:**
- Consumes: existing `env(key, fallback string) string` helper.
- Produces: `Config.SMTPPassword string` (loaded from `FERVID_SMTP_PASSWORD`, default `""`). Consumed by `notify.NewSMTPMailer` (Task 5) and `cmd/server` (Task 9).

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:
```go
func TestSMTPPasswordLoadsFromEnvOnly(t *testing.T) {
	t.Setenv("FERVID_SMTP_PASSWORD", "")
	if got := Load().SMTPPassword; got != "" {
		t.Fatalf("default SMTPPassword = %q, want empty", got)
	}
	t.Setenv("FERVID_SMTP_PASSWORD", "env-secret-123")
	if got := Load().SMTPPassword; got != "env-secret-123" {
		t.Fatalf("SMTPPassword = %q, want env-secret-123", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestSMTPPasswordLoadsFromEnvOnly -v`
Expected: FAIL — build error `unknown field SMTPPassword in struct literal` / `defaults.SMTPPassword undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/config/config.go`, add the field to `Config`:
```go
	BackupKeepDays int
	SMTPPassword   string // FERVID_SMTP_PASSWORD — env only, never persisted
```
And in `Load()`, add:
```go
		BackupKeepDays: envInt("FERVID_BACKUP_KEEP_DAYS", 30),
		SMTPPassword:   env("FERVID_SMTP_PASSWORD", ""),
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestSMTPPasswordLoadsFromEnvOnly -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): load SMTP password from FERVID_SMTP_PASSWORD env"
```

---

### Task 2: Migration v6 — `notification_settings` (12 events) + `notifications` table

**Files:**
- Modify: `internal/store/migrations.go` (append one entry to the `migrations` slice — Phase 1 owns this file)
- Create: `internal/store/migrations_notifications.go` (`NotificationSetting` type + `upNotifications` + `defaultNotificationSettings`)
- Test: `internal/store/migrations_test.go` (add one test; Phase 1 owns this file)

**Interfaces:**
- Consumes: Phase 1 migration runner (`[]migration{Version int, Name string, Up func(*sql.Tx) error}`, `migrate`, `PRAGMA user_version`); `boolInt` helper. Phase 2's `v3` migration (already-present `app_settings` table — **not** re-created here).
- Produces: the `NotificationSetting` type (defined **here** — the first task that references it, via `defaultNotificationSettings`; Task 3's accessors reuse this same definition); the `notification_settings(event,…)` table seeded with the **twelve** events of G20; the `notifications(...)` table backing the in-app centre (G19); package var `defaultNotificationSettings []NotificationSetting` (reused as seed source). Phase 5 does **not** create `app_settings` (Phase 2's `v3` owns it).

**The twelve events, in the order `admin-notifications.html` lists them:**

| Event key | Screen label | Goes to |
|---|---|---|
| `request_submitted` | Request submitted | Approver |
| `request_edited` | Request edited before approval | Approver |
| `request_returned` | Returned for correction | Requester |
| `request_rejected` | Rejected | Requester |
| `request_approved` | Approved | Requester + Accounts group |
| `request_urgent` | Urgent request raised | Approver immediately, Accounts on approval |
| `request_on_hold` | Put on hold | Requester |
| `request_cancellation_requested` | Cancellation requested | Approver + assigned accountant |
| `payment_settled` | Payment recorded and settled | Requester + approver |
| `payment_partial_review` | Partial payment sent for review | Approver |
| `reminder_pending` | Pending reminder · after the configured wait, then on the configured cadence | Whoever it is waiting on |
| `reminder_stale_reservation` | Stale reservation · after the configured wait | Assigned accountant |

`request_urgent` replaces the earlier draft's `request_submitted_urgent` + `request_approved_urgent` pair: one admin row, two send points, recipients resolved from the request's status (Task 8).

- [ ] **Step 1: Write the failing test**

Add to `internal/store/migrations_test.go`:
```go
func TestMigrationV6CreatesNotificationTablesAndSeedsTwelveEvents(t *testing.T) { // G19, G20
	s := newTestStore(t)
	// v6 creates notification_settings and notifications; app_settings is Phase 2's (v3) and asserted there.
	for _, table := range []string{"notification_settings", "notifications"} {
		var name string
		if err := s.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("table %q missing: %v", table, err)
		}
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM notification_settings`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 12 {
		t.Fatalf("seeded events = %d, want 12", count)
	}
	want := []string{
		"request_submitted", "request_edited", "request_returned", "request_rejected",
		"request_approved", "request_urgent", "request_on_hold", "request_cancellation_requested",
		"payment_settled", "payment_partial_review", "reminder_pending", "reminder_stale_reservation",
	}
	for _, event := range want {
		var subject, body string
		if err := s.DB().QueryRow(`SELECT subject_template,body_template FROM notification_settings WHERE event=?`, event).Scan(&subject, &body); err != nil {
			t.Fatalf("event %q missing: %v", event, err)
		}
		if subject == "" || body == "" {
			t.Fatalf("event %q seeded with an empty template", event)
		}
		// G20: the admin-facing vocabulary is {{token}}, never Go template syntax.
		if strings.Contains(subject+body, "{{.") || strings.Contains(subject+body, "{{money") {
			t.Fatalf("event %q still uses Go template syntax: %q / %q", event, subject, body)
		}
	}
	// The unread-count index the shell bell depends on exists.
	var idx string
	if err := s.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_notifications_user_unread'`).Scan(&idx); err != nil {
		t.Fatalf("unread index missing: %v", err)
	}
	var version int
	if err := s.DB().QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 6 {
		t.Fatalf("user_version = %d, want >= 6", version)
	}
}
```
Add `"strings"` to the `internal/store/migrations_test.go` import block if Phase 1 did not already need it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestMigrationV6CreatesNotificationTablesAndSeedsTwelveEvents -v`
Expected: FAIL — `table "notification_settings" missing: sql: no rows in result set`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/store/migrations_notifications.go`:
```go
package store

import "database/sql"

// NotificationSetting is defined here (Task 2) because it is the first task that
// references the type — defaultNotificationSettings below needs it, and the seed
// runs inside upNotifications. Task 3's store accessors reuse this same definition,
// so the package compiles task-by-task with no duplicate declaration.
//
// Label is the human sentence the admin screen shows; Audience is the plain-English
// "Goes to" column. Both are seeded so the screen never hardcodes copy per event.
type NotificationSetting struct {
	Event            string
	Label            string
	Audience         string
	EmailEnabled     bool
	ToRecipients     string
	CcRecipients     string
	IncludeRequester bool
	IncludeManager   bool
	IncludeAccounts  bool
	SubjectTemplate  string
	BodyTemplate     string
}

// The twelve events of G20, in the order admin-notifications.html lists them.
// Templates use the {{token}} vocabulary; see renderTemplate in internal/notify.
var defaultNotificationSettings = []NotificationSetting{
	{Event: "request_submitted", Label: "Request submitted", Audience: "Approver", IncludeManager: true,
		SubjectTemplate: "{{number}} needs your approval — {{amount}} to {{payee}}",
		BodyTemplate:    "{{requester}} raised {{number}} for {{amount}} to {{payee}}.\n\nProject: {{project}} / {{head}}\nNeeded by: {{needed_by}}\n\nOpen it: {{link}}"},
	{Event: "request_edited", Label: "Request edited before approval", Audience: "Approver", IncludeManager: true,
		SubjectTemplate: "{{number}} was edited and re-sent for approval",
		BodyTemplate:    "{{requester}} edited {{number}} and sent it back for your approval. It is now {{amount}} to {{payee}}.\n\nOpen it: {{link}}"},
	{Event: "request_returned", Label: "Returned for correction", Audience: "Requester", IncludeRequester: true,
		SubjectTemplate: "{{number}} was returned for correction",
		BodyTemplate:    "{{approver}} returned {{number}} for correction.\n\nOpen it: {{link}}"},
	{Event: "request_rejected", Label: "Rejected", Audience: "Requester", IncludeRequester: true,
		SubjectTemplate: "{{number}} was rejected",
		BodyTemplate:    "{{approver}} rejected {{number}} for {{amount}}. This is final.\n\nOpen it: {{link}}"},
	{Event: "request_approved", Label: "Approved", Audience: "Requester + Accounts group", IncludeRequester: true, IncludeAccounts: true,
		SubjectTemplate: "{{number}} approved — {{approved_amount}} to {{payee}}",
		BodyTemplate:    "{{approver}} approved {{number}} for {{approved_amount}} to {{payee}}.\n\nProject: {{project}} / {{head}}\nNeeded by: {{needed_by}}\n\nOpen it: {{link}}"},
	{Event: "request_urgent", Label: "Urgent request raised", Audience: "Approver immediately, Accounts on approval",
		IncludeManager: true, IncludeAccounts: true,
		SubjectTemplate: "URGENT: {{number}} — {{amount}} to {{payee}}",
		BodyTemplate:    "{{number}} is marked urgent.\n\n{{purpose}}\nNeeded by: {{needed_by}}\n\nOpen it: {{link}}"},
	{Event: "request_on_hold", Label: "Put on hold", Audience: "Requester", IncludeRequester: true,
		SubjectTemplate: "{{number}} was put on hold",
		BodyTemplate:    "Accounts put {{number}} on hold and needs more information before paying.\n\nOpen it: {{link}}"},
	{Event: "request_cancellation_requested", Label: "Cancellation requested", Audience: "Approver + assigned accountant",
		IncludeManager: true, IncludeAccounts: true,
		SubjectTemplate: "{{requester}} asked to cancel {{number}}",
		BodyTemplate:    "{{requester}} asked to cancel {{number}} ({{approved_amount}} to {{payee}}). The payment is frozen until you accept or decline.\n\nOpen it: {{link}}"},
	{Event: "payment_settled", Label: "Payment recorded and settled", Audience: "Requester + approver",
		IncludeRequester: true, IncludeManager: true,
		SubjectTemplate: "{{number}} has been paid — {{amount}} to {{payee}}",
		BodyTemplate:    "{{number}} was paid in full: {{amount}} to {{payee}}. The request is now complete.\n\nOpen it: {{link}}"},
	{Event: "payment_partial_review", Label: "Partial payment sent for review", Audience: "Approver", IncludeManager: true,
		SubjectTemplate: "{{number}} was partly paid — your review is needed",
		BodyTemplate:    "{{number}} was approved for {{approved_amount}} but only {{amount}} was paid to {{payee}}. Accept the difference or raise a concern.\n\nOpen it: {{link}}"},
	{Event: "reminder_pending", Label: "Pending reminder", Audience: "Whoever it is waiting on", IncludeManager: true,
		SubjectTemplate: "Reminder: {{number}} is still waiting on you",
		BodyTemplate:    "{{number}} for {{amount}} has been pending since {{submitted_on}}.\n\nOpen it: {{link}}"},
	{Event: "reminder_stale_reservation", Label: "Stale reservation", Audience: "Assigned accountant", IncludeAccounts: true,
		SubjectTemplate: "Reminder: {{number}} is still reserved and unpaid",
		BodyTemplate:    "{{number}} has been in processing since {{processing_on}} with no settlement recorded.\n\nOpen it: {{link}}"},
}

// upNotifications creates notification_settings and the in-app notifications table.
// app_settings (key/value) is created by Phase 2's v3 migration and is NOT
// (re)created here.
func upNotifications(tx *sql.Tx) error {
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS notification_settings (
  event             TEXT PRIMARY KEY,
  label             TEXT NOT NULL DEFAULT '',
  audience          TEXT NOT NULL DEFAULT '',
  email_enabled     INTEGER NOT NULL DEFAULT 0,
  to_recipients     TEXT NOT NULL DEFAULT '',
  cc_recipients     TEXT NOT NULL DEFAULT '',
  include_requester INTEGER NOT NULL DEFAULT 0,
  include_manager   INTEGER NOT NULL DEFAULT 0,
  include_accounts  INTEGER NOT NULL DEFAULT 0,
  subject_template  TEXT NOT NULL DEFAULT '',
  body_template     TEXT NOT NULL DEFAULT '',
  sort_order        INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS notifications (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id),
  event      TEXT NOT NULL,
  kind       TEXT NOT NULL DEFAULT 'activity',
  request_id INTEGER,
  title      TEXT NOT NULL,
  body       TEXT NOT NULL DEFAULT '',
  href       TEXT NOT NULL DEFAULT '',
  read_at    DATETIME,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_notifications_user_unread ON notifications(user_id, read_at, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_request ON notifications(request_id);`); err != nil {
		return err
	}
	for i, n := range defaultNotificationSettings {
		if _, err := tx.Exec(`INSERT INTO notification_settings
			(event,label,audience,email_enabled,to_recipients,cc_recipients,include_requester,include_manager,include_accounts,subject_template,body_template,sort_order)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(event) DO NOTHING`,
			n.Event, n.Label, n.Audience, boolInt(n.EmailEnabled), n.ToRecipients, n.CcRecipients,
			boolInt(n.IncludeRequester), boolInt(n.IncludeManager), boolInt(n.IncludeAccounts),
			n.SubjectTemplate, n.BodyTemplate, i+1); err != nil {
			return err
		}
	}
	return nil
}
```
In `internal/store/migrations.go`, append to the ordered `migrations` slice (after the Phase-4 `v5` entry):
```go
	{Version: 6, Name: "notifications", Up: upNotifications},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestMigrationV6CreatesNotificationTablesAndSeedsTwelveEvents -v`
Expected: PASS. (`NotificationSetting` is declared in this task's `migrations_notifications.go`, so the package compiles standalone — no forward dependency on Task 3.)

- [ ] **Step 5: Commit**

```bash
git add internal/store/migrations.go internal/store/migrations_notifications.go internal/store/migrations_test.go
git commit -m "feat(store): add migration v6 with twelve notification events and the in-app table"
```

---

### Task 3: Store accessors — app_settings, notification_settings, UsersWithPermission

**Files:**
- Create: `internal/store/notifications.go` (types + accessors)
- Test: `internal/store/notifications_test.go`

**Interfaces:**
- Consumes: `recordAuditTx`, `classify`, `boolInt`, `scanUser`, `ErrNotFound`, `ErrValidation`; Phase-1 tables `roles`, `role_permissions`, `user_roles`; **Phase-2 key/value accessors `AppSetting(ctx,key)`/`SetAppSetting(ctx,actor,key,value)`** (the `app_settings` table and these accessors ship in Phase 2's `v3`); the `NotificationSetting` type (defined in Task 2).
- Produces:
  - `type AppSettings struct { SMTPHost string; SMTPPort int; SMTPUsername, SMTPFromName, SMTPFromAddr, ManagementRecipients, BaseURL string }`
  - `func (s *Store) GetAppSettings(ctx) (AppSettings, error)` — typed wrapper reading `smtp_*` + `management_recipients` + `base_url` via `AppSetting`
  - `func (s *Store) SetAppSettings(ctx, actor User, in AppSettings) error` — typed wrapper writing those keys via `SetAppSetting`

`BaseURL` is new: `{{link}}` in an email must be an absolute URL, and a relative path is useless in a mail client. It is admin-editable on the rules screen and defaults to `""`, in which case `{{link}}` renders the bare path.
  - `func (s *Store) NotificationSetting(ctx, event string) (NotificationSetting, error)`
  - `func (s *Store) AllNotificationSettings(ctx) ([]NotificationSetting, error)`
  - `func (s *Store) SetNotificationSetting(ctx, actor User, in NotificationSetting) error`
  - `func (s *Store) UsersWithPermission(ctx, resource, action string) ([]User, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/store/notifications_test.go`:
```go
package store

import (
	"context"
	"strings"
	"testing"
)

func TestAppSettingsRoundTripAndNeverStoresPassword(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)
	in := AppSettings{SMTPHost: "smtp.test", SMTPPort: 2525, SMTPUsername: "u",
		SMTPFromName: "Fervid", SMTPFromAddr: "no@reply.test", ManagementRecipients: "boss@test",
		BaseURL: "https://budget.fervid.test"}
	if err := s.SetAppSettings(ctx, actor, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAppSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Fatalf("GetAppSettings = %#v, want %#v", got, in)
	}
	rows, err := s.DB().QueryContext(ctx, `SELECT key FROM app_settings`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(k), "password") {
			t.Fatalf("app_settings must never hold a password key, found %q", k)
		}
	}
}

func TestNotificationSettingUpsertAndFetch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)
	in := NotificationSetting{Event: "request_approved", EmailEnabled: true, ToRecipients: "ops@test",
		IncludeRequester: true, IncludeAccounts: true, SubjectTemplate: "S {{number}}", BodyTemplate: "B {{amount}}"}
	if err := s.SetNotificationSetting(ctx, actor, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.NotificationSetting(ctx, "request_approved")
	if err != nil {
		t.Fatal(err)
	}
	// The editable columns round-trip exactly…
	if got.EmailEnabled != in.EmailEnabled || got.ToRecipients != in.ToRecipients || got.CcRecipients != in.CcRecipients ||
		got.IncludeRequester != in.IncludeRequester || got.IncludeManager != in.IncludeManager || got.IncludeAccounts != in.IncludeAccounts ||
		got.SubjectTemplate != in.SubjectTemplate || got.BodyTemplate != in.BodyTemplate {
		t.Fatalf("NotificationSetting editable fields = %#v, want %#v", got, in)
	}
	// …and the seeded presentation columns survive an edit that never sends them.
	if got.Label != "Approved" || got.Audience == "" {
		t.Fatalf("seeded label/audience lost on upsert: %q / %q", got.Label, got.Audience)
	}
	all, err := s.AllNotificationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 12 {
		t.Fatalf("events = %d, want 12 (seeded)", len(all))
	}
	if all[0].Event != "request_submitted" || all[len(all)-1].Event != "reminder_stale_reservation" {
		t.Fatalf("AllNotificationSettings must return screen order, got %q … %q", all[0].Event, all[len(all)-1].Event)
	}
}

func TestUsersWithPermissionResolvesAccounts(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id1, err := s.CreateUser(ctx, "acct1@test", "Acct One", "h", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.CreateUser(ctx, "acct2@test", "Acct Two", "h", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "req@test", "Requester", "h", "data_entry", true); err != nil {
		t.Fatal(err)
	}
	res, err := s.DB().ExecContext(ctx, `INSERT INTO roles(name,is_system) VALUES('Accounts',1)`)
	if err != nil {
		t.Fatal(err)
	}
	roleID, _ := res.LastInsertId()
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO role_permissions(role_id,resource,action) VALUES(?,?,?)`, roleID, "payment", "process"); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []int64{id1, id2} {
		if _, err := s.DB().ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, uid, roleID); err != nil {
			t.Fatal(err)
		}
	}
	users, err := s.UsersWithPermission(ctx, "payment", "process")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Fatalf("accounts users = %d, want 2", len(users))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestAppSettingsRoundTripAndNeverStoresPassword|TestNotificationSettingUpsertAndFetch|TestUsersWithPermissionResolvesAccounts' -v`
Expected: FAIL — `undefined: AppSettings` (and `SetAppSettings`, `UsersWithPermission`).

- [ ] **Step 3: Write minimal implementation**

Create `internal/store/notifications.go`:
```go
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

type AppSettings struct {
	SMTPHost             string
	SMTPPort             int
	SMTPUsername         string
	SMTPFromName         string
	SMTPFromAddr         string
	ManagementRecipients string
	BaseURL              string // absolute origin for {{link}}; "" renders the bare path
}

// NotificationSetting is declared in migrations_notifications.go (Task 2), the first
// file that references it. Do NOT redeclare it here.

// GetAppSettings / SetAppSettings are typed convenience wrappers over Phase 2's
// key/value accessors AppSetting(ctx,key) / SetAppSetting(ctx,actor,key,value).
// Phase 5 never creates app_settings and never touches it with raw SQL.
// (Assumes AppSetting returns "" (nil error) for an unset key — the key/value default.)
func (s *Store) GetAppSettings(ctx context.Context) (AppSettings, error) {
	keys := []string{"smtp_host", "smtp_port", "smtp_username", "smtp_from_name", "smtp_from_addr", "management_recipients", "base_url"}
	m := map[string]string{}
	for _, k := range keys {
		v, err := s.AppSetting(ctx, k)
		if err != nil {
			return AppSettings{}, err
		}
		m[k] = v
	}
	port, _ := strconv.Atoi(m["smtp_port"])
	return AppSettings{
		SMTPHost:             m["smtp_host"],
		SMTPPort:             port,
		SMTPUsername:         m["smtp_username"],
		SMTPFromName:         m["smtp_from_name"],
		SMTPFromAddr:         m["smtp_from_addr"],
		ManagementRecipients: m["management_recipients"],
		BaseURL:              strings.TrimRight(m["base_url"], "/"),
	}, nil
}

func (s *Store) SetAppSettings(ctx context.Context, actor User, in AppSettings) error {
	pairs := [][2]string{
		{"smtp_host", strings.TrimSpace(in.SMTPHost)},
		{"smtp_port", strconv.Itoa(in.SMTPPort)},
		{"smtp_username", strings.TrimSpace(in.SMTPUsername)},
		{"smtp_from_name", strings.TrimSpace(in.SMTPFromName)},
		{"smtp_from_addr", strings.TrimSpace(in.SMTPFromAddr)},
		{"management_recipients", strings.TrimSpace(in.ManagementRecipients)},
		{"base_url", strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")},
	}
	for _, p := range pairs {
		if err := s.SetAppSetting(ctx, actor, p[0], p[1]); err != nil {
			return err
		}
	}
	return nil
}

const notificationSettingCols = `event,label,audience,email_enabled,to_recipients,cc_recipients,include_requester,include_manager,include_accounts,subject_template,body_template`

func scanNotificationSetting(sc interface{ Scan(...any) error }) (NotificationSetting, error) {
	var n NotificationSetting
	var en, ir, im, ia int
	err := sc.Scan(&n.Event, &n.Label, &n.Audience, &en, &n.ToRecipients, &n.CcRecipients, &ir, &im, &ia, &n.SubjectTemplate, &n.BodyTemplate)
	if err == sql.ErrNoRows {
		return n, ErrNotFound
	}
	n.EmailEnabled, n.IncludeRequester, n.IncludeManager, n.IncludeAccounts = en == 1, ir == 1, im == 1, ia == 1
	return n, err
}

func (s *Store) NotificationSetting(ctx context.Context, event string) (NotificationSetting, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+notificationSettingCols+` FROM notification_settings WHERE event=?`, event)
	return scanNotificationSetting(row)
}

// AllNotificationSettings returns the events in the order the admin screen shows
// them (the seeded sort_order), not alphabetically.
func (s *Store) AllNotificationSettings(ctx context.Context) ([]NotificationSetting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+notificationSettingCols+` FROM notification_settings ORDER BY sort_order,event`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotificationSetting
	for rows.Next() {
		n, err := scanNotificationSetting(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) SetNotificationSetting(ctx context.Context, actor User, in NotificationSetting) error {
	if strings.TrimSpace(in.Event) == "" {
		return fmt.Errorf("%w: notification event is required", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Only the editable columns are written; label, audience and sort_order are
	// seeded presentation data the admin screen never posts back.
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_settings
		(event,label,audience,email_enabled,to_recipients,cc_recipients,include_requester,include_manager,include_accounts,subject_template,body_template)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(event) DO UPDATE SET email_enabled=excluded.email_enabled, to_recipients=excluded.to_recipients,
			cc_recipients=excluded.cc_recipients, include_requester=excluded.include_requester,
			include_manager=excluded.include_manager, include_accounts=excluded.include_accounts,
			subject_template=excluded.subject_template, body_template=excluded.body_template`,
		in.Event, in.Label, in.Audience, boolInt(in.EmailEnabled), in.ToRecipients, in.CcRecipients,
		boolInt(in.IncludeRequester), boolInt(in.IncludeManager), boolInt(in.IncludeAccounts),
		in.SubjectTemplate, in.BodyTemplate); err != nil {
		return classify(err)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "update", EntityType: "notification_settings", Summary: "Updated notification event " + in.Event, After: in}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UsersWithPermission(ctx context.Context, resource, action string) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT u.id,u.email,u.name,u.password_hash,u.role,u.active,u.created_at,u.updated_at
		FROM users u
		JOIN user_roles ur ON ur.user_id=u.id
		JOIN role_permissions rp ON rp.role_id=ur.role_id
		WHERE rp.resource=? AND rp.action=? AND u.active=1
		ORDER BY u.name`, resource, action)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
```
The `NotificationSetting` type stays defined once, in Task 2's `migrations_notifications.go`; this file reuses it. There is no duplicate declaration to remove — the package compiles task-by-task.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestAppSettingsRoundTripAndNeverStoresPassword|TestNotificationSettingUpsertAndFetch|TestUsersWithPermissionResolvesAccounts' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/notifications.go internal/store/notifications_test.go
git commit -m "feat(store): add app_settings, notification_settings, and UsersWithPermission accessors"
```

---

### Task 4: In-app notification store — write, list, unread count, mark read (G19)

The in-app centre is a real persisted channel, not a view over the work queues. This task is the whole storage side of it; Task 8 writes the rows, Task 10 renders them.

**Files:**
- Create: `internal/store/inapp.go` (`Notification` type + accessors)
- Test: `internal/store/inapp_test.go`

**Interfaces:**
- Consumes: the `notifications` table (Task 2); `classify`, `ErrValidation`, `ErrNotFound`.
- Produces:
  - `type Notification struct { ID, UserID int64; Event, Kind, Title, Body, Href string; RequestID *int64; ReadAt *time.Time; CreatedAt time.Time }`
  - `type NotificationFilter struct { UserID int64; Scope string; Limit int }` — `Scope` is `""`/`"all"` · `"unread"` · `"mentions"` · `"reminders"`
  - `type NotificationCounts struct { All, Unread, Mentions, Reminders int }`
  - `func (s *Store) AddNotification(ctx context.Context, in Notification) (int64, error)`
  - `func (s *Store) ListNotifications(ctx context.Context, f NotificationFilter) ([]Notification, error)`
  - `func (s *Store) NotificationCounts(ctx context.Context, userID int64) (NotificationCounts, error)`
  - `func (s *Store) UnreadNotificationCount(ctx context.Context, userID int64) (int, error)`
  - `func (s *Store) MarkNotificationRead(ctx context.Context, userID, id int64, now time.Time) error`
  - `func (s *Store) MarkAllNotificationsRead(ctx context.Context, userID int64, now time.Time) error`

**Two rules this task fixes:**

1. **Ownership is enforced in the query, not the handler.** Every read and every mutation is scoped by `user_id=?`. `MarkNotificationRead` for another user's row affects zero rows and returns `ErrNotFound` — a user can neither read nor dismiss someone else's notification by guessing an id.
2. **`kind` is derived once, at write time**, from the event: `reminder` for the two reminder events, `mention` for the three events where a person wrote something addressed to you (`request_returned`, `request_on_hold`, `request_rejected`), `activity` otherwise. The `.segmented` strip filters on it.

- [ ] **Step 1: Write the failing test**

Create `internal/store/inapp_test.go`:
```go
package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNotificationLifecycleAndScopedFilters(t *testing.T) { // G19
	ctx := context.Background()
	s := newTestStore(t)
	me, err := s.CreateUser(ctx, "me@test", "Me", "h", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateUser(ctx, "other@test", "Other", "h", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	reqID := int64(7)
	seed := []Notification{
		{UserID: me, Event: "request_returned", Kind: "mention", RequestID: &reqID, Title: "Kavita Rao returned PR-2026-000131", Body: "Attach the July invoice", Href: "/requests/7"},
		{UserID: me, Event: "reminder_pending", Kind: "reminder", RequestID: &reqID, Title: "PR-2026-000134 has waited 3 days", Href: "/requests/7"},
		{UserID: me, Event: "request_approved", Kind: "activity", RequestID: &reqID, Title: "Kavita Rao approved PR-2026-000128", Href: "/requests/7"},
		{UserID: other, Event: "request_approved", Kind: "activity", Title: "Not yours", Href: "/requests/9"},
	}
	var ids []int64
	for _, n := range seed {
		id, err := s.AddNotification(ctx, n)
		if err != nil {
			t.Fatalf("AddNotification(%s): %v", n.Event, err)
		}
		ids = append(ids, id)
	}

	all, err := s.ListNotifications(ctx, NotificationFilter{UserID: me})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("my notifications = %d, want 3 (never another user's)", len(all))
	}
	if all[0].Title != "Kavita Rao approved PR-2026-000128" {
		t.Fatalf("newest first expected, got %q", all[0].Title)
	}

	counts, err := s.NotificationCounts(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	if counts.All != 3 || counts.Unread != 3 || counts.Mentions != 1 || counts.Reminders != 1 {
		t.Fatalf("counts = %+v, want All 3 / Unread 3 / Mentions 1 / Reminders 1", counts)
	}

	for _, scope := range []struct {
		name string
		want int
	}{{"unread", 3}, {"mentions", 1}, {"reminders", 1}, {"all", 3}} {
		rows, err := s.ListNotifications(ctx, NotificationFilter{UserID: me, Scope: scope.name})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != scope.want {
			t.Fatalf("scope %q = %d rows, want %d", scope.name, len(rows), scope.want)
		}
	}

	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	if err := s.MarkNotificationRead(ctx, me, ids[0], now); err != nil {
		t.Fatal(err)
	}
	if unread, err := s.UnreadNotificationCount(ctx, me); err != nil || unread != 2 {
		t.Fatalf("unread after one read = %d (err %v), want 2", unread, err)
	}
	// A user may not mark another user's notification read.
	if err := s.MarkNotificationRead(ctx, me, ids[3], now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user mark-read = %v, want ErrNotFound", err)
	}
	if unread, _ := s.UnreadNotificationCount(ctx, other); unread != 1 {
		t.Fatal("the other user's notification must still be unread")
	}

	if err := s.MarkAllNotificationsRead(ctx, me, now); err != nil {
		t.Fatal(err)
	}
	if unread, err := s.UnreadNotificationCount(ctx, me); err != nil || unread != 0 {
		t.Fatalf("unread after mark-all = %d (err %v), want 0", unread, err)
	}
	if unread, _ := s.UnreadNotificationCount(ctx, other); unread != 1 {
		t.Fatal("mark-all must not touch another user's rows")
	}
}

func TestAddNotificationRequiresUserAndTitle(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.AddNotification(ctx, Notification{Event: "request_approved", Title: "x"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing user = %v, want ErrValidation", err)
	}
	uid, err := s.CreateUser(ctx, "u@test", "U", "h", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNotification(ctx, Notification{UserID: uid, Event: "request_approved", Title: "  "}); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank title = %v, want ErrValidation", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestNotificationLifecycleAndScopedFilters|TestAddNotificationRequiresUserAndTitle' -v`
Expected: FAIL (build) — `undefined: Notification`, `s.AddNotification undefined`, `undefined: NotificationFilter`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/store/inapp.go`:
```go
package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Notification is one row of a user's in-app centre. Kind drives the .segmented
// filter strip: "activity" | "mention" | "reminder".
type Notification struct {
	ID        int64
	UserID    int64
	Event     string
	Kind      string
	RequestID *int64
	Title     string
	Body      string
	Href      string
	ReadAt    *time.Time
	CreatedAt time.Time
}

type NotificationFilter struct {
	UserID int64
	Scope  string // "" | "all" | "unread" | "mentions" | "reminders"
	Limit  int    // 0 → 100
}

type NotificationCounts struct {
	All       int
	Unread    int
	Mentions  int
	Reminders int
}

const notificationCols = `id,user_id,event,kind,request_id,title,body,href,read_at,created_at`

func (s *Store) AddNotification(ctx context.Context, in Notification) (int64, error) {
	if in.UserID == 0 {
		return 0, fmt.Errorf("%w: a notification needs a recipient", ErrValidation)
	}
	if strings.TrimSpace(in.Title) == "" {
		return 0, fmt.Errorf("%w: a notification needs a title", ErrValidation)
	}
	kind := in.Kind
	if kind == "" {
		kind = "activity"
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO notifications(user_id,event,kind,request_id,title,body,href) VALUES(?,?,?,?,?,?,?)`,
		in.UserID, in.Event, kind, in.RequestID, strings.TrimSpace(in.Title), in.Body, in.Href)
	if err != nil {
		return 0, classify(err)
	}
	return res.LastInsertId()
}

// scopeClause is the single place a filter name becomes SQL, so a handler can
// never smuggle a scope string into the query.
func scopeClause(scope string) string {
	switch scope {
	case "unread":
		return ` AND read_at IS NULL`
	case "mentions":
		return ` AND kind='mention'`
	case "reminders":
		return ` AND kind='reminder'`
	default:
		return ``
	}
}

func (s *Store) ListNotifications(ctx context.Context, f NotificationFilter) ([]Notification, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+notificationCols+` FROM notifications
		WHERE user_id=?`+scopeClause(f.Scope)+` ORDER BY created_at DESC, id DESC LIMIT ?`, f.UserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Event, &n.Kind, &n.RequestID, &n.Title, &n.Body, &n.Href, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) NotificationCounts(ctx context.Context, userID int64) (NotificationCounts, error) {
	var c NotificationCounts
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN read_at IS NULL THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN kind='mention' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN kind='reminder' THEN 1 ELSE 0 END),0)
		FROM notifications WHERE user_id=?`, userID).
		Scan(&c.All, &c.Unread, &c.Mentions, &c.Reminders)
	return c, err
}

// UnreadNotificationCount is the shell bell badge. It is one indexed COUNT served
// by idx_notifications_user_unread and runs once per rendered page.
func (s *Store) UnreadNotificationCount(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id=? AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

func (s *Store) MarkNotificationRead(ctx context.Context, userID, id int64, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE notifications SET read_at=? WHERE id=? AND user_id=? AND read_at IS NULL`, now.UTC(), id, userID)
	if err != nil {
		return classify(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Either it does not exist, it is already read, or it belongs to someone
		// else. All three are indistinguishable to the caller on purpose.
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE id=? AND user_id=?`, id, userID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func (s *Store) MarkAllNotificationsRead(ctx context.Context, userID int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notifications SET read_at=? WHERE user_id=? AND read_at IS NULL`, now.UTC(), userID)
	return classify(err)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestNotificationLifecycleAndScopedFilters|TestAddNotificationRequiresUserAndTitle' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/inapp.go internal/store/inapp_test.go
git commit -m "feat(store): add in-app notifications with scoped filters and unread counts"
```

---

### Task 5: Reminder store queries + bookkeeping + calendar-day helper (admin thresholds)

**Files:**
- Create: `internal/store/reminders.go`
- Modify: `internal/store/models.go` (ensure `Request` has `ReminderLastSent *time.Time`; add if Phase 2 did not)
- Test: `internal/store/reminders_test.go`

**Interfaces:**
- Consumes: `store.Request` (Phase 2/3) with `ID int64`, `Number string`, `Status string`, `Treatment string`, `Type string`, `Amount int64`, `ApprovedAmount *int64`, `Purpose string`, `VendorPayee string`, `RequesterID int64`, `ManagerID int64`, `SubmittedAt *time.Time`, `ProcessingAt *time.Time`, `Urgent bool`, and (owned here) `ReminderLastSent *time.Time`; Phase-2 `AppSetting(ctx,key)`.
- Produces:
  - `func calendarDaysBetween(a, b time.Time) int`
  - `type ReminderThresholds struct { PendingAfterDays, RepeatEveryDays, StaleAfterDays int }`
  - `func (s *Store) ReminderThresholds(ctx context.Context) (ReminderThresholds, error)` — reads `reminder_pending_days` (3), `reminder_repeat_days` (1), `reminder_stale_days` (1) from `app_settings`, falling back to those defaults when a key is unset, blank or not a positive integer
  - `func (s *Store) RequestsPendingReminder(ctx, now time.Time, th ReminderThresholds) ([]Request, error)`
  - `func (s *Store) RequestsStaleProcessing(ctx, now time.Time, th ReminderThresholds) ([]Request, error)`
  - `func (s *Store) MarkReminderSent(ctx, requestID int64, now time.Time) error`
  - `func (s *Store) ResetReminder(ctx, requestID int64) error`

The hardcoded `3` and `1` are gone: the thresholds are admin-set data (Configuration → Reminders and ageing, Task 6) and are passed in explicitly, so a test can pin them without touching the database.

- [ ] **Step 1: Write the failing test**

Create `internal/store/reminders_test.go`:
```go
package store

import (
	"context"
	"testing"
	"time"
)

func seedReminderRequest(t *testing.T, s *Store, ctx context.Context, status string) int64 {
	t.Helper()
	reqUser, err := s.CreateUser(ctx, "req@test", "Req", "h", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := s.CreateUser(ctx, "mgr@test", "Mgr", "h", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO payment_requests(number,status,treatment,type,amount,purpose,requester_id,manager_id,submitted_at)
		VALUES(?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		"PR-2026-000001", status, "budget", "vendor_invoice", int64(500000), "Rent", reqUser, mgr)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCalendarDaysBetweenCountsDateBoundaries(t *testing.T) {
	a := time.Date(2026, 7, 20, 23, 0, 0, 0, time.UTC)
	b := time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC)
	if got := calendarDaysBetween(a, b); got != 3 {
		t.Fatalf("calendarDaysBetween = %d, want 3", got)
	}
}

func TestReminderThresholdsDefaultsAndOverrides(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)
	th, err := s.ReminderThresholds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if th.PendingAfterDays != 3 || th.RepeatEveryDays != 1 || th.StaleAfterDays != 1 {
		t.Fatalf("defaults = %+v, want 3/1/1", th)
	}
	for k, v := range map[string]string{"reminder_pending_days": "5", "reminder_repeat_days": "2", "reminder_stale_days": "4"} {
		if err := s.SetAppSetting(ctx, actor, k, v); err != nil {
			t.Fatal(err)
		}
	}
	th, err = s.ReminderThresholds(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if th.PendingAfterDays != 5 || th.RepeatEveryDays != 2 || th.StaleAfterDays != 4 {
		t.Fatalf("configured = %+v, want 5/2/4", th)
	}
	// Garbage never disables reminders; it falls back to the default.
	if err := s.SetAppSetting(ctx, actor, "reminder_pending_days", "not a number"); err != nil {
		t.Fatal(err)
	}
	if th, _ := s.ReminderThresholds(ctx); th.PendingAfterDays != 3 {
		t.Fatalf("invalid value = %d, want the 3-day default", th.PendingAfterDays)
	}
}

func TestRequestsPendingReminderHonoursConfiguredThreshold(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id := seedReminderRequest(t, s, ctx, "pending")
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	th := ReminderThresholds{PendingAfterDays: 3, RepeatEveryDays: 1, StaleAfterDays: 1}
	if _, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET submitted_at=? WHERE id=?`, now.AddDate(0, 0, -4), id); err != nil {
		t.Fatal(err)
	}
	due, err := s.RequestsPendingReminder(ctx, now, th)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("pending reminders = %d, want 1", len(due))
	}
	// A longer configured wait suppresses the same request.
	if due, _ := s.RequestsPendingReminder(ctx, now, ReminderThresholds{PendingAfterDays: 7, RepeatEveryDays: 1, StaleAfterDays: 1}); len(due) != 0 {
		t.Fatalf("7-day threshold reminders = %d, want 0 (submitted 4 days ago)", len(due))
	}
	if err := s.MarkReminderSent(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	if due, _ := s.RequestsPendingReminder(ctx, now, th); len(due) != 0 {
		t.Fatalf("same-day reminders = %d, want 0 (throttled)", len(due))
	}
	if due, _ := s.RequestsPendingReminder(ctx, now.AddDate(0, 0, 1), th); len(due) != 1 {
		t.Fatalf("next-day reminders = %d, want 1", len(due))
	}
	// RepeatEveryDays=3 stretches the throttle: one day later is still too soon.
	slow := ReminderThresholds{PendingAfterDays: 3, RepeatEveryDays: 3, StaleAfterDays: 1}
	if due, _ := s.RequestsPendingReminder(ctx, now.AddDate(0, 0, 1), slow); len(due) != 0 {
		t.Fatalf("3-day cadence reminders after 1 day = %d, want 0", len(due))
	}
	if due, _ := s.RequestsPendingReminder(ctx, now.AddDate(0, 0, 3), slow); len(due) != 1 {
		t.Fatalf("3-day cadence reminders after 3 days = %d, want 1", len(due))
	}
}

func TestRequestsStaleProcessingHonoursConfiguredThreshold(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id := seedReminderRequest(t, s, ctx, "processing")
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	th := ReminderThresholds{PendingAfterDays: 3, RepeatEveryDays: 1, StaleAfterDays: 1}
	if _, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET processing_at=? WHERE id=?`, now.AddDate(0, 0, -2), id); err != nil {
		t.Fatal(err)
	}
	due, err := s.RequestsStaleProcessing(ctx, now, th)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Fatalf("stale processing = %d, want 1", len(due))
	}
	if due, _ := s.RequestsStaleProcessing(ctx, now, ReminderThresholds{PendingAfterDays: 3, RepeatEveryDays: 1, StaleAfterDays: 5}); len(due) != 0 {
		t.Fatalf("5-day stale threshold = %d, want 0 (reserved 2 days ago)", len(due))
	}
}

func TestResetReminderClearsThrottle(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id := seedReminderRequest(t, s, ctx, "pending")
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	th := ReminderThresholds{PendingAfterDays: 3, RepeatEveryDays: 1, StaleAfterDays: 1}
	if _, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET submitted_at=? WHERE id=?`, now.AddDate(0, 0, -4), id); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkReminderSent(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetReminder(ctx, id); err != nil {
		t.Fatal(err)
	}
	if due, _ := s.RequestsPendingReminder(ctx, now, th); len(due) != 1 {
		t.Fatalf("after reset, reminders = %d, want 1", len(due))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestCalendarDaysBetweenCountsDateBoundaries|TestReminderThresholdsDefaultsAndOverrides|TestRequestsPendingReminderHonoursConfiguredThreshold|TestRequestsStaleProcessingHonoursConfiguredThreshold|TestResetReminderClearsThrottle' -v`
Expected: FAIL — `undefined: calendarDaysBetween` / `s.RequestsPendingReminder undefined` / `undefined: ReminderThresholds`.

- [ ] **Step 3: Write minimal implementation**

If `internal/store/models.go`'s `Request` struct lacks `ReminderLastSent`, add it (leave existing fields intact):
```go
	ReminderLastSent *time.Time
```
Create `internal/store/reminders.go`:
```go
package store

import (
	"context"
	"strconv"
	"time"
)

// calendarDaysBetween returns whole calendar-date boundaries from a to b (b>=a),
// timezone-normalised to UTC so reminder timing is deterministic.
func calendarDaysBetween(a, b time.Time) int {
	da := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	db := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	return int(db.Sub(da).Hours() / 24)
}

// ReminderThresholds is the admin-configured reminder policy, stored in
// app_settings and edited in the Configuration screen's "Reminders and ageing"
// fieldset. Values are always positive; an unset, blank or invalid key falls back
// to the default rather than disabling reminders.
type ReminderThresholds struct {
	PendingAfterDays int // reminder_pending_days, default 3
	RepeatEveryDays  int // reminder_repeat_days, default 1
	StaleAfterDays   int // reminder_stale_days, default 1
}

func (s *Store) ReminderThresholds(ctx context.Context) (ReminderThresholds, error) {
	read := func(key string, fallback int) (int, error) {
		raw, err := s.AppSetting(ctx, key)
		if err != nil {
			return 0, err
		}
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n <= 0 {
			return fallback, nil
		}
		return n, nil
	}
	var th ReminderThresholds
	var err error
	if th.PendingAfterDays, err = read("reminder_pending_days", 3); err != nil {
		return ReminderThresholds{}, err
	}
	if th.RepeatEveryDays, err = read("reminder_repeat_days", 1); err != nil {
		return ReminderThresholds{}, err
	}
	if th.StaleAfterDays, err = read("reminder_stale_days", 1); err != nil {
		return ReminderThresholds{}, err
	}
	return th, nil
}

func (s *Store) requestsByStatus(ctx context.Context, status string) ([]Request, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,number,status,treatment,type,amount,COALESCE(approved_amount,0),
		COALESCE(purpose,''),COALESCE(vendor_payee,''),requester_id,manager_id,submitted_at,processing_at,reminder_last_sent,urgent
		FROM payment_requests WHERE status=?`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		var r Request
		var appr int64
		var urgent int
		if err := rows.Scan(&r.ID, &r.Number, &r.Status, &r.Treatment, &r.Type, &r.Amount, &appr,
			&r.Purpose, &r.VendorPayee, &r.RequesterID, &r.ManagerID, &r.SubmittedAt, &r.ProcessingAt, &r.ReminderLastSent, &urgent); err != nil {
			return nil, err
		}
		if appr != 0 {
			a := appr
			r.ApprovedAmount = &a
		}
		r.Urgent = urgent == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) reminderCandidates(ctx context.Context, now time.Time, status string, repeatEvery int, ageOK func(Request) bool) ([]Request, error) {
	reqs, err := s.requestsByStatus(ctx, status)
	if err != nil {
		return nil, err
	}
	if repeatEvery < 1 {
		repeatEvery = 1
	}
	now = now.UTC()
	var out []Request
	for _, r := range reqs {
		if !ageOK(r) {
			continue
		}
		if r.ReminderLastSent != nil && calendarDaysBetween(r.ReminderLastSent.UTC(), now) < repeatEvery {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Store) RequestsPendingReminder(ctx context.Context, now time.Time, th ReminderThresholds) ([]Request, error) {
	nowUTC := now.UTC()
	after := th.PendingAfterDays
	if after < 1 {
		after = 1
	}
	return s.reminderCandidates(ctx, nowUTC, "pending", th.RepeatEveryDays, func(r Request) bool {
		return r.SubmittedAt != nil && calendarDaysBetween(r.SubmittedAt.UTC(), nowUTC) >= after
	})
}

func (s *Store) RequestsStaleProcessing(ctx context.Context, now time.Time, th ReminderThresholds) ([]Request, error) {
	nowUTC := now.UTC()
	after := th.StaleAfterDays
	if after < 1 {
		after = 1
	}
	return s.reminderCandidates(ctx, nowUTC, "processing", th.RepeatEveryDays, func(r Request) bool {
		return r.ProcessingAt != nil && calendarDaysBetween(r.ProcessingAt.UTC(), nowUTC) >= after
	})
}

func (s *Store) MarkReminderSent(ctx context.Context, requestID int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET reminder_last_sent=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, now.UTC(), requestID)
	return err
}

func (s *Store) ResetReminder(ctx context.Context, requestID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE payment_requests SET reminder_last_sent=NULL, updated_at=CURRENT_TIMESTAMP WHERE id=?`, requestID)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestCalendarDaysBetweenCountsDateBoundaries|TestReminderThresholdsDefaultsAndOverrides|TestRequestsPendingReminderHonoursConfiguredThreshold|TestRequestsStaleProcessingHonoursConfiguredThreshold|TestResetReminderClearsThrottle' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/reminders.go internal/store/reminders_test.go internal/store/models.go
git commit -m "feat(store): reminder queries with injected clock and admin-set thresholds"
```

---

### Task 6: Configuration "Reminders and ageing" fieldset (D6)

The three thresholds are now data, so an administrator must be able to see and change them. They belong in the one Configuration screen Phase 2 owns — the same append-a-fieldset pattern Phase 4 uses for recoverable categories — not on the notification rules screen.

**Files:**
- Modify: `internal/app/configuration.go` (Phase 2 — surface the three keys)
- Modify: `internal/app/app.go` (one PageData field)
- Modify: `internal/app/templates.go` (append one `<fieldset>` to the Phase-2 `configuration` template)
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: Phase 2's `GET/POST /configuration` behind `config`{view,edit} and its generic `app_settings` save handler; `store.ReminderThresholds` (Task 5).
- Produces: PageData field `Reminders store.ReminderThresholds`; one appended `<fieldset>` writing `reminder_pending_days`, `reminder_repeat_days`, `reminder_stale_days`.

No new route: the fieldset's inputs post with the rest of the Configuration form, and Phase 2's generic handler writes any `app_settings` key it is given from an allow-list. Phase 5's only handler-side change is adding those three keys to that allow-list.

- [ ] **Step 1: Write the failing test**

Add to `internal/app/app_integration_test.go`:
```go
func TestConfigurationRemindersFieldsetDrivesThresholds(t *testing.T) { // D6
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/configuration", nil, ""))
	for _, want := range []string{"Reminders and ageing", `name="reminder_pending_days"`, `name="reminder_repeat_days"`, `name="reminder_stale_days"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("configuration screen missing %q", want)
		}
	}
	// The defaults are rendered, not blank.
	if !strings.Contains(body, `value="3"`) {
		t.Fatal("the pending threshold must render its 3-day default")
	}

	form := url.Values{"reminder_pending_days": {"5"}, "reminder_repeat_days": {"2"}, "reminder_stale_days": {"4"}}
	requireStatus(t, s.postForm("/configuration", form), http.StatusSeeOther)
	th, err := s.st.ReminderThresholds(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if th.PendingAfterDays != 5 || th.RepeatEveryDays != 2 || th.StaleAfterDays != 4 {
		t.Fatalf("thresholds after save = %+v, want 5/2/4", th)
	}
	// The rules screen must not offer them a second time.
	admin := responseBody(t, s.request(http.MethodGet, "/admin/notifications", nil, ""))
	if strings.Contains(admin, `name="reminder_pending_days"`) {
		t.Fatal("reminder thresholds belong to Configuration, not the notification rules screen")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestConfigurationRemindersFieldsetDrivesThresholds -v`
Expected: FAIL — the Configuration screen renders no Reminders fieldset, so the first `strings.Contains` assertion fails.

- [ ] **Step 3: Write minimal implementation**

In `internal/app/app.go`, add to `PageData`:
```go
	Reminders store.ReminderThresholds
```
In `internal/app/configuration.go`, inside the GET handler before it renders:
```go
	// Phase 5 fieldset: reminder policy, read through the same defaulting the
	// scheduler uses so the screen can never show a value the scheduler ignores.
	th, err := a.st.ReminderThresholds(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data.Reminders = th
```
and add the three keys to the POST handler's `app_settings` allow-list:
```go
	"reminder_pending_days", "reminder_repeat_days", "reminder_stale_days",
```

- [ ] **Step 4: Append the fieldset**

In `internal/app/templates.go`, inside the Phase-2 `configuration` template's `<form>`, append after the Attachments fieldset:

```html
  <fieldset>
    <legend>Reminders and ageing</legend>
    <div class="form-grid">
      <div class="field span-4 m-half"><label for="rem-days">Start reminders after</label><input id="rem-days" name="reminder_pending_days" type="number" min="1" value="{{.Reminders.PendingAfterDays}}"><span class="hint">Calendar days, not working days.</span></div>
      <div class="field span-4 m-half"><label for="rem-freq">Then remind every</label><input id="rem-freq" name="reminder_repeat_days" type="number" min="1" value="{{.Reminders.RepeatEveryDays}}"><span class="hint">Days between repeat reminders on the same request.</span></div>
      <div class="field span-4 m-half"><label for="stale">Stale reservation reminder after</label><input id="stale" name="reminder_stale_days" type="number" min="1" value="{{.Reminders.StaleAfterDays}}"><span class="hint">Days a request may sit in Processing.</span></div>
    </div>
  </fieldset>
```

The mockup renders "Then remind" as a Daily / Every 2 days / Weekly select. A plain day count is the same setting without a fixed option list, and it is what `ReminderThresholds.RepeatEveryDays` already is — so no mapping layer exists to drift.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/app/ -run TestConfigurationRemindersFieldsetDrivesThresholds -v`
Expected: PASS. (The final assertion needs Task 11's `/admin/notifications` route; run this test again at the end of Task 11 if executing strictly in order.)

- [ ] **Step 6: Commit**

```bash
git add internal/app/app.go internal/app/configuration.go internal/app/templates.go internal/app/app_integration_test.go
git commit -m "feat(app): surface reminder thresholds in the Configuration screen"
```

---

### Task 7: notify package — Mailer, SMTPMailer, event constants, test helpers

**Files:**
- Create: `internal/notify/events.go`
- Create: `internal/notify/mailer.go`
- Test: `internal/notify/notify_test.go` (shared helpers + `fakeMailer`), `internal/notify/mailer_test.go`

**Interfaces:**
- Consumes: `store.Store`, `store.AppSettings`, `config.SMTPPassword` (via caller), `net/smtp`.
- Produces:
  - The twelve event constants of G20 (see the code below), plus `AllEvents` for the admin screen and a `kindFor(event)` classifier used by the in-app writer.
  - `type Message struct { From string; To, Cc []string; Subject, Body string }`
  - `type Mailer interface { Send(ctx context.Context, msg Message) error }`
  - `func NewSMTPMailer(st *store.Store, password string) *SMTPMailer` with `Send` and an injectable `sendMail` field.
  - Test helpers (package `notify`): `fakeMailer`, `openTestStore`, `testActor`, `mustUser`, `grantAccounts`, `enableEvent`, `insertRequest`, `must`, `contains`, `countOccurrences`.

- [ ] **Step 1: Write the failing test**

Create `internal/notify/notify_test.go`:
```go
package notify

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"fervidbudget/internal/store"
)

type fakeMailer struct {
	mu   sync.Mutex
	sent []Message
}

func (f *fakeMailer) Send(_ context.Context, m Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakeMailer) messages() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Message(nil), f.sent...)
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "notify-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func testActor(t *testing.T, st *store.Store) store.User {
	t.Helper()
	ctx := context.Background()
	id, err := st.CreateUser(ctx, "admin@notify.test", "Admin", "h", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.UserByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mustUser(t *testing.T, st *store.Store, email, name, role string) int64 {
	t.Helper()
	id, err := st.CreateUser(context.Background(), email, name, "h", role, true)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func grantAccounts(t *testing.T, st *store.Store, userIDs ...int64) {
	t.Helper()
	ctx := context.Background()
	res, err := st.DB().ExecContext(ctx, `INSERT INTO roles(name,is_system) VALUES('Accounts',1)`)
	if err != nil {
		t.Fatal(err)
	}
	roleID, _ := res.LastInsertId()
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO role_permissions(role_id,resource,action) VALUES(?,?,?)`, roleID, "payment", "process"); err != nil {
		t.Fatal(err)
	}
	for _, uid := range userIDs {
		if _, err := st.DB().ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, uid, roleID); err != nil {
			t.Fatal(err)
		}
	}
}

func enableEvent(t *testing.T, st *store.Store, actor store.User, in store.NotificationSetting) {
	t.Helper()
	if err := st.SetNotificationSetting(context.Background(), actor, in); err != nil {
		t.Fatal(err)
	}
}

func insertRequest(t *testing.T, st *store.Store, number, status string, requesterID, managerID int64, submittedAt time.Time, processingAt *time.Time) int64 {
	t.Helper()
	res, err := st.DB().ExecContext(context.Background(),
		`INSERT INTO payment_requests(number,status,treatment,type,amount,purpose,vendor_payee,requester_id,manager_id,submitted_at,processing_at,urgent)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,0)`,
		number, status, "budget", "vendor_invoice", int64(500000), "Rent", "Acme", requesterID, managerID, submittedAt.UTC(), processingAt)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func countOccurrences(list []string, want string) int {
	n := 0
	for _, v := range list {
		if v == want {
			n++
		}
	}
	return n
}
```
Create `internal/notify/mailer_test.go`:
```go
package notify

import (
	"context"
	"net/smtp"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

func TestSMTPMailerBuildsMessageAndUsesEnvPassword(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	must(t, st.SetAppSettings(ctx, actor, store.AppSettings{
		SMTPHost: "smtp.example.test", SMTPPort: 2525, SMTPUsername: "mailer@example.test",
		SMTPFromName: "Fervid", SMTPFromAddr: "noreply@example.test",
	}))
	m := NewSMTPMailer(st, "s3cr3t-from-env")
	var gotAddr, gotFrom string
	var gotAuth smtp.Auth
	var gotTo []string
	var gotMsg []byte
	m.sendMail = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr, gotAuth, gotFrom, gotTo, gotMsg = addr, a, from, to, msg
		return nil
	}
	err := m.Send(ctx, Message{From: "Fervid <noreply@example.test>", To: []string{"a@x.test"}, Cc: []string{"b@y.test"}, Subject: "Hi", Body: "Body ₹1.00"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAddr != "smtp.example.test:2525" {
		t.Fatalf("addr = %q, want smtp.example.test:2525", gotAddr)
	}
	if gotFrom != "noreply@example.test" {
		t.Fatalf("envelope from = %q, want noreply@example.test", gotFrom)
	}
	if gotAuth == nil {
		t.Fatal("expected PLAIN auth when username is configured")
	}
	if len(gotTo) != 2 {
		t.Fatalf("recipients = %v, want To+Cc", gotTo)
	}
	s := string(gotMsg)
	for _, want := range []string{"From: Fervid <noreply@example.test>", "To: a@x.test", "Cc: b@y.test", "Subject: Hi", "Body ₹1.00"} {
		if !strings.Contains(s, want) {
			t.Fatalf("message missing %q:\n%s", want, s)
		}
	}
}

func TestSMTPMailerRequiresHost(t *testing.T) {
	st := openTestStore(t)
	m := NewSMTPMailer(st, "")
	err := m.Send(context.Background(), Message{To: []string{"a@x.test"}, Subject: "x", Body: "y"})
	if err == nil {
		t.Fatal("expected error when smtp host is not configured")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/notify/ -run 'TestSMTPMailer' -v`
Expected: FAIL — `undefined: NewSMTPMailer` / `undefined: Message`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/notify/events.go`:
```go
package notify

// The twelve events of G20. Keys match the notification_settings rows seeded by
// migration v6; AllEvents is the order the admin rules screen renders.
const (
	EventRequestSubmitted             = "request_submitted"
	EventRequestEdited                = "request_edited"
	EventRequestReturned              = "request_returned"
	EventRequestRejected              = "request_rejected"
	EventRequestApproved              = "request_approved"
	EventRequestUrgent                = "request_urgent"
	EventRequestOnHold                = "request_on_hold"
	EventCancellationRequested        = "request_cancellation_requested"
	EventPaymentSettled               = "payment_settled"
	EventPaymentPartialReview         = "payment_partial_review"
	EventReminderPending              = "reminder_pending"
	EventReminderStaleReservation     = "reminder_stale_reservation"
)

var AllEvents = []string{
	EventRequestSubmitted, EventRequestEdited, EventRequestReturned, EventRequestRejected,
	EventRequestApproved, EventRequestUrgent, EventRequestOnHold, EventCancellationRequested,
	EventPaymentSettled, EventPaymentPartialReview, EventReminderPending, EventReminderStaleReservation,
}

// kindFor classifies an event for the in-app centre's .segmented filter strip.
// "mention" is reserved for events where a person wrote something addressed to
// the recipient; everything else is activity, and the two reminders are reminders.
func kindFor(event string) string {
	switch event {
	case EventReminderPending, EventReminderStaleReservation:
		return "reminder"
	case EventRequestReturned, EventRequestOnHold, EventRequestRejected:
		return "mention"
	default:
		return "activity"
	}
}
```

`EventRequestUrgent` replaces the earlier draft's `EventRequestSubmittedUrgent` / `EventRequestApprovedUrgent` pair. It is fired at both send points; `resolveRecipients` (Task 8) reads the request's status to decide whether it goes to the approver or to Accounts + management.
Create `internal/notify/mailer.go`:
```go
package notify

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"

	"fervidbudget/internal/store"
)

type Message struct {
	From    string
	To      []string
	Cc      []string
	Subject string
	Body    string
}

type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

type SMTPMailer struct {
	st       *store.Store
	password string
	sendMail func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

func NewSMTPMailer(st *store.Store, password string) *SMTPMailer {
	return &SMTPMailer{st: st, password: password, sendMail: smtp.SendMail}
}

func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	app, err := m.st.GetAppSettings(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(app.SMTPHost) == "" {
		return fmt.Errorf("smtp host is not configured")
	}
	port := app.SMTPPort
	if port == 0 {
		port = 587
	}
	recipients := append(append([]string{}, msg.To...), msg.Cc...)
	if len(recipients) == 0 {
		return nil
	}
	var auth smtp.Auth
	if strings.TrimSpace(app.SMTPUsername) != "" {
		auth = smtp.PlainAuth("", app.SMTPUsername, m.password, app.SMTPHost)
	}
	addr := fmt.Sprintf("%s:%d", app.SMTPHost, port)
	return m.sendMail(addr, auth, envelopeAddr(app.SMTPFromAddr, msg.From), recipients, []byte(buildRFC822(msg)))
}

func buildRFC822(msg Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", msg.From)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(msg.To, ", "))
	if len(msg.Cc) > 0 {
		fmt.Fprintf(&b, "Cc: %s\r\n", strings.Join(msg.Cc, ", "))
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", msg.Subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(msg.Body)
	return b.String()
}

func envelopeAddr(fromAddr, fromHeader string) string {
	if a := strings.TrimSpace(fromAddr); a != "" {
		return a
	}
	if i := strings.LastIndex(fromHeader, "<"); i >= 0 {
		if j := strings.Index(fromHeader[i:], ">"); j > 0 {
			return fromHeader[i+1 : i+j]
		}
	}
	return fromHeader
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/notify/ -run 'TestSMTPMailer' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/events.go internal/notify/mailer.go internal/notify/notify_test.go internal/notify/mailer_test.go
git commit -m "feat(notify): Mailer interface, SMTP mailer with env password, and test harness"
```

---

### Task 8: notify.Service.Notify — in-app rows always, email opt-in (G19, G20)

`Notify` gains a first responsibility: writing the in-app rows. Those always fire — the mockup's admin screen states it as a fixed "On" pill — and email remains the per-event opt-in layer on top.

**Files:**
- Create: `internal/notify/service.go`
- Test: `internal/notify/service_test.go`

**Interfaces:**
- Consumes: `store.Store` (`NotificationSetting`, `GetAppSettings`, `UsersWithPermission`, `UserByID`, `AddNotification`), `store.Request`, `store.NotificationSetting`, `store.AppSettings`, `store.Notification`, `Mailer`, `money.FormatPaise`, event constants and `kindFor`.
- Produces:
  - `func NewService(st *store.Store, mailer Mailer) *Service`
  - `func (s *Service) Notify(ctx context.Context, event string, req store.Request) error`
  - `type RequestView struct { … }` with an added `Link string`
  - `func resolveRecipients(event string, cfg store.NotificationSetting, app store.AppSettings, v RequestView, accounts []string) (to, cc []string)`
  - `func resolveInAppUsers(event string, cfg store.NotificationSetting, req store.Request, accountsIDs []int64) []int64`
  - `func renderTemplate(tmpl string, v RequestView) (string, error)` — strict `{{token}}` substitution
  - `func notifyFields(v RequestView) map[string]string` — the single definition of the placeholder vocabulary

**Three behaviours fixed here:**

1. **In-app first, unconditionally.** Before any email decision, `Notify` writes one `notifications` row per resolved in-app recipient. A failure to send email never loses the in-app record.
2. **`request_urgent` is status-driven.** Pre-approval (`status == "pending"`) it goes To the approver with no management Cc. Post-approval it goes To the Accounts group with the management list on Cc. One admin row, two behaviours, no second event.
3. **`renderTemplate` no longer executes Go templates.** Admin-editable text is substituted token by token from `notifyFields`; an unknown `{{token}}` is an error, so a typo surfaces on save rather than emitting an empty string into an email.

- [ ] **Step 0: Recipient reference**

| Event | In-app + email To | Cc |
|---|---|---|
| `request_submitted`, `request_edited`, `payment_partial_review` | approver | fixed Cc only |
| `request_returned`, `request_rejected`, `request_on_hold` | requester | fixed Cc only |
| `request_approved` | requester + Accounts group | fixed Cc + management list |
| `request_urgent` (pending) | approver | fixed Cc only |
| `request_urgent` (approved onward) | Accounts group | fixed Cc + management list |
| `request_cancellation_requested` | approver + assigned accountant (`ProcessingBy`), else Accounts group | fixed Cc only |
| `payment_settled` | requester + approver | fixed Cc only |
| `reminder_pending` | approver | fixed Cc only |
| `reminder_stale_reservation` | assigned accountant (`ProcessingBy`), else Accounts group | fixed Cc only |

The include flags on each row are what actually drive this — the table is the seeded default, and an admin who unticks "The requester" changes it.

- [ ] **Step 1: Write the failing test**

Create `internal/notify/service_test.go`:
```go
package notify

import (
	"context"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

func TestRenderTemplateSubstitutesTheTokenVocabulary(t *testing.T) { // G20
	v := RequestView{
		Number: "PR-1", Amount: 123456, ApprovedAmount: 100000, Payee: "Acme",
		ManagerName: "Kavita Rao", Project: "Ops", Head: "Rent", NeededBy: "2026-08-01",
		Link: "https://budget.test/requests/1",
	}
	out, err := renderTemplate("{{number}}: {{amount}} to {{payee}}", v)
	if err != nil {
		t.Fatal(err)
	}
	if out != "PR-1: ₹1,234.56 to Acme" {
		t.Fatalf("render = %q, want PR-1: ₹1,234.56 to Acme", out)
	}
	full, err := renderTemplate("{{approver}} {{project}} {{head}} {{needed_by}} {{link}} {{approved_amount}}", v)
	if err != nil {
		t.Fatal(err)
	}
	if full != "Kavita Rao Ops Rent 2026-08-01 https://budget.test/requests/1 ₹1,000.00" {
		t.Fatalf("render = %q", full)
	}
	// Go template syntax is no longer executed — it is just an unknown token.
	if _, err := renderTemplate("{{.Number}}", v); err == nil {
		t.Fatal("Go template syntax must be rejected, not executed")
	}
	if _, err := renderTemplate("Hello {{nmuber}}", v); err == nil {
		t.Fatal("an unknown token must error so a typo is caught at save time")
	}
	// Text with no tokens passes through untouched.
	if out, err := renderTemplate("plain text, 100% of it", v); err != nil || out != "plain text, 100% of it" {
		t.Fatalf("plain render = %q (err %v)", out, err)
	}
}

func TestResolveRecipientsManagementCcOnlyOnApprovalAndDedupes(t *testing.T) {
	cfg := store.NotificationSetting{IncludeManager: true, IncludeRequester: true}
	app := store.AppSettings{ManagementRecipients: "board@test, board@test"}
	v := RequestView{ManagerEmail: "mgr@test", RequesterEmail: "mgr@test", Status: "pending"} // same address -> dedupe
	to, cc := resolveRecipients(EventRequestUrgent, cfg, app, v, nil)
	if len(to) != 1 || to[0] != "mgr@test" {
		t.Fatalf("to = %v, want [mgr@test] de-duplicated", to)
	}
	if len(cc) != 0 {
		t.Fatalf("cc = %v, want empty for a pre-approval urgent request", cc)
	}
	_, cc2 := resolveRecipients(EventRequestApproved, cfg, app, v, nil)
	if len(cc2) != 1 || cc2[0] != "board@test" {
		t.Fatalf("cc2 = %v, want [board@test] once on approval", cc2)
	}
	// The same urgent event, post-approval, does copy management.
	approved := v
	approved.Status = "approved"
	_, cc3 := resolveRecipients(EventRequestUrgent, cfg, app, approved, nil)
	if len(cc3) != 1 || cc3[0] != "board@test" {
		t.Fatalf("post-approval urgent cc = %v, want [board@test]", cc3)
	}
}

func TestNotifyApprovedResolvesAccountsRequesterAndManagement(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	reqUser := mustUser(t, st, "requester@test", "Requester", "data_entry")
	mgr := mustUser(t, st, "manager@test", "Manager", "admin")
	acct := mustUser(t, st, "accounts@test", "Accounts", "data_entry")
	grantAccounts(t, st, acct)
	must(t, st.SetAppSettings(ctx, actor, store.AppSettings{SMTPFromName: "Fervid", SMTPFromAddr: "no@reply.test", ManagementRecipients: "board@test"}))
	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventRequestApproved, EmailEnabled: true, IncludeRequester: true, IncludeAccounts: true,
		SubjectTemplate: "Request {{number}} approved",
		BodyTemplate:    "{{number}} for {{approved_amount}} to {{payee}}",
	})
	fm := &fakeMailer{}
	svc := NewService(st, fm)
	appr := int64(750000)
	req := store.Request{Number: "PR-2026-000009", Amount: 800000, ApprovedAmount: &appr, Project: "Ops", Head: "Rent", VendorPayee: "Acme", RequesterID: reqUser, ManagerID: mgr}
	must(t, svc.Notify(ctx, EventRequestApproved, req))
	msgs := fm.messages()
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
	m := msgs[0]
	if !contains(m.To, "requester@test") || !contains(m.To, "accounts@test") {
		t.Fatalf("To = %v, want requester + accounts", m.To)
	}
	if !contains(m.Cc, "board@test") {
		t.Fatalf("Cc = %v, want management list", m.Cc)
	}
	if m.Subject != "Request PR-2026-000009 approved" {
		t.Fatalf("subject = %q", m.Subject)
	}
	if !strings.Contains(m.Body, "₹7,500.00") {
		t.Fatalf("body missing formatted approved amount: %q", m.Body)
	}
	if m.From != "Fervid <no@reply.test>" {
		t.Fatalf("from = %q", m.From)
	}
	// G19: the same call wrote in-app rows for both To recipients.
	for _, uid := range []int64{reqUser, acct} {
		rows, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: uid})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Event != EventRequestApproved {
			t.Fatalf("user %d in-app rows = %+v, want one request_approved", uid, rows)
		}
		if rows[0].ReadAt != nil {
			t.Fatal("a new notification must start unread")
		}
	}
}

func TestNotifyDisabledEventStillWritesInAppButSendsNoEmail(t *testing.T) { // N1, G19
	ctx := context.Background()
	st := openTestStore(t)
	_ = testActor(t, st)
	reqUser := mustUser(t, st, "requester@test", "Requester", "data_entry")
	mgr := mustUser(t, st, "manager@test", "Manager", "admin")
	fm := &fakeMailer{}
	svc := NewService(st, fm)
	// request_approved is seeded with email_enabled=0 -> in-app only
	req := store.Request{Number: "PR-2026-000001", Amount: 100, Status: "approved", RequesterID: reqUser, ManagerID: mgr}
	must(t, svc.Notify(ctx, EventRequestApproved, req))
	if got := len(fm.messages()); got != 0 {
		t.Fatalf("messages = %d, want 0 when email is disabled", got)
	}
	rows, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: reqUser})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("in-app rows = %d, want 1 — in-app always fires, email is the opt-in layer", len(rows))
	}
}

func TestNotifyInAppKindsDriveTheFilterStrip(t *testing.T) { // G19
	ctx := context.Background()
	st := openTestStore(t)
	reqUser := mustUser(t, st, "requester@test", "Requester", "data_entry")
	mgr := mustUser(t, st, "manager@test", "Manager", "admin")
	svc := NewService(st, &fakeMailer{})
	base := store.Request{Number: "PR-2026-000030", Status: "returned", RequesterID: reqUser, ManagerID: mgr}
	must(t, svc.Notify(ctx, EventRequestReturned, base))
	must(t, svc.Notify(ctx, EventReminderPending, store.Request{Number: "PR-2026-000031", Status: "pending", RequesterID: reqUser, ManagerID: mgr}))
	counts, err := st.NotificationCounts(ctx, reqUser)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Mentions != 1 {
		t.Fatalf("mentions = %d, want 1 (a returned request names you)", counts.Mentions)
	}
	mgrCounts, err := st.NotificationCounts(ctx, mgr)
	if err != nil {
		t.Fatal(err)
	}
	if mgrCounts.Reminders != 1 {
		t.Fatalf("manager reminders = %d, want 1", mgrCounts.Reminders)
	}
}

func TestNotifyUrgentSubmitEmailsManagerAndConfersNoApprovalAuthority(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	reqUser := mustUser(t, st, "requester@test", "Requester", "data_entry")
	mgr := mustUser(t, st, "manager@test", "Manager", "admin")
	must(t, st.SetAppSettings(ctx, actor, store.AppSettings{SMTPFromAddr: "no@reply.test", ManagementRecipients: "board@test"}))
	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventRequestUrgent, EmailEnabled: true, IncludeManager: true,
		SubjectTemplate: "URGENT {{number}}", BodyTemplate: "please approve",
	})
	id := insertRequest(t, st, "PR-2026-000020", "pending", reqUser, mgr, timeNowUTC(), nil)
	fm := &fakeMailer{}
	svc := NewService(st, fm)
	req := store.Request{ID: id, Number: "PR-2026-000020", Status: "pending", Urgent: true, RequesterID: reqUser, ManagerID: mgr}
	must(t, svc.Notify(ctx, EventRequestUrgent, req))
	msgs := fm.messages()
	if len(msgs) != 1 || !contains(msgs[0].To, "manager@test") {
		t.Fatalf("urgent submit To = %v, want manager", msgs)
	}
	if len(msgs[0].Cc) != 0 {
		t.Fatalf("urgent submit Cc = %v, want empty (no management pre-approval)", msgs[0].Cc)
	}
	// Notification confers no approval authority: request row is unchanged.
	var status string
	if err := st.DB().QueryRow(`SELECT status FROM payment_requests WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("status = %q after urgent notify, want unchanged pending", status)
	}
}

func TestNotifyApprovedUrgentEmailsAccountsAndManagement(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	reqUser := mustUser(t, st, "requester@test", "Requester", "data_entry")
	mgr := mustUser(t, st, "manager@test", "Manager", "admin")
	acct := mustUser(t, st, "accounts@test", "Accounts", "data_entry")
	grantAccounts(t, st, acct)
	must(t, st.SetAppSettings(ctx, actor, store.AppSettings{SMTPFromAddr: "no@reply.test", ManagementRecipients: "board@test"}))
	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventRequestUrgent, EmailEnabled: true, IncludeAccounts: true,
		SubjectTemplate: "URGENT approved {{number}}", BodyTemplate: "pay now",
	})
	fm := &fakeMailer{}
	svc := NewService(st, fm)
	// The same event, now past approval: recipients follow the request's status.
	req := store.Request{Number: "PR-2026-000021", Status: "approved", Urgent: true, RequesterID: reqUser, ManagerID: mgr}
	must(t, svc.Notify(ctx, EventRequestUrgent, req))
	msgs := fm.messages()
	if len(msgs) != 1 || !contains(msgs[0].To, "accounts@test") || !contains(msgs[0].Cc, "board@test") {
		t.Fatalf("approved-urgent recipients = %+v, want accounts To + management Cc", msgs)
	}
}

func TestNotifyRequestEditedEmailsManager(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	reqUser := mustUser(t, st, "requester@test", "Requester", "data_entry")
	mgr := mustUser(t, st, "manager@test", "Manager", "admin")
	must(t, st.SetAppSettings(ctx, actor, store.AppSettings{SMTPFromAddr: "no@reply.test"}))
	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventRequestEdited, EmailEnabled: true, IncludeManager: true,
		SubjectTemplate: "Edited {{number}}", BodyTemplate: "re-sent",
	})
	fm := &fakeMailer{}
	svc := NewService(st, fm)
	req := store.Request{Number: "PR-2026-000022", Status: "pending", RequesterID: reqUser, ManagerID: mgr}
	must(t, svc.Notify(ctx, EventRequestEdited, req))
	msgs := fm.messages()
	if len(msgs) != 1 || !contains(msgs[0].To, "manager@test") || msgs[0].Subject != "Edited PR-2026-000022" {
		t.Fatalf("edited notification = %+v, want manager email", msgs)
	}
}

func TestNotifyRejectsAnInvalidTemplateInsteadOfSendingEmptyText(t *testing.T) { // G20
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	reqUser := mustUser(t, st, "requester@test", "Requester", "data_entry")
	mgr := mustUser(t, st, "manager@test", "Manager", "admin")
	must(t, st.SetAppSettings(ctx, actor, store.AppSettings{SMTPFromAddr: "no@reply.test"}))
	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventRequestEdited, EmailEnabled: true, IncludeManager: true,
		SubjectTemplate: "Edited {{nmuber}}", BodyTemplate: "re-sent",
	})
	fm := &fakeMailer{}
	svc := NewService(st, fm)
	err := svc.Notify(ctx, EventRequestEdited, store.Request{Number: "PR-2026-000023", Status: "pending", RequesterID: reqUser, ManagerID: mgr})
	if err == nil {
		t.Fatal("a template with an unknown token must error, not send a garbled email")
	}
	if got := len(fm.messages()); got != 0 {
		t.Fatalf("messages = %d, want 0", got)
	}
	// The in-app row was still written before the email step failed.
	rows, _ := st.ListNotifications(ctx, store.NotificationFilter{UserID: mgr})
	if len(rows) != 1 {
		t.Fatalf("in-app rows = %d, want 1 (in-app must not depend on the email template)", len(rows))
	}
}
```
Add a tiny helper `timeNowUTC` to `internal/notify/notify_test.go`:
```go
func timeNowUTC() time.Time { return time.Now().UTC() }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/notify/ -run 'TestRenderTemplate|TestResolveRecipients|TestNotify' -v`
Expected: FAIL — `undefined: renderTemplate` / `undefined: NewService`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/notify/service.go`:
```go
package notify

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"fervidbudget/internal/money"
	"fervidbudget/internal/store"
)

type Service struct {
	st     *store.Store
	mailer Mailer
}

func NewService(st *store.Store, mailer Mailer) *Service {
	return &Service{st: st, mailer: mailer}
}

type RequestView struct {
	Number, Treatment, Type, Project, Head, Payee, Purpose, Status string
	Amount, ApprovedAmount                                         int64
	NeededBy                                                       string
	RequesterName, RequesterEmail                                  string
	ManagerName, ManagerEmail                                      string
	SubmittedOn, ProcessingOn                                      string
	Link                                                           string
}

// Notify writes the in-app rows first — they always fire — and then, only if the
// event's email channel is switched on, resolves email recipients and sends.
func (s *Service) Notify(ctx context.Context, event string, req store.Request) error {
	cfg, err := s.st.NotificationSetting(ctx, event)
	if errors.Is(err, store.ErrNotFound) {
		return nil // unknown event: nothing configured, nothing to deliver
	}
	if err != nil {
		return err
	}
	app, err := s.st.GetAppSettings(ctx)
	if err != nil {
		return err
	}
	view, err := s.newRequestView(ctx, req, app)
	if err != nil {
		return err
	}
	accountUsers, err := s.accountsUsers(ctx, cfg)
	if err != nil {
		return err
	}

	// 1. In-app, unconditionally (G19).
	if err := s.writeInApp(ctx, event, cfg, req, view, accountUsers); err != nil {
		return err
	}

	// 2. Email, only when this event opted in.
	if !cfg.EmailEnabled {
		return nil
	}
	to, cc := resolveRecipients(event, cfg, app, view, emails(accountUsers))
	if len(to) == 0 && len(cc) == 0 {
		return nil
	}
	subject, err := renderTemplate(cfg.SubjectTemplate, view)
	if err != nil {
		return fmt.Errorf("notification %q subject: %w", event, err)
	}
	body, err := renderTemplate(cfg.BodyTemplate, view)
	if err != nil {
		return fmt.Errorf("notification %q body: %w", event, err)
	}
	return s.mailer.Send(ctx, Message{From: formatFrom(app), To: to, Cc: cc, Subject: subject, Body: body})
}

func (s *Service) accountsUsers(ctx context.Context, cfg store.NotificationSetting) ([]store.User, error) {
	if !cfg.IncludeAccounts {
		return nil, nil
	}
	return s.st.UsersWithPermission(ctx, "payment", "process")
}

// writeInApp creates one notifications row per resolved recipient. The title is
// the rendered subject when the template is valid, and a plain fallback when it
// is not — a broken email template must never cost the user their in-app record.
func (s *Service) writeInApp(ctx context.Context, event string, cfg store.NotificationSetting, req store.Request, v RequestView, accountUsers []store.User) error {
	var accountIDs []int64
	for _, u := range accountUsers {
		accountIDs = append(accountIDs, u.ID)
	}
	title, err := renderTemplate(cfg.SubjectTemplate, v)
	if err != nil || strings.TrimSpace(title) == "" {
		title = cfg.Label
		if title == "" {
			title = event
		}
		title += " · " + v.Number
	}
	body, err := renderTemplate(cfg.BodyTemplate, v)
	if err != nil {
		body = ""
	}
	var reqID *int64
	if req.ID != 0 {
		id := req.ID
		reqID = &id
	}
	for _, uid := range resolveInAppUsers(event, cfg, req, accountIDs) {
		if _, err := s.st.AddNotification(ctx, store.Notification{
			UserID: uid, Event: event, Kind: kindFor(event), RequestID: reqID,
			Title: title, Body: body, Href: requestHref(req),
		}); err != nil {
			return err
		}
	}
	return nil
}

// resolveInAppUsers mirrors resolveRecipients in user-id space. The include flags
// are the same switches an admin sees; the assigned accountant is added for the
// two events the design routes to them personally.
func resolveInAppUsers(event string, cfg store.NotificationSetting, req store.Request, accountIDs []int64) []int64 {
	var ids []int64
	if cfg.IncludeRequester && req.RequesterID != 0 {
		ids = append(ids, req.RequesterID)
	}
	if cfg.IncludeManager && req.ManagerID != 0 && !(event == EventRequestUrgent && req.Status != "pending") {
		ids = append(ids, req.ManagerID)
	}
	if cfg.IncludeAccounts && !(event == EventRequestUrgent && req.Status == "pending") {
		switch event {
		case EventCancellationRequested, EventReminderStaleReservation:
			if req.ProcessingBy != nil && *req.ProcessingBy != 0 {
				ids = append(ids, *req.ProcessingBy) // the person who reserved it
			} else {
				ids = append(ids, accountIDs...)
			}
		default:
			ids = append(ids, accountIDs...)
		}
	}
	seen := map[int64]bool{}
	var out []int64
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func requestHref(req store.Request) string {
	if req.ID == 0 {
		return "/requests"
	}
	return fmt.Sprintf("/requests/%d", req.ID)
}

func (s *Service) newRequestView(ctx context.Context, req store.Request, app store.AppSettings) (RequestView, error) {
	requester, err := s.st.UserByID(ctx, req.RequesterID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return RequestView{}, err
	}
	manager, err := s.st.UserByID(ctx, req.ManagerID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return RequestView{}, err
	}
	v := RequestView{
		Number: req.Number, Treatment: req.Treatment, Type: req.Type,
		Project: req.Project, Head: req.Head, Payee: req.VendorPayee,
		Purpose: req.Purpose, Status: req.Status, Amount: req.Amount, NeededBy: req.NeededBy,
		RequesterName: requester.Name, RequesterEmail: requester.Email,
		ManagerName: manager.Name, ManagerEmail: manager.Email,
		Link: app.BaseURL + requestHref(req),
	}
	if req.ApprovedAmount != nil {
		v.ApprovedAmount = *req.ApprovedAmount
	}
	if req.SubmittedAt != nil {
		v.SubmittedOn = req.SubmittedAt.UTC().Format("2006-01-02")
	}
	if req.ProcessingAt != nil {
		v.ProcessingOn = req.ProcessingAt.UTC().Format("2006-01-02")
	}
	return v, nil
}

func resolveRecipients(event string, cfg store.NotificationSetting, app store.AppSettings, v RequestView, accounts []string) (to, cc []string) {
	// One urgent row, two send points: before approval it is the approver's
	// problem, after approval it is Accounts' — and only then does management care.
	urgentPreApproval := event == EventRequestUrgent && v.Status == "pending"
	toList := splitList(cfg.ToRecipients)
	if cfg.IncludeRequester {
		toList = append(toList, v.RequesterEmail)
	}
	if cfg.IncludeManager && !(event == EventRequestUrgent && !urgentPreApproval) {
		toList = append(toList, v.ManagerEmail)
	}
	if cfg.IncludeAccounts && !urgentPreApproval {
		toList = append(toList, accounts...)
	}
	ccList := splitList(cfg.CcRecipients)
	if event == EventRequestApproved || (event == EventRequestUrgent && !urgentPreApproval) {
		ccList = append(ccList, splitList(app.ManagementRecipients)...)
	}
	to = dedupe(nil, toList)
	cc = dedupe(to, ccList)
	return to, cc
}

var tokenPattern = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

// notifyFields is the single definition of the admin-facing placeholder
// vocabulary (G20). The admin sheet's .hint lists exactly these keys.
func notifyFields(v RequestView) map[string]string {
	return map[string]string{
		"number":          v.Number,
		"amount":          money.FormatPaise(v.Amount),
		"approved_amount": money.FormatPaise(v.ApprovedAmount),
		"payee":           v.Payee,
		"requester":       v.RequesterName,
		"approver":        v.ManagerName,
		"project":         v.Project,
		"head":            v.Head,
		"purpose":         v.Purpose,
		"status":          v.Status,
		"needed_by":       v.NeededBy,
		"submitted_on":    v.SubmittedOn,
		"processing_on":   v.ProcessingOn,
		"link":            v.Link,
	}
}

// NotifyFieldNames is the sorted vocabulary the admin sheet renders in its .hint.
func NotifyFieldNames() []string {
	return []string{"number", "amount", "approved_amount", "payee", "requester", "approver",
		"project", "head", "purpose", "status", "needed_by", "submitted_on", "processing_on", "link"}
}

// renderTemplate substitutes {{token}} placeholders. It deliberately does NOT
// execute Go templates: notification text is admin-editable, and an editable
// string that reaches text/template is an execution surface. An unrecognised
// token is an error so a typo fails on save instead of silently disappearing.
func renderTemplate(tmpl string, v RequestView) (string, error) {
	fields := notifyFields(v)
	var bad []string
	out := tokenPattern.ReplaceAllStringFunc(tmpl, func(match string) string {
		key := strings.TrimSpace(match[2 : len(match)-2])
		val, ok := fields[key]
		if !ok {
			bad = append(bad, key)
			return match
		}
		return val
	})
	if len(bad) > 0 {
		return "", fmt.Errorf("unknown template field(s): %s", strings.Join(bad, ", "))
	}
	return out, nil
}

func splitList(raw string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if e := strings.ToLower(strings.TrimSpace(part)); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func dedupe(alreadySeen, in []string) []string {
	seen := map[string]bool{}
	for _, e := range alreadySeen {
		seen[e] = true
	}
	var out []string
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

func emails(users []store.User) []string {
	var out []string
	for _, u := range users {
		if u.Email != "" {
			out = append(out, u.Email)
		}
	}
	return out
}

func formatFrom(app store.AppSettings) string {
	name := strings.TrimSpace(app.SMTPFromName)
	addr := strings.TrimSpace(app.SMTPFromAddr)
	if name != "" && addr != "" {
		return fmt.Sprintf("%s <%s>", name, addr)
	}
	return addr
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/notify/ -run 'TestRenderTemplate|TestResolveRecipients|TestNotify' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/service.go internal/notify/service_test.go internal/notify/notify_test.go
git commit -m "feat(notify): Service.Notify writing in-app rows and token-substituted email"
```

---

### Task 9: notify.Service.RunReminders + Scheduler (injected clock, admin thresholds)

**Files:**
- Create: `internal/notify/reminders.go`
- Test: `internal/notify/reminders_test.go`

**Interfaces:**
- Consumes: `Service`, `store.ReminderThresholds`, `store.RequestsPendingReminder`, `store.RequestsStaleProcessing`, `store.MarkReminderSent`, `store.NotificationSetting`, event constants.
- Produces:
  - `func (s *Service) RunReminders(ctx context.Context, now time.Time) error`
  - `func (s *Service) Scheduler(ctx context.Context, interval time.Duration, now func() time.Time)`

`RunReminders` reads `ReminderThresholds` once per run and passes it to both queries, so an administrator's change on the Configuration screen takes effect at the next tick with no restart. It also no longer short-circuits on `EmailEnabled`: the in-app reminder always fires, and `Notify` decides whether an email follows.

- [ ] **Step 1: Write the failing test**

Create `internal/notify/reminders_test.go`:
```go
package notify

import (
	"context"
	"testing"
	"time"

	"fervidbudget/internal/store"
)

func TestRunRemindersEmailsManagerAfterThreeCalendarDays(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	reqUser := mustUser(t, st, "req@test", "Req", "data_entry")
	mgr := mustUser(t, st, "mgr@test", "Mgr", "admin")
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	insertRequest(t, st, "PR-2026-000010", "pending", reqUser, mgr, now.AddDate(0, 0, -4), nil)
	must(t, st.SetAppSettings(ctx, actor, store.AppSettings{SMTPFromAddr: "no@reply.test"}))
	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventReminderPending, EmailEnabled: true, IncludeManager: true,
		SubjectTemplate: "Reminder {{number}}", BodyTemplate: "pending",
	})
	fm := &fakeMailer{}
	svc := NewService(st, fm)
	must(t, svc.RunReminders(ctx, now))
	if got := len(fm.messages()); got != 1 {
		t.Fatalf("first run messages = %d, want 1", got)
	}
	if !contains(fm.messages()[0].To, "mgr@test") {
		t.Fatalf("reminder To = %v, want manager", fm.messages()[0].To)
	}
	must(t, svc.RunReminders(ctx, now)) // same day -> throttled
	if got := len(fm.messages()); got != 1 {
		t.Fatalf("same-day messages = %d, want 1", got)
	}
	must(t, svc.RunReminders(ctx, now.AddDate(0, 0, 1))) // next day -> sends again
	if got := len(fm.messages()); got != 2 {
		t.Fatalf("next-day messages = %d, want 2", got)
	}
}

func TestRunRemindersHonoursConfiguredThresholds(t *testing.T) { // D6 + G20
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	reqUser := mustUser(t, st, "req@test", "Req", "data_entry")
	mgr := mustUser(t, st, "mgr@test", "Mgr", "admin")
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	insertRequest(t, st, "PR-2026-000013", "pending", reqUser, mgr, now.AddDate(0, 0, -4), nil)
	must(t, st.SetAppSettings(ctx, actor, store.AppSettings{SMTPFromAddr: "no@reply.test"}))
	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventReminderPending, EmailEnabled: true, IncludeManager: true,
		SubjectTemplate: "Reminder {{number}}", BodyTemplate: "pending",
	})
	// An administrator raises the wait to 7 days: a 4-day-old request goes quiet.
	must(t, st.SetAppSetting(ctx, actor, "reminder_pending_days", "7"))
	fm := &fakeMailer{}
	svc := NewService(st, fm)
	must(t, svc.RunReminders(ctx, now))
	if got := len(fm.messages()); got != 0 {
		t.Fatalf("messages = %d, want 0 while the configured wait is 7 days", got)
	}
	must(t, st.SetAppSetting(ctx, actor, "reminder_pending_days", "3"))
	must(t, svc.RunReminders(ctx, now))
	if got := len(fm.messages()); got != 1 {
		t.Fatalf("messages = %d, want 1 once the wait is back to 3 days", got)
	}
}

func TestRunRemindersWriteInAppEvenWithEmailOff(t *testing.T) { // G19
	ctx := context.Background()
	st := openTestStore(t)
	_ = testActor(t, st)
	reqUser := mustUser(t, st, "req@test", "Req", "data_entry")
	mgr := mustUser(t, st, "mgr@test", "Mgr", "admin")
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	insertRequest(t, st, "PR-2026-000014", "pending", reqUser, mgr, now.AddDate(0, 0, -4), nil)
	// reminder_pending is seeded with email_enabled=0; leave it off.
	fm := &fakeMailer{}
	svc := NewService(st, fm)
	must(t, svc.RunReminders(ctx, now))
	if got := len(fm.messages()); got != 0 {
		t.Fatalf("messages = %d, want 0 with email off", got)
	}
	rows, err := st.ListNotifications(ctx, store.NotificationFilter{UserID: mgr, Scope: "reminders"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("in-app reminders = %d, want 1 — reminders always reach the centre", len(rows))
	}
}

func TestRunRemindersNudgesStaleProcessing(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	reqUser := mustUser(t, st, "req@test", "Req", "data_entry")
	mgr := mustUser(t, st, "mgr@test", "Mgr", "admin")
	acct := mustUser(t, st, "acct@test", "Acct", "data_entry")
	grantAccounts(t, st, acct)
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	proc := now.AddDate(0, 0, -2)
	insertRequest(t, st, "PR-2026-000011", "processing", reqUser, mgr, now, &proc)
	must(t, st.SetAppSettings(ctx, actor, store.AppSettings{SMTPFromAddr: "no@reply.test"}))
	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventReminderStaleReservation, EmailEnabled: true, IncludeAccounts: true,
		SubjectTemplate: "Stale {{number}}", BodyTemplate: "still processing",
	})
	fm := &fakeMailer{}
	svc := NewService(st, fm)
	must(t, svc.RunReminders(ctx, now))
	msgs := fm.messages()
	if len(msgs) != 1 || !contains(msgs[0].To, "acct@test") {
		t.Fatalf("stale nudge = %+v, want accounts", msgs)
	}
}

func TestSchedulerRunsOnStartAndStopsOnCancel(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	actor := testActor(t, st)
	reqUser := mustUser(t, st, "req@test", "Req", "data_entry")
	mgr := mustUser(t, st, "mgr@test", "Mgr", "admin")
	now := time.Now().UTC()
	insertRequest(t, st, "PR-2026-000012", "pending", reqUser, mgr, now.AddDate(0, 0, -4), nil)
	must(t, st.SetAppSettings(ctx, actor, store.AppSettings{SMTPFromAddr: "no@reply.test"}))
	enableEvent(t, st, actor, store.NotificationSetting{
		Event: EventReminderPending, EmailEnabled: true, IncludeManager: true,
		SubjectTemplate: "R {{number}}", BodyTemplate: "pending",
	})
	fm := &fakeMailer{}
	svc := NewService(st, fm)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		svc.Scheduler(runCtx, time.Hour, func() time.Time { return now })
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for len(fm.messages()) == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatal("scheduler did not run on start")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop on cancel")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/notify/ -run 'TestRunReminders|TestScheduler' -v`
Expected: FAIL — `svc.RunReminders undefined` / `svc.Scheduler undefined`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/notify/reminders.go`:
```go
package notify

import (
	"context"
	"errors"
	"time"

	"fervidbudget/internal/store"
)

// RunReminders reads the admin-set thresholds once per run so a Configuration
// change takes effect on the next tick without a restart.
func (s *Service) RunReminders(ctx context.Context, now time.Time) error {
	now = now.UTC()
	th, err := s.st.ReminderThresholds(ctx)
	if err != nil {
		return err
	}
	if err := s.runReminderBatch(ctx, now, th, EventReminderPending, s.st.RequestsPendingReminder); err != nil {
		return err
	}
	return s.runReminderBatch(ctx, now, th, EventReminderStaleReservation, s.st.RequestsStaleProcessing)
}

func (s *Service) runReminderBatch(ctx context.Context, now time.Time, th store.ReminderThresholds, event string,
	load func(context.Context, time.Time, store.ReminderThresholds) ([]store.Request, error)) error {
	// No EmailEnabled short-circuit: the in-app reminder always fires and Notify
	// decides on its own whether an email follows.
	if _, err := s.st.NotificationSetting(ctx, event); errors.Is(err, store.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	reqs, err := load(ctx, now, th)
	if err != nil {
		return err
	}
	for _, req := range reqs {
		if err := s.Notify(ctx, event, req); err != nil {
			return err
		}
		if err := s.st.MarkReminderSent(ctx, req.ID, now); err != nil {
			return err
		}
	}
	return nil
}

// Scheduler runs reminders once immediately, then on every interval tick,
// and returns when ctx is cancelled. now is injected for determinism.
func (s *Service) Scheduler(ctx context.Context, interval time.Duration, now func() time.Time) {
	_ = s.RunReminders(ctx, now())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.RunReminders(ctx, now())
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/notify/ -run 'TestRunReminders|TestScheduler' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/reminders.go internal/notify/reminders_test.go
git commit -m "feat(notify): RunReminders and Scheduler on an injected clock and admin thresholds"
```

---

### Task 10: In-app notification centre `GET /notifications` + unread bell (G19)

`mockups/screens/notifications.html` is an approved screen that nothing rendered. This task builds it, plus the two mark-read routes and the shell bell badge Phase 0 left waiting for a number.

**Files:**
- Create: `internal/app/inapp.go` (handlers)
- Modify: `internal/app/app.go` (routes, PageData fields), `internal/app/http_errors.go` or wherever Phase 0 builds the shell (populate `Shell.Unread`)
- Modify: `internal/app/templates.go` (`notifications` template)
- Test: `internal/app/inapp_app_test.go`

**Interfaces:**
- Consumes: `store.ListNotifications`, `store.NotificationCounts`, `store.UnreadNotificationCount`, `store.MarkNotificationRead`, `store.MarkAllNotificationsRead` (Task 4); Phase-0 `Shell.Unread`; `auth.CurrentUser`; `withCSRF`.
- Produces: routes `GET /notifications`, `POST /notifications/read`, `POST /notifications/{id}/read`; handlers `notificationCentre`, `notificationsMarkAllRead`, `notificationMarkRead`; template `notifications`; PageData fields `Notifs []store.Notification`, `NotifCounts store.NotificationCounts`, `NotifScope string`.

**Access:** the centre needs an authenticated session only, no permission verb. Every row it can return is already scoped to `user_id = CurrentUser(r).ID` by the store, so there is nothing a permission could add and nothing a URL guess can reach.

`POST /notifications/{id}/read` is what the `.notif` link posts through before following its `href` — it is also the no-JS path, redirecting to the notification's own `href` on success.

- [ ] **Step 1: Write the failing test**

Create `internal/app/inapp_app_test.go`:
```go
package app

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func seedNotification(t *testing.T, s *appTestServer, userID int64, event, kind, title, href string) int64 {
	t.Helper()
	res, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO notifications(user_id,event,kind,title,body,href) VALUES(?,?,?,?,?,?)`,
		userID, event, kind, title, "detail line", href)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestNotificationCentreRendersFilterStripAndMarksRead(t *testing.T) { // G19
	s := newAppTestServer(t)
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	returned := seedNotification(t, s, admin.ID, "request_returned", "mention", "Kavita Rao returned PR-2026-000131", "/requests/1")
	seedNotification(t, s, admin.ID, "reminder_pending", "reminder", "PR-2026-000134 has waited 3 days", "/requests/2")
	seedNotification(t, s, admin.ID, "request_approved", "activity", "Kavita Rao approved PR-2026-000128", "/requests/3")
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/notifications", nil, ""))
	for _, want := range []string{
		"Kavita Rao returned PR-2026-000131", "PR-2026-000134 has waited 3 days",
		`class="segmented"`, `class="notif-list"`, `class="notif unread"`, `class="n-ico"`, `class="n-main"`,
		"All", "Unread", "Mentions", "Reminders",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("notification centre missing %q", want)
		}
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("templates must use .pill, never .badge (D5)")
	}
	// The bell badge in the shell reflects the unread count.
	if !strings.Contains(body, `data-unread="3"`) {
		t.Fatal("the shell must expose the unread count for the bell badge")
	}

	// The .segmented filters narrow the list.
	mentions := responseBody(t, s.request(http.MethodGet, "/notifications?scope=mentions", nil, ""))
	if !strings.Contains(mentions, "returned PR-2026-000131") || strings.Contains(mentions, "waited 3 days") {
		t.Fatal("scope=mentions must show only mention notifications")
	}

	// Marking one read redirects to that notification's target.
	resp := s.postForm("/notifications/"+itoa(returned)+"/read", url.Values{})
	requireStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != "/requests/1" {
		t.Fatalf("mark-read redirect = %q, want the notification's href", loc)
	}
	_ = responseBody(t, resp)
	if n, err := s.st.UnreadNotificationCount(s.ctx, admin.ID); err != nil || n != 2 {
		t.Fatalf("unread after one read = %d (err %v), want 2", n, err)
	}

	requireStatus(t, s.postForm("/notifications/read", url.Values{}), http.StatusSeeOther)
	if n, _ := s.st.UnreadNotificationCount(s.ctx, admin.ID); n != 0 {
		t.Fatalf("unread after mark-all = %d, want 0", n)
	}
}

func TestNotificationCentreNeverLeaksAnotherUsersRows(t *testing.T) { // G19
	s := newAppTestServer(t)
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hashForTest("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := s.st.CreateUser(s.ctx, "entry@corp.test", "Entry", hash, "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	adminOnly := seedNotification(t, s, admin.ID, "request_approved", "activity", "Admin only secret", "/requests/1")
	seedNotification(t, s, otherID, "request_approved", "activity", "Entry user item", "/requests/2")

	s.login("entry@corp.test", "EntryPassword123")
	body := responseBody(t, s.request(http.MethodGet, "/notifications", nil, ""))
	if strings.Contains(body, "Admin only secret") {
		t.Fatal("the centre must only ever render the signed-in user's notifications")
	}
	if !strings.Contains(body, "Entry user item") {
		t.Fatal("the signed-in user's own notification is missing")
	}
	// Guessing another user's id neither reads nor dismisses it.
	resp := s.postForm("/notifications/"+itoa(adminOnly)+"/read", url.Values{})
	requireStatus(t, resp, http.StatusNotFound)
	_ = responseBody(t, resp)
	if n, _ := s.st.UnreadNotificationCount(s.ctx, admin.ID); n != 1 {
		t.Fatal("another user's notification must remain unread")
	}
}
```
`itoa` and `hashForTest` are the existing app-test helpers (`strconv.FormatInt` wrapper and `auth.HashPassword`); if the harness names them differently, use the existing names rather than adding duplicates.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run 'TestNotificationCentreRendersFilterStripAndMarksRead|TestNotificationCentreNeverLeaksAnotherUsersRows' -v`
Expected: FAIL — `GET /notifications` returns 404 (no route) so every assertion fails.

- [ ] **Step 3: Write minimal implementation**

In `internal/app/app.go`, add to `PageData`:
```go
	Notifs      []store.Notification
	NotifCounts store.NotificationCounts
	NotifScope  string
```
and register:
```go
	mux.Handle("GET /notifications", a.auth.RequireLogin(http.HandlerFunc(a.notificationCentre)))
	mux.Handle("POST /notifications/read", a.auth.RequireLogin(http.HandlerFunc(a.withCSRF(a.notificationsMarkAllRead))))
	mux.Handle("POST /notifications/{id}/read", a.auth.RequireLogin(http.HandlerFunc(a.withCSRF(a.notificationMarkRead))))
```
(`RequireLogin` is the existing session-only wrapper every non-permission route already uses; if the codebase names it differently, use that name.)

Where Phase 0 builds the shell (`renderStatus`), populate the bell before rendering:
```go
	if u.ID != 0 && !isHXRequest(r) {
		if unread, err := a.st.UnreadNotificationCount(r.Context(), u.ID); err == nil {
			data.Shell.Unread = unread
		} else {
			a.log.ErrorContext(r.Context(), "unread count failed", "request_id", requestID(r), "error", err)
		}
	}
```
A failed count must never fail the page: the bell simply shows nothing.

Create `internal/app/inapp.go`:
```go
package app

import (
	"net/http"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

func (a *App) notificationCentre(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	scope := r.URL.Query().Get("scope")
	switch scope {
	case "", "all", "unread", "mentions", "reminders":
	default:
		scope = "all"
	}
	rows, err := a.st.ListNotifications(r.Context(), store.NotificationFilter{UserID: u.ID, Scope: scope})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	counts, err := a.st.NotificationCounts(r.Context(), u.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "notifications", PageData{Title: "Notifications", Notifs: rows, NotifCounts: counts, NotifScope: scope})
}

func (a *App) notificationsMarkAllRead(w http.ResponseWriter, r *http.Request) {
	if err := a.st.MarkAllNotificationsRead(r.Context(), auth.CurrentUser(r).ID, time.Now().UTC()); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/notifications", http.StatusSeeOther)
}

// notificationMarkRead marks one row read and forwards to whatever it points at,
// so a click works identically with and without JavaScript.
func (a *App) notificationMarkRead(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	id := parseID(r.PathValue("id"))
	target, err := a.notificationHref(r, u.ID, id)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if err := a.st.MarkNotificationRead(r.Context(), u.ID, id, time.Now().UTC()); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// notificationHref resolves the redirect target before the row is marked read.
// A row that is not this user's yields ErrNotFound, which respondStoreError maps
// to 404 — the same answer as a row that does not exist.
func (a *App) notificationHref(r *http.Request, userID, id int64) (string, error) {
	rows, err := a.st.ListNotifications(r.Context(), store.NotificationFilter{UserID: userID, Limit: 200})
	if err != nil {
		return "", err
	}
	for _, n := range rows {
		if n.ID == id {
			if n.Href != "" {
				return n.Href, nil
			}
			return "/notifications", nil
		}
	}
	return "", store.ErrNotFound
}
```

- [ ] **Step 4: Add the template**

In `internal/app/templates.go`, add the `notifications` template before the closing backtick:

```html
{{define "notifications"}}
{{template "top" .}}
<section class="page-banner d-only"><div><div class="eyebrow">In-app</div><h1>Notifications</h1><p class="sub">Notifications are in-app by default. Email is switched on per event by an administrator.</p></div><div class="pb-actions"><form method="post" action="/notifications/read"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="btn outline">Mark all as read</button></form></div></section>
<div class="segmented" style="margin-bottom:12px">
  <a class="{{if or (eq .NotifScope "") (eq .NotifScope "all")}}is-active{{end}}" href="/notifications?scope=all">All <span class="n">{{.NotifCounts.All}}</span></a>
  <a class="{{if eq .NotifScope "unread"}}is-active{{end}}" href="/notifications?scope=unread">Unread <span class="n">{{.NotifCounts.Unread}}</span></a>
  <a class="{{if eq .NotifScope "mentions"}}is-active{{end}}" href="/notifications?scope=mentions">Mentions <span class="n">{{.NotifCounts.Mentions}}</span></a>
  <a class="{{if eq .NotifScope "reminders"}}is-active{{end}}" href="/notifications?scope=reminders">Reminders <span class="n">{{.NotifCounts.Reminders}}</span></a>
</div>
<div class="card">
  <div class="notif-list">
  {{range .Notifs}}
    <form class="notif-form" method="post" action="/notifications/{{.ID}}/read"><input type="hidden" name="csrf" value="{{$.CSRF}}">
      <button class="notif{{if not .ReadAt}} unread{{end}}" type="submit">
        <span class="n-ico">{{notifIcon .Event}}</span>
        <span class="n-main"><b>{{.Title}}</b>{{if .Body}}<p>{{.Body}}</p>{{end}}</span>
        <time>{{.CreatedAt.Format "02 Jan, 15:04"}}</time>
      </button>
    </form>
  {{else}}
    <p class="empty">Nothing here yet. Notifications appear when a request you are involved in moves.</p>
  {{end}}
  </div>
</div>
{{template "bottom" .}}
{{end}}
```

The mockup renders each row as an `<a>`; a POST is required to mark the row read without JavaScript, so the row is a submit button styled by the same `.notif` rule. Add one line beside the existing component CSS **only if Phase 0 did not already neutralise button chrome** — check first; `.notif` is already a flex row and a `<button class="notif">` inherits it.

Add the icon helper to the template FuncMap next to `check`/`boolText`:
```go
	"notifIcon": func(event string) string {
		switch event {
		case "request_returned":
			return "↩"
		case "request_on_hold":
			return "⏸"
		case "request_rejected":
			return "✕"
		case "request_approved":
			return "✓"
		case "payment_settled":
			return "₹"
		case "payment_partial_review":
			return "◐"
		case "reminder_pending", "reminder_stale_reservation":
			return "✉"
		case "request_urgent":
			return "!"
		case "request_cancellation_requested":
			return "⊘"
		default:
			return "•"
		}
	},
```

In the Phase-0 `top` template, the bell already renders `Shell.Unread`; add `data-unread="{{.Shell.Unread}}"` to it so the count is assertable and available to the mobile top bar without a second query.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/app/ -run 'TestNotificationCentreRendersFilterStripAndMarksRead|TestNotificationCentreNeverLeaksAnotherUsersRows' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/inapp.go internal/app/app.go internal/app/templates.go internal/app/http_errors.go internal/app/inapp_app_test.go
git commit -m "feat(app): add the in-app notification centre and unread bell badge"
```

---

### Task 11: Admin rules screen `GET /admin/notifications` + App wiring + event hooks (D7)

**Files:**
- Create: `internal/app/notifications.go` (handlers)
- Modify: `internal/app/app.go` (App struct field, `New` builds `a.notify`, register routes, extend `PageData`)
- Modify: `internal/app/nav.go` (one `navSpec` entry in the Admin group)
- Modify: `internal/app/templates.go` (add the `admin_notifications` block with its `.overlay > .sheet` editor)
- Modify: Phase-2/3 request handlers to call `a.notify.Notify(...)` and `a.st.ResetReminder(...)` on edit
- Test: `internal/app/app_integration_test.go`

**Interfaces:**
- Consumes: `notify.NewService`, `notify.NewSMTPMailer`, `notify.AllEvents`, `notify.NotifyFieldNames`, `store.GetAppSettings`, `store.AllNotificationSettings`, `store.SetAppSettings`, `store.SetNotificationSetting`, `auth.RequirePermission("notification", …)`, `.Perms.Can` (Phase-1 nav), `withCSRF`.
- Produces: routes `GET /admin/notifications`, `POST /admin/notifications/smtp`, `POST /admin/notifications/events/{event}`, `POST /admin/notifications/test`; `App.notify *notify.Service`; `PageData.AppSettings store.AppSettings`, `PageData.Notifications []store.NotificationSetting`, `PageData.NotifyFields []string`.

**What this screen no longer does:** it does not render or write `require_attachments`. That toggle belongs to the Configuration screen's Attachments fieldset, which Phase 2 owns (D6), together with the T10 test that proves it drives submit enforcement. Nor does it render the reminder thresholds — Task 6 put those in Configuration.

Rendered from `mockups/screens/admin-notifications.html`: `table.t-cards` with six columns — Event · In-app · Email · Goes to · Fixed To / CC · Edit. **In-app** is a fixed `.pill.good.no-dot` "On" with no control behind it, because in-app delivery is not optional. **Email** is the only inline control. Everything else — include toggles, fixed To/CC, subject, message — moves into one `.overlay > .sheet` editor opened per row.

- [ ] **Step 1: Write the failing test**

Add to `internal/app/app_integration_test.go`:
```go
func TestAdminNotificationsScreenSavesAndBlocksUnprivileged(t *testing.T) { // D7, G20
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/admin/notifications", nil, ""))
	if !strings.Contains(body, "FERVID_SMTP_PASSWORD") {
		t.Fatal("the rules screen must document the env-only SMTP password")
	}
	if strings.Contains(body, `type="password"`) {
		t.Fatal("the rules screen must not render a password input")
	}
	// All twelve events are listed (G20).
	for _, event := range []string{
		"request_submitted", "request_edited", "request_returned", "request_rejected",
		"request_approved", "request_urgent", "request_on_hold", "request_cancellation_requested",
		"payment_settled", "payment_partial_review", "reminder_pending", "reminder_stale_reservation",
	} {
		if !strings.Contains(body, event) {
			t.Fatalf("rules screen missing event %q", event)
		}
	}
	// Design-system markup: t-cards grid, static In-app pill, sheet editor.
	for _, want := range []string{
		`class="t-cards"`, `class="pill good no-dot"`, `data-label="In-app"`, `data-label="Goes to"`,
		`data-label="Fixed To / CC"`, `class="overlay"`, `class="sheet"`, `class="sh-head"`,
		`class="sh-body stack-12"`, `class="sh-foot"`, `class="checkline"`, `class="hint"`,
		"Send a test email",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rules screen missing design-system markup %q", want)
		}
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("templates must use .pill, never .badge (D5)")
	}
	// require_attachments and the reminder thresholds moved to Configuration.
	for _, gone := range []string{`name="require_attachments"`, `name="reminder_pending_days"`} {
		if strings.Contains(body, gone) {
			t.Fatalf("%s belongs to the Configuration screen, not here", gone)
		}
	}
	// The available-field hint lists the token vocabulary (G20).
	for _, field := range []string{"number", "amount", "payee", "approver", "project", "head", "needed_by", "link"} {
		if !strings.Contains(body, field) {
			t.Fatalf("the sheet's field hint must list %q", field)
		}
	}
	assertTCardsLabelled(t, body)

	smtp := url.Values{"smtp_host": {"smtp.corp.test"}, "smtp_port": {"2525"}, "smtp_from_addr": {"no@corp.test"},
		"management_recipients": {"board@corp.test"}, "base_url": {"https://budget.corp.test/"}}
	resp := s.postForm("/admin/notifications/smtp", smtp)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	app, err := s.st.GetAppSettings(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if app.SMTPHost != "smtp.corp.test" || app.SMTPPort != 2525 || app.BaseURL != "https://budget.corp.test" {
		t.Fatalf("SMTP settings not saved: %#v", app)
	}

	evt := url.Values{"email_enabled": {"on"}, "include_requester": {"on"}, "include_accounts": {"on"},
		"to_recipients": {"ops@corp.test"}, "subject_template": {"Approved {{number}}"}, "body_template": {"done"}}
	resp = s.postForm("/admin/notifications/events/request_approved", evt)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	cfg, err := s.st.NotificationSetting(s.ctx, "request_approved")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.EmailEnabled || !cfg.IncludeRequester || cfg.ToRecipients != "ops@corp.test" {
		t.Fatalf("event settings not saved: %#v", cfg)
	}
	// A template with an unknown token is rejected at save time, not at send time.
	bad := url.Values{"email_enabled": {"on"}, "subject_template": {"Approved {{nmuber}}"}, "body_template": {"done"}}
	if resp := s.postForm("/admin/notifications/events/request_approved", bad); resp.StatusCode == http.StatusSeeOther {
		t.Fatal("an unknown template field must be rejected on save")
	}

	// No app_settings row may hold a password.
	rows, err := s.st.DB().QueryContext(s.ctx, `SELECT key FROM app_settings`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(k), "password") {
			t.Fatalf("app_settings persisted a password key %q", k)
		}
	}

	// RBAC: a non-notification user is blocked by URL.
	hash, err := auth.HashPassword("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.CreateUser(s.ctx, "entry@corp.test", "Entry", hash, "data_entry", true); err != nil {
		t.Fatal(err)
	}
	s.login("entry@corp.test", "EntryPassword123")
	for _, tc := range []struct {
		method, path string
		form         url.Values
	}{
		{http.MethodGet, "/admin/notifications", nil},
		{http.MethodPost, "/admin/notifications/smtp", url.Values{"smtp_host": {"x"}}},
		{http.MethodPost, "/admin/notifications/events/request_approved", url.Values{"email_enabled": {"on"}}},
		{http.MethodPost, "/admin/notifications/test", url.Values{"event": {"request_approved"}}},
	} {
		var resp *http.Response
		if tc.method == http.MethodGet {
			resp = s.request(http.MethodGet, tc.path, nil, "")
		} else {
			resp = s.postForm(tc.path, tc.form)
		}
		requireStatus(t, resp, http.StatusForbidden)
		_ = responseBody(t, resp)
	}
	// …but the user's own centre is still theirs.
	requireStatus(t, s.request(http.MethodGet, "/notifications", nil, ""), http.StatusOK)
}

func TestAdminNotificationsSendTestEmail(t *testing.T) { // D7
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm("/admin/notifications/smtp", url.Values{
		"smtp_host": {"smtp.corp.test"}, "smtp_port": {"2525"}, "smtp_from_addr": {"no@corp.test"},
	}), http.StatusSeeOther)

	// With no reachable SMTP server the send fails, and the screen must say so
	// rather than silently claiming success.
	resp := s.postForm("/admin/notifications/test", url.Values{"event": {"request_approved"}, "to": {"admin@corp.test"}})
	if resp.StatusCode == http.StatusOK {
		t.Fatal("a failed test send must not render as a success page")
	}
	_ = responseBody(t, resp)
}
```

`assertTCardsLabelled` is the helper Phase 4 added to `internal/app/recoverables_app_test.go`; both files are in package `app`, so it is reused, not redefined.

**Moved test:** `TestRequireAttachmentsTogglePersistsAndDrivesSubmitEnforcement` is **deleted from this plan** and belongs to Phase 2's Configuration Attachments fieldset. Do not re-add it here; do not delete it outright — if Phase 2's plan has not yet claimed it, carry the test body across when landing that fieldset.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run 'TestAdminNotificationsScreenSavesAndBlocksUnprivileged|TestAdminNotificationsSendTestEmail' -v`
Expected: FAIL — `GET /admin/notifications` returns 404/redirect (route not registered) so `requireStatus` fails, or build error `PageData has no field AppSettings`.

- [ ] **Step 3: Write minimal implementation**

In `internal/app/app.go`:
- Add import `"fervidbudget/internal/notify"`.
- Add field to `App`: `notify *notify.Service`.
- In `New`, after `a := &App{...}`, set:
```go
	a.notify = notify.NewService(st, notify.NewSMTPMailer(st, cfg.SMTPPassword))
```
- Add to `PageData`:
```go
	AppSettings   store.AppSettings
	Notifications []store.NotificationSetting
	NotifyFields  []string
```
- In `routes`, register:
```go
	mux.Handle("GET /admin/notifications", a.auth.RequirePermission("notification", "view", http.HandlerFunc(a.adminNotifications)))
	mux.Handle("POST /admin/notifications/smtp", a.auth.RequirePermission("notification", "edit", http.HandlerFunc(a.withCSRF(a.notificationsSMTPSave))))
	mux.Handle("POST /admin/notifications/events/{event}", a.auth.RequirePermission("notification", "edit", http.HandlerFunc(a.withCSRF(a.notificationEventSave))))
	mux.Handle("POST /admin/notifications/test", a.auth.RequirePermission("notification", "edit", http.HandlerFunc(a.withCSRF(a.notificationSendTest))))
```
There is **no** `GET /notifications` here — that path is Task 10's user-facing centre (D7). If the earlier draft of this plan was already executed, move all four registrations under `/admin/` and delete the old ones; the two screens must not share a path.

Create `internal/app/notifications.go`:
```go
package app

import (
	"net/http"
	"strconv"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

func (a *App) adminNotifications(w http.ResponseWriter, r *http.Request) {
	app, err := a.st.GetAppSettings(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	events, err := a.st.AllNotificationSettings(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "admin_notifications", PageData{
		Title: "Notification rules", AppSettings: app, Notifications: events,
		NotifyFields: notify.NotifyFieldNames(),
	})
}

func (a *App) notificationsSMTPSave(w http.ResponseWriter, r *http.Request) {
	port, _ := strconv.Atoi(r.FormValue("smtp_port"))
	in := store.AppSettings{
		SMTPHost:             r.FormValue("smtp_host"),
		SMTPPort:             port,
		SMTPUsername:         r.FormValue("smtp_username"),
		SMTPFromName:         r.FormValue("smtp_from_name"),
		SMTPFromAddr:         r.FormValue("smtp_from_addr"),
		ManagementRecipients: r.FormValue("management_recipients"),
		BaseURL:              r.FormValue("base_url"),
	}
	// require_attachments is deliberately absent: Phase 2's Configuration screen
	// owns that key and its Attachments fieldset (D6).
	if err := a.st.SetAppSettings(r.Context(), auth.CurrentUser(r), in); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/notifications", http.StatusSeeOther)
}

func (a *App) notificationEventSave(w http.ResponseWriter, r *http.Request) {
	in := store.NotificationSetting{
		Event:            r.PathValue("event"),
		EmailEnabled:     r.FormValue("email_enabled") == "on",
		ToRecipients:     r.FormValue("to_recipients"),
		CcRecipients:     r.FormValue("cc_recipients"),
		IncludeRequester: r.FormValue("include_requester") == "on",
		IncludeManager:   r.FormValue("include_manager") == "on",
		IncludeAccounts:  r.FormValue("include_accounts") == "on",
		SubjectTemplate:  r.FormValue("subject_template"),
		BodyTemplate:     r.FormValue("body_template"),
	}
	// Validate the placeholders before persisting: a typo caught here is a form
	// error, a typo caught at send time is a broken email nobody sees.
	if err := notify.ValidateTemplates(in.SubjectTemplate, in.BodyTemplate); err != nil {
		a.respondError(w, r, http.StatusBadRequest, "That message uses a field name that does not exist. Check the list of available fields.", err)
		return
	}
	if err := a.st.SetNotificationSetting(r.Context(), auth.CurrentUser(r), in); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/notifications", http.StatusSeeOther)
}

// notificationSendTest renders one event's templates against a sample request and
// sends it to the address given, so an administrator can prove SMTP works without
// waiting for a real approval.
func (a *App) notificationSendTest(w http.ResponseWriter, r *http.Request) {
	to := r.FormValue("to")
	if to == "" {
		to = auth.CurrentUser(r).Email
	}
	if err := a.notify.SendTest(r.Context(), r.FormValue("event"), to); err != nil {
		a.respondError(w, r, http.StatusBadGateway, "The test email could not be sent. Check the SMTP settings and FERVID_SMTP_PASSWORD.", err)
		return
	}
	http.Redirect(w, r, "/admin/notifications?sent=1", http.StatusSeeOther)
}
```

Add the two small helpers this handler needs to `internal/notify/service.go`:
```go
// ValidateTemplates reports the first unknown {{token}} in either template. The
// admin screen calls it before saving.
func ValidateTemplates(subject, body string) error {
	sample := RequestView{}
	if _, err := renderTemplate(subject, sample); err != nil {
		return err
	}
	_, err := renderTemplate(body, sample)
	return err
}

// SendTest renders one event against a sample request and mails it to addr.
func (s *Service) SendTest(ctx context.Context, event, addr string) error {
	cfg, err := s.st.NotificationSetting(ctx, event)
	if err != nil {
		return err
	}
	app, err := s.st.GetAppSettings(ctx)
	if err != nil {
		return err
	}
	v := RequestView{
		Number: "PR-0000-000000", Amount: 10000000, ApprovedAmount: 10000000,
		Payee: "Sample Vendor Pvt Ltd", RequesterName: "Sample Requester", ManagerName: "Sample Approver",
		Project: "Sample Project", Head: "Sample Head", Purpose: "Test message", Status: "approved",
		NeededBy: "2026-12-31", SubmittedOn: "2026-12-01", ProcessingOn: "2026-12-02",
		Link: app.BaseURL + "/requests/0",
	}
	subject, err := renderTemplate(cfg.SubjectTemplate, v)
	if err != nil {
		return err
	}
	body, err := renderTemplate(cfg.BodyTemplate, v)
	if err != nil {
		return err
	}
	return s.mailer.Send(ctx, Message{From: formatFrom(app), To: []string{addr}, Subject: "[TEST] " + subject, Body: body})
}
```

- [ ] **Step 3b: Add the template**

In `internal/app/templates.go`, append an `"admin_notifications"` block before the closing backtick:
```html
{{define "admin_notifications"}}
{{template "top" .}}
<section class="page-banner d-only"><div><div class="eyebrow">Administration</div><h1>Notification rules</h1><p class="sub">In-app notifications always fire. Email is configured per event.</p></div><div class="pb-actions"><button class="btn outline" form="test-send">Send a test email</button></div></section>
<form class="toolbar" method="post" action="/admin/notifications/smtp"><input type="hidden" name="csrf" value="{{.CSRF}}">
  <div class="field"><label for="smtp_host">SMTP host</label><input id="smtp_host" name="smtp_host" value="{{.AppSettings.SMTPHost}}"></div>
  <div class="field"><label for="smtp_port">Port</label><input id="smtp_port" name="smtp_port" type="number" value="{{.AppSettings.SMTPPort}}"></div>
  <div class="field"><label for="smtp_username">Username</label><input id="smtp_username" name="smtp_username" value="{{.AppSettings.SMTPUsername}}"></div>
  <div class="field"><label for="smtp_from_name">From name</label><input id="smtp_from_name" name="smtp_from_name" value="{{.AppSettings.SMTPFromName}}"></div>
  <div class="field"><label for="smtp_from_addr">From address</label><input id="smtp_from_addr" name="smtp_from_addr" type="email" value="{{.AppSettings.SMTPFromAddr}}"></div>
  <div class="field"><label for="mgmt">Management recipients</label><input id="mgmt" name="management_recipients" value="{{.AppSettings.ManagementRecipients}}" placeholder="comma separated"></div>
  <div class="field"><label for="base_url">Link base URL</label><input id="base_url" name="base_url" value="{{.AppSettings.BaseURL}}" placeholder="https://budget.example.com"><span class="hint">Used to build the {{"{{link}}"}} in every email.</span></div>
  <span class="row-end"></span><button class="btn primary">Save delivery settings</button>
</form>
<p class="hint">The password is read from <code>FERVID_SMTP_PASSWORD</code>; it is never stored, shown, or written to the database.</p>
<form id="test-send" method="post" action="/admin/notifications/test"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="event" value="request_approved"></form>
<div class="table-wrap"><table class="t-cards">
<thead><tr><th>Event</th><th class="c">In-app</th><th class="c">Email</th><th>Goes to</th><th>Fixed To / CC</th><th class="c">Edit</th></tr></thead>
<tbody>{{range .Notifications}}<tr>
  <td class="t-lead" data-label="Event">{{.Label}}<span class="hint"> · {{.Event}}</span></td>
  <td class="c" data-label="In-app"><span class="pill good no-dot">On</span></td>
  <td class="c" data-label="Email"><input form="evt-{{.Event}}" type="checkbox" name="email_enabled" {{check .EmailEnabled}}></td>
  <td data-label="Goes to">{{.Audience}}</td>
  <td data-label="Fixed To / CC">{{if or .ToRecipients .CcRecipients}}{{.ToRecipients}}{{if .CcRecipients}} · cc {{.CcRecipients}}{{end}}{{else}}—{{end}}</td>
  <td class="c" data-label="Edit"><button class="btn small outline" type="button" data-open="tpl-{{.Event}}">Edit</button></td>
</tr>{{else}}<tr><td colspan="6" class="empty" data-label="">No notification events.</td></tr>{{end}}</tbody>
</table></div>

{{range .Notifications}}
<div class="overlay" id="tpl-{{.Event}}" hidden>
  <form class="sheet" id="evt-{{.Event}}" method="post" action="/admin/notifications/events/{{.Event}}">
    <input type="hidden" name="csrf" value="{{$.CSRF}}">
    <div class="sh-head"><div><h2>{{.Label}}</h2><p class="sh-sub">{{.Audience}}</p></div><button class="sh-close" type="button" data-close="tpl-{{.Event}}">✕</button></div>
    <div class="sh-body stack-12">
      <label class="checkline"><input type="checkbox" name="email_enabled" {{check .EmailEnabled}}> Send this email</label>
      <div class="field"><span class="flabel">Include</span>
        <div class="stack-8" style="margin-top:5px">
          <label class="checkline"><input type="checkbox" name="include_requester" {{check .IncludeRequester}}> The requester</label>
          <label class="checkline"><input type="checkbox" name="include_manager" {{check .IncludeManager}}> The approver</label>
          <label class="checkline"><input type="checkbox" name="include_accounts" {{check .IncludeAccounts}}> The Accounts group</label>
        </div>
      </div>
      <div class="field"><label for="to-{{.Event}}">Fixed To</label><input id="to-{{.Event}}" name="to_recipients" value="{{.ToRecipients}}"></div>
      <div class="field"><label for="cc-{{.Event}}">Fixed CC</label><input id="cc-{{.Event}}" name="cc_recipients" value="{{.CcRecipients}}"></div>
      <div class="field"><label for="subj-{{.Event}}">Subject</label><input id="subj-{{.Event}}" name="subject_template" value="{{.SubjectTemplate}}"></div>
      <div class="field"><label for="body-{{.Event}}">Message</label>
        <textarea id="body-{{.Event}}" name="body_template" rows="8">{{.BodyTemplate}}</textarea>
        <span class="hint">Available fields: {{range $i, $f := $.NotifyFields}}{{if $i}}, {{end}}{{$f}}{{end}}. Write them as {{"{{number}}"}}.</span>
      </div>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="tpl-{{.Event}}">Cancel</button><span class="row-end"></span><button class="btn" type="submit" formaction="/admin/notifications/test" name="event" value="{{.Event}}">Send test</button><button class="btn primary" type="submit">Save rule</button></div>
  </form>
</div>
{{end}}
{{template "bottom" .}}
{{end}}
```

The two `email_enabled` checkboxes (inline row and sheet) share one `evt-{{.Event}}` form via the HTML `form` attribute, so the row control and the sheet control are the same input — they can never disagree. The Phase-0 overlay JS handles `data-open` / `data-close` / Esc / focus trap; no new script is added.

- [ ] **Step 3c: Nav entry and the event hooks**

In `internal/app/nav.go`, add one entry to `navSpec`'s Admin group:
```go
	{Key: "notif-admin", Label: "Notification rules", Href: "/admin/notifications", Icon: "✉", Resource: "notification", Action: "view"},
```
The user's centre is reached from the shell bell, not from a nav item, so it adds no `navSpec` entry.

Finally, wire the event hooks in the Phase-2/3 request handlers (place each call immediately after the successful store mutation, fire-and-forget with the error logged via `a.log` — a mail failure must never roll back a workflow transition):
```go
	// after CreateRequest (create-and-submit, D1) succeeds:
	a.fireNotify(r, notify.EventRequestSubmitted, req)
	if req.Urgent {
		a.fireNotify(r, notify.EventRequestUrgent, req)
	}
	// after an edit-while-pending UpdateRequest succeeds:
	a.fireNotify(r, notify.EventRequestEdited, req)
	_ = a.st.ResetReminder(r.Context(), req.ID)
	// after ReturnRequest / RejectRequest succeed:
	a.fireNotify(r, notify.EventRequestReturned, req)   // or EventRequestRejected
	// after ApproveRequest succeeds:
	a.fireNotify(r, notify.EventRequestApproved, req)
	if req.Urgent {
		a.fireNotify(r, notify.EventRequestUrgent, req) // req.Status is now approved
	}
	// after HoldRequest succeeds:
	a.fireNotify(r, notify.EventRequestOnHold, req)
	// after RequestCancellation (G1) succeeds:
	a.fireNotify(r, notify.EventCancellationRequested, req)
	// after RecordPaymentForRequest succeeds, by settlement:
	a.fireNotify(r, notify.EventPaymentSettled, req)        // settlement == "settled"
	a.fireNotify(r, notify.EventPaymentPartialReview, req)  // settlement == "partial"
```
with one helper in `internal/app/notifications.go`:
```go
// fireNotify delivers a notification without letting a delivery failure affect the
// HTTP result. The in-app row and the email are both best-effort at this point —
// the workflow transition has already committed.
func (a *App) fireNotify(r *http.Request, event string, req store.Request) {
	if err := a.notify.Notify(r.Context(), event, req); err != nil {
		a.log.ErrorContext(r.Context(), "notification failed", "event", event, "request_id", requestID(r), "error", err)
	}
}
```
(These hook lines land in the Phase-2/3 handlers once those exist; if executing Phase 5 before those handlers are merged, add them at that time — the rules screen, the centre, the store and the `notify` package are independently complete and tested.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run 'TestAdminNotificationsScreenSavesAndBlocksUnprivileged|TestAdminNotificationsSendTestEmail|TestConfigurationRemindersFieldsetDrivesThresholds' -v`
Expected: PASS — including Task 6's test, whose final assertion needed this route.

- [ ] **Step 5: Commit**

```bash
git add internal/app/notifications.go internal/app/app.go internal/app/nav.go internal/app/templates.go internal/notify/service.go internal/app/app_integration_test.go
git commit -m "feat(app): notification rules screen at /admin/notifications with sheet editor"
```

---

### Task 12: cmd/server — scheduler goroutine wired to graceful shutdown

**Files:**
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: `notify.NewService`, `notify.NewSMTPMailer`, `config.SMTPPassword`, the existing `signal.NotifyContext` `ctx`, `db`.
- Produces: a background reminder scheduler that stops when `ctx` is cancelled (SIGINT/SIGTERM).

- [ ] **Step 1: Write the failing test**

Scheduler start/stop behaviour is already unit-tested in Task 9 (`TestSchedulerRunsOnStartAndStopsOnCancel`). This task is verified by build + vet (main wiring is not independently unit-testable). Confirm the current baseline builds first:

Run: `go build ./cmd/server`
Expected: PASS (baseline builds; the goroutine is not yet wired).

- [ ] **Step 2: Add the wiring**

In `cmd/server/main.go`, add the import `"fervidbudget/internal/notify"`. After `ctx, stop := signal.NotifyContext(...)` and `defer stop()` (and before the serve goroutine), insert:
```go
	notifier := notify.NewService(db, notify.NewSMTPMailer(db, cfg.SMTPPassword))
	go notifier.Scheduler(ctx, 24*time.Hour, func() time.Time { return time.Now().UTC() })
```

- [ ] **Step 3: Verify build, vet, and the full suite**

Run: `go build ./cmd/server && go vet ./...`
Expected: PASS (no output).

Run: `go test ./internal/notify/ ./internal/store/ ./internal/config/ ./internal/app/ -count=1`
Expected: `ok` for each package.

- [ ] **Step 4: Run the race + coverage gates**

Run: `make test-race`
Expected: PASS (all packages `ok`, no race reports).

Run: `make test-cover`
Expected: PASS (coverage profile written to `output/coverage.out`).

- [ ] **Step 5: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): start reminder scheduler goroutine bound to shutdown context"
```

---

## Coverage

| ID | Requirement | Task | Test name |
|---|---|---|---|
| N1 | In-app always fires; email is the additive opt-in layer on top | Task 8 | `TestNotifyDisabledEventStillWritesInAppButSendsNoEmail` |
| N2 | Email per-event configurable (enable/To/CC/include flags/subject/body) | Task 3 · Task 11 | `TestNotificationSettingUpsertAndFetch` · `TestAdminNotificationsScreenSavesAndBlocksUnprivileged` |
| N3 | Approval email → Accounts + requester + management list | Task 8 | `TestNotifyApprovedResolvesAccountsRequesterAndManagement` |
| N4 | Manager reminder after the configured wait, then on the configured cadence | Task 9 | `TestRunRemindersEmailsManagerAfterThreeCalendarDays` · `TestRunRemindersHonoursConfiguredThresholds` |
| N5 | Stale-reservation nudge after the configured wait | Task 9 | `TestRunRemindersNudgesStaleProcessing` |
| N6 | Urgent → immediate approver email (pre) + accounts/mgmt (post); no authority | Task 8 | `TestNotifyUrgentSubmitEmailsManagerAndConfersNoApprovalAuthority` · `TestNotifyApprovedUrgentEmailsAccountsAndManagement` |
| N8 | SMTP settings + management list configurable; password from env only | Task 1 · Task 3 · Task 7 · Task 11 | `TestSMTPPasswordLoadsFromEnvOnly` · `TestAppSettingsRoundTripAndNeverStoresPassword` · `TestSMTPMailerBuildsMessageAndUsesEnvPassword` · `TestAdminNotificationsScreenSavesAndBlocksUnprivileged` |
| S8 | Stale-reservation reminder after the configured wait | Task 5 · Task 9 | `TestRequestsStaleProcessingHonoursConfiguredThreshold` · `TestRunRemindersNudgesStaleProcessing` |
| A8 | Edit-while-pending re-notifies approver + resets reminder | Task 8 · Task 5 | `TestNotifyRequestEditedEmailsManager` · `TestResetReminderClearsThrottle` |
| **G19** | In-app notification centre + unread bell badge | Task 4 · Task 8 · Task 10 | `TestNotificationLifecycleAndScopedFilters` · `TestNotifyInAppKindsDriveTheFilterStrip` · `TestNotificationCentreRendersFilterStripAndMarksRead` · `TestNotificationCentreNeverLeaksAnotherUsersRows` |
| **G20** | Twelve seeded events and the `{{token}}` vocabulary | Task 2 · Task 8 · Task 11 | `TestMigrationV6CreatesNotificationTablesAndSeedsTwelveEvents` · `TestRenderTemplateSubstitutesTheTokenVocabulary` · `TestNotifyRejectsAnInvalidTemplateInsteadOfSendingEmptyText` · `TestAdminNotificationsScreenSavesAndBlocksUnprivileged` |
| **D7** | `/notifications` is the user's centre; the rules screen is `/admin/notifications` | Task 10 · Task 11 | `TestNotificationCentreRendersFilterStripAndMarksRead` · `TestAdminNotificationsScreenSavesAndBlocksUnprivileged` (its final assertion: an unprivileged user is 403 on `/admin/notifications` and 200 on `/notifications`) |
| **D6** | Reminder thresholds live in Configuration, not on the rules screen | Task 6 | `TestConfigurationRemindersFieldsetDrivesThresholds` |
| ~~T10~~ | **Moved to Phase 2.** Attachments optional by default with an `app_settings`-backed admin toggle now belongs to the Configuration screen's Attachments fieldset (D6). Phase 5 renders no such toggle and writes no such key. | Phase 2 | `TestRequireAttachmentsTogglePersistsAndDrivesSubmitEnforcement` (carried across with the fieldset) |

Supporting coverage (infrastructure the above depend on): `notification_settings` + `notifications` tables and the twelve-event seed (`v6`; `app_settings` is Phase 2's `v3`, not re-created here) — Task 2 `TestMigrationV6CreatesNotificationTablesAndSeedsTwelveEvents`; in-app validation — Task 4 `TestAddNotificationRequiresUserAndTitle`; accounts expansion — Task 3 `TestUsersWithPermissionResolvesAccounts`; calendar-day math, configurable thresholds and the repeat throttle — Task 5 `TestCalendarDaysBetweenCountsDateBoundaries` / `TestReminderThresholdsDefaultsAndOverrides` / `TestRequestsPendingReminderHonoursConfiguredThreshold`; SMTP host guard — Task 7 `TestSMTPMailerRequiresHost`; management-Cc and urgent status branching — Task 8 `TestResolveRecipientsManagementCcOnlyOnApprovalAndDedupes`; test-email path — Task 11 `TestAdminNotificationsSendTestEmail`; scheduler start/stop — Task 9 `TestSchedulerRunsOnStartAndStopsOnCancel`.

---

## Final verification

- [ ] **Design-system conformance**

Run: `grep -c 'class="badge' internal/app/templates.go`
Expected: `0` (D5).

Run: `grep -c '"GET /notifications"' internal/app/app.go`
Expected: `1` — the user's centre. The rules screen must appear only as `"GET /admin/notifications"` (D7).

Run: `grep -c 'require_attachments' internal/app/notifications.go`
Expected: `0` — the key belongs to Phase 2's Configuration screen.

Run: `git diff --stat -- web/static/fervid-ds.css`
Expected: empty — Phase 5 adds no CSS.

- [ ] **Full suite, race and coverage**

Run: `make test-race`
Expected: PASS (all packages `ok`, no race reports).

Run: `make test-cover`
Expected: PASS (coverage profile written to `output/coverage.out`).

Run: `make vet`
Expected: no output (clean).
</content>
