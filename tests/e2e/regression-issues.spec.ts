import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname } from 'node:path';
import type { Page, TestInfo } from '@playwright/test';
import {
  test,
  expect,
  createApprovedRequest,
  createDataEntryUser,
  login,
  openNewUserSheet,
  settlePayment
} from './fixtures';

test.beforeEach(async ({}, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium', 'Issue regressions run once; mobile coverage has dedicated smoke tests.');
});

async function csrf(page: import('@playwright/test').Page) {
  return page.locator('input[name="csrf"]').first().inputValue();
}

/**
 * Reserves an approved request and lands on its payment entry screen.
 *
 * fixtures' settlePayment walks the whole journey; the tests below that stop
 * half-way — on the screen itself, before anything is written — take these two
 * steps and no more. Reserving is a POST, so the queue's control is a submit
 * button rather than a link.
 */
async function openPaymentEntry(page: Page, requestId: number) {
  await page.goto('/accounts-queue?tab=approved');
  const row = page.locator('tr').filter({ has: page.locator(`a[href="/requests/${requestId}"]`) });
  await row.getByRole('button', { name: 'Take for processing' }).click();
  await expect(page).toHaveURL(new RegExp(`/payments/new\\?request=${requestId}$`));
}

/**
 * Fills the five fields the accountant still owns.
 *
 * Project/Head, Payee and Invoice are hidden inputs copied from the approved
 * request — the entry screen has no controls for them, and a payment may not
 * disagree with the request it settles.
 */
async function fillPaymentEntry(
  page: Page,
  values: { amount: string; paidOn: string; mode?: string; reference?: string; note?: string }
) {
  // initMoneyFields stamps data-money-bound on the field once it has taken it
  // over. Filling before that boot lands in an unguarded input, which is a
  // different field from the one a person types into.
  const amount = page.getByLabel('Amount actually paid');
  await expect(amount).toHaveAttribute('data-money-bound', '1');
  await amount.fill(values.amount);
  await page.getByLabel('Paid on').fill(values.paidOn);
  await page.getByLabel('Payment mode').selectOption(values.mode ?? 'bank_transfer');
  await page.getByLabel('Transaction / UTR reference').fill(values.reference ?? 'UTR-REGRESSION');
  if (values.note) await page.getByLabel('Processing note').fill(values.note);
}

/**
 * Writes a proof file into this test's own output directory and returns its
 * path, because settlePayment takes a path rather than bytes.
 *
 * Never output/playwright/baseline/ — that is the frozen pre-redesign record.
 * testInfo.outputPath is under outputDir (output/playwright/test-results).
 */
function proofFile(testInfo: TestInfo, name: string, body: string) {
  const path = testInfo.outputPath(name);
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, body);
  return path;
}

async function addProject(page: import('@playwright/test').Page, name: string, active = true) {
  await page.goto('/projects');
  await page.getByLabel('Project name').fill(name);
  const checkbox = page.locator('form.setup-form input[name="active"]');
  if (!active) await checkbox.uncheck();
  await page.getByRole('button', { name: 'Add Project' }).click();
}

async function activeCheckboxFor(
  page: import('@playwright/test').Page,
  fieldName: string,
  value: string
) {
  const formId = await page.locator(`tbody input[name="${fieldName}"]`).evaluateAll(
    (elements, expected) =>
      (elements as HTMLInputElement[]).find(element => element.value === expected)?.getAttribute('form'),
    value
  );
  expect(formId).toBeTruthy();
  return page.locator(`input[form="${formId}"][name="active"]`);
}

