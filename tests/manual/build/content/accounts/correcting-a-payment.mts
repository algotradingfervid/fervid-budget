import { page, prose, section, shot, steps, fields, note, warning, table, faq } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'correcting-a-payment',
  title: 'Correcting or voiding a payment',
  summary: 'What can still be changed after a payment is recorded, and what is sealed.',
  blocks: [
    prose(
      `Almost nothing. A payment that settled a request is the outcome of that request, and the
       requester and the approver were both told what it said. Editing it afterwards would rewrite
       what they were told, so the product does not allow it — not by you, not by an administrator,
       not at all.`,
    ),
    prose(
      `The confirmation step says this before you save: confirming saves the payment, and it cannot
       be edited or cancelled afterwards. The saved payment repeats it, and its detail card carries
       a **Read-only** badge.`,
    ),

    warning(
      `There is no undo. If the figure is wrong, the way through is words, not edits: explain it in
       the conversation on the request, and let the requester raise a fresh request for whatever is
       genuinely still owed. Refunds and reversals are outside this version.`,
      'A settled payment is final',
    ),

    section('What can still be changed'),
    prose(
      `One kind of payment is still editable: a payment with no request behind it. These are the
       historical rows recorded before every payment had to belong to an approved request. They are
       still ledger entries, so they are still corrected the old way.`,
    ),
    shot('accounts/payments-ledger', 'The ledger. Almost every row offers only **View**; one row here also offers **Edit** and **Remove**, because it has no request behind it.'),
    table(
      ['Kind of payment', 'View', 'Edit', 'Remove'],
      [
        ['Settled against a request', 'Yes', 'No', 'No'],
        ['No request behind it, month open, not already removed', 'Yes', 'Yes', 'Yes'],
        ['No request behind it, month locked', 'Yes', 'No — the ledger shows `Locked` in place of the controls, and the form itself opens read-only', 'No'],
        ['Already removed', 'Yes', 'No', 'No — the reason it was removed is shown instead'],
      ],
    ),
    note(
      `The Actions column is where you can tell the two kinds apart at a glance. Opening the edit
       address for a payment that settled a request takes you to the payment itself rather than to
       a form whose **Save** could only ever fail.`,
    ),

    section('Editing a request-less payment'),
    shot('accounts/payment-edit', 'The edit form. This particular payment sits in a locked period, so the banner says it is read-only and **Save Payment** is unavailable.'),
    fields([
      { name: 'Project / Head', required: true, note: 'What the spend is booked against, chosen from the active heads.' },
      { name: 'Paid on', required: true, note: 'The date the money left the bank.' },
      { name: 'Amount', required: true, note: 'In rupees. Commas and the ₹ sign are accepted.' },
      { name: 'Vendor / Payee', required: false, note: 'Free text on this form — there is no request to take the payee from.' },
      { name: 'Payment mode', required: false, note: '`Cash`, `Bank transfer`, `Cheque`, `Card`, `UPI` or `Other`.' },
      { name: 'Invoice / Bill No', required: false, note: 'The supplier’s document number.' },
      { name: 'Reference No', required: false, note: 'The bank’s reference for the transfer.' },
      { name: 'Attachment', required: false, note: 'An optional bill, receipt or payment proof. Uploading one adds to what is there rather than replacing it.' },
      { name: 'Remarks', required: false, note: 'Free text, recorded with the payment.' },
    ]),
    steps([
      { text: 'Open **Payments ledger** and find the payment.' },
      { text: 'Select **Edit** in its Actions column.' },
      { text: 'Change what is wrong.' },
      {
        text: 'Select **Save Payment**.',
        note: 'The change is written into the audit trail with the before and after values, so the correction is itself a record.',
      },
    ]),
    note(
      `**Back to grid** at the top right returns you to the variance grid for the month the payment
       belongs to.`,
    ),

    section('Removing a payment'),
    prose(
      `Removing — voiding — takes a payment out of the actual totals without deleting it. The row
       stays in the ledger, marked \`Removed\`, and shows the reason it was removed. It is offered
       on the same rows **Edit** is: a payment with no request behind it, in a month that is not
       locked.`,
    ),
    steps([
      { text: 'In the ledger, select **Remove** in the payment’s Actions column.' },
      {
        text: 'Type a **Reason**. It is required.',
        note: 'The browser asks you to confirm removing the payment from actual totals before anything happens.',
      },
      { text: 'Select **Remove**.' },
    ]),
    prose(
      `The payment’s own screen offers the same thing under **Void Payment**, with the same
       required reason. Switch the ledger’s **Status** filter to \`Removed / voided\` to list
       everything that has been taken out this way.`,
    ),

    section('When a settled payment is wrong'),
    prose(
      `Work out which of these it is, and the route follows.`,
    ),
    table(
      ['What is wrong', 'What to do'],
      [
        ['You paid less than approved and marked it fully settled', 'Nothing changes the payment. Say so in the conversation on the request; the balance needs a fresh request.'],
        ['The reference or the note is wrong', 'Post a comment on the request with the correct detail. The trail then carries both, in order.'],
        ['The approver disputes the shortfall', 'They raise a concern and the request stays in review — see [raising a concern](page:accounts/raising-a-concern).'],
        ['The money went to the wrong payee', 'That is an accounting adjustment outside this product. Record what happened in the conversation so the trail explains it.'],
      ],
    ),

    faq([
      {
        q: 'Can an administrator edit a settled payment?',
        a: `No. The rule sits in the store, under every screen, so it holds for every role.`,
      },
      {
        q: 'I attached the wrong bank advice.',
        a: `A payment tied to a request stops accepting documents the moment it is saved. Post the
            right one’s details in the conversation on the request.`,
      },
      {
        q: 'The Edit form opened but everything is greyed out.',
        a: `The payment belongs to a locked or voided period, and the banner at the top says so.
            A locked month is opened again by an administrator — see [monthly plans and
            locking](page:admin/monthly-plans-and-locking).`,
      },
      {
        q: 'Does removing a payment reopen the request?',
        a: `The question does not arise: only a payment with no request behind it can be removed.`,
      },
    ]),
  ],
});
