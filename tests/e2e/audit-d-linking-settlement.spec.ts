/**
 * Audit D — payment linking, reservation and settlement.
 *
 * Everything from `approved` to `completed`: the two reservation entry points,
 * the atomicity of the reservation itself, release and reassign, the hold, the
 * pure settlement preview, the settlement outcomes, and the immutability of
 * what comes out the other end. Raising a request, the approval decision,
 * recoverables arithmetic and notification delivery belong to sibling audits.
 *
 * WHAT THIS FILE ADDS TO THE SHIPPED SUITE. `linking-settlement.spec.ts` walks
 * the happy journey once at 390 px and shows the loser of one race a screen
 * rather than a stack trace. `core-workflows.spec.ts` proves the ledger row and
 * the G13 ceiling. Neither asks what a person holding exactly one role may do,
 * neither drives the race more than once, and neither posts past the browser to
 * see what the store actually guarantees. This file does all three, and it
 * modifies neither of them.
 *
 * READING THE ANNOTATIONS. Five tests call `test.fail()` as their first
 * statement. Each names the finding it pins in
 * `docs/qa/results/findings-d-linking-settlement.md`. The assertion inside is
 * the assertion the product *should* pass — the day somebody fixes the defect
 * the test goes red with "passed unexpectedly", which is exactly the signal a
 * deleted assertion would have thrown away.
 *
 * THE SHARED CORPUS. Four approved requests, one of each shape the linkable set
 * has to distinguish (unclaimed · on hold · reserved by somebody else ·
 * completed), plus the single-role sessions the permission probes need. Every
 * one of them is expensive — an approved request needs a second signed-in person
 * to approve it (G8) — and every read-only test wants the same four, so they are
 * built once, lazily, and closed at the end. Tests that mutate build their own
 * request from their own `runId` and share nothing.
 *
 * STATUS VOCABULARY. `requestStatusText` (internal/app/requests.go:988) spells
 * `processing` as **"With Accounts"**, not "Processing". Every assertion below
 * reads the sentence a person sees rather than the enum value.
 */
import { type Browser, type BrowserContext, type Page } from '@playwright/test';
import {
  test,
  expect,
  admin as adminUser,
  approverFor,
  capturePageErrors,
  createApprovedRequest,
  createApproverUser,
  login,
  settlePayment
} from './fixtures';
import {
  asRole,
  createUserWithExactRoles,
  probeAnonymous,
  probeGet,
  probePost,
  expectOutcome,
  signIn,
  type Probe
} from './audit-support';

/** Every payment this file records lands in the current seeded month. */
const PAID_ON = '2026-07-20';
/** What `.rh-status .pill` reads while a request is reserved. */
const RESERVED = 'With Accounts';

interface Ref {
  number: string;
  id: number;
}

interface Session {
  page: Page;
  close(): Promise<void>;
}

// ---------------------------------------------------------------------------
// Shared helpers. Kept in this file deliberately: fixtures.ts and
// audit-support.ts are shipped and shared, and the brief forbids touching them.
// ---------------------------------------------------------------------------

/**
 * The seeded head ids, keyed by the label the request form shows.
 *
 * Read off the form rather than hardcoded: the tamper test needs a *different*
 * active head from the one a request was approved against, and an id guessed
 * from the seed order is a test that breaks the day somebody reorders it.
 */
async function headIds(page: Page): Promise<Map<string, string>> {
  await page.goto('/requests/new?type=vendor_invoice');
  const options = page.locator('#head option');
  const out = new Map<string, string>();
  for (let i = 0; i < (await options.count()); i++) {
    const option = options.nth(i);
    const value = (await option.getAttribute('value')) ?? '';
    const label = ((await option.textContent()) ?? '').trim();
    if (value) out.set(label, value);
  }
  return out;
}

/** The user id behind an email, read off the edit overlay's own DOM id. */
async function userIdByEmail(adminPage: Page, email: string): Promise<string> {
  await adminPage.goto('/users');
  const overlay = adminPage.locator(`.overlay:has(input[name="email"][value="${email}"])`);
  const domId = await overlay.getAttribute('id');
  expect(domId, `no user overlay for ${email}`).toBeTruthy();
  return domId!.replace('user-', '');
}

/**
 * Reserves through the queue the way a person does.
 *
 * The search box narrows the tab to one row first. The approved tab has no LIMIT
 * and this file leaves several requests parked in `approved` and `processing`,
 * so a bare `hasText` row locator would be reading a table that grows all run.
 */
async function takeFromQueue(page: Page, request: Ref) {
  await page.goto(`/accounts-queue?tab=approved&q=${encodeURIComponent(request.number)}`);
  const row = page.locator('tbody tr').filter({ hasText: request.number });
  await row.getByRole('button', { name: 'Take for processing' }).click();
  await expect(page, 'taking a request from the queue must land on its entry screen').toHaveURL(
    new RegExp(`/payments/new\\?request=${request.id}$`)
  );
}

/** Reserves at the HTTP layer, for setup that is not itself the subject. */
async function reserve(page: Page, id: number): Promise<Probe> {
  return probePost(page, `/requests/${id}/record-payment`);
}

/**
 * The hidden inputs the entry screen would post.
 *
 * A hand-rolled settlement has to carry head_id and vendor_payee or
 * validatePayment refuses it for the wrong reason, and reading them off the
 * screen is what keeps a probe honest: it posts exactly what the browser would.
 */
async function entryFields(page: Page, id: number) {
  await page.goto(`/payments/new?request=${id}`);
  const value = (name: string) => page.locator(`input[name="${name}"]`).inputValue();
  return {
    head_id: await value('head_id'),
    vendor_payee: await value('vendor_payee'),
    invoice_no: await value('invoice_no')
  };
}

/** Fills the entry form. Every field but the note is `required`. */
async function fillEntry(
  page: Page,
  opts: { amount: string; paidOn?: string; mode?: string; reference: string; note?: string }
) {
  await page.getByLabel('Amount actually paid').fill(opts.amount);
  await page.getByLabel('Paid on').fill(opts.paidOn ?? PAID_ON);
  await page.getByLabel('Payment mode').selectOption(opts.mode ?? 'bank_transfer');
  await page.getByLabel('Transaction / UTR reference').fill(opts.reference);
  if (opts.note) await page.getByLabel('Processing note').fill(opts.note);
}

/** Opens the settlement sheet from a filled entry form. */
async function openSheet(page: Page) {
  await page.getByRole('button', { name: /Payment settled/ }).click();
  const sheet = page.locator('.overlay .sheet');
  await expect(sheet, 'the settlement sheet is the only place the choice is made').toBeVisible();
  return sheet;
}

/** The request's status as a person reads it, off its own detail screen. */
async function statusPill(page: Page, id: number): Promise<string> {
  await page.goto(`/requests/${id}`);
  return (await page.locator('.rh-status .pill').first().innerText()).trim();
}

/** True when the queue's approved tab still offers to reserve this request. */
async function isTakeable(page: Page, request: Ref): Promise<boolean> {
  await page.goto(`/accounts-queue?tab=approved&q=${encodeURIComponent(request.number)}`);
  const row = page.locator('tbody tr').filter({ hasText: request.number });
  if ((await row.count()) === 0) return false;
  return (await row.getByRole('button', { name: 'Take for processing' }).count()) > 0;
}

/** The picker fragment, which is the same set the full picker page renders. */
async function pickerOptions(page: Page, query: string): Promise<string> {
  const probe = await probeGet(page, `/payments/new/options?q=${encodeURIComponent(query)}`);
  expect(probe.status, 'the picker fragment must answer for anybody who may create a payment').toBe(200);
  return probe.body;
}

/** Rows in a `t-cards` table body, counted from the response text. */
function bodyRows(html: string, firstColumn: string): number {
  return html.split(`data-label="${firstColumn}"`).length - 1;
}

/**
 * A POST carrying repeated field names, which `probePost`'s flat record cannot
 * express. `role_ids` and `perm` are both multi-valued, so building a custom
 * role or assigning exactly one role needs this.
 */
async function postRepeated(page: Page, path: string, pairs: Array<[string, string]>): Promise<Probe> {
  const cookie = (await page.context().cookies()).find(c => c.name === 'fervid_csrf');
  if (!cookie?.value) throw new Error('no fervid_csrf cookie — is this context signed in?');
  const body = new URLSearchParams();
  body.append('csrf', cookie.value);
  for (const [key, value] of pairs) body.append(key, value);
  const response = await page.request.post(path, {
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    data: body.toString(),
    maxRedirects: 0,
    failOnStatusCode: false
  });
  const status = response.status();
  const location = response.headers()['location'] ?? null;
  const text = status < 300 || status >= 400 ? await response.text().catch(() => '') : '';
  return { status, location, body: text, outcome: location ? `${status} → ${location}` : String(status) };
}

/**
 * Creates a role holding exactly the canonical grants named, and a signed-in
 * user holding exactly that role.
 *
 * No seeded role separates `payment:create` from `payment:settle` — Accounts and
 * Admin hold both — so the only way to ask "does the writer check the verb the
 * preview checks?" is to build the role the question is about. `perm` is the
 * roles screen's own per-grant field (its "Advanced" disclosure), so this is the
 * same write the screen performs.
 */
async function subjectWithGrants(
  adminPage: Page,
  browser: Browser,
  runId: string,
  prefix: string,
  grants: string[],
  scopes: Array<[string, string]> = [['request', 'all']]
) {
  const name = `${prefix} ${runId}`;
  const created = await probePost(adminPage, '/roles/new', { name, description: 'audit D probe role' });
  expect(created.status, 'the audit needs a custom role to separate two grants no seeded role separates').toBe(303);
  const roleID = new URL(created.location!, 'http://127.0.0.1').searchParams.get('role');
  expect(roleID, 'the new role id comes back in the redirect').toBeTruthy();

  const saved = await postRepeated(adminPage, '/roles', [
    ['role_id', roleID!],
    ['name', name],
    ['description', 'audit D probe role'],
    ...grants.map(g => ['perm', g] as [string, string]),
    ...scopes.map(([resource, scope]) => [`scope_${resource}`, scope] as [string, string])
  ]);
  expect(saved.status, 'and the grants are saved through the roles screen itself').toBe(303);

  const subject = await createUserWithExactRoles(adminPage, prefix, runId, []);
  const userID = await userIdByEmail(adminPage, subject.email);
  const assigned = await postRepeated(adminPage, '/users', [
    ['id', userID],
    ['email', subject.email],
    ['name', subject.name],
    ['role', 'data_entry'],
    ['active', 'on'],
    ['role_ids', roleID!]
  ]);
  expect(assigned.status, 'the subject holds exactly the probe role and nothing else').toBe(303);
  const session = await signIn(browser, subject, adminPage.viewportSize());
  return { subject, roleID: roleID!, ...session };
}

/**
 * Raises a recoverable request through the real form and has it approved.
 *
 * `createApprovedRequest` only builds budget-treatment vendor invoices, and the
 * question here is specifically about a recoverable whose category needs no
 * project — the one shape that reaches the settlement with no head at all.
 *
 * `type=employee_advance` is the vehicle: with a recoverable treatment its
 * validation asks for the advance reason plus the recoverable rules and nothing
 * else (`internal/store/requests.go:231-238`), and it forces the payee to the
 * requester so the payment still has somebody to pay. The store's fifth type,
 * `recoverable`, cannot be used — it is in `requestTypes`
 * (`internal/store/requests.go:78-81`) but has no card in `requestTypeOptions`
 * (`internal/app/requests.go:42-55`), so `/requests/new?type=recoverable` falls
 * back to the chooser and renders no form at all.
 */
async function createApprovedRecoverable(
  adminPage: Page,
  browser: Browser,
  runId: string,
  category: string,
  amount: string
): Promise<Ref> {
  const approver = await createApproverUser(adminPage, `rec-${category}-${runId}`);

  await adminPage.goto('/requests/new?type=employee_advance');
  await adminPage.getByLabel('Short title').fill(`Deposit ${category} ${runId}`);
  // The treatment radios swap #form-fields from the server; the recoverable
  // fieldset does not exist until this is checked.
  await adminPage.getByRole('radio', { name: /Refundable or recoverable/ }).check();
  await expect(adminPage.locator('#rcategory'), 'choosing recoverable reveals the category').toBeVisible();

  // Choosing a category swaps again. The reliable signal is the server-rendered
  // `selected` attribute: before the swap no option carries one, because the
  // first render had no category to select.
  await adminPage.locator('#rcategory').selectOption(category);
  await expect(
    adminPage.locator(`#rcategory option[value="${category}"]`),
    'and the swap comes back with the category selected server-side'
  ).toHaveAttribute('selected', '');
  await expect(
    adminPage.locator('#rproject'),
    `${category} requires no project, so the form offers no project — and therefore no head`
  ).toHaveCount(0);
  await expect(adminPage.locator('#head'), 'nor a head selector anywhere on the form').toHaveCount(0);

  await adminPage.getByLabel('Expected return date').fill('2027-03-31');
  if (category === 'icd' || category === 'security_deposit') {
    await adminPage.getByLabel('Counterparty company').fill(`Counterparty ${runId}`);
  }
  await adminPage.getByLabel('Repayment or refund terms').fill(`Refundable on completion ${runId}`);
  await adminPage.getByLabel('Amount').fill(amount);
  await adminPage.getByLabel('What the money is for').fill(`Site deposit ${runId}`);
  await adminPage.getByLabel('Purpose').fill(`Recoverable ${category} for ${runId}.`);
  await adminPage.getByLabel('Approver').selectOption({ label: approver.name });
  await adminPage.getByRole('button', { name: 'Submit request' }).click();
  await expect(adminPage, 'D1: one POST creates, numbers and submits it').toHaveURL(
    /\/requests\/\d+\/submitted$/
  );
  const id = Number(new URL(adminPage.url()).pathname.split('/')[2]);
  const number = (await adminPage.locator('.rh-no').first().innerText()).trim();

  const context = await browser.newContext({ viewport: adminPage.viewportSize() ?? undefined });
  try {
    const page = await context.newPage();
    await login(page, approver.email, approver.password);
    await page.goto(`/requests/${id}`);
    await page.getByRole('button', { name: /^Approve / }).click();
    const sheet = page.locator('#approve-sheet');
    await expect(sheet).toBeVisible();
    await sheet.getByLabel('Amount approved').fill(amount);
    await sheet.getByRole('button', { name: 'Approve request' }).click();
    await expect(page).toHaveURL(/\/approvals$/);
  } finally {
    await context.close();
  }
  return { number, id };
}

