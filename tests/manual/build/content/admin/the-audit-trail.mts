import { page, prose, section, subsection, shot, steps, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'the-audit-trail',
  title: 'The audit trail',
  summary: 'Who did what, when — the record that cannot be edited.',
  blocks: [
    prose(
      'Every act that changes a financial record leaves a row here, with the person who did it, the ' +
      'time they did it, and — where there is one — the before and the after. The screen’s own ' +
      'sub-line puts it plainly: review who changed financial records, when, and why.\n\n' +
      'It is a record and not a workspace. There is nothing on this screen that changes anything, ' +
      'because the value of the trail is that it cannot be tidied up afterwards.',
    ),

    section('The screen'),
    shot('admin/audit', 'The audit trail, newest first. Entries with a before-and-after carry an expander.'),
    table(
      ['Column', 'What it holds'],
      [
        ['When', 'The date and time of the act.'],
        ['Actor', 'Who did it, by name.'],
        ['Entity', 'What kind of thing was touched — `User`, `Month Lock`, `Payment`, `Vendor` and so on.'],
        ['Action', 'What was done to it, as a coloured pill.'],
        ['Summary', 'A sentence describing the act, and a **Before / after** expander where there is stored detail.'],
      ],
    ),
    prose(
      'The capture above shows two month-lock entries in among the sign-ins: "Locked 2026-03: Month ' +
      'closed for reporting." and "Unlocked 2026-03: Reopened after capturing the manual." The text ' +
      'after each colon is the reason somebody typed at the time. That is the whole argument for ' +
      'requiring a reason on a month lock — a year later it is the only thing that explains the row.',
    ),

    subsection('Before and after'),
    prose(
      'Where the product stored the record’s state, the Summary carries a **Before / after** ' +
      'disclosure. Open it and you get the stored values as they were and as they became. Not every ' +
      'action has one — a sign-in has nothing to compare — so the expander appears only on rows that ' +
      'can fill it.',
    ),

    section('Finding something'),
    steps([
      { text: 'Choose an **Entity** if you know what kind of record you are after.' },
      { text: 'Choose an **Action** if you know what was done to it.' },
      { text: 'Type a name into **Actor** to narrow to one person. It matches on any part of the name.' },
      { text: 'Select **Filter**.' },
      { text: 'Select **Reset** to clear everything and start again.' },
    ]),

    subsection('The entities you can filter by'),
    prose(
      '`Payment request`, `Payment`, `Budget`, `Budget month`, `Month lock`, `Project`, `Head`, ' +
      '`Vendor`, `Recoverable category`, `Recoverable report`, `User`, `Role`, `Notification ' +
      'setting`, `App setting`, `Report`, `Backup`.',
    ),

    subsection('The actions you can filter by'),
    prose(
      'The list is ordered so the request workflow reads as a workflow rather than as an alphabet: ' +
      '`create`, `update`, `delete`, `attach`, `comment`, then `submit`, `approve`, `return`, ' +
      '`reject`, `withdraw`, `reraise`, then `cancel_request`, `cancel`, `approval_reassign`, then ' +
      '`process`, `release`, `reassign`, `hold`, `unhold`, then `settle`, `mark_partial`, ' +
      '`accept_partial`, `concern`, `remind`, then `void`, `lock`, `unlock`, `settings`, `export`, ' +
      'and finally `login`, `logout`, `login_failed`.',
    ),
    tip(
      'Two filters are usually enough. "Everything Fervid Admin did to a Month Lock" is Entity plus ' +
      'Actor, and it answers most of the questions people actually bring to this screen.',
    ),

    section('What the trail cannot do'),
    warning(
      'Nothing on this screen edits or removes a row, and there is no route anywhere in the product ' +
      'that does. An entry that is wrong stays; the correction is a new act, which leaves its own ' +
      'entry beside it. That is what makes the trail worth reading.',
      'The record cannot be edited',
    ),
    note(
      'The screen shows at most 1000 entries, newest first, and it does not page beyond that. On a ' +
      'busy system the unfiltered view is therefore recent history rather than all of it — filter by ' +
      'entity, action or actor to reach further back.',
      'The 1000-row limit',
    ),
    warning(
      'The trail carries the summaries and stored values of financial records, so treat it as ' +
      'sensitive. Vendor bank details are the one thing deliberately kept out of it: a vendor save ' +
      'records the name, type, status, GSTIN and city, and for the bank block only a flag saying ' +
      'whether it was touched. See [Vendor bank details](page:admin/vendor-bank-details).',
      'Who should be able to read it',
    ),

    section('What lands in it'),
    prose(
      'Not everything the product does, but everything that changes a record somebody may later have ' +
      'to account for. Among them: creating and editing users and roles, saving configuration, saving ' +
      'a budget, opening a month, locking and unlocking a month, creating and updating projects, heads ' +
      'and vendors, saving and deleting recoverable categories, recording, editing and voiding ' +
      'payments, every decision on a request, taking a backup, and signing in and out.',
    ),
    note(
      'Some acts are recorded in more than one place, and deliberately so. A month lock writes a row ' +
      'here and shows its reason on the variance grid; a payment void writes a row here and keeps its ' +
      'reason on the payment. The trail is the copy that survives when a record is changed again.',
    ),

    faq([
      {
        q: 'Can I export the audit trail?',
        a: 'There is no export control on this screen. The filters are how you narrow it, and the browser is how you read it.',
      },
      {
        q: 'Somebody says they did not do something, but their name is on a row.',
        a: 'The name is the account that was signed in. If it is genuinely not them, that is a password question — reset it from [People and accounts](page:admin/people) and look at the `login` and `login_failed` rows around the time.',
      },
      {
        q: 'A change I made is not here.',
        a: 'Check the filters first, then the 1000-row limit. If it is genuinely absent, the act may not be one the product records — not every read or navigation leaves a row, only changes do.',
      },
      {
        q: 'Why does one row have Before / after and the next one does not?',
        a: 'Because only some acts have a stored state worth comparing. A sign-in has nothing to compare; a budget save has an old figure and a new one.',
      },
      {
        q: 'Can I stop somebody seeing this screen?',
        a: 'Yes. It is gated on the audit view grant in the **Administration** row of [Roles and permissions](page:admin/roles-and-permissions).',
      },
    ]),
  ],
});
