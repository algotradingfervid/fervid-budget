import { page, prose, section, shot, steps, fields, table, note, warning, tip, faq } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'approving',
  title: 'Approving a request',
  summary: 'Approving in full, and approving a smaller amount than was asked for.',
  blocks: [
    prose(
      `Approving is what releases a request to the accounts team. Until you do it, nobody can reserve
       the request or pay a rupee against it; the moment you do, it appears in the payment queue.`,
    ),

    section('Approving'),
    steps([
      { text: 'Open the request from [the approvals queue](page:manager/the-approvals-queue) and read it.' },
      {
        text: 'Select **Approve ₹80,700.00** — the control carries the amount that was asked for, so you can see what you are agreeing to before the panel opens.',
        note: 'Nothing is written yet. This opens the approve panel.',
      },
      {
        text: 'Check **Amount approved**. It is filled in with the requested figure, and the line under it spells the same number out in words.',
        shot: 'manager/approve-sheet',
      },
      { text: 'Change the amount if you are approving less than was asked for. See below.' },
      { text: 'Add a **Note** if you want to record why. It is optional, and everybody on the request will see it.' },
      {
        text: 'Select **Approve request**.',
        note: 'You land back on the approvals queue, and the request has left the **To approve** tab.',
      },
    ]),

    section('The panel, field by field'),
    fields(
      [
        {
          name: 'Amount approved',
          required: true,
          note: 'Pre-filled with the amount requested. Commas are accepted; the ₹ sits outside the box. It may be reduced but never raised.',
        },
        {
          name: 'Note',
          required: false,
          note: 'Recorded in the history and visible to everyone who can see the request. Use it when you have approved less than was asked for.',
        },
      ],
      'The approve panel.',
    ),
    prose(
      `The panel also states, under the note box, what your decision sets off: Accounts will be able
       to reserve the request immediately, and the requester can no longer edit it.`,
    ),

    section('Approving less than was asked for'),
    prose(
      `The hint under the amount says it plainly — *You may approve a smaller amount than was asked
       for.* Type the lower figure into **Amount approved** before you confirm, and that figure is
       what the accounts team will pay and what the budget will carry.

       The request keeps its original amount as the amount requested; the approved figure sits beside
       it. On the request afterwards you see both, and the history line records what you approved.`,
    ),
    warning(
      `You cannot approve more than was requested. The refusal says so and names the figure you may
       approve up to. If more money is genuinely needed, the request has to be cancelled and a new
       one raised for the right amount.`,
      'The amount is a ceiling',
    ),
    tip(
      `Approving a reduced amount without a note leaves the requester guessing. One sentence in
       **Note** saves an email.`,
    ),

    section('When approval is refused'),
    table(
      ['What you see', 'Why', 'What to do'],
      [
        ['*Enter the amount you are approving.*', 'The amount box was empty, or held something that is not a number.', 'Type the figure in digits.'],
        ['A message naming a month and saying it is locked', 'The month the request is needed by has been closed.', 'Ask an administrator to reopen the month, or ask the requester to change the required-by date.'],
        ['*You do not have permission to perform this action.*', 'You are not the approver this request was routed to, or it is your own request.', 'Check the **Approver** field. If that person cannot act, [reassign it](page:manager/reassigning).'],
        ['A message saying a request in that state cannot be approved', 'Somebody else has already decided it, or the requester withdrew it.', 'Reload the request and read the status pill.'],
        ['A message about the cancellation decision', 'The requester has asked for this request to be cancelled, so it is frozen.', 'Decide the cancellation first. Declining it is what returns the request to approved.'],
      ],
    ),

    section('What happens next'),
    prose(
      `The status becomes \`approved\`, and the pill reads **Approved — awaiting payment**. The
       requester and the accounts team are told. The request can no longer be edited or withdrawn by
       its raiser — from here, undoing it means a cancellation, either because the requester
       [asks for one](page:requester/asking-to-cancel) or because you
       [take one yourself](page:manager/cancellation-requests).

       What happens after that belongs to Accounts: they claim the request, record the payment, and
       the request closes as \`completed\`. If they pay less than you approved, the difference comes
       back to you — see [Reviewing a shortfall](page:manager/shortfall-review).`,
    ),
    note(
      `Approving does not spend the budget. The variance grid counts money that has actually been
       paid, so an approved request shows up there only once Accounts settles it.`,
    ),

    faq([
      {
        q: 'I approved the wrong amount. Can I change it?',
        a: `Not by approving again. An approved request is closed to further approval. If the figure
            is wrong, cancel the request with a reason and ask for a new one — see
            [Deciding a cancellation request](page:manager/cancellation-requests).`,
      },
      {
        q: 'Can I approve and pay in one go?',
        a: 'No. The approver role holds no payment permissions at all; settlement is the accounts team’s job.',
      },
      {
        q: 'The requester says it is urgent. Does approving faster do anything?',
        a: `The urgent flag sorts the request to the top of queues and shows an **! Urgent** pill. It
            changes nothing about the decision itself.`,
      },
    ]),
  ],
});
