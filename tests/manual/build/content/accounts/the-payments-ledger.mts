import { page, prose, section, subsection, shot, note, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'the-payments-ledger',
  title: 'The payments ledger',
  summary: 'Every payment recorded, how to filter it, and how to read one in detail.',
  blocks: [
    prose(`The ledger is the record of money that has left. It is the sidebar item **Payments ledger**,
       and it holds every payment ever recorded — the ones settled against a request the way this
       product now works, and the older ones recorded before that.`),

    shot('accounts/payments-ledger', 'The ledger for one month. The line under the heading counts the rows and totals the active ones.'),

    section(`What the screen tells you`),

    prose(`The subtitle under **Payments** is a summary of what is on screen: the month, how many
       entries matched, and the total of the active ones. Voided rows are counted in the entries
       and left out of the total.`),

    table(
      [`Column`, `What is in it`],
      [
        [`Date`, `The date the money left the bank, not the date it was typed in`],
        [`Project / Head`, `What the spend was booked against, or a \`Recoverable\` pill for money expected back. The cell is the link to the payment.`],
        [`Amount`, `What was paid`],
        [`Payee`, `The vendor or person the money went to`],
        [`Mode`, `How it was paid, such as \`NEFT\`, \`UPI\` or \`Cheque\``],
        [`Reference`, `The invoice or bill number, with the transaction reference under it`],
        [`Entered by`, `Whoever recorded it`],
        [`Status`, `\`Active\`, or \`Removed\` for a voided payment`],
        [`Actions`, `**View** on every row, plus **Edit** and **Remove** on the few that still allow them`],
      ],
    ),

    note(`A payment against a recoverable request has no budget head, so its Project / Head cell shows
       a \`Recoverable\` pill instead. Those payments are deliberately outside budget spend — see
       [the recoverables register](page:accounts/the-recoverables-register).`),

    section(`Filtering`),

    prose(`Three controls sit above the table, and **Filter** applies all three at once. **Reset**
       clears them back to the current month.`),

    table(
      [`Control`, `What it does`],
      [
        [`**Month**`, `One calendar month at a time. The ledger opens on the current one.`],
        [`**Status**`, `\`Active\` by default; \`Removed / voided\` for the ones taken out of the totals; \`All\` for both.`],
        [`**Search**`, `Matches the project, the head, the payee, the invoice number, the transaction reference and the remarks. The placeholder names the first four.`],
      ],
    ),

    prose(`Rows come newest first by the date the money left. A single month never returns more than
       300 rows, so on a very busy month the count in the subtitle is the count of what is on
       screen.

       When nothing matches, the table says so and offers **Record a payment**, which opens the
       picker of approved requests. The same journey starts from **+ Add payment** at the top
       right.`),

    shot('accounts/payments-ledger-empty', 'A month with nothing in it. The empty table offers **Record a payment**, and **+ Add payment** stays at the top right.'),

    tip(`The number on the sidebar’s **Payments ledger** item is not the number of payments. It
       counts the active payments that have no proof attached — a standing list of the advices
       still to be chased.`),

    note(`If the month you are looking at has been locked, **+ Add payment** is replaced by a
       \`Locked\` badge, and a payment dated inside that month is refused. Ask an administrator to
       unlock it, or date the payment in an open month.`),

    section(`One payment in detail`),

    prose(`A payment is a permanent record in its request’s story. Installment requests may have
       several payments and a remaining approved balance. The detail shows the recorded payment
       alongside the request context.`),

    shot('accounts/payment-detail', 'A payment settled against a request: the comparison, the payment itself, and the whole story behind it.'),

    subsection(`The comparison`),

    prose(`**Approved**, **Paid** (this payment) and **Total paid on this request**, then one line for
       the balance. It depends on where the request has got to:`),

    table(
      [`The request is`, `The balance line reads`],
      [
        [`Completed, paid in full`, `**Difference · confirmed settled by Accounts**`],
        [`Completed, but paid short with an explanation`, `**Difference · agreed deduction or adjustment · confirmed settled by Accounts**`],
        [`Approved again after an installment`, `**Still owed to the payee · remaining approved balance**`],
        [`Waiting on the approver’s shortfall decision`, `**Still owed to the payee · awaiting manager decision**`],
        [`Closed with the shortfall accepted`, `**Balance written off · shortfall accepted by** the approver’s name`],
      ],
    ),

    subsection(`The payment`),

    prose(`Marked **Read-only**, because it is: the date, the mode, the reference, who recorded it and
       when, the payee and the invoice number. **Recorded settlement** reads either \`Fully settled —
       deductions handled outside this system\` or \`Partial payment recorded\`; the second covers
       both an installment and a shortfall sent for review, so read the balance line above to tell
       them apart. Any explanation you gave shows as **Settlement reason**, and your note as
       **Processing note**.`),

    subsection(`The proof`),

    prose(`Both halves of it, when there is any: the bank advice Accounts attached at settlement, and
       the invoice the request came in with, marked *Invoice from the request*. Each has a
       **Download** control.`),

    subsection(`The trail`),

    prose(`**Full trail, request to payment** merges the request’s history with the payment’s, oldest
       first — submitted, approved, reserved, paid, settled. It is the same story anybody else
       reading the request sees.

       At the foot, **Open the request** and **Back to ledger**, beside a note stating the limit of
       this version: refunds and reversals are outside it.`),

    faq([
      { q: `A payment I can see in the ledger opens as “not found”.`, a: `The payment is real, but the request behind it is outside the records your role is allowed to read. The product answers the same way for a record that does not exist and one that is not yours, on purpose.` },
      { q: `The total does not match the sum of the amounts on screen.`, a: `The total counts active payments only. Switch **Status** to \`All\` and the removed rows appear, marked \`Removed\`, still outside the total.` },
      { q: `Can I export the ledger?`, a: `Not from this screen. The reports and the recoverables register both offer **Export CSV**.` },
      { q: `Why can I not edit a payment I recorded five minutes ago?`, a: `Because it settled a request, and what the requester and the approver were told was paid cannot be quietly rewritten. [Correcting or voiding a payment](page:accounts/correcting-a-payment) explains what is still possible.` },
    ]),
  ],
});
