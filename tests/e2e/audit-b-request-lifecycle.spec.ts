/**
 * Audit B — raising a payment request, and everything the requester can still
 * do to it afterwards.
 *
 * Scope: `POST /requests` and the requester's own verbs. The manager's decision
 * is a sibling's area, so where this file needs an approved / returned /
 * rejected request it puts the manager's POST in directly rather than driving
 * the approval screens.
 *
 * Three ideas the file is built around.
 *
 * 1. `hidden` is not validation. The form renders the budget and recoverable
 *    fieldsets as ALTERNATIVES, so a browser cannot post the other type's
 *    fields — which means every one of those rules can only be probed by a
 *    hand-rolled POST. That is the highest-value group here (TC-B-048..056).
 * 2. Expectations come from the code, never from the output.
 *    `store.validateRequestInput` (internal/store/requests.go:127) is the whole
 *    required/forced matrix; `requestInput` (internal/app/requests.go:298) reads
 *    every field unconditionally, so the store is the only gate.
 * 3. One worker-scoped world. Every subject in this file costs several page
 *    loads to create, and ninety tests cannot each afford four users. The world
 *    is built once per worker and never mutated by a test — anything a test
 *    needs to change (a head's active flag, a setting) it creates or restores
 *    itself.
 */
import { Buffer } from 'node:buffer';
import type { Browser, BrowserContext, Page } from '@playwright/test';
import {
  admin,
  capturePageErrors,
  createApprovedRequest,
  expect,
  login,
  settlePayment,
  test
} from './fixtures';
import {
  asRole,
  csrfToken,
  expectOutcome,
  probeAnonymous,
  probeGet,
  probePost,
  type Probe,
  type Subject
} from './audit-support';

/** PR-YYYY-NNNNNN — prefix "PR", calendar year, width 6 are the seeded defaults. */
const NUMBER_RE = /PR-\d{4}-\d{6}/;

interface Actor {
  subject: Subject;
  ctx: BrowserContext;
  page: Page;
  /** The users table's own id, read off `data-open="user-{id}"`. */
  id: string;
}

interface World {
  browser: Browser;
  runId: string;
  adminPage: Page;
  /** Holds exactly Requester: request view/create/edit/withdraw/reraise/comment/cancel, scope own. */
  requester: Actor;
  /** A second exactly-Requester subject — the one who must never see the first one's work. */
  stranger: Actor;
  /** Holds exactly Manager: approval verbs, request scope all. The default approver here. */
  manager: Actor;
  /** A second Manager, so an edit can prove the reroute lands in a different queue. */
  manager2: Actor;
  /** Signed in, permitted nothing. */
  noRole: Actor;
  vendor: { id: string; name: string };
  otherVendor: { id: string; name: string };
  inactiveVendor: { id: string; name: string };
  operationsId: string;
  peopleId: string;
  officeRentId: string;
  payrollId: string;
  /** A head created inactive: hidden from the form, but a real row a POST can name. */
  retiredHeadId: string;
}

async function userId(adminPage: Page, email: string): Promise<string> {
  await adminPage.goto('/users');
  const open = await adminPage
    .locator('tr', { hasText: email })
    .getByRole('button', { name: 'Edit' })
    .getAttribute('data-open');
  if (!open) throw new Error(`no users row for ${email}`);
  return open.replace('user-', '');
}

async function makeActor(
  adminPage: Page,
  browser: Browser,
  runId: string,
  roles: Array<'Requester' | 'Manager' | 'Accounts' | 'Admin'>,
  prefix: string
): Promise<Actor> {
  const { subject, context, page } = await asRole(adminPage, browser, runId, roles, prefix);
  return { subject, ctx: context, page, id: await userId(adminPage, subject.email) };
}

