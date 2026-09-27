# Budget creation verification

The production planner lives at `/budgets/new` and `/budgets/plan?month=YYYY-MM`.
The approved HTML prototype is a separate artifact; testing the prototype does
not establish production persistence.

The 26 September 2026 acceptance run uses an isolated application on
`http://127.0.0.1:8930` and SQLite database at
`output/playwright/budget-workflow-2026-09-26/runtime/demo.db`.
Application code is shared with the normal server. No live database is needed.

## Evidence

Open `output/playwright/budget-workflow-2026-09-26/verification-report.html` for
the combined report. `independent-qa/report.md` records 29 frontend scenarios,
screenshots and read-only database comparisons. The verifier entered budgets,
requests, approvals and payments through the browser without reading product
implementation. `regression/README.md` explains automated results and existing
duplicate mobile-project skips.

Synthetic fixtures cover July–October 2026. July remains locked. September has
two projects, three heads and six lines. Request 4 intentionally has no payment
after a future-date validation check. These records are test fixtures, not
business transactions.

## Browser helpers

- `browser.py`: root Playwright CLI session `budget-workflow`.
- `qa-ui.py`: independent verification session `budget-qa`.
- `check-mockup.py`: prototype-only checks.

The helpers use the locally installed Playwright CLI under
`tools/workflow-videos/node_modules/.bin/playwright-cli`. Open their session
before running actions. Use fresh snapshots and observed accessible names;
database access for acceptance comparisons must remain read-only.

## Automated checks

```sh
go test ./...
go test -race -timeout 20m ./...
npm run typecheck
node --check web/static/budget-planner.js
FERVID_E2E_PORT=4370 npx playwright test
```

The standard Playwright runner creates its own isolated database. Choose an
unused port. The Go tests create independent temporary databases and cover
atomic writes, audit failure, concurrent editors, locks, permissions, legacy
copy compatibility and financial reporting.

Migration 20 adds `budget_lines`; existing head amounts remain valid and appear
as a single Monthly allocation line until saved through the planner. Reports
and payments still use the head-level budget aggregate. Each detailed head's
aggregate must equal its line sum, checked in `final-database-integrity.json`.
