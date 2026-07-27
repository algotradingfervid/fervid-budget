# docs/qa — models, use cases, test cases and audit results

A UML model of the shipped system, 133 use-case specifications, ~564 executed
test cases, and the defect register they produced. Built 2026-07-27 against
commit `30edd6a`, after all eight build phases were complete and both suites
were green.

**Start here:** [`results/AUDIT-REPORT.md`](results/AUDIT-REPORT.md) — the
consolidated findings, deduplicated across areas and ranked by severity, with a
suggested order of repair grouped by root cause.

**Then:** [`results/REPAIR-LOG.md`](results/REPAIR-LOG.md) — what was *done* about
each finding, and the decisions taken along the way. The audit report is a
snapshot of `30edd6a` and is not updated as defects are fixed; the repair log is
the current status. [`results/FIX-PLAN.md`](results/FIX-PLAN.md) is the wave
layout and file ownership the repair follows.

---

## The rule this documentation was built on

**The code is the source of truth; the specs are the intent.** Where they
disagree the code is what ships, so a divergence is recorded, never silently
reconciled. Every load-bearing claim cites `file:line`. Anything that could not
be confirmed from source is marked `UNVERIFIED` rather than smoothed over.

That rule is why these documents disagree with
`docs/superpowers/specs/` and with `PROGRESS.md` in several places — see the
"Documentation that is now wrong" table in the audit report. The specs remain the
record of what was intended; this directory records what exists.

---

## Layout

```
uml/          the structural and behavioural model
use-cases/    133 detailed use-case specifications
test-cases/   the test cases, with expected result written before the run
results/      the audit report and the per-area findings
```

### `uml/`

| File | What it holds |
|---|---|
| `01-domain-model.md` | Four ER diagrams by subject area, 22 tables, 27 indexes with what each one *prevents*, Go class diagrams, every enumeration, 15 invariants with their enforcement point, and a divergence log |
| `02-state-machines.md` | The request lifecycle over its **eleven** live statuses, `on_hold` as an orthogonal region, the reservation lock, the cancellation sub-machine, settlement, reminders — plus a from×to legality grid and the dead transitions |
| `03-sequence-diagrams.md` | 19 sequence diagrams from login to reporting, each naming real handlers and real methods, ending in a hop-by-hop information-flow table |
| `04-use-case-diagram.md` | Seven actors, eight use-case diagrams, an inventory covering all 97 route registrations, and an actor × use-case matrix filled from the seeded grant sets |
| `05-route-permission-matrix.md` | 97 routes × 5 callers with expected statuses, the ownership overlay, a reverse index over all 66 grant pairs, and 40 privilege-escalation probes |
| `06-deployment-and-components.md` | Package dependency graph, startup sequence, configuration surface, deployment topology, test architecture, operational risks |

Diagrams are Mermaid and were rendered with `mermaid-cli` to confirm they parse.
GitHub and most Markdown viewers render them inline.

### `use-cases/`

`UC-A` platform, RBAC and administration (46) · `UC-B` requests and approvals
(42) · `UC-C` settlement, recoverables and notifications (45).

Each use case carries the full template: goal, primary and supporting actors,
trigger, routes, permission gate, coverage IDs, priority, numbered
preconditions, success and failure postconditions, a main scenario naming real
controls and exact field labels, alternate flows, exception flows, business rules
cited to `file:line`, data touched, UX notes, and open questions.

Audit areas: **A** RBAC and permission enforcement · **B** request lifecycle and
validation · **C** approvals and cancellation · **D** linking, reservation and
settlement · **E** recoverables · **F** notifications and reminders ·
**G** information-flow integrity · **H** the UI at data volume.

### `test-cases/`

`TC-A`–`TC-G`, one per audit area. Every case records `traces to` (use-case ID +
coverage-matrix ID), type, priority, preconditions, steps, **expected result
written before the run**, actual result, and verdict. That ordering matters: an
expectation written after seeing the output only documents current behaviour and
can never find a defect.

### `results/`

`AUDIT-REPORT.md` plus `findings-a`…`findings-g`. Each finding gives severity,
confidence, type, location, traceability, what happens, why it is wrong, a
replayable reproduction, impact, evidence, and a suggested direction — not a patch.

---

## Running the suites

There are two, and they are deliberately separate.

```sh
make test-e2e     # the shipped suite: 163 passed / 49 skipped / 0 failed
make test-audit   # the audit suite: one server and one database per area
```

