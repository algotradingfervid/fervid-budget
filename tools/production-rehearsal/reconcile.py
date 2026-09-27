#!/usr/bin/env python3
"""Local-only fidelity checks; output contains only counts and pass/fail.

No stored row, financial amount, identity, password hash, attachment name or
comparison digest is printed. Both databases are opened read-only for checks.
"""
import argparse
import collections
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import sys
from urllib.parse import quote

ROOT = Path('/Users/narendhupati/Library/Application Support/Fervid Budget QA/production-rehearsal-2026-09-26')
SOURCE_ATTACHMENTS = Path('/opt/fervid-budget/data/attachments')
TABLES = ('audit_log', 'budget_months', 'budgets', 'heads', 'month_locks', 'payment_attachments', 'payments', 'projects', 'users')


def safe(path, exists=True):
    path = Path(path)
    if not path.is_absolute() or '..' in path.parts or not path.is_relative_to(ROOT):
        raise ValueError('path not allowlisted')
    if ROOT.stat().st_mode & 0o077:
        raise ValueError('root permissions')
    for parent in (path, *path.parents):
        if parent.is_symlink():
            raise ValueError('symlink denied')
    if exists and not path.exists():
        raise ValueError('missing path')
    for suffix in ('-wal', '-shm', '-journal'):
        if Path(str(path) + suffix).is_symlink():
            raise ValueError('symlink sidecar denied')
    return path


def ro(path):
    safe(path)
    db = sqlite3.connect('file:' + quote(str(path), safe='/') + '?mode=ro', uri=True)
    db.execute('PRAGMA query_only=ON')
    db.execute('BEGIN')
    return db


def filehash(path):
    h = hashlib.sha256()
    with safe(path).open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(chunk)
    return h.digest()


def prepare(baseline, destination, attachment_source, attachment_destination):
    safe(baseline)
    safe(destination, False)
    if destination.exists() or destination.parent.exists():
        raise ValueError('refusing existing destination')
    safe(attachment_source)
    for path in attachment_source.rglob('*'):
        safe(path)
        if not path.is_file() and not path.is_dir():
            raise ValueError('nonregular attachment')
    original = filehash(baseline)
    destination.parent.mkdir(parents=True, mode=0o700)
    source = ro(baseline)
    target = sqlite3.connect(destination)
    try:
        source.backup(target)
    finally:
        target.close()
        source.close()
    destination.chmod(0o600)
    shutil.copytree(attachment_source, attachment_destination)
    for path in (attachment_destination, *attachment_destination.rglob('*')):
        path.chmod(0o700 if path.is_dir() else 0o600)
    if original != filehash(baseline):
        raise ValueError('baseline changed')
    return {'prepared_local_copy': True, 'baseline_file_unchanged': True}


