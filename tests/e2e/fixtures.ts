import { expect, test as base, type Page } from '@playwright/test';

export const admin = {
  email: 'admin@fervid.local',
  password: 'TestAdmin12345'
};

/** Every user these fixtures create gets this password. */
export const fixturePassword = 'StrongTestPassword!42';

export const test = base.extend<{ adminPage: Page; secondPage: Page; runId: string }>({
  runId: async ({}, use) => {
    await use(`e2e-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`);
  },
  adminPage: async ({ page }, use) => {
    await login(page, admin.email, admin.password);
    await use(page);
  },
  // A second signed-in accountant, in a context of its own. Reserving is
  // atomic in the store, so the only way to drive a real race — one person
  // takes a request, the other meets the conflict screen — is two browsers.
  // The admin creates them, because only an admin may grant a role.
  // runId, not testId: the e2e server reuses its database between local runs,
  // and a deterministic id would try to create the same email twice.
  secondPage: async ({ browser, adminPage, runId }, use) => {
    const accountant = await createAccountsUser(adminPage, `second-${runId}`);
    const context = await browser.newContext({ viewport: adminPage.viewportSize() ?? undefined });
    const page = await context.newPage();
    try {
      await login(page, accountant.email, accountant.password);
      await use(page);
    } finally {
      await context.close();
    }
  }
});

export { expect };

export async function login(page: Page, email: string, password: string) {
  await page.goto('/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Login' }).click();
  await expect(page).toHaveURL(/\/$/);
}

// Creating a user now happens inside the "＋ Add user" overlay sheet, so the
// sheet is opened first and every field is scoped to it: each user's own edit
// sheet also carries a Name field and a "Reset password" field, and an
// unscoped getByLabel would match several of them.
export async function createDataEntryUser(page: Page, runId: string) {
  const email = `${runId}@example.test`;
  await page.goto('/users');
  await openNewUserSheet(page);
  const sheet = page.locator('#user-new');
  await sheet.getByLabel('Email').fill(email);
  await sheet.getByLabel('Name').fill(`Data Entry ${runId}`);
  // Accounts, ticked explicitly. The sheet used to offer a two-value account
  // type whose "Data entry" option was silently mapped to this very role, so
  // this is the same user these tests always got — now asked for by name.
  await sheet.getByRole('checkbox', { name: /^Accounts/ }).check();
  await sheet.getByLabel('Password').fill(fixturePassword);
  await sheet.getByRole('button', { name: 'Add User' }).click();
  await expect(page).toHaveURL(/\/users$/);
  return { email, password: fixturePassword };
}

export async function openNewUserSheet(page: Page) {
  await page.locator('.pb-actions').getByRole('button', { name: 'Add user' }).click();
  await expect(page.locator('#user-new')).toBeVisible();
}

/**
 * Creates a user and gives them one system role.
 *
 * One round trip. This used to take two, because the "Add user" sheet offered
 * only a two-value legacy account type and the roles that actually carry
 * permissions had to be ticked afterwards in the user's own edit sheet. The
 * sheet now asks for the real roles, so the role is set where it is chosen.
 */
async function createUserWithRole(page: Page, prefix: string, runId: string, role: RegExp) {
  const email = `${prefix}-${runId}@example.test`.toLowerCase();
  const name = `${prefix} ${runId}`;
  await page.goto('/users');
  await openNewUserSheet(page);
  const sheet = page.locator('#user-new');
  await sheet.getByLabel('Email').fill(email);
  await sheet.getByLabel('Name').fill(name);
  await sheet.getByRole('checkbox', { name: role }).check();
  await sheet.getByLabel('Password').fill(fixturePassword);
  await sheet.getByRole('button', { name: 'Add User' }).click();
  await expect(page).toHaveURL(/\/users$/);
  return { email, name, password: fixturePassword };
}

/**
 * An accountant: the Accounts role holds payment:process and reservation:reserve,
 * which is what puts a person in front of /accounts-queue and lets them take a
 * request out of it.
 */
export async function createAccountsUser(page: Page, runId: string) {
  return createUserWithRole(page, 'accounts', runId, /^Accounts/);
}

/**
 * An approver: the Manager role holds approval:approve over every request.
 * G8 forbids approving your own request, so every approved request in this
 * suite needs one of these — the requester can never be the decider.
 */
export async function createApproverUser(page: Page, runId: string) {
  return createUserWithRole(page, 'approver', runId, /^Manager/);
}

// One approver and one vendor per runId is enough for any number of requests in
// a test, and each costs several page loads, so both are remembered. The keys
// carry the runId, so nothing is ever shared between tests.
const approvers = new Map<string, { email: string; name: string; password: string }>();
const vendors = new Set<string>();
const requestSeq = new Map<string, number>();

async function ensureApprover(page: Page, runId: string) {
  const cached = approvers.get(runId);
  if (cached) return cached;
  const approver = await createApproverUser(page, runId);
  approvers.set(runId, approver);
  return approver;
}

