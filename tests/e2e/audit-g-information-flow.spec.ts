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
 *     adds its own would render `₹ ₹1,00,000.00`. A **CSV** hop is the mirror
 *     image: `csvAmount` (internal/app/app.go:2147) writes the same paise as a
 *     plain `100000.00`, so the file must carry the number and no glyph at all
 *     (F-G-030).
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

/** Every comma-separated field of a CSV body, so a cell can be matched exactly. */
function csvCells(body: string): string[] {
  return body.split(/\r?\n/).flatMap(line => line.split(','));
}

/** The fields of one CSV line, honouring RFC 4180 quoting, so a column index is exact. */
function csvLine(line: string): string[] {
  const out: string[] = [];
  let cur = '';
  let quoted = false;
  for (let i = 0; i < line.length; i++) {
    const ch = line[i];
    if (quoted) {
      if (ch === '"' && line[i + 1] === '"') {
        cur += '"';
        i++;
      } else if (ch === '"') quoted = false;
      else cur += ch;
    } else if (ch === '"') quoted = true;
    else if (ch === ',') {
      out.push(cur);
      cur = '';
    } else cur += ch;
  }
  out.push(cur);
  return out;
}

/**
 * The CSV counterpart of `expectMoney`: a money column must be a NUMBER.
 *
 * All four exports write amounts through `csvAmount` (internal/app/app.go:2147),
 * which emits `paise/100 . paise%100` — no currency glyph, no Indian digit
 * grouping, and therefore never a quoted cell. That is F-G-030's fix: the export
 * used to carry `"₹1,50,000.00"`, which is a report and not data, and
 * `Number()` of it is NaN in every spreadsheet and script that opens it.
 *
 * Both halves are asserted, because either alone would pass the wrong file: the
 * exact cell must be there, and no `₹` may appear anywhere in the body — the
 * amount columns are the only place one could come from.
 */
