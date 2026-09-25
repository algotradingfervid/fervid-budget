# Fervid UX review checklist

Use these as investigation prompts, not automatic defects. Ground findings in a user's task, an observed outcome and reproducible evidence. Research referenced guidance when it affects a judgment. The skill's review-only boundary takes precedence: commit actions only in authorized disposable fixtures.

## Product and coverage

Map Home, requests, approvals, Accounts queue, recoverables, ledger, variance grid, budgets, monthly plans, reports, vendors, projects, heads, users, roles, configuration, notifications, audit and backups when present. Include important tabs, dialogs, empty results and error states. Record missing routes rather than inventing them.

Walk requester → manager → Accounts handoffs. Distinguish approval, payment recording, settlement and recovery. Use exact-role fixtures; an administrator seeing an action does not prove a requester can. Test priority flows on desktop and phone, and tablet layout. Record which outcomes were committed, previewed, or blocked.

## State, money and trust

- Does each status explain what happened, what remains, and who acts next?
- Are approved, paid, remaining, written-off and recoverable amounts distinct? Does a short payment require a deliberate balance decision?
- Is payment recording distinguishable from moving actual funds?
- Do duplicate invoice cues identify existing records without treating legitimate recurring invoices or installments as fraud?
- Do totals and exports state their period, project/head scope and applied filters? Does a filtered subtotal falsely imply a company total?
- Does a zero budget mean no plan, a recurring obligation, or something else? Treat this as an intent question until evidence establishes the semantics.
- Can a report total be explained through supporting records? Can a supplier total be traced to its requests and payments?
- Can audit entries show actor, time and meaningful old/new values, rather than technical counts alone?
- Are dangerous consequences disclosed with a way to review, correct or reverse appropriate actions? WCAG 3.3.4 does not mandate confirmation on every routine save.

Sources: [NN/G heuristics](https://www.nngroup.com/articles/ten-usability-heuristics/), [WCAG error prevention](https://www.w3.org/WAI/WCAG22/Understanding/error-prevention-legal-financial-data.html).

## Navigation and task continuity

Check recognition of the main action, clear labels, active location, context preservation and a reasonable return route. Open relevant overflow menus and collapsed sections. Verify mobile equivalents rather than assuming desktop-only actions are missing.

Investigate hover-revealed controls with keyboard focus, touch and alternate menus before calling them inaccessible. A tool's `revealsOnHover` result alone proves no absence of alternatives. Do not impose a universal click count or disclosure-depth limit.

Check role-specific home screens, sign-in recovery, preservation of an authorized cold-link destination, and explicit operational handoffs. A backup may safely require an operator; the UX should explain the path without implying that a one-click restore is mandatory.

## Forms and recovery

Persistent labels, understandable defaults, clear required/optional fields and contextual help should support the task. Test invalid input, server validation, a no-match filter and preservation of entered values. Distinguish a genuinely empty system from filtered-out existing data.

Locate field errors and summary links. For partial batch saves, identify saved versus unsaved rows and preserve rejected values. Neither atomic saving nor validation-on-blur is universally required; choose validation timing based on task, accessibility and research. Avoid interrupting unfinished input.

Check Back, Cancel, Escape, refresh and double submission where safe. Record actual outcomes; do not claim testing that stopped before commit was complete. For uploads, distinguish selecting a file, submitting it, and verifying the downloaded content.

Sources: [GOV.UK validation](https://design-system.service.gov.uk/patterns/validation/), [error summary](https://design-system.service.gov.uk/components/error-summary/).

## Layout and accessibility spot checks

Review 1440×900 desktop, 390×844 phone, 820×1180 tablet and 320 CSS pixel reflow where appropriate. These are emulated Chromium viewports, not native Safari or Android tests.

- Challenge long names, large values, dense tables and fixed-bar interactions. Scroll to the end before calling a partially obscured action blocked; inspect hit testing rather than relying on a programmatic locator click.
- Necessary two-dimensional tables have a reflow exception. Their headings, prose, filters and ordinary controls do not inherit it. Preserve row/column context; prefer bounded scrolling where practical.
- WCAG 2.5.8 uses 24 CSS pixels with spacing and other exceptions. Approximately 44 CSS pixel primary touch targets are a stronger usability recommendation, not the AA minimum; platform pt/dp units are not interchangeable measurements.
- Verify keyboard operation, visible focus, modal focus entry/containment/return, and Escape. Missing a skip link alone is not a proven bypass failure when landmarks/headings provide a valid mechanism; record keyboard traversal effort separately.
- Where a control boundary or authored focus indicator is necessary, assess adjacent-color contrast. Corroborate screenshot pixels with computed colors; antialiasing can mislead. Decorative boundaries, inactive controls and unmodified browser styling have relevant exceptions.
- Automated axe/layout/focus results are candidates. Manually reject false positives and record incomplete checks. A zero-violation scan is not conformance certification. A separate accessibility audit may be necessary for complete assistive-technology coverage.

Sources: [reflow](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html), [target size](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html), [non-text contrast](https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast.html), [bypass blocks](https://www.w3.org/WAI/WCAG22/Understanding/bypass-blocks.html), [modal dialog pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/).

## Feedback and timing

Check whether users can tell that an action was received, is pending, succeeded, or failed. Consider delayed feedback, repeat-click risk and recovery. The familiar 0.1/1/10-second thresholds are contextual guidance, not pass/fail laws. Toolkit command duration includes automation overhead; do not present it as app latency or a performance benchmark. Determine progress treatment from observed waits and task consequences, not an arbitrary spinner rule.

## Content and visual judgment

Prefer task language to internal enums or implementation explanations. Use consistent names, currency, date formatting and units. Assess hierarchy and grouping by whether users can find and interpret information. Avoid reporting style differences, a second action, an icon or a disabled control as defects without task impact. Explain relevant unavailable actions; roadmap-only features may be better outside operational navigation.

## Evidence and acceptance

Each finding needs device/state, URL, exact visible text where relevant, reproducible steps, impact and a concrete recommendation. Inspect annotated screenshots and keep callouts short enough to read. Distinguish observed behavior from an inferred risk, a proposed enhancement, a business-policy assumption and a measured standards issue. Give high-impact recommendations observable acceptance criteria. Retain strengths and rejected hypotheses, and make every coverage limitation explicit.
