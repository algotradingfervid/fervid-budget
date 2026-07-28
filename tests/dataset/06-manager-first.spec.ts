import { test, expect } from '@playwright/test';
import { login, loadArtifact, loadMerged } from './helpers';
import { approve, reject, returnForCorrection, reassignApprover } from './actions';
import { requests, users, PASSWORD, type Scenario } from './plan';

// Phase 5: the approver's first decision on each request they were sent.
// Sharded by approver, so a request is only ever touched by the one manager it
// belongs to.

type Created = { id: number; number: string };

const APPROVE: Scenario[] = [
  'APPROVED', 'APPROVED_PARTIAL_AMOUNT', 'CANCELLATION_REQUESTED', 'CANCELLATION_ACCEPTED',
  'CANCELLATION_DECLINED', 'MANAGER_CANCELLED', 'ON_HOLD', 'HOLD_RELEASED_PAID', 'PROCESSING',
  'RESERVED_RELEASED', 'PAID_COMPLETED', 'PAID_PARTIAL_REVIEW', 'PARTIAL_ACCEPTED',
  'PARTIAL_CONCERN', 'OVER_BUDGET_PAID',
];
const RETURN: Scenario[] = ['RETURNED', 'RETURNED_RESUBMITTED'];
const REJECT: Scenario[] = ['REJECTED', 'REJECTED_RERAISED'];
const REASSIGN: Scenario[] = ['REASSIGNED_APPROVED'];

test.describe.configure({ mode: 'parallel' });

const byApprover = new Map<string, typeof requests>();
for (const r of requests) {
  const list = byApprover.get(r.approverKey) ?? [];
  list.push(r);
  byApprover.set(r.approverKey, list);
}

for (const [approverKey, own] of byApprover) {
  const approver = users.find((u) => u.key === approverKey)!;

  test(`first decisions as ${approverKey}`, async ({ page }) => {
    const created = loadMerged<Created>('requests-');
    const userIds = loadArtifact<Record<string, number>>('users.json', {});
    expect(Object.keys(created).length, 'no request artifacts').toBeGreaterThan(0);

    await login(page, approver.email, PASSWORD);

    for (const r of own) {
      const row = created[r.key];
      if (!row) continue;

      if (APPROVE.includes(r.scenario)) {
        await approve(page, row.id, r.approvedAmount, `Approved for ${r.scenario}.`);
      } else if (RETURN.includes(r.scenario)) {
        await returnForCorrection(page, row.id, 'Please attach the supporting invoice and resubmit.');
      } else if (REJECT.includes(r.scenario)) {
        await reject(page, row.id, 'Outside the approved budget for this head.');
      } else if (REASSIGN.includes(r.scenario)) {
        const target = users.find((u) => u.key === r.reassignToKey)!;
        const targetId = userIds[target.email.toLowerCase()];
        if (targetId) {
          await reassignApprover(page, row.id, targetId, 'Reassigning — this head sits with another approver.');
        }
      }
    }
  });
}