// eslint-disable-next-line @typescript-eslint/no-empty-object-type -- no new test-scoped fixtures, only a worker-scoped one
const it = test.extend<{}, { world: World }>({
  world: [
    async ({ browser }, use) => {
      const runId = `audb-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
      const adminCtx = await browser.newContext();
      const adminPage = await adminCtx.newPage();
      await login(adminPage, admin.email, admin.password);

      const requester = await makeActor(adminPage, browser, runId, ['Requester'], 'req-a');
      const stranger = await makeActor(adminPage, browser, runId, ['Requester'], 'req-b');
      const manager = await makeActor(adminPage, browser, runId, ['Manager'], 'mgr-a');
      const manager2 = await makeActor(adminPage, browser, runId, ['Manager'], 'mgr-b');
      const noRole = await makeActor(adminPage, browser, runId, [], 'nil-a');

      // Vendors through the store's own POST rather than the form: this file
      // tests what the request does with a vendor id, not how a vendor is made.
      const newVendor = async (name: string, status: string) => {
        const created = await probePost(adminPage, '/vendors', { name, vendor_type: 'company', status });
        const id = (created.location ?? '').split('/').pop() ?? '';
        if (!/^\d+$/.test(id)) throw new Error(`vendor ${name} not created: ${created.outcome}`);
        return { id, name };
      };
      const vendor = await newVendor(`Audit B Supplier ${runId}`, 'active');
      const otherVendor = await newVendor(`Audit B Unpicked ${runId}`, 'active');
      const inactiveVendor = await newVendor(`Audit B Retired ${runId}`, 'inactive');

      // Project and head ids, read from the form's own <option> values so the
      // test never guesses at the seed's autoincrement.
      await adminPage.goto('/requests/new?type=vendor_invoice');
      const ids = await adminPage.evaluate(() => {
        const value = (selector: string, label: string) =>
          Array.from(document.querySelectorAll<HTMLOptionElement>(selector)).find(
            option => (option.textContent ?? '').trim() === label
          )?.value ?? '';
        return {
          operations: value('#project option', 'Operations'),
          people: value('#project option', 'People'),
          officeRent: value('#head option', 'Operations / Office Rent'),
          payroll: value('#head option', 'People / Payroll')
        };
      });
      for (const [key, id] of Object.entries(ids)) {
        if (!/^\d+$/.test(id)) throw new Error(`seed lookup failed for ${key}: got "${id}"`);
      }

      const retiredHeadName = `Audit B Retired Head ${runId}`;
      await probePost(adminPage, '/heads', {
        project_id: ids.operations,
        name: retiredHeadName,
        sort_order: '91'
      });
      await adminPage.goto('/heads');
      const retiredForm = await adminPage
        .locator(`input[name="name"][value="${retiredHeadName}"]`)
        .first()
        .getAttribute('form');
      if (!retiredForm) throw new Error('the inactive head was not created');

      await use({
        browser,
        runId,
        adminPage,
        requester,
        stranger,
        manager,
        manager2,
        noRole,
        vendor,
        otherVendor,
        inactiveVendor,
        operationsId: ids.operations,
        peopleId: ids.people,
        officeRentId: ids.officeRent,
        payrollId: ids.payroll,
        retiredHeadId: retiredForm.replace('head-', '')
      });

      for (const actor of [requester, stranger, manager, manager2, noRole]) await actor.ctx.close();
      await adminCtx.close();
    },
    { scope: 'worker' }
  ]
});

// ---------------------------------------------------------------------------
// Bodies. One place that knows what a valid POST /requests looks like per type,
// so a "required field omitted" test is a one-key override and nothing else.
// ---------------------------------------------------------------------------

let bodySeq = 0;

function validBody(w: World, type: string, extra: Record<string, string> = {}): Record<string, string> {
  bodySeq += 1;
  const tag = `${w.runId}-${bodySeq}`;
  const common: Record<string, string> = {
    type,
    treatment: 'budget',
    short_title: `Audit B ${type} ${tag}`,
    amount: '12500',
    purpose: `Raised by the request-lifecycle audit, case ${tag}.`,
    manager_id: w.manager.id
  };
  const perType: Record<string, Record<string, string>> = {
    vendor_invoice: {
      project_id: w.operationsId,
      head_id: w.officeRentId,
      vendor_id: w.vendor.id,
      invoice_no: `INV-${tag}`,
      invoice_date: '2026-07-18'
    },
    vendor_advance: {
      project_id: w.operationsId,
      head_id: w.officeRentId,
      vendor_id: w.vendor.id,
      advance_reason: 'Deposit against the switchgear order.'
    },
    reimbursement: {
      project_id: w.operationsId,
      head_id: w.officeRentId,
      expense_date: '2026-07-17'
    },
    employee_advance: {
      treatment: 'recoverable',
      recoverable_category: 'employee_advance',
      expected_return_date: '2026-12-31',
      repayment_notes: 'Settled against the trip bills on return.',
      advance_reason: 'Float for the Hyderabad site trip.'
    },
    recoverable: {
      treatment: 'recoverable',
      recoverable_category: 'emd',
      project_id: w.operationsId,
      expected_return_date: '2026-12-31',
      repayment_notes: 'Refunded when the tender closes.'
    }
  };
  const body = { ...common, ...perType[type], ...extra };
  for (const [key, value] of Object.entries(extra)) {
    // An explicit empty string is how a test says "omit this field".
    if (value === '') delete body[key];
  }
  return body;
}

/** POSTs a request body and returns the probe plus the created id, when there is one. */
async function raise(page: Page, body: Record<string, string>): Promise<Probe & { id: string }> {
  const probe = await probePost(page, '/requests', body);
  const id = /\/requests\/(\d+)\/submitted/.exec(probe.location ?? '')?.[1] ?? '';
  return { ...probe, id };
}

/** Raises a request that must succeed, and fails loudly with the server's message if it does not. */
async function raiseOk(page: Page, body: Record<string, string>): Promise<string> {
  const probe = await raise(page, body);
  expect(
    probe.id,
    `this body must be accepted; the server answered ${probe.outcome}: ${message(probe)}`
  ).toMatch(/^\d+$/);
  return probe.id;
}

/** The `.alert.error` text a rejected form re-renders with, for a readable failure. */
function message(probe: Probe): string {
  const alert = /<div class="alert error"[^>]*>([\s\S]*?)<\/div>/.exec(probe.body)?.[1];
  const page = /<p>([\s\S]*?)<\/p>/.exec(probe.body)?.[1];
  return (alert ?? page ?? probe.body.slice(0, 200)).replace(/\s+/g, ' ').trim();
}

/**
 * Asserts a POST was refused with a message naming the rule.
 *
 * 400 rather than a redirect is the whole assertion: a validation failure that
 * still created the row would show up here as a 303.
 */
function expectRefused(probe: Probe, needle: string | RegExp, what: string) {
  expect(probe.status, `${what}: expected 400, got ${probe.outcome} — ${message(probe)}`).toBe(400);
  const text = message(probe);
  if (typeof needle === 'string') {
    expect(text, `${what}: the refusal must say why`).toContain(needle);
  } else {
    expect(text, `${what}: the refusal must say why`).toMatch(needle);
  }
}

/** The number the confirmation screen shows for a request. */
async function numberOf(page: Page, id: string): Promise<string> {
  await page.goto(`/requests/${id}`);
  return (await page.locator('.rh-no').first().innerText()).trim();
}

const sequenceOf = (number: string) => Number(number.split('-').pop());

/**
 * Mirrors store.categoryCode (internal/store/recoverables.go:99): lowercase, every
 * run of non-alphanumerics collapsed to one underscore, ends trimmed. It is how
 * an admin-entered category NAME becomes the code a request stores.
 */
function categoryCodeOf(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '');
}

/**
 * Waits for an htmx swap of #form-fields to have been applied and settled.
 *
 * Both controls that drive the swap — #rcategory and #project — live INSIDE
 * #form-fields, so the swap replaces the very element that triggered it. htmx
 * drops a request whose element is no longer in the document, and says nothing
 * when it does, so interacting with the new control before the settle has
 * finished loses the next change outright.
 *
 * The signal is the server's own: it echoes the choice as a `selected`
 * ATTRIBUTE, which `selectOption` never sets. Waiting on that is waiting for
 * the fragment the server sent back rather than for the click.
 */
async function swapSettled(page: Page, select: string, value: string) {
  await expect(page.locator(`${select} option[selected]`)).toHaveAttribute('value', value);
  await expect(page.locator('.htmx-request, .htmx-settling, .htmx-swapping')).toHaveCount(0);
}

/** A tiny PDF, so nothing on disk has to exist for an upload test. */
const samplePdf = {
  name: 'audit-proof.pdf',
  mimeType: 'application/pdf',
  buffer: Buffer.from('%PDF-1.4 audit-b evidence')
};

// ===========================================================================
// A — every type × treatment, raised through the real form
// ===========================================================================

it.describe('A · every type and treatment, through the real form', () => {
  it('TC-B-001 — a vendor invoice is raised on the budget treatment through the form', async ({ world }) => {
    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto('/requests/new');
      await expect(page.locator('.type-grid .type-card')).toHaveCount(4);
      await page.getByRole('link', { name: /Vendor invoice payment/ }).click();
      await expect(page).toHaveURL(/\/requests\/new\?type=vendor_invoice$/);

      // A16: the type is the route, never a control on the form.
      await expect(page.locator('select[name="type"]')).toHaveCount(0);
      await expect(page.locator('input[name="type"]')).toHaveValue('vendor_invoice');

      // Treatment first, then the type's own fields (T1). A vendor invoice can
      // only ever be a budget expense — the store refuses anything else for it —
      // so the form states that and submits it, rather than offering a radio it
      // would reject after the recoverable fields had been filled in.
      await expect(page.getByRole('radio', { name: /Refundable or recoverable/ })).toHaveCount(0);
      await expect(page.locator('input[type="hidden"][name="treatment"]')).toHaveValue('budget');
      await page.getByLabel('Short title').fill(`Switchgear ${world.runId}`);
      await page.locator('#project').selectOption({ label: 'Operations' });
      await expect(page.locator('#head option').filter({ hasNotText: 'Operations /' })).toHaveCount(1);
      await page.locator('#head').selectOption({ label: 'Operations / Office Rent' });

      // A Requester holds no vendor:view, so the form gives them the plain
      // select rather than the combobox (internal/app/templates.go:1919).
      await page.locator('#vendor').selectOption(world.vendor.id);
      await page.getByLabel('Amount').fill('100000');
      await expect(page.locator('.in-words')).toContainText('lakh', { ignoreCase: true });
      await page.getByLabel('Invoice number').fill(`INV-A1-${world.runId}`);
      await page.getByLabel('Invoice date').fill('2026-07-18');
      await page.getByLabel('Purpose').fill('11kV switchgear panels for the substation.');
      await page.getByLabel('Approver').selectOption(world.manager.id);
      await page.getByRole('button', { name: 'Submit request' }).click();

      await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);
      await expect(page.locator('.rh-no')).toHaveText(NUMBER_RE);
      await expect(page.locator('.rh-status .pill')).toHaveText('Awaiting approval');
      const id = new URL(page.url()).pathname.split('/')[2];

      await page.goto(`/requests/${id}`);
      await expect(page.locator('.card-head .pill')).toHaveText('Budget expense');
      await expect(page.locator('dt', { hasText: /^Vendor$/ })).toHaveCount(1);
      await expect(page.locator('.dl')).toContainText(world.vendor.name);
      await expect(page.locator('.rh-meta')).toContainText('Vendor invoice payment');
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  it('TC-B-002 — a vendor advance is raised on the budget treatment through the form', async ({ world }) => {
    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto('/requests/new');
      await page.getByRole('link', { name: /Vendor advance/ }).click();
      await expect(page).toHaveURL(/type=vendor_advance$/);
      // The advance has a reason where the invoice has a number and a date.
      await expect(page.getByLabel('Invoice number')).toHaveCount(0);
      await page.getByLabel('Short title').fill(`Order deposit ${world.runId}`);
      await page.locator('#project').selectOption(world.operationsId);
      await page.locator('#head').selectOption(world.officeRentId);
      await page.locator('#vendor').selectOption(world.vendor.id);
      await page.getByLabel('Amount').fill('45000');
      await page.getByLabel('Reason for the advance').fill('30% deposit before the order is cut.');
      await page.getByLabel('Purpose').fill('Advance against purchase order 4471.');
      await page.getByLabel('Approver').selectOption(world.manager.id);
      await page.getByRole('button', { name: 'Submit request' }).click();
      await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);

      const id = new URL(page.url()).pathname.split('/')[2];
      await page.goto(`/requests/${id}`);
      await expect(page.locator('.dl')).toContainText('30% deposit before the order is cut.');
      await expect(page.locator('.card-head .pill')).toHaveText('Budget expense');
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  it('TC-B-003 — a reimbursement is raised and its payee is the requester', async ({ world }) => {
    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto('/requests/new?type=reimbursement');
      // T11: the payee is shown read-only and posts nothing at all.
      const paidTo = page.locator('#paid-to');
      await expect(paidTo).toHaveValue(world.requester.subject.name);
      expect(await paidTo.getAttribute('name'), 'the payee is never a posted field').toBeNull();
      await expect(page.locator('#vendor')).toHaveCount(0);

      await page.getByLabel('Short title').fill(`Site visit ${world.runId}`);
      await page.locator('#project').selectOption(world.operationsId);
      await page.locator('#head').selectOption(world.officeRentId);
      await page.getByLabel('Amount').fill('18400');
      await page.getByLabel('Expense date').fill('2026-07-17');
      await page.getByLabel('Purpose').fill('Flight and two nights for the inspection.');
      await page.getByLabel('Approver').selectOption(world.manager.id);
      await page.getByRole('button', { name: 'Submit request' }).click();
      await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);

      const id = new URL(page.url()).pathname.split('/')[2];
      await page.goto(`/requests/${id}`);
      await expect(page.locator('dt', { hasText: /^Paid to$/ })).toHaveCount(1);
      await expect(page.locator('.dl')).toContainText(world.requester.subject.name);
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  it('TC-B-004 — an employee advance opens recoverable and already categorised', async ({ world }) => {
    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto('/requests/new?type=employee_advance');
      await expect(page.getByRole('radio', { name: /Refundable or recoverable/ })).toBeChecked();
      await expect(page.locator('#rcategory')).toHaveValue('employee_advance');
      // The recoverable fieldset replaces the budget one — it does not hide it.
      await expect(page.locator('#form-fields select[name="head_id"]')).toHaveCount(0);

      await page.getByLabel('Short title').fill(`Trip float ${world.runId}`);
      await page.locator('#expected-return').fill('2026-12-31');
      await page.locator('#terms').fill('Settled against the trip bills on return.');
      await page.getByLabel('Amount').fill('30000');
      await page.getByLabel('What the money is for').fill('Float for the Hyderabad site trip.');
      await page.getByLabel('Purpose').fill('Cash advance for a five-day site trip.');
      await page.getByLabel('Approver').selectOption(world.manager.id);
      await page.getByRole('button', { name: 'Submit request' }).click();
      await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);

      const id = new URL(page.url()).pathname.split('/')[2];
      await page.goto(`/requests/${id}`);
      await expect(page.locator('.card-head .pill')).toHaveText('Recoverable · Employee advance');
      await expect(page.locator('.dl')).toContainText(world.requester.subject.name);
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  it('TC-B-005 — an employee advance switched to the budget treatment asks for project and head', async ({
    world
  }) => {
    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto('/requests/new?type=employee_advance');
      await page.getByRole('radio', { name: /Budget expense/ }).check();
      // The htmx swap replaces the recoverable fieldset with the budget one.
      await expect(page.locator('#form-fields select[name="head_id"]')).toBeVisible();
      await expect(page.locator('#form-fields #rcategory')).toHaveCount(0);

      await page.getByLabel('Short title').fill(`Budgeted float ${world.runId}`);
      await page.locator('#project').selectOption(world.operationsId);
      await page.locator('#head').selectOption(world.officeRentId);
      await page.getByLabel('Amount').fill('9000');
      await page.getByLabel('What the money is for').fill('Petty cash for the site office.');
      await page.getByLabel('Purpose').fill('Non-refundable float, charged to Operations.');
      await page.getByLabel('Approver').selectOption(world.manager.id);
      await page.getByRole('button', { name: 'Submit request' }).click();
      await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);

      const id = new URL(page.url()).pathname.split('/')[2];
      await page.goto(`/requests/${id}`);
      await expect(page.locator('.card-head .pill')).toHaveText('Budget expense');
      await expect(page.locator('.dl')).toContainText('Operations');
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  // REWRITTEN. This case was named "…and no form, yet POST /requests accepts
  // it" and its middle assertion said an unknown `?type=` "falls back to the
  // chooser". That silent fallback is gone: `requestNew`
  // (internal/app/requests.go:105-117) answers **400** for any `?type=` naming
  // no card, carrying `unofferedRequestType` (:100) as the reason, and only a
  // bare /requests/new is the ordinary 200 chooser. A URL that names something
  // the system will not raise is a client error about that URL, and answering
  // 200 said the opposite.
  //
  // The gap the case exists for is unchanged and still pinned below: the
  // store's fifth type has no way in through the UI, and a hand-rolled POST
  // raising one is accepted and stored.
  it('TC-B-006 — the recoverable type has no card, ?type=recoverable is refused, yet POST /requests accepts it', async ({
    world
  }) => {
    // requestTypeOptions (internal/app/requests.go:42) carries four types;
    // store.requestTypes (internal/store/requests.go:78) carries five.
    const page = world.requester.page;
    await page.goto('/requests/new');
    await expect(
      page.getByRole('link', { name: /Recoverable payment/ }),
      'there is no chooser card for the recoverable type'
    ).toHaveCount(0);

    const refused = await probeGet(page, '/requests/new?type=recoverable');
    expect(
      refused.status,
      `a ?type= naming no card is refused, never quietly ignored; got ${refused.outcome}`
    ).toBe(400);
    expect(refused.body, 'and the reader is told what was wrong with the URL they arrived on').toContain(
      'That is not a request type this system raises'
    );
    expect(refused.body, 'on the chooser itself, which is the nearest screen there is').toContain(
      'What are you asking to be paid?'
    );

    const id = await raiseOk(page, validBody(world, 'recoverable'));
    await page.goto(`/requests/${id}`);
    await expect(page.locator('.card-head .pill')).toHaveText('Recoverable · EMD — earnest money deposit');
    // typeLabel falls through to the raw enum for a type with no label.
    await expect(page.locator('.rh-meta')).toContainText('recoverable');
  });

  it('TC-B-007 — the treatment radio swaps #form-fields and the fieldsets are alternatives', async ({
    world
  }) => {
    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      const swaps: string[] = [];
      page.on('request', request => {
        if (request.url().includes('/requests/new/fields')) swaps.push(request.url());
      });

      // An employee advance, because it is the one type the store accepts a
      // recoverable treatment for and therefore the one type the form offers the
      // choice on. A vendor invoice states its treatment instead of offering a
      // radio it would reject — see TC-B-001.
      await page.goto('/requests/new?type=employee_advance');
      await expect(page.locator('#form-fields select[name="head_id"]')).toBeVisible();

      await page.getByRole('radio', { name: /Refundable or recoverable/ }).check();
      await expect(page.locator('#form-fields textarea[name="repayment_notes"]')).toBeVisible();
      await expect(
        page.locator('#form-fields select[name="head_id"]'),
        'a hidden head would post a stale head_id — the fieldsets must be alternatives'
      ).toHaveCount(0);

      await page.getByRole('radio', { name: /Budget expense/ }).check();
      await expect(page.locator('#form-fields select[name="head_id"]')).toBeVisible();
      await expect(page.locator('#form-fields textarea[name="repayment_notes"]')).toHaveCount(0);

      expect(swaps.length, 'each treatment change asks the server for the fieldset').toBeGreaterThanOrEqual(2);
      expect(swaps[0], 'the swap carries the type and the treatment').toContain('treatment=recoverable');
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  it('TC-B-008 — the category select swaps the fieldset and keeps what was typed', async ({ world }) => {
    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto('/requests/new?type=employee_advance');

      // Every pick waits for the swap it depends on — see swapSettled.
      const pick = async (category: string) => {
        await page.locator('#rcategory').selectOption(category);
        await swapSettled(page, '#rcategory', category);
      };

      await page.locator('#terms').fill('Refunded when the tender closes.');
      await page.locator('#expected-return').fill('2026-11-30');

      await pick('emd');
      await expect(page.locator('#rproject'), 'EMD always belongs to a project').toBeVisible();
      await expect(page.locator('#counterparty')).toHaveCount(0);
      await expect(page.locator('#terms'), 'the swap must not punish the typist').toHaveValue(
        'Refunded when the tender closes.'
      );
      await expect(page.locator('#expected-return')).toHaveValue('2026-11-30');

      await pick('icd');
      await expect(page.locator('#counterparty'), 'an ICD needs a counterparty').toBeVisible();
      await expect(page.locator('#rproject')).toHaveCount(0);

      await pick('security_deposit');
      await expect(page.locator('#counterparty'), 'so does a security deposit').toBeVisible();

      await pick('other');
      await expect(page.locator('#counterparty'), '"Other" asks for neither').toHaveCount(0);
      await expect(page.locator('#rproject')).toHaveCount(0);
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  it('TC-B-009 — choosing a project narrows the heads to that project own', async ({ world }) => {
    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto('/requests/new?type=vendor_invoice');
      const all = await page.locator('#head option').count();
      expect(all, 'every head is offered until a project is chosen').toBeGreaterThan(4);

      await page.locator('#project').selectOption(world.peopleId);
      await swapSettled(page, '#project', world.peopleId);
      // Only the "Choose a head" placeholder survives the filter.
      await expect(page.locator('#head option').filter({ hasNotText: 'People /' })).toHaveCount(1);
      await expect(page.locator('#head option', { hasText: 'Operations /' })).toHaveCount(0);

      await page.locator('#project').selectOption(world.operationsId);
      await swapSettled(page, '#project', world.operationsId);
      await expect(page.locator('#head option').filter({ hasNotText: 'Operations /' })).toHaveCount(1);
      await expect(page.locator('#head option', { hasText: 'People /' })).toHaveCount(0);
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  it('TC-B-010 — GET /requests/new/fields answers a bare fragment and needs request:create', async ({
    world
  }) => {
    const fragment = await probeGet(
      world.requester.page,
      `/requests/new/fields?type=employee_advance&treatment=recoverable&recoverable_category=icd`
    );
    expect(fragment.status, 'the fragment is a read').toBe(200);
    expect(fragment.body, 'a partial carries no shell').not.toContain('<!doctype html>');
    expect(fragment.body, 'the ICD rule reveals the counterparty').toContain('name="counterparty"');
    expect(fragment.body, 'a recoverable fieldset carries no head').not.toContain('name="head_id"');

    const refused = await probeGet(world.noRole.page, '/requests/new/fields?type=vendor_invoice');
    expectOutcome(refused, [403], 'a caller with no role cannot reach the fieldset fragment');
  });
});

// ===========================================================================
// B — the validation matrix, one cell per case
// ===========================================================================

it.describe('B · the required, forced and optional matrix', () => {
  it('TC-B-011 — an absent amount is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'vendor_invoice', { amount: '' }));
    expectRefused(probe, 'enter the amount you are requesting', 'no amount');
  });

  it('TC-B-012 — an amount of zero is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'vendor_invoice', { amount: '0' }));
    expectRefused(probe, 'enter the amount you are requesting', 'zero');
  });

  it('TC-B-013 — a negative amount is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'vendor_invoice', { amount: '-500' }));
    expectRefused(probe, 'enter the amount you are requesting', 'negative');
  });

  it('TC-B-014 — a whitespace-only amount is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'vendor_invoice', { amount: '   ' }));
    expectRefused(probe, 'enter the amount you are requesting', 'whitespace');
  });

  it('TC-B-015 — a non-numeric amount is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'vendor_invoice', { amount: 'one hundred' })
    );
    expectRefused(probe, 'enter the amount you are requesting', 'not a number');
  });

  it('TC-B-016 — an amount with grouping separators and a rupee sign is accepted', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'vendor_invoice', { amount: '₹ 1,00,000.50' }));
    const page = world.requester.page;
    await page.goto(`/requests/${id}`);
    // C3: 10000050 paise, rendered once, with exactly one ₹.
    const amount = (await page.locator('.rh-amt').innerText()).trim();
    expect(amount, 'money renders once with a single symbol').toBe('₹1,00,000.50');
    expect(amount.match(/₹/g)?.length, 'FormatPaise already carries the symbol').toBe(1);
  });

  it('TC-B-017 — a very large amount that fits int64 paise round-trips exactly', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'vendor_invoice', { amount: '99999999999' }));
    await world.requester.page.goto(`/requests/${id}`);
    await expect(world.requester.page.locator('.rh-amt')).toHaveText('₹99,99,99,99,999.00');
  });

  // Regression guard for F-B-01. Go's float-to-int64 conversion saturates rather
  // than wrapping, so an amount whose paise exceed int64 used to arrive as a
  // positive MaxInt64 — a different number from the one submitted — and
  // ParsePaise's own "non-positive amounts are rejected" guard never fired.
  // money.ParsePaise now checks the rounded paise against float64(MaxInt64)
  // BEFORE the conversion (internal/money/money.go:36-39), so an overflowing
  // amount is the requester's error, reported with a sentence, and never a
  // silently substituted figure.
  it('TC-B-018 — an amount whose paise overflow int64 is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'vendor_invoice', { amount: '1e300' }));
    expectRefused(probe, 'enter the amount you are requesting', 'int64 overflow');
  });

  it('TC-B-019 — a blank short title is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'vendor_invoice', { short_title: '   ' }));
    expectRefused(probe, 'a short title is required', 'no title');
  });

  it('TC-B-020 — a blank purpose is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'vendor_invoice', { purpose: ' \n ' }));
    expectRefused(probe, 'purpose is required', 'no purpose');
  });

  it('TC-B-021 — no approver is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'vendor_invoice', { manager_id: '' }));
    expectRefused(probe, 'choose an approver', 'no approver');
  });

  it('TC-B-022 — naming yourself as the approver is refused (G8)', async ({ world }) => {
    const page = world.requester.page;
    await page.goto('/requests/new?type=vendor_invoice');
    await expect(
      page.locator(`#approver option[value="${world.requester.id}"]`),
      'the requester is structurally absent from their own approver list'
    ).toHaveCount(0);

    const probe = await raise(page, validBody(world, 'vendor_invoice', { manager_id: world.requester.id }));
    expectRefused(probe, 'you cannot approve your own request', 'self-approval');
  });

  it('TC-B-023 — a treatment that is neither budget nor recoverable is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'vendor_invoice', { treatment: 'capex' })
    );
    expectRefused(probe, 'treatment must be budget or recoverable', 'bad treatment');
  });

  it('TC-B-024 — an unknown request type is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'vendor_invoice', { type: 'bribe' }));
    expect(probe.status, `an unknown type must not create anything; got ${probe.outcome}`).toBe(400);
    expect(message(probe), 'the refusal names the problem').toContain('not a kind of request');
  });

  it('TC-B-025 — a malformed needed-by date is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'vendor_invoice', { needed_by: '31/07/2026' })
    );
    expectRefused(probe, 'required-by date is invalid', 'bad needed_by');
  });

  it('TC-B-026 — a vendor invoice without a project is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'vendor_invoice', { project_id: '' }));
    expectRefused(probe, 'project and head are required', 'no project');
  });

  it('TC-B-027 — a vendor invoice without a head is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'vendor_invoice', { head_id: '' }));
    expectRefused(probe, 'project and head are required', 'no head');
  });

  it('TC-B-028 — a vendor invoice without a vendor is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'vendor_invoice', { vendor_id: '' }));
    expectRefused(probe, 'choose a vendor from the vendor master', 'no vendor');
  });

  it('TC-B-029 — a vendor invoice without an invoice number is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'vendor_invoice', { invoice_no: '  ' }));
    expectRefused(probe, 'the invoice number is required', 'no invoice number');
  });

  it('TC-B-030 — a vendor invoice without a valid invoice date is refused', async ({ world }) => {
    const absent = await raise(world.requester.page, validBody(world, 'vendor_invoice', { invoice_date: '' }));
    expectRefused(absent, 'the invoice date is required', 'no invoice date');
    const bad = await raise(world.requester.page, validBody(world, 'vendor_invoice', { invoice_date: '2026-13-45' }));
    expectRefused(bad, 'invoice date is invalid', 'malformed invoice date');
  });

  it('TC-B-031 — a vendor advance without a reason is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'vendor_advance', { advance_reason: ' ' })
    );
    expectRefused(probe, 'say what the advance is for', 'no advance reason');
  });

  it('TC-B-032 — a reimbursement without an expense date is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'reimbursement', { expense_date: '' }));
    expectRefused(probe, 'the expense date is required', 'no expense date');
  });

  it('TC-B-033 — a reimbursement without a project and head is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'reimbursement', { project_id: '', head_id: '' })
    );
    expectRefused(probe, 'project and head are required', 'no charge line');
  });

  it('TC-B-034 — an employee advance without a reason is refused on either treatment', async ({ world }) => {
    const recoverable = await raise(
      world.requester.page,
      validBody(world, 'employee_advance', { advance_reason: '' })
    );
    expectRefused(recoverable, 'say what the money is for', 'recoverable advance, no reason');

    const budget = await raise(
      world.requester.page,
      validBody(world, 'employee_advance', {
        treatment: 'budget',
        project_id: world.operationsId,
        head_id: world.officeRentId,
        advance_reason: ''
      })
    );
    expectRefused(budget, 'say what the money is for', 'budget advance, no reason');
  });

  it('TC-B-035 — a recoverable employee advance without an expected return date is refused', async ({
    world
  }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'employee_advance', { expected_return_date: '' })
    );
    expectRefused(probe, 'expected return date is required for recoverables', 'no return date');
  });

  it('TC-B-036 — a recoverable employee advance without repayment terms is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'employee_advance', { repayment_notes: '   ' })
    );
    expectRefused(probe, 'repayment or refund terms are required', 'no terms');
  });

  it('TC-B-037 — a recoverable with an unknown category is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'recoverable', { recoverable_category: 'slush_fund' })
    );
    // The rule fires. What the requester is told about it is F-B-02, proved by TC-B-091.
    expect(probe.status, `an unknown category must not create anything; got ${probe.outcome}`).toBe(400);
  });

  it('TC-B-038 — an EMD recoverable without a project is refused', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'recoverable', { project_id: '' }));
    expect(probe.status, `EMD always belongs to a project; got ${probe.outcome}`).toBe(400);
  });

  it('TC-B-039 — an ICD recoverable without a counterparty is refused, and with one is accepted', async ({
    world
  }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'recoverable', { recoverable_category: 'icd', project_id: '' })
    );
    expect(probe.status, `an ICD needs a counterparty; got ${probe.outcome}`).toBe(400);

    const ok = await raiseOk(
      world.requester.page,
      validBody(world, 'recoverable', {
        recoverable_category: 'icd',
        project_id: '',
        counterparty: `Sundaram Finance ${world.runId}`
      })
    );
    await world.requester.page.goto(`/requests/${ok}`);
    await expect(world.requester.page.locator('.dl')).toContainText(`Sundaram Finance ${world.runId}`);
  });

  it('TC-B-040 — a reimbursement payee is forced to the requester even when the POST names a vendor', async ({
    world
  }) => {
    const id = await raiseOk(
      world.requester.page,
      validBody(world, 'reimbursement', {
        vendor_id: world.vendor.id,
        vendor_payee: 'Cayman Holdings Ltd'
      })
    );
    const page = world.requester.page;
    await page.goto(`/requests/${id}`);
    // CreateRequest clears VendorID and overwrites VendorPayee with actor.Name.
    await expect(page.locator('dt', { hasText: /^Paid to$/ }), 'no vendor was stored').toHaveCount(1);
    await expect(page.locator('.dl')).toContainText(world.requester.subject.name);
    await expect(page.locator('.dl')).not.toContainText('Cayman Holdings Ltd');
    await expect(page.locator('.dl')).not.toContainText(world.vendor.name);
  });

  it('TC-B-041 — an employee advance payee is forced to the requester', async ({ world }) => {
    const id = await raiseOk(
      world.requester.page,
      validBody(world, 'employee_advance', {
        vendor_id: world.vendor.id,
        vendor_payee: `${world.stranger.subject.name}`
      })
    );
    const page = world.requester.page;
    await page.goto(`/requests/${id}`);
    await expect(page.locator('.dl')).toContainText(world.requester.subject.name);
    await expect(page.locator('.dl')).not.toContainText(world.vendor.name);
  });

  it('TC-B-042 — a needed-by date in the past is accepted', async ({ world }) => {
    // validateRequestInput checks the format of needed_by and nothing else.
    const id = await raiseOk(
      world.requester.page,
      validBody(world, 'vendor_invoice', { needed_by: '2019-01-01' })
    );
    await world.requester.page.goto(`/requests/${id}`);
    await expect(world.requester.page.locator('.dl')).toContainText('1 January 2019');
  });

  it('TC-B-043 — an expected return date before today is accepted', async ({ world }) => {
    const id = await raiseOk(
      world.requester.page,
      validBody(world, 'recoverable', { expected_return_date: '2019-06-30' })
    );
    await world.requester.page.goto(`/requests/${id}`);
    await expect(world.requester.page.locator('.dl')).toContainText('30 June 2019');
  });

  it('TC-B-044 — a purpose at length round-trips', async ({ world }) => {
    const long = `Head. ${'The audit needs a long purpose. '.repeat(600)}Tail.`;
    expect(long.length, 'long enough to matter').toBeGreaterThan(18000);
    const id = await raiseOk(world.requester.page, validBody(world, 'vendor_invoice', { purpose: long }));
    await world.requester.page.goto(`/requests/${id}`);
    const shown = await world.requester.page.locator('dd').filter({ hasText: 'Tail.' }).innerText();
    expect(shown.startsWith('Head.'), 'nothing was truncated at the front').toBe(true);
    expect(shown.trim().endsWith('Tail.'), 'nothing was truncated at the back').toBe(true);
  });

  it('TC-B-045 — HTML and script in text fields render escaped, never executed', async ({ world }) => {
    const title = `<script>window.__pwned=1</script><b>bold</b> ${world.runId}`;
    const purpose = `<img src=x onerror="window.__pwned=2"> and <iframe src="/"></iframe>`;
    const id = await raiseOk(
      world.requester.page,
      validBody(world, 'vendor_invoice', { short_title: title, purpose })
    );

    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto(`/requests/${id}`);
      await expect(page.locator('h1'), 'the markup is text, not markup').toHaveText(title);
      expect(await page.locator('h1 script, h1 b').count(), 'nothing was parsed as an element').toBe(0);
      expect(await page.locator('main img, main iframe').count(), 'no injected node exists').toBe(0);
      expect(await page.evaluate(() => (window as unknown as { __pwned?: number }).__pwned), 'no script ran')
        .toBeUndefined();
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  // F-B-03, fixed. The store — not the form — is what stands between a retired
  // head and a new request: `validateRequestInput` now checks that the named head
  // is one an active project still offers, so a hand-rolled POST naming a head
  // nobody may charge to any more is refused. This case is the regression guard
  // on that check; the form hiding the option is not evidence of anything.
  it('TC-B-046 — an inactive head named in the POST is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'vendor_invoice', { head_id: world.retiredHeadId })
    );
    expectRefused(probe, /inactive|head/i, 'an inactive head named directly in the POST');
  });

  it('TC-B-047 — a request keeps showing a head that was retired after it was raised', async ({ world }) => {
    const name = `Audit B Live Head ${world.runId}-${bodySeq}`;
    await probePost(world.adminPage, '/heads', {
      project_id: world.operationsId,
      name,
      active: 'on',
      sort_order: '92'
    });
    await world.adminPage.goto('/heads');
    const formId = await world.adminPage
      .locator(`input[name="name"][value="${name}"]`)
      .first()
      .getAttribute('form');
    const headId = (formId ?? '').replace('head-', '');
    expect(headId, 'the head under test exists').toMatch(/^\d+$/);

    const id = await raiseOk(world.requester.page, validBody(world, 'vendor_invoice', { head_id: headId }));

    // Retire it, the way an administrator would.
    await probePost(world.adminPage, '/heads', {
      id: headId,
      project_id: world.operationsId,
      name,
      sort_order: '92'
    });

    await world.requester.page.goto(`/requests/${id}`);
    await expect(
      world.requester.page.locator('.dl'),
      'T12: a historical request still names the head it was charged to'
    ).toContainText(name);

    await world.requester.page.goto('/requests/new?type=vendor_invoice');
    await expect(
      world.requester.page.locator(`#head option[value="${headId}"]`),
      'and the retired head leaves the new-request form'
    ).toHaveCount(0);
    await expect(
      world.requester.page.locator(`#head option[value="${world.retiredHeadId}"]`),
      'T12: a head that was never active is never offered either'
    ).toHaveCount(0);
  });

  // F-B-02, fixed — annotation retired. `renderRejectedRequestForm` looked the
  // type up in `requestTypeLabels` and the `recoverable` type is not in it, so
  // every refusal of that type answered one generic sentence — "that is not a
  // kind of request this system raises" — about a request the store had accepted
  // seconds earlier, and threw the whole submission away with it. It now tells
  // the two apart by the store's own message rather than by a second copy of the
  // type vocabulary (internal/app/requests.go:249-275): an unknown type keeps the
  // vocabulary sentence, and a refused `recoverable` keeps **the rule that
  // refused it** and comes back on the chooser.
  //
  // WHAT THIS CASE NO LONGER ASSERTS, and why that is not a weakening. It
  // demanded the form come back "with their typing still in it". There is no
  // form to come back to: `recoverable` has no chooser card, and TC-B-006 pins
  // that `/requests/new?type=recoverable` is refused outright — so the product
  // answered the second half of F-B-02 by removing the screen rather than by
  // repopulating it. The typing-survives-a-refusal rule is real and is asserted
  // below against a type that *does* have a form, which is where it can hold.
  it('TC-B-091 — a refused recoverable-type submit names the rule that refused it', async ({ world }) => {
    const probe = await raise(world.requester.page, validBody(world, 'recoverable', { repayment_notes: '' }));
    expect(probe.status, 'it is refused, which is the rule working').toBe(400);
    expect(message(probe), 'the requester must be told which rule refused them').toContain(
      'repayment or refund terms are required'
    );
    expect(
      message(probe),
      'and where to go, since this type has no form of its own to be returned to'
    ).toContain('start again from a request type');
    expect(probe.body, 'which is the chooser').toContain('What are you asking to be paid?');
    expect(
      probe.body,
      'never the vocabulary sentence: the store raises this type, so saying it does not is the defect'
    ).not.toContain('not a kind of request this system raises');

    // The same rule, one type over, where there is a form: a carded type comes
    // back with the fieldset and the typing intact.
    const carded = await raise(
      world.requester.page,
      validBody(world, 'employee_advance', { repayment_notes: '', purpose: `Kept typing ${world.runId}` })
    );
    expect(carded.status, 'refused for the same reason').toBe(400);
    expect(carded.body, 'and its form does come back').toContain('name="repayment_notes"');
    expect(carded.body, 'with what was typed still in it').toContain(`Kept typing ${world.runId}`);
  });
});

