// Who the manual is photographed as, and which real records it photographs.
//
// One place, because the shot list, the capture run and the written manual all
// have to agree. These are rows in data/fervid.db as it stands: the users were
// picked for having the richest history in each seat, and each request id was
// picked because it is genuinely in the state the shot needs to show.

export type RoleKey = 'requester' | 'manager' | 'accounts' | 'admin';

export interface Actor {
  role: RoleKey;
  label: string;
  email: string;
  password: string;
}

const GENERATED_PASSWORD = 'Fervid#2026pass';

export const ACTORS: Record<RoleKey, Actor> = {
  // 26 requests spread across 8 statuses.
  requester: { role: 'requester', label: 'Requester', email: 'rahul.gupta.req11@fervid.test', password: GENERATED_PASSWORD },
  // 65 requests, and it is the approver of record for every status including
  // cancellation_requested and partial_review, which the action sheets need.
  manager: { role: 'manager', label: 'Manager', email: 'karthik.bose.mgr1@fervid.test', password: GENERATED_PASSWORD },
  // 22 payments recorded, and holds live reservations.
  accounts: { role: 'accounts', label: 'Accounts', email: 'harsh.agarwal.acc4@fervid.test', password: GENERATED_PASSWORD },
  admin: { role: 'admin', label: 'Administrator', email: 'admin@fervid.local', password: 'admin123' },
};

/**
 * Example records. Each id is a request the named actor can act on — the
 * pairing matters, because the action bar only renders for the requester of
 * record or the approver of record.
 */
export const EXAMPLES = {
  // Seen by the manager (approver of record for all of these).
  managerPending: 2,
  managerReturned: 27,
  managerRejected: 45,
  managerApproved: 71,
  managerCancellationRequested: 96,
  managerPartialReview: 151,
  managerProcessing: 134,
  managerCompleted: 106,
  managerCompletedPartial: 392,
  managerCancelled: 352,
  managerWithdrawn: 67,

  // Seen by the requester (raised all of these).
  requesterPending: 426,
  requesterReturned: 432,
  requesterRejected: 442,
  requesterApproved: 467,
  requesterWithdrawn: 461,
  requesterCancellationRequested: 526,
  requesterProcessing: 533,
  requesterCompleted: 537,

  // Seen by Accounts.
  // Genuinely mid-flight: status is processing, this actor holds the
  // reservation, and no payment exists yet. Picking one that merely had
  // processing_by set was not enough — request 74 was already paid, so the
  // payment form correctly answered 409.
  accountsHeldByMe: 85,
  accountsTakeable: 24, // approved, not held, nobody processing
  accountsOnHold: 59,
  accountsPartialReview: 82,
  // Processing, but reserved by a different accountant (user 42), so opening the
  // payment form for it is refused with the reservation-conflict screen.
  heldBySomebodyElse: 61,
  // Already settled: the payment form refuses it so the same request cannot be
  // paid twice.
  alreadyPaid: 58,

  // Cross-cutting examples.
  urgent: 12,
  recoverable: 4,
  partiallyApproved: 70,

  linkedPayment: 124, // settled, belongs to request 58 — immutable
  legacyPayment: 1, // no request_id — the only kind that can still be edited or voided

  vendor: 1,
  project: 9,

  // A non-admin account, used to photograph the edit panel on the people screen.
  editableUser: 14,

  // Budgets exist for 2026-03 through 2026-08, but payments only start in
  // 2026-05 — so this month renders the ledger's genuinely empty state without
  // anything having to be deleted to produce it.
  emptyPaymentMonth: '2026-04',

  // Locked-month states are captured by locking this month during the run and
  // unlocking it afterwards. 2026-03 carries budgets but no payments, so the
  // lock is inert while it is held.
  lockMonth: '2026-03',
  openMonth: '2026-07',
} as const;
