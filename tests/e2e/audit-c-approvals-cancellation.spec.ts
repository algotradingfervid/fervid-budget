/**
 * Audit C — the approver's decisions and the cancellation flow.
 *
 * SCOPE. Everything between "the request is pending" and "the request is
 * approved", plus the whole cancellation flow that hangs off `approved`.
 * Raising a request belongs to another agent and so does everything after
 * approval (reservation, payment, settlement); both appear here only as the
 * cheapest way to reach a state this audit needs.
 *
 * WHY IT DOES NOT DUPLICATE request-workflow.spec.ts. That file walks one happy
 * journey on two devices and applies the quality bar (axe, tap targets, chrome)
 * to every screen it lands on. It never asks who is refused, never sends a
 * malformed amount, never drives an illegal transition, and never races two
 * sessions. This file asks only those questions, and it asks them with subjects
 * whose role set is EXACTLY one role — which is what `audit-support.ts` exists
 * for and what `fixtures.createApproverUser` cannot give you (see that file's
 * header: a "Manager" made the fixtures' way is Accounts + Manager).
 *
 * THE CAST IS BUILT ONCE. Seven users at ~10 page interactions each is a minute
 * of setup, so the cast lives in a file-level `beforeAll` rather than in a
 * per-test fixture. Every test raises its own request against that cast, so no
 * test depends on another's state.
 *
 * WHY SO MANY PROBES AND SO FEW CLICKS. A permission matrix is a statement about
 * status codes, and a click that lands on an error page proves less than the
 * code itself: `probeGet`/`probePost` do not follow redirects, so a refusal
 * stays a refusal instead of becoming a 200 for the login screen. The journeys
 * that a person actually walks — approve, return, cancel, decline, reroute — are
 * driven through the real controls.
 *
 * TRACEABILITY. Every test is named with its TC id from
 * docs/qa/test-cases/TC-C-approvals-cancellation.md, whose verdict column names
 * the finding (F-C-01 … F-C-06) that any failing case belongs to.
 */
import { test, expect, admin, login, settlePayment } from './fixtures';
import {
  createUserWithExactRoles,
  probeGet,
  probePost,
  probeAnonymous,
  signIn,
  type Probe,
  type RoleName,
  type Subject
} from './audit-support';
import type { Browser, BrowserContext, Page } from '@playwright/test';

// The cast is expensive to build and several journeys walk three screens with a
// second signed-in person, so the default 30 s is not enough for either.
test.describe.configure({ timeout: 180_000 });

interface Actor {
  subject: Subject;
  context: BrowserContext;
  page: Page;
  /** The user's own row id, read from an approver <select> the server rendered. */
  id: string;
}

let runId!: string;
let bossCtx!: BrowserContext;
/** The seeded administrator: holds all 66 grants and is nobody's approver here. */
let boss!: Page;
/** The same administrator wearing the Actor shape, so the helpers accept them. */
let bossActor!: Actor;
let requester!: Actor; // exactly [Requester] — raises everything
let outsider!: Actor; // exactly [Requester] — a second one, for ownership probes
let mgrA!: Actor; // exactly [Manager] — the approver every request is sent to
let mgrB!: Actor; // exactly [Manager] — an approver it was NOT sent to
let accounts!: Actor; // exactly [Accounts] — holds payment:hold, no approval verb
let nobody!: Actor; // no role at all
let dual!: Actor; // [Requester, Manager] — the sharpest G8 probe
let projectId!: string;
let headId!: string;
let adminId!: string;
let seq = 0;

test.beforeAll(async ({ browser }) => {
  test.setTimeout(300_000);
  runId = `auditc${Date.now().toString(36)}${Math.random().toString(36).slice(2, 5)}`;

  bossCtx = await browser.newContext();
  boss = await bossCtx.newPage();
  await login(boss, admin.email, admin.password);

  const cast = async (prefix: string, roles: RoleName[]): Promise<Actor> => {
    const subject = await createUserWithExactRoles(boss, prefix, runId, roles);
    const session = await signIn(browser, subject);
    return { subject, context: session.context, page: session.page, id: '' };
  };

  requester = await cast('creq', ['Requester']);
  outsider = await cast('cout', ['Requester']);
  mgrA = await cast('cmga', ['Manager']);
  mgrB = await cast('cmgb', ['Manager']);
  accounts = await cast('cacc', ['Accounts']);
  nobody = await cast('cnon', []);
  dual = await cast('cdua', ['Requester', 'Manager']);

  // The project, the head and every approver's row id come from the form the
  // requester actually fills in, so the ids under test are the ones the product
  // would have posted. ListApprovers is also what makes the G8 assertions
  // meaningful: a name absent from this list is a name the server refuses.
  const form = await probeGet(requester.page, '/requests/new?type=reimbursement');
  expect(form.status, 'a Requester must reach the new-request form').toBe(200);
  projectId = optionValue(form.body, 'Operations');
  headId = optionValue(form.body, 'Operations / Office Rent');
  mgrA.id = optionValue(form.body, mgrA.subject.name);
  mgrB.id = optionValue(form.body, mgrB.subject.name);
  dual.id = optionValue(form.body, dual.subject.name);
  adminId = optionValue(form.body, 'Fervid Admin');
  // `roles` is only ever read by signIn, which the administrator does not need;
  // the seeded admin's grants come from the `admin` legacy role, not from a
  // checkbox this suite ticked.
  bossActor = {
    subject: { email: admin.email, name: 'Fervid Admin', password: admin.password, roles: [] },
    context: bossCtx,
    page: boss,
    id: adminId
  };
});

test.afterAll(async () => {
  for (const actor of [requester, outsider, mgrA, mgrB, accounts, nobody, dual]) {
    await actor?.context.close();
  }
  await bossCtx?.close();
});

// --------------------------------------------------------------------------
// Helpers. All local to this file: audit-support.ts and fixtures.ts are shared
// and shipped, and this audit adds to the repo without touching them.
// --------------------------------------------------------------------------

/** The row id behind an <option> the server rendered, matched on its exact label. */
function optionValue(html: string, label: string): string {
  const escaped = label.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&');
  const match = html.match(new RegExp(`<option value="(\\d+)"[^>]*>${escaped}</option>`));
  if (!match) throw new Error(`no <option> labelled "${label}" in the rendered form`);
  return match[1];
}

/** A complete, valid reimbursement body. Reimbursement needs no vendor row. */
function requestBody(title: string, managerId: string, amount = '18400'): Record<string, string> {
  return {
    type: 'reimbursement',
    treatment: 'budget',
    project_id: projectId,
    head_id: headId,
    short_title: title,
    amount,
    expense_date: '2026-07-17',
    purpose: `Audit C — ${title}.`,
    manager_id: managerId
  };
}

/** Raises a pending request and returns its id and title. */
async function raise(
  actor: Actor,
  managerId: string,
  opts: { amount?: string; title?: string } = {}
): Promise<{ id: number; title: string }> {
  const title = opts.title ?? `AC-${++seq} ${runId}`;
  const probe = await probePost(actor.page, '/requests', requestBody(title, managerId, opts.amount));
  expect(probe.status, `raising "${title}" must succeed — got ${probe.outcome} ${probe.body.slice(0, 200)}`).toBe(303);
  return { id: Number(probe.location!.split('/')[2]), title };
}

const approve = (actor: Actor, id: number, amount: string, note = '') =>
  probePost(actor.page, `/requests/${id}/approve`, { approved_amount: amount, note });
const sendBack = (actor: Actor, id: number, comment: string) =>
  probePost(actor.page, `/requests/${id}/return`, { comment });
const reject = (actor: Actor, id: number, reason: string) =>
  probePost(actor.page, `/requests/${id}/reject`, { reason });
const withdraw = (actor: Actor, id: number) => probePost(actor.page, `/requests/${id}/withdraw`, {});
const reraise = (actor: Actor, id: number) => probePost(actor.page, `/requests/${id}/reraise`, {});
const askCancel = (actor: Actor, id: number, reason: string) =>
  probePost(actor.page, `/requests/${id}/cancel-request`, { reason });
const cancelOutright = (actor: Actor, id: number, reason: string) =>
  probePost(actor.page, `/requests/${id}/cancel`, { reason });
const decideCancel = (actor: Actor, id: number, decision: 'accept' | 'decline', note: string) =>
  probePost(actor.page, `/requests/${id}/cancellation`, { decision, note });
const hold = (actor: Actor, id: number, reason: string) =>
  probePost(actor.page, `/requests/${id}/hold`, { reason });
const resubmit = (actor: Actor, id: number, title: string, managerId: string) =>
  probePost(actor.page, `/requests/${id}/edit`, { ...requestBody(title, managerId), submit_action: 'resubmit' });
/**
 * Saves a correction WITHOUT resubmitting.
 *
 * The distinction is not cosmetic. `submit_action=resubmit` also calls
 * SubmitRequest, and `canTransition("pending","pending")` is false, so a
 * resubmission of a still-pending request is refused (400) — see TC-C-123. A
 * requester rerouting a pending request therefore saves; only the returned
 * screen resubmits.
 */
const editRequest = (actor: Actor, id: number, title: string, managerId: string) =>
  probePost(actor.page, `/requests/${id}/edit`, { ...requestBody(title, managerId), submit_action: 'save' });

/** Asserts a probe succeeded, naming the body when it did not. */
function succeeded(probe: Probe, what: string) {
  expect(probe.status, `${what}: expected 303, got ${probe.outcome} ${probe.body.slice(0, 300)}`).toBe(303);
}

/**
 * Asserts the app refused, and refused as a client error rather than falling
 * over. 400 and 403 are the only two refusals the request handlers can produce:
 * store.ErrValidation maps to 400 and store.ErrForbidden to 403
 * (internal/app/http_errors.go:199). A 404 would mean the route vanished and a
 * 5xx would mean a guard crashed instead of answering.
 */
function refused(probe: Probe, what: string) {
  expect([400, 403], `${what}: expected a refusal, got ${probe.outcome} ${probe.body.slice(0, 200)}`).toContain(
    probe.status
  );
}

/**
 * One row of the detail screen's definition list, addressed by its own label.
 *
 * `.dl` as a whole is useless for this: it holds both "Amount" and "Approved",
 * so `toContainText('₹12,000.00')` would pass whichever of the two carried it —
 * which is exactly the confusion an adjusted approval has to rule out.
 */
const dlValue = (page: Page, label: string) => page.locator(`.dl dt:text-is("${label}") + dd`);

/** The status pill a reader sees on the request's own screen. */
async function statusPill(actor: Actor, id: number): Promise<string> {
  await actor.page.goto(`/requests/${id}`);
  return (await actor.page.locator('.rh-status .pill').last().innerText()).trim();
}

/** Every pill in the request head, so "On hold" can be proved absent. */
async function statusPills(actor: Actor, id: number): Promise<string[]> {
  await actor.page.goto(`/requests/${id}`);
  return (await actor.page.locator('.rh-status .pill').allInnerTexts()).map(t => t.trim());
}

/** The merged history-and-conversation stream, as text. */
async function thread(actor: Actor, id: number): Promise<string> {
  await actor.page.goto(`/requests/${id}`);
  return (await actor.page.locator('ol.thread').textContent()) ?? '';
}

/**
 * The audit log filtered to one request, read by the administrator — the only
 * person holding audit:view. textContent, not innerText: the before/after JSON
 * lives inside a closed <details> and innerText would drop it.
 */
async function auditTrail(id: number): Promise<string> {
  await boss.goto(`/audit?entity=payment_request&id=${id}`);
  return (await boss.locator('table.t-cards').textContent()) ?? '';
}

/** PR-YYYY-NNNNNN, which is the only thing the Accounts queue search matches. */
async function requestNumber(actor: Actor, id: number): Promise<string> {
  const probe = await probeGet(actor.page, `/requests/${id}`);
  const match = probe.body.match(/class="rh-no">([^<]+)</);
  if (!match) throw new Error(`no .rh-no on /requests/${id}`);
  return match[1].trim();
}

/** How many cards a manager's queue shows for one title, in one bucket. */
async function queueCards(actor: Actor, bucket: string, title: string): Promise<number> {
  await actor.page.goto(`/approvals?bucket=${bucket}&q=${encodeURIComponent(title)}`);
  return actor.page.locator('.req-card').count();
}

