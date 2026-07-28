// Builds the HTML user manual from the content files and the captured shots.
//
// Run with plain node — no transpiler, no new dependency:
//   node tests/manual/build/build.mts
//
// The build is the thing that keeps ~57 independently written pages honest. It
// refuses to emit a manual when:
//   * a page names a screenshot that was never captured,
//   * a screenshot was captured but no page ever shows it,
//   * an internal link points at a page that does not exist,
//   * a page declared in the site manifest has no content file.
// Each of those is a way for prose to describe a product that isn't there, which
// is the failure mode this manual most needs to avoid.

import { readFileSync, writeFileSync, mkdirSync, existsSync, readdirSync, copyFileSync, statSync } from 'node:fs';
import { join, dirname, relative } from 'node:path';
import { pathToFileURL } from 'node:url';
import { execFileSync } from 'node:child_process';
import type { Block, Page, SectionKey } from './schema.mts';
import { SITE, SECTIONS, type SectionDef } from './site.mts';

const ROOT = join(import.meta.dirname, '..', '..', '..');
const OUT = join(ROOT, 'docs', 'manual');
const SHOTS_DIR = join(OUT, 'assets', 'shots');
const SHOTS_JSON = join(OUT, 'assets', 'shots.json');
const META_DIR = join(ROOT, 'output', 'manual', 'meta');
const CONTENT = join(import.meta.dirname, 'content');
const SRC_ASSETS = join(import.meta.dirname, 'assets');

const strict = !process.argv.includes('--allow-unreferenced');

interface ShotMeta {
  id: string;
  role: string;
  title: string;
  shows: string;
  url: string;
  file: string;
  bytes: number;
  clipped: boolean;
  pageHeight: number;
  headings: string[];
  findings: { kind: string; detail: string }[];
}

const errors: string[] = [];
const warnings: string[] = [];

// ------------------------------------------------------------------ manifest

/**
 * Merges the per-shot JSON the capture run wrote into one manifest.
 *
 * Capture writes one file per shot precisely because it runs in parallel and
 * concurrent appends to a single manifest would interleave. If no capture has
 * run since output/ was last cleaned, the committed manifest is kept rather
 * than being replaced with an empty one.
 */
function buildManifest(): ShotMeta[] {
  if (existsSync(META_DIR)) {
    const files = readdirSync(META_DIR).filter((f) => f.endsWith('.json'));
    if (files.length > 0) {
      const shots = files
        .map((f) => JSON.parse(readFileSync(join(META_DIR, f), 'utf8')) as ShotMeta)
        .sort((a, b) => a.id.localeCompare(b.id));
      mkdirSync(dirname(SHOTS_JSON), { recursive: true });
      writeFileSync(SHOTS_JSON, JSON.stringify({ shots }, null, 2));
      return shots;
    }
  }
  if (existsSync(SHOTS_JSON)) {
    warnings.push('no capture metadata in output/manual/meta — using the committed shots.json');
    return (JSON.parse(readFileSync(SHOTS_JSON, 'utf8')) as { shots: ShotMeta[] }).shots;
  }
  errors.push('no shot manifest and no capture metadata: run the capture first');
  return [];
}

/** Commit and dataset facts, stamped on the cover so a stale manual is obvious. */
function provenance(): { sha: string; counts: string } {
  let sha = 'unknown';
  try {
    sha = execFileSync('git', ['rev-parse', '--short', 'HEAD'], { cwd: ROOT, encoding: 'utf8' }).trim();
  } catch {
    warnings.push('could not read the git SHA for the cover');
  }
  let counts = '';
  try {
    const db = join(ROOT, 'data', 'fervid.db');
    const sql =
      "select (select count(*) from users)||' people, '||(select count(*) from projects)||' projects, '" +
      "||(select count(*) from payment_requests)||' requests, '||(select count(*) from payments)||' payments';";
    counts = execFileSync('sqlite3', ['-readonly', db, sql], { encoding: 'utf8' }).trim();
  } catch {
    warnings.push('could not read dataset counts for the cover');
  }
  return { sha, counts };
}

// -------------------------------------------------------------------- markup

const esc = (s: string): string =>
  s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

