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

  /**
   * Retargeted at the reserved entry screen, where payment entry now lives.
   *
   * The old test typed "not-money" into a plain text Amount on the free-entry
   * form and read the refusal off a role="alert". Two things changed underneath
   * it, and the first is the reason this test cannot simply be pointed at the
   * new URL:
   *
   *  1. "Amount actually paid" is a `.money-field`. fervid-app.js binds every
   *     one of them and rewrites the value on each keystroke, keeping only
   *     digits and a decimal point — so a browser with JavaScript can no longer
   *     put "not-money" into the field at all, let alone post it. The first leg
   *     below pins that guard, because it is what retired the old premise.
   *  2. The rejection this test is really about — "nothing you typed is lost" —
   *     therefore has to come from the server. The refusal that a person can
   *     actually reach is G13: the approved amount is a hard ceiling, and paying
   *     over it is not a settlement decision but a different obligation. The
   *     preview is pure (D8) and enforces nothing, so the store meets it at the
   *     confirm — which is an ordinary form POST, so its 400 is a rendered
   *     confirmation page carrying every figure back.
   *
   * The store's own "invalid amount" is exercised by its Go twin,
   * TestPaymentErrorRetainsInputAndUsesHumanModes, which posts past the field.
   */
  test('a settlement the store refuses comes back with every typed value intact', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '5000.00' });
    await adminPage.goto('/accounts-queue');
    await adminPage.locator('tr', { hasText: request.number })
      .getByRole('button', { name: 'Take for processing' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${request.id}$`));

    // initMoneyFields stamps data-money-bound on each field it takes over.
    // Waiting for it is waiting for the guard to be live: filling before the
    // boot would land in an unguarded input and prove nothing.
    const amount = adminPage.getByLabel('Amount actually paid');
    await expect(amount).toHaveAttribute('data-money-bound', '1');
    await amount.fill('not-money');
    await expect(amount).toHaveValue('');

    // A figure over the approved 5,000.00. The field accepts it and groups it;
    // the banner says the refusal is coming, which is all the client does.
    await amount.fill('6000.00');
    await expect(amount).toHaveValue('6,000.00');
    await expect(adminPage.locator('#diff-banner')).toHaveClass(/bad/);
    await expect(adminPage.locator('#diff-text')).toContainText('more than approved');

    await adminPage.getByLabel('Paid on').fill('2026-06-15');
    await adminPage.getByLabel('Payment mode').selectOption('bank_transfer');
    await adminPage.getByLabel('Transaction / UTR reference').fill(`UTR-${runId}`);
    await adminPage.getByLabel('Processing note').fill(`Retain this note ${runId}`);

    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    await expect(adminPage.locator('.overlay .sheet')).toBeVisible();
    await expect(adminPage.locator('.overlay .sheet input[name="settlement"]:checked')).toHaveCount(0);
    await adminPage.locator('.overlay .sheet input[name="settlement"][value="settled"]').check();
    await adminPage.locator('.overlay .sheet')
      .getByRole('button', { name: 'Confirm and save payment' }).click();

    // No payment was created, so the URL is the collection and not /payments/{id}.
    await expect(adminPage).toHaveURL(/\/payments$/);
    const sheet = adminPage.locator('.overlay .sheet');
    await expect(sheet.locator('.banner.bad')).toContainText('more than the approved');
    await expect(sheet.locator('.banner.bad')).toContainText('Nothing has been saved');

    // Every typed value is posted back, so confirming again costs one correction
    // rather than a retyped form. The money field strips its own grouping on
    // submit, which is why the amount returns as plain digits.
    await expect(adminPage.locator('input[name="amount"]')).toHaveValue('6000.00');
    await expect(adminPage.locator('input[name="paid_on"]')).toHaveValue('2026-06-15');
    await expect(adminPage.locator('input[name="payment_mode"]')).toHaveValue('bank_transfer');
    await expect(adminPage.locator('input[name="reference_no"]')).toHaveValue(`UTR-${runId}`);
    await expect(adminPage.locator('input[name="remarks"]')).toHaveValue(`Retain this note ${runId}`);
    // And the ones a person reads are on the page, spelled for a person.
    await expect(adminPage.locator('.card .dl')).toContainText('Bank transfer');
    await expect(adminPage.locator('.card .dl')).toContainText(`Retain this note ${runId}`);

    // The reservation survived the refusal, so correcting the figure is one
    // click away — the accountant never has to fight for the request again.
    await sheet.getByRole('button', { name: 'Go back' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${request.id}$`));
    await expect(adminPage.locator('.reserve-bar')).toContainText('Reserved by you');
    await expect(adminPage.getByLabel('Amount actually paid')).toHaveValue('6,000.00');
    await expect(adminPage.getByLabel('Paid on')).toHaveValue('2026-06-15');
    await expect(adminPage.getByLabel('Payment mode')).toHaveValue('bank_transfer');
    await expect(adminPage.getByLabel('Transaction / UTR reference')).toHaveValue(`UTR-${runId}`);
    await expect(adminPage.getByLabel('Processing note')).toHaveValue(`Retain this note ${runId}`);
  });

  test('the primary grid is keyboard reachable and has no detectable axe violations', async ({ adminPage }) => {
    await adminPage.goto('/grid?month=2026-06');
    await adminPage.keyboard.press('Tab');
    await expect(adminPage.locator(':focus')).toBeVisible();
    const report = await new AxeBuilder({ page: adminPage }).analyze();
    expect(report.violations).toEqual([]);
  });
});