// ===========================================================================
// C — hidden is not validation
// ===========================================================================

it.describe('C · hidden is not validation — the server re-enforces every reveal', () => {
  it('TC-B-048 — a vendor invoice posted as recoverable is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'vendor_invoice', {
        treatment: 'recoverable',
        recoverable_category: 'emd',
        expected_return_date: '2026-12-31',
        repayment_notes: 'Refunded on close.'
      })
    );
    expectRefused(probe, 'a vendor invoice is a budget expense', 'vendor invoice as recoverable');
  });

  it('TC-B-049 — a vendor advance posted as recoverable is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'vendor_advance', {
        treatment: 'recoverable',
        recoverable_category: 'emd',
        expected_return_date: '2026-12-31',
        repayment_notes: 'Refunded on close.'
      })
    );
    expectRefused(probe, 'a vendor advance is a budget expense', 'vendor advance as recoverable');
  });

  it('TC-B-050 — a reimbursement posted as recoverable is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'reimbursement', {
        treatment: 'recoverable',
        recoverable_category: 'other',
        expected_return_date: '2026-12-31',
        repayment_notes: 'Repaid from salary.'
      })
    );
    expectRefused(probe, 'reimbursement is a budget expense', 'reimbursement as recoverable');
  });

  it('TC-B-051 — the recoverable type posted as a budget expense is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'recoverable', {
        treatment: 'budget',
        head_id: world.officeRentId
      })
    );
    // Refused, as the rule demands. The message it is refused with is F-B-02.
    expect(probe.status, `a recoverable must stay recoverable; got ${probe.outcome}`).toBe(400);
  });

  it('TC-B-052 — a recoverable employee advance carrying only the budget fieldset is refused', async ({
    world
  }) => {
    // The browser can never send this: the recoverable fieldset replaces the
    // budget one. Only a hand-rolled POST reaches the rule.
    const probe = await raise(
      world.requester.page,
      validBody(world, 'employee_advance', {
        recoverable_category: '',
        expected_return_date: '',
        repayment_notes: '',
        project_id: world.operationsId,
        head_id: world.officeRentId
      })
    );
    expectRefused(probe, 'choose a recoverable category', 'budget fieldset on a recoverable advance');
  });

  it('TC-B-053 — a budget employee advance carrying only the recoverable fieldset is refused', async ({
    world
  }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'employee_advance', {
        treatment: 'budget',
        counterparty: 'Somebody Ltd',
        expected_return_date: '2026-12-31',
        repayment_notes: 'Terms that do not belong on a budget expense.'
      })
    );
    expectRefused(probe, 'project and head are required', 'recoverable fieldset on a budget advance');
  });

  it('TC-B-054 — a reimbursement posted with the whole vendor fieldset stores no vendor', async ({ world }) => {
    const id = await raiseOk(
      world.requester.page,
      validBody(world, 'reimbursement', {
        vendor_id: world.otherVendor.id,
        invoice_no: 'FORGED-1',
        invoice_date: '2026-07-01',
        advance_reason: 'Not a field this type has.'
      })
    );
    const page = world.requester.page;
    await page.goto(`/requests/${id}`);
    await expect(page.locator('.dl'), 'the vendor was dropped').not.toContainText(world.otherVendor.name);
    await expect(page.locator('.dl'), 'and the payee is the requester').toContainText(
      world.requester.subject.name
    );
    await expect(page.locator('dt', { hasText: /^Vendor$/ })).toHaveCount(0);
    // What it does with the rest of that fieldset is F-B-04, proved by TC-B-092.
  });

  it('TC-B-055 — a vendor invoice posted with the reimbursement fieldset is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'vendor_invoice', {
        vendor_id: '',
        invoice_no: '',
        invoice_date: '',
        expense_date: '2026-07-17'
      })
    );
    expectRefused(probe, 'choose a vendor from the vendor master', 'reimbursement fieldset on an invoice');
  });

  // F-B-05, fixed. The form only ever offers the chosen project's own heads, and
  // the store now insists the two agree rather than merely that both are present.
  // This case guards that agreement: a head belonging to another project charges
  // the wrong project's budget, which is a number somebody reports on.
  it('TC-B-056 — a head belonging to another project is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'vendor_invoice', { project_id: world.operationsId, head_id: world.payrollId })
    );
    expectRefused(probe, /head/i, 'a head from a different project');
  });

  // F-B-04, fixed. CreateRequest used to write every column it was handed
  // whatever the type was, and request_detail prints any column that is
  // non-empty — so a forged invoice number on a reimbursement was stored and
  // then displayed as if the reimbursement had one. The store now clears the
  // columns a type does not own. This case guards that scrub: a reimbursement's
  // detail screen must never show invoice or advance fields.
  it('TC-B-092 — a field belonging to another type is neither stored nor shown', async ({ world }) => {
    const id = await raiseOk(
      world.requester.page,
      validBody(world, 'reimbursement', {
        vendor_id: world.otherVendor.id,
        invoice_no: `FORGED-${world.runId}`,
        invoice_date: '2026-07-01',
        advance_reason: 'Not a field this type has.'
      })
    );
    await world.requester.page.goto(`/requests/${id}`);
    const detail = await world.requester.page.locator('.dl').innerText();
    expect(detail.includes(`FORGED-${world.runId}`), 'a reimbursement has no invoice number').toBe(false);
    expect(detail.includes('Not a field this type has.'), 'and no advance reason').toBe(false);
  });

  // F-B-15, fixed. CreateRequest used to write counterparty,
  // expected_return_date and repayment_notes for every treatment, and
  // request_detail prints each one whenever it is non-empty — so the concealed
  // recoverable fieldset, posted by hand, appeared on a budget expense. The
  // store now clears the columns a treatment does not own. This case guards that
  // scrub, and with it a sibling audit's DS3: a budget expense carries no
  // counterparty, no return date and no repayment terms.
  it('TC-B-096 — the recoverable fieldset posted on a budget request is neither stored nor shown', async ({
    world
  }) => {
    const id = await raiseOk(
      world.requester.page,
      validBody(world, 'vendor_invoice', {
        counterparty: `Shadow Counterparty ${world.runId}`,
        expected_return_date: '2027-03-31',
        repayment_notes: 'Terms that do not belong on a budget expense.'
      })
    );
    const page = world.requester.page;
    await page.goto(`/requests/${id}`);
    await expect(page.locator('.card-head .pill'), 'it is a budget expense').toHaveText('Budget expense');
    const detail = await page.locator('.dl').innerText();
    expect(detail.includes(`Shadow Counterparty ${world.runId}`), 'a budget expense has no counterparty').toBe(
      false
    );
    expect(detail.includes('31 March 2027'), 'and no expected return date').toBe(false);
    expect(detail.includes('Terms that do not belong'), 'and no repayment terms').toBe(false);
  });

  it('TC-B-097 — an admin-added recoverable category is enforced on submit and stored as itself', async ({
    world
  }) => {
    // V4 in the half that works. The category rules are read from
    // recoverable_categories rows (internal/store/recoverables.go:119), and
    // requestCreate reads recoverable_category straight off the form — the
    // rewriting normalizeRecoverableCategory does is reached only by the htmx
    // fragment, so a crafted submit is NOT silently refiled under EMD.
    const name = `Retention deposit ${world.runId}`;
    const code = categoryCodeOf(name);
    const added = await probePost(world.adminPage, '/configuration/recoverable-categories', {
      name,
      requires: 'counterparty',
      active: 'on',
      sort_order: '80'
    });
    expect(added.status, `the category must be created; got ${added.outcome}`).toBeLessThan(400);
    await world.adminPage.goto('/configuration');
    // `td.t-lead`, not any `td`: F-E-06's delete control names the category
    // again for a screen reader — `Delete<span class="sr-only"> {{.Name}}</span>`
    // (internal/app/templates.go:3324) — so a bare `td` filter now matches two
    // cells in the one row. The lead cell is the listing; the second is the
    // accessible name of a button, and counting it as a second listing would be
    // reading the fix as a duplicate.
    await expect(world.adminPage.locator('td.t-lead', { hasText: name })).toHaveCount(1);

    const refused = await raise(
      world.requester.page,
      validBody(world, 'recoverable', { recoverable_category: code, project_id: '' })
    );
    expect(
      refused.status,
      `the counterparty rule the admin chose must be enforced; got ${refused.outcome}`
    ).toBe(400);

    const id = await raiseOk(
      world.requester.page,
      validBody(world, 'recoverable', {
        recoverable_category: code,
        project_id: '',
        counterparty: `Retention Counterparty ${world.runId}`
      })
    );
    await world.requester.page.goto(`/requests/${id}`);
    await expect(
      world.requester.page.locator('.card-head .pill'),
      'the code is stored as itself — and rendered raw, because recoverableCategoryLabels is a hardcoded map'
    ).toHaveText(`Recoverable · ${code}`);
  });

  // F-B-17 · F-E-02, fixed — annotation retired. The category `<select>` was
  // hardcoded to the six seeded codes and `normalizeRecoverableCategory`
  // rewrote anything else, so a category an administrator added was enforceable
  // on submit (TC-B-097) and unreachable on the form: the only way to raise one
  // was to hand-roll the POST. The `<select>` now ranges over `.Categories` —
  // the live `recoverable_categories` rows (internal/app/templates.go:1826-1830)
  // — and `normalizeRecoverableCategory` (internal/app/requests.go:171-) checks
  // against those rows instead of substituting a code, so the htmx swap keeps
  // the category it was given and reveals the field that category's own rule
  // requires. This case is the regression guard on the whole path from the
  // configuration screen to the fieldset.
  it('TC-B-098 — an admin-added recoverable category is offered on the form and survives a swap', async ({
    world
  }) => {
    const name = `Retention bond ${world.runId}`;
    const code = categoryCodeOf(name);
    const added = await probePost(world.adminPage, '/configuration/recoverable-categories', {
      name,
      requires: 'counterparty',
      active: 'on',
      sort_order: '81'
    });
    expect(added.status, `the category must be created; got ${added.outcome}`).toBeLessThan(400);

    const page = world.requester.page;
    await page.goto('/requests/new?type=employee_advance');
    await expect(
      page.locator(`#rcategory option[value="${code}"]`),
      'the form must offer every active category'
    ).toHaveCount(1);

    const fragment = await probeGet(
      page,
      `/requests/new/fields?type=employee_advance&treatment=recoverable&recoverable_category=${code}`
    );
    expect(fragment.body, 'and the swap must keep the category it was given').toContain(
      `value="${code}" selected`
    );
    expect(fragment.body, 'and reveal the field that category rule requires').toContain('name="counterparty"');
  });
});

