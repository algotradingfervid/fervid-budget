#!/usr/bin/env python3
"""Render aggregate-only rehearsal reports from explicitly named evidence files.

Never renders input text, paths, identities, hashes, monetary values or filenames.
Lane case labels and limitations are fixed here. Unknown lane/check/issue IDs fail
closed. Missing evidence remains pending; a lane's declared status cannot override
missing or failing checks. Only run --final when every browser lane is complete.
"""
import argparse
from datetime import datetime, timezone
import html
import json
import math
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]
PRIVATE = Path.home() / "Library/Application Support/Fervid Budget QA/production-rehearsal-2026-09-26"
OUTPUT = ROOT / "output/playwright/production-db-rehearsal-2026-09-26"
LANES = {
    "historical": ("Historical data", {
        "HIST-01": "Baseline inventory", "HIST-02": "Admin dashboard",
        "HIST-03": "Monthly ledger reconciliation", "HIST-04": "Active, removed and all-status filters",
        "HIST-05": "Search and reset", "HIST-06": "Empty results",
        "HIST-07": "Historical detail fidelity", "HIST-08": "Attachment download integrity",
        "HIST-09": "Report filter reconciliation", "HIST-10": "CSV export reconciliation",
        "HIST-11": "Accounts visibility", "HIST-12": "Mobile layout",
        "HIST-13": "Read-only preservation", "HIST-14": "Browser cleanup",
    }),
    "lifecycle": ("Request lifecycle", {
        "lifecycle-role-setup": "Synthetic role setup through the UI",
        "lifecycle-invalid-amount": "Invalid request amount rejection",
        "lifecycle-request-attachment": "Request creation and document",
        "lifecycle-return-revision": "Return, revision and resubmission",
        "lifecycle-approval": "Approval and reservation",
        "lifecycle-installments": "Installment payment completion",
        "lifecycle-overpayment": "Overpayment rejection",
        "lifecycle-duplicate-submit": "Duplicate submission protection",
        "lifecycle-ledger-reconciliation": "Ledger, report and history reconciliation",
        "lifecycle-rejection": "Request rejection",
        "lifecycle-role-boundaries": "Unauthorized role access denied",
    }),
    "budgets": ("Budget planning", {
        "BUD-PROD-01": "Historical allocations: UI and database reconciliation",
        "BUD-PROD-02": "Historical reports and CSV reconciliation",
        "BUD-PROD-03": "New plan, project, heads and multiple lines",
        "BUD-PROD-04": "Legacy copy and source isolation",
        "BUD-PROD-05": "Mobile synthetic copy and edit",
        "BUD-PROD-06": "Stale edit against a locked month",
        "BUD-PROD-07": "Unlock and edit recovery",
        "BUD-PROD-08": "Invalid decimal correction",
        "BUD-PROD-09": "Original historical records unchanged",
        "BUD-PROD-10": "Owned browser cleanup",
    }),
    "admin": ("Administration", {
        "A01": "Copied administrator login and session handling",
        "A02": "Vendor required-field, email and duplicate validation",
        "A03": "Vendor creation, editing, mode and persistence",
        "A04": "Vendor inactive and reactivated filters",
        "A05": "Users, roles and permissions", "A06": "Configuration persistence",
        "A07": "Notification settings and mail suppression", "A08": "Audit visibility",
        "A09": "Backup screen", "A10": "Not-found and forbidden error handling",
        "A11": "Mobile layout",
    }),
    "recoverables": ("Recoverables", {
        "REC-PROD-01": "Recoverable request creation",
        "REC-PROD-02": "Independent approval and payment",
        "REC-PROD-03": "Excessive recovery rejection",
        "REC-PROD-04": "Invalid inputs and missing explanation",
        "REC-PROD-05": "Partial recovery",
        "REC-PROD-06": "Full recovery reconciliation",
        "REC-PROD-07": "Export, list, history and audit",
        "REC-PROD-08": "Budget exclusion and original payout preservation",
        "REC-PROD-09": "Owned browser cleanup",
    }),
}
ISSUES = {
    "STARTUP_DEFAULT_ADMIN": "Existing database gains an unintended default administrator on application startup.",
    "NUMBER_WIDTH_VALIDATION": "Configuration accepts request-number widths outside the supported range, so saved settings and generated numbers disagree.",
    "legacy-role-review": "Legacy role mapping needs an access review before deployment.",
    "mobile-overflow": "A mobile layout overflow was observed.",
    "validation-feedback": "Validation feedback needs correction.",
    "authorization": "An authorization boundary did not meet the expected behavior.",
    "data-reconciliation": "A data reconciliation did not match the expected result.",
    "attachment-download": "An attachment download check did not pass.",
    "workflow-blocked": "A planned workflow could not be completed.",
    "browser-error": "A browser error needs investigation.",
    "configuration-restore": "QA configuration restoration needs follow-up.",
    "coverage-incomplete": "One or more planned acceptance checks remain incomplete.",
}
RESOLUTION_EVIDENCE = {
    "STARTUP_DEFAULT_ADMIN": "The final build was started against a fresh QA copy: nine users before startup, nine after HTTP startup and nine after restart, with every user column unchanged. Login with a copied administrator's separate QA credential also passed.",
    "NUMBER_WIDTH_VALIDATION": "On the final build, request-number widths -1, 0, 13, 1.5 and blank produced form errors without partial database saves. Supported boundary values 1 and 12 saved and persisted.",
}
LEGACY_TABLES = ["audit_log", "budget_months", "budgets", "heads", "month_locks",
                 "payment_attachments", "payments", "projects", "users"]
