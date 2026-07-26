import { expect, test } from './fixtures';

/**
 * Phase 0 Task 20 acceptance criteria, as a test rather than a one-off review.
 *
 * Every screen, on both devices: no horizontal overflow, a visible heading,
 * and exactly one navigation chrome. Screenshot diffing catches what a page
 * looks like; this catches what it *does*, and it keeps catching it.
 */

const ROUTES = [
  '/', '/dashboard', '/requests', '/requests/new', '/approvals', '/accounts-queue',
  '/payments', '/payments/new',
  '/recoverables', '/recoverables/list',
  '/budgets', '/months', '/reports/monthly', '/projects', '/heads', '/vendors', '/vendors/new',
  '/users', '/roles', '/configuration', '/audit', '/backups',
];

test.describe('app shell', () => {
  for (const route of ROUTES) {
    test(`${route} fits its viewport and shows one navigation chrome`, async ({ adminPage }) => {
      await adminPage.goto(route);
      const mobile = adminPage.viewportSize()!.width <= 860;

      const state = await adminPage.evaluate(() => {
        const vis = (selector: string) => {
          const el = document.querySelector(selector);
          if (!el) return false;
          const style = getComputedStyle(el);
          if (style.display === 'none' || style.visibility === 'hidden') return false;
          const box = el.getBoundingClientRect();
          return box.width > 0 && box.height > 0;
        };
        return {
          path: location.pathname,
          overflow: document.documentElement.scrollWidth - window.innerWidth,
          sidebar: vis('.sidebar'),
          topbar: vis('.m-topbar'),
          tabbar: vis('.tabbar'),
          heading: vis('h1'),
        };
      });

      // A redirect to /login means the fixture lost its session; asserting on
      // the login page would pass vacuously for every route.
      expect(state.path, 'route should not redirect').toBe(route);
      expect(state.overflow, 'horizontal overflow in px').toBeLessThanOrEqual(0);
      expect(state.heading, 'page has a visible h1').toBe(true);

      // Sidebar and tab bar are alternatives. Both visible is two "Primary"
      // landmarks and half a wasted viewport; neither is a page with no nav.
      expect(state.sidebar, 'sidebar visible').toBe(!mobile);
      expect(state.tabbar, 'tab bar visible').toBe(mobile);
      expect(state.topbar, 'mobile top bar visible').toBe(mobile);
    });
  }
});
