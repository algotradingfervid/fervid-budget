import { type Browser, type Page } from '@playwright/test';
import {
  test,
  expect,
  login,
  createApproverUser,
  createApprovedRequest,
  settlePayment,
  capturePageErrors
} from './fixtures';
import { asRole, probeGet, probePost, probeAnonymous, expectOutcome } from './audit-support';

/**
 * TC-E — Recoverables.
 *
 * Covers: recoverable-category CRUD (V4), per-category field rules (V5/V6),
 * exclusion of recoverable money from budget actuals (V2/V3), the register /
 * dashboard / detail screens, separate payout/recovery lifecycles, project linkage (V8),
 * append-only recovery events and unsupported legacy aliases, plus the recoverable
 * permission matrix (R6). Full reasoning and expected results are in
 * docs/qa/test-cases/TC-E-recoverables.md — this file is the executable half.
 *
 * F-E-01 (critical, see docs/qa/results/findings-e-recoverables.md) is FIXED. It
 * held that no recoverable request raised through the real screens could ever be
 * settled: the recoverable fieldset collects no head, the payment form's hidden
 * head_id therefore defaults to 0, and `validatePayment` refused it. Migration v8
 * made `payments.head_id` nullable and `validatePayment` now demands a head only
 * for a budget-treatment payment, so a recoverable settles with head_id NULL.
 * TC-E-030 is the regression guard for that, and TC-E-031 / TC-E-042 — which were
 * test.fixme() because a *paid* recoverable was unreachable — run again.
 */

// ---------------------------------------------------------------------------
// Local helpers. Nothing here touches fixtures.ts or audit-support.ts.
// ---------------------------------------------------------------------------

/** Parses a rendered "₹1,23,456.78" (or "-₹12.00") cell into paise. */
function parseRupees(text: string): number {
  const cleaned = text.replace(/[₹,\s]/g, '').trim();
  if (cleaned === '' || cleaned === '—' || cleaned === '-') return 0;
  const value = parseFloat(cleaned);
  if (Number.isNaN(value)) throw new Error(`cannot parse rupee text "${text}"`);
  return Math.round(value * 100);
}

/** A small quoted-field-aware CSV parser — Go's encoding/csv quotes any field containing a comma. */
function parseCsv(text: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = '';
  let inQuotes = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (inQuotes) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else inQuotes = false;
      } else field += c;
    } else if (c === '"') inQuotes = true;
    else if (c === ',') {
      row.push(field);
      field = '';
    } else if (c === '\r') {
      // skip
    } else if (c === '\n') {
      row.push(field);
      rows.push(row);
      row = [];
      field = '';
    } else field += c;
  }
  if (field.length > 0 || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  return rows;
}

/** The <option> value whose visible text is exactly `label`, read without operating the control. */
async function optionValueByExactLabel(page: Page, selectSelector: string, label: string): Promise<string> {
  const value = await page.locator(selectSelector).evaluate((el: HTMLSelectElement, wanted: string) => {
    const opt = Array.from(el.options).find(o => o.textContent?.trim() === wanted);
    return opt ? opt.value : null;
  }, label);
  if (!value) throw new Error(`no <option> with exact label "${label}" in ${selectSelector}`);
  return value;
}

/** A real project id, read from a plain budget-type request form (never operated). */
async function projectId(page: Page, label: string): Promise<string> {
  await page.goto('/requests/new?type=vendor_invoice');
  return optionValueByExactLabel(page, '#project', label);
}

/** A real user id for `manager_id`, read from a requester's own Approver select. */
async function approverIdFor(requesterPage: Page, approverName: string): Promise<string> {
  await requesterPage.goto('/requests/new?type=vendor_invoice');
  return optionValueByExactLabel(requesterPage, '#approver', approverName);
}

type RecoverableFormOpts = {
  category: 'emd' | 'pbg' | 'icd' | 'employee_advance' | 'security_deposit' | 'other';
  projectLabel?: string; // fill '#rproject' when the category's fieldset shows it
  counterparty?: string; // fill '#counterparty' when the category's fieldset shows it
  payee?: string; // 'Paid to' on the deposit form; defaults to the counterparty or an authority
  expectedReturn?: string; // '' explicitly omits the field
  notes?: string; // '' explicitly omits the field
  advanceReason?: string;
  amount?: string;
  purpose?: string;
  shortTitle?: string;
  approverName?: string;
};

/**
 * Drives a recoverable request form up to (not including) the submit click.
 *
 * Two forms since form-1 / recoverables-1. An employee advance is
 * `/requests/new?type=employee_advance`: it opens on the recoverable treatment
 * with its category fixed to Employee advance and pays the requester. Every
 * other category — EMD, PBG, ICD, a security deposit, Other, an admin-added one
 * — is a deposit or guarantee, `/requests/new?type=recoverable`, which carries
 * the category picker and asks who is paid. (Before that fix the employee
 * advance was the only card carrying the recoverable treatment, so every
 * deposit was recorded as paid to the employee.)
 */
async function fillRecoverableRequestForm(page: Page, runId: string, opts: RecoverableFormOpts) {
  // The Approver <select> carries the `required` attribute (templates.go:2015), so a
  // submission with no approver never reaches the server at all — the browser silently
  // blocks it. Every caller therefore needs a real approver name; one is created here
  // when the caller does not already have a specific person it needs to approve as.
  const approverName = opts.approverName ?? (await createApproverUser(page, runId)).name;

  const advance = opts.category === 'employee_advance';
  await page.goto(advance ? '/requests/new?type=employee_advance' : '/requests/new?type=recoverable');
  await page.getByLabel('Short title').fill(opts.shortTitle ?? `Recoverable ${opts.category} ${runId}`);
  if (!advance) {
    await page.locator('#rcategory').selectOption(opts.category);
    // The category change swaps #form-fields from the server; the swap is
    // settled when the option comes back selected server-side.
    await expect(page.locator(`#rcategory option[value="${opts.category}"]`)).toHaveAttribute('selected', '');
    await page.locator('#payee').fill(opts.payee ?? opts.counterparty ?? `Tender authority ${runId}`);
  }
  if (opts.projectLabel !== undefined) {
    await page.locator('#rproject').waitFor();
    if (opts.projectLabel) await page.locator('#rproject').selectOption({ label: opts.projectLabel });
  }
  if (opts.counterparty !== undefined) {
    await page.locator('#counterparty').waitFor();
    if (opts.counterparty) await page.locator('#counterparty').fill(opts.counterparty);
  }
  if (opts.expectedReturn !== undefined && opts.expectedReturn !== '') {
    await page.getByLabel('Expected return date').fill(opts.expectedReturn);
  }
  if (opts.notes !== undefined && opts.notes !== '') {
    await page.getByLabel('Repayment or refund terms').fill(opts.notes);
  }
  if (advance) await page.getByLabel('What the money is for').fill(opts.advanceReason ?? `Advance for ${opts.category} ${runId}`);
  await page.getByLabel('Amount').fill(opts.amount ?? '25000');
  await page.getByLabel('Purpose').fill(opts.purpose ?? `Purpose ${opts.category} ${runId}`);
  await page.getByLabel('Approver').selectOption({ label: approverName });
}

async function submitAndExpectAccepted(page: Page): Promise<{ id: number; number: string }> {
  await page.getByRole('button', { name: 'Submit request' }).click();
  await expect(page, 'a valid recoverable submission must reach the confirmation screen').toHaveURL(/\/requests\/\d+\/submitted$/);
  const id = Number(new URL(page.url()).pathname.split('/')[2]);
  const number = (await page.locator('.rh-no').first().innerText()).trim();
  return { id, number };
}

async function submitAndExpectRejected(page: Page, messageSubstring: string) {
  await page.getByRole('button', { name: 'Submit request' }).click();
  await expect(page, 'a rejected submission re-renders the same form at /requests').toHaveURL(/\/requests$/);
  await expect(
    page.locator('.alert.error'),
    `expected the validation message to mention "${messageSubstring}"`
  ).toContainText(messageSubstring);
}

/** Approves an already-submitted request as `approver`, in a context of its own. G8 forbids self-approval. */
async function approveAsManager(
  browser: Browser,
  requestId: number,
  amount: string,
  approver: { email: string; password: string }
) {
  const context = await browser.newContext();
  try {
    const approverPage = await context.newPage();
    await login(approverPage, approver.email, approver.password);
    await approverPage.goto(`/requests/${requestId}`);
    await approverPage.getByRole('button', { name: /^Approve / }).click();
    const sheet = approverPage.locator('#approve-sheet');
    await expect(sheet).toBeVisible();
    await sheet.getByLabel('Amount approved').fill(amount);
    await sheet.getByRole('button', { name: 'Approve request' }).click();
    await expect(approverPage).toHaveURL(/\/approvals$/);
  } finally {
    await context.close();
  }
}

/** Raises and approves a recoverable request end to end (create + approve; never settles it — see F-E-01). */
async function raiseAndApproveRecoverable(
  page: Page,
  browser: Browser,
  runId: string,
  approver: { email: string; name: string; password: string },
  opts: RecoverableFormOpts
): Promise<{ id: number; number: string }> {
  await fillRecoverableRequestForm(page, runId, { ...opts, approverName: approver.name });
  const created = await submitAndExpectAccepted(page);
  await approveAsManager(browser, created.id, opts.amount ?? '25000', approver);
  return created;
}

async function gridActualForHead(page: Page, month: string, headName: string): Promise<number> {
  await page.goto(`/grid?month=${month}`);
  const row = page.locator('tr.head').filter({ has: page.locator('.hname', { hasText: headName }) });
  await expect(row, `expected exactly one grid row for head "${headName}"`).toHaveCount(1);
  const text = await row.locator('td.num').nth(1).innerText(); // Budget, Actual, Variance, Remaining% — Actual is index 1
  return parseRupees(text);
}

