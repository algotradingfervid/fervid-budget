// The content schema for the user manual.
//
// Every manual page is a `Page` object built from a small, closed set of blocks.
// The point of a closed set is that the pages are written in parallel by several
// authors: if the only way to place a screenshot is `shot(id, caption)`, then no
// page can invent its own markup, drift from the house style, or reference a
// picture that was never taken — the build resolves every id against the shots
// actually on disk and fails on a miss.
//
// Authors write prose in a deliberately tiny markup subset (bold, italics, code,
// links). Everything else is a block. Raw HTML is not accepted anywhere, so a
// stray tag cannot break the page chrome.

export type SectionKey =
  | 'getting-started'
  | 'requester'
  | 'manager'
  | 'accounts'
  | 'admin'
  | 'reference';

/** A single step in a numbered procedure. */
export interface Step {
  /** What the reader does. One action per step. */
  text: string;
  /** Screenshot showing the result of this step, if one was captured. */
  shot?: string;
  /** A short aside — what to watch out for, or what the app does in response. */
  note?: string;
}

export interface FieldRow {
  /** The field's visible label, exactly as the screen spells it. */
  name: string;
  required: boolean;
  /** What to put in it, and what the app does with it. */
  note: string;
}

export type Block =
  /** One or more paragraphs. Blank line separates them. */
  | { kind: 'prose'; text: string }
  /** An h2 that opens a new part of the page. */
  | { kind: 'section'; title: string }
  /** An h3 inside a section. */
  | { kind: 'subsection'; title: string }
  /** A captured screen. `id` must match a shot on disk. */
  | { kind: 'shot'; id: string; caption: string }
  /** A numbered procedure. */
  | { kind: 'steps'; intro?: string; steps: Step[] }
  /** A highlighted aside. */
  | { kind: 'callout'; tone: 'note' | 'warning' | 'tip'; title?: string; text: string }
  /** A generic table. Every row must be the same length as `headers`. */
  | { kind: 'table'; caption?: string; headers: string[]; rows: string[][] }
  /** A form-field reference table. */
  | { kind: 'fields'; caption?: string; rows: FieldRow[] }
  /** Question-and-answer pairs, for troubleshooting sections. */
  | { kind: 'faq'; items: { q: string; a: string }[] };

export interface Page {
  /** Path within the section, no slashes: 'raising-a-request'. */
  slug: string;
  section: SectionKey;
  /** The h1, and the nav label. */
  title: string;
  /** One sentence. Shown under the title and in the section index. */
  summary: string;
  blocks: Block[];
}

// ---------------------------------------------------------------- helpers
//
// Thin constructors, so content files read as content rather than as object
// literals, and so a typo in a block kind is a compile error.

export const prose = (text: string): Block => ({ kind: 'prose', text });

export const section = (title: string): Block => ({ kind: 'section', title });

export const subsection = (title: string): Block => ({ kind: 'subsection', title });

export const shot = (id: string, caption: string): Block => ({ kind: 'shot', id, caption });

export const steps = (steps: Step[], intro?: string): Block => ({ kind: 'steps', intro, steps });

export const note = (text: string, title?: string): Block =>
  ({ kind: 'callout', tone: 'note', title, text });

export const warning = (text: string, title?: string): Block =>
  ({ kind: 'callout', tone: 'warning', title, text });

export const tip = (text: string, title?: string): Block =>
  ({ kind: 'callout', tone: 'tip', title, text });

export const table = (headers: string[], rows: string[][], caption?: string): Block =>
  ({ kind: 'table', caption, headers, rows });

export const fields = (rows: FieldRow[], caption?: string): Block =>
  ({ kind: 'fields', caption, rows });

export const faq = (items: { q: string; a: string }[]): Block => ({ kind: 'faq', items });

/** Declares a page. Content files `export default page({...})`. */
export const page = (p: Page): Page => p;
