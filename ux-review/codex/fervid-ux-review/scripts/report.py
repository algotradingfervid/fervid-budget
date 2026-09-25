#!/usr/bin/env python3
"""Build the autonomous UX report: RUN/final.json -> report.html and report.md."""
import html, json, os, sys
from urllib.parse import urlparse

SEV = ["critical", "high", "medium", "low"]
SEV_COL = {"critical": "#c92a2a", "high": "#d9480f", "medium": "#9a6700", "low": "#1864ab"}


def e(s):
    return html.escape("" if s is None else str(s))


def rel(run, p):
    p = str(p or "")
    return os.path.relpath(p, run) if os.path.isabs(p) else p


def txt(x, *keys):
    """A list item may be a string or a dict; pick the first present key."""
    if isinstance(x, dict):
        for k in keys:
            if x.get(k) not in (None, ""):
                return str(x[k])
        return json.dumps(x, ensure_ascii=False)
    return str(x)


def load(run):
    with open(os.path.join(run, "final.json")) as f:
        doc = json.load(f)
    fs = list(doc.get("findings") or [])
    # The editor writes long finding lists in chunks, because one reply cannot hold them all.
    for fn in sorted(os.listdir(run)):
        if fn.startswith("final-findings-") and fn.endswith(".json"):
            with open(os.path.join(run, fn)) as f:
                part = json.load(f)
            fs += part.get("findings", []) if isinstance(part, dict) else part
    fs.sort(key=lambda f: (SEV.index(f["severity"]) if f.get("severity") in SEV else 9, str(f.get("id", ""))))
    doc["findings"] = fs
    for key in ("research", "decisions"):
        filename = os.path.join(run, key + ".json")
        if not doc.get(key) and os.path.isfile(filename):
            with open(filename, encoding="utf-8") as f:
                value = json.load(f)
            doc[key] = value.get(key, []) if isinstance(value, dict) else value
        if not isinstance(doc.get(key, []), list):
            raise ValueError(key + " must be an array")
    ids = [f.get("id") for f in fs]
    if len(ids) != len(set(ids)):
        raise ValueError("Duplicate finding IDs: keep findings in final.json OR chunks, not both")
    for key in ("research", "decisions"):
        records = doc.get(key, [])
        if any(not isinstance(record, dict) for record in records):
            raise ValueError(key + " entries must be objects")
    validate(doc, run)
    return doc


def validate(doc, run):
    """Reject broken evidence links and unverified final findings before rendering."""
    fs = doc.get("findings") or []
    groups = {"findings": {f.get("id") for f in fs},
              "research": {r.get("id") for r in doc.get("research") or []},
              "decisions": {d.get("id") for d in doc.get("decisions") or []}}
    errors = []
    def check(owner, refs, group):
        for ref in refs or []:
            if ref not in groups[group]:
                errors.append(f"{owner}: unknown {group} reference {ref}")
    for f in fs:
        check(f.get("id"), f.get("researchSourceIds"), "research")
        check(f.get("id"), f.get("decisionIds"), "decisions")
        if doc.get("status") == "final" and f.get("verdict") not in ("confirmed", "adjusted"):
            errors.append(f"{f.get('id')}: final findings require independent confirmed/adjusted verdict")
        for shot in shots_of(f):
            if not os.path.isfile(os.path.join(run, shot["path"])):
                errors.append(f"{f.get('id')}: missing screenshot {shot['path']}")
    for p in doc.get("priorities") or []:
        if isinstance(p, dict) and p.get("ref"):
            check("priority", [p["ref"]], "findings")
    for r in doc.get("decisions") or []:
        check(r.get("id"), r.get("sourceIds"), "research")
        check(r.get("id"), r.get("findingIds"), "findings")
    for g in doc.get("roadmap") or []:
        check(g.get("phase"), g.get("findingIds"), "findings")
    if errors:
        raise ValueError("Report validation failed:\n" + "\n".join(errors))


def answer_of(q):
    a = str(q.get("answer") or "").strip()
    return "" if a.lower() in ("open", "not asked") else a


