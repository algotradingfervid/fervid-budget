// Every payment-request transition, expressed as the UI interaction a human
// would perform. Nothing here touches SQLite.
//
// Two structural facts drive the shape of this file:
//  1. Most manager/accounts controls live behind a disclosure "sheet". The
//     opener is `button[data-open="<sheet-id>"]` and the form lives inside
//     `#<sheet-id>`. Scoping to the sheet matters: the accept and decline
//     cancellation forms share one action attribute and differ only by sheet.
//  2. Recording a payment is a two-step confirm: the entry form posts to
//     /requests/{id}/settlement-preview, and only the sheet that comes back
//     posts to /payments.

import { expect, type Page } from '@playwright/test';
import { requestIdFromUrl } from './helpers';
import type { PlanRequest } from './plan';

export interface Ids {
  projects: Record<string, number>;
  heads: Record<string, number>;
  users: Record<string, number>;
  vendors: Record<string, number>;
}

export interface CreatedRequest {
  id: number;
  number: string;
}

/**
 * A PlanRequest with the lookups the specs already resolved: `headLabel` is the
 * "Project / Head" string the budget grid uses as its own key, and
 * `approverEmail` identifies the approver independently of database ids.
 */
export type ResolvedRequest = PlanRequest & {
  headLabel: string;
  approverEmail: string;
};

const NUMBER_RE = /PR-\d{4}-\d+/;

async function fillField(scope: ReturnType<Page['locator']>, name: string, value: string): Promise<void> {
  const field = scope.locator(`[name="${name}"]`).first();
  if (!(await field.count())) throw new Error(`field ${name} not present`);
  const tag = await field.evaluate((el) => el.tagName.toLowerCase());
  if (tag === 'select') {
    await field.selectOption(value);
    return;
  }
  const type = (await field.getAttribute('type')) ?? 'text';
  if (type === 'checkbox') {
    if (value === 'on') await field.check();
    else await field.uncheck();
  } else if (type === 'radio') {
    await scope.locator(`[name="${name}"][value="${value}"]`).first().check();
  } else {
    await field.fill(value);
  }
}

/** Assert the app did not answer with an error banner or an error page. */
async function assertAccepted(page: Page, what: string): Promise<void> {
  await page.waitForLoadState('domcontentloaded');
  const bad = page.locator('.banner.bad, .error, [role="alert"]').first();
  if (await bad.count()) {
    const text = (await bad.innerText().catch(() => '')).trim();
    if (text && !/on hold|frozen|rejected this|sent this back/i.test(text)) {
      throw new Error(`${what} refused: ${text.slice(0, 200)}`);
    }
  }
}

export async function actOnRequest(
  page: Page,
  id: number,
  opts: {
    path?: string;
    opener?: string;
    action: string;
    fields?: Record<string, string>;
  },
): Promise<void> {
  await page.goto(opts.path ?? `/requests/${id}`);
  if (opts.opener) {
    const opener = page.locator(`[data-open="${opts.opener}"]`).first();
    await expect(opener, `opener ${opts.opener} missing on request ${id}`).toHaveCount(1);
    await opener.click();
  }
  // Scope to the sheet when there is one — sibling sheets can share an action.
  const scope = opts.opener ? page.locator(`#${opts.opener}`) : page;
  const form = scope.locator(`form[action="/requests/${id}/${opts.action}"]`).first();
  await expect(form, `form ${opts.action} missing on request ${id}`).toHaveCount(1);

  for (const [name, value] of Object.entries(opts.fields ?? {})) {
    await fillField(form, name, value);
  }
  await form.locator('button[type="submit"], button:not([type="button"])').first().click();
  await assertAccepted(page, `${opts.action} on request ${id}`);
}

// ------------------------------------------------------------------ create

