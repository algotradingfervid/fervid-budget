import { expect, test } from '@playwright/test';

// Exercise the shipped script and native form gate together. The intercepted
// form makes submitted values observable without inventing financial records.
test.beforeEach(async ({ page }) => {
  await page.route(/\/money-validation-harness(?:\?|$)/, route => route.fulfill({
    contentType: 'text/html',
    body: `<!doctype html><form method="get" action="/money-validation-harness">
      <div class="money-field"><label for="amount">Amount</label>
        <input id="amount" name="amount" required inputmode="decimal">
        <span class="in-words"></span></div>
      <button>Save</button></form><script src="/static/fervid-app.js" defer></script>`
  }));
  await page.goto('/money-validation-harness');
  await expect(page.getByLabel('Amount', { exact: true })).toHaveAttribute('data-money-bound', '1');
});

test('invalid money stays intact, cannot submit, and clears its error after correction', async ({ page }) => {
  const amount = page.getByLabel('Amount', { exact: true });
  for (const invalid of ['-77.89', '−88.90', '1.001', '2.998', '1.2.3', '12abc', '₹ -1.01', '0', '92233720368547758.08', '1e999999']) {
    await amount.fill(invalid);
    await amount.blur();
    await expect(amount).toHaveValue(invalid);
    await page.getByRole('button', { name: 'Save' }).click();
    await expect(page.locator('.client-error-summary')).toBeVisible();
    await expect(amount).toHaveAttribute('aria-invalid', 'true');
    expect(new URL(page.url()).search).toBe('');
    await amount.fill('400.13');
    await expect(page.locator('.client-error-summary')).toHaveCount(0);
    await expect(amount).not.toHaveAttribute('aria-invalid', 'true');
    await expect(page.locator('.in-words')).toHaveText('Four hundred rupees and thirteen paise only');
  }
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(/amount=400.13/);
});

test('grouping, fractional rupees, exponent notation and large exact values keep their value', async ({ page }) => {
  const amount = page.getByLabel('Amount', { exact: true });
  for (const [entered, submitted] of [
    ['₹ 1,23,456.78', '123456.78'], ['.5', '.5'], ['1.', '1.'],
    ['1e1', '1e1'], ['1e-2', '1e-2'], ['1.001e3', '1.001e3'],
    ['90071992547409.93', '90071992547409.93'],
    ['92233720368547758.07', '92233720368547758.07']
  ]) {
    await amount.fill(entered);
    await page.getByRole('button', { name: 'Save' }).click();
    await expect.poll(() => new URL(page.url()).searchParams.get('amount')).toBe(submitted);
  }
});

test('data-money recovery fields use the same gate without requiring the currency wrapper', async ({ page }) => {
  await page.locator('.money-field').evaluate(el => el.classList.remove('money-field'));
  await page.locator('#amount').evaluate(el => el.setAttribute('data-money', ''));
  const amount = page.getByLabel('Amount', { exact: true });
  await amount.fill('1.001');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(amount).toHaveValue('1.001');
  await expect(page.locator('.client-error-summary')).toContainText('two decimal places');
  await amount.fill('1.01');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page).toHaveURL(/amount=1.01/);
});
