#!/usr/bin/env python3
"""One-shot read-only SQLite snapshot with verified, snapshot-referenced files.

No application imports, migrations, checkpoint or source journal-mode changes.
Production mode permits only the fixed source and root-owned staging parent.
Attachment immutability must independently be established by the operator.
"""

import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import stat
import sys
import time
import uuid


class CaptureError(Exception):
    pass


def check_deadline(deadline):
    if time.monotonic() >= deadline:
        raise CaptureError("capture deadline exceeded")


def safe_absolute(path):
    path = Path(path)
    if not path.is_absolute() or ".." in path.parts:
        raise CaptureError("an absolute non-traversing path is required")
    for part in (path, *path.parents):
        if part.is_symlink():
            raise CaptureError("symlink path rejected")
    return path


def write_json(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as stream:
        json.dump(value, stream, separators=(",", ":"), sort_keys=True)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())


def open_beneath(root_fd, relative):
    """Walk using dirfds so no symlink in a source descendant is followed."""
    parts = Path(relative).parts
    if not parts or Path(relative).is_absolute() or any(p in (".", "..") for p in parts):
        raise CaptureError("attachment path rejected")
    current = os.dup(root_fd)
    try:
        for part in parts[:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=current)
            os.close(current)
            current = child
        return os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=current)
    finally:
        os.close(current)


def signature(info):
    return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def hash_fd(fd, deadline, output=None):
    os.lseek(fd, 0, os.SEEK_SET)
    digest = hashlib.sha256()
    size = 0
    while True:
        check_deadline(deadline)
        block = os.read(fd, 1024 * 1024)
        if not block:
            break
        digest.update(block)
        size += len(block)
        if output is not None:
            output.write(block)
    return digest.hexdigest(), size


def copy_attachment(root_fd, relative, target, expected_size, deadline):
    fd = open_beneath(root_fd, relative)
    try:
        before = os.fstat(fd)
        if not stat.S_ISREG(before.st_mode) or before.st_size != expected_size:
            raise CaptureError("attachment type or recorded size mismatch")
        first = hash_fd(fd, deadline)
        target.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
        out_fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(out_fd, "wb") as output:
            copied = hash_fd(fd, deadline, output)
            output.flush()
            os.fsync(output.fileno())
        after_hash = hash_fd(fd, deadline)
        after = os.fstat(fd)
        path_fd = open_beneath(root_fd, relative)
        try:
            current = os.fstat(path_fd)
        finally:
            os.close(path_fd)
        dest_fd = os.open(target, os.O_RDONLY | os.O_NOFOLLOW)
        try:
            destination = hash_fd(dest_fd, deadline)
        finally:
            os.close(dest_fd)
        if not (first == copied == after_hash == destination and
                signature(before) == signature(after) == signature(current)):
            raise CaptureError("attachment changed during capture")
        return first
    finally:
        os.close(fd)


def read_only(path):
    conn = sqlite3.connect(path.as_uri() + "?mode=ro", uri=True, timeout=0.1)
    conn.execute("PRAGMA query_only=ON")
    return conn


