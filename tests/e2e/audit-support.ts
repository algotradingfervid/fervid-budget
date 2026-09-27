/**
 * Shared support for the QA audit suite (`audit-*.spec.ts`).
 *
 * The audit asks a question the shipped suite does not: "what can a person
 * holding exactly one role do?" Answering it needs a user whose role set is
 * exact, and a way to see a bare HTTP status rather than a rendered page.
 *
 * Why exact roles need care: the "Add user" sheet insists on at least one role,
 * and the user edit sheet replaces the whole role set with whatever boxes are
 * ticked when it is saved. A probe user holding one role more than intended
 * sees that role's grants too, so createUserWithExactRoles creates the user as
 * a Requester and then unticks every role before ticking the ones asked for
 * (possibly none).
 */
import { expect, type APIResponse, type Browser, type BrowserContext, type Page } from '@playwright/test';
import { fixturePassword, login, openNewUserSheet } from './fixtures';

export { fixturePassword, login };

/** The four seeded system roles, matched the way the edit sheet labels them. */
export const SystemRole = {
  Requester: /^Requester/,
  Manager: /^Manager/,
  Accounts: /^Accounts/,
  Admin: /^Admin/
} as const;

export type RoleName = keyof typeof SystemRole;

export interface Subject {
  email: string;
  name: string;
  password: string;
  roles: RoleName[];
}

/**
 * Creates a user holding EXACTLY the roles named — no more.
 *
 * Pass `[]` for a user with no role at all: signed in, permitted nothing. That
 * subject is the sharpest probe in the suite, because every route it can still
 * reach is a route with no effective gate.
 */
export async function createUserWithExactRoles(
  adminPage: Page,
  prefix: string,
  runId: string,
  roles: RoleName[]
): Promise<Subject> {
  const email = `${prefix}-${runId}@example.test`.toLowerCase();
  const name = `${prefix} ${runId}`;

  await adminPage.goto('/users');
  await openNewUserSheet(adminPage);
  const sheet = adminPage.locator('#user-new');
  await sheet.getByLabel('Email').fill(email);
  await sheet.getByLabel('Name').fill(name);
  // The create form now insists on at least one role, so the account cannot be
  // born able to do nothing. Requester is the least of them, and setExactRoles
  // rewrites the set immediately afterwards — including to the empty set, which
  // several of these probes depend on.
  await sheet.getByRole('checkbox', { name: /^Requester/ }).check();
  await sheet.getByLabel('Password').fill(fixturePassword);
  await sheet.getByRole('button', { name: 'Add User' }).click();
  await expect(adminPage).toHaveURL(/\/users$/);

  await setExactRoles(adminPage, email, roles);
  return { email, name, password: fixturePassword, roles };
}

/**
 * Rewrites an existing user's role set to exactly `roles`, unticking every
 * role first (see the file header). Separate from creation so a test can change
 * a user's roles mid-test and prove the change applies to their next request
 * rather than at their next sign-in.
 */
export async function setExactRoles(adminPage: Page, email: string, roles: RoleName[]) {
  await adminPage.goto('/users');
  await adminPage.locator('tr', { hasText: email }).getByRole('button', { name: 'Edit' }).click();
  const edit = adminPage.locator('.overlay:not([hidden])');
  await expect(edit).toBeVisible();

  const boxes = edit.getByRole('checkbox');
  for (let i = 0; i < (await boxes.count()); i++) {
    const box = boxes.nth(i);
    // The sheet also carries an "Active" checkbox; only role_ids may be cleared.
    if ((await box.getAttribute('name')) === 'role_ids') await box.uncheck();
  }
  for (const role of roles) await edit.getByRole('checkbox', { name: SystemRole[role] }).check();

  await edit.getByRole('button', { name: 'Save user' }).click();
  await expect(adminPage).toHaveURL(/\/users$/);
}

/** A signed-in browser context of its own, so several subjects can act at once. */
export async function signIn(browser: Browser, subject: Subject, viewport?: { width: number; height: number } | null) {
  const context = await browser.newContext({ viewport: viewport ?? undefined });
  const page = await context.newPage();
  await login(page, subject.email, subject.password);
  return { context, page, close: () => context.close() };
}

/** Creates a subject and signs it in, in one step. Remember to `close()`. */
export async function asRole(
  adminPage: Page,
  browser: Browser,
  runId: string,
  roles: RoleName[],
  prefix = roles.length ? roles.join('-').toLowerCase() : 'norole'
) {
  const subject = await createUserWithExactRoles(adminPage, prefix, runId, roles);
  const session = await signIn(browser, subject, adminPage.viewportSize());
  return { subject, ...session };
}

