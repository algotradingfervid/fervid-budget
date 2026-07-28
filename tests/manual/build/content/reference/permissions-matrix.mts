import { page, prose, section, subsection, shot, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'reference',
  slug: 'permissions-matrix',
  title: 'The permission matrix',
  summary: 'Which of the four built-in roles may do what, taken from the running system.',
  blocks: [
    prose(
      `Nothing in this product decides anything by looking at a role name. Every screen, every button
       and every route asks the same question — does this person hold this permission — and the answer
       comes from the roles their account carries. A permission is a pair: a resource and an action,
       written here as \`request:withdraw\`.`,
    ),
    prose(
      `There are 21 resources and 66 permissions between them. Four roles ship with the product, and
       this is what each of them holds.`,
    ),
    table(
      ['Role', 'What it is for', 'Permissions held', 'People holding it'],
      [
        ['Requester', 'Raise and manage your own payment requests.', '9 of 66', '30'],
        ['Manager', 'Review and decide on payment requests.', '10 of 66', '12'],
        ['Accounts', 'Process approved requests and record payments.', '19 of 66', '10'],
        ['Admin', 'Full administrative access.', 'All 66', '4'],
      ],
      'The four built-in roles, as they stand in this system today. Both the descriptions and the counts are read from the live data.',
    ),
    note(
      `A person can hold more than one role, and their permissions are the union of all of them. These
       four are only the ones the product ships with — an administrator can create others, so your
       organisation may have roles this page does not name.`,
    ),

    section('The matrix'),
    prose(
      `Read a row as "may this role take this action on this thing". A tick is a grant that exists in
       the database right now.`,
    ),
    table(
      ['Resource', 'Action', 'Requester', 'Manager', 'Accounts', 'Admin'],
      [
        ['`approval`', '`accept_partial`', '—', '✓', '—', '✓'],
        ['`approval`', '`approve`', '—', '✓', '—', '✓'],
        ['`approval`', '`cancel`', '—', '✓', '—', '✓'],
        ['`approval`', '`reassign`', '—', '✓', '—', '✓'],
        ['`approval`', '`reject`', '—', '✓', '—', '✓'],
        ['`approval`', '`return`', '—', '✓', '—', '✓'],
        ['`attachment`', '`create`', '✓', '—', '✓', '✓'],
        ['`attachment`', '`view`', '✓', '—', '✓', '✓'],
        ['`audit`', '`view`', '—', '—', '—', '✓'],
        ['`backup`', '`create`', '—', '—', '—', '✓'],
        ['`backup`', '`view`', '—', '—', '—', '✓'],
        ['`budget`', '`edit`', '—', '—', '—', '✓'],
        ['`budget`', '`view`', '—', '—', '—', '✓'],
        ['`config`', '`edit`', '—', '—', '—', '✓'],
        ['`config`', '`view`', '—', '—', '—', '✓'],
        ['`grid`', '`export`', '—', '—', '—', '✓'],
        ['`grid`', '`view`', '—', '✓', '✓', '✓'],
        ['`head`', '`create`', '—', '—', '—', '✓'],
        ['`head`', '`edit`', '—', '—', '—', '✓'],
        ['`head`', '`view`', '—', '—', '—', '✓'],
        ['`month`', '`create`', '—', '—', '—', '✓'],
        ['`month`', '`lock`', '—', '—', '—', '✓'],
        ['`month`', '`view`', '—', '—', '—', '✓'],
        ['`notification`', '`edit`', '—', '—', '—', '✓'],
        ['`notification`', '`view`', '—', '—', '—', '✓'],
        ['`payment`', '`create`', '—', '—', '✓', '✓'],
        ['`payment`', '`edit`', '—', '—', '✓', '✓'],
        ['`payment`', '`hold`', '—', '—', '✓', '✓'],
        ['`payment`', '`mark_partial`', '—', '—', '✓', '✓'],
        ['`payment`', '`process`', '—', '—', '✓', '✓'],
        ['`payment`', '`settle`', '—', '—', '✓', '✓'],
        ['`payment`', '`view`', '—', '—', '✓', '✓'],
        ['`payment`', '`void`', '—', '—', '✓', '✓'],
        ['`project`', '`create`', '—', '—', '—', '✓'],
        ['`project`', '`edit`', '—', '—', '—', '✓'],
        ['`project`', '`view`', '—', '—', '—', '✓'],
        ['`recoverable_category`', '`create`', '—', '—', '—', '✓'],
        ['`recoverable_category`', '`delete`', '—', '—', '—', '✓'],
        ['`recoverable_category`', '`edit`', '—', '—', '—', '✓'],
        ['`recoverable_category`', '`view`', '—', '—', '—', '✓'],
        ['`recoverable_report`', '`export`', '—', '—', '✓', '✓'],
        ['`recoverable_report`', '`view`', '—', '—', '✓', '✓'],
        ['`report`', '`export`', '—', '—', '✓', '✓'],
        ['`report`', '`view`', '—', '✓', '✓', '✓'],
        ['`request`', '`cancel`', '✓', '—', '—', '✓'],
        ['`request`', '`comment`', '✓', '✓', '✓', '✓'],
        ['`request`', '`create`', '✓', '—', '—', '✓'],
        ['`request`', '`edit`', '✓', '—', '—', '✓'],
        ['`request`', '`reraise`', '✓', '—', '—', '✓'],
        ['`request`', '`view`', '✓', '✓', '✓', '✓'],
        ['`request`', '`withdraw`', '✓', '—', '—', '✓'],
        ['`reservation`', '`reassign`', '—', '—', '—', '✓'],
        ['`reservation`', '`release`', '—', '—', '✓', '✓'],
        ['`reservation`', '`reserve`', '—', '—', '✓', '✓'],
        ['`role`', '`create`', '—', '—', '—', '✓'],
        ['`role`', '`delete`', '—', '—', '—', '✓'],
        ['`role`', '`edit`', '—', '—', '—', '✓'],
        ['`role`', '`view`', '—', '—', '—', '✓'],
        ['`user`', '`create`', '—', '—', '—', '✓'],
        ['`user`', '`edit`', '—', '—', '—', '✓'],
        ['`user`', '`view`', '—', '—', '—', '✓'],
        ['`vendor`', '`create`', '—', '—', '—', '✓'],
        ['`vendor`', '`edit`', '—', '—', '—', '✓'],
        ['`vendor`', '`view`', '—', '—', '—', '✓'],
        ['`vendor_bank`', '`edit`', '—', '—', '—', '✓'],
        ['`vendor_bank`', '`view`', '—', '—', '—', '✓'],
      ],
      'All 66 permissions against the four built-in roles.',
    ),
    prose(
      `The roles screen does not draw all 66 rows. It groups them into nine pages and seven action
       columns, so one tick there can stand for several permissions at once — the **Approve** cell on
       the Payment requests row covers all six \`approval\` actions together. Every row carries an
       Advanced disclosure listing its individual permissions, which is where you go when you need one
       of them and not the rest.`,
    ),
    shot(
      'admin/roles',
      'The roles screen with the Accounts role open. On the right, **Records it can see** is the data scope for that row.',
    ),

    section('Data scopes'),
    prose(
      `A permission says what you may do. A data scope says which records you may do it to. Holding
       \`request:view\` somewhere is not the same as being allowed to read a particular request, and
       the product treats those as two separate questions.`,
    ),
    table(
      ['Scope', 'On the roles screen', 'Which records it reaches'],
      [
        ['`own`', '**Own**', 'Only the records you raised.'],
        ['`assigned`', '**Assigned**', 'Records where you are either the requester or the approver they were sent to.'],
        ['`all`', '**All**', 'Every record of that kind.'],
        ['no scope row', '**None**', 'No records at all, whatever permissions the role holds.'],
      ],
      'The four settings the Records it can see control offers.',
    ),
    prose(`These are the scopes the four built-in roles carry today.`),
    table(
      ['Role', 'Scope on `request`', 'Scope on `payment`'],
      [
        ['Requester', '`own`', 'None'],
        ['Manager', '`all`', 'None'],
        ['Accounts', '`all`', '`all`'],
        ['Admin', '`all`', '`all`'],
      ],
      'Data scopes as they stand in this system. Only these two resources are scoped.',
    ),
    warning(
      `A request your scope does not reach answers **404 Not Found**, not 403 Forbidden. That is
       deliberate. If out-of-scope requests answered 403 while missing ones answered 404, the status
       code would tell anybody typing URLs exactly which request numbers exist, and by extension how
       many requests the company raises. The two cases are made indistinguishable. The attempt is still
       recorded in the log.`,
      'Out of scope reads as "does not exist"',
    ),
    note(
      `A scope narrows and never widens. A URL may ask for a tighter view than your role carries — an
       administrator can add \`?scope=own\` to see only their own requests — but asking for a wider one
       changes nothing.`,
    ),
    prose(
      `The Manager role's request scope is \`all\`, which surprises people: an approver may open any
       request in the system, not only the ones sent to them. What is narrowed is the decision, not the
       reading. The approvals queue is always built from the requests assigned to you, and every
       decision route refuses anybody who is not the approver named on that particular request — so
       being able to read a colleague's request is not being able to approve it.`,
    ),

    section('What each role cannot reach'),
    prose(
      `The sidebar is built from permissions, so it is already an honest answer to "what can I get to".
       An entry you do not hold the permission for is not greyed out — it is simply absent, and your
       menu is shorter than a colleague's. Typing the address anyway gets the 403 page.`,
    ),

    subsection('Requester'),
    prose(
      `A requester's menu has three live entries: Home, Notifications and **My requests**. Every other
       screen in the product is closed to them — the approvals queue, the payment queue, the payments
       ledger, the recoverables register, the variance grid, budgets, monthly plans, reports, vendors,
       projects, heads, and the whole of administration.`,
    ),
    note(
      `Within their own requests a requester can do a great deal: raise, edit, withdraw, comment, attach
       documents, ask for a cancellation and re-raise a rejected request. What they cannot do is see
       anybody else's.`,
    ),

    subsection('Manager'),
    prose(
      `An approver adds **Approvals**, **Variance grid** and **Reports**. They cannot reach the payments
       ledger, the payment queue, the recoverables register, budgets, monthly plans, the vendor,
       project and head masters, or any administration screen. They can read the variance grid but not
       export it, and read reports but not export those either.`,
    ),
    shot(
      'manager/payments-refused',
      'The payments ledger opened by an approver. The role holds no `payment:view`, so the address answers 403.',
    ),

    subsection('Accounts'),
    prose(
      `The accounts role adds **Accounts queue**, **Recoverables** and **Payments ledger**, and keeps
       the variance grid and reports. It cannot reach the approvals queue — no approval permission at
       all — nor budgets, monthly plans, the masters or administration. It can export reports and the
       recoverables list, but not the variance grid.`,
    ),
    warning(
      `\`reservation:reassign\` is an administrator permission. An accountant can release a claim they
       hold themselves, but taking one off a colleague is not theirs to do. Somebody who tries is told
       "Only the person holding this reservation can release it."`,
    ),

    subsection('Admin'),
    prose(
      `The administrator holds all 66 permissions and reaches every screen. The full menu is what that
       looks like.`,
    ),
    shot(
      'shared/sidebar-admin',
      'An administrator’s sidebar, here on the Configuration screen: Requests, Payments, Budget, Reports, Masters and Admin, every group present.',
    ),
    note(
      `Holding every permission is still not holding every seat. Some acts are tied to the person, not
       the grant: only the person who raised a request may edit it or ask for it to be cancelled, and
       only the approver a request was sent to may decide it. An administrator holding
       \`request:cancel\` is still refused the cancellation form on somebody else's request.`,
    ),

    tip(
      `Roles are data, not code. If a role is the wrong shape for how your organisation works, an
       administrator can change it or copy it into a new one — see
       [Roles and permissions](page:admin/roles-and-permissions).`,
    ),

    faq([
      {
        q: 'My colleague can open a screen I cannot.',
        a: `That is roles, not a fault. Each screen is tied to a permission, and your menu lists only
            what your roles carry. Ask an administrator if you need one added.`,
      },
      {
        q: 'I opened a request URL a colleague sent me and got "not found", but they can see it.',
        a: `Your data scope does not reach it. A Requester's scope is \`own\`, so anything raised by
            somebody else answers 404 whether or not it exists. See
            [Messages and refusals](page:reference/error-messages).`,
      },
      {
        q: 'Can an approver approve their own request?',
        a: `Never, whatever roles they hold. Their own name is structurally absent from the approver
            list on the form, and the store refuses the decision even if a request somehow reached that
            state.`,
      },
      {
        q: 'Why does the roles screen show fewer rows than this page?',
        a: `It groups the 66 permissions into nine pages and seven columns so the common case is one
            tick. Open the Advanced disclosure on a row to see and set its individual permissions.`,
      },
    ]),
  ],
});
