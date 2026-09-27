import { page, prose, section, shot, steps, note, warning, faq } from '../../schema.mts';

export default page({
  section: 'getting-started',
  slug: 'signing-in',
  title: 'Signing in',
  summary: 'How to get into Fervid Budget, and what to do when it will not let you.',
  blocks: [
    prose(
      `Fervid Budget runs in a web browser. Your organisation will have given you an address to
       open and an account to sign in with — there is nothing to install.`,
    ),

    section('Signing in'),
    shot('shared/login', 'The sign-in screen. It asks for two things and nothing else.'),
    steps([
      { text: 'Open the address your organisation gave you.' },
      { text: 'Type the email address your account was created with.' },
      { text: 'Type your password.' },
      {
        text: 'Select **Login**.',
        note: 'You land on your home screen, which is tailored to what your account is allowed to do.',
      },
    ]),
    prose(
      `If you opened a link to a particular request or payment — from an email, say — before you were
       signed in, the product takes you straight to that record once you sign in, provided your account
       may open it. If it may not, you land on Home with a note that the page you asked for is not
       available to your account. The note does not name the page.`,
    ),

    section('When it will not let you in'),
    prose(
      `If the email and password do not match an account, the product says so and lets you try
       again. It deliberately does not tell you *which* of the two was wrong, because that would
       tell anybody typing at your keyboard whether an address has an account behind it.`,
    ),
    shot('shared/login-rejected', 'A refused sign-in. The wording is the same whichever field was wrong.'),
    prose(
      `A wrong password does not lock your account. What the product does limit is how fast sign-in
       attempts may arrive: too many from the same place in one minute, and it answers *Too many
       attempts. Try again later.* Wait a minute and try again.`,
    ),
    note(
      `Below the form is a **Can't sign in?** link. When your organisation has set up email, that page
       sends you a one-time link to set a new password; the link expires after 30 minutes. Otherwise it
       tells you to ask an administrator, who can set a new password for you from the
       [People and accounts](page:admin/people) screen.`,
    ),

    section('What happens next'),
    prose(
      `What you see after signing in depends entirely on the roles your account holds. Two colleagues
       signing in side by side can get quite different screens: one may see a queue of approvals, the
       other only their own requests. [Finding your way around](page:getting-started/finding-your-way)
       explains how to read your own menu.`,
    ),
    warning(
      `Sign out when you are finished on a machine other people use. The **Log out** control sits at
       the bottom of the sidebar, under your name.`,
      'Shared computers',
    ),

    faq([
      {
        q: 'I have forgotten my password.',
        a: 'Use **Can\'t sign in?** under the form. If email reset is not available there, ask an administrator to set a new password; they do this from the people screen, and it takes effect immediately.',
      },
      {
        q: 'My colleague can see a screen that I cannot.',
        a: `That is roles, not a fault. Each screen is tied to a permission, and your menu only lists
            what your roles carry. See [the permission matrix](page:reference/permissions-matrix) for
            exactly which role reaches what.`,
      },
    ]),
  ],
});
