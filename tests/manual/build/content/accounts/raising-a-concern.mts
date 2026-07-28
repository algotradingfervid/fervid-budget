import { page, prose, section, subsection, shot, steps, note, warning, table, faq } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'raising-a-concern',
  title: 'Raising a concern',
  summary: 'Pushing back on a request you cannot pay as it stands.',
  blocks: [
    prose(
      `The accounts role has no reject and no return. Those are an approver’s decisions, and the
       product does not lend them to you — a request that reached your queue has already been
       agreed to by somebody with the authority to agree to it.`,
    ),
    prose(
      `What you have instead are three ways of saying “not like this”, and they differ in how much
       they block.`,
    ),

    table(
      ['If you want to', 'Use', 'What it blocks'],
      [
        ['Ask a question and wait for the answer before anybody pays', '**Put on hold**', 'Payment, for everybody, until Accounts lifts it'],
        ['Ask a question without stopping anything', 'A comment on the request', 'Nothing'],
        ['Step back from a request you claimed but should not pay', '**Release reservation**', 'Nothing — it returns to the open queue'],
      ],
    ),

    section('Blocking: put it on hold'),
    prose(
      `A hold is the strong form. Payment stops for everybody until somebody in Accounts lifts it,
       and the requester is asked, in your words, for whatever you need. Use it when paying as
       things stand would be wrong: bank details that do not match the vendor master, a missing
       invoice, a figure that has to be confirmed first.`,
    ),
    prose(
      `[Putting a request on hold](page:accounts/holding-a-request) is the whole procedure, both
       halves.`,
    ),

    section('Not blocking: say it in the conversation'),
    shot('accounts/request-on-hold', 'Every request carries one stream of history and conversation, and a box under it. What you post is visible to everybody who can see the request.'),
    prose(
      `Every request has one conversation, shared by everybody who can see the request — the
       requester, the approver and Accounts. The box under the history says so above the button:
       anyone who can see this request will see your comment.`,
    ),
    steps([
      { text: 'Open the request from the queue, from a notification, or from the payment.' },
      { text: 'Scroll to **History and conversation**, at the foot of the screen.' },
      { text: 'Type into **Add a comment** and select **Post comment**.' },
    ]),
    note(
      `The comment box is the only thing Accounts adds to a request. Documents on a request are
       attached by the requester, so if the wrong invoice is on it, ask for the right one in a
       comment or a hold.`,
    ),
    warning(
      `A comment blocks nothing. If the request is still approved and unclaimed, a colleague can
       take it and pay it while your question sits unanswered. When it genuinely must not be paid
       yet, place a hold instead.`,
      'A comment is not a brake',
    ),

    section('A concern raised at you'),
    prose(
      `The phrase also travels the other way, and this is the sense in which the product itself
       uses it. When you settle a request short of the approved amount, the approver reviews the
       shortfall — and one of their two decisions is **Raise a concern**.`,
    ),
    shot('manager/partial-review', 'The approver’s review of a shortfall. **Raise a concern** is the decision that does not close the request.'),
    prose(
      `Their concern comes to the accountant who recorded the payment. You are notified, their
       words land in the conversation on the request, and the request stays in \`partial_review\`
       until the two of you agree what happens next.`,
    ),
    subsection('What you can and cannot do about it'),
    table(
      ['You can', 'You cannot'],
      [
        ['Reply in the conversation on the request', 'Change the payment — it was sealed the moment it was saved'],
        ['Explain the deduction or the part payment in words', 'Reverse or refund the money, which is outside this version'],
        ['Point at the proof you attached when you settled it', 'Attach a further document to the payment, which stopped accepting them when it was saved'],
        ['Ask the requester to raise a fresh request for the balance', 'Decide the shortfall yourself — it is the approver’s'],
      ],
    ),
    prose(
      `The sheet the approver uses says the same thing in one line: raising a concern does not
       reverse anything, because the money has already left the bank. It keeps the request open so
       that a person, not the product, decides what follows. See [paying part of a
       request](page:accounts/paying-part-of-a-request) and [correcting or voiding a
       payment](page:accounts/correcting-a-payment).`,
    ),

    faq([
      {
        q: 'Can I send a request back to the requester to fix?',
        a: `Not directly. Returning for correction is an approver’s decision and only applies before
            approval. A hold is the accounts equivalent: the request stays approved, payment stops,
            and the requester is asked for what you need.`,
      },
      {
        q: 'I think the request should never have been approved.',
        a: `Say so on the request and hold it. Cancelling an approved request is a decision for the
            requester and their approver, not for Accounts.`,
      },
      {
        q: 'Who is told when I comment?',
        a: `Everybody who can see the request sees the comment on it. What arrives as a
            notification depends on the rules an administrator has configured — see
            [notifications](page:getting-started/notifications).`,
      },
      {
        q: 'The approver raised a concern and I still think the payment was right.',
        a: `Reply in the conversation with what you did and why. The decision is theirs and the
            request stays in review until they take it. Nothing about it changes the payment.`,
      },
    ]),
  ],
});
