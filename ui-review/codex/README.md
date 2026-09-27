# Codex UI review

The reusable source is [`fervid-ui-review/SKILL.md`](fervid-ui-review/SKILL.md). It adapts the UI review roles for Codex and includes measured browser tooling, an audit report builder, and a separate fix-evidence report builder. It has no dependency on the original agent runtime or its hooks.

Install or update the personal skill from the repository root:

```sh
python3 ui-review/codex/install.py
```

The installer copies the packaged skill to `${CODEX_HOME:-~/.codex}/skills/fervid-ui-review`. Start a new Codex task if the current task's skill catalog has not refreshed. Invoke `$fervid-ui-review`, or link the repository's SKILL.md directly. Example: “Use $fervid-ui-review to review this application thoroughly, then fix confirmed issues and provide before/after evidence.”

Browser tooling needs Python with Playwright, Chromium, and Pillow; use a separate runtime rather than modifying application dependencies. The bundled axe script retains its upstream license. Both report builders use Python's standard library. Audit results are a bounded review, not an accessibility certification; the reports retain coverage limits and rejected findings.