/** Pages are addressed as `page:section/slug`; the build resolves and checks them. */
function resolveHref(href: string, depth: number, where: string): string {
  if (href.startsWith('page:')) {
    const target = href.slice(5);
    if (!SITE.some((p) => `${p.section}/${p.slug}` === target)) {
      errors.push(`${where}: link to unknown page "${target}"`);
    }
    return `${up(depth)}${target}.html`;
  }
  if (href.startsWith('#') || /^https?:\/\//.test(href)) return href;
  errors.push(`${where}: link "${href}" is neither a page: reference, an anchor, nor an absolute URL`);
  return href;
}

const up = (depth: number): string => (depth === 0 ? '' : '../'.repeat(depth));

/**
 * The authoring markup: bold, italics, code and links. Deliberately tiny —
 * content is escaped first, so an author cannot inject markup that would break
 * the page chrome, and there is exactly one way to express each idea.
 */
function inline(text: string, depth: number, where: string): string {
  let s = esc(text);
  s = s.replace(/`([^`]+)`/g, (_m, c) => `<code>${c}</code>`);
  s = s.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_m, label, href) => {
    const resolved = resolveHref(href, depth, where);
    const external = /^https?:\/\//.test(resolved);
    return `<a href="${esc(resolved)}"${external ? ' rel="noreferrer"' : ''}>${label}</a>`;
  });
  s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  s = s.replace(/(^|[^*])\*([^*]+)\*/g, '$1<em>$2</em>');
  return s;
}

const paragraphs = (text: string, depth: number, where: string): string =>
  text
    .trim()
    .split(/\n\s*\n/)
    .map((p) => `<p>${inline(p.trim().replace(/\s*\n\s*/g, ' '), depth, where)}</p>`)
    .join('\n');

// --------------------------------------------------------------- block render

function renderShot(id: string, caption: string, depth: number, where: string, shots: Map<string, ShotMeta>, used: Set<string>): string {
  const meta = shots.get(id);
  if (!meta) {
    errors.push(`${where}: no screenshot captured with id "${id}"`);
    return '';
  }
  if (!existsSync(join(SHOTS_DIR, `${id}.png`))) {
    errors.push(`${where}: shot "${id}" is in the manifest but the PNG is missing on disk`);
    return '';
  }
  used.add(id);
  const src = `${up(depth)}assets/shots/${id}.png`;
  const clipNote = meta.clipped
    ? '<span class="shot-note">Long screen — showing the top of the page.</span>'
    : '';
  return `<figure class="shot">
  <button class="shot-btn" type="button" data-full="${esc(src)}" aria-label="Enlarge: ${esc(caption)}">
    <img src="${esc(src)}" alt="${esc(meta.shows)}" loading="lazy" width="1440">
  </button>
  <figcaption>${inline(caption, depth, where)} ${clipNote}</figcaption>
</figure>`;
}

function renderBlock(b: Block, depth: number, where: string, shots: Map<string, ShotMeta>, used: Set<string>): string {
  switch (b.kind) {
    case 'prose':
      return paragraphs(b.text, depth, where);

    case 'section':
      return `<h2 id="${slugify(b.title)}">${inline(b.title, depth, where)}</h2>`;

    case 'subsection':
      return `<h3 id="${slugify(b.title)}">${inline(b.title, depth, where)}</h3>`;

    case 'shot':
      return renderShot(b.id, b.caption, depth, where, shots, used);

    case 'steps': {
      const intro = b.intro ? `<p>${inline(b.intro, depth, where)}</p>` : '';
      const items = b.steps
        .map((s) => {
          const shotHtml = s.shot ? renderShot(s.shot, '', depth, where, shots, used) : '';
          const noteHtml = s.note ? `<p class="step-note">${inline(s.note, depth, where)}</p>` : '';
          return `<li>${inline(s.text, depth, where)}${noteHtml}${shotHtml}</li>`;
        })
        .join('\n');
      return `${intro}<ol class="steps">\n${items}\n</ol>`;
    }

    case 'callout': {
      const title = b.title ? `<p class="callout-title">${inline(b.title, depth, where)}</p>` : '';
      return `<aside class="callout callout-${b.tone}">${title}${paragraphs(b.text, depth, where)}</aside>`;
    }

    case 'table': {
      const bad = b.rows.find((r) => r.length !== b.headers.length);
      if (bad) errors.push(`${where}: a table row has ${bad.length} cells but there are ${b.headers.length} headers`);
      const head = b.headers.map((h) => `<th scope="col">${inline(h, depth, where)}</th>`).join('');
      const body = b.rows
        .map((r) => `<tr>${r.map((c) => `<td>${inline(c, depth, where)}</td>`).join('')}</tr>`)
        .join('\n');
      const cap = b.caption ? `<caption>${inline(b.caption, depth, where)}</caption>` : '';
      return `<div class="table-wrap"><table>${cap}<thead><tr>${head}</tr></thead><tbody>\n${body}\n</tbody></table></div>`;
    }

    case 'fields': {
      const body = b.rows
        .map(
          (r) =>
            `<tr><td><strong>${inline(r.name, depth, where)}</strong></td>` +
            `<td>${r.required ? '<span class="req">Required</span>' : '<span class="opt">Optional</span>'}</td>` +
            `<td>${inline(r.note, depth, where)}</td></tr>`,
        )
        .join('\n');
      const cap = b.caption ? `<caption>${inline(b.caption, depth, where)}</caption>` : '';
      return `<div class="table-wrap"><table class="fields">${cap}<thead><tr><th scope="col">Field</th><th scope="col"></th><th scope="col">What it is for</th></tr></thead><tbody>\n${body}\n</tbody></table></div>`;
    }

    case 'faq':
      return `<dl class="faq">${b.items
        .map((i) => `<dt>${inline(i.q, depth, where)}</dt><dd>${paragraphs(i.a, depth, where)}</dd>`)
        .join('\n')}</dl>`;
  }
}

const slugify = (s: string): string =>
  s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');

// ---------------------------------------------------------------- page chrome

function sidebar(current: string, depth: number): string {
  const groups = SECTIONS.map((sec: SectionDef) => {
    const pages = SITE.filter((p) => p.section === sec.key);
    const items = pages
      .map((p) => {
        const id = `${p.section}/${p.slug}`;
        const cls = id === current ? ' class="here" aria-current="page"' : '';
        return `<li><a href="${up(depth)}${id}.html"${cls}>${esc(p.title)}</a></li>`;
      })
      .join('\n');
    return `<li class="nav-group">
  <p class="nav-head">${esc(sec.title)}</p>
  <ul>\n${items}\n</ul>
</li>`;
  }).join('\n');
  return `<nav class="sidebar" aria-label="Manual contents">
  <a class="brand" href="${up(depth)}index.html"><span class="mark">F</span><span><strong>Fervid Budget</strong><small>User manual</small></span></a>
  <ul class="nav">\n${groups}\n</ul>
</nav>`;
}

function shell(opts: {
  title: string;
  depth: number;
  current: string;
  breadcrumb: string;
  body: string;
}): string {
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(opts.title)} — Fervid Budget manual</title>
<link rel="stylesheet" href="${up(opts.depth)}assets/manual.css">
</head>
<body>
${sidebar(opts.current, opts.depth)}
<main class="doc">
<div class="wrap">
${opts.breadcrumb}
${opts.body}
</div>
</main>
<div class="lightbox" id="lightbox" hidden>
  <button class="lightbox-close" type="button" aria-label="Close">&times;</button>
  <img src="" alt="">
</div>
<script src="${up(opts.depth)}assets/manual.js"></script>
</body>
</html>
`;
}

