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
 * matrix grows a column per role. That is a product defect the shipped gate
 * cannot see, and it was found only by accident.
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
   * TC-H-001 — F-H-01, the roles matrix outgrows the viewport.
   *
   * ANNOTATED test.fail(): the assertion is the one the project's own UX gate
   * makes, and the screen does not satisfy it. When the matrix is made to fit,
   * this test reports "passed unexpectedly" and the annotation should be removed.
   *
   * The annotation goes INSIDE the test body. As a bare `test.fail()` statement
   * in the describe block it would apply to every test in the block, and the two
   * that legitimately pass would be reported as failures.
   */
  test('TC-H-001 — the roles matrix still fits its viewport once an organisation has a dozen roles', async ({
    adminPage,
    runId
  }) => {
    test.fail();
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
   * The other half of F-H-01: the data is still reachable, so this is a layout
   * failure and not a loss of function. Kept separate and unannotated, because
   * it passes and is the evidence that the finding is "the screen breaks" rather
   * than "the feature breaks".
   */
  test('TC-H-002 — every role created is still present and editable on the matrix', async ({ adminPage, runId }) => {
    await adminPage.setViewportSize({ width: 1440, height: 900 });
    const names = await createRoles(adminPage, runId, 8);

    await adminPage.goto('/roles');
    for (const name of names) {
      expect(
        await adminPage.getByText(name, { exact: false }).count(),
        `role "${name}" should appear on the matrix — the overflow in TC-H-001 is a layout fault, not a data one`
      ).toBeGreaterThan(0);
    }
  });

  /**
   * TC-H-003 — the same question on a phone, and the answer is worse.
   *
   * ANNOTATED test.fail(): measured at 3024px of page overflow. The guess that
   * the matrix sits inside a horizontally scrollable wrapper — which would let
   * the table scroll while the page did not, the pattern the design system uses
   * for every other wide table — is wrong for this screen. The page itself
   * scrolls, which is what `ux.spec.ts` forbids outright, and the overflow grows
   * with the number of roles.
   *
   * Roles accumulate across the tests in this file (one worker, one server), so
   * the count here is the seeded four plus everything the preceding tests
   * created. The exact figure is not the point and is not asserted; that it is
   * greater than zero at all is.
   */
  test('TC-H-003 — at 390px the page does not scroll sideways once several roles exist', async ({
    adminPage,
    runId
  }) => {
    test.fail();
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
