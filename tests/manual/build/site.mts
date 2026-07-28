// The manual's table of contents.
//
// This file is the contract between the people writing the pages. It fixes the
// slug, title and one-line summary of every page up front; each content file
// then fills in only the blocks. Because the inventory lives here and not in the
// content files, several authors can work at once without touching a shared
// file, and the build can tell the difference between "page not written yet"
// and "page written but never linked".
//
// The inventory is derived from the verified route map: 41 full-page screens
// across four roles, grouped by the job the reader is trying to do rather than
// by URL, because a reader looks for "how do I get paid back" and not for
// "/requests/new?type=reimbursement".

import type { SectionKey } from './schema.mts';

export interface SectionDef {
  key: SectionKey;
  title: string;
  /** Shown on the cover card. Written for somebody deciding where to start. */
  blurb: string;
}

export const SECTIONS: SectionDef[] = [
  {
    key: 'getting-started',
    title: 'Getting started',
    blurb: 'Signing in, reading the screen, and the parts of the product everybody shares.',
  },
  {
    key: 'requester',
    title: 'Raising requests',
    blurb: 'For anybody who needs money paid out: raising a request and following it through.',
  },
  {
    key: 'manager',
    title: 'Approving',
    blurb: 'For approvers: the queue, the five decisions you can take, and what each one does.',
  },
  {
    key: 'accounts',
    title: 'Paying',
    blurb: 'For the accounts team: claiming work, recording payments, and the ledger.',
  },
  {
    key: 'admin',
    title: 'Administration',
    blurb: 'For administrators: people, permissions, budgets, masters and the audit trail.',
  },
  {
    key: 'reference',
    title: 'Reference',
    blurb: 'The lifecycle, the statuses, the permission matrix and what the error messages mean.',
  },
];

export interface PageDecl {
  section: SectionKey;
  slug: string;
  title: string;
  summary: string;
}

