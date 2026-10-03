import { page, prose, section, shot, table } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'shortfall-review',
  title: 'Reviewing a shortfall',
  summary: 'Decide whether a recorded unpaid balance stays payable or is permanently closed.',
  blocks: [
    prose(`Accounts can ask you to review a payment shortfall. These requests wait on the **Partial
       review** tab of [the approvals queue](page:manager/the-approvals-queue), count towards the
       number beside **Approvals** in the sidebar, and appear on your home screen under **Partial
       payments to review**. Open the request from there or from the notification, then select
       **Decide the partial payment**. Review the approval, cumulative paid amount, remaining
       balance, recorded payment and Accounts' explanation. Where Accounts attached proof of
       payment, its name under **Proof** opens it.`),

    shot('manager/partial-review', `A shortfall review: what was approved, the total paid to date and what is still owed, with the accountant's reason beneath. Raise a concern and Accept and close sit in the bar at the foot, with the Keep balance payable panel below them.`),

    section(`The decisions`),

    table(
      [`Action`, `Use it when`, `What happens`],
      [
        [`**Keep balance payable**`, `The unpaid amount will be paid later.`, `Fill in **Payment plan or reason**, which is required. The request returns to Approved without a reservation. Accounts claims it again and records another payment under the same approval.`],
        [`**Accept and close**`, `You accept permanently closing the unpaid balance.`, `The confirmation names the write-off. The request becomes Completed — partial accepted and cannot be paid again.`],
        [`**Raise a concern**`, `You need Accounts to explain the short payment.`, `**What is wrong** is required; it is recorded on the thread and the accountant who recorded the payment is told. The request stays under review, its pill reading **Partial — under discussion** until they answer.`],
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
