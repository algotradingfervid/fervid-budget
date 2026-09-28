import { page, prose, section, shot, table, note, warning, tip, faq } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'reviewing-a-request',
  title: 'Reviewing a request',
  summary: 'What to check before you decide, and where to find it on the screen.',
  blocks: [
    prose(
      `Selecting a row in [the approvals queue](page:manager/the-approvals-queue) opens the request
       in full. It is the same screen everybody else sees — the requester, the accounts team, an
       administrator — with one difference: the bar of decisions along the bottom, which is drawn
       only for you.`,
    ),
    shot('manager/request-pending', 'A request awaiting your decision, with the action bar at the foot.'),

    section('The head of the page'),
    prose(
      `The top block carries the number, the amount asked for, the short title, and a line naming the
       request type, the project and head it is booked to, and who raised it and when. Under that sit
       two labels: the status pill, and a line saying who is holding the request up. On a request of
       yours that reads **Waiting on you**.`,
    ),

    section('The Details block'),
    prose(
      `Every field the requester filled in is here, laid out in two columns. Which fields appear
       depends on the request type — an invoice number and date for a vendor invoice, an expense
       date for a reimbursement, a reason for an advance. The chip at the top right of the block
       says whether the money is ordinary spend — **Budget expense** — or is expected back, when it
       reads **Recoverable** with the category.`,
    ),
    table(
      ['Field', 'What to look at'],
      [
        ['**Amount**', 'What is being asked for. You may approve this or less, never more.'],
        ['**Needed by**', 'The date the money is wanted. It also decides which month the spend lands in.'],
        ['**Project** and **Head**', 'The budget line this will be booked against. Check it against the grid before a large approval.'],
        ['**Vendor** or **Paid to**', 'Who will receive the money. The label reads **Vendor** when a vendor from the master was chosen, and **Paid to** otherwise.'],
        ['**Invoice number** and **Invoice date**', 'Shown on a vendor invoice. Check them against the document.'],
        ['**Expense date**', 'Shown on a reimbursement: when the cost was incurred, which is not always the month it will be paid in.'],
        ['**Requested by**', 'Who raised it. It is never you — the product refuses a self-approval.'],
        ['**Approver**', 'Whose decision it is. If this is not your name, no action bar is drawn.'],
        ['**Urgency**', 'Either **Normal**, or **Urgent** followed by the reason the requester gave.'],
        ['**Purpose**', 'The requester’s own words about what the money is for.'],
      ],
    ),

    section('Attachments'),
    prose(
      `The Attachments section lists every document on the request with its name, its size and when
       it was added, and the count sits beside the heading. Where there is nothing, it reads *No
       documents attached.*

       Select **Download** beside a document to open it. Read the invoice before you approve
       against it: the amount, the payee and the date on the paper should match the request.

       If the requester sent the request without a document, a banner headed **No document was
       attached** sits above the Details block with the reason they gave.`,
    ),

    section('History and conversation'),
    prose(
      `The thread at the foot is the request's whole life in order: who submitted it, who approved,
       returned or rejected it and with what wording, every hold, every payment, and every comment
       anybody has left. The note beside the heading is worth taking seriously — *Everyone who can
       see this request sees this whole stream.*

       Use **Add a comment** and then **Post comment** to ask the requester something before you
       decide. A question in the thread is usually quicker than returning the request, because
       returning it takes it out of your queue.`,
    ),

    section('What to check before deciding'),
    prose(
      `There is no checklist built into the screen, so this is the job rather than the product:`,
    ),
    table(
      ['Question', 'Where the answer is'],
      [
        ['Is the amount right?', 'The **Amount** field, and the invoice number on the queue card.'],
        ['Is it booked to the right budget line?', '**Project** and **Head**, checked against [the variance grid](page:manager/the-variance-grid) for that month.'],
        ['Is there room in the budget?', 'The grid row for that head — its **Remaining ₹** and **Used** figures.'],
        ['Is the paperwork there, and does it match?', 'The Attachments section. Download the document and compare it with the **Amount**, payee and invoice fields.'],
        ['Has this been asked for before?', 'The request number and invoice number, searched from the queue.'],
        ['Is anything unclear?', 'Ask in the thread rather than guessing.'],
      ],
    ),
    tip(
      `The grid counts money that has actually been paid, not money you have approved. A head can
       look healthy in the grid and still have several large approvals queued behind it, so treat a
       comfortable-looking budget line with care when you have been approving against it all month.`,
    ),

    section('Two refusals worth knowing about'),
    warning(
      `If the month the request is needed by has been locked, approving it is refused. The message
       names the month and offers the two ways out — *Reopen the month, or ask for the required-by
       date to be changed.* Only an administrator can reopen a month —
       [Monthly plans and locking](page:admin/monthly-plans-and-locking) explains it.`,
      'A locked month',
    ),
    note(
      `You cannot approve a request you raised yourself, and you cannot decide a request whose
       approver is somebody else. Both are refused outright rather than quietly ignored — and on a
       request routed to somebody else the decision controls are not drawn in the first place.`,
    ),

    section('Deciding'),
    prose(
      `When you are ready, the four controls along the bottom are, from left to right, **Reject**,
       **Return for correction**, **Approve ₹…** and **Reassign approval**. Each opens a panel and
       none of them writes anything until you press the button inside that panel. They are covered
       one at a time in [Approving a request](page:manager/approving),
       [Returning a request for correction](page:manager/returning-for-correction),
       [Rejecting a request](page:manager/rejecting) and
       [Reassigning an approval](page:manager/reassigning).`,
    ),

    faq([
      {
        q: 'The request mentions an invoice but there is nothing to download.',
        a: `Then no document was attached — the Attachments count reads 0 and the section says *No
            documents attached.* Ask for it in the conversation, or
            [return the request](page:manager/returning-for-correction) and say what you need.`,
      },
      {
        q: 'I want to check the vendor before approving.',
        a: `The vendor master is not open to the approver role. The payee name is on the request; for
            anything more, ask an administrator, who reaches
            [the vendor screens](page:admin/vendors).`,
      },
      {
        q: 'Can I change the project or head before approving?',
        a: `No. An approver changes only the amount. If the wrong head has been used, return the
            request and say so — the requester can correct it and send it again.`,
      },
    ]),
  ],
});
