import { page, prose, section, shot, steps, note, warning, table, faq } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'paying-part-of-a-request',
  title: 'Paying part of a request',
  summary: 'Settling short of the approved amount, and what happens to the difference.',
  blocks: [
    prose(
      `Sometimes the amount that leaves the bank is less than the amount that was approved. The
       product does not guess why. It asks you one question, and your answer decides whether the
       request closes or goes to somebody else.`,
    ),

    section('The question the confirmation asks'),
    shot('accounts/settlement-sheet', 'A payment short of the approved amount. The difference is called out, and the two outcomes are named with the status each produces.'),
    prose(
      `Enter the amount you actually paid on the form, then select **Payment settled →**. Because
       the figures do not match, the confirmation says “You paid less than was approved. Which is
       it?” and offers two choices.`,
    ),
    table(
      ['Choice', 'When it is right', 'What the product does'],
      [
        ['**Fully settled**', 'The obligation is discharged. TDS, retention or any other deduction was handled outside this system.', 'The request closes as `completed`. The requester and the approver are told it has been paid; neither is asked to decide anything.'],
        ['**Partial payment**', 'A balance is genuinely still owed to the payee.', 'The request moves to `partial_review` and the approver is asked to decide.'],
      ],
    ),
    warning(
      `The distinction is not cosmetic. **Fully settled** ends the request; **Partial payment**
       puts a decision on somebody else’s desk. Choosing the first because it is quicker leaves a
       real debt with no record that anybody owes it.`,
      'Two very different answers',
    ),

    section('Recording a partial payment'),
    steps([
      { text: 'Enter the amount that actually left the bank, then select **Payment settled →**.' },
      { text: 'On the confirmation, choose **Partial payment**.' },
      {
        text: 'Write into **Why only part was paid** — the field appears when you make that choice, and it is required.',
        note: 'The approver reads exactly what you write. Say what the payee agreed to, or what is still being waited on.',
      },
      {
        text: 'Select **Confirm and save payment**.',
        note: 'The payment is written, the request moves to `partial_review`, and the approver is told that it was approved for one figure and paid another.',
      },
    ]),
    note(
      `Recording a partial payment is a permission of its own. If your account does not carry it,
       the product refuses the partial branch and says so, while **Fully settled** still works.`,
    ),

    section('What the approver sees'),
    shot('manager/partial-review', 'The approver’s screen. Your reason is quoted at the top, and the payment underneath it is marked **Cannot be edited**.'),
    prose(
      `The approver gets the approved figure, what was paid, what is still owed, your reason, and
       the payment itself — read-only. They have two decisions, and neither of them moves any
       money.`,
    ),
    table(
      ['Their decision', 'What it does'],
      [
        ['**Accept and close**', 'The balance is written off for good. The request closes as `completed_partial`, which the screens write **Completed — partial accepted**.'],
        ['**Raise a concern**', 'The request stays in review and their words come back to you. Nothing is reversed — see [raising a concern](page:accounts/raising-a-concern).'],
      ],
    ),
    prose(
      `[Reviewing a shortfall](page:manager/shortfall-review) is the same story written for the
       person taking that decision.`,
    ),

    section('Following it up'),
    shot('accounts/queue-partial-review', 'The Partial review tab lists every shortfall waiting on an approver.'),
    prose(
      `The **Partial review** tab of [the payment queue](page:accounts/the-payment-queue) is where
       your shortfalls sit while they wait. **View** on a row opens the approver’s screen so you
       can see what has been said, and you can reply in the conversation there even though the
       decision is not yours.`,
    ),

    section('The balance'),
    prose(
      `Whichever way the approver decides, this request accepts no further payment. If the payee
       is later paid the rest, that is a new request raised by the requester for the outstanding
       amount, approved in its own right and paid in its own right. The blue note on the
       confirmation sheet says as much before you save.`,
    ),

    faq([
      {
        q: 'Can I pay the balance later against the same request?',
        a: `No. One request, one payment — the rule is stated at the top of the payment queue and
            enforced when the payment is written. The balance needs a fresh request.`,
      },
      {
        q: 'The approver accepted a shortfall I now think was a mistake.',
        a: `The request is closed as \`completed_partial\` and nothing reopens it. The trail on the
            payment records every step, including the reason you gave and the note the approver
            left, so the history explains itself. Anything further is a new request.`,
      },
      {
        q: 'I deducted TDS. Is that a partial payment?',
        a: `No. The confirmation is explicit: deductions handled outside this system still settle
            the obligation, so choose **Fully settled** and describe the deduction in the
            processing note.`,
      },
      {
        q: 'Nothing happened for days after I marked it partial.',
        a: `It is waiting on one person: the approver the request was routed to. Their name is on
            the request. You can post a comment on it to ask.`,
      },
    ]),
  ],
});
