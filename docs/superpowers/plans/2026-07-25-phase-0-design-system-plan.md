# Phase 0 — Design System & App Shell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

## Status — 2026-07-25

Tasks 1–17 and 19 are shipped. Task bodies are kept verbatim as the record of what was built; **do not re-execute them.** The `- [ ]` checkboxes below were never ticked during execution — this table, not the boxes, is the source of truth.

| Task | Commit | Task | Commit |
|---|---|---|---|
| 1 Visual baseline | `4322d37` | 11 Mobile chrome CSS | `3c095a1` |
| 2 Token block + aliases | `4394ca6` | 12 Permission accessors | `8d7f9fd` |
| 3 Retoken rule families | `d8d3ce3` | 13 Nav model | `e83087a` |
| 4 Collapse breakpoints | `c8ab2ef` | 14 Tab bar resolution | `1ad3030` |
| 5 Kitchen-sink fixture | `9002de3` | 15 Badge counts | `bb03be8` |
| 6 Shell layout CSS | `3f7725a` | 16 Shell into PageData | `db60566` |
| 7 Status CSS | `1176fc8` | 17 top/bottom templates | `e407794` |
| 8 Form CSS | `bf8c74f` | 18 Rewrite fervid-app.js | **in progress** |
| 9 List CSS | `8fffd51` | 19 money.InWords | `44cc095` |
| 10 Overlay + table CSS | `b520df8` | 20 Visual regression review | **pending** |

Two corrections landed after the tasks that introduced them:

- `6821cc8` — `.a-list` was scoped to `.area`, but two approved mockups place it inside `.card`. Unscoped.
- `ecf4b46` — Task 17 shipped a `PageData.Perms()` **method** that lazily rebuilt its own `auth.Manager`, because `app.go` was outside its file set. Replaced with a real `Perms store.PermissionSet` **field** resolved once in `renderStatus` from the same call that builds the shell. Also split the notification hrefs per adoption spec D7.

**Baseline hazard, already mitigated:** `make test-e2e` re-runs `baseline.spec.ts`, which overwrites `output/playwright/baseline/`. A verified-identical copy is preserved at `output/playwright/baseline-keep/`. Task 20 must diff against **`baseline-keep`**.

**Goal:** Port the approved mockup design system into the production application and replace the app shell, so every later phase renders approved screens without inventing markup.

**Architecture:** One stylesheet (`web/static/fervid-ds.css`) with a single `:root` token block; the legacy `--fb-*` names survive as aliases so the eleven existing screens reskin without rule rewriting. Responsiveness moves from the prototype's `html[data-device]` attribute to real media queries at 860 px and 560 px. The shell (sidebar groups, mobile top bar, bottom tab bar, More sheet) is rendered server-side from a **permission-driven** nav model — never from role names — and delivered through one new `PageData.Shell` field.

**Tech Stack:** Go 1.25 `html/template`, htmx, vanilla JS (`web/static/fervid-app.js`), SQLite, Playwright.

## Global Constraints

- **No role strings in the shell.** Nav, tab bar and every gated control derive from `PermissionSet.Can(resource, action)`. `grep -c 'eq .User.Role' internal/app/templates.go` must end at `0`.
- **Never port prototype-only code.** The persona switcher, device toggle, `qs()/screenHref()`, `rewriteLinks()`, `applyPersonaVisibility()` and the phone-bezel `.stage`/`.appshell` rules are prototype scaffolding. Client-side hiding of a permitted-only action leaks it; use server-side `{{if}}`.
- **`hidden` is not validation.** Every `[data-when]` reveal must be re-enforced in the Go handler.
- **Exactly one `:root`** in `fervid-ds.css` when this phase ends.
- **Three `@media (max-width: …)` blocks only:** 860, 560, print.
- Commits: stage only files you touched, by explicit path. The tree contains unrelated modified files — never `git add -A`.
- Money stays `int64` paise; `internal/money` is authoritative. Client-side formatting is display only.

---

## File structure