/**
 * `/reports/heads` drops any row where Budget AND Actual are both zero
 * (`nonEmptyReportRows`, app.go:1562-1570), so a head with no budget set for
 * the current wall-clock month simply would not appear. This makes sure the
 * row can never disappear on us, without perturbing the Actual figure the
 * before/after tests depend on — it only acts when the budget is genuinely 0.
 */
async function ensureHeadHasBudget(page: Page, month: string, headName: string) {
  await page.goto(`/budgets?month=${month}`);
  const row = page.locator('tr').filter({ has: page.locator('td.t-lead', { hasText: headName }) });
  const input = row.locator('input[name^="budget_"]');
  const current = parseRupees((await input.inputValue()) || '0');
  if (current > 0) return;
  const name = await input.getAttribute('name');
  if (!name) throw new Error(`no budget input found for head "${headName}"`);
  const resp = await probePost(page, '/budgets', { month, [name]: '1,00,000.00' });
  if (resp.status !== 303) throw new Error(`failed to seed a budget for "${headName}": ${resp.outcome}\n${resp.body.slice(0, 300)}`);
}

async function reportHeadsActual(page: Page, month: string, headName: string): Promise<number> {
  await ensureHeadHasBudget(page, month, headName);
  await page.goto(`/reports/heads?from=${month}&to=${month}`);
  const row = page.locator('tr').filter({ has: page.locator('td[data-label="Head"]', { hasText: headName }) });
  await expect(row, `expected exactly one /reports/heads row for head "${headName}"`).toHaveCount(1);
  const text = await row.locator('td[data-label="Actual"]').innerText();
  return parseRupees(text);
}