// ===========================================================================
// D — the vendor combobox
// ===========================================================================

it.describe('D · the vendor control', () => {
  it('TC-B-057 — choosing a vendor writes the hidden id, and the visible text is never read', async ({
    world
  }) => {
    // The admin holds vendor:view, so the admin gets the real combobox.
    const page = await world.adminPage.context().newPage();
    const failures = capturePageErrors(page);
    try {
      await login(page, admin.email, admin.password).catch(() => undefined);
      await page.goto('/requests/new?type=vendor_invoice');
      await page.locator('#vendor').pressSequentially(world.vendor.name.slice(0, 18));
      await page.locator('#vendor-options .co', { hasText: world.vendor.name }).first().click();
      await expect(page.locator('#vendor-id')).toHaveValue(world.vendor.id);
      expect(
        await page.locator('#vendor').getAttribute('name'),
        'the visible box posts a search term, never the payee'
      ).toBe('q');
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }

    // And the server reads only the hidden id: a lying visible text changes nothing.
    const id = await raiseOk(
      world.requester.page,
      validBody(world, 'vendor_invoice', { vendor_id: world.vendor.id, q: 'Some Other Company' })
    );
    await world.requester.page.goto(`/requests/${id}`);
    await expect(world.requester.page.locator('.dl')).toContainText(world.vendor.name);
    await expect(world.requester.page.locator('.dl')).not.toContainText('Some Other Company');
  });

  // Regression guard for F-B-06. needsVendor only checks VendorID > 0, so a
  // forged id still reaches the INSERT and `PRAGMA foreign_keys=ON` still refuses
  // it — but `classify` (internal/store/store.go:1856) now recognises a FOREIGN
  // KEY violation and returns ErrValidation, so the requester meets a 400 they
  // can act on instead of a 500. The driver text stays out of the page.
  it('TC-B-058 — a vendor_id naming no vendor is refused, not answered with a server error', async ({
    world
  }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'vendor_invoice', { vendor_id: '99999999' })
    );
    expect(
      probe.status,
      `a forged vendor id must be a validation refusal, not a server error; got ${probe.outcome}`
    ).toBe(400);
  });

  it('TC-B-059 — a vendor_id naming a real vendor the requester never searched for is accepted', async ({
    world
  }) => {
    // needsVendor (internal/store/requests.go:167) checks VendorID > 0 and the
    // foreign key checks existence. Selection is not part of the contract.
    const id = await raiseOk(
      world.requester.page,
      validBody(world, 'vendor_invoice', { vendor_id: world.otherVendor.id })
    );
    await world.requester.page.goto(`/requests/${id}`);
    await expect(world.requester.page.locator('.dl')).toContainText(world.otherVendor.name);
  });

  // F-B-07, fixed — the same hole as F-B-03, one table over. `vendorChoices`
  // offers active vendors only and the store now re-checks the status instead of
  // trusting it, so a retired vendor named directly in the POST is refused.
  // This case is the regression guard on that check.
  it('TC-B-060 — a vendor_id naming an inactive vendor is refused', async ({ world }) => {
    const probe = await raise(
      world.requester.page,
      validBody(world, 'vendor_invoice', { vendor_id: world.inactiveVendor.id })
    );
    expectRefused(probe, /vendor/i, 'an inactive vendor named directly in the POST');
  });

  it('TC-B-061 — a Requester without vendor:view gets the plain select and cannot search', async ({
    world
  }) => {
    const search = await probeGet(world.requester.page, '/vendors/search?q=audit');
    expectOutcome(search, [403], 'GET /vendors/search needs vendor:view');

    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto('/requests/new?type=vendor_invoice');
      const vendorField = page.locator('#vendor');
      expect(await vendorField.evaluate(node => node.tagName), 'the degraded control is a select').toBe(
        'SELECT'
      );
      expect(await vendorField.getAttribute('name'), 'and it posts the vendor id itself').toBe('vendor_id');
      await expect(page.locator('.combo-input'), 'no combobox is offered').toHaveCount(0);
      await expect(page.locator(`#vendor option[value="${world.vendor.id}"]`)).toHaveCount(1);
      await expect(
        page.locator(`#vendor option[value="${world.inactiveVendor.id}"]`),
        'vendorChoices lists active vendors only'
      ).toHaveCount(0);

      await page.getByLabel('Short title').fill(`Degraded pick ${world.runId}`);
      await page.locator('#project').selectOption(world.operationsId);
      await page.locator('#head').selectOption(world.officeRentId);
      await vendorField.selectOption(world.vendor.id);
      await page.getByLabel('Amount').fill('7700');
      await page.getByLabel('Invoice number').fill(`INV-D61-${world.runId}`);
      await page.getByLabel('Invoice date').fill('2026-07-18');
      await page.getByLabel('Purpose').fill('Raised with no vendor:view at all.');
      await page.getByLabel('Approver').selectOption(world.manager.id);
      await page.getByRole('button', { name: 'Submit request' }).click();
      await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  // F-B-13, fixed — annotation retired. `request_vendor_field`'s combo-input
  // carried no `name` where the create form's carries `name="q"`, and htmx sends
  // the triggering input's own name/value pair — so the edit screen asked
  // GET /vendors/search with no `q` at all and `SearchVendors("")` returns nil
  // (internal/store/vendors.go:337). The box was there, it typed, and it could
  // never offer a single vendor, which meant the vendor on a pending request
  // could not be changed by anybody holding vendor:view. The partial now carries
  // the same `name="q"` the create form does. This case is the regression guard
  // on that one attribute, driven end to end through the real control.
  it('TC-B-094 — the vendor combobox on the edit screen can search', async ({ world }) => {
    // The admin holds vendor:view, so the edit screen renders the combobox for
    // them; a Requester gets the plain select on both screens (TC-B-061).
    const id = await raiseOk(world.adminPage, validBody(world, 'vendor_invoice'));
    const page = await world.adminPage.context().newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto(`/requests/${id}/edit`);
      const combo = page.locator('input#vendor.combo-input');
      await expect(combo, 'the edit screen offers a combobox to a caller with vendor:view').toHaveCount(1);
      expect(
        await combo.getAttribute('name'),
        'htmx sends the triggering input own name/value pair, so the box has to be named q'
      ).toBe('q');
      await combo.fill('');
      await combo.pressSequentially(world.vendor.name.slice(0, 18));
      await expect(
        page.locator('#vendor-options .co[data-id]'),
        'typing a vendor name must offer that vendor, or the vendor on a pending request can never be changed'
      ).not.toHaveCount(0);
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  // The same regression guard as TC-B-058, over the other three columns:
  // manager_id, project_id and head_id reach the INSERT unchecked exactly as
  // vendor_id does, so every one of them is a forgeable foreign key and every one
  // of them must answer 400 rather than 500 now that `classify` maps a FOREIGN KEY
  // violation to ErrValidation (F-B-06).
  it('TC-B-095 — a forged manager, project or head id is refused, not answered with a server error', async ({
    world
  }) => {
    for (const field of ['manager_id', 'project_id', 'head_id'] as const) {
      const probe = await raise(
        world.requester.page,
        validBody(world, 'vendor_invoice', { [field]: '99999999' })
      );
      expect(
        probe.status,
        `a forged ${field} must be a validation refusal, not a server error; got ${probe.outcome}`
      ).toBe(400);
    }
  });
});

// ===========================================================================
// E — numbering
// ===========================================================================

it.describe('E · PR-YYYY-NNNNNN is unique and monotonic', () => {
  it('TC-B-062 — three raises take three consecutive numbers', async ({ world }) => {
    const numbers: string[] = [];
    for (let i = 0; i < 3; i++) {
      const id = await raiseOk(world.requester.page, validBody(world, 'vendor_invoice'));
      numbers.push(await numberOf(world.requester.page, id));
    }
    for (const number of numbers) expect(number, 'the seeded format is PR-YYYY-NNNNNN').toMatch(NUMBER_RE);
    expect(new Set(numbers).size, 'no number is ever reused').toBe(3);
    expect(sequenceOf(numbers[1]) - sequenceOf(numbers[0]), 'no gap').toBe(1);
    expect(sequenceOf(numbers[2]) - sequenceOf(numbers[1]), 'no gap').toBe(1);
    const year = String(new Date().getFullYear());
    expect(numbers[0], 'the year segment is the calendar year').toContain(`PR-${year}-`);
  });

  /*
   * F-B-10, fixed. CreateRequest used to open a DEFERRED transaction, read the
   * numbering settings and only then write, so SQLite refused the read-to-write
   * upgrade and answered SQLITE_BUSY without ever consulting the busy handler —
   * busy_timeout(5000) cannot help an upgrade that can never succeed. It now
   * takes the write lock with the transaction's first statement
   * (`beginWriteTx`, internal/store/requests.go:38), so the second submit waits
   * for the first instead of being refused.
   *
   * That is what made this case promotable. It was parked as `fixme` because the
   * loser's fate depended on the interleaving, so neither a plain test nor a
   * `test.fail()` could state the truth on every run. With the race removed the
   * outcome is deterministic: both submits commit, and the sequence is reserved
   * inside the same transaction so they are consecutive. Its integrity twin
   * TC-B-093 covers the other half — a number is never handed out twice.
   */
  it('TC-B-063 — two concurrent raises from two browsers both succeed, with consecutive numbers', async ({
    world
  }) => {
    const [a, b] = await Promise.all([
      raise(world.requester.page, validBody(world, 'vendor_invoice')),
      raise(world.stranger.page, validBody(world, 'reimbursement'))
    ]);
    expect(a.id, `the first concurrent submit failed: ${a.outcome}`).toMatch(/^\d+$/);
    expect(b.id, `the second concurrent submit failed: ${b.outcome}`).toMatch(/^\d+$/);

    const first = await numberOf(world.requester.page, a.id);
    const second = await numberOf(world.stranger.page, b.id);
    expect(first, 'two racing submits never share a number').not.toBe(second);
    expect(
      Math.abs(sequenceOf(first) - sequenceOf(second)),
      'the sequence is reserved inside the same transaction, so a race leaves no gap'
    ).toBe(1);
  });

  it('TC-B-093 — a race never mints a duplicate number, whatever it does to the loser', async ({ world }) => {
    // The availability half of this is F-B-10. The integrity half must hold
    // regardless: whoever does get through gets a number nobody else has.
    //
    // Three POSTs at once, two of them from the same browser — an API request
    // never conflicts with another on the same page, so the race is real.
    const attempts = [
      { page: world.requester.page, body: validBody(world, 'vendor_invoice') },
      { page: world.stranger.page, body: validBody(world, 'reimbursement') },
      { page: world.requester.page, body: validBody(world, 'employee_advance') }
    ];
    const results = await Promise.all(attempts.map(attempt => raise(attempt.page, attempt.body)));
    expect(
      results.filter(probe => probe.id !== '').length,
      'at least one racing submit must get through'
    ).toBeGreaterThan(0);
    for (const probe of results) {
      if (probe.id === '') {
        expect(
          probe.status,
          `a refused racing submit must not answer 2xx or 3xx with no request behind it; got ${probe.outcome}`
        ).toBeGreaterThanOrEqual(400);
      }
    }
    // Read the numbers one at a time: two of these share a page, and a second
    // goto on a page still navigating aborts the first.
    const numbers: string[] = [];
    for (const [index, probe] of results.entries()) {
      if (probe.id !== '') numbers.push(await numberOf(attempts[index].page, probe.id));
    }
    for (const number of numbers) expect(number).toMatch(NUMBER_RE);
    expect(new Set(numbers).size, 'a number is never handed out twice').toBe(numbers.length);
  });

  it('TC-B-064 — a refused submit consumes no number', async ({ world }) => {
    const before = sequenceOf(
      await numberOf(world.requester.page, await raiseOk(world.requester.page, validBody(world, 'vendor_invoice')))
    );

    // Two refusals: one before the transaction opens, one that fails inside it.
    expectRefused(
      await raise(world.requester.page, validBody(world, 'vendor_invoice', { purpose: '' })),
      'purpose is required',
      'validation refusal'
    );
    await raise(world.requester.page, validBody(world, 'vendor_invoice', { vendor_id: '99999999' }));

    const after = sequenceOf(
      await numberOf(world.requester.page, await raiseOk(world.requester.page, validBody(world, 'vendor_invoice')))
    );
    expect(after - before, 'a refused submit must not burn a request number').toBe(1);
  });
});

// ===========================================================================
// F — the single-POST contract (decision D1)
// ===========================================================================

it.describe('F · one POST creates, numbers and submits', () => {
  it('TC-B-065 — the confirmation screen shows the number and who has it', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'vendor_invoice'));
    const page = world.requester.page;
    await page.goto(`/requests/${id}/submitted`);
    await expect(page.locator('.rh-no')).toHaveText(NUMBER_RE);
    await expect(page.locator('.rh-status .pill')).toHaveText('Awaiting approval');
    await expect(page.locator('.banner.good')).toContainText(world.manager.subject.name);
    await expect(page.locator('.rh-status .waiting')).toContainText(world.manager.subject.name);
  });

  it('TC-B-066 — no route creates, saves or lists a draft', async ({ world }) => {
    const page = world.requester.page;
    for (const path of [
      '/requests/draft',
      '/requests/new/draft',
      `/requests/1/submit`,
      `/requests/1/save-draft`,
      `/requests/1/copy`
    ]) {
      const probe = await probePost(page, path, {});
      expectOutcome(probe, [404, 405], `${path} must not exist`);
    }

    // The form has exactly one submit, and the status enum has no draft: the
    // CHECK on payment_requests.status makes it unrepresentable.
    await page.goto('/requests/new?type=vendor_invoice');
    await expect(page.locator('form[action="/requests"] button[type="submit"]')).toHaveCount(1);

    const list = await probeGet(page, '/requests?bucket=draft');
    expect(list.status, 'an unknown bucket is a filter, not an error').toBe(200);
    expect(list.body, 'nothing is ever in a draft bucket').toContain('No requests match');
  });

  it('TC-B-067 — a pending request cannot be submitted a second time', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const resubmit = await probePost(world.requester.page, `/requests/${id}/edit`, {
      ...validBody(world, 'reimbursement'),
      submit_action: 'resubmit'
    });
    expect(
      resubmit.status,
      `there is no second submit on a pending request; got ${resubmit.outcome}`
    ).toBe(400);
    expect(message(resubmit)).toContain('a pending request cannot be submitted');
  });
});

