import { page, prose, section, shot, steps } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'backups',
  title: 'Backups',
  summary: 'Create a server copy and give the operator enough information to test a restore.',
  blocks: [
    section(`Create a backup`),

    shot('admin/backups', 'The backups screen before any copy has been made. **Create Backup** sits at the top right; below it are the server folder the copies are stored in, the operator to contact, the table of copies (empty here) and the restore steps.'),

    steps([
      { text: `Open Backups and select **Create Backup**.`, note: `There is no confirmation step: the copy is made as soon as you select the button, and the page reloads when it is finished. The button appears only if your role can create backups.` },
      { text: `Find the new copy at the top of the table — copies are listed newest first — with its **Created** time and its **Backup folder** name.` },
      { text: `Check the **Readiness** column. \`Files present · not restore-tested\` means the database copy, the attachments folder and the manifest are all there; \`Needs inspection\` means one of them is missing or unreadable, and the operator should look at that folder before relying on it.` },
    ]),

    prose(`Each copy contains the database, uploaded documents and a manifest. "Files present" means only
       that those files are there; it is not a claim that a restore has been tested. Server configuration
       and secrets are not in the copy — keep them separately. Every backup made from this button is
       written to the audit log.`),

    section(`Recover through the operator`),

    prose(`The page gives the operator's name and email, the server folder, and for each copy an
       **Operator restore command** you can open under **Recovery**. There is no browser restore action. Hand the operator the exact backup folder and the recovery
       date you intend.`),

    steps([
      { text: `Agree the rollback point. Restoring replaces the current database and attachments with the backup's state.` },
      { text: `Have the operator preserve the current database, WAL/SHM files and attachments for rollback, then stop the application.` },
      { text: `Restore to a staging location and verify the selected copy before replacing production. Use the command and paths shown on the backup screen for the deployed environment.` },
      { text: `Restart and verify representative requests, budgets, payments and attachment downloads before resuming work.` },
    ]),

    prose(`The operator command uses the supported server \`--restore\` path with explicit database and
       attachment destinations, and is run from the Fervid Budget project directory with the application
       stopped. For an installed binary, the operator replaces \`go run ./cmd/server\` with that binary.
       Do not substitute guessed production paths.`),

    section(`Copies and retention`),

    prose(`The copy contains sensitive records regardless of the in-app viewer's permissions. Control
       access to the backup folder. The normal backup destination is on the same server, so an
       off-machine copy remains an operator responsibility.

       The server also makes one automatic backup a day; the hour and how long copies are kept are
       configured on the server, not on this screen. Old copies are removed when they fall outside
       that retention window — including straight after somebody selects **Create Backup**. An entry
       disappearing this way does not mean somebody changed the underlying financial record.`),
  ],
});