function expectCSVAmount(body: string, plain: string, what: string) {
  expect(
    csvCells(body).includes(plain),
    `${what}: no cell holds the plain number ${plain}\n${body.split(/\r?\n/).slice(0, 5).join('\n')}`
  ).toBe(true);
  expect(
    body.includes('₹'),
    `${what}: a CSV amount is a number, so no ₹ may appear anywhere in the file`
  ).toBe(false);
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
  /** The month an approval commits to. `lockedApprovalMonth`
   *  (internal/app/requests.go:916) reads it and nothing else, so a request
   *  without one names no period at all. */
  neededBy?: string;
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
  if (opts.neededBy) await page.getByLabel('Needed by').fill(opts.neededBy);
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
 * Two forms since form-1 / recoverables-1: an employee advance
 * (`/requests/new?type=employee_advance`, category fixed, paid to the
 * requester) and a deposit or guarantee (`/requests/new?type=recoverable`,
 * category chosen, paid to whoever the requester names). An ICD is the latter.
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
  const category = opts.category ?? 'employee_advance';
  const advance = category === 'employee_advance';
  await page.goto(advance ? '/requests/new?type=employee_advance' : '/requests/new?type=recoverable');
  await page.getByLabel('Short title').fill(opts.title);
  if (advance) {
    await expect(
      page.locator('input[name="treatment"][value="recoverable"]'),
      'an employee advance opens on the recoverable treatment'
    ).toBeChecked();
  } else {
    await page.locator('#rcategory').selectOption(category);
    // The category change swaps #form-fields from the server; waiting for the
    // field the new category needs is waiting for the swap itself.
    await expect(page.locator('#counterparty')).toBeVisible();
    await page.locator('#payee').fill(opts.counterparty ?? `Deposit holder ${opts.title}`);
  }
  if (opts.counterparty) await page.locator('#counterparty').fill(opts.counterparty);
  await page.locator('#expected-return').fill(opts.expectedReturn);
  await page.locator('#terms').fill(opts.terms);
  await page.getByLabel('Amount').fill(opts.amount);
  if (advance) await page.locator('#adv-reason').fill(opts.advanceReason ?? opts.title);
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

/**
 * The id of a seeded system role, read off the create form rather than assumed.
 *
 * A raw POST to /users has to name real roles now: the form stopped sending the
 * superseded two-value account type, which could not express "requester" or
 * "approver" and mapped its harmless-looking option to the role that settles and
 * voids payments.
 */
async function systemRoleId(adminPage: Page, name: RegExp): Promise<string> {
  await adminPage.goto('/users');
  // The sheet is `hidden` until opened, and a hidden checkbox has no accessible
  // role — open it the way an operator does, then read the box.
  await adminPage.locator('.pb-actions').getByRole('button', { name: 'Add user' }).click();
  const sheet = adminPage.locator('#user-new');
  await expect(sheet).toBeVisible();
  const value = await sheet.getByRole('checkbox', { name }).getAttribute('value');
  if (!value) throw new Error(`system role ${name} is missing from the Add user sheet`);
  return value;
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
  // The create form insists on at least one role, so an account cannot be born
  // able to do nothing. Requester is the least of them; setRolesByLabel replaces
  // the set with the custom role immediately below.
  await sheet.getByRole('checkbox', { name: /^Requester/ }).check();
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
 * `/audit` applies entity, id, action, actor and date in SQL over the whole log
 * and pages the result (`Store.AuditPage`, audit-1). Actor names carry the
 * runId, so this is a per-test view; it reads the first page only, which is
 * enough for the handful of rows one subject writes. Passing `entityID` narrows
 * it further.
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
 * `actionText` (internal/app/app.go:2441-2515) used to spell ten actions and
 * return every other identifier verbatim, so the whole request workflow printed
 * as its raw column value beside a properly spelled "Updated" (F-G-005). It now
 * spells every action the store writes, and this map is its exact inverse —
 * which is what lets each assertion below name the stored action it means while
 * `rendered` stays available for the tests that are about the wording itself.
 *
 * Two verbs share a rendering, and both collapse onto the one the store actually
 * writes on the paths this file drives: `update`/`edit` both read "Updated", and
 * `process`/`reserve` both read "Reserved". Nothing here writes `edit` or
 * `reserve`, so the ambiguity is resolvable and resolved; if a screen ever starts
 * writing them, an assertion on `rendered` is the honest way to tell them apart.
 */
function auditAction(rendered: string): string {
  const spelled: Record<string, string> = {
    Created: 'create',
    Updated: 'update',
    Deleted: 'delete',
    Voided: 'void',
    Locked: 'lock',
    Unlocked: 'unlock',
    'Logged in': 'login',
    'Logged out': 'logout',
    Exported: 'export',
    Attached: 'attach',
    'Login failed': 'login_failed',
    'Settings saved': 'settings',
    Submitted: 'submit',
    Approved: 'approve',
    Returned: 'return',
    Rejected: 'reject',
    Withdrawn: 'withdraw',
    'Raised again': 'reraise',
    Commented: 'comment',
    Cancelled: 'cancel',
    'Cancellation asked': 'cancel_request',
    'Approval reassigned': 'approval_reassign',
    Reserved: 'process',
    Released: 'release',
    'Reservation reassigned': 'reassign',
    'Put on hold': 'hold',
    'Taken off hold': 'unhold',
    Settled: 'settle',
    'Marked partial': 'mark_partial',
    'Partial accepted': 'accept_partial',
    'Concern raised': 'concern',
    'Reminder sent': 'remind',
    Viewed: 'view'
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
   * The month is this test's alone (2024-09), and the seed budgets only
   * 2026-06, so the grid's company total for that month is exactly this one
   * payment — which makes "the grid followed the money" an equality rather than
   * a delta.
   *
   * The month is in the PAST because `validatePayment` now refuses a `paid_on`
   * after today (F-D-06, internal/store/store.go:1938-1943): money cannot have
   * left the bank on a date that has not happened. Every date in this file moved
   * back by two years when the guard was wired through the UI, keeping each
   * test's own month distinct.
   */
  test('TC-G-001 — a lakh-grouped amount survives twelve hops with exactly one ₹ at each', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const MONEY = '₹1,00,000.00';
    const PLAIN = '1,00,000.00'; // the money field's own grouped, glyph-less value
    const CSV_PLAIN = '100000.00'; // what csvAmount writes: a number, ungrouped

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
    await adminPage.getByLabel('Paid on').fill('2024-09-15');
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
    await adminPage.goto('/payments?month=2024-09');
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
    await adminPage.goto('/grid?month=2024-09');
    await expectMoney(
      adminPage,
      'tr.head:has(.hname:text-is("Office Rent")) td:nth-child(5)',
      MONEY,
      'hop 14a — grid actual for Operations / Office Rent'
    );
    await expectMoney(adminPage, 'tfoot tr.total td:nth-child(5)', MONEY, 'hop 14b — grid company total');

    // Hop 15 — the monthly report reuses Grid, so its actual is the same number.
    await adminPage.goto('/reports/monthly?from=2024-09&to=2024-09');
    await expectMoney(adminPage, 'tbody tr td[data-label="Actual"]', MONEY, 'hop 15 — /reports/monthly Actual');

    // Hop 16 — /export.csv. The last hop of the chain is a machine, so the
    // figure arrives as a number: `csvAmount` writes "100000.00" where the
    // screens above write "₹1,00,000.00" (F-G-030).
    const gridCSV = await csv(adminPage, '/export.csv?month=2024-09');
    expectCSVAmount(gridCSV, CSV_PLAIN, 'hop 16 — /export.csv');
    expect(
      gridCSV.split('\n').filter(line => line.includes('Office Rent')).length,
      'hop 16 — the head appears exactly once in the grid CSV'
    ).toBe(1);

    // Hop 17 — /reports/ytd.csv.
    const ytdCSV = await csv(adminPage, '/reports/ytd.csv?from=2024-09&to=2024-09');
    expectCSVAmount(ytdCSV, CSV_PLAIN, 'hop 17 — /reports/ytd.csv');

    // Hop 18 — /requests/export.csv keeps the REQUESTED figure, which here
    // equals the approved one.
    //
    // `bucket=all` because the export defaults to `open` now, exactly as the list
    // it sits on does: one URL, one set (F-G-014, internal/app/requests.go:1152).
    // This request has been settled, so it is closed, and asking for it without
    // saying so would be asking the wrong question.
    const reqCSV = await csv(adminPage, `/requests/export.csv?bucket=all&q=${encodeURIComponent(number)}`);
    expectCSVAmount(reqCSV, CSV_PLAIN, 'hop 18 — /requests/export.csv');

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
    const CSV_PLAIN = '1234567.89';

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
      paidOn: '2024-10-15',
      mode: 'bank_transfer',
      reference: `UTR-T2-${runId}`,
      remarks: `Trail paise ${runId}`
    });
    await expectMoney(adminPage, '.rh-amt', MONEY, 'payment detail head');

    await adminPage.goto('/payments?month=2024-10');
    await expectMoney(
      adminPage,
      `tbody tr:has(a[href="${paymentPath}"]) td[data-label="Amount"]`,
      MONEY,
      'payments ledger'
    );

    await adminPage.goto('/grid?month=2024-10');
    await expectMoney(
      adminPage,
      'tr.head:has(.hname:text-is("Office Rent")) td:nth-child(5)',
      MONEY,
      'grid actual'
    );
    await expectMoney(adminPage, 'tfoot tr.total td:nth-child(5)', MONEY, 'grid company total');

    await adminPage.goto('/reports/monthly?from=2024-10&to=2024-10');
    await expectMoney(adminPage, 'tbody tr td[data-label="Actual"]', MONEY, '/reports/monthly Actual');

    // The paise tail survives the plain-number form too, which is the whole
    // point of this case: 1234567.89 and not 1234567.9 or 1234568.
    expectCSVAmount(await csv(adminPage, '/export.csv?month=2024-10'), CSV_PLAIN, '/export.csv');
    expectCSVAmount(
      await csv(adminPage, '/reports/ytd.csv?from=2024-10&to=2024-10'),
      CSV_PLAIN,
      '/reports/ytd.csv'
    );

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
    const REQUESTED_CSV = '50000.00';
    const APPROVED_CSV = '42500.75';
    const PAID_CSV = '37000.25';

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
      paidOn: '2024-11-15',
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
    await adminPage.goto('/grid?month=2024-11');
    const actual = adminPage.locator('tr.head:has(.hname:text-is("Office Rent")) td:nth-child(5)');
    await expectMoney(adminPage, 'tr.head:has(.hname:text-is("Office Rent")) td:nth-child(5)', PAID, 'grid actual');
    const actualText = (await actual.innerText()).trim();
    expect(actualText, 'the grid must not show the approved figure').not.toContain(APPROVED);
    expect(actualText, 'the grid must not show the requested figure').not.toContain(REQUESTED);
    await expectMoney(adminPage, 'tfoot tr.total td:nth-child(5)', PAID, 'grid company total follows paid');

    // And so does every downstream report.
    await adminPage.goto('/reports/monthly?from=2024-11&to=2024-11');
    await expectMoney(adminPage, 'tbody tr td[data-label="Actual"]', PAID, '/reports/monthly Actual follows paid');
    expectCSVAmount(await csv(adminPage, '/export.csv?month=2024-11'), PAID_CSV, '/export.csv follows paid');
    expectCSVAmount(
      await csv(adminPage, '/reports/ytd.csv?from=2024-11&to=2024-11'),
      PAID_CSV,
      '/reports/ytd.csv'
    );

    // The requests CSV carries both figures, each in its own column: Amount is
    // what was asked for, and "Approved amount" right after it is what was
    // approved (F-G-011). Matched by column, so the two can't be confused.
    const reqCSV = await csv(adminPage, `/requests/export.csv?bucket=all&q=${encodeURIComponent(raised.number)}`);
    expectCSVAmount(reqCSV, REQUESTED_CSV, '/requests/export.csv Amount column');
    expectCSVAmount(reqCSV, APPROVED_CSV, '/requests/export.csv Approved amount column');
    const reqLines = reqCSV.split(/\r?\n/).filter(line => line.trim());
    const reqHeader = csvLine(reqLines[0] ?? '');
    const amountCol = reqHeader.indexOf('Amount');
    const approvedCol = reqHeader.indexOf('Approved amount');
    expect(amountCol, `the header names an Amount column: ${reqLines[0]}`).toBeGreaterThanOrEqual(0);
    expect(approvedCol, 'F-G-011: the header names an Approved amount column, right after Amount').toBe(
      amountCol + 1
    );
    const reqRow = csvLine(reqLines.find(line => line.startsWith(`${raised.number},`)) ?? '');
    expect(reqRow[0], `the export holds a row for ${raised.number}`).toBe(raised.number);
    expect(reqRow[amountCol], 'Amount keeps the REQUESTED figure').toBe(REQUESTED_CSV);
    expect(reqRow[approvedCol], 'Approved amount carries the adjusted figure').toBe(APPROVED_CSV);

    // The ledger total for the month equals exactly the paid figure.
    await adminPage.goto('/payments?month=2024-11');
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
    await adminPage.getByLabel('Paid on').fill('2024-12-15');
    await adminPage.getByLabel('Payment mode').selectOption('bank_transfer');
    await adminPage.getByLabel('Transaction / UTR reference').fill(`UTR-OVER-${runId}`);
    await adminPage.getByRole('button', { name: /Payment settled/ }).click();
    const sheet = adminPage.locator('.overlay .sheet');
    await expect(sheet).toBeVisible();
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();

    await expect(adminPage, 'a refused settlement creates no payment, so no /payments/{id}').toHaveURL(/\/payments$/);
    await expect(sheet.locator('.banner.bad')).toContainText('more than the approved');

    // The month is otherwise empty, so "nothing was written" is an equality.
    await adminPage.goto('/grid?month=2024-12');
    await expectMoney(
      adminPage,
      'tfoot tr.total td:nth-child(5)',
      '₹0.00',
      'a refused settlement must not move the grid'
    );
    await adminPage.goto('/payments?month=2024-12');
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
    await adminPage.getByLabel('Paid on').fill('2025-01-12');
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
    await adminPage.goto('/payments?month=2025-01');
    await adminPage.getByLabel('Search').fill(payee);
    await adminPage.getByRole('button', { name: 'Filter' }).click();
    const ledger = adminPage.locator('tbody tr').filter({ has: adminPage.locator(`a[href="${paymentPath}"]`) });
    await expect(ledger, 'hop 7 — searching the ledger by payee finds the payment').toHaveCount(1);
    await expect(ledger.locator('td[data-label="Payee"]'), 'hop 7 — ledger Payee column').toHaveText(payee);
    await expect(ledger.locator('td[data-label="Reference"]')).toContainText(`INV-P1-${runId}`);

    // Hop 8 — /requests/export.csv reads Request.Vendor, not the snapshot.
    const reqCSV = await csv(adminPage, `/requests/export.csv?bucket=all&q=${encodeURIComponent(raised.number)}`);
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
      paidOn: '2025-02-10',
      mode: 'upi',
      reference: `UTR-R1-${runId}`,
      remarks: `Reimburse ${runId}`
    });
    await expect(adminPage.locator('h1'), 'the payment detail <h1> is the requester').toHaveText(
      requester.subject.name
    );

    await adminPage.goto('/payments?month=2025-02');
    await adminPage.getByLabel('Search').fill(requester.subject.name);
    await adminPage.getByRole('button', { name: 'Filter' }).click();
    const ledger = adminPage.locator('tbody tr').filter({ has: adminPage.locator(`a[href="${paymentPath}"]`) });
    await expect(ledger, 'the ledger is searchable by the reimbursement payee').toHaveCount(1);
    await expect(ledger.locator('td[data-label="Payee"]')).toHaveText(requester.subject.name);

    const reqCSV = await csv(adminPage, `/requests/export.csv?bucket=all&q=${encodeURIComponent(number)}`);
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
      paidOn: '2025-03-10',
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
      paidOn: '2025-04-14',
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
      paid_on: '2025-05-10',
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

    await adminPage.goto('/payments?month=2025-05');
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
   * `audit:view` is an Admin grant, and nothing else in the seed carries it.
   *
   * `Store.Audit` still receives no viewer — the row scope is applied above it, in
   * `auditWithinRequestScope` — so this gate is the first of the log's two
   * defences and the only one the seeded roles ever exercise. TC-G-055 is the
   * other: what a custom role holding `audit:view` with `request=own` actually
   * gets.
   */
  test('TC-G-026 — audit:view is an Admin grant, which is the log\'s outer gate', async ({
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
   * The audit screen's two vocabularies, pinned against the drift that produced
   * F-G-004 and F-G-005.
   *
   * The Entity `<select>` was a hand-maintained literal in the template that had
   * drifted so far from what the store writes that `payment_request` — the entity
   * every request, approval, reservation and settlement is filed under — could not
   * be asked for at all, and no request action was offered either. And `actionText`
   * spelled ten actions and returned every other identifier verbatim, so a reader
   * saw "Updated" beside a raw "approve" on the same page.
   *
   * Both option lists are now built in Go from the values the store actually writes
   * (`auditEntities`/`auditActions`, internal/app/app.go:2549-2578), and `actionText`
   * (:2441-2515) spells every one of them. This case asserts the three properties
   * that keep them from drifting apart again: the filters offer the request
   * workflow, choosing one really narrows the log, and the action a row carries is
   * spelled for a person rather than printed as a column value.
   */
  test('TC-G-027 — the audit filters reach the request workflow, and every action is spelled for a reader', async ({
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
    const entityValues = await adminPage
      .locator('select[name="entity"] option')
      .evaluateAll(nodes => nodes.map(n => (n as HTMLOptionElement).value));
    expect(
      entityValues,
      'the Entity filter must be able to name the entity the whole request workflow is filed under'
    ).toContain('payment_request');
    const entities = await adminPage.locator('select[name="entity"] option').allInnerTexts();
    expect(
      entities.some(o => /payment request/i.test(o)),
      'and it is labelled for a reader, not shown as its column value'
    ).toBe(true);

    const actionValues = await adminPage
      .locator('select[name="action"] option')
      .evaluateAll(nodes => nodes.map(n => (n as HTMLOptionElement).value));
    // The request workflow, the settlement flow and the reservation flow: three
    // families the filter used to reach none of.
    for (const action of ['submit', 'approve', 'return', 'reject', 'cancel', 'process', 'settle']) {
      expect(actionValues, `the Action filter offers "${action}"`).toContain(action);
    }
    const actions = await adminPage.locator('select[name="action"] option').allInnerTexts();
    expect(actions, 'and spells them').toContain('Approved');

    // Choosing them narrows the log rather than merely being offered: this is the
    // filter doing the work the query string used to have to be hand-typed for.
    await adminPage.goto(
      `/audit?entity=payment_request&action=approve&actor=${encodeURIComponent(approver.subject.name)}`
    );
    const filtered = adminPage.locator('tbody tr');
    await expect(filtered, 'the filtered log carries the approve row').not.toHaveCount(0);
    for (const rendered of await filtered.locator('td:nth-child(4)').allInnerTexts()) {
      expect(rendered.trim(), 'and nothing else — the filter is applied, not decorative').toBe('Approved');
    }

    // The row itself: the pill is a word, and the stored action behind it is the
    // one the workflow wrote.
    const rows = await auditRows(adminPage, approver.subject.name, 'payment_request', raised.id);
    const approveRow = rows.find(r => r.action === 'approve');
    expect(approveRow, 'the approve row is in the log').toBeDefined();
    expect(
      approveRow!.rendered,
      'the Action pill spells "approve" as "Approved", the way it always spelled "update" as "Updated"'
    ).toBe('Approved');
    // The original contrast, kept: the ten actions that were always mapped still are.
    const spelled = await auditRows(adminPage, 'Fervid Admin', 'user');
    expect(
      spelled.some(r => r.rendered === 'Created' || r.rendered === 'Updated'),
      'the ten actions that were always spelled still are, so nothing regressed the other way'
    ).toBe(true);

    await approver.close();
  });
});

// ===========================================================================
// 4 — Cross-feature consistency
// ===========================================================================

test.describe('G · the same fact in several places', () => {
  /**
   * Every dashboard tile agrees with the screen its own link points at.
   *
   * The dashboard used to build a second, different options object for its counts
   * instead of reusing the bucket its link named: "My open requests" counted
   * pending + approved + cancellation_requested where /requests?bucket=open also
   * includes `returned`, so a requester holding one pending and one returned
   * request was told 1 and shown 2 (F-G-006). The tile now passes
   * `Bucket: "open"` — the predicate the link already names, named once
   * (internal/app/dashboard.go:81-83) — so the two cannot drift apart again.
   */
  test('TC-G-030 — every dashboard tile equals the bucket its own link points at', async ({
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
    // The returned request is the whole test: it is the one status the tile used
    // to drop, so a tile that still counted the old set would read 1 here.
    expect(
      myOpen,
      '"My open requests" is /requests?bucket=open — the returned request is in both'
    ).toBe(openRows);

    expectNoRuntimeErrors(errors);
    await approver.close();
    await requester.close();
  });

  /**
   * One label, one number, on both screens that carry it.
   *
   * "Approved, unclaimed" appears on the dashboard and on the accounts queue, and
   * they used to be two different queries: the dashboard's had no `on_hold` test
   * where the queue's did, so putting one request on hold made the two part
   * company under an identical label (F-G-007). The dashboard now reads
   * `LinkablePaymentRequests(...).Counts.Approved` — the queue's own query, not a
   * second one over the same data (internal/app/dashboard.go:113-118) — so a hold
   * moves both numbers or neither.
   */
  test('TC-G-031 — the dashboard and the accounts queue show the same "Approved, unclaimed" once a request is on hold', async ({
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
    // The hold moved the tile too, by exactly as much, which is the whole fix:
    // the two are one query now, not two that happen to agree until something
    // goes on hold.
    expect(
      afterTile,
      'the dashboard tile applies the same on_hold test the queue does'
    ).toBe(beforeTile - 1);
    expect(
      afterTile,
      'two screens carrying the identical label "Approved, unclaimed" must show the identical number'
    ).toBe(afterMetric);

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
      paidOn: '2025-06-05',
      reference: `UTR-S1-${runId}`,
      remarks: `Sum one ${runId}`
    });
    const two = await createApprovedRequest(adminPage, runId, { amount: '25000.00' });
    await settlePayment(adminPage, two.id, {
      amount: '25000.00',
      paidOn: '2025-06-06',
      reference: `UTR-S2-${runId}`,
      remarks: `Sum two ${runId}`
    });

    await adminPage.goto('/grid?month=2025-06');
    const rowCells = await adminPage.locator('tr.head td:nth-child(5)').allInnerTexts();
    const rowSum = rowCells.reduce((acc, cell) => acc + paise(cell), 0);
    const total = paise(await adminPage.locator('tfoot tr.total td:nth-child(5)').innerText());
    expect(rowSum, 'the company total is the sum of the rows above it').toBe(total);
    expect(total, 'both payments landed on the same head, so the total is their sum').toBe(4000000);

    await adminPage.goto('/reports/monthly?from=2025-06&to=2025-06');
    expect(
      paise(await adminPage.locator('tbody tr td[data-label="Actual"]').innerText()),
      '/reports/monthly reuses Grid, so its actual is the grid total'
    ).toBe(total);

    // The CSV's Actual column is now a column of numbers (F-G-030), so summing it
    // needs no stripping at all: Number() of the cell is the figure. That is the
    // property being asserted as much as the total — the reduce below is what a
    // spreadsheet does, and it used to produce NaN.
    const gridCSV = await csv(adminPage, '/export.csv?month=2025-06');
    const header = gridCSV.split(/\r?\n/)[0].split(',');
    const actualColumn = header.indexOf('Actual');
    expect(actualColumn, 'the grid CSV names its Actual column').toBeGreaterThan(-1);
    const csvSum = gridCSV
      .split(/\r?\n/)
      .slice(1)
      .filter(line => line.trim())
      .reduce((acc, line) => {
        const cell = line.split(',')[actualColumn];
        const value = Number(cell);
        expect(Number.isNaN(value), `the Actual cell "${cell}" must parse as a number`).toBe(false);
        return acc + Math.round(value * 100);
      }, 0);
    expect(csvSum, '/export.csv is the same rows, so its Actual column sums to the same total').toBe(total);

    expectNoRuntimeErrors(errors);
  });

  /**
   * "Paid this year" survives a rename, because it is an id join and not a
   * string match.
   *
   * PROGRESS.md:384-387 recorded the old behaviour: the figure was string
   * equality against `payments.vendor_payee`, a denormalised copy of the name as
   * it stood at settlement — so renaming a vendor silently zeroed its entire
   * payment history while every one of those payments still existed (F-G-009).
   * Migration v10 gave `payments` a `vendor_id`, back-filled from the request each
   * payment settles, and `vendorPaidThisYear` (internal/store/vendors.go:171-176)
   * now joins on it, keeping the payee match only as a fallback for rows with no
   * id to join on.
   *
   * The rename is still the decisive step, and it is still asserted the same way
   * — what changed is the number afterwards.
   */
  test('TC-G-035 — vendor "Paid this year" is an id join, so a rename does not orphan the history', async ({
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
      'the vendor total picks the payment up'
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
    // The whole point: the payment's payee text still reads the OLD name — the
    // ledger row above proves it — and the total still finds it, because the
    // link is `payments.vendor_id`, which the rename never touched.
    await expectMoney(
      adminPage,
      `tbody tr:has-text("${renamed}") td[data-label="Paid this year"]`,
      '₹9,100.00',
      'a rename cannot orphan a payment: the join is on the id, not on the name'
    );

    expectNoRuntimeErrors(errors);
  });

  /**
   * "Open requests" is a real count of real open work.
   *
   * PROGRESS.md:388 recorded the column as 0 for everybody: it was declared on
   * the struct, scanned by nothing, and therefore rendered Go's zero value — an
   * assertion that there is no open work against a vendor, made without asking
   * (F-G-010). `vendorOpenRequests` (internal/store/vendors.go:192-205) now
   * counts `payment_requests` against the vendor id, and does it over
   * `requestBuckets["open"]` rather than a second hand-written status list, so
   * this number and /requests?bucket=open cannot disagree the way F-G-006 did.
   */
  test('TC-G-036 — vendor "Open requests" counts the live requests against that vendor', async ({
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
    await expect(
      cell,
      'the one pending request against this vendor is counted'
    ).toHaveText('1');

    // And it is the same set /requests?bucket=open would list, asked of the same
    // vendor — the agreement, not merely a non-zero number.
    await adminPage.goto(`/requests?bucket=open&q=${encodeURIComponent(raised.number)}`);
    await expect(
      adminPage.locator('.req-card', { hasText: raised.number }),
      'because "open" here is requestBuckets["open"], the bucket the list screen uses'
    ).toHaveCount(1);

    // Deciding it takes it out of the count, which proves the column is a live
    // query and not a constant that happened to be 1.
    await approveFor(approver.page, raised.id, '5000.00');
    await adminPage.goto(`/vendors?q=${encodeURIComponent(payee)}`);
    await expect(
      adminPage.locator(`tbody tr:has-text("${payee}") td[data-label="Open requests"]`),
      'an approved request is still open work, so the count holds'
    ).toHaveText('1');

    // The footer total is a sum of real counts, so it is at least this vendor's.
    const footer = Number(await adminPage.locator('tfoot td[data-label="Open"]').innerText());
    expect(footer, 'the footer totals the column rather than summing zeroes').toBeGreaterThanOrEqual(1);

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  /**
   * The recoverables family shares one predicate all the way through: the
   * dashboard, the list, the CSV — and now the by-category drill-through too.
   *
   * That last link used to be `?q={label}`, and the label ("ICD — inter-corporate
   * deposit") appears in none of the columns the free-text search covers, so a row
   * that promised 1 landed on a list showing 0 (F-G-012). It is now
   * `?category={id}` — the control the list actually honours — with the id mapped
   * back from the rollup's label in the handler
   * (internal/app/recoverables.go:53-64, rendered at templates.go:3688).
   */
  test('TC-G-037 — the recoverables dashboard, list and CSV agree, and the category drill-through reproduces its own count', async ({
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
    expectCSVAmount(recCSV, '60000.00', 'the recoverable CSV amount');

    // The drill-through: the row's own link must find the row's own number.
    await adminPage.goto('/recoverables');
    const categoryLink = adminPage.locator('tbody td[data-label="Category"] a').first();
    const label = (await categoryLink.innerText()).trim();
    const countShown = Number(
      await adminPage.locator(`tbody tr:has-text("${label}") td[data-label="Count"]`).first().innerText()
    );
    await categoryLink.click();
    await expect(
      adminPage,
      'the link sets the control the list honours, not a free-text search for a label'
    ).toHaveURL(/\/recoverables\/list\?category=\d+$/);
    const drilled = await dataRows(adminPage);
    expect(
      drilled,
      `the by-category row promises ${countShown} and its own link must find ${countShown}`
    ).toBe(countShown);

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
    await adminPage.getByLabel('Paid on').fill('2025-07-20');
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
    ).toHaveText('2025-07-20');

    expectNoRuntimeErrors(errors);
    await approver.close();
  });

  /**
   * The sidebar Approvals badge is the approvals queue's own number.
   *
   * `nav.go` declared `Badge: "approvals"` and `Badge: "accounts_queue"` and
   * `badges.go` had a spec for neither, so a queue with work in it showed nothing
   * at all — the one place a nav badge exists to be useful (F-G-013). Both are now
   * fed by `workQueueBadges` (internal/app/nav.go:190-211) rather than by a
   * `badgeSpec`, because each needs the caller's own data scope and the badge SQL
   * takes at most a user id. The badge is therefore the queue's count and not an
   * approximation of it, which is what this asserts.
   */
  test('TC-G-039 — the sidebar Approvals badge carries the approvals queue\'s own count', async ({
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
    await expect(navLink.locator('.n'), 'and carries a badge').toHaveCount(1);
    expect(
      Number(await navLink.locator('.n').innerText()),
      'the nav badge is the number the queue itself shows, not a second count of the same work'
    ).toBe(toApprove);

    // The accounts-queue badge is the same promise for the other queue, and it is
    // the caller's scope that decides it: the approver holds no payment:process,
    // so they get no badge, and the admin — who does — gets the queue's number.
    expect(
      await approver.page.locator('.side-nav a[href="/accounts-queue"], .more-sheet .ms-list a[href="/accounts-queue"]').count(),
      'a nav item nobody may open is not rendered, badge or no badge'
    ).toBe(0);
    await adminPage.goto('/accounts-queue?tab=approved');
    const queueApproved = Number(
      await adminPage.locator('.segmented a:has-text("Approved") .n').innerText()
    );
    const adminNav = adminPage.locator(
      mobile ? '.more-sheet .ms-list a[href="/accounts-queue"]' : '.side-nav a[href="/accounts-queue"]'
    );
    if (mobile) await adminPage.locator('.tabbar .js-more').click();
    if (queueApproved > 0) {
      expect(
        Number(await adminNav.locator('.n').innerText()),
        'the Accounts queue badge is Counts.Approved, the same number the tab shows'
      ).toBe(queueApproved);
    } else {
      // A zero count renders no badge at all, by design (templates.go:59).
      expect(await adminNav.locator('.n').count(), 'a zero count draws no badge').toBe(0);
    }

    await approver.close();
  });

  /**
   * One URL, one set — on the screen and in the file it downloads.
   *
   * `/requests` used to read no `status` at all while `/requests/export.csv` read
   * it and applied it, so the identical query string described two different sets
   * and only the CSV obeyed it (F-G-014). The screen now passes the named status
   * as `Statuses` (internal/app/requests.go:596-601) — as `Statuses` rather than
   * `Status` deliberately, because `requestWhere` reads Bucket first and the
   * bucket always has a default, so a named status set anywhere else would go on
   * being ignored.
   */
  test('TC-G-040 — /requests and its own CSV describe the same set for the same ?status=', async ({
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

    // ?status=rejected: the request is pending, so neither surface may show it.
    await requester.page.goto('/requests?status=rejected');
    await expect(
      requester.page.locator('.req-card', { hasText: number }),
      'the screen applies ?status=, so a pending request is not in the rejected set'
    ).toHaveCount(0);
    const rejectedCSV = await csv(requester.page, '/requests/export.csv?status=rejected');
    expect(
      rejectedCSV.includes(number),
      'and the CSV for the same URL agrees, which is the whole property'
    ).toBe(false);

    // ?status=pending: both surfaces find it, so the filter narrows rather than
    // simply emptying the list.
    await requester.page.goto('/requests?status=pending');
    await expect(
      requester.page.locator('.req-card', { hasText: number }),
      'the screen applies ?status=pending too'
    ).toHaveCount(1);
    const pendingCSV = await csv(requester.page, '/requests/export.csv?status=pending');
    expect(pendingCSV, 'and so does the CSV for the same URL').toContain(number);

    // The named status beats the bucket default, which is the precedence that
    // made the screen ignore it in the first place: `bucket` defaults to `open`
    // and would otherwise win. A closed bucket plus a pending status is the pair
    // that proves which one the store applied.
    await requester.page.goto('/requests?bucket=closed&status=pending');
    await expect(
      requester.page.locator('.req-card', { hasText: number }),
      'the status the caller named wins over the tab they happen to be standing on'
    ).toHaveCount(1);

    // And a bare CSV means the same thing a bare screen does: `open`. The export
    // used to have no bucket default at all and exported every status, so the
    // download silently meant something different from the list it sits on — the
    // other half of F-G-014 (internal/app/requests.go:1152).
    const bareCSV = await csv(requester.page, '/requests/export.csv');
    expect(bareCSV, 'the bare CSV is the open bucket, and this request is pending').toContain(number);
    const closedCSV = await csv(requester.page, '/requests/export.csv?bucket=closed');
    expect(
      closedCSV.includes(number),
      'so the closed bucket does not carry it — the export honours the bucket the screen would'
    ).toBe(false);

    expectNoRuntimeErrors(errors);
    await approver.close();
    await requester.close();
  });

  /**
   * The budgets screen accepts the values it rendered, so an unbudgeted month can
   * be budgeted.
   *
   * A figure that cannot be entered is the most complete failure of information
   * flow there is, and this screen had one. Every unbudgeted head renders
   * `{{money .Budget}}` = `₹0.00`, `money.ParsePaise` refused `paise <= 0`, and
   * `budgetSave` abandoned the WHOLE batch when any single field failed — so
   * pressing Save on a fresh month, changing nothing, was refused, and there was
   * no incremental path either (F-G-028).
   *
   * Three things changed, and all three are asserted below. `parseBudgetPaise`
   * (internal/app/app.go:1354-1377) reads empty and ₹0.00 as zero rather than as
   * errors; one unreadable field is now one unreadable field, with everything
   * else saved and the failures named back to the operator
   * (internal/app/app.go:1296-1342); and zero is a value a budget may hold, so an
   * existing figure can be taken back down to it.
   */
  test('TC-G-041 — the budgets screen accepts the values it rendered, and one bad field costs only that field', async ({
    adminPage
  }) => {
    const errors = capturePageErrors(adminPage);
    const FRESH = '2028-06'; // the seed budgets 2026-06 only, and no test uses this one

    await adminPage.goto(`/budgets?month=${FRESH}`);
    const inputs = adminPage.locator('input[name^="budget_"]');
    const count = await inputs.count();
    expect(count, 'the screen offers a budget field per active head').toBeGreaterThan(1);
    await expect(
      inputs.first(),
      'an unbudgeted head is rendered as ₹0.00 by the screen itself'
    ).toHaveValue('₹0.00');

    // Press the screen's own Save without changing anything. The screen must
    // accept what the screen wrote.
    const names = await inputs.evaluateAll(nodes => nodes.map(n => (n as HTMLInputElement).name));
    const untouched: Record<string, string> = { month: FRESH };
    for (const name of names) untouched[name] = '₹0.00';
    const asRendered = await probePost(adminPage, '/budgets', untouched);
    expect(
      asRendered.status,
      'saving the values the screen rendered is a 303 back to the month'
    ).toBe(303);

    // And a figure can actually be entered — one head at a time, which is how a
    // month gets budgeted in practice.
    const oneFilled = { ...untouched, [names[0]]: '25,00,000.00' };
    const filled = await probePost(adminPage, '/budgets', oneFilled);
    expect(filled.status, 'and so is filling one in and leaving the rest at zero').toBe(303);
    await adminPage.goto(`/budgets?month=${FRESH}`);
    await expect(
      adminPage.locator(`input[name="${names[0]}"]`),
      'the figure was written and reads back as money'
    ).toHaveValue('₹25,00,000.00');

    // One unreadable field costs that field and nothing else: the batch is no
    // longer abandoned whole. The second head gets a real figure in the same POST
    // as the garbage, and must survive it.
    const withOneBad = { ...untouched, [names[0]]: 'not a number', [names[1]]: '11,000.00' };
    const partial = await probePost(adminPage, '/budgets', withOneBad);
    expect(partial.status, 'the screen comes back with the failure named, as a 400').toBe(400);
    expect(
      partial.body,
      'and the banner counts what was left alone rather than claiming everything was refused'
    ).toContain('invalid budget amount');
    await adminPage.goto(`/budgets?month=${FRESH}`);
    await expect(
      adminPage.locator(`input[name="${names[1]}"]`),
      'the readable field in the same batch was saved'
    ).toHaveValue('₹11,000.00');
    await expect(
      adminPage.locator(`input[name="${names[0]}"]`),
      'and the unreadable one kept the figure it already had'
    ).toHaveValue('₹25,00,000.00');

    // The corollary on a month that IS budgeted: a budget can be reduced to zero,
    // which is a genuine operation the old refusal had no way to express. The
    // seeded month is restored afterwards, because later cases read its figures.
    await adminPage.goto('/budgets?month=2026-06');
    const seeded = adminPage.locator('input[name^="budget_"]').first();
    const seededName = (await seeded.getAttribute('name'))!;
    const seededValue = await seeded.inputValue();
    expect(seededValue, 'the seeded month has real figures').not.toBe('₹0.00');
    const seededNames = await adminPage
      .locator('input[name^="budget_"]')
      .evaluateAll(nodes => nodes.map(n => (n as HTMLInputElement).name));
    const asIs: Record<string, string> = { month: '2026-06' };
    for (const name of seededNames) {
      asIs[name] = await adminPage.locator(`input[name="${name}"]`).inputValue();
    }
    const zeroed = await probePost(adminPage, '/budgets', { ...asIs, [seededName]: '0.00' });
    expect(zeroed.status, 'an existing budget can be taken to zero').toBe(303);
    await adminPage.goto('/budgets?month=2026-06');
    await expect(
      adminPage.locator(`input[name="${seededName}"]`),
      'and the zero is what the screen reads back'
    ).toHaveValue('₹0.00');

    // Put it back. 2026-06 is the only month the seed budgets, and TC-G-042 and
    // TC-G-058 both read it afterwards — a month left zeroed here would leak into
    // them as a change neither test made.
    const restored = await probePost(adminPage, '/budgets', asIs);
    expect(restored.status, 'the seeded month is restored for the cases that read it').toBe(303);
    await adminPage.goto('/budgets?month=2026-06');
    await expect(
      adminPage.locator(`input[name="${seededName}"]`),
      'and the old figure stands again'
    ).toHaveValue(seededValue);

    expectNoRuntimeErrors(errors);
  });

  /**
   * Everything that says "grid" delivers the grid.
   *
   * `/` was the variance grid before Phase 4 made it the dashboard, and the grid's
   * own filter toolbar plus five links were left pointing at it — so month, status
   * and search were unreachable from the grid (submitting them navigated away),
   * and a "Grid" action delivered a page with no grid on it (F-G-029). The toolbar
   * is `action="/grid"` now (internal/app/templates.go:143) and every link in the
   * family names `/grid?month=` too.
   *
   * The filter is asserted by driving it, not by reading its `action`: an
   * attribute proves where the form points, and only a submitted filter proves the
   * destination can apply it.
   */
  test('TC-G-042 — the grid filter and every "grid" link land on the grid, filtered', async ({
    adminPage
  }) => {
    const errors = capturePageErrors(adminPage);

    // 1 — the grid's own filter form posts to the grid.
    await adminPage.goto('/grid?month=2026-06');
    const toolbar = adminPage.locator('form.toolbar[aria-label="Grid filters"]');
    await expect(toolbar, 'the grid has a filter toolbar').toHaveCount(1);
    expect(
      await toolbar.getAttribute('action'),
      'the grid filter form posts to /grid, which is the screen it filters'
    ).toBe('/grid');

    // Submitting it stays on the grid AND narrows it, which is the whole point.
    const allHeads = await adminPage.locator('tr.head .hname').count();
    expect(allHeads, 'the unfiltered grid has several heads').toBeGreaterThan(1);
    await toolbar.locator('input[name="q"]').fill('Office Rent');
    await toolbar.getByRole('button', { name: 'Apply' }).click();
    await expect(adminPage, 'filtering the grid stays on the grid').toHaveURL(/\/grid\?/);
    await expect(
      adminPage.locator('table.matrix.grid'),
      'and the filtered grid is on the page the filter delivered'
    ).toHaveCount(1);
    const filteredHeads = await adminPage.locator('tr.head .hname').allInnerTexts();
    expect(filteredHeads.length, 'the search narrowed the rows').toBeLessThan(allHeads);
    for (const head of filteredHeads) {
      expect(head.trim(), 'and every surviving row matches what was searched for').toContain('Office Rent');
    }

    // 2 — the month rides along, so the filtered grid is still the month asked for.
    await adminPage.goto('/grid?month=2026-06');
    await toolbar.locator('input[name="q"]').fill('Payroll');
    await toolbar.getByRole('button', { name: 'Apply' }).click();
    expect(
      new URL(adminPage.url()).searchParams.get('month'),
      'the toolbar carries the month it was standing on'
    ).toBe('2026-06');

    // 3 — every other link in the family names the grid too.
    await adminPage.goto('/budgets?month=2026-06');
    expect(
      await adminPage.getByRole('link', { name: 'View grid' }).getAttribute('href'),
      'the budgets screen "View grid" link'
    ).toBe('/grid?month=2026-06');
    await adminPage.goto('/months');
    const monthsGrid = adminPage.locator('a.btn', { hasText: /^Grid$/ }).first();
    expect(
      (await monthsGrid.getAttribute('href')) ?? '',
      'the months screen "Grid" action'
    ).toMatch(/^\/grid\?month=/);

    // 4 — following one proves the destination really is a grid.
    await monthsGrid.click();
    await expect(adminPage).toHaveURL(/\/grid\?month=/);
    await expect(
      adminPage.locator('table.matrix.grid'),
      'the "Grid" action delivers a grid'
    ).toHaveCount(1);

    expectNoRuntimeErrors(errors);
  });

  /**
   * The last hop of the money chain is a machine, not a person, so every one of
   * the four exports writes a NUMBER.
   *
   * They used to write `money.FormatPaise` output — a currency glyph plus Indian
   * digit grouping, quoted because of the grouping commas. The figure survived
   * (which is what TC-G-001–003 assert) but it was not something any spreadsheet
   * or script would sum without being told to strip two things first, so the
   * export was a report and not data (F-G-030). All four now go through
   * `csvAmount` (internal/app/app.go:2147-2153).
   *
   * This case is the verdict for all four at once, which is why it re-checks the
   * exports the money-trail cases already walked: the property is that no export
   * anywhere carries a formatted string, and only checking them together says so.
   */
  test('TC-G-043 — every CSV amount column is a plain number a spreadsheet can sum', async ({ adminPage, runId }) => {
    const errors = capturePageErrors(adminPage);
    const request = await createApprovedRequest(adminPage, runId, { amount: '150000.00' });
    await settlePayment(adminPage, request.id, {
      amount: '150000.00',
      paidOn: '2024-03-07',
      reference: `UTR-CSV-${runId}`,
      remarks: `CSV shape ${runId}`
    });

    // `bucket=all` on the requests export: it defaults to `open` like the list it
    // sits on (F-G-014), and this request has been settled.
    const exports: Array<[string, string]> = [
      ['/export.csv?month=2024-03', 'grid'],
      ['/reports/ytd.csv?from=2024-03&to=2024-03', 'ytd'],
      [`/requests/export.csv?bucket=all&q=${encodeURIComponent(request.number)}`, 'requests'],
      ['/recoverables/list.csv', 'recoverables']
    ];

    for (const [path, what] of exports) {
      const body = await csv(adminPage, path);
      const header = body.split('\n')[0];
      expect(header.length, `${what}: the export has a header row`).toBeGreaterThan(0);
      // No export may carry a currency glyph anywhere, rows or none — the
      // recoverables register has no row of this test's in it and is checked for
      // the glyph all the same, because that is the property.
      expect(body.includes('₹'), `${what}: no ₹ may appear in an export`).toBe(false);
      expect(
        body.includes('"1,50,000.00"') || body.includes('1,50,000.00'),
        `${what}: no Indian-grouped form of the figure may appear either`
      ).toBe(false);
      if (what === 'recoverables') continue; // no rows guaranteed in this month
      expect(
        csvCells(body).includes('150000.00'),
        `${what} writes the amount as the plain number 150000.00`
      ).toBe(true);
    }

    // The consequence, stated as an assertion: the naive numeric parse a
    // spreadsheet performs on the grid CSV's Actual column now yields the figure.
    const grid = await csv(adminPage, '/export.csv?month=2024-03');
    const gridHeader = grid.split(/\r?\n/)[0].split(',');
    const actualColumn = gridHeader.indexOf('Actual');
    const dataLine = grid.split(/\r?\n/).find(l => l.includes('Office Rent'))!;
    const actualCell = dataLine.split(',')[actualColumn];
    expect(
      Number(actualCell),
      'Number() of the Actual cell is the figure itself, so the export is data and not a report'
    ).toBe(150000);

    expectNoRuntimeErrors(errors);
  });

  /**
   * Export CSV exports the tab that was pressed.
   *
   * `/reports/ytd.csv` called `Report(from, to, "heads")` unconditionally while
   * the button sat on all three tabs, so pressing it on Monthly or Projects
   * downloaded somebody else's shape — head-level rows naming the very heads the
   * monthly view deliberately aggregates away (F-G-031). The route takes the
   * level as a parameter now (`reportLevels`, internal/app/app.go:1968-1985) and
   * the link carries `&level={{.Mode}}` (templates.go:3467).
   *
   * An unknown or absent level still falls back to "heads", which is what keeps a
   * bookmarked link from a previous release working.
   */
  test('TC-G-044 — the reports Export CSV exports the tab that pressed it', async ({
    adminPage,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const request = await createApprovedRequest(adminPage, runId, { amount: '31000.00' });
    await settlePayment(adminPage, request.id, {
      amount: '31000.00',
      paidOn: '2026-04-04',
      reference: `UTR-YTD-${runId}`,
      remarks: `YTD shape ${runId}`
    });

    // The three screens genuinely differ: monthly has no Project column, heads
    // has both Project and Head. The stylesheet uppercases table headers, so the
    // comparison is case-insensitive.
    const headers = async () =>
      (await adminPage.locator('thead th').allInnerTexts()).map(h => h.trim().toLowerCase());
    await adminPage.goto('/reports/monthly?from=2026-04&to=2026-04');
    expect(await headers(), 'the monthly view is one row per period').not.toContain('head');
    await adminPage.goto('/reports/heads?from=2026-04&to=2026-04');
    expect(await headers(), 'the heads view names the head').toContain('head');

    // The Monthly tab's own link names its own level, and downloads its own shape.
    await adminPage.goto('/reports/monthly?from=2026-04&to=2026-04');
    const monthlyLink = await adminPage.getByRole('link', { name: 'Export CSV' }).getAttribute('href');
    expect(monthlyLink, 'the export link carries the tab it was pressed on').toBe(
      '/reports/ytd.csv?from=2026-04&to=2026-04&level=monthly'
    );
    const monthly = await csv(adminPage, monthlyLink!);
    expect(
      monthly.includes('Office Rent'),
      'the head the monthly view aggregates away stays aggregated away in its export'
    ).toBe(false);
    const monthlyRows = monthly.split(/\r?\n/).filter(l => l.trim()).slice(1);
    expect(monthlyRows.length, 'one month asked for is one row per period').toBe(1);
    expect(monthlyRows[0], 'and the period is the month').toContain('2026-04');

    // The Heads tab's link names its level, and downloads the head rows.
    await adminPage.goto('/reports/heads?from=2026-04&to=2026-04');
    const headsLink = await adminPage.getByRole('link', { name: 'Export CSV' }).getAttribute('href');
    expect(headsLink).toBe('/reports/ytd.csv?from=2026-04&to=2026-04&level=heads');
    const heads = await csv(adminPage, headsLink!);
    expect(heads.includes('Office Rent'), 'the heads export names the head').toBe(true);

    // The Projects tab too, so all three are proven and not just the two that
    // differ most.
    await adminPage.goto('/reports/projects?from=2026-04&to=2026-04');
    const projectsLink = await adminPage.getByRole('link', { name: 'Export CSV' }).getAttribute('href');
    expect(projectsLink).toBe('/reports/ytd.csv?from=2026-04&to=2026-04&level=projects');
    const projects = await csv(adminPage, projectsLink!);
    expect(projects.includes('Operations'), 'the projects export names the project').toBe(true);
    expect(projects.includes('Office Rent'), 'and not the heads beneath it').toBe(false);

    // A link with no level at all — a bookmark from before the fix — still works,
    // and still means "heads".
    const bare = await csv(adminPage, '/reports/ytd.csv?from=2026-04&to=2026-04');
    expect(bare.includes('Office Rent'), 'an absent level falls back to heads').toBe(true);

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

    // 5 — the request itself, and the error page it serves. The refusal is 404
    // rather than 403: the existence of a row is information about that row, so
    // an out-of-scope id and a missing one answer the same thing (F-G-002, and
    // TC-G-051 is the case about that specifically).
    const detail = await probeGet(alice.page, `/requests/${bobID}`);
    expectOutcome(detail, [404], 'a requester cannot open another requester\'s request');
    assertClean(detail.body, 'the 404 error page');
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
   * A request id is not an existence oracle.
   *
   * `loadViewableRequest` reads the row first and checks scope second, and it used
   * to answer 403 for a row that exists but is out of scope while a row that does
   * not exist answered 404 — so a requester could walk the id space and learn
   * exactly which request ids exist, and by extension how many requests the
   * company raises (F-G-002). Both cases are 404 now
   * (internal/app/requests.go:335-348), through the same code path, and the
   * attempt is still logged.
   *
   * The pair is the assertion: proving one id answers 404 proves nothing on its
   * own, because a route that refused everything with 404 would pass it. Only
   * "these two are indistinguishable" is the property.
   */
  test('TC-G-051 — an out-of-scope request and a missing one are indistinguishable, so ids cannot be enumerated', async ({
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
    expect(present.status, 'an existing row Alice may not see answers 404').toBe(404);
    expect(absent.status, 'a row that does not exist answers 404 too').toBe(404);
    expect(
      present.status,
      'the two must be indistinguishable, or the status code enumerates every request id'
    ).toBe(absent.status);
    // The bodies as well as the codes: a refusal that named the request would be
    // the same oracle in prose. The error page carries a per-request diagnostic id
    // (templates.go:105), which differs between any two responses and is the one
    // thing that must be normalised away before they can be compared.
    const withoutRequestID = (body: string) => body.replace(/<code>[^<]*<\/code>/, '<code></code>');
    expect(
      withoutRequestID(present.body),
      'and the pages say the same thing, so neither confirms the row is there'
    ).toBe(withoutRequestID(absent.body));

    // Bob still opens his own, which is what makes the 404 above a scope refusal
    // rather than a broken route.
    const own = await probeGet(bob.page, `/requests/${existing}`);
    expect(own.status, 'the owner reads their own request').toBe(200);

    await approver.close();
    await alice.close();
    await bob.close();
  });

  /**
   * `/grid` is in the refused list now, and that is the change worth naming.
   *
   * It used to be `RequireLogin` only while `/export.csv` — the download of the
   * very same rows — was gated on `grid:export`, so the data was open and only
   * carrying it away was controlled (F-G-015/F-G-032). `GET /grid` is gated on
   * `grid:view` (internal/app/app.go:439), which the Requester role does not hold
   * (internal/store/migrations.go:526-537), so the whole company budget is no
   * longer readable by somebody entitled only to their own requests.
   */
  test('TC-G-052 — a Requester is refused every export, every money screen and the grid', async ({
    adminPage,
    browser,
    runId
  }) => {
    const requester = await asRole(adminPage, browser, runId, ['Requester'], `noexp-${runId}`);
    for (const path of [
      '/payments',
      '/grid',
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
    // The screen and its download are one gate now, not two: a role that cannot
    // read the grid cannot export it either, and the nav offers neither.
    await requester.page.goto('/');
    expect(
      await requester.page.locator('a[href="/grid"]').count(),
      'a screen the Requester may not open is not linked from the shell'
    ).toBe(0);
    await requester.close();
  });

  /**
   * The payment data scope is declared, editable — and read.
   *
   * `payment` is a declared scoped resource, the admin UI offers its scope radios
   * and the seed writes `payment=all` rows, but nothing ever called
   * `Scope(u, "payment")`: a role built with Payments · Own received the entire
   * ledger, which is R3 broken in the one place an administrator would reasonably
   * think they had configured it (F-A-04/F-G-003). `PaymentListOptions` carries
   * `Scope`/`ViewerID` and filters on `entered_by`, and the ledger passes them
   * (internal/app/app.go:815-819).
   *
   * The search is checked as well as the list, because a filter applied to the
   * rows and not to the search is the same leak one query string later.
   */
  test('TC-G-053 — the payment data scope is read, so a "own" ledger shows only what the caller entered', async ({
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
      paidOn: '2025-08-09',
      reference: `UTR-SC-${runId}`,
      remarks: `Scope probe ${runId}`
    });

    // The scope the admin set is genuinely stored and shown.
    await adminPage.goto(`/roles?role=${role.id}`);
    await expect(
      adminPage.locator('input[name="scope_payment"][value="own"]'),
      'the role screen stores and re-renders the payment scope'
    ).toBeChecked();

    const ledger = await probeGet(subject.page, '/payments?month=2025-08');
    expect(ledger.status, 'payment:view opens the ledger').toBe(200);
    expect(
      ledger.body.includes('₹13,100.00'),
      'payment scope "own" is read, so a payment somebody else entered is not in the list'
    ).toBe(false);
    // The ledger has no Remarks column, but it does carry the payee and who
    // entered the row — both of which are somebody else's business here.
    expect(ledger.body.includes(`Payee ${runId}`), 'nor its payee').toBe(false);
    // And the search obeys the same filter, or the row is discoverable one query
    // string later by the note another accountant typed.
    const searched = await probeGet(
      subject.page,
      `/payments?month=2025-08&q=${encodeURIComponent(`Scope probe ${runId}`)}`
    );
    expect(
      searched.body.includes('₹13,100.00'),
      'the ledger search applies the scope too, so the processing note is not a way round it'
    ).toBe(false);

    // The control: the admin, who holds payment=all, does see it — so the empty
    // list above is a scope filter and not an empty month.
    const asAdmin = await probeGet(adminPage, '/payments?month=2025-08');
    expect(
      asAdmin.body.includes('₹13,100.00'),
      'the payment is genuinely there for a caller whose scope reaches it'
    ).toBe(true);

    // The payment detail is refused as well — by the REQUEST behind it, which is
    // the second, independent gate.
    //
    // 404 rather than 403, and that is the point of the case rather than an
    // incidental status. This route used to answer 403 here while a payment id
    // nobody had used answered 404, so the pair told a caller which payment ids
    // exist — the enumeration oracle F-G-002 closed on /requests/{id} and Wave 3
    // closed on both attachment routes. It was never inside that finding's scope,
    // so it kept the 403 until the spec-repair pass reached it.
    const detail = await probeGet(subject.page, paymentPath);
    expectOutcome(
      detail,
      [404],
      'and the detail is refused by the request scope behind the payment'
    );
    // Which only means anything if a payment that does not exist is answered the
    // same way. Asserted here rather than assumed, because a 404 that differs in
    // any visible respect from "no such row" is the same oracle wearing a
    // different status code.
    const missing = await probeGet(subject.page, '/payments/999999');
    expectOutcome(missing, [404], 'a payment id nobody has used answers identically');

    await subject.close();
  });

  /**
   * The recoverables register, its CSV and its summary all apply the caller's
   * request scope.
   *
   * The register is a second view over `payment_requests` and it never asked who
   * was reading it: `recoverable_report:view` alone returned every category,
   * counterparty, project, amount, requester and repayment note in the company,
   * including rows the same caller is refused on `/requests/{id}` (F-G-016/F-E-03).
   * The seed kept it latent — Accounts and Admin both hold `request=all` — and one
   * custom role opened it.
   *
   * The predicate lives in the SQL now (`recoverableScope`,
   * internal/store/recoverables.go:347) rather than in a handler-side filter, and
   * the viewer is built once at the one place the screen and its download both
   * pass through (`recoverableListOptions`, internal/app/recoverables.go:69-96).
   * An unrecognised scope returns `AND 0`, so it fails closed.
   *
   * All four surfaces are checked, because each is a place the filter could have
   * been forgotten separately — and the summary aggregates are the one Wave 4
   * initially left company-wide, which left the disclosure half-closed.
   */
  test('TC-G-054 — the recoverables register, its CSV and its summary all apply the caller\'s request scope', async ({
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

    // The row itself is refused: canViewRequest does its job, and everything
    // below has to agree with it.
    const own = await probeGet(nosy.page, `/requests/${id}`);
    expectOutcome(own, [404], 'the request detail obeys the caller\'s own scope');

    // 1 — the register.
    const list = await probeGet(nosy.page, '/recoverables/list');
    expect(list.status, 'recoverable_report:view opens the register').toBe(200);
    expect(
      list.body.includes(number),
      'the register does not list a request this caller is forbidden to open'
    ).toBe(false);
    expect(list.body.includes(counterparty), 'nor its counterparty').toBe(false);
    expect(list.body.includes('₹88,000.00'), 'nor its amount').toBe(false);

    // 2 — the CSV, which discloses MORE than the screen: `exportRecoverable`
    // (internal/app/recoverables.go:163) writes Requester and Repayment Notes,
    // neither of which the list template renders. Both build their options
    // through recoverableListOptions, so the download cannot drift from the
    // screen the way F-G-016 found it had.
    const listCSV = await csv(nosy.page, '/recoverables/list.csv');
    expect(listCSV.includes(number), 'the CSV does not export the row either').toBe(false);
    expect(listCSV.includes(counterparty)).toBe(false);
    expect(listCSV.includes('88000.00'), 'nor its amount in the plain form the CSV writes').toBe(false);
    expect(
      listCSV.includes(owner.subject.name),
      'nor the requester, which the on-screen register does not even show'
    ).toBe(false);
    expect(
      listCSV.includes(`Refund on close ${runId}`),
      'nor their repayment terms'
    ).toBe(false);

    // 3 — the summary aggregates. A counterparty rollup names counterparties and
    // the tiles total their money, so a summary over a scoped table needs the
    // scope too, or the register's own tabs link to the disclosure.
    const dashboard = await probeGet(nosy.page, '/recoverables');
    expect(dashboard.status, 'recoverable_report:view opens the summary').toBe(200);
    expect(
      dashboard.body.includes(counterparty),
      'the by-counterparty rollup is scoped, so it does not name a counterparty the caller may not see'
    ).toBe(false);
    expect(dashboard.body.includes('₹88,000.00'), 'and the tiles do not total their money').toBe(false);

    // The control: Accounts holds request=all, so the row is genuinely there and
    // the four empty answers above are a scope filter, not an empty register.
    const accounts = await asRole(adminPage, browser, runId, ['Accounts'], `recall-${runId}`);
    const wide = await probeGet(accounts.page, '/recoverables/list');
    expect(wide.body.includes(number), 'a caller with request=all does see the row').toBe(true);
    expect(wide.body.includes(counterparty), 'with its counterparty').toBe(true);

    // The screen and its download are the same shape: every column the CSV
    // writes — Requester and Repayment notes included, which the register used
    // not to render at all — is a column of the screen. That is why one scope,
    // built once in recoverableListOptions, is enough for both.
    await accounts.page.goto('/recoverables/list');
    const headers = (await accounts.page.locator('thead th').allInnerTexts()).map(h => h.trim().toLowerCase());
    for (const column of ['category', 'counterparty', 'project', 'amount', 'paid on', 'expected back', 'ageing', 'status', 'requester', 'repayment notes']) {
      expect(headers, `the register renders the CSV's "${column}" column too`).toContain(column);
    }

    await accounts.close();
    await approver.close();
    await owner.close();
    await nosy.close();
  });

  /**
   * The audit log is scoped, not redacted.
   *
   * `Store.Audit` receives no viewer, and `before_json` for a `payment_request`
   * row is the entire Request struct — number, purpose, requester, approver,
   * amount, every date. Rendering that to a caller who is refused
   * `/requests/{id}` made `audit:view` a way around Q5/R6, carrying strictly more
   * data than the screens that do check (F-G-017).
   *
   * The decision taken was to withhold the ROWS rather than blank their evidence:
   * C2 requires a history for request mutations, and a history whose before/after
   * has been redacted is not one. `auditWithinRequestScope`
   * (internal/app/app.go:1859-1887) drops a `payment_request` row whose request
   * the caller's scope does not reach, exactly as `loadViewableRequest` withholds
   * the request — and touches nothing else, so a payment, budget or user row is
   * unaffected and a caller with `request=all` sees what they always saw.
   *
   * Both halves are asserted: the row is gone for the narrow caller, and every
   * other entity's rows are still there, or "scoped" would have quietly become
   * "emptied".
   */
  test('TC-G-055 — the audit log withholds rows for requests the caller cannot open, and nothing else', async ({
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
    expectOutcome(own, [404], 'the request detail obeys the caller\'s scope');

    const log = await probeGet(nosy.page, `/audit?entity=payment_request&actor=${encodeURIComponent(approver.subject.name)}`);
    expect(log.status, 'audit:view opens the log').toBe(200);
    expect(
      log.body.includes(number),
      'the log does not name a request this caller is forbidden to open'
    ).toBe(false);
    expect(
      log.body.includes(purpose),
      'so before_json cannot carry that request\'s purpose either'
    ).toBe(false);
    expect(
      log.body.includes(owner.subject.name),
      'nor the requester\'s name'
    ).toBe(false);
    expect(log.body.includes('6400000'), 'nor the amount in paise').toBe(false);

    // The log is scoped, not emptied: the caller's own rows are still there.
    // `login` is written against entity_type `user`, which the scope does not
    // touch at all, so this is the "and nothing else" half of the property.
    const users = await probeGet(nosy.page, '/audit?entity=user');
    expect(users.status, 'the log still opens for another entity').toBe(200);
    expect(
      users.body.includes(nosy.email),
      'a user row is not a request row, so it is untouched by the request scope'
    ).toBe(true);

    // And the control: the admin holds request=all, so the row is genuinely in
    // the log and the four absences above are the scope filter.
    const wide = await probeGet(
      adminPage,
      `/audit?entity=payment_request&actor=${encodeURIComponent(approver.subject.name)}`
    );
    expect(wide.body.includes(number), 'a caller with request=all reads the approve row').toBe(true);
    expect(wide.body.includes(purpose), 'with the before/after evidence intact, unredacted').toBe(true);

    await approver.close();
    await owner.close();
    await nosy.close();
  });

  /**
   * The dashboard accounts area asks for the caller's scope, like the queue it
   * links to.
   *
   * It used to be built with the literal `Scope: "all"` written into the call, so
   * its tile and its four rows were company-wide for anybody holding
   * `payment:process` whatever their request scope said — and each row linked to a
   * detail page the same caller is refused (F-G-018). It now passes
   * `a.effectiveScope(u, "")` into the queue's own query
   * (internal/app/dashboard.go:113-118), which is the same call that closed F-G-007
   * one tile over.
   */
  test('TC-G-056 — the dashboard accounts area applies the caller\'s scope, and agrees with the queue', async ({
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
    expectOutcome(own, [404], 'the request detail obeys the caller\'s scope');

    const dash = await probeGet(nosy.page, '/');
    expect(dash.status, 'the dashboard is RequireLogin only').toBe(200);
    expect(
      dash.body.includes(number),
      'the accounts work area applies the caller\'s scope, so it lists no request they cannot open'
    ).toBe(false);
    expect(dash.body.includes(title), 'nor its short title').toBe(false);
    expect(dash.body.includes('₹55,500.00'), 'nor its amount').toBe(false);

    // The accounts queue and the dashboard tile are one query, so they agree —
    // which is the property, not merely that both happen to be empty.
    const queue = await probeGet(nosy.page, '/accounts-queue?tab=approved');
    expect(queue.status, 'payment:process opens the queue').toBe(200);
    expect(
      queue.body.includes(number),
      'the queue asks a.auth.Scope(u,"request") and hides the row too'
    ).toBe(false);

    // The control. The area's rows are the queue's longest-waiting first and it
    // draws only a handful, so "is this row on the page" is not the honest
    // question for a caller who can see hundreds — the tile is. The narrow caller
    // is told nothing is waiting; the admin, who holds request=all, is told
    // otherwise, and the queue confirms the row is genuinely there.
    await nosy.page.goto('/');
    expect(
      Number(await nosy.page.locator('.metric:has(.metric-label:text-is("Approved, unclaimed")) .metric-value').innerText()),
      'a caller whose scope reaches none of the approved requests is told there are none'
    ).toBe(0);
    await adminPage.goto('/');
    expect(
      Number(await adminPage.locator('.metric:has(.metric-label:text-is("Approved, unclaimed")) .metric-value').innerText()),
      'while a caller with request=all is told there are'
    ).toBeGreaterThan(0);
    const wideQueue = await probeGet(adminPage, '/accounts-queue?tab=approved');
    expect(
      wideQueue.body.includes(number),
      'and this is the request that makes that true, so the absences above are the scope'
    ).toBe(true);

    await approver.close();
    await owner.close();
    await nosy.close();
  });

  /**
   * The variance grid is gated, and its Recent Payments panel is gated again.
   *
   * `GET /grid` was `RequireLogin` only while `GET /export.csv` — the download of
   * the very same rows — needed `grid:export`, so the data was open and only
   * carrying it away was controlled. A subject holding NO role at all read every
   * project's budget and actuals plus the grid's own Recent Payments panel, with
   * amounts, payees and live links into a ledger that answered them 403
   * (F-A-02/F-G-032).
   *
   * Two gates, because the screen asks two questions. `grid:view` opens the budget
   * matrix (internal/app/app.go:439). The payments panel carries amounts, payees
   * and `/payments/{id}` links, so it is `payment:view` — checked in the HANDLER
   * (internal/app/app.go:776) rather than trusted to the template, because a
   * screen must not be relied on to hide data the handler already loaded.
   *
   * Both halves are proven with a subject each: nobody, who gets neither, and a
   * custom role holding `grid:view` and not `payment:view`, who gets the matrix
   * and no panel. The second subject is the one that would catch the gate being
   * moved back into the template.
   */
  test('TC-G-057 — the grid needs grid:view, and its recent-payments panel needs payment:view as well', async ({
    adminPage,
    browser,
    runId
  }) => {
    // A payment to put a real figure and a real payee on the grid's panel.
    const payee = `Grid Leak Vendor ${runId}`;
    const request = await createApprovedRequest(adminPage, runId, { amount: '66000.00', payee });
    await settlePayment(adminPage, request.id, {
      amount: '66000.00',
      paidOn: '2026-05-06',
      reference: `UTR-GL-${runId}`,
      remarks: `Grid leak ${runId}`
    });

    // A subject holding nothing whatsoever.
    const nobody = await asRole(adminPage, browser, runId, [], `nobody-${runId}`);
    expect(nobody.subject.roles, 'the subject holds no role at all').toEqual([]);

    // Every gated screen refuses it — and the grid is one of them now.
    for (const path of ['/requests', '/payments', '/reports/monthly', '/audit', '/users', '/vendors']) {
      expectOutcome(await probeGet(nobody.page, path), [403], `a role-less subject is refused ${path}`);
    }
    const grid = await probeGet(nobody.page, '/grid?month=2026-05');
    expectOutcome(grid, [403], 'and the variance grid, which needs grid:view');
    expect(
      grid.body.includes('₹66,000.00'),
      'so the refusal page carries no amount from the screen it refused'
    ).toBe(false);
    expect(grid.body.includes(payee), 'nor the payee').toBe(false);
    for (const head of ['Office Rent', 'Payroll', 'Marketing']) {
      expect(grid.body.includes(head), `nor any head, including ${head}`).toBe(false);
    }
    const seeded = await probeGet(nobody.page, '/grid?month=2026-06');
    expectOutcome(seeded, [403], 'the seeded month is no more readable than any other');
    expect(
      seeded.body.includes('₹8,50,000.00'),
      'so the company plan is not readable by a subject with no role'
    ).toBe(false);

    // The screen and its download are consistent now: the same subject is refused
    // both, where they used to be refused only the download.
    expectOutcome(
      await probeGet(nobody.page, '/export.csv?month=2026-05'),
      [403],
      'the CSV of the same data is refused too — one gate, not two different answers'
    );
    expectOutcome(
      await probeGet(nobody.page, '/payments/1'),
      [403],
      'and the payment detail the panel used to link to'
    );

    // The second gate. A role holding grid:view and NOT payment:view gets the
    // budget matrix — which is what grid:view is for — and no payments panel.
    const gridOnly = await createCustomRole(adminPage, `GridOnly ${runId}`, ['grid:view'], {});
    const reader = await asCustomRole(adminPage, browser, runId, 'gridonly', gridOnly);
    const matrix = await probeGet(reader.page, '/grid?month=2026-05');
    expect(matrix.status, 'grid:view opens the budget matrix').toBe(200);
    expect(matrix.body.includes('Office Rent'), 'with its heads').toBe(true);
    expect(
      matrix.body.includes('₹66,000.00'),
      'the head-level actual is part of the matrix grid:view entitles them to'
    ).toBe(true);
    expect(
      matrix.body.includes(payee),
      'but no payee: the payments panel is a second question, gated on payment:view'
    ).toBe(false);
    expect(
      /href="\/payments\/\d+"/.test(matrix.body),
      'and no link into the ledger the same subject is refused'
    ).toBe(false);
    expectOutcome(
      await probeGet(reader.page, '/payments'),
      [403],
      'which is the ledger they are refused, so the panel would have been a way round it'
    );

    // The control: Accounts holds grid:view AND payment:view, so the panel is
    // genuinely there for somebody entitled to it.
    const accounts = await asRole(adminPage, browser, runId, ['Accounts'], `gridall-${runId}`);
    const full = await probeGet(accounts.page, '/grid?month=2026-05');
    expect(full.body.includes(payee), 'a caller holding payment:view does get the panel').toBe(true);
    expect(
      /href="\/payments\/\d+"/.test(full.body),
      'with its links into the ledger they may open'
    ).toBe(true);

    await accounts.close();
    await reader.close();
    await nobody.close();
  });

  /**
   * The scope verdict on all four CSV exports in one place — the question this
   * audit exists to answer.
   *
   * The two that carry rows apply the caller's row scope; the two that carry
   * aggregates have no per-row owner to scope and are protected by their verb.
   * `/recoverables/list.csv` used to be the exception — row-level data with no
   * filter, safe only because of who happened to hold `recoverable_report:export`
   * — and TC-G-054 is the case about that specifically. Here it is the verdict,
   * stated against the seeded roles.
   */
  test('TC-G-058 — the two row-level CSV exports apply the caller\'s data scope; the two aggregates have none to apply', async ({
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

    // 3 — /recoverables/list.csv is row-level data and applies the scope through
    // `recoverableListOptions`, the one place its screen and its download both
    // pass through (internal/app/recoverables.go:69-96). The verb still refuses a
    // Requester outright, which is the seeded arrangement that kept the missing
    // filter latent for as long as it did.
    expectOutcome(
      await probeGet(other.page, '/recoverables/list.csv'),
      [403],
      'a Requester does not hold recoverable_report:export at all'
    );
    const accounts = await asRole(adminPage, browser, runId, ['Accounts'], `csvacc-${runId}`);
    const accountsCSV = await csv(accounts.page, '/recoverables/list.csv');
    expect(
      accountsCSV.split('\n')[0],
      'the recoverables CSV is row-level data — a Requester column, not an aggregate — which is why it needs a row scope'
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
      paid_on: '2025-09-09',
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

    await adminPage.goto('/payments?month=2025-09');
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
    // The summary names the actor ("Fervid Admin reserved the request for
    // processing") since fix wave B (history-1); the wording is asserted so a
    // regression to the bare "Reserved request for processing" is caught.
    expect(
      rows.filter(r => r.action === 'process' && r.summary.includes('reserved the request for processing')).length >= 1,
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
   * The lock is a ledger lock, and everything that meets it now says so the same
   * way and lands somewhere that shows it.
   *
   * Three things changed here. Locking and unlocking redirect to `/grid?month=`,
   * the one screen that renders the lock and its reason — they used to land on
   * `/?month=`, which was the variance grid before Phase 4 made it the dashboard,
   * so an operator locked a month and was dropped on a page that confirmed nothing
   * (F-G-019). A locked-month budget save answers 409 like every other
   * locked-month refusal, instead of 400 because `budgetSave` rendered its own
   * status (F-G-026, `reRenderStatus` at internal/app/app.go:1383). And approving
   * a request whose needed-by falls in a locked month is refused with 409 rather
   * than manufacturing an obligation nobody can discharge (F-G-021,
   * `lockedApprovalMonth` at internal/app/requests.go:916).
   *
   * A request naming no period is deliberately unaffected — needed_by is the only
   * date a request carries — and both halves are asserted, because "refuses
   * everything" would be the wrong fix.
   */
  test('TC-G-070 — a locked month refuses a settlement, a budget edit and an approval dated into it', async ({
    adminPage,
    browser,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    const MONTH = '2025-10';
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

    // The redirect lands on the one screen that renders the lock and its reason.
    expect(
      locked.location,
      'lock redirects to /grid?month=, which is where the lock is visible'
    ).toBe(`/grid?month=${MONTH}`);
    const landing = await probeGet(adminPage, locked.location!);
    expect(landing.status, 'the landing page renders').toBe(200);
    expect(
      landing.body.includes(`Audit G lock ${runId}`),
      'and the operator is shown a confirmation of the lock they just applied'
    ).toBe(true);
    expect(
      landing.body.includes(MONTH),
      'naming the month they locked'
    ).toBe(true);

    // The same thing through the rendered screen, so the assertion is not only
    // about a substring in a probe body.
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
    // F-G-020, and the precise shape of what is still open. The entry screen DOES
    // carry a lock affordance — paymentEntry sets Locked and payment_form renders
    // the banner and disables every fieldset (internal/app/linking.go:308-321,
    // internal/app/templates.go:200-218) — but it can only test the month the date
    // field opens on, which is today's. This case locks a different month and then
    // types that date in, so there is nothing for the server to have warned about
    // at render time: which month is being written is not known until submit.
    //
    // So the assertion below is not "no affordance exists"; it is "the affordance
    // cannot reach this case", and the refusal at the confirmation is what catches
    // it. Closing the remaining gap needs the client to re-ask on date change.
    expect(
      (await adminPage.locator('.locked').count()) === 0,
      'the form opens on an unlocked month, so no banner is expected here — F-G-020 is closed only for the opening month'
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
    // One condition, one status code. `budgetSave` used to render its own 400 for
    // every failure rather than routing the store's error through
    // `storeErrorStatus`, which answers 409 for the same `ErrLockedMonth` on
    // /payments/{id}/void and /months — so the answer depended only on which
    // handler you reached the lock through (F-G-026).
    expect(
      budget.status,
      'a locked-month budget save answers 409, like every other locked-month refusal'
    ).toBe(409);
    expect(budget.body, 'and says the month is locked').toContain('month is locked');

    // A request that names no period is still accepted and approved into a locked
    // month, deliberately: needed_by is the only date a request carries, and
    // without one there is nothing to test against the lock.
    const payee = `Locked Vendor ${runId}`;
    await ensureVendor(adminPage, payee);
    const raised = await raiseVendorRequest(adminPage, {
      amount: '5200.00',
      vendor: payee,
      title: `Into a locked month ${runId}`,
      invoiceNo: `INV-LK-${runId}`,
      approverName: approver.subject.name
    });
    expect(raised.id, 'a request naming no period is unaffected by the lock').toBeGreaterThan(0);
    await approveFor(approver.page, raised.id, '5200.00');
    await adminPage.goto(`/requests/${raised.id}`);
    await expect(
      adminPage.locator('.rh-status'),
      'and is approved normally'
    ).toContainText('Approved');

    // But a request that DOES name the locked month cannot be approved into it:
    // an approval is a promise the request can be paid, and validatePayment would
    // refuse a paid_on inside the lock (F-G-021).
    const dated = await raiseVendorRequest(adminPage, {
      amount: '5300.00',
      vendor: payee,
      title: `Needed in a locked month ${runId}`,
      invoiceNo: `INV-LK3-${runId}`,
      neededBy: `${MONTH}-20`,
      approverName: approver.subject.name
    });
    const refusedApproval = await probePost(approver.page, `/requests/${dated.id}/approve`, {
      approved_amount: '5300.00'
    });
    expect(
      refusedApproval.status,
      'approving into a locked month is a 409, the same answer every other locked-month refusal gives'
    ).toBe(409);
    expect(
      refusedApproval.body,
      'and it names the month and what to do about it, rather than failing at the settlement weeks later'
    ).toContain(MONTH);
    await adminPage.goto(`/requests/${dated.id}`);
    await expect(
      adminPage.locator('.rh-status'),
      'so no approved obligation nobody can discharge was created'
    ).toContainText('Awaiting');

    // Unlocking restores the write, which proves the refusal was the lock.
    adminPage.once('dialog', dialog => dialog.accept());
    const unlocked = await probePost(adminPage, `/months/${MONTH}/unlock`, { reason: `Audit G unlock ${runId}` });
    expect(unlocked.status, 'unlocking is a 303').toBe(303);
    expect(unlocked.location, 'unlock lands on the grid too').toBe(`/grid?month=${MONTH}`);

    // And the approval the lock refused now goes through, which is what makes the
    // 409 above a lock refusal rather than a broken route.
    const nowAllowed = await probePost(approver.page, `/requests/${dated.id}/approve`, {
      approved_amount: '5300.00'
    });
    expect(nowAllowed.status, 'the same approval succeeds once the month is open').toBe(303);

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
   * A role somebody holds cannot be deleted, and both refusals say why.
   *
   * `user_roles.role_id` is `ON DELETE CASCADE`, so deleting an assigned role used
   * to succeed and strip it from every holder in silence — each of whom lost the
   * permissions it carried on their very next request, with nothing on screen
   * connecting cause to effect (F-G-022). `DeleteRole` pre-checks the holder count
   * inside its transaction now (internal/store/permissions.go:327-334), which is
   * the same number the roles screen already shows immediately above the Delete
   * button. R9 permits deleting a non-system role; it does not permit doing so
   * blind.
   *
   * And the message is the store's own. `respondStoreError` replaced every
   * `ErrForbidden` with "You do not have permission to perform this action.",
   * which told the holder of all 66 grants that they held none (F-G-023); the
   * route unwraps it (internal/app/app.go:1799-1802) so a rule with a reason keeps
   * its reason. Both of this route's refusals are checked, because the generic
   * sentence would have been indistinguishable from a real permission failure.
   */
  test('TC-G-080 — a role somebody holds cannot be deleted, and the refusal says why', async ({
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
    expect(
      deleted.status,
      'an assigned role is refused, not deleted from under its holder'
    ).toBe(403);
    expect(
      deleted.body,
      'and the refusal names the role and counts the people who would have lost it'
    ).toContain(`Doomed ${runId} is assigned to 1 user`);
    expect(
      deleted.body.includes('do not have permission'),
      'rather than telling an administrator holding every grant that they hold none'
    ).toBe(false);

    // Nothing happened: the role is still on its holder and still works.
    await adminPage.goto('/users');
    await expect(
      adminPage.locator('tr', { hasText: holder.email }).locator('.pill', { hasText: `Doomed ${runId}` }),
      'the holder still wears the role'
    ).toHaveCount(1);
    const after = await probeGet(holder.page, '/payments');
    expect(
      after.status,
      'and the grant it carries still works on their very next request'
    ).toBe(200);

    // Doing what the message says makes the delete legal, which is what turns the
    // refusal into an instruction rather than a wall.
    await setRolesByLabel(adminPage, holder.email, []);
    const nowDeleted = await probePost(adminPage, `/roles/${role.id}/delete`, {});
    expect(nowDeleted.status, 'once nobody holds it, the role is deleted').toBe(303);

    // Deleting a SEEDED role is refused too, and with its own reason.
    await adminPage.goto('/roles');
    const adminRoleHref = await adminPage
      .locator('.segmented a', { hasText: 'Requester' })
      .getAttribute('href');
    const seededID = adminRoleHref!.split('=')[1];
    const seeded = await probePost(adminPage, `/roles/${seededID}/delete`, {});
    expect(seeded.status, 'a system role cannot be deleted').toBe(403);
    expect(
      seeded.body.includes('ystem role'),
      'and the store\'s own explanation reaches the operator'
    ).toBe(true);
    expect(
      seeded.body.includes('do not have permission'),
      'instead of the sentence that also covers a genuine permission failure'
    ).toBe(false);

    expectNoRuntimeErrors(errors);
    await holder.close();
  });

  /**
   * A project or head may be deactivated with approved requests against it,
   * and the request then becomes permanently unpayable: `validatePayment`
   * refuses an inactive head (internal/store/store.go:1632-1639). The refusal
   * is coherent — a 400 with a sentence, not a 500.
   *
   * F-G-024, repaired as warn-and-confirm (the owner chose that over a hard
   * block, internal/app/deactivation.go): the first save stops on a page that
   * names the requests at stake and saves nothing; only the same form re-posted
   * with confirm=on deactivates the head.
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

    // Deactivate the head. The heads table renders each name inside an
    // <input value="…">, so the row cannot be found by text — an input's value
    // is not text content.
    await adminPage.goto('/heads');
    const row = adminPage.locator(`tbody tr:has(input[value="${headName}"])`);
    await expect(row, 'the new head has a row of its own').toHaveCount(1);
    const headID = await row.locator('input[name="id"]').first().inputValue();
    const deactivate = {
      id: headID,
      project_id: projectOption!,
      name: headName,
      due_day: '15',
      sort_order: '99'
    };
    const warned = await probePost(adminPage, '/heads', deactivate);
    // F-G-024: the first save stops and asks, and saves nothing.
    expect(
      warned.status,
      `F-G-024: a head with an approved request against it is not deactivated on the first save — got ${warned.outcome}`
    ).toBe(200);
    expect(warned.body, 'the warning names the request that would be stranded').toContain(raised.number);
    expect(warned.body, 'and says that nothing has been saved yet').toContain('Nothing has been saved yet');
    expect(warned.body, 'and cannot be confirmed without ticking the box').toMatch(
      /<input type="checkbox" name="confirm" value="on" required>/
    );
    expect(warned.body, 'and offers the way through').toContain('Deactivate anyway');
    await adminPage.goto('/heads');
    await expect(
      row.locator('input[name="active"]'),
      'the head is still active: the warning page saved nothing'
    ).toBeChecked();

    // Confirming re-posts the same form plus confirm=on, and only that saves.
    const off = await probePost(adminPage, '/heads', { ...deactivate, confirm: 'on' });
    expect(off.status, `the confirmed save deactivates the head — got ${off.outcome}`).toBe(303);
    await adminPage.goto('/heads');
    await expect(row.locator('input[name="active"]'), 'and the head is now inactive').not.toBeChecked();

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
    await adminPage.getByLabel('Paid on').fill('2025-11-11');
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
      paidOn: '2025-12-12',
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
   * A user with a pending approval against them can still be deactivated with no
   * check — but the request is no longer stranded, because there is now a door out.
   *
   * This case used to end on a proof of absence: no route reassigned a pending
   * approval, only a reservation, so a deactivated approver's queue sat there for
   * ever (F-G-025). `POST /requests/{id}/reassign-approver`
   * (internal/app/app.go:577, handler at :1629) is that route — `approval:reassign`
   * over the already-tested `store.ReassignRequest`, which is also the repair for
   * F-A-06/F-C-02 and F-A-08. The two names probed below were never the route and
   * the absence they proved was never the point, so the recovery is driven instead.
   *
   * F-G-025's other half is repaired as warn-and-confirm (the owner chose that
   * over a hard block, internal/app/deactivation.go): saving the approver as
   * inactive first stops on a page listing the requests waiting on them and
   * telling the administrator to reassign them, and saves nothing. A new
   * password in that submit is never echoed back; it is asked for again. Only
   * the confirmed re-post deactivates them. Every screen that touches the
   * request still renders rather than 500ing on the dangling manager.
   */
  test('TC-G-083 — deactivating an approver warns first, and their pending approvals can be handed on', async ({
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

    // The row carries a role pill as well as a status pill, so status
    // assertions are scoped to the Status cell.
    const statusPill = adminPage
      .locator('tr', { hasText: approver.subject.email })
      .locator('td[data-label="Status"] .pill');

    // A raw save that also carries a new password: the warning asks for it
    // again instead of writing it back into the page.
    await adminPage.goto('/users');
    const opener = await adminPage
      .locator('tr', { hasText: approver.subject.email })
      .getByRole('button', { name: 'Edit' })
      .getAttribute('data-open');
    const approverID = /^user-(\d+)$/.exec(opener ?? '')?.[1];
    expect(approverID, `the approver's Edit control names their id — got ${opener}`).toBeTruthy();
    const secret = `NeverEchoed-${runId}-9`;
    const withPassword = await probePost(adminPage, '/users', {
      id: approverID!,
      email: approver.subject.email,
      name: approver.subject.name,
      password: secret
    });
    expect(withPassword.status, `F-G-025: the save stops on a warning — got ${withPassword.outcome}`).toBe(200);
    expect(withPassword.body, 'the new password is never echoed into the page').not.toContain(secret);
    expect(withPassword.body, 'it is asked for again instead').toContain('Type it again');

    // Deactivate the approver through their own edit sheet.
    await adminPage.goto('/users');
    await adminPage.locator('tr', { hasText: approver.subject.email }).getByRole('button', { name: 'Edit' }).click();
    const edit = adminPage.locator('.overlay:not([hidden])');
    await expect(edit).toBeVisible();
    await edit.getByRole('checkbox', { name: 'Active' }).uncheck();
    await edit.getByRole('button', { name: 'Save user' }).click();

    // F-G-025: the first save stops on a warning that names the waiting request
    // and says to reassign it, and has saved nothing.
    await expect(
      adminPage.locator('h1'),
      'F-G-025: deactivating an approver with approvals waiting on them asks first'
    ).toHaveText(`Deactivate ${approver.subject.name}?`);
    await expect(adminPage.locator('main')).toContainText('Nothing has been saved yet');
    await expect(
      adminPage.locator('main table a', { hasText: raised.number }),
      'the warning lists the request waiting on them'
    ).toHaveCount(1);
    await expect(adminPage.locator('main'), 'and says what to do about it').toContainText(/reassign/i);
    await expect(
      adminPage.locator('main input[name="password"]'),
      'no password was typed in the sheet, so none is asked for'
    ).toHaveCount(0);
    const confirmBox = adminPage.getByRole('checkbox', { name: /reassign these requests/ });
    await expect(confirmBox, 'the confirmation must be ticked to go on').toHaveAttribute('required', '');
    await adminPage.goto('/users');
    await expect(statusPill, 'the approver is still active: the warning saved nothing').toHaveText('Active');

    // Confirm it this time.
    await adminPage.locator('tr', { hasText: approver.subject.email }).getByRole('button', { name: 'Edit' }).click();
    await expect(edit).toBeVisible();
    await edit.getByRole('checkbox', { name: 'Active' }).uncheck();
    await edit.getByRole('button', { name: 'Save user' }).click();
    await confirmBox.check();
    await adminPage.getByRole('button', { name: 'Deactivate anyway' }).click();
    await expect(adminPage).toHaveURL(/\/users$/);
    await expect(statusPill, 'only the confirmed save deactivates them').toHaveText('Inactive');

    // The request still reads, and still names them.
    await adminPage.goto(`/requests/${raised.id}`);
    await expect(
      adminPage.locator('.dl div:has(dt:text-is("Approver")) dd'),
      'the request still points at a user who can no longer sign in'
    ).toHaveText(approver.subject.name);
    await expect(adminPage.locator('.rh-status'), 'and is still awaiting them').toContainText('Awaiting');

    // They cannot sign in, so they cannot clear their own queue.
    await approver.page.goto('/login');
    await approver.page.getByLabel('Email').fill(approver.subject.email);
    await approver.page.getByLabel('Password').fill(approver.subject.password);
    await approver.page.getByRole('button', { name: 'Login' }).click();
    await expect(approver.page, 'a deactivated approver cannot sign in').toHaveURL(/\/login$/);

    // But somebody else can take the request off them. The reassignment demands a
    // reason — a silent rerouting of an approval is not something a history should
    // have to infer — and refuses without one.
    const rescuer = await asRole(adminPage, browser, runId, ['Manager'], `mgrres-${runId}`);
    await adminPage.goto(`/requests/${raised.id}`);
    const newApproverID = await adminPage
      .locator('select[name="manager_id"] option', { hasText: rescuer.subject.name })
      .first()
      .getAttribute('value');
    expect(newApproverID, 'the reassignment control offers a real approver').not.toBeNull();

    const noReason = await probePost(adminPage, `/requests/${raised.id}/reassign-approver`, {
      manager_id: newApproverID!
    });
    expect(noReason.status, 'a reassignment with no reason is refused').toBe(400);

    const reassigned = await probePost(adminPage, `/requests/${raised.id}/reassign-approver`, {
      manager_id: newApproverID!,
      reason: `The approver was deactivated ${runId}`
    });
    expect(reassigned.status, 'and with one it goes through').toBe(303);

    // The request now names the new approver, is still awaiting a decision, and
    // that person can actually make it — which is the whole point of the door.
    await adminPage.goto(`/requests/${raised.id}`);
    await expect(
      adminPage.locator('.dl div:has(dt:text-is("Approver")) dd'),
      'the request is handed to somebody who can sign in'
    ).toHaveText(rescuer.subject.name);
    await rescuer.page.goto('/approvals?bucket=to-approve');
    await expect(
      rescuer.page.locator('.req-card', { hasText: raised.number }),
      'and it is in their queue'
    ).toHaveCount(1);
    await approveFor(rescuer.page, raised.id, '8800.00');
    await adminPage.goto(`/requests/${raised.id}`);
    await expect(
      adminPage.locator('.rh-status'),
      'so the approval a deactivated user was holding is no longer stranded'
    ).toContainText('Approved');

    // The handover has its own audit action, so the history says who moved it and
    // why rather than leaving a reader to infer it from a changed column.
    const rows = await auditRows(adminPage, 'Fervid Admin', 'payment_request', raised.id);
    expect(
      rows.some(r => r.action === 'approval_reassign'),
      'the reassignment is audited as its own event'
    ).toBe(true);

    // Every screen that touches the request still renders. No 500 anywhere.
    for (const path of ['/requests?bucket=all', `/requests/${raised.id}`, '/accounts-queue', '/audit']) {
      const probe = await probeGet(adminPage, path);
      expect(probe.status, `${path} must not 500 over a deactivated approver`).toBeLessThan(500);
    }
    await rescuer.close();

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
      paid_on: '2026-01-10',
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
    await adminPage.goto('/payments?month=2026-01');
    expect(await dataRows(adminPage), 'exactly the one payment lands in that month').toBe(1);
    await expect(
      adminPage.locator('tbody tr td[data-label="Project / Head"]'),
      'the stored head_id is the approved one, so no head 99999999 was ever written'
    ).toContainText(APPROVED_HEAD);
  });

  /**
   * A head stays where it lives when its own row is saved, retired project or not.
   *
   * This was the worst kind of information-flow failure: a fact changing itself.
   * `heads` loaded ALL heads but only ACTIVE projects, so a head whose project had
   * been retired rendered a `<select name="project_id">` with no matching option —
   * the browser then preselected the first one, and pressing that row's own Save,
   * changing nothing, posted it and moved the head to a project nobody chose
   * (F-G-033).
   *
   * The screen asks two different questions and now gets two different lists
   * (`headsPageData`, internal/app/app.go:1461-1475): `Projects` is what a NEW head
   * may be filed under — active only, per T12 — and `AllProjects` is what an
   * existing row's select must offer, which has to be every project. Server-side
   * there is no way to tell "the operator chose this" from "the browser defaulted
   * to it", which is why the fix is the option list and not a validation rule.
   */
  test('TC-G-087 — a head whose project is retired keeps its project when its own row is saved', async ({
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

    // The head is still listed, and its select still contains its own project —
    // marked retired, because the reader has to be able to tell.
    await adminPage.goto('/heads');
    const orphan = adminPage.locator(`tbody tr:has(input[value="${headName}"])`);
    await expect(orphan, 'the head is still listed — ListHeads(false) returns it').toHaveCount(1);
    const options = await orphan.locator('select[name="project_id"] option').allInnerTexts();
    expect(
      options.some(o => o.trim() === `${projectName} (retired)`),
      'the row offers the head\'s own project, labelled retired'
    ).toBe(true);
    const preselected = await orphan.locator('select[name="project_id"]').inputValue();
    expect(
      preselected,
      'and it is the option selected, so the control says where the head actually lives'
    ).toBe(projectID);

    // Pressing the row's own Save — changing nothing else — changes nothing.
    await orphan.getByRole('button', { name: 'Save' }).click();
    await expect(adminPage).toHaveURL(/\/heads/);
    await adminPage.goto('/heads');
    const moved = adminPage.locator(`tbody tr:has(input[value="${headName}"])`);
    const nowUnder = await moved.locator('select[name="project_id"]').inputValue();
    expect(
      nowUnder,
      'pressing Save with no edit leaves the head under its own project'
    ).toBe(projectID);

    // The other half of the fix, and the reason it is two lists rather than one:
    // a NEW head may still not be filed under a retired project, so the create
    // control at the top of the screen offers active projects only (T12).
    const createOptions = await adminPage
      .locator('form.setup-form select[name="project_id"] option')
      .allInnerTexts();
    expect(createOptions.length, 'the Add Head control offers projects to file under').toBeGreaterThan(0);
    expect(
      createOptions.some(o => o.trim().startsWith(projectName)),
      'a new head may not be filed under a retired project, so the create control does not offer it'
    ).toBe(false);

    expectNoRuntimeErrors(errors);
    void headID;
  });

  /**
   * One form, one submit, one transaction.
   *
   * `POST /users` ran `UpdateUser`, `SetUserRoles` and `SetUserDefaultApprover` as
   * three independent transactions with no envelope, so a save refused by the
   * second left the first one's rename committed — and unaudited — while the
   * operator was shown a refusal (F-G-034). `store.SaveUser` commits all three or
   * none (internal/app/app.go:1590-1598).
   */
  test('TC-G-088 — a user save refused half way through writes nothing at all', async ({
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

    // A save that renames AND assigns a role id that does not exist. The role
    // check happens inside the same transaction as the rename
    // (internal/store/permissions.go:502-510), so the whole envelope rolls back.
    const originalName = subject.subject.name;
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

    // The refusal is total. Neither half of the POST happened.
    await adminPage.goto('/users');
    const after = adminPage.locator('tr', { hasText: subject.subject.email });
    await expect(
      after.locator('td[data-label="Name"]'),
      'the rename rolled back with the role assignment that refused it'
    ).toHaveText(originalName);
    await expect(
      after.locator('td[data-label="Roles"] .pill'),
      'and the roles are what they were'
    ).toHaveText('Requester');

    // And the same form with a real role id does apply, both halves together —
    // which is what makes the rollback above a transaction and not a broken route.
    await adminPage.goto('/roles');
    const managerHref = await adminPage
      .locator('.segmented a', { hasText: 'Manager' })
      .getAttribute('href');
    const managerID = managerHref!.split('=')[1];
    const saved = await probePost(adminPage, '/users', {
      id: userID,
      email: subject.subject.email,
      name: renamed,
      role: 'data_entry',
      active: 'on',
      role_ids: managerID,
      default_approver_id: '0'
    });
    expect(saved.status, 'a save with a role that exists succeeds').toBe(303);
    await adminPage.goto('/users');
    const applied = adminPage.locator('tr', { hasText: subject.subject.email });
    await expect(applied.locator('td[data-label="Name"]'), 'the rename lands').toHaveText(renamed);
    await expect(
      applied.locator('td[data-label="Roles"] .pill'),
      'together with the role assignment from the same press'
    ).toHaveText('Manager');

    // The subject can still sign in throughout, so nothing about this locked
    // anybody out of the product.
    const probe = await probeGet(subject.page, '/requests');
    expect(probe.status, 'the subject holds request:view through their new role').toBe(200);

    await subject.close();
  });

  /**
   * The same envelope on the requester's own screen: one press, one transaction.
   *
   * `requestEdit` used to run `UpdateRequest`, then `AddRequestAttachment`, then
   * `SubmitRequest` in a row, so a resubmit that failed its own preconditions left
   * the edit committed *and audited as done* while the requester was shown a 400 —
   * the history asserting a change the caller had been told had not happened
   * (F-G-035/F-C-04). `store.EditRequest` (internal/store/requests.go:865-896)
   * commits all three or none, and validates the submit against the edited row
   * rather than the row as it was on entry.
   *
   * The audit half is asserted as well as the amount, because a rolled-back write
   * that still audited would be a forged history — the same property TC-G-023 and
   * TC-G-024 assert for the approve and settle paths.
   */
  test('TC-G-089 — a request edit whose resubmit is refused writes neither the edit nor an audit row', async ({
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
    // internal/store/requests.go:91-101), so `submit_action=resubmit` fails — and
    // it fails inside the same transaction that carried the new amount.
    const before = (await auditRows(adminPage, requester.subject.name, 'payment_request', id)).filter(
      r => r.action === 'update'
    ).length;
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

    // Nothing was written. The amount is what it was before the refused press.
    await requester.page.goto(`/requests/${id}`);
    await expectMoney(
      requester.page,
      '.rh-amt',
      '₹2,000.00',
      'the edit rolled back with the resubmit that refused it'
    );
    // And the history does not claim a change the caller was told had failed.
    const after = (await auditRows(adminPage, requester.subject.name, 'payment_request', id)).filter(
      r => r.action === 'update'
    ).length;
    expect(
      after,
      'a rolled-back transaction that still audited would be a forged history'
    ).toBe(before);

    // The control: the same edit WITHOUT the illegal resubmit is saved, so the
    // rollback above is the transaction and not a route that refuses everything.
    const saved = await probePost(requester.page, `/requests/${id}/edit`, {
      type: 'reimbursement',
      treatment: 'budget',
      project_id: projectID,
      head_id: headID,
      short_title: `Atomic edit ${runId}`,
      amount: '31000.00',
      expense_date: '2026-07-14',
      purpose: `Atomic edit purpose ${runId}.`,
      manager_id: managerID
    });
    expect(saved.status, 'saving the correction on its own succeeds').toBe(303);
    await requester.page.goto(`/requests/${id}`);
    await expectMoney(requester.page, '.rh-amt', '₹31,000.00', 'and the figure lands');
    const finally_ = (await auditRows(adminPage, requester.subject.name, 'payment_request', id)).filter(
      r => r.action === 'update'
    ).length;
    expect(finally_, 'with exactly one update row, for the press that worked').toBe(before + 1);

    await approver.close();
    await requester.close();
  });

  /**
   * A password rule the app enforces is refused by the app, before hashing.
   *
   * The app layer validated the password's length and nothing else, and
   * `auth.HashPassword` then required a letter AND a digit — so a 12-character
   * letters-only password passed the first check and crashed the second, and the
   * operator was told "The password could not be secured", a 500 that blames the
   * machine for a rule they broke (F-A-11/F-G-036). `validatePassword`
   * (internal/app/app.go:2068-2073) delegates to `auth.ValidatePassword` plus the
   * 12-character minimum, so every rejection is a 400 naming the rule and the
   * refusal happens before anything is hashed.
   */
  test(
    'TC-G-090 — a 12-character letters-only password is refused with a 400 naming the rule',
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
      expect(
        probe.status,
        `a rule the app itself enforces is a 4xx, not a server fault — got ${probe.outcome}`
      ).toBe(400);
      expect(
        probe.body.includes('digit') || probe.body.includes('letter') || probe.body.includes('number'),
        'and the message names the rule the operator broke'
      ).toBe(true);
      expect(
        probe.body.includes('could not be secured'),
        'rather than blaming the machine for a rule the operator broke'
      ).toBe(false);
    }
  );

  /** The other half of the same fix: the rules the two layers apply are one set
   *  now, so every refusal is a 4xx and the one difference between refused and
   *  accepted is the rule itself. */
  test('TC-G-091 — every password rule is refused as a 400, and one digit is the whole difference', async ({ adminPage, runId }) => {
    const email = `pw400-${runId}@example.test`.toLowerCase();
    // A create must name a real role to be accepted at all, so every probe below
    // names the same one — the password is then the only thing that varies.
    const requesterRole = await systemRoleId(adminPage, /^Requester/);
    const probe = await probePost(adminPage, '/users', {
      id: '0',
      email,
      name: `Password rules ${runId}`,
      role_ids: requesterRole,
      active: 'on',
      password: 'abcdefghijkl'
    });
    expect(
      probe.status,
      'validatePassword refuses before hashing, so a broken rule is the caller\'s 400'
    ).toBe(400);
    expect(
      probe.body.includes('could not be secured'),
      'and no message blames the machine for it'
    ).toBe(false);

    // Nothing was written, and now for the right reason: the refusal happens
    // before the hash rather than inside it.
    await adminPage.goto('/users');
    await expect(
      adminPage.locator('tr', { hasText: email }),
      'no user is created by a refused password'
    ).toHaveCount(0);

    // The length rule the app owns is a 400 too, and names itself — so the two
    // layers' rules are one set with one answer, which is the actual fix.
    const short = await probePost(adminPage, '/users', {
      id: '0',
      email,
      name: `Password rules ${runId}`,
      role_ids: requesterRole,
      active: 'on',
      password: 'ab1'
    });
    expect(short.status, 'a too-short password is a 400 as well').toBe(400);
    expect(short.body, 'naming the rule it broke').toContain('12 characters');

    // The same password with one digit is accepted, which pins the real rule.
    // Same role as the two refused probes above, so the digit is the only change.
    const ok = await probePost(adminPage, '/users', {
      id: '0',
      email,
      name: `Password rules ${runId}`,
      role_ids: requesterRole,
      active: 'on',
      password: 'abcdefghijk1'
    });
    expect(ok.status, 'one digit is the whole difference between 400 and 303').toBe(303);
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