/**
 * The approver every request this runId raised was routed to.
 *
 * Some screens belong to that one person and nobody else: the partial review
 * offers its "Accept and close" and "Raise a concern" sheets only when the
 * reader IS the request's manager, so a spec about those decisions has to sign
 * in as this user rather than as the admin. Call it after createApprovedRequest.
 */
export function approverFor(runId: string) {
  const approver = approvers.get(runId);
  if (!approver) throw new Error(`no approver for runId ${runId} — call createApprovedRequest first`);
  return approver;
}

/**
 * A vendor by that exact name, so the payee on the request — and therefore on
 * the payment — is the caller's to choose.
 *
 * Vendor names are uniquely indexed case-insensitively, so a name a previous
 * run already used lands on the refusal page instead of the vendor. The vendor
 * exists either way, which is all this needs; the combobox on the request form
 * is what proves it, and it fails loudly if the name is genuinely absent.
 */
async function ensureVendor(page: Page, name: string) {
  if (vendors.has(name)) return name;
  await page.goto('/vendors/new');
  await page.getByLabel('Vendor name').fill(name);
  await page.getByLabel('Type').selectOption('company');
  await page.getByRole('button', { name: 'Save vendor' }).click();
  await page.waitForURL(url => !url.pathname.endsWith('/vendors/new'));
  vendors.add(name);
  return name;
}

/**
 * Raises a vendor-invoice request and has it approved, the way two people do it.
 *
 * Free-standing payment entry is gone: a payment exists only against an
 * approved request, so this is the first half of every payment journey. It
 * drives the real Phase-2 screens rather than seeding rows, because the
 * reserve→settle flow reads fields — the approved amount, the payee, the
 * project and head — that only those screens fill in correctly.
 *
 * The approval happens in a context of its own. G8 means the requester can
 * never be the approver, and the seeded database has exactly one user, so a
 * second signed-in person is not optional.
 *
 * `payee` names the VENDOR on the request, and it DOES reach the payment.
 *
 * This paragraph used to carry a KNOWN GAP saying the opposite — that the queue's
 * Payee column, the entry screen, the payment row and the payment detail `<h1>`
 * were all blank for a request raised through the real form. That was true once
 * and was fixed in commit `fca6939`: `store.Request.Vendor` resolves the display
 * payee as `COALESCE(NULLIF(v.name,''), r.vendor_payee)`, and the settlement path
 * copies it onto the payment (`internal/app/linking.go:260`).
 *
 * The 2026-07-27 QA audit walked all five hops and found the vendor's name at
 * every one — see `docs/qa/results/findings-d-linking-settlement.md` F-D-09. The
 * stale comment had already cost two agents a pass and had talked two shipped
 * specs out of asserting on the payee at all, so a spec that wants to search for
 * or assert on a payee should simply do it.
 *
 * The underlying asymmetry is still worth knowing: a `vendor_invoice` request
 * names its payee with `vendor_id` and leaves the `vendor_payee` snapshot column
 * empty, while reimbursement and employee advance fill the snapshot with the
 * requester's name. Read `.Vendor`, never the raw snapshot. Go tests that seed a
 * request must use `seedVendorRequest`, not `seedApprovedRequest`, to get the
 * shape the real form produces.
 *
 * Returns the request number (PR-YYYY-NNNNNN) and its id.
 */
