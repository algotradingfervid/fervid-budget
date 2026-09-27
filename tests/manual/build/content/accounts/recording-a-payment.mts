import { page, prose, section, shot, steps } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'recording-a-payment',
  title: 'Recording a payment',
  summary: 'Record real money movement against an approved request, then confirm its effect on the balance.',
  blocks: [
    prose(`Every new payment belongs to an approved request. Open **Accounts queue** and take a request
       for processing, or choose one from the payment picker. Only the reservation holder may
       record it.`),

    shot('accounts/payment-picker', 'The payment picker. Search the approved requests or choose one from the list; payments you recorded recently are listed underneath.'),

    section(`Enter the transfer`),

    prose(`The request card supplies the payee, project/head, invoice and approved amount. Check
       previously paid and remaining figures before entering another installment.`),

    shot('accounts/payment-form', 'The payment form. **What was approved** at the top is the request; the **Payment** and **Proof and notes** fieldsets below it are what you fill in.'),

    steps([
      { text: `Enter **Amount actually paid**. It must be positive and no greater than the approval minus all existing active payments.` },
      { text: `Enter the date the money left, an available payment mode and the transaction/reference number. Future dates, dates before the day the request was approved, and dates in a locked month are refused.` },
      { text: `Attach any advice or proof and add the processing note. Saved request-linked payments and their documents are permanent.` },
      { text: `Open the settlement confirmation. This preview writes no payment.` },
    ]),

    section(`Confirm the balance outcome`),

    prose(`If the payment clears the remaining balance, confirm it as **Fully settled**. If it is
       short, explicitly select **Installment — pay the balance later**, **Fully settled** with a
       required explanation of the deduction/agreed adjustment, or **Ask manager to accept a
       shortfall** with a required reason.

       [Paying part of a request](page:accounts/paying-part-of-a-request) explains all three paths.
       An installment preserves the approval for the remaining amount; a full settlement closes it;
       a requested shortfall decision waits for the manager.`),

    shot('accounts/settlement-sheet', 'The confirmation. This payment is short of the approved amount, so the sheet asks what happens to the remaining balance and nothing is selected for you.'),

    section(`After confirming`),

    prose(`The saved payment screen shows the immutable payment, proof and history. The requester and
       approver can follow the request outcome. Another installment requires a fresh reservation on
       the same request. The payment ceiling is recalculated when confirming, so an old form cannot
       spend past the remaining approval.

       Repeated confirmation of the same submission leads to the already recorded payment. It does
       not create another installment. Saved payments cannot be edited or cancelled; see
       [Correcting a payment](page:accounts/correcting-a-payment).`),

    shot('accounts/payment-detail', 'A saved payment. It is marked **Read-only**, the line beneath it says it can no longer be edited or cancelled, and the full trail from request to payment follows.'),
  ],
});
