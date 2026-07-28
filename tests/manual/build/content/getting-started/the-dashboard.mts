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
          'One tile per queue that concerns you, each a label and a number. Every tile is a link to the full screen behind it. A tile with work outstanding is tinted.',
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
       Under them, **Needs your action** collects the requests that are stuck on you, and
       **In progress** everything of yours still moving.`,
    ),
    prose(
      `Read one row across. The number and the title come first, then the status badge, then a
       plain sentence naming whoever owes the next move — "Waiting on you" in brand colour when it
       is you, "Waiting on Manish Patel" or "Waiting on Accounts" when it is not. The amount sits
       on the right. In this screenshot two requests were returned for correction, so they appear
       in both panels: they are in progress, and they are also waiting on their raiser.`,
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
      `Three tiles here: **Awaiting my approval**, tinted; **Cancellation requests**; and
       **Budget**, whose value is the words **Variance grid** rather than a count, because it is a
       way through to that screen rather than a queue. There is no **+ New request** button,
       because the approver role does not carry permission to raise requests.`,
    ),
    prose(
      `**Awaiting your approval** lists what is sitting with you; the first row in the screenshot
       also carries an **Urgent** flag before its status. **Decisions only you can make** holds the
       requests whose raisers have asked to cancel something you already approved — those cannot be
       handed to anybody else. The feet lead to **Open the approvals queue →** and **Review all →**,
       both covered in [The approvals queue](page:manager/the-approvals-queue).`,
    ),

    section('The accounts team'),
    shot(
      'shared/dashboard-accounts',
      'The accounts team. One queue matters at the start of the day: what is approved and unclaimed.',
    ),
    prose(
      `One count and one panel. **Approved, unclaimed** is the number of approved requests that
       nobody has taken and that are not on hold — the same number the accounts queue itself shows,
       drawn from the same query, so the two can never disagree. **Budget** appears again as a way
       through to the variance grid.`,
    ),
    note(
      `The rows in this panel carry the request number, the status and the amount, but no title.
       Open a row to see what the money is for. **Open the payment queue →** at the foot leads to
       [The payment queue](page:accounts/the-payment-queue).`,
    ),

    section('An administrator'),
    shot(
      'shared/dashboard-admin',
      'An administrator sees every tile the product has, and an extra panel of destinations.',
    ),
    prose(
      `An administrator holds every permission, so every tile is drawn: all six, wrapping onto a
       second line. Four of them read zero in this screenshot — the administrator account had
       raised nothing and was nobody's approver — and because those queues are empty, no panels are
       drawn for them. The counts are still true; there is just nothing to list.`,
    ),
    prose(
      `The second panel, **Administration**, is the one nobody else gets. Instead of requests it
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
        ['Cancellation requests', 'Approvers', 'Approved requests whose raisers have asked to cancel'],
        ['Approved, unclaimed', 'The accounts team', 'Approved requests nobody has claimed and that are not on hold'],
        ['Budget', 'Anybody who may open the variance grid', 'Nothing — it is a link to the grid'],
      ],
      'Every tile the home screen can draw. You get the ones your permissions reach.',
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
       "Waiting on you" is drawn in brand colour; a finished request reads "Nothing pending".`,
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
