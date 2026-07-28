import { page, prose, section, shot, steps, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'recoverable-categories',
  title: 'Recoverable categories',
  summary: 'The list requesters choose from when money is expected back.',
  blocks: [
    prose(
      'Some money that goes out is expected to come back: an advance to an employee, a deposit with a ' +
      'landlord, an earnest-money deposit against a tender. The product calls that a recoverable, and ' +
      'a requester marking a request as one has to say which kind it is. The list they choose from is ' +
      'this one.\n\n' +
      'Getting the list right matters more than it looks. The category is what the recoverables ' +
      'register groups by, and it also decides which extra fields the requester is asked to fill in.',
    ),

    section('Where the control is'),
    prose(
      'There is no separate screen for recoverable categories. They are a block at the very foot of ' +
      '[Configuration](page:admin/configuration), below the **Save configuration** bar, headed ' +
      '**Recoverable categories**.\n\n' +
      'It sits outside the settings form on purpose. The rest of the Configuration screen is one form ' +
      'with one save; each category here needs its own submission, so adding one does not mean ' +
      're-posting every other setting on the page.',
    ),
    shot(
      'admin/configuration',
      'The Configuration screen. The Recoverable categories block is the last one, under the Save configuration bar at the foot.',
    ),

    section('Reading the table'),
    table(
      ['Column', 'What it shows'],
      [
        ['Category', 'The name the requester sees.'],
        ['Requires', 'What the category asks for on top of the standard recoverable fields, in words.'],
        ['Active', 'A tick box. Unticking it retires the category — see below.'],
        ['In use', 'How many requests already point at this category.'],
        ['Actions', 'A **Delete** button, shown only to a role holding the delete grant.'],
      ],
    ),
    prose('The seeded list is six categories:'),
    table(
      ['Category', 'Requires'],
      [
        ['Employee advance', '`Nothing extra`'],
        ['EMD', '`Related project`'],
        ['PBG', '`Related project`'],
        ['ICD', '`Counterparty company`'],
        ['Security deposit', '`Counterparty company`'],
        ['Other', '`Nothing extra`'],
      ],
    ),
    note(
      'Every recoverable always captures the reason, an expected return date and the refund terms, ' +
      'whatever its category. The **Requires** setting only adds what a particular category needs on ' +
      'top of that. The hint under the block says so, and adds one thing worth knowing: an employee ' +
      'advance fills the payee in from the requester automatically, and that is a rule about the ' +
      'request type rather than a category setting — it does not fill in the counterparty.',
      'What every recoverable captures anyway',
    ),

    section('Adding a category'),
    steps([
      { text: 'Open **Configuration** and scroll to **Recoverable categories**.' },
      {
        text: 'Type a **New category** name.',
        note: 'The placeholder shows the sort of thing expected — "e.g. Retention deposit". The name must contain at least one letter or digit.',
      },
      {
        text: 'Choose what it **Must also capture**.',
        note: 'One of `Nothing extra`, `Related project`, `Counterparty company` or `Related project and counterparty company`.',
      },
      {
        text: 'Select **Add category**.',
        note: 'It appears in the table above, active, and is written to the audit trail. Requesters can pick it immediately.',
      },
    ]),
    tip(
      'Pick **Must also capture** by asking what you would need to chase the money later. A deposit ' +
      'with another company is useless without knowing which company; a bid deposit is useless without ' +
      'knowing which project it was against.',
    ),

    section('Changing a category'),
    prose(
      'Renaming is safe. A category keeps its underlying identity when the name changes, so every ' +
      'request already filed under it follows the new name rather than being orphaned.',
    ),
    warning(
      'Changing what a category must capture changes the form for future requests only. It cannot go ' +
      'back and collect a counterparty for the requests raised before you changed it — those records ' +
      'keep whatever they were asked for at the time.',
      'A rule change is not retrospective',
    ),

    section('Retiring a category'),
    steps([
      { text: 'Untick **Active** on the row.' },
      { text: 'The change saves as soon as you untick it.' },
    ]),
    prose(
      'The category stops being offered on new requests. Every request already using it keeps it, and ' +
      'the recoverables register still groups by it. This is the ordinary way to take a category out ' +
      'of use.',
    ),

    section('Deleting a category'),
    prose(
      'Deleting is the other option, and it exists only for a category nothing points at. Select ' +
      '**Delete** on the row. There is no confirmation dialog — the guard is on the server rather ' +
      'than in the browser.',
    ),
    warning(
      'A category that is in use cannot be deleted. The refusal names the number and the remedy: ' +
      '"*Name* is used by N requests — deactivate it instead, so those requests keep their category." ' +
      'You land back on Configuration with the sentence at the top and the **In use** count on the row ' +
      'beside it.',
      'In use means it stays',
    ),
    note(
      'The **In use** column is a forecast of that answer, not the gate. The count is re-checked ' +
      'inside the delete itself, so a category that gained a request between your reading the number ' +
      'and pressing the button is still refused.',
    ),

    section('Who can change this block'),
    prose(
      'The controls here follow the **Recoverables** row of ' +
      '[Roles and permissions](page:admin/roles-and-permissions), not the Administration row. The ' +
      '**Edit** column governs the Active toggle and the **Add category** form; the **Cancel** column ' +
      'governs the **Delete** button. A role without the edit grant sees the categories as plain `On` ' +
      'and `Off` pills with no form beneath them.',
    ),

    faq([
      {
        q: 'I cannot find a Recoverable categories screen in the menu.',
        a: 'There is not one. The block is at the foot of [Configuration](page:admin/configuration), below the **Save configuration** bar.',
      },
      {
        q: 'I ticked Active and there was no Save button.',
        a: 'The toggle saves itself. That is why it is its own little form rather than part of the main configuration save.',
      },
      {
        q: 'Delete refused and told me a number.',
        a: 'That number is how many requests point at the category. Untick **Active** instead — the category stops being offered and those requests keep it.',
      },
      {
        q: 'Can a requester add a category while raising a request?',
        a: 'No. The list is fixed by whoever administers it, which is the point of having one.',
      },
      {
        q: 'Where do I see the money that is actually outstanding?',
        a: 'The [recoverables register](page:accounts/the-recoverables-register), which groups by these categories.',
      },
    ]),
  ],
});
