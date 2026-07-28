import { page, prose, section, shot, steps, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'urgent-requests',
  title: 'Marking a request urgent',
  summary: 'When to flag a request as urgent, and what it changes for the people downstream.',
  blocks: [
    prose(
      `Urgency is a flag you set as you raise a request. It does not shorten the route or skip anybody:
       the same approver still decides, and Accounts still pays. What it changes is where the request
       sits in other people's lists and how loudly they are told about it.`,
    ),

    section('Setting it'),
    steps([
      {
        text: 'On the request form, find **Mark this urgent** in the **Amount and timing** fieldset.',
        note: 'The hint beneath reads: *Urgent requests follow the same approval rules. They send an immediate email to your approver, and to Accounts once approved.*',
      },
      {
        text: 'Tick the box.',
        note: 'A field appears below it: **Why is it urgent**, carrying a red asterisk.',
      },
      {
        text: 'Say why, in one line.',
        note: 'The placeholder shows the register expected: *Supply stops if this is not cleared by Monday.*',
      },
    ]),
    warning(
      `Leave the reason empty and the form comes back with \`say why this is urgent\`. The reason is
       not decoration — it is what the approver reads before deciding whether to drop what they are
       doing.`,
    ),

    section('What it changes'),
    shot('requester/urgent-request', 'An urgent request. The red Urgent flag sits ahead of the status, and the reason is written into the Urgency row.'),
    table(
      ['Where', 'What urgency does'],
      [
        ['**My requests**, and the approver’s queue', 'Urgent requests sort to the top of the list, ahead of everything else, whatever their age.'],
        ['Each card in a list', 'A red **Urgent** pill appears in front of the status pill.'],
        ['The request itself', 'The same red **Urgent** pill sits before the status, and **Urgency** reads *Urgent —* followed by your reason, instead of *Normal*.'],
        ['Notifications', 'The urgent event has its own notification, addressed to the approver at once and to Accounts once the request is approved.'],
      ],
    ),
    note(
      `Whether that notification also arrives as an email depends on your administrator: every event
       writes an in-app notification unconditionally, and sends email only if that event has been
       switched on and a mail server is configured. See
       [Notification rules and email](page:admin/notification-rules).`,
    ),

    section('What it does not change'),
    prose(
      `Nothing about the decision. An urgent request needs the same approver, obeys the same
       validation, and can be returned or rejected exactly like any other. It does not raise the
       amount anybody may approve, and it does not let a request skip the accounts queue.`,
    ),
    tip(
      `If a request truly cannot wait, mark it urgent *and* talk to your approver. The flag moves it up
       a list; it does not make anybody read the list.`,
    ),

    section('Changing your mind'),
    prose(
      `While the request is still yours to edit, the edit form carries the same checkbox — labelled
       **Marked urgent** there — and the reason field beneath it. Untick it to drop the flag, or tick
       it to add one. The change is recorded in the request's history as *Urgency* moving between
       \`Urgent\` and \`Normal\`, so nobody has to guess whether it was always flagged.`,
    ),

    section('When urgency is switched off'),
    prose(
      `Urgency is configurable, and an administrator can set it three ways. You will only ever see one
       of them, so it is worth knowing which you are looking at.`,
    ),
    table(
      ['Setting', 'What you see on the form'],
      [
        ['Reason required — the default', '**Mark this urgent**, and a required **Why is it urgent** field once ticked.'],
        ['No reason required', '**Mark this urgent** on its own, with no reason field.'],
        ['Urgency switched off', 'No checkbox at all. A request submitted as urgent anyway is refused with `urgent requests are switched off`.'],
      ],
      'Set on the [Configuration](page:admin/configuration) screen, under **Urgency**.',
    ),

    faq([
      {
        q: 'Everything I raise is urgent. Should I tick it every time?',
        a: `No. Urgency works by contrast — when every request carries the flag, the flag stops telling
            anybody anything, and the approver is back to reading dates.`,
      },
      {
        q: 'I forgot to mark it urgent.',
        a: `Edit the request while it is still awaiting approval and tick the box. If it has already
            been approved, it is locked — talk to Accounts instead.`,
      },
      {
        q: 'Does urgent mean it gets paid first?',
        a: `It puts the request at the top of the queues people work from, which usually means sooner.
            It is not a rule the product enforces, and Accounts still work in the order they choose.`,
      },
    ]),
  ],
});
