import { page, prose, section, subsection, shot, note, warning, table, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'what-you-can-do',
  title: 'What an administrator can do',
  summary: 'The administrator reaches every screen in the product. Here is the map.',
  blocks: [
    prose(
      `The administrator role holds every permission the product defines — all 66 of them, and the
       widest data scope on both requests and payments. It is a strict superset of the other three
       built-in roles, so anything a requester, an approver or the accounts team can reach, you can
       reach as well.

       That is worth saying plainly because it changes how you read the rest of this manual. Where
       another page says "you will only see this with such-and-such a permission", you see it. When
       a colleague reports a missing button, the way to reproduce it is not to look at your own
       screen — it is to look at their role.`,
    ),

    section('Your home screen'),
    shot('shared/dashboard-admin', 'The dashboard as an administrator: every queue in the product, plus an Administration card.'),
    prose(
      `The tiles across the top count work, not records. **Needs my action** and **My open requests**
       are your own requests; **Awaiting my approval** and **Cancellation requests** are decisions
       routed to you; **Approved, unclaimed** is the accounts team's queue. An administrator who
       raises nothing and approves nothing sees zeros in the first four and a large number in the
       fifth, which is what the capture above shows.

       Underneath, each area lists the first four rows you can act on and links to the queue behind
       them. An area with nothing in it is not drawn at all.`,
    ),

    subsection('The Administration card'),
    prose(
      `The card on the right is a shortcut, not a menu. It carries four destinations, each shown only
       to somebody who may open it:`,
    ),
    table(
      ['Link', 'Sub-line', 'Where it goes'],
      [
        ['**Configuration**', '`Numbering, attachments, urgency, approvals`', '[Configuration](page:admin/configuration)'],
        ['**Roles & permissions**', '`Who can do what`', '[Roles and permissions](page:admin/roles-and-permissions)'],
        ['**Users**', '`People and their default approvers`', '[People and accounts](page:admin/people)'],
        ['**Vendors**', '`The vendor master`', '[Vendors](page:admin/vendors)'],
      ],
    ),

    section('The full menu'),
    shot('shared/sidebar-admin', 'The sidebar as an administrator sees it, grouped by the job each screen does.'),
    prose(
      `The sidebar is built from permissions alone. Every entry declares the resource and action it
       needs, and a group with no visible entries disappears rather than sitting there empty. Yours
       is the longest version of it that exists.`,
    ),

    subsection('Budget'),
    table(
      ['Menu entry', 'What it is for', 'Page'],
      [
        ['**Variance grid**', 'Budget against actual for every head in a month. The month lock lives at the foot of it.', '[Monthly plans and locking](page:admin/monthly-plans-and-locking)'],
        ['**Budgets**', 'The figure each head is allowed, month by month.', '[Budgets](page:admin/budgets)'],
        ['**Monthly plans**', 'Opening a month and reviewing the ones already opened.', '[Monthly plans and locking](page:admin/monthly-plans-and-locking)'],
      ],
    ),

    subsection('Masters'),
    table(
      ['Menu entry', 'What it is for', 'Page'],
      [
        ['**Vendors**', 'Everybody you pay, and the restricted bank block behind each one.', '[Vendors](page:admin/vendors)'],
        ['**Projects**', 'The top level of the budget structure.', '[Projects](page:admin/projects)'],
        ['**Heads**', 'The lines within a project that spending is booked against.', '[Expense heads](page:admin/expense-heads)'],
      ],
    ),

    subsection('Admin'),
    table(
      ['Menu entry', 'What it is for', 'Page'],
      [
        ['**Users**', 'Accounts, roles held, default approvers, deactivation.', '[People and accounts](page:admin/people)'],
        ['**Roles & permissions**', 'The matrix, the data scopes, and roles of your own.', '[Roles and permissions](page:admin/roles-and-permissions)'],
        ['**Configuration**', 'The settings that change how the product behaves for everybody.', '[Configuration](page:admin/configuration)'],
        ['**Notification rules**', 'Who is told what, and the mail server that tells them.', '[Notification rules and email](page:admin/notification-rules)'],
        ['**Audit log**', 'Who did what, when.', '[The audit trail](page:admin/the-audit-trail)'],
        ['**Backups**', 'Taking a copy of the database and the attachments.', '[Backups](page:admin/backups)'],
      ],
    ),

    section('What the role still will not let you do'),
    prose(
      `Holding every permission is not the same as being able to do everything. A handful of rules
       are structural, and they apply to you exactly as they apply to everybody else.`,
    ),
    table(
      ['Rule', 'Why it is there'],
      [
        ['You can never approve a request you raised.', 'Self-approval is blocked always, whatever roles you hold. The Configuration screen shows the control ticked and greyed out to say so.'],
        ['You cannot edit or remove a row of the audit trail.', 'There is no route that changes one. See [The audit trail](page:admin/the-audit-trail).'],
        ['A payment tied to a request cannot be edited or voided.', 'It would silently rewrite what the requester and the approver were told was paid.'],
        ['You cannot deactivate or demote your own administrator account.', 'The refusal reads `You cannot deactivate or demote your own administrator account.`'],
        ['The last active administrator cannot be removed.', 'The refusal reads `at least one active administrator is required`.'],
        ['A locked month refuses budget and payment changes until it is reopened.', 'Reopening is a control you hold, with a reason. See [Monthly plans and locking](page:admin/monthly-plans-and-locking).'],
      ],
    ),

    note(
      `Every administrative change is written to the audit trail as you make it — configuration saves,
       role edits, user saves, month locks, category deletes and backups all leave a row with your
       name on it. That is the point of holding the role: the record is what makes the access
       accountable.`,
    ),

    warning(
      `Your account can reach vendor bank details, every request in the company and every payment ever
       recorded. Sign out on any machine you share, and prefer giving a colleague a narrower role over
       lending them yours.`,
      'The account with the widest reach',
    ),

    faq([
      {
        q: 'A colleague says a button is missing. Where do I start?',
        a: `Not on your own screen — you hold every permission, so you will always see it. Open
            [Roles and permissions](page:admin/roles-and-permissions), select the role they hold, and
            read the row the button belongs to.`,
      },
      {
        q: 'Can I make a role that is almost an administrator?',
        a: `Yes. Copy the Admin role, give the copy a name, then untick what it should not have. See
            [Roles and permissions](page:admin/roles-and-permissions).`,
      },
      {
        q: 'Is there a screen that lists everything I have changed?',
        a: `[The audit trail](page:admin/the-audit-trail). Filter the Actor field by your own name.`,
      },
    ]),
  ],
});
