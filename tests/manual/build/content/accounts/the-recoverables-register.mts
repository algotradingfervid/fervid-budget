import { page, prose, section, subsection, shot, note, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'the-recoverables-register',
  title: 'The recoverables register',
  summary: 'Tracking money that is expected back, by category and by counterparty.',
  blocks: [
    prose(
      `Some money that leaves the organisation is not spent. A security deposit, an advance to a
       member of staff, a payment against something that will be returned — the cash has gone, but
       it is expected back. A requester marks a request that way when they raise it, and every one
       of them lands here.`,
    ),
    prose(
      `The register is the sidebar item **Recoverables**. Its whole reason for existing is stated
       in the banner across the top of it: a deposit is not an expense, so none of this appears in
       the variance grid or in project spend.`,
    ),

    section('The summary'),
    shot('accounts/recoverables', 'The register. Four tiles, a rollup by category, a rollup by counterparty, and a plain statement of what this version does not do.'),
    table(
      ['Tile', 'What it counts'],
      [
        ['**Outstanding**', 'Everything still expected back, and how many payments that is'],
        ['**Past expected return**', 'The part of it that is already overdue'],
        ['**Due in 30 days**', 'What falls due within the month ahead'],
        ['**Paid out this month**', 'Recoverable money that left this month'],
      ],
    ),
    prose(
      `Under the tiles, two rollups of the same set. **By category** groups by the recoverable
       category the requester chose, with a count, the outstanding total, the overdue part and the
       oldest item; a category name is a link into the full list already filtered. **By
       counterparty** groups by whoever holds the money, with the categories involved and the date
       it is expected back.`,
    ),
    note(
      `The categories are a list an administrator maintains — see [recoverable
       categories](page:admin/recoverable-categories). A row that says \`Not recorded\` under
       Counterparty is a request that named none.`,
    ),
    tip('**Export CSV** at the top right downloads the full list, not only what is on screen.'),

    section('The full list'),
    shot('accounts/recoverables-list', 'Every live recoverable, aged against its expected return date.'),
    prose(
      `**Full list →** beside the category rollup opens this. It is one row per recoverable, and it
       includes items that are approved but not yet paid — those show \`Not yet paid\` where a date
       would be.`,
    ),
    table(
      ['Filter', 'What it does'],
      [
        ['**Search**', 'Matches the request number, the counterparty, the project and the requester'],
        ['**Category**', 'One recoverable category, or all of them'],
        ['**Ageing**', '`Overdue`, `Due in 30 days`, `Due later`, `Not yet paid`, or all'],
      ],
    ),
    prose(
      `The two status columns answer different questions and are worth reading together. **Ageing**
       is about the money — whether it is overdue, how long there is to go, or whether it has even
       been paid out. **Status** is the request’s own, so a recoverable can be overdue-flagged and
       still be awaiting approval.`,
    ),

    section('One recoverable'),
    shot('accounts/recoverable-detail', 'A single recoverable. This one is approved but not yet paid, so the payment section says so rather than showing figures.'),
    subsection('Recoverable details'),
    prose(
      `The amount, the category, the counterparty, the related project, the expected return date,
       the ageing and the refund terms the requester wrote. The card carries a **Not in budget
       actuals** badge, which is the same point the banner on the summary makes.`,
    ),
    subsection('Payment'),
    prose(
      `Once the money has gone, this shows the date, the amount, the mode, the reference, who
       recorded it and the payee. Until then it says the money has not left, so nothing is
       outstanding against a counterparty yet.`,
    ),
    subsection('History and conversation'),
    prose(
      `The same stream the request itself carries, and the same comment box. The placeholder here
       is pointed: record what you heard from the counterparty. That conversation is the only
       record of a chase, because the product does not track one.`,
    ),
    prose(
      `At the foot, **Back to list** and **Export this record**.`,
    ),

    section('What this version does not do'),
    prose(
      `The banner at the bottom of the summary is explicit, and it is worth taking at face value. A
       recoverable closes when its payment is made. Repayments, forfeitures and turning a lost
       deposit into an expense are deliberately out of scope — they need an accounting adjustment
       process, not an edit to a completed payment.`,
    ),
    prose(
      `In practice that means the register is a watch list. It tells you what is outstanding,
       against whom and how late; recovering it happens outside the product, and what you did about
       it is recorded in the conversation.`,
    ),

    faq([
      {
        q: 'Why does none of this show up in the variance grid?',
        a: `Because it is not spend. A recoverable request carries no budget head at all, and the
            grid and the monthly report both work through heads, so these payments never reach
            either. [Budget spend or money you expect back](page:requester/budget-and-recoverable)
            is the same distinction from the requester’s side.`,
      },
      {
        q: 'An item is overdue. What does the product do about it?',
        a: `It flags it — red in the list, counted in **Past expected return**, and a banner on the
            item itself. Chasing the money and recording its return happen outside this version,
            and the screen says so.`,
      },
      {
        q: 'A recoverable shows no expected return date.',
        a: `The requester did not give one. The row reads \`No fixed date\` and the item is never
            counted as overdue.`,
      },
      {
        q: 'My colleague’s register shows more rows than mine.',
        a: `The register obeys the same data scope your role has for requests, and so does the CSV
            behind it. [The permission matrix](page:reference/permissions-matrix) has the scopes as
            the running system holds them.`,
      },
    ]),
  ],
});
