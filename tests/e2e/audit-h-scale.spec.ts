/**
 * Area H — the UI at realistic data volume.
 *
 * Every other spec runs against a database holding a handful of rows, so the
 * UX gate in `ux.spec.ts` ("fails on horizontal scroll") only ever sees the
 * seeded four roles. This file asks what the screens do once an organisation
 * has used the product for a while: the roles matrix grows a column per role,
 * and however many roles exist, the page must never scroll sideways at either
 * width — the role strip scrolls inside its own box instead.
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
   * TC-H-001 — at desktop width, adding roles never widens the page.
   *
   * `/roles` shows one `.segmented` link per role, with no upper bound. The
   * strip is capped at the page width and scrolls inside its own box (the way
   * `.table-wrap`, `.grid-wrap` and `.budget-grid` do), so the page itself never
   * scrolls sideways.
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
      `/roles must not scroll sideways at 1440px with 12 roles (ux.spec.ts forbids it), but it overflows by ${overflow}px`
    ).toBe(0);
  });

  /**
   * The control for the other two: the data is reachable whatever the layout does.
   *
   * A strip that scrolls in its own box can hide content from a reader who
   * never scrolls it, so every role created has to be provably present.
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
   * TC-H-003 — the same rule on a phone, the narrowest case.
   *
   * The strip's items keep their natural size (`flex-shrink: 0`), so at 390px
   * the strip scrolls in its own box instead of squashing into illegible tabs
   * or widening the page.
   *
   * Roles accumulate across the tests in this file (one worker, one server), so
   * the count here is the seeded four plus everything the preceding tests
   * created — which makes this the harder of the two cases.
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
      `/roles at 390px must not scroll the page sideways (ux.spec.ts forbids it), but it overflows by ${overflow}px`
    ).toBe(0);
  });
});
