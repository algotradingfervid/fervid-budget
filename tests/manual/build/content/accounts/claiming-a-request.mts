import { page, prose, section, shot, steps, note, warning, tip, faq } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'claiming-a-request',
  title: 'Taking a request for processing',
  summary: 'Claiming a request so two people cannot pay the same thing twice.',
  blocks: [
    prose(
      `Before you can record a payment you have to claim the request. The claim is what stops two
       accountants paying the same invoice on the same afternoon: while you hold it, nobody else
       can open the payment form for that request at all.`,
    ),
    prose(
      `Claiming is not a decision and it changes nothing about the money. It is a way of saying
       “I am working on this one”, and you can give it back.`,
    ),

    section('Claiming from the queue'),
    steps([
      {
        text: 'Open **Accounts queue** and stay on the **Approved, unclaimed** tab.',
        shot: 'accounts/queue-approved',
      },
      {
        text: 'Find the request, then select **Take for processing** at the end of its row.',
        note: 'The queue’s search matches the number, the requester, the payee, the project, the head and the amount.',
      },
      {
        text: 'You land on the payment form with the claim already in place.',
        note: 'A bar across the top reads **Reserved by you**, with the time it started and the words “nobody else can process this request”.',
        shot: 'accounts/payment-form',
      },
    ]),
    note(
      `You can also start from the payment form itself. Opening it without naming a request shows
       a picker of approved requests; choosing one claims it exactly as the queue button does.
       [Recording a payment](page:accounts/recording-a-payment) shows the picker.`,
    ),

    section('What the claim does'),
    prose(
      `The request moves to \`processing\` and your name goes on it. Everybody who can see the
       request sees that — the queue, the request screen and the history all say the same thing,
       and a line appears in the history reading “reserved it for processing”.`,
    ),
    shot('accounts/queue-processing', 'The Processing tab. Your own claims say **Reserved by you** and offer **Resume**; a colleague’s names them and offers **View** only.'),
    prose(
      `Only three kinds of request can be claimed: approved, unclaimed, and not on hold. Anything
       else refuses, and it refuses with a screen that explains which of the three failed rather
       than an error — see [reservations and
       conflicts](page:accounts/reservations-and-conflicts).`,
    ),

    section('Coming back to a claim'),
    prose(
      `A claim survives you closing the browser. Go back to the **Processing** tab and select
       **Resume** on your own row, and you are returned to the payment form with nothing lost —
       the form saves nothing until you confirm, so there is nothing to lose.`,
    ),
    tip(
      `If **Resume** takes you to a screen headed **Pick one** rather than to the form, the claim
       has been open longer than a day. That screen is a nudge, not a punishment: choose **Carry
       on and record the payment** to continue.`,
    ),

    section('Giving a claim back'),
    prose(
      `If you cannot pay a request after all, hand the claim back rather than leaving it parked.
       Releasing puts the request into the open queue again for anybody in Accounts to pick up.`,
    ),
    steps([
      {
        text: 'From the payment form, select **Release** in the reserved bar, or **Cancel and release** at the foot of the form.',
        note: 'Both open the same screen.',
      },
      {
        text: 'Type a reason. It is required, and it is recorded in the history where everybody who can see the request can read it.',
        shot: 'accounts/reservation-release',
      },
      { text: 'Tick **I confirm no payment has been initiated for this request**. It is also required.' },
      {
        text: 'Select **Release reservation**.',
        note: 'The request goes back to `approved`, and both the requester and the approver are told it is unclaimed again.',
      },
    ]),
    warning(
      `Do not release a request whose transfer you have already started in the bank portal.
       Releasing lets somebody else take it and pay it a second time. Finish recording it instead
       — that is exactly what the red banner on the release screen is warning you about.`,
      'Money already on its way',
    ),

    section('Claims that are not yours'),
    prose(
      `You can see who holds every claim, and you cannot take one off them. Handing a colleague’s
       claim to somebody else needs an administrator; if a request is stuck with somebody who is
       away, ask one. The alternative — waiting — is safe, because a claim blocks payment and
       nothing else.`,
    ),

    faq([
      {
        q: 'I selected **Take for processing** and got a screen saying a colleague took it first.',
        a: `Two of you pressed the button within moments of each other and the product gave it to
            one. Nothing was saved and no payment was created. The screen offers you the queue and
            a read-only view of the request.`,
      },
      {
        q: 'Does a claim expire on its own?',
        a: `No. After the configured number of days a reminder goes out and the queue flags it, but
            nothing is ever released automatically — a released request can be taken and paid by
            somebody else, and the product will not risk that on a timer.`,
      },
      {
        q: 'Can I claim a request that is on hold?',
        a: `No. A hold blocks payment until Accounts lifts it. Open the request, read the reply,
            and select **Release hold** first — [putting a request on
            hold](page:accounts/holding-a-request) covers both halves.`,
      },
    ]),
  ],
});
