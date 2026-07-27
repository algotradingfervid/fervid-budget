# UC-A — Platform, RBAC and Administration

Use-case specifications for the platform area of Fervid Budget: authentication and
session, the dashboard as permission-driven home, the shell navigation, roles and
permissions, users, configuration, the masters (projects, heads, months, budgets),
the vendor master, audit and backups, and the variance grid, reports and CSV
exports.

`UC-B-*` owns requests and approvals; `UC-C-*` owns settlement, recoverables and
notifications. This document references those areas but never specifies them.

**Every screen control, label, route and permission below was read out of the
code.** Citations are `file:line`. Where the code is ambiguous or surprising it is
recorded under **Open questions** rather than smoothed over.

---

## 0. Conventions

### 0.1 Actors

| ID | Actor | How it is constituted | Source |
|---|---|---|---|
| AC1 | **Administrator** | holds the seeded `Admin` system role — every one of the 66 canonical grants, plus `request=all` and `payment=all` data scope | `internal/store/migrations.go:396-401`, `internal/store/permissions.go:404-412` |
| AC2 | **Requester** | seeded `Requester` role: `request{view,create,edit,withdraw,reraise,comment,cancel}`, `attachment{view,create}`, scope `request=own` | `internal/store/migrations.go:352-364` |
| AC3 | **Manager** | seeded `Manager` role: `request{view,comment}`, `approval{approve,reject,return,reassign,accept_partial,cancel}`, `grid:view`, `report:view`, scope `request=all` | `internal/store/migrations.go:365-378` |
| AC4 | **Accounts** | seeded `Accounts` role: `payment{view,create,edit,void,process,settle,mark_partial,hold}`, `reservation{reserve,release}`, `grid:view`, `report{view,export}`, `recoverable_report{view,export}`, `attachment{view,create}`, `request{view,comment}`, scopes `request=all`, `payment=all` | `internal/store/migrations.go:379-395` |
| AC5 | **Role-less signed-in user** | a user row with no `user_roles` row at all — signed in, entitled to nothing. The sharpest probe in the suite | `tests/e2e/audit-support.ts:53-79` |
| AC6 | **Anonymous caller** | no `fervid_session` cookie, or one that fails HMAC/expiry/active checks | `internal/auth/auth.go:202-232` |
| AC7 | **Custom-role holder** | a role built on the roles screen with exactly the grants under test; the seeded roles cannot express e.g. "`vendor:view` but not `vendor_bank:view`" | `internal/app/app_integration_test.go:1159-1183` |

### 0.2 The one authorisation question

Every gate — middleware, handler check, or rendered control — goes through
`auth.Manager.Can` (`internal/auth/auth.go:122-128`), which asks the
`store.PermissionSet` resolved per request from `user_roles → role_permissions`
(`internal/store/permissions.go:560-598`). Matching is **exact**: there are no
wildcards (`internal/store/permissions.go:70-85`). Multiple roles union their
grants; the broadest data scope wins (`all > assigned > own`,
`internal/store/permissions.go:117-137`).

`RequirePermission` wraps `RequireLogin`, so an anonymous caller is **redirected**
(303 → `/login`) rather than refused, and a signed-in caller without the grant gets
**403** rendered as the chrome-less error page (`internal/auth/auth.go:140-148`,
`internal/app/http_errors.go:101-125`).

### 0.3 Standing facts used throughout

| ID | Fact | Source |
|---|---|---|
| F1 | Home (`GET /` and `GET /dashboard`) renders the dashboard; the variance grid has its own `GET /grid` | `internal/app/app.go:375-381` |
| F2 | `GET /` is registered as `GET /{$}` (root only); a catch-all `GET /` answers 404 through `notFound`. An **unrouted POST answers 405**, because the path matches the catch-all and the method does not | `internal/app/app.go:375-376`, `internal/app/http_errors.go:158-160` |
| F3 | Money is `int64` paise; `money.FormatPaise` already emits `₹` | `internal/money/money.go`, PROGRESS.md standing rules |
| F4 | The error page renders chrome-less — no sidebar, no tab bar — on every status | `internal/app/http_errors.go:121-125`, `internal/app/phase6_screens_test.go:68-82` |
| F5 | Every list table is `class="t-cards"` and every `<td>` carries `data-label`, which is what captions the field once the table restacks into cards below 860px | `internal/app/phase6_screens_test.go:18-46` |
| F6 | Overlay sheets (`.overlay > .sheet`) are opened by `data-open="<id>"` and closed by `data-close="<id>"`; focus moves to the first focusable, Escape closes, Tab is trapped, and focus returns to the opener | `web/static/fervid-app.js:113-175`, `:402-412` |
| F7 | `unbuiltPrefixes` is **empty** — every navigable screen resolves to a real route | `internal/app/nav.go:250` |
| F8 | CSRF is a double-submit cookie: `fervid_csrf` (not HttpOnly, SameSite=Lax) compared constant-time against the `csrf` form field or `X-CSRF-Token` header | `internal/auth/auth.go:167-189` |

### 0.4 Route inventory for this area

| ID | Method + path | Gate | Use case |
|---|---|---|---|
| RT01 | `GET /login` · `POST /login` | none | UC-A-01, UC-A-02 |
| RT02 | `POST /logout` | CSRF only | UC-A-03 |
| RT03 | `GET /{$}` · `GET /dashboard` | session | UC-A-07 |
| RT04 | `GET /` (catch-all) | session | UC-A-06 |
| RT05 | `GET /grid` | `grid:view` — **session only at `30edd6a`**; gated by Wave 3, `1fac147` (`internal/app/app.go:431`, F-A-02/F-G-032). See 09.e1. | UC-A-42 |
| RT06 | `GET /export.csv` | `grid:export` | UC-A-43 |
| RT07 | `GET /reports/monthly` · `/reports/projects` · `/reports/heads` | `report:view` | UC-A-44 |
| RT08 | `GET /reports/ytd.csv` | `report:export` | UC-A-45 |
| RT09 | `GET /months` | `month:view` | UC-A-30 |
| RT10 | `POST /months` | `month:create` | UC-A-30 |
| RT11 | `POST /months/{month}/lock` · `/unlock` | `month:lock` | UC-A-31 |
| RT12 | `GET /budgets` | `budget:view` | UC-A-32 |
| RT13 | `POST /budgets` | `budget:edit` | UC-A-32, UC-A-33 |
| RT14 | `GET /projects` · `POST /projects` | `project:view` / `project:edit` | UC-A-28 |
| RT15 | `GET /heads` · `POST /heads` | `head:view` / `head:edit` | UC-A-29, UC-A-34 |
| RT16 | `GET /vendors` | `vendor:view` | UC-A-35 |
| RT17 | `GET /vendors/search` | `vendor:view` | UC-A-36 |
| RT18 | `GET /vendors/new` · `POST /vendors` | `vendor:create` | UC-A-37 |
| RT19 | `GET /vendors/{id}` · `POST /vendors/{id}` | `vendor:view` / `vendor:edit` | UC-A-38, UC-A-39 |
| RT20 | `GET /users` | `user:view` | UC-A-18 |
| RT21 | `POST /users` | `user:edit` | UC-A-19 … UC-A-23 |
| RT22 | `GET /roles` | `role:view` | UC-A-10 |
| RT23 | `POST /roles` | `role:edit` | UC-A-13, UC-A-16, UC-A-17 |
| RT24 | `POST /roles/new` | `role:create` | UC-A-11 |
| RT25 | `POST /roles/{id}/copy` | `role:create` | UC-A-12 |
| RT26 | `POST /roles/{id}/delete` | `role:delete` | UC-A-14, UC-A-15 |
| RT27 | `GET /configuration` · `POST /configuration` | `config:view` / `config:edit` | UC-A-24, UC-A-25, UC-A-26 |
| RT28 | `POST /configuration/recoverable-categories` | `recoverable_category:edit` | UC-A-27 |
| RT29 | `GET /audit` | `audit:view` | UC-A-40 |
| RT30 | `GET /backups` · `POST /backups` | `backup:view` / `backup:create` | UC-A-41 |

All route registrations: `internal/app/app.go:362-524`.

---

## 1. Authentication, session and error handling

### UC-A-01 — Sign in

| | |
|---|---|
| **Goal** | "I want to get into Fervid Budget and land where my work is." |
| **Primary actor** | Any user with an active account (AC1–AC5) |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | The actor opens any URL without a valid session, or opens `/login` directly |
| **Route(s)** | `GET /login` → `POST /login` → 303 `/` |
| **Permission gate** | None. `POST /login` is deliberately **not** wrapped in `withCSRF` — there is no session yet to protect (`internal/app/app.go:364-365`) |
| **Coverage IDs** | R6 (nearest — permissions are resolved from the session this creates); no dedicated matrix ID |
| **Priority** | critical |

**Preconditions**

1. A `users` row exists with the submitted email, lower-cased and trimmed (`internal/store/store.go:99-102`).
2. `users.active = 1`.
3. `users.locked` is NULL or in the past (`internal/store/store.go:170-183`).
4. The submitted password verifies against the bcrypt hash (`internal/auth/auth.go:62-64`).

**Postconditions (success)**

1. A `fervid_session` cookie holds `base64url("<userID>:<expUnix>:<hmac>")` with a 12-hour expiry, `HttpOnly`, `SameSite=Lax`, and `Secure` only when `FERVID_SECURE_COOKIES` is set (`internal/auth/auth.go:66-72`).
2. A `fervid_csrf` cookie exists (`internal/auth/auth.go:182-189`).
3. `users.attempt_count = 0`, `last_attempt = NULL`, `locked = NULL` (`internal/store/store.go:229-232`).
4. One `audit_log` row: `action='login'`, `entity_type='user'`, `entity_id=<userID>`, `summary='Logged in'`, `ip=r.RemoteAddr` (`internal/app/app.go:616`).
5. The browser is at `/`, showing the dashboard (F1).

**Postconditions (failure)** — no session cookie is issued; `users` is unchanged except `attempt_count` / `last_attempt` / `locked`; no `action='login'` audit row.

**Main success scenario**

1. The actor requests `GET /login`.
2. The system renders the `login` template: a chrome-less `body.login-body > main.login-card` with `<h1>Fervid Budget</h1>`, the field **Email** (`name="email"`, `type="email"`, `autocomplete="username"`, `required`), the field **Password** (`name="password"`, `type="password"`, `autocomplete="current-password"`, `required`), and a single submit button reading **Login** (`internal/app/templates.go:92-95`).
3. The actor types their email and password and presses **Login**.
4. The system lower-cases and trims the email, loads the user, checks the lockout window, then verifies the password (`internal/app/app.go:589-611`).
5. The system clears the failure counters, issues the session and CSRF cookies, writes the `login` audit row, and redirects 303 to `/`.
6. `GET /` renders the dashboard with the actor's name in the heading (UC-A-07).

**Alternate flows**

- **1.a** The actor arrives from a deep link. `RequireLogin` sent them here with 303; the target URL is **not** remembered, so step 6 lands on `/` and not on the page they asked for (`internal/auth/auth.go:87-95`).
- **3.a** The actor submits with an empty field: the browser's own `required` validation blocks the POST; no request reaches the server.

**Exception flows**

- **01.e1** Wrong password, unknown email, or an inactive user → the login screen re-renders with `<div class="alert error" role="alert" aria-live="assertive">Invalid email or password</div>`. **The HTTP status is 200, not 401** — the handler calls `a.render`, which is `renderStatus(…, http.StatusOK, …)` (`internal/app/app.go:609`, `:526-528`). All three causes produce the same message, so the screen cannot be used to enumerate accounts.
- **01.e2** Malformed body → `renderStatus(400, "login", Error: "Invalid form")` (`internal/app/app.go:585-588`).
- **01.e3** Five consecutive failures → UC-A-02.

**Business rules**

- Passwords are bcrypt at default cost; a hash is only ever produced through `auth.HashPassword`, which re-validates length ≥ 8 with at least one letter and one digit (`internal/auth/auth.go:37-60`).
- The session payload is HMAC-SHA256 over `"<id>:<exp>"` with `cfg.SessionKey`; a tampered cookie fails `hmac.Equal` and is treated as anonymous (`internal/auth/auth.go:216-218`).
- Every request re-reads the user row and rejects a deactivated one, so authorisation is never served from the cookie alone (`internal/auth/auth.go:227-230`).

**Data touched** — read `users(id,email,name,password_hash,role,active,locked,attempt_count)`; write `users(attempt_count,last_attempt,locked,updated_at)`, `audit_log(actor_id,actor_name,action,entity_type,entity_id,summary,ip,created_at)`.

**Non-functional / UX notes** — the login screen has `Shell.Chrome` unset, so it renders neither sidebar nor tab bar and is the same at 390px as at 1440px. The error alert carries `role="alert"` and `aria-live="assertive"` so a screen reader announces it (`internal/app/app_integration_test.go:161-177`). `autocomplete` metadata is asserted by the same test. The page never contains the bootstrap credentials.

**Open questions**

- A failed login answering **200 OK** makes "did the login fail?" a body-text question, not a status question. Intentional (it is a re-render, not an API), but every test must assert on the message.
- `cfg.SessionKey` defaults to a fresh random 32 bytes per process when `FERVID_SESSION_KEY` is unset (`internal/config/config.go:34`, `:80-86`), so a server restart silently invalidates every session. Correct for security, surprising in operation.

---

### UC-A-02 — A failed sign-in is counted, and five failures lock the account

| | |
|---|---|
| **Goal** | "If someone guesses at my password, the system should stop them — and tell me it has." |
| **Primary actor** | AC6 (an attacker, or a user who has forgotten their password) |
| **Supporting actors** | AC1 (reads the audit log afterwards) |
| **Scope / level** | system · subfunction |
| **Trigger** | `POST /login` with a wrong password for a known email |
| **Route(s)** | `POST /login` |
| **Permission gate** | None |
| **Coverage IDs** | C2 (audit history — the failures are audited); no dedicated matrix ID |
| **Priority** | high |

**Preconditions**

1. A `users` row exists for the submitted email.
2. `users.locked` is NULL or in the past.
3. `users.attempt_count` is known (0 on a fresh row).

**Postconditions (success — i.e. the lock engages)**

1. `users.attempt_count = 5`, `users.locked = now + 15 minutes` (UTC), `users.last_attempt = CURRENT_TIMESTAMP` (`internal/store/store.go:206-215`).
2. Five `audit_log` rows with `action='login_failed'`, `summary='Failed login attempt'` (`internal/app/app.go:608`).
3. A sixth attempt — **even with the correct password** — writes a further `login_failed` row whose summary is `"Login blocked until <RFC3339>"` and re-renders the login screen (`internal/app/app.go:592-596`).
4. No session cookie is ever issued.

**Postconditions (failure)** — nothing else in the database changes; no `login` row.

**Main success scenario**

1. The actor submits **Email** + a wrong **Password** and presses **Login**.
2. The system finds the user, sees no live lock, fails the bcrypt comparison.
3. The system calls `RecordFailedLogin`, which increments `attempt_count` inside a transaction (`internal/store/store.go:188-220`).
4. The system writes a `login_failed` audit row and re-renders the login screen with **Invalid email or password**.
5. Steps 1–4 repeat until the fifth failure, at which point `RecordFailedLogin` also sets `locked = now + 15m`.
6. The actor submits again — right password or wrong — and the system answers before checking the password at all, rendering **Too many failed attempts. Try again later.**
7. Fifteen minutes later the lock has expired; the next failure resets the counter to 1 rather than continuing from 5 (`internal/store/store.go:203-206`).

**Alternate flows**

- **2.a** The email is unknown. `RecordFailedLogin` finds no row and returns `(zero, nil)` without writing anything, and the audit row is written with `actor_id = NULL` and `actor_name = <the submitted email>` (`internal/app/app.go:599-608`). No lockout exists for an address that does not exist — deliberate anti-enumeration.
- **2.b** The account is inactive. The password check short-circuits on `!u.Active`, the failure is still counted, and the message is the same generic one.

**Exception flows**

- **02.e1** `RecordFailedLogin` errors for a reason other than `ErrNotFound` → logged at ERROR with the request id; the user still sees **Invalid email or password** (`internal/app/app.go:599-601`).
- **02.e2** `ResetLoginFailures` fails after a *successful* login → logged, but the login proceeds (`internal/app/app.go:612-614`). The counter stays high; the next single failure could therefore reach 5.

**Business rules**

- Lockout is 5 attempts / 15 minutes, both hard-coded (`internal/store/store.go:209-212`).
- The lock is checked **before** the password, so a locked account cannot be probed for a correct password (`internal/app/app.go:591-597`).
- `LoginLocked` returns `ErrNotFound` for an unknown email and the handler ignores that error, which is what keeps unknown and known addresses indistinguishable (`internal/store/store.go:170-176`).

**Data touched** — `users(attempt_count,last_attempt,locked,updated_at)`, `audit_log`.

**Non-functional / UX notes** — the lockout message does not say how long the wait is, even though the store knows (`until` is computed and only used in the audit summary). At 390px the login card is the whole screen; the alert appears above the fields.

**Open questions**

- There is no self-service unlock and no admin "unlock" control anywhere in the UI. `ResetLoginAttempts` exists in the store (`internal/store/store.go:222-225`) but no route calls it. An operator's only remedies are to wait 15 minutes or edit the database. Worth a spec decision.
- A successful login is audited with the caller's `RemoteAddr` verbatim, including the port. `remoteIP` (`internal/app/http_errors.go:230-236`) strips the port for logs but is not used for the audit row.

---

### UC-A-03 — Sign out

| | |
|---|---|
| **Goal** | "I want to end my session on this machine." |
| **Primary actor** | Any signed-in user |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | The actor presses **Log out** in the sidebar footer, or in the More sheet on a phone |
| **Route(s)** | `POST /logout` → 303 `/login` |
| **Permission gate** | CSRF only (`a.withCSRF(a.logoutPost)`, `internal/app/app.go:366`). No `RequireLogin` wrapper |
| **Coverage IDs** | none (session management predates the matrix) |
| **Priority** | high |

**Preconditions**

1. The actor is signed in (otherwise the control is not rendered).
2. The `fervid_csrf` cookie and the form's hidden `csrf` field agree.

**Postconditions (success)**

1. `fervid_session` is cleared with `MaxAge=-1` (`internal/auth/auth.go:74-76`).
2. One `audit_log` row: `action='logout'`, `entity_type='user'`, `summary='Logged out'` (`internal/app/app.go:620-624`).
3. The browser is at `/login`.
4. The `fervid_csrf` cookie is **not** cleared.

**Postconditions (failure)** — the session survives; no audit row.

**Main success scenario**

1. On desktop the actor presses **Log out** in `form.side-logout` at the bottom of the sidebar (`internal/app/templates.go:68`).
2. The browser POSTs `/logout` with the hidden `csrf` field.
3. `withCSRF` parses the form and compares the token constant-time.
4. `logoutPost` writes the `logout` audit row for the current user, clears the session cookie, and redirects 303 to `/login`.
5. The login screen renders with no error banner.

**Alternate flows**

- **1.a** On a phone the actor taps **More** in the tab bar, then **⏻ Log out** in the sheet's **Session** group (`internal/app/templates.go:88-89`). Same route, same wording — "one action, one name on every device" (`internal/app/templates.go:67`).
- **2.a** The caller is already anonymous. `auth.CurrentUser` returns a zero user, `u.ID == 0`, so no audit row is written and the redirect still happens (`internal/app/app.go:621-622`).

**Exception flows**

- **03.e1** Missing or forged `csrf` → 403 chrome-less error page, **Your form session expired. Refresh the page and try again.** (`internal/app/app.go:572-574`; asserted at `tests/e2e/audit-smoke.spec.ts:70-79`).

**Business rules**

- Logging out does not invalidate the signed cookie server-side — there is no session table. A copied cookie value remains valid until its 12-hour expiry or the user is deactivated (`internal/auth/auth.go:202-232`).

**Data touched** — `audit_log`; cookies only otherwise.

**Non-functional / UX notes** — the sidebar logout is a real `<form>`, so it works with JavaScript off. In the More sheet the button is inside `form.ms-logout`; the sheet traps Tab, and Escape closes it (F6).

**Open questions** — because `fervid_csrf` outlives the session, the token issued to one user is presented by the next user on that browser. It is not bound to the session id, so it cannot be used to detect a session swap.

---

### UC-A-04 — An anonymous or expired session is sent to the login screen

| | |
|---|---|
| **Goal** | "If my session is gone I want to be asked to sign in again, not shown a broken page." |
| **Primary actor** | AC6 |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | Any request to a gated route with no cookie, a tampered cookie, an expired cookie, or a cookie for a user who has since been deactivated |
| **Route(s)** | every route except `GET /login`, `POST /login`, `POST /logout` and `/static/…` |
| **Permission gate** | `RequireLogin` (`internal/auth/auth.go:87-95`) |
| **Coverage IDs** | R6 |
| **Priority** | critical |

**Preconditions**

1. The request targets a route wrapped in `RequireLogin` or `RequirePermission`.
2. `auth.CurrentUser(r).ID == 0` — the middleware could not resolve a user (`internal/auth/auth.go:78-85`).

**Postconditions (success)**

1. The response is **303 See Other** with `Location: /login`.
2. No handler body runs; no data is read and nothing is written.
3. No audit row.

**Postconditions (failure)** — n/a; there is no failure branch.

**Main success scenario**

1. The actor requests, say, `GET /users` with no session cookie.
2. `Manager.Middleware` finds no resolvable user and puts nothing in the context.
3. `RequireLogin` sees `CurrentUser(r).ID == 0` and issues `http.Redirect(…, "/login", http.StatusSeeOther)`.
4. The browser follows to `/login` and UC-A-01 begins.

**Alternate flows**

- **2.a** The cookie is present but its base64 or its three-part `id:exp:sig` shape is wrong → same outcome (`internal/auth/auth.go:207-214`).
- **2.b** The HMAC does not match — the cookie was edited or signed with a different `SessionKey`, e.g. after a restart with no `FERVID_SESSION_KEY` → same outcome (`internal/auth/auth.go:216-218`).
- **2.c** `exp` has passed (12 hours after sign-in) → same outcome (`internal/auth/auth.go:219-222`).
- **2.d** The user was **deactivated** while signed in: `UserByID` succeeds but `!u.Active`, so the middleware treats the request as anonymous and the actor is bounced on their very next click (`internal/auth/auth.go:227-230`).

**Exception flows**

- **04.e1** A **POST** from an expired session is redirected 303 to `/login`, which the browser follows as a **GET**. The submitted form data is lost with no message. Confirmed by the redirect being unconditional in `RequireLogin` — there is no method branch.
- **04.e2** An htmx-issued request is redirected the same way; because the response is a full login page, htmx swaps a login page into whatever target the fragment was bound to. Not handled anywhere in `internal/app`.

**Business rules**

- The redirect status is **303**, not 302 — a matrix that pins 302 fails for a reason unrelated to authorisation (`tests/e2e/audit-smoke.spec.ts:59-67`).
- Permission gating always runs *after* login gating, because `RequirePermission` wraps `RequireLogin` (`internal/auth/auth.go:140-148`). An anonymous caller therefore never sees 403.

**Data touched** — `users` (one row read per request, when a cookie is present).

**Non-functional / UX notes** — no "your session expired" notice is shown on the login screen, so an expiry is indistinguishable from a first visit. The permission set is resolved per request with no cache (`internal/auth/auth.go:97-105`), so a revoked grant takes effect on the next click.

**Open questions**

- The redirect discards the requested URL. There is no `?next=` parameter anywhere, so a deep link mailed to a signed-out user always lands on the dashboard.
- 04.e2 (htmx + expiry) has no test and no server-side handling: no `HX-Redirect` header is ever set. Whether the resulting page-inside-a-fragment is acceptable is a product question.

---

### UC-A-05 — A state-changing POST without a valid CSRF token is refused

| | |
|---|---|
| **Goal** | "A form posted from somewhere that isn't my screen must not change anything." |
| **Primary actor** | AC6 (a cross-site attacker) |
| **Supporting actors** | any signed-in user, as the victim |
| **Scope / level** | system · subfunction |
| **Trigger** | Any POST wrapped in `withCSRF` arrives with no `csrf` field, a wrong one, or no `fervid_csrf` cookie |
| **Route(s)** | every POST in this area except `POST /login`: `/logout`, `/months`, `/months/{month}/lock`, `/months/{month}/unlock`, `/budgets`, `/projects`, `/heads`, `/vendors`, `/vendors/{id}`, `/users`, `/roles`, `/roles/new`, `/roles/{id}/copy`, `/roles/{id}/delete`, `/configuration`, `/configuration/recoverable-categories`, `/backups` |
| **Permission gate** | The route's own grant is checked **first** (`RequirePermission` wraps the CSRF wrapper), then `withCSRF` (`internal/app/app.go:554-578`) |
| **Coverage IDs** | R6 |
| **Priority** | critical |

**Preconditions**

1. The request method is POST (`CheckCSRF` returns true unconditionally for GET/HEAD/OPTIONS — `internal/auth/auth.go:167-170`).
2. The caller already passed the route's permission gate.

**Postconditions (success — i.e. the refusal)**

1. **403** rendered as the chrome-less error page: error code `403`, eyebrow **Request could not be completed**, `<h1>Forbidden</h1>`, body **Your form session expired. Refresh the page and try again.**, a `Request ID: <hex>` line, and two ways out — **Return to dashboard** and **Go back** (`internal/app/app.go:573`, `internal/app/templates.go:97-110`).
2. Nothing is written: the handler function is never called.

**Postconditions (failure)** — n/a.

**Main success scenario**

1. The actor (or a page on another origin) POSTs to a gated route without the `csrf` field.
2. `withCSRF` caps the body at 21 MiB and parses the form (`internal/app/app.go:556-571`).
3. `CheckCSRF` reads the `fervid_csrf` cookie; if absent it returns false immediately.
4. It reads `csrf` from the form, falling back to the `X-CSRF-Token` header, and compares constant-time with `subtle.ConstantTimeCompare`.
5. On mismatch the system responds 403 with the recovery message and never reaches the handler.

**Alternate flows**

- **4.a** htmx sends the token in the `X-CSRF-Token` header instead of the body; both are accepted (`internal/auth/auth.go:175-179`).
- **2.a** The body is `multipart/form-data` → parsed with `ParseMultipartForm(1<<20)` instead; the same CSRF check follows (`internal/app/app.go:558-562`).

**Exception flows**

- **05.e1** The body exceeds `maxRequestBodyBytes` (21 MiB) → **413**, **The submitted form is too large.** (`internal/app/app.go:564-567`; pinned at `internal/app/http_safety_test.go:57-95`, which also asserts no partial file survives).
- **05.e2** The body is unparseable → **400**, **The submitted form could not be read.**
- **05.e3** The caller lacks the route's grant **and** the token — the answer is the permission 403 (**You do not have permission to perform this action.**), because the permission gate is the outer wrapper. A test that expects the CSRF wording here will fail.

**Business rules**

- Order of checks is permission → body size → parse → CSRF → handler (`internal/app/app.go:511-523` shows the composition; `:554-577` the wrapper).
- `EnsureCSRF` is called on every render, so any freshly loaded screen carries a usable token (`internal/app/http_errors.go:103`).
- The token is `sign(unixNano)[:32]` — unpredictable to a third party but **not bound to the user or the session** (`internal/auth/auth.go:186`).

**Data touched** — none.

**Non-functional / UX notes** — the 403 page is chrome-less by design: "a sidebar invites the reader to click deeper into an app that has just failed" (`internal/app/http_errors.go:116-120`). **Go back** is `href="javascript:history.back()"`, so it does nothing with JavaScript disabled.

**Open questions**

- Because the token lives in a non-HttpOnly cookie and is compared only against itself, a subdomain able to set cookies on the parent domain could forge a matching pair. `SameSite=Lax` is the real defence. Recording as a hardening observation, not a live exploit.
- `CSRFMiddleware` (`internal/auth/auth.go:191-200`) is dead code: `routes()` never installs it. Every protected POST uses `a.withCSRF` instead.

---

### UC-A-06 — An unknown URL answers with the chrome-less error page

| | |
|---|---|
| **Goal** | "If a page doesn't exist, tell me so — don't show me some other page." |
| **Primary actor** | Any signed-in user |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | A GET to any path the app does not serve |
| **Route(s)** | `GET /` (the catch-all, → `notFound`); any POST to an unrouted path → **405** |
| **Permission gate** | `RequireLogin` (`internal/app/app.go:376`) |
| **Coverage IDs** | D5 (proof-of-absence relies on this), X2, X3, X4 (all assert 404-or-405 through this route) |
| **Priority** | high |

**Preconditions**

1. The actor is signed in.
2. No `mux` pattern matches the path, other than the catch-all `GET /`.

**Postconditions (success)**

1. **404** with the chrome-less error page: code `404`, `<h1>Not Found</h1>`, body **That page does not exist. It may have moved, or it may not be built yet.** (`internal/app/http_errors.go:158-160`).
2. `X-Request-ID` is set on the response and printed on the page (`internal/app/http_errors.go:59`, `internal/app/templates.go:105`).
3. One structured log line at WARN level with method, path, status and duration (`internal/app/http_errors.go:76-94`).

**Postconditions (failure)** — n/a.

**Main success scenario**

