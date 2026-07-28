import { test, expect } from '@playwright/test';
import { login, loadArtifact, saveArtifact, vendorIdMap } from './helpers';
import { createRequest, type Ids, type ResolvedRequest } from './actions';
import { requests, users, projects, PASSWORD } from './plan';

// Phase 4: every request is raised by its own requester, through the real form.
// Sharded by requester — one worker per person, so no two browsers ever submit
// as the same identity.

const LIMIT = Number(process.env.FERVID_DATASET_LIMIT ?? 0);

test.describe.configure({ mode: 'parallel' });

const labelFor = new Map<string, string>();
for (const p of projects) for (const h of p.heads) labelFor.set(h.key, `${p.name} / ${h.name}`);

const byRequester = new Map<string, typeof requests>();
for (const r of requests) {
  const list = byRequester.get(r.requesterKey) ?? [];
  list.push(r);
  byRequester.set(r.requesterKey, list);
}

for (const [requesterKey, own] of byRequester) {
  const requester = users.find((u) => u.key === requesterKey)!;

  test(`raise requests as ${requesterKey}`, async ({ page }) => {
    await login(page, requester.email, PASSWORD);

    const ids: Ids = {
      projects: loadArtifact<Record<string, number>>('projects.json', {}),
      heads: loadArtifact<Record<string, number>>('heads.json', {}),
      users: loadArtifact<Record<string, number>>('users.json', {}),
      // Resolved here rather than from an artifact: the vendor select renders
      // inside <noscript> for an admin, so an admin-side scrape comes back empty.
      vendors: await vendorIdMap(page),
    };
    expect(Object.keys(ids.heads).length, 'head ids artifact missing').toBeGreaterThan(0);
    expect(Object.keys(ids.vendors).length, 'no vendors visible to requester').toBeGreaterThan(0);

    const created: Record<string, { id: number; number: string }> = {};
    const slice = LIMIT ? own.slice(0, LIMIT) : own;

    for (const r of slice) {
      const approver = users.find((u) => u.key === r.approverKey)!;
      const resolved: ResolvedRequest = {
        ...r,
        headLabel: labelFor.get(r.headKey) ?? '',
        approverEmail: approver.email.toLowerCase(),
      };
      const out = await createRequest(page, resolved, ids);
      created[r.key] = out;
    }

    saveArtifact(`requests-${requesterKey}.json`, created);
  });
}
