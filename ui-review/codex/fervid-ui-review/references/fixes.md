# Implementation and closure evidence

Use this only when the user requests fixes. Preserve `final.json`, verified candidates and original screenshots as the audit baseline. Keep source-assisted diagnosis, implementation and after evidence separate from rendered discovery.

## Workflow

For each confirmed/adjusted finding:

1. Define observable acceptance criteria from the original outcome. Record the original screenshot paths and exact role/state/device. Group shared causes where appropriate, but preserve every finding ID.
2. Inspect source and existing tests. Fix the confirmed behavior, preserving unrelated local changes. Do not silently redesign a larger workflow because a cosmetic change seems attractive. If private intent is necessary, finish independent fixes and document the unresolved decision.
3. Run relevant existing tests, build or lint checks; add targeted regression coverage for substantive behavior when it catches a meaningful failure. Record command, outcome and artifact. Code compilation alone does not demonstrate UI success.
4. Rebuild or restart the isolated app so the browser serves changed code. Have an independent verifier reproduce each acceptance criterion with fresh sessions and synthetic fixtures, using the same viewport/theme/role. Repeat measurements; compare after screenshots with before. Check affected adjacent flows and devices.
5. Keep failures in the ledger and iterate. Fixed means all stated acceptance criteria passed, before/after screenshots exist, rendered verification passed and appropriate regression checks passed. Otherwise use partially-fixed, unresolved or deferred, and state why. “Deferred” is transparent remaining work, not permission to abandon feasible authorized fixes.
6. Generate and inspect the fix report. Preserve remaining limitations in the final response. Never claim all issues fixed if the strict report validation fails.

## `RUN/fixes.json`

Every final audit finding must have exactly one record. Paths are relative files contained inside RUN; symlink escape and parent traversal are rejected. Put test logs under `measurements/` and new screenshots under `shots/after/`. The script validates evidence presence and declared outcomes; a human or agent must actually inspect artifacts and behavior.

```json
{
  "environment": "isolated synthetic fixture; Chromium; revision and date",
  "fixes": [{
    "findingId": "UX-001",
    "status": "fixed",
    "summary": "Primary action remains visible at 390px width.",
    "changedFiles": ["web/static/app.css"],
    "before": ["shots/before/UX-001.png"],
    "after": ["shots/after/UX-001.png"],
    "acceptanceChecks": [{"criterion": "Save is visible and operable at 390px", "result": "pass", "evidence": ["measurements/UX-001-after.json"]}],
    "verification": {"result": "pass", "method": "rendered-browser", "independent": true, "reviewer": "fix-verifier", "details": "Fresh session; same role/state; keyboard and pointer activation tested."},
    "regressionChecks": [{"name": "Relevant existing tests and adjacent browser flow", "result": "pass", "evidence": ["measurements/regression.txt"]}],
    "remaining": ""
  }]
}
```

For non-fixed entries, include `findingId`, `status`, `summary` and a nonempty `remaining` explanation; include evidence and checks already obtained. Status must be `fixed`, `partially-fixed`, `unresolved` or `deferred`. Checks use `pass`, `fail` or `blocked`.

```sh
python3 <skill>/scripts/fix_report.py RUN
python3 <skill>/scripts/fix_report.py RUN --require-complete
```

The second invocation fails if any finding remains non-fixed. This does not replace review judgment or provide a technical guarantee of independence. Link the original audit and the fix report together; do not rewrite the baseline to make an issue disappear.
