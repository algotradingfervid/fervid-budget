/**
 * Proves `audit-support.ts` works before the audit suite is built on it.
 *
 * Every assertion here is about the harness, not the product: an exact role set
 * really is exact, a probe really does report a refusal rather than following it,
 * and a forged CSRF token really is rejected.
 */
import { test, expect } from './fixtures';
import { asRole, probeGet, probePost, probeAnonymous, csrfToken } from './audit-support';

// Runs on both device projects. There is nothing device-dependent here, but the
// guard that would have skipped the mobile copy cannot be written on
// `browserName`: the Pixel 5 project drives chromium too, so `browserName` is
// 'chromium' in both and the skip never fires. The suite's convention for a
// genuinely single-run file is `project.name !== 'chromium'`. These four are
// cheap and pass on both, so they simply run twice.
test.describe('audit harness', () => {
  test('a subject given exactly [Requester] holds no Accounts grant', async ({ adminPage, browser, runId }) => {
    const requester = await asRole(adminPage, browser, runId, ['Requester']);
    try {
      // Requester holds request{view own,create,…} and attachment{view,create}
      // and nothing else — no payment verbs, no grid, no reports.
      const own = await probeGet(requester.page, '/requests');
      expect(own.status, 'Requester may list requests').toBe(200);

      const queue = await probeGet(requester.page, '/accounts-queue');
      expect(queue.status, 'accounts-queue needs payment:process, which Requester lacks').toBe(403);

      const payments = await probeGet(requester.page, '/payments');
      expect(payments.status, 'payments needs payment:view').toBe(403);

      const grid = await probeGet(requester.page, '/grid');
      expect(grid.status, 'grid is RequireLogin only, so a Requester reaches it').toBe(200);

      const users = await probeGet(requester.page, '/users');
      expect(users.status, 'admin screens need user:view').toBe(403);
    } finally {
      await requester.close();
    }
  });

  test('a subject given no role at all is signed in and permitted almost nothing', async ({
    adminPage,
    browser,
    runId
  }) => {
    const nobody = await asRole(adminPage, browser, runId, []);
    try {
      const home = await probeGet(nobody.page, '/');
      expect(home.status, 'the dashboard is RequireLogin, so a role-less user still sees it').toBe(200);

      for (const path of ['/requests', '/payments', '/users', '/roles', '/configuration', '/audit']) {
        const probe = await probeGet(nobody.page, path);
        expect(probe.status, `${path} must refuse a user with no grants`).toBe(403);
      }
    } finally {
      await nobody.close();
    }
  });

  // 303 See Other, not 302 — the app answers a GET-after-gate with See Other, so
  // a matrix row that pins 302 fails for a reason that has nothing to do with
  // authorisation. Assert the class of redirect, and the target.
  test('an anonymous caller is redirected to /login rather than answered', async ({ browser, baseURL }) => {
    const probe = await probeAnonymous(browser, '/requests', baseURL!);
    expect(probe.status, 'gated routes redirect an anonymous caller').toBeGreaterThanOrEqual(300);
    expect(probe.status, 'with a 3xx, not an answer').toBeLessThan(400);
    expect(probe.location, 'and the redirect target is the login screen').toContain('/login');
  });

  test('withCSRF refuses a POST with a missing or forged token', async ({ adminPage }) => {
    const token = await csrfToken(adminPage.context());
    expect(token, 'the csrf cookie is readable, which is what makes forgery testable').not.toBe('');

    const omitted = await probePost(adminPage, '/logout', {}, { csrf: 'omit' });
    expect(omitted.status, 'a POST with no csrf field must be refused').toBe(403);

    const forged = await probePost(adminPage, '/logout', {}, { csrf: 'bogus' });
    expect(forged.status, 'a POST with a wrong csrf field must be refused').toBe(403);
  });
});
