#!/usr/bin/env python3
"""Regression checks for fix-ledger evidence integrity and safe rendering."""
import copy
import json
from pathlib import Path
import tempfile
import unittest
import fix_report
import report


class EvidenceIntegrity(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="fervid-skill-check-")
        self.addCleanup(self.temp.cleanup)
        self.run = Path(self.temp.name)
        for name in ("before.png", "after.png", "checks.txt"):
            (self.run / name).write_bytes(b"fixture")
        self.audit = {"status": "final", "findings": [{"id": "UI-1", "severity": "medium", "verdict": "confirmed", "screenshots": ["before.png"]}]}
        self.ledger = {"fixes": [{"findingId": "UI-1", "status": "fixed", "summary": "<script>untrusted</script>", "changedFiles": ["web/app.css"], "before": ["before.png"], "after": ["after.png"], "acceptanceChecks": [{"criterion": "Visible action", "result": "pass", "evidence": ["checks.txt"]}], "regressionChecks": [{"name": "Flow regression", "result": "pass", "evidence": ["checks.txt"]}], "verification": {"result": "pass", "method": "rendered-browser", "independent": True, "reviewer": "verifier", "details": "Fresh browser reproduction"}}]}

    def test_valid_ledger_and_safe_html(self):
        entries = fix_report.validate(self.run, self.audit, self.ledger, True)
        page, markdown = fix_report.render(self.ledger, entries)
        self.assertNotIn("<script>untrusted</script>", page)
        self.assertIn("&lt;script&gt;", page)
        self.assertIn("after.png", markdown)
        report.validate(self.audit, str(self.run))

    def test_missing_or_duplicate_dispositions_fail(self):
        for entries in ([], self.ledger["fixes"] * 2):
            with self.assertRaises(ValueError):
                fix_report.validate(self.run, self.audit, {"fixes": entries})

    def test_missing_after_evidence_fails(self):
        (self.run / "after.png").unlink()
        with self.assertRaises(ValueError):
            fix_report.validate(self.run, self.audit, self.ledger)

    def test_source_only_or_nonindependent_claim_fails(self):
        for key, value in (("method", "source-only"), ("independent", False)):
            ledger = copy.deepcopy(self.ledger)
            ledger["fixes"][0]["verification"][key] = value
            with self.assertRaises(ValueError):
                fix_report.validate(self.run, self.audit, ledger)

    def test_failed_regression_cannot_be_fixed(self):
        self.ledger["fixes"][0]["regressionChecks"][0]["result"] = "fail"
        with self.assertRaises(ValueError):
            fix_report.validate(self.run, self.audit, self.ledger)

    def test_partial_disposition_allowed_but_not_complete(self):
        entry = self.ledger["fixes"][0]
        entry.update(status="partially-fixed", remaining="Mobile verification blocked")
        fix_report.validate(self.run, self.audit, self.ledger)
        with self.assertRaises(ValueError):
            fix_report.validate(self.run, self.audit, self.ledger, True)

    def test_path_and_symlink_escape_rejected(self):
        for path in ("../outside.txt", "/etc/hosts"):
            with self.assertRaises(ValueError):
                fix_report.evidence_path(self.run, path)
        (self.run / "escape").symlink_to("/etc/hosts")
        with self.assertRaises(ValueError):
            fix_report.evidence_path(self.run, "escape")

    def test_audit_rejects_unverified_final_finding(self):
        self.audit["findings"][0]["verdict"] = "unverified"
        with self.assertRaises(ValueError):
            report.validate(self.audit, str(self.run))


if __name__ == "__main__":
    unittest.main()
