import { page, prose, section, shot, steps, note, warning, tip, table, fields, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'expense-heads',
  title: 'Expense heads',
  summary: 'The lines within a project that spending is booked against.',
  blocks: [
    prose(
      'A head is a line of spending inside a project — Payroll, Utilities, Freight & Logistics. It is ' +
      'the level everything else attaches to: a budget figure belongs to a head and a month, a ' +
      'request names a head, and every payment is booked against one. Projects group heads; heads are ' +
      'where the money actually lands.\n\n' +
      'Head names repeat across projects on purpose. "Payroll" under Bengaluru R&D Centre and ' +
      '"Payroll" under Bhopal Assembly Shop are two different heads with two different budgets, and ' +
      'the pairing of project and head is what makes each one unique.',
    ),

    section('The screen'),
    shot('admin/heads', 'The heads list. Every head in the company, grouped by project and editable in place.'),
    prose(
      'The dataset behind this capture has 229 heads across 24 projects, so the table is long. It is ' +
      'ordered by project and then by head, and each row is its own little form with its own **Save**.',
    ),
    table(
      ['Column', 'What it holds'],
      [
        ['Project', 'Which project the head belongs to. A select, so it can be changed — see the warning below.'],
        ['Head', 'The name of the line.'],
        ['Due', 'The day of the month the spend is expected by. It drives the grid’s `Overdue` markers.'],
        ['Status', '`Active` or retired. Untick to retire.'],
        ['Order', 'Where the head sits within its project. Lower first; ties fall back to alphabetical.'],
        ['Update', 'The **Save** for that row.'],
      ],
    ),

    section('Adding a head'),
    steps([
      {
        text: 'Choose a **Project** in the form at the top.',
        note: 'Only active projects are offered here. A new head cannot be filed under a retired project.',
      },
      { text: 'Type a **Head name**.' },
      {
        text: 'Type a **Due day**.',
        note: 'A day of the month from 1 to 31. Anything else is refused with `due day must be a day from 1 to 31`.',
      },
      { text: 'Set **Order** if the head should sit somewhere particular within its project.' },
      { text: 'Leave **Active** ticked.' },
      {
        text: 'Select **Add Head**.',
        note: 'The head appears in the table. It has no budget yet — set that on [Budgets](page:admin/budgets), month by month.',
      },
    ]),
    fields([
      { name: 'Project', required: true, note: 'Required. Active projects only.' },
      { name: 'Head name', required: true, note: 'Required. An empty name is refused with `project and head name are required`.' },
      { name: 'Due day', required: false, note: 'A number from 1 to 31, or blank. It is what the variance grid uses to decide whether a head is overdue in the month you are looking at.' },
      { name: 'Order', required: false, note: 'Sort position within the project.' },
      { name: 'Active', required: false, note: 'Untick to retire the head. See below.' },
    ]),

    section('Changing a head'),
    steps([
      { text: 'Edit the fields on the row.' },
      { text: 'Select **Save** on that row.' },
    ]),
    prose(
      'Renaming a head is safe: it keeps its identity, so every budget and every payment already ' +
      'booked against it follows the new name.',
    ),
    warning(
      'Changing the **Project** on a row is not a cosmetic move. It takes every payment ever recorded ' +
      'against that head into the other project’s variance, retrospectively, and there is no undo ' +
      'beyond setting it back. Change it only when a head genuinely belongs somewhere else, and check ' +
      'the variance grid afterwards.',
      'Moving a head between projects',
    ),
    note(
      'The row select lists every project, including retired ones — a retired project reads ' +
      '`(retired)` after its name. That is deliberate: a head whose project has been retired needs a ' +
      'matching option in its own row, or saving anything else about it would move it somewhere ' +
      'nobody chose.',
    ),

    section('Retiring a head'),
    prose('There is no delete. Untick **Active** and save the row.'),
    fields([
      {
        name: 'New requests and payments',
        required: false,
        note: 'A payment booked against a retired head is refused with `This project/head is inactive.`',
      },
      {
        name: 'Budgets',
        required: false,
        note: 'A retired head keeps its row on [Budgets](page:admin/budgets) for any month in which it has a budget figure or a payment. It carries a `Retired` pill and its budget box is disabled, with the hint "Retired: kept for history, not editable."',
      },
      {
        name: 'The variance grid',
        required: false,
        note: 'Same rule: the row stays for any month it has a budget or spending in, and its Action cell reads `Retired` instead of offering **Add**. In a month where it has neither, it drops off both screens.',
      },
      {
        name: 'History',
        required: false,
        note: 'Every figure already recorded stays exactly where it is.',
      },
    ]),

    tip(
      'Retire a head at the end of a month rather than in the middle of one. Anything approved against ' +
      'it before you retire it still has to be paid, and the payment will be refused.',
    ),

    faq([
      {
        q: 'Two projects have a head with the same name. Is that a problem?',
        a: 'No — it is the normal shape. Each project keeps its own budget for its own copy, and the grid shows them as separate rows under separate project headings.',
      },
      {
        q: 'What does the Due day actually change?',
        a: 'It is the day the spend is expected by, and it is what the variance grid uses to mark a head `Overdue` when the month has moved past it and nothing has been paid. It changes no approval and blocks nothing.',
      },
      {
        q: 'Can I delete a head I created by mistake this morning?',
        a: 'No. Untick **Active** and save. If nothing was ever booked against it, a retired head is invisible everywhere that matters.',
      },
      {
        q: 'I retired a head and it vanished from a month.',
        a: 'A retired head is shown only for months in which it has a budget figure or a payment. In a month where it has neither there is nothing to show, and nothing has been lost — check a month it was actually used in.',
      },
    ]),
  ],
});
