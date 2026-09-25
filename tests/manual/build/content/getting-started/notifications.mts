import { page, prose, section, subsection, shot, note, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'getting-started',
  slug: 'notifications',
  title: 'Notifications',
  summary: 'How the product tells you something needs you, and how to catch up on what you missed.',
  blocks: [
    prose(
      `When something in the workflow changes what somebody has to do next, the product writes that
       person a notification. It is how you learn that a request landed in your queue, that an
       approver sent yours back, or that money you were waiting on has gone out — without anybody
       having to remember to tell you.`,
    ),
    prose(
      `Notifications inside the product always happen. Email is separate: an administrator decides,
       event by event, whether an email goes out as well. So an empty inbox does not mean nothing
       happened — the record is always here.`,
    ),

    section('Opening the centre'),
    prose(
      `**Notifications** sits near the top of the sidebar, under **Home**, with the number you have
       not read yet beside it. Selecting it opens the centre.`,
    ),
    shot(
      'shared/notifications',
      'The notification centre. Newest first, unread ones sitting on a tinted background.',
    ),
    prose(
      `The header band tells you where you stand: **Notifications**, and under it a count in the
       form "87 unread of 87". When you are up to date it reads "Everything here is read" instead.
       **Mark all read** sits on the right, and only appears while something is unread.`,
    ),

    section('What a row says'),
    prose(
      `Each row is one thing that happened, and reads the same way across the list.`,
    ),
    table(
      ['Part of the row', 'What it is'],
      [
        ['The mark on the left', 'A small glyph for the kind of notification this is — see the tabs below.'],
        ['The heading, in bold', 'What happened, with the request number and usually the amount and payee. "PR-2026-000576 has been paid — ₹1,000.00 to Nandini Chopra".'],
        ['The line beneath', 'The same event in a full sentence, with whatever context matters — the project and head, when the money is needed by, the reason the approver gave.'],
        ['The timestamp on the right', 'When it happened, to the minute. The list is newest first.'],
      ],
    ),
    prose(
      `The whole row is a link. Selecting one marks it read and takes you straight to the request it
       is about, so you land on the thing you have to act on rather than on a list you then have to
       search. A row with nothing to open leaves you in the centre.`,
    ),
    note(
      `The wording of these rows is not fixed by the product. An administrator writes the templates
       the messages are built from, so what you read may differ from the screenshot. See
       [Notification rules and email](page:admin/notification-rules).`,
    ),

    section('Unread and read'),
    prose(
      `An unread row sits on a tinted background. Once it has been read the tint goes and the row
       becomes plain. Nothing is deleted — read notifications stay in the list, in the same order,
       for as long as they exist.`,
    ),
    prose(
      `A notification becomes read in one of two ways: you open it, or you use **Mark all read**,
       which clears every unread row you have at once. Either way the count beside
       **Notifications** in the sidebar drops to match, because that count is your unread total and
       nothing else.`,
    ),
    tip(
      `Reading a notification is not the same as dealing with the request behind it. Clearing the
       list changes only what you have looked at. Use the queues — your home screen, the approvals
       queue, the payment queue — to know what is actually outstanding.`,
    ),

    section('The four tabs'),
    prose(
      `Below the header band is a strip of four filters, each carrying its own count. They narrow
       the same list; none of them is a separate inbox.`,
    ),
    table(
      ['Tab', 'What it holds'],
      [
        ['**All**', 'Everything you have been sent, read and unread.'],
        ['**Unread**', 'The same list with the read rows taken out.'],
        ['**Mentions**', 'The events where somebody’s words are addressed to you and something is expected back: a request returned for correction, a request put on hold, a rejection, a concern raised about a short payment, and a request cancelled by its approver.'],
        ['**Reminders**', 'The two automatic nudges the product sends by itself.'],
      ],
    ),
    prose(
      `Everything else — an approval granted, a payment settled, a reservation released, a
       withdrawal — is ordinary activity and appears under **All** and **Unread** only.`,
    ),

    subsection('The two reminders'),
    prose(
      `Reminders are not raised by a person. The product looks for work that has gone quiet and
       sends them itself.`,
    ),
    table(
      ['Reminder', 'What it is about'],
      [
        ['Pending approval', 'A request has sat with its approver longer than your organisation allows, and keeps repeating on a set cadence until it is decided.'],
        ['Stale reservation', 'Somebody in Accounts claimed a request and has recorded no payment against it since.'],
      ],
    ),
    prose(
      `How long is "too long" for each is set on the [Configuration](page:admin/configuration)
       screen, in days rather than working hours. A request put on hold is waiting on its
       raiser by design, so it is never reminded about.`,
    ),

    section('Who gets told'),
    prose(
      `Every event has its own list of recipients, and an administrator sets them: the person who
       raised the request, its approver, everybody in Accounts, and any fixed addresses on top.
       That is why a colleague can be told about something you were not, and why you may be told
       about a request you did not raise.`,
    ),
    prose(
      `Two events fire alongside the ordinary ones. A request marked urgent raises its own event as
       well as the normal one, both when it is submitted and when it is approved, so urgency can be
       announced to a different group of people. Reassigning an approval tells the approver it moved
       to. Neither changes who may decide anything — see [Marking a request
       urgent](page:requester/urgent-requests) and [Reassigning an
       approval](page:manager/reassigning).`,
    ),

    section('Older notifications'),
    prose(
      `The centre draws a hundred rows at a time. When you have more than that, a line under the
       list says which of them you are looking at — "Showing 1–100 of" your total — with
       **Older →** and, once you have moved, **← Newer** beside it. Both keep whichever tab you are
       standing in, so paging never widens the filter behind your back.`,
    ),

    section('When there is nothing'),
    prose(
      `A new account, or a tab with nothing under it, shows a single row reading "Nothing here yet —
       You will be told when something needs you". That is the whole of it: there is no separate
       archive and nothing hidden behind a setting.`,
    ),

    faq([
      {
        q: 'The number in the sidebar does not match the number of rows I can see.',
        a: `The sidebar counts unread notifications only, and the centre opens on **All**. Select
            **Unread** to see exactly what the badge is counting.`,
      },
      {
        q: 'I was told about a request and now I cannot open it.',
        a: `The notification survives changes to your roles; access does not. If your permissions
            changed after the row was written, the row stays and the request refuses you.`,
      },
      {
        q: 'I get notifications inside the product but no emails.',
        a: `Email is switched on per event, and needs a working mail server behind it. Both are an
            administrator's job — [Notification rules and
            email](page:admin/notification-rules).`,
      },
      {
        q: 'Can I turn notifications off?',
        a: `Not for yourself. In-app notifications always fire. What can be changed is who is
            listed as a recipient for each event, and that is set centrally by an administrator.`,
      },
      {
        q: 'I marked everything read by mistake.',
        a: `Nothing is lost — the rows are all still there under **All**, newest first, with their
            timestamps. There is no way to mark one unread again, so work from your queues rather
            than from the tint.`,
      },
    ]),
  ],
});