def safe_url(value):
    value = str(value or "")
    return value if urlparse(value).scheme.lower() in ("http", "https") else ""


def research_html(d):
    out = []
    for r in d.get("research") or []:
        url = safe_url(r.get("url"))
        title = e(r.get("title") or r.get("id"))
        link = f"<a href='{e(url)}' target='_blank' rel='noopener noreferrer'>{title}</a>" if url else title
        out.append(f"<article class='reference' id='{e(r.get('id'))}'><h3>{e(r.get('id'))}: {link}</h3>"
                   f"<p class=meta>{e(r.get('publisher'))} · {e(r.get('type'))} · accessed {e(r.get('accessed'))}</p>"
                   f"<p>{e(r.get('claim'))}</p><p><b>Limits.</b> {e(r.get('limitations') or 'Not specified')}</p></article>")
    return "".join(out) or "<p class=none>No research sources supplied.</p>"


def source_links(ids):
    return ", ".join(f"<a href='#{e(i)}'>{e(i)}</a>" for i in ids or [])


def decisions_html(d):
    out = []
    for r in d.get("decisions") or []:
        out.append(f"<article class='decision' id='{e(r.get('id'))}'><h3>{e(r.get('id'))} · {e(r.get('questionId'))}: {e(r.get('question'))}</h3>"
                   f"<p><b>Answer.</b> {e(r.get('answer'))}</p><p><b>Basis.</b> {e(r.get('basis'))}</p>"
                   f"<p class=meta>Status: {e(r.get('status') or 'provisional')} · confidence: {e(r.get('confidence'))}</p>"
                   f"<p><b>Sources.</b> {source_links(r.get('sourceIds')) or 'See evidence limitations in the basis.'}</p>"
                   f"<p><b>Affected findings.</b> {source_links(r.get('findingIds')) or 'Product-level decision'}</p>"
                   f"<p><b>Owner intent.</b> {e(r.get('ownerIntent') or 'Not established by research.')}</p>"
                   f"<p><b>Revisit if.</b> {e(r.get('revisitIf') or 'New evidence changes the recommendation.')}</p></article>")
    for q in d.get("questions") or []:
        q = q if isinstance(q, dict) else {"q": str(q)}
        out.append(f"<p><b>{e(q.get('q'))}</b> — {e(answer_of(q) or 'Not resolved; report limitation')}</p>")
    return "".join(out) or "<p class=none>No decisions supplied.</p>"


def research_md(d):
    out = ["## Researched decisions\n"]
    for r in d.get("decisions") or []:
        out.append(f"### {r.get('id')} · {r.get('questionId')}: {r.get('question')}\n\n"
                   f"**Answer:** {r.get('answer')}\n\n**Basis:** {r.get('basis')}\n\n"
                   f"Status: {r.get('status') or 'provisional'}; confidence: {r.get('confidence')}\n\n"
                   f"Sources: {', '.join(r.get('sourceIds') or [])}; findings: {', '.join(r.get('findingIds') or [])}\n\n"
                   f"Owner intent: {r.get('ownerIntent') or 'Not established by research.'}\n\n"
                   f"Revisit if: {r.get('revisitIf') or 'New evidence changes the recommendation.'}\n")
    for q in d.get("questions") or []:
        q = q if isinstance(q, dict) else {"q": str(q)}
        out.append(f"- {q.get('q')} → {answer_of(q) or 'Not resolved; report limitation'}\n")
    out.append("## Research sources\n")
    for r in d.get("research") or []:
        url = safe_url(r.get("url"))
        title = r.get("title") or r.get("id")
        link = f"[{title}]({url})" if url else title
        out.append(f"- **{r.get('id')}: {link}** ({r.get('publisher')}; {r.get('type')}; accessed {r.get('accessed')})\n"
                   f"  {r.get('claim')} Limits: {r.get('limitations') or 'Not specified'}\n")
    return "\n".join(out)


def shots_of(f):
    out = []
    for s in f.get("screenshots") or []:
        out.append(s if isinstance(s, dict) else {"path": s, "caption": ""})
    return out


