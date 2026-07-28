import AxeBuilder from '@axe-core/playwright';
import { expect, test, login } from './fixtures';
import type { Browser, Page } from '@playwright/test';

/**
 * The approval workflow, driven the way two people drive it.
 *
 * ux.spec.ts discovers its routes from the navigation, so it reaches
 * /approvals and /configuration but never a request's own screens — those have
 * an id in the path and exist only once somebody has raised something. This
 * file raises it, walks the request through an approval and a cancellation
 * with two signed-in people, and applies the same quality bar to every screen
 * it lands on: no sideways scroll, a visible h1, the right chrome for the
 * device, tap targets a thumb can hit, and no WCAG 2 A/AA violation.
 */

const MIN_TAP_PX = 40;
const APPROVER_PASSWORD = 'StrongTestPassword!42';

async function qualityProblems(page: Page, label: string): Promise<string[]> {
  const mobile = page.viewportSize()!.width <= 860;
  const problems: string[] = [];

  const state = await page.evaluate(() => {
    const box = (s: string) => {
      const el = document.querySelector(s);
      if (!el) return null;
      const style = getComputedStyle(el);
      if (style.display === 'none' || style.visibility === 'hidden') return null;
      const r = el.getBoundingClientRect();
      return r.width > 0 && r.height > 0 ? r : null;
    };
    return {
      overflow: document.documentElement.scrollWidth - window.innerWidth,
      heading: !!box('h1'),
      tabbar: !!box('.tabbar'),
      sidebar: !!box('.sidebar'),
    };
  });

  if (state.overflow > 0) problems.push(`${label}: scrolls sideways by ${state.overflow}px`);
  if (!state.heading) problems.push(`${label}: no visible h1`);
  if (mobile && state.sidebar) problems.push(`${label}: sidebar visible on a phone`);
  if (mobile && !state.tabbar) problems.push(`${label}: tab bar missing on a phone`);
  if (!mobile && !state.sidebar) problems.push(`${label}: sidebar missing on desktop`);

  if (mobile) {
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    const trapped = await page.evaluate(() => {
      const bar = document.querySelector('.tabbar')?.getBoundingClientRect();
      if (!bar) return [];
      const hits: string[] = [];
      for (const el of document.querySelectorAll('main.page a, main.page button, main.page input, .overlay:not([hidden]) a, .overlay:not([hidden]) button, .overlay:not([hidden]) input')) {
        const r = el.getBoundingClientRect();
        if (r.width === 0 || r.height === 0) continue;
        if (r.right <= 0 || r.left >= window.innerWidth) continue;
        if (r.bottom <= bar.top + 1 || r.top >= window.innerHeight) continue;
        // Overlapping the bar's rectangle is not the same as being covered by
        // it. A modal overlay is a fixed layer painted above the bar, so ask
        // the browser what is actually on top at the control's centre: that is
        // reachability, where the rectangle alone is only a guess.
        const x = Math.min(Math.max(r.left + r.width / 2, 1), window.innerWidth - 1);
        const y = Math.min(Math.max(r.top + r.height / 2, 1), window.innerHeight - 1);
        const top = document.elementFromPoint(x, y);
        if (top && (top === el || el.contains(top) || top.contains(el))) continue;
        hits.push((el.textContent || el.className).trim().slice(0, 30) || el.tagName);
      }
      return hits;
    });
    if (trapped.length) problems.push(`${label}: tab bar traps ${trapped.join(', ')} at page end`);
    await page.evaluate(() => window.scrollTo(0, 0));

    const small = await page.evaluate((min: number) => {
      const bad: string[] = [];
      for (const el of document.querySelectorAll('main.page button, main.page .btn, .tabbar a')) {
        const r = el.getBoundingClientRect();
        if (r.width === 0 || r.height === 0) continue;
        if (r.height < min) bad.push(`${(el.textContent || '').trim().slice(0, 20)}@${Math.round(r.height)}px`);
      }
      return bad;
    }, MIN_TAP_PX);
    if (small.length) problems.push(`${label}: tap targets under ${MIN_TAP_PX}px — ${small.join(', ')}`);
  }

  const axe = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze();
  for (const v of axe.violations) {
    problems.push(`${label}: ${v.id} (${v.nodes.length}x) — ${v.help}`);
  }
  return problems;
}

