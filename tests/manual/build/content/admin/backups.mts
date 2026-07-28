import { page, prose, section, shot, steps, note, warning, tip, table, faq } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'backups',
  title: 'Backups',
  summary: 'Taking a copy of the database, and what the copy does and does not include.',
  blocks: [
    prose(
      'The whole product is one SQLite database plus a folder of uploaded documents. A backup is a ' +
      'timestamped copy of both, taken while the product is running, and dropped into a folder on the ' +
      'same server. The screen’s own sub-line says as much: create timestamped backups of the SQLite ' +
      'database and payment attachments.',
    ),

    section('The screen'),
    shot('admin/backups', 'The backups screen. One button, and a list of the copies that exist.'),
    table(
      ['Column', 'What it shows'],
      [
        ['Backup folder', 'The folder name, which is `backup-` followed by the date and time it was taken — `backup-20260630-071251`.'],
        ['Status', 'A green `Available` pill for every backup on disk.'],
      ],
    ),
    prose(
      'Newest first. When there are none at all the table reads "No backups created yet. Use Create ' +
      'Backup above."',
    ),

    section('Taking one'),
    steps([
      { text: 'Select **Create Backup** at the top right.' },
      {
        text: 'Confirm the "Create a fresh backup now?" prompt.',
        note: 'The copy is taken there and then. On a large database this takes a few seconds, and the screen returns when it is finished.',
      },
      {
        text: 'Check that the new folder is at the top of the list with an `Available` pill.',
        note: 'The act is written to [the audit trail](page:admin/the-audit-trail) as a `Backup` entry with your name on it.',
      },
    ]),
    note(
      'A backup is assembled somewhere temporary and only moved into place once every part of it has ' +
      'been written. A run that fails part-way leaves nothing behind, so a folder in the list is a ' +
      'complete backup rather than a half-finished one.',
      'Why a listed backup is a whole backup',
    ),

    section('What is in one'),
    table(
      ['Part', 'What it is'],
      [
        ['`fervid.db`', 'A complete, consistent copy of the database — every request, payment, budget, user, role, vendor and audit row.'],
        ['`attachments`', 'A copy of the uploaded documents tree: the invoices and receipts people attached to requests and payments.'],
        ['A manifest', 'A small file recording when the backup was taken and where it was taken from.'],
      ],
    ),
    warning(
      'The copy of the database contains everything the database contains, including vendor bank ' +
      'details and every attachment anybody uploaded. The permissions that keep bank details away from ' +
      'most people inside the product do not travel with the file. Anybody who can read the backup ' +
      'folder on the server has all of it.',
      'A backup is not permission-aware',
    ),
    warning(
      'The backup lands on the same server as the thing it is backing up. That protects you from a ' +
      'mistake — a bad import, a month deleted, a table emptied — and not from losing the machine. ' +
      'Copying the folder somewhere else, on a schedule, is a job for whoever runs the server, and it ' +
      'is not something this screen does.',
      'Same server, so not disaster recovery',
    ),

    section('What is not in one'),
    prose(
      'Two things live outside the database and therefore outside the backup. The SMTP password is ' +
      'read from an environment variable on the server and never stored, so it is not in the copy — ' +
      'see [Notification rules and email](page:admin/notification-rules). The product’s own ' +
      'configuration file and environment, including where the database and attachment folders are, ' +
      'belong to the server rather than to the product.',
    ),

    section('Housekeeping'),
    prose(
      'Old backups are pruned automatically each time a new one is taken. Anything older than the ' +
      'retention window is removed from the folder. The window is set on the server rather than on ' +
      'this screen, and it defaults to 30 days.',
    ),
    note(
      'Pruning never stops a backup being taken. If the tidy-up fails for any reason, the new backup ' +
      'is still made and the failure is logged on the server.',
    ),
    tip(
      'Take a backup before anything you would not want to undo by hand: a role rework, a bulk change ' +
      'to heads, unlocking and re-editing a closed month. It costs a few seconds and it changes what ' +
      '"I have made a mistake" means.',
    ),

    section('Restoring'),
    prose(
      'There is no restore control on this screen, and no download control either. Restoring a backup ' +
      'means putting the copied files back where the running product expects them, which is work on ' +
      'the server rather than in the browser. If you need it done, hand whoever runs the server the ' +
      'folder name from this list — that is what identifies the copy you want.',
    ),
    warning(
      'Restoring replaces the current state with the state at the moment the backup was taken. ' +
      'Everything done since — requests raised, approvals given, payments recorded — is gone. Take a ' +
      'fresh backup of the current database before restoring an older one, so the decision is ' +
      'reversible.',
      'A restore is a rollback',
    ),

    faq([
      {
        q: 'How often should I take one?',
        a: 'That depends on how much work you are willing to redo, which is your organisation’s call. What the product gives you is a backup on demand; a schedule is something the server can be set up to run.',
      },
      {
        q: 'Can I download a backup from this screen?',
        a: 'No. The list shows what exists on the server, and nothing more. Getting a copy off the machine is a server job.',
      },
      {
        q: 'A backup I took last month has gone.',
        a: 'It will have been pruned. Backups older than the retention window are removed when a new one is taken, and the default window is 30 days.',
      },
      {
        q: 'Does taking a backup interrupt anybody?',
        a: 'No. The copy is taken while the product is running and nothing is locked out while it happens.',
      },
      {
        q: 'Who can take one?',
        a: 'Viewing this screen needs the backup view grant and **Create Backup** needs the create grant. Both are in the **Administration** row of [Roles and permissions](page:admin/roles-and-permissions).',
      },
    ]),
  ],
});
