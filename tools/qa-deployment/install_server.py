#!/usr/bin/env python3
"""First QA install only. Run over SSH after reviewing the fixed paths/hash."""
import hashlib
import json
import os
from pathlib import Path
import pwd
import shutil
import socket
import sqlite3
import subprocess
import tarfile

STAGE = Path('/root/fervid-budget-qa-deploy-20260926')
QA = Path('/opt/fervid-budget-qa')
RELEASE = '20260926-qa-final'
EXPECTED = '5ed1e87c79b4bd989641c35bc10309ea70d0dbc1a10bfe29c27cddb8968c75a5'
UNIT = Path('/etc/systemd/system/fervid-budget-qa.service')
ENV = Path('/etc/fervid-budget-qa.env')

def run(*args):
    return subprocess.check_output(args, text=True).strip()

def main():
    os.umask(0o077)
    assert os.geteuid() == 0 and run('hostname') == 'fervid-budget'
    assert run('systemctl', 'show', 'fervid-budget.service', '-p', 'MainPID', '--value') == '400907'
    assert not QA.exists() and not UNIT.exists() and not ENV.exists()
    assert run('systemctl', 'show', 'fervid-budget-qa.service', '-p', 'LoadState', '--value') == 'not-found'
    with socket.socket() as s:
        s.bind(('127.0.0.1', 8081))
    assert hashlib.sha256((STAGE / 'package.tar.gz').read_bytes()).hexdigest() == EXPECTED
    unpack = STAGE / 'unpacked'
    unpack.mkdir(mode=0o700)
    with tarfile.open(STAGE / 'package.tar.gz') as archive:
        for member in archive.getmembers():
            assert member.isfile() or member.isdir()
            assert not member.issym() and not member.islnk()
            assert (unpack / member.name).resolve().is_relative_to(unpack)
        archive.extractall(unpack, filter='data')
    db = sqlite3.connect((unpack / 'data/qa.db').as_uri() + '?mode=ro', uri=True)
    assert db.execute('PRAGMA integrity_check').fetchall() == [('ok',)]
    assert not db.execute('PRAGMA foreign_key_check').fetchall()
    assert db.execute('PRAGMA user_version').fetchone()[0] == 20
    assert db.execute('SELECT COUNT(*) FROM users').fetchone()[0] == 9
    assert db.execute('SELECT COUNT(*) FROM payments').fetchone()[0] == 626
    assert db.execute('SELECT COUNT(*) FROM notification_settings WHERE email_enabled<>0').fetchone()[0] == 0
    for stored, size in db.execute('SELECT stored_path,size_bytes FROM payment_attachments'):
        rel = Path(stored).relative_to(QA / 'data/attachments')
        assert (unpack / 'data/attachments' / rel).stat().st_size == size
    db.close()
    try:
        pwd.getpwnam('fervid-qa')
        raise RuntimeError('QA account already exists; review it before installation')
    except KeyError:
        subprocess.run(['useradd', '--system', '--user-group', '--home-dir', str(QA), '--no-create-home', '--shell', '/usr/sbin/nologin', 'fervid-qa'], check=True)
    account = pwd.getpwnam('fervid-qa')
    QA.mkdir(mode=0o755)
    (QA / 'releases').mkdir(mode=0o755)
    os.chmod(QA, 0o755)
    os.chmod(QA / 'releases', 0o755)
    release = QA / 'releases' / RELEASE
    shutil.copytree(unpack / 'release', release)
    for path in [release, *release.rglob('*')]:
        os.chmod(path, 0o755 if path.is_dir() or path.name == 'server' else 0o644)
    shutil.copytree(unpack / 'data', QA / 'data')
    for path in [QA / 'data', *(QA / 'data').rglob('*')]:
        assert not path.is_symlink()
        os.chown(path, account.pw_uid, account.pw_gid)
        os.chmod(path, 0o700 if path.is_dir() else 0o600)
    shutil.copyfile(unpack / 'config/fervid-budget-qa.env', ENV)
    os.chmod(ENV, 0o600)
    shutil.copyfile(unpack / 'config/fervid-budget-qa.service', UNIT)
    os.chmod(UNIT, 0o644)
    (QA / 'current').symlink_to(release)
    subprocess.run(['systemd-analyze', 'verify', str(UNIT)], check=True)
    subprocess.run(['systemctl', 'daemon-reload'], check=True)
    subprocess.run(['systemctl', 'enable', '--now', 'fervid-budget-qa.service'], check=True)
    print(json.dumps({'installed': True, 'release': RELEASE, 'port': 8081, 'production_pid_unchanged': run('systemctl', 'show', 'fervid-budget.service', '-p', 'MainPID', '--value') == '400907'}))

if __name__ == '__main__':
    main()
