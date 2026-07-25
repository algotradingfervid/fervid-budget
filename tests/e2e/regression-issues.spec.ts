import { test, expect, createDataEntryUser, createPayment, login, openNewUserSheet } from './fixtures';

test.beforeEach(async ({}, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium', 'Issue regressions run once; mobile coverage has dedicated smoke tests.');
});

async function csrf(page: import('@playwright/test').Page) {
  return page.locator('input[name="csrf"]').first().inputValue();
}

async function paymentDetailFor(page: import('@playwright/test').Page, vendor: string, month: string) {
  await page.goto(`/payments?month=${month}`);
  await page.getByLabel('Search').fill(vendor);
  await page.getByRole('button', { name: 'Filter' }).click();
  const row = page.locator('tr', { hasText: vendor });
  await row.getByRole('link', { name: 'View' }).click();
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
    await adminPage.goto('/?month=2026-07');
    const baseline = await adminPage.getByText(/heads are unpaid.*over budget/i).textContent();
    await adminPage.goto('/?month=2026-07&q=does-not-exist');
    await expect(adminPage.getByText(/heads are unpaid.*over budget/i)).toHaveText(baseline ?? '');
  });

  test('ISS-003 labels spend without a budget as unbudgeted', async ({ adminPage, runId }) => {
    await createPayment(adminPage, runId, { month: '2027-09', amount: '20.00' });
    await adminPage.goto('/?month=2027-09&status=unbudgeted');
    await expect(adminPage.locator('.pill.unbudgeted', { hasText: 'Unbudgeted spend' }).first()).toBeVisible();
  });

  test('ISS-004 makes locked-month payment surfaces read-only', async ({ adminPage, runId }) => {
    const month = '2027-10';
    await createPayment(adminPage, runId, { month });
    await adminPage.goto(`/?month=${month}`);
    await adminPage.getByLabel('Lock reason').fill('Regression close');
    adminPage.once('dialog', dialog => dialog.accept());
    await adminPage.getByRole('button', { name: 'Lock Month' }).click();
    await adminPage.goto(`/payments/new?month=${month}`);
    await expect(adminPage.getByRole('status')).toContainText(/read-only/i);
    await expect(adminPage.getByRole('button', { name: 'Save Payment' })).toBeDisabled();
    await paymentDetailFor(adminPage, `Vendor ${runId}`, month);
    await expect(adminPage.getByRole('link', { name: 'Edit' })).toHaveCount(0);
    await expect(adminPage.getByRole('button', { name: 'Upload' })).toHaveCount(0);
  });

  test('ISS-005 stores an attachment selected during payment edit', async ({ adminPage, runId }) => {
    const month = '2027-11';
    await createPayment(adminPage, runId, { month });
    await paymentDetailFor(adminPage, `Vendor ${runId}`, month);
    await adminPage.getByRole('link', { name: 'Edit' }).click();
    await adminPage.getByLabel('Attachment').setInputFiles({
      name: 'edited-proof.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from('edited proof')
    });
    await adminPage.getByRole('button', { name: 'Save Payment' }).click();
    await expect(adminPage.getByRole('link', { name: 'edited-proof.txt' })).toBeVisible();
  });

  test('ISS-006 retains every payment field after invalid amount input', async ({ adminPage, runId }) => {
    await adminPage.goto('/payments/new?month=2027-12');
    await adminPage.getByLabel('Project / Head').selectOption({ index: 1 });
    await adminPage.getByLabel('Paid on').fill('2027-12-19');
    await adminPage.getByLabel('Amount').fill('abc');
    await adminPage.getByLabel('Vendor / Payee').fill(`Retain ${runId}`);
    await adminPage.getByLabel('Payment mode').selectOption('bank_transfer');
    await adminPage.getByLabel('Invoice / Bill No').fill(`INV-${runId}`);
    await adminPage.getByLabel('Reference No').fill(`REF-${runId}`);
    await adminPage.getByLabel('Remarks').fill(`Remark ${runId}`);
    await adminPage.getByRole('button', { name: 'Save Payment' }).click();
    await expect(adminPage.getByLabel('Amount')).toHaveValue('abc');
    await expect(adminPage.getByLabel('Paid on')).toHaveValue('2027-12-19');
    await expect(adminPage.getByLabel('Vendor / Payee')).toHaveValue(`Retain ${runId}`);
    await expect(adminPage.getByLabel('Payment mode')).toHaveValue('bank_transfer');
    await expect(adminPage.getByLabel('Invoice / Bill No')).toHaveValue(`INV-${runId}`);
    await expect(adminPage.getByLabel('Reference No')).toHaveValue(`REF-${runId}`);
    await expect(adminPage.getByLabel('Remarks')).toHaveValue(`Remark ${runId}`);
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
    for (const path of ['/?month=bad', '/payments?month=bad', '/budgets?month=bad']) {
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
    await createPayment(adminPage, `${runId}-jan`, { month: '2028-01' });
    await createPayment(adminPage, `${runId}-feb`, { month: '2028-02' });
    await adminPage.goto('/?month=2028-01');
    await expect(adminPage.getByText(`Vendor ${runId}-jan`)).toBeVisible();
    await expect(adminPage.getByText(`Vendor ${runId}-feb`)).toHaveCount(0);
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

  test('ISS-023 downloads authenticated attachment bytes', async ({ adminPage, runId }) => {
    const month = '2028-03';
    await adminPage.goto(`/payments/new?month=${month}`);
    await adminPage.getByLabel('Project / Head').selectOption({ index: 1 });
    await adminPage.getByLabel('Paid on').fill(`${month}-15`);
    await adminPage.getByLabel('Amount').fill('10.00');
    await adminPage.getByLabel('Vendor / Payee').fill(`Download ${runId}`);
    await adminPage.getByLabel('Attachment').setInputFiles({
      name: 'download-proof.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from('download proof')
    });
    await adminPage.getByRole('button', { name: 'Save Payment' }).click();
    await paymentDetailFor(adminPage, `Download ${runId}`, month);
    const pending = adminPage.waitForEvent('download');
    await adminPage.getByRole('link', { name: 'download-proof.txt' }).click();
    expect((await pending).suggestedFilename()).toBe('download-proof.txt');
  });

  test('ISS-024 includes attachment uploads in the payment timeline', async ({ adminPage, runId }) => {
    const month = '2028-04';
    await createPayment(adminPage, runId, { month });
    await paymentDetailFor(adminPage, `Vendor ${runId}`, month);
    await adminPage.getByLabel('Attachment').setInputFiles({
      name: 'timeline-proof.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from('timeline')
    });
    await adminPage.getByRole('button', { name: 'Upload' }).click();
    await expect(adminPage.getByText('Uploaded attachment timeline-proof.txt')).toBeVisible();
    await expect(adminPage.getByText('Attached', { exact: true })).toBeVisible();
  });

  test('ISS-025 removes attachment mutation controls after voiding', async ({ adminPage, runId }) => {
    const month = '2028-05';
    await createPayment(adminPage, runId, { month });
    await paymentDetailFor(adminPage, `Vendor ${runId}`, month);
    await adminPage.getByLabel('Reason').fill('Regression void');
    adminPage.once('dialog', dialog => dialog.accept());
    await adminPage.getByRole('button', { name: 'Void' }).click();
    await expect(adminPage.getByRole('button', { name: 'Upload' })).toHaveCount(0);
    await expect(adminPage.locator('.locked', { hasText: 'Voided payment' })).toBeVisible();
  });

  test('ISS-026 treats percent and underscore as literal search characters', async ({ adminPage, runId }) => {
    const month = '2028-06';
    await adminPage.goto(`/payments/new?month=${month}`);
    await adminPage.getByLabel('Project / Head').selectOption({ index: 1 });
    await adminPage.getByLabel('Paid on').fill(`${month}-15`);
    await adminPage.getByLabel('Amount').fill('10.00');
    await adminPage.getByLabel('Vendor / Payee').fill(`Literal%_${runId}`);
    await adminPage.getByRole('button', { name: 'Save Payment' }).click();
    await adminPage.goto(`/payments?month=${month}&q=%25_`);
    await expect(adminPage.getByText(`Literal%_${runId}`)).toBeVisible();
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
    const month = '2028-07';
    await adminPage.goto(`/payments/new?month=${month}`);
    await adminPage.getByLabel('Project / Head').selectOption({ index: 1 });
    await adminPage.getByLabel('Paid on').fill(`${month}-15`);
    await adminPage.getByLabel('Amount').fill('10.00');
    await adminPage.getByLabel('Vendor / Payee').fill(`Mode ${runId}`);
    await adminPage.getByLabel('Payment mode').selectOption('bank_transfer');
    await adminPage.getByRole('button', { name: 'Save Payment' }).click();
    await paymentDetailFor(adminPage, `Mode ${runId}`, month);
    await expect(adminPage.locator('dd', { hasText: 'Bank transfer' })).toBeVisible();
  });

  test('ISS-029 gives project subtotal bars their utilization status', async ({ adminPage, runId }) => {
    await createPayment(adminPage, runId, { month: '2026-07', amount: '10.00' });
    await adminPage.goto('/?month=2026-07');
    await expect(adminPage.locator('tr.project-row .vbar.is-unbudgeted').first()).toBeVisible();
  });

  test('ISS-030 preserves the selected day after Save and add another', async ({ adminPage, runId }) => {
    await adminPage.goto('/payments/new?month=2028-08');
    await adminPage.getByLabel('Project / Head').selectOption({ index: 1 });
    await adminPage.getByLabel('Paid on').fill('2028-08-19');
    await adminPage.getByLabel('Amount').fill('10.00');
    await adminPage.getByLabel('Vendor / Payee').fill(`Another ${runId}`);
    await adminPage.getByRole('button', { name: 'Save and add another' }).click();
    await expect(adminPage.getByLabel('Paid on')).toHaveValue('2028-08-19');
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
      await adminPage.goto('/?month=2026-07');
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
      await adminPage.goto('/?month=2026-07');
      await expect(adminPage.getByRole('heading', { name: 'Variance grid' })).toBeVisible();
      await adminPage.screenshot({ path: testInfo.outputPath(`grid-${name}.png`), fullPage: true });
    }
  });

  test('payment upload control accepts a file and retains visual evidence', async ({ adminPage, runId }, testInfo) => {
    await adminPage.goto('/payments/new?month=2028-09');
    await adminPage.getByLabel('Project / Head').selectOption({ index: 1 });
    await adminPage.getByLabel('Paid on').fill('2028-09-15');
    await adminPage.getByLabel('Amount').fill('10.00');
    await adminPage.getByLabel('Vendor / Payee').fill(`Upload ${runId}`);
    await adminPage.getByLabel('Attachment').setInputFiles({
      name: 'receipt.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from('receipt')
    });
    await adminPage.screenshot({ path: testInfo.outputPath('payment-upload-form.png'), fullPage: true });
  });
});
