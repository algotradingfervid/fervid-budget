import { page, prose, section, subsection, shot, note, warning, table, faq } from '../../schema.mts';

export default page({
  section: 'getting-started',
  slug: 'a-request-in-detail',
  title: 'Anatomy of a request',
  summary: 'The one screen everybody shares: the details, the documents and the conversation thread.',
  blocks: [
    prose(
      `A request has one screen, and everybody who can see the request sees the same one. The raiser,
       the approver and the accountant are all reading the same figures and the same conversation.
       What changes between them is the row of buttons at the bottom — what each person is allowed
       to do about what they are reading.`,
    ),
    prose(
      `Whose requests you can open at all is set by your roles. A requester normally sees only their
       own; approvers, the accounts team and administrators normally see every request in the
       organisation. Your organisation can change that, so check with an administrator rather than
       assuming.`,
    ),

    section('The head'),
    shot(
      'requester/detail-pending',
      'A freshly submitted request, as the person who raised it sees it.',
    ),
    prose(
      `The screen opens with the request itself rather than the usual page header band. Reading down:`,
    ),
    table(
      ['What you see', 'What it is'],
      [
        ['`PR-2026-000426`, top left', 'The request number. It never changes, and it is what to quote when you talk to anybody about this request.'],
        ['`₹9,500.00`, top right', 'The amount asked for.'],
        ['**Staff training programme**', 'The short title the raiser gave it.'],
        ['The grey line beneath', 'The request type, then the project and head it is booked to, then who raised it and when.'],
        ['The badge', 'The status in words. Here, "Awaiting approval".'],
        ['The sentence beside it', 'Who owes the next move — "Waiting on Manish Patel" here. It reads "Waiting on you" in brand colour when that person is you.'],
      ],
    ),
    note(
      `A request marked urgent carries a red **Urgent** flag in front of its status badge. A request
       Accounts has parked shows **On hold** in place of its normal badge, because that is the truth
       about it while the hold lasts. While somebody in Accounts has the request reserved, the badge
       names them: "With Accounts — taken by" and their name, or "taken by you".`,
    ),

    subsection('Banners'),
    prose(
      `Between the head and the details, the request may raise a banner about its own state. Each
       one carries the words the person responsible actually wrote, not a paraphrase.`,
    ),
    table(
      ['Banner', 'When it appears'],
      [
        ['**Approved requests are locked**', 'You raised it, it is approved, and you can no longer edit it.'],
        ['**This request is on hold**', 'Accounts has parked it, with the question they asked and a note that payment is blocked until they lift it. The person who raised it also gets an **Add a document** box and an **Attach it** button here, to send what Accounts asked for.'],
        ['**… raised a concern about the partial payment**', 'The approver has questioned a short payment and Accounts has not answered yet.'],
        ['**Payment is frozen**', 'A cancellation has been asked for, and nobody can reserve or pay the request until the approver decides.'],
        ['**… sent this back**', 'The approver returned it for correction, quoting what they wrote.'],
        ['**… rejected this**', 'The approver rejected it, quoting the reason.'],
        ['**No document was attached**', 'The raiser gave a reason for having no supporting document.'],
      ],
    ),

    section('Details'),
    prose(
      `The **Details** card is the request as filled in. It is a grid of labelled rows, and a row is
       only drawn when the request carries that value — which is why two requests of different types
       do not show the same rows. The pill in the card's top right says whether the money is a
       **Budget expense** or **Recoverable**, and a recoverable names its category there too.`,
    ),
    table(
      ['Row', 'What it holds'],
      [
        ['Amount', 'What was asked for.'],
        ['Approved', 'What the approver approved. Only once a decision has been taken, and it may be less than the amount asked for.'],
        ['Needed by', 'The date the raiser said the money is needed.'],
        ['Project / Head', 'The budget line the spending is booked against.'],
        ['Vendor, or Paid to', 'Who is being paid. It reads Vendor when a vendor from the master was chosen, and Paid to otherwise. The vendor name is a link if you may open the vendor master.'],
        ['GSTIN', 'The vendor’s tax registration, when the vendor record has one.'],
        ['Invoice number / Invoice date', 'On a vendor invoice.'],
        ['Expense date', 'On a reimbursement — when the money was actually spent.'],
        ['Advance reason', 'On an advance — why money is needed before the spending.'],
        ['Counterparty / Expected return / Repayment terms', 'On money expected back.'],
        ['Requested by / Approver', 'The two people the request runs between.'],
        ['Urgency', '"Normal", or "Urgent" followed by the reason given.'],
        ['Purpose', 'The raiser’s own description, across the full width at the foot of the card.'],
      ],
      'Every row the Details card can draw. You will not see all of them on any one request.',
    ),
    prose(
      `Compare the two screenshots on this page. The vendor invoice above shows **Vendor**, **Invoice
       number** and **Invoice date**; the employee advance below shows **Paid to** and **Advance
       reason** instead. Nothing was hidden — those requests were never asked for those fields.
       [Choosing a request type](page:requester/choosing-a-request-type) explains which type asks
       for what.`,
    ),

    section('Payment outcome'),
    shot(
      'requester/detail-completed',
      'The same screen after the money has gone out. Payment outcome and Payment history have appeared between the details and the attachments.',
    ),
    prose(
      `Once a payment has been recorded against the request, a **Payment outcome** block appears.
       It sets what was approved beside what has been paid so far, and names the difference —
       because a difference that has been agreed and a difference that is still owed must never
       read the same. Its third line depends on where the request stands.`,
    ),
    table(
      ['Line', 'What it means'],
      [
        ['Approved', 'The figure the approver signed off.'],
        ['Total paid to date', 'Everything Accounts has paid against the request so far, across every payment.'],
        ['**Difference · confirmed settled by Accounts**', 'The request is completed. In the screenshot the difference is `₹0.00`. If Accounts closed it for less than the approved figure, the line reads **Difference · agreed deduction or adjustment · confirmed settled by Accounts**.'],
        ['**Still owed to the payee · remaining approved balance**', 'Part has been paid and the rest is still approved for payment — an installment.'],
        ['**Still owed to the payee · awaiting manager decision**', 'Accounts paid short and has asked the approver to decide about the rest.'],
        ['**Balance written off · shortfall accepted by …**', 'The approver accepted the short payment, and the balance will not be paid.'],
      ],
    ),
    prose(
      `When Accounts gives a reason for paying less, it appears under the block in a banner headed
       with their name and "provided this settlement reason". Under that, **Payment history** lists
       every payment linked to the request — its number, date, amount and reference — so a request
       paid in installments shows each one. [Getting paid](page:requester/getting-paid) covers this
       from the raiser's side and [Reviewing a shortfall](page:manager/shortfall-review) from the
       approver's.`,
    ),

    section('Attachments'),
    prose(
      `Under the details sits **Attachments**, with a file count on the right. Each document is one
       row: an icon for its kind, the file name, its size, and the date it was added. A **Download**
       button on the right appears if your permissions let you open documents — all four built-in
       roles, Requester, Manager, Accounts and Admin, can. With nothing attached
       the section reads "No documents attached", as it does in both screenshots on this page.`,
    ),
    prose(
      `Adding documents is done from the request form and the edit form, not from here — except
       while the request is on hold, when its raiser can add one from the hold banner. See
       [Attaching documents](page:requester/attachments).`,
    ),

    section('History and conversation'),
    prose(
      `The last section is the request's whole life in order, oldest first, under the heading
       **History and conversation**. The line beside it states the rule plainly: everyone who can
       see this request sees this whole stream. There is no private channel on a request.`,
    ),
    prose(
      `The finished request above shows four entries: "Rahul Gupta submitted request PR-2026-000537
       for ₹5,300.00", "Manish Patel approved request PR-2026-000537 for ₹5,300.00" with the field
       it changed shown underneath as **Approved amount** and its new value, then the accountant's
       two lines — reserving the request for processing, and settling it as completed. Each line
       carries the date and time it happened. Lines written today start with the name of the person
       who acted, for example "Harsh Agarwal reserved the request for processing"; the two
       accountant lines in this screenshot were recorded before that and read without a name.`,
    ),
    prose(
      `Comments and attached documents appear in the same stream, in place. Below it, if you are
       allowed to comment, is an **Add a comment** box and a **Post comment** button. The box says
       what it does: anyone who can see this request will see your comment.`,
    ),
    warning(
      `Nothing in the thread can be edited or deleted, including your own comments. It is the
       record of what happened. Read a comment back before you post it.`,
    ),

    section('The action bar'),
    shot(
      'manager/request-pending',
      'The same screen, as the approver it was sent to. Four buttons along the bottom that the raiser never sees.',
    ),
    prose(
      `The row of buttons at the foot is the only part of this screen that differs between people.
       It is built from two things: what your permissions allow, and where you sit on this
       particular request — whether you raised it, and whether it was sent to you to decide. The
       request's current status then decides which of those actions still make sense.`,
    ),
    prose(
      `In the approver's screenshot the bar reads **Reject**, **Return for correction**, **Approve
       ₹80,700.00** and **Reassign approval**. On the raiser's screenshot of a request in the same
       state, the same bar reads **Edit request** and **Withdraw**. Same screen, same status,
       different seat.`,
    ),
    table(
      ['Control', 'Offered to', 'While the request is'],
      [
        ['**Edit request**', 'The person who raised it', 'Awaiting approval'],
        ['**Edit every field**, **Save corrections**, **Resubmit for approval**', 'The person who raised it, on the correction screen that opens in place of this one', 'Returned for correction'],
        ['**Withdraw**', 'The person who raised it', 'Awaiting approval'],
        ['**Request cancellation**', 'The person who raised it', 'Approved, with nothing paid against it yet'],
        ['**Raise it again**', 'The person who raised it', 'Rejected'],
        ['**Approve …**, **Return for correction**, **Reject**', 'The approver it was sent to', 'Awaiting approval'],
        ['**Reassign approval**', 'The approver it was sent to, or an administrator who may edit users — either way holding the reassign permission', 'Awaiting approval, returned for correction, cancellation requested, or paid short and back with the approver'],
        ['**Cancel with reason**', 'The approver it was sent to', 'Approved, with nothing paid against it yet'],
        ['**Decide the cancellation**', 'The approver it was sent to', 'Cancellation requested'],
        ['**Decide the partial payment**', 'The approver it was sent to', 'Paid short and back with the approver'],
        ['**Record payment**', 'The accounts team', 'Approved, claimed by nobody and not on hold'],
        ['**Put on hold**', 'The accounts team', 'Approved and not already held'],
        ['**Keep on hold**, **Release hold**', 'The accounts team', 'On hold'],
        ['**View the payment**', 'Anybody who may see payments', 'Carrying a recorded payment'],
      ],
      'Each control also needs the matching permission. Holding the seat is not enough on its own.',
    ),
    note(
      `An empty action bar is an answer, not a fault: there is nothing for you to do about this
       request as it stands. A rejected request somebody else raised, or a completed one, offers
       nobody anything.`,
    ),
    prose(
      `The decisions that need words — returning, rejecting, cancelling, holding, reassigning — open
       a panel over the screen and will not go through until you have written the reason. That is
       deliberate: the reason is the whole message the next person gets, and it lands in the thread
       above for everybody to read.`,
    ),

    faq([
      {
        q: 'I can see the request but there are no buttons at all.',
        a: `Then there is nothing for you to do on it right now. Either it is not at a stage that
            asks anything of you, or it was never yours to act on. The waiting line at the top says
            who it is with.`,
      },
      {
        q: 'The Approved row shows less than the Amount row.',
        a: `The approver approved a smaller figure than was asked for. That is allowed, and the
            reason should be in the thread. [Approving a request](page:manager/approving) covers it.`,
      },
      {
        q: 'Why can I not delete a comment I posted by mistake?',
        a: `The thread is a record. Post a follow-up correcting yourself — that is visible to
            exactly the same people the original was.`,
      },
      {
        q: 'Somebody told me to look at a request and I get a refusal.',
        a: `Requests are scoped: your roles decide whose requests you may open, and for most
            requesters that is their own only. Ask an administrator what your account holds.`,
      },
    ]),
  ],
});
