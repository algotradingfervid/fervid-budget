import path from 'node:path';
import { expect, test } from './fixtures';

/**
 * Visual baseline capture.
 *
 * Screenshots every existing route at desktop and phone widths so the design
 * system port can be diffed against the pre-redesign rendering.
 *
 * Output: output/playwright/baseline/<route>-<desktop|mobile>.png
 */

const OUT_DIR = path.join('output', 'playwright', 'baseline');

const ROUTES: Array<{ slug: string; path: string }> = [
  { slug: 'grid', path: '/' },
  { slug: 'payments', path: '/payments' },
  { slug: 'payments-new', path: '/payments/new' },
  { slug: 'budgets', path: '/budgets' },
  { slug: 'months', path: '/months' },
  { slug: 'reports-monthly', path: '/reports/monthly' },
  { slug: 'projects', path: '/projects' },
  { slug: 'heads', path: '/heads' },
  { slug: 'users', path: '/users' },
  { slug: 'audit', path: '/audit' },
  { slug: 'backups', path: '/backups' }
];

const VIEWPORTS = [
  { name: 'desktop', width: 1440, height: 1200 },
  { name: 'mobile', width: 390, height: 850 }
] as const;

test.describe('visual baseline', () => {
  // One capture pass only. The mobile-chrome project is an emulated device and
  // forbids setViewportSize, and it would rewrite the same 22 files anyway.
  test.beforeEach(() => {
    test.skip(test.info().project.name !== 'chromium', 'baseline captures once');
  });

  for (const viewport of VIEWPORTS) {
    for (const route of ROUTES) {
      test(`${route.slug} @ ${viewport.name}`, async ({ adminPage }) => {
        await adminPage.setViewportSize({ width: viewport.width, height: viewport.height });
        const response = await adminPage.goto(route.path, { waitUntil: 'domcontentloaded' });
        expect(response?.status(), `${route.path} should render`).toBeLessThan(400);
        await adminPage.waitForLoadState('networkidle').catch(() => undefined);
        await adminPage.screenshot({
          path: path.join(OUT_DIR, `${route.slug}-${viewport.name}.png`),
          fullPage: true
        });
      });
    }
  }
});
