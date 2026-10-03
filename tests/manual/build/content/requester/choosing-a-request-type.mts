import { page, prose, section, subsection, shot, steps, note, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'choosing-a-request-type',
  title: 'Choosing a request type',
  summary: 'Vendor invoice, vendor advance, reimbursement, employee advance or deposit or guarantee — which one you want, and why it matters.',
  blocks: [
    prose(
      `Raising a request is two screens. The first asks one question — what kind of payment is this —
       and the answer decides which fields the second screen puts in front of you. There is no way to
       change the type afterwards, so it is worth a moment.`,
    ),

    section('The chooser'),
    shot('requester/choose-type', 'Step 1 of 2. Five cards, each with a one-line description and a tag saying what that type will ask for.'),
    steps([
      { text: 'Select **+ New request**, from the dashboard or from **My requests**.' },
      {
        text: 'Read the five cards and select the one that describes your money.',
        note: 'Selecting a card opens the form for that type. Nothing is saved yet.',
      },
    ]),
    prose(
      `The heading asks *What are you asking to be paid?* and the line under it promises the rest:
       *Pick a type. The form only asks for what that type needs. Nothing is saved until you submit.*
       **Cancel**, at the top right, takes you back to your home screen without raising anything.`,
    ),

    section('The five types'),
    table(
      ['Type', 'Use it when', 'The card’s tag'],
      [
        ['**Vendor invoice payment**', 'You have an invoice from a vendor and it needs paying.', 'Needs invoice number and date'],
        ['**Vendor advance**', 'Money to a vendor before any invoice exists — a deposit against an order.', 'Needs a reason for the advance'],
        ['**Reimbursement**', 'You already spent your own money for the company and want it back.', 'Paid to you · needs the expense date'],
        ['**Employee advance**', 'Money to you up front for organisation spending you are about to make.', 'Paid to you · usually recoverable'],
        ['**Deposit or guarantee**', 'EMD, PBG, ICD or a security deposit — money paid to another company or authority that comes back.', 'Paid to the counterparty · always recoverable'],
      ],
      'The wording is the chooser’s own.',
    ),

    subsection('Vendor invoice payment'),
    shot('requester/form-vendor-invoice', 'The vendor invoice form. Its own fieldset is Vendor and invoice.'),
    prose(
      `This is the ordinary case: a supplier has billed you and the bill needs settling. The form adds
       a **Vendor and invoice** fieldset with **Vendor**, **Invoice number** and **Invoice date**, all
       three required. The vendor comes from the vendor master — you pick an existing one, you do not
       type a new payee — and the hint under the control says why: *Bank details stay in the vendor
       record — never on this form.*`,
    ),

    subsection('Vendor advance'),
    shot('requester/form-vendor-advance', 'The vendor advance form. Same vendor control, but it asks for a reason instead of an invoice.'),
    prose(
      `Use this when money has to reach a vendor before there is anything to pay against — a deposit,
       a booking, a payment on order. The fieldset is **Vendor and advance**, and it asks for
       **Vendor** and **Reason for the advance**. There is no invoice number, because there is no
       invoice.`,
    ),

    subsection('Reimbursement'),
    shot('requester/form-reimbursement', 'The reimbursement form. Paid to is filled in with your name and cannot be changed.'),
    prose(
      `Use this when the money has already left your own pocket. The **Your expense** fieldset shows
       **Paid to** already filled in with your name and not editable — the hint says *Reimbursements
       always pay the person raising them* — and asks for the **Expense date**. There is no vendor,
       because the payee is you.`,
    ),

    subsection('Employee advance'),
    shot('requester/form-employee-advance', 'The employee advance form. It opens on the recoverable treatment, already categorised.'),
    prose(
      `Use this when the organisation is handing you money to spend on its behalf. **Paid to** is your
       name again — the hint says *An employee advance always pays the person raising it* — and the
       form asks **What the money is for**. It opens on **Refundable or recoverable** rather than
       **Budget expense**, because money advanced to a person is money the organisation expects to see
       accounted for. On the recoverable treatment the **Category** is fixed at **Employee advance** and
       cannot be changed; its hint reads *An employee advance is always in this category. A deposit,
       guarantee or loan is raised as its own request type.* It is the only type that lets you choose
       its treatment, so you can still switch it to a budget expense — see
       [Budget spend or money you expect back](page:requester/budget-and-recoverable).`,
    ),

    subsection('Deposit or guarantee'),
    prose(
      `Use this when money goes to another company or authority and is expected to come back — an
       earnest money deposit on a tender, a performance bank guarantee, an inter-corporate deposit, a
       security deposit. It is always recoverable: under **How this is treated** the form states
       *Refundable or recoverable.* rather than offering a choice. It asks for **Recoverable details** —
       a **Category** from your administrator's list (every category except Employee advance), an
       **Expected return date**, **Repayment or refund terms**, and the extra field the category needs —
       and a **Who is paid** fieldset with one required field, **Paid to**. You type the payee's name
       yourself; the placeholder reads *Counterparty company or tender authority*, and the hint says it
       is never you: *money advanced to yourself is an employee advance.*`,
    ),

    section('Choosing between them'),
    tip(
      `The banner at the foot of the chooser is the shortest rule there is: *If the money leaves the
       company and never comes back, it is an expense. A deposit or guarantee paid to another company
       or authority is money the company expects back — raise it as a deposit or guarantee, paid to
       them. Money advanced to you that you will account for is an employee advance, paid to you.*`,
      'Not sure which one?',
    ),
    prose(
      `Two questions settle almost every case. *Who is being paid?* — a vendor takes you to the two
       vendor types, yourself to reimbursement or employee advance, and another company or authority
       holding money that will come back to a deposit or guarantee. *Has the money been spent yet?* —
       spent takes you to vendor invoice or reimbursement, not yet takes you to one of the advances.`,
    ),
    note(
      `"Recoverable" is a *treatment*, not a type. A vendor invoice payment, a vendor advance and a
       reimbursement are always budget expenses; a deposit or guarantee is always recoverable; only an
       employee advance lets you choose. Typing a type into the address that has no card here brings
       you back to this screen with the message *That is not a request type this system raises. Pick
       one below — a deposit or guarantee has its own card, and an advance to yourself is an employee
       advance.*`,
    ),

    section('Changing your mind'),
    prose(
      `The form carries a **← Change type** button at the top right. It takes you back to the chooser.
       Nothing you have typed is kept when you swap type, because a different type is a different set
       of fields — so change type before you fill the form in, not after.`,
    ),

    faq([
      {
        q: 'I paid a vendor out of my own pocket. Which type?',
        a: `Reimbursement. The organisation is paying *you* back, so the payee is you, not the vendor —
            and the vendor invoice type would ask you to name a vendor as the payee.`,
      },
      {
        q: 'The vendor I need is not in the list.',
        a: `Vendors come from the vendor master, which administrators maintain. Ask for the vendor to
            be added; you cannot type a payee that is not on the list.`,
      },
      {
        q: 'Can I change the type after submitting?',
        a: `No. The type is fixed when the request is created. Withdraw the request while it is still
            awaiting approval and raise it again under the right type — see
            [Changing or withdrawing a request](page:requester/editing-and-withdrawing).`,
      },
    ]),
  ],
});
