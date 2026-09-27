#!/usr/bin/env python3
"""uirev: a frontend-only UI review toolkit.

One long-lived browser per session (a small local daemon holds a Playwright
page), driven by short CLI commands so an agent can explore interactively.
Everything is measured from the rendered page: contrast from screenshot
pixels, focus indicators from before/after pixel diffs, sizes from layout
boxes. Nothing here reads application source code.

  uirev.py start   --run DIR --session S --device desktop|tablet|mobile [--state auth.json]
  uirev.py <cmd>   --run DIR --session S [args]      (see `uirev.py help`)
  uirev.py stop    --run DIR --session S
  uirev.py login   URL --save auth.json               (headed, for a person)
  uirev.py report  --run DIR                          (builds DIR/report.html)
"""
import argparse, base64, colorsys, html, math, io, json, os, re, socket, subprocess, sys, time
import urllib.request
from http.server import BaseHTTPRequestHandler, HTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))

DEVICES = {
    # CSS viewport sizes of common reference devices.
    "desktop": dict(viewport={"width": 1440, "height": 900}, device_scale_factor=1,
                    is_mobile=False, has_touch=False),
    "tablet": dict(viewport={"width": 820, "height": 1180}, device_scale_factor=2,
                   is_mobile=True, has_touch=True,
                   user_agent="Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"),
    "mobile": dict(viewport={"width": 390, "height": 844}, device_scale_factor=2,
                   is_mobile=True, has_touch=True,
                   user_agent="Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"),
}

# ---------------------------------------------------------------- colour maths

def lin(c):
    c = c / 255.0
    return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4

def lum(rgb):
    r, g, b = rgb[:3]
    return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)

def ratio(a, b):
    la, lb = lum(a), lum(b)
    hi, lo = max(la, lb), min(la, lb)
    return math.floor((hi + 0.05) / (lo + 0.05) * 100) / 100   # floor: 4.499 must never display as a pass

def over(fg, bg):
    """Composite rgba fg (alpha 0..1) over opaque rgb bg."""
    a = fg[3] if len(fg) > 3 else 1.0
    return tuple(round(fg[i] * a + bg[i] * (1 - a)) for i in range(3))

def hexc(rgb):
    return "#%02x%02x%02x" % tuple(int(v) for v in rgb[:3])

# ------------------------------------------------------------------ page JS

JS_HELPERS = r"""
window.__uirev = window.__uirev || {};
(() => {
const U = window.__uirev;
U.visible = (el) => {
  if (!el || !el.isConnected) return false;
  const s = getComputedStyle(el);
  if (s.display === 'none' || s.visibility === 'hidden' || parseFloat(s.opacity) === 0) return false;
  const r = el.getBoundingClientRect();
  return r.width > 0 && r.height > 0;
};
U.opacity = (el) => { let o = 1; for (let e = el; e && e.nodeType === 1; e = e.parentElement) o *= parseFloat(getComputedStyle(e).opacity); return o; };
U.name = (el) => {
  const t = (s) => (s || '').replace(/\s+/g, ' ').trim().slice(0, 80);
  if (el.getAttribute('aria-label')) return t(el.getAttribute('aria-label'));
  const lb = el.getAttribute('aria-labelledby');
  if (lb) return t(lb.split(/\s+/).map(i => (document.getElementById(i) || {}).textContent || '').join(' '));
  if (el.labels && el.labels.length) return t(el.labels[0].textContent);
  if (el.tagName === 'IMG') return t(el.alt);
  if (el.tagName === 'INPUT' && ['submit','button','reset'].includes(el.type)) return t(el.value);
  const txt = t(el.innerText || el.textContent);
  if (txt) return txt;
  return t(el.getAttribute('title') || el.getAttribute('placeholder') || '');
};
U.role = (el) => {
  const r = el.getAttribute('role'); if (r) return r;
  const tag = el.tagName.toLowerCase();
  if (tag === 'a') return el.hasAttribute('href') ? 'link' : 'generic';
  if (tag === 'button') return 'button';
  if (tag === 'select') return 'combobox';
  if (tag === 'textarea') return 'textbox';
  if (tag === 'summary') return 'button';
  if (tag === 'input') return ({checkbox:'checkbox',radio:'radio',range:'slider',submit:'button',button:'button',reset:'button',file:'file',search:'searchbox'})[el.type] || 'textbox';
  return tag;
};
U.INTERACTIVE = 'a[href],button,input:not([type=hidden]),select,textarea,summary,[role=button],[role=link],[role=tab],[role=menuitem],[role=checkbox],[role=radio],[role=switch],[role=option],[tabindex]:not([tabindex="-1"]),[contenteditable=true],label[for]';
U.ref = (el) => { if (!el.dataset.uirev) { U.n = (U.n || 0) + 1; el.dataset.uirev = 'e' + U.n; } return el.dataset.uirev; };
U.byRef = (r) => document.querySelector('[data-uirev="' + r + '"]');
U.rect = (el) => { const r = el.getBoundingClientRect(); return {x: Math.round(r.x + scrollX), y: Math.round(r.y + scrollY), w: Math.round(r.width), h: Math.round(r.height)}; };
})();
"""

JS_SNAPSHOT = r"""
() => {
  const U = window.__uirev;
  const out = {url: location.href, title: document.title, lang: document.documentElement.lang || null,
    viewport: {w: innerWidth, h: innerHeight}, doc: {w: document.documentElement.scrollWidth, h: document.documentElement.scrollHeight},
    scroll: {x: scrollX, y: scrollY}, headings: [], landmarks: [], elements: [], dialogs: []};
  document.querySelectorAll('h1,h2,h3,h4,h5,h6,[role=heading]').forEach(h => { if (U.visible(h)) out.headings.push({level: h.tagName[1] ? +h.tagName[1] : +(h.getAttribute('aria-level')||2), text: U.name(h)}); });
  document.querySelectorAll('header,nav,main,footer,aside,form[aria-label],section[aria-label],[role=banner],[role=navigation],[role=main],[role=contentinfo],[role=search]').forEach(l => { if (U.visible(l)) out.landmarks.push({tag: l.tagName.toLowerCase(), role: l.getAttribute('role'), label: l.getAttribute('aria-label')}); });
  document.querySelectorAll('dialog[open],[role=dialog],[role=alertdialog],[aria-modal=true]').forEach(d => { if (U.visible(d)) out.dialogs.push({ref: U.ref(d), name: U.name(d).slice(0, 60)}); });
  const seen = new Set();
  document.querySelectorAll(U.INTERACTIVE).forEach(el => {
    if (seen.has(el) || !U.visible(el)) return; seen.add(el);
    const r = el.getBoundingClientRect(); const s = getComputedStyle(el);
    const inView = r.bottom > 0 && r.right > 0 && r.top < innerHeight && r.left < innerWidth;
    const e = {ref: U.ref(el), role: U.role(el), name: U.name(el), tag: el.tagName.toLowerCase(),
      box: U.rect(el), inView};
    if (el.disabled || el.getAttribute('aria-disabled') === 'true') e.disabled = true;
    if (el.type && el.tagName === 'INPUT') e.type = el.type;
    if ('value' in el && el.tagName !== 'BUTTON' && el.value) e.value = String(el.value).slice(0, 40);
    if (el.required) e.required = true;
    if (el.getAttribute('aria-expanded')) e.expanded = el.getAttribute('aria-expanded');
    if (el.checked) e.checked = true;
    if (el.tagName === 'A') e.href = el.getAttribute('href');
    if (s.position === 'fixed' || s.position === 'sticky') e.pinned = s.position;
    out.elements.push(e);
  });
  return out;
}
"""

JS_HIDDEN = r"""
() => {
  const U = window.__uirev, out = [];
  const docW = document.documentElement.scrollWidth;
  document.querySelectorAll(U.INTERACTIVE).forEach(el => {
    if (out.length >= 200) return;
    const s = getComputedStyle(el), r = el.getBoundingClientRect();
    let why = null;
    if (el.closest('[hidden]') && !U.visible(el)) why = 'hidden-attr';
    else if (s.display === 'none' || (el.offsetParent === null && s.position !== 'fixed' && !U.visible(el))) why = 'display-none';
    else if (s.visibility === 'hidden') why = 'visibility-hidden';
    else if (U.opacity(el) === 0) why = 'opacity-0';
    else if (r.width <= 1 || r.height <= 1) why = 'zero-size';
    else {
      const ax = r.left + scrollX, ay = r.top + scrollY;
      if (ax + r.width <= 0 || ay + r.height <= 0 || ax >= docW) why = 'off-screen';
    }
    if (!why) return;
    let host = el.parentElement;
    while (host && host !== document.body && !U.visible(host)) host = host.parentElement;
    const it = {ref: U.ref(el), name: U.name(el), role: U.role(el), tag: el.tagName.toLowerCase(), why};
    if (el.tagName === 'A') it.href = el.getAttribute('href');
    if (host && host !== document.body) { it.hostRef = U.ref(host); it.hostName = U.name(host).slice(0, 60); }
    out.push(it);
  });
  return out;
}
"""

