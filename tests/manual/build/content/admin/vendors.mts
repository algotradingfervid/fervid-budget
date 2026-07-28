import { page, prose, section, subsection, shot, steps, note, warning, tip, table, fields, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'vendors',
  title: 'Vendors',
  summary: 'The vendor master, and the payment history behind each one.',
  blocks: [
    prose(
      'The vendor master is the list of everybody the company pays that is not an employee. A ' +
      'requester picks from it rather than typing a name, so the same supplier is spelt the same way ' +
      'on every request, and the reporting can add them up.\n\n' +
      'A vendor record has four ordinary parts — identity, statutory numbers, contact details and ' +
      'notes — and one restricted part, the bank block, which is covered on its own page: ' +
      '[Vendor bank details](page:admin/vendor-bank-details).',
    ),

    section('The vendor list'),
    shot('admin/vendors', 'The vendor list. The sub-line under the heading counts what is there and what is missing.'),
    prose(
      'The line under the heading is a summary of the master itself: how many vendors are active, how ' +
      'many are missing a GSTIN, and the reminder that bank details are visible only with permission.',
    ),
    table(
      ['Column', 'What it shows'],
      [
        ['Vendor', 'The name, linking to the record. Its categories, if any, sit underneath.'],
        ['Type', '`Company`, `Proprietor` or `Individual`.'],
        ['GSTIN', 'The number, or a `Missing` pill.'],
        ['City', 'From the contact block, or an em dash.'],
        ['Paid this year', 'What has actually gone out to this vendor.'],
        ['Open requests', 'How many requests naming this vendor are still in flight.'],
        ['Status', '`Active` or `Inactive`.'],
      ],
    ),
    prose(
      'The row across the foot totals the two figures for whatever the filters left on screen, and ' +
      'says how many of the whole master you are looking at — "12 shown of 12" in the capture above.',
    ),

    subsection('The GSTIN banner'),
    prose(
      'When any vendor has no GSTIN, a banner appears above the filters counting them. It is a nudge, ' +
      'not a block — the wording is explicit that "They can still be paid. The gap shows up in vendor ' +
      'reporting until someone fills it in." **Show them** filters the list down to exactly those ' +
      'vendors, and **Show all** puts it back.',
    ),

    subsection('Finding a vendor'),
    fields([
      { name: 'Search', required: false, note: 'Matches on name, GSTIN, PAN or city.' },
      { name: 'Type', required: false, note: '`All types`, `Company`, `Proprietor` or `Individual`.' },
      { name: 'Category', required: false, note: 'The categories that exist in the master, offered as a list.' },
      { name: 'Status', required: false, note: '`Active` by default, or `Inactive`, or `All`. An inactive vendor will not appear until you change this.' },
    ]),
    steps([
      { text: 'Fill in whichever filters narrow things down.' },
      { text: 'Select **Apply**.' },
    ]),

    section('Adding a vendor'),
    shot('admin/vendor-new', 'The blank vendor form. Only two fields carry the required marker.'),
    steps([
      { text: 'Select **＋ Add vendor**.' },
      { text: 'Type the **Vendor name**. It must be unique across the master.' },
      { text: 'Choose a **Type**.' },
      { text: 'Fill in whatever else you have to hand.' },
      {
        text: 'Select **Save vendor**.',
        note: 'You land on the vendor’s own record. The note beside the buttons on the blank form says it plainly: the name must be unique, and everything else can be filled in later.',
      },
    ]),

    subsection('The fields'),
    fields([
      { name: 'Vendor name', required: true, note: 'The full legal name. Marked required on the form.' },
      { name: 'Short name', required: false, note: 'Used in lists and dropdowns when the full name is unwieldy.' },
      { name: 'Type', required: true, note: '`Company`, `Proprietor` or `Individual`.' },
      { name: 'Categories', required: false, note: 'Comma separated. Used for vendor reporting, and it is what fills the Category filter on the list.' },
      { name: 'Status', required: false, note: '`Active` or `Inactive`. Inactive vendors disappear from new requests but stay on old ones.' },
      { name: 'GSTIN', required: false, note: 'Exactly 15 characters, or blank. Anything else is refused with `a GSTIN is 15 characters; leave it blank if the vendor has none`. It is stored upper-case.' },
      { name: 'PAN', required: false, note: 'Recorded as given.' },
      { name: 'MSME / Udyam number', required: false, note: 'Recorded as given.' },
      { name: 'TDS section', required: false, note: '`Not applicable`, `194C — contractors`, `194J — professional` or `194Q — purchase of goods`.' },
      { name: 'Default TDS rate', required: false, note: 'A rate for reference. The product never computes tax from it.' },
      { name: 'Contact person', required: false, note: 'Who to speak to.' },
      { name: 'Phone', required: false, note: 'Recorded as given.' },
      { name: 'Email', required: false, note: 'Recorded as given.' },
      { name: 'Billing address', required: false, note: 'Free text.' },
      { name: 'City', required: false, note: 'Shown on the vendor list and searchable.' },
      { name: 'State', required: false, note: 'Free text.' },
      { name: 'State code', required: false, note: 'The first two digits of the GSTIN.' },
      { name: 'Internal notes', required: false, note: 'For your own team. Nothing outside this screen shows it.' },
    ]),
    note(
      'The statutory block is a record, not a calculator. The hint beside it says so: the numbers are ' +
      'recorded for reference only, this system never computes tax, and Accounts confirms deductions ' +
      'outside it.',
      'TDS is recorded, not calculated',
    ),

    section('One vendor in detail'),
    shot('admin/vendor-detail', 'A vendor record. The same form as the blank one, filled in, with a bar of tabs above it.'),
    prose(
      'The record is the form. There is no read-only view and no separate edit screen — if your role ' +
      'carries the vendor edit permission you can change what you are looking at and select ' +
      '**Save vendor**; if it does not, every box is disabled and the only control is **Back to ' +
      'vendors**. The foot of the form carries the record’s dates: when it was created, and when it ' +
      'was last edited.',
    ),
    warning(
      'Three of the four tabs above the form — **Requests**, **Payments** and **History** — are ' +
      'marked `Soon`. They are announcements of screens that do not exist yet, not links, and ' +
      'selecting them does nothing. Only **Record** is built.\n\n' +
      'Until they are, the vendor-level figures you have are **Paid this year** and **Open requests** ' +
      'on the list, and the [payments ledger](page:accounts/the-payments-ledger) filtered by hand.',
      'The Requests, Payments and History tabs',
    ),

    section('Retiring a vendor'),
    steps([
      { text: 'Open the vendor.' },
      { text: 'Change **Status** to `Inactive`.' },
      { text: 'Select **Save vendor**.' },
    ]),
    prose(
      'The hint under the field is the whole rule: inactive vendors disappear from new requests but ' +
      'stay on old ones. Nothing already raised, approved or paid is touched, and the vendor is still ' +
      'reachable from the list once you set the Status filter to `Inactive` or `All`.',
    ),
    tip(
      'A vendor cannot be deleted. If a record was created twice by mistake, retire the wrong one and ' +
      'note in **Internal notes** which record replaced it — the requests already pointing at it keep ' +
      'pointing at it.',
    ),

    faq([
      {
        q: 'A requester says a vendor is missing from their picker.',
        a: 'The picker offers active vendors only. Check the record’s **Status**, and check the spelling — the picker matches on name, GSTIN and city.',
      },
      {
        q: 'Can a requester add a vendor themselves?',
        a: 'Only if their role carries the vendor create permission. The picker offers **＋ Add a new vendor** to somebody who could actually complete it, and to nobody else.',
      },
      {
        q: 'Two vendors have the same name.',
        a: 'They cannot. The name is unique across the master and the check ignores case, so a duplicate save is refused with `A record with this name already exists.`',
      },
      {
        q: 'Why can I not see the bank block on a vendor?',
        a: 'It is a separate permission from the rest of the record. See [Vendor bank details](page:admin/vendor-bank-details).',
      },
    ]),
  ],
});
