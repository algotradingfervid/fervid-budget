# /ux-review data formats

Every file lives in the run folder `RUN` (`~/ux-reviews/<host>-<stamp>/`). All JSON is UTF-8. Save your file after every screen or step so a stopped run keeps its work.

## Run folder

| Path | Written by | Contents |
|---|---|---|
| `auth.json` | the person (login) | Browser session. Read-only for agents. |
| `CHECKLIST.md`, `SCHEMA.md` | lead | Copies of these reference files. |
| `map/<section>.json` | ux-mapper | Screens in one section. |
| `product.json` | ux-analyst | Product hypothesis. |
| `screens.json` | lead | Merged map. |
| `brief.json` | lead | Product, the person's answers and policies. Every reviewer reads it. |
| `findings/<source>.json` | screen-reviewer, task-walker, hunter | Findings. |
| `walks/<taskId>.json` | ux-task-walker | Walk record. |
| `verified/<source>.json` | ux-verifier | Findings plus verdicts. |
| `created-all.jsonl` | lead | Every agent's change log combined, rebuilt before each verifier. |
| `files.json` | lead | Lists the verified, walks and created files for the editor, for an explicit artifact inventory. |
| `redact.json` | lead | `{"text": [...]}`: the person's name, email and other identifying strings; every `shot` blanks them. |
| `answers.json` | lead | The person's answers to major questions. |
| `notCovered.json` | lead | Areas that failed twice. |
| `created/<source>.jsonl` | any agent that creates or sends something | One file per agent, one line per change (never shared, so parallel agents cannot overwrite each other). |
| `final.json`, `final-findings-NN.json` | ux-editor | Merged report data; findings are split into chunk files of at most 15. |
| `report.html`, `report.md` | lead (`report.py`) | The report. |
| `shots/`, `fixtures/`, `sessions/`, `measurements/` | toolkit | Screenshots, dummy uploads, browser sessions, measurements. |

## map/&lt;section&gt;.json and screens.json

```json
{"app": "Demo", "baseUrl": "http://127.0.0.1:8080", "auth": "auth.json or null",
 "danger": [{"screen": "S03", "control": "Delete account", "why": "irreversible"}],
 "screens": [{"id": "S01", "name": "Home", "url": "/", "reach": "logo or first nav item", "kind": "page|modal|drawer|tab|wizard-step|menu",
              "section": "Main", "components": ["nav", "card list"], "forms": ["search"], "states": ["empty", "populated"],
              "weight": 3, "mobileOnly": false, "shot": "shots/desktop__m-main__S01-home.png", "notes": "", "tasks": ["T1"]}],
 "flows": [{"id": "F1", "name": "Add an item", "screens": ["S01", "S04"], "steps": ["click Add", "fill form", "Save"]}],
 "outbound": [{"from": "S02", "to": "/settings", "label": "Settings"}]}
```

`weight` runs from 1 (a trivial page) to 5 (a dense, interactive screen). Screen ids in a mapper's file are prefixed with the section slug (`main-S01`). The lead renumbers them when it merges.

## product.json

```json
{"purpose": "One paragraph.", "domain": "expense tracking",
 "users": [{"persona": "Freelancer", "goals": "Claim expenses quickly", "context": "phone, on the go"}],
 "tasks": [{"id": "T1", "name": "Add an expense from a receipt", "persona": "Freelancer", "priority": 1,
            "start": "/", "steps": ["open Add", "upload receipt", "check amount", "save"], "mobilePrimary": true}],
 "domainTerms": [{"term": "Claim", "meaning": "a submitted report"}],
 "intentQuestions": [{"q": "Is the review queue meant to be the home screen?", "options": ["Yes", "No, Overview is"], "guess": "No, Overview is", "why": "affects whether nav order is judged wrong"}],
 "confidence": "medium", "inferred": true}
```

## brief.json

This is `product.json` plus the fields below. `corrections` holds the person's corrections to the product summary, and `intentAnswers` holds their answers to the check-in questions.

