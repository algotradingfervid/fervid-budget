# Findings — H · the UI at realistic data volume

One finding, and it was found by accident, which is the interesting part.

Every other spec in this repository — shipped and audit alike — runs against a
database holding a handful of rows. `tests/e2e/ux.spec.ts` is the project's
quality gate and it is emphatic about horizontal scroll: *"Fails on horizontal
scroll … Do not weaken it to pass — fix the screen."* But it only ever sees the
four seeded roles. Nothing in the suite asked what any screen does once an
organisation has actually used the product.

The defect surfaced when the whole audit suite was first run against **one**
shared database. `ux.spec.ts` failed with `/roles: scrolls sideways by 806px`.
No audit test had touched the roles screen; the audit had merely created a few
custom roles in the course of testing permissions, and the roles matrix grows a
column per role. The shipped gate had been passing for eight phases because the
data was never allowed to get realistic.

`tests/e2e/audit-h-scale.spec.ts` now pins it deliberately.

| ID | Claim | Severity | Confidence |
|---|---|---|---|
| F-H-01 | The roles matrix scrolls the page sideways once a handful of roles exist — at 1440 px and far worse at 390 px | **high** | confirmed |

---

### F-H-01 — The roles matrix outgrows the page once an organisation has more than a few roles

- **Severity** — high
- **Confidence** — confirmed (three ways: observed as a `ux.spec.ts` failure during a shared-database run, then reproduced deliberately at both widths with a measured overflow figure)
- **Type** — UX / spec-divergence
- **Where** — the roles matrix template, `internal/app/templates.go` (the `/roles` screen); measured against the project's own gate in `tests/e2e/ux.spec.ts:54`, which computes `document.documentElement.scrollWidth - window.innerWidth` and fails on anything above zero
- **Traces to** — TC-H-001, TC-H-003 · coverage `R1`, `R2`, `R5`, `R8` (the roles matrix is how every one of those requirements is operated) · UC-A-10…17
- **What happens** — The matrix lays a column out per role. With the seeded four it fits. Adding eight more — one per department, which is the arrangement the whole feature exists to support — makes the **page** scroll sideways, not merely the table:
  - at **1440 × 900**: the page overflows by a non-zero margin, first observed as `ux.spec.ts` reporting `806px`;
  - at **390 × 844** with roles accumulated across the file: **3024 px**.
- **Why it is wrong** — Three reasons, in increasing order of seriousness.
  1. It violates the project's own stated gate. `ux.spec.ts` forbids horizontal
     page scroll on every navigable screen and instructs that the screen be fixed
     rather than the test weakened. `/roles` passes that gate today only because
     the test database never holds enough roles to break it.
  2. The design system already solves this everywhere else. Wide tables sit
     inside a horizontally scrollable wrapper so the table scrolls and the page
     does not — `.table-wrap`, used by the payments ledger, the requests list and
     the recoverables register. The roles matrix does not get that treatment, so
     the whole page moves and the navigation chrome moves with it.
  3. It scales with use. The overflow is proportional to the number of roles, so
     the screen degrades exactly as an organisation adopts the feature. An
     administrator with twelve roles is not an edge case; requirement `R5`
     ("create role by copying") actively encourages arriving there.
- **Reproduction** — from a signed-in administrator session:
  1. `POST /roles/new` eight times with distinct names (or press ＋ New role eight
     times on `/roles`).
  2. Set the viewport to 1440 × 900 and open `/roles`.
  3. Evaluate `document.documentElement.scrollWidth - window.innerWidth` — it is
     greater than zero, and the page can be scrolled right.
  4. Set the viewport to 390 × 844 and reload. The overflow is far larger.
- **Impact** — The screen through which **all** permission administration happens
  becomes progressively unusable as the permission model is actually used. On a
  phone it is unusable well before that. Because the page rather than a wrapper
  scrolls, the sidebar and the mobile tab bar slide out of view with the content.
  There is no data loss and no security consequence: TC-H-002 confirms every role
  created is still present and editable, so this is a layout fault, not a
  functional one — which is why it is high and not critical.
- **Evidence** —
  - The original accidental observation: `ux.spec.ts` › *"every navigable screen
    is reachable, fits, and is accessible"* → `checked 19 screens: /roles: scrolls
    sideways by 806px`, during a run where the audit specs shared one database.
  - TC-H-001, annotated `test.fail()`, at 1440 px with twelve roles.
  - TC-H-003, annotated `test.fail()`: `/roles at 390px must not scroll the page
    sideways (ux.spec.ts forbids it), but it overflows by 3024px`.
  - TC-H-002 passes, proving the roles themselves are all present and editable.
- **Suggested direction** — Wrap the matrix in the `.table-wrap` pattern the rest
  of the design system uses, so the matrix scrolls within its own box and the page
  does not. That alone satisfies the gate at both widths. Worth considering
  separately: with a dozen roles a role-per-column matrix is hard to read even
  when it fits, so a per-role editor (or a transposed layout with resources as
  columns) may be the better long-term answer — but that is a design decision,
  not this fix.

---

## A note on why this area exists at all

This finding is the argument for the area. The audit's other seven areas each
interrogate a feature; this one interrogates an assumption — that a screen which
fits with seed data fits with real data. One accident produced one high-severity
finding, and the same question has not been asked of any other screen.

**Not yet asked, and worth a future pass:** the requests list at 200+ rows (area
B already found a silent 200-row cap there, `F-B-16`); the users screen with
dozens of users; the audit log after months of writes; the payments ledger across
a full year; the variance grid with many projects and heads; the notification
centre past its 100-row limit (area G recorded that as `F-G-037`, unverified).
Each is the same shape of question and none of them is covered by the shipped
suite.