// ===========================================================================
// G — the requester's own verbs, each with its legal and illegal case
// ===========================================================================

/** Puts the manager's decision in directly — the decision itself is a sibling's area. */
async function decide(
  world: World,
  id: string,
  verb: 'approve' | 'return' | 'reject',
  extra: Record<string, string> = {}
) {
  const form =
    verb === 'approve'
      ? { approved_amount: '12500', note: 'Approved by the audit fixture.', ...extra }
      : verb === 'return'
        ? { comment: 'Attach the receipts.', ...extra }
        : { reason: 'Not budgeted this quarter.', ...extra };
  const probe = await probePost(world.manager.page, `/requests/${id}/${verb}`, form);
  expect(probe.status, `the fixture's ${verb} failed: ${probe.outcome} ${message(probe)}`).toBeLessThan(400);
}

it.describe('G · what the requester may still do', () => {
  it('TC-B-068 — a returned request is corrected and resubmitted, keeping its number', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const number = await numberOf(world.requester.page, id);
    await decide(world, id, 'return');

    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto(`/requests/${id}`);
      await expect(page.locator('.rh-status .pill.returned')).toBeVisible();
      await expect(page.locator('.banner.warn')).toContainText('Attach the receipts.');
      await page.getByLabel('Purpose').fill('Flight, hotel and the airport cabs.');
      await page.getByRole('button', { name: 'Resubmit for approval' }).click();
      await expect(page).toHaveURL(new RegExp(`/requests/${id}$`));
      await expect(page.locator('.rh-status .pill')).toHaveText('Awaiting approval');
      await expect(page.locator('.rh-no'), 'a returned request keeps its number').toHaveText(number);
      await expect(page.locator('.dl')).toContainText('Flight, hotel and the airport cabs.');
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  it('TC-B-069 — editing while pending reroutes, re-notifies and resets the reminder clock', async ({
    world
  }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const number = await numberOf(world.requester.page, id);

    // It starts in the first manager's queue.
    await world.manager.page.goto('/approvals');
    await expect(world.manager.page.locator('.req-card', { hasText: number })).toHaveCount(1);

    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto(`/requests/${id}/edit`);
      // F-F-03: the wait is `reminder_pending_days` read from configuration, not
      // a three spelled out in the copy — the banner renders
      // `{{.Reminders.PendingAfterDays}}-day` (internal/app/templates.go:2696-2697)
      // and the seeded default is 3 (internal/store/reminders.go:68). "day", not
      // "calendar day", is decision 2 of the repair: a calendar boundary needs a
      // timezone this app does not have, so every threshold is elapsed days.
      // Nothing in this file changes the setting, so the number here is the seed's.
      await expect(page.locator('.banner.info')).toContainText(/restarts the\s+3-day reminder clock/);
      await page.getByLabel('Amount').fill('21500');
      await page.locator('#apr').selectOption(world.manager2.id);
      await page.getByRole('button', { name: /^Save and notify/ }).click();
      await expect(page).toHaveURL(new RegExp(`/requests/${id}$`));
      await expect(page.locator('.thread .tl-change')).toContainText('Amount');
      await expect(page.locator('.thread .tl-change')).toContainText('Approver');
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }

    // Rerouted: out of one queue, into the other.
    await world.manager.page.goto('/approvals');
    await expect(
      world.manager.page.locator('.req-card', { hasText: number }),
      'the old approver no longer owes a decision'
    ).toHaveCount(0);
    await world.manager2.page.goto('/approvals');
    await expect(
      world.manager2.page.locator('.req-card', { hasText: number }),
      'the new approver does'
    ).toHaveCount(1);

    // Re-notified: the in-app row always fires (G19).
    await world.manager2.page.goto('/notifications?scope=unread');
    await expect(world.manager2.page.locator('.notif-list')).toContainText(number);
    await expect(world.manager2.page.locator('.notif-list')).toContainText('edited and re-sent');

    // The reminder clock: reminder_last_sent is cleared, which the audit
    // snapshot is the only surface for.
    await world.adminPage.goto(`/audit?entity=payment_request&id=${id}`);
    const update = world.adminPage.locator('tr', { hasText: 'Updated' }).first();
    await update.locator('summary').click();
    await expect(update.locator('pre').last()).toContainText('"ReminderLastSent": null');
  });

  it('TC-B-070 — a pending request is withdrawn', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto(`/requests/${id}`);
      await page.getByRole('button', { name: 'Withdraw' }).click();
      await expect(page).toHaveURL(new RegExp(`/requests/${id}$`));
      await expect(page.locator('.rh-status .pill')).toHaveText('Withdrawn');
      await expect(page.locator('.rh-status .waiting')).toContainText('Withdrawn by the requester');
      await expect(page.getByRole('button', { name: 'Withdraw' })).toHaveCount(0);
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  it('TC-B-071 — an approved request cannot be withdrawn', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    await decide(world, id, 'approve');

    const page = world.requester.page;
    await page.goto(`/requests/${id}`);
    await expect(page.locator('.banner.locked')).toBeVisible();
    await expect(
      page.getByRole('button', { name: 'Withdraw' }),
      'the control is never offered on an approved request'
    ).toHaveCount(0);

    const probe = await probePost(page, `/requests/${id}/withdraw`, {});
    expect(probe.status, `the store must refuse it too; got ${probe.outcome}`).toBe(400);
    // The store's own words, article and all — F-B-08 is the grammar, not the rule.
    expect(message(probe)).toContain('approved request cannot be withdrawn');
  });

  it('TC-B-072 — a rejected request is re-raised as a new pending request', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const original = await numberOf(world.requester.page, id);
    await decide(world, id, 'reject');

    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto(`/requests/${id}`);
      await expect(page.locator('.rh-status .pill.rejected')).toBeVisible();
      await expect(page.locator('.banner.bad')).toContainText('Not budgeted this quarter.');
      await page.getByRole('button', { name: 'Raise it again' }).click();
      await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);

      const copyId = new URL(page.url()).pathname.split('/')[2];
      expect(copyId, 'a re-raise is a new request, not a reopened one').not.toBe(id);
      const copy = (await page.locator('.rh-no').innerText()).trim();
      expect(copy, 'and it takes its own number').not.toBe(original);
      expect(sequenceOf(copy), 'monotonically after the source').toBeGreaterThan(sequenceOf(original));
      await expect(page.locator('.rh-status .pill')).toHaveText('Awaiting approval');
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }

    // The source stays rejected and final.
    await world.requester.page.goto(`/requests/${id}`);
    await expect(world.requester.page.locator('.rh-status .pill.rejected')).toBeVisible();
  });

  it('TC-B-073 — a withdrawn request cannot be re-raised', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const withdraw = await probePost(world.requester.page, `/requests/${id}/withdraw`, {});
    expect(withdraw.status, 'the withdraw must land first').toBeLessThan(400);

    await world.requester.page.goto(`/requests/${id}`);
    await expect(
      world.requester.page.getByRole('button', { name: 'Raise it again' }),
      'the control belongs to a rejected request alone'
    ).toHaveCount(0);

    const probe = await probePost(world.requester.page, `/requests/${id}/reraise`, {});
    expect(probe.status, `got ${probe.outcome}`).toBe(400);
    expect(message(probe)).toContain('only a rejected request can be re-raised');
  });

  it('TC-B-074 — a rejected request cannot be edited', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    await decide(world, id, 'reject');

    const page = world.requester.page;
    await page.goto(`/requests/${id}`);
    await expect(page.getByRole('link', { name: 'Edit request' })).toHaveCount(0);

    const form = await probeGet(page, `/requests/${id}/edit`);
    expect(form.status, `the edit screen must not open; got ${form.outcome}`).toBe(400);
    expect(form.body).toContain('A rejected request is final');

    const save = await probePost(page, `/requests/${id}/edit`, validBody(world, 'reimbursement'));
    expect(save.status, `and the save must bounce; got ${save.outcome}`).toBe(400);
  });

  it('TC-B-075 — asking for cancellation of an approved request freezes payment', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    await decide(world, id, 'approve');

    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto(`/requests/${id}`);
      await page.getByRole('link', { name: 'Request cancellation' }).click();
      await expect(page).toHaveURL(new RegExp(`/requests/${id}/cancel$`));
      await expect(page.locator('.banner.warn')).toContainText('Payment freezes the moment you ask');
      await page.getByLabel('Reason').fill('The site cancelled the trip.');
      await page.getByRole('button', { name: 'Send cancellation request' }).click();
      await expect(page).toHaveURL(new RegExp(`/requests/${id}$`));
      await expect(page.locator('.rh-status .pill.cancelreq')).toBeVisible();
      await expect(page.locator('.banner.warn')).toContainText('Payment is frozen');
      await expect(page.locator('.banner.warn')).toContainText('The site cancelled the trip.');
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }

    // An empty reason is refused, and asking twice is refused.
    const blank = await probePost(world.requester.page, `/requests/${id}/cancel-request`, { reason: '  ' });
    expect(blank.status, `an empty reason must bounce; got ${blank.outcome}`).toBe(400);
    const again = await probePost(world.requester.page, `/requests/${id}/cancel-request`, { reason: 'again' });
    expect(again.status, 'a cancellation cannot be asked for twice').toBe(400);
  });

  it('TC-B-076 — asking for cancellation of a pending request is refused', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const page = world.requester.page;
    await page.goto(`/requests/${id}`);
    await expect(page.getByRole('link', { name: 'Request cancellation' })).toHaveCount(0);

    const form = await probeGet(page, `/requests/${id}/cancel`);
    expect(form.status, `the form must not render; got ${form.outcome}`).toBe(400);
    expect(form.body).toContain('withdraw it instead');

    const ask = await probePost(page, `/requests/${id}/cancel-request`, { reason: 'Changed my mind.' });
    expect(ask.status, `and the store must refuse it; got ${ask.outcome}`).toBe(400);
    expect(message(ask)).toContain('cannot be sent for cancellation');
  });

  // REWRITTEN for the F-G-002 fix. Four of these probes used to require 403 and
  // now require 404, and the split between the two is the fix, not an accident.
  //
  // Everything routed through `loadViewableRequest`
  // (internal/app/requests.go:325-348) answers **404** for a row outside the
  // caller's data scope — deliberately the same answer an id that does not exist
  // gets, so the status code stops being an existence oracle a requester could
  // walk the id space with (TC-B-088 pins the indistinguishability itself).
  //
  // The three POSTs that go straight to the store still answer **403**:
  // `WithdrawRequest` (internal/store/requests.go:986), `ReraiseRequest` (:1199)
  // and `RequestCancellation` (:1262) each compare `requester_id` to the actor
  // and return ErrForbidden. That is not an inconsistency — a caller who names
  // an id in a write already knows it exists, and hiding that would cost the
  // clearer refusal for nothing.
  it('TC-B-077 — a stranger cannot edit, withdraw, re-raise or ask to cancel another request', async ({
    world
  }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const stranger = world.stranger.page;

    expectOutcome(await probeGet(stranger, `/requests/${id}`), [404], 'reading another requester request');
    expectOutcome(await probeGet(stranger, `/requests/${id}/edit`), [404], 'opening the edit screen');
    expectOutcome(await probeGet(stranger, `/requests/${id}/cancel`), [404], 'opening the cancel screen');
    expectOutcome(
      await probePost(stranger, `/requests/${id}/edit`, validBody(world, 'reimbursement')),
      [404],
      'saving somebody else edit'
    );
    expectOutcome(await probePost(stranger, `/requests/${id}/withdraw`, {}), [403], 'withdrawing');
    expectOutcome(await probePost(stranger, `/requests/${id}/reraise`, {}), [403], 're-raising');
    expectOutcome(
      await probePost(stranger, `/requests/${id}/cancel-request`, { reason: 'no' }),
      [403],
      'asking for cancellation'
    );

    // And nothing changed: it is still pending and still the first requester's.
    await world.requester.page.goto(`/requests/${id}`);
    await expect(world.requester.page.locator('.rh-status .pill')).toHaveText('Awaiting approval');
  });

  it('TC-B-078 — the approver cannot edit the request they were sent', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const manager = world.manager.page;
    await manager.goto(`/requests/${id}`);
    await expect(manager.locator('.waiting.you')).toBeVisible();
    await expect(
      manager.getByRole('link', { name: 'Edit request' }),
      'an approver who wants a change returns the request'
    ).toHaveCount(0);

    // A Manager holds no request:edit at all, so the route itself refuses.
    expectOutcome(await probeGet(manager, `/requests/${id}/edit`), [403], 'the approver opening the edit form');
    expectOutcome(
      await probePost(manager, `/requests/${id}/edit`, validBody(world, 'reimbursement')),
      [403],
      'the approver saving an edit'
    );
  });
});

