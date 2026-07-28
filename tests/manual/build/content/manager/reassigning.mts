import { page, prose, section, shot, steps, fields, table, note, tip, faq } from '../../schema.mts';

export default page({
  section: 'manager',
  slug: 'reassigning',
  title: 'Reassigning an approval',
  summary: 'Handing a decision to somebody else when it is not yours to take.',
  blocks: [
    prose(
      `Every request is routed to one approver by name, and only that person can approve, return or
       reject it. That is a good rule until the named person is on leave, has left the organisation
       or has lost the approver role — at which point the request is stuck with nobody able to move
       it.

       Reassigning is the way out. It moves the decision to another approver, keeping the request's
       number, its amount and its whole history.`,
    ),

    section('Reassigning an approval'),
    steps([
      { text: 'Open the request. The control sits in the bar along the bottom, at the far right.' },
      {
        text: 'Select **Reassign approval**.',
        note: 'The panel names the person who currently holds it — *Currently with Karthik Bose*.',
      },
      {
        text: 'Choose the new approver from **Send it to**. The list starts on *Choose an approver*.',
        shot: 'manager/reassign-sheet',
      },
      { text: 'Say why in **Why**. It is required, and everybody who can see the request will read it.' },
      {
        text: 'Select **Reassign**.',
        note: 'You land back on the request. The **Approver** field now names somebody else.',
      },
    ]),

    section('The panel, field by field'),
    fields(
      [
        {
          name: 'Send it to',
          required: true,
          note: 'Every active person whose roles let them approve requests, in name order, minus the requester. The current approver appears in the list but cannot be chosen.',
        },
        {
          name: 'Why',
          required: true,
          note: 'Recorded in the history and visible to everyone who can see this request. A blank reason is refused.',
        },
      ],
      'The reassign panel.',
    ),
    prose(
      `The line under the panel says what the move costs and what it does not: *The request keeps its
       number and its history. The new approver is told, and the reminder clock starts again.*`,
    ),

    section('Who may reassign'),
    prose(
      `This is the one approver action that is not restricted to the person named on the request.
       Anybody holding the reassign permission and able to see the request can move it, including an
       administrator. That is deliberate — the case that most needs rescuing is a request routed to
       somebody who can no longer act, and if only that person could hand it on, there would be no way
       out of it.

       So if a colleague has left mid-decision, you do not need them to do anything. Open their
       request and reassign it.`,
    ),
    note(
      `The control appears only while the request is at \`pending\`, and only when there is somebody
       else to send it to. If no other eligible approver exists in the organisation, the button is
       not drawn at all — there is no message, because there is nothing to offer. Ask an
       administrator to give somebody the approver role.`,
    ),

    section('What changes and what does not'),
    table(
      ['', 'After a reassignment'],
      [
        ['The request number', 'Unchanged'],
        ['The status', 'Still `pending` — reassigning is not a decision'],
        ['The amount', 'Unchanged'],
        ['The **Approver** field', 'The person you chose'],
        ['The history', 'Kept in full, with a new line naming you, them and your reason'],
        ['Your queue', 'The request leaves it'],
        ['Their queue', 'The request appears in it, and they are notified'],
        ['The reminder clock', 'Reset, so the chasing starts again from now'],
      ],
    ),

    section('When reassigning is refused'),
    table(
      ['What you see', 'Why'],
      [
        ['*Choose the approver this request should go to.*', 'No name was picked from the list.'],
        ['*Give a reason for the reassignment.*', 'The **Why** box was empty.'],
        ['A message saying only a pending request can be reassigned', 'It has already been approved, returned, rejected, withdrawn or cancelled.'],
        ['A message saying that person cannot approve requests', 'The chosen account no longer carries the approver role. Pick another, or ask an administrator.'],
        ['A message saying a request cannot be reassigned to its own requester', 'You picked the person who raised it. Nobody may approve their own request.'],
      ],
    ),
    tip(
      `Reassigning is not a way to avoid a decision you find awkward. It is recorded with your name
       and your reason, in a stream the requester reads. Say something true and specific — *I am on
       leave from Friday* rather than *not mine*.`,
    ),

    faq([
      {
        q: 'Can I reassign a request to myself?',
        a: `Yes, if you hold the reassign permission and are an eligible approver — that is how a
            stranded request is picked up. You cannot reassign it to the person who raised it.`,
      },
      {
        q: 'The person I want is not in the list.',
        a: `The list holds only active accounts whose roles grant approval. If somebody is missing,
            they are either deactivated or do not have the approver role. An administrator can fix
            both from [People and accounts](page:admin/people).`,
      },
      {
        q: 'I reassigned it by mistake.',
        a: `Reassign it back. The new approver — or anybody with the permission — can move it again,
            and both moves stay in the history.`,
      },
      {
        q: 'Does reassigning tell the requester?',
        a: `The reassignment and your reason go into the History and conversation stream, which the
            requester can read. The new approver is notified directly.`,
      },
    ]),
  ],
});
