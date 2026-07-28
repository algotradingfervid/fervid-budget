import { page, prose, section, subsection, shot, steps, note, warning, table, faq } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'reservations-and-conflicts',
  title: 'Reservations and conflicts',
  summary: 'What happens when somebody else already holds a request, and how a stale claim is released.',
  blocks: [
    prose(
      `A reservation is the claim you take on a request before you pay it. Its only job is to stop
       one invoice being paid twice: while a request is reserved, the payment form opens for the
       holder and for nobody else.`,
    ),
    prose(
      `Because it does that job properly, it will sometimes get in your way. When it does, the
       product does not answer with an error page — it answers with a screen naming what actually
       happened and what you may do about it.`,
    ),

    section('The three refusals'),
    prose(
      `Opening the payment form for a request you cannot take gives you one of three screens. All
       three are the same shape: a banner saying what is wrong, the request itself, a list headed
       **What you can do**, and **Pick another request** and **Back to queue** at the foot. All
       three end their banner with the same reassurance — nothing you typed has been saved, and no
       payment was created.`,
    ),

    subsection('Somebody else took it first'),
    shot('accounts/conflict-taken', 'The reservation race, lost. The banner names the colleague who now holds the request.'),
    prose(
      `Two people pressed **Take for processing** within moments of each other and the product gave
       it to one of them. The request now shows **Processing** with their name, and the time they
       took it. You are offered the queue and a read-only view of the request, where you can still
       read it and comment on it but not pay it.`,
    ),

    subsection('It is on hold'),
    shot('accounts/conflict-on-hold', 'A held request refuses to be claimed, and quotes the reason it was held for.'),
    prose(
      `Nobody holds a reservation on it. Payment is blocked because Accounts asked the requester a
       question and is waiting on the answer, and the banner quotes that question. This screen adds
       a third choice — **See what is on hold** — which takes you to the queue’s hold tab. See
       [putting a request on hold](page:accounts/holding-a-request).`,
    ),

    subsection('It is not available at all'),
    shot('accounts/conflict-already-paid', 'A request that has already been settled. Only an approved request can be taken for processing.'),
    prose(
      `Only an approved request can be taken. This screen names the status the request actually
       holds — in the shot above, \`Completed\` — and it is the one you meet when a request has
       already been paid, cancelled or frozen while a cancellation is decided.`,
    ),
    table(
      ['The banner says', 'What is true', 'What to do'],
      [
        ['`… took this request before you`', 'A colleague holds the reservation', 'Take another request, or ask them'],
        ['`… is on hold, so it cannot be taken for processing`', 'Nobody holds it; Accounts is waiting on the requester', 'Read the reply and lift the hold, then take it'],
        ['`… is not available to process`', 'The request is not in an approved state at all', 'Open it read-only to see what became of it'],
      ],
    ),

    section('Giving your own claim back'),
    shot('accounts/reservation-release', 'The release screen. A reason and the confirmation are both required.'),
    prose(
      `You reach this from **Release** in the reserved bar on the payment form, from **Cancel and
       release** at the foot of it, or from the queue. It asks for two things and refuses without
       either: a reason, which is recorded in the history and visible to everybody who can see the
       request, and the tick confirming no payment has been initiated.`,
    ),
    steps([
      { text: 'Choose **Release it** — the request goes back to `approved` for anybody in Accounts to pick up.' },
      { text: 'Type the reason.' },
      { text: 'Tick **I confirm no payment has been initiated for this request**.' },
      {
        text: 'Select **Release reservation**.',
        note: 'The requester and the approver are both told the request is unclaimed again.',
      },
    ]),
    warning(
      `The red banner on that screen is the important one. If you have already started the transfer
       in the bank portal, do not release — somebody else can take the request and pay it a second
       time. Finish recording it instead.`,
      'Never release a payment already under way',
    ),
    note(
      `The screen’s heading reads **Release or reassign this request**, because an administrator
       opening the same screen can hand a reservation to a named colleague. The accounts role
       cannot: you can give back a claim you hold, and that is all.`,
    ),

    section('A claim that has been open too long'),
    shot('accounts/reservation-stale', 'The nudge. It names how long the claim has been open, and lists what may be done about it.'),
    prose(
      `Once a reservation has been open longer than the configured number of days, the holder is
       reminded and the queue starts sending **Resume** here rather than back to the payment form.
       Nothing is released automatically, and the banner explains why: a transfer may already be
       under way, so a person decides.`,
    ),
    prose(
      `The list under **Pick one** shows only the choices the person reading it can actually take.
       Holding your own stale reservation, they are:`,
    ),
    table(
      ['Choice', 'What it does'],
      [
        ['**Carry on and record the payment**', 'Opens the payment form with your reservation intact'],
        ['**Release it**', 'Goes to the release screen, which needs a reason and the confirmation'],
        ['**Put it on hold**', 'Also goes to the release screen — a hold works on an unclaimed request, so the claim comes off first'],
      ],
    ),
    prose(
      `Below the list, **Who has been told** is the request’s history to date: who submitted it,
       who approved it and who reserved it.`,
    ),

    section('Somebody else’s stale claim'),
    prose(
      `You cannot take a reservation off a colleague. Handing one on is an administrator’s
       permission, and the accounts role does not carry it. If a request is stuck with somebody who
       is away, ask an administrator to reassign it; the reassignment demands a reason and the same
       confirmation, and the requester, the approver and the new holder are all told.`,
    ),

    faq([
      {
        q: 'Does a reservation expire?',
        a: `No. It is nudged, never expired. An expiring reservation would let a second accountant
            pay an invoice already moving through a bank portal, which is the one thing the whole
            mechanism exists to prevent.`,
      },
      {
        q: 'I closed my browser with the payment form open. Have I lost anything?',
        a: `No. The form writes nothing until you confirm, and the reservation survives. Go to the
            queue’s **Processing** tab and select **Resume**.`,
      },
      {
        q: 'The conflict screen names a colleague who says they never took it.',
        a: `The reservation history on the release screen lists every claim, release and
            reassignment on that request with a timestamp and a name. So does [the audit
            trail](page:admin/the-audit-trail), which an administrator can read.`,
      },
      {
        q: 'Why did I get a conflict screen rather than a plain error?',
        a: `Because the useful information is not “that failed” but who has it now and what to do
            instead. Every path that meets a refused reservation — the queue, the picker and the
            payment form — ends on the same three screens.`,
      },
    ]),
  ],
});
