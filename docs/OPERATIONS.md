# Fervid Budget Operations

## Data Locations

The app stores business data in SQLite and payment files on disk.

- Database: `FERVID_DB`, default `data/fervid.db`
- Attachments: `FERVID_ATTACHMENT_DIR`, default `data/attachments`
- Backups: `FERVID_BACKUP_DIR`, default `data/backups`
- Daily backup hour: `FERVID_BACKUP_HOUR`, local hour 0-23, default `2`
- Retention: `FERVID_BACKUP_KEEP_DAYS`, default `30`; `FERVID_BACKUP_KEEP_MONTHS`, default `12`

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

The running server takes one backup a day by itself (`store.DailyBackup`, started in serve mode beside the reminder scheduler). It checks every ten minutes and, once the local clock has passed `FERVID_BACKUP_HOUR`, makes the day's backup unless one stamped after that hour already exists, so restarts and manual backups never cause a second one. Each run is logged (`daily backup created` / `daily backup failed`) and prunes with the rules below. `--backup` still makes a one-off backup and exits.

## Restore

Stop the app before restoring. Then call `store.RestoreBackup` or `store.Restore` with the selected backup folder and configured live paths. The helper stages the database and attachment directory first, then swaps them into place. Any `-wal` and `-shm` files beside the live database belong to the database being replaced and are moved aside with it; otherwise SQLite would replay the old WAL — after an unclean stop, most of the live data — over the restored file.

After restore:

1. Start the app.
2. Run a quick smoke test: login, open the dashboard, and view a payment with an attachment.
3. Record the operator, backup folder, and reason in the deployment notes.

## Retention

`store.PruneBackups` keeps every backup younger than `FERVID_BACKUP_KEEP_DAYS` plus the newest backup of each of the previous `FERVID_BACKUP_KEEP_MONTHS` calendar months, and removes the other `backup-*` folders. A backup's age is the timestamp in its folder name (a folder without one falls back to its modification time). It runs after every manual and daily backup. Periodically test restore into a temporary directory.
