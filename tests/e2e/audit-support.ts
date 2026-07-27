/**
 * Shared support for the QA audit suite (`audit-*.spec.ts`).
 *
 * These helpers exist because the audit asks a question the original suite does
 * not: "what can a person holding exactly one role do?" Answering it needs a
 * user whose role set is exact, and a way to see a bare HTTP status rather than
 * a rendered page.
 *
 * WHY THIS FILE AND NOT fixtures.ts — `fixtures.ts` is shipped, green and
 * relied on by ten spec files; the audit adds to the repo without touching it.
 * Import from both.
 *
 * THE TRAP THIS FILE EXISTS TO AVOID. `store.CreateUser` calls
 * `assignDefaultRoleTx` (internal/store/migrations.go:501), which gives every
 * new user the **Accounts** system role — "Admin" only when the legacy role
 * field is literally `admin`. The "＋ Add user" sheet posts no `role_ids`, so a
 * freshly created user is an accountant holding payment create/edit/void/
 * process/settle/mark_partial/hold and request view-with-scope-all.
 *
 * The edit sheet renders the current assignment pre-checked, and
 * `store.SetUserRoles` (internal/store/permissions.go:460) DELETEs the whole set
 * before re-inserting, so the checkbox state at save time is authoritative.
 * Ticking "Manager" on a new user therefore yields Accounts **+** Manager —
 * which is what `fixtures.createApproverUser` does. For a permission probe that
 * is useless: the subject holds two roles' grants and every refusal you expected
 * turns into a 200.
 *
 * `createUserWithExactRoles` unchecks every box before ticking the ones asked
 * for, so the subject holds precisely the grants under test.
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
  await sheet.getByLabel('Password').fill(fixturePassword);
  await sheet.getByRole('button', { name: 'Add User' }).click();
  await expect(adminPage).toHaveURL(/\/users$/);

  await setExactRoles(adminPage, email, roles);
  return { email, name, password: fixturePassword, roles };
}

/**
 * Rewrites an existing user's role set to exactly `roles`.
 *
 * Unchecking first is the whole point — see the file header. Kept separate from
 * creation so a test can re-grant mid-flight and prove that a permission change
 * takes effect on the next request rather than at next login.
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
 * `internal/auth/auth.go:187` sets it with `HttpOnly: false` and the templates
 * echo the same value into a hidden `csrf` field, so the cookie is the token —
 * which is what makes a forged POST testable without scraping a page.
 */
export async function csrfToken(context: BrowserContext): Promise<string> {
  const cookie = (await context.cookies()).find(c => c.name === 'fervid_csrf');
  if (!cookie?.value) throw new Error('no fervid_csrf cookie — is this context signed in?');
  return cookie.value;
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
  return describe(await page.request.get(path, { maxRedirects: 0, failOnStatusCode: false }));
}

/**
 * POSTs a form through a signed-in context, attaching a valid CSRF token unless
 * told otherwise.
 *
 * `csrf: 'omit'` sends no token, `csrf: 'bogus'` sends a wrong one — both must
 * be refused by `withCSRF` (internal/auth/auth.go:171). Anything else is used
 * verbatim as the token, which is how a cross-session token is tested.
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
  return describe(await page.request.post(path, { form: body, maxRedirects: 0, failOnStatusCode: false }));
}

/** An unauthenticated probe — no cookies at all. */
export async function probeAnonymous(browser: Browser, path: string, baseURL: string): Promise<Probe> {
  const context = await browser.newContext();
  try {
    return describe(
      await context.request.get(new URL(path, baseURL).toString(), { maxRedirects: 0, failOnStatusCode: false })
    );
  } finally {
    await context.close();
  }
}

async function describe(response: APIResponse): Promise<Probe> {
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
