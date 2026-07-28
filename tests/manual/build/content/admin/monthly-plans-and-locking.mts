import { page, prose, section, subsection, shot, steps, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'monthly-plans-and-locking',
  title: 'Monthly plans and locking',
  summary: 'Opening a month, closing it, and what a locked month refuses.',
  blocks: [
    prose(
      'Budgets live per month, so every month has to be opened before anything can be budgeted in it. ' +
      'A month is opened here, reviewed here, and — once the figures are settled and reported — ' +
      'locked, which stops anybody writing into it by accident.',
    ),

    section('The monthly plans screen'),
    shot('admin/months', 'Every month the product knows about, newest first, with a row of shortcuts each.'),
    table(
      ['Column', 'What it shows'],
      [
        ['Month', 'The month, with the number of heads it covers underneath.'],
        ['Status', '`Open` or `Locked`. A locked month also shows the date and time it was locked.'],
        ['Source', 'The month its budgets were copied from, or `Manual` if it was started blank.'],
        ['Budget', 'The total budgeted across every head in the month.'],
        ['Actual', 'The total actually paid in the month.'],
        ['Remaining', 'Budget minus actual.'],
        ['Used', 'Actual as a percentage of budget.'],
        ['Created', 'When the plan was opened.'],
        ['Actions', '**Grid**, **Budget**, **Payments** and **Report** — the same month, on four different screens.'],
      ],
    ),

    section('Opening a month'),
    steps([
      { text: 'Pick the month in **New month**.' },
      {
        text: 'Choose a **Plan type**.',
        note: '`Copy from month` brings last month’s budget figures over as a starting point. `Start blank` opens the month with no figures at all.',
      },
      {
        text: 'If you are copying, pick the **Source month**.',
        note: 'The source has to have budgets in it. Copying from a month with none is refused with `source month has no budgets to copy`.',
      },
      {
        text: 'Select **Create Month**.',
        note: 'The month appears at the top of the table as `Open`, and you can go straight to **Budget** to adjust the figures.',
      },
    ]),
    tip(
      'Copying is almost always right. Most heads carry the same figure month after month, and ' +
      'adjusting a dozen of them on [Budgets](page:admin/budgets) is quicker than typing 229 from ' +
      'scratch.',
    ),
    note(
      'A month that is already locked cannot be recreated — the create is refused with the locked-month ' +
      'message. Unlock it first if you genuinely need to rebuild its plan.',
    ),

    section('Locking a month'),
    prose(
      'The lock control is not on the monthly plans screen. It lives in a **Month Close** panel at the ' +
      'foot of the variance grid for that month, and it is drawn only for somebody whose role carries ' +
      'the month lock permission — the **Process** column of the Budgets & variance grid row on ' +
      '[Roles and permissions](page:admin/roles-and-permissions).',
    ),
    shot(
      'admin/grid-earlier-month',
      'The variance grid for 2026-03 — the screen the lock lives on. The Month Close panel sits below the grid, past the foot of this capture.',
    ),
    steps([
      { text: 'Open **Variance grid** and pick the month you want to close.' },
      {
        text: 'Scroll to the **Month Close** panel at the foot of the screen.',
        note: 'The line above the form counts what you might want to look at first: how many heads are unpaid, how many have unbudgeted spend, and how many are over budget.',
      },
      {
        text: 'Type a **Lock reason**.',
        note: 'It is required. The reason goes into the audit trail and is shown on the grid while the month stays locked.',
      },
      {
        text: 'Select **Lock Month** and confirm.',
        note: 'The browser asks "Lock 2026-03? Budgets and payments will become read-only." first.',
      },
    ]),
    prose(
      'The month’s row on the monthly plans screen changes to a `Locked` pill with the time it was ' +
      'locked underneath.',
    ),
    shot('admin/months-locked', 'The same list with 2026-03 closed. The lock is stamped with its date and time.'),

    subsection('What a locked month refuses'),
    table(
      ['Attempt', 'What happens'],
      [
        ['Saving budgets for that month', 'Refused. The screen disables every box and says so before you try.'],
        ['Recording a payment dated in that month', 'Refused, and the payment form warns before you fill it in.'],
        ['Editing a payment dated in that month', 'Refused.'],
        ['Voiding a payment dated in that month', 'Refused.'],
        ['Opening a month plan for that month', 'Refused.'],
        ['Approving a request whose required-by date falls in that month', 'Refused, because an approval is a promise the request can be paid and it could not be.'],
      ],
    ),
    prose(
      'The general refusal reads "This month is locked. Unlock it with a reason before changing ' +
      'budgets or payments." The approval refusal is more specific: it names the month, and suggests ' +
      'either reopening it or changing the required-by date.',
    ),
    note(
      'A lock stops writes, not reads. Every screen still shows the month, the grid still totals it, ' +
      'and the reports still cover it. The variance grid marks the month with a `Locked` pill and a ' +
      'bar naming who locked it and why.',
    ),

    section('Unlocking a month'),
    steps([
      { text: 'Open the variance grid for the locked month.' },
      { text: 'Scroll to the **Month Close** panel, which now offers **Unlock Month**.' },
      {
        text: 'Type an **Unlock reason**.',
        note: 'It is required, exactly as the lock reason is. An empty reason is refused — the product will not let a closed period be reopened anonymously.',
      },
      { text: 'Select **Unlock Month** and confirm the "Unlock 2026-03 and allow changes again?" prompt.' },
    ]),
    warning(
      'Reopening a closed month reopens it for everything at once — budgets, new payments, payment ' +
      'edits and voids. If you only needed to correct one figure, lock the month again as soon as the ' +
      'correction is in.',
      'Unlocking is not selective',
    ),
    note(
      'Both halves are recorded. [The audit trail](page:admin/the-audit-trail) carries a `Month Lock` ' +
      'entry for each, reading "Locked 2026-03: …" and "Unlocked 2026-03: …" with your reason after ' +
      'the colon, and a **Before / after** expander beside it.',
      'The reason is the record',
    ),

    faq([
      {
        q: 'Where is the Lock button on the monthly plans screen?',
        a: 'There is not one. The **Actions** column offers **Grid**, **Budget**, **Payments** and **Report** only; locking is done from the **Month Close** panel at the foot of the variance grid.',
      },
      {
        q: 'I cannot see the Month Close panel at all.',
        a: 'It is drawn only for a role that holds the month lock permission. Check the **Process** column on the Budgets & variance grid row of [Roles and permissions](page:admin/roles-and-permissions).',
      },
      {
        q: 'An accountant says they cannot record a payment for last month.',
        a: 'Check the month’s status on this screen. If it reads `Locked`, either the payment belongs in an open month or the month has to be reopened with a reason.',
      },
      {
        q: 'Does locking a month stop the reports?',
        a: 'No. Reading is untouched. A lock only refuses writes.',
      },
      {
        q: 'Can I lock a month that was never opened?',
        a: 'Yes — locking opens the month plan first if it is missing, then locks it. In practice you will have opened it long before, to budget in it.',
      },
    ]),
  ],
});
