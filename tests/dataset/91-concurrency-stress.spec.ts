import { test, expect } from '@playwright/test';
import { login, loadArtifact, vendorIdMap } from './helpers';
import { approve, createRequest, recordPayment, reserve, type Ids, type ResolvedRequest } from './actions';
import { requests, users, projects, requesters, managers, accountsUsers, PASSWORD } from './plan';

// The load that used to take the application down.
//
// Several accounts users settling payments at the same moment produced HTTP 500s
// ("cannot start a transaction within a transaction", "database is locked") and
// then wedged the write lock permanently — after which nobody could even log in.
//
// Each worker raises its OWN request and carries it all the way to settlement, so
// this proves the fix without consuming any request from the curated dataset.
// The payment writes overlap across workers, which is exactly the collision that
// used to break it.

const SUBJECTS = 8;

test.describe.configure({ mode: 'parallel' });

const template = requests[0]!;
const labelFor = new Map<string, string>();
for (const p of projects) for (const h of p.heads) labelFor.set(h.key, `${p.name} / ${h.name}`);

for (let i = 0; i < SUBJECTS; i++) {
  const requester = requesters[i % requesters.length]!;
  const approver = managers[i % managers.length]!;
  const accountant = accountsUsers[i % accountsUsers.length]!;

  test(`concurrent settlement ${i}`, async ({ page }) => {
    const ids: Ids = {
      projects: loadArtifact<Record<string, number>>('projects.json', {}),
      heads: loadArtifact<Record<string, number>>('heads.json', {}),
      users: loadArtifact<Record<string, number>>('users.json', {}),
      vendors: {},
    };

    await login(page, requester.email, PASSWORD);
    ids.vendors = await vendorIdMap(page);

    const subject: ResolvedRequest = {
      ...template,
      key: `stress${i}`,
      scenario: 'PAID_COMPLETED',
      type: 'reimbursement',
      treatment: 'budget',
      amount: 1000,
      approvedAmount: 1000,
      shortTitle: `[stress${i}] Concurrency proof`,
      purpose: 'Raised by the concurrency stress test to prove parallel settlement is safe.',
      expenseDate: '2026-07-15',
      paidOn: '2026-07-21',
      headLabel: labelFor.get(template.headKey) ?? '',
      approverEmail: approver.email.toLowerCase(),
    };

    const created = await createRequest(page, subject, ids);

    await login(page, approver.email, PASSWORD);
    await approve(page, created.id, subject.approvedAmount, 'Approved for the stress run.');

    await login(page, accountant.email, PASSWORD);
    await reserve(page, created.id, created.number);
    await recordPayment(page, created.id, {
      amount: subject.approvedAmount,
      paidOn: subject.paidOn,
      mode: 'bank_transfer',
      reference: `UTR-STRESS-${i}`,
      settlement: 'settled',
    });
  });
}

// The wedge was the worse half: after it, ordinary writes failed for everyone.
// Logging in performs a write, so a fast successful login is the honest check.
test('the application still accepts writes after concurrent settlement', async ({ page }) => {
  const started = Date.now();
  await login(page, users.find((u) => u.role === 'Admin')!.email, PASSWORD);
  const elapsed = Date.now() - started;
  await expect(page).not.toHaveURL(/\/login/);
  expect(elapsed, 'login took the shape of a busy-timeout stall').toBeLessThan(5_000);
});
