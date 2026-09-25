import { page, prose, section, subsection, shot, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'getting-paid',
  title: 'Getting paid',
  summary: 'What happens after approval, and how to read a payment that is short of the full amount.',
  blocks: [
    prose(
      `Approval is not payment. It is permission to pay, handed to the accounts team, who then claim
       the request, record the money going out, and close it. You do not do any of that — but the
       whole of it is reported back on your request, so you never have to ask anybody where a payment
       has got to.`,
    ),

    section('The four states after approval'),
    table(
      ['Status pill', 'What has happened', 'Waiting on'],
      [
        ['**Approved — awaiting payment**', 'Your approver signed it. Nobody in Accounts has claimed it yet.', 'Accounts'],
        ['**With Accounts**', 'An accountant has claimed the request and is working on it. Nobody else can pay it while they hold it.', 'Accounts'],
        ['**Completed**', 'The payment has been recorded and covers what was approved.', 'Nothing pending'],
        ['**Completed — partial accepted**', 'Less was paid than approved, and your approver agreed to close it anyway.', 'Nothing pending'],
      ],
    ),

    subsection('Approved, and waiting'),
    shot('requester/detail-approved', 'An approved request. Amount and Approved sit side by side in Details.'),
    prose(
      `The moment a request is approved, **Details** grows a second figure. **Amount** is what you
       asked for; **Approved** is what your approver signed. They are usually the same. Where they are
       not, the approved figure is the smaller one — an approver may reduce what was asked for and can
       never raise it.`,
    ),

    subsection('Claimed by Accounts'),
    shot('requester/detail-processing', 'A request an accountant has taken. The pill reads With Accounts, and the history records the reservation.'),
    prose(
      `When an accountant takes the request, the status becomes **With Accounts** and a line appears in
       the history, naming the accountant: *Harsh Agarwal reserved the request for processing*. That
       claim is exclusive — it exists so two people cannot pay the same request twice.`,
    ),

    subsection('Paid'),
    shot('requester/detail-completed', 'A completed request. Payment outcome sets the approved figure against what was actually paid.'),
    prose(
      `A recorded payment adds a **Payment outcome** block between the details and the attachments. It
       has two or three rows: **Approved** with the signed figure, **Paid on** with the date and the amount
       that actually went out, and a third row naming the difference between them.`,
    ),

    section('Reading a payment that is short'),
    prose(
      `Every payment is recorded either as settling the request or as a partial settlement, and the
       third row of **Payment outcome** tells you which — the two must never read the same.`,
    ),
    table(
      ['The third row says', 'What it means'],
      [
        ['**Difference · confirmed settled by Accounts**', 'Accounts declared the obligation discharged. Where they paid the full amount, the difference is ₹0.00; where they paid slightly less, they have confirmed nothing more is owed.'],
        ['**Still owed to the payee**', 'A genuine shortfall. The balance has not been paid, and the request has gone to your approver to decide what happens to it.'],
      ],
    ),
    prose(
      `A partial payment always carries a written reason, and it appears on your request as a banner
       headed with the accountant's name and the words **marked this a genuine partial payment**, their
       sentence beneath it. The status becomes **Partial — manager review**, and the request goes back to your approver, who
       either accepts the shortfall — closing it as **Completed — partial accepted** — or raises a
       concern with Accounts.`,
    ),
    warning(
      `Nobody can pay more than was approved. An attempt to is refused outright, with a message saying
       the amount *is more than the approved* figure, and *to pay more, cancel this request and raise a
       new one*. If the true cost has risen, that is a new request, not a bigger payment.`,
    ),

    section('If Accounts put it on hold'),
    prose(
      `An accountant who cannot pay a request as it stands can park it and ask you something. The
       request then shows an amber banner headed **This request is on hold**, quoting their question,
       and adds *Payment is blocked until Accounts lifts the hold.* The status pill reads **On hold**
       and the waiting sentence becomes **Waiting on you**.`,
    ),
    prose(
      `Answer in the comment box at the foot of the request — that is where the hold expects to be
       answered. Only Accounts can lift a hold; the button to do it is not yours, and neither an
       approval nor an edit removes it.`,
    ),

    section('What you are told, and when'),
    prose(
      `Every step writes you a notification. The ones addressed to the person who raised the request
       are the approval, the payment being settled, a hold going on and coming off, a reservation being
       released or handed to somebody else, a shortfall being accepted, and any cancellation decision.`,
    ),
    note(
      `Those notifications always appear in the product, on the
       [Notifications](page:getting-started/notifications) screen. Whether they also arrive by email
       depends on what your administrator has switched on and whether a mail server is configured.`,
    ),

    section('Reading the payment itself'),
    prose(
      `A requester sees the outcome on the request and not in the payments ledger — the ledger is an
       accounts screen, and it is not on your menu. Everything you need is on your own request:
       the approved figure, the amount paid, the date, and the difference named for what it is.`,
    ),
    tip(
      `If you need the payment reference — a transaction number for a supplier chasing you, say — ask
       Accounts and quote your request number. They can read it straight off the payment record.`,
    ),

    section('The end of the story'),
    prose(
      `**Completed** and **Completed — partial accepted** are both terminal: there is nothing further
       to do, and the waiting sentence says **Nothing pending**. The request stays readable for good,
       with its whole history — submission, approval, reservation, payment — in one stream.`,
    ),
    note(
      `A completed request is not in the **Open** tab or the **Closed** tab of
       [My requests](page:requester/tracking-your-requests). Look for it on **All**, or search for its
       number.`,
    ),

    faq([
      {
        q: 'It has been approved for a week and nothing has happened.',
        a: `It is sitting in the accounts queue. Reminders go out to whoever a request is waiting on
            after the number of days your organisation has configured; if it is genuinely urgent, talk
            to Accounts.`,
      },
      {
        q: 'I was approved for less than I asked for.',
        a: `That is the approver’s decision, and the reason is usually in the history or in a note on
            the approval. The approved figure is the ceiling Accounts may pay to.`,
      },
      {
        q: 'The request says Completed but the money has not reached my account.',
        a: `**Completed** means Accounts recorded the payment on their side. The bank transfer is a
            separate thing with its own timing — ask Accounts, quoting the request number.`,
      },
      {
        q: 'Can a payment be undone?',
        a: `Not by you, and not on a request. A payment tied to a request is deliberately sealed: it is
            the record of the outcome, and editing it would rewrite what you and your approver were
            told was paid.`,
      },
    ]),
  ],
});
