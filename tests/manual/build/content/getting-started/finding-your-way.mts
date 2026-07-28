import { page, prose, section, subsection, shot, note, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'getting-started',
  slug: 'finding-your-way',
  title: 'Finding your way around',
  summary: 'The parts of every screen: the sidebar, the header, and why your menu may be shorter than a colleague’s.',
  blocks: [
    prose(
      `Every screen in Fervid Budget is built the same way. A dark menu runs down the left, and the
       screen you asked for fills the rest. Learn the frame once and you can find anything in the
       product without hunting for it.`,
    ),

    section('The sidebar'),
    shot(
      'shared/sidebar-admin',
      'The menu at its fullest, as an administrator sees it. Most people see a shorter version of this.',
    ),
    prose(
      `The product name sits at the top. Under it come **Home** and **Notifications**, and under
       those the menu is broken into headed groups. The entry you are currently on is highlighted —
       in this screenshot that is **Configuration**, under **Admin**.`,
    ),
    table(
      ['Group', 'What is in it'],
      [
        ['*(no heading)*', 'Home, Notifications'],
        ['Requests', 'My requests, Approvals, Accounts queue, Recoverables'],
        ['Payments', 'Payments ledger'],
        ['Budget', 'Variance grid, Budgets, Monthly plans'],
        ['Reports', 'Reports'],
        ['Masters', 'Vendors, Projects, Heads'],
        ['Admin', 'Users, Roles & permissions, Configuration, Notification rules, Audit log, Backups'],
        ['Coming soon', 'Invoices, Payments received, Inventory, Purchase orders'],
      ],
      'Every entry the sidebar can hold, in the order it draws them.',
    ),
    prose(
      `The highlight follows the area rather than the exact address. Open one request and **My
       requests** stays lit, because that is the section the request belongs to.`,
    ),

    subsection('The number beside an entry'),
    prose(
      `Some entries carry a small count. Each one counts a different thing, and an entry with
       nothing to count draws no number at all rather than a bare zero.`,
    ),
    table(
      ['Entry', 'What its number counts'],
      [
        ['Notifications', 'Notifications you have not opened yet'],
        ['Approvals', 'Requests waiting on a decision from you'],
        ['Accounts queue', 'Approved requests nobody has claimed and that are not on hold'],
        ['Payments ledger', 'Recorded payments with no document attached to them'],
        ['Monthly plans', 'Budget months that are still open'],
      ],
    ),

    subsection('Entries marked Soon'),
    prose(
      `Four entries sit under **Coming soon** in a dimmed grey with the word **Soon** beside them:
       **Invoices**, **Payments received**, **Inventory** and **Purchase orders**. These are
       announcements of screens that are not built yet, not links. Selecting one does nothing —
       deliberately, so that nobody clicks through to a dead address. Everybody sees this group,
       whatever their roles.`,
    ),

    section('Why your menu is shorter than a colleague’s'),
    prose(
      `Each entry names the permission its screen needs. If your roles do not carry that permission,
       the entry is not drawn — and if a whole group is left with nothing in it, the group heading
       disappears too. Nothing here looks at your job title; it is permissions alone.`,
    ),
    prose(
      `That is why two people at the same desk can open the same product and see menus of very
       different lengths. Taking the four roles the product ships with:`,
    ),
    table(
      ['If your roles are', 'The sidebar lists'],
      [
        [
          'Requester',
          'My requests, and nothing else. No approvals queue, no payments, no budget screens.',
        ],
        [
          'Approver',
          'My requests, Approvals, Variance grid and Reports.',
        ],
        [
          'Accounts',
          'My requests, Accounts queue, Recoverables, Payments ledger, Variance grid and Reports.',
        ],
        [
          'Administrator',
          'Every entry in the table above, including the Masters and Admin groups.',
        ],
      ],
      'Read from the four accounts used to capture the screenshots in this manual.',
    ),
    prose(
      `**Home**, **Notifications** and the **Coming soon** group need no permission, so everybody
       has those. Your organisation may also have roles of its own making, in which case your menu
       is whatever those roles add up to. [The permission matrix](page:reference/permissions-matrix)
       lists exactly which role reaches what, and
       [Roles and permissions](page:admin/roles-and-permissions) explains how an administrator
       changes it.`,
    ),

    subsection('What happens if you go there anyway'),
    prose(
      `A hidden entry is not the only lock. The address behind it is gated as well, so following a
       colleague's link, or a bookmark you kept after your roles changed, gets you a refusal rather
       than the screen.`,
    ),
    shot('shared/forbidden', 'A `403` refusal. The permission, not the record, is what is missing.'),
    prose(
      'An address that matches no record at all — a request that was never created, or a number ' +
        'typed wrongly — gives you a `404` instead.',
    ),
    shot('shared/not-found', 'A `404`. The address is fine; there is nothing behind it.'),
    note(
      `Both refusals drop the sidebar on purpose, and offer **Return to dashboard** and **Go back**
       instead. Each one also prints a request ID. If you need to report the problem, quote it —
       it is what ties your refusal to the line in the server's log. See
       [Messages and refusals](page:reference/error-messages).`,
    ),

    section('The page header'),
    prose(
      `Every screen opens with the same band: a small line in capitals saying which part of the
       product you are in, the screen's name, and one sentence explaining what it is for. Anything
       the screen wants you to do first sits on the right of the band.`,
    ),
    prose(
      `On the configuration screenshot above, that band reads *Administration* / **Configuration** /
       "The rules the request module runs on." On your home screen the name is **Good day**
       followed by your own, with **+ New request** on the right if you are allowed to raise
       requests. On the notification centre it reads *Your activity* / **Notifications**, with
       **Mark all read** on the right.`,
    ),
    note(
      `One screen does not use the band: a single request opens with the request's own head instead —
       its number, its amount and its title. [Anatomy of a
       request](page:getting-started/a-request-in-detail) takes that screen apart.`,
    ),

    section('The block at the bottom'),
    prose(
      `At the foot of the sidebar sit your initials in a circle, your name, and a short line
       beneath it. Under that is **Log out**.`,
    ),
    prose(
      `The line under your name is a property of the account itself, not the list of roles that
       decides what your menu holds. Two people can both read the same word there and still see
       different menus. To find out which roles you actually hold, ask an administrator to look you
       up on [People and accounts](page:admin/people).`,
    ),
    tip(
      `**Log out** is the only way out. There is nothing else in the sidebar that ends your session,
       so use it on any machine other people can reach.`,
    ),

    section('On a small screen'),
    prose(
      `Below roughly 860 pixels of width — a phone, or a narrow window — the sidebar is not drawn at
       all. In its place you get a bar across the top carrying a back or home control, the screen's
       name and a notification icon with your unread count on it, and a bar of tabs fixed along the
       bottom. The tabs are built from your permissions in the same way the sidebar is: **Home**,
       then your main list, then one large centre action — approving, paying or raising, whichever
       is the first of those you are allowed to do — then payments or the budget grid, then
       **More**, which opens the same grouped menu the sidebar holds. The screenshots in this
       manual were all taken on a wide screen, so they show the sidebar.`,
    ),

    faq([
      {
        q: 'An entry I used yesterday has gone.',
        a: `Your roles changed. The menu is rebuilt from your permissions on every page load, so a
            role taken away removes its entries at once. An administrator can tell you what you now
            hold.`,
      },
      {
        q: 'I can see a screen but a control on it is missing.',
        a: `Reaching a screen and acting on it are separate permissions. An approver can open a
            request, for example, and still not be offered the buttons that decide it if the request
            was not sent to them.`,
      },
      {
        q: 'What does the number beside Payments ledger mean? It is not the number of payments.',
        a: `It counts payments that have no document attached, so it goes down as receipts are
            added rather than up as payments are recorded.`,
      },
      {
        q: 'When will the Soon entries work?',
        a: `That is a question for whoever runs your installation. The product lists them so the
            shape of what is planned is visible, and each one lights up as a working link on the
            day its screen is built.`,
      },
    ]),
  ],
});
