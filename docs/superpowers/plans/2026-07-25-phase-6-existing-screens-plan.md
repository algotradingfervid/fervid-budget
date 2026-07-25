# Phase 6 — Existing Screens on the New Design System

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Bring the eleven pre-existing screens onto the new design system and make them work on a phone.

**Why this phase is small.** Phase 0 Task 2 aliases every legacy `--fb-*` token to the new palette, so all eleven screens recolour from the old green to terracotta with **no rule rewriting**. What remains is the work aliasing cannot do: the `.badge` → `.pill` rename, converting wide tables to the `t-cards` mobile restack, and the three screens that need genuinely new mobile markup.

**Architecture:** Template-only changes. No store or handler signature changes. Each task is one screen, a rendering test, and a mobile-width assertion.

**Tech Stack:** Go 1.25 `html/template`, Playwright.

## Global Constraints

- Phase 0 must be complete. This phase adds no CSS — every class it uses already exists.
- **No visual regression that is not intended.** Phase 0 Task 1 captured `output/playwright/baseline/`; every diff must be explainable as a deliberate change.
- Each screen must render without horizontal overflow at 390 px and must not be obscured by the fixed bottom tab bar.
- Money stays `int64` paise, formatted via `internal/money`.
- Commits: stage only your own files by explicit path; the tree contains unrelated modified files.
- TDD: write the assertion first, watch it fail, then change the template.

---

### Task 1: Global `.badge` → `.pill` rename

**Files:** Modify `internal/app/templates.go`; Test `internal/app/app_integration_test.go`

The design system standardises on `.pill`. `.badge` appears on every existing screen. This is one global rename, not eleven local ones.

- [ ] **Step 1: Write the failing test**