def checks(baseline, target, attachment_source, attachment_target):
    a, b = ro(baseline), ro(target)
    result = {'baseline_schema_version': a.execute('PRAGMA user_version').fetchone()[0],
              'migrated_schema_version': b.execute('PRAGMA user_version').fetchone()[0], 'tables': {}}
    checks = []
    def check(name, ok):
        result[name] = bool(ok)
        checks.append(bool(ok))
    check('expected_schema_versions', result['baseline_schema_version'] == 0 and result['migrated_schema_version'] == 20)
    check('full_integrity_pass', list(a.execute('PRAGMA integrity_check')) == [('ok',)] and list(b.execute('PRAGMA integrity_check')) == [('ok',)])
    check('foreign_keys_pass', not list(a.execute('PRAGMA foreign_key_check')) and not list(b.execute('PRAGMA foreign_key_check')))
    source_tables = {r[0] for r in a.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")}
    check('baseline_tables_expected', source_tables == set(TABLES))
    for table in TABLES:
        columns = list(a.execute('PRAGMA table_info("' + table + '")'))
        names = [c[1] for c in columns]
        pk = [c[1] for c in sorted(columns, key=lambda c: c[5]) if c[5]]
        select = 'SELECT ' + ','.join('"' + c.replace('"', '""') + '"' for c in names) + ' FROM "' + table + '"'
        aa, bb = list(a.execute(select)), list(b.execute(select))
        equal = collections.Counter(aa) == collections.Counter(bb)
        pk_indices = [names.index(c) for c in pk]
        ids_equal = {tuple(row[i] for i in pk_indices) for row in aa} == {tuple(row[i] for i in pk_indices) for row in bb}
        result['tables'][table] = {'baseline_count': len(aa), 'migrated_count': len(bb), 'all_original_columns_match': equal, 'primary_keys_match': ids_equal}
        checks.extend((equal, ids_equal))
    queries = {
        'payment_month_head_void_status_aggregates_match': "SELECT substr(paid_on,1,7),head_id,voided_at IS NOT NULL,COUNT(*),SUM(amount) FROM payments GROUP BY 1,2,3",
        'payment_month_project_aggregates_match': "SELECT substr(p.paid_on,1,7),h.project_id,p.voided_at IS NOT NULL,COUNT(*),SUM(p.amount) FROM payments p JOIN heads h ON h.id=p.head_id GROUP BY 1,2,3",
        'budget_month_head_aggregates_match': 'SELECT month,head_id,COUNT(*),SUM(amount) FROM budgets GROUP BY 1,2',
        'budget_month_project_aggregates_match': 'SELECT b.month,h.project_id,COUNT(*),SUM(b.amount) FROM budgets b JOIN heads h ON h.id=b.head_id GROUP BY 1,2',
        'password_hashes_and_account_state_match': 'SELECT id,password_hash,active,role,attempt_count,last_attempt,locked FROM users',
    }
    for name, sql in queries.items():
        check(name, collections.Counter(a.execute(sql)) == collections.Counter(b.execute(sql)))
    check('all_legacy_payments_remain_unlinked', b.execute("SELECT COUNT(*) FROM payments WHERE request_id IS NOT NULL OR vendor_id IS NOT NULL OR settlement<>'' OR partial_reason<>'' OR submission_key IS NOT NULL").fetchone()[0] == 0)
    for table in ('payment_requests', 'request_attachments', 'request_comments', 'request_number_seq', 'vendors', 'recovery_events', 'budget_lines', 'notifications', 'password_resets', 'password_reset_throttle'):
        check(table + '_initially_empty', b.execute('SELECT COUNT(*) FROM ' + table).fetchone()[0] == 0)
    expected_roles = {(uid, 'Admin' if role == 'admin' else 'Accounts') for uid, role in a.execute('SELECT id,role FROM users')}
    actual_roles = set(b.execute('SELECT ur.user_id,r.name FROM user_roles ur JOIN roles r ON r.id=ur.role_id'))
    check('legacy_role_mapping_exact', expected_roles == actual_roles)
    check('new_user_fields_default', b.execute('SELECT COUNT(*) FROM users WHERE default_approver_id IS NOT NULL OR session_version<>0').fetchone()[0] == 0)
    # INTEGER PRIMARY KEY (without AUTOINCREMENT) derives successor from MAX(id).
    # Compare maxima internally, without exposing identifiers or inserting rows.
    check('legacy_integer_id_successors_unchanged', all(a.execute('SELECT MAX(id) FROM ' + t).fetchone() == b.execute('SELECT MAX(id) FROM ' + t).fetchone() for t in TABLES if t not in ('month_locks', 'budget_months')))
    check('no_autoincrement_sequence_expected', not list(a.execute("SELECT name FROM sqlite_master WHERE name='sqlite_sequence'")) and not list(b.execute("SELECT name FROM sqlite_master WHERE name='sqlite_sequence'")))
    attachments = list(a.execute('SELECT stored_path,size_bytes FROM payment_attachments'))
    attachment_pass = True
    for original, size in attachments:
        rel = Path(original).relative_to(SOURCE_ATTACHMENTS)
        if '..' in rel.parts:
            raise ValueError('unsafe attachment relative path')
        source, copied = safe(attachment_source / rel), safe(attachment_target / rel)
        attachment_pass = attachment_pass and source.is_file() and copied.is_file() and source.stat().st_size == size and copied.stat().st_size == size and filehash(source) == filehash(copied)
    check('referenced_attachment_files_size_and_hash_match', attachment_pass)
    result['referenced_attachment_count'] = len(attachments)
    result['all_checks_pass'] = all(checks)
    a.close(); b.close()
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('prepare', 'check'))
    parser.add_argument('--synthetic', action='store_true')
    args = parser.parse_args()
    lane = ROOT / ('synthetic' if args.synthetic else 'migrated')
    baseline_root = ROOT / ('synthetic-baseline' if args.synthetic else 'baseline')
    baseline, target = baseline_root / 'fervid.db', lane / 'data/fervid.db'
    source_attachments, target_attachments = baseline_root / 'attachments', lane / 'data/attachments'
    try:
        os.umask(0o077)
        if args.action == 'prepare':
            result = prepare(baseline, target, source_attachments, target_attachments)
        else:
            result = checks(baseline, target, source_attachments, target_attachments)
        print(json.dumps(result, sort_keys=True, indent=2))
        if result.get('all_checks_pass') is False:
            return 1
        return 0
    except Exception:
        # Underlying exceptions may reveal identities or stored values.
        print(json.dumps({'all_checks_pass': False, 'failed_stage': args.action, 'details_redacted': True}))
        return 1


if __name__ == '__main__':
    sys.exit(main())
