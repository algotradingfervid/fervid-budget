/**
 * Area H — the UI at realistic data volume.
 *
 * WHY THIS FILE EXISTS. Every other spec in the repo, shipped and audit alike,
 * runs against a database holding a handful of rows. `ux.spec.ts` is the
 * project's quality gate and it is emphatic — "Fails on horizontal scroll … Do
 * not weaken it to pass — fix the screen" — but it only ever sees the seeded
 * four roles, a dozen requests and one vendor. Nothing asks what the screens do
 * once an organisation has actually used the product for a while.
 *
 * The question is not academic. This file was written because running the whole
 * audit suite against one shared database made `ux.spec.ts` fail with
 * `/roles: scrolls sideways by 806px` — not because any audit test touched the
 * grid, but because the audit had created a handful of roles and the roles
 * matrix grows a column per role. That was F-H-01: a product defect the shipped
 * gate could not see, found only by accident.
 *
 * F-H-01 is FIXED — `.segmented` now contains its own overflow
 * (web/static/fervid-ds.css:524-527) — so all three tests here pass, and the file
 * has become the thing it should always have been: the guard that says the roles
 * screen still fits at either width no matter how many roles exist.
 *
 * The measurement is deliberately the same one `ux.spec.ts` uses —
 * `document.documentElement.scrollWidth - window.innerWidth` — so a failure here
 * means exactly what a failure there would mean.
 */
import { test, expect } from './fixtures';
import { probePost, csrfToken } from './audit-support';

/** The shipped gate's own overflow measurement, so the two agree. */
async function sidewaysOverflow(page: import('@playwright/test').Page) {
  return page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
}

/**
 * Creates `n` roles by POSTing the real create form.
 *
 * Through the route rather than the screen because this is arranging a fixture,
 * not testing role creation — area A covers that. Names carry the runId because
 * `idx_roles_name_nocase` is unique and the e2e database is reused locally.
 */
async function createRoles(page: import('@playwright/test').Page, runId: string, n: number) {
  const names: string[] = [];
  const csrf = await csrfToken(page.context());
  for (let i = 0; i < n; i++) {
    const name = `Scale ${i} ${runId}`;
    const res = await probePost(page, '/roles/new', { name, description: `scale probe ${i}` }, { csrf });
    // 303 on success. A duplicate name would be 400 — that would mean the runId
    // collided, which is worth failing loudly rather than silently under-creating.
    expect(res.status, `creating role "${name}" should succeed, got ${res.outcome}`).toBe(303);
    names.push(name);
  }
  return names;
}

// No project guard: every test here sets the viewport it cares about, so the
// result is the same on either device project and a guard would only add a way
// to get the condition wrong. `scripts/run-audit.sh` selects chromium anyway.
test.describe('area H — the UI at data volume', () => {
  /**
   * TC-H-001 — the regression guard for F-H-01 at desktop width.
   *
   * The finding was that `/roles` grows one `.segmented` link per role with no
   * upper bound a template can enforce, so the strip pushed the whole page wide
   * and the project's own UX gate would have failed on it — `ux.spec.ts` forbids
   * horizontal page scroll outright and says "do not weaken it to pass — fix the
   * screen". The fix does exactly that: `.segmented` now carries
   * `max-width: 100%; overflow-x: auto` (web/static/fervid-ds.css:524-527), so the
   * strip scrolls inside its own box the way `.table-wrap`, `.grid-wrap` and
   * `.budget-grid` already do, and the page does not.
   *
   * The assertion below is unchanged and is the shipped gate's own measurement,
   * so this test now says: adding roles cannot reintroduce the overflow.
   */
  test('TC-H-001 — the roles matrix still fits its viewport once an organisation has a dozen roles', async ({
    adminPage,
    runId
  }) => {
    await adminPage.setViewportSize({ width: 1440, height: 900 });

    // The seeded four plus eight more. Eight is not a stress test — it is one
    // role per department, which is the arrangement the whole feature exists for.
    await createRoles(adminPage, runId, 8);

    await adminPage.goto('/roles');
    await expect(adminPage.locator('h1')).toBeVisible();

    const overflow = await sidewaysOverflow(adminPage);
    expect(
      overflow,
      `/roles must not scroll sideways at 1440px with 12 roles (ux.spec.ts forbids it), but it overflows by ${overflow}px — F-H-01`
    ).toBe(0);
  });

  /**
   * The control for the other two: the data is reachable whatever the layout does.
   *
   * While F-H-01 stood this was the evidence that the finding was "the screen
   * breaks" and not "the feature breaks". It keeps that job now that the strip
   * scrolls in its own box: a container with `overflow-x: auto` can hide content
   * from a reader who never scrolls it, so the roles have to be provably present.
   */
  test('TC-H-002 — every role created is still present and editable on the matrix', async ({ adminPage, runId }) => {
    await adminPage.setViewportSize({ width: 1440, height: 900 });
    const names = await createRoles(adminPage, runId, 8);

    await adminPage.goto('/roles');
    for (const name of names) {
      expect(
        await adminPage.getByText(name, { exact: false }).count(),
        `role "${name}" should appear on the matrix — containing the strip's overflow must not cost a role its row`
      ).toBeGreaterThan(0);
    }
  });

  /**
   * TC-H-003 — the same regression guard on a phone, where the fault was worst.
   *
   * WHAT THE AUDIT GOT WRONG HERE, recorded because it is the reason this test
   * exists in two halves. The original note guessed that the matrix already sat
   * inside a horizontally scrollable wrapper — so the table would scroll while the
   * page did not, the pattern the design system uses everywhere else — and that
   * only the desktop case was broken. It was not: at 390px the measurement was
   * 3024px of PAGE overflow, so both widths overflowed and neither was containing
   * anything. TC-H-001 and this test are therefore the same finding at two widths,
   * not a broken case and a working one.
   *
   * Both pass now. `.segmented` contains its own overflow
   * (web/static/fervid-ds.css:524-527) and its items keep their natural size via
   * `flex-shrink: 0`, so the strip scrolls in its own box at either width instead
   * of squashing into illegible tabs or widening the page.
   *
   * Roles accumulate across the tests in this file (one worker, one server), so
   * the count here is the seeded four plus everything the preceding tests
   * created — which is what makes this the harder of the two cases.
   */
  test('TC-H-003 — at 390px the page does not scroll sideways once several roles exist', async ({
    adminPage,
    runId
  }) => {
    await createRoles(adminPage, runId, 8);
    await adminPage.setViewportSize({ width: 390, height: 844 });
    await adminPage.goto('/roles');
    await expect(adminPage.locator('h1')).toBeVisible();

    const overflow = await sidewaysOverflow(adminPage);
    expect(
      overflow,
      `/roles at 390px must not scroll the page sideways (ux.spec.ts forbids it), but it overflows by ${overflow}px — F-H-01`
    ).toBe(0);
  });
});
