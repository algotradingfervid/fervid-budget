import { mkdirSync } from 'node:fs';
import type { Browser, Page } from '@playwright/test';
import {
  test,
  expect,
  admin,
  createApprovedRequest,
  createApproverUser,
  login,
  openNewUserSheet,
  settlePayment
} from './fixtures';

/*
 * The grid-and-reports repairs (fix wave 2026-09-25), each driven from the
 * screen a person uses. Every test also leaves 1440px and 390px photographs in
 * output/playwright/fixwave-grid/ (ignored by git), which is where the visual
 * half of each fix — a colour, a card, a drawer — was looked at.
 *
 * Budgets and payments here live in 2026-08 and in the current month, which no
 * other default spec reads totals from; 2026-06 is the seeded month and is only
 * filtered, never written.
 */

test.beforeEach(async ({}, testInfo) => {
  test.skip(testInfo.project.name !== 'chromium', 'Each test sets both viewports itself.');
});

const SHOTS = 'output/playwright/fixwave-grid';
mkdirSync(SHOTS, { recursive: true });

const DESKTOP = { width: 1440, height: 900 };
const PHONE = { width: 390, height: 844 };

async function shoot(page: Page, name: string, full = true) {
  await page.screenshot({ path: `${SHOTS}/${name}.png`, fullPage: full });
}

function paise(text: string | null): number {
  const t = (text ?? '').replace(/[₹,\s]/g, '');
  if (!t || t === '—') return 0;
  return Math.round(Number(t) * 100);
}

function today() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

/** Nothing is printed as "Project / Head" with one side missing. */
function assertNoDanglingSlash(text: string, where: string) {
  expect(/(^|\s)\/(\s|$|·)/.test(text.replace(/\S+\s\/\s\S+/g, '')), `${where} renders a dangling "/": ${JSON.stringify(text)}`).toBe(false);
}

async function raiseAndApproveEmd(page: Page, browser: Browser, runId: string, amount: string, expectedReturn: string) {
  const approver = await createApproverUser(page, runId);
  // A deposit is its own request type since the C cluster's form-1 fix: the
  // "Deposit or guarantee" card carries the category picker and asks who is
  // paid, and the employee advance no longer offers the deposit categories.
  await page.goto('/requests/new?type=recoverable');
  await page.getByLabel('Short title').fill(`EMD ${runId}`);
  await page.locator('#rcategory').selectOption('emd');
  // The category change swaps #form-fields from the server; the swap is
  // settled when the option comes back selected server-side.
  await expect(page.locator('#rcategory option[value="emd"]')).toHaveAttribute('selected', '');
  await page.locator('#payee').fill(`Tender authority ${runId}`);
  await page.locator('#rproject').waitFor();
  await page.locator('#rproject').selectOption({ label: 'Operations' });
  await page.getByLabel('Expected return date').fill(expectedReturn);
  await page.getByLabel('Repayment or refund terms').fill('Refunded when the tender is awarded');
  await page.getByLabel('Amount').fill(amount);
  await page.getByLabel('Purpose').fill(`Tender deposit ${runId}`);
  await page.getByLabel('Approver').selectOption({ label: approver.name });
  await page.getByRole('button', { name: 'Submit request' }).click();
  await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);
  const id = Number(new URL(page.url()).pathname.split('/')[2]);

  const context = await browser.newContext();
  try {
    const ap = await context.newPage();
    await login(ap, approver.email, approver.password);
    await ap.goto(`/requests/${id}`);
    await ap.getByRole('button', { name: /^Approve / }).click();
    const sheet = ap.locator('#approve-sheet');
    await sheet.getByLabel('Amount approved').fill(amount);
    await sheet.getByRole('button', { name: 'Approve request' }).click();
    await expect(ap).toHaveURL(/\/approvals$/);
  } finally {
    await context.close();
  }
  return { id, approver };
}