def capture(source, attachment_root, output_parent, deadline_seconds=15, local_test=False):
    os.umask(0o077)
    source, attachment_root, output_parent = map(safe_absolute, (source, attachment_root, output_parent))
    if not local_test:
        if (source != Path("/opt/fervid-budget/data/fervid.db") or
                attachment_root != Path("/opt/fervid-budget/data/attachments") or
                output_parent != Path("/root/fervid-qa-snapshots") or os.geteuid() != 0):
            raise CaptureError("production path or account allowlist failed")
    if not source.is_file() or not attachment_root.is_dir():
        raise CaptureError("source input missing")
    if output_parent == source.parent or output_parent.is_relative_to(attachment_root):
        raise CaptureError("output overlaps source")
    if deadline_seconds <= 0 or deadline_seconds > 300:
        raise CaptureError("deadline must be in range 0–300 seconds")
    output_parent.mkdir(mode=0o700, parents=False, exist_ok=True)
    parent_info = output_parent.stat()
    if parent_info.st_uid != os.geteuid() or stat.S_IMODE(parent_info.st_mode) != 0o700:
        raise CaptureError("staging parent must be owned by caller and mode 0700")
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    package = output_parent / ("snapshot-" + stamp + "-" + uuid.uuid4().hex[:12])
    package.mkdir(mode=0o700)
    deadline = time.monotonic() + deadline_seconds
    complete = False
    started = datetime.datetime.now(datetime.timezone.utc).isoformat()
    try:
        db_path = package / "fervid.db"
        src = read_only(source)
        dst = sqlite3.connect(db_path)
        try:
            src.backup(dst, pages=64, progress=lambda *_: check_deadline(deadline), sleep=0.05)
        finally:
            dst.close()
            src.close()
        os.chmod(db_path, 0o600)
        conn = read_only(db_path)
        conn.set_progress_handler(lambda: int(time.monotonic() >= deadline), 1000)
        try:
            if conn.execute("PRAGMA integrity_check").fetchall() != [("ok",)]:
                raise CaptureError("snapshot integrity check failed")
            if conn.execute("PRAGMA foreign_key_check").fetchone() is not None:
                raise CaptureError("snapshot foreign key check failed")
            version = conn.execute("PRAGMA user_version").fetchone()[0]
            tables = {row[0] for row in conn.execute("SELECT name FROM sqlite_master WHERE type='table'")}
            counts = {}
            # Identifier quoting accommodates any source table; no table content is printed.
            for table in sorted(tables):
                escaped = table.replace('"', '""')
                counts[table] = conn.execute('SELECT COUNT(*) FROM "' + escaped + '"').fetchone()[0]
            (package / "attachments").mkdir(mode=0o700)
            root_fd = os.open(attachment_root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            inventory, copied_paths = [], {}
            try:
                for table in ("payment_attachments", "request_attachments"):
                    if table not in tables:
                        continue
                    for row_id, stored, size in conn.execute("SELECT id,stored_path,size_bytes FROM " + table + " ORDER BY id"):
                        check_deadline(deadline)
                        if not isinstance(stored, str) or not isinstance(size, int) or size < 0:
                            raise CaptureError("invalid attachment metadata")
                        stored_path = Path(stored)
                        if not stored_path.is_absolute() or ".." in stored_path.parts:
                            raise CaptureError("attachment path must be absolute and non-traversing")
                        try:
                            relative = stored_path.relative_to(attachment_root)
                        except ValueError:
                            raise CaptureError("attachment escapes source root") from None
                        key = relative.as_posix()
                        if key in copied_paths:
                            digest, actual_size = copied_paths[key]
                            if actual_size != size:
                                raise CaptureError("conflicting attachment metadata")
                        else:
                            digest, actual_size = copy_attachment(root_fd, relative, package / "attachments" / relative, size, deadline)
                            copied_paths[key] = (digest, actual_size)
                        inventory.append({"table": table, "id": row_id, "relative_path": key, "size_bytes": actual_size, "sha256": digest})
            finally:
                os.close(root_fd)
        finally:
            conn.close()
        check_deadline(deadline)
        db_fd = os.open(db_path, os.O_RDONLY | os.O_NOFOLLOW)
        try:
            db_hash, db_size = hash_fd(db_fd, deadline)
            os.fsync(db_fd)
        finally:
            os.close(db_fd)
        finished = datetime.datetime.now(datetime.timezone.utc).isoformat()
        write_json(package / "manifest.json", {"created_at": finished, "database_file": "fervid.db", "attachment_dir": "attachments", "source_db_path": str(source), "source_attachment_dir": str(attachment_root), "source_attachment_absolute": str(attachment_root)})
        write_json(package / "capture-evidence.json", {"started_at": started, "finished_at": finished, "sqlite_version": sqlite3.sqlite_version, "user_version": version, "table_counts": counts, "database_sha256": db_hash, "database_size_bytes": db_size, "attachments": inventory, "attachment_consistency": "verified stable during copy; operator must establish immutable lifecycle"})
        directory_fd = os.open(package, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
        complete = True
        return {"status": "complete", "package": str(package), "user_version": version, "table_count": len(tables), "attachment_rows": len(inventory), "attachment_files": len(copied_paths), "database_bytes": db_size, "attachment_bytes": sum(size for _, size in copied_paths.values())}
    finally:
        if not complete:
            shutil.rmtree(package)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True)
    parser.add_argument("--attachment-root", required=True)
    parser.add_argument("--output-parent", required=True)
    parser.add_argument("--deadline-seconds", type=float, default=15)
    parser.add_argument("--local-test", action="store_true", help="Allow explicit synthetic fixture paths; never use for production")
    args = parser.parse_args()
    try:
        result = capture(args.source, args.attachment_root, args.output_parent, args.deadline_seconds, args.local_test)
    except Exception as exc:
        # SQLite and OS exceptions may include SQL, paths or data: suppress them.
        reason = str(exc) if isinstance(exc, CaptureError) else type(exc).__name__
        print(json.dumps({"status": "failed", "reason": reason}), file=sys.stderr)
        return 1
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