/** The metric-strip figure on /recoverables for a given label, e.g. "Past expected return". */
async function dashboardMetric(page: Page, label: string): Promise<{ amount: number; count: number }> {
  const body = (await probeGet(page, '/recoverables')).body;
  const idx = body.indexOf(`>${label}<`);
  if (idx < 0) throw new Error(`no metric tile labelled "${label}" on /recoverables`);
  const chunk = body.slice(idx, idx + 250);
  const amountMatch = chunk.match(/metric-value">([^<]+)</);
  const footMatch = chunk.match(/metric-foot">([^<]+)</);
  if (!amountMatch || !footMatch) throw new Error(`could not parse the "${label}" metric tile: ${chunk}`);
  const countMatch = footMatch[1].match(/\d+/);
  return { amount: parseRupees(amountMatch[1]), count: countMatch ? Number(countMatch[0]) : 0 };
}

async function reportMonthlyActual(page: Page, month: string): Promise<number> {
  await page.goto(`/reports/monthly?from=${month}&to=${month}`);
  const row = page.locator('tbody tr').first();
  const text = await row.locator('td[data-label="Actual"]').innerText();
  return parseRupees(text);
}

async function csvActual(
  page: Page,
  url: string,
  matchCols: Array<{ col: number; value: string }>,
  actualCol: number
): Promise<number> {
  const resp = await page.request.get(url);
  const text = await resp.text();
  const rows = parseCsv(text);
  const match = rows.find(r => matchCols.every(m => r[m.col] === m.value));
  if (!match) throw new Error(`no CSV row matched ${JSON.stringify(matchCols)} in ${url}\n${text.slice(0, 500)}`);
  return parseRupees(match[actualCol]);
}

/** The Configuration row for a category. Its name is an editable input (recoverables-5), so the row is found by that input's value. */
function categoryRow(page: Page, name: string) {
  return page.locator('tr').filter({ has: page.locator(`td.t-lead input[name="name"][value="${name}"]`) });
}

/** Every category name on the Configuration screen, in the order the table lists them. */
async function categoryNames(page: Page): Promise<string[]> {
  return page.locator('td.t-lead[data-label="Category"] input[name="name"]').evaluateAll(els => els.map(e => (e as HTMLInputElement).value));
}

async function categoryRowFields(page: Page, name: string): Promise<{ id: string; requires: string; sortOrder: string; active: boolean }> {
  await page.goto('/configuration');
  const row = categoryRow(page, name);
  await expect(row, `expected exactly one Configuration row for category "${name}"`).toHaveCount(1);
  const form = row.locator('form');
  const id = (await form.locator('input[name="id"]').getAttribute('value')) ?? '';
  // The rule is a visible select in the row since recoverables-5, not a hidden input.
  const requires = (await row.locator('select[name="requires"]').inputValue()) || 'none';
  const sortOrder = (await form.locator('input[name="sort_order"]').getAttribute('value')) ?? '0';
  const active = await form.locator('input[name="active"]').isChecked();
  return { id, requires, sortOrder, active };
}

async function createCategoryThroughScreen(page: Page, name: string, requires: 'none' | 'project' | 'counterparty' | 'both') {
  await page.goto('/configuration');
  await page.locator('#nc-name').fill(name);
  await page.locator('#nc-req').selectOption(requires);
  await page.getByRole('button', { name: 'Add category' }).click();
  await expect(page).toHaveURL(/\/configuration$/);
}

test.describe('TC-E — Recoverables', () => {
  // -------------------------------------------------------------------------
  // Section 1 — Category CRUD (V4)
  // -------------------------------------------------------------------------

  test('TC-E-001 — Admin creates a recoverable category through the real screen', async ({ adminPage, runId }) => {
    const name = `Retention money ${runId}`;
    await createCategoryThroughScreen(adminPage, name, 'project');
    const row = categoryRow(adminPage, name);
    await expect(row, 'the new category must appear in the Configuration table').toHaveCount(1);
    await expect(row.locator('select[name="requires"]'), 'Requires must show the rule chosen, editable in place').toHaveValue('project');
    await expect(row.locator('td[data-label="In use"]'), 'a brand-new category starts with zero usage').toHaveText('0');
  });

  test('TC-E-002 — Renaming preserves the code; the existing request and register still resolve to it', async ({ adminPage, browser, runId }) => {
    const oldName = `Escrow ${runId}`;
    const newName = `Escrow renamed ${runId}`;
    await createCategoryThroughScreen(adminPage, oldName, 'none');
    const before = await categoryRowFields(adminPage, oldName);

    const requester = await asRole(adminPage, browser, runId, ['Requester']);
    const managerSubject = await asRole(adminPage, browser, runId, ['Manager']);
    try {
      const managerId = await approverIdFor(requester.page, managerSubject.subject.name);
      const submit = await probePost(requester.page, '/requests', {
        treatment: 'recoverable',
        // The deposit-or-guarantee type: an admin-added category is a deposit,
        // and an employee advance is always in its own category (form-1).
        type: 'recoverable',
        vendor_payee: `Escrow agent ${runId}`,
        recoverable_category: `escrow_${runId}`.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, ''),
        short_title: `Escrow request ${runId}`,
        purpose: 'Escrow test',
        amount: '15000',
        expected_return_date: '2027-05-31',
        repayment_notes: 'Released on handover',
        manager_id: managerId
      });
      // The derived code follows categoryCode(name) (internal/store/recoverables.go:99-113):
      // lowercase, non [a-z0-9] runs collapsed to one underscore, trimmed. "Escrow «runId»"
      // already matches that shape once lowercased, so the code equals the slug above.
      expect(submit.status, `expected the recoverable request to be accepted, got ${submit.outcome}`).toBe(303);
      const requestId = Number(submit.location?.match(/\/requests\/(\d+)\/submitted/)?.[1]);
      expect(requestId, 'expected a numeric request id in the Location header').toBeGreaterThan(0);

      let detail = await probeGet(adminPage, `/recoverables/${requestId}`);
      expect(detail.body).toContain(oldName);

      // Rename through the row's own name input and Save button (recoverables-5):
      // the name used to travel as a hidden input with no control behind it.
      await adminPage.goto('/configuration');
      const row = categoryRow(adminPage, oldName);
      await expect(row, 'the category row carries a visible name input').toHaveCount(1);
      await row.getByLabel(`Name for ${oldName}`).fill(newName);
      await row.getByRole('button', { name: `Save ${oldName}` }).click();
      await expect(adminPage).toHaveURL(/\/configuration$/);
      await expect(adminPage.locator(`input[name="name"][value="${newName}"]`), 'the rename is shown back').toHaveCount(1);

      detail = await probeGet(adminPage, `/recoverables/${requestId}`);
      expect(detail.body, 'the existing request must now show the NEW name').toContain(newName);
      expect(detail.body, 'the old name must be gone once renamed').not.toContain(oldName);

      const listBody = (await probeGet(adminPage, '/recoverables/list')).body;
      expect(listBody, 'the register must also show the new name').toContain(newName);
    } finally {
      await requester.close();
      await managerSubject.close();
    }
  });

  test('TC-E-003 — Reordering changes list order in Configuration and in the /recoverables/list filter', async ({ adminPage, runId }) => {
    const nameA = `Order A ${runId}`;
    const nameB = `Order B ${runId}`;
    await probePost(adminPage, '/configuration/recoverable-categories', { name: nameA, requires: 'none', active: 'on', sort_order: '97' });
    await probePost(adminPage, '/configuration/recoverable-categories', { name: nameB, requires: 'none', active: 'on', sort_order: '98' });

    await adminPage.goto('/configuration');
    let names = await categoryNames(adminPage);
    expect(names.indexOf(nameA), 'A (sort_order 97) must precede B (sort_order 98)').toBeLessThan(names.indexOf(nameB));

    await adminPage.goto('/recoverables/list');
    let options = await adminPage.locator('#cat option').allTextContents();
    expect(options.indexOf(nameA), 'the filter dropdown must honour the same order').toBeLessThan(options.indexOf(nameB));

    const a = await categoryRowFields(adminPage, nameA);
    const b = await categoryRowFields(adminPage, nameB);
    await probePost(adminPage, '/configuration/recoverable-categories', { id: a.id, name: nameA, requires: 'none', active: 'on', sort_order: '99' });
    await probePost(adminPage, '/configuration/recoverable-categories', { id: b.id, name: nameB, requires: 'none', active: 'on', sort_order: '97' });

    await adminPage.goto('/configuration');
    names = await categoryNames(adminPage);
    expect(names.indexOf(nameB), 'after swapping sort_order, B must now precede A').toBeLessThan(names.indexOf(nameA));
  });

  test('TC-E-004 — Deactivating blocks new requests but leaves history and the list filter intact', async ({ adminPage, browser, runId }) => {
    const requester = await asRole(adminPage, browser, runId, ['Requester']);
    const managerSubject = await asRole(adminPage, browser, runId, ['Manager']);
    const pbg = await categoryRowFields(adminPage, 'PBG');
    try {
      const managerId = await approverIdFor(requester.page, managerSubject.subject.name);
      const proj = await projectId(requester.page, 'Operations');

      const before = await probePost(requester.page, '/requests', {
        treatment: 'recoverable',
        type: 'recoverable',
        vendor_payee: `Ridge Metro ${runId}`,
        recoverable_category: 'pbg',
        project_id: proj,
        short_title: `PBG before deactivate ${runId}`,
        purpose: 'PBG test',
        amount: '10000',
        expected_return_date: '2027-05-31',
        repayment_notes: 'On completion',
        manager_id: managerId
      });
      expect(before.status, 'a PBG request with a project must be accepted while PBG is active').toBe(303);

      // Deactivate PBG (the checkbox auto-submits; POST directly with active omitted).
      const deactivate = await probePost(adminPage, '/configuration/recoverable-categories', {
        id: pbg.id,
        name: 'PBG',
        requires: pbg.requires,
        sort_order: pbg.sortOrder
        // active omitted == unchecked
      });
      expect(deactivate.status).toBe(303);

      const after = await probePost(requester.page, '/requests', {
        treatment: 'recoverable',
        type: 'recoverable',
        vendor_payee: `Ridge Metro ${runId}`,
        recoverable_category: 'pbg',
        project_id: proj,
        short_title: `PBG after deactivate ${runId}`,
        purpose: 'PBG test',
        amount: '10000',
        expected_return_date: '2027-05-31',
        repayment_notes: 'On completion',
        manager_id: managerId
      });
      expect(after.status, 'a deactivated category must refuse a new request naming it').toBe(400);
      expect(after.body).toContain('choose a recoverable category');

      const filterOptions = await (async () => {
        await adminPage.goto('/recoverables/list');
        return adminPage.locator('#cat option').allTextContents();
      })();
      expect(filterOptions, 'a deactivated category must remain filterable for historical rows').toContain('PBG');
    } finally {
      // Restore PBG to active unconditionally — even if an assertion above throws — so a
      // failure here can never poison every later test that raises a PBG request.
      await probePost(adminPage, '/configuration/recoverable-categories', {
        id: pbg.id,
        name: 'PBG',
        requires: pbg.requires,
        sort_order: pbg.sortOrder,
        active: 'on'
      });
      await requester.close();
      await managerSubject.close();
    }
  });

  test('TC-E-005 — Duplicate category name differing only by case is refused', async ({ adminPage, runId }) => {
    const name = `Deposit probe ${runId}`;
    await createCategoryThroughScreen(adminPage, name, 'none');
    const dup = await probePost(adminPage, '/configuration/recoverable-categories', {
      name: name.toUpperCase(),
      requires: 'none',
      active: 'on'
    });
    expect(dup.status, 'a case-insensitive duplicate name must be refused').toBe(400);

    await adminPage.goto('/configuration');
    const count = (await categoryNames(adminPage)).filter(n => n.toLowerCase() === name.toLowerCase()).length;
    expect(count, 'the duplicate must not have been created a second time').toBeLessThanOrEqual(1);
  });

  test('TC-E-006 — "In use" count matches the number of requests naming the category', async ({ adminPage, browser, runId }) => {
    const name = `Usage probe ${runId}`;
    await createCategoryThroughScreen(adminPage, name, 'none');
    const { id: categoryId } = await categoryRowFields(adminPage, name);
    const code = `usage_probe_${runId}`.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '');

    const requester = await asRole(adminPage, browser, runId, ['Requester']);
    const managerSubject = await asRole(adminPage, browser, runId, ['Manager']);
    try {
      const managerId = await approverIdFor(requester.page, managerSubject.subject.name);
      for (let i = 0; i < 2; i++) {
        const resp = await probePost(requester.page, '/requests', {
          treatment: 'recoverable',
          type: 'recoverable',
          vendor_payee: `Usage payee ${runId}`,
          recoverable_category: code,
          short_title: `Usage ${i} ${runId}`,
          purpose: 'usage test',
          amount: '5000',
          expected_return_date: '2027-05-31',
          repayment_notes: 'n/a',
          manager_id: managerId
        });
        expect(resp.status, `request ${i} against the new category must be accepted`).toBe(303);
      }
      void categoryId;
      const row = (await categoryRowFields(adminPage, name), categoryRow(adminPage, name));
      await adminPage.goto('/configuration');
      const usageRow = categoryRow(adminPage, name);
      await expect(usageRow.locator('td[data-label="In use"]'), 'two requests were filed against this category').toHaveText('2');
      void row;
    } finally {
      await requester.close();
      await managerSubject.close();
    }
  });

  test('TC-E-007 — [F-E-06] A category can be deleted while nothing names it, and the refusal counts what does', async ({
    adminPage,
    browser,
    runId
  }) => {
    // F-E-06, fixed. `recoverable_category:delete` had been grantable from the
    // Roles screen since Phase 4 and no route ever consulted it, so an
    // administrator could hand it out and buy nothing at all; this test recorded
    // that absence as if it were the design. The door is now
    // POST /configuration/recoverable-categories/{id}/delete (internal/app/app.go:614),
    // gated on that verb, over store.DeleteRecoverableCategory
    // (internal/store/recoverables.go:260), which counts the requests still
    // pointing at the row inside the DELETE's own transaction.
    //
    // The refusal is the interesting half and the reason the finding was worth a
    // door rather than dropping the verb: deactivating stays the way to retire a
    // category that has been used, and the refusal has to say so with the count
    // the "In use" column beside the button already prints, not flatten into "you
    // do not have permission" (the F-G-023 mistake).
    const bodyBefore = (await probeGet(adminPage, '/configuration')).body;
    expect(bodyBefore, 'an admin holds recoverable_category:delete, so the fieldset offers the control').toMatch(
      />Delete</i
    );

    // Only POST is routed. `GET /` is a catch-all, so a GET of this path is
    // handled by it rather than met with a method refusal — either answer proves
    // the same thing, that deleting is not reachable by navigation.
    const get = await probeGet(adminPage, '/configuration/recoverable-categories/1/delete');
    expectOutcome(get, [404, 405], 'deleting is a mutation, so the path answers no GET');

    const missing = await probePost(adminPage, '/configuration/recoverable-categories/999999/delete', {});
    expect(missing.status, 'a category id that does not exist is not found').toBe(404);

    // Both halves are driven against categories this test made, never a seeded
    // one: deleting is real now, and a test that reached for EMD would be one
    // "in use" count away from destroying the row five later cases raise
    // requests against.
    const inUseName = `Delete probe in use ${runId}`;
    await createCategoryThroughScreen(adminPage, inUseName, 'none');
    const inUse = await categoryRowFields(adminPage, inUseName);
    const inUseCode = `delete_probe_in_use_${runId}`.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '');

    const requester = await asRole(adminPage, browser, runId, ['Requester'], 'e007req');
    const managerSubject = await asRole(adminPage, browser, runId, ['Manager'], 'e007mgr');
    try {
      const managerId = await approverIdFor(requester.page, managerSubject.subject.name);
      const filed = await probePost(requester.page, '/requests', {
        treatment: 'recoverable',
        type: 'recoverable',
        vendor_payee: `Delete probe payee ${runId}`,
        recoverable_category: inUseCode,
        short_title: `Delete probe request ${runId}`,
        purpose: 'Delete probe',
        amount: '5000',
        expected_return_date: '2027-05-31',
        repayment_notes: 'n/a',
        manager_id: managerId
      });
      expect(filed.status, 'one request now names the category, which is what makes it in use').toBe(303);

      const refused = await probePost(adminPage, `/configuration/recoverable-categories/${inUse.id}/delete`, {});
      expect(refused.status, 'a category a request still points at cannot be deleted').toBe(403);
      expect(
        refused.body,
        'the refusal names the category and counts what is holding it, so it agrees with the "In use" column beside the button'
      ).toContain(`${inUseName} is used by 1 request`);
      expect(refused.body, 'and names the remedy that does work').toContain('deactivate it instead');
      expect(
        refused.body,
        'never the permission sentence — the caller holds the verb, the data is what refused'
      ).not.toContain('You do not have permission to perform this action.');

      await adminPage.goto('/configuration');
      const stillThere = categoryRow(adminPage, inUseName);
      await expect(stillThere, 'and the refused row is still there').toHaveCount(1);
      await expect(stillThere.locator('td[data-label="In use"]'), 'with the count the refusal quoted').toHaveText('1');
    } finally {
      await requester.close();
      await managerSubject.close();
    }

    // Not in use: deleted, and gone from the screen.
    const freshName = `Delete probe unused ${runId}`;
    await createCategoryThroughScreen(adminPage, freshName, 'none');
    const fresh = await categoryRowFields(adminPage, freshName);
    const deleted = await probePost(adminPage, `/configuration/recoverable-categories/${fresh.id}/delete`, {});
    expect(deleted.status, 'a category nothing names is a mistake to undo, and delete is what undoes it').toBe(303);
    await adminPage.goto('/configuration');
    await expect(categoryRow(adminPage, freshName), 'the row is gone from Configuration').toHaveCount(0);

    // An in-use seeded category can always be re-saved; usage blocks the delete,
    // never the save.
    const emd = await categoryRowFields(adminPage, 'EMD');
    const resave = await probePost(adminPage, '/configuration/recoverable-categories', {
      id: emd.id,
      name: 'EMD',
      requires: emd.requires,
      sort_order: emd.sortOrder,
      active: emd.active ? 'on' : ''
    });
    expect(resave.status, 're-saving an in-use category must succeed').toBe(303);
  });

  test('TC-E-008 — The pre-Phase-4 standalone /recoverable-categories screen is gone', async ({ adminPage }) => {
    const get = await probeGet(adminPage, '/recoverable-categories');
    expect(get.status, 'the standalone screen must be gone (folded into /configuration)').toBe(404);
    const post = await probePost(adminPage, '/recoverable-categories', { name: 'Nope' });
    expectOutcome(post, [404, 405], 'POST /recoverable-categories must not exist either');
  });

  test('TC-E-009 — Requester, Manager, Accounts refused POST /configuration/recoverable-categories; Admin succeeds', async ({ adminPage, browser, runId }) => {
    const roles = ['Requester', 'Manager', 'Accounts'] as const;
    for (const role of roles) {
      const subject = await asRole(adminPage, browser, runId, [role]);
      try {
        const resp = await probePost(subject.page, '/configuration/recoverable-categories', {
          name: `${role} probe ${runId}`,
          requires: 'none',
          active: 'on'
        });
        expect(resp.status, `${role} must not be able to save a recoverable category`).toBe(403);
      } finally {
        await subject.close();
      }
    }
    const admin = await asRole(adminPage, browser, runId, ['Admin']);
    try {
      const resp = await probePost(admin.page, '/configuration/recoverable-categories', {
        name: `Admin probe ${runId}`,
        requires: 'none',
        active: 'on'
      });
      expect(resp.status, 'Admin holds recoverable_category:edit and must succeed').toBe(303);
    } finally {
      await admin.close();
    }
  });

  test('TC-E-010 — [F-E-02] A newly admin-created category is selectable on the real request form', async ({ adminPage, runId }) => {
    // F-E-02 · F-B-17, fixed. The Category <select> was six hardcoded <option>
    // literals and never read `recoverable_categories`, so V4's promise — an
    // admin adds a category and its rules are enforced without a code change —
    // was half kept: the *enforcement* shipped (TC-E-024/025/026 prove the new
    // category's own project/counterparty rule is honoured) and the affordance
    // did not, so the category could only ever be reached by a hand-rolled POST.
    // The options are now `{{range .Categories}}` over the active rows
    // (internal/app/templates.go:1829), fed by ListRecoverableCategories(ctx, true)
    // (internal/app/requests.go:488).
    const name = `Selectable probe ${runId}`;
    await createCategoryThroughScreen(adminPage, name, 'none');
    const code = `selectable_probe_${runId}`.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '');

    // The picker lives on the deposit form (form-1); the employee advance form
    // states its one category instead.
    await adminPage.goto('/requests/new?type=recoverable');
    const values = await adminPage.locator('#rcategory option').evaluateAll(opts => opts.map(o => (o as HTMLOptionElement).value));
    expect(
      values,
      'a category an administrator added must be choosable through the product, not only through a forged POST — F-E-02'
    ).toContain(code);
    await expect(
      adminPage.locator(`#rcategory option[value="${code}"]`),
      'and it is offered under the name the administrator typed'
    ).toHaveText(name);
    expect(
      values,
      'the seeded deposit codes are still offered — the new source is the table, not a replacement vocabulary'
    ).toEqual(expect.arrayContaining(['emd', 'icd', 'other', 'pbg', 'security_deposit']));
    expect(values, 'an advance to an employee is the employee advance type, never a deposit category').not.toContain('employee_advance');

    // The <select> is the table, so it can be *driven* to the new category too:
    // a hardcoded list would have made this selectOption throw.
    await adminPage.locator('#rcategory').selectOption(code);
    await expect(
      adminPage.locator(`#rcategory option[value="${code}"]`),
      'and the server-rendered swap comes back with it selected'
    ).toHaveAttribute('selected', '');
  });

  test('TC-E-011 — [F-E-02] A deactivated seed category is no longer offered on the request form', async ({ adminPage }) => {
    // The other half of F-E-02 · F-B-17. Deactivating was already enforced —
    // validateRequestInput refused a request naming an inactive category
    // (TC-E-004) — but the option stayed on the form, so the refusal arrived only
    // after the whole form had been filled in. The <select> now ranges over the
    // *active* rows alone, so a retired category is simply not offered.
    const other = await categoryRowFields(adminPage, 'Other');
    await probePost(adminPage, '/configuration/recoverable-categories', {
      id: other.id,
      name: 'Other',
      requires: other.requires,
      sort_order: other.sortOrder
      // active omitted -> deactivated
    });
    try {
      await adminPage.goto('/requests/new?type=recoverable');
      await expect(
        adminPage.locator('#rcategory option[value="other"]'),
        'a deactivated category must not be offered — the refusal cannot be the first the requester hears of it'
      ).toHaveCount(0);
      await expect(
        adminPage.locator('#rcategory option[value="emd"]'),
        'while the categories that are still active are untouched'
      ).toHaveCount(1);
      // TC-E-004 already proves the deactivated category stays filterable in the
      // register: history keeps its category, only new requests are refused.
    } finally {
      await probePost(adminPage, '/configuration/recoverable-categories', {
        id: other.id,
        name: 'Other',
        requires: other.requires,
        sort_order: other.sortOrder,
        active: 'on'
      });
    }
  });

  // -------------------------------------------------------------------------
  // Section 2 — Per-category field rules (V5, V6), exhaustive
  // -------------------------------------------------------------------------

  test('TC-E-012 — EMD without a project is refused', async ({ adminPage, runId }) => {
    await fillRecoverableRequestForm(adminPage, runId, { category: 'emd', expectedReturn: '2027-03-31', notes: 'Refund on award' });
    await submitAndExpectRejected(adminPage, 'this recoverable category always belongs to a project');
  });

  test('TC-E-013 — EMD with a project is accepted', async ({ adminPage, runId }) => {
    await fillRecoverableRequestForm(adminPage, runId, {
      category: 'emd',
      projectLabel: 'Operations',
      expectedReturn: '2027-03-31',
      notes: 'Refund on award'
    });
    const { id } = await submitAndExpectAccepted(adminPage);
    expect(id).toBeGreaterThan(0);
  });

  test('TC-E-014 — PBG without a project is refused', async ({ adminPage, runId }) => {
    await fillRecoverableRequestForm(adminPage, runId, { category: 'pbg', expectedReturn: '2027-03-31', notes: 'On completion' });
    await submitAndExpectRejected(adminPage, 'this recoverable category always belongs to a project');
  });

  test('TC-E-015 — PBG with a project is accepted', async ({ adminPage, runId }) => {
    await fillRecoverableRequestForm(adminPage, runId, {
      category: 'pbg',
      projectLabel: 'Operations',
      expectedReturn: '2027-03-31',
      notes: 'On completion'
    });
    const { id } = await submitAndExpectAccepted(adminPage);
    expect(id).toBeGreaterThan(0);
  });

  test('TC-E-016 — ICD without a counterparty is refused', async ({ adminPage, runId }) => {
    await fillRecoverableRequestForm(adminPage, runId, { category: 'icd', expectedReturn: '2027-03-31', notes: 'Recover on maturity' });
    await submitAndExpectRejected(adminPage, 'this recoverable category needs a counterparty company');
  });

  test('TC-E-017 — ICD with a counterparty is accepted', async ({ adminPage, runId }) => {
    await fillRecoverableRequestForm(adminPage, runId, {
      category: 'icd',
      counterparty: `Harith Infra ${runId}`,
      expectedReturn: '2027-03-31',
      notes: 'Recover on maturity'
    });
    const { id } = await submitAndExpectAccepted(adminPage);
    expect(id).toBeGreaterThan(0);
  });

  test('TC-E-018 — Security deposit without a counterparty is refused (pins the plan-vs-code divergence)', async ({ adminPage, runId }) => {
    await fillRecoverableRequestForm(adminPage, runId, { category: 'security_deposit', expectedReturn: '2027-03-31', notes: 'On vacating' });
    await submitAndExpectRejected(adminPage, 'this recoverable category needs a counterparty company');
  });

  test('TC-E-019 — Security deposit with a counterparty is accepted', async ({ adminPage, runId }) => {
    await fillRecoverableRequestForm(adminPage, runId, {
      category: 'security_deposit',
      counterparty: `Whitefield Estates ${runId}`,
      expectedReturn: '2027-03-31',
      notes: 'On vacating'
    });
    const { id } = await submitAndExpectAccepted(adminPage);
    expect(id).toBeGreaterThan(0);
  });

  test('TC-E-020 — Employee advance needs neither project nor counterparty; the payee auto-records the requester', async ({ adminPage, runId }) => {
    await fillRecoverableRequestForm(adminPage, runId, {
      category: 'employee_advance',
      expectedReturn: '2027-03-31',
      notes: 'Settle against expense claims'
    });
    const { id } = await submitAndExpectAccepted(adminPage);
    const body = (await probeGet(adminPage, `/requests/${id}`)).body;
    expect(body, 'employee_advance forces VendorPayee to the requester — "Paid to" is the label used with no vendor row').toContain('Paid to');
    expect(body, 'and the payee is the person who raised it').toContain('Fervid Admin');
  });

  test('TC-E-021 — Other needs neither project nor counterparty', async ({ adminPage, runId }) => {
    await fillRecoverableRequestForm(adminPage, runId, { category: 'other', expectedReturn: '2027-03-31', notes: 'Misc recoverable' });
    const { id } = await submitAndExpectAccepted(adminPage);
    expect(id).toBeGreaterThan(0);
  });

  test('TC-E-022 — Expected return date is required for every recoverable category', async ({ adminPage, runId }) => {
    await fillRecoverableRequestForm(adminPage, runId, { category: 'other', expectedReturn: '', notes: 'Misc recoverable' });
    await submitAndExpectRejected(adminPage, 'expected return date is required for recoverables');
  });

  test('TC-E-023 — Repayment/refund terms is required for every recoverable category', async ({ adminPage, runId }) => {
    // One approver, reused for both submissions below: fillRecoverableRequestForm's
    // auto-created approver is keyed by runId alone, and this test calls it twice — a
    // second createApproverUser with the same runId would collide on the unique email.
    const approverName = (await createApproverUser(adminPage, runId)).name;

    await fillRecoverableRequestForm(adminPage, runId, { category: 'other', expectedReturn: '2027-03-31', notes: '', approverName });
    await submitAndExpectRejected(adminPage, 'repayment or refund terms are required for recoverables');

    // Whitespace-only must be refused identically (strings.TrimSpace, requests.go:181).
    await fillRecoverableRequestForm(adminPage, runId, { category: 'other', expectedReturn: '2027-03-31', notes: '   ', approverName });
    await submitAndExpectRejected(adminPage, 'repayment or refund terms are required for recoverables');
  });

  test('TC-E-024 / 025 / 026 — A brand-new category with a "both" rule is honoured through direct submission', async ({ adminPage, browser, runId }) => {
    const name = `Retention combo ${runId}`;
    await createCategoryThroughScreen(adminPage, name, 'both');
    const code = `retention_combo_${runId}`.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '');

    const requester = await asRole(adminPage, browser, runId, ['Requester']);
    const managerSubject = await asRole(adminPage, browser, runId, ['Manager']);
    try {
      const managerId = await approverIdFor(requester.page, managerSubject.subject.name);
      const proj = await projectId(requester.page, 'Operations');
      const base = {
        treatment: 'recoverable',
        type: 'recoverable',
        vendor_payee: `Ridge Metro ${runId}`,
        recoverable_category: code,
        short_title: `Retention combo ${runId}`,
        purpose: 'Retention held on the contract',
        amount: '250000',
        expected_return_date: '2027-06-30',
        repayment_notes: 'Release at defect-liability end',
        manager_id: managerId
      };

      // TC-E-024: no project.
      const missingProject = await probePost(requester.page, '/requests', { ...base, counterparty: `Ridge Metro ${runId}` });
      expect(missingProject.status, 'the new category requires a project; omitting it must be refused').toBe(400);
      expect(missingProject.body).toContain('this recoverable category always belongs to a project');

      // TC-E-025: project present, no counterparty.
      const missingCounterparty = await probePost(requester.page, '/requests', { ...base, project_id: proj });
      expect(missingCounterparty.status, 'the new category requires a counterparty; omitting it must be refused').toBe(400);
      expect(missingCounterparty.body).toContain('this recoverable category needs a counterparty company');

      // TC-E-026: both present — accepted, and resolves to the new category by name.
      const accepted = await probePost(requester.page, '/requests', {
        ...base,
        project_id: proj,
        counterparty: `Ridge Metro ${runId}`
      });
      expect(accepted.status, 'both fields present must be accepted — the rule truly comes from the table').toBe(303);
      const requestId = Number(accepted.location?.match(/\/requests\/(\d+)\/submitted/)?.[1]);
      const detail = await probeGet(adminPage, `/recoverables/${requestId}`);
      expect(detail.body).toContain(name);
    } finally {
      await requester.close();
      await managerSubject.close();
    }
  });

  // -------------------------------------------------------------------------
  // Section 3 — Exclusion from budget actuals (V2, V3), before/after
  // -------------------------------------------------------------------------

  const HEAD = 'Office Rent';
  const currentMonth = () => new Date().toISOString().slice(0, 7);
  const today = () => new Date().toISOString().slice(0, 10);

  test('TC-E-027 — Baseline: grid and per-head report agree before any change', async ({ adminPage }) => {
    const month = currentMonth();
    const gridActual = await gridActualForHead(adminPage, month, HEAD);
    const reportActual = await reportHeadsActual(adminPage, month, HEAD);
    expect(reportActual, 'the grid and the per-head report must never legitimately disagree').toBe(gridActual);
  });

  test('TC-E-028 — A settled budget payment raises the grid actual by exactly the paid amount', async ({ adminPage, runId }) => {
    const month = currentMonth();
    const before = await gridActualForHead(adminPage, month, HEAD);

    const { id } = await createApprovedRequest(adminPage, runId, { amount: '700.00' });
    await settlePayment(adminPage, id, { amount: '700.00', paidOn: today() });

    const after = await gridActualForHead(adminPage, month, HEAD);
    expect(after - before, 'the grid actual must rise by exactly the paid amount').toBe(70000);
  });

  test('TC-E-029 — The rise is reflected in the per-head report and both CSV exports', async ({ adminPage, runId }) => {
    const month = currentMonth();
    const before = await reportHeadsActual(adminPage, month, HEAD);
    const beforeCsvGrid = await csvActual(adminPage, `/export.csv?month=${month}`, [{ col: 1, value: HEAD }], 3);
    const beforeCsvYtd = await csvActual(adminPage, `/reports/ytd.csv?from=${month}&to=${month}`, [{ col: 2, value: HEAD }], 4);

    const { id } = await createApprovedRequest(adminPage, runId, { amount: '450.00' });
    await settlePayment(adminPage, id, { amount: '450.00', paidOn: today() });

    const after = await reportHeadsActual(adminPage, month, HEAD);
    const afterCsvGrid = await csvActual(adminPage, `/export.csv?month=${month}`, [{ col: 1, value: HEAD }], 3);
    const afterCsvYtd = await csvActual(adminPage, `/reports/ytd.csv?from=${month}&to=${month}`, [{ col: 2, value: HEAD }], 4);

    expect(after - before, 'the per-head report must rise by exactly the paid amount').toBe(45000);
    expect(afterCsvGrid, 'the grid CSV export must agree with the on-screen report figure').toBe(after);
    expect(afterCsvYtd, 'the YTD CSV export must agree with the on-screen report figure').toBe(after);
    expect(afterCsvGrid - beforeCsvGrid).toBe(45000);
    expect(afterCsvYtd - beforeCsvYtd).toBe(45000);
  });

  /**
   * Reserves an approved request and posts the settlement exactly as the real
   * "Confirm and save payment" button would (same route, same fields), without
   * driving the confirmation sheet's UI — this is what lets TC-E-030 collect
   * evidence across five categories in one pass instead of five slow UI flows.
   */
  async function attemptSettleRecoverable(page: Page, id: number, amount: string) {
    const reserve = await probePost(page, `/requests/${id}/record-payment`, {});
    if (reserve.status !== 303) throw new Error(`could not reserve request ${id} for settlement: ${reserve.outcome}`);
    return probePost(page, '/payments', {
      request_id: String(id),
      head_id: '0', // exactly what payments/new's hidden field carries for a recoverable request (linking.go:242-244)
      amount,
      paid_on: today(),
      payment_mode: 'bank_transfer',
      reference_no: `UTR-${id}`,
      settlement: 'settled'
    });
  }

  test('TC-E-030 — A recoverable request raised through the real screens can be reserved, paid and closed, in every category', async ({
    adminPage,
    browser,
    runId
  }) => {
    // Regression guard for F-E-01. The recoverable fieldset still collects no head
    // — none of its categories has one to collect — so payments/new still leaves
    // the hidden head_id at 0. The fix is in the data model: migration v8 made
    // `payments.head_id` nullable and `validatePayment` now requires a head only
    // for a budget-treatment payment (store.go:1741), so a headless recoverable
    // settlement is written with head_id NULL instead of being refused. All five
    // categories are driven — one that requires a project (EMD), two that require
    // a counterparty (ICD, Security deposit) and two that require neither
    // (Employee advance, Other) — because the whole finding was that the category
    // decided whether approved money could be paid at all.
    const approver = await createApproverUser(adminPage, runId);
    const cases: Array<{ category: RecoverableFormOpts['category']; opts: Partial<RecoverableFormOpts> }> = [
      { category: 'emd', opts: { projectLabel: 'Operations' } },
      { category: 'icd', opts: { counterparty: `Beacon Infra ${runId}` } },
      { category: 'security_deposit', opts: { counterparty: `Whitefield Estates ${runId}` } },
      { category: 'employee_advance', opts: {} },
      { category: 'other', opts: {} }
    ];
    const outcomes: Array<{ category: string; status: number; body: string }> = [];
    for (const c of cases) {
      const { id } = await raiseAndApproveRecoverable(adminPage, browser, runId, approver, {
        category: c.category,
        amount: '50000',
        expectedReturn: '2027-03-31',
        notes: 'Refund on award',
        ...c.opts
      });
      const settle = await attemptSettleRecoverable(adminPage, id, '50000');
      outcomes.push({ category: c.category, status: settle.status, body: settle.body });
    }
    // Every category is driven before any assertion runs, and the outcomes are
    // printed, so one run reports all five rather than stopping at the first —
    // the whole finding was that the *category* decided whether approved money
    // could be paid at all, which a first-failure abort would hide.
    console.log('F-E-01 evidence, one settlement attempt per category:', JSON.stringify(outcomes.map(o => ({ category: o.category, status: o.status }))));
    for (const o of outcomes) {
      expect(o.status, `${o.category}: settlement must succeed (302/303 → /payments/{id}) — got ${o.status}`).toBeGreaterThanOrEqual(300);
      expect(o.status, `${o.category}: settlement must succeed`).toBeLessThan(400);
    }
  });

  test(
    'TC-E-031 — Grid/report stay unchanged after a genuinely paid recoverable',
    async ({ adminPage, browser, runId }) => {
      // Was test.fixme() while F-E-01 stood: no recoverable request raised through
      // the product could be settled, so the "a recoverable payment exists" state
      // this test needs was unreachable. It is reachable now, so the exclusion is
      // proved end to end rather than only at store level
      // (internal/store/recoverables_test.go:470 TestGridAndReportExcludeRecoverablePayments,
      // :516 TestRecoverablePaymentExcludedFromActualsButInRecoverableReport).
      const month = currentMonth();
      const before = await gridActualForHead(adminPage, month, HEAD);
      const approver = await createApproverUser(adminPage, runId);
      const { id } = await raiseAndApproveRecoverable(adminPage, browser, runId, approver, {
        category: 'emd',
        projectLabel: 'Operations',
        amount: '60000',
        expectedReturn: '2027-03-31',
        notes: 'Refund on award'
      });
      await settlePayment(adminPage, id, { amount: '60000', paidOn: today() });
      const after = await gridActualForHead(adminPage, month, HEAD);
      expect(after, 'a settled recoverable payment must not move the grid actual at all').toBe(before);
    }
  );

  test('TC-E-032 — An approved-but-unpaid recoverable contributes nothing to grid or report', async ({ adminPage, browser, runId }) => {
    const month = currentMonth();
    const beforeGrid = await gridActualForHead(adminPage, month, HEAD);
    const beforeReport = await reportHeadsActual(adminPage, month, HEAD);

    const approver = await createApproverUser(adminPage, runId);
    await raiseAndApproveRecoverable(adminPage, browser, runId, approver, {
      category: 'emd',
      projectLabel: 'Operations',
      amount: '80000',
      expectedReturn: '2027-03-31',
      notes: 'Refund on award'
    });

    const afterGrid = await gridActualForHead(adminPage, month, HEAD);
    const afterReport = await reportHeadsActual(adminPage, month, HEAD);
    expect(afterGrid, 'an unpaid recoverable has no payment row and cannot appear in any actual sum').toBe(beforeGrid);
    expect(afterReport).toBe(beforeReport);
  });

  test("TC-E-033 — An unpaid recoverable is listed with zero outstanding and excluded from paid-balance totals", async ({
    adminPage,
    browser,
    runId
  }) => {
    const before = await dashboardMetric(adminPage, 'Outstanding');
    const approver = await createApproverUser(adminPage, runId);
    const { number } = await raiseAndApproveRecoverable(adminPage, browser, runId, approver, {
      category: 'other',
      amount: '35000',
      expectedReturn: '2027-03-31',
      notes: 'Refund pending review'
    });

    const listBody = (await probeGet(adminPage, `/recoverables/list?q=${encodeURIComponent(number)}`)).body;
    expect(listBody, 'the unpaid recoverable must appear in the register').toContain(number);
    expect(listBody, 'an unpaid, approved row reads "Awaiting payment"').toContain('Awaiting payment');

    const after = await dashboardMetric(adminPage, 'Outstanding');
    expect(after.count, 'unpaid approvals do not increase outstanding count').toBe(before.count);
    expect(after.amount, 'unpaid approvals do not increase the outstanding amount').toBe(before.amount);
    const rows = parseCsv((await probeGet(adminPage, `/recoverables/list.csv?q=${encodeURIComponent(number)}`)).body);
    expect(rows).toHaveLength(2);
    expect(parseRupees(rows[1][rows[0].indexOf('Outstanding')])).toBe(0);
    expect(parseRupees(rows[1][rows[0].indexOf('Paid out')])).toBe(0);
  });

  test('TC-E-034 — /recoverables/list.csv total agrees with the on-screen list total', async ({ adminPage }) => {
    await adminPage.goto('/recoverables/list');
    const onScreenTotal = parseRupees(await adminPage.locator('tfoot td[data-label="Outstanding"]').innerText());

    const csvResp = await probeGet(adminPage, '/recoverables/list.csv');
    const rows = parseCsv(csvResp.body);
    const header = rows[0];
    const amountCol = header.indexOf('Outstanding');
    expect(amountCol, 'CSV has an explicit outstanding column').toBeGreaterThanOrEqual(0);
    const csvTotal = rows
      .slice(1)
      .filter(r => r.length > 1)
      .reduce((sum, r) => sum + parseRupees(r[amountCol]), 0);

    expect(csvTotal, 'the CSV total must equal the on-screen tfoot total').toBe(onScreenTotal);
  });

  // -------------------------------------------------------------------------
  // Section 4 — Register, dashboard, detail
  // -------------------------------------------------------------------------

  test('TC-E-035 — Dashboard renders its metric strip, rollups and the two explanatory banners', async ({ adminPage }) => {
    const body = (await probeGet(adminPage, '/recoverables')).body;
    for (const want of [
      'Kept out of budget actuals on purpose',
      'Track each return and reconciliation',
      'Outstanding',
      'Past expected return',
      'Due in 30 days',
      'Paid out this month',
      'By category',
      'By counterparty'
    ]) {
      expect(body, `dashboard must contain "${want}"`).toContain(want);
    }
  });

  test('TC-E-036 — An unpaid recoverable never reads as overdue, however old its expected return date', async ({ adminPage, browser, runId }) => {
    const before = await dashboardMetric(adminPage, 'Past expected return');

    const approver = await createApproverUser(adminPage, runId);
    const { number } = await raiseAndApproveRecoverable(adminPage, browser, runId, approver, {
      category: 'other',
      amount: '12000',
      expectedReturn: '2020-01-01', // far in the past
      notes: 'Ageing boundary probe'
    });

    const listBody = (await probeGet(adminPage, `/recoverables/list?q=${encodeURIComponent(number)}`)).body;
    const rowStart = listBody.indexOf('>' + number + '<');
    const rowChunk = listBody.slice(rowStart, rowStart + 1200);
    expect(rowChunk, 'money that never left cannot be overdue: the pill must read "Awaiting payment"').toContain('Awaiting payment');
    expect(rowChunk, 'the row must not render a "days overdue" pill').not.toMatch(/\d+ days overdue/);
    expect(rowChunk, 'the ageing pill tone must be "approved", not "bad"').toContain('pill approved');
    void before;
  });

  test('TC-E-036b — [F-E-05] The dashboard\'s "Past expected return" tile applies the same "unpaid cannot be overdue" rule as the row', async ({
    adminPage,
    browser,
    runId
  }) => {
    // F-E-05, fixed. RecoverableMetrics' OverdueAmount/OverdueCount used to count
    // by `expected_return_date < today` alone, with no `paid_on <> ''` guard —
    // unlike recoverableAgeing, which checks paidOn=="" first. An unpaid, past-due
    // recoverable therefore inflated this dashboard tile while its own row
    // correctly read "Awaiting payment". The metric now applies the same guard as
    // the row. This case is what keeps the two agreeing: money that never left
    // cannot be overdue, on the tile as well as in the register.
    const before = await dashboardMetric(adminPage, 'Past expected return');
    const approver = await createApproverUser(adminPage, runId);
    await raiseAndApproveRecoverable(adminPage, browser, runId, approver, {
      category: 'other',
      amount: '13000',
      expectedReturn: '2020-01-01',
      notes: 'F-E-05 probe'
    });
    const after = await dashboardMetric(adminPage, 'Past expected return');
    expect(after.count, 'an unpaid recoverable must never move the "Past expected return" tile').toBe(before.count);
  });

  test('TC-E-037 — /recoverables/list.csv reaches the export handler, not the {id} detail handler', async ({ adminPage }) => {
    const resp = await probeGet(adminPage, '/recoverables/list.csv');
    expect(resp.status).toBe(200);
    expect(resp.body.startsWith('Number,Category,Counterparty,Project,Outstanding,Paid out,Recovered or reconciled,Paid On,Expected Return,Ageing,Status,Requester,Repayment Notes')).toBe(
      true
    );
  });

  test('TC-E-038 — /recoverables/{id} for a non-recoverable request answers 404', async ({ adminPage, runId }) => {
    const { id } = await createApprovedRequest(adminPage, runId, { amount: '100.00' });
    const resp = await probeGet(adminPage, `/recoverables/${id}`);
    expect(resp.status, 'a budget request must not be reachable through the recoverable detail screen').toBe(404);
  });

  test('TC-E-039 — ageing=unpaid narrows the register correctly', async ({ adminPage, browser, runId }) => {
    const approver = await createApproverUser(adminPage, runId);
    const { number } = await raiseAndApproveRecoverable(adminPage, browser, runId, approver, {
      category: 'other',
      amount: '9000',
      expectedReturn: '2027-01-01',
      notes: 'Unpaid filter probe'
    });
    const body = (await probeGet(adminPage, `/recoverables/list?ageing=unpaid&q=${encodeURIComponent(number)}`)).body;
    expect(body).toContain(number);
    expect(body).toContain('Not yet paid');
  });

  test("TC-E-040 — Dashboard Outstanding metric and category rollup agree with the list's own footer total", async ({ adminPage }) => {
    await adminPage.goto('/recoverables/list');
    const listTotal = parseRupees(await adminPage.locator('tfoot td[data-label="Outstanding"]').innerText());
    const listAmounts = await adminPage.locator('tbody td[data-label="Outstanding"]').allInnerTexts();
    expect(listAmounts.reduce((sum, amount) => sum + parseRupees(amount), 0), 'list rows sum to their footer').toBe(listTotal);
    await adminPage.goto('/recoverables');
    const categoryTable = adminPage.locator('table').first();
    const categoryTotal = parseRupees(await categoryTable.locator('tfoot td[data-label="Outstanding"]').innerText());
    const categoryRows = await categoryTable.locator('tbody td[data-label="Outstanding"]').allInnerTexts();
    expect(categoryRows.reduce((sum, amount) => sum + parseRupees(amount), 0), 'category rows sum to their footer').toBe(categoryTotal);
    expect(categoryTotal, 'category and list full-precision totals agree').toBe(listTotal);
    const subtitle = await adminPage.locator('.page-banner .sub').innerText();
    expect(parseRupees(subtitle.split(' outstanding')[0]), 'dashboard full-precision summary agrees with the list').toBe(listTotal);
    await expect(adminPage.locator('.metric').filter({ has: adminPage.locator('.metric-label', { hasText: /^Outstanding$/ }) }).locator('.metric-value')).not.toBeEmpty();
  });

  test('TC-E-040b — Outstanding dashboard drilldowns exclude unpaid and reconciled siblings in the same category', async ({ adminPage, browser, runId }, testInfo) => {
    const approver = await createApproverUser(adminPage, runId);
    const counterparty = `Drilldown Company ${runId}`;
    const records: Array<{ id: number; number: string }> = [];
    for (const [index, amount] of ['7000', '8000', '3000'].entries()) {
      const record = await raiseAndApproveRecoverable(adminPage, browser, `${runId}-${index}`, approver, {
        category: 'icd', counterparty, amount, expectedReturn: '2027-12-31', notes: 'Same category and counterparty, distinct payout/recovery states'
      });
      records.push(record);
      if (index === 1) continue;
      await settlePayment(adminPage, record.id, { amount, paidOn: today() });
      await adminPage.goto(`/recoverables/${record.id}`);
      const token = await adminPage.locator('#record-recovery input[name="token"]').inputValue();
      const result = await probePost(adminPage, `/recoverables/${record.id}/events`, { kind: 'return', amount: index === 0 ? '2000' : '3000', occurred_on: today(), reference: `DRILL-${runId}-${index}`, note: 'Bank statement confirms this return; retained for dashboard drilldown evidence.', token });
      expect(result.status).toBe(303);
    }
    for (const [tag, viewport] of [['desktop', { width: 1440, height: 900 }], ['mobile', { width: 390, height: 844 }]] as const) {
      await adminPage.setViewportSize(viewport);
      await adminPage.goto('/recoverables');
      const categoryRow = adminPage.locator('tbody tr', { has: adminPage.locator('td[data-label="Category"] a', { hasText: /^ICD$/ }) });
      const categoryCount = Number(await categoryRow.locator('td[data-label="Count"]').innerText());
      const categoryAmount = parseRupees(await categoryRow.locator('td[data-label="Outstanding"]').innerText());
      await categoryRow.getByRole('link', { name: 'ICD', exact: true }).click();
      expect(new URL(adminPage.url()).searchParams.get('ageing')).toBe('outstanding');
      await expect(adminPage.locator('tbody a[href^="/recoverables/"]')).toHaveCount(categoryCount);
      expect(parseRupees(await adminPage.locator('tfoot td[data-label="Outstanding"]').innerText())).toBe(categoryAmount);
      await expect(adminPage.locator(`tbody a[href="/recoverables/${records[0].id}"]`)).toHaveCount(1);
      await expect(adminPage.locator(`tbody a[href="/recoverables/${records[1].id}"]`)).toHaveCount(0);
      await expect(adminPage.locator(`tbody a[href="/recoverables/${records[2].id}"]`)).toHaveCount(0);
      await adminPage.goto('/recoverables');
      await adminPage.locator('td[data-label="Counterparty"]').getByRole('link', { name: counterparty, exact: true }).click();
      await expect(adminPage.locator('tbody a[href^="/recoverables/"]')).toHaveCount(1);
      expect(parseRupees(await adminPage.locator('tfoot td[data-label="Outstanding"]').innerText())).toBe(500000);
      await adminPage.screenshot({ path: testInfo.outputPath(`outstanding-drilldown-${tag}.png`), fullPage: true });
      const exported = parseCsv((await probeGet(adminPage, '/recoverables/list.csv' + new URL(adminPage.url()).search)).body);
      expect(exported).toHaveLength(2);
      expect(exported[1][0]).toBe(records[0].number);
      await adminPage.goto(`/recoverables/list?counterparty=${encodeURIComponent(counterparty)}`);
      await expect(adminPage.locator('tbody a[href^="/recoverables/"]')).toHaveCount(3);
      if (tag === 'mobile') await adminPage.locator('.recovery-filters > summary').click();
      const form = tag === 'mobile' ? adminPage.locator('.recovery-filters form') : adminPage.locator('form.toolbar');
      const ageing = form.locator('select[name="ageing"]');
      for (const [value, expectedId] of [['unpaid', records[1].id], ['recovered', records[2].id], ['outstanding', records[0].id]] as const) {
        await ageing.selectOption(value);
        await form.getByRole('button', { name: /^Apply/ }).click();
        await expect(adminPage.locator('tbody a[href^="/recoverables/"]')).toHaveCount(1);
        await expect(adminPage.locator(`tbody a[href="/recoverables/${expectedId}"]`)).toHaveCount(1);
      }
    }
  });

  test('TC-E-041 — .metric-foot now carries a real CSS rule (informational)', async ({ adminPage }) => {
    await adminPage.goto('/recoverables');
    const foot = adminPage.locator('.metric-foot').first();
    await expect(foot).toBeVisible();
    const fontSize = await foot.evaluate(el => getComputedStyle(el).fontSize);
    expect(fontSize, '.metric-foot must not render at the browser default font size').not.toBe('16px');
  });

  // -------------------------------------------------------------------------
  // Section 5 — Payout classification remains independent of recovery
  // -------------------------------------------------------------------------

  test('TC-E-042 — Category and expected-return survive settlement', async ({ adminPage, browser, runId }) => {
    // Was test.fixme() while F-E-01 stood, because settling any real-UI recoverable
    // request failed. Store-level proof of the same property:
    // internal/store/recoverables_test.go:793 TestRecoverableRequestClosesOnPaymentRetainingClassification.
    const approver = await createApproverUser(adminPage, runId);
    const { id } = await raiseAndApproveRecoverable(adminPage, browser, runId, approver, {
      category: 'emd',
      projectLabel: 'Operations',
      amount: '40000',
      expectedReturn: '2027-08-31',
      notes: 'Refund on award'
    });
    await settlePayment(adminPage, id, { amount: '40000', paidOn: today() });
    const body = (await probeGet(adminPage, `/recoverables/${id}`)).body;
    expect(body).toContain('EMD');
    expect(body).toContain('2027-08-31');
  });

  test('TC-E-043 — Before settlement, the detail screen already shows category and expected return correctly', async ({
    adminPage,
    browser,
    runId
  }) => {
    const approver = await createApproverUser(adminPage, runId);
    const { id } = await raiseAndApproveRecoverable(adminPage, browser, runId, approver, {
      category: 'icd',
      counterparty: `Beacon Infra ${runId}`,
      amount: '90000',
      expectedReturn: '2027-09-30',
      notes: 'Recover on maturity'
    });
    const body = (await probeGet(adminPage, `/recoverables/${id}`)).body;
    expect(body).toContain('ICD');
    expect(body).toContain(`Beacon Infra ${runId}`);
    expect(body).toContain('2027-09-30');
    expect(body).toContain('Recover on maturity');
    expect(body, 'unpaid recoverables must say so rather than render an empty payment block').toContain('Approved but not yet paid');
  });

  // -------------------------------------------------------------------------
  // Section 6 — Linked project (V8)
  // -------------------------------------------------------------------------

  test("TC-E-044 — An EMD linked to a real project shows in that project's register row while absent from its grid actuals", async ({
    adminPage,
    browser,
    runId
  }) => {
    const month = currentMonth();
    const beforeGrid = await gridActualForHead(adminPage, month, HEAD);

    const approver = await createApproverUser(adminPage, runId);
    const { number } = await raiseAndApproveRecoverable(adminPage, browser, runId, approver, {
      category: 'emd',
      projectLabel: 'Operations',
      amount: '55000',
      expectedReturn: '2027-03-31',
      notes: 'Refund on award'
    });

    const listBody = (await probeGet(adminPage, `/recoverables/list?q=${encodeURIComponent(number)}`)).body;
    const rowStart = listBody.indexOf('>' + number + '<');
    expect(listBody.slice(rowStart, rowStart + 800), 'the register row must show the linked project').toContain('Operations');

    const afterGrid = await gridActualForHead(adminPage, month, HEAD);
    expect(afterGrid, "the project's grid actual is unaffected (still unpaid, and would be excluded if paid)").toBe(beforeGrid);
  });

  test('TC-E-045 — A recoverable with no project reads "Not project linked" everywhere', async ({ adminPage, browser, runId }) => {
    const approver = await createApproverUser(adminPage, runId);
    const { id, number } = await raiseAndApproveRecoverable(adminPage, browser, runId, approver, {
      category: 'icd',
      counterparty: `Harith Infra ${runId}`,
      amount: '30000',
      expectedReturn: '2027-03-31',
      notes: 'Recover on maturity'
    });

    const listBody = (await probeGet(adminPage, `/recoverables/list?q=${encodeURIComponent(number)}`)).body;
    const rowStart = listBody.indexOf('>' + number + '<');
    expect(listBody.slice(rowStart, rowStart + 800)).toContain('Not project linked');

    const detailBody = (await probeGet(adminPage, `/recoverables/${id}`)).body;
    expect(detailBody).toContain('Not project linked');
  });

  // -------------------------------------------------------------------------
  // Section 7 — Canonical recovery lifecycle and unsupported legacy aliases
  // -------------------------------------------------------------------------

  test('TC-E-046 / 047 — Unsupported repay aliases remain unavailable (canonical writes use events)', async ({ adminPage }) => {
    for (const path of ['/recoverables/1/repay', '/recoverables/list/repay', '/configuration/recoverable-categories/1/repay']) {
      const get = await probeGet(adminPage, path);
      expect(get.status, `GET ${path} must not exist`).toBe(404);
      const post = await probePost(adminPage, path, { amount: '100000' });
      expectOutcome(post, [404, 405], `POST ${path} must not exist`);
    }
  });

  test('TC-E-048 / 049 — Unsupported direct forfeiture/write-off aliases remain unavailable', async ({ adminPage }) => {
    for (const path of ['/recoverables/1/forfeit', '/recoverables/1/write-off', '/recoverables/list/forfeit']) {
      const get = await probeGet(adminPage, path);
      expect(get.status, `GET ${path} must not exist`).toBe(404);
      const post = await probePost(adminPage, path, { reason: 'defaulted' });
      expectOutcome(post, [404, 405], `POST ${path} must not exist`);
    }
  });

  test('TC-E-050 — Accounts records a dated recovery, retries safely, refuses excess and retains reconciled history', async ({ adminPage, browser, runId }, testInfo) => {
    const approver = await createApproverUser(adminPage, runId);
    const { id, number } = await raiseAndApproveRecoverable(adminPage, browser, runId, approver, {
      category: 'other', amount: '7000', expectedReturn: '2027-03-31', notes: 'Recovery lifecycle audit'
    });
    await settlePayment(adminPage, id, { amount: '7000', paidOn: today() });
    const accounts = await asRole(adminPage, browser, runId, ['Accounts']);
    try {
      const page = accounts.page;
      await page.setViewportSize({ width: 390, height: 844 });
      await page.goto(`/recoverables/${id}`);
      const balance = () => page.locator('dt').filter({ hasText: /^Outstanding balance$/ }).locator('xpath=following-sibling::dd[1]');
      expect(parseRupees(await balance().innerText())).toBe(700000);
      const token = await page.locator('#record-recovery input[name="token"]').inputValue();
      const fields = { kind: 'return', amount: '2000', occurred_on: today(), reference: `UTR-${runId}`, note: 'Receipt filed against the bank statement for this dated return.', token };
      await page.getByLabel('Recovery type').selectOption('return');
      await page.getByLabel('Amount received or reconciled').fill(fields.amount);
      await page.getByLabel('Recovery date').fill(fields.occurred_on);
      await page.getByLabel('Bank / accounting reference').fill(fields.reference);
      await page.getByLabel('Evidence and explanation').fill(fields.note);
      await page.getByLabel('Amount received or reconciled').fill('8000');
      page.once('dialog', dialog => dialog.accept());
      await page.getByRole('button', { name: 'Record recovery', exact: true }).click();
      const recoveryError = page.locator('#recovery-error');
      await expect(recoveryError).toBeFocused();
      await expect(recoveryError).toBeInViewport();
      await expect(page.getByLabel('Bank / accounting reference')).toHaveValue(fields.reference);
      await expect(page.getByLabel('Evidence and explanation')).toHaveValue(fields.note);
      expect(parseRupees(await balance().innerText())).toBe(700000);
      await page.getByLabel('Amount received or reconciled').fill(fields.amount);
      page.once('dialog', dialog => dialog.accept());
      await page.getByRole('button', { name: 'Record recovery', exact: true }).click();
      await expect(page).toHaveURL(new RegExp(`/recoverables/${id}\\?saved=1#recovery-history$`));
      expect(parseRupees(await balance().innerText())).toBe(500000);
      await expect(page.locator('#recovery-history')).toContainText(fields.reference);
      await expect(page.locator('#recovery-history')).toContainText(fields.occurred_on);
      await expect(page.locator('#recovery-history')).toContainText('₹2,000.00');
      await page.screenshot({ path: testInfo.outputPath('recovery-2000-of-7000-mobile.png'), fullPage: true });
      const retry = await probePost(page, `/recoverables/${id}/events`, fields);
      expect(retry.status, 'retrying the identical submission is idempotent').toBe(303);
      await page.reload();
      expect(parseRupees(await balance().innerText())).toBe(500000);
      await expect(page.locator('#recovery-history .thread > li')).toHaveCount(1);
      const nextToken = await page.locator('#record-recovery input[name="token"]').inputValue();
      const excess = await probePost(page, `/recoverables/${id}/events`, { ...fields, token: nextToken, amount: '6000' });
      expect(excess.status, 'cannot record more than the remaining balance').toBe(422);
      expect(excess.body).toContain('outstanding balance');
      expect(excess.body).toContain('value="6000"');
      expect(excess.body).toContain(fields.reference);
      const finish = await probePost(page, `/recoverables/${id}/events`, { ...fields, token: nextToken, kind: 'expense', amount: '5000', reference: `EXP-${runId}`, note: 'Approved expense report reconciles the remaining advance; evidence retained in finance.' });
      expect(finish.status).toBe(303);
      await page.reload();
      expect(parseRupees(await balance().innerText())).toBe(0);
      await expect(page.locator('#recovery-history .thread > li')).toHaveCount(2);
      await expect(page.locator('.rh-status')).toContainText('Completed');
      await expect(page.locator('.rh-status')).toContainText('Reconciled');
      await expect(page.locator('#record-recovery')).toHaveCount(0);
      const rows = parseCsv((await probeGet(page, `/recoverables/list.csv?q=${encodeURIComponent(number)}&ageing=recovered`)).body);
      expect(rows).toHaveLength(2);
      expect(parseRupees(rows[1][rows[0].indexOf('Outstanding')])).toBe(0);
      expect(parseRupees(rows[1][rows[0].indexOf('Paid out')])).toBe(700000);
      expect(parseRupees(rows[1][rows[0].indexOf('Recovered or reconciled')])).toBe(700000);
      await page.screenshot({ path: testInfo.outputPath('recovery-reconciled-mobile.png'), fullPage: true });
    } finally { await accounts.close(); }
  });

  // -------------------------------------------------------------------------
  // Section 8 — Permissions
  // -------------------------------------------------------------------------

  async function withRecoverableId(adminPage: Page, browser: Browser, runId: string): Promise<number> {
    const approver = await createApproverUser(adminPage, runId);
    const { id } = await raiseAndApproveRecoverable(adminPage, browser, runId, approver, {
      category: 'other',
      amount: '5000',
      expectedReturn: '2027-03-31',
      notes: 'Permission probe'
    });
    return id;
  }

  test('TC-E-051 — Requester refused recoverable_report:view on all three view routes', async ({ adminPage, browser, runId }) => {
    const id = await withRecoverableId(adminPage, browser, runId);
    const requester = await asRole(adminPage, browser, runId, ['Requester']);
    try {
      for (const path of ['/recoverables', '/recoverables/list', `/recoverables/${id}`]) {
        const resp = await probeGet(requester.page, path);
        expect(resp.status, `Requester must be refused ${path}`).toBe(403);
      }
      const write = await probePost(requester.page, `/recoverables/${id}/events`, { kind: 'return', amount: '100', occurred_on: today(), reference: 'AUTH-PROBE', note: 'Must never be recorded', token: `refused-${runId}` });
      expect(write.status, 'Requester must be refused the canonical recovery write route').toBe(403);
    } finally {
      await requester.close();
    }
  });

  test('TC-E-052 — Manager refused recoverable_report:view on all three view routes', async ({ adminPage, browser, runId }) => {
    const id = await withRecoverableId(adminPage, browser, runId);
    const manager = await asRole(adminPage, browser, runId, ['Manager']);
    try {
      for (const path of ['/recoverables', '/recoverables/list', `/recoverables/${id}`]) {
        const resp = await probeGet(manager.page, path);
        expect(resp.status, `Manager must be refused ${path}`).toBe(403);
      }
      const write = await probePost(manager.page, `/recoverables/${id}/events`, { kind: 'return', amount: '100', occurred_on: today(), reference: 'AUTH-PROBE', note: 'Must never be recorded', token: `refused-${runId}` });
      expect(write.status, 'Manager must be refused the canonical recovery write route').toBe(403);
    } finally {
      await manager.close();
    }
  });

  test('TC-E-053 — Accounts granted recoverable_report:view on all three view routes', async ({ adminPage, browser, runId }) => {
    const id = await withRecoverableId(adminPage, browser, runId);
    const accounts = await asRole(adminPage, browser, runId, ['Accounts']);
    try {
      for (const path of ['/recoverables', '/recoverables/list', `/recoverables/${id}`]) {
        const resp = await probeGet(accounts.page, path);
        expect(resp.status, `Accounts must be granted ${path}`).toBe(200);
      }
    } finally {
      await accounts.close();
    }
  });

  test('TC-E-054 — Requester/Manager refused recoverable_report:export; Accounts granted it', async ({ adminPage, browser, runId }) => {
    const requester = await asRole(adminPage, browser, runId, ['Requester']);
    const manager = await asRole(adminPage, browser, runId, ['Manager']);
    const accounts = await asRole(adminPage, browser, runId, ['Accounts']);
    try {
      expect((await probeGet(requester.page, '/recoverables/list.csv')).status).toBe(403);
      expect((await probeGet(manager.page, '/recoverables/list.csv')).status).toBe(403);
      expect((await probeGet(accounts.page, '/recoverables/list.csv')).status).toBe(200);
    } finally {
      await requester.close();
      await manager.close();
      await accounts.close();
    }
  });

  test('TC-E-055 — Admin holds every recoverable verb; anonymous refused all four routes', async ({ adminPage, browser, runId, baseURL }) => {
    const id = await withRecoverableId(adminPage, browser, runId);
    const admin = await asRole(adminPage, browser, runId, ['Admin']);
    try {
      for (const path of ['/recoverables', '/recoverables/list', '/recoverables/list.csv', `/recoverables/${id}`]) {
        const resp = await probeGet(admin.page, path);
        expect(resp.status, `Admin must be granted ${path}`).toBe(200);
      }
    } finally {
      await admin.close();
    }

    const base = baseURL ?? 'http://127.0.0.1:4305';
    for (const path of ['/recoverables', '/recoverables/list', '/recoverables/list.csv', `/recoverables/${id}`]) {
      const resp = await probeAnonymous(browser, path, base);
      expect(resp.status, `anonymous must be redirected to login for ${path}`).toBeGreaterThanOrEqual(300);
      expect(resp.status).toBeLessThan(400);
      expect(resp.location).toContain('/login');
    }
  });
});

void capturePageErrors;