// ===========================================================================
// H — comments
// ===========================================================================

it.describe('H · the conversation', () => {
  it('TC-B-079 — a comment persists and everyone who may view the request sees it', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const body = `Receipts are with the travel desk. ${world.runId}`;

    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto(`/requests/${id}`);
      await page.getByLabel('Add a comment').fill(body);
      await page.getByRole('button', { name: 'Post comment' }).click();
      await expect(page).toHaveURL(new RegExp(`/requests/${id}$`));
      const mine = page.locator('.thread li.is-comment.is-me');
      await expect(mine).toContainText(body);
      await expect(mine).toContainText(world.requester.subject.name);
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }

    // The approver reads the same stream, and it is not marked as theirs.
    await world.manager.page.goto(`/requests/${id}`);
    await expect(world.manager.page.locator('.thread')).toContainText(body);
    await expect(world.manager.page.locator('.thread li.is-comment.is-me')).toHaveCount(0);
    await expect(
      world.manager.page.locator('.section-head', { has: world.manager.page.locator('h2', { hasText: 'History' }) })
    ).toContainText('Everyone who can see this request sees this whole stream');
  });

  it('TC-B-080 — a stranger cannot comment on someone else request', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const probe = await probePost(world.stranger.page, `/requests/${id}/comment`, { body: 'Let me in.' });
    // 404, not 403 (F-G-002). `requestComment` resolves the row through
    // `loadViewableRequest` (internal/app/requests.go:976), which answers a row
    // outside the caller's scope exactly as it answers one that does not exist —
    // so posting comments at ids is not a way to find out which ids are real.
    // The comment is still refused, which is the whole point of the case.
    expectOutcome(probe, [404], 'commenting on a request outside your scope');

    await world.requester.page.goto(`/requests/${id}`);
    await expect(world.requester.page.locator('.thread')).not.toContainText('Let me in.');
  });

  it('TC-B-081 — an empty comment is refused', async ({ world }) => {
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const probe = await probePost(world.requester.page, `/requests/${id}/comment`, { body: '   ' });
    expect(probe.status, `got ${probe.outcome}`).toBe(400);
    expect(message(probe)).toContain('comment cannot be empty');
  });
});