```json
{"corrections": ["It is also used by teams"], "intentAnswers": [{"q": "...", "answer": "..."}],
 "testEmail": "", "mutationPolicy": "read-only",
 "realDataPolicy": "read-only", "neverDo": ["Delete account", "Sign out"], "danger": [], "baseUrl": "http://127.0.0.1:8080", "focus": null}
```

`mutationPolicy: "read-only"` prohibits product changes, submissions, sends, and other external effects. Inspect safely up to the commit boundary. An empty `testEmail` is mandatory for this mode.

## Finding

```json
{"id": "G1-mobile-03", "title": "Save gives no feedback",
 "kind": "layout|interaction|navigation|feedback|state|content|consistency|hidden|flow|trust|a11y-note",
 "severity": "critical|high|medium|low", "effort": "S|M|L",
 "major": false, "majorReason": "",
 "screen": "S04 Edit expense", "devices": ["mobile"], "task": "T1", "persona": "Freelancer",
 "observation": "Tapped \"Save\" (e12). The page did not change for 2.4 s and no message appeared; the list later showed the new amount.",
 "impact": "Users tap Save again and create duplicates.",
 "recommendation": "Disable Save and show \"Saving…\" on the button, then a toast \"Expense saved\" with Undo.",
 "principle": "Visibility of system status (Nielsen 1); response time 1 s",
 "steps": ["goto /expenses/new", "fill Amount with 12.34", "tap Save"],
 "screenshots": [{"path": "shots/mobile__G1-mobile__S04-save.png", "caption": "Save button with no pending state"}],
 "confidence": "high", "source": "G1-mobile"}
```

Rules:
- **observation** quotes the exact on-screen text and says what you did and what happened. No guesses about code.
- **recommendation** says exactly what to change: new copy, a layout or a behaviour. Never "improve" or "consider".
- **Screenshot.** Every critical, high or medium finding that can be seen on screen gets one annotated screenshot: `shot NAME --mark REF:"what should change":critical` with at most 3 marks. For something missing, mark where it should be with `"Missing: …"`.
- **severity:**
  - `critical` blocks a main task, loses or corrupts data, or misleads about money or consequences;
  - `high` badly derails or confuses in a common flow;
  - `medium` slows the user down;
  - `low` is polish.
- **effort** is the design and build effort of the recommendation: S is under a day, M a few days, L a week or more.
- **major** is `true` only when the recommendation would **restructure navigation or change a UI/UX flow in a way that seriously affects how the app is used**: moving sections, merging or splitting screens, reordering a main workflow, or replacing a flow. Set `majorReason` to one line. Everything else is `false`, however severe.

## Findings file

```json
{"source": "G1-mobile", "device": "mobile", "covered": ["S01", "S04"], "notReached": [{"screen": "S05", "why": "needs a paid plan"}],
 "hidden": [{"item": "Export", "where": "S02 row kebab", "howToReach": "hover the row, then open ⋮"}],
 "questions": [{"q": "Is Archive meant to delete?", "why": "copy says archive, item disappears from all views"}],
 "findings": []}
```

Only the hunter fills `hidden`. `questions` holds non-major doubts about intent; they go into the report.

## walks/&lt;taskId&gt;.json

```json
{"task": "T1", "name": "Add an expense from a receipt", "persona": "Freelancer", "device": "mobile",
 "steps": [{"n": 1, "screen": "S01", "intent": "start adding", "action": "tap +", "q1": true, "q2": true, "q3": false, "q4": true,
            "pass": false, "note": "+ has no label; unclear it adds an expense", "ms": 180}],
 "clicks": 9, "decisions": 4, "hesitations": 2, "deadEnds": ["Back from upload lost the file"],
 "recovery": {"back": "ok", "refresh": "lost form", "deepLinkCold": "ok"},
 "completed": true, "idealFlow": "Home → Add (camera) → confirm amount → Save: 4 taps"}
```

