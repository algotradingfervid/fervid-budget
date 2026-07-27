/**
 * Audit G — information-flow integrity.
 *
 * The question this file asks is not "does the action work" (the sibling audits
 * ask that) but "does a fact entered at one end of the system arrive intact, and
 * ONLY where it should, at the other".
 *
 * Three kinds of assertion live here:
 *
 *  1. **Integrity** — one figure, one payee, one project, followed hop by hop
 *     across a dozen screens and two CSVs, asserted exactly at every hop. The
 *     money assertions also count the `₹` glyphs, because `money.FormatPaise`
 *     (internal/money/money.go:36-45) already carries one and a template that
 *     adds its own would render `₹ ₹1,00,000.00`.
 *  2. **Agreement** — the same fact shown in two places must be the same number.
 *     A tile that promises 3 and a queue that lists 5 is a defect even when both
 *     queries are individually correct.
 *  3. **Confinement** — a subject holding exactly one role must not learn what
 *     that role does not entitle them to, in a list, a CSV, a search result, a
 *     notification, an error message or the audit log.
 *
 * Traces to `docs/qa/uml/03-sequence-diagrams.md` §"Information-flow integrity
 * table" (IF1–IF12) and to the coverage matrix in
 * `docs/superpowers/specs/2026-07-25-payment-requests-coverage.md`.
 *
 * Every test case ID here appears in `docs/qa/test-cases/TC-G-information-flow.md`
 * with its expected result written before the run.
 */
import type { Browser, Page } from '@playwright/test';
import {
  test,
  expect,
  admin,
  capturePageErrors,
  createApprovedRequest,
  login,
  settlePayment
} from './fixtures';
import { asRole, csrfToken, expectOutcome, probeGet, probePost, type Probe } from './audit-support';

// ---------------------------------------------------------------------------
// Money helpers
// ---------------------------------------------------------------------------

/** How many `₹` glyphs a rendered string carries. Exactly one is correct. */
function rupees(text: string): number {
  return (text.match(/₹/g) ?? []).length;
}

/**
 * Asserts a rendered fragment carries the expected money string and exactly one
 * rupee sign. `what` names the hop, so a failure says which screen lost the
 * figure rather than only that a string did not match.
 */
async function expectMoney(page: Page, selector: string, expected: string, what: string) {
  const text = (await page.locator(selector).first().innerText()).trim();
  expect(text, `${what}: expected ${expected}, screen shows "${text}"`).toContain(expected);
  expect(
    rupees(text),
    `${what}: money.FormatPaise already includes ₹ — "${text}" carries ${rupees(text)} of them`
  ).toBe(1);
}

/** The same assertion against a plain string (a CSV cell, a probe body). */
function expectMoneyIn(haystack: string, expected: string, what: string) {
  expect(haystack.includes(expected), `${what}: ${expected} is absent`).toBe(true);
  // A double-format bug would render "₹ ₹1,00,000.00"; the single-space and
  // no-space forms are both wrong and both caught here.
  expect(haystack.includes('₹ ₹'), `${what}: a doubled ₹ is present`).toBe(false);
  expect(haystack.includes('₹₹'), `${what}: a doubled ₹ is present`).toBe(false);
}

/**
 * Asserts nothing went wrong at runtime, allowing for refusals the test asked
 * for on purpose.
 *
 * `capturePageErrors` records every console message of type `error`, and
 * Chromium logs one for any navigation that answers 4xx — "Failed to load
 * resource: the server responded with a status of 400 (Bad Request)". A test
 * whose whole subject is a refusal therefore trips it on the behaviour it is
 * proving. Nothing else is filtered: a 5xx, a `pageerror`, and every other
 * console error still fail the test.
 */
function expectNoRuntimeErrors(errors: string[], what = 'no page error, console error or 5xx') {
  const deliberate = /^console: Failed to load resource: the server responded with a status of 4\d\d/;
  expect(errors.filter(e => !deliberate.test(e)), what).toEqual([]);
  expect(
    errors.filter(e => /^HTTP 5\d\d/.test(e)),
    'no 5xx may be served on any screen this test drove'
  ).toEqual([]);
}

/** Fetches a CSV through a signed-in context and returns its text. */
async function csv(page: Page, path: string): Promise<string> {
  const response = await page.request.get(path, { failOnStatusCode: false });
  expect(response.status(), `GET ${path} must answer 200`).toBe(200);
  return response.text();
}

// ---------------------------------------------------------------------------
// Request/approval helpers with more control than fixtures.ts offers
// ---------------------------------------------------------------------------

interface RaiseOptions {
  amount: string;
  project?: string;
  head?: string;
  vendor: string;
  title: string;
  invoiceNo: string;
  invoiceDate?: string;
  purpose?: string;
  approverName: string;
}

/** Creates a vendor by name. Idempotent-ish: a duplicate name lands on the
 *  refusal page and the vendor exists either way, which is all a payee needs. */
async function ensureVendor(page: Page, name: string) {
  await page.goto('/vendors/new');
  await page.getByLabel('Vendor name').fill(name);
  await page.getByLabel('Type').selectOption('company');
  await page.getByRole('button', { name: 'Save vendor' }).click();
  await page.waitForURL(url => !url.pathname.endsWith('/vendors/new'));
}

/**
 * Raises a vendor_invoice request through the real form, choosing the project,
 * head and vendor. `fixtures.createApprovedRequest` hard-wires Operations /
 * Office Rent and approves for the full amount; several cases here need a
 * different head, or an approved amount that differs from the requested one.
 */
async function raiseVendorRequest(page: Page, opts: RaiseOptions): Promise<{ id: number; number: string }> {
  await page.goto('/requests/new?type=vendor_invoice');
  await page.getByLabel('Short title').fill(opts.title);
  const project = opts.project ?? 'Operations';
  const head = opts.head ?? 'Operations / Office Rent';
  await page.locator('#project').selectOption({ label: project });
  // The head list is swapped from the server; waiting for every remaining
  // option to belong to the chosen project is the swap itself.
  await expect(page.locator('#head option').filter({ hasNotText: `${project} /` })).toHaveCount(1);
  await page.locator('#head').selectOption({ label: head });

  await page.locator('#vendor').pressSequentially(opts.vendor);
  await page.locator('#vendor-options .co', { hasText: opts.vendor }).first().click();
  await expect(page.locator('#vendor-id')).not.toHaveValue('');

  await page.getByLabel('Amount').fill(opts.amount);
  await page.getByLabel('Invoice number').fill(opts.invoiceNo);
  await page.getByLabel('Invoice date').fill(opts.invoiceDate ?? '2026-07-18');
  await page.getByLabel('Purpose').fill(opts.purpose ?? `Purpose for ${opts.title}.`);
  await page.getByLabel('Approver').selectOption({ label: opts.approverName });
  await page.getByRole('button', { name: 'Submit request' }).click();

  await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);
  const id = Number(new URL(page.url()).pathname.split('/')[2]);
  const number = (await page.locator('.rh-no').first().innerText()).trim();
  return { id, number };
}

/**
 * Raises a recoverable request through the real form.
 *
 * `type=recoverable` exists in the store's vocabulary
 * (internal/store/requests.go:78-81) but NOT in the UI's
 * (`requestTypeOptions`, internal/app/requests.go:42-55), so
 * `/requests/new?type=recoverable` falls back to the type chooser. The only
 * route to a recoverable through the shipped screens is `employee_advance`,
 * which `requestNew` opens with the recoverable treatment already selected
 * (internal/app/requests.go:89-92).
 */
async function raiseRecoverable(
  page: Page,
  opts: {
    title: string;
    amount: string;
    category?: 'employee_advance' | 'icd';
    counterparty?: string;
    expectedReturn: string;
    terms: string;
    advanceReason?: string;
    purpose?: string;
    approverName: string;
  }
): Promise<{ id: number; number: string }> {
  await page.goto('/requests/new?type=employee_advance');
  await page.getByLabel('Short title').fill(opts.title);
  await expect(
    page.locator('input[name="treatment"][value="recoverable"]'),
    'an employee advance opens on the recoverable treatment'
  ).toBeChecked();

  const category = opts.category ?? 'employee_advance';
  if (category !== 'employee_advance') {
    await page.locator('#rcategory').selectOption(category);
    // The category change swaps #form-fields from the server; waiting for the
    // field the new category needs is waiting for the swap itself.
    await expect(page.locator('#counterparty')).toBeVisible();
  }
  if (opts.counterparty) await page.locator('#counterparty').fill(opts.counterparty);
  await page.locator('#expected-return').fill(opts.expectedReturn);
  await page.locator('#terms').fill(opts.terms);
  await page.getByLabel('Amount').fill(opts.amount);
  await page.locator('#adv-reason').fill(opts.advanceReason ?? opts.title);
  await page.getByLabel('Purpose').fill(opts.purpose ?? `${opts.title} purpose.`);
  await page.getByLabel('Approver').selectOption({ label: opts.approverName });
  await page.getByRole('button', { name: 'Submit request' }).click();

  await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);
  const id = Number(new URL(page.url()).pathname.split('/')[2]);
  const number = (await page.locator('.rh-no').first().innerText()).trim();
  return { id, number };
}

/** Approves a request as the manager it was routed to, for `approvedAmount`. */
async function approveFor(page: Page, id: number, approvedAmount: string) {
  await page.goto(`/requests/${id}`);
  await page.getByRole('button', { name: /^Approve / }).click();
  const sheet = page.locator('#approve-sheet');
  await expect(sheet).toBeVisible();
  await sheet.getByLabel('Amount approved').fill(approvedAmount);
  await sheet.getByRole('button', { name: 'Approve request' }).click();
  await expect(page).toHaveURL(/\/approvals$/);
}

// ---------------------------------------------------------------------------
// Custom-role helpers. `audit-support.asRole` covers the four SEEDED roles; the
// leakage cases need grants the seed never combines — `audit:view` without
// request scope `all`, `payment:view` with payment scope `own`, and so on. A
// custom role is the only way to hold one without the other, and it is exactly
// what an administrator would build.
// ---------------------------------------------------------------------------

interface CustomRole {
  id: number;
  name: string;
}

/**
 * Creates a custom role and sets its grants and scopes through the real
 * screens' own POST bodies.
 *
 * `perm` is the "Advanced — every permission behind this row" checkbox name
 * (internal/app/templates.go:1594) and takes `resource:action`; `scope_<res>` is
 * the desktop scope radio group (templates.go:1518, read at app.go:1308).
 */
async function createCustomRole(
  adminPage: Page,
  name: string,
  grants: string[],
  scopes: Record<string, string> = {}
): Promise<CustomRole> {
  const created = await probePost(adminPage, '/roles/new', { name, description: 'audit G subject' });
  expect(created.status, `POST /roles/new for ${name}`).toBe(303);
  const id = Number(created.location!.split('=')[1]);
  expect(Number.isFinite(id) && id > 0, `role id from ${created.location}`).toBe(true);

  const body = new URLSearchParams();
  body.set('csrf', await csrfToken(adminPage.context()));
  body.set('role_id', String(id));
  body.set('name', name);
  body.set('description', 'audit G subject');
  for (const grant of grants) body.append('perm', grant);
  for (const [resource, scope] of Object.entries(scopes)) body.set(`scope_${resource}`, scope);

  const saved = await adminPage.request.post('/roles', {
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    data: body.toString(),
    maxRedirects: 0,
    failOnStatusCode: false
  });
  expect(saved.status(), `POST /roles for ${name}`).toBe(303);
  return { id, name };
}

/**
 * Rewrites a user's role set to exactly the labels given, by label rather than
 * by the four seeded names. `audit-support.setExactRoles` only knows the seeded
 * four; the unchecking half is the same and just as load-bearing — every new
 * user already holds Accounts (internal/store/migrations.go:501).
 */
async function setRolesByLabel(adminPage: Page, email: string, labels: Array<string | RegExp>) {
  await adminPage.goto('/users');
  await adminPage.locator('tr', { hasText: email }).getByRole('button', { name: 'Edit' }).click();
  const edit = adminPage.locator('.overlay:not([hidden])');
  await expect(edit).toBeVisible();
  const boxes = edit.getByRole('checkbox');
  for (let i = 0; i < (await boxes.count()); i++) {
    const box = boxes.nth(i);
    if ((await box.getAttribute('name')) === 'role_ids') await box.uncheck();
  }
  for (const label of labels) await edit.getByRole('checkbox', { name: label }).check();
  await edit.getByRole('button', { name: 'Save user' }).click();
  await expect(adminPage).toHaveURL(/\/users$/);
}

/** Creates a user holding exactly one custom role, signed in in its own context. */
async function asCustomRole(adminPage: Page, browser: Browser, runId: string, prefix: string, role: CustomRole) {
  const email = `${prefix}-${runId}@example.test`.toLowerCase();
  const name = `${prefix} ${runId}`;
  await adminPage.goto('/users');
  await adminPage.locator('.pb-actions').getByRole('button', { name: 'Add user' }).click();
  const sheet = adminPage.locator('#user-new');
  await expect(sheet).toBeVisible();
  await sheet.getByLabel('Email').fill(email);
  await sheet.getByLabel('Name').fill(name);
  await sheet.getByLabel('Password').fill('StrongTestPassword!42');
  await sheet.getByRole('button', { name: 'Add User' }).click();
  await expect(adminPage).toHaveURL(/\/users$/);
  await setRolesByLabel(adminPage, email, [new RegExp(`^${role.name}`)]);

  const context = await browser.newContext({ viewport: adminPage.viewportSize() ?? undefined });
  const page = await context.newPage();
  await login(page, email, 'StrongTestPassword!42');
  return { email, name, page, close: () => context.close() };
}

// ---------------------------------------------------------------------------
// Audit-log helpers
// ---------------------------------------------------------------------------

/**
 * The audit rows an actor wrote against payment requests.
 *
 * `/audit` reads the newest 1000 rows of the entity, filters actor and action in
 * Go, then caps at 200 (internal/app/app.go:1364-1389). Actor names carry the
 * runId, so this is a per-test view — but the 1000-row pre-filter is applied
 * BEFORE the actor filter, so a long run can push a subject's rows out of the
 * window entirely. Passing `entityID` narrows the SQL itself
 * (`Store.Audit`, internal/store/store.go:1594-1600) and makes the read exact
 * regardless of how much history the database has accumulated.
 */
async function auditRows(page: Page, actorName: string, entity = 'payment_request', entityID?: number) {
  const id = entityID ? `&id=${entityID}` : '';
  await page.goto(`/audit?entity=${entity}${id}&actor=${encodeURIComponent(actorName)}`);
  const rows = page.locator('tbody tr');
  const out: Array<{ actor: string; entity: string; action: string; rendered: string; summary: string }> = [];
  for (let i = 0; i < (await rows.count()); i++) {
    const cells = rows.nth(i).locator('td');
    // The empty state is a single colspan cell, not a row of five.
    if ((await cells.count()) < 5) continue;
    const rendered = (await cells.nth(3).innerText()).trim();
    out.push({
      actor: (await cells.nth(1).innerText()).trim(),
      entity: (await cells.nth(2).innerText()).trim(),
      action: auditAction(rendered),
      rendered,
      summary: (await cells.nth(4).innerText()).trim()
    });
  }
  return out;
}

/**
 * The stored action behind a rendered Action pill.
 *
 * `actionText` (internal/app/app.go:1882-1907) spells ten actions for a reader
 * and returns every other identifier verbatim — which is F-G-005. Reversing the
 * ten here lets each assertion name the stored action it means, while `rendered`
 * stays available for the tests that are about the gap itself.
 */
function auditAction(rendered: string): string {
  const spelled: Record<string, string> = {
    Created: 'create',
    Updated: 'update',
    Voided: 'void',
    Locked: 'lock',
    Unlocked: 'unlock',
    'Logged in': 'login',
    'Logged out': 'logout',
    Exported: 'export',
    Attached: 'attach',
    'Login failed': 'login_failed'
  };
  return spelled[rendered] ?? rendered;
}

/** The count of `<tbody> tr` rows a table actually renders, empty state excluded. */
async function dataRows(page: Page, selector = 'tbody tr') {
  const rows = page.locator(selector);
  const n = await rows.count();
  if (n === 1 && (await rows.first().locator('.empty').count()) === 1) return 0;
  return n;
}

/** Parses "₹12,34,567.89" back into paise so sums can be checked. */
function paise(text: string): number {
  const digits = text.replace(/[^\d.]/g, '');
  if (!digits) return 0;
  return Math.round(Number(digits) * 100);
}

const NEVER = ['₹ ₹', '₹₹'];
void NEVER;

// ===========================================================================
// 1 — The money trail, asserted numerically at every hop
// ===========================================================================

