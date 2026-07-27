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
      name: 'mobile-chrome',
      testIgnore: /regression-issues\.spec\.ts/,
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
      'FERVID_ADMIN_PASSWORD=admin123',
      'go run ./cmd/server --seed'
    ].join(' '),
    url: `http://127.0.0.1:${port}/login`,
    reuseExistingServer: !process.env.CI,
    timeout: 45_000,
    stdout: 'pipe',
    stderr: 'pipe'
  }
});
