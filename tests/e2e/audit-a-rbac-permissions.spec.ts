/**
 * TC-A — permission and authorisation enforcement.
 *
 * The one question this file asks, over and over: **who may reach what?**
 * Workflow meaning belongs to the sibling suites; here every assertion is about
 * a gate opening or refusing.
 *
 * FOUR THINGS THAT SHAPE EVERY TEST BELOW, all read out of the code first:
 *
 * 1. `RequirePermission` wraps `RequireLogin` (internal/auth/auth.go:141), so an
 *    anonymous caller is **redirected 303 → /login** and never 403s. The
 *    permission refusal is only reachable with a session.
 *
 * 2. `withCSRF` sits *inside* the permission gate — every POST is registered as
 *    `RequirePermission(res, act, http.HandlerFunc(a.withCSRF(handler)))`. The
 *    order is therefore login → permission → CSRF → handler. That is what makes
 *    the whole POST half of the matrix testable **without writing anything**:
 *    probe every POST with no `csrf` field and read the refusal.
 *      - not signed in            → 303 → /login
 *      - signed in, no grant      → 403 "You do not have permission to perform this action."
 *      - signed in, grant held    → 403 "Your form session expired. Refresh the page and try again."
 *    The third outcome is the proof the gate opened, and nothing was mutated.
 *    The two messages come from different places (auth.go:143 vs app.go's
 *    `withCSRF`) and telling them apart is what makes the POST half readable.
 *
 *    A store's own refusal is now a third message again. It used to be a
 *    fourth outcome indistinguishable from the first: `respondStoreError`
 *    rendered every `ErrForbidden` with the middleware's exact wording, so a
 *    caller who simply had not reserved a request was told they lacked a
 *    permission they held (F-A-09/F-G-023). It renders `friendly(err)` now
 *    (http_errors.go), which keeps the sentence a wrapped ErrForbidden was
 *    written with — "Reserve this request before recording its payment",
 *    "System roles cannot be deleted" — and falls back to the permission
 *    wording only for a bare one. So a body carrying GATE_REFUSAL really does
 *    mean the gate refused, and TC-A-102 and TC-A-120 lean on that.
 *
 * 3. Every subject holds **exactly one** role, via `asRole`. `fixtures.
 *    createApproverUser` cannot be used here: `store.CreateUser` →
 *    `assignDefaultRoleTx` (internal/store/migrations.go:501) gives every new
 *    user the Accounts role, so ticking "Manager" yields Accounts + Manager and
 *    every refusal under test turns into a 200.
 *
 * 4. **A row outside the caller's data scope answers 404, not 403.** This is why
 *    so many cells below expect a status that reads like "missing" on an object
 *    the fixture certainly created. `loadViewableRequest` (requests.go:335-348),
 *    `notFoundAttachment` (app.go:1054-1062) and `requestAttachmentDownload` all
 *    refuse that way on purpose: request and attachment ids are consecutive
 *    integers, and a 403 that can be told apart from a 404 lets a caller who may
 *    read nothing still map everything the system holds (F-G-002). The rule is
 *    therefore that the ROUTE IS NOT AN EXISTENCE ORACLE, and 404 here is a
 *    stronger refusal than 403, never a weaker one. Ownership rules layered
 *    ABOVE the scope — "only the raiser may edit", "only the person holding this
 *    reservation" — still answer 403 with their own sentence, because by then
 *    the caller has already been told the row exists.
 *
 * Row IDs `RT-nn` and caller IDs `C-xxx` are the ones in
 * docs/qa/uml/05-route-permission-matrix.md. TC IDs are in
 * docs/qa/test-cases/TC-A-rbac-permissions.md.
 */
