import { page, prose, section, shot, steps, note, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'holding-a-request',
  title: 'Putting a request on hold',
  summary: 'Parking an approved request with a reason, and taking it off hold again.',
  blocks: [
    prose(
      `A hold is what you use when a request is approved but you cannot pay it as it stands —
       the bank details do not match the vendor master, the invoice attached is the wrong one, a
       figure needs confirming. It is not a rejection and not a decision. It parks the request and
       asks the requester a question.`,
    ),
    prose(
      `Only Accounts can place a hold, and only Accounts can lift one (an administrator can do
       both too). While a request is held, nobody in Accounts — including you — can claim or pay
       it.`,
    ),

    section('Placing a hold'),
    steps([
      {
        text: 'Open the request. Select **Put on hold** at the foot of the screen.',
        note: 'The control appears only on an approved request that nobody has claimed and that is not already held. If you hold the claim yourself, release it first — the first question at the end of this page explains how.',
      },
      {
        text: 'Under **What do you need from the requester**, write your question.',
        note: 'It is required, and the placeholder is the point: they see this exactly as you write it.',
        shot: 'accounts/hold-sheet',
      },
      {
        text: 'Select **Put on hold**.',
        note: 'The requester is asked to answer, and is notified that the request has been held.',
      },
    ]),
    tip(
      `Ask one specific question. The text you type is the entire message the requester gets, and
       it is the text everybody else sees quoted on the request from then on.`,
    ),

    section('What a hold changes'),
    shot('accounts/request-on-hold', 'A held request. The banner quotes the reason, and the action bar offers **Keep on hold** and **Release hold**.'),
    table(
      ['What', 'While the hold is on'],
      [
        ['The status', 'Unchanged underneath — the request is still approved — but every screen shows an `On hold` badge instead, because the request cannot be paid.'],
        ['Who it waits on', 'The requester. The line beside the badge reads “Waiting on” with their name and “to clarify”.'],
        ['The payment queue', 'The request leaves the takeable set and appears on the **On hold** tab instead.'],
        ['The reason', 'Quoted in a banner headed **This request is on hold**, and written into the history as a line with your name, “put the request on hold:” and the reason.'],
      ],
    ),
    shot('accounts/queue-hold', 'The On hold tab of the payment queue. **Read reply** on a row opens the request.'),
    note(
      `The requester answers in the conversation on the request, in the same comment box everybody
       else uses, or with a file: while the hold is on, the banner offers them **Add a document**
       and **Attach it**, which is the only way to add a document to an approved request. Nothing
       about their reply lifts the hold — you read it and decide.`,
    ),

    section('Taking a request off hold'),
    prose(
      `On a held request the action bar carries two controls. **Keep on hold** leaves things as
       they are — it is there so that reading an answer and deciding it is not enough is a
       deliberate act rather than an unfinished one. **Release hold** lifts the hold.`,
    ),
    steps([
      { text: 'Open the request, either from the **On hold** tab or from a notification.' },
      { text: 'Read the reply in **History and conversation**.' },
      {
        text: 'Select **Release hold** if the answer is good enough.',
        note: 'No reason is asked for. The hold was the thing that needed explaining; lifting it is answering the question.',
      },
      {
        text: 'The `On hold` badge goes, the request shows as approved again, and it reappears on the queue’s first tab for anybody to claim.',
        note: 'The requester is told the hold is lifted and that nothing further is needed from them.',
      },
    ]),

    section('Hold, or release, or neither'),
    prose(
      `Three tools look similar from a distance. They are not interchangeable.`,
    ),
    table(
      ['Use', 'When', 'Effect'],
      [
        ['**Put on hold**', 'You need something from the requester before anybody can pay it', 'Blocks payment for everybody until Accounts lifts it'],
        ['**Release reservation**', 'You claimed it but somebody else should pay it', 'Hands it back to the open queue — see [taking a request for processing](page:accounts/claiming-a-request)'],
        ['A comment', 'You want to ask something without blocking anything', 'Adds to the conversation and blocks nothing — see [raising a concern](page:accounts/raising-a-concern)'],
      ],
    ),

    faq([
      {
        q: 'I cannot find **Put on hold** on a request.',
        a: `Either it is not approved, or it is already on hold, or somebody has it claimed. Placing
            a hold works only on an approved request that is unclaimed and not already held. If you
            hold the claim yourself, release it first — give the reason and tick the confirmation —
            then open the request and select **Put on hold** straight away, because while it is
            released a colleague could take it. The stale-reservation screen’s **Put it on hold**
            choice takes you to the same release screen. A claim held by a colleague is theirs to
            release; you cannot hold that request until they do.`,
      },
      {
        q: 'Can the requester take their own request off hold by replying?',
        a: `No. Only Accounts lifts a hold. Their reply appears in the conversation and the request
            stays held until somebody in Accounts presses **Release hold**.`,
      },
      {
        q: 'Does a hold stop the approver doing anything?',
        a: `A hold is about payment: it takes the request out of the payment queue’s takeable set.
            The approval has already been given by the time you can place one. The approver can
            still cancel the request outright, and the requester can still ask for it to be
            cancelled.`,
      },
      {
        q: 'The request needs to be cancelled, not held.',
        a: `Cancelling an approved request is not something Accounts can do. The requester asks for
            a cancellation and their approver decides it — see [asking to cancel an approved
            request](page:requester/asking-to-cancel). A held request can be sent for cancellation
            too: the hold is set aside while the approver decides, and comes back with your
            original question if they decline.`,
      },
    ]),
  ],
});
