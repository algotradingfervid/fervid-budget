# Fervid Budget vs Actuals — Design Spec

**Date:** 2026-06-29
**Status:** Draft for review
**Author:** brainstormed with Narendh (Fervid Smart Solutions)

---

## 1. Problem

Fervid Smart Solutions tracks a monthly cash forecast — budget vs actual spend, per project
and per "head" (line item) — in an Excel workbook (`Forcast reports.xlsx`). The workbook is
fragile: the `Budget − Actual` column is full of `#REF!` errors, the daily actuals are mostly
unfilled, line-item numbering is inconsistent, and several heads are untagged. It cannot be
trusted or maintained.

We will replace it with a small, reliable web application that lets the accounts team record
payments and instantly see budget-vs-actual variance — with the variance math computed live
(never a stored formula that can break).

## 2. Goals & non-goals

**Goals**
- Record payments by **project → head → date → amount**, with full payment details.
- Capture payment metadata: vendor/payee, payment mode, bill/invoice number, reference number,
  attachment/photo, and optional remarks.
- Define **budgets per head, per month** (monthly cycles); compare actual vs budget and show
  variance at head, project, and company level.
- Maintain a clear **transaction audit trail** for every payment: when it was added, edited,
  voided/deleted, what changed, and which user performed the action.
- Support **month close/lock** after review so prior-month budgets and payments cannot be changed
  accidentally.