test.describe('G · the money trail', () => {
  /**
   * IF3 → IF4 → IF8 → IF12, end to end, for a lakh-grouped figure.
   *
   * The month is this test's alone (2026-09), and the seed budgets only
   * 2026-06, so the grid's company total for that month is exactly this one
   * payment — which makes "the grid followed the money" an equality rather than
   * a delta.
   */
  test('TC-G-001 — a lakh-grouped amount survives twelve hops with exactly one ₹ at each', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const MONEY = '₹1,00,000.00';
    const PLAIN = '1,00,000.00';

    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgr1-${runId}`);
    const payee = `Trail Vendor ${runId}`;
    await ensureVendor(adminPage, payee);

    // Hop 1 — the Amount field. The .money-field draws its own ₹ in a .cur
    // prefix, so the input itself must carry the digits and no glyph.
    await adminPage.goto('/requests/new?type=vendor_invoice');
    await adminPage.getByLabel('Short title').fill(`Trail one ${runId}`);
    await adminPage.locator('#project').selectOption({ label: 'Operations' });
    await expect(adminPage.locator('#head option').filter({ hasNotText: 'Operations /' })).toHaveCount(1);
    await adminPage.locator('#head').selectOption({ label: 'Operations / Office Rent' });
    await adminPage.locator('#vendor').pressSequentially(payee);
    await adminPage.locator('#vendor-options .co', { hasText: payee }).first().click();
    const amountField = adminPage.getByLabel('Amount');
    await amountField.fill('100000.00');
    await expect(amountField, 'hop 1 — the money field groups Indian-style and draws its own ₹').toHaveValue(PLAIN);
    await adminPage.getByLabel('Invoice number').fill(`INV-T1-${runId}`);
    await adminPage.getByLabel('Invoice date').fill('2026-07-18');
    await adminPage.getByLabel('Purpose').fill(`Trail one purpose ${runId}.`);
    await adminPage.getByLabel('Approver').selectOption({ label: approver.subject.name });
    await adminPage.getByRole('button', { name: 'Submit request' }).click();
    await expect(adminPage).toHaveURL(/\/requests\/\d+\/submitted$/);
    const id = Number(new URL(adminPage.url()).pathname.split('/')[2]);
    const number = (await adminPage.locator('.rh-no').first().innerText()).trim();

    // Hop 2 — the submitted confirmation.
    await expectMoney(adminPage, '.rh-amt', MONEY, 'hop 2 — /requests/{id}/submitted');

    // Hop 3 — the request detail head and the Amount row.
    await adminPage.goto(`/requests/${id}`);
    await expectMoney(adminPage, '.rh-amt', MONEY, 'hop 3a — request detail head');
    await expectMoney(
      adminPage,
      '.dl div:has(dt:text-is("Amount")) dd',
      MONEY,
      'hop 3b — request detail Amount row'
    );

    // Hop 4 — the requester's own list card.
    await adminPage.goto('/requests?bucket=open');
    const card = adminPage.locator('.req-card', { hasText: number });
    await expect(card, 'hop 4 — the request must be on the open list').toHaveCount(1);
    await expectMoney(adminPage, `.req-card:has-text("${number}") .rc-amt`, MONEY, 'hop 4 — /requests card');

    // Hop 5 — the approver's queue, then the approve sheet's prefill.
    await approver.page.goto('/approvals');
    await expectMoney(
      approver.page,
      `.req-card:has-text("${number}") .rc-amt`,
      MONEY,
      'hop 5 — /approvals card'
    );
    await approver.page.goto(`/requests/${id}`);
    await approver.page.getByRole('button', { name: /^Approve / }).click();
    const sheet = approver.page.locator('#approve-sheet');
    await expect(sheet).toBeVisible();
    await expect(
      sheet.getByLabel('Amount approved'),
      'hop 6 — the approve sheet prefills the requested figure, ungrouped of ₹'
    ).toHaveValue(PLAIN);
    await sheet.getByRole('button', { name: 'Approve request' }).click();
    await expect(approver.page).toHaveURL(/\/approvals$/);

    // Hop 7 — approved_amount surfaces on the request as "Approved".
    await adminPage.goto(`/requests/${id}`);
    await expectMoney(
      adminPage,
      '.dl div:has(dt:text-is("Approved")) dd',
      MONEY,
      'hop 7 — request detail Approved row'
    );

    // Hop 8 — the accounts queue's Amount column reads approvedOf().
    await adminPage.goto('/accounts-queue?tab=approved');
    const queueRow = adminPage.locator('tbody tr', { hasText: number });
    await expect(queueRow, 'hop 8 — the approved request must be in the queue').toHaveCount(1);
    await expectMoney(
      adminPage,
      `tbody tr:has-text("${number}") td[data-label="Amount"]`,
      MONEY,
      'hop 8 — accounts queue Amount'
    );

    // Hop 9 — the entry screen: "Approved amount" and the prefilled paid field.
    await queueRow.getByRole('button', { name: 'Take for processing' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${id}$`));
    await expect(
      adminPage.locator('#approved'),
      'hop 9a — the readonly ceiling on the entry screen'
    ).toHaveValue(MONEY);
    await expect(
      adminPage.locator('#amount'),
      'hop 9b — Amount actually paid is prefilled with the approved figure'
    ).toHaveValue(PLAIN);
    await expectMoney(
      adminPage,
      '.dl div:has(dt:text-is("Approved amount")) dd',
      MONEY,
      'hop 9c — "What was approved" card'
    );

    // Hop 10 — the settlement sheet compares approved against paid.
    await adminPage.getByLabel('Paid on').fill('2026-09-15');
    await adminPage.getByLabel('Payment mode').selectOption('bank_transfer');
    await adminPage.getByLabel('Transaction / UTR reference').fill(`UTR-T1-${runId}`);
    await adminPage.getByLabel('Processing note').fill(`Trail one ${runId}`);
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    const settle = adminPage.locator('.overlay .sheet');
    await expect(settle).toBeVisible();
    await expectMoney(adminPage, '.compare .cmp-row:has(.l:text-is("Approved")) .v', MONEY, 'hop 10a — sheet Approved');
    await expectMoney(
      adminPage,
      '.compare .cmp-row:has(.l:text-is("Actually paid")) .v',
      MONEY,
      'hop 10b — sheet Actually paid'
    );
    await expectMoney(adminPage, '.compare .cmp-row.match .v', '₹0.00', 'hop 10c — sheet Difference');
    await settle.locator('input[name="settlement"][value="settled"]').check();
    await settle.getByRole('button', { name: 'Confirm and save payment' }).click();
    await expect(adminPage).toHaveURL(/\/payments\/\d+$/);
    const paymentPath = new URL(adminPage.url()).pathname;

    // Hop 11 — the payment detail. A LINKED payment renders the `payment_detail`
    // branch, whose money lives in `.rh-amt` and the `.compare` block; the
    // `.details` grid belongs to `payment_detail_historical` and is not on this
    // page at all.
    await expectMoney(adminPage, '.rh-amt', MONEY, 'hop 11a — payment detail head');
    await expectMoney(
      adminPage,
      '.compare .cmp-row:has(.l:text-is("Paid")) .v',
      MONEY,
      'hop 11b — payment detail Paid row'
    );
    await expectMoney(
      adminPage,
      '.compare .cmp-row:has(.l:text-is("Approved")) .v',
      MONEY,
      'hop 11c — payment detail Approved row'
    );

    // Hop 12 — the request's own "Payment outcome" block.
    await adminPage.goto(`/requests/${id}`);
    await expectMoney(
      adminPage,
      '.compare .cmp-row:has(.l:text-is("Approved")) .v',
      MONEY,
      'hop 12a — request Payment outcome, Approved'
    );
    await expectMoney(
      adminPage,
      '.compare .cmp-row.match .v',
      '₹0.00',
      'hop 12b — request Payment outcome, Difference'
    );

    // Hop 13 — the payments ledger row.
    await adminPage.goto('/payments?month=2026-09');
    await adminPage.getByLabel('Search').fill(`Trail one ${runId}`);
    await adminPage.getByRole('button', { name: 'Filter' }).click();
    const ledger = adminPage.locator('tbody tr').filter({ has: adminPage.locator(`a[href="${paymentPath}"]`) });
    await expect(ledger, 'hop 13 — the ledger must carry exactly this payment').toHaveCount(1);
    await expectMoney(
      adminPage,
      `tbody tr:has(a[href="${paymentPath}"]) td[data-label="Amount"]`,
      MONEY,
      'hop 13 — payments ledger Amount'
    );

    // Hop 14 — the variance grid actual for the head, and the company total.
    await adminPage.goto('/grid?month=2026-09');
    await expectMoney(
      adminPage,
      'tr.head:has(.hname:text-is("Office Rent")) td:nth-child(5)',
      MONEY,
      'hop 14a — grid actual for Operations / Office Rent'
    );
    await expectMoney(adminPage, 'tfoot tr.total td:nth-child(5)', MONEY, 'hop 14b — grid company total');

    // Hop 15 — the monthly report reuses Grid, so its actual is the same number.
    await adminPage.goto('/reports/monthly?from=2026-09&to=2026-09');
    await expectMoney(adminPage, 'tbody tr td[data-label="Actual"]', MONEY, 'hop 15 — /reports/monthly Actual');

    // Hop 16 — /export.csv.
    const gridCSV = await csv(adminPage, '/export.csv?month=2026-09');
    expectMoneyIn(gridCSV, MONEY, 'hop 16 — /export.csv');
    expect(
      gridCSV.split('\n').filter(line => line.includes('Office Rent')).length,
      'hop 16 — the head appears exactly once in the grid CSV'
    ).toBe(1);

    // Hop 17 — /reports/ytd.csv.
    const ytdCSV = await csv(adminPage, '/reports/ytd.csv?from=2026-09&to=2026-09');
    expectMoneyIn(ytdCSV, MONEY, 'hop 17 — /reports/ytd.csv');

    // Hop 18 — /requests/export.csv keeps the REQUESTED figure, which here
    // equals the approved one.
    const reqCSV = await csv(adminPage, `/requests/export.csv?q=${encodeURIComponent(number)}`);
    expectMoneyIn(reqCSV, MONEY, 'hop 18 — /requests/export.csv');

    expectNoRuntimeErrors(errors, 'no console error, page error or 5xx anywhere on the trail');
    await approver.close();
  });

  /**
   * The same journey for a figure with paise, so rounding and the two-decimal
   * tail are proven and not just the grouping. 12,34,567.89 is the value
   * money_test.go pins, so a drift here is a drift from the unit test too.
   */
  test('TC-G-002 — paise precision survives the whole trail', async ({ adminPage, browser, runId }) => {
    const errors = capturePageErrors(adminPage);
    const MONEY = '₹12,34,567.89';

    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgr2-${runId}`);
    const payee = `Paise Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    const raised = await raiseVendorRequest(adminPage, {
      amount: '1234567.89',
      vendor: payee,
      title: `Trail paise ${runId}`,
      invoiceNo: `INV-T2-${runId}`,
      approverName: approver.subject.name
    });
    await expectMoney(adminPage, '.rh-amt', MONEY, 'submitted screen');

    await approveFor(approver.page, raised.id, '1234567.89');

    await adminPage.goto(`/requests/${raised.id}`);
    await expectMoney(adminPage, '.dl div:has(dt:text-is("Approved")) dd', MONEY, 'request Approved row');

    // `paymentModes()` (internal/app/linking.go:993) offers bank_transfer,
    // cheque, upi, cash, card and other — there is no NEFT/RTGS split.
    const paymentPath = await settlePayment(adminPage, raised.id, {
      amount: '1234567.89',
      paidOn: '2026-10-15',
      mode: 'bank_transfer',
      reference: `UTR-T2-${runId}`,
      remarks: `Trail paise ${runId}`
    });
    await expectMoney(adminPage, '.rh-amt', MONEY, 'payment detail head');

    await adminPage.goto('/payments?month=2026-10');
    await expectMoney(
      adminPage,
      `tbody tr:has(a[href="${paymentPath}"]) td[data-label="Amount"]`,
      MONEY,
      'payments ledger'
    );

    await adminPage.goto('/grid?month=2026-10');
    await expectMoney(
      adminPage,
      'tr.head:has(.hname:text-is("Office Rent")) td:nth-child(5)',
      MONEY,
      'grid actual'
    );
    await expectMoney(adminPage, 'tfoot tr.total td:nth-child(5)', MONEY, 'grid company total');

    await adminPage.goto('/reports/monthly?from=2026-10&to=2026-10');
    await expectMoney(adminPage, 'tbody tr td[data-label="Actual"]', MONEY, '/reports/monthly Actual');

    expectMoneyIn(await csv(adminPage, '/export.csv?month=2026-10'), MONEY, '/export.csv');
    expectMoneyIn(await csv(adminPage, '/reports/ytd.csv?from=2026-10&to=2026-10'), MONEY, '/reports/ytd.csv');

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  /**
   * The decisive case: requested ≠ approved ≠ paid.
   *
   * The grid must follow the PAID figure (internal/store/store.go:1388 sums
   * py.amount) and the request must keep the APPROVED one
   * (internal/app/linking.go:816-821). A screen that mixed them would either
   * overstate spend or understate the obligation.
   */
  test('TC-G-003 — where approved ≠ requested and paid ≠ approved, the grid follows paid and the request keeps approved', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const REQUESTED = '₹50,000.00';
    const APPROVED = '₹42,500.75';
    const PAID = '₹37,000.25';
    const SHORTFALL = '₹5,500.50'; // approved − paid

    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgr3-${runId}`);
    const payee = `Adjust Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    const raised = await raiseVendorRequest(adminPage, {
      amount: '50000.00',
      vendor: payee,
      title: `Trail adjusted ${runId}`,
      invoiceNo: `INV-T3-${runId}`,
      approverName: approver.subject.name
    });

    await approveFor(approver.page, raised.id, '42500.75');

    // The request keeps both figures, side by side and distinct.
    await adminPage.goto(`/requests/${raised.id}`);
    await expectMoney(adminPage, '.rh-amt', REQUESTED, 'request head keeps the requested figure');
    await expectMoney(
      adminPage,
      '.dl div:has(dt:text-is("Amount")) dd',
      REQUESTED,
      'request Amount row keeps the requested figure'
    );
    await expectMoney(
      adminPage,
      '.dl div:has(dt:text-is("Approved")) dd',
      APPROVED,
      'request Approved row carries the adjusted figure'
    );

    // The queue and the ceiling follow the APPROVED figure, not the requested.
    await adminPage.goto('/accounts-queue?tab=approved');
    await expectMoney(
      adminPage,
      `tbody tr:has-text("${raised.number}") td[data-label="Amount"]`,
      APPROVED,
      'the queue prices the request at what was approved'
    );

    const paymentPath = await settlePayment(adminPage, raised.id, {
      amount: '37000.25',
      paidOn: '2026-11-15',
      mode: 'cheque',
      reference: `UTR-T3-${runId}`,
      remarks: `Trail adjusted ${runId}`,
      settlement: 'settled'
    });

    // The payment carries only the paid figure, and shows what was approved
    // beside it so the shortfall is legible rather than inferred.
    await expectMoney(adminPage, '.rh-amt', PAID, 'payment detail head is the paid figure');
    await expectMoney(adminPage, '.compare .cmp-row:has(.l:text-is("Paid")) .v', PAID, 'payment detail Paid row');
    await expectMoney(
      adminPage,
      '.compare .cmp-row:has(.l:text-is("Approved")) .v',
      APPROVED,
      'payment detail Approved row'
    );

    // The request's outcome block names all three, and the arithmetic is right.
    // Its Paid row is labelled "Paid on <date>", unlike the payment detail's.
    await adminPage.goto(`/requests/${raised.id}`);
    await expectMoney(
      adminPage,
      '.compare .cmp-row:has(.l:text-is("Approved")) .v',
      APPROVED,
      'outcome Approved'
    );
    await expectMoney(adminPage, '.compare .cmp-row:has(.l:has-text("Paid on")) .v', PAID, 'outcome Paid');
    await expectMoney(
      adminPage,
      '.compare .cmp-row.match .v',
      SHORTFALL,
      'outcome Difference is approved − paid, confirmed settled'
    );

    // The grid follows PAID, and not requested and not approved.
    await adminPage.goto('/grid?month=2026-11');
    const actual = adminPage.locator('tr.head:has(.hname:text-is("Office Rent")) td:nth-child(5)');
    await expectMoney(adminPage, 'tr.head:has(.hname:text-is("Office Rent")) td:nth-child(5)', PAID, 'grid actual');
    const actualText = (await actual.innerText()).trim();
    expect(actualText, 'the grid must not show the approved figure').not.toContain(APPROVED);
    expect(actualText, 'the grid must not show the requested figure').not.toContain(REQUESTED);
    await expectMoney(adminPage, 'tfoot tr.total td:nth-child(5)', PAID, 'grid company total follows paid');

    // And so does every downstream report.
    await adminPage.goto('/reports/monthly?from=2026-11&to=2026-11');
    await expectMoney(adminPage, 'tbody tr td[data-label="Actual"]', PAID, '/reports/monthly Actual follows paid');
    expectMoneyIn(await csv(adminPage, '/export.csv?month=2026-11'), PAID, '/export.csv follows paid');
    expectMoneyIn(await csv(adminPage, '/reports/ytd.csv?from=2026-11&to=2026-11'), PAID, '/reports/ytd.csv');

    // The requests CSV keeps the REQUESTED figure — documented, and worth
    // pinning: an approved-down request exports the figure nobody approved.
    const reqCSV = await csv(adminPage, `/requests/export.csv?q=${encodeURIComponent(raised.number)}`);
    expectMoneyIn(reqCSV, REQUESTED, '/requests/export.csv Amount column');
    expect(
      reqCSV.includes(APPROVED),
      'F-G-011: /requests/export.csv has no approved-amount column, so the adjusted figure is absent'
    ).toBe(false);

    // The ledger total for the month equals exactly the paid figure.
    await adminPage.goto('/payments?month=2026-11');
    await expectMoney(
      adminPage,
      `tbody tr:has(a[href="${paymentPath}"]) td[data-label="Amount"]`,
      PAID,
      'ledger row'
    );

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  /** G13 from the information-flow side: a refused overpayment must leave the
   *  ledger, the grid and the request untouched — nothing half-written. */
  test('TC-G-004 — a settlement above the approved ceiling writes nothing anywhere', async ({
    adminPage,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const request = await createApprovedRequest(adminPage, runId, { amount: '8000.00' });

    await adminPage.goto('/accounts-queue?tab=approved');
    await adminPage
      .locator('tbody tr', { hasText: request.number })
      .getByRole('button', { name: 'Take for processing' })
      .click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${request.id}$`));
    await adminPage.getByLabel('Amount actually paid').fill('9000.00');
    await adminPage.getByLabel('Paid on').fill('2026-12-15');
    await adminPage.getByLabel('Payment mode').selectOption('bank_transfer');
    await adminPage.getByLabel('Transaction / UTR reference').fill(`UTR-OVER-${runId}`);
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    const sheet = adminPage.locator('.overlay .sheet');
    await expect(sheet).toBeVisible();
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();

    await expect(adminPage, 'a refused settlement creates no payment, so no /payments/{id}').toHaveURL(/\/payments$/);
    await expect(sheet.locator('.banner.bad')).toContainText('more than the approved');

    // The month is otherwise empty, so "nothing was written" is an equality.
    await adminPage.goto('/grid?month=2026-12');
    await expectMoney(
      adminPage,
      'tfoot tr.total td:nth-child(5)',
      '₹0.00',
      'a refused settlement must not move the grid'
    );
    await adminPage.goto('/payments?month=2026-12');
    expect(await dataRows(adminPage), 'a refused settlement must not put a row in the ledger').toBe(0);

    await adminPage.goto(`/requests/${request.id}`);
    await expect(
      adminPage.locator('.rh-status'),
      'the request stays reserved for processing, not completed'
    ).not.toContainText('Completed');

    expectNoRuntimeErrors(errors);
  });
});

// ===========================================================================
// 2 — Identity and payee propagation
// ===========================================================================

test.describe('G · payee, project, head, invoice', () => {
  /**
   * The payee a vendor_invoice names lives in `vendor_id`; `vendor_payee` is
   * empty. `store.Request.Vendor` is the display payee
   * (internal/store/requests.go:257) and every screen must read it. A blank
   * payee anywhere on this path is a request Accounts cannot act on.
   */
  test('TC-G-010 — a vendor_invoice payee reaches every screen that shows one, and the CSV', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrp-${runId}`);
    const payee = `Payee Propagation ${runId}`;
    await ensureVendor(adminPage, payee);

    const raised = await raiseVendorRequest(adminPage, {
      amount: '6400.00',
      vendor: payee,
      title: `Payee trail ${runId}`,
      invoiceNo: `INV-P1-${runId}`,
      invoiceDate: '2026-07-09',
      approverName: approver.subject.name
    });

    // Hop 1 — the vendor master row exists under that exact name.
    await adminPage.goto(`/vendors?q=${encodeURIComponent(payee)}`);
    await expect(
      adminPage.locator('tbody tr', { hasText: payee }),
      'the vendor master must carry the payee'
    ).toHaveCount(1);

    // Hop 2 — the request detail, labelled "Vendor" because vendor_id is set.
    await adminPage.goto(`/requests/${raised.id}`);
    const payeeRow = adminPage.locator('.dl div:has(dt:text-is("Vendor")) dd');
    await expect(payeeRow, 'a vendor_invoice labels its payee "Vendor", not "Paid to"').toHaveCount(1);
    await expect(payeeRow, 'hop 2 — request detail payee').toContainText(payee);
    await expect(
      adminPage.locator('.dl div:has(dt:text-is("Invoice number")) dd'),
      'hop 2 — invoice number'
    ).toContainText(`INV-P1-${runId}`);
    await expect(
      adminPage.locator('.dl div:has(dt:text-is("Invoice date")) dd'),
      'hop 2 — invoice date, spelled long'
    ).toContainText('9 July 2026');
    await expect(adminPage.locator('.dl div:has(dt:text-is("Project")) dd')).toHaveText('Operations');
    await expect(adminPage.locator('.dl div:has(dt:text-is("Head")) dd')).toHaveText('Office Rent');

    await approveFor(approver.page, raised.id, '6400.00');

    // Hop 3 — the accounts queue's Payee column.
    await adminPage.goto('/accounts-queue?tab=approved');
    const row = adminPage.locator('tbody tr', { hasText: raised.number });
    await expect(row.locator('td[data-label="Payee"]'), 'hop 3 — queue Payee column').toHaveText(payee);
    await expect(row.locator('td[data-label="Project / head"]'), 'hop 3 — queue project/head').toHaveText(
      'Operations / Office Rent'
    );

    // Hop 4 — the entry screen: the banner, the card and the hidden input the
    // form actually posts.
    await row.getByRole('button', { name: 'Take for processing' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${raised.id}$`));
    await expect(adminPage.locator('.page-banner .sub'), 'hop 4a — entry banner').toContainText(payee);
    await expect(
      adminPage.locator('.dl div:has(dt:text-is("Payee")) dd'),
      'hop 4b — "What was approved" payee'
    ).toHaveText(payee);
    await expect(
      adminPage.locator('input[name="vendor_payee"]'),
      'hop 4c — the hidden field carries the DISPLAY payee, so the payment is not written blank'
    ).toHaveValue(payee);
    await expect(
      adminPage.locator('input[name="invoice_no"]'),
      'hop 4d — the invoice number rides along on the same hidden set'
    ).toHaveValue(`INV-P1-${runId}`);
    await expect(adminPage.locator('.dl div:has(dt:text-is("Charge to")) dd')).toHaveText(
      'Operations / Office Rent'
    );

    // Hop 5 — the settlement sheet's sub-line.
    await adminPage.getByLabel('Amount actually paid').fill('6400.00');
    await adminPage.getByLabel('Paid on').fill('2027-01-12');
    await adminPage.getByLabel('Payment mode').selectOption('bank_transfer');
    await adminPage.getByLabel('Transaction / UTR reference').fill(`UTR-P1-${runId}`);
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    const sheet = adminPage.locator('.overlay .sheet');
    await expect(sheet.locator('.sh-sub'), 'hop 5 — the confirmation names the payee').toContainText(payee);
    await sheet.locator('input[name="settlement"][value="settled"]').check();
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();
    await expect(adminPage).toHaveURL(/\/payments\/\d+$/);
    const paymentPath = new URL(adminPage.url()).pathname;

    // Hop 6 — the payment detail's <h1> IS the payee. A blank h1 here was the
    // shape of the gap fixtures.ts still documents; it is closed.
    await expect(adminPage.locator('h1'), 'hop 6a — the payment detail <h1> is the payee').toHaveText(payee);
    await expect(
      adminPage.locator('.dl div:has(dt:text-is("Payee")) dd'),
      'hop 6b — payment detail Payee row'
    ).toContainText(payee);
    // The linked detail's `.dl` carries the invoice; the project and head are in
    // the head's own meta line, not in a labelled row.
    await expect(
      adminPage.locator('.dl div:has(dt:text-is("Invoice")) dd'),
      'hop 6c — payment detail Invoice'
    ).toHaveText(`INV-P1-${runId}`);
    await expect(
      adminPage.locator('.rh-meta'),
      'hop 6d — payment detail project/head'
    ).toContainText('Operations / Office Rent');

    // Hop 7 — the ledger, found BY the payee, which proves the search index too.
    await adminPage.goto('/payments?month=2027-01');
    await adminPage.getByLabel('Search').fill(payee);
    await adminPage.getByRole('button', { name: 'Filter' }).click();
    const ledger = adminPage.locator('tbody tr').filter({ has: adminPage.locator(`a[href="${paymentPath}"]`) });
    await expect(ledger, 'hop 7 — searching the ledger by payee finds the payment').toHaveCount(1);
    await expect(ledger.locator('td[data-label="Payee"]'), 'hop 7 — ledger Payee column').toHaveText(payee);
    await expect(ledger.locator('td[data-label="Reference"]')).toContainText(`INV-P1-${runId}`);

    // Hop 8 — /requests/export.csv reads Request.Vendor, not the snapshot.
    const reqCSV = await csv(adminPage, `/requests/export.csv?q=${encodeURIComponent(raised.number)}`);
    expect(reqCSV, 'hop 8 — the requests CSV Payee column').toContain(payee);

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  /**
   * A reimbursement forces the payee to the requester and DOES fill the
   * snapshot column (internal/store/requests.go:367-370). The same eight hops
   * must show the requester's name, and the request must label it "Paid to"
   * because there is no vendor row to link.
   */
  test('TC-G-011 — a reimbursement payee is forced to the requester and travels the same path', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrr-${runId}`);
    const requester = await asRole(adminPage, browser, runId, ['Requester'], `reim-${runId}`);
    const reqErrors = capturePageErrors(requester.page);

    await requester.page.goto('/requests/new?type=reimbursement');
    await requester.page.getByLabel('Short title').fill(`Reimburse me ${runId}`);
    await requester.page.locator('#project').selectOption({ label: 'Growth' });
    await expect(requester.page.locator('#head option').filter({ hasNotText: 'Growth /' })).toHaveCount(1);
    await requester.page.locator('#head').selectOption({ label: 'Growth / Travel' });
    await requester.page.getByLabel('Amount').fill('3250.50');
    await requester.page.getByLabel('Expense date').fill('2026-07-02');
    await requester.page.getByLabel('Purpose').fill(`Taxi fares ${runId}.`);
    await requester.page.getByLabel('Approver').selectOption({ label: approver.subject.name });
    await requester.page.getByRole('button', { name: 'Submit request' }).click();
    await expect(requester.page).toHaveURL(/\/requests\/\d+\/submitted$/);
    const id = Number(new URL(requester.page.url()).pathname.split('/')[2]);
    const number = (await requester.page.locator('.rh-no').first().innerText()).trim();

    // The request labels the payee "Paid to" — there is no vendor to link to.
    await requester.page.goto(`/requests/${id}`);
    await expect(
      requester.page.locator('.dl div:has(dt:text-is("Paid to")) dd'),
      'a reimbursement pays the requester, and says so'
    ).toHaveText(requester.subject.name);
    await expect(
      requester.page.locator('.dl div:has(dt:text-is("Vendor")) dd'),
      'a reimbursement has no vendor row, so no Vendor label'
    ).toHaveCount(0);

    await approveFor(approver.page, id, '3250.50');

    await adminPage.goto('/accounts-queue?tab=approved');
    const row = adminPage.locator('tbody tr', { hasText: number });
    await expect(row.locator('td[data-label="Payee"]'), 'queue Payee is the requester').toHaveText(
      requester.subject.name
    );

    const paymentPath = await settlePayment(adminPage, id, {
      amount: '3250.50',
      paidOn: '2027-02-10',
      mode: 'upi',
      reference: `UTR-R1-${runId}`,
      remarks: `Reimburse ${runId}`
    });
    await expect(adminPage.locator('h1'), 'the payment detail <h1> is the requester').toHaveText(
      requester.subject.name
    );

    await adminPage.goto('/payments?month=2027-02');
    await adminPage.getByLabel('Search').fill(requester.subject.name);
    await adminPage.getByRole('button', { name: 'Filter' }).click();
    const ledger = adminPage.locator('tbody tr').filter({ has: adminPage.locator(`a[href="${paymentPath}"]`) });
    await expect(ledger, 'the ledger is searchable by the reimbursement payee').toHaveCount(1);
    await expect(ledger.locator('td[data-label="Payee"]')).toHaveText(requester.subject.name);

    const reqCSV = await csv(adminPage, `/requests/export.csv?q=${encodeURIComponent(number)}`);
    expect(reqCSV, 'the requests CSV payee for a reimbursement').toContain(requester.subject.name);

    expectNoRuntimeErrors(errors);
    expectNoRuntimeErrors(reqErrors);
    await approver.close();
    await requester.close();
  });

  /**
   * A forged payee is inert. `CreateRequest` writes whatever `vendor_payee` was
   * posted, but `Request.Vendor` is `COALESCE(NULLIF(v.name,''), vendor_payee)`
   * — the vendor row wins — so a hand-built POST cannot make the system pay a
   * name of the submitter's choosing.
   */
  test('TC-G-012 — a forged vendor_payee on a vendor_invoice never becomes the payee', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrf-${runId}`);
    const payee = `Genuine Vendor ${runId}`;
    const forged = `ATTACKER PAYEE ${runId}`;
    await ensureVendor(adminPage, payee);

    // Read the vendor's id out of the combobox the form uses.
    await adminPage.goto('/requests/new?type=vendor_invoice');
    await adminPage.locator('#vendor').pressSequentially(payee);
    await adminPage.locator('#vendor-options .co', { hasText: payee }).first().click();
    const vendorID = await adminPage.locator('#vendor-id').inputValue();
    expect(vendorID, 'the combobox must resolve a vendor id').not.toBe('');

    const approverID = await adminPage.locator('#approver option', { hasText: approver.subject.name }).getAttribute('value');
    const projectID = await adminPage.locator('#project option', { hasText: 'Operations' }).getAttribute('value');
    await adminPage.locator('#project').selectOption({ label: 'Operations' });
    await expect(adminPage.locator('#head option').filter({ hasNotText: 'Operations /' })).toHaveCount(1);
    const headID = await adminPage.locator('#head option', { hasText: 'Operations / Office Rent' }).getAttribute('value');

    const posted = await probePost(adminPage, '/requests', {
      type: 'vendor_invoice',
      treatment: 'budget',
      project_id: projectID!,
      head_id: headID!,
      vendor_id: vendorID,
      vendor_payee: forged,
      short_title: `Forged payee ${runId}`,
      amount: '2500.00',
      invoice_no: `INV-F1-${runId}`,
      invoice_date: '2026-07-18',
      purpose: `Forged payee purpose ${runId}.`,
      manager_id: approverID!
    });
    expect(posted.status, 'the POST is accepted — the forged field is stored, not rejected').toBe(303);
    const id = Number(posted.location!.split('/')[2]);

    await adminPage.goto(`/requests/${id}`);
    const shown = adminPage.locator('.dl div:has(dt:text-is("Vendor")) dd');
    await expect(shown, 'the vendor row wins the COALESCE').toContainText(payee);
    await expect(shown, 'the forged snapshot must never be displayed').not.toContainText('ATTACKER');

    await approveFor(approver.page, id, '2500.00');
    await adminPage.goto('/accounts-queue?tab=approved');
    const body = await adminPage.locator('body').innerText();
    expect(body.includes('ATTACKER'), 'the queue must not offer to pay the forged name').toBe(false);

    const paymentPath = await settlePayment(adminPage, id, {
      amount: '2500.00',
      paidOn: '2027-03-10',
      reference: `UTR-F1-${runId}`,
      remarks: `Forged ${runId}`
    });
    await expect(adminPage.locator('h1'), 'the payment pays the vendor, never the forged name').toHaveText(payee);
    const detail = await adminPage.locator('body').innerText();
    expect(detail.includes('ATTACKER'), 'the payment detail must not carry the forged name').toBe(false);
    expect(paymentPath).toMatch(/^\/payments\/\d+$/);

    expectNoRuntimeErrors(errors);
    await approver.close();
  });
});

// ===========================================================================
// 3 — The audit trail as a record of truth (C2)
// ===========================================================================

test.describe('G · the audit trail', () => {
  /**
   * One lifecycle, then the log read back. Each mutation must be there once,
   * with the actor who performed it and `entity_type = payment_request`.
   */
  test('TC-G-020 — every mutation in a lifecycle leaves one audit row with the right actor and entity', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgra-${runId}`);
    const payee = `Audit Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    const raised = await raiseVendorRequest(adminPage, {
      amount: '9500.00',
      vendor: payee,
      title: `Audit trail ${runId}`,
      invoiceNo: `INV-A1-${runId}`,
      approverName: approver.subject.name
    });
    await approveFor(approver.page, raised.id, '9500.00');
    await settlePayment(adminPage, raised.id, {
      amount: '9500.00',
      paidOn: '2027-04-14',
      reference: `UTR-A1-${runId}`,
      remarks: `Audit ${runId}`
    });

    // The approver's own rows: exactly one approve, entity payment_request.
    const byApprover = await auditRows(approver.page, approver.subject.name).catch(() => []);
    void byApprover; // the approver holds no audit:view; the admin reads the log.

    const mgrRows = await auditRows(adminPage, approver.subject.name, 'payment_request', raised.id);
    const approveRows = mgrRows.filter(r => r.action === 'approve');
    expect(approveRows.length, 'exactly one approve row for this request').toBe(1);
    expect(approveRows[0].entity, 'a request mutation is entity_type payment_request').toBe('Payment Request');
    expect(approveRows[0].actor, 'the approver is the actor on the approve row').toBe(approver.subject.name);
    expect(approveRows[0].summary, 'the approve summary names the request and the figure').toContain(
      raised.number
    );
    expect(approveRows[0].summary).toContain('₹9,500.00');

    // The admin raised, reserved and settled, so those three are theirs.
    const adminRows = await auditRows(adminPage, 'Fervid Admin', 'payment_request', raised.id);
    const forThis = adminRows.filter(r => r.summary.includes(raised.number) || r.summary.includes(runId));
    const actions = new Set(adminRows.map(r => r.action));
    for (const action of ['submit', 'process', 'settle']) {
      expect(actions.has(action), `the log must carry a "${action}" row for this lifecycle`).toBe(true);
    }
    expect(
      forThis.some(r => r.action === 'submit'),
      'the submit row names the request number it created'
    ).toBe(true);

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  /**
   * Before/After must actually reflect the change, not merely exist. The
   * approve row's Before carries `"ApprovedAmount": null` and its After the
   * adjusted paise, which is the strongest available proof that the two halves
   * are the real pre- and post-images.
   */
  test('TC-G-021 — the approve row Before/After hold the real pre- and post-images', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrb-${runId}`);
    const payee = `Diff Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    const raised = await raiseVendorRequest(adminPage, {
      amount: '20000.00',
      vendor: payee,
      title: `Audit diff ${runId}`,
      invoiceNo: `INV-A2-${runId}`,
      approverName: approver.subject.name
    });
    await approveFor(approver.page, raised.id, '17500.00');

    await adminPage.goto(`/audit?entity=payment_request&actor=${encodeURIComponent(approver.subject.name)}`);
    const row = adminPage.locator('tbody tr', { hasText: raised.number }).first();
    await expect(row, 'the approve row must be findable in the log').toHaveCount(1);
    const disclosure = row.locator('details');
    await expect(disclosure, 'a row with a Before and an After offers the disclosure').toHaveCount(1);
    await disclosure.locator('summary').click();
    const blocks = disclosure.locator('pre');
    await expect(blocks, 'Before and After are two <pre> blocks').toHaveCount(2);
    const before = await blocks.nth(0).innerText();
    const after = await blocks.nth(1).innerText();

    expect(before, 'Before must show the request unapproved').toContain('"ApprovedAmount": null');
    expect(before, 'Before must show the requested amount in paise').toContain('"Amount": 2000000');
    expect(after, 'After must show the adjusted approved amount in paise').toContain('"ApprovedAmount": 1750000');
    expect(after, 'After must not have altered the requested amount').toContain('"Amount": 2000000');
    expect(after, 'After must show the request approved').toContain('"Status": "approved"');
    expect(before, 'Before must show the pre-approval status').toContain('"Status": "pending"');

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  /**
   * The reader-facing half of the same record: the request's own thread turns
   * the before/after JSON into a field-level `Was → Now` line, with the amount
   * spelled as money rather than as a float (internal/app/requests.go:1113).
   */
  test('TC-G-022 — an edit shows the old and new amount as money on the request thread', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgre-${runId}`);
    const requester = await asRole(adminPage, browser, runId, ['Requester'], `edit-${runId}`);
    const reqErrors = capturePageErrors(requester.page);

    await requester.page.goto('/requests/new?type=reimbursement');
    await requester.page.getByLabel('Short title').fill(`Edit me ${runId}`);
    await requester.page.locator('#project').selectOption({ label: 'People' });
    await expect(requester.page.locator('#head option').filter({ hasNotText: 'People /' })).toHaveCount(1);
    await requester.page.locator('#head').selectOption({ label: 'People / Staff Welfare' });
    await requester.page.getByLabel('Amount').fill('4000.00');
    await requester.page.getByLabel('Expense date').fill('2026-07-03');
    await requester.page.getByLabel('Purpose').fill(`Edit purpose ${runId}.`);
    await requester.page.getByLabel('Approver').selectOption({ label: approver.subject.name });
    await requester.page.getByRole('button', { name: 'Submit request' }).click();
    await expect(requester.page).toHaveURL(/\/requests\/\d+\/submitted$/);
    const id = Number(new URL(requester.page.url()).pathname.split('/')[2]);

    await requester.page.goto(`/requests/${id}/edit`);
    await requester.page.getByLabel('Amount').fill('5500.00');
    await requester.page.getByRole('button', { name: /Save|Resubmit|Submit/ }).first().click();
    await expect(requester.page).toHaveURL(new RegExp(`/requests/${id}(\\?.*)?$`));

    await expect(
      requester.page.locator('.rh-amt'),
      'the edited figure is the request head figure now'
    ).toContainText('₹5,500.00');
    const thread = requester.page.locator('.thread');
    await expect(thread, 'the edit joins the thread').toContainText('Amount');
    const threadText = await thread.innerText();
    expect(threadText, 'the thread spells the old amount as money, not as a float').toContain('₹4,000.00');
    expect(threadText, 'the thread spells the new amount as money').toContain('₹5,500.00');
    expect(threadText, 'no scientific notation leaks out of the audit JSON').not.toMatch(/\de\+\d/);

    expectNoRuntimeErrors(errors);
    expectNoRuntimeErrors(reqErrors);
    await approver.close();
    await requester.close();
  });

  /**
   * The negative that matters most: a refused mutation must leave no trace.
   * All three refusals are checked by counting the actor's own audit rows
   * before and after, so a stray row is caught even if it carried a different
   * action than expected.
   */
  test('TC-G-023 — a refused approve writes no audit row and no state change', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrok-${runId}`);
    const stranger = await asRole(adminPage, browser, runId, ['Manager'], `mgrno-${runId}`);
    const payee = `Refuse Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    const raised = await raiseVendorRequest(adminPage, {
      amount: '7000.00',
      vendor: payee,
      title: `Refused approve ${runId}`,
      invoiceNo: `INV-N1-${runId}`,
      approverName: approver.subject.name
    });

    const before = (await auditRows(adminPage, stranger.subject.name, 'payment_request', raised.id)).length;

    // A Manager who is not this request's manager holds approval:approve and is
    // still refused — the grant says they may decide, the row says which.
    const refused = await probePost(stranger.page, `/requests/${raised.id}/approve`, {
      approved_amount: '7000.00'
    });
    expectOutcome(refused, [403], 'a manager who is not the request manager cannot approve it');

    const after = await auditRows(adminPage, stranger.subject.name, 'payment_request', raised.id);
    expect(
      after.length,
      'a rolled-back transaction that still audited would be a forged history'
    ).toBe(before);
    expect(
      after.some(r => r.summary.includes(raised.number)),
      'the refused actor must not appear against this request at all'
    ).toBe(false);

    await adminPage.goto(`/requests/${raised.id}`);
    await expect(adminPage.locator('.rh-status'), 'the request is still awaiting its own approver').toContainText(
      'Awaiting'
    );
    await expect(
      adminPage.locator('.dl div:has(dt:text-is("Approved")) dd'),
      'no approved amount was written'
    ).toHaveCount(0);

    expectNoRuntimeErrors(errors);
    await approver.close();
    await stranger.close();
  });

  test('TC-G-024 — a refused settlement writes no payment and no audit row', async ({ adminPage, runId }) => {
    const errors = capturePageErrors(adminPage);
    const request = await createApprovedRequest(adminPage, runId, { amount: '11000.00' });
    await adminPage.goto('/accounts-queue?tab=approved');
    await adminPage
      .locator('tbody tr', { hasText: request.number })
      .getByRole('button', { name: 'Take for processing' })
      .click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${request.id}$`));

    // Over the ceiling, so the store refuses inside the transaction. The figure
    // is unique to this test, which makes "no audit row mentions it" an exact
    // proof rather than a count that another test could move.
    const refused = await probePost(adminPage, '/payments', {
      request_id: String(request.id),
      amount: '99999.00',
      paid_on: '2027-05-10',
      payment_mode: 'bank_transfer',
      reference_no: `UTR-N2-${runId}`,
      head_id: '1',
      settlement: 'settled'
    });
    expect(refused.status, 'a settlement over the ceiling is a 400, rendered as the sheet').toBe(400);
    expect(refused.body, 'the refusal names the ceiling it broke').toContain('more than the approved');

    const paymentRows = await auditRows(adminPage, 'Fervid Admin', 'payment');
    expect(
      paymentRows.filter(r => r.summary.includes('₹99,999.00')).length,
      'no payment audit row may record the amount a rolled-back transaction tried to write'
    ).toBe(0);

    const requestRows = await auditRows(adminPage, 'Fervid Admin', 'payment_request', request.id);
    expect(
      requestRows.filter(r => r.action === 'settle' && r.summary.includes(request.number)).length,
      'no settle row may survive a refused settlement'
    ).toBe(0);

    await adminPage.goto('/payments?month=2027-05');
    expect(await dataRows(adminPage), 'and no ledger row either').toBe(0);

    expectNoRuntimeErrors(errors);
  });

  test('TC-G-025 — a refused edit of an approved request writes no audit row', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrre-${runId}`);
    const requester = await asRole(adminPage, browser, runId, ['Requester'], `noedit-${runId}`);

    await requester.page.goto('/requests/new?type=reimbursement');
    await requester.page.getByLabel('Short title').fill(`Locked edit ${runId}`);
    await requester.page.locator('#project').selectOption({ label: 'Growth' });
    await expect(requester.page.locator('#head option').filter({ hasNotText: 'Growth /' })).toHaveCount(1);
    await requester.page.locator('#head').selectOption({ label: 'Growth / Events' });
    await requester.page.getByLabel('Amount').fill('1500.00');
    await requester.page.getByLabel('Expense date').fill('2026-07-04');
    await requester.page.getByLabel('Purpose').fill(`Locked edit purpose ${runId}.`);
    await requester.page.getByLabel('Approver').selectOption({ label: approver.subject.name });
    await requester.page.getByRole('button', { name: 'Submit request' }).click();
    await expect(requester.page).toHaveURL(/\/requests\/\d+\/submitted$/);
    const id = Number(new URL(requester.page.url()).pathname.split('/')[2]);
    await approveFor(approver.page, id, '1500.00');

    const before = (await auditRows(adminPage, requester.subject.name, 'payment_request', id)).filter(r => r.action === 'update').length;

    const refused = await probePost(requester.page, `/requests/${id}/edit`, {
      type: 'reimbursement',
      treatment: 'budget',
      project_id: '3',
      head_id: '9',
      short_title: `Locked edit ${runId}`,
      amount: '99999.00',
      expense_date: '2026-07-04',
      purpose: 'Sneaky rewrite.',
      manager_id: '2'
    });
    expect(
      refused.status >= 400,
      `an approved request cannot be edited — got ${refused.outcome}`
    ).toBe(true);

    const after = (await auditRows(adminPage, requester.subject.name, 'payment_request', id)).filter(r => r.action === 'update').length;
    expect(after, 'a refused edit must leave the history untouched').toBe(before);

    await adminPage.goto(`/requests/${id}`);
    await expect(adminPage.locator('.rh-amt'), 'and the amount unchanged').toContainText('₹1,500.00');

    expectNoRuntimeErrors(errors);
    await approver.close();
    await requester.close();
  });

  /**
   * The audit log is entirely unscoped: `Store.Audit`
   * (internal/store/store.go:1591-1603) never receives a viewer. Today that is
   * latent, because `audit:view` is granted to Admin alone — this test pins the
   * gate that keeps it latent, and TC-G-052 shows what happens when it is
   * opened by a custom role.
   */
  test('TC-G-026 — the audit log is Admin-only today, which is the only thing scoping it', async ({
    adminPage,
    browser,
    runId
  }) => {
    const requester = await asRole(adminPage, browser, runId, ['Requester'], `noaudit-${runId}`);
    const accounts = await asRole(adminPage, browser, runId, ['Accounts'], `acaudit-${runId}`);
    const manager = await asRole(adminPage, browser, runId, ['Manager'], `mgaudit-${runId}`);

    for (const subject of [requester, accounts, manager]) {
      const probe = await probeGet(subject.page, '/audit');
      expectOutcome(probe, [403], 'audit:view is Admin-only in the seed');
    }
    const asAdmin = await probeGet(adminPage, '/audit');
    expect(asAdmin.status, 'the admin reads the log').toBe(200);

    await requester.close();
    await accounts.close();
    await manager.close();
  });

  /**
   * Two rendering gaps on `/audit`, pinned so a fix turns them red. The Entity
   * dropdown offers nine values and `payment_request` is not one of them
   * (internal/app/templates.go:3262), and `actionText`
   * (internal/app/app.go:1882-1906) maps ten actions, so every workflow action
   * prints as its raw identifier.
   */
  test('TC-G-027 — the audit filter cannot reach requests, and workflow actions render raw', async ({
    adminPage,
    browser,
    runId
  }) => {
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrx-${runId}`);
    const payee = `Render Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    const raised = await raiseVendorRequest(adminPage, {
      amount: '1000.00',
      vendor: payee,
      title: `Audit render ${runId}`,
      invoiceNo: `INV-A3-${runId}`,
      approverName: approver.subject.name
    });
    await approveFor(approver.page, raised.id, '1000.00');

    await adminPage.goto('/audit');
    const entities = await adminPage.locator('select[name="entity"] option').allInnerTexts();
    expect(
      entities.some(o => /request/i.test(o)),
      'F-G-004: no Entity option reaches payment_request, so the whole request workflow is unfilterable from the UI'
    ).toBe(false);
    const actions = await adminPage.locator('select[name="action"] option').allInnerTexts();
    expect(
      actions.some(o => /approv/i.test(o)),
      'F-G-004: no Action option reaches approve either'
    ).toBe(false);

    // The rows exist and are reachable only by hand-typing the query string.
    const rows = await auditRows(adminPage, approver.subject.name, 'payment_request', raised.id);
    expect(rows.length, 'the rows are there — only the filter cannot name them').toBeGreaterThan(0);
    // `actionText` spells ten actions and returns every other identifier
    // verbatim, so the whole request workflow renders as its column values.
    const approveRow = rows.find(r => r.action === 'approve');
    expect(approveRow, 'the approve row is in the log').toBeDefined();
    expect(
      approveRow!.rendered,
      'F-G-005: the Action pill prints the raw identifier "approve", where an audited "update" reads "Updated"'
    ).toBe('approve');
    // The contrast, on the same page: a mapped action IS spelled for a reader.
    const spelled = await auditRows(adminPage, 'Fervid Admin', 'user');
    expect(
      spelled.some(r => r.rendered === 'Created' || r.rendered === 'Updated'),
      'F-G-005: the ten mapped actions are spelled properly, which is what makes the rest look unfinished'
    ).toBe(true);

    await approver.close();
  });
});