| File | Responsibility |
|---|---|
| `web/static/fervid-ds.css` | modify — single token block, component layer, three breakpoints |
| `web/static/fervid-app.js` | rewrite — accordion, overlay, `[data-when]`, money field, More sheet; keep `toggleProject` |
| `web/static/_kitchensink.html` | create — renders every component for visual + Playwright checking |
| `internal/app/nav.go` | create — `Shell`, `NavGroup`, `NavItem`, `TabBar`, `navSpec`, `buildShell`, `resolveTabs` |
| `internal/app/templates.go` | modify — `top`/`bottom` rewritten around the shell |
| `internal/app/app.go` | modify — `PageData.Shell` |
| `internal/app/http_errors.go` | modify — populate shell in `renderStatus`; skip on `HX-Request` |
| `internal/auth/auth.go` | modify — `Can`, `Permissions`, `RequirePermission` refactor |
| `internal/store/badges.go` | create — `BadgeCounts` single-statement query + cache |
| `internal/money/money.go` | modify — `InWords`, comma-tolerant parsing |

---

### Task 1: Visual baseline before any change

**Files:** Create `tests/e2e/baseline.spec.ts`

- [ ] **Step 1: Write the capture spec** — for each of the eleven existing routes (`/`, `/payments`, `/payments/new`, `/budgets`, `/months`, `/reports/monthly`, `/projects`, `/heads`, `/users`, `/audit`, `/backups`) screenshot at 1440×1200 and 390×850 into `output/playwright/baseline/`.
- [ ] **Step 2: Run** `make test-e2e` — expect 22 PNGs written.
- [ ] **Step 3: Commit** `test(e2e): capture pre-redesign visual baseline`

**Acceptance:** 22 files exist under `output/playwright/baseline/`.

---

### Task 2: Single token block with legacy aliases

**Files:** Modify `web/static/fervid-ds.css:6-34` and `:1221-1242`

**Interfaces:** Produces every `--*` custom property later tasks consume.

- [ ] **Step 1:** Replace the block at `:6-34` with `mockup.css:7-48` verbatim **minus** `--proto-h`. Take `--muted-2: #726a61` (the mockup value) where it conflicts with `#665f57`.
- [ ] **Step 2:** Append 26 alias lines inside the same `:root`:

```css
  /* Legacy aliases — every pre-redesign rule reads these. Do not delete. */
  --fb-bg: var(--paper);              --fb-surface: var(--surface);
  --fb-surface-2: var(--surface-2);   --fb-surface-3: var(--surface-3);
  --fb-text: var(--ink);              --fb-muted: var(--muted);
  --fb-subtle: var(--muted-2);        --fb-border: var(--line);
  --fb-border-strong: var(--line-2);  --fb-accent: var(--brand);
  --fb-accent-strong: var(--brand-2); --fb-accent-soft: var(--brand-tint);
  --fb-info: var(--info);             --fb-info-soft: var(--info-soft);
  --fb-warn: var(--warn);             --fb-warn-soft: var(--warn-soft);
  --fb-danger: var(--bad);            --fb-danger-soft: var(--bad-soft);
  --fb-success: var(--ok);            --fb-success-soft: var(--ok-soft);
  --fb-radius: var(--r);              --fb-radius-sm: var(--r-sm);
  --fb-font: var(--font-ui);          --fb-mono: var(--font-mono);
  --fb-focus: var(--focus);           --fb-shadow: var(--shadow-2);
```

- [ ] **Step 3:** Delete the second `:root` at `:1221-1242` entirely.
- [ ] **Step 4: Verify** `grep -c '^:root' web/static/fervid-ds.css` → `1`. Load `/` and confirm no element renders with an empty computed colour.
- [ ] **Step 5: Commit** `refactor(css): unify design tokens and alias legacy names`

---

### Task 3: Retoken the four hardcoded rule families

**Files:** Modify `fervid-ds.css` `.alert-*` (`:753-764`), `.badge.*` (`:794-841`), `.duepill.*` (`:2076-2110`), `.timeline-item.*` (`:2228-2243`)

