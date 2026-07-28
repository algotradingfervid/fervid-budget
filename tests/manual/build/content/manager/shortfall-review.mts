import { page, prose, section, shot, steps, fields, table, note, warning, tip, faq } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'shortfall-review',
  title: 'Reviewing a shortfall',
  summary: 'When Accounts pays less than you approved, the difference comes back to you.',
  blocks: [
    prose(
      `Sometimes the accounts team pays less than you approved and says the shorter figure is
       genuinely what was owed — a vendor agreed to a part payment, a deduction was applied, an
       invoice turned out to be smaller than the request. When they record that as a partial
       settlement, the request does not close. It comes back to you.

       The status is \`partial_review\` and the pill reads **Partial — manager review**. You are the
       only person who can close it.`,
    ),

    section('Finding it'),
    prose(
      `This decision has no tab in [the approvals queue](page:manager/the-approvals-queue) — the
       queue covers approvals and cancellations only. You reach a shortfall review in one of two
       ways: from the notification the product sends you when Accounts records the partial payment,
       or by opening the request and selecting **Decide the partial payment**.`,
    ),
    shot('manager/partial-review', 'A shortfall review: what you approved, what was paid, and what is still owed.'),

    section('Reading the screen'),
    table(
      ['On the screen', 'What it holds'],
      [
        ['**Approved**', 'The figure you approved. Not the figure that was originally requested, if you reduced it.'],
        ['**Paid on** *date*', 'What actually left the bank, and when.'],
        ['**Still owed to the vendor**', 'The difference between the two. This is the number you are being asked about.'],
        ['The banner', 'The accountant’s name and their reason, in quotation marks — why they say the short payment is genuine.'],
        ['**The payment that was recorded**', 'Amount paid, paid on, mode, reference, and who recorded it. The pill beside it reads **Cannot be edited**.'],
        ['**History and conversation**', 'Everything from submission to settlement, in order.'],
        ['**Reply to Accounts**', 'A comment box aimed at the accountant, so you can ask before you decide.'],
      ],
    ),
    prose(
      `The two controls sit along the bottom: **Raise a concern** and **Accept and close**. The note
       beside them is the fact that shapes the whole decision — *No further payment can be attached
       either way.* Whichever you choose, no more money will be paid against this request.`,
    ),
    note(
      `Ask before you decide. The comment box is labelled **Reply to Accounts** and is aimed at the
       person who recorded the payment. A question there is free; accepting is not.`,
    ),

    section('Accepting the shortfall and closing'),
    prose(
      `Accept when the shorter payment settles the matter — the vendor is content, the deduction was
       correct, nothing further is owed on this request.`,
    ),
    steps([
      { text: 'Read the accountant’s reason and check the three figures.' },
      {
        text: 'Select **Accept and close**.',
        note: 'The panel restates the figures under their own headings and names the amount that will never be paid.',
      },
      {
        text: 'Read the panel. The middle row is **Paid and accepted**; the row below it is **Written off from this request**.',
        shot: 'manager/partial-close-sheet',
      },
      { text: 'Add a **Note** if you want to record why. It is optional.' },
      { text: 'Select **Accept and close**.' },
    ]),
    prose(
      `The panel spells out what closing means: the request closes as **Completed — partial
       accepted**, and if the balance is still due later, the requester raises a new request for it.
       That is the only route to the rest of the money — there is no way to attach a second payment
       to a closed request.`,
    ),
    warning(
      `Closing is the end of the request. The status becomes \`completed_partial\`, which has nowhere
       further to go. The written-off difference is not owed by the product to anybody; it is no
       longer part of this request at all.`,
      'The remainder is written off here',
    ),

    section('Raising a concern'),
    prose(
      `Raise a concern when the short payment does not look right to you and you want the accounts
       team to answer for it.`,
    ),
    steps([
      {
        text: 'Select **Raise a concern**.',
        note: 'The panel opens with the box already focused.',
      },
      {
        text: 'Write in **What is wrong**. It is required.',
        shot: 'manager/partial-concern-sheet',
      },
      { text: 'Select **Raise concern**.' },
    ]),
    warning(
      `The panel says it in as many words — *This does not reverse anything.* The money has left the
       bank. Raising a concern records your objection, puts it in the conversation and tells the
       accountant. It does not undo the payment, does not change the amount, and does not change the
       status: the request stays under review, and the same two controls are waiting for you next
       time you open it.`,
      'A concern reverses nothing',
    ),
    prose(
      `A concern is a way of keeping the request open while the two of you agree what happens next.
       Accounts can reply in the thread, but the recorded payment itself cannot be changed from this
       screen.`,
    ),

    section('The two panels, field by field'),
    fields(
      [
        {
          name: 'Note',
          required: false,
          note: 'On the accept panel. Recorded in the history and visible to everyone.',
        },
        {
          name: 'What is wrong',
          required: true,
          note: 'On the concern panel. A blank one is refused. Accounts can reply in the conversation, but the recorded payment cannot be changed.',
        },
      ],
    ),

    section('What the two decisions do'),
    table(
      ['', 'Accept and close', 'Raise a concern'],
      [
        ['Status afterwards', '`completed_partial`', 'Unchanged — still `partial_review`'],
        ['Reverses the payment', 'No', 'No'],
        ['Can be taken again', 'No — the request is closed', 'Yes, as often as you need'],
        ['Who is told', 'The requester and the assigned accountant', 'The assigned accountant'],
        ['The remaining balance', 'Written off from this request', 'Still open as a question'],
      ],
    ),

    section('A request that settled in full'),
    prose(
      `For contrast, a request Accounts paid in full closes without ever reaching you. The status is
       **Completed**, the line beside it reads *Nothing pending*, and the Payment outcome block
       shows the approved figure, what was paid, and a difference of ₹0.00 marked *confirmed settled
       by Accounts*.`,
    ),
    shot('manager/detail-completed', 'A request that completed in full. No decision was needed from the approver.'),
    tip(
      `If the balance is genuinely still due, accept and close, and tell the requester to raise a new
       request for the remainder. Leaving the request open with a concern does not get the vendor
       paid.`,
    ),

    faq([
      {
        q: 'Can I make Accounts pay the difference?',
        a: `Not from this screen. No further payment can be attached to this request either way. The
            balance has to come through a new request.`,
      },
      {
        q: 'I raised a concern and nothing happened.',
        a: `That is what it does. The concern is in the conversation and the accountant has been told;
            the request stays under review until you accept and close it.`,
      },
      {
        q: 'Somebody else is looking at the same shortfall.',
        a: `They can read it and comment, but only the approver named on the request sees the two
            controls. Everybody else is shown a line saying who decides.`,
      },
      {
        q: 'I opened the review and was sent back to the request.',
        a: 'The request is no longer under review — somebody has already closed it, or it never reached that state. Read the status pill at the top.',
      },
    ]),
  ],
});
