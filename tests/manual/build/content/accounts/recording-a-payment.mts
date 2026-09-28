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
      { text: `Enter **Amount actually paid**. It opens filled in with the **Remaining approved balance**, and it must be positive and no greater than that — the approval minus all existing active payments.` },
      { text: `Enter **Paid on**, the date the money left, choose a **Payment mode** and type the **Transaction / UTR reference**. All three are required. Future dates, dates before the day the request was approved, and dates in a locked month are refused.` },
      { text: `Under **Add the bank advice**, attach any advice or proof, and add a **Processing note** if you need one — it is optional. Saved request-linked payments and their documents are permanent.` },
      { text: `Select **Payment settled →** to open the confirmation. It writes no payment.` },
    ]),

    section(`Confirm the balance outcome`),

    prose(`If the payment clears the remaining balance, **Fully settled** is already selected and the
       sheet says confirming closes the request. If it is short, nothing is selected: choose
       **Installment — pay the balance later**, **Fully settled** with the deduction or agreed
       adjustment explained under **Reason for any deduction or shortfall**, or **Ask manager to
       accept a shortfall** with a reason in the same box. Then select **Confirm and save
       payment**.

       [Paying part of a request](page:accounts/paying-part-of-a-request) explains all three paths.
       An installment preserves the approval for the remaining amount; a full settlement closes it;
       a requested shortfall decision waits for the manager.`),

    shot('accounts/settlement-sheet', 'The confirmation. This payment is short of the approved amount, so the sheet asks what happens to the remaining balance and nothing is selected for you.'),

    section(`After confirming`),

    prose(`The saved payment screen opens with a banner saying the payment is saved and whether the
       request is now completed, with the manager, or still payable for the remaining balance. Below
       it are the payment, the proof and the history.

       Who is told depends on your choice. Under the default notification rules, a full settlement notifies the requester and the
       approver; a shortfall sent for review notifies the approver. An installment sends no
       notification at all — the payment shows in the request’s history, and the request goes back
       to the first tab of the queue. Another installment requires a fresh reservation on the same
       request. The payment ceiling is recalculated when confirming, so an old form cannot
       spend past the remaining approval.

       Repeated confirmation of the same submission leads to the already recorded payment, under a
       banner reading **This payment confirmation was already recorded**. It does not create another
       installment. Saved payments cannot be edited or cancelled; see
       [Correcting a payment](page:accounts/correcting-a-payment).`),

    shot('accounts/payment-detail', 'A saved payment. It is marked **Read-only**, the line beneath it says it can no longer be edited or cancelled, and the full trail from request to payment follows.'),
  ],
});
