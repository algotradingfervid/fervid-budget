import { page, prose, section, shot, steps } from '../../schema.mts';

export default page({
  section: 'getting-started',
  slug: 'signing-in',
  title: 'Signing in',
  summary: 'Sign in, recover access, and return to the page you were opening.',
  blocks: [
    section(`Sign in`),

    shot('shared/login', `The sign-in screen: **Email**, **Password** and **Login**, with the **Can't sign in?** link beneath.`),

    steps([
      { text: `Open your organisation's Fervid Budget address.` },
      { text: `Enter your account email and password.` },
      { text: `Select **Login**.` },
    ]),

    prose(`If you came from a protected page, successful sign-in returns you to that permitted
       destination. Otherwise your role-based home screen opens. Invalid credentials use a general
       error without identifying which field was wrong.`),

    shot('shared/login-rejected', 'A refused sign-in. The message reads "Invalid email or password", whichever of the two was wrong.'),

    section(`Can't sign in?`),

    prose(`Select **Can't sign in?** on the login page. If email recovery is configured, request a
       reset using your account email. The response does not reveal whether an account exists. An
       eligible account receives a single-use reset link valid for 30 minutes; resetting the
       password also signs out existing sessions.

       If email recovery is unavailable or the message does not arrive, the help page names your
       administrator/operator contact. Use your organisation's usual support channel when the
       displayed address is local-only. An administrator can set a new password from Users.

       A wrong password does not lock your account. The product limits how fast sign-in attempts
       may arrive from one place; past that limit it answers "Too many attempts. Try again later."
       Wait a minute and try again.`),

    section(`Access depends on roles`),

    prose(`Successful sign-in does not grant access to every screen. Your menu and records depend on
       your roles and their data scopes. Ask an administrator about missing access.`),

    section(`Shared computers`),

    prose(`Use **Log out** when finished. Do not share reset links or leave a signed-in browser open
       for other people.`),
  ],
});
