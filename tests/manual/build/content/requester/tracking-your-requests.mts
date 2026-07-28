import { page, prose, section, subsection, shot, note, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'tracking-your-requests',
  title: 'Tracking your requests',
  summary: 'The list, its tabs and filters, and how to tell at a glance what is waiting on you.',
  blocks: [
    prose(
      `**My requests** is the one screen you will live in. It holds every request you have ever raised,
       divided into four tabs, and every card on it ends with a plain sentence naming whoever owes the
       next move.`,
    ),

    section('The screen'),
    shot('requester/requests-open', 'My requests on the Open tab. The line under the heading says how many are shown and how they are sorted.'),
    prose(
      `The heading is followed by *N shown · sorted by who is holding them up*. That sort is literal:
       urgent requests come first, and after them the oldest, because the request nobody has touched
       for a fortnight is the one that needs finding. At the top right sit **⤓ Export CSV** and
       **+ New request**.`,
    ),

    section('The four tabs'),
    prose(
      `Each tab is its own address, so a view can be bookmarked or pasted to somebody. The number
       beside each label is the count for that tab under the filters currently applied.`,
    ),
    table(
      ['Tab', 'What is in it'],
      [
        ['**Open**', 'Everything still live: awaiting approval, returned to you, approved and waiting for payment, and anything with a cancellation pending.'],
        ['**Needs me**', 'Only what cannot move until you act — for a requester, the requests your approver returned.'],
        ['**Closed**', 'Rejected, withdrawn and cancelled requests. Finished without a payment.'],
        ['**All**', 'Every request you have raised, whatever its state.'],
      ],
    ),
    note(
      `**Open** and **Closed** do not add up to **All**. Requests being paid or already paid — *With
       Accounts*, *Partial — manager review*, *Completed* — are in neither, because they are neither
       still open to change nor closed without a payment. In the screenshot above, 16 open and 6 closed
       sit inside a total of 26.`,
    ),

    subsection('Needs me'),
    shot('requester/requests-needs-me', 'The Needs me tab. Two returned requests, both saying Waiting on you.'),
    prose(
      `This is the tab worth checking first. For somebody who only raises requests it collects the ones
       your approver sent back for correction, and nothing else — an approved request waiting on
       Accounts is not yours to move, so it does not appear here.`,
    ),

    subsection('Closed'),
    shot('requester/requests-closed', 'The Closed tab, mixing rejections and withdrawals. The sentence at the right of each card says which is which.'),

    subsection('All'),
    shot('requester/requests-all', 'The All tab. Live, closed and finished requests in one list, oldest first.'),

    section('Reading a card'),
    prose(
      `Each card carries, in order: the request number at the left and the amount at the right; the
       short title you gave it; a meta line naming the type and, where there is one, the project, the
       head and the invoice number; and a foot with the status pill on the left and the waiting
       sentence on the right. A recoverable request also shows a violet **Recoverable** pill naming its
       category, and an urgent one a red **Urgent** pill.`,
    ),
    table(
      ['Status pill', 'What it means', 'Waiting sentence'],
      [
        ['**Awaiting approval**', 'Submitted, nobody has decided it yet.', 'Waiting on your approver, by name'],
        ['**Returned for correction**', 'Your approver sent it back with a reason.', 'Waiting on you'],
        ['**Approved — awaiting payment**', 'Cleared approval, nobody in Accounts has claimed it.', 'Waiting on Accounts'],
        ['**Cancellation requested**', 'You asked for it to be cancelled; payment is frozen.', 'Waiting on your approver, by name'],
        ['**With Accounts**', 'An accountant has claimed it and is working on it.', 'Waiting on Accounts'],
        ['**Partial — manager review**', 'Less was paid than approved; your approver is deciding about the difference.', 'Waiting on your approver, by name'],
        ['**Completed**', 'Paid in full. Nothing left to do.', 'Nothing pending'],
        ['**Completed — partial accepted**', 'Paid short, and the approver accepted the shortfall.', 'Nothing pending'],
        ['**Rejected — final**', 'Refused. It cannot be revived.', 'Closed. Raise a new request if needed'],
        ['**Withdrawn**', 'You took it back before anybody decided it.', 'Withdrawn by the requester'],
        ['**Cancelled**', 'Killed after approval. Nothing will be paid.', 'Cancelled. Nothing can be paid against it'],
      ],
      'The full set is in [The request lifecycle](page:reference/request-lifecycle).',
    ),
    note(
      `A hold is not a status. When Accounts park an approved request, its card still reads **Approved
       — awaiting payment**, but the sentence beside it changes to **Waiting on you**. The **On hold**
       pill and the question they asked appear when you open the request. See
       [Getting paid](page:requester/getting-paid).`,
    ),

    section('Searching and filtering'),
    prose(
      `Above the tabs is a toolbar with three controls and an **Apply** button. **Search** matches the
       request number, the payee, the invoice number, the purpose, the short title and the requester's
       name — its placeholder lists most of them: *Number, payee, invoice, purpose…*. **Type** narrows
       to one of the four request types. **Treatment** narrows to **Budget expense** or
       **Recoverable**.`,
    ),
    shot('requester/requests-search-empty', 'A search that matched nothing. Every tab count drops to zero and the list says so.'),
    prose(
      `When nothing matches, the body reads **No requests match. Try another tab, or clear the
       filters.** Note that the tab counts fall to zero with it — the counts are computed under the
       same filters as the rows, so a tab can never promise a number the list does not show.`,
    ),
    tip(
      `Filters survive when you change tabs, and they are in the address. If a colleague asks what
       happened to a particular invoice, search for the invoice number on **All** and send them the
       resulting address.`,
    ),

    section('When the list is longer than the page'),
    prose(
      `A very long list is drawn in pages. The sub-line then reads *N of M shown*, and a row appears
       beneath the cards saying **Showing 1–200 of 214** with **← Newer** and **Older →** buttons. Both
       carry every filter with them, so paging never quietly widens the set.`,
    ),

    section('Exporting'),
    prose(
      `**⤓ Export CSV** downloads the list as a spreadsheet file with one row per request and these
       columns: Number, Status, Type, Title, Amount, Payee, Requester, Approver, Created. The export
       carries every matching row — it is never truncated the way the screen is.`,
    ),
    note(
      `The export follows the tab you are on and the text in **Search**, but not the **Type** and
       **Treatment** dropdowns. If those matter to you, filter the spreadsheet afterwards.`,
    ),

    faq([
      {
        q: 'A request I raised is missing from every tab.',
        a: `Clear the search box and the two dropdowns, then look at **All**. The counts drop with the
            filters, so an active filter can empty a tab completely.`,
      },
      {
        q: 'I cannot see a colleague’s request.',
        a: `You are not meant to. A requester sees their own requests and nothing else, and an address
            pointing at somebody else’s answers *Not Found*.`,
      },
      {
        q: 'Why is an old request above a new one?',
        a: `Because the list is sorted by who has been kept waiting longest — oldest first, with urgent
            requests ahead of everything. It is a work queue, not a diary.`,
      },
      {
        q: 'What is the difference between Withdrawn and Cancelled?',
        a: `You withdrew it yourself before anybody decided; a cancellation happens after approval and
            is your approver’s decision. See [Asking to cancel an approved request](page:requester/asking-to-cancel).`,
      },
    ]),
  ],
});
