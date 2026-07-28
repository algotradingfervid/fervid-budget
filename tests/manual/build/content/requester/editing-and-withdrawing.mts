import { page, prose, section, subsection, shot, steps, note, warning, table, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'editing-and-withdrawing',
  title: 'Changing or withdrawing a request',
  summary: 'What you can still edit after submitting, and how to take a request back.',
  blocks: [
    prose(
      `A submitted request is not sealed straight away. While it is still waiting on your approver, or
       has been sent back to you, it is yours to change — or to take back altogether. Once it is
       approved, both doors close.`,
    ),

    section('When you can still change it'),
    table(
      ['Status', 'Edit', 'Withdraw', 'Instead'],
      [
        ['`pending` — awaiting approval', 'Yes', 'Yes', '—'],
        ['`returned` — sent back to you', 'Yes', 'No', 'Correct and resubmit, then withdraw if you must'],
        ['`approved` and later', 'No', 'No', '[Ask for it to be cancelled](page:requester/asking-to-cancel)'],
        ['`rejected`', 'No', 'No', '[Raise it again](page:requester/after-a-rejection)'],
        ['`withdrawn`, `cancelled`', 'No', 'No', 'Raise a new request'],
      ],
    ),
    prose(
      `Only the person who raised a request may edit it. An approver who wants something changed
       returns the request instead, which is a different act with its own line in the history.`,
    ),

    section('Editing'),
    shot('requester/detail-pending', 'A request awaiting approval. Edit request and Withdraw sit in the bar at the foot.'),
    steps([
      { text: 'Open the request from **My requests**.' },
      { text: 'Select **Edit request** at the foot of the screen.' },
      { text: 'Change what needs changing.' },
      {
        text: 'Select **Save and notify**, which carries your approver’s name.',
        note: 'You land back on the request, with the change recorded in its history.',
      },
    ]),
    shot('requester/edit-form', 'The edit form. The blue banner at the top says what saving will do before you touch anything.'),
    prose(
      `The banner reads **Editing tells** your approver **again** — *Every change is recorded in the
       history, notifies your approver, and restarts the 3-day reminder clock. Change the approver and
       it moves to that person instead.* The number of days is your organisation's setting.`,
    ),

    subsection('What the edit form lets you change'),
    prose(
      `Nearly everything: the short title, the project and head, the amount, the needed-by date, the
       urgency flag and its reason, the type's own fields — vendor, invoice number, invoice date,
       expense date, reason for the advance — the purpose, and the approver. You can also add another
       document; the uploader is labelled **Add another document** and lists whatever is already
       attached above it.`,
    ),
    warning(
      `Two things you cannot change: the request **type** and the **treatment**. Neither control is on
       this form. If either is wrong, withdraw the request and raise it again under the right one.`,
    ),
    prose(
      `Changing the **Approver** moves the request to that person and tells them, exactly as the hint
       under the control says. The request leaves the first approver's queue at that moment.`,
    ),
    prose(
      `The bar at the foot carries **Discard changes**, which returns you to the request without
       saving, and **Save and notify**, carrying your approver's name. The note beside them is the shortest summary
       of the whole screen: *Every change is recorded in the history.*`,
    ),
    note(
      `On a request your approver returned to you, the primary screen is the correction form rather
       than this one, and its buttons are **Save corrections** and **Resubmit for approval**. See
       [When a request comes back to you](page:requester/when-a-request-is-returned).`,
    ),

    section('Withdrawing'),
    prose(
      `Withdrawing takes a request out of your approver's queue without their deciding it. Use it when
       the spend is no longer needed, when you raised it under the wrong type, or when the request
       is wrong enough that starting again is cleaner than correcting it.`,
    ),
    steps([
      { text: 'Open the request. It must still say **Awaiting approval**.' },
      {
        text: 'Select **Withdraw**.',
        note: 'There is no confirmation step. The request is withdrawn as soon as you press it.',
      },
    ]),
    shot('requester/detail-withdrawn', 'A withdrawn request. The pill reads Withdrawn, the sentence beside it says who did it, and the action bar is empty.'),
    prose(
      `The status becomes **Withdrawn** and the waiting sentence reads *Withdrawn by the requester*.
       The request moves to the **Closed** tab, the withdrawal is written into the history, and your
       approver is told — their queue item has disappeared without a decision, so they are owed
       the reason it went.`,
    ),
    warning(
      `A withdrawal cannot be undone. There is no route back from \`withdrawn\` to any other state:
       the request is closed for good, and getting the money instead means raising a new request with
       a new number.`,
      'Withdrawing is final',
    ),

    section('When the product refuses'),
    table(
      ['What you see', 'Why'],
      [
        ['`This request can no longer be edited. It is approved and locked; ask for it to be cancelled instead.`', 'The approver has already decided it.'],
        ['`This request can no longer be edited. A cancellation is already pending on it.`', 'You have asked for it to be cancelled; nothing moves until that is decided.'],
        ['`This request can no longer be edited. A rejected request is final — raise a new one.`', 'Rejection is the end of that request.'],
        ['`Only the person who raised a request may edit it.`', 'You are looking at somebody else’s request.'],
        ['`Only an approved request is cancelled this way. It is still awaiting approval; withdraw it instead.`', 'Cancellation is for approved requests; a pending one is withdrawn.'],
      ],
      'The wording is the product’s own.',
    ),
    prose(
      `The controls follow the same rules, so in practice you rarely meet these messages: **Edit
       request** and **Withdraw** are not drawn at all on a request that cannot take them.`,
    ),

    faq([
      {
        q: 'I withdrew the wrong request.',
        a: `Raise it again from scratch. Nothing revives a withdrawn request, and there is deliberately
            no undo — the record of the withdrawal is meant to stay true.`,
      },
      {
        q: 'Can I edit a request while my approver is looking at it?',
        a: `Yes, right up until they decide it. They are notified again when you save, and the figures
            they see refresh — which is exactly why the notification is not optional.`,
      },
      {
        q: 'I want to change the amount on an approved request.',
        a: `You cannot. Ask for the request to be cancelled and raise a new one for the right amount —
            approving a larger figure than was asked for is not something the product allows.`,
      },
      {
        q: 'Does editing lose my attachments?',
        a: `No. Everything already attached stays, and the edit form lists it above the uploader.`,
      },
    ]),
  ],
});
