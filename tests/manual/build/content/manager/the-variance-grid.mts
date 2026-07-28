import { page, prose, section, shot, table, note, warning, tip, faq } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'the-variance-grid',
  title: 'The variance grid',
  summary: 'Budget against actual for every head in a month, and how to read the colours.',
  blocks: [
    prose(
      `**Variance grid** in the sidebar opens the widest screen in the product: one row for every
       expense head in every project, for one month, with what was budgeted beside what was spent.
       It is the screen to consult before approving something large against a head you do not know
       well.

       The line under the heading states the arithmetic the whole page rests on — *remaining = budget
       - actual*.`,
    ),
    shot('manager/grid', 'The variance grid for one month, grouped by project.'),

    section('The four figures at the top'),
    table(
      ['Figure', 'What it is'],
      [
        ['**Budget**', 'Everything budgeted across every head, for the month shown.'],
        ['**Actual**', 'Everything paid in that month.'],
        ['**Remaining**', 'Budget minus actual. Shown in red when spend has gone past budget.'],
        ['**Used**', 'Actual as a percentage of budget.'],
      ],
    ),
    prose(
      `Large figures are abbreviated here — *₹10.34 Cr* — while the table below carries them in full.`,
    ),

    section('What counts as actual'),
    warning(
      `The grid counts money that has been paid, not money you have approved. A request you approved
       this morning changes nothing on this screen until the accounts team settles it. A head can
       look comfortable and still have a queue of approvals waiting behind it.`,
      'Approvals are invisible here',
    ),
    prose(
      `Three further rules decide what lands in the **Actual** column. A payment counts in the month
       it was paid in, not the month the request was raised. A payment that has been voided does not
       count at all. And a payment against a request marked as recoverable does not count either —
       money you expect back is a deposit rather than an expense.`,
    ),

    section('Reading a row'),
    prose(
      `Rows are grouped by project. The project line is a running total for the heads beneath it and
       can be collapsed; each head then carries its own figures.`,
    ),
    table(
      ['Column', 'What it holds'],
      [
        ['**Project** / **Head**', 'The budget line. The project row totals the heads under it.'],
        ['**Due**', 'A pill saying where the head stands against its due day: **Settled**, **Overdue**, **Due today**, **Upcoming**, or **No due day**. The small number under the pill is the due day itself.'],
        ['**Budget**', 'What was set for this head this month.'],
        ['**Actual**', 'What was paid against it this month.'],
        ['**Remaining ₹**', 'Budget minus actual. Green while there is money left, red once it is negative.'],
        ['**Remaining %**', 'The same figure as a share of the budget. It reads *N/A* where no budget was set.'],
        ['**Used**', 'A bar and a percentage: actual as a share of budget.'],
      ],
      'The columns of the grid, left to right. The screenshot is cut off at the right edge; two more columns follow.',
    ),
    prose(
      `Two columns sit beyond the right edge of the picture above. **Status** repeats the head's
       standing as a pill, and **Action** offers a control to record a payment — which the approver
       role does not carry, so for you that column is empty. The table ends with a **Company total**
       row.`,
    ),

    section('The five statuses, and the legend'),
    prose(
      `The row of swatches under the filters is a legend and a count in one: it names each status and
       says how many heads are in it this month.`,
    ),
    table(
      ['Status', 'What it means'],
      [
        ['**Not paid**', 'Nothing has been paid against this head this month, whatever its budget.'],
        ['**Unbudgeted**', 'Money has been paid against a head with no budget set for the month.'],
        ['**Under**', 'Spend is below budget.'],
        ['**On track**', 'Spend equals the budget exactly.'],
        ['**Over**', 'Spend has gone past the budget.'],
      ],
    ),
    note(
      `**On track** is narrower than it sounds — it is spend equal to budget to the rupee, not spend
       that is roughly on plan. Most healthy heads read **Under**.`,
    ),

    section('Filtering'),
    prose(
      `Three filters sit above the table, and **Apply** runs them together.`,
    ),
    table(
      ['Control', 'What it does'],
      [
        ['**Month**', 'Which month to show. The grid always shows one month at a time.'],
        ['**Status**', 'Narrows to one of *Unbudgeted spend*, *Over budget*, *Under budget*, *On track* or *Not paid*, or leaves it at *All*.'],
        ['**Search**', 'Matches on head or project name.'],
      ],
    ),
    prose(
      `A month an administrator has closed carries a **Locked** pill beside the project count, and a
       line saying who locked it and why. A locked month refuses new budget and payment changes; see
       [Monthly plans and locking](page:admin/monthly-plans-and-locking).`,
    ),

    section('Recent payments'),
    prose(
      `Under the grid sits **Recent Payments for** the month shown, listing the date, the project and
       head, the amount and the payee. Where a month has nothing, it says *No payments in this
       month.*

       The project-and-head cell links to the payment in detail — but the approver role holds no
       payment permission, so following that link returns the Forbidden screen. Read the figures
       here and go no further.`,
    ),
    tip(
      `Before approving against a head that is already **Over**, check whether the overspend is real
       or an accounting artefact — a payment booked to the wrong head shows up the same way as a
       genuine overspend. The recent payments list under the grid is the quickest way to tell.`,
    ),

    section('Exporting'),
    prose(
      `There is a CSV export of the grid, but it is gated on a separate permission that the approver
       role does not carry, so the control is not drawn for you. [Reports](page:manager/reports)
       covers the same figures at three levels of detail and is the screen to take numbers from.`,
    ),

    faq([
      {
        q: 'A head shows zero actual but I know I approved something for it.',
        a: 'Approval is not spend. The figure moves only when Accounts records the payment.',
      },
      {
        q: 'The Remaining % column says N/A.',
        a: 'No budget has been set for that head this month, so there is nothing to take a percentage of. An administrator sets budgets from [the budgets screen](page:admin/budgets).',
      },
      {
        q: 'A row is marked Unbudgeted. Is that an error?',
        a: `It means money was paid against a head with no budget for the month. It may be a missing
            budget line or a payment booked to the wrong head. Either way it is worth raising with an
            administrator.`,
      },
      {
        q: 'Why can I not open a payment from the recent payments list?',
        a: `The payments ledger is not part of the approver role. See
            [What an approver cannot see](page:manager/limits-of-the-role).`,
      },
    ]),
  ],
});
