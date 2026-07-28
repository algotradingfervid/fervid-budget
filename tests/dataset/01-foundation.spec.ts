import { test, expect } from '@playwright/test';
import { login, shardOf, saveArtifact, projectIdMap, headIdMap } from './helpers';
import { projects, VENDORS, MONTHS } from './plan';

// Phase 1: the things everything else hangs off — vendors, projects, heads.
// Sharded by project so no two workers ever touch the same rows.

const SHARDS = 4;
const ADMIN = { email: 'admin@fervid.local', password: 'admin123' };

test.describe.configure({ mode: 'parallel' });

test('vendors', async ({ page }) => {
  await login(page, ADMIN.email, ADMIN.password);
  for (const name of VENDORS) {
    await page.goto('/vendors/new');
    await page.fill('input[name="name"]', name);
    const type = page.locator('select[name="vendor_type"]');
    if (await type.count()) await type.selectOption('company');
    await page.fill('input[name="city"]', 'Chennai');
    await page.locator('form[action="/vendors"] button[type="submit"], form[action="/vendors"] button.primary').first().click();
    await page.waitForLoadState('domcontentloaded');
    // A duplicate name is fine on a rerun; anything else is not.
    const bad = page.locator('.banner.bad, .error').first();
    if (await bad.count()) {
      const text = (await bad.innerText().catch(() => '')).trim();
      if (text && !/already|exists|duplicate/i.test(text)) throw new Error(`vendor ${name}: ${text}`);
    }
  }
});

for (let shard = 0; shard < SHARDS; shard++) {
  test(`projects and heads shard ${shard}`, async ({ page }) => {
    await login(page, ADMIN.email, ADMIN.password);
    const mine = shardOf(projects, shard, SHARDS);

    for (const project of mine) {
      await page.goto('/projects');
      const form = page.locator('form.setup-form[action="/projects"]').first();
      await form.locator('input[name="name"]').fill(project.name);
      await form.locator('button[type="submit"], button.primary').first().click();
      await page.waitForLoadState('domcontentloaded');
    }

    // Heads need their project to exist, so resolve ids after the projects land.
    const projectIds = await projectIdMap(page);
    for (const project of mine) {
      const pid = projectIds[project.name];
      expect(pid, `project ${project.name} was not created`).toBeTruthy();
      for (const head of project.heads) {
        await page.goto('/heads');
        const form = page.locator('form.setup-form[action="/heads"]').first();
        await form.locator('select[name="project_id"]').selectOption(String(pid));
        await form.locator('input[name="name"]').fill(head.name);
        await form.locator('input[name="due_day"]').fill(head.dueDay);
        await form.locator('button[type="submit"], button.primary').first().click();
        await page.waitForLoadState('domcontentloaded');
      }
    }
  });
}

test('record resolved project and head ids', async ({ page }) => {
  // Runs last by virtue of depending on nothing — re-read at the start of every
  // later phase anyway, so this artifact is a convenience, not a contract.
  await login(page, ADMIN.email, ADMIN.password);
  await expect
    .poll(async () => Object.keys(await projectIdMap(page)).length, { timeout: 120_000, intervals: [5_000] })
    .toBeGreaterThanOrEqual(projects.length);
  saveArtifact('projects.json', await projectIdMap(page));
  saveArtifact('heads.json', await headIdMap(page, MONTHS[4]!));
});
