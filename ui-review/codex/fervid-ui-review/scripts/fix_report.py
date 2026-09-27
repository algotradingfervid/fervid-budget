#!/usr/bin/env python3
"""Validate before/after closure evidence and build separate fix reports."""
import argparse
from collections import Counter
import html
import json
from pathlib import Path
import sys
from urllib.parse import quote

STATUSES = {"fixed", "partially-fixed", "unresolved", "deferred"}


def evidence_path(run, value):
    if not isinstance(value, str) or not value or Path(value).is_absolute():
        raise ValueError(f"Evidence must be a relative path: {value!r}")
    path = (run / value).resolve()
    if not path.is_relative_to(run.resolve()) or not path.is_file():
        raise ValueError(f"Missing or out-of-run evidence: {value}")
    return path


def validate(run, audit, ledger, require_complete=False):
    findings = list(audit.get("findings", []))
    for path in sorted(run.glob("final-findings-*.json")):
        chunk = json.loads(path.read_text())
        findings += chunk.get("findings", []) if isinstance(chunk, dict) else chunk
    if any(f.get("verdict") not in {"confirmed", "adjusted"} for f in findings):
        raise ValueError("Fix closure requires verified audit findings")
    ids = [f["id"] for f in findings]
    if len(ids) != len(set(ids)):
        raise ValueError("Duplicate audit finding IDs")
    entries = ledger.get("fixes", [])
    fix_ids = [f.get("findingId") for f in entries]
    if len(fix_ids) != len(set(fix_ids)) or set(ids) != set(fix_ids):
        raise ValueError("Exactly one fix disposition is required for every audit finding")
    for entry in entries:
        ident = entry["findingId"]
        status = entry.get("status")
        if status not in STATUSES or not entry.get("summary"):
            raise ValueError(f"{ident}: valid status and summary required")
        for key in ("before", "after"):
            for value in entry.get(key, []):
                evidence_path(run, value)
        for key in ("acceptanceChecks", "regressionChecks"):
            for check in entry.get(key, []):
                if check.get("result") not in {"pass", "fail", "blocked"}:
                    raise ValueError(f"{ident}: invalid check result")
                for value in check.get("evidence", []):
                    evidence_path(run, value)
        if status != "fixed":
            if not entry.get("remaining"):
                raise ValueError(f"{ident}: non-fixed finding requires remaining explanation")
            if require_complete:
                raise ValueError(f"{ident}: still {status}")
            continue
        verifier = entry.get("verification", {})
        if not entry.get("changedFiles") or not entry.get("before") or not entry.get("after"):
            raise ValueError(f"{ident}: fixed requires changed files and before/after evidence")
        if (verifier.get("result") != "pass" or verifier.get("method") != "rendered-browser"
                or verifier.get("independent") is not True or not verifier.get("reviewer")
                or not verifier.get("details")):
            raise ValueError(f"{ident}: fixed requires independent rendered verification")
        for key in ("acceptanceChecks", "regressionChecks"):
            checks = entry.get(key, [])
            if not checks or any(c.get("result") != "pass" or not c.get("evidence") for c in checks):
                raise ValueError(f"{ident}: fixed requires passing evidenced {key}")
        if entry.get("remaining"):
            raise ValueError(f"{ident}: fixed cannot have unresolved acceptance work")
    return entries


def render(ledger, entries):
    esc = lambda x: html.escape(str(x))
    counts = Counter(e["status"] for e in entries)
    summary = ", ".join(f"{key}: {value}" for key, value in sorted(counts.items())) or "No findings"
    md = ["# UI fix evidence", "", summary, "", str(ledger.get("environment", "")), "", "[Original audit](report.html)", ""]
    body = [f"<h1>UI fix evidence</h1><p>{esc(summary)}</p><p>{esc(ledger.get('environment', ''))}</p><p><a href='report.html'>Original audit</a></p>"]
    for entry in entries:
        heading = f"{entry['findingId']} · {entry['status']}"
        md += [f"## {heading}", "", entry["summary"], ""]
        body += [f"<article><h2>{esc(heading)}</h2><p>{esc(entry['summary'])}</p>"]
        if entry.get("changedFiles"):
            text = ", ".join(entry["changedFiles"])
            md += [f"Changed files: {text}", ""]
            body += [f"<p>Changed files: {esc(text)}</p>"]
        body += ["<div class='comparison'>"]
        for kind in ("before", "after"):
            body += [f"<section><h3>{kind.title()}</h3>"]
            for path in entry.get(kind, []):
                url = quote(path, safe="/")
                md += [f"![{kind} evidence]({url})", ""]
                body += [f"<a href='{esc(url)}'><img src='{esc(url)}' alt='{kind} evidence' loading='lazy'></a>"]
            body += ["</section>"]
        body += ["</div>"]
        for kind in ("acceptanceChecks", "regressionChecks"):
            for check in entry.get(kind, []):
                label = f"{check.get('criterion', check.get('name', kind))}: {check['result']}"
                links = [(quote(p, safe="/"), p) for p in check.get("evidence", [])]
                md += [f"- {label}. " + ", ".join(f"[{p}]({url})" for url, p in links)]
                body += [f"<p>{esc(label)}. " + ", ".join(f"<a href='{esc(url)}'>{esc(p)}</a>" for url, p in links) + "</p>"]
        if entry.get("verification"):
            v = entry["verification"]
            text = f"Verification: {v.get('reviewer', 'unspecified')} — {v.get('result', '')}. {v.get('details', '')}"
            md += ["", text, ""]
            body += [f"<p>{esc(text)}</p>"]
        if entry.get("remaining"):
            md += ["", "Remaining: " + entry["remaining"], ""]
            body += [f"<p><strong>Remaining:</strong> {esc(entry['remaining'])}</p>"]
        body += ["</article>"]
    page = "<!doctype html><html lang='en'><meta charset='utf-8'><meta name='viewport' content='width=device-width,initial-scale=1'><title>UI fix evidence</title><style>body{font:16px/1.6 system-ui;margin:2rem auto;padding:0 1rem;max-width:1200px;color:#17212b}article{border-top:1px solid #9ca3af;margin-top:2rem}.comparison{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:1rem}img{max-width:100%;height:auto}a{color:#005ea8}p{overflow-wrap:anywhere}@media(max-width:640px){.comparison{grid-template-columns:1fr}}</style><main>" + "".join(body) + "</main></html>"
    return page, "\n".join(md)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run", type=Path)
    parser.add_argument("--require-complete", action="store_true")
    args = parser.parse_args()
    run = args.run.expanduser().resolve()
    try:
        audit = json.loads((run / "final.json").read_text())
        ledger = json.loads((run / "fixes.json").read_text())
        entries = validate(run, audit, ledger, args.require_complete)
        page, markdown = render(ledger, entries)
        (run / "fix-report.html").write_text(page)
        (run / "fix-report.md").write_text(markdown)
    except (ValueError, KeyError, TypeError, OSError) as error:
        print(f"Fix report validation failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps({"html": str(run / "fix-report.html"), "md": str(run / "fix-report.md"), "counts": dict(Counter(e['status'] for e in entries))}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
