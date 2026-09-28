import { page, prose, section, shot, steps, note, warning, tip, table, fields, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'people',
  title: 'People and accounts',
  summary: 'Adding colleagues, setting who approves for whom, and deactivating leavers.',
  blocks: [
    prose(`**Users** is the roll of everybody with an account. It is where a new colleague gets one,
       where somebody's roles change, where you set who approves their requests, and where a leaver
       is switched off. Nothing on this screen deletes a person: accounts are deactivated, never
       removed, so the requests and payments they touched keep their name on them.`),

    section(`Reading the list`),

    shot('admin/users', 'The people screen. One row per account, sorted by name.'),

    table(
      [`Column`, `What it holds`],
      [
        [`Name`, `The person, as the product will show them everywhere else.`],
        [`Email`, `What they sign in with. Stored lower-case, and it must contain an \`@\`.`],
        [`Roles`, `Every role they hold, as a pill each. A person can hold several at once.`],
        [`Default approver`, `Who their own requests go to. An em dash means none is set.`],
        [`Status`, `\`Active\` or \`Inactive\`. Only active people whose roles let them approve can be chosen as an approver.`],
        [`Edit`, `An **Edit** button that opens the panel for that person. It appears only if your role may edit users.`],
      ],
    ),

    prose(`The sub-line under the heading — "A person can hold several roles at once" — is the thing to
       hold on to. Roles add up. Somebody who holds both Requester and Manager can raise requests
       and decide other people's, and their menu is the union of the two.`),

    section(`Adding somebody`),

    steps([
      { text: `Select **＋ Add user** at the top right.`, note: `The button appears only if your role may create users.` },
      { text: `Type their **Email**. This is what they will sign in with.` },
      { text: `Type their **Name**.` },
      { text: `Tick one or more **Roles**.`, note: `One tick box per role, each followed by its description. At least one is required; a new account with no role ticked is refused with \`Choose at least one role for the new user.\`` },
      { text: `Type a starting **Password**.`, note: `At least 12 characters (and no more than 72 bytes, which is fewer characters if it contains symbols or non-English letters), with at least one letter and one number. Anything else is refused with a reason such as \`password must be at least 12 characters\` or \`password must include a letter and a number\`.` },
      { text: `Leave **Active** ticked unless you are creating an account that should not be usable yet.` },
      { text: `Select **Add User**.`, note: `The new account appears in the list, and the creation is written to the audit trail.` },
      { text: `If the new person will raise requests, select **Edit** on their row and choose their **Default approver for their own requests**, then select **Save user**.`, note: `The Add user panel has no default approver field, so a new account always starts with none (an em dash in the list). The edit panel is the only place to set it.` },
    ]),

    shot('admin/user-new-sheet', 'The Add user panel: Email, Name, a tick box for each role, Password and Active.'),

    note(`If the save is refused, the panel opens again with the reason at the top and the email,
       name and roles you entered still filled in. The password is never kept, so type it again.
       An email address that already belongs to an account is refused with \`A user with this
       email address already exists. Use a different email or edit the existing user.\`, followed
       by \`The password is never kept, so enter it again.\``, `When adding is refused`),

    warning(`Give the starting password over a trusted channel. **Can’t sign in?** offers email reset
       when configured, or an administrator/operator route when it is not. Administrators can set a
       new password here; see [Signing in](page:getting-started/signing-in).`, `The starting password`),

    section(`Changing somebody`),

    fields([
      { name: `Name`, required: true, note: `How they appear on every request, approval and payment.` },
      { name: `Roles`, required: false, note: `One tick box per role, each with its description: \`Accounts — Process approved requests and record payments.\`, \`Admin — Full administrative access.\`, \`Manager — Review and decide on payment requests.\`, \`Requester — Raise and manage your own payment requests.\` Tick as many as apply.` },
      { name: `Default approver for their own requests`, required: false, note: `Who their requests go to by default. Only active people allowed to approve are offered, and never the person themselves. The hint under the field reads *Only active users with permission to approve requests are offered. Self-approval is not allowed.*, and the save refuses self-approval again on the server. If the approver saved earlier can no longer approve (deactivated, or their role changed), the panel says \`The saved default approver can no longer approve requests. Choose an eligible approver before saving.\` \`None\` is a valid answer for somebody who raises nothing.` },
      { name: `Reset password`, required: false, note: `Leave blank to keep the current one. Anything you type replaces it immediately, with the same rules as a new account, and signs the person out of any session they have open.` },
      { name: `Active`, required: false, note: `Untick to switch the account off. See below.` },
    ]),

    shot('admin/user-edit-sheet', 'The edit panel for one person: their name, a tick box for each role, their default approver, a password reset and Active.'),

    steps([
      { text: `Select **Edit** on the person’s row.` },
      { text: `Change what needs changing.` },
      { text: `Select **Save user**.`, note: `The name, the roles and the default approver are saved together in one go, so a save that is refused leaves none of them applied. A refused save shows the reason on an error page; go back to the list and try again.` },
    ]),

    note(`What you see depends on your role. Opening the screen needs the users view grant; the
       **＋ Add user** button appears only with the create grant, and the **Edit** buttons only with
       the edit grant. The server checks again when you save, so a save without the matching grant
       is refused with \`You do not have permission to perform this action.\` In the seeded roles
       only administrators can reach this screen at all.`, `Who sees which controls`),

    section(`Deactivating a leaver`),

    steps([
      { text: `Select **Edit** on their row.` },
      { text: `Untick **Active**.` },
      { text: `Select **Save user**.` },
      { text: `If requests are still waiting for this person's decision, a confirmation page opens instead of saving.`, note: `It is headed *Deactivate Name?*, says \`Nothing has been saved yet.\`, and lists the waiting requests with the warning that they "stay stuck until someone reassigns them". Tick \`I will reassign these requests to another approver\` and select **Deactivate anyway**, or select **Cancel** to go back without saving. If you typed a new password in the panel, the page asks for it again.` },
    ]),

    prose(`Their status pill changes to \`Inactive\`, any session they have open ends, and they can no
       longer sign in. Everything they raised, approved or paid stays exactly where it is, still
       carrying their name.`),

    warning(`Deactivating somebody does not move the requests waiting on them. Open each request the
       confirmation page listed and use **Reassign approval**. Then change the default approver on
       every person who pointed at them: their edit panel flags the approver that can no longer
       approve, and saving that person is refused with \`choose an active default approver with
       permission to approve requests\` until you pick someone else. Where your organisation does
       not let requesters choose their approver, those people cannot submit a request until this is
       corrected.`, `Check what was pointing at them`),

    warning(`The product will not let the last active administrator be switched off, and it will not let
       you switch off or demote your own administrator account. The refusals read, exactly as the
       screen writes them, \`at least one active administrator is required\` and \`you cannot
       deactivate or demote your own administrator account\`. Create the replacement administrator
       before you retire the old one.`, `The last administrator`),

    tip(`Setting a default approver for everybody is worth the half hour it takes. It is what makes a
       request route itself, and the [Configuration](page:admin/configuration) screen can be set so
       that the default is the only choice a requester gets.`),

    faq([
      { q: `Somebody has forgotten their password.`, a: `If email is set up, they can reset it themselves with **Can't sign in?** on the sign-in page. Otherwise open their **Edit** panel, type a new one in **Reset password**, and select **Save user**; it takes effect immediately.` },
      { q: `Why can I not pick a person as their own approver?`, a: `Self-approval is blocked throughout the product. The select leaves them out of their own list, and the save refuses it a second time with \`a user cannot be their own default approver\`.` },
      { q: `I want to remove an account entirely.`, a: `You cannot, and that is deliberate. Deactivate it instead — the history has to keep the name attached to it.` },
      { q: `Somebody holds two roles and I only meant to give them one.`, a: `Open **Edit**, untick the role you did not mean to give, and save. The change applies on their next request; there is nothing to restart.` },
      { q: `Where do I see what a role actually grants?`, a: `[Roles and permissions](page:admin/roles-and-permissions). Select the role's tab and read the matrix.` },
    ]),
  ],
});
