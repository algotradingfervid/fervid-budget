import { page, prose, section, shot, note, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'reference',
  slug: 'glossary',
  title: 'Glossary',
  summary: 'The words this product uses, and exactly what each one means here.',
  blocks: [
    prose(`Several of these words mean something looser in ordinary finance conversation. What follows
       is what they mean *in this product*, which is the meaning the screens and the rules go by.`),

    section(`The request`),

    table(
      [`Term`, `What it means here`],
      [
        [`Request`, `One ask for money to be paid out. It exists from the moment it is submitted — there is no draft — and it holds exactly one status at a time.`],
        [`Request number`, `The identifier printed at the top of every request, in the form \`PR-2026-000467\`. It is reserved inside the same step that creates the request, so numbers are unique and never reused. The prefix, the year segment and the number of digits are all configurable.`],
        [`Request type`, `Which of five kinds of ask this is: vendor invoice payment, vendor advance, reimbursement, employee advance, or deposit or guarantee. The type is chosen on the first step of a new request and decides which fields exist. It is not the same thing as treatment.`],
        [`Treatment`, `The other axis of the form, and the one that decides where the money is counted. Every request is either a **Budget expense** — money spent and gone, counted against a project and head — or **Refundable or recoverable** — money you expect back, kept out of budget actuals. Only an employee advance offers the choice; a deposit or guarantee is always recoverable, and the other three types are always budget expenses.`],
        [`Requester`, `The person who raised the request. Several things belong to them alone: editing it, withdrawing it, asking for it to be cancelled, and raising it again after a rejection.`],
        [`Short title`, `The one line the approver reads in their queue. It is required on every request.`],
        [`Purpose`, `The longer explanation of what the money is for. Also required on every request.`],
        [`Payee`, `Who the money goes to. On a vendor invoice or vendor advance that is a row from the vendor master; on a reimbursement or an employee advance it is always the person raising the request, and the field is filled in for you; on a deposit or guarantee it is the counterparty company or authority you type in.`],
        [`Urgent`, `A flag on a request, shown as a solid red **Urgent** badge. It changes who is told and how soon — an immediate email to the approver, and to Accounts once approved. It never changes who may decide, and an urgent request follows exactly the same approval rules as any other.`],
        [`Attachment`, `A document on a request or on a payment — an invoice, a receipt, a bank advice. Whether one is compulsory is a configuration setting; when it is, the product still does not hard-block, it asks for a written reason instead.`],
      ],
      `The vocabulary of a request itself.`,
    ),

    section(`Recoverable money`),

    prose(`A deposit is not an expense. When you mark a request recoverable you are saying the
       organisation expects the money back, and the product keeps it out of budget actuals and
       tracks it separately.`),

    shot('requester/form-recoverable', 'The recoverable treatment chosen on an employee advance. Picking it reveals the recoverable details: the category — fixed at Employee advance for this type — the expected return date and the repayment terms.'),

    table(
      [`Term`, `What it means here`],
      [
        [`Recoverable`, `The treatment for a deposit, guarantee, loan or advance you expect back. Choosing it demands three things: a category, an expected return date, and the repayment or refund terms.`],
        [`Recoverable category`, `What kind of recoverable this is. The list is maintained by your administrator, and each entry may demand something extra — EMD and PBG each require a related project; ICD and Security deposit each require a counterparty company; Employee advance and Other require nothing beyond the three above. A recoverable employee advance is always in the Employee advance category, and a deposit or guarantee may use any category except that one.`],
        [`Counterparty`, `The other party to a recoverable: the company or person holding your money. Some categories require it, and it is the second axis the recoverables register totals by.`],
        [`EMD`, `Earnest money deposit. A built-in recoverable category, which requires the project it relates to.`],
        [`PBG`, `Performance bank guarantee. A built-in recoverable category, which requires the project it relates to.`],
        [`ICD`, `Inter-corporate deposit. A built-in recoverable category, which requires a counterparty company.`],
        [`Recoverables register`, `The screen that tracks money expected back, totalled by category and by counterparty, with what is outstanding and what is past its expected return date.`],
      ],
      `Words that only appear once the treatment is recoverable.`,
    ),

    shot('accounts/recoverables', 'The recoverables register. The banner states the rule the whole treatment turns on: these payments never appear in the variance grid or in project spend.'),

    note(`A recoverable request carries no expense head, and its payment is written without one. That
       is why recoverable spend cannot show up in the variance grid or the monthly report — both
       are built by joining payments to heads.`),

    note(`Of the five request types, a deposit or guarantee is always recoverable and an employee
       advance may be either. A vendor invoice, a vendor advance and a reimbursement are budget
       expenses and the product refuses them any other way.`),

    section(`Approving`),

    table(
      [`Term`, `What it means here`],
      [
        [`Approver of record`, `The one person a request was sent to. It is stored on the request, it is what the approvals queue is built from, and it is what every decision route checks. Holding the approval permissions is not enough — you have to be the approver on that particular request.`],
        [`Approved amount`, `What the approver signed off, which may be less than was asked for and can never be more. It is the ceiling Accounts may pay to, not a note: a payment above it is refused.`],
        [`Return`, `Sending a request back to its requester with a comment, for correction. The request keeps its number and its whole history, and can be corrected and resubmitted.`],
        [`Reject`, `Refusing a request with a reason. It is final — nothing reopens a rejected request. The requester’s remedy is to raise a new one.`],
        [`Re-raise`, `Copying a rejected request into a brand-new one, with its own number, already awaiting approval. The rejected request stays rejected.`],
        [`Withdraw`, `The requester taking their own request back, before it has been decided. Only possible while it is awaiting approval.`],
        [`Cancel`, `Ending an approved request so that nothing can be paid against it. Two routes reach it: the requester asks and the approver agrees, or the approver cancels outright with a reason.`],
        [`Reassigning an approval`, `Handing a request that is waiting on its approver — awaiting approval, returned, cancellation requested, or a short payment under review — to a different approver, with a reason. It is a different act from reassigning a reservation, has a different permission behind it, and appears in the history under its own name.`],
      ],
      `The vocabulary of a decision.`,
    ),

    section(`Paying`),

    table(
      [`Term`, `What it means here`],
      [
        [`Reservation`, `A claim on an approved request while somebody works on it. Taking one moves the request to \`processing\` and stops anybody else recording a payment against it. It is the single thing standing between two accountants and a duplicate payment.`],
        [`Release`, `Giving a reservation up so the request returns to the open queue. It demands a written reason and a tick confirming no payment was started. There is no automatic release.`],
        [`Reassigning a reservation`, `Handing a claim straight to a named colleague, without the request becoming takeable in between. It needs \`reservation:reassign\`, which is an administrator permission.`],
        [`Stale reservation`, `A claim that has been open for more than a day with no payment recorded; the payment queue flags it. The one-day mark is fixed; only the reminder notification about it follows the number of days set in configuration. Nothing is released automatically — a transfer sitting for a day may well be moving through a bank portal — the product only puts the choice in front of a person.`],
        [`Settlement`, `The accountant’s balance decision. **Fully settled** closes the request; **Installment — pay the balance later** keeps the unpaid amount payable under the same approval; **Ask manager to accept a shortfall** sends the decision to the approver.`],
        [`Installment`, `A payment for part of the approved amount, with the rest left payable under the same approval. The request goes back to approved and can be paid again later. An installment sends no notification.`],
        [`Partial settlement`, `A payment recorded with a required reason and a request for the manager to decide the remaining shortfall. It differs from an installment, which directly keeps the balance payable.`],
        [`Shortfall`, `The difference between what was approved and what was actually paid on a partial settlement. It is what the approver is being asked to decide about.`],
        [`Hold`, `Accounts pausing an approved request that nobody has claimed, with a question — bank details that do not match, an invoice number that does not exist. The status stays \`approved\`; the request drops out of the takeable set until the hold is lifted, and only somebody holding \`payment:hold\` — the accounts team or an administrator — lifts it.`],
        [`Concern`, `The approver pushing back on a shortfall rather than accepting it. It reverses nothing — the money has already left — it puts the objection in the conversation and leaves the request in review, with the badge reading **Partial — under discussion** until Accounts answers.`],
        [`Void`, `Removing a payment from the actual totals with a reason. It applies only to payments that are not linked to a request; a payment recorded against a request cannot be edited or voided. Every new payment starts from an approved request, so only a payment recorded on its own before that rule — if your records hold any — can be voided.`],
      ],
      `The vocabulary of settlement.`,
    ),

    note(`A payment linked to a request is sealed. It cannot be amended and it cannot be voided, and
       the partial-review screen says so in as many words: **Cannot be edited**. The way to correct
       a mistaken settlement is an accounting adjustment outside this product, not an edit to the
       record of what happened.`),

    section(`Budget`),

    shot('manager/grid', 'The variance grid for one month: projects, the heads beneath them, and budget against actual for each.'),

    table(
      [`Term`, `What it means here`],
      [
        [`Project`, `The top level of the budget structure. Every budget expense is booked to a project and to a head within it.`],
        [`Head`, `Short for expense head: the line within a project that spending is booked against — Office Rent, Freight & Logistics, Staff Welfare. A head belongs to exactly one project, and a request must name a head under the project it names.`],
        [`Retired head`, `A head that has been switched off. It is kept so history still reads correctly, but it cannot be chosen on a new request and its budget cannot be edited.`],
        [`Budget`, `The figure a head is allowed for one month. Budgets are set month by month, not once for the year.`],
        [`Actual`, `What has actually been paid against a head in that month. Recoverable payments are excluded by design.`],
        [`Remaining`, `Budget minus actual, shown both as an amount and as a percentage. The grid also shows how much of the budget has been used.`],
        [`Variance grid`, `The screen that lays budget against actual for every head in a month, grouped by project, with a company total when unfiltered or a clearly labelled filtered subtotal when narrowed.`],
        [`Monthly plan`, `A month that has been opened for budgeting. A month has to exist as a plan before budgets can be set in it.`],
        [`Month lock`, `Closing a month so its budgets and payments become read-only. Locking and unlocking both demand a written reason. A locked month refuses new payments dated inside it, refuses budget edits, and blocks an approval whose Needed by date falls in it.`],
      ],
      `The vocabulary of the budget structure.`,
    ),

    section(`Access`),

    table(
      [`Term`, `What it means here`],
      [
        [`Permission`, `A pair of a resource and an action, written \`request:withdraw\`. There are 66 of them. Every screen, control and route is gated on one, and nothing in the product decides anything by looking at a role name.`],
        [`Role`, `A named bundle of permissions and data scopes. Roles are data, not code: an administrator can create them, copy them and change them. Four ship with the product, and a person may hold more than one.`],
        [`Data scope`, `Which records a permission reaches. \`own\` means only what you raised, \`assigned\` means where you are the requester or the approver, \`all\` means everything, and no scope at all means nothing. Only requests and payments are scoped.`],
        [`Default approver`, `The approver pre-selected on a person’s request form. An administrator sets it in the **Edit** panel of that person's user record, and — depending on configuration — they may or may not be allowed to choose somebody else.`],
        [`Audit trail`, `The record of who did what and when. It cannot be edited from anywhere in the product.`],
        [`Request ID`, `The long hexadecimal string printed under every error message. It identifies one attempt in the log, and quoting it is the fastest way to have a problem looked into.`],
      ],
      `The vocabulary of who may do what.`,
    ),

    tip(`A scope narrows what a permission reaches; it never widens it. A Requester holding
       \`request:view\` with the \`own\` scope sees only their own requests, and somebody else's
       answers "not found" rather than "forbidden". [The permission
       matrix](page:reference/permissions-matrix) explains why.`),

    section(`On every screen`),

    table(
      [`Term`, `What it means here`],
      [
        [`Status`, `Where a request has got to. There are eleven, listed in full on [Statuses and badges](page:reference/statuses-and-badges).`],
        [`Badge`, `The small coloured label carrying the status. Some badges are not statuses at all — **Urgent**, **Recoverable** and **Budget expense** qualify a request rather than placing it.`],
        [`Waiting line`, `The plain sentence beside the badge naming who owes the next action. It is brand-coloured and reads "Waiting on you" when the answer is you.`],
        [`Needs me`, `The tab on the request list that answers "what is mine to do": your returned requests, requests waiting on your decision as approver, and short payments where an approver's concern is waiting for your answer. Your own request while it is on hold reads "Waiting on you" but is not in this tab.`],
        [`Needed by`, `The optional date on a request telling the approver how long they have. It is also the date the product tests against the month lock when an approval is made.`],
        [`Thread`, `The single stream at the bottom of a request carrying its history and its conversation, oldest first. Everyone who can see the request sees the whole thread.`],
        [`Reminder`, `The nudge sent when a request has been waiting too long. How long, and how often it repeats, are configuration settings counted in days. A request on hold is waiting on its requester by design and is never reminded about.`],
      ],
      `Words that appear on nearly every screen.`,
    ),

    faq([
      { q: `What is the difference between the type and the treatment?`, a: `The type is what kind of ask it is — a vendor invoice, an advance, a reimbursement — and it decides which fields the form shows. The treatment is whether the money is coming back, and it decides whether the spend lands in the budget. An employee advance opens on the recoverable treatment, but you can still make it a budget expense.` },
      { q: `Are "reassign the approval" and "reassign the reservation" the same thing?`, a: `No, and confusing them is the commonest vocabulary mistake here. The first moves a request that is waiting on its approver to a different approver. The second moves a claim on an approved request to a different accountant. Different people, different permissions, different entries in the history.` },
      { q: `Is "on hold" a status?`, a: `No. It is a flag on an approved request. The badge says **On hold**, but the status behind it is \`approved\`.` },
      { q: `Why is a recoverable payment missing from the variance grid?`, a: `Because it is not budget spend. Recoverable money is expected back, so it is deliberately kept out of actuals and tracked on the recoverables register instead.` },
    ]),
  ],
});
