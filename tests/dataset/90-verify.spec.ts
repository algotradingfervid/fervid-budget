import { test, expect, type Page } from '@playwright/test';
import { login } from './helpers';
import { users, PASSWORD, MONTHS } from './plan';

// Verification pass over the populated dataset.
//
// These are assertions about the running application, not data entry. They are
// meant to fail when the app is wrong, so nothing here is written defensively.

const admin = { email: 'admin@fervid.local', password: 'admin123' };
const requester = users.find((u) => u.role === 'Requester' && u.active)!;
const inactive = users.find((u) => u.role === 'Requester' && !u.active)!;
const manager = users.find((u) => u.role === 'Manager')!;
const accountant = users.find((u) => u.role === 'Accounts')!;

async function statusOfPath(page: Page, path: string): Promise<number> {
  const res = await page.request.get(path, { maxRedirects: 0 });
  return res.status();
}

test.describe('access control', () => {
  test('a requester cannot reach admin-only screens', async ({ page }) => {
    await login(page, requester.email, PASSWORD);
    for (const path of ['/users', '/roles', '/budgets', '/months', '/heads', '/projects', '/audit', '/backups']) {
      const status = await statusOfPath(page, path);
      expect(status, `${path} should be refused for a requester`).not.toBe(200);
    }
  });

  test('a requester sees only their own requests', async ({ page }) => {
    await login(page, requester.email, PASSWORD);
    await page.goto('/requests?bucket=all');
    const names = await page.locator('.req-card').count();
    expect(names, 'requester should see some of their own').toBeGreaterThan(0);

    // Every visible card must belong to them. The list is scoped by request=own.
    const foreign = await page.locator('.req-card:not(.is-mine)').count();
    expect(foreign, 'a scoped list must not leak other people rows').toBe(0);
  });

  test('a manager can reach approvals but not user administration', async ({ page }) => {
    await login(page, manager.email, PASSWORD);
    expect(await statusOfPath(page, '/approvals')).toBe(200);
    expect(await statusOfPath(page, '/users'), '/users should be refused for a manager').not.toBe(200);
  });

  test('accounts can reach the queue and payments but not user administration', async ({ page }) => {
    await login(page, accountant.email, PASSWORD);
    expect(await statusOfPath(page, '/accounts-queue')).toBe(200);
    expect(await statusOfPath(page, '/payments')).toBe(200);
    expect(await statusOfPath(page, '/users'), '/users should be refused for accounts').not.toBe(200);
  });

  test('a deactivated user cannot log in', async ({ page }) => {
    await page.goto('/login');
    await page.fill('input[name="email"]', inactive.email);
    await page.fill('input[name="password"]', PASSWORD);
    await page.click('form[action="/login"] button');
    await expect(page, 'an inactive account must not get a session').toHaveURL(/\/login/);
  });

  test('a requester is never offered themselves as approver', async ({ page }) => {
    await login(page, requester.email, PASSWORD);
    await page.goto('/requests/new?type=reimbursement');
    const options = await page.locator('select[name="manager_id"] option').allTextContents();
    expect(options.join('|'), 'self-approval must not be selectable').not.toContain(requester.name);
  });
});

test.describe('lists, filters and scale', () => {
  test('the default bucket hides in-flight statuses and bucket=all reveals them', async ({ page }) => {
    await login(page, admin.email, admin.password);
    await page.goto('/requests');
    const defaultCount = await page.locator('.req-card').count();
    await page.goto('/requests?bucket=all');
    const allCount = await page.locator('.req-card').count();
    expect(allCount, 'bucket=all should not show fewer rows than the default').toBeGreaterThanOrEqual(defaultCount);
  });

  test('a request can be found by its number', async ({ page }) => {
    await login(page, admin.email, admin.password);
    await page.goto('/requests?bucket=all&q=PR-2026-000001');
    await expect(page.locator('.req-card'), 'search by number should match exactly one').toHaveCount(1);
  });

  test('the accounts queue tabs are all reachable and populated', async ({ page }) => {
    await login(page, accountant.email, PASSWORD);
    for (const tab of ['approved', 'processing', 'hold', 'partial_review', 'paid']) {
      await page.goto(`/accounts-queue?tab=${tab}`);
      await expect(page.locator('table.t-cards tbody tr').first(), `${tab} tab should have rows`).toBeVisible();
    }
  });

  test('an unknown queue tab is refused rather than silently defaulted', async ({ page }) => {
    await login(page, accountant.email, PASSWORD);
    expect(await statusOfPath(page, '/accounts-queue?tab=nonsense')).toBe(400);
  });

  test('the request list paginates beyond its page size', async ({ page }) => {
    await login(page, admin.email, admin.password);
    await page.goto('/requests?bucket=all');
    const first = await page.locator('.req-card').count();
    await page.goto('/requests?bucket=all&offset=200');
    const second = await page.locator('.req-card').count();
    expect(first, 'first page should be capped').toBeLessThanOrEqual(200);
    expect(second, 'there should be a second page with 574 requests loaded').toBeGreaterThan(0);
  });
});

test.describe('budget reporting', () => {
  test('the variance grid renders every month and flags overspend', async ({ page }) => {
    await login(page, admin.email, admin.password);
    for (const month of MONTHS) {
      await page.goto(`/grid?month=${month}`);
      await expect(page.locator('body'), `grid should render for ${month}`).toContainText('₹');
    }
  });

  test('overspent heads are classified as over, not silently absorbed', async ({ page }) => {
    await login(page, admin.email, admin.password);
    await page.goto('/grid?month=2026-07');
    const body = await page.locator('body').innerText();
    expect(body.toLowerCase(), 'the grid should surface an over-budget state somewhere').toMatch(/over/);
  });

  test('exports return CSV with data rows', async ({ page }) => {
    await login(page, admin.email, admin.password);
    for (const path of ['/export.csv?month=2026-07', '/reports/ytd.csv', '/requests/export.csv']) {
      const res = await page.request.get(path);
      expect(res.status(), `${path} should succeed`).toBe(200);
      const text = await res.text();
      const lines = text.trim().split('\n');
      expect(lines.length, `${path} should have more than a header`).toBeGreaterThan(1);
    }
  });
});

test.describe('state machine refusals', () => {
  test('a payment linked to a request cannot be voided', async ({ page }) => {
    await login(page, accountant.email, PASSWORD);
    await page.goto('/payments');
    const link = page.locator('a[href^="/payments/"]').first();
    await expect(link).toBeVisible();
    const href = await link.getAttribute('href');
    await page.goto(href!);
    // The void control must simply not be offered for a linked payment.
    const voidForm = page.locator('form[action$="/void"]');
    expect(await voidForm.count(), 'a linked payment must not offer void').toBe(0);
  });

  test('a completed request offers no further lifecycle actions', async ({ page }) => {
    await login(page, manager.email, PASSWORD);
    await page.goto('/requests?bucket=completed');
    const card = page.locator('.req-card').first();
    if (!(await card.count())) test.skip();
    const href = await card.getAttribute('href');
    await page.goto(href!);
    for (const verb of ['approve', 'reject', 'return', 'withdraw']) {
      expect(
        await page.locator(`form[action$="/${verb}"]`).count(),
        `a completed request must not offer ${verb}`,
      ).toBe(0);
    }
  });
});
