import { page, prose, section, subsection, shot, note, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'getting-started',
  slug: 'the-dashboard',
  title: 'Your home screen',
  summary: 'What the dashboard puts in front of you, and how it changes with your role.',
  blocks: [
    prose(
      `Signing in lands you on Home, and so does **Home** at the top of the sidebar. It is not a
       report. It is the work itself: the things waiting on you, listed so you can open one and get
       on with it.`,
    ),

    section('The three parts'),
    prose(
      `Whatever your role, the screen is built from the same three pieces, top to bottom.`,
    ),
    table(
      ['Part', 'What it does'],
      [
        [
          'The greeting band',
          'Reads *Home*, then **Good day** and your name, then "Everything below is waiting on someone. The ones marked *you* are yours." **+ New request** sits on the right if you are allowed to raise requests.',
        ],
        [
          'The strip of tiles',
          'One tile per queue that concerns you, each a label and a number. Every tile is a link to the full screen behind it. The three tiles that count work waiting on you — **Needs my action**, **Awaiting my approval** and **Partial payments to review** — are tinted while they are above zero.',
        ],
        [
          'The panels',
          'One panel per queue, listing at most four rows, with a link at the foot to the whole queue.',
        ],
      ],
    ),
    prose(
      `A panel is only drawn when it has something in it, so an empty queue costs you no space —
       its tile still shows the zero. If nothing anywhere is waiting on you, you get a single panel
       reading "Nothing is waiting on you".`,
    ),

    section('A requester'),
    shot(
      'shared/dashboard-requester',
      'Somebody who only raises requests. Two tiles, two panels, and the button to start a new one.',
    ),
    prose(
      `Two tiles: **Needs my action**, tinted because it is not zero, and **My open requests**.
       Under them sit two panels. Note that the tile says *my* and the panel below it says *your*
       — they are the same queue. **Needs your action** lists your own requests that were returned
       to you for correction, and **In progress** lists everything of yours that is not finished.`,
    ),
    prose(
      `Read one row across. The number and the title come first, then the status badge, then a
       plain sentence naming whoever owes the next move — "Waiting on you" in brand colour when it
       is you, "Waiting on Manish Patel" or "Waiting on Accounts" when it is not. The amount sits
       on the right. In this screenshot two requests were returned for correction, so they appear
       in both panels: they are in progress, and they are also waiting on their raiser.`,
    ),
    note(
      `A request of yours that Accounts has put on hold reads "Waiting on you" in the **In
       progress** panel, because Accounts is waiting for your answer. It is not counted under
       **Needs my action**, which counts returned requests only, so look down the **In progress**
       panel as well.`,
    ),
    prose(
      `The foot of each panel opens the list behind it — **Open my requests →** and
       **See all my requests →**. Both land on [Tracking your
       requests](page:requester/tracking-your-requests).`,
    ),

    section('An approver'),
    shot(
      'shared/dashboard-manager',
      'An approver. The work is other people’s requests, so there is no button for raising one.',
    ),
    prose(
      `Four tiles here: **Awaiting my approval** and **Partial payments to review**, both tinted;
       **Cancellation requests**; and **Budget**, whose value is the words **Variance grid** rather
       than a count, because it is a way through to that screen rather than a queue. There is no
       **+ New request** button, because the Manager role does not carry permission to raise
       requests.`,
    ),
    prose(
      `**Awaiting your approval** lists what is sitting with you; the first row in the screenshot
       also carries an **Urgent** flag before its status. **Decisions only you can make** holds the
       requests whose raisers have asked to cancel something you already approved. **Partial
       payments to review** holds the requests Accounts paid short and sent back for you to decide.
       The feet lead to **Open the approvals queue →**, **Review all →** and **Open the partial
       reviews →**, all covered in [The approvals queue](page:manager/the-approvals-queue).`,
    ),

    section('The accounts team'),
    shot(
      'shared/dashboard-accounts',
      'The accounts team. One queue matters at the start of the day: what is approved and unclaimed.',
    ),
    prose(
      `Two tiles and, in the screenshot, one panel. The **Approved, unclaimed** tile is the number of
       approved requests that nobody has taken and that are not on hold — the same number the
       accounts queue itself shows, drawn from the same query, so the two can never disagree. The
       panel under it is headed **Approved and unclaimed** and lists the first four of them, urgent
       ones first. **Budget** appears again as a way through to the variance grid.`,
    ),
    prose(
      `A second panel, **Concerns to answer**, appears when an approver has raised a concern about a
       partial payment you recorded and still hold. It has no tile of its own. Its foot, **Open the
       partial review tab →**, leads to that tab of the queue.`,
    ),
    note(
      `Each row carries the request number and title, the status, the waiting line and the amount.
       **Open the payment queue →** at the foot leads to
       [The payment queue](page:accounts/the-payment-queue).`,
    ),

    section('An administrator'),
    shot(
      'shared/dashboard-admin',
      'An administrator sees every tile the product has, and an extra panel of destinations.',
    ),
    prose(
      `An administrator holds every permission, so every tile is drawn: all seven, wrapping onto a
       second line. Five of them read zero in this screenshot — the administrator account had
       raised nothing and was nobody's approver — and because those queues are empty, no panels are
       drawn for them. The counts are still true; there is just nothing to list.`,
    ),
    prose(
      `The **Administration** panel is the one the other built-in roles do not get. Instead of requests it
       holds four destinations, each with a line saying what it is for: **Configuration**
       ("Numbering, attachments, urgency, approvals"), **Roles & permissions** ("Who can do what"),
       **Users** ("People and their default approvers") and **Vendors** ("The vendor master"). Each
       one appears only if you may open it, so a role holding some of those permissions and not
       others gets a shorter list.`,
    ),

    section('The tiles, side by side'),
    table(
      ['Tile', 'Who sees it', 'What it counts'],
      [
        ['Needs my action', 'Anybody who may raise requests', 'Your own requests that are stuck on you'],
        ['My open requests', 'Anybody who may raise requests', 'Your own requests that are not finished'],
        ['Awaiting my approval', 'Approvers', 'Requests sent to you and not yet decided'],
        ['Cancellation requests', 'Approvers', 'Requests sent to you whose raisers have asked to cancel them after approval'],
        ['Partial payments to review', 'Approvers who may decide shortfalls', 'Requests sent to you that Accounts paid short and sent back for your decision'],
        ['Approved, unclaimed', 'The accounts team', 'Approved requests nobody has claimed and that are not on hold'],
        ['Budget', 'Anybody who may open the variance grid', 'Nothing — it is a link to the grid'],
      ],
      'Every tile the home screen can draw. You get the ones your permissions reach. The Concerns to answer panel has no tile.',
    ),
    tip(
      `The number on a tile and the number on the panel below it are the same figure, and the panel
       shows at most four rows of it. A panel headed with 130 that lists four rows is not truncating
       anything quietly — the foot link is where the rest lives.`,
    ),

    section('Reading a row'),
    prose(
      `Rows are the same shape everywhere on this screen, and the same shape they take in the
       lists. Each is a link: selecting one opens [that request in
       full](page:getting-started/a-request-in-detail).`,
    ),
    subsection('The status badge'),
    prose(
      'The badge is the request\'s state in words — "Awaiting approval", "Returned for correction", ' +
        '"Approved — awaiting payment", "Cancellation requested", "Completed". ' +
        '[Statuses and badges](page:reference/statuses-and-badges) has the full set with the colours ' +
        'the product uses, and [The request lifecycle](page:reference/request-lifecycle) shows how ' +
        'one becomes another.',
    ),
    subsection('The waiting line'),
    prose(
      `Next to the badge is a sentence naming who owes the next action. It is worked out once, in
       one place, so the home screen, the lists and the request itself always give the same answer.
       "Waiting on you" is drawn in brand colour; a finished request reads "Nothing pending".
       [Statuses and badges](page:reference/statuses-and-badges#the-waiting-line) lists every form
       it takes.`,
    ),

    faq([
      {
        q: 'A tile shows a number but there is no panel for it.',
        a: `Panels are only drawn for queues that have something in them, so this should not happen
            for a tile above zero. It does happen the other way round: a tile reading zero has no
            panel, which is the intended behaviour.`,
      },
      {
        q: 'My colleague has a queue on their home screen that I do not have.',
        a: `Every tile and every panel is gated on the same permission as the screen it leads to, so
            you are never shown work behind a door you cannot open. See [Finding your way
            around](page:getting-started/finding-your-way).`,
      },
      {
        q: 'The screen says "Nothing is waiting on you" but I know I have requests open.',
        a: `That panel appears when no queue you can act on has anything in it. Requests of yours
            that are sitting with somebody else are counted on the **My open requests** tile, and
            the tile links to the full list.`,
      },
      {
        q: 'Where is the button to raise a request?',
        a: `**+ New request**, top right of the greeting band. It is only drawn for accounts that
            may raise requests, so an approver-only or accounts-only account will not have it.`,
      },
    ]),
  ],
});