// ---------------------------------------------------------------------------
// The corpus
// ---------------------------------------------------------------------------

interface Corpus {
  runId: string;
  admin: Page;
  heads: Map<string, string>;
  /** Approved, unclaimed, not on hold — the only shape that may be reserved. */
  open: Ref;
  /** Approved and on hold: still `approved`, never takeable. */
  onHold: Ref;
  /** Reserved by `holder`, so `admin` is a non-holder for every probe. */
  taken: Ref;
  /** Settled in full, therefore `completed` and out of the linkable set. */
  done: Ref;
  holder: Session;
  manager: Session;
  requester: Session;
  norole: Session;
  payee: string;
  close(): Promise<void>;
}

let corpusCache: Corpus | null = null;

async function corpus(browser: Browser): Promise<Corpus> {
  if (corpusCache) return corpusCache;
  const runId = `auditd-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const ctx: BrowserContext = await browser.newContext();
  const admin = await ctx.newPage();
  await login(admin, adminUser.email, adminUser.password);

  const heads = await headIds(admin);
  const payee = `Acme Supplies ${runId}`;
  const open = await createApprovedRequest(admin, runId, { amount: '7431.00', payee });
  const onHold = await createApprovedRequest(admin, runId, { amount: '2100.00' });
  const taken = await createApprovedRequest(admin, runId, { amount: '3300.00' });
  const done = await createApprovedRequest(admin, runId, { amount: '4400.00' });

  const hold = await probePost(admin, `/requests/${onHold.id}/hold`, {
    reason: `Which site is this rent for? ${runId}`
  });
  expect(hold.status, 'the corpus needs an on-hold request').toBe(303);

  const holder = await asRole(admin, browser, runId, ['Accounts'], 'holder');
  const held = await reserve(holder.page, taken.id);
  expect(held.status, 'the corpus needs a request reserved by somebody other than the admin').toBe(303);

  await settlePayment(admin, done.id, {
    amount: '4400.00',
    paidOn: PAID_ON,
    reference: `UTR-DONE-${runId}`,
    remarks: `Corpus settled ${runId}`
  });

  const manager = await asRole(admin, browser, runId, ['Manager']);
  const requester = await asRole(admin, browser, runId, ['Requester']);
  const norole = await asRole(admin, browser, runId, []);

  corpusCache = {
    runId,
    admin,
    heads,
    open,
    onHold,
    taken,
    done,
    holder,
    manager,
    requester,
    norole,
    payee,
    close: async () => {
      await Promise.all([holder.close(), manager.close(), requester.close(), norole.close()]);
      await ctx.close();
    }
  };
  return corpusCache;
}

test.afterAll(async () => {
  if (corpusCache) {
    await corpusCache.close();
    corpusCache = null;
  }
});

/**
 * 30 s is not enough here, and the reason is structural rather than slow code.
 *
 * Every approved request in this file is raised and approved through the real
 * screens by two different people (G8 forbids self-approval), which is a dozen
 * navigations; several tests build three of them, and whichever test happens to
 * build the shared corpus builds four plus five signed-in subjects. The seeded
 * database also grows all run — /users and the queue both get longer — so the
 * same work costs more by the end than at the start.
 */
test.beforeEach(async ({}, testInfo) => {
  testInfo.setTimeout(180_000);
});

// ===========================================================================
// A — the two reservation entry points, the linkable set and its search
// ===========================================================================

test.describe('D · reservation entry points and the linkable set', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'Read-only list assertions run once, on chromium.');
  });

  test('TC-D-001 — the queue reserves with a submit button in a POST form, never a link', async ({ browser }) => {
    const c = await corpus(browser);
    await c.admin.goto(`/accounts-queue?tab=approved&q=${encodeURIComponent(c.open.number)}`);
    const row = c.admin.locator('tbody tr').filter({ hasText: c.open.number });
    const action = row.locator('td[data-label="Action"]');

    await expect(
      action.getByRole('button', { name: 'Take for processing' }),
      'reserving is a mutation, so the control must be a submit button'
    ).toHaveCount(1);
    await expect(
      action.getByRole('link', { name: /Take for processing/ }),
      'a GET link that reserves would let a crawler take a request'
    ).toHaveCount(0);
    const form = action.locator('form');
    await expect(form).toHaveAttribute('method', 'post');
    await expect(form).toHaveAttribute('action', `/requests/${c.open.id}/record-payment`);
    await expect(form.locator('input[name="csrf"]'), 'the reservation POST carries a CSRF token').toHaveCount(1);
  });

  test('TC-D-002 — S1 entry point one: the queue hands the reservation to the entry screen', async ({ browser }) => {
    const c = await corpus(browser);
    const request = await createApprovedRequest(c.admin, c.runId, { amount: '1250.00' });
    await takeFromQueue(c.admin, request);
    await expect(c.admin.locator('.reserve-bar'), 'the winner is told the reservation is theirs').toContainText(
      'Reserved by you'
    );
    await expect(
      c.admin.locator('.reserve-bar'),
      'and that it is exclusive — that is the whole point of reserving'
    ).toContainText('nobody else can process this request');
  });

  test('TC-D-003 — S1 entry point two: the payment picker reserves the same way', async ({ browser }) => {
    const c = await corpus(browser);
    const request = await createApprovedRequest(c.admin, c.runId, { amount: '1350.00' });
    await c.admin.goto(`/payments/new?q=${encodeURIComponent(request.number)}`);
    await expect(c.admin.locator('h1')).toHaveText('Which approved request is this for?');
    const option = c.admin.locator('#picker-list form', { hasText: request.number });
    await expect(option.locator('button.co'), 'a takeable picker row is a submit button').toHaveCount(1);
    await expect(option).toHaveAttribute('action', `/requests/${request.id}/record-payment`);
    await option.locator('button.co').click();
    await expect(c.admin, 'the picker is a reservation entry point in its own right').toHaveURL(
      new RegExp(`/payments/new\\?request=${request.id}$`)
    );
    await expect(c.admin.locator('.reserve-bar')).toContainText('Reserved by you');
  });

  test('TC-D-004 — S3: only approved, unclaimed, not-on-hold requests are takeable', async ({ browser }) => {
    const c = await corpus(browser);
    const takeable = await pickerOptions(c.admin, c.open.number);
    expect(takeable, 'an approved unclaimed request is offered as a POST form').toContain(
      `action="/requests/${c.open.id}/record-payment"`
    );

    for (const [label, ref] of [
      ['on hold', c.onHold],
      ['reserved by somebody else', c.taken]
    ] as const) {
      const body = await pickerOptions(c.admin, ref.number);
      expect(body, `a request ${label} must never be offered as takeable`).not.toContain(
        `action="/requests/${ref.id}/record-payment"`
      );
      expect(body, `a request ${label} is still worth reading, so it degrades to a read-only row`).toContain(
        `class="co is-taken" href="/requests/${ref.id}"`
      );
    }
  });

  test('TC-D-005 — an on-hold request is read-only in the picker and states its hold reason', async ({ browser }) => {
    const c = await corpus(browser);
    const body = await pickerOptions(c.admin, c.onHold.number);
    expect(body, 'the accountant is told why it is paused, not merely that it is').toContain('On hold —');
    expect(body).toContain(`Which site is this rent for? ${c.runId}`);
    expect(
      await isTakeable(c.admin, c.onHold),
      'L7: on hold leaves the status approved, and the availability test drops it out of the takeable set'
    ).toBe(false);
  });

  test('TC-D-006 — a reserved request names its holder and offers no way to take it', async ({ browser }) => {
    const c = await corpus(browser);
    const body = await pickerOptions(c.admin, c.taken.number);
    expect(body, 'the reader is told who has it').toContain('Reserved by');
    expect(body, 'and that it is not theirs to take').toContain('you cannot take this one');
  });

  test('TC-D-007 — L10: a completed request drops out of the linkable list', async ({ browser }) => {
    const c = await corpus(browser);
    const body = await pickerOptions(c.admin, c.done.number);
    expect(body, 'a settled request is finished; offering it again would create a second payment').not.toContain(
      c.done.number
    );
    expect(body).toContain('No approved requests match');
    expect(await isTakeable(c.admin, c.done), 'nor is it takeable from the queue').toBe(false);

    await c.admin.goto(`/accounts-queue?tab=paid&q=${encodeURIComponent(c.done.number)}`);
    const paidRow = c.admin.locator('tbody tr').filter({ hasText: c.done.number });
    await expect(paidRow, 'it moves to the Paid tab rather than disappearing from the product').toHaveCount(1);
    await expect(paidRow.locator('td[data-label="Status"] .pill')).toHaveText('Completed');
  });

  test('TC-D-008/009 — S4: the picker search finds a request by number and by requester', async ({ browser }) => {
    const c = await corpus(browser);
    expect(await pickerOptions(c.admin, c.open.number), 'by request number').toContain(c.open.number);
    expect(await pickerOptions(c.admin, 'Fervid Admin'), 'by the requester who raised it').toContain(c.open.number);
    expect(
      await pickerOptions(c.admin, `no-such-requester-${c.runId}`),
      'and a search that matches nothing says so rather than listing everything'
    ).toContain('No approved requests match');
  });

  test('TC-D-010 — S4: the picker search finds a request by payee', async ({ browser }) => {
    const c = await corpus(browser);
    const body = await pickerOptions(c.admin, c.payee);
    expect(body, 'typing a vendor name must find the row that shows that name').toContain(c.open.number);
    expect(body, 'and the row prints the payee it was found by').toContain(c.payee);
  });

  test('TC-D-011/012 — S4: the picker search finds a request by project and by head', async ({ browser }) => {
    const c = await corpus(browser);
    expect(await pickerOptions(c.admin, 'Operations'), 'by project').toContain(c.open.number);
    expect(await pickerOptions(c.admin, 'Office Rent'), 'by head').toContain(c.open.number);
    expect(
      await pickerOptions(c.admin, 'Staff Welfare'),
      'a head the request is not charged to must exclude it — otherwise the search is not filtering'
    ).not.toContain(c.open.number);
  });

  test('TC-D-013 — S4: the amount search matches the amount a person can see', async ({ browser }) => {
    // FINDING F-D-04. The amount column is searched as `CAST(r.amount AS TEXT)`,
    // which is paise, so the figure a person can actually read — ₹7,431.00 —
    // never matches. The rupee digits match only by substring luck.
    test.fail();
    const c = await corpus(browser);
    expect(
      await pickerOptions(c.admin, '743100'),
      'the stored paise integer matches, which is what the query actually compares'
    ).toContain(c.open.number);
    expect(
      await pickerOptions(c.admin, '7,431.00'),
      'the amount every screen displays must be searchable: the hint promises "or amount"'
    ).toContain(c.open.number);
  });

  test('TC-D-014 — the queue search narrows the rows without moving the counts', async ({ browser }) => {
    const c = await corpus(browser);
    await c.admin.goto('/accounts-queue?tab=approved');
    const metrics = c.admin.locator('.metric-strip .metric .metric-value');
    await expect(metrics, 'the queue carries four metric tiles').toHaveCount(4);
    const before = await metrics.allInnerTexts();
    const unfiltered = await c.admin.locator('tbody tr').count();
    expect(unfiltered, 'the search has to have something to narrow for this to mean anything').toBeGreaterThan(1);

    // Driven through the query the form posts rather than the form itself: the
    // queue's only search control is the mobile one, and it is display:none at
    // this width (F-D-07, pinned by TC-D-094).
    await c.admin.goto(`/accounts-queue?tab=approved&q=${encodeURIComponent(c.open.number)}`);
    await expect(c.admin.locator('tbody tr')).toHaveCount(1);
    expect(
      await metrics.allInnerTexts(),
      'the counts span the whole scope, so searching must never move them'
    ).toEqual(before);
  });

  test('TC-D-094 — the queue offers a search control at desktop width', async ({ browser }) => {
    // FINDING F-D-07. accounts_queue renders only `<form class="m-filters">`,
    // which fervid-ds.css:3853 sets to display:none outside the 860 px media
    // query. Every other searchable list — requests, approvals, vendors,
    // recoverables — ships a `.toolbar` beside its `.m-filters`; the Accounts
    // queue is the one that does not, so its search box exists only on a phone.
    test.fail();
    const c = await corpus(browser);
    expect(c.admin.viewportSize()!.width, 'this case is about desktop width').toBeGreaterThan(860);
    await c.admin.goto('/accounts-queue?tab=approved');
    await expect(
      c.admin.locator('#queue-q'),
      'S4: an accountant with a queue of hundreds needs the search on the screen they work from'
    ).toBeVisible();
  });

  test('TC-D-015 — the queue and the picker are gated on the verbs they need', async ({ browser, baseURL }) => {
    const c = await corpus(browser);
    expect(baseURL, 'the anonymous probe needs an absolute base').toBeTruthy();

    for (const [who, session] of [
      ['a Manager', c.manager],
      ['a Requester', c.requester],
      ['a user with no role at all', c.norole]
    ] as const) {
      expectOutcome(await probeGet(session.page, '/accounts-queue'), [403], `${who} holds no payment:process`);
      expectOutcome(await probeGet(session.page, '/payments/new'), [403], `${who} holds no payment:create`);
      expectOutcome(
        await probeGet(session.page, '/payments/new/options'),
        [403],
        `${who} cannot read the linkable set through the fragment either`
      );
    }
    expectOutcome(await probeGet(c.holder.page, '/accounts-queue'), [200], 'Accounts works the queue');
    expectOutcome(await probeGet(c.holder.page, '/payments/new'), [200], 'Accounts may record a payment');

    for (const path of ['/accounts-queue', '/payments/new', '/payments/new/options']) {
      const anon = await probeAnonymous(browser, path, baseURL!);
      expect(anon.status, `${path} must not answer an anonymous caller`).toBeGreaterThanOrEqual(300);
      expect(anon.status).toBeLessThan(400);
      expect(anon.location, `${path} sends an anonymous caller to the login screen`).toContain('/login');
    }
  });

  test('TC-D-016 — an unknown queue tab is refused rather than silently defaulted', async ({ browser }) => {
    const c = await corpus(browser);
    const probe = await probeGet(c.admin, '/accounts-queue?tab=everything');
    expect(probe.status, 'a tab the store cannot build a set for is a bad request').toBe(400);
    expect(probe.body).toContain('That queue tab does not exist.');
  });
});

// ===========================================================================
// B — atomicity (S2, S5). The centrepiece.
// ===========================================================================

test.describe('D · the reservation is atomic', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'The race is about the store, not the engine.');
  });

  test('TC-D-017 — two accountants click at once: one wins, the other meets the conflict screen', async ({
    adminPage,
    secondPage,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '5200.00' });
    for (const page of [adminPage, secondPage]) {
      await page.goto(`/accounts-queue?tab=approved&q=${encodeURIComponent(request.number)}`);
      await expect(page.locator('tbody tr').filter({ hasText: request.number })).toHaveCount(1);
    }
    const click = (page: Page) =>
      page
        .locator('tbody tr')
        .filter({ hasText: request.number })
        .getByRole('button', { name: 'Take for processing' })
        .click();

    await Promise.all([click(adminPage), click(secondPage)]);

    const entry = new RegExp(`/payments/new\\?request=${request.id}$`);
    const wonByAdmin = entry.test(adminPage.url());
    const wonBySecond = entry.test(secondPage.url());
    expect(
      [wonByAdmin, wonBySecond].filter(Boolean).length,
      'S2/S5: exactly one of two simultaneous reservations may succeed'
    ).toBe(1);

    const winner = wonByAdmin ? adminPage : secondPage;
    const loser = wonByAdmin ? secondPage : adminPage;
    await expect(winner.locator('.reserve-bar')).toContainText('Reserved by you');
    await expect(loser.locator('.banner.bad'), 'G15: the loser gets a screen, not a stack trace').toContainText(
      'took this request before you'
    );
    await expect(loser.locator('.banner.bad'), 'and is told in as many words that nothing was written').toContainText(
      'no payment was created'
    );
    await expect(loser.locator('.error-code'), 'the error page is what this deliberately is not').toHaveCount(0);
    await expect(loser.locator('.reserve-bar'), 'and the loser is never told the reservation is theirs').toHaveCount(0);
  });

  test('TC-D-018/019 — four concurrent HTTP races: one 303, one 409, and one reservation recorded', async ({
    adminPage,
    secondPage,
    runId
  }) => {
    for (let round = 1; round <= 4; round++) {
      const request = await createApprovedRequest(adminPage, runId, { amount: `${1000 + round}.00` });
      const [a, b] = await Promise.all([reserve(adminPage, request.id), reserve(secondPage, request.id)]);
      const outcomes = [a.status, b.status].sort((x, y) => x - y);
      expect(outcomes, `round ${round}: the conditional UPDATE lets exactly one caller through`).toEqual([303, 409]);

      const won = a.status === 303 ? a : b;
      const lost = a.status === 303 ? b : a;
      expect(won.location, `round ${round}: the winner is sent to the entry screen`).toBe(
        `/payments/new?request=${request.id}`
      );
      expect(lost.body, `round ${round}: the loser is told who took it`).toContain('took this request before you');
      expect(lost.body, `round ${round}: and that no payment exists`).toContain('no payment was created');

      // The database's own record of who holds it. audit_log is written inside
      // the same transaction as the conditional UPDATE, so one row is one
      // reservation and processing_by can only have moved once.
      const audit = await probeGet(adminPage, `/audit?entity=payment_request&id=${request.id}&action=process`);
      expect(audit.status).toBe(200);
      expect(
        bodyRows(audit.body, 'When'),
        `round ${round}: processing_by moved exactly once, so the trail carries exactly one line`
      ).toBe(1);

      await adminPage.goto(`/accounts-queue?tab=processing&q=${encodeURIComponent(request.number)}`);
      const row = adminPage.locator('tbody tr').filter({ hasText: request.number });
      await expect(row, `round ${round}: the reserved request shows once in Processing`).toHaveCount(1);
      const pill = (await row.locator('.pill.processing').innerText())
        .replace(/^Reserved by\s*/, '')
        .replace(/\s*·.*$/, '')
        .trim();
      expect(pill, `round ${round}: the queue names a holder`).not.toBe('');
      const holderName = pill === 'you' ? 'Fervid Admin' : pill;
      expect(
        audit.body.includes(holderName),
        `round ${round}: the audit's single reservation names the same person the queue does (${holderName})`
      ).toBe(true);
    }
  });

  test('TC-D-020 — reserving a completed request is refused', async ({ browser }) => {
    const c = await corpus(browser);
    const probe = await reserve(c.admin, c.done.id);
    expect(probe.status, 'a settled request has left the reservable set for good').toBe(409);
    // The wording is wrong here too — nobody holds a completed request. See
    // F-D-02; TC-D-023 is the annotated test that pins it.
    expect(probe.body, 'and it is refused through the conflict screen').toContain('took this request before you');
  });

  test('TC-D-021 — the reservation POST refuses a missing and a forged CSRF token', async ({ browser }) => {
    const c = await corpus(browser);
    const request = await createApprovedRequest(c.admin, c.runId, { amount: '1450.00' });
    for (const csrf of ['omit', 'bogus'] as const) {
      const probe = await probePost(c.admin, `/requests/${request.id}/record-payment`, {}, { csrf });
      expect(probe.status, `a ${csrf} CSRF token must not reserve anything`).toBe(403);
    }
    expect(await isTakeable(c.admin, request), 'and the request is still unclaimed afterwards').toBe(true);
  });

  test('TC-D-022 — a Manager cannot reserve at all: reservation:reserve is not theirs', async ({ browser }) => {
    const c = await corpus(browser);
    expectOutcome(
      await probePost(c.manager.page, `/requests/${c.open.id}/record-payment`),
      [403],
      'a Manager holds no reservation verb'
    );
    expectOutcome(
      await probePost(c.requester.page, `/requests/${c.open.id}/record-payment`),
      [403],
      'nor does a Requester'
    );
    expect(await isTakeable(c.admin, c.open), 'and the request is untouched').toBe(true);
  });

  test('TC-D-023 — reserving an on-hold request says it is on hold, not that somebody took it', async ({
    browser
  }) => {
    // FINDING F-D-02. requestRecordPayment funnels every ErrForbidden from
    // ReserveRequest into reservationConflict, which is written for one cause —
    // somebody else got there first. An on-hold request has nobody holding it,
    // so the screen invents "Someone else" and reports a reservation that does
    // not exist.
    test.fail();
    const c = await corpus(browser);
    const probe = await reserve(c.admin, c.onHold.id);
    expect(probe.status, 'an on-hold request is not available to process').toBe(409);
    expect(
      probe.body.toLowerCase().includes('on hold'),
      'the accountant has to be told the real reason payment is blocked'
    ).toBe(true);
    expect(probe.body, 'and must not be told a colleague holds a reservation that does not exist').not.toContain(
      'Someone else took this request before you'
    );
  });
});

