// Deterministic synthetic dataset for Fervid Budget.
//
// Pure data — no Playwright, no HTTP. The entry specs consume this and map each
// request's `scenario` onto real UI steps. A rerun must produce the identical
// plan so a failed run can be resumed or diffed, hence the seeded PRNG.
//
// Every rule encoded here was verified against internal/store/requests.go and
// internal/store/store.go. In particular:
//   - there is no `draft` status; payment_requests has CHECK (status <> 'draft')
//   - approved_amount must be <= amount; paid amount must be <= approved
//   - paid_on may not be in the future
//   - a request must be reserved before a payment can be recorded
//   - one payment per request, and a linked payment can never be voided

export const TODAY = '2026-07-27';
export const PASSWORD = 'Fervid#2026pass';

// Role ids are stable: seedSystemRoles inserts Requester, Manager, Accounts,
// Admin in that order on migration v1.
export const ROLE_IDS = { Requester: 1, Manager: 2, Accounts: 3, Admin: 4 } as const;
export type RoleName = keyof typeof ROLE_IDS;

function rng(seed: number): () => number {
  let s = seed;
  return () => {
    s |= 0;
    s = (s + 0x6d2b79f5) | 0;
    let t = Math.imul(s ^ (s >>> 15), 1 | s);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const rand = rng(20260727);
const pick = <T>(a: readonly T[]): T => a[Math.floor(rand() * a.length)]!;
const int = (lo: number, hi: number): number => lo + Math.floor(rand() * (hi - lo + 1));
const round100 = (n: number): number => Math.max(100, Math.round(n / 100) * 100);

// ------------------------------------------------------------------ users

const FIRST = ['Aarav', 'Vivaan', 'Aditya', 'Vihaan', 'Arjun', 'Reyansh', 'Krishna', 'Ishaan', 'Rohan', 'Kabir',
  'Ananya', 'Diya', 'Aadhya', 'Saanvi', 'Myra', 'Anika', 'Navya', 'Kiara', 'Ira', 'Riya',
  'Rahul', 'Karthik', 'Nikhil', 'Siddharth', 'Varun', 'Manish', 'Pranav', 'Tarun', 'Gaurav', 'Harsh',
  'Meera', 'Divya', 'Shruti', 'Nandini', 'Priya', 'Sneha', 'Pooja', 'Lakshmi', 'Radha', 'Tanvi',
  'Imran', 'Farhan', 'Zoya', 'Aisha', 'Rizwan', 'Sameer', 'Neha', 'Kavya', 'Rohit', 'Deepak',
  'Yash', 'Ritu', 'Sanjay', 'Alok', 'Bhavna'];
const LAST = ['Sharma', 'Verma', 'Iyer', 'Nair', 'Reddy', 'Patel', 'Menon', 'Rao', 'Gupta', 'Joshi',
  'Kulkarni', 'Desai', 'Chopra', 'Malhotra', 'Bose', 'Banerjee', 'Pillai', 'Shetty', 'Kapoor', 'Sinha',
  'Mehta', 'Agarwal', 'Khanna', 'Bhatt', 'Naidu', 'Ahuja', 'Saxena', 'Trivedi', 'Ghosh', 'Dutta'];

export interface PlanUser {
  key: string;
  name: string;
  email: string;
  role: RoleName;
  legacyRole: 'admin' | 'data_entry';
  active: boolean;
  defaultApproverKey?: string;
}

const ROLE_PLAN: ReadonlyArray<{ role: RoleName; count: number; prefix: string }> = [
  { role: 'Requester', count: 30, prefix: 'req' },
  { role: 'Manager', count: 12, prefix: 'mgr' },
  { role: 'Accounts', count: 10, prefix: 'acc' },
  { role: 'Admin', count: 3, prefix: 'adm' },
];

export const users: PlanUser[] = [];
{
  const used = new Set<string>(['admin@fervid.local']);
  for (const { role, count, prefix } of ROLE_PLAN) {
    for (let i = 1; i <= count; i++) {
      let email = '';
      let name = '';
      do {
        const f = pick(FIRST);
        const l = pick(LAST);
        name = `${f} ${l}`;
        email = `${f}.${l}`.toLowerCase() + `.${prefix}${i}@fervid.test`;
      } while (used.has(email));
      used.add(email);
      users.push({
        key: `${prefix}${i}`,
        name,
        email,
        role,
        legacyRole: role === 'Admin' ? 'admin' : 'data_entry',
        // One deliberately inactive requester, so login-refusal and the
        // "inactive user cannot be an approver" rules have a subject.
        active: !(role === 'Requester' && i === 30),
      });
    }
  }
}

export const requesters = users.filter((u) => u.role === 'Requester' && u.active);
export const managers = users.filter((u) => u.role === 'Manager');
export const accountsUsers = users.filter((u) => u.role === 'Accounts');
export const admins = users.filter((u) => u.role === 'Admin');

// A user may never be their own default approver — the store refuses it.
requesters.forEach((u, i) => {
  u.defaultApproverKey = managers[i % managers.length]!.key;
});

// --------------------------------------------------------- projects & heads

const PROJECT_NAMES = [
  'Chennai Plant Expansion', 'Mumbai Retail Rollout', 'Bengaluru R&D Centre', 'Hyderabad Data Centre',
  'Pune Logistics Hub', 'Delhi Corporate Office', 'Kolkata Warehouse', 'Ahmedabad Solar Farm',
  'Kochi Port Terminal', 'Jaipur Training Academy', 'Nagpur Cold Chain', 'Surat Textile Unit',
  'Indore Packaging Line', 'Lucknow Distribution', 'Coimbatore Foundry', 'Vizag Chemical Works',
  'Bhopal Assembly Shop', 'Chandigarh Sales Region', 'Guwahati Field Ops', 'Mysuru Innovation Lab',
];

const HEAD_POOL = [
  'Payroll', 'Contractor Fees', 'Staff Welfare', 'Office Rent', 'Utilities', 'Office Supplies',
  'Marketing', 'Travel', 'Events', 'Equipment Purchase', 'Equipment Maintenance', 'Raw Materials',
  'Freight & Logistics', 'Professional Fees', 'Insurance', 'Security Services', 'Housekeeping',
  'Software Licences', 'Telecom', 'Training & Development', 'Fuel', 'Statutory Dues',
];

export interface PlanHead {
  key: string;
  name: string;
  dueDay: string;
  baseBudget: number; // rupees
}
export interface PlanProject {
  key: string;
  name: string;
  heads: PlanHead[];
}

export const projects: PlanProject[] = PROJECT_NAMES.map((name, pi) => {
  const pool = [...HEAD_POOL];
  const heads: PlanHead[] = [];
  const headCount = int(10, 12);
  for (let h = 0; h < headCount; h++) {
    const headName = pool.splice(Math.floor(rand() * pool.length), 1)[0]!;
    heads.push({
      key: `p${pi + 1}h${h + 1}`,
      name: headName,
      dueDay: String(int(1, 28)),
      baseBudget: pick([50_000, 75_000, 120_000, 250_000, 400_000, 650_000, 900_000, 1_500_000]),
    });
  }
  return { key: `p${pi + 1}`, name, heads };
});

export const headByKey = new Map<string, { project: PlanProject; head: PlanHead }>();
for (const p of projects) for (const h of p.heads) headByKey.set(h.key, { project: p, head: h });

// ------------------------------------------------------------ months & budgets

// 2026-08 is included so there is a future month with budget but no actuals.
export const MONTHS = ['2026-03', '2026-04', '2026-05', '2026-06', '2026-07', '2026-08'];

export interface PlanBudget {
  month: string;
  headKey: string;
  amount: number; // rupees
}

export const budgets: PlanBudget[] = [];
for (const month of MONTHS) {
  for (const p of projects) {
    for (const h of p.heads) {
      const factor = 0.75 + rand() * 0.5; // month-to-month variance for the grid
      budgets.push({ month, headKey: h.key, amount: round100(h.baseBudget * factor) });
    }
  }
}

// ------------------------------------------------------------------ vendors

export const VENDORS = [
  'Sundaram Electricals', 'Meridian Facility Services', 'BlueOak Logistics', 'Kaveri Constructions',
  'Nexus IT Solutions', 'Trident Office Supplies', 'Anand Travels', 'Precision Tooling Co',
  'GreenLeaf Catering', 'Sterling Security', 'Orbit Telecom', 'Vardhman Freight',
];

// ----------------------------------------------------------------- requests

// Scenarios are full lifecycle paths. Impossible paths are deliberately absent:
//   - there is no draft state to park a request in
//   - a linked payment cannot be voided, so "paid then voided" cannot exist
//   - self-approval cannot be set up through the UI (the approver select
//     excludes you), so it is a negative test, not a dataset row
export type Scenario =
  | 'PENDING'
  | 'URGENT_PENDING'
  | 'RETURNED'
  | 'RETURNED_RESUBMITTED'
  | 'REJECTED'
  | 'REJECTED_RERAISED'
  | 'WITHDRAWN'
  | 'APPROVED'
  | 'APPROVED_PARTIAL_AMOUNT'
  | 'REASSIGNED_APPROVED'
  | 'CANCELLATION_REQUESTED'
  | 'CANCELLATION_ACCEPTED'
  | 'CANCELLATION_DECLINED'
  | 'MANAGER_CANCELLED'
  | 'ON_HOLD'
  | 'HOLD_RELEASED_PAID'
  | 'PROCESSING'
  | 'RESERVED_RELEASED'
  | 'PAID_COMPLETED'
  | 'PAID_PARTIAL_REVIEW'
  | 'PARTIAL_ACCEPTED'
  | 'PARTIAL_CONCERN'
  | 'OVER_BUDGET_PAID';

const SCENARIOS: ReadonlyArray<{ name: Scenario; count: number }> = [
  { name: 'PENDING', count: 55 },
  { name: 'URGENT_PENDING', count: 15 },
  { name: 'RETURNED', count: 35 },
  { name: 'RETURNED_RESUBMITTED', count: 20 },
  { name: 'REJECTED', count: 35 },
  { name: 'REJECTED_RERAISED', count: 15 },
  { name: 'WITHDRAWN', count: 18 },
  { name: 'APPROVED', count: 60 },
  { name: 'APPROVED_PARTIAL_AMOUNT', count: 28 },
  { name: 'REASSIGNED_APPROVED', count: 18 },
  { name: 'CANCELLATION_REQUESTED', count: 14 },
  { name: 'CANCELLATION_ACCEPTED', count: 18 },
  { name: 'CANCELLATION_DECLINED', count: 12 },
  { name: 'MANAGER_CANCELLED', count: 10 },
  { name: 'ON_HOLD', count: 26 },
  { name: 'HOLD_RELEASED_PAID', count: 16 },
  { name: 'PROCESSING', count: 20 },
  { name: 'RESERVED_RELEASED', count: 12 },
  { name: 'PAID_COMPLETED', count: 70 },
  { name: 'PAID_PARTIAL_REVIEW', count: 22 },
  { name: 'PARTIAL_ACCEPTED', count: 18 },
  { name: 'PARTIAL_CONCERN', count: 10 },
  { name: 'OVER_BUDGET_PAID', count: 12 },
];

export type RequestType = 'vendor_invoice' | 'vendor_advance' | 'reimbursement' | 'employee_advance';

const PURPOSES = [
  'Monthly vendor settlement', 'Quarterly maintenance contract', 'Emergency equipment repair',
  'Staff training programme', 'Site safety audit', 'Annual licence renewal', 'Bulk material purchase',
  'Consultant engagement', 'Facility upgrade', 'Transport reimbursement', 'Utility arrears',
  'Marketing campaign spend', 'Statutory filing fees', 'Spare parts procurement',
];

export interface PlanRequest {
  key: string;
  scenario: Scenario;
  type: RequestType;
  treatment: 'budget' | 'recoverable';
  requesterKey: string;
  approverKey: string;
  reassignToKey: string;
  accountsKey: string;
  projectKey: string;
  headKey: string;
  amount: number;          // rupees
  approvedAmount: number;  // <= amount
  partialPayAmount: number;// <= approvedAmount
  vendor: string;
  shortTitle: string;
  purpose: string;
  neededBy: string;
  paidOn: string;
  invoiceNo: string;
  invoiceDate: string;
  expenseDate: string;
  advanceReason: string;
  expectedReturnDate: string;
  repaymentNotes: string;
  urgent: boolean;
  urgencyReason: string;
}

// Dates must never exceed TODAY where the app forbids it (paid_on, invoice_date
// in practice). Months run to 2026-08 but payments stay on or before TODAY.
const PAYABLE_DAYS = ['2026-05-14', '2026-06-03', '2026-06-19', '2026-07-02', '2026-07-11', '2026-07-21', '2026-07-25'];
const PAST_DAYS = ['2026-04-08', '2026-05-06', '2026-06-11', '2026-07-06', '2026-07-15'];

export const requests: PlanRequest[] = [];
{
  let seq = 0;
  for (const sc of SCENARIOS) {
    for (let i = 0; i < sc.count; i++) {
      seq++;
      const project = pick(projects);
      const head = pick(project.heads);
      const requester = pick(requesters);
      const approver = users.find((u) => u.key === requester.defaultApproverKey)!;
      const others = managers.filter((m) => m.key !== approver.key);

      const roll = rand();
      let type: RequestType;
      if (roll < 0.4) type = 'vendor_invoice';
      else if (roll < 0.55) type = 'vendor_advance';
      else if (roll < 0.8) type = 'reimbursement';
      else type = 'employee_advance';

      // A slice of employee advances are recoverable rather than budgeted, which
      // exercises the recoverable register and the head_id-NULL payment path.
      const treatment: 'budget' | 'recoverable' =
        type === 'employee_advance' && rand() < 0.35 ? 'recoverable' : 'budget';

      const amount =
        sc.name === 'OVER_BUDGET_PAID'
          ? round100(head.baseBudget * int(3, 6))
          : round100(Math.max(500, head.baseBudget * (0.02 + rand() * 0.25)));

      const partialApproval = sc.name === 'APPROVED_PARTIAL_AMOUNT';
      const approvedAmount = partialApproval ? round100(amount * (0.4 + rand() * 0.4)) : amount;
      const partialPayAmount = round100(approvedAmount * (0.3 + rand() * 0.4));

      const urgent = sc.name === 'URGENT_PENDING';

      requests.push({
        key: `r${seq}`,
        scenario: sc.name,
        type,
        treatment,
        requesterKey: requester.key,
        approverKey: approver.key,
        reassignToKey: pick(others).key,
        accountsKey: pick(accountsUsers).key,
        projectKey: project.key,
        headKey: head.key,
        amount,
        approvedAmount,
        partialPayAmount,
        vendor: pick(VENDORS),
        // The key is embedded in the title so a row can be found again in the UI
        // and so a human browsing the app can see which scenario produced it.
        shortTitle: `[r${seq}] ${pick(PURPOSES)}`,
        purpose: `${sc.name} scenario for ${project.name} / ${head.name}. Generated by the synthetic dataset run.`,
        neededBy: pick(['2026-07-31', '2026-08-14', '2026-08-28', '2026-09-10']),
        paidOn: pick(PAYABLE_DAYS),
        invoiceNo: `INV/2026/${String(seq).padStart(5, '0')}`,
        invoiceDate: pick(PAST_DAYS),
        expenseDate: pick(PAST_DAYS),
        advanceReason: 'Advance required ahead of the scheduled site work.',
        expectedReturnDate: '2026-09-30',
        repaymentNotes: 'To be recovered from the next settlement cycle.',
        urgent,
        urgencyReason: urgent ? 'Vendor has stopped supply pending payment.' : '',
      });
    }
  }
}

export const summary = {
  users: users.length,
  byRole: Object.fromEntries(ROLE_PLAN.map((r) => [r.role, r.count])),
  projects: projects.length,
  heads: projects.reduce((n, p) => n + p.heads.length, 0),
  months: MONTHS.length,
  budgetCells: budgets.length,
  vendors: VENDORS.length,
  requests: requests.length,
  scenarios: Object.fromEntries(SCENARIOS.map((s) => [s.name, s.count])),
};
