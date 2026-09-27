import { page, prose, section, shot, steps, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'budgets',
  title: 'Budgets',
  summary: 'Build a monthly plan from projects, expense heads, and budget lines, then review and save it.',
  blocks: [
    prose(
      'A monthly budget groups planned spending by project and expense head. Each head can contain ' +
      'several named budget lines: for example, a Travel head can contain train fares, accommodation, ' +
      'and local transport. Line amounts add up to the head total, heads add up to their project, ' +
      'and projects add up to the monthly budget. There is no separate annual budget to enter.',
    ),
    note(
      'Budget lines explain the plan. Payments and actual spending are recorded against expense ' +
      'heads, not against individual budget lines. You can compare the head total with its actual ' +
      'spending, but the application does not allocate that spending among the lines.',
      'Planning detail and actual spending',
    ),

    section('Create a monthly budget'),
    steps([
      {
        text: 'Select **Create budget** on Budgets or [Monthly plans](page:admin/monthly-plans-and-locking).',
        note: 'Creating a month requires permission to create monthly plans and edit budgets. Creating a new project or expense head also requires the corresponding permission; otherwise choose an existing item.',
      },
      {
        text: 'In **1. Choose month**, set **Budget month** and choose your starting point.',
        note: '**Start with a blank budget** starts with no projects, heads, or lines, even if the application already has projects. **Copy an existing budget** lets you choose a saved month in **Copy from**. Copying brings its active projects, heads, budget lines, and amounts into the draft. Payments are not copied, and retired projects and heads are excluded.',
      },
      {
        text: 'Select **Continue to amounts →**, then **＋ Add project**.',
        note: 'Choose an existing project, or choose **＋ Create a new project** and enter its name. Adding a project does not automatically add all its heads. Expand the project and select **＋ Add expense head** to choose or create the heads you need.',
      },
      {
        text: 'Expand an expense head and select **＋ Add line** for each part of its budget.',
        note: 'Give every line a description and an amount in rupees, such as 1250.50. Enter zero or a positive number with up to two decimal places, without a rupee symbol or commas. Blank amounts, negative numbers, and extra decimal places are refused. **Remove** removes a line from the draft.',
      },
      {
        text: 'Select **Review budget →** and check the month, starting point, lines, and totals.',
        note: 'The review also lists any new projects and heads that will be created. Expand groups to inspect their lines. Use **← Edit amounts** to make corrections.',
      },
      {
        text: 'Select **Create budget** to save the reviewed plan.',
        note: 'The month, new projects and heads, budget lines, and totals are saved together. The saved plan opens with a confirmation. Nothing in the draft creates a project, head, or monthly budget before this final submission.',
      },
    ]),
    note(
      'Projects and expense heads start collapsed, with their totals visible. Expand just the item ' +
      'you need, or use **Expand all** and **Collapse all**. Adding a new project or head leaves it ' +
      'collapsed too. Amount edits update the head, project, and monthly totals immediately in the draft.',
      'Work with a short outline',
    ),
    warning(
      'Drafts are not auto-saved. Finish with **Create budget** or **Save budget changes**. Leaving ' +
      'a changed draft prompts a warning, but the draft is not a saved budget. Changing the starting ' +
      'point asks before discarding the unsaved project, head, and line changes.',
      'Save before leaving',
    ),
    note(
      'The guided planner saves the whole reviewed plan in one transaction. If validation or saving ' +
      'fails, it does not save only some of the lines or leave newly created projects behind. Correct ' +
      'the reported problem and review again. At least one project, one head, and one described line ' +
      'are required; a blank starting point is an empty draft, not a completed budget.',
      'One complete save',
    ),

    section('Change an existing budget'),
    steps([
      { text: 'Open the month on **Budgets** and select **Edit budget lines**.' },
      {
        text: 'Expand the relevant project and head, then adjust descriptions or amounts, add lines, or remove lines.',
        note: 'You can add more projects and heads too. The month of an existing plan is fixed. Unsaved projects and heads can be removed from the draft; populated groups ask for confirmation. This does not delete master records. Already-saved groups cannot be removed; retain a described zero-value line if an existing head no longer needs an allocation.',
      },
      {
        text: 'Select **Review budget →**, inspect the changes, then **Save budget changes**.',
        note: 'If another editor changed the plan after you opened it, saving is refused so their work is not overwritten. Note your intended changes, reopen the latest saved plan, and apply them to that version.',
      },
    ]),
    tip(
      'For a similar next month, use **Create budget** and **Copy an existing budget**. Change the ' +
      'copied lines before saving. The source month stays unchanged, and only the target month gets ' +
      'the revised plan. A locked source month can still be read and copied into a different month.',
    ),

    section('Budget overview'),
    shot('admin/budgets', 'The budget overview: one aggregate row per expense head. This earlier capture does not show the guided planner.'),
    prose(
      'The overview remains useful for comparing each head’s total budget with actual spending. ' +
      '**Open** loads the selected month; **Month history** opens monthly plans, and **View grid** ' +
      'opens its variance grid. Heads with saved budget lines show a read-only total and an **Edit ' +
      'lines** link to the relevant head in the planner. Older budgets without a line breakdown can ' +
      'still be edited as aggregate amounts in this overview with **Save Budgets**. Opening an older ' +
      'budget in the planner presents its amount as a **Monthly allocation** line.',
    ),
    table(
      ['Column', 'What it shows'],
      [
        ['Project / Head', 'The project and expense head for the row.'],
        ['Status', 'Active, or Retired for an item retained for history.'],
        ['Budget', 'The head’s total planned amount. Detailed budgets are changed through Edit lines.'],
        ['Actual', 'Actual spending recorded against the head for the selected month.'],
        ['Used', 'Actual as a percentage of budget. Positive actual spending against a zero budget is labelled No budget.'],
      ],
    ),

    section('Limits on changes'),
    table(
      ['Situation', 'What happens'],
      [
        ['The month is locked', 'The saved plan can be expanded and read, but its amounts and structure cannot be changed. An authorized user must unlock it first.'],
        ['A project or head is retired', 'Its saved lines remain visible and read-only in the existing month. They are excluded when copying to a new month.'],
        ['The target month already exists', 'Create is refused. Open that month and edit its existing budget instead.'],
        ['The user cannot create projects or heads', 'Existing items can be chosen when available; the corresponding new-item option is not offered.'],
      ],
    ),
    warning(
      'Unlocking a month to change a budget reopens it for payments as well, and requires a recorded ' +
      'reason. See [Monthly plans and locking](page:admin/monthly-plans-and-locking) before reopening ' +
      'a closed period.',
      'Editing a closed month',
    ),

    section('How budgets are used'),
    prose(
      'Budgets show planned versus actual spending; they do not block requests or payments just ' +
      'because an amount is over budget. The variance grid and reports compare actual spending with ' +
      'the summed head budgets, and monthly plans show budget, actual, and remaining amounts for the ' +
      'whole month. Overspending can show more than 100% used and a negative remaining amount.',
    ),
    faq([
      {
        q: 'Why is a blank budget empty when projects already exist?',
        a: 'Blank means start from scratch. Use **＋ Add project** and then **＋ Add expense head** to include only the existing or new items you need.',
      },
      {
        q: 'Can I split a head into several budget lines?',
        a: 'Yes. Expand the head and use **＋ Add line** repeatedly. Each line has its own description and amount, and the head total is their sum.',
      },
      {
        q: 'Why is a copied total smaller than the source total?',
        a: 'Retired projects and heads are excluded. The source preview names the original month’s total; check the copied draft’s total and review before saving.',
      },
      {
        q: 'Do the live totals mean my changes have been saved?',
        a: 'No. They calculate the draft. Review and select **Create budget** or **Save budget changes** to persist the plan.',
      },
    ]),
  ],
});
