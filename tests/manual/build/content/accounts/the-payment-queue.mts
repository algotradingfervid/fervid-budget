import { page, prose, section, subsection, shot, note, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'the-payment-queue',
  title: 'The payment queue',
  summary: 'Approved requests waiting to be paid, and how the tabs divide the work.',
  blocks: [
    prose(`The payment queue is the accounts team’s starting point. It is the sidebar item **Accounts
       queue**, and the number on it is the count of approved requests nobody has claimed yet and
       nobody has put on hold. The screen itself is headed **Payment queue**, and **＋ Record a
       payment** at the top right opens the payment picker.`),

    shot('accounts/queue-approved', 'The queue on its first tab. The four tiles sit above the tabs, and every row here offers **Take for processing**.'),

    section(`Reading the top of the screen`),

    prose(`Four tiles sit above the tabs. They count your whole view of the data, not the tab you are
       on and not the rows a search has left on screen — so typing in the search box narrows the
       table and never moves the numbers.`),

    table(
      [`Tile`, `What it counts`],
      [
        [`**Approved, unclaimed**`, `Requests that are approved, unclaimed and not on hold — the only ones you may take. The figure underneath is their remaining approved balance after existing payments.`],
        [`**Reserved by you**`, `Requests you are holding. The line underneath reads \`All recent\` unless one has been open longer than a day, in which case it says how many.`],
        [`**Reserved by others**`, `Requests a colleague is holding. The line underneath reads \`Visible, not yours\`.`],
        [`**On hold**`, `Approved requests parked with a question. The line underneath reads \`Waiting on the requester\`.`],
      ],
    ),

    section(`The five tabs`),

    prose(`Each tab is a different kind of work, and each carries its own count. The action at the end
       of a row changes with the tab, because what you can do to a request depends on where it has
       got to.`),

    table(
      [`Tab`, `What is in it`, `The action on a row`],
      [
        [`**Approved, unclaimed**`, `Approved requests nobody has claimed and nobody has put on hold — the same set its count and the tile count`, `**Take for processing**`],
        [`**Processing**`, `Requests somebody has claimed, and who is holding each one`, `**Resume** on your own; **View** on everybody else’s`],
        [`**On hold**`, `Approved requests parked with a reason, waiting on the requester`, `**Read reply**`],
        [`**Partial review**`, `Requests paid short, now waiting on the approver`, `**View**`],
        [`**Paid**`, `Finished work, kept for reference`, `**View**`],
      ],
    ),

    subsection(`Approved, unclaimed`),

    prose(`This is the takeable set. Every row carries **Take for processing**, which claims the
       request and opens the payment form — see [taking a request for
       processing](page:accounts/claiming-a-request).`),

    subsection(`Processing`),

    shot('accounts/queue-processing', 'The Processing tab. Each row names its holder; your own rows say **Reserved by you** with how long the claim has been open, and offer **Resume**.'),

    prose(`A row you hold offers **Resume**. Everybody else’s names the holder and offers **View**
       only; an administrator, who may hand claims on, sees **Reassign** there instead. Once a claim
       has been open more than a day, **Resume** takes you to a screen about the
       claim itself rather than back to the payment form — [reservations and
       conflicts](page:accounts/reservations-and-conflicts) covers what to do there.`),

    subsection(`On hold`),

    shot('accounts/queue-hold', 'The On hold tab. These are approved requests Accounts has paused with a question.'),

    prose(`A hold does not change a request’s status — it stays approved — but it takes the request out
       of the takeable set until somebody in Accounts lifts it. **Read reply** opens the request,
       where the question and the requester’s answer sit in the conversation. See [putting a
       request on hold](page:accounts/holding-a-request).`),

    subsection(`Partial review`),

    shot('accounts/queue-partial-review', 'The Partial review tab. Every row is a shortfall waiting on an approver’s decision.'),

    prose(`These are requests that were paid less than the approved amount. They are marked
       \`partial_review\` — the screen writes it **Partial — manager review**, or **Partial — under
       discussion** while the approver has a concern open — and the decision is not yours: the approver can keep the balance payable, accept and close the difference, or
       raise a concern. See [paying part of a request](page:accounts/paying-part-of-a-request).`),

    subsection(`Paid`),

    shot('accounts/queue-paid', 'The Paid tab. `Completed` closed cleanly; `Completed — partial accepted` closed with a shortfall the approver wrote off.'),

    prose(`Every row here offers **View**, which opens the request. Its payment is linked from there
       under **View the payment**.`),

    section(`Finding one request`),

    prose(`The search box above the table matches the request number, the requester, the payee, the
       project, the head and the amount. It searches within the tab you are on, and it leaves the
       tiles alone.`),

    tip(`Amounts can be typed the way the screen writes them. Searching for \`13,800.00\` finds the
       row showing ₹13,800.00.`),

    note(`A request whose money is expected back shows a \`Recoverable\` pill where the other rows
       show their project and head. Those requests carry no budget head at all — see [the
       recoverables register](page:accounts/the-recoverables-register).`),

    section(`When a claim of yours has gone stale`),

    prose(`If any of the requests you are holding has been reserved for more than a day, a banner
       appears above the table saying how many you hold and how many of them have been open that
       long, with a **Review** button that takes you to the Processing tab. The one-day mark is
       fixed; the reminder that goes out by notification follows the number of days an
       administrator has configured. Nothing is ever released for you: a transfer may already be
       moving through a bank portal, so the product nudges and a person decides.`),

    faq([
      { q: `The request I was about to pay has gone from the first tab.`, a: `The first tab lists only what you may take — approved, unclaimed and not on hold — so its rows always match its count and the tile. A request somebody has claimed moves to **Processing**, where the row names who holds it; one that has been held is on **On hold**.` },
      { q: `A request I expected is missing from every tab.`, a: `The queue only holds requests that reached approval. Anything still awaiting a decision, returned, rejected, withdrawn or cancelled never appears here. [The request lifecycle](page:reference/request-lifecycle) lists every status and the routes between them.` },
      { q: `Can I pay several requests at once?`, a: `No bulk payment entry is offered. Record one payment submission at a time. A request can have multiple installments, each within its remaining approval.` },
    ]),
  ],
});