ALL_TABLES = LEGACY_TABLES + ["app_settings", "budget_lines", "notification_settings", "notifications",
    "password_reset_throttle", "password_resets", "payment_requests", "recoverable_categories",
    "recovery_events", "request_attachments", "request_comments", "request_number_seq",
    "role_data_scope", "role_permissions", "roles", "user_roles", "vendors"]
STATUSES = {"passed": "Passed", "failed": "Failed", "pending": "Pending", "not_tested": "Not tested"}
SANDBOX = {
    "loopback_ingress_allowed": "Loopback HTTP ingress allowed",
    "outbound_blocked": "Outbound TCP connection denied",
    "no_outbound_connection": "Local test sink received no outbound connection",
    "public_bind_blocked": "Public-interface binding denied",
    "outside_write_blocked": "Writes outside permitted lane directories denied",
    "release_write_blocked": "Release writes denied",
    "private_read_blocked": "Other private snapshot and credential contents unreadable",
    "data_write_allowed": "Lane data writes allowed", "sqlite_wal_allowed": "SQLite WAL works",
    "synthetic_app_started_under_sandbox": "Application starts under the sandbox",
    "qa_password_login_pass": "Randomized QA password login works",
    "static_pass": "Static assets served under the sandbox",
}


def read(path):
    if path is None:
        return {}
    value = json.loads(Path(path).read_text())
    if not isinstance(value, dict):
        raise ValueError("evidence must be an object")
    return value


def get(data, dotted):
    for key in dotted.split("."):
        if not isinstance(data, dict):
            return None
        data = data.get(key)
    return data


def count(value):
    return str(value) if type(value) is int and 0 <= value < 1000000000 else "Pending"


def seconds(value):
    return f"{value:.3f} s" if type(value) in (int, float) and math.isfinite(value) and 0 <= value < 86400 else "Pending"


def truth(value, expected=True):
    if type(value) is not bool:
        return "pending"
    return "passed" if value is expected else "failed"


def combine(values):
    values = list(values)
    if "failed" in values:
        return "failed"
    return "passed" if values and all(v == "passed" for v in values) else "pending"


def lane_summaries(paths):
    result = {lane: {"checks": {key: "pending" for key in checks}, "issues": [], "declared": "pending"}
              for lane, (_, checks) in LANES.items()}
    seen = set()
    for path in paths:
        raw = read(path); lane = raw.get("lane")
        if raw.get("schema_version") != 1 or lane not in LANES or lane in seen:
            raise ValueError("invalid or duplicate lane summary")
        seen.add(lane)
        if raw.get("status") not in ("pending", "passed", "failed", "partial"):
            raise ValueError("invalid declared lane status")
        result[lane]["declared"] = raw["status"]
        checks = raw.get("checks", [])
        if not isinstance(checks, list):
            raise ValueError("invalid check list")
        ids = set()
        for check in checks:
            key, status = check.get("id"), check.get("status")
            if key not in LANES[lane][1] or key in ids or status not in STATUSES:
                raise ValueError("unknown or duplicate check ID or status")
            ids.add(key); result[lane]["checks"][key] = status
        for issue in raw.get("issues", []):
            if issue.get("code") not in ISSUES or issue.get("status") not in ("open", "resolved"):
                raise ValueError("unknown issue code or status")
            result[lane]["issues"].append((issue["code"], issue["status"]))
    return result