// --------------------------------------------------------------------- render

async function main(): Promise<void> {
  const shotList = buildManifest();
  const shots = new Map(shotList.map((s) => [s.id, s]));
  const used = new Set<string>();

  mkdirSync(join(OUT, 'assets'), { recursive: true });
  for (const asset of ['manual.css', 'manual.js']) {
    const src = join(SRC_ASSETS, asset);
    if (existsSync(src)) copyFileSync(src, join(OUT, 'assets', asset));
    else errors.push(`missing source asset ${asset}`);
  }

  // Pages.
  for (const decl of SITE) {
    const file = join(CONTENT, decl.section, `${decl.slug}.mts`);
    if (!existsSync(file)) {
      errors.push(`no content file for ${decl.section}/${decl.slug} (expected ${relative(ROOT, file)})`);
      continue;
    }
    const mod = (await import(pathToFileURL(file).href)) as { default?: Page };
    const p = mod.default;
    if (!p) {
      errors.push(`${relative(ROOT, file)}: no default export`);
      continue;
    }
    if (p.section !== decl.section || p.slug !== decl.slug) {
      errors.push(`${relative(ROOT, file)}: declares ${p.section}/${p.slug} but lives at ${decl.section}/${decl.slug}`);
      continue;
    }

    const where = `${decl.section}/${decl.slug}`;
    const depth = 1;
    const sec = SECTIONS.find((s) => s.key === decl.section)!;
    const body = [
      `<header class="page-head"><h1>${inline(p.title, depth, where)}</h1><p class="lede">${inline(p.summary, depth, where)}</p></header>`,
      ...p.blocks.map((b) => renderBlock(b, depth, where, shots, used)),
      pager(decl, depth),
    ].join('\n');
    const breadcrumb = `<p class="crumb"><a href="${up(depth)}index.html">Manual</a> <span>/</span> ${esc(sec.title)}</p>`;

    const html = shell({ title: p.title, depth, current: where, breadcrumb, body });
    const outFile = join(OUT, decl.section, `${decl.slug}.html`);
    mkdirSync(dirname(outFile), { recursive: true });
    writeFileSync(outFile, html);
  }

  // Cover.
  writeFileSync(join(OUT, 'index.html'), cover(shotList));

  // Every captured shot must earn its place in the repo.
  const unused = shotList.map((s) => s.id).filter((id) => !used.has(id));
  if (unused.length > 0) {
    const msg = `${unused.length} captured screenshot(s) are not shown on any page:\n    ` + unused.join('\n    ');
    if (strict) errors.push(msg);
    else warnings.push(msg);
  }

  for (const w of warnings) console.warn(`warning: ${w}`);
  if (errors.length > 0) {
    console.error(`\nBuild failed with ${errors.length} error(s):`);
    for (const e of errors) console.error(`  - ${e}`);
    process.exit(1);
  }
  const bytes = dirSize(OUT);
  console.log(`Built ${SITE.length} pages, ${used.size}/${shotList.length} shots shown, ${(bytes / 1024 / 1024).toFixed(1)} MB total.`);
}