/**
 * Gives somebody the Manager role. Nobody may approve their own request, so
 * every one of these journeys needs a second person; the admin raises and this
 * person decides.
 */
async function createApprover(page: Page, runId: string) {
  const email = `mgr-${runId}@example.test`;
  const name = `Manager ${runId}`;
  await page.goto('/users');
  await page.locator('.pb-actions').getByRole('button', { name: 'Add user' }).click();
  const sheet = page.locator('#user-new');
  await sheet.getByLabel('Email').fill(email);
  await sheet.getByLabel('Name').fill(name);
  // Ticked at creation. The create sheet used to offer only a two-value legacy
  // account type, so the real role needed a second trip through the edit sheet.
  await sheet.getByRole('checkbox', { name: /^Manager/ }).check();
  await sheet.getByLabel('Password').fill(APPROVER_PASSWORD);
  await sheet.getByRole('button', { name: 'Add User' }).click();
  await expect(page).toHaveURL(/\/users$/);
  return { email, name };
}

/** Raises a reimbursement and returns the id it was given. */
async function raiseRequest(page: Page, title: string, approver: string, amount = '18400') {
  await page.goto('/requests/new?type=reimbursement');
  await page.getByLabel('Short title').fill(title);
  await page.locator('#project').selectOption({ label: 'Operations' });
  await page.locator('#head').selectOption({ index: 1 });
  await page.getByLabel('Amount').fill(amount);
  await page.getByLabel('Expense date').fill('2026-07-17');
  await page.getByLabel('Purpose').fill('Flight and two nights for the site inspection.');
  await page.getByLabel('Approver').selectOption({ label: approver });
  await page.getByRole('button', { name: 'Submit request' }).click();
  await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);
  return new URL(page.url()).pathname.split('/')[2];
}

async function asApprover(browser: Browser, page: Page, email: string) {
  const context = await browser.newContext({ viewport: page.viewportSize() ?? undefined });
  const approverPage = await context.newPage();
  await login(approverPage, email, APPROVER_PASSWORD);
  return { context, approverPage };
}

