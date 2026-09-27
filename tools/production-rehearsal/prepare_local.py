#!/usr/bin/env python3
"""Prepare private local QA copies; never opens the source DB for writing.

prepare requires an already migrated/reconciled DB and a bcrypt helper accepting
the password on stdin and returning only its bcrypt hash. run additionally needs
an explicit READY file, created by the coordinating operator after reconciliation.
All credentials and detailed manifests stay beneath PRIVATE_ROOT, never web output.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time

PRIVATE_ROOT = Path.home() / "Library/Application Support/Fervid Budget QA/production-rehearsal-2026-09-26"
TEMPLATE = Path(__file__).with_name("local-qa.sb")


def private(path):
    path = Path(path).resolve()
    if not path.is_relative_to(PRIVATE_ROOT.resolve()):
        raise ValueError("path is outside the private rehearsal root")
    return path


def write_json(path, value):
    with open(path, "x", encoding="utf-8") as handle:
        json.dump(value, handle, indent=2)
        handle.write("\n")
    os.chmod(path, 0o600)


def check_port(port):
    if port not in range(9000, 9004):
        raise ValueError("only loopback ports 9000–9003 are allowed")
    with socket.socket() as sock:
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        sock.bind(("127.0.0.1", port))


def tree_copy(source, target):
    source = Path(source).resolve(strict=True)
    if not source.is_dir():
        raise ValueError("expected a directory")
    for item in source.rglob("*"):
        if item.is_symlink() or not (item.is_file() or item.is_dir()):
            raise ValueError("copy source contains a symlink or special file")
    target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    shutil.copytree(source, target, copy_function=shutil.copyfile)
    for item in [target, *target.rglob("*")]:
        os.chmod(item, 0o700 if item.is_dir() else 0o600)


def sandbox_command(lane, port, executable, *args):
    params = {"PRIVATE": PRIVATE_ROOT.resolve(), "DATA": lane / "data",
              "RELEASE": lane / "release", "LOGS": lane / "logs",
              "TMP": lane / "tmp", "LISTEN": f"localhost:{port}"}
    command = ["/usr/bin/sandbox-exec"]
    for key, value in params.items():
        command += ["-D", f"{key}={value}"]
    return command + ["-f", str(TEMPLATE), str(executable), *map(str, args)]


def prepare(args):
    check_port(args.port)
    if not re.fullmatch(r"[a-z][a-z0-9-]{0,40}", args.lane):
        raise ValueError("invalid lane name")
    source = private(args.source_db)
    attachments = private(args.source_attachments)
    lane = private(PRIVATE_ROOT / "lanes" / args.lane)
    lane.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    lane.mkdir(mode=0o700)  # Refuse to overwrite an earlier lane.
    for folder in ("data", "logs", "tmp", "release"):
        (lane / folder).mkdir(mode=0o700)
    (lane / "data/backups").mkdir(mode=0o700)
    tree_copy(attachments, lane / "data/attachments")
    shutil.copyfile(Path(args.binary).resolve(strict=True), lane / "release/server")
    os.chmod(lane / "release/server", 0o500)
    tree_copy(args.static_dir, lane / "release/web/static")
    for item in (lane / "release").rglob("*"):
        os.chmod(item, 0o500 if item.is_dir() else (0o500 if item.name == "server" else 0o400))
    db_path = lane / "data/qa.db"
    with sqlite3.connect(source.as_uri() + "?mode=ro", uri=True) as src:
        if src.execute("PRAGMA user_version").fetchone()[0] != 20:
            raise ValueError("source must be reconciled schema version 20")
        with sqlite3.connect(db_path) as dst:
            src.backup(dst)
    password = "LocalQA9-" + secrets.token_urlsafe(24)
    hashed = subprocess.run([args.hash_helper, "-hash-password"], input=password,
                            text=True, capture_output=True, check=True).stdout.strip()
    if not re.fullmatch(r"\$2[aby]\$\d\d\$[./A-Za-z0-9]{53}", hashed):
        raise ValueError("helper did not return one bcrypt hash")
    source_roots = [Path(p) for p in args.stored_root] + [attachments]
    rebased = 0
    with sqlite3.connect(db_path) as db:
        db.execute("PRAGMA foreign_keys=ON")
        users = list(db.execute("SELECT id,email,name,role,active FROM users ORDER BY id"))
        db.execute("UPDATE users SET password_hash=?,session_version=session_version+1,"
                   "attempt_count=0,last_attempt=NULL,locked=NULL", (hashed,))
        for table in ("password_resets", "password_reset_throttle"):
            db.execute(f"DELETE FROM {table}")
        db.execute("UPDATE notification_settings SET email_enabled=0,to_recipients='',cc_recipients=''")
        for key in ("smtp_host", "smtp_port", "smtp_username", "smtp_password", "smtp_from_name",
                    "smtp_from_addr", "management_recipients"):
            db.execute("INSERT INTO app_settings(key,value) VALUES(?,'') "
                       "ON CONFLICT(key) DO UPDATE SET value=''", (key,))
        db.execute("INSERT INTO app_settings(key,value) VALUES('base_url',?) "
                   "ON CONFLICT(key) DO UPDATE SET value=excluded.value", (f"http://127.0.0.1:{args.port}",))
        for table in ("request_attachments", "payment_attachments"):
            for row_id, stored in list(db.execute(f"SELECT id,stored_path FROM {table}")):
                path = Path(stored)
                matches = [path.relative_to(root) for root in source_roots if path.is_relative_to(root)]
                if not path.is_absolute() or not matches or len(set(matches)) != 1:
                    raise ValueError("attachment path lacks an unambiguous approved source root")
                relative = matches[0]
                target = (lane / "data/attachments" / relative).resolve(strict=True)
                if not target.is_relative_to(lane / "data/attachments") or not target.is_file():
                    raise ValueError("attachment escapes lane or is not a file")
                db.execute(f"UPDATE {table} SET stored_path=? WHERE id=?", (str(target), row_id))
                rebased += 1
        if db.execute("PRAGMA integrity_check").fetchall() != [("ok",)]:
            raise ValueError("QA database integrity failed")
        if db.execute("PRAGMA foreign_key_check").fetchall():
            raise ValueError("QA database foreign keys failed")
        if users != list(db.execute("SELECT id,email,name,role,active FROM users ORDER BY id")):
            raise ValueError("user identities or active state unexpectedly changed")
    env = {"FERVID_ADDR": f"127.0.0.1:{args.port}", "FERVID_DB": str(db_path),
           "FERVID_ATTACHMENT_DIR": str(lane / "data/attachments"),
           "FERVID_BACKUP_DIR": str(lane / "data/backups"),
           "FERVID_SESSION_KEY": secrets.token_hex(32), "FERVID_SECURE_COOKIES": "false",
           "FERVID_SMTP_PASSWORD": "", "FERVID_BACKUP_KEEP_DAYS": "2",
           "FERVID_BACKUP_KEEP_MONTHS": "0", "FERVID_BACKUP_HOUR": "23"}
    write_json(lane / "credentials.json", {"password": password, "users": users})
    write_json(lane / "run.json", {"port": args.port, "env": env,
                                   "binary_sha256": hashlib.sha256((lane / "release/server").read_bytes()).hexdigest()})
    write_json(lane / "preparation.json", {"schema": 20, "users_preserved": len(users),
                                           "attachments_rebased": rebased, "status": "prepared_not_started"})
    print(json.dumps({"lane": args.lane, "prepared": True, "users_preserved": len(users),
                      "attachments_rebased": rebased, "port": args.port}))


def run(args):
    lane = private(args.lane_root)
    ready = private(args.ready_file)
    if not ready.is_file() or ready.read_text().strip() != "READY":
        raise ValueError("coordinator READY marker is required")
    settings = json.loads((lane / "run.json").read_text())
    check_port(settings["port"])
    if hashlib.sha256((lane / "release/server").read_bytes()).hexdigest() != settings["binary_sha256"]:
        raise ValueError("release binary changed")
    env = {"PATH": "/usr/bin:/bin", "HOME": str(lane / "tmp"), "TMPDIR": str(lane / "tmp"),
           **settings["env"]}
    os.chdir(lane / "release")
    command = sandbox_command(lane, settings["port"], lane / "release/server")
    os.execve(command[0], command, env)


def self_test(args):
    """Use only dummy files and loopback TCP; never touch external network."""
    check_port(args.port)
    PRIVATE_ROOT.mkdir(mode=0o700, parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="sandbox-selftest-", dir=PRIVATE_ROOT) as name:
        lane = Path(name)
        for folder in ("data", "logs", "tmp", "release"):
            (lane / folder).mkdir(mode=0o700)
        (lane / "unreadable-dummy").write_text("dummy")
        probe = '''import json,socket,pathlib,sys,sqlite3
root=pathlib.Path(sys.argv[1]); port=int(sys.argv[2]); sink=int(sys.argv[3]); results={}
try:
    socket.create_connection(('127.0.0.1',sink),timeout=1)
    results['outbound_blocked']=False
except PermissionError: results['outbound_blocked']=True
try:
    sock=socket.socket(); sock.bind(('0.0.0.0',0)); sock.close()
    results['public_bind_blocked']=False
except PermissionError: results['public_bind_blocked']=True
for key,path in [('outside_write_blocked',root/'forbidden'),('release_write_blocked',root/'release/forbidden')]:
    try: path.write_text('dummy'); results[key]=False
    except PermissionError: results[key]=True
try: (root/'unreadable-dummy').read_text(); results['private_read_blocked']=False
except PermissionError: results['private_read_blocked']=True
(root/'data/allowed').write_text('dummy'); results['data_write_allowed']=True
with sqlite3.connect(root/'data/probe.db') as db:
    db.execute('PRAGMA journal_mode=WAL'); db.execute('CREATE TABLE probe (id INTEGER)')
    db.execute('INSERT INTO probe VALUES (1)')
    results['sqlite_wal_allowed']=db.execute('SELECT id FROM probe').fetchone()==(1,)
s=socket.socket(); s.bind(('127.0.0.1',port)); s.listen(1); print('READY',flush=True)
c,_=s.accept(); c.recv(100); c.sendall(json.dumps(results).encode()); c.close(); s.close()
'''
        with socket.socket() as sink:
            sink.bind(("127.0.0.1", 0)); sink.listen(1); sink.settimeout(0.1)
            command = sandbox_command(lane, args.port, sys.executable, "-B", "-c", probe,
                                      lane, args.port, sink.getsockname()[1])
            process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            try:
                deadline = time.monotonic() + 8
                connection = None
                while time.monotonic() < deadline:
                    if process.poll() is not None:
                        _, errors = process.communicate()
                        raise RuntimeError("dummy sandbox probe exited: " + errors.strip())
                    try:
                        connection = socket.create_connection(("127.0.0.1", args.port), timeout=0.2)
                        break
                    except ConnectionRefusedError:
                        time.sleep(0.05)
                if connection is None:
                    raise RuntimeError("sandbox loopback ingress did not become available")
                with connection:
                    connection.settimeout(2); connection.sendall(b"probe")
                    results = json.loads(connection.recv(4096))
                results["loopback_ingress_allowed"] = True
                try: unexpected, _ = sink.accept(); unexpected.close(); results["no_outbound_connection"] = False
                except TimeoutError: results["no_outbound_connection"] = True
                process.communicate(timeout=2)
                if process.returncode != 0 or not all(results.values()):
                    raise RuntimeError("sandbox self-test failed: " + json.dumps(results))
                print(json.dumps(results, sort_keys=True))
            finally:
                if process.poll() is None:
                    process.kill(); process.communicate()


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    prep = sub.add_parser("prepare")
    for key in ("source-db", "source-attachments", "binary", "static-dir", "hash-helper", "lane"):
        prep.add_argument("--" + key, required=True)
    prep.add_argument("--stored-root", action="append", required=True)
    prep.add_argument("--port", type=int, required=True)
    launch = sub.add_parser("run")
    launch.add_argument("--lane-root", required=True)
    launch.add_argument("--ready-file", required=True)
    test = sub.add_parser("self-test")
    test.add_argument("--port", type=int, default=9003)
    args = parser.parse_args()
    try:
        {"prepare": prepare, "run": run, "self-test": self_test}[args.action](args)
    except Exception as exc:
        # Do not echo SQL rows, subprocess stderr or credentials on failure.
        detail = str(exc) if args.action == "self-test" else "inspect inputs and private lane state"
        print(f"FAILED: {type(exc).__name__}: {detail}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
