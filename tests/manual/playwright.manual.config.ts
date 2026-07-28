import { defineConfig, devices } from '@playwright/test';

// Config for the user-manual capture run.
//
// Deliberately points at the DEVELOPER'S running app on :8080 against
// data/fervid.db, not at the throwaway server playwright.config.ts starts on
// 4173. That server is freshly seeded — 3 projects, 9 heads, no requests — and a
// manual illustrated with empty queues would be worthless. The dataset on :8080
// has 582 requests covering every status, which is the whole point.
//
// No webServer block, for the same reason tests/dataset uses none: if the app is
// not already up the run must fail loudly rather than quietly start a second one
// against a different database.

const port = Number(process.env.FERVID_MANUAL_PORT ?? 8080);
const workers = Number(process.env.FERVID_MANUAL_WORKERS ?? 4);

export default defineConfig({
  testDir: '.',
  outputDir: '../../output/manual/test-results',
  fullyParallel: true,
  workers,
  retries: 0,
  reporter: [['list']],
  timeout: 5 * 60_000,
  expect: { timeout: 15_000 },
  use: {
    // The device spread goes FIRST: it carries its own viewport (1280×720), so
    // spreading it after would silently undo the width the manual is shot at.
    ...devices['Desktop Chrome'],
    baseURL: `http://localhost:${port}`,
    // Full-page captures at the desktop width the app's own baseline spec uses.
    viewport: { width: 1440, height: 1200 },
    deviceScaleFactor: 1,
    trace: 'off',
    screenshot: 'off',
    video: 'off',
    actionTimeout: 15_000,
    navigationTimeout: 30_000,
  },
});
