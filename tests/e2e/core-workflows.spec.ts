import AxeBuilder from '@axe-core/playwright';
import { test, expect, capturePageErrors, createDataEntryUser, createPayment, login } from './fixtures';

test.describe('authentication, permissions, and navigation', () => {
  test('admin can log in, traverse primary navigation, and log out without browser errors', async ({ adminPage }) => {
    const errors = capturePageErrors(adminPage);
    // Login lands on Home, which is the dashboard. The variance grid has its
    // own /grid entry and is reached through the nav below.
    await expect(adminPage.locator('h1')).toBeVisible();
    // The two devices navigate through different chrome: the sidebar on
    // desktop, the More sheet on a phone. Driving whichever is actually on
    // screen keeps this a real navigation test on both.
    const mobile = adminPage.viewportSize()!.width <= 860;
    const openNav = async () => {
      if (mobile) await adminPage.locator('.tabbar .js-more').click();
    };
    const navLink = (href: string) =>
      adminPage.locator(mobile ? `.more-sheet .ms-list a[href="${href}"]` : `.side-nav a[href="${href}"]`);

    for (const href of ['/payments', '/months', '/budgets', '/reports/monthly', '/projects', '/heads', '/users', '/audit', '/backups']) {
      await openNav();
      await navLink(href).click();
      await expect(adminPage.locator('h1')).toBeVisible();
    }
    await openNav();
    await adminPage.getByRole('button', { name: 'Log out' }).click();
    await expect(adminPage).toHaveURL(/\/login$/);
    expect(errors).toEqual([]);
  });

  test('exactly one navigation chrome is on screen, and the tab bar never covers a control', async ({ adminPage }) => {
    await adminPage.goto('/grid?month=2026-06');
    const mobile = adminPage.viewportSize()!.width <= 860;

    // Sidebar and tab bar are alternatives, never both: two visible "Primary"
    // navs is a landmark-uniqueness violation and half a viewport of waste.
    await expect(adminPage.locator('.sidebar')).toBeVisible({ visible: !mobile });
    await expect(adminPage.locator('.tabbar')).toBeVisible({ visible: mobile });
    await expect(adminPage.locator('.m-topbar')).toBeVisible({ visible: mobile });

    if (!mobile) return;

    // Scrolling a mid-page control into view must not park it under the fixed
    // bar. Clicking is the assertion: Playwright fails the click if anything
    // intercepts the pointer.
    const exportLink = adminPage.getByRole('link', { name: /Export/ });
    const download = adminPage.waitForEvent('download');
    await exportLink.click();
    expect(await download).toBeTruthy();
  });

  test('data-entry accounts are denied administrative routes and controls', async ({ page, runId }) => {
    await login(page, 'admin@fervid.local', 'admin123');
    const user = await createDataEntryUser(page, runId);
    await login(page, user.email, user.password);
    await page.goto('/users');
    await expect(page).toHaveURL(/\/users$/);
    await expect(page.getByRole('alert')).toContainText(/forbidden|permission/i);
  });

  test('invalid credentials remain on login and do not authenticate', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('admin@fervid.local');
    await page.getByLabel('Password').fill('wrong-password');
    await page.getByRole('button', { name: 'Login' }).click();
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByText(/invalid/i)).toBeVisible();
  });
});

test.describe('payments, budgets, locks, exports, and accessibility', () => {
  test('admin can create, filter, view, edit, void, and export a payment', async ({ adminPage, runId }) => {
    const errors = capturePageErrors(adminPage);
    await createPayment(adminPage, runId);
    await adminPage.goto('/payments?month=2026-06');
    await adminPage.getByLabel('Search').fill(`Vendor ${runId}`);
    await adminPage.getByRole('button', { name: 'Filter' }).click();
    await expect(adminPage.getByText(`Vendor ${runId}`)).toBeVisible();
    await adminPage.getByRole('link', { name: 'View' }).click();
    await expect(adminPage.getByRole('heading', { name: /Payment #/ })).toBeVisible();
    await adminPage.getByRole('link', { name: 'Edit' }).click();
    await adminPage.getByLabel('Remarks').fill(`edited ${runId}`);
    await adminPage.getByRole('button', { name: 'Save Payment' }).click();
    await expect(adminPage.locator('dd', { hasText: `edited ${runId}` })).toBeVisible();
    const download = adminPage.waitForEvent('download');
    await adminPage.goto('/grid?month=2026-06');
    await adminPage.getByRole('link', { name: /Export/ }).click();
    await expect(await download).toBeTruthy();
    expect(errors).toEqual([]);
  });

  test('payment validation preserves user input after malformed amount', async ({ adminPage, runId }) => {
    await adminPage.goto('/payments/new?month=2026-06');
    await adminPage.getByLabel('Project / Head').selectOption({ index: 1 });
    await adminPage.getByLabel('Paid on').fill('2026-06-15');
    await adminPage.getByLabel('Amount').fill('not-money');
    await adminPage.getByLabel('Vendor / Payee').fill(`Invalid ${runId}`);
    await adminPage.getByRole('button', { name: 'Save Payment' }).click();
    await expect(adminPage.getByRole('alert')).toContainText(/invalid amount/i);
    await expect(adminPage.getByLabel('Vendor / Payee')).toHaveValue(`Invalid ${runId}`);
  });

  test('the primary grid is keyboard reachable and has no detectable axe violations', async ({ adminPage }) => {
    await adminPage.goto('/grid?month=2026-06');
    await adminPage.keyboard.press('Tab');
    await expect(adminPage.locator(':focus')).toBeVisible();
    const report = await new AxeBuilder({ page: adminPage }).analyze();
    expect(report.violations).toEqual([]);
  });
});