// ===========================================================================
// I — attachments
// ===========================================================================

it.describe('I · documents', () => {
  it('TC-B-082 — a document uploaded with the request lands on the request', async ({ world }) => {
    const page = await world.requester.ctx.newPage();
    const failures = capturePageErrors(page);
    try {
      await page.goto('/requests/new?type=reimbursement');
      await page.getByLabel('Short title').fill(`With proof ${world.runId}`);
      await page.locator('#project').selectOption(world.operationsId);
      await page.locator('#head').selectOption(world.officeRentId);
      await page.getByLabel('Amount').fill('4200');
      await page.getByLabel('Expense date').fill('2026-07-17');
      await page.getByLabel('Purpose').fill('Two nights, receipt attached.');
      await page.getByLabel('Approver').selectOption(world.manager.id);
      // The uploader's input is inside <label class="uploader"> and carries no
      // accessible name of its own, so it is addressed by name.
      await page.locator('input[name="attachment"]').setInputFiles(samplePdf);
      await page.getByRole('button', { name: 'Submit request' }).click();
      await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);

      const id = new URL(page.url()).pathname.split('/')[2];
      await page.goto(`/requests/${id}`);
      await expect(page.locator('.file-row')).toContainText(samplePdf.name);
      await expect(page.locator('.file-row')).toContainText('PDF');
      await expect(page.locator('.thread'), 'the upload joins the story').toContainText(samplePdf.name);
      expect(failures, failures.join('\n')).toEqual([]);
    } finally {
      await page.close();
    }
  });

  it('TC-B-083 — require_attachments makes a document or a written reason mandatory', async ({ world }) => {
    const label = 'Require a supporting document on every request';
    const setRequired = async (on: boolean) => {
      // The whole Configuration form is posted, so every other setting survives.
      await world.adminPage.goto('/configuration');
      const box = world.adminPage.getByLabel(label);
      if (on) await box.check();
      else await box.uncheck();
      await world.adminPage.getByRole('button', { name: 'Save configuration' }).click();
      await expect(world.adminPage).toHaveURL(/\/configuration$/);
    };

    try {
      await setRequired(true);

      // G10: it never hard-blocks — it asks for a reason instead.
      const refused = await raise(world.requester.page, validBody(world, 'reimbursement'));
      expectRefused(refused, 'attach a supporting document, or say why you cannot', 'no document, no reason');

      const excused = await raiseOk(
        world.requester.page,
        validBody(world, 'reimbursement', {
          attachment_exception_reason: 'The hotel posts the bill; it arrives Monday.'
        })
      );
      await world.requester.page.goto(`/requests/${excused}`);
      await expect(world.requester.page.locator('.banner.info')).toContainText('No document was attached');
      await expect(world.requester.page.locator('.banner.info')).toContainText('arrives Monday');

      // And the form says so before anybody presses anything.
      const page = await world.requester.ctx.newPage();
      try {
        await page.goto('/requests/new?type=reimbursement');
        await expect(page.locator('#att-exception')).toBeVisible();
        await expect(page.locator('.flabel', { hasText: 'Supporting document' })).not.toContainText('optional');
      } finally {
        await page.close();
      }

      // A document satisfies it with no reason at all.
      const withFile = await world.requester.ctx.newPage();
      try {
        await withFile.goto('/requests/new?type=reimbursement');
        await withFile.getByLabel('Short title').fill(`Mandatory proof ${world.runId}`);
        await withFile.locator('#project').selectOption(world.operationsId);
        await withFile.locator('#head').selectOption(world.officeRentId);
        await withFile.getByLabel('Amount').fill('3300');
        await withFile.getByLabel('Expense date').fill('2026-07-17');
        await withFile.getByLabel('Purpose').fill('Receipt attached, so no exception is needed.');
        await withFile.getByLabel('Approver').selectOption(world.manager.id);
        await withFile.locator('input[name="attachment"]').setInputFiles(samplePdf);
        await withFile.getByRole('button', { name: 'Submit request' }).click();
        await expect(withFile).toHaveURL(/\/requests\/\d+\/submitted$/);
      } finally {
        await withFile.close();
      }
    } finally {
      await setRequired(false);
      // Back to optional, or every later test in the file would need a reason.
      const relaxed = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
      expect(relaxed, 'the setting was restored').toMatch(/^\d+$/);
    }
  });

  // F-B-09 (with F-A-05) **critical**, fixed — annotation retired, and the
  // expected href changed with it. The request screens linked a
  // `request_attachments` row to `GET /attachments/{id}`, which reads
  // `payment_attachments` — a different table whose id sequence also starts at
  // 1 — so Download on your own invoice served whichever stranger's bank advice
  // happened to share the number, and served nothing at all once the sequences
  // diverged. Request documents now have a route of their own,
  // `GET /requests/{id}/attachments/{attachmentID}` (internal/app/app.go:468),
  // whose handler checks the attachment really belongs to the request in the
  // path and then applies the request's own row scope
  // (internal/app/app.go:1129-1159).
  //
  // So the assertion this case makes about the href is not cosmetic: a link
  // back to `/attachments/{id}` would be the defect returning. The scoped half
  // of the same route is TC-B-085's subject.
  it('TC-B-084 — a requester can download the document on their own request', async ({ world }) => {
    const page = await world.requester.ctx.newPage();
    let href = '';
    try {
      await page.goto('/requests/new?type=reimbursement');
      await page.getByLabel('Short title').fill(`Downloadable ${world.runId}`);
      await page.locator('#project').selectOption(world.operationsId);
      await page.locator('#head').selectOption(world.officeRentId);
      await page.getByLabel('Amount').fill('2100');
      await page.getByLabel('Expense date').fill('2026-07-17');
      await page.getByLabel('Purpose').fill('One receipt, to be downloaded again.');
      await page.getByLabel('Approver').selectOption(world.manager.id);
      await page.locator('input[name="attachment"]').setInputFiles(samplePdf);
      await page.getByRole('button', { name: 'Submit request' }).click();
      await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);
      const id = new URL(page.url()).pathname.split('/')[2];
      await page.goto(`/requests/${id}`);
      href = (await page.locator('.file-row a', { hasText: 'Download' }).getAttribute('href')) ?? '';
    } finally {
      await page.close();
    }
    expect(
      href,
      'a request document is served by the request-scoped route, never by the payment one'
    ).toMatch(/^\/requests\/\d+\/attachments\/\d+$/);

    const probe = await probeGet(world.requester.page, href);
    expect(
      probe.status,
      `the link the screen renders must serve the requester's own document; got ${probe.outcome}`
    ).toBe(200);

    // The {id} in the path is a check, not decoration: the same attachment id
    // hung off somebody else's request number is refused, and refused as a 404
    // so the route cannot be used to count attachments either.
    const attachmentID = href.split('/').pop();
    const theirs = await raiseOk(world.stranger.page, validBody(world, 'reimbursement'));
    const crossed = await probeGet(world.requester.page, `/requests/${theirs}/attachments/${attachmentID}`);
    expect(
      crossed.status,
      `an attachment asked for under another request must not be served; got ${crossed.outcome}`
    ).toBe(404);
  });

  // F-B-11 (with F-A-01), fixed while this suite was being repaired — the
  // annotation is removed here because the fix landed in another wave's file, not
  // because this case changed. `attachmentDownload` used to check only that the
  // path was inside the attachment directory and that the file existed; it never
  // asked who was calling, so `attachment:view` — which the Requester role holds —
  // read the bank advice on any payment in the system. It now resolves the
  // attachment's payment and applies the same scope check `paymentDetail` runs,
  // answering 404 rather than 403 so the route is not an enumeration oracle.
  // This case is the regression guard on that check.
  it('TC-B-085 — /attachments/{id} obeys the row scope its request obeys', async ({
    world,
    adminPage,
    runId
  }) => {
    // A payment attachment belonging to a request this Requester cannot see.
    const accounts = await asRole(adminPage, world.browser, runId, ['Accounts'], 'acc-b85');
    let download = '';
    try {
      const { id } = await createApprovedRequest(adminPage, runId, { amount: '5000' });
      // The bank advice rides inside the settlement's own multipart POST.
      // F-D-08's fix makes a post-hoc POST /payments/{id}/attachments a 400 on a
      // linked payment (internal/store/store.go:1403-1405), so the only way a
      // settled payment ever carries proof is `RecordPaymentForRequest`'s
      // `attachment` part, written in the settlement's own transaction — the same
      // mechanism `fixtures.settlePayment`'s `attachment` option drives through
      // the screen. head_id is not sent: F-D-01 derives it from the request.
      const reserved = await probePost(accounts.page, `/requests/${id}/record-payment`, {});
      expect(reserved.status, 'the accountant takes the request out of the queue first').toBe(303);
      const settled = await accounts.page.request.post('/payments', {
        multipart: {
          csrf: await csrfToken(accounts.context),
          request_id: String(id),
          amount: '5000',
          paid_on: '2026-07-20',
          payment_mode: 'bank_transfer',
          reference_no: `UTR-B85-${runId}`,
          settlement: 'settled',
          attachment: { name: 'bank-advice.pdf', mimeType: 'application/pdf', buffer: samplePdf.buffer }
        },
        maxRedirects: 0,
        failOnStatusCode: false
      });
      expect(settled.status(), 'the fixture settlement carrying its proof must land').toBe(303);
      const paymentPath = settled.headers()['location'] ?? '';
      expect(paymentPath, 'and it lands on the payment it wrote').toMatch(/^\/payments\/\d+$/);

      await accounts.page.goto(paymentPath);
      download =
        (await accounts.page
          .locator('.file-row', { hasText: 'bank-advice.pdf' })
          .locator('a[href^="/attachments/"]')
          .getAttribute('href')) ?? '';
      expect(download, 'the payment carries a downloadable proof').toMatch(/^\/attachments\/\d+$/);

      // The Requester cannot see the request behind it…
      //
      // 404 on the request and 403 on the payment, and the two are read from
      // different code: `loadViewableRequest` (internal/app/requests.go:335-347)
      // withholds the row's existence as well as its contents (F-G-002), while
      // `paymentDetail` (internal/app/app.go:911-915) still names the reason —
      // "You cannot see the request behind this payment." Both are refusals,
      // which is what this case turns on; the difference is recorded here rather
      // than smoothed over so a later reader can see it was measured.
      expectOutcome(
        await probeGet(world.requester.page, `/requests/${id}`),
        [404],
        'the request behind the payment is out of scope'
      );
      expectOutcome(
        await probeGet(world.requester.page, paymentPath),
        [403],
        'and so is the payment'
      );
    } finally {
      await accounts.close();
    }

    // …so the proof on it must be out of reach too.
    const probe = await probeGet(world.requester.page, download);
    expect(
      probe.status,
      `attachmentDownload must obey the same row scope /requests/{id} obeys; got ${probe.outcome}`
    ).not.toBe(200);
  });
});

