import type { Page, Route } from '@playwright/test';
import { capturePageErrors, expect, test } from './fixtures';

/**
 * Design-system behaviours (web/static/fervid-app.js).
 *
 * Most behaviours are driven against the component fixture at
 * /static/_kitchensink.html. Two of them — `[data-when]` and the submit-time
 * un-grouping of the money field — have no markup in the kitchen sink, so they
 * run against a small same-origin harness page served by route interception.
 * The harness loads the real /static/fervid-app.js, never a copy.
 */

const KITCHEN_SINK = '/static/_kitchensink.html';
const HARNESS_URL = '/static/_ds-harness.html';
const HARNESS_MATCH = /\/static\/_ds-harness\.html/;

const PHONE = { width: 390, height: 850 };

async function openHarness(page: Page, body: string) {
  await page.route(HARNESS_MATCH, (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'text/html; charset=utf-8',
      body:
        '<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">' +
        '<title>design system harness</title></head><body>' +
        body +
        '<script src="/static/fervid-app.js" defer></script></body></html>'
    })
  );
  await page.goto(HARNESS_URL);
  await page.waitForFunction(() => document.readyState === 'complete');
}

test.describe('design system behaviours', () => {
  // The behaviours are viewport-driven and one of them resizes the window,
  // which the emulated mobile-chrome project forbids. Run the pass once.
  test.beforeEach(() => {
    test.skip(test.info().project.name !== 'chromium', 'component pass runs once');
  });

  /* ------------------------------------------------------------------ */
  /* 1. Accordion                                                        */
  /* ------------------------------------------------------------------ */

  test('.acc-head toggles its .acc-item with aria-expanded and hidden', async ({ page }) => {
    const errors = capturePageErrors(page);
    await page.goto(KITCHEN_SINK);

    const items = page.locator('.acc > .acc-item');
    const openHead = items.nth(0).locator('.acc-head');
    const openBody = items.nth(0).locator('.acc-body');
    const shutHead = items.nth(1).locator('.acc-head');
    const shutBody = items.nth(1).locator('.acc-body');

    // The markup ships the first item open and the second closed; the script
    // must publish that state through the button, not through a class alone.
    await expect(openHead).toHaveAttribute('aria-expanded', 'true');
    await expect(openBody).toBeVisible();
    await expect(shutHead).toHaveAttribute('aria-expanded', 'false');
    await expect(shutBody).toBeHidden();
    expect(await shutBody.evaluate(el => (el as HTMLElement).hidden)).toBe(true);

    await openHead.click();
    await expect(openHead).toHaveAttribute('aria-expanded', 'false');
    await expect(openBody).toBeHidden();
    expect(await openBody.evaluate(el => (el as HTMLElement).hidden)).toBe(true);
    await expect(items.nth(0)).not.toHaveClass(/is-open/);

    await openHead.click();
    await expect(openHead).toHaveAttribute('aria-expanded', 'true');
    await expect(openBody).toBeVisible();
    await expect(items.nth(0)).toHaveClass(/is-open/);

    await shutHead.click();
    await expect(shutHead).toHaveAttribute('aria-expanded', 'true');
    await expect(shutBody).toBeVisible();

    expect(errors).toEqual([]);
  });

  test('.pa-head toggles the permission accordion', async ({ page }) => {
    await page.goto(KITCHEN_SINK);

    const head = page.locator('.perm-acc .pa-head').first();
    const body = page.locator('.perm-acc .pa-body').first();

    await expect(head).toHaveAttribute('aria-expanded', 'true');
    await expect(body).toBeVisible();

    await head.click();
    await expect(head).toHaveAttribute('aria-expanded', 'false');
    await expect(body).toBeHidden();

    await head.click();
    await expect(head).toHaveAttribute('aria-expanded', 'true');
    await expect(body).toBeVisible();
  });

  /* ------------------------------------------------------------------ */
  /* 2. Overlay / sheet                                                  */
  /* ------------------------------------------------------------------ */

  test('[data-open] opens the overlay and moves focus into the sheet', async ({ page }) => {
    await page.goto(KITCHEN_SINK);

    const overlay = page.locator('#ks-overlay');
    await expect(overlay).toBeHidden();

    await page.getByRole('button', { name: 'Open overlay' }).click();
    await expect(overlay).toBeVisible();
    await expect(page.locator('#ks-overlay .sh-close')).toBeFocused();
  });

  test('[data-close] closes the overlay and restores focus to the opener', async ({ page }) => {
    await page.goto(KITCHEN_SINK);

    const opener = page.getByRole('button', { name: 'Open overlay' });
    const overlay = page.locator('#ks-overlay');

    await opener.click();
    await expect(overlay).toBeVisible();

    await page.locator('#ks-overlay .sh-foot').getByRole('button', { name: 'Cancel' }).click();
    await expect(overlay).toBeHidden();
    await expect(opener).toBeFocused();
  });

  test('clicking the overlay backdrop closes the sheet', async ({ page }) => {
    await page.goto(KITCHEN_SINK);

    const opener = page.getByRole('button', { name: 'Open overlay' });
    const overlay = page.locator('#ks-overlay');

    await opener.click();
    await expect(overlay).toBeVisible();

    // Top-left corner is backdrop: .overlay centres the sheet with 20px padding.
    await overlay.click({ position: { x: 4, y: 4 } });
    await expect(overlay).toBeHidden();
    await expect(opener).toBeFocused();
  });

  test('Escape closes the open sheet', async ({ page }) => {
    await page.goto(KITCHEN_SINK);

    const opener = page.getByRole('button', { name: 'Open overlay' });
    const overlay = page.locator('#ks-overlay');

    await opener.click();
    await expect(overlay).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(overlay).toBeHidden();
    await expect(opener).toBeFocused();
  });

  test('focus is trapped inside the open sheet', async ({ page }) => {
    await page.goto(KITCHEN_SINK);

    await page.getByRole('button', { name: 'Open overlay' }).click();

    const close = page.locator('#ks-overlay .sh-close');
    const confirm = page.locator('#ks-overlay .sh-foot').getByRole('button', { name: 'Confirm' });

    await expect(close).toBeFocused();

    // Backwards off the first focusable must wrap to the last, not escape.
    await page.keyboard.press('Shift+Tab');
    await expect(confirm).toBeFocused();

    // Forwards off the last must wrap back to the first.
    await page.keyboard.press('Tab');
    await expect(close).toBeFocused();

    // And it never leaks out however long you tab.
    for (let i = 0; i < 8; i += 1) await page.keyboard.press('Tab');
    expect(
      await page.evaluate(() => !!document.getElementById('ks-overlay')?.contains(document.activeElement))
    ).toBe(true);
  });

  /* ------------------------------------------------------------------ */
  /* 3. More sheet                                                       */
  /* ------------------------------------------------------------------ */

  test('the More tab opens the more sheet and .ms-close dismisses it', async ({ page }) => {
    await page.setViewportSize(PHONE);
    await page.goto(KITCHEN_SINK);

    const more = page.locator('.more-sheet');
    const tab = page.locator('.tabbar .js-more');

    await expect(more).toBeHidden();
    await expect(tab).toBeVisible();

    await tab.click();
    await expect(more).toBeVisible();

    await page.locator('.more-sheet .ms-close').click();
    await expect(more).toBeHidden();
    await expect(tab).toBeFocused();
  });

  /* ------------------------------------------------------------------ */
  /* 4. [data-when] conditional reveal                                   */
  /* ------------------------------------------------------------------ */

  const CONDITIONAL_FORM = `
    <form id="hf" method="get" action="${HARNESS_URL}">
      <select name="mode" aria-label="Mode">
        <option value="upi">UPI</option>
        <option value="neft">NEFT</option>
        <option value="rtgs">RTGS</option>
        <option value="cash">Cash</option>
      </select>
      <div id="w-bank" data-when="mode:neft|rtgs">Bank block</div>

      <label><input type="radio" name="kind" value="advance" checked> Advance</label>
      <label><input type="radio" name="kind" value="invoice"> Invoice</label>
      <div id="w-invoice" data-when="kind:invoice">Invoice block</div>

      <label><input type="checkbox" name="recover"> Recoverable</label>
      <div id="w-recover" data-when="recover:on">Recover block</div>
      <div id="w-not-recover" data-when="recover:off">Not-recoverable block</div>
    </form>`;

  test('[data-when] reveals on a select value', async ({ page }) => {
    await openHarness(page, CONDITIONAL_FORM);
    const bank = page.locator('#w-bank');

    await expect(bank).toBeHidden();
    await page.getByLabel('Mode').selectOption('neft');
    await expect(bank).toBeVisible();

    // The `|` alternation must match every listed value.
    await page.getByLabel('Mode').selectOption('rtgs');
    await expect(bank).toBeVisible();

    await page.getByLabel('Mode').selectOption('cash');
    await expect(bank).toBeHidden();
  });

  test('[data-when] reveals on a radio group value', async ({ page }) => {
    await openHarness(page, CONDITIONAL_FORM);
    const invoice = page.locator('#w-invoice');

    await expect(invoice).toBeHidden();
    await page.locator('input[name="kind"][value="invoice"]').check();
    await expect(invoice).toBeVisible();

    await page.locator('input[name="kind"][value="advance"]').check();
    await expect(invoice).toBeHidden();
  });

  test('[data-when] maps a checkbox to on and off', async ({ page }) => {
    await openHarness(page, CONDITIONAL_FORM);
    const on = page.locator('#w-recover');
    const off = page.locator('#w-not-recover');

    await expect(on).toBeHidden();
    await expect(off).toBeVisible();

    await page.locator('input[name="recover"]').check();
    await expect(on).toBeVisible();
    await expect(off).toBeHidden();

    await page.locator('input[name="recover"]').uncheck();
    await expect(on).toBeHidden();
    await expect(off).toBeVisible();
  });

  /* ------------------------------------------------------------------ */
  /* 5. Money field                                                      */
  /* ------------------------------------------------------------------ */

  test('the money field groups digits in the Indian system as you type', async ({ page }) => {
    await page.goto(KITCHEN_SINK);
    const money = page.locator('#ks-money');

    await money.fill('');
    await money.pressSequentially('1234567');
    // Typed one key at a time — wrong caret handling scrambles the digits.
    await expect(money).toHaveValue('12,34,567');

    await money.fill('');
    await money.pressSequentially('250000.5');
    await expect(money).toHaveValue('2,50,000.5');
  });

  test('the money field spells the amount into .in-words', async ({ page }) => {
    await page.goto(KITCHEN_SINK);
    const money = page.locator('#ks-money');
    const words = page.locator('.money-field .in-words');

    // Ships pre-filled at 1,00,000 — the script must reconcile on load.
    await expect(words).toHaveText('One lakh rupees only');

    await money.fill('250000');
    await expect(words).toHaveText('Two lakh fifty thousand rupees only');

    await money.fill('12345678');
    await expect(money).toHaveValue('1,23,45,678');
    await expect(words).toHaveText('One crore twenty three lakh forty five thousand six hundred seventy eight rupees only');

    await money.fill('');
    await expect(words).toHaveText('Enter the amount you are requesting');
    await expect(words).toHaveClass(/empty/);
  });

  test('the money field posts ungrouped digits on submit', async ({ page }) => {
    await openHarness(
      page,
      `<form id="mf" method="get" action="${HARNESS_URL}">
         <div class="money-field">
           <label for="amt">Amount</label>
           <input id="amt" name="amount" value="">
           <span class="in-words"></span>
         </div>
         <button type="submit">Save</button>
       </form>`
    );

    const money = page.locator('#amt');
    await money.fill('250000');
    await expect(money).toHaveValue('2,50,000');

    await Promise.all([
      page.waitForURL(/_ds-harness\.html\?/),
      page.getByRole('button', { name: 'Save' }).click()
    ]);

    // Commas must be gone before the request leaves the browser.
    expect(new URL(page.url()).searchParams.get('amount')).toBe('250000');
  });

  /* ------------------------------------------------------------------ */
  /* 6. Retained toggleProject                                           */
  /* ------------------------------------------------------------------ */

  test('toggleProject still collapses the variance grid', async ({ adminPage }) => {
    await adminPage.goto('/');

    const toggle = adminPage.locator('[data-project-toggle]').first();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');

    const projectID = await toggle.getAttribute('data-project-toggle');
    const rows = adminPage.locator(`[data-project-row="${projectID}"]`);
    expect(await rows.count()).toBeGreaterThan(0);
    await expect(rows.first()).toBeVisible();

    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(rows.first()).toBeHidden();
    await expect(adminPage.locator('.project-row').first()).toHaveClass(/is-collapsed/);

    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    await expect(rows.first()).toBeVisible();
  });
});
