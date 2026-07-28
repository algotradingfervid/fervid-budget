import { page, prose, section, shot, steps, note, warning, table, fields, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'vendor-bank-details',
  title: 'Vendor bank details',
  summary: 'The most sensitive data in the product, and who is allowed to see it.',
  blocks: [
    prose(
      'An account number and an IFSC are the two pieces of data that turn a request into money leaving ' +
      'a bank. They are held on the vendor record, and they are the one block in the product with ' +
      'their own permission — separate from the permission that opens the rest of the vendor.\n\n' +
      'The consequence is that "can see vendors" and "can see how to pay them" are two different ' +
      'grants, and most people are meant to hold only the first.',
    ),

    section('Where the block is'),
    shot('admin/vendor-detail', 'A vendor record. The Payment details block sits between Contact and Notes, behind its own padlocked banner.'),
    prose(
      'On a vendor you are allowed to see it, the block is headed **Payment details — restricted** and ' +
      'opens with a padlocked banner: "Visible only with vendor bank permission. They are deliberately ' +
      'kept off the employee request form — an employee raising a request never sees or types them."',
    ),

    section('The fields'),
    fields([
      { name: 'Account name', required: false, note: 'The name the account is held in. It will not always match the vendor name.' },
      { name: 'Account number', required: false, note: 'Recorded as given.' },
      { name: 'IFSC', required: false, note: 'Recorded as given.' },
      { name: 'Bank', required: false, note: 'The bank.' },
      { name: 'Branch', required: false, note: 'The branch.' },
      { name: 'UPI ID', required: false, note: 'Optional, and marked as such on the form.' },
      { name: 'Default mode', required: false, note: '`Not set`, `Bank transfer — NEFT`, `RTGS` or `Cheque`. The mode you would normally pay this vendor by.' },
      { name: 'Payment terms (days)', required: false, note: 'How many days the vendor is normally given. A number.' },
    ]),
    steps([
      { text: 'Open the vendor from [Vendors](page:admin/vendors).' },
      { text: 'Scroll to **Payment details — restricted**.' },
      { text: 'Fill in or correct the fields.' },
      {
        text: 'Select **Save vendor**.',
        note: 'The bank block is saved with the rest of the record, in the same submission.',
      },
    ]),

    section('Who sees what'),
    prose(
      'There are two grants, not one, and they combine into three states. They are set on the ' +
      '**Vendor bank details** row of [Roles and permissions](page:admin/roles-and-permissions), which ' +
      'offers a **View** column and an **Edit** column and nothing else.',
    ),
    table(
      ['The role holds', 'What the person sees on a vendor'],
      [
        ['Neither grant', 'A block headed **Payment details** carrying a padlocked banner: "You do not have permission to see bank details — Accounts maintains them. The record is hidden, not just the buttons."'],
        ['View only', 'The real block, filled in, with every field greyed out and unchangeable.'],
        ['View and edit', 'The real block, editable, saved along with the rest of the record.'],
      ],
    ),
    note(
      'The refusal banner is deliberately shown instead of the block vanishing. A missing section ' +
      'leaves a reader wondering whether the vendor has no bank details or whether they are not ' +
      'allowed to see them; the banner answers that question.',
      'Why the block does not disappear instead',
    ),
    warning(
      'For somebody without the view grant, the bank columns are never read out of the database at ' +
      'all. It is not a hidden field on a page that quietly holds the number — there is nothing on the ' +
      'page to find. That is the guarantee worth understanding before you decide who gets the grant.',
      'Hidden, not merely not shown',
    ),
    note(
      'A field that is greyed out is not submitted by the browser, and the save ignores it in any case. ' +
      'Somebody with view but not edit cannot change a bank detail by any route the screen offers.',
      'View without edit',
    ),

    section('Where the details deliberately are not'),
    prose(
      'The vendor picker on the request form returns no bank details to anybody, whatever their role. ' +
      'A requester choosing a vendor gets the name, the GSTIN and the city, and nothing else — the ' +
      'picker cannot become a side door onto the restricted block.\n\n' +
      'This is why the banner says the details are kept off the employee request form. An employee ' +
      'raising a request never sees or types an account number, so a mistyped one cannot reach a ' +
      'payment from that direction.',
    ),

    section('Who should hold it'),
    prose(
      'That is your organisation’s decision rather than the product’s, so this page will not pretend ' +
      'to make it. What the product gives you is the ability to make it precisely: the grant is ' +
      'separable from vendor viewing, from vendor editing and from paying, so "the accounts team ' +
      'maintains bank details and nobody else sees them" is a policy you can actually implement.',
    ),
    warning(
      'A backup contains the bank block in full, because it is a copy of the database. Anybody who can ' +
      'reach the backup folder on the server has the account numbers, whatever their role inside the ' +
      'product says. See [Backups](page:admin/backups).',
      'Backups carry them too',
    ),

    faq([
      {
        q: 'I can open a vendor but the Payment details block is padlocked.',
        a: 'Your role does not hold the vendor bank view grant. It is a different grant from the one that let you open the vendor.',
      },
      {
        q: 'I can see the bank details but cannot type in them.',
        a: 'You hold view and not edit. Both are on the **Vendor bank details** row of [Roles and permissions](page:admin/roles-and-permissions).',
      },
      {
        q: 'Does the accounts team need this grant to record a payment?',
        a: 'Recording a payment does not require it — the payment form asks for the mode and the reference, not the account number. Whether your accounts team needs to read the details to make the transfer is a question about how they bank, not about the product.',
      },
      {
        q: 'Are bank details written into the audit trail?',
        a: 'No. A vendor save writes an audit row carrying the name, type, status, GSTIN and city, and for the bank block only a `bank_changed` flag saying whether it was touched. The trail records that somebody changed the details, not what they changed them to.',
      },
    ]),
  ],
});
