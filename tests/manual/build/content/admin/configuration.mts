import { page, prose, section, subsection, shot, steps, note, warning, tip, fields, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'configuration',
  title: 'Configuration',
  summary: 'The settings that change how the product behaves for everybody.',
  blocks: [
    prose(
      'Configuration holds the rules the request module runs on. The line under the heading makes the ' +
      'claim the screen is built around: everything here is data, so changing how the product behaves ' +
      'does not need a code change.\n\n' +
      'These settings are company-wide. There is nothing per-person and nothing per-project — a change ' +
      'here changes the product for everybody, from their next page load onwards.',
    ),

    section('The screen'),
    shot('admin/configuration', 'The Configuration screen: six blocks of settings, one save, and the recoverable categories at the foot.'),
    prose(
      'Six fieldsets, top to bottom: **Request numbering**, **Attachments**, **Urgency**, ' +
      '**Approvals**, **Payments**, and **Reminders and ageing**. One **Save configuration** button ' +
      'at the foot covers all six. **Discard** leaves without saving.\n\n' +
      'Below the save bar sits a seventh block, **Recoverable categories**, which is not part of the ' +
      'same form and has its own page: ' +
      '[Recoverable categories](page:admin/recoverable-categories).',
    ),
    note(
      'The note beside the buttons is worth taking at face value: "Every change here is written to the ' +
      'audit log." Anything you change is attributable, with a before and an after — see ' +
      '[The audit trail](page:admin/the-audit-trail).',
    ),

    section('Request numbering'),
    prose('How a request number is built. The seeded shape is `PR-2026-000036`.'),
    fields([
      { name: 'Prefix', required: false, note: 'The letters at the front. Seeded as `PR`.' },
      { name: 'Year segment', required: false, note: '`Calendar year`, `Financial year` or `None`.' },
      { name: 'Number width', required: false, note: 'How many digits the running number is padded to. Seeded as `6`.' },
    ]),
    warning(
      'Changing the numbering changes new requests only. Everything already numbered keeps the number ' +
      'it was given, so a change mid-year leaves two shapes of number in the system. Change it at a ' +
      'year boundary if you can.',
      'Existing numbers are not reissued',
    ),

    section('Attachments'),
    fields([
      {
        name: 'Require a supporting document on every request',
        required: false,
        note: 'Turning it on does not block submission. The hint explains why: it asks for an exception reason when no document is attached, because legitimate documents are sometimes genuinely unavailable.',
      },
      { name: 'Maximum file size (MB)', required: false, note: 'The largest file the product will accept. Seeded as `10`.' },
    ]),

    section('Urgency'),
    fields([
      {
        name: 'Marking a request urgent',
        required: false,
        note: '`Requires a reason`, `Free to mark, no reason`, or `Disabled`.',
      },
    ]),
    note(
      'The note under the block draws the line the setting cannot cross: "Urgent requests follow the ' +
      'same approval rules. Urgency changes who is told and how soon, never who may decide."',
      'What urgency does not do',
    ),

    section('Approvals'),
    fields([
      {
        name: 'Let the employee choose a different approver',
        required: false,
        note: 'On by default. Off means everyone must use the default approver set on their user record — see [People and accounts](page:admin/people).',
      },
      {
        name: 'Block self-approval',
        required: false,
        note: 'Ticked and greyed out. It is always on: a person can never approve a request they raised, whatever roles they hold. There is nothing to change.',
      },
      {
        name: 'Second approval above a threshold',
        required: false,
        note: 'Greyed out and unticked. The hint says it is reserved for a future version, and that the status model already has room for it. It does nothing today.',
      },
    ]),

    section('Payments'),
    fields([
      {
        name: 'Allow direct payments without a request',
        required: false,
        note: 'Off by default. The hint states what turning it on costs: every new payment normally starts from an approved request, and switching this on demands a written reason and is flagged in the audit log.',
      },
      {
        name: 'Payment modes offered',
        required: false,
        note: 'Comma separated, in the order the payment form should offer them. Seeded as `NEFT, RTGS, UPI, Cheque, Cash, Card, DD`.',
      },
    ]),
    warning(
      'A payment without a request is a payment nobody approved. Leaving **Allow direct payments ' +
      'without a request** off is what keeps the approval trail unbroken, and it is off for that ' +
      'reason. Turn it on only if you have a process for reviewing what comes through it.',
      'Direct payments',
    ),

    section('Reminders and ageing'),
    fields([
      { name: 'Remind after (days pending)', required: false, note: 'How long a request may sit with an approver before the first reminder. Default 3.' },
      { name: 'Then repeat every (days)', required: false, note: 'How often the reminder returns while nothing happens. Default 1.' },
      { name: 'Reservation goes stale after (days)', required: false, note: 'How long an accountant may hold a reservation with no payment recorded. Default 1.' },
    ]),
    note(
      'The note under the block covers two things people get wrong: reminders are counted in days, ' +
      'not working hours, and a request put on hold is waiting on the requester by design and is ' +
      'never reminded about.',
      'How the counting works',
    ),
    note(
      'Leaving one of these blank or setting it to zero does not switch reminders off — the default is ' +
      'used instead. Reminders cannot be disabled by accident from this screen.',
    ),

    section('Saving'),
    steps([
      { text: 'Change what needs changing, in any of the six blocks.' },
      {
        text: 'Select **Save configuration**.',
        note: 'All six blocks are saved together, and the change is written to the audit trail.',
      },
    ]),
    tip(
      'Change one setting at a time and watch what it does. These are company-wide switches, and a ' +
      'save that changes four of them at once leaves you guessing which one caused the phone calls.',
    ),

    subsection('Who can change it'),
    prose(
      'Opening this screen needs the configuration view grant and saving needs the edit grant; both ' +
      'sit in the **Administration** row of ' +
      '[Roles and permissions](page:admin/roles-and-permissions). The **Recoverable categories** block ' +
      'at the foot follows the **Recoverables** row instead, which is why somebody can be able to see ' +
      'the settings and not touch the categories, or the reverse.',
    ),

    faq([
      {
        q: 'I saved and nothing looks different.',
        a: 'Most of these settings show up on somebody else’s screen rather than yours — the request form, the payment form, the reminder that goes out tomorrow. Reload the screen the setting governs.',
      },
      {
        q: 'Why is Block self-approval greyed out?',
        a: 'Because it is always on. It is drawn ticked and disabled so the screen is honest about the rule rather than silent about it.',
      },
      {
        q: 'Can I set a different approval threshold for large amounts?',
        a: 'Not yet. **Second approval above a threshold** is drawn on the screen but marked as reserved for a future version, and it does nothing today.',
      },
      {
        q: 'Where are the recoverable categories?',
        a: 'At the foot of this screen, below the **Save configuration** bar. They have their own page: [Recoverable categories](page:admin/recoverable-categories).',
      },
      {
        q: 'Where are the email settings?',
        a: 'On [Notification rules and email](page:admin/notification-rules), not here.',
      },
    ]),
  ],
});
