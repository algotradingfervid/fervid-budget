import { page, prose, section, shot, steps, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'budgets',
  title: 'Budgets',
  summary: 'Setting the figures each head is allowed, month by month.',
  blocks: [
    prose(
      'A budget is one number: what a single expense head is allowed to spend in a single month. ' +
      'There is no annual figure and no project-level figure — the project total on the variance grid ' +
      'is the sum of its heads, and the year is the sum of its months. This screen is where those ' +
      'numbers are typed.',
    ),

    section('The screen'),
    shot('admin/budgets', 'The budget editor for one month. Every active head gets a row and a box to type in.'),
    prose(
      'The toolbar at the top holds the month and three ways out of it. **Open** loads the month you ' +
      'picked. **Month history** goes to [Monthly plans](page:admin/monthly-plans-and-locking). ' +
      '**View grid** goes to the variance grid for the same month.',
    ),
    table(
      ['Column', 'What it shows'],
      [
        ['Project', 'The project the head belongs to.'],
        ['Head', 'The head being budgeted.'],
        ['Status', '`Active`, or `Retired` for a head that has been switched off but still carries figures for this month.'],
        ['Budget', 'The editable figure. This is the only column you can change here.'],
        ['Actual', 'What has actually been paid against the head in this month, so far.'],
        ['Used', 'Actual as a percentage of budget.'],
      ],
    ),
    note(
      'Actual and Used are read-only and they update themselves. A head that has been overspent shows ' +
      'a percentage above 100 — the capture above has one at 486%, which is a real thing the screen ' +
      'will show you rather than an error.',
    ),

    section('Setting the figures'),
    steps([
      { text: 'Pick the month in **Month** and select **Open**.' },
      {
        text: 'Type the figures into the **Budget** boxes.',
        note: 'Amounts are shown the way the product writes money everywhere else — ₹2,23,300.00. The rupee sign and the grouping commas are both optional when you type. An empty box means no budget; a negative figure is refused with `a budget cannot be negative`, and anything that is not a number with `enter a number, or leave it empty for no budget`.',
      },
      {
        text: 'Select **Save Budgets** at the foot of the table.',
        note: 'One press saves the whole month. Every figure you changed is written, and each write leaves its own audit row.',
      },
    ]),
    note(
      'If one box cannot be read, only that box is rejected. Everything else on the page is still ' +
      'saved, and the screen comes back naming the fields it could not understand with your typing ' +
      'still in them. A single stray character does not cost you the whole month.',
      'One bad box does not lose the rest',
    ),
    tip(
      'A month that already has budgets is easier to copy than to retype. Create the next month with ' +
      '**Copy from month** on [Monthly plans](page:admin/monthly-plans-and-locking), then come here ' +
      'and adjust the handful of heads that changed.',
    ),

    section('What you cannot edit'),
    table(
      ['Situation', 'What the screen does'],
      [
        ['The month is locked', 'A `Locked` pill sits beside the heading, a bar reads "This month is locked. Unlock it before changing budgets.", every box is disabled and **Save Budgets** is disabled with them.'],
        ['The head is retired', 'The row keeps its figure, the box is disabled, and the hint under it reads "Retired: kept for history, not editable."'],
        ['The month has no active heads', 'The table reads "No active heads for this month." with a link to the heads screen.'],
      ],
    ),
    warning(
      'Unlocking a month to change a budget reopens it for payments as well, and the unlock needs a ' +
      'reason that goes on the record. Think about whether the figure genuinely has to change ' +
      'retrospectively before you reopen a closed period — see ' +
      '[Monthly plans and locking](page:admin/monthly-plans-and-locking).',
      'Editing a closed month',
    ),

    section('How budgets are used'),
    prose(
      'The figure you type here does not block anything on its own. Nothing refuses a request or a ' +
      'payment for being over budget; the product records the overspend and shows it. Where the ' +
      'number does its work is in what people see:\n\n' +
      'The variance grid colours each head by how much of its budget is used and totals the ' +
      'difference. The reports break the same comparison down by month, by project and by head. The ' +
      'monthly plan list shows a budget, an actual and a remaining figure for the whole month at once.',
    ),

    faq([
      {
        q: 'I typed a figure and the total did not move.',
        a: 'Check that you selected **Save Budgets**. Typing in the boxes changes nothing until the form is submitted, and there is no auto-save.',
      },
      {
        q: 'A head I need is not in the list.',
        a: 'Either it does not exist yet or it has been retired. Add it or reactivate it on [Expense heads](page:admin/expense-heads), then come back and reload the month.',
      },
      {
        q: 'Can I budget for a month that does not exist yet?',
        a: 'Open the month first on [Monthly plans](page:admin/monthly-plans-and-locking). Saving budgets does create the month plan if it is missing, but opening it deliberately is clearer and lets you copy last month’s figures in one step.',
      },
      {
        q: 'What happens if spending goes past the budget?',
        a: 'Nothing is refused. The head shows over 100% used and the variance goes negative, which is what the grid and the reports are there to surface.',
      },
    ]),
  ],
});
