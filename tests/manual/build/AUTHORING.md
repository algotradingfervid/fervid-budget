# Writing a manual page

Every page is one `.mts` file under `content/<section>/<slug>.mts`, default-exporting a
`Page`. The slug, title and summary are fixed in `site.mts` — copy them exactly; the build
refuses a file whose declared `section`/`slug` disagree with where it sits.

```ts
import { page, prose, section, shot, steps, note, warning, tip, table, fields, faq } from '../../schema.mts';

export default page({
  section: 'requester',
  slug: 'raising-a-request',
  title: 'Raising a request',        // exactly as site.mts has it
  summary: '…',                      // exactly as site.mts has it
  blocks: [ /* … */ ],
});
```

## The blocks

| Helper | Use it for |
|---|---|
| `prose('…')` | Paragraphs. A blank line inside the string starts a new paragraph. |
| `section('…')` | An `h2`. Use these to break a page into parts. |
| `subsection('…')` | An `h3` inside a section. Never skip from `section` to nothing. |
| `shot('id', 'caption')` | A screenshot. The id must exist in `assets/shots.json`. |
| `steps([{text, note?, shot?}])` | A numbered procedure. One action per step. |
| `note/warning/tip('text', 'optional title')` | An aside. `warning` is for things that cannot be undone. |
| `table(headers, rows, caption?)` | Any table. Every row must have as many cells as there are headers. |
| `fields([{name, required, note}])` | A form-field reference. |
| `faq([{q, a}])` | Troubleshooting pairs at the end of a page. |

## Markup inside text

Only four things, and nothing else — no raw HTML, it will be escaped and shown literally.

- `**bold**` — for control labels the reader must find on screen: **Approve request**
- `*italic*` — for emphasis, sparingly
- `` `code` `` — for literal values, statuses and field names: `pending`
- `[label](page:section/slug)` — a link to another manual page. The build fails on an
  unknown target, so you cannot link to a page that does not exist.

External links use a full `https://` URL. Anchors use `#the-heading-id`.

## The rules that matter

**1. Describe only what the screenshot actually shows.** This is the one rule that makes the
manual trustworthy. If you are writing about a screen, open its PNG under
`docs/manual/assets/shots/<id>.png` and look at it. Do not describe a button because a
similar product has one.

**2. Verify anything you assert about behaviour.** The code is the source of truth. Templates
are Go string literals in `internal/app/templates.go` (there is no `web/templates/`
directory); routes and permissions are in `internal/app/app.go`; validation lives in
`internal/store/store.go`. You may query the live dataset read-only:

```
sqlite3 -readonly "data/fervid.db" "select …"
```

**3. Mark what you could not confirm.** Write it plainly in the text — "this depends on how
your organisation has configured X" — rather than guessing. Never invent a field name, a
button label, a status or a message.

**4. Write for somebody doing the job, not reading a spec.** Short sentences. Say what the
reader does and what happens as a result. Prefer "Select **Approve request**" to "The
approve action may be invoked".

**5. Do not promise behaviour you have not seen.** If a screen's content depends on
permissions or record state, say so.

## Voice

Match the house style. Plain British English, second person, present tense. No exclamation
marks, no "simply", no "just", no "easily" — if it were easy the reader would not be reading.
Money is written as `₹80,700.00`. Statuses are written in code font in their literal form
(`partial_review`) when naming the value, and in prose form ("under review") when describing
it to a reader.

Use they/them for any person you refer to generically.

## Checking your work

```
node tests/manual/build/build.mts --allow-unreferenced
npx tsc --noEmit
```

The build fails if you name a shot that does not exist, link to a page that does not exist,
or give a table a ragged row. Those failures are the point — they are what stops prose
describing a product that is not there.
