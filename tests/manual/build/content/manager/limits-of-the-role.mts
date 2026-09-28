import { page, prose, section, shot, table, note, warning, faq } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'limits-of-the-role',
  title: 'What an approver cannot see',
  summary: 'The screens the approver role does not reach, and what to do when you need them.',
  blocks: [
    prose(
      `The approver role is deliberately narrow. It carries the decisions on a request, the documents
       attached to requests, the variance grid and the reports — and nothing else. This page sets out what it does not reach, so you can
       tell a permission from a fault and know who to ask.`,
    ),

    section('The whole of the role'),
    prose(
      `These are every permission the built-in approver role holds, as the product sets it up.
       An administrator can change the role, so yours may differ:`,
    ),
    table(
      ['Permission', 'What it lets you do'],
      [
        ['`approval:approve`', 'Approve a pending request'],
        ['`approval:return`', 'Return a pending request for correction'],
        ['`approval:reject`', 'Reject a pending request'],
        ['`approval:reassign`', 'Hand a pending approval to a different approver'],
        ['`approval:cancel`', 'Decide a cancellation request, or cancel an approved request'],
        ['`approval:accept_partial`', 'Close a shortfall review, or raise a concern about one'],
        ['`request:view`', 'Read any request in the organisation'],
        ['`request:comment`', 'Post in a request’s conversation'],
        ['`attachment:view`', 'Download the documents attached to a request, and the proof on a payment you are reviewing'],
        ['`grid:view`', 'Open the variance grid'],
        ['`report:view`', 'Open the reports'],
      ],
      'The approver role, in full.',
    ),
    prose(
      `Anything not on that list is closed to you. Your sidebar is built from the same list, which is
       why it is shorter than a colleague's: **Home**, **Notifications**, **My requests**,
       **Approvals**, **Variance grid** and **Reports**, plus the four *Soon* announcements
       everybody sees.`,
    ),

    section('The payments ledger'),
    prose(
      `The approver role holds no payment permission of any kind. There is no **Payments ledger** entry
       in your sidebar, and following a link to the payments ledger — or to any single payment — answers
       with the Forbidden screen.`,
    ),
    shot('manager/payments-refused', 'An approver opening the payments ledger. The role does not carry that permission.'),
    prose(
      `The screen gives you a code, a sentence — *You do not have permission to perform this action.*
       — and a request ID worth quoting if you report it. **Return to dashboard** and **Go back** are
       the only two ways on.

       The screens you use do not link you there — payment numbers on a request are plain text for
       you, and the grid and the reports leave out their payment lists — so you meet it only by
       following a bookmarked or pasted link to a payment.`,
    ),
    note(
      `You are not cut off from payment facts entirely. A request that has been paid shows its
       **Payment outcome** on the request itself — approved against paid, and the difference — and
       lists each payment's number, date, amount and reference under **Payment history**. A
       shortfall review shows the payment in detail, including its mode and reference, and any
       proof of payment Accounts attached. What you cannot open is the ledger of all payments.`,
    ),

    section('The documents on a request you are approving'),
    prose(
      `You can read them. On any request you can see, the Attachments section lists each document
       by name, size and date with a **Download** button beside it. The same holds for the proof of
       payment on a shortfall review.

       If there is nothing to download, no document was attached. Ask for it in the request’s
       conversation, or [return the request](page:manager/returning-for-correction) and say what you
       need.`,
    ),
    note(
      `If you see no **Download** button beside a document, your account's roles do not carry the
       attachment permission — for example because your organisation changed the approver role or
       gave you a custom one. An administrator can check it in
       [Roles and permissions](page:admin/roles-and-permissions).`,
    ),

    section('The masters and the registers'),
    table(
      ['Screen', 'Why it is closed to you', 'Who to ask'],
      [
        ['The vendor master and vendor bank details', 'Needs the vendor permission, which the approver role does not hold. The payee name on the request is all you get.', 'An administrator — see [Vendors](page:admin/vendors)'],
        ['The recoverables register', 'Needs the recoverable-report permission. You can see that a request is marked recoverable, but not the register of what is outstanding.', 'The accounts team, or an administrator'],
        ['Budgets, projects and expense heads', 'These are administration screens. You see their effect in the grid, not the settings behind them.', 'An administrator'],
        ['Monthly plans and locking', 'Only an administrator opens and closes a month, which is why a locked month refuses your approval rather than letting you unlock it.', 'An administrator — see [Monthly plans and locking](page:admin/monthly-plans-and-locking)'],
        ['The audit trail', 'A separate permission again. The history on each request is your view of the record.', 'An administrator'],
      ],
    ),

    section('Things you might expect to be able to do and cannot'),
    table(
      ['', 'Why not'],
      [
        ['Raise a request of your own', 'The approver role carries no create permission. If you also need to raise requests, ask for the requester role to be added to your account.'],
        ['Record a payment', 'Settlement belongs to the accounts team. Approving is where your part ends.'],
        ['Export the reports or the variance grid', 'Both exports need their own permission, which the approver role does not hold, so no export control is drawn on either screen. The approvals queue and **My requests** do offer **Export CSV**.'],
        ['Change a request’s project, head or amount before approving', 'An approver changes only the amount approved, and only downwards. For anything else, return the request.'],
        ['Undo a rejection', 'Rejection is final by design. A new request has to be raised.'],
        ['Approve on somebody else’s behalf', 'Only the approver named on the request can decide it. If they cannot act, they or an administrator can [reassign it](page:manager/reassigning).'],
        ['Approve your own request', 'Refused outright, and a requester is never offered their own name as approver.'],
      ],
    ),

    section('Reading a refusal'),
    prose(
      `Two different refusals mean two different things, and it is worth telling them apart before you
       ask for help.`,
    ),
    table(
      ['What you see', 'What it means'],
      [
        ['The **Forbidden** screen after following a link to a screen', 'Your role does not reach that screen at all. Nothing about the record is the problem.'],
        ['The **Forbidden** screen after pressing a button on a request', 'You reached the request, but you are not the person entitled to take that action on it — usually because you are not its approver. The sentence is usually *You do not have permission to perform this action.*; some actions name the reason, such as *Only the approver this request was sent to can decide its cancellation.*'],
        ['*The requested record was not found.*', 'The record is outside what your account may see, or it does not exist. Approving a request that is not routed to you also answers this way.'],
      ],
    ),
    prose(
      `[The permission matrix](page:reference/permissions-matrix) sets out all four built-in roles
       side by side, and [Messages and refusals](page:reference/error-messages) explains what the
       product says when it will not do something.`,
    ),

    faq([
      {
        q: 'A colleague with the same job title can see the payments ledger and I cannot.',
        a: `Then their account holds a second role. Roles stack: somebody who is both an approver and
            an accountant reaches both sets of screens. An administrator can tell you what your
            account carries.`,
      },
      {
        q: 'I am being asked to approve an invoice that is not attached.',
        a: `Ask for it in the request’s conversation, or return the request. Approving a figure you
            have not been able to check is a decision to make deliberately, not by default.`,
      },
      {
        q: 'Can I be given more permissions?',
        a: `Yes, but by an administrator and as a change to a role or to your account's roles — not
            from any screen you can reach. See
            [Roles and permissions](page:admin/roles-and-permissions).`,
      },
    ]),
  ],
});
