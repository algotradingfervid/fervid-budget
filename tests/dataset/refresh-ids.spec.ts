import { test, expect } from '@playwright/test';
import { login, projectIdMap, headIdMap, userIdMap, saveArtifact } from './helpers';
import { MONTHS, projects, users } from './plan';

// Re-snapshot the app's own ids. Run this after any setup phase — the phase-1
// snapshot raced head creation and came back partial, which is exactly the sort
// of thing that shows up 500 rows later as an unhelpful validation message.

test('refresh id artifacts', async ({ page }) => {
  await login(page, 'admin@fervid.local', 'admin123');

  const p = await projectIdMap(page);
  const h = await headIdMap(page, MONTHS[4]!);
  const u = await userIdMap(page);

  const wantHeads = projects.reduce((n, x) => n + x.heads.length, 0);
  expect(Object.keys(p).length, 'projects').toBeGreaterThanOrEqual(projects.length);
  expect(Object.keys(h).length, 'heads').toBeGreaterThanOrEqual(wantHeads);
  expect(Object.keys(u).length, 'users').toBeGreaterThanOrEqual(users.length);

  saveArtifact('projects.json', p);
  saveArtifact('heads.json', h);
  saveArtifact('users.json', u);
  console.log(`projects=${Object.keys(p).length} heads=${Object.keys(h).length} users=${Object.keys(u).length}`);
});
