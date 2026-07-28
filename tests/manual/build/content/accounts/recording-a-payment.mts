import { page, prose, section, subsection, shot, steps, fields, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'recording-a-payment',
  title: 'Recording a payment',
  summary: 'The payment form field by field, and what the product checks before it accepts one.',
  blocks: [
    prose(
      `Recording a payment is two screens. The first collects what left the bank. The second
       compares it with what was approved and asks you to confirm. Nothing is written until you
       confirm, and once you have, nothing can change it.`,
    ),
    prose(
      `You reach the form by [claiming a request](page:accounts/claiming-a-request). If you open
       it without one, the product asks which request the payment is for.`,
    ),

    shot('accounts/payment-picker', 'The picker, shown when you open the payment form without naming a request. Choosing a row claims it.'),
    note(
      `The blue banner explains why there is a picker at all: free payment entry has been removed.
       Every payment now belongs to exactly one approved request. Payments recorded before that
       change are still in the ledger as history.`,
    ),

    section('The form'),
    shot('accounts/payment-form', 'The payment form. The card at the top is the request; the fieldsets below it are what you fill in.'),
    prose(
      `**What was approved** is a read-only summary of the request: the approved amount, who
       approved it and when, the payee, the project and head it is charged to, and its purpose.
       **Open the request →** takes you to the request itself if you need the invoice or the
       conversation.`,
    ),

    subsection('The fields'),
    fields([
      {
        name: 'Approved amount',
        required: false,
        note: 'Read-only, and the ceiling for what follows. The hint under it says so: a payment can never exceed this.',
      },
      {
        name: 'Amount actually paid',
        required: true,
        note: 'What left the bank. It is spelled out in words underneath as you type, and a banner above tells you whether it matches the approved figure.',
      },
      {
        name: 'Paid on',
        required: true,
        note: 'The date the money left. It opens on today and cannot be in the future.',
      },
      {
        name: 'Payment mode',
        required: true,
        note: 'Choose from `Bank transfer`, `Cheque`, `UPI`, `Cash`, `Card` or `Other`.',
      },
      {
        name: 'Transaction / UTR reference',
        required: true,
        note: 'The bank’s reference for the transfer. It is what somebody reading the ledger later matches against a statement.',
      },
      {
        name: 'Payment advice or proof',
        required: false,
        note: 'Attach the bank advice — PDF, JPG or PNG up to 10 MB. It is uploaded once, when you confirm.',
      },
      {
        name: 'Processing note',
        required: false,
        note: 'Free text. The hint is worth reading: the product does no tax arithmetic, so if you deducted anything, say so here and the trail explains the difference.',
      },
    ]),
    note(
      `The payee, the project and head, and the invoice number are not on the form. They are facts
       of the request the approver signed off, so the product carries them across itself rather
       than offering you a chance to disagree with them.`,
    ),
    warning(
      `Attach the advice now. Once the payment is saved it will not accept a new document, because
       a payment tied to a request is sealed the moment it is written.`,
      'The one chance to attach proof',
    ),

    subsection('What the form refuses'),
    table(
      ['If you', 'The product says'],
      [
        ['Enter more than the approved amount', 'It names both figures and tells you to cancel the request and raise a new one — paying more is a different obligation, not a settlement decision.'],
        ['Date the payment in the future', 'Money cannot have left the bank after today, so the date is refused.'],
        ['Date it inside a locked month', 'The month is closed. A banner appears above the form before you start typing, naming the month and telling you to pick a date in an open one or ask for that month to be unlocked.'],
        ['Leave the amount, date, mode or reference empty', 'The field is required and the form will not move to the confirmation.'],
      ],
    ),
    note(
      `The lock is a monthly plan an administrator opened and closed — see [monthly plans and
       locking](page:admin/monthly-plans-and-locking).`,
    ),

    section('Confirming the settlement'),
    prose(
      `**Payment settled →** does not save anything. It opens the confirmation, which sets what
       you are about to record beside what was approved and asks one question: does this discharge
       the obligation?`,
    ),
    shot('accounts/settlement-sheet', 'The confirmation. Here the payment is short of the approved amount, so the sheet asks which kind of shortfall it is.'),
    prose(
      `When the two figures match, the sheet says so and the difference reads ₹0.00; confirming
       closes the request. When they do not, the difference is shown and you choose between two
       outcomes.`,
    ),
    table(
      ['Choice', 'What it means', 'Where the request goes'],
      [
        ['**Fully settled**', 'The obligation is discharged. Deductions such as TDS or retention were handled outside this system.', '`completed`'],
        ['**Partial payment**', 'A balance is genuinely still owed to the payee. A reason is required.', '`partial_review`, waiting on the approver'],
      ],
    ),
    prose(
      `[Paying part of a request](page:accounts/paying-part-of-a-request) covers the second choice
       in full. The blue note on the sheet states the consequence of either: confirming saves the
       payment, it cannot be edited or cancelled afterwards, and the request accepts no further
       payment.`,
    ),

    steps([
      { text: 'Check the two figures at the top of the sheet against the advice in front of you.' },
      { text: 'Choose **Fully settled** or **Partial payment**.' },
      { text: 'If you chose **Partial payment**, write why only part was paid. The approver reads it.' },
      {
        text: 'Select **Confirm and save payment**.',
        note: '**Go back** returns to the form with everything you typed still there.',
      },
    ]),

    section('What happens next'),
    shot('accounts/payment-detail', 'The saved payment. The green banner names who was notified and states that this can no longer be edited or cancelled.'),
    prose(
      `You land on the payment. It shows the approved figure beside the paid one, the payment
       itself marked **Read-only**, any proof attached to it, and the full trail from the moment
       the request was submitted to the moment it was settled. The requester and the approver are
       both notified.`,
    ),
    tip(
      `The action bar carries **Open the request** and **Back to ledger**. The note beside them is
       the honest limit of this version: refunds and reversals are outside it.`,
    ),

    faq([
      {
        q: 'I pressed **Confirm and save payment** twice.',
        a: `The second press finds the payment already exists and takes you to it. There is one
            payment per request and the product will not write a second.`,
      },
      {
        q: 'The amount I paid included a deduction. Which choice is right?',
        a: `**Fully settled**. The sheet spells this out: a deduction handled outside this system
            still discharges the obligation. Record what you did in the processing note so the
            trail explains the gap. Choose **Partial payment** only when the payee is genuinely
            still owed money.`,
      },
      {
        q: 'I lost the reservation while I was filling the form in.',
        a: `The confirmation re-checks it, and if it has gone you get the conflict screen naming
            who holds the request now. Nothing you typed was saved and no payment was created —
            see [reservations and conflicts](page:accounts/reservations-and-conflicts).`,
      },
      {
        q: 'Where did the invoice number go?',
        a: `Onto the payment, from the request, without passing through the form. When the request
            carried one, the payment’s own screen shows it under **Invoice**.`,
      },
    ]),
  ],
});
