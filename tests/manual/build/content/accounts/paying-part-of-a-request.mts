import { page, prose, section, shot, steps, table } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'paying-part-of-a-request',
  title: 'Paying part of a request',
  summary: 'Record the payment and make an explicit decision about the unpaid balance.',
  blocks: [
    prose(`Enter the amount that actually left the bank. The confirmation compares the approved amount,
       payments already recorded, this payment, and the remaining balance. A short payment has no
       settlement choice selected for you.`),

    section(`Choose what happens to the balance`),

    shot('accounts/settlement-sheet', 'The confirmation for a payment short of the approved amount. The remaining balance is called out, and none of the three choices is selected yet.'),

    table(
      [`Choice`, `When to use it`, `Result`],
      [
        [`**Installment — pay the balance later**`, `Part has been paid and the rest remains payable under this approval.`, `The payment is recorded, the reservation is released, and the request returns to Approved. Accounts can claim it again for another installment.`],
        [`**Fully settled**`, `No further amount is owed. For a short payment, explain the agreed deduction or adjustment.`, `The request closes. The explanation is required when the paid total is short of the approval; the product does not calculate tax.`],
        [`**Ask manager to accept a shortfall**`, `You need the approver to decide whether to close an unpaid balance.`, `A reason is required. The request moves to Partial — manager review.`],
      ],
    ),

    section(`Record an installment`),

    steps([
      { text: `Claim the approved request from the Accounts queue.` },
      { text: `Record the actual amount, date, payment mode and reference, and any proof.` },
      { text: `Open the confirmation and choose **Installment — pay the balance later**.` },
      { text: `Check the remaining balance and confirm.` },
      { text: `For the next transfer, claim the same request again. The next payment cannot exceed the remaining approved balance.` },
    ]),

    prose(`Each saved payment is permanent. Multiple installments share the original request and
       approval; they are not edits to earlier payments. Confirming the same submission twice does
       not create a second transfer record. Cancellation is unavailable after any payment has been
       recorded.`),

    section(`When the approver reviews a shortfall`),

    prose(`The approver can **Keep balance payable** with a required plan note, which returns the
       request to Approved for further payment; **Accept and close**, which permanently writes off
       the unpaid balance from this request; or **Raise a concern**, which records the question and
       leaves the request under review.

       Do not use Accept and close merely to arrange an installment. See [Reviewing a
       shortfall](page:manager/shortfall-review).`),

    shot('manager/partial-review', 'The approver’s screen. Your reason is quoted near the top, the payment is marked **Cannot be edited**, and the three decisions sit at the bottom.'),

    section(`Following the balance`),

    prose(`The request shows cumulative payments and what remains. Installments awaiting payment return
       to the Accounts queue. Closed full settlements and accepted shortfalls remain closed;
       correcting a mistaken closure needs an agreed accounting process, not rewriting a saved
       payment.`),
  ],
});
