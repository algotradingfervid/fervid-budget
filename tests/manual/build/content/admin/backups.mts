import { page, prose, section, shot, steps } from '../../schema.mts';

export default page({
  section: 'admin',
  slug: 'backups',
  title: 'Backups',
  summary: 'Create a server copy and give the operator enough information to test a restore.',
  blocks: [
    section(`Create a backup`),

    shot('admin/backups', 'The backups screen. **Create Backup** sits at the top right, and each copy has its own row: when it was created, its folder, its readiness — files present, not restore-tested — and an operator restore command.'),

    steps([
      { text: `Open Backups and select **Create Backup**.` },
      { text: `Confirm the action.` },
      { text: `Find the new entry and its readable creation time and folder.` },
    ]),

    prose(`Each copy contains the database, uploaded documents and manifest. The screen identifies
       files present; that is not a claim that a restore has been tested. Keep server configuration
       and secrets separately.`),

    section(`Recover through the operator`),

    prose(`The page gives operator contact, the server folder, and a per-backup restore command. There
       is no browser restore action. Hand the operator the exact backup folder and the recovery
       date you intend.`),

    steps([
      { text: `Agree the rollback point. Restoring replaces the current database and attachments with the backup's state.` },
      { text: `Have the operator preserve the current database, WAL/SHM files and attachments for rollback, then stop the application.` },
      { text: `Restore to a staging location and verify the selected copy before replacing production. Use the command and paths shown on the backup screen for the deployed environment.` },
      { text: `Restart and verify representative requests, budgets, payments and attachment downloads before resuming work.` },
    ]),

    prose(`The operator command uses the supported server \`--restore\` path with explicit database and
       attachment destinations. Do not substitute guessed production paths.`),

    section(`Copies and retention`),

    prose(`The copy contains sensitive records regardless of the in-app viewer's permissions. Control
       access to the backup folder. The normal backup destination is on the same server, so an
       off-machine copy remains an operator responsibility.

       Automatic schedules and retention are configured on the server. An entry disappearing after
       its retention window does not mean somebody changed the underlying financial record.`),
  ],
});