export const SITE: PageDecl[] = [
  // ------------------------------------------------------- getting started
  {
    section: 'getting-started',
    slug: 'signing-in',
    title: 'Signing in',
    summary: 'How to get into Fervid Budget, and what to do when it will not let you.',
  },
  {
    section: 'getting-started',
    slug: 'finding-your-way',
    title: 'Finding your way around',
    summary: 'The parts of every screen: the sidebar, the header, and why your menu may be shorter than a colleague’s.',
  },
  {
    section: 'getting-started',
    slug: 'the-dashboard',
    title: 'Your home screen',
    summary: 'What the dashboard puts in front of you, and how it changes with your role.',
  },
  {
    section: 'getting-started',
    slug: 'a-request-in-detail',
    title: 'Anatomy of a request',
    summary: 'The one screen everybody shares: the details, the documents and the conversation thread.',
  },
  {
    section: 'getting-started',
    slug: 'notifications',
    title: 'Notifications',
    summary: 'How the product tells you something needs you, and how to catch up on what you missed.',
  },

  // ------------------------------------------------------------- requester
  {
    section: 'requester',
    slug: 'what-you-can-do',
    title: 'What a requester can do',
    summary: 'The whole job in one page: what you may raise, see and change.',
  },
  {
    section: 'requester',
    slug: 'choosing-a-request-type',
    title: 'Choosing a request type',
    summary: 'Vendor invoice, reimbursement, employee advance or vendor advance — which one you want, and why it matters.',
  },
  {
    section: 'requester',
    slug: 'raising-a-request',
    title: 'Raising a request',
    summary: 'Filling in the form and sending it for approval, field by field.',
  },
  {
    section: 'requester',
    slug: 'budget-and-recoverable',
    title: 'Budget spend or money you expect back',
    summary: 'The treatment choice, and what changes when you say the money is coming back.',
  },
  {
    section: 'requester',
    slug: 'attachments',
    title: 'Attaching documents',
    summary: 'Adding invoices and receipts, and what to do when you do not have one yet.',
  },
  {
    section: 'requester',
    slug: 'urgent-requests',
    title: 'Marking a request urgent',
    summary: 'When to flag a request as urgent, and what it changes for the people downstream.',
  },
  {
    section: 'requester',
    slug: 'tracking-your-requests',
    title: 'Tracking your requests',
    summary: 'The list, its tabs and filters, and how to tell at a glance what is waiting on you.',
  },
  {
    section: 'requester',
    slug: 'when-a-request-is-returned',
    title: 'When a request comes back to you',
    summary: 'Reading the approver’s reason, correcting the request and sending it again.',
  },
  {
    section: 'requester',
    slug: 'editing-and-withdrawing',
    title: 'Changing or withdrawing a request',
    summary: 'What you can still edit after submitting, and how to take a request back.',
  },
  {
    section: 'requester',
    slug: 'asking-to-cancel',
    title: 'Asking to cancel an approved request',
    summary: 'Once a request is approved you ask rather than act — how that works.',
  },
  {
    section: 'requester',
    slug: 'after-a-rejection',
    title: 'After a rejection',
    summary: 'Why rejection is final, and how to raise a corrected request in its place.',
  },
  {
    section: 'requester',
    slug: 'getting-paid',
    title: 'Getting paid',
    summary: 'What happens after approval, and how to read a payment that is short of the full amount.',
  },

  // --------------------------------------------------------------- manager
  {
    section: 'manager',
    slug: 'what-you-can-do',
    title: 'What an approver can do',
    summary: 'The five decisions available to you, and the limits of the role.',
  },
  {
    section: 'manager',
    slug: 'the-approvals-queue',
    title: 'The approvals queue',
    summary: 'Everything waiting on you, split by the kind of decision it needs.',
  },
  {
    section: 'manager',
    slug: 'reviewing-a-request',
    title: 'Reviewing a request',
    summary: 'What to check before you decide, and where to find it on the screen.',
  },
  {
    section: 'manager',
    slug: 'approving',
    title: 'Approving a request',
    summary: 'Approving in full, and approving a smaller amount than was asked for.',
  },
  {
    section: 'manager',
    slug: 'returning-for-correction',
    title: 'Returning a request for correction',
    summary: 'Sending a request back with a reason, and what the requester sees next.',
  },
  {
    section: 'manager',
    slug: 'rejecting',
    title: 'Rejecting a request',
    summary: 'The decision that cannot be undone, and when to prefer returning instead.',
  },
  {
    section: 'manager',
    slug: 'reassigning',
    title: 'Reassigning an approval',
    summary: 'Handing a decision to somebody else when it is not yours to take.',
  },
  {
    section: 'manager',
    slug: 'cancellation-requests',
    title: 'Deciding a cancellation request',
    summary: 'When a requester asks to cancel something you already approved.',
  },
  {
    section: 'manager',
    slug: 'shortfall-review',
    title: 'Reviewing a shortfall',
    summary: 'When Accounts pays less than you approved, the difference comes back to you.',
  },
  {
    section: 'manager',
    slug: 'the-variance-grid',
    title: 'The variance grid',
    summary: 'Budget against actual for every head in a month, and how to read the colours.',
  },
  {
    section: 'manager',
    slug: 'reports',
    title: 'Reports',
    summary: 'The monthly, by-project and by-head views, and what each answers.',
  },
  {
    section: 'manager',
    slug: 'limits-of-the-role',
    title: 'What an approver cannot see',
    summary: 'The screens the approver role does not reach, and what to do when you need them.',
  },

  // -------------------------------------------------------------- accounts
  {
    section: 'accounts',
    slug: 'what-you-can-do',
    title: 'What the accounts role can do',
    summary: 'The settlement job end to end, and what is deliberately out of your hands.',
  },
  {
    section: 'accounts',
    slug: 'the-payment-queue',
    title: 'The payment queue',
    summary: 'Approved requests waiting to be paid, and how the tabs divide the work.',
  },
  {
    section: 'accounts',
    slug: 'claiming-a-request',
    title: 'Taking a request for processing',
    summary: 'Claiming a request so two people cannot pay the same thing twice.',
  },
  {
    section: 'accounts',
    slug: 'recording-a-payment',
    title: 'Recording a payment',
    summary: 'The payment form field by field, and what the product checks before it accepts one.',
  },
  {
    section: 'accounts',
    slug: 'paying-part-of-a-request',
    title: 'Paying part of a request',
    summary: 'Settling short of the approved amount, and what happens to the difference.',
  },
  {
    section: 'accounts',
    slug: 'holding-a-request',
    title: 'Putting a request on hold',
    summary: 'Parking an approved request with a reason, and taking it off hold again.',
  },
  {
    section: 'accounts',
    slug: 'raising-a-concern',
    title: 'Raising a concern',
    summary: 'Pushing back on a request you cannot pay as it stands.',
  },
  {
    section: 'accounts',
    slug: 'reservations-and-conflicts',
    title: 'Reservations and conflicts',
    summary: 'What happens when somebody else already holds a request, and how a stale claim is released.',
  },
  {
    section: 'accounts',
    slug: 'the-payments-ledger',
    title: 'The payments ledger',
    summary: 'Every payment recorded, how to filter it, and how to read one in detail.',
  },
  {
    section: 'accounts',
    slug: 'correcting-a-payment',
    title: 'Correcting or voiding a payment',
    summary: 'What can still be changed after a payment is recorded, and what is sealed.',
  },
  {
    section: 'accounts',
    slug: 'the-recoverables-register',
    title: 'The recoverables register',
    summary: 'Tracking money that is expected back, by category and by counterparty.',
  },

  // ----------------------------------------------------------------- admin
  {
    section: 'admin',
    slug: 'what-you-can-do',
    title: 'What an administrator can do',
    summary: 'The administrator reaches every screen in the product. Here is the map.',
  },
  {
    section: 'admin',
    slug: 'people',
    title: 'People and accounts',
    summary: 'Adding colleagues, setting who approves for whom, and deactivating leavers.',
  },
  {
    section: 'admin',
    slug: 'roles-and-permissions',
    title: 'Roles and permissions',
    summary: 'The permission matrix, data scopes, and how to make a role of your own.',
  },
  {
    section: 'admin',
    slug: 'projects',
    title: 'Projects',
    summary: 'The top level of the budget structure, and what happens when you retire one.',
  },
  {
    section: 'admin',
    slug: 'expense-heads',
    title: 'Expense heads',
    summary: 'The lines within a project that spending is booked against.',
  },
  {
    section: 'admin',
    slug: 'budgets',
    title: 'Budgets',
    summary: 'Setting the figures each head is allowed, month by month.',
  },
  {
    section: 'admin',
    slug: 'monthly-plans-and-locking',
    title: 'Monthly plans and locking',
    summary: 'Opening a month, closing it, and what a locked month refuses.',
  },
  {
    section: 'admin',
    slug: 'vendors',
    title: 'Vendors',
    summary: 'The vendor master, and the payment history behind each one.',
  },
  {
    section: 'admin',
    slug: 'vendor-bank-details',
    title: 'Vendor bank details',
    summary: 'The most sensitive data in the product, and who is allowed to see it.',
  },
  {
    section: 'admin',
    slug: 'recoverable-categories',
    title: 'Recoverable categories',
    summary: 'The list requesters choose from when money is expected back.',
  },
  {
    section: 'admin',
    slug: 'notification-rules',
    title: 'Notification rules and email',
    summary: 'Deciding who is told what, and pointing the product at a mail server.',
  },
  {
    section: 'admin',
    slug: 'configuration',
    title: 'Configuration',
    summary: 'The settings that change how the product behaves for everybody.',
  },
  {
    section: 'admin',
    slug: 'the-audit-trail',
    title: 'The audit trail',
    summary: 'Who did what, when — the record that cannot be edited.',
  },
  {
    section: 'admin',
    slug: 'backups',
    title: 'Backups',
    summary: 'Taking a copy of the database, and what the copy does and does not include.',
  },

  // ------------------------------------------------------------- reference
  {
    section: 'reference',
    slug: 'request-lifecycle',
    title: 'The request lifecycle',
    summary: 'Every status a request can hold, and every route between them.',
  },
  {
    section: 'reference',
    slug: 'statuses-and-badges',
    title: 'Statuses and badges',
    summary: 'What each badge means on sight, in the colours the product uses.',
  },
  {
    section: 'reference',
    slug: 'permissions-matrix',
    title: 'The permission matrix',
    summary: 'Which of the four built-in roles may do what, taken from the running system.',
  },
  {
    section: 'reference',
    slug: 'error-messages',
    title: 'Messages and refusals',
    summary: 'What the product says when it will not do something, and what to do about it.',
  },
  {
    section: 'reference',
    slug: 'glossary',
    title: 'Glossary',
    summary: 'The words this product uses, and exactly what each one means here.',
  },
];
