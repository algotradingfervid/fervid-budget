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
          note: 'Every active person whose roles let them approve requests, in name order, minus the requester. The current approver and your own name appear in the list but cannot be chosen — a request is never handed to the person moving it.',
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
      `Two people. The approver the request was sent to may hand it on — it is theirs to decide, so it
       is theirs to give away. And an administrator may move any request, which is the way out when
       the named approver is on leave, has left or has lost the approver role: the case that most
       needs rescuing is a request routed to somebody who can no longer act, and if only that person
       could hand it on there would be no way out of it. So if a colleague has left mid-decision, ask
       an administrator; you do not need the colleague to do anything.

       Nobody else. Another manager who can see the request cannot move it, and nobody — not even an
       administrator — can reassign a request to themselves. Being able to see a request is not the
       same as being able to decide it, and reassigning must not become a way of making it yours.`,
    ),
    note(
      `The control appears while the request is waiting on its approver — \`pending\`, returned for
       correction, awaiting a cancellation decision, or in partial review — and only to somebody who
       may use it, when there is somebody else to send it to. If no other eligible approver exists
       in the organisation, the button is not drawn at all — there is no message, because there is
       nothing to offer. Ask an administrator to give somebody the approver role.`,
    ),

    section('What changes and what does not'),
    table(
      ['', 'After a reassignment'],
      [
        ['The request number', 'Unchanged'],
        ['The status', 'Unchanged — reassigning is not a decision. A request awaiting a cancellation decision or in partial review stays so; only the person deciding it changes'],
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
        ['*Only the approver this request was sent to, or an administrator, can reassign it.*', 'You are not the approver named on it. Ask them, or an administrator.'],
        ['A message saying an approved (or completed, rejected, withdrawn or cancelled) request cannot be reassigned', 'Nothing is waiting on an approver: it is with Accounts, or it is closed.'],
        ['A message saying you cannot reassign a request to yourself', 'You picked your own name. A request is never handed to the person moving it.'],
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
        a: `No — nobody can, an administrator included. A stranded request is picked up by an
            administrator moving it to you, and that move is recorded with their name and reason.
            Nor can a request be reassigned to the person who raised it.`,
      },
      {
        q: 'The person I want is not in the list.',
        a: `The list holds only active accounts whose roles grant approval. If somebody is missing,
            they are either deactivated or do not have the approver role. An administrator can fix
            both from [People and accounts](page:admin/people).`,
      },
      {
        q: 'I reassigned it by mistake.',
        a: `Ask the new approver to send it back, or an administrator to move it. It is no longer
            yours to move, and both moves stay in the history.`,
      },
      {
        q: 'Does reassigning tell the requester?',
        a: `The reassignment and your reason go into the History and conversation stream, which the
            requester can read. The new approver is notified directly.`,
      },
    ]),
  ],
});
