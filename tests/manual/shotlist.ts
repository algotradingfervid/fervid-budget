import type { Page } from '@playwright/test';
import { EXAMPLES, type RoleKey } from './actors';

/**
 * Every screenshot the manual uses, declared in one place.
 *
 * The written manual references shots by `id`, and the build refuses to render a
 * page that names an id with no file behind it — so prose describing a screen
 * nobody photographed cannot ship.
 *
 * Preconditions matter more than they look. Most of the action panels are absent
 * from the DOM, not merely hidden, unless the record is in exactly the right
 * state and the viewer is exactly the right person: the approve panel needs a
 * pending request whose approver of record is the signed-in user; the hold panel
 * needs an approved request with no hold already on it. Every `url` below names
 * a record verified to be in the state its shot needs.
 */
export interface Shot {
  /** Stable id, also the file path under assets/shots. */
  id: string;
  /** Whose session to take it in. */
  role: RoleKey;
  /** Human label, shown in the manifest and as the default caption. */
  title: string;
  /** One line on what the picture actually shows. Written for a reader. */
  shows: string;
  /** Path to visit. */
  url: string;
  /** A `data-open` value to click before capturing, revealing an overlay. */
  openSheet?: string;
  /** Anything else that has to happen first — filling a field, submitting. */
  prepare?: (page: Page) => Promise<void>;
  /** Wait for this selector to be visible before capturing. */
  waitFor?: string;
  /** Undo whatever `prepare` changed. Runs after the screenshot, always. */
  cleanup?: (page: Page) => Promise<void>;
  /** Skip the accessibility scan (used where a deliberate error page is shown). */
  skipAxe?: boolean;
  /** Capture the entire page however tall it is, instead of the two-screenful cap. */
  fullHeight?: boolean;
  /** Expected HTTP status, when the point of the shot is a refusal. */
  expectStatus?: number;
}

const e = EXAMPLES;

/**
 * Posts a form the way the app does, carrying the double-submit CSRF token.
 *
 * Throws on refusal rather than ignoring it. That matters most in `cleanup`: an
 * unlock that quietly fails leaves the month locked for every shot that follows
 * and for the developer's database afterwards, and a swallowed `fetch` is
 * exactly how that goes unnoticed.
 */
async function post(page: Page, path: string, body: Record<string, string> = {}): Promise<void> {
  const cookies = await page.context().cookies();
  const csrf = cookies.find((c) => c.name === 'fervid_csrf')?.value ?? '';
  const result = await page.evaluate(
    async ({ path, body, csrf }) => {
      const form = new URLSearchParams({ ...body, csrf });
      const res = await fetch(path, {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: form.toString(),
        redirect: 'follow',
      });
      return { ok: res.ok, status: res.status };
    },
    { path, body, csrf },
  );
  if (!result.ok) {
    throw new Error(`POST ${path} was refused with ${result.status}`);
  }
}

