import AxeBuilder from '@axe-core/playwright';
import { expect, test } from './fixtures';
import type { Page } from '@playwright/test';

/**
 * The request creation flow, driven the way a person drives it.
 *
 * ux.spec.ts discovers its routes from the navigation, so it covers /requests
 * and nothing deeper. The screens that matter most here — the adaptive form and
 * the confirmation — are reachable only by walking the flow, so this file walks
 * it and then applies the same quality bar to what it finds: no sideways
 * scroll, a visible h1, the right chrome for the device, tap targets a thumb
 * can hit, and no WCAG 2 A/AA violation.
 */

const MIN_TAP_PX = 40;

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
    // Nothing may be trapped under the fixed tab bar at the end of the page,
    // and nothing a thumb has to hit may be smaller than the design's floor.
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    const trapped = await page.evaluate(() => {
      const bar = document.querySelector('.tabbar')?.getBoundingClientRect();
      if (!bar) return [];
      const hits: string[] = [];
      for (const el of document.querySelectorAll('main.page a, main.page button, main.page input')) {
        const r = el.getBoundingClientRect();
        if (r.width === 0 || r.height === 0) continue;
        if (r.right <= 0 || r.left >= window.innerWidth) continue;
        if (r.bottom > bar.top + 1 && r.top < window.innerHeight) {
          hits.push((el.textContent || el.className).trim().slice(0, 30) || el.tagName);
        }
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

/** Gives somebody the Manager role, because nobody may approve their own request. */
async function createApprover(page: Page, runId: string) {
  const email = `approver-${runId}@example.test`;
  const name = `Approver ${runId}`;
  await page.goto('/users');
  await page.locator('.pb-actions').getByRole('button', { name: 'Add user' }).click();
  const sheet = page.locator('#user-new');
  await sheet.getByLabel('Email').fill(email);
  await sheet.getByLabel('Name').fill(name);
  // The role is ticked where the account is created. This used to need a second
  // trip through the edit sheet, because the create form offered only a
  // two-value legacy account type that could not name a real role.
  await sheet.getByRole('checkbox', { name: /^Manager/ }).check();
  await sheet.getByLabel('Password').fill('StrongTestPassword!42');
  await sheet.getByRole('button', { name: 'Add User' }).click();
  await expect(page).toHaveURL(/\/users$/);
  return name;
}

async function createVendor(page: Page, runId: string) {
  const name = `Sundaram Electricals ${runId}`;
  await page.goto('/vendors/new');
  await page.getByLabel('Vendor name').fill(name);
  await page.getByLabel('Type').selectOption('company');
  await page.getByRole('button', { name: 'Save vendor' }).click();
  await expect(page).toHaveURL(/\/vendors\/\d+$/);
  return name;
}

test.describe('raising a request', () => {
  test('the type chooser, the adaptive form and the confirmation all hold up', async ({ adminPage, runId }) => {
    const approver = await createApprover(adminPage, runId);
    const vendor = await createVendor(adminPage, runId);
    const problems: string[] = [];

    // Step 1 — the chooser. Picking a type is a navigation, so the card is a link.
    await adminPage.goto('/requests/new');
    await expect(adminPage.locator('.type-grid .type-card')).toHaveCount(4);
    problems.push(...(await qualityProblems(adminPage, '/requests/new')));

    await adminPage.getByRole('link', { name: /Vendor invoice payment/ }).click();
    await expect(adminPage).toHaveURL(/\/requests\/new\?type=vendor_invoice$/);
    await expect(adminPage.getByRole('heading', { level: 1 })).toHaveText('Vendor invoice payment');

    // Step 2 — the adaptive form. There is no type control on it at all.
    await expect(adminPage.locator('select[name="type"]')).toHaveCount(0);
    problems.push(...(await qualityProblems(adminPage, '/requests/new?type=vendor_invoice')));

    // A vendor invoice is always a budget expense — the store refuses any other
    // treatment for it — so the form states that rather than offering a choice
    // it would reject after the requester had filled in the fields it reveals.
    await expect(adminPage.getByRole('radio', { name: /Refundable or recoverable/ })).toHaveCount(0);
    await expect(adminPage.locator('input[type="hidden"][name="treatment"][value="budget"]')).toHaveCount(1);
    await expect(adminPage.locator('#form-fields select[name="head_id"]')).toBeVisible();

    // The choice, and the server-side swap it drives, belong to the one type
    // that may be recoverable.
    await adminPage.goto('/requests/new?type=employee_advance');
    await adminPage.getByRole('radio', { name: /Refundable or recoverable/ }).check();
    await expect(adminPage.locator('#form-fields textarea[name="repayment_notes"]')).toBeVisible();
    await expect(adminPage.locator('#form-fields select[name="head_id"]')).toHaveCount(0);
    await adminPage.getByRole('radio', { name: /Budget expense/ }).check();
    await expect(adminPage.locator('#form-fields select[name="head_id"]')).toBeVisible();

    await adminPage.goto('/requests/new?type=vendor_invoice');
    await expect(adminPage.locator('#form-fields select[name="head_id"]')).toBeVisible();

    // Choosing a project narrows the heads to that project's own.
    await adminPage.locator('#project').selectOption({ label: 'Operations' });
    await expect(adminPage.locator('#form-fields select[name="head_id"] option')).not.toHaveCount(1);
    await adminPage.locator('#head').selectOption({ index: 1 });

    // The combobox writes the vendor's id into the hidden input the form posts.
    await adminPage.locator('#vendor').pressSequentially(`Sundaram Electricals ${runId}`);
    await adminPage.locator('.combo-list .co', { hasText: vendor }).first().click();
    await expect(adminPage.locator('#vendor-id')).not.toHaveValue('');

    await adminPage.getByLabel('Short title').fill(`Switchgear supply ${runId}`);
    await adminPage.getByLabel('Amount').fill('100000');
    await expect(adminPage.locator('.in-words')).toContainText('lakh', { ignoreCase: true });
    await adminPage.getByLabel('Invoice number').fill(`SE/${runId}`);
    await adminPage.getByLabel('Invoice date').fill('2026-07-18');
    await adminPage.getByLabel('Purpose').fill('11kV switchgear panels for the substation.');
    await adminPage.getByLabel('Approver').selectOption({ label: approver });

    await adminPage.getByRole('button', { name: 'Submit request' }).click();

    // D1: one POST created it, numbered it and sent it. No draft step existed.
    await expect(adminPage).toHaveURL(/\/requests\/\d+\/submitted$/);
    await expect(adminPage.locator('.banner.good')).toContainText(approver);
    await expect(adminPage.locator('.req-head .rh-no')).toContainText('PR-');
    await expect(adminPage.locator('.rh-status .pill')).toHaveText('Awaiting approval');
    problems.push(...(await qualityProblems(adminPage, '/requests/{id}/submitted')));

    // The list shows it, waiting on the approver, under the Open tab.
    await adminPage.getByRole('link', { name: 'My requests' }).first().click();
    await expect(adminPage).toHaveURL(/\/requests$/);
    const card = adminPage.locator('.req-card', { hasText: `Switchgear supply ${runId}` });
    await expect(card).toHaveCount(1);
    await expect(card.locator('.waiting')).toContainText(approver);
    problems.push(...(await qualityProblems(adminPage, '/requests')));

    expect(problems, problems.join('\n')).toEqual([]);
  });

  test('the duplicate check warns and never blocks', async ({ adminPage, runId }) => {
    const approver = await createApprover(adminPage, runId);

    const raise = async (title: string) => {
      await adminPage.goto('/requests/new?type=reimbursement');
      await adminPage.getByLabel('Short title').fill(title);
      await adminPage.locator('#project').selectOption({ label: 'Operations' });
      await adminPage.locator('#head').selectOption({ index: 1 });
      await adminPage.getByLabel('Amount').fill('18400');
      await adminPage.getByLabel('Expense date').fill('2026-07-17');
      await adminPage.getByLabel('Purpose').fill('flight and hotel');
      await adminPage.getByLabel('Approver').selectOption({ label: approver });
      await adminPage.getByRole('button', { name: 'Submit request' }).click();
      await expect(adminPage).toHaveURL(/\/requests\/\d+\/submitted$/);
    };

    await raise(`Hyderabad site visit ${runId}`);

    // The same claim again: the check points at the first one…
    await adminPage.goto('/requests/new?type=reimbursement');
    await adminPage.getByLabel('Short title').fill(`Hyderabad site visit again ${runId}`);
    await adminPage.locator('#project').selectOption({ label: 'Operations' });
    await adminPage.locator('#head').selectOption({ index: 1 });
    await adminPage.getByLabel('Amount').fill('18400');
    await adminPage.getByLabel('Amount').blur();
    await expect(adminPage.locator('#dup-check .banner.warn')).toContainText('you can still go ahead');
    // The e2e database persists between runs, so there may be older look-alikes
    // in the list too — that is the feature working, not a fixture leaking.
    await expect(adminPage.locator('#dup-check')).toContainText(`Hyderabad site visit ${runId}`);

    // …and the submit goes through anyway. The system points, the person decides.
    await adminPage.getByLabel('Expense date').fill('2026-07-18');
    await adminPage.getByLabel('Purpose').fill('the second leg of the same trip');
    await adminPage.getByLabel('Approver').selectOption({ label: approver });
    await adminPage.getByRole('button', { name: 'Submit request' }).click();
    await expect(adminPage).toHaveURL(/\/requests\/\d+\/submitted$/);
  });
});
