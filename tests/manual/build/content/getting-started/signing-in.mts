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
        text: 'Select **Sign in**.',
        note: 'You land on your home screen, which is tailored to what your account is allowed to do.',
      },
    ]),

    section('When it will not let you in'),
    prose(
      `If the email and password do not match an account, the product says so and lets you try
       again. It deliberately does not tell you *which* of the two was wrong, because that would
       tell anybody typing at your keyboard whether an address has an account behind it.`,
    ),
    shot('shared/login-rejected', 'A refused sign-in. The wording is the same whichever field was wrong.'),
    note(
      `There is no self-service password reset. If you cannot get in, ask an administrator — they can
       set a new password for you from the [People and accounts](page:admin/people) screen.`,
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
        a: 'Ask an administrator to set a new one. They do this from the people screen, and it takes effect immediately.',
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