### Why they are separate

**The audit cannot share a database with the shipped specs.** It builds a world
in order to interrogate it — custom roles, dozens of single-role users, 214
requests for the pagination case. The shipped specs assume something close to a
freshly seeded database.

This was discovered by doing it wrong. Running everything through one server made
`ux.spec.ts` fail with `/roles: scrolls sideways by 806px` — not because any audit
test touched that screen, but because the audit had created a few roles and the
roles matrix grows a column per role. Two conclusions followed: the audit needs
per-area isolation, and **the roles matrix has a real layout defect** that eight
phases of a green suite never surfaced because the data was never realistic. That
is finding `F-H-01`, and area H exists to pin it.

So `playwright.config.ts` carries `testIgnore: /audit-.*\.spec\.ts/` and
`make test-e2e` is exactly what it always was. The audit lives in
`playwright.audit.config.ts`, driven by `scripts/run-audit.sh`, which gives each
area its own port, server and SQLite file — which is also the only configuration
in which any number in this directory was ever measured.

### Running one area

```sh
scripts/run-audit.sh a d g            # only those areas
FERVID_AUDIT_JOBS=1 scripts/run-audit.sh   # sequential, for a smaller machine
FERVID_AUDIT_PROJECT=mobile-chrome scripts/run-audit.sh d
```

Or directly:

```sh
FERVID_E2E_PORT=4301 npx playwright test -c playwright.audit.config.ts \
  tests/e2e/audit-a-rbac-permissions.spec.ts --project=chromium --reporter=line
```

Do **not** run every audit spec through one invocation of the audit config: they
would share the one `webServer` and one database, which is the situation the
split exists to prevent. Use the script.

Both configs key the port, the test-results directory and the HTML report off
`FERVID_E2E_PORT`; the runtime directory is already per-process. With it unset,
`playwright.config.ts` is byte-for-byte the original single runner on 4173.

### `tests/e2e/audit-support.ts`

Shared helpers, and one trap worth knowing before writing any permission test:

**`store.CreateUser` gives every new user the Accounts system role.**
`assignDefaultRoleTx` (`internal/store/migrations.go:501`) grants Accounts unless
the legacy role field is literally `admin`, and the "＋ Add user" sheet posts no
`role_ids`. So a fresh user already holds all eight payment verbs and request
scope `all`. The edit sheet pre-checks the current set and `SetUserRoles` DELETEs
before re-inserting, so ticking "Manager" yields **Accounts + Manager** — which is
what `fixtures.createApproverUser` does. For a permission probe that is useless:
the subject holds two roles and every refusal you expected becomes a 200.

`createUserWithExactRoles` unchecks every box first, so a subject holds precisely
the grants under test. Pass `[]` for a signed-in user with no role at all — the
sharpest probe in the suite, because every route it still reaches is a route with
no effective gate.

`probeGet`/`probePost` do **not** follow redirects. A gated route answers an
anonymous caller `303 → /login`, and a client that follows it reports `200` for
the login page, turning a refusal into an apparent success.

---

## How a known defect is represented in code

> **This section describes the audit pass, which is over.** The instruction
> *report, do not repair* was reversed, and four repair waves have since changed
> application code — so "no application code changed" is true of `30edd6a` and
> false of the working tree. As each defect is fixed its annotation is
> **removed**, which is the whole point of the scheme below. The count has
> therefore fallen from roughly 35 at the audit to **15 `test.fail()` and 3
> `test.fixme()`** across `tests/e2e/audit-*.spec.ts` as of this writing, and it
> is still falling — count them rather than trusting this figure, and read
> [`results/REPAIR-LOG.md`](results/REPAIR-LOG.md) for what is actually fixed.
> Note also that areas A and G are **red on purpose** right now: assertions there
> recorded defective behaviour as truth without `test.fail()`, so they fail
> because the product improved.

The audit was instructed to report, not repair, so no application code changed.
A confirmed defect keeps its honest assertion and is annotated:

- `test.fail()` — the failure is deterministic. Playwright expects it, so the
  suite is green; the day the bug is fixed the test goes red with *"passed
  unexpectedly"*, which is the signal you want.
- `test.fixme()` — the case hangs, flakes, or is blocked by another defect.

No assertion was weakened, loosened or deleted to force a pass. Roughly 35 of the
~564 new cases were annotated at the audit; each names its finding ID in a
comment. See the note above for the current count.
