import { test, expect } from '@playwright/test';
import { login, headIdMap, projectIdMap, userIdMap, vendorIdMap } from './helpers';
import { summary } from './plan';

// Proves the helpers can read the app's own ids before we write anything at
// scale. If this fails, the entry phases would fail 500 rows deep instead.
test('admin can log in and every id map is readable', async ({ page }) => {
  await login(page, 'admin@fervid.local', 'admin123');

  const users = await userIdMap(page);
  expect(Object.keys(users).length, 'at least the seeded admin').toBeGreaterThan(0);
  expect(users['admin@fervid.local']).toBeTruthy();

  const projects = await projectIdMap(page);
  expect(Object.keys(projects).length, 'seeded projects visible').toBeGreaterThan(0);

  const heads = await headIdMap(page, '2026-07');
  expect(Object.keys(heads).length, 'budget grid exposes head ids').toBeGreaterThan(0);

  const vendors = await vendorIdMap(page);

  console.log('--- baseline before entry ---');
  console.log('users:', Object.keys(users).length);
  console.log('projects:', Object.keys(projects).length, Object.keys(projects).join(', '));
  console.log('heads:', Object.keys(heads).length);
  console.log('vendors:', Object.keys(vendors).length);
  console.log('--- plan to be entered ---');
  console.log(JSON.stringify(summary, null, 2));
});