export async function createApprovedRequest(
  page: Page,
  runId: string,
  opts: { amount: string; payee?: string; neededBy?: string }
): Promise<{ number: string; id: number }> {
  const approver = await ensureApprover(page, runId);
  const payee = opts.payee ?? `Payee ${runId}`;
  await ensureVendor(page, payee);

  const seq = (requestSeq.get(runId) ?? 0) + 1;
  requestSeq.set(runId, seq);

  await page.goto('/requests/new?type=vendor_invoice');
  await page.getByLabel('Short title').fill(`Supply ${seq} ${runId}`);

  // Choosing a project swaps #form-fields from the server and narrows the heads
  // to that project's own. Waiting for a head count would be guessing at the
  // seed; waiting for every remaining option to belong to Operations is the
  // swap itself. Only the "Choose a head" placeholder survives the filter.
  await page.locator('#project').selectOption({ label: 'Operations' });
  await expect(page.locator('#head option').filter({ hasNotText: 'Operations /' })).toHaveCount(1);
  await page.locator('#head').selectOption({ label: 'Operations / Office Rent' });

  // The combobox writes the vendor's id into the hidden input the form posts;
  // the visible text is never trusted by the server.
  await page.locator('#vendor').pressSequentially(payee);
  await page.locator('#vendor-options .co', { hasText: payee }).first().click();
  await expect(page.locator('#vendor-id')).not.toHaveValue('');

  await page.getByLabel('Amount').fill(opts.amount);
  await page.getByLabel('Invoice number').fill(`INV-${seq}-${runId}`);
  await page.getByLabel('Invoice date').fill('2026-07-18');
  if (opts.neededBy) await page.getByLabel('Needed by').fill(opts.neededBy);
  await page.getByLabel('Purpose').fill(`Materials for ${runId}.`);
  await page.getByLabel('Approver').selectOption({ label: approver.name });
  await page.getByRole('button', { name: 'Submit request' }).click();

  // D1: one POST created it, numbered it and sent it. There is no draft step.
  await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);
  const id = Number(new URL(page.url()).pathname.split('/')[2]);
  const number = (await page.locator('.rh-no').first().innerText()).trim();

  const browser = page.context().browser();
  if (!browser) throw new Error('createApprovedRequest needs a browser-backed context to sign the approver in');
  const context = await browser.newContext({ viewport: page.viewportSize() ?? undefined });
  try {
    const approverPage = await context.newPage();
    await login(approverPage, approver.email, approver.password);
    await approverPage.goto(`/requests/${id}`);
    await approverPage.getByRole('button', { name: /^Approve / }).click();
    const sheet = approverPage.locator('#approve-sheet');
    await expect(sheet).toBeVisible();
    // Approving the full amount keeps approvedOf(request) equal to the amount
    // the caller asked for, so a settlement can be compared against it.
    await sheet.getByLabel('Amount approved').fill(opts.amount);
    await sheet.getByRole('button', { name: 'Approve request' }).click();
    await expect(approverPage).toHaveURL(/\/approvals$/);
  } finally {
    await context.close();
  }

  return { number, id };
}

/**
 * Reserves an approved request and records its payment, end to end.
 *
 * The whole journey, in the order a person walks it:
 *   /accounts-queue → "Take for processing" (the reservation is a POST, so the
 *   queue's control is a submit button, not a link)
 *   → /payments/new?request={id}, the entry screen
 *   → "Payment settled →", which posts the settlement preview and writes nothing
 *   → the .overlay .sheet, where the settled/partial choice is made
 *   → "Confirm and save payment", the only writer in the flow
 *   → /payments/{id}.
 *
 * The entry screen carries no payee, no invoice and no head: they are hidden
 * inputs copied from the request. Anything a spec needs to control about them
 * belongs on createApprovedRequest.
 *
 * Returns the /payments/{id} path the payment landed on.
 */
export async function settlePayment(
  page: Page,
  requestId: number,
  opts: {
    amount: string;
    paidOn: string;
    mode?: string;
    reference?: string;
    remarks?: string;
    settlement?: 'settled' | 'partial';
    partialReason?: string;
    attachment?: string;
  }
): Promise<string> {
  await page.goto('/accounts-queue?tab=approved');
  const row = page.locator('tr').filter({ has: page.locator(`a[href="/requests/${requestId}"]`) });
  await row.getByRole('button', { name: 'Take for processing' }).click();
  await expect(page).toHaveURL(new RegExp(`/payments/new\\?request=${requestId}$`));

  await page.getByLabel('Amount actually paid').fill(opts.amount);
  await page.getByLabel('Paid on').fill(opts.paidOn);
  await page.getByLabel('Payment mode').selectOption(opts.mode ?? 'neft');
  await page.getByLabel('Transaction / UTR reference').fill(opts.reference ?? `UTR${requestId}`);
  if (opts.remarks) await page.getByLabel('Processing note').fill(opts.remarks);
  // The uploader is a hidden <input type="file"> inside <label class="uploader">
  // and has no accessible name at all, so getByLabel never resolves it.
  // setInputFiles works on a hidden input.
  if (opts.attachment) await page.locator('input[name="attachment"]').setInputFiles(opts.attachment);

  await page.getByRole('button', { name: /Payment settled/ }).click();
  const sheet = page.locator('.overlay .sheet');
  await expect(sheet).toBeVisible();

  const settlement = opts.settlement ?? 'settled';
  await sheet.locator(`input[name="settlement"][value="${settlement}"]`).check();
  if (settlement === 'partial' || opts.partialReason) {
    await sheet.getByLabel('Reason for any deduction or shortfall').fill(opts.partialReason ?? 'A balance is still owed.');
  }

  await sheet.getByRole('button', { name: 'Confirm and save payment' }).click();
  await expect(page).toHaveURL(/\/payments\/\d+$/);
  return new URL(page.url()).pathname;
}

export function capturePageErrors(page: Page) {
  const failures: string[] = [];
  page.on('pageerror', error => failures.push(`pageerror: ${error.message}`));
  page.on('console', message => {
    if (message.type() === 'error') failures.push(`console: ${message.text()}`);
  });
  page.on('response', response => {
    if (response.status() >= 500) failures.push(`HTTP ${response.status()} ${response.url()}`);
  });
  return failures;
}