JS_TEXT_ITEMS = r"""
(scope) => {
  const U = window.__uirev;
  const root = scope ? U.byRef(scope) : document.body;
  const items = [];
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {acceptNode: n => n.nodeValue.trim() ? 1 : 2});
  const done = new Set();
  let n;
  while ((n = walker.nextNode())) {
    const el = n.parentElement; if (!el || done.has(el) || !U.visible(el)) continue;
    if (['SCRIPT','STYLE','NOSCRIPT','TITLE','OPTION'].includes(el.tagName)) continue;
    done.add(el);
    const range = document.createRange(); range.selectNodeContents(n);
    const rects = [...range.getClientRects()].filter(r => r.width >= 2 && r.height >= 4);
    if (!rects.length) continue;
    const s = getComputedStyle(el);
    const m = s.color.match(/[\d.]+/g).map(Number);
    const ctl = el.closest('button,input,select,textarea,[aria-disabled=true],fieldset[disabled]');
    items.push({ref: U.ref(el), tag: el.tagName.toLowerCase(), text: n.nodeValue.replace(/\s+/g,' ').trim().slice(0, 60),
      rects: rects.slice(0, 4).map(r => ({x: r.x, y: r.y, w: r.width, h: r.height})),
      fg: [m[0], m[1], m[2], (m[3] === undefined ? 1 : m[3]) * U.opacity(el)],
      size: parseFloat(s.fontSize), weight: parseInt(s.fontWeight) || 400,
      disabled: !!(ctl && (ctl.disabled || ctl.getAttribute('aria-disabled') === 'true')),
      hiddenFromAT: !!el.closest('[aria-hidden=true]')});
  }
  document.querySelectorAll('input[placeholder],textarea[placeholder]').forEach(el => {
    if (!U.visible(el) || el.value || (scope && !root.contains(el))) return;
    const s = getComputedStyle(el, '::placeholder'); const m = s.color.match(/[\d.]+/g).map(Number);
    const r = el.getBoundingClientRect(); const pl = parseFloat(getComputedStyle(el).paddingLeft);
    items.push({ref: U.ref(el), tag: 'placeholder', text: el.placeholder.slice(0, 60),
      rects: [{x: r.x + pl, y: r.y + 2, w: Math.max(4, Math.min(r.width - pl * 2, el.placeholder.length * parseFloat(s.fontSize) * 0.5)), h: r.height - 4}],
      fg: [m[0], m[1], m[2], (m[3] === undefined ? 1 : m[3]) * U.opacity(el)], size: parseFloat(s.fontSize), weight: parseInt(s.fontWeight) || 400, disabled: el.disabled});
  });
  return items;
}
"""

HIDE_TEXT_CSS = """*,*::before,*::after,*::placeholder{color:transparent!important;-webkit-text-fill-color:transparent!important;text-shadow:none!important;caret-color:transparent!important;text-decoration-color:transparent!important}"""

JS_COMPONENTS = r"""
(scope) => {
  const U = window.__uirev; const root = scope ? U.byRef(scope) : document;
  const out = [];
  root.querySelectorAll('input:not([type=hidden]),select,textarea,button,[role=button],[role=checkbox],[role=radio],[role=switch],[role=tab],[role=slider]').forEach(el => {
    if (!U.visible(el)) return;
    const r = el.getBoundingClientRect(); const s = getComputedStyle(el);
    if (r.bottom < 0 || r.top > innerHeight) return;
    const col = (v) => { const m = (v.match(/[\d.]+/g) || [0,0,0,0]).map(Number); return [m[0], m[1], m[2], m[3] === undefined ? 1 : m[3]]; };
    const bw = Math.max(...['Top','Right','Bottom','Left'].map(k => parseFloat(s['border' + k + 'Width']) || 0));
    out.push({ref: U.ref(el), role: U.role(el), name: U.name(el), rect: {x: r.x, y: r.y, w: r.width, h: r.height},
      border: bw > 0 && s.borderStyle !== 'none' ? col(s.borderTopColor) : null, borderWidth: bw,
      fill: col(s.backgroundColor), disabled: !!(el.disabled || el.getAttribute('aria-disabled') === 'true'),
      appearanceNative: s.appearance !== 'none' && ['checkbox','radio'].includes(el.type)});
  });
  return out;
}
"""

JS_LAYOUT = r"""
() => {
  const U = window.__uirev; const vw = innerWidth; const out = {};
  const de = document.documentElement;
  out.horizontalScroll = de.scrollWidth > vw + 1 ? {docWidth: de.scrollWidth, viewport: vw} : null;
  out.offscreen = []; out.clipped = []; out.smallText = []; out.smallTargets = []; out.overlaps = [];
  out.imagesNoAlt = []; out.brokenImages = []; out.longLines = []; out.smallInputs = []; out.unlabeled = [];
  const all = document.body.querySelectorAll('*');
  for (const el of all) {
    if (!U.visible(el)) continue;
    const r = el.getBoundingClientRect(); const s = getComputedStyle(el);
    if (r.right > vw + 1 && s.position !== 'fixed' && r.width < vw * 3 && !el.closest('[style*="overflow"]')) {
      let p = el.parentElement, scrolls = false;
      while (p) { const ps = getComputedStyle(p); if (/(auto|scroll|hidden)/.test(ps.overflowX)) { scrolls = true; break; } p = p.parentElement; }
      if (!scrolls) out.offscreen.push({ref: U.ref(el), tag: el.tagName.toLowerCase(), right: Math.round(r.right), text: U.name(el).slice(0, 40)});
    }
    const hasText = [...el.childNodes].some(n => n.nodeType === 3 && n.nodeValue.trim());
    if (hasText) {
      if ((el.scrollWidth > el.clientWidth + 1 && /(hidden|clip)/.test(s.overflowX)) || (el.scrollHeight > el.clientHeight + 1 && /(hidden|clip)/.test(s.overflowY) && s.webkitLineClamp === 'none'))
        out.clipped.push({ref: U.ref(el), text: U.name(el).slice(0, 50), ellipsis: s.textOverflow === 'ellipsis'});
      const fs = parseFloat(s.fontSize);
      if (fs < 12) out.smallText.push({ref: U.ref(el), size: fs, text: U.name(el).slice(0, 40)});
      if (['P','LI','DD','BLOCKQUOTE'].includes(el.tagName)) {
        const ch = r.width / (fs * 0.5); if (ch > 95 && el.innerText.length > 120) out.longLines.push({ref: U.ref(el), approxChars: Math.round(ch)});
      }
    }
    if (el.tagName === 'IMG') {
      if (!el.hasAttribute('alt')) out.imagesNoAlt.push({ref: U.ref(el), src: (el.currentSrc || el.src).slice(-60)});
      if (el.complete && el.naturalWidth === 0) out.brokenImages.push({ref: U.ref(el), src: (el.currentSrc || el.src).slice(-60)});
    }
  }
  const targets = [...document.querySelectorAll(U.INTERACTIVE)].filter(el => U.visible(el) && el.tagName !== 'LABEL');
  const boxes = targets.map(el => ({el, r: el.getBoundingClientRect()}));
  const inlineLink = (el) => el.tagName === 'A' && getComputedStyle(el).display === 'inline' && el.parentElement && (el.parentElement.innerText || '').length > (el.innerText || '').length + 20;
  for (const {el, r} of boxes) {
    if (inlineLink(el)) continue;
    const w = r.width, h = r.height;
    if (w < 44 || h < 44) {
      const cx = r.x + w / 2, cy = r.y + h / 2;
      let spaced = true;
      for (const o of boxes) { if (o.el === el || o.el.contains(el) || el.contains(o.el)) continue;
        const ox = Math.max(o.r.left, Math.min(cx, o.r.right)), oy = Math.max(o.r.top, Math.min(cy, o.r.bottom));
        if (Math.hypot(ox - cx, oy - cy) < 12 && !(w >= 24 && h >= 24)) { spaced = false; break; } }
      out.smallTargets.push({ref: U.ref(el), role: U.role(el), name: U.name(el).slice(0, 40), w: Math.round(w), h: Math.round(h),
        wcag258: (w >= 24 && h >= 24) ? 'pass' : (spaced ? 'pass (spacing exception)' : 'FAIL'), below44: true});
    }
  }
  for (let i = 0; i < boxes.length; i++) for (let j = i + 1; j < boxes.length; j++) {
    const a = boxes[i], b = boxes[j]; if (a.el.contains(b.el) || b.el.contains(a.el)) continue;
    const ix = Math.min(a.r.right, b.r.right) - Math.max(a.r.left, b.r.left), iy = Math.min(a.r.bottom, b.r.bottom) - Math.max(a.r.top, b.r.top);
    if (ix > 2 && iy > 2) out.overlaps.push({a: U.ref(a.el), b: U.ref(b.el), aName: U.name(a.el).slice(0, 30), bName: U.name(b.el).slice(0, 30), px: Math.round(ix * iy)});
  }
  document.querySelectorAll('input:not([type=hidden]):not([type=checkbox]):not([type=radio]),select,textarea').forEach(el => {
    if (!U.visible(el)) return;
    const fs = parseFloat(getComputedStyle(el).fontSize);
    if (fs < 16) out.smallInputs.push({ref: U.ref(el), size: fs, name: U.name(el).slice(0, 40)});
    const labelled = el.getAttribute('aria-label') || el.getAttribute('aria-labelledby') || (el.labels && el.labels.length) || el.getAttribute('title');
    if (!labelled) out.unlabeled.push({ref: U.ref(el), placeholderOnly: !!el.placeholder, name: U.name(el).slice(0, 40)});
  });
  for (const k of Object.keys(out)) if (Array.isArray(out[k]) && out[k].length > 40) out[k] = out[k].slice(0, 40).concat([{truncated: out[k].length}]);
  return out;
}
"""