export async function createRequest(page: Page, req: ResolvedRequest, ids: Ids): Promise<CreatedRequest> {
  await page.goto(`/requests/new?type=${req.type}`);

  // Changing the treatment makes the server re-render the field block over htmx.
  // Do it first, only when it actually changes, and let the swap settle — a swap
  // that lands after we have filled the block silently wipes what we typed.
  const treatment = page.locator(`input[name="treatment"][value="${req.treatment}"]`);
  if (await treatment.count()) {
    if (!(await treatment.first().isChecked())) {
      await treatment.first().check();
      await page.waitForLoadState('networkidle');
    }
  }

  await page.fill('input[name="short_title"]', req.shortTitle);
  await page.fill('input[name="amount"]', String(req.amount));
  await page.fill('textarea[name="purpose"], input[name="purpose"]', req.purpose);

  const neededBy = page.locator('input[name="needed_by"]');
  if (await neededBy.count()) await neededBy.fill(req.neededBy);

  if (req.treatment === 'budget') {
    // Resolve strictly: a missing id used to fall through and submit the form
    // without a project/head, which the app rejects with a message that does not
    // name the cause. Fail here instead, where the cause is obvious.
    const projectName = projectNameOf(req);
    const projectId = ids.projects[projectName];
    if (!projectId) throw new Error(`no id for project "${projectName}" (request ${req.key})`);
    const headId = ids.heads[req.headLabel];
    if (!headId) throw new Error(`no id for head "${req.headLabel}" (request ${req.key})`);

    await page.selectOption('select[name="project_id"]', String(projectId));
    await page.selectOption('select[name="head_id"]', String(headId));
  }

  if (req.type === 'vendor_invoice' || req.type === 'vendor_advance') {
    const vendorId = ids.vendors[req.vendor];
    if (vendorId) await page.selectOption('select[name="vendor_id"]', String(vendorId));
  }
  if (req.type === 'vendor_invoice') {
    await page.fill('input[name="invoice_no"]', req.invoiceNo);
    await page.fill('input[name="invoice_date"]', req.invoiceDate);
  }
  if (req.type === 'vendor_advance' || req.type === 'employee_advance') {
    const reason = page.locator('textarea[name="advance_reason"], input[name="advance_reason"]');
    if (await reason.count()) await reason.first().fill(req.advanceReason);
  }
  if (req.type === 'reimbursement') {
    await page.fill('input[name="expense_date"]', req.expenseDate);
  }
  if (req.treatment === 'recoverable') {
    // These controls only exist after the treatment swap has landed. Waiting for
    // them beats probing with count(), which silently skips the field when the
    // swap is still in flight and lets the form submit incomplete.
    const ret = page.locator('input[name="expected_return_date"]');
    await ret.waitFor({ state: 'visible', timeout: 15_000 });

    const cat = page.locator('select[name="recoverable_category"]');
    if (await cat.count()) await cat.selectOption('employee_advance');
    await ret.fill(req.expectedReturnDate);
    const notes = page.locator('textarea[name="repayment_notes"], input[name="repayment_notes"]');
    await notes.first().fill(req.repaymentNotes);
  }
  if (req.urgent) {
    const flag = page.locator('input[name="urgent"]');
    if (await flag.count()) {
      await flag.first().check();
      const why = page.locator('textarea[name="urgency_reason"], input[name="urgency_reason"]');
      if (await why.count()) await why.first().fill(req.urgencyReason);
    }
  }

  const approverId = ids.users[req.approverEmail];
  if (approverId) {
    const sel = page.locator('select[name="manager_id"]');
    if (await sel.count()) await sel.selectOption(String(approverId));
  }

  // Last line of defence against a late htmx swap, which replaces the field
  // block with a fresh empty one and discards whatever was typed into the old.
  // Re-assert every field the server validates, immediately before submitting.
  const required: Array<[string, string]> = [
    ['input[name="short_title"]', req.shortTitle],
    ['input[name="amount"]', String(req.amount)],
  ];
  if (req.treatment === 'budget') {
    required.push(
      ['select[name="project_id"]', String(ids.projects[projectNameOf(req)])],
      ['select[name="head_id"]', String(ids.heads[req.headLabel])],
    );
  } else {
    required.push(
      ['input[name="expected_return_date"]', req.expectedReturnDate],
      ['textarea[name="repayment_notes"]', req.repaymentNotes],
    );
  }
  if (req.type === 'vendor_invoice') {
    required.push(['input[name="invoice_no"]', req.invoiceNo], ['input[name="invoice_date"]', req.invoiceDate]);
  }
  if (req.type === 'reimbursement') required.push(['input[name="expense_date"]', req.expenseDate]);

  for (const [selector, want] of required) {
    const field = page.locator(selector).first();
    if (!(await field.count())) continue;
    if ((await field.inputValue()) === want) continue;
    if (selector.startsWith('select')) await field.selectOption(want);
    else await field.fill(want);
  }

  await page.locator('form[action="/requests"] button[type="submit"], form[action="/requests"] button.primary').first().click();
  await page.waitForLoadState('domcontentloaded');

  const id = requestIdFromUrl(page.url());
  if (!id) {
    const err = (await page.locator('.banner.bad, .error, [role="alert"]').first().innerText().catch(() => '')) || '';
    throw new Error(`request ${req.key} was not created (url ${page.url()}) ${err.slice(0, 200)}`);
  }
  await page.goto(`/requests/${id}`);
  const body = await page.locator('body').innerText();
  const number = NUMBER_RE.exec(body)?.[0] ?? '';
  return { id, number };
}

