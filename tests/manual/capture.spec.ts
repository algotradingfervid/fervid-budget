import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { ACTORS, type RoleKey } from './actors';
import { SHOTS, type Shot } from './shotlist';

// Captures every screenshot the manual uses, and measures the page while it is
// there. Both outputs are per-shot JSON files rather than one shared file,
// because the run is parallel and concurrent appends to one manifest would race.

const SHOT_DIR = 'docs/manual/assets/shots';
const META_DIR = 'output/manual/meta';

const OVERFLOW_WIDTHS = [1440, 1024, 768];

// Captures are full-page but capped. A plain fullPage shot of the Accounts queue
// is 12,000px tall and 3.4 MB, and shows 200 rows nobody reads — a manual wants
// the screen as you actually meet it. Two screenfuls keeps every form and sheet
// whole while stopping the long lists from dominating the repo. Shots that
// genuinely need the entire page set `fullHeight`.
const MAX_SHOT_HEIGHT = 2200;
const SHOT_WIDTH = 1440;

interface Finding {
  kind: string;
  detail: string;
}

interface ShotMeta {
  id: string;
  role: RoleKey;
  title: string;
  shows: string;
  url: string;
  file: string;
  bytes: number;
  /** True when the page was taller than the cap and the shot shows only the top. */
  clipped: boolean;
  pageHeight: number;
  headings: string[];
  findings: Finding[];
}

async function login(page: Page, role: RoleKey): Promise<void> {
  const actor = ACTORS[role];
  await page.goto('/login');
  await page.fill('input[name="email"]', actor.email);
  await page.fill('input[name="password"]', actor.password);
  await page.click('form[action="/login"] button');
  await expect(page, `${actor.email} should be able to sign in`).not.toHaveURL(/\/login/);
}

/**
 * Document-level horizontal overflow at each width. The app's own rule is zero
 * tolerance.
 *
 * This asks the page to scroll rather than comparing scrollWidth to innerWidth.
 * The arithmetic version over-reports: a `position: sticky` table header inside
 * a `.table-wrap` that scrolls on its own inflates
 * `documentElement.scrollWidth` even though the document itself does not move,
 * which flagged the roles matrix as broken when it was behaving exactly as
 * designed. Trying to scroll and seeing whether anything moves cannot produce
 * that false positive.
 */
async function overflowFindings(page: Page): Promise<Finding[]> {
  const out: Finding[] = [];
  for (const width of OVERFLOW_WIDTHS) {
    await page.setViewportSize({ width, height: 1200 });
    const over = await page.evaluate(() => {
      const el = document.scrollingElement ?? document.documentElement;
      const before = el.scrollLeft;
      el.scrollLeft = 99999;
      const moved = el.scrollLeft;
      el.scrollLeft = before;
      return moved;
    });
    if (over > 0) {
      out.push({ kind: 'horizontal-overflow', detail: `the page itself scrolls sideways by ${over}px at ${width}px wide` });
    }
  }
  await page.setViewportSize({ width: 1440, height: 1200 });
  return out;
}

/**
 * Heading outline. The shipped suite only asserts that an h1 exists; a skipped
 * level or a second h1 passes today because axe's heading-order rule is tagged
 * best-practice and the suite filters it out.
 */
async function headingFindings(page: Page): Promise<{ headings: string[]; findings: Finding[] }> {
  const headings = await page.evaluate(() =>
    Array.from(document.querySelectorAll('h1,h2,h3,h4,h5,h6'))
      .filter((h) => (h as HTMLElement).offsetParent !== null)
      .map((h) => `${h.tagName}:${(h.textContent ?? '').trim().slice(0, 60)}`),
  );
  const findings: Finding[] = [];
  const levels = headings.map((h) => Number(h[1]));
  const h1s = levels.filter((l) => l === 1).length;
  if (h1s === 0) findings.push({ kind: 'no-h1', detail: 'the page has no visible level-one heading' });
  if (h1s > 1) findings.push({ kind: 'multiple-h1', detail: `${h1s} level-one headings on one page` });
  for (let i = 1; i < levels.length; i++) {
    if (levels[i]! - levels[i - 1]! > 1) {
      findings.push({
        kind: 'heading-skip',
        detail: `h${levels[i - 1]} is followed by h${levels[i]}, skipping a level`,
      });
      break;
    }
  }
  return { headings, findings };
}