def build(args):
    migration = read(args.migration_evidence); backup = read(args.backup_evidence)
    sandbox = read(args.sandbox_evidence); capture = read(args.capture_evidence)
    startup = read(args.startup_evidence)
    final_safety = read(args.final_safety_evidence)
    safety = read(args.production_safety); lanes = lane_summaries(args.lane_summary)
    for issue in args.issue:
        code, state = issue.split(":", 1)
        if (code, state) not in lanes["admin"]["issues"]:
            lanes["admin"]["issues"].append((code, state))
    reconciliation = get(migration, "reconciliation.result") or {}
    migration_checks = [
        ("Source capture verified", truth(capture.get("verified"))),
        ("Migration integrity", truth(get(migration, "migration.result.full_integrity_pass"))),
        ("Foreign keys", truth(get(migration, "migration.result.foreign_keys_pass"))),
        ("Every original field and primary key reconciled", truth(reconciliation.get("all_checks_pass"))),
        ("Second open leaves schema and rows unchanged", truth(get(migration, "migration.result.second_open_schema_and_all_rows_match"))),
        ("Original passwords and account state preserved in fidelity copy", truth(reconciliation.get("password_hashes_and_account_state_match"))),
        ("All referenced attachment bytes and sizes match", truth(reconciliation.get("referenced_attachment_files_size_and_hash_match"))),
        ("Sealed baseline files unchanged", truth(migration.get("sealed_baseline_all_files_unchanged"))),
        ("Migration starts no HTTP server, seed, mail or scheduler", truth(get(migration, "migration.result.http_seed_mail_scheduler_started"), False)),
    ]
    backup_checks = [(label, truth(backup.get(key), expected)) for key, label, expected in [
        ("all_checks_pass", "All backup and restore comparisons", True),
        ("schema_objects_match", "Schema objects preserved", True),
        ("financial_groups_match", "Grouped financial reconciliation", True),
        ("all_attachment_bytes_match", "All attachment bytes match", True),
        ("restored_attachment_paths_local_and_sizes_match", "Restored attachments resolve only to the local copy", True),
        ("full_integrity_pass", "Restored database integrity", True),
        ("foreign_keys_pass", "Restored database foreign keys", True),
        ("schema_version_20_preserved", "Restored schema version preserved", True),
        ("sealed_baseline_all_files_unchanged", "Sealed baseline remains unchanged", True),
        ("fidelity_migrated_database_file_unchanged", "Fidelity migration input remains unchanged", True),
        ("cli_started_http_or_schedulers", "Backup and restore start no HTTP or schedulers", False),
    ]]
    sandbox_checks = [(label, truth(sandbox.get(key))) for key, label in SANDBOX.items()]
    states = {}
    for lane, value in lanes.items():
        states[lane] = combine(value["checks"].values())
        if value["declared"] == "failed" or any(s == "open" for _, s in value["issues"]):
            states[lane] = "failed"
        elif value["declared"] != "passed" and states[lane] == "passed":
            states[lane] = "pending"
    overall = combine([combine(s for _, s in migration_checks), combine(s for _, s in backup_checks),
                       combine(s for _, s in sandbox_checks), args.go_tests, *states.values()])
    if args.final and any("pending" in v["checks"].values() or v["declared"] == "pending" for v in lanes.values()):
        raise ValueError("final report refused: browser acceptance is pending")
    mode = "Final evidence report" if args.final else "Draft · acceptance in progress"
    timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
    parts = []; md = ["# Local production database rehearsal", "", mode, "", f"Generated: {timestamp}", ""]

    def paragraph(text, cls=""):
        parts.append(f'<p class="{cls}">{html.escape(text)}</p>'); md.extend([text, ""])

    def heading(title, anchor):
        parts.append(f'<h2 id="{anchor}">{html.escape(title)}</h2>'); md.extend([f"## {title}", ""])

    def table(headers, rows):
        parts.append('<div class="table-wrap"><table><thead><tr>' + ''.join(f'<th>{html.escape(h)}</th>' for h in headers) + '</tr></thead><tbody>')
        md.extend(["| " + " | ".join(headers) + " |", "| " + " | ".join("---" for _ in headers) + " |"])
        for row in rows:
            parts.append('<tr>' + ''.join(f'<td>{badge(cell) if cell in STATUSES else html.escape(str(cell))}</td>' for cell in row) + '</tr>')
            md.append("| " + " | ".join(STATUSES.get(c, str(c)).replace("|", "\\|") for c in row) + " |")
        parts.append('</tbody></table></div>'); md.append("")

    def badge(status):
        return f'<span class="badge {status}">{STATUSES[status]}</span>'

    heading("Summary", "summary")
    paragraph("This rehearsal checks the current application against a private local copy of production data. It does not deploy the application or change production DNS, TLS or services.")
    paragraph("Browser acceptance is still in progress. Completed database and infrastructure checks do not imply that every workflow has passed." if not args.final else
              "This report records completed and explicitly untested checks. Read the limitations before deciding whether to deploy.", "callout")
    table(["Area", "Result"], [("Overall recorded evidence", overall),
        ("Migration and fidelity", combine(s for _, s in migration_checks)),
        ("Backup and restore", combine(s for _, s in backup_checks)),
        ("Local sandbox", combine(s for _, s in sandbox_checks)), ("Go test suite", args.go_tests),
        *[(LANES[lane][0], states[lane]) for lane in LANES]])
    if ("STARTUP_DEFAULT_ADMIN", "open") in lanes["admin"]["issues"]:
        paragraph("Startup defect under investigation: migration preserved all nine original users, but serving an existing copy created an additional unintended default administrator. These are different stages. The original nine-account migration result remains valid; startup account preservation requires a fix and fresh-copy verification.", "callout")
    both_fixes_resolved = all((code, "resolved") in lanes["admin"]["issues"] for code in RESOLUTION_EVIDENCE)
    if both_fixes_resolved:
        paragraph("The broad workflow checks ran on the initial rehearsal build. Two scoped fixes were then applied for startup account creation and request-number configuration. Their affected behavior was retested on the captured final build, including a fresh database startup and restart. The complete workflow suite was not repeated against that final binary.", "callout")
    heading("Source, protection and timing", "source")
    paragraph("The baseline, database copies, attachment files, credentials, screenshots and detailed logs are held in a private local directory. This report contains only approved counts, statuses and generic case labels. Each browser lane uses a separate copy, session key and QA credential; original identities and history references are retained.")
    table(["Checkpoint", "Observed result"], [
        ("Source schema version", count(reconciliation.get("baseline_schema_version"))),
        ("Migrated schema version", count(reconciliation.get("migrated_schema_version"))),
        ("Production login HTTP status after capture", count(get(safety, "production_health_after_capture.login_http"))),
        ("Baseline verification", truth(safety.get("snapshot_baseline_verified"))),
        ("Copy preparation", seconds(get(migration, "prepare.elapsed_seconds"))),
        ("Migration plus repeated open", seconds(get(migration, "migration.elapsed_seconds"))),
        ("Original-data reconciliation", seconds(get(migration, "reconciliation.elapsed_seconds"))),
        ("Backup command", seconds(backup.get("backup_elapsed_seconds"))),
        ("Restore command", seconds(backup.get("restore_elapsed_seconds"))),
    ])
    paragraph("These measured timings cover only the named local operations. They are not total test duration, production downtime, or a Linux performance benchmark.")
    paragraph("The final binary and matching static assets are captured in release-final. Its source-manifest.json is the source proof for the final fixes; the earlier tested-source-manifest.json describes the initial rehearsal build. The Go test status above refers to the coordinator-supplied final complete test run.")
    after = safety.get("production_health_after_rehearsal")
    before = safety.get("production_health_after_capture")
    if isinstance(after, dict) and isinstance(before, dict):
        same_process = (type(after.get("pid")) is int and after.get("pid") == before.get("pid")
                        and isinstance(after.get("active_since"), str)
                        and after.get("active_since") == before.get("active_since"))
        table(["Production continuity after rehearsal", "Result"], [
            ("Login HTTP status", count(after.get("login_http"))),
            ("Same service process and start time as capture", truth(same_process)),
        ])
    if final_safety:
        safe_keys = {
            "original_records_preserved": "Original historical records preserved across QA lanes",
            "baseline_db_unchanged": "Sealed baseline database unchanged",
            "baseline_attachment_files_unchanged": "All sealed baseline attachment bytes unchanged",
            "production_staging_removed": "Temporary production snapshot staging removed",
            "owned_browsers_closed": "Agent-owned QA browser sessions closed",
            "qa_apps_stopped": "Agent-owned QA application processes stopped",
        }
        safe_rows = [(label, truth(final_safety.get(key))) for key, label in safe_keys.items()]
        matches = final_safety.get("public_artifact_sensitive_matches")
        safe_rows.append(("Original email, attachment-name and QA-password matches in public artifacts",
                          count(matches)))
        table(["Final preservation and cleanup", "Observed result"], safe_rows)
        paragraph("The public-artifact scan checks the explicitly identified sensitive values. A zero-match result is scoped to that scan and is not a general guarantee against every possible disclosure.")
    heading("Migration and historical fidelity", "migration")
    table(["Check", "Result"], migration_checks)
    rows = []
    for name in LEGACY_TABLES:
        v = get(reconciliation, "tables." + name) or {}
        rows.append((name.replace("_", " ").title(), count(v.get("baseline_count")), count(v.get("migrated_count")),
                     combine([truth(v.get("primary_keys_match")), truth(v.get("all_original_columns_match"))])))
    table(["Legacy table", "Before", "After", "Original fields and IDs"], rows)
    table(["Cross-lane preservation check", "Result"], [("Original historical records preserved except documented QA credentials and login metadata", args.cross_lane_preservation)])
    if args.cross_lane_preservation == "passed":
        paragraph("Read-only comparisons across the QA lanes verified preservation of the original historical records. Documented QA password/session preparation and login metadata were excluded from that comparison. Synthetic QA additions and the initial build's separately documented startup account defect are not historical source records.")
    if startup:
        parts.append('<h3>Fresh startup verification after the account fix</h3>')
        md.extend(["### Fresh startup verification after the account fix", ""])
        table(["Checkpoint", "Observed result"], [
            ("Users before first HTTP startup", count(startup.get("copied_user_count_before"))),
            ("Users after first HTTP startup", count(startup.get("user_count_after_first_http_start"))),
            ("Users after restart", count(startup.get("user_count_after_restart"))),
            ("All user columns unchanged after startup", truth(startup.get("all_user_columns_unchanged_after_first_start"))),
            ("All user columns unchanged after restart", truth(startup.get("all_user_columns_unchanged_after_restart"))),
            ("No additional default administrator", truth(startup.get("default_administrator_not_added"))),
            ("Login page served successfully", truth(startup.get("login_http_200"))),
            ("Static assets served successfully", truth(startup.get("static_http_200"))),
        ])
    heading("Coverage by workflow", "coverage")
    paragraph("The entries below are acceptance case groups. A group can contain multiple scenarios and assertions; the number of groups is not a count of all interactions or test assertions.")
    for lane, (label, catalog) in LANES.items():
        parts.append(f'<h3>{html.escape(label)}</h3>'); md.extend([f"### {label}", ""])
        table(["Case", "Acceptance check", "Result"], [(key, title, lanes[lane]["checks"][key]) for key, title in catalog.items()])
    heading("Backup and recovery", "recovery")
    table(["Check", "Result"], backup_checks)
    restore_rows = []
    for name in ALL_TABLES:
        item = get(backup, "tables." + name) or {}
        restore_rows.append((name.replace("_", " ").title(), count(item.get("row_count")), truth(item.get("all_columns_match_after_path_normalization"))))
    table(["Migrated table", "Restored rows", "All columns after path normalization"], restore_rows)
    heading("Local isolation", "isolation")
    table(["Check", "Result"], sandbox_checks)
    paragraph("The macOS sandbox was checked using synthetic files and a local connection sink; no external SMTP contact was needed. Filesystem metadata needed for SQLite path resolution is allowed, while contents of other private copies remain inaccessible. Each lane must use a separate browser context because cookies on the same loopback hostname are shared across ports.")
    heading("Issues and limits", "limits")
    issue_rows = [(LANES[lane][0], ISSUES[code], "Open" if state == "open" else "Resolved",
                   RESOLUTION_EVIDENCE.get(code, "Resolved according to the accepted lane summary.") if state == "resolved" else "Verification pending.")
                  for lane in LANES for code, state in lanes[lane]["issues"]]
    if issue_rows:
        table(["Area", "Observation", "State", "Verification evidence"], issue_rows)
    else:
        paragraph("No coded issues have been supplied yet. This is not a claim that no defects exist." if not args.final else "No open coded defects were supplied in the accepted workflow summaries.")
    limitations = [
        "The deployed production binary reports a modified source revision. The exact deployed source tree has not been reconstructed; the tested local working tree and static assets are a separate captured release.",
        "SQLite snapshot consistency does not make attachment capture atomic. Source files and references were reconciled, but an attachment created or removed during capture could require a fresh snapshot and repeat verification.",
        "Original passwords are not known or used. Their stored hashes and account state are compared in the fidelity copy; browser lanes replace passwords with private QA credentials and test those logins.",
        "The legacy migration maps six users to Admin and three to Accounts. Review the intended production permissions before deployment; newer requester and manager roles are not inferred from historical users.",
        "The baseline contains no historical vendors or requests. New request, approval and settlement workflows use clearly separate synthetic QA activity; no historical workflow records are fabricated.",
        "One historical payment has an implausibly early year. The record was preserved as captured; this rehearsal did not correct or alter the historical row.",
        "A report filter covering 24,001 months exceeded a five-second command-line interaction threshold, then eventually rendered. Exact application performance was not measured. This is a report-range usability and performance limitation, not a timed benchmark result.",
        "Outbound email is disabled and blocked locally. Actual SMTP delivery, production password-reset delivery and email-provider integration are not tested.",
        "Linux service configuration, shared Caddy routing, public DNS, certificates, HTTPS cookie behavior and production resource limits are not validated by this macOS rehearsal. No server deployment is performed.",
        "A restored database passing integrity checks does not establish every browser workflow. Review the case-by-case acceptance status above and any untested items.",
    ]
    parts.append('<ul>' + ''.join(f'<li>{html.escape(t)}</li>' for t in limitations) + '</ul>')
    md.extend(["- " + t for t in limitations]); md.append("")
    heading("Evidence and reproducibility", "evidence")
    paragraph("Case identifiers above map to private lane evidence. The public database summary contains aggregate results only. Browser screenshots, database records and credentials are intentionally excluded from this report.")
    parts.append('<p><a href="database-migration-and-restore.md">Aggregate database migration and restore evidence</a></p>')
    md.extend(["[Aggregate database migration and restore evidence](database-migration-and-restore.md)", ""])
    evidence_paths = ["migration-evidence.json", "backup-restore-evidence.json", "sandbox-evidence.json",
                      "capture-transfer.json", "production-safety.json", "startup-fix-evidence.json",
                      "release-final/source-manifest.json", "go-tests-final.log", "admin/safe-summary.json",
                      "lanes/admin-fixed/"]
    for lane in LANES:
        evidence_paths.append(f"lanes/{lane}/")
    if args.include_private_paths:
        paragraph("Private evidence locations below are local paths, not HTTP links. They may contain production data and should not be published or shared.")
        table(["Private artifact", "Local location"], [(p, str(PRIVATE / p)) for p in evidence_paths])
    else:
        table(["Evidence group", "Access"], [("Migration, snapshot, restore and source manifest", "Private local files"),
            ("Per-lane screenshots, logs and data", "Private local files"), ("Credentials and session keys", "Private; omitted")])
    paragraph("Re-run this generator with explicit approved lane summaries to update the report. Draft reports remain labeled as drafts until the coordinator requests a final report and pending browser checks have been resolved or explicitly recorded as not tested.")
    OUTPUT.mkdir(parents=True, exist_ok=True)
    css = '''*{box-sizing:border-box}body{margin:0;background:#f3f5f8;color:#17283b;font:16px/1.6 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}header{background:#142d44;color:#fff;padding:48px max(24px,calc((100vw - 1080px)/2));border-bottom:5px solid #36b6a0}header p{color:#cddce8;margin:4px 0}h1{font-size:clamp(28px,4vw,42px);line-height:1.15;letter-spacing:-.03em;margin:10px 0 18px}main{max-width:1128px;margin:auto;padding:24px}h2{font-size:25px;margin:44px 0 14px;scroll-margin-top:20px}h3{font-size:19px;margin:28px 0 12px}.eyebrow{font-size:12px;letter-spacing:.12em;text-transform:uppercase;font-weight:750}nav{display:flex;flex-wrap:wrap;gap:8px 18px;background:white;border:1px solid #dbe2e9;padding:16px 20px;border-radius:12px}a{color:#09665f;text-decoration-thickness:1px;text-underline-offset:3px}.table-wrap{overflow:auto;background:#fff;border:1px solid #dbe2e9;border-radius:12px;margin:18px 0 24px}table{border-collapse:collapse;width:100%;text-align:left;font-size:14px}th{background:#eaf0f5;color:#314b61;font-size:12px;text-transform:uppercase;letter-spacing:.04em}th,td{padding:12px 16px;border-bottom:1px solid #e6ecf1;vertical-align:top}td:first-child{font-weight:600}tr:last-child td{border-bottom:0}td{overflow-wrap:anywhere}.badge{display:inline-block;font-size:12px;font-weight:750;padding:3px 10px;border-radius:20px;white-space:nowrap}.passed{background:#ddf3e9;color:#155a3d}.failed{background:#fde3e2;color:#a52124}.pending{background:#fff0c6;color:#775709}.not_tested{background:#e9edf2;color:#536273}.callout{background:#fff4d6;border-left:4px solid #c99322;padding:16px 20px;border-radius:0 8px 8px 0}li{margin:12px 0}footer{padding:28px 24px;color:#596b7e;text-align:center;font-size:13px}@media(max-width:600px){main{padding:16px}header{padding:32px 20px}th,td{padding:10px;font-size:12px}nav{gap:8px 14px}h2{font-size:22px}}@media print{body{background:white}header{background:white;color:#17283b;padding:10px 0;border-color:#36b6a0}header p{color:#536273}main{padding:0}nav{display:none}.table-wrap{overflow:visible}h2,h3{break-after:avoid}tr{break-inside:avoid}}'''
    navigation = [('summary', 'Summary'), ('source', 'Source'), ('migration', 'Migration'), ('coverage', 'Workflows'),
                  ('recovery', 'Recovery'), ('isolation', 'Isolation'), ('limits', 'Limits'), ('evidence', 'Evidence')]
    document = '<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex,nofollow"><title>Local production database rehearsal</title><style>' + css + '</style></head><body><header><div class="eyebrow">Fervid Budget · Local QA</div><h1>Production database rehearsal</h1><p>' + html.escape(mode) + '</p><p>' + timestamp + '</p></header><main><nav aria-label="Report sections">' + ''.join(f'<a href="#{a}">{b}</a>' for a,b in navigation) + '</nav>' + ''.join(parts) + '</main><footer>Aggregate evidence only · No server deployment performed</footer></body></html>'
    (OUTPUT / "report.html").write_text(document)
    (OUTPUT / "report.md").write_text("\n".join(md))
    print(json.dumps({"report_generated": True, "draft": not args.final, "overall": overall,
                      "workflow_cases": sum(len(c) for _, c in LANES.values()),
                      "workflow_passed": sum(s == "passed" for v in lanes.values() for s in v["checks"].values())}))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for flag in ("migration-evidence", "backup-evidence", "sandbox-evidence", "capture-evidence", "production-safety", "startup-evidence", "final-safety-evidence"):
        parser.add_argument("--" + flag)
    parser.add_argument("--lane-summary", action="append", default=[])
    parser.add_argument("--issue", action="append", default=[],
                        choices=[code + ":" + status for code in ISSUES for status in ("open", "resolved")])
    parser.add_argument("--go-tests", choices=["passed", "failed", "pending"], default="pending")
    parser.add_argument("--cross-lane-preservation", choices=["passed", "failed", "pending"], default="pending")
    parser.add_argument("--include-private-paths", action="store_true")
    parser.add_argument("--final", action="store_true")
    parser.add_argument("--print-schema", action="store_true")
    args = parser.parse_args()
    try:
        if args.print_schema:
            print(json.dumps({"schema_version": 1, "lanes": {lane: {"status": "pending", "checks":
                [{"id": key, "status": "pending"} for key in checks]} for lane, (_, checks) in LANES.items()},
                "issue_codes": list(ISSUES)}, indent=2))
        else:
            build(args)
    except Exception:
        print("Report generation refused: evidence schema or completion validation failed.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