function projectNameOf(req: ResolvedRequest): string {
  return req.headLabel.split(' / ')[0]!.trim();
}

// -------------------------------------------------------------- transitions

export const approve = (page: Page, id: number, amount: number, note = 'Approved.') =>
  actOnRequest(page, id, {
    opener: 'approve-sheet',
    action: 'approve',
    fields: { approved_amount: String(amount), note },
  });

export const returnForCorrection = (page: Page, id: number, comment: string) =>
  actOnRequest(page, id, { opener: 'return-sheet', action: 'return', fields: { comment } });

export const reject = (page: Page, id: number, reason: string) =>
  actOnRequest(page, id, { opener: 'reject-sheet', action: 'reject', fields: { reason } });

export const reassignApprover = (page: Page, id: number, managerId: number, reason: string) =>
  actOnRequest(page, id, {
    opener: 'reassign-approver-sheet',
    action: 'reassign-approver',
    fields: { manager_id: String(managerId), reason },
  });

export const withdraw = (page: Page, id: number) =>
  actOnRequest(page, id, { action: 'withdraw' });

export const reraise = async (page: Page, id: number): Promise<number | null> => {
  await actOnRequest(page, id, { action: 'reraise' });
  return requestIdFromUrl(page.url());
};

/**
 * Send a returned request back up for approval.
 *
 * The correction form is embedded in the request DETAIL page, not on
 * /requests/{id}/edit — that page renders a single "Save and notify" button with
 * no submit_action, so saving there leaves the request sitting in `returned`.
 * The detail page is the one carrying both submit_action buttons, and only
 * `resubmit` performs the transition.
 */
export async function resubmit(page: Page, id: number, note: string): Promise<void> {
  await page.goto(`/requests/${id}`);
  const button = page.locator('button[name="submit_action"][value="resubmit"]').first();
  await expect(button, `no resubmit control on request ${id}`).toHaveCount(1);

  const purpose = page.locator(`form[action="/requests/${id}/edit"] textarea[name="purpose"]`).first();
  if (await purpose.count()) {
    const current = await purpose.inputValue();
    await purpose.fill(`${current}\n\nRevised after review: ${note}`);
  }
  await button.click();
  await assertAccepted(page, `resubmit ${id}`);
}

export const askCancellation = (page: Page, id: number, reason: string) =>
  actOnRequest(page, id, { path: `/requests/${id}/cancel`, action: 'cancel-request', fields: { reason } });

export const decideCancellation = (page: Page, id: number, accept: boolean, note: string) =>
  actOnRequest(page, id, {
    path: `/requests/${id}/cancellation`,
    opener: accept ? 'accept-sheet' : 'decline-sheet',
    action: 'cancellation',
    fields: { note },
  });

// Cancelling an approved request outright sits behind the "Cancel with reason"
// disclosure on the cancellation page, not inline.
export const cancelWithReason = (page: Page, id: number, reason: string) =>
  actOnRequest(page, id, {
    path: `/requests/${id}/cancellation`,
    opener: 'outright-sheet',
    action: 'cancel',
    fields: { reason },
  });

export const hold = (page: Page, id: number, reason: string) =>
  actOnRequest(page, id, { opener: 'hold-sheet', action: 'hold', fields: { reason } });

export const unhold = (page: Page, id: number) => actOnRequest(page, id, { action: 'unhold' });

export const acceptPartial = (page: Page, id: number, note: string) =>
  actOnRequest(page, id, {
    path: `/requests/${id}/partial-review`,
    opener: 'close-sheet',
    action: 'accept-partial',
    fields: { note },
  });

export const raiseConcern = (page: Page, id: number, comment: string) =>
  actOnRequest(page, id, {
    path: `/requests/${id}/partial-review`,
    opener: 'concern-sheet',
    action: 'raise-concern',
    fields: { comment },
  });

// ------------------------------------------------------------------- status

// The detail page shows the status as a pill whose class is the stable handle.
// Hold is rendered INSTEAD of the real status, so it reads back as 'on_hold'.
const PILL_TO_STATUS: Record<string, string> = {
  awaiting: 'pending',
  returned: 'returned',
  approved: 'approved',
  rejected: 'rejected',
  cancelreq: 'cancellation_requested',
  cancelled: 'cancelled',
  processing: 'processing',
  partial: 'partial_review',
  completed: 'completed',
  'completed-partial': 'completed_partial',
  hold: 'on_hold',
};