// ===========================================================================
// C — no auto-release, release and reassign (S6, S7, G11, G12)
// ===========================================================================

test.describe('D · giving a reservation up', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'Reservation handover runs once, on chromium.');
  });

  test('TC-D-024 — S6: nothing expires a reservation', async ({ adminPage, secondPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '6100.00' });
    await takeFromQueue(adminPage, request);

    // Everything that happens around a reservation, none of which may end it:
    // a reload, a lost race, a refused settlement, and a walk away and back.
    await adminPage.reload();
    await expect(adminPage.locator('.reserve-bar')).toContainText('Reserved by you');
    expect((await reserve(secondPage, request.id)).status, 'a second caller is refused').toBe(409);
    const over = await probePost(adminPage, '/payments', {
      request_id: String(request.id),
      amount: '9999.00',
      paid_on: PAID_ON,
      payment_mode: 'bank_transfer',
      reference_no: `UTR-${request.id}`,
      settlement: 'settled',
      ...(await entryFields(adminPage, request.id))
    });
    expect(over.status, 'G13: over-paying is refused').toBe(400);

    await adminPage.goto('/');
    await adminPage.goto('/accounts-queue?tab=processing');
    await adminPage.goto(`/payments/new?request=${request.id}`);
    await expect(
      adminPage.locator('.reserve-bar'),
      'S6: there is no auto-release, so the reservation is still the same person’s'
    ).toContainText('Reserved by you');
    expect(await isTakeable(adminPage, request), 'and nobody else may take it').toBe(false);
  });

  test('TC-D-025 — the release screen belongs to the holder and states the reservation', async ({
    adminPage,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '2600.00' });
    await takeFromQueue(adminPage, request);
    await adminPage.goto(`/requests/${request.id}/reservation`);
    await expect(adminPage.locator('h1')).toHaveText('Release or reassign this request');
    await expect(adminPage.locator('.reserve-bar')).toContainText('Reserved by you');
    await expect(adminPage.getByLabel('Reason'), 'G12: a release is explained to the requester').toBeVisible();
    await expect(
      adminPage.getByLabel('I confirm no payment has been initiated for this request'),
      'S7: the one thing a reservation protects against is two people paying the same invoice'
    ).toBeVisible();
    await expect(adminPage.getByRole('button', { name: 'Release reservation' })).toBeVisible();
  });

  test('TC-D-026/027/028 — a release needs the confirmation and a reason, and then it works', async ({
    adminPage,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '2700.00' });
    await reserve(adminPage, request.id);

    const noConfirm = await probePost(adminPage, `/requests/${request.id}/release`, { reason: 'Wrong invoice' });
    expect(noConfirm.status, 'S7: an unconfirmed release must not release').toBe(400);
    expect(noConfirm.body).toContain('confirm that no payment was initiated');
    expect(await isTakeable(adminPage, request), 'and the reservation survives the refusal').toBe(false);

    const noReason = await probePost(adminPage, `/requests/${request.id}/release`, { confirm: 'on' });
    expect(noReason.status, 'G12: a reason is required').toBe(400);
    expect(noReason.body).toContain('a reason is required');
    expect(await isTakeable(adminPage, request)).toBe(false);

    const whitespace = await probePost(adminPage, `/requests/${request.id}/release`, {
      confirm: 'on',
      reason: '   '
    });
    expect(whitespace.status, 'a whitespace-only reason is no reason').toBe(400);
    expect(await isTakeable(adminPage, request)).toBe(false);

    const released = await probePost(adminPage, `/requests/${request.id}/release`, {
      confirm: 'on',
      reason: `Wrong invoice attached ${runId}`
    });
    expect(released.status, 'with both, the holder releases it').toBe(303);
    expect(released.location, 'and lands back on the queue').toBe('/accounts-queue');
    expect(await isTakeable(adminPage, request), 'S6: the request is back in the open queue').toBe(true);
    expect(await statusPill(adminPage, request.id), 'and back to approved').toContain('Approved');
  });

  test('TC-D-029/030 — a second accountant can neither open nor act on somebody else’s reservation', async ({
    browser
  }) => {
    const c = await corpus(browser);
    const other = await asRole(c.admin, browser, `${c.runId}b`, ['Accounts'], 'other');
    try {
      const screen = await probeGet(other.page, `/requests/${c.taken.id}/reservation`);
      expect(screen.status, 'holding reservation:release is not permission to release this one').toBe(403);
      expect(screen.body).toContain('Only the person holding this reservation can release it.');

      const post = await probePost(other.page, `/requests/${c.taken.id}/release`, {
        confirm: 'on',
        reason: 'I want it'
      });
      expect(post.status, 'S6: only the assignee, or a caller who may reassign, may release').toBe(403);
      const stillHeld = await probeGet(c.holder.page, `/payments/new?request=${c.taken.id}`);
      expect(stillHeld.status, 'and the holder still holds it').toBe(200);
    } finally {
      await other.close();
    }
  });

  test('TC-D-031 — a Manager holds neither reservation verb', async ({ browser }) => {
    const c = await corpus(browser);
    expectOutcome(
      await probePost(c.manager.page, `/requests/${c.taken.id}/release`, { confirm: 'on', reason: 'no' }),
      [403],
      'release is gated on reservation:release, which a Manager does not hold'
    );
    expectOutcome(
      await probePost(c.manager.page, `/requests/${c.taken.id}/reassign`, {
        confirm: 'on',
        reason: 'no',
        to_user_id: '1'
      }),
      [403],
      'and reassign is gated on reservation:reassign, which only an Admin holds'
    );
  });

  test('TC-D-032 — an Accounts user cannot reassign: reservation:reassign is Admin-only', async ({ browser }) => {
    const c = await corpus(browser);
    expectOutcome(
      await probePost(c.holder.page, `/requests/${c.taken.id}/reassign`, {
        confirm: 'on',
        reason: 'hand it over',
        to_user_id: '1'
      }),
      [403],
      'the holder may release their own reservation but may not hand it to a named colleague'
    );
    await c.holder.page.goto(`/requests/${c.taken.id}/reservation`);
    await expect(
      c.holder.page.locator('#to_user_id'),
      'and the screen never renders a target select the POST would refuse'
    ).toHaveCount(0);
  });

  test('TC-D-033 — an Admin may release a reservation that is not theirs', async ({ adminPage, secondPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '2800.00' });
    expect((await reserve(secondPage, request.id)).status).toBe(303);

    const screen = await probeGet(adminPage, `/requests/${request.id}/reservation`);
    expect(screen.status, 'reservation:reassign is what opens somebody else’s reservation').toBe(200);
    expect(screen.body, 'and it names the person who holds it').toContain('Reserved by');

    const released = await probePost(adminPage, `/requests/${request.id}/release`, {
      confirm: 'on',
      reason: `Colleague is on leave ${runId}`
    });
    expect(released.status, 'taking work off somebody is the reassign verb, and the store honours it').toBe(303);
    expect(await isTakeable(adminPage, request)).toBe(true);
  });

  test('TC-D-034/035 — reassign moves the reservation and the trail records it', async ({
    adminPage,
    browser,
    secondPage,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '2900.00' });
    const target = await asRole(adminPage, browser, runId, ['Accounts'], 'target');
    try {
      expect((await reserve(secondPage, request.id)).status).toBe(303);

      await adminPage.goto(`/requests/${request.id}/reservation`);
      const select = adminPage.locator('#to_user_id');
      // The target select starts hidden behind [data-when="action:reassign"]:
      // an Admin holds both verbs, so the screen opens on the release branch.
      await expect(select, 'only an Admin is offered a target at all').toHaveCount(1);
      await adminPage.locator('input[name="action"][value="reassign"]').check();
      await expect(select, 'and choosing the reassign branch reveals it').toBeVisible();
      const option = select.locator('option', { hasText: target.subject.name });
      await expect(option, 'somebody who can work the queue is offered').toHaveCount(1);
      const targetId = await option.getAttribute('value');

      const moved = await probePost(adminPage, `/requests/${request.id}/reassign`, {
        confirm: 'on',
        reason: `Handing to the invoice desk ${runId}`,
        to_user_id: targetId!
      });
      expect(moved.status, 'G11: the reservation changes hands').toBe(303);
      expect(moved.location, 'and the reader is returned to the request').toBe(`/requests/${request.id}`);

      const now = await probeGet(target.page, `/payments/new?request=${request.id}`);
      expect(now.status, 'the new holder can record the payment').toBe(200);
      const old = await probeGet(secondPage, `/payments/new?request=${request.id}`);
      expect(old.status, 'and the old holder now meets the conflict screen').toBe(409);
      expect(old.body).toContain('took this request before you');

      await adminPage.goto(`/requests/${request.id}/reservation`);
      const trail = adminPage.locator('ol.thread li');
      await expect(trail.filter({ hasText: 'reserved it for processing' })).toHaveCount(1);
      await expect(
        trail.filter({ hasText: 'reassigned the reservation' }),
        'the handover is in the history with the reason that was given'
      ).toHaveCount(1);
      await expect(trail.filter({ hasText: `Handing to the invoice desk ${runId}` })).toHaveCount(1);
    } finally {
      await target.close();
    }
  });

  test('TC-D-036/037/038/039 — every field the reassign reveal implies is re-checked on the server', async ({
    adminPage,
    browser,
    secondPage,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '3100.00' });
    const manager = await asRole(adminPage, browser, runId, ['Manager'], 'mgrtarget');
    try {
      expect((await reserve(secondPage, request.id)).status).toBe(303);
      const reason = `Reassignment reason ${runId}`;

      const noTarget = await probePost(adminPage, `/requests/${request.id}/reassign`, { confirm: 'on', reason });
      expect(noTarget.status, 'a reassignment with nobody to reassign to is a bad request').toBe(400);
      expect(noTarget.body).toContain('Choose who should take this reservation.');

      await adminPage.goto(`/requests/${request.id}/reservation`);
      const targetId = await adminPage.locator('#to_user_id option').nth(1).getAttribute('value');
      expect(targetId, 'the select has to offer somebody for this test to mean anything').toBeTruthy();

      const noConfirm = await probePost(adminPage, `/requests/${request.id}/reassign`, {
        reason,
        to_user_id: targetId!
      });
      expect(noConfirm.status, 'the screen asks the same question for both branches, so both require the answer').toBe(
        400
      );
      expect(noConfirm.body).toContain('Confirm that no payment has been initiated.');

      // The select never offers a Manager, and the handler is what makes that a
      // rule: a reservation parked on somebody without payment:process is a
      // request nobody can move.
      await expect(
        adminPage.locator('#to_user_id option').filter({ hasText: manager.subject.name }),
        'somebody who cannot work the queue is not offered'
      ).toHaveCount(0);
      const strand = await probePost(adminPage, `/requests/${request.id}/reassign`, {
        confirm: 'on',
        reason,
        to_user_id: await userIdByEmail(adminPage, manager.subject.email)
      });
      expect(strand.status, 'and a hand-rolled POST naming them is refused').toBe(400);
      expect(strand.body).toContain('That person cannot work the Accounts queue.');

      const noReason = await probePost(adminPage, `/requests/${request.id}/reassign`, {
        confirm: 'on',
        to_user_id: targetId!
      });
      expect(noReason.status, 'a handover with no reason is refused in the store').toBe(400);
      expect(noReason.body).toContain('a reason is required');

      const stillHeld = await probeGet(secondPage, `/payments/new?request=${request.id}`);
      expect(stillHeld.status, 'and after four refusals the original holder still holds it').toBe(200);
    } finally {
      await manager.close();
    }
  });

  test('TC-D-040 — the reservation screens refuse a request nobody has reserved', async ({ browser }) => {
    const c = await corpus(browser);
    const screen = await probeGet(c.admin, `/requests/${c.open.id}/reservation`);
    expect(screen.status, 'there is no reservation to release or hand over').toBe(409);
    expect(screen.body).toContain('This request is not reserved by anyone.');

    const post = await probePost(c.admin, `/requests/${c.open.id}/reassign`, {
      confirm: 'on',
      reason: 'nothing to move',
      to_user_id: '1'
    });
    expect(post.status, 'and the POST says so too').toBe(403);
  });
});