- [ ] **Step 1:** Replace every literal hex in those blocks with the matching token.
- [ ] **Step 2: Verify** `grep -nE '#(116b5a|0a4f43|dcefeb|a33a35|277045|dff1e7|f9dfdc)' web/static/fervid-ds.css` returns nothing.
- [ ] **Step 3: Commit** `refactor(css): retoken alert, badge, duepill and timeline colours`

---

### Task 4: Collapse the breakpoints

**Files:** Modify `fervid-ds.css:1049, 1081, 1810, 2332, 2380, 2441`

Three sidebar-collapse rules currently fight at 1100 / 920 / 760 px.

- [ ] **Step 1:** Merge into `@media (max-width: 860px)` and `@media (max-width: 560px)`; keep the print block.
- [ ] **Step 2: Verify** `grep -c '@media (max-width' web/static/fervid-ds.css` → `2`; sidebar collapses exactly once as the viewport narrows.
- [ ] **Step 3: Commit** `refactor(css): collapse to a single responsive breakpoint pair`

---

### Task 5: Kitchen-sink fixture

**Files:** Create `web/static/_kitchensink.html`

- [ ] **Step 1:** Static page rendering every component added in Tasks 6-11, each under an `<h2>` matching its class name. No server dependency; opens directly and is served under `/static/`.
- [ ] **Step 2: Commit** `chore(css): add component kitchen sink fixture`

**Acceptance:** loads at `/static/_kitchensink.html`.

---

### Task 6: Shell layout CSS

**Files:** Modify `fervid-ds.css` (append "New components — layout")

Port from `mockup.css`: `.page-inner` (211), `.side-brand` (165-175), `.card-head`/`.card-body`/`.panel-pad-sm` (380-388), `.section-head` (348-355), `.greet` (362), `.stack-8/12/16`, `.row`, `.row-end`, `.num`, `.small`, `.nowrap` (366-374), `.side-nav a .n` and `.side-nav a.soon` (186-193).

`.m-only`/`.d-only` use a **complement query**, not `display: revert`:

```css
@media (min-width: 861px) { .m-only { display: none !important; } }
@media (max-width: 860px) { .d-only { display: none !important; } }
```

**Corrected during implementation.** This plan originally specified `display: revert` inside the 860 px block, on the theory that it would restore the element's own `grid`/`flex`. That was measured and found false: `revert` rolls the whole author origin back, so `.type-grid.m-only` returned `block`, not `grid`. The complement query hides from the viewport that should not see the element and never touches `display` on the one that should, so the element keeps its own layout. It sits on the same 860 px line, so the two-breakpoint constraint still holds.

- [ ] **Step 1:** Append the rules. **Step 2:** Add each to the kitchen sink. **Step 3:** Verify at 1440 and 390 px, no horizontal overflow. **Step 4: Commit** `feat(css): add shell layout components`

---

### Task 7: Status CSS — pills and the waiting-on line

**Files:** Modify `fervid-ds.css`

Port `.pill` + 20 modifiers (`mockup.css:459-496`) and `.waiting`/`.you`/`.done`/`.closed` (499-512).

**Collision:** the new `.banner` (605-622) collides with the existing `.banner, .alert, .locked-banner` at `ds:730-743`. Keep the existing selector list working; add the new tone modifiers and `.b-ico`/`.b-actions` alongside.

Carry over the two colour rules established during mockup review:
- `.pill.urgent` is a **solid** red fill with an `!` glyph via `::before` — as a tint it is indistinguishable from `.pill.approved` terracotta.
- `.waiting.closed` uses a neutral dash, never the green tick, for rejected and cancelled.

- [ ] **Step 1:** Append. **Step 2:** Kitchen sink shows all 12 statuses plus urgent and recoverable. **Step 3:** Confirm `.alert.error` / `.alert.success` render unchanged. **Step 4: Commit** `feat(css): add status pills and waiting-on line`

---

### Task 8: Form CSS

