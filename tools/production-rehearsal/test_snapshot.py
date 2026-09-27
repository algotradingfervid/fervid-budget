"""Synthetic-only tests; all fixtures stay under the private QA workspace."""

import importlib.util
import json
import os
from pathlib import Path
import sqlite3
import tempfile
import time
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("snapshot", Path(__file__).with_name("snapshot.py"))
snapshot = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(snapshot)
PRIVATE = Path.home() / "Library/Application Support/Fervid Budget QA/production-rehearsal-2026-09-26"


class CaptureTests(unittest.TestCase):
    def setUp(self):
        PRIVATE.mkdir(parents=True, mode=0o700, exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(prefix="synthetic-snapshot-", dir=PRIVATE)
        self.root = Path(self.temp.name)
        self.source = self.root / "source.db"
        self.attachments = self.root / "attachments"
        self.attachments.mkdir(mode=0o700)
        self.output = self.root / "staging"
        self.conn = sqlite3.connect(self.source)
        self.conn.executescript("CREATE TABLE payments(id INTEGER PRIMARY KEY, amount INTEGER); CREATE TABLE payment_attachments(id INTEGER PRIMARY KEY,payment_id INTEGER REFERENCES payments(id),stored_path TEXT,size_bytes INTEGER); INSERT INTO payments VALUES(1,12345);")
        (self.attachments / "synthetic.bin").write_bytes(b"synthetic fixture")
        self.conn.execute("INSERT INTO payment_attachments VALUES(1,1,?,17)", (str(self.attachments / "synthetic.bin"),))
        self.conn.commit()

    def tearDown(self):
        self.conn.close()
        self.temp.cleanup()

    def capture(self, **kw):
        return snapshot.capture(self.source, self.attachments, self.output, local_test=True, **kw)

    def assert_clean_failure(self):
        self.assertEqual(list(self.output.iterdir()), [])

    def test_delete_snapshot_preserves_source_and_filters_unreferenced_files(self):
        original = self.source.read_bytes()
        (self.attachments / "unreferenced.bin").write_bytes(b"not copied")
        result = self.capture()
        self.assertEqual(self.source.read_bytes(), original)
        self.assertEqual(self.conn.execute("PRAGMA journal_mode").fetchone()[0], "delete")
        package = Path(result["package"])
        self.assertEqual(result["attachment_rows"], 1)
        self.assertEqual(result["attachment_files"], 1)
        self.assertEqual(list((package / "attachments").iterdir()), [package / "attachments/synthetic.bin"])
        self.assertEqual(os.stat(package).st_mode & 0o777, 0o700)
        self.assertEqual(os.stat(package / "fervid.db").st_mode & 0o777, 0o600)
        self.assertEqual(json.loads((package / "capture-evidence.json").read_text())["table_counts"]["payments"], 1)

    def test_wal_committed_data_included_uncommitted_data_excluded(self):
        self.conn.execute("PRAGMA journal_mode=WAL")
        self.conn.execute("PRAGMA wal_autocheckpoint=0")
        self.conn.execute("INSERT INTO payments VALUES(2,200)")
        self.conn.commit()
        self.conn.execute("INSERT INTO payments VALUES(3,300)")
        result = self.capture()
        with sqlite3.connect(Path(result["package"]) / "fervid.db") as copied:
            self.assertEqual(copied.execute("SELECT id FROM payments ORDER BY id").fetchall(), [(1,), (2,)])
        self.assertEqual(self.conn.execute("PRAGMA journal_mode").fetchone()[0], "wal")
        self.conn.rollback()

    def test_busy_source_hits_deadline_and_cleans_package(self):
        self.conn.execute("BEGIN EXCLUSIVE")
        started = time.monotonic()
        with self.assertRaises(snapshot.CaptureError):
            self.capture(deadline_seconds=0.15)
        self.assertLess(time.monotonic() - started, 2)
        self.assert_clean_failure()
        self.conn.rollback()

    def test_missing_attachment_cleans_package(self):
        (self.attachments / "synthetic.bin").unlink()
        with self.assertRaises(OSError):
            self.capture()
        self.assert_clean_failure()

    def test_wrong_size_cleans_package(self):
        (self.attachments / "synthetic.bin").write_bytes(b"wrong")
        with self.assertRaises(snapshot.CaptureError):
            self.capture()
        self.assert_clean_failure()

    def test_symlink_rejected(self):
        target = self.attachments / "synthetic.bin"
        target.unlink()
        target.symlink_to(self.source)
        with self.assertRaises(OSError):
            self.capture()
        self.assert_clean_failure()

    def test_ancestor_symlink_rejected(self):
        (self.attachments / "link").symlink_to(self.root, target_is_directory=True)
        self.conn.execute("UPDATE payment_attachments SET stored_path=?", (str(self.attachments / "link/source.db"),))
        self.conn.commit()
        with self.assertRaises(OSError):
            self.capture()
        self.assert_clean_failure()

    def test_escape_rejected(self):
        self.conn.execute("UPDATE payment_attachments SET stored_path=?", (str(self.root / "outside.bin"),))
        self.conn.commit()
        with self.assertRaises(snapshot.CaptureError):
            self.capture()
        self.assert_clean_failure()

    def test_foreign_key_violation_rejected(self):
        self.conn.execute("UPDATE payment_attachments SET payment_id=999")
        self.conn.commit()
        with self.assertRaises(snapshot.CaptureError):
            self.capture()
        self.assert_clean_failure()

    def test_mutation_during_copy_rejected(self):
        original = snapshot.hash_fd
        called = 0

        def mutate(fd, deadline, output=None):
            nonlocal called
            result = original(fd, deadline, output)
            called += 1
            if called == 1:
                (self.attachments / "synthetic.bin").write_bytes(b"changed fixture!!")
            return result

        with mock.patch.object(snapshot, "hash_fd", mutate):
            with self.assertRaises(snapshot.CaptureError):
                self.capture()
        self.assert_clean_failure()

    def test_production_allowlist_rejects_synthetic_paths(self):
        with self.assertRaises(snapshot.CaptureError):
            snapshot.capture(self.source, self.attachments, self.output)
        self.assertFalse(self.output.exists())


if __name__ == "__main__":
    unittest.main()