// ===========================================================================
// D — the stale nudge (Q6)
// ===========================================================================

test.describe('D · the stale-reservation nudge', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'Gating assertions run once, on chromium.');
  });

  test('TC-D-041 — the nudge screen is gated on payment:process', async ({ browser, baseURL }) => {
    const c = await corpus(browser);
    const path = `/requests/${c.taken.id}/reservation/stale`;
    expectOutcome(await probeGet(c.holder.page, path), [200], 'the holder works the queue and reaches the nudge');
    expectOutcome(await probeGet(c.admin, path), [200], 'so does an administrator');
    expectOutcome(await probeGet(c.manager.page, path), [403], 'a Manager holds no payment:process');
    expectOutcome(await probeGet(c.requester.page, path), [403], 'nor does a Requester');
    expectOutcome(await probeGet(c.norole.page, path), [403], 'nor a user with no role');
    const anon = await probeAnonymous(browser, path, baseURL!);
    expect(anon.status).toBeGreaterThanOrEqual(300);
    expect(anon.location).toContain('/login');
  });

  test('TC-D-042 — the holder is offered three live choices and no dead ends', async ({ browser }) => {
    const c = await corpus(browser);
    await c.holder.page.goto(`/requests/${c.taken.id}/reservation/stale`);
    await expect(c.holder.page.locator('.banner.warn')).toContainText('reserved by you');
    await expect(
      c.holder.page.locator('.banner.warn'),
      'Q6: the nudge is a nudge — nothing is released automatically'
    ).toContainText('Nothing is released automatically');

    const choices = c.holder.page.locator('.a-list a');
    await expect(choices, 'carry on · release · put on hold — reassign is not theirs').toHaveCount(3);
    await expect(choices.nth(0)).toContainText('Carry on and record the payment');
    await expect(choices.nth(1)).toContainText('Release it');
    await expect(choices.nth(2)).toContainText('Put it on hold');

    for (let i = 0; i < 3; i++) {
      const href = await choices.nth(i).getAttribute('href');
      expect(
        (await probeGet(c.holder.page, href!)).status,
        `a choice on the nudge screen must not be a dead end: ${href}`
      ).toBe(200);
    }
  });

  test('TC-D-043 — an administrator is offered release, reassign and hold', async ({ browser }) => {
    const c = await corpus(browser);
    await c.admin.goto(`/requests/${c.taken.id}/reservation/stale`);
    const choices = c.admin.locator('.a-list a');
    await expect(choices, 'not "Carry on": the reservation is not the administrator’s').toHaveCount(3);
    await expect(choices.nth(0)).toContainText('Release it');
    await expect(choices.nth(1)).toContainText('Hand it to a colleague');
    await expect(choices.nth(2)).toContainText('Put it on hold');
    await expect(
      c.admin.locator('.action-bar a').filter({ hasText: 'Reassign with reason' }),
      'and the forced reassignment is offered only to somebody who may reassign'
    ).toHaveCount(1);
    for (let i = 0; i < 3; i++) {
      const href = await choices.nth(i).getAttribute('href');
      expect((await probeGet(c.admin, href!)).status, `${href} must answer`).toBe(200);
    }
  });

  test('TC-D-044 — a non-holding accountant is offered no choice that answers 403', async ({ browser }) => {
    // FINDING F-D-03. The template's own rule is "a choice the reader could not
    // take is not shown greyed out — it is not shown". "Put it on hold" is gated
    // on payment:hold alone, but it links to the release screen, which refuses
    // anybody who is neither the holder nor able to reassign.
    test.fail();
    const c = await corpus(browser);
    const other = await asRole(c.admin, browser, `${c.runId}c`, ['Accounts'], 'bystander');
    try {
      await other.page.goto(`/requests/${c.taken.id}/reservation/stale`);
      const choices = other.page.locator('.a-list a');
      const count = await choices.count();
      for (let i = 0; i < count; i++) {
        const href = await choices.nth(i).getAttribute('href');
        expect(
          (await probeGet(other.page, href!)).status,
          `every choice the nudge screen offers must answer for the reader it is offered to: ${href}`
        ).toBe(200);
      }
    } finally {
      await other.close();
    }
  });

  test('TC-D-045 — the nudge screen refuses a request that is not reserved', async ({ browser }) => {
    const c = await corpus(browser);
    const probe = await probeGet(c.admin, `/requests/${c.open.id}/reservation/stale`);
    expect(probe.status, 'off processing there is no holder to name and no elapsed time to count').toBe(409);
    expect(probe.body).toContain('This request is not reserved by anyone.');
  });
});

// ===========================================================================
// E — hold and unhold (L7, Q6)
// ===========================================================================

