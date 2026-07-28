import { page, prose, section, subsection, shot, steps, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'roles-and-permissions',
  title: 'Roles and permissions',
  summary: 'The permission matrix, data scopes, and how to make a role of your own.',
  blocks: [
    prose(
      `Roles are data in this product, not code. The sub-line on the screen says so in as many words:
       you create them, copy them, and set what each one may do and see. Nothing needs rebuilding for
       a new role to exist, and nothing about a role is hard-wired to a screen — every screen asks the
       permission set what the signed-in person may do, and the answer comes from the roles they hold.`,
    ),

    section('The screen'),
    shot('admin/roles', 'The permission matrix for one role. The tabs across the top are the roles that exist.'),
    prose(
      'The tabs are the roles. Four are seeded and marked as system roles — Requester, Manager, ' +
      'Accounts and Admin — and any role you create yourself appears alongside them carrying a ' +
      '`custom` marker.\n\n' +
      'Under the tabs sits the selected role’s card: its name, its description, and a pill counting ' +
      'how many people hold it. The matrix below is that role’s permissions, and the bar at the foot ' +
      'warns you what a save costs — "Changes apply to all N users holding this role."',
    ),

    subsection('The rows'),
    prose(
      `Nine rows, each one a part of the product rather than a database table. Every permission the
       product defines lives in exactly one of them.`,
    ),
    table(
      ['Row', 'What it governs'],
      [
        ['Payment requests', 'Raising, viewing, editing, commenting and deciding requests, plus their attachments.'],
        ['Payments', 'The ledger: viewing, recording, editing, processing and voiding payments.'],
        ['Reservations', 'Claiming an approved request for processing, handing it on, and releasing it.'],
        ['Recoverables', 'The recoverables register and the categories behind it.'],
        ['Vendors', 'The vendor master — everything except the bank block.'],
        ['Vendor bank details', 'The bank block alone. Its own row because it is its own permission.'],
        ['Budgets & variance grid', 'Budgets, the grid, monthly plans and the month lock, plus projects and heads.'],
        ['Reports', 'The monthly, by-project and by-head reports, and their export.'],
        ['Administration', 'Users, roles, notification rules, configuration, the audit trail and backups.'],
      ],
    ),

    subsection('The columns'),
    table(
      ['Column', 'Roughly'],
      [
        ['View', 'May open the screen and read the records.'],
        ['Create', 'May add a new one.'],
        ['Edit', 'May change an existing one.'],
        ['Approve', 'May take an approval decision — approve, reject, return, reassign, accept a shortfall.'],
        ['Process', 'May move work along: settle a payment, lock a month.'],
        ['Cancel', 'May take something out of the pipeline: withdraw, cancel, void, delete.'],
        ['Export', 'May download the screen as a file.'],
      ],
    ),
    note(
      `A cell that shows an em dash instead of a tick box is not "off" — it is unavailable. There is no
       such action for that row, so there is nothing to grant. A disabled tick box would read as off,
       which is a different claim, so the screen does not draw one.`,
      'Em dash, not an unticked box',
    ),

    subsection('Advanced'),
    prose(
      `Each row carries an **Advanced — every permission behind this row** disclosure. A cell is a set
       of permissions, not one: ticking **Approve** on the Payment requests row grants approve, reject,
       return, reassign and accept-partial together. Open Advanced when you need finer control than
       that — for instance a role that may return a request for correction but never reject one
       outright.`,
    ),

    subsection('Records it can see'),
    prose(
      'The last column is the data scope, and it is a different question from the tick boxes. The ' +
      'tick boxes decide what the role may *do*; the scope decides *which records* it may do it to. ' +
      'Only two rows carry one — Payment requests and Payments — because only requests and payments ' +
      'are scoped. Every other row shows a fixed `None`.',
    ),
    table(
      ['Setting', 'What the role sees'],
      [
        ['None', 'No scope stored at all.'],
        ['Own', 'Only records the person raised themselves.'],
        ['Assigned', 'Records routed to them — the ones they are the approver or the assigned accountant for.'],
        ['All', 'Every record in the company.'],
      ],
    ),
    prose(
      'In the seeded data the Requester role sees `own` requests; Manager and Accounts both see ' +
      '`all` requests; Accounts and Admin see `all` payments. Administrators hold every permission ' +
      'and the widest scope on both.',
    ),

    section('Editing a role'),
    steps([
      { text: 'Select the role’s tab.' },
      { text: 'Change the tick boxes, or open **Advanced** and change individual permissions.' },
      { text: 'Choose a scope in **Records it can see** if the row has one.' },
      {
        text: 'Select **Save role**.',
        note: 'The whole grant set is replaced by what is ticked, so anything you untick is revoked. **Discard** abandons the changes and reloads the stored version.',
      },
    ]),
    warning(
      `A save takes effect on the holders' very next page load. There is nothing to restart and no
       grace period — if you untick a row somebody is halfway through using, their next click is
       refused. Make role changes when the people holding them are not mid-task.`,
      'Changes are immediate',
    ),

    section('Making a role of your own'),
    subsection('From nothing'),
    shot('admin/role-new-sheet', 'The New role panel. The sub-line is a warning worth reading: it starts with no permissions at all.'),
    steps([
      { text: 'Select **＋ New role**.' },
      { text: 'Type a **Role name**.' },
      { text: 'Type a **Description**. It is what people read beside the tick box on somebody’s user record, so make it a sentence.' },
      {
        text: 'Select **Create role**.',
        note: 'You land on the new role’s own tab with an empty matrix. Nothing is granted until you tick something and save.',
      },
    ]),

    subsection('From an existing role'),
    shot('admin/role-copy-sheet', 'Copying a role. The copy starts with the same permissions and the same data scope.'),
    steps([
      { text: 'Select the role you want to start from.' },
      { text: 'Select **Copy this role**.' },
      { text: 'Type a **New role name**.' },
      {
        text: 'Select **Copy role**.',
        note: 'You land on the copy, already carrying everything the original had. Take away what it should not have, then save.',
      },
    ]),
    tip(
      `Copying is almost always the better start. "Accounts, but without the ability to void a payment"
       is two clicks from a copy and twenty from an empty role.`,
    ),

    section('Deleting a role'),
    prose(
      'A **Delete role** control appears in the role card, beside the holder count, only for a role ' +
      'you created. The four system roles cannot be deleted; there is no control for them, and the ' +
      'server refuses it a second time with `system roles cannot be deleted`.',
    ),
    warning(
      `A role that somebody still holds cannot be deleted either. The refusal names the number:
       "*Name* is assigned to N users — reassign them before deleting the role". Take the role off
       everybody first, from [People and accounts](page:admin/people), and delete it afterwards.`,
      'Reassign before you delete',
    ),

    section('The administrator role'),
    prose(
      'Admin is the widest role there is. It holds every one of the 66 permissions the product ' +
      'defines and the `all` scope on both requests and payments, which makes it a strict superset ' +
      'of the other three. That is why an administrator reaches every screen, and why you cannot ' +
      'reproduce a colleague’s missing button on your own account — see ' +
      '[What an administrator can do](page:admin/what-you-can-do).',
    ),

    faq([
      {
        q: 'A cell is ticked but the person still cannot do the thing.',
        a: 'Check the scope in the same row. A role can hold `payment:edit` and see no payments at all if its scope on that row is `None`.',
      },
      {
        q: 'Why can I not rename a system role?',
        a: 'The **Role name** field is read-only for the four seeded roles. The **Description** is not, so you can still explain what your organisation uses the role for.',
      },
      {
        q: 'I ticked a cell and only some of the Advanced boxes came on.',
        a: `That is a partially granted cell — the role already held some of the permissions behind it.
            Open **Advanced** to see exactly which.`,
      },
      {
        q: 'Does deleting a role delete the people who held it?',
        a: 'No, and it cannot even be attempted: the delete refuses while anybody holds the role.',
      },
      {
        q: 'Where is the full list of what each built-in role holds?',
        a: '[The permission matrix](page:reference/permissions-matrix), taken from the running system.',
      },
    ]),
  ],
});