def table(headers, rows):
    if not rows:
        return "<p class=none>None</p>"
    th = "".join(f"<th>{e(h)}</th>" for h in headers)
    tr = "".join("<tr>" + "".join(f"<td>{e(c)}</td>" for c in r) + "</tr>" for r in rows)
    return f"<table><thead><tr>{th}</tr></thead><tbody>{tr}</tbody></table>"


def ul(items):
    return "<ul>" + "".join(f"<li>{e(i)}</li>" for i in items) + "</ul>" if items else "<p class=none>None</p>"


def html_report(run, d):
    fs = d["findings"]
    counts = {s: sum(1 for f in fs if f.get("severity") == s) for s in SEV}
    prod = d.get("product") or {}
    sec = []
    sec.append(("Summary", "".join(f"<p>{e(p)}</p>" for p in str(d.get("summary") or "None").split("\n\n"))))
    prios = d.get("priorities") or []
    sec.append(("Top priorities", "<ol>" + "".join(
        f"<li>{e(txt(p, 'text'))}" + (f" <a href='#{e(p['ref'])}'>{e(p['ref'])}</a>" if isinstance(p, dict) and p.get("ref") else "") + "</li>"
        for p in prios) + "</ol>" if prios else "<p class=none>None</p>"))
    if d.get("roadmap"):
        groups = []
        for g in d["roadmap"]:
            groups.append(f"<article class='decision'><h3>{e(g.get('phase'))}: {e(g.get('title'))}</h3>"
                          f"<p>{e(g.get('outcome'))}</p><p><b>Recommendations.</b> {source_links(g.get('findingIds'))}</p>"
                          + "<h4>Acceptance criteria</h4>" + ul(g.get("acceptanceCriteria") or []) + "</article>")
        sec.append(("Recommended sequence", "".join(groups)))
    if d.get("taskOutcomes"):
        sec.append(("Tested task outcomes", table(["Task", "Outcome", "Evidence / limit"],
                    [[t.get("task"), t.get("outcome"), t.get("evidence")] for t in d["taskOutcomes"]])))
    users = [[txt(u, "persona"), u.get("goals", "") if isinstance(u, dict) else ""] for u in prod.get("users") or []]
    tasks = [[t.get("id"), t.get("name"), t.get("persona"), t.get("priority")] for t in prod.get("tasks") or [] if isinstance(t, dict)]
    sec.append(("The product as understood",
                f"<p>{e(prod.get('purpose') or 'None')}</p><h3>Users</h3>" + table(["Persona", "Goals"], users)
                + "<h3>Main tasks</h3>" + table(["Id", "Task", "Persona", "Priority"], tasks)
                + ("<h3>Your corrections</h3>" + ul(prod.get("corrections")) if prod.get("corrections") else "")))
    sec.append(("Screen inventory", table(["Screen", "URL", "Section", "Reviewed on"],
                [[s.get("name"), s.get("url"), s.get("section"), ", ".join(s.get("devices") or [])] for s in d.get("screens") or []])))
    walks = []
    for w in d.get("walks") or []:
        steps = table(["#", "Screen", "Action", "Pass", "Note"],
                      [[s.get("n"), s.get("screen"), s.get("action"), "yes" if s.get("pass") else "no", s.get("note")] for s in w.get("steps") or []])
        recovery = "; ".join(f"{k}: {v}" for k, v in (w.get("recovery") or {}).items())
        status = "Complete within stated scope" if w.get("completed") else "Partial / incomplete"
        walks.append(f"<details><summary><b>{e(w.get('task'))}</b> · {e(status)} · {e(w.get('persona'))} · {e(w.get('device'))} · {e(w.get('clicks'))} clicks · {e(w.get('decisions'))} decisions</summary>"
                     f"<p><b>Scope.</b> {e(w.get('scopeNote') or w.get('name'))}</p><p><b>Recovery.</b> {e(recovery or 'Not recorded')}</p>"
                     f"<p><b>Dead ends.</b> {e('; '.join(w.get('deadEnds') or []) or 'None')}</p><p><b>Ideal flow.</b> {e(w.get('idealFlow'))}</p>{steps}</details>")
    sec.append(("Task walkthroughs", "".join(walks) or "<p class=none>None</p>"))
    sec.append(("Hidden and hard-to-find elements", table(["Element", "Where", "How to reach it", "Finding"],
                [[h.get("item"), h.get("where"), h.get("howToReach"), h.get("finding")] for h in d.get("hidden") or [] if isinstance(h, dict)])))
    cards = []
    for f in fs:
        sev = f.get("severity", "low")
        shots = "".join(f"<figure><a href='{e(rel(run, s['path']))}' target=_blank><img loading=lazy src=\"{e(rel(run, s['path']))}\" alt='{e(s.get('caption') or 'screenshot')}'></a>"
                        f"<figcaption>{e(s.get('caption'))}</figcaption></figure>" for s in shots_of(f))
        steps = f.get("steps")
        steps = "; ".join(steps) if isinstance(steps, list) else steps
        rows = [("Observed", f.get("observation")), ("Impact", f.get("impact")), ("Recommendation", f.get("recommendation")),
                ("Principle", f.get("principle")), ("Steps", steps), ("Decision note", f.get("answer")), ("Verification", f.get("verdict")), ("Verifier note", f.get("verifierNote")), ("Limitations", f.get("limitations"))]
        body = "".join(f"<p><b>{k}.</b> {e(v)}</p>" for k, v in rows if v)
        if f.get("contextEvidence"):
            body += "<h4>Affected contexts</h4>" + ul([str(c.get("sourceId")) + ": " + str(c.get("observation")) for c in f["contextEvidence"]])
        if f.get("acceptanceCriteria"):
            criteria = f["acceptanceCriteria"]
            body += "<h4>Acceptance criteria</h4>" + ul(criteria if isinstance(criteria, list) else [criteria])
        if f.get("researchSourceIds"):
            body += "<p><b>Research.</b> " + source_links(f["researchSourceIds"]) + "</p>"
        if f.get("decisionIds"):
            body += "<p><b>Decisions.</b> " + source_links(f["decisionIds"]) + "</p>"
        if f.get("sourceIds"):
            body += "<p class=meta><b>Original evidence IDs.</b> " + e(", ".join(f["sourceIds"])) + "</p>"
        major = " · <b>major recommendation; owner approval not implied</b>" if f.get("major") else ""
        cards.append(f"<article id='{e(f.get('id'))}' class=card data-sev='{e(sev)}' data-dev='{e(' '.join(f.get('devices') or []))}' data-kind='{e(f.get('kind'))}'>"
                     f"<header><span class=sev style='--c:{SEV_COL.get(sev, '#555')}'>{e(sev)}</span> <span class=fid>{e(f.get('id'))}</span> <h3>{e(f.get('title'))}</h3></header>"
                     f"<p class=meta>{e(f.get('screen'))} · {e(', '.join(f.get('devices') or []))} · {e(f.get('kind'))} · effort {e(f.get('effort'))} · found by {e(f.get('foundBy', 1))}{major}</p>"
                     f"<div class=body><div>{body}</div><div class=shots>{shots}</div></div></article>")
    filt = "".join(f"<button data-f='{s}'>{s} ({counts[s]})</button>" for s in SEV)
    sec.append(("Findings", f"<div class=filters><button data-f=''>all ({len(fs)})</button>{filt}</div>" + ("".join(cards) or "<p class=none>None</p>")))
    if d.get("heuristics"):
        sec.append(("Heuristic assessment", table(["Heuristic", "Assessment", "Evidence"],
                    [[h.get("name"), h.get("assessment", "Qualitative observation"), h.get("why")] for h in d["heuristics"] if isinstance(h, dict)])))
    sec.append(("What works well", ul([txt(s, "text") for s in d.get("strengths") or []])))
    sec.append(("Researched decisions", decisions_html(d)))
    sec.append(("Research sources", research_html(d)))
    if d.get("methodology"):
        sec.append(("Method and evidence limits", f"<p>{e(d['methodology'])}</p>"))
    if d.get("coverage"):
        sec.append(("Coverage and provenance", table(["Area", "Actual coverage", "Evidence"],
                    [[c.get("area"), c.get("coverage"), ", ".join(c.get("sourceFiles") or [])] for c in d["coverage"]])))
    if d.get("qaNotes"):
        sec.append(("Review quality checks", ul([txt(q, "text") for q in d["qaNotes"]])))
    sec.append(("Test data created", table(["What", "Where", "Removed"],
                [[c.get("what"), c.get("where"), "yes" if c.get("removed") else "no"] for c in d.get("created") or [] if isinstance(c, dict)])))
    sec.append(("Not covered", ul([txt(n, "text") for n in d.get("notCovered") or []])))
    order = ["Summary", "Top priorities", "Recommended sequence", "Tested task outcomes", "Findings", "What works well", "Researched decisions", "Heuristic assessment", "Method and evidence limits", "Coverage and provenance", "Review quality checks", "Not covered", "The product as understood", "Screen inventory", "Task walkthroughs", "Hidden and hard-to-find elements", "Test data created", "Research sources"]
    sec.sort(key=lambda item: order.index(item[0]) if item[0] in order else len(order))
    nav = "".join(f"<a href='#s{i}'>{e(t)}</a>" for i, (t, _) in enumerate(sec))
    body = "".join(f"<section id='s{i}'><h2>{e(t)}</h2>{c}</section>" for i, (t, c) in enumerate(sec))
    tally = " · ".join(f"{counts[s]} {s}" for s in SEV)
    return f"""<!doctype html><html lang=en><head><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1">
<title>UX review: {e(d.get('app') or 'app')}</title><style>
:root{{--bg:#fff;--fg:#1d1d1f;--mut:#5f6368;--line:#e3e3e3;--card:#fafafa}}
@media (prefers-color-scheme:dark){{:root{{--bg:#16181c;--fg:#ececec;--mut:#a8adb4;--line:#2e3238;--card:#1e2126}}}}
body{{margin:0;background:var(--bg);color:var(--fg);font:16px/1.5 system-ui,-apple-system,sans-serif}}
main{{max-width:1100px;margin:auto;padding:16px}} nav{{display:flex;flex-wrap:wrap;gap:8px 14px;font-size:14px;margin:8px 0 24px}}
a{{color:inherit}} table{{border-collapse:collapse;width:100%;font-size:14px;display:block;overflow-x:auto}} th,td{{border-bottom:1px solid var(--line);padding:6px 8px;text-align:left;vertical-align:top}}
.card{{border:1px solid var(--line);background:var(--card);border-radius:8px;padding:12px 14px;margin:12px 0}} .card h3{{display:inline;font-size:17px}}
.sev{{background:var(--c);color:#fff;border-radius:4px;padding:2px 7px;font-size:13px;font-weight:700}} .fid{{font-weight:700;color:var(--mut)}}
.meta,.none{{color:var(--mut);font-size:14px}} .body{{display:grid;grid-template-columns:1fr;gap:12px}} @media (min-width:800px){{.body{{grid-template-columns:3fr 2fr}}}}
.shots img{{max-width:100%;border:1px solid var(--line);border-radius:4px}} figure{{margin:0 0 8px}} figcaption{{font-size:13px;color:var(--mut)}}
.filters button{{margin:0 6px 6px 0;padding:6px 10px;border-radius:6px;border:1px solid var(--line);background:var(--card);color:var(--fg);cursor:pointer}}
details{{border-bottom:1px solid var(--line);padding:8px 0}}
.qform{{padding-left:22px}} .qform li{{margin:0 0 14px}} .qform label{{display:block;margin-bottom:4px}}
.qform textarea{{width:100%;box-sizing:border-box;font:inherit;font-size:16px;padding:8px;border:1px solid var(--line);border-radius:6px;background:var(--card);color:var(--fg)}}
.qbar{{position:sticky;bottom:0;background:var(--bg);padding:10px 0;border-top:1px solid var(--line)}}
.qbar button{{padding:8px 14px;border-radius:6px;border:1px solid var(--line);background:var(--card);color:var(--fg);cursor:pointer;font:inherit}} #qdl{{background:#1864ab;color:#fff;border-color:#1864ab}}
</style></head><body><main>
<h1>UX review: {e(d.get('app') or 'app')}</h1><p class=meta>{e(d.get('baseUrl'))} · {e(d.get('date'))} · {e(', '.join(d.get('devices') or []))} · {len(fs)} findings: {e(tally)}</p>
<nav>{nav}</nav>{body}</main>
<script>
document.querySelectorAll('.filters button').forEach(b=>b.onclick=()=>document.querySelectorAll('.card').forEach(c=>c.hidden=!!b.dataset.f&&c.dataset.sev!==b.dataset.f))</script>
</body></html>"""


