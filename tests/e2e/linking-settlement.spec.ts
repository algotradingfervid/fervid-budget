import { test, expect, capturePageErrors, createApprovedRequest } from './fixtures';

/**
 * The accounts journey, on a phone.
 *
 * Free-standing payment entry is gone: a payment exists only against an
 * approved request that the person recording it holds a reservation on. So the
 * only route money takes through this product is
 *
 *   /accounts-queue → "Take for processing" → /payments/new?request={id}
 *   → "Payment settled →" → the settlement sheet → "Confirm and save payment"
 *   → /payments/{id}, which nobody can edit or void.
 *
 * ux.spec.ts discovers its routes from the navigation, so it crawls
 * /accounts-queue but never reaches an id-parameterised screen — the entry
 * form, the sheet, the conflict screen and the payment detail are all invisible
 * to it. This file walks them, at 390 px, where the queue's table restacks into
 * cards and the sheet has to share the viewport with the tab bar.
 *
 * Chromium only: the journey is about the flow, not the engine, and it is
 * expensive — every approved request needs a second signed-in person to approve
 * it (G8), so running it twice buys nothing.
 */

test.beforeEach(async ({}, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium', 'Linking flow runs once on chromium.');
});

test.describe('payment linking and settlement', () => {
  test('accountant reserves from the queue, confirms in the sheet, and the payment is immutable', async ({ adminPage, runId }) => {
    const errors = capturePageErrors(adminPage);
    await adminPage.setViewportSize({ width: 390, height: 850 });
    const request = await createApprovedRequest(adminPage, runId, { amount: '5000.00' });

    // Entry point: the queue's "Take for processing" reserves atomically (S1/S2).
    // The queue is /accounts-queue, not the plan's /requests/to-pay, which is
    // not a route; its default tab is "approved".
    await adminPage.goto('/accounts-queue');
    await expect(adminPage.locator('.metric-strip .metric')).toHaveCount(4);
    await expect(adminPage.locator('.segmented a')).toHaveCount(5);
    const row = adminPage.locator('tr', { hasText: request.number });
    // Reserving is a mutation, so the control is a submit button in a POST
    // form, not a link. At 390 px the table is display:block cards, but the
    // <tr> elements survive the restack and the row locator still resolves.
    await row.getByRole('button', { name: 'Take for processing' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${request.id}$`));
    await expect(adminPage.locator('.reserve-bar')).toContainText('Reserved by you');

    // Under-paying opens the settlement sheet; nothing is saved yet (D8).
    await adminPage.getByLabel('Amount actually paid').fill('4000.00');
    await expect(adminPage.locator('#diff-banner')).toHaveClass(/warn/);
    // The plan filled only the amount. Every one of these three is `required`
    // on the entry form and they all live inside the same <form> as "Confirm
    // and save payment", so leaving them empty makes the browser refuse the
    // confirm submit — the sheet opens and then nothing happens. Paid on is
    // prefilled with today; the mode and the reference are not.
    await adminPage.getByLabel('Paid on').fill('2026-07-24');
    await adminPage.getByLabel('Payment mode').selectOption('bank_transfer');
    await adminPage.getByLabel('Transaction / UTR reference').fill(`UTR-${request.id}`);

    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    // htmx swaps the sheet into #settle-mount inside the live form, so the URL
    // does not change and the overlay arrives already visible.
    const sheet = adminPage.locator('.overlay .sheet');
    await expect(sheet).toBeVisible();
    await expect(sheet.locator('.cmp-row.diff')).toContainText('1,000.00');
    await expect(sheet.locator('.outcome.good')).toHaveText('Completed');
    await expect(sheet.locator('.outcome.warn')).toHaveText(['Keep payable', 'Manager review']);
    // Short payments require a deliberate disposition. This test closes with
    // an explained adjustment; an installment would keep the balance payable.
    await expect(sheet.locator('input[name="settlement"]:checked')).toHaveCount(0);
    await sheet.locator('input[name="settlement"][value="settled"]').check();
    // A short close without an explanation must fail without losing the form.
    const refusedPayment = adminPage.waitForResponse(response => response.url().endsWith('/payments') && response.request().method() === 'POST');
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();
    expect((await refusedPayment).status()).toBe(400);
    await expect(adminPage).toHaveURL(/\/payments$/);
    await expect(sheet.locator('.banner.bad')).toContainText('explain the deduction');
    await sheet.getByRole('button', { name: 'Go back' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${request.id}$`));
    await expect(adminPage.getByLabel('Amount actually paid')).toHaveValue('4,000.00');
    await expect(adminPage.getByLabel('Paid on')).toHaveValue('2026-07-24');
    await expect(adminPage.getByLabel('Payment mode')).toHaveValue('bank_transfer');
    await expect(adminPage.getByLabel('Transaction / UTR reference')).toHaveValue(`UTR-${request.id}`);
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    await expect(sheet.locator('input[name="settlement"]:checked')).toHaveCount(0);
    await sheet.locator('input[name="settlement"][value="settled"]').check();
    await sheet.locator('textarea[name="partial_reason"]').fill('Agreed contractual deduction of 1000; no balance remains owed.');

    // Confirm: one POST, and we land on the payment (S10).
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();
    await expect(adminPage).toHaveURL(/\/payments\/\d+$/);
    await expect(adminPage.locator('.pill.completed')).toBeVisible();
    await expect(adminPage.locator('.compare .cmp-row.match')).toBeVisible();
    await expect(adminPage.locator('ol.thread li')).not.toHaveCount(0);

    // S12: no mutation control anywhere on a linked payment.
    await expect(adminPage.getByRole('link', { name: /Edit/ })).toHaveCount(0);
    await expect(adminPage.getByRole('button', { name: /Void/ })).toHaveCount(0);

    // The page never scrolls sideways at 390 px.
    const overflow = await adminPage.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(0);
    // Chromium reports the deliberately exercised HTTP400 as a resource error.
    // Exactly that expected response is allowed; script and all other errors fail.
    expect(errors).toEqual(['console: Failed to load resource: the server responded with a status of 400 (Bad Request)']);
  });

  test('a second accountant gets the conflict screen, not an error page', async ({ adminPage, secondPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '2500.00' });
    await adminPage.goto('/accounts-queue');
    await adminPage.locator('tr', { hasText: request.number })
      .getByRole('button', { name: 'Take for processing' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${request.id}$`));

    // The second accountant sees it in Processing, named to whoever holds it.
    await secondPage.goto('/accounts-queue?tab=processing');
    const held = secondPage.locator('tr', { hasText: request.number });
    await expect(held).toBeVisible();
    const holder = (await held.locator('.pill.processing').innerText()).replace(/^Reserved by\s*/, '').trim();
    expect(holder).not.toBe('');

    // Their row offers no way to take it — reservation:reassign is admin-only,
    // so an Accounts user gets a read-only "View". Losing the race means going
    // at the entry screen directly, which is exactly what a second tab left
    // open would do. G15: that is a rendered screen carrying HTTP 409, not an
    // error page.
    const response = await secondPage.goto(`/payments/new?request=${request.id}`);
    expect(response?.status()).toBe(409);
    await expect(secondPage.locator('.banner.bad')).toContainText(`${holder} took this request before you`);
    await expect(secondPage.locator('.banner.bad')).toContainText('no payment was created');
    await expect(secondPage.locator('.rh-status .pill.processing')).toContainText(holder);
    // The three things the loser may actually do, not a dead end.
    await expect(secondPage.locator('.a-list a[href="/accounts-queue"]')).toBeVisible();
    await expect(secondPage.locator(`.a-list a[href="/requests/${request.id}"]`)).toBeVisible();
    // .error-code belongs to the error_page template. Its absence is what
    // separates "a screen about a lost race" from "something went wrong".
    await expect(secondPage.locator('.error-code')).toHaveCount(0);
  });
});
