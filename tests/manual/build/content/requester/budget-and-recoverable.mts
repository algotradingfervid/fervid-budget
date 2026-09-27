import { page, prose, section, subsection, shot, note, warning, tip, table, fields, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'budget-and-recoverable',
  title: 'Budget spend or money you expect back',
  summary: 'The treatment choice, and what changes when you say the money is coming back.',
  blocks: [
    prose(
      `Every request carries a *treatment* as well as a type. The type says what kind of payment it is;
       the treatment says what happens to the money afterwards. There are two. Only an employee
       advance lets you choose; every other type states its treatment near the top of the form.`,
    ),
    table(
      ['Treatment', 'What the form says about it', 'Where the money lands'],
      [
        ['**Budget expense**', 'Money spent and gone. Counts against a project and head.', 'Against the budget for that head, in the variance figures.'],
        ['**Refundable or recoverable**', 'A deposit, guarantee, loan or advance you expect back. Kept out of budget actuals.', 'In the recoverables register, tracked until it comes back.'],
      ],
      'The descriptions are the employee advance form’s own wording.',
    ),

    section('Which one you want'),
    prose(
      `Ask one question: is the organisation going to see this money again? A supplier's bill, a
       courier charge, a repair — that money is gone, and it is a budget expense. A deposit against a
       tender, a guarantee lodged with a client, money handed to you before you spend it — that money
       is expected back, and it is recoverable.`,
    ),
    tip(
      `Getting this wrong distorts the budget. A ₹5,00,000 deposit booked as an expense makes a head
       look overspent for a month and under-spent again when the deposit returns, and nobody reading
       the variance grid can tell why.`,
    ),

    section('Where the choice lives'),
    shot('requester/form-employee-advance', 'The treatment radio sits in the first fieldset, above everything the choice affects.'),
    prose(
      `On an **Employee advance** the two choices sit under **How should this be treated**, and the
       form opens on **Refundable or recoverable**. Whichever one is selected is highlighted; the
       block immediately below changes with it.

       The other types do not ask. Under **How this is treated** a vendor invoice payment, a vendor
       advance and a reimbursement show a fixed line, *Budget expense. Money spent and gone. It
       counts against a project and head.*, with the hint *Only an employee advance can be marked
       refundable or recoverable. A deposit or guarantee is its own request type.* A **Deposit or
       guarantee** shows the opposite fixed line, *Refundable or recoverable.*, because it is always
       money expected back.`,
    ),
    note(
      `If money you expect back is going to another company or authority — a tender deposit, a
       guarantee — raise it as a **Deposit or guarantee**, not as a vendor payment.`,
      'Money a vendor will return',
    ),

    section('The recoverable fieldset'),
    shot('requester/form-recoverable', 'Choosing Refundable or recoverable replaces Charge it to with Recoverable details.'),
    prose(
      `**Charge it to** — the project and head pair — disappears, because a recoverable is not charged
       to a budget line. In its place comes **Recoverable details**.`,
    ),
    fields([
      {
        name: 'Category',
        required: true,
        note: 'What kind of recoverable this is. The list is maintained by your administrator, and the hint under the control says so. Some categories bring an extra field with them.',
      },
      {
        name: 'Expected return date',
        required: true,
        note: 'When the money is due back. This is what the recoverables register ages against.',
      },
      {
        name: 'Related project',
        required: false,
        note: 'Appears only for categories that always belong to a project, and is required when it does. The hint names the category: *EMD always belongs to a project.*',
      },
      {
        name: 'Counterparty company',
        required: false,
        note: 'Appears only for categories that need one, and is required when it does. The placeholder reads *Company receiving the deposit*.',
      },
      {
        name: 'Repayment or refund terms',
        required: true,
        note: 'A free-text box saying how and when the money comes back. Always required on a recoverable.',
      },
    ]),
    prose(
      `Below the fieldset sits a banner repeating the consequence: **This will not touch budget
       actuals** — *It appears in Recoverable payments instead.*`,
    ),

    subsection('The categories, and what each one asks for'),
    table(
      ['Category', 'Also requires'],
      [
        ['Employee advance', 'Nothing beyond the common fields'],
        ['EMD', 'A related project'],
        ['PBG', 'A related project'],
        ['ICD', 'A counterparty company'],
        ['Security deposit', 'A counterparty company'],
        ['Other', 'Nothing beyond the common fields'],
      ],
      'The set your administrator has configured. Categories can be added, renamed or retired, so yours may differ.',
    ),
    note(
      `Pick the category before you fill in the rest of the fieldset. The extra field a category needs
       only appears once that category is chosen, and choosing a different one afterwards swaps the
       field for a different one.`,
    ),

    section('The employee advance is different'),
    prose(
      `Open an employee advance and the form is already on **Refundable or recoverable**, with the
       **Employee advance** category already chosen. That is deliberate: money handed to a person
       before it is spent is money the organisation expects to see accounted for. You may still switch
       it to **Budget expense**, and the form then asks for a project and head like any other spend.`,
    ),

    section('How a recoverable reads afterwards'),
    shot('requester/recoverable-request', 'A recoverable request. The pill beside Details names the treatment and the category, and two extra rows carry the return date and terms.'),
    prose(
      `On the request itself, the badge at the top right of **Details** reads
       **Recoverable · Employee advance** rather than **Budget expense**. **Expected return** and
       **Repayment terms** appear as rows of their own, and there is no **Project** or **Head** row
       because the request was never charged to one.`,
    ),
    prose(
      `In **My requests**, a recoverable carries a violet pill on its card reading **Recoverable**
       followed by its category, so you can pick one out of a list at a glance. The **Treatment** filter
       on the same screen narrows the list to **Budget expense** or **Recoverable**.`,
    ),
    prose(
      `Once paid, the request also appears in the recoverables register that the accounts team works
       from — see [The recoverables register](page:accounts/the-recoverables-register) for what happens
       to it there.`,
    ),

    faq([
      {
        q: 'I chose the wrong treatment. Can I change it?',
        a: `Only while the request is still yours to edit — while it is awaiting approval or has been
            returned to you. The edit form keeps the treatment you chose and does not offer to swap it,
            so in practice you withdraw the request and raise it again. See
            [Changing or withdrawing a request](page:requester/editing-and-withdrawing).`,
      },
      {
        q: 'The category I need is not in the list.',
        a: `Categories are administrator-maintained. Ask for it to be added — see
            [Recoverable categories](page:admin/recoverable-categories) — and use **Other** with a clear
            explanation in **Repayment or refund terms** in the meantime.`,
      },
      {
        q: 'Does a recoverable still need approving?',
        a: `Yes. It follows exactly the same route as a budget expense: your approver decides it, and
            Accounts pays it. Only the accounting treatment differs.`,
      },
      {
        q: 'What if the money never comes back?',
        a: `That is handled outside your request, in the recoverables register. The request itself is
            complete once the money has been paid out.`,
      },
    ]),
  ],
});
