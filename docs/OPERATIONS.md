# Fervid Budget Operations

## Data Locations

The app stores business data in SQLite and payment files on disk.

- Database: `FERVID_DB`, default `data/fervid.db`
- Attachments: `FERVID_ATTACHMENT_DIR`, default `data/attachments`
- Backups: `FERVID_BACKUP_DIR`, default `data/backups`

## Seeding

Use `store.Seed` after opening the database when a new environment needs starter data. It bootstraps the admin account with `EnsureUser`, then creates sample projects, heads, and June 2026 budgets only when projects, heads, and budgets are all empty.

If any setup data already exists, sample data is skipped and the existing records are left untouched.

## Backup

Use `store.CreateBackup` or `store.Backup` with the configured database path, attachment directory, and backup directory. Each run creates a timestamped folder:

```text
data/backups/backup-YYYYMMDD-HHMMSS/
  fervid.db
  attachments/
  manifest.json
```

The database copy is created with SQLite `VACUUM INTO`, which gives a consistent copy while the app is running. Attachments are copied into the same backup folder so the database rows and uploaded files travel together.

## Restore

Stop the app before restoring. Then call `store.RestoreBackup` or `store.Restore` with the selected backup folder and configured live paths. The helper stages the database and attachment directory first, then swaps them into place.

After restore:

1. Start the app.
2. Run a quick smoke test: login, open the dashboard, and view a payment with an attachment.
3. Record the operator, backup folder, and reason in the deployment notes.

## Retention

`store.PruneBackups` removes timestamped backup folders older than the configured day count. Keep at least 30 daily backups unless local policy says otherwise, and periodically test restore into a temporary directory.
