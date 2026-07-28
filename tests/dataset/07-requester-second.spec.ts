import { test, expect } from '@playwright/test';
import { login, loadMerged, saveArtifact } from './helpers';
import { withdraw, resubmit, reraise, askCancellation } from './actions';
import { requests, users, PASSWORD } from './plan';

// Phase 6: what the requester does after the first decision — withdraw, fix and
// resubmit a returned request, raise a rejected one again, or ask for an
// approved one to be cancelled.

type Created = { id: number; number: string };

// Set FERVID_ONLY to a comma-separated scenario list to replay one step without
// re-running the others — the rest of this phase is not idempotent (re-raising
// twice would create a second copy).
const ONLY = (process.env.FERVID_ONLY ?? '').split(',').filter(Boolean);
const wanted = (scenario: string): boolean => ONLY.length === 0 || ONLY.includes(scenario);

test.describe.configure({ mode: 'parallel' });

const byRequester = new Map<string, typeof requests>();
for (const r of requests) {
  const list = byRequester.get(r.requesterKey) ?? [];
  list.push(r);
  byRequester.set(r.requesterKey, list);
}

for (const [requesterKey, own] of byRequester) {
  const requester = users.find((u) => u.key === requesterKey)!;

  test(`requester follow-ups as ${requesterKey}`, async ({ page }) => {
    const created = loadMerged<Created>('requests-');
    expect(Object.keys(created).length, 'no request artifacts').toBeGreaterThan(0);
    await login(page, requester.email, PASSWORD);

    const reraised: Record<string, { id: number; number: string }> = {};

    for (const r of own) {
      const row = created[r.key];
      if (!row || !wanted(r.scenario)) continue;

      switch (r.scenario) {
        case 'WITHDRAWN':
          await withdraw(page, row.id);
          break;
        case 'RETURNED_RESUBMITTED':
          await resubmit(page, row.id, 'invoice copy attached and the amount corrected');
          break;
        case 'REJECTED_RERAISED': {
          const newId = await reraise(page, row.id);
          if (newId) reraised[`${r.key}-reraised`] = { id: newId, number: '' };
          break;
        }
        case 'CANCELLATION_REQUESTED':
        case 'CANCELLATION_ACCEPTED':
        case 'CANCELLATION_DECLINED':
          await askCancellation(page, row.id, 'The vendor withdrew the invoice — please cancel this.');
          break;
        default:
          break;
      }
    }

    if (Object.keys(reraised).length) saveArtifact(`reraised-${requesterKey}.json`, reraised);
  });
}
