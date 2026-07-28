import { page, prose, section, subsection, shot, note, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'the-payment-queue',
  title: 'The payment queue',
  summary: 'Approved requests waiting to be paid, and how the tabs divide the work.',
  blocks: [
    prose(
      `The payment queue is the accounts team’s starting point. It is the sidebar item
       **Accounts queue**, and the number on it is the count of approved requests nobody has
       claimed yet.`,
    ),

    shot('accounts/queue-approved', 'The queue on its first tab: approved requests nobody has taken.'),

    section('Reading the top of the screen'),
    prose(
      `Four tiles sit above the tabs. They count your whole view of the data, not the tab you are
       on and not the rows a search has left on screen — so typing in the search box narrows the
       table and never moves the numbers.`,
    ),
    table(
      ['Tile', 'What it counts'],
      [
        ['**Approved, unclaimed**', 'Requests that are approved, unclaimed and not on hold — the only ones you may take. The figure underneath is their total value.'],
        ['**Reserved by you**', 'Requests you are holding. The line underneath reads `All recent` unless one has been open longer than a day, in which case it says how many.'],
        ['**Reserved by others**', 'Requests a colleague is holding. You can see them; you cannot take them.'],
        ['**On hold**', 'Approved requests parked with a question. They are waiting on the requester, not on you.'],
      ],
    ),

    section('The five tabs'),
    prose(
      `Each tab is a different kind of work, and each carries its own count. The action at the end
       of a row changes with the tab, because what you can do to a request depends on where it has
       got to.`,
    ),
    table(
      ['Tab', 'What is in it', 'The action on a row'],
      [
        ['**Approved, unclaimed**', 'Approved requests, plus the ones somebody has already taken so you can see who has them', '**Take for processing**'],
        ['**Processing**', 'Requests somebody has claimed, and who is holding each one', '**Resume** on your own; **View** on everybody else’s'],
        ['**On hold**', 'Approved requests parked with a reason, waiting on the requester', '**Read reply**'],
        ['**Partial review**', 'Requests paid short, now waiting on the approver', '**View**'],
        ['**Paid**', 'Finished work, kept for reference', '**View**, or **Resume** on a row you settled yourself'],
      ],
    ),

    subsection('Approved, unclaimed'),
    prose(
      `This is the takeable set. Every row carries **Take for processing**, which claims the
       request and opens the payment form — see [taking a request for
       processing](page:accounts/claiming-a-request).`,
    ),

    subsection('Processing'),
    shot('accounts/queue-processing', 'The Processing tab. Each row names its holder, and your own rows say **Reserved by you** with the time the claim started.'),
    prose(
      `A row you hold offers **Resume**. Everybody else’s names the holder and offers **View**
       only. Once a claim has been open more than a day, **Resume** takes you to a screen about
       the claim itself rather than back to the payment form — [reservations and
       conflicts](page:accounts/reservations-and-conflicts) covers what to do there.`,
    ),

    subsection('On hold'),
    shot('accounts/queue-hold', 'The On hold tab. These are approved requests Accounts has paused with a question.'),
    prose(
      `A hold does not change a request’s status — it stays approved — but it takes the request out
       of the takeable set until somebody in Accounts lifts it. **Read reply** opens the request,
       where the question and the requester’s answer sit in the conversation. See [putting a
       request on hold](page:accounts/holding-a-request).`,
    ),

    subsection('Partial review'),
    shot('accounts/queue-partial-review', 'The Partial review tab. Every row is a shortfall waiting on an approver’s decision.'),
    prose(
      `These are requests that were paid less than the approved amount. They are marked
       \`partial_review\` — the screen writes it **Partial — manager review** — and the decision is
       not yours: the approver either accepts the difference or raises a concern. See [paying part
       of a request](page:accounts/paying-part-of-a-request).`,
    ),

    subsection('Paid'),
    shot('accounts/queue-paid', 'The Paid tab. `Completed` closed cleanly; `Completed — partial accepted` closed with a shortfall the approver wrote off.'),
    prose(
      `A few rows here offer **Resume** rather than **View**. Those are the ones you settled
       yourself, and following that control lands on a refusal, because the request has already
       been paid. Open the request from its number in the first column instead — the payment is
       linked from there.`,
    ),

    section('Finding one request'),
    prose(
      `The search box above the table matches the request number, the requester, the payee, the
       project, the head and the amount. It searches within the tab you are on, and it leaves the
       tiles alone.`,
    ),
    tip(
      `Amounts can be typed the way the screen writes them. Searching for \`13,800.00\` finds the
       row showing ₹13,800.00.`,
    ),
    note(
      `A request whose money is expected back shows a \`Recoverable\` pill where the other rows show
       their project and head. Those requests carry no budget head at all — see [the recoverables
       register](page:accounts/the-recoverables-register).`,
    ),

    section('When a claim of yours has gone stale'),
    prose(
      `If any of the requests you are holding has been reserved for longer than the reminder
       threshold, a banner appears above the table saying how many, with a **Review** button that
       takes you to the Processing tab. Nothing is ever released for you: a transfer may already be
       moving through a bank portal, so the product nudges and a person decides.`,
    ),

    faq([
      {
        q: 'The tile says 130 and the first tab lists more rows than that.',
        a: `The tile counts only what you may take — approved, unclaimed and not on hold. The tab
            also lists the requests somebody has already claimed, so that you can see who holds the
            one you were about to pay rather than having it silently hidden from you.`,
      },
      {
        q: 'A request I expected is missing from every tab.',
        a: `The queue only holds requests that reached approval. Anything still awaiting a
            decision, returned, rejected, withdrawn or cancelled never appears here. [The request
            lifecycle](page:reference/request-lifecycle) lists every status and the routes between
            them.`,
      },
      {
        q: 'Can I pay several requests at once?',
        a: `No. The line under the heading states the rule the product is built on: one request,
            one payment.`,
      },
    ]),
  ],
});