/** An unauthenticated POST. probeAnonymous only does GET. */
async function anonymousPost(browser: Browser, path: string, baseURL: string): Promise<Probe> {
  const context = await browser.newContext();
  try {
    const response = await context.request.post(new URL(path, baseURL).toString(), {
      form: { csrf: 'irrelevant' },
      maxRedirects: 0,
      failOnStatusCode: false
    });
    const location = response.headers()['location'] ?? null;
    return {
      status: response.status(),
      location,
      body: '',
      outcome: location ? `${response.status()} → ${location}` : String(response.status())
    };
  } finally {
    await context.close();
  }
}

// State builders. Each returns a request already in the state its name says.
async function pendingRequest(managerId = mgrA.id, amount?: string) {
  return raise(requester, managerId, { amount });
}
async function approvedRequest(amount = '18400', approved = amount) {
  const req = await pendingRequest(mgrA.id, amount);
  succeeded(await approve(mgrA, req.id, approved), 'approving the fixture');
  return req;
}
async function returnedRequest() {
  const req = await pendingRequest();
  succeeded(await sendBack(mgrA, req.id, 'Attach the receipt.'), 'returning the fixture');
  return req;
}
async function rejectedRequest() {
  const req = await pendingRequest();
  succeeded(await reject(mgrA, req.id, 'Not budgeted this quarter.'), 'rejecting the fixture');
  return req;
}
async function withdrawnRequest() {
  const req = await pendingRequest();
  succeeded(await withdraw(requester, req.id), 'withdrawing the fixture');
  return req;
}
async function cancellationRequested() {
  const req = await approvedRequest();
  succeeded(await askCancel(requester, req.id, 'The trip is off.'), 'asking for cancellation');
  return req;
}
async function cancelledRequest() {
  const req = await approvedRequest();
  succeeded(await cancelOutright(mgrA, req.id, 'Duplicate of last month.'), 'cancelling the fixture');
  return req;
}

// --------------------------------------------------------------------------
// 1. The approvals queue.
// --------------------------------------------------------------------------

test.describe('the approvals queue', () => {
  test('TC-C-001 — an anonymous caller is sent to the login screen, not answered', async ({ browser, baseURL }) => {
    const probe = await probeAnonymous(browser, '/approvals', baseURL!);
    expect(probe.status, '/approvals must redirect an anonymous caller').toBeGreaterThanOrEqual(300);
    expect(probe.status, 'with a 3xx rather than an answer').toBeLessThan(400);
    expect(probe.location, 'and the destination is /login').toContain('/login');
  });

  test('TC-C-002 — a Requester holding no approval verb is refused the queue', async () => {
    const probe = await probeGet(requester.page, '/approvals');
    expect(probe.status, '/approvals is gated on approval:approve, which Requester does not hold').toBe(403);
  });

  test('TC-C-003 — an Accounts user is refused the queue', async () => {
    const probe = await probeGet(accounts.page, '/approvals');
    expect(probe.status, 'Accounts holds every payment verb and no approval verb').toBe(403);
  });

  test('TC-C-004 — a user with no role at all is refused the queue', async () => {
    const probe = await probeGet(nobody.page, '/approvals');
    expect(probe.status, 'a signed-in user with no grant may reach no gated screen').toBe(403);
  });

  test('TC-C-005 — the queue is scoped to the manager it was assigned to, not to request=all', async () => {
    const mine = await pendingRequest(mgrA.id);
    const theirs = await pendingRequest(mgrB.id);

    expect(await queueCards(mgrA, 'to-approve', mine.title), "a manager's queue holds the request sent to them").toBe(
      1
    );
    expect(
      await queueCards(mgrA, 'to-approve', theirs.title),
      'and never another manager\'s, even though Manager carries request scope "all"'
    ).toBe(0);

    // The scope really is "all" — the row is readable, it is just not theirs to
    // decide. Proving both halves is what makes the filter a design choice
    // rather than a missing grant (internal/app/requests.go:896).
    const readable = await probeGet(mgrA.page, `/requests/${theirs.id}`);
    expect(readable.status, "request scope all lets a manager READ another manager's request").toBe(200);
  });

  test('TC-C-006 — the dashboard count, the tab count and the rows in the queue agree', async () => {
    await pendingRequest(mgrA.id);
    await pendingRequest(mgrA.id);

    await mgrA.page.goto('/');
    const metric = Number(
      await mgrA.page.locator('a.metric:has(.metric-label:text-is("Awaiting my approval")) .metric-value').innerText()
    );

    await mgrA.page.goto('/approvals');
    const tab = Number(await mgrA.page.locator('.segmented a', { hasText: 'To approve' }).locator('.n').innerText());
    const rows = await mgrA.page.locator('.req-card').count();

    expect(metric, 'the dashboard metric and the queue tab are the same COUNT query').toBe(tab);
    expect(rows, 'and the rows the queue shows are the rows it counted').toBe(tab);
    expect(metric, 'the two requests just raised are at least in there').toBeGreaterThanOrEqual(2);
  });

  test('TC-C-007 — a request moves between the three tabs as its status changes', async () => {
    const req = await approvedRequest();
    expect(await queueCards(mgrA, 'to-approve', req.title), 'an approved request is no longer to approve').toBe(0);
    expect(await queueCards(mgrA, 'decided', req.title), 'it is decided').toBe(1);

    succeeded(await askCancel(requester, req.id, 'Order withdrawn.'), 'asking for cancellation');
    expect(await queueCards(mgrA, 'cancellations', req.title), 'a frozen request is a cancellation to decide').toBe(1);
    expect(await queueCards(mgrA, 'decided', req.title), 'and is no longer merely decided').toBe(0);

    succeeded(await decideCancel(mgrA, req.id, 'accept', ''), 'accepting the cancellation');
    expect(await queueCards(mgrA, 'cancellations', req.title), 'a cancelled request needs no decision').toBe(0);
    expect(await queueCards(mgrA, 'decided', req.title), 'and reads as decided').toBe(1);
  });

  test('TC-C-008 — an unrecognised bucket falls back to "To approve" rather than emptying the queue', async () => {
    await mgrA.page.goto('/approvals?bucket=not-a-tab');
    const current = await mgrA.page.locator('.segmented a[aria-current="page"]').innerText();
    expect(current, 'an unknown ?bucket= resolves to the first tab').toContain('To approve');
  });
});

// --------------------------------------------------------------------------
// 2. Approve, and approve with an adjusted amount.
// --------------------------------------------------------------------------