JS_MARKS = r"""
(marks) => {
  // Draws numbered boxes and callouts. Fixed-position elements are marked in a fixed layer so the mark
  // stays on them; callouts are placed where they cover no other marked box and stay inside the viewport.
  const U = window.__uirev;
  const find = (ref) => { if (/^e\d+$/.test(ref)) return U.byRef(ref); try { return document.querySelector(ref); } catch (e) { return null; } };
  const isFixed = (el) => { for (let e = el; e && e.nodeType === 1; e = e.parentElement) if (getComputedStyle(e).position === 'fixed') return true; return false; };
  const mk = (fixed) => { const l = document.createElement('div'); l.className = '__uirev_marks';
    l.style.cssText = `position:${fixed ? 'fixed' : 'absolute'};left:0;top:0;width:0;height:0;z-index:2147483647;pointer-events:none`; document.body.appendChild(l); return l; };
  const layers = {abs: mk(false), fix: mk(true)};
  const colors = {critical: '#d0021b', major: '#e8590c', minor: '#b58900', info: '#1971c2', pass: '#2b8a3e'};
  const missing = [], items = [];
  marks.forEach((m, i) => {
    let b = m.box, fixed = false;
    if (m.ref) { const el = find(m.ref); if (!el) { missing.push(m.ref); return; }
      const r = el.getBoundingClientRect(); fixed = isFixed(el);
      b = fixed ? {x: r.x, y: r.y, w: r.width, h: r.height} : {x: r.x + scrollX, y: r.y + scrollY, w: r.width, h: r.height}; }
    items.push({m, i, b, fixed});
  });
  // all boxes in page coordinates, for collision checks
  const pageBox = (it) => it.fixed ? {x: it.b.x + scrollX, y: it.b.y + scrollY, w: it.b.w, h: it.b.h} : it.b;
  const boxes = items.map(it => { const p = pageBox(it); return {x: p.x - 4, y: p.y - 4, w: p.w + 8, h: p.h + 8}; });
  const hit = (a, b) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
  const placed = [], tags = [];
  const vw = document.documentElement.clientWidth;
  items.forEach((it, k) => {
    const c = colors[it.m.color] || it.m.color || '#d0021b';
    const L = it.fixed ? layers.fix : layers.abs;
    const box = document.createElement('div');
    box.style.cssText = `position:absolute;left:${it.b.x - 4}px;top:${it.b.y - 4}px;width:${it.b.w + 8}px;height:${it.b.h + 8}px;border:3px solid ${c};border-radius:4px;box-shadow:0 0 0 2px #fff,inset 0 0 0 1px #fff;box-sizing:border-box`;
    const tag = document.createElement('div');
    tag.textContent = it.m.label || String(it.i + 1);
    tag.style.cssText = `position:absolute;left:0;top:0;background:${c};color:#fff;font:700 13px/1.2 -apple-system,system-ui,sans-serif;padding:4px 7px;border-radius:4px;border:2px solid #fff;white-space:nowrap;max-width:${Math.min(360, vw - 8)}px;overflow:hidden;text-overflow:ellipsis`;
    L.appendChild(box); L.appendChild(tag);
    const tw = tag.offsetWidth, th = tag.offsetHeight;
    const pb = pageBox(it);
    const x = Math.max(scrollX + 2, Math.min(pb.x - 4, scrollX + vw - tw - 2));
    const cands = [pb.y - th - 8, pb.y + pb.h + 8, pb.y - th - 8 - th, pb.y + pb.h + 8 + th, pb.y + 2];
    let y = cands[0], best = null;
    for (const cy of cands) {
      const r = {x, y: cy, w: tw, h: th};
      if (cy < scrollY && !it.fixed && cy !== cands[4]) continue;
      if (boxes.every((bx, j) => !hit(r, bx)) && tags.every(t => !hit(r, t))) { best = cy; break; }
    }
    y = best === null ? cands[0] : best;
    const r = {x, y, w: tw, h: th};
    tags.push(r);
    const off = it.fixed ? {x: scrollX, y: scrollY} : {x: 0, y: 0};
    tag.style.left = (x - off.x) + 'px'; tag.style.top = (y - off.y) + 'px';
    const bb = boxes[k];
    placed.push({label: tag.textContent, fixed: it.fixed, box: {x: Math.round(bb.x), y: Math.round(bb.y), w: Math.round(bb.w), h: Math.round(bb.h)},
                 tag: {x: Math.round(x), y: Math.round(y), w: tw, h: th}});
  });
  return {missing, placed};
}
"""

JS_REDACT = r"""
(terms) => {
  // Blanks every listed term in text nodes and input values; returns how many were hidden.
  const saved = window.__uirevRedact = [];
  let n = 0;
  const esc = t => t.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const re = new RegExp(terms.map(esc).join('|'), 'gi');
  const blank = s => s.replace(re, m => { n++; return '\u2588'.repeat(Math.min(m.length, 12)); });
  const w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  for (let t; (t = w.nextNode());) { re.lastIndex = 0; if (re.test(t.nodeValue)) { saved.push([t, 'nodeValue', t.nodeValue]); t.nodeValue = blank(t.nodeValue); } }
  document.querySelectorAll('input,textarea').forEach(el => { re.lastIndex = 0; if (re.test(el.value)) { saved.push([el, 'value', el.value]); el.value = blank(el.value); } });
  return n;
}
"""

JS_UNREDACT = "() => { (window.__uirevRedact || []).forEach(([n, k, v]) => { n[k] = v; }); window.__uirevRedact = []; }"

# ------------------------------------------------------------------ daemon

