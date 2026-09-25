# Toolkit and report interface

`TOOLKIT` is this skill's `scripts/uirev.py`. It uses Python, `playwright` (Chromium installed), and Pillow for screenshots; these are runtime dependencies, not product dependencies. Use an existing suitable runtime or isolated environment; do not modify the product's dependency files. Browser automation alternatives already available in the session are acceptable when they provide equivalent evidence.

```sh
python3 "$TOOLKIT" help
python3 "$TOOLKIT" start --run "$RUN" --session m-main --device desktop
python3 "$TOOLKIT" goto "$URL" --run "$RUN" --session m-main
python3 "$TOOLKIT" snapshot --run "$RUN" --session m-main
python3 "$TOOLKIT" stop --run "$RUN" --session m-main
```

Use `--state /absolute/auth.json` only when a state file exists; omit it for unauthenticated sessions. Desktop is 1440×900 CSS pixels, mobile 390×844, tablet 820×1180. These are emulated Chromium viewports, not native-device tests. Take a fresh `snapshot` after navigation or DOM replacement and before using element refs; stale refs can target a different control. `snapshot` supplies element refs; then use click/tap, hover, fill, select, press, scroll, text, hidden, layout, console, and shot as needed. Read `help` for precise syntax. `batch --file <steps.json>` avoids shell-quoting problems. Browser-evaluated code must measure rendered UI only; don't read application internals to substitute for user-observable evidence.

Screenshots: `shot NAME --mark 'REF:short explanation:high'` accepts refs/selectors/boxes and up to three marks. Prefer a crop with `--ref REF`. Inspect the output image and mark coordinates. Put personal identifiers in `RUN/redact.json` as `{"text": ["…"]}` before capturing. Keep auth state out of reports and repository commits.

`axe.min.js` is bundled for optional `axe` scans; retain its embedded upstream license notice. Automated accessibility scans are partial evidence, not a conformance certification. UI toolkit timing includes automation overhead; use it cautiously and distinguish it from application response instrumentation.

## Build

```sh
python3 <skill>/scripts/report.py /absolute/RUN
```

Reads `final.json`, optional `final-findings-*.json` (arrays or `{findings: [...]}`), and optional `research.json` / `decisions.json` (arrays, or wrapped under their matching key). Writes local `report.html` and `report.md`; screenshot files remain relative assets. For sharing, package these reports, final/research/decision JSON, and only the referenced screenshots while preserving their relative paths. Exclude browser `sessions`, auth state, fixture databases, and runtime logs. The report displays verification, acceptance criteria, research sources, and decisions. HTML source URLs permit only HTTP(S); content is escaped.

The original final schema remains supported. Extensions are documented in SCHEMA.md. No third-party Python package is needed to build reports. No answer form or implicit claim of owner approval is generated.
