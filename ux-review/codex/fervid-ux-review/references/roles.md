# Independent role briefs

Every brief supplies absolute `RUN`, toolkit path, unique `SESSION`, URL, device(s), auth-state path when required, assignment, allowed actions, and output paths. Read the run brief, schema, and checklist. Never reuse another agent's browser session. Save progress incrementally and close the session at the end. Initial reviewers never read other reviewers' output; verifiers receive the assigned candidate file; the editor sees all completed evidence.

## Mapper

Map the assigned section breadth-first through visible navigation on desktop and mobile. Open safe tabs, menus, drawers, and modals; record routes, titles, reach steps, components, forms, states, and dangerous controls. Distinguish a new screen from the same layout with different data. Inspect hidden controls for alternate routes. Write `map/<section>.json`; no judgments, submissions, or product mutations.

## Analyst / researcher

Infer purpose, personas, domain terms, and 5–10 main jobs from product copy and actual UI. Write `product.json`, mark inferences and confidence, and propose questions whose answers change the audit. In the second round, research all assigned Q IDs using current primary sources and relevant comparable products; write source records and provisional decisions. Never replace private owner intent with market consensus or present documented competitor features as observed hands-on tests.

## Screen reviewer

Review assigned screens and states on one device: hierarchy, terminology, action visibility, persistent form labels, validation, feedback, error recovery, empty states, financial meaning, and responsive behavior. Check basic keyboard access and record measured accessibility evidence separately from a full compliance claim. Tablet cells may be layout-only. Observe unsaved states without committing. Write `findings/<cell>.json`, coverage, and questions; visible high-impact findings need screenshot evidence or an explicit evidence limitation.

## Task walker

Start at the normal entry point as the assigned persona. At each step record intent and four questions: will they try this, notice the control, understand it, and recognize feedback? Count interactions and decisions; record dead ends and hesitation as expert predictions, not measured user-study results. Check safe back/refresh/deep-link recovery. Stop before committing a mutation; distinguish successful navigation from successful persistence. Write `walks/<task>-<device>.json` and a findings file. Describe an ideal flow using existing capabilities.

## Hunter

On desktop and mobile inspect overflow menus, hover-only actions, collapsed sections, deep settings, hidden controls, keyboard routes, dead ends, and navigation differences. Hidden content is not automatically defective: assess task frequency, labeling, and alternate access. A DOM-hidden duplicate or responsive variant is not a finding merely because it exists. Write discoverability inventory, findings, and questions to `findings/hunter.json`.

## Verifier

Use a fresh session to reproduce assigned candidates from their steps. Check exact text, the relevant state and device, screenshots, recovery, and plausible alternative explanations. Rate the observed consequence, not an imagined worst case. Apply researched decisions with explicit confidence; never infer the owner approved them. Write the same file shape to `verified/<source>.json`, preserving rejected/unverified candidates and explanations. Do not silently introduce new findings: return suspected misses to the lead for review. Reproduction can be read-only or blocked; state which.

## Editor

Merge only after verification dispositions and question research exist. Deduplicate by underlying problem + state + screen; consolidate systemic causes with examples. Preserve source IDs, devices, evidence, verdict, and uncertainty. Rerate consistently; order by impact, exposure, evidence, then effort. Write `final.json` and optional `final-findings-NN.json` arrays (≤15 per chunk), with source/decision links and acceptance criteria. Include first-round hypotheses rejected or changed by research. The editor does not browse the product or claim independent reproduction.
