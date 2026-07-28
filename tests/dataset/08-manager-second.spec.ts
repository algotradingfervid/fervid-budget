import { test, expect } from '@playwright/test';
import { login, loadMerged } from './helpers';
import { approve, decideCancellation, cancelWithReason } from './actions';
import { requests, users, PASSWORD } from './plan';

// Phase 7: the manager's second round — approve what was reassigned to them,
// decide the cancellations that were asked for, and cancel outright the ones
// that never should have gone ahead.
//
// Sharded by the manager who actually acts, which for a reassigned request is
// the new approver, not the original one.

type Created = { id: number; number: string };

const ONLY = (process.env.FERVID_ONLY ?? '').split(',').filter(Boolean);
const wanted = (s: string): boolean => ONLY.length === 0 || ONLY.includes(s);

test.describe.configure({ mode: 'parallel' });

interface Job {
  scenario: string;
  key: string;
  approvedAmount: number;
}

const byActor = new Map<string, Job[]>();
const addJob = (actorKey: string, job: Job): void => {
  const list = byActor.get(actorKey) ?? [];
  list.push(job);
  byActor.set(actorKey, list);
};

for (const r of requests) {
  const job: Job = { scenario: r.scenario, key: r.key, approvedAmount: r.approvedAmount };
  if (r.scenario === 'REASSIGNED_APPROVED') addJob(r.reassignToKey, job);
  else if (['CANCELLATION_ACCEPTED', 'CANCELLATION_DECLINED', 'MANAGER_CANCELLED'].includes(r.scenario)) {
    addJob(r.approverKey, job);
  }
}

for (const [actorKey, jobs] of byActor) {
  const actor = users.find((u) => u.key === actorKey)!;

  test(`second decisions as ${actorKey}`, async ({ page }) => {
    const created = loadMerged<Created>('requests-');
    expect(Object.keys(created).length, 'no request artifacts').toBeGreaterThan(0);
    await login(page, actor.email, PASSWORD);

    for (const job of jobs) {
      const row = created[job.key];
      if (!row || !wanted(job.scenario)) continue;

      switch (job.scenario) {
        case 'REASSIGNED_APPROVED':
          await approve(page, row.id, job.approvedAmount, 'Approved after reassignment.');
          break;
        case 'CANCELLATION_ACCEPTED':
          await decideCancellation(page, row.id, true, 'Agreed — cancelling as requested.');
          break;
        case 'CANCELLATION_DECLINED':
          await decideCancellation(page, row.id, false, 'The commitment stands; keeping this live.');
          break;
        case 'MANAGER_CANCELLED':
          await cancelWithReason(page, row.id, 'Cancelled centrally — the scope was dropped.');
          break;
        default:
          break;
      }
    }
  });
}
