import { page, prose, section, shot, table, note, warning, tip, faq } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'reports',
  title: 'Reports',
  summary: 'The monthly, by-project and by-head views, and what each answers.',
  blocks: [
    prose(
      `**Reports** in the sidebar is the same budget-against-actual comparison as
       [the variance grid](page:manager/the-variance-grid), rolled up three different ways and over
       any range of months you choose. Where the grid answers *how is this month going*, reports
       answer *how has this been going*.

       The line under the heading always names the range and the view you are looking at — *2026-07
       to 2026-07 · monthly view*.`,
    ),

    section('The three views'),
    prose(
      `**Monthly**, **Projects** and **Heads** are three tabs over one set of figures. Each takes the
       same date range and totals it at a different level, so the same money appears in all three
       and only the grouping changes.`,
    ),
    table(
      ['Tab', 'One row is', 'The question it answers'],
      [
        ['**Monthly**', 'One month, for the whole organisation', 'Are we spending against plan overall, and is it getting better or worse?'],
        ['**Projects**', 'One project, in one month', 'Which projects are running hot?'],
        ['**Heads**', 'One expense head within one project, in one month', 'Exactly which budget line is the problem?'],
      ],
    ),
    shot('manager/reports-monthly', 'The monthly view. One row per month, for the whole organisation.'),
    shot('manager/reports-projects', 'The projects view. The same money, grouped by project.'),
    shot('manager/reports-heads', 'The heads view. The same money again, down to the individual budget line.'),

    section('Setting the range'),
    prose(
      `**From** and **To** are month pickers, not dates — the smallest range is a single month.
       Select **Run** to apply them. **Current month** resets both to the month you are in.

       Ask for several months and the monthly view gives you one row each; the projects and heads
       views repeat their rows for every month in the range, with the month in the **Period**
       column.`,
    ),

    section('Reading the table'),
    table(
      ['Column', 'What it holds'],
      [
        ['**Period**', 'The month, written `2026-07`.'],
        ['**Project**', 'On the projects and heads views only.'],
        ['**Head**', 'On the heads view only.'],
        ['**Budget**', 'What was budgeted for that row in that month.'],
        ['**Actual**', 'What was paid.'],
        ['**Remaining**', 'Budget minus actual. Green while there is money left, red once it is negative.'],
        ['**Used**', 'Actual as a percentage of budget.'],
      ],
    ),
    prose(
      `The four cards above the table — **Budget**, **Actual**, **Remaining**, **Used** — total the
       whole range, whichever view you are on. They are the same figures the variance grid puts at
       the top of its month.`,
    ),
    note(
      `A row with an empty range says *No report rows for this range.* That means no budget was set
       and no payment was made in those months, not that the report failed.`,
    ),

    section('What the figures count'),
    warning(
      `**Actual** is money that has been paid. Nothing you approve appears in any of these three
       views until the accounts team settles it, and voided payments and recoverable spend are left
       out entirely. Reports are a record of what happened, not a forecast of what is committed.`,
      'These are payments, not approvals',
    ),
    prose(
      `Because the figures are built the same way as the grid's, a number here will always agree with
       the same number there for the same month. If they disagree, one of them is being read for a
       different month.`,
    ),

    section('Exporting'),
    prose(
      `**Export CSV** sits at the top right of every view. The download is gated on a separate export
       permission which the approver role does not carry, so selecting it returns the Forbidden
       screen rather than a file.

       If you need the figures as a spreadsheet, ask somebody in the accounts team or an
       administrator — both roles carry the export permission. The one export an approver can use is
       **Export CSV** on [the approvals queue](page:manager/the-approvals-queue), which downloads the
       requests routed to you.`,
    ),
    tip(
      `Run **Heads** across the last three or four months before approving a large request. A head
       that has been over budget every month is a different conversation from one that has drifted
       once.`,
    ),

    faq([
      {
        q: 'The totals at the top do not match the rows I can see.',
        a: `They total the whole range, not the visible page. On the heads view a wide range produces
            a great many rows.`,
      },
      {
        q: 'A project appears with a budget and no spend at all.',
        a: 'Nothing was paid against it in that month. Approved but unpaid requests do not appear in any report.',
      },
      {
        q: 'Can I report on one project only?',
        a: `Not from this screen — the only filter is the date range. Use the **Search** box on
            [the variance grid](page:manager/the-variance-grid) to narrow to one project or head
            within a month.`,
      },
      {
        q: 'Export CSV gave me a Forbidden page.',
        a: `That is the permission, not a fault. See
            [What an approver cannot see](page:manager/limits-of-the-role).`,
      },
    ]),
  ],
});