// ===========================================================================
// J — scope, and the doors that must stay shut
// ===========================================================================

it.describe('J · a Requester sees only their own work', () => {
  it('TC-B-086 — the list holds only the requester own requests', async ({ world }) => {
    const mine = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const theirs = await raiseOk(world.stranger.page, validBody(world, 'reimbursement'));
    const mineNumber = await numberOf(world.requester.page, mine);
    const theirsNumber = await numberOf(world.stranger.page, theirs);

    const page = world.requester.page;
    await page.goto('/requests?bucket=all');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('My requests');
    await expect(page.locator('.req-list')).toContainText(mineNumber);
    await expect(page.locator('.req-list'), 'scope own means own').not.toContainText(theirsNumber);

    // ?scope=all cannot widen a narrow scope.
    await page.goto('/requests?bucket=all&scope=all');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('My requests');
    await expect(page.locator('.req-list')).not.toContainText(theirsNumber);
  });

  it('TC-B-087 — the CSV export is scoped, and ?scope=all cannot widen it', async ({ world }) => {
    const mine = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    const theirs = await raiseOk(world.stranger.page, validBody(world, 'reimbursement'));
    const mineNumber = await numberOf(world.requester.page, mine);
    const theirsNumber = await numberOf(world.stranger.page, theirs);

    const csv = await probeGet(world.requester.page, '/requests/export.csv?bucket=all&scope=all');
    expect(csv.status, 'a Requester holds request:view, so the export is theirs to take').toBe(200);
    expect(csv.body, 'their own request is in it').toContain(mineNumber);
    expect(csv.body, 'somebody else is not').not.toContain(theirsNumber);
    const header = csv.body.split('\n')[0].split(',');
    expect(header.slice(0, 3).join(','), 'the header names the columns').toBe('Number,Status,Type');

    // REWRITTEN for F-G-030. This used to demand `₹12,500.00` in every money
    // cell, on C3's "money renders once with a single symbol" — which is the
    // rule for a *screen*. A CSV is opened by a spreadsheet, and "₹12,500.00" is
    // text there: the column cannot be summed, sorted or averaged, so the export
    // was money nobody could do arithmetic on. `csvAmount`
    // (internal/app/app.go:2147) writes a bare `12500.00`. The rendered screen
    // still carries the symbol and TC-B-016 pins that; the two are deliberately
    // different, and this case is what stops the symbol coming back here.
    const row = csv.body.split('\n').find(line => line.startsWith(mineNumber));
    expect(row, 'the requester own row is in the export').toBeTruthy();
    const cells = row!.split(',');
    expect(cells.length, 'no field in this fixture contains a comma, so the split is the row').toBe(header.length);
    expect(
      cells[header.indexOf('Amount')],
      'money in the export is a number a spreadsheet can add up, not a formatted string'
    ).toMatch(/^\d+\.\d{2}$/);
    expect(csv.body.includes('₹'), 'so no cell in the file carries a currency symbol at all').toBe(false);
  });

  // REWRITTEN for the F-G-002 fix. This case used to require 403 on somebody
  // else's request and 404 on an id that does not exist, and its comment said
  // "the two must not be swapped". They are now deliberately the same answer:
  // the existence of a row is information about that row, so a requester walking
  // the id space could otherwise read off exactly which ids exist and, by
  // extension, how many requests the company raises. `loadViewableRequest`
  // (internal/app/requests.go:325-348) answers 404 either way and logs the
  // attempt.
  //
  // What this case now protects is the indistinguishability itself — the same
  // status AND the same sentence, since a page that names the difference leaks
  // what the status no longer does. A 403 anywhere in this list puts the oracle
  // back.
  it('TC-B-088 — another requester request by direct id is indistinguishable from one that does not exist', async ({
    world
  }) => {
    const theirs = await raiseOk(world.stranger.page, validBody(world, 'reimbursement'));
    const missing = await probeGet(world.requester.page, '/requests/98765432');
    expectOutcome(missing, [404], 'an id that does not exist');
    expect(missing.body, 'and it says only that').toContain('The requested record was not found.');

    for (const path of [
      `/requests/${theirs}`,
      `/requests/${theirs}/submitted`,
      `/requests/${theirs}/edit`,
      `/requests/${theirs}/cancel`
    ]) {
      const probe = await probeGet(world.requester.page, path);
      expectOutcome(probe, [404], `${path} by URL typing`);
      expect(
        probe.body,
        `${path}: a row that exists must answer word for word what a row that does not exist answers`
      ).toContain('The requested record was not found.');
    }

    // And the refusal is a refusal, not a rendered request that happens to carry
    // a 404: none of the stranger's own data comes back with it.
    const detail = await probeGet(world.requester.page, `/requests/${theirs}`);
    expect(detail.body, 'no request number rides out on the refusal').not.toMatch(NUMBER_RE);
  });

  it('TC-B-089 — anonymous and no-role callers are refused the form and the POST', async ({
    world
  }, testInfo) => {
    const baseURL = testInfo.project.use.baseURL!;
    for (const path of ['/requests', '/requests/new', '/requests/new?type=vendor_invoice']) {
      const probe = await probeAnonymous(world.browser, path, baseURL);
      expect(probe.status, `${path} anonymous: expected a redirect, got ${probe.outcome}`).toBeGreaterThanOrEqual(
        300
      );
      expect(probe.status).toBeLessThan(400);
      expect(probe.location ?? '', `${path} must send an anonymous caller to the login screen`).toContain(
        '/login'
      );
    }

    // Signed in and permitted nothing.
    expectOutcome(await probeGet(world.noRole.page, '/requests'), [403], 'the list with no role');
    expectOutcome(await probeGet(world.noRole.page, '/requests/new'), [403], 'the form with no role');
    expectOutcome(
      await probePost(world.noRole.page, '/requests', validBody(world, 'reimbursement')),
      [403],
      'the create POST with no role'
    );
    // A Manager holds request:view and comment, and no request:create.
    expectOutcome(await probeGet(world.manager.page, '/requests/new'), [403], 'the form as an approver');
    expectOutcome(
      await probePost(world.manager.page, '/requests', validBody(world, 'reimbursement')),
      [403],
      'the create POST as an approver'
    );
  });

  // F-B-16, fixed — annotation retired, and the case now asserts the fix rather
  // than the shape it was guessed to have. `ListRequests` capped the rows at 200
  // while `CountRequests` — the tab count beside them — had no cap at all, so
  // the All tab promised 214 and the list drew 200 with nothing on the page
  // saying so, and the CSV silently dropped the same fourteen.
  //
  // The repair is NOT "render every row": 205 cards on one page is a slower
  // screen and a worse one. It is that the truncation stops being silent —
  // `ListRequestsPage` returns Total/Offset/Truncated
  // (internal/store/requests.go:1700-1794, handler at internal/app/requests.go:612),
  // the screen states "Showing 1–200 of 205" and offers "Older →"
  // (internal/app/templates.go:2264-2269), and the export takes
  // `RequestsUnlimited` (internal/app/requests.go:1157-1163) because an export
  // taken for reconciliation that drops rows without saying so is worse than no
  // export.
  //
  // So the name still holds — never SILENTLY truncated — and the assertions
  // below are what makes it true: the count is reachable, the page says how much
  // of it is on screen, and the file carries all of it.
  it('TC-B-099 — a list of more than 200 requests is never silently truncated', async ({ world }) => {
    // Its own subject, so 205 rows cannot disturb any other case's list.
    const bulk = await asRole(world.adminPage, world.browser, world.runId, ['Requester'], 'bulk-b99');
    try {
      for (let i = 0; i < 205; i++) {
        const probe = await probePost(
          bulk.page,
          '/requests',
          validBody(world, 'reimbursement', { short_title: `Bulk ${i} ${world.runId}` })
        );
        expect(probe.status, `bulk raise ${i} failed: ${probe.outcome}`).toBe(303);
      }

      await bulk.page.goto('/requests?bucket=all');
      const counted = Number(
        (await bulk.page.locator('.segmented a', { hasText: /^All\s/ }).locator('.n').innerText()).trim()
      );
      expect(counted, 'the fixture raised 205').toBeGreaterThanOrEqual(205);

      // The page carries one screenful and says which screenful it is.
      const rendered = await bulk.page.locator('.req-card').count();
      expect(rendered, 'the first page is the store default of 200 rows').toBe(200);
      // The pager is a bare `.cluster` (internal/app/templates.go:2265), so it
      // is addressed by the sentence it exists to say rather than by a class.
      const showing = bulk.page.locator('span.muted.small', { hasText: /^Showing / });
      await expect(
        showing,
        `the All tab promises ${counted} and the list draws ${rendered} — the page must say so in as many words`
      ).toHaveText(`Showing 1–${rendered} of ${counted}`);
      await expect(
        bulk.page.getByRole('link', { name: /Older/ }),
        'and the rest must be reachable'
      ).toHaveCount(1);

      // Following it reaches the remainder, so "Total" is a promise the product keeps.
      await bulk.page.goto(`/requests?bucket=all&offset=${rendered}`);
      const remainder = await bulk.page.locator('.req-card').count();
      expect(remainder, 'page two carries the rows page one did not').toBe(counted - rendered);
      await expect(bulk.page.locator('span.muted.small', { hasText: /^Showing / })).toHaveText(
        `Showing ${rendered + 1}–${counted} of ${counted}`
      );

      const csv = await probeGet(bulk.page, '/requests/export.csv?bucket=all');
      expect(csv.status).toBe(200);
      const rows = csv.body.trim().split('\n').length - 1;
      expect(
        rows,
        `the export must carry every row it counted (${counted}) or say that it did not; it carried ${rows}`
      ).toBe(counted);
    } finally {
      await bulk.close();
    }
  });

  it('TC-B-090 — POST /requests without a valid CSRF token is refused', async ({ world }) => {
    for (const csrf of ['omit', 'bogus'] as const) {
      const probe = await probePost(world.requester.page, '/requests', validBody(world, 'reimbursement'), {
        csrf
      });
      expectOutcome(probe, [403], `a ${csrf} CSRF token on the create POST`);
      expect(probe.body, 'and the refusal explains itself').toContain('form session expired');
    }
    // The same gate on every requester verb.
    const id = await raiseOk(world.requester.page, validBody(world, 'reimbursement'));
    for (const path of [`/requests/${id}/withdraw`, `/requests/${id}/comment`, `/requests/${id}/edit`]) {
      expectOutcome(await probePost(world.requester.page, path, {}, { csrf: 'bogus' }), [403], `${path} forged`);
    }
  });
});
