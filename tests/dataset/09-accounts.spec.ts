import { test, expect } from '@playwright/test';
import { login, loadMerged } from './helpers';
import { hold, unhold, reserve, releaseReservation, recordPayment, statusOf } from './actions';
import { requests, users, PASSWORD } from './plan';

// Phase 8: everything Accounts does — put on hold, take for processing, release
// back to the queue, and record the payment (fully settled or partial).
//
// Sharded by the accounts user assigned to each request. Reservation is
// exclusive in the app, so two workers must never chase the same row.

type Created = { id: number; number: string };

const ONLY = (process.env.FERVID_ONLY ?? '').split(',').filter(Boolean);
const wanted = (s: string): boolean => ONLY.length === 0 || ONLY.includes(s);

test.describe.configure({ mode: 'parallel' });

const byAccounts = new Map<string, typeof requests>();
for (const r of requests) {
  const list = byAccounts.get(r.accountsKey) ?? [];
  list.push(r);
  byAccounts.set(r.accountsKey, list);
}

for (const [accountsKey, own] of byAccounts) {
  const actor = users.find((u) => u.key === accountsKey)!;

  test(`accounts work as ${accountsKey}`, async ({ page }) => {
    const created = loadMerged<Created>('requests-');
    expect(Object.keys(created).length, 'no request artifacts').toBeGreaterThan(0);
    await login(page, actor.email, PASSWORD);

    for (const r of own) {
      const row = created[r.key];
      if (!row || !wanted(r.scenario)) continue;

      // Resume-safe: a run interrupted by the payment concurrency defect leaves
      // requests half-advanced, so read where this one actually is before acting
      // rather than assuming it is still `approved`.
      const now = await statusOf(page, row.id);
      if (now === 'completed' || now === 'completed_partial' || now === 'partial_review') continue;

      const takeIfNeeded = async (): Promise<void> => {
        if ((await statusOf(page, row.id)) !== 'processing') await reserve(page, row.id, row.number);
      };

      const pay = async (settlement: 'settled' | 'partial'): Promise<void> => {
        await recordPayment(page, row.id, {
          amount: settlement === 'settled' ? r.approvedAmount : r.partialPayAmount,
          paidOn: r.paidOn,
          mode: 'bank_transfer',
          reference: `UTR${String(row.id).padStart(8, '0')}`,
          settlement,
          partialReason: 'Vendor agreed to a part payment against this invoice.',
        });
      };

      switch (r.scenario) {
        case 'ON_HOLD':
          if (now !== 'on_hold') await hold(page, row.id, 'Bank details do not match the vendor master.');
          break;
        case 'HOLD_RELEASED_PAID':
          if (now !== 'processing') {
            if (now !== 'on_hold') await hold(page, row.id, 'Awaiting confirmation of the account number.');
            await unhold(page, row.id);
          }
          await takeIfNeeded();
          await pay('settled');
          break;
        case 'PROCESSING':
          await takeIfNeeded();
          break;
        case 'RESERVED_RELEASED':
          await takeIfNeeded();
          await releaseReservation(page, row.id, 'Passing this back — the invoice needs re-checking.');
          break;
        case 'PAID_COMPLETED':
        case 'OVER_BUDGET_PAID':
          await takeIfNeeded();
          await pay('settled');
          break;
        case 'PAID_PARTIAL_REVIEW':
        case 'PARTIAL_ACCEPTED':
        case 'PARTIAL_CONCERN':
          await takeIfNeeded();
          await pay('partial');
          break;
        default:
          break;
      }
    }
  });
}
