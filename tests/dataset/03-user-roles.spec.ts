import { test, expect } from '@playwright/test';
import { login, shardOf, userIdMap, saveArtifact } from './helpers';
import { users, ROLE_IDS } from './plan';

// Phase 2b: give every user its real role and, for requesters, a default
// approver.
//
// This pass is not optional. store.CreateUser silently grants the Accounts role
// to every new user, so without it all 55 would be Accounts and every
// permission assertion later would be meaningless. The edit form REPLACES the
// whole role set, so we uncheck what is there before checking what we want.

const SHARDS = 5;
const ADMIN = { email: 'admin@fervid.local', password: 'admin123' };

test.describe.configure({ mode: 'parallel' });

for (let shard = 0; shard < SHARDS; shard++) {
  test(`assign roles shard ${shard}`, async ({ page }) => {
    await login(page, ADMIN.email, ADMIN.password);
    const ids = await userIdMap(page);

    for (const u of shardOf(users, shard, SHARDS)) {
      const id = ids[u.email.toLowerCase()];
      expect(id, `user ${u.email} was not created in phase 2a`).toBeTruthy();

      await page.goto('/users');
      const opener = page.locator(`[data-open="user-${id}"]`).first();
      if (await opener.count()) await opener.click();

      const form = page.locator(`form[action="/users"]`).filter({
        has: page.locator(`input[name="id"][value="${id}"]`),
      }).first();
      await expect(form, `no edit form for ${u.email}`).toHaveCount(1);

      const boxes = form.locator('input[name="role_ids"]');
      const n = await boxes.count();
      const want = String(ROLE_IDS[u.role]);
      for (let i = 0; i < n; i++) {
        const box = boxes.nth(i);
        const value = await box.getAttribute('value');
        if (value === want) await box.check();
        else await box.uncheck();
      }

      if (u.defaultApproverKey) {
        const approver = users.find((x) => x.key === u.defaultApproverKey)!;
        const approverId = ids[approver.email.toLowerCase()];
        const select = form.locator('select[name="default_approver_id"]');
        if (approverId && (await select.count())) {
          await select.selectOption(String(approverId));
        }
      }

      await form.locator('button[type="submit"], button.primary').first().click();
      await page.waitForLoadState('domcontentloaded');

      const bad = page.locator('.banner.bad, .error').first();
      if (await bad.count()) {
        const text = (await bad.innerText().catch(() => '')).trim();
        if (text) throw new Error(`role assignment for ${u.email}: ${text.slice(0, 200)}`);
      }
    }
  });
}

test('record resolved user ids', async ({ page }) => {
  await login(page, ADMIN.email, ADMIN.password);
  await expect
    .poll(async () => Object.keys(await userIdMap(page)).length, { timeout: 180_000, intervals: [5_000] })
    .toBeGreaterThanOrEqual(users.length);
  saveArtifact('users.json', await userIdMap(page));
});
