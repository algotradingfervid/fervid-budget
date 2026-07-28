import { defineConfig, devices } from '@playwright/test';

// Standalone config for the synthetic data-entry run.
//
// Deliberately separate from playwright.config.ts and playwright.audit.config.ts:
// those two spin up their own throwaway server and database. This one drives the
// DEVELOPER'S OWN running app on :8080 against data/fervid.db, because the whole
// point is to populate the instance a human is browsing. There is no webServer
// block for the same reason — if the app is not already up, the run should fail
// loudly rather than quietly start a second one against a different database.

const port = Number(process.env.FERVID_DATASET_PORT ?? 8080);
const workers = Number(process.env.FERVID_DATASET_WORKERS ?? 6);

export default defineConfig({
  testDir: '.',
  outputDir: '../../output/dataset/test-results',
  fullyParallel: true,
  workers,
  // Retries would replay a half-finished lifecycle onto a request that already
  // moved, which corrupts the dataset rather than repairing it.
  retries: 0,
  reporter: [['list']],
  // Generous: a shard walks hundreds of pages in one test body.
  timeout: 30 * 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL: `http://localhost:${port}`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off',
    actionTimeout: 15_000,
    navigationTimeout: 30_000,
    ...devices['Desktop Chrome'],
  },
});
