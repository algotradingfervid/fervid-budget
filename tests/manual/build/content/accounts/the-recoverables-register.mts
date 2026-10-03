import { page, prose, section, shot, steps } from '../../schema.mts';

export default page({
  section: 'accounts',
  slug: 'the-recoverables-register',
  title: 'The recoverables register',
  summary: 'Track money paid out, record returns or reconciliations, and retain the history.',
  blocks: [
    prose(`Recoverables are money expected back, such as employee advances and deposits. They do not
       count as budget actuals. The sidebar item is **Recoverables** and the screen is headed
       **Recoverable payments**. The payout request's approval/payment status is separate from
       whether the money has been recovered.`),

    section(`Read the balances`),

    prose(`**Outstanding** is active recoverable payouts less recorded returns and reconciliations.
       Unpaid requests do not add to outstanding or overdue money. **Past expected return** and
       **Due in 30 days** apply to money still outstanding; paid-out-this-month describes outgoing
       recoverable payments.

       Rollups group this balance by category and counterparty. Employee advances identify the
       employee even when no separate counterparty was entered. The register obeys your request
       data scope.`),

    shot('accounts/recoverables', 'The register. Four tiles, a rollup by category and a rollup by counterparty, under a banner saying none of it counts as budget spend.'),

    section(`Filter the register`),

    shot('accounts/recoverables-list', 'The full list: one row per recoverable, aged against its expected return date. On a desktop the filters sit in a bar above the table, where the button reads **Apply**.'),

    prose(`Use Search, Category and Ageing on desktop and select **Apply**, or open **Filters** on
       mobile, choose the options, then **Apply filters**. **Reset** clears them. The Ageing options
       are All, Outstanding, Overdue, Due in 30 days, Due later, Not yet paid and Reconciled; the
       Reconciled option includes items whose paid balance has been cleared, and their history is
       retained.`),

    section(`Export the register`),

    prose(`**⤓ Export CSV** at the top right downloads the register as \`recoverables.csv\`. On the
       full list it downloads exactly the rows the list is showing, with every filter applied —
       search, category, ageing, and the counterparty when you arrived from the counterparty
       rollup. On the summary screen it downloads the outstanding recoverables. A single
       recoverable offers **⤓ Export this record** at its foot. Each download is written to the
       audit log.

       The file has the same columns as the list: Number, Category, Counterparty, Project,
       Outstanding, Paid out, Recovered or reconciled, Paid On, Expected Return, Ageing, Status,
       Requester and Repayment Notes. The export needs the recoverables export permission, which
       the accounts role holds; without it the button is not shown.`),

    section(`One recoverable`),

    prose(`The detail shows **Outstanding balance**, **Total paid out** and **Returned or reconciled**,
       followed by **Latest payment** (with a link to all payments and the request) and **Recovery
       history**. Conversation notes remain useful for chasing a counterparty, but they do not
       change the financial balance.`),

    shot('accounts/recoverable-detail', 'A single recoverable. This one is still awaiting approval, so the payment section says no payment has been recorded and the recovery history is empty.'),

    section(`Record a return or reconciliation`),

    steps([
      { text: `Open the recoverable and scroll to **Record recovery**. The form appears only while there is an outstanding balance, and only for someone with recoverable report access, payment creation permission and access to that request.` },
      { text: `Choose **Recovery type**: Money returned, Expense reconciled or Balance adjustment.` },
      { text: `Enter **Amount received or reconciled (₹)** and **Recovery date**.` },
      { text: `Provide the **Bank / accounting reference** and **Evidence and explanation**. All fields are required; retain the underlying bank advice or accounting evidence outside this form and describe it.` },
      { text: `Select **Record recovery**. The browser asks you to confirm that the entry is permanent; check the amount, date and evidence before you do.` },
    ]),

    prose(`The amount must be positive and within both the current and historical balance at that date.
       A recovery cannot precede the first payout or be future-dated. Each entry reduces
       outstanding; the last one marks the balance Reconciled while retaining the history.`),

    section(`Accounting and corrections`),

    prose(`Expense reconciliation and balance adjustment record a referenced accounting decision; they
       do not automatically post an expense to the budget. Recovery entries are immutable and have
       no edit/delete reversal flow. Check the evidence before confirming and use your
       organisation's accounting correction process for mistakes.

       Recovery does not change the payout request's status, and saving an outgoing payment does
       not mean its recoverable has been returned.`),
  ],
});
