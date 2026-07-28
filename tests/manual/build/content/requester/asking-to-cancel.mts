import { page, prose, section, shot, steps, note, warning, table, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'asking-to-cancel',
  title: 'Asking to cancel an approved request',
  summary: 'Once a request is approved you ask rather than act — how that works.',
  blocks: [
    prose(
      `Approval changes who owns a request. Until it is approved you can edit it or take it back on
       your own say-so; afterwards it is a commitment somebody else has signed, and Accounts may
       already be working on it. So the product replaces withdrawal with a request: you say why it
       should not be paid, and the approver who signed it decides.`,
    ),

    section('The locked banner'),
    shot('requester/detail-approved', 'An approved request. The banner says what has changed, and the only control left to you is Request cancellation.'),
    prose(
      `Open an approved request of your own and a banner sits under the heading: **Approved requests
       are locked** — *You can no longer edit this. If it should not be paid, ask for it to be
       cancelled — payment freezes while your approver decides.* The action bar carries one button,
       **Request cancellation**.`,
    ),

    section('Asking'),
    shot('requester/cancel-form', 'The cancellation request. It says what asking costs before you ask.'),
    steps([
      { text: 'Open the approved request and select **Request cancellation**.' },
      {
        text: 'Read the amber banner.',
        note: '**Payment freezes the moment you ask** — *Accounts cannot reserve or pay this request while a cancellation is pending. If an accountant has already reserved it, they are told immediately.*',
      },
      {
        text: 'Write the **Reason**.',
        note: 'It is required, and the placeholder tells you who reads it: *Say what changed*, followed by your approver’s name and *sees exactly this*.',
      },
      {
        text: 'Select **Send cancellation request**.',
        note: '**Never mind** takes you back to the request and changes nothing.',
      },
    ]),
    prose(
      `The screen sets out the three outcomes before you commit, under **What happens next**:`,
    ),
    table(
      ['Then', 'What happens'],
      [
        ['Straight away', 'Your approver is notified and the request shows **Cancellation requested**.'],
        ['If they accept', 'The request is cancelled and closed. Nothing can be paid against it.'],
        ['If they decline', 'It returns to **Approved — awaiting payment** and Accounts can proceed.'],
      ],
      'The wording is the screen’s own.',
    ),
    warning(
      `Leave the reason empty and the product refuses with \`say why it should be cancelled\`. The
       reason is the whole of what your approver has to go on, and Accounts read it too.`,
    ),

    section('While it is pending'),
    shot('requester/detail-cancellation-asked', 'A request with a cancellation pending. Payment is frozen and the request is waiting on the approver again.'),
    prose(
      `The status pill becomes **Cancellation requested**, and the waiting sentence goes back to naming
       your approver. An amber banner replaces the locked one: **Payment is frozen**, over a sentence
       saying no accountant can reserve or pay the request until your approver decides, and quoting
       back the reason you wrote after the words *Reason given:*.`,
    ),
    prose(
      `Your asking is written into the history as its own line, quoting the reason and ending
       *Payment frozen.* — so anybody reading the request later can see when the freeze started and
       why.`,
    ),
    note(
      `You cannot edit the request while a cancellation is pending, and you cannot withdraw the
       cancellation. It is with your approver until they answer.`,
    ),

    section('The two answers'),
    table(
      ['Answer', 'The request becomes', 'What you can do next'],
      [
        ['**Accepted**', '`cancelled` — closed for good. Nothing will be paid against it.', 'Raise a new request if the money is still needed.'],
        ['**Declined**', '`approved` again, with the approver’s written reason on the record.', 'Nothing. Accounts carry on and the payment goes ahead.'],
      ],
    ),
    prose(
      `Either way you are told, and the answer joins the request's history. A decline always carries a
       reason — the approver is refused an empty one — because you and Accounts both have to read it.`,
    ),

    section('Cancellation you did not ask for'),
    prose(
      `An approver can also cancel an approved request outright, without your having asked, and must
       give a reason for that too. You are notified when it happens; the request shows **Cancelled**
       and the waiting sentence reads *Cancelled. Nothing can be paid against it.* See
       [Deciding a cancellation request](page:manager/cancellation-requests) for the approver's side.`,
    ),

    faq([
      {
        q: 'Accounts have already paid it. Can I still cancel?',
        a: `No. Once a payment has been recorded the request is settled, and cancelling it would
            contradict a payment that exists. Talk to Accounts about a refund instead.`,
      },
      {
        q: 'How long does a cancellation take to decide?',
        a: `As long as your approver takes. The payment stays frozen throughout, so a request nobody
            answers is a request nobody can pay — chase it if it matters.`,
      },
      {
        q: 'My approver declined. Can I ask again?',
        a: `The request is approved again, so yes, the control comes back. Asking a second time with
            the same reason is unlikely to get a different answer — say what has changed.`,
      },
      {
        q: 'Can I cancel a request that is still awaiting approval?',
        a: `That is a withdrawal, not a cancellation, and it needs nobody’s permission. See
            [Changing or withdrawing a request](page:requester/editing-and-withdrawing).`,
      },
    ]),
  ],
});