1. The actor requests `GET /definitely-not-a-route`.
2. `ServeMux` matches the catch-all `GET /` and `RequireLogin` passes the signed-in caller through.
3. `notFound` calls `respondError(404, …)`.
4. `renderStatus` sees `name == "error_page"` and sets `Shell{Chrome: chromeNone}`, so no sidebar and no tab bar are built — and the badge queries behind them are not run.
5. The actor sees the error state and can press **Return to dashboard** (`href="/"`) or **Go back**.

**Alternate flows**

- **1.a** A **POST** to an unrouted path answers **405 Method Not Allowed** from `ServeMux` itself, because the path matches the catch-all `GET /` and only the method fails (F2, PROGRESS.md "Traps"). The body is Go's plain text, not the error page.
- **1.b** An anonymous caller gets 303 → `/login` instead, because `RequireLogin` runs first (UC-A-04).

**Exception flows**

- **06.e1** Any handler panic is recovered, logged with a stack trace, and answered with **500** and **Something went wrong while processing your request.** — unless a response was already started (`internal/app/http_errors.go:63-75`; pinned at `internal/app/http_safety_test.go:133`).
- **06.e2** A template fails to execute → plain-text 500 carrying the request id (`internal/app/http_errors.go:128-136`).

**Business rules**

- The root is registered as `GET /{$}` precisely so the dashboard is not a catch-all; before that, "every URL the app does not serve … silently rendered the dashboard under the wrong address" (`internal/app/app.go:368-374`).
- The error page must contain neither `<aside` nor `class="tabbar"` (`internal/app/phase6_screens_test.go:68-82`).

**Data touched** — none.

**Non-functional / UX notes** — the error page is the only screen besides login that renders with `Chrome: none`; at 390px it is a single column with the code, heading, message, request id and the two buttons. `.error-state` carries `role="alert"` and `aria-live="assertive"`.

**Open questions**

- 405 answers with Go's default `text/plain` body, so the only "not built" experience that is *not* on the design system is the one an unrouted POST produces. Proof-of-absence tests must accept 404 **or** 405 (`internal/app/app_integration_test.go:1530`, `internal/app/recoverables_app_test.go:311-353`).

---

### UC-A-07 — Open the dashboard as the permission-driven home

| | |
|---|---|
| **Goal** | "Show me, in one place, everything that is waiting on me — and nothing I'm not allowed to touch." |
| **Primary actor** | Any signed-in user (AC1–AC5) |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sign-in redirect, the sidebar's **Home**, the tab bar's **Home**, or the topbar's `⌂` back control |
| **Route(s)** | `GET /{$}` and `GET /dashboard` — the same handler (`internal/app/app.go:375-381`) |
| **Permission gate** | Session only. "Every area inside it is gated on the queue it opens: a person with no areas is told nothing is waiting on them rather than refused the page" (`internal/app/app.go:378-381`) |
| **Coverage IDs** | D1, D2, D4 |
| **Priority** | critical |

**Preconditions**

1. The actor is signed in.
2. Their effective permission set has been resolved (deny-all on error — `internal/auth/auth.go:111-117`).

**Postconditions (success)**

1. A 200 HTML page titled `Home - Fervid Budget` with the app shell.
2. No rows are written. Reads only: `CountRequests` and `ListRequests` per permitted area, plus `BadgeCounts` and `UnreadNotificationCount` for the chrome.

**Postconditions (failure)** — nothing written; a store error becomes `respondStoreError` (500 unless it maps to a friendlier status).

**Main success scenario**

1. The actor requests `GET /`.
2. The system reads the effective permission set and, for each area the actor may act on, counts the whole queue and lists at most `dashboardRows = 4` rows (`internal/app/dashboard.go:41`, `:50-68`).
3. The system renders the page banner: eyebrow **Home**, `<h1>Good day, {name}</h1>`, sub **Everything below is waiting on someone. The ones marked *you* are yours.**, and — for a holder of `request:create` — the button **＋ New request** linking to `/requests/new` (`internal/app/templates.go:3181-3190`).
4. The system renders `div.metric-strip` with the tiles the actor is entitled to: **Needs my action** and **My open requests** (`request:create`), **Awaiting my approval** and **Cancellation requests** (`approval:approve`), **Approved, unclaimed** (`payment:process`), and **Budget / Variance grid** (`grid:view`, linking to `/grid`) (`internal/app/templates.go:3192-3225`).
5. The system renders one `section.area` per non-empty queue, in this order: **Needs your action** (`!`), **In progress** (`▤`), **Awaiting your approval** (`✓`), **Decisions only you can make** (`⏸`), **Approved and unclaimed** (`₹`), **Administration** (`⚙`) (`internal/app/dashboard.go:70-103`).
6. Each request row shows `{number} · {short title}`, an **Urgent** pill when flagged, the status pill, the waiting-on line, and the amount; the row links to `/requests/{id}` (`internal/app/templates.go:3232-3242`).
7. Each area ends with its queue link: **Open my requests →**, **See all my requests →**, **Open the approvals queue →**, **Review all →**, **Open all requests →** (`internal/app/dashboard.go:71-99`).
8. The **Administration** area lists only the destinations the actor may reach: **Configuration** / *Numbering, attachments, urgency, approvals*, **Roles & permissions** / *Who can do what*, **Users** / *People and their default approvers*, **Vendors** / *The vendor master* (`internal/app/dashboard.go:111-127`).

**Alternate flows**

