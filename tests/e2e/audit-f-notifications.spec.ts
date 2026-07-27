/**
 * TC-F — Notifications & Reminders audit.
 *
 * See docs/qa/test-cases/TC-F-notifications.md for the full test-case document
 * this file implements one-for-one. Every test is named with its TC ID.
 *
 * This spec drives a browser only: it never reads SMTP and never stands up a
 * mail server. Behaviours only provable at the Go level (the reminder scheduler
 * needs elapsed days a Playwright run cannot wait out; SMTP wire format needs a
 * socket) are listed as NOT RUN in the test-case document, not faked here.
 */
import { expect, test as base, type Browser, type BrowserContext, type Page } from '@playwright/test';
import { admin, login, settlePayment } from './fixtures';
import {
  asRole,
  signIn,
  probeGet,
  probePost,
  probeAnonymous
} from './audit-support';

const RUN = `fnotif${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;

// One dedicated context per role, created via a plain Playwright test so the
// long-lived describe.serial block below can hand out browser/adminPage
// without depending on fixtures.ts's test-scoped `page`/`adminPage` fixtures
// (which do not exist inside beforeAll).
const test = base;

// ---------------------------------------------------------------------------
// Local helpers. fixtures.ts's own createApprovedRequest/ensureVendor/
// ensureApprover are unexported and, more importantly, require a page that
// holds vendor:create — which a real least-privilege Requester never does.
// These helpers drive the same real screens with an explicit, exact-role
// actor at every step, so a permission gap in the app would show up as a
// failed step here rather than being silently worked around by an admin page.
// ---------------------------------------------------------------------------

async function createVendor(adminPage: Page, name: string) {
  await adminPage.goto('/vendors/new');
  await adminPage.getByLabel('Vendor name').fill(name);
  await adminPage.getByLabel('Type').selectOption('company');
  await adminPage.getByRole('button', { name: 'Save vendor' }).click();
  await adminPage.waitForURL(url => !url.pathname.endsWith('/vendors/new'));
}

interface RaisedRequest {
  id: number;
  number: string;
}

/**
 * Raises a vendor_invoice request as a real Requester-role actor. A plain
 * Requester never holds vendor:view, so the vendor field is the plain
 * <select> fallback (internal/app/requests.go:282-292), never the combobox —
 * using the combobox path here would silently test a different actor.
 */
async function raiseRequest(
  requesterPage: Page,
  opts: {
    title: string;
    amount: string;
    vendorName: string;
    approverName: string;
    invoiceNo: string;
    urgent?: boolean;
    urgencyReason?: string;
  }
): Promise<RaisedRequest> {
  await requesterPage.goto('/requests/new?type=vendor_invoice');
  await requesterPage.getByLabel('Short title').fill(opts.title);
  await requesterPage.locator('#project').selectOption({ label: 'Operations' });
  await expect(requesterPage.locator('#head option').filter({ hasNotText: 'Operations /' })).toHaveCount(1);
  await requesterPage.locator('#head').selectOption({ label: 'Operations / Office Rent' });
  await requesterPage.locator('#vendor').selectOption({ label: opts.vendorName });
  await requesterPage.getByLabel('Amount').fill(opts.amount);
  await requesterPage.getByLabel('Invoice number').fill(opts.invoiceNo);
  await requesterPage.getByLabel('Invoice date').fill('2026-07-18');
  await requesterPage.getByLabel('Purpose').fill(`Materials for ${opts.title}.`);
  if (opts.urgent) {
    await requesterPage.getByRole('checkbox', { name: 'Mark this urgent' }).check();
    await requesterPage.getByLabel('Why is it urgent').fill(opts.urgencyReason ?? 'Needed before month end.');
  }
  await requesterPage.getByLabel('Approver').selectOption({ label: opts.approverName });
  await requesterPage.getByRole('button', { name: 'Submit request' }).click();
  await expect(requesterPage).toHaveURL(/\/requests\/\d+\/submitted$/);
  const id = Number(new URL(requesterPage.url()).pathname.split('/')[2]);
  const number = (await requesterPage.locator('.rh-no').first().innerText()).trim();
  return { id, number };
}

async function approveAsManager(managerPage: Page, requestId: number, approvedAmount: string) {
  await managerPage.goto(`/requests/${requestId}`);
  await managerPage.getByRole('button', { name: /^Approve / }).click();
  const sheet = managerPage.locator('#approve-sheet');
  await expect(sheet).toBeVisible();
  await sheet.getByLabel('Amount approved').fill(approvedAmount);
  await sheet.getByRole('button', { name: 'Approve request' }).click();
  await expect(managerPage).toHaveURL(/\/approvals$/);
}

async function returnRequest(managerPage: Page, requestId: number, comment: string) {
  await managerPage.goto(`/requests/${requestId}`);
  await managerPage.getByRole('button', { name: 'Return for correction' }).click();
  const sheet = managerPage.locator('#return-sheet');
  await expect(sheet).toBeVisible();
  await sheet.getByLabel('What needs correcting').fill(comment);
  await sheet.getByRole('button', { name: 'Return request' }).click();
  await expect(managerPage).toHaveURL(/\/approvals$/);
}

async function rejectRequest(managerPage: Page, requestId: number, reason: string) {
  await managerPage.goto(`/requests/${requestId}`);
  await managerPage.getByRole('button', { name: 'Reject', exact: true }).click();
  const sheet = managerPage.locator('#reject-sheet');
  await expect(sheet).toBeVisible();
  await sheet.getByLabel('Reason for rejection').fill(reason);
  await sheet.getByRole('button', { name: 'Reject permanently' }).click();
  await expect(managerPage).toHaveURL(/\/approvals$/);
}

async function editAndResubmit(requesterPage: Page, requestId: number) {
  await requesterPage.goto(`/requests/${requestId}`);
  await requesterPage.getByRole('button', { name: 'Resubmit for approval' }).click();
  await expect(requesterPage).toHaveURL(new RegExp(`/requests/${requestId}$`));
}

async function holdRequest(accountsPage: Page, requestId: number, reason: string) {
  await accountsPage.goto(`/requests/${requestId}`);
  await accountsPage.locator('[data-open="hold-sheet"]').click();
  const sheet = accountsPage.locator('#hold-sheet');
  await expect(sheet).toBeVisible();
  await sheet.getByLabel('What do you need from the requester').fill(reason);
  await sheet.getByRole('button', { name: 'Put on hold' }).click();
  await expect(accountsPage).toHaveURL(new RegExp(`/requests/${requestId}$`));
}

async function askCancel(requesterPage: Page, requestId: number, reason: string) {
  await requesterPage.goto(`/requests/${requestId}/cancel`);
  await requesterPage.getByLabel('Reason').fill(reason);
  await requesterPage.getByRole('button', { name: 'Send cancellation request' }).click();
  await expect(requesterPage).toHaveURL(new RegExp(`/requests/${requestId}$`));
}

async function reserveOnly(accountsPage: Page, requestId: number) {
  await accountsPage.goto('/accounts-queue?tab=approved');
  const row = accountsPage.locator('tr').filter({ has: accountsPage.locator(`a[href="/requests/${requestId}"]`) });
  await row.getByRole('button', { name: 'Take for processing' }).click();
  await expect(accountsPage).toHaveURL(new RegExp(`/payments/new\\?request=${requestId}$`));
}

/** Notification list titles for whoever `page` is signed in as. */
async function notificationTitles(page: Page, scope: 'all' | 'unread' | 'mentions' | 'reminders' = 'all'): Promise<string[]> {
  await page.goto(`/notifications?scope=${scope}`);
  return page.locator('.notif-list .notif .n-main b').allTextContents();
}

async function segmentedCounts(page: Page) {
  await page.goto('/notifications');
  const read = async (scope: string) => Number(await page.locator(`.segmented a[href="/notifications?scope=${scope}"] .n`).innerText());
  return { all: await read('all'), unread: await read('unread'), mentions: await read('mentions'), reminders: await read('reminders') };
}

test.describe.serial('TC-F — notifications & reminders', () => {
  let adminCtx: BrowserContext;
  let adminPage: Page;
  let vendorName: string;

  // The shared cast. `req` is the sole requester across the whole request
  // lifecycle so its notification history is fully known and only ever grows
  // the way this file grows it — that is what makes the Section 3 count math
  // trustworthy without hand-computing a magic number in advance.
  let req: Awaited<ReturnType<typeof asRole>>;
  let mgr: Awaited<ReturnType<typeof asRole>>;
  let wrongMgr: Awaited<ReturnType<typeof asRole>>;
  let accA: Awaited<ReturnType<typeof asRole>>;
  let accB: Awaited<ReturnType<typeof asRole>>;
  let noRole: Awaited<ReturnType<typeof asRole>>;

  let invoiceSeq = 0;
  function nextInvoice() {
    invoiceSeq += 1;
    return `INV-F-${RUN}-${invoiceSeq}`;
  }

  test.beforeAll(async ({ browser }: { browser: Browser }) => {
    adminCtx = await browser.newContext();
    adminPage = await adminCtx.newPage();
    await login(adminPage, admin.email, admin.password);

    vendorName = `Notif Vendor ${RUN}`;
    await createVendor(adminPage, vendorName);

    req = await asRole(adminPage, browser, RUN, ['Requester'], 'freq');
    mgr = await asRole(adminPage, browser, RUN, ['Manager'], 'fmgr');
    wrongMgr = await asRole(adminPage, browser, RUN, ['Manager'], 'fwrongmgr');
    accA = await asRole(adminPage, browser, RUN, ['Accounts'], 'facca');
    accB = await asRole(adminPage, browser, RUN, ['Accounts'], 'faccb');
    noRole = await asRole(adminPage, browser, RUN, [], 'fnorole');
  });

  test.afterAll(async () => {
    await req?.close();
    await mgr?.close();
    await wrongMgr?.close();
    await accA?.close();
    await accB?.close();
    await noRole?.close();
    await adminCtx?.close();
  });

  // -------------------------------------------------------------------------
  // Section 1 — the in-app channel is unconditional (N1). Request A walks
  // submit -> return -> edit/resubmit -> reject, one continuous lifecycle.
  // -------------------------------------------------------------------------

  let requestA: RaisedRequest;

  test('TC-F-001 — submitting a request fires request_submitted in-app to the assigned manager only', async () => {
    requestA = await raiseRequest(req.page, {
      title: `Chain A ${RUN}`, amount: '5000.00', vendorName, approverName: mgr.subject.name,
      invoiceNo: nextInvoice()
    });
    const mgrTitles = await notificationTitles(mgr.page);
    expect(
      mgrTitles.some(t => t.includes(requestA.number)),
      'the assigned manager must see a request_submitted row naming the new request'
    ).toBe(true);
    const reqTitles = await notificationTitles(req.page);
    expect(
      reqTitles.some(t => t.includes(requestA.number)),
      'the requester gets nothing from request_submitted — only IncludeManager is set'
    ).toBe(false);
  });

  test('TC-F-002 — returning a pending request fires request_returned (mention) in-app to the requester', async () => {
    await returnRequest(mgr.page, requestA.id, 'Please attach the invoice copy.');
    const mentions = await notificationTitles(req.page, 'mentions');
    expect(
      mentions.some(t => t.includes(requestA.number)),
      'a returned request must land a mention-kind row for the requester'
    ).toBe(true);
  });

  test('TC-F-003 — editing and resubmitting a returned request fires request_edited in-app to the manager', async () => {
    const before = await notificationTitles(mgr.page);
    await editAndResubmit(req.page, requestA.id);
    const after = await notificationTitles(mgr.page);
    expect(after.length, 'resubmitting must add a new row for the manager (request_edited)').toBe(before.length + 1);
    expect(after.some(t => t.toLowerCase().includes('edited'))).toBe(true);
  });

  test('TC-F-004 — rejecting a pending request fires request_rejected (mention) in-app to the requester', async () => {
    const before = (await notificationTitles(req.page, 'mentions')).length;
    await rejectRequest(mgr.page, requestA.id, 'Budget for this line item was already spent.');
    const mentions = await notificationTitles(req.page, 'mentions');
    expect(mentions.length, 'rejecting must add a second mention row for the requester').toBe(before + 1);
    expect(mentions.some(t => t.includes(requestA.number) && t.toLowerCase().includes('reject'))).toBe(true);
  });

  // Request D — approve fires to the requester AND to every independent
  // Accounts-permission holder (N1, N3): proves group resolution, not a
  // single hardcoded recipient.
  let requestD: RaisedRequest;

  test("TC-F-005 — approving a request fires request_approved to the requester and to every independent Accounts-permission holder", async () => {
    requestD = await raiseRequest(req.page, {
      title: `Chain D ${RUN}`, amount: '7500.00', vendorName, approverName: mgr.subject.name,
      invoiceNo: nextInvoice()
    });
    await approveAsManager(mgr.page, requestD.id, '7500.00');

    const reqTitles = await notificationTitles(req.page);
    const accATitles = await notificationTitles(accA.page);
    const accBTitles = await notificationTitles(accB.page);
    for (const [who, titles] of [['requester', reqTitles], ['accounts A', accATitles], ['accounts B', accBTitles]] as const) {
      expect(titles.some(t => t.includes(requestD.number)), `${who} must receive a request_approved row`).toBe(true);
    }
  });

  test("TC-F-050 — request_approved's seeded rule never notifies the manager, only the requester and Accounts", async () => {
    // migrations_notifications.go's request_approved struct literal sets
    // IncludeRequester and IncludeAccounts only — no IncludeManager. The
    // manager already has a request_submitted row for Chain D (from raising
    // it); none of their rows may mention "approved" for that same request.
    const mgrTitles = await notificationTitles(mgr.page);
    const approvedRowsForD = mgrTitles.filter(t => t.includes(requestD.number) && t.toLowerCase().includes('approved'));
    expect(approvedRowsForD, 'the approving manager must never be told about their own approval').toEqual([]);
  });

  // Request E — hold fires request_on_hold (mention) to the requester only.
  let requestE: RaisedRequest;

  test('TC-F-006 — putting an approved request on hold fires request_on_hold (mention) to the requester only', async () => {
    requestE = await raiseRequest(req.page, {
      title: `Chain E ${RUN}`, amount: '3000.00', vendorName, approverName: mgr.subject.name,
      invoiceNo: nextInvoice()
    });
    await approveAsManager(mgr.page, requestE.id, '3000.00');
    const mentionsBefore = (await notificationTitles(req.page, 'mentions')).length;
    await holdRequest(accA.page, requestE.id, 'Please confirm the GST number on the invoice.');
    const mentions = await notificationTitles(req.page, 'mentions');
    expect(mentions.length, 'a hold must add a mention row for the requester').toBe(mentionsBefore + 1);
    expect(mentions.some(t => t.includes(requestE.number) && t.toLowerCase().includes('hold'))).toBe(true);
  });

  // Request F — full settlement fires payment_settled to requester AND manager.
  let requestF: RaisedRequest;

  test('TC-F-007 — settling a payment in full fires payment_settled to both requester and approver', async () => {
    requestF = await raiseRequest(req.page, {
      title: `Chain F ${RUN}`, amount: '4200.00', vendorName, approverName: mgr.subject.name,
      invoiceNo: nextInvoice()
    });
    await approveAsManager(mgr.page, requestF.id, '4200.00');
    await settlePayment(accA.page, requestF.id, { amount: '4200.00', paidOn: '2026-07-20' });

    const reqTitles = await notificationTitles(req.page);
    const mgrTitles = await notificationTitles(mgr.page);
    expect(reqTitles.some(t => t.includes(requestF.number) && t.toLowerCase().includes('paid'))).toBe(true);
    expect(mgrTitles.some(t => t.includes(requestF.number) && t.toLowerCase().includes('paid'))).toBe(true);
  });

  // Request G — partial settlement fires payment_partial_review to the
  // manager only. The requester's OWN count must not move.
  let requestG: RaisedRequest;

  test('TC-F-008 — a partial settlement fires payment_partial_review to the approver only, never the requester', async () => {
    requestG = await raiseRequest(req.page, {
      title: `Chain G ${RUN}`, amount: '6000.00', vendorName, approverName: mgr.subject.name,
      invoiceNo: nextInvoice()
    });
    await approveAsManager(mgr.page, requestG.id, '6000.00');
    const reqCountBefore = (await notificationTitles(req.page)).length;

    await settlePayment(accA.page, requestG.id, {
      amount: '5000.00', paidOn: '2026-07-21', settlement: 'partial', partialReason: 'Vendor short-shipped the order.'
    });

    const mgrTitles = await notificationTitles(mgr.page);
    expect(
      mgrTitles.some(t => t.includes(requestG.number) && t.toLowerCase().includes('partly paid')),
      'the manager must be told a partial payment needs review'
    ).toBe(true);
    const reqCountAfter = (await notificationTitles(req.page)).length;
    expect(reqCountAfter, 'payment_partial_review must never reach the requester').toBe(reqCountBefore);
  });

  // Request H — a requester's own cancellation ask notifies the manager,
  // never the requester who asked.
  let requestH: RaisedRequest;

  test('TC-F-009 — asking to cancel an approved request fires request_cancellation_requested to the approver, never the asking requester', async () => {
    requestH = await raiseRequest(req.page, {
      title: `Chain H ${RUN}`, amount: '2200.00', vendorName, approverName: mgr.subject.name,
      invoiceNo: nextInvoice()
    });
    await approveAsManager(mgr.page, requestH.id, '2200.00');
    const reqCountBefore = (await notificationTitles(req.page)).length;

    await askCancel(req.page, requestH.id, 'The project was cancelled internally.');

    const mgrTitles = await notificationTitles(mgr.page);
    expect(mgrTitles.some(t => t.includes(requestH.number) && t.toLowerCase().includes('cancel'))).toBe(true);
    const reqCountAfter = (await notificationTitles(req.page)).length;
    expect(reqCountAfter, 'the requester who asked to cancel gets no row of their own from this event').toBe(reqCountBefore);
  });

  // Twelve became twenty-one when migration v9 seeded rows for the nine actions
  // F-F-06 found notifying nobody, and twenty-three when v11 added the two the
  // repair itself left behind: the approver reassignment whose own sheet promises
  // "The new approver is told", and the outright cancellation by an approver,
  // which is a different act from deciding a cancellation somebody asked for.
  // Each migration only appends — v7's twelve keep their order and come first,
  // v9 appends at sort_order 13-21, v11 at 22-23, and none rewrites an existing
  // row — so this assertion still pins "the screen shows notify.AllEvents in
  // seeded order" (internal/notify/events.go:90-100), and it is the test that
  // catches a new event declared but never seeded, or seeded in the wrong place.
  test('TC-F-010 — the admin rules screen renders exactly the seeded events, in notify.AllEvents order', async () => {
    await adminPage.goto('/admin/notifications');
    const slugs = await adminPage.locator('.t-cards .t-sub').allTextContents();
    expect(slugs.map(s => s.trim())).toEqual([
      // v7 — Phase 5's twelve
      'request_submitted', 'request_edited', 'request_returned', 'request_rejected',
      'request_approved', 'request_urgent', 'request_on_hold', 'request_cancellation_requested',
      'payment_settled', 'payment_partial_review', 'reminder_pending', 'reminder_stale_reservation',
      // v9 — the nine the audit found unnotified (F-F-06)
      'request_withdrawn', 'request_reraised', 'request_unheld',
      'reservation_released', 'reservation_reassigned',
      'payment_partial_accepted', 'payment_partial_concern',
      'request_cancellation_accepted', 'request_cancellation_declined',
      // v11 — the two found while reconciling the repair against its own docs
      'approval_reassigned', 'request_cancelled'
    ]);
  });

  test('TC-F-010b — email is disabled by default for every one of the seeded events', async () => {
    // Must run before any Section-4 test flips one, and before any local
    // dev re-run has ever flipped one on this database. defaultNotification-
    // Settings (internal/store/migrations_notifications.go) never sets
    // EmailEnabled on any of the twelve struct literals, so it is Go's zero
    // value (false/0) for every one of them on a freshly-migrated database.
    // Consequence for N3: "approval email to Accounts + requester +
    // management" does not happen on a fresh install until an admin opts
    // request_approved in — only the in-app row is unconditional.
    await adminPage.goto('/admin/notifications');
    const pills = await adminPage.locator('.t-cards tbody tr td[data-label="Email"] .pill').allTextContents();
    // 23 since v11. Asserted as a count rather than left open because a row that
    // failed to seed would otherwise pass this test silently.
    expect(pills.length, 'every seeded event has an Email cell').toBe(23);
    for (const text of pills) {
      expect(text.trim(), 'every seeded event ships with email Off').toBe('Off');
    }
  });

  // -------------------------------------------------------------------------
  // Section 2 — scoping.
  // -------------------------------------------------------------------------

  test("TC-F-011 — /notifications never renders another user's rows", async () => {
    const reqTitles = await notificationTitles(req.page);
    const mgrOnlyTitles = await notificationTitles(mgr.page);
    // request_submitted (TC-F-001) and request_edited (TC-F-003) went to the
    // manager only — the requester's own list must not contain them.
    const managerOnlyMarkers = mgrOnlyTitles.filter(t => t.toLowerCase().includes('needs your approval') || t.toLowerCase().includes('edited'));
    for (const marker of managerOnlyMarkers) {
      expect(reqTitles.includes(marker), `the requester's list must not contain the manager-only row ${JSON.stringify(marker)}`).toBe(false);
    }
    // And the reverse: the manager's own list must not contain the
    // requester's mention-only rows (returned/rejected/on-hold are never
    // addressed to the manager per the seeded rules).
    const requesterOnlyMarkers = reqTitles.filter(
      t => t.toLowerCase().includes('returned') || t.toLowerCase().includes('rejected') || t.toLowerCase().includes('hold')
    );
    for (const marker of requesterOnlyMarkers) {
      expect(mgrOnlyTitles.includes(marker), `the manager's list must not contain the requester-only row ${JSON.stringify(marker)}`).toBe(false);
    }
  });

  let managerNotifId: number;

  test("TC-F-012 — GET /notifications/{id}/open for another user's id answers 404 and does not mark it read", async () => {
    await mgr.page.goto('/notifications?scope=all');
    const href = await mgr.page.locator('.notif-list .notif').first().getAttribute('href');
    expect(href, 'the manager must have at least one notification to borrow an id from').toBeTruthy();
    managerNotifId = Number((href as string).split('/')[2]);

    const before = await segmentedCounts(mgr.page);
    const probe = await probeGet(req.page, `/notifications/${managerNotifId}/open`);
    expect(probe.status, 'a foreign notification id must never open — this is a high-severity authorisation test').toBe(404);
    const after = await segmentedCounts(mgr.page);
    expect(after.unread, "the manager's own unread count must be unaffected by the requester's attempt").toBe(before.unread);
  });

  test('TC-F-013 — an anonymous GET /notifications redirects to /login', async ({ browser }) => {
    const probe = await probeAnonymous(browser, '/notifications', req.page.url());
    expect(probe.status).toBe(303);
    expect(probe.location).toContain('/login');
  });

  test('TC-F-014 — GET /notifications/{id}/open for a nonexistent id answers 404', async () => {
    const probe = await probeGet(req.page, '/notifications/999999999/open');
    expect(probe.status).toBe(404);
  });

  test('TC-F-015 — a user holding no role at all can still reach /notifications', async () => {
    const probe = await probeGet(noRole.page, '/notifications');
    expect(probe.status, 'RequireLogin only — no permission verb gates the users own centre (G19)').toBe(200);
  });

  // -------------------------------------------------------------------------
  // Section 3 — bell count, mark-read, and the .segmented filter. All of this
  // reads the requester's history as it stands after Section 1: two mention
  // rows (returned, rejected) plus activity rows from approve/hold/settle.
  // -------------------------------------------------------------------------

  test("TC-F-016 — the mobile shell's bell dot equals the unread count shown on the notifications page", async ({ browser }) => {
    const narrow = await signIn(browser, req.subject, { width: 390, height: 844 });
    try {
      await narrow.page.goto('/');
      const dotText = await narrow.page.locator('.m-topbar .m-icon .dot').innerText();
      const counts = await segmentedCounts(narrow.page);
      expect(Number(dotText), 'the mobile bell dot must equal NotificationCounts.Unread').toBe(counts.unread);
      expect(counts.unread).toBeGreaterThan(0);
    } finally {
      await narrow.close();
    }
  });

  test('TC-F-049 — the notification centre has a desktop entry point carrying the unread count (F-F-05)', async ({ browser }) => {
    // F-F-05, fixed. navSpec — the sidebar's only source — carried an item for
    // the ADMIN rules screen (notif-admin) and none for the user's own centre,
    // so the only link anywhere was the mobile .m-topbar .m-icon, which is
    // display:none above 860 px (fervid-ds.css:3881-3884/4135). A desktop user
    // could not discover their own notifications existed; only a typed URL
    // worked. navSpec now opens with one (internal/app/nav.go:61), carrying no
    // Resource — every row the screen returns is already scoped to the signed-in
    // user by the store, which is why the route itself needs no verb either
    // (TC-F-015) — and a Badge of "notifications", which buildPageShell fills
    // with the unread count (internal/app/nav.go:169).
    const wide = await signIn(browser, req.subject, { width: 1440, height: 900 });
    try {
      await wide.page.goto('/');
      await expect(wide.page.locator('.m-topbar'), 'the mobile bell is still hidden at this width').toBeHidden();
      const item = wide.page.locator('.sidebar a[href="/notifications"]');
      await expect(item, 'the desktop sidebar links to the centre exactly once').toHaveCount(1);
      await expect(item, 'and a link nobody can see is no entry point, so it must be visible').toBeVisible();
      await expect(item).toContainText('Notifications');

      // The badge is the unread count, not decoration: it must equal the number
      // the centre's own filter strip prints for the same user.
      const counts = await segmentedCounts(wide.page);
      expect(counts.unread, 'this subject has unread rows, so the badge has something to say').toBeGreaterThan(0);
      await wide.page.goto('/');
      expect(
        Number(await item.locator('.n').innerText()),
        'the sidebar badge must equal NotificationCounts.Unread, the same figure the mobile bell dot carries'
      ).toBe(counts.unread);

      // And it is a real door, not merely a link: following it lands on the centre.
      await item.click();
      await expect(wide.page).toHaveURL(/\/notifications$/);
      await expect(wide.page.locator('h1')).toHaveText('Notifications');
    } finally {
      await wide.close();
    }
  });

  test('TC-F-017 — the "All" segmented count equals the number of rows rendered for scope=all', async () => {
    const counts = await segmentedCounts(req.page);
    const rows = await req.page.locator('.notif-list .notif').count();
    expect(counts.all).toBe(rows);
  });

  test('TC-F-018 — the "Mentions" segmented filter shows only mention-kind rows, and its count matches', async () => {
    const counts = await segmentedCounts(req.page);
    await req.page.goto('/notifications?scope=mentions');
    const rows = req.page.locator('.notif-list .notif');
    await expect(rows).toHaveCount(counts.mentions);
    const glyphs = await req.page.locator('.notif-list .notif .n-ico').allTextContents();
    for (const g of glyphs) expect(g.trim(), 'every scope=mentions row must carry the mention glyph').toBe('✎');

    await req.page.goto('/notifications?scope=all');
    const allGlyphs = await req.page.locator('.notif-list .notif .n-ico').allTextContents();
    expect(allGlyphs.some(g => g.trim() !== '✎'), 'scope=all must also contain non-mention rows, proving the filter narrows').toBe(true);
  });

  test('TC-F-019 — the "Reminders" segmented filter is empty in this environment; bucket counts never exceed "All"', async () => {
    const counts = await segmentedCounts(req.page);
    expect(counts.reminders, 'no reminder can fire inside a Playwright run — see NOT RUN section').toBe(0);
    expect(counts.mentions + counts.reminders).toBeLessThanOrEqual(counts.all);
  });

  test("TC-F-020 — opening one unread notification marks only that row read and navigates to its own stored href", async () => {
    // The anchor's own href is /notifications/{id}/open — clicking it is a GET
    // that 303s onward to n.Href (requestHref() always yields /requests/{id},
    // internal/notify/service.go:165-170). "Its own stored href" is proven by
    // reading the request number out of the clicked row's title, then
    // confirming the request page landed on carries that same number.
    await req.page.goto('/notifications?scope=unread');
    const firstRow = req.page.locator('.notif-list .notif').first();
    const title = await firstRow.locator('.n-main b').innerText();
    const numberMatch = title.match(/PR-\d{4}-\d{6}/);
    expect(numberMatch, `the clicked row's title must name its own request number: ${JSON.stringify(title)}`).toBeTruthy();
    const before = await segmentedCounts(req.page);

    await firstRow.click();
    await expect(req.page).toHaveURL(/\/requests\/\d+$/);
    await expect(req.page.locator('.rh-no').first()).toHaveText((numberMatch as RegExpMatchArray)[0]);

    const after = await segmentedCounts(req.page);
    expect(after.unread, 'opening one row must decrease unread by exactly 1').toBe(before.unread - 1);
  });

  test("TC-F-021 — POST /notifications/read marks every one of the caller's unread rows read", async () => {
    const before = await segmentedCounts(req.page);
    expect(before.unread).toBeGreaterThan(0);
    await req.page.goto('/notifications');
    await req.page.getByRole('button', { name: 'Mark all read' }).click();
    const after = await segmentedCounts(req.page);
    expect(after.unread).toBe(0);
    const unreadClassCount = await req.page.locator('.notif-list .notif.unread').count();
    expect(unreadClassCount).toBe(0);
  });

  test('TC-F-022 — unread counts and mark-all-read are scoped per user', async () => {
    const mgrBefore = await segmentedCounts(mgr.page);
    expect(mgrBefore.unread, "the manager must still have unread rows of their own at this point").toBeGreaterThan(0);
    // req's mark-all-read already ran in TC-F-021; confirm it did not touch mgr.
    const mgrAfter = await segmentedCounts(mgr.page);
    expect(mgrAfter.unread).toBe(mgrBefore.unread);
  });

  test('TC-F-023 — POST /notifications/read without a CSRF token is refused and changes nothing', async () => {
    // Give the requester one more unread row so there is something to protect.
    // Submitting alone only notifies the manager (TC-F-001), so this needs a
    // request that reaches a requester-addressed event: approve, then hold.
    const requestI = await raiseRequest(req.page, {
      title: `Chain I ${RUN}`, amount: '1500.00', vendorName, approverName: mgr.subject.name,
      invoiceNo: nextInvoice()
    });
    await approveAsManager(mgr.page, requestI.id, '1500.00');
    await holdRequest(accA.page, requestI.id, 'Confirm the delivery address.');

    const before = await segmentedCounts(req.page);
    expect(before.unread).toBeGreaterThan(0);

    const probe = await probePost(req.page, '/notifications/read', {}, { csrf: 'omit' });
    expect(probe.status).toBe(403);
    const after = await segmentedCounts(req.page);
    expect(after.unread, 'a refused mark-all-read must not change the unread count').toBe(before.unread);
  });

  // -------------------------------------------------------------------------
  // Section 4 — the admin rules screen: RBAC and persistence.
  // -------------------------------------------------------------------------

  test('TC-F-024 — a Requester is refused GET /admin/notifications', async () => {
    expect((await probeGet(req.page, '/admin/notifications')).status).toBe(403);
  });

  test('TC-F-025 — a Manager is refused GET /admin/notifications', async () => {
    expect((await probeGet(mgr.page, '/admin/notifications')).status).toBe(403);
  });

  test('TC-F-026 — an Accounts user is refused GET /admin/notifications', async () => {
    expect((await probeGet(accA.page, '/admin/notifications')).status).toBe(403);
  });

  test('TC-F-027 — a Requester is refused all three admin notification POSTs', async () => {
    expect((await probePost(req.page, '/admin/notifications/smtp', { smtp_host: 'x' })).status).toBe(403);
    expect((await probePost(req.page, '/admin/notifications/events/request_approved', { email_enabled: 'on' })).status).toBe(403);
    expect((await probePost(req.page, '/admin/notifications/test', {})).status).toBe(403);
  });

  test('TC-F-028 — a Manager is refused all three admin notification POSTs', async () => {
    expect((await probePost(mgr.page, '/admin/notifications/smtp', { smtp_host: 'x' })).status).toBe(403);
    expect((await probePost(mgr.page, '/admin/notifications/events/request_approved', { email_enabled: 'on' })).status).toBe(403);
    expect((await probePost(mgr.page, '/admin/notifications/test', {})).status).toBe(403);
  });

  test('TC-F-029 — an Accounts user is refused all three admin notification POSTs', async () => {
    expect((await probePost(accA.page, '/admin/notifications/smtp', { smtp_host: 'x' })).status).toBe(403);
    expect((await probePost(accA.page, '/admin/notifications/events/request_approved', { email_enabled: 'on' })).status).toBe(403);
    expect((await probePost(accA.page, '/admin/notifications/test', {})).status).toBe(403);
  });

  // TC-F-030 through TC-F-033 read one event's sheet fields via
  // .inputValue()/.isChecked() (a DOM property read, which Playwright does not
  // require actionability/visibility for) and write them through the real POST
  // route (probePost) rather than by clicking each sheet's own "Edit" trigger
  // and "Save rule" button.
  //
  // That was originally forced. internal/app/templates.go rendered every
  // <div class="overlay" id="ev-{event}"> sheet WITHOUT the `hidden` attribute
  // every other .overlay in the app carries, so all of them were simultaneously
  // laid out full-viewport, position:fixed, z-index:50 on load and only the last
  // in DOM order was reachable by a pointer — every other Edit button was
  // permanently covered (F-F-01). It is fixed: the per-event overlays carry
  // `hidden` like approve-sheet/return-sheet/reject-sheet/hold-sheet do
  // (templates.go:3538), and TC-F-047 below drives the pointer path to prove it.
  //
  // These four keep posting to the route because what they are about is the
  // persistence contract — that a saved rule round-trips, that a rejected one
  // does not overwrite — and the route is where that contract lives. The button
  // that reaches it has its own test now, which is the honest division.

  interface EventRuleFields {
    emailEnabled: boolean;
    toRecipients: string;
    ccRecipients: string;
    includeRequester: boolean;
    includeManager: boolean;
    includeAccounts: boolean;
    subjectTemplate: string;
    bodyTemplate: string;
  }

  async function readEventRule(page: Page, event: string): Promise<EventRuleFields> {
    const sheet = page.locator(`#ev-${event}`);
    return {
      emailEnabled: await sheet.locator('input[name="email_enabled"]').isChecked(),
      toRecipients: await sheet.locator('input[name="to_recipients"]').inputValue(),
      ccRecipients: await sheet.locator('input[name="cc_recipients"]').inputValue(),
      includeRequester: await sheet.locator('input[name="include_requester"]').isChecked(),
      includeManager: await sheet.locator('input[name="include_manager"]').isChecked(),
      includeAccounts: await sheet.locator('input[name="include_accounts"]').isChecked(),
      subjectTemplate: await sheet.locator('input[name="subject_template"]').inputValue(),
      bodyTemplate: await sheet.locator('textarea[name="body_template"]').inputValue()
    };
  }

  async function saveEventRule(page: Page, event: string, fields: EventRuleFields) {
    const form: Record<string, string> = {
      to_recipients: fields.toRecipients,
      cc_recipients: fields.ccRecipients,
      subject_template: fields.subjectTemplate,
      body_template: fields.bodyTemplate
    };
    if (fields.emailEnabled) form.email_enabled = 'on';
    if (fields.includeRequester) form.include_requester = 'on';
    if (fields.includeManager) form.include_manager = 'on';
    if (fields.includeAccounts) form.include_accounts = 'on';
    const probe = await probePost(page, `/admin/notifications/events/${event}`, form);
    expect(probe.status, `saving the ${event} rule must redirect on success`).toBe(303);
  }

  test('TC-F-030 — toggling email_enabled for an event persists and round-trips', async () => {
    // Flips whatever the current value is, rather than assuming the seeded
    // default — this suite's own database is reused across local re-runs
    // (playwright.config.ts's reuseExistingServer), so "the default" is only
    // true the very first time the seed has ever run.
    await adminPage.goto('/admin/notifications');
    const before = await readEventRule(adminPage, 'request_returned');
    const flipped = !before.emailEnabled;

    await saveEventRule(adminPage, 'request_returned', { ...before, emailEnabled: flipped });

    await adminPage.goto('/admin/notifications');
    const after = await readEventRule(adminPage, 'request_returned');
    expect(after.emailEnabled, 'email_enabled must persist the flipped value').toBe(flipped);
    // A user-visible cross-check that needs no sheet open at all: the outer
    // table's own Email column is never buried under another overlay.
    const row = adminPage.locator('.t-cards tbody tr', { hasText: 'request_returned' });
    await expect(row.locator('td[data-label="Email"] .pill')).toHaveText(flipped ? 'On' : 'Off');
  });

  test('TC-F-031 — recipient lists and include-requester/manager/accounts flags persist and round-trip', async () => {
    const to = `qa-to-${RUN}@example.test`;
    const cc = `qa-cc-${RUN}@example.test`;
    await adminPage.goto('/admin/notifications');
    const before = await readEventRule(adminPage, 'request_returned');

    await saveEventRule(adminPage, 'request_returned', {
      ...before, toRecipients: to, ccRecipients: cc, includeManager: true, includeAccounts: true
    });

    await adminPage.goto('/admin/notifications');
    const after = await readEventRule(adminPage, 'request_returned');
    expect(after.toRecipients).toBe(to);
    expect(after.ccRecipients).toBe(cc);
    expect(after.includeManager).toBe(true);
    expect(after.includeAccounts).toBe(true);
    expect(after.includeRequester, 'include_requester was not touched by this save and must be unchanged').toBe(before.includeRequester);

    const row = adminPage.locator('.t-cards tbody tr', { hasText: 'request_returned' });
    await expect(row.locator('td[data-label="Fixed To / CC"]')).toContainText(to);
    await expect(row.locator('td[data-label="Fixed To / CC"]')).toContainText(cc);
  });

  const escapedSubject = `{{number}} & <b>"quoted"</b> — 'ok'`;

  test('TC-F-032 — subject/body templates containing characters needing escaping persist exactly and render safely', async () => {
    await adminPage.goto('/admin/notifications');
    const before = await readEventRule(adminPage, 'request_returned');

    await saveEventRule(adminPage, 'request_returned', { ...before, subjectTemplate: escapedSubject });

    await adminPage.goto('/admin/notifications');
    const after = await readEventRule(adminPage, 'request_returned');
    expect(after.subjectTemplate, 'the template must round-trip exactly, unescaped, as the admin typed it').toBe(escapedSubject);
    // The rest of the page must still be intact — an unescaped `<b>`/`"`
    // breaking out of the value="" attribute would corrupt the markup after it.
    await expect(adminPage.getByRole('button', { name: 'Save email settings' })).toBeVisible();
  });

  test('TC-F-033 — saving a template referencing an unknown token is rejected and the previous value is retained', async () => {
    const probe = await probePost(adminPage, '/admin/notifications/events/request_returned', {
      subject_template: '{{not_a_real_token}}', body_template: 'irrelevant'
    });
    expect(probe.status).toBe(400);
    expect(probe.body).toContain('did not save');

    await adminPage.goto('/admin/notifications');
    const after = await readEventRule(adminPage, 'request_returned');
    expect(after.subjectTemplate, 'a rejected save must not overwrite the previous value').toBe(escapedSubject);
  });

  test("TC-F-034 — the In-app column always reads \"On\" regardless of the event's email toggle", async () => {
    await adminPage.goto('/admin/notifications');
    const rows = adminPage.locator('.t-cards tbody tr');
    const count = await rows.count();
    expect(count, 'one row per seeded event — 23 since migration v11').toBe(23);
    for (let i = 0; i < count; i++) {
      await expect(rows.nth(i).locator('td[data-label="In-app"] .pill')).toHaveText('On');
    }
  });

  test('TC-F-047 — a real click on an events own Edit trigger opens that events own sheet (F-F-01)', async () => {
    // Regression guard for F-F-01. .overlay is position:fixed;inset:0;z-index:50;
    // display:grid and there is no .overlay[hidden] rule — the author stylesheet
    // beats the user agent's [hidden]{display:none} whatever the specificity — so
    // while the per-event sheets were rendered without the attribute every other
    // overlay in the product carries, all of them laid out full-viewport at once
    // and only the last in DOM order was reachable by a pointer. Every other
    // Edit button, and the SMTP form's own Save button, sat underneath the stack.
    // The attribute is back (internal/app/templates.go:3555); openDialog/closeDialog
    // already toggled it, so that was the whole fix.
    //
    // request_returned is deliberately not the last event in seeded order — that
    // one was reachable even while the defect stood, so pinning it would have
    // proved nothing.
    await adminPage.goto('/admin/notifications');
    await expect(
      adminPage.locator('#ev-request_returned'),
      'the sheet starts closed, like every other overlay in the product'
    ).toBeHidden();
    await adminPage.locator('[data-open="ev-request_returned"]').click();
    await expect(adminPage.locator('#ev-request_returned')).toBeVisible();
    await expect(
      adminPage.locator('#ev-request_returned .sh-head b'),
      'and it is that event\'s own sheet, not whichever one happened to be on top'
    ).toHaveText('Returned for correction');

    // The stack is gone, not merely re-ordered: nothing else is covering the page.
    await adminPage.locator('#ev-request_returned .sh-close').click();
    await expect(adminPage.locator('#ev-request_returned')).toBeHidden();
    await expect(
      adminPage.locator('.overlay:not([hidden])'),
      'with every sheet closed, no overlay is laid out over the screen at all'
    ).toHaveCount(0);
    await adminPage.getByRole('button', { name: 'Save email settings' }).click();
    await expect(adminPage, 'so the SMTP form underneath is clickable again').toHaveURL(/\/admin\/notifications$/);
  });

  // -------------------------------------------------------------------------
  // Section 5 — SMTP settings and the password rule.
  // -------------------------------------------------------------------------

  // TC-F-035 and TC-F-037 also save through probePost rather than clicking
  // "Save email settings". That was once forced — the button sat underneath the
  // same un-hidden overlay stack the Section-4 comment above TC-F-030 describes,
  // and this suite's first attempt to click it timed out with the
  // stale-reservation overlay intercepting the click, which is why F-F-01 was
  // filed against the whole page and not only the sheets. It is fixed, and
  // TC-F-047 now clicks this very button to prove it. These two keep posting to
  // the route because what they are about is what persists and what is never
  // echoed back, not which control reaches it.

  test('TC-F-035 — SMTP host/port/username/from-name/from-addr/base URL/management recipients persist and round-trip', async () => {
    const values = {
      smtp_host: `smtp-${RUN}.example.test`,
      smtp_port: '2526',
      smtp_username: `user-${RUN}`,
      smtp_from_name: `Fervid QA ${RUN}`,
      smtp_from_addr: `qa-${RUN}@example.test`,
      base_url: `https://qa-${RUN}.example.test`,
      management_recipients: `mgmt-${RUN}@example.test`
    };
    const probe = await probePost(adminPage, '/admin/notifications/smtp', values);
    expect(probe.status, 'saving SMTP settings must redirect on success').toBe(303);

    await adminPage.goto('/admin/notifications');
    for (const [name, value] of Object.entries(values)) {
      await expect(adminPage.locator(`[name="${name}"]`), `${name} must round-trip`).toHaveValue(value);
    }
  });

  test('TC-F-036 — no password field exists on the page, and a posted smtp_password is never echoed or audited', async () => {
    await adminPage.goto('/admin/notifications');
    expect(await adminPage.locator('input[type="password"]').count()).toBe(0);
    expect(await adminPage.locator('body').innerText()).toContain('FERVID_SMTP_PASSWORD');

    const marker = `NOPWLEAK-${RUN}`;
    const probe = await probePost(adminPage, '/admin/notifications/smtp', {
      smtp_host: `smtp-${RUN}.example.test`, smtp_password: marker, smtp_username: `user-${RUN}`
    });
    expect(probe.status).toBe(303);

    const settingsBody = await (await adminPage.goto('/admin/notifications'))?.text();
    expect(settingsBody ?? '').not.toContain(marker);

    const auditBody = await (await adminPage.goto('/audit?entity=app_setting'))?.text();
    expect(auditBody ?? '', 'the marker must never appear in the audit trail either').not.toContain(marker);
  });

  test('TC-F-037 — sending a test email with SMTP unconfigured fails gracefully with a message, not a 500 or a hang', async () => {
    // Clear the host set by TC-F-035 so this is a genuine unconfigured case.
    const clearHost = await probePost(adminPage, '/admin/notifications/smtp', {
      smtp_host: '', smtp_port: '', smtp_username: '', smtp_from_name: '', smtp_from_addr: '',
      base_url: '', management_recipients: ''
    });
    expect(clearHost.status).toBe(303);

    const probe = await probePost(adminPage, '/admin/notifications/test', { test_to: `x-${RUN}@example.test` });
    expect(probe.status, 'an unconfigured SMTP host must fail with 502, never 500').toBe(502);
    expect(probe.body).toContain('could not be sent');
  });

  // -------------------------------------------------------------------------
  // Section 6 — recipient resolution and urgency confers no authority.
  // -------------------------------------------------------------------------

  let requestK: RaisedRequest;

  test('TC-F-038 — an urgent request stays pending until its assigned manager decides', async () => {
    requestK = await raiseRequest(req.page, {
      title: `Chain K urgent ${RUN}`, amount: '9000.00', vendorName, approverName: mgr.subject.name,
      invoiceNo: nextInvoice(), urgent: true, urgencyReason: 'Vendor cuts off supply Monday.'
    });
    await req.page.goto(`/requests/${requestK.id}`);
    // requestStatusText('pending') = "Awaiting approval" (internal/app/requests.go:988-991).
    await expect(req.page.locator('.rh-status')).toContainText('Awaiting approval');
  });

  test('TC-F-039 — an urgent pending request never appears in the Accounts "approved" queue', async () => {
    await accA.page.goto('/accounts-queue?tab=approved');
    await expect(accA.page.locator(`a[href="/requests/${requestK.id}"]`)).toHaveCount(0);
  });

  test('TC-F-040 — a Manager who is not the assigned approver is refused approving an urgent request', async () => {
    const probe = await probePost(wrongMgr.page, `/requests/${requestK.id}/approve`, {
      approved_amount: '9000.00', note: ''
    });
    expect(probe.status, 'only the requests own assigned manager may approve it, urgent or not').toBe(403);
  });

  test('TC-F-041 — submitting an urgent request notifies the assigned manager immediately and not Accounts', async () => {
    const mgrTitles = await notificationTitles(mgr.page);
    expect(mgrTitles.some(t => t.startsWith('URGENT:') && t.includes(requestK.number))).toBe(true);
    const accBTitles = await notificationTitles(accB.page);
    expect(accBTitles.some(t => t.startsWith('URGENT:') && t.includes(requestK.number)), 'Accounts must not be told before approval').toBe(false);
  });

  test('TC-F-042 — approving the urgent request then notifies Accounts, completing the two-send-point design', async () => {
    await approveAsManager(mgr.page, requestK.id, '9000.00');
    const accBTitles = await notificationTitles(accB.page);
    expect(accBTitles.some(t => t.startsWith('URGENT:') && t.includes(requestK.number)), 'Accounts must be told once the urgent request is approved').toBe(true);
  });

  // -------------------------------------------------------------------------
  // Section 7 — reminders/ageing configuration and the stale-reservation screen.
  // -------------------------------------------------------------------------

  test('TC-F-043 — the Reminders-and-ageing thresholds on /configuration persist and round-trip', async () => {
    await adminPage.goto('/configuration');
    await adminPage.locator('[name="reminder_pending_days"]').fill('5');
    await adminPage.locator('[name="reminder_repeat_days"]').fill('2');
    await adminPage.locator('[name="reminder_stale_days"]').fill('3');
    await adminPage.getByRole('button', { name: /Save/ }).first().click();
    await expect(adminPage).toHaveURL(/\/configuration$/);

    await adminPage.goto('/configuration');
    await expect(adminPage.locator('[name="reminder_pending_days"]')).toHaveValue('5');
    await expect(adminPage.locator('[name="reminder_repeat_days"]')).toHaveValue('2');
    await expect(adminPage.locator('[name="reminder_stale_days"]')).toHaveValue('3');
  });

  let requestU: RaisedRequest;

  test('TC-F-044 — the "reserved too long" screen renders a reservation-history trail for a live reservation', async () => {
    requestU = await raiseRequest(req.page, {
      title: `Chain U ${RUN}`, amount: '1800.00', vendorName, approverName: mgr.subject.name,
      invoiceNo: nextInvoice()
    });
    await approveAsManager(mgr.page, requestU.id, '1800.00');
    await reserveOnly(accA.page, requestU.id);

    await accA.page.goto(`/requests/${requestU.id}/reservation/stale`);
    await expect(accA.page.locator('h2', { hasText: 'Pick one' })).toBeVisible();
    const trail = accA.page.locator('ol.thread li');
    await expect(trail.first()).toBeVisible();
    const trailText = (await trail.allTextContents()).join(' | ').toLowerCase();
    expect(trailText).toContain('reserved');
    // F-F-02, fixed: MarkReminderSent now writes an audit row of its own
    // (internal/store/reminders.go:194-198, action "remind", labelled by which
    // reminder went out) and auditPhrase falls through to actionText, which
    // renders it "Reminder sent" (internal/app/linking.go:1042). So the trail
    // *can* represent a reminder now — which is what makes this assertion worth
    // making: this reservation was taken moments ago and no reminder has fired,
    // so no line claims one did.
    expect(trailText).not.toContain('reminder');

    // F-F-03, fixed. The banner used to state "A reminder went out at the
    // one-day mark" with no {{if}} at all — untied to reminder_stale_days, which
    // is admin-configurable, and untied to whether a reminder had genuinely
    // fired. It now reads the configured threshold (internal/app/templates.go:1161),
    // and says "days" rather than "calendar days" because the thresholds are
    // elapsed days and a calendar boundary needs a timezone this app does not
    // have. TC-F-043 has just set that threshold, so the sentence is checked
    // against the live setting rather than a number written twice.
    await adminPage.goto('/configuration');
    const staleDays = await adminPage.locator('[name="reminder_stale_days"]').inputValue();
    expect(staleDays, 'TC-F-043 set this, and the banner has to be reading the same value').toBe('3');
    await accA.page.goto(`/requests/${requestU.id}/reservation/stale`);
    await expect(accA.page.locator('.banner.warn p')).toContainText(
      `A reminder goes out once a reservation has been open ${staleDays} days.`
    );
    await expect(
      accA.page.locator('.banner.warn p'),
      'and it no longer states a one-day mark the setting can contradict'
    ).not.toContainText('one-day mark');
  });

  // -------------------------------------------------------------------------
  // Section 8 — CSRF hygiene and screen boundaries.
  // -------------------------------------------------------------------------

  test('TC-F-045 — POST /admin/notifications/events/{event} with a forged CSRF token is refused, even for an Admin session', async () => {
    const probe = await probePost(adminPage, '/admin/notifications/events/request_approved', { email_enabled: 'on' }, { csrf: 'bogus' });
    expect(probe.status).toBe(403);
  });

  test("TC-F-046 — the admin rules screen never renders require_attachments or the reminder-threshold fields (Configuration's own)", async () => {
    await adminPage.goto('/admin/notifications');
    const body = await adminPage.content();
    expect(body).not.toContain('name="require_attachments"');
    expect(body).not.toContain('name="reminder_pending_days"');
  });

  test('TC-F-048 — the pending-reminder hint on /requests/new reflects the configured reminder_pending_days threshold (F-F-03)', async () => {
    // Regression guard for F-F-03. internal/app/templates.go hardcoded "three
    // calendar days" / "three-day reminder clock" in four places — the
    // new-request form's Reminders hint, the edit-request banner, the
    // returned-request banner and the "What happens next" thread — instead of
    // reading the admin-configurable reminder_pending_days threshold (default 3,
    // internal/store/reminders.go:44), so every one of those sentences became
    // wrong the moment an administrator changed it. All four now read
    // .Reminders.PendingAfterDays (templates.go:2122, :2697, :3167).
    //
    // "7 days", not "7 calendar days": the thresholds are elapsed days, and
    // calendar semantics would need a timezone this app does not have — under
    // them reminder_repeat_days degrades into nightly spam. calendarDaysBetween
    // was deleted rather than wired up, and the copy follows the code.
    // Preserve the three toggles exactly as they stand — configurationSave
    // treats an absent toggle key as "off" (internal/app/configuration.go:146-149),
    // so posting without them would silently clobber unrelated settings.
    await adminPage.goto('/configuration');
    const toggles: Record<string, string> = {};
    for (const key of ['require_attachments', 'allow_approver_choice', 'allow_direct_payments']) {
      const checked = await adminPage.locator(`input[name="${key}"]`).isChecked();
      if (checked) toggles[key] = 'on';
    }
    const configured = await probePost(adminPage, '/configuration', {
      ...toggles, reminder_pending_days: '7', reminder_repeat_days: '1', reminder_stale_days: '1'
    });
    expect(configured.status).toBe(303);

    await req.page.goto('/requests/new?type=vendor_invoice');
    const hint = await req.page.locator('.hint', { hasText: 'reminder' }).first().innerText();
    expect(hint, 'the hint names the threshold an administrator actually set').toContain('7 days');
    expect(hint, 'and never the hardcoded default it used to state whatever the setting said').not.toContain('3 days');
  });

  // -------------------------------------------------------------------------
  // Section 9 — the coverage gaps F-F-06 found in the twelve-event vocabulary,
  // now closed.
  //
  // Six real workflow actions called a.fire nowhere in internal/app —
  // withdraw, re-raise, release, unhold, accept-partial and either cancellation
  // decision — and there was no seeded event for any of them, so nothing was
  // even "failing to fire an existing rule". Migration v9 added nine events and
  // the handlers now fire them (internal/app/requests.go:956,:972,:1069,:1071;
  // internal/app/linking.go:554,:567,:712,:755,:803). Each test below drives the
  // same real action it always did and now proves the person the seeded rule
  // addresses is genuinely told, counted as a delta on their own list so a
  // colleague's rows can never stand in for theirs.
  //
  // TC-F-057 is the one that inverts the other way: F-F-07 was the mirror image
  // of the same fault — an event fired where the seeded sentence was false.
  // -------------------------------------------------------------------------

  test('TC-F-051 — withdrawing a pending request tells the manager whose queue item vanished', async () => {
    const requestWithdraw = await raiseRequest(req.page, {
      title: `Chain Withdraw ${RUN}`, amount: '900.00', vendorName, approverName: mgr.subject.name,
      invoiceNo: nextInvoice()
    });
    const before = (await notificationTitles(mgr.page)).length;
    const probe = await probePost(req.page, `/requests/${requestWithdraw.id}/withdraw`, {});
    expect(probe.status).toBe(303);
    const after = await notificationTitles(mgr.page);
    expect(
      after.length,
      'withdrawing takes the request out of the manager\'s queue, so request_withdrawn tells them it happened'
    ).toBe(before + 1);
    expect(
      after.some(t => t.includes(requestWithdraw.number) && t.toLowerCase().includes('withdrew')),
      'and the row names the request that left'
    ).toBe(true);
  });

  test('TC-F-052 — reraising a rejected request tells the manager a second attempt now needs deciding', async () => {
    // requestA (Section 1) is rejected. Reraising it creates a brand-new pending
    // request (D1) with its own number addressed to the same mgr, who would
    // otherwise never learn the second attempt exists. request_reraised fires for
    // the NEW id (internal/app/requests.go:972), which is why the row names a
    // number this suite has not seen before rather than requestA's.
    const before = await notificationTitles(mgr.page);
    const probe = await probePost(req.page, `/requests/${requestA.id}/reraise`, {});
    expect(probe.status).toBe(303);
    const after = await notificationTitles(mgr.page);
    expect(after.length, 'the approver is told the request was raised again').toBe(before.length + 1);
    const fresh = after.filter(t => !before.includes(t));
    expect(fresh.length).toBe(1);
    expect(fresh[0].toLowerCase()).toContain('again');
    expect(
      fresh[0].includes(requestA.number),
      'and it names the new request, not the rejected one it came from'
    ).toBe(false);
  });

  test('TC-F-053 — releasing a reservation tells the requester and the approver, as the release screen promises', async () => {
    // requestU (TC-F-044) is reserved by accA. The release screen states in as
    // many words that "The requester and the approver are both notified", and
    // reservation_released is seeded with IncludeRequester and IncludeManager
    // (internal/store/migrations_notifications.go:96-100), so both are.
    const reqBefore = (await notificationTitles(req.page)).length;
    const mgrBefore = (await notificationTitles(mgr.page)).length;
    const probe = await probePost(accA.page, `/requests/${requestU.id}/release`, { reason: 'Reassigning workload.', confirm: 'on' });
    expect(probe.status).toBe(303);
    const reqAfter = await notificationTitles(req.page);
    expect(reqAfter.length, 'the requester is told their invoice is unclaimed again').toBe(reqBefore + 1);
    expect(reqAfter.some(t => t.includes(requestU.number) && t.toLowerCase().includes('unclaimed'))).toBe(true);
    expect(
      (await notificationTitles(mgr.page)).length,
      'and so is the approver — the screen names both, so both must be'
    ).toBe(mgrBefore + 1);
  });

  test('TC-F-054 — lifting a hold tells the requester the block is gone', async () => {
    // requestE (TC-F-006) is on hold. request_on_hold told the requester the
    // pause began (TC-F-006); request_unheld is the other half, and without it
    // the only event in the pair was the bad news.
    const before = (await notificationTitles(req.page)).length;
    const probe = await probePost(accA.page, `/requests/${requestE.id}/unhold`, {});
    expect(probe.status).toBe(303);
    const after = await notificationTitles(req.page);
    expect(after.length, 'lifting a hold makes the request payable again, and the requester is told').toBe(before + 1);
    expect(after.some(t => t.includes(requestE.number) && t.toLowerCase().includes('off hold'))).toBe(true);
  });

  test('TC-F-055 — accepting a partial payment tells the requester the balance is not coming', async () => {
    // requestG (TC-F-008) is in partial_review. Accepting it closes the request
    // as completed_partial: the requester learns the shortfall is permanent, and
    // the assigned accountant learns to stop chasing it.
    const before = (await notificationTitles(req.page)).length;
    const probe = await probePost(mgr.page, `/requests/${requestG.id}/accept-partial`, { note: 'Accepted, the shortfall is not worth chasing.' });
    expect(probe.status).toBe(303);
    const after = await notificationTitles(req.page);
    expect(after.length, 'the request is closed against the requester\'s figure, so they are told').toBe(before + 1);
    expect(after.some(t => t.includes(requestG.number) && t.toLowerCase().includes('closed'))).toBe(true);
  });

  test('TC-F-056 — declining a cancellation tells the requester who asked', async () => {
    // requestH (TC-F-009) is cancellation_requested. Declining it returns the
    // request to Approved — awaiting payment. The two decisions are two events
    // rather than one because the sentences are opposites ("nothing will be
    // paid" / "the payment is unfrozen"); this drives the decline half.
    const before = (await notificationTitles(req.page)).length;
    const probe = await probePost(mgr.page, `/requests/${requestH.id}/cancellation`, { decision: 'decline', note: 'Payment is already in motion.' });
    expect(probe.status).toBe(303);
    const after = await notificationTitles(req.page);
    expect(after.length, 'the person who asked is told the answer').toBe(before + 1);
    expect(
      after.some(t => t.includes(requestH.number) && t.toLowerCase().includes('stands')),
      'and the row says the request stands rather than that it was cancelled'
    ).toBe(true);
  });

  test('TC-F-057 — saving corrections on a returned request without resubmitting fires nothing (F-F-07)', async () => {
    // F-F-07, fixed. requestEdit fired EventRequestEdited unconditionally, and
    // that event's seeded template says the request "was edited and re-sent for
    // approval" — false for exactly one case, "Save corrections" on a *returned*
    // request, the button whose whole purpose is staying put without re-sending.
    // The approver was told something was back in their queue while the status
    // was still `returned`. The fire is now conditional on `resubmitting ||
    // req.Status == "pending"` (internal/app/requests.go:842), so editing a
    // pending request still re-notifies — the figures the approver is about to
    // decide on have changed — and this one does not. TC-F-003 covers the
    // resubmit half, so both branches are pinned.
    const requestSaveOnly = await raiseRequest(req.page, {
      title: `Chain SaveOnly ${RUN}`, amount: '1100.00', vendorName, approverName: mgr.subject.name,
      invoiceNo: nextInvoice()
    });
    await returnRequest(mgr.page, requestSaveOnly.id, 'Please add the GST breakup.');
    const before = await notificationTitles(mgr.page);

    // "Save corrections" — submit_action=save, deliberately NOT resubmit.
    await req.page.goto(`/requests/${requestSaveOnly.id}`);
    await req.page.getByRole('button', { name: 'Save corrections' }).click();
    await expect(req.page).toHaveURL(new RegExp(`/requests/${requestSaveOnly.id}$`));

    const after = await notificationTitles(mgr.page);
    expect(
      after.length,
      'nothing was re-sent, so nothing may claim it was — the approver\'s list must not move'
    ).toBe(before.length);

    // The request itself really did stay "returned" — nothing was resubmitted,
    // which is what makes the silence correct rather than a missed event.
    await req.page.goto(`/requests/${requestSaveOnly.id}`);
    await expect(req.page.locator('body')).toContainText('sent this back');
  });
});