/** Read a request's current status from the UI, so a phase can resume safely. */
export async function statusOf(page: Page, id: number): Promise<string> {
  await page.goto(`/requests/${id}`);
  const classes = await page
    .locator('.rh-status span.pill')
    .evaluateAll((els) => els.map((e) => e.className));
  for (const cls of classes) {
    for (const token of cls.split(/\s+/)) {
      const mapped = PILL_TO_STATUS[token];
      if (mapped) return mapped;
    }
  }
  return 'unknown';
}

// ----------------------------------------------------------------- payments

/**
 * Take a request for processing (approved -> processing). The press lives on the
 * accounts queue, which is searchable by request number, so that is how we find
 * the row rather than guessing a page offset.
 */
export async function reserve(page: Page, id: number, number: string): Promise<void> {
  await page.goto(`/accounts-queue?tab=approved&q=${encodeURIComponent(number)}`);
  const form = page.locator(`form[action="/requests/${id}/record-payment"]`).first();

  if (!(await form.count())) {
    // Not on the approved queue. Either somebody else holds it, or this is a
    // resumed run and we already took it — the payment entry form only renders
    // for the holder, so it is the honest test of which case we are in.
    await page.goto(`/payments/new?request=${id}`);
    if (await page.locator('input[name="amount"]').count()) return;
    throw new Error(`request ${number} is neither takeable nor already held by this user`);
  }

  await form.locator('button[type="submit"]').first().click();
  await page.waitForLoadState('domcontentloaded');
  const conflict = page.locator('.banner.warn, .banner.bad').first();
  if (await conflict.count()) {
    const text = (await conflict.innerText().catch(() => '')).trim();
    if (/cannot be taken|not available|took this request/i.test(text)) {
      throw new Error(`reserve refused for ${number}: ${text.slice(0, 160)}`);
    }
  }
}

export async function releaseReservation(page: Page, id: number, reason: string): Promise<void> {
  await page.goto(`/requests/${id}/reservation`);
  const form = page.locator('form').filter({ has: page.locator('[name="reason"]') }).first();
  await expect(form, `no reservation form on ${id}`).toHaveCount(1);
  await form.locator('[name="reason"]').first().fill(reason);
  const confirm = form.locator('[name="confirm"]').first();
  if (await confirm.count()) await confirm.check();
  await form.locator(`button[formaction="/requests/${id}/release"], button:has-text("Release reservation")`).first().click();
  await assertAccepted(page, `release ${id}`);
}

/**
 * Record the payment. Assumes the caller already holds the reservation.
 * settlement: 'settled' completes the request; 'partial' sends it to the
 * manager for partial review.
 */
export async function recordPayment(
  page: Page,
  id: number,
  opts: { amount: number; paidOn: string; mode: string; reference: string; settlement: 'settled' | 'partial'; partialReason?: string },
): Promise<void> {
  await page.goto(`/payments/new?request=${id}`);
  await page.fill('input[name="amount"]', String(opts.amount));
  await page.fill('input[name="paid_on"]', opts.paidOn);

  const mode = page.locator('select[name="payment_mode"]');
  if (await mode.count()) {
    const values = await mode.locator('option').evaluateAll((os) =>
      os.map((o) => (o as HTMLOptionElement).value).filter(Boolean),
    );
    await mode.selectOption(values.includes(opts.mode) ? opts.mode : values[0]!);
  }
  await page.fill('input[name="reference_no"]', opts.reference);

  // Step 1 -> the settlement sheet.
  await page.locator(`button[formaction="/requests/${id}/settlement-preview"]`).first().click();
  await page.waitForLoadState('domcontentloaded');

  const sheet = page.locator('#settle-sheet');
  await expect(sheet, `settlement sheet did not open for ${id}`).toBeVisible({ timeout: 15_000 });

  if (opts.settlement === 'partial') {
    await sheet.locator('input[name="settlement"][value="partial"]').first().check();
    await sheet.locator('[name="partial_reason"]').first().fill(opts.partialReason ?? 'Short payment against the approved amount.');
  } else {
    const settled = sheet.locator('input[name="settlement"][value="settled"]').first();
    if (await settled.count()) await settled.check();
  }

  await sheet.locator('button[formaction="/payments"]').first().click();
  await page.waitForLoadState('domcontentloaded');
  await assertAccepted(page, `payment for request ${id}`);
}
