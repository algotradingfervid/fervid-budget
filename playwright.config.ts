import { defineConfig, devices } from '@playwright/test';

const port = 4173;
const runtime = `output/playwright/runtime/run-${process.pid}`;

export default defineConfig({
  testDir: './tests/e2e',
  outputDir: 'output/playwright/test-results',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: [['list'], ['html', { outputFolder: 'output/playwright/report', open: 'never' }]],
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
