# Fervid Budget Operations

## Data Locations

The app stores business data in SQLite and payment files on disk. The paths below are relative to the working directory, and so is `web/static` (the CSS and JavaScript served under `/static/`), so start the server from the repository root or from a release directory with the same layout.

- Database: `FERVID_DB`, default `data/fervid.db`
- Attachments: `FERVID_ATTACHMENT_DIR`, default `data/attachments`
- Backups: `FERVID_BACKUP_DIR`, default `data/backups`
- Daily backup hour: `FERVID_BACKUP_HOUR`, local hour 0-23, default `2`
- Retention: `FERVID_BACKUP_KEEP_DAYS`, default `30`; `FERVID_BACKUP_KEEP_MONTHS`, default `12`

## Server Settings

The full list of security-related settings is in `docs/SECURITY-DEPLOYMENT.md`. These three decide how sign-in is rate limited:

- `FERVID_TRUSTED_PROXIES`: comma-separated proxy IPs or narrow CIDRs whose `X-Forwarded-For` header is believed. Production runs behind Caddy on the same host, so it **must** be set to `127.0.0.1`. Without it every visitor appears to come from Caddy's address and shares one login and password-reset allowance, so a few busy minutes get everyone refused with "Too many attempts".
- `FERVID_LOGIN_LIMIT_PER_IP`: POSTs to `/login` and its sub-pages (sign-in, reset request, reset) allowed per minute from one client address, default `30`.
- `FERVID_LOGIN_LIMIT_GLOBAL`: the same POSTs allowed per minute from all clients together, default `300`.

These limits are the only sign-in throttle: a wrong password never locks the account itself, and every failure gets the same "Invalid email or password". Both limits must be whole numbers above zero; anything else (zero, negative, text) falls back to the default, so a typo cannot switch the limit off. Leave them at the defaults in production. The Playwright suites (`playwright.config.ts`, `playwright.audit.config.ts`, which `scripts/run-audit.sh` uses) set both to `100000` because every test signs in from `127.0.0.1`. A local server that the manual capture run (`tests/manual/playwright.manual.config.ts`) points at needs the same two settings if the run hits "Too many attempts".

## Seeding

Start the server once with `--seed` (`make seed` locally) when a new environment needs starter data. `store.Seed` creates the admin account from `FERVID_ADMIN_EMAIL` / `FERVID_ADMIN_PASSWORD` if it does not exist, then creates sample projects, heads, and June 2026 budgets only when projects, heads, and budgets are all empty. The server then keeps serving as usual.

Without `--seed`, a database with no users at all still gets its first admin from the same two settings; nothing else is created.

If any setup data already exists, sample data is skipped and the existing records are left untouched.

## Backup

There are three ways to take a backup: the **Backups** screen (**Create Backup**, needs `backup:create`), the command `server --backup` (`make backup` locally; it makes one backup, prunes, and exits), and the automatic daily backup described below. All use `store.Backup` / `store.CreateBackup` with the configured database, attachment and backup paths. Each run creates a timestamped folder:

```text
data/backups/backup-YYYYMMDD-HHMMSS/
  fervid.db
  attachments/
  manifest.json
```

The database copy is created with SQLite `VACUUM INTO`, which gives a consistent copy while the app is running. Attachments are copied into the same backup folder so the database rows and uploaded files travel together.

The running server takes one backup a day by itself (`store.DailyBackup`, started in serve mode beside the reminder scheduler). It checks every ten minutes and, once the local clock has passed `FERVID_BACKUP_HOUR`, makes the day's backup unless one stamped after that hour already exists, so restarts and manual backups never cause a second one. Each run is logged (`daily backup created` / `daily backup failed`) and prunes with the rules below. `--backup` still makes a one-off backup and exits.

## Restore

Stop the app before restoring. Then run the server binary once with `--restore <backup folder>` and the same `FERVID_*` settings as the service; it calls `store.Restore` with the configured live paths and exits. The helper stages the database and attachment directory first, then swaps them into place. Any `-wal` and `-shm` files beside the live database belong to the database being replaced and are moved aside with it; otherwise SQLite would replay the old WAL — after an unclean stop, most of the live data — over the restored file.

After restore:

1. Start the app.
2. Run a quick smoke test: login, open the dashboard, and view a payment with an attachment.
3. Record the operator, backup folder, and reason in the deployment notes.

## Retention

`store.PruneBackups` keeps every backup younger than `FERVID_BACKUP_KEEP_DAYS` plus the newest backup of each of the previous `FERVID_BACKUP_KEEP_MONTHS` calendar months, and removes the other `backup-*` folders. A backup's age is the timestamp in its folder name (a folder without one falls back to its modification time). It runs after every manual and daily backup. Periodically test restore into a temporary directory.