**Files:** Modify `fervid-ds.css`

Port field chrome (`label .req/.opt`, `.flabel`, `.err` — 657-664), `fieldset`/`legend` (682-687), `.money-field` (690-702), `.type-grid`/`.type-card` (705-730), `.choice` (733-749), `.combo` (752-775), `.uploader`/`.file-row` (778-795), `.action-bar` (798-812). Dedupe `.checkline` against `ds:2260`.

Replace the mockup's JS-driven radio-card highlight with CSS:

```css
.choice label:has(input:checked) { border-color: var(--brand); background: var(--brand-tint); box-shadow: 0 0 0 1px var(--brand); }
```

- [ ] **Step 1:** Append. **Step 2:** Kitchen sink. **Step 3:** Confirm the existing payment form diff is colour-only. **Step 4: Commit** `feat(css): add form components`

---

### Task 9: List CSS

**Files:** Modify `fervid-ds.css`

Port `.req-card` (518-542), `.thread` (548-591) — distinct from the legacy `.timeline`, both must coexist — `.comment-box` (593-599), `.work-areas`/`.area` (1002-1037), `.notif` (1040-1056), `.dl` (1080-1088), `.req-head` (1091-1101), `.acc`/`.head-row` (973-996), `.perm-acc`/`.perm-table`/`.perm-scope` (1059-1077).

`.vbar` collides: existing modifiers are `.is-under/.is-ontrack/.is-over/.is-unbudgeted` (`ds:1689-1692`); add `.ok/.warn/.bad` as aliases rather than renaming. Also apply the mockup-review fix: `.vbar { display: inline-block; }` — as an inline span it ignores `width` and the bars vanish.

- [ ] **Step 1:** Append. **Step 2:** Kitchen sink. **Step 3: Commit** `feat(css): add list and detail components`

---

### Task 10: Overlay and table CSS

**Files:** Modify `fervid-ds.css`

Port `.overlay` (818-823), `.sheet` (825-848), `.compare` (851-862), sticky `thead th` (873-878), dark `tfoot` (886), `table.t-cards` + `td[data-label]` + `td.t-lead` (891-925), `.toolbar`/`.m-filters`/`.filter-btn` (936-949), `.empty` (888).

`.overlay` must be `position: fixed` unconditionally — in the mockup it is `absolute` only because `.appshell` is a fake phone.

- [ ] **Step 1:** Append. **Step 2:** Kitchen sink includes a 10-column table. **Step 3:** Verify it restacks into labelled cards below 860 px. **Step 4: Commit** `feat(css): add overlay, sheet and responsive table components`

---

### Task 11: Mobile chrome CSS

**Files:** Modify `fervid-ds.css`

Port `.m-topbar` (242-273), `.tabbar` (275-297), `.more-sheet` (300-330). Corrections over the mockup:
- `.tabbar { position: fixed; padding-bottom: env(safe-area-inset-bottom); }`
- Use `100dvh` not `100vh` so mobile browser chrome does not eat the tab bar.
- `.page` gets `padding-bottom: 96px` below 860 px so content never sits under the bar.

- [ ] **Step 1:** Append inside the 860 px block. **Step 2:** At 390 px confirm the bar is fixed to the viewport bottom and never overlaps content. **Step 3: Commit** `feat(css): add mobile top bar, tab bar and more sheet`

---

### Task 12: Permission accessors on auth.Manager

**Files:** Modify `internal/auth/auth.go:133-143`; Test `internal/auth/auth_test.go`

**Interfaces:** Produces `func (m *Manager) Can(u store.User, resource, action string) bool` and `func (m *Manager) Permissions(u store.User) store.PermissionSet`. Consumed by Tasks 13-17 and by every later phase.

- [ ] **Step 1: Write the failing test** — admin allowed everywhere; `data_entry` allowed `payment:create`, denied `role:edit`; unknown resource denied.
- [ ] **Step 2:** Run, confirm fail. **Step 3:** Extract the enforcement currently inline at `auth.go:136` into `Can`; add `Permissions` returning a snapshot so a template can call `.Perms.Can` repeatedly without re-entering Casbin. Refactor `RequirePermission` onto `Can`.
- [ ] **Step 4:** `go test ./internal/auth` green. **Step 5: Commit** `feat(auth): add Can and Permissions accessors`