test.describe('D · hold and unhold', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'The hold is a state, not a layout.');
  });

  test('TC-D-046 — Accounts holds an approved request with a reason, and it stays approved', async ({
    adminPage,
    browser,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '3200.00' });
    const accounts = await asRole(adminPage, browser, runId, ['Accounts'], 'holder2');
    try {
      await accounts.page.goto(`/requests/${request.id}`);
      await accounts.page.getByRole('button', { name: 'Put on hold' }).click();
      const sheet = accounts.page.locator('#hold-sheet');
      await expect(sheet).toBeVisible();
      await sheet.getByLabel('What do you need from the requester').fill(`Which cost centre? ${runId}`);
      await sheet.getByRole('button', { name: 'Put on hold' }).click();
      await expect(accounts.page).toHaveURL(new RegExp(`/requests/${request.id}$`));

      await expect(accounts.page.locator('.rh-status .pill.hold'), 'the pill says what is true').toHaveText('On hold');
      await expect(accounts.page.locator('.banner.warn')).toContainText(`Which cost centre? ${runId}`);
      await expect(accounts.page.locator('.banner.warn')).toContainText('Payment is blocked until Accounts lifts');

      // The hold tab's own query is `on_hold=1 AND status='approved'`, so the row
      // being in it is the proof that L7 holds — no separate status was invented.
      await accounts.page.goto(`/accounts-queue?tab=hold&q=${encodeURIComponent(request.number)}`);
      await expect(
        accounts.page.locator('tbody tr').filter({ hasText: request.number }),
        'L7: on_hold=1 implies status=approved, which is exactly what this tab selects'
      ).toHaveCount(1);
    } finally {
      await accounts.close();
    }
  });

  test('TC-D-047 — a hold with no reason, or a whitespace reason, is refused', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '3300.00' });
    for (const reason of ['', '   ']) {
      const probe = await probePost(adminPage, `/requests/${request.id}/hold`, { reason });
      expect(probe.status, `a hold reason of ${JSON.stringify(reason)} is no reason at all`).toBe(400);
      expect(probe.body).toContain('a hold reason is required');
    }
    expect(await isTakeable(adminPage, request), 'and the request is still payable').toBe(true);
  });

  test('TC-D-048 — payment:hold is one grant for both directions, and a Manager holds neither', async ({
    browser
  }) => {
    const c = await corpus(browser);
    expectOutcome(
      await probePost(c.manager.page, `/requests/${c.open.id}/hold`, { reason: 'stop' }),
      [403],
      'a Manager cannot place a hold'
    );
    expectOutcome(
      await probePost(c.manager.page, `/requests/${c.onHold.id}/unhold`),
      [403],
      'and cannot lift one either — it is the same grant in both directions'
    );
    expectOutcome(
      await probePost(c.requester.page, `/requests/${c.open.id}/hold`, { reason: 'stop' }),
      [403],
      'nor may a requester pause a request'
    );
    await c.manager.page.goto(`/requests/${c.onHold.id}`);
    await expect(
      c.manager.page.getByRole('button', { name: 'Release hold' }),
      'and the screen offers a Manager no control the route would refuse'
    ).toHaveCount(0);
    await expect(c.manager.page.locator('.action-bar')).toContainText('Only Accounts can take this off hold.');
  });

  test('TC-D-049 — an on-hold request cannot be reserved and is takeable from neither list', async ({ browser }) => {
    const c = await corpus(browser);
    expect((await reserve(c.holder.page, c.onHold.id)).status, 'L7: payment is blocked while the hold stands').toBe(
      409
    );
    expect(await isTakeable(c.admin, c.onHold), 'the queue offers no Take button').toBe(false);
    expect(await pickerOptions(c.admin, c.onHold.number), 'and the picker offers no POST form').not.toContain(
      `action="/requests/${c.onHold.id}/record-payment"`
    );
    expect(await statusPill(c.admin, c.onHold.id), 'while the request itself is still on hold').toContain('On hold');
  });

  test('TC-D-050 — Q6: the requester answers while on hold and not one field changes', async ({
    adminPage,
    browser,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '3400.00' });
    const accounts = await asRole(adminPage, browser, runId, ['Accounts'], 'holder3');
    try {
      const fields = async () => {
        await adminPage.goto(`/requests/${request.id}`);
        return (await adminPage.locator('.card .dl').first().innerText()).trim();
      };
      const before = await fields();

      expect(
        (await probePost(accounts.page, `/requests/${request.id}/hold`, { reason: `Missing GSTIN ${runId}` })).status
      ).toBe(303);
      expect(await fields(), 'a hold changes no field on the request — it only pauses payment').toBe(before);

      // The requester of every fixture request is the signed-in admin, so this
      // is the person who raised it answering the question they were asked.
      await adminPage.goto(`/requests/${request.id}`);
      await adminPage.getByLabel('Add a comment').fill(`The cost centre is Operations. ${runId}`);
      await adminPage.getByRole('button', { name: 'Post comment' }).click();
      await expect(adminPage).toHaveURL(new RegExp(`/requests/${request.id}$`));
      await expect(
        adminPage.locator('ol.thread li.is-comment').filter({ hasText: `The cost centre is Operations. ${runId}` }),
        'the clarification lands in the conversation the requester already reads'
      ).toHaveCount(1);
      expect(await fields(), 'and answering changes no field either').toBe(before);
      await expect(adminPage.locator('.rh-status .pill.hold'), 'the hold is still on').toHaveText('On hold');
    } finally {
      await accounts.close();
    }
  });

  test('TC-D-051/052 — unholding returns the request to the queue; unholding twice is refused', async ({
    adminPage,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '3500.00' });
    expect((await probePost(adminPage, `/requests/${request.id}/hold`, { reason: `Query ${runId}` })).status).toBe(303);
    expect(await isTakeable(adminPage, request)).toBe(false);

    await adminPage.goto(`/requests/${request.id}`);
    await adminPage.getByRole('button', { name: 'Release hold' }).click();
    await expect(adminPage).toHaveURL(new RegExp(`/requests/${request.id}$`));
    await expect(adminPage.locator('.rh-status .pill'), 'lifting the hold restores the ordinary status').toHaveText(
      /Approved/
    );
    expect(await isTakeable(adminPage, request), 'and the request is payable again').toBe(true);

    const again = await probePost(adminPage, `/requests/${request.id}/unhold`);
    expect(again.status, 'a request that is not on hold cannot be taken off hold').toBe(403);
    await adminPage.goto(`/accounts-queue?tab=hold&q=${encodeURIComponent(request.number)}`);
    await expect(adminPage.locator('tbody tr').filter({ hasText: request.number })).toHaveCount(0);
  });

  test('TC-D-053 — a reserved request cannot be put on hold', async ({ browser }) => {
    const c = await corpus(browser);
    // The message is the generic permission sentence rather than the state — see
    // F-D-02 — so this asserts the refusal and the state, not the words.
    const probe = await probePost(c.admin, `/requests/${c.taken.id}/hold`, { reason: 'too late' });
    expect(probe.status, 'a hold pauses an approved request; this one is already being paid').toBe(403);
    expect(await statusPill(c.admin, c.taken.id), 'and the reservation is untouched').toContain(RESERVED);
    await c.holder.page.goto(`/requests/${c.taken.id}`);
    await expect(
      c.holder.page.getByRole('button', { name: 'Put on hold' }),
      'and the control is not offered on a reserved request'
    ).toHaveCount(0);
  });
});

// ===========================================================================
// F — the entry screen and its prefill (S14)
// ===========================================================================

test.describe('D · the entry screen and its prefill', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'Field-level prefill runs once, on chromium.');
  });

  test('TC-D-054 — the entry screen carries the labels the design names', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '4100.00' });
    await takeFromQueue(adminPage, request);

    // The label text and the accessible name are two different strings here:
    // every required field appends `<span class="req" aria-hidden="true">*</span>`,
    // which Playwright's getByLabel includes (it reads the label's text) and a
    // screen reader does not (it is aria-hidden). Both are asserted, so the
    // exact wording is pinned either way.
    for (const [id, text, required] of [
      ['approved', 'Approved amount', false],
      ['amount', 'Amount actually paid', true],
      ['paid_on', 'Paid on', true],
      ['payment_mode', 'Payment mode', true],
      ['reference_no', 'Transaction / UTR reference', true]
    ] as const) {
      const label = adminPage.locator(`label[for="${id}"]`);
      await expect(label, `the field is labelled exactly "${text}"`).toHaveText(required ? `${text} *` : text);
      if (required) {
        await expect(
          label.locator('span.req'),
          'the asterisk is decoration, so it is aria-hidden and never part of the accessible name'
        ).toHaveAttribute('aria-hidden', 'true');
      }
      await expect(adminPage.getByLabel(text), `and getByLabel('${text}') resolves it`).toHaveCount(1);
    }
    // "Processing note" is the exception: its "optional" chip is NOT aria-hidden,
    // so the accessible name is "Processing note optional" (finding F-D-05).
    await expect(adminPage.getByLabel('Processing note')).toHaveCount(1);
    await expect(adminPage.locator('label[for="remarks"]')).toHaveText('Processing note optional');
    await expect(
      adminPage.locator('label[for="remarks"] span.opt'),
      'unlike the required asterisk, the optional chip joins the accessible name'
    ).not.toHaveAttribute('aria-hidden', /./);

    const upload = adminPage.locator('input[name="attachment"]');
    await expect(upload, 'the uploader is a hidden nameless input inside label.uploader').toHaveCount(1);
    await expect(upload).toHaveAttribute('hidden', '');
    await expect(upload).not.toHaveAttribute('aria-label', /./);
    await expect(adminPage.locator('label.uploader')).toContainText('Add the bank advice');
  });

  test('TC-D-055/056 — S14: the payee, head and invoice arrive as hidden inputs copied from the request', async ({
    browser
  }) => {
    const c = await corpus(browser);
    const request = await createApprovedRequest(c.admin, c.runId, {
      amount: '4200.00',
      payee: `Hidden Fields Vendor ${c.runId}`
    });
    await takeFromQueue(c.admin, request);

    await expect(c.admin.locator('input[name="request_id"]')).toHaveValue(String(request.id));
    await expect(
      c.admin.locator('input[name="head_id"]'),
      'the head is the request’s own — the accountant never chooses where the money is charged'
    ).toHaveValue(c.heads.get('Operations / Office Rent')!);
    await expect(
      c.admin.locator('input[name="vendor_payee"]'),
      'and the payee is the display payee, not the empty vendor_invoice snapshot column'
    ).toHaveValue(`Hidden Fields Vendor ${c.runId}`);
    await expect(c.admin.locator('input[name="invoice_no"]')).toHaveValue(new RegExp(`^INV-\\d+-${c.runId}$`));

    await expect(c.admin.getByLabel('Amount actually paid'), 'prefilled with what was approved').toHaveValue(
      '4,200.00'
    );
    await expect(c.admin.getByLabel('Approved amount')).toHaveValue('₹4,200.00');
    await expect(c.admin.getByLabel('Paid on'), 'and with today, because that is when money usually moves').toHaveValue(
      /^\d{4}-\d{2}-\d{2}$/
    );
    await expect(c.admin.locator('.card .dl')).toContainText(`Hidden Fields Vendor ${c.runId}`);
    await expect(c.admin.locator('.card .dl'), 'the card states where it will be charged').toContainText(
      'Operations / Office Rent'
    );
  });

  test('TC-D-057 — the payee survives the whole flow, from queue to payment detail', async ({ browser }) => {
    const c = await corpus(browser);
    const payee = `Payee Walk ${c.runId}`;
    const request = await createApprovedRequest(c.admin, c.runId, { amount: '4300.00', payee });

    await c.admin.goto(`/accounts-queue?tab=approved&q=${encodeURIComponent(request.number)}`);
    const queueRow = c.admin.locator('tbody tr').filter({ hasText: request.number });
    await expect(
      queueRow.locator('td[data-label="Payee"]'),
      'the queue’s Payee column reads the display payee (fixed at fca6939)'
    ).toHaveText(payee);

    await takeFromQueue(c.admin, request);
    await expect(c.admin.locator('.card .dl'), 'the entry screen names who is being paid').toContainText(payee);
    await expect(
      c.admin.locator('input[name="vendor_payee"]'),
      'and posts that same name, so the payment is written with a payee'
    ).toHaveValue(payee);

    // Settled here rather than through settlePayment, because that helper
    // reserves from the queue and this request is already reserved above.
    await fillEntry(c.admin, { amount: '4300.00', reference: `UTR-WALK-${c.runId}` });
    const sheet = await openSheet(c.admin);
    await sheet.locator('input[name="settlement"][value="settled"]').check();
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();
    await expect(c.admin).toHaveURL(/\/payments\/\d+$/);
    const payment = new URL(c.admin.url()).pathname;
    await expect(
      c.admin.locator('.req-head h1'),
      'the payment detail’s h1 is the payee, and it is not blank'
    ).toHaveText(payee);

    await c.admin.goto(`/payments?month=2026-07&q=${encodeURIComponent(payee)}`);
    const ledgerRow = c.admin.locator('tbody tr').filter({ has: c.admin.locator(`a[href="${payment}"]`) });
    await expect(ledgerRow, 'and the ledger row is findable by the payee it was written with').toHaveCount(1);
    await expect(ledgerRow.locator('td[data-label="Payee"]')).toHaveText(payee);
  });

  test('TC-D-058 — a tampered head_id and vendor_payee are ignored or refused', async ({
    adminPage,
    browser,
    runId
  }) => {
    // FINDING F-D-01. paymentInput reads head_id and vendor_payee straight off
    // the form, and RecordPaymentForRequest never compares them with the request
    // it is settling. An accountant holding the reservation can charge the
    // payment to any active head and write any payee on it.
    test.fail();
    const c = await corpus(browser);
    const request = await createApprovedRequest(adminPage, runId, { amount: '4400.00' });
    await reserve(adminPage, request.id);
    const honest = await entryFields(adminPage, request.id);
    const foreignHead = c.heads.get('People / Payroll')!;
    expect(foreignHead, 'the tamper needs a second, valid, active head').not.toBe(honest.head_id);

    const probe = await probePost(adminPage, '/payments', {
      request_id: String(request.id),
      amount: '4400.00',
      paid_on: PAID_ON,
      payment_mode: 'bank_transfer',
      reference_no: `UTR-TAMPER-${runId}`,
      settlement: 'settled',
      head_id: foreignHead,
      vendor_payee: `NOT THE APPROVED PAYEE ${runId}`,
      invoice_no: honest.invoice_no
    });
    expect(probe.status, 'the settlement either ignores the tampered snapshot or refuses it').toBe(303);

    await adminPage.goto(probe.location!);
    await expect(
      adminPage.locator('.rh-meta'),
      'the payment must be charged to the head the approval named, not one the poster chose'
    ).toContainText('Operations / Office Rent');
    await expect(adminPage.locator('.req-head h1'), 'and paid to the payee the request named').not.toContainText(
      'NOT THE APPROVED PAYEE'
    );
  });

  test('TC-D-059 — an entry screen for somebody else’s reservation is the conflict screen, not a 403', async ({
    browser
  }) => {
    const c = await corpus(browser);
    const probe = await probeGet(c.admin, `/payments/new?request=${c.taken.id}`);
    expect(probe.status, 'G15: HTTP 409 on a rendered screen').toBe(409);
    expect(probe.body, 'the reader needs to know who has it').toContain('took this request before you');
    expect(probe.body, 'and what they may do instead').toContain('Go back to the queue');
    expect(probe.body, 'this is not the error page').not.toContain('class="error-code"');
  });

  test('TC-D-060 — the entry screen refuses an unreserved request and an unknown one', async ({ browser }) => {
    const c = await corpus(browser);
    const unreserved = await probeGet(c.admin, `/payments/new?request=${c.open.id}`);
    expect(unreserved.status, 'a payment exists only against a reservation the caller holds').toBe(409);
    const missing = await probeGet(c.admin, '/payments/new?request=99999999');
    expect(missing.status, 'and an id that is not a request is not found').toBe(404);
    const nonsense = await probeGet(c.admin, '/payments/new?request=not-a-number');
    expect(nonsense.status, 'while an unparseable id falls back to the picker').toBe(200);
    expect(nonsense.body).toContain('Which approved request is this for?');
  });
});

