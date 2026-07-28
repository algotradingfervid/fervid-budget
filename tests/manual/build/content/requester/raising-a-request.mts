import { page, prose, section, subsection, shot, steps, note, warning, tip, table, fields, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'raising-a-request',
  title: 'Raising a request',
  summary: 'Filling in the form and sending it for approval, field by field.',
  blocks: [
    prose(
      `Once you have picked a type, the second screen is the request itself. It is one form, one
       button, and one act: there is no draft to save and come back to. Pressing **Submit request**
       creates the request, gives it its number and sends it to your approver, all at the same moment.`,
    ),

    section('The form, top to bottom'),
    shot('requester/form-vendor-invoice', 'Step 2 of 2, here for a vendor invoice. The fieldsets run in a fixed order and only the middle of the form changes with the type.'),
    prose(
      `Every fieldset is the same on every type except two: the block under **What is this for**,
       which follows your treatment choice, and the type's own block — **Vendor and invoice**,
       **Vendor and advance**, **Your expense** or **Advance details**. A red asterisk marks the
       fields the product will insist on.`,
    ),

    subsection('What is this for'),
    fields([
      {
        name: 'Short title',
        required: true,
        note: 'One line naming the spend. The hint says what it is for: *What your approver will see in their approval list.*',
      },
      {
        name: 'How should this be treated',
        required: true,
        note: 'Two radio buttons — **Budget expense** or **Refundable or recoverable**. Changing it changes the next fieldset. See [Budget spend or money you expect back](page:requester/budget-and-recoverable).',
      },
    ]),

    subsection('Charge it to'),
    fields([
      {
        name: 'Project',
        required: true,
        note: 'The project the spend belongs to. Choosing one narrows the **Head** list below to that project’s own heads.',
      },
      {
        name: 'Head',
        required: true,
        note: 'The budget line the spend is booked against. Options read *Project / Head*, so you can see which project a head belongs to.',
      },
    ]),
    note(
      `This fieldset only appears on a **Budget expense**. Choose **Refundable or recoverable** and it
       is replaced by **Recoverable details** — a category, an expected return date and the repayment
       terms.`,
    ),

    subsection('Amount and timing'),
    fields([
      {
        name: 'Amount',
        required: true,
        note: 'The figure you are asking for, typed without the ₹ — the field draws its own. Before you type, the line beneath reads *Enter the amount you are requesting*; once you do, it spells the figure out in words.',
      },
      {
        name: 'Needed by',
        required: false,
        note: 'A date. The hint says *Optional. It tells your approver how long they have.*',
      },
      {
        name: 'Mark this urgent',
        required: false,
        note: 'A checkbox. Ticking it reveals **Why is it urgent**, which is then required. See [Marking a request urgent](page:requester/urgent-requests).',
      },
    ]),

    subsection('The type’s own fieldset'),
    table(
      ['Type', 'Fieldset', 'What it asks'],
      [
        ['Vendor invoice payment', '**Vendor and invoice**', '**Vendor**, **Invoice number**, **Invoice date** — all three required'],
        ['Vendor advance', '**Vendor and advance**', '**Vendor** and **Reason for the advance**'],
        ['Reimbursement', '**Your expense**', '**Paid to** (your name, fixed) and **Expense date**'],
        ['Employee advance', '**Advance details**', '**Paid to** (your name, fixed) and **What the money is for**'],
      ],
    ),
    shot('requester/form-reimbursement', 'The reimbursement form. Paid to carries your name and cannot be edited; the hint explains why.'),

    subsection('Purpose and documents'),
    fields([
      {
        name: 'Purpose',
        required: true,
        note: 'A free-text box. This is where you explain the spend to somebody who was not in the conversation.',
      },
      {
        name: 'Supporting document',
        required: false,
        note: 'One file. The uploader says **Add invoice, receipt or proof** and, beneath, *PDF, JPG or PNG up to 10 MB* — the size is a figure your administrator sets. See [Attaching documents](page:requester/attachments).',
      },
    ]),
    note(
      `**Supporting document** is marked *optional* here because this organisation has not made
       documents compulsory. Where an administrator has switched that on, the label carries a red
       asterisk instead and a further field appears asking you to say why, if you cannot attach one.`,
    ),

    subsection('Who approves it'),
    fields([
      {
        name: 'Approver',
        required: true,
        note: 'The person who will decide. Your own default is already selected and marked *(your default)*. The hint says *You cannot approve your own request. Your own name is never in this list.*',
      },
    ]),
    prose(
      `Beside it sits **Reminders**, which is not a control but a statement of what happens if nobody
       acts: *If nothing happens for 3 days, this request starts sending a reminder every day to
       whoever it is waiting on.* Both numbers are settings, so yours may read differently.`,
    ),

    section('Submitting'),
    steps([
      { text: 'Fill in every field carrying a red asterisk.' },
      {
        text: 'Attach the invoice, receipt or proof if you have one.',
        note: 'You can add more documents later while the request is still editable.',
      },
      { text: 'Check the **Approver** is the right person.' },
      {
        text: 'Select **Submit request**.',
        note: 'The note beside the button says what that does: *Submitting sends it to your approver and creates the request number.*',
      },
    ]),
    prose(
      `You land on a confirmation screen. It leads with **Submitted.**, followed by your approver's name
       and **has been notified.** It shows the request's new number and amount, and lays out **What
       happens next** in three steps: your approver reviews it, Accounts picks it up once approved, and
       it settles. The buttons at the foot are **Raise another**, **My requests** and
       **View this request**.`,
    ),
    warning(
      `There is no draft. Nothing you type is stored until you press **Submit request**, so do not
       leave the form half-filled and expect to come back to it. If you close the tab, you start again.`,
      'Nothing is saved until you submit',
    ),

    section('When the form comes back'),
    shot('requester/form-validation', 'A submission the product refused: the red band across the top names the first problem it found, and the form comes back with everything you typed still in place. Every field it insists on carries a red asterisk beside its label.'),
    prose(
      `If something is missing or contradictory, the form returns with a red band across the top
       carrying one sentence — the first problem it found. Everything you had already typed is still
       there; you are never made to fill the page in twice.`,
    ),
    table(
      ['What the band says', 'What to do'],
      [
        ['`a short title is required — it is what your approver sees in their list`', 'Type a **Short title**.'],
        ['`enter the amount you are requesting`', 'The **Amount** was empty, zero, negative or not a number. This is the message in the screenshot above.'],
        ['`purpose is required`', 'Fill in **Purpose**.'],
        ['`choose an approver`', 'Pick somebody in **Approver**.'],
        ['`project and head are required for this type`', 'Choose both **Project** and **Head**.'],
        ['`choose a vendor from the vendor master`', 'Pick a vendor from the list rather than typing one.'],
        ['`the invoice number is required`', 'Fill in **Invoice number** on a vendor invoice.'],
        ['`the invoice date is required`', 'Fill in **Invoice date**, in a form the product recognises as a date.'],
        ['`the expense date is required`', 'Fill in **Expense date** on a reimbursement.'],
        ['`say what the advance is for`', 'Fill in **Reason for the advance** on a vendor advance.'],
        ['`say what the money is for`', 'Fill in **What the money is for** on an employee advance.'],
        ['`that budget head belongs to a different project — choose a head under the project you named`', 'The head and the project disagree. Re-pick the project, then the head.'],
        ['`that budget head has been retired — choose a head that is still open`', 'An administrator has closed that head. Pick another.'],
        ['`that vendor is no longer active — choose an active vendor`', 'The vendor has been deactivated in the vendor master.'],
        ['`say why this is urgent`', 'You ticked **Mark this urgent** and left the reason empty.'],
      ],
      'The wording is the product’s own, taken from the running system.',
    ),
    note(
      `Every one of these rules is enforced on the server, not only in the browser. Hiding a field or
       switching one off in the page does not get a request past them.`,
    ),

    section('The duplicate warning'),
    prose(
      `When you leave the **Amount** field — and, on a vendor invoice, the **Invoice number** field —
       the product quietly looks for requests that resemble this one: the same payee, and either an
       amount within about one per cent or the very same invoice reference, raised in the last thirty
       days. If it finds any, a warning appears above the buttons listing them, saying *Same payee and
       a close amount in the last 30 days. Check before you submit — you can still go ahead.*`,
    ),
    tip(
      `The warning never blocks you. Repeat payments are real — the same rent, the same monthly
       retainer — so the product points and you decide.`,
    ),

    section('The request number'),
    prose(
      `The number is issued at submission and never changes afterwards. In this organisation it is
       shaped \`PR-2026-000426\`: a prefix, the year, and a running count. Editing a request keeps its
       number; withdrawing it keeps it; only raising a brand-new request produces a new one.`,
    ),

    faq([
      {
        q: 'Can I save this and finish it tomorrow?',
        a: `No — there are no drafts. Either submit it, or raise it again tomorrow from scratch. A
            submitted request can still be edited while it is awaiting approval, so submitting early
            is usually the safer choice.`,
      },
      {
        q: 'The Head list is empty.',
        a: `Choose a **Project** first. The head list narrows to the heads belonging to the project
            you named, and only heads that are still open appear at all.`,
      },
      {
        q: 'My approver is not in the list.',
        a: `The list holds only active people who are allowed to approve, and never you. If the person
            you want is missing, ask an administrator to give them the approver role.`,
      },
      {
        q: 'I typed ₹ in the amount and it was refused.',
        a: `The field draws its own ₹ symbol at the left. Type the figure only.`,
      },
    ]),
  ],
});
