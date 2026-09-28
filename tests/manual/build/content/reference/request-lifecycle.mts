import { page, prose, section, subsection, shot, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'reference',
  slug: 'request-lifecycle',
  title: 'The request lifecycle',
  summary: 'Every status a request can hold, and every route between them.',
  blocks: [
    prose(`A request holds exactly one status at a time. There are eleven of them, and this page is the
       complete list of the moves between them: what each move is called on screen, who is allowed
       to make it, and what the product checks before it agrees.

       Two things are commonly mistaken for statuses and are not. **On hold** is a flag on an
       approved request — the status stays \`approved\` while the hold lasts. And there is no
       draft: creating a request and sending it for approval are one press, so a request exists
       only from the moment it is with an approver. The database refuses the value \`draft\`
       outright.`),

    section(`The whole table`),

    prose(`Read a row as "from this status, this move takes it to that one". A move with the same
       status in both columns changes something else on the request and leaves the status alone.`),

    table(
      [`From`, `To`, `The move`, `Who may make it`, `What it requires`],
      [
        [`— (no request yet)`, `\`pending\``, `**Submit request** on the new-request form`, `Anybody holding \`request:create\``, `Every field the chosen type requires, and an approver who holds \`approval:approve\` and is not you. The request number and the submission time are written at this moment.`],
        [`\`pending\``, `\`approved\``, `**Approve**`, `The approver the request was sent to, holding \`approval:approve\``, `An approved amount above zero and no higher than the amount asked for. If the request carries a Needed by date, that month must not be locked.`],
        [`\`pending\``, `\`returned\``, `**Return for correction**`, `The approver the request was sent to, holding \`approval:return\``, `A comment. It is shown to the requester as the reason.`],
        [`\`pending\``, `\`rejected\``, `**Reject**`, `The approver the request was sent to, holding \`approval:reject\``, `A reason.`],
        [`\`pending\``, `\`withdrawn\``, `**Withdraw**`, `The person who raised it, holding \`request:withdraw\``, `Nothing else. This is the only status a request can be withdrawn from.`],
        [`\`pending\`, \`returned\`, \`cancellation_requested\` or \`partial_review\``, `the same status`, `**Reassign approval**`, `Somebody holding \`approval:reassign\` who is either the approver the request was sent to or also holds \`user:edit\``, `A reason, and a new approver who holds \`approval:approve\` and is neither the requester nor you. Only a request waiting on its approver can be reassigned.`],
        [`\`returned\``, `\`pending\``, `**Resubmit for approval**`, `The person who raised it, holding \`request:edit\``, `The corrected request has to pass the same checks the first submission did. **Save corrections** saves without resending, and **Edit every field** opens the full form.`],
        [`\`approved\``, `\`processing\``, `**Take for processing** on the payment queue, or **Record payment** on the request`, `Anybody holding \`reservation:reserve\``, `The request must be approved, unclaimed and not on hold. Two people pressing at once: the first one wins and the second gets a conflict screen.`],
        [`\`approved\``, `\`cancellation_requested\``, `**Request cancellation**`, `The person who raised it, holding \`request:cancel\``, `A reason and no payment already recorded. Payment freezes the moment it succeeds. It is allowed while the request is on hold; the hold is suspended meanwhile.`],
        [`\`approved\``, `\`cancelled\``, `**Cancel with reason**`, `The approver the request was sent to, holding \`approval:cancel\``, `A reason and no payment already recorded. Nobody needs to have asked first.`],
        [`\`cancellation_requested\``, `\`cancelled\``, `Accept the cancellation, from **Decide the cancellation**`, `The approver the request was sent to, holding \`approval:cancel\``, `A note is optional.`],
        [`\`cancellation_requested\``, `\`approved\``, `Decline the cancellation, from **Decide the cancellation**`, `The approver the request was sent to, holding \`approval:cancel\``, `A written reason. This is the only route back to \`approved\` from the frozen state — approving again will not do it.`],
        [`\`processing\``, `\`approved\``, `**Release** the reservation`, `The person holding it, with \`reservation:release\`. Releasing somebody else’s needs \`reservation:reassign\``, `A reason, and the tick confirming no payment was started.`],
        [`\`processing\``, `\`processing\``, `**Reassign** the reservation`, `Anybody holding \`reservation:reassign\``, `A reason, the same confirmation, and an active colleague who holds \`payment:process\`. The request never returns to the open queue in between.`],
        [`\`processing\``, `\`completed\``, `Record the payment and confirm it as settled`, `The person holding the reservation, with \`payment:create\` and \`payment:settle\``, `An amount no greater than the remaining approved balance, with an explanation if a short amount is marked fully settled, a payment date that is not in the future, not before the day of approval and not in a locked month, and a live head unless the request is recoverable.`],
        [`\`processing\``, `\`partial_review\``, `Record the payment and mark it a partial settlement`, `The same person, and also \`payment:mark_partial\``, `Everything above, plus a written reason for paying short.`],
        [`\`processing\``, `\`approved\``, `Record the payment as **Installment — pay the balance later**`, `The person holding the reservation, with \`payment:create\` and \`payment:settle\``, `An amount below the remaining approved balance. The payment is permanent, the reservation is released and the balance stays payable under the same approval. No notification is sent for an installment.`],
        [`\`partial_review\``, `\`approved\``, `**Keep balance payable**`, `The approver the request was sent to, holding \`approval:accept_partial\``, `A written **Payment plan or reason**, and some approved balance left to pay. The unpaid balance goes back to Accounts under the original approval.`],
        [`\`partial_review\``, `\`completed_partial\``, `**Accept and close**`, `The approver the request was sent to, holding \`approval:accept_partial\``, `A note is optional. The unpaid balance is written off for good.`],
        [`\`partial_review\``, `\`partial_review\``, `**Raise a concern**`, `The approver the request was sent to, holding \`approval:accept_partial\``, `A comment. It goes into the conversation and the request stays in review.`],
        [`\`rejected\``, `a new \`pending\` request`, `**Raise it again**`, `The person who raised it, holding \`request:reraise\``, `Only from \`rejected\`. It creates a fresh request with its own number; the rejected one stays rejected for ever.`],
      ],
      `Every move the product implements between request statuses.`,
    ),

    note(`Five statuses have no move out of them at all: \`rejected\`, \`withdrawn\`, \`cancelled\`,
       \`completed\` and \`completed_partial\`. Nothing reopens them. **Raise it again** is not an
       exception — it makes a second request and leaves the first one where it is.`, `Where the road ends`),

    section(`The ordinary path, end to end`),

    prose(`Most requests take five steps and nothing else: submitted, approved, taken for processing,
       paid, closed. This is that path.`),

    subsection(`1. Submitted`),

    prose(`You fill in the form and press **Submit request**. There is no save-and-finish-later: the
       request is created already in \`pending\`, it takes its number at that instant, and it
       appears in the queue of the approver you chose. The badge reads **Awaiting approval** and
       the line beside it names the person it is waiting on.`),

    shot('manager/request-pending', 'A pending request as its approver sees it. The four decisions sit at the bottom: **Reject**, **Return for correction**, **Approve ₹80,700.00** and **Reassign approval**.'),

    subsection(`2. Approved`),

    prose(`The approver presses **Approve**. The amount on that sheet can be lowered but never raised —
       the approved amount is the ceiling Accounts may pay to, so raising it would create an
       obligation nobody asked for. The answer to needing more money is a new request.

       Or they press **Return for correction** instead. That is not a rejection: the request goes
       back to \`returned\`, keeps its number and its whole history, and the requester gets a
       correction form with the approver's comment above it. **Save corrections** keeps it with
       them; **Resubmit for approval** sends it back and returns the status to \`pending\`.`),

    shot('requester/detail-returned', 'A returned request. The banner carries the approver’s words, and the line under it says plainly that returned is not rejected.'),

    prose(`Once approved, the request is locked to the requester. It can no longer be edited or
       withdrawn; the only thing they may do is ask for it to be cancelled.`),

    shot('requester/detail-approved', 'An approved request. The banner spells out the lock, and the one control left is **Request cancellation**.'),

    subsection(`3. Taken for processing`),

    prose(`Somebody in Accounts presses **Take for processing** from the payment queue, or **Record
       payment** on the request itself. That claim is the whole point of the \`processing\` status:
       while one person holds it, nobody else can record a payment against it, so the same invoice
       cannot be paid twice. The badge the requester sees changes to **With Accounts — taken by**
       and the accountant's name.`),

    shot('requester/detail-processing', 'A claimed request. The badge names who took it, and the history shows the reservation as its own line: "Reserved request for processing".'),

    subsection(`4. Paid`),

    prose(`The accountant fills in the payment and confirms it. If the figure matches what was approved
       — or is short and they confirm the request as settled anyway — the request closes as
       \`completed\` in the same transaction that writes the payment. The two either both happen or
       neither does.`),

    shot('requester/detail-completed', 'A completed request. Payment outcome compares what was approved with what was paid, and the difference below it.'),

    subsection(`5. Or paid short, and sent back to the approver`),

    prose(`An installment returns the request to Approved for the remaining balance. If Accounts
       instead asks the manager to accept a shortfall, the request moves to Partial — manager
       review. The approver can Keep balance payable, Accept and close the permanent write-off, or
       Raise a concern. While a concern waits for Accounts to answer, the badge reads **Partial —
       under discussion**; the status is still \`partial_review\`.`),

    shot('manager/partial-review', 'A shortfall waiting on its approver: approved against paid, what is still owed, and the three decisions at the bottom — **Raise a concern**, **Accept and close** and **Keep balance payable**.'),

    warning(`None of these decisions moves money or amends the recorded payment. Keeping the balance
       payable permits Accounts to record a later installment. Accepting writes the balance off
       permanently. Raising a concern reverses nothing — the money has already left.`),

    section(`The cancellation branch`),

    prose(`An approved request cannot be taken back on the requester’s own say-so, because Accounts may
       already be acting on it. The requester asks instead, and the request moves to
       \`cancellation_requested\`. Payment freezes at that moment: no accountant can reserve or pay
       it while the approver is deciding.`),

    shot('requester/detail-cancellation-asked', 'A cancellation the requester has asked for. The banner names who has to decide and repeats the reason given.'),

    prose(`The approver accepts, and the request is \`cancelled\` for good. Or they decline with a
       written reason, and it returns to \`approved\` and becomes payable again. Declining is the
       only route back — an approver who tries to approve the frozen request instead is refused and
       told to decide the cancellation first.`),

    section(`Holds, which are not a status`),

    prose(`When Accounts needs something answered before they can pay — bank details that do not match
       the vendor master, an invoice number that does not exist — they put the request on hold with
       a question. The status stays \`approved\`. What changes is that the request drops out of the
       takeable set, so nobody can claim it, and the badge on every screen reads **On hold**
       instead.`),

    table(
      [`Move`, `Control`, `Who`, `What it requires`],
      [
        [`Pause an approved request`, `**Put on hold**`, `Anybody holding \`payment:hold\` — the accounts team, and administrators`, `A written reason. The request must be approved, not already held, and claimed by nobody: a request somebody has taken for processing has to be released first.`],
        [`Lift the pause`, `**Release hold**`, `Anybody holding \`payment:hold\` — the accounts team, and administrators`, `Nothing. Lifting the hold is the answer to the question, so it does not ask for a second one.`],
      ],
      `The hold, and the one grant that governs both directions of it.`,
    ),

    note(`Only Accounts lifts a hold. A reader without \`payment:hold\` who opens a held request is
       told so on the request itself: "Only Accounts can take this off hold."`),

    tip(`If a hold is in force when the requester asks for a cancellation, the pause is suspended
       rather than thrown away. Decline the cancellation and the hold comes back with the request,
       still carrying the question nobody answered.`),

    section(`The reservation, and giving it up`),

    prose(`A reservation is a claim, not a decision, and it is the only thing standing between two
       accountants and a duplicate payment. There is no automatic release. A request sits in
       \`processing\` until the person holding it either records a payment or gives the claim up.

       **Release** puts it back in the open queue as \`approved\`. **Reassign** hands it straight
       to a named colleague without it ever becoming takeable in between. Both demand a reason —
       the requester and the approver are told why their money stopped moving — and both demand the
       tick confirming no payment was started.

       A reservation held for more than a day is flagged as stale on the payment queue. That one-day
       mark is fixed. Nothing is released automatically when it passes: a transfer that has been
       open a day may well be moving through a bank portal. The product only puts the choice in
       front of a person. Separately, the stale-reservation reminder notification follows the number
       of days your administrator sets on the [Configuration](page:admin/configuration) screen.`),

    faq([
      { q: `Can I withdraw a request that was returned to me?`, a: `No. Withdrawal is only possible from \`pending\`. A returned request can be corrected and resubmitted, or left where it is — there is no route from \`returned\` to \`withdrawn\`.` },
      { q: `Can a rejected request be reopened?`, a: `No. Rejection is final by design. **Raise it again** copies the request into a new one with a new number, which starts the lifecycle over from \`pending\`. See [After a rejection](page:requester/after-a-rejection).` },
      { q: `Why can my approver not approve more than I asked for?`, a: `Because the approved amount is the authority to pay, not a note. Accounts may not pay above it, so raising it on one signature would create an obligation nobody requested. The product refuses and suggests cancelling and raising a new request instead.` },
      { q: `What is the difference between \`completed\` and \`completed_partial\`?`, a: `\`completed\` means the request was settled and closed by Accounts. \`completed_partial\` means less was paid than was approved and the approver accepted the shortfall. The two are deliberately distinct so a written-off balance stays visible for ever.` },
      { q: `A request is approved but nobody can pay it. Why?`, a: `Either it is on hold, or a cancellation has been asked for and payment is frozen while the approver decides. Both show a banner on the request saying which. See [Reservations and conflicts](page:accounts/reservations-and-conflicts).` },
    ]),
  ],
});
