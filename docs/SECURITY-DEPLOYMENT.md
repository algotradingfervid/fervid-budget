# Security deployment requirements

The security remediation targets Go **1.27.1**. The Go tests and dependency scan run before `deploy.sh` transfers a release. The script requires a clean checkout, an explicitly selected non-root SSH destination, a verified host key in the operator's `known_hosts`, and HTTPS for the health check. It saves build information, the source revision and SHA-256 hashes, takes a backup, and retains the previous binary and assets. Do not substitute an unreviewed local build.

## Configuration

- `FERVID_ADMIN_PASSWORD` has **no default**. A new database requires a unique password of 12–72 bytes, including a letter and a digit. Existing databases retain their accounts; rotate any previously deployed default credentials through the administrator/password-reset workflow.
- `FERVID_ADDR` defaults to `127.0.0.1:8080`.
- `FERVID_SECURE_COOKIES` defaults to `true`. Serve production behind HTTPS. Set it to `false` only for an explicitly local HTTP development instance.
- Set a persistent, randomly generated `FERVID_SESSION_KEY` in the protected service environment.
- `FERVID_TRUSTED_PROXIES` is an optional comma-separated list of **exact proxy IPs or narrow CIDRs**. Without this, forwarded client IPs are ignored and login/reset throttles use the connection peer. With a reverse proxy, configure its actual address and ensure it overwrites untrusted forwarding headers. Never trust all networks. Production behind Caddy on the same host must set it to `127.0.0.1`.
- `FERVID_LOGIN_LIMIT_PER_IP` (default `30`) and `FERVID_LOGIN_LIMIT_GLOBAL` (default `300`) are the per-minute caps on POSTs to `/login` and its sub-pages, per client address and overall. Only whole numbers above zero are accepted; anything else keeps the default. Keep the defaults in production; the Playwright test servers raise them.
- `FERVID_SMTP_ALLOWED_HOSTS` is a comma-separated operator allowlist of exact relay hostnames. An empty list disables SMTP delivery. Configure it before using email or password reset. SMTP requires a publicly routable relay, verified TLS, and port 587 (STARTTLS) or 465 (implicit TLS). The SMTP password remains environment-only. SMTP operations have a 10-second deadline and honor cancellation. Private/internal relays need a separately reviewed transport policy; they are deliberately refused by this default.
- `FERVID_ATTACHMENT_QUOTA_MB` defaults to **1024 MiB** across the attachment directory, including files staged by concurrent requests. The app logs a warning above 80% usage and refuses writes above the quota. Monitor disk use for backups and logs as well; this is not a whole-filesystem quota.

Storage directories for attachments/backups use mode 0700; new database, sidecar and backup files use 0600. Run the service as its dedicated account, with `UMask=0077`, and keep database/backup paths off public file servers. Existing backup files should remain inside the private backup directory. Never give application users filesystem write access.

## Deployment account setup

Install `scripts/ops/fervid-budget-backup` as root-owned mode 0755 at `/usr/local/sbin/fervid-budget-backup`. Protect `/etc/fervid-budget.env` as root-owned mode 0600. The helper accepts no arguments, reads that trusted environment, changes to `/opt/fervid-budget`, and runs the existing binary's `-backup` as the `fervid` service account.

Give the deployment account write access to the application release directory, but not the service environment or systemd unit. Limit sudo to the exact backup helper and start/stop commands for `fervid-budget.service`; do not grant unrestricted shell or sudo access. Verify the SSH host fingerprint out of band. Set `FERVID_DEPLOY_HOST` and `FERVID_PUBLIC_URL` locally; there is no checked-in production destination. Host firewall rules, root SSH policy, operator sudoers and repository visibility must be verified on the actual host/account before release. No remediation test changes them remotely.

Rollbacks restore binary/assets only. Review the saved backup before any database rollback; never discard newer financial records by automatically restoring an older database.

## Intentional behavior changes

- Login/reset submissions are limited by source and globally. Failed guesses cannot globally lock a victim out of a correct-password login from a different source. Failures use the same message and password-hash verification path for unknown/inactive accounts.
- Logout increments the user's session version, invalidating **all existing sessions** for that account.
- Non-administrators can delegate only permissions and scopes they already hold. They cannot manage higher-privilege users or the system Admin role. The administrator's user/role management grants cannot be removed, and self-demotion/deactivation through role IDs is refused.
- Approval, return and rejection forms must match the reviewed request revision. A stale form must be reloaded and reviewed. A settlement date cannot precede approval; recoveries cannot be entered in a locked month.
- Each financial entry is capped at **100 crore rupees** (`100,000,000,000` paise). Aggregate storage is bounded at `900,000,000,000,000` paise per financial table. Startup refuses an existing database outside these limits with an operator-review error, without rewriting financial values. Budget amounts are parsed exactly; malformed digit grouping and excess decimal precision are rejected. Reports are limited to 120 months.
- A head with financial history cannot move to another project. Create a new head instead. Project/head edits have atomic before/after audit records.
- Vendor totals are marked **Restricted** unless the viewer can read the corresponding company-wide payment/request data. Notification delivery and inbox reads enforce current request visibility, including after permission changes.
- Uploads require valid PDF/JPEG/PNG structure. Images are re-encoded to remove appended data. Downloads remain attachments and cannot escape the attachment root through symlinks. This is structural validation, not antivirus scanning or a guarantee that every PDF is harmless.
- Only the seven named release assets are public under `/static/`; arbitrary files and directory listings are refused. The design-system reference page (`_kitchensink.html`) is served only when `FERVID_DEV_PAGES=true`, which test and development servers set and production never does. HTMX is pinned to 2.0.11 with evaluation and script execution disabled. The CSP permits same-origin scripts; no inline script/event handlers are required.

## Verification

Run `make test`, `make vet`, and the targeted race/security tests. `Makefile` scopes checks to `cmd`, `internal`, and `tools`; generated review probes under ignored `output/` are not application packages. Run `go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./cmd/... ./internal/...` with Go 1.27.1.

The detailed local issue-by-issue report is generated under `output/security-remediation/`. Deployment and host hardening are separate from passing local checks.

## Known issues

None open. The last one found while re-verifying the 2026-09-27 security review
(one client could use up the shared login allowance and block everyone) is fixed:
the per-client limit is now checked first, so a client's refused attempts never
count against the shared limit.
