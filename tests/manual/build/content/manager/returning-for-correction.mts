import { page, prose, section, shot, steps, fields, table, note, tip, faq } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'returning-for-correction',
  title: 'Returning a request for correction',
  summary: 'Sending a request back with a reason, and what the requester sees next.',
  blocks: [
    prose(
      `Returning hands a request back to the person who raised it, with a message saying what needs
       fixing. They correct it and send it again; it comes back to you with the same number and the
       whole history intact.

       It is the right decision for anything that is wrong rather than unwanted — the wrong head, a
       missing invoice, a figure that does not match the paperwork. Prefer it to rejection whenever
       the request could be made right, because rejection cannot be undone.`,
    ),

    section('Returning a request'),
    steps([
      { text: 'Open the request and satisfy yourself that the problem is fixable.' },
      {
        text: 'Select **Return for correction**.',
        note: 'The panel that opens says who can act next: the requester can edit and resubmit, and the number and history stay.',
      },
      {
        text: 'Write in **What needs correcting**. This is required, and the placeholder is a fair warning — *Be specific — this is the whole message they get.*',
        shot: 'manager/return-sheet',
      },
      {
        text: 'Select **Return request**.',
        note: 'You land back on the approvals queue and the request has left the **To approve** tab.',
      },
    ]),

    section('The panel, field by field'),
    fields(
      [
        {
          name: 'What needs correcting',
          required: true,
          note: 'The message the requester receives, word for word. A blank one is refused. It is recorded in the history, so everybody on the request sees it.',
        },
      ],
      'The return panel.',
    ),
    tip(
      `Name the field and the fix. *Booked to Office Rent — this is a training cost, please move it
       to Training and Development* tells the requester exactly what to change. *Wrong head* does
       not.`,
    ),

    section('What the requester sees'),
    prose(
      `The request moves to \`returned\`. It leaves your queue and reappears on the requester's home
       screen under **Needs your action**, carrying your message. They can edit it and submit it
       again, at which point it comes back to \`pending\` and to you.

       Nothing about the request is lost in the round trip: the number, the thread and every earlier
       decision stay on it. Your message stays in the history too, so anybody reading the request
       later can see why it went back.`,
    ),
    note(
      `A returned request does not appear on the **Decided** tab of your queue. Decided holds only
       approved, rejected and cancelled requests. A returned one is genuinely still in flight.`,
    ),

    section('When returning is refused'),
    table(
      ['What you see', 'Why'],
      [
        ['A message saying a comment is required to return a request', 'The message box was left empty. It is checked before anything else.'],
        ['*You do not have permission to perform this action.*', 'You are not the approver this request was routed to.'],
        ['A message saying a request in that state cannot be returned', 'Only a `pending` request can be returned. It has already been decided, withdrawn, or returned once already and not yet resubmitted.'],
      ],
    ),

    section('Returning against rejecting'),
    table(
      ['', 'Return', 'Reject'],
      [
        ['Reversible', 'Yes — the requester fixes it and sends it again', 'No'],
        ['Keeps the number', 'Yes', 'The number survives, but the request is closed'],
        ['Requires wording', 'Yes — **What needs correcting**', 'Yes — **Reason for rejection**'],
        ['Status afterwards', '`returned`', '`rejected`'],
        ['Use it when', 'The request could be right', 'The spend should not happen at all'],
      ],
    ),
    prose(
      `If you are unsure which you want, return it. A returned request can still be rejected later;
       a rejected one cannot be brought back.
       [Rejecting a request](page:manager/rejecting) covers the other half.`,
    ),

    faq([
      {
        q: 'Can I return a request more than once?',
        a: `Yes, as often as it needs. Each round trip is recorded in the history, so a request that
            keeps coming back is visible for what it is.`,
      },
      {
        q: 'I returned it by mistake. Can I pull it back?',
        a: `Not directly — the request is with its requester now. Ask them to submit it again, and
            leave a comment in the thread saying why.`,
      },
      {
        q: 'The requester says they never got the message.',
        a: `It is on the request, in the History and conversation stream, which everybody who can see
            the request can read. Whether an email also went out depends on how your organisation has
            set up [notification rules](page:admin/notification-rules).`,
      },
    ]),
  ],
});
