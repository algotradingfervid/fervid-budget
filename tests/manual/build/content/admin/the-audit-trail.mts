import { page, prose, section, subsection, shot, steps, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'the-audit-trail',
  title: 'The audit trail',
  summary: 'Who did what, when — the record that cannot be edited.',
  blocks: [
    prose(`Every act that changes a financial record leaves a row here, with the person who did it, the
       time they did it, and — where there is one — the before and the after. The screen’s own
       sub-line puts it plainly: review who changed financial records, when, and why.

       It is a record and not a workspace. There is nothing on this screen that changes anything,
       because the value of the trail is that it cannot be tidied up afterwards.`),

    section(`The screen`),

    shot('admin/audit', `The audit trail, newest first. Entries with a before-and-after carry an expander.`),

    table(
      [`Column`, `What it holds`],
      [
        [`When`, `The date and time of the act, to the minute.`],
        [`Actor`, `Who did it, by name.`],
        [`Entity`, `What kind of thing was touched — \`User\`, \`Month Lock\`, \`Payment\`, \`Vendor\` and so on.`],
        [`Action`, `What was done to it, as a coloured pill.`],
        [`Summary`, `A sentence describing the act, and a **Before / after** expander where there is stored detail. A role row lists the permission changes instead (see below).`],
      ],
    ),

    prose(`The capture above shows two month-lock entries in among the sign-ins: "Locked 2026-03: Month
       closed for reporting." and "Unlocked 2026-03: Reopened after capturing the manual." The text
       after each colon is the reason somebody typed at the time. That is the whole argument for
       requiring a reason on a month lock — a year later it is the only thing that explains the
       row.

       A change to a role's permissions is shown differently. Instead of a **Before / after**
       expander, the Summary lists the individual permissions and data scopes that were added or
       removed, by name rather than as a count, and — if you can view roles — an **Open affected
       role** link. Use the actor and time to identify who granted or removed access.`),

    subsection(`Before and after`),

    prose(`Where the product stored the record’s state, the Summary carries a **Before / after**
       disclosure. Open it and you get the stored values as they were and as they became. Not every
       action has one — a sign-in has nothing to compare — so the expander appears only on rows
       that can fill it.`),

    section(`Finding something`),

    steps([
      { text: `Choose an **Entity** if you know what kind of record you are after.` },
      { text: `Choose an **Action** if you know what was done to it.` },
      { text: `Type a name into **Actor** to narrow to one person. It matches on any part of the name.` },
      { text: `Pick a **From** date, a **To** date, or both, to narrow to a period. Both days are included in full.` },
      { text: `Select **Filter**.` },
      { text: `Select **Reset** to clear everything and start again.` },
    ]),

    subsection(`The entities you can filter by`),

    prose(`The list reads exactly as the screen writes it: \`Payment Request\`, \`Payment\`, \`Budget\`,
       \`Budget Month\`, \`Month Lock\`, \`Project\`, \`Head\`, \`Vendor\`, \`Recoverable Category\`,
       \`Recoverable Report\`, \`User\`, \`Role\`, \`Notification Setting\`, \`App Setting\`,
       \`Report\`, \`Backup\`.`),

    subsection(`The actions you can filter by`),

    prose(`The list is ordered so the request workflow reads as a workflow rather than as an alphabet,
       and each action appears under the same label as its pill in the Action column:
       \`Created\`, \`Updated\`, \`Deleted\`, \`Attached\`, \`Commented\`, then \`Submitted\`,
       \`Approved\`, \`Returned\`, \`Rejected\`, \`Withdrawn\`, \`Raised again\`, then
       \`Cancellation asked\`, \`Cancelled\`, \`Approval reassigned\`, then \`Reserved\`,
       \`Released\`, \`Reservation reassigned\`, \`Put on hold\`, \`Taken off hold\`, then
       \`Settled\`, \`Marked partial\`, \`Partial accepted\`, \`Concern raised\`, \`Reminder sent\`,
       \`Recovery recorded\`, then \`Voided\`, \`Locked\`, \`Unlocked\`, \`Settings saved\`,
       \`Exported\`, and finally \`Logged in\`, \`Logged out\`, \`Login failed\`.`),

    tip(`Two filters are usually enough. "Everything Fervid Admin did to a Month Lock" is Entity plus
       Actor, and it answers most of the questions people actually bring to this screen. Add a
       **From** and **To** date when you know roughly when it happened.`),

    section(`What the trail cannot do`),

    warning(`Nothing on this screen edits or removes a row, and there is no route anywhere in the product
       that does. An entry that is wrong stays; the correction is a new act, which leaves its own
       entry beside it. That is what makes the trail worth reading.`, `The record cannot be edited`),

    note(`The screen shows 100 entries at a time, newest first. When there are more, a line under the
       table says which ones you are looking at — for example \`Showing 1–100 of 2345\` — with **Older →** to go further back and
       **← Newer** to come forward again. The paging keeps every filter you set, so moving between
       pages never widens the search. The filters work over the whole trail, not only the page you
       are on.`, `One hundred rows to a page`),

    note(`If your role cannot see every request and every payment, rows about the requests and
       payments outside your reach are left out of the list altogether, and the count under the
       table counts only what you can see. Rows about budgets, users, settings and the rest are
       shown to everybody who can open the screen.`, `Rows you are not allowed to see`),

    warning(`The trail carries the summaries and stored values of financial records, so treat it as
       sensitive. Vendor bank details are the one thing deliberately kept out of it: a vendor save
       records the vendor's profile — name, type, status, GSTIN, PAN, contact details, address and
       so on — and for the bank block only a flag saying whether it was touched. See [Vendor bank details](page:admin/vendor-bank-details).`, `Who should be able to read it`),

    section(`What lands in it`),

    prose(`Not everything the product does, but everything that changes a record somebody may later
       have to account for. Among them: creating and editing users and roles, saving configuration,
       saving a budget, opening a month, locking and unlocking a month, creating and updating
       projects, heads and vendors, saving and deleting recoverable categories, recording, editing
       and voiding payments, every decision on a request, taking a backup, and signing in and out.`),

    note(`Some acts are recorded in more than one place, and deliberately so. A month lock writes a
       row here and shows its reason on the variance grid; a payment void writes a row here and
       keeps its reason on the payment. The trail is the copy that survives when a record is
       changed again.`),

    faq([
      { q: `Can I export the audit trail?`, a: `There is no export control on this screen. The filters are how you narrow it, and the browser is how you read it.` },
      { q: `Somebody says they did not do something, but their name is on a row.`, a: `The name is the account that was signed in. If it is genuinely not them, that is a password question — reset it from [People and accounts](page:admin/people) and look at the \`Logged in\` and \`Login failed\` rows around the time.` },
      { q: `A change I made is not here.`, a: `Check the filters first — including **From** and **To** — then select **Older →** in case it is on a later page. If it is genuinely absent, the act may not be one the product records — not every read or navigation leaves a row, only changes do.` },
      { q: `Why does one row have Before / after and the next one does not?`, a: `Because only some acts have a stored state worth comparing. A sign-in has nothing to compare; a budget save has an old figure and a new one. Role rows list their permission changes instead.` },
      { q: `Can I stop somebody seeing this screen?`, a: `Yes. It is gated on the \`Audit · view\` permission, which sits behind the **View** cell of the **Administration** row in [Roles and permissions](page:admin/roles-and-permissions). Open that row's **Advanced** list to take it away on its own.` },
    ]),
  ],
});