function pager(decl: { section: SectionKey; slug: string }, depth: number): string {
  const within = SITE.filter((p) => p.section === decl.section);
  const i = within.findIndex((p) => p.slug === decl.slug);
  const prev = within[i - 1];
  const next = within[i + 1];
  const link = (p: typeof prev, dir: string) =>
    p ? `<a class="pager-${dir}" href="${up(depth)}${p.section}/${p.slug}.html"><span>${dir === 'prev' ? 'Previous' : 'Next'}</span>${esc(p.title)}</a>` : '';
  if (!prev && !next) return '';
  return `<nav class="pager" aria-label="Within this section">${link(prev, 'prev')}${link(next, 'next')}</nav>`;
}

function cover(shotList: ShotMeta[]): string {
  const { sha, counts } = provenance();
  const cards = SECTIONS.map((sec) => {
    const pages = SITE.filter((p) => p.section === sec.key);
    const first = pages[0];
    return `<a class="role-card" href="${first ? `${first.section}/${first.slug}.html` : '#'}">
  <h3>${esc(sec.title)}</h3>
  <p>${esc(sec.blurb)}</p>
  <span class="count">${pages.length} page${pages.length === 1 ? '' : 's'}</span>
</a>`;
  }).join('\n');

  const body = `<header class="cover">
  <p class="eyebrow">Fervid Budget</p>
  <h1>User manual</h1>
  <p class="lede">How to raise, approve, pay and administer payment requests — every screen, photographed from the running product.</p>
</header>
<section class="roles">${cards}</section>
<section class="provenance">
  <h2 id="about-this-manual">About this manual</h2>
  <p>Every screenshot in these pages was captured from a running copy of Fervid Budget, not drawn or mocked up. Where a screen's appearance depends on your permissions or on the state of a record, the page says so.</p>
  <dl>
    <dt>Built from commit</dt><dd><code>${esc(sha)}</code></dd>
    <dt>Screenshots</dt><dd>${shotList.length}</dd>
    ${counts ? `<dt>Illustrated with</dt><dd>${esc(counts)}</dd>` : ''}
  </dl>
</section>`;
  return shell({ title: 'User manual', depth: 0, current: '', breadcrumb: '', body });
}

function dirSize(dir: string): number {
  let total = 0;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, entry.name);
    total += entry.isDirectory() ? dirSize(p) : statSync(p).size;
  }
  return total;
}

await main();
