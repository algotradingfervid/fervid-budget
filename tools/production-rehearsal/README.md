# Local production-data rehearsal tools

These tools are scoped to the private local directory `/Users/narendhupati/Library/Application Support/Fervid Budget QA/production-rehearsal-2026-09-26`. They do not connect to a server. The root directory must be private (mode 0700); DBs passed to the migration helper must be mode 0600 or stricter. Symlink paths are rejected.

The operator captures and seals `baseline/fervid.db` and `baseline/attachments` separately. Do not run preparation until capture is explicitly complete. Preparation refuses an existing destination and does not modify baseline data:

```sh
python3 tools/production-rehearsal/reconcile.py prepare
go run ./tools/production-rehearsal/migrate --db '/Users/narendhupati/Library/Application Support/Fervid Budget QA/production-rehearsal-2026-09-26/migrated/data/fervid.db'
python3 tools/production-rehearsal/reconcile.py check
```

Run each command only if its predecessor succeeds. The Go helper imports the current `internal/store` directly. It starts no HTTP server, seed operation, mailer or scheduler. It checks full integrity and foreign keys, closes/reopens the database, and compares a logical digest of every schema object and every row across the repeat open. Only the exact migrated and synthetic paths are accepted. Expected resulting schema version is deliberately fixed at 20; update the reviewed procedure if the release's migrations change.

The fidelity copy retains the **original production attachment paths in database metadata** so that every original column can be compared exactly. Its copied attachment files reside in `migrated/data/attachments`. Never serve the fidelity copy directly. Service/test lanes must clone it and rebase their attachment metadata before startup, with independent credentials, session keys, paths, mail isolation and network controls. See `prepare_local.py` for lane preparation.

Reconciliation checks all original columns and primary keys in all nine baseline tables, grouped payment/budget counts and sums, password/account state, expected role mapping, empty newly introduced workflow/history tables, ID maxima/absence of AUTOINCREMENT sequences, and all referenced attachment sizes/content hashes. The output contains only counts and boolean outcomes. It never prints row values, monetary totals, comparison digests, hashes, filenames or identities. Existing INTEGER PRIMARY KEY IDs are preserved; future automatic IDs use SQLite's maximum-existing-ID successor behavior. Request sequence state starts empty because baseline has no requests.

`--synthetic` selects `synthetic-baseline` and `synthetic/data`, exclusively for synthetic tooling validation. It does not create the fixture itself. Preparation still refuses an existing target.

For the QA lane preparer, `go run ./tools/production-rehearsal/migrate --hash-password` accepts 12–72 exact password bytes on stdin and outputs one bcrypt hash. It does not open a database. Pass synthetic credentials by captured subprocess input/output; never print or save them in public reports. A trailing newline is part of the input, not automatically removed.

All real data, captured test logs and screenshots belong inside the private root. The repository's `output/playwright` may be served by an existing local HTTP service and must contain only redacted summaries.

`python3 tools/production-rehearsal/backup_roundtrip.py` tests the already-built private `release-build/server` using its supported `--backup` and `--restore` options. It creates one additional local copy in a new `backup-restore` directory, rebases that copy's attachment metadata, and runs with a whitelisted environment and explicit local paths. It refuses an existing work directory. Raw CLI logs and structured evidence remain private. It compares every migrated table/schema object after path normalization, financial groups, integrity, foreign keys and attachment bytes, and proves both sealed baseline and fidelity migration inputs remain unchanged. It starts no HTTP service or scheduler.

`go run ./tools/production-rehearsal/startupcheck` constructs the real application on the fixed additional path `startup-check/data/fervid.db`, which must first be prepared as an independent copy. It does not listen or start schedulers. It compares every users-table column and user-role assignment before and after `app.New`, with bootstrap configuration deliberately differing from the restored accounts. It emits only account counts and boolean results. The surrounding rehearsal also compared all 26 tables, sealed-baseline hashes and the fidelity input file before/after; those results are in private `startup-bootstrap-evidence.json`.