// ===========================================================================
// G — the settlement preview is pure (D8, S13)
// ===========================================================================

test.describe('D · the settlement preview writes nothing', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'Purity is a server property.');
  });

  test('TC-D-061 — D8: previewing, then abandoning, records nothing at all', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '5100.00' });
    await reserve(adminPage, request.id);
    const fields = await entryFields(adminPage, request.id);

    const snapshot = async () => {
      const detail = await probeGet(adminPage, `/requests/${request.id}`);
      const ledger = await probeGet(adminPage, `/payments?month=2026-07&q=UTR-PURE-${runId}`);
      return {
        status: await statusPill(adminPage, request.id),
        outcome: detail.body.includes('Payment outcome'),
        ledgerEmpty: ledger.body.includes('No payments match these filters')
      };
    };
    const before = await snapshot();
    expect(before.status, 'the request is reserved and nothing has been paid').toContain(RESERVED);
    expect(before.outcome, 'so it carries no payment outcome').toBe(false);
    expect(before.ledgerEmpty, 'and the ledger has no row for this reference').toBe(true);

    const preview = await probePost(adminPage, `/requests/${request.id}/settlement-preview`, {
      amount: '4000.00',
      paid_on: PAID_ON,
      payment_mode: 'bank_transfer',
      reference_no: `UTR-PURE-${runId}`,
      ...fields
    });
    expect(preview.status, 'the preview answers with the confirmation screen').toBe(200);
    expect(preview.body, 'which says out loud that nothing is saved yet').toContain('Not saved yet');
    expect(preview.body).toContain('Confirming saves the payment');
    expect(preview.body, 'and carries every typed value forward as a hidden input').toContain(
      `value="UTR-PURE-${runId}"`
    );

    expect(await snapshot(), 'D8: the preview holds no transaction, so nothing about the world changed').toEqual(
      before
    );

    // Abandon it the way a person does — walk away and never confirm.
    await adminPage.goto('/accounts-queue');
    await adminPage.goto('/');
    expect(await snapshot(), 'and abandoning the flow leaves the request exactly as it was').toEqual(before);
  });

  test('TC-D-062 — S13: the sheet swaps into #settle-mount without changing the URL, and Go back is a link', async ({
    adminPage,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '5200.00' });
    await takeFromQueue(adminPage, request);
    await fillEntry(adminPage, { amount: '5000.00', reference: `UTR-SHEET-${runId}` });

    const url = adminPage.url();
    const sheet = await openSheet(adminPage);
    expect(adminPage.url(), 'the sheet is a swap inside the live form, not a navigation').toBe(url);
    await expect(adminPage.locator('#settle-mount .overlay .sheet'), 'and it lands in #settle-mount').toHaveCount(1);
    // "Not saved yet" is the no-JS confirmation page's pill (TC-D-061); the
    // fragment says the same thing in its own banner.
    await expect(sheet.locator('.banner.info')).toContainText('Confirming saves the payment');
    await expect(sheet.locator('.banner.info')).toContainText('It cannot be edited or cancelled afterwards');

    const goBack = sheet.locator('.sh-foot a').filter({ hasText: 'Go back' });
    await expect(goBack, 'going back must not post anything, so it is an anchor').toHaveCount(1);
    expect(
      await goBack.evaluate(node => node.tagName),
      'a button here would submit the form it sits inside'
    ).toBe('A');
    await expect(goBack).toHaveAttribute('href', `/payments/new?request=${request.id}`);

    await goBack.click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${request.id}$`));
    await expect(adminPage.locator('.reserve-bar'), 'and the reservation survives the retreat').toContainText(
      'Reserved by you'
    );
  });

  test('TC-D-063 — the preview is gated on payment:settle and refuses a non-holder', async ({ browser }) => {
    const c = await corpus(browser);
    expectOutcome(
      await probePost(c.manager.page, `/requests/${c.taken.id}/settlement-preview`, { amount: '10.00' }),
      [403],
      'a Manager holds no payment:settle'
    );
    const nonHolder = await probePost(c.admin, `/requests/${c.taken.id}/settlement-preview`, { amount: '10.00' });
    expect(nonHolder.status, 'losing the reservation between entry and confirmation is a screen (G15)').toBe(409);
    expect(nonHolder.body).toContain('took this request before you');
  });

  test('TC-D-064 — a malformed amount comes back as the sheet with the message, not an error page', async ({
    adminPage,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '5300.00' });
    await reserve(adminPage, request.id);
    const probe = await probePost(adminPage, `/requests/${request.id}/settlement-preview`, {
      amount: 'not-money',
      paid_on: PAID_ON,
      payment_mode: 'bank_transfer',
      reference_no: `UTR-BAD-${runId}`,
      ...(await entryFields(adminPage, request.id))
    });
    expect(probe.status, 'a rejected figure is a bad request').toBe(400);
    expect(probe.body, 'and the accountant is told what to fix, in the sheet').toContain('enter a valid amount');
    expect(probe.body).toContain('Nothing has been saved');
    expect(probe.body, 'not thrown onto the error page').not.toContain('class="error-code"');
  });
});

// ===========================================================================
// H — settlement outcomes (S10, S11)
// ===========================================================================

test.describe('D · settlement outcomes', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'Outcomes are driven once, on chromium.');
  });

  test('TC-D-065 — S10: paid less than approved, settled, completes the request', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '6000.00' });
    await takeFromQueue(adminPage, request);
    await fillEntry(adminPage, { amount: '4500.00', reference: `UTR-UNDER-${runId}` });
    await expect(adminPage.locator('#diff-banner'), 'the client warns before the server is asked').toHaveClass(/warn/);
    const sheet = await openSheet(adminPage);
    await expect(sheet.locator('.cmp-row.diff')).toContainText('1,500.00');
    await expect(sheet.locator('.outcome.good')).toHaveText('Completed');
    await sheet.locator('input[name="settlement"][value="settled"]').check();
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();

    await expect(adminPage).toHaveURL(/\/payments\/\d+$/);
    await expect(
      adminPage.locator('.pill.completed'),
      'S10: settled completes it even under the approved figure'
    ).toBeVisible();
    await expect(adminPage.locator('.compare .cmp-row.match')).toContainText('confirmed settled by Accounts');

    await adminPage.goto(`/requests/${request.id}`);
    await expect(adminPage.locator('.rh-status .pill')).toHaveText('Completed');
    await expect(adminPage.locator('.compare')).toContainText('₹4,500.00');
    await expect(
      adminPage.locator('.compare .cmp-row.match'),
      'a settled shortfall is agreed, and must never read like money still owed'
    ).toContainText('confirmed settled by Accounts');
    await expect(adminPage.locator('.compare')).not.toContainText('Still owed');
  });

  test('TC-D-066 — paid exactly the approved amount completes with a matching difference', async ({
    adminPage,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '6100.00' });
    await settlePayment(adminPage, request.id, {
      amount: '6100.00',
      paidOn: PAID_ON,
      reference: `UTR-EXACT-${runId}`
    });
    await expect(adminPage.locator('.pill.completed')).toBeVisible();
    await expect(adminPage.locator('.compare .cmp-row.match')).toContainText('₹0.00');
    expect(await statusPill(adminPage, request.id)).toContain('Completed');
  });

  test('TC-D-067 — G13: paid more than approved is refused and writes nothing', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '6200.00' });
    await reserve(adminPage, request.id);
    const probe = await probePost(adminPage, '/payments', {
      request_id: String(request.id),
      amount: '6200.01',
      paid_on: PAID_ON,
      payment_mode: 'bank_transfer',
      reference_no: `UTR-OVER-${runId}`,
      settlement: 'settled',
      ...(await entryFields(adminPage, request.id))
    });
    expect(probe.status, 'one paise over the ceiling is over the ceiling').toBe(400);
    expect(probe.body).toContain('more than the approved');
    expect(probe.body).toContain('cancel this request and raise a new one');
    expect(await statusPill(adminPage, request.id), 'and the request is still being processed').toContain(RESERVED);
    const ledger = await probeGet(adminPage, `/payments?month=2026-07&q=UTR-OVER-${runId}`);
    expect(ledger.body, 'with no payment behind it').toContain('No payments match these filters');
  });

  test('TC-D-068 — a partial settlement with no reason is refused', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '6300.00' });
    await reserve(adminPage, request.id);
    const fields = await entryFields(adminPage, request.id);
    for (const partial_reason of ['', '   ']) {
      const probe = await probePost(adminPage, '/payments', {
        request_id: String(request.id),
        amount: '3000.00',
        paid_on: PAID_ON,
        payment_mode: 'bank_transfer',
        reference_no: `UTR-NOREASON-${runId}`,
        settlement: 'partial',
        partial_reason,
        ...fields
      });
      expect(probe.status, 'L9: the manager reads this reason when deciding, so it cannot be blank').toBe(400);
      expect(probe.body).toContain('a reason is required for a partial settlement');
    }
    expect(await statusPill(adminPage, request.id)).toContain(RESERVED);
  });

  test('TC-D-069/070/071 — S11: a partial settlement routes to the manager, who accepts and closes it', async ({
    adminPage,
    browser,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '6400.00' });
    await takeFromQueue(adminPage, request);
    await fillEntry(adminPage, { amount: '4000.00', reference: `UTR-PARTIAL-${runId}` });
    const sheet = await openSheet(adminPage);
    await expect(sheet.locator('.outcome.warn')).toHaveText('Manager review');
    await expect(
      sheet.locator('[data-when="settlement:partial"]'),
      'the reason appears only once partial is chosen'
    ).toBeHidden();
    await sheet.locator('input[name="settlement"][value="partial"]').check();
    await sheet.getByLabel('Why only part was paid').fill(`Retention held back until handover ${runId}`);
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();
    await expect(adminPage).toHaveURL(/\/payments\/\d+$/);
    await expect(adminPage.locator('.pill.partial')).toBeVisible();
    expect(await statusPill(adminPage, request.id), 'L9: the request goes to the manager, not to completed').toContain(
      'Partial'
    );

    const approver = approverFor(runId);
    const ctx = await browser.newContext({ viewport: adminPage.viewportSize() ?? undefined });
    try {
      const page = await ctx.newPage();
      await login(page, approver.email, approver.password);
      await page.goto(`/requests/${request.id}`);
      await expect(
        page.getByRole('link', { name: 'Decide the partial payment' }),
        'the decision has to be reachable by the one person who can take it'
      ).toBeVisible();
      await page.getByRole('link', { name: 'Decide the partial payment' }).click();
      await expect(page).toHaveURL(new RegExp(`/requests/${request.id}/partial-review$`));
      // The first .compare is the screen's own; the second lives inside the
      // hidden #close-sheet, which is in the DOM from the start.
      await expect(page.locator('.compare').first().locator('.cmp-row.diff')).toContainText('₹2,400.00');
      // The first .banner.warn is the shortfall's own; the second is inside the
      // hidden #concern-sheet ("This does not reverse anything").
      await expect(page.locator('.banner.warn').first()).toContainText(
        `Retention held back until handover ${runId}`
      );
      await expect(page.locator('.card .pill.neutral.no-dot'), 'S12: nothing here can amend the payment').toHaveText(
        'Cannot be edited'
      );

      await page.getByRole('button', { name: 'Accept and close' }).click();
      const close = page.locator('#close-sheet');
      await expect(close).toBeVisible();
      await expect(close.locator('.cmp-row.diff')).toContainText('₹2,400.00');
      await close.getByLabel(/^Note/).fill(`Balance written off ${runId}`);
      await close.getByRole('button', { name: 'Accept and close' }).click();
      await expect(page).toHaveURL(new RegExp(`/requests/${request.id}$`));
      await expect(
        page.locator('.rh-status .pill.completed-partial'),
        'G14: a written-off balance stays visible for the life of the record'
      ).toHaveText('Completed — partial accepted');
      await expect(
        page.locator('ol.thread li').filter({ hasText: `Balance written off ${runId}` }),
        'and the note the manager left is in the history'
      ).toHaveCount(1);
    } finally {
      await ctx.close();
    }
  });

  test('TC-D-072/073 — the manager raises a concern: the request stays in review with the words in the trail', async ({
    adminPage,
    browser,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '6500.00' });
    await settlePayment(adminPage, request.id, {
      amount: '5000.00',
      paidOn: PAID_ON,
      reference: `UTR-CONCERN-${runId}`,
      settlement: 'partial',
      partialReason: `Short by agreement ${runId}`
    });

    const approver = approverFor(runId);
    const ctx = await browser.newContext({ viewport: adminPage.viewportSize() ?? undefined });
    try {
      const page = await ctx.newPage();
      await login(page, approver.email, approver.password);

      const empty = await probePost(page, `/requests/${request.id}/raise-concern`, { comment: '  ' });
      expect(empty.status, 'a concern with nothing in it is not a concern').toBe(400);
      expect(empty.body).toContain('a concern comment is required');

      await page.goto(`/requests/${request.id}/partial-review`);
      await page.getByRole('button', { name: 'Raise a concern' }).click();
      const sheet = page.locator('#concern-sheet');
      await expect(sheet).toBeVisible();
      await expect(sheet.locator('.banner.warn'), 'and it reverses nothing — the money has gone').toContainText(
        'This does not reverse anything'
      );
      await sheet.getByLabel('What is wrong').fill(`The retention was not agreed ${runId}`);
      await sheet.getByRole('button', { name: 'Raise concern' }).click();

      await expect(page, 'a concern comes back to the decision').toHaveURL(
        new RegExp(`/requests/${request.id}/partial-review$`)
      );
      await expect(page.locator('.rh-status .pill')).toHaveText(/Partial/);
      await expect(
        page.locator('ol.thread li.is-comment').filter({ hasText: `The retention was not agreed ${runId}` }),
        'the manager’s words are in the conversation, not only in the audit'
      ).toHaveCount(1);
      await expect(
        page.getByRole('button', { name: 'Accept and close' }),
        'and the decision is still open afterwards'
      ).toBeVisible();
    } finally {
      await ctx.close();
    }
  });

  test('TC-D-074 — the partial decision belongs to the request’s own manager, not to everyone holding the verb', async ({
    adminPage,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '6600.00' });
    await settlePayment(adminPage, request.id, {
      amount: '4800.00',
      paidOn: PAID_ON,
      reference: `UTR-NOTMINE-${runId}`,
      settlement: 'partial',
      partialReason: `Balance disputed ${runId}`
    });

    // The admin holds approval:accept_partial — every grant, in fact — and is
    // not the manager this request was routed to.
    const accept = await probePost(adminPage, `/requests/${request.id}/accept-partial`, { note: 'closing it' });
    expect(accept.status, 'holding the verb says a person may accept a shortfall, never whose').toBe(403);
    const concern = await probePost(adminPage, `/requests/${request.id}/raise-concern`, { comment: 'not happy' });
    expect(concern.status, 'and disputing one answers to the same owner').toBe(403);
    expect(await statusPill(adminPage, request.id), 'so the request is still awaiting its own manager').toContain(
      'Partial'
    );

    await adminPage.goto(`/requests/${request.id}/partial-review`);
    await expect(
      adminPage.getByRole('button', { name: 'Accept and close' }),
      'and the screen offers the administrator no decision at all'
    ).toHaveCount(0);
    await expect(adminPage.locator('.action-bar')).toContainText('decides');
  });

  test('TC-D-075 — the partial review redirects when there is no partial payment to review', async ({ browser }) => {
    const c = await corpus(browser);
    const settled = await probeGet(c.admin, `/requests/${c.done.id}/partial-review`);
    expect(settled.status, 'off partial_review every sentence on that screen is false').toBe(303);
    expect(settled.location).toBe(`/requests/${c.done.id}`);
    expect((await probeGet(c.admin, `/requests/${c.open.id}/partial-review`)).status).toBe(303);
  });
});

// ===========================================================================
// I — one payment per request, and immutability (S9, S12, S15, X5, X6)
// ===========================================================================

test.describe('D · one request, one payment, never edited', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'Immutability is a server property.');
  });

  test('TC-D-076 — S9: a second settlement creates no second payment', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '7100.00' });
    await reserve(adminPage, request.id);
    const form = {
      request_id: String(request.id),
      amount: '7100.00',
      paid_on: PAID_ON,
      payment_mode: 'bank_transfer',
      reference_no: `UTR-ONCE-${runId}`,
      settlement: 'settled',
      ...(await entryFields(adminPage, request.id))
    };
    const first = await probePost(adminPage, '/payments', form);
    expect(first.status).toBe(303);
    const paymentPath = first.location!;

    const second = await probePost(adminPage, '/payments', {
      ...form,
      amount: '1.00',
      reference_no: `UTR-SECOND-${runId}`
    });
    expect(
      second.status,
      'S9: the request has left processing and idx_payments_request is unique on request_id'
    ).toBe(303);
    expect(second.location, 'a double confirm goes to the payment that already exists').toBe(paymentPath);

    await adminPage.goto(`/payments?month=2026-07&q=UTR-ONCE-${runId}`);
    await expect(adminPage.locator('tbody tr'), 'and there is exactly one payment against the request').toHaveCount(1);
    await adminPage.goto(`/payments?month=2026-07&q=UTR-SECOND-${runId}`);
    await expect(adminPage.locator('td.empty'), 'the second attempt wrote nothing').toHaveCount(1);
  });

  test('TC-D-077/078 — S12: a linked payment can be neither edited nor voided', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '7200.00' });
    const payment = await settlePayment(adminPage, request.id, {
      amount: '7200.00',
      paidOn: PAID_ON,
      reference: `UTR-IMMUTABLE-${runId}`
    });
    const id = payment.split('/').pop()!;

    await expect(adminPage.getByRole('link', { name: /Edit/ }), 'no control offers it').toHaveCount(0);
    await expect(adminPage.getByRole('button', { name: /Void/ })).toHaveCount(0);
    await expect(adminPage.locator('.card .pill.neutral.no-dot')).toHaveText('Read-only');

    const form = await probeGet(adminPage, `/payments/${id}/edit`);
    expect(form.status, 'the URL is closed, not merely unlinked').toBe(303);
    expect(form.location).toBe(`/payments/${id}`);

    const heads = await headIds(adminPage);
    const edit = await probePost(adminPage, `/payments/${id}/edit`, {
      head_id: heads.get('Operations / Office Rent')!,
      paid_on: PAID_ON,
      amount: '1.00',
      vendor_payee: 'rewritten',
      payment_mode: 'cash'
    });
    expect(edit.status, 'S12: editing it would rewrite what the requester was told was paid').toBe(400);
    expect(edit.body).toContain('a payment linked to a request cannot be edited');

    const voided = await probePost(adminPage, `/payments/${id}/void`, { reason: 'mistake' });
    expect(voided.status, 'and voiding it would leave the request completed with nothing paid').toBe(400);
    expect(voided.body).toContain('a payment linked to a request cannot be voided');

    await adminPage.goto(`/payments/${id}`);
    await expect(adminPage.locator('.rh-amt'), 'so the figures are exactly what was confirmed').toContainText(
      '7,200.00'
    );
    await expect(adminPage.locator('.pill.completed')).toBeVisible();
  });

  test('TC-D-079/080/081 — X5/S15: no payment without a reservation the caller holds', async ({
    adminPage,
    browser,
    runId
  }) => {
    const c = await corpus(browser);
    const request = await createApprovedRequest(adminPage, runId, { amount: '7300.00' });
    const base = {
      amount: '100.00',
      paid_on: PAID_ON,
      payment_mode: 'bank_transfer',
      reference_no: `UTR-NOLINK-${runId}`,
      settlement: 'settled',
      head_id: c.heads.get('Operations / Office Rent')!,
      vendor_payee: 'Anybody'
    };

    const noRequest = await probePost(adminPage, '/payments', base);
    expect(noRequest.status, 'X5: free-standing payment entry is gone').toBe(400);
    expect(noRequest.body).toContain('Payments must be linked to an approved request.');

    // Both of the next two are ErrForbidden, which settlementError renders as a
    // 403 confirmation sheet carrying the generic "Something went wrong"
    // sentence rather than the store's reason. See F-D-02.
    const unreserved = await probePost(adminPage, '/payments', { ...base, request_id: String(request.id) });
    expect(unreserved.status, 'S15: approved is not enough — the payment needs a reservation').toBe(403);

    const notMine = await probePost(adminPage, '/payments', { ...base, request_id: String(c.taken.id) });
    expect(notMine.status, 'and it has to be the caller’s own reservation').toBe(403);

    const ledger = await probeGet(adminPage, `/payments?month=2026-07&q=UTR-NOLINK-${runId}`);
    expect(ledger.body, 'none of the three wrote anything').toContain('No payments match these filters');
    expect(await statusPill(adminPage, request.id), 'and the unreserved request is still merely approved').toContain(
      'Approved'
    );
  });

  test('TC-D-095 — S12: a linked payment accepts no new attachment either', async ({ adminPage, runId }) => {
    // FINDING F-D-08 (the sibling's SD-03, reproduced here because S12 is this
    // audit's). POST /payments/{id}/attachments is gated only on
    // attachment:create, and store.AddAttachment has no RequestID guard — unlike
    // UpdatePaymentWithAttachment and VoidPayment, which both refuse a linked row.
    test.fail();
    const request = await createApprovedRequest(adminPage, runId, { amount: '7400.00' });
    const payment = await settlePayment(adminPage, request.id, {
      amount: '7400.00',
      paidOn: PAID_ON,
      reference: `UTR-ATTACH-${runId}`
    });
    const id = payment.split('/').pop()!;
    await expect(
      adminPage.locator('form[action$="/attachments"]'),
      'no screen offers the control on a linked payment'
    ).toHaveCount(0);

    // A real multipart upload: probePost is form-encoded, and
    // stageUploadedAttachment treats a non-multipart body as "no file at all".
    const csrf = (await adminPage.context().cookies()).find(c => c.name === 'fervid_csrf')!.value;
    const response = await adminPage.request.post(`/payments/${id}/attachments`, {
      multipart: {
        csrf,
        attachment: { name: 'late-advice.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.4 audit-d') }
      },
      maxRedirects: 0,
      failOnStatusCode: false
    });
    expect(
      response.status(),
      'S12: a payment the screen calls read-only must refuse a new document as firmly as it refuses an edit'
    ).toBe(400);
  });

  test('TC-D-082 — X6: every payment this product can create is linked, and offers only View', async ({
    browser
  }) => {
    const c = await corpus(browser);
    await c.admin.goto('/payments?month=2026-07&status=all');
    const rows = c.admin.locator('tbody tr');
    const count = await rows.count();
    expect(count, 'the corpus alone put a settled payment in this month').toBeGreaterThan(0);
    for (let i = 0; i < count; i++) {
      const actions = rows.nth(i).locator('.actions-cell a');
      await expect(actions, 'a linked row offers exactly one action').toHaveCount(1);
      await expect(actions, 'and that action is View').toHaveText('View');
      await expect(
        rows.nth(i).locator('details.inline-danger'),
        'the Remove disclosure belongs to historical rows only (X6)'
      ).toHaveCount(0);
    }
  });
});

// ===========================================================================
// J — proof of absence (X3)
// ===========================================================================

test.describe('D · what deliberately does not exist', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'Absence is absence on one engine.');
  });

  test('TC-D-083 — X3: there is no refund, reversal or return-of-money route', async ({ browser }) => {
    const c = await corpus(browser);
    for (const path of [
      `/requests/${c.done.id}/refund`,
      `/requests/${c.done.id}/reverse`,
      `/requests/${c.done.id}/return-money`,
      '/payments/1/refund',
      '/payments/1/reverse',
      '/refunds'
    ]) {
      expectOutcome(
        await probePost(c.admin, path, { reason: 'give it back' }),
        [405, 404],
        `POST ${path} must not exist — an unrouted POST answers 405 because GET / is a catch-all`
      );
      expectOutcome(await probeGet(c.admin, path), [404], `GET ${path} must not exist either`);
    }
  });

  test('TC-D-084 — the payment detail says refunds are out of scope and offers no such control', async ({
    browser
  }) => {
    const c = await corpus(browser);
    const request = await probeGet(c.admin, `/requests/${c.done.id}`);
    const link = /href="(\/payments\/\d+)"/.exec(request.body);
    expect(link, 'the requester reads the outcome on the request and can open the payment').toBeTruthy();
    await c.admin.goto(link![1]);
    await expect(c.admin.locator('.action-bar')).toContainText('Refunds and reversals are outside this version.');
    await expect(c.admin.getByRole('button', { name: /Refund|Reverse/ })).toHaveCount(0);
    await expect(c.admin.getByRole('link', { name: /Refund|Reverse/ })).toHaveCount(0);
    await expect(c.admin.locator('.banner.good')).toContainText('can no longer be edited or cancelled');
  });
});

// ===========================================================================
// K — tampering with the settlement POST
// ===========================================================================

test.describe('D · tampering with the settlement', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'Tampering is a server property.');
  });

  test('TC-D-085/086/087 — a settlement outside the vocabulary, and amounts that are not money, are refused', async ({
    adminPage,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '8100.00' });
    await reserve(adminPage, request.id);
    const base = {
      request_id: String(request.id),
      amount: '100.00',
      paid_on: PAID_ON,
      payment_mode: 'bank_transfer',
      reference_no: `UTR-TAMPER2-${runId}`,
      ...(await entryFields(adminPage, request.id))
    };

    for (const settlement of ['', 'completed', 'SETTLED', 'part', 'refund']) {
      const probe = await probePost(adminPage, '/payments', { ...base, settlement });
      expect(probe.status, `settlement=${JSON.stringify(settlement)} is not in the vocabulary`).toBe(400);
      expect(probe.body).toContain('choose payment settled or partial settlement');
    }

    for (const amount of ['0', '0.00', '-100.00', 'not-money', '', '  ', '1e400']) {
      const probe = await probePost(adminPage, '/payments', { ...base, settlement: 'settled', amount });
      expect(probe.status, `an amount of ${JSON.stringify(amount)} is not a payment`).toBe(400);
    }

    expect(
      await statusPill(adminPage, request.id),
      'and after twelve refusals the request is still reserved'
    ).toContain(RESERVED);
    const ledger = await probeGet(adminPage, `/payments?month=2026-07&q=UTR-TAMPER2-${runId}`);
    expect(ledger.body, 'with nothing written').toContain('No payments match these filters');
  });

  test('TC-D-088/089 — a malformed paid_on is refused, and a locked month is refused with 409', async ({
    adminPage,
    runId
  }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '8200.00' });
    await reserve(adminPage, request.id);
    const base = {
      request_id: String(request.id),
      amount: '8200.00',
      payment_mode: 'bank_transfer',
      reference_no: `UTR-DATE-${runId}`,
      settlement: 'settled',
      ...(await entryFields(adminPage, request.id))
    };

    for (const paid_on of ['', '2026-13-45', '20-07-2026', 'yesterday']) {
      const probe = await probePost(adminPage, '/payments', { ...base, paid_on });
      expect(probe.status, `paid_on=${JSON.stringify(paid_on)} is not a date`).toBe(400);
      expect(probe.body).toContain('valid head, date, and positive amount are required');
    }

    // A locked month is the store's other refusal, and it is a conflict rather
    // than a validation failure: the figures are fine, the period is closed.
    const month = '2025-03';
    expect((await probePost(adminPage, `/months/${month}/lock`, { reason: `Audit D lock ${runId}` })).status).toBe(303);
    try {
      const locked = await probePost(adminPage, '/payments', { ...base, paid_on: `${month}-15` });
      expect(locked.status, 'a payment may not be recorded into a locked month').toBe(409);
      expect(locked.body).toContain('This month is locked');
    } finally {
      expect(
        (await probePost(adminPage, `/months/${month}/unlock`, { reason: `Audit D unlock ${runId}` })).status
      ).toBe(303);
    }
    expect(await statusPill(adminPage, request.id)).toContain(RESERVED);
  });

  test('TC-D-090 — a payment cannot be dated in the future', async ({ adminPage, runId }) => {
    // FINDING F-D-06. validatePayment checks the shape of paid_on and the month
    // lock, and nothing else. A payment dated years ahead is accepted and lands
    // in a future month's actuals.
    test.fail();
    const request = await createApprovedRequest(adminPage, runId, { amount: '8300.00' });
    await reserve(adminPage, request.id);
    const probe = await probePost(adminPage, '/payments', {
      request_id: String(request.id),
      amount: '8300.00',
      paid_on: '2029-12-31',
      payment_mode: 'bank_transfer',
      reference_no: `UTR-FUTURE-${runId}`,
      settlement: 'settled',
      ...(await entryFields(adminPage, request.id))
    });
    expect(probe.status, 'money cannot have left the bank in 2029, so the ledger must refuse the date').toBe(400);
  });

  test('TC-D-091 — the settlement POST refuses a missing and a forged CSRF token', async ({ adminPage, runId }) => {
    const request = await createApprovedRequest(adminPage, runId, { amount: '8400.00' });
    await reserve(adminPage, request.id);
    const form = {
      request_id: String(request.id),
      amount: '8400.00',
      paid_on: PAID_ON,
      payment_mode: 'bank_transfer',
      reference_no: `UTR-CSRF-${runId}`,
      settlement: 'settled',
      ...(await entryFields(adminPage, request.id))
    };
    for (const csrf of ['omit', 'bogus'] as const) {
      expect((await probePost(adminPage, '/payments', form, { csrf })).status, `a ${csrf} token records nothing`).toBe(
        403
      );
      expect(
        (await probePost(adminPage, `/requests/${request.id}/settlement-preview`, form, { csrf })).status,
        `nor may a ${csrf} token reach the preview`
      ).toBe(403);
    }
    const ledger = await probeGet(adminPage, `/payments?month=2026-07&q=UTR-CSRF-${runId}`);
    expect(ledger.body).toContain('No payments match these filters');
  });
});

// ===========================================================================
// M — the two refusals that strand money, and the gate that is not there
// ===========================================================================

test.describe('D · settlements the product cannot complete', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(testInfo.project.name !== 'chromium', 'These are server properties.');
  });

  test('TC-D-096 — a recoverable whose category needs no project can still be paid', async ({
    adminPage,
    browser,
    runId
  }) => {
    // FINDING F-D-11, and the most serious defect this audit found. A recoverable
    // category that requires no project gives the request no project and
    // therefore no head. paymentEntry then posts head_id=0
    // (internal/app/linking.go:244-247, templates.go:389), payments.head_id is
    // NOT NULL (internal/store/schema.go:60) and validatePayment refuses
    // HeadID == 0 (internal/store/store.go:1621-1623). The entry screen offers no
    // head selector, so there is nothing the accountant can do about it: the
    // request is stranded in processing, holding its reservation, for ever.
    //
    // Every category is driven before the assertion, so one run reports all four
    // rather than stopping at the first.
    test.fail();
    const categories = ['icd', 'security_deposit', 'employee_advance', 'other'] as const;
    const outcomes: Array<{ category: string; status: number; message: string; strandedAt: string }> = [];

    for (const category of categories) {
      const request = await createApprovedRecoverable(adminPage, browser, runId, category, '5000.00');
      expect((await reserve(adminPage, request.id)).status, `${category}: it reserves like any other`).toBe(303);

      // What the entry screen actually posts, read off the screen.
      await adminPage.goto(`/payments/new?request=${request.id}`);
      await expect(
        adminPage.locator('input[name="head_id"]'),
        `${category}: the hidden head is zero, because the request has none`
      ).toHaveValue('0');
      await expect(
        adminPage.locator('select[name="head_id"], #head'),
        `${category}: and the screen offers no way to choose one`
      ).toHaveCount(0);

      const probe = await probePost(adminPage, '/payments', {
        request_id: String(request.id),
        amount: '5000.00',
        paid_on: PAID_ON,
        payment_mode: 'bank_transfer',
        reference_no: `UTR-REC-${category}-${runId}`,
        settlement: 'settled',
        head_id: '0',
        vendor_payee: `Counterparty ${runId}`
      });
      const message = /validation failed: [^<"]+/.exec(probe.body)?.[0] ?? probe.body.slice(0, 120);
      outcomes.push({
        category,
        status: probe.status,
        message,
        strandedAt: await statusPill(adminPage, request.id)
      });
    }

    // The stranding is the harm, and it is the same for every category: the
    // reservation is still held, so nobody else can even look at it afresh.
    for (const o of outcomes) {
      if (o.status === 303) continue;
      expect(o.strandedAt, `${o.category}: the request is stuck in processing after the refusal`).toContain(RESERVED);
    }

    expect(
      outcomes.filter(o => o.status !== 303).map(o => `${o.category}: ${o.status} ${o.message}`),
      'a recoverable request that was approved must be payable — its category deciding whether it has a project cannot decide whether it can be paid at all'
    ).toEqual([]);
  });

  test('TC-D-097/098 — the settlement write is gated at least as tightly as its preview', async ({
    adminPage,
    browser,
    runId
  }) => {
    // FINDING F-D-10. POST /requests/{id}/settlement-preview — which writes
    // nothing — is gated on payment:settle (internal/app/app.go:454), while
    // POST /payments, the only writer in the flow, is gated on payment:create
    // alone (internal/app/app.go:387). RecordPaymentForRequest checks the
    // reservation and nothing else, and payment:mark_partial gates no route at
    // all, so a subject holding create+reserve can settle a request in full and
    // mark one a partial without either verb.
    test.fail();
    const heads = await headIds(adminPage);
    const head = heads.get('Operations / Office Rent')!;
    const entry = await subjectWithGrants(adminPage, browser, runId, 'entryonly', [
      'payment:view',
      'payment:create',
      'reservation:reserve',
      'request:view'
    ]);
    try {
      const full = await createApprovedRequest(adminPage, runId, { amount: '3900.00' });
      const part = await createApprovedRequest(adminPage, runId, { amount: '3800.00' });
      for (const request of [full, part]) {
        expect(
          (await reserve(entry.page, request.id)).status,
          'the subject holds reservation:reserve, so it takes the request'
        ).toBe(303);
      }

      // The read-only preview is refused, which is the asymmetry in one line.
      const preview = await probePost(entry.page, `/requests/${full.id}/settlement-preview`, { amount: '3900.00' });
      expect(preview.status, 'the pure preview is gated on payment:settle, which this subject does not hold').toBe(403);

      const settled = await probePost(entry.page, '/payments', {
        request_id: String(full.id),
        amount: '3900.00',
        paid_on: PAID_ON,
        payment_mode: 'bank_transfer',
        reference_no: `UTR-NOSETTLE-${runId}`,
        settlement: 'settled',
        head_id: head,
        vendor_payee: 'Anybody'
      });
      const partial = await probePost(entry.page, '/payments', {
        request_id: String(part.id),
        amount: '1000.00',
        paid_on: PAID_ON,
        payment_mode: 'bank_transfer',
        reference_no: `UTR-NOMARK-${runId}`,
        settlement: 'partial',
        partial_reason: `Balance to follow ${runId}`,
        head_id: head,
        vendor_payee: 'Anybody'
      });

      expect(
        [settled.status, partial.status],
        'settling and marking a partial are the two decisions payment:settle and payment:mark_partial exist for, so the writer must ask for them'
      ).toEqual([403, 403]);
    } finally {
      await entry.close();
    }
  });

  test('TC-D-099 — releasing a reservation notifies the requester and the approver, as the screen promises', async ({
    adminPage,
    browser,
    runId
  }) => {
    // FINDING F-D-12 (the sibling's SD-02, reproduced here because the promise
    // is made on this audit's screen). templates.go:1042 states "The requester
    // and the approver are both notified."; requestRelease and requestReassign
    // call no a.fire at all and internal/notify/events.go declares no release,
    // reassign or unhold event.
    test.fail();
    const request = await createApprovedRequest(adminPage, runId, { amount: '3700.00' });
    const approver = approverFor(runId);
    await reserve(adminPage, request.id);

    // The promise, on the screen, in those words.
    await adminPage.goto(`/requests/${request.id}/reservation`);
    await expect(adminPage.locator('.action-bar')).toContainText('The requester and the approver are both notified.');

    const context = await browser.newContext({ viewport: adminPage.viewportSize() ?? undefined });
    try {
      const approverPage = await context.newPage();
      await login(approverPage, approver.email, approver.password);
      const count = async (page: Page) => {
        await page.goto('/notifications?scope=all');
        return page.locator('a.notif').count();
      };
      const approverBefore = await count(approverPage);
      const requesterBefore = await count(adminPage);

      const released = await probePost(adminPage, `/requests/${request.id}/release`, {
        confirm: 'on',
        reason: `Waiting on a corrected invoice ${runId}`
      });
      expect(released.status, 'the release itself succeeds').toBe(303);

      expect(
        [(await count(approverPage)) - approverBefore, (await count(adminPage)) - requesterBefore],
        'both people the screen names must actually be told, in app or by email'
      ).toEqual([1, 1]);
    } finally {
      await context.close();
    }
  });
});

// ===========================================================================
// L — the journey at 390 px, which is a shipped guarantee
// ===========================================================================

test.describe('D · the reserve→settle journey on a phone', () => {
  test('TC-D-092 — [mobile] reserve, preview and settle at 390 px without sideways scroll', async ({
    adminPage,
    runId
  }) => {
    const errors = capturePageErrors(adminPage);
    await adminPage.setViewportSize({ width: 390, height: 850 });
    const request = await createApprovedRequest(adminPage, runId, { amount: '9100.00' });

    const noOverflow = async (where: string) => {
      const overflow = await adminPage.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth
      );
      expect(overflow, `${where} must never scroll sideways at 390 px`).toBeLessThanOrEqual(0);
    };

    await adminPage.goto(`/accounts-queue?tab=approved&q=${encodeURIComponent(request.number)}`);
    await expect(adminPage.locator('.metric-strip .metric')).toHaveCount(4);
    await expect(adminPage.locator('.segmented a')).toHaveCount(5);
    await noOverflow('the queue');

    await adminPage
      .locator('tbody tr')
      .filter({ hasText: request.number })
      .getByRole('button', { name: 'Take for processing' })
      .click();
    await expect(adminPage).toHaveURL(new RegExp(`/payments/new\\?request=${request.id}$`));
    await expect(adminPage.locator('.reserve-bar')).toContainText('Reserved by you');
    await noOverflow('the entry screen');

    await fillEntry(adminPage, { amount: '8500.00', reference: `UTR-MOBILE-${runId}`, note: `Phone ${runId}` });
    const sheet = await openSheet(adminPage);
    await expect(sheet.locator('.cmp-row.diff')).toContainText('600.00');
    await noOverflow('the settlement sheet');

    await sheet.locator('input[name="settlement"][value="settled"]').check();
    await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();
    await expect(adminPage).toHaveURL(/\/payments\/\d+$/);
    await expect(adminPage.locator('.pill.completed')).toBeVisible();
    await expect(adminPage.getByRole('link', { name: /Edit/ })).toHaveCount(0);
    await expect(adminPage.getByRole('button', { name: /Void/ })).toHaveCount(0);
    await noOverflow('the payment detail');
    expect(errors, 'and the journey logs no browser error at all').toEqual([]);
  });

  test('TC-D-093 — [mobile] the conflict screen and the release screen fit a phone', async ({
    adminPage,
    secondPage,
    runId
  }) => {
    await adminPage.setViewportSize({ width: 390, height: 850 });
    await secondPage.setViewportSize({ width: 390, height: 850 });
    const request = await createApprovedRequest(adminPage, runId, { amount: '9200.00' });
    await takeFromQueue(adminPage, request);

    const response = await secondPage.goto(`/payments/new?request=${request.id}`);
    expect(response?.status(), 'G15: a rendered screen carrying 409').toBe(409);
    await expect(secondPage.locator('.banner.bad')).toContainText('took this request before you');
    await expect(secondPage.locator('.a-list a[href="/accounts-queue"]')).toBeVisible();
    const conflictOverflow = await secondPage.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    );
    expect(conflictOverflow, 'the conflict screen must not scroll sideways at 390 px').toBeLessThanOrEqual(0);

    await adminPage.goto(`/requests/${request.id}/reservation`);
    await expect(adminPage.getByRole('button', { name: 'Release reservation' })).toBeVisible();
    await adminPage.getByLabel('Reason').fill(`Handing back ${runId}`);
    await adminPage.getByLabel('I confirm no payment has been initiated for this request').check();
    const releaseOverflow = await adminPage.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth
    );
    expect(releaseOverflow, 'nor may the release screen').toBeLessThanOrEqual(0);
    await adminPage.getByRole('button', { name: 'Release reservation' }).click();
    await expect(adminPage).toHaveURL(/\/accounts-queue$/);
  });
});
