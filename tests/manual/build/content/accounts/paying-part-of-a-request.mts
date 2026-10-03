import { page, prose, section, shot, steps, table } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'paying-part-of-a-request',
  title: 'Paying part of a request',
  summary: 'Record the payment and make an explicit decision about the unpaid balance.',
  blocks: [
    prose(`Enter the amount that actually left the bank. The confirmation compares **Remaining before
       this payment** with **Actually paid** and shows the difference. A short payment has no
       settlement choice selected for you. A payment that clears the balance is offered only
       **Fully settled** and **Ask manager to accept a shortfall**, and the second of those is
       refused — there is no shortfall to review.`),

    section(`Choose what happens to the balance`),

    shot('accounts/settlement-sheet', 'The confirmation for a payment short of the approved amount. The remaining balance is called out, and none of the three choices is selected yet.'),

    prose(`The explanation for either short-payment choice goes in **Reason for any deduction or
       shortfall**, under the choices.`),

    table(
      [`Choice`, `When to use it`, `Result`],
      [
        [`**Installment — pay the balance later**`, `Part has been paid and the rest remains payable under this approval.`, `The payment is recorded, the reservation is released, and the request returns to Approved. Accounts can claim it again for another installment. No notification is sent.`],
        [`**Fully settled**`, `No further amount is owed. For a short payment, explain the agreed deduction or adjustment.`, `The request closes as Completed, and by default the requester and approver are notified. The explanation is required when the payment is short of the remaining balance; the product does not calculate tax. The unpaid difference is never paid against this request.`],
        [`**Ask manager to accept a shortfall**`, `You need the approver to decide whether to close an unpaid balance.`, `A reason is required. The request moves to Partial — manager review, and by default the approver is notified.`],
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

    section(`Following it up`),

    shot('accounts/queue-partial-review', 'The Partial review tab of the payment queue. Every row carries the Partial — manager review pill and a View button.'),

    prose(`While the approver decides, the request sits on the **Partial review** tab of [the payment
       queue](page:accounts/the-payment-queue), its count on the tab. Each row shows the request
       number with the requester and approval time beneath it, the payee, the project and head (or a
       **Recoverable** pill), the approved amount, the needed-by date and the status pill **Partial —
       manager review**, which reads **Partial — under discussion** while the approver has a
       concern open.

       **View** opens the shortfall screen. It shows **Approved**, **Total paid to date** and **Still
       owed to the payee**; your reason under a banner reading *… marked this a genuine partial
       payment*; the recorded payment, marked **Cannot be edited**; and the history and conversation.
       The decision buttons are not yours — the foot of the screen says the approver *decides. You
       can still comment.* — so answer any question they raise in the comment box there. When they
       have raised a concern and you recorded the payment, the box’s label changes to
       “Answer *approver’s name*’s concern”, and posting your reply hands the
       decision back to them.`),

    section(`Following the balance`),

    prose(`The request shows cumulative payments and what remains. Installments awaiting payment return
       to the Accounts queue. Closed full settlements and accepted shortfalls remain closed;
       correcting a mistaken closure needs an agreed accounting process, not rewriting a saved
       payment.`),
  ],
});
