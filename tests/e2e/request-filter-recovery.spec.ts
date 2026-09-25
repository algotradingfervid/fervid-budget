import { test, expect, createApprovedRequest, settlePayment } from './fixtures';

test('completed requests live in Closed and visible Clear filters recovers an empty list', async ({ adminPage, runId }) => {
  const request = await createApprovedRequest(adminPage, runId, { amount: '50.00' });
  await settlePayment(adminPage, request.id, { amount: '50.00', paidOn: '2026-07-15', reference: `CLOSED-${runId}` });
  await adminPage.goto(`/requests?bucket=closed&type=vendor_invoice&treatment=budget&q=${encodeURIComponent(request.number)}`);
  const card = adminPage.locator('.req-card', { hasText: request.number });
  await expect(card).toBeVisible();
  await expect(adminPage.locator('.filter-reset').getByRole('link', { name: 'Clear filters' })).toBeVisible();
  await adminPage.locator('.segmented a').filter({ hasText: /^Open\s/ }).click();
  await expect(card).toHaveCount(0);
  await expect(adminPage).toHaveURL(/type=vendor_invoice/);
  await expect(adminPage).toHaveURL(/treatment=budget/);
  await adminPage.locator('.segmented a').filter({ hasText: /^Closed\s/ }).click();
  await expect(card).toBeVisible();

  if (adminPage.viewportSize()!.width <= 860) {
    const search = adminPage.locator('form.m-filters input[name="q"]');
    await search.fill(`no-such-invoice-${runId}`);
    await search.press('Enter');
  } else {
    await adminPage.locator('#q').fill(`no-such-invoice-${runId}`);
    await adminPage.getByRole('button', { name: 'Apply', exact: true }).click();
  }
  await expect(adminPage.locator('.req-list .empty')).toBeVisible();
  await adminPage.locator('.filter-reset').getByRole('link', { name: 'Clear filters' }).click();
  const reset = new URL(adminPage.url());
  expect(reset.searchParams.get('bucket')).toBe('closed');
  expect(reset.searchParams.has('q')).toBe(false);
  expect(reset.searchParams.has('type')).toBe(false);
  expect(reset.searchParams.has('treatment')).toBe(false);
  await expect(card).toBeVisible();
});
