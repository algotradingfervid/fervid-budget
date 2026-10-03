import { page, prose, section, shot, steps } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'reports',
  title: 'Reports',
  summary: 'Compare budgets and actual payments, then inspect the records behind the figures.',
  blocks: [
    section(`Choose a view and range`),

    prose(`**Reports** in the sidebar opens the **Monthly** tab. **Monthly** has one row per month,
       **Projects** one project per month, and **Heads** one expense head per month. Set **From** and
       **To**, then **Run**; **Current month** jumps to this month. The heading and any Showing context
       identify the exact date and project/head scope of the displayed totals.`),

    shot('manager/reports-monthly', 'The monthly view. One row per month, for the whole organisation.'),
    shot('manager/reports-projects', 'The projects view. The same money, grouped by project.'),
    shot('manager/reports-heads', 'The heads view. The same money again, down to the individual budget line.'),

    section(`Read and follow the figures`),

    prose(`Budget is planned spend; Actual is active budget payments, including recorded installments.
       Remaining is Budget minus Actual, and Used is Actual as a percentage of Budget. Recoverable
       deposits and voided payments are excluded. Approval alone is not an actual payment.`),

    steps([
      { text: `Use **View projects** on a monthly row to inspect that month.` },
      { text: `Use **View heads** on a project row to inspect its heads.` },
      { text: `Use **View budget and payments** on a head to narrow the report to that head.` },
      { text: `Use **Back to source report** or the context back link to restore the exact prior report range and filters.` },
    ]),

    prose(`Once you have narrowed the report to one head, the trail stops at the figures. Colleagues
       with budget access see a **View budget** link on each row, and colleagues with payment access
       see a **Payments behind these actuals** list of the records under the total. The approver
       role holds neither permission, so for you the **Details** cell on a single head is empty and
       no payment list appears. Ask the accounts team if you need to see which payments make up a
       figure.`),

    section(`Retaining context`),

    prose(`Changing From/To within a head or project drilldown retains that selection. The
       source-report link retains the original range. Current month changes the date range while
       retaining the selected project/head and the source-report back link. A selected period with
       no activity still shows its context and zero figures. The monthly tab returns to the broader
       monthly comparison.`),

    section(`Exporting`),

    prose(`The approver role cannot export reports, so there is no export button at the top of the
       screen for you. Colleagues who hold the export permission see **Export CSV** there (or
       **Export full … report CSV** on a drilldown, because the export covers the full grouping for
       the selected dates). Ask one of them — usually the accounts team — for a spreadsheet.`),
  ],
});