The four questions:
- `q1`: will the user try to do this?
- `q2`: will they notice the control?
- `q3`: will they understand that it does this?
- `q4`: will they get feedback?

## Verified findings

The findings file with every finding given two more fields:

```json
{"verdict": "confirmed|adjusted|rejected|unverified", "verifierNote": "Reproduced on mobile; severity lowered to medium because the toast appears after 900 ms."}
```

Rejected findings stay in the file. Verifiers never add findings; they put suspected misses in `verifierNote`.

## answers.json

```json
[{"ids": ["G2-desktop-04", "task-T3-02"], "question": "Move Settings into the top nav?", "answer": "agree|intended|<free text>"}]
```

## created/<source>.jsonl

One JSON object per line:

```json
{"at": "2026-09-24T21:10:00", "agent": "G1-mobile", "what": "Expense \"UXR-TEST lunch\"", "where": "S04", "how": "form submit", "removed": false}
```

When you delete your own item, append a second line for it with `"removed": true`.

## final.json

```json
{"app": "", "baseUrl": "", "date": "", "devices": [],
 "product": {"purpose": "", "users": [], "tasks": [], "corrections": []},
 "summary": "5–8 sentences",
 "priorities": [{"text": "one line", "ref": "UX-001"}],
 "screens": [{"name": "", "url": "", "section": "", "devices": []}],
 "walks": [ /* walk records */ ],
 "hidden": [{"item": "", "where": "", "howToReach": "", "finding": "UX-007"}],
 "findings": [ /* findings, with id UX-nnn, foundBy, sourceIds[], answer? */ ],
 "heuristics": [{"name": "Visibility of system status", "score": 3, "why": ""}],
 "strengths": [""],
 "questions": [{"q": "", "answer": ""}],
 "created": [{"what": "", "where": "", "removed": true}],
 "notCovered": [""]}
```


## Autonomous research extensions (authoritative for this skill)

`answers.json` is only for actual user statements. Do not populate it with research recommendations. Use these instead; include arrays in `final.json` or standalone files.

`research.json`:
```json
[{"id":"R01","title":"Official source title","url":"https://example.org/page","publisher":"Organization","accessed":"YYYY-MM-DD","claim":"Narrow claim supported by this page","type":"standard|official-product-doc|research","limitations":"What this does not establish"}]
```

`questions.json` is the complete question ledger, from analyst, reviewers, major findings, verifier doubts, and research follow-ups:
```json
[{"id":"Q01","question":"Which workflow should be primary?","origin":"analyst","findingIds":["UX-001"]}]
```

`decisions.json`:
```json
[{"id":"D01","questionId":"Q01","question":"Which workflow should be primary?","answer":"Recommended provisional answer","basis":"Why evidence applies; tradeoff and contrary evidence","confidence":"medium","status":"provisional","sourceIds":["R01"],"findingIds":["UX-001"],"ownerIntent":"Unknown; source research cannot establish this","revisitIf":"The owner confirms a different primary user"}]
```

A researched question can remain provisional when owner intent is unknown; it cannot remain unanswered. When no suitable public source exists, record sources searched and explain the evidence limit in `basis`, with a conservative provisional answer. Do not claim “industry standard” from a single competitor.

Finding extensions: `verdict`, `verifierNote`, `acceptanceCriteria` (array), `sourceIds` (original finding provenance), `researchSourceIds` (R IDs), `decisionIds` (D IDs), `evidenceType` (`browser-observed|source-corroborated|inference`), and `limitations`. `major: true` describes the scale of the recommendation, never owner approval. `effort` is an estimate; give a scope-dependent rationale when material. Include rejected candidates in verified source files, omit them from final active findings, and summarize meaningful rejected hypotheses in the report.

Optional `final.json.methodology` is plain text explaining scope, environment, device emulation, verification coverage, and read-only limits. Every `questions[]` legacy entry must have an answer; prefer the richer decision ledger for new runs. Preserve all question IDs through deduplication or document their aliases.
