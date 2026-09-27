import AxeBuilder from '@axe-core/playwright';
import { test, expect, capturePageErrors, createApprovedRequest, createDataEntryUser, login, settlePayment } from './fixtures';

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
    await login(page, 'admin@fervid.local', 'TestAdmin12345');
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
  /**
   * Re-scoped, not shrunk. This test used to read "create, filter, view, edit,
   * void, and export", and all six were one screen's worth of work because a
   * payment was a free-standing row anybody could type in and change again.
   *
   * Phase 3 split those six in half. A payment now exists only as the end of an
   * approved request, and a linked payment is immutable (S12): the store refuses
   * to edit or void one, the ledger row offers nothing but View, and
   * /payments/{id}/edit redirects back to the payment rather than serving a form
   * whose Save can only fail. Edit and Void survive solely for HISTORICAL,
   * request-less rows (X6) — and the shipped UI can no longer create one
   * (CreatePayment is off every HTTP route) and the e2e seed contains none, so
   * there is nothing in this environment to drive them against. Their Go twins
   * own that branch.
   *
   * So the honest subject of this test is the LEDGER: that the journey puts a
   * row there, that the filters find it, that it opens, and that it exports.
   * Edit and void stay in the title's place as a proof-of-absence — the thing
   * this file used to assert worked is now asserted to be gone, on purpose,
   * rather than quietly dropped.
   */
  test('admin can record, filter, view and export a payment — and cannot edit or void it', async ({ adminPage, runId }) => {
    const errors = capturePageErrors(adminPage);
    const request = await createApprovedRequest(adminPage, runId, { amount: '4200.00' });
    // The processing note is the search term: the ledger's search covers
    // project, head, payee, invoice, reference and remarks, and the remarks are
    // unique to this run. The payee is asserted on the detail screen below
    // (F-D-09: it reaches every hop, whatever the old KNOWN GAP comment said).
    const payment = await settlePayment(adminPage, request.id, {
      amount: '4200.00',
      paidOn: '2026-06-15',
      mode: 'upi',
      reference: `UTR-${runId}`,
      remarks: `Settled by e2e ${runId}`
    });

    await adminPage.goto('/payments?month=2026-06');
    await adminPage.getByLabel('Search').fill(runId);
    await adminPage.getByRole('button', { name: 'Filter' }).click();
    // Both halves matter: the filter narrowed the ledger to a single row, and
    // that row is this payment. Either assertion alone would pass on an empty
    // result, because the "No payments match these filters" state is a <tr> too.
    const row = adminPage.locator('tbody tr').filter({ has: adminPage.locator(`a[href="${payment}"]`) });
    await expect(adminPage.locator('tbody tr')).toHaveCount(1);
    await expect(row).toHaveCount(1);
    await expect(row).toContainText('4,200.00');

    // S12 from the ledger's side: View is the only action a linked row offers.
    // The Edit link and the Remove disclosure are gated on `not .RequestID`.
    const actions = row.locator('.actions-cell a');
    await expect(actions).toHaveCount(1);
    await expect(actions).toHaveText('View');
    await expect(row.locator('details.inline-danger')).toHaveCount(0);

    await row.getByRole('link', { name: 'View' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`${payment}$`));
    // The linked detail's h1 is the payee — the vendor named on the request —
    // and the number line ties it back: "PAY-{id} · from PR-YYYY-NNNNNN".
    await expect(adminPage.locator('h1')).toContainText(`Payee ${runId}`);
    await expect(adminPage.locator('.rh-no')).toContainText(request.number);
    await expect(adminPage.locator('.rh-amt')).toContainText('4,200.00');
    await expect(adminPage.locator('.compare .cmp-row.match')).toBeVisible();
    await expect(adminPage.getByRole('link', { name: /Edit/ })).toHaveCount(0);
    await expect(adminPage.getByRole('button', { name: /Void/ })).toHaveCount(0);

    // And the URL itself is closed, not merely unlinked: a 303 back to the
    // payment, so no edit form is ever served for a settled payment.
    await adminPage.goto(`${payment}/edit`);
    await expect(adminPage).toHaveURL(new RegExp(`${payment}$`));
    await expect(adminPage.getByRole('button', { name: 'Save Payment' })).toHaveCount(0);

    const download = adminPage.waitForEvent('download');
    await adminPage.goto('/grid?month=2026-06');
    await adminPage.getByRole('link', { name: /Export/ }).click();
    expect(await download).toBeTruthy();
    expect(errors).toEqual([]);
  });

  test('payment validation retains every typed value and recovers before confirmation', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '5000.00' });
    await adminPage.goto(`/requests/${request.id}`);
    await adminPage.getByRole('button', { name: 'Record payment', exact: true }).click();
    const amount = adminPage.getByLabel('Amount actually paid');
    await amount.fill('not-money');
    await expect(amount).toHaveValue('not-money');
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    await expect(adminPage.locator('.client-error-summary')).toBeVisible();
    await expect(amount).toHaveAttribute('aria-invalid', 'true');
    await amount.fill('6000.00');
    await expect(adminPage.locator('#diff-banner')).toHaveClass(/bad/);
    await adminPage.getByLabel('Paid on').fill('2026-06-15');
    await adminPage.getByLabel('Payment mode').selectOption('neft');
    await adminPage.getByLabel('Transaction / UTR reference').fill(`UTR-${runId}`);
    await adminPage.getByLabel('Processing note').fill(`Retain this note ${runId}`);
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    await expect(adminPage.locator('.client-error-summary')).toContainText('remaining approved balance');
    await expect(adminPage.locator('#settle-sheet')).not.toBeVisible();
    await expect(amount).toHaveValue('6,000.00');
    await expect(adminPage.getByLabel('Paid on')).toHaveValue('2026-06-15');
    await expect(adminPage.getByLabel('Payment mode')).toHaveValue('neft');
    await expect(adminPage.getByLabel('Transaction / UTR reference')).toHaveValue(`UTR-${runId}`);
    await expect(adminPage.getByLabel('Processing note')).toHaveValue(`Retain this note ${runId}`);
    await amount.fill('5000.00');
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    await expect(adminPage.locator('#settle-sheet')).toBeVisible();
    await adminPage.getByRole('button', { name: 'Confirm and save payment' }).click();
    await expect(adminPage).toHaveURL(/\/payments\/\d+$/);
    await expect(adminPage.getByText(`UTR-${runId}`, { exact: true })).toBeVisible();
  });

  test('the primary grid is keyboard reachable and has no detectable axe violations', async ({ adminPage }) => {
    await adminPage.goto('/grid?month=2026-06');
    await adminPage.keyboard.press('Tab');
    await expect(adminPage.locator(':focus')).toBeVisible();
    const report = await new AxeBuilder({ page: adminPage }).analyze();
    expect(report.violations).toEqual([]);
  });
});
