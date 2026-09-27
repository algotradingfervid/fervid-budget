import { defineConfig, devices } from '@playwright/test';

/**
 * The QA audit suite — `tests/e2e/audit-*.spec.ts`.
 *
 * Separate from `playwright.config.ts` for one reason: **each audit area needs a
 * database of its own.** The audit builds a world in order to interrogate it —
 * custom roles, dozens of single-role users, and 214 requests for the pagination
 * case — so two areas sharing one server contaminate each other's counts, and
 * sharing with the shipped specs breaks them outright. Every number in
 * `docs/qa/` was measured one area per server, and this config is how that is
 * reproduced.
 *
 * Run one area:
 *   FERVID_E2E_PORT=4301 npx playwright test -c playwright.audit.config.ts \
 *     tests/e2e/audit-a-rbac-permissions.spec.ts
 *
 * Run every area, each isolated:
 *   make test-audit          # scripts/run-audit.sh
 *
 * Do NOT run every audit spec through one invocation of this config: they would
 * share the one webServer and the one database, which is the situation it exists
 * to prevent. `scripts/run-audit.sh` gives each spec its own port and DB.
 */
const port = Number(process.env.FERVID_E2E_PORT ?? 4301);
const runtime = `output/playwright/runtime/audit-${process.pid}`;
const slot = `-audit-${port}`;

export default defineConfig({
  testDir: './tests/e2e',
  testMatch: /audit-.*\.spec\.ts/,
  outputDir: `output/playwright/test-results${slot}`,
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  // No retries even in CI. A flaky audit result is a finding about the audit,
  // not something to paper over — and a `test.fail()` case that passes on retry
  // would report the opposite of the truth.
  retries: 0,
  reporter: [['list'], ['html', { outputFolder: `output/playwright/report${slot}`, open: 'never' }]],
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    actionTimeout: 10_000,
    navigationTimeout: 20_000
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'mobile-chrome', use: { ...devices['Pixel 5'] } }
  ],
  webServer: {
    command: [
      `mkdir -p ${runtime}/attachments ${runtime}/backups`,
      '&& env',
      `FERVID_ADDR=127.0.0.1:${port}`,
      `FERVID_DB=${runtime}/fervid-audit.db`,
      `FERVID_ATTACHMENT_DIR=${runtime}/attachments`,
      `FERVID_BACKUP_DIR=${runtime}/backups`,
      'FERVID_SESSION_KEY=fervid-playwright-session-key',
      'FERVID_ADMIN_EMAIL=admin@fervid.local',
      'FERVID_ADMIN_PASSWORD=TestAdmin12345',
      'FERVID_SECURE_COOKIES=false',
      'go run ./cmd/server --seed'
    ].join(' '),
    url: `http://127.0.0.1:${port}/login`,
    // Never reuse: a reused server means a reused database, and an audit area
    // that inherits another's rows measures the wrong thing.
    reuseExistingServer: false,
    timeout: 45_000,
    stdout: 'pipe',
    stderr: 'pipe'
  }
});