```go
func TestNoTemplateUsesLegacyBadgeClass(t *testing.T) {
	src, err := os.ReadFile("templates.go")
	if err != nil { t.Fatal(err) }
	if bytes.Contains(src, []byte(`class="badge`)) {
		t.Fatal(`templates still use class="badge"; the design system uses .pill`)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/app -run TestNoTemplateUsesLegacyBadgeClass -v` — expect FAIL.
- [ ] **Step 3:** Rename every `class="badge …"` to `class="pill …"`, mapping the modifiers: `good`→`good`, `danger`→`bad`, `neutral`→`neutral`, `locked`→`neutral`, and the grid statuses `not-paid`/`unbudgeted`/`under`/`on-track`/`over` to the matching pill tones. Keep the `.badge` CSS rules in place for one release so any missed instance degrades rather than breaks.
- [ ] **Step 4:** Green; all existing `internal/app` tests still pass.
- [ ] **Step 5: Commit** `refactor(templates): replace badge with pill`

---

### Task 2: Login screen

**Files:** Modify `internal/app/templates.go` (`login`); Test `internal/app/app_integration_test.go`

Match `mockups/screens/login.html`: `.login-stage` + `.login-card` with the `.lc-mark` brand block. `Chrome` is `"none"` — no sidebar, no tab bar.

- [ ] **Step 1: Write the failing test** — `GET /login` returns 200, contains `login-card`, and contains no `<aside`.
- [ ] **Step 2:** Fail. **Step 3:** Rewrite the template. **Step 4:** Green. **Step 5: Commit** `feat(templates): restyle login on the design system`

---

### Task 3: Variance grid — mobile accordion

**Files:** Modify `internal/app/templates.go` (`grid`); Test `tests/e2e/grid-mobile.spec.ts`

The widest screen in the app. Desktop keeps the sticky-column matrix; mobile gets the project accordion. One markup, both devices: the matrix carries `.d-only`, the accordion `.m-only`.

Accordion structure per `mockups/screens/variance-grid.html`: `.acc > .acc-item > .acc-head` (`.chev`, `.ah-main` with project name and head count, `.ah-amt` with remaining) and `.acc-body > .head-row` (`.hr-top`, `.hr-figs` with three `.hr-fig`, `.hr-bar` with a full-width `.vbar`).

The Committed column slot stays **reserved and hidden** — the client deferred it.

- [ ] **Step 1: Write the failing Playwright spec** — at 390 px, `.acc-item` count equals the project count, the matrix is not visible, tapping a header reveals its heads, and `document.documentElement.scrollWidth <= 390`.
- [ ] **Step 2:** Fail. **Step 3:** Add the accordion markup driven by the existing `.Grid.Groups` data — no store change. **Step 4:** Green. **Step 5: Commit** `feat(templates): add mobile accordion to the variance grid`

---

### Task 4: Payments ledger

**Files:** Modify `internal/app/templates.go` (`payments`); Test `tests/e2e/payments-mobile.spec.ts`

Convert the table to `table.t-cards` with `data-label` on every `<td>` and `.t-lead` on the date cell. Desktop `.toolbar` gains a mobile `.m-filters` counterpart. Historical payments (those with no linked request) render a `.pill.neutral` "Historical" so nobody mistakes them for unapproved spending.

- [ ] **Step 1: Write the failing spec** — at 390 px each row renders as a card with its labels visible and no horizontal overflow.
- [ ] **Step 2:** Fail. **Step 3:** Convert. **Step 4:** Green. **Step 5: Commit** `feat(templates): restack the payments ledger on mobile`

---

### Task 5: Budgets editor

**Files:** Modify `internal/app/templates.go` (`budgets`); Test `tests/e2e/budgets-mobile.spec.ts`

Desktop keeps the table. Mobile gets one `.card.panel-pad-sm` per head with a single amount input and a sticky `.action-bar` Save. Retired heads keep their disabled input and explanatory `.hint`.

- [ ] **Step 1: Write the failing spec** — at 390 px there is one card per head and the Save bar is reachable without horizontal scrolling.
- [ ] **Step 2:** Fail. **Step 3:** Add the `.m-only` card layout. **Step 4:** Green. **Step 5: Commit** `feat(templates): add mobile card layout to the budgets editor`

---

### Task 6: Monthly plans, reports, audit, backups

**Files:** Modify `internal/app/templates.go` (`months`, `reports`, `audit`, `backups`); Test `internal/app/app_integration_test.go`

All four are list screens with the same treatment: `table.t-cards` + `data-label`, `.pill` statuses, `.m-filters` on mobile, `<tfoot>` totals where the screen has them. Reports additionally gains the `.segmented` tab strip and a `.metric-strip` above the table.

- [ ] **Step 1: Write the failing test** — each of the four routes returns 200 and its table carries `t-cards`.
- [ ] **Step 2:** Fail. **Step 3:** Convert all four. **Step 4:** Green. **Step 5: Commit** `feat(templates): restack list screens on mobile`

---

### Task 7: Projects and heads masters

**Files:** Modify `internal/app/templates.go` (`projects`, `heads`); Test `internal/app/app_integration_test.go`

Match `mockups/screens/masters-projects.html` and `masters-heads.html`: `table.t-cards`, editing moves from inline row forms into an `.overlay > .sheet`, and the retire explanation renders as a `.banner.info`.

- [ ] **Step 1: Write the failing test** — both routes return 200, contain `t-cards`, and contain a `sheet` editor.
- [ ] **Step 2:** Fail. **Step 3:** Convert. **Step 4:** Green. **Step 5: Commit** `feat(templates): move masters editing into sheets`

---

### Task 8: Error and empty states

**Files:** Modify `internal/app/templates.go` (`error_page`); Test `internal/app/http_safety_test.go`

The error page renders with `Chrome: "none"`. Every list template gains a `.empty` state with an action, not a bare "no rows" line — an empty screen is an invitation to act.

- [ ] **Step 1: Write the failing test** — a 404 renders the error template with no `<aside`; an empty payments list contains `class="empty"` and a link to create.
- [ ] **Step 2:** Fail. **Step 3:** Implement. **Step 4:** Green. **Step 5: Commit** `feat(templates): add empty states and chrome-less error page`

---

### Task 9: Visual regression review and full suite

**Files:** Modify `tests/e2e/baseline.spec.ts`

> **Hazard, learned the hard way in Phase 0:** `make test-e2e` re-runs `baseline.spec.ts`, which **overwrites** `output/playwright/baseline/` with post-change renders, destroying the comparison. Copy `baseline/` aside, or re-point the spec at an `after/` directory, **before** running the suite.

- [ ] **Step 1:** Copy `output/playwright/baseline/` to a safe location, then capture all eleven screens at 1440 px and 390 px into `output/playwright/phase6/`.
- [ ] **Step 2:** Diff against the saved baseline and review every delta by eye.
- [ ] **Step 3:** Every difference must be intended. Nothing missing, nothing overlapping, no horizontal overflow at 390 px, tab bar never covering content.
- [ ] **Step 4:** `make test-all` green.
- [ ] **Step 5: Commit** `test(e2e): verify existing screens on the design system`

---

## Self-review

- **Spec coverage:** UI/UX spec §7 "Mobile patterns for dense screens" maps to Tasks 3-6; the redraw requirement in §5 "Existing screens redrawn (9)" maps to Tasks 2-8.
- **No new CSS:** every class used here is delivered by Phase 0 Tasks 6-11. If a task needs a class that does not exist, that is a Phase 0 defect — fix it there, not here.
- **Ordering:** Task 1 must run first; the rename touches every other template this phase edits.
