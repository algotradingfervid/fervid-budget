# Fervid Budget: disposable review setup

Use this when an authorized audit needs synthetic workflow fixtures. These commands were exercised against revision `3f702d62e1ca82060ca22e0df06942cb47f91b0d` on 2026-09-25; recheck startup configuration when the project changes. The app is Go-backed. Launch from its repository root; the compiled `server` binary may be older than the source.

Record `git status --short` and revision first. Each state-mutating reviewer/verifier gets a different temporary directory and a different free localhost port. Never point the review at `data/fervid.db`, copy working finance data, run a restore, or reuse a pre-existing database.

```sh
review_runtime=$(mktemp -d /tmp/fervid-ux-XXXXXX)
# Replace 8791 with a verified free port unique to this reviewer.
env FERVID_ADDR=127.0.0.1:8791 \
  FERVID_DB="$review_runtime/fervid.db" \
  FERVID_ATTACHMENT_DIR="$review_runtime/attachments" \
  FERVID_BACKUP_DIR="$review_runtime/backups" \
  FERVID_SESSION_KEY=isolated-ux-review-fixture \
  FERVID_SECURE_COOKIES=false \
  FERVID_ADMIN_EMAIL=admin@fervid.local \
  FERVID_ADMIN_PASSWORD=admin123 \
  FERVID_SMTP_PASSWORD= \
  go run ./cmd/server --seed
```

This creates only sample projects, heads, budgets and a synthetic administrator. Sign in through `/login` with those fixture credentials. Create exact-role Requester, Manager and Accounts fixtures through the UI as needed; role combinations can invalidate a role-specific observation. Keep notification delivery disabled, use reserved `.invalid` emails, and never connect banking services. Payment forms record synthetic payments; no real funds may be moved.

Retain the command session ID and assigned runtime/port in run metadata, outside shareable screenshots. Keep auth states in the temporary runtime. Log fixture changes and whether they remain. A read-only verifier may inspect another reviewer's persisted records in a separate browser, but reproducing mutations requires its own records or instance.

Stop only the processes started for this run after verification and report QA. Remove only the known temporary fixture directories when no longer required; otherwise document retained test state. Compare the final working tree to the baseline. During the audit phase, reports, evidence and the review skill are allowed deliverables. Product files may change during the separately authorized implementation phase; working finance data remains out of scope.
