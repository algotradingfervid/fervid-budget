import { page, prose, section, shot, note, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'reference',
  slug: 'statuses-and-badges',
  title: 'Statuses and badges',
  summary: 'What each badge means on sight, in the colours the product uses.',
  blocks: [
    prose(`Every request carries a small coloured badge and, beside it, one plain sentence naming who
       owes the next action. The badge is the status. The sentence is the "waiting on" line, and it
       is computed in one place, so the list, the queue and the request itself can never give you
       three different answers.`),

    shot('requester/requests-all', 'The request list. Every card carries the badge and, on the right, the waiting line — in brand colour when the answer is you.'),

    prose(`The **My requests** tabs group these states by whether work remains: Open contains Awaiting
       approval, Returned, Approved, Cancellation requested, With Accounts and Partial — manager
       review. Closed contains Completed, Completed — partial accepted, Rejected, Withdrawn and
       Cancelled. Open plus Closed covers All under the same filters. See [Tracking your
       requests](page:requester/tracking-your-requests#the-four-tabs).`),

    section(`The eleven statuses`),

    prose(`The left column is the value the system stores and the one you can filter a list by. The
       second is the wording the interface actually prints.`),

    table(
      [`Stored value`, `What the badge says`, `What it means`, `Whose move it is next`],
      [
        [`\`pending\``, `**Awaiting approval**`, `The request has been submitted and is with an approver. It can still be edited or withdrawn by the person who raised it.`, `The approver. The line reads "Waiting on" and their name.`],
        [`\`returned\``, `**Returned for correction**`, `The approver sent it back with a comment. It keeps its number and its whole history — this is not a rejection.`, `The requester. Correct it and resubmit.`],
        [`\`approved\``, `**Approved — awaiting payment**`, `An approver has signed off an amount. The request is locked to the requester and is now in the Accounts queue.`, `Accounts. The line reads "Waiting on Accounts".`],
        [`\`rejected\``, `**Rejected — final**`, `The approver refused it with a reason. Nothing reopens it.`, `Nobody. The line reads "Closed. Raise a new request if needed".`],
        [`\`withdrawn\``, `**Withdrawn**`, `The requester took it back before it was decided. Only a pending request can reach this state.`, `Nobody. The line reads "Withdrawn by the requester".`],
        [`\`cancellation_requested\``, `**Cancellation requested**`, `The requester has asked for an approved request to be cancelled. Payment is frozen while the ask stands.`, `The approver. The line names them.`],
        [`\`cancelled\``, `**Cancelled**`, `The request was cancelled, either because the approver accepted the ask or because they cancelled it outright with a reason.`, `Nobody. The line reads "Cancelled. Nothing can be paid against it".`],
        [`\`processing\``, `**With Accounts — taken by** and the holder's name`, `Somebody in Accounts has claimed the request so that nobody else can pay it at the same time. The badge names them, or reads "taken by you" if it is you.`, `Accounts. The line reads "Waiting on" the holder's name followed by "(Accounts)", or "Waiting on you" if you hold it.`],
        [`\`partial_review\``, `**Partial — manager review**, or **Partial — under discussion**`, `Accounts paid less than was approved and asked the approver to decide about the rest. The approver can keep the balance payable, accept and close it, or raise a concern. While a concern is waiting for Accounts to answer, the badge reads **Partial — under discussion** instead.`, `The approver. The line names them. While a concern is open it reads "Waiting on" the approver's name "and Accounts" — or "Waiting on you and Accounts" for the approver, and "Waiting on you" for the accountant who has to answer.`],
        [`\`completed\``, `**Completed**`, `The request was paid and closed by Accounts.`, `Nobody. The line reads "Nothing pending".`],
        [`\`completed_partial\``, `**Completed — partial accepted**`, `Less was paid than was approved and the approver accepted the shortfall, writing the balance off. Deliberately distinct from a clean completion.`, `Nobody. The line reads "Nothing pending".`],
      ],
      `Every status the product stores, and the words it shows for each. The wording in bold is what the badge prints; where a badge has two forms, both are given.`,
    ),

    section(`On hold, which has no status of its own`),

    prose(`A held request is still \`approved\`. What changes is that it drops out of the set Accounts
       can claim, and the badge is replaced: wherever you would expect **Approved — awaiting
       payment**, a held request shows **On hold** instead, and the waiting line names the
       requester rather than Accounts, because it is their answer everybody is waiting for.`),

    shot('accounts/queue-hold', 'The On hold tab of the payment queue. Every row shows the **On hold** badge and offers **Read reply** rather than a way to pay it.'),

    note(`There is no \`on_hold\` status and there never was. If a screen shows **On hold**, the
       stored status behind it is \`approved\` — which is why a held request disappears from the
       "approved and unclaimed" count while still being an approved request.`),

    section(`Where the accounts screens say it differently`),

    prose(`\`processing\` is the one status the payment screens spell out differently, because the
       person holding the claim is the whole point of it. On the request, in the request list and on
       the conflict screen the badge reads **With Accounts — taken by** and the holder's name. On
       the payment queue it reads **Reserved by you** with the time you took it, or **Reserved by**
       a colleague’s name. The queue's own tab for these requests is called **Processing**. The
       stored status is \`processing\` in every case.`),

    section(`Reading the colours`),

    prose(`The palette is doing work, not decoration. Five tones cover every badge, and two of them
       carry an extra treatment so that "somebody is mid-action" reads at a glance.`),

    table(
      [`Tone`, `Used for`, `What it is telling you`],
      [
        [`Blue`, `**Awaiting approval**`, `In the pipeline and behaving normally.`],
        [`Amber`, `**Returned for correction**, **Cancellation requested**, **Partial — manager review**, **Partial — under discussion**, **On hold**`, `Something is stuck and a named person has to unstick it. **Partial — under discussion** has a dashed outline, to mark that two people are talking it over.`],
        [`Terracotta`, `**Approved — awaiting payment**, **With Accounts**`, `Live, moving, and somebody is acting on it.`],
        [`Green`, `**Completed**, **Completed — partial accepted**`, `Finished. **Completed** is a filled green badge; **Completed — partial accepted** is drawn as a green outline on the plain background, so the two never read as the same outcome.`],
        [`Grey and faded brick`, `**Withdrawn**, **Cancelled**, **Rejected — final**`, `Closed without payment. These are drawn quieter than the live states on purpose.`],
      ],
      `The five tones, and what each one is claiming.`,
    ),

    prose(`**With Accounts** and **On hold** additionally carry fine diagonal stripes. That is the
       product saying somebody has their hand on this request right now — either an accountant
       holding a reservation, or a question waiting on a reply.`),

    shot('accounts/queue-paid', 'The Paid tab, where the two green badges sit side by side: the filled **Completed** and the outlined **Completed — partial accepted**.'),

    section(`The other badges you will see`),

    prose(`Not every badge is a status. Three more appear beside them and mean something different.`),

    table(
      [`Badge`, `Where it appears`, `What it means`],
      [
        [`**Urgent**`, `Beside the status on a request card and on the request itself`, `A solid red badge with an exclamation mark. Urgency changes who is told and how soon; it never changes who may decide, and an urgent request follows exactly the same approval rules.`],
        [`**Recoverable**`, `Beside the status, in purple, sometimes with the category after it`, `The money is expected back. It is kept out of budget actuals and tracked on the recoverables register instead.`],
        [`**Budget expense**`, `In the corner of the Details card on a request`, `The opposite treatment: money spent and gone, counting against a project and head.`],
      ],
      `Badges that qualify a request rather than describing where it is.`,
    ),

    section(`The waiting line`),

    prose(`The sentence beside the badge is the one thing on the screen that answers "is this mine?".
       It has three appearances: brand-coloured with an arrow when the answer is you, grey with an
       arrow when it is somebody else, and a tick or a dash when the request is closed.`),

    table(
      [`What you see`, `What it means`],
      [
        [`→ Waiting on you`, `You are the person who owes the next action, whatever your role is.`],
        [`→ Waiting on a name`, `That person owes it. On a pending, frozen or partially paid request that is the approver; on a returned one it is the requester.`],
        [`→ Waiting on Accounts`, `Approved, and the next move belongs to the accounts team.`],
        [`→ Waiting on a name (Accounts)`, `Claimed by that accountant, who is working on the payment.`],
        [`→ Waiting on a name and Accounts`, `A short payment with an open concern: Accounts has to answer, then the approver decides.`],
        [`→ Waiting on a name to clarify`, `The request is on hold and Accounts is waiting for that person to answer the question.`],
        [`✓ Nothing pending`, `Completed, in either form. Nothing else will happen to it.`],
        [`— Closed. Raise a new request if needed`, `Rejected.`],
        [`— Withdrawn by the requester`, `Withdrawn.`],
        [`— Cancelled. Nothing can be paid against it`, `Cancelled. The dash is used for these three instead of a tick because they are closed, not achieved.`],
      ],
      `The waiting line, and what each form of it is saying.`,
    ),

    tip(`On the request list, the **Needs me** tab is the short answer to "what is mine to do": your
       own requests returned for correction, requests waiting on your decision as approver
       (awaiting approval, cancellation requested, or paid short and back with you), and short
       payments you hold where an approver's concern is waiting for your answer. One case reads
       "Waiting on you" but is not in that tab: your own request while Accounts has it on hold.
       Check the **Open** tab for those.`),

    faq([
      { q: `The badge says On hold but the filter says approved. Which is right?`, a: `Both. The hold is a flag on an approved request, not a status of its own, so filtering by \`approved\` includes held requests. The payment queue keeps them on their own **On hold** tab.` },
      { q: `Why is a rejected badge fainter than the others?`, a: `Closed states are drawn deliberately quieter than live ones, so a screenful of requests reads at a glance. Only the frame is faded — the words keep their full contrast.` },
      { q: `Can I filter a list by one of these values?`, a: `Yes. The request list accepts a status filter, and the CSV export honours the same one, so the file and the screen always describe the same set. See [Tracking your requests](page:requester/tracking-your-requests).` },
      { q: `Is there a draft status?`, a: `No. Creating a request and sending it for approval are one press, so there is nothing to be a draft of. See [The request lifecycle](page:reference/request-lifecycle).` },
    ]),
  ],
});