---

### Task 13: Nav model

**Files:** Create `internal/app/nav.go`; Test `internal/app/nav_test.go`

**Interfaces:** Produces the types below; `PageData.Shell` (Task 16) carries them.

```go
type NavItem  struct { Key, Label, Href, Icon string; Resource, Action string; Badge string; Soon bool }
type NavGroup struct { Title string; Items []NavItem }
type TabItem  struct { Key, Label, Href, Icon string }
type TabBar   struct { Left []TabItem; Fab TabItem; Right []TabItem }
type Shell    struct {
	Groups []NavGroup
	Tabs   TabBar
	Active string
	Badges map[string]int
	Unread int
	Chrome string // "app" | "none"
	Title, Sub, BackHref, ActionLabel, ActionHref string
}
```

`navSpec` mirrors the mockup's groups — Home / Requests / Payments / Budget / Reports / Masters / Admin / Coming soon — with `Resource`+`Action` replacing the prototype's persona lists. Filter rule: keep when `it.Soon || perms.Can(it.Resource, it.Action)`; drop groups left empty.

- [ ] **Step 1: Write the failing table test** — admin sees every non-soon item; `data_entry` sees exactly Home, My requests, Payments ledger, Variance grid, Reports.
- [ ] **Step 2:** Run, fail. **Step 3:** Implement `buildShell(perms) []NavGroup`. **Step 4:** Green. **Step 5: Commit** `feat(app): add permission-driven nav model`

---

### Task 14: Tab bar resolution

**Files:** Modify `internal/app/nav.go`; Test `internal/app/nav_test.go`

Centre action, first match wins — no role strings:

| Permission | Centre action |
|---|---|
| `request:approve` | Approve → `/approvals` |
| `payment:create` | Pay → `/payments/new` |
| `request:create` | New → `/requests/new` |
| otherwise | Home → `/` |

- [ ] **Step 1: Write the failing test** for the four permission sets, plus `grep -c '"admin"' internal/app/nav.go` == 0.
- [ ] **Step 2:** Fail. **Step 3:** Implement `resolveTabs(perms) TabBar`; Left and Right cap at two items each, "More" always last. **Step 4:** Green. **Step 5: Commit** `feat(app): resolve mobile tab bar from permissions`

---

### Task 15: Badge counts without N+1

**Files:** Create `internal/store/badges.go`; Test `internal/store/badges_test.go`

**Interfaces:** Produces `func (s *Store) BadgeCounts(ctx context.Context, userID int64, perms PermissionSet) (map[string]int, error)`.

One statement with scalar sub-selects; each sub-select is included only when the caller holds the matching permission. Results memoised per user for 15 s in a `sync.Map` so htmx bursts do not re-query.

- [ ] **Step 1: Write the failing test** — seed two pending approvals for user A, assert `counts["approvals"] == 2`; assert a second call inside the window issues no further query (count statements via a wrapping driver).
- [ ] **Step 2:** Fail. **Step 3:** Implement. **Step 4:** Green, including `-race`. **Step 5: Commit** `feat(store): add cached badge counts`

---

### Task 16: Wire the shell into PageData

**Files:** Modify `internal/app/app.go:36-75`, `internal/app/http_errors.go:101-107`; Test `internal/app/app_integration_test.go`

- [ ] **Step 1: Write the failing test** — a rendered page contains the sidebar; the same request with `HX-Request: true` contains no `<aside`.
- [ ] **Step 2:** Fail. **Step 3:** Add `Shell Shell` to `PageData`; populate it in `renderStatus`, the single choke point where `data.User` is already set. Skip shell construction entirely when `HX-Request` is present and render a fragment wrapper.
- [ ] **Step 4:** Green. **Step 5: Commit** `feat(app): render shell from permissions`

