import { page, prose, section, shot, steps, fields, table, note, warning, faq } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'cancellation-requests',
  title: 'Deciding a cancellation request',
  summary: 'When a requester asks to cancel something you already approved.',
  blocks: [
    prose(
      `Once you approve a request, its raiser can no longer withdraw it — it is on its way to being
       paid. If it should not be paid after all, they ask instead, and the ask lands on you.

       The moment they ask, payment freezes: no accountant can reserve or pay the request until you
       have decided. That is why these sit in a tab of their own, and why the home screen calls them
       *Decisions only you can make*.`,
    ),
    shot('manager/approvals-cancellations', 'The Cancellations tab of the approvals queue.'),

    section('Opening the decision'),
    prose(
      `Select the request from the **Cancellations** tab, or open it from anywhere and select
       **Decide the cancellation**. Either way you land on the cancellation screen, which is a
       stripped-back version of the request built around the one question you have to answer.`,
    ),
    shot('manager/cancellation-decision', 'The cancellation screen. Payment is frozen while you decide.'),
    table(
      ['On the screen', 'What it holds'],
      [
        ['The **Payment is frozen** banner', 'A reminder that nothing can be paid until you decide.'],
        ['**Why … wants it cancelled**', 'The requester’s own reason, in quotation marks. This is the case you are ruling on.'],
        ['**What you approved**', 'The amount, the date you approved it, the vendor or payee, and the purpose.'],
        ['**History and conversation**', 'The full stream, including the line recording the cancellation ask.'],
      ],
    ),
    prose(
      `The two controls sit along the bottom: **Decline — keep it live** and **Cancel the request**.
       The note beside them says what declining does — *Declining sends it back to Accounts to pay as
       approved.*`,
    ),

    section('Accepting the cancellation'),
    steps([
      { text: 'Read the requester’s reason and the amount you approved.' },
      {
        text: 'Select **Cancel the request**.',
        note: 'The panel names the request and the payee, and warns that the request closes permanently.',
      },
      {
        text: 'Add a **Note** if you want to. It is optional here, and it goes into the history.',
        shot: 'manager/cancellation-accept-sheet',
      },
      {
        text: 'Select **Cancel request**.',
        note: 'You land back on the Cancellations tab, and the request is gone from it.',
      },
    ]),
    warning(
      `The panel is blunt about the consequence: *The request closes permanently. Nothing can be paid
       against it. A new request is needed if the order comes back.* There is no route from a
       cancelled request back to an approved one.`,
      'Cancelling is final',
    ),

    section('Declining the cancellation'),
    prose(
      `Declining means the request stands and should still be paid. It is the right answer when the
       requester has changed their mind about something the organisation is nonetheless committed
       to.`,
    ),
    steps([
      {
        text: 'Select **Decline — keep it live**.',
        note: 'The panel says where the request goes: *The request returns to Approved — awaiting payment.*',
      },
      {
        text: 'Write in **Why it should still be paid**. This one is required — the placeholder notes that the requester and Accounts both see it.',
        shot: 'manager/cancellation-decline-sheet',
      },
      { text: 'Select **Decline and unfreeze**.' },
    ]),
    fields(
      [
        {
          name: 'Note',
          required: false,
          note: 'On the accept panel. Recorded in the history.',
        },
        {
          name: 'Why it should still be paid',
          required: true,
          note: 'On the decline panel. A blank one is refused, because a declined cancellation needs a reason the requester can read.',
        },
        {
          name: 'Reason',
          required: true,
          note: 'On the outright cancel panel, below. Always required.',
        },
      ],
      'The three cancellation panels and what each asks for.',
    ),
    note(
      `Declining unfreezes payment and puts the request back to \`approved\`. If the accounts team had
       parked the request on hold before the cancellation was asked for, that hold comes back with
       it — only Accounts can lift a hold.`,
    ),

    section('Cancelling a request nobody asked about'),
    prose(
      `You can also cancel an approved request on your own initiative, without anybody asking. The
       control is on the request itself and reads **Cancel with reason**; it appears while the
       request is \`approved\` and you are its approver.`,
    ),
    shot('manager/outright-cancel-sheet', 'Cancelling an approved request when the requester has not asked.'),
    prose(
      `The panel is honest about the situation — the sub-line reads *… did not ask for this.* — and
       the reason is required, not optional. Both the requester and the accounts team see what you
       wrote.

       Use it when you learn something the requester has not: the order was never placed, the vendor
       has been paid through another route, the project has been stopped.`,
    ),

    section('What each decision leaves behind'),
    table(
      ['Decision', 'Status afterwards', 'What Accounts can do'],
      [
        ['Accept the cancellation', '`cancelled`', 'Nothing. The request is closed and nothing can be paid against it.'],
        ['Decline the cancellation', '`approved`', 'Reserve and pay it as approved, subject to any hold that was already on it.'],
        ['Cancel outright', '`cancelled`', 'Nothing.'],
      ],
    ),
    note(
      `None of the three touches the budget. The variance grid counts payments, and a request that
       was cancelled before it was paid never contributed anything to spend. Cancelling frees no
       money, because none had been consumed.`,
    ),

    section('When a cancellation decision is refused'),
    table(
      ['What you see', 'Why'],
      [
        ['*Only the approver this request was sent to can decide its cancellation.*', 'You opened somebody else’s cancellation. The approver named on the request has to decide it.'],
        ['A message saying to say why it should still be paid', 'You declined without filling in the reason.'],
        ['A message saying a reason is required to cancel a request', 'You used **Cancel with reason** and left the box empty.'],
        ['A message saying there is no cancellation to decide on a request in that state', 'Somebody has already decided it. Reload and read the status pill.'],
        ['*… somebody else changed this request a moment ago — reload it and look again*', 'Two people acted on the request at once. Reload it; the first decision stands.'],
      ],
    ),

    faq([
      {
        q: 'The requester asked to cancel and I approved it by mistake.',
        a: `Approving is refused while a cancellation is pending — the message tells you to decide the
            cancellation first, and points out that declining it is what returns the request to
            approved.`,
      },
      {
        q: 'Can I cancel a request that has already been paid?',
        a: `No. Cancellation applies to an approved request that has not been settled. Once a payment
            is recorded the request is closed a different way — talk to the accounts team about
            [correcting or voiding the payment](page:accounts/correcting-a-payment).`,
      },
      {
        q: 'Who is told when I decide?',
        a: 'The requester and the accountant assigned to the request. The decision and any wording you gave are in the history for anybody else who looks.',
      },
    ]),
  ],
});