/**
 * The CSRF token for a context, read from the `fervid_csrf` cookie.
 *
 * The cookie is readable by scripts and the forms echo the same value in a
 * hidden `csrf` field, so the cookie is the token. That lets a test build a
 * valid (or deliberately forged) POST without scraping a page.
 */
export async function csrfToken(context: BrowserContext): Promise<string> {
  const cookie = (await context.cookies()).find(c => c.name === 'fervid_csrf');
  if (!cookie?.value) throw new Error('no fervid_csrf cookie — is this context signed in?');
  return cookie.value;
}

/**
 * The request's current revision, as the decision forms carry it.
 *
 * Approve, return, reject, cancel and the cancellation decision are all bound
 * to the request as the decider saw it: the form posts a hidden `revision`
 * field and the server refuses a missing or out-of-date one. The value does not
 * depend on who reads it, but only some screens render it, so this looks on
 * the request page (the approver's decision sheets), the cancellation screen
 * and the edit form, in that order, as `page` sees them.
 *
 * Read it immediately before the POST: any change to the request, including
 * one made by somebody else, gives it a new revision.
 */
export async function requestRevision(page: Page, requestId: number): Promise<string> {
  for (const path of [`/requests/${requestId}`, `/requests/${requestId}/cancellation`, `/requests/${requestId}/edit`]) {
    const response = await page.request.get(path, { maxRedirects: 0, failOnStatusCode: false });
    if (response.status() !== 200) continue;
    const match = /name="revision" value="([^"]+)"/.exec(await response.text());
    if (match) return match[1];
  }
  throw new Error(`request ${requestId}: no screen this user can open shows its revision`);
}

export interface Probe {
  status: number;
  location: string | null;
  body: string;
  /** `302 → /login` and friends, for readable matrix assertions. */
  outcome: string;
}

/**
 * Sends a request through a signed-in context WITHOUT following redirects, and
 * reports the bare outcome.
 *
 * Not following redirects is essential: an unauthenticated GET of a gated route
 * answers `302 → /login`, and a client that follows it reports `200` for the
 * login page — turning a refusal into an apparent success.
 */
export async function probeGet(page: Page, path: string): Promise<Probe> {
  return toProbe(await page.request.get(path, { maxRedirects: 0, failOnStatusCode: false }));
}

/**
 * POSTs a form through a signed-in context, attaching a valid CSRF token unless
 * told otherwise.
 *
 * `csrf: 'omit'` sends no token, `csrf: 'bogus'` sends a wrong one; the server
 * must refuse both. Anything else is used verbatim as the token, which is how a
 * token from another session is tested.
 */
export async function probePost(
  page: Page,
  path: string,
  form: Record<string, string> = {},
  opts: { csrf?: 'valid' | 'omit' | 'bogus' | string } = {}
): Promise<Probe> {
  const mode = opts.csrf ?? 'valid';
  const body: Record<string, string> = { ...form };
  if (mode === 'valid') body.csrf = await csrfToken(page.context());
  else if (mode === 'bogus') body.csrf = 'not-a-real-token';
  else if (mode !== 'omit') body.csrf = mode;
  return toProbe(await page.request.post(path, { form: body, maxRedirects: 0, failOnStatusCode: false }));
}

/** An unauthenticated probe — no cookies at all. */
export async function probeAnonymous(browser: Browser, path: string, baseURL: string): Promise<Probe> {
  const context = await browser.newContext();
  try {
    return toProbe(
      await context.request.get(new URL(path, baseURL).toString(), { maxRedirects: 0, failOnStatusCode: false })
    );
  } finally {
    await context.close();
  }
}

/** Reduces a response to its bare outcome, without following a redirect. */
export async function toProbe(response: APIResponse): Promise<Probe> {
  const status = response.status();
  const location = response.headers()['location'] ?? null;
  let body = '';
  // A redirect has no body worth reading, and a CSV/binary body is not text.
  if (status < 300 || status >= 400) body = await response.text().catch(() => '');
  return { status, location, body, outcome: location ? `${status} → ${location}` : String(status) };
}

/**
 * Asserts a probe matches one of the outcomes a matrix row allows.
 *
 * Several routes legitimately answer either of two statuses — a proof-of-absence
 * POST is `405` where the path exists and `404` where it does not — so the
 * matrix stores a set, and a failure message has to name what it actually got.
 */
export function expectOutcome(probe: Probe, allowed: Array<number | string>, what: string) {
  const ok = allowed.some(a => (typeof a === 'number' ? probe.status === a : probe.outcome.startsWith(a)));
  expect(ok, `${what}: expected one of [${allowed.join(', ')}], got ${probe.outcome}`).toBe(true);
}

/** True when the response is the app refusing rather than the app answering. */
export function isRefusal(probe: Probe) {
  return probe.status === 403 || probe.status === 401 || (probe.status === 302 && probe.location?.includes('/login') === true);
}