test.describe('documented issue regression guards', () => {
  test('ISS-001 rejects malformed budget rows atomically and preserves input', async ({ adminPage }) => {
    await adminPage.goto('/budgets?month=2026-07');
    const inputs = adminPage.locator('input[name^="budget_"]');
    const original = await inputs.nth(0).inputValue();
    await inputs.nth(0).fill('123.45');
    await inputs.nth(1).fill('bad-value');
    await adminPage.getByRole('button', { name: 'Save Budgets' }).click();
    await expect(adminPage.getByRole('alert')).toContainText(/invalid budget/i);
    await expect(inputs.nth(0)).toHaveValue('123.45');
    await expect(inputs.nth(1)).toHaveValue('bad-value');
    await adminPage.goto('/budgets?month=2026-07');
    await expect(adminPage.locator('input[name^="budget_"]').nth(0)).toHaveValue(original);
  });

  test('ISS-002 keeps month-close counts independent of grid filters', async ({ adminPage }) => {
    await adminPage.goto('/grid?month=2026-07');
    const baseline = await adminPage.getByText(/heads are unpaid.*over budget/i).textContent();
    await adminPage.goto('/grid?month=2026-07&q=does-not-exist');
    await expect(adminPage.getByText(/heads are unpaid.*over budget/i)).toHaveText(baseline ?? '');
  });

  test('ISS-003 labels spend without a budget as unbudgeted', async ({ adminPage, runId }) => {
    // Operations / Office Rent is budgeted for 2026-06 alone, so a payment
    // recorded into any other month is spend against no budget at all.
    const request = await createApprovedRequest(adminPage, runId, { amount: '20.00' });
    await settlePayment(adminPage, request.id, { amount: '20.00', paidOn: '2027-09-15' });
    await adminPage.goto('/grid?month=2027-09&status=unbudgeted');
    await expect(adminPage.locator('.pill.unbudgeted', { hasText: 'Unbudgeted spend' }).first()).toBeVisible();
  });

  // Split from one assertion into three, because the surfaces moved.
  //
  // The store still refuses a locked month — validatePayment returns
  // ErrLockedMonth and RecordPaymentForRequest calls it — but the entry screen
  // cannot warn on load the way the old free-entry form did: the accountant
  // chooses paid_on ON the form, so which month is being written is not known
  // until submit. The refusal therefore has to be met at the confirmation, and
  // it has to be met in words rather than on an error page.
  //
  // The "read-only form, disabled Save" half belongs to payment_edit_form,
  // which a linked payment can no longer reach at all: /payments/{id}/edit is a
  // 303 back to the payment (S12 — it is immutable, locked month or not), and
  // no route in this product creates an unlinked payment to reach it with. The
  // redirect is asserted instead of a form this phase put out of reach.
  test('ISS-004 makes locked-month payment surfaces read-only', async ({ adminPage, runId }) => {
    const month = '2027-10';
    const settled = await createApprovedRequest(adminPage, runId, { amount: '30.00' });
    const blocked = await createApprovedRequest(adminPage, runId, { amount: '40.00' });

    // The e2e server can outlive a run and keep its database, so a month this
    // test locked last time is still locked. Reopen it before recording into it.
    await adminPage.goto(`/grid?month=${month}`);
    const unlock = adminPage.getByRole('button', { name: 'Unlock Month' });
    if ((await unlock.count()) > 0) {
      await adminPage.getByLabel('Unlock reason').fill('Regression reopen');
      adminPage.once('dialog', dialog => dialog.accept());
      await unlock.click();
      await expect(adminPage).toHaveURL(new RegExp(`month=${month}$`));
    }

    const paymentPath = await settlePayment(adminPage, settled.id, { amount: '30.00', paidOn: `${month}-15` });

    await adminPage.goto(`/grid?month=${month}`);
    await adminPage.getByLabel('Lock reason').fill('Regression close');
    adminPage.once('dialog', dialog => dialog.accept());
    await adminPage.getByRole('button', { name: 'Lock Month' }).click();
    // lockMonth redirects to /?month=... (app.go:971), which is the dashboard and
    // shows nothing about the month. The lock notice lives on the grid, so read
    // it there rather than wherever the redirect happens to land.
    await adminPage.goto(`/grid?month=${month}`);
    await expect(adminPage.locator('.locked')).toContainText(`Month ${month} is locked`);

    // Detail side, unchanged in spirit: nothing that would alter the payment.
    await adminPage.goto(paymentPath);
    await expect(adminPage.getByRole('link', { name: 'Edit' })).toHaveCount(0);
    await expect(adminPage.getByRole('button', { name: 'Upload' })).toHaveCount(0);
    await adminPage.goto(`${paymentPath}/edit`);
    await expect(adminPage).toHaveURL(new RegExp(`${paymentPath}$`));

    // Recording INTO the locked month is refused where the accountant is
    // standing, with the reason spelled out and nothing saved.
    await openPaymentEntry(adminPage, blocked.id);
    await fillPaymentEntry(adminPage, { amount: '40.00', paidOn: `${month}-20`, reference: `UTR-${runId}` });
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    const sheet = adminPage.locator('.overlay .sheet');
    await expect(sheet).toBeVisible();
    await sheet.locator('input[name="settlement"][value="settled"]').check();
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();
    await expect(adminPage.getByRole('alert')).toContainText(/month is locked/i);
    await expect(adminPage.locator('.sheet .banner.bad')).toContainText(/month is locked/i);
    await expect(adminPage.locator('.error-state')).toHaveCount(0);
    await expect(adminPage).not.toHaveURL(/\/payments\/\d+$/);
  });

  // The original attached the file on /payments/{id}/edit. A linked payment has
  // no edit screen — the store refuses the write (S12) and the route redirects
  // — so the one moment a payment can be given its proof is while it is being
  // recorded. The guard moves to that moment; it does not disappear.
  test('ISS-005 stores an attachment selected while the payment is recorded', async ({ adminPage, runId }, testInfo) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '50.00' });
    const proof = proofFile(testInfo, 'recorded-proof.txt', 'recorded proof');
    await settlePayment(adminPage, request.id, {
      amount: '50.00',
      paidOn: '2027-11-15',
      attachment: proof
    });
    const file = adminPage.locator('.file-row', { hasText: 'recorded-proof.txt' });
    await expect(file).toBeVisible();
    await expect(file.getByRole('link', { name: 'Download' })).toBeVisible();
  });

  // The list of fields shrank, deliberately. Project/Head, Payee and Invoice
  // are hidden inputs copied from the approved request, so asserting they
  // "survive" would assert nothing a person typed. What is left is the five
  // fields the entry screen still owns.
  //
  // 'abc' is no longer a way in either: #amount sits in a .money-field and
  // fervid-app.js strips everything but digits as it is typed, so a malformed
  // amount cannot leave this screen. Paying more than was approved can — the
  // client only warns, G13 is the store's rule — and that is the rejection this
  // now drives. core-workflows covers the same refusal from the journey's side;
  // this one is the documented issue's own guard: what was typed comes back.
  test('ISS-006 retains every payment field after a rejected amount', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '5000.00' });
    await openPaymentEntry(adminPage, request.id);
    await fillPaymentEntry(adminPage, {
      amount: '6000.00',
      paidOn: '2027-12-19',
      mode: 'bank_transfer',
      reference: `REF-${runId}`,
      note: `Remark ${runId}`
    });
    await expect(adminPage.locator('#diff-banner')).toHaveClass(/bad/);
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    const sheet = adminPage.locator('.overlay .sheet');
    await expect(sheet).toBeVisible();
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();

    await expect(adminPage.getByRole('alert')).toContainText(/more than the approved/i);
    // Every value comes back on the confirmation, both as the fields the next
    // attempt will post and as the figures the accountant can read.
    const carried = adminPage.locator('form[action="/payments"]');
    await expect(carried.locator('input[name="amount"]')).toHaveValue(/^6,?000\.00$/);
    await expect(carried.locator('input[name="paid_on"]')).toHaveValue('2027-12-19');
    await expect(carried.locator('input[name="payment_mode"]')).toHaveValue('bank_transfer');
    await expect(carried.locator('input[name="reference_no"]')).toHaveValue(`REF-${runId}`);
    await expect(carried.locator('input[name="remarks"]')).toHaveValue(`Remark ${runId}`);
    const readable = adminPage.locator('.card .dl');
    await expect(readable).toContainText('2027-12-19');
    await expect(readable).toContainText('Bank transfer');
    await expect(readable).toContainText(`REF-${runId}`);
    await expect(readable).toContainText(`Remark ${runId}`);
  });

  test('ISS-007 hides administrator navigation and payment mutations from data entry', async ({ page, runId }) => {
    await login(page, 'admin@fervid.local', 'admin123');
    const account = await createDataEntryUser(page, runId);
    await login(page, account.email, account.password);
    await expect(page.locator('.side-nav a[href="/users"]')).toHaveCount(0);
    await expect(page.locator('.side-nav a[href="/budgets"]')).toHaveCount(0);
    const response = await page.goto('/users');
    expect(response?.status()).toBe(403);
  });

  test('ISS-008 respects unchecked Active when creating setup records', async ({ adminPage, runId }) => {
    const project = `Inactive ${runId}`;
    await addProject(adminPage, project, false);
    await expect(await activeCheckboxFor(adminPage, 'name', project)).not.toBeChecked();

    await adminPage.goto('/users');
    const email = `inactive-${runId}@example.test`;
    await openNewUserSheet(adminPage);
    const sheet = adminPage.locator('#user-new');
    await sheet.getByLabel('Email').fill(email);
    await sheet.getByLabel('Name').fill(`Inactive User ${runId}`);
    await sheet.getByLabel('Password').fill('InactivePassword42');
    await sheet.locator('form.setup-form input[name="active"]').uncheck();
    await sheet.getByRole('button', { name: 'Add User' }).click();
    // The users table restacked into t-cards, so the state reads off the row's
    // Status cell rather than an inline per-row checkbox.
    await expect(
      adminPage.locator('tr', { hasText: email }).locator('td[data-label="Status"]')
    ).toHaveText('Inactive');
  });

  test('ISS-009 rejects trivially weak account passwords', async ({ adminPage, runId }) => {
    await adminPage.goto('/users');
    const response = await adminPage.request.post('/users', {
      form: {
        csrf: await csrf(adminPage),
        email: `weak-${runId}@example.test`,
        name: 'Weak Password',
        role: 'data_entry',
        password: 'x',
        active: 'on'
      }
    });
    expect(response.status()).toBe(400);
    expect(await response.text()).toContain('password must be at least');
  });

  test('ISS-010 temporarily locks repeated failed sign-ins', async ({ page, runId }) => {
    await login(page, 'admin@fervid.local', 'admin123');
    const email = `lock-${runId}@example.test`;
    await page.goto('/users');
    await openNewUserSheet(page);
    const sheet = page.locator('#user-new');
    await sheet.getByLabel('Email').fill(email);
    await sheet.getByLabel('Name').fill('Lock Test');
    await sheet.getByLabel('Role').selectOption('admin');
    await sheet.getByLabel('Password').fill('LockPassword42');
    await sheet.getByRole('button', { name: 'Add User' }).click();
    for (let attempt = 0; attempt < 5; attempt += 1) {
      await page.goto('/login');
      await page.getByLabel('Email').fill(email);
      await page.getByLabel('Password').fill('WrongPassword42');
      await page.getByRole('button', { name: 'Login' }).click();
    }
    await page.goto('/login');
    await page.getByLabel('Email').fill(email);
    await page.getByLabel('Password').fill('LockPassword42');
    await page.getByRole('button', { name: 'Login' }).click();
    await expect(page.getByRole('alert')).toContainText(/too many failed attempts/i);
  });

  test('ISS-011 records failed sign-ins in the audit log', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('unknown-audit@example.test');
    await page.getByLabel('Password').fill('Incorrect42');
    await page.getByRole('button', { name: 'Login' }).click();
    await login(page, 'admin@fervid.local', 'admin123');
    await page.goto('/audit?action=login_failed');
    await expect(page.getByText('Failed login attempt').first()).toBeVisible();
  });

  test('ISS-012 audits new setup records as Created', async ({ adminPage, runId }) => {
    const project = `Audit Project ${runId}`;
    await addProject(adminPage, project);
    await adminPage.goto('/audit?entity=project&action=create');
    await expect(adminPage.getByText(`Created project ${project}`)).toBeVisible();
  });

  test('ISS-013 exposes filters for every recorded entity and action', async ({ adminPage }) => {
    await adminPage.goto('/audit');
    for (const value of ['payment', 'budget', 'budget_month', 'month_lock', 'project', 'head', 'user', 'report', 'backup']) {
      await expect(adminPage.locator(`select[name="entity"] option[value="${value}"]`)).toHaveCount(1);
    }
    for (const value of ['create', 'attach', 'update', 'void', 'lock', 'unlock', 'export', 'login', 'login_failed', 'logout']) {
      await expect(adminPage.locator(`select[name="action"] option[value="${value}"]`)).toHaveCount(1);
    }
  });

  test('ISS-014 normalizes malformed month parameters consistently', async ({ adminPage }) => {
    for (const path of ['/grid?month=bad', '/payments?month=bad', '/budgets?month=bad']) {
      await adminPage.goto(path);
      await expect(adminPage.getByLabel('Month')).toHaveValue(/^\d{4}-\d{2}$/);
    }
  });

  test('ISS-015 normalizes reversed report ranges in output and controls', async ({ adminPage }) => {
    await adminPage.goto('/reports/heads?from=2026-12&to=2026-01');
    await expect(adminPage.getByLabel('From')).toHaveValue('2026-01');
    await expect(adminPage.getByLabel('To')).toHaveValue('2026-12');
    await expect(adminPage.getByText(/2026-01 to 2026-12/)).toBeVisible();
  });

  test('ISS-016 exports the selected report range', async ({ adminPage }) => {
    await adminPage.goto('/reports/heads?from=2026-05&to=2026-06');
    const pending = adminPage.waitForEvent('download');
    await adminPage.getByRole('link', { name: 'Export CSV' }).click();
    const download = await pending;
    expect(download.suggestedFilename()).toBe('report-2026-05-to-2026-06.csv');
  });

  test('ISS-017 scopes recent payments to the selected month', async ({ adminPage, runId }) => {
    // One request, one payment: two months now means two approved requests,
    // each reserved and settled in its own month.
    const january = await createApprovedRequest(adminPage, runId, { amount: '11.11' });
    const february = await createApprovedRequest(adminPage, runId, { amount: '22.22' });
    const janPath = await settlePayment(adminPage, january.id, { amount: '11.11', paidOn: '2028-01-15' });
    const febPath = await settlePayment(adminPage, february.id, { amount: '22.22', paidOn: '2028-02-15' });
    await adminPage.goto('/grid?month=2028-01');
    // Keyed on each payment's own link rather than its payee: a request raised
    // through the real form leaves payments.vendor_payee empty (payment_form
    // posts .Request2.VendorPayee, which only reimbursements fill), so the
    // Payee column is blank for every payment this journey produces.
    await expect(adminPage.locator(`a[href="${janPath}"]`)).toBeVisible();
    await expect(adminPage.locator(`a[href="${febPath}"]`)).toHaveCount(0);
  });

  test('ISS-018 renders empty future head reports without all-zero rows', async ({ adminPage }) => {
    await adminPage.goto('/reports/heads?from=2099-01&to=2099-01');
    await expect(adminPage.getByText('No report rows.')).toBeVisible();
    await expect(adminPage.locator('tbody tr')).toHaveCount(1);
  });

  test('ISS-019 rejects copying from a nonexistent source month', async ({ adminPage }) => {
    await adminPage.goto('/months');
    await adminPage.getByLabel('New month').fill('2030-02');
    await adminPage.getByLabel('Plan type').selectOption('copy');
    await adminPage.getByLabel('Source month').fill('2030-01');
    await adminPage.getByRole('button', { name: 'Create Month' }).click();
    await expect(adminPage.getByRole('alert')).toContainText(/source month has no budgets/i);
  });

  test('ISS-020 validates due days in HTML and on the server', async ({ adminPage, runId }) => {
    await adminPage.goto('/heads');
    const due = adminPage.getByLabel('Due day').first();
    await expect(due).toHaveAttribute('min', '1');
    await expect(due).toHaveAttribute('max', '31');
    const project = await adminPage.locator('select[name="project_id"] option').first().getAttribute('value');
    const response = await adminPage.request.post('/heads', {
      form: {
        csrf: await csrf(adminPage),
        project_id: project ?? '1',
        name: `Bad Due ${runId}`,
        due_day: '32',
        active: 'on',
        sort_order: '0'
      }
    });
    expect(response.status()).toBe(400);
  });

  test('ISS-021 rejects case-only duplicate project names', async ({ adminPage, runId }) => {
    const name = `Case Project ${runId}`;
    await addProject(adminPage, name);
    const response = await adminPage.request.post('/projects', {
      form: { csrf: await csrf(adminPage), name: name.toLowerCase(), active: 'on', sort_order: '0' }
    });
    expect(response.status()).toBe(400);
    expect(await response.text()).toContain('already exists');
  });

  test('ISS-022 rejects whitespace-only user names', async ({ adminPage, runId }) => {
    await adminPage.goto('/users');
    const response = await adminPage.request.post('/users', {
      form: {
        csrf: await csrf(adminPage),
        email: `blank-${runId}@example.test`,
        name: '   ',
        role: 'data_entry',
        password: 'BlankPassword42',
        active: 'on'
      }
    });
    expect(response.status()).toBe(400);
  });

  test('ISS-023 downloads authenticated attachment bytes', async ({ adminPage, runId }, testInfo) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '10.00' });
    const proof = proofFile(testInfo, 'download-proof.txt', 'download proof');
    await settlePayment(adminPage, request.id, {
      amount: '10.00',
      paidOn: '2028-03-15',
      attachment: proof
    });
    // The proof list is .file-row with its own Download link; the old detail
    // screen's bare filename link belongs to a payment with no request behind
    // it, and this product no longer creates one.
    const file = adminPage.locator('.file-row', { hasText: 'download-proof.txt' });
    await expect(file).toBeVisible();
    const pending = adminPage.waitForEvent('download');
    await file.getByRole('link', { name: 'Download' }).click();
    expect((await pending).suggestedFilename()).toBe('download-proof.txt');
  });

  // The upload moved with the rest of payment entry: a linked payment is
  // immutable, so its detail screen offers no upload form, and the advice is
  // attached while the payment is being recorded instead. What ISS-024 actually
  // guards — that attaching a file is written into the trail and not silently
  // absorbed — is unchanged, and is asserted on the trail that now spans the
  // request and the payment together.
  test('ISS-024 includes attachment uploads in the payment trail', async ({ adminPage, runId }, testInfo) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '10.00' });
    const proof = proofFile(testInfo, 'timeline-proof.txt', 'timeline');
    await settlePayment(adminPage, request.id, {
      amount: '10.00',
      paidOn: '2028-04-15',
      attachment: proof
    });
    const trail = adminPage.locator('ol.thread');
    await expect(trail).toContainText('Uploaded attachment timeline-proof.txt');
    await expect(trail).toContainText('attached proof of payment');
  });

  // Voiding has no user-reachable subject any more, so this is re-scoped rather
  // than repaired — deleting it would lose the record.
  //
  // Which payment could it void? Neither kind. A linked payment is refused by
  // the store (VoidPayment: "a payment linked to a request cannot be voided",
  // S12 — voiding would leave the request completed with nothing paid). A
  // historical, request-less payment can still be voided, but nothing can
  // create one: POST /payments answers 400 without a request_id, and the seed
  // makes projects, heads and budgets only. So the guarantee ISS-025 was
  // written for — no mutation control is offered where mutation is refused —
  // is asserted on the payment that does exist, screen and route alike.
  test('ISS-025 offers no attachment or void mutation on a payment that cannot be mutated', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '60.00' });
    const paymentPath = await settlePayment(adminPage, request.id, { amount: '60.00', paidOn: '2028-05-15' });

    await expect(adminPage.locator('.card-head .pill', { hasText: 'Read-only' })).toBeVisible();
    await expect(adminPage.getByRole('button', { name: 'Upload' })).toHaveCount(0);
    await expect(adminPage.getByRole('button', { name: 'Void' })).toHaveCount(0);
    await expect(adminPage.getByRole('link', { name: 'Edit' })).toHaveCount(0);
    await expect(adminPage.locator('input[name="attachment"]')).toHaveCount(0);

    // The ledger row agrees: View, and no Remove disclosure to open.
    await adminPage.goto('/payments?month=2028-05');
    const row = adminPage.locator('tr').filter({ has: adminPage.locator(`a[href="${paymentPath}"]`) });
    await expect(row.getByRole('link', { name: 'View' })).toBeVisible();
    await expect(row.getByRole('link', { name: 'Edit' })).toHaveCount(0);
    await expect(row.locator('details.inline-danger')).toHaveCount(0);

    // And the route itself, in case a control is ever put back by accident. The
    // CSRF token is read from its cookie because this screen carries no form to
    // read it from — that is the whole point of the test.
    const token = (await adminPage.context().cookies()).find(cookie => cookie.name === 'fervid_csrf')?.value ?? '';
    expect(token).not.toBe('');
    const refused = await adminPage.request.post(`${paymentPath}/void`, {
      form: { csrf: token, reason: 'Regression void' }
    });
    expect(refused.status()).toBe(400);
    expect(await refused.text()).toContain('cannot be voided');
  });

  test('ISS-026 treats percent and underscore as literal search characters', async ({ adminPage, runId }) => {
    const month = '2028-06';
    const literal = `Literal%_${runId}`;
    // The payee is the request's, not the payment's — it is a hidden input
    // copied from the approved request, and a vendor request stores it as
    // vendor_id. That snapshot column is empty on every payment this journey
    // produces, so the same literal is also given to the reference, which the
    // ledger does carry and does search. Both halves exercise the same
    // escaping: SearchVendors finds the vendor by its literal name, and
    // ListPayments must match "%_" as two characters.
    const marked = await createApprovedRequest(adminPage, runId, { amount: '10.00', payee: literal });
    const plain = await createApprovedRequest(adminPage, runId, { amount: '10.00' });
    const markedPath = await settlePayment(adminPage, marked.id, {
      amount: '10.00',
      paidOn: `${month}-15`,
      reference: literal
    });
    const plainPath = await settlePayment(adminPage, plain.id, {
      amount: '10.00',
      paidOn: `${month}-16`,
      reference: `REF-${runId}`
    });
    await adminPage.goto(`/payments?month=${month}&q=%25_`);
    // A ledger row links to its payment twice — the Project / Head cell and the
    // View button (templates.go:190) — so assert on the row, not on the href.
    const markedRow = adminPage.locator('tr').filter({ has: adminPage.locator(`a[href="${markedPath}"]`) });
    await expect(markedRow).toHaveCount(1);
    await expect(markedRow).toBeVisible();
    await expect(adminPage.getByText(literal).first()).toBeVisible();
    // The row without the literal is what proves the escaping: unescaped, "%_"
    // is a wildcard that matches every payment in the month, including this one.
    await expect(adminPage.locator(`a[href="${plainPath}"]`)).toHaveCount(0);
  });

  test('ISS-027 contains wide tables without page-level mobile overflow', async ({ adminPage }) => {
    await adminPage.setViewportSize({ width: 390, height: 844 });
    for (const path of ['/months', '/projects', '/heads', '/users', '/audit', '/reports/heads?from=2026-07&to=2026-07', '/backups']) {
      await adminPage.goto(path);
      const sizes = await adminPage.evaluate(() => ({
        document: document.documentElement.scrollWidth,
        viewport: window.innerWidth
      }));
      expect(sizes.document).toBeLessThanOrEqual(sizes.viewport);
      await expect(adminPage.locator('.table-wrap')).toBeVisible();
    }
  });

  test('ISS-028 renders human-readable payment mode labels', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '10.00' });
    await settlePayment(adminPage, request.id, {
      amount: '10.00',
      paidOn: '2028-07-15',
      mode: 'bank_transfer'
    });
    await expect(adminPage.locator('dd', { hasText: 'Bank transfer' }).first()).toBeVisible();
  });

  test('ISS-029 gives project subtotal bars their utilization status', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '10.00' });
    await settlePayment(adminPage, request.id, { amount: '10.00', paidOn: '2026-07-10' });
    await adminPage.goto('/grid?month=2026-07');
    await expect(adminPage.locator('tr.project-row .vbar.is-unbudgeted').first()).toBeVisible();
  });

  // Proof of absence, in the spirit of X3's TestNoRefundRoute. "Save and add
  // another" was payment_edit_form's, behind {{if not .Payment.ID}}, and it
  // existed because free-standing entry let one person key in payment after
  // payment. Phase 3 retires that: a payment exists only against the approved
  // request it settles, one request takes exactly one payment (S9, enforced by
  // idx_payments_request), and the request leaves the queue the moment it is
  // paid — so there is nothing to add another of, and no screen from which to
  // add it. The test stays as the record of what went and why.
  test('ISS-030 no longer offers Save and add another, because one request takes one payment', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '10.00' });
    await openPaymentEntry(adminPage, request.id);
    await expect(adminPage.getByRole('button', { name: 'Save and add another' })).toHaveCount(0);
    await expect(adminPage.getByRole('button', { name: 'Save Payment' })).toHaveCount(0);
    // What the screen offers instead: one confirmation, and nothing written
    // before it.
    await expect(adminPage.getByRole('button', { name: /Payment settled/ })).toBeVisible();

    // The other way in is the picker, which is a list of approved requests
    // rather than a form, so there is nothing to save twice there either.
    await adminPage.goto('/payments/new');
    await expect(adminPage.getByRole('heading', { name: 'Which approved request is this for?' })).toBeVisible();
    await expect(adminPage.getByRole('button', { name: 'Save and add another' })).toHaveCount(0);
  });

  test('ISS-031 lists backup history newest first', async ({ adminPage }) => {
    await adminPage.goto('/backups');
    adminPage.on('dialog', dialog => dialog.accept());
    await adminPage.getByRole('button', { name: 'Create Backup' }).click();
    await adminPage.getByRole('button', { name: 'Create Backup' }).click();
    const names = await adminPage.locator('tbody tr td:first-child').allTextContents();
    expect(names).toEqual([...names].sort().reverse());
  });

  test('ISS-032 exposes login errors through an assertive alert', async ({ page }) => {
    await page.goto('/login');
    await page.getByLabel('Email').fill('bad@example.test');
    await page.getByLabel('Password').fill('BadPassword42');
    await page.getByRole('button', { name: 'Login' }).click();
    const alert = page.getByRole('alert');
    await expect(alert).toBeVisible();
    await expect(alert).toHaveAttribute('aria-live', 'assertive');
  });

  test('ISS-033 does not publish default administrator credentials', async ({ page }) => {
    await page.goto('/login');
    await expect(page.locator('body')).not.toContainText('admin123');
    await expect(page.getByLabel('Email')).toHaveValue('');
  });

  test('ISS-034 aligns sticky grid columns without clipping due badges', async ({ adminPage }) => {
    for (const width of [1440, 390]) {
      await adminPage.setViewportSize({ width, height: 1000 });
      await adminPage.goto('/grid?month=2026-07');
      const geometry = await adminPage.locator('table.matrix').evaluate(table => {
        const rect = (selector: string) =>
          (table.querySelector(selector) as HTMLElement).getBoundingClientRect();
        const project = rect('th.project-col');
        const head = rect('th.head-col');
        const due = rect('tbody td.due');
        const pillsContained = [...table.querySelectorAll<HTMLElement>('tbody td.due')].every(cell => {
          const cellRect = cell.getBoundingClientRect();
          const pillRect = cell.querySelector<HTMLElement>('.duepill')!.getBoundingClientRect();
          return pillRect.left >= cellRect.left && pillRect.right <= cellRect.right;
        });
        return {
          projectToHeadGap: head.left - project.right,
          headDueOverlap: head.right - due.left,
          pillsContained
        };
      });
      expect(Math.abs(geometry.projectToHeadGap), `${width}px Project/Head boundary`).toBeLessThanOrEqual(1);
      expect(Math.abs(geometry.headDueOverlap), `${width}px Head/Due boundary`).toBeLessThanOrEqual(1);
      expect(geometry.pillsContained, `${width}px Due badge containment`).toBe(true);
    }
  });
});

