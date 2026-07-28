import { page, prose, section, subsection, shot, steps, note, warning, tip, table, fields, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'notification-rules',
  title: 'Notification rules and email',
  summary: 'Deciding who is told what, and pointing the product at a mail server.',
  blocks: [
    prose(
      'The product announces 23 things that can happen to a request. Each announcement has a rule ' +
      'behind it saying who is told and what the message says, and this screen is where those rules ' +
      'live.\n\n' +
      'The sub-line states the split the whole screen is built on: in-app notifications always fire; ' +
      'email is opt-in per event. Nobody can be cut out of the in-app notification centre, and nobody ' +
      'gets an email until you switch that event on.',
    ),
    note(
      'This is not the same screen as the bell in your own sidebar. **Notification rules** is the ' +
      'editor; [Notifications](page:getting-started/notifications) is your own centre, showing what ' +
      'the product has told you.',
      'Two different screens',
    ),

    section('Email delivery'),
    shot('admin/notification-rules', 'The rules screen. Mail settings at the top, then one row per event.'),
    prose(
      'Nothing is emailed anywhere until the product knows about a mail server. The **Email delivery** ' +
      'block at the top is where you tell it.',
    ),
    fields([
      { name: 'SMTP host', required: false, note: 'The mail server. The placeholder shows the shape — `smtp.example.com`.' },
      { name: 'Port', required: false, note: 'The port the server listens on.' },
      { name: 'Username', required: false, note: 'The account the product authenticates as.' },
      { name: 'From name', required: false, note: 'The display name on the messages the product sends.' },
      { name: 'From address', required: false, note: 'The address the messages come from.' },
      { name: 'Base URL for links', required: false, note: 'Where the product is reachable, so the links inside an email go somewhere. The placeholder is `https://budget.example.com`.' },
      { name: 'Management copy list', required: false, note: 'Comma-separated addresses. The hint says exactly when they are used: "Copied on approvals and on urgent requests once they are approved."' },
    ]),
    warning(
      'There is no password field on this screen, and its absence is deliberate. The SMTP password is ' +
      'read from the `FERVID_SMTP_PASSWORD` environment variable on the server and is never stored in ' +
      'the database. If mail is not going out and everything here looks right, that variable is the ' +
      'first thing to check with whoever runs the server.',
      'Where the password lives',
    ),
    steps([
      { text: 'Fill in the mail server fields.' },
      { text: 'Select **Save email settings**.' },
      {
        text: 'Type an address in **Send a test email to** and select **Send a test email**.',
        note: 'Your own address is filled in already. A successful send reports "Test email sent to …"; a failure reports the transport error rather than pretending it worked.',
      },
    ]),
    tip(
      'Send the test before you enable email on any event. A test that fails costs you nothing; ' +
      'twenty-three events quietly failing to deliver costs you a week of people saying they were ' +
      'never told.',
    ),

    section('The events'),
    prose('The table below the mail settings has one row per event. There are 23 of them.'),
    table(
      ['Column', 'What it shows'],
      [
        ['Event', 'The event in words, with its internal name underneath — `request_submitted` and so on.'],
        ['In-app', 'Always `On`. It is a fixed pill, not a control, because in-app delivery always fires and a switch that did nothing would be a lie.'],
        ['Email', '`On` or `Off` for this event.'],
        ['Goes to', 'Who is told, in words: `Approver`, `Requester`, `Requester + approver`, `Approver + assigned accountant`, and so on.'],
        ['Fixed To / CC', 'Any addresses this event always goes to on top of the audience, or an em dash.'],
        ['Edit', 'Opens the rule editor for that event.'],
      ],
    ),

    subsection('What the 23 events are'),
    table(
      ['Event', 'Internal name', 'Goes to'],
      [
        ['Request submitted', '`request_submitted`', 'Approver'],
        ['Request edited before approval', '`request_edited`', 'Approver'],
        ['Returned for correction', '`request_returned`', 'Requester'],
        ['Rejected', '`request_rejected`', 'Requester'],
        ['Approved', '`request_approved`', 'Requester + Accounts group'],
        ['Urgent request raised', '`request_urgent`', 'Approver immediately, Accounts on approval'],
        ['Put on hold', '`request_on_hold`', 'Requester'],
        ['Cancellation requested', '`request_cancellation_requested`', 'Approver + assigned accountant'],
        ['Payment recorded and settled', '`payment_settled`', 'Requester + approver'],
        ['Partial payment sent for review', '`payment_partial_review`', 'Approver'],
        ['Pending reminder', '`reminder_pending`', 'Whoever it is waiting on'],
        ['Stale reservation', '`reminder_stale_reservation`', 'Assigned accountant'],
        ['Withdrawn by the requester', '`request_withdrawn`', 'Approver'],
        ['Raised again after a rejection', '`request_reraised`', 'Approver'],
        ['Hold lifted', '`request_unheld`', 'Requester'],
        ['Reservation released', '`reservation_released`', 'Requester + approver'],
        ['Reservation handed to someone else', '`reservation_reassigned`', 'Requester + approver + the new assignee'],
        ['Partial payment accepted', '`payment_partial_accepted`', 'Requester + assigned accountant'],
        ['Concern raised about a partial payment', '`payment_partial_concern`', 'Assigned accountant'],
        ['Cancellation accepted', '`request_cancellation_accepted`', 'Requester + assigned accountant'],
        ['Cancellation declined', '`request_cancellation_declined`', 'Requester + assigned accountant'],
        ['Sent to a different approver', '`approval_reassigned`', 'The new approver'],
        ['Cancelled by the approver', '`request_cancelled`', 'Requester'],
      ],
      'The event catalogue, in the order the screen lists it. The "Goes to" column is the seeded audience — you can change it.',
    ),

    section('Editing one rule'),
    shot('admin/notification-rule-sheet', 'The editor for one event, opened on Request submitted.'),
    steps([
      { text: 'Select **Edit** on the event’s row.' },
      {
        text: 'Tick **Also send an email for this event** if you want mail as well as the in-app notification.',
        note: 'Leaving it clear does not silence the event. The in-app notification still fires.',
      },
      {
        text: 'Choose the audience under **Who it goes to**.',
        note: 'Three tick boxes: **The requester**, **The approver** and **The Accounts group**. They are resolved per request, so "the approver" means whoever is actually approving that one.',
      },
      {
        text: 'Add fixed addresses in **Always also send To** and **Always copy (Cc)** if the event has a standing audience.',
        note: 'Comma-separated. These are added to the audience above rather than replacing it.',
      },
      { text: 'Edit the **Subject** and the **Message**.' },
      {
        text: 'Select **Save rule**.',
        note: 'The panel closes and the row updates. **Cancel** abandons the change.',
      },
    ]),

    subsection('Writing the subject and message'),
    prose(
      'The wording is yours. Placeholders in double braces are replaced with the request’s own values ' +
      'when the message goes out. The fourteen available are listed under the message box on the ' +
      'screen itself:',
    ),
    table(
      ['Placeholder', 'What it becomes'],
      [
        ['`{{number}}`', 'The request number.'],
        ['`{{amount}}`', 'The amount asked for.'],
        ['`{{approved_amount}}`', 'The amount actually approved.'],
        ['`{{payee}}`', 'Who is being paid.'],
        ['`{{requester}}`', 'Who raised it.'],
        ['`{{approver}}`', 'Who is deciding it.'],
        ['`{{project}}`', 'The project.'],
        ['`{{head}}`', 'The expense head.'],
        ['`{{purpose}}`', 'What the request is for.'],
        ['`{{status}}`', 'Where the request has got to.'],
        ['`{{needed_by}}`', 'The required-by date.'],
        ['`{{submitted_on}}`', 'When it was submitted.'],
        ['`{{processing_on}}`', 'When it was taken for processing.'],
        ['`{{link}}`', 'A link straight to the request, built from the Base URL above.'],
      ],
    ),
    note(
      'Anything else in double braces is rejected when you save, and the screen tells you so. A typo ' +
      'cannot reach an inbox as an empty gap that nobody can debug afterwards — it fails here, while ' +
      'you are looking at it.',
      'A typo fails on save',
    ),
    tip(
      'Always put `{{link}}` in the message. An email telling somebody a request needs them, with no ' +
      'way to open it, is an email that generates a reply asking where it is.',
    ),

    faq([
      {
        q: 'Can I turn off in-app notifications for an event?',
        a: 'No. The In-app column is a fixed `On` pill rather than a switch. Email is the only per-event delivery choice.',
      },
      {
        q: 'Nothing is arriving by email.',
        a: 'Check three things in order: the mail server settings save, the **Send a test email** result, and whether the event itself has **Email** switched on.',
      },
      {
        q: 'The test email failed with a message I do not understand.',
        a: 'It is the error the mail server gave back, passed on rather than swallowed. Hand it to whoever runs your mail — it is the fastest thing they can act on.',
      },
      {
        q: 'A workflow action happened but nobody was told.',
        a: 'Delivery failures never fail the action itself — an approval succeeds whether or not the email goes. The in-app notification is written before any email is attempted, so check the recipient’s notification centre before you conclude nothing fired.',
      },
      {
        q: 'Who is in "the Accounts group"?',
        a: 'Everybody whose roles carry the payment **Process** permission. It is not a list you maintain here — you change it by changing roles on [Roles and permissions](page:admin/roles-and-permissions) or by changing who holds them on [People and accounts](page:admin/people).',
      },
    ]),
  ],
});
