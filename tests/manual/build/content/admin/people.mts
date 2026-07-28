import { page, prose, section, shot, steps, note, warning, tip, table, fields, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'people',
  title: 'People and accounts',
  summary: 'Adding colleagues, setting who approves for whom, and deactivating leavers.',
  blocks: [
    prose(
      `**Users** is the roll of everybody with an account. It is where a new colleague gets one, where
       somebody's roles change, where you set who approves their requests, and where a leaver is
       switched off. Nothing on this screen deletes a person: accounts are deactivated, never removed,
       so the requests and payments they touched keep their name on them.`,
    ),

    section('Reading the list'),
    shot('admin/users', 'The people screen. One row per account, sorted by name.'),
    table(
      ['Column', 'What it holds'],
      [
        ['Name', 'The person, as the product will show them everywhere else.'],
        ['Email', 'What they sign in with. Stored lower-case, and it must contain an `@`.'],
        ['Roles', 'Every role they hold, as a pill each. A person can hold several at once.'],
        ['Default approver', 'Who their own requests go to. An em dash means none is set.'],
        ['Status', '`Active` or `Inactive`. Only active people can be chosen as an approver.'],
        ['Edit', 'Opens the panel for that person.'],
      ],
    ),
    prose(
      `The sub-line under the heading — "A person can hold several roles at once" — is the thing to
       hold on to. Roles add up. Somebody who holds both Requester and Manager can raise requests and
       decide other people's, and their menu is the union of the two.`,
    ),

    section('Adding somebody'),
    shot('admin/user-new-sheet', 'The Add user panel. It asks for the minimum, and the rest is set afterwards.'),
    steps([
      { text: 'Select **＋ Add user** at the top right.' },
      { text: 'Type their **Email**. This is what they will sign in with.' },
      { text: 'Type their **Name**.' },
      {
        text: 'Choose a **Role**.',
        note: 'This field offers two values only — `Data entry` and `Admin`. It is the account-level flag, not the role list; the fuller set of roles is assigned afterwards from the edit panel.',
      },
      {
        text: 'Type a starting **Password**.',
        note: 'At least 12 characters, and it must contain a letter and a number. Anything shorter is refused.',
      },
      { text: 'Leave **Active** ticked unless you are creating an account that should not be usable yet.' },
      {
        text: 'Select **Add User**.',
        note: 'The new account appears in the list, and the creation is written to the audit trail.',
      },
    ]),
    note(
      'The **Role** choice on this panel also seeds the person’s first role: choosing `Admin` gives the new account the Admin role, and choosing `Data entry` gives it Accounts. If neither is what you want, open the new row’s **Edit** panel straight away and set the roles properly.',
      'What the Role field actually does',
    ),
    warning(
      `There is no self-service password reset anywhere in the product, so the password you type here
       is the one the person will use to sign in for the first time. Give it to them over a channel
       you trust, and ask them to tell you if they cannot get in.`,
      'The starting password',
    ),

    section('Changing somebody'),
    shot('admin/user-edit-sheet', 'The edit panel, opened on one person. The email is shown but not editable here.'),
    fields([
      { name: 'Name', required: true, note: 'How they appear on every request, approval and payment.' },
      {
        name: 'Roles',
        required: false,
        note: 'One tick box per role, each with its description: `Accounts — Process approved requests and record payments.`, `Admin — Full administrative access.`, `Manager — Review and decide on payment requests.`, `Requester — Raise and manage your own payment requests.` Tick as many as apply.',
      },
      {
        name: 'Default approver for their own requests',
        required: false,
        note: 'Who their requests go to by default. The person themselves is never in their own list — the hint under the field says so, and the save refuses it again on the server. `None` is a valid answer for somebody who raises nothing.',
      },
      {
        name: 'Reset password',
        required: false,
        note: 'Leave blank to keep the current one. Anything you type replaces it immediately, with the same 12-character, letter-and-number rule as a new account.',
      },
      { name: 'Active', required: false, note: 'Untick to switch the account off. See below.' },
    ]),
    steps([
      { text: 'Select **Edit** on the person’s row.' },
      { text: 'Change what needs changing.' },
      {
        text: 'Select **Save user**.',
        note: 'The name, the roles and the default approver are saved together in one go, so a save that is refused leaves none of them applied.',
      },
    ]),

    note(
      `The **Edit** control on this screen carries no permission check of its own, so anybody who can
       open the people screen can open the panel. Saving is a separate matter: the save is refused
       unless you hold the create or edit grant for users. Opening the panel is not the same as being
       allowed to save from it. In practice only administrators can view this screen at all.`,
      'Opening the panel and saving from it',
    ),

    section('Deactivating a leaver'),
    steps([
      { text: 'Select **Edit** on their row.' },
      { text: 'Untick **Active**.' },
      { text: 'Select **Save user**.' },
    ]),
    prose(
      'Their status pill changes to `Inactive` and they can no longer sign in. Everything they raised, approved or paid stays exactly where it is, still carrying their name.',
    ),
    warning(
      'Deactivating somebody does not move the requests waiting on them. If they were an approver, reassign anything sitting in their queue first, and change the default approver on every person who pointed at them — an inactive user is refused as an approver with `the chosen approver is not an active user`, but only when that record is next saved.',
      'Check what was pointing at them',
    ),
    warning(
      'The product will not let the last active administrator be switched off, and it will not let you switch off or demote your own administrator account. The refusals read `at least one active administrator is required` and `You cannot deactivate or demote your own administrator account.` Create the replacement administrator before you retire the old one.',
      'The last administrator',
    ),

    tip(
      `Setting a default approver for everybody is worth the half hour it takes. It is what makes a
       request route itself, and the [Configuration](page:admin/configuration) screen can be set so
       that the default is the only choice a requester gets.`,
    ),

    faq([
      {
        q: 'Somebody has forgotten their password.',
        a: `Open their **Edit** panel, type a new one in **Reset password**, and select **Save user**.
            It takes effect immediately. There is no reset email.`,
      },
      {
        q: 'Why can I not pick a person as their own approver?',
        a: 'Self-approval is blocked throughout the product. The select leaves them out of their own list, and the save refuses it a second time with `a user cannot be their own default approver`.',
      },
      {
        q: 'I want to remove an account entirely.',
        a: `You cannot, and that is deliberate. Deactivate it instead — the history has to keep the
            name attached to it.`,
      },
      {
        q: 'Somebody holds two roles and I only meant to give them one.',
        a: `Open **Edit**, untick the role you did not mean to give, and save. The change applies on
            their next request; there is nothing to restart.`,
      },
      {
        q: 'Where do I see what a role actually grants?',
        a: `[Roles and permissions](page:admin/roles-and-permissions). Select the role's tab and read
            the matrix.`,
      },
    ]),
  ],
});
