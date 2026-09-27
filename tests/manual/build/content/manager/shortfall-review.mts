import { page, prose, section, shot, table } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'shortfall-review',
  title: 'Reviewing a shortfall',
  summary: 'Decide whether a recorded unpaid balance stays payable or is permanently closed.',
  blocks: [
    prose(`Accounts can ask you to review a payment shortfall. Open the notification or the request's
       **Decide the partial payment** action. Review the approval, cumulative paid amount,
       remaining balance, recorded payment and Accounts' explanation.`),

    shot('manager/partial-review', `A shortfall review: what was approved, the total paid to date and what is still owed, with the accountant's reason beneath. The three decisions sit at the foot.`),

    section(`The decisions`),

    table(
      [`Action`, `Use it when`, `What happens`],
      [
        [`**Keep balance payable**`, `The unpaid amount will be paid later.`, `Add the required plan note. The request returns to Approved without a reservation. Accounts claims it again and records another payment under the same approval.`],
        [`**Accept and close**`, `You accept permanently closing the unpaid balance.`, `The confirmation names the write-off. The request becomes Completed — partial accepted and cannot be paid again.`],
        [`**Raise a concern**`, `You need Accounts to explain the short payment.`, `A required comment records the concern and tells Accounts. Status stays under review.`],
      ],
    ),

    section(`Accepting is a permanent closure`),

    prose(`Do not accept and close a balance that is still owed next week. Use **Keep balance payable**
       and record the plan. Closing does not reverse a payment or move money, and reopening a
       completed request is not supported.`),

    shot('manager/partial-close-sheet', 'The Accept and close panel names the amount that will never be paid, and sets Total paid and accepted against Written off from this request.'),

    section(`Discussing the decision`),

    prose(`The conversation retains questions and replies with the financial history. Raising a concern
       leaves the payment unchanged. Only the assigned approver with the relevant permission can
       decide the shortfall; other readers can follow the history their access allows.`),

    shot('manager/partial-concern-sheet', 'The Raise a concern panel. What is wrong is required, and the panel states that raising a concern does not reverse anything.'),

    section(`Installments without a shortfall decision`),

    prose(`When Accounts chooses **Installment — pay the balance later**, the request returns directly
       to Approved for the remaining balance. The prior approval continues to apply. When total
       payments clear the balance, full settlement closes the request.`),

    shot('manager/detail-completed', 'A request settled in full. Payment outcome shows the total paid to date and a difference of ₹0.00, confirmed settled by Accounts.'),
  ],
});