test.describe('the approval workflow', () => {
  test('raise, edit, approve and ask for cancellation, on both devices', async ({ adminPage, runId, browser }) => {
    const approver = await createApprover(adminPage, runId);
    const problems: string[] = [];
    const id = await raiseRequest(adminPage, `Site visit ${runId}`, approver.name);

    // --- The requester's detail view. ---
    await adminPage.goto(`/requests/${id}`);
    await expect(adminPage.locator('.rh-status .pill')).toHaveText('Awaiting approval');
    await expect(adminPage.locator('.thread li')).not.toHaveCount(0);
    // The requester is never offered an approve control on their own request.
    await expect(adminPage.locator('[data-open="approve-sheet"]')).toHaveCount(0);
    problems.push(...(await qualityProblems(adminPage, `/requests/{id} (requester)`)));

    // --- The correction screen, and the change on the thread. ---
    await adminPage.getByRole('link', { name: 'Edit request' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/requests/${id}/edit$`));
    problems.push(...(await qualityProblems(adminPage, '/requests/{id}/edit')));
    await adminPage.getByLabel('Amount').fill('21500');
    await adminPage.getByRole('button', { name: /^Save and notify/ }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/requests/${id}$`));
    await expect(adminPage.locator('.thread .tl-change')).toContainText('Amount');

    // --- The approver's view of the same page: a different action bar. ---
    const { context, approverPage } = await asApprover(browser, adminPage, approver.email);
    try {
      await approverPage.goto('/approvals');
      await expect(approverPage.locator('.req-card', { hasText: `Site visit ${runId}` })).toHaveCount(1);

      await approverPage.goto(`/requests/${id}`);
      await expect(approverPage.locator('.waiting.you')).toBeVisible();
      // The requester's own controls are not on the approver's bar.
      await expect(approverPage.getByRole('link', { name: 'Edit request' })).toHaveCount(0);
      problems.push(...(await qualityProblems(approverPage, `/requests/{id} (approver)`)));

      // The approve sheet is a real dialog with a real form in it.
      await approverPage.getByRole('button', { name: /^Approve / }).click();
      const sheet = approverPage.locator('#approve-sheet');
      await expect(sheet).toBeVisible();
      await expect(sheet.getByLabel('Amount approved')).toHaveValue('21,500.00');
      problems.push(...(await qualityProblems(approverPage, '/requests/{id} approve sheet')));

      // An approver may approve less than was asked for.
      await sheet.getByLabel('Amount approved').fill('20000');
      await sheet.getByLabel(/^Note/).fill('Cut the cab fare.');
      await sheet.getByRole('button', { name: 'Approve request' }).click();
      await expect(approverPage).toHaveURL(/\/approvals$/);
      await expect(approverPage.locator('.req-card', { hasText: `Site visit ${runId}` })).toHaveCount(0);
    } finally {
      await context.close();
    }

    // --- Approved: locked for the requester, and cancellable by asking. ---
    await adminPage.goto(`/requests/${id}`);
    await expect(adminPage.locator('.banner.locked')).toBeVisible();
    await expect(adminPage.getByRole('link', { name: 'Edit request' })).toHaveCount(0);
    await adminPage.getByRole('link', { name: 'Request cancellation' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/requests/${id}/cancel$`));
    problems.push(...(await qualityProblems(adminPage, '/requests/{id}/cancel')));

    await adminPage.getByLabel('Reason').fill('The site cancelled the trip.');
    await adminPage.getByRole('button', { name: 'Send cancellation request' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/requests/${id}$`));
    await expect(adminPage.locator('.rh-status .pill.cancelreq')).toBeVisible();
    await expect(adminPage.locator('.banner.warn')).toContainText('Payment is frozen');
    problems.push(...(await qualityProblems(adminPage, '/requests/{id} (frozen)')));

    expect(problems, problems.join('\n')).toEqual([]);
  });

  test('a returned request is corrected and resubmitted on its own screen', async ({ adminPage, runId, browser }) => {
    const approver = await createApprover(adminPage, `ret-${runId}`);
    const problems: string[] = [];
    const id = await raiseRequest(adminPage, `Cab receipts ${runId}`, approver.name);

    const { context, approverPage } = await asApprover(browser, adminPage, approver.email);
    try {
      await approverPage.goto(`/requests/${id}`);
      await approverPage.getByRole('button', { name: 'Return for correction' }).click();
      const sheet = approverPage.locator('#return-sheet');
      await expect(sheet).toBeVisible();
      await sheet.getByLabel('What needs correcting').fill('Attach the cab receipts.');
      await sheet.getByRole('button', { name: 'Return request' }).click();
      await expect(approverPage).toHaveURL(/\/approvals$/);
    } finally {
      await context.close();
    }

    // The same URL now hands the requester a correction form, not a read-only
    // page: returned keeps its number and its history.
    await adminPage.goto(`/requests/${id}`);
    await expect(adminPage.locator('.rh-status .pill.returned')).toBeVisible();
    await expect(adminPage.locator('.banner.warn')).toContainText('Attach the cab receipts.');
    const number = await adminPage.locator('.rh-no').textContent();
    problems.push(...(await qualityProblems(adminPage, '/requests/{id} (returned)')));

    await adminPage.getByLabel('Purpose').fill('Flight, hotel and the airport cabs.');
    await adminPage.getByRole('button', { name: 'Resubmit for approval' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/requests/${id}$`));
    await expect(adminPage.locator('.rh-status .pill')).toHaveText('Awaiting approval');
    await expect(adminPage.locator('.rh-no')).toHaveText(number!);

    expect(problems, problems.join('\n')).toEqual([]);
  });
});
