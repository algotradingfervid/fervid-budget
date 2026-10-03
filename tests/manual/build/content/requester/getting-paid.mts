import { page, prose, section, subsection, shot, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'getting-paid',
  title: 'Getting paid',
  summary: 'What happens after approval, and how to read a payment that is short of the full amount.',
  blocks: [
    prose(`Approval is not payment. It is permission to pay, handed to the accounts team, who then
       claim the request, record the money going out, and close it. You do not do any of that — but
       the whole of it is reported back on your request, so you never have to ask anybody where a
       payment has got to.`),

    section(`The four states after approval`),

    table(
      [`Status pill`, `What has happened`, `Waiting on`],
      [
        [`**Approved — awaiting payment**`, `Your approver signed it. Nobody in Accounts has claimed it yet.`, `Accounts`],
        [`**With Accounts — taken by …**`, `An accountant has claimed the request and is working on it; the pill names them. Nobody else can pay it while they hold it.`, `That accountant`],
        [`**Completed**`, `The payment has been recorded and covers what was approved.`, `Nothing pending`],
        [`**Completed — partial accepted**`, `Less was paid than approved, and your approver agreed to close it anyway.`, `Nothing pending`],
      ],
    ),

    subsection(`Approved, and waiting`),

    shot('requester/detail-approved', 'An approved request. Amount and Approved sit side by side in Details.'),

    prose(`The moment a request is approved, **Details** grows a second figure. **Amount** is what you
       asked for; **Approved** is what your approver signed. They are usually the same. Where they
       are not, the approved figure is the smaller one — an approver may reduce what was asked for
       and can never raise it.`),

    subsection(`Claimed by Accounts`),

    prose(`When an accountant takes the request, the status pill becomes **With Accounts — taken by**
       and their name, the sentence beside it reads *Waiting on* followed by their name and *(Accounts)*, and the history
       gains a line recording that the request was reserved for processing. That claim is exclusive — it exists so two
       people cannot pay the same request twice. If they hold it for too long without paying, the
       history also records each *Stale-reservation reminder sent to* them.`),

    shot('requester/detail-processing', 'A request claimed by Accounts. The pill names the accountant who took it, and the history ends with the reservation and the reminders sent to them.'),

    subsection(`Paid`),

    prose(`A recorded payment adds a **Payment outcome** block between the details and the attachments.
       It always has three rows: **Approved** with the signed figure, **Total paid to date** with
       everything paid against the request so far, and a third row naming the difference between
       them. Beneath it, **Payment history** lists each linked payment on one line — its number, the
       date it was paid, the amount and the transaction reference, for example *PAY-17 · 19 June
       2026 · ₹5,300.00 · UTR00000537*.`),

    shot('requester/detail-completed', 'A completed request. Payment outcome shows Approved, Total paid to date and a ₹0.00 difference confirmed settled by Accounts; Payment history lists the one payment.'),

    section(`Reading a payment that is short`),

    prose(`Payments can fully settle the request, keep an installment balance payable, or ask the
       manager to decide a shortfall. The request shows cumulative payments and the remaining
       balance. An installment returns it to **Approved — awaiting payment** for another payment
       under the same approval, and while any money has been paid the request can no longer be
       cancelled.`),

    table(
      [`The third row says`, `What it means`],
      [
        [`**Difference · confirmed settled by Accounts**`, `The request is **Completed** and paid in full; the difference is ₹0.00.`],
        [`**Difference · agreed deduction or adjustment · confirmed settled by Accounts**`, `The request is **Completed**, but less than the approved figure was paid. Accounts declared the obligation discharged and recorded why; nothing more is owed.`],
        [`**Still owed to the payee · awaiting manager decision**`, `A genuine shortfall. The balance has not been paid, and the request has gone to your approver to decide what happens to it.`],
        [`**Still owed to the payee · remaining approved balance**`, `An installment. Part has been paid and the rest is still payable under the same approval.`],
        [`**Balance written off · shortfall accepted by …**`, `Your approver, named in the row, accepted the shortfall and the request closed as **Completed — partial accepted**.`],
      ],
    ),

    prose(`A payment sent for shortfall review, or closed short as fully settled, always carries a
       written reason. It appears on your request under the payment outcome as a banner headed with
       the accountant's name and the words **provided this settlement reason**, their sentence
       beneath it. A shortfall review turns the status to **Partial — manager review** and sends the
       request to your approver, who can choose **Keep balance payable** so the rest can be paid in
       another installment, **Accept and close** so the request ends as **Completed — partial
       accepted**, or **Raise a concern** with Accounts. While a concern is open the pill reads
       **Partial — under discussion**.`),

    warning(`Nobody can pay more than was approved. An attempt to is refused outright, with a message
       saying the amount *is more than the approved* figure, and *to pay more, cancel this request
       and raise a new one*. Once an installment has been paid, the refusal instead says the amount
       *exceeds the remaining approved balance* and that *additional spending needs a separately
       approved request*. If the true cost has risen, that is a new request, not a bigger
       payment.`),

    section(`If Accounts put it on hold`),

    prose(`An accountant who cannot pay a request as it stands can park it and ask you something. When
       you open the request it shows an amber banner headed **This request is on hold**, quoting their
       question, and adds *Payment is blocked until Accounts lifts the hold.* The status pill at the
       top reads **On hold** and the waiting sentence becomes **Waiting on you**. The action bar says
       *Only Accounts can take this off hold.*

       On the card in **My requests** and on your home screen, a hold is not a status of its own: the
       pill still reads **Approved — awaiting payment**, and only the sentence beside it changes to
       **Waiting on you**.

       Answer in the comment box at the foot of the request — that is where the hold expects to be
       answered. If Accounts asked for a document, the banner also carries an **Add a document**
       control and an **Attach it** button; this is the only way to add a file to a request once it
       is approved. Only Accounts can lift a hold; the button to do it is not yours, and editing is
       closed on an approved request.`),

    section(`What you are told, and when`),

    prose(`Most steps write you a notification. As the product is set up, the ones addressed to the
       person who raised the request are a return, a rejection, the approval, the payment being
       settled, a hold going on and coming off, a reservation being released or handed to somebody
       else, a shortfall being accepted, and any cancellation decision — including a cancellation
       your approver made without your asking.

       Two payment steps do not notify you: an installment that leaves a balance payable, and a
       payment sent for shortfall review (that one goes to your approver). You see both on the
       request itself. An administrator can change who receives each notification.`),

    note(`Those notifications always appear in the product, on the
       [Notifications](page:getting-started/notifications) screen. Whether they also arrive by
       email depends on what your administrator has switched on and whether a mail server is
       configured.`),

    section(`Reading the payment itself`),

    prose(`A requester sees the outcome on the request and not in the payments ledger — the ledger is
       an accounts screen, and it is not on your menu. Everything you need is on your own request:
       the approved figure, the amount paid, the date, and the difference named for what it is.`),

    tip(`If you need the payment reference — a transaction number for a supplier chasing you, say —
       it is the last item on each line of **Payment history** on your request. The payment number
       there is plain text for you, not a link, because the payment record itself is an accounts
       screen.`),

    section(`The end of the story`),

    prose(`**Completed** and **Completed — partial accepted** are both terminal: there is nothing
       further to do, and the waiting sentence says **Nothing pending**. The request stays readable
       for good, with its whole history — submission, approval, reservation, payment — in one
       stream.`),

    note(`A completed request — paid in full or closed with a shortfall accepted — moves to the
       **Closed** tab of [My requests](page:requester/tracking-your-requests), alongside rejected,
       withdrawn and cancelled requests. It is also on **All**, or search for its number.`),

    faq([
      { q: `It has been approved for a week and nothing has happened.`, a: `It is sitting in the accounts queue. Reminders go out to whoever a request is waiting on after the number of days your organisation has configured; if it is genuinely urgent, talk to Accounts.` },
      { q: `I was approved for less than I asked for.`, a: `That is the approver’s decision, and the reason is usually in the history or in a note on the approval. The approved figure is the ceiling Accounts may pay to.` },
      { q: `The request says Completed but the money has not reached my account.`, a: `**Completed** means Accounts recorded the payment on their side. The bank transfer is a separate thing with its own timing — ask Accounts, quoting the request number.` },
      { q: `Can a payment be undone?`, a: `Not by you, and not on a request. A payment tied to a request is deliberately sealed: it is the record of the outcome, and editing it would rewrite what you and your approver were told was paid.` },
    ]),
  ],
});
