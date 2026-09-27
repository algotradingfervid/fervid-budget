import { defineConfig, devices } from '@playwright/test';

// One runner per port. Several agents audit different domains at the same time
// and each needs its own server and its own database, so the port, the runtime
// directory and every output path are keyed off FERVID_E2E_PORT. Unset, this is
// byte-for-byte the original single-runner configuration on 4173.
const port = Number(process.env.FERVID_E2E_PORT ?? 4173);
const slot = port === 4173 ? '' : `-${port}`;
const runtime = `output/playwright/runtime/run-${process.pid}`;

export default defineConfig({
  testDir: './tests/e2e',
  // The QA audit suite is deliberately NOT part of this run — see
  // playwright.audit.config.ts and `make test-audit`.
  //
  // It cannot share a database with these specs. The audit builds a world in
  // order to interrogate it: custom roles, dozens of users, and (for the
  // pagination case) 214 requests. The shipped specs assume something close to a
  // freshly seeded database, so running both against one server breaks them for
  // reasons that have nothing to do with the code under test. The first symptom
  // is ux.spec.ts reporting "/roles: scrolls sideways by 806px", because the
  // roles matrix grows a column per role.
  //
  // Each audit area therefore gets its own server and its own database, which is
  // also the only way its numbers were ever verified.
  testIgnore: /audit-.*\.spec\.ts/,
  outputDir: `output/playwright/test-results${slot}`,
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
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
    {
      // A project-level testIgnore REPLACES the top-level one rather than adding
      // to it, so the audit pattern has to be repeated here or this project
      // collects the audit specs the top level just excluded.
      name: 'mobile-chrome',
      testIgnore: [/regression-issues\.spec\.ts/, /audit-.*\.spec\.ts/],
      use: { ...devices['Pixel 5'] }
    }
  ],
  webServer: {
    command: [
      `mkdir -p ${runtime}/attachments ${runtime}/backups`,
      '&& env',
      `FERVID_ADDR=127.0.0.1:${port}`,
      `FERVID_DB=${runtime}/fervid-e2e.db`,
      `FERVID_ATTACHMENT_DIR=${runtime}/attachments`,
      `FERVID_BACKUP_DIR=${runtime}/backups`,
      'FERVID_SESSION_KEY=fervid-playwright-session-key',
      'FERVID_ADMIN_EMAIL=admin@fervid.local',
      'FERVID_ADMIN_PASSWORD=TestAdmin12345',
      'FERVID_SECURE_COOKIES=false',
      'go run ./cmd/server --seed'
    ].join(' '),
    url: `http://127.0.0.1:${port}/login`,
    reuseExistingServer: !process.env.CI,
    timeout: 45_000,
    stdout: 'pipe',
    stderr: 'pipe'
  }
});
