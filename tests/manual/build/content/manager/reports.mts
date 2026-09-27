import { page, prose, section, shot, steps } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'reports',
  title: 'Reports',
  summary: 'Compare budgets and actual payments, then inspect the records behind the figures.',
  blocks: [
    section(`Choose a view and range`),

    prose(`**Monthly** has one row per month, **Projects** one project per month, and **Heads** one
       expense head per month. Set From and To, then Run. The heading and any Showing context
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
      { text: `Use **View budget and payments** on a head to see its period and supporting records.` },
      { text: `Use **View budget** or **Open payment** where your permissions allow.` },
      { text: `Use **Back to source report** or the context back link to restore the exact prior report range and filters.` },
    ]),

    prose(`Underlying payment records need separate payment access. The payment section explicitly
       states its limit of the latest 300 records per month and your data scope; a restricted user
       can see fewer payments than the overall report total.`),

    section(`Retaining context`),

    prose(`Changing From/To within a head or project drilldown retains that selection. The
       source-report link retains the original range. Current month changes the date range while
       retaining the selected project/head and the source-report back link. A selected period with
       no activity still shows its context and zero figures. The monthly tab returns to the broader
       monthly comparison.`),

    section(`Exporting`),

    prose(`CSV export appears only with export permission. A scoped drilldown labels its download
       **Export full … report CSV** because the export covers the full grouping for the selected
       dates, not just the selected project or head. Ask an authorised colleague for exports you
       cannot access.`),
  ],
});
