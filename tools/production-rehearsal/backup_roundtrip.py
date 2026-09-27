#!/usr/bin/env python3
"""Exercise supported CLI backup/restore only on extra private local copies."""
import collections
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import sqlite3
import subprocess
import sys
import time

from reconcile import ROOT, SOURCE_ATTACHMENTS, filehash, ro, safe


def tree_hashes(root):
    return {str(p.relative_to(root)): filehash(p) for p in root.rglob('*') if p.is_file()}


def q(name):
    return '"' + name.replace('"', '""') + '"'


def normalized_rows(db, table, root):
    columns = [c[1] for c in db.execute('PRAGMA table_info(' + q(table) + ')')]
    result = []
    for row in db.execute('SELECT * FROM ' + q(table)):
        row = list(row)
        if table in ('payment_attachments', 'request_attachments'):
            index = columns.index('stored_path')
            path = Path(row[index])
            if '..' in path.parts:
                raise ValueError('unsafe attachment')
            row[index] = str(path.relative_to(root))
        result.append(tuple(row))
    return columns, collections.Counter(result)


def main():
    os.umask(0o077)
    work = ROOT / 'backup-restore'
    evidence = {'all_checks_pass': False, 'cli_started_http_or_schedulers': False}
    report = ROOT / 'backup-restore-evidence.json'
    try:
        safe(work, False)
        if work.exists():
            raise ValueError('refusing existing roundtrip directory')
        original_baseline = tree_hashes(ROOT / 'baseline')
        original_migrated = filehash(ROOT / 'migrated/data/fervid.db')
        source = work / 'source/data'
        source.mkdir(parents=True, mode=0o700)
        source_db, source_attachments = source / 'fervid.db', source / 'attachments'
        a = ro(ROOT / 'migrated/data/fervid.db')
        c = sqlite3.connect(source_db)
        a.backup(c); a.close()
        shutil.copytree(safe(ROOT / 'migrated/data/attachments'), source_attachments)
        for table in ('payment_attachments', 'request_attachments'):
            for ident, path in list(c.execute('SELECT id,stored_path FROM ' + table)):
                rel = Path(path).relative_to(SOURCE_ATTACHMENTS)
                if '..' in rel.parts:
                    raise ValueError('unsafe relative path')
                safe(source_attachments / rel)
                c.execute('UPDATE ' + table + ' SET stored_path=? WHERE id=?', (str(source_attachments / rel), ident))
        c.commit(); c.close(); source_db.chmod(0o600)
        binary = safe(ROOT / 'release-build/server')
        backup_dir = work / 'packages'
        def cli_env(data, backups):
            return {'PATH': '/usr/bin:/bin', 'TZ': 'Asia/Kolkata',
                    'FERVID_ADDR': '127.0.0.1:0', 'FERVID_DB': str(data / 'fervid.db'),
                    'FERVID_ATTACHMENT_DIR': str(data / 'attachments'),
                    'FERVID_BACKUP_DIR': str(backups),
                    'FERVID_SESSION_KEY': secrets.token_hex(32), 'FERVID_SECURE_COOKIES': 'false',
                    'FERVID_ADMIN_EMAIL': 'unused@example.test', 'FERVID_ADMIN_NAME': 'Unused QA Seed',
                    'FERVID_ADMIN_PASSWORD': secrets.token_urlsafe(32), 'FERVID_SMTP_PASSWORD': '',
                    'FERVID_BACKUP_KEEP_DAYS': '36500', 'FERVID_BACKUP_KEEP_MONTHS': '1200', 'FERVID_BACKUP_HOUR': '23'}
        def run(stage, args, env):
            started = time.monotonic()
            result = subprocess.run([str(binary), *args], env=env, cwd=work, capture_output=True, timeout=60)
            (work / (stage + '-private.log')).write_bytes(result.stdout + result.stderr)
            evidence[stage + '_exit_code'] = result.returncode
            evidence[stage + '_elapsed_seconds'] = round(time.monotonic() - started, 3)
            if result.returncode:
                raise ValueError('CLI stage failed')
        run('backup', ['--backup'], cli_env(source, backup_dir))
        packages = [p for p in backup_dir.iterdir() if p.is_dir() and (p / 'manifest.json').is_file()]
        if len(packages) != 1:
            raise ValueError('unexpected backup count')
        package = safe(packages[0])
        manifest = json.loads((package / 'manifest.json').read_text())
        evidence['backup_manifest_roots_match_local_source'] = manifest['source_attachment_dir'] == str(source_attachments) and manifest['source_db_path'] == str(source_db)
        restored = work / 'restored/data'
        run('restore', ['--restore', str(package)], cli_env(restored, work / 'restored/backups'))
        before, after = ro(source_db), ro(restored / 'fervid.db')
        tables = [r[0] for r in before.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")]
        evidence['schema_version_20_preserved'] = before.execute('PRAGMA user_version').fetchone() == after.execute('PRAGMA user_version').fetchone() == (20,)
        evidence['schema_objects_match'] = list(before.execute('SELECT type,name,tbl_name,sql FROM sqlite_master ORDER BY type,name')) == list(after.execute('SELECT type,name,tbl_name,sql FROM sqlite_master ORDER BY type,name'))
        table_results = {}
        for table in tables:
            columns1, rows1 = normalized_rows(before, table, source_attachments)
            columns2, rows2 = normalized_rows(after, table, restored / 'attachments')
            table_results[table] = {'row_count': sum(rows1.values()), 'all_columns_match_after_path_normalization': columns1 == columns2 and rows1 == rows2}
        evidence['tables'] = table_results
        evidence['full_integrity_pass'] = list(after.execute('PRAGMA integrity_check')) == [('ok',)]
        evidence['foreign_keys_pass'] = not list(after.execute('PRAGMA foreign_key_check'))
        queries = ["SELECT substr(paid_on,1,7),head_id,voided_at IS NOT NULL,COUNT(*),SUM(amount) FROM payments GROUP BY 1,2,3", 'SELECT month,head_id,COUNT(*),SUM(amount) FROM budgets GROUP BY 1,2']
        evidence['financial_groups_match'] = all(collections.Counter(before.execute(sql)) == collections.Counter(after.execute(sql)) for sql in queries)
        source_files = tree_hashes(source_attachments)
        restored_files = tree_hashes(restored / 'attachments')
        evidence['attachment_file_count'] = len(source_files)
        evidence['all_attachment_bytes_match'] = source_files == restored_files
        valid_paths = True
        referenced_count = 0
        for table in ('payment_attachments', 'request_attachments'):
            for path, size in after.execute('SELECT stored_path,size_bytes FROM ' + table):
                path = safe(Path(path))
                valid_paths = valid_paths and path.is_relative_to(restored / 'attachments') and path.is_file() and path.stat().st_size == size
                referenced_count += 1
        evidence['referenced_attachment_count'] = referenced_count
        evidence['restored_attachment_paths_local_and_sizes_match'] = valid_paths
        before.close(); after.close()
        evidence['sealed_baseline_all_files_unchanged'] = original_baseline == tree_hashes(ROOT / 'baseline')
        evidence['fidelity_migrated_database_file_unchanged'] = original_migrated == filehash(ROOT / 'migrated/data/fervid.db')
        evidence['all_checks_pass'] = all(v for k,v in evidence.items() if isinstance(v, bool) and k not in ('all_checks_pass', 'cli_started_http_or_schedulers')) and all(v['all_columns_match_after_path_normalization'] for v in table_results.values())
    except Exception:
        evidence['all_checks_pass'] = False
        evidence['failure_details_redacted'] = True
    report.write_text(json.dumps(evidence, indent=2, sort_keys=True))
    # Safe summary only; raw subprocess logs stay under the private root.
    print(json.dumps(evidence, indent=2, sort_keys=True))
    return 0 if evidence['all_checks_pass'] else 1


if __name__ == '__main__':
    sys.exit(main())
