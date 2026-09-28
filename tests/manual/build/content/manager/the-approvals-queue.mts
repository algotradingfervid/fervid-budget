import { page, prose, section, shot, table, note, tip, faq } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'the-approvals-queue',
  title: 'The approvals queue',
  summary: 'Everything waiting on you, split by the kind of decision it needs.',
  blocks: [
    prose(
      `**Approvals** in the sidebar opens your queue. The number beside it counts the requests with
       your name on them that are waiting at \`pending\` or in partial review, so the sidebar
       tells you whether there is anything to do without your opening the screen.

       The queue is headed *Waiting on you*, and the line under the heading counts three kinds of
       work separately — how many there are to approve, how many cancellations there are to decide,
       and how many partial payments there are to review.`,
    ),
    shot('manager/approvals-to-approve', 'The queue on its first tab. Each row is one request, urgent ones first.'),

    section('The four tabs'),
    prose(
      `The tabs divide the queue by what has already happened to the request, not by project or
       amount. The count beside each tab is live and honours whatever you have typed into the search
       box.`,
    ),
    table(
      ['Tab', 'What it holds', 'The statuses behind it'],
      [
        ['**To approve**', 'Requests waiting on your decision. This is where the work is.', '`pending`'],
        ['**Cancellations**', 'Approved requests whose raiser has asked you to cancel them.', '`cancellation_requested`'],
        ['**Partial review**', 'Requests Accounts paid less than you approved, waiting for your decision on the balance.', '`partial_review`'],
        ['**Decided**', 'The record of what you have already settled one way or the other.', '`approved`, `rejected`, `cancelled`'],
      ],
    ),
    shot('manager/approvals-cancellations', 'The Cancellations tab. Each row is somebody asking you to undo an approval.'),
    shot('manager/approvals-decided', 'The Decided tab: what you approved, what you rejected, what was cancelled.'),
    note(
      `**Decided** is narrower than it sounds. A request you *returned* is not in it, because a
       returned request is back with its requester and may yet come round again. Nor are requests
       that have since been paid and closed. For those, use **My requests**, which reaches every
       request in the product.`,
    ),

    section('Partial reviews'),
    prose(
      `When Accounts pays less than you approved, the request lands on the **Partial review** tab.
       Selecting the row opens the request; the control there reads **Decide the partial payment**.
       You can also follow the notification the product sends you.
       [Reviewing a shortfall](page:manager/shortfall-review) covers the decision.`,
    ),

    section('Only your rows'),
    prose(
      `The queue is scoped to you by name — it lists requests whose approver is you, and nothing
       else. This is not a matter of how much of the product your account can otherwise see: an
       administrator with the run of every screen still sees only their own rows on this one.

       If a colleague says a request is waiting and it is not in your queue, it was routed to
       somebody else. Open it from **My requests** and read the **Approver** field.`,
    ),

    section('Reading a row'),
    prose(
      `The queue is a list of cards rather than a table, so each row carries the same handful of
       facts rather than a grid of columns.`,
    ),
    table(
      ['On the card', 'What it is'],
      [
        ['`PR-2026-000287`', 'The request number. It never changes, whatever happens to the request.'],
        ['The amount on the right', 'The amount that was *asked for*. Once you approve a smaller figure the card still shows what was requested.'],
        ['The bold line', 'The short title the requester gave it.'],
        ['**! Urgent**', 'Present only when the requester flagged it. Urgent rows sort to the top.'],
        ['**Recoverable**', 'Present only when the money is expected back, followed by the category where there is one.'],
        ['The grey line', 'The request type, then project and head, then the invoice number where there is one.'],
        ['The status pill', 'Where the request stands: **Awaiting approval**, **Cancellation requested**, **Partial — manager review** (or **Partial — under discussion** once you have raised a concern), **Approved — awaiting payment**, **Rejected — final** or **Cancelled**. An approved request Accounts has put on hold still shows **Approved — awaiting payment** here.'],
        ['The line on the right', 'Who is holding it up. On the first three tabs that reads **Waiting on you** (or *Waiting on you and Accounts* while a concern you raised is unanswered); on **Decided** it names whoever acts next.'],
      ],
    ),
    prose(
      `Within a tab, urgent requests come first and then the oldest, so the row at the top is the one
       that has been waiting longest. Select any row to open the request in full.`,
    ),

    section('Searching and exporting'),
    prose(
      `The **Search** box matches the request number, the payee, the invoice number, the purpose and
       the requester's name. Type and select **Apply**; the tab counts change with it, so you can see
       at a glance how many of each kind match.

       **Export CSV** at the top right downloads a spreadsheet of the open requests routed to you —
       every open status, not only the tab you are on — narrowed by your search. It also includes
       any open requests you raised yourself. The columns are Number, Status, Type, Title, Amount,
       Approved amount, Payee, Requester, Approver and Created.`,
    ),
    note(
      `The list draws at most 200 rows, while the counts beside the tabs are not capped. If a tab
       claims more than the page shows, narrow it with the search box.`,
    ),

    section('When there is nothing to do'),
    prose(
      `An empty tab says *Nothing is waiting on you here.* — which is the whole message. Check the
       other tabs before you conclude your day is clear.`,
    ),
    tip(
      `The foot of the queue reads *Every approval is a decision made after opening the request.
       There is no bulk approval, by design.* There is no tick-box column and no **Approve
       selected** button to look for; every decision starts by opening the request.`,
    ),

    faq([
      {
        q: 'The sidebar badge says 19 but the To approve tab says 14. Which is right?',
        a: `Both. The sidebar badge adds the **To approve** and **Partial review** tabs together.
            Cancellations are not in the badge; they are counted on the line under the heading and
            on their own tab.`,
      },
      {
        q: 'A request I approved has vanished from Decided.',
        a: `It has not been removed, but the tab only holds \`approved\`, \`rejected\` and
            \`cancelled\`. Once Accounts pays it the status moves on to \`completed\`, and it is then
            found through **My requests**.`,
      },
      {
        q: 'Can I approve several requests at once?',
        a: 'No. Each decision is taken on the request itself, after you have read it.',
      },
    ]),
  ],
});
