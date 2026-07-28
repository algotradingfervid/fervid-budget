import { page, prose, section, shot, steps, note, warning, table, faq } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'what-you-can-do',
  title: 'What the accounts role can do',
  summary: 'The settlement job end to end, and what is deliberately out of your hands.',
  blocks: [
    prose(
      `The accounts role turns approved requests into money that has left the bank. Everything
       you do starts from a request somebody else raised and somebody else approved: there is no
       way to create a payment out of nothing, and no screen that offers one.

       The shape of the day is the same every day. Find the request, claim it so nobody else
       pays it too, record what actually left the bank, and say whether that settles the
       obligation. The product holds you to that order.`,
    ),

    shot('shared/dashboard-accounts', 'The home screen for the accounts team. One work area — **Approved and unclaimed** — and the requests at the head of it.'),

    section('The job, end to end'),
    steps([
      {
        text: 'Open [the payment queue](page:accounts/the-payment-queue) and find the request.',
        note: 'The queue is also the sidebar item **Accounts queue**, and its badge is the number of approved requests nobody has claimed.',
      },
      {
        text: 'Select **Take for processing**, which [claims the request](page:accounts/claiming-a-request) for you.',
        note: 'From that moment nobody else can pay it. It shows as `processing` and the queue names you as the holder.',
      },
      { text: '[Record the payment](page:accounts/recording-a-payment) — the amount, the date, the mode and the transaction reference.' },
      {
        text: 'Confirm on the second step whether this is **Fully settled** or a **Partial payment**.',
        note: 'A full settlement closes the request. A partial one hands the difference to the approver — see [paying part of a request](page:accounts/paying-part-of-a-request).',
      },
      { text: 'The payment lands in [the payments ledger](page:accounts/the-payments-ledger), where it can be read but not changed.' },
    ]),

    section('What you may do'),
    table(
      ['You can', 'Where', 'What to read'],
      [
        ['Work the payment queue and search it', 'Accounts queue', '[The payment queue](page:accounts/the-payment-queue)'],
        ['Claim an approved request, and hand your own claim back', 'Accounts queue, or the request', '[Taking a request for processing](page:accounts/claiming-a-request)'],
        ['Record one payment against one approved request', 'The payment form', '[Recording a payment](page:accounts/recording-a-payment)'],
        ['Settle short of the approved amount, with a reason', 'The confirmation step', '[Paying part of a request](page:accounts/paying-part-of-a-request)'],
        ['Park a request you cannot pay yet, and take it off hold again', 'The request', '[Putting a request on hold](page:accounts/holding-a-request)'],
        ['Comment on any request you can see', 'The request', '[Raising a concern](page:accounts/raising-a-concern)'],
        ['Attach the bank advice as you settle a payment', 'The payment form', '[Recording a payment](page:accounts/recording-a-payment)'],
        ['Read every payment ever recorded', 'Payments ledger', '[The payments ledger](page:accounts/the-payments-ledger)'],
        ['Track money the organisation expects back', 'Recoverables', '[The recoverables register](page:accounts/the-recoverables-register)'],
        ['Read the variance grid and the reports', 'Variance grid, Reports', '—'],
      ],
    ),

    section('What is deliberately not yours'),
    prose(
      `Several things you might expect to find are missing on purpose. Each one belongs to
       somebody else, and the product will not let the accounts role reach around them.`,
    ),
    table(
      ['Not yours', 'Whose it is'],
      [
        ['Approving, returning or rejecting a request', 'The approver it was routed to. You never see an approve control at all.'],
        ['Raising a request', 'The requester. The accounts role holds no create permission, so **My requests** lists what you can see rather than what you can start.'],
        ['Deciding a shortfall you recorded', 'The request’s own approver — see [reviewing a shortfall](page:manager/shortfall-review).'],
        ['Taking a claim off a colleague', 'An administrator. You can release a claim you hold; you cannot take one you do not.'],
        ['Changing a payment once it is saved', 'Nobody. A payment tied to a request is sealed — see [correcting or voiding a payment](page:accounts/correcting-a-payment).'],
        ['Recording money coming back on a recoverable', 'Out of scope in this version, and the register says so on screen.'],
      ],
    ),
    note(
      `Two accounts colleagues can still see different rows. What a role may *do* is one thing;
       which records it may *see* is a separate setting per role. [The permission
       matrix](page:reference/permissions-matrix) lists both as the running system has them.`,
    ),

    warning(
      `Confirming a payment saves it, and nothing afterwards can edit or cancel it. Check the
       amount, the date and the reference on the confirmation step before you press
       **Confirm and save payment**.`,
      'The one irreversible step',
    ),

    faq([
      {
        q: 'Where is the button to add a payment that has no request behind it?',
        a: `There is not one. Opening the payment form without naming a request shows a picker of
            approved requests and a note that free payment entry has been removed. Payments recorded
            before that change are still in the ledger as history.`,
      },
      {
        q: 'A request I need to pay is not in the queue.',
        a: `It is either not approved yet, on hold, or somebody has already claimed it. The tabs
            across the top of the queue split exactly those cases —
            [the payment queue](page:accounts/the-payment-queue) explains each one.`,
      },
      {
        q: 'I paid the vendor more than was approved.',
        a: `The form refuses an amount above the approved figure and says so. Paying more is a
            different obligation: the request is cancelled and a new one is raised for the right
            amount.`,
      },
    ]),
  ],
});