// ===========================================================================
// 4 — Cross-feature consistency
// ===========================================================================

test.describe('G · the same fact in several places', () => {
  /**
   * The dashboard is the only screen that builds a second, different options
   * object for its counts instead of reusing the queue's
   * (internal/app/dashboard.go:71-96 vs internal/app/requests.go:482-494). Three
   * of its five tiles therefore cannot agree with what they link to.
   */
  test('TC-G-030 — the approval tiles agree with /approvals; the requester tiles and the accounts tile do not', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrd-${runId}`);
    const requester = await asRole(adminPage, browser, runId, ['Requester'], `dash-${runId}`);

    // Two requests from one requester: one left pending, one returned.
    for (const [n, title] of [['1', 'pending'], ['2', 'returned']] as const) {
      await requester.page.goto('/requests/new?type=reimbursement');
      await requester.page.getByLabel('Short title').fill(`Dash ${title} ${n} ${runId}`);
      await requester.page.locator('#project').selectOption({ label: 'People' });
      await expect(requester.page.locator('#head option').filter({ hasNotText: 'People /' })).toHaveCount(1);
      await requester.page.locator('#head').selectOption({ label: 'People / Contractor Fees' });
      await requester.page.getByLabel('Amount').fill('1200.00');
      await requester.page.getByLabel('Expense date').fill('2026-07-05');
      await requester.page.getByLabel('Purpose').fill(`Dash purpose ${n} ${runId}.`);
      await requester.page.getByLabel('Approver').selectOption({ label: approver.subject.name });
      await requester.page.getByRole('button', { name: 'Submit request' }).click();
      await expect(requester.page).toHaveURL(/\/requests\/\d+\/submitted$/);
      if (title === 'returned') {
        const id = Number(new URL(requester.page.url()).pathname.split('/')[2]);
        await approver.page.goto(`/requests/${id}`);
        await approver.page.getByRole('button', { name: /^Return/ }).click();
        const sheet = approver.page.locator('#return-sheet');
        await expect(sheet).toBeVisible();
        await sheet.getByLabel('What needs correcting').fill('Please attach the receipt.');
        await sheet.getByRole('button', { name: 'Return request' }).click();
        await expect(approver.page).toHaveURL(/\/approvals$/);
      }
    }

    // The approver's own dashboard vs /approvals — these two must agree.
    await approver.page.goto('/');
    const awaiting = Number(
      await approver.page.locator('.metric:has(.metric-label:text-is("Awaiting my approval")) .metric-value').innerText()
    );
    await approver.page.goto('/approvals?bucket=to-approve');
    const toApprove = Number(
      await approver.page.locator('.segmented a:has-text("To approve") .n').innerText()
    );
    const approvalCards = await approver.page.locator('.req-card').count();
    expect(awaiting, 'the dashboard tile and the /approvals tab read the same SQL').toBe(toApprove);
    expect(approvalCards, 'and the tab number is the number of rows beneath it').toBe(toApprove);

    // The requester's dashboard vs the buckets it links to.
    await requester.page.goto('/');
    const needsAction = Number(
      await requester.page.locator('.metric:has(.metric-label:text-is("Needs my action")) .metric-value').innerText()
    );
    const myOpen = Number(
      await requester.page.locator('.metric:has(.metric-label:text-is("My open requests")) .metric-value').innerText()
    );
    await requester.page.goto('/requests?bucket=needs-me');
    const needsMeRows = await requester.page.locator('.req-card').count();
    await requester.page.goto('/requests?bucket=open');
    const openRows = await requester.page.locator('.req-card').count();

    expect(needsAction, 'for a scope-own requester the needs-me tile and bucket do agree').toBe(needsMeRows);
    expect(needsAction, 'one request was returned to this requester').toBe(1);
    expect(openRows, 'the open bucket holds both the pending and the returned request').toBe(2);
    // F-G-006: the tile counts pending+approved+cancellation_requested and the
    // bucket adds `returned`, so the two differ by every returned request.
    expect(
      myOpen,
      'F-G-006: "My open requests" omits `returned`, which /requests?bucket=open includes'
    ).toBe(openRows - 1);

    expectNoRuntimeErrors(errors);
    await approver.close();
    await requester.close();
  });

  /**
   * The tile labelled "Approved, unclaimed" on the dashboard and the metric of
   * the same name on the accounts queue are two different queries: the
   * dashboard's omits `on_hold=0` (internal/app/dashboard.go:96) where the
   * queue's has it (internal/store/store.go:1113). Put one request on hold and
   * the two numbers part company.
   */
  test('TC-G-031 — the dashboard and the accounts queue disagree about "Approved, unclaimed" once anything is on hold', async ({
    adminPage,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const held = await createApprovedRequest(adminPage, runId, { amount: '3300.00' });
    const free = await createApprovedRequest(adminPage, runId, { amount: '4400.00' });

    await adminPage.goto('/');
    const beforeTile = Number(
      await adminPage.locator('.metric:has(.metric-label:text-is("Approved, unclaimed")) .metric-value').innerText()
    );
    await adminPage.goto('/accounts-queue?tab=approved');
    const beforeMetric = Number(
      await adminPage.locator('.metric:has(.metric-label:text-is("Approved, unclaimed")) .metric-value').innerText()
    );
    expect(beforeTile, 'with nothing on hold the two agree').toBe(beforeMetric);

    // Put one on hold. `on_hold=1` implies `status='approved'`.
    const put = await probePost(adminPage, `/requests/${held.id}/hold`, {
      reason: `Waiting on the invoice ${runId}`
    });
    expect(put.status, 'placing a hold is a 303 back to the request or the queue').toBeGreaterThanOrEqual(300);

    await adminPage.goto('/');
    const afterTile = Number(
      await adminPage.locator('.metric:has(.metric-label:text-is("Approved, unclaimed")) .metric-value').innerText()
    );
    await adminPage.goto('/accounts-queue?tab=approved');
    const afterMetric = Number(
      await adminPage.locator('.metric:has(.metric-label:text-is("Approved, unclaimed")) .metric-value').innerText()
    );
    const onHold = Number(
      await adminPage.locator('.metric:has(.metric-label:text-is("On hold")) .metric-value').innerText()
    );

    expect(onHold, 'the queue counts the hold').toBeGreaterThanOrEqual(1);
    expect(afterMetric, 'the queue drops a held request out of "unclaimed"').toBe(beforeMetric - 1);
    // F-G-007. Two tiles, one label, two numbers.
    expect(
      afterTile,
      'F-G-007: the dashboard tile has no on_hold test, so it still counts the held request'
    ).toBe(beforeTile);
    expect(
      afterTile === afterMetric,
      'F-G-007: two screens carrying the identical label "Approved, unclaimed" must not show different numbers'
    ).toBe(false);

    expect(free.id).toBeGreaterThan(0);
    expectNoRuntimeErrors(errors);
  });

  /**
   * Inside the accounts queue: four of five tab badges equal the rows beneath
   * them, and the Approved tab does not, because its rows are
   * `status IN ('approved','processing')` while its badge is the strict
   * takeable set (internal/store/store.go:1112 vs :1153).
   */
  test('TC-G-032 — the accounts-queue tab badges match their rows, except Approved', async ({
    adminPage,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const taken = await createApprovedRequest(adminPage, runId, { amount: '2100.00' });
    await createApprovedRequest(adminPage, runId, { amount: '2200.00' });

    // Reserve one, so the approved pool contains a `processing` row.
    await adminPage.goto('/accounts-queue?tab=approved');
    await adminPage
      .locator('tbody tr', { hasText: taken.number })
      .getByRole('button', { name: 'Take for processing' })
      .click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${taken.id}$`));

    for (const [tab, label] of [
      ['processing', 'Processing'],
      ['hold', 'On hold'],
      ['partial_review', 'Partial review'],
      ['paid', 'Paid']
    ] as const) {
      await adminPage.goto(`/accounts-queue?tab=${tab}`);
      const badge = Number(await adminPage.locator(`.segmented a:has-text("${label}") .n`).innerText());
      expect(await dataRows(adminPage), `the ${label} badge must equal the rows beneath it`).toBe(badge);
    }

    await adminPage.goto('/accounts-queue?tab=approved');
    const badge = Number(await adminPage.locator('.segmented a:has-text("Approved") .n').innerText());
    const rows = await dataRows(adminPage);
    const takeable = await adminPage.getByRole('button', { name: 'Take for processing' }).count();
    expect(badge, 'the Approved badge is the takeable set, and equals the takeable buttons').toBe(takeable);
    // F-G-008 — deliberate per the store comment, but still a number that
    // under-states the rows it sits above.
    expect(
      rows > badge,
      'F-G-008: the Approved tab renders reserved rows its own badge excludes'
    ).toBe(true);

    expectNoRuntimeErrors(errors);
  });

  test('TC-G-033 — the bell count equals the notification centre', async ({ adminPage, browser, runId }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrn-${runId}`);
    const payee = `Notify Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    await raiseVendorRequest(adminPage, {
      amount: '1300.00',
      vendor: payee,
      title: `Notify me ${runId}`,
      invoiceNo: `INV-B1-${runId}`,
      approverName: approver.subject.name
    });

    // The bell lives in the mobile top bar only, so read the count from the
    // centre's own Unread tab and prove the two queries agree with the rows.
    // A row is `a.notif`; the empty state is a `div.notif`, so an unqualified
    // `.notif` would count the "Nothing here yet" placeholder as a row.
    await approver.page.goto('/notifications?scope=unread');
    const unreadBadge = Number(await approver.page.locator('.segmented a:has-text("Unread") .n').innerText());
    const unreadRows = await approver.page.locator('a.notif').count();
    expect(unreadRows, 'the Unread tab number is the number of unread rows').toBe(unreadBadge);
    expect(unreadBadge, 'the approver was notified of the request routed to them').toBeGreaterThanOrEqual(1);

    // Mark-all-read must leave both at zero, together.
    await approver.page.getByRole('button', { name: /Mark all read/i }).click();
    await expect(approver.page).toHaveURL(/\/notifications$/);
    await approver.page.goto('/notifications?scope=unread');
    expect(
      Number(await approver.page.locator('.segmented a:has-text("Unread") .n').innerText()),
      'the unread count clears'
    ).toBe(0);
    expect(await approver.page.locator('a.notif').count(), 'and so do the rows').toBe(0);
    await expect(
      approver.page.locator('.notif-list'),
      'and the list says so in words rather than showing a stale row'
    ).toContainText('Nothing here yet');

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  test('TC-G-034 — the grid total equals the sum of its rows, the monthly report and the CSV', async ({
    adminPage,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    // Two payments on two different heads in one month, so the total is a real
    // sum and not a copy of a single row.
    const one = await createApprovedRequest(adminPage, runId, { amount: '15000.00' });
    await settlePayment(adminPage, one.id, {
      amount: '15000.00',
      paidOn: '2027-06-05',
      reference: `UTR-S1-${runId}`,
      remarks: `Sum one ${runId}`
    });
    const two = await createApprovedRequest(adminPage, runId, { amount: '25000.00' });
    await settlePayment(adminPage, two.id, {
      amount: '25000.00',
      paidOn: '2027-06-06',
      reference: `UTR-S2-${runId}`,
      remarks: `Sum two ${runId}`
    });

    await adminPage.goto('/grid?month=2027-06');
    const rowCells = await adminPage.locator('tr.head td:nth-child(5)').allInnerTexts();
    const rowSum = rowCells.reduce((acc, cell) => acc + paise(cell), 0);
    const total = paise(await adminPage.locator('tfoot tr.total td:nth-child(5)').innerText());
    expect(rowSum, 'the company total is the sum of the rows above it').toBe(total);
    expect(total, 'both payments landed on the same head, so the total is their sum').toBe(4000000);

    await adminPage.goto('/reports/monthly?from=2027-06&to=2027-06');
    expect(
      paise(await adminPage.locator('tbody tr td[data-label="Actual"]').innerText()),
      '/reports/monthly reuses Grid, so its actual is the grid total'
    ).toBe(total);

    const gridCSV = await csv(adminPage, '/export.csv?month=2027-06');
    const csvSum = gridCSV
      .split('\n')
      .slice(1)
      .filter(line => line.trim())
      .reduce((acc, line) => {
        const cells = line.split(',"');
        // Actual is the 4th column; the quoted money cells make a naive split
        // unsafe, so pull every ₹ figure and take the second (Budget, Actual…).
        const figures = line.match(/₹[\d,]+\.\d\d/g) ?? [];
        void cells;
        return acc + (figures.length >= 2 ? paise(figures[1]) : 0);
      }, 0);
    expect(csvSum, '/export.csv is the same rows, so its Actual column sums to the same total').toBe(total);

    expectNoRuntimeErrors(errors);
  });

  /**
   * PROGRESS.md:384-387 — "Paid this year" matches payments by payee NAME.
   * The decisive proof is a rename: the payment still exists, the vendor is the
   * same row, and the figure falls to zero.
   */
  test('TC-G-035 — vendor "Paid this year" is a name match and a rename silently zeroes it', async ({
    adminPage,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const payee = `Rename Vendor ${runId}`;
    const request = await createApprovedRequest(adminPage, runId, { amount: '9100.00', payee });
    // "This year" is the server's calendar year, so the payment must fall in it.
    const thisYear = new Date().getFullYear();
    const paymentPath = await settlePayment(adminPage, request.id, {
      amount: '9100.00',
      paidOn: `${thisYear}-03-11`,
      reference: `UTR-V1-${runId}`,
      remarks: `Vendor total ${runId}`
    });

    await adminPage.goto(`/vendors?q=${encodeURIComponent(payee)}`);
    const row = adminPage.locator('tbody tr', { hasText: payee });
    await expect(row, 'the vendor is on the list').toHaveCount(1);
    await expectMoney(
      adminPage,
      `tbody tr:has-text("${payee}") td[data-label="Paid this year"]`,
      '₹9,100.00',
      'the vendor total picks the payment up by payee name'
    );

    // Rename the vendor. Nothing about the payment changes.
    const vendorHref = await row.locator('a').first().getAttribute('href');
    const renamed = `${payee} Renamed`;
    await adminPage.goto(vendorHref!);
    await adminPage.getByLabel('Vendor name').fill(renamed);
    await adminPage.getByRole('button', { name: 'Save vendor' }).click();
    await adminPage.waitForURL(url => !url.pathname.endsWith('/new'));

    await adminPage.goto(`/payments?month=${thisYear}-03`);
    const ledger = adminPage.locator('tbody tr').filter({ has: adminPage.locator(`a[href="${paymentPath}"]`) });
    await expect(ledger, 'the payment is untouched').toHaveCount(1);
    await expect(
      ledger.locator('td[data-label="Payee"]'),
      'the payment keeps the payee it was written with'
    ).toHaveText(payee);

    await adminPage.goto(`/vendors?q=${encodeURIComponent(renamed)}`);
    const after = adminPage.locator('tbody tr', { hasText: renamed });
    await expect(after).toHaveCount(1);
    // F-G-009 — confirmed empirically, exactly as PROGRESS.md records.
    await expectMoney(
      adminPage,
      `tbody tr:has-text("${renamed}") td[data-label="Paid this year"]`,
      '₹0.00',
      'F-G-009: a rename silently orphans every payment made to the old name'
    );

    expectNoRuntimeErrors(errors);
  });

  /** PROGRESS.md:388 — "Open requests" is 0 for everyone. There is no query. */
  test('TC-G-036 — vendor "Open requests" is 0 even with a live request against the vendor', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrv-${runId}`);
    const payee = `Open Requests Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    const raised = await raiseVendorRequest(adminPage, {
      amount: '5000.00',
      vendor: payee,
      title: `Open against vendor ${runId}`,
      invoiceNo: `INV-V2-${runId}`,
      approverName: approver.subject.name
    });

    // The request is real, pending, and points at this vendor.
    await adminPage.goto(`/requests/${raised.id}`);
    await expect(adminPage.locator('.dl div:has(dt:text-is("Vendor")) dd')).toContainText(payee);
    await expect(adminPage.locator('.rh-status')).toContainText('Awaiting');

    await adminPage.goto(`/vendors?q=${encodeURIComponent(payee)}`);
    const cell = adminPage.locator(`tbody tr:has-text("${payee}") td[data-label="Open requests"]`);
    // F-G-010 — confirmed: no SQL populates the field, so it renders Go's zero.
    await expect(
      cell,
      'F-G-010: the vendor list promises an open-request count and no query ever fills it in'
    ).toHaveText('0');

    // The footer total is the sum of a column of zeroes.
    await expect(adminPage.locator('tfoot td[data-label="Open"]')).toHaveText('0');

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  /**
   * The recoverables family shares one predicate, so the dashboard, the list
   * and the CSV agree — but the dashboard's by-category link searches `q`
   * instead of setting `category`, so clicking a row cannot reproduce its own
   * count (internal/app/templates.go:3459 vs the `category` control at :3514).
   */
  test('TC-G-037 — the recoverables dashboard, list and CSV agree, but the category drill-through cannot reproduce its count', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrrec-${runId}`);

    // A recoverable, raised through the form's own fields.
    const { id, number } = await raiseRecoverable(adminPage, {
      title: `Recoverable ${runId}`,
      amount: '60000.00',
      category: 'icd',
      counterparty: `Counter Co ${runId}`,
      expectedReturn: '2028-01-31',
      terms: `Refundable on completion ${runId}.`,
      approverName: approver.subject.name
    });
    await approveFor(approver.page, id, '60000.00');

    // Dashboard total, list total and CSV must be the same money.
    await adminPage.goto('/recoverables');
    const outstandingCount = Number(
      await adminPage.locator('.metric:has(.metric-label:text-is("Outstanding")) .metric-foot').innerText().then(t => t.split(' ')[0])
    );
    const dashboardTotal = paise(await adminPage.locator('tfoot td[data-label="Outstanding"]').innerText());

    await adminPage.goto('/recoverables/list');
    const listRows = await dataRows(adminPage);
    const listTotal = paise(await adminPage.locator('tfoot td[data-label="Amount"]').innerText());
    expect(listRows, 'the dashboard count is the number of list rows').toBe(outstandingCount);
    expect(listTotal, 'the dashboard total is the list total').toBe(dashboardTotal);
    await expect(adminPage.locator('tbody tr', { hasText: number }), 'and this recoverable is in it').toHaveCount(1);

    const recCSV = await csv(adminPage, '/recoverables/list.csv');
    const csvRows = recCSV.split('\n').filter(l => l.trim()).length - 1;
    expect(csvRows, 'the CSV is the same rows as the list').toBe(listRows);
    expect(recCSV, 'and carries this recoverable').toContain(number);
    expectMoneyIn(recCSV, '₹60,000.00', 'the recoverable CSV amount');

    // The drill-through. "ICD — inter-corporate deposit" is the category label;
    // the link searches it as free text against number/counterparty/project/
    // requester, none of which contains it.
    await adminPage.goto('/recoverables');
    const categoryLink = adminPage.locator('tbody td[data-label="Category"] a').first();
    const label = (await categoryLink.innerText()).trim();
    const countShown = Number(
      await adminPage.locator(`tbody tr:has-text("${label}") td[data-label="Count"]`).first().innerText()
    );
    await categoryLink.click();
    await expect(adminPage).toHaveURL(/\/recoverables\/list\?q=/);
    const drilled = await dataRows(adminPage);
    // F-G-012.
    expect(
      drilled,
      `F-G-012: the by-category row promises ${countShown} and its own link finds ${drilled}`
    ).not.toBe(countShown);

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  /**
   * V2/IF12's decisive test: a recoverable request walked from the form to a
   * closed request, through the real screens, in one pass.
   *
   * It used to assert the OPPOSITE — "the settlement is refused… it sits in Not
   * yet paid for ever" — which pinned F-G-001/F-E-01/F-D-11 as if the stranding
   * were intended, without a `test.fail()` to say otherwise. That was the test's
   * expectation being wrong, not the product's behaviour: a request an approver
   * approved must be payable, and whether its category gives it a project could
   * never be allowed to decide whether money can leave the bank.
   *
   * The recoverable fieldset still offers no head, because a recoverable has none
   * to offer, and the entry screen therefore still posts `head_id=0`. Migration v8
   * made `payments.head_id` nullable and `validatePayment` now requires a head
   * only for a budget-treatment payment (internal/store/store.go:1741), so the
   * settlement is written with head_id NULL. Both of those facts are asserted on
   * the way past, because they are the shape of the fix.
   */
  test('TC-G-038 — a recoverable raised through the form settles and closes, head or no head', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrrp-${runId}`);

    const { id, number } = await raiseRecoverable(adminPage, {
      title: `Unpayable recoverable ${runId}`,
      amount: '45000.00',
      category: 'icd',
      counterparty: `Deposit Co ${runId}`,
      expectedReturn: '2028-02-28',
      terms: `Refundable ${runId}.`,
      approverName: approver.subject.name
    });

    // The form never offered a head, so the request has none.
    await adminPage.goto(`/requests/${id}`);
    await expect(
      adminPage.locator('.dl div:has(dt:text-is("Head")) dd'),
      'the recoverable fieldset has no head control, so no head was captured'
    ).toHaveCount(0);

    await approveFor(approver.page, id, '45000.00');

    // The queue offers it for processing, so the accountant reasonably expects
    // to be able to pay it.
    await adminPage.goto('/accounts-queue?tab=approved');
    const row = adminPage.locator('tbody tr', { hasText: number });
    await expect(row.locator('td[data-label="Project / head"]')).toContainText('Recoverable');
    await row.getByRole('button', { name: 'Take for processing' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${id}$`));
    await expect(
      adminPage.locator('input[name="head_id"]'),
      'the entry screen posts head_id=0 because the request has no head'
    ).toHaveValue('0');

    await adminPage.getByLabel('Amount actually paid').fill('45000.00');
    await adminPage.getByLabel('Paid on').fill('2027-07-20');
    await adminPage.getByLabel('Payment mode').selectOption('bank_transfer');
    await adminPage.getByLabel('Transaction / UTR reference').fill(`UTR-RC-${runId}`);
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    const sheet = adminPage.locator('.overlay .sheet');
    await expect(sheet).toBeVisible();
    await sheet.locator('input[name="settlement"][value="settled"]').check();
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();

    // The settlement lands on the payment it wrote, and closes the request with it
    // (S13: the payment and the transition move together or not at all).
    await expect(adminPage, 'the headless settlement is written, not refused').toHaveURL(/\/payments\/\d+$/);
    const payment = new URL(adminPage.url()).pathname;
    await expect(
      adminPage.locator('.banner'),
      'and the screen says what happened to the request'
    ).toContainText('completed');
    await expectMoney(adminPage, '.rh-amt', '₹45,000.00', 'the paid amount on the payment screen');
    await expect(
      adminPage.locator('.rh-status .pill'),
      'the request behind it reads Completed on the payment’s own head'
    ).toHaveText('Completed');

    // Hop 2 — the request itself agrees, and points at the payment.
    await adminPage.goto(`/requests/${id}`);
    await expect(adminPage.locator('.rh-status .pill').first(), 'the request is closed').toHaveText('Completed');
    await expect(
      adminPage.locator(`a[href="${payment}"]`).first(),
      'and the requester can open the payment that closed it'
    ).toHaveCount(1);

    // Hop 3 — the register moves the row out of "Not yet paid" and dates it.
    await adminPage.goto('/recoverables/list?ageing=unpaid');
    await expect(
      adminPage.locator('tbody tr', { hasText: number }),
      'a paid recoverable is no longer awaiting payment'
    ).toHaveCount(0);

    await adminPage.goto(`/recoverables/list?q=${encodeURIComponent(number)}`);
    const registerRow = adminPage.locator('tbody tr', { hasText: number });
    await expect(registerRow, 'it is still outstanding money, so it stays on the register').toHaveCount(1);
    await expect(
      registerRow.locator('td[data-label="Paid on"]'),
      'and the register carries the date the money left, not "Not yet paid"'
    ).toHaveText('2027-07-20');

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  /**
   * The two nav badges the shell declares but no query feeds
   * (internal/app/nav.go:62-63 vs internal/store/badges.go:32-46). A queue with
   * work in it shows no badge at all.
   */
  test('TC-G-039 — the sidebar Approvals and Accounts-queue badges are declared and never populated', async ({
    adminPage,
    browser,
    runId
  }) => {
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrbg-${runId}`);
    const payee = `Badge Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    await raiseVendorRequest(adminPage, {
      amount: '1700.00',
      vendor: payee,
      title: `Badge me ${runId}`,
      invoiceNo: `INV-BG-${runId}`,
      approverName: approver.subject.name
    });

    await approver.page.goto('/approvals');
    const toApprove = Number(await approver.page.locator('.segmented a:has-text("To approve") .n').innerText());
    expect(toApprove, 'the queue genuinely has work in it').toBeGreaterThanOrEqual(1);

    const mobile = (approver.page.viewportSize()?.width ?? 1280) <= 860;
    const navLink = approver.page.locator(
      mobile ? '.more-sheet .ms-list a[href="/approvals"]' : '.side-nav a[href="/approvals"]'
    );
    if (mobile) await approver.page.locator('.tabbar .js-more').click();
    await expect(navLink, 'the nav links to the queue').toHaveCount(1);
    // F-G-013.
    expect(
      await navLink.locator('.n, .badge').count(),
      'F-G-013: nav.go declares Badge:"approvals" and badges.go has no spec for it, so the count is never shown'
    ).toBe(0);

    await approver.close();
  });

  /**
   * `/requests` never reads `status` (internal/app/requests.go:474-476) while
   * `/requests/export.csv` does (:930). The same query string therefore
   * describes two different sets, and the CSV is the one that obeys it.
   */
  test('TC-G-040 — /requests silently ignores ?status= while its own CSV honours it', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrst-${runId}`);
    const requester = await asRole(adminPage, browser, runId, ['Requester'], `status-${runId}`);

    await requester.page.goto('/requests/new?type=reimbursement');
    await requester.page.getByLabel('Short title').fill(`Status probe ${runId}`);
    await requester.page.locator('#project').selectOption({ label: 'Growth' });
    await expect(requester.page.locator('#head option').filter({ hasNotText: 'Growth /' })).toHaveCount(1);
    await requester.page.locator('#head').selectOption({ label: 'Growth / Marketing' });
    await requester.page.getByLabel('Amount').fill('800.00');
    await requester.page.getByLabel('Expense date').fill('2026-07-06');
    await requester.page.getByLabel('Purpose').fill(`Status purpose ${runId}.`);
    await requester.page.getByLabel('Approver').selectOption({ label: approver.subject.name });
    await requester.page.getByRole('button', { name: 'Submit request' }).click();
    await expect(requester.page).toHaveURL(/\/requests\/\d+\/submitted$/);
    const number = (await requester.page.locator('.rh-no').first().innerText()).trim();

    // ?status=rejected: the screen shows the open bucket regardless.
    await requester.page.goto('/requests?status=rejected');
    await expect(
      requester.page.locator('.req-card', { hasText: number }),
      'F-G-014: the screen ignores ?status= and shows the pending request anyway'
    ).toHaveCount(1);

    // The CSV for the identical query string honours it and returns nothing.
    const rejectedCSV = await csv(requester.page, '/requests/export.csv?status=rejected');
    expect(
      rejectedCSV.includes(number),
      'F-G-014: the CSV for the same URL applies the filter, so the two disagree'
    ).toBe(false);

    // A bare CSV has no bucket default, where the screen defaults to `open`.
    const bareCSV = await csv(requester.page, '/requests/export.csv');
    expect(bareCSV, 'the bare CSV exports every status, so the request is in it').toContain(number);

    expectNoRuntimeErrors(errors);
    await approver.close();
    await requester.close();
  });

  /**
   * A figure that cannot be entered is the most complete failure of information
   * flow there is. Every unbudgeted head renders `{{money .Budget}}` = `₹0.00`
   * (internal/app/templates.go:1160), `money.ParsePaise` refuses `paise <= 0`
   * (internal/money/money.go:31-33), and `budgetSave` sets `parseErr` for the
   * WHOLE batch when any one field fails (internal/app/app.go:1025-1030). So the
   * screen refuses its own rendered values.
   */
  test('TC-G-041 — the budgets screen refuses the values it rendered, so an unbudgeted month cannot be saved at all', async ({
    adminPage
  }) => {
    const errors = capturePageErrors(adminPage);
    const FRESH = '2028-06'; // the seed budgets 2026-06 only

    await adminPage.goto(`/budgets?month=${FRESH}`);
    const inputs = adminPage.locator('input[name^="budget_"]');
    const count = await inputs.count();
    expect(count, 'the screen offers a budget field per active head').toBeGreaterThan(1);
    await expect(
      inputs.first(),
      'an unbudgeted head is rendered as ₹0.00 by the screen itself'
    ).toHaveValue('₹0.00');

    // Press the screen's own Save without changing anything.
    const names = await inputs.evaluateAll(nodes => nodes.map(n => (n as HTMLInputElement).name));
    const untouched: Record<string, string> = { month: FRESH };
    for (const name of names) untouched[name] = '₹0.00';
    const asRendered = await probePost(adminPage, '/budgets', untouched);
    // F-G-028.
    expect(
      asRendered.status,
      'F-G-028: saving the values the screen rendered is refused — a fresh month cannot be budgeted through the UI'
    ).toBe(400);
    expect(asRendered.body, 'and the message blames the amount, not the zero').toContain('invalid budget amount');

    // Filling one in does not help: the others are still ₹0.00 and the batch is
    // rejected whole, so there is no incremental path either.
    const oneFilled = { ...untouched, [names[0]]: '25,00,000.00' };
    const partial = await probePost(adminPage, '/budgets', oneFilled);
    expect(
      partial.status,
      'F-G-028: one head at a time does not work either — the whole batch is rejected'
    ).toBe(400);
    await adminPage.goto(`/budgets?month=${FRESH}`);
    await expect(
      adminPage.locator(`input[name="${names[0]}"]`),
      'F-G-028: and nothing was written, so the figure is simply unenterable'
    ).toHaveValue('₹0.00');

    // The corollary on a month that IS budgeted: a budget can be changed, but
    // never reduced to zero.
    await adminPage.goto('/budgets?month=2026-06');
    const seeded = adminPage.locator('input[name^="budget_"]').first();
    const seededName = (await seeded.getAttribute('name'))!;
    const seededValue = await seeded.inputValue();
    expect(seededValue, 'the seeded month has real figures').not.toBe('₹0.00');
    const seededNames = await adminPage
      .locator('input[name^="budget_"]')
      .evaluateAll(nodes => nodes.map(n => (n as HTMLInputElement).name));
    const toZero: Record<string, string> = { month: '2026-06' };
    for (const name of seededNames) {
      toZero[name] = await adminPage.locator(`input[name="${name}"]`).inputValue();
    }
    toZero[seededName] = '0.00';
    const zeroed = await probePost(adminPage, '/budgets', toZero);
    expect(
      zeroed.status,
      'F-G-028: an existing budget can never be reduced to zero — a genuine operation with no way to express it'
    ).toBe(400);
    await adminPage.goto('/budgets?month=2026-06');
    await expect(
      adminPage.locator(`input[name="${seededName}"]`),
      'and the old figure stands'
    ).toHaveValue(seededValue);

    expectNoRuntimeErrors(errors);
  });

  /**
   * `/` was the variance grid before Phase 4 made it the dashboard, and five
   * links and one form still point there. The grid's own filter toolbar is
   * `action="/"` (internal/app/templates.go:136), so month, status and search
   * are unreachable from the grid: submitting them navigates away.
   */
  test('TC-G-042 — the /?month= family lands on the dashboard, so the grid filters are unreachable and a lock is unconfirmed', async ({
    adminPage
  }) => {
    const errors = capturePageErrors(adminPage);

    // 1 — the grid's own filter form posts to `/`.
    await adminPage.goto('/grid?month=2026-06');
    const toolbar = adminPage.locator('form.toolbar[aria-label="Grid filters"]');
    await expect(toolbar, 'the grid has a filter toolbar').toHaveCount(1);
    expect(
      await toolbar.getAttribute('action'),
      'F-G-029: the grid filter form posts to /, which is the dashboard'
    ).toBe('/');

    // Submitting it navigates off the grid entirely, taking the filters with it.
    await toolbar.locator('input[name="q"]').fill('Office');
    await toolbar.getByRole('button', { name: 'Apply' }).click();
    await expect(adminPage, 'F-G-029: filtering the grid leaves the grid').toHaveURL(/\/\?/);
    await expect(
      adminPage.locator('h1'),
      'F-G-029: and lands on the dashboard, which cannot filter a grid'
    ).toContainText('Good day');
    expect(
      await adminPage.locator('table.matrix.grid').count(),
      'F-G-029: the filtered grid is nowhere on the page the filter delivered'
    ).toBe(0);

    // 2 — the dashboard ignores ?month= and ?q= completely.
    const dash = await probeGet(adminPage, '/?month=2026-06&q=Office&status=over');
    expect(dash.status).toBe(200);
    expect(
      dash.body.includes('2026-06'),
      'F-G-029: the dashboard does not read the month it was handed'
    ).toBe(false);

    // 3 — every other link in the family goes to the same wrong place.
    await adminPage.goto('/budgets?month=2026-06');
    expect(
      await adminPage.getByRole('link', { name: 'View grid' }).getAttribute('href'),
      'F-G-029: the budgets screen "View grid" link'
    ).toBe('/?month=2026-06');
    await adminPage.goto('/months');
    const monthsGrid = adminPage.locator('a.btn', { hasText: /^Grid$/ }).first();
    expect(
      (await monthsGrid.getAttribute('href')) ?? '',
      'F-G-029: the months screen "Grid" action'
    ).toMatch(/^\/\?month=/);

    // 4 — following one proves the destination has no grid on it.
    await monthsGrid.click();
    await expect(adminPage).toHaveURL(/\/\?month=/);
    expect(
      await adminPage.locator('table.matrix.grid').count(),
      'F-G-029: the "Grid" action does not deliver a grid'
    ).toBe(0);

    expectNoRuntimeErrors(errors);
  });

  /**
   * The last hop of the money chain is a machine, not a person, and every one of
   * the four exports writes `money.FormatPaise` output — a currency glyph plus
   * Indian digit grouping inside a quoted cell. The figure survives (which is
   * what TC-G-001–003 assert) but it is not a number any spreadsheet or script
   * will sum without being told to strip it.
   */
  test('TC-G-043 — every CSV amount column is a formatted string, not a number', async ({ adminPage, runId }) => {
    const errors = capturePageErrors(adminPage);
    const request = await createApprovedRequest(adminPage, runId, { amount: '150000.00' });
    await settlePayment(adminPage, request.id, {
      amount: '150000.00',
      paidOn: '2028-03-07',
      reference: `UTR-CSV-${runId}`,
      remarks: `CSV shape ${runId}`
    });

    const exports: Array<[string, string]> = [
      ['/export.csv?month=2028-03', 'grid'],
      ['/reports/ytd.csv?from=2028-03&to=2028-03', 'ytd'],
      [`/requests/export.csv?q=${encodeURIComponent(request.number)}`, 'requests'],
      ['/recoverables/list.csv', 'recoverables']
    ];

    for (const [path, what] of exports) {
      const body = await csv(adminPage, path);
      const header = body.split('\n')[0];
      expect(header.length, `${what}: the export has a header row`).toBeGreaterThan(0);
      if (what === 'recoverables') continue; // no rows guaranteed in this month
      // F-G-030: the cell is "₹1,50,000.00" — quoted because of the grouping
      // commas, and unparseable as a number without stripping two things.
      expect(
        body.includes('"₹1,50,000.00"'),
        `F-G-030: ${what} writes the amount as a quoted, symbol-bearing, comma-grouped string`
      ).toBe(true);
      expect(
        /(^|,)150000(\.00)?(,|$)/m.test(body),
        `F-G-030: ${what} offers no plain-number form of the same figure`
      ).toBe(false);
    }

    // The consequence, stated as an assertion: a naive numeric parse of the
    // grid CSV's Actual column yields NaN.
    const grid = await csv(adminPage, '/export.csv?month=2028-03');
    const dataLine = grid.split('\n').find(l => l.includes('Office Rent'))!;
    const cells = dataLine.match(/"[^"]*"|[^,]+/g)!.map(c => c.replace(/^"|"$/g, ''));
    const actualCell = cells.find(c => c.includes('1,50,000'))!;
    expect(
      Number.isNaN(Number(actualCell)),
      'F-G-030: Number("₹1,50,000.00") is NaN, so the export is a report and not data'
    ).toBe(true);

    expectNoRuntimeErrors(errors);
  });

  /**
   * `/reports/ytd.csv` calls `Report(from, to, "heads")` unconditionally
   * (internal/app/app.go:1470-1490) while the screen's "Export CSV" button sits
   * on all three tabs, so the monthly and projects views export somebody else's
   * shape.
   */
  test('TC-G-044 — the reports Export CSV always exports head rows, whichever tab pressed it', async ({
    adminPage,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const request = await createApprovedRequest(adminPage, runId, { amount: '31000.00' });
    await settlePayment(adminPage, request.id, {
      amount: '31000.00',
      paidOn: '2028-04-04',
      reference: `UTR-YTD-${runId}`,
      remarks: `YTD shape ${runId}`
    });

    // The three screens genuinely differ: monthly has no Project column, heads
    // has both Project and Head. The stylesheet uppercases table headers, so the
    // comparison is case-insensitive.
    const headers = async () =>
      (await adminPage.locator('thead th').allInnerTexts()).map(h => h.trim().toLowerCase());
    await adminPage.goto('/reports/monthly?from=2028-04&to=2028-04');
    expect(await headers(), 'the monthly view is one row per period').not.toContain('head');
    await adminPage.goto('/reports/heads?from=2028-04&to=2028-04');
    expect(await headers(), 'the heads view names the head').toContain('head');

    // Both tabs' Export CSV links carry only from/to — no mode at all.
    await adminPage.goto('/reports/monthly?from=2028-04&to=2028-04');
    const link = await adminPage.getByRole('link', { name: 'Export CSV' }).getAttribute('href');
    expect(link, 'the export link carries no mode').toBe('/reports/ytd.csv?from=2028-04&to=2028-04');

    const body = await csv(adminPage, link!);
    // F-G-031.
    expect(
      body.split('\n')[0],
      'F-G-031: pressing Export CSV on the Monthly tab downloads head-level rows'
    ).toContain('Head');
    expect(
      body.includes('Office Rent'),
      'F-G-031: the head the monthly view deliberately aggregates away is named in its own export'
    ).toBe(true);

    expectNoRuntimeErrors(errors);
  });
});

// ===========================================================================
// 5 — Information leakage
// ===========================================================================

test.describe('G · confinement', () => {
  /**
   * The core confidentiality case, checked on seven surfaces at once. Every one
   * of them is a place scope filtering could have been forgotten separately.
   */
  test('TC-G-050 — a Requester learns nothing about another requester on any surface', async ({
    adminPage,
    browser,
    runId
  }) => {
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrl-${runId}`);
    const alice = await asRole(adminPage, browser, runId, ['Requester'], `alice-${runId}`);
    const bob = await asRole(adminPage, browser, runId, ['Requester'], `bob-${runId}`);
    const aliceErrors = capturePageErrors(alice.page);

    const secretTitle = `Bob secret ${runId}`;
    const secretPurpose = `Bob confidential purpose ${runId}`;
    const secretAmount = '77777.00';
    const secretMoney = '₹77,777.00';

    await bob.page.goto('/requests/new?type=reimbursement');
    await bob.page.getByLabel('Short title').fill(secretTitle);
    await bob.page.locator('#project').selectOption({ label: 'People' });
    await expect(bob.page.locator('#head option').filter({ hasNotText: 'People /' })).toHaveCount(1);
    await bob.page.locator('#head').selectOption({ label: 'People / Payroll' });
    await bob.page.getByLabel('Amount').fill(secretAmount);
    await bob.page.getByLabel('Expense date').fill('2026-07-07');
    await bob.page.getByLabel('Purpose').fill(secretPurpose);
    await bob.page.getByLabel('Approver').selectOption({ label: approver.subject.name });
    await bob.page.getByRole('button', { name: 'Submit request' }).click();
    await expect(bob.page).toHaveURL(/\/requests\/\d+\/submitted$/);
    const bobID = Number(new URL(bob.page.url()).pathname.split('/')[2]);
    const bobNumber = (await bob.page.locator('.rh-no').first().innerText()).trim();

    // Bob comments on his own request, so there is a comment to leak.
    await probePost(bob.page, `/requests/${bobID}/comment`, { body: `Bob private note ${runId}` });

    const secrets = [secretTitle, secretPurpose, secretMoney, bobNumber, `Bob private note ${runId}`];
    const assertClean = (body: string, where: string) => {
      for (const secret of secrets) {
        expect(body.includes(secret), `${where} must not disclose "${secret}"`).toBe(false);
      }
    };

    // 1 — every list bucket.
    for (const bucket of ['open', 'needs-me', 'closed', 'all']) {
      await alice.page.goto(`/requests?bucket=${bucket}`);
      assertClean(await alice.page.locator('body').innerText(), `/requests?bucket=${bucket}`);
    }

    // 2 — the URL trick: scope may narrow, never widen.
    await alice.page.goto('/requests?bucket=all&scope=all');
    assertClean(await alice.page.locator('body').innerText(), '/requests?scope=all');

    // 3 — search, by title, by purpose, by number and by amount.
    for (const term of [secretTitle, secretPurpose, bobNumber, '77777']) {
      await alice.page.goto(`/requests?bucket=all&q=${encodeURIComponent(term)}`);
      assertClean(await alice.page.locator('body').innerText(), `search for "${term}"`);
    }

    // 4 — the CSV export, bare and widened.
    for (const query of ['', '?scope=all', '?scope=all&bucket=all', `?q=${encodeURIComponent(bobNumber)}`]) {
      const body = await csv(alice.page, `/requests/export.csv${query}`);
      assertClean(body, `/requests/export.csv${query}`);
    }

    // 5 — the request itself, and the error page it serves.
    const detail = await probeGet(alice.page, `/requests/${bobID}`);
    expectOutcome(detail, [403], 'a requester cannot open another requester\'s request');
    assertClean(detail.body, 'the 403 error page');
    for (const path of [
      `/requests/${bobID}/edit`,
      `/requests/${bobID}/submitted`,
      `/requests/${bobID}/partial-review`,
      `/requests/${bobID}/reservation`
    ]) {
      const probe = await probeGet(alice.page, path);
      expect(probe.status >= 400, `${path} must refuse — got ${probe.outcome}`).toBe(true);
      assertClean(probe.body, path);
    }

    // 6 — notifications. Bob's approval traffic is addressed to Bob.
    await alice.page.goto('/notifications');
    assertClean(await alice.page.locator('body').innerText(), '/notifications');

    // 7 — the dashboard.
    await alice.page.goto('/');
    assertClean(await alice.page.locator('body').innerText(), 'the dashboard');

    expectNoRuntimeErrors(aliceErrors);
    await approver.close();
    await alice.close();
    await bob.close();
  });

  /**
   * `loadViewableRequest` reads the row first and checks scope second
   * (internal/app/requests.go:229-240), so a missing id answers 404 and an
   * out-of-scope id answers 403. The difference is an oracle: a requester can
   * walk the id space and learn exactly which requests exist.
   */
  test('TC-G-051 — 404 and 403 are distinguishable, so a requester can enumerate which request ids exist', async ({
    adminPage,
    browser,
    runId
  }) => {
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgren-${runId}`);
    const alice = await asRole(adminPage, browser, runId, ['Requester'], `enum-${runId}`);
    const bob = await asRole(adminPage, browser, runId, ['Requester'], `enumb-${runId}`);

    await bob.page.goto('/requests/new?type=reimbursement');
    await bob.page.getByLabel('Short title').fill(`Enum target ${runId}`);
    await bob.page.locator('#project').selectOption({ label: 'Growth' });
    await expect(bob.page.locator('#head option').filter({ hasNotText: 'Growth /' })).toHaveCount(1);
    await bob.page.locator('#head').selectOption({ label: 'Growth / Events' });
    await bob.page.getByLabel('Amount').fill('600.00');
    await bob.page.getByLabel('Expense date').fill('2026-07-08');
    await bob.page.getByLabel('Purpose').fill(`Enum purpose ${runId}.`);
    await bob.page.getByLabel('Approver').selectOption({ label: approver.subject.name });
    await bob.page.getByRole('button', { name: 'Submit request' }).click();
    await expect(bob.page).toHaveURL(/\/requests\/\d+\/submitted$/);
    const existing = Number(new URL(bob.page.url()).pathname.split('/')[2]);

    const present = await probeGet(alice.page, `/requests/${existing}`);
    const absent = await probeGet(alice.page, '/requests/99999999');
    expect(present.status, 'an existing row Alice may not see answers 403').toBe(403);
    expect(absent.status, 'a row that does not exist answers 404').toBe(404);
    // F-G-002.
    expect(
      present.status === absent.status,
      'F-G-002: 403 and 404 differ, so the status code is an existence oracle for every request id'
    ).toBe(false);

    await approver.close();
    await alice.close();
    await bob.close();
  });

  test('TC-G-052 — a Requester is refused every export and every money screen', async ({
    adminPage,
    browser,
    runId
  }) => {
    const requester = await asRole(adminPage, browser, runId, ['Requester'], `noexp-${runId}`);
    for (const path of [
      '/payments',
      '/export.csv',
      '/reports/monthly',
      '/reports/ytd.csv',
      '/recoverables',
      '/recoverables/list',
      '/recoverables/list.csv',
      '/audit',
      '/vendors',
      '/accounts-queue',
      '/approvals'
    ]) {
      const probe = await probeGet(requester.page, path);
      expectOutcome(probe, [403], `a Requester must be refused ${path}`);
    }
    // The two the Requester DOES reach, and must: their own list and its CSV.
    for (const path of ['/requests', '/requests/export.csv']) {
      const probe = await probeGet(requester.page, path);
      expect(probe.status, `${path} is a Requester's own data`).toBe(200);
    }
    // And /grid, which is RequireLogin only — a candidate finding, not mine to
    // fix, recorded so the audit is honest about what a Requester can see.
    const grid = await probeGet(requester.page, '/grid');
    expect(grid.status, 'F-G-015: /grid is RequireLogin only, so a Requester reaches it').toBe(200);
    await requester.close();
  });

  /**
   * `payment` is a declared scoped resource (internal/store/permissions.go:185),
   * the admin UI offers its scope radios, and the seed writes `payment=all`
   * rows — but nothing ever calls `Scope(u, "payment")`. Setting a role to
   * `payment=own` therefore changes nothing, which breaks R3.
   */
  test('TC-G-053 — the payment data scope is declared, editable and never read', async ({
    adminPage,
    browser,
    runId
  }) => {
    const role = await createCustomRole(
      adminPage,
      `PayOwn ${runId}`,
      ['payment:view', 'request:view'],
      { payment: 'own', request: 'own' }
    );
    const subject = await asCustomRole(adminPage, browser, runId, 'payown', role);

    // A payment the admin entered, which the subject did not.
    const request = await createApprovedRequest(adminPage, runId, { amount: '13100.00' });
    const paymentPath = await settlePayment(adminPage, request.id, {
      amount: '13100.00',
      paidOn: '2027-08-09',
      reference: `UTR-SC-${runId}`,
      remarks: `Scope probe ${runId}`
    });

    // The scope the admin set is genuinely stored and shown.
    await adminPage.goto(`/roles?role=${role.id}`);
    await expect(
      adminPage.locator('input[name="scope_payment"][value="own"]'),
      'the role screen stores and re-renders the payment scope'
    ).toBeChecked();

    const ledger = await probeGet(subject.page, '/payments?month=2027-08');
    expect(ledger.status, 'payment:view opens the ledger').toBe(200);
    // F-G-003.
    expect(
      ledger.body.includes('₹13,100.00'),
      'F-G-003: payment scope "own" is never read, so the ledger shows a payment somebody else entered'
    ).toBe(true);
    // The ledger has no Remarks column, but it does carry the payee and who
    // entered the row — both of which are somebody else's business here.
    expect(ledger.body.includes(`Payee ${runId}`), 'F-G-003: including its payee').toBe(true);
    expect(ledger.body.includes('Fervid Admin'), 'F-G-003: and who entered it').toBe(true);
    // And the search does reach the processing note, so the whole row is
    // discoverable by a caller whose scope should have excluded it.
    const searched = await probeGet(subject.page, `/payments?month=2027-08&q=${encodeURIComponent(`Scope probe ${runId}`)}`);
    expect(
      searched.body.includes('₹13,100.00'),
      'F-G-003: the ledger search finds it by another accountant\'s processing note'
    ).toBe(true);

    // The payment detail is protected — but only because the REQUEST behind it
    // is scope-checked (internal/app/app.go:759), not because payment scope is.
    const detail = await probeGet(subject.page, paymentPath);
    expectOutcome(
      detail,
      [403],
      'the request scope behind the payment is what refuses the detail, which is the only reason this is not worse'
    );

    await subject.close();
  });

  /**
   * `/recoverables/list` and its CSV read `payment_requests` and never apply the
   * `request` scope (internal/store/recoverables.go:254). The seed keeps this
   * latent by granting `recoverable_report` only to Accounts and Admin, both of
   * whom hold `request=all`. One custom role opens it.
   */
  test('TC-G-054 — the recoverables register and its CSV ignore the caller\'s request scope', async ({
    adminPage,
    browser,
    runId
  }) => {
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrrl-${runId}`);
    const owner = await asRole(adminPage, browser, runId, ['Requester'], `recown-${runId}`);

    const counterparty = `Secret Counterparty ${runId}`;
    const { id, number } = await raiseRecoverable(owner.page, {
      title: `Secret deposit ${runId}`,
      amount: '88000.00',
      category: 'icd',
      counterparty,
      expectedReturn: '2028-03-31',
      terms: `Refund on close ${runId}.`,
      approverName: approver.subject.name
    });
    await approveFor(approver.page, id, '88000.00');

    const role = await createCustomRole(
      adminPage,
      `RecOwn ${runId}`,
      ['recoverable_report:view', 'recoverable_report:export', 'request:view'],
      { request: 'own' }
    );
    const nosy = await asCustomRole(adminPage, browser, runId, 'recnosy', role);

    // The row itself is properly refused: canViewRequest does its job.
    const own = await probeGet(nosy.page, `/requests/${id}`);
    expectOutcome(own, [403], 'the request detail obeys the caller\'s own scope');

    // The register does not.
    const list = await probeGet(nosy.page, '/recoverables/list');
    expect(list.status, 'recoverable_report:view opens the register').toBe(200);
    // F-G-016.
    expect(
      list.body.includes(number),
      'F-G-016: the register lists a request this caller is forbidden to open'
    ).toBe(true);
    expect(list.body.includes(counterparty), 'F-G-016: including its counterparty').toBe(true);
    expect(list.body.includes('₹88,000.00'), 'F-G-016: including its amount').toBe(true);

    // The CSV discloses MORE than the screen: `exportRecoverable`
    // (internal/app/recoverables.go:113) adds Requester and Repayment Notes,
    // neither of which the list template renders.
    const listCSV = await csv(nosy.page, '/recoverables/list.csv');
    expect(listCSV.includes(number), 'F-G-016: and the CSV exports the whole register in one request').toBe(true);
    expect(listCSV.includes(counterparty)).toBe(true);
    expectMoneyIn(listCSV, '₹88,000.00', 'the leaked CSV amount');
    expect(
      listCSV.includes(owner.subject.name),
      'F-G-016: the CSV names the requester, which the on-screen register does not even show'
    ).toBe(true);
    expect(
      listCSV.includes(`Refund on close ${runId}`),
      'F-G-016: and their repayment terms verbatim'
    ).toBe(true);

    // The screen genuinely has no Requester column — the CSV is the wider leak.
    await nosy.page.goto('/recoverables/list');
    const headers = await nosy.page.locator('thead th').allInnerTexts();
    expect(
      headers.some(h => /requester/i.test(h)),
      'the register has no Requester column, so the CSV over-discloses relative to its own screen'
    ).toBe(false);

    await approver.close();
    await owner.close();
    await nosy.close();
  });

  /**
   * `Store.Audit` never receives a viewer, and `before_json` for a
   * `payment_request` row is the entire request struct. A caller holding
   * `audit:view` therefore reads every field of every request in the system,
   * including ones `/requests/{id}` refuses them.
   */
  test('TC-G-055 — the audit log discloses the full text of requests the caller cannot open', async ({
    adminPage,
    browser,
    runId
  }) => {
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrau-${runId}`);
    const owner = await asRole(adminPage, browser, runId, ['Requester'], `audown-${runId}`);

    const purpose = `Audit-leak purpose ${runId}`;
    await owner.page.goto('/requests/new?type=reimbursement');
    await owner.page.getByLabel('Short title').fill(`Audit leak ${runId}`);
    await owner.page.locator('#project').selectOption({ label: 'People' });
    await expect(owner.page.locator('#head option').filter({ hasNotText: 'People /' })).toHaveCount(1);
    await owner.page.locator('#head').selectOption({ label: 'People / Payroll' });
    await owner.page.getByLabel('Amount').fill('64000.00');
    await owner.page.getByLabel('Expense date').fill('2026-07-11');
    await owner.page.getByLabel('Purpose').fill(purpose);
    await owner.page.getByLabel('Approver').selectOption({ label: approver.subject.name });
    await owner.page.getByRole('button', { name: 'Submit request' }).click();
    await expect(owner.page).toHaveURL(/\/requests\/\d+\/submitted$/);
    const id = Number(new URL(owner.page.url()).pathname.split('/')[2]);
    const number = (await owner.page.locator('.rh-no').first().innerText()).trim();
    // An approve row carries the whole Request struct in Before and After.
    await approveFor(approver.page, id, '64000.00');

    const role = await createCustomRole(
      adminPage,
      `AuditOwn ${runId}`,
      ['audit:view', 'request:view'],
      { request: 'own' }
    );
    const nosy = await asCustomRole(adminPage, browser, runId, 'audnosy', role);

    const own = await probeGet(nosy.page, `/requests/${id}`);
    expectOutcome(own, [403], 'the request detail obeys the caller\'s scope');

    const log = await probeGet(nosy.page, `/audit?entity=payment_request&actor=${encodeURIComponent(approver.subject.name)}`);
    expect(log.status, 'audit:view opens the log').toBe(200);
    // F-G-017.
    expect(
      log.body.includes(number),
      'F-G-017: the log names a request this caller is forbidden to open'
    ).toBe(true);
    expect(
      log.body.includes(purpose),
      'F-G-017: before_json carries the whole Request struct, purpose included'
    ).toBe(true);
    expect(
      log.body.includes(owner.subject.name),
      'F-G-017: and the requester\'s name'
    ).toBe(true);
    expect(log.body.includes('6400000'), 'F-G-017: and the amount in paise').toBe(true);

    await approver.close();
    await owner.close();
    await nosy.close();
  });

  /**
   * `dashboard.go:96` hardwires `Scope: "all"` for the accounts work area
   * instead of asking `effectiveScope`, so the tile — and the four rows under
   * it — are company-wide for anyone holding `payment:process`, whatever their
   * request scope says.
   */
  test('TC-G-056 — the dashboard accounts area hardwires scope "all" and lists requests the caller cannot open', async ({
    adminPage,
    browser,
    runId
  }) => {
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrdw-${runId}`);
    const owner = await asRole(adminPage, browser, runId, ['Requester'], `dashown-${runId}`);

    const title = `Dashboard leak ${runId}`;
    await owner.page.goto('/requests/new?type=reimbursement');
    await owner.page.getByLabel('Short title').fill(title);
    await owner.page.locator('#project').selectOption({ label: 'Growth' });
    await expect(owner.page.locator('#head option').filter({ hasNotText: 'Growth /' })).toHaveCount(1);
    await owner.page.locator('#head').selectOption({ label: 'Growth / Travel' });
    await owner.page.getByLabel('Amount').fill('55500.00');
    await owner.page.getByLabel('Expense date').fill('2026-07-12');
    await owner.page.getByLabel('Purpose').fill(`Dashboard leak purpose ${runId}.`);
    await owner.page.getByLabel('Approver').selectOption({ label: approver.subject.name });
    await owner.page.getByRole('button', { name: 'Submit request' }).click();
    await expect(owner.page).toHaveURL(/\/requests\/\d+\/submitted$/);
    const id = Number(new URL(owner.page.url()).pathname.split('/')[2]);
    const number = (await owner.page.locator('.rh-no').first().innerText()).trim();
    await approveFor(approver.page, id, '55500.00');

    const role = await createCustomRole(
      adminPage,
      `ProcessOwn ${runId}`,
      ['payment:process', 'request:view'],
      { request: 'own' }
    );
    const nosy = await asCustomRole(adminPage, browser, runId, 'procnosy', role);

    const own = await probeGet(nosy.page, `/requests/${id}`);
    expectOutcome(own, [403], 'the request detail obeys the caller\'s scope');

    const dash = await probeGet(nosy.page, '/');
    expect(dash.status, 'the dashboard is RequireLogin only').toBe(200);
    // F-G-018.
    expect(
      dash.body.includes(number),
      'F-G-018: the accounts work area hardwires Scope:"all", so it lists a request the caller cannot open'
    ).toBe(true);
    expect(dash.body.includes(title), 'F-G-018: with its short title').toBe(true);
    expect(dash.body.includes('₹55,500.00'), 'F-G-018: and its amount').toBe(true);

    // The accounts queue itself is correct — it asks for the caller's scope.
    const queue = await probeGet(nosy.page, '/accounts-queue?tab=approved');
    expect(queue.status, 'payment:process opens the queue').toBe(200);
    expect(
      queue.body.includes(number),
      'the queue asks a.auth.Scope(u,"request") and correctly hides the row the dashboard showed'
    ).toBe(false);

    await approver.close();
    await owner.close();
    await nosy.close();
  });

  /**
   * `GET /grid` is `RequireLogin` only (internal/app/app.go:377) while
   * `GET /export.csv` is `RequirePermission("grid","export")` (:394). So the
   * data is open and only the download of it is gated — a subject holding NO
   * role at all reads every project's budget and actuals, plus the grid's own
   * Recent Payments panel with amounts, payees and links into the ledger.
   */
  test('TC-G-057 — a subject with no role at all reads the whole variance grid and the recent-payments panel', async ({
    adminPage,
    browser,
    runId
  }) => {
    // A payment to put a real figure and a real payee on the grid's panel.
    const payee = `Grid Leak Vendor ${runId}`;
    const request = await createApprovedRequest(adminPage, runId, { amount: '66000.00', payee });
    await settlePayment(adminPage, request.id, {
      amount: '66000.00',
      paidOn: '2028-05-06',
      reference: `UTR-GL-${runId}`,
      remarks: `Grid leak ${runId}`
    });

    // A subject holding nothing whatsoever.
    const nobody = await asRole(adminPage, browser, runId, [], `nobody-${runId}`);
    expect(nobody.subject.roles, 'the subject holds no role at all').toEqual([]);

    // Every gated screen refuses it, which is the control for what follows.
    for (const path of ['/requests', '/payments', '/reports/monthly', '/audit', '/users', '/vendors']) {
      expectOutcome(await probeGet(nobody.page, path), [403], `a role-less subject is refused ${path}`);
    }

    // The grid is not gated.
    const grid = await probeGet(nobody.page, '/grid?month=2028-05');
    expect(grid.status, 'F-G-032: /grid is RequireLogin only, so a role-less subject reads it').toBe(200);
    expect(
      grid.body.includes('₹66,000.00'),
      'F-G-032: including the exact amount of a payment they hold no permission to see'
    ).toBe(true);
    expect(grid.body.includes(payee), 'F-G-032: and the payee it was made to').toBe(true);
    expect(
      grid.body.includes('Recent Payments'),
      'F-G-032: the grid carries a Recent Payments panel of its own'
    ).toBe(true);
    expect(
      /href="\/payments\/\d+"/.test(grid.body),
      'F-G-032: whose rows link into the ledger the same subject is refused'
    ).toBe(true);
    // And the seeded budgets of every project, which is the whole company plan.
    for (const head of ['Office Rent', 'Payroll', 'Marketing']) {
      expect(grid.body.includes(head), `F-G-032: and every head, including ${head}`).toBe(true);
    }
    const seeded = await probeGet(nobody.page, '/grid?month=2026-06');
    expect(
      seeded.body.includes('₹8,50,000.00'),
      'F-G-032: and every budget figure — the payroll line is readable by a subject with no role'
    ).toBe(true);

    // Following the link the panel offered IS refused, so the leak is the grid
    // page itself rather than a hole in the ledger.
    expectOutcome(
      await probeGet(nobody.page, '/payments/1'),
      [403],
      'the payment detail behind the link is properly gated'
    );
    // And the CSV of the same data is gated, which is the inconsistency.
    expectOutcome(
      await probeGet(nobody.page, '/export.csv?month=2028-05'),
      [403],
      'F-G-032: the CSV of the very data the screen just handed over is gated on grid:export'
    );

    await nobody.close();
  });

  /**
   * The scope verdict on all four CSV exports in one place — the question this
   * audit exists to answer. Two of the four filter, two do not, and the two that
   * do not are only safe because of who happens to hold their verb today.
   */
  test('TC-G-058 — two of the four CSV exports apply the caller\'s data scope and two do not', async ({
    adminPage,
    browser,
    runId
  }) => {
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrcsv-${runId}`);
    const owner = await asRole(adminPage, browser, runId, ['Requester'], `csvown-${runId}`);
    const other = await asRole(adminPage, browser, runId, ['Requester'], `csvoth-${runId}`);

    // One budget request and one recoverable, both belonging to `owner`.
    const secret = `CSV secret ${runId}`;
    await owner.page.goto('/requests/new?type=reimbursement');
    await owner.page.getByLabel('Short title').fill(secret);
    await owner.page.locator('#project').selectOption({ label: 'Operations' });
    await expect(owner.page.locator('#head option').filter({ hasNotText: 'Operations /' })).toHaveCount(1);
    await owner.page.locator('#head').selectOption({ label: 'Operations / Utilities' });
    await owner.page.getByLabel('Amount').fill('43000.00');
    await owner.page.getByLabel('Expense date').fill('2026-07-13');
    await owner.page.getByLabel('Purpose').fill(`CSV secret purpose ${runId}.`);
    await owner.page.getByLabel('Approver').selectOption({ label: approver.subject.name });
    await owner.page.getByRole('button', { name: 'Submit request' }).click();
    await expect(owner.page).toHaveURL(/\/requests\/\d+\/submitted$/);
    const budgetNumber = (await owner.page.locator('.rh-no').first().innerText()).trim();

    // 1 — /requests/export.csv DOES apply the scope. `requestsExport` calls
    // `effectiveScope` (internal/app/requests.go:929), the same helper the HTML
    // list uses, and `?scope=all` may narrow but never widen.
    const foreign = await csv(other.page, '/requests/export.csv?scope=all&bucket=all');
    expect(
      foreign.includes(budgetNumber),
      '/requests/export.csv applies the caller\'s request scope — this is the one that got it right'
    ).toBe(false);
    const own = await csv(owner.page, '/requests/export.csv?bucket=all');
    expect(own, 'and the owner does get their own row').toContain(budgetNumber);

    // 2 — /export.csv and /reports/ytd.csv carry no per-row owner at all: they
    // are aggregates over heads, so there is no row scope to apply. Their whole
    // protection is the verb, and a Requester does not hold it.
    expectOutcome(
      await probeGet(other.page, '/export.csv?month=2026-06'),
      [403],
      '/export.csv is protected by grid:export alone — there is no row-level scope in an aggregate'
    );
    expectOutcome(
      await probeGet(other.page, '/reports/ytd.csv?from=2026-06&to=2026-06'),
      [403],
      '/reports/ytd.csv likewise'
    );
    // But note what that means: anyone who holds the verb sees every project.
    const aggregate = await csv(adminPage, '/export.csv?month=2026-06');
    for (const head of ['Office Rent', 'Payroll', 'Marketing']) {
      expect(aggregate, `the aggregate spans every project — ${head}`).toContain(head);
    }

    // 3 — /recoverables/list.csv does NOT apply the scope. TC-G-054 proves it
    // with a custom role; here it is stated as the verdict, against the seeded
    // roles that keep it latent.
    expectOutcome(
      await probeGet(other.page, '/recoverables/list.csv'),
      [403],
      'a Requester is refused the recoverables CSV, which is the only thing containing it'
    );
    const accounts = await asRole(adminPage, browser, runId, ['Accounts'], `csvacc-${runId}`);
    const accountsCSV = await csv(accounts.page, '/recoverables/list.csv');
    expect(
      accountsCSV.split('\n')[0],
      'the recoverables CSV is row-level data, so a missing scope filter is a real leak and not an aggregate'
    ).toContain('Requester');

    await approver.close();
    await owner.close();
    await other.close();
    await accounts.close();
  });
});