test.describe('approving', () => {
  test('TC-C-010 — the assigned manager approves the full amount through the sheet', async () => {
    const req = await pendingRequest(mgrA.id, '18400');
    await mgrA.page.goto(`/requests/${req.id}`);
    await expect(mgrA.page.locator('.waiting.you'), 'the request says it is waiting on this reader').toBeVisible();

    await mgrA.page.getByRole('button', { name: /^Approve / }).click();
    const sheet = mgrA.page.locator('#approve-sheet');
    await expect(sheet).toBeVisible();
    await expect(sheet.getByLabel('Amount approved'), 'the sheet opens on the amount that was asked for').toHaveValue(
      '18,400.00'
    );
    await sheet.getByRole('button', { name: 'Approve request' }).click();
    await expect(mgrA.page, 'approving returns the manager to their queue').toHaveURL(/\/approvals$/);

    expect(await statusPill(mgrA, req.id), 'the request is approved').toBe('Approved — awaiting payment');
    await mgrA.page.goto(`/requests/${req.id}`);
    await expect(dlValue(mgrA.page, 'Approved'), 'the approved amount is on the detail screen').toHaveText(
      '₹18,400.00'
    );
  });

  test('TC-C-011 — an approver may approve less than was asked for', async () => {
    const req = await pendingRequest(mgrA.id, '18400');
    await mgrA.page.goto(`/requests/${req.id}`);
    await mgrA.page.getByRole('button', { name: /^Approve / }).click();
    const sheet = mgrA.page.locator('#approve-sheet');
    await sheet.getByLabel('Amount approved').fill('12000');
    await sheet.getByLabel(/^Note/).fill('Cut the cab fare.');
    await sheet.getByRole('button', { name: 'Approve request' }).click();
    await expect(mgrA.page).toHaveURL(/\/approvals$/);

    await requester.page.goto(`/requests/${req.id}`);
    await expect(dlValue(requester.page, 'Amount'), 'the amount asked for is still on the record').toHaveText(
      '₹18,400.00'
    );
    await expect(dlValue(requester.page, 'Approved'), 'and the reduced amount is what was approved').toHaveText(
      '₹12,000.00'
    );
  });

  test('TC-C-012 — the approval is audited against entity_type payment_request', async () => {
    const req = await approvedRequest('18400', '12000');
    const trail = await auditTrail(req.id);

    expect(trail, 'the audit row is filed under the request entity').toContain('Payment Request');
    expect(trail, 'the action is recorded verbatim').toContain('approve');
    expect(trail, 'the summary names the approver and the approved amount').toContain(
      `${mgrA.subject.name} approved request`
    );
    expect(trail, 'and the amount is the approved one').toContain('₹12,000.00');
    expect(trail, 'approved_by is written').toMatch(/"ApprovedBy":\s*\d+/);
    expect(trail, 'approved_at is written').toMatch(/"ApprovedAt":\s*"\d{4}-/);
    expect(trail, 'and approved_amount is written').toMatch(/"ApprovedAmount":\s*1200000/);
  });

  test('TC-C-013 — the requester reads the outcome on their own request', async () => {
    const req = await approvedRequest('18400', '12000');
    await requester.page.goto(`/requests/${req.id}`);

    await expect(requester.page.locator('.rh-status .pill'), 'the status is spelled for a person').toHaveText(
      'Approved — awaiting payment'
    );
    await expect(requester.page.locator('.banner.locked'), 'an approved request is locked to its requester')
      .toContainText('Approved requests are locked');
    await expect(
      requester.page.getByRole('link', { name: 'Edit request' }),
      'and the edit control is gone'
    ).toHaveCount(0);
    expect(await thread(requester, req.id), 'the decision is on the shared history').toContain('approved request');
  });

  test('TC-C-014 — an adjusted amount GREATER than requested is refused (finding F-C-01)', async () => {
    // REWRITTEN for the F-C-01 fix. As written for the audit this case asserted
    // the defect — that an over-approval commits, because `ApproveRequest`
    // checked only `approvedAmount <= 0` and there was no ceiling anywhere. The
    // approved amount is not a note: `approvedOf()` makes it the ceiling Accounts
    // may pay to (G13), so approving above the request raised that ceiling above
    // what anybody asked for, on one person's signature, and the audit trail
    // could not tell it from an ordinary approval. `ApproveRequest` now refuses
    // it. Downwards remains the adjustment the sheet offers ("You may approve a
    // smaller amount than was asked for") and TC-C-011 pins that.
    //
    // What this case now protects: the ceiling, and that a refused approval is a
    // no-op — the request is still pending, still asks for what it asked for,
    // and carries no approved figure at all.
    const req = await pendingRequest(mgrA.id, '18400');
    const probe = await approve(mgrA, req.id, '25000');
    refused(probe, 'approving ₹25,000 against a ₹18,400 request');
    expect(probe.status, 'an over-approval is a validation refusal, not an authorisation one').toBe(400);
    expect(probe.body, 'and the approver is told the rule and what to do instead').toContain(
      'you cannot approve more than'
    );
    expect(probe.body, 'naming the figure that was actually requested').toContain('₹18,400.00');

    expect(await statusPill(mgrA, req.id), 'the request is untouched — still awaiting a legal decision').toBe(
      'Awaiting approval'
    );
    await requester.page.goto(`/requests/${req.id}`);
    await expect(dlValue(requester.page, 'Amount'), 'it still asks for ₹18,400.00').toHaveText('₹18,400.00');
    await expect(
      dlValue(requester.page, 'Approved'),
      'and no approved amount was recorded: the refusal wrote nothing'
    ).toHaveCount(0);
  });

  test('TC-C-014B — the ceiling holds end to end: Accounts cannot pay above what was approved', async () => {
    // REWRITTEN for the F-C-01 fix. As written this case drove the consequence
    // half of the defect — ₹25,000 approved and paid against a ₹18,400 request,
    // ending Completed. That path is unreachable now that the over-approval is
    // refused, so the case drives the ceiling itself instead, end to end rather
    // than argued from the code: an approver may approve at or below the
    // requested amount, and G13 caps the payment at that approved figure
    // (`approvedOf`, internal/store/store.go:900–910) — not at the requested one.
    const req = await pendingRequest(mgrA.id, '18400');
    succeeded(await approve(mgrA, req.id, '12000'), 'approving below the request is the adjustment on offer');

    // Reserved first, the way the queue's own button does it, so the probe below
    // is refused on the ceiling and not on the reservation guard.
    succeeded(await probePost(accounts.page, `/requests/${req.id}/record-payment`, {}), 'reserving it for payment');

    // 2026-06-15, not 2029-06-15. F-D-06 is enforced through the UI now:
    // `validatePayment` (internal/store/store.go:1936-1943) refuses a `paid_on`
    // after today, because paid_on records when money left the bank and a date
    // in the future records something that has not happened. The date keeps its
    // own month, so nothing in this file settles twice into one month.
    const settleBody = (amount: string) => ({
      request_id: String(req.id),
      amount,
      paid_on: '2026-06-15',
      payment_mode: 'bank_transfer',
      reference_no: `UTR-C014B-${req.id}`,
      settlement: 'settled'
    });

    // The requested amount is NOT the ceiling; the approved amount is. Paying the
    // ₹18,400 that was asked for must be refused once ₹12,000 was approved.
    const over = await probePost(accounts.page, '/payments', settleBody('18400'));
    expect(
      over.status,
      `paying ₹18,400 against a ₹12,000 approval must be refused; got ${over.outcome} ${over.body.slice(0, 200)}`
    ).toBe(400);
    expect(over.body, 'and the accountant is told the approved amount is the ceiling').toContain(
      'is more than the approved'
    );
    expect(over.body, 'naming the ceiling itself').toContain('₹12,000.00');
    // toContain: since settlement-7 the pill names the holder — "With Accounts — taken by …".
    expect(await statusPill(mgrA, req.id), 'nothing was paid: the request is still reserved, not completed').toContain(
      'With Accounts'
    );

    // At the approved figure the same POST goes through, and the outcome panel
    // reconciles against the approved amount rather than the requested one.
    succeeded(await probePost(accounts.page, '/payments', settleBody('12000')), 'paying the approved amount');
    expect(await statusPill(mgrA, req.id), 'a payment at the approved amount completes the request').toBe(
      'Completed'
    );
    await requester.page.goto(`/requests/${req.id}`);
    const outcome = requester.page.locator('.compare');
    await expect(outcome, 'the outcome panel compares against the approved figure').toContainText('₹12,000.00');
    await expect(outcome, 'with the difference confirmed settled').toContainText('confirmed settled');
    await expect(
      outcome,
      'and the requested figure is nowhere in it — ₹18,400 was never authority to pay anything'
    ).not.toContainText('₹18,400.00');
    await expect(dlValue(requester.page, 'Amount'), 'though the request still records what was asked for').toHaveText(
      '₹18,400.00'
    );
  });

  const badAmounts: Array<{ id: string; label: string; form: Record<string, string> }> = [
    { id: 'TC-C-015', label: 'zero', form: { approved_amount: '0' } },
    { id: 'TC-C-016', label: 'negative', form: { approved_amount: '-500' } },
    { id: 'TC-C-017', label: 'non-numeric', form: { approved_amount: 'twelve thousand' } },
    { id: 'TC-C-018', label: 'absent', form: {} },
    { id: 'TC-C-019', label: 'whitespace-only', form: { approved_amount: '   ' } }
  ];
  for (const bad of badAmounts) {
    test(`${bad.id} — an approved amount that is ${bad.label} is refused`, async () => {
      const req = await pendingRequest(mgrA.id, '18400');
      const probe = await probePost(mgrA.page, `/requests/${req.id}/approve`, { note: '', ...bad.form });
      expect(probe.status, `a ${bad.label} approved amount must be refused`).toBe(400);
      expect(probe.body, 'and the reader is told what to do about it').toContain('Enter the amount you are approving');
      expect(await statusPill(mgrA, req.id), 'the request is untouched').toBe('Awaiting approval');
    });
  }

  test('TC-C-020 — a grouped Indian amount is understood', async () => {
    const req = await pendingRequest(mgrA.id, '200000');
    succeeded(await approve(mgrA, req.id, '1,00,000'), 'the money field writes grouped digits back');
    await requester.page.goto(`/requests/${req.id}`);
    await expect(dlValue(requester.page, 'Approved'), '1,00,000 is one lakh, not one hundred').toHaveText(
      '₹1,00,000.00'
    );
  });

  test('TC-C-021 — one paise is a legal approved amount and zero is not', async () => {
    const req = await pendingRequest(mgrA.id, '18400');
    succeeded(await approve(mgrA, req.id, '0.01'), 'the guard is > 0, so the smallest positive amount passes');
    await requester.page.goto(`/requests/${req.id}`);
    await expect(dlValue(requester.page, 'Approved'), 'and it is rendered as paise, not rounded away').toHaveText(
      '₹0.01'
    );
  });

  test('TC-C-022 — an approval with a missing or forged CSRF token is refused', async () => {
    const req = await pendingRequest(mgrA.id);
    const omitted = await probePost(mgrA.page, `/requests/${req.id}/approve`, { approved_amount: '18400' }, {
      csrf: 'omit'
    });
    expect(omitted.status, 'a POST with no csrf field must be refused').toBe(403);
    const forged = await probePost(mgrA.page, `/requests/${req.id}/approve`, { approved_amount: '18400' }, {
      csrf: 'bogus'
    });
    expect(forged.status, 'a POST with a wrong csrf field must be refused').toBe(403);
    expect(await statusPill(mgrA, req.id), 'and neither wrote anything').toBe('Awaiting approval');
  });
});

// --------------------------------------------------------------------------
// 3. Who may decide. G8, and "an admin holds every grant but is not that person".
// --------------------------------------------------------------------------

test.describe('who may decide', () => {
  test('TC-C-030 — a person who may approve never finds their own name in the approver list (G8)', async () => {
    const form = await probeGet(dual.page, '/requests/new?type=reimbursement');
    expect(form.status, 'a Requester+Manager may raise a request').toBe(200);
    expect(
      form.body.includes(`>${dual.subject.name}</option>`),
      'ListApprovers excludes the requester, so self-approval is not offerable'
    ).toBe(false);
    expect(form.body, 'while other approvers are offered').toContain(`>${mgrA.subject.name}</option>`);
  });

  test('TC-C-031 — a hand-rolled POST naming yourself as approver is refused (G8)', async () => {
    const probe = await probePost(dual.page, '/requests', requestBody(`self ${runId}`, dual.id));
    expect(probe.status, 'the store refuses what the form never offered').toBe(400);
    expect(probe.body, 'and says why').toContain('you cannot approve your own request');
  });

  test('TC-C-032 — G8 binds an administrator holding all 66 grants too', async () => {
    const probe = await probePost(boss, '/requests', requestBody(`selfadmin ${runId}`, adminId));
    expect(probe.status, 'self-approval is a rule about people, not about permissions').toBe(400);
    expect(probe.body, 'and the administrator gets the same sentence').toContain('you cannot approve your own request');
  });

  test('TC-C-033 — an administrator cannot approve a request assigned to somebody else', async () => {
    const req = await pendingRequest(mgrA.id);
    const probe = await approve(bossActor, req.id, '18400');
    expect(probe.status, "holding approval:approve is not being this request's approver").toBe(403);
    expect(await statusPill(mgrA, req.id), 'the request is untouched').toBe('Awaiting approval');
  });

  test('TC-C-034 — a Manager cannot approve a request assigned to a different manager', async () => {
    const req = await pendingRequest(mgrA.id);
    const probe = await approve(mgrB, req.id, '18400');
    expect(probe.status, 'manager_id on the row is what decides, not the role').toBe(403);
    expect(await statusPill(mgrA, req.id), 'the request is untouched').toBe('Awaiting approval');
  });

  test('TC-C-035 — a Manager cannot return or reject another manager\'s request', async () => {
    const req = await pendingRequest(mgrA.id);
    expect((await sendBack(mgrB, req.id, 'Fix it.')).status, 'returning belongs to the assigned approver').toBe(403);
    expect((await reject(mgrB, req.id, 'No.')).status, 'so does rejecting').toBe(403);
    expect(await statusPill(mgrA, req.id), 'the request is untouched').toBe('Awaiting approval');
  });

  test('TC-C-036 — an Accounts user holding no approval:approve is refused at the route', async () => {
    const req = await pendingRequest(mgrA.id);
    expect((await approve(accounts, req.id, '18400')).status, 'approve is gated on approval:approve').toBe(403);
    expect((await sendBack(accounts, req.id, 'Fix it.')).status, 'return is gated on approval:return').toBe(403);
    expect((await reject(accounts, req.id, 'No.')).status, 'reject is gated on approval:reject').toBe(403);
  });

  test('TC-C-037 — the requester cannot approve their own request over HTTP either', async () => {
    const req = await pendingRequest(mgrA.id);
    expect((await approve(requester, req.id, '18400')).status, 'Requester holds no approval verb').toBe(403);
    expect((await approve(nobody, req.id, '18400')).status, 'and neither does a user with no role').toBe(403);
  });

  test('TC-C-038 — the wrong manager is offered no decision controls on the screen', async () => {
    const req = await pendingRequest(mgrA.id);
    await mgrB.page.goto(`/requests/${req.id}`);
    await expect(mgrB.page.locator('[data-open="approve-sheet"]'), 'no approve control').toHaveCount(0);
    await expect(mgrB.page.locator('[data-open="return-sheet"]'), 'no return control').toHaveCount(0);
    await expect(mgrB.page.locator('[data-open="reject-sheet"]'), 'no reject control').toHaveCount(0);
    await expect(mgrB.page.locator('.waiting.you'), 'and nothing claims it is waiting on them').toHaveCount(0);
    await expect(mgrB.page.locator('.waiting'), 'the line names the approver it really waits on').toContainText(
      mgrA.subject.name
    );
  });
});

// --------------------------------------------------------------------------
// 4. Return and reject.
// --------------------------------------------------------------------------

test.describe('returning and rejecting', () => {
  test('TC-C-040 — returning with no comment is refused', async () => {
    const req = await pendingRequest();
    const probe = await sendBack(mgrA, req.id, '');
    expect(probe.status, 'a return with nothing in it tells the requester nothing').toBe(400);
    expect(probe.body, 'and says so').toContain('a comment is required to return a request');
    expect(await statusPill(mgrA, req.id), 'the request is untouched').toBe('Awaiting approval');
  });

  test('TC-C-041 — returning with a whitespace-only comment is refused', async () => {
    const req = await pendingRequest();
    const probe = await sendBack(mgrA, req.id, '   \t  ');
    expect(probe.status, 'spaces are not a reason').toBe(400);
    expect(probe.body, 'and the same sentence is shown').toContain('a comment is required to return a request');
  });

  test('TC-C-042 — a returned request goes back to the requester and becomes editable', async () => {
    const req = await pendingRequest();
    await mgrA.page.goto(`/requests/${req.id}`);
    await mgrA.page.getByRole('button', { name: 'Return for correction' }).click();
    const sheet = mgrA.page.locator('#return-sheet');
    await expect(sheet).toBeVisible();
    await sheet.getByLabel('What needs correcting').fill('Attach the cab receipts.');
    await sheet.getByRole('button', { name: 'Return request' }).click();
    await expect(mgrA.page).toHaveURL(/\/approvals$/);

    await requester.page.goto(`/requests/${req.id}`);
    await expect(requester.page.locator('.rh-status .pill.returned'), 'the requester sees it came back').toBeVisible();
    await expect(requester.page.locator('.banner.warn'), 'with the approver\'s words, verbatim').toContainText(
      'Attach the cab receipts.'
    );
    await expect(
      requester.page.getByRole('button', { name: 'Resubmit for approval' }),
      'and the same URL is now a correction form'
    ).toBeVisible();
  });

  test('TC-C-043 — a returned request leaves the queue and comes back on resubmission, keeping its number', async () => {
    const req = await returnedRequest();
    expect(await queueCards(mgrA, 'to-approve', req.title), 'nothing is waiting on the approver').toBe(0);

    const before = await requestNumber(requester, req.id);
    succeeded(await resubmit(requester, req.id, req.title, mgrA.id), 'correcting and resubmitting is one press');

    expect(await queueCards(mgrA, 'to-approve', req.title), 'and it is back in the queue').toBe(1);
    expect(await statusPill(mgrA, req.id), 'as a pending request again').toBe('Awaiting approval');
    expect(await requestNumber(requester, req.id), 'a return is not a new request — the number survives').toBe(before);
  });

  test('TC-C-044 — the return reason reaches the audit trail as well as the screen', async () => {
    const req = await pendingRequest();
    succeeded(await sendBack(mgrA, req.id, 'The invoice date is wrong.'), 'returning');
    expect(await thread(requester, req.id), 'the reason is on the shared history').toContain(
      'The invoice date is wrong.'
    );
    const trail = await auditTrail(req.id);
    expect(trail, 'the audit action is "return"').toContain('return');
    expect(trail, 'and the reason is in the summary').toContain('The invoice date is wrong.');
  });

  test('TC-C-045 — rejecting with no reason is refused', async () => {
    const req = await pendingRequest();
    const probe = await reject(mgrA, req.id, '');
    expect(probe.status, 'a rejection is final, so it must be explained').toBe(400);
    expect(probe.body, 'and says so').toContain('a reason is required to reject a request');
    expect(await statusPill(mgrA, req.id), 'the request is untouched').toBe('Awaiting approval');
  });

  test('TC-C-046 — rejecting with a whitespace-only reason is refused', async () => {
    const req = await pendingRequest();
    const probe = await reject(mgrA, req.id, '\n \t ');
    expect(probe.status, 'spaces are not a reason').toBe(400);
    expect(probe.body, 'and the same sentence is shown').toContain('a reason is required to reject a request');
  });

  test('TC-C-047 — a rejected request is final, and says so with the reason', async () => {
    const req = await pendingRequest();
    await mgrA.page.goto(`/requests/${req.id}`);
    // exact, or the substring also matches "Reject permanently" in the sheet.
    await mgrA.page.getByRole('button', { name: 'Reject', exact: true }).click();
    const sheet = mgrA.page.locator('#reject-sheet');
    await expect(sheet).toBeVisible();
    await sheet.getByLabel('Reason for rejection').fill('Outside this year’s budget.');
    await sheet.getByRole('button', { name: 'Reject permanently' }).click();
    await expect(mgrA.page).toHaveURL(/\/approvals$/);

    await requester.page.goto(`/requests/${req.id}`);
    await expect(requester.page.locator('.rh-status .pill'), 'the pill says final').toHaveText('Rejected — final');
    await expect(requester.page.locator('.banner.bad'), 'and names who rejected it and why').toContainText(
      'Outside this year’s budget.'
    );
    await expect(requester.page.locator('.banner.bad')).toContainText(mgrA.subject.name);
  });

  test('TC-C-048 — a rejected request is read-only for its requester', async () => {
    const req = await rejectedRequest();
    await requester.page.goto(`/requests/${req.id}`);
    await expect(requester.page.getByRole('link', { name: 'Edit request' }), 'no edit link').toHaveCount(0);
    await expect(requester.page.locator('form[action$="/withdraw"]'), 'no withdraw control').toHaveCount(0);
    await expect(requester.page.getByRole('link', { name: 'Request cancellation' }), 'no cancel link').toHaveCount(0);

    const edit = await probeGet(requester.page, `/requests/${req.id}/edit`);
    expect(edit.status, 'and the URL says the same thing').toBe(400);
    expect(edit.body, 'in words a person can act on').toContain('A rejected request is final');
    refused(await resubmit(requester, req.id, req.title, mgrA.id), 'editing a rejected request');
    refused(await withdraw(requester, req.id), 'withdrawing a rejected request');
    refused(await askCancel(requester, req.id, 'Please cancel.'), 'asking to cancel a rejected request');
  });

  test('TC-C-049 — a rejected request refuses every approver decision too', async () => {
    const req = await rejectedRequest();
    refused(await approve(mgrA, req.id, '18400'), 'approving a rejected request');
    refused(await sendBack(mgrA, req.id, 'Fix it.'), 'returning a rejected request');
    refused(await reject(mgrA, req.id, 'Again.'), 'rejecting a rejected request twice');
    refused(await cancelOutright(mgrA, req.id, 'Cancel it.'), 'cancelling a rejected request');
    refused(await decideCancel(mgrA, req.id, 'accept', ''), 'deciding a cancellation nobody asked for');
    expect(await statusPill(mgrA, req.id), 'and it is still rejected').toBe('Rejected — final');
  });

  test('TC-C-050 — the rejection reason is in the audit trail under the reject action', async () => {
    const req = await pendingRequest();
    succeeded(await reject(mgrA, req.id, 'Vendor is blacklisted.'), 'rejecting');
    const trail = await auditTrail(req.id);
    expect(trail, 'the action is recorded').toContain('reject');
    expect(trail, 'with the reason').toContain('Vendor is blacklisted.');
    expect(trail, 'against the request entity').toContain('Payment Request');
  });

  test('TC-C-051 — re-raising a rejected request creates a new one and leaves the original rejected', async () => {
    const req = await rejectedRequest();
    const oldNumber = await requestNumber(requester, req.id);
    const probe = await reraise(requester, req.id);
    succeeded(probe, 're-raising a rejected request');
    const newId = Number(probe.location!.split('/')[2]);
    expect(newId, 'a re-raise is a new row, not a resurrection').not.toBe(req.id);
    expect(await statusPill(requester, newId), 'the copy is already pending').toBe('Awaiting approval');
    expect(await requestNumber(requester, newId), 'with a number of its own').not.toBe(oldNumber);
    expect(await statusPill(requester, req.id), 'and the original stays rejected').toBe('Rejected — final');
  });

  test('TC-C-052 — a withdrawn request is terminal for both actors', async () => {
    const req = await withdrawnRequest();
    expect(await statusPill(requester, req.id), 'withdrawing is the requester\'s own exit').toBe('Withdrawn');
    refused(await approve(mgrA, req.id, '18400'), 'approving a withdrawn request');
    refused(await sendBack(mgrA, req.id, 'Fix it.'), 'returning a withdrawn request');
    refused(await reject(mgrA, req.id, 'No.'), 'rejecting a withdrawn request');
    refused(await reraise(requester, req.id), 're-raising a withdrawn request');
    expect(await statusPill(requester, req.id), 'and it is still withdrawn').toBe('Withdrawn');
  });
});

// --------------------------------------------------------------------------
// 5. Reassignment. The approval reassignment now has a route (F-A-06/F-C-02),
//    and the reservation reassignment that is easy to mistake for it does not
//    reach it.
// --------------------------------------------------------------------------

test.describe('reassignment', () => {
  test('TC-C-060 — the reservation reassign route is not the approval one, and a Manager is refused it', async () => {
    // REWRITTEN: this case was titled "a Manager holding approval:reassign has no
    // route to use it (finding F-C-02)" and its comment said routes() registered
    // no handler that called store.ReassignRequest, so the grant was dead. It is
    // not dead any more — POST /requests/{id}/reassign-approver is that handler,
    // and TC-C-060B drives it. Every assertion below is unchanged and still worth
    // making: /requests/{id}/reassign is the RESERVATION reassignment, gated on
    // reservation:reassign, which Admin alone holds, so a Manager reaching for the
    // obvious name is refused and moves nothing.
    const req = await pendingRequest(mgrA.id);
    const probe = await probePost(mgrA.page, `/requests/${req.id}/reassign`, {
      to_user_id: mgrB.id,
      reason: 'On leave for a fortnight.',
      confirm: 'on'
    });
    expect(probe.status, 'the reservation reassign route is gated on reservation:reassign, which Manager lacks').toBe(
      403
    );

    await requester.page.goto(`/requests/${req.id}`);
    await expect(dlValue(requester.page, 'Approver'), 'and the approver on the row is unchanged').toHaveText(
      mgrA.subject.name
    );
  });

  test('TC-C-060B — the approval reassignment has exactly one URL, and it moves the approver', async () => {
    // REWRITTEN for the F-A-06/F-C-02 fix. This was a proof-of-absence case: it
    // fired four plausible names at the server and required all four to be
    // unrouted, because `approval:reassign` was a grant with nothing behind it.
    // One of the four is now the real route, so requiring its absence asserts the
    // defect. What the case protects instead is the property that made it worth
    // writing — the reassignment has ONE door, not several — plus coverage
    // requirement A7, which nothing else drives end to end.
    //
    // An unrouted POST answers 405, not 404 (CV1).
    const req = await pendingRequest(mgrA.id);
    for (const path of [
      `/requests/${req.id}/approval-reassign`,
      `/requests/${req.id}/change-approver`,
      `/approvals/${req.id}/reassign`
    ]) {
      for (const actor of [mgrA, bossActor]) {
        const probe = await probePost(actor.page, path, { manager_id: mgrB.id, reason: 'Hand it on.' });
        expect([404, 405], `POST ${path} must not exist — got ${probe.outcome}`).toContain(probe.status);
      }
    }
    await requester.page.goto(`/requests/${req.id}`);
    await expect(
      dlValue(requester.page, 'Approver'),
      'and none of them moved the approver'
    ).toHaveText(mgrA.subject.name);

    // The one that does exist, driven by the approver handing their own request
    // on. store.ReassignRequest carries the rules — pending only, a real approver
    // at the other end, G8, a mandatory reason — and this is its only HTTP caller.
    succeeded(
      await probePost(mgrA.page, `/requests/${req.id}/reassign-approver`, {
        manager_id: mgrB.id,
        reason: 'On leave for a fortnight.'
      }),
      'the approval reassignment route'
    );
    await requester.page.goto(`/requests/${req.id}`);
    await expect(dlValue(requester.page, 'Approver'), 'the request is now the other manager\'s to decide').toHaveText(
      mgrB.subject.name
    );
    expect(await queueCards(mgrB, 'to-approve', req.title), "and it is in that manager's queue").toBe(1);
    expect(await queueCards(mgrA, 'to-approve', req.title), 'and out of the one it came from').toBe(0);

    // Its own audit action, so an approver swap never reads as somebody taking
    // over the payment. `actionText` renders it for a person, which is what the
    // screen shows and therefore what this matches.
    expect(await auditTrail(req.id), 'the swap is audited as its own event').toContain('Approval reassigned');
    expect(await auditTrail(req.id), 'with the reason that was given').toContain('On leave for a fortnight.');
  });

  test('TC-C-061 — the reservation reassign route cannot move an approver either', async () => {
    const req = await pendingRequest(mgrA.id);
    // The administrator DOES hold reservation:reassign, so these get past the
    // route gate and into the handler — which is about reservations, not
    // approvals. Every branch of it refuses, and none of them is about approving.
    const noTarget = await probePost(boss, `/requests/${req.id}/reassign`, { reason: 'Hand it over.', confirm: 'on' });
    refused(noTarget, 'reassigning with no target');
    expect(noTarget.body, 'the handler asks about a reservation, not an approval').toContain(
      'Choose who should take this reservation'
    );

    const noConfirm = await probePost(boss, `/requests/${req.id}/reassign`, {
      to_user_id: mgrB.id,
      reason: 'Hand it over.'
    });
    refused(noConfirm, 'reassigning without confirming');
    expect(noConfirm.body, 'it asks about payments already initiated').toContain(
      'Confirm that no payment has been initiated'
    );

    const wrongTarget = await probePost(boss, `/requests/${req.id}/reassign`, {
      to_user_id: mgrB.id,
      reason: 'Hand it over.',
      confirm: 'on'
    });
    refused(wrongTarget, 'reassigning an approval through the reservation route');
    expect(wrongTarget.body, 'and a manager is not somebody who can work the Accounts queue').toContain(
      'cannot work the Accounts queue'
    );

    await requester.page.goto(`/requests/${req.id}`);
    await expect(dlValue(requester.page, 'Approver'), 'the approver is still the one who was chosen').toHaveText(
      mgrA.subject.name
    );
  });

  test('TC-C-062 — no screen offers an approver a way to hand the decision on', async () => {
    const req = await pendingRequest(mgrA.id);
    await mgrA.page.goto(`/requests/${req.id}`);
    const bar = mgrA.page.locator('.action-bar');
    await expect(bar, 'the approver bar offers approve, return and reject and nothing else').toContainText('Approve');
    await expect(bar).toContainText('Return for correction');
    await expect(bar).toContainText('Reject');
    await expect(
      mgrA.page.locator('.action-bar a[href$="/reassign"], .action-bar form[action$="/reassign"]'),
      'and no reassignment control at all'
    ).toHaveCount(0);
    await expect(mgrA.page.locator('form[action$="/reassign"]'), 'nowhere on the page').toHaveCount(0);
  });

  test('TC-C-063 — the only rerouting the product ships is the requester\'s own edit', async () => {
    // Documented here so the absence is deliberate rather than forgotten: the
    // approver cannot hand a decision on, but the requester can move it, and
    // TC-C-120..122 prove that path end to end.
    const req = await pendingRequest(mgrA.id);
    succeeded(await editRequest(requester, req.id, req.title, mgrB.id), 'the requester reroutes by editing');
    await requester.page.goto(`/requests/${req.id}`);
    await expect(dlValue(requester.page, 'Approver'), 'and the approver on the row really moved').toHaveText(
      mgrB.subject.name
    );
  });
});

// --------------------------------------------------------------------------
// 6. No bulk approval (A6).
// --------------------------------------------------------------------------

test.describe('no bulk approval', () => {
  test('TC-C-070 — there is no bulk-approve route', async () => {
    // An unrouted POST answers 405, not 404: routes() registers a catch-all
    // GET /, so the path matches and the method does not.
    for (const path of [
      '/approvals/bulk-approve',
      '/approvals/approve-all',
      '/requests/bulk-approve',
      '/requests/approve',
      '/approvals'
    ]) {
      const probe = await probePost(mgrA.page, path, { ids: '1,2,3' });
      expect([404, 405], `POST ${path} must not exist — got ${probe.outcome}`).toContain(probe.status);
    }
  });

  test('TC-C-071 — the queue offers no selection and says why', async () => {
    await pendingRequest(mgrA.id);
    await mgrA.page.goto('/approvals');
    const body = mgrA.page.locator('main.page');
    await expect(body.locator('input[type="checkbox"]'), 'no row selection').toHaveCount(0);
    await expect(
      body.getByRole('button', { name: /approve/i }),
      'no approve control on the list at all — a decision is made after opening the request'
    ).toHaveCount(0);
    await expect(body, 'and the screen states the design decision').toContainText(
      'There is no bulk approval, by design'
    );
  });
});

// --------------------------------------------------------------------------
// 7. The cancellation flow: five routes, two actors.
// --------------------------------------------------------------------------

test.describe('the cancellation flow', () => {
  const requesterRoutes = (id: number) => [
    { method: 'GET', path: `/requests/${id}/cancel` },
    { method: 'POST', path: `/requests/${id}/cancel-request` }
  ];
  const approverRoutes = (id: number) => [
    { method: 'POST', path: `/requests/${id}/cancel` },
    { method: 'GET', path: `/requests/${id}/cancellation` },
    { method: 'POST', path: `/requests/${id}/cancellation` }
  ];

  async function fire(actor: Actor, route: { method: string; path: string }): Promise<Probe> {
    return route.method === 'GET'
      ? probeGet(actor.page, route.path)
      : probePost(actor.page, route.path, { reason: 'x', note: 'x', decision: 'decline' });
  }

  test('TC-C-080 — the requester\'s cancel routes are refused to the approver', async () => {
    const req = await approvedRequest();
    for (const route of requesterRoutes(req.id)) {
      const probe = await fire(mgrA, route);
      expect(probe.status, `${route.method} ${route.path} is gated on request:cancel, which Manager lacks`).toBe(403);
    }
  });

  test('TC-C-081 — the approver\'s cancellation routes are refused to the requester', async () => {
    const req = await approvedRequest();
    for (const route of approverRoutes(req.id)) {
      const probe = await fire(requester, route);
      expect(probe.status, `${route.method} ${route.path} is gated on approval:cancel, which Requester lacks`).toBe(
        403
      );
    }
  });

  test('TC-C-082 — an Accounts user is refused all five cancellation routes', async () => {
    const req = await approvedRequest();
    for (const route of [...requesterRoutes(req.id), ...approverRoutes(req.id)]) {
      const probe = await fire(accounts, route);
      expect(probe.status, `Accounts holds neither cancel verb — ${route.method} ${route.path}`).toBe(403);
    }
  });

  test('TC-C-083 — a user with no role is refused all five cancellation routes', async () => {
    const req = await approvedRequest();
    for (const route of [...requesterRoutes(req.id), ...approverRoutes(req.id)]) {
      const probe = await fire(nobody, route);
      expect(probe.status, `no grant, no route — ${route.method} ${route.path}`).toBe(403);
    }
  });

  test('TC-C-084 — an anonymous caller is redirected off all five', async ({ browser, baseURL }) => {
    const req = await approvedRequest();
    for (const route of [...requesterRoutes(req.id), ...approverRoutes(req.id)]) {
      const probe =
        route.method === 'GET'
          ? await probeAnonymous(browser, route.path, baseURL!)
          : await anonymousPost(browser, route.path, baseURL!);
      expect(probe.status, `${route.method} ${route.path} must redirect, not answer`).toBeGreaterThanOrEqual(300);
      expect(probe.status, 'with a 3xx').toBeLessThan(400);
      expect(probe.location, 'to /login').toContain('/login');
    }
  });

  // REWRITTEN for the F-G-002 fix. The GET used to be required to answer 403 and
  // now answers 404, and the difference is the fix rather than a slip: a
  // Requester's data scope really is "own", so the row is not readable — and
  // `loadViewableRequest` (internal/app/requests.go:325-348), which
  // `requestCancelForm` resolves through (:1006), now withholds the row's
  // EXISTENCE too. A 403 there said "this id is real but not yours", which let
  // anybody holding request:view walk the id space and count the company's
  // requests.
  //
  // The POST is unchanged at 403 and is the sharper half of the case either way:
  // `RequestCancellation` (internal/store/requests.go:1262) compares
  // `requester_id` to the actor, so holding request:cancel is not permission to
  // cancel somebody else's request even for a caller who already knows the id.
  test('TC-C-085 — holding request:cancel is not permission to cancel somebody else\'s request', async () => {
    const req = await approvedRequest();
    const form = await probeGet(outsider.page, `/requests/${req.id}/cancel`);
    expect(form.status, 'a Requester\'s data scope is "own", so the row is not even readable — nor findable').toBe(
      404
    );
    const ask = await askCancel(outsider, req.id, 'I want this stopped.');
    expect(ask.status, 'and the POST is refused by the store as well').toBe(403);
    expect(await statusPill(mgrA, req.id), 'the request is untouched').toBe('Approved — awaiting payment');
  });

  test('TC-C-086 — only the approver the request was sent to may decide its cancellation', async () => {
    const req = await cancellationRequested();
    const screen = await probeGet(mgrB.page, `/requests/${req.id}/cancellation`);
    expect(screen.status, 'a manager who is not this one is refused the screen').toBe(403);
    expect(screen.body, 'and told exactly why').toContain(
      'Only the approver this request was sent to can decide its cancellation'
    );
    expect((await decideCancel(mgrB, req.id, 'accept', '')).status, 'and the POST too').toBe(403);
    expect((await cancelOutright(mgrB, req.id, 'Kill it.')).status, 'as is an outright cancel').toBe(403);
    expect(await statusPill(mgrA, req.id), 'the request is untouched').toBe('Cancellation requested');
  });

  test('TC-C-087 — the requester asks, and payment freezes at that moment', async () => {
    const req = await approvedRequest();
    await requester.page.goto(`/requests/${req.id}`);
    await requester.page.getByRole('link', { name: 'Request cancellation' }).click();
    await expect(requester.page).toHaveURL(new RegExp(`/requests/${req.id}/cancel$`));
    // The ask screen is where approved_at surfaces to the requester.
    await expect(requester.page.locator('.rh-meta'), 'the screen names who approved it and when').toContainText(
      `approved by ${mgrA.subject.name} on`
    );

    await requester.page.getByLabel('Reason').fill('The site cancelled the trip.');
    await requester.page.getByRole('button', { name: 'Send cancellation request' }).click();
    await expect(requester.page).toHaveURL(new RegExp(`/requests/${req.id}$`));

    await expect(requester.page.locator('.rh-status .pill.cancelreq'), 'the status changes').toBeVisible();
    await expect(requester.page.locator('.banner.warn'), 'and payment is frozen, with the reason').toContainText(
      'Payment is frozen'
    );
    await expect(requester.page.locator('.banner.warn')).toContainText('The site cancelled the trip.');
    // The stored action is `cancel_request`; `actionText` now renders it as
    // "Cancellation asked" (it used to fall through to the raw token), so the
    // trail is matched on the words a reader actually sees — the same convention
    // TC-C-121 follows for `update` → "Updated".
    expect(await auditTrail(req.id), 'the ask is audited as its own action').toContain('Cancellation asked');
  });

  test('TC-C-088 — a cancellation ask with an empty or whitespace-only reason is refused', async () => {
    const req = await approvedRequest();
    const empty = await askCancel(requester, req.id, '');
    expect(empty.status, 'the approver reads this reason, so it cannot be blank').toBe(400);
    expect(empty.body).toContain('say why it should be cancelled');
    const blank = await askCancel(requester, req.id, '    ');
    expect(blank.status, 'spaces are not a reason').toBe(400);
    expect(await statusPill(mgrA, req.id), 'and nothing froze').toBe('Approved — awaiting payment');
  });

  test('TC-C-089 — the ask screen is offered only for an approved request', async () => {
    const pending = await pendingRequest();
    const onPending = await probeGet(requester.page, `/requests/${pending.id}/cancel`);
    expect(onPending.status, 'a pending request is withdrawn, not cancelled').toBe(400);
    expect(onPending.body, 'and the screen says which control to use instead').toContain('withdraw it instead');

    const rejected = await rejectedRequest();
    const onRejected = await probeGet(requester.page, `/requests/${rejected.id}/cancel`);
    expect(onRejected.status, 'and a closed request is already closed').toBe(400);
  });

  test('TC-C-090 — the approver\'s decision screen shows the ask and offers both answers', async () => {
    const req = await cancellationRequested();
    await mgrA.page.goto(`/requests/${req.id}/cancellation`);
    await expect(mgrA.page.locator('.card', { hasText: 'wants it cancelled' }), 'the ask is quoted').toContainText(
      'The trip is off.'
    );
    await expect(mgrA.page.getByRole('button', { name: 'Decline — keep it live' })).toBeVisible();
    await expect(mgrA.page.getByRole('button', { name: 'Cancel the request' })).toBeVisible();
  });

  test('TC-C-091 — declining a cancellation demands a written reason', async () => {
    const req = await cancellationRequested();
    const empty = await decideCancel(mgrA, req.id, 'decline', '');
    expect(empty.status, 'the requester and Accounts both read this').toBe(400);
    expect(empty.body).toContain('say why it should still be paid');
    const blank = await decideCancel(mgrA, req.id, 'decline', ' \t ');
    expect(blank.status, 'spaces are not a reason').toBe(400);
    expect(await statusPill(mgrA, req.id), 'and the request stays frozen').toBe('Cancellation requested');
  });

  test('TC-C-092 — declining unfreezes the request and is recorded', async () => {
    const req = await cancellationRequested();
    await mgrA.page.goto(`/requests/${req.id}/cancellation`);
    await mgrA.page.getByRole('button', { name: 'Decline — keep it live' }).click();
    const sheet = mgrA.page.locator('#decline-sheet');
    await expect(sheet).toBeVisible();
    await sheet.getByLabel('Why it should still be paid').fill('The deposit is non-refundable.');
    await sheet.getByRole('button', { name: 'Decline and unfreeze' }).click();
    await expect(mgrA.page, 'the decision returns the approver to the cancellations tab').toHaveURL(
      /\/approvals\?bucket=cancellations$/
    );

    await requester.page.goto(`/requests/${req.id}`);
    await expect(requester.page.locator('.rh-status .pill'), 'the request is live again').toHaveText(
      'Approved — awaiting payment'
    );
    await expect(requester.page.locator('.banner.locked'), 'and locked again').toBeVisible();
    await expect(requester.page.locator('.banner.warn'), 'with nothing frozen').toHaveCount(0);
    const trail = await auditTrail(req.id);
    expect(trail, 'the decision has its own action').toContain('cancel_decline');
    expect(trail, 'carrying the reason').toContain('The deposit is non-refundable.');
  });

  test('TC-C-093 — accepting closes the request permanently, and the note is optional', async () => {
    const req = await cancellationRequested();
    succeeded(await decideCancel(mgrA, req.id, 'accept', ''), 'accepting needs no note — only declining does');
    expect(await statusPill(requester, req.id), 'the request is cancelled').toBe('Cancelled');
    await requester.page.goto(`/requests/${req.id}`);
    await expect(requester.page.locator('.waiting'), 'and nothing can be paid against it').toContainText(
      'Cancelled. Nothing can be paid against it'
    );
    // Accepting a cancellation IS cancelling, so it lands under the same
    // `cancel` action as an outright cancel; only the summary says who asked.
    expect(await auditTrail(req.id), 'and the summary says who asked for it').toContain("at the requester's asking");
  });

  test('TC-C-094 — the approver may cancel an approved request outright, with a reason', async () => {
    const req = await approvedRequest();
    await mgrA.page.goto(`/requests/${req.id}`);
    await mgrA.page.getByRole('link', { name: 'Cancel with reason' }).click();
    await expect(mgrA.page).toHaveURL(new RegExp(`/requests/${req.id}/cancellation$`));
    await mgrA.page.getByRole('button', { name: 'Cancel with reason' }).click();
    const sheet = mgrA.page.locator('#outright-sheet');
    await expect(sheet).toBeVisible();
    await sheet.getByLabel('Reason').fill('The order was placed twice.');
    await sheet.getByRole('button', { name: 'Cancel request' }).click();

    expect(await statusPill(requester, req.id), 'the request is cancelled without anybody having asked').toBe(
      'Cancelled'
    );
    const trail = await auditTrail(req.id);
    expect(trail, 'with the reason in the trail').toContain('The order was placed twice.');
    expect(await thread(requester, req.id), 'and on the requester\'s own history').toContain(
      'The order was placed twice.'
    );
  });

  test('TC-C-095 — an outright cancellation with no reason is refused', async () => {
    const req = await approvedRequest();
    const probe = await cancelOutright(mgrA, req.id, '   ');
    expect(probe.status, 'cancelling somebody else\'s approved request must be explained').toBe(400);
    expect(probe.body).toContain('a reason is required to cancel a request');
    expect(await statusPill(mgrA, req.id), 'and nothing changed').toBe('Approved — awaiting payment');
  });

  test('TC-C-096 — an outright cancellation is also legal while a cancellation is pending', async () => {
    // legalTransitions allows cancellation_requested → cancelled
    // (internal/store/requests.go:97), and CancelRequest does not additionally
    // insist on 'approved', so this route reaches the same end as accepting.
    const req = await cancellationRequested();
    succeeded(await cancelOutright(mgrA, req.id, 'Cancelling it myself.'), 'cancelling a frozen request outright');
    expect(await statusPill(requester, req.id), 'the request is cancelled').toBe('Cancelled');
  });

  test('TC-C-097 — a hold is suspended by a cancellation ask and restored when the ask is declined', async () => {
    // REWRITTEN for the F-C-07 fix. As written this case asserted that the hold
    // was DROPPED by the round trip: a requester asking for cancellation and an
    // approver declining it lifted an accountant's hold between them, and the
    // request came back genuinely re-reservable with the accountant's question
    // still unanswered. That cost requirement L7, "On hold (only Accounts lifts)".
    //
    // The fix suspends the hold rather than destroying it: `RequestCancellation`
    // clears `on_hold` and deliberately keeps `hold_reason`, and
    // `DecideCancellation` restores `on_hold` from it on a decline (and clears
    // both on an accept, because nothing may still be on hold on a dead row).
    //
    // The first half of this case is UNCHANGED and is not a defect: the flag
    // really is suspended while the request is frozen, because `on_hold=1` implies
    // `status='approved'` is an invariant the hold tab, the hold pill and
    // `ReserveRequest` all trust one column for — recorded in the build and pinned
    // by linking_test.go. What changed is the second half: after the decline the
    // pause is back, and only Accounts lifts it.
    const req = await approvedRequest();
    succeeded(await hold(accounts, req.id, 'Send the original receipt.'), 'Accounts puts an approved request on hold');
    expect(await statusPills(requester, req.id), 'the hold is what the reader sees').toContain('On hold');
    await expect(requester.page.locator('.banner.warn'), 'with the question that caused it').toContainText(
      'Send the original receipt.'
    );

    succeeded(await askCancel(requester, req.id, 'No longer needed.'), 'the requester asks for cancellation');
    const frozenPills = await statusPills(requester, req.id);
    expect(frozenPills, 'the status moves off approved').toContain('Cancellation requested');
    expect(frozenPills, 'and on_hold=1 implies status=approved, so the hold is suspended').not.toContain('On hold');
    await requester.page.goto(`/requests/${req.id}`);
    await expect(
      requester.page.locator('.banner.warn'),
      'the frozen request shows the cancellation banner and only that: a suspended hold is never rendered as one'
    ).toContainText('Payment is frozen');

    succeeded(await decideCancel(mgrA, req.id, 'decline', 'Pay it as approved.'), 'the approver declines');
    const backPills = await statusPills(requester, req.id);
    expect(
      backPills,
      'the pause survives the round trip: the request is live again but back on hold, ' +
        'so the pill says the thing that blocks payment'
    ).toContain('On hold');
    expect(backPills, 'and the freeze is over — nothing is waiting on a cancellation decision').not.toContain(
      'Cancellation requested'
    );
    await requester.page.goto(`/requests/${req.id}`);
    await expect(
      requester.page.locator('.banner.warn'),
      "the hold banner is back, still carrying the accountant's unanswered question"
    ).toContainText('Send the original receipt.');
    await expect(
      requester.page.locator('.banner.warn'),
      'and it says who may lift it: not the requester, and not the approver who declined',
    ).toContainText('until Accounts lifts the hold');

    // The pause survived; so did the record of it.
    const history = await thread(requester, req.id);
    expect(history, 'the hold event is on the history').toContain('Send the original receipt.');
    const trail = await auditTrail(req.id);
    expect(trail, 'and in the audit trail, under its own action').toContain('hold');
    expect(trail, 'with its reason').toContain('Send the original receipt.');
  });

  test('TC-C-098 — after the decline the request is NOT takeable: only Accounts lifts the restored hold', async () => {
    // REWRITTEN for the F-C-07 fix. As written this case asserted the request was
    // immediately re-reservable after the declined cancellation — which was the
    // defect, and its own proof requirement was that a reservation should be
    // refused instead. `ReserveRequest`'s conditional UPDATE requires
    // `status='approved' AND processing_by IS NULL AND on_hold=0` and now names its
    // refusal `ErrRequestOnHold` (a 403), so the reservation probe is the only
    // proof that `on_hold` is genuinely 1 rather than merely rendered.
    //
    // What this case now protects: the restored hold is real all the way down —
    // the queue offers a reply rather than the work, the reservation route refuses,
    // and `UnholdRequest` — Accounts — is what clears it.
    const req = await approvedRequest();
    succeeded(await hold(accounts, req.id, 'Clarify the head.'), 'holding');
    succeeded(await askCancel(requester, req.id, 'Might not need it.'), 'asking');
    succeeded(await decideCancel(mgrA, req.id, 'decline', 'Still needed.'), 'declining');

    const number = await requestNumber(requester, req.id);
    await accounts.page.goto(`/accounts-queue?tab=approved&q=${encodeURIComponent(number)}`);
    const row = accounts.page.locator('tr', { has: accounts.page.locator(`a[href="/requests/${req.id}"]`) });
    await expect(row, 'the request is back in the queue').toHaveCount(1);
    await expect(row.locator('.pill.hold'), 'and back on hold, which is what the row says').toHaveText('On hold');
    await expect(
      row.getByRole('button', { name: 'Take for processing' }),
      'so it is not offered as work: the question is still unanswered'
    ).toHaveCount(0);
    await expect(
      row.getByRole('link', { name: 'Read reply' }),
      'the accountant is offered the question instead'
    ).toBeVisible();

    // It also shows up under the hold tab, which is the queue's own record that a
    // held request is somebody's outstanding question rather than lost.
    await accounts.page.goto(`/accounts-queue?tab=hold&q=${encodeURIComponent(number)}`);
    await expect(
      accounts.page.locator('tr', { has: accounts.page.locator(`a[href="/requests/${req.id}"]`) }),
      'the restored hold puts it back on the hold tab'
    ).toHaveCount(1);

    // Not offered is not the same as not permitted. The route is the proof:
    // ReserveRequest answers ErrRequestOnHold, and requestRecordPayment renders
    // every ErrForbidden reservation refusal as the conflict screen carrying 409
    // (G15) — the same status TC-D-023 pins for a hold placed the ordinary way.
    const reserve = await probePost(accounts.page, `/requests/${req.id}/record-payment`, {});
    expect(
      reserve.status,
      `reserving a re-held request must be refused (ErrRequestOnHold); got ${reserve.outcome}`
    ).toBe(409);
    expect(await statusPill(mgrA, req.id), 'and it was not taken').toBe('On hold');

    // And the one route that does clear it is the accountant's own.
    succeeded(
      await probePost(accounts.page, `/requests/${req.id}/unhold`, {}),
      'only Accounts lifts a hold — and when they do, the request is payable again'
    );
    succeeded(
      await probePost(accounts.page, `/requests/${req.id}/record-payment`, {}),
      'now it reserves'
    );
    // toContain: since settlement-7 the pill names the holder — "With Accounts — taken by …".
    expect(await statusPill(mgrA, req.id), 'and it is with Accounts, ready to pay').toContain('With Accounts');
  });

  /**
   * The information-flow half of the hold interaction.
   *
   * REWRITTEN TWICE, and the second rewrite inverts the case's own subject.
   *
   * As written for the audit it was called "nobody is told when a cancellation
   * is decided", and that was true: the notify vocabulary stopped at
   * `EventCancellationRequested`, so an accountant who had frozen work on a
   * request learned it was asked about and never learned the answer. That is
   * F-F-06, and it is fixed. There are now two events, not one, because the two
   * sentences are opposites — "nothing will be paid" and "payment is unfrozen"
   * (`EventCancellationAccepted` / `EventCancellationDeclined`,
   * `internal/notify/events.go:60-67`) — `requestCancellationDecide` fires
   * whichever is true (`internal/app/requests.go:1057-1064`), and both seed rows
   * carry IncludeRequester and IncludeAccounts
   * (`internal/store/migrations_notifications.go:112-119`).
   *
   * Which accountant: `resolveInAppUsers` addresses `processing_by` personally
   * when a reservation exists and falls back to the whole Accounts group when it
   * does not (`internal/notify/service.go:181-205`). The request here is held,
   * never reserved, so the group is the right list and the holder is in it.
   *
   * The control is kept and still matters — the ask's own notification proves
   * the mechanism is live in this environment, so a count that did NOT move
   * afterwards would be a real absence rather than switched-off mail.
   *
   * The earlier rewrite, for F-C-07, is unchanged: the tail used to assert the
   * accountant's question was gone from every screen after the decline. It is
   * not — the hold is suspended by the ask and restored by the decline, so the
   * question comes back onto the screen they work from. Between the two fixes
   * the accountant now both keeps the hold and hears about the decision.
   */
  test('TC-C-097B — the cancellation decision reaches the accountant and the requester, and the hold survives it', async () => {
    const req = await approvedRequest();
    const number = await requestNumber(requester, req.id);
    const notices = async (actor: Actor) => {
      await actor.page.goto('/notifications?scope=all');
      return actor.page.locator('.notif-list .notif', { hasText: number }).count();
    };

    succeeded(await hold(accounts, req.id, 'Which head should this hit?'), 'holding');
    const beforeAsk = await notices(accounts);

    succeeded(await askCancel(requester, req.id, 'Might not need it after all.'), 'asking');
    const afterAsk = await notices(accounts);
    expect(
      afterAsk,
      'CONTROL: request_cancellation_requested includes Accounts, so the ask must reach the accountant — ' +
        'without this a count that did not move below would prove nothing'
    ).toBeGreaterThan(beforeAsk);

    // The requester already holds notices for the approval and for the hold —
    // both correctly addressed to them — so the question is not how many they
    // have but whether the decision adds one. It does.
    const requesterBefore = await notices(requester);
    succeeded(await decideCancel(mgrA, req.id, 'decline', 'Pay it as approved.'), 'declining');
    expect(
      await notices(accounts),
      'the decision that unfroze the payment tells the accountant who had stopped working on it'
    ).toBeGreaterThan(afterAsk);
    expect(
      await notices(requester),
      'and it tells the requester who asked for the cancellation what became of their ask'
    ).toBeGreaterThan(requesterBefore);

    // It is the DECLINE they are told about, not the ask again: the two events
    // exist separately because the sentences are opposites, and an accountant
    // told "cancellation requested" twice would stop rather than restart.
    await accounts.page.goto('/notifications?scope=all');
    await expect(
      accounts.page.locator('.notif-list .notif', { hasText: number }).first(),
      'the newest notice on this request says the cancellation was declined'
    ).toContainText('declined', { ignoreCase: true });

    // And the question the accountant asked is back on the screen they work from
    // (F-C-07), so the notification and the hold agree with each other.
    await accounts.page.goto(`/requests/${req.id}`);
    await expect(
      accounts.page.locator('.banner.warn'),
      'the hold banner is back, still asking what it asked'
    ).toContainText('Which head should this hit?');
    expect(await statusPills(accounts, req.id), 'and the pill says payment is still paused').toContain('On hold');
    expect(await auditTrail(req.id), 'the reason is on the trail as well as the screen').toContain(
      'Which head should this hit?'
    );
  });

  test('TC-C-099 — a hold cannot be placed while a cancellation is pending', async () => {
    const req = await cancellationRequested();
    const probe = await hold(accounts, req.id, 'One more question.');
    // HoldRequest returns ErrForbidden, and respondStoreError replaces the
    // store's sentence with the generic one for that class, so the observable
    // truth is the status and the unchanged state — not the wording.
    expect(probe.status, 'a hold only ever describes an approved request').toBe(403);
    expect(await statusPill(mgrA, req.id), 'and the frozen request is untouched').toBe('Cancellation requested');
    expect(await statusPills(requester, req.id), 'with no hold on it').not.toContain('On hold');
  });
});

// --------------------------------------------------------------------------
// 8. Legal transitions, exhaustively. Table-driven: one test per from-state,
//    every illegal action fired at a request really in that state.
// --------------------------------------------------------------------------

interface TransitionAction {
  key: string;
  /** The states the design intends this action to be legal from. */
  legalFrom: string[];
  /**
   * States where the code accepts the action although the design does not
   * intend it. Excluded from the grid loop and asserted in a dedicated,
   * annotated test instead, so the failure is a named signal rather than one
   * lost row in a table.
   */
  bypasses?: string[];
  run: (id: number, title: string) => Promise<Probe>;
}

const transitionActions: TransitionAction[] = [
  {
    key: 'approve',
    legalFrom: ['pending'],
    // No bypass any more. `cancellation_requested` was excluded from the grid
    // while F-C-03 was open — legalTransitions carries cancellation_requested →
    // approved so DecideCancellation can decline, and ApproveRequest reused that
    // edge — so the failure was reported once, by name, as TC-C-113B rather than
    // as one lost row in a table. ApproveRequest now tests the frozen status
    // itself, so approve belongs in the grid like every other action and
    // TC-C-113 fires it at a frozen request on every run.
    run: id => approve(mgrA, id, '18400')
  },
  { key: 'return', legalFrom: ['pending'], run: id => sendBack(mgrA, id, 'Needs work.') },
  { key: 'reject', legalFrom: ['pending'], run: id => reject(mgrA, id, 'No.') },
  { key: 'withdraw', legalFrom: ['pending'], run: id => withdraw(requester, id) },
  { key: 'resubmit', legalFrom: ['returned'], run: (id, title) => resubmit(requester, id, title, mgrA.id) },
  { key: 'cancel-request', legalFrom: ['approved'], run: id => askCancel(requester, id, 'Please stop it.') },
  {
    key: 'cancel-outright',
    legalFrom: ['approved', 'cancellation_requested'],
    run: id => cancelOutright(mgrA, id, 'Cancelling.')
  },
  {
    key: 'decide-cancellation',
    legalFrom: ['cancellation_requested'],
    run: id => decideCancel(mgrA, id, 'decline', 'Keep it.')
  },
  { key: 'reraise', legalFrom: ['rejected'], run: id => reraise(requester, id) }
];

test.describe('legal transitions', () => {
  const states: Array<{ tc: string; status: string; pill: string; build: () => Promise<{ id: number; title: string }> }> =
    [
      { tc: 'TC-C-110', status: 'pending', pill: 'Awaiting approval', build: () => pendingRequest() },
      { tc: 'TC-C-111', status: 'returned', pill: 'Returned for correction', build: returnedRequest },
      { tc: 'TC-C-112', status: 'approved', pill: 'Approved — awaiting payment', build: () => approvedRequest() },
      {
        tc: 'TC-C-113',
        status: 'cancellation_requested',
        pill: 'Cancellation requested',
        build: cancellationRequested
      },
      { tc: 'TC-C-114', status: 'rejected', pill: 'Rejected — final', build: rejectedRequest },
      { tc: 'TC-C-115', status: 'withdrawn', pill: 'Withdrawn', build: withdrawnRequest },
      { tc: 'TC-C-116', status: 'cancelled', pill: 'Cancelled', build: cancelledRequest }
    ];

  for (const state of states) {
    test(`${state.tc} — every illegal action against a ${state.status} request is refused`, async () => {
      const req = await state.build();
      const illegal = transitionActions.filter(
        a => !a.legalFrom.includes(state.status) && !(a.bypasses ?? []).includes(state.status)
      );
      expect(illegal.length, 'the grid must actually have illegal combinations to fire').toBeGreaterThan(0);
      for (const action of illegal) {
        refused(await action.run(req.id, req.title), `${action.key} against a ${state.status} request`);
      }
      expect(await statusPill(mgrA, req.id), `and a ${state.status} request is still ${state.status}`).toBe(state.pill);
    });
  }

  /**
   * F-C-03, fixed. `legalTransitions` carries cancellation_requested → approved
   * so `DecideCancellation` can decline, and `ApproveRequest` reused the same
   * edge — so POST /approve unfroze a request whose cancellation nobody had
   * decided, with none of the decline's obligations: no `approval:cancel` gate,
   * no written reason, no `cancel_decline` row. `ApproveRequest` now tests the
   * frozen status explicitly and refuses.
   *
   * Soft assertions are kept so one run reports the whole extent of any
   * regression rather than stopping at the first thing that is wrong.
   *
   * WHAT THIS CASE NO LONGER ASSERTS, and must not be given back. It used to
   * soft-assert that the audit trail contained a `cancel_decline` row. That
   * assertion could never pass here whatever the product did: this case performs
   * exactly one POST — the refused approve — and never decides the cancellation,
   * so there is no decline to record. Its subject is the refusal, not the
   * decline. The `cancel_decline` row is asserted where a decline is actually
   * driven: TC-C-092 above, which declines through the sheet and reads the trail,
   * and `TestApproveIsRefusedWhileACancellationIsUndecided`
   * (internal/store/requests_test.go:1348), which drives this exact sequence and
   * then declines, asserting exactly one `cancel_decline` row.
   */
  test('TC-C-113B — approving a request whose cancellation is undecided is refused (finding F-C-03)', async () => {
    const req = await cancellationRequested();
    const probe = await approve(mgrA, req.id, '9999');

    expect
      .soft(probe.status, 'a frozen request must not be unfrozen through the approve route')
      .not.toBe(303);
    expect
      .soft(await statusPill(mgrA, req.id), 'the cancellation is still nobody\'s decision, so the status must hold')
      .toBe('Cancellation requested');
    // The requester must be able to see what became of their ask. Refusing the
    // approve leaves the status at cancellation_requested, so the frozen banner
    // — which is the only place cancel_reason is ever rendered — still carries
    // it. Accepting the approve stranded the reason in a column nothing shows.
    await requester.page.goto(`/requests/${req.id}`);
    expect
      .soft(
        (await requester.page.locator('main.page').textContent()) ?? '',
        'and the requester can still see what happened to the cancellation they asked for'
      )
      .toContain('The trip is off.');
  });

  /**
   * The three live statuses the Phase-2 enum does not list. There are **eleven**
   * in the shipped product — the seven in `requestStatuses`
   * (`internal/store/requests.go:86–89`) plus `processing`, `partial_review`,
   * `completed` and `completed_partial`, which Phase 3 appended — and `draft` is
   * unrepresentable, not merely unused: `CHECK (status <> 'draft')`
   * (`internal/store/migrations.go:156`).
   *
   * Reservation and settlement belong to another agent's scope; they appear here
   * only as the way to reach a state whose *legality* is this suite's question.
   */
  test('TC-C-119A — every action in the grid is refused against a processing request', async () => {
    const req = await approvedRequest();
    // One POST reserves it: requestRecordPayment → ReserveRequest → 'processing'.
    succeeded(await probePost(accounts.page, `/requests/${req.id}/record-payment`, {}), 'reserving the fixture');
    // toContain: since settlement-7 the pill names the holder — "With Accounts — taken by …".
    expect(await statusPill(mgrA, req.id), 'a reserved request is with Accounts').toContain('With Accounts');

    for (const action of transitionActions) {
      refused(await action.run(req.id, req.title), `${action.key} against a processing request`);
    }
    expect(await statusPill(mgrA, req.id), 'and it is still with Accounts').toContain('With Accounts');
  });

  test('TC-C-119B — every action in the grid is refused against a partial_review request', async () => {
    const req = await approvedRequest('18400', '18400');
    await settlePayment(accounts.page, req.id, {
      amount: '10000',
      // A past date, one month of its own — see the note on TC-C-014B's settleBody.
      paidOn: '2026-04-15',
      settlement: 'partial',
      partialReason: 'The vendor accepted a part payment for now.'
    });
    expect(await statusPill(mgrA, req.id), 'a partial payment sends it to the manager for review').toBe(
      'Partial — manager review'
    );

    for (const action of transitionActions) {
      refused(await action.run(req.id, req.title), `${action.key} against a partial_review request`);
    }
    expect(await statusPill(mgrA, req.id), 'and it is still in review').toBe('Partial — manager review');
  });

  test('TC-C-119C — every action in the grid is refused against a completed_partial request', async () => {
    const req = await approvedRequest('18400', '18400');
    await settlePayment(accounts.page, req.id, {
      amount: '10000',
      // A past date, one month of its own — see the note on TC-C-014B's settleBody.
      paidOn: '2026-05-15',
      settlement: 'partial',
      partialReason: 'Part now, the balance is written off.'
    });
    succeeded(
      await probePost(mgrA.page, `/requests/${req.id}/accept-partial`, { note: 'Accepted; close it.' }),
      'the manager accepts the shortfall'
    );
    // AcceptPartial writes 'completed_partial', a terminal state distinct from a
    // clean 'completed' (internal/store/store.go:983) — which is why the
    // legalTransitions edge partial_review → completed has no writer at all.
    expect(await statusPill(mgrA, req.id), 'accepting a partial closes it as completed_partial').toBe(
      'Completed — partial accepted'
    );

    for (const action of transitionActions) {
      refused(await action.run(req.id, req.title), `${action.key} against a completed_partial request`);
    }
    expect(await statusPill(mgrA, req.id), 'and it is still completed — partial accepted').toBe(
      'Completed — partial accepted'
    );
  });

  test('TC-C-117 — a completed request is terminal: nothing in the grid moves it', async () => {
    const req = await approvedRequest('18400', '18400');
    // A past date, one month of its own — see the note on TC-C-014B's settleBody.
    await settlePayment(accounts.page, req.id, { amount: '18400', paidOn: '2026-03-15' });
    expect(await statusPill(mgrA, req.id), 'a settled payment completes the request').toBe('Completed');

    for (const action of transitionActions) {
      refused(await action.run(req.id, req.title), `${action.key} against a completed request`);
    }
    expect(await statusPill(mgrA, req.id), 'and it is still completed').toBe('Completed');
  });

  test('TC-C-118 — the cancellation screen degrades rather than lying on a closed request', async () => {
    // requestCancellationForm has no status guard: it renders for the request's
    // own approver whatever the status, offering accept/decline only while a
    // cancellation is pending and an outright cancel only while approved. On
    // anything else it must offer a way back and no decision at all.
    for (const build of [rejectedRequest, withdrawnRequest, cancelledRequest]) {
      const req = await build();
      await mgrA.page.goto(`/requests/${req.id}/cancellation`);
      await expect(mgrA.page.locator('.action-bar'), 'the only offer is a way back').toContainText(
        'Back to the request'
      );
      await expect(mgrA.page.locator('#accept-sheet'), 'no accept sheet').toHaveCount(0);
      await expect(mgrA.page.locator('#decline-sheet'), 'no decline sheet').toHaveCount(0);
      await expect(mgrA.page.locator('#outright-sheet'), 'no outright sheet').toHaveCount(0);
    }
  });
});

// --------------------------------------------------------------------------
// 9. Edit-while-pending rerouting, from the approver's side.
// --------------------------------------------------------------------------

test.describe('rerouting by edit', () => {
  test('TC-C-120 — after a reroute the original approver cannot decide and the new one can', async () => {
    const req = await pendingRequest(mgrA.id);
    expect(await queueCards(mgrA, 'to-approve', req.title), 'it starts in the first approver\'s queue').toBe(1);

    await requester.page.goto(`/requests/${req.id}/edit`);
    await requester.page.locator('#apr').selectOption({ label: mgrB.subject.name });
    await requester.page.getByRole('button', { name: /^Save and notify/ }).click();
    await expect(requester.page).toHaveURL(new RegExp(`/requests/${req.id}$`));

    expect(await queueCards(mgrA, 'to-approve', req.title), 'the old approver loses the row').toBe(0);
    expect(await queueCards(mgrB, 'to-approve', req.title), 'and the new one gains it').toBe(1);

    expect((await approve(mgrA, req.id, '18400')).status, 'the old approver may no longer decide it').toBe(403);
    succeeded(await approve(mgrB, req.id, '18400'), 'and the new one may');
    expect(await statusPill(mgrB, req.id), 'so the request is approved by the new approver').toBe(
      'Approved — awaiting payment'
    );
  });

  test('TC-C-121 — the reroute is written into the history as an approver change', async () => {
    const req = await pendingRequest(mgrA.id);
    succeeded(await editRequest(requester, req.id, req.title, mgrB.id), 'rerouting by edit');
    await requester.page.goto(`/requests/${req.id}`);
    await expect(requester.page.locator('.thread .tl-change'), 'the change names the field a person recognises')
      .toContainText('Approver');
    // actionText renders the stored "update" as "Updated" on the audit screen,
    // so the trail is matched on the words a reader actually sees.
    const trail = await auditTrail(req.id);
    expect(trail, 'and the edit is audited').toContain('Updated');
    expect(trail, 'with a summary naming the requester and the request').toContain('edited request');
  });

  test('TC-C-122 — the two approvers see two different screens for the same request', async () => {
    const req = await pendingRequest(mgrA.id);
    succeeded(await editRequest(requester, req.id, req.title, mgrB.id), 'rerouting by edit');

    await mgrA.page.goto(`/requests/${req.id}`);
    await expect(mgrA.page.locator('.waiting.you'), 'the old approver owes nothing').toHaveCount(0);
    await expect(mgrA.page.locator('[data-open="approve-sheet"]'), 'and is offered nothing').toHaveCount(0);
    await expect(mgrA.page.locator('.waiting'), 'the line names the new approver').toContainText(mgrB.subject.name);

    await mgrB.page.goto(`/requests/${req.id}`);
    await expect(mgrB.page.locator('.waiting.you'), 'the new approver owes the decision').toBeVisible();
    await expect(mgrB.page.locator('[data-open="approve-sheet"]'), 'and is offered it').toHaveCount(1);
  });

  /**
   * F-C-04, fixed — annotation retired. `requestEdit` was three calls in a row —
   * update, attach, submit — so a resubmission that failed its own preconditions
   * left the edit committed AND audited as done while the requester was shown a
   * 400: the history asserted a change the caller had just been told did not
   * happen, and the approver had silently moved. `canTransition("pending",
   * "pending")` is false, so this is the ordinary way to reach that state, not a
   * contrived one.
   *
   * One press is now one transaction: `store.EditRequest` commits all three or
   * none, and validates the submit against the EDITED row rather than the row as
   * it was on entry (`internal/app/requests.go:797-822`). So a refused resubmit
   * writes nothing and audits nothing.
   *
   * The soft assertions are kept: a regression that reroutes AND renames should
   * be reported as both, not stopped at the first. The audit-trail assertion is
   * new — the original defect's worst half was the audit row, and a rollback
   * that left the row would be the same lie with the data put back.
   */
  test('TC-C-123 — a refused resubmission leaves nothing applied (finding F-C-04)', async () => {
    const req = await pendingRequest(mgrA.id);
    const trailBefore = await auditTrail(req.id);
    const probe = await resubmit(requester, req.id, `${req.title} corrected`, mgrB.id);
    expect(probe.status, 'a pending request cannot be resubmitted — there is nothing to resubmit').toBe(400);
    expect(probe.body, 'and the store says so').toContain('a pending request cannot be submitted');

    await requester.page.goto(`/requests/${req.id}`);
    expect
      .soft(await dlValue(requester.page, 'Approver').innerText(), 'a refused submission must not have rerouted it')
      .toBe(mgrA.subject.name);
    expect
      .soft(await requester.page.locator('h1').innerText(), 'nor renamed it')
      .toBe(req.title);
    expect
      .soft(await queueCards(mgrB, 'to-approve', req.title), 'nor put it in the other approver\'s queue')
      .toBe(0);
    expect
      .soft(await auditTrail(req.id), 'and nothing was written down as having happened')
      .toBe(trailBefore);
  });
});

// --------------------------------------------------------------------------
// 10. Concurrency.
// --------------------------------------------------------------------------

test.describe('concurrency', () => {
  test('TC-C-130 — two sessions of the approver approving at once: exactly one wins', async ({ browser }) => {
    const req = await pendingRequest(mgrA.id);
    const second = await signIn(browser, mgrA.subject);
    try {
      const [first, other] = await Promise.all([
        probePost(mgrA.page, `/requests/${req.id}/approve`, { approved_amount: '18400', note: 'first' }),
        probePost(second.page, `/requests/${req.id}/approve`, { approved_amount: '12000', note: 'second' })
      ]);
      const wins = [first, other].filter(p => p.status === 303);
      expect(wins.length, `exactly one approval may commit — got ${first.outcome} and ${other.outcome}`).toBe(1);
      // What the loser is TOLD is a separate question, and today it is told the
      // wrong thing: see TC-C-134 and finding F-C-05. What must hold either way
      // is that the loser wrote nothing.
      expect(await statusPill(mgrA, req.id), 'and the request is approved exactly once').toBe(
        'Approved — awaiting payment'
      );
      const trail = await auditTrail(req.id);
      const approvals = (trail.match(/approved request/g) ?? []).length;
      expect(approvals, 'one approval, one audit row — never two').toBe(1);
    } finally {
      await second.close();
    }
  });

  test('TC-C-131 — approving and rejecting at once: the request lands in exactly one state', async ({ browser }) => {
    const req = await pendingRequest(mgrA.id);
    const second = await signIn(browser, mgrA.subject);
    try {
      const [approval, rejection] = await Promise.all([
        probePost(mgrA.page, `/requests/${req.id}/approve`, { approved_amount: '18400' }),
        probePost(second.page, `/requests/${req.id}/reject`, { reason: 'Race the approval.' })
      ]);
      const wins = [approval, rejection].filter(p => p.status === 303);
      expect(
        wins.length,
        `one decision, not two — got ${approval.outcome} and ${rejection.outcome}`
      ).toBe(1);
      const pill = await statusPill(mgrA, req.id);
      expect(
        ['Approved — awaiting payment', 'Rejected — final'],
        `the request must be in one of the two decided states, not ${pill}`
      ).toContain(pill);
    } finally {
      await second.close();
    }
  });

  test('TC-C-132 — two different managers racing: authorisation does not depend on timing', async () => {
    const req = await pendingRequest(mgrA.id);
    const [assigned, other] = await Promise.all([
      probePost(mgrA.page, `/requests/${req.id}/approve`, { approved_amount: '18400' }),
      probePost(mgrB.page, `/requests/${req.id}/approve`, { approved_amount: '1' })
    ]);
    succeeded(assigned, 'the assigned approver wins whatever the timing');
    expect(other.status, 'and the other manager is refused on authorisation, not on the race').toBe(403);
    await requester.page.goto(`/requests/${req.id}`);
    await expect(dlValue(requester.page, 'Approved'), "the approved amount is the assigned approver's").toHaveText(
      '₹18,400.00'
    );
  });

  test('TC-C-133 — accepting and declining a cancellation at once resolves to one answer', async ({ browser }) => {
    const req = await cancellationRequested();
    const second = await signIn(browser, mgrA.subject);
    try {
      const [accept, decline] = await Promise.all([
        probePost(mgrA.page, `/requests/${req.id}/cancellation`, { decision: 'accept', note: '' }),
        probePost(second.page, `/requests/${req.id}/cancellation`, { decision: 'decline', note: 'Keep it live.' })
      ]);
      const wins = [accept, decline].filter(p => p.status === 303);
      expect(wins.length, `one answer, not two — got ${accept.outcome} and ${decline.outcome}`).toBe(1);
      const pill = await statusPill(mgrA, req.id);
      expect(
        ['Cancelled', 'Approved — awaiting payment'],
        `the request must be either cancelled or live, not ${pill}`
      ).toContain(pill);
    } finally {
      await second.close();
    }
  });

  /**
   * F-C-05, fixed. The loser of a decision race used to be handed a 500
   * internal-error page: every decision writer opened a deferred transaction,
   * READ the request and then wrote, so two of them deadlocked on the
   * read-to-write upgrade and SQLite answered SQLITE_BUSY immediately — the busy
   * handler is deliberately not consulted for an upgrade that can never succeed,
   * which is why busy_timeout(5000) never saved it.
   *
   * Each writer now takes the write lock with its first statement
   * (`beginWriteTx`) and applies one conditional UPDATE whose row count decides
   * the outcome — the shape `ReserveRequest` always had — so the loser matches no
   * rows and is told `ErrRequestRaced`, a 400.
   *
   * This case is the regression guard on all three races at once, and the soft
   * assertions are kept so one run reports every race that regresses rather than
   * stopping at the first. The bar is deliberately "not a 5xx": which of the two
   * callers wins is still timing, and TC-C-130/131/133 pin that exactly one does.
   */
  test('TC-C-134 — the loser of a decision race is refused, not handed a 500 (finding F-C-05)', async ({
    browser
  }) => {
    const second = await signIn(browser, mgrA.subject);
    try {
      const approveRace = await pendingRequest(mgrA.id);
      const [a1, a2] = await Promise.all([
        probePost(mgrA.page, `/requests/${approveRace.id}/approve`, { approved_amount: '18400' }),
        probePost(second.page, `/requests/${approveRace.id}/approve`, { approved_amount: '12000' })
      ]);
      expect
        .soft([a1, a2].find(p => p.status !== 303)!.status, 'approve vs approve: the loser must be told no')
        .toBeLessThan(500);

      const mixedRace = await pendingRequest(mgrA.id);
      const [m1, m2] = await Promise.all([
        probePost(mgrA.page, `/requests/${mixedRace.id}/approve`, { approved_amount: '18400' }),
        probePost(second.page, `/requests/${mixedRace.id}/reject`, { reason: 'Race it.' })
      ]);
      expect
        .soft([m1, m2].find(p => p.status !== 303)!.status, 'approve vs reject: the loser must be told no')
        .toBeLessThan(500);

      const cancelRace = await cancellationRequested();
      const [c1, c2] = await Promise.all([
        probePost(mgrA.page, `/requests/${cancelRace.id}/cancellation`, { decision: 'accept', note: '' }),
        probePost(second.page, `/requests/${cancelRace.id}/cancellation`, { decision: 'decline', note: 'Keep it.' })
      ]);
      expect
        .soft([c1, c2].find(p => p.status !== 303)!.status, 'accept vs decline: the loser must be told no')
        .toBeLessThan(500);
    } finally {
      await second.close();
    }
  });
});