test('recoverables-1/-2, grid-2, grid-1 — a recoverable never reaches the grid, is named not slashed, and is only overdue once paid', async ({
  adminPage,
  browser,
  runId
}) => {
  const amount = '250000';
  const { id, approver } = await raiseAndApproveEmd(adminPage, browser, runId, amount, '2026-01-15');

  // recoverables-1: approved, unpaid, past its return date. It is outstanding,
  // but money that never left is not overdue — in the category rows, the Total
  // row and the tile alike.
  for (const [vp, tag] of [[DESKTOP, '1440'], [PHONE, '390']] as const) {
    await adminPage.setViewportSize(vp);
    await adminPage.goto('/recoverables');
    const tile = adminPage.locator('.metric', { hasText: 'Past expected return' }).locator('.metric-value');
    const tileOverdue = paise(await tile.innerText());
    const table = adminPage.locator('table', { has: adminPage.locator('tfoot td[data-label="Overdue"]') }).first();
    const rows = await table.locator('tbody td[data-label="Overdue"]').allInnerTexts();
    const rowSum = rows.reduce((sum, t) => sum + paise(t), 0);
    const total = paise(await table.locator('tfoot td[data-label="Overdue"]').innerText());
    expect(rowSum, 'the By-category overdue rows add up to their own Total row').toBe(total);
    expect(total, 'and the Total row agrees with the Past expected return tile').toBe(tileOverdue);
    const emdRow = table.locator('tbody tr', { hasText: 'EMD' });
    expect(paise(await emdRow.locator('td[data-label="Overdue"]').innerText()), 'the unpaid EMD is not overdue').toBe(0);
    await shoot(adminPage, `rec1-dashboard-unpaid-${tag}`);
  }
  await adminPage.setViewportSize(DESKTOP);
  await adminPage.goto('/recoverables/list?ageing=overdue');
  await expect(adminPage.locator(`a[href="/recoverables/${id}"]`), 'the overdue filter the tile links to leaves it out too').toHaveCount(0);

  // The picker and the reservation screen, before the payment exists.
  await adminPage.goto('/payments/new');
  const pickRow = adminPage.locator('.co', { has: adminPage.locator(`text=EMD ${runId}`) }).first();
  if (await pickRow.count()) assertNoDanglingSlash(await pickRow.innerText(), 'the payment picker row');
  await adminPage.goto('/accounts-queue?tab=approved');
  await adminPage
    .locator('tr')
    .filter({ has: adminPage.locator(`a[href="/requests/${id}"]`) })
    .getByRole('button', { name: 'Take for processing' })
    .click();
  await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${id}$`));
  const chargeTo = adminPage.locator('dt', { hasText: 'Charge to' }).locator('xpath=following-sibling::dd[1]');
  await expect(chargeTo, 'a recoverable is named on the reservation screen').toContainText('Recoverable');
  assertNoDanglingSlash(await chargeTo.innerText(), 'Charge to');
  await shoot(adminPage, 'rec2-reserve-1440');
  await adminPage.setViewportSize(PHONE);
  await shoot(adminPage, 'rec2-reserve-390');
  await adminPage.setViewportSize(DESKTOP);

  await adminPage.getByLabel('Amount actually paid').fill(amount);
  await adminPage.getByLabel('Paid on').fill(today());
  await adminPage.getByLabel('Payment mode').selectOption('bank_transfer');
  await adminPage.getByLabel('Transaction / UTR reference').fill(`UTR${id}`);
  await adminPage.getByRole('button', { name: /Payment settled/ }).click();
  const sheet = adminPage.locator('.overlay .sheet');
  await sheet.locator('input[name="settlement"][value="settled"]').check();
  await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();
  await expect(adminPage).toHaveURL(/\/payments\/\d+$/);
  const payPath = new URL(adminPage.url()).pathname;

  const meta = adminPage.locator('.req-head .rh-meta').first();
  await expect(meta, 'the payment detail names the recoverable').toContainText('Recoverable · Operations');
  assertNoDanglingSlash(await meta.innerText(), 'the payment detail head');
  await shoot(adminPage, 'rec2-paid-1440');
  await adminPage.setViewportSize(PHONE);
  await shoot(adminPage, 'rec2-paid-390');

  // grid-2: the grid's own figures never counted it, and now its panel agrees.
  const month = today().slice(0, 7);
  for (const [vp, tag] of [[DESKTOP, '1440'], [PHONE, '390']] as const) {
    await adminPage.setViewportSize(vp);
    await adminPage.goto(`/grid?month=${month}`);
    const panel = adminPage.locator('section.split', { hasText: 'Recent Payments for' });
    await expect(panel, 'an admin still has the panel').toHaveCount(1);
    await expect(panel.locator(`a[href="${payPath}"]`), 'but not the recoverable payment in it').toHaveCount(0);
    await panel.scrollIntoViewIfNeeded();
    await shoot(adminPage, `grid2-recent-${tag}`);

    await adminPage.goto(`/payments?month=${month}`);
    const cell = adminPage.locator(`td[data-label="Project / Head"] a[href="${payPath}"]`);
    await expect(cell, 'the ledger names the recoverable').toContainText('Recoverable');
    assertNoDanglingSlash(await cell.innerText(), 'the ledger row');
    await shoot(adminPage, `rec2-ledger-${tag}`);
  }

  // grid-1: the approver holds grid:view but not payment:view. The panel is not
  // theirs, so it is absent rather than "No payments in this month.".
  const context = await browser.newContext({ viewport: DESKTOP });
  try {
    const mgr = await context.newPage();
    await login(mgr, approver.email, approver.password);
    const resp = await mgr.goto(`/grid?month=${month}`);
    expect(resp?.status()).toBe(200);
    await expect(mgr.locator('h1', { hasText: 'Variance grid' })).toBeVisible();
    await expect(mgr.getByText('Recent Payments for')).toHaveCount(0);
    await expect(mgr.getByText('No payments in this month.')).toHaveCount(0);
    await shoot(mgr, 'grid1-manager-1440');
    await mgr.setViewportSize(PHONE);
    await shoot(mgr, 'grid1-manager-390');
  } finally {
    await context.close();
  }
});

test('grid-pill-1 + audit-2 — On track is green like its legend, and budget saves audit only what changed', async ({
  adminPage,
  runId
}) => {
  // audit-2: the first save of 2026-08 writes what it creates; saving again with
  // nothing changed writes nothing; changing one head writes one readable row.
  const auditRows = async () => {
    await adminPage.goto('/audit?entity=budget');
    return (await adminPage.locator('body').innerText()).match(/2026-08:/g)?.length ?? 0;
  };
  const beforeFirst = await auditRows();
  await adminPage.goto('/budgets?month=2026-08');
  await adminPage.getByLabel('Budget for Operations / Office Rent').fill('2,50,000.00');
  await adminPage.getByRole('button', { name: 'Save Budgets' }).click();
  await expect(adminPage).toHaveURL(/\/budgets\?month=2026-08/);
  const afterFirst = await auditRows();
  // Every other head on the form went in at ₹0 with no budget behind it, which
  // is no change at all: the first save records the one head that was set.
  expect(afterFirst - beforeFirst, 'the first save audits only the head that was set').toBe(1);
  await expect(adminPage.getByText('Operations / Office Rent 2026-08: set to ₹2,50,000.00').first()).toBeVisible();

  await adminPage.goto('/budgets?month=2026-08');
  await adminPage.getByRole('button', { name: 'Save Budgets' }).click();
  await expect(adminPage).toHaveURL(/\/budgets\?month=2026-08/);
  expect(await auditRows(), 'an unchanged save writes no audit rows').toBe(afterFirst);

  await adminPage.goto('/budgets?month=2026-08');
  const utilities = adminPage.getByLabel('Budget for Operations / Utilities');
  const before = await utilities.inputValue();
  const next = paise(before) === 100000 ? '2,000.00' : '1,000.00';
  await utilities.fill(next);
  await adminPage.getByRole('button', { name: 'Save Budgets' }).click();
  await expect(adminPage).toHaveURL(/\/budgets\?month=2026-08/);
  expect(await auditRows(), 'one head changed, one audit row').toBe(afterFirst + 1);
  // Utilities had no 2026-08 budget until now (an untouched head is not
  // written), so its first figure is a "set to"; a later one names both sides.
  const nextRe = next.replace(/[.]/g, '\\.');
  const summary = paise(before) === 0
    ? new RegExp(`Operations / Utilities 2026-08: set to ₹${nextRe}`)
    : new RegExp(`Operations / Utilities 2026-08: ₹[\\d,.]+ → ₹${nextRe}`);
  await expect(adminPage.getByText(summary).first()).toBeVisible();
  await shoot(adminPage, 'audit2-1440', false);

  // grid-pill-1: pay Office Rent exactly its 2026-08 budget.
  const request = await createApprovedRequest(adminPage, runId, { amount: '250000' });
  await settlePayment(adminPage, request.id, { amount: '250000', paidOn: '2026-08-10' });
  for (const [vp, tag] of [[DESKTOP, '1440'], [PHONE, '390']] as const) {
    await adminPage.setViewportSize(vp);
    await adminPage.goto('/grid?month=2026-08&status=on-track');
    const pill = adminPage.locator('.pill.on-track:visible').first();
    await expect(pill).toHaveText('On track');
    const colours = await adminPage.evaluate(() => {
      const p = document.querySelector('.pill.on-track') as HTMLElement;
      const sw = document.querySelector('.legend .sw.track') as HTMLElement;
      const over = document.createElement('span');
      over.className = 'pill over';
      document.body.appendChild(over);
      const c = {
        dot: getComputedStyle(p, '::before').backgroundColor,
        border: getComputedStyle(p).borderTopColor,
        swatch: getComputedStyle(sw).backgroundColor,
        text: getComputedStyle(p).color,
        overText: getComputedStyle(over).color
      };
      over.remove();
      return c;
    });
    expect(colours.dot, 'the pill dot is the legend swatch').toBe(colours.swatch);
    expect(colours.border, 'and so is its rule').toBe(colours.swatch);
    const [r, g, b] = colours.text.match(/\d+/g)!.map(Number);
    expect(g > r && g > b, `the pill text is green, not rust: ${colours.text}`).toBe(true);
    expect(colours.text).not.toBe(colours.overText);
    await shoot(adminPage, `pill-ontrack-${tag}`);
  }
});

test('grid-htmx-1 — filters swap the grid in over htmx, push the URL, and still work without JavaScript', async ({
  adminPage,
  browser
}) => {
  for (const [vp, tag] of [[DESKTOP, '1440'], [PHONE, '390']] as const) {
    await adminPage.setViewportSize(vp);
    await adminPage.goto('/grid?month=2026-06');
    await adminPage.evaluate(() => ((window as unknown as { __stay: number }).__stay = 1));
    const toolbar = adminPage.locator('form.toolbar[aria-label="Grid filters"]');
    const rows = vp === DESKTOP ? adminPage.locator('tr.head .hname') : adminPage.locator('.acc .hr-name');
    const all = await rows.count();
    expect(all).toBeGreaterThan(1);

    // Search: typing is enough, and the page is never reloaded.
    const swapped = adminPage.waitForResponse(r => r.url().includes('/grid?') && r.request().headers()['hx-request'] === 'true');
    await toolbar.locator('input[name="q"]').fill('Payroll');
    await swapped;
    await expect(rows).toHaveCount(1);
    await expect(rows.first()).toContainText('Payroll');
    await expect(adminPage).toHaveURL(/\/grid\?month=2026-06&status=[^&]*&q=Payroll/);
    expect(await adminPage.evaluate(() => (window as unknown as { __stay?: number }).__stay), 'no full page load').toBe(1);
    await expect(toolbar.locator('input[name="q"]'), 'the search box keeps its value and focus').toBeFocused();
    await expect(adminPage.locator('#grid-summary .sub')).toContainText('· 1 head ·');
    await expect(adminPage.locator('#grid-export')).toHaveAttribute('href', /q=Payroll/);
    await shoot(adminPage, `htmx-search-${tag}`);

    // Month: a change event swaps the header, the legend and the body.
    await toolbar.locator('input[name="q"]').fill('');
    await expect(rows).toHaveCount(all);
    await toolbar.locator('select[name="status"]').selectOption('not-paid');
    await expect(adminPage).toHaveURL(/status=not-paid/);
    await toolbar.locator('input[name="month"]').fill('2026-05');
    await expect(adminPage.locator('#grid-summary .sub')).toContainText('2026-05');
    await expect(adminPage).toHaveURL(/month=2026-05/);
    await expect(adminPage.locator('#grid-add')).toHaveAttribute('href', '/payments/new?month=2026-05');
    expect(await adminPage.evaluate(() => (window as unknown as { __stay?: number }).__stay), 'still no full page load').toBe(1);

    // Back returns to the pushed state.
    await adminPage.goBack();
    await expect(adminPage).toHaveURL(/month=2026-06/);
    await expect(adminPage.locator('#grid-summary .sub')).toContainText('2026-06');
    await expect(adminPage.locator('.sidebar, .tabbar').first()).toBeAttached();
  }

  // Without JavaScript the same form is a plain GET with an Apply button.
  const context = await browser.newContext({ javaScriptEnabled: false, viewport: DESKTOP });
  try {
    const page = await context.newPage();
    await page.goto('/login');
    await page.getByLabel('Email').fill(admin.email);
    await page.getByLabel('Password').fill(admin.password);
    await page.getByRole('button', { name: 'Login' }).click();
    await page.goto('/grid?month=2026-06');
    const toolbar = page.locator('form.toolbar[aria-label="Grid filters"]');
    await toolbar.locator('input[name="q"]').fill('Office Rent');
    await toolbar.getByRole('button', { name: 'Apply' }).click();
    await expect(page).toHaveURL(/\/grid\?month=2026-06&status=all&q=Office\+Rent/);
    await expect(page.locator('tr.head .hname')).toHaveCount(1);
    await shoot(page, 'htmx-nojs-1440');
  } finally {
    await context.close();
  }
});

/*
 * grid-htmx-1, review. Typing a search and pressing Enter (or Apply) before the
 * live-search pause runs out used to fire two requests, the second cancelling
 * the first, and htmx logged the cancellation as console errors. Back and
 * Forward restored the grid rows from htmx's snapshot but not what was typed or
 * picked in the controls, so the next change silently dropped the filter.
 */
test('grid-htmx-1 — search then Enter logs no errors, and Back/Forward keep the controls in step with the grid', async ({
  adminPage
}) => {
  const errors: string[] = [];
  adminPage.on('console', m => { if (m.type() === 'error') errors.push(`console: ${m.text()}`); });
  adminPage.on('pageerror', e => errors.push(`pageerror: ${e.message}`));
  const gridRequests: string[] = [];
  adminPage.on('request', r => { if (r.headers()['hx-request'] && r.url().includes('/grid?')) gridRequests.push(r.url()); });

  for (const [vp, tag] of [[DESKTOP, '1440'], [PHONE, '390']] as const) {
    await adminPage.setViewportSize(vp);
    await adminPage.goto('/grid?month=2026-06');
    const toolbar = adminPage.locator('form.toolbar[aria-label="Grid filters"]');
    const q = toolbar.locator('input[name="q"]');
    const status = toolbar.locator('select[name="status"]');
    const rows = vp === DESKTOP ? adminPage.locator('tr.head .hname') : adminPage.locator('.acc .hr-name');
    const all = await rows.count();

    // 1 — type and press Enter at once: one filtered grid, no cancelled request.
    gridRequests.length = 0;
    await q.fill('Office');
    await q.press('Enter');
    await expect(adminPage).toHaveURL(/q=Office/);
    await expect(rows.first()).toContainText('Office');
    await adminPage.waitForTimeout(800); // past the 350ms live-search pause
    expect(errors, 'no console error from a search submitted mid-pause').toEqual([]);
    expect(gridRequests, 'the pause does not fire the same search a second time').toHaveLength(1);
    await shoot(adminPage, `htmx-enter-${tag}`);

    // 2 — search Payroll, then narrow by status, then go Back and Forward.
    await q.fill('Payroll');
    await expect(adminPage).toHaveURL(/q=Payroll/);
    await expect(rows).toHaveCount(1);
    await status.selectOption('under');
    await expect(adminPage).toHaveURL(/status=under&q=Payroll/);

    await adminPage.goBack();
    await expect(adminPage).toHaveURL(/\/grid\?month=2026-06&status=all&q=Payroll$/);
    await expect(rows).toHaveCount(1);
    await expect(q, 'Back: the search box says what the grid is filtered by').toHaveValue('Payroll');
    await expect(status, 'Back: and so does the status').toHaveValue('all');
    await shoot(adminPage, `htmx-back-${tag}`);

    await adminPage.goForward();
    await expect(adminPage).toHaveURL(/status=under&q=Payroll$/);
    await expect(q, 'Forward: the search box is restored').toHaveValue('Payroll');
    await expect(status, 'Forward: the status is restored').toHaveValue('under');

    // 3 — the next change carries the filter the page shows, not an empty one.
    await adminPage.goBack();
    await expect(q).toHaveValue('Payroll');
    await status.selectOption('not-paid');
    await expect(adminPage).toHaveURL(/status=not-paid&q=Payroll$/);
    expect(await rows.count()).toBeLessThan(all);
    await shoot(adminPage, `htmx-after-back-${tag}`);
  }
  expect(errors, 'no console or page error anywhere in the run').toEqual([]);
});

test('recoverables-6 + ui-1 — compact total cards on a phone, rows that line up, and an Add user refusal that keeps the drawer', async ({
  adminPage,
  runId
}) => {
  // recoverables-6: no blank stripes in the total card at 390px.
  await adminPage.setViewportSize(PHONE);
  for (const path of ['/recoverables/list', '/vendors']) {
    await adminPage.goto(path);
    const blanks = await adminPage.locator('table.t-cards tfoot td').evaluateAll(cells =>
      cells.filter(c => (c as HTMLElement).offsetHeight > 0 && !(c.textContent ?? '').trim()).length
    );
    expect(blanks, `${path}: empty tfoot cells render as stripes`).toBe(0);
    await adminPage.locator('table.t-cards tfoot').scrollIntoViewIfNeeded();
    await shoot(adminPage, `rec6-tfoot${path.replace(/\//g, '-')}-390`, false);
  }
  await adminPage.setViewportSize(DESKTOP);
  await adminPage.goto('/recoverables/list');
  const desktopCells = await adminPage.locator('table.t-cards tfoot td').evaluateAll(cells => cells.filter(c => (c as HTMLElement).offsetHeight > 0).length);
  expect(desktopCells, 'on a desktop the spacers still hold Amount under its column').toBe(11);

  // ui-1 (b): /months at 1440 — the Actions cell ends where its row ends.
  await adminPage.goto('/months');
  const gaps = await adminPage.locator('table.t-cards tbody tr').evaluateAll(trs =>
    trs
      .filter(tr => tr.querySelector('td.actions-cell'))
      .map(tr => {
        const row = tr.getBoundingClientRect();
        const cell = tr.querySelector('td.actions-cell')!.getBoundingClientRect();
        return Math.abs(row.bottom - cell.bottom) + Math.abs(row.top - cell.top);
      })
  );
  expect(gaps.length).toBeGreaterThan(0);
  for (const gap of gaps) expect(gap, 'the Actions cell spans its row').toBeLessThanOrEqual(1);
  await shoot(adminPage, 'ui1-months-1440', false);

  // ui-1 (c): Add user with no role, at both widths.
  for (const [vp, tag] of [[DESKTOP, '1440'], [PHONE, '390']] as const) {
    await adminPage.setViewportSize(vp);
    await adminPage.goto('/users');
    await openNewUserSheet(adminPage);
    const sheet = adminPage.locator('#user-new');
    await sheet.getByLabel('Email').fill(`norole-${tag}-${runId}@example.test`);
    await sheet.getByLabel('Name').fill(`No Role ${tag}`);
    await sheet.getByLabel('Password').fill('StrongTestPassword!42');
    await sheet.getByRole('button', { name: 'Add User' }).click();
    await expect(sheet, 'the drawer is still open after the refusal').toBeVisible();
    await expect(sheet.locator('.alert.error')).toContainText('Choose at least one role for the new user.');
    await expect(sheet.getByLabel('Email')).toHaveValue(`norole-${tag}-${runId}@example.test`);
    await expect(sheet.getByLabel('Name')).toHaveValue(`No Role ${tag}`);
    await expect(sheet.getByLabel('Password')).toHaveValue('');
    await shoot(adminPage, `ui1-norole-${tag}`, false);
    // It is a registered dialog: Escape closes it.
    await adminPage.keyboard.press('Escape');
    await expect(sheet).toBeHidden();
  }
});