---

### Task 17: Rewrite the top/bottom templates

**Files:** Modify `internal/app/templates.go:4-43`; Test `internal/app/app_integration_test.go`

- [ ] **Step 1: Write the failing test** — every one of the eleven existing routes renders 200 with the new shell; `grep -c 'eq .User.Role' internal/app/templates.go` == 0.
- [ ] **Step 2:** Fail. **Step 3:** Rewrite `top`: `.appshell` → `.sidebar` ranging `Shell.Groups` with `.sgroup` headings, `.ico`, `.n` badges and `soon` entries; `.m-topbar`; `main.page > .page-inner`; `.tabbar`; `.more-sheet`. Honour `Chrome == "none"` for login and error pages. Replace every `{{if eq .User.Role "admin"}}` with `{{if .Perms.Can "…" "…"}}`.
- [ ] **Step 4:** Green; all eleven screens render. **Step 5: Commit** `feat(app): rebuild app shell on the new design system`

---

### Task 18: Rewrite fervid-app.js

**Files:** Modify `web/static/fervid-app.js`; Test `tests/e2e/components.spec.ts`

Behaviours: accordion (`.acc-head`, `.pa-head`, `aria-expanded` + `hidden`), overlay (`[data-open]`/`[data-close]`/backdrop/**Esc**/**focus trap** — the mockup has neither and a real app needs both), `[data-when]` conditional reveal keeping the `name:value|value` contract, money field live grouping and spell-out, More sheet. Retain the existing `toggleProject`. Delete the choice-card JS — Task 8 made it CSS.

- [ ] **Step 1: Write the failing Playwright spec** driving all five behaviours on the kitchen sink.
- [ ] **Step 2:** Fail. **Step 3:** Implement. **Step 4:** `make test-e2e` green. **Step 5: Commit** `feat(js): implement design system behaviours`

---

### Task 19: money.InWords and comma-tolerant parsing

**Files:** Modify `internal/money/money.go`; Test `internal/money/money_test.go`

The client writes grouped digits back into the input (`1,00,000`), so handlers must accept them.

- [ ] **Step 1: Write the failing table test** — `InWords` for 0, 1, 100000, 12345678, 1000000000; `ParsePaise` accepts `1,00,000`, `₹1,00,000.50`, rejects `abc`.
- [ ] **Step 2:** Fail. **Step 3:** Implement, mirroring the mockup's lakh/crore grouping. **Step 4:** Green. **Step 5: Commit** `feat(money): add InWords and comma-tolerant parsing`

---

### Task 20: Visual regression review

**Files:** Modify `tests/e2e/baseline.spec.ts`

- [ ] **Step 1:** Re-capture all 22 screenshots into `output/playwright/after/`.
- [ ] **Step 2:** Diff each against `baseline/` and review every delta by eye.
- [ ] **Step 3:** Every difference must be an intended terracotta/spacing change. Nothing missing, nothing overlapping, no horizontal overflow at 390 px.
- [ ] **Step 4:** `make test-all` green.
- [ ] **Step 5: Commit** `test(e2e): verify redesign against visual baseline`

---

## Self-review

- **Spec coverage:** every component in UI/UX spec §9 maps to Tasks 6-11; the shell in §3 maps to 13-17; the JS in §7 to 18; responsiveness in §7 to 4 and 11.
- **Type consistency:** `Shell`/`NavGroup`/`NavItem`/`TabBar`/`TabItem` are defined once in Task 13 and consumed unchanged in 14, 16, 17. `PermissionSet` comes from Phase 1 Task 7; until that lands, Task 12 supplies it from the current Casbin engine so Phase 0 and Phase 1 can proceed in parallel.
- **Known ordering note:** Tasks 12-17 depend on `store.PermissionSet` existing. Phase 1 Tasks 1-9 create the real one. Phase 0 defines the interface it needs and adapts, so the two tracks converge at Phase 1 Task 9 (auth engine swap) without either blocking the other.