- A single dense **Variance Grid** as the home screen (the spreadsheet's clean replacement).
- Monthly, project-wise, head-wise, and multi-month/YTD reports with CSV export.
- **Per-user accounts with roles** (admin / data-entry) and an **audit trail** of all changes.
- Indian-rupee presentation (₹, lakh/crore). Ship as one binary + one SQLite file.

**Non-goals (this version)**
- Income / sales tracking and Net Income (the Excel's income section). Expenses only.
- The other dashboard concepts explored (Executive Cockpit, Card Grid, Ledger Explorer,
  Analytics Studio). They are designed on the same system and can be added later as screens.
- Due-date reminders/alerts (due day is stored as **reference only**).
- Multi-company / multi-tenant. Single company.
- Email-based flows (password self-reset, email confirmation) — see §8.

## 3. Users & roles

| Role | Can do |
|------|--------|
| **admin** | Everything: manage users, projects, heads, **set budgets**, record/edit payments, export, view audit log. |
| **data_entry** | Record payments; view & export the Variance Grid. Cannot edit budgets, setup, or users. |

Roles are enforced with **Casbin** (RBAC). The role→permission mapping is a static Casbin
model+policy; each user's role is stored on the user record and supplied as the Casbin subject.
Admins assign roles through the Users screen. (Upgrade path: move policy into a Casbin
SQL adapter if dynamic, fine-grained permissions are ever needed.)

## 4. Data model (SQLite)

```sql
-- Users (managed by Authboss; password_hash is bcrypt via Authboss)
CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  role          TEXT NOT NULL DEFAULT 'data_entry',   -- 'admin' | 'data_entry'
  active        INTEGER NOT NULL DEFAULT 1,
  -- Authboss lock module fields:
  attempt_count INTEGER NOT NULL DEFAULT 0,
  last_attempt  DATETIME,
  locked        DATETIME,
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE projects (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  active      INTEGER NOT NULL DEFAULT 1,
  sort_order  INTEGER NOT NULL DEFAULT 0,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE heads (
  id          INTEGER PRIMARY KEY,
  project_id  INTEGER NOT NULL REFERENCES projects(id),
  name        TEXT NOT NULL,
  due_day     TEXT,                          -- free text reference, e.g. "5th", "5th & 25th"
  active      INTEGER NOT NULL DEFAULT 1,
  sort_order  INTEGER NOT NULL DEFAULT 0,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(project_id, name)
);

CREATE TABLE budgets (
  id          INTEGER PRIMARY KEY,
  head_id     INTEGER NOT NULL REFERENCES heads(id),
  month       TEXT NOT NULL,                 -- 'YYYY-MM'
  amount      INTEGER NOT NULL,              -- paise (integer money; avoid float)
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(head_id, month)
);

CREATE TABLE payments (
  id          INTEGER PRIMARY KEY,
  head_id     INTEGER NOT NULL REFERENCES heads(id),
  paid_on     TEXT NOT NULL,                 -- 'YYYY-MM-DD'; month derived from this
  amount      INTEGER NOT NULL,              -- paise
  vendor_payee TEXT,                         -- vendor, staff member, bank, payee, etc.
  payment_mode TEXT,                         -- cash | bank_transfer | cheque | card | upi | other
  invoice_no  TEXT,
  reference_no TEXT,                         -- bank UTR, cheque no., payment ref, etc.
  remarks     TEXT,
  entered_by  INTEGER NOT NULL REFERENCES users(id),
  updated_by  INTEGER REFERENCES users(id),  -- last user to edit this payment, if any
  voided_by   INTEGER REFERENCES users(id),  -- user who voided/deleted it, if any
  void_reason TEXT,
  voided_at   DATETIME,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE payment_attachments (
  id            INTEGER PRIMARY KEY,
  payment_id    INTEGER NOT NULL REFERENCES payments(id),
  original_name TEXT NOT NULL,
  stored_path   TEXT NOT NULL,               -- local path under configured attachment directory
  mime_type     TEXT,
  size_bytes    INTEGER NOT NULL,
  uploaded_by   INTEGER NOT NULL REFERENCES users(id),
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE month_locks (
  month       TEXT PRIMARY KEY,              -- 'YYYY-MM'
  locked_by   INTEGER NOT NULL REFERENCES users(id),
  locked_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  reason      TEXT
);

CREATE TABLE audit_log (
  id           INTEGER PRIMARY KEY,
  actor_id     INTEGER REFERENCES users(id),
  actor_name   TEXT,                         -- denormalised so log survives user deletion
  action       TEXT NOT NULL,                -- 'create' | 'update' | 'void' | 'delete' | 'lock' | 'unlock' | 'login' | 'logout' | 'export'
  entity_type  TEXT,                         -- 'payment' | 'budget' | 'project' | 'head' | 'user' | 'month_lock'
  entity_id    INTEGER,
  summary      TEXT,                         -- human-readable, e.g. "Recorded ₹15,70,000 to Head Office / Salaries"
  before_json  TEXT,                         -- JSON snapshot before change (null for create)
  after_json   TEXT,                         -- JSON snapshot after change (null for delete)
  ip           TEXT,
  created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Money is stored as integer paise** to avoid floating-point drift (the spreadsheet's other
sin). Presentation converts to ₹ with Indian grouping and lakh/crore short forms.

**Variance is always computed, never stored:**
```
actual(head, month)   = Σ payments.amount WHERE head_id = ? AND substr(paid_on,1,7) = month
                        AND voided_at IS NULL
variance(head, month) = budget.amount − actual(head, month)        -- positive = under budget (good)
```
Project and company figures are sums of their heads. A single SQL query with `GROUP BY` powers
the whole grid.

**Payment transaction audit trail**

Every payment mutation writes an `audit_log` row with `entity_type='payment'` and
`entity_id=<payment id>`. The payment detail/audit view must show a chronological history:

- **Added:** created timestamp, entered-by user, project/head/date/amount/remarks.
- **Edited:** edited timestamp, edited-by user, field-level before/after values from
  `before_json` and `after_json`.
- **Voided/deleted:** void timestamp, voided-by user, reason, and full prior payment snapshot.

Payment rows are soft-voided for traceability (`voided_at` set) rather than physically removed
from normal application flows. Actual totals exclude voided payments, but the audit trail and
admin audit screen still show them.

**Month close/lock**

After the month is reviewed, an admin can close/lock it. A locked month blocks create/edit/void
operations for:

- Budgets where `budgets.month` is locked.
- Payments where `substr(payments.paid_on,1,7)` is locked.

Admins may unlock a month only with a reason, and both lock and unlock actions are audited. The UI
must show a clear locked status on the grid, budget editor, payment form, and payment detail page.
Exports remain available for locked months.

**Validation rules**

- Amounts must be positive integer paise; zero or negative payments/budgets are rejected unless a
  future credit/refund workflow is explicitly added.
- Dates are entered and displayed in the office business timezone. `paid_on` must be a valid
  `YYYY-MM-DD` date; month filters use `YYYY-MM`.
- New payments cannot be posted to inactive projects or inactive heads.
- Project names are unique; head names are unique within a project.
- If budget is zero, variance amount is still shown, but variance percent displays as `N/A` to
  avoid divide-by-zero or misleading infinity values.
- A locked month rejects budget/payment mutations before any database write and returns a clear
  user-facing error.

## 5. Architecture & stack (all latest, pinned)

| Layer | Choice | Version |
|-------|--------|---------|
| Language | Go | **1.26.4** |
| HTTP | stdlib `net/http` (1.22+ method/path routing) — no framework | — |
| DB driver | `modernc.org/sqlite` (pure Go, **no cgo**) via `database/sql` | **v1.53.0** |
| Auth / users | `github.com/aarondl/authboss/v3` (modules: `auth`, `lock`) | **v3.5.3** |
| Session state | `github.com/aarondl/authboss-clientstate` (encrypted cookie store — no session table) | latest |
| Passwords | `golang.org/x/crypto/bcrypt` (used by Authboss) | **v0.53.0** |
| Authorization | `github.com/casbin/casbin/v2` (static RBAC model+policy) | **v2.135.0** |
| Templates | stdlib `html/template` (server-rendered pages + HTMX partials) | — |
| Frontend interactivity | **HTMX** (vendored locally; stable line, **not** the v4 beta) | **v2.0.9** |
| Styling | `fervid-ds.css` (the approved Fervid design system) + logo | — |
| Export | stdlib `encoding/csv` | — |

Transitive: `gorilla/sessions` v1.4.0, `gorilla/securecookie` v1.1.2 (pulled by authboss-clientstate).

> **As built (see §14):** Authboss, authboss-clientstate and Casbin were never adopted — `go.mod`
> requires only `modernc.org/sqlite` and `golang.org/x/crypto`. Login, lockout, sessions and
> CSRF are hand-written in `internal/auth`, and authorization is the DB-driven permission engine.

**Minimal JS:** all interactivity is HTMX (`hx-get`/`hx-post` → server renders an HTML partial →
swapped in). At most ~30 lines of vanilla JS for the add-payment modal open/close and collapsing
project groups in the grid (or done with `<details>`/`<dialog>` and CSS where possible).

### Project layout
```
cmd/server/main.go              -- wiring, config, startup, embedded migrations
internal/db/                    -- schema.sql (embedded), connection, queries
internal/models/                -- Project, Head, Budget, Payment, User, AuditEntry
internal/auth/                  -- Authboss storer, Casbin enforcer, middleware (RequireLogin, RequirePermission)
internal/audit/                 -- audit.Record(...) helper
internal/handlers/              -- grid, payments, budgets, projects, heads, users, export, audit
internal/money/                 -- paise <-> ₹ Indian formatting
web/templates/                  -- layout.html + page & partial templates
web/static/                     -- htmx.min.js, fervid-ds.css, fervid-logo.svg
data/fervid.db                  -- SQLite file (created on first run)
data/attachments/               -- uploaded bill/receipt/payment proof files
data/backups/                   -- timestamped database + attachment backups
```

## 6. Screens & routes

| Route | Method | Who | Purpose |
|-------|--------|-----|---------|
| `/login`, `/logout` | GET/POST | all | Authboss-backed login; cookie session |
| `/` | GET | all | **Variance Grid** for current month (the home screen) |
| `/grid` | GET | all | Grid partial for `?month=&status=&q=` (HTMX swap on month/filter/search) |
| `/payments/new` | GET | data_entry, admin | Add-payment form (modal partial) |
| `/payments` | POST | data_entry, admin | Record a payment → swap affected row + project subtotal + grand total |
| `/payments/{id}` | GET | admin | Payment detail with full transaction audit trail |
| `/payments/{id}/edit` | GET/POST | admin | Edit a payment; write before/after audit row |
| `/payments/{id}` | DELETE | admin | Soft-void a payment with reason; write audit row |
| `/payments/{id}/attachments` | POST | data_entry, admin | Upload bill/receipt/photo for a payment |
| `/export.csv` | GET | all | CSV of the current month's grid |
| `/reports/monthly` | GET | all | Monthly company summary for selected month |
| `/reports/projects` | GET | all | Project-wise summary for selected month or range |
| `/reports/heads` | GET | all | Head-wise summary for selected month or range |
| `/reports/ytd.csv` | GET | all | Multi-month/YTD export for selected financial/calendar year |
| `/budgets` | GET/POST | **admin** | Budget editor for `?month=`; per-head amount inputs; **"copy last month"** button |
| `/months/{month}/lock` | POST | **admin** | Close/lock a reviewed month with reason |
| `/months/{month}/unlock` | POST | **admin** | Reopen a locked month with reason; audited |
| `/projects`, `/heads` | GET/POST | **admin** | Manage projects & heads (name, due day, active, order) |
| `/users` | GET/POST | **admin** | Create users, assign role, activate/deactivate, reset password |
| `/audit` | GET | **admin** | Browse the audit log (filter by actor/entity/date) |

## 7. Key flows

1. **Setup (admin):** create projects → add heads (with due day) → open Budget editor for the
   month and enter an amount per head. **"Copy last month's budgets"** pre-fills recurring heads
   (rent, salaries, interest) so only changes are typed.
2. **Daily entry (data-entry or admin):** "Add payment" → pick project → head → date (defaults to
   today) → amount → vendor/payee → payment mode → invoice/reference numbers → optional
   attachment/photo → remarks → save. The grid row, its project subtotal, and the grand total
   update via HTMX without a page reload.
3. **Correction (admin):** open a payment detail page → view who added it and when → edit fields
   or void it with a reason. Every correction records a before/after audit row, so the original
   transaction and all later changes remain visible.
4. **Month close (admin):** after review/export, lock the month with a reason. Locked months are
   read-only for budgets and payments unless an admin deliberately unlocks them with a reason.
5. **Review (all):** the Variance Grid shows, per the selected month, every project (collapsible
   group with subtotals) and head with Budget / Actual / Variance ₹ / Variance % / a budget-line
   bar / status. Filter by status (over/under/on-track/not-paid), search, switch month, export CSV.
   Additional report screens/export routes provide monthly company summary, project-wise summary,
   head-wise summary, and multi-month/YTD exports.
6. **Audit (admin):** every create/update/void/delete on payments, budgets, projects, heads, users —
   plus logins/exports — writes an `audit_log` row via `audit.Record(...)`. Optional SQLite
   triggers on `payments`/`budgets` provide a DB-level backstop so changes are captured even if a
   future code path forgets to call the helper.

## 8. Auth, roles & audit detail

- **Authboss** handles the user lifecycle with the `auth` (password login) and `lock` (lockout
  after repeated failures) modules. The **email-dependent modules (`recover`, `confirm`) are
  intentionally NOT enabled** — this is an internal LAN tool without SMTP. Instead, **admins create accounts and reset passwords** from the Users screen. If SMTP is
  added later, enabling `recover` for self-service reset is a small change.
- Sessions are **encrypted cookies** via `authboss-clientstate` (securecookie) — no server-side
  session table to manage. Cookie keys come from config/env; rotated by changing the key.
- **Casbin** enforces permissions. A static RBAC model maps roles to actions on resources
  (`payment`, `budget`, `project`, `head`, `user`, `report`, `month_lock`, `backup`). Middleware
  `RequirePermission(obj, act)` checks the logged-in user's role before each protected handler.
- **Audit** is application-level (`audit.Record`) for rich, human-readable summaries, optionally
  hardened with SQLite triggers for tamper-evidence.

> **As built (see §14):** there is no Authboss and no Casbin. `internal/auth` does bcrypt login,
> the five-failure / fifteen-minute lockout (`Store.RecordFailedLogin`), an HMAC-signed session
> cookie and a CSRF token; permissions come from roles stored in the database.

## 9. Seeding from the Excel

A one-time importer (`cmd/seed` or a `--seed` flag) loads the **11 projects, 83 heads, due days,
and June 2026 budgets** from `Forcast reports.xlsx` (we already have this parsed in
`mockups/data/budget-data.json`) so the team does not re-key the setup. It also creates an initial
admin user from config. Idempotent: safe to skip if data already exists.

> **As built (see §14):** the Excel importer was not built. `--seed` creates the admin and a
> small hard-coded sample (3 projects, 9 heads, June 2026 budgets) from `internal/store/seed.go`.

## 10. Deployment

Single self-contained binary plus `data/fervid.db`, run on a Mac or a small office server,
reachable on the LAN at `http://<host>:8080`. No cloud dependency. Config (port, cookie keys,
initial admin) via env vars or a small config file. HTTPS (a reverse proxy or Go's autocert) can
be added if it is ever exposed beyond the LAN.

### Backup and restore

The SQLite database and uploaded payment attachments are business records and must be backed up
together.

- Automated daily backup creates a timestamped copy of `data/fervid.db` plus the attachment
  directory into a configured backup folder.
- Use SQLite's online backup API or `VACUUM INTO` so backups are consistent while the app is
  running.
- Keep at least 30 daily backups and 12 monthly backups, with older retention configurable.
- Provide an admin-only restore procedure: stop the app, replace the database and attachment
  directory from a selected backup, restart, and record the restore event in an operator log.
- Periodically test restore on a separate machine/folder so backups are proven, not assumed.

## 11. Non-functional

- **Security:** bcrypt passwords; httpOnly + secure (when TLS) + SameSite cookies; CSRF protection
  on state-changing POSTs (Authboss/own middleware); parameterised SQL only; role checks on every
  mutating route; audit trail.
- **Correctness:** integer-paise money; variance computed in SQL; foreign keys enforced (`PRAGMA
  foreign_keys=ON`); validation for positive amounts, valid dates, active heads/projects, duplicate
  names, zero-budget variance percent, and locked-month mutation attempts.
- **Performance:** trivial data volume (hundreds of heads, thousands of payments/month); indexes on
  `payments(head_id)`, `payments(paid_on)`, `budgets(head_id, month)`.
- **Accessibility:** server-rendered semantic HTML; visible keyboard focus (already in the design
  system); works without JS for core reads (HTMX is progressive).

## 12. Testing

- Unit: money formatting/parsing; variance computation; Casbin permission matrix per role.
- Integration (httptest + temp SQLite): login required; data_entry blocked from budget/admin
  routes; record-payment updates totals; editing a payment writes before/after audit detail;
  voided payments are excluded from actual totals; locked months reject budget/payment changes;
  zero-budget variance percent renders as `N/A`; CSV and YTD export; audit row written per mutation.
- Backup/restore: create a test backup, restore into a temp directory, verify database integrity
  and attachment file availability.
- Follows test-driven development per the team's workflow.

## 13. Open questions / future

- Add the other dashboards (Executive Cockpit, Analytics Studio) as additional screens once the
  grid is in use.
- Income & Net Income (the Excel's second half) if expense tracking proves valuable.
- SMTP → enable Authboss `recover` for self-service password reset.
- "Remember me" persistent login (Authboss `remember` module + a `remember_tokens` table).
- Optional: refund/credit-note workflow if negative adjustments become necessary.

## 14. As-built notes (post-implementation)

Corrected 2026-09-25 against the code (docs-1). The first version of these notes described the
`feature/budget-variance` build, and parts of it had never been true or have since changed.

**Auth and authorization.** Neither Authboss nor Casbin is used: `go.mod` requires only
`modernc.org/sqlite` and `golang.org/x/crypto`. Authboss never entered `go.mod`, and Casbin was
replaced by a DB-driven permission engine in commit `a73e92a`. What ships:
- `internal/auth`: bcrypt passwords, login/logout (audited), an HMAC-signed session cookie
  (`fervid_session`), and a CSRF token checked on every state-changing POST.
- Lockout after five consecutive failures for fifteen minutes (`Store.RecordFailedLogin`);
  failed logins are audited.
- Roles and their `resource:action` permissions (with row scopes such as `all`) live
  in the database (`internal/store/permissions.go`) and are edited on `/roles`; `/audit` needs
  `audit:view`, an Admin grant in the seed.

**Seed.** There is no `seed.FromJSON` and no `mockups/data/budget-data.json`. `--seed`
(`Store.Seed`, `internal/store/seed.go`) creates the admin from `FERVID_ADMIN_*` and, only on an
empty database, 3 sample projects (Operations, People, Growth) with 3 heads each and a June 2026
budget per head — 3 projects / 9 heads / 9 budgets. It does not seed payments or requests.
`TestSeedCreatesTheAdminAndThreeSampleProjects` pins this.

**Built since the first notes** (previously listed as deferred): payment detail, edit and
soft-void with a per-payment audit trail; payment attachments; month lock/unlock with a reason;
CSRF tokens; backups (`/backups`, `--backup`, `--restore`) with an automated daily backup in the
server (`FERVID_BACKUP_HOUR`, default 02:00 local) and retention of every backup for
`FERVID_BACKUP_KEEP_DAYS` (30) plus the newest of each of the previous
`FERVID_BACKUP_KEEP_MONTHS` (12) months. The payment-request workflow, recoverables and
notifications are specified in `docs/superpowers/specs/`.

Config (env): `FERVID_ADDR`, `FERVID_DB`, `FERVID_ATTACHMENT_DIR`, `FERVID_BACKUP_DIR`,
`FERVID_BACKUP_KEEP_DAYS`, `FERVID_BACKUP_KEEP_MONTHS`, `FERVID_BACKUP_HOUR`,
`FERVID_SESSION_KEY`, `FERVID_ADMIN_EMAIL/PASSWORD/NAME`, `FERVID_SMTP_PASSWORD`, and
`FERVID_SECURE_COOKIES` (default `false`; set `true` behind HTTPS). There is no
`FERVID_COOKIE_KEY`. Session cookies are HMAC-authenticated (tamper-proof) but not encrypted —
run behind HTTPS in production (`internal/config/config.go`).
