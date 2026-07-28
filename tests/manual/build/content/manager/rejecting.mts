import { page, prose, section, shot, steps, fields, table, warning, note, faq } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'rejecting',
  title: 'Rejecting a request',
  summary: 'The decision that cannot be undone, and when to prefer returning instead.',
  blocks: [
    prose(
      `Rejection closes a request for good. There is no route back from it: the requester cannot edit
       it, resubmit it or appeal it, and no other approver can overturn it. If the spend should still
       happen in some form, somebody has to raise a fresh request with a new number.

       Use it when the answer is no — the money should not be spent, not that the paperwork is wrong.
       For anything fixable, [return the request](page:manager/returning-for-correction) instead.`,
    ),

    section('Rejecting a request'),
    steps([
      { text: 'Open the request and read it through. There is no undo after this.' },
      {
        text: 'Select **Reject**.',
        note: 'The panel repeats the consequence: rejection is final and read-only, and the requester would have to raise a new request.',
      },
      {
        text: 'Read the banner inside the panel — *This cannot be undone. If the request is fixable, return it for correction instead.*',
        shot: 'manager/reject-sheet',
      },
      { text: 'Write in **Reason for rejection**. It is required, and it is what the requester is left with.' },
      {
        text: 'Select **Reject permanently**.',
        note: 'You land back on the approvals queue and the request moves to the **Decided** tab.',
      },
    ]),

    section('The panel, field by field'),
    fields(
      [
        {
          name: 'Reason for rejection',
          required: true,
          note: 'A blank reason is refused. It is recorded on the request and shown to everybody who can see it, including anybody who reads the request months later.',
        },
      ],
      'The reject panel.',
    ),
    warning(
      `**Reject permanently** is the last press. Nothing about a rejected request can be changed
       afterwards — not the reason, not the status.`,
      'There is no undo',
    ),

    section('What a rejected request looks like'),
    prose(
      `The status pill reads **Rejected — final**, and the line beside it says *Closed. Raise a new
       request if needed.* Your reason is shown twice: once in a banner across the top of the
       request, headed with your name, and again as a line in the history with the time you took the
       decision.`,
    ),
    shot('manager/detail-rejected', 'A rejected request. The reason sits in a banner and in the history.'),
    prose(
      `The requester can raise a replacement from the rejected request — a new request, with a new
       number, carrying nothing of this one but whatever they copy across.
       [After a rejection](page:requester/after-a-rejection) covers their side.`,
    ),

    section('When rejection is refused'),
    table(
      ['What you see', 'Why'],
      [
        ['A message saying a reason is required to reject a request', 'The reason box was empty. It is checked before anything else.'],
        ['*You do not have permission to perform this action.*', 'You are not the approver this request was routed to.'],
        ['A message saying a request in that state cannot be rejected', 'Only a `pending` request can be rejected. Somebody has already decided it, or the requester withdrew it.'],
      ],
    ),

    section('Choosing between reject and return'),
    prose(
      `The question is not how annoyed you are with the request; it is whether the same money, spent
       the same way, could ever be right.`,
    ),
    table(
      ['The situation', 'The decision'],
      [
        ['Booked to the wrong head', 'Return'],
        ['The invoice is missing', 'Return'],
        ['The amount does not match the invoice', 'Return'],
        ['A duplicate of something already approved', 'Reject, naming the request it duplicates'],
        ['Outside the approved budget for this head', 'Reject'],
        ['The spend is not authorised at all', 'Reject'],
      ],
    ),
    note(
      `If the request is right but the timing is wrong, neither decision fits well. Leave it in your
       queue and say so in the thread — a request sitting at \`pending\` costs nothing, and the
       requester can withdraw it themselves if they would rather start again.`,
    ),

    faq([
      {
        q: 'I rejected the wrong request.',
        a: `It cannot be reversed. Tell the requester at once and ask them to raise it again — and
            leave a comment on the rejected request explaining the mistake, so the record is honest.`,
      },
      {
        q: 'Can the requester argue with a rejection?',
        a: `They can comment on the request, and everybody who can see it will read that. But the
            status does not move, and no control on the screen will change it.`,
      },
      {
        q: 'Does rejecting free up budget?',
        a: `There was nothing to free. The variance grid counts money that has been paid, and a
            request that was never approved never reached a payment.`,
      },
    ]),
  ],
});
