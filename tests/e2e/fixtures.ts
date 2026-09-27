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

/**
 * The only date a payment can carry in these specs.
 *
 * A settlement may be dated no earlier than the day its request was approved
 * and no later than today, and every request these fixtures approve is
 * approved moments before it is paid. So "today" it is — read from the local
 * clock, because the server compares against its own local date and both run
 * on this machine.
 */
export function today(): string {
  const now = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

/** The month today's payments land in, as the ledger and grid key it (YYYY-MM). */
export function thisMonth(): string {
  return today().slice(0, 7);
}

/** The month before thisMonth(): a month no payment of this run can land in. */
export function lastMonth(): string {
  const [year, month] = thisMonth().split('-').map(Number);
  const previous = new Date(year, month - 2, 1);
  return `${previous.getFullYear()}-${String(previous.getMonth() + 1).padStart(2, '0')}`;
}

// The smallest PDF the upload validator accepts: one empty page, with the
// cross-reference offsets exactly right. Uploads are parsed as real PDFs now
// rather than sniffed for a "%PDF" prefix, so a stand-in made of a header and
// a trailer is refused as a corrupt file. The trailing spaces in the xref
// entries are part of the format (each entry is exactly twenty bytes).
export const validProofPdf = [
  '%PDF-1.4',
  '1 0 obj',
  '<< /Type /Catalog /Pages 2 0 R >>',
  'endobj',
  '2 0 obj',
  '<< /Type /Pages /Count 1 /Kids [3 0 R] >>',
  'endobj',
  '3 0 obj',
  '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << >> >>',
  'endobj',
  'xref',
  '0 4',
  '0000000000 65535 f ',
  '0000000009 00000 n ',
  '0000000058 00000 n ',
  '0000000115 00000 n ',
  'trailer',
  '<< /Size 4 /Root 1 0 R >>',
  'startxref',
  '203',
  '%%EOF',
  ''
].join('\n');

/**
 * validProofPdf as an in-memory upload, for setInputFiles or a multipart POST.
 * `name` is the file name the server stores and shows.
 */
export function pdfUpload(name: string) {
  return { name, mimeType: 'application/pdf', buffer: Buffer.from(validProofPdf) };
}

export async function login(page: Page, email: string, password: string) {
  await page.goto('/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Login' }).click();
  await expect(page).toHaveURL(/\/$/);
}

/**
 * Adds a user through the "＋ Add user" sheet on /users, ticking the given
 * roles, with fixturePassword. Every field is looked up inside the sheet,
 * because each existing user's edit sheet has its own Name and password fields.
 */
async function addUser(page: Page, user: { email: string; name: string; roles: RegExp[] }) {
  await page.goto('/users');
  await openNewUserSheet(page);
  const sheet = page.locator('#user-new');
  await sheet.getByLabel('Email').fill(user.email);
  await sheet.getByLabel('Name').fill(user.name);
  for (const role of user.roles) await sheet.getByRole('checkbox', { name: role }).check();
  await sheet.getByLabel('Password').fill(fixturePassword);
  await sheet.getByRole('button', { name: 'Add User' }).click();
  await expect(page).toHaveURL(/\/users$/);
}

/** An accountant whose email is `${runId}@example.test`, named "Data Entry …". */
export async function createDataEntryUser(page: Page, runId: string) {
  const email = `${runId}@example.test`;
  await addUser(page, { email, name: `Data Entry ${runId}`, roles: [/^Accounts/] });
  return { email, password: fixturePassword };
}

export async function openNewUserSheet(page: Page) {
  await page.locator('.pb-actions').getByRole('button', { name: 'Add user' }).click();
  await expect(page.locator('#user-new')).toBeVisible();
}

/**
 * Creates a user holding the given system roles (matched by their checkbox
 * label, e.g. /^Manager/). The email is `${prefix}-${runId}@example.test` in
 * lower case and the name `${prefix} ${runId}`.
 */
export async function createUser(page: Page, prefix: string, runId: string, roles: RegExp[]) {
  const email = `${prefix}-${runId}@example.test`.toLowerCase();
  const name = `${prefix} ${runId}`;
  await addUser(page, { email, name, roles });
  return { email, name, password: fixturePassword };
}

/**
 * An accountant: the Accounts role holds payment:process and reservation:reserve,
 * which is what puts a person in front of /accounts-queue and lets them take a
 * request out of it.
 */
export async function createAccountsUser(page: Page, runId: string) {
  return createUser(page, 'accounts', runId, [/^Accounts/]);
}