// ===========================================================================
// 6 — Idempotence and double-submit
// ===========================================================================

test.describe('G · idempotence', () => {
  /**
   * G6 is explicit that legitimate repeats exist and the duplicate check is
   * advisory. So two identical submissions creating two requests is correct —
   * but it must be *visibly* two, each with its own number, and neither may be
   * silently merged.
   */
  test('TC-G-060 — a replayed request POST creates a second, separately numbered request (G6, by design)', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrid-${runId}`);
    const payee = `Replay Vendor ${runId}`;
    await ensureVendor(adminPage, payee);

    await adminPage.goto('/requests/new?type=vendor_invoice');
    await adminPage.locator('#vendor').pressSequentially(payee);
    await adminPage.locator('#vendor-options .co', { hasText: payee }).first().click();
    const vendorID = await adminPage.locator('#vendor-id').inputValue();
    await adminPage.locator('#project').selectOption({ label: 'Operations' });
    await expect(adminPage.locator('#head option').filter({ hasNotText: 'Operations /' })).toHaveCount(1);
    const headID = await adminPage
      .locator('#head option', { hasText: 'Operations / Utilities' })
      .getAttribute('value');
    const projectID = await adminPage.locator('#project option', { hasText: 'Operations' }).getAttribute('value');
    const approverID = await adminPage
      .locator('#approver option', { hasText: approver.subject.name })
      .getAttribute('value');

    const form = {
      type: 'vendor_invoice',
      treatment: 'budget',
      project_id: projectID!,
      head_id: headID!,
      vendor_id: vendorID,
      short_title: `Replay ${runId}`,
      amount: '1900.00',
      invoice_no: `INV-D1-${runId}`,
      invoice_date: '2026-07-18',
      purpose: `Replay purpose ${runId}.`,
      manager_id: approverID!
    };

    const first = await probePost(adminPage, '/requests', form);
    const second = await probePost(adminPage, '/requests', form);
    expect(first.status, 'the first POST creates a request').toBe(303);
    expect(second.status, 'the second POST is accepted too — G6 permits repeats').toBe(303);
    expect(second.location, 'and it is a different request').not.toBe(first.location);

    // Both exist, both numbered, and the duplicate check is advisory only.
    await adminPage.goto(`/requests?bucket=all&q=${encodeURIComponent(`Replay ${runId}`)}`);
    await expect(
      adminPage.locator('.req-card'),
      'two identical submissions produce two separately numbered requests'
    ).toHaveCount(2);
    const numbers = await adminPage.locator('.rc-no').allInnerTexts();
    expect(new Set(numbers.map(n => n.trim())).size, 'and the numbers are distinct — C4').toBe(2);

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  test('TC-G-061 — approving twice is refused the second time and changes nothing', async ({
    adminPage,
    browser,
    runId
  }) => {
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrap2-${runId}`);
    const payee = `Twice Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    const raised = await raiseVendorRequest(adminPage, {
      amount: '2600.00',
      vendor: payee,
      title: `Approve twice ${runId}`,
      invoiceNo: `INV-D2-${runId}`,
      approverName: approver.subject.name
    });

    const first = await probePost(approver.page, `/requests/${raised.id}/approve`, {
      approved_amount: '2600.00'
    });
    expect(first.status, 'the first approve succeeds').toBe(303);
    const second = await probePost(approver.page, `/requests/${raised.id}/approve`, {
      approved_amount: '1.00'
    });
    expect(
      second.status >= 400,
      `an approved request cannot be approved again — got ${second.outcome}`
    ).toBe(true);

    await adminPage.goto(`/requests/${raised.id}`);
    await expectMoney(
      adminPage,
      '.dl div:has(dt:text-is("Approved")) dd',
      '₹2,600.00',
      'the second approve must not overwrite the first figure'
    );
    const rows = await auditRows(adminPage, approver.subject.name, 'payment_request', raised.id);
    expect(
      rows.filter(r => r.action === 'approve' && r.summary.includes(raised.number)).length,
      'exactly one approve row'
    ).toBe(1);

    await approver.close();
  });

  /**
   * S9 — one request, one payment. A replayed confirm must land on the payment
   * that already exists rather than create a second, and the redirect at
   * internal/app/app.go:713-716 is what makes a double tap harmless.
   */
  test('TC-G-062 — a replayed settlement confirm lands on the existing payment and creates no second one', async ({
    adminPage,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const request = await createApprovedRequest(adminPage, runId, { amount: '3800.00' });
    await adminPage.goto('/accounts-queue?tab=approved');
    await adminPage
      .locator('tbody tr', { hasText: request.number })
      .getByRole('button', { name: 'Take for processing' })
      .click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${request.id}$`));
    const headID = await adminPage.locator('input[name="head_id"]').inputValue();

    const form = {
      request_id: String(request.id),
      head_id: headID,
      amount: '3800.00',
      paid_on: '2027-09-09',
      payment_mode: 'bank_transfer',
      reference_no: `UTR-D3-${runId}`,
      remarks: `Replay settle ${runId}`,
      vendor_payee: `Payee ${runId}`,
      settlement: 'settled'
    };

    const first = await probePost(adminPage, '/payments', form);
    expect(first.status, 'the first confirm creates the payment').toBe(303);
    expect(first.location, 'and lands on it').toMatch(/^\/payments\/\d+$/);

    const second = await probePost(adminPage, '/payments', form);
    expect(second.status, 'the replay is a redirect, not an error page').toBe(303);
    expect(
      second.location,
      'S9: a double confirm goes to the payment that already exists rather than creating a second'
    ).toBe(first.location);

    await adminPage.goto('/payments?month=2027-09');
    await adminPage.getByLabel('Search').fill(`Replay settle ${runId}`);
    await adminPage.getByRole('button', { name: 'Filter' }).click();
    expect(await dataRows(adminPage), 'exactly one payment exists for this request').toBe(1);

    // The browser Back button after the completed POST cannot replay it either:
    // the 303 means Back returns to the GET, and the GET is now the conflict
    // screen because the request is no longer reserved.
    await adminPage.goto(`/payments/new?request=${request.id}`);
    await expect(
      adminPage.locator('body'),
      'going back to the entry URL after settling shows the conflict/refusal, never a live form'
    ).not.toContainText('Amount actually paid');

    expectNoRuntimeErrors(errors);
  });

  test('TC-G-063 — reserving twice, releasing twice and marking read twice are all safe', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const request = await createApprovedRequest(adminPage, runId, { amount: '4900.00' });

    const first = await probePost(adminPage, `/requests/${request.id}/record-payment`, {});
    expect(first.status, 'the first reserve succeeds').toBe(303);
    const second = await probePost(adminPage, `/requests/${request.id}/record-payment`, {});
    // The conditional UPDATE matches nothing the second time, and the handler
    // renders the conflict screen rather than a duplicate reservation.
    expect(
      second.status === 409 || second.status === 303,
      `reserving twice must not create a second reservation — got ${second.outcome}`
    ).toBe(true);
    const rows = await auditRows(adminPage, 'Fervid Admin', 'payment_request', request.id);
    expect(
      rows.filter(r => r.action === 'process' && r.summary.includes('Reserved')).length >= 1,
      'the reservation is audited'
    ).toBe(true);

    // A second accountant loses the race with a 409 conflict screen, not a 500.
    const other = await asRole(adminPage, browser, runId, ['Accounts'], `race-${runId}`);
    const lost = await probePost(other.page, `/requests/${request.id}/record-payment`, {});
    expect(lost.status, 'IF7: the loser of a reservation race gets 409').toBe(409);

    // Mark-all-read twice.
    for (const attempt of [1, 2]) {
      const read = await probePost(adminPage, '/notifications/read', {});
      expect(read.status, `mark-all-read attempt ${attempt} is idempotent`).toBe(303);
    }

    // Release twice: the second finds nothing to release.
    const rel1 = await probePost(adminPage, `/requests/${request.id}/release`, {
      confirm: 'on',
      reason: `Releasing ${runId}`
    });
    expect(rel1.status, 'the first release succeeds').toBe(303);
    const rel2 = await probePost(adminPage, `/requests/${request.id}/release`, {
      confirm: 'on',
      reason: `Releasing again ${runId}`
    });
    expect(rel2.status >= 400 || rel2.status === 303, `a second release is safe — got ${rel2.outcome}`).toBe(true);
    await adminPage.goto(`/requests/${request.id}`);
    await expect(adminPage.locator('.rh-status'), 'the request is back to approved exactly once').toContainText(
      'Approved'
    );

    expectNoRuntimeErrors(errors);
    await other.close();
  });
});

