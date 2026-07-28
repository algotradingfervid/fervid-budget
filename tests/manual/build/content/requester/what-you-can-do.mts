import { page, prose, section, shot, note, table, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'what-you-can-do',
  title: 'What a requester can do',
  summary: 'The whole job in one page: what you may raise, see and change.',
  blocks: [
    prose(
      `A requester is anybody who needs the organisation to pay money out. You raise a request,
       answer whatever your approver asks, and follow it through to the payment. You do not decide
       your own requests and you do not record payments — those are somebody else's job, and the
       product enforces the split rather than trusting you to remember it.`,
    ),

    section('Your home screen'),
    shot('shared/dashboard-requester', 'The dashboard as a requester sees it: two panels, both about your own work.'),
    prose(
      `The line under your name says it plainly — *Everything below is waiting on someone. The ones
       marked you are yours.* **Needs my action** counts the requests that cannot move until you do
       something; **My open requests** counts everything of yours that is still live. The two panels
       below list those same sets as cards — **Needs your action**, ending in **Open my requests →**,
       and **In progress**, ending in **See all my requests →**. Both tiles are links to the matching
       tab of [My requests](page:requester/tracking-your-requests).`,
    ),
    prose(
      `The sidebar is short: **Home**, **Notifications**, and **My requests** under the *Requests*
       heading. Below that sits a *Coming soon* group — **Invoices**, **Payments received**,
       **Inventory** and **Purchase orders** — each marked \`Soon\`. Those are not screens yet.`,
    ),

    section('The nine things you may do'),
    table(
      ['You may', 'Where', 'Read more'],
      [
        ['Raise a request', '**+ New request**, on the dashboard and on the list', '[Raising a request](page:requester/raising-a-request)'],
        ['See your own requests', '**My requests**', '[Tracking your requests](page:requester/tracking-your-requests)'],
        ['Edit a request', 'While it is `pending` or `returned`, and only your own', '[Changing or withdrawing](page:requester/editing-and-withdrawing)'],
        ['Withdraw a request', 'While it is `pending`', '[Changing or withdrawing](page:requester/editing-and-withdrawing)'],
        ['Ask for a cancellation', 'Once it is `approved` — you ask, you do not act', '[Asking to cancel](page:requester/asking-to-cancel)'],
        ['Raise a rejected request again', 'On a `rejected` request, as a brand-new request', '[After a rejection](page:requester/after-a-rejection)'],
        ['Comment', 'The conversation box on any request you can see', '[Anatomy of a request](page:getting-started/a-request-in-detail)'],
        ['Attach a document', 'On the new-request form and on the edit form', '[Attaching documents](page:requester/attachments)'],
        ['Download a document', 'From the Attachments list on a request', '[Attaching documents](page:requester/attachments)'],
      ],
      'Taken from the permissions the Requester role actually carries in the running system.',
    ),

    section('What you can see'),
    prose(
      `Your view of the product is scoped to your own work. **My requests** lists the requests you
       raised and nothing else, and that is not a filter you can widen — the scope is fixed on the
       role, so a colleague's request is out of reach even if you know its address.`,
    ),
    note(
      `Opening a request that is not yours answers *Not Found*, not *Forbidden*. That is deliberate:
       a refusal that distinguished "exists but is not yours" from "does not exist" would let anybody
       count the organisation's requests by walking through the addresses.`,
    ),

    section('What you cannot do'),
    prose(
      `You hold no approval and no payment permissions at all. You cannot approve — not your own
       request, not anybody's — you cannot return or reject, you cannot record a payment, and the
       variance grid, the approvals queue and the payments ledger are not on your menu.`,
    ),
    shot('shared/forbidden', 'Following a link to a screen your role does not reach. The page names the status, the reason and a request ID.'),
    prose(
      `The **Request ID** on that screen is worth copying if you report the problem: it is what an
       administrator uses to find the attempt in the log.`,
    ),
    prose(
      `Self-approval is not a setting anybody can turn on. Your own name never appears in the
       **Approver** list on the request form, and the store refuses the submission a second time if
       it arrives anyway.`,
    ),

    section('Where a request goes after you'),
    prose(
      `Once you submit, the request belongs to a chain of people. Your approver decides it; the
       accounts team pays it. You keep the right to comment throughout, and the history on the
       request records every step in one stream that everybody who can see the request also sees.`,
    ),
    shot('requester/requests-needs-me', 'The Needs me tab: the only view that answers "what is mine to do".'),

    faq([
      {
        q: 'A colleague sent me a link to their request and it says Not Found.',
        a: `That is the scope rule, not a broken link. Ask them to tell you what it says, or ask an
            administrator whether your role should be wider.`,
      },
      {
        q: 'Can I approve a small request myself to save time?',
        a: `No. There is no threshold below which self-approval is allowed, and the control to do it
            does not exist. Choose an approver from the list on the form.`,
      },
      {
        q: 'Why is my menu shorter than my colleague’s?',
        a: `Because each entry is tied to a permission and your roles carry fewer of them. See
            [the permission matrix](page:reference/permissions-matrix) for exactly which role reaches what.`,
      },
    ]),
  ],
});