/**
 * An approver: the Manager role holds approval:approve. Nobody may approve
 * their own request, so every approved request needs a second person like this.
 *
 * Its email is the one createApprovedRequest's cached approver uses, so do not
 * call both with the same runId in one test.
 */
export async function createApproverUser(page: Page, runId: string) {
  return createUser(page, 'approver', runId, [/^Manager/]);
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
 * Makes sure a company vendor with exactly this name exists, so the payee on a
 * request (and therefore on its payment) is the caller's to choose.
 *
 * Vendor names are unique regardless of case, so a name an earlier run already
 * used lands on the refusal page instead of the new vendor. The vendor exists
 * either way, which is all this needs; the request form's vendor picker fails
 * loudly if the name is genuinely absent. A spec that must prove a vendor was
 * created should assert on /vendors/{id} itself.
 */
export async function ensureVendor(page: Page, name: string) {
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
 * Raises a vendor-invoice request through the real form and has a second user
 * (a Manager created once per runId) approve it in full, because nobody may
 * approve their own request.
 *
 * `payee` becomes the request's vendor and appears on the resulting payment.
 * `head` is the "Project / Head" label to charge (default Operations / Office
 * Rent); every payment lands in the current month, so a spec that needs a head
 * nobody else in the run has paid or budgeted picks its own here.
 *
 * Returns the request number (PR-YYYY-NNNNNN) and id.
 */
export async function createApprovedRequest(
  page: Page,
  runId: string,
  opts: { amount: string; payee?: string; neededBy?: string; head?: string }
): Promise<{ number: string; id: number }> {
  const approver = await ensureApprover(page, runId);
  const payee = opts.payee ?? `Payee ${runId}`;
  await ensureVendor(page, payee);
  const headLabel = opts.head ?? 'Operations / Office Rent';
  const projectLabel = headLabel.split(' / ')[0];

  const seq = (requestSeq.get(runId) ?? 0) + 1;
  requestSeq.set(runId, seq);

  await page.goto('/requests/new?type=vendor_invoice');
  await page.getByLabel('Short title').fill(`Supply ${seq} ${runId}`);

  // Choosing a project swaps #form-fields from the server and narrows the heads
  // to that project's own. Waiting for a head count would be guessing at the
  // seed; waiting for every remaining option to belong to the project is the
  // swap itself. Only the "Choose a head" placeholder survives the filter.
  await page.locator('#project').selectOption({ label: projectLabel });
  await expect(page.locator('#head option').filter({ hasNotText: `${projectLabel} /` })).toHaveCount(1);
  await page.locator('#head').selectOption({ label: headLabel });

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

  // One POST creates, numbers and submits the request; there is no draft step.
  await expect(page).toHaveURL(/\/requests\/\d+\/submitted$/);
  const id = Number(new URL(page.url()).pathname.split('/')[2]);
  const number = (await page.locator('.rh-no').first().innerText()).trim();

  await approveInFull(page, approver, id, opts.amount);

  return { number, id };
}

/**
 * Signs `approver` in, in a browser context of its own (sized like `page`),
 * and approves request `id` for `amount` through the approve sheet.
 *
 * Approving the full amount keeps the approved amount equal to what the caller
 * asked for, so a settlement can be compared against it.
 */
export async function approveInFull(
  page: Page,
  approver: { email: string; password: string },
  id: number,
  amount: string
) {
  const browser = page.context().browser();
  if (!browser) throw new Error('approveInFull needs a browser-backed context to sign the approver in');
  const context = await browser.newContext({ viewport: page.viewportSize() ?? undefined });
  try {
    const approverPage = await context.newPage();
    await login(approverPage, approver.email, approver.password);
    await approveOnSheet(approverPage, id, amount);
  } finally {
    await context.close();
  }
}

/**
 * As the request's approver, already signed in on `approverPage`: opens
 * request `id`, approves `amount` through the approve sheet and lands back on
 * /approvals.
 */
export async function approveOnSheet(approverPage: Page, id: number, amount: string) {
  await approverPage.goto(`/requests/${id}`);
  await approverPage.getByRole('button', { name: /^Approve / }).click();
  const sheet = approverPage.locator('#approve-sheet');
  await expect(sheet).toBeVisible();
  await sheet.getByLabel('Amount approved').fill(amount);
  await sheet.getByRole('button', { name: 'Approve request' }).click();
  await expect(approverPage).toHaveURL(/\/approvals$/);
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
