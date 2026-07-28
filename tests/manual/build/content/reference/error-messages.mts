import { page, prose, section, subsection, shot, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'reference',
  slug: 'error-messages',
  title: 'Messages and refusals',
  summary: 'What the product says when it will not do something, and what to do about it.',
  blocks: [
    prose(
      `A refusal arrives in one of three shapes, and knowing which one you are looking at is most of the
       diagnosis.`,
    ),
    table(
      ['Shape', 'What it looks like', 'What it means'],
      [
        [
          'The error page',
          'A large status number, a heading, one sentence, a request ID, and two buttons — **Return to dashboard** and **Go back**.',
          'The whole action was refused before anything happened. There is no sidebar on this page, deliberately.',
        ],
        [
          'A red band at the top of a form',
          'The screen you were on redraws with a message above it and everything you typed still in place.',
          'One field or one rule was wrong. Fix it and submit again — nothing was saved and nothing was lost.',
        ],
        [
          'A conflict screen',
          'A coloured banner, the request it is about, and a **What you can do** list.',
          'Somebody else got there first, or the request is not in a state you can act on. Nothing was saved and no payment was created.',
        ],
      ],
      'The three ways the product says no.',
    ),
    tip(
      `Every error page prints a request ID under the message. If you are reporting a problem, quote it
       — it is the one thing that takes an administrator straight to the log line for your exact
       attempt.`,
    ),

    section('403 — you may not do this'),
    prose(
      `The most common refusal, and it has two quite different causes. The first is the permission
       itself: your roles do not carry the grant the route asks for. Every screen and every route is
       gated on the same permission its sidebar entry is, so this is what you get if you type an
       address for a screen your menu does not list.`,
    ),
    shot(
      'shared/forbidden',
      'The 403 page. The wording is the same for every permission refusal, which is why the request ID underneath matters.',
    ),
    prose(
      `**What to do.** Nothing you can fix yourself. Ask an administrator for the role that carries the
       permission — [The permission matrix](page:reference/permissions-matrix) says which one that is.`,
    ),

    subsection('Refused because of who you are on this request'),
    prose(
      `The second cause is not about permissions at all. Some acts belong to a seat rather than to a
       grant, and holding every permission in the product does not put you in somebody else's seat.
       These refusals name the reason, so they are easy to tell apart from the generic one.`,
    ),
    table(
      ['What the product says', 'Why', 'What to do'],
      [
        [
          '"Only the person who raised a request may edit it."',
          'Correcting a request is the requester’s act. An approver who wants a change returns the request instead, which is a different verb with its own audit line.',
          'Return the request for correction, or ask its requester to make the change.',
        ],
        [
          '"Only the person who raised a request may ask for it to be cancelled."',
          'Asking is the requester’s act, whoever else holds `request:cancel`.',
          'An approver with `approval:cancel` can cancel the request outright with a reason instead.',
        ],
        [
          '"Only the approver this request was sent to can decide its cancellation."',
          'The decision belongs to the named approver, not to everybody holding the permission.',
          'If that approver cannot act, reassign the approval to somebody who can.',
        ],
        [
          '"Only the person holding this reservation can release it."',
          'Releasing a claim you do not hold is taking work off a colleague, which needs `reservation:reassign` — an administrator permission.',
          'Ask an administrator to reassign the reservation.',
        ],
        [
          '"You do not have permission to record a partial payment."',
          'Settling short of the approved amount is its own decision and needs `payment:mark_partial`.',
          'Record the payment as settled if that is what it is, or ask a colleague who holds the grant.',
        ],
      ],
      'Refusals that name a seat rather than a permission.',
    ),

    subsection('State refusals that also answer 403'),
    prose(
      `A few refusals are about the state of the record rather than about you. They keep their own
       sentence rather than falling back to the permission wording, because sending somebody to an
       administrator for a grant they already hold helps nobody.`,
    ),
    table(
      ['What the product says', 'When'],
      [
        ['"This request is on hold"', 'You tried to take a held request for processing.'],
        ['"Someone else is already processing this request"', 'Somebody else holds the claim.'],
        ['"Only an approved request can be taken for processing"', 'The request is in some other status entirely.'],
        ['"Reserve this request before recording its payment"', 'A payment was posted for a request you do not currently hold.'],
        ['"Only the assignee may release this request"', 'You tried to release somebody else’s claim without `reservation:reassign`.'],
        ['"Only a processing request can be released"', 'There is nothing claimed to release.'],
        ['"The reservation changed while you were deciding"', 'The claim moved between the screen loading and your press.'],
        ['"Reservation was lost before settlement"', 'The claim moved between the confirmation sheet and the save. No payment was written.'],
        ['"Only an approved, not-already-held request can be held"', 'The request is not approved, or is already on hold.'],
        ['"Request is not on hold"', 'There is no hold to lift.'],
        ['"Only a partial-review request can be accepted"', 'The shortfall has already been decided.'],
        ['"Only a partial-review request can receive a concern"', 'Same, for the other decision.'],
      ],
      'State conflicts that carry their own reason.',
    ),

    section('404 — not found, and not-found-on-purpose'),
    prose(
      `Two different sentences answer with 404, and they mean different things.`,
    ),
    table(
      ['What the product says', 'What it means'],
      [
        [
          '"That page does not exist. It may have moved, or it may not be built yet."',
          'The address itself is not a screen this product serves. Check the URL.',
        ],
        [
          '"The requested record was not found."',
          'The record does not exist — or it exists and your data scope does not reach it. These two are made deliberately indistinguishable.',
        ],
      ],
      'The two 404 messages.',
    ),
    shot(
      'shared/not-found',
      'The 404 page. "The requested record was not found." is also what an out-of-scope record answers.',
    ),
    warning(
      `If a colleague sends you a link to their request and you get this page, you have almost certainly
       not found a bug. A Requester's data scope is \`own\`, so anybody else's request is invisible to
       them — and it answers 404 rather than 403 so that nobody can walk the numbers and learn which
       requests exist. Ask the person to tell you what you need, or ask an administrator for a wider
       scope.`,
      'Why "not found" sometimes means "not yours"',
    ),
    note(
      `The same rule applies to the payments ledger: a payment whose request lies outside your scope
       answers "The requested record was not found." even though you hold \`payment:view\`. And opening
       the partial-review screen for a request with no payment recorded yet says "No payment has been
       recorded against this request yet, so there is nothing to review."`,
    ),

    section('409 — somebody else got there first'),
    prose(
      `The whole point of claiming a request before paying it is that two people cannot pay the same
       invoice. Losing that race is not treated as an error; it is a screen that tells you what actually
       happened and what to do next. All three versions end with the same reassurance: nothing you typed
       has been saved, and no payment was created.`,
    ),

    subsection('Somebody else has it'),
    prose(
      `The heading names the person holding the claim, the badge below it reads **Processing** followed
       by their name, and the line beside it says when they took it.`,
    ),
    shot(
      'accounts/conflict-taken',
      'The taken conflict. The banner names the holder; **What you can do** offers the queue and a read-only view of the request.',
    ),
    prose(
      `**What to do.** Take a different request. If this one really has to be yours, and you hold
       \`reservation:reassign\`, the list offers **Ask for it to be reassigned** — an administrator
       permission, so most accountants will not see that entry.`,
    ),

    subsection('It is on hold'),
    prose(
      `Nobody holds a reservation. The request has been parked with a question, and the question is
       quoted in the banner.`,
    ),
    shot(
      'accounts/conflict-on-hold',
      'The hold conflict. The accountant’s own question is repeated back, and the badge reads **On hold** rather than naming a holder.',
    ),
    prose(
      `**What to do.** Payment is blocked until Accounts lifts the hold, and the requester is the one
       being waited on. **See what is on hold** takes you to the hold tab of the queue, where every held
       request offers **Read reply**.`,
    ),

    subsection('It is not in a state that can be paid'),
    prose(
      `The request is not approved. Most often it has already been paid, but a cancelled or frozen
       request lands here too. The banner names the status it is actually in.`,
    ),
    shot(
      'accounts/conflict-already-paid',
      'A request that was already completed. The badge reads **Completed** and the waiting line reads "Nothing pending".',
    ),
    prose(
      `**What to do.** Nothing. If you had a payment to record against it, the payment is already there
       — open the request read-only and follow it to the payment.`,
    ),
    note(
      `Double-pressing the confirm button does not create a second payment. If a settlement is submitted
       for a request that already has one, the product takes you to the payment that exists rather than
       showing you a failure.`,
    ),

    subsection('Other 409s'),
    table(
      ['What the product says', 'When', 'What to do'],
      [
        [
          '"This request is not reserved by anyone."',
          'You opened the release or the reserved-too-long screen for a request nobody has claimed.',
          'Go back to the queue. If the request is approved and unclaimed, take it for processing.',
        ],
        [
          '"This month is locked and cannot be changed."',
          'A write landed in a month an administrator has closed.',
          'Ask an administrator to unlock the month, or use a date inside an open one.',
        ],
      ],
      'Conflicts that are not about reservations.',
    ),

    section('Locked months'),
    prose(
      `Locking a month makes its budgets and its payments read-only. Three refusals come out of that, and
       they read differently because they arrive at different moments.`,
    ),
    table(
      ['Where', 'What the product says', 'What to do'],
      [
        [
          'Approving a request whose Needed by date falls in a locked month',
          'The month, then "is locked, so this request cannot be approved for payment in it. Reopen the month, or ask for the required-by date to be changed."',
          'The check happens before the approval, not after — so the obligation is never created. Ask for the month to be reopened, or for the date to be changed.',
        ],
        [
          'A screen that redraws with the message in place, such as the budget editor or the settlement confirmation',
          '"This month is locked. Unlock it with a reason before changing budgets or payments."',
          'Everything you typed is still on the screen. Unlocking is an administrator act and demands a written reason.',
        ],
        [
          'A refusal that takes you to the error page',
          '"This month is locked and cannot be changed."',
          'The same remedy; only the delivery differs.',
        ],
      ],
      'The three faces of a locked month.',
    ),
    note(
      `Recording a payment also refuses a date in the future: "paid on cannot be a future date — money
       cannot have left the bank after today". The payment date records when money left the bank, so a
       date after today records something that has not happened.`,
    ),

    section('Expired forms'),
    prose(
      `If you leave a form open for a long time, or your session is replaced in another tab, submitting
       it answers 403 with:`,
    ),
    prose(`"Your form session expired. Refresh the page and try again."`),
    prose(
      `**What to do.** Exactly what it says. Reload the page, retype what you had, and submit again.
       Nothing was written. This is not a permission problem and adding roles will not stop it.`,
    ),
    warning(
      `Use **Go back** and the browser, not a second submit. Pressing submit again on the stale page
       produces the same refusal, because the page itself is what has gone out of date.`,
    ),
    note(
      `Two neighbours of the same check: a form carrying more than the size limit answers "The submitted
       form is too large.", and a form the server cannot parse at all answers "The submitted form could
       not be read."`,
    ),

    section('Form validation'),
    prose(
      `A form that fails a rule comes straight back with the message in a red band at the top and every
       field still holding what you typed. Losing a page of typing to one bad field is the cruellest
       thing a form can do, so the product does not do it.`,
    ),
    prose(
      `These messages are written as fragments of a sentence rather than as headlines, so they read
       exactly as quoted here — lower case and all.`,
    ),
    table(
      ['What the product says', 'What it wants'],
      [
        ['"a positive amount is required"', 'An amount above zero. Commas and the ₹ sign are accepted.'],
        ['"enter the amount you are requesting"', 'The amount field could not be read as money at all.'],
        ['"a short title is required — it is what your approver sees in their list"', 'A title. It is the line your approver reads in their queue.'],
        ['"purpose is required"', 'The purpose field.'],
        ['"choose an approver"', 'Somebody to send it to.'],
        ['"you cannot approve your own request — choose another approver"', 'Anybody but you. Your own name is never in the list.'],
        ['"that person cannot approve requests — choose one of the approvers offered"', 'Somebody who actually holds `approval:approve`.'],
        ['"project and head are required for this type"', 'Both, for a budget expense.'],
        ['"that budget head belongs to a different project — choose a head under the project you named"', 'A head under the project you picked.'],
        ['"that budget head has been retired — choose a head that is still open"', 'A live head. Retired ones are kept for history only.'],
        ['"choose a vendor from the vendor master"', 'A vendor row, not free text.'],
        ['"that vendor is no longer active — choose an active vendor"', 'An active vendor.'],
        ['"the invoice number is required" / "the invoice date is required"', 'Both, on a vendor invoice.'],
        ['"the expense date is required"', 'When you spent the money, on a reimbursement.'],
        ['"say what the advance is for"', 'A reason, on a vendor advance.'],
        [
          '"a vendor invoice is a budget expense" / "a vendor advance is a budget expense" / "reimbursement is a budget expense"',
          'The recoverable treatment on a type that cannot carry it. Only an employee advance may be recoverable.',
        ],
        ['"choose a recoverable category"', 'A category, once the treatment is recoverable.'],
        ['"expected return date is required for recoverables"', 'When you expect the money back.'],
        ['"repayment or refund terms are required for recoverables"', 'How it comes back.'],
        ['"this recoverable category always belongs to a project"', 'A project, because that category demands one.'],
        ['"this recoverable category needs a counterparty company"', 'The other party, because that category demands one.'],
        ['"say why this is urgent"', 'A reason, when your organisation has urgency set to require one.'],
        ['"attach a supporting document, or say why you cannot"', 'A document, or a written explanation. The product never hard-blocks here.'],
      ],
      'The messages the request form produces, and what each one is asking for.',
    ),

    subsection('Decisions and settlements'),
    table(
      ['What the product says', 'When'],
      [
        ['"Enter the amount you are approving."', 'The approve sheet was submitted with an unreadable amount.'],
        ['"you cannot approve more than the …" and the amount requested', 'The approved amount was raised above what was asked for. Approve up to it, or cancel and ask for a new request.'],
        ['"a comment is required to return a request"', 'Returning without a reason.'],
        ['"a reason is required to reject a request"', 'Rejecting without a reason.'],
        ['"a reason is required to cancel a request"', 'Cancelling outright without a reason.'],
        ['"say why it should be cancelled"', 'Asking for a cancellation without a reason.'],
        ['"say why it should still be paid"', 'Declining a cancellation without a reason.'],
        ['"this request is waiting on your cancellation decision — decide that first, and declining it is what returns it to approved"', 'Trying to approve a frozen request instead of deciding the cancellation.'],
        ['"only a pending request can be reassigned"', 'Reassigning the approval on anything else.'],
        ['"only a rejected request can be re-raised"', 'Re-raising something that was not rejected.'],
        ['"choose payment settled or partial settlement"', 'A settlement was posted without saying which it is.'],
        ['"a reason is required for a partial settlement"', 'Paying short without explaining why.'],
        ['"… is more than the approved …"', 'The payment exceeds the approved amount. To pay more, cancel the request and raise a new one.'],
        ['"confirm that no payment was initiated before releasing"', 'Releasing a claim without ticking the confirmation.'],
        ['"a reason is required so the requester and approver know why"', 'Releasing a claim without a reason.'],
        ['"a reason is required when a reservation changes hands"', 'Reassigning a claim without a reason.'],
        ['"That person cannot work the Accounts queue."', 'The reassignment target holds no `payment:process`, so the request would be stranded.'],
        ['"a hold reason is required"', 'Putting a request on hold without saying what you need.'],
        ['"a concern comment is required"', 'Raising a concern on a shortfall with nothing written.'],
        ['"comment cannot be empty"', 'Posting an empty comment.'],
      ],
      'Messages from the decision and settlement screens.',
    ),

    subsection('Acting on a request that has moved on'),
    prose(
      `Two people working the same request at the same time is normal, and the product does not resolve
       it by guessing. If the request changed between the screen loading and your press, you are told
       so rather than having your decision applied to a request that is no longer the one you read:`,
    ),
    prose(`"somebody else changed this request a moment ago — reload it and look again"`),
    prose(
      `The same family covers acting on the wrong status. Trying to edit a request that is no longer
       editable says "This request can no longer be edited." followed by the reason — "It is approved
       and locked; ask for it to be cancelled instead.", "A rejected request is final — raise a new
       one.", "It is still awaiting approval; withdraw it instead.", or "A cancellation is already
       pending on it."`,
    ),

    section('Signing in'),
    table(
      ['What the product says', 'What it means'],
      [
        [
          '"Invalid email or password"',
          'One of the two was wrong. It deliberately does not say which, because that would tell anybody at your keyboard whether an address has an account behind it.',
        ],
        [
          '"Too many failed attempts. Try again later."',
          'Repeated failures have locked the account for a period. Wait, or ask an administrator.',
        ],
      ],
      'The two sign-in refusals. See [Signing in](page:getting-started/signing-in).',
    ),

    section('When something has genuinely broken'),
    prose(
      `"Something went wrong while processing your request." is the only message the product shows for a
       fault it did not expect, and it is the one message that is never your fault. So is "Something
       went wrong while preparing this page.", which carries the request ID in the sentence itself.`,
    ),
    prose(
      `**What to do.** Copy the request ID, then tell an administrator. It is the key to the log entry
       for your exact attempt, and without it the search is a great deal harder.`,
    ),

    faq([
      {
        q: 'I got 403 on a screen I use every day.',
        a: `Check whether the message is the generic "You do not have permission to perform this
            action." or one that names a reason. If it is the generic one, a role has changed. If it is
            "Your form session expired. Refresh the page and try again.", it is not a permission problem
            at all — reload and try again.`,
      },
      {
        q: 'The conflict screen says nothing was saved. Is that reliable?',
        a: `Yes. The confirmation sheet writes nothing — it only computes and displays. The payment and
            the request status are written together in a single step, so either both happened or
            neither did.`,
      },
      {
        q: 'Can I see the messages somebody else got?',
        a: `Every refusal is logged with its request ID, and administrators can read the record on
            [The audit trail](page:admin/the-audit-trail). The messages themselves are not stored on the
            request.`,
      },
      {
        q: 'The form came back with a message but I cannot see which field is wrong.',
        a: `The band names the rule rather than highlighting the field. Read the message against
            [Raising a request](page:requester/raising-a-request), which lists every field and what it
            requires for each type.`,
      },
    ]),
  ],
});