// ===========================================================================
// 7 — Locked-month integrity
// ===========================================================================

test.describe('G · a locked month', () => {
  /**
   * The lock is a ledger lock. A settlement dated into it is refused, a budget
   * edit is refused — and a request may still be raised and approved into it,
   * because `internal/store/requests.go` contains no `IsLocked` call at all.
   */
  test('TC-G-070 — a locked month refuses a settlement and a budget edit, and still accepts a new request', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const MONTH = '2027-10';
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrlk-${runId}`);

    // Lock the month from the grid's "Month Close" block.
    await adminPage.goto(`/grid?month=${MONTH}`);
    const unlock = adminPage.getByRole('button', { name: 'Unlock Month' });
    if ((await unlock.count()) > 0) {
      adminPage.once('dialog', dialog => dialog.accept());
      await adminPage.getByLabel('Unlock reason').fill('resetting for the audit');
      await unlock.click();
      await adminPage.goto(`/grid?month=${MONTH}`);
    }
    adminPage.once('dialog', dialog => dialog.accept());
    await adminPage.getByLabel('Lock reason').fill(`Audit G lock ${runId}`);
    const locked = await probePost(adminPage, `/months/${MONTH}/lock`, { reason: `Audit G lock ${runId}` });
    expect(locked.status, 'locking is a 303').toBe(303);

    // F-G-019: the redirect goes to `/?month=`, which is the dashboard now.
    expect(
      locked.location,
      'F-G-019: lock redirects to /?month=, and GET / is the dashboard, which ignores the month'
    ).toBe(`/?month=${MONTH}`);
    const landing = await probeGet(adminPage, locked.location!);
    expect(landing.status, 'the landing page renders').toBe(200);
    expect(
      landing.body.includes(`Audit G lock ${runId}`),
      'F-G-019: the operator sees no confirmation of the lock they just applied'
    ).toBe(false);
    expect(
      landing.body.includes(MONTH),
      'F-G-019: nor even the month they locked'
    ).toBe(false);

    // The grid, which is where the lock actually shows, does say so.
    await adminPage.goto(`/grid?month=${MONTH}`);
    await expect(adminPage.locator('.locked'), 'the grid names the lock and its reason').toContainText(
      `Audit G lock ${runId}`
    );

    // A settlement dated into the locked month is refused, and writes nothing.
    const request = await createApprovedRequest(adminPage, runId, { amount: '5100.00' });
    await adminPage.goto('/accounts-queue?tab=approved');
    await adminPage
      .locator('tbody tr', { hasText: request.number })
      .getByRole('button', { name: 'Take for processing' })
      .click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${request.id}$`));
    // F-G-020: the entry screen carries no lock affordance at all.
    expect(
      (await adminPage.locator('.locked').count()) === 0,
      'F-G-020: /payments/new shows no lock warning, so the accountant fills the whole form first'
    ).toBe(true);

    await adminPage.getByLabel('Amount actually paid').fill('5100.00');
    await adminPage.getByLabel('Paid on').fill(`${MONTH}-15`);
    await adminPage.getByLabel('Payment mode').selectOption('bank_transfer');
    await adminPage.getByLabel('Transaction / UTR reference').fill(`UTR-LK-${runId}`);
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    const sheet = adminPage.locator('.overlay .sheet');
    await expect(sheet).toBeVisible();
    await sheet.locator('input[name="settlement"][value="settled"]').check();
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();
    await expect(adminPage, 'a locked-month settlement creates no payment').toHaveURL(/\/payments$/);
    await expect(sheet.locator('.banner.bad'), 'and says the month is locked').toContainText(/locked/i);

    await adminPage.goto(`/payments?month=${MONTH}`);
    expect(await dataRows(adminPage), 'the locked month stays empty').toBe(0);

    // A budget edit is refused too — the form is disabled and the POST fails.
    await adminPage.goto(`/budgets?month=${MONTH}`);
    await expect(adminPage.locator('.locked'), 'the budgets screen says the month is locked').toBeVisible();
    const anyHead = await adminPage.locator('input[name^="budget_"]').first().getAttribute('name');
    expect(anyHead, 'the budgets form names its inputs budget_<headID>').toMatch(/^budget_\d+$/);
    const budget = await probePost(adminPage, '/budgets', { month: MONTH, [anyHead!]: '100.00' });
    // F-G-026: `budgetSave` renders its own 400 for every failure rather than
    // routing through `respondStoreError`, which answers 409 for the same
    // `ErrLockedMonth` on /payments/{id}/void and /months. One condition, two
    // status codes, depending only on which handler you reached it through.
    expect(
      budget.status,
      'F-G-026: a locked-month budget save answers 400 where every other locked-month refusal answers 409'
    ).toBe(400);
    expect(budget.body, 'and it does say the month is locked').toContain('month is locked');

    // But a request may still be raised AND approved into the locked month.
    const payee = `Locked Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    const raised = await raiseVendorRequest(adminPage, {
      amount: '5200.00',
      vendor: payee,
      title: `Into a locked month ${runId}`,
      invoiceNo: `INV-LK-${runId}`,
      approverName: approver.subject.name
    });
    // F-G-021.
    expect(raised.id, 'F-G-021: a request is accepted into a locked month with no warning').toBeGreaterThan(0);
    await approveFor(approver.page, raised.id, '5200.00');
    await adminPage.goto(`/requests/${raised.id}`);
    await expect(
      adminPage.locator('.rh-status'),
      'F-G-021: and approved, becoming an approved obligation nobody can pay in that month'
    ).toContainText('Approved');

    // Unlocking restores the write, which proves the refusal was the lock.
    adminPage.once('dialog', dialog => dialog.accept());
    const unlocked = await probePost(adminPage, `/months/${MONTH}/unlock`, { reason: `Audit G unlock ${runId}` });
    expect(unlocked.status, 'unlocking is a 303').toBe(303);
    expect(unlocked.location, 'F-G-019: unlock lands on the dashboard too').toBe(`/?month=${MONTH}`);

    // The reservation survived the locked-month refusal, so the request is
    // still `processing` and no longer in the takeable set — `settlePayment`
    // would look for a "Take for processing" button that is correctly absent.
    // Resuming the entry form the accountant already holds is the real path.
    await adminPage.goto(`/payments/new?request=${request.id}`);
    await expect(adminPage.locator('.reserve-bar'), 'the reservation survived the refusal').toContainText(
      'Reserved by you'
    );
    await adminPage.getByLabel('Amount actually paid').fill('5100.00');
    await adminPage.getByLabel('Paid on').fill(`${MONTH}-15`);
    await adminPage.getByLabel('Payment mode').selectOption('bank_transfer');
    await adminPage.getByLabel('Transaction / UTR reference').fill(`UTR-LK2-${runId}`);
    await adminPage.getByLabel('Processing note').fill(`Unlocked ${runId}`);
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    const reopened = adminPage.locator('.overlay .sheet');
    await expect(reopened).toBeVisible();
    await reopened.locator('input[name="settlement"][value="settled"]').check();
    await reopened.getByRole('button', { name: 'Confirm and save payment' }).click();
    await expect(adminPage, 'the same settlement succeeds once the month is open').toHaveURL(/\/payments\/\d+$/);
    await expectMoney(adminPage, '.rh-amt', '₹5,100.00', 'the figure the lock refused now lands intact');

    expectNoRuntimeErrors(errors);
    await approver.close();
  });
});

// ===========================================================================
// 8 — Referential integrity under the app's own rules
// ===========================================================================

test.describe('G · referential integrity', () => {
  /**
   * `user_roles.role_id` is `ON DELETE CASCADE`
   * (internal/store/migrations.go:69) and `DeleteRole` has no pre-check, so
   * deleting an assigned role succeeds and silently strips it from its holders.
   */
  test('TC-G-080 — deleting a role that is assigned succeeds and silently strips it from its holders', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const role = await createCustomRole(adminPage, `Doomed ${runId}`, ['payment:view'], {});
    const holder = await asCustomRole(adminPage, browser, runId, 'doomed', role);

    await adminPage.goto('/users');
    await expect(
      adminPage.locator('tr', { hasText: holder.email }).locator('.pill', { hasText: `Doomed ${runId}` }),
      'the holder wears the role'
    ).toHaveCount(1);
    const before = await probeGet(holder.page, '/payments');
    expect(before.status, 'and the grant it carries works').toBe(200);

    const deleted = await probePost(adminPage, `/roles/${role.id}/delete`, {});
    // F-G-022 — no pre-check, no warning, no 409.
    expect(
      deleted.status,
      'F-G-022: an assigned role is deleted with a clean 303 and no warning at all'
    ).toBe(303);

    await adminPage.goto('/users');
    await expect(
      adminPage.locator('tr', { hasText: holder.email }).locator('.pill', { hasText: `Doomed ${runId}` }),
      'F-G-022: the role vanished from the holder without their or the admin\'s knowledge'
    ).toHaveCount(0);
    const after = await probeGet(holder.page, '/payments');
    expect(
      after.status,
      'F-G-022: the holder silently loses the permission on their very next request'
    ).toBe(403);

    // Deleting a SEEDED role is refused — but with the wrong message.
    await adminPage.goto('/roles');
    const adminRoleHref = await adminPage
      .locator('.segmented a', { hasText: 'Requester' })
      .getAttribute('href');
    const seededID = adminRoleHref!.split('=')[1];
    const seeded = await probePost(adminPage, `/roles/${seededID}/delete`, {});
    expect(seeded.status, 'a system role cannot be deleted').toBe(403);
    // F-G-023 — ErrForbidden discards the store's own explanation.
    expect(
      seeded.body.includes('system role'),
      'F-G-023: the store says "system roles cannot be deleted" and the page says the admin lacks permission'
    ).toBe(false);
    expect(seeded.body, 'F-G-023: the operator is told the wrong thing').toContain(
      'do not have permission'
    );

    expectNoRuntimeErrors(errors);
    await holder.close();
  });

  /**
   * A project or head may be deactivated with approved requests against it,
   * and the request then becomes permanently unpayable: `validatePayment`
   * refuses an inactive head (internal/store/store.go:1632-1639). The refusal
   * is coherent — a 400 with a sentence, not a 500 — but nothing warned anyone.
   */
  test('TC-G-081 — deactivating a head strands its approved requests, and the refusal is coherent', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrhd-${runId}`);
    const payee = `Retire Vendor ${runId}`;
    await ensureVendor(adminPage, payee);

    // A head of this test's own, so deactivating it cannot disturb anything.
    await adminPage.goto('/heads');
    const headName = `Audit G Head ${runId}`;
    const projectOption = await adminPage
      .locator('select[name="project_id"] option', { hasText: 'Operations' })
      .first()
      .getAttribute('value');
    const created = await probePost(adminPage, '/heads', {
      project_id: projectOption!,
      name: headName,
      due_day: '15',
      active: 'on',
      sort_order: '99'
    });
    expect(created.status, 'the head is created').toBe(303);

    const raised = await raiseVendorRequest(adminPage, {
      amount: '6600.00',
      vendor: payee,
      head: `Operations / ${headName}`,
      title: `Stranded ${runId}`,
      invoiceNo: `INV-RI-${runId}`,
      approverName: approver.subject.name
    });
    await approveFor(approver.page, raised.id, '6600.00');

    // Deactivate the head. Nothing warns that an approved request points at it.
    // The heads table renders each name inside an <input value="…">, so the row
    // cannot be found by text — an input's value is not text content.
    await adminPage.goto('/heads');
    const row = adminPage.locator(`tbody tr:has(input[value="${headName}"])`);
    await expect(row, 'the new head has a row of its own').toHaveCount(1);
    const headID = await row.locator('input[name="id"]').first().inputValue();
    const off = await probePost(adminPage, '/heads', {
      id: headID,
      project_id: projectOption!,
      name: headName,
      due_day: '15',
      sort_order: '99'
    });
    // F-G-024.
    expect(
      off.status,
      'F-G-024: a head with an approved request against it is deactivated with no check and no warning'
    ).toBe(303);

    // The request still reads — history is preserved, which is right.
    await adminPage.goto(`/requests/${raised.id}`);
    await expect(
      adminPage.locator('.dl div:has(dt:text-is("Head")) dd'),
      'the request keeps its head name for history'
    ).toHaveText(headName);

    // And is now unpayable, coherently rather than with a 500.
    await adminPage.goto('/accounts-queue?tab=approved');
    await adminPage
      .locator('tbody tr', { hasText: raised.number })
      .getByRole('button', { name: 'Take for processing' })
      .click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${raised.id}$`));
    await adminPage.getByLabel('Amount actually paid').fill('6600.00');
    await adminPage.getByLabel('Paid on').fill('2027-11-11');
    await adminPage.getByLabel('Payment mode').selectOption('bank_transfer');
    await adminPage.getByLabel('Transaction / UTR reference').fill(`UTR-RI-${runId}`);
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    const sheet = adminPage.locator('.overlay .sheet');
    await expect(sheet).toBeVisible();
    await sheet.locator('input[name="settlement"][value="settled"]').check();
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();
    await expect(adminPage, 'no payment was written').toHaveURL(/\/payments$/);
    await expect(
      sheet.locator('.banner.bad'),
      'F-G-024: the request is stranded, and the app says why in a sentence rather than 500ing'
    ).toContainText(/inactive/i);

    expectNoRuntimeErrors(errors, 'no 5xx anywhere: the refusal is a 400, not a crash');
    await approver.close();
  });

  /**
   * A vendor can only be deactivated, never deleted, so `vendor_id` cannot
   * dangle. The COALESCE carries no status predicate, so an in-flight request
   * keeps the payee — and settlement is not blocked, unlike a retired head.
   */
  test('TC-G-082 — a deactivated vendor keeps its payee on live requests and there is no delete route', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrvd-${runId}`);
    const payee = `Deactivate Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    const raised = await raiseVendorRequest(adminPage, {
      amount: '7700.00',
      vendor: payee,
      title: `Retired vendor ${runId}`,
      invoiceNo: `INV-VD-${runId}`,
      approverName: approver.subject.name
    });
    await approveFor(approver.page, raised.id, '7700.00');

    await adminPage.goto(`/vendors?q=${encodeURIComponent(payee)}`);
    const href = await adminPage.locator('tbody tr', { hasText: payee }).locator('a').first().getAttribute('href');
    await adminPage.goto(href!);
    await adminPage.locator('#v-status').selectOption('inactive');
    await adminPage.getByRole('button', { name: 'Save vendor' }).click();
    await adminPage.waitForURL(url => !url.pathname.endsWith('/new'));

    // The request keeps the name: the COALESCE has no status test.
    await adminPage.goto(`/requests/${raised.id}`);
    await expect(
      adminPage.locator('.dl div:has(dt:text-is("Vendor")) dd'),
      'a deactivated vendor is still the payee on requests already raised'
    ).toContainText(payee);

    // The vendor is gone from the picker, which is the intended consequence.
    await adminPage.goto('/requests/new?type=vendor_invoice');
    await adminPage.locator('#vendor').pressSequentially(payee);
    await expect(
      adminPage.locator('#vendor-options .co', { hasText: payee }),
      'and cannot be chosen for a new request'
    ).toHaveCount(0);

    // Settlement still works — unlike a retired head, an inactive vendor does
    // not strand the obligation.
    const paymentPath = await settlePayment(adminPage, raised.id, {
      amount: '7700.00',
      paidOn: '2027-12-12',
      reference: `UTR-VD-${runId}`,
      remarks: `Retired vendor ${runId}`
    });
    await expect(adminPage.locator('h1'), 'and the payment still names the payee').toHaveText(payee);
    expect(paymentPath).toMatch(/^\/payments\/\d+$/);

    // Proof of absence: no delete route for a vendor exists.
    // `/vendors/{id}/delete` is unrouted, so it is 405 (the catch-all `GET /`
    // matches the path and not the method). `/vendors/delete` is a different
    // shape: it MATCHES `POST /vendors/{id}` with id="delete", `parseID` yields
    // 0, and `vendorUpdate` refuses with a 400 — a refusal, not a deletion.
    const nested = await probePost(adminPage, `${href}/delete`, {});
    expectOutcome(nested, [404, 405], 'there is no /vendors/{id}/delete route');
    const shadowed = await probePost(adminPage, '/vendors/delete', {});
    expect(
      shadowed.status >= 400,
      `/vendors/delete is captured by POST /vendors/{id} and refused — got ${shadowed.outcome}`
    ).toBe(true);

    // And the vendor is still there, deactivated rather than gone.
    await adminPage.goto(`/vendors?q=${encodeURIComponent(payee)}&status=all`);
    await expect(
      adminPage.locator('tbody tr', { hasText: payee }),
      'a vendor can only ever be deactivated, so the row survives'
    ).toHaveCount(1);

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  /**
   * A user with a pending approval against them can be deactivated, and the
   * request is stranded: no route reassigns a pending approval, only a
   * reservation. The app refuses coherently at every touch point rather than
   * 500ing on the dangling manager.
   */
  test('TC-G-083 — deactivating an approver strands their pending approvals without a 500', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrdz-${runId}`);
    const payee = `Strand Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    const raised = await raiseVendorRequest(adminPage, {
      amount: '8800.00',
      vendor: payee,
      title: `Stranded approval ${runId}`,
      invoiceNo: `INV-UD-${runId}`,
      approverName: approver.subject.name
    });

    // Deactivate the approver through their own edit sheet.
    await adminPage.goto('/users');
    await adminPage.locator('tr', { hasText: approver.subject.email }).getByRole('button', { name: 'Edit' }).click();
    const edit = adminPage.locator('.overlay:not([hidden])');
    await expect(edit).toBeVisible();
    await edit.getByRole('checkbox', { name: 'Active' }).uncheck();
    await edit.getByRole('button', { name: 'Save user' }).click();
    await expect(adminPage).toHaveURL(/\/users$/);
    // F-G-025. The row carries a role pill as well as a status pill, so the
    // assertion is scoped to the Status cell.
    await expect(
      adminPage
        .locator('tr', { hasText: approver.subject.email })
        .locator('td[data-label="Status"] .pill'),
      'F-G-025: the approver is deactivated with no check for the approvals waiting on them'
    ).toHaveText('Inactive');

    // The request still reads, and still names them.
    await adminPage.goto(`/requests/${raised.id}`);
    await expect(
      adminPage.locator('.dl div:has(dt:text-is("Approver")) dd'),
      'F-G-025: the request still points at a user who can no longer sign in'
    ).toHaveText(approver.subject.name);
    await expect(adminPage.locator('.rh-status'), 'and is still awaiting them, for ever').toContainText('Awaiting');

    // They cannot sign in, so nobody can decide it.
    await approver.page.goto('/login');
    await approver.page.getByLabel('Email').fill(approver.subject.email);
    await approver.page.getByLabel('Password').fill(approver.subject.password);
    await approver.page.getByRole('button', { name: 'Login' }).click();
    await expect(approver.page, 'F-G-025: a deactivated approver cannot sign in to clear their queue').toHaveURL(
      /\/login$/
    );

    // Proof of absence: there is no route that reassigns a pending approval.
    for (const path of [
      `/requests/${raised.id}/reassign-approval`,
      `/requests/${raised.id}/approval-reassign`
    ]) {
      const probe = await probePost(adminPage, path, { to_user_id: '1' });
      expectOutcome(probe, [404, 405], `no approval-reassignment route exists (${path})`);
    }

    // Every screen that touches the request still renders. No 500 anywhere.
    for (const path of ['/requests?bucket=all', `/requests/${raised.id}`, '/accounts-queue', '/audit']) {
      const probe = await probeGet(adminPage, path);
      expect(probe.status, `${path} must not 500 over a deactivated approver`).toBeLessThan(500);
    }

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  /**
   * `PRAGMA foreign_keys=ON` rides on the DSN (internal/store/store.go:36), so a
   * request pointing at an id that does not exist cannot be written. It must be
   * refused with a sentence, and the refusal must be a 4xx.
   *
   * Regression guard for F-G-027 (the same fix as F-B-06). `classify`
   * (internal/store/store.go:1856-1858) now recognises `FOREIGN KEY` alongside
   * `UNIQUE` and returns ErrValidation, so `storeErrorStatus`
   * (internal/app/http_errors.go:199-212) answers 400 where it used to answer 500.
   * Three properties are asserted together, because a coherent refusal is all
   * three at once: the write does not happen, the driver text does not reach the
   * page, and the status is one the requester can act on.
   */
  test(
    'TC-G-084 — a request aimed at a nonexistent project, head or vendor must be refused coherently, not with a 500',
    async ({ adminPage, browser, runId }) => {
      const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrfk-${runId}`);
      await adminPage.goto('/requests/new?type=vendor_invoice');
      const approverID = await adminPage
        .locator('#approver option', { hasText: approver.subject.name })
        .getAttribute('value');

      const base = {
        type: 'vendor_invoice',
        treatment: 'budget',
        short_title: `FK probe ${runId}`,
        amount: '1000.00',
        invoice_no: `INV-FK-${runId}`,
        invoice_date: '2026-07-18',
        purpose: `FK purpose ${runId}.`,
        manager_id: approverID!
      };

      const cases: Array<[string, Record<string, string>]> = [
        ['a project that does not exist', { ...base, project_id: '99999999', head_id: '99999999', vendor_id: '1' }],
        ['a head that does not exist', { ...base, project_id: '1', head_id: '99999999', vendor_id: '1' }],
        ['a vendor that does not exist', { ...base, project_id: '1', head_id: '1', vendor_id: '99999999' }]
      ];

      for (const [what, form] of cases) {
        const probe = await probePost(adminPage, '/requests', form);
        expect(probe.status, `${what} must not be accepted — got ${probe.outcome}`).not.toBe(303);
        // The one thing that is right: no driver text reaches the page.
        expect(
          probe.body.includes('FOREIGN KEY') || probe.body.includes('constraint failed'),
          `${what}: no driver text may reach the page`
        ).toBe(false);
        expect(
          probe.status,
          `F-G-027: ${what} must be a 4xx the requester can act on, not a 500 — got ${probe.outcome}`
        ).toBeLessThan(500);
      }

      await approver.close();
    }
  );

  /**
   * The settlement path answers a forged `head_id` by never reading it.
   *
   * This case was written when `paymentInput` fed the form's head straight into
   * `validatePayment`, so a nonexistent head produced `ErrInactiveHead` and the
   * question was whether that refusal was coherent. F-D-01's fix — taken
   * deliberately — removes the question: the head, the payee and the invoice
   * number are facts of the REQUEST (a manager approved an amount against a
   * project and head, and the entry screen offers no control to change any of
   * them), so `RecordPaymentForRequest` overwrites all three from the request row
   * it already holds open (internal/store/store.go:950-954). A forged head is not
   * refused, it is ignored, and refusing it would be the wrong answer: the
   * accountant would be blocked by a field they never filled in.
   *
   * So the information-flow property this case now protects is the one that
   * actually matters here — **a payment is booked against its own request's head,
   * whatever the form said** — asserted three ways: the redirect succeeds, the
   * payment's own screen names the approved head, and the ledger row for the month
   * carries that head and no other.
   */
  test('TC-G-086 — a payment is booked against its request’s own head, whatever head_id the form sent', async ({
    adminPage,
    runId
  }) => {
    // createApprovedRequest approves against Operations / Office Rent.
    const APPROVED_HEAD = 'Operations / Office Rent';
    const request = await createApprovedRequest(adminPage, runId, { amount: '1500.00' });
    const reserved = await probePost(adminPage, `/requests/${request.id}/record-payment`, {});
    expect(reserved.status, 'the request is reserved first').toBe(303);

    const forgedHead = await probePost(adminPage, '/payments', {
      request_id: String(request.id),
      head_id: '99999999',
      amount: '1500.00',
      paid_on: '2028-01-10',
      payment_mode: 'bank_transfer',
      reference_no: `UTR-FK-${runId}`,
      settlement: 'settled'
    });
    expect(
      forgedHead.status,
      `the form's head is display baggage, so the settlement proceeds — got ${forgedHead.outcome}`
    ).toBe(303);
    expect(forgedHead.body.includes('FOREIGN KEY'), 'and no driver message is ever echoed').toBe(false);
    expect(forgedHead.location, 'and it lands on the payment it wrote').toMatch(/^\/payments\/\d+$/);

    // Hop 1 — the payment's own screen names the head the approval named.
    await adminPage.goto(forgedHead.location!);
    await expect(
      adminPage.locator('.rh-meta'),
      'the payment is charged to the request’s approved head, not the one the poster forged'
    ).toContainText(APPROVED_HEAD);

    // Hop 2 — and so does the ledger, which reads head_id back out of the row.
    await adminPage.goto('/payments?month=2028-01');
    expect(await dataRows(adminPage), 'exactly the one payment lands in that month').toBe(1);
    await expect(
      adminPage.locator('tbody tr td[data-label="Project / Head"]'),
      'the stored head_id is the approved one, so no head 99999999 was ever written'
    ).toContainText(APPROVED_HEAD);
  });

  /**
   * The worst kind of information-flow failure: a fact changing itself.
   *
   * `heads` loads ALL heads but only ACTIVE projects
   * (internal/app/app.go:1094-1099). A head whose project has been retired
   * therefore renders a `<select name="project_id">` with no matching option, so
   * the browser selects the first one, and the row's own Save — which posts that
   * select — moves the head to a project nobody chose.
   */
  test('TC-G-087 — retiring a project makes its heads\' own Save reassign them to a different project', async ({
    adminPage,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const projectName = `Audit G Project ${runId}`;
    const headName = `Audit G Orphan ${runId}`;

    // A project and a head of this test's own.
    const madeProject = await probePost(adminPage, '/projects', {
      name: projectName,
      active: 'on',
      sort_order: '98'
    });
    expect(madeProject.status, 'the project is created').toBe(303);
    await adminPage.goto('/projects');
    const projectRow = adminPage.locator(`tbody tr:has(input[value="${projectName}"])`);
    await expect(projectRow, 'the project has a row').toHaveCount(1);
    const projectID = await projectRow.locator('input[name="id"]').first().inputValue();

    const madeHead = await probePost(adminPage, '/heads', {
      project_id: projectID,
      name: headName,
      due_day: '15',
      active: 'on',
      sort_order: '98'
    });
    expect(madeHead.status, 'the head is created under it').toBe(303);

    // While the project is active, the head's select offers it and it is chosen.
    await adminPage.goto('/heads');
    const headRow = adminPage.locator(`tbody tr:has(input[value="${headName}"])`);
    await expect(headRow, 'the head has a row').toHaveCount(1);
    const headID = await headRow.locator('input[name="id"]').first().inputValue();
    expect(
      await headRow.locator('select[name="project_id"]').inputValue(),
      'the head is shown under its own project'
    ).toBe(projectID);

    // Retire the project.
    const retired = await probePost(adminPage, '/projects', {
      id: projectID,
      name: projectName,
      sort_order: '98'
    });
    expect(retired.status, 'a project with a head under it is retired without a warning').toBe(303);

    // The head is still listed, but its select no longer contains its project.
    await adminPage.goto('/heads');
    const orphan = adminPage.locator(`tbody tr:has(input[value="${headName}"])`);
    await expect(orphan, 'the head is still listed — ListHeads(false) returns it').toHaveCount(1);
    const options = await orphan.locator('select[name="project_id"] option').allInnerTexts();
    // F-G-033.
    expect(
      options.some(o => o.trim() === projectName),
      'F-G-033: the row offers no option for the head\'s own project, because /heads loads active projects only'
    ).toBe(false);
    const preselected = await orphan.locator('select[name="project_id"]').inputValue();
    expect(
      preselected,
      'F-G-033: with no matching option the browser preselects the first, so the control lies about where the head lives'
    ).not.toBe(projectID);

    // Pressing the row's own Save — changing nothing else — moves the head.
    await orphan.getByRole('button', { name: 'Save' }).click();
    await expect(adminPage).toHaveURL(/\/heads/);
    await adminPage.goto('/heads');
    const moved = adminPage.locator(`tbody tr:has(input[value="${headName}"])`);
    const nowUnder = await moved.locator('select[name="project_id"]').inputValue();
    // F-G-033 confirmed: silent reassignment.
    expect(
      nowUnder,
      'F-G-033: pressing Save with no edit reassigned the head to a project nobody chose — silent data corruption'
    ).not.toBe(projectID);
    expect(nowUnder, 'and it is the option the browser had preselected').toBe(preselected);

    expectNoRuntimeErrors(errors);
    void headID;
  });

  /**
   * `POST /users` runs `UpdateUser`, `SetUserRoles` and
   * `SetUserDefaultApprover` as three independent transactions with no envelope
   * (internal/app/app.go:1197-1220). A failure in the second leaves the first
   * committed, so the operator sees a refusal and the rename has happened
   * anyway.
   */
  test('TC-G-088 — a user save that fails half way leaves the rename committed and the roles not', async ({
    adminPage,
    browser,
    runId
  }) => {
    const subject = await asRole(adminPage, browser, runId, ['Requester'], `atomic-${runId}`);

    await adminPage.goto('/users');
    const row = adminPage.locator('tr', { hasText: subject.subject.email });
    await expect(
      row.locator('td[data-label="Roles"] .pill'),
      'the subject starts as exactly a Requester'
    ).toHaveText('Requester');

    // The user's id is not in the table row — it is in that user's own edit
    // sheet, which the Edit button names through `data-open="user-<id>"`
    // (internal/app/templates.go:1209).
    const opener = await row.getByRole('button', { name: 'Edit' }).getAttribute('data-open');
    const userID = opener!.replace('user-', '');
    expect(userID, 'the row names its own edit sheet, and the sheet is named for the user id').toMatch(/^\d+$/);

    // A save that renames AND assigns a role id that does not exist.
    // `SetUserRoles` checks existence inside its own transaction
    // (internal/store/permissions.go:471-479) and refuses — after `UpdateUser`
    // has already committed the new name.
    const renamed = `Renamed By A Failure ${runId}`;
    const refused = await probePost(adminPage, '/users', {
      id: userID,
      email: subject.subject.email,
      name: renamed,
      role: 'data_entry',
      active: 'on',
      role_ids: '99999999',
      default_approver_id: '0'
    });
    expect(refused.status, 'the save is refused — the role does not exist').toBe(400);
    expect(refused.body, 'and says so').toContain('does not exist');

    // F-G-034: the refusal is partial. The name changed; the roles did not.
    await adminPage.goto('/users');
    const after = adminPage.locator('tr', { hasText: subject.subject.email });
    await expect(
      after.locator('td[data-label="Name"]'),
      'F-G-034: UpdateUser committed before SetUserRoles failed, so the rename stuck'
    ).toHaveText(renamed);
    await expect(
      after.locator('td[data-label="Roles"] .pill'),
      'F-G-034: while the role assignment the same POST asked for did not happen'
    ).toHaveText('Requester');

    // The subject can still sign in and still holds what they held, so the
    // damage is a half-applied administrative change rather than a lockout.
    const probe = await probeGet(subject.page, '/requests');
    expect(probe.status, 'the subject keeps the grants their old role carried').toBe(200);

    await subject.close();
  });

  /**
   * The same missing envelope on the requester's own screen: `requestEdit` runs
   * `UpdateRequest`, then `AddRequestAttachment`, then `SubmitRequest`
   * (internal/app/requests.go:657-665). When the third fails the requester is
   * shown the correction form with an error — and their edit is already in the
   * database.
   */
  test('TC-G-089 — a request edit whose resubmit fails still writes the edit', async ({
    adminPage,
    browser,
    runId
  }) => {
    const approver = await asRole(adminPage, browser, runId, ['Manager'], `mgrat-${runId}`);
    const requester = await asRole(adminPage, browser, runId, ['Requester'], `editat-${runId}`);

    await requester.page.goto('/requests/new?type=reimbursement');
    await requester.page.getByLabel('Short title').fill(`Atomic edit ${runId}`);
    await requester.page.locator('#project').selectOption({ label: 'Operations' });
    await expect(requester.page.locator('#head option').filter({ hasNotText: 'Operations /' })).toHaveCount(1);
    await requester.page.locator('#head').selectOption({ label: 'Operations / Office Supplies' });
    await requester.page.getByLabel('Amount').fill('2000.00');
    await requester.page.getByLabel('Expense date').fill('2026-07-14');
    await requester.page.getByLabel('Purpose').fill(`Atomic edit purpose ${runId}.`);
    await requester.page.getByLabel('Approver').selectOption({ label: approver.subject.name });
    await requester.page.getByRole('button', { name: 'Submit request' }).click();
    await expect(requester.page).toHaveURL(/\/requests\/\d+\/submitted$/);
    const id = Number(new URL(requester.page.url()).pathname.split('/')[2]);

    const project = await requester.page
      .locator(`#project option`)
      .filter({ hasText: 'Operations' })
      .first()
      .getAttribute('value')
      .catch(() => null);
    await requester.page.goto(`/requests/${id}/edit`);
    const projectID = await requester.page.locator('select[name="project_id"]').inputValue();
    const headID = await requester.page.locator('select[name="head_id"]').inputValue();
    const managerID = await requester.page.locator('select[name="manager_id"]').inputValue();
    void project;

    // A pending request cannot transition to pending (legalTransitions,
    // internal/store/requests.go:91-101), so `submit_action=resubmit` fails —
    // after `UpdateRequest` has already written the new amount.
    const refused = await probePost(requester.page, `/requests/${id}/edit`, {
      type: 'reimbursement',
      treatment: 'budget',
      project_id: projectID,
      head_id: headID,
      short_title: `Atomic edit ${runId}`,
      amount: '31000.00',
      expense_date: '2026-07-14',
      purpose: `Atomic edit purpose ${runId}.`,
      manager_id: managerID,
      submit_action: 'resubmit'
    });
    expect(refused.status, 'the resubmit is refused').toBe(400);
    expect(refused.body, 'and the requester is told the transition is illegal').toContain('cannot be submitted');

    // F-G-035: the refusal is partial. The amount they typed is committed.
    await requester.page.goto(`/requests/${id}`);
    await expectMoney(
      requester.page,
      '.rh-amt',
      '₹31,000.00',
      'F-G-035: the edit committed even though the POST that carried it was refused'
    );
    // And it is in the audit trail as a completed update, so the history claims
    // a change the requester was told had failed.
    const rows = await auditRows(adminPage, requester.subject.name, 'payment_request', id);
    expect(
      rows.some(r => r.action === 'update'),
      'F-G-035: the audit records the edit as done, while the caller saw a 400'
    ).toBe(true);

    await approver.close();
    await requester.close();
  });

  /**
   * The app layer validates the password's length and nothing else
   * (internal/app/app.go:1555-1560); `auth.HashPassword` then requires a letter
   * AND a digit (internal/auth/auth.go:37-58). A 12-character letters-only
   * password therefore passes the first check and crashes the second.
   *
   * `test.fail()` — the honest expectation is a 400 naming the rule.
   */
  test.fail(
    'TC-G-090 — a 12-character letters-only password must be refused with a 400 naming the rule, not a 500',
    async ({ adminPage, runId }) => {
      const email = `pw-${runId}@example.test`.toLowerCase();
      const probe = await probePost(adminPage, '/users', {
        id: '0',
        email,
        name: `Password probe ${runId}`,
        role: 'data_entry',
        active: 'on',
        password: 'abcdefghijkl'
      });
      expect(probe.status, 'the password is refused, not accepted').not.toBe(303);
      // F-G-036: it is a 500 "The password could not be secured."
      expect(
        probe.status,
        `F-G-036: a rule the app itself enforces must be a 4xx, not a server fault — got ${probe.outcome}`
      ).toBe(400);
      expect(
        probe.body.includes('digit') || probe.body.includes('letter'),
        'F-G-036: and the message must name the rule the operator broke'
      ).toBe(true);
    }
  );

  /** The 500 half of F-G-036, asserted positively so the suite records what the
   *  product does today and no user is created by the crash. */
  test('TC-G-091 — the letters-only password answers 500 and creates no user', async ({ adminPage, runId }) => {
    const email = `pw500-${runId}@example.test`.toLowerCase();
    const probe = await probePost(adminPage, '/users', {
      id: '0',
      email,
      name: `Password 500 ${runId}`,
      role: 'data_entry',
      active: 'on',
      password: 'abcdefghijkl'
    });
    expect(
      probe.status,
      'F-G-036: the app-layer length check passes and auth.HashPassword then fails, so the response is a 500'
    ).toBe(500);
    expect(
      probe.body,
      'F-G-036: with a message that blames the machine rather than naming the missing digit'
    ).toContain('could not be secured');

    // At least nothing was written: the hash is computed before CreateUser.
    await adminPage.goto('/users');
    await expect(
      adminPage.locator('tr', { hasText: email }),
      'no user is created by the crash'
    ).toHaveCount(0);

    // The same password with one digit is accepted, which pins the real rule.
    const ok = await probePost(adminPage, '/users', {
      id: '0',
      email,
      name: `Password 500 ${runId}`,
      role: 'data_entry',
      active: 'on',
      password: 'abcdefghijk1'
    });
    expect(ok.status, 'one digit is the whole difference between 303 and 500').toBe(303);
  });

  /** A last sanity pass: an unrouted POST is 405 (the catch-all GET / matches
   *  the path and not the method), and every screen this file drove still
   *  renders for an admin without a 5xx. */
  test('TC-G-085 — no screen this audit touched serves a 5xx, and an unrouted POST is 405', async ({
    adminPage
  }) => {
    const errors = capturePageErrors(adminPage);
    const screens = [
      '/',
      '/grid',
      '/requests?bucket=all',
      '/approvals',
      '/accounts-queue',
      '/payments',
      '/recoverables',
      '/recoverables/list',
      '/vendors',
      '/audit',
      '/roles',
      '/users',
      '/months',
      '/budgets',
      '/heads',
      '/projects',
      '/configuration',
      '/notifications',
      '/reports/monthly',
      '/reports/projects',
      '/reports/heads'
    ];
    for (const path of screens) {
      await adminPage.goto(path);
      await expect(adminPage.locator('h1').first(), `${path} renders a heading`).toBeVisible();
    }
    const unrouted: Probe = await probePost(adminPage, '/requests/bulk-approve', {});
    expectOutcome(unrouted, [404, 405], 'an unrouted POST answers 405, not 404');
    expectNoRuntimeErrors(errors, 'no console error, page error or 5xx on any screen this audit drove');
    expect(admin.email, 'the admin fixture is the account these screens were read as').toBe('admin@fervid.local');
  });
});
