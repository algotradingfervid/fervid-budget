import AxeBuilder from '@axe-core/playwright';
import { expect, test } from './fixtures';
import type { Page } from '@playwright/test';

/**
 * UI/UX quality net.
 *
 * shell.spec.ts checks structure on a fixed route list. This checks quality,
 * and discovers its routes from the navigation itself — so every screen a
 * later phase adds is covered the moment it appears in the nav, with no list
 * to remember to update. A nav link that 404s fails here too, which is the
 * point: a link the user can see is a promise.
 */

const MIN_TAP_PX = 40; // WCAG 2.5.8 target size (minimum) is 24px; 40 is the design system's own floor.

async function navRoutes(page: Page): Promise<string[]> {
  const mobile = page.viewportSize()!.width <= 860;
  await page.goto('/');
  if (mobile) await page.locator('.tabbar .js-more').click();
  const selector = mobile ? '.more-sheet .ms-list a[href^="/"]' : '.side-nav a[href^="/"]';
  const hrefs = await page.locator(selector).evaluateAll(nodes =>
    nodes.map(n => (n as HTMLAnchorElement).getAttribute('href') || '').filter(Boolean));
  return [...new Set(hrefs)];
}

test.describe('UI/UX quality', () => {
  test('every navigable screen is reachable, fits, and is accessible', async ({ adminPage }) => {
    const mobile = adminPage.viewportSize()!.width <= 860;
    const routes = await navRoutes(adminPage);
    expect(routes.length, 'nav should expose routes').toBeGreaterThan(5);

    const problems: string[] = [];

    for (const route of routes) {
      const response = await adminPage.goto(route);
      const status = response?.status() ?? 0;
      if (status >= 400) {
        problems.push(`${route}: nav link returns HTTP ${status}`);
        continue;
      }

      const state = await adminPage.evaluate(() => {
        const box = (s: string) => {
          const el = document.querySelector(s);
          if (!el) return null;
          const style = getComputedStyle(el);
          if (style.display === 'none' || style.visibility === 'hidden') return null;
          const r = el.getBoundingClientRect();
          return r.width > 0 && r.height > 0 ? r : null;
        };
        return {
          path: location.pathname,
          overflow: document.documentElement.scrollWidth - window.innerWidth,
          heading: !!box('h1'),
          tabbar: box('.tabbar'),
          sidebar: !!box('.sidebar'),
        };
      });

      if (state.overflow > 0) problems.push(`${route}: scrolls sideways by ${state.overflow}px`);
      if (!state.heading) problems.push(`${route}: no visible h1`);
      if (mobile && state.sidebar) problems.push(`${route}: sidebar visible on a phone`);
      if (mobile && !state.tabbar) problems.push(`${route}: tab bar missing on a phone`);
      if (!mobile && !state.sidebar) problems.push(`${route}: sidebar missing on desktop`);

      // Nothing may be trapped under the fixed tab bar. Mid-scroll the bar
      // covers content by design -- that is what scrolling is for -- so the
      // real contract is at the *end* of the document, where .page's bottom
      // padding has to clear the bar. Scroll there and check.
      if (mobile && state.tabbar) {
        await adminPage.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
        const trapped = await adminPage.evaluate(() => {
          const bar = document.querySelector('.tabbar')?.getBoundingClientRect();
          if (!bar) return [];
          const hits: string[] = [];
          for (const el of document.querySelectorAll('main.page a, main.page button, main.page input')) {
            const r = el.getBoundingClientRect();
            if (r.width === 0 || r.height === 0) continue;
            // Must actually be on screen. An element scrolled out of a wide
            // table's horizontal overflow is not covered by the tab bar; it is
            // simply elsewhere, and flagging it says nothing about the bar.
            if (r.right <= 0 || r.left >= window.innerWidth) continue;
            // Overlap by more than a pixel: content that ends exactly at the
            // bar's top edge is correctly cleared, not trapped.
            if (r.bottom > bar.top + 1 && r.top < window.innerHeight) {
              hits.push((el.textContent || el.className).trim().slice(0, 30) || el.tagName);
            }
          }
          return hits;
        });
        if (trapped.length) problems.push(`${route}: tab bar traps ${trapped.join(', ')} at page end`);
        await adminPage.evaluate(() => window.scrollTo(0, 0));
      }

      // Tap targets. Small controls are the single most common phone defect.
      if (mobile) {
        const small = await adminPage.evaluate((min: number) => {
          const bad: string[] = [];
          for (const el of document.querySelectorAll('main.page button, main.page .btn, .tabbar a')) {
            const r = el.getBoundingClientRect();
            if (r.width === 0 || r.height === 0) continue;
            if (r.height < min) bad.push(`${(el.textContent || '').trim().slice(0, 20)}@${Math.round(r.height)}px`);
          }
          return bad;
        }, MIN_TAP_PX);
        if (small.length) problems.push(`${route}: tap targets under ${MIN_TAP_PX}px — ${small.join(', ')}`);
      }

      const axe = await new AxeBuilder({ page: adminPage })
        .withTags(['wcag2a', 'wcag2aa'])
        .analyze();
      for (const v of axe.violations) {
        problems.push(`${route}: ${v.id} (${v.nodes.length}x) — ${v.help}`);
      }
    }

    expect(problems, `checked ${routes.length} screens:\n${problems.join('\n')}`).toEqual([]);
  });

  test('keyboard focus is always visible', async ({ adminPage }) => {
    await adminPage.goto('/');
    const invisible: string[] = [];
    for (let i = 0; i < 12; i++) {
      await adminPage.keyboard.press('Tab');
      const info = await adminPage.evaluate(() => {
        const el = document.activeElement as HTMLElement | null;
        if (!el || el === document.body) return null;
        const s = getComputedStyle(el);
        const ring = s.outlineStyle !== 'none' && parseFloat(s.outlineWidth) > 0;
        const shadow = s.boxShadow !== 'none';
        return { label: (el.textContent || el.tagName).trim().slice(0, 25), visible: ring || shadow };
      });
      if (info && !info.visible) invisible.push(info.label);
    }
    expect(invisible, `focusable elements with no visible focus ring: ${invisible.join(', ')}`).toEqual([]);
  });
});
