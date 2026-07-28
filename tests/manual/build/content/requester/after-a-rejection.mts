import { page, prose, section, shot, steps, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'after-a-rejection',
  title: 'After a rejection',
  summary: 'Why rejection is final, and how to raise a corrected request in its place.',
  blocks: [
    prose(
      `Rejection is the answer an approver gives when a request should not exist — not when it needs
       fixing. It is deliberately one-way: a rejected request is read-only for ever, and the route
       forward is a new request rather than a revived one.`,
    ),
    tip(
      `If your approver wanted a correction they would have returned the request instead, which keeps
       its number and sends it back to you. A rejection means they decided against it. See
       [When a request comes back to you](page:requester/when-a-request-is-returned) for the other case.`,
    ),

    section('Reading the rejection'),
    shot('requester/detail-rejected', 'A rejected request. The red banner carries the approver’s reason word for word.'),
    prose(
      `The status pill reads **Rejected — final** and the sentence beside it says *Closed. Raise a new
       request if needed.* Under the heading, a red banner leads with your approver's name and the
       words **rejected this**, and quotes their reason exactly as they wrote it. The same reason
       appears again as a line in **History and conversation**, so it survives with the record.`,
    ),
    prose(
      `A reason is compulsory — the product refuses a rejection without one — so there is always
       something to read. If it is not clear enough to act on, the comment box is still open: post a
       question there rather than guessing at a second attempt.`,
    ),

    section('What "final" means'),
    warning(
      `Nothing brings a rejected request back. It cannot be edited, resubmitted, withdrawn or
       cancelled, and no approver can reverse their own rejection. The request stays in your **Closed**
       tab as a permanent record of what was asked for and why it was refused.`,
    ),

    section('Raising it again'),
    prose(
      `The action bar on a rejected request carries one button: **Raise it again**. It creates a fresh
       request from this one and takes you to its confirmation screen.`,
    ),
    steps([
      { text: 'Open the rejected request from **My requests** → **Closed**.' },
      { text: 'Read the reason, and work out what has to be different this time.' },
      {
        text: 'Select **Raise it again**.',
        note: 'A new request is created immediately, already submitted, with a new number of its own.',
      },
      {
        text: 'Select **View this request** on the confirmation screen, then **Edit request**.',
        note: 'This is where you make the change your approver asked for. The copy is otherwise identical to the request they refused.',
      },
    ]),
    warning(
      `**Raise it again** does not open a form for you to adjust first. The new request goes straight
       to your approver as an exact copy of the rejected one. If you do not edit it, they are being
       asked the same question a second time — edit it before they get to it.`,
      'The copy is submitted immediately',
    ),

    section('What is copied, and what is not'),
    table(
      ['Carried across', 'Not carried across'],
      [
        ['The type and treatment, the project and head, the vendor and payee', 'The request number — the copy gets a new one'],
        ['The short title, amount, purpose and needed-by date', 'The attachments — you have to attach documents again'],
        ['The invoice number and date, expense date, reason for the advance', 'The history and the conversation, including the rejection'],
        ['The recoverable category, expected return date, counterparty and terms', 'Any decision the approver made on the original'],
        ['The urgency flag and its reason, and the approver you chose', '—'],
      ],
    ),
    note(
      `Documents are the one people are caught by. Re-raising copies the request's fields but not its
       files, so an invoice you attached to the rejected request has to be attached to the new one.
       See [Attaching documents](page:requester/attachments).`,
    ),
    prose(
      `The two requests are linked in the record: the new one's history opens with a line saying it was
       re-raised from the old number, so anybody reading either can find the other.`,
    ),

    section('Should you raise it again at all?'),
    prose(
      `Sometimes not. A rejection for *"outside the approved budget for this head"* is not answered by
       an identical request; it is answered by a different head, a smaller amount, or a conversation
       with your approver first. The button exists because the second attempt is often legitimate —
       the wrong project, the wrong month, a figure that has since been agreed — not because every
       rejection deserves one.`,
    ),

    faq([
      {
        q: 'Can my approver undo a rejection?',
        a: `No. There is no route out of a rejected request for anybody, whatever permissions they
            hold. The second attempt is a new request.`,
      },
      {
        q: 'Will the same approver get the copy?',
        a: `Yes — the approver is one of the fields copied across. Change it on the new request if it
            should go to somebody else.`,
      },
      {
        q: 'I raised it again by mistake.',
        a: `The copy is a normal pending request, so withdraw it. See
            [Changing or withdrawing a request](page:requester/editing-and-withdrawing).`,
      },
      {
        q: 'Does the rejected request still count against my budget?',
        a: `No. Nothing was paid, so nothing was booked. Only a recorded payment moves budget actuals.`,
      },
    ]),
  ],
});