export const SHOTS: Shot[] = [
  // ==================================================== getting started
  {
    id: 'shared/login',
    role: 'requester',
    title: 'The sign-in screen',
    shows: 'The sign-in form: an email field, a password field, and nothing else.',
    url: '/login',
    prepare: async (page) => {
      await page.context().clearCookies();
      await page.goto('/login');
    },
  },
  {
    id: 'shared/login-rejected',
    role: 'requester',
    title: 'A sign-in that was refused',
    shows: 'The message shown when the email and password do not match an account.',
    url: '/login',
    prepare: async (page) => {
      await page.context().clearCookies();
      await page.goto('/login');
      await page.fill('input[name="email"]', 'someone@fervid.test');
      await page.fill('input[name="password"]', 'not-the-right-password');
      await page.click('form[action="/login"] button');
      await page.waitForLoadState('domcontentloaded');
    },
  },
  {
    id: 'shared/dashboard-requester',
    role: 'requester',
    title: 'The dashboard, as a requester',
    shows: 'The home screen for somebody who only raises requests: their own work and nothing else.',
    url: '/',
  },
  {
    id: 'shared/dashboard-manager',
    role: 'manager',
    title: 'The dashboard, as an approver',
    shows: 'The home screen for an approver, led by what is waiting on their decision.',
    url: '/',
  },
  {
    id: 'shared/dashboard-accounts',
    role: 'accounts',
    title: 'The dashboard, as the accounts team',
    shows: 'The home screen for the accounts team, led by what is approved and waiting to be paid.',
    url: '/',
  },
  {
    id: 'shared/dashboard-admin',
    role: 'admin',
    title: 'The dashboard, as an administrator',
    shows: 'The home screen for an administrator, who sees every queue in the product.',
    url: '/',
  },
  {
    id: 'shared/notifications',
    role: 'manager',
    title: 'The notification centre',
    shows: 'Everything the product has told this person, newest first, unread ones marked.',
    url: '/notifications',
  },
  {
    id: 'shared/sidebar-admin',
    role: 'admin',
    title: 'The full navigation menu',
    shows: 'The sidebar as an administrator sees it, with every section the product has.',
    url: '/configuration',
  },
  {
    id: 'shared/not-found',
    role: 'requester',
    title: 'A page that does not exist',
    shows: 'What the product shows for an address that matches nothing.',
    url: '/requests/99999999',
    skipAxe: true,
    expectStatus: 404,
  },
  {
    id: 'shared/forbidden',
    role: 'requester',
    title: 'A screen your role cannot open',
    shows: 'The refusal shown when you follow a link to something your permissions do not cover.',
    url: '/grid',
    skipAxe: true,
    expectStatus: 403,
  },

  // ========================================================== requester
  {
    id: 'requester/requests-open',
    role: 'requester',
    title: 'My requests — Open',
    shows: 'The requester’s own list on the Open tab, with the four tabs and their counts.',
    url: '/requests?bucket=open',
  },
  {
    id: 'requester/requests-needs-me',
    role: 'requester',
    title: 'My requests — Needs me',
    shows: 'The tab that collects only the requests waiting on something from the requester.',
    url: '/requests?bucket=needs-me',
  },
  {
    id: 'requester/requests-closed',
    role: 'requester',
    title: 'My requests — Closed',
    shows: 'Finished requests: paid, rejected, withdrawn and cancelled.',
    url: '/requests?bucket=closed',
  },
  {
    id: 'requester/requests-all',
    role: 'requester',
    title: 'My requests — All',
    shows: 'Every request this person has ever raised, in one list.',
    url: '/requests?bucket=all',
  },
  {
    id: 'requester/requests-search-empty',
    role: 'requester',
    title: 'A search that found nothing',
    shows: 'What the list shows when a search matches none of your requests.',
    url: '/requests?bucket=all&q=zzzznothingmatchesthis',
  },
  {
    id: 'requester/choose-type',
    role: 'requester',
    title: 'Choosing what kind of request to raise',
    shows: 'Step one of raising a request: the four type cards and what each is for.',
    url: '/requests/new',
  },
  {
    id: 'requester/form-vendor-invoice',
    role: 'requester',
    title: 'The vendor invoice form',
    shows: 'The form for paying a vendor against an invoice.',
    url: '/requests/new?type=vendor_invoice',
    fullHeight: true,
  },
  {
    id: 'requester/form-reimbursement',
    role: 'requester',
    title: 'The reimbursement form',
    shows: 'The form for claiming back money you spent yourself.',
    url: '/requests/new?type=reimbursement',
    fullHeight: true,
  },
  {
    id: 'requester/form-employee-advance',
    role: 'requester',
    title: 'The employee advance form',
    shows: 'The form for money paid to a colleague before it is spent.',
    url: '/requests/new?type=employee_advance',
    fullHeight: true,
  },
  {
    id: 'requester/form-vendor-advance',
    role: 'requester',
    title: 'The vendor advance form',
    shows: 'The form for paying a vendor before the invoice exists.',
    url: '/requests/new?type=vendor_advance',
    fullHeight: true,
  },
  {
    id: 'requester/form-recoverable',
    role: 'requester',
    title: 'Marking money as recoverable',
    shows: 'The extra fields that appear when you say the money is expected back.',
    url: '/requests/new?type=employee_advance',
    fullHeight: true,
    prepare: async (page) => {
      await page.goto('/requests/new?type=employee_advance');
      await page.waitForLoadState('networkidle').catch(() => undefined);
      const recoverable = page.locator('input[name="treatment"][value="recoverable"]').first();
      if ((await recoverable.count()) > 0 && !(await recoverable.isChecked())) {
        await recoverable.check();
        await page.waitForLoadState('networkidle').catch(() => undefined);
      }
    },
  },
  {
    id: 'requester/form-validation',
    role: 'requester',
    title: 'A request the product would not accept',
    shows: 'The refusal shown when a request is submitted without the details it requires.',
    url: '/requests/new?type=reimbursement',
    skipAxe: true,
    // The new-request form carries no inline error banner: a refused submission
    // renders the error page instead. Reaching it needs a real POST that the
    // browser will not block, and every field is marked `required` — so this
    // builds a deliberately incomplete submission and posts it, rather than
    // trying to drive the form and quietly capturing a pristine one, which is
    // what an earlier version of this shot did.
    prepare: async (page) => {
      await page.goto('/requests/new?type=reimbursement');
      const cookies = await page.context().cookies();
      const csrf = cookies.find((c) => c.name === 'fervid_csrf')?.value ?? '';
      await page.evaluate((token) => {
        const f = document.createElement('form');
        f.method = 'POST';
        f.action = '/requests';
        for (const [name, value] of [['csrf', token], ['type', 'reimbursement'], ['treatment', 'budget']]) {
          const i = document.createElement('input');
          i.name = name;
          i.value = value;
          f.appendChild(i);
        }
        document.body.appendChild(f);
        f.submit();
      }, csrf);
      await page.waitForLoadState('domcontentloaded');
    },
  },
  {
    id: 'requester/detail-pending',
    role: 'requester',
    title: 'A request awaiting approval',
    shows: 'A freshly submitted request as its raiser sees it, waiting on the approver.',
    url: `/requests/${e.requesterPending}`,
  },
  {
    id: 'requester/detail-returned',
    role: 'requester',
    title: 'A request that was sent back',
    shows: 'The approver’s reason for returning a request, and the form to correct and resubmit it.',
    url: `/requests/${e.requesterReturned}`,
    fullHeight: true,
  },
  {
    id: 'requester/detail-approved',
    role: 'requester',
    title: 'An approved request',
    shows: 'A request that has cleared approval and is waiting for the accounts team.',
    url: `/requests/${e.requesterApproved}`,
  },
  {
    id: 'requester/detail-rejected',
    role: 'requester',
    title: 'A rejected request',
    shows: 'A rejected request, with the reason, and the option to raise a corrected one.',
    url: `/requests/${e.requesterRejected}`,
  },
  {
    id: 'requester/detail-withdrawn',
    role: 'requester',
    title: 'A withdrawn request',
    shows: 'A request the raiser took back before anybody decided it.',
    url: `/requests/${e.requesterWithdrawn}`,
  },
  {
    id: 'requester/detail-processing',
    role: 'requester',
    title: 'A request being paid',
    shows: 'A request the accounts team has claimed and is working on.',
    url: `/requests/${e.requesterProcessing}`,
  },
  {
    id: 'requester/detail-completed',
    role: 'requester',
    title: 'A request that has been paid',
    shows: 'A finished request, showing the payment recorded against it.',
    url: `/requests/${e.requesterCompleted}`,
  },
  {
    id: 'requester/detail-cancellation-asked',
    role: 'requester',
    title: 'A cancellation you have asked for',
    shows: 'What the raiser sees after asking to cancel an approved request.',
    url: `/requests/${e.requesterCancellationRequested}`,
  },
  {
    id: 'requester/edit-form',
    role: 'requester',
    title: 'Editing a request',
    shows: 'The edit form, reached from a request that has not yet been decided.',
    url: `/requests/${e.requesterReturned}/edit`,
    fullHeight: true,
  },
  {
    id: 'requester/cancel-form',
    role: 'requester',
    title: 'Asking to cancel',
    shows: 'The form for asking an approver to cancel a request they already approved.',
    url: `/requests/${e.requesterApproved}/cancel`,
  },
  {
    id: 'requester/urgent-request',
    role: 'manager',
    title: 'An urgent request',
    shows: 'How a request marked urgent is flagged, and where the reason appears.',
    url: `/requests/${e.urgent}`,
  },
  {
    id: 'requester/recoverable-request',
    role: 'manager',
    title: 'A request for money expected back',
    shows: 'A recoverable request, showing the counterparty and the date the money is due back.',
    url: `/requests/${e.recoverable}`,
  },

  // ============================================================ manager
  {
    id: 'manager/approvals-to-approve',
    role: 'manager',
    title: 'The approvals queue',
    shows: 'Everything waiting on this approver’s decision.',
    url: '/approvals?bucket=to-approve',
  },
  {
    id: 'manager/approvals-cancellations',
    role: 'manager',
    title: 'Cancellation requests waiting',
    shows: 'The tab holding requests whose raisers have asked to cancel them.',
    url: '/approvals?bucket=cancellations',
  },
  {
    id: 'manager/approvals-decided',
    role: 'manager',
    title: 'Decisions already taken',
    shows: 'The record of what this approver has already approved, rejected or cancelled.',
    url: '/approvals?bucket=decided',
  },
  {
    id: 'manager/request-pending',
    role: 'manager',
    title: 'A request awaiting your decision',
    shows: 'The request as its approver sees it, with the full action bar along the bottom.',
    url: `/requests/${e.managerPending}`,
  },
  {
    id: 'manager/approve-sheet',
    role: 'manager',
    title: 'Approving a request',
    shows: 'The approve panel, where the amount can be reduced before approving.',
    url: `/requests/${e.managerPending}`,
    openSheet: 'approve-sheet',
  },
  {
    id: 'manager/return-sheet',
    role: 'manager',
    title: 'Returning a request for correction',
    shows: 'The return panel, where a correction message is required.',
    url: `/requests/${e.managerPending}`,
    openSheet: 'return-sheet',
  },
  {
    id: 'manager/reject-sheet',
    role: 'manager',
    title: 'Rejecting a request',
    shows: 'The reject panel. A reason is required, and rejection cannot be undone.',
    url: `/requests/${e.managerPending}`,
    openSheet: 'reject-sheet',
  },
  {
    id: 'manager/reassign-sheet',
    role: 'manager',
    title: 'Reassigning an approval',
    shows: 'The panel for handing a decision to a different approver, with a required reason.',
    url: `/requests/${e.managerPending}`,
    openSheet: 'reassign-approver-sheet',
  },
  {
    id: 'manager/cancellation-decision',
    role: 'manager',
    title: 'Deciding a cancellation request',
    shows: 'The screen for accepting or declining a raiser’s request to cancel.',
    url: `/requests/${e.managerCancellationRequested}/cancellation`,
  },
  {
    id: 'manager/cancellation-accept-sheet',
    role: 'manager',
    title: 'Accepting a cancellation',
    shows: 'The confirmation panel for cancelling the request permanently.',
    url: `/requests/${e.managerCancellationRequested}/cancellation`,
    openSheet: 'accept-sheet',
  },
  {
    id: 'manager/cancellation-decline-sheet',
    role: 'manager',
    title: 'Declining a cancellation',
    shows: 'The panel for refusing a cancellation, which needs a reason it should still be paid.',
    url: `/requests/${e.managerCancellationRequested}/cancellation`,
    openSheet: 'decline-sheet',
  },
  {
    id: 'manager/outright-cancel-sheet',
    role: 'manager',
    title: 'Cancelling a request nobody asked about',
    shows: 'The approver cancelling an approved request on their own initiative.',
    url: `/requests/${e.managerApproved}/cancellation`,
    openSheet: 'outright-sheet',
  },
  {
    id: 'manager/partial-review',
    role: 'manager',
    title: 'Reviewing a shortfall',
    shows: 'What the approver sees when Accounts paid less than was approved.',
    url: `/requests/${e.managerPartialReview}/partial-review`,
    fullHeight: true,
  },
  {
    id: 'manager/partial-close-sheet',
    role: 'manager',
    title: 'Accepting the shortfall and closing',
    shows: 'The panel comparing approved, paid and written-off amounts before closing the request.',
    url: `/requests/${e.managerPartialReview}/partial-review`,
    openSheet: 'close-sheet',
  },
  {
    id: 'manager/partial-concern-sheet',
    role: 'manager',
    title: 'Raising a concern about a shortfall',
    shows: 'The panel for pushing back on a short payment. Nothing is reversed by it.',
    url: `/requests/${e.managerPartialReview}/partial-review`,
    openSheet: 'concern-sheet',
  },
  {
    id: 'manager/detail-rejected',
    role: 'manager',
    title: 'A request you rejected',
    shows: 'A rejected request from the approver’s side, with the reason recorded.',
    url: `/requests/${e.managerRejected}`,
  },
  {
    id: 'manager/detail-completed',
    role: 'manager',
    title: 'A request that completed',
    shows: 'The end state: approved, paid, and closed.',
    url: `/requests/${e.managerCompleted}`,
  },
  {
    id: 'manager/grid',
    role: 'manager',
    title: 'The variance grid',
    shows: 'Budget against actual for every head in a month, with over- and under-spend called out.',
    url: `/grid?month=${e.openMonth}`,
  },
  {
    id: 'manager/reports-monthly',
    role: 'manager',
    title: 'The monthly report',
    shows: 'Spending by month across the whole organisation.',
    url: '/reports/monthly',
  },
  {
    id: 'manager/reports-projects',
    role: 'manager',
    title: 'The report by project',
    shows: 'The same figures grouped by project instead of by month.',
    url: '/reports/projects',
  },
  {
    id: 'manager/reports-heads',
    role: 'manager',
    title: 'The report by expense head',
    shows: 'Spending broken down to the individual budget line.',
    url: '/reports/heads',
  },
  {
    id: 'manager/payments-refused',
    role: 'manager',
    title: 'The payments ledger, refused',
    shows: 'An approver opening the payments ledger. The approver role does not carry that permission.',
    url: '/payments',
    skipAxe: true,
    expectStatus: 403,
  },

  // =========================================================== accounts
  {
    id: 'accounts/queue-approved',
    role: 'accounts',
    title: 'The payment queue',
    shows: 'Approved requests nobody has claimed yet — the accounts team’s starting point.',
    url: '/accounts-queue?tab=approved',
  },
  {
    id: 'accounts/queue-processing',
    role: 'accounts',
    title: 'Requests being worked on',
    shows: 'Requests somebody has claimed, and who is holding each one.',
    url: '/accounts-queue?tab=processing',
  },
  {
    id: 'accounts/queue-hold',
    role: 'accounts',
    title: 'Requests on hold',
    shows: 'Approved requests parked with a reason, waiting on the requester.',
    url: '/accounts-queue?tab=hold',
  },
  {
    id: 'accounts/queue-partial-review',
    role: 'accounts',
    title: 'Shortfalls awaiting an approver',
    shows: 'Requests paid short, now waiting on the approver to accept or object.',
    url: '/accounts-queue?tab=partial_review',
  },
  {
    id: 'accounts/queue-paid',
    role: 'accounts',
    title: 'Requests already paid',
    shows: 'The finished work, kept for reference.',
    url: '/accounts-queue?tab=paid',
  },
  {
    id: 'accounts/payment-picker',
    role: 'accounts',
    title: 'Choosing a request to pay',
    shows: 'The picker shown when you open the payment form without naming a request.',
    url: '/payments/new',
  },
  {
    id: 'accounts/payment-form',
    role: 'accounts',
    title: 'Recording a payment',
    shows: 'The payment entry form for a request this user has claimed.',
    url: `/payments/new?request=${e.accountsHeldByMe}`,
    fullHeight: true,
  },
  {
    id: 'accounts/settlement-sheet',
    role: 'accounts',
    title: 'Confirming a settlement',
    shows: 'The confirmation panel comparing what was approved against what is being paid.',
    url: `/payments/new?request=${e.accountsHeldByMe}`,
    waitFor: '#settle-mount .sheet',
    prepare: async (page) => {
      await page.goto(`/payments/new?request=${e.accountsHeldByMe}`);
      await page.waitForLoadState('networkidle').catch(() => undefined);
      await page.fill('input[name="amount"]', '5000');
      const mode = page.locator('select[name="payment_mode"]');
      if ((await mode.count()) > 0) await mode.selectOption({ index: 1 }).catch(() => undefined);
      await page.locator('button[hx-post*="settlement-preview"]').first().click();
    },
  },
  // The three ways the payment form refuses a request. Each answers 409 but
  // renders a full work screen — a banner, a heading and a "what you can do"
  // card — rather than a bare error page, so all three are worth showing.
  {
    id: 'accounts/conflict-taken',
    role: 'accounts',
    title: 'Somebody else already has it',
    shows: 'The screen shown when you open a request another person has claimed.',
    url: `/payments/new?request=${e.heldBySomebodyElse}`,
    skipAxe: true,
    expectStatus: 409,
  },
  {
    id: 'accounts/conflict-on-hold',
    role: 'accounts',
    title: 'The request is on hold',
    shows: 'The refusal shown when you try to pay a request somebody has parked.',
    url: `/payments/new?request=${e.accountsOnHold}`,
    skipAxe: true,
    expectStatus: 409,
  },
  {
    id: 'accounts/conflict-already-paid',
    role: 'accounts',
    title: 'The request has already been paid',
    shows: 'The refusal that stops the same request being paid twice.',
    url: `/payments/new?request=${e.alreadyPaid}`,
    skipAxe: true,
    expectStatus: 409,
  },
  {
    id: 'accounts/reservation-release',
    role: 'accounts',
    title: 'Releasing a claim',
    shows: 'The screen for handing back a request you claimed but will not be paying.',
    url: `/requests/${e.accountsHeldByMe}/reservation`,
  },
  {
    id: 'accounts/reservation-stale',
    role: 'accounts',
    title: 'A claim held too long',
    shows: 'The nudge shown for a request that has been claimed and untouched for a day.',
    url: `/requests/${e.accountsHeldByMe}/reservation/stale`,
  },
  {
    id: 'accounts/hold-sheet',
    role: 'accounts',
    title: 'Putting a request on hold',
    shows: 'The hold panel, which needs a note saying what you want from the requester.',
    url: `/requests/${e.accountsTakeable}`,
    openSheet: 'hold-sheet',
  },
  {
    id: 'accounts/request-on-hold',
    role: 'accounts',
    title: 'A request already on hold',
    shows: 'A held request, showing the reason and the controls for releasing it.',
    url: `/requests/${e.accountsOnHold}`,
  },
  {
    id: 'accounts/payments-ledger',
    role: 'accounts',
    title: 'The payments ledger',
    shows: 'Every payment recorded, filtered by month.',
    url: '/payments',
  },
  {
    id: 'accounts/payments-ledger-empty',
    role: 'accounts',
    title: 'A month with no payments',
    shows: 'What the ledger shows for a month in which nothing was paid.',
    url: `/payments?month=${e.emptyPaymentMonth}`,
  },
  {
    id: 'accounts/payment-detail',
    role: 'accounts',
    title: 'One payment in detail',
    shows: 'A recorded payment: the figures, the reference and the request behind it.',
    url: `/payments/${e.linkedPayment}`,
  },
  {
    id: 'accounts/payment-edit',
    role: 'accounts',
    title: 'Correcting a payment',
    shows: 'The edit form, available only for payments not tied to a request.',
    url: `/payments/${e.legacyPayment}/edit`,
  },
  {
    id: 'accounts/recoverables',
    role: 'accounts',
    title: 'The recoverables register',
    shows: 'Money expected back, summarised by category and by counterparty.',
    url: '/recoverables',
  },
  {
    id: 'accounts/recoverables-list',
    role: 'accounts',
    title: 'The full recoverables list',
    shows: 'Every recoverable item, with its counterparty and due date.',
    url: '/recoverables/list',
  },
  {
    id: 'accounts/recoverable-detail',
    role: 'accounts',
    title: 'One recoverable in detail',
    shows: 'A single item of money expected back, and what has been recovered so far.',
    url: `/recoverables/${e.recoverable}`,
  },

  // ============================================================== admin
  {
    id: 'admin/users',
    role: 'admin',
    title: 'People and accounts',
    shows: 'Everybody with an account, the roles they hold, and who approves their requests.',
    url: '/users',
  },
  {
    id: 'admin/user-new-sheet',
    role: 'admin',
    title: 'Adding a person',
    shows: 'The panel for creating an account: email, name, roles and a starting password.',
    url: '/users',
    openSheet: 'user-new',
  },
  {
    id: 'admin/user-edit-sheet',
    role: 'admin',
    title: 'Editing a person',
    shows: 'The panel for changing somebody’s roles, approver or password.',
    url: '/users',
    openSheet: `user-${e.editableUser}`,
  },
  {
    id: 'admin/roles',
    role: 'admin',
    title: 'Roles and permissions',
    shows: 'The permission matrix: what each role may do, and how much data it may see.',
    url: '/roles',
    fullHeight: true,
  },
  {
    id: 'admin/role-new-sheet',
    role: 'admin',
    title: 'Creating a role',
    shows: 'The panel for adding a role of your own, alongside the four built-in ones.',
    url: '/roles',
    openSheet: 'role-new',
  },
  {
    id: 'admin/role-copy-sheet',
    role: 'admin',
    title: 'Copying a role',
    shows: 'Starting a new role from an existing one rather than from nothing.',
    url: '/roles',
    openSheet: 'role-copy',
  },
  {
    id: 'admin/projects',
    role: 'admin',
    title: 'Projects',
    shows: 'The top level of the budget structure.',
    url: '/projects',
  },
  {
    id: 'admin/heads',
    role: 'admin',
    title: 'Expense heads',
    shows: 'The individual budget lines that spending is booked against.',
    url: '/heads',
  },
  {
    id: 'admin/budgets',
    role: 'admin',
    title: 'Budgets',
    shows: 'The figures each head is allowed, month by month.',
    url: '/budgets',
  },
  {
    id: 'admin/months',
    role: 'admin',
    title: 'Monthly plans',
    shows: 'Every budget month and whether it is open or locked.',
    url: '/months',
  },
  {
    id: 'admin/months-locked',
    role: 'admin',
    title: 'A locked month',
    shows: 'A month that has been closed, which no longer accepts new payments.',
    url: '/months',
    prepare: async (page) => {
      await page.goto('/months');
      await post(page, `/months/${e.lockMonth}/lock`, { reason: 'Month closed for reporting.' });
      await page.goto('/months');
    },
    cleanup: async (page) => {
      // A reason is mandatory on unlock (internal/store/store.go:1876) — without
      // one the store answers a validation error and the month stays locked.
      await post(page, `/months/${e.lockMonth}/unlock`, { reason: 'Reopened after capturing the manual.' });
    },
  },
  {
    id: 'admin/vendors',
    role: 'admin',
    title: 'The vendor list',
    shows: 'Every vendor, with how much has been paid to each.',
    url: '/vendors',
  },
  {
    id: 'admin/vendor-new',
    role: 'admin',
    title: 'Adding a vendor',
    shows: 'The blank vendor form, including the bank details block.',
    url: '/vendors/new',
    fullHeight: true,
  },
  {
    id: 'admin/vendor-detail',
    role: 'admin',
    title: 'One vendor in detail',
    shows: 'A vendor’s details, bank information and payment history.',
    url: `/vendors/${e.vendor}`,
    fullHeight: true,
  },
  {
    id: 'admin/configuration',
    role: 'admin',
    title: 'Configuration',
    shows: 'The settings that change how the product behaves for everybody.',
    url: '/configuration',
    fullHeight: true,
  },
  {
    id: 'admin/notification-rules',
    role: 'admin',
    title: 'Notification rules',
    shows: 'Every event the product can announce, and who is told about it.',
    url: '/admin/notifications',
  },
  {
    id: 'admin/notification-rule-sheet',
    role: 'admin',
    title: 'Editing one notification rule',
    shows: 'The panel for one event: who is told, and the wording of the email.',
    url: '/admin/notifications',
    openSheet: 'ev-request_submitted',
  },
  {
    id: 'admin/audit',
    role: 'admin',
    title: 'The audit trail',
    shows: 'Who did what and when, in a record that cannot be edited.',
    url: '/audit',
  },
  {
    id: 'admin/backups',
    role: 'admin',
    title: 'Backups',
    shows: 'The list of database copies taken, and the control for taking another.',
    url: '/backups',
  },
  {
    id: 'admin/grid-earlier-month',
    role: 'admin',
    title: 'The variance grid for an earlier month',
    shows: 'The same grid pointed at a month with budgets but little spending.',
    url: `/grid?month=${e.lockMonth}`,
  },
];
