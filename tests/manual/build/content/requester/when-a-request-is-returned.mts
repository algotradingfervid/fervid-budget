import { page, prose, section, shot, steps, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'when-a-request-is-returned',
  title: 'When a request comes back to you',
  summary: 'Reading the approver’s reason, correcting the request and sending it again.',
  blocks: [
    prose(
      `Returning is what an approver does when a request is fixable. It comes back to you with a
       written reason, keeps its number and its whole history, and waits until you send it again.
       Nothing is lost, and nobody has to start over.`,
    ),
    tip(
      `Returned is not rejected. A rejection is final and cannot be revived; a return is a question.
       The screen itself says so, in a line under the banner.`,
    ),

    section('Finding it'),
    prose(
      `A returned request turns up in three places at once: the **Needs your action** panel on your
       home screen, the **Needs me** tab of [My requests](page:requester/tracking-your-requests), and
       your notifications. On every card it carries the amber **Returned for correction** pill and the
       words **Waiting on you**.`,
    ),

    section('The correction screen'),
    shot('requester/detail-returned', 'A returned request as its raiser sees it: the reason, then the fields to fix, then the history.'),
    prose(
      `Opening a returned request does not give you the ordinary read-only view — it gives you the
       correction screen, because there is nothing to read that you did not write. The amber banner
       leads with your approver's name and the words **sent this back**, and quotes their reason word
       for word. Below it: *Returned is not rejected. This request keeps its number and its history —
       correct it and send it again.*`,
    ),

    section('What you can change here'),
    prose(
      `The fieldset is headed **Correct and resubmit** and holds the fields an approver usually asks
       about. Which ones appear depends on the request type.`,
    ),
    table(
      ['Always', 'On a vendor invoice', 'On a reimbursement', 'On either advance'],
      [
        ['**Short title**, **Amount**, **Purpose**, **Documents**', '**Invoice number**, **Invoice date**', '**Expense date**', '**Reason for the advance**'],
      ],
    ),
    prose(
      `The uploader at the foot is labelled **Attach the corrected document**, and any documents
       already on the request are listed above it with their own **Download** buttons.`,
    ),
    note(
      `The project, the head, the needed-by date, the approver, the urgency flag and the vendor are not
       on this screen, and they keep the values they already had. To change one of those, open the full
       edit form — it is the same address with \`/edit\` on the end. There is no button to it from the
       correction screen.`,
    ),

    section('Sending it back'),
    steps([
      { text: 'Read the banner. It is the whole of what your approver told you.' },
      { text: 'Make the corrections in the fields below it.' },
      {
        text: 'Attach the corrected document if that is what was asked for.',
        note: 'The document is saved in the same act as the corrections — either both land or neither does.',
      },
      {
        text: 'Select **Resubmit for approval**.',
        note: 'The request goes back to `pending`, your approver is told, and the reminder clock starts again.',
      },
    ]),
    prose(
      `The note beside the buttons spells out the difference between them: *Saving keeps it with you;
       resubmitting sends it back to your approver.* **Save corrections** writes your changes and
       leaves the request sitting with you, still returned and still nobody else's to act on.
       **Resubmit for approval** does both.`,
    ),
    warning(
      `**Save corrections** does not tell your approver anything. A request saved and not resubmitted
       stays out of everybody's queue — it is not waiting on them, and no reminder will go out — so it
       can sit for a week with nobody noticing. Use it only when you genuinely mean to come back.`,
    ),

    section('The conversation'),
    prose(
      `Below the form is **History and conversation**, showing the submission, the return and its
       reason, and anything anybody has written since. If the reason is not clear, ask there rather
       than guessing: use the **Add a comment** box and select **Post comment**. Everyone who can see
       the request sees your question, and the request stays with you in the meantime.`,
    ),

    section('If it comes back a second time'),
    prose(
      `There is no limit. A request can be returned, corrected and resubmitted as often as it takes,
       and it keeps its number throughout — so the history reads as one story rather than a pile of
       near-identical requests. If the same point keeps coming back, the comment box is usually
       quicker than another round.`,
    ),

    faq([
      {
        q: 'Can I withdraw a returned request instead of fixing it?',
        a: `Not directly — withdrawal is offered only while a request is awaiting approval. Resubmit it
            and then withdraw it, or ask your approver to reject it.`,
      },
      {
        q: 'I resubmitted and it was refused.',
        a: `The same validation runs on a resubmission as on a new request, and it runs against the
            corrected request rather than the one you started with. Read the red band at the top — it
            names the first thing that is wrong.`,
      },
      {
        q: 'My approver returned it by mistake.',
        a: `Resubmit it unchanged. The return and the resubmission both stay in the history, so the
            record shows what happened.`,
      },
      {
        q: 'Does resubmitting change the request number?',
        a: `No. Only raising a brand-new request produces a new number — including the re-raise after a
            rejection. See [After a rejection](page:requester/after-a-rejection).`,
      },
    ]),
  ],
});