def md_report(run, d):
    fs, prod, L = d["findings"], d.get("product") or {}, []
    add = L.append
    add(f"# UX review: {d.get('app') or 'app'}\n\n{d.get('baseUrl') or ''} · {d.get('date') or ''} · {len(fs)} findings\n")
    add("## Summary\n\n" + (d.get("summary") or "None") + "\n")
    add("## Top priorities\n\n" + ("\n".join(f"{i}. {txt(p, 'text')}" + (f" ({p['ref']})" if isinstance(p, dict) and p.get('ref') else "")
                                             for i, p in enumerate(d.get("priorities") or [], 1)) or "None") + "\n")
    if d.get("roadmap"):
        add("## Recommended sequence\n")
        for g in d["roadmap"]:
            add(f"### {g.get('phase')}: {g.get('title')}\n\n{g.get('outcome')}\n\nRecommendations: {', '.join(g.get('findingIds') or [])}\n\nAcceptance criteria:\n\n"
                + "\n".join("- " + str(c) for c in g.get("acceptanceCriteria") or []) + "\n")
    if d.get("taskOutcomes"):
        add("## Tested task outcomes\n\n" + "\n".join(f"- **{t.get('task')}: {t.get('outcome')}** — {t.get('evidence')}" for t in d["taskOutcomes"]) + "\n")
    add("## The product as understood\n\n" + (prod.get("purpose") or "None") + "\n\n"
        + "\n".join(f"- Task {t.get('id')}: {t.get('name')} ({t.get('persona')})" for t in prod.get("tasks") or [] if isinstance(t, dict)) + "\n")
    add("## Screen inventory\n\n| Screen | URL | Section | Reviewed on |\n|---|---|---|---|\n"
        + "\n".join(f"| {s.get('name')} | {s.get('url')} | {s.get('section')} | {', '.join(s.get('devices') or [])} |" for s in d.get("screens") or []) + "\n")
    add("## Task walkthroughs\n\n" + ("\n".join(
        f"### {w.get('task')}\n{w.get('persona')} · {w.get('device')} · {w.get('clicks')} clicks · {w.get('decisions')} decisions\n\n"
        f"Status: {'Complete within stated scope' if w.get('completed') else 'Partial / incomplete'}\n\nScope: {w.get('scopeNote') or w.get('name')}\n\nRecovery: {json.dumps(w.get('recovery') or {}, ensure_ascii=False)}\n\n"
        f"Dead ends: {'; '.join(w.get('deadEnds') or []) or 'None'}\n\nIdeal flow: {w.get('idealFlow')}\n" for w in d.get("walks") or []) or "None") + "\n")
    add("## Hidden and hard-to-find elements\n\n" + ("\n".join(
        f"- **{h.get('item')}**: {h.get('where')}; reach: {h.get('howToReach')} ({h.get('finding')})" for h in d.get("hidden") or [] if isinstance(h, dict)) or "None") + "\n")
    add("## Findings\n")
    for f in fs:
        steps = f.get("steps")
        steps = "; ".join(steps) if isinstance(steps, list) else steps
        add(f"### {f.get('id')} {f.get('title')}\n- Severity / Effort: {f.get('severity')} / {f.get('effort')}" + (" · major recommendation; owner approval not implied" if f.get("major") else "")
            + f"\n- Where: {f.get('screen')}, {', '.join(f.get('devices') or [])}\n- Observed: {f.get('observation')}\n- Impact: {f.get('impact') or ''}"
            + f"\n- Recommendation: {f.get('recommendation')}\n- Principle: {f.get('principle') or ''}\n- Steps: {steps or ''}\n- Found by: {f.get('foundBy', 1)}"
            + (f"\n- Decision note: {f.get('answer')}" if f.get("answer") else "")
            + "".join(f"\n- Screenshot: {rel(run, s['path'])} ({s.get('caption', '')})" for s in shots_of(f)) + "\n")
    for f in fs:
        details = []
        if f.get("verdict"):
            details.append("Verification: " + str(f["verdict"]) + "; " + str(f.get("verifierNote") or ""))
        if f.get("researchSourceIds"):
            details.append("Research: " + ", ".join(f["researchSourceIds"]))
        if f.get("decisionIds"):
            details.append("Decisions: " + ", ".join(f["decisionIds"]))
        criteria = f.get("acceptanceCriteria") or []
        if criteria:
            details.append("Acceptance criteria: " + "; ".join(criteria if isinstance(criteria, list) else [criteria]))
        if f.get("limitations"):
            details.append("Limitations: " + str(f["limitations"]))
        if f.get("sourceIds"):
            details.append("Original evidence IDs: " + ", ".join(f["sourceIds"]))
        for c in f.get("contextEvidence") or []:
            details.append("Context " + str(c.get("sourceId")) + ": " + str(c.get("observation")))
        if details:
            add("### " + str(f.get("id")) + " evidence and acceptance\n\n" + "\n".join("- " + v for v in details) + "\n")
    if not fs:
        add("None\n")
    if d.get("heuristics"):
        add("## Heuristic assessment\n\n| Heuristic | Assessment | Evidence |\n|---|---|---|\n"
            + "\n".join(f"| {h.get('name')} | {h.get('assessment', 'Qualitative observation')} | {h.get('why')} |" for h in d["heuristics"] if isinstance(h, dict)) + "\n")
    add("## What works well\n\n" + ("\n".join(f"- {txt(s, 'text')}" for s in d.get("strengths") or []) or "None") + "\n")
    add(research_md(d))
    if d.get("methodology"):
        add("## Method and evidence limits\n\n" + str(d["methodology"]) + "\n")
    if d.get("coverage"):
        add("## Coverage and provenance\n\n" + "\n".join(f"- **{c.get('area')}**: {c.get('coverage')} Evidence: {', '.join(c.get('sourceFiles') or [])}" for c in d["coverage"]) + "\n")
    if d.get("qaNotes"):
        add("## Review quality checks\n\n" + "\n".join("- " + txt(q, "text") for q in d["qaNotes"]) + "\n")
    add("## Test data created\n\n" + ("\n".join(f"- {c.get('what')} ({c.get('where')}), removed: {'yes' if c.get('removed') else 'no'}"
                                               for c in d.get("created") or [] if isinstance(c, dict)) or "None") + "\n")
    add("## Not covered\n\n" + ("\n".join(f"- {txt(n, 'text')}" for n in d.get("notCovered") or []) or "None") + "\n")
    return "\n".join(L)


def main():
    if len(sys.argv) != 2:
        print("usage: report.py RUN"); sys.exit(2)
    run = os.path.abspath(os.path.expanduser(sys.argv[1]))
    if not os.path.isfile(os.path.join(run, "final.json")):
        print(f"no final.json in {run}"); sys.exit(1)
    d = load(run)
    hp, mp = os.path.join(run, "report.html"), os.path.join(run, "report.md")
    with open(hp, "w") as f:
        f.write(html_report(run, d))
    with open(mp, "w") as f:
        f.write(md_report(run, d))
    print(json.dumps({"html": hp, "md": mp, "findings": len(d["findings"])}))


if __name__ == "__main__":
    main()
