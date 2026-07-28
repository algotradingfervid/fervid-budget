import { page, prose, section, shot, table, note, tip, faq } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'what-you-can-do',
  title: 'What an approver can do',
  summary: 'The five decisions available to you, and the limits of the role.',
  blocks: [
    prose(
      `An approver decides whether money is allowed to leave. You do not raise the requests and you
       do not pay them — somebody asks, you decide, and the accounts team settles what you approved.

       The product routes each request to one person by name. That person is the request's approver,
       and while the request waits, it waits on them. Your home screen opens on what is waiting on
       you.`,
    ),
    shot('shared/dashboard-manager', 'The home screen for an approver: what needs deciding, and nothing else.'),

    section('The decisions you take'),
    prose(
      `Every decision is taken on one request at a time, from the request's own screen. There is no
       bulk approval — the approvals queue says so at the foot of the page.`,
    ),
    table(
      ['Decision', 'The control', 'Offered when', 'Where it leaves the request'],
      [
        ['Approve', '**Approve ₹…**', 'The request is `pending`', '`approved` — Accounts may now pay it'],
        ['Return for correction', '**Return for correction**', 'The request is `pending`', '`returned` — back with the requester to fix and send again'],
        ['Reject', '**Reject**', 'The request is `pending`', '`rejected` — closed for good'],
        ['Reassign', '**Reassign approval**', 'The request is `pending` and another approver exists', 'Still `pending`, but routed to a different approver'],
        ['Decide a cancellation', '**Decide the cancellation**', 'The requester has asked to cancel', '`cancelled`, or back to `approved`'],
        ['Cancel outright', '**Cancel with reason**', 'The request is `approved` and nobody has asked', '`cancelled`'],
        ['Review a shortfall', '**Decide the partial payment**', 'Accounts paid less than you approved', 'Closed as `completed_partial`, or left open with your concern recorded'],
      ],
      'The seven controls an approver can be offered, and what each one does.',
    ),
    note(
      `Every one of these is a two-press action. The control in the bar along the bottom opens a
       panel; nothing is written until you press the button inside that panel. Closing the panel
       changes nothing.`,
    ),

    section('Whose decision it is'),
    prose(
      `The action panels appear only for the person the request was routed to. Holding the approver
       role is not enough: if you open somebody else's request you see the whole thing — the
       amounts, the purpose, the conversation — with no action bar at the bottom.

       Reassignment is the one exception, and it is deliberate. It is offered to anybody who holds
       the reassign permission, because it is the way out when the routed approver has left or can
       no longer act. [Reassigning an approval](page:manager/reassigning) explains it.`,
    ),
    shot('manager/request-pending', 'A request routed to you. The four controls sit along the bottom.'),

    section('What you can see'),
    prose(
      `Your request scope is the whole organisation, so **My requests** in the sidebar opens every
       request in the product, not only the ones addressed to you — the heading on that screen reads
       *All requests* to say so. The approvals queue is the narrower view: only the rows where you
       are the approver.

       Two reporting screens come with the role. [The variance grid](page:manager/the-variance-grid)
       shows budget against actual for every head in a month, and [Reports](page:manager/reports)
       roll the same figures up by month, by project or by head. Both are worth a look before you
       approve something large.`,
    ),

    section('What the role does not carry'),
    prose(
      `You cannot raise a request, record a payment, open the payments ledger, see the vendor master
       or the recoverables register, or download the documents attached to a request. None of that
       is a fault or a setting on your own account — the approver role does not hold those
       permissions. [What an approver cannot see](page:manager/limits-of-the-role) sets out each one
       and what to do instead.`,
    ),
    tip(
      `If a request is fixable, return it. Returning keeps the number, the history and the thread,
       and lets the requester correct it. Rejection cannot be undone.`,
    ),

    faq([
      {
        q: 'I can see a request but there are no buttons at the bottom.',
        a: `Then you are not its approver. Look at the **Approver** field in the Details block — that
            names the person who has to decide. If they cannot,
            [reassign it](page:manager/reassigning).`,
      },
      {
        q: 'Can I approve a request I raised myself?',
        a: `No. The product refuses a self-approval outright, and the approver list a requester
            chooses from never contains their own name.`,
      },
      {
        q: 'Where do I find a request that has already been paid?',
        a: `In **My requests**, which reaches every request. The approvals queue stops at approved,
            rejected and cancelled — see [The approvals queue](page:manager/the-approvals-queue).`,
      },
    ]),
  ],
});
