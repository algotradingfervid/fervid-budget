package app

const budgetPlannerTemplate = `
{{define "budget-planner"}}{{template "top" .}}
<link rel="stylesheet" href="/static/budget-planner.css">
<section class="budget-planner" id="budget-planner" data-server-error="{{if .Error}}true{{else}}false{{end}}">
<header class="bp-header"><div><p class="bp-eyebrow">Monthly planning</p><h1 id="bp-page-title">Create a monthly budget</h1><p class="muted" id="bp-intro">Start fresh or reuse an existing month, then review before saving.</p></div><a href="/budgets">Budget overview</a></header>
<noscript><div class="alert error">This guided planner needs JavaScript. Enable JavaScript, or use the <a href="/budgets">budget overview</a>.</div></noscript>
<div id="bp-status" class="bp-notice" role="status" hidden></div>
<ol class="bp-steps" aria-label="Budget planning steps"><li aria-current="step">1. Choose month</li><li>2. Set amounts</li><li id="bp-review-step">3. Review &amp; create</li></ol>
<p class="bp-error" id="bp-error" role="alert" tabindex="-1" hidden></p>
<section class="bp-panel" id="bp-step-1" aria-labelledby="bp-title-1">
<h2 id="bp-title-1" tabindex="-1">Which month are you planning?</h2>
<label class="bp-field" for="bp-month">Budget month<input id="bp-month" type="month" required min="0001-01" max="9999-12"></label>
<fieldset class="bp-start"><legend>How would you like to start?</legend>
<label class="bp-choice"><input type="radio" name="bp-start" value="blank" checked><span>Start with a blank budget<small>Begin with no projects, heads, or lines. Add only what you need.</small></span></label>
<label class="bp-choice"><input type="radio" name="bp-start" value="copy" id="bp-copy"><span>Copy an existing budget<small>Reuse its budget lines and amounts, then adjust what has changed.</small></span></label>
</fieldset>
<div id="bp-source" hidden><label class="bp-field" for="bp-source-month">Copy from<select id="bp-source-month"></select></label><p class="bp-hint" id="bp-source-preview"></p></div>
<p class="bp-hint">Only the plan is copied. Payments and actual spending remain in their original month. Retired projects and heads are not copied.</p>
<div class="bp-actions bp-end"><button type="button" class="primary" id="bp-continue">Continue to amounts →</button></div>
</section>
<section class="bp-panel" id="bp-step-2" aria-labelledby="bp-title-2" hidden>
<h2 id="bp-title-2" tabindex="-1">Set your planned spending</h2><p class="muted" id="bp-context"></p>
<p class="bp-hint">Add projects, expense heads, and the lines that make up each head. Line amounts roll up into head, project, and monthly totals.</p>
<div class="bp-outline-controls" id="bp-outline-controls" hidden><button type="button" id="bp-expand">Expand all</button><button type="button" id="bp-collapse">Collapse all</button></div>
<div id="bp-groups"></div><button type="button" class="bp-add-project" id="bp-add-project">＋ Add project</button>
<div class="bp-total"><span>Total monthly budget<small id="bp-count" class="muted"></small></span><output id="bp-live-total" aria-label="Total monthly budget"></output></div>
<div class="bp-actions"><button type="button" data-bp-back="1" id="bp-back-month">← Back</button><button type="button" class="primary" id="bp-review">Review budget →</button></div>
</section>
<section class="bp-panel" id="bp-step-3" aria-labelledby="bp-title-3" hidden>
<h2 id="bp-title-3" tabindex="-1">Review before creating</h2><p class="muted">Check the month, budget lines, and totals. Go back to adjust anything before saving.</p>
<div class="bp-summary"><div>Budget month<strong id="bp-review-month"></strong></div><div>Starting point<strong id="bp-review-source"></strong></div></div>
<div id="bp-review-groups"></div><div class="bp-additions" id="bp-additions" hidden></div>
<div class="bp-total"><span>Total monthly budget</span><strong id="bp-review-total"></strong></div>
<p class="bp-hint">This saves your plan. Actual spending appears when payments are recorded against the relevant expense heads and payment dates. Budget lines are planning detail; spending is tracked at the expense head level.</p>
<div class="bp-actions"><button type="button" data-bp-back="2">← Edit amounts</button><form action="/budgets/plan" method="post" id="bp-save-form"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="mode" id="bp-save-mode" value="create"><input type="hidden" name="draft" id="bp-save-draft"><button type="submit" class="primary" id="bp-save">Create budget</button></form></div>
</section>
<nav class="bp-links" aria-label="Budget related pages"><a id="bp-grid-link" href="/grid">Budget versus actual</a><a href="/months">Monthly plans</a></nav>
<dialog id="bp-item-dialog" aria-labelledby="bp-item-title"><form id="bp-item-form"><h2 id="bp-item-title">Add project</h2><p class="muted" id="bp-item-context"></p><label class="bp-field" id="bp-existing-label" for="bp-existing">Choose an existing project<select id="bp-existing"></select></label><div id="bp-new-name" hidden><label class="bp-field" id="bp-name-label" for="bp-name">Project name<input id="bp-name" maxlength="120" autocomplete="off"></label><p class="bp-hint">New items are created only when you save the completed budget.</p></div><p class="bp-error" id="bp-item-error" role="alert" hidden></p><div class="bp-actions"><button type="button" id="bp-item-cancel">Cancel</button><button type="submit" class="primary" id="bp-item-save">Add project</button></div></form></dialog>
</section>
<script type="application/json" id="budget-planner-data">{{.BudgetPlannerJSON}}</script>
<script src="/static/budget-planner.js" defer></script>
{{template "bottom" .}}{{end}}
`
