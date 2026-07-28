import { test } from '@playwright/test';
import { login, shardOf } from './helpers';
import { users, PASSWORD } from './plan';

// Phase 2a: create the user rows. The create sheet is an overlay that starts
// hidden and carries no role fields — roles and default approvers are a second
// pass (03-user-roles.spec.ts) because the edit form is where they live.

const SHARDS = 5;
const ADMIN = { email: 'admin@fervid.local', password: 'admin123' };

test.describe.configure({ mode: 'parallel' });

for (let shard = 0; shard < SHARDS; shard++) {
  test(`create users shard ${shard}`, async ({ page }) => {
    await login(page, ADMIN.email, ADMIN.password);

    for (const u of shardOf(users, shard, SHARDS)) {
      await page.goto('/users');
      const opener = page.locator('[data-open="user-new"]').first();
      if (await opener.count()) await opener.click();

      const form = page.locator('form.setup-form[action="/users"]').first();
      await form.locator('input[name="email"]').fill(u.email);
      await form.locator('input[name="name"]').fill(u.name);
      await form.locator('select[name="role"]').selectOption(u.legacyRole);
      await form.locator('input[name="password"]').fill(PASSWORD);

      const active = form.locator('input[name="active"]');
      if (await active.count()) {
        if (u.active) await active.check();
        else await active.uncheck();
      }

      await form.locator('button[type="submit"], button.primary').first().click();
      await page.waitForLoadState('domcontentloaded');

      const bad = page.locator('.banner.bad, .error').first();
      if (await bad.count()) {
        const text = (await bad.innerText().catch(() => '')).trim();
        // A rerun re-entering an existing email is not a failure.
        if (text && !/already|exists|duplicate|taken/i.test(text)) {
          throw new Error(`user ${u.email}: ${text.slice(0, 200)}`);
        }
      }
    }
  });
}