- **5.a** A queue's count is 0 → the area is not rendered at all; the metric tile still carries the zero (`internal/app/dashboard.go:56-59`).
- **5.b** The actor may reach none of the four admin destinations → the **Administration** area disappears entirely (`internal/app/dashboard.go:101-103`).
- **5.c** No area at all applies (AC5, a role-less user) → a single area reading **Nothing is waiting on you** with **When something needs you, it appears here.** (`internal/app/templates.go:3249-3254`).
- **2.a** `/dashboard` is requested instead of `/` → identical output, but `activeNavKey` matches nothing for `/dashboard` (the nav item's href is `/`), so **no sidebar item is highlighted** (`internal/app/nav.go:170-193`).

**Exception flows**

- **07.e1** `CountRequests` or `ListRequests` errors → `respondStoreError`, i.e. the 500 error page. The whole dashboard is lost for one failing queue (`internal/app/dashboard.go:72-100`).
- **07.e2** `BadgeCounts` errors → logged at WARN, badges render as absent, the page still renders (`internal/app/nav.go:140-148`).
- **07.e3** `UnreadNotificationCount` errors → logged at WARN, the bell renders with no dot (`internal/app/nav.go:151-159`).

**Business rules**

- Every area is gated on the same permission its queue is, so nobody is shown work behind a door they cannot open (`internal/app/dashboard.go:10-20`).
- The count on the heading is the **whole** queue, never the number of rows shown (`internal/app/dashboard.go:49-50`).
- `Scope` on each area's `RequestListOptions` is fixed in code — `own` for the requester areas, `assigned` for the approver areas, `all` for the Accounts area — and the viewer id is always the caller's (`internal/app/dashboard.go:51-99`).
- No template may compare a role name (`internal/app/app_integration_test.go:695`).

**Data touched** — reads `payment_requests` (via `CountRequests`/`ListRequests`), `users`, `role_permissions`, `role_data_scope`, `user_roles`, `budget_months`, `payments`, `payment_attachments`, `notifications`.

**Non-functional / UX notes** — at 390px the sidebar is replaced by `.m-topbar` + `.tabbar` and the metric strip wraps; the shell test asserts zero horizontal overflow and exactly one navigation chrome on both `/` and `/dashboard` (`tests/e2e/shell.spec.ts:11-51`). The `h1` is visible at every width — it must be, because `.page-banner d-only` would leave a phone with no heading (PROGRESS.md trap).

**Open questions**

- The **Budget / Variance grid** tile is gated on `grid:view`, but `GET /grid` itself is not (UC-A-42). A user without `grid:view` has no tile and full access to the screen.
- `/dashboard` highlights no nav item. Harmless, but a test asserting `aria-current="page"` on Home must use `/`, not `/dashboard`.

---

### UC-A-08 — Navigate by sidebar, mobile tab bar and More sheet

| | |
|---|---|
| **Goal** | "I want to see only the parts of the product I'm allowed to use, on whatever device I'm holding." |
| **Primary actor** | Any signed-in user |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | Every full page render |
| **Route(s)** | n/a — the shell is built by `buildPageShell` on every non-fragment render (`internal/app/http_errors.go:108-125`) |
| **Permission gate** | Per item: each `NavItem` names the resource and action it needs; items with no resource are visible to everyone (`internal/app/nav.go:269-277`) |
| **Coverage IDs** | D2 |
| **Priority** | high |

**Preconditions**

1. The actor is signed in — a signed-out render gets `Shell{Chrome: none}` (`internal/app/nav.go:129-132`).
2. The request is not an htmx fragment; `HX-Request` skips shell construction entirely (`internal/app/http_errors.go:121-125`).

**Postconditions (success)**

1. `Shell.Groups` holds only the items the actor may reach; a group left with no items disappears (`internal/app/nav.go:103-123`).
2. `Shell.Tabs` holds at most two left items, one centre action, and at most two right items with **More** always last (`internal/app/nav.go:212-235`).
3. `Shell.Active` is the key of the longest matching href (`internal/app/nav.go:168-193`).
4. `Shell.Badges` holds only counts the actor is entitled to; `Shell.Unread` is the bell count.

**Postconditions (failure)** — a badge failure costs the badge, never the page (`internal/app/nav.go:140-159`).

**Main success scenario**

1. The actor loads any screen.
2. The system renders `aside.sidebar` with the brand block **Fervid Budget / Smart Solutions**, then the permitted groups in fixed order: (untitled) **Home**; **Requests** — *My requests*, *Approvals*, *Accounts queue*, *Recoverables*; **Payments** — *Payments ledger*; **Budget** — *Variance grid*, *Budgets*, *Monthly plans*; **Reports** — *Reports*; **Masters** — *Vendors*, *Projects*, *Heads*; **Admin** — *Users*, *Roles & permissions*, *Configuration*, *Notification rules*, *Audit log*, *Backups*; **Coming soon** — *Invoices*, *Payments received*, *Inventory*, *Purchase orders* (`internal/app/nav.go:52-99`).
3. Items in **Coming soon** render as `a.soon` with `aria-disabled="true"` and a `Soon` chip — announcements, not links (`internal/app/templates.go:64`).
4. The active item gets `class="active"` and `aria-current="page"`.
5. A badge renders only when its count is non-zero: `receipts_missing` on *Payments ledger*, `open_months` on *Monthly plans* (`internal/store/badges.go:32-46`).
6. The sidebar footer shows the actor's initials avatar, their name, and `roleText(User.Role)` — **Admin** or **Data entry** — above the **Log out** form (`internal/app/templates.go:66-68`).
7. At ≤860px the sidebar is replaced by `header.m-topbar` (back control, title, `✉` bell to `/notifications` with an unread dot) and `nav.tabbar`.

**Alternate flows**

- **7.a** The tab bar's centre action is the first permitted candidate in priority order: **Approve** → `/approvals` (`approval:approve`), **Pay** → `/payments/new` (`payment:create`), **New** → `/requests/new` (`request:create`); **Home** is the floor (`internal/app/nav.go:198-227`).
- **7.b** The right side is **Payments** → `/payments` for a holder of `payment:view`, otherwise **Budget** → `/grid`; **More** is always last (`internal/app/nav.go:228-234`).
- **7.c** Tapping **More** opens `.more-sheet`: the actor's name, a `✕` close, the same permitted groups (the untitled group is captioned **Overview** here), and a **Session** group holding **⏻ Log out** (`internal/app/templates.go:83-90`).

**Exception flows**

- **08.e1** The left side's second tab is **Requests** → `/requests` **unconditionally**, because the condition is `routeBuilt("/requests")` and `unbuiltPrefixes` is empty — `request:view` is never consulted (`internal/app/nav.go:217-221`). A role-less user therefore has a tab that answers **403**. The `grid:view` fallback below it is now dead code. Pinned as expected behaviour at `internal/app/nav_test.go:217-228`.
- **08.e2** The nav declares badges `approvals` and `accounts_queue` (`internal/app/nav.go:62-63`) but `badgeSpecs` produces only `my_payments`, `receipts_missing` and `open_months` (`internal/store/badges.go:32-46`). Those two badges never render a number. The comment claiming they "stay dormant until Phase 1 creates the requests table" is stale — the table has existed since migration v3.

**Business rules**

- Nothing in `nav.go` may branch on a role name (`internal/app/nav_test.go:244-254`).
- Every gate must name a pair from the canonical vocabulary; a typo silently hides the item forever (`internal/app/nav_test.go:259-275`).
- Every linked nav item must resolve to a real route (`internal/app/nav_test.go:310`), and the tab bar must never link to an unbuilt one (`:341`).
- Badge counts are memoised per user for 15 s, keyed on the *set* of permitted badges, so revoking a grant changes the key and invalidates the memo (`internal/store/badges.go:14`, `:122-130`).

**Data touched** — reads `user_roles`, `role_permissions`, `role_data_scope`, `payments`, `payment_attachments`, `budget_months`, `notifications`.

**Non-functional / UX notes** — sidebar and tab bar are alternatives; both visible at once is two elements labelled "Primary" and fails the shell test (`tests/e2e/shell.spec.ts:51+`). `ux.spec.ts` discovers its route list from the nav itself, so a nav link that 404s or 403s fails there (`tests/e2e/ux.spec.ts:17-41`) — which is how 08.e1 would be caught for a least-privilege subject, if such a subject were swept.

**Open questions**

- 08.e1 and 08.e2 are both live gaps. The first is a promise the tab bar cannot keep; the second is a badge the design shows and the store never fills.
- PROGRESS.md still says `unbuiltPrefixes` "holds exactly one line, `/admin/notifications`" (PROGRESS.md, Standing rules). The code's list is empty (`internal/app/nav.go:250`). Doc is stale; code is right.

---

### UC-A-09 — A least-privilege session is refused an admin route reached by URL

| | |
|---|---|
| **Goal** | "Hiding a menu item must not be the only thing stopping me." |
| **Primary actor** | AC2 (Requester-only) and AC5 (role-less) |
| **Supporting actors** | AC1 (who granted the role) |
| **Scope / level** | system · subfunction |
| **Trigger** | The actor types an administration URL, or replays a POST |
| **Route(s)** | `GET /roles`, `/users`, `/audit`, `/projects`, `/heads`, `/budgets`, `/months`, `/payments`, `/reports/monthly`, `/configuration`, `/approvals`, `/backups`, `/vendors`; `POST /roles/new`, `POST /users`, … |
| **Permission gate** | each route's own pair (see §0.4); enforced by `RequirePermission` before any handler code |
| **Coverage IDs** | **R6**, D2 |
| **Priority** | critical |

**Preconditions**

1. The actor is signed in.
2. Their effective permission set lacks the route's pair.
3. For a POST, the actor holds a valid CSRF token — otherwise the refusal proves nothing about authorisation (`internal/app/app_integration_test.go:1139-1145`).

**Postconditions (success — i.e. the refusal)**

1. **403**, chrome-less error page, `<h1>Forbidden</h1>`, body **You do not have permission to perform this action.** (`internal/auth/auth.go:143`).
2. Nothing is read from the domain tables and nothing is written; the handler never runs.
3. No audit row — refusals are not audited.

**Postconditions (failure)** — n/a.

**Main success scenario**

1. AC1 creates a user and sets their roles to exactly `[Requester]`, overriding the default assignment.
2. The Requester signs in and requests `GET /roles`.
3. `RequirePermission("role","view", …)` asks `Can` and gets false.
4. The system renders 403 as above.
5. Steps 2–4 repeat identically for `/users`, `/audit`, `/projects`, `/heads`, `/budgets`, `/months`, `/payments`, `/reports/monthly`, `/configuration` and `/approvals` (`internal/app/app_integration_test.go:1131-1137`).
6. The Requester POSTs `/roles/new` with `name=Sneaky` and a valid CSRF token → 403; `POST /users` with an admin payload → 403.
7. The Requester's dashboard exposes no `href="/roles"`, `href="/users"`, `href="/audit"`, `href="/projects"`, `href="/budgets"` and no **Monthly plans** (`internal/app/app_integration_test.go:1147-1153`).

**Alternate flows**

- **1.a** AC5 — a user with **no** role at all — is refused `/requests`, `/payments`, `/users`, `/roles`, `/configuration` and `/audit`, and still gets 200 on `/` (`tests/e2e/audit-smoke.spec.ts:38-56`).
- **2.a** An anonymous caller gets 303 → `/login` instead of 403 (UC-A-04).

**Exception flows**

- **09.e1** — **fixed.** At `30edd6a`, `GET /grid` answered **200** for a Requester because the route was `RequireLogin` only and never consulted `grid:view` (`internal/app/app.go:377`), recorded as expected behaviour in `tests/e2e/audit-smoke.spec.ts:27-29` (F-A-02/F-G-032). As of the 2026-07-27 repair, Wave 3, commit `1fac147` (`docs/qa/results/REPAIR-LOG.md`), the route is `a.auth.RequirePermission("grid", "view", …)` (`internal/app/app.go:431`) and a Requester — who holds no `grid` grant at all — is refused. The Recent Payments panel the grid also carried is gated separately, on `payment:view`, in the handler rather than the template, so `grid:view` alone cannot see payment data.
- **09.e2** `GET /notifications` and `POST /notifications/read` are session-only by design — every row is already scoped to the caller (`internal/app/app.go:408-413`). Not a hole; owned by UC-C.
- **09.e3** `GET /requests/{id}/reservation` is session-only, with the real gate inside `reservationForm` (`internal/app/app.go:456-464`). Owned by UC-B/UC-C.

**Business rules**

- A refusal is decided in exactly one place — `Manager.Can` — for middleware, handlers and rendered controls alike (`internal/auth/auth.go:119-128`).
- A permission-set resolution error is a **denial**, never an absence of policy (`internal/auth/auth.go:111-117`, `internal/store/vendors.go:133-137`).
- Creating a user through the UI assigns the **Accounts** system role by default, so "least privilege" must be constructed deliberately by clearing the checklist (`internal/store/migrations.go:498-510`, `tests/e2e/audit-support.ts:13-29`).

**Data touched** — reads `user_roles`, `role_permissions`, `role_data_scope`, `users`.

**Non-functional / UX notes** — the 403 page carries a request id, so an operator can correlate the refusal with the WARN log line (`internal/app/http_errors.go:80-94`). Because it is chrome-less, the refused actor's only navigation is **Return to dashboard**.

**Open questions**

- **09.e1 was a genuine permission hole measured against R6, now closed.** `grid:view` is granted, revoked and displayed on the roles matrix (`internal/app/permmap.go:142-146`) and consulted by the nav (`internal/app/nav.go:70`) and the dashboard tile, and as of Wave 3 (`1fac147`) it also guards the route: `RequirePermission("grid","view", …)` (`internal/app/app.go:431`).
- **As of `30edd6a`, eight canonical pairs had no enforcement point anywhere: `approval:reassign`, `payment:mark_partial`, `project:create`, `head:create`, `user:create`, `recoverable_category:{view,create,delete}` — plus `grid:view` as above.** The 2026-07-27 repair (`docs/qa/results/REPAIR-LOG.md`, Wave 3, commit `1fac147`) closed four of the eight: `grid:view` now gates `GET /grid` (`internal/app/app.go:431`, F-A-02/F-G-032); `payment:mark_partial` is checked inside the settlement handler when `settlement=partial` (`app.go:833`, F-D-10); `user:create` gates `POST /users` alongside `user:edit` via `a.requireAnyOf("user", []string{"create","edit"}, …)` (`app.go:590`, F-A-07) — the button/route disagreement described below is resolved, not merely described; and `approval:reassign` now gates the new `POST /requests/{id}/reassign-approver` (`app.go:569`, F-A-06/F-C-02, coverage requirement A7). A fifth closed shortly after: **`recoverable_category:delete`** now gates `POST /configuration/recoverable-categories/{id}/delete` (`internal/app/app.go:614`, handler `internal/app/configuration.go:166`, store `DeleteRecoverableCategory` at `internal/store/recoverables.go:260`) — F-E-06, Wave 5. **Still unenforced:** `project:create` and `head:create` (both projects and heads are created through the same `POST /projects`/`POST /heads` the edit form posts to, gated only on `project:edit`/`head:edit`), and `recoverable_category:{view,create}` — only `edit` and now `delete` gate anything, so creating a category still travels the `edit` route at `app.go:608`.

---

## 2. Roles and permissions

### UC-A-10 — Review the roles-and-permissions matrix

| | |
|---|---|
| **Goal** | "Show me what each role may do and which records it may see." |
| **Primary actor** | AC1, or any holder of `role:view` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar **Roles & permissions**, or the dashboard's Administration link **Roles & permissions / Who can do what** |
| **Route(s)** | `GET /roles` · `GET /roles?role={id}` |
| **Permission gate** | `role:view` (`internal/app/app.go:511`) |
| **Coverage IDs** | R1, R2, R3, R7, R8 |
| **Priority** | critical |

**Preconditions**

1. The actor holds `role:view`.
2. At least one `roles` row exists — the four system roles are seeded by migration v1 (`internal/store/migrations.go:38-83`).

**Postconditions (success)**

1. 200 HTML titled `Roles - Fervid Budget`; nothing is written.
2. The selected role is `?role=` when it parses, else the first row of `AllRoles` — ordered `is_system DESC, name`, so **Accounts** is first (`internal/store/permissions.go:218-233`, `internal/app/app.go:1237-1240`).

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. The actor opens `/roles`.
2. The system renders the page banner: eyebrow **Access control**, `<h1>Roles &amp; permissions</h1>`, sub **Roles are data, not code. Create them, copy them, and set what each one may do and see.** For a holder of `role:create` it also renders **Copy this role** and **＋ New role** (`internal/app/templates.go:1544-1554`).
3. The system renders `div.segmented` — one link per role, `?role={id}`, with a `custom` chip on every non-system role (`internal/app/templates.go:1556-1558`).
4. The system renders the role card: `<h2>{role name}</h2>`, a `pill good` reading **{N} users**, and — only when the role is **not** a system role and the actor holds `role:delete` — a **Delete role** button (`internal/app/templates.go:1564-1569`).
5. The card body carries **Role name** (`name="name"`, `required`, and `readonly` when the role is a system role) and **Description** (`name="description"`) (`internal/app/templates.go:1572-1573`).
6. The system renders the desktop matrix `table.perm-table` inside `.table-wrap.d-only`: header **Page**, then the seven columns **View**, **Create**, **Edit**, **Approve**, **Process**, **Cancel**, **Export**, then **Records it can see** (`internal/app/permmap.go:39-47`, `internal/app/templates.go:1581`).
7. Nine rows, in this order: **Payment requests**, **Payments**, **Reservations**, **Recoverables**, **Vendors**, **Vendor bank details**, **Budgets & variance grid**, **Reports**, **Administration** (`internal/app/permmap.go:58-180`).
8. Each cell is `<input type="checkbox" name="cell" value="{row}:{column}">` when the row has at least one canonical action for that column, and a `<span class="muted">—</span>` when it has none — an em dash, never a disabled checkbox, because "a disabled checkbox reads as *off*, which is a different claim" (`internal/app/templates.go:1536-1541`, `:1587`).
9. Under each row sits `tr.perm-advanced` with `<summary>Advanced — every permission behind this row</summary>` enumerating every canonical action as `name="perm" value="{resource}:{action}"`, labelled `{Resource} · {action with underscores as spaces}` (`internal/app/permmap.go:343-348`, `internal/app/templates.go:1592-1595`).
10. The **Records it can see** cell renders `permscope` — radios **None** / **Own** / **Assigned** / **All** named `scope_request` — for the two data-scoped rows, and a static **None** label otherwise (`internal/app/templates.go:1518-1525`, `:1588`).
11. The action bar reads **Changes apply to all {N} users holding this role.**, then **Discard** (a link back to `?role={id}`) and **Save role** (`internal/app/templates.go:1619-1624`).

**Alternate flows**

- **6.a** At ≤860px the same data renders as `div.perm-acc.m-only`: one `.pa-item` per row whose head shows the label and **{Held} of 7**, its body the available cells as checklines, an **Advanced** disclosure, and — for a scoped row — **Records it can see** with `m_scope_request` radios. Its inputs ship `disabled` in the markup and `fervid-app.js` hands over at the 860px breakpoint, so exactly one rendering ever posts (`internal/app/templates.go:1603-1617`, `web/static/fervid-app.js:455-479`).
- **3.a** `?role=` names a role that does not exist → `Role` is the zero value: the card head has an empty `<h2>`, `role_id` posts as `0`, and the matrix renders all-unchecked. `ErrNotFound` is deliberately tolerated (`internal/app/app.go:1241-1245`).
- **3.b** `?role=abc` → `parseID` yields 0 → falls back to the first role.

**Exception flows**

- **10.e1** The actor lacks `role:view` → 403 (UC-A-09).
- **10.e2** The actor holds `role:view` but **not** `role:edit`. The whole matrix, the scope radios and the **Save role** button still render — none of them is gated in the template — and pressing **Save role** answers 403 from `POST /roles`. Verified by comparing `internal/app/templates.go:1560-1624` (no `.Perms.Can` around the form) with `internal/app/app.go:512`.

**Business rules**

- A cell is a **set** of canonical grants; toggling it grants or revokes all of them, and the Advanced disclosure is what preserves fine-grained control (R2) (`internal/app/permmap.go:9-20`).
- Every canonical resource belongs to exactly one row and every canonical action to exactly one cell — asserted total and unambiguous (`internal/app/permmap_test.go:32`).
- `attachment` sits in **Payment requests**; `project` and `head` sit in **Budgets & variance grid** — they are masters of the budget, not of administration (`internal/app/permmap.go:22-31`).
- Enforcement never consults this file: `role_permissions` stays one row per (resource, action) and neither `internal/store` nor `internal/auth` imports `internal/app` (`internal/app/permmap.go:14-17`).
- A cell is `Granted` only when **every** action behind it is held, and `Partial` when some are (`internal/app/permmap.go:329-331`).

**Data touched** — reads `roles`, `role_permissions`, `role_data_scope`, `users`, `user_roles`.

**Non-functional / UX notes** — the desktop table is authoritative with JavaScript off: it scrolls inside `.table-wrap` at any width (`internal/app/templates.go:1536-1539`). `RoleUserCounts` is built by looping every user and querying `UserRoles` per user — one query per user per page load (`internal/app/app.go:1251-1266`); fine at this user count, an N+1 to watch.

**Open questions**

- 10.e2 is the first instance of a pattern that repeats across this whole area: **read routes are gated, write controls are not.** A `role:view`-only actor is shown a fully interactive editor whose Save can only ever 403.
- The Advanced disclosure has `colspan="9"` (`internal/app/templates.go:1591`) while the header row has 9 cells (Page + 7 columns + Records) — correct today, and silently wrong the moment a column is added.

---

### UC-A-11 — Create a role from scratch

| | |
|---|---|
| **Goal** | "I want a new role that starts with nothing, so I can grant exactly what it needs." |
| **Primary actor** | AC1, or any holder of `role:create` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | **＋ New role** on `/roles` |
| **Route(s)** | `POST /roles/new` → 303 `/roles?role={newID}` |
| **Permission gate** | `role:create` + CSRF (`internal/app/app.go:513`) |
| **Coverage IDs** | R1, R9 (the created role is deletable because it is not a system role) |
| **Priority** | high |

**Preconditions**

1. The actor holds `role:create`.
2. The submitted name, trimmed, is non-empty (`internal/store/permissions.go:241-244`).
3. No existing role has the same name case-insensitively (`idx_roles_name_nocase`, `internal/store/migrations.go:52`).

**Postconditions (success)**

1. One `roles` row with `is_system = 0`, the trimmed name, the trimmed description.
2. **No** `role_permissions` and **no** `role_data_scope` rows — the role starts with nothing.
3. One `audit_log` row inside the same transaction: `action='create'`, `entity_type='role'`, `entity_id={newID}`, `summary='Created role {name}'`, `after_json={"id":…,"name":…}` (`internal/store/permissions.go:258`).
4. The browser is on `/roles?role={newID}` with every checkbox clear.

**Postconditions (failure)** — no `roles` row, no audit row; the transaction rolls back (`internal/store/permissions.go:245-249`).

**Main success scenario**

1. The actor presses **＋ New role**; `data-open="role-new"` reveals the overlay and focus moves to its first field (F6).
2. The sheet shows `<h2>New role</h2>` and the sub-line **It starts with no permissions at all.**, with fields **Role name** (`id="nr-name"`, required) and **Description** (`id="nr-desc"`) (`internal/app/templates.go:1629-1640`).
3. The actor types a name and presses **Create role**.
4. `POST /roles/new` runs `CreateRole` inside a transaction, writing the role and its audit row together.
5. The system redirects 303 to `/roles?role={newID}`.
6. The roles screen re-renders with the new role selected, `custom` chip present, **0 users**, and a **Delete role** button available.

**Alternate flows**

- **3.a** The actor presses **Cancel** or `✕`, or presses Escape → the sheet closes and focus returns to **＋ New role**; nothing is posted.

**Exception flows**

- **11.e1** Blank or whitespace-only name → **400**, error page, body `validation failed: role name is required` (`internal/store/permissions.go:242`, surfaced by `friendly` → `err.Error()`, `internal/app/app.go:1636-1637`). The browser's `required` normally prevents this, so it needs a hand-crafted POST.
- **11.e2** The name collides case-insensitively with an existing role → **400**, **A record with this name already exists.** (`classify` → `ErrDuplicate`, `internal/app/app.go:1632-1633`). See UC-A-16.
- **11.e3** No `role:create` → 403 before the handler.
- **11.e4** Missing CSRF → 403, **Your form session expired…**.

**Business rules**

- A created role is never a system role: `is_system` is hard-coded 0 in the INSERT (`internal/store/permissions.go:250`).
- The sheets sit **outside** `#role-form` because HTML forbids nested forms and the create/copy/delete posts must not carry the matrix (`internal/app/templates.go:1627-1628`).
- Role names are unique on `lower(name)`, so uniqueness is decided by the index and not by a check-then-insert race (`internal/store/migrations.go:52`).

**Data touched** — writes `roles(name,description,is_system,created_at,updated_at)`, `audit_log`.

**Non-functional / UX notes** — the overlay is a real modal: Tab is trapped, Escape closes, focus returns to the opener (`web/static/fervid-app.js:144-175`). With JavaScript off the overlay stays `hidden` and there is **no** way to create a role — the sheet is the only affordance.

**Open questions** — a JavaScript-free browser cannot create, copy or delete a role at all, because all three live in `hidden` overlays with no `<noscript>` fallback (unlike the recoverable-categories toggle, which has one — `internal/app/templates.go:3143`).

---

### UC-A-12 — Create a role by copying an existing one

| | |
|---|---|
| **Goal** | "Give me a copy of Accounts that I can adjust, without re-ticking sixty boxes." |
| **Primary actor** | AC1, or any holder of `role:create` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | **Copy this role** on `/roles`, with a role selected |
| **Route(s)** | `POST /roles/{id}/copy` → 303 `/roles?role={newID}` |
| **Permission gate** | `role:create` + CSRF (`internal/app/app.go:514`) |
| **Coverage IDs** | **R5**, R1, R3 |
| **Priority** | high |

**Preconditions**

1. The actor holds `role:create`.
2. `{id}` names an existing role — system or custom, either may be a source.
3. The new name, trimmed, is non-empty and does not collide case-insensitively.

**Postconditions (success)**

1. One `roles` row with `is_system = 0`, the submitted name, and the **source's description** copied verbatim (`internal/store/permissions.go:435`).
2. Every `role_permissions` row of the source is duplicated for the new role (`internal/store/permissions.go:443-445`).
3. Every `role_data_scope` row of the source is duplicated (`internal/store/permissions.go:446-448`).
4. One `audit_log` row: `action='create'`, `entity_type='role'`, `summary='Copied role {source} to {name}'`, `after_json={"id":…,"name":…,"source":…}` (`internal/store/permissions.go:449`).
5. The browser is on the copy's own matrix, which mirrors the source's ticks exactly.

**Postconditions (failure)** — nothing at all: role, grants, scopes and audit are one transaction (`internal/store/permissions.go:422-455`).

**Main success scenario**

1. The actor selects the source role in the `.segmented` strip — the copy always copies **the currently selected role**, because the form's action is `/roles/{{.Role.ID}}/copy` (`internal/app/templates.go:1645`).
2. The actor presses **Copy this role**; the overlay `role-copy` opens showing `<h2>Copy {source name}</h2>` and the sub-line **The copy starts with the same permissions and data scope.**
3. The actor fills **New role name** (`id="cr-name"`, required) and presses **Copy role**.
4. `CopyRole` resolves the source's name, inserts the new role with the source's description, then bulk-copies grants and scopes with `INSERT … SELECT`.
5. The system redirects 303 to the copy's matrix.

**Alternate flows**

- **1.a** The selected role is a **system** role, e.g. Admin. The copy is still permitted and produces a non-system role holding all 66 grants and both `all` scopes — which is how a second all-powerful role is legitimately created.
- **3.a** Cancel / `✕` / Escape → nothing posted.

**Exception flows**

- **12.e1** Blank name → **400**, `validation failed: role name is required` (`internal/store/permissions.go:419-421`).
- **12.e2** Name collides case-insensitively → **400**, **A record with this name already exists.**
- **12.e3** `{id}` does not exist (or is unparseable, giving 0) → **404**, **The requested record was not found.** (`internal/store/permissions.go:429-434`).
- **12.e4** No `role:create` → 403. Note the sheet itself is gated on `role:create` too, so the affordance and the route agree here (`internal/app/templates.go:1551`).

**Business rules**

- The copy is never a system role (`is_system` literal 0 in the SELECT list — `internal/store/permissions.go:435`).
- Copying is atomic across three tables plus the audit row (R5) (`internal/store/permissions.go:417-455`; `TestCopyRoleDuplicatesGrantsAndScopes`).

**Data touched** — reads `roles`, `role_permissions`, `role_data_scope` of the source; writes the same three tables for the copy, plus `audit_log`.

**Non-functional / UX notes** — the sheet does not name the source anywhere except its heading, so on a phone (where the `.segmented` strip may have scrolled) the heading is the only confirmation of what is being copied.

**Open questions** — the copy inherits the source's **description** but not its name, so a copy of *Accounts* reads "Process approved requests and record payments." until someone edits it. Harmless, mildly misleading.

---

### UC-A-13 — Change a role's grants and data scope

| | |
|---|---|
| **Goal** | "Set exactly what this role may do, and which records it may see, and save it once." |
| **Primary actor** | AC1, or any holder of `role:edit` |
| **Supporting actors** | every user holding the role — their access changes on their next request |
| **Scope / level** | system · user-goal |
| **Trigger** | **Save role** on `/roles` |
| **Route(s)** | `POST /roles` → 303 `/roles?role={id}` |
| **Permission gate** | `role:edit` + CSRF (`internal/app/app.go:512`) |
| **Coverage IDs** | **R1**, **R2**, **R3**, R8 |
| **Priority** | critical |

**Preconditions**

1. The actor holds `role:edit`.
2. `role_id` names an existing role.
3. Every submitted `cell` value resolves to a known row and column, and every submitted `perm` value is a pair in the canonical vocabulary.
4. If the role is a system role, the submitted **Role name** is case-insensitively equal to the stored one (the input is `readonly`, so the browser always satisfies this).

**Postconditions (success)**

1. `roles.name`, `roles.description` and `roles.updated_at` are rewritten (`internal/store/permissions.go:293-296`).
2. **The role's entire grant set is replaced**: `DELETE FROM role_permissions WHERE role_id=?` then one INSERT per submitted grant. Anything absent from the POST is revoked (`internal/store/permissions.go:390-397`).
3. The scope set is replaced the same way; an empty value stores **no row at all**, which is the explicit **None** state (`internal/app/app.go:1301-1315`, `internal/store/permissions.go:398-405`).
4. Two `audit_log` rows: `update`/`role`/`Updated role {name}` with before/after names (`internal/store/permissions.go:297`), and `update`/`role`/`Updated permissions for role {name}` with `after_json={"grants":N,"scopes":M}` (`internal/store/permissions.go:409`).
5. Every holder of the role sees the new permissions on their next request — the set is resolved per request with no cache (`internal/auth/auth.go:97-105`).

**Postconditions (failure)** — see the pre-screen rule below: a grant outside the vocabulary is rejected **before** the rename is written, so nothing changes.

**Main success scenario**

1. The actor selects the role in `.segmented`.
2. The actor ticks the **Approve** cell on the **Payment requests** row and the **View** cell on **Reports**, and additionally ticks one Advanced box, `request:view`.
3. The actor sets **Records it can see** to **All** on the Payment requests row.
4. The actor presses **Save role**.
5. `expandCells` turns `requests:approve` into `approval:{approve,reject,return,reassign,accept_partial}` and `reports:view` into `report:view`; the individual `perm` value adds `request:view`. The union is deduplicated and order-independent (`internal/app/app.go:1284-1300`, `internal/app/permmap.go:274-295`).
6. The system screens every grant against `store.ValidGrant` before any write (`internal/app/app.go:1316-1326`).
7. `UpdateRole` writes name and description; `UpdateRolePermissions` replaces grants and scopes atomically.
8. The system redirects 303 to `/roles?role={id}`; the matrix re-renders with exactly 7 grants held and `request=all` scope. Verified end to end at `internal/app/app_integration_test.go:907-947`.

**Alternate flows**

- **2.a** The actor **clears** a cell. Because the save replaces the whole set, clearing the Approve cell removes all five `approval:*` grants behind it (`internal/app/app_integration_test.go:949-967`).
- **2.b** The actor ticks a cell on the desktop table; `fervid-app.js` ticks every Advanced checkbox that cell covers, so the two renderings agree before submission (`web/static/fervid-app.js:479-489`).
- **3.a** At ≤860px the mobile accordion is live and the desktop inputs are disabled, so `scope_request` posts nothing and `m_scope_request` is read as the fallback (`internal/app/app.go:1308-1312`).
- **3.b** The actor selects **None** — value `""` — so no `role_data_scope` row is stored and `Scope("request")` returns `""` for every holder.

**Exception flows**

- **13.e1** A submitted `perm` value is not in the vocabulary (e.g. `payment:read`) → **400**, **That permission does not exist.**, and **nothing is written** — the pre-screen loop exists precisely because `UpdateRole` and `UpdateRolePermissions` are two transactions and a rejection in the second would otherwise leave the first one's rename committed (`internal/app/app.go:1316-1326`; pinned at `internal/app/app_integration_test.go:969-976`).
- **13.e2** A submitted `cell` names an unknown row or column, or a cell with no canonical action → it expands to **nothing** and is silently ignored, so a hand-crafted POST cannot invent a grant (`internal/app/permmap.go:274-295`). See UC-A-17.
- **13.e3** The role is a system role and the submitted name differs by more than case → **403**, **You do not have permission to perform this action.** The specific reason (`system roles cannot be renamed`) is *not* shown, because `respondStoreError` replaces the message for every `ErrForbidden` (`internal/store/permissions.go:290-292`, `internal/app/http_errors.go:191-192`).
- **13.e4** `role_id` names no role → **404** from `UpdateRole`'s `sql.ErrNoRows` branch (`internal/store/permissions.go:284-286`).
- **13.e5** Blank name → **400**, `validation failed: role name is required`.
- **13.e6** A submitted scope value outside `own|assigned|all`, or a scope for an unscoped resource → **400**, `validation failed: invalid data scope …` (`internal/store/permissions.go:372-376`). Not reachable from the screen.

**Business rules**

- `UpdateRolePermissions` **replaces** the whole grant set — it is not a delta (`internal/store/permissions.go:364-366`).
- Grants are re-validated inside the store as well as in the handler; the handler's loop is a guard against a half-applied save, not the security boundary (`internal/app/app.go:1316-1320`).
- The desktop and mobile scope radios must not share a name: same-name radios are one group across the whole form, so enabling the mobile set would silently clear the desktop selection (`internal/app/templates.go:1513-1517`).
- Only the scoped resources get a scope control, and the set the save handler reads is exactly the set the matrix draws (`internal/app/permmap.go:250-268`). `ValidScope` is true only for `request` and `payment` (`internal/store/permissions.go:185`, `:200-205`).

**Data touched** — writes `roles(name,description,updated_at)`, `role_permissions(role_id,resource,action)`, `role_data_scope(role_id,resource,scope)`, `audit_log` ×2.

**Non-functional / UX notes** — the action bar is sticky and reads **Changes apply to all {N} users holding this role.** — the only warning that this is not a private edit. **Discard** is a plain link back to the same URL, so it discards by reloading. At 390px the accordion's `{Held} of 7` badge is the only summary of a row's state.

**Open questions**

- The **Payments** row also owns a scoped resource (`payment`), so `buildPermMatrix` marks it `Scoped` with `ScopeResource = "payment"` (`internal/app/permmap.go:351-355`) and the save reads `scope_payment` — but the seeded roles are the only place `payment=all` is ever set. Confirmed reachable from the screen; simply untested end to end.
- A save with **no** `cell` and **no** `perm` fields (e.g. an accidental empty POST with a valid CSRF token) revokes **every** grant the role holds and returns 303, with no confirmation step. `UpdateRolePermissions` accepts an empty slice without complaint (`internal/store/permissions.go:366-371`).

---

### UC-A-14 — Delete a custom role

| | |
|---|---|
| **Goal** | "This role is no longer used; remove it." |
| **Primary actor** | AC1, or any holder of `role:delete` |
| **Supporting actors** | every user holding the role — they lose everything it granted |
| **Scope / level** | system · user-goal |
| **Trigger** | **Delete role** on the role card of `/roles`, then **Delete role** in the confirmation sheet |
| **Route(s)** | `POST /roles/{id}/delete` → 303 `/roles` |
| **Permission gate** | `role:delete` + CSRF (`internal/app/app.go:515`) |
| **Coverage IDs** | **R9** |
| **Priority** | high |

**Preconditions**

1. The actor holds `role:delete`.
2. `{id}` names an existing role.
3. `roles.is_system = 0`.

**Postconditions (success)**

1. The `roles` row is deleted.
2. Its `role_permissions`, `role_data_scope` and `user_roles` rows are removed by `ON DELETE CASCADE` (`internal/store/migrations.go:53-71`) — foreign keys are on for every pooled connection (PROGRESS.md trap).
3. One `audit_log` row: `action='delete'`, `entity_type='role'`, `summary='Deleted role {name}'`, `before_json={"id":…,"name":…}` (`internal/store/permissions.go:324`).
4. The browser is on `/roles` with the first remaining role selected.
5. Anyone who held only this role is now AC5 — signed in, entitled to nothing.

**Postconditions (failure)** — the role, its grants, its scopes and its assignments are all intact (one transaction, `internal/store/permissions.go:303-328`).

**Main success scenario**

1. The actor selects the custom role.
2. The card head shows **Delete role**; the actor presses it.
3. The overlay `role-delete` opens: `<h2>Delete {name}?</h2>` and the sub-line **{N} users hold this role and will lose everything it grants.** (`internal/app/templates.go:1657-1665`).
4. The actor presses the red **Delete role** (`class="btn danger"`).
5. `DeleteRole` re-reads `name` and `is_system`, deletes the row, writes the audit entry, commits.
6. The system redirects 303 to `/roles`.

**Alternate flows**

- **3.a** Cancel / `✕` / Escape → nothing posted.
- **1.a** The role has users. The delete is **not** blocked — the count in the sheet is the only warning (`internal/store/permissions.go:303-328` performs no usage check).

**Exception flows**

- **14.e1** The role is a system role → **403** (UC-A-15). The button and the overlay are not even rendered (`internal/app/templates.go:1568`, `:1656`).
- **14.e2** `{id}` does not exist → **404**, **The requested record was not found.**
- **14.e3** No `role:delete` → 403 before the handler; the button is also hidden.
- **14.e4** Missing CSRF → 403.

**Business rules**

- System roles cannot be deleted, enforced in the store and not only in the markup (`internal/store/permissions.go:318-320`; `TestDeleteRoleRejectsSystemRole`).
- Deleting a role is not reversible and leaves no backup of its grants beyond the `before_json`, which records only id and name — **not** the grant list.

**Data touched** — deletes `roles`; cascades `role_permissions`, `role_data_scope`, `user_roles`; writes `audit_log`.

**Non-functional / UX notes** — the destructive action uses `.btn.danger` and sits behind a modal, per the design system. The sheet does not name the users who will be affected, only their count.

**Open questions**

- Deleting a role silently strips its holders' access with no notification to them and no mention on the Users screen. The only trace is the audit row.
- Because the audit `before_json` does not carry the grant set, an accidental delete cannot be reconstructed from the audit log.

---

### UC-A-15 — A system role resists deletion and renaming

| | |
|---|---|
| **Goal** | "The four roles the product ships with must not be removable or renameable by accident." |
| **Primary actor** | AC1 (who holds every grant, and still cannot do this) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | A hand-crafted `POST /roles/{systemID}/delete`, or `POST /roles` carrying a changed name for a system role |
| **Route(s)** | `POST /roles/{id}/delete`; `POST /roles` |
| **Permission gate** | `role:delete` / `role:edit` — both of which the actor holds; the refusal comes from the **store** |
| **Coverage IDs** | **R9**, R7 |
| **Priority** | high |

**Preconditions**

1. `roles.is_system = 1` for the target — true for **Requester**, **Manager**, **Accounts** and **Admin** (`internal/store/migrations.go:351-402`).
2. The actor holds the route's grant and a valid CSRF token, so nothing earlier can refuse the request.

**Postconditions (success — i.e. the refusal)**

1. Delete → **403**, **You do not have permission to perform this action.**; the role, its grants, its scopes and its assignments are untouched.
2. Rename → **403**, same message; `roles.name`, `roles.description` and **the grant set** are all untouched, because `UpdateRole` runs before `UpdateRolePermissions` and its failure short-circuits the handler (`internal/app/app.go:1327-1334`).

**Postconditions (failure)** — n/a.

**Main success scenario (delete)**

1. The actor POSTs `/roles/{adminRoleID}/delete` with a valid token.
2. `DeleteRole` reads `name, is_system` inside its transaction, sees `is_system = 1`, and returns `ErrForbidden` wrapping `system roles cannot be deleted` (`internal/store/permissions.go:318-320`).
3. `respondStoreError` maps `ErrForbidden` to 403 and replaces the message with the generic permission wording (`internal/app/http_errors.go:191-192`).
4. Nothing is written. Pinned at `internal/app/app_integration_test.go:990-998`.

**Main success scenario (rename)**

1. The actor POSTs `/roles` with `role_id={adminRoleID}` and `name=Administrators`.
2. `UpdateRole` reads the current name and `is_system`, and since `is_system = 1` and `!strings.EqualFold("Admin","Administrators")`, returns `ErrForbidden` wrapping `system roles cannot be renamed` (`internal/store/permissions.go:290-292`).
3. 403; nothing written.

**Alternate flows**

- **A case-only rename is allowed.** `EqualFold("Admin","ADMIN")` is true, so posting `name=ADMIN` for the Admin role **succeeds** and rewrites `roles.name` to `ADMIN`. The screen cannot do this — the input is `readonly` and therefore always submits the stored value — but a hand-crafted POST can. Verified from `internal/store/permissions.go:290` and `internal/app/templates.go:1572`.
- **The description of a system role is freely editable.** Only the name is protected, so **Save role** on a system role legitimately rewrites its description.
- **The grants of a system role are freely editable.** Nothing in `UpdateRolePermissions` checks `is_system` (`internal/store/permissions.go:366-413`), so an administrator can strip **Admin** of every grant. Combined with UC-A-13's open question about an empty POST, this is the product's only self-lockout path.

**Exception flows**

- **15.e1** No `role:delete` → 403 from the route gate, before the store's own refusal; the two are indistinguishable in the response.
- **15.e2** Missing CSRF → 403 with the CSRF wording instead.

**Business rules**

- System role **names** are immutable because `seedSystemRoles` and `backfillUserRoles` both resolve them by name; a rename would make the next migration seed a duplicate (`internal/store/permissions.go:267-269`).
- Seeding happens only inside migration v1, so an installed database never re-seeds and a later phase that needs a new grant on a system role must add its own migration with `INSERT OR IGNORE` — never a re-seed, which would reset every deliberate administrator change (`internal/store/migrations.go:295-312`, PROGRESS.md trap).

**Data touched** — reads `roles`; writes nothing.

**Non-functional / UX notes** — the screen hides both affordances for a system role, so the refusal is normally unreachable: the **Delete role** button is rendered only when `not .Role.IsSystem`, and the whole `role-delete` overlay is wrapped in the same condition (`internal/app/templates.go:1568`, `:1656-1666`).

**Open questions**

- The 403 body says **You do not have permission to perform this action.** for an administrator who holds every permission. The store knows the real reason; `respondStoreError` throws it away. A test asserting on "system role" text will fail.
- Nothing prevents an administrator from stripping the **Admin** role of `role:edit` and `user:edit`, after which no one can restore it through the UI. There is no bootstrap re-seed path (see 15's business rules).

---

### UC-A-16 — Name a role the same as an existing one, differing only by case

| | |
|---|---|
| **Goal** | "Two roles called *Accounts* and *accounts* would be a trap; the system should say no." |
| **Primary actor** | AC1, or any holder of `role:create` / `role:edit` |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | **Create role** / **Copy role** / **Save role** with a name that already exists in a different case |
| **Route(s)** | `POST /roles/new`, `POST /roles/{id}/copy`, `POST /roles` |
| **Permission gate** | `role:create` or `role:edit` — held; the refusal is a database constraint |
| **Coverage IDs** | R1, R8 |
| **Priority** | medium |

**Preconditions**

1. A `roles` row exists whose `lower(name)` equals the submitted name lower-cased.
2. The actor holds the relevant grant and a valid CSRF token.

**Postconditions (success — i.e. the refusal)**

1. **400** with the chrome-less error page, body **A record with this name already exists.** (`friendly` → `ErrDuplicate`, `internal/app/app.go:1632-1633`).
2. No new `roles` row; on the create/copy paths the whole transaction rolls back, so no grants and no audit row.
3. On the **Save role** path the rename is refused **before** `UpdateRolePermissions` runs, so the grant set is unchanged too (`internal/app/app.go:1327-1334`).

**Postconditions (failure)** — n/a.

**Main success scenario**

1. The actor opens **＋ New role** and types `accounts` into **Role name**.
2. The system runs `INSERT INTO roles(name,description,is_system) VALUES('accounts','',0)`.
3. SQLite rejects it on `idx_roles_name_nocase` (`internal/store/migrations.go:52`).
4. `classify` sees `UNIQUE` in the driver message and wraps the error as `ErrDuplicate` (`internal/store/store.go:1712-1720`).
5. `respondStoreError` answers **400** with the duplicate wording.
6. The overlay is gone — the response is a full error page, not a re-render of the sheet — so the actor loses what they typed and must press **Go back**.

**Alternate flows**

- **1.a** Renaming an existing **custom** role to a name that collides case-insensitively with any other role, system or custom → identical 400 (`UpdateRole` → `classify`, `internal/store/permissions.go:293-296`).
- **1.b** Copying a role into a colliding name → identical 400 (`internal/store/permissions.go:435-438`).
- **1.c** The names differ only by **surrounding whitespace**: both `CreateRole` and `UpdateRole` trim first, so ` Accounts ` also collides (`internal/store/permissions.go:241`, `:271`).

**Exception flows**

- **16.e1** A name differing by a Unicode case fold SQLite's `lower()` does not know (it is ASCII-only) — for example a Turkish dotless *ı* — would **not** collide. Unverified against a live database; recorded as a theoretical gap in the index's reach.

**Business rules**

- Uniqueness is enforced by the index, deliberately, because check-then-insert is a race (`internal/store/vendors.go:429-431` states the same rule for vendors).
- `classify` detects a duplicate by substring-matching `"UNIQUE"` in the driver's error text (`internal/store/store.go:1716`) — a string dependency on the SQLite driver's wording.

**Data touched** — reads/writes `roles` (rolled back).

**Non-functional / UX notes** — because the failure renders the error page instead of the sheet, a duplicate name is a *lossy* failure: the typed name and description are gone. Contrast the vendor form, which re-renders with the input preserved (`internal/app/vendors.go:109-112`).

**Open questions**

- Should a duplicate name re-render the roles screen with the sheet open and the error inline, as the vendor form does? Today it is a full-page 400. This is a UX divergence between two screens in the same admin area.

---

### UC-A-17 — A hand-crafted role POST cannot invent a permission

| | |
|---|---|
| **Goal** | "Someone who can edit roles must not be able to grant a permission the product has never heard of." |
| **Primary actor** | AC1, or any holder of `role:edit` |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | `POST /roles` carrying a `perm` or `cell` value the vocabulary does not define |
| **Route(s)** | `POST /roles` |
| **Permission gate** | `role:edit` + CSRF — both held |
| **Coverage IDs** | **R2**, **R8**, R1 |
| **Priority** | high |

**Preconditions**

1. The actor holds `role:edit` and a valid CSRF token.
2. `role_id` names an existing role.

**Postconditions (success — i.e. the refusal or the silent drop)**

1. An unknown `perm` pair → **400**, **That permission does not exist.**, and **nothing at all is written** — not the name, not the description, not the grants (`internal/app/app.go:1321-1326`).
2. An unknown `cell` value → the cell expands to nothing and is ignored; the save proceeds with whatever else was submitted (`internal/app/permmap.go:274-295`).

**Postconditions (failure)** — n/a.

**Main success scenario**

1. The actor POSTs `role_id={id}`, `name=Reviewer`, `perm=payment:read`.
2. `rolesSave` splits the value into `{Resource: "payment", Action: "read"}` and appends it to the grant list.
3. The pre-screen loop calls `store.ValidGrant("payment","read")`, which finds `read` absent from `resourceActions["payment"]` (`internal/store/permissions.go:153`, `:187-198`).
4. The system answers **400** with **That permission does not exist.** and returns before `UpdateRole` is called.
5. Pinned at `internal/app/app_integration_test.go:969-976`.

**Alternate flows**

- **2.a** The `perm` value has no `:` separator → skipped by the `len(parts) != 2` guard, silently (`internal/app/app.go:1289-1293`).
- **2.b** The `cell` value names an unknown row (`cell=nonsense:view`) → `permGroup` returns false and the value is dropped (`internal/app/permmap.go:282-285`).
- **2.c** The `cell` value names a real row and a column that row has no action for (`cell=reservations:view` — Reservations has no View) → `group.Cells["view"]` is empty and nothing is granted (`internal/app/permmap.go:100-104`, `:286-292`).
- **2.d** A duplicate grant arrives twice, once via a cell and once via Advanced → deduplicated by the `seen` map, and by `role_permissions`' composite primary key even if it were not (`internal/app/app.go:1285-1300`, `internal/store/migrations.go:54-59`).

**Exception flows**

- **17.e1** A grant that survives the handler's pre-screen but fails the store's own `ValidGrant` → `400`, `validation failed: unknown permission {r}:{a}` (`internal/store/permissions.go:367-371`). Not reachable today, because both checks call the same function.
- **17.e2** An unknown scope value for a scoped resource, or any scope for an unscoped one → `400`, `validation failed: invalid data scope {r}={s}` (`internal/store/permissions.go:372-376`).

**Business rules**

- The canonical vocabulary is **21 resources / 66 (resource, action) pairs**, declared once in `internal/store/permissions.go:150-183`. No phase may invent a verb; `TestPermissionVocabularyIsCanonical` pins the count and asserts every key of `resourceActions` appears exactly once in `resourceOrder`.
- Wildcards do not exist: `"*"` was a Casbin artefact and a stale wildcard row is exactly what would grant everything to everyone (`internal/store/permissions.go:73-75`).
- Validation happens twice by design: once in the handler to prevent a half-applied save, once in the store as the security boundary (`internal/app/app.go:1316-1320`).

**Data touched** — reads `roles`; writes nothing on the refusal path.

**Non-functional / UX notes** — this path is unreachable from the screen: the matrix only ever emits values `permmap.go` generated. It exists so that a forged POST is answered by policy, not by luck.

**Open questions**

- 17.e1 and 17.e2 are unreachable duplicates of the handler's checks. Keeping them is correct (defence in depth) but means their error wording is untestable through HTTP.

---

## 3. Users

### UC-A-18 — Review the users list

| | |
|---|---|
| **Goal** | "Who has an account, what can they do, and who approves their requests?" |
| **Primary actor** | AC1, or any holder of `user:view` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar **Users**, or the dashboard's **Users / People and their default approvers** |
| **Route(s)** | `GET /users` |
| **Permission gate** | `user:view` (`internal/app/app.go:509`) |
| **Coverage IDs** | **R4**, R7 |
| **Priority** | high |

**Preconditions**

1. The actor holds `user:view`.
2. At least one `users` row exists — `EnsureUser` guarantees the bootstrap administrator at start-up (`internal/app/app.go:315-321`).

**Postconditions (success)** — 200 HTML titled `Users - Fervid Budget`; nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. The actor opens `/users`.
2. The system loads every user (ordered by name), every role, and each user's role set — one `UserRoles` query per user (`internal/app/app.go:1127-1164`).
3. The system renders the banner: eyebrow **Access**, `<h1>Users</h1>`, sub **A person can hold several roles at once.**, and — for a holder of `user:create` — **＋ Add user** (`internal/app/templates.go:1189-1196`).
4. The system renders `table.t-cards` with headers **Name**, **Email**, **Roles**, **Default approver**, **Status**, **Edit** (`internal/app/templates.go:1200`).
5. Each row shows the name, the email, one `pill neutral no-dot` per assigned role, the default approver's **name** (or an em dash), an **Active** / **Inactive** pill, and an **Edit** button opening that user's sheet (`internal/app/templates.go:1202-1213`).
6. With no users at all the table shows **No users yet.**

**Alternate flows**

- **5.a** At ≤860px the table restacks into cards; every `<td>` carries `data-label`, including the trailing action cell (`internal/app/templates.go:1180-1182`).

**Exception flows**

- **18.e1** No `user:view` → 403 (UC-A-09).
- **18.e2** The actor holds `user:view` but not `user:edit`. Every row still shows an **Edit** button and the sheets still render fully; **Save user** answers 403. The button is not gated (`internal/app/templates.go:1209`).
- **18.e3** A `UserRoles` query fails → `respondStoreError`, i.e. the whole screen becomes an error page (`internal/app/app.go:1146-1150`).

**Business rules**

- The **Roles** cell iterates `AllRoles` and prints those in the user's set, so role chips always appear in the canonical order `is_system DESC, name` (`internal/app/templates.go:1206`).
- The approver column resolves the id through `ApproverNames`, which is built from every user — including inactive ones — so a deactivated approver still displays by name (`internal/app/app.go:1138-1145`).
- `Approvers` (the select's option list) contains only **active** users (`internal/app/app.go:1141-1143`).

**Data touched** — reads `users`, `roles`, `user_roles`.

**Non-functional / UX notes** — one `UserRoles` query per user, plus one per user again on `/roles` for the counts: an N+1 on both admin screens. `reassignCandidates` has the same shape elsewhere and PROGRESS.md already flags it. Editing lives in one overlay sheet per user, all of them present in the DOM at once — with 200 users that is 200 sheets in the markup.

**Open questions**

- There is no pagination, search or filter on `/users`. `ListUsers` has no LIMIT (`internal/store/store.go:109-124`).

---

### UC-A-19 — Add a user through the Add user sheet

| | |
|---|---|
| **Goal** | "Create an account for a new colleague." |
| **Primary actor** | AC1, or any holder of `user:edit` |
| **Supporting actors** | the new user, who will sign in with the password set here |
| **Scope / level** | system · user-goal |
| **Trigger** | **＋ Add user** on `/users` |
| **Route(s)** | `POST /users` (no `id` field) → 303 `/users` |
| **Permission gate** | `user:edit` + CSRF (`internal/app/app.go:510`). The **button** is gated on `user:create` (`internal/app/templates.go:1195`) — the two disagree |
| **Coverage IDs** | R4, R7 |
| **Priority** | critical |

**Preconditions**

1. The actor holds `user:edit`.
2. The submitted email is non-empty, contains `@`, and is unique after lower-casing and trimming (`internal/store/store.go:1664-1670`, `users.email` unique).
3. The submitted name is non-empty.
4. `role` is `data_entry` or `admin` (or empty, which defaults to `data_entry`) (`internal/store/store.go:60-62`, `:1668-1670`).
5. A password is supplied, at least **12** characters (`internal/app/app.go:1555-1560`), containing at least one letter and one digit (`internal/auth/auth.go:47-60`).

**Postconditions (success)**

1. One `users` row: lower-cased email, trimmed name, bcrypt hash, legacy `role`, `active` from the checkbox.
2. **One `user_roles` row assigning a system role derived from the legacy role field**: `Admin` when `role == "admin"`, otherwise **Accounts** (`internal/store/migrations.go:498-510`).
3. One `audit_log` row: `action='create'`, `entity_type='user'`, `entity_id={newID}`, `summary='Created user {email}'` (`internal/app/app.go:1222-1227`).
4. **No** `role_ids` are applied and **no** default approver is set — both are skipped on the create path (`internal/app/app.go:1204-1221`).
5. The browser is back on `/users` with the new row present.

**Postconditions (failure)** — no `users` row, no `user_roles` row, no audit row; `CreateUser` is one transaction (`internal/store/store.go:63-83`).

**Main success scenario**

1. The actor presses **＋ Add user**; the overlay `user-new` opens with `<h2>Add user</h2>` and the sub-line **They can be given more roles once they exist.** (`internal/app/templates.go:1248-1263`).
2. The actor fills **Email** (`id="nu-email"`, `type="email"`, required), **Name** (`id="nu-name"`, required), chooses **Role** — a select offering **Data entry** and **Admin** — and fills **Password** (`id="nu-pw"`, `type="password"`, `minlength="12"`, required). **Active** is checked by default.
3. The actor presses **Add User**.
4. `userSave` sees no `id`, validates the password length, hashes it, and calls `CreateUser`.
5. `CreateUser` normalises and validates, inserts the row, and calls `assignDefaultRoleTx`.
6. The system writes the `create` audit row and redirects 303 to `/users`.

**Alternate flows**

- **2.a** The actor chooses **Admin** → the user is given the **Admin** system role and therefore every one of the 66 grants immediately.
- **2.b** The actor clears **Active** → `active = 0`; the account exists but every sign-in answers **Invalid email or password** (UC-A-01, e1).
- **3.a** Cancel / `✕` / Escape → nothing posted.

**Exception flows**

- **19.e1** No password → **400**, **A password is required.** (`internal/app/app.go:1186-1189`).
- **19.e2** Password under 12 characters → **400**, `password must be at least 12 characters` (`internal/app/app.go:1171-1174`, `:1555-1560`). `minlength="12"` normally catches it in the browser.
- **19.e3** A 12-character password with **no digit**, or no letter — e.g. `abcdefghijkl` — passes `validatePassword` and then fails `auth.HashPassword`, which answers **500**, **The password could not be secured.** (`internal/app/app.go:1175-1180`, `internal/auth/auth.go:37-60`). **A validation failure is reported as a server error.**
- **19.e4** Email missing `@`, or a blank name → **400** with `validation failed: email and name are required` (`internal/store/store.go:1664-1667`).
- **19.e5** `role` is neither `admin` nor `data_entry` → **400**, `validation failed: invalid user role`. Not reachable from the select.
- **19.e6** Duplicate email → **400**, **A record with this name already exists.** — the duplicate message is written for named records and reads oddly for an email (`internal/store/store.go:70-72` → `classify` → `friendly`).
- **19.e7** Missing CSRF → 403.

**Business rules**

- The legacy `users.role` column survives because `UpdateUser` still validates it and `RequireAnotherActiveAdmin` still guards it; the RBAC roles are the real authority (`internal/app/templates.go:1183-1184`).
- The create path relies on `CreateUser`'s default-role assignment rather than posting `role_ids` (`internal/app/app.go:1204-1206`).
- Passwords are never echoed; the field posts and is discarded.

**Data touched** — writes `users`, `user_roles`, `audit_log`.

**Non-functional / UX notes** — the sheet is a modal with focus trapping (F6). With JavaScript off the overlay never opens and there is no other way to add a user.

**Open questions**

- **A "Data entry" user silently becomes an accountant.** `assignDefaultRoleTx` grants the **Accounts** system role, which carries `payment:{create,edit,void,process,settle,mark_partial,hold}`, `reservation:{reserve,release}` and `request:view` with scope `all`. Nothing on the sheet says so; the **Role** select says "Data entry". This trap is documented in the QA harness (`tests/e2e/audit-support.ts:13-29`) precisely because it turns every expected refusal into a 200.
- The route requires `user:edit` while the button requires `user:create`. A holder of `user:create` alone sees the button and gets 403; a holder of `user:edit` alone sees no button but can create by POST. `user:create` is enforced nowhere.
- 19.e3 (a 500 for a weak password) is a genuine defect: two validators disagree about what a valid password is, and the second one's failure is not treated as user error.

---

### UC-A-20 — Edit a user and assign several roles

| | |
|---|---|
| **Goal** | "This person is both a requester and a manager; give them both roles." |
| **Primary actor** | AC1, or any holder of `user:edit` |
| **Supporting actors** | the edited user, whose access changes on their next request |
| **Scope / level** | system · user-goal |
| **Trigger** | **Edit** on a row of `/users` |
| **Route(s)** | `POST /users` with `id={userID}` → 303 `/users` |
| **Permission gate** | `user:edit` + CSRF |
| **Coverage IDs** | **R4**, R3 |
| **Priority** | critical |

**Preconditions**

1. The actor holds `user:edit`.
2. `id` names an existing user.
3. Every submitted `role_ids` value names an existing role (`internal/store/permissions.go:471-479`).
4. The edit does not remove the last active administrator (UC-A-23).

**Postconditions (success)**

1. `users.name`, `users.role`, `users.active`, `users.updated_at` are rewritten; `password_hash` only when a new password was supplied (`internal/store/store.go:126-142`).
2. **The user's whole role assignment is replaced**: `DELETE FROM user_roles WHERE user_id=?` then one INSERT per unique submitted id (`internal/store/permissions.go:480-487`).
3. `users.default_approver_id` is set or cleared (UC-A-21).
4. **Three** `audit_log` rows for one save: `Updated role assignment (N roles)` (`internal/store/permissions.go:488`), `Updated default approver for {name}` (`internal/store/permissions.go:551`), and `Updated user {email}` (`internal/app/app.go:1222-1227`).
5. The user's effective permission set is the **union** of every assigned role's grants, with the broadest scope per scoped resource (`internal/store/permissions.go:557-598`).

**Postconditions (failure)** — see the ordering caveat below; the save is **not** atomic across its three store calls.

**Main success scenario**

1. The actor presses **Edit** on the row; the overlay `user-{id}` opens with `<h2>{name}</h2>` and the email as the sub-line (`internal/app/templates.go:1219-1245`).
2. The sheet shows **Name**, then a **Roles** block of checklines — one per role, labelled `{role name} — {description}`, pre-checked from the current assignment — then **Default approver for their own requests**, **Reset password**, and an **Active** checkline.
3. The actor ticks **Requester** and **Manager** and presses **Save user**.
4. `userSave` calls `UpdateUser` (name, legacy role, active, optional hash), then `SetUserRoles` with the parsed ids, then `SetUserDefaultApprover`.
5. The system writes the `update` audit row and redirects 303 to `/users`.
6. The row now shows two role chips, and the user can both create requests and approve them (`internal/app/app_integration_test.go:1053-1079`).

**Alternate flows**

- **3.a** The actor clears **every** role checkbox → `role_ids` is absent from the POST, `roleIDs` is nil, and `SetUserRoles` deletes the whole set: the user becomes AC5, signed in and entitled to nothing. This is how the QA harness builds an exact-role subject (`tests/e2e/audit-support.ts:53-79`).
- **3.b** The same role id is submitted twice → deduplicated by the `unique` map (`internal/store/permissions.go:461-464`).
- **2.a** The **Email** is not editable; it rides along as a hidden input purely so the audit summary can name it (`internal/app/templates.go:1224`). `UpdateUser` validates against a placeholder address, so email is structurally unchangeable (`internal/store/store.go:128`).
- **2.b** The legacy **Role** is not editable either; the sheet posts it as a hidden field carrying the stored value (`internal/app/templates.go:1239`).

**Exception flows**

- **20.e1** A `role_ids` value names no role → **400**, `validation failed: role N does not exist`, and **no role change is applied** — the existence check runs before any write inside the transaction (`internal/store/permissions.go:471-479`).
- **20.e2** `id` names no user → `UpdateUser`'s `RequireAnotherActiveAdmin` returns `ErrNotFound` → **404** (`internal/store/store.go:150-155`).
- **20.e3** The actor edits **their own** row and the hidden legacy `role` is not `admin` → **400**, **You cannot deactivate or demote your own administrator account.** See UC-A-23; this fires for any non-admin editing themselves.
- **20.e4** `UpdateUser` succeeds but `SetUserRoles` fails → **the name/active/password change is already committed** while the role change is not. `userSave` calls three independent store methods with no enclosing transaction (`internal/app/app.go:1191-1221`).
- **20.e5** Missing CSRF → 403.

**Business rules**

- Access is the **union** of all assigned roles; scope is the broadest of them (`all > assigned > own`) (`internal/store/permissions.go:117-137`, `:557-560`; R4).
- `SetUserRoles` replaces the whole set, so the checkbox state at save time is authoritative (`tests/e2e/audit-support.ts:19-23`).
- The permission set is resolved per request, so a role change takes effect on the edited user's next click with no re-login (`internal/auth/auth.go:97-105`).

**Data touched** — writes `users(name,role,active,password_hash,default_approver_id,updated_at)`, `user_roles`, `audit_log` ×3.

**Non-functional / UX notes** — role descriptions appear inline in the checkline label, which is the only place the seeded descriptions are surfaced to an administrator. At 390px the sheet is full-height with `.sh-foot` holding **Cancel** and **Save user**.

**Open questions**

- 20.e4: one logical save spans three transactions. A failure in the middle leaves a user renamed but with the wrong roles, and the audit log shows only the parts that succeeded.
- Because the legacy `role` is a hidden field, a hand-crafted POST can flip a user between `data_entry` and `admin` — which changes what `RequireAnotherActiveAdmin` protects and what `roleText` displays in the sidebar, but **not** their actual permissions.

---

### UC-A-21 — Set a user's default approver

| | |
|---|---|
| **Goal** | "Route this person's requests to their manager by default." |
| **Primary actor** | AC1, or any holder of `user:edit` |
| **Supporting actors** | the chosen approver; UC-B consumes this value when a request is raised |
| **Scope / level** | system · subfunction |
| **Trigger** | **Default approver for their own requests** in the user's edit sheet |
| **Route(s)** | `POST /users` with `default_approver_id` → 303 `/users` |
| **Permission gate** | `user:edit` + CSRF |
| **Coverage IDs** | R4 (nearest); adoption-spec **G9** / **D9** |
| **Priority** | high |

**Preconditions**

1. The actor holds `user:edit`.
2. `id` names an existing user (`internal/store/permissions.go:526-532`).
3. `default_approver_id` is `0`, or names an existing user who is **active** and is **not** the same user (`internal/store/permissions.go:516-546`).

**Postconditions (success)**

1. `users.default_approver_id` is the chosen id, or NULL when `0` was submitted (`internal/store/permissions.go:533-548`).
2. `users.updated_at` is refreshed.
3. One `audit_log` row: `action='update'`, `entity_type='user'`, `summary='Updated default approver for {name}'`, `after_json={"default_approver_id":N}`.
4. The **Default approver** column on `/users` shows the approver's name.

**Postconditions (failure)** — `default_approver_id` is unchanged (one transaction, `internal/store/permissions.go:520-555`).

**Main success scenario**

1. The actor opens the user's edit sheet.
2. The system renders the select `id="u-apr-{id}"`, `name="default_approver_id"`, whose first option is **None** (`value="0"`), followed by every **active** user **except this one**, each carrying `data-approver-for="{selfID}"`, with the current choice pre-selected (`internal/app/templates.go:1231-1237`).
3. Below the select the hint reads **Self-approval is not allowed, so they never appear in their own list.**
4. The actor chooses an approver and presses **Save user**.
5. `SetUserDefaultApprover` re-checks that the approver exists and is active, writes the column, and audits.
6. `/users` shows the approver's name in the row.

**Alternate flows**

- **4.a** The actor chooses **None** → `parseID("0")` is 0 → the column is set to NULL and the row shows an em dash.
- **2.a** All other users are inactive → the select offers only **None**.

**Exception flows**

- **21.e1** The submitted approver **is** the user → **400**, `validation failed: a user cannot be their own default approver`, and the stored value is unchanged. Enforced server-side because "a hidden option is not validation" (`internal/store/permissions.go:512-519`; pinned at `internal/app/app_integration_test.go:1081-1093`).
- **21.e2** The approver does not exist → **400**, `validation failed: the chosen approver does not exist`.
- **21.e3** The approver exists but is inactive → **400**, `validation failed: the chosen approver is not an active user` (`internal/store/permissions.go:543-545`).
- **21.e4** `id` names no user → **404**.

**Business rules**

- **G8 / self-approval:** nobody may approve their own request, and the default-approver rule is the first line of that — enforced in the store, not in the `<select>` (`internal/store/permissions.go:512-515`).
- The approver need hold **no** approval grant. `SetUserDefaultApprover` checks only existence and active status, so a Requester can be set as another Requester's default approver and the resulting request would sit with someone who cannot decide it. Verified: there is no permission check in `internal/store/permissions.go:516-555`.
- Deactivating a user does **not** clear the rows that name them as default approver; `UpdateUser` touches no other row (`internal/store/store.go:126-142`).

**Data touched** — writes `users(default_approver_id,updated_at)`, `audit_log`.

**Non-functional / UX notes** — the hint explains an absence, which is the right pattern for a control whose omission would otherwise look like a bug.

**Open questions**

- A default approver who holds no `approval:approve` grant is accepted. Should the option list be filtered to holders of `approval:approve`? That would need a permission query per candidate — the same cost `reassignCandidates` already pays (PROGRESS.md, Known gaps).
- Deactivating a user leaves stale `default_approver_id` references. `/users` renders their name from `ApproverNames`, which includes inactive users, so the screen looks correct while the routing target is unusable.

---

### UC-A-22 — Reset a user's password

| | |
|---|---|
| **Goal** | "They've forgotten it; give them a new one." |
| **Primary actor** | AC1, or any holder of `user:edit` |
| **Supporting actors** | the user, who must be told the new password out of band |
| **Scope / level** | system · subfunction |
| **Trigger** | **Reset password** in the user's edit sheet |
| **Route(s)** | `POST /users` with a non-empty `password` → 303 `/users` |
| **Permission gate** | `user:edit` + CSRF |
| **Coverage IDs** | none (predates the matrix) |
| **Priority** | high |

**Preconditions**

1. The actor holds `user:edit`.
2. The submitted password is at least 12 characters and contains at least one letter and one digit.
3. `id` names an existing user.

**Postconditions (success)**

1. `users.password_hash` is a fresh bcrypt hash; `updated_at` is refreshed (`internal/store/store.go:134-138`).
2. The failure counters and lock are **not** cleared — `UpdateUser` does not touch `attempt_count` or `locked`.
3. One `audit_log` row: `Updated user {email}`. The row does **not** record that the password changed; `before_json`/`after_json` are absent (`internal/app/app.go:1222-1227`).
4. Any existing session of that user **continues to work** — sessions are stateless HMAC cookies, so changing the password does not invalidate them (`internal/auth/auth.go:202-232`).

**Postconditions (failure)** — the old hash stands.

**Main success scenario**

1. The actor opens the user's edit sheet.
2. The system renders **Reset password** (`id="u-pw-{id}"`, `name="password"`, `type="password"`, `minlength="12"`) with the placeholder **Leave blank to keep the current one** (`internal/app/templates.go:1238`).
3. The actor types a new password and presses **Save user**.
4. `userSave` validates the length, hashes it, and passes the hash to `UpdateUser`, which includes `password_hash` in the UPDATE only because the hash is non-empty.
5. The system redirects 303 to `/users`.

**Alternate flows**

- **3.a** The field is left blank → `hash` stays `""` and the UPDATE omits `password_hash` entirely; nothing about the password changes (`internal/app/app.go:1169-1181`, `internal/store/store.go:134-141`).

**Exception flows**

- **22.e1** Under 12 characters → **400**, `password must be at least 12 characters`.
- **22.e2** Twelve or more characters but no digit, or no letter → **500**, **The password could not be secured.** (see UC-A-19, 19.e3). The same defect on the reset path.
- **22.e3** A locked-out user gets a new password and is **still locked** for the rest of the 15-minute window, because the reset does not clear `users.locked`.

**Business rules**

- The app-level minimum is 12 characters (`internal/app/app.go:1555-1560`); the auth-level minimum is 8 plus a letter and a digit (`internal/auth/auth.go:47-60`). The effective rule is the intersection: **≥12, at least one letter, at least one digit**.
- The bootstrap administrator password (`FERVID_ADMIN_PASSWORD`, default `admin123`) is only 8 characters and passes `HashPassword` — which is why the lower bound exists at that layer (`internal/auth/auth.go:45-46`, `internal/config/config.go:37`).

**Data touched** — writes `users(password_hash,updated_at)`, `audit_log`.

**Non-functional / UX notes** — there is no "confirm password" field and no generated-password affordance; the administrator must transmit the value themselves. The field's placeholder is the only documentation of the blank-means-keep rule.

**Open questions**

- Resetting a password does not end the user's other sessions and does not clear a lockout. Both are reasonable to want; neither is implemented.
- The audit row for a password reset is indistinguishable from a rename: same action, same summary, no before/after. An auditor cannot tell that a credential changed.

---

### UC-A-23 — The last active administrator cannot be demoted or deactivated

| | |
|---|---|
| **Goal** | "Don't let me lock everybody out of the product." |
| **Primary actor** | AC1 |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | Saving a user whose legacy role is `admin` and who is currently active, with `active` cleared or the legacy role changed |
| **Route(s)** | `POST /users` |
| **Permission gate** | `user:edit` + CSRF — held; the refusal comes from the handler and the store |
| **Coverage IDs** | none (predates the matrix) |
| **Priority** | high |

**Preconditions**

1. `id` names an existing user.
2. For the store-level guard: that user's current `role = 'admin'` and `active = 1`, and the proposed state is not (`admin` and active) (`internal/store/store.go:147-158`).
3. For the handler-level guard: `id` equals the caller's own id (`internal/app/app.go:1192-1196`).

**Postconditions (success — i.e. the refusal)**

1. **400** with the chrome-less error page.
2. Self-edit path: **You cannot deactivate or demote your own administrator account.**
3. Last-admin path: `validation failed: at least one active administrator is required` (`internal/store/store.go:164`).
4. Nothing is written — the handler returns before `UpdateUser`, or `UpdateUser` returns before its UPDATE.

**Postconditions (failure)** — n/a.

**Main success scenario (self-edit guard)**

1. The signed-in administrator opens **their own** row and clears **Active**.
2. `userSave` sees `id == current.ID` and `active == false` and answers 400 with the self-edit message, before any store call (`internal/app/app.go:1192-1196`).

**Main success scenario (last-admin guard)**

1. The actor opens the row of the **only** active `admin`-legacy user — not themselves — and clears **Active**.
2. `UpdateUser` calls `RequireAnotherActiveAdmin`, which counts other active admins and finds none.
3. **400**, `validation failed: at least one active administrator is required`. Nothing written.

**Alternate flows**

- **A second active admin exists** → both guards pass on the non-self path and the user is deactivated normally.
- **The target's legacy role is `data_entry`** → `RequireAnotherActiveAdmin` returns nil immediately, because the guard only protects rows whose current role is `admin` and active (`internal/store/store.go:156-158`).

**Exception flows**

- **23.e1 (defect)** A **non-admin** user holding `user:edit` who edits **their own** row — even to change nothing but their name — is refused. The edit sheet posts the stored legacy role as a hidden field, so for a `data_entry` user `r.FormValue("role")` is `"data_entry"`, the condition `id == current.ID && (!active || role != "admin")` is true, and they get **400 You cannot deactivate or demote your own administrator account.** Verified from `internal/app/app.go:1192-1196` and `internal/app/templates.go:1239`. The message is also wrong: they have no administrator account.
- **23.e2** The last-admin guard counts `users.role = 'admin'`, the **legacy** column — not holders of the RBAC `Admin` role. A database whose only holder of the RBAC `Admin` role has legacy role `data_entry` is not protected at all (`internal/store/store.go:159-162`).

**Business rules**

- `RequireAnotherActiveAdmin` is deliberately exported so a caller can warn before showing a confirmation form (`internal/store/store.go:144-147`) — no caller does.
- The guard is a **count of other rows**, evaluated outside any transaction, so two simultaneous demotions could each see the other as "another admin". A real but very narrow race (`internal/store/store.go:147-167`).

**Data touched** — reads `users(role,active)`; writes nothing on the refusal path.

**Non-functional / UX notes** — both refusals are full error pages, so the actor loses everything they typed in the sheet.

**Open questions**

- 23.e1 is a live defect with a misleading message. Any non-admin user granted `user:edit` cannot edit their own record.
- 23.e2 means the safety net protects the legacy column while authorisation reads the RBAC tables. The two can diverge, and nothing keeps them in step.

---

## 4. Configuration

### UC-A-24 — Read the Configuration screen

| | |
|---|---|
| **Goal** | "Show me the rules this product runs on." |
| **Primary actor** | AC1, or any holder of `config:view` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar **Configuration**, or the dashboard's **Configuration / Numbering, attachments, urgency, approvals** |
| **Route(s)** | `GET /configuration` |
| **Permission gate** | `config:view` (`internal/app/app.go:516`) |
| **Coverage IDs** | adoption-spec **G21** / **D6**; T10 and V4 are the two settings whose behaviour is pinned |
| **Priority** | high |

**Preconditions**

1. The actor holds `config:view`.
2. `app_settings` exists with its nine seeded keys (`internal/store/migrations.go:239-249`).

**Postconditions (success)** — 200 HTML titled `Configuration - Fervid Budget`; nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. The actor opens `/configuration`.
2. The system reads every `app_settings` row into one map and, separately, the recoverable categories with their usage counts (`internal/app/configuration.go:89-104`).
3. The system renders the banner: eyebrow **Administration**, `<h1>Configuration</h1>`, sub **The rules the request module runs on. Everything here is data — no code change needed.**
4. The system renders one `<fieldset>` per `ConfigSection`, in this order, each field's `name` being the `app_settings` key it reads and writes (`internal/app/configuration.go:44-87`):
   - **Request numbering** — **Prefix** (text), **Year segment** (select: *Calendar year* / *Financial year* / *None*), **Number width** (number, hint *How many digits the running number is padded to.*)
   - **Attachments** — **Require a supporting document on every request** (toggle, hint *Turning it on does not block submission — it asks for an exception reason when no document is attached, because legitimate documents are sometimes genuinely unavailable.*), **Maximum file size (MB)** (number)
   - **Urgency** — **Marking a request urgent** (select: *Requires a reason* / *Free to mark, no reason* / *Disabled*), note *Urgent requests follow the same approval rules. Urgency changes who is told and how soon, never who may decide.*
   - **Approvals** — **Let the employee choose a different approver** (toggle, hint *Off means everyone must use the default approver set on their user record.*), note *Self-approval is blocked always. A person can never approve a request they raised, whatever roles they hold.*
   - **Payments** — **Allow direct payments without a request** (toggle), **Payment modes offered** (text, hint *Comma separated, in the order the payment form should offer them.*)
   - **Reminders and ageing** — see UC-A-26
5. Inside the **Approvals** fieldset the system renders two permanently disabled controls: **Block self-approval** (checked, disabled, hint *Always on…*) and **Second approval above a threshold** (unchecked, disabled, hint *Reserved for a future version. The status model already has room for it.*) (`internal/app/templates.go:3096-3105`).
6. The action bar reads **Every change here is written to the audit log.**, then **Discard** (a link to `/dashboard`) and **Save configuration** (`internal/app/templates.go:3111-3116`).
7. **Outside** the settings form the system renders one more `<fieldset>` with `<legend>Recoverable categories</legend>` — see UC-A-27.

**Alternate flows**

- **4.a** A key is absent from `app_settings` → `index $.Config .Key` yields `""`: a text/number input renders empty, a select falls back to its first option (because `select` compares against `""`), and a toggle renders unchecked. This is the live state of the three reminder keys, which are **not** seeded (`internal/store/migrations.go:239-249` lists nine keys, none of them `reminder_*`).

**Exception flows**

- **24.e1** No `config:view` → 403 (UC-A-09).
- **24.e2** The actor holds `config:view` but not `config:edit`: every input is enabled and **Save configuration** renders, and pressing it answers 403 from `POST /configuration`. The form is not gated (`internal/app/templates.go:3073-3117` vs `internal/app/app.go:517`).
- **24.e3** `ListRecoverableCategoriesWithUsage` errors → the whole screen becomes an error page (`internal/app/configuration.go:98-102`).

**Business rules**

- The screen is a rendering of the `configSections` table; a later phase adds a section by appending to it and nothing else. "If a phase has to touch `configuration` or `configurationSave` to add a control, this file got the shape wrong." (`internal/app/configuration.go:11-21`).
- Recoverable categories are rows, not scalars, so they cannot be a `ConfigSection`; they get their own fieldset with its own sub-form (`internal/app/configuration.go:95-97`).
- Both extra Approvals controls are display-only: in-code invariants, not settings.

**Data touched** — reads `app_settings`, `recoverable_categories`, `payment_requests` (the usage count).

**Non-functional / UX notes** — every field carries `class="field span-N"` plus `m-half` when `Span < 12`, which is what makes the 12-column grid collapse to two-up at 390px (`internal/app/templates.go:3080`). Every control has a real `<label for>` except the toggles, which use `label.checkline` wrapping the input.

**Open questions**

- The three **Reminders and ageing** inputs render **blank** on a fresh install even though the effective values are 3 / 1 / 1. The screen tells the operator nothing about what is actually in force (see UC-A-26).
- The audit trail for a settings change uses `action='settings'` and `entity_type='app_setting'` (`internal/store/settings.go:76-78`), and the audit screen's Entity and Action selects offer neither — see UC-A-40.

---

### UC-A-25 — Save the application settings, including Require attachments

| | |
|---|---|
| **Goal** | "Turn on the requirement for a supporting document, and change the request number prefix." |
| **Primary actor** | AC1, or any holder of `config:edit` |
| **Supporting actors** | every requester — UC-B's submit path reads `require_attachments` |
| **Scope / level** | system · user-goal |
| **Trigger** | **Save configuration** on `/configuration` |
| **Route(s)** | `POST /configuration` → 303 `/configuration` |
| **Permission gate** | `config:edit` + CSRF (`internal/app/app.go:517`) |
| **Coverage IDs** | **T10**, D6/G21 |
| **Priority** | critical |

**Preconditions**

1. The actor holds `config:edit`.
2. The POST carries only keys that a registered `ConfigField` declares — anything else is ignored, not rejected (`internal/app/configuration.go:138-154`).

**Postconditions (success)**

1. One `app_settings` row per written key, upserted (`internal/store/settings.go:71-74`).
2. **Every toggle is written whether or not it was posted**, because an unchecked checkbox sends nothing and "absent" has to mean off (`internal/app/configuration.go:141-148`). `on|1|true|yes` become `"1"`; everything else becomes `"0"` (`internal/app/configuration.go:162-169`).
3. Non-toggle fields are written **only when present in the form**, trimmed.
4. **One** `audit_log` row for the whole save: `action='settings'`, `entity_type='app_setting'`, `summary='Updated configuration: {keys, comma-separated, sorted}'`, with `before_json` and `after_json` holding the old and new values of every written key (`internal/store/settings.go:76-79`).
5. The browser is back on `/configuration` showing the new values.
6. With `require_attachments = "1"`, UC-B's submit path asks for an exception reason when no document is attached — it never blocks submission (`internal/store/requests.go:544`, hint text at `internal/app/configuration.go:56`).

**Postconditions (failure)** — nothing at all: the whole batch is one transaction (`internal/store/settings.go:46-82`).

**Main success scenario**

1. The actor ticks **Require a supporting document on every request**.
2. The actor changes **Prefix** from `PR` to `REQ`.
3. The actor presses **Save configuration**.
4. `configurationSave` walks `configSections`, collecting the three toggles unconditionally and the posted text/number/select fields, and hands the map to `SetAppSettings`.
5. `SetAppSettings` sorts the keys, records each previous value, upserts each, writes the single audit row, and commits.
6. The system redirects 303 to `/configuration`.

**Alternate flows**

- **1.a** The actor un-ticks a toggle → nothing is posted for it and it is written as `"0"`, which is exactly why toggles are written unconditionally.
- **2.a** The actor clears a text field → it is present but empty, so `""` is written. For `number_prefix` this means future request numbers have no prefix; for `reminder_*` it means the code falls back to its default (UC-A-26).
- **3.a** The actor presses **Discard** → a plain link to `/dashboard`; nothing is posted and nothing changes.

**Exception flows**

- **25.e1** No `config:edit` → 403 before the handler.
- **25.e2** Missing CSRF → 403.
- **25.e3 (hazard)** A **partial** POST — for example a hand-crafted request carrying only `number_prefix` — silently writes `require_attachments=0`, `allow_approver_choice=0` **and** `allow_direct_payments=0`, because all three toggles are written whether posted or not (`internal/app/configuration.go:141-144`). The real form always posts every section, so normal use is safe; any partial submission is destructive.
- **25.e4** A key the form does not declare is simply dropped, so a hand-rolled POST cannot invent a setting (`internal/app/configuration.go:138-140`).
- **25.e5** `SetAppSettings` with an empty map returns nil and writes nothing — including no audit row (`internal/store/settings.go:47-49`). Not reachable from the form, which always posts three toggles.

**Business rules**

- Only registered keys are writable; the field key **is** the `app_settings` key (`internal/app/configuration.go:41-43`).
- `require_attachments` asks for an exception reason rather than blocking, per T10 and G10.
- Self-approval is blocked unconditionally and is not a setting (`internal/app/configuration.go:68`).
- The batch is all-or-nothing with one audit row (`internal/store/settings.go:44-46`).

**Data touched** — writes `app_settings(key,value)`, `audit_log`.

**Non-functional / UX notes** — no success notice is shown: the redirect lands on the same screen with the new values and no confirmation banner. `PageData.Notice` exists and renders `div.alert.success` (`internal/app/templates.go:49`) but no configuration path sets it.

**Open questions**

- 25.e3 makes any partial POST silently disable three behaviours. A more defensive shape would post a hidden marker per section and write only the sections present.
- There is no validation of any value: `number_width` accepts `abc`, `attachment_max_mb` accepts `-5`. Consumers apply their own fallbacks (`internal/store/requests.go:53` defaults the prefix to `PR`), but nothing tells the operator their input was ignored.

---

### UC-A-26 — Set the reminder and ageing thresholds

| | |
|---|---|
| **Goal** | "Nudge an approver after three days, then daily; call a reservation stale after one day." |
| **Primary actor** | AC1, or any holder of `config:edit` |
| **Supporting actors** | the reminder scheduler (UC-C) |
| **Scope / level** | system · subfunction |
| **Trigger** | The **Reminders and ageing** fieldset on `/configuration`, then **Save configuration** |
| **Route(s)** | `GET /configuration` · `POST /configuration` |
| **Permission gate** | `config:view` / `config:edit` |
| **Coverage IDs** | N4, N5, S8 (the thresholds those rules count in); D6 |
| **Priority** | medium |

**Preconditions**

1. The actor holds `config:edit`.
2. The three keys are `reminder_pending_days`, `reminder_repeat_days`, `reminder_stale_days` (`internal/app/configuration.go:79-86`).

**Postconditions (success)**

1. Three `app_settings` rows hold the submitted strings, trimmed.
2. `ReminderThresholds` returns the parsed values; a blank, non-integer or non-positive value falls back to **3**, **1**, **1** respectively (`internal/store/reminders.go:44-46`, `:64-82`).
3. One `audit_log` row, shared with the rest of the save (UC-A-25).

**Postconditions (failure)** — the whole configuration save rolls back.

**Main success scenario**

1. The actor scrolls to the **Reminders and ageing** fieldset.
2. The system shows three number inputs: **Remind after (days pending)** with hint *How long a request may sit with an approver before the first reminder. Default 3.*; **Then repeat every (days)** with hint *How often the reminder returns while nothing happens. Default 1.*; **Reservation goes stale after (days)** with hint *How long an accountant may hold a reservation with no payment recorded. Default 1.*; and beneath them the note *Reminders are counted in calendar days, not working hours. A request put on hold is waiting on the requester by design and is never reminded about.*
3. The actor types `5`, `2`, `1` and presses **Save configuration**.
4. The values are written as strings, and the scheduler reads them on its next tick.

**Alternate flows**

- **3.a** The actor leaves a field blank → `""` is stored and the default applies. "Garbage must never silently disable reminders — a zero wait would remind on every tick and a negative one would never remind at all." (`internal/store/reminders.go:60-63`).
- **3.b** The actor types `0` or `-1` → stored verbatim, and `read` rejects it (`n <= 0`) so the default applies. The screen then shows the stored `0` while the system behaves as `3`.

**Exception flows**

- **26.e1** No `config:edit` → 403.
- **26.e2** A non-numeric value (`type="number"` normally prevents it) → stored, then rejected by `strconv.Atoi` and defaulted.

**Business rules**

- Thresholds are parameters, not constants: "The old hardcoded 3 and 1 are gone." (`internal/store/reminders.go:26-32`).
- `RequestsPendingReminder` re-applies the defaults defensively even if handed a non-positive threshold (`internal/store/reminders.go:88-93`).
- Reminders are counted in calendar days.

**Data touched** — writes `app_settings`; read later by `internal/store/reminders.go` and `internal/notify`.

**Non-functional / UX notes** — three `span-4` fields become two-up then one-up as the viewport narrows.

**Open questions**

- **The screen and the behaviour can disagree.** Because the keys are not seeded and invalid values are stored rather than rejected, the fieldset can show blank or `0` while the scheduler uses 3 / 1 / 1. The hints name the defaults, which is the only mitigation. Either seeding the three keys in a migration or echoing the effective value would fix it.

---

### UC-A-27 — Maintain the recoverable categories outside the settings form

| | |
|---|---|
| **Goal** | "Add a *Retention deposit* category, and switch off one we no longer use." |
| **Primary actor** | AC1, or any holder of `recoverable_category:edit` |
| **Supporting actors** | UC-B (the request form offers only active categories); UC-C (the recoverables register) |
| **Scope / level** | system · user-goal |
| **Trigger** | The **Recoverable categories** fieldset at the foot of `/configuration` |
| **Route(s)** | `POST /configuration/recoverable-categories` → 303 `/configuration` |
| **Permission gate** | `recoverable_category:edit` + CSRF (`internal/app/app.go:518-520`) — **not** `config:edit` |
| **Coverage IDs** | **V4**, V5, D6 |
| **Priority** | high |

**Preconditions**

1. The actor holds `recoverable_category:edit`; the screen itself needs only `config:view`.
2. The submitted name, trimmed, is non-empty and contains at least one letter or digit (`internal/store/recoverables.go:186-200`).
3. `requires` is one of `none`, `project`, `counterparty`, `both`, or empty (`internal/app/configuration.go:114-127`).
4. On create, the derived code and the name are both unique case-insensitively (`internal/store/recoverables.go:49-50`).

**Postconditions (success — create)**

1. One `recoverable_categories` row with a code derived from the name (lower-cased, non-alphanumerics collapsed to `_`, trimmed), the name, the two `requires_*` flags, `active`, and `sort_order` (`internal/store/recoverables.go:96-113`, `:201-209`).
2. One `audit_log` row: `action='create'`, `entity_type='recoverable_category'`, `summary='Saved recoverable category {name}'` (`internal/store/recoverables.go:219-221`).
3. The new category is immediately usable on the request form, because `recoverableRules` reads the active rows rather than a built-in map (`internal/store/recoverables.go:115-129`).

**Postconditions (success — toggle)**

1. `recoverable_categories.active` flips; `code` is deliberately **not** in the UPDATE's SET list, because it is the identity existing requests resolve their rules by (`internal/store/recoverables.go:210-217`).
2. One `audit_log` row with `action='update'`.
3. Deactivating stops **new** requests naming the category; existing requests keep resolving through the unchanged code.

**Postconditions (failure)** — nothing: one transaction (`internal/store/recoverables.go:189-227`).

**Main success scenario (add)**

1. The actor scrolls to the fieldset `<legend>Recoverable categories</legend>`.
2. The system renders `table.t-cards` with headers **Category**, **Requires**, **Active**, **In use** — where **Requires** is one derived phrase (*Nothing extra* / *Related project* / *Counterparty company* / *Related project and counterparty company*), never two raw booleans (`internal/store/recoverables.go:80-94`, `internal/app/templates.go:3125-3134`).
3. Below the table, for a holder of `recoverable_category:edit`, the system renders the add form: **New category** (`id="nc-name"`, placeholder *e.g. Retention deposit*, required), **Must also capture** (select: *Nothing extra* / *Related project* / *Counterparty company* / *Related project and counterparty company*), and the button **Add category**. A hidden `active=on` makes every added category active (`internal/app/templates.go:3149-3164`).
4. The actor types a name, chooses a requirement, and presses **Add category**.
5. `recoverableCategorySave` maps the single `requires` value to the two booleans — the only place that mapping exists — and calls `UpsertRecoverableCategory` with `id=0`.
6. The system redirects 303 to `/configuration`; the new row appears with **In use** `0`.
7. Under the table the trailing hint reads: *All recoverables always capture the counterparty, the reason, an expected return date and the refund terms. These rules only add what the category needs on top. An employee advance fills the counterparty in from the requester automatically — that is a request-type rule, not a category setting.*

**Main success scenario (toggle Active)**

1. Each row's **Active** cell holds its **own** `<form id="rc-{id}">` carrying hidden `id`, `name`, `requires` (round-tripped through `requiresKey`) and `sort_order`, and a checkbox with `onchange="this.form.submit()"` plus an `<span class="sr-only">Active</span>` label (`internal/app/templates.go:3135-3145`).
2. The actor clicks the checkbox; the form self-submits.
3. `UpsertRecoverableCategory` runs with the existing `id`, rewriting name, flags, active and sort order.

**Alternate flows**

- **1.a** The actor lacks `recoverable_category:edit` → the **Active** cell renders a static **On** / **Off** pill and the add form is absent entirely (`internal/app/templates.go:3145`, `:3149`).
- **2.a** With JavaScript off the row form still works: a `<noscript>` **Save** button is rendered inside it (`internal/app/templates.go:3143`).

**Exception flows**

- **27.e1** `requires` is an unrecognised value → **400**, **That category requirement is not recognised.** (`internal/app/configuration.go:124-127`).
- **27.e2** Blank name → **400**, `validation failed: recoverable category name is required`.
- **27.e3** A name with no letters or digits (e.g. `---`) → **400**, `validation failed: recoverable category name must contain a letter or digit` (`internal/store/recoverables.go:197-200`).
- **27.e4** A duplicate name, or a name whose derived code collides with an existing code → **400**, **A record with this name already exists.** Note two different names can derive the same code — `Retention deposit` and `Retention-Deposit` both give `retention_deposit` — so the code index refuses the second even though the names differ.
- **27.e5** No `recoverable_category:edit` → 403, even for an actor who holds `config:edit` and can save every other part of the screen.

**Business rules**

- This fieldset sits **outside** the settings form on purpose: a `<form>` nested inside another `<form>` is invalid HTML that the parser silently drops, and the row toggles would post nothing at all. A bare `<fieldset>` outside a form is valid (`internal/app/templates.go:3119-3127`, PROGRESS.md trap).
- `code` is the stable identity and is never rewritten, so renaming a category cannot orphan its requests (`internal/store/recoverables.go:96-99`, `:211-212`).
- Only active categories are offered to new requests, which is the whole point of the Active switch (`internal/store/recoverables.go:115-118`).
- The screen is gated on `config:view` while the mutation keeps its own Phase-1 verb, which is what the store audits against (`internal/app/app.go:518-519`).

**Data touched** — writes `recoverable_categories(code,name,requires_project,requires_counterparty,active,sort_order)`, `audit_log`; reads `payment_requests` for **In use**.

**Non-functional / UX notes** — the toggle's only accessible name is the `.sr-only` span, so a screen reader hears "Active" per row with no row context; the row's **Category** cell is the `t-lead`, which is what a card-restacked view leads with.

**Open questions**

- **There is no delete.** `recoverable_category:delete` exists in the vocabulary (`internal/store/permissions.go:164`) and is mapped to the **Cancel** column of the **Recoverables** matrix row (`internal/app/permmap.go:114`), but no route and no store method delete a category — `internal/store/recoverables.go` has no `DeleteRecoverableCategory`. The grant is grantable and unenforceable.
- `recoverable_category:create` is likewise never checked: adding a category needs `edit`. A role granted **Create** but not **Edit** on the Recoverables row can do nothing.
- **Sort order is not editable from the screen.** The add form posts no `sort_order` (so every new category gets 0) and the row form round-trips the stored value. Ordering is `sort_order, name`, so new categories all sort together at the top by name.

---

## 5. Masters — projects, heads, months, budgets

### UC-A-28 — Maintain the project master

| | |
|---|---|
| **Goal** | "Add a project bucket, rename one, retire one." |
| **Primary actor** | AC1, or any holder of `project:view` + `project:edit` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar **Projects** |
| **Route(s)** | `GET /projects`; `POST /projects` → 303 `/projects` |
| **Permission gate** | `project:view` for the screen, `project:edit` for the save (`internal/app/app.go:425-426`) |
| **Coverage IDs** | T12 (the active flag is what hides a project from new work), C2 |
| **Priority** | high |

**Preconditions**

1. The actor holds `project:view` to read and `project:edit` to save.
2. The submitted name, trimmed, is non-empty (`internal/store/store.go:235-238`).
3. The name is unique case-insensitively (`idx_projects_name_nocase`, `internal/store/schema.go:120`).

**Postconditions (success)**

1. On create: one `projects` row with the trimmed name, `active` from the checkbox, and `sort_order` (`internal/store/store.go:239-245`).
2. On update: `projects.name`, `active`, `sort_order` are rewritten for that id.
3. One `audit_log` row: `action='create'` or `'update'`, `entity_type='project'`, `entity_id={id}`, `summary='Created project {name}'` / `'Updated project {name}'` (`internal/app/app.go:1078-1085`).
4. The browser is back on `/projects`.

**Postconditions (failure)** — nothing written; the audit row is written **only** when the upsert succeeded (`internal/app/app.go:1077-1085`).

**Main success scenario**

1. The actor opens `/projects`; the system lists **every** project, active and inactive, ordered `sort_order, name` (`internal/store/store.go:250-256`, called with `activeOnly=false` at `internal/app/app.go:1066`).
2. The system renders the banner: eyebrow **Setup**, `<h1>Projects</h1>`, sub **Manage project buckets used by the grid and payment entry.**
3. The system renders the add toolbar — **Project name** (required), **Order** (number, default 0), the checkline **Active** (checked), and the button **Add Project** (`internal/app/templates.go:1167`).
4. The system renders `table.t-cards` with headers **Name**, **Status**, **Order**, **Update**. Each row is an inline editor: the name input, an **Active** checkline showing `boolText` (**Active** / **Inactive**), the order input, and a **Save** button — all bound by `form="project-{id}"` to a tiny hidden `<form>` in the last cell that carries the CSRF token and the row's `id` (`internal/app/templates.go:1168`).
5. The actor edits a name and presses that row's **Save**.
6. `projectSave` upserts, audits, and redirects 303 to `/projects`.
7. With no projects the table shows **No projects yet. Add the first one above.**

**Alternate flows**

- **3.a** The actor clears **Active** before pressing **Add Project** → the project is created inactive, so `ListProjects(activeOnly=true)` never offers it and `ListHeads(activeOnly=true)` excludes all its heads (`internal/store/store.go:296-302`).
- **5.a** The actor clears a row's **Active** checkbox and presses **Save** → the project is retired. Its heads keep their rows in the grid whenever a budget or a payment exists for them (`internal/store/store.go:1387-1400`), and the grid marks them **Retired** (`internal/app/templates.go:146`).
- **4.a** At ≤860px the table restacks; the inline inputs keep working because the `form=` attribute does not depend on layout (`internal/app/app_integration_test.go:727`).

**Exception flows**

- **28.e1** Blank name → **400**, `validation failed: project name is required`.
- **28.e2** A duplicate name (case-insensitively) → **400**, **A record with this name already exists.** (`classify` on the unique index).
- **28.e3** `sort_order` is not a number → `strconv.Atoi` error is **discarded** and 0 is used (`internal/app/app.go:1076`). A typo silently reorders the project to the top.
- **28.e4** The actor holds `project:view` but not `project:edit` → the add toolbar and every row **Save** still render, and pressing either answers 403. Neither is gated (`internal/app/templates.go:1167-1168`).
- **28.e5** Missing CSRF → 403.
- **28.e6** `id` names no project → the UPDATE affects zero rows and the handler still writes an `update` audit row and redirects 303. **A save against a non-existent project reports success.** `UpsertProject` does not check `RowsAffected` (`internal/store/store.go:246-247`).

**Business rules**

- Project names are unique on `lower(name)` (`internal/store/schema.go:120`).
- There is **no delete**: `project` has actions `view`, `create`, `edit` only (`internal/store/permissions.go:158`). Retirement is via **Active**.
- `project:create` is never enforced — creating a project needs `project:edit` (`internal/app/app.go:426`).

**Data touched** — writes `projects(name,active,sort_order)`, `audit_log`.

**Non-functional / UX notes** — every inline input carries an aria-label naming its row (`aria-label="Name for {name}"`, `"Sort order for {name}"`), which is what makes the restacked card readable (`internal/app/app_integration_test.go:727`). The summary uses `strings.Title`, deprecated since Go 1.18, to build "Created"/"Updated" (`internal/app/app.go:1084`).

**Open questions**

- 28.e6 (a silent no-op reported as success) applies equally to heads. `UpsertProject`/`UpsertHead` return the submitted id without verifying the row exists.
- Retiring a project does not retire its heads. `ListHeads(activeOnly=true)` filters on both flags, so behaviour is right, but `/heads` shows those heads as **Active** while they are effectively retired (see UC-A-29).

---

### UC-A-29 — Maintain the head master

| | |
|---|---|
| **Goal** | "Add a spend head under a project, give it a due day, retire an old one." |
| **Primary actor** | AC1, or any holder of `head:view` + `head:edit` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar **Heads** |
| **Route(s)** | `GET /heads`; `POST /heads` → 303 `/heads` |
| **Permission gate** | `head:view` / `head:edit` (`internal/app/app.go:427-428`) |
| **Coverage IDs** | **T12**, C2 |
| **Priority** | high |

**Preconditions**

1. The actor holds `head:view` to read and `head:edit` to save.
2. `project_id` is non-zero and the name, trimmed, is non-empty (`internal/store/store.go:277-279`).
3. `due_day` is blank, or an integer from 1 to 31 (`internal/store/store.go:1656-1662`).
4. The name is unique within the project, case-insensitively (`idx_heads_project_name_nocase`, `internal/store/schema.go:121`).

**Postconditions (success)**

1. On create: one `heads` row with `project_id`, name, `due_day`, `active`, `sort_order` (`internal/store/store.go:283-290`).
2. On update: all five columns are rewritten.
3. One `audit_log` row: `create`/`update`, `entity_type='head'`, `summary='Created head {name}'` / `'Updated head {name}'` (`internal/app/app.go:1113-1118`).

**Postconditions (failure)** — nothing written, no audit row.

**Main success scenario**

1. The actor opens `/heads`; the system lists **every** head with its project name, ordered `p.sort_order, p.name, h.sort_order, h.name` (`internal/store/store.go:296-302` with `activeOnly=false`), and loads **only active projects** for the selects (`internal/app/app.go:1093-1104`).
2. The system renders the banner: eyebrow **Setup**, `<h1>Heads</h1>`, sub **Maintain spend heads, due days, and sort order.**
3. The system renders the add toolbar — **Project** (select of active projects, required), **Head name** (required), **Due day** (number, min 1, max 31, placeholder `5`), **Order** (number, 0), the checkline **Active** (checked), and **Add Head** (`internal/app/templates.go:1175`).
4. The system renders `table.t-cards` with headers **Project**, **Head**, **Due**, **Status**, **Order**, **Update**; each row is an inline editor bound to `form="head-{id}"` (`internal/app/templates.go:1176`).
5. The actor changes a due day and presses that row's **Save**.
6. `headSave` upserts, audits, redirects 303 to `/heads`.
7. With no heads the table shows **No heads yet. Add the first one above.**

**Alternate flows**

- **5.a** The actor clears a row's **Active** and saves → the head is retired: hidden from `ListHeads(activeOnly=true)` (the payment form's select and the copy-month source), still shown in the grid and on the budgets screen whenever a budget or payment exists, and marked **Retired** there (UC-A-34).
- **3.a** **Due day** left blank → stored as `''`; the grid shows the due pill as **No due day** (`internal/app/app.go:1780-1785`).

**Exception flows**

- **29.e1** `project_id=0` or a blank name → **400**, `validation failed: project and head name are required` (pinned at `internal/app/http_safety_test.go:39-44`).
- **29.e2** `due_day` outside 1–31 → **400**, `validation failed: due day must be a day from 1 to 31`. `min`/`max` on the input normally prevents it.
- **29.e3** A duplicate head name within the same project → **400**, **A record with this name already exists.** The same name under a *different* project is allowed.
- **29.e4** `project_id` names no project → the foreign key on `heads.project_id` rejects the INSERT and the error is returned unclassified, so `respondStoreError` answers **500** (`internal/store/schema.go:30`, `internal/app/http_errors.go:183-197`).
- **29.e5 (defect)** The head's project is **inactive**. The row's `<select name="project_id">` is built from *active* projects only, so no `<option>` matches and the browser pre-selects the **first** option. Pressing that row's **Save** silently **reassigns the head to a different project**. Verified from `internal/app/app.go:1099` (`ListProjects(ctx, true)`) against `internal/app/templates.go:1176` (`{{if eq $head.ProjectID .ID}}selected{{end}}`).
- **29.e6** `head:view` without `head:edit` → toolbar and row **Save** buttons render; both answer 403.

**Business rules**

- A head is unique per project, case-insensitively (`internal/store/schema.go:121`).
- `ListHeads(activeOnly=true)` requires **both** the head and its project to be active (`internal/store/store.go:299-301`) — which is what implements T12 for a retired project.
- There is no delete: `head` has `view`, `create`, `edit` only (`internal/store/permissions.go:159`); `head:create` is never enforced.
- A payment may only name an active head: `validatePayment` returns `ErrInactiveHead` otherwise (`internal/store/store.go:1627-1634`).

**Data touched** — writes `heads(project_id,name,due_day,active,sort_order)`, `audit_log`.

**Non-functional / UX notes** — the project select carries `aria-label="Project for {name}"`, the name input `aria-label="Head name for {name}"`, the due day `aria-label="Due day for {name}"`, the order `aria-label="Sort order for {name}"` (`internal/app/templates.go:1176`).

**Open questions**

- 29.e5 is a **silent data-corruption path**: it needs no hand-crafted request, only an inactive project and someone pressing Save on one of its heads. The fix is either to load all projects for the row selects or to render the current project as a disabled option.
- 29.e4 answering 500 rather than 400 is a classification gap: a foreign-key violation is user error here, and `classify` only recognises `UNIQUE` (`internal/store/store.go:1712-1720`).

---

### UC-A-30 — Open a monthly plan, optionally copying the previous month

| | |
|---|---|
| **Goal** | "Open next month and carry this month's budgets forward." |
| **Primary actor** | AC1, or any holder of `month:view` + `month:create` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar **Monthly plans** |
| **Route(s)** | `GET /months`; `POST /months` → 303 `/budgets?month={target}` |
| **Permission gate** | `month:view` / `month:create` (`internal/app/app.go:382-383`) |
| **Coverage IDs** | C1 (nearest), C2 |
| **Priority** | high |

**Preconditions**

1. The actor holds `month:view` to read and `month:create` to save.
2. `target_month` parses as `2006-01` (`internal/store/store.go:419-421`).
3. `target_month` is not locked (`internal/store/store.go:425-427`).
4. When copying, `source_month` parses **and** has at least one budget with `amount > 0` (`internal/store/store.go:434-442`).

**Postconditions (success)**

1. One `budget_months` row for the target with `status='open'`, `source_month` (or NULL), `created_by` — upserted, and on conflict `source_month` is only filled if it was NULL (`internal/store/store.go:448-452`).
2. When copying: one `budgets` row per **active** head of an **active** project whose source-month amount is `> 0`, with `ON CONFLICT(head_id,month) DO NOTHING` so an existing target budget is never overwritten (`internal/store/store.go:455-467`).
3. One `audit_log` row: `action='create'`, `entity_type='budget_month'`, summary `Created monthly plan {target}` — extended to `… from {source} with {N} copied budgets` when copying (`internal/store/store.go:472-476`).
4. The browser lands on `/budgets?month={target}`.

**Postconditions (failure)** — no `budget_months` row and no copied budgets; both are inside one transaction (`internal/store/store.go:429-471`). The audit row is written **after** the commit, so a failed audit write leaves the plan created (`internal/store/store.go:469-476`).

**Main success scenario**

1. The actor opens `/months`.
2. The system lists every month it knows about — the union of `budget_months`, `budgets`, `substr(paid_on,1,7)` and `month_locks` — newest first, and computes each one's totals by running the whole grid query per month (`internal/store/store.go:479-533`).
3. The system renders the banner: eyebrow **Month control**, `<h1>Monthly Plans</h1>`, sub **Create each month, review prior months, and open locked history whenever needed.**, and the action **Reports** linking to `/reports/monthly`.
4. The system renders the create toolbar: **New month** (`type="month"`, defaulted to the current month, or one month after the newest plan — `internal/app/app.go:1572-1578`), **Plan type** (select: *Copy from month* / *Start blank*), **Source month** (`type="month"`, defaulted to the month before the target), and **Create Month** (`internal/app/templates.go:1151`).
5. The system renders `table.t-cards` with headers **Month**, **Status**, **Source**, **Budget**, **Actual**, **Remaining**, **Used**, **Created**, **Actions**; the status pill reads **Open** or **Locked**, the source cell reads the month or **Manual**, and **Created** reads a timestamp or **Imported history** (`internal/app/templates.go:1152`).
6. The actor accepts the defaults and presses **Create Month**.
7. `monthCreate` reads `source_month` **only when `source_mode == "copy"`** (`internal/app/app.go:971-976`), calls `CreateMonthPlan`, and redirects to the new month's budgets screen.

**Alternate flows**

- **6.a** The actor selects **Start blank** → `source` is `""`, no budgets are copied, and the summary is just `Created monthly plan {target}`.
- **6.b** The target month already has a `budget_months` row → the upsert only refreshes `updated_at`, and a copy still runs, adding budgets for heads that have none (`DO NOTHING` protects the ones that do).
- **5.a** Each row's **Actions** cell offers **Grid**, **Budget**, **Payments** and **Report**. **Grid** points at `/?month={month}` — which is now the **dashboard**, not the grid (see 30.e5).

**Exception flows**

- **30.e1** `target_month` does not parse → the months screen re-renders at **400** with `validation failed: target month is required`, preserving the typed target and source (`internal/app/app.go:977-985`).
- **30.e2** `source_month` does not parse → **400**, `validation failed: source month is invalid`.
- **30.e3** The source month has no budget above zero → **400**, `validation failed: source month has no budgets to copy`.
- **30.e4** The target month is **locked** → `ErrLockedMonth`, and because `monthCreate` re-renders rather than calling `respondStoreError`, the status is **400** with **This month is locked. Unlock it with a reason before changing budgets or payments.** (`internal/app/app.go:983`, `internal/app/app.go:1628-1632`). The documented mapping for `ErrLockedMonth` is **409** (`internal/app/http_errors.go:189-190`); this path diverges.
- **30.e5** Following the row's **Grid** action lands on the dashboard with an ignored `?month=` parameter, because `GET /{$}` is the dashboard (F1) and `dashboard` never reads `month`.
- **30.e6** No `month:create` → 403; the toolbar is not gated, so a `month:view`-only actor sees **Create Month** and gets 403.

**Business rules**

- Only active heads of active projects, and only amounts above zero, are copied (`internal/store/store.go:456-462`) — T12 applied to the copy.
- An existing target budget is never overwritten by a copy.
- `ListMonthPlans` derives a month's existence from four tables, so a month with payments but no plan still appears, with **Created** showing **Imported history**.

**Data touched** — writes `budget_months`, `budgets`, `audit_log`; reads `budgets`, `payments`, `month_locks`, `users`, `heads`, `projects`.

**Non-functional / UX notes** — `ListMonthPlans` runs the full `Grid` query **once per month** (`internal/store/store.go:517-524`), so the screen's cost grows with history. The **Source month** field stays enabled when **Start blank** is chosen — the handler ignores it, but the screen does not say so.

**Open questions**

- 30.e4 (400 instead of 409 for a locked month) and UC-A-33's identical divergence on `/budgets` mean the "locked month" status code is **inconsistent across the product**: 409 through `respondStoreError`, 400 through the two screens that re-render.
- The **Grid** action on every row is a link to the dashboard. Same root cause as UC-A-31's redirect.

---

### UC-A-31 — Lock and unlock a month

| | |
|---|---|
| **Goal** | "Close March so nobody can change its budgets or payments — and reopen it if I must, with a reason on the record." |
| **Primary actor** | AC1, or any holder of `month:lock` |
| **Supporting actors** | everybody who enters payments or budgets in that month |
| **Scope / level** | system · user-goal |
| **Trigger** | The **Month Close** panel on the variance grid |
| **Route(s)** | `POST /months/{month}/lock` and `POST /months/{month}/unlock` → 303 `/?month={month}` |
| **Permission gate** | `month:lock` for **both** directions + CSRF (`internal/app/app.go:423-424`) |
| **Coverage IDs** | C2; T12 is adjacent (retired heads on a locked month) |
| **Priority** | high |

**Preconditions**

1. The actor holds `month:lock`.
2. `{month}` parses as `2006-01` (`internal/store/store.go:1514`).
3. The submitted **reason** is non-empty after trimming — for lock **and** for unlock.

**Postconditions (success — lock)**

1. A `budget_months` row exists for the month (created if absent, `internal/store/store.go:1517-1519`).
2. One `month_locks` row with `locked_by`, `locked_at`, `reason` — upserted, so re-locking refreshes all three (`internal/store/store.go:1520-1521`).
3. `budget_months.status = 'locked'`.
4. One `audit_log` row: `action='lock'`, `entity_type='month_lock'`, `summary='Locked {month}: {reason}'` (`internal/store/store.go:1528`).
5. `IsLocked(month)` is true, so `SetBudgets`, `CreateMonthPlan` and `validatePayment` all refuse that month (`internal/store/store.go:341-343`, `:425-427`, `:1624-1626`).

**Postconditions (success — unlock)**

1. The `month_locks` row is deleted; `budget_months.status = 'open'`.
2. One `audit_log` row: `action='unlock'`, `summary='Unlocked {month}: {reason}'` (`internal/store/store.go:1546`).

**Postconditions (failure)** — no lock row, no status change, no audit row. Note lock and unlock are **not** transactional: each performs two or three separate `ExecContext` calls plus a post-hoc audit write (`internal/store/store.go:1513-1547`).

**Main success scenario**

1. The actor opens `/grid?month=2026-03`.
2. Because they hold `month:lock`, the system renders the **Month Close** panel beside **Recent Payments for 2026-03**, reading **{N} heads are unpaid, {M} have unbudgeted spend, and {K} are over budget. Review before closing.** (`internal/app/templates.go:182`).
3. The actor types a **Lock reason** (required) and presses **Lock Month**.
4. The browser confirms: **Lock 2026-03? Budgets and payments will become read-only.**
5. `lockMonth` writes the lock, flips the plan status, audits, and redirects 303 to `/?month=2026-03`.
6. The actor lands on the **dashboard** — see 31.e3.

**Alternate flows**

- **2.a** The month is already locked → the panel instead reads **Unlocking reopens this month for budget and payment changes.** with an **Unlock reason** field and an **Unlock Month** button, confirming **Unlock 2026-03 and allow changes again?** (`internal/app/templates.go:182`).
- **2.b** While locked, the grid also shows the banner **Month {month} is locked by {actor}. Reason: {reason}** and every row's action cell reads **Locked** instead of **Add** (`internal/app/templates.go:145-146`).
- **1.a** The actor does not hold `month:lock` → the whole **Month Close** column is absent; the panel is the one place in the product that is properly gated in the template.

**Exception flows**

- **31.e1** A blank reason → **400**, `validation failed: month and reason are required` (`internal/store/store.go:1514-1516`, `:1532-1534`). `required` on the input normally prevents it.
- **31.e2** `{month}` does not parse (e.g. `POST /months/2026-13/lock`) → **400**, same message.
- **31.e3 (defect)** Both handlers redirect to `/?month={month}`, which was the variance grid before `/` became the dashboard. **The operator lands on the dashboard, which shows no month, no lock banner and no confirmation that anything happened** (`internal/app/app.go:1047-1063`; recorded in PROGRESS.md → Known gaps). `/grid?month={month}` would be correct.
- **31.e4** Unlocking a month that was never locked **succeeds**: the DELETE affects zero rows, `budget_months.status` is set to `'open'`, and an `unlock` audit row is written for a no-op (`internal/store/store.go:1531-1546`).
- **31.e5** No `month:lock` → 403.
- **31.e6** Missing CSRF → 403.

**Business rules**

- One grant, `month:lock`, governs both directions: "the person who can stop a payment is the person who must be able to start it again" is the same reasoning applied to `payment:hold` (`internal/app/app.go:472-475`).
- A lock reason is mandatory in both directions — it is the audit evidence.
- Locking does **not** validate that the month is ready; the panel's counts are advisory only.
- `IsLocked` swallows its query error and returns false on failure (`internal/store/store.go:1549-1553`) — a database error therefore reads as "not locked".

**Data touched** — writes `month_locks`, `budget_months(status,updated_at)`, `audit_log`; reads `budget_months`, `payments`, `budgets`, `users`.

**Non-functional / UX notes** — both forms rely on `onsubmit="return confirm(...)"`, so with JavaScript off a month locks on one click with no confirmation. The panel is the second column of `section.split`, which stacks below the payments table at 390px — meaning the lock control is at the very bottom of a long page on a phone.

**Open questions**

- 31.e3 is the single most user-visible consequence of `/` becoming the dashboard. Four other places have the same defect: the grid's own filter form posts to `/` (UC-A-42), the budgets screen's **View grid** and the months screen's **Grid** action both link to `/?month=`, and the historical payment edit screen's **Back to grid** links to `/` (`internal/app/templates.go:136`, `:1159`, `:1152`, `:192`).
- 31.e4 pollutes the audit log with unlock events for months that were never locked.

---

### UC-A-32 — Enter budgets for a month

| | |
|---|---|
| **Goal** | "Set the planned spend for each head this month." |
| **Primary actor** | AC1, or any holder of `budget:view` + `budget:edit` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar **Budgets**, or the redirect after creating a monthly plan |
| **Route(s)** | `GET /budgets?month=YYYY-MM`; `POST /budgets` → 303 `/budgets?month={month}` |
| **Permission gate** | `budget:view` / `budget:edit` (`internal/app/app.go:421-422`) |
| **Coverage IDs** | T12, C3, C2 |
| **Priority** | critical |

**Preconditions**

1. The actor holds `budget:view` to read and `budget:edit` to save.
2. `month` parses as `2006-01`; an invalid value silently becomes the current month (`internal/app/app.go:990`, `:1536-1548`).
3. The month is not locked (`internal/store/store.go:341-343`).
4. Every submitted `budget_{headID}` value parses to a **positive** amount (`internal/app/app.go:1024-1030`, `internal/money/money.go:15-34`).

**Postconditions (success)**

1. A `budget_months` row exists for the month, upserted with `status='open'` (`internal/store/store.go:353-355`).
2. One `budgets` row per submitted head, upserted on `(head_id, month)` (`internal/store/store.go:365-368`).
3. One `audit_log` row **per head**: `action='create'` or `'update'`, `entity_type='budget'`, `summary='Saved budget ₹N'`, with before/after JSON (`internal/store/store.go:374-382`).
4. Everything is one transaction, so a malformed row cannot leave a partially saved plan (`internal/store/store.go:325-326`).
5. The grid, the reports and the monthly-plan totals all reflect the new budgets immediately, because they are all computed from `Grid` (`internal/store/store.go:1380-1478`).

**Postconditions (failure)** — no budget row and no audit row changes; the screen re-renders at 400 with the submitted values preserved in `BudgetInputs` (`internal/app/app.go:1034-1043`).

**Main success scenario**

1. The actor opens `/budgets?month=2026-04`.
2. The system loads the grid for that month and every head (`internal/app/app.go:989-1001`).
3. The system renders the banner: eyebrow **Monthly plan**, `<h1>Budgets</h1>`, sub **Edit planned spend per head for the selected month.**, plus a **Locked** pill when the month is locked.
4. The system renders the month toolbar — **Month** (`type="month"`), **Open**, **Month history** (→ `/months`), **View grid** (→ `/?month=…`, see UC-A-31 open questions) (`internal/app/templates.go:1159`).
5. The system renders the editor `table.t-cards` with headers **Project**, **Head**, **Status**, **Budget**, **Actual**, **Used**. Each row's status is a **Active** or **Retired** pill; the budget cell is `<input name="budget_{headID}" aria-label="Budget for {project} / {head}">`, pre-filled with `money .Budget`; a retired head's input is `disabled` and carries the hint **Retired: kept for history, not editable.** (`internal/app/templates.go:1160`).
6. The actor types amounts and presses **Save Budgets**.
7. `budgetSave` validates the month, walks every `budget_` form key, parses each amount, and calls `SetBudgets` with the whole batch.
8. The system redirects 303 to `/budgets?month={month}`.

**Alternate flows**

- **6.a** Amounts may be typed with `₹`, with Indian digit grouping, or plainly: `ParsePaise` strips `₹` and commas and rounds to the nearest paise (`internal/money/money.go:15-33`).
- **5.a** No active heads for the month → the table shows **No active heads for this month. Add a head first.** with `/heads` linked (`internal/app/templates.go:1160`, `internal/app/phase6_screens_test.go:86-102`).
- **5.b** The month is locked → every input is `disabled`, the **Save Budgets** button is `disabled`, and the banner **This month is locked. Unlock it before changing budgets.** appears (UC-A-33).

**Exception flows**

- **32.e1** `month` is missing or unparseable on the POST → **400**, **A valid month is required.** (`internal/app/app.go:1005-1009`).
- **32.e2 (defect)** **A budget of zero cannot be saved, and a fresh month cannot be saved at all.** An unbudgeted head renders `value="₹0.00"` (`money .Budget` of 0 → `₹0.00`, `internal/money/money.go:36-45`), and `ParsePaise("₹0.00")` returns *amount must be positive* because it rejects `paise <= 0` (`internal/money/money.go:30-32`; pinned by the test case `{name: "zero", input: "0", wantErr: true}` at `internal/money/money_test.go:22`). `budgetSave` turns any parse failure into `validation failed: invalid budget amount for one or more heads` and rejects **the entire batch** (`internal/app/app.go:1024-1030`). So on a month where two or more active heads have no budget, filling in one of them and pressing **Save Budgets** answers **400** — the operator must enter a positive amount for *every* enabled row — and an existing budget can never be reduced to zero through the screen.
- **32.e3** A non-numeric amount → the same 400, with the offending text preserved in the input (`internal/app/app_integration_test.go:368-394`).
- **32.e4** No `budget_` keys at all in the POST → `SetBudgets` receives an empty slice and returns `validation failed: valid month and at least one budget are required` → **400** (`internal/store/store.go:328-330`).
- **32.e5** A duplicate `budget_{id}` for one head → `validation failed: duplicate budget head` (`internal/store/store.go:336-338`). Not reachable from the screen.
- **32.e6** The month is locked → UC-A-33.
- **32.e7** `budget:view` without `budget:edit` → the editor and **Save Budgets** still render and answer 403.

**Business rules**

- Validation happens before any write, so a malformed row cannot partially save (`internal/store/store.go:325-326`; `TestBudgetBatchValidationDoesNotPartiallySave`).
- A retired head's input is `disabled`, and a disabled input is not submitted, so retirement genuinely protects the historical figure (T12).
- Money is `int64` paise end to end (`internal/store/store.go:333`, C3).
- Every budget write is audited individually with before/after JSON.

**Data touched** — writes `budget_months`, `budgets(head_id,month,amount,updated_at)`, `audit_log` (one row per head); reads `heads`, `projects`, `payments`, `month_locks`.

**Non-functional / UX notes** — the amount inputs are plain text, not `type="number"`, so the `₹` and grouping survive a round trip. Each carries `aria-label="Budget for {project} / {head}"`, which is what makes the restacked card usable at 390px.

**Open questions**

- 32.e2 is a **significant usability defect** and is untested: the existing batch test posts only positive amounts (`internal/app/app_integration_test.go:376-380`). Either the form should render an empty input for a zero budget, or `budgetSave` should treat an empty/zero value as "no budget" rather than an error.
- `SetBudgets` upserts a `budget_months` row for any month it is given, so saving budgets for a month that was never "opened" silently opens it — which is why the `open_months` badge can grow without anybody visiting `/months`.

---

### UC-A-33 — A locked month refuses a budget change

| | |
|---|---|
| **Goal** | "Once a month is closed, its numbers must not move." |
| **Primary actor** | AC1, or any holder of `budget:edit` |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | `POST /budgets` for a month that has a `month_locks` row |
| **Route(s)** | `POST /budgets` |
| **Permission gate** | `budget:edit` + CSRF — held; the refusal comes from the store |
| **Coverage IDs** | T12 (adjacent), C2 |
| **Priority** | high |

**Preconditions**

1. A `month_locks` row exists for the month (UC-A-31).
2. The actor holds `budget:edit` and a valid CSRF token.
3. The POST carries at least one valid `budget_{headID}` value — otherwise the parse error fires first.

**Postconditions (success — i.e. the refusal)**

1. **400** — the budgets screen re-rendered with **This month is locked. Unlock it with a reason before changing budgets or payments.** as an `div.alert.error` above the table (`internal/app/app.go:1034-1043`, `:1628-1632`).
2. No `budgets` row changes, no `budget_months` upsert, no audit row: `SetBudgets` checks the lock **before** opening its transaction (`internal/store/store.go:341-343`).
3. The submitted values are preserved in the re-rendered inputs via `BudgetInputs`.

**Postconditions (failure)** — n/a.

**Main success scenario**

1. The actor opens `/budgets?month=2026-03`, a locked month.
2. The system renders every input `disabled`, the **Save Budgets** button `disabled`, and the banner **This month is locked. Unlock it before changing budgets.** (`internal/app/templates.go:1159-1160`).
3. The actor (or a replayed/hand-crafted POST) submits anyway.
4. `SetBudgets` validates the inputs, calls `IsLocked`, and returns `ErrLockedMonth`.
5. The system re-renders the screen at **400** with the locked message.

**Alternate flows**

- **1.a** The actor unlocks the month first (UC-A-31) and the same POST then succeeds.
- **3.a** The same lock blocks `POST /payments` and `POST /payments/{id}/edit` through `validatePayment`, and `POST /months` through `CreateMonthPlan` (`internal/store/store.go:1624-1626`, `:425-427`).

**Exception flows**

- **33.e1 (divergence)** The status is **400**, not the **409** that `respondStoreError`/`storeErrorStatus` define for `ErrLockedMonth` (`internal/app/http_errors.go:189-190`, `:201-204`). `budgetSave` hard-codes `http.StatusBadRequest` when it re-renders (`internal/app/app.go:1041`). The same divergence exists in `monthCreate` (`internal/app/app.go:983`). Every other locked-month path — a payment edit that falls through to `respondStoreError`, for instance — answers 409.
- **33.e2** A locked month combined with an unparseable amount → the parse error wins, because `parseErr` is set in the loop before `SetBudgets` is ever called (`internal/app/app.go:1031-1033`). The message is the invalid-amount one, and the lock is never mentioned.

**Business rules**

- `ErrLockedMonth` is a distinct sentinel and maps to **This month is locked. Unlock it with a reason before changing budgets or payments.** through `friendly` (`internal/app/app.go:1628-1632`).
- The lock is enforced in the store, so no screen and no hand-crafted POST can get round it (`internal/store/store.go:341-343`).
- Unlocking demands a written reason, so reopening a closed month is always on the record (UC-A-31).

**Data touched** — reads `month_locks`; writes nothing.

**Non-functional / UX notes** — the screen's disabled state and the server's refusal say slightly different things: the banner says *Unlock it before changing budgets*, the error says *Unlock it with a reason before changing budgets or payments*. A test must know which one it is looking at.

**Open questions**

- 33.e1: is a locked month a client error (400) or a conflict (409)? The product currently answers both, depending on which screen you are on. A single answer would make the QA matrix simpler and would not change behaviour.

---

### UC-A-34 — An inactive head is kept out of new work and kept on history

| | |
|---|---|
| **Goal** | "Retire a head without losing what was already budgeted and paid against it." |
| **Primary actor** | AC1 (retires the head); AC4 (meets the consequence when recording a payment) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | A head or its project is set inactive while budgets or payments already reference it |
| **Route(s)** | `POST /heads` (retire); `GET /grid`, `GET /budgets`, `POST /budgets`, `POST /payments`, `GET /payments/new` (consequences) |
| **Permission gate** | `head:edit` to retire; the consequences are on whatever route meets them |
| **Coverage IDs** | **T12** |
| **Priority** | high |

**Preconditions**

1. The head exists and has at least one `budgets` row or one `payments` row.
2. `heads.active = 0`, or its project's `projects.active = 0`.

**Postconditions (success)**

1. `ListHeads(activeOnly=true)` excludes it, so the payment form's **Project / Head** select and the copy-month source both omit it (`internal/store/store.go:296-302`).
2. `Grid` **still returns** the row, because its `WHERE` admits any head with a budget row or a month payment even when inactive; `GridRow.Active` is false (`internal/store/store.go:1387-1400`, `:1419`).
3. The variance grid renders it with `<small class="muted">Retired</small>` in the Action cell instead of an **Add** button (`internal/app/templates.go:146`).
4. The budgets screen renders its input `disabled` with the hint **Retired: kept for history, not editable.** and a `pill neutral` reading **Retired** (`internal/app/templates.go:1160`).
5. Any attempt to record a **new** payment against it is refused with `ErrInactiveHead` (`internal/store/store.go:1627-1634`).

**Postconditions (failure)** — n/a; retirement is not reversible in effect, only in state.

**Main success scenario**

1. AC1 opens `/heads`, clears the head's **Active** checkbox, and presses that row's **Save**.
2. `UpsertHead` writes `active=0`; the row's Status cell now reads **Inactive**.
3. AC1 opens `/grid` for a month in which the head has spend: the row is still there, with its budget, actual and status, and the Action cell reads **Retired**.
4. AC1 opens `/budgets` for that month: the head's row shows the **Retired** pill and a disabled input carrying the hint.
5. AC4 opens the payment entry form: the **Project / Head** select does not offer the head at all.
6. A hand-crafted `POST /payments` naming the head is refused: **400**, **This project/head is inactive.** (`internal/app/app.go:1634-1635`).

**Alternate flows**

- **1.a** The **project** is retired instead of the head. `ListHeads(activeOnly=true)` filters on `h.active=1 AND p.active=1`, so every head under it disappears from new work at once; `Grid` marks each row `Active=false` because it requires both flags (`internal/store/store.go:1419`).
- **3.a** The head has **no** budget and **no** payment in the month → `Grid`'s `WHERE` excludes it entirely and it vanishes from both screens for that month.

**Exception flows**

- **34.e1** A **new** record naming an inactive head: refused with `ErrInactiveHead` → 400 **This project/head is inactive.** for payments (`internal/store/store.go:1632-1633`).
- **34.e2** A budget POST that includes a retired head's id — possible only by hand, since the input is disabled — **succeeds**. `SetBudgets` validates the head id is positive and the amount non-negative but never checks `heads.active` (`internal/store/store.go:327-343`). The screen's `disabled` attribute is the *only* thing protecting a retired head's budget.
- **34.e3** A retired head under an **inactive project** is silently reassigned to another project if anybody presses **Save** on its row (UC-A-29, 29.e5).

**Business rules**

- **T12: inactive projects and heads are hidden from new records and shown on historical ones.** Implemented in three places that must agree: `ListHeads(activeOnly)` for the pickers, `Grid`'s `WHERE` for the historical rows, and `validatePayment` for the write gate.
- The grid's inactive rows exist so a month's actuals stay complete: dropping the row would change the company total.
- `GridRow.Active` requires **both** the head and its project to be active (`internal/store/store.go:1419`).

**Data touched** — reads `heads(active)`, `projects(active)`, `budgets`, `payments`; writes `heads(active)` on retirement.

**Non-functional / UX notes** — the grid says **Retired** and the heads screen says **Inactive** for the same state; the budgets screen says both (**Retired** pill, *Retired:* hint). Three words, one concept.

**Open questions**

- 34.e2 means budget protection for a retired head is presentation-only. Whether `SetBudgets` should refuse an inactive head is a spec decision — refusing it would also block the legitimate case of correcting a historical budget.
- A head retired **after** a month is locked is doubly protected; one retired in an open month relies on the disabled input alone.

---

## 6. The vendor master

### UC-A-35 — Browse, filter and search the vendor master

| | |
|---|---|
| **Goal** | "Find a vendor, and see which of my vendors are missing a GSTIN." |
| **Primary actor** | AC1, AC4, or any holder of `vendor:view` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar **Vendors**, or the dashboard's **Vendors / The vendor master** |
| **Route(s)** | `GET /vendors?q=&type=&category=&status=&gap=` |
| **Permission gate** | `vendor:view` (`internal/app/app.go:431`). The bank block is gated separately inside the store |
| **Coverage IDs** | adoption-spec **G5** / **D3**; T3 (bank details stay off the request form) |
| **Priority** | high |

**Preconditions**

1. The actor holds `vendor:view`.
2. `vendors` exists (migration v2, `internal/store/migrations.go:95-135`).

**Postconditions (success)** — 200 HTML titled `Vendors - Fervid Budget`; nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. The actor opens `/vendors`.
2. The system resolves the caller's permission set and hands it to `ListVendors`, which appends the bank columns to the projection **only** for a holder of `vendor_bank:view` (`internal/app/vendors.go:21-34`, `internal/store/vendors.go:204-260`).
3. The system also loads `VendorStats` — total, active, inactive, missing-GSTIN — **independently of the active filters**, because "3 missing a GSTIN" is a fact about the master, not about the current search (`internal/store/vendors.go:262-281`), and the category option list, derived from what is actually in use (`internal/store/vendors.go:283-311`).
4. The system renders the banner: eyebrow **Masters**, `<h1>Vendors</h1>`, sub **{N} active · {M} missing a GSTIN · bank details visible only with permission**, and — for a holder of `vendor:create` — **＋ Add vendor** linking to `/vendors/new` (`internal/app/templates.go:1280-1287`).
5. When any active vendor lacks a GSTIN the system renders `div.banner.warn`: **{N} vendor has / vendors have no GSTIN** and **They can still be paid. The gap shows up in vendor reporting until someone fills it in.**, with **Show them** (→ `/vendors?gap=gstin`) or **Show all** (→ `/vendors`) when the filter is already on (`internal/app/templates.go:1289-1298`).
6. The system renders the desktop `form.toolbar` (a real GET form, so filtering works with JavaScript off): **Search** (`id="q"`, placeholder *Name, GSTIN, PAN, city*), **Type** (*All types* / *Company* / *Proprietor* / *Individual*), **Category** (*All categories* + the in-use list), **Status** (*Active* / *Inactive* / *All*, defaulting to Active), and **Apply** (`internal/app/templates.go:1300-1319`).
7. The system also renders `form.m-filters` for phones: a search box with the placeholder **Search vendors…** and a **Filters** button, carrying the current status as a hidden field. The stylesheet hides whichever form does not belong at that width (`internal/app/templates.go:1321-1325`).
8. The system renders `table.t-cards` with headers **Vendor**, **Type**, **GSTIN**, **City**, **Paid this year**, **Open requests**, **Status**. Each row links the name to `/vendors/{id}`, shows the categories as a `·` chain beneath it, shows a `pill warn no-dot` reading **Missing** where the GSTIN is blank, an em dash for a blank city, and an **Active** / **Inactive** pill (`internal/app/templates.go:1327-1344`).
9. The footer row reads **{N} shown of {Total}** and totals **Paid this year** and **Open** (`internal/app/templates.go:1345-1353`).
10. With no matches the table shows **No vendors match these filters.**

**Alternate flows**

- **6.a** **Search** matches `name`, `display_name`, `gstin`, `pan` or `city`, case-insensitively, as a substring; wildcards in the term are escaped so a search for `100%` is a search (`internal/store/vendors.go:229-234`, `:560-568`).
- **6.b** **Category** filters with `instr(lower(categories), ?) > 0` — a substring test on the whole comma-separated field, so the term `service` also matches *Housekeeping Services* (`internal/store/vendors.go:225-228`).
- **6.c** **Status** defaults to `active`; `all` removes the clause entirely (`internal/store/vendors.go:214-220`).
- **5.a** No active vendor is missing a GSTIN → the banner is absent.

**Exception flows**

- **35.e1** No `vendor:view` → 403 (UC-A-09).
- **35.e2** More than 300 matches → silently truncated: `ListVendors` defaults `Limit` to 300 and nothing on the screen says so (`internal/store/vendors.go:206-208`). The footer's **{N} shown of {Total}** is the only hint.
- **35.e3** `VendorStats` or `VendorCategories` errors → the whole screen becomes an error page (`internal/app/vendors.go:35-44`).

**Business rules**

- One screen cannot become a side door onto another's secret: `ListVendors` applies the same bank gate as `Vendor` (`internal/store/vendors.go:202-205`).
- A nil permission set is a **denial**, never an absence of policy (`internal/store/vendors.go:133-137`).
- `SplitCategories` decides where a category ends, and both the filter list and the `·` chain use it, so the two can never disagree (`internal/store/vendors.go:313-324`, `internal/app/app.go:1858-1864`).

**Data touched** — reads `vendors` (bank columns only when entitled), `payments` (the paid-this-year subquery).

**Non-functional / UX notes** — the banner keeps its `h1` at every width; the mobile top bar carries a title too, but the shell contract is that every screen has a visible `h1` (`internal/app/templates.go:1267-1272`). Two filter renderings, one dataset, both real GET forms.

**Open questions**

- **"Paid this year" matches payments by payee *name*.** `payments` has no `vendor_id` column at all (`internal/store/schema.go:58-75`), so the subquery joins on `lower(trim(payments.vendor_payee)) = lower(trim(vendors.name))` for the current calendar year (`internal/store/vendors.go:150-160`). It therefore **under-counts** a payment whose payee was typed differently, and can never credit the wrong vendor because vendor names are uniquely indexed. Confirmed against PROGRESS.md → Known gaps.
- **"Open requests" is 0 for every vendor, always.** `Vendor.OpenRequests` is declared and read by the template (`internal/app/templates.go:1338`) and summed into the footer (`internal/app/vendors.go:49`), but **no query ever populates it** — a full grep of `internal/` finds only the declaration, the template and the sum. Confirmed against `internal/store/vendors.go:56-59`.
- **There is no vendor CSV export.** The approved mockup has one; `vendor` carries only `view`, `create`, `edit` in the canonical vocabulary (`internal/store/permissions.go:156`), so there is no `export` action to gate a route on. Confirmed against PROGRESS.md → Known gaps.
- The mobile **Filters** button is a plain submit that re-runs the search; it opens no filter sheet. Type, category and status are unreachable on a phone.

---

### UC-A-36 — Find a vendor from the request form's combobox

| | |
|---|---|
| **Goal** | "Type three letters and pick the vendor I mean." |
| **Primary actor** | AC2 (on the request form) — the fragment itself is reachable by any holder of `vendor:view` |
| **Supporting actors** | UC-B owns the request form that mounts this fragment |
| **Scope / level** | system · subfunction |
| **Trigger** | Typing into the vendor picker on the request form; htmx issues the GET |
| **Route(s)** | `GET /vendors/search?q=…` |
| **Permission gate** | `vendor:view` (`internal/app/app.go:433`) |
| **Coverage IDs** | **T3**, T6, T7 (the fields this picker fills); G5 |
| **Priority** | high |

**Preconditions**

1. The actor holds `vendor:view`.
2. `q` is non-empty after trimming — an empty term returns no rows at all (`internal/store/vendors.go:336-339`).

**Postconditions (success)**

1. 200 with an HTML **fragment** starting at `div.combo-list` — it never renders `top`/`bottom`, so it can be swapped straight into the picker (`internal/app/templates.go:1365-1369`).
2. At most **10** options, only `status='active'` vendors, with **no bank columns in the projection at all** (`internal/store/vendors.go:335-369`).
3. Nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. The actor types `sund`; htmx requests `GET /vendors/search?q=sund` with `HX-Request` set.
2. `SearchVendors` escapes the wildcards, searches `name`, `display_name`, `gstin` and `city` as substrings, and orders exact **prefix** matches first — "someone who has typed *sund* is looking for a vendor whose name starts that way" (`internal/store/vendors.go:326-356`).
3. The system renders one `a.co[role="option"]` per vendor, carrying `data-id` and `data-name`, whose main line is the name and whose sub-line is the GSTIN (or **No GSTIN**) and the city (`internal/app/templates.go:1366`).
4. For a holder of `vendor:create` the list ends with **＋ Add a new vendor** linking to `/vendors/new` — offered only to a caller who could actually complete it, "because an affordance that 403s is worse than no affordance" (`internal/app/templates.go:1359-1368`).
5. The actor picks an option; UC-B's form records the id.

**Alternate flows**

- **2.a** No vendor matches → a single non-interactive `span.co` reading **No vendor matches “{q}”** with the sub-line **Only active vendors can be picked.**
- **2.b** `q` is empty → `SearchVendors` returns nil and the same empty slot renders **Type a name, GSTIN or city** instead (`internal/app/templates.go:1367`).
- **1.a** The route is requested **without** `HX-Request`, e.g. by typing the URL. It still answers 200 with the bare fragment, because the template never calls `top`. `renderStatus` even builds the shell (and runs the badge queries) for nothing, since the handler uses `a.render` rather than `renderPartial` (`internal/app/vendors.go:84-92`, `internal/app/http_errors.go:121-125`).

**Exception flows**

- **36.e1** No `vendor:view` → 403 — and because htmx swaps the response body, the picker would receive a 403 error page. Nothing sets `HX-Reswap` or similar.
- **36.e2** An **inactive** vendor is never offered, even by exact name: `status='active'` is hard-coded in the WHERE. An inactive vendor stays readable on the requests that already name it (`internal/store/vendors.go:326-334`).

**Business rules**

- The picker is hard-wired to the bank-free projection and takes **no** `PermissionSet`, so it cannot become a side door onto the restricted block (`internal/store/vendors.go:326-330`) — this is T3 enforced at the query, not at the markup.
- The limit is clamped to 1–25, and defaults to 10 when the caller passes 0 (`internal/store/vendors.go:340-345`). The handler always passes 0 (`internal/app/vendors.go:86`).
- Served as HTML rather than JSON so there is no client renderer to keep in step with the server's idea of a vendor (`internal/app/vendors.go:79-83`).

**Data touched** — reads `vendors` (non-bank columns only).

**Non-functional / UX notes** — `div.combo-list` carries `role="listbox"` and `aria-label="Vendor results"`; each option carries `role="option"`. The empty and no-match states are rendered as non-focusable `span.co`, so they cannot be selected.

**Open questions**

- The fragment is served through `a.render`, which builds the whole shell — nav, tab bar, badge counts — and then discards it. `renderPartial` exists for exactly this case (`internal/app/app.go:536-552`). A wasted badge query per keystroke.

---

### UC-A-37 — Add a vendor

| | |
|---|---|
| **Goal** | "Register a new supplier so a request can name them and Accounts can pay them." |
| **Primary actor** | AC1, or any holder of `vendor:create` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | **＋ Add vendor** on `/vendors`, or **＋ Add a new vendor** in the combobox |
| **Route(s)** | `GET /vendors/new`; `POST /vendors` → 303 `/vendors/{newID}` |
| **Permission gate** | `vendor:create` for both (`internal/app/app.go:432`, `:435`) + CSRF on the POST |
| **Coverage IDs** | G5 / D3; T3 |
| **Priority** | high |

**Preconditions**

1. The actor holds `vendor:create`.
2. **Vendor name**, trimmed, is non-empty (`internal/store/vendors.go:401-403`).
3. **Type** is `company`, `proprietor` or `individual` — blank defaults to `company` (`internal/store/vendors.go:395-406`).
4. **Status** is `active` or `inactive` — blank defaults to `active`.
5. **GSTIN**, if given, is exactly 15 characters (`internal/store/vendors.go:410-412`).
6. The name is unique case-insensitively (`idx_vendors_name_nocase`, `internal/store/migrations.go:131`).

**Postconditions (success)**

1. One `vendors` row with every submitted field trimmed, GSTIN and PAN upper-cased (`internal/store/vendors.go:375-393`, `:443-450`).
2. The bank block is written **only** when the submitter held `vendor_bank:edit` (`internal/app/vendors.go:147-186`, `internal/store/vendors.go:458-462`).
3. One `audit_log` row: `action='create'`, `entity_type='vendor'`, `entity_id={newID}`, `summary='Created vendor {name}'`, `after_json` holding name, vendor_type, status, gstin, city and a **`bank_changed` boolean — never the bank values themselves**, because `audit:view` and `vendor_bank:view` are different permissions (`internal/store/vendors.go:463-470`, `:545-558`).
4. The browser lands on `/vendors/{newID}`, the record's own detail screen.

**Postconditions (failure)** — nothing written (one transaction, `internal/store/vendors.go:437-474`); the form re-renders with everything the actor typed.

**Main success scenario**

1. The actor presses **＋ Add vendor**.
2. `vendorNew` builds a blank vendor with `VendorType: "company"` and `Status: "active"`, and attaches an **empty** bank block for a holder of `vendor_bank:view` — because "this vendor has no bank details yet" is something they are entitled to be told (`internal/app/vendors.go:66-77`).
3. The system renders the detail template in create mode: eyebrow **Masters · vendor**, `<h1>Add vendor</h1>`, sub **A vendor the request form can find, and Accounts can pay.**, and a **Cancel** link back to `/vendors` (`internal/app/templates.go:1389-1396`).
4. The `.segmented` tab strip is **not** rendered for a new vendor (`internal/app/templates.go:1398-1405`).
5. The actor fills the **Identity** fieldset — **Vendor name \*** (required), **Short name** (hint *Used in lists and dropdowns.*), **Type \***, **Categories** (hint *Comma separated. Used for vendor reporting.*), **Status** (hint *Inactive vendors disappear from new requests but stay on old ones.*) (`internal/app/templates.go:1410-1426`).
6. Optionally the **Statutory** fieldset — **GSTIN** (`maxlength="15"`, hint *15 characters, or blank if the vendor has none.*), **PAN**, **MSME / Udyam number**, **TDS section** (*Not applicable* / *194C — contractors* / *194J — professional* / *194Q — purchase of goods*), **Default TDS rate** (placeholder `2%`), and the standing hint *Recorded for reference only. This system never computes tax — Accounts confirms deductions outside it.* (`internal/app/templates.go:1428-1443`).
7. Optionally the **Contact** fieldset — **Contact person**, **Phone**, **Email**, **Billing address**, **City**, **State**, **State code** (hint *The first two digits of the GSTIN.*) — and **Notes** → **Internal notes**.
8. The action bar's note reads **The name must be unique. Everything else can be filled in later.**; the actor presses **Save vendor** (`internal/app/templates.go:1504-1508`).
9. `vendorCreate` normalises, inserts, writes the bank block if entitled, audits, commits, and redirects 303 to the new record.

**Alternate flows**

- **6.a** The actor holds `vendor_bank:view` **and** `vendor_bank:edit` → the **Payment details — restricted** fieldset is rendered and editable (UC-A-39).
- **8.a** The actor presses **Cancel** → a plain link to `/vendors`; nothing is posted.

**Exception flows**

- **37.e1** Blank name → **400** and the form re-renders in place with `div.alert.error` reading `validation failed: a vendor name is required` **and every other typed value preserved** (`internal/app/vendors.go:108-113`, `internal/store/vendors.go:401-403`; pinned at `internal/app/app_integration_test.go:1394-1400`).
- **37.e2** A GSTIN of any length other than 15 → **400**, `validation failed: a GSTIN is 15 characters; leave it blank if the vendor has none`.
- **37.e3** An unrecognised type or status → **400** with the corresponding message. Not reachable from the selects.
- **37.e4** A duplicate name → **400**, **A record with this name already exists.** The unique index decides, because check-then-insert is a race (`internal/store/vendors.go:429-431`).
- **37.e5** No `vendor:create` → 403 on both the form and the POST.
- **37.e6** Missing CSRF → 403.

**Business rules**

- The store checks the GSTIN's **length only** — it records statutory identifiers, it never computes tax from them (`internal/store/vendors.go:118-121`; X1).
- Bank details are read from the form only for a holder of `vendor_bank:edit`, because `CreateVendor` takes no permission set and this is where a create is gated (`internal/app/vendors.go:147-151`).
- The audit payload records **that** the bank block changed, never what it changed to (`internal/store/vendors.go:545-549`).
- A rejected submission echoes the bank block back **only** when the submitter was allowed to send it (`internal/app/vendors.go:188-201`).

**Data touched** — writes `vendors` (including the bank columns when entitled), `audit_log`.

**Non-functional / UX notes** — required fields carry a visible `*` marked `aria-hidden="true"` plus the input's own `required`. The 12-column grid uses `m-half` on narrow fields so the form is two-up at 390px. On the error path `renderVendorForm` is called with `editable=true` regardless of the caller's grants (`internal/app/vendors.go:110`) — harmless on the create path, which already requires `vendor:create`.

**Open questions**

- `vendorCreate` re-renders with `VendorEditable: true` unconditionally; on the **update** path the same shortcut means a rejected save always shows an editable form even though the route already required `vendor:edit` (`internal/app/vendors.go:125`). Cosmetic today.

---

### UC-A-38 — Open and edit a vendor record

| | |
|---|---|
| **Goal** | "Correct this vendor's phone number." |
| **Primary actor** | AC1, AC4, or any holder of `vendor:view` (read) / `vendor:edit` (write) |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Clicking a vendor's name on `/vendors` |
| **Route(s)** | `GET /vendors/{id}`; `POST /vendors/{id}` → 303 `/vendors/{id}` |
| **Permission gate** | `vendor:view` to read, `vendor:edit` to write (`internal/app/app.go:434`, `:436`) + CSRF |
| **Coverage IDs** | G5 / D3 |
| **Priority** | high |

**Preconditions**

1. The actor holds `vendor:view`; `vendor:edit` to save.
2. `{id}` names an existing vendor (`internal/store/vendors.go:195-200`).
3. The same normalisation rules as UC-A-37 apply to the submitted values.

**Postconditions (success)**

1. Every non-bank column is rewritten, `updated_at` refreshed (`internal/store/vendors.go:503-513`).
2. The bank block is written **only** when the caller holds `vendor_bank:edit`; otherwise it is **ignored rather than rejected**, so a clerk without the bank permission can fix a phone number without being blocked by a block they cannot see (`internal/store/vendors.go:477-482`, `:515-520`).
3. One `audit_log` row: `action='update'`, `entity_type='vendor'`, `summary='Updated vendor {name}'`, `before_json={"name":<old name>}`, `after_json` as in UC-A-37.
4. The browser returns to `/vendors/{id}`.

**Postconditions (failure)** — nothing written (one transaction, `internal/store/vendors.go:488-530`); the form re-renders with the submitted values.

**Main success scenario**

1. The actor clicks the vendor's name on `/vendors`.
2. `vendorDetail` loads the vendor **through the caller's permission set** and sets `VendorEditable` from `vendor:edit` (`internal/app/vendors.go:94-102`).
3. The system renders eyebrow **Masters · vendor**, `<h1>{vendor name}</h1>`, and the sub-line **{Type} · {City} · {Status}**.
4. The system renders `div.segmented`: **Record** (active) plus three `aria-disabled` placeholders — **Requests**, **Payments**, **History** — each carrying a `Soon` chip (`internal/app/templates.go:1398-1405`).
5. The four editable fieldsets render as in UC-A-37, each `disabled` when `VendorEditable` is false.
6. The action bar's note reads **Created {date} · last edited {date}**; for an editor it offers **Cancel** and **Save vendor**, and for a reader only **Back to vendors** (`internal/app/templates.go:1504-1508`).
7. The actor changes **Phone** and presses **Save vendor**.
8. `vendorUpdate` normalises, rewrites the row, applies the bank block if entitled, audits, and redirects 303.

**Alternate flows**

- **5.a** Without `vendor:edit` every fieldset is `disabled` and the page-banner action reads **Back to vendors** instead of **Cancel** (`internal/app/templates.go:1395`).
- **7.a** Setting **Status** to *Inactive* removes the vendor from the request-form combobox at once, while leaving it on the requests that already name it (UC-A-36, 36.e2).

**Exception flows**

- **38.e1** `{id}` names no vendor, or is unparseable (so `pathID` yields 0) → **404**, **The requested record was not found.**
- **38.e2** A validation failure (blank name, bad GSTIN, unknown type/status) → **400** with the form re-rendered in place and the input preserved (`internal/app/vendors.go:123-127`).
- **38.e3** A duplicate name → **400**, **A record with this name already exists.**
- **38.e4** `ErrNotFound` on the update path is routed to `respondStoreError` rather than the in-place re-render, deliberately (`internal/app/vendors.go:124`).
- **38.e5** No `vendor:edit` → 403; the **Save vendor** button is correctly not rendered, so this needs a hand-crafted POST.
- **38.e6** Missing CSRF → 403.

**Business rules**

- Every read hands the caller's permission set to the store, which decides whether the bank block comes back at all; the template's `.Perms` check is presentation only (`internal/app/vendors.go:12-18`).
- A bank block the caller may not edit is dropped silently — refusing the whole save would make the contact fields unusable to exactly the people who maintain them (`internal/store/vendors.go:477-482`).
- GSTIN and PAN are stored upper-cased; IFSC too (`internal/store/vendors.go:381-382`, `:539`).

**Data touched** — reads and writes `vendors`; writes `audit_log`.

**Non-functional / UX notes** — three of the four `.segmented` tabs are `Soon` placeholders with no `href`, so they are not focusable and cannot be clicked. `VendorEditable` collapses two questions into one — `vendor:create` on a new record, `vendor:edit` on an existing one (`internal/app/app.go:113-116`).

**Open questions**

- The **Requests**, **Payments** and **History** tabs are announced but unbuilt. Given "Open requests" is always 0 (UC-A-35), the vendor screen currently promises three views it cannot fill.

---

### UC-A-39 — Vendor bank details are withheld from a caller without the grant

| | |
|---|---|
| **Goal** | "Only the people who pay vendors should see how they are paid." |
| **Primary actor** | AC7 — a custom role holding `vendor:view` with and without `vendor_bank:view` / `vendor_bank:edit` |
| **Supporting actors** | AC1 |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | Opening `/vendors` or `/vendors/{id}` |
| **Route(s)** | `GET /vendors`, `GET /vendors/{id}`, `POST /vendors/{id}`, `GET /vendors/search` |
| **Permission gate** | `vendor_bank:view` decides the **projection**; `vendor_bank:edit` decides whether the block is read from the form. Neither gates a route |
| **Coverage IDs** | **T3**, G5 / D3, R8 |
| **Priority** | critical |

**Preconditions**

1. A vendor exists with bank details stored.
2. The reader holds `vendor:view` and — for the negative case — **not** `vendor_bank:view`.

**Postconditions (success — the withholding)**

1. The bank columns are **not in the SQL projection at all** for a caller without `vendor_bank:view`: `vendorSelect(false)` omits them and `Vendor.Bank` stays **nil** (`internal/store/vendors.go:123-148`, `:162-193`).
2. The screen renders a substitute fieldset `<legend>Payment details</legend>` containing `div.banner.locked` with **🔒 You do not have permission to see bank details** and **Accounts maintains them. The record is hidden, not just the buttons.** (`internal/app/templates.go:1484-1495`).
3. The response body contains none of `bank_ifsc`, `bank_account_number`, the IFSC, the account number, or the bank name — pinned at `internal/app/app_integration_test.go:1326-1331`.
4. Nothing is written.

**Postconditions (failure)** — n/a.

**Main success scenario**

1. AC1 builds a role holding exactly `vendor:view` and assigns it to a user.
2. That user opens `/vendors/{id}`.
3. `vendorDetail` passes the permission set to `store.Vendor`, which selects only `vendorColumns` (`internal/store/vendors.go:196-199`).
4. The template's condition `{{if and (.Perms.Can "vendor_bank" "view") .Vendor.Bank}}` is false on both halves, so the locked banner takes the fieldset's place — deliberately, because hiding the section entirely would leave a reader wondering whether the vendor has no bank details or whether they simply cannot see them (`internal/app/templates.go:1371-1386`).
5. The rest of the record — GSTIN, contact, notes — is still readable (`internal/app/app_integration_test.go:1344-1347`).

**Alternate flows**

- **`vendor_bank:view` without `:edit`** → the values render, and every bank input carries its own `disabled` attribute, because `vendor_bank:edit` is a separate permission from `vendor:edit`: someone may edit the contact block and not the bank block. A disabled input is not submitted, so the browser cannot send what the caller may not change — and `UpdateVendor` ignores it regardless (`internal/app/templates.go:1382-1386`, `:1469-1481`; pinned at `internal/app/app_integration_test.go:1349-1360`).
- **`vendor_bank:view` and `:edit`** → the fieldset renders as **Payment details — restricted** with `div.banner.locked` reading **🔒 Visible only with vendor bank permission** and **They are deliberately kept off the employee request form — an employee raising a request never sees or types them.** Its fields are **Account name**, **Account number**, **IFSC**, **Bank**, **Branch**, **UPI ID** (placeholder *Optional*), **Default mode** (*Not set* / *Bank transfer — NEFT* / *RTGS* / *Cheque*) and **Payment terms (days)** (`internal/app/templates.go:1458-1483`).
- **The combobox** never returns bank details to anybody, whatever they hold (UC-A-36).

**Exception flows**

- **39.e1** A hand-crafted `POST /vendors/{id}` carrying `bank_ifsc`, `bank_account_number` and `bank_account_name` from a caller holding `vendor:edit` but **not** `vendor_bank:edit` → the contact change is applied and **the bank block is untouched**. `vendorInputFromForm` does not even read the fields, and `UpdateVendor` re-applies the rule independently: "neither relies on the other" (`internal/app/vendors.go:147-151`, `internal/store/vendors.go:515-516`; pinned at `internal/app/app_integration_test.go:1415-1445`).
- **39.e2 (defect)** A caller holding **`vendor_bank:edit` but not `vendor_bank:view`** — a combination the matrix allows, since View and Edit are independent cells on the **Vendor bank details** row (`internal/app/permmap.go:128-136`) — is shown the locked banner and **no bank inputs at all**. Pressing the ordinary **Save vendor** therefore submits **no** bank fields, `vendorInputFromForm` still builds a non-nil `in.Bank` from the empty values because the caller holds `vendor_bank:edit` (`internal/app/vendors.go:172-184`), `bankTouched` is true (`internal/store/vendors.go:515`), and `writeVendorBankTx` overwrites **every bank column with an empty string** (`internal/store/vendors.go:533-543`). **A routine contact edit silently erases the vendor's account number, IFSC, bank, branch, UPI ID, default mode and payment terms.** No test covers this combination — `TestVendorUpdateIgnoresBankFieldsFromACallerWithoutBankEdit` covers the *without-edit* case only.
- **39.e3** The audit row for a bank change records `bank_changed: true` and nothing else, so an account number never appears in `before_json`/`after_json` where the weaker `audit:view` grant would reach it (`internal/store/vendors.go:545-549`).

**Business rules**

- The **data** gate is the one that matters; the markup gate is presentation. By the time the template runs, a caller without `vendor_bank:view` is holding a `Vendor` whose `Bank` is nil, so there is nothing for a mistaken `{{…}}` to print (`internal/app/vendors.go:12-18`).
- `nil` means **withheld**, not **empty** (`internal/store/vendors.go:48-50`).
- Bank columns live on the vendor row rather than a second table on purpose: splitting them would move the secret without adding a boundary, because the same code would join it (`internal/store/migrations.go:84-89`).
- T3: bank details are excluded from the request form entirely.

**Data touched** — reads `vendors` (projection varies by grant); writes `vendors` bank columns only when `vendor_bank:edit` is held.

**Non-functional / UX notes** — the locked banner is the design system's `banner locked` with a `🔒` glyph, and it is the only place the product explains an absence of data rather than an absence of buttons.

**Open questions**

- **39.e2 is the most serious defect in this document.** The safest fix is to make `vendorInputFromForm` require **both** `vendor_bank:view` and `vendor_bank:edit`, or to make `writeVendorBankTx` a no-op when every submitted field is empty. Until then, `vendor_bank:edit` must never be granted without `vendor_bank:view`, and nothing in the roles screen says so.
- The payment-detail mockup's `Bank` row (`HDFC ····4471 · IFSC …`) is deliberately unbuilt: it needs `vendor_bank`-restricted data on a screen no spec authorises for it (PROGRESS.md → Known gaps).

---

## 7. Evidence and operations

### UC-A-40 — Review the audit log

| | |
|---|---|
| **Goal** | "Show me who changed a financial record, when, and why." |
| **Primary actor** | AC1, or any holder of `audit:view` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar **Audit log** |
| **Route(s)** | `GET /audit?entity=&id=&action=&actor=` |
| **Permission gate** | `audit:view` (`internal/app/app.go:521`) |
| **Coverage IDs** | **C2** |
| **Priority** | high |

**Preconditions**

1. The actor holds `audit:view`.
2. `audit_log` exists (`internal/store/schema.go:95-107`).

**Postconditions (success)** — 200 HTML titled `Audit Log - Fervid Budget`; nothing written. Reading the audit log is not itself audited.

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. The actor opens `/audit`.
2. The system reads the newest **1000** rows for the chosen entity type (all types when blank), ordered `created_at DESC` (`internal/app/app.go:1368`, `internal/store/store.go:1591-1618`).
3. The system filters that page **in memory** by action (exact match) and actor (case-insensitive substring), then truncates to **200** rows (`internal/app/app.go:1373-1388`).
4. The system renders the banner: eyebrow **Evidence**, `<h1>Audit Log</h1>`, sub **Review who changed financial records, when, and why.**
5. The system renders `form.toolbar` with **Entity** (*All*, *Payment*, *Budget*, *Monthly plan*, *Month lock*, *Project*, *Head*, *User*, *Report*, *Backup*), **Action** (*All*, *Created*, *Attached*, *Updated*, *Voided*, *Locked*, *Unlocked*, *Exported*, *Login*, *Failed login*, *Logout*), **Actor** (text, placeholder *Name*), then **Filter** and a **Reset** link to `/audit` (`internal/app/templates.go:3262`).
6. The system renders `table.t-cards` with headers **When**, **Actor**, **Entity**, **Action**, **Summary**. The timestamp is localised (`internal/app/app.go:342`), the entity is title-cased with underscores replaced, and the action is a `pill` whose tone is `good` for create, `info` for update, `warn` for lock/unlock, `bad` for void and failed login, and `neutral` otherwise (`internal/app/app.go:1882-1922`).
7. Where before/after JSON exists, the summary cell carries a `<details><summary>Before / after</summary>` with each side pretty-printed (`internal/app/templates.go:3263`).
8. With no matches the table shows **No audit entries match these filters.**

**Alternate flows**

- **5.a** `?id=` narrows to a single entity instance, but **only when `entity` is also given** — the store adds the id clause inside the entity branch (`internal/store/store.go:1594-1600`). There is no control for it on the screen.
- **6.a** At ≤860px the table restacks into labelled cards (`internal/app/phase6_screens_test.go:32-45`).

**Exception flows**

- **40.e1** No `audit:view` → 403 (UC-A-09).
- **40.e2 (gap)** **Several entity types cannot be chosen from the Entity select at all**: `role`, `vendor`, `app_setting`, `payment_request`, `recoverable_category`, `reservation` and the notification entities all write audit rows but have no option (`internal/app/templates.go:3262` vs the writers at `internal/store/permissions.go:258`, `internal/store/vendors.go:463`, `internal/store/settings.go:76`, `internal/store/recoverables.go:219`). They appear under *All*, and can be filtered only by hand-editing `?entity=role`.
- **40.e3 (gap)** The **Action** select likewise omits `delete` (role deletion) and `settings` (a configuration save), so the two administration actions with the widest blast radius cannot be filtered for. `actionText("settings")` also has no case, so the pill renders the raw string **settings** in lower case (`internal/app/app.go:1882-1907`).
- **40.e4** More than 200 matching rows → silently truncated with no pagination and no notice (`internal/app/app.go:1386-1388`).
- **40.e5** The action/actor filters apply only **within** the newest 1000 rows of the selected entity, so an older matching row is invisible even though it exists.

**Business rules**

- Every mutation writes an audit row through `RecordAudit` or `recordAuditTx`; the transactional form is used wherever the row must live or die with the change (`internal/store/store.go:1583-1589`).
- `before_json`/`after_json` are written as NULL when empty, so `hasText` can suppress the disclosure (`internal/store/store.go:1584-1587`, `internal/app/app.go:1928-1930`).
- Vendor bank values are never in the audit payload (UC-A-39).
- `CURRENT_TIMESTAMP` is second-resolution, so a burst of rows shares a timestamp and any ordering must tie-break on `id` — the display order does not (PROGRESS.md trap).

**Data touched** — reads `audit_log`.

**Non-functional / UX notes** — `entityText` uses `strings.Title` (deprecated) so `app_setting` renders as **App Setting** (`internal/app/app.go:1924-1926`). The 1000-row read happens on every page load with no index hint beyond `idx_audit_created` and `idx_audit_entity`.

**Open questions**

- The screen's two filter vocabularies were written for the pre-RBAC product and have not been extended for roles, vendors, configuration, requests or recoverables. For an auditor asking "who changed the permissions?", the answer requires a hand-typed URL.
- A password reset is indistinguishable from a rename in the log (UC-A-22).

---

### UC-A-41 — Create and review backups

| | |
|---|---|
| **Goal** | "Take a copy of the database and the attachments before I do something risky." |
| **Primary actor** | AC1, or any holder of `backup:view` + `backup:create` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar **Backups** |
| **Route(s)** | `GET /backups`; `POST /backups` → 303 `/backups` |
| **Permission gate** | `backup:view` / `backup:create` + CSRF (`internal/app/app.go:522-523`) |
| **Coverage IDs** | none (operations screen, predates the matrix); C2 for the audit row |
| **Priority** | medium |

**Preconditions**

1. The actor holds `backup:view` to read, `backup:create` to act.
2. `cfg.BackupDir` exists and is readable — `EnsureDirs` creates it at start-up (`internal/config/config.go:44-51`).
3. `cfg.DBPath` exists (`internal/store/backup.go:51-53`).

**Postconditions (success)**

1. A new directory `{BackupDir}/backup-YYYYMMDD-HHMMSS` containing the SQLite copy, an `attachments/` tree, and a manifest recording `created_at`, the database file name, the attachment directory and both source paths (`internal/store/backup.go:47-105`).
2. The directory is assembled under a `.tmp` suffix and moved into place with a single `os.Rename`, so a partially written backup is never visible (`internal/store/backup.go:63-98`).
3. Backups older than `cfg.BackupKeepDays` (default **30**) are pruned; a pruning failure is logged at WARN and does not fail the request (`internal/app/app.go:1414-1416`, `internal/config/config.go:39`).
4. One `audit_log` row: `action='create'`, `entity_type='backup'`, `summary='Created backup {path}'` — with **no** `entity_id` (`internal/app/app.go:1417-1418`).
5. The browser is back on `/backups` with the new folder listed first.

**Postconditions (failure)** — no backup directory survives: the temp tree is removed by the deferred cleanup (`internal/store/backup.go:71-76`).

**Main success scenario**

1. The actor opens `/backups`.
2. The system reads `cfg.BackupDir`, keeps only directories whose names start with `backup-`, and sorts them in reverse — newest first (`internal/app/app.go:1392-1405`).
3. The system renders the banner: eyebrow **Operations**, `<h1>Backups</h1>`, sub **Create timestamped backups of the SQLite database and payment attachments.**, and — inside the banner — a form whose button reads **Create Backup** (`internal/app/templates.go:3279`).
4. The system renders `table.t-cards` with headers **Backup folder** and **Status**, each row showing the folder name and a `pill good` reading **Available**.
5. The actor presses **Create Backup**; the browser confirms **Create a fresh backup now?**
6. `backupCreate` runs the backup, prunes, audits, and redirects 303 to `/backups`.
7. With no backups the table shows **No backups created yet. Use Create Backup above.**

**Alternate flows**

- **2.a** A directory not named `backup-…` in the same folder is ignored, as is any file.
- **5.a** Two backups within the same second → `uniqueBackupPath` disambiguates (`internal/store/backup.go:249`).

**Exception flows**

- **41.e1** `os.ReadDir(BackupDir)` fails — the directory was removed or is unreadable → **500**, **Backups could not be listed.** (`internal/app/app.go:1393-1396`).
- **41.e2** The backup itself fails — the database file is missing, the disk is full → **500**, **The backup could not be created.**, and no audit row (`internal/app/app.go:1408-1412`).
- **41.e3** No `backup:create` → 403; the **Create Backup** button is **not** gated in the template, so a `backup:view`-only actor sees it and gets 403 (`internal/app/templates.go:3279`).
- **41.e4** Missing CSRF → 403.

**Business rules**

- A backup is the database **plus** the attachment tree **plus** a manifest — restoring one needs all three (`internal/store/backup.go:78-94`).
- The SQLite half is taken with `VACUUM INTO`, not a file copy, because "a plain file copy can miss pages that are still in a WAL file" — the image is transactionally consistent under concurrent writes (`internal/store/backup.go:197-227`).
- Pruning is best-effort and never fails the request.
- There is **no restore route and no restore control**. Restoring is a command-line operation: `server -restore <backup dir>`, which restores and exits, and whose help text says *stop the app first* (`cmd/server/main.go:27`, `:36-43`). Nothing in `internal/app` calls `store.Restore`. The same binary also offers `-backup` and `-seed` (`cmd/server/main.go:25-26`).

**Data touched** — reads the database file and `cfg.AttachmentDir`; writes to `cfg.BackupDir`; writes `audit_log`.

**Non-functional / UX notes** — the button lives inside `section.page-banner`, so at 390px it sits under the sub-line rather than in a separate action bar. The confirmation is `onsubmit="return confirm(...)"`, so with JavaScript off a backup starts on the first click. A large attachment tree makes this a slow synchronous request against a 2-minute write timeout (`internal/app/app.go:330`).

**Open questions**

- The list shows only folder names — no size, no row counts, no manifest data — even though the manifest is written. There is also no download link, so a backup can only be retrieved from the server's filesystem.
- The audit row records the full server path in the summary, which leaks the deployment layout to anyone with `audit:view`.

---

## 8. The variance grid, reports and exports

### UC-A-42 — Read the variance grid

| | |
|---|---|
| **Goal** | "Show me budget against actual for every head this month, and what is left." |
| **Primary actor** | AC3, AC4, AC1 — and, in practice, **any signed-in user** (see the gate) |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar **Variance grid**, the dashboard's **Budget / Variance grid** tile, or the mobile tab bar's **Budget** tab |
| **Route(s)** | `GET /grid?month=YYYY-MM&status=&q=` |
| **Permission gate** | **Session only.** `mux.Handle("GET /grid", a.auth.RequireLogin(…))` — `grid:view` is never consulted (`internal/app/app.go:377`) |
| **Coverage IDs** | **V2**, C3, T12 |
| **Priority** | critical |

**Preconditions**

1. The actor is signed in.
2. `month` parses as `2006-01`; anything else silently becomes the current month (`internal/app/app.go:630`, `:1536-1548`).

**Postconditions (success)** — 200 HTML titled `Variance Grid - Fervid Budget`; nothing written. Three store reads: the filtered grid, the unfiltered grid for the Month Close counts, and the ten most recent active payments of the month (`internal/app/app.go:629-648`).

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. The actor opens `/grid`.
2. The system computes the grid: one row per head that is active with an active project, **or** has a budget row for the month, **or** has a non-recoverable payment in the month; the actual excludes voided payments and excludes any payment whose request treatment is `recoverable` (V2) (`internal/store/store.go:1384-1400`).
3. The system renders `div.gridhead`: eyebrow **Budget vs actuals**, `<h1>Variance grid</h1>`, sub **{month} · {N} heads · remaining = budget - actual**, a `countpill` reading **{N} projects**, and a **Locked** pill when the month is locked (`internal/app/templates.go:125-129`).
4. The system renders the compact metric strip: **Budget**, **Actual**, **Remaining** (colour-coded by sign) and **Used** (`internal/app/templates.go:130-135`).
5. The system renders the filter toolbar: **Month** (`type="month"`), **Status** (*All* / *Unbudgeted spend* / *Over budget* / *Under budget* / *On track* / *Not paid*), **Search** (placeholder *Search head or project...*), then **Apply**, **⤓ Export** (→ `/export.csv?month=&status=&q=`) and **+ Add payment** (→ `/payments/new?month=`) (`internal/app/templates.go:136-142`).
6. The system renders the legend: **Not paid {N}**, **Unbudgeted {N}**, **Under {N}**, **On track {N}**, **Over {N}**.
7. When the month is locked the system renders **Month {month} is locked by {actor}. Reason: {reason}** (`internal/app/templates.go:145`).
8. The desktop matrix `table.matrix.grid` inside `.d-only` has a sticky **Project** and **Head** column and the headers **Due**, **Budget**, **Actual**, **Remaining ₹**, **Remaining %**, **Used**, **Status**, **Action**. Each project is a collapsible group row with a `project-toggle` button and its own totals; each head row shows a due pill (**Settled** / **Overdue** / **Due today** / **Upcoming** / **No due day** / **Due {n}**), the money columns, a `.vbar` usage bar capped at 118%, a status pill, and an **Add** link to `/payments/new?month=&head_id=` — replaced by **Locked** when the month is locked and **Retired** when the head is inactive (`internal/app/templates.go:146`).
9. The footer row reads **Company total**.
10. Below, `section.split` shows **Recent Payments for {month}** — a table of **Date**, **Project / Head**, **Amount**, **Payee** — and, for a holder of `month:lock`, the **Month Close** panel (UC-A-31).

**Alternate flows**

- **8.a** At ≤860px the matrix is replaced by `div.acc.m-only`, a project accordion driven by the same `.Grid.Groups` data. Each `.acc-item` ships **`is-open`** — the contract `fervid-app.js` implements — with a head showing the project, **{N} heads · {P}% used** and the remaining amount, and a body of `.head-row` blocks each carrying **Budget**, **Actual**, **Left** and a usage bar (`internal/app/templates.go:148-180`, `internal/app/phase6_screens_test.go:50-64`).
- **5.a** The **Status** filter and the search are applied **after** the SQL, in Go, so the company total reflects only the filtered rows (`internal/store/store.go:1423-1428`, then the accumulation at `:1455-1460`).
- **2.a** No heads at all → the matrix shows **No heads found. Add projects and heads to begin.** and the accordion shows the same with `/heads` linked.

**Exception flows**

- **42.e1 (defect)** **The filter form posts to the wrong place.** `form.toolbar` is `method="get" action="/"` (`internal/app/templates.go:136`), and `/` is the dashboard (F1). Pressing **Apply** navigates to `/?month=…&status=…&q=…`, which renders the dashboard and ignores every parameter. **The grid's month, status and search filters are unreachable from the grid itself** — only by editing the URL to `/grid?month=…`.
- **42.e2 (permission hole)** Any signed-in user reaches this screen, including AC5 and AC2, and sees every project's budget and actuals. Recorded as expected behaviour at `tests/e2e/audit-smoke.spec.ts:27-29`. See UC-A-09, 09.e1.
- **42.e3** A `Grid` error → the whole screen becomes an error page (`internal/app/app.go:633-647`).
- **42.e4** The **⤓ Export** link is not gated on `grid:export`, so it 403s for almost everybody — see UC-A-46.
- **42.e5** The **+ Add payment** and per-row **Add** links are not gated on `payment:create` either, so they 403 for a reader without it.

**Business rules**

- **V2: a recoverable payment is a deposit, not an expense** — it never counts as budget actuals. The `COALESCE(pr.treatment,'') <> 'recoverable'` clause appears three times in one query, and the `COALESCE` is what keeps historical payments (no linked request) counting exactly as they always have (`internal/store/store.go:1384-1398`).
- Status is derived, never stored: `not-paid` when actual is 0, `unbudgeted` when budget is 0 and actual is not, then `over` / `on-track` / `under` (`internal/store/store.go:1722-1738`).
- The grid is the one table that cannot restack into cards — "a head means nothing without its project, its budget and its actual side by side" (`internal/app/templates.go:148-155`).
- `Report` and `ListMonthPlans` are both built on `Grid`, so the three screens can never disagree (`internal/store/store.go:1487-1509`, `:517-524`).

**Data touched** — reads `heads`, `projects`, `budgets`, `payments`, `payment_requests`, `month_locks`, `users`.

**Non-functional / UX notes** — `baseline.spec.ts` captures this route at `/grid`; for several phases its route list said `/`, so `grid-*.png` was a picture of the dashboard and the widest screen in the app had no visual record (PROGRESS.md → Where the Phase 6 plan was wrong). The accordion's open state ships in the markup as `.is-open`; built from `<details>/<summary>` every project rendered permanently collapsed. `/grid` is **not** in `shell.spec.ts`'s `ROUTES` list (`tests/e2e/shell.spec.ts:11-18`), so its chrome is swept only through `ux.spec.ts`'s nav discovery.

**Open questions**

- 42.e1 and 42.e2 are both live defects: the screen's own filters are broken, and the screen has no permission gate. They are independent fixes.
- The `usedPct` helper caps the bar at 118% (`internal/app/app.go:1643-1652`), so a head at 400% of budget looks identical to one at 118%. The numeric **Used** column tells the truth.

---

### UC-A-43 — Export the variance grid as CSV

| | |
|---|---|
| **Goal** | "Give me this month's variance grid as a spreadsheet." |
| **Primary actor** | AC1 — **the only seeded role that can** (see the business rules) |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | **⤓ Export** on the variance grid |
| **Route(s)** | `GET /export.csv?month=&status=&q=` |
| **Permission gate** | `grid:export` (`internal/app/app.go:394`) |
| **Coverage IDs** | **D3**, C2, V2 |
| **Priority** | high |

**Preconditions**

1. The actor holds `grid:export`.
2. `month` parses, or silently becomes the current month.

**Postconditions (success)**

1. A `text/csv` response with `Content-Disposition: attachment; filename="variance-{month}.csv"` (`internal/app/app.go:1460-1461`).
2. A header row **Project, Head, Budget, Actual, Variance, Variance %, Status** followed by one row per grid row, **after** the status and search filters have been applied (`internal/app/app.go:1451-1454`).
3. One `audit_log` row: `action='export'`, `entity_type='report'`, `summary='Exported grid {month}'` — written **before** the body is generated (`internal/app/app.go:1447-1448`).

**Postconditions (failure)** — the audit row may already exist even when the body fails; a CSV writer error answers **500**, **The export could not be generated.** (`internal/app/app.go:1456-1459`).

**Main success scenario**

1. The actor presses **⤓ Export** on `/grid`; the link carries the current `month`, `status` and `q`.
2. `exportGrid` recomputes the grid with the same three parameters.
3. The system writes the audit row, builds the CSV in memory, sets the headers, and writes the body.
4. The browser downloads `variance-2026-04.csv`.

**Alternate flows**

- **1.a** The URL is requested directly with no parameters → the current month, no status filter, no search.
- **1.b** `status` and `q` are honoured, so the export is exactly what the screen shows.

**Exception flows**

- **43.e1** No `grid:export` → 403 (UC-A-46).
- **43.e2** A `Grid` error → `respondStoreError`, i.e. an HTML error page in response to a CSV request; nothing distinguishes the two by content type (`internal/app/app.go:1442-1446`).
- **43.e3** A write failure after the headers are sent → logged at ERROR; the client sees a truncated file (`internal/app/app.go:1462-1464`).

**Business rules**

- Every export is audited (`internal/app/app.go:1447`), which is what D3 means by "by permission".
- Amounts are written through `money.FormatPaise`, so each cell reads `₹1,00,000.00` — including the `₹` and Indian digit grouping (`internal/app/app.go:1453`).
- The export inherits V2: recoverable payments are already excluded by `Grid`.
- **No seeded role except Admin holds `grid:export`.** Manager has `grid:view` and `report:view`; Accounts has `grid:view`, `report:view` and `report:export`; neither has `grid:export` (`internal/store/migrations.go:365-395`).

**Data touched** — reads the same tables as `Grid`; writes `audit_log`.

**Non-functional / UX notes** — the whole CSV is buffered in memory before the first byte is written, so a failure yields a clean 500 rather than a half-file (`internal/app/app.go:1449-1459`).

**Open questions**

- **The CSV is not machine-readable as numbers.** `₹1,00,000.00` contains commas, so the field is quoted and every consumer must strip `₹` and the grouping before arithmetic. A second numeric column, or raw paise, would make the export usable in a spreadsheet without cleaning.
- The audit row is written **before** the export succeeds, so a failed export still records as exported.

---

### UC-A-44 — Run the monthly, project and head reports

| | |
|---|---|
| **Goal** | "Compare budget and actual across a range of months, by month, by project or by head." |
| **Primary actor** | AC3, AC4, AC1 — any holder of `report:view` |
| **Supporting actors** | — |
| **Scope / level** | system · user-goal |
| **Trigger** | Sidebar **Reports**, the months screen's **Reports** action, or a month row's **Report** action |
| **Route(s)** | `GET /reports/monthly`, `GET /reports/projects`, `GET /reports/heads` — all one handler, with `?from=&to=&month=` |
| **Permission gate** | `report:view` (`internal/app/app.go:395-397`) |
| **Coverage IDs** | **V2**, C3, D3 |
| **Priority** | high |

**Preconditions**

1. The actor holds `report:view`.
2. `from` parses, or falls back to `month`, or to the current month; `to` falls back to `from` (`internal/app/app.go:1423-1424`).

**Postconditions (success)** — 200 HTML titled `Reports - Fervid Budget`; nothing written.

**Postconditions (failure)** — nothing written.

**Main success scenario**

1. The actor opens `/reports/monthly`.
2. The system resolves the range, swapping `from` and `to` when they are the wrong way round (`internal/app/app.go:1425-1427`), derives the mode from the URL path suffix, and calls `Report`.
3. `Report` runs the **whole grid query once per month** in the range, then projects it: one row per month in `monthly` mode, one row per project in `projects` mode, one row per head otherwise (`internal/store/store.go:1480-1511`).
4. In every mode except `monthly` the system drops rows where both budget and actual are zero (`internal/app/app.go:1434-1436`, `:1562-1570`).
5. The system renders the banner: eyebrow **Analysis**, `<h1>Reports</h1>`, sub **{from} to {to} · {mode} view**, and head actions — **Monthly plans** (gated on `budget:view`) and **Export CSV** (→ `/reports/ytd.csv?from=&to=`) (`internal/app/templates.go:3269`).
6. The system renders the tab strip **Monthly** / **Projects** / **Heads**, each carrying the current range (`internal/app/templates.go:3270`).
7. The system renders the range toolbar: **From**, **To**, **Run**, and a **Current month** link back to the same mode for the current month (`internal/app/templates.go:3271`).
8. The system renders the metric strip **Budget**, **Actual**, **Remaining**, **Used**, summarised across the returned rows (`internal/app/app.go:1605-1626`).
9. The system renders `table.t-cards` with **Period**, then **Project** in non-monthly modes, then **Head** in heads mode, then **Budget**, **Actual**, **Remaining**, **Used**.
10. With no rows the table shows **No report rows for this range. Set a budget to start comparing.** with `/budgets` linked (`internal/app/templates.go:3273`, `internal/app/phase6_screens_test.go:89-92`).

**Alternate flows**

- **7.a** The actor sets `from` later than `to` → the handler swaps them silently.
- **2.a** An unparseable `from` or `to` → the fallback chain applies with no message (`internal/app/app.go:1540-1548`).
- **6.a** Switching tabs preserves the range because each tab link carries `from` and `to`.

**Exception flows**

- **44.e1** No `report:view` → 403 (UC-A-09).
- **44.e2** A very wide range means one full grid query per month; the range is **unbounded** — nothing caps it (`internal/store/store.go:1487-1496`). A ten-year range is 120 grid queries in one request against a 2-minute write timeout.
- **44.e3** The **Export CSV** link is not gated on `report:export` — see UC-A-46.
- **44.e4** A `Report` error → the error page.

**Business rules**

- Every mode is computed from `Grid`, so the reports, the grid and the monthly-plan totals can never disagree (`internal/store/store.go:1493`).
- V2 therefore applies here too: recoverable payments are excluded from actuals.
- `monthly` mode keeps a month with no data so the period appears in the series; the other modes drop empty rows.
- `ReportSummary` recounts each row's status from budget and actual rather than trusting a stored status (`internal/app/app.go:1605-1626`).

**Data touched** — reads `heads`, `projects`, `budgets`, `payments`, `payment_requests`, `month_locks`.

**Non-functional / UX notes** — `.Mode` is printed raw in the sub-line, so it reads "monthly view", "projects view", "heads view" — lower case, unlike every other label on the screen. The table's empty-state `colspan` is 7 in every mode, though monthly mode has 5 columns.

**Open questions**

- 44.e2: an unbounded month range is the one place in this area where a user can make the server do arbitrary work. `monthRange` is the natural place to cap it.

---

### UC-A-45 — Export the year-to-date report as CSV

| | |
|---|---|
| **Goal** | "Give me the year's budget-versus-actual by head as a spreadsheet." |
| **Primary actor** | AC4, AC1 — any holder of `report:export` |
| **Supporting actors** | — |
| **Scope / level** | system · subfunction |
| **Trigger** | **Export CSV** on `/reports/*` |
| **Route(s)** | `GET /reports/ytd.csv?from=&to=&year=` |
| **Permission gate** | `report:export` (`internal/app/app.go:398`) |
| **Coverage IDs** | **D3**, C2, V2 |
| **Priority** | high |

**Preconditions**

1. The actor holds `report:export`.
2. `from` and `to` parse, or fall back to `{year}-01` and `{year}-12`, where `year` defaults to the current calendar year (`internal/app/app.go:1467-1470`).

**Postconditions (success)**

1. A `text/csv` response with `Content-Disposition: attachment; filename="report-{from}-to-{to}.csv"` (`internal/app/app.go:1492-1493`).
2. A header row **Period, Project, Head, Budget, Actual, Variance, Variance %** and one row per head per month in the range (`internal/app/app.go:1483-1486`).
3. One `audit_log` row: `action='export'`, `entity_type='report'`, `summary='Exported report {from} to {to}'` (`internal/app/app.go:1479-1480`).

**Postconditions (failure)** — a CSV writer error answers **500**, **The export could not be generated.**; the audit row may already exist.

**Main success scenario**

1. The actor presses **Export CSV** on `/reports/heads?from=2026-01&to=2026-06`.
2. `exportYTD` resolves the range, swapping the ends if reversed.
3. `Report(from, to, "heads")` runs one grid query per month and returns head-level rows.
4. The system audits, buffers the CSV, sets the headers, and writes it.

**Alternate flows**

- **1.a** No parameters at all → the whole current calendar year, `{year}-01` to `{year}-12`.
- **1.b** `?year=2025` with no `from`/`to` → `2025-01` to `2025-12`.

**Exception flows**

- **45.e1** No `report:export` → 403 (UC-A-46).
- **45.e2 (surprise)** **The export is always head-level, whatever tab you pressed it from.** `exportYTD` hard-codes mode `"heads"` (`internal/app/app.go:1474`), so exporting from the **Monthly** or **Projects** tab yields a file that does not match the screen. The link is identical on all three tabs (`internal/app/templates.go:3269`).
- **45.e3** `Report` errors → an HTML error page in answer to a CSV request.
- **45.e4** A twelve-month default range is twelve grid queries; a hand-typed decade is 120 (see UC-A-44, 44.e2).

**Business rules**

- Amounts use `money.FormatPaise`, so cells carry `₹` and Indian grouping (UC-A-43's open question applies here too).
- Every export is audited.
- The `Variance %` column comes from `money.Percent` via `Grid`, so it matches the screen exactly.

**Data touched** — reads the same tables as `Grid`; writes `audit_log`.

**Non-functional / UX notes** — buffered in memory before the first byte, as with the grid export.

**Open questions**

- 45.e2: either the link should carry the current mode, or the button should be labelled **Export heads CSV**. Today the file silently disagrees with the screen it came from.

---

### UC-A-46 — An export refuses a caller who lacks the export verb

| | |
|---|---|
| **Goal** | "Export must obey permission — and I should not be shown a button that cannot work." |
| **Primary actor** | AC3 (Manager: `grid:view` + `report:view`, no export verb) and AC4 (Accounts: `report:export` but **not** `grid:export`) |
| **Supporting actors** | AC1 |
| **Scope / level** | system · subfunction (negative) |
| **Trigger** | Pressing **⤓ Export** on the variance grid, or **Export CSV** on the reports screen, without the matching grant |
| **Route(s)** | `GET /export.csv` (`grid:export`); `GET /reports/ytd.csv` (`report:export`) |
| **Permission gate** | `grid:export` / `report:export` — the routes are gated, the **links are not** |
| **Coverage IDs** | **D3**, R6 |
| **Priority** | high |

**Preconditions**

1. The actor is signed in and holds `grid:view` and/or `report:view`, so the screen renders.
2. The actor lacks the export verb the route requires.

**Postconditions (success — i.e. the refusal)**

1. **403**, chrome-less error page, **You do not have permission to perform this action.**
2. **No audit row** — `exportGrid`/`exportYTD` write theirs inside the handler, which never runs (`internal/app/app.go:1447`, `:1479`).
3. Nothing is read from the domain tables.

**Postconditions (failure)** — n/a.

**Main success scenario (grid)**

1. A Manager opens `/grid` (which needs no grant at all — UC-A-42) and sees **⤓ Export** in the toolbar, ungated (`internal/app/templates.go:141`).
2. They press it; the browser navigates to `/export.csv?month=…`.
3. `RequirePermission("grid","export", …)` refuses, and the actor gets a full-page 403 **in place of the screen they were on**.

**Main success scenario (reports)**

1. A Manager opens `/reports/monthly` and sees **Export CSV** in the head actions, ungated (`internal/app/templates.go:3269`).
2. They press it and get the same 403.

**Alternate flows**

- **Accounts on the grid** — holds `report:export` but not `grid:export`, so **Export CSV** on the reports screen works and **⤓ Export** on the grid does not. Two buttons that look the same behave differently for the same person.
- **Admin** — holds both and neither refusal occurs.
- **A recoverables export** (`/recoverables/list.csv`, `recoverable_report:export`) has the same shape but is owned by UC-C.

**Exception flows**

- **46.e1** Because the response is a full HTML error page delivered to a link click, the actor loses the filter state they had built up on the grid or the reports screen. **Go back** restores it only with JavaScript enabled.
- **46.e2** A role-less user (AC5) can still reach `/grid` and therefore still sees the **⤓ Export** button, and gets the same 403.

**Business rules**

- **D3: exports are gated by permission.** Both routes are correctly gated; the failure is that neither affordance is (`internal/app/app.go:394`, `:398`).
- The immediately adjacent control **is** gated correctly on the same screen — **Monthly plans** on the reports banner checks `budget:view` (`internal/app/templates.go:3269`) — so the omission is inconsistency, not a missing convention.
- The product's own rule is stated in the vendor combobox: "an affordance that 403s is worse than no affordance" (`internal/app/templates.go:1359-1364`).

**Data touched** — reads the permission tables only.

**Non-functional / UX notes** — this is a family, not a one-off. The same "read route gated, write control ungated" pattern appears on `/roles` (**Save role**), `/users` (**Edit**), `/configuration` (**Save configuration**), `/projects` and `/heads` (add toolbars and per-row **Save**), `/budgets` (**Save Budgets**), `/backups` (**Create Backup**), and the grid's **+ Add payment** and per-row **Add**. Each is cited in its own use case.

**Open questions**

- Wrapping each control in `{{if .Perms.Can "grid" "export"}}` would fix the export case in two lines and is the pattern the same file already uses ten lines away. Whether the wider family should be fixed as one change is a product decision.
- **No seeded role except Admin can export the variance grid**, which makes 46's grid path the *normal* experience for Manager and Accounts rather than an edge case.

---

## 9. Traceability

| UC ID | Name | Coverage matrix IDs | Route(s) | Primary actor |
|---|---|---|---|---|
| UC-A-01 | Sign in | R6 (nearest) | `GET/POST /login` | any active user |
| UC-A-02 | Failed sign-in and lockout | C2 | `POST /login` | AC6 |
| UC-A-03 | Sign out | — | `POST /logout` | any signed-in user |
| UC-A-04 | Anonymous / expired session redirect | R6 | all gated routes | AC6 |
| UC-A-05 | CSRF-less POST refused | R6 | every `withCSRF` POST | AC6 |
| UC-A-06 | Unknown URL → error page | D5, X2, X3, X4 | `GET /` catch-all; unrouted POST | any signed-in user |
| UC-A-07 | Dashboard as home | D1, D2, D4 | `GET /{$}`, `GET /dashboard` | any signed-in user |
| UC-A-08 | Sidebar, tab bar, More sheet | D2 | every full render | any signed-in user |
| UC-A-09 | Admin route refused by URL | **R6**, D2 | 13 GET + 2 POST admin routes | AC2, AC5 |
| UC-A-10 | Roles matrix screen | R1, R2, R3, R7, R8 | `GET /roles` | AC1 |
| UC-A-11 | Create a role | R1, R9 | `POST /roles/new` | AC1 |
| UC-A-12 | Create a role by copy | **R5**, R1, R3 | `POST /roles/{id}/copy` | AC1 |
| UC-A-13 | Change grants and data scope | **R1**, **R2**, **R3**, R8 | `POST /roles` | AC1 |
| UC-A-14 | Delete a custom role | **R9** | `POST /roles/{id}/delete` | AC1 |
| UC-A-15 | System role protection | **R9**, R7 | `POST /roles/{id}/delete`, `POST /roles` | AC1 |
| UC-A-16 | Case-only duplicate role name | R1, R8 | `POST /roles/new`, `/roles/{id}/copy`, `/roles` | AC1 |
| UC-A-17 | Hand-crafted grant refused | **R2**, **R8**, R1 | `POST /roles` | AC1 |
| UC-A-18 | Users list | **R4**, R7 | `GET /users` | AC1 |
| UC-A-19 | Add a user | R4, R7 | `POST /users` | AC1 |
| UC-A-20 | Edit a user, assign several roles | **R4**, R3 | `POST /users` | AC1 |
| UC-A-21 | Set a default approver | R4; G9/D9 | `POST /users` | AC1 |
| UC-A-22 | Reset a password | — | `POST /users` | AC1 |
| UC-A-23 | Last-admin / self-edit guard | — | `POST /users` | AC1 |
| UC-A-24 | Read the Configuration screen | G21/D6, T10, V4 | `GET /configuration` | AC1 |
| UC-A-25 | Save app settings incl. Require attachments | **T10**, D6 | `POST /configuration` | AC1 |
| UC-A-26 | Reminder and ageing thresholds | N4, N5, S8 | `GET/POST /configuration` | AC1 |
| UC-A-27 | Recoverable categories | **V4**, V5, D6 | `POST /configuration/recoverable-categories` | AC1 |
| UC-A-28 | Project master | T12, C2 | `GET/POST /projects` | AC1 |
| UC-A-29 | Head master | **T12**, C2 | `GET/POST /heads` | AC1 |
| UC-A-30 | Open a monthly plan | C1, C2 | `GET/POST /months` | AC1 |
| UC-A-31 | Lock and unlock a month | C2 | `POST /months/{month}/lock`, `/unlock` | AC1 |
| UC-A-32 | Enter budgets | T12, C3, C2 | `GET/POST /budgets` | AC1 |
| UC-A-33 | Locked month refuses a budget change | T12, C2 | `POST /budgets` | AC1 |
| UC-A-34 | Inactive head: new vs history | **T12** | `POST /heads`; grid, budgets, payments | AC1, AC4 |
| UC-A-35 | Vendor list, filters, GSTIN gap | G5/D3, T3 | `GET /vendors` | AC1, AC4 |
| UC-A-36 | Vendor combobox search | **T3**, T6, T7, G5 | `GET /vendors/search` | AC2 |
| UC-A-37 | Add a vendor | G5/D3, T3 | `GET /vendors/new`, `POST /vendors` | AC1 |
| UC-A-38 | Open and edit a vendor | G5/D3 | `GET/POST /vendors/{id}` | AC1, AC4 |
| UC-A-39 | Vendor bank details withheld | **T3**, G5/D3, R8 | `GET /vendors`, `GET/POST /vendors/{id}`, `GET /vendors/search` | AC7 |
| UC-A-40 | Audit log | **C2** | `GET /audit` | AC1 |
| UC-A-41 | Backups | C2 | `GET/POST /backups` | AC1 |
| UC-A-42 | Variance grid | **V2**, C3, T12 | `GET /grid` | AC3, AC4, AC1 |
| UC-A-43 | Grid CSV export | **D3**, C2, V2 | `GET /export.csv` | AC1 |
| UC-A-44 | Monthly / project / head reports | **V2**, C3, D3 | `GET /reports/monthly`, `/projects`, `/heads` | AC3, AC4, AC1 |
| UC-A-45 | Year-to-date CSV export | **D3**, C2, V2 | `GET /reports/ytd.csv` | AC4, AC1 |
| UC-A-46 | Export refused without the export verb | **D3**, R6 | `GET /export.csv`, `GET /reports/ytd.csv` | AC3, AC4 |

### Coverage matrix IDs touched by UC-A

**R1–R9** (all nine) · **T3, T6, T7, T10, T12** · **Q5** (via the scope model in UC-A-13) · **S8** (threshold data only) · **V2, V4, V5** · **N4, N5** (threshold data only) · **D1–D5** · **X1** (no tax computation, UC-A-37) · **X2, X3, X4** (the 404/405 contract they rely on, UC-A-06) · **C1, C2, C3**.

Not touched by UC-A, by design: A1–A8, L1–L11, Q1–Q4, Q6, S1–S7, S9–S15, V1, V3, V6–V8, N1–N3, N6–N8, X5, X6, C4 — these belong to UC-B and UC-C.