test.describe('responsive and visual smoke', () => {
  test('core pages render at desktop and mobile dimensions', async ({ adminPage }, testInfo) => {
    for (const [name, size] of Object.entries({ desktop: { width: 1440, height: 1000 }, mobile: { width: 390, height: 844 } })) {
      await adminPage.setViewportSize(size);
      await adminPage.goto('/grid?month=2026-07');
      await expect(adminPage.getByRole('heading', { name: 'Variance grid' })).toBeVisible();
      await adminPage.screenshot({ path: testInfo.outputPath(`grid-${name}.png`), fullPage: true });
    }
  });

  test('payment upload control accepts a file and retains visual evidence', async ({ adminPage, runId }, testInfo) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '10.00' });
    await openPaymentEntry(adminPage, request.id);
    await fillPaymentEntry(adminPage, { amount: '10.00', paidOn: '2028-09-15', reference: `UTR-${runId}` });
    // The uploader is a hidden <input type="file"> inside <label class="uploader">
    // with no accessible name at all, so getByLabel('Attachment') finds nothing
    // on this screen. setInputFiles works on a hidden input.
    const file = adminPage.locator('input[name="attachment"]');
    await file.setInputFiles({
      name: 'receipt.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from('receipt')
    });
    await expect(file).toHaveValue(/receipt\.txt$/);
    await adminPage.screenshot({ path: testInfo.outputPath('payment-upload-form.png'), fullPage: true });
  });
});