/** Interactive targets too small to hit comfortably. The suite checks height on mobile only. */
async function targetFindings(page: Page): Promise<Finding[]> {
  const small = await page.evaluate(() => {
    const out: string[] = [];
    document.querySelectorAll('main.page button, main.page .btn, main.page a.btn').forEach((el) => {
      const r = el.getBoundingClientRect();
      if (r.width === 0 && r.height === 0) return;
      if (r.width < 24 || r.height < 24) {
        out.push(`${(el.textContent ?? '').trim().slice(0, 30) || el.className} ${Math.round(r.width)}×${Math.round(r.height)}`);
      }
    });
    return out.slice(0, 5);
  });
  return small.map((detail) => ({ kind: 'small-target', detail: `${detail} — under 24×24 CSS px` }));
}

async function axeFindings(page: Page): Promise<Finding[]> {
  try {
    // best-practice is included deliberately: it is what carries heading-order,
    // landmark and region rules that the shipped suite's tag filter excludes.
    const result = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'best-practice'])
      .analyze();
    return result.violations.map((v) => ({
      kind: `a11y:${v.id}`,
      detail: `${v.impact ?? 'unknown'} — ${v.help} (${v.nodes.length} element${v.nodes.length === 1 ? '' : 's'})`,
    }));
  } catch (err) {
    return [{ kind: 'a11y:scan-failed', detail: String(err).slice(0, 160) }];
  }
}

test.describe.configure({ mode: 'parallel' });

for (const shot of SHOTS) {
  test(`shot ${shot.id}`, async ({ page }) => {
    await login(page, shot.role);

    // Anything a shot changed to make itself possible is undone in `finally`,
    // including when the shot fails. A shot that locks a month to photograph a
    // locked month must not leave the month locked for every later shot.
    try {
    if (shot.prepare) {
      await shot.prepare(page);
    } else {
      const response = await page.goto(shot.url, { waitUntil: 'domcontentloaded' });
      if (shot.expectStatus !== undefined) {
        // The refusal *is* the screenshot. Asserting the exact status keeps the
        // manual honest: if the app stops answering 403 here, the shot that
        // illustrates "your role cannot open this" fails rather than quietly
        // photographing a page that now works.
        expect(response?.status(), `${shot.url} should answer ${shot.expectStatus}`).toBe(shot.expectStatus);
      } else {
        expect(response?.status(), `${shot.url} should render`).toBeLessThan(400);
      }
    }
    await page.waitForLoadState('networkidle').catch(() => undefined);

    if (shot.openSheet) {
      const opener = page.locator(`[data-open="${shot.openSheet}"]`).first();
      await expect(opener, `opener ${shot.openSheet} missing on ${shot.url}`).toHaveCount(1);
      await opener.click();
      await page.locator(`#${shot.openSheet}`).waitFor({ state: 'visible', timeout: 10_000 });
    }

    if (shot.waitFor) {
      await page.locator(shot.waitFor).first().waitFor({ state: 'visible', timeout: 15_000 });
    }

    const html = await page.content();
    const findings: Finding[] = [];
    findings.push(...(await overflowFindings(page)));
    const { headings, findings: headingIssues } = await headingFindings(page);
    findings.push(...headingIssues);
    findings.push(...(await targetFindings(page)));
    if (!shot.skipAxe) findings.push(...(await axeFindings(page)));

    const file = join(SHOT_DIR, `${shot.id}.png`);
    mkdirSync(dirname(file), { recursive: true });
    const pageHeight = await page.evaluate(() => document.documentElement.scrollHeight);
    const clipped = !shot.fullHeight && pageHeight > MAX_SHOT_HEIGHT;
    await page.screenshot({
      path: file,
      fullPage: true,
      ...(clipped ? { clip: { x: 0, y: 0, width: SHOT_WIDTH, height: MAX_SHOT_HEIGHT } } : {}),
    });

    const meta: ShotMeta = {
      id: shot.id,
      role: shot.role,
      title: shot.title,
      shows: shot.shows,
      url: shot.url,
      file: `${shot.id}.png`,
      bytes: Buffer.byteLength(html, 'utf8'),
      clipped,
      pageHeight,
      headings,
      findings,
    };
    const metaFile = join(META_DIR, `${shot.id.replace(/\//g, '__')}.json`);
    mkdirSync(dirname(metaFile), { recursive: true });
    writeFileSync(metaFile, JSON.stringify(meta, null, 2));
    } finally {
      if (shot.cleanup) await shot.cleanup(page);
    }
  });
}
