import { page, prose, section, shot, steps, note, warning, tip, fields, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'projects',
  title: 'Projects',
  summary: 'The top level of the budget structure, and what happens when you retire one.',
  blocks: [
    prose(
      'The budget has three levels, and a project is the first of them: projects hold expense heads, ' +
      'and heads hold a budget figure for each month. Everything downstream depends on this list — ' +
      'the variance grid groups by project, the reports break down by project, and every payment is ' +
      'booked against a head that belongs to one.\n\n' +
      'The dataset behind these captures has 24 projects and 229 heads.',
    ),

    section('The screen'),
    shot('admin/projects', 'The projects list. Every row is editable in place, and the form at the top adds a new one.'),
    prose(
      'There is no separate detail page. The name and the sort order are text boxes on the row itself, ' +
      'the status is a tick box, and each row has its own **Save**. Change one row and save it; the ' +
      'others are untouched.',
    ),

    section('Adding a project'),
    steps([
      { text: 'Type a **Project name** in the form at the top.' },
      {
        text: 'Set **Order** if you want the project to sit somewhere particular in the lists.',
        note: 'It is a plain number and it sorts ascending. Rows sharing a number fall back to alphabetical order, which is why the seeded projects all carry `0` and read alphabetically.',
      },
      { text: 'Leave **Active** ticked.' },
      {
        text: 'Select **Add Project**.',
        note: 'The project appears in the table below and is written to the audit trail. It has no expense heads yet, so nothing can be booked against it.',
      },
    ]),
    fields([
      { name: 'Project name', required: true, note: 'Required. An empty name is refused with `project name is required`.' },
      { name: 'Order', required: false, note: 'Where the project sits in every list that shows projects. Lower first.' },
      { name: 'Active', required: false, note: 'Ticked means the project can be used. See below for what unticking does.' },
    ]),
    tip(
      'Add the project first and its heads second. A head has to name a project, and the ' +
      '[Expense heads](page:admin/expense-heads) form only offers active ones.',
    ),

    section('Renaming a project'),
    steps([
      { text: 'Type over the name in the row.' },
      { text: 'Select **Save** on that row.' },
    ]),
    prose(
      'Renaming is safe. The project keeps its identity, so every head, budget and payment already ' +
      'filed under it stays where it is, and reads under the new name from that moment on.',
    ),

    section('Retiring a project'),
    prose(
      'There is no delete. A project that is no longer used is retired by unticking **Active** and ' +
      'saving the row, and the history stays intact.',
    ),
    steps([
      { text: 'Untick **Active** on the row.' },
      { text: 'Select **Save**.' },
    ]),
    prose('Retiring a project has four consequences worth knowing before you do it.'),
    fields([
      {
        name: 'New heads',
        required: false,
        note: 'The **Add Head** form on the heads screen lists active projects only, so no new head can be filed under a retired project.',
      },
      {
        name: 'Existing heads',
        required: false,
        note: 'They stay, and they still appear on the heads screen. Their project select shows the retired project marked `(retired)` so the row can be saved without silently moving.',
      },
      {
        name: 'New payments',
        required: false,
        note: 'A payment against a head whose project is retired is refused with `This project/head is inactive.` The check is on both the head and its project.',
      },
      {
        name: 'History',
        required: false,
        note: 'Untouched. Every payment already recorded keeps counting towards the actuals it always did, and the reports still show it.',
      },
    ]),
    warning(
      'Retiring a project stops money being booked against it, but it does not close anything that is ' +
      'already in flight. A request approved against one of its heads will be refused at the payment ' +
      'stage, and the accountant will find out only when they try to record it. Check the queue before ' +
      'you retire a project mid-month.',
      'Work already in flight',
    ),
    note(
      'A retired project cannot be brought back through some separate route — tick **Active** again ' +
      'and save. Nothing was thrown away.',
    ),

    faq([
      {
        q: 'Can I delete a project?',
        a: 'No. Untick **Active** instead. Deleting would orphan every payment ever booked under it.',
      },
      {
        q: 'The order numbers are all zero and nothing moves.',
        a: 'Rows that share a number sort alphabetically. Give the ones you want at the top a `1`, `2`, `3` and leave the rest at `0`.',
      },
      {
        q: 'I renamed a project and the grid still shows the old name.',
        a: 'Reload the screen. Nothing is cached beyond the page you are looking at.',
      },
      {
        q: 'Where do I set the budget for a project?',
        a: 'You do not. Budgets sit on heads, not on projects — see [Expense heads](page:admin/expense-heads) and then [Budgets](page:admin/budgets). The project total on the grid is the sum of its heads.',
      },
    ]),
  ],
});