import { test, expect, admin, login } from './fixtures';
import {
  asRole,
  createUserWithExactRoles,
  setExactRoles,
  signIn,
  csrfToken,
  probeGet,
  probePost,
  fixturePassword,
  type Probe,
  type RoleName,
  type Subject
} from './audit-support';
import type { Browser, BrowserContext, Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// The five callers of the matrix, plus the two probes that sharpen it.
// ---------------------------------------------------------------------------

type CallerId = 'C-anon' | 'C-req' | 'C-mgr' | 'C-acc' | 'C-adm' | 'C-none';

/**
 * The seeded grants, transcribed from `systemRoleDefaults`
 * (internal/store/migrations.go:351-402). This table is the *expectation*: if
 * the seed ever changes, these tests must fail rather than quietly re-baseline.
 */
const SEEDED_GRANTS: Record<Exclude<CallerId, 'C-anon'>, string[]> = {
  'C-req': [
    'request:view', 'request:create', 'request:edit', 'request:withdraw',
    'request:reraise', 'request:comment', 'request:cancel',
    'attachment:view', 'attachment:create'
  ],
  'C-mgr': [
    'request:view', 'request:comment',
    'approval:approve', 'approval:reject', 'approval:return',
    'approval:reassign', 'approval:accept_partial', 'approval:cancel',
    'grid:view', 'report:view',
    // Granted by migration 12. Approving is the one moment somebody is asked to
    // judge whether a payment is justified, and it is the moment the supporting
    // document matters most; the approver could not open it. The grant is still
    // scoped — the download handlers check the caller can see the request the
    // document belongs to before serving a byte.
    'attachment:view'
  ],
  'C-acc': [
    'request:view', 'request:comment',
    'payment:view', 'payment:create', 'payment:edit', 'payment:void',
    'payment:process', 'payment:settle', 'payment:mark_partial', 'payment:hold',
    'reservation:reserve', 'reservation:release',
    'attachment:view', 'attachment:create',
    'grid:view', 'report:view', 'report:export',
    'recoverable_report:view', 'recoverable_report:export'
  ],
  // Admin holds adminGrants() — every one of the 66 canonical pairs.
  'C-adm': ['*'],
  'C-none': []
};

/** The canonical vocabulary, transcribed from `resourceActions` (permissions.go:150-172). */
const VOCABULARY: Record<string, string[]> = {
  request: ['view', 'create', 'edit', 'withdraw', 'reraise', 'comment', 'cancel'],
  approval: ['approve', 'reject', 'return', 'reassign', 'accept_partial', 'cancel'],
  payment: ['view', 'create', 'edit', 'void', 'process', 'settle', 'mark_partial', 'hold'],
  reservation: ['reserve', 'release', 'reassign'],
  attachment: ['view', 'create'],
  vendor: ['view', 'create', 'edit'],
  vendor_bank: ['view', 'edit'],
  project: ['view', 'create', 'edit'],
  head: ['view', 'create', 'edit'],
  budget: ['view', 'edit'],
  month: ['view', 'create', 'lock'],
  grid: ['view', 'export'],
  report: ['view', 'export'],
  recoverable_category: ['view', 'create', 'edit', 'delete'],
  recoverable_report: ['view', 'export'],
  user: ['view', 'create', 'edit'],
  role: ['view', 'create', 'edit', 'delete'],
  notification: ['view', 'edit'],
  config: ['view', 'edit'],
  audit: ['view'],
  backup: ['view', 'create']
};

function holdsGrant(caller: CallerId, gate: string): boolean {
  if (caller === 'C-anon') return false;
  const grants = SEEDED_GRANTS[caller];
  return grants.includes('*') || grants.includes(gate);
}

// ---------------------------------------------------------------------------
// Expectations. `gate403` and `csrf403` are two different 403s and the whole
// POST matrix turns on telling them apart.
// ---------------------------------------------------------------------------

const GATE_REFUSAL = 'You do not have permission to perform this action.';
const CSRF_REFUSAL = 'Your form session expired.';

type Expected = number | 'login' | 'gate403' | 'csrf403';

interface Row {
  /** RT-nn from docs/qa/uml/05-route-permission-matrix.md */
  id: string;
  method: 'GET' | 'POST';
  /** Resolved lazily: most paths carry an id created in beforeAll. */
  path: () => string;
  /** The route's own gate, or 'login' / 'public'. */
  gate: string;
  /** Per-caller overrides where an ownership rule refuses a permitted caller. */
  over?: Partial<Record<CallerId, Expected>>;
  note?: string;
}

function expectedFor(row: Row, caller: CallerId): Expected {
  if (row.over && row.over[caller] !== undefined) return row.over[caller]!;
  if (row.gate === 'public') return 200;
  if (caller === 'C-anon') return 'login';
  if (row.gate === 'login' || holdsGrant(caller, row.gate)) {
    return row.method === 'POST' ? 'csrf403' : 200;
  }
  return 'gate403';
}

function describeExpected(e: Expected): string {
  if (e === 'login') return '303 → /login';
  if (e === 'gate403') return '403 (permission refused)';
  if (e === 'csrf403') return '403 (gate opened, CSRF refused)';
  return String(e);
}

/** Returns null when the cell matches, or a human sentence naming what went wrong. */
function cellFailure(probe: Probe, expected: Expected): string | null {
  if (expected === 'login') {
    if (probe.status < 300 || probe.status >= 400) return `got ${probe.outcome}`;
    if (!(probe.location ?? '').includes('/login')) return `redirected to ${probe.location} instead of /login`;
    return null;
  }
  if (expected === 'gate403') {
    if (probe.status !== 403) return `got ${probe.outcome}`;
    if (!probe.body.includes(GATE_REFUSAL)) return `403 but not the permission refusal — body says ${firstError(probe.body)}`;
    return null;
  }
  if (expected === 'csrf403') {
    if (probe.status !== 403) return `got ${probe.outcome}`;
    if (probe.body.includes(GATE_REFUSAL)) return `403 from the PERMISSION gate — the grant was not honoured`;
    if (!probe.body.includes(CSRF_REFUSAL)) return `403 but not the CSRF refusal — body says ${firstError(probe.body)}`;
    return null;
  }
  return probe.status === expected ? null : `got ${probe.outcome}`;
}

/** Pulls the message out of the rendered error page so a failure reads usefully. */
function firstError(body: string): string {
  const m = /<p class="e-msg">([^<]*)<\/p>/.exec(body) ?? /class="alert error"[^>]*>([^<]*)</.exec(body);
  return m ? `"${m[1].trim()}"` : `"${body.replace(/\s+/g, ' ').slice(0, 120)}"`;
}

// ---------------------------------------------------------------------------
// The world. Built once: five callers, three foils, and the real entities every
// id-parameterised route needs.
// ---------------------------------------------------------------------------

interface Session {
  subject: Subject;
  page: Page;
  context: BrowserContext;
  close: () => Promise<void>;
}

interface World {
  runId: string;
  baseURL: string;
  today: string;
  adminPage: Page;
  adminClose: () => Promise<void>;
  /** No cookies at all — C-anon. */
  anonPage: Page;
  callers: Record<Exclude<CallerId, 'C-anon'>, Session>;
  /** The foils: the people who own the objects the callers probe. */
  r2: Session;
  m2: Session;
  a2: Session;
  projectId: string;
  headId: string;
  vendorId: string;
  customRoleId: string;
  systemRoleIds: Record<RoleName, string>;
  /** Objects the five callers do NOT own — matrix targets (matrix doc §0.3). */
  tPending: number;
  tApproved: number;
  tProc: number;
  tPaid: number;
  tPartial: number;
  payId: number;
  payAttId: number;
  foreignTitle: string;
  /** Objects C-req DOES own — ownership and data-scope targets. */
  ownPending: number;
  ownApproved: number;
  ownTitle: string;
  ownReqAttId: number;
}

let W: World;
let worldError: string | null = null;

test.describe('TC-A — RBAC and permission enforcement', () => {
  /**
   * The project guard and the world build, both in `beforeEach`, in that order.
   *
   * The guard has to be the PROJECT name, not `browserName`: Pixel 5 is chromium
   * too, so a browserName check would run the whole matrix twice. And the build
   * cannot live in `beforeAll`, because a `beforeAll` runs before any per-test
   * skip is evaluated — it would build a second world at 390px, where the edit
   * screen's sticky action bar covers its own submit button. Skipping first and
   * building lazily gets both right, and `workers: 1` means it happens once.
   */
  test.beforeEach(async ({ browser, baseURL }, testInfo) => {
    testInfo.skip(testInfo.project.name !== 'chromium', 'the permission matrix runs once, on desktop');
    if (W || worldError) {
      expect(worldError, 'the fixture world must be built before any cell is probed').toBeNull();
      return;
    }
    // The build raises seven users and a dozen entities through real requests;
    // it belongs to the first test's budget, not to the default 30 s.
    testInfo.setTimeout(240_000);
    try {
      W = await buildWorld(browser, baseURL!);
    } catch (error) {
      worldError = error instanceof Error ? `${error.message}\n${error.stack}` : String(error);
      throw error;
    }
  });

  test.afterAll(async () => {
    if (!W) return;
    await Promise.all([
      W.adminClose(),
      W.anonPage.context().close(),
      ...Object.values(W.callers).map(c => c.close()),
      W.r2.close(),
      W.m2.close(),
      W.a2.close()
    ]);
  });

  // =========================================================================
  // 1. The expected-status matrix: every registered route × five callers.
  //
  // One test per distinct route gate. Every route carrying that gate is probed
  // by every caller, and each cell carries its own soft assertion, so a single
  // failure names a single cell rather than collapsing the whole matrix.
  // =========================================================================

  matrixTest('TC-A-M01', 'RequireLogin only — session, no verb', () => [
    { id: 'RT-05', method: 'GET', path: () => '/', gate: 'login' },
    { id: 'RT-08', method: 'GET', path: () => '/dashboard', gate: 'login' },
    // RT-07 used to sit here, and that was the finding: the nav gated the
    // variance grid on grid:view while the route was RequireLogin only, so the
    // screen the sidebar hid opened to anybody with a session (F-A-02). The
    // route now carries the verb the nav always claimed for it
    // (app.go:439), so the row is no longer a session-only row — it is
    // TC-A-M58 below.
    { id: 'RT-30', method: 'GET', path: () => '/notifications', gate: 'login' },
    { id: 'RT-31', method: 'POST', path: () => '/notifications/read', gate: 'login' },
    // The catch-all: RequireLogin fires before notFound, so a 404 probe needs a
    // session (matrix doc MX-2/MX-3).
    {
      id: 'RT-06', method: 'GET', path: () => `/no-such-screen-${W.runId}`, gate: 'login',
      over: { 'C-req': 404, 'C-mgr': 404, 'C-acc': 404, 'C-adm': 404, 'C-none': 404 }
    },
    // Somebody else's notification is indistinguishable from a missing one
    // (inapp.go:76-81), which is exactly right.
    {
      id: 'RT-32', method: 'GET', path: () => '/notifications/999999/open', gate: 'login',
      over: { 'C-req': 404, 'C-mgr': 404, 'C-acc': 404, 'C-adm': 404, 'C-none': 404 }
    },
    // RT-60 is gated on a session, then on the request's data scope, then on
    // ownership: release-if-yours OR reassign (`reservationForm`,
    // internal/app/linking.go). A2 holds the reservation, so of the five callers
    // only the Admin — the only holder of reservation:reassign — opens it.
    //
    // The Requester answers 404, not 403, and the order is why: the screen loads
    // through `loadViewableRequest` (requests.go:335-348), and a request outside
    // the caller's data scope is not-found rather than forbidden, so the route
    // cannot be used to discover which ids exist (F-G-002). A Requester whose
    // scope is `own` never reaches the reservation rule at all.
    {
      id: 'RT-60', method: 'GET', path: () => `/requests/${W.tProc}/reservation`, gate: 'login',
      over: { 'C-req': 404, 'C-mgr': 403, 'C-acc': 403, 'C-adm': 200, 'C-none': 404 }
    }
  ]);

  matrixTest('TC-A-M02', 'public — no gate at all', () => [
    { id: 'RT-01', method: 'GET', path: () => '/static/fervid-ds.css', gate: 'public' },
    { id: 'RT-02', method: 'GET', path: () => '/login', gate: 'public' }
  ]);

  matrixTest('TC-A-M03', 'month:view', () => [{ id: 'RT-09', method: 'GET', path: () => '/months', gate: 'month:view' }]);
  matrixTest('TC-A-M04', 'month:create', () => [{ id: 'RT-10', method: 'POST', path: () => '/months', gate: 'month:create' }]);
  matrixTest('TC-A-M05', 'month:lock', () => [
    { id: 'RT-39', method: 'POST', path: () => '/months/2099-01/lock', gate: 'month:lock' },
    { id: 'RT-40', method: 'POST', path: () => '/months/2099-01/unlock', gate: 'month:lock' }
  ]);

  matrixTest('TC-A-M06', 'payment:create', () => [
    { id: 'RT-11', method: 'GET', path: () => '/payments/new', gate: 'payment:create' },
    { id: 'RT-12', method: 'GET', path: () => '/payments/new/options', gate: 'payment:create' },
    { id: 'RT-14', method: 'POST', path: () => '/payments', gate: 'payment:create' }
  ]);
  matrixTest('TC-A-M07', 'payment:view', () => [
    { id: 'RT-13', method: 'GET', path: () => '/payments', gate: 'payment:view' },
    { id: 'RT-15', method: 'GET', path: () => `/payments/${W.payId}`, gate: 'payment:view' }
  ]);
  matrixTest('TC-A-M08', 'payment:edit', () => [
    // S12: a linked payment is immutable, so the form redirects to the payment
    // rather than offering a Save that can only fail (app.go:803-806).
    {
      id: 'RT-16', method: 'GET', path: () => `/payments/${W.payId}/edit`, gate: 'payment:edit',
      over: { 'C-acc': 303, 'C-adm': 303 }
    },
    { id: 'RT-17', method: 'POST', path: () => `/payments/${W.payId}/edit`, gate: 'payment:edit' }
  ]);
  matrixTest('TC-A-M09', 'payment:void', () => [
    { id: 'RT-18', method: 'POST', path: () => `/payments/${W.payId}/void`, gate: 'payment:void' }
  ]);
  matrixTest('TC-A-M10', 'payment:process', () => [
    { id: 'RT-66', method: 'GET', path: () => '/accounts-queue', gate: 'payment:process' },
    { id: 'RT-63', method: 'GET', path: () => `/requests/${W.tProc}/reservation/stale`, gate: 'payment:process' }
  ]);
  matrixTest('TC-A-M11', 'payment:settle', () => [
    { id: 'RT-59', method: 'POST', path: () => `/requests/${W.tProc}/settlement-preview`, gate: 'payment:settle' }
  ]);
  matrixTest('TC-A-M12', 'payment:hold', () => [
    { id: 'RT-64', method: 'POST', path: () => `/requests/${W.tApproved}/hold`, gate: 'payment:hold' },
    { id: 'RT-65', method: 'POST', path: () => `/requests/${W.tApproved}/unhold`, gate: 'payment:hold' }
  ]);

  matrixTest('TC-A-M13', 'attachment:create', () => [
    { id: 'RT-19', method: 'POST', path: () => `/payments/${W.payId}/attachments`, gate: 'attachment:create' }
  ]);
  // RT-20 serves *payment* documents and nothing else, and it resolves the
  // attachment to its payment before serving it: `canReadPayment`
  // (app.go:1028-1039) applies the request's own row scope, which is the same
  // rule `paymentDetail` runs — "or payment:view becomes a way around Q5/R6".
  // A Requester holds attachment:view, so the gate opens and the ownership check
  // is the thing doing the work: the bank advice on somebody else's request is
  // refused. It is refused as **404**, deliberately: attachment ids are small
  // sequential integers and a distinguishable refusal turns the route into an
  // enumeration oracle over the whole table (`notFoundAttachment`,
  // app.go:1054-1062). F-A-01.
  matrixTest('TC-A-M14', 'attachment:view', () => [
    {
      id: 'RT-20', method: 'GET', path: () => `/attachments/${W.payAttId}`, gate: 'attachment:view',
      over: { 'C-req': 404 }
    }
  ]);

  matrixTest('TC-A-M15', 'grid:export', () => [
    { id: 'RT-21', method: 'GET', path: () => '/export.csv', gate: 'grid:export' }
  ]);
  matrixTest('TC-A-M16', 'report:view', () => [
    { id: 'RT-22', method: 'GET', path: () => '/reports/monthly', gate: 'report:view' },
    { id: 'RT-23', method: 'GET', path: () => '/reports/projects', gate: 'report:view' },
    { id: 'RT-24', method: 'GET', path: () => '/reports/heads', gate: 'report:view' }
  ]);
  matrixTest('TC-A-M17', 'report:export', () => [
    { id: 'RT-25', method: 'GET', path: () => '/reports/ytd.csv', gate: 'report:export' }
  ]);
  matrixTest('TC-A-M18', 'recoverable_report:view', () => [
    { id: 'RT-26', method: 'GET', path: () => '/recoverables', gate: 'recoverable_report:view' },
    { id: 'RT-27', method: 'GET', path: () => '/recoverables/list', gate: 'recoverable_report:view' },
    // No recoverable request exists in this fixture, so a permitted caller sees
    // the store's 404 — which still proves the gate opened.
    {
      id: 'RT-29', method: 'GET', path: () => '/recoverables/999999', gate: 'recoverable_report:view',
      over: { 'C-acc': 404, 'C-adm': 404 }
    }
  ]);
  matrixTest('TC-A-M19', 'recoverable_report:export', () => [
    { id: 'RT-28', method: 'GET', path: () => '/recoverables/list.csv', gate: 'recoverable_report:export' }
  ]);
  matrixTest('TC-A-M20', 'recoverable_category:edit', () => [
    { id: 'RT-94', method: 'POST', path: () => '/configuration/recoverable-categories', gate: 'recoverable_category:edit' }
  ]);

  matrixTest('TC-A-M21', 'notification:view', () => [
    { id: 'RT-33', method: 'GET', path: () => '/admin/notifications', gate: 'notification:view' }
  ]);
  matrixTest('TC-A-M22', 'notification:edit', () => [
    { id: 'RT-34', method: 'POST', path: () => '/admin/notifications/smtp', gate: 'notification:edit' },
    { id: 'RT-35', method: 'POST', path: () => '/admin/notifications/events/request_submitted', gate: 'notification:edit' },
    { id: 'RT-36', method: 'POST', path: () => '/admin/notifications/test', gate: 'notification:edit' }
  ]);

  matrixTest('TC-A-M23', 'budget:view', () => [{ id: 'RT-37', method: 'GET', path: () => '/budgets', gate: 'budget:view' }]);
  matrixTest('TC-A-M24', 'budget:edit', () => [{ id: 'RT-38', method: 'POST', path: () => '/budgets', gate: 'budget:edit' }]);
  matrixTest('TC-A-M25', 'project:view', () => [{ id: 'RT-41', method: 'GET', path: () => '/projects', gate: 'project:view' }]);
  matrixTest('TC-A-M26', 'project:edit', () => [{ id: 'RT-42', method: 'POST', path: () => '/projects', gate: 'project:edit' }]);
  matrixTest('TC-A-M27', 'head:view', () => [{ id: 'RT-43', method: 'GET', path: () => '/heads', gate: 'head:view' }]);
  matrixTest('TC-A-M28', 'head:edit', () => [{ id: 'RT-44', method: 'POST', path: () => '/heads', gate: 'head:edit' }]);

  matrixTest('TC-A-M29', 'vendor:view', () => [
    { id: 'RT-45', method: 'GET', path: () => '/vendors', gate: 'vendor:view' },
    { id: 'RT-47', method: 'GET', path: () => '/vendors/search?q=a', gate: 'vendor:view' },
    { id: 'RT-48', method: 'GET', path: () => `/vendors/${W.vendorId}`, gate: 'vendor:view' }
  ]);
  matrixTest('TC-A-M30', 'vendor:create', () => [
    { id: 'RT-46', method: 'GET', path: () => '/vendors/new', gate: 'vendor:create' },
    { id: 'RT-49', method: 'POST', path: () => '/vendors', gate: 'vendor:create' }
  ]);
  matrixTest('TC-A-M31', 'vendor:edit', () => [
    { id: 'RT-50', method: 'POST', path: () => `/vendors/${W.vendorId}`, gate: 'vendor:edit' }
  ]);

  matrixTest('TC-A-M32', 'request:view', () => [
    { id: 'RT-51', method: 'GET', path: () => '/requests', gate: 'request:view' },
    { id: 'RT-52', method: 'GET', path: () => '/requests/export.csv', gate: 'request:view' },
    // Every id row below targets a request C-req did not raise: scope own then
    // refuses it at `loadViewableRequest` (requests.go:335-348) even though the
    // verb is held. That is Q5/R6.
    //
    // The refusal is **404**, not 403. Holding request:view and being told
    // "forbidden" is an answer — it says the row exists — and these ids are
    // consecutive integers, so a caller could map every request in the system
    // without reading one. A row outside the data scope now answers exactly as a
    // row that was never written does (F-G-002).
    {
      id: 'RT-70', method: 'GET', path: () => `/requests/${W.tPending}`, gate: 'request:view',
      over: { 'C-req': 404 }
    },
    {
      id: 'RT-57', method: 'GET', path: () => `/requests/${W.tPending}/submitted`, gate: 'request:view',
      over: { 'C-req': 404 }
    },
    {
      id: 'RT-67', method: 'GET', path: () => `/requests/${W.tPartial}/partial-review`, gate: 'request:view',
      over: { 'C-req': 404 }
    }
  ]);
  matrixTest('TC-A-M33', 'request:create', () => [
    { id: 'RT-53', method: 'GET', path: () => '/requests/new', gate: 'request:create' },
    { id: 'RT-54', method: 'GET', path: () => '/requests/new/fields?type=reimbursement', gate: 'request:create' },
    { id: 'RT-55', method: 'POST', path: () => '/requests/duplicate-check', gate: 'request:create' },
    { id: 'RT-56', method: 'POST', path: () => '/requests', gate: 'request:create' }
  ]);
  matrixTest('TC-A-M34', 'request:edit', () => [
    // Only the raiser may edit (`loadEditableRequest`, requests.go:739-752), so
    // both holders of the verb are refused on a request neither raised — and
    // they are refused differently, which is the point of listing both.
    // `loadEditableRequest` calls `loadViewableRequest` first: the Requester's
    // scope is `own`, so the row is 404 before "only the raiser may edit" is
    // ever asked. The administrator's scope is `all`, so the row resolves and
    // the ownership rule is what refuses them — 403, but never the gate's 403.
    {
      id: 'RT-71', method: 'GET', path: () => `/requests/${W.tPending}/edit`, gate: 'request:edit',
      over: { 'C-req': 404, 'C-adm': 403 }
    },
    { id: 'RT-72', method: 'POST', path: () => `/requests/${W.tPending}/edit`, gate: 'request:edit' }
  ]);
  matrixTest('TC-A-M35', 'request:comment', () => [
    { id: 'RT-73', method: 'POST', path: () => `/requests/${W.tPending}/comment`, gate: 'request:comment' }
  ]);
  matrixTest('TC-A-M36', 'request:withdraw', () => [
    { id: 'RT-74', method: 'POST', path: () => `/requests/${W.tPending}/withdraw`, gate: 'request:withdraw' }
  ]);
  matrixTest('TC-A-M37', 'request:reraise', () => [
    { id: 'RT-75', method: 'POST', path: () => `/requests/${W.tPending}/reraise`, gate: 'request:reraise' }
  ]);
  matrixTest('TC-A-M38', 'request:cancel', () => [
    // Asking for a cancellation is the raiser's act, so an administrator holding
    // every verb is still refused here (`requestCancelForm`,
    // requests.go:1005-1023) — 403, and its own sentence rather than the gate's.
    // The Requester is 404 for the same reason RT-71 is: the screen loads
    // through `loadViewableRequest`, and a request outside the data scope is
    // not-found before any ownership rule is consulted.
    {
      id: 'RT-79', method: 'GET', path: () => `/requests/${W.tApproved}/cancel`, gate: 'request:cancel',
      over: { 'C-req': 404, 'C-adm': 403 }
    },
    { id: 'RT-80', method: 'POST', path: () => `/requests/${W.tApproved}/cancel-request`, gate: 'request:cancel' }
  ]);

  matrixTest('TC-A-M39', 'approval:approve', () => [
    { id: 'RT-84', method: 'GET', path: () => '/approvals', gate: 'approval:approve' },
    { id: 'RT-76', method: 'POST', path: () => `/requests/${W.tPending}/approve`, gate: 'approval:approve' }
  ]);
  matrixTest('TC-A-M40', 'approval:return', () => [
    { id: 'RT-77', method: 'POST', path: () => `/requests/${W.tPending}/return`, gate: 'approval:return' }
  ]);
  matrixTest('TC-A-M41', 'approval:reject', () => [
    { id: 'RT-78', method: 'POST', path: () => `/requests/${W.tPending}/reject`, gate: 'approval:reject' }
  ]);
  matrixTest('TC-A-M42', 'approval:cancel', () => [
    { id: 'RT-81', method: 'POST', path: () => `/requests/${W.tApproved}/cancel`, gate: 'approval:cancel' },
    // The cancellation decision belongs to the request's own approver
    // (requests.go:837-841): M2 is the manager, so C-mgr and C-adm are refused.
    {
      id: 'RT-82', method: 'GET', path: () => `/requests/${W.tApproved}/cancellation`, gate: 'approval:cancel',
      over: { 'C-mgr': 403, 'C-adm': 403 }
    },
    { id: 'RT-83', method: 'POST', path: () => `/requests/${W.tApproved}/cancellation`, gate: 'approval:cancel' }
  ]);
  matrixTest('TC-A-M43', 'approval:accept_partial', () => [
    { id: 'RT-68', method: 'POST', path: () => `/requests/${W.tPartial}/accept-partial`, gate: 'approval:accept_partial' },
    { id: 'RT-69', method: 'POST', path: () => `/requests/${W.tPartial}/raise-concern`, gate: 'approval:accept_partial' }
  ]);

  matrixTest('TC-A-M44', 'reservation:reserve', () => [
    { id: 'RT-58', method: 'POST', path: () => `/requests/${W.tApproved}/record-payment`, gate: 'reservation:reserve' }
  ]);
  matrixTest('TC-A-M45', 'reservation:release', () => [
    { id: 'RT-61', method: 'POST', path: () => `/requests/${W.tProc}/release`, gate: 'reservation:release' }
  ]);
  // reservation:reassign is held by Admin alone (migrations.go:386-388).
  matrixTest('TC-A-M46', 'reservation:reassign', () => [
    { id: 'RT-62', method: 'POST', path: () => `/requests/${W.tProc}/reassign`, gate: 'reservation:reassign' }
  ]);

  matrixTest('TC-A-M47', 'user:view', () => [{ id: 'RT-85', method: 'GET', path: () => '/users', gate: 'user:view' }]);
  matrixTest('TC-A-M48', 'user:edit', () => [{ id: 'RT-86', method: 'POST', path: () => '/users', gate: 'user:edit' }]);
  matrixTest('TC-A-M49', 'role:view', () => [{ id: 'RT-87', method: 'GET', path: () => '/roles', gate: 'role:view' }]);
  matrixTest('TC-A-M50', 'role:edit', () => [{ id: 'RT-88', method: 'POST', path: () => '/roles', gate: 'role:edit' }]);
  matrixTest('TC-A-M51', 'role:create', () => [
    { id: 'RT-89', method: 'POST', path: () => '/roles/new', gate: 'role:create' },
    { id: 'RT-90', method: 'POST', path: () => `/roles/${W.customRoleId}/copy`, gate: 'role:create' }
  ]);
  matrixTest('TC-A-M52', 'role:delete', () => [
    { id: 'RT-91', method: 'POST', path: () => `/roles/${W.customRoleId}/delete`, gate: 'role:delete' }
  ]);
  matrixTest('TC-A-M53', 'config:view', () => [
    { id: 'RT-92', method: 'GET', path: () => '/configuration', gate: 'config:view' }
  ]);
  matrixTest('TC-A-M54', 'config:edit', () => [
    { id: 'RT-93', method: 'POST', path: () => '/configuration', gate: 'config:edit' }
  ]);
  matrixTest('TC-A-M55', 'audit:view', () => [{ id: 'RT-95', method: 'GET', path: () => '/audit', gate: 'audit:view' }]);
  matrixTest('TC-A-M56', 'backup:view', () => [{ id: 'RT-96', method: 'GET', path: () => '/backups', gate: 'backup:view' }]);
  matrixTest('TC-A-M57', 'backup:create', () => [
    { id: 'RT-97', method: 'POST', path: () => '/backups', gate: 'backup:create' }
  ]);

  // A row and an ID that did not exist at the audit, because the gate did not.
  // RT-07 was probed under TC-A-M01 as a RequireLogin row and that WAS the
  // finding — the sidebar gated the variance grid on grid:view and the route
  // gated it on nothing, so a Requester who typed the URL read every budget,
  // every actual, and a Recent Payments table (F-A-02/F-G-032). The route now
  // names the verb (`RequirePermission("grid","view")`, app.go:439), which makes
  // it an ordinary matrix row for the first time: Manager, Accounts and Admin
  // hold grid:view and open it, Requester does not and is refused by the gate.
  matrixTest('TC-A-M58', 'grid:view', () => [
    { id: 'RT-07', method: 'GET', path: () => '/grid', gate: 'grid:view' }
  ]);

  // =========================================================================
  // 2. Menu hiding is not enforcement — and the converse.
  // =========================================================================

  /** The sidebar contract: every navigable screen and the verb nav.go gates it on. */
  const NAV = [
    { label: 'My requests', href: '/requests', gate: 'request:view' },
    { label: 'Approvals', href: '/approvals', gate: 'approval:approve' },
    { label: 'Accounts queue', href: '/accounts-queue', gate: 'payment:process' },
    { label: 'Recoverables', href: '/recoverables', gate: 'recoverable_report:view' },
    { label: 'Payments ledger', href: '/payments', gate: 'payment:view' },
    { label: 'Variance grid', href: '/grid', gate: 'grid:view' },
    { label: 'Budgets', href: '/budgets', gate: 'budget:view' },
    { label: 'Monthly plans', href: '/months', gate: 'month:view' },
    { label: 'Reports', href: '/reports/monthly', gate: 'report:view' },
    { label: 'Vendors', href: '/vendors', gate: 'vendor:view' },
    { label: 'Projects', href: '/projects', gate: 'project:view' },
    { label: 'Heads', href: '/heads', gate: 'head:view' },
    { label: 'Users', href: '/users', gate: 'user:view' },
    { label: 'Roles & permissions', href: '/roles', gate: 'role:view' },
    { label: 'Configuration', href: '/configuration', gate: 'config:view' },
    { label: 'Notification rules', href: '/admin/notifications', gate: 'notification:view' },
    { label: 'Audit log', href: '/audit', gate: 'audit:view' },
    { label: 'Backups', href: '/backups', gate: 'backup:view' }
  ] as const;

  for (const caller of ['C-req', 'C-mgr', 'C-acc', 'C-adm', 'C-none'] as const) {
    const n = { 'C-req': '58', 'C-mgr': '59', 'C-acc': '60', 'C-adm': '61', 'C-none': '62' }[caller];
    test(`TC-A-${n} — ${caller}: every screen its sidebar hides, the route also refuses`, async () => {
      const session = W.callers[caller];
      await session.page.goto('/');
      for (const item of NAV) {
        const offered = (await session.page.locator(`.sidebar a[href="${item.href}"]`).count()) > 0;
        const probe = await probeGet(session.page, item.href);
        if (offered) {
          expect
            .soft(probe.status, `${caller} is offered "${item.label}" in the sidebar, so ${item.href} must answer 200`)
            .toBe(200);
        } else {
          // No exceptions. /grid used to be one — the sidebar hid it and the
          // route opened it to any session — and the exception is gone because
          // the hole is (F-A-02, app.go:439). Every screen the sidebar withholds
          // is now a screen the route withholds, with no route allowed to be
          // more generous than the menu that advertises it.
          expect
            .soft(
              probe.status,
              `${caller}'s sidebar hides "${item.label}", so ${item.href} must refuse — got ${probe.outcome}`
            )
            .toBe(403);
        }
      }
    });
  }

  test('TC-A-63 — the variance grid is hidden from a Requester and refused to them too', async () => {
    // What this protects, in two halves. The grid is a budget screen carrying a
    // Recent Payments panel — amounts, payees, and live links into
    // /payments/{id} — so it discloses two different things and needs two
    // different verbs. Both were missing (F-A-02/F-G-032):
    //   (a) the route was RequireLogin only while the nav gated it on grid:view,
    //       so the screen the sidebar hid opened to anybody who typed the URL;
    //   (b) the payments panel rode along with the budget matrix, handing a
    //       ledger to a caller /payments answered 403.
    const req = W.callers['C-req'];
    await req.page.goto('/');
    await expect(
      req.page.locator('.sidebar a[href="/grid"]'),
      'a Requester holds no grid:view, so nav.go hides the Variance grid entry'
    ).toHaveCount(0);

    // Half (a). The route names the verb the menu always claimed for it
    // (app.go:439), so the menu and the URL now say the same thing.
    const probe = await probeGet(req.page, '/grid');
    expect(probe.status, 'GET /grid is gated on grid:view, which a Requester does not hold').toBe(403);
    expect(probe.body.includes(GATE_REFUSAL), 'and the refusal comes from the permission gate').toBe(true);
    expect(
      /href="\/payments\/\d+"/.test(probe.body),
      'and nothing of the screen is rendered on the way out — no payment link survives the refusal'
    ).toBe(false);
    const ledger = await probeGet(req.page, '/payments');
    expect(ledger.status, 'while /payments refuses them as it always did — the two screens now agree').toBe(403);

    // The mobile tab bar agrees as well, and it has to: on a phone the bar is
    // the only navigation there is, so a tab that 403s is worse than no tab
    // (resolveTabs, nav.go:262-291).
    await expect(
      req.page.locator('.tabbar a[href="/grid"]'),
      'the tab bar offers no /grid to a caller with no grid:view either'
    ).toHaveCount(0);

    // Half (b): grid:view opens the budget matrix and buys nothing else. The
    // panel is filtered in the handler (app.go:775-784) rather than hidden in
    // the template, because a screen must not be trusted to withhold data the
    // handler already loaded.
    const roleName = `gridonly-${W.runId}`;
    const roleId = await createCustomRole(W.adminPage, roleName);
    await grantMatrix(W.adminPage, roleId, roleName, { perms: ['grid:view'] });
    const subject = await createUserWithExactRoles(W.adminPage, `gridonly-${W.runId}`, W.runId, []);
    await setRolesByLabel(W.adminPage, subject.email, [new RegExp(`^${roleName}`)]);
    const session = await signIn(W.adminPage.context().browser()!, subject, W.adminPage.viewportSize());
    try {
      const grid = await probeGet(session.page, `/grid?month=${W.today.slice(0, 7)}`);
      expect(grid.status, 'grid:view alone opens the variance grid').toBe(200);
      expect(grid.body.includes('Variance'), 'and what opens is the grid itself, not a stub').toBe(true);
      expect(
        /href="\/payments\/\d+"/.test(grid.body),
        'but the Recent Payments panel is empty without payment:view — no amount, no payee, no link'
      ).toBe(false);

      // The control, so the emptiness above is the gate and not an empty month:
      // the same panel, the same month, a caller who does hold payment:view.
      const withVerb = await probeGet(W.callers['C-acc'].page, `/grid?month=${W.today.slice(0, 7)}`);
      expect(withVerb.status, 'Accounts holds grid:view too').toBe(200);
      expect(
        withVerb.body.includes(`href="/payments/${W.payId}"`),
        'and holding payment:view is what fills the panel — this month really does have a payment in it'
      ).toBe(true);
    } finally {
      await session.close();
    }
  });

  test('TC-A-64 — a role-less user is offered nothing but the two ungated screens, and refused every other', async () => {
    const none = W.callers['C-none'];
    await none.page.goto('/');
    // Two, not one. `navItemVisible` shows an item with no Resource to everyone
    // (nav.go), and the Notifications entry has none — every row that screen can
    // return is already scoped to the signed-in user by the store, which is why
    // its route needs no verb either. It was added because the .m-topbar bell is
    // display:none on every desktop, so above 860px a user had no way to reach
    // their own notifications at all (F-F-05, nav.go:61). The rule this case
    // pins is unchanged: the sidebar offers exactly the screens the route layer
    // will open, and both of these are RequireLogin-only screens asserted as
    // reachable in the second loop below.
    await expect(
      none.page.locator('.sidebar .side-nav a:not(.soon)'),
      'a role-less user is offered only the screens that need no verb: Home and Notifications'
    ).toHaveCount(2);
    await expect(none.page.locator('.sidebar a[href="/"]'), 'the first is Home').toHaveCount(1);
    await expect(
      none.page.locator('.sidebar a[href="/notifications"]'),
      'the second is the notification centre, which is scoped to the reader by the store'
    ).toHaveCount(1);

    for (const path of ['/requests', '/payments', '/users', '/roles', '/configuration', '/audit', '/approvals',
      '/accounts-queue', '/recoverables', '/budgets', '/months', '/reports/monthly', '/vendors', '/projects',
      '/heads', '/admin/notifications', '/backups', '/export.csv', '/reports/ytd.csv']) {
      const probe = await probeGet(none.page, path);
      expect.soft(probe.status, `${path} must refuse a signed-in user holding no grant at all`).toBe(403);
    }
    // The three the design deliberately leaves open to any session.
    for (const path of ['/', '/dashboard', '/notifications']) {
      const probe = await probeGet(none.page, path);
      expect.soft(probe.status, `${path} is RequireLogin only, so a role-less user still reaches it`).toBe(200);
    }
  });

  // =========================================================================
  // 3. Data scope: own / assigned / all.
  // =========================================================================

  test('TC-A-65 — a Requester sees only their own requests in the list', async () => {
    const req = W.callers['C-req'];
    await req.page.goto('/requests?bucket=all');
    await expect(
      req.page.locator(`a.req-card[href="/requests/${W.ownPending}"]`),
      'scope request=own shows the requester their own request'
    ).toHaveCount(1);
    await expect(
      req.page.locator(`a.req-card[href="/requests/${W.tPending}"]`),
      'and never a request somebody else raised (Q5/R6)'
    ).toHaveCount(0);
  });

  test('TC-A-66 — the request CSV export obeys the same scope as the list', async () => {
    const probe = await probeGet(W.callers['C-req'].page, '/requests/export.csv?bucket=all');
    expect(probe.status, 'a Requester may export their own list (D3)').toBe(200);
    expect(probe.body.includes(W.ownTitle), 'the export carries the requester’s own request').toBe(true);
    expect(
      probe.body.includes(W.foreignTitle),
      'and never another requester’s — an export is a list, and the scope must survive the format change'
    ).toBe(false);
  });

  test('TC-A-67 — a Requester cannot open another requester’s request by id, and cannot tell it exists', async () => {
    const probe = await probeGet(W.callers['C-req'].page, `/requests/${W.tPending}`);
    // Holding request:view somewhere is not permission to read this row — and
    // the refusal must not say which of the two it is. A 403 on a real id and a
    // 404 on an unused one is an existence oracle over consecutive integers, so
    // `loadViewableRequest` answers a row outside the data scope exactly as it
    // answers a row that was never written (requests.go:343-346, F-G-002).
    expect(probe.status, 'a request outside the caller’s data scope is not-found, not forbidden').toBe(404);
    expect(
      probe.body.includes('The requested record was not found'),
      'and the wording gives nothing away either'
    ).toBe(true);

    // The proof that it is an oracle no longer: an id that certainly does not
    // exist answers identically, byte for byte in the part that matters.
    const missing = await probeGet(W.callers['C-req'].page, '/requests/99999999');
    expect(missing.status, 'an id nobody ever raised answers the same status').toBe(probe.status);
    expect(
      missing.body.includes('The requested record was not found'),
      'and the same sentence — so the two cases are indistinguishable to the caller'
    ).toBe(true);
  });

  test('TC-A-68 — a Manager with request=all sees every request but decides only their own', async () => {
    const mgr = W.callers['C-mgr'];
    await mgr.page.goto('/requests?bucket=all');
    await expect(
      mgr.page.locator(`a.req-card[href="/requests/${W.tPending}"]`),
      'scope request=all shows a manager a request routed to a different manager (A5)'
    ).toHaveCount(1);

    // Reading is not deciding: the queue is hard-scoped to "assigned"
    // (requests.go:896), so a request routed elsewhere is not in it.
    await mgr.page.goto('/approvals');
    await expect(
      mgr.page.locator(`a.req-card[href="/requests/${W.tPending}"]`),
      'but /approvals lists only what is theirs to decide'
    ).toHaveCount(0);

    const decide = await probePost(mgr.page, `/requests/${W.tPending}/approve`, { approved_amount: '100.00' });
    expect(decide.status, 'and the decision itself is refused: ManagerID != actor (store/requests.go:687)').toBe(403);
  });

  test('TC-A-69 — Accounts with payment=all sees every payment', async () => {
    const acc = W.callers['C-acc'];
    const probe = await probeGet(acc.page, `/payments/${W.payId}`);
    expect(probe.status, 'payment=all reaches a payment another accountant entered').toBe(200);
    await acc.page.goto(`/payments?month=${W.today.slice(0, 7)}&status=all`);
    await expect(
      acc.page.locator(`a[href="/payments/${W.payId}"]`).first(),
      'and the ledger lists it'
    ).toBeVisible();
  });

  test('TC-A-70 — two roles union to the broadest scope, and the visible row set widens', async () => {
    // A subject of its own: widening the roles of a matrix caller would poison
    // every later cell.
    const subject = await createUserWithExactRoles(W.adminPage, `union-${W.runId}`, W.runId, ['Requester']);
    const session = await signIn(W.adminPage.context().browser()!, subject, W.adminPage.viewportSize());
    try {
      await session.page.goto('/requests?bucket=all');
      await expect(
        session.page.locator(`a.req-card[href="/requests/${W.tPending}"]`),
        'as a Requester alone the subject sees nothing it did not raise'
      ).toHaveCount(0);

      await setExactRoles(W.adminPage, subject.email, ['Requester', 'Manager']);

      await session.page.goto('/requests?bucket=all');
      await expect(
        session.page.locator(`a.req-card[href="/requests/${W.tPending}"]`),
        'holding Requester AND Manager the scope unions to all (R4/R3), so every request is visible'
      ).toHaveCount(1);
    } finally {
      await session.close();
    }
  });

  test('TC-A-71 — a role change takes effect on the very next request, without re-login', async () => {
    const subject = await createUserWithExactRoles(W.adminPage, `live-${W.runId}`, W.runId, []);
    const session = await signIn(W.adminPage.context().browser()!, subject, W.adminPage.viewportSize());
    try {
      const before = await probeGet(session.page, '/users');
      expect(before.status, 'a role-less user is refused the admin screen').toBe(403);

      await setExactRoles(W.adminPage, subject.email, ['Admin']);
      // No new sign-in: the same session cookie, the next request.
      const after = await probeGet(session.page, '/users');
      expect(
        after.status,
        'permsFor queries per request (auth.go:97-105), so a grant added mid-session is live immediately'
      ).toBe(200);

      await setExactRoles(W.adminPage, subject.email, []);
      const revoked = await probeGet(session.page, '/users');
      expect(revoked.status, 'and a revocation is live immediately too — no cached permission set').toBe(403);
    } finally {
      await session.close();
    }
  });

  test('TC-A-72 — the payment data scope is declared, grantable AND enforced', async () => {
    // F-A-04, and the annotation is gone because the defect is. `payment` is in
    // scopedResources (permissions.go:185) and the roles screen offers
    // Own / Assigned / All for it, but nothing ever called Scope(u, "payment") —
    // so a role built as "Payments · Own" received the whole ledger, and the
    // control on the roles screen was decoration. `ListPayments` now takes
    // Scope/ViewerID and filters on `entered_by` (app.go:812-818), and
    // `paymentScopeReaches` (app.go:1042-1049) mirrors that filter exactly so a
    // payment invisible in the ledger is invisible by id too.
    //
    // Only Accounts and Admin hold payment:view, and both carry payment=all, so
    // the rule needs a custom role to see at all. This is the roles screen doing
    // exactly what it offers: payment:view with scope "own".
    const roleName = `payments-own-${W.runId}`;
    const roleId = await createCustomRole(W.adminPage, roleName);
    await grantMatrix(W.adminPage, roleId, roleName, { cells: ['payments:view'], scopes: { payment: 'own' } });

    const subject = await createUserWithExactRoles(W.adminPage, `payown-${W.runId}`, W.runId, []);
    await setRolesByLabel(W.adminPage, subject.email, [new RegExp(`^${roleName}`)]);
    const session = await signIn(W.adminPage.context().browser()!, subject, W.adminPage.viewportSize());
    try {
      const probe = await probeGet(session.page, `/payments?month=${W.today.slice(0, 7)}&status=all`);
      expect(probe.status, 'payment:view opens the ledger').toBe(200);
      // The rule: scope=own narrows the ledger to the holder's own payments —
      // and this holder has entered none, so the ledger is empty of A2's.
      expect(
        probe.body.includes(`/payments/${W.payId}`),
        'scope payment=own hides a payment this user did not enter (R3) — F-A-04'
      ).toBe(false);
    } finally {
      await session.close();
    }
  });

  // =========================================================================
  // 4. Ownership checks layered above the route gate.
  // =========================================================================

  test('TC-A-73 — G8: nobody approves their own request', async () => {
    // A subject holding Requester AND Manager is the only way to even reach the
    // attempt: it may raise, and it may approve.
    const subject = await createUserWithExactRoles(W.adminPage, `selfapp-${W.runId}`, W.runId, ['Requester', 'Manager']);
    const session = await signIn(W.adminPage.context().browser()!, subject, W.adminPage.viewportSize());
    try {
      const selfId = await userIdOf(W.adminPage, subject.email);
      // First refusal: the form will not accept the requester as approver.
      const raiseSelf = await probePost(session.page, '/requests', requestForm({
        title: `Self ${W.runId}`, managerId: selfId, amount: '110.00'
      }));
      expect(raiseSelf.status, 'validateRequestInput refuses a self-approver outright (store/requests.go:142)').toBe(400);
      expect(raiseSelf.body.includes('cannot approve your own request'), 'and says why').toBe(true);

      // Second refusal: routed to a real manager, the raiser still cannot decide it.
      const raised = await raiseRequest(session.page, {
        title: `SelfLater ${W.runId}`, managerId: await userIdOf(W.adminPage, W.m2.subject.email), amount: '111.00'
      });
      const approve = await probePost(session.page, `/requests/${raised}/approve`, { approved_amount: '111.00' });
      expect(approve.status, 'and ApproveRequest is the last line of defence for G8').toBe(403);
    } finally {
      await session.close();
    }
  });

  test('TC-A-74 — a Manager cannot approve a request assigned to a different manager', async () => {
    const probe = await probePost(W.callers['C-mgr'].page, `/requests/${W.tPending}/approve`, {
      approved_amount: '100.00'
    });
    expect(probe.status, 'approval:approve says you may decide; manager_id says which requests are yours').toBe(403);
    // Reproduced a second way: the request is not in this manager's queue either.
    await W.callers['C-mgr'].page.goto('/approvals');
    await expect(
      W.callers['C-mgr'].page.locator(`a.req-card[href="/requests/${W.tPending}"]`),
      'and the queue never offered it'
    ).toHaveCount(0);
  });

  test('TC-A-75 — the partial-review decision belongs to the request’s own manager', async () => {
    // The manager it was routed to may decide.
    const owner = await probePost(W.m2.page, `/requests/${W.tPartial}/raise-concern`, {
      comment: `Shortfall queried ${W.runId}`
    });
    expect(owner.status, 'the request’s own manager may raise a concern on it').toBe(303);

    // An administrator holds approval:accept_partial and every other grant, and
    // is still refused: AcceptPartial checks ManagerID != actor (store.go:980).
    const adminAttempt = await probePost(W.callers['C-adm'].page, `/requests/${W.tPartial}/accept-partial`, {
      note: 'Written off by an administrator'
    });
    expect(
      adminAttempt.status,
      'holding approval:accept_partial says a person may accept a shortfall, never whose'
    ).toBe(403);

    const otherManager = await probePost(W.callers['C-mgr'].page, `/requests/${W.tPartial}/accept-partial`, {
      note: 'Written off by a bystander'
    });
    expect(otherManager.status, 'and another manager holding the same verb is refused for the same reason').toBe(403);
  });

  test('TC-A-76 — the reservation screen opens for the holder and for the reassigner, nobody else', async () => {
    const holder = await probeGet(W.a2.page, `/requests/${W.tProc}/reservation`);
    expect(holder.status, 'the holder arrives to release: mine && reservation:release').toBe(200);

    const reassigner = await probeGet(W.callers['C-adm'].page, `/requests/${W.tProc}/reservation`);
    expect(reassigner.status, 'an administrator arrives to reassign: reservation:reassign').toBe(200);

    const otherAccountant = await probeGet(W.callers['C-acc'].page, `/requests/${W.tProc}/reservation`);
    expect(
      otherAccountant.status,
      'a second accountant holds release but not this reservation, and cannot reassign — 403'
    ).toBe(403);

    const manager = await probeGet(W.callers['C-mgr'].page, `/requests/${W.tProc}/reservation`);
    expect(manager.status, 'a manager holds no reservation verb at all').toBe(403);

    // A requester who cannot read the request is refused before the reservation
    // rule is reached at all, and refused as not-found: `reservationForm` loads
    // through `loadViewableRequest`, which will not tell a caller outside the
    // data scope that the row exists (requests.go:343-346, F-G-002). The two
    // refusals above say "this reservation is not yours"; this one says nothing.
    const stranger = await probeGet(W.callers['C-req'].page, `/requests/${W.tProc}/reservation`);
    expect(stranger.status, 'and a requester who cannot even read the request is refused first, as not-found').toBe(404);
    expect(
      stranger.body.includes('Only the person holding this reservation'),
      'and is never told there is a reservation to be refused'
    ).toBe(false);
  });

  test('TC-A-77 — releasing a reservation you do not hold needs reservation:reassign', async () => {
    // Its own request: this is the one test that really releases something, and
    // the shared `tProc` has to stay reserved for the rows that read it.
    const target = await freshReservedRequest('Release');
    const otherAccountant = await probePost(W.callers['C-acc'].page, `/requests/${target}/release`, {
      reason: 'Taking it off a colleague', confirm: 'on'
    });
    expect(
      otherAccountant.status,
      'reservation:release lets you give up YOUR reservation; ReleaseRequest refuses somebody else’s (S6)'
    ).toBe(403);

    // Admin is the only role holding reassign, which is what `authorized` is.
    const administrator = await probePost(W.callers['C-adm'].page, `/requests/${target}/release`, {
      reason: `Cleared by an administrator ${W.runId}`, confirm: 'on'
    });
    expect(
      administrator.status,
      'reservation:reassign is what authorises releasing work that is not yours (linking.go:630)'
    ).toBe(303);
    // And it really went back to the queue rather than merely answering 303.
    const reopened = await probeGet(W.a2.page, `/requests/${target}/reservation`);
    expect(reopened.status, 'the reservation screen now says nobody holds it — 409, not 200').toBe(409);
  });

  test('TC-A-78 — only the raiser may edit, ask to cancel, or withdraw their request', async () => {
    const req = W.callers['C-req'];
    const own = await probeGet(req.page, `/requests/${W.ownPending}/edit`);
    expect(own.status, 'the raiser edits their own pending request').toBe(200);

    // Never anybody else's — and the two refusals differ, which is the whole
    // shape of `loadEditableRequest` (requests.go:739-752). It asks the data
    // scope first: a Requester's scope is `own`, so a request they did not raise
    // is not-found before "only the raiser may edit" is ever reached (F-G-002).
    const foreign = await probeGet(req.page, `/requests/${W.tPending}/edit`);
    expect(foreign.status, 'and never anybody else’s — outside the scope, so not-found').toBe(404);

    // The administrator's scope is `all`, so the row resolves and the ownership
    // rule is what refuses them, in its own words rather than the gate's.
    const adminEdit = await probeGet(W.callers['C-adm'].page, `/requests/${W.ownPending}/edit`);
    expect(adminEdit.status, 'an administrator holding request:edit is refused too — editing is the raiser’s act').toBe(403);
    expect(
      adminEdit.body.includes('Only the person who raised a request may edit it'),
      'and the refusal names the rule rather than claiming they lack the verb'
    ).toBe(true);

    const adminAsk = await probeGet(W.callers['C-adm'].page, `/requests/${W.ownApproved}/cancel`);
    expect(adminAsk.status, 'and so is asking for a cancellation (requests.go:802)').toBe(403);

    const ownerAsk = await probeGet(req.page, `/requests/${W.ownApproved}/cancel`);
    expect(ownerAsk.status, 'while the raiser of an approved request may ask').toBe(200);
  });

  // =========================================================================
  // 5. CSRF — swept across one representative POST per resource family.
  // =========================================================================

  /** One state-changing POST per resource family, with a caller that may reach it. */
  function csrfFamilies(): Array<{ family: string; caller: Session; path: string; form: Record<string, string> }> {
    return [
      { family: 'request', caller: W.callers['C-req'], path: `/requests/${W.ownPending}/comment`, form: { body: 'csrf probe' } },
      { family: 'approval', caller: W.m2, path: `/requests/${W.tPending}/return`, form: { comment: 'csrf probe' } },
      { family: 'payment', caller: W.callers['C-acc'], path: `/payments/${W.payId}/void`, form: { reason: 'csrf probe' } },
      { family: 'reservation', caller: W.a2, path: `/requests/${W.tProc}/release`, form: { reason: 'csrf probe', confirm: 'on' } },
      { family: 'notification', caller: W.callers['C-req'], path: '/notifications/read', form: {} },
      { family: 'user', caller: W.callers['C-adm'], path: '/users', form: { id: '0', email: 'x@y.test', name: 'x' } },
      { family: 'role', caller: W.callers['C-adm'], path: '/roles/new', form: { name: `csrf-${W.runId}` } },
      { family: 'config', caller: W.callers['C-adm'], path: '/configuration', form: {} },
      { family: 'budget', caller: W.callers['C-adm'], path: '/budgets', form: { month: W.today.slice(0, 7) } },
      { family: 'vendor', caller: W.callers['C-adm'], path: '/vendors', form: { name: `csrf-${W.runId}`, type: 'company' } },
      { family: 'month', caller: W.callers['C-adm'], path: '/months', form: { month: '2099-02' } },
      { family: 'backup', caller: W.callers['C-adm'], path: '/backups', form: {} }
    ];
  }

  test('TC-A-79 — every state-changing POST refuses a missing CSRF token', async () => {
    for (const f of csrfFamilies()) {
      const probe = await probePost(f.caller.page, f.path, f.form, { csrf: 'omit' });
      expect.soft(probe.status, `${f.family}: POST ${f.path} with no csrf field must be refused`).toBe(403);
      expect
        .soft(probe.body.includes(CSRF_REFUSAL), `${f.family}: and refused BY withCSRF, not by the permission gate`)
        .toBe(true);
    }
  });

  test('TC-A-80 — every state-changing POST refuses a forged CSRF token', async () => {
    for (const f of csrfFamilies()) {
      const probe = await probePost(f.caller.page, f.path, f.form, { csrf: 'bogus' });
      expect.soft(probe.status, `${f.family}: POST ${f.path} with a wrong csrf field must be refused`).toBe(403);
      expect.soft(probe.body.includes(CSRF_REFUSAL), `${f.family}: and refused by withCSRF`).toBe(true);
    }
  });

  test('TC-A-81 — a CSRF token from another signed-in session is refused', async () => {
    // The token is per-session (auth.go:186 signs the nanosecond it was minted),
    // so one person's token must not authorise another person's form.
    const foreign = await csrfToken(W.callers['C-adm'].context);
    const mine = await csrfToken(W.callers['C-req'].context);
    expect(foreign, 'two sessions must not share a token, or there is nothing to test').not.toBe(mine);

    for (const f of csrfFamilies()) {
      const other = f.caller === W.callers['C-adm'] ? mine : foreign;
      const probe = await probePost(f.caller.page, f.path, f.form, { csrf: other });
      expect
        .soft(probe.status, `${f.family}: a token minted for another session must not authorise POST ${f.path}`)
        .toBe(403);
      expect.soft(probe.body.includes(CSRF_REFUSAL), `${f.family}: and the refusal is the CSRF one`).toBe(true);
    }
  });

  test('TC-A-82 — the same POST with the session’s own token is accepted (control)', async () => {
    const probe = await probePost(W.callers['C-req'].page, '/notifications/read', {});
    expect(
      probe.status,
      'the CSRF tests above must be failing on the token and nothing else, so the valid case has to pass'
    ).toBe(303);
  });

  test('TC-A-83 — POST /login carries no CSRF gate at all', async () => {
    // Not a hole to fix in this pass, but a fact the matrix has to record:
    // loginPost is the one state-changing POST outside withCSRF (app.go:365).
    const probe = await probePost(W.anonPage, '/login', { email: admin.email, password: 'wrong-on-purpose' }, {
      csrf: 'omit'
    });
    expect(
      probe.status,
      'POST /login answers 200 with the login template and an error — it never reaches a CSRF check'
    ).toBe(200);
    expect(probe.body.includes(CSRF_REFUSAL), 'and no CSRF refusal is involved').toBe(false);
  });

  test('TC-A-84 — POST /logout refuses an anonymous caller with no token', async () => {
    const probe = await probePost(W.anonPage, '/logout', {}, { csrf: 'omit' });
    expect(probe.status, 'logout is CSRF-gated even though it is not login-gated (app.go:366)').toBe(403);
  });

  // =========================================================================
  // 6. Privilege escalation and tampering. Every one of these must be refused;
  //    any that succeeds is a finding.
  // =========================================================================

  const ADMIN_BY_URL: Array<[string, string]> = [
    ['TC-A-85', '/users'],
    ['TC-A-86', '/roles'],
    ['TC-A-87', '/audit'],
    ['TC-A-88', '/configuration'],
    ['TC-A-89', '/backups'],
    ['TC-A-90', '/admin/notifications']
  ];
  for (const [tc, path] of ADMIN_BY_URL) {
    test(`${tc} — a Requester typing ${path} is refused (R6)`, async () => {
      const probe = await probeGet(W.callers['C-req'].page, path);
      expect(probe.status, `${path} is an administration screen and a Requester holds none of its verbs`).toBe(403);
      expect(probe.body.includes(GATE_REFUSAL), 'and the refusal comes from the permission gate').toBe(true);
    });
  }

  test('TC-A-91 — a Requester cannot read a payment by id', async () => {
    const probe = await probeGet(W.callers['C-req'].page, `/payments/${W.payId}`);
    expect(probe.status, 'a Requester holds no payment verb of any kind').toBe(403);
  });

  test('TC-A-92 — a Requester cannot download a payment attachment they have no business with', async () => {
    // F-A-01 was the worst finding in this area: GET /attachments/{id} checked
    // attachment:view and then only that the file sat inside AttachmentDir — no
    // ownership check at all — and attachment:view is a seeded **Requester**
    // grant, so any signed-in user read every bank advice in the system by
    // walking ids from 1.
    //
    // `attachmentDownload` now resolves the attachment to its payment and runs
    // `canReadPayment` (app.go:1105-1120), which is the same row scope
    // `paymentDetail` applies — the check whose own comment says "or
    // payment:view becomes a way around Q5/R6". This route was the hole that
    // comment was written about.
    const probe = await probeGet(W.callers['C-req'].page, `/attachments/${W.payAttId}`);
    // 404 and not 403, deliberately: these ids are small consecutive integers,
    // and a refusal you can tell apart from a miss is an enumeration oracle over
    // the whole table (`notFoundAttachment`, app.go:1054-1062).
    expect(
      probe.status,
      'the bank advice for a payment on somebody else’s request is refused, as not-found — F-A-01'
    ).toBe(404);
    expect(
      probe.body.includes(`bank-advice-${W.runId}`),
      'and no part of the file comes back with the refusal'
    ).toBe(false);
    // The refusal is indistinguishable from an id that was never written.
    const missing = await probeGet(W.callers['C-req'].page, '/attachments/99999999');
    expect(missing.status, 'an id nobody ever wrote answers the same way').toBe(404);

    // The control, so the 404 above is the ownership check and not a broken
    // route: the accountant whose request scope reaches that payment gets the
    // file, over the same URL, with the same verb.
    const holder = await W.a2.page.request.get(new URL(`/attachments/${W.payAttId}`, W.baseURL).toString(), {
      maxRedirects: 0, failOnStatusCode: false
    });
    expect(holder.status(), 'while a caller the payment’s request is visible to is served').toBe(200);
    expect(
      (holder.headers()['content-disposition'] ?? '').includes(`bank-advice-${W.runId}`),
      'and served the real bank advice — so the route works, and it is the reader that was refused'
    ).toBe(true);
  });

  test('TC-A-93 — a Manager reads the same attachment, and the scope check is what still guards it', async () => {
    // Inverted by migration 12. The Manager role held no attachment:view, so an
    // approver could not open the invoice on the request they were being asked
    // to approve — the check moved outside the product, where it leaves no
    // record. The grant is not a skeleton key: attachmentDownload still asks
    // canReadPayment, which for a request-linked payment defers to whether the
    // caller may see that request. A Manager may, so this resolves; the verb
    // opened the door and the scope is what decides who walks through it.
    const probe = await probeGet(W.callers['C-mgr'].page, `/attachments/${W.payAttId}`);
    expect(probe.status, 'an approver can read the documents on a request they can see').toBe(200);
  });

  test('TC-A-94 — the Download link on a request document resolves to that document and nothing else', async () => {
    // F-A-05, and this is the case that pins its fix. One download route used to
    // serve two tables: `AttachmentByID` reads `payment_attachments`, three
    // templates rendered **request** attachments through it, and both sequences
    // start at 1 — so pressing Download on your own invoice could hand you a
    // stranger's bank advice, filed under your own document's name.
    //
    // The tables now have a route each. A request document is served by
    // `GET /requests/{id}/attachments/{attachmentID}` (app.go:468), which checks
    // the document really belongs to the request in the path and then scopes it
    // by that request; `GET /attachments/{id}` serves payment documents only.
    // Three halves, because the fix has three parts:
    const own = W.callers['C-req'];
    const link = `/requests/${W.ownPending}/attachments/${W.ownReqAttId}`;

    // (a) The raiser's own Download link returns the raiser's own invoice.
    const mine = await own.page.request.get(new URL(link, W.baseURL).toString(), {
      maxRedirects: 0, failOnStatusCode: false
    });
    expect(mine.status(), 'half (a): the requester’s own Download link answers with a file').toBe(200);
    expect(
      (mine.headers()['content-disposition'] ?? '').includes(`invoice-${W.runId}`),
      'half (a): and the file is the invoice that link is under — F-A-05'
    ).toBe(true);

    // (b) The id spaces no longer cross. Asking the *payment* route for the
    // request document's id gets a payment document or nothing — never the
    // invoice — whichever way the two sequences happen to line up this run.
    const crossed = await own.page.request.get(
      new URL(`/attachments/${W.ownReqAttId}`, W.baseURL).toString(),
      { maxRedirects: 0, failOnStatusCode: false }
    );
    expect(
      (crossed.headers()['content-disposition'] ?? '').includes(`invoice-${W.runId}`),
      `half (b): /attachments/${W.ownReqAttId} reads payment_attachments, so it can never answer with a ` +
        'request document whatever the id collision — F-A-05'
    ).toBe(false);
    expect(
      [404, 200].includes(crossed.status()),
      `half (b): and it answers as the payment route: the id either names a payment attachment this ` +
        `caller may read, or nothing — got ${crossed.status()}`
    ).toBe(true);

    // (c) The {id} in the path is a check, not decoration. The same attachment
    // id under a different request is refused, or the longer URL would be
    // /attachments/{id} again under another name.
    const wrongParent = await own.page.request.get(
      new URL(`/requests/${W.ownApproved}/attachments/${W.ownReqAttId}`, W.baseURL).toString(),
      { maxRedirects: 0, failOnStatusCode: false }
    );
    expect(
      wrongParent.status(),
      'half (c): a document asked for under a request it does not belong to is not-found — F-A-05'
    ).toBe(404);

    // And the route is scoped like every other request read: a stranger holding
    // attachment:view is refused the raiser's invoice, as not-found (F-A-01).
    const stranger = await W.r2.page.request.get(new URL(link, W.baseURL).toString(), {
      maxRedirects: 0, failOnStatusCode: false
    });
    expect(
      stranger.status(),
      'and another requester, holding the same attachment:view, cannot read it at all'
    ).toBe(404);
  });

  test('TC-A-95 — posting another user’s id to /users is refused without user:edit', async () => {
    const victim = await userIdOf(W.adminPage, W.m2.subject.email);
    const probe = await probePost(W.callers['C-req'].page, '/users', {
      id: victim, name: 'Renamed by a requester', active: 'on', role: 'data_entry'
    });
    expect(probe.status, 'POST /users is gated on user:edit, which a Requester does not hold').toBe(403);
    // Reproduced a second way: the victim's name is unchanged.
    await W.adminPage.goto('/users');
    await expect(
      W.adminPage.locator('tr', { hasText: W.m2.subject.email }),
      'and the victim row still carries its own name'
    ).toContainText(W.m2.subject.name);
  });

  test('TC-A-96 — granting yourself a role by posting role_ids is refused', async () => {
    const req = W.callers['C-req'];
    const selfId = await userIdOf(W.adminPage, req.subject.email);
    const probe = await probePost(req.page, '/users', {
      id: selfId, name: req.subject.name, active: 'on', role: 'admin', role_ids: W.systemRoleIds.Admin
    });
    expect(probe.status, 'self-promotion goes through POST /users, and that route needs user:edit').toBe(403);
    // And the grant did not land: the admin screen is still refused.
    const after = await probeGet(req.page, '/users');
    expect(after.status, 'so the Requester still holds no user:view on the next request').toBe(403);
  });

  test('TC-A-97 — a system role cannot be deleted, even by an administrator', async () => {
    const probe = await probePost(W.callers['C-adm'].page, `/roles/${W.systemRoleIds.Requester}/delete`, {});
    expect(probe.status, 'DeleteRole refuses is_system=1 (store/permissions.go:318) — R9').toBe(403);
    await W.adminPage.goto('/roles');
    await expect(
      W.adminPage.locator('.segmented a', { hasText: 'Requester' }),
      'and the role is still there'
    ).toHaveCount(1);
  });

  test('TC-A-98 — a system role cannot be renamed, even by an administrator', async () => {
    const probe = await probePost(W.callers['C-adm'].page, '/roles', {
      role_id: W.systemRoleIds.Manager, name: `Manager-renamed-${W.runId}`, description: 'tampered'
    });
    expect(probe.status, 'UpdateRole refuses renaming a system role (store/permissions.go:290)').toBe(403);
    await W.adminPage.goto('/roles');
    await expect(W.adminPage.locator('.segmented a', { hasText: 'Manager' }), 'and the name held').toHaveCount(1);
  });

  test('TC-A-99 — a Requester cannot create a role', async () => {
    const probe = await probePost(W.callers['C-req'].page, '/roles/new', { name: `sneaky-${W.runId}` });
    expect(probe.status, 'role:create is an administration verb').toBe(403);
  });

  test('TC-A-100 — a hand-crafted cell value cannot invent a grant', async () => {
    // expandCells drops unknown rows, unknown columns and cells with no
    // canonical action (permmap.go:274-295), and rolesSave re-screens every
    // expanded grant with store.ValidGrant (app.go:1321-1326).
    const roleName = `forged-${W.runId}`;
    const roleId = await createCustomRole(W.adminPage, roleName);
    const probe = await probePost(W.callers['C-adm'].page, '/roles', {
      role_id: roleId, name: roleName, description: '', cell: 'administration:destroy'
    });
    expect(probe.status, 'an unknown cell expands to nothing, so the save succeeds and grants nothing').toBe(303);

    const forgedPair = await probePost(W.callers['C-adm'].page, '/roles', {
      role_id: roleId, name: roleName, description: '', perm: 'user:obliterate'
    });
    expect(forgedPair.status, 'a permission pair outside the vocabulary is refused outright').toBe(400);
    expect(forgedPair.body.includes('That permission does not exist'), 'and named as such').toBe(true);

    await W.adminPage.goto(`/roles?role=${roleId}`);
    await openAdvanced(W.adminPage);
    await expect(
      W.adminPage.locator('.perm-table input[name="perm"]:checked'),
      'and after both attempts the role still holds nothing'
    ).toHaveCount(0);
  });

  test('TC-A-101 — a Requester cannot reserve, pay for, or settle anything', async () => {
    const req = W.callers['C-req'];
    for (const [path, form] of [
      [`/requests/${W.ownApproved}/record-payment`, {}],
      ['/payments', { request_id: String(W.ownApproved), amount: '10.00', paid_on: W.today, head_id: W.headId, payment_mode: 'bank_transfer', settlement: 'settled' }],
      [`/requests/${W.ownApproved}/settlement-preview`, { amount: '10.00' }],
      [`/requests/${W.ownApproved}/hold`, { reason: 'stop' }]
    ] as Array<[string, Record<string, string>]>) {
      const probe = await probePost(req.page, path, form);
      expect.soft(probe.status, `${path}: a Requester holds no payment or reservation verb`).toBe(403);
    }
  });

  test('TC-A-102 — the request id on the payment form cannot be swapped for one you have not reserved', async () => {
    // A2 holds a reservation on tProc. Posting a *different* approved request's
    // id must not create a payment against it (X5).
    const probe = await probePost(W.a2.page, '/payments', {
      request_id: String(W.tApproved), amount: '10.00', paid_on: W.today,
      head_id: W.headId, payment_mode: 'bank_transfer', settlement: 'settled'
    });
    expect(probe.status, 'RecordPaymentForRequest demands status=processing AND processing_by=actor').toBe(403);
    expect(
      probe.body.includes(GATE_REFUSAL),
      'and the refusal is the store’s reservation check, not the route gate — payment:create was held'
    ).toBe(false);
    // Reproduced a second way: the entry screen for that request answers 409,
    // because this accountant does not hold its reservation.
    const entry = await probeGet(W.a2.page, `/payments/new?request=${W.tApproved}`);
    expect(entry.status, 'and the entry screen for a request you do not hold is a conflict, not a form').toBe(409);
    // F-A-09: the refusal now carries its own reason. `friendly` keeps the
    // sentence a wrapped ErrForbidden was written with and falls back to the
    // permission wording only for a bare one (app.go:2172-2182) — so an
    // accountant who simply has not reserved the request is told to reserve it,
    // instead of being sent to an administrator for a grant they already hold.
    expect(
      probe.body.includes('Reserve this request before recording its payment'),
      'the state conflict says what to do about it, not "something went wrong" — F-A-09'
    ).toBe(true);
    expect(
      probe.body.includes('Something went wrong while processing your request'),
      'and the generic server-fault sentence is nowhere near it'
    ).toBe(false);
  });

  test('TC-A-103 — a forged settlement value is refused', async () => {
    const probe = await probePost(W.a2.page, '/payments', {
      request_id: String(W.tProc), amount: '10.00', paid_on: W.today,
      head_id: W.headId, payment_mode: 'bank_transfer', settlement: 'written-off'
    });
    expect(probe.status, 'settlement is an explicit disposition vocabulary checked in the store').toBe(400);
    expect(probe.body.includes('choose how the remaining balance should be handled'), 'and named').toBe(true);
  });

  test('TC-A-104 — an amount above the approved ceiling is refused (G13)', async () => {
    const probe = await probePost(W.a2.page, '/payments', {
      request_id: String(W.tProc), amount: '9999999.00', paid_on: W.today,
      head_id: W.headId, payment_mode: 'bank_transfer', settlement: 'settled'
    });
    expect(probe.status, 'the approved amount is a hard ceiling, not a suggestion').toBe(400);
    expect(probe.body.includes('more than the approved'), 'and the refusal explains the ceiling').toBe(true);
  });

  test('TC-A-105 — vendor_id on a reimbursement is ignored, and requester_id cannot be forged', async () => {
    const req = W.callers['C-req'];
    const victimId = await userIdOf(W.adminPage, W.r2.subject.email);
    const title = `Tampered ${W.runId}`;
    const created = await raiseRequest(req.page, {
      title, managerId: await userIdOf(W.adminPage, W.m2.subject.email), amount: '112.00',
      extra: { vendor_id: W.vendorId, requester_id: victimId }
    });

    const detail = await probeGet(req.page, `/requests/${created}`);
    expect(detail.status, 'the request belongs to the caller, so the caller can read it').toBe(200);
    expect(
      detail.body.includes(req.subject.name),
      'CreateRequest sets requester_id from the session (store/requests.go:366) — a posted one is ignored'
    ).toBe(true);

    // A reimbursement never carries a vendor: forcesRequesterPayee zeroes it.
    await W.r2.page.goto('/requests?bucket=all');
    await expect(
      W.r2.page.locator(`a.req-card[href="/requests/${created}"]`),
      'and the request never appears in the impersonated user’s own list'
    ).toHaveCount(0);
  });

  test('TC-A-106 — a GET on a mutation route reaches no handler', async () => {
    const probe = await probeGet(W.callers['C-mgr'].page, `/requests/${W.tPending}/approve`);
    expect(
      [404, 405].includes(probe.status),
      `only POST /requests/{id}/approve is registered, so a GET falls to the catch-all — got ${probe.outcome}`
    ).toBe(true);
  });

  test('TC-A-107 — a POST on a read route reaches no handler', async () => {
    const probe = await probePost(W.callers['C-req'].page, `/requests/${W.ownPending}`, {});
    expect(
      [404, 405].includes(probe.status),
      `GET /requests/{id} matches the path and POST matches no method, so ServeMux answers 405 — got ${probe.outcome}`
    ).toBe(true);
  });

  test('TC-A-108 — an anonymous caller reaches nothing but the login screen and the assets', async () => {
    for (const path of ['/', '/dashboard', '/grid', '/requests', '/payments', '/users', '/roles', '/audit',
      '/configuration', '/notifications', '/approvals', '/accounts-queue', '/recoverables', '/budgets',
      '/months', '/reports/monthly', '/vendors', '/projects', '/heads', '/backups', '/admin/notifications',
      '/export.csv', '/reports/ytd.csv', '/attachments/1', `/requests/${W.tPending}`]) {
      const probe = await probeGet(W.anonPage, path);
      expect
        .soft(probe.status >= 300 && probe.status < 400, `${path} must redirect an anonymous caller — got ${probe.outcome}`)
        .toBe(true);
      expect.soft(probe.location ?? '', `${path} must redirect to the login screen`).toContain('/login');
    }
  });

  test('TC-A-109 — another user’s notification cannot be opened', async () => {
    // Every row is resolved as (user_id, id), so somebody else's is not-found
    // rather than forbidden — the safer of the two answers (inapp.go:76-81).
    const probe = await probeGet(W.callers['C-req'].page, '/notifications/1/open');
    expect(
      [404, 303].includes(probe.status),
      `a notification addressed to somebody else must not open — got ${probe.outcome}`
    ).toBe(true);
    if (probe.status === 303) {
      // 303 would mean the row WAS the caller's own; assert that reading is fine.
      expect(probe.location ?? '', 'a 303 here means the row belonged to the caller after all').not.toContain('/login');
    }
  });

  test('TC-A-110 — a Requester cannot comment on a request they cannot see', async () => {
    const marker = `intruded-${W.runId}`;
    const probe = await probePost(W.callers['C-req'].page, `/requests/${W.tPending}/comment`, { body: marker });
    // request:comment is held and the gate opens; `requestComment` then loads
    // through `loadViewableRequest`, which refuses the row — as 404, so a write
    // route cannot be used to find out which ids exist either (F-G-002).
    expect(probe.status, 'request:comment is held, but loadViewableRequest refuses the row first').toBe(404);
    // Nothing was written: the request's own manager sees no such comment.
    const detail = await probeGet(W.m2.page, `/requests/${W.tPending}`);
    expect(detail.body.includes(marker), 'and the refused comment left no trace on the request').toBe(false);
  });

  test('TC-A-111 — a Requester cannot withdraw or re-raise somebody else’s request', async () => {
    for (const path of [`/requests/${W.tPending}/withdraw`, `/requests/${W.tPending}/reraise`]) {
      const probe = await probePost(W.callers['C-req'].page, path, {});
      expect.soft(probe.status, `${path}: the store checks RequesterID != actor`).toBe(403);
    }
  });

  test('TC-A-112 — a Manager cannot cancel a request routed to another manager', async () => {
    const probe = await probePost(W.callers['C-mgr'].page, `/requests/${W.tApproved}/cancel`, {
      reason: 'Cancelled by a bystander'
    });
    expect(probe.status, 'CancelRequest checks ManagerID != actor (store/requests.go:976)').toBe(403);
  });

  test('TC-A-113 — a reservation cannot be handed to somebody who cannot work the queue', async () => {
    const requesterId = await userIdOf(W.adminPage, W.callers['C-req'].subject.email);
    const probe = await probePost(W.callers['C-adm'].page, `/requests/${W.tProc}/reassign`, {
      to_user_id: requesterId, confirm: 'on', reason: 'Parked on a requester'
    });
    expect(
      probe.status,
      'canWorkTheQueue is a rule, not a cosmetic filter: a holder without payment:process strands the request'
    ).toBe(400);
  });

  test('TC-A-114 — an inactive account cannot use a session it already holds', async () => {
    const subject = await createUserWithExactRoles(W.adminPage, `deact-${W.runId}`, W.runId, ['Admin']);
    const session = await signIn(W.adminPage.context().browser()!, subject, W.adminPage.viewportSize());
    try {
      const before = await probeGet(session.page, '/users');
      expect(before.status, 'the subject starts as a working administrator').toBe(200);

      await W.adminPage.goto('/users');
      await W.adminPage.locator('tr', { hasText: subject.email }).getByRole('button', { name: 'Edit' }).click();
      const sheet = W.adminPage.locator('.overlay:not([hidden])');
      await sheet.getByRole('checkbox', { name: 'Active' }).uncheck();
      await sheet.getByRole('button', { name: 'Save user' }).click();
      await expect(W.adminPage).toHaveURL(/\/users$/);

      const after = await probeGet(session.page, '/users');
      expect(
        after.status >= 300 && after.status < 400,
        `userFromRequest rejects an inactive user (auth.go:228), so the live session dies — got ${after.outcome}`
      ).toBe(true);
      expect(after.location ?? '', 'and the caller is sent to the login screen').toContain('/login');
    } finally {
      await session.close();
    }
  });

  // =========================================================================
  // 7. Vocabulary integrity.
  // =========================================================================

  test('TC-A-115 — the roles matrix offers all 21 resources and all 66 pairs', async () => {
    await W.adminPage.goto(`/roles?role=${W.systemRoleIds.Admin}`);
    await openAdvanced(W.adminPage);
    // The desktop Advanced disclosures carry one live checkbox per canonical
    // pair; the mobile accordion repeats them disabled (templates.go:1594,1611).
    const values = await W.adminPage.locator('.perm-table input[name="perm"]').evaluateAll(nodes =>
      nodes.map(n => (n as HTMLInputElement).value)
    );
    const pairs = new Set(values);
    const resources = new Set(values.map(v => v.split(':')[0]));

    const expectedPairs = Object.entries(VOCABULARY).flatMap(([r, acts]) => acts.map(a => `${r}:${a}`));
    expect(expectedPairs.length, 'the vocabulary is 66 (resource, action) pairs').toBe(66);
    expect(Object.keys(VOCABULARY).length, 'across 21 resources').toBe(21);
    expect(pairs.size, 'and the matrix screen offers every one of them, exactly once').toBe(66);
    expect(resources.size, 'covering all 21 resources').toBe(21);

    const missing = expectedPairs.filter(p => !pairs.has(p));
    expect(missing, 'no canonical pair may be ungrantable from the screen (R2/R8)').toEqual([]);
    const extra = [...pairs].filter(p => !expectedPairs.includes(p));
    expect(extra, 'and the screen may not offer a pair the vocabulary has never heard of').toEqual([]);
  });

  test('TC-A-116 — the seeded Admin role holds every pair, and the screen renders it so', async () => {
    await W.adminPage.goto(`/roles?role=${W.systemRoleIds.Admin}`);
    await openAdvanced(W.adminPage);
    await expect(
      W.adminPage.locator('.perm-table input[name="perm"]:checked'),
      'adminGrants() is every canonical pair (R7), so all 66 render checked'
    ).toHaveCount(66);
  });

  test('TC-A-117 — saving the matrix round-trips exactly', async () => {
    const roleName = `roundtrip-${W.runId}`;
    const roleId = await createCustomRole(W.adminPage, roleName);

    await W.adminPage.goto(`/roles?role=${roleId}`);
    await openAdvanced(W.adminPage);
    await expect(
      W.adminPage.locator('.perm-table input[name="perm"]:checked'),
      'a new role starts with no permissions at all'
    ).toHaveCount(0);

    // Grant every pair through the screen's own Advanced checkboxes.
    const boxes = W.adminPage.locator('.perm-table input[name="perm"]');
    const count = await boxes.count();
    expect(count, 'the desktop matrix draws one live checkbox per canonical pair').toBe(66);
    for (let i = 0; i < count; i++) await boxes.nth(i).check();
    await W.adminPage.getByRole('button', { name: 'Save role' }).click();
    await expect(W.adminPage).toHaveURL(new RegExp(`/roles\\?role=${roleId}$`));

    await openAdvanced(W.adminPage);
    await expect(
      W.adminPage.locator('.perm-table input[name="perm"]:checked'),
      'every pair ticked comes back ticked — UpdateRolePermissions replaces the whole set'
    ).toHaveCount(66);
    // Every cell whose whole action set is now held renders as fully granted, so
    // the summary row and the Advanced disclosure cannot disagree.
    const cells = W.adminPage.locator('.perm-table input[name="cell"]');
    const cellCount = await cells.count();
    await expect(
      W.adminPage.locator('.perm-table input[name="cell"]:checked'),
      'and every available cell comes back granted, derived from the same stored set'
    ).toHaveCount(cellCount);

    // And the reverse. Clearing a cell revokes everything behind it: the
    // screen's own script unticks the Advanced boxes it covers (fervid-app.js,
    // initRoleMatrix). Since fixwave rbac-1 the Advanced boxes are the grant
    // set the server saves, so unticking boxes alone revokes too; that path
    // has its own case below.
    for (let i = 0; i < cellCount; i++) await cells.nth(i).uncheck();
    await expect(
      W.adminPage.locator('.perm-table input[name="perm"]:checked'),
      'clearing the cells clears every canonical action behind them, in the browser'
    ).toHaveCount(0);
    await W.adminPage.getByRole('button', { name: 'Save role' }).click();
    await expect(W.adminPage).toHaveURL(new RegExp(`/roles\\?role=${roleId}$`));
    await openAdvanced(W.adminPage);
    await expect(
      W.adminPage.locator('.perm-table input[name="perm"]:checked'),
      'and on the server too — anything absent from the save is revoked'
    ).toHaveCount(0);
  });

  test('TC-A-118 — the data scope control round-trips for both scoped resources', async () => {
    const roleName = `scopes-${W.runId}`;
    const roleId = await createCustomRole(W.adminPage, roleName);
    await grantMatrix(W.adminPage, roleId, roleName, {
      cells: ['requests:view', 'payments:view'],
      scopes: { request: 'assigned', payment: 'own' }
    });

    await W.adminPage.goto(`/roles?role=${roleId}`);
    await expect(
      W.adminPage.locator('input[name="scope_request"][value="assigned"]'),
      'the request scope comes back as saved'
    ).toBeChecked();
    await expect(
      W.adminPage.locator('input[name="scope_payment"][value="own"]'),
      'and so does the payment scope — request and payment are the two scoped resources'
    ).toBeChecked();
  });

  test('TC-A-119 — seven granted verbs are named by no route gate, four of them by nothing at all', async () => {
    // Transcribed from the permission gates across internal/app/app.go and
    // cross-checked against templates. A verb an administrator can grant, and
    // that nothing consumes, is a promise the product does not keep.
    //
    // WHY THE PINNED NUMBERS MOVED, 55 → 59 and 9 → 4. Four verbs acquired a
    // route gate during the repair, and each is a finding closing, not a
    // transcription being tidied:
    //   · grid:view              — GET /grid was RequireLogin only (F-A-02),
    //                              app.go:439.
    //   · approval:reassign      — A7 had no door at all; POST
    //                              /requests/{id}/reassign-approver is it
    //                              (F-A-06/F-C-02), app.go:577.
    //   · recoverable_category:delete — the category could be created and edited
    //                              but never removed (F-E-06), app.go:614.
    //   · user:create            — the ＋ Add user control was gated on it while
    //                              its POST asked for user:edit (F-A-07). The
    //                              route now takes BOTH through `requireAnyOf`
    //                              (app.go:598) and `userSave` demands whichever
    //                              one the press actually needs, which is why
    //                              user:create and user:edit both count here even
    //                              though neither appears in a RequirePermission
    //                              call any more.
    // Nothing was removed from the set. If a future change deletes a gate this
    // list will fail on the size, which is the point of pinning it.
    const routeGates = new Set([
      'month:view', 'month:create', 'month:lock',
      'payment:view', 'payment:create', 'payment:edit', 'payment:void', 'payment:process',
      'payment:settle', 'payment:hold',
      'attachment:view', 'attachment:create',
      'grid:view', 'grid:export', 'report:view', 'report:export',
      'recoverable_report:view', 'recoverable_report:export',
      'recoverable_category:edit', 'recoverable_category:delete',
      'notification:view', 'notification:edit',
      'budget:view', 'budget:edit', 'project:view', 'project:edit', 'head:view', 'head:edit',
      'vendor:view', 'vendor:create', 'vendor:edit',
      'request:view', 'request:create', 'request:edit', 'request:withdraw', 'request:reraise',
      'request:comment', 'request:cancel',
      'approval:approve', 'approval:reject', 'approval:return', 'approval:reassign',
      'approval:accept_partial', 'approval:cancel',
      'reservation:reserve', 'reservation:release', 'reservation:reassign',
      'user:view', 'user:create', 'user:edit', 'role:view', 'role:edit', 'role:create', 'role:delete',
      'config:view', 'config:edit', 'audit:view', 'backup:view', 'backup:create'
    ]);
    expect(routeGates.size, 'routes() names 59 distinct pairs').toBe(59);

    const all = Object.entries(VOCABULARY).flatMap(([r, acts]) => acts.map(a => `${r}:${a}`));
    const unconsumed = all.filter(p => !routeGates.has(p)).sort();
    expect(unconsumed, 'the pairs no route gate names — F-A-06').toEqual([
      'head:create',
      'payment:mark_partial',
      'project:create',
      'recoverable_category:create',
      'recoverable_category:view',
      'vendor_bank:edit',
      'vendor_bank:view'
    ]);
    // Three of the seven are enforced *below* the route, which is the right place
    // for each of them: the store leaves the bank columns out of its SELECT
    // without vendor_bank:view (store/vendors.go:16,48) and the vendor form
    // disables them without vendor_bank:edit; and payment:mark_partial cannot be
    // a route gate at all, because it applies to one value of one field —
    // `paymentCreate` checks it when settlement=partial (app.go:847, F-D-10).
    // The remaining four are consulted by nothing: no route, no handler, no
    // template. They are still a lie the roles screen tells.
    const enforcedBelowTheRoute = ['payment:mark_partial', 'vendor_bank:view', 'vendor_bank:edit'];
    const enforcedNowhere = unconsumed.filter(p => !enforcedBelowTheRoute.includes(p));
    expect(enforcedNowhere.length, 'four pairs are enforced by nothing at all — F-A-06').toBe(4);
    // Every gate a route does name has to be inside the vocabulary, or an
    // administrator could never grant it and the route would be dead.
    const outsideVocabulary = [...routeGates].filter(p => !all.includes(p));
    expect(outsideVocabulary, 'no route may be gated on a pair outside resourceActions').toEqual([]);
  });

  test('TC-A-120 — approval:reassign is granted to Manager and Admin, and now has a door', async () => {
    // `store.ReassignRequest` existed, was tested, and had no HTTP caller — so
    // A7, "reassign with reason and history", was a verb two seeded roles held
    // and nobody could exercise (F-A-06/F-C-02). `POST /requests/{id}/
    // reassign-approver` (app.go:577) is that door, gated on approval:reassign,
    // taking `manager_id` and `reason`. It is also the recovery path F-A-08 and
    // F-G-025 need: the way out when a request is routed to the wrong approver.
    //
    // First, the gate, probed with no CSRF token so nothing is written. A holder
    // of the verb reaches withCSRF — which is the proof the permission gate
    // opened — and a non-holder is stopped at the gate itself.
    const opened = await probePost(W.callers['C-mgr'].page, `/requests/${W.tPending}/reassign-approver`, {}, {
      csrf: 'omit'
    });
    expect(opened.status, 'a Manager holds approval:reassign, so the route is reachable at all').toBe(403);
    expect(
      opened.body.includes(CSRF_REFUSAL),
      'and the refusal is CSRF, not permission — the gate opened and nothing was written'
    ).toBe(true);

    const refused = await probePost(W.callers['C-acc'].page, `/requests/${W.tPending}/reassign-approver`, {}, {
      csrf: 'omit'
    });
    expect(refused.status, 'Accounts holds no approval:reassign').toBe(403);
    expect(refused.body.includes(GATE_REFUSAL), 'and is stopped by the permission gate').toBe(true);

    // The route is exactly one path, not three: the two names the audit guessed
    // at while looking for it still resolve to nothing.
    for (const path of [
      `/requests/${W.tPending}/reassign-manager`,
      `/requests/${W.tPending}/approver`
    ]) {
      const probe = await probePost(W.callers['C-adm'].page, path, { manager_id: '1', reason: 'x' });
      expect
        .soft([404, 405].includes(probe.status), `${path} must not exist — got ${probe.outcome}`)
        .toBe(true);
    }

    // And it works, on a request of this case's own — the shared fixtures are
    // read by too many rows to have their approver moved out from under them.
    const managerId = await managerIdFor(W.r2.page, W.m2.subject.name);
    const target = await raiseRequest(W.r2.page, {
      title: `Reassigned ${W.runId}-${Date.now()}`, managerId, amount: '121.00'
    });
    const newApprover = await userIdOf(W.adminPage, W.callers['C-mgr'].subject.email);
    const done = await probePost(W.m2.page, `/requests/${target}/reassign-approver`, {
      manager_id: newApprover, reason: `Handed over for the audit ${W.runId}`
    });
    expect(done.status, 'the holder of the verb reassigns the request').toBe(303);
    // Reproduced where it matters: the new approver can now decide it, and the
    // old one cannot. That is what "reassigned" has to mean.
    await W.callers['C-mgr'].page.goto('/approvals');
    await expect(
      W.callers['C-mgr'].page.locator(`a.req-card[href="/requests/${target}"]`),
      'and the request is in the new approver’s queue'
    ).toHaveCount(1);
    const oldApprover = await probePost(W.m2.page, `/requests/${target}/approve`, { approved_amount: '121.00' });
    expect(oldApprover.status, 'while the approver it was taken from can no longer decide it').toBe(403);

    // Two verbs, two routes, and they are not the same verb: the reservation's
    // /reassign is gated on reservation:reassign, which a Manager does not hold.
    const reservation = await probePost(W.callers['C-mgr'].page, `/requests/${W.tProc}/reassign`, {});
    expect(
      reservation.status,
      'a Manager holds approval:reassign and not reservation:reassign — the two /reassign routes stay distinct'
    ).toBe(403);
    expect(
      reservation.body.includes(GATE_REFUSAL),
      'and the reservation route refuses them at its own gate'
    ).toBe(true);
  });

  test('TC-A-121 — user:create is offered on the screen and honoured by the route', async () => {
    // F-A-07: the "＋ Add user" button was gated on user:create while POST /users
    // was gated on user:edit, so a role holding create but not edit saw a control
    // whose submit answered 403 — the button and the route disagreeing about
    // which verb the press needs.
    //
    // One handler still serves both presses, so the route cannot name a single
    // verb: `requireAnyOf("user", ["create","edit"])` (app.go:598) establishes
    // that the caller has business here at all, and `userSave` then demands the
    // verb the press actually needs — create when `id` is 0, edit otherwise
    // (app.go, F-A-07). This case pins both directions.
    const roleName = `usercreate-${W.runId}`;
    const roleId = await createCustomRole(W.adminPage, roleName);
    await grantMatrix(W.adminPage, roleId, roleName, { perms: ['user:view', 'user:create'] });

    const subject = await createUserWithExactRoles(W.adminPage, `ucreate-${W.runId}`, W.runId, []);
    await setRolesByLabel(W.adminPage, subject.email, [new RegExp(`^${roleName}`)]);
    const session = await signIn(W.adminPage.context().browser()!, subject, W.adminPage.viewportSize());
    try {
      await session.page.goto('/users');
      await expect(
        session.page.locator('.pb-actions').getByRole('button', { name: 'Add user' }),
        'user:create renders the ＋ Add user control'
      ).toBeVisible();

      const created = `late-${W.runId}@example.test`;
      // role_ids, not the superseded account type. The create form asks for real
      // roles now: the old two-value field could not name a requester or an
      // approver, and its innocuous option was mapped to the role that settles
      // and voids payments.
      const probe = await probePost(session.page, '/users', {
        id: '0', email: created, name: 'Late', password: fixturePassword, active: 'on',
        role_ids: W.systemRoleIds.Requester
      });
      expect(
        probe.status,
        'the control is offered and its POST is honoured, because the route asks for the verb it renders — F-A-07'
      ).toBe(303);
      // Reproduced a second way: the user really exists.
      await W.adminPage.goto('/users');
      await expect(
        W.adminPage.locator('tr', { hasText: created }),
        'and the row is on the users screen'
      ).toHaveCount(1);

      // The other direction, which is what makes this a permission and not a
      // formality: user:create is not user:edit, so the same subject cannot
      // rewrite somebody who already exists.
      const victim = await userIdOf(W.adminPage, W.m2.subject.email);
      const edit = await probePost(session.page, '/users', {
        id: victim, name: 'Renamed by a creator', active: 'on', role: 'data_entry'
      });
      expect(edit.status, 'while editing an existing user needs user:edit, which this role does not hold').toBe(403);
      expect(edit.body.includes(GATE_REFUSAL), 'and says so in the permission gate’s own words').toBe(true);
      await W.adminPage.goto('/users');
      await expect(
        W.adminPage.locator('tr', { hasText: W.m2.subject.email }),
        'and the victim row still carries its own name'
      ).toContainText(W.m2.subject.name);
    } finally {
      await session.close();
    }
  });

  test('TC-A-123 — nobody may plant a file on a linked payment, whatever they hold', async () => {
    // Two rules stand between a caller and the evidence on a payment, and this
    // case pins both, in the order the handler asks them.
    //
    // F-A-03 was the original subject: POST /payments/{id}/attachments checked
    // attachment:create and nothing else, and attachment:create is a seeded
    // Requester grant — so a stranger could append to the proof on a payment
    // whose request they cannot even read. `attachmentUpload` now resolves the
    // payment and runs `canReadPayment` FIRST (app.go:1072-1083), the same row
    // scope the download route runs, and refuses with the same 404 so the write
    // route is not an enumeration oracle either.
    //
    // F-D-08 is the second rule and applies to everyone who gets past the first:
    // `store.AddAttachment` refuses a payment whose request_id is set, joining
    // edit and void in treating a linked payment as the request's immutable
    // outcome, and `paymentCreate` refuses a payment without a request — so
    // every payment this product can create is linked, and the route is a 400.
    //
    // That ordering is why the fix was made anyway rather than declared
    // unreachable (repair decision 4): the day a free-standing payment becomes
    // creatable again, F-D-08 stops covering for F-A-03, and the ownership check
    // is already there.
    const before = await W.a2.page.request.get(new URL(`/payments/${W.payId}`, W.baseURL).toString());
    const beforeBody = await before.text();
    const beforeCount = (beforeBody.match(/href="\/attachments\/\d+"/g) ?? []).length;
    expect(beforeCount, 'the settlement wrote its own bank advice and nothing else').toBe(1);

    // Three callers who all hold attachment:create: the stranger F-A-03 was
    // about, the accountant who entered the payment, and an administrator. Every
    // one of them is refused, and the two who are refused differently is the
    // whole point — the Requester never reaches the linked-payment rule, because
    // the ownership check turns them away first, and turns them away without
    // admitting the payment is there to be refused.
    const planters: Array<[string, Session, number, string]> = [
      [
        'a Requester who cannot even read the request', W.callers['C-req'], 404,
        'The requested attachment was not found'
      ],
      [
        'the Accounts caller whose role entered it', W.a2, 400,
        'a payment linked to a request cannot receive a new attachment'
      ],
      [
        'an administrator holding every grant', W.callers['C-adm'], 400,
        'a payment linked to a request cannot receive a new attachment'
      ]
    ];
    for (const [who, session, want, sentence] of planters) {
      const token = await csrfToken(session.context);
      const response = await session.page.request.post(
        new URL(`/payments/${W.payId}/attachments`, W.baseURL).toString(),
        {
          multipart: {
            csrf: token,
            attachment: {
              name: `planted-${W.runId}.pdf`,
              mimeType: 'application/pdf',
              buffer: Buffer.from('%PDF-1.4\nan attempt to append to a settled payment’s evidence\n%%EOF')
            }
          },
          maxRedirects: 0,
          failOnStatusCode: false
        }
      );
      expect(
        response.status(),
        `${who}: the upload is refused — got ${response.status()}`
      ).toBe(want);
      expect(
        (await response.text()).includes(sentence),
        `${who}: and the refusal is "${sentence}" — the rule that actually stopped them`
      ).toBe(true);
    }

    // Reproduced a second way: the evidence on the payment is untouched.
    await W.a2.page.goto(`/payments/${W.payId}`);
    await expect(
      W.a2.page.getByText(`planted-${W.runId}.pdf`),
      'no attempt left a trace on the payment'
    ).toHaveCount(0);
    const after = await W.a2.page.request.get(new URL(`/payments/${W.payId}`, W.baseURL).toString());
    const afterCount = ((await after.text()).match(/href="\/attachments\/\d+"/g) ?? []).length;
    expect(afterCount, 'the proof list is exactly as the settlement left it').toBe(beforeCount);

    // And this is the same answer S12 already gave to an edit, which is the point:
    // a payment the screen calls read-only refuses every kind of change alike.
    const edit = await probePost(W.callers['C-acc'].page, `/payments/${W.payId}/edit`, {
      amount: '1.00', paid_on: W.today, head_id: W.headId, payment_mode: 'bank_transfer'
    });
    expect(
      edit.status,
      'a linked payment refuses an edit exactly as firmly as it now refuses a new document (S12)'
    ).toBe(400);
  });

  test('TC-A-124 — a 12-character letters-only password is a 400 naming the rule it broke', async () => {
    // F-A-11: two password rules disagreed about who owned the refusal. The app
    // layer checked length alone (">= 12 characters"), then `userSave` called
    // `auth.HashPassword` → `auth.ValidatePassword` (auth.go), which demands a
    // letter AND a digit. A 12-character letters-only password passed the first
    // rule, failed the second, and came back as `respondError(500, "The password
    // could not be secured.")` — a server fault reported for input the server
    // understood perfectly, telling the administrator nothing about the missing
    // digit and implying the fault was the product's.
    //
    // `validatePassword` now delegates to `auth.ValidatePassword` and adds the
    // 12-character minimum on top (app.go), so both rules are checked in the
    // one place that can still answer 400, and every rejection names its rule.
    const lettersOnly = 'abcdefghijkl';
    const create = await probePost(W.callers['C-adm'].page, '/users', {
      id: '0', email: `pw500-${W.runId}@example.test`, name: `PW ${W.runId}`,
      password: lettersOnly, active: 'on', role: 'data_entry'
    });
    expect(
      create.status,
      'input the server understood and refused is a 400, not a server fault — F-A-11'
    ).toBe(400);
    expect(
      create.body.includes('must include a letter and a number'),
      'and the message names the rule that was broken, so the administrator can fix it'
    ).toBe(true);
    expect(
      create.body.includes('The password could not be secured'),
      'and never blames the hasher for input it never reached'
    ).toBe(false);

    // The same on the reset path, which is the one an administrator uses daily.
    const victim = await userIdOf(W.adminPage, W.callers['C-req'].subject.email);
    const reset = await probePost(W.callers['C-adm'].page, '/users', {
      id: victim, name: W.callers['C-req'].subject.name, password: lettersOnly, active: 'on', role: 'data_entry'
    });
    expect(reset.status, 'and resetting an existing user’s password behaves the same way — F-A-11').toBe(400);
    expect(reset.body.includes('must include a letter and a number'), 'with the same usable message').toBe(true);

    // The control: eleven characters is caught by the app's own rule, correctly.
    const tooShort = await probePost(W.callers['C-adm'].page, '/users', {
      id: '0', email: `pwshort-${W.runId}@example.test`, name: `PW2 ${W.runId}`,
      password: 'abcdefghij1', active: 'on', role: 'data_entry'
    });
    expect(tooShort.status, 'an 11-character password is a 400 with a usable message').toBe(400);
    expect(tooShort.body.includes('at least 12 characters'), 'naming the rule it broke').toBe(true);

    // And the Requester's password still works, so the failed reset changed nothing.
    const stillWorks = await probeGet(W.callers['C-req'].page, '/requests');
    expect(stillWorks.status, 'the refused reset left the victim’s session and credentials intact').toBe(200);
  });

  test('TC-A-122 — a request cannot be routed to an approver who cannot approve it', async () => {
    // F-A-08 was a request that could be created and then decided by nobody.
    // `ListApprovers` only offers users holding approval:approve, so the form
    // could not produce a bad manager_id — but the form's narrowed <select> was
    // the ONLY enforcement, and a hand-rolled POST walked past it. The result was
    // a stranded request: the named "approver" refused by the route gate because
    // they hold no approval:approve, and every real manager refused by the
    // ownership check because they are not this request's manager. Withdrawal
    // was the only exit, and only the raiser could take it.
    //
    // `requireApprover` (store/requests.go:414-426) now re-runs ListApprovers'
    // own query against the id that was actually submitted, from
    // `validateRequestRefs` — so the guard is in the store, where a forged form
    // cannot reach around it.
    const strandedManager = await userIdOf(W.adminPage, W.callers['C-req'].subject.email);
    const requester = W.r2;
    const title = `Stranded ${W.runId}`;
    const probe = await probePost(requester.page, '/requests', requestForm({
      title, managerId: strandedManager, amount: '113.00'
    }));
    expect(probe.status, 'a manager_id holding no approval:approve is refused on create — F-A-08').toBe(400);
    expect(
      probe.body.includes('that person cannot approve requests'),
      'and the refusal names the reason, rather than a generic validation failure'
    ).toBe(true);

    // Nothing was created, so there is no stranded request to rescue.
    await requester.page.goto('/requests?bucket=all');
    await expect(
      requester.page.getByText(title, { exact: false }),
      'and no request was raised — the refusal is the whole outcome'
    ).toHaveCount(0);

    // The same guard on the edit path, which is the other way in: rerouting a
    // request by editing it strands it exactly as raising it that way did
    // (store/requests.go:915-918). `tPending` is R2's own pending request, so R2
    // may edit it — and this edit is refused, so it stays exactly as it was.
    const reroute = await probePost(requester.page, `/requests/${W.tPending}/edit`, requestForm({
      title: W.foreignTitle, managerId: strandedManager, amount: '101.00'
    }));
    expect(reroute.status, 'and rerouting an existing request to a non-approver is refused too').toBe(400);
    expect(
      reroute.body.includes('that person cannot approve requests'),
      'for the same named reason'
    ).toBe(true);

    // Reproduced where it counts: the request's approver is unchanged, so its
    // own manager can still decide it — which is what "not stranded" means.
    await W.m2.page.goto('/approvals');
    await expect(
      W.m2.page.locator(`a.req-card[href="/requests/${W.tPending}"]`),
      'the request is still in its own approver’s queue, undamaged by the attempt'
    ).toHaveCount(1);
  });

  // =========================================================================
  // 9. Fixwave 2026-09-25, cluster A — the roles screen and the data scope.
  // =========================================================================

  test('rbac-1 — an Advanced box unticked under a ticked cell is revoked, and the cell shows the partial state', async () => {
    const roleName = `partial-${W.runId}`;
    const roleId = await createCustomRole(W.adminPage, roleName);
    // The bare View cell, with no Advanced box behind it, expands to both.
    await grantMatrix(W.adminPage, roleId, roleName, { cells: ['requests:view'] });

    await W.adminPage.goto(`/roles?role=${roleId}`);
    await openAdvanced(W.adminPage);
    const cell = W.adminPage.locator('.perm-table input[name="cell"][value="requests:view"]');
    const attachmentView = W.adminPage.locator('.perm-table input[name="perm"][value="attachment:view"]');
    const requestView = W.adminPage.locator('.perm-table input[name="perm"][value="request:view"]');
    await expect(cell, 'both boxes behind View are held, so the cell is fully ticked').toBeChecked();
    await expect(attachmentView).toBeChecked();

    await attachmentView.uncheck();
    await expect(cell, 'the cell follows its boxes down: unchecked, so it is not submitted').not.toBeChecked();
    expect(
      await cell.evaluate(node => (node as HTMLInputElement).indeterminate),
      'and indeterminate, so the reader can see the row is only partly held'
    ).toBe(true);

    await W.adminPage.getByRole('button', { name: 'Save role' }).click();
    await expect(W.adminPage).toHaveURL(new RegExp(`/roles\\?role=${roleId}$`));
    await openAdvanced(W.adminPage);
    await expect(attachmentView, 'the untick was saved — the Advanced boxes are the grant set').not.toBeChecked();
    await expect(requestView, 'and the box left ticked is still held').toBeChecked();
    await expect(cell, 'on load the half-held cell is unchecked').not.toBeChecked();
    expect(
      await cell.evaluate(node => (node as HTMLInputElement).indeterminate),
      'and drawn indeterminate from the server-rendered data-partial mark'
    ).toBe(true);

    // The other direction still works: ticking the cell ticks every box.
    await cell.check();
    await expect(attachmentView).toBeChecked();
    expect(await cell.evaluate(node => (node as HTMLInputElement).indeterminate)).toBe(false);
  });

  test('rbac-2 — a request scope of None lists and exports nothing, as the detail page already 404s', async ({ browser }) => {
    const roleName = `viewer-${W.runId}`;
    const roleId = await createCustomRole(W.adminPage, roleName);
    await grantMatrix(W.adminPage, roleId, roleName, { perms: ['request:view', 'grid:view'] });
    const subject = await createUserWithExactRoles(W.adminPage, `viewer-${W.runId}`, W.runId, []);
    await setRolesByLabel(W.adminPage, subject.email, [new RegExp(`^${roleName}`)]);
    const viewer = await signIn(browser, subject, W.adminPage.viewportSize());
    try {
      await viewer.page.goto('/requests?bucket=all');
      await expect(viewer.page.locator('.page-banner .sub'), 'the list is empty').toContainText('0 shown');
      await expect(viewer.page.locator('a.req-card'), 'no request card at all').toHaveCount(0);
      const csv = await probeGet(viewer.page, '/requests/export.csv?bucket=all');
      expect(csv.status, 'the export still answers (request:view is held)').toBe(200);
      expect(csv.body.includes('PR-2026-'), 'but carries no rows').toBe(false);
      await expectStatus(probeGet(viewer.page, `/requests/${W.tPending}`), 404, 'the detail page, as before');
    } finally {
      await viewer.close();
    }
  });

  test('rbac-4 — the scope pill highlight follows the selection', async () => {
    await W.adminPage.goto(`/roles?role=${W.systemRoleIds.Requester}`);
    const own = W.adminPage.locator('.perm-table label:has(input[name="scope_request"][value="own"])');
    const all = W.adminPage.locator('.perm-table label:has(input[name="scope_request"][value="all"])');
    await expect(own, 'Requester starts on Own, lit by the server').toHaveClass(/is-on/);
    await all.click();
    await expect(all.locator('input')).toBeChecked();
    await expect(all, 'the pill just chosen lights up').toHaveClass(/is-on/);
    await expect(own, 'and the old one goes out').not.toHaveClass(/is-on/);
  });

  test('rbac-5 — a row held only through Advanced shows partial cells and counts its grants', async () => {
    const roleName = `advanced-${W.runId}`;
    const roleId = await createCustomRole(W.adminPage, roleName);
    await grantMatrix(W.adminPage, roleId, roleName, {
      perms: ['request:view', 'request:create', 'approval:approve', 'attachment:create']
    });
    await W.adminPage.goto(`/roles?role=${roleId}`);
    for (const value of ['requests:view', 'requests:create', 'requests:approve']) {
      const cell = W.adminPage.locator(`.perm-table input[name="cell"][value="${value}"]`);
      await expect(cell, `${value} is not fully held`).not.toBeChecked();
      expect(await cell.evaluate(node => (node as HTMLInputElement).indeterminate), `${value} is drawn partial`).toBe(true);
    }
    // The accordion badge is the phone's only summary of the row.
    const desktop = W.adminPage.viewportSize();
    await W.adminPage.setViewportSize({ width: 390, height: 844 });
    try {
      const head = W.adminPage.locator('.perm-acc .pa-head', { hasText: 'Payment requests' });
      await expect(head.locator('.n'), 'four of the row’s fifteen actions are held — not "0 of 7"').toHaveText('4 of 15');
    } finally {
      if (desktop) await W.adminPage.setViewportSize(desktop);
    }
  });

  test('rbac-6 — the delete sheet for a held role explains itself instead of dead-ending on a 403', async () => {
    const roleName = `held-${W.runId}`;
    const roleId = await createCustomRole(W.adminPage, roleName);
    const subject = await createUserWithExactRoles(W.adminPage, `holder-${W.runId}`, W.runId, []);
    await setRolesByLabel(W.adminPage, subject.email, [new RegExp(`^${roleName}`)]);

    await W.adminPage.goto(`/roles?role=${roleId}`);
    await expect(W.adminPage.locator('.card-head .pill'), 'the count is a person, not "1 users"').toHaveText('1 user');
    await W.adminPage.getByRole('button', { name: 'Delete role' }).click();
    const sheet = W.adminPage.locator('#role-delete');
    await expect(sheet).toBeVisible();
    await expect(sheet, 'the sheet says why it cannot be deleted').toContainText('1 user holds this role, so it cannot be deleted yet');
    await expect(sheet.locator('button[type="submit"]'), 'and offers no Delete button').toHaveCount(0);
    await sheet.getByRole('link', { name: 'Open Users' }).click();
    await expect(W.adminPage, 'it sends the reader where the fix is').toHaveURL(/\/users$/);

    // Once nobody holds it, the deletion is offered and goes through.
    await setExactRoles(W.adminPage, subject.email, ['Requester']);
    await W.adminPage.goto(`/roles?role=${roleId}`);
    await W.adminPage.getByRole('button', { name: 'Delete role' }).click();
    await expect(sheet).toContainText('Nobody holds this role');
    await sheet.getByRole('button', { name: 'Delete role' }).click();
    await expect(W.adminPage).toHaveURL(/\/roles$/);
    await expect(W.adminPage.locator('.segmented a', { hasText: roleName })).toHaveCount(0);
  });
});

// ---------------------------------------------------------------------------
// The matrix runner.
// ---------------------------------------------------------------------------

function matrixTest(tc: string, gateLabel: string, rows: () => Row[]) {
  test(`${tc} — ${gateLabel} × five callers`, async () => {
    const callers: CallerId[] = ['C-anon', 'C-req', 'C-mgr', 'C-acc', 'C-adm'];
    for (const row of rows()) {
      const path = row.path();
      for (const caller of callers) {
        const expected = expectedFor(row, caller);
        const probe = await probeCell(caller, row.method, path);
        const failure = cellFailure(probe, expected);
        expect
          .soft(
            failure,
            `${row.id} ${row.method} ${path} as ${caller} (gate ${row.gate}): expected ${describeExpected(expected)}` +
              (failure ? ` — ${failure}` : '')
          )
          .toBeNull();
      }
    }
  });
}

async function probeCell(caller: CallerId, method: 'GET' | 'POST', path: string): Promise<Probe> {
  const page = caller === 'C-anon' ? W.anonPage : W.callers[caller].page;
  const url = new URL(path, W.baseURL).toString();
  // Every POST cell is probed with NO csrf field on purpose: the CSRF check sits
  // inside the permission gate, so a permitted caller is refused by withCSRF and
  // nothing is written. See the file header.
  return method === 'GET' ? probeGet(page, url) : probePost(page, url, {}, { csrf: 'omit' });
}

// ---------------------------------------------------------------------------
// Building the world.
// ---------------------------------------------------------------------------

async function buildWorld(browser: Browser, baseURL: string): Promise<World> {
  const runId = `perm-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
  const adminContext = await browser.newContext({ baseURL });
  const adminPage = await adminContext.newPage();
  await login(adminPage, admin.email, admin.password);

  const anonContext = await browser.newContext({ baseURL });
  const anonPage = await anonContext.newPage();

  const today = localToday();

  // The five callers, each holding exactly one role, plus the role-less probe.
  const callers = {
    'C-req': await asRole(adminPage, browser, runId, ['Requester'], `c-req-${runId}`),
    'C-mgr': await asRole(adminPage, browser, runId, ['Manager'], `c-mgr-${runId}`),
    'C-acc': await asRole(adminPage, browser, runId, ['Accounts'], `c-acc-${runId}`),
    'C-adm': await asRole(adminPage, browser, runId, ['Admin'], `c-adm-${runId}`),
    'C-none': await asRole(adminPage, browser, runId, [], `c-none-${runId}`)
  } as Record<Exclude<CallerId, 'C-anon'>, Session>;

  // The foils: the people who own the objects the callers probe.
  const r2 = await asRole(adminPage, browser, runId, ['Requester'], `foil-req-${runId}`);
  const m2 = await asRole(adminPage, browser, runId, ['Manager'], `foil-mgr-${runId}`);
  const a2 = await asRole(adminPage, browser, runId, ['Accounts'], `foil-acc-${runId}`);

  const world: World = {
    runId, baseURL, today, adminPage,
    adminClose: () => adminContext.close(),
    anonPage,
    callers, r2, m2, a2,
    projectId: '', headId: '', vendorId: '', customRoleId: '',
    systemRoleIds: { Requester: '', Manager: '', Accounts: '', Admin: '' },
    tPending: 0, tApproved: 0, tProc: 0, tPaid: 0, tPartial: 0, payId: 0, payAttId: 0,
    foreignTitle: `Foreign ${runId}`,
    ownPending: 0, ownApproved: 0, ownTitle: `Mine ${runId}`, ownReqAttId: 0
  };
  W = world;

  // Masters: the project and head every request and payment needs, read off the
  // requester's own form so the ids are the ones a real submission would carry.
  await r2.page.goto('/requests/new?type=reimbursement');
  world.projectId = await optionValue(r2.page, '#project', 'Operations');
  world.headId = await optionValue(r2.page, '#head', 'Operations / Office Rent');
  const m2Id = await optionValue(r2.page, 'select[name="manager_id"]', m2.subject.name);

  world.vendorId = await ensureVendor(adminPage, `Audit Vendor ${runId}`);
  world.systemRoleIds = await readSystemRoleIds(adminPage);
  world.customRoleId = await createCustomRole(adminPage, `matrix-target-${runId}`);

  // The five foreign requests, all raised by R2 and routed to M2.
  world.tPending = await raiseRequest(r2.page, { title: world.foreignTitle, managerId: m2Id, amount: '101.00' });
  world.tApproved = await raiseRequest(r2.page, { title: `ForeignApproved ${runId}`, managerId: m2Id, amount: '102.00' });
  world.tProc = await raiseRequest(r2.page, { title: `ForeignProc ${runId}`, managerId: m2Id, amount: '103.00' });
  world.tPaid = await raiseRequest(r2.page, { title: `ForeignPaid ${runId}`, managerId: m2Id, amount: '104.00' });
  world.tPartial = await raiseRequest(r2.page, { title: `ForeignPartial ${runId}`, managerId: m2Id, amount: '105.00' });

  for (const [id, amount] of [
    [world.tApproved, '102.00'], [world.tProc, '103.00'], [world.tPaid, '104.00'], [world.tPartial, '105.00']
  ] as Array<[number, string]>) {
    await expectStatus(probePost(m2.page, `/requests/${id}/approve`, { approved_amount: amount }), 303,
      `approving request ${id}`);
  }

  // A2 reserves all three payable requests, settles one, part-pays another, and
  // keeps the third reserved so the reservation screens have a live target.
  for (const id of [world.tProc, world.tPaid, world.tPartial]) {
    await expectStatus(probePost(a2.page, `/requests/${id}/record-payment`, {}), 303, `reserving request ${id}`);
  }
  // The settled payment is born carrying its bank advice — the object F-A-01 is
  // about. The document rides inside the settlement's own multipart POST, which
  // `RecordPaymentForRequest` writes in the same transaction as the payment
  // (internal/store/store.go:1011-1015).
  //
  // It cannot be added afterwards. This fixture used to POST
  // /payments/{id}/attachments once the payment existed; F-D-08's fix makes that
  // request a 400, because `store.AddAttachment` now refuses a payment whose
  // request_id is set (:1403-1405), joining edit and void in treating a linked
  // payment as the request's immutable outcome. That behaviour is correct — see
  // TC-A-123 and audit-d's TC-D-095 — so the fixture changed, not the product.
  // `fixtures.settlePayment` offers the same `attachment` option through the
  // screen; this is that request hand-rolled, because the world is built over
  // HTTP rather than through the UI.
  const advice = `bank-advice-${runId}.pdf`;
  const paid = await expectStatus(settleWithAttachment(a2.page, {
    request_id: String(world.tPaid), amount: '104.00', paid_on: today, head_id: world.headId,
    payment_mode: 'bank_transfer', reference_no: `UTR-${runId}-paid`, settlement: 'settled'
  }, advice), 303, 'recording the settled payment and its bank advice');
  world.payId = Number(/\/payments\/(\d+)/.exec(paid.location ?? '')?.[1] ?? 0);
  if (!world.payId) throw new Error(`could not read the payment id from ${paid.location}`);
  world.payAttId = await paymentAttachmentId(a2.page, world.payId, advice);

  await expectStatus(probePost(a2.page, '/payments', {
    request_id: String(world.tPartial), amount: '50.00', paid_on: today, head_id: world.headId,
    payment_mode: 'bank_transfer', reference_no: `UTR-${runId}-part`, settlement: 'partial',
    partial_reason: 'Only half was released by the bank.'
  }), 303, 'recording the partial payment');

  // The two requests C-req owns, one of them carrying its own document.
  const cReqManagerId = await managerIdFor(callers['C-req'].page, m2.subject.name);
  world.ownPending = await raiseRequest(callers['C-req'].page, {
    title: world.ownTitle, managerId: cReqManagerId, amount: '106.00'
  });
  world.ownApproved = await raiseRequest(callers['C-req'].page, {
    title: `MineApproved ${runId}`, managerId: cReqManagerId, amount: '107.00'
  });
  await expectStatus(probePost(m2.page, `/requests/${world.ownApproved}/approve`, { approved_amount: '107.00' }), 303,
    'approving the requester’s own request');
  world.ownReqAttId = await requestAttachmentId(callers['C-req'].page, world.ownPending, `invoice-${runId}.pdf`);

  return world;
}

// ---------------------------------------------------------------------------
// Small helpers. All local to this spec — audit-support.ts and fixtures.ts are
// untouched.
// ---------------------------------------------------------------------------

async function expectStatus(probe: Promise<Probe>, want: number, what: string) {
  const p = await probe;
  if (p.status !== want) {
    throw new Error(`${what}: expected ${want}, got ${p.outcome}. Body: ${p.body.replace(/\s+/g, ' ').slice(0, 400)}`);
  }
  return p;
}

/**
 * Today, in the local zone. The server and the runner are the same machine, and
 * `validMonthOrCurrent` defaults the ledger to `time.Now().Format("2006-01")`,
 * so this is the month a new payment has to land in to be visible there.
 */
function localToday(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/**
 * Opens all nine "Advanced — every permission behind this row" disclosures.
 *
 * They are `<details>` and start closed, so the 66 canonical checkboxes inside
 * them are present but not visible, and Playwright refuses to tick an invisible
 * control. Clicking each summary is what a person does.
 */
async function openAdvanced(page: Page) {
  const summaries = page.locator('.perm-table .perm-advanced summary');
  const count = await summaries.count();
  for (let i = 0; i < count; i++) {
    const details = page.locator('.perm-table .perm-advanced details').nth(i);
    if (await details.evaluate(node => (node as HTMLDetailsElement).open)) continue;
    await summaries.nth(i).click();
  }
}

async function optionValue(page: Page, selector: string, label: string): Promise<string> {
  const value = await page.locator(`${selector} option`, { hasText: label }).first().getAttribute('value');
  if (!value) throw new Error(`no option labelled "${label}" in ${selector}`);
  return value;
}

async function managerIdFor(page: Page, managerName: string): Promise<string> {
  await page.goto('/requests/new?type=reimbursement');
  return optionValue(page, 'select[name="manager_id"]', managerName);
}

interface RaiseOpts {
  title: string;
  managerId: string;
  amount: string;
  extra?: Record<string, string>;
}

/** The form a reimbursement request needs — no vendor, so no vendor:view. */
function requestForm(opts: RaiseOpts): Record<string, string> {
  return {
    treatment: 'budget',
    type: 'reimbursement',
    project_id: W.projectId,
    head_id: W.headId,
    short_title: opts.title,
    amount: opts.amount,
    purpose: `Audit fixture for ${opts.title}.`,
    expense_date: W.today,
    manager_id: opts.managerId,
    ...(opts.extra ?? {})
  };
}

/** Raises a reimbursement request through POST /requests and returns its id. */
async function raiseRequest(page: Page, opts: RaiseOpts): Promise<number> {
  const probe = await expectStatus(probePost(page, '/requests', requestForm(opts)), 303, `raising "${opts.title}"`);
  const id = Number(/\/requests\/(\d+)\/submitted/.exec(probe.location ?? '')?.[1] ?? 0);
  if (!id) throw new Error(`could not read the new request id from ${probe.location}`);
  return id;
}

/**
 * Raises, approves and reserves a fresh request, held by A2.
 *
 * Used by the tests that genuinely change a reservation: the shared `tProc` is
 * read by nine matrix rows and must stay reserved for all of them.
 */
async function freshReservedRequest(tag: string): Promise<number> {
  const managerId = await managerIdFor(W.r2.page, W.m2.subject.name);
  const id = await raiseRequest(W.r2.page, {
    title: `${tag} ${W.runId}-${Date.now()}`, managerId, amount: '120.00'
  });
  await expectStatus(probePost(W.m2.page, `/requests/${id}/approve`, { approved_amount: '120.00' }), 303,
    `approving ${tag} request ${id}`);
  await expectStatus(probePost(W.a2.page, `/requests/${id}/record-payment`, {}), 303,
    `reserving ${tag} request ${id}`);
  return id;
}

/**
 * POSTs a settlement as `multipart/form-data`, so the payment's proof is written
 * by the settlement itself.
 *
 * `POST /payments` takes an `attachment` part and hands it to
 * `RecordPaymentForRequest`, which inserts the `payment_attachments` row inside
 * the settlement's transaction. Since F-D-08 that is the ONLY way a linked
 * payment ever acquires a document, so it is the only way this world can plant
 * the bank advice the F-A-01 probes read.
 *
 * `probePost` cannot do this — it sends a URL-encoded body, and
 * `stageUploadedAttachment` reads a non-multipart request as "no file at all" —
 * so the Probe is assembled here from the raw response, exactly as
 * `audit-support.describe` would.
 */
async function settleWithAttachment(
  page: Page,
  form: Record<string, string>,
  filename: string
): Promise<Probe> {
  const response = await page.request.post(new URL('/payments', W.baseURL).toString(), {
    multipart: {
      ...form,
      csrf: await csrfToken(page.context()),
      attachment: { name: filename, mimeType: 'application/pdf', buffer: Buffer.from(`%PDF-1.4\nadvice body ${filename}\n%%EOF`) }
    },
    maxRedirects: 0,
    failOnStatusCode: false
  });
  const status = response.status();
  const location = response.headers()['location'] ?? null;
  const body = status >= 300 && status < 400 ? '' : await response.text().catch(() => '');
  return { status, location, body, outcome: location ? `${status} → ${location}` : String(status) };
}

/** The `payment_attachments` id of a named document, read off the payment screen. */
async function paymentAttachmentId(page: Page, paymentId: number, filename: string): Promise<number> {
  await page.goto(`/payments/${paymentId}`);
  const href = await page
    .locator('.file-row', { hasText: filename })
    .locator('a[href^="/attachments/"]')
    .first()
    .getAttribute('href');
  const id = Number(/\/attachments\/(\d+)/.exec(href ?? '')?.[1] ?? 0);
  if (!id) throw new Error(`could not find ${filename} on payment ${paymentId}`);
  return id;
}

/**
 * Puts a document on a request through the real edit screen and returns the id
 * the detail screen's Download link carries — a `request_attachments` id.
 *
 * THE LINK IS SCOPED TO THE REQUEST, and must be read that way. F-A-05's fix
 * split one download route into two: `GET /attachments/{id}` reads
 * `payment_attachments` and now serves payment documents only, while a request
 * document is served by `GET /requests/{id}/attachments/{attachmentID}`
 * (internal/app/app.go:467-468), which checks the document really belongs to the
 * request in the path. `request_detail` renders the request's own route
 * accordingly (internal/app/templates.go:319), so an `a[href^="/attachments/"]`
 * selector finds nothing here — and on a screen that also lists payment proofs
 * it would find the wrong table's id, which is the very confusion the split
 * exists to end.
 */
async function requestAttachmentId(page: Page, requestId: number, filename: string): Promise<number> {
  await page.goto(`/requests/${requestId}/edit`);
  // The uploader's input is nameless to a screen reader beyond its own label, so
  // it is addressed by name attribute the way fixtures.settlePayment does.
  await page.locator('input[name="attachment"]').setInputFiles({
    name: filename, mimeType: 'application/pdf', buffer: Buffer.from(`%PDF-1.4\ninvoice body ${filename}\n%%EOF`)
  });
  await page.getByRole('button', { name: /^Save and notify/ }).click();
  await page.waitForURL(new RegExp(`/requests/${requestId}$`));
  const href = await page
    .locator(`a[href^="/requests/${requestId}/attachments/"]`)
    .first()
    .getAttribute('href');
  const id = Number(/\/attachments\/(\d+)/.exec(href ?? '')?.[1] ?? 0);
  if (!id) throw new Error(`no document rendered on request ${requestId} after upload`);
  return id;
}

async function ensureVendor(page: Page, name: string): Promise<string> {
  await page.goto('/vendors/new');
  await page.getByLabel('Vendor name').fill(name);
  await page.getByLabel('Type').selectOption('company');
  await page.getByRole('button', { name: 'Save vendor' }).click();
  await page.waitForURL(url => !url.pathname.endsWith('/vendors/new'));
  await page.goto('/vendors');
  const href = await page.locator('a[href^="/vendors/"]', { hasText: name }).first().getAttribute('href');
  const id = /\/vendors\/(\d+)/.exec(href ?? '')?.[1];
  if (!id) throw new Error(`vendor "${name}" was not created`);
  return id;
}

async function readSystemRoleIds(page: Page): Promise<Record<RoleName, string>> {
  await page.goto('/roles');
  const out: Record<string, string> = {};
  for (const name of ['Requester', 'Manager', 'Accounts', 'Admin']) {
    const href = await page.locator('.segmented a', { hasText: name }).first().getAttribute('href');
    const id = /role=(\d+)/.exec(href ?? '')?.[1];
    if (!id) throw new Error(`system role "${name}" is missing from /roles`);
    out[name] = id;
  }
  return out as Record<RoleName, string>;
}

async function createCustomRole(page: Page, name: string): Promise<string> {
  const probe = await expectStatus(probePost(page, '/roles/new', { name, description: '' }), 303,
    `creating role "${name}"`);
  const id = /role=(\d+)/.exec(probe.location ?? '')?.[1];
  if (!id) throw new Error(`could not read the new role id from ${probe.location}`);
  return id;
}

/**
 * Writes a role's matrix through POST /roles — the screen's own form, posted
 * directly because `probePost` cannot repeat a field name and the matrix needs
 * several `cell`/`perm` values at once. Uses URLSearchParams for that reason.
 */
async function grantMatrix(
  page: Page,
  roleId: string,
  name: string,
  grants: { cells?: string[]; perms?: string[]; scopes?: Record<string, string> }
) {
  const body = new URLSearchParams();
  body.append('csrf', await csrfToken(page.context()));
  body.append('role_id', roleId);
  body.append('name', name);
  body.append('description', '');
  for (const cell of grants.cells ?? []) body.append('cell', cell);
  for (const perm of grants.perms ?? []) body.append('perm', perm);
  for (const [resource, scope] of Object.entries(grants.scopes ?? {})) body.append(`scope_${resource}`, scope);

  const response = await page.request.post(new URL('/roles', W.baseURL).toString(), {
    data: body.toString(),
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    maxRedirects: 0,
    failOnStatusCode: false
  });
  if (response.status() !== 303) {
    throw new Error(`granting role ${roleId}: got ${response.status()} — ${(await response.text()).slice(0, 300)}`);
  }
}

/** setExactRoles, but for role labels that are not the four system names. */
async function setRolesByLabel(page: Page, email: string, labels: RegExp[]) {
  await page.goto('/users');
  await page.locator('tr', { hasText: email }).getByRole('button', { name: 'Edit' }).click();
  const edit = page.locator('.overlay:not([hidden])');
  await expect(edit).toBeVisible();
  const boxes = edit.getByRole('checkbox');
  for (let i = 0; i < (await boxes.count()); i++) {
    const box = boxes.nth(i);
    if ((await box.getAttribute('name')) === 'role_ids') await box.uncheck();
  }
  for (const label of labels) await edit.getByRole('checkbox', { name: label }).check();
  await edit.getByRole('button', { name: 'Save user' }).click();
  await expect(page).toHaveURL(/\/users$/);
}

async function userIdOf(page: Page, email: string): Promise<string> {
  await page.goto('/users');
  const attr = await page
    .locator('tr', { hasText: email })
    .locator('button[data-open^="user-"]')
    .first()
    .getAttribute('data-open');
  const id = /user-(\d+)/.exec(attr ?? '')?.[1];
  if (!id) throw new Error(`no user row for ${email}`);
  return id;
}
