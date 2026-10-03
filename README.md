# Fervid Budget

Fervid Budget is a small web application a company uses to plan its monthly
spending and to control how that money is paid out.

- **Budgets.** An administrator sets a budget for every expense head (a line
  such as "Travel" inside a project) for each month, and compares it with what
  was actually paid on the variance grid and in the reports.
- **Payment requests.** Nobody pays money without a request. A person raises a
  request, their approver approves, returns or rejects it, and the Accounts team
  pays it and records the payment. Every step is kept on the request's history
  and in the audit log.
- **Recoverables.** Deposits, guarantees and advances that are expected to come
  back are tracked until they are recovered.

It is one Go program with a SQLite database. Pages are rendered on the server
(Go `html/template`) with [htmx](https://htmx.org) for partial updates.

## Who uses it

Access comes from roles. Four roles are built in, and an administrator can copy
and edit them or create new ones.

| Role | Typically does |
|---|---|
| Requester | Raises payment requests, answers questions, asks to cancel. Sees only their own requests. |
| Manager | Approves, returns, rejects and reassigns requests routed to them; decides cancellations and short payments. |
| Accounts | Takes approved requests from the payment queue, pays them, records the payment, holds requests that need more information. |
| Admin | Everything above, plus projects, heads, budgets, month locks, vendors, users, roles, configuration, notifications, audit log and backups. |

The user manual in [`docs/manual/`](docs/manual/index.html) explains every screen
for each of these roles.

## Quick start

Prerequisites:

- **Go 1.27.1** (the version in `go.mod`).
- **Node.js and npm**, only for the browser tests and the manual build.
- `make` (optional; every target is a one-line command you can run by hand).

Run everything **from the repository root**. The server serves `/static/` from
the relative folder `web/static`, so started anywhere else the pages load
without their styles and scripts.

```sh
# First start on an empty database: create the admin account and sample
# projects, heads and budgets, then keep serving.
FERVID_ADMIN_PASSWORD='choose-a-password-1' \
FERVID_SECURE_COOKIES=false \
make seed
```

Open <http://127.0.0.1:8080> and sign in as `admin@fervid.local` (the default
`FERVID_ADMIN_EMAIL`) with the password you chose. The password must be 12 to
72 bytes and contain a letter and a digit.

`FERVID_SECURE_COOKIES=false` is needed only because a local server speaks plain
HTTP; the default (`true`) marks the session cookie HTTPS-only. Never turn it
off on a real deployment.

Later starts: `FERVID_SECURE_COOKIES=false make run`. Data lives in `data/`
(`data/fervid.db`, `data/attachments/`, `data/backups/`), which git ignores.

### Settings

All settings are `FERVID_*` environment variables with safe defaults; they are
read in `internal/config/config.go`. [`.env.example`](.env.example) lists the
ones you are most likely to set. The server does not read a `.env` file itself:
export the variables, or let your service manager provide them. The ones that
matter first:

| Variable | Default | Meaning |
|---|---|---|
| `FERVID_ADDR` | `127.0.0.1:8080` | Address to listen on |
| `FERVID_DB` | `data/fervid.db` | SQLite database file |
| `FERVID_ADMIN_PASSWORD` | none | Password for the first admin; needed only when the database has no users |
| `FERVID_SESSION_KEY` | random per start | Signs session cookies; set a fixed secret or everybody is signed out on restart |
| `FERVID_SECURE_COOKIES` | `true` | HTTPS-only cookies |

Operations (backups, restore, retention, rate limits) are in
[`docs/OPERATIONS.md`](docs/OPERATIONS.md); production settings and hardening in
[`docs/SECURITY-DEPLOYMENT.md`](docs/SECURITY-DEPLOYMENT.md).

## Make targets

`make help` lists them all.

| Target | Does |
|---|---|
| `make run` | Start the server (`go run ./cmd/server`) |
| `make seed` | Create the admin and sample data if the database is empty, then serve |
| `make backup` | Make one backup of the database and attachments, then exit |
| `make fmt` / `make vet` | Format / statically check the Go code |
| `make test` | Go tests |
| `make test-race` / `make test-cover` | Go tests with the race detector / with coverage |
| `make typecheck` | Type-check the Playwright tests (`tsc --noEmit`) |
| `make test-e2e` | The shipped browser test suite |
| `make test-audit` | The QA audit browser suite |
| `make test-all` | vet, race tests, coverage, typecheck and the shipped browser suite |
| `make manual` | Re-photograph every screen and rebuild `docs/manual` (needs your own server on :8080) |

## Tests

```sh
make test             # Go unit and integration tests
npm install           # once, before any browser test
npx playwright install chromium
make test-e2e         # shipped browser suite
make test-audit       # QA audit suite
```

- **Go tests** live next to the code (`*_test.go`). They create temporary
  SQLite databases and in-process servers, so nothing needs to be running.
- **Shipped browser suite** (`playwright.config.ts`, specs in `tests/e2e/`
  except `audit-*`). Playwright starts its own freshly seeded server on port
  4173 with a throwaway database under `output/playwright/runtime/`.
- **Audit suite** (`playwright.audit.config.ts`, the `tests/e2e/audit-*.spec.ts`
  files, run by `scripts/run-audit.sh`). It builds a large world (custom roles,
  dozens of users, hundreds of requests), so each area gets its own server and
  database on ports 4301 and up. See [`docs/qa/README.md`](docs/qa/README.md).

**The two browser suites must never share a database or a server.** The audit
data breaks the shipped suite's assumptions about a freshly seeded database.
Both configs take `FERVID_E2E_PORT` so parallel runs can use different ports.

## Project layout

| Path | What is there |
|---|---|
| `cmd/server/` | The program: flags `--seed`, `--backup`, `--restore <dir>`; starts the web server, the hourly reminder job and the daily backup |
| `internal/app/` | Web layer: route table (`routes.go`), middleware, one handler file per screen, page templates in `templates/*.html` |
| `internal/store/` | All database access and business rules: schema, migrations, request workflow, payments, permissions, audit, backups |
| `internal/auth/` | Session cookie, CSRF tokens, sign-in and permission checks for routes |
| `internal/notify/` | In-app notifications, email, and the reminder job |
| `internal/config/` | `FERVID_*` settings |
| `internal/money/` | Rupee amounts as whole paise; Indian number formatting |
| `internal/password/` | The password policy |
| `web/static/` | CSS, JavaScript, htmx and the logo, served under `/static/` |
| `tests/e2e/` | Playwright browser tests (shipped suite and audit suite) |
| `tests/manual/` | Sources and capture scripts for the user manual |
| `tests/dataset/` | Playwright scripts that build a large demonstration dataset |
| `scripts/` | Audit-suite runner and server operations helpers |
| `tools/` | Production-rehearsal checks and QA/video tooling (not part of the app) |
| `docs/` | Architecture, operations, security, QA material and the built user manual |

## Documentation

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md): how the code is organised, the
  request lifecycle, permissions, data model and conventions. Start here to
  change the code.
- [`docs/OPERATIONS.md`](docs/OPERATIONS.md): data locations, backups, restore,
  retention, sign-in rate limits.
- [`docs/SECURITY-DEPLOYMENT.md`](docs/SECURITY-DEPLOYMENT.md): production
  configuration and security behaviour.
- [`docs/manual/`](docs/manual/index.html): the user manual (open `index.html` in
  a browser). Sources and rules for editing it are in
  [`tests/manual/build/AUTHORING.md`](tests/manual/build/AUTHORING.md).
- [`docs/qa/`](docs/qa/README.md): QA test cases, use cases, UML diagrams and
  audit results.
