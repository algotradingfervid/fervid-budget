import { test, expect } from '@playwright/test';
import { login, loadMerged } from './helpers';
import { acceptPartial, raiseConcern, statusOf } from './actions';
import { requests, users, PASSWORD } from './plan';

// Phase 9: the manager closes out a part-paid request, or pushes back on it.
// Raising a concern deliberately does NOT change the status — it leaves the
// request in partial_review with the objection recorded on the thread.

type Created = { id: number; number: string };

const ONLY = (process.env.FERVID_ONLY ?? '').split(',').filter(Boolean);
const wanted = (s: string): boolean => ONLY.length === 0 || ONLY.includes(s);

test.describe.configure({ mode: 'parallel' });

const byApprover = new Map<string, typeof requests>();
for (const r of requests) {
  if (r.scenario !== 'PARTIAL_ACCEPTED' && r.scenario !== 'PARTIAL_CONCERN') continue;
  const list = byApprover.get(r.approverKey) ?? [];
  list.push(r);
  byApprover.set(r.approverKey, list);
}

for (const [approverKey, own] of byApprover) {
  const approver = users.find((u) => u.key === approverKey)!;

  test(`partial decisions as ${approverKey}`, async ({ page }) => {
    const created = loadMerged<Created>('requests-');
    expect(Object.keys(created).length, 'no request artifacts').toBeGreaterThan(0);
    await login(page, approver.email, PASSWORD);

    for (const r of own) {
      const row = created[r.key];
      if (!row || !wanted(r.scenario)) continue;
      if ((await statusOf(page, row.id)) !== 'partial_review') continue;

      if (r.scenario === 'PARTIAL_ACCEPTED') {
        await acceptPartial(page, row.id, 'Shortfall accepted — closing this against the invoice.');
      } else {
        await raiseConcern(page, row.id, 'Why was the balance not paid? Please confirm with the vendor.');
      }
    }
  });
}