class Daemon:
    def __init__(self, a):
        from playwright.sync_api import sync_playwright
        self.a = a
        self.run = os.path.abspath(a.run)
        self.shots = os.path.join(self.run, "shots")
        os.makedirs(self.shots, exist_ok=True)
        self.device = a.device
        self.pw = sync_playwright().start()
        self.browser = self.pw.chromium.launch(headless=not a.headed)
        opts = dict(DEVICES[a.device])
        opts["color_scheme"] = a.scheme
        if a.state and os.path.exists(a.state):
            opts["storage_state"] = a.state
        self.ctx = self.browser.new_context(**opts)
        self.ctx.add_init_script(JS_HELPERS)
        self.page = self.ctx.new_page()
        self.console, self.failed = [], []
        self._wire(self.page)
        self.ctx.on("page", self._newpage)

    def _wire(self, p):
        p.on("console", lambda m: m.type in ("error", "warning") and self.console.append({"type": m.type, "text": m.text[:300], "url": p.url}))
        p.on("pageerror", lambda e: self.console.append({"type": "pageerror", "text": str(e)[:300], "url": p.url}))
        p.on("response", lambda r: r.status >= 400 and self.failed.append({"status": r.status, "url": r.url[:200]}))
        p.on("dialog", self._dialog)

    def _dialog(self, d):
        accept, self.accept_next = getattr(self, "accept_next", False), False
        self.console.append({"type": "native-dialog", "text": f"{d.type}: {d.message[:200]} ({'accepted' if accept else 'auto-dismissed'})", "url": self.page.url})
        d.accept() if accept else d.dismiss()

    def c_dialog(self, action):
        """The next native dialog (confirm/alert/prompt) is accepted or dismissed; later ones are dismissed again."""
        if action not in ("accept", "dismiss"):
            raise ValueError("dialog takes accept or dismiss")
        self.accept_next = action == "accept"
        return {"next": action}

    def _newpage(self, p):
        self._wire(p)
        self.console.append({"type": "info", "text": f"new tab opened: {p.url}"})

    # helpers
    def ev(self, js, arg=None):
        self.page.evaluate(JS_HELPERS)
        return self.page.evaluate(js, arg)

    def loc(self, ref):
        if ref.startswith("e") and ref[1:].isdigit():
            return self.page.locator(f'[data-uirev="{ref}"]')
        return self.page.locator(ref)

    def settle(self):
        try:
            self.page.wait_for_load_state("networkidle", timeout=4000)
        except Exception:
            pass
        self.page.wait_for_timeout(250)

    def state(self):
        return {"url": self.page.url, "title": self.page.title()}

    def shotpath(self, name):
        safe = "".join(ch if ch.isalnum() or ch in "-_." else "-" for ch in name).strip("-")
        return os.path.join(self.shots, f"{self.device}__{self.a.session}__{safe}.png")   # per-session: parallel agents never overwrite each other

    def png(self, **kw):
        from PIL import Image
        return Image.open(io.BytesIO(self.page.screenshot(**kw))).convert("RGB")

    # commands
    def c_goto(self, url):
        if not url.startswith(("http://", "https://")):
            raise ValueError("only http(s) URLs are allowed")
        self.page.goto(url, wait_until="domcontentloaded", timeout=30000)
        self.settle()
        return self.state()

    def c_snapshot(self, all_=False):
        s = self.ev(JS_SNAPSHOT)
        if not all_:
            s["elements"] = [e for e in s["elements"] if e.get("inView")] + [{"note": f"{sum(1 for e in s['elements'] if not e.get('inView'))} more below/beside the viewport; use snapshot --all"}]
        return s

    def c_click(self, ref):
        self.loc(ref).first.click(timeout=8000)
        self.settle()
        return self.state()

    def c_tap(self, ref):
        self.loc(ref).first.tap(timeout=8000)
        self.settle()
        return self.state()

    def c_fill(self, ref, text):
        self.loc(ref).first.fill(text, timeout=8000)
        return self.state()

    def c_type(self, ref, text):
        self.loc(ref).first.press_sequentially(text, delay=20, timeout=8000)
        return self.state()

    def c_press(self, key):
        self.page.keyboard.press(key)
        self.settle()
        return self.state()

    def c_hover(self, ref):
        self.loc(ref).first.hover(timeout=8000)
        self.page.wait_for_timeout(300)
        return self.state()

    def c_select(self, ref, value):
        self.loc(ref).first.select_option(label=value, timeout=8000)
        self.settle()
        return self.state()

    def c_check(self, ref):
        self.loc(ref).first.click(timeout=8000)
        return self.state()

    def c_upload(self, ref, files):
        root = os.path.realpath(self.run)
        paths = []
        for f in files:
            p = os.path.realpath(f if os.path.isabs(f) else os.path.join(self.run, f))
            if not p.startswith(root + os.sep):
                raise ValueError(f"upload files must be inside the run folder: {f}")
            if not os.path.isfile(p):
                raise ValueError(f"no such file: {f}")
            paths.append(p)
        self.loc(ref).first.set_input_files(paths, timeout=8000)
        self.settle()
        return {**self.state(), "uploaded": [os.path.basename(p) for p in paths]}

    def c_batch(self, steps):
        out, failed = [], None
        for i, s in enumerate(steps):
            s = dict(s)
            cmd = s.pop("cmd", None)
            if cmd not in BATCH_CMDS:
                out.append({"i": i, "cmd": cmd, "ok": False, "error": f"'{cmd}' is not allowed in batch"})
                failed = i
                break
            if cmd == "snapshot" and "all" in s:
                s["all_"] = s.pop("all")
            if cmd == "shot" and "mark" in s:
                s["marks"] = [parse_mark(m) for m in s.pop("mark")]
            t0 = time.perf_counter()
            try:
                r = getattr(self, "c_" + cmd)(**s)
                out.append({"i": i, "cmd": cmd, "ok": True, "ms": round((time.perf_counter() - t0) * 1000), "result": r})
            except Exception as e:
                out.append({"i": i, "cmd": cmd, "ok": False, "ms": round((time.perf_counter() - t0) * 1000),
                            "error": str(e).split("\n")[0][:400], "state": self.state()})
                failed = i
                break
        return {"steps": out, "completed": sum(1 for x in out if x["ok"]), "total": len(steps), "failedAt": failed}

    def c_hidden(self, probe=True, limit=40):
        items = self.ev(JS_HIDDEN)
        if probe:
            # Only elements hidden by CSS can be revealed by hovering their container; off-screen and zero-size ones are reported as found.
            probeable = [i for i in items if i.get("hostRef") and i["why"] in ("display-none", "visibility-hidden", "opacity-0", "hidden-attr")]
            for it in probeable[:int(limit)]:
                try:
                    self.loc(it["hostRef"]).first.hover(timeout=2000)
                    self.page.wait_for_timeout(350)
                    it["revealsOnHover"] = bool(self.ev("r => window.__uirev.visible(window.__uirev.byRef(r))", it["ref"]))
                except Exception:
                    it["revealsOnHover"] = None
            self.page.mouse.move(0, 0)
        return {"count": len(items), "items": items}

    def c_scroll(self, to):
        if to in ("top", "bottom"):
            self.page.evaluate("t => window.scrollTo(0, t === 'top' ? 0 : document.documentElement.scrollHeight)", to)
        elif to.lstrip("-").isdigit():
            self.page.evaluate("y => window.scrollBy(0, y)", int(to))
        else:
            self.loc(to).first.scroll_into_view_if_needed()
        self.page.wait_for_timeout(300)
        return self.ev("() => ({scrollY, docH: document.documentElement.scrollHeight, viewH: innerHeight})")

    def c_back(self):
        self.page.go_back()
        self.settle()
        return self.state()

    def c_wait(self, ms):
        self.page.wait_for_timeout(int(ms))
        return self.state()

    def c_eval(self, js):
        return self.ev(js)

    def c_text(self, ref=None):
        el = self.loc(ref).first if ref else self.page.locator("body")
        return el.inner_text()[:6000]

    def c_emulate(self, scheme=None, motion=None, width=None, height=None):
        kw = {}
        if scheme: kw["color_scheme"] = scheme
        if motion: kw["reduced_motion"] = motion
        if kw: self.page.emulate_media(**kw)
        if width:
            self.page.set_viewport_size({"width": int(width), "height": int(height or self.page.viewport_size["height"])})
        self.page.wait_for_timeout(300)
        return {"viewport": self.page.viewport_size, **kw}

    def redact_terms(self):
        """Terms from RUN/redact.json ({"text": [...]}) that every screenshot blanks out."""
        try:
            with open(os.path.join(self.run, "redact.json")) as f:
                return [t for t in json.load(f).get("text", []) if isinstance(t, str) and t.strip()]
        except (OSError, ValueError, AttributeError):
            return []

    def c_shot(self, name, marks=None, full=False, ref=None, pad=24):
        marks = marks or []
        path = self.shotpath(name)
        clip = None
        if ref:
            el = self.loc(ref).first
            el.scroll_into_view_if_needed(timeout=5000)
            self.page.wait_for_timeout(150)
            box = el.bounding_box()
            vp = self.page.viewport_size
            top = max(0, box["y"] - pad - 34)   # room for the label drawn above the box
            clip = {"x": max(0, box["x"] - pad), "y": top,
                    "width": min(vp["width"], box["width"] + 2 * pad), "height": min(vp["height"] - top, box["y"] - top + box["height"] + pad + 34)}
        drawn = {"missing": [], "placed": []}
        redacted = 0
        terms = self.redact_terms()
        try:
            if marks:
                drawn = self.ev(JS_MARKS, marks)
            if terms:
                redacted = self.page.evaluate(JS_REDACT, terms)
            if clip:
                # viewport-relative clip, so fixed bars stay where the user sees them
                self.page.screenshot(path=path, clip=clip)
            else:
                self.page.screenshot(path=path, full_page=full)
        finally:
            self.page.evaluate("() => document.querySelectorAll('.__uirev_marks').forEach(l => l.remove())")
            if terms:
                self.page.evaluate(JS_UNREDACT)
        res = {"path": path, "relpath": os.path.relpath(path, self.run)}
        if terms: res["redacted"] = redacted
        if drawn["missing"]: res["missingRefs"] = drawn["missing"]
        if drawn["placed"]: res["marks"] = drawn["placed"]
        return res

    def _pair(self):
        """Viewport screenshots with text visible and text hidden."""
        a = self.png()
        h = self.page.add_style_tag(content=HIDE_TEXT_CSS)
        self.page.wait_for_timeout(60)
        b = self.png()
        h.evaluate("n => n.remove()")
        return a, b

    def _measure_text(self, items, a, b, dsf):
        from PIL import ImageChops
        res = []
        W, H = a.size
        for it in items:
            bg_px, ink = [], []
            for r in it["rects"]:
                x0, y0 = max(0, int(r["x"] * dsf)), max(0, int(r["y"] * dsf))
                x1, y1 = min(W, int((r["x"] + r["w"]) * dsf)), min(H, int((r["y"] + r["h"]) * dsf))
                if x1 - x0 < 2 or y1 - y0 < 2: continue
                ca, cb = a.crop((x0, y0, x1, y1)), b.crop((x0, y0, x1, y1))
                step = max(1, int(((x1 - x0) * (y1 - y0) / 4000) ** 0.5))
                pa, pb = ca.load(), cb.load()
                for yy in range(0, y1 - y0, step):
                    for xx in range(0, x1 - x0, step):
                        bg_px.append(pb[xx, yy])
                        if sum(abs(pa[xx, yy][k] - pb[xx, yy][k]) for k in range(3)) > 30:
                            ink.append((pa[xx, yy], pb[xx, yy]))
            if not bg_px: continue
            fg = it["fg"]
            # contrast of the declared text colour against every sampled background pixel
            rs = sorted(ratio(over(fg, p), p) for p in bg_px)
            worst_i = max(0, int(len(rs) * 0.05) - 1)      # 5th percentile: robust to stray pixels
            med = sorted(bg_px, key=lum)[len(bg_px) // 2]
            large = it["size"] >= 24 or (it["size"] >= 18.66 and it["weight"] >= 700)
            need = 3.0 if large else 4.5
            worst = rs[worst_i]
            # rendered ink check: the strongest-contrast ink pixel vs the background under it
            ink_ratio = max((ratio(p, q) for p, q in ink), default=None)
            varied = rs[-1] - rs[0] > 1.0
            d = {"ref": it["ref"], "text": it["text"], "tag": it["tag"], "fontPx": round(it["size"], 1), "weight": it["weight"],
                 "large": large, "fg": hexc(over(fg, med)), "fgAlpha": round(fg[3], 2), "bg": hexc(med),
                 "ratio": ratio(over(fg, med), med), "worstRatio": worst, "required": need,
                 "AA": "pass" if worst >= need else "FAIL", "AAA": "pass" if worst >= (4.5 if large else 7) else "fail",
                 "renderedInkRatio": ink_ratio, "bgVaries": varied}
            if it.get("disabled"): d["exempt"] = "disabled control (WCAG 1.4.3 exempts inactive UI)"
            if it.get("hiddenFromAT"): d["note"] = "inside aria-hidden"
            if ink_ratio is not None and ink_ratio + 0.3 < min(d["ratio"], need) and not large:
                d["inkWarning"] = "rendered glyphs are lighter than the declared colour (thin font / antialiasing / filter)"
            if not ink and it["tag"] != "placeholder":
                d["inkWarning"] = "no rendered glyph pixels found (text may be hidden, clipped or covered)"
            res.append(d)
        return res

    def c_contrast(self, scope=None, full=True, limit=80):
        dsf = self.page.evaluate("devicePixelRatio")
        vh = self.page.viewport_size["height"]
        start_y = self.page.evaluate("scrollY")
        doc_h = self.page.evaluate("document.documentElement.scrollHeight")
        positions = list(range(0, doc_h, max(200, vh - 80))) if full else [start_y]
        done, results = set(), []
        for y in positions[:25]:
            if full:
                self.page.evaluate("y => window.scrollTo(0, y)", y)
                self.page.wait_for_timeout(150)
            items = self.ev(JS_TEXT_ITEMS, scope)
            vis = [i for i in items if i["ref"] + i["tag"] not in done and all(0 <= r["y"] and r["y"] + r["h"] <= vh for r in i["rects"])]
            if not vis: continue
            a, b = self._pair()
            for d in self._measure_text(vis, a, b, dsf):
                d["pageY"] = y
                results.append(d)
                done.add(d["ref"] + d["tag"])
        if full:
            self.page.evaluate("y => window.scrollTo(0, y)", start_y)
        fails = [r for r in results if r["AA"] == "FAIL" and "exempt" not in r]
        fails.sort(key=lambda r: r["worstRatio"])
        warn = [r for r in results if r.get("inkWarning") or r.get("bgVaries")]
        self._save("contrast", results)
        return {"measured": len(results), "failCount": len(fails), "fails": fails[:limit],
                "warnings": warn[:30], "exempt": [r for r in results if "exempt" in r][:20],
                "lowestPassing": sorted([r for r in results if r["AA"] == "pass"], key=lambda r: r["worstRatio"] - r["required"])[:8],
                "method": "declared text colour composited over rendered background pixels (text hidden via CSS); worst = 5th-percentile pixel; ink = rendered glyph pixels cross-check. Viewport-by-viewport so fixed/sticky layers are measured where they sit."}

    def c_nontext(self, scope=None):
        """WCAG 1.4.11: component boundary (border or fill) against the pixels just outside it."""
        dsf = self.page.evaluate("devicePixelRatio")
        comps = self.ev(JS_COMPONENTS, scope)
        a, b = self._pair()
        W, H = b.size
        pb = b.load()
        out = []
        for c in comps:
            r = c["rect"]
            ring = []
            for dx, dy in ((-3, 0.5), (1.0, 0.5), (0.5, -3), (0.5, 1.0)):
                for t in (0.2, 0.5, 0.8):
                    if dx in (-3, 1.0):
                        x = r["x"] + (r["w"] + 3 if dx == 1.0 else -3); y = r["y"] + r["h"] * t
                    else:
                        x = r["x"] + r["w"] * t; y = r["y"] + (r["h"] + 3 if dy == 1.0 else -3)
                    px, py = int(x * dsf), int(y * dsf)
                    if 0 <= px < W and 0 <= py < H: ring.append(pb[px, py])
            if not ring: continue
            outside = sorted(ring, key=lum)[len(ring) // 2]
            cands = {}
            if c["border"] and c["borderWidth"] >= 1 and c["border"][3] > 0:
                cands["border"] = ratio(over(c["border"], outside), outside)
            if c["fill"][3] > 0:
                cands["fill"] = ratio(over(c["fill"], outside), outside)
            best = max(cands.values()) if cands else None
            d = {"ref": c["ref"], "role": c["role"], "name": c["name"][:40], "outside": hexc(outside), **{k + "Ratio": v for k, v in cands.items()},
                 "best": best, "required": 3.0}
            if c["appearanceNative"]: d["note"] = "native checkbox/radio: browser-drawn, verify visually"
            if best is None:
                d["result"] = "no visible boundary (text-only control): check it is identifiable by other means"
            else:
                d["result"] = "pass" if best >= 3.0 else "FAIL"
            if c["disabled"]: d["exempt"] = "disabled"
            out.append(d)
        self._save("nontext", out)
        fails = [d for d in out if d["result"] != "pass" and "exempt" not in d and "note" not in d]
        return {"measured": len(out), "failOrUnclear": fails[:40]}

    def c_layout(self):
        r = self.ev(JS_LAYOUT)
        self._save("layout", r)
        return r

    def c_reflow(self):
        """WCAG 1.4.10: at 320 CSS px wide, content should not need horizontal scrolling."""
        old = self.page.viewport_size
        self.page.set_viewport_size({"width": 320, "height": 640})
        self.page.wait_for_timeout(400)
        r = self.ev(JS_LAYOUT)
        path = self.shotpath("reflow-320")
        self.page.screenshot(path=path, full_page=True)
        self.page.set_viewport_size(old)
        self.page.wait_for_timeout(300)
        return {"horizontalScroll": r["horizontalScroll"], "offscreen": r["offscreen"][:15], "clipped": r["clipped"][:15],
                "shot": os.path.relpath(path, self.run)}

    def c_textspacing(self):
        """WCAG 1.4.12: apply the SC text spacing and report newly clipped/overlapping text."""
        before = self.ev(JS_LAYOUT)
        h = self.page.add_style_tag(content="*{line-height:1.5!important;letter-spacing:0.12em!important;word-spacing:0.16em!important}p{margin-bottom:2em!important}")
        self.page.wait_for_timeout(300)
        after = self.ev(JS_LAYOUT)
        path = self.shotpath("text-spacing")
        self.page.screenshot(path=path, full_page=True)
        h.evaluate("n => n.remove()")
        known = {c["ref"] for c in before["clipped"] if "ref" in c}
        return {"newlyClipped": [c for c in after["clipped"] if c.get("ref") not in known][:20],
                "newOverlaps": after["overlaps"][len(before["overlaps"]):][:20],
                "horizontalScroll": after["horizontalScroll"], "shot": os.path.relpath(path, self.run)}

    def c_focus(self, count=40, name="focus"):
        """Tab through the page; verify each focus indicator from pixels (before/after diff)."""
        from PIL import ImageChops
        dsf = self.page.evaluate("devicePixelRatio")
        self.page.evaluate("() => { document.activeElement && document.activeElement.blur(); window.scrollTo(0,0); }")
        self.page.mouse.click(1, 1)
        rows, last = [], None
        for i in range(int(count)):
            self.page.keyboard.press("Tab")
            self.page.wait_for_timeout(120)
            info = self.ev("""() => { const U = window.__uirev; const el = document.activeElement;
                if (!el || el === document.body) return null;
                const r = el.getBoundingClientRect();
                return {ref: U.ref(el), role: U.role(el), name: U.name(el).slice(0,40), r: {x: r.x, y: r.y, w: r.width, h: r.height}, vh: innerHeight, vw: innerWidth}; }""")
            if not info:
                rows.append({"step": i + 1, "indicator": "end", "focus": "returned to body (end of tab cycle, or focus lost if this is early)"}); break
            if last and info["ref"] == last:
                break
            last = info["ref"]
            r = info["r"]
            pad = 8
            clip = {"x": max(0, r["x"] - pad), "y": max(0, r["y"] - pad), "width": min(info["vw"], r["w"] + 2 * pad), "height": min(info["vh"], r["h"] + 2 * pad)}
            row = {"step": i + 1, "ref": info["ref"], "role": info["role"], "name": info["name"], "box": {k: round(v) for k, v in r.items()}}
            if r["y"] + r["h"] <= 0 or r["y"] >= info["vh"] or clip["width"] < 2 or clip["height"] < 2:
                row["indicator"] = "focused element is off-screen (not scrolled into view / hidden)"
                rows.append(row); continue
            from PIL import Image
            focused = Image.open(io.BytesIO(self.page.screenshot(clip=clip))).convert("RGB")
            self.page.evaluate("() => document.activeElement.blur()")
            self.page.wait_for_timeout(80)
            blurred = Image.open(io.BytesIO(self.page.screenshot(clip=clip))).convert("RGB")
            self.page.evaluate("r => { const el = document.querySelector('[data-uirev=\"' + r + '\"]'); el && el.focus({preventScroll: true}); }", info["ref"])
            self.page.wait_for_timeout(60)
            diff = ImageChops.difference(focused, blurred).convert("L").point(lambda v: 255 if v > 40 else 0)
            changed = sum(1 for v in diff.getdata() if v)
            per = r["w"] * r["h"] * dsf * dsf
            fp, bp = focused.load(), blurred.load()
            best = 1.0
            for yy in range(0, focused.size[1], 2):
                for xx in range(0, focused.size[0], 2):
                    if diff.getpixel((xx, yy)):
                        best = max(best, ratio(fp[xx, yy], bp[xx, yy]))
            row["changedPx"] = changed
            row["changeContrast"] = best
            # 2.4.11 focus not obscured: is the focused element's centre and corners covered by other content?
            row["obscured"] = self.ev("""(ref) => { const el = document.querySelector('[data-uirev="' + ref + '"]'); const r = el.getBoundingClientRect();
                const pts = [[r.x + r.width/2, r.y + r.height/2], [r.x + 2, r.y + 2], [r.x + r.width - 2, r.y + r.height - 2]];
                const hidden = pts.filter(([x, y]) => { const t = document.elementFromPoint(x, y); return t && !el.contains(t) && !t.contains(el) && !(t.tagName === 'LABEL' && t.control === el); }).length;
                return hidden === 3 ? 'ENTIRELY (fails 2.4.11)' : hidden ? 'partly (fails 2.4.12 AAA only)' : 'no'; }""", info["ref"])
            # 2.4.13-style heuristic: area of change ~ a 2px perimeter and >= 3:1 change contrast
            perim_area = 2 * (r["w"] + r["h"]) * 2 * dsf * dsf
            row["indicator"] = ("NONE visible" if changed < 4 else
                                "weak (<3:1 change)" if best < 3 else
                                "small area (below ~2px perimeter)" if changed < perim_area * 0.5 else "visible")
            rows.append(row)
        self._save(name, rows)
        return {"tabStops": len(rows), "problems": [r for r in rows if r.get("indicator") not in ("visible", "end") or r.get("obscured", "no") != "no"][:40], "order": [f'{r.get("step")}:{r.get("role")}:{r.get("name")}' for r in rows]}

    def c_axe(self, scope=None):
        src = open(os.path.join(HERE, "axe.min.js")).read()
        self.page.add_script_tag(content=src)
        r = self.page.evaluate("""async (scope) => {
            const ctx = scope ? document.querySelector('[data-uirev="' + scope + '"]') : document;
            const res = await axe.run(ctx, {runOnly: {type: 'tag', values: ['wcag2a','wcag2aa','wcag21a','wcag21aa','wcag22aa','best-practice']}, resultTypes: ['violations','incomplete'], rules: {'target-size': {enabled: true}}});
            const f = (v) => ({id: v.id, impact: v.impact, help: v.help, url: v.helpUrl, tags: v.tags.filter(t => t.startsWith('wcag')), count: v.nodes.length,
                nodes: v.nodes.slice(0, 5).map(n => ({target: n.target.join(' '), summary: (n.failureSummary || '').slice(0, 220)}))});
            return {violations: res.violations.map(f), incomplete: res.incomplete.map(v => ({id: v.id, count: v.nodes.length, help: v.help}))};
        }""", scope)
        self._save("axe", r)
        return r

    def c_console(self, clear=False):
        r = {"console": self.console[-60:], "failedRequests": self.failed[-60:]}
        if clear:
            self.console, self.failed = [], []
        return r

    def c_pick(self, x, y):
        """Pixel colour at a CSS point in the current viewport (for manual checks)."""
        dsf = self.page.evaluate("devicePixelRatio")
        img = self.png()
        p = img.getpixel((int(float(x) * dsf), int(float(y) * dsf)))
        return {"rgb": p, "hex": hexc(p)}

    def c_ratio(self, c1, c2):
        f = lambda s: tuple(int(s.lstrip("#")[i:i + 2], 16) for i in (0, 2, 4))
        return {"ratio": ratio(f(c1), f(c2))}

    def c_savestate(self, path):
        self.ctx.storage_state(path=path)
        return {"saved": path}

    def _save(self, kind, data):
        d = os.path.join(self.run, "measurements")
        os.makedirs(d, exist_ok=True)
        slug = "".join(ch if ch.isalnum() else "-" for ch in self.page.url.split("://", 1)[-1])[:80]
        with open(os.path.join(d, f"{self.a.session}__{self.device}__{kind}__{slug}.json"), "w") as f:
            json.dump({"url": self.page.url, "device": self.device, "at": time.time(), "data": data}, f, indent=1)


def serve(a):
    d = Daemon(a)
    srv = HTTPServer(("127.0.0.1", 0), None)
    port = srv.server_address[1]

    class H(BaseHTTPRequestHandler):
        def log_message(self, *x): pass
        def do_POST(self):
            req = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            cmd = req.pop("cmd")
            try:
                if cmd == "stop":
                    out = {"ok": True, "stopped": True}
                else:
                    fn = getattr(d, "c_" + cmd)
                    out = {"ok": True, "result": fn(**req)}
            except Exception as e:
                msg = str(e).split("\n")[0][:400]
                try:
                    out = {"ok": False, "error": msg, "state": d.state()}
                except Exception:
                    out = {"ok": False, "error": msg}
            body = json.dumps(out, default=str).encode()
            self.send_response(200); self.send_header("Content-Length", str(len(body))); self.end_headers()
            self.wfile.write(body)
            if cmd == "stop":
                d.browser.close(); d.pw.stop(); os._exit(0)
    srv.RequestHandlerClass = H
    sess = sessfile(a.run, a.session)
    with open(sess, "w") as f:
        json.dump({"port": port, "pid": os.getpid(), "device": a.device}, f)
    srv.serve_forever()


def sessfile(run, session):
    d = os.path.join(os.path.abspath(run), "sessions")
    os.makedirs(d, exist_ok=True)
    return os.path.join(d, session + ".json")


def send(run, session, payload, timeout=300):
    s = json.load(open(sessfile(run, session)))
    req = urllib.request.Request(f"http://127.0.0.1:{s['port']}/", data=json.dumps(payload).encode(),
                                 headers={"Content-Type": "application/json"})
    return json.loads(urllib.request.urlopen(req, timeout=timeout).read())

# ------------------------------------------------------------------ fixtures (ux-review)

FIXTURE_KINDS = ("pdf", "png", "jpg", "csv", "txt")
BATCH_CMDS = {"goto", "click", "tap", "fill", "type", "press", "hover", "select", "check",
              "scroll", "back", "wait", "snapshot", "text", "shot", "upload"}

def tiny_pdf(text):
    t = text.replace("\\", "\\\\").replace("(", "\\(").replace(")", "\\)")
    stream = f"BT /F1 18 Tf 72 720 Td ({t}) Tj ET".encode("latin-1", "replace")
    objs = [b"<< /Type /Catalog /Pages 2 0 R >>",
            b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
            b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
            b"<< /Length %d >>\nstream\n" % len(stream) + stream + b"\nendstream",
            b"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"]
    out, offs = b"%PDF-1.4\n", []
    for i, o in enumerate(objs, 1):
        offs.append(len(out))
        out += b"%d 0 obj\n" % i + o + b"\nendobj\n"
    xref = len(out)
    out += b"xref\n0 %d\n0000000000 65535 f \n" % (len(objs) + 1)
    out += b"".join(b"%010d 00000 n \n" % o for o in offs)
    out += b"trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n" % (len(objs) + 1, xref)
    return out

def make_fixture(run, kind, name):
    """Write a small dummy upload file marked UXR-TEST into RUN/fixtures."""
    if kind not in FIXTURE_KINDS:
        raise ValueError("kind must be one of " + ", ".join(FIXTURE_KINDS))
    d = os.path.join(os.path.abspath(run), "fixtures")
    os.makedirs(d, exist_ok=True)
    safe = "".join(ch if ch.isalnum() or ch in "-_" else "-" for ch in name).strip("-") or "file"
    path = os.path.join(d, f"UXR-TEST-{safe}.{kind}")
    label = f"UXR-TEST {name}"
    if kind in ("png", "jpg"):
        from PIL import Image, ImageDraw
        im = Image.new("RGB", (600, 800), "white")
        dr = ImageDraw.Draw(im)
        for i, line in enumerate([label, "Dummy file for a UX review", "Total 12.34"]):
            dr.text((40, 40 + 40 * i), line, fill="black")
        im.save(path, "PNG" if kind == "png" else "JPEG")
    elif kind == "pdf":
        with open(path, "wb") as f:
            f.write(tiny_pdf(label))
    elif kind == "csv":
        with open(path, "w") as f:
            f.write("date,description,amount\n2026-01-15,%s,12.34\n2026-01-16,UXR-TEST second row,56.78\n" % label.replace(",", " "))
    else:
        with open(path, "w") as f:
            f.write(label + "\nDummy file for a UX review.\n")
    return {"path": path, "kind": kind, "bytes": os.path.getsize(path)}

# ------------------------------------------------------------------ report

SEV = ["critical", "major", "minor", "cosmetic", "positive"]
SEV_COL = {"critical": "#c92a2a", "major": "#d9480f", "minor": "#a07000", "cosmetic": "#1864ab", "positive": "#2b8a3e"}

def build_report(run):
    run = os.path.abspath(run)
    fdir = os.path.join(run, "findings")
    final = os.path.join(run, "final.json")
    if os.path.exists(final):
        doc = json.load(open(final))
    else:
        doc = {"app": "", "summary": "", "findings": []}
        for fn in sorted(os.listdir(fdir)) if os.path.isdir(fdir) else []:
            if fn.endswith(".json"):
                part = json.load(open(os.path.join(fdir, fn)))
                doc["findings"] += part.get("findings", [])
    fs = doc.get("findings", [])
    fs.sort(key=lambda f: (SEV.index(f.get("severity", "minor")) if f.get("severity") in SEV else 9, f.get("screen", "")))
    counts = {s: sum(1 for f in fs if f.get("severity") == s) for s in SEV}
    devices = sorted({d for f in fs for d in (f.get("devices") or [f.get("device", "")]) if d})
    screens = sorted({f.get("screen", "") for f in fs if f.get("screen")})
    e = html.escape

    def img(p):
        if not p: return ""
        p = p if not os.path.isabs(p) else os.path.relpath(p, run)
        return f'<a href="{e(p)}" target="_blank"><img loading="lazy" src="{e(p)}" alt="Marked screenshot {e(os.path.basename(p))}"></a>'

    cards = []
    for i, f in enumerate(fs, 1):
        sev = f.get("severity", "minor")
        devs = f.get("devices") or [f.get("device", "")]
        meas = f.get("measured") or {}
        mrows = "".join(f"<tr><th>{e(str(k))}</th><td>{e(json.dumps(v) if not isinstance(v, str) else v)}</td></tr>" for k, v in meas.items())
        shots = f.get("screenshots") or ([f["screenshot"]] if f.get("screenshot") else [])
        cards.append(f"""
<article class="card" data-theme="{e(f.get('theme','light'))}" data-sev="{e(sev)}" data-dev="{e(' '.join(devs))}" data-screen="{e(f.get('screen',''))}">
  <header><span class="sev" style="--c:{SEV_COL.get(sev,'#555')}">{e(sev)}</span><span class="fid">{e(f.get('id', 'F%03d' % i))}</span>
  <h3>{e(f.get('title',''))}</h3></header>
  <p class="meta">{e(f.get('screen',''))} · {e(', '.join(devs))} · {e(f.get('theme','light'))} theme · {e(f.get('category',''))}{' · <b>verified</b>' if f.get('verified') else ''}{(' · confidence ' + e(str(f.get('confidence')))) if f.get('confidence') else ''}</p>
  <div class="body">
    <div class="text">
      <p><b>Observed.</b> {e(f.get('observation',''))}</p>
      {f'<p><b>Impact.</b> {e(f["impact"])}</p>' if f.get('impact') else ''}
      <p><b>Recommendation.</b> {e(f.get('recommendation',''))}</p>
      {f'<p class="principle"><b>Principle.</b> {e(f["principle"])}</p>' if f.get('principle') else ''}
      {f'<table class="meas">{mrows}</table>' if mrows else ''}
      {f'<p class="steps"><b>Steps.</b> {e(f["steps"])}</p>' if f.get('steps') else ''}
    </div>
    <div class="shots">{''.join(img(s) for s in shots)}</div>
  </div>
</article>""")
    opt = lambda xs: "".join(f'<option>{e(x)}</option>' for x in xs)
    page = f"""<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>UI Review Report</title><style>
:root{{--bg:#f6f7f9;--card:#fff;--ink:#1c1f24;--mute:#4a5360;--line:#d9dee5}}
@media (prefers-color-scheme:dark){{:root{{--bg:#14171b;--card:#1d2127;--ink:#e8ebef;--mute:#aab3bf;--line:#333a43}}}}
*{{box-sizing:border-box}}body{{margin:0;background:var(--bg);color:var(--ink);font:16px/1.55 -apple-system,system-ui,Segoe UI,sans-serif}}
main{{max-width:1200px;margin:0 auto;padding:24px 16px 64px}}h1{{margin:0 0 4px;font-size:28px}}.lede{{color:var(--mute);margin:0 0 20px}}
.summary{{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:16px 20px;margin-bottom:16px;white-space:pre-wrap}}
.counts{{display:flex;flex-wrap:wrap;gap:8px;margin:0 0 16px}}.counts span{{background:var(--card);border:1px solid var(--line);border-radius:999px;padding:4px 12px}}
.filters{{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:20px}}select{{font:inherit;padding:6px 10px;border-radius:8px;border:1px solid var(--line);background:var(--card);color:var(--ink)}}
.card{{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:16px 20px;margin-bottom:16px}}
.card header{{display:flex;align-items:center;gap:10px;flex-wrap:wrap}}.card h3{{margin:0;font-size:18px;flex:1 1 300px}}
.sev{{background:var(--c);color:#fff;font-weight:700;font-size:13px;text-transform:uppercase;letter-spacing:.04em;padding:3px 9px;border-radius:6px}}
.fid{{color:var(--mute);font-family:ui-monospace,monospace}}.meta{{color:var(--mute);margin:6px 0 10px;font-size:15px}}
.body{{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:20px}}@media (max-width:820px){{.body{{grid-template-columns:1fr}}}}
.text p{{margin:0 0 8px}}.shots{{display:flex;flex-direction:column;gap:10px}}.shots img{{width:100%;border:1px solid var(--line);border-radius:6px;display:block}}
.meas{{border-collapse:collapse;font-size:14px;margin:8px 0;width:100%}}.meas th,.meas td{{border:1px solid var(--line);padding:4px 8px;text-align:left;vertical-align:top;word-break:break-word}}.meas th{{width:35%;color:var(--mute);font-weight:600}}
.principle{{color:var(--mute)}}
</style></head><body><main>
<h1>UI Review Report</h1><p class="lede">{e(doc.get('app',''))} · {len(fs)} findings · devices: {e(', '.join(devices))} · generated {time.strftime('%Y-%m-%d %H:%M')}</p>
{f'<div class="summary">{e(doc["summary"])}</div>' if doc.get('summary') else ''}
<div class="counts">{''.join(f'<span><b style="color:{SEV_COL[s]}">{counts[s]}</b> {s}</span>' for s in SEV)}</div>
<div class="filters"><label>Severity <select id="fs"><option value="">all</option>{opt(SEV)}</select></label>
<label>Device <select id="fd"><option value="">all</option>{opt(devices)}</select></label>
<label>Theme <select id="fth"><option value="">all</option><option>light</option><option>dark</option><option>both</option></select></label>
<label>Screen <select id="fsc"><option value="">all</option>{opt(screens)}</select></label></div>
{''.join(cards)}
</main><script>
const f=()=>{{const s=fs.value,d=fd.value,c=fsc.value,t=fth.value;document.querySelectorAll('.card').forEach(x=>{{x.hidden=(s&&x.dataset.sev!==s)||(d&&!x.dataset.dev.split(' ').includes(d))||(c&&x.dataset.screen!==c)||(t&&x.dataset.theme!==t&&!(x.dataset.theme==='both'&&t!=='both'))}})}};
[fs,fd,fsc,fth].forEach(x=>x.onchange=f);
</script></body></html>"""
    out = os.path.join(run, "report.html")
    open(out, "w").write(page)
    return {"report": out, "findings": len(fs), "counts": counts}

# ------------------------------------------------------------------ CLI

HELP = """commands (all take --run DIR --session S):
  start --device desktop|tablet|mobile [--state auth.json] [--scheme light|dark] [--headed]
  stop
  goto URL                       open a page (http/https only)
  snapshot [--all]               title, headings, landmarks, dialogs, interactive elements with refs (e12)
  click REF | tap REF | hover REF | check REF
  fill REF TEXT | type REF TEXT  (type = key by key)
  select REF LABEL | press KEY | back | wait MS | scroll top|bottom|PIXELS|REF
  text [REF]                     visible text
  eval JS                        run a JS expression/function in the page (read-only inspection)
  shot NAME [--full] [--ref REF] [--mark REF|x,y,w,h:LABEL:SEVERITY ...]
                                 screenshot with numbered boxes; severity colours critical|major|minor|info|pass
  contrast [--scope REF] [--here] pixel-verified text contrast (WCAG 1.4.3 / 1.4.6), whole page by default
  nontext [--scope REF]          component boundary contrast (WCAG 1.4.11), current viewport
  layout                         overflow, clipping, small text, target sizes (2.5.8), overlaps, alt, labels
  reflow                         320px reflow check (1.4.10) + screenshot
  textspacing                    1.4.12 text-spacing stress + screenshot
  focus [--count N]              keyboard tab order + pixel-verified focus indicators (2.4.7 / 2.4.11)
  axe [--scope REF]              axe-core WCAG 2.2 AA + best-practice scan
  console [--clear]              console errors, native dialogs, failed requests
  emulate [--scheme dark|light] [--motion reduce] [--width W --height H]
  pick X Y | ratio HEX HEX       pixel colour / contrast of two colours
  savestate PATH                 save cookies+storage for other sessions
  dialog accept|dismiss          what to do with the next native dialog (default: dismiss)
  batch --steps JSON | --file P  run many steps in one call; stops at the first failure; per-step ms
  hidden [--no-probe] [--limit N] interactive elements that are not visible; hover-probes their container
  upload REF FILE...             set files on a file input (files must be inside RUN)
  fixture pdf|png|jpg|csv|txt NAME   dummy upload file in RUN/fixtures, marked UXR-TEST
other: login URL --save PATH (headed, for a person) · report --run DIR"""


def parse_mark(m):
    # A CSS selector that itself contains ':' is separated from its label by ': ' (colon, space).
    if ": " in m and not re.match(r"^(e\d+|[\d.]+,[\d.]+,[\d.]+,[\d.]+):", m):
        target, rest = m.split(": ", 1)
        d = parse_mark("x:" + rest)
        d.pop("ref", None); d.pop("box", None)
        d["ref"] = target
        return d
    parts = m.split(":")
    sevs = ("critical", "major", "minor", "info", "pass", "high", "medium", "low")
    alias = {"high": "major", "medium": "minor", "low": "info"}   # ux-review severity words
    target = parts[0]
    sev = parts[-1] if len(parts) > 2 and parts[-1] in sevs else "critical"
    label = ":".join(parts[1:-1] if len(parts) > 2 and parts[-1] in sevs else parts[1:]) or None
    d = {"label": label, "color": alias.get(sev, sev)}
    if target.count(",") == 3:
        x, y, w, h = map(float, target.split(","))
        d["box"] = {"x": x, "y": y, "w": w, "h": h}
    else:
        d["ref"] = target
    return d


def main():
    argv = sys.argv[1:]
    if not argv or argv[0] in ("help", "-h", "--help"):
        print(__doc__); print(HELP); return
    cmd, rest = argv[0], argv[1:]
    p = argparse.ArgumentParser(prog="uirev " + cmd)
    p.add_argument("--run", default=os.environ.get("UIREV_RUN"))
    p.add_argument("--session", default=os.environ.get("UIREV_SESSION", "main"))
    if cmd in ("start", "_serve"):
        p.add_argument("--device", default="desktop", choices=list(DEVICES))
        p.add_argument("--state"); p.add_argument("--scheme", default="light"); p.add_argument("--headed", action="store_true")
        a = p.parse_args(rest)
        if cmd == "_serve":
            return serve(a)
        sf = sessfile(a.run, a.session)
        if os.path.exists(sf): os.remove(sf)
        log = open(os.path.join(os.path.abspath(a.run), "sessions", a.session + ".log"), "w")
        args = [sys.executable, os.path.abspath(__file__), "_serve", "--run", a.run, "--session", a.session, "--device", a.device, "--scheme", a.scheme]
        if a.state: args += ["--state", a.state]
        if a.headed: args += ["--headed"]
        subprocess.Popen(args, stdout=log, stderr=log, start_new_session=True)
        for _ in range(150):
            if os.path.exists(sf):
                print(json.dumps({"ok": True, "session": a.session, "device": a.device, "viewport": DEVICES[a.device]["viewport"]})); return
            time.sleep(0.2)
        print(json.dumps({"ok": False, "error": "browser did not start; see " + log.name})); sys.exit(1)
    if cmd == "login":
        p.add_argument("url"); p.add_argument("--save", required=True)
        p.add_argument("--device", default="desktop", choices=list(DEVICES))
        a = p.parse_args(rest)
        from playwright.sync_api import sync_playwright
        with sync_playwright() as pw:
            try:   # real Chrome: identity providers often refuse automation builds of Chromium
                b = pw.chromium.launch(headless=False, channel="chrome", args=["--disable-blink-features=AutomationControlled"])
            except Exception:
                b = pw.chromium.launch(headless=False, args=["--disable-blink-features=AutomationControlled"])
            c = b.new_context(**DEVICES[a.device]); pg = c.new_page(); pg.goto(a.url)
            print("Sign in in the browser window. The session is saved every 2s; close the window when you see the app.", flush=True)
            saves = 0
            while b.is_connected() and c.pages:   # no stdin needed: finishes when the window is closed
                try:
                    c.storage_state(path=a.save); saves += 1
                    pg.wait_for_timeout(2000)
                except Exception:
                    break
            try: b.close()
            except Exception: pass
        print(json.dumps({"saved": a.save})); return
    if cmd == "report":
        a = p.parse_args(rest)
        print(json.dumps(build_report(a.run), indent=1)); return
    if cmd == "fixture":
        p.add_argument("kind"); p.add_argument("name")
        a = p.parse_args(rest)
        if not a.run:
            print("--run DIR is required (or UIREV_RUN)"); sys.exit(2)
        try:
            out = {"ok": True, "result": make_fixture(a.run, a.kind, a.name)}
        except Exception as ex:
            out = {"ok": False, "error": str(ex)}
        print(json.dumps(out)); sys.exit(0 if out["ok"] else 1)

    payload = {"cmd": cmd}
    if cmd == "goto": p.add_argument("url");
    elif cmd == "snapshot": p.add_argument("--all", dest="all_", action="store_true")
    elif cmd in ("click", "tap", "hover", "check"): p.add_argument("ref")
    elif cmd in ("fill", "type"): p.add_argument("ref"); p.add_argument("text")
    elif cmd == "select": p.add_argument("ref"); p.add_argument("value")
    elif cmd == "press": p.add_argument("key")
    elif cmd == "upload": p.add_argument("ref"); p.add_argument("files", nargs="+")
    elif cmd == "batch": p.add_argument("--steps"); p.add_argument("--file")
    elif cmd == "hidden": p.add_argument("--no-probe", dest="probe", action="store_false"); p.add_argument("--limit", type=int, default=40)
    elif cmd == "wait": p.add_argument("ms")
    elif cmd == "scroll": p.add_argument("to")
    elif cmd == "text": p.add_argument("ref", nargs="?")
    elif cmd == "eval": p.add_argument("js")
    elif cmd == "shot":
        p.add_argument("name"); p.add_argument("--full", action="store_true"); p.add_argument("--ref")
        p.add_argument("--mark", action="append", default=[])
    elif cmd == "contrast": p.add_argument("--scope"); p.add_argument("--here", action="store_true")
    elif cmd in ("nontext", "axe"): p.add_argument("--scope")
    elif cmd == "focus": p.add_argument("--count", type=int, default=40); p.add_argument("--name", default="focus")
    elif cmd == "console": p.add_argument("--clear", action="store_true")
    elif cmd == "emulate": p.add_argument("--scheme"); p.add_argument("--motion"); p.add_argument("--width"); p.add_argument("--height")
    elif cmd == "pick": p.add_argument("x"); p.add_argument("y")
    elif cmd == "ratio": p.add_argument("c1"); p.add_argument("c2")
    elif cmd == "savestate": p.add_argument("path")
    elif cmd == "dialog": p.add_argument("action", choices=("accept", "dismiss"))
    elif cmd in ("stop", "back", "layout", "reflow", "textspacing"): pass
    else:
        print(f"unknown command {cmd}\n{HELP}"); sys.exit(2)
    a = p.parse_args(rest)
    if not a.run:
        print("--run DIR is required (or UIREV_RUN)"); sys.exit(2)
    for k, v in vars(a).items():
        if k not in ("run", "session"): payload[k] = v
    if cmd == "batch":
        src, raw = payload.pop("file"), payload.pop("steps")
        def bad(msg):
            print(json.dumps({"ok": False, "error": msg})); sys.exit(1)
        if src:
            fp = os.path.realpath(src if os.path.isabs(src) else os.path.join(a.run, src))
            if not fp.startswith(os.path.realpath(a.run) + os.sep):
                bad("batch --file must be inside the run folder")
            if not os.path.isfile(fp):
                bad(f"no such file: {src}")
            raw = open(fp).read()
        if not raw:
            bad("batch needs --steps JSON or --file PATH")
        try:
            steps = json.loads(raw)
        except ValueError as ex:
            bad(f"steps are not valid JSON: {ex}")
        if not isinstance(steps, list) or not all(isinstance(s, dict) for s in steps):
            bad("steps must be a JSON list of objects")
        payload["steps"] = steps
    if cmd == "shot":
        payload["marks"] = [parse_mark(m) for m in payload.pop("mark")]
    if cmd == "contrast":
        payload["full"] = not payload.pop("here")
    try:
        out = send(a.run, a.session, payload)
    except FileNotFoundError:
        out = {"ok": False, "error": f"no session '{a.session}' in {a.run}; run start first"}
    except Exception as ex:
        out = {"ok": False, "error": f"session unreachable: {ex}"}
    print(json.dumps(out, separators=(",", ":"), ensure_ascii=False, default=str))
    if not out.get("ok"): sys.exit(1)


if __name__ == "__main__":
    main()
