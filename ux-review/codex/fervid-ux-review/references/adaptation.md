# Adaptation provenance

Adapted from the user's `ux-review/skill/SKILL.md`, seven `ux-review/agents/*.md` roles, and existing local UX/UI toolkit. Preserved independent mapping, product analysis, screen/device cells, cognitive walkthroughs, discoverability hunting, independent verification, and editorial deduplication.

Changed for Codex and this assignment:
- Collaboration tools replace Claude agent types, fixed model names, tool allowlists, hooks, and a fixed twenty-agent limit. Capacity is discovered at runtime and agents are queued.
- No claim that a guard hook enforces isolation. Read-only scope is an explicit task constraint; safe sessions and evidence discipline support it.
- Routine owner check-ins are replaced by researched provisional decisions. Private intent stays unknown unless the owner stated it. Structural proposals are never marked “confirmed with you.”
- Real-data mutation permissions and test email flows are removed from this review-only mode. The task can be assessed through the commit boundary; unsaved checks do not prove persistence.
- Fixed rules such as “desktop hamburger is always a defect,” “maximum two disclosure levels,” and automatic severity from menu depth are replaced by context-sensitive evaluation. Linked checklist claims must be checked at the actual source before using quantitative claims.
- Accessibility heuristics and automated measurement are separated from verified standards failures; no full WCAG certification is implied.
- Research/question traceability, dispositions, report checks, and a bounded completion criterion replace an unbounded autonomous loop.

`uirev.py` is copied from the user's existing toolkit without Claude dependencies. `report.py` is adapted from the user's existing report builder; `axe.min.js` retains its embedded upstream licensing. The skill folder can be copied between projects or into `~/.codex/skills/fervid-ux-review`; no original `.claude` path is required at runtime.
