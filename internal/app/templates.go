package app

import (
	"strings"
	"unicode"
)

// Initials renders the signed-in user's avatar text. Two letters at most, so
// the round avatar in the sidebar never overflows.
func (d PageData) Initials() string {
	initials := make([]rune, 0, 2)
	for _, word := range strings.Fields(d.User.Name) {
		for _, letter := range word {
			if unicode.IsLetter(letter) || unicode.IsDigit(letter) {
				initials = append(initials, unicode.ToUpper(letter))
			}
			break
		}
		if len(initials) == 2 {
			break
		}
	}
	if len(initials) == 0 {
		return "FB"
	}
	return string(initials)
}

const templates = `
{{define "top"}}
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} - Fervid Budget</title>
  <link rel="stylesheet" href="/static/fervid-ds.css">
  <script src="/static/htmx.min.js" defer></script>
  <script src="/static/fervid-app.js" defer></script>
</head>
<body>
{{if eq .Shell.Chrome "app"}}<div class="appshell">
{{template "shell_sidebar" .}}
{{template "shell_topbar" .}}
{{end}}
<main class="page">
<div class="page-inner">
{{if and .Error (not .ErrorCode)}}<div class="alert error" role="alert" aria-live="assertive">{{.Error}}</div>{{end}}
{{if .Notice}}<div class="alert success">{{.Notice}}</div>{{end}}
{{end}}

{{define "bottom"}}</div></main>
{{if eq .Shell.Chrome "app"}}{{template "shell_tabbar" .}}
{{template "shell_more" .}}
</div>{{end}}
</body></html>{{end}}

{{/* Soon items are announcements, not links, so they carry no href at all.
     Badges are counts the caller is entitled to see; a zero count renders
     nothing rather than a bare "0". */}}
{{define "shell_sidebar"}}<aside class="sidebar">
  <div class="side-brand"><span class="sq">F</span><span><b>Fervid Budget</b><small>Smart Solutions</small></span></div>
  {{range .Shell.Groups}}{{if .Title}}<div class="sgroup">{{.Title}}</div>{{end}}
  <nav class="side-nav" aria-label="{{if .Title}}{{.Title}}{{else}}Primary{{end}}">{{range .Items}}{{if .Soon}}<a class="soon" aria-disabled="true"><span class="ico">{{.Icon}}</span>{{.Label}}<span class="n">Soon</span></a>{{else}}<a class="{{if eq $.Shell.Active .Key}}active{{end}}" href="{{.Href}}"{{if eq $.Shell.Active .Key}} aria-current="page"{{end}}><span class="ico">{{.Icon}}</span>{{.Label}}{{$count := index $.Shell.Badges .Badge}}{{if $count}}<span class="n">{{$count}}</span>{{end}}</a>{{end}}{{end}}</nav>
  {{end}}
  <div class="side-user"><span class="avatar">{{.Initials}}</span><span><strong>{{.User.Name}}</strong><small>{{.Shell.RoleNames}}</small></span></div>
  {{/* "Log out" here and in the More sheet: one action, one name on every device. */}}
  <form class="side-logout" method="post" action="/logout"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>Log out</button></form>
</aside>{{end}}

{{define "shell_topbar"}}<header class="m-topbar">
  <a class="m-back" href="{{if .Shell.BackHref}}{{.Shell.BackHref}}{{else}}/{{end}}" aria-label="{{if .Shell.BackHref}}Back{{else}}Home{{end}}">{{if .Shell.BackHref}}‹{{else}}⌂{{end}}</a>
  <div class="m-title"><b>{{if .Shell.Title}}{{.Shell.Title}}{{else}}Fervid Budget{{end}}</b>{{if .Shell.Sub}}<small>{{.Shell.Sub}}</small>{{end}}</div>
  <div class="m-actions">{{if and .Shell.ActionLabel .Shell.ActionHref}}<a class="m-cta" href="{{.Shell.ActionHref}}">{{.Shell.ActionLabel}}</a>{{end}}<a class="m-icon" href="/notifications" aria-label="Notifications">✉{{if .Shell.Unread}}<span class="dot">{{.Shell.Unread}}</span>{{end}}</a></div>
</header>{{end}}

{{define "shell_tabbar"}}<nav class="tabbar" aria-label="Primary">
  {{$shell := .Shell}}{{range .Shell.Tabs.Left}}<a href="{{.Href}}" class="{{if eq $shell.Active .Key}}active{{end}}"><span class="ti">{{.Icon}}</span>{{.Label}}</a>{{end}}
  <a class="fab-wrap" href="{{.Shell.Tabs.Fab.Href}}"><span class="fab">{{.Shell.Tabs.Fab.Icon}}</span>{{.Shell.Tabs.Fab.Label}}</a>
  {{range .Shell.Tabs.Right}}{{if eq .Key "more"}}<a href="{{.Href}}" class="js-more"><span class="ti">{{.Icon}}</span>{{.Label}}</a>{{else}}<a href="{{.Href}}" class="{{if eq $shell.Active .Key}}active{{end}}"><span class="ti">{{.Icon}}</span>{{.Label}}</a>{{end}}{{end}}
</nav>{{end}}

{{define "shell_more"}}<div class="more-sheet" hidden>
  <div class="ms-head"><b>{{.User.Name}}</b><button type="button" class="ms-close" aria-label="Close">✕</button></div>
  {{range .Shell.Groups}}<div class="ms-group">{{if .Title}}{{.Title}}{{else}}Overview{{end}}</div>
  <div class="ms-list">{{range .Items}}{{if .Soon}}<a class="soon" aria-disabled="true"><span class="ico">{{.Icon}}</span>{{.Label}}<span class="n">Soon</span></a>{{else}}<a href="{{.Href}}"{{if eq $.Shell.Active .Key}} class="active" aria-current="page"{{end}}><span class="ico">{{.Icon}}</span>{{.Label}}{{$count := index $.Shell.Badges .Badge}}{{if $count}}<span class="n">{{$count}}</span>{{end}}</a>{{end}}{{end}}</div>
  {{end}}
  <div class="ms-group">Session</div>
  <div class="ms-list"><form class="ms-logout" method="post" action="/logout"><input type="hidden" name="csrf" value="{{.CSRF}}"><button type="submit"><span class="ico">⏻</span>Log out</button></form></div>
</div>{{end}}

{{define "login"}}
<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Login - Fervid Budget</title><link rel="stylesheet" href="/static/fervid-ds.css"></head>
<body class="login-body"><main class="login-card"><h1>Fervid Budget</h1>{{if .Error}}<div class="alert error" role="alert" aria-live="assertive">{{.Error}}</div>{{end}}<form method="post" action="/login" class="stack"><label>Email<input name="email" type="email" autocomplete="username" required></label><label>Password<input name="password" type="password" autocomplete="current-password" required></label><button class="primary">Login</button></form></main></body></html>
{{end}}

{{define "error_page"}}
{{template "top" .}}
<section class="error-state" role="alert" aria-live="assertive">
  <div class="error-code">{{.ErrorCode}}</div>
  <div>
    <div class="eyebrow">Request could not be completed</div>
    <h1>{{.Title}}</h1>
    <p>{{.Error}}</p>
    <p class="request-id">Request ID: <code>{{.RequestID}}</code></p>
    <div class="cluster"><a class="btn primary" href="/">Return to dashboard</a><a class="btn outline" href="javascript:history.back()">Go back</a></div>
  </div>
</section>
{{template "bottom" .}}
{{end}}

{{define "page_header"}}
<section class="page-banner">
  <div>
    <div class="eyebrow">{{.Kicker}}</div>
    <h1>{{.Heading}}</h1>
    <p class="sub muted">{{.Sub}}</p>
  </div>
  {{.Action}}
</section>
{{end}}

{{define "grid"}}
{{template "top" .}}
<div class="gridhead">
  <div class="between">
    <div><div class="eyebrow">Budget vs actuals</div><h1>Variance grid</h1><p class="sub muted">{{.Month}} · {{len .Grid.Rows}} heads · remaining = budget - actual</p></div>
    <div class="head-actions"><span class="countpill">{{len .Grid.Projects}} projects</span>{{if .Grid.Locked}}<span class="pill warn">Locked</span>{{end}}</div>
  </div>
  <div class="metric-strip compact">
    <div class="metric"><span class="metric-label">Budget</span><span class="metric-value">{{short .Grid.Total.Budget}}</span></div>
    <div class="metric"><span class="metric-label">Actual</span><span class="metric-value">{{short .Grid.Total.Actual}}</span></div>
    <div class="metric"><span class="metric-label">Remaining</span><span class="metric-value {{varClass .Grid.Total.Variance}}">{{short .Grid.Total.Variance}}</span></div>
    <div class="metric"><span class="metric-label">Used</span><span class="metric-value">{{usedText .Grid.Total.Budget .Grid.Total.Actual}}</span></div>
  </div>
  {{/* action="/grid", not "/". / was the variance grid until Phase 4 made it the
       dashboard, which reads none of these three parameters — so the grid's own
       filters navigated away from the thing they filter and the headline screen
       could not be filtered at all (F-G-029). The two buttons beside them are
       gated on the verbs their own routes are gated on: /export.csv is
       grid:export and /payments/new is payment:create, and a grid:view-only role
       was offered both. */}}
  <form class="toolbar" method="get" action="/grid" aria-label="Grid filters">
    <label>Month<input class="input" type="month" name="month" value="{{.Month}}"></label>
    <label>Status<select class="select" name="status"><option value="all">All</option><option value="unbudgeted" {{select .Status "unbudgeted"}}>Unbudgeted spend</option><option value="over" {{select .Status "over"}}>Over budget</option><option value="under" {{select .Status "under"}}>Under budget</option><option value="on-track" {{select .Status "on-track"}}>On track</option><option value="not-paid" {{select .Status "not-paid"}}>Not paid</option></select></label>
    <label class="search-label">Search<input class="input" name="q" value="{{.Query}}" placeholder="Search head or project..."></label>
    <div class="spacer"></div>
    <button>Apply</button>{{if .Perms.Can "grid" "export"}}<a class="btn outline" href="/export.csv?month={{.Month}}&status={{.Status}}&q={{.Query}}">⤓ Export</a>{{end}}{{if .Perms.Can "payment" "create"}}<a class="btn primary" href="/payments/new?month={{.Month}}">+ Add payment</a>{{end}}
  </form>
  <div class="legend"><span class="lk"><span class="sw none"></span>Not paid {{.Grid.NotPaid}}</span><span class="lk"><span class="sw warn"></span>Unbudgeted {{.Grid.Unbudgeted}}</span><span class="lk"><span class="sw ok"></span>Under {{.Grid.Under}}</span><span class="lk"><span class="sw track"></span>On track {{.Grid.OnTrack}}</span><span class="lk"><span class="sw bad"></span>Over {{.Grid.Over}}</span></div>
</div>
{{if .Grid.Locked}}<div class="locked">Month {{.Month}} is locked by {{.Grid.Lock.ActorName}}. Reason: {{.Grid.Lock.Reason}}</div>{{end}}
<div class="gridwrap d-only"><div class="card scroll"><table class="matrix grid"><caption class="sr-only">Budget versus actuals by project and head</caption><thead><tr><th class="sticky project-col">Project</th><th class="sticky head-col">Head</th><th class="c">Due</th><th class="num">Budget</th><th class="num">Actual</th><th class="num">Remaining ₹</th><th class="num">Remaining %</th><th class="c">Used</th><th class="c">Status</th><th class="c">Action</th></tr></thead><tbody>{{range .Grid.Groups}}<tr class="project-row" data-project-id="{{.ProjectID}}"><td class="sticky project-col" colspan="2"><button class="project-toggle" type="button" data-project-toggle="{{.ProjectID}}" aria-expanded="true"><span class="chev" aria-hidden="true">▾</span><span class="project-title">{{.Project}}</span><span class="project-count">{{len .Rows}} heads</span></button></td><td></td><td class="num">{{money .Total.Budget}}</td><td class="num">{{money .Total.Actual}}</td><td class="num {{varClass .Total.Variance}}">{{money .Total.Variance}}</td><td class="num">{{remainingText .Total.Budget .Total.Actual}}</td><td><div class="usedcell"><div class="vbar {{barClass (statusFor .Total.Budget .Total.Actual)}}"><i style="--pct:{{usedPct .Total.Budget .Total.Actual}}"></i><span class="mark"></span></div><span class="pctxt">{{usedText .Total.Budget .Total.Actual}}</span></div></td><td></td><td></td></tr>{{range .Rows}}<tr class="head {{if eq .Status "not-paid"}}unpaid{{end}}" data-project-row="{{.ProjectID}}"><td class="sticky project-col project-spacer" aria-hidden="true"></td><td class="sticky head-col"><span class="hname">{{.Head}}</span></td><td class="c due"><span class="duepill {{dueClass .DueDay $.Month .Status}}">{{dueText .DueDay $.Month .Status}}</span><small>{{.DueDay}}</small></td><td class="num">{{money .Budget}}</td><td class="num">{{money .Actual}}</td><td class="num {{varClass .Variance}}">{{money .Variance}}</td><td class="num">{{remainingText .Budget .Actual}}</td><td><div class="usedcell"><div class="vbar {{barClass .Status}}"><i style="--pct:{{usedPct .Budget .Actual}}"></i><span class="mark"></span></div><span class="pctxt">{{usedText .Budget .Actual}}</span></div></td><td class="c"><span class="pill {{.Status}}">{{statusText .Status}}</span></td><td class="c">{{if and .Active (not $.Grid.Locked) ($.Perms.Can "payment" "create")}}<a class="btn small" href="/payments/new?month={{$.Month}}&head_id={{.HeadID}}">Add</a>{{else if $.Grid.Locked}}<small class="muted">Locked</small>{{else if not .Active}}<small class="muted">Retired</small>{{end}}</td></tr>{{end}}{{else}}<tr><td colspan="10" class="empty">No heads found. Add projects and heads to begin.</td></tr>{{end}}</tbody><tfoot><tr class="total"><td colspan="2"><span class="tlabel">Company total</span></td><td class="c">—</td><td class="num">{{money .Grid.Total.Budget}}</td><td class="num">{{money .Grid.Total.Actual}}</td><td class="num {{varClass .Grid.Total.Variance}}">{{money .Grid.Total.Variance}}</td><td class="num">{{remainingText .Grid.Total.Budget .Grid.Total.Actual}}</td><td><div class="usedcell"><div class="vbar {{barClass (statusFor .Grid.Total.Budget .Grid.Total.Actual)}}"><i style="--pct:{{usedPct .Grid.Total.Budget .Grid.Total.Actual}}"></i><span class="mark"></span></div><span class="pctxt">{{usedText .Grid.Total.Budget .Grid.Total.Actual}}</span></div></td><td></td><td></td></tr></tfoot></table></div></div>

{{/* The variance grid is the widest screen in the app and is the one table that
     cannot restack into cards — a head means nothing without its project, its
     budget and its actual side by side. On a phone it becomes a project
     accordion instead, driven by exactly the same .Grid.Groups data as the
     matrix above, so the two can never disagree. The open/closed truth ships
     in the markup as .is-open, which is the contract fervid-app.js's accordion
     already implements — it republishes that state through aria-expanded and
     the body. */}}
<div class="acc m-only">
  {{range .Grid.Groups}}
  <div class="acc-item is-open">
    <button class="acc-head" type="button" aria-expanded="true"><span class="chev" aria-hidden="true">›</span>
      <span class="ah-main"><b>{{.Project}}</b><small>{{len .Rows}} {{plural (len .Rows) "head" "heads"}} · {{usedText .Total.Budget .Total.Actual}} used</small></span>
      <span class="ah-amt"><b>{{money .Total.Variance}}</b><small class="muted">remaining</small></span>
    </button>
    <div class="acc-body">
      {{range .Rows}}
      <div class="head-row">
        <div class="hr-top"><span class="hr-name">{{.Head}}</span><span class="pill {{.Status}}">{{statusText .Status}}</span></div>
        <div class="hr-figs">
          <span class="hr-fig"><span class="l">Budget</span><span class="v">{{money .Budget}}</span></span>
          <span class="hr-fig"><span class="l">Actual</span><span class="v">{{money .Actual}}</span></span>
          <span class="hr-fig"><span class="l">Left</span><span class="v {{varClass .Variance}}">{{money .Variance}}</span></span>
        </div>
        <div class="hr-bar"><div class="vbar {{barClass .Status}}"><i style="--pct:{{usedPct .Budget .Actual}}"></i><span class="mark"></span></div></div>
      </div>
      {{end}}
    </div>
  </div>
  {{else}}
  <div class="empty">No heads found. <a href="/heads">Add projects and heads</a> to begin.</div>
  {{end}}
</div>

<section class="split"><div><h2>Recent Payments for {{.Month}}</h2><div class="table-wrap"><table class="t-cards"><thead><tr><th>Date</th><th>Project / Head</th><th>Amount</th><th>Payee</th></tr></thead><tbody>{{range .Payments}}<tr><td class="t-lead" data-label="Date">{{.PaidOn}}</td><td data-label="Project / Head"><a href="/payments/{{.ID}}">{{.Project}} / {{.Head}}</a></td><td class="num" data-label="Amount">{{money .Amount}}</td><td data-label="Payee">{{.VendorPayee}}</td></tr>{{else}}<tr><td colspan="4" class="empty" data-label="">No payments in this month.</td></tr>{{end}}</tbody></table></div></div>{{if .Perms.Can "month" "lock"}}<div><h2>Month Close</h2>{{if .Grid.Locked}}<p class="muted">Unlocking reopens this month for budget and payment changes.</p><form method="post" action="/months/{{.Month}}/unlock" onsubmit="return confirm('Unlock {{.Month}} and allow changes again?')"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Unlock reason<input name="reason" required></label><button>Unlock Month</button></form>{{else}}<p class="muted">{{.CloseGrid.NotPaid}} heads are unpaid, {{.CloseGrid.Unbudgeted}} have unbudgeted spend, and {{.CloseGrid.Over}} are over budget. Review before closing.</p><form method="post" action="/months/{{.Month}}/lock" onsubmit="return confirm('Lock {{.Month}}? Budgets and payments will become read-only.')"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Lock reason<input name="reason" required></label><button>Lock Month</button></form>{{end}}</div>{{end}}</section>
{{template "bottom" .}}
{{end}}

{{/* The historical payment's edit screen. Phase 3 retired free-standing
     creation, but a payment recorded before this module has no request behind
     it and stays editable (X6); this is the form that edits it, unchanged from
     the pre-Phase-3 markup. Phase 6 redraws the ledger. */}}
{{define "payment_edit_form"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Daily entry</div><h1>{{.Title}}</h1><p class="sub muted">Record the head, date, amount, reference details, and proof in one pass.</p></div><a class="btn outline" href="/grid?month={{.Month}}">Back to grid</a></section>
{{if .Locked}}<div class="locked" role="status">This payment belongs to a locked or voided period and is read-only.</div>{{end}}
<form class="panel payment-form" method="post" enctype="multipart/form-data" action="{{if .Payment.ID}}/payments/{{.Payment.ID}}/edit{{else}}/payments{{end}}">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <fieldset class="payment-section payment-core" {{if .Locked}}disabled{{end}}><legend>Payment</legend><div class="form-grid">
    <label class="field span-6">Project / Head<select name="head_id" required>{{range .Heads}}<option value="{{.ID}}" {{if eq $.Payment.HeadID .ID}}selected{{end}}>{{.Project}} / {{.Name}}</option>{{end}}</select></label>
    <label class="field span-3">Paid on<input type="date" name="paid_on" value="{{.Payment.PaidOn}}" required></label>
    <label class="field span-3">Amount<input name="amount" inputmode="decimal" value="{{if .PaymentAmount}}{{.PaymentAmount}}{{else if .Payment.Amount}}{{money .Payment.Amount}}{{end}}" placeholder="₹0.00" required><small class="hint">Use rupees. Commas and ₹ are accepted.</small></label>
  </div></fieldset>
  <fieldset class="payment-section payment-reference" {{if .Locked}}disabled{{end}}><legend>Vendor and reference</legend><div class="form-grid">
    <label class="field span-4">Vendor / Payee<input name="vendor_payee" value="{{.Payment.VendorPayee}}"></label>
    <label class="field span-3">Payment mode<select name="payment_mode"><option value="">Select</option><option value="cash" {{select .Payment.PaymentMode "cash"}}>Cash</option><option value="bank_transfer" {{select .Payment.PaymentMode "bank_transfer"}}>Bank transfer</option><option value="cheque" {{select .Payment.PaymentMode "cheque"}}>Cheque</option><option value="card" {{select .Payment.PaymentMode "card"}}>Card</option><option value="upi" {{select .Payment.PaymentMode "upi"}}>UPI</option><option value="other" {{select .Payment.PaymentMode "other"}}>Other</option></select></label>
    <label class="field span-3">Invoice / Bill No<input name="invoice_no" value="{{.Payment.InvoiceNo}}"></label>
    <label class="field span-2">Reference No<input name="reference_no" value="{{.Payment.ReferenceNo}}"></label>
  </div></fieldset>
  <fieldset class="payment-section payment-proof" {{if .Locked}}disabled{{end}}><legend>Proof and notes</legend><div class="form-grid">
    <label class="field span-5">Attachment<input type="file" name="attachment"><small class="hint">Optional bill, receipt, or payment proof.</small></label>
    <label class="field span-7">Remarks<textarea name="remarks">{{.Payment.Remarks}}</textarea></label>
  </div></fieldset>
  <div class="form-actions"><button class="primary" name="submit_action" value="save" {{if .Locked}}disabled{{end}}>Save Payment</button>{{if not .Payment.ID}}<button name="submit_action" value="add_another" {{if .Locked}}disabled{{end}}>Save and add another</button>{{end}}</div>
</form>
{{template "bottom" .}}
{{end}}

{{define "payments"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Payment ledger</div><h1>Payments</h1><p class="sub muted">{{.Month}} · {{len .Payments}} entries · active total {{money .PaymentTotal}}</p></div>{{if .Locked}}<span class="pill warn">Locked</span>{{else}}<a class="btn primary" href="/payments/new?month={{.Month}}">+ Add payment</a>{{end}}</section>
<form class="toolbar" method="get" action="/payments">
  <label>Month<input type="month" name="month" value="{{.Month}}"></label>
  <label>Status<select name="status"><option value="active" {{select .Status "active"}}>Active</option><option value="voided" {{select .Status "voided"}}>Removed / voided</option><option value="all" {{select .Status "all"}}>All</option></select></label>
  <label class="search-label">Search<input name="q" value="{{.Query}}" placeholder="Project, head, payee, invoice..."></label>
  <button>Filter</button><a class="btn outline" href="/payments">Reset</a>
</form>
<div class="table-wrap payments-table" tabindex="0" role="region" aria-label="Payments table, scrollable"><table class="t-cards"><thead><tr><th>Date</th><th>Project / Head</th><th class="num">Amount</th><th>Payee</th><th>Mode</th><th>Reference</th><th>Entered by</th><th>Status</th><th>Actions</th></tr></thead><tbody>{{range .Payments}}<tr class="{{if voided .}}is-voided{{end}}"><td class="t-lead" data-label="Date">{{.PaidOn}}</td><td data-label="Project / Head"><a href="/payments/{{.ID}}">{{.Project}} / {{.Head}}</a></td><td class="num" data-label="Amount">{{money .Amount}}</td><td data-label="Payee">{{.VendorPayee}}</td><td data-label="Mode">{{paymentMode .PaymentMode}}</td><td data-label="Reference"><span><span class="nowrap">{{.InvoiceNo}}</span>{{if .ReferenceNo}}<br><small>{{.ReferenceNo}}</small>{{end}}</span></td><td data-label="Entered by">{{.EnteredByName}}</td><td data-label="Status">{{if voided .}}<span class="pill bad">Removed</span>{{else}}<span class="pill good">Active</span>{{end}}</td><td class="actions-cell" data-label="Actions"><span><a class="btn small" href="/payments/{{.ID}}">View</a>{{if and ($.Perms.Can "payment" "edit") (not .RequestID) (not $.Locked) (not (voided .))}}<a class="btn small outline" href="/payments/{{.ID}}/edit">Edit</a>{{if $.Perms.Can "payment" "void"}}<details class="inline-danger"><summary>Remove</summary><form method="post" action="/payments/{{.ID}}/void" onsubmit="return confirm('Remove payment #{{.ID}} from actual totals?')"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="next" value="/payments?month={{$.Month}}&status={{$.Status}}&q={{urlquery $.Query}}"><label>Reason<input name="reason" required></label><button class="danger">Remove</button></form></details>{{end}}{{else if voided .}}<small class="muted">{{.VoidReason}}</small>{{else if $.Locked}}<small class="muted">Locked</small>{{end}}</span></td></tr>{{else}}<tr><td colspan="9" class="empty" data-label="">No payments match these filters. <a href="/payments/new">Record a payment</a></td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}

{{/* The recorded payment — mockups/screens/payment-detail.html.

     Two readings of one screen. A payment linked to a request is the end of
     that request's story: what was approved beside what left the bank, the
     proof, and the whole trail from submission to settlement. It offers no way
     to change any of it, because there is none — the store refuses to edit or
     void a linked payment (S12), and a control the route would reject is a lie.

     A historical, request-less payment predates the link and is still an
     editable ledger row, so it keeps the pre-Phase-3 screen verbatim in the
     {{else}} branch until Phase 6 redraws the ledger (X6). */}}
{{define "payment_detail"}}
{{template "top" .}}
{{if .Request2.ID}}
{{/* The banner reports how the reader got here, not that a payment exists:
     "Payment saved" is true once, on the redirect from the confirming POST,
     and was being shown on every visit for the life of the record — including
     after a repeat confirm the store had refused (settlement-5). */}}
{{if eq .Outcome "saved"}}
<div class="banner good">
  <span class="b-ico" aria-hidden="true">✓</span>
  <div>
    <b>Payment saved. The request is {{if eq .Request2.Status "partial_review"}}with the manager{{else}}completed{{end}}.</b>
    <p>{{.Request2.RequesterName}} and {{.Request2.ManagerName}} have been notified. This payment can no longer be edited or cancelled.</p>
  </div>
</div>
{{else if eq .Outcome "duplicate"}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">i</span>
  <div>
    <b>This request already has its payment</b>
    <p>{{.Request2.Number}} was settled by PAY-{{.Payment.ID}} for {{money .Payment.Amount}}, so nothing new was saved. A request accepts exactly one payment; any balance needs a fresh request.</p>
  </div>
</div>
{{else if eq .Outcome "immutable"}}
<div class="banner info">
  <span class="b-ico" aria-hidden="true">i</span>
  <div>
    <b>This payment cannot be edited</b>
    <p>It settled {{.Request2.Number}}, and a payment linked to a request is never amended or cancelled. Nothing was changed.</p>
  </div>
</div>
{{end}}

<div class="req-head">
  <div class="rh-top"><span class="rh-no">PAY-{{.Payment.ID}} · from {{.Request2.Number}}</span><span class="rh-amt">{{money .Payment.Amount}}</span></div>
  <h1>{{.Payment.VendorPayee}}</h1>
  <p class="rh-meta">{{.Payment.Project}} / {{.Payment.Head}} · paid {{dateLong .Payment.PaidOn}}</p>
  {{/* The same three helpers every other head uses. Waiting exists so the
       list, the queue and the detail head cannot drift into three answers, and
       a hand-written line here was already a fourth: it read a manager their
       own name where every other screen says "you". */}}
  <div class="rh-status">
    {{$p := statusPill .Request2 .User.ID}}<span class="pill {{$p.Class}}">{{$p.Text}}</span>
    {{$w := waitingOn .Request2 .User.ID}}<span class="waiting {{$w.Class}}">{{$w.Text}}</span>
  </div>
</div>

<div class="compare" style="margin-bottom:14px">
  <div class="cmp-row"><span class="l">Approved</span><span class="v">{{money (approvedOf .Request2)}}</span></div>
  <div class="cmp-row"><span class="l">Paid</span><span class="v">{{money .Payment.Amount}}</span></div>
  {{/* The balance row reads the request's status, not only the payment's
       settlement: an accepted shortfall keeps settlement="partial" for ever,
       and "still owed" over "Completed — partial accepted" was a contradiction
       (partial-1). */}}
  {{if eq .Request2.Status "completed_partial"}}
  <div class="cmp-row"><span class="l">Balance written off · shortfall accepted by {{.Request2.ManagerName}}</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>
  {{else if eq .Payment.Settlement "partial"}}
  <div class="cmp-row diff"><span class="l">Still owed to the payee</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>
  {{else}}
  <div class="cmp-row match"><span class="l">Difference · confirmed settled by Accounts</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>
  {{end}}
</div>

<div class="card">
  <div class="card-head"><h2>Payment</h2><span class="pill neutral no-dot">Read-only</span></div>
  <dl class="dl">
    <div><dt>Paid on</dt><dd>{{dateLong .Payment.PaidOn}}</dd></div>
    <div><dt>Mode</dt><dd>{{paymentMode .Payment.PaymentMode}}</dd></div>
    {{if .Payment.ReferenceNo}}<div><dt>Reference</dt><dd class="num">{{.Payment.ReferenceNo}}</dd></div>{{end}}
    <div><dt>Recorded by</dt><dd>{{.Payment.EnteredByName}} at {{date .Payment.CreatedAt}}</dd></div>
    <div><dt>Payee</dt><dd>{{if and .Request2.VendorID (.Perms.Can "vendor" "view")}}<a href="/vendors/{{deref .Request2.VendorID}}">{{.Payment.VendorPayee}}</a>{{else}}{{.Payment.VendorPayee}}{{end}}</dd></div>
    {{if .Payment.InvoiceNo}}<div><dt>Invoice</dt><dd class="num">{{.Payment.InvoiceNo}}</dd></div>{{end}}
    <div><dt>Settlement</dt><dd>{{if eq .Payment.Settlement "partial"}}Partial — a balance is still owed{{else}}Fully settled — deductions handled outside this system{{end}}</dd></div>
    {{if .Payment.PartialReason}}<div style="grid-column:1/-1"><dt>Partial reason</dt><dd>{{.Payment.PartialReason}}</dd></div>{{end}}
    {{if .Payment.Remarks}}<div style="grid-column:1/-1"><dt>Processing note</dt><dd>{{.Payment.Remarks}}</dd></div>{{end}}
  </dl>
  {{/* Said here, on every visit, now that the confirmation banner is shown
       once: the reader who arrives from the ledger months later still needs
       to know why there is no Edit (S12). */}}
  <p class="hint">This payment settled {{.Request2.Number}} and can no longer be edited or cancelled.</p>
</div>

{{if or .Attachments .RequestAtts}}
<div class="section-head"><h2>Proof</h2></div>
<div class="stack-8">
  {{range .Attachments}}
  <div class="file-row">
    <span class="f-ico" aria-hidden="true">{{fileKind .OriginalName}}</span>
    <span><b>{{.OriginalName}}</b><small>{{fileSize .SizeBytes}} · added {{date .CreatedAt}}</small></span>
    {{if $.Perms.Can "attachment" "view"}}<span class="f-actions"><a class="btn small outline" href="/attachments/{{.ID}}">Download</a></span>{{end}}
  </div>
  {{end}}
  {{/* The bill the request came in with, beside the advice for the money that
       went out. Both halves of the proof are on the screen that closes the
       story, so nobody has to open the request to check what was billed. */}}
  {{/* A request document is served by the request's own route. /attachments/{id}
       reads payment_attachments, and the two tables have unrelated autoincrement
       sequences — so this link used to 404, or worse, hand the reader an
       unrelated payment's bank advice under the request document's filename
       (F-A-05/F-B-09). */}}
  {{range .RequestAtts}}
  <div class="file-row">
    <span class="f-ico" aria-hidden="true">{{fileKind .OriginalName}}</span>
    <span><b>{{.OriginalName}}</b><small>Invoice from the request · {{fileSize .SizeBytes}}</small></span>
    {{if $.Perms.Can "attachment" "view"}}<span class="f-actions"><a class="btn small outline" href="/requests/{{$.Request2.ID}}/attachments/{{.ID}}">Download</a></span>{{end}}
  </div>
  {{end}}
</div>
{{end}}

<div class="section-head"><h2>Full trail, request to payment</h2></div>
<ol class="thread">
  {{range .Audit}}{{$a := trailAction .}}
  <li>
    <span class="tl-dot {{auditTone $a}}" aria-hidden="true">{{auditGlyph $a}}</span>
    <div class="tl-head"><b>{{.ActorName}} {{auditPhrase $a}}</b><time>{{date .CreatedAt}}</time></div>
    <div class="tl-body">{{trailBody .}}</div>
  </li>
  {{else}}<li><span class="tl-dot" aria-hidden="true">·</span><div class="tl-body muted">No history recorded.</div></li>{{end}}
</ol>

<div class="action-bar">
  <span class="ab-note d-only">Refunds and reversals are outside this version.</span>
  <span class="row-end"></span>
  {{if .Perms.Can "request" "view"}}<a class="btn outline" href="/requests/{{.Request2.ID}}">Open the request</a>{{end}}
  <a class="btn" href="/payments">Back to ledger</a>
</div>
{{else}}
{{template "payment_detail_historical" .}}
{{end}}
{{template "bottom" .}}
{{end}}

{{/* The pre-Phase-3 ledger detail, moved here unchanged. It is reachable only
     for a payment with no request behind it, which is the only kind this
     version still lets anybody edit or void. */}}
{{define "payment_detail_historical"}}
<section class="page-banner"><div><div class="eyebrow">Payment record</div><h1>Payment #{{.Payment.ID}}</h1><p class="sub muted">{{.Payment.Project}} / {{.Payment.Head}} · {{money .Payment.Amount}}</p></div>{{if voided .Payment}}<span class="pill bad">Voided</span>{{else if .Locked}}<span class="pill warn">Locked</span>{{else if .Perms.Can "payment" "edit"}}<a class="btn outline" href="/payments/{{.Payment.ID}}/edit">Edit</a>{{end}}</section>
{{if voided .Payment}}<div class="locked">Voided payment. Reason: {{.Payment.VoidReason}}</div>{{else if .Locked}}<div class="locked">This payment belongs to a locked month and is read-only.</div>{{end}}
<dl class="details detail-grid"><div><dt>Project / Head</dt><dd>{{.Payment.Project}} / {{.Payment.Head}}</dd></div><div><dt>Date</dt><dd>{{.Payment.PaidOn}}</dd></div><div><dt>Amount</dt><dd>{{money .Payment.Amount}}</dd></div><div><dt>Payee</dt><dd>{{.Payment.VendorPayee}}</dd></div><div><dt>Mode</dt><dd>{{paymentMode .Payment.PaymentMode}}</dd></div><div><dt>Invoice</dt><dd>{{.Payment.InvoiceNo}}</dd></div><div><dt>Reference</dt><dd>{{.Payment.ReferenceNo}}</dd></div><div><dt>Entered by</dt><dd>{{.Payment.EnteredByName}} at {{date .Payment.CreatedAt}}</dd></div><div><dt>Remarks</dt><dd>{{.Payment.Remarks}}</dd></div></dl>
<section class="split"><div><h2>Attachments</h2>{{if and (not .Locked) (not (voided .Payment))}}<form class="cluster" method="post" enctype="multipart/form-data" action="/payments/{{.Payment.ID}}/attachments"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="file" name="attachment" aria-label="Attachment" required><button>Upload</button></form>{{end}}<ul class="file-list">{{range .Attachments}}<li><a href="/attachments/{{.ID}}">{{.OriginalName}}</a><small>{{fileSize .SizeBytes}}</small></li>{{else}}<li class="muted">No attachments.</li>{{end}}</ul></div>{{if and (.Perms.Can "payment" "void") (not .Locked) (not (voided .Payment))}}<div class="danger-zone"><h2>Void Payment</h2><p class="muted">Voiding keeps the audit trail but excludes this payment from actual totals.</p><form method="post" action="/payments/{{.Payment.ID}}/void" onsubmit="return confirm('Void payment #{{.Payment.ID}}? This cannot be undone without a new correction.')"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Reason<input name="reason" required></label><button class="danger">Void</button></form></div>{{end}}</section>
<h2>Transaction Audit Trail</h2><ol class="timeline">{{range .Audit}}<li class="timeline-item {{actionClass .Action}}"><div class="audit-meta"><strong>{{actionText .Action}}</strong><span>{{date .CreatedAt}}</span><span>{{.ActorName}}</span></div><p>{{.Summary}}</p>{{if or (hasText .BeforeJSON) (hasText .AfterJSON)}}<details><summary>Before / after</summary>{{if hasText .BeforeJSON}}<pre>{{jsonPretty .BeforeJSON}}</pre>{{end}}{{if hasText .AfterJSON}}<pre>{{jsonPretty .AfterJSON}}</pre>{{end}}</details>{{end}}</li>{{else}}<li class="muted">No audit entries.</li>{{end}}</ol>
{{end}}

{{/* The payment entry screen — mockups/screens/payment-entry.html.

     Nothing here writes. The primary button posts to the settlement preview,
     which is pure (D8), and only the confirmation inside that sheet posts to
     /payments. formenctype on the preview button keeps that honest without
     JavaScript: the preview is URL-encoded, so it cannot receive the file
     bytes, and hx-include names the same text fields for the htmx path. Either
     way the file is transmitted exactly once, by the final multipart POST.

     The ₹ lives in the .money-field's .cur prefix, so the input carries
     amountValue and never money — money.FormatPaise already has one. */}}
{{/* One element with id lock-banner, always rendered so htmx has a target:
     payment_form draws it inline, and GET /payments/lock-status returns it on
     its own when "Paid on" changes (F-G-020). It sits inside the Payment
     fieldset's .form-grid, so the wrapper is display:contents — empty, it is no
     grid item and leaves no gap — and the banner itself spans the row. */}}
{{define "payment_lock_banner"}}<div id="lock-banner" style="display:contents">{{if .Locked}}<div class="locked" role="status" style="grid-column:1/-1;margin:0">{{.Month}} is locked, so a payment dated in it will be refused. Pick a date in an open month, or ask for {{.Month}} to be unlocked.</div>{{end}}</div>{{end}}

{{define "payment_form"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Accounts · payment entry</div>
    <h1>Record the payment</h1>
    <p class="sub">{{.Request2.Number}} · {{.Request2.Vendor}} · one request, one payment</p>
  </div>
</section>

<div class="reserve-bar">
  <span class="rb-dot" aria-hidden="true"></span>
  <b>Reserved by you</b>
  <span class="rb-meta">since {{hhmm .Request2.ProcessingAt}} · nobody else can process this request</span>
  {{if .Perms.Can "reservation" "release"}}<span class="rb-actions"><a class="btn small outline" href="/requests/{{.Request2.ID}}/reservation">Release</a></span>{{end}}
</div>

<div class="card" style="margin-bottom:14px">
  <div class="card-head"><h2>What was approved</h2><a class="small" href="/requests/{{.Request2.ID}}">Open the request →</a></div>
  <dl class="dl">
    <div><dt>Approved amount</dt><dd class="big">{{money (approvedOf .Request2)}}</dd></div>
    <div><dt>Approved by</dt><dd>{{.Request2.ApprovedByName}}{{if .Request2.ApprovedAt}} · {{datep .Request2.ApprovedAt}}{{end}}</dd></div>
    <div><dt>Payee</dt><dd>{{.Request2.Vendor}}</dd></div>
    <div><dt>Charge to</dt><dd>{{.Request2.Project}} / {{.Request2.Head}}</dd></div>
    <div style="grid-column:1/-1"><dt>Purpose</dt><dd>{{.Request2.Purpose}}</dd></div>
  </dl>
</div>

<form method="post" action="/payments" enctype="multipart/form-data">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <input type="hidden" name="request_id" value="{{.Request2.ID}}">
  <input type="hidden" name="head_id" value="{{.SelectedHeadID}}">
  <input type="hidden" name="vendor_payee" value="{{.Request2.Vendor}}">
  <input type="hidden" name="invoice_no" value="{{.Request2.InvoiceNo}}">
  <fieldset>
    <legend>Payment</legend>
    <div class="form-grid">
      <div class="field span-6">
        <label for="approved">Approved amount</label>
        <input id="approved" value="{{money (approvedOf .Request2)}}" readonly
               data-approved="{{approvedOf .Request2}}">
        <span class="hint">A payment can never exceed this. Overpayment means cancelling and raising a new request.</span>
      </div>
      <div class="field span-6 money-field">
        <label for="amount">Amount actually paid <span class="req" aria-hidden="true">*</span></label>
        <span class="money-wrap"><span class="cur" aria-hidden="true">₹</span><input id="amount" name="amount" inputmode="decimal" value="{{if .PaymentAmount}}{{.PaymentAmount}}{{else}}{{amountValue .Payment.Amount}}{{end}}" required></span>
        <span class="in-words">{{inWords .Payment.Amount}}</span>
      </div>
      <div class="field span-12">
        <div class="banner good" id="diff-banner" style="margin:0">
          <span class="b-ico" aria-hidden="true">✓</span>
          <div><b id="diff-text">Matches the approved amount exactly</b>
            <p>You will be asked whether this settles the obligation in full or leaves a balance due.</p></div>
        </div>
      </div>
      {{/* The lock, before the date rather than after the submit. The grid
           renders "Locked" in place of its Add buttons and /budgets carries a
           banner; this screen carried nothing, so an accountant filled in the
           amount, the date, the mode, the reference and the note and only
           learned the period was closed on the confirmation (F-G-020). The
           reservation survives the refusal, so nothing is lost — but nothing
           warned them either. It sits directly above "Paid on" so it is on
           screen beside the field that decides it, at every width: the field
           re-asks GET /payments/lock-status on change and this banner is
           swapped for the one the new date's month calls for. */}}
      {{template "payment_lock_banner" .}}
      <div class="field span-4 m-half"><label for="paid_on">Paid on <span class="req" aria-hidden="true">*</span></label><input id="paid_on" name="paid_on" type="date" value="{{.Payment.PaidOn}}" required
               hx-get="/payments/lock-status" hx-trigger="change" hx-target="#lock-banner" hx-swap="outerHTML"></div>
      <div class="field span-4 m-half"><label for="payment_mode">Payment mode <span class="req" aria-hidden="true">*</span></label>
        <select id="payment_mode" name="payment_mode" required>
          <option value="">Choose…</option>
          {{range paymentModes}}<option value="{{.}}" {{select . $.Payment.PaymentMode}}>{{paymentMode .}}</option>{{end}}
        </select>
      </div>
      <div class="field span-4"><label for="reference_no">Transaction / UTR reference <span class="req" aria-hidden="true">*</span></label><input id="reference_no" name="reference_no" class="num" value="{{.Payment.ReferenceNo}}" required></div>
    </div>
  </fieldset>

  <fieldset>
    <legend>Proof and notes</legend>
    <div class="form-grid">
      <div class="field span-12">
        <span class="flabel">Payment advice or proof</span>
        <label class="uploader"><div class="up-ico" aria-hidden="true">⇪</div><b>Add the bank advice</b><small>PDF, JPG or PNG up to 10 MB</small><input type="file" name="attachment" accept=".pdf,.jpg,.jpeg,.png" hidden></label>
      </div>
      <div class="field span-12">
        <label for="remarks">Processing note <span class="opt" aria-hidden="true">optional</span></label>
        <textarea id="remarks" name="remarks">{{.Payment.Remarks}}</textarea>
        <span class="hint">This system does no tax arithmetic. Record what you did so the trail explains the difference.</span>
      </div>
    </div>
  </fieldset>

  <div id="settle-mount"></div>

  {{/* The preview and the write are both gated on payment:settle, so the
       button is too. A role built with payment:create and reservation:reserve
       but no settle could take a request and then press a button whose POST
       answered 403 with nothing on screen (queue-2). */}}
  {{if not (.Perms.Can "payment" "settle")}}
  <div class="banner warn">
    <span class="b-ico" aria-hidden="true">i</span>
    <div>
      <b>You can take a request but cannot settle a payment</b>
      <p>Recording the settlement needs the payment:settle permission, which your role does not hold. Release the reservation so a colleague can record it, or ask an administrator for the permission.</p>
    </div>
  </div>
  {{end}}
  <div class="action-bar">
    <span class="ab-note d-only">Nothing is saved until you confirm on the next step.</span>
    <span class="row-end"></span>
    {{if .Perms.Can "reservation" "release"}}<a class="btn outline" href="/requests/{{.Request2.ID}}/reservation">Cancel and release</a>{{end}}
    {{if .Perms.Can "payment" "settle"}}<button class="btn primary" type="submit"
            formaction="/requests/{{.Request2.ID}}/settlement-preview" formmethod="post" formenctype="application/x-www-form-urlencoded"
            hx-post="/requests/{{.Request2.ID}}/settlement-preview"
            hx-include="#amount, #paid_on, #payment_mode, #reference_no, #remarks, [name=csrf], [name=head_id], [name=vendor_payee], [name=invoice_no]"
            hx-target="#settle-mount" hx-swap="innerHTML">Payment settled →</button>{{end}}
  </div>
</form>
{{template "bottom" .}}
{{end}}

{{/* The settlement confirmation — mockups/screens/payment-settlement-confirm.html.

     One markup source, two deliveries (D8). On the htmx path the sheet is swapped
     into #settle-mount inside the live entry form, so its own file input posts
     with the confirmation and the advice is uploaded exactly once. Without JS the
     same sheet is wrapped in a full page that re-offers the upload and carries
     every typed field forward as hidden inputs.

     Nothing on this screen has been written. The .pill.neutral.no-dot says so,
     and it is true: settlementPreview holds no transaction. */}}
{{define "settlement_sheet"}}
<div class="overlay" id="settle-sheet">
  <div class="sheet" role="dialog" aria-modal="true" aria-labelledby="settle-title">
    <div class="sh-head">
      <div><h2 id="settle-title">Confirm the payment</h2><p class="sh-sub">{{.Request2.Number}} · {{.Request2.Vendor}}</p></div>
      {{/* On the htmx path the sheet is inside the live form, so the closers
           dismiss it in place and the typed amount, mode and reference survive
           (settlement-6). Anchors, not buttons: a button here would submit the
           form it sits in, and without JavaScript the href is the way back. */}}
      <a class="sh-close" href="/payments/new?request={{.Request2.ID}}"{{if .Settlement.Fragment}} data-close="settle-sheet"{{end}} aria-label="Close">✕</a>
    </div>
    <div class="sh-body stack-12">
      {{if .Error}}<div class="banner bad" style="margin:0"><span class="b-ico" aria-hidden="true">✕</span><div><b>{{.Error}}</b><p>Nothing has been saved. Correct it and confirm again.</p></div></div>{{end}}
      <div class="compare">
        <div class="cmp-row"><span class="l">Approved</span><span class="v">{{money .Settlement.Approved}}</span></div>
        <div class="cmp-row"><span class="l">Actually paid</span><span class="v">{{money .Settlement.Paid}}</span></div>
        {{if .Settlement.Match}}
        <div class="cmp-row match"><span class="l">Difference</span><span class="v">{{money 0}}</span></div>
        {{else}}
        <div class="cmp-row diff"><span class="l">Difference</span><span class="v">{{money .Settlement.Difference}} lower ⚠</span></div>
        {{end}}
      </div>

      <div>
        <span class="flabel" style="margin-bottom:6px">{{if .Settlement.Match}}This matches the approved amount. Confirm to close the request.{{else}}You paid less than was approved. Which is it?{{end}}</span>
        <div class="choice">
          <label>
            <input type="radio" name="settlement" value="settled" {{if ne .Settlement.Settlement "partial"}}checked{{end}}>
            <span><b>Fully settled</b><small>The obligation is discharged. Deductions such as TDS or retention were handled outside this system.</small></span>
            <span class="outcome good">Completed</span>
          </label>
          <label>
            <input type="radio" name="settlement" value="partial" {{if eq .Settlement.Settlement "partial"}}checked{{end}}>
            <span><b>Partial payment</b><small>A balance is genuinely still owed to the payee.</small></span>
            <span class="outcome warn">Manager review</span>
          </label>
        </div>
      </div>

      {{/* aria-required, not required: a hidden required control makes the form
           unsubmittable in Chrome. The store re-enforces this server-side. */}}
      <div class="field" data-when="settlement:partial" {{if ne .Settlement.Settlement "partial"}}hidden{{end}}>
        <label for="partial_reason">Why only part was paid <span class="req" aria-hidden="true">*</span></label>
        <textarea id="partial_reason" name="partial_reason" aria-required="true" placeholder="{{.Request2.ManagerName}} reads this when deciding whether to close it.">{{.Settlement.PartialReason}}</textarea>
      </div>

      <div class="banner info" style="margin:0">
        <span class="b-ico" aria-hidden="true">i</span>
        <div>
          <b>Confirming saves the payment</b>
          <p>It cannot be edited or cancelled afterwards. The request accepts no further payment — any balance needs a fresh request.</p>
        </div>
      </div>
    </div>
    <div class="sh-foot">
      <a class="btn outline" href="/payments/new?request={{.Request2.ID}}"{{if .Settlement.Fragment}} data-close="settle-sheet"{{end}}>Go back</a>
      <span class="row-end"></span>
      <button class="btn primary" type="submit" formaction="/payments" formmethod="post">Confirm and save payment</button>
    </div>
  </div>
</div>
{{end}}

{{define "settlement_confirm"}}
{{template "top" .}}
<div class="reserve-bar">
  <span class="rb-dot" aria-hidden="true"></span>
  <b>Reserved by you</b>
  <span class="rb-meta">since {{hhmm .Request2.ProcessingAt}}</span>
</div>

<div class="card">
  <div class="card-head"><h2>Payment about to be saved</h2><span class="pill neutral no-dot">Not saved yet</span></div>
  <dl class="dl">
    <div><dt>Paid on</dt><dd>{{index .Settlement.Fields "paid_on"}}</dd></div>
    <div><dt>Mode</dt><dd>{{paymentMode (index .Settlement.Fields "payment_mode")}}</dd></div>
    <div><dt>Reference</dt><dd class="num">{{index .Settlement.Fields "reference_no"}}</dd></div>
    <div style="grid-column:1/-1"><dt>Processing note</dt><dd>{{index .Settlement.Fields "remarks"}}</dd></div>
  </dl>
</div>

<form method="post" action="/payments" enctype="multipart/form-data">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <input type="hidden" name="request_id" value="{{.Request2.ID}}">
  {{range $k, $v := .Settlement.Fields}}<input type="hidden" name="{{$k}}" value="{{$v}}">{{end}}
  <div class="field">
    <span class="flabel">Payment advice or proof <span class="opt" aria-hidden="true">optional</span></span>
    <label class="uploader"><div class="up-ico" aria-hidden="true">⇪</div><b>Attach the bank advice</b><small>Attach it here — it is uploaded once, when you confirm.</small><input type="file" name="attachment" accept=".pdf,.jpg,.jpeg,.png" hidden></label>
  </div>
  {{template "settlement_sheet" .}}
</form>
{{template "bottom" .}}
{{end}}

{{/* The request picker — mockups/screens/payment-request-picker.html.

     Three deliberate points. A takeable .co is a submit button inside a POST
     form, not a link, because selecting one reserves the request and a GET must
     never mutate. A taken .co.is-taken links to the read-only request instead,
     which is the only thing the loser may do. And the search degrades to a plain
     GET on /payments/new when htmx is absent, re-rendering the same list from
     the same template.

     The mockup's banner ends "An administrator can re-enable direct entry in
     Configuration if you ever need it." No such control exists, in this phase or
     any planned one, so that sentence is not shipped. */}}
{{define "payment_pick_options"}}
{{/* role="group", not "listbox". A listbox promises a set of option children,
     and these rows are neither options nor selectable: each is a <form> whose
     button claims the request, or a plain link for a reader without
     reservation:reserve. Declaring listbox here made assistive technology
     announce a control that does not exist and tripped a critical
     aria-required-children failure; "group" is what this actually is, and it
     keeps the accessible name. The keyboard handling in fervid-app.js keys off
     the .combo-list [data-id] selector, not the role, so nothing depends on
     the old value. */}}
<div class="combo-list" id="picker-list" role="group" aria-label="Approved requests">
  {{range .Linkable.Available}}
  {{/* Taking a request is reservation:reserve, exactly as it is in the queue.
       Without the grant the row is still worth reading, so it degrades to the
       same read-only .co the reservation's loser gets rather than to a button
       the route answers 403 to. */}}
  {{if $.Perms.Can "reservation" "reserve"}}
  <form method="post" action="/requests/{{.ID}}/record-payment"><input type="hidden" name="csrf" value="{{$.CSRF}}">
    <button class="co" type="submit">{{template "picker_row" .}}</button>
  </form>
  {{else}}
  <a class="co is-taken" href="/requests/{{.ID}}">{{template "picker_row" .}}</a>
  {{end}}
  {{end}}
  {{range .Linkable.Unavailable}}
  <a class="co is-taken" href="/requests/{{.ID}}">
    <span class="co-main"><b>{{.Number}} · {{.Vendor}}</b>
      <small>{{if .OnHold}}On hold — {{.HoldReason}}{{else if .ProcessingByName}}Reserved by {{.ProcessingByName}} at {{hhmm .ProcessingAt}} — you cannot take this one{{else}}{{reqStatus .Status}} — you cannot take this one{{end}}</small></span>
    <span class="co-amt">{{money (approvedOf .)}}</span>
  </a>
  {{end}}
  {{if and (not .Linkable.Available) (not .Linkable.Unavailable)}}<div class="co"><span class="co-main"><b>No approved requests match</b><small>Clear the search, or check the queue.</small></span></div>{{end}}
</div>
{{end}}

{{/* One takeable row's contents, so the button and the read-only link that may
     stand in for it cannot describe the same request differently. Dot is one
     store.Request. */}}
{{define "picker_row"}}<span class="co-main"><b>{{.Number}} · {{.Vendor}}</b>
    <small>{{if .Urgent}}<span class="pill urgent">Urgent</span> {{end}}{{.RequesterName}} · {{.Project}} / {{.Head}}{{if .NeededBy}} · needed {{dateLong .NeededBy}}{{end}}</small></span>
  <span class="co-amt">{{money (approvedOf .)}}</span>{{end}}

{{define "payment_pick_request"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Accounts · new payment</div>
    <h1>Which approved request is this for?</h1>
    <p class="sub">Every payment belongs to exactly one approved request.</p>
  </div>
  <div class="pb-actions">
    {{if .Perms.Can "payment" "process"}}<a class="btn outline" href="/accounts-queue">Open the queue</a>{{end}}
  </div>
</section>

<div class="banner info">
  <span class="b-ico" aria-hidden="true">i</span>
  <div>
    <b>Free payment entry has been removed</b>
    <p>Payments recorded before this module remain in the ledger as history. New money out starts here.</p>
  </div>
</div>

<form class="field" method="get" action="/payments/new" style="margin-bottom:4px">
  <label for="picker">Search approved requests</label>
  <span class="combo">
    <input class="combo-input" id="picker" name="q" value="{{.Query}}" autocomplete="off"
           hx-get="/payments/new/options" hx-trigger="keyup changed delay:250ms, search"
           hx-target="#picker-list" hx-swap="outerHTML">
    <span class="combo-caret" aria-hidden="true">▾</span>
  </span>
  <span class="hint">Search by request number, requester, payee, project, head or amount.</span>
</form>

{{template "payment_pick_options" .}}

<div class="section-head"><h2>Recently paid by you</h2></div>
<div class="table-wrap">
  <table class="t-cards">
    <thead><tr><th>Request</th><th>Payee</th><th class="num">Paid</th><th>Date</th><th>Status</th></tr></thead>
    <tbody>
      {{range .RecentPaid}}
      <tr>
        <td class="t-lead" data-label="Request"><a href="/payments/{{.PaymentID}}">{{.Number}}</a></td>
        <td data-label="Payee">{{.Payee}}</td>
        <td class="num" data-label="Paid">{{money .Amount}}</td>
        <td data-label="Date">{{dateLong .PaidOn}}</td>
        <td data-label="Status">{{if eq .Status "partial_review"}}<span class="pill partial">Partial — manager review</span>{{else if eq .Status "completed_partial"}}<span class="pill completed-partial">Completed — partial accepted</span>{{else}}<span class="pill completed">Completed</span>{{end}}</td>
      </tr>
      {{else}}<tr><td colspan="5" class="empty">You have not recorded a payment yet.</td></tr>{{end}}
    </tbody>
  </table>
</div>
{{template "bottom" .}}
{{end}}

{{/* The Accounts work queue — mockups/screens/accounts-queue.html.

     Two departures from the mockup, both deliberate. Its "Take for processing"
     is a link; reserving is a mutation, so here it is a submit button inside a
     POST form. And its .page-banner carries .d-only, which would leave a phone
     with no visible h1 at all — the banner is unconditional and the heading
     travels with it.

     .metric.warn has no rule in the design system, so the "Reserved by you"
     tile is a plain .metric until Phase 0 grows one. */}}
{{define "accounts_queue"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Accounts</div>
    <h1>Payment queue</h1>
    <p class="sub">{{.Linkable.Counts.Approved}} approved and unclaimed · {{money .Linkable.Counts.ApprovedAmount}} · one request, one payment</p>
  </div>
  <div class="pb-actions">
    {{if .Perms.Can "payment" "create"}}<a class="btn primary" href="/payments/new">＋ Record a payment</a>{{end}}
  </div>
</section>

<div class="metric-strip">
  <div class="metric"><span class="metric-label">Approved, unclaimed</span><span class="metric-value">{{.Linkable.Counts.Approved}}</span><span class="metric-foot">{{money .Linkable.Counts.ApprovedAmount}}</span></div>
  <div class="metric"><span class="metric-label">Reserved by you</span><span class="metric-value">{{.Linkable.Counts.ReservedByMe}}</span><span class="metric-foot">{{if .Linkable.Counts.StaleReservations}}{{.Linkable.Counts.StaleReservations}} open over a day{{else}}All recent{{end}}</span></div>
  <div class="metric"><span class="metric-label">Reserved by others</span><span class="metric-value">{{.Linkable.Counts.ReservedByOthers}}</span><span class="metric-foot">Visible, not yours</span></div>
  <div class="metric"><span class="metric-label">On hold</span><span class="metric-value">{{.Linkable.Counts.Hold}}</span><span class="metric-foot">Waiting on the requester</span></div>
</div>

<div class="segmented" style="margin-bottom:12px">
  {{$counts := .Linkable.Counts}}
  <a class="{{if eq .Tab "approved"}}is-active{{end}}" {{if eq .Tab "approved"}}aria-current="page"{{end}} href="/accounts-queue?tab=approved">Approved, unclaimed <span class="n">{{$counts.Approved}}</span></a>
  <a class="{{if eq .Tab "processing"}}is-active{{end}}" {{if eq .Tab "processing"}}aria-current="page"{{end}} href="/accounts-queue?tab=processing">Processing <span class="n">{{$counts.Processing}}</span></a>
  <a class="{{if eq .Tab "hold"}}is-active{{end}}" {{if eq .Tab "hold"}}aria-current="page"{{end}} href="/accounts-queue?tab=hold">On hold <span class="n">{{$counts.Hold}}</span></a>
  <a class="{{if eq .Tab "partial_review"}}is-active{{end}}" {{if eq .Tab "partial_review"}}aria-current="page"{{end}} href="/accounts-queue?tab=partial_review">Partial review <span class="n">{{$counts.PartialReview}}</span></a>
  <a class="{{if eq .Tab "paid"}}is-active{{end}}" {{if eq .Tab "paid"}}aria-current="page"{{end}} href="/accounts-queue?tab=paid">Paid <span class="n">{{$counts.Paid}}</span></a>
</div>

{{/* Both renderings, as requests, approvals, vendors and recoverables all ship.
     The queue used to ship the mobile form alone, and .m-filters is display:none
     above 860 px — so the one screen whose job is finding the request that matches
     the invoice in your hand had no search box on a laptop at all, while the
     server-side search worked perfectly (F-D-07). #queue-q stays on the desktop
     twin, which is the one a wide viewport can actually use. */}}
<form class="toolbar" method="get" action="/accounts-queue">
  <input type="hidden" name="tab" value="{{.Tab}}">
  <div class="field search"><label for="queue-q">Search</label><input id="queue-q" name="q" value="{{.Query}}" placeholder="Number, payee, project, head or amount…"></div>
  <span class="row-end"></span>
  <button class="btn">Search</button>
</form>

<form class="m-filters" method="get" action="/accounts-queue">
  <input type="hidden" name="tab" value="{{.Tab}}">
  <span class="m-search"><input name="q" value="{{.Query}}" placeholder="Number, payee, project…" aria-label="Search the payment queue"></span>
  <button class="btn filter-btn" type="submit">Search</button>
</form>

{{if .Linkable.Counts.StaleReservations}}
<div class="banner brand">
  <span class="b-ico" aria-hidden="true">◷</span>
  <div>
    <b>You have {{.Linkable.Counts.ReservedByMe}} {{plural .Linkable.Counts.ReservedByMe "request" "requests"}} reserved</b>
    <p>{{.Linkable.Counts.StaleReservations}} of them {{plural .Linkable.Counts.StaleReservations "has" "have"}} been open for more than a day. Finish {{plural .Linkable.Counts.StaleReservations "it" "them"}} or release {{plural .Linkable.Counts.StaleReservations "it" "them"}} so someone else can.</p>
  </div>
  <span class="b-actions"><a class="btn small outline" href="/accounts-queue?tab=processing">Review</a></span>
</div>
{{end}}

<div class="table-wrap">
  <table class="t-cards">
    <thead><tr><th>Request</th><th>Payee</th><th>Project / head</th><th class="num">Amount</th><th>Needed by</th><th>Status</th><th class="c">Action</th></tr></thead>
    <tbody>
      {{range .Linkable.Available}}
      <tr>
        <td class="t-lead" data-label="Request"><a href="/requests/{{.ID}}">{{.Number}}</a> <span class="t-sub">{{.RequesterName}} · approved {{datep .ApprovedAt}}</span></td>
        <td data-label="Payee">{{.Vendor}}</td>
        <td data-label="Project / head">{{if eq .Treatment "recoverable"}}<span class="pill recoverable">Recoverable</span>{{else}}{{.Project}} / {{.Head}}{{end}}</td>
        <td class="num" data-label="Amount">{{money (approvedOf .)}}</td>
        <td data-label="Needed by">{{if .NeededBy}}{{dateLong .NeededBy}}{{else}}—{{end}}</td>
        <td data-label="Status">{{if .Urgent}}<span class="pill urgent">Urgent</span> {{end}}<span class="pill approved">Approved</span></td>
        <td class="c" data-label="Action">{{if $.Perms.Can "reservation" "reserve"}}<form method="post" action="/requests/{{.ID}}/record-payment"><input type="hidden" name="csrf" value="{{$.CSRF}}"><button class="btn small primary" type="submit">Take for processing</button></form>{{else}}<a class="btn small outline" href="/requests/{{.ID}}">View</a>{{end}}</td>
      </tr>
      {{end}}
      {{range .Linkable.Unavailable}}
      <tr>
        <td class="t-lead" data-label="Request"><a href="/requests/{{.ID}}">{{.Number}}</a> <span class="t-sub">{{.RequesterName}} · approved {{datep .ApprovedAt}}</span></td>
        <td data-label="Payee">{{.Vendor}}</td>
        <td data-label="Project / head">{{if eq .Treatment "recoverable"}}<span class="pill recoverable">Recoverable</span>{{else}}{{.Project}} / {{.Head}}{{end}}</td>
        <td class="num" data-label="Amount">{{money (approvedOf .)}}</td>
        <td data-label="Needed by">{{if .NeededBy}}{{dateLong .NeededBy}}{{else}}—{{end}}</td>
        <td data-label="Status">
          {{if and .Urgent (ne .Status "completed") (ne .Status "completed_partial")}}<span class="pill urgent">Urgent</span> {{end}}
          {{if .OnHold}}<span class="pill hold">On hold</span>
          {{else if eq .Status "partial_review"}}{{$p := statusPill . $.User.ID}}<span class="pill {{$p.Class}}">{{$p.Text}}</span>
          {{else if eq .Status "completed_partial"}}<span class="pill completed-partial">Completed — partial accepted</span>
          {{else if eq .Status "completed"}}<span class="pill completed">Completed</span>
          {{else if and .ProcessingBy (eq (deref .ProcessingBy) $.User.ID)}}<span class="pill processing">Reserved by you · {{reservedLabel .ProcessingAt}}</span>
          {{else}}<span class="pill processing">Reserved by {{.ProcessingByName}}</span>{{end}}
        </td>
        <td class="c" data-label="Action">
          {{if .OnHold}}<a class="btn small" href="/requests/{{.ID}}">Read reply</a>
          {{else if eq .Status "partial_review"}}<a class="btn small outline" href="/requests/{{.ID}}/partial-review">View</a>
          {{/* Past the one-day mark the mockup's own Resume goes to
               accounts-stale-processing.html, not back to the form: the nudge
               screen is the only place that names what may be done about a
               reservation this old, and this row is the only thing that knows
               which reservation it is. The count in the banner above is an
               aggregate and cannot. */}}
          {{/* Resume only while the request is still in flight. processing_by
               survives settlement, so on the Paid tab this offered every row the
               reader had settled themselves a link back to the payment form —
               which answers a conflict, because the request is no longer
               approved. Once it is paid, the useful destination is the request. */}}
          {{else if and .ProcessingBy (eq (deref .ProcessingBy) $.User.ID) (eq .Status "processing")}}<a class="btn small" href="{{if stale .ProcessingAt}}/requests/{{.ID}}/reservation/stale{{else}}/payments/new?request={{.ID}}{{end}}">Resume</a>
          {{else if and .ProcessingBy ($.Perms.Can "reservation" "reassign")}}<a class="btn small outline" href="/requests/{{.ID}}/reservation">Reassign</a>
          {{else}}<a class="btn small outline" href="/requests/{{.ID}}">View</a>{{end}}
        </td>
      </tr>
      {{end}}
      {{if and (not .Linkable.Available) (not .Linkable.Unavailable)}}<tr><td colspan="7" class="empty">Nothing in this tab right now.</td></tr>{{end}}
    </tbody>
  </table>
</div>
{{template "bottom" .}}
{{end}}

{{/* G15 — a refused reservation is a screen, not an error page.
     mockups/screens/accounts-reservation-conflict.html: say what happened, say in
     as many words that nothing was saved, show the request in context, and offer
     the things the reader may actually do next.

     Three refusals, three screens. ReserveRequest returns a different sentinel for
     each — already taken, on hold, not approved — and this screen used to report
     all three as "Someone else took this request before you", on a request whose
     status was approved and whose processing_by was NULL. The accountant's natural
     next step was to go and ask a colleague who did not exist, while the real
     answer — the requester owes us an answer — was one click away on the request
     (F-D-02). The picker already got the hold case right in the same codebase;
     only the reserve path discarded it.

     The mockup lists "Ask for it to be reassigned" unconditionally with the
     note "Needs reassign permission". Offering a control the reader cannot use
     leaks it, so the entry is gated and the note moves into the sub-line. */}}
{{define "reservation_conflict"}}
{{template "top" .}}
{{if eq .ConflictCause "hold"}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">⏸</span>
  <div>
    <b>{{.Request2.Number}} is on hold, so it cannot be taken for processing</b>
    {{if .Request2.HoldReason}}<p>“{{.Request2.HoldReason}}”</p>{{end}}
    <p>Nobody holds a reservation on it. Payment is blocked until Accounts lifts the hold, and the requester is the one being waited on. Nothing you typed has been saved, and no payment was created.</p>
  </div>
</div>
{{else if eq .ConflictCause "not-approved"}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">✕</span>
  <div>
    <b>{{.Request2.Number}} is not available to process</b>
    <p>Only an approved request can be taken for processing, and this one is {{reqStatus .Request2.Status}}. Nobody holds a reservation on it. Nothing you typed has been saved, and no payment was created.</p>
  </div>
</div>
{{else if eq .ConflictCause "unclaimed"}}
{{/* Nothing was refused: the reader opened the entry screen for a request
     nobody holds — after releasing it, or by URL. It used to be reported as a
     lost race to "Someone else" (settlement-2). The form needs a reservation
     first, and taking one is offered right here. */}}
<div class="banner info">
  <span class="b-ico" aria-hidden="true">i</span>
  <div>
    <b>Nobody holds {{.Request2.Number}} — it is approved and unclaimed</b>
    <p>The payment form opens only for the person who has taken the request for processing, and right now that is nobody. Take it below and the form opens; nothing has been saved and no payment was created.</p>
  </div>
</div>
{{else}}
<div class="banner bad">
  <span class="b-ico" aria-hidden="true">✕</span>
  <div>
    <b>{{.Holder}} took this request before you</b>
    <p>{{.Request2.Number}} is now reserved by {{.Holder}}. Nothing you typed has been saved, and no payment was created.</p>
  </div>
</div>
{{end}}

<div class="req-head">
  <div class="rh-top"><span class="rh-no">{{.Request2.Number}}</span><span class="rh-amt">{{money (approvedOf .Request2)}}</span></div>
  <h1>{{.Request2.Vendor}}{{if .Request2.ShortTitle}} — {{.Request2.ShortTitle}}{{end}}</h1>
  <p class="rh-meta">{{.Request2.Project}} / {{.Request2.Head}}{{if .Request2.ApprovedAt}} · approved {{datep .Request2.ApprovedAt}}{{end}}</p>
  <div class="rh-status">
    {{/* The pill tells the same truth the banner does. A .pill.processing naming a
         holder is a lie on a request nobody holds. */}}
    {{if eq .ConflictCause "hold"}}<span class="pill hold">On hold</span>
    {{else if or (eq .ConflictCause "not-approved") (eq .ConflictCause "unclaimed")}}<span class="pill {{pillClass .Request2.Status}}">{{reqStatus .Request2.Status}}</span>
    {{else}}<span class="pill processing">Processing — {{.Holder}}</span>{{end}}
    {{if eq .ConflictCause "taken"}}<span class="waiting">Reserved {{datep .Request2.ProcessingAt}}</span>
    {{else}}{{$w := waitingOn .Request2 .User.ID}}<span class="waiting {{$w.Class}}">{{$w.Text}}</span>{{end}}
  </div>
</div>

<div class="card">
  <div class="card-head"><h2>What you can do</h2></div>
  <div class="a-list">
    <a href="/accounts-queue"><span class="al-main"><b>Go back to the queue</b><small>Other approved requests are unclaimed</small></span><span class="al-amt" aria-hidden="true">→</span></a>
    {{if and (eq .ConflictCause "taken") (.Perms.Can "reservation" "reassign")}}<a href="/requests/{{.Request2.ID}}/reservation"><span class="al-main"><b>Ask for it to be reassigned</b><small>{{.Holder}} is told and must confirm no payment was started</small></span><span class="al-amt" aria-hidden="true">→</span></a>{{end}}
    {{if eq .ConflictCause "hold"}}<a href="/accounts-queue?tab=hold"><span class="al-main"><b>See what is on hold</b><small>The hold tab lists every request waiting on a reply</small></span><span class="al-amt" aria-hidden="true">→</span></a>{{end}}
    <a href="/requests/{{.Request2.ID}}"><span class="al-main"><b>Open the request read-only</b><small>{{if eq .ConflictCause "hold"}}The question Accounts asked, and the requester's reply when it comes{{else}}You can see it and comment, but not pay it{{end}}</small></span><span class="al-amt" aria-hidden="true">→</span></a>
  </div>
</div>

<div class="action-bar">
  <span class="row-end"></span>
  <a class="btn outline" href="/payments/new">Pick another request</a>
  {{/* A POST, exactly as the queue's button is: reserving is a mutation. The
       same route, so the concurrency guarantee is the same one (S1/S2). */}}
  {{if and (eq .ConflictCause "unclaimed") (.Perms.Can "reservation" "reserve")}}
  <a class="btn outline" href="/accounts-queue">Back to queue</a>
  <form method="post" action="/requests/{{.Request2.ID}}/record-payment"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="btn primary" type="submit">Take it for processing</button></form>
  {{else}}
  <a class="btn primary" href="/accounts-queue">Back to queue</a>
  {{end}}
</div>
{{template "bottom" .}}
{{end}}

{{/* The manager's partial review — mockups/screens/payment-partial-review.html.

     This is the one settlement Accounts cannot close on its own, so the screen
     puts the whole question on one page: what was approved, what left the bank,
     what is still owed, why Accounts thinks that is legitimate, and the payment
     itself — read-only, because a recorded payment is never amended (S12).

     Two decisions, each in its own .overlay > .sheet. Both are permanent in
     different directions: accepting writes the balance off for good (G14), and
     raising a concern is a line in the trail rather than a chat message. An
     inline disclosure would offer either one with a shrug.

     The mockup's data-for / data-not-for persona attributes are prototype
     scaffolding. The action bar is chosen here, server-side, on the very
     permission the decision routes are gated by — never hidden in the browser,
     which hides nothing from a hand-rolled POST. */}}
{{define "partial_review"}}
{{template "top" .}}
<div class="req-head">
  <div class="rh-top"><span class="rh-no">{{.Request2.Number}}</span><span class="rh-amt">{{money (approvedOf .Request2)}}</span></div>
  <h1>{{.Request2.Vendor}} — {{.Request2.ShortTitle}}</h1>
  <p class="rh-meta">Raised by {{.Request2.RequesterName}}{{if .Request2.Project}} · {{.Request2.Project}}{{if .Request2.Head}} / {{.Request2.Head}}{{end}}{{end}}{{if .Request2.ApprovedAt}} · approved {{datep .Request2.ApprovedAt}}{{end}}</p>
  {{/* The same status pill and waiting line every other screen uses, so the
       queue, the request and this page cannot give three answers to "whose
       move is it". */}}
  <div class="rh-status">
    {{$p := statusPill .Request2 .User.ID}}<span class="pill {{$p.Class}}">{{$p.Text}}</span>
    {{$w := waitingOn .Request2 .User.ID}}<span class="waiting {{$w.Class}}">{{$w.Text}}</span>
  </div>
</div>

<div class="compare" style="margin-bottom:14px">
  <div class="cmp-row"><span class="l">Approved</span><span class="v">{{money (approvedOf .Request2)}}</span></div>
  <div class="cmp-row"><span class="l">Paid on {{dateLong .Payment.PaidOn}}</span><span class="v">{{money .Payment.Amount}}</span></div>
  {{/* "payee", as the request and the payment screens say: a reimbursement
       is owed to the employee, not to a vendor (partial-1). */}}
  <div class="cmp-row diff"><span class="l">Still owed to the payee</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>
</div>

<div class="banner warn">
  <span class="b-ico" aria-hidden="true">i</span>
  <div>
    <b>{{.Payment.EnteredByName}} marked this a genuine partial payment</b>
    <p>“{{.Payment.PartialReason}}”</p>
  </div>
</div>

{{/* An open concern is displayed as "Partial — under discussion" and the
     accountant who recorded the shortfall owes the answer (settlement-8). The
     status is untouched; this banner and the pill are what change. */}}
{{if .Request2.ConcernOpen}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">?</span>
  <div>
    <b>{{.Request2.ManagerName}} raised a concern about this shortfall</b>
    <p>{{if and .Request2.ProcessingBy (eq (deref .Request2.ProcessingBy) .User.ID)}}Answer it in the conversation below; the review returns to {{.Request2.ManagerName}} once you do.{{else}}{{.Payment.EnteredByName}} is expected to answer it in the conversation; {{.Request2.ManagerName}} then decides.{{end}} The recorded payment cannot be changed either way.</p>
  </div>
</div>
{{end}}

<div class="card">
  <div class="card-head"><h2>The payment that was recorded</h2><span class="pill neutral no-dot">Cannot be edited</span></div>
  <dl class="dl">
    <div><dt>Amount paid</dt><dd class="big">{{money .Payment.Amount}}</dd></div>
    <div><dt>Paid on</dt><dd>{{dateLong .Payment.PaidOn}}</dd></div>
    <div><dt>Mode</dt><dd>{{paymentMode .Payment.PaymentMode}}</dd></div>
    {{if .Payment.ReferenceNo}}<div><dt>Reference</dt><dd class="num">{{.Payment.ReferenceNo}}</dd></div>{{end}}
    <div><dt>Recorded by</dt><dd>{{.Payment.EnteredByName}} at {{date .Payment.CreatedAt}}</dd></div>
    {{if and .Attachments (.Perms.Can "attachment" "view")}}<div><dt>Proof</dt><dd>{{range .Attachments}}<a href="/attachments/{{.ID}}">{{.OriginalName}}</a> {{end}}</dd></div>{{end}}
  </dl>
</div>

{{/* One stream, both entities: the request's own history, the payment's and the
     words people wrote, interleaved oldest-first by the handler — two blocks
     would sink every comment below every event whatever time it was written. */}}
<div class="section-head"><h2>History and conversation</h2></div>
<ol class="thread">
  {{range .Trail}}
  {{if .Comment}}
  <li class="is-comment{{if eq .ActorID $.User.ID}} is-me{{end}}">
    <span class="tl-dot" aria-hidden="true">{{initials .ActorName}}</span>
    <div class="tl-head"><b>{{.ActorName}}</b><time>{{date .CreatedAt}}</time></div>
    <div class="tl-body"><p>{{.Body}}</p></div>
  </li>
  {{else}}
  <li>
    <span class="tl-dot {{auditTone .Action}}" aria-hidden="true">{{auditGlyph .Action}}</span>
    <div class="tl-head"><b>{{.ActorName}} {{auditPhrase .Action}}</b><time>{{date .CreatedAt}}</time></div>
    <div class="tl-body">{{.Body}}</div>
  </li>
  {{end}}
  {{end}}
</ol>

{{if .Perms.Can "request" "comment"}}
<form class="comment-box" method="post" action="/requests/{{.Request2.ID}}/comment">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  {{/* A reply asked mid-decision comes back to the decision. */}}
  <input type="hidden" name="return_to" value="partial-review">
  {{/* The prompt follows the seat, not the permission: "before you decide" is
       the manager's sentence, and the requester holding request:comment was
       being read it too (partial-1). The holder is asked to answer the open
       concern; everybody else simply comments. */}}
  {{if eq .User.ID .Request2.ManagerID}}
  <label for="cmt" class="flabel">Reply to Accounts</label>
  <textarea id="cmt" name="body" placeholder="Ask {{.Payment.EnteredByName}} something before you decide." required></textarea>
  {{else if and .Request2.ConcernOpen .Request2.ProcessingBy (eq (deref .Request2.ProcessingBy) .User.ID)}}
  <label for="cmt" class="flabel">Answer {{.Request2.ManagerName}}’s concern</label>
  <textarea id="cmt" name="body" placeholder="Your reply is recorded on the thread and the review returns to {{.Request2.ManagerName}}." required></textarea>
  {{else}}
  <label for="cmt" class="flabel">Add a comment</label>
  <textarea id="cmt" name="body" placeholder="Recorded on the thread. {{.Request2.ManagerName}} decides this one." required></textarea>
  {{end}}
  <div class="cb-actions"><span class="row-end"></span><button class="btn primary small" type="submit">Post comment</button></div>
</form>
{{end}}

{{/* Two questions, not one: may this person accept a shortfall at all, and is
     this shortfall theirs to accept. The request was routed to one manager and
     the store refuses everybody else, so the bar offers the decision to exactly
     the person who can carry it out. */}}
{{if and (eq .User.ID .Request2.ManagerID) (.Perms.Can "approval" "accept_partial")}}
<div class="action-bar">
  <span class="ab-note d-only">No further payment can be attached either way.</span>
  <span class="row-end"></span>
  <button class="btn outline" type="button" data-open="concern-sheet">Raise a concern</button>
  {{/* The mockup's green .btn.approve has no rule in fervid-ds.css, so the
       accept is a plain .btn.primary like every other button in this file. */}}
  <button class="btn primary" type="button" data-open="close-sheet">Accept and close</button>
</div>

<div class="overlay" id="close-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/accept-partial">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="sh-head"><div><h2>Accept {{money .Payment.Amount}} and close?</h2><p class="sh-sub">{{.Request2.Number}} · {{money (sub (approvedOf .Request2) .Payment.Amount)}} will never be paid against this request</p></div><button class="sh-close" type="button" data-close="close-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="compare">
        <div class="cmp-row"><span class="l">Approved</span><span class="v">{{money (approvedOf .Request2)}}</span></div>
        <div class="cmp-row"><span class="l">Paid and accepted</span><span class="v">{{money .Payment.Amount}}</span></div>
        <div class="cmp-row diff"><span class="l">Written off from this request</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>
      </div>
      <p class="hint" style="margin:0">The request closes as <b>Completed — partial accepted</b>. If the balance is still due later, {{.Request2.RequesterName}} raises a new request for {{money (sub (approvedOf .Request2) .Payment.Amount)}}.</p>
      <div class="field"><label for="cl-note">Note <span class="opt" aria-hidden="true">optional</span></label><textarea id="cl-note" name="note" placeholder="Recorded in the history and visible to everyone."></textarea></div>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="close-sheet">Back</button><span class="row-end"></span><button class="btn primary" type="submit">Accept and close</button></div>
  </form>
</div>

<div class="overlay" id="concern-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/raise-concern">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="sh-head"><div><h2>Raise a concern</h2><p class="sh-sub">The request stays open as Partial — under discussion until {{.Payment.EnteredByName}} answers; your objection is recorded on the thread</p></div><button class="sh-close" type="button" data-close="concern-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="field"><label for="cn-reason">What is wrong <span class="req" aria-hidden="true">*</span></label><textarea id="cn-reason" name="comment" required placeholder="Accounts can reply in the conversation, but the recorded payment cannot be changed."></textarea></div>
      <div class="banner warn" style="margin:0"><span class="b-ico" aria-hidden="true">i</span><div><b>This does not reverse anything</b><p>The {{money .Payment.Amount}} has left the bank. Raising a concern keeps the request open so the two of you can agree what happens next.</p></div></div>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="concern-sheet">Back</button><span class="row-end"></span><button class="btn primary" type="submit">Raise concern</button></div>
  </form>
</div>
{{else}}
<div class="action-bar">
  {{/* Only promise the reply the box above actually offers: a reader holding
       request:view and nothing else cannot comment, and should not be told
       otherwise on a page that renders no comment box. */}}
  <span class="ab-note d-only">{{.Request2.ManagerName}} decides{{if .Perms.Can "request" "comment"}}. You can still comment.{{else}} this one.{{end}}</span>
  <span class="row-end"></span>
  <a class="btn outline" href="/requests/{{.Request2.ID}}">Back</a>
</div>
{{end}}
{{template "bottom" .}}
{{end}}

{{/* Release or reassign a reservation — mockups/screens/accounts-release-reassign.html.

     One screen, one .choice, two POSTs. The radio picks which formaction the
     submit carries, and the same [data-when] contract that reveals the target
     select swaps the buttons underneath it. None of that is a rule: both
     handlers re-validate everything the reveal implies, so a hand-rolled POST
     can no more reassign without a target than release without a reason.

     Three departures from the mockup. Its .page-banner carries .d-only, which
     would leave a phone with no visible h1 at all, so the banner is
     unconditional here exactly as it is on the queue. Its second choice is
     offered to everyone with the note "Needs reassign permission"; a control the
     reader cannot use is gated off instead, the rule the conflict screen already
     applies. And its history reads newest-first, while every other .thread in
     the product tells the story forwards — one product, one direction. */}}
{{define "reservation_form"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Accounts · reservation</div>
    <h1>Release or reassign this request</h1>
    <p class="sub">{{.Request2.Number}} · {{.Request2.Vendor}} · {{money (approvedOf .Request2)}}</p>
  </div>
</section>

<div class="reserve-bar">
  <span class="rb-dot" aria-hidden="true"></span>
  <b>{{if .ReserveMine}}Reserved by you{{else}}Reserved by {{.Request2.ProcessingByName}}{{end}}</b>
  <span class="rb-meta">since {{hhmm .Request2.ProcessingAt}} · {{since .Request2.ProcessingAt}}</span>
</div>

<div class="banner bad">
  <span class="b-ico" aria-hidden="true">!</span>
  <div>
    <b>Confirm no payment has been started</b>
    <p>Releasing puts the request back in the open queue and anyone in Accounts can take it. If you have already initiated a transfer in the bank portal, do not release — finish recording it.</p>
  </div>
</div>

{{/* The form's own action is the branch the reader may actually take, so
     implicit submission never posts a verb that answers 403. The screen is
     reachable on either grant, and the two are independent cells of the
     matrix. */}}
<form id="reservation-form" method="post" action="/requests/{{.Request2.ID}}/{{if .Perms.Can "reservation" "release"}}release{{else}}reassign{{end}}">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <fieldset>
    <legend>What do you want to do</legend>
    <div class="choice">
      {{if .Perms.Can "reservation" "release"}}
      <label>
        <input type="radio" name="action" value="release" checked>
        <span><b>Release it</b><small>Back to Approved — awaiting payment. Anyone in Accounts can pick it up.</small></span>
      </label>
      {{end}}
      {{if .Perms.Can "reservation" "reassign"}}
      <label>
        <input type="radio" name="action" value="reassign"{{if not (.Perms.Can "reservation" "release")}} checked{{end}}>
        <span><b>Reassign to someone else</b><small>Stays in Processing, assigned to the person you choose.</small></span>
      </label>
      {{end}}
    </div>

    <div class="form-grid" style="margin-top:13px">
      {{if .Perms.Can "reservation" "reassign"}}
      {{/* hidden only while there is another branch to be on. A reader who can
           only reassign has the reassign radio checked, so the reveal would
           unhide it the moment the script boots — and without the script they
           would face a form whose only field was invisible. */}}
      <div class="field span-6" data-when="action:reassign"{{if .Perms.Can "reservation" "release"}} hidden{{end}}>
        <label for="to_user_id">Reassign to <span class="req" aria-hidden="true">*</span></label>
        {{/* aria-required, not required: a required control inside a hidden
             field makes the whole form unsubmittable in Chrome. requestReassign
             is the enforcement, and it refuses an empty target outright. */}}
        <select id="to_user_id" name="to_user_id" aria-required="true">
          <option value="">Choose…</option>
          {{range .Users}}<option value="{{.ID}}">{{.Name}}</option>{{end}}
        </select>
      </div>
      {{end}}
      <div class="field span-12">
        <label for="reason">Reason <span class="req" aria-hidden="true">*</span></label>
        <textarea id="reason" name="reason" required placeholder="Recorded in the history and visible to everyone who can see this request."></textarea>
      </div>
      <div class="field span-12">
        <label class="checkline"><input type="checkbox" name="confirm" value="on" required> I confirm no payment has been initiated for this request</label>
      </div>
    </div>
  </fieldset>

  <div class="action-bar">
    <span class="ab-note d-only">The requester and the approver are both notified.</span>
    <span class="row-end"></span>
    {{if and .ReserveMine (.Perms.Can "payment" "create")}}<a class="btn outline" href="/payments/new?request={{.Request2.ID}}">Keep working on it</a>{{else}}<a class="btn outline" href="/accounts-queue">Back to the queue</a>{{end}}
    {{/* Release is first in the DOM and last on the screen. HTML makes the
         first submit button in tree order the form's default button, and
         hidden — unlike disabled — does not exempt it: with Reassign first,
         pressing Enter on the checked "Release it" radio submitted through the
         hidden button, posted to /reassign with no target, and lost the typed
         reason on a 400. .action-bar is a flex row, so order: 2 puts the
         danger button back on the right where the mockup has it. */}}
    {{if .Perms.Can "reservation" "release"}}<button class="btn danger" type="submit" data-when="action:release" style="order:2" formaction="/requests/{{.Request2.ID}}/release">Release reservation</button>{{end}}
    {{if .Perms.Can "reservation" "reassign"}}<button class="btn outline" type="submit" data-when="action:reassign"{{if .Perms.Can "reservation" "release"}} hidden{{end}} formaction="/requests/{{.Request2.ID}}/reassign">Reassign</button>{{end}}
  </div>
</form>

<div class="section-head"><h2>Reservation history</h2></div>
<ol class="thread">
  {{range .Audit}}
  <li>
    <span class="tl-dot {{auditTone .Action}}" aria-hidden="true">{{auditGlyph .Action}}</span>
    <div class="tl-head"><b>{{.ActorName}} {{auditPhrase .Action}}</b><time>{{date .CreatedAt}}</time></div>
    <div class="tl-body">{{trailBody .}}</div>
  </li>
  {{else}}
  <li><div class="tl-body muted">No reservation history yet.</div></li>
  {{end}}
</ol>
{{template "bottom" .}}
{{end}}

{{/* Q6 — the 26-hour nudge, mockups/screens/accounts-stale-processing.html.

     Nothing on this screen releases anything, and nothing about it is
     automatic. An automatic release would let a second accountant pay an
     invoice that is already moving through a bank portal, so the reminder
     nudges and a person decides — which is why the whole page is one .a-list of
     four choices and no default.

     The mockup's data-for="admin" bar is server-side gating here, as
     everywhere: "Carry on" is the holder's, "Release it" needs the holder plus
     reservation:release, "Hand it to a colleague" needs reservation:reassign
     and "Put it on hold" needs payment:hold. A choice the reader could not take
     is not shown greyed out — it is not shown. */}}
{{define "reservation_stale"}}
{{template "top" .}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">◷</span>
  <div>
    <b>This has been reserved by {{if .ReserveMine}}you{{else}}{{.Request2.ProcessingByName}}{{end}} for {{since .Request2.ProcessingAt}}</b>
    {{/* The threshold, not "the one-day mark": reminder_stale_days is
         admin-configurable and an admin who sets it to 3 made this sentence
         false (F-F-03). Elapsed days, not calendar days, for the reason above. */}}
    <p>A reminder goes out once a reservation has been open {{.Reminders.StaleAfterDays}} {{plural .Reminders.StaleAfterDays "day" "days"}}. Nothing is released automatically — a transfer may
      already be under way, so only {{if .ReserveMine}}you{{else}}{{.Request2.ProcessingByName}}{{end}} or an authorised colleague can act.</p>
  </div>
</div>

<div class="req-head">
  <div class="rh-top"><span class="rh-no">{{.Request2.Number}}</span><span class="rh-amt">{{money (approvedOf .Request2)}}</span></div>
  <h1>{{.Request2.Vendor}}{{if .Request2.ShortTitle}} — {{.Request2.ShortTitle}}{{end}}</h1>
  <p class="rh-meta">Raised by {{.Request2.RequesterName}}{{if .Request2.Project}} · {{.Request2.Project}}{{if .Request2.Head}} / {{.Request2.Head}}{{end}}{{end}}{{if .Request2.NeededBy}} · needed by {{dateLong .Request2.NeededBy}}{{end}}</p>
  <div class="rh-status">
    <span class="pill processing">Processing — {{.Request2.ProcessingByName}}</span>
    <span class="waiting">Reserved by {{if .ReserveMine}}you{{else}}{{.Request2.ProcessingByName}}{{end}} since {{datep .Request2.ProcessingAt}}</span>
  </div>
</div>

{{/* Every entry below is gated on holding the reservation or on
     reservation:reassign, so a reader with neither was shown "Pick one" over
     an empty list (queue-1). They are told instead who can act. */}}
{{if not (or .ReserveMine (.Perms.Can "reservation" "reassign"))}}
<div class="banner info">
  <span class="b-ico" aria-hidden="true">i</span>
  <div>
    <b>You cannot act on this reservation</b>
    <p>{{.Request2.ProcessingByName}} can carry on and record the payment, release it back to the queue, or put it on hold. An administrator with the reassign permission can hand it to somebody else. Nothing here is yours to change.</p>
  </div>
</div>
{{else}}
<div class="card">
  <div class="card-head"><h2>Pick one</h2></div>
  <div class="a-list">
    {{if and .ReserveMine (.Perms.Can "payment" "create")}}<a href="/payments/new?request={{.Request2.ID}}"><span class="al-main"><b>Carry on and record the payment</b><small>Opens the payment form with your reservation intact</small></span><span class="al-amt" aria-hidden="true">→</span></a>{{end}}
    {{/* The store lets a non-holder release when the caller resolved
         reservation:reassign — taking work off somebody is that verb — so an
         administrator clearing a colleague's stale reservation is offered the
         choice the store would honour, rather than having to infer it from the
         reassign entry that happens to share this href. */}}
    {{if and (.Perms.Can "reservation" "release") (or .ReserveMine (.Perms.Can "reservation" "reassign"))}}<a href="/requests/{{.Request2.ID}}/reservation"><span class="al-main"><b>Release it</b><small>Requires a reason and confirming no payment was initiated</small></span><span class="al-amt" aria-hidden="true">→</span></a>{{end}}
    {{if .Perms.Can "reservation" "reassign"}}<a href="/requests/{{.Request2.ID}}/reservation"><span class="al-main"><b>Hand it to a colleague</b><small>Stays reserved, assigned to them, with your reason recorded</small></span><span class="al-amt" aria-hidden="true">→</span></a>{{end}}
    {{/* A hold pauses an approved request, and this screen only exists while the
         request is reserved — so the first step is the release screen, not the
         request. The mockup links straight to the on-hold state; sending the
         reader there would land them on a page whose hold control is gated off.
         And because the first step *is* the release screen, this entry is gated
         the way the release entry is: on being able to act on **this**
         reservation. Gated on payment:hold alone it was offered to every
         accountant, and /reservation answers 403 to anybody who neither holds the
         reservation nor may reassign — so a non-holding accountant was shown
         exactly one thing to do and refused when they took it (F-D-03). */}}
    {{if and (.Perms.Can "payment" "hold") (or .ReserveMine (.Perms.Can "reservation" "reassign"))}}<a href="/requests/{{.Request2.ID}}/reservation"><span class="al-main"><b>Put it on hold</b><small>If you are waiting on the requester for something — release the reservation first</small></span><span class="al-amt" aria-hidden="true">→</span></a>{{end}}
  </div>
</div>
{{end}}

<div class="section-head"><h2>Who has been told</h2></div>
<ol class="thread">
  {{range .Audit}}
  <li>
    <span class="tl-dot {{auditTone .Action}}" aria-hidden="true">{{auditGlyph .Action}}</span>
    <div class="tl-head"><b>{{.ActorName}} {{auditPhrase .Action}}</b><time>{{date .CreatedAt}}</time></div>
    <div class="tl-body">{{trailBody .}}</div>
  </li>
  {{else}}
  <li><div class="tl-body muted">Nothing has happened on this request yet.</div></li>
  {{end}}
</ol>

{{if .Perms.Can "reservation" "reassign"}}
<div class="action-bar">
  <span class="ab-note d-only">Administrators can force a reassignment.</span>
  <span class="row-end"></span>
  <a class="btn outline" href="/requests/{{.Request2.ID}}/reservation">Reassign with reason</a>
</div>
{{end}}
{{template "bottom" .}}
{{end}}

{{define "months"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Month control</div><h1>Monthly Plans</h1><p class="sub muted">Create each month, review prior months, and open locked history whenever needed.</p></div><a class="btn outline" href="/reports/monthly">Reports</a></section>
<form class="toolbar setup-form" method="post" action="/months"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>New month<input type="month" name="target_month" value="{{.TargetMonth}}" required></label><label>Plan type<select name="source_mode"><option value="copy">Copy from month</option><option value="blank">Start blank</option></select></label><label>Source month<input type="month" name="source_month" value="{{.SourceMonth}}"></label><button class="primary">Create Month</button></form>
<div class="table-wrap"><table class="t-cards"><thead><tr><th>Month</th><th>Status</th><th>Source</th><th class="num">Budget</th><th class="num">Actual</th><th class="num">Remaining</th><th class="num">Used</th><th>Created</th><th>Actions</th></tr></thead><tbody>{{range .MonthPlans}}<tr><td class="t-lead" data-label="Month"><strong>{{.Month}}</strong><br><small>{{.HeadCount}} heads</small></td><td data-label="Status"><span><span class="pill {{.Status}}">{{planStatusText .Status}}</span>{{if .LockedAt}}<br><small>{{datep .LockedAt}}</small>{{end}}</span></td><td data-label="Source">{{if .SourceMonth}}{{.SourceMonth}}{{else}}<span class="muted">Manual</span>{{end}}</td><td class="num" data-label="Budget">{{money .Budget}}</td><td class="num" data-label="Actual">{{money .Actual}}</td><td class="num {{varClass .Variance}}" data-label="Remaining">{{money .Variance}}</td><td class="num" data-label="Used">{{.UsedPercent}}</td><td data-label="Created">{{if .CreatedAt}}{{datep .CreatedAt}}{{else}}<span class="muted">Imported history</span>{{end}}</td><td class="actions-cell" data-label="Actions"><span><a class="btn small" href="/grid?month={{.Month}}">Grid</a><a class="btn small outline" href="/budgets?month={{.Month}}">Budget</a><a class="btn small outline" href="/payments?month={{.Month}}">Payments</a><a class="btn small outline" href="/reports/heads?from={{.Month}}&to={{.Month}}">Report</a></span></td></tr>{{else}}<tr><td colspan="9" class="empty" data-label="">No monthly plans yet. <a href="/months">Open a month</a> to start one.</td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}

{{define "budgets"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Monthly plan</div><h1>Budgets</h1><p class="sub muted">Edit planned spend per head for the selected month.</p></div>{{if .Grid.Locked}}<span class="pill warn">Locked</span>{{end}}</section>
<form class="toolbar" method="get"><label>Month<input type="month" name="month" value="{{.Month}}"></label><button>Open</button><a class="btn outline" href="/months">Month history</a><a class="btn outline" href="/grid?month={{.Month}}">View grid</a></form>{{if .Grid.Locked}}<div class="locked">This month is locked. Unlock it with a reason before changing budgets or payments.</div>{{end}}
<form class="budget-editor" method="post" action="/budgets"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="month" value="{{.Month}}"><table class="t-cards"><thead><tr><th>Project</th><th>Head</th><th>Status</th><th>Budget</th><th class="num">Actual</th><th class="num">Used</th></tr></thead><tbody>{{range .Grid.Rows}}<tr><td data-label="Project">{{.Project}}</td><td class="t-lead" data-label="Head">{{.Head}}</td><td data-label="Status">{{if .Active}}<span class="pill good">Active</span>{{else}}<span class="pill neutral">Retired</span>{{end}}</td><td data-label="Budget"><span><input name="budget_{{.HeadID}}" aria-label="Budget for {{.Project}} / {{.Head}}" value="{{if $.BudgetInputs}}{{index $.BudgetInputs .HeadID}}{{else}}{{money .Budget}}{{end}}" {{if or $.Grid.Locked (not .Active)}}disabled{{end}}>{{if not .Active}}<span class="hint">Retired: kept for history, not editable.</span>{{end}}</span></td><td class="num" data-label="Actual">{{money .Actual}}</td><td class="num" data-label="Used">{{usedText .Budget .Actual}}</td></tr>{{else}}<tr><td colspan="6" class="empty" data-label="">No active heads for this month. <a href="/heads">Add a head</a> first.</td></tr>{{end}}</tbody></table><div class="form-actions"><button class="primary" {{if .Grid.Locked}}disabled{{end}}>Save Budgets</button></div></form>
{{template "bottom" .}}
{{end}}

{{define "projects"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Setup</div><h1>Projects</h1><p class="sub muted">Manage project buckets used by the grid and payment entry.</p></div></section>
<form class="toolbar setup-form" method="post" action="/projects"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Project name<input name="name" required></label><label>Order<input name="sort_order" type="number" value="0"></label><label class="checkline"><input type="checkbox" name="active" checked> Active</label><button class="primary">Add Project</button></form>
<div class="table-wrap"><table class="t-cards"><thead><tr><th>Name</th><th>Status</th><th>Order</th><th>Update</th></tr></thead><tbody>{{range .Projects}}<tr><td class="t-lead" data-label="Name"><input form="project-{{.ID}}" name="name" aria-label="Name for {{.Name}}" value="{{.Name}}" required></td><td data-label="Status"><label class="checkline"><input form="project-{{.ID}}" type="checkbox" name="active" {{check .Active}}> {{boolText .Active}}</label></td><td data-label="Order"><input form="project-{{.ID}}" name="sort_order" type="number" aria-label="Sort order for {{.Name}}" value="{{.SortOrder}}"></td><td data-label="Update"><span><form id="project-{{.ID}}" method="post" action="/projects"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"></form><button form="project-{{.ID}}">Save</button></span></td></tr>{{else}}<tr><td colspan="4" class="empty" data-label="">No projects yet. Add the first one above.</td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}

{{define "heads"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Setup</div><h1>Heads</h1><p class="sub muted">Maintain spend heads, due days, and sort order.</p></div></section>
<form class="toolbar setup-form" method="post" action="/heads"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Project<select name="project_id" required>{{range .Projects}}<option value="{{.ID}}">{{.Name}}</option>{{end}}</select></label><label>Head name<input name="name" required></label><label>Due day<input name="due_day" type="number" min="1" max="31" placeholder="5"></label><label>Order<input name="sort_order" type="number" value="0"></label><label class="checkline"><input type="checkbox" name="active" checked> Active</label><button class="primary">Add Head</button></form>
<div class="table-wrap"><table class="t-cards"><thead><tr><th>Project</th><th>Head</th><th>Due</th><th>Status</th><th>Order</th><th>Update</th></tr></thead><tbody>{{range .Heads}}{{$head := .}}<tr><td data-label="Project">{{/* $.AllProjects, not $.Projects. This screen lists every head, retired
     projects included (T12), and the row select used to be populated from the
     active set only — so a head whose project had been retired had no matching
     option, the browser selected the first one, and that row's own Save moved the
     head to a project nobody chose, taking every payment ever recorded against it
     into another project's variance (F-G-033). The "Add head" form above keeps the
     active set: a *new* head may not be filed under a retired project.

     Only the project this head is filed under is rendered here. fervid-app.js
     swaps in the full list from the shared <template> below the moment somebody
     reaches for the control, because rendering all of them in all of the rows is
     229 x 24 option elements and 470 KB, of which a reader ever reads one. The
     one option that is rendered is looked up rather than taken from the head, so
     it keeps its "(retired)" marker: which project a head is filed under is the
     whole point of the column. */}}<select form="head-{{.ID}}" name="project_id" aria-label="Project for {{.Name}}" data-project-select data-chosen="{{$head.ProjectID}}">{{range $.AllProjects}}{{if eq .ID $head.ProjectID}}<option value="{{.ID}}" selected>{{.Name}}{{if not .Active}} (retired){{end}}</option>{{end}}{{end}}</select></td><td class="t-lead" data-label="Head"><input form="head-{{.ID}}" name="name" aria-label="Head name for {{.Name}}" value="{{.Name}}" required></td><td data-label="Due"><input form="head-{{.ID}}" name="due_day" type="number" min="1" max="31" aria-label="Due day for {{.Name}}" value="{{.DueDay}}"></td><td data-label="Status"><label class="checkline"><input form="head-{{.ID}}" type="checkbox" name="active" {{check .Active}}> {{boolText .Active}}</label></td><td data-label="Order"><input form="head-{{.ID}}" name="sort_order" type="number" aria-label="Sort order for {{.Name}}" value="{{.SortOrder}}"></td><td data-label="Update"><span><form id="head-{{.ID}}" method="post" action="/heads"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"></form><button form="head-{{.ID}}">Save</button></span></td></tr>{{else}}<tr><td colspan="6" class="empty" data-label="">No heads yet. Add the first one above.</td></tr>{{end}}</tbody></table></div>
{{/* Every project, once, for the row pickers above. Retired ones are included
     and marked, because a head already filed under a retired project must keep
     showing it — only the Add form above is restricted to the active set. */}}
<template id="project-options">{{range .AllProjects}}<option value="{{.ID}}">{{.Name}}{{if not .Active}} (retired){{end}}</option>{{end}}</template>
{{template "bottom" .}}
{{end}}

{{/* Every <td> carries data-label, including the trailing action cell, because
     table.t-cards uses that attribute to caption each field once the table
     restacks into cards below 860px. Editing lives in one sheet per user; the
     legacy users.role rides along as a hidden input because UpdateUser still
     validates it and RequireAnotherActiveAdmin still guards it. The approver
     <select> omits the user themselves and SetUserDefaultApprover rejects them
     again server-side — a hidden option is not validation. */}}
{{define "confirm_deactivate"}}
{{template "top" .}}
{{with .Deactivation}}
<section class="page-banner"><div><div class="eyebrow">Confirm</div><h1>{{.Heading}}</h1><p class="sub muted">Nothing has been saved yet.</p></div></section>
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">!</span>
  <div><b>{{.Lead}}</b><p>{{.Consequence}}</p></div>
</div>
<div class="table-wrap"><table class="t-cards"><thead><tr><th>Request</th><th>Title</th><th>Status</th><th class="r">Amount</th></tr></thead><tbody>
{{range .Requests}}<tr><td class="t-lead" data-label="Request"><a href="/requests/{{.ID}}">{{.Number}}</a></td><td data-label="Title">{{.Title}}</td><td data-label="Status">{{reqStatus .Status}}</td><td class="r" data-label="Amount">{{money .Amount}}</td></tr>{{end}}
</tbody></table></div>
<form method="post" action="{{.Action}}" class="stack-12">
  <input type="hidden" name="csrf" value="{{$.CSRF}}">
  {{range .Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}
  {{if .AskPassword}}<div class="field"><label for="confirm-pw">New password</label><input id="confirm-pw" name="password" type="password" minlength="12" required><span class="hint">You set a new password. Type it again, because it is never sent back to the page.</span></div>{{end}}
  <label class="checkline"><input type="checkbox" name="confirm" value="on" required> {{.ConfirmText}}</label>
  <div class="row"><a class="btn outline" href="{{.Back}}">Cancel</a><button class="btn primary" type="submit">Deactivate anyway</button></div>
</form>
{{end}}
{{template "bottom" .}}
{{end}}

{{define "users"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Access</div>
    <h1>Users</h1>
    <p class="sub">A person can hold several roles at once.</p>
  </div>
  <div class="pb-actions">{{if .Perms.Can "user" "create"}}<button class="btn primary" type="button" data-open="user-new">＋ Add user</button>{{end}}</div>
</section>

<div class="table-wrap">
  <table class="t-cards">
    <thead><tr><th>Name</th><th>Email</th><th>Roles</th><th>Default approver</th><th class="c">Status</th><th class="c">Edit</th></tr></thead>
    <tbody>
      {{range .Users}}
      <tr>
        <td class="t-lead" data-label="Name">{{.Name}}</td>
        <td data-label="Email">{{.Email}}</td>
        <td data-label="Roles">{{$uid := .ID}}{{range $.AllRoles}}{{if index (index $.UserRoleIDs $uid) .ID}}<span class="pill neutral no-dot">{{.Name}}</span> {{end}}{{end}}</td>
        <td data-label="Default approver">{{if .DefaultApproverID}}{{index $.ApproverNames .DefaultApproverID}}{{else}}—{{end}}</td>
        <td class="c" data-label="Status"><span class="pill {{if .Active}}good{{else}}neutral{{end}}">{{boolText .Active}}</span></td>
        <td class="c" data-label="">{{if $.Perms.Can "user" "edit"}}<button class="btn small outline" type="button" data-open="user-{{.ID}}">Edit</button>{{end}}</td>
      </tr>
      {{else}}
      <tr><td colspan="6" class="empty">No users yet.</td></tr>
      {{end}}
    </tbody>
  </table>
</div>

{{range .Users}}
<div class="overlay" id="user-{{.ID}}" hidden>
  <div class="sheet">
    <form method="post" action="/users">
      <input type="hidden" name="csrf" value="{{$.CSRF}}">
      <input type="hidden" name="id" value="{{.ID}}">
      <input type="hidden" name="email" value="{{.Email}}">
      <div class="sh-head"><div><h2>{{.Name}}</h2><p class="sh-sub">{{.Email}}</p></div><button class="sh-close" type="button" data-close="user-{{.ID}}">✕</button></div>
      <div class="sh-body stack-12">
        <div class="field"><label for="u-name-{{.ID}}">Name</label><input id="u-name-{{.ID}}" name="name" value="{{.Name}}" required></div>
        <div class="field"><span class="flabel">Roles</span>
          <div class="stack-8">{{$uid := .ID}}{{range $.AllRoles}}<label class="checkline"><input type="checkbox" name="role_ids" value="{{.ID}}" {{if index (index $.UserRoleIDs $uid) .ID}}checked{{end}}> {{.Name}}{{if .Description}} — {{.Description}}{{end}}</label>{{end}}</div>
        </div>
        <div class="field"><label for="u-apr-{{.ID}}">Default approver for their own requests</label>
          {{/* Left empty on purpose. fervid-app.js fills it from the single
               shared option list at the foot of this page when the panel opens.
               Rendering every approver inside every user's panel is quadratic —
               56 people produced about 3,000 option elements and 346 KB of
               markup, none of it read until a panel is opened. */}}
          <select id="u-apr-{{.ID}}" name="default_approver_id"
                  data-approver-select data-self="{{.ID}}" data-chosen="{{.DefaultApproverID}}">
            <option value="0">None</option>
          </select>
          <span class="hint">Self-approval is not allowed, so they never appear in their own list.</span>
        </div>
        <div class="field"><label for="u-pw-{{.ID}}">Reset password</label><input id="u-pw-{{.ID}}" name="password" type="password" minlength="12" placeholder="Leave blank to keep the current one"></div>
        <input type="hidden" name="role" value="{{.Role}}">
        <label class="checkline"><input type="checkbox" name="active" {{check .Active}}> Active</label>
      </div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="user-{{.ID}}">Cancel</button><span class="row-end"></span><button class="btn primary" type="submit">Save user</button></div>
    </form>
  </div>
</div>
{{end}}

{{/* Every approver, once. Each user's panel borrows these when it opens rather
     than carrying its own copy — see the select above. A <template> is inert:
     the browser parses it but does not render it or submit anything inside it. */}}
<template id="approver-options">
  {{range .Approvers}}<option value="{{.ID}}">{{.Name}}</option>{{end}}
</template>

<div class="overlay" id="user-new" hidden>
  <div class="sheet">
    <form class="setup-form" method="post" action="/users">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="sh-head"><div><h2>Add user</h2><p class="sh-sub">They can be given more roles once they exist.</p></div><button class="sh-close" type="button" data-close="user-new">✕</button></div>
      <div class="sh-body stack-12">
        <div class="field"><label for="nu-email">Email</label><input id="nu-email" name="email" type="email" required></div>
        <div class="field"><label for="nu-name">Name</label><input id="nu-name" name="name" required></div>
        {{/* The real roles, the same ones the edit panel offers. This used to be
             a two-value "Data entry / Admin" select taken from the superseded
             users.role column: it could not create a requester or an approver,
             and "Data entry" was mapped to the Accounts role, which can settle
             and void payments. */}}
        <div class="field"><span class="flabel">Roles <span class="req" aria-hidden="true">*</span></span>
          <div class="stack-8">{{range .AllRoles}}<label class="checkline"><input type="checkbox" name="role_ids" value="{{.ID}}"> {{.Name}}{{if .Description}} — {{.Description}}{{end}}</label>{{end}}</div>
          <span class="hint">A person can hold several roles at once. Their permissions are everything their roles allow.</span>
        </div>
        <div class="field"><label for="nu-pw">Password</label><input id="nu-pw" name="password" type="password" minlength="12" required></div>
        <label class="checkline"><input type="checkbox" name="active" checked> Active</label>
      </div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="user-new">Cancel</button><span class="row-end"></span><button class="btn primary" type="submit">Add User</button></div>
    </form>
  </div>
</div>
{{template "bottom" .}}
{{end}}

{{/* Vendors list — mockups/screens/vendors-list.html.

     The page banner keeps its h1 on every width: the mobile top bar carries a
     title too, but the shell contract is that every screen has a visible h1,
     so this is not a d-only banner.

     Two filter renderings, one dataset: the desktop .toolbar and the mobile
     .m-filters are both real GET forms, so filtering works with no JavaScript
     at all, and the stylesheet hides whichever one does not belong at that
     width. Every <td> carries data-label because t-cards restacks into cards
     below 860px and the attribute is what captions each field. */}}
{{define "vendors_list"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Masters</div>
    <h1>Vendors</h1>
    <p class="sub">{{.VendorStats.Active}} active · {{.VendorStats.MissingGSTIN}} missing a GSTIN · bank details visible only with permission</p>
  </div>
  <div class="pb-actions">{{if .Perms.Can "vendor" "create"}}<a class="btn primary" href="/vendors/new">＋ Add vendor</a>{{end}}</div>
</section>

{{if .VendorStats.MissingGSTIN}}
<div class="banner warn">
  <span class="b-ico">⚠</span>
  <div>
    <b>{{.VendorStats.MissingGSTIN}} {{plural .VendorStats.MissingGSTIN "vendor has" "vendors have"}} no GSTIN</b>
    <p>They can still be paid. The gap shows up in vendor reporting until someone fills it in.</p>
  </div>
  <span class="b-actions">{{if eq .VendorGap "gstin"}}<a class="btn small outline" href="/vendors">Show all</a>{{else}}<a class="btn small outline" href="/vendors?gap=gstin">Show them</a>{{end}}</span>
</div>
{{end}}

<form class="toolbar" method="get" action="/vendors">
  {{if .VendorGap}}<input type="hidden" name="gap" value="{{.VendorGap}}">{{end}}
  <div class="field search"><label for="q">Search</label><input id="q" name="q" value="{{.Query}}" placeholder="Name, GSTIN, PAN, city"></div>
  <div class="field"><label for="ty">Type</label><select id="ty" name="type">
    <option value="">All types</option>
    <option value="company" {{select .VendorType "company"}}>Company</option>
    <option value="proprietor" {{select .VendorType "proprietor"}}>Proprietor</option>
    <option value="individual" {{select .VendorType "individual"}}>Individual</option>
  </select></div>
  <div class="field"><label for="ct">Category</label><select id="ct" name="category">
    <option value="">All categories</option>
    {{range .VendorCategories}}<option value="{{.}}" {{select $.VendorCategory .}}>{{.}}</option>{{end}}
  </select></div>
  <div class="field"><label for="st">Status</label><select id="st" name="status">
    <option value="active" {{select .Status "active"}}>Active</option>
    <option value="inactive" {{select .Status "inactive"}}>Inactive</option>
    <option value="all" {{select .Status "all"}}>All</option>
  </select></div>
  <span class="row-end"></span><button class="btn">Apply</button>
</form>

<form class="m-filters" method="get" action="/vendors">
  <input type="hidden" name="status" value="{{.Status}}">
  <span class="m-search"><input name="q" value="{{.Query}}" placeholder="Search vendors…" aria-label="Search vendors"></span>
  <button class="btn filter-btn" type="submit">Filters</button>
</form>

<div class="table-wrap">
  <table class="t-cards">
    <thead><tr><th>Vendor</th><th>Type</th><th>GSTIN</th><th>City</th><th class="num">Paid this year</th><th class="c">Open requests</th><th class="c">Status</th></tr></thead>
    <tbody>
      {{range .Vendors}}
      <tr>
        <td class="t-lead" data-label="Vendor"><a href="/vendors/{{.ID}}">{{.Name}}</a>{{if .Categories}}<span class="t-sub">{{categories .Categories}}</span>{{end}}</td>
        <td data-label="Type">{{vendorType .VendorType}}</td>
        <td class="num" data-label="GSTIN">{{if .GSTIN}}{{.GSTIN}}{{else}}<span class="pill warn no-dot">Missing</span>{{end}}</td>
        <td data-label="City">{{if .City}}{{.City}}{{else}}—{{end}}</td>
        <td class="num" data-label="Paid this year">{{money .PaidThisYear}}</td>
        <td class="c" data-label="Open requests">{{.OpenRequests}}</td>
        <td class="c" data-label="Status"><span class="pill {{if eq .Status "active"}}good{{else}}neutral{{end}}">{{vendorStatus .Status}}</span></td>
      </tr>
      {{else}}
      <tr><td colspan="7" class="empty">No vendors match these filters.</td></tr>
      {{end}}
    </tbody>
    <tfoot>
      <tr>
        <td data-label="">{{len .Vendors}} shown of {{.VendorStats.Total}}</td>
        <td data-label=""></td><td data-label=""></td><td data-label=""></td>
        <td class="num" data-label="Paid this year">{{money .VendorPaidTotal}}</td>
        <td class="c" data-label="Open">{{.VendorOpenTotal}}</td>
        <td data-label=""></td>
      </tr>
    </tfoot>
  </table>
</div>
{{template "bottom" .}}
{{end}}

{{/* Vendor combobox options — an htmx fragment, not a page. It starts at
     .combo-list and never at "top", so it can be swapped straight into the
     request form's picker. SearchVendors returns no bank details to anyone, so
     the picker cannot become a side door onto the restricted block. The
     add-a-vendor row is offered only to a caller who could actually complete
     it, because an affordance that 403s is worse than no affordance. */}}
{{define "vendor_combo_options"}}<div class="combo-list" role="listbox" aria-label="Vendor results">
{{range .Vendors}}<a class="co" role="option" href="/vendors/{{.ID}}" data-id="{{.ID}}" data-name="{{.Name}}"><span class="co-main"><b>{{.Name}}</b><small>{{if .GSTIN}}{{.GSTIN}}{{else}}No GSTIN{{end}}{{if .City}} · {{.City}}{{end}}</small></span></a>
{{else}}<span class="co"><span class="co-main"><b>{{if .Query}}No vendor matches “{{.Query}}”{{else}}Type a name, GSTIN or city{{end}}</b><small>Only active vendors can be picked.</small></span></span>
{{end}}{{if .Perms.Can "vendor" "create"}}<a class="co co-add" href="/vendors/new">＋ Add a new vendor</a>{{end}}
</div>{{end}}

{{/* Vendor detail — mockups/screens/vendor-detail.html.

     The Payment details fieldset is gated twice over, and the two gates are
     not redundant. .Perms.Can is the presentation gate; .Vendor.Bank being
     non-nil is the DATA gate, and it is the one that matters — the store never
     read the bank columns for a caller without vendor_bank:view, so there is
     nothing here to print even if this condition were wrong. The locked banner
     takes the fieldset's place rather than the fieldset quietly vanishing:
     hiding the section entirely would leave a reader wondering whether the
     vendor has no bank details or whether they simply cannot see them.

     Bank inputs carry their own disabled attribute rather than relying on the
     surrounding fieldset, because vendor_bank:edit is a separate permission
     from vendor:edit: someone may edit the contact block and not the bank
     block. A disabled input is not submitted, so the browser cannot send what
     the caller may not change — and UpdateVendor ignores it regardless. */}}
{{define "vendor_detail"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Masters · vendor</div>
    <h1>{{if .Vendor.ID}}{{.Vendor.Name}}{{else}}Add vendor{{end}}</h1>
    <p class="sub">{{if .Vendor.ID}}{{vendorType .Vendor.VendorType}}{{if .Vendor.City}} · {{.Vendor.City}}{{end}} · {{vendorStatus .Vendor.Status}}{{else}}A vendor the request form can find, and Accounts can pay.{{end}}</p>
  </div>
  <div class="pb-actions"><a class="btn outline" href="/vendors">{{if .VendorEditable}}Cancel{{else}}Back to vendors{{end}}</a></div>
</section>

{{if .Vendor.ID}}
<div class="segmented">
  <a class="is-active" href="/vendors/{{.Vendor.ID}}">Record</a>
  <a aria-disabled="true">Requests <span class="n">Soon</span></a>
  <a aria-disabled="true">Payments <span class="n">Soon</span></a>
  <a aria-disabled="true">History <span class="n">Soon</span></a>
</div>
{{end}}

<form method="post" action="{{if .Vendor.ID}}/vendors/{{.Vendor.ID}}{{else}}/vendors{{end}}">
  <input type="hidden" name="csrf" value="{{.CSRF}}">

  <fieldset {{if not .VendorEditable}}disabled{{end}}>
    <legend>Identity</legend>
    <div class="form-grid">
      <div class="field span-6"><label for="v-name">Vendor name <span class="req" aria-hidden="true">*</span></label><input id="v-name" name="name" value="{{.Vendor.Name}}" required></div>
      <div class="field span-3 m-half"><label for="v-short">Short name</label><input id="v-short" name="display_name" value="{{.Vendor.DisplayName}}"><span class="hint">Used in lists and dropdowns.</span></div>
      <div class="field span-3 m-half"><label for="v-type">Type <span class="req" aria-hidden="true">*</span></label><select id="v-type" name="vendor_type">
        <option value="company" {{select .Vendor.VendorType "company"}}>Company</option>
        <option value="proprietor" {{select .Vendor.VendorType "proprietor"}}>Proprietor</option>
        <option value="individual" {{select .Vendor.VendorType "individual"}}>Individual</option>
      </select></div>
      <div class="field span-6"><label for="v-cat">Categories</label><input id="v-cat" name="categories" value="{{.Vendor.Categories}}"><span class="hint">Comma separated. Used for vendor reporting.</span></div>
      <div class="field span-6 m-half"><label for="v-status">Status</label><select id="v-status" name="status">
        <option value="active" {{select .Vendor.Status "active"}}>Active</option>
        <option value="inactive" {{select .Vendor.Status "inactive"}}>Inactive</option>
      </select><span class="hint">Inactive vendors disappear from new requests but stay on old ones.</span></div>
    </div>
  </fieldset>

  <fieldset {{if not .VendorEditable}}disabled{{end}}>
    <legend>Statutory</legend>
    <div class="form-grid">
      <div class="field span-4 m-half"><label for="v-gst">GSTIN</label><input id="v-gst" class="num" name="gstin" value="{{.Vendor.GSTIN}}" maxlength="15"><span class="hint">15 characters, or blank if the vendor has none.</span></div>
      <div class="field span-4 m-half"><label for="v-pan">PAN</label><input id="v-pan" class="num" name="pan" value="{{.Vendor.PAN}}"></div>
      <div class="field span-4 m-half"><label for="v-msme">MSME / Udyam number</label><input id="v-msme" class="num" name="msme_udyam" value="{{.Vendor.MSMEUdyam}}"></div>
      <div class="field span-4 m-half"><label for="v-tds">TDS section</label><select id="v-tds" name="tds_section">
        <option value="" {{select .Vendor.TDSSection ""}}>Not applicable</option>
        <option value="194C" {{select .Vendor.TDSSection "194C"}}>194C — contractors</option>
        <option value="194J" {{select .Vendor.TDSSection "194J"}}>194J — professional</option>
        <option value="194Q" {{select .Vendor.TDSSection "194Q"}}>194Q — purchase of goods</option>
      </select></div>
      <div class="field span-4 m-half"><label for="v-rate">Default TDS rate</label><input id="v-rate" name="tds_rate" value="{{.Vendor.TDSRate}}" placeholder="2%"></div>
      <div class="field span-4 m-half"><p class="hint">Recorded for reference only. This system never computes tax — Accounts confirms deductions outside it.</p></div>
    </div>
  </fieldset>

  <fieldset {{if not .VendorEditable}}disabled{{end}}>
    <legend>Contact</legend>
    <div class="form-grid">
      <div class="field span-4 m-half"><label for="v-person">Contact person</label><input id="v-person" name="contact_person" value="{{.Vendor.ContactPerson}}"></div>
      <div class="field span-4 m-half"><label for="v-phone">Phone</label><input id="v-phone" class="num" name="phone" value="{{.Vendor.Phone}}"></div>
      <div class="field span-4"><label for="v-email">Email</label><input id="v-email" type="email" name="email" value="{{.Vendor.Email}}"></div>
      <div class="field span-12"><label for="v-addr">Billing address</label><textarea id="v-addr" name="address">{{.Vendor.Address}}</textarea></div>
      <div class="field span-4 m-half"><label for="v-city">City</label><input id="v-city" name="city" value="{{.Vendor.City}}"></div>
      <div class="field span-4 m-half"><label for="v-state">State</label><input id="v-state" name="state" value="{{.Vendor.State}}"></div>
      <div class="field span-4 m-half"><label for="v-code">State code</label><input id="v-code" class="num" name="state_code" value="{{.Vendor.StateCode}}"><span class="hint">The first two digits of the GSTIN.</span></div>
    </div>
  </fieldset>

  {{if and (.Perms.Can "vendor_bank" "view") .Vendor.Bank}}
  <fieldset {{if not .VendorEditable}}disabled{{end}}>
    <legend>Payment details — restricted</legend>
    <div class="banner locked">
      <span class="b-ico">🔒</span>
      <div>
        <b>Visible only with vendor bank permission</b>
        <p>They are deliberately kept off the employee request form — an employee raising a request never sees or types them.</p>
      </div>
    </div>
    <div class="form-grid">
      <div class="field span-6"><label for="b-name">Account name</label><input id="b-name" name="bank_account_name" value="{{.Vendor.Bank.AccountName}}" {{if not (.Perms.Can "vendor_bank" "edit")}}disabled{{end}}></div>
      <div class="field span-3 m-half"><label for="b-acc">Account number</label><input id="b-acc" class="num" name="bank_account_number" value="{{.Vendor.Bank.AccountNumber}}" {{if not (.Perms.Can "vendor_bank" "edit")}}disabled{{end}}></div>
      <div class="field span-3 m-half"><label for="b-ifsc">IFSC</label><input id="b-ifsc" class="num" name="bank_ifsc" value="{{.Vendor.Bank.IFSC}}" {{if not (.Perms.Can "vendor_bank" "edit")}}disabled{{end}}></div>
      <div class="field span-3 m-half"><label for="b-bank">Bank</label><input id="b-bank" name="bank_name" value="{{.Vendor.Bank.BankName}}" {{if not (.Perms.Can "vendor_bank" "edit")}}disabled{{end}}></div>
      <div class="field span-3 m-half"><label for="b-branch">Branch</label><input id="b-branch" name="bank_branch" value="{{.Vendor.Bank.Branch}}" {{if not (.Perms.Can "vendor_bank" "edit")}}disabled{{end}}></div>
      <div class="field span-3 m-half"><label for="b-upi">UPI ID</label><input id="b-upi" name="upi_id" value="{{.Vendor.Bank.UPIID}}" placeholder="Optional" {{if not (.Perms.Can "vendor_bank" "edit")}}disabled{{end}}></div>
      <div class="field span-3 m-half"><label for="b-mode">Default mode</label><select id="b-mode" name="default_payment_mode" {{if not (.Perms.Can "vendor_bank" "edit")}}disabled{{end}}>
        <option value="" {{select .Vendor.Bank.DefaultPaymentMode ""}}>Not set</option>
        <option value="bank_transfer" {{select .Vendor.Bank.DefaultPaymentMode "bank_transfer"}}>Bank transfer — NEFT</option>
        <option value="rtgs" {{select .Vendor.Bank.DefaultPaymentMode "rtgs"}}>RTGS</option>
        <option value="cheque" {{select .Vendor.Bank.DefaultPaymentMode "cheque"}}>Cheque</option>
      </select></div>
      <div class="field span-3 m-half"><label for="b-terms">Payment terms (days)</label><input id="b-terms" class="num" type="number" min="0" name="payment_terms_days" value="{{.Vendor.Bank.PaymentTermsDays}}" {{if not (.Perms.Can "vendor_bank" "edit")}}disabled{{end}}></div>
    </div>
  </fieldset>
  {{else}}
  <fieldset>
    <legend>Payment details</legend>
    <div class="banner locked">
      <span class="b-ico">🔒</span>
      <div>
        <b>You do not have permission to see bank details</b>
        <p>Accounts maintains them. The record is hidden, not just the buttons.</p>
      </div>
    </div>
  </fieldset>
  {{end}}

  <fieldset {{if not .VendorEditable}}disabled{{end}}>
    <legend>Notes</legend>
    <div class="form-grid">
      <div class="field span-12"><label for="v-notes">Internal notes</label><textarea id="v-notes" name="notes">{{.Vendor.Notes}}</textarea></div>
    </div>
  </fieldset>

  <div class="action-bar">
    <span class="ab-note d-only">{{if .Vendor.ID}}Created {{date .Vendor.CreatedAt}} · last edited {{date .Vendor.UpdatedAt}}{{else}}The name must be unique. Everything else can be filled in later.{{end}}</span>
    <span class="row-end"></span>
    {{if .VendorEditable}}<a class="btn outline" href="/vendors">Cancel</a><button class="btn primary" type="submit">Save vendor</button>{{else}}<a class="btn outline" href="/vendors">Back to vendors</a>{{end}}
  </div>
</form>
{{template "bottom" .}}
{{end}}

{{/* The two scope pill groups render the same state under two names. Same-name
     radios are one group across the whole form, so the mobile set must not
     borrow the desktop names: enabling it would silently clear the desktop
     selection. rolesSave reads scope_<resource> first and m_scope_<resource>
     as the fallback. */}}
{{define "permscope"}}
<span class="perm-scope">
  <label class="{{if eq .Scope ""}}is-on{{end}}"><input type="radio" name="scope_{{.ScopeResource}}" value="" {{if eq .Scope ""}}checked{{end}}> None</label>
  <label class="{{if eq .Scope "own"}}is-on{{end}}"><input type="radio" name="scope_{{.ScopeResource}}" value="own" {{if eq .Scope "own"}}checked{{end}}> Own</label>
  <label class="{{if eq .Scope "assigned"}}is-on{{end}}"><input type="radio" name="scope_{{.ScopeResource}}" value="assigned" {{if eq .Scope "assigned"}}checked{{end}}> Assigned</label>
  <label class="{{if eq .Scope "all"}}is-on{{end}}"><input type="radio" name="scope_{{.ScopeResource}}" value="all" {{if eq .Scope "all"}}checked{{end}}> All</label>
</span>
{{end}}

{{define "mpermscope"}}
<span class="perm-scope">
  <label class="{{if eq .Scope ""}}is-on{{end}}"><input type="radio" name="m_scope_{{.ScopeResource}}" value="" disabled {{if eq .Scope ""}}checked{{end}}> None</label>
  <label class="{{if eq .Scope "own"}}is-on{{end}}"><input type="radio" name="m_scope_{{.ScopeResource}}" value="own" disabled {{if eq .Scope "own"}}checked{{end}}> Own</label>
  <label class="{{if eq .Scope "assigned"}}is-on{{end}}"><input type="radio" name="m_scope_{{.ScopeResource}}" value="assigned" disabled {{if eq .Scope "assigned"}}checked{{end}}> Assigned</label>
  <label class="{{if eq .Scope "all"}}is-on{{end}}"><input type="radio" name="m_scope_{{.ScopeResource}}" value="all" disabled {{if eq .Scope "all"}}checked{{end}}> All</label>
</span>
{{end}}

{{/* Two renderings of one dataset inside one form. The desktop table is
     authoritative without JavaScript — it scrolls inside .table-wrap at any
     width — so the mobile accordion's inputs ship disabled and fervid-app.js
     hands over below 860px. Exactly one rendering ever contributes to a POST.
     An unavailable cell renders an em dash, never a disabled checkbox: a
     disabled checkbox reads as "off", which is a different claim. */}}
{{define "roles"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Access control</div>
    <h1>Roles &amp; permissions</h1>
    <p class="sub">Roles are data, not code. Create them, copy them, and set what each one may do and see.</p>
  </div>
  <div class="pb-actions">
    {{if .Perms.Can "role" "create"}}<button class="btn outline" type="button" data-open="role-copy">Copy this role</button>
    <button class="btn primary" type="button" data-open="role-new">＋ New role</button>{{end}}
  </div>
</section>

<div class="segmented">
  {{range .Roles}}<a class="{{if eq .ID $.Role.ID}}is-active{{end}}" href="/roles?role={{.ID}}">{{.Name}}{{if not .IsSystem}} <span class="n">custom</span>{{end}}</a>{{end}}
</div>

<form id="role-form" method="post" action="/roles">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <input type="hidden" name="role_id" value="{{.Role.ID}}">

  <div class="card">
    <div class="card-head">
      <h2>{{.Role.Name}}</h2>
      <span class="pill good">{{$n := index .RoleUserCounts .Role.ID}}{{$n}} {{plural $n "user" "users"}}</span>
      {{if and (not .Role.IsSystem) (.Perms.Can "role" "delete")}}<button class="btn small outline" type="button" data-open="role-delete">Delete role</button>{{end}}
    </div>
    <div class="card-body">
      <div class="form-grid">
        <div class="field span-4 m-half"><label for="rname">Role name</label><input id="rname" name="name" value="{{.Role.Name}}" required {{if .Role.IsSystem}}readonly{{end}}></div>
        <div class="field span-8"><label for="rdesc">Description</label><input id="rdesc" name="description" value="{{.Role.Description}}"></div>
      </div>
    </div>
  </div>

  <div class="table-wrap d-only">
    <table class="perm-table">
      <thead>
        <tr><th>Page</th>{{range .PermColumns}}<th class="c">{{.Label}}</th>{{end}}<th>Records it can see</th></tr>
      </thead>
      <tbody>
        {{range .PermMatrix}}
        <tr>
          <td class="perm-row-head">{{.Label}}</td>
          {{range .Cells}}<td class="c">{{if .Available}}<input type="checkbox" name="cell" value="{{.Value}}" data-cell="{{.Value}}" aria-label="{{.Label}}" {{if .Granted}}checked{{end}}{{if .Partial}} data-partial="1"{{end}}>{{else}}<span class="muted">—</span>{{end}}</td>{{end}}
          <td>{{if .Scoped}}{{template "permscope" .}}{{else}}<span class="perm-scope"><label class="is-on">None</label></span>{{end}}</td>
        </tr>
        <tr class="perm-advanced">
          <td colspan="9">
            <details>
              <summary>Advanced — every permission behind this row</summary>
              <div class="row">{{range .Advanced}}<label class="checkline"><input type="checkbox" name="perm" value="{{.Resource}}:{{.Action}}" data-cell="{{.Cell}}" {{if .Granted}}checked{{end}}> {{.Label}}</label>{{end}}</div>
            </details>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>

  <div class="perm-acc m-only">
    {{range .PermMatrix}}
    <div class="pa-item">
      <button class="pa-head" type="button"><span class="chev">›</span><b>{{.Label}}</b><span class="n">{{.Held}} of {{.Total}}</span></button>
      <div class="pa-body">
        {{range .Cells}}{{if .Available}}<label class="checkline"><input type="checkbox" name="cell" disabled value="{{.Value}}" data-cell="{{.Value}}" {{if .Granted}}checked{{end}}{{if .Partial}} data-partial="1"{{end}}> {{.Label}}</label>{{end}}{{end}}
        <details>
          <summary>Advanced</summary>
          {{range .Advanced}}<label class="checkline"><input type="checkbox" name="perm" disabled value="{{.Resource}}:{{.Action}}" data-cell="{{.Cell}}" {{if .Granted}}checked{{end}}> {{.Label}}</label>{{end}}
        </details>
        {{if .Scoped}}<div class="field"><span class="flabel">Records it can see</span>{{template "mpermscope" .}}</div>{{end}}
      </div>
    </div>
    {{end}}
  </div>

  <div class="action-bar">
    <span class="ab-note d-only">{{$n := index .RoleUserCounts .Role.ID}}{{if eq $n 1}}Changes apply to the 1 user holding this role.{{else}}Changes apply to all {{$n}} users holding this role.{{end}}</span>
    <span class="row-end"></span>
    <a class="btn outline" href="/roles?role={{.Role.ID}}">Discard</a>
    <button class="btn primary" type="submit">Save role</button>
  </div>
</form>

{{/* The sheets sit outside #role-form: HTML forbids nested forms, and the
     create/copy/delete posts must not carry the matrix. */}}
<div class="overlay" id="role-new" hidden>
  <div class="sheet">
    <form method="post" action="/roles/new">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="sh-head"><div><h2>New role</h2><p class="sh-sub">It starts with no permissions at all.</p></div><button class="sh-close" type="button" data-close="role-new">✕</button></div>
      <div class="sh-body stack-12">
        <div class="field"><label for="nr-name">Role name</label><input id="nr-name" name="name" required></div>
        <div class="field"><label for="nr-desc">Description</label><input id="nr-desc" name="description"></div>
      </div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="role-new">Cancel</button><span class="row-end"></span><button class="btn primary" type="submit">Create role</button></div>
    </form>
  </div>
</div>

<div class="overlay" id="role-copy" hidden>
  <div class="sheet">
    <form method="post" action="/roles/{{.Role.ID}}/copy">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="sh-head"><div><h2>Copy {{.Role.Name}}</h2><p class="sh-sub">The copy starts with the same permissions and data scope.</p></div><button class="sh-close" type="button" data-close="role-copy">✕</button></div>
      <div class="sh-body stack-12">
        <div class="field"><label for="cr-name">New role name</label><input id="cr-name" name="name" required></div>
      </div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="role-copy">Cancel</button><span class="row-end"></span><button class="btn primary" type="submit">Copy role</button></div>
    </form>
  </div>
</div>

{{/* The store refuses to delete a role anybody still holds (F-G-022), so the
     sheet says so up front and sends the reader to the Users screen instead
     of offering a Delete button that can only land on a 403 (rbac-6). */}}
{{if not .Role.IsSystem}}{{$holders := index .RoleUserCounts .Role.ID}}
<div class="overlay" id="role-delete" hidden>
  <div class="sheet">
    {{if gt $holders 0}}
    <div class="sh-head"><div><h2>Delete {{.Role.Name}}?</h2><p class="sh-sub">{{$holders}} {{plural $holders "user holds" "users hold"}} this role, so it cannot be deleted yet. Remove it from {{plural $holders "that user" "those users"}} on the Users screen first; they would otherwise lose everything it grants without warning.</p></div><button class="sh-close" type="button" data-close="role-delete">✕</button></div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="role-delete">Cancel</button><span class="row-end"></span><a class="btn primary" href="/users">Open Users</a></div>
    {{else}}
    <form method="post" action="/roles/{{.Role.ID}}/delete">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="sh-head"><div><h2>Delete {{.Role.Name}}?</h2><p class="sh-sub">Nobody holds this role. Deleting it cannot be undone.</p></div><button class="sh-close" type="button" data-close="role-delete">✕</button></div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="role-delete">Cancel</button><span class="row-end"></span><button class="btn danger" type="submit">Delete role</button></div>
    </form>
    {{end}}
  </div>
</div>
{{end}}
{{template "bottom" .}}
{{end}}

{{/* Payment requests — mockups/screens/request-new-type.html.

     Step 1 of 2. Picking a type is a NAVIGATION, not a form control (A16):
     each card is a link to the same route carrying ?type=, so the form that
     follows can be built for one type and never has to un-build itself. There
     is deliberately no <select name="type"> anywhere in the application.

     The banner keeps its heading on every device rather than carrying .d-only
     as the mockup does: the mobile top bar renders the screen title as a <b>,
     not a heading, so hiding this one would leave a phone with no h1 at all. */}}
{{define "request_new_type"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">New request · step 1 of 2</div>
    <h1>What are you asking to be paid?</h1>
    <p class="sub">Pick a type. The form only asks for what that type needs. Nothing is saved until you submit.</p>
  </div>
  <div class="pb-actions"><a class="btn outline" href="/">Cancel</a></div>
</section>

<div class="type-grid">
  {{range requestTypes}}
  <a class="type-card" href="/requests/new?type={{.Key}}">
    <span class="tc-ico" aria-hidden="true">{{.Icon}}</span>
    <b>{{.Label}}</b>
    <p>{{.Blurb}}</p>
    <span class="tc-tag pill {{.TagClass}}">{{.Tag}}</span>
  </a>
  {{end}}
</div>

<div class="banner info" style="margin-top:16px">
  <span class="b-ico" aria-hidden="true">?</span>
  <div>
    <b>Not sure which one?</b>
    <p>If the money leaves the company and never comes back, it is an expense. A deposit or guarantee
      paid to another company or authority is money the company expects back — raise it as a deposit
      or guarantee, paid to them. Money advanced to you that you will account for is an employee
      advance, paid to you.</p>
  </div>
</div>
{{template "bottom" .}}
{{end}}

{{/* The treatment-dependent middle of the request form — the htmx fragment
     GET /requests/new/fields returns, and the same markup the full page
     renders inline on first paint.

     Budget and recoverable are rendered as ALTERNATIVES, not as two fieldsets
     with one hidden: a hidden control is still submitted, so leaving the
     budget project/head in the document during a recoverable request would
     post two project_id values and let the stale one win. data-when is the
     instant local feedback while the swap is in flight; the server is what
     decides which fieldset exists.

     Nothing in here carries the required attribute. A required control that data-when has
     hidden makes the whole form unsubmittable in Chrome ("not focusable"), and
     validateRequestInput enforces every one of these rules anyway — the
     asterisk is the promise to the reader, the store is the enforcement. */}}
{{define "request_form_fields"}}
{{if eq .Request2.Treatment "recoverable"}}
<fieldset data-when="treatment:recoverable">
  <legend>Recoverable details</legend>
  <div class="form-grid">
    <div class="field span-6 m-half">
      {{if eq .Request2.Type "employee_advance"}}
      {{/* The category is the type's, not a choice (form-1 / recoverables-1). An
           employee advance pays the person raising it, so an EMD, PBG or ICD
           filed under it was a deposit recorded as owed to the employee; those
           are the "Deposit or guarantee" type now, and this form states its
           category the way the vendor invoice states its treatment.
           validateRequestInput refuses any other code for this type. */}}
      <label for="rcategory-fixed">Category</label>
      {{if .RecCategory.Code}}
      <input id="rcategory-fixed" value="{{.RecCategory.Name}}" readonly>
      <input type="hidden" name="recoverable_category" value="{{.RecCategory.Code}}">
      <span class="hint">An employee advance is always in this category. A deposit, guarantee or loan is raised as its own request type.</span>
      {{else}}
      <p id="rcategory-fixed" class="fixed-treatment">The Employee advance category has been retired by your administrator, so an employee advance cannot be recorded as recoverable until it is reactivated.</p>
      {{end}}
      {{else}}
      <label for="rcategory">Category <span class="req" aria-hidden="true">*</span></label>
      {{/* The options are the active category rows, not six literals. V4's promise
           is that an admin can add a category and have its rules enforced without
           a code change; the enforcement shipped and the affordance did not, so a
           new category was unselectable and a deactivated one was still offered
           and only refused after the whole form had been filled in
           (F-E-02/F-B-17). The empty option exists for the same reason: an
           unrecognised code is no longer silently rewritten to "emd", so
           "nothing chosen" is a state the control has to be able to show.
           categoriesForType leaves the Employee advance category out: an advance
           to an employee is the employee advance type, which pays them. */}}
      <select id="rcategory" name="recoverable_category" aria-required="true"
              hx-get="/requests/new/fields" hx-include="closest form" hx-target="#form-fields" hx-trigger="change">
        {{if not .RecCategory.Code}}<option value="" selected>Choose a category</option>{{end}}
        {{range .Categories}}<option value="{{.Code}}" {{select $.Request2.RecoverableCategory .Code}}>{{.Name}}</option>{{end}}
      </select>
      <span class="hint">Categories are maintained by your administrator.</span>
      {{end}}
    </div>
    <div class="field span-6 m-half">
      <label for="expected-return">Expected return date <span class="req" aria-hidden="true">*</span></label>
      <input id="expected-return" type="date" name="expected_return_date" aria-required="true" value="{{.Request2.ExpectedReturnDate}}">
    </div>
    {{/* Which extra fields a category needs is the category's own data, so an
         admin-added one behaves like a seeded one. data-when carries the current
         code rather than a hardcoded pair: the field stays while the select still
         reads what the server rendered and disappears the instant it changes,
         until the htmx swap arrives with the right fieldset. */}}
    {{/* Every category may link a project (design §4: a recoverable "may still
         link to a real project"); only EMD/PBG must. The field used to exist
         solely inside the RequiresProject branch, so an ICD or a security deposit
         could never be linked and read "Not project linked" in the register with
         no way to say otherwise (recoverables-7). Required and its hint follow
         the category's own flag. Absent a category there is nothing to say yet. */}}
    {{if .RecCategory.Code}}
    <div class="field span-6 m-half" data-when="recoverable_category:{{.RecCategory.Code}}">
      <label for="rproject">Related project {{if .RecCategory.RequiresProject}}<span class="req" aria-hidden="true">*</span>{{else}}<span class="opt" aria-hidden="true">optional</span>{{end}}</label>
      <select id="rproject" name="project_id" {{if .RecCategory.RequiresProject}}aria-required="true"{{end}}>
        <option value="">{{if .RecCategory.RequiresProject}}Choose a project{{else}}Not linked to a project{{end}}</option>
        {{range .Projects}}<option value="{{.ID}}" {{if eq (deref $.Request2.ProjectID) .ID}}selected{{end}}>{{.Name}}{{if not .Active}} (retired){{end}}</option>{{end}}
      </select>
      <span class="hint">{{if .RecCategory.RequiresProject}}{{.RecCategory.Name}} always belongs to a project.{{else}}Link it if the money belongs to a project. It stays out of that project's budget actuals either way.{{end}}</span>
    </div>
    {{end}}
    {{if .RecCategory.RequiresCounterparty}}
    <div class="field span-6 m-half" data-when="recoverable_category:{{.RecCategory.Code}}">
      <label for="counterparty">Counterparty company <span class="req" aria-hidden="true">*</span></label>
      <input id="counterparty" name="counterparty" aria-required="true" value="{{.Request2.Counterparty}}" placeholder="Company receiving the deposit">
    </div>
    {{end}}
    <div class="field span-12">
      <label for="terms">Repayment or refund terms <span class="req" aria-hidden="true">*</span></label>
      <textarea id="terms" name="repayment_notes" aria-required="true">{{.Request2.RepaymentNotes}}</textarea>
    </div>
    <div class="field span-12">
      <div class="banner brand" style="margin:0">
        <span class="b-ico" aria-hidden="true">↩</span>
        <div>
          <b>This will not touch budget actuals</b>
          <p>It appears in Recoverable payments instead.</p>
        </div>
      </div>
    </div>
  </div>
</fieldset>
{{else}}
<fieldset data-when="treatment:budget">
  <legend>Charge it to</legend>
  <div class="form-grid">
    <div class="field span-6 m-half">
      <label for="project">Project <span class="req" aria-hidden="true">*</span></label>
      <select id="project" name="project_id" aria-required="true"
              hx-get="/requests/new/fields" hx-include="closest form" hx-target="#form-fields" hx-trigger="change">
        <option value="">Choose a project</option>
        {{/* "(retired)" only ever appears on the edit form: offerRetiredRefs adds
             the request's own project or head back when it was retired after the
             request was raised, so the form shows what is charged today (T12,
             form-3). The new-request lists are the active set and carry none. */}}
        {{range .Projects}}<option value="{{.ID}}" {{if eq (deref $.Request2.ProjectID) .ID}}selected{{end}}>{{.Name}}{{if not .Active}} (retired){{end}}</option>{{end}}
      </select>
      <span class="hint">Picking a project narrows the heads below to that project's own.</span>
    </div>
    <div class="field span-6 m-half">
      <label for="head">Head <span class="req" aria-hidden="true">*</span></label>
      <select id="head" name="head_id" aria-required="true">
        <option value="">Choose a head</option>
        {{range .Heads}}{{if or (not (deref $.Request2.ProjectID)) (eq .ProjectID (deref $.Request2.ProjectID))}}<option value="{{.ID}}" {{if eq (deref $.Request2.HeadID) .ID}}selected{{end}}>{{.Project}} / {{.Name}}{{if not .Active}} (retired){{end}}</option>{{end}}{{end}}
      </select>
    </div>
  </div>
</fieldset>
{{end}}
{{end}}

{{/* The adaptive request form — mockups/screens/request-new-form.html.

     Step 2 of 2. The type is fixed by the route and travels as one hidden
     input; treatment and recoverable category swap #form-fields through htmx.
     There is exactly one submit button and exactly one POST: D1 removed drafts,
     so creating and submitting are the same act and the request takes its
     number at that moment.

     No bank or account field appears here or on any other request screen —
     bank details live on the vendor record behind vendor_bank (Phase 1V). */}}
{{define "request_form"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">New request · step 2 of 2</div>
    <h1>{{.Title}}</h1>
    <p class="sub">One form that changes with what you pick. Nothing is saved until you submit.</p>
  </div>
  <div class="pb-actions"><a class="btn outline" href="/requests/new">← Change type</a></div>
</section>

<form method="post" enctype="multipart/form-data" action="/requests">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <input type="hidden" name="type" value="{{.FormType}}">

  <fieldset>
    <legend>What is this for</legend>
    <div class="form-grid">
      <div class="field span-12">
        <label for="short-title">Short title <span class="req" aria-hidden="true">*</span></label>
        <input id="short-title" name="short_title" value="{{.Request2.ShortTitle}}" required>
        <span class="hint">What your approver will see in their approval list.</span>
      </div>
      {{/* The recoverable treatment is only offered where the store will accept
           it. internal/store/requests.go refuses it for a vendor invoice, a
           vendor advance and a reimbursement, each with its own "... is a budget
           expense" message — but this fieldset sits in the shared top of the
           form, so it used to present the choice on all four types and reject it
           on three of them after the requester had filled in the recoverable
           fields it revealed. Where there is no choice, the form now says so
           instead of offering one. */}}
      <div class="field span-12">
        {{if eq .Request2.Type "employee_advance"}}
        <span class="flabel">How should this be treated <span class="req" aria-hidden="true">*</span></span>
        <div class="choice"
             hx-get="/requests/new/fields" hx-include="closest form" hx-target="#form-fields" hx-trigger="change">
          <label>
            <input type="radio" name="treatment" value="budget" {{if ne .Request2.Treatment "recoverable"}}checked{{end}}>
            <span><b>Budget expense</b><small>Money spent and gone. Counts against a project and head.</small></span>
          </label>
          <label>
            <input type="radio" name="treatment" value="recoverable" {{if eq .Request2.Treatment "recoverable"}}checked{{end}}>
            <span><b>Refundable or recoverable</b><small>A deposit, guarantee, loan or advance you expect back. Kept out of budget actuals.</small></span>
          </label>
        </div>
        {{else if eq .Request2.Type "recoverable"}}
        {{/* A deposit or guarantee is recoverable by definition (form-1): the
             store refuses any other treatment for the type, so the form states
             it the way a vendor invoice states "budget expense". */}}
        <span class="flabel">How this is treated</span>
        <input type="hidden" name="treatment" value="recoverable">
        <p class="fixed-treatment"><b>Refundable or recoverable.</b> A deposit, guarantee or loan the company expects back. Kept out of budget actuals.</p>
        <span class="hint">A deposit or guarantee is always recoverable. Money that is spent and gone is a vendor payment, a reimbursement or a budget employee advance.</span>
        {{else}}
        <span class="flabel">How this is treated</span>
        <input type="hidden" name="treatment" value="budget">
        <p class="fixed-treatment"><b>Budget expense.</b> Money spent and gone. It counts against a project and head.</p>
        <span class="hint">Only an employee advance can be marked refundable or recoverable. A deposit or guarantee is its own request type.</span>
        {{end}}
      </div>
    </div>
  </fieldset>

  <div id="form-fields">{{template "request_form_fields" .}}</div>

  {{if eq .FormType "recoverable"}}
  {{template "request_payee_field" .}}
  {{end}}

  <fieldset>
    <legend>Amount and timing</legend>
    <div class="form-grid">
      <div class="field span-6 money-field">
        <label for="amount">Amount <span class="req" aria-hidden="true">*</span></label>
        <span class="money-wrap"><span class="cur" aria-hidden="true">₹</span><input id="amount" name="amount" inputmode="decimal" value="{{amountValue .Request2.Amount}}" required
          hx-post="/requests/duplicate-check" hx-include="closest form" hx-target="#dup-check" hx-trigger="blur"></span>
        <span class="in-words">{{if .Request2.Amount}}{{inWords .Request2.Amount}}{{else}}Enter the amount you are requesting{{end}}</span>
      </div>
      <div class="field span-6 m-half">
        <label for="needed-by">Needed by</label>
        <input id="needed-by" type="date" name="needed_by" value="{{.Request2.NeededBy}}">
        <span class="hint">Optional. It tells your approver how long they have.</span>
      </div>
      {{if ne (index .Settings "urgency_mode") "disabled"}}
      <div class="field span-12">
        <label class="checkline"><input type="checkbox" name="urgent" {{check .Request2.Urgent}}> Mark this urgent</label>
        <span class="hint">Urgent requests follow the same approval rules. They send an immediate email to your approver, and to Accounts once approved.</span>
      </div>
      {{if eq (index .Settings "urgency_mode") "reason"}}
      <div class="field span-12" data-when="urgent:on" {{if not .Request2.Urgent}}hidden{{end}}>
        <label for="urgency-reason">Why is it urgent <span class="req" aria-hidden="true">*</span></label>
        <input id="urgency-reason" name="urgency_reason" aria-required="true" value="{{.Request2.UrgencyReason}}"
               placeholder="Supply stops if this is not cleared by Monday">
      </div>
      {{end}}
      {{end}}
    </div>
  </fieldset>

  {{if or (eq .FormType "vendor_invoice") (eq .FormType "vendor_advance")}}
  <fieldset>
    <legend>Vendor and {{if eq .FormType "vendor_invoice"}}invoice{{else}}advance{{end}}</legend>
    <div class="form-grid">
      <div class="field span-6">
        <label for="vendor">Vendor <span class="req" aria-hidden="true">*</span></label>
        {{/* The combobox is offered only to somebody who may actually reach
             GET /vendors/search. Everyone else — and every browser with no
             JavaScript — gets the plain select below, which posts the same
             vendor_id. The visible text is never trusted: the hidden id is
             what the server reads, and it comes from the vendor master. */}}
        {{if .Perms.Can "vendor" "view"}}
        <span class="combo">
          <input class="combo-input" id="vendor" name="q" aria-required="true" autocomplete="off" value="{{.Request2.Vendor}}"
                 placeholder="Type a name, GSTIN or city"
                 hx-get="/vendors/search" hx-trigger="input changed delay:250ms" hx-target="#vendor-options">
          <span class="combo-caret" aria-hidden="true">▾</span>
        </span>
        <div id="vendor-options"></div>
        <noscript>
          <label for="vendor-plain">Or pick from the list</label>
          <select id="vendor-plain" name="vendor_id">
            <option value="">Choose a vendor</option>
            {{range .Vendors}}<option value="{{.ID}}" {{if eq (deref $.Request2.VendorID) .ID}}selected{{end}}>{{.Name}}</option>{{end}}
          </select>
        </noscript>
        {{/* Last, so that with scripting off the <noscript> select above is the
             first vendor_id in the body and therefore the one that wins. */}}
        <input type="hidden" name="vendor_id" id="vendor-id" data-combo-value value="{{deref .Request2.VendorID}}">
        <span class="hint">Type to search the vendor master. Bank details stay in the vendor record — never on this form.</span>
        {{else}}
        <select id="vendor" name="vendor_id" aria-required="true">
          <option value="">Choose a vendor</option>
          {{range .Vendors}}<option value="{{.ID}}" {{if eq (deref $.Request2.VendorID) .ID}}selected{{end}}>{{.Name}}</option>{{end}}
        </select>
        <span class="hint">Bank details stay in the vendor record — never on this form.</span>
        {{end}}
      </div>
      {{if eq .FormType "vendor_invoice"}}
      <div class="field span-3 m-half">
        <label for="invoice-no">Invoice number <span class="req" aria-hidden="true">*</span></label>
        <input id="invoice-no" name="invoice_no" value="{{.Request2.InvoiceNo}}" required
               hx-post="/requests/duplicate-check" hx-include="closest form" hx-target="#dup-check" hx-trigger="blur">
      </div>
      <div class="field span-3 m-half">
        <label for="invoice-date">Invoice date <span class="req" aria-hidden="true">*</span></label>
        <input id="invoice-date" type="date" name="invoice_date" value="{{.Request2.InvoiceDate}}" required>
      </div>
      {{else}}
      <div class="field span-6">
        <label for="advance-reason">Reason for the advance <span class="req" aria-hidden="true">*</span></label>
        <input id="advance-reason" name="advance_reason" value="{{.Request2.AdvanceReason}}" required>
      </div>
      {{end}}
    </div>
  </fieldset>
  {{end}}

  {{if eq .FormType "reimbursement"}}
  <fieldset>
    <legend>Your expense</legend>
    <div class="form-grid">
      <div class="field span-6 m-half">
        <label for="paid-to">Paid to</label>
        <input id="paid-to" value="{{.User.Name}}" readonly>
        <span class="hint">Reimbursements always pay the person raising them.</span>
      </div>
      <div class="field span-6 m-half">
        <label for="expense-date">Expense date <span class="req" aria-hidden="true">*</span></label>
        <input id="expense-date" type="date" name="expense_date" value="{{.Request2.ExpenseDate}}" required>
      </div>
    </div>
  </fieldset>
  {{end}}

  {{if eq .FormType "employee_advance"}}
  <fieldset>
    <legend>Advance details</legend>
    <div class="form-grid">
      <div class="field span-6 m-half">
        <label for="adv-to">Paid to</label>
        <input id="adv-to" value="{{.User.Name}}" readonly>
        <span class="hint">An employee advance always pays the person raising it.</span>
      </div>
      <div class="field span-12">
        <label for="adv-reason">What the money is for <span class="req" aria-hidden="true">*</span></label>
        <input id="adv-reason" name="advance_reason" value="{{.Request2.AdvanceReason}}" required>
      </div>
    </div>
  </fieldset>
  {{end}}

  <fieldset>
    <legend>Purpose and documents</legend>
    <div class="form-grid">
      <div class="field span-12">
        <label for="purpose">Purpose <span class="req" aria-hidden="true">*</span></label>
        <textarea id="purpose" name="purpose" required>{{.Request2.Purpose}}</textarea>
      </div>
      <div class="field span-12">
        <span class="flabel">Supporting document {{if eq (index .Settings "require_attachments") "1"}}<span class="req" aria-hidden="true">*</span>{{else}}<span class="opt" aria-hidden="true">optional</span>{{end}}</span>
        <div class="stack-8">
          <div class="uploader">
            <div class="up-ico" aria-hidden="true">⇪</div>
            <label for="attachment"><b>Add invoice, receipt or proof</b></label>
            <small>PDF, JPG or PNG up to {{index .Settings "attachment_max_mb"}} MB</small>
            <input type="file" id="attachment" name="attachment">
          </div>
        </div>
      </div>
      {{if eq (index .Settings "require_attachments") "1"}}
      <div class="field span-12">
        <label for="att-exception">If you cannot attach a document, say why <span class="req" aria-hidden="true">*</span></label>
        <input id="att-exception" name="attachment_exception_reason" aria-required="true" value="{{.Request2.AttachmentExceptionReason}}"
               placeholder="Vendor posts the invoice; it arrives Monday">
        <span class="hint">A missing document never blocks you — it asks for this instead, and your approver sees it.</span>
      </div>
      {{end}}
    </div>
  </fieldset>

  <fieldset>
    <legend>Who approves it</legend>
    <div class="form-grid">
      <div class="field span-6">
        <label for="approver">Approver <span class="req" aria-hidden="true">*</span></label>
        <select id="approver" name="manager_id" required {{if eq (index .Settings "allow_approver_choice") "0"}}disabled{{end}}>
          <option value="">Choose an approver</option>
          {{range .Approvers}}<option value="{{.ID}}" {{if eq $.Request2.ManagerID .ID}}selected{{end}}>{{.Name}}{{if eq $.Request2.ManagerID .ID}} (your default){{end}}</option>{{end}}
        </select>
        {{if eq (index .Settings "allow_approver_choice") "0"}}<input type="hidden" name="manager_id" value="{{.Request2.ManagerID}}">{{end}}
        <span class="hint">You cannot approve your own request. Your own name is never in this list.</span>
      </div>
      <div class="field span-6">
        <span class="flabel">Reminders</span>
        {{/* The number is the configured threshold, not a literal: Phase 5 turned
             this wait into app_settings.reminder_pending_days and left four
             sentences describing the old constant (F-F-03). "days", not "calendar
             days" — Wave 2b deleted calendarDaysBetween deliberately, because a
             calendar boundary needs a timezone this application does not have and
             reminder_repeat_days becomes nightly spam under those semantics, so
             every threshold in the product is elapsed days and the copy says so. */}}
        <p class="hint" style="margin:4px 0 0">If nothing happens for {{.Reminders.PendingAfterDays}} {{plural .Reminders.PendingAfterDays "day" "days"}}, this request starts sending a reminder every {{if eq .Reminders.RepeatEveryDays 1}}day{{else}}{{.Reminders.RepeatEveryDays}} days{{end}} to whoever it is waiting on.</p>
      </div>
    </div>
  </fieldset>

  {{/* Where the advisory duplicate warning lands. It is never on the path
       between the requester and the submit button: an empty answer leaves it
       empty, and a full one only points. */}}
  <div id="dup-check" aria-live="polite">{{if .Similar}}{{template "request_duplicates" .}}{{end}}</div>

  <div class="action-bar">
    <span class="ab-note d-only">Submitting sends it to your approver and creates the request number.</span>
    <span class="row-end"></span>
    <a class="btn outline" href="/">Cancel</a>
    <button class="btn primary" type="submit">Submit request</button>
  </div>
</form>
{{template "bottom" .}}
{{end}}

{{/* Who a deposit or guarantee is paid to — the one type whose payee is free
     and is not a vendor row (form-1 / recoverables-1). An EMD goes to the tender
     authority, an ICD to the counterparty company; the requester names them
     here and Accounts pay that name. It is rendered on the new and the edit form
     alike, and validateRequestInput refuses the type without it: a request that
     names nobody cannot be paid. */}}
{{define "request_payee_field"}}
<fieldset>
  <legend>Who is paid</legend>
  <div class="form-grid">
    <div class="field span-6">
      <label for="payee">Paid to <span class="req" aria-hidden="true">*</span></label>
      <input id="payee" name="vendor_payee" value="{{.Request2.VendorPayee}}" required placeholder="Counterparty company or tender authority"
             hx-post="/requests/duplicate-check" hx-include="closest form" hx-target="#dup-check" hx-trigger="blur">
      <span class="hint">The company or authority that receives the deposit. Never you — money advanced to yourself is an employee advance. Bank details stay with Accounts, never on this form.</span>
    </div>
  </div>
</fieldset>
{{end}}

{{/* One request as a card. Shared by the list, the approvals queue and the
     duplicate warning, so the three describe a request the same way.

     It is called through the "card" function rather than with a bare dot: a
     {{template}} inside a {{range}} rebinds dot to the request, and the viewer
     id the "waiting on" line needs lives on the page. */}}
{{define "request_card"}}
{{$r := .Req}}{{$w := waitingOn $r .ViewerID}}
<a class="req-card{{if $r.Urgent}} is-urgent{{end}}{{if eq $r.RequesterID .ViewerID}} is-mine{{end}}" href="/requests/{{$r.ID}}">
  <span class="rc-top"><span class="rc-no">{{$r.Number}}</span><span class="rc-amt">{{money $r.Amount}}</span></span>
  <span class="rc-title">{{$r.ShortTitle}}</span>
  <span class="rc-meta">
    {{if $r.Urgent}}<span class="pill urgent">Urgent</span> {{end}}
    {{if eq $r.Treatment "recoverable"}}<span class="pill recoverable">Recoverable{{if $r.RecoverableCategory}} · {{recoverable $r.RecoverableCategory}}{{end}}</span> {{end}}
    {{typeLabel $r.Type}}{{if $r.Project}} · {{$r.Project}}{{if $r.Head}} / {{$r.Head}}{{end}}{{end}}{{if $r.InvoiceNo}} · invoice {{$r.InvoiceNo}}{{end}}
  </span>
  <span class="rc-foot">
    {{$p := statusPill $r .ViewerID}}<span class="pill {{$p.Class}}">{{$p.Text}}</span>
    <span class="waiting {{$w.Class}}">{{$w.Text}}</span>
  </span>
</a>
{{end}}

{{/* The mobile filter sheet. It is a plain GET form, so the filters a phone
     applies produce the same URL a desktop toolbar would — one shareable
     address per view, and no state hiding in a drawer. */}}
{{define "request_filter_sheet"}}
<div class="overlay" id="filter-sheet" hidden>
  <form class="sheet" method="get" action="/requests">
    <div class="sh-head">
      <div><h2>Filters</h2><p class="sh-sub">Narrow the list</p></div>
      <button class="sh-close" type="button" data-close="filter-sheet" aria-label="Close">✕</button>
    </div>
    <div class="sh-body stack-12">
      <input type="hidden" name="bucket" value="{{.Bucket}}">
      <div class="field"><label for="fs-ty">Type</label><select id="fs-ty" name="type">
        <option value="">Any type</option>
        {{range requestTypes}}<option value="{{.Key}}" {{select $.TypeFilter .Key}}>{{.Label}}</option>{{end}}
      </select></div>
      <div class="field"><label for="fs-tr">Treatment</label><select id="fs-tr" name="treatment">
        <option value="">Any</option>
        <option value="budget" {{select .Treatment "budget"}}>Budget expense</option>
        <option value="recoverable" {{select .Treatment "recoverable"}}>Recoverable</option>
      </select></div>
      <div class="field"><label for="fs-pr">Project</label><select id="fs-pr" name="project_id">
        <option value="">All projects</option>
        {{range .Projects}}<option value="{{.ID}}">{{.Name}}</option>{{end}}
      </select></div>
      <div class="field"><label for="fs-q">Search</label><input id="fs-q" name="q" value="{{.Query}}"></div>
    </div>
    <div class="sh-foot">
      <a class="btn outline" href="/requests">Clear all</a>
      <span class="row-end"></span>
      <button class="btn primary" type="submit">Show requests</button>
    </div>
  </form>
</div>
{{end}}

{{/* The requests list — mockups/screens/requests-list.html.

     Sorted by who has been kept waiting longest, urgent first, and every card
     ends with the plain sentence naming whoever owes the next action. The
     .segmented tabs are links, not client-side tabs: each is its own URL, so a
     view can be bookmarked and sent to somebody.

     The desktop toolbar and the mobile .m-filters are both plain GET forms
     over the same parameters; the stylesheet hides whichever does not belong
     on the device. */}}
{{define "requests"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Requests</div>
    <h1>{{if eq .Scope "own"}}My requests{{else}}All requests{{end}}</h1>
    {{/* The sub-line names both numbers. The row query was capped at 200 while the
         tab count beside it had none, so the All tab promised 214 and the list drew
         200 with nothing anywhere admitting it (F-B-16). */}}
    <p class="sub">{{if .Page.Truncated}}{{len .Requests}} of {{.Page.Total}} shown{{else}}{{len .Requests}} shown{{end}} · sorted by who is holding them up</p>
  </div>
  <div class="pb-actions">
    <a class="btn outline" href="/requests/export.csv?scope={{.Scope}}&amp;bucket={{.Bucket}}&amp;q={{.Query}}">⤓ Export CSV</a>
    {{if .Perms.Can "request" "create"}}<a class="btn primary" href="/requests/new">＋ New request</a>{{end}}
  </div>
</section>

<form class="toolbar" method="get" action="/requests">
  <input type="hidden" name="bucket" value="{{.Bucket}}">
  <div class="field search"><label for="q">Search</label><input id="q" name="q" value="{{.Query}}" placeholder="Number, payee, invoice, purpose…"></div>
  <div class="field"><label for="ty">Type</label><select id="ty" name="type">
    <option value="">Any type</option>
    {{range requestTypes}}<option value="{{.Key}}" {{select $.TypeFilter .Key}}>{{.Label}}</option>{{end}}
  </select></div>
  <div class="field"><label for="tr">Treatment</label><select id="tr" name="treatment">
    <option value="">Any</option>
    <option value="budget" {{select .Treatment "budget"}}>Budget expense</option>
    <option value="recoverable" {{select .Treatment "recoverable"}}>Recoverable</option>
  </select></div>
  <span class="row-end"></span>
  <button class="btn">Apply</button>
</form>

<form class="m-filters" method="get" action="/requests">
  <input type="hidden" name="bucket" value="{{.Bucket}}">
  <span class="m-search"><input name="q" value="{{.Query}}" placeholder="Search requests…" aria-label="Search requests"></span>
  <button class="btn filter-btn" type="button" data-open="filter-sheet">Filters</button>
</form>

<div class="segmented" style="margin-bottom:12px">
  {{range requestTabs}}<a class="{{if eq $.Bucket .Key}}is-active{{end}}"
    {{if eq $.Bucket .Key}}aria-current="page"{{end}}
    href="/requests?bucket={{.Key}}&amp;q={{$.Query}}">{{.Label}} <span class="n">{{index $.Counts .Key}}</span></a>{{end}}
</div>

<div class="req-list">
  {{range .Requests}}{{template "request_card" (card . $.User.ID)}}{{else}}<p class="empty">No requests match. Try another tab, or clear the filters.</p>{{end}}
</div>

{{/* Paging, so nothing is merely unreachable. The cap is real — a screen does not
     render ten thousand cards — but it now says so and offers the rest, and the
     export carries every row (F-B-16). Both links repeat every filter, so a page
     boundary never silently widens the set. */}}
{{if or .Page.Truncated .Page.Offset}}
<div class="cluster" style="margin-top:14px">
  <span class="muted small">Showing {{if .Requests}}{{add .Page.Offset 1}}–{{add .Page.Offset (len .Requests)}}{{else}}0{{end}} of {{.Page.Total}}</span>
  <span class="row-end"></span>
  {{if .Page.Offset}}<a class="btn outline" href="/requests?bucket={{.Bucket}}&amp;status={{.Status}}&amp;type={{.TypeFilter}}&amp;treatment={{.Treatment}}&amp;q={{urlquery .Query}}&amp;offset={{sub0 .Page.Offset .Page.Limit}}">← Newer</a>{{end}}
  {{if .Page.Truncated}}<a class="btn outline" href="/requests?bucket={{.Bucket}}&amp;status={{.Status}}&amp;type={{.TypeFilter}}&amp;treatment={{.Treatment}}&amp;q={{urlquery .Query}}&amp;offset={{add .Page.Offset (len .Requests)}}">Older →</a>{{end}}
</div>
{{end}}

{{template "request_filter_sheet" .}}
{{template "bottom" .}}
{{end}}

{{/* The merged history-and-conversation stream — one .thread, not a history
     list and a conversation list side by side. Everything that happened to a
     request happened in one order, and that order is the story: an edit sits
     between the comment that asked for it and the approval that followed.

     The comment box is part of the thread rather than a separate screen,
     because asking a question is the alternative to returning a request. */}}
{{define "request_thread"}}
<div class="section-head">
  <h2>History and conversation</h2>
  <span class="small muted">Everyone who can see this request sees this whole stream</span>
</div>
<ol class="thread">
  {{range .Thread}}
  <li{{if eq .Kind "comment"}} class="is-comment{{if eq .ActorID $.User.ID}} is-me{{end}}"{{end}}>
    <span class="tl-dot {{threadDot .}}" aria-hidden="true">{{threadGlyph .}}</span>
    <div class="tl-head"><b>{{if eq .Kind "comment"}}{{.ActorName}}{{else}}{{.Title}}{{end}}</b><time>{{date .CreatedAt}}</time></div>
    <div class="tl-body">
      {{if eq .Kind "comment"}}<p>{{.Body}}</p>{{end}}
      {{if eq .Kind "attachment"}}<span class="tl-file">📎 {{.FileName}} · {{fileSize .FileSize}}</span>{{end}}
      {{if .Changes}}<div class="tl-change">{{range .Changes}}{{threadField .Field}} <span class="was">{{threadValue .Field .Was}}</span> → <span class="now">{{threadValue .Field .Now}}</span><br>{{end}}</div>{{end}}
    </div>
  </li>
  {{else}}<li><span class="tl-dot" aria-hidden="true">·</span><div class="tl-body muted">Nothing has happened yet.</div></li>{{end}}
</ol>

{{if .Perms.Can "request" "comment"}}
<form class="comment-box" method="post" action="/requests/{{.Request2.ID}}/comment">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <label for="cmt" class="flabel">Add a comment</label>
  <textarea id="cmt" name="body" placeholder="Anyone who can see this request will see your comment." required></textarea>
  <div class="cb-actions"><span class="row-end"></span><button class="btn primary small" type="submit">Post comment</button></div>
</form>
{{end}}
{{end}}

{{/* The approver's three decisions, each in its own .overlay > .sheet. None
     of them is a bare inline button: returning and rejecting demand words, and
     approving offers an amount, so each needs a moment and a form of its own.

     The amount uses amountValue, not money: the .money-field draws its own ₹
     in the .cur prefix and money.FormatPaise already carries one. */}}
{{define "request_sheets"}}
<div class="overlay" id="approve-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/approve">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="sh-head"><div><h2>Approve {{money .Request2.Amount}}?</h2><p class="sh-sub">{{.Request2.Number}} · {{.Request2.Vendor}}</p></div><button class="sh-close" type="button" data-close="approve-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="field money-field">
        <label for="ap-amount">Amount approved</label>
        <span class="money-wrap"><span class="cur" aria-hidden="true">₹</span><input id="ap-amount" name="approved_amount" inputmode="decimal" value="{{amountValue .Request2.Amount}}" required></span>
        <span class="in-words">{{inWords .Request2.Amount}}</span>
        <span class="hint">You may approve a smaller amount than was asked for.</span>
      </div>
      <div class="field"><label for="ap-note">Note <span class="opt" aria-hidden="true">optional</span></label><textarea id="ap-note" name="note" placeholder="Recorded in the history and visible to everyone."></textarea></div>
      <p class="hint" style="margin:0">Accounts will be able to reserve this immediately. {{.Request2.RequesterName}} can no longer edit it.</p>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="approve-sheet">Cancel</button><span class="row-end"></span><button class="btn primary" type="submit">Approve request</button></div>
  </form>
</div>

<div class="overlay" id="return-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/return">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="sh-head"><div><h2>Return for correction</h2><p class="sh-sub">{{.Request2.RequesterName}} can edit and resubmit. The number and history stay.</p></div><button class="sh-close" type="button" data-close="return-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="field"><label for="rt-reason">What needs correcting <span class="req" aria-hidden="true">*</span></label><textarea id="rt-reason" name="comment" required placeholder="Be specific — this is the whole message they get."></textarea></div>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="return-sheet">Cancel</button><span class="row-end"></span><button class="btn primary" type="submit">Return request</button></div>
  </form>
</div>

<div class="overlay" id="reject-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/reject">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="sh-head"><div><h2>Reject this request?</h2><p class="sh-sub">Rejection is final and read-only. {{.Request2.RequesterName}} would have to raise a new request.</p></div><button class="sh-close" type="button" data-close="reject-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="banner bad" style="margin:0"><span class="b-ico" aria-hidden="true">!</span><div><b>This cannot be undone</b><p>If the request is fixable, return it for correction instead.</p></div></div>
      <div class="field"><label for="rj-reason">Reason for rejection <span class="req" aria-hidden="true">*</span></label><textarea id="rj-reason" name="reason" required></textarea></div>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="reject-sheet">Cancel</button><span class="row-end"></span><button class="btn danger" type="submit">Reject permanently</button></div>
  </form>
</div>
{{end}}

{{/* The request head — number, amount, title, status and the plain sentence
     naming whoever owes the next action. Shared by every screen that is about
     one request, so the four of them cannot describe it four ways. */}}
{{define "request_head"}}
<div class="req-head">
  <div class="rh-top">
    <span class="rh-no">{{.Request2.Number}}</span>
    <span class="rh-amt">{{money .Request2.Amount}}</span>
  </div>
  <h1>{{.Request2.ShortTitle}}</h1>
  <p class="rh-meta">{{typeLabel .Request2.Type}}{{if .Request2.Project}} · {{.Request2.Project}}{{if .Request2.Head}} / {{.Request2.Head}}{{end}}{{end}} · raised by {{.Request2.RequesterName}} on {{date .Request2.CreatedAt}}</p>
  <div class="rh-status">
    {{if .Request2.Urgent}}<span class="pill urgent">Urgent</span>{{end}}
    {{/* L7: a hold leaves the status 'approved' so the queue's availability
         test drops it out of the takeable set without a status of its own.
         The pill has to say what is true anyway — a screen reading "Approved —
         awaiting payment" over a banner saying payment is blocked is a lie
         either the reader or the accountant acts on. */}}
    {{if activeHold .Request2}}<span class="pill hold">On hold</span>{{else}}{{$p := statusPill .Request2 .User.ID}}<span class="pill {{$p.Class}}">{{$p.Text}}</span>{{end}}
    {{$w := waitingOn .Request2 .User.ID}}<span class="waiting {{$w.Class}}">{{$w.Text}}</span>
  </div>
</div>
{{end}}

{{/* One request, every audience — mockups/screens/request-detail-employee.html
     and request-detail-manager.html are the same page.

     Only the .action-bar changes between them, and it changes on permission
     plus the reader's seat on this row: whether they raised it, whether they
     are the approver it was sent to. Nothing here compares a role name, and
     nothing depends on which link the reader followed to get here. */}}
{{define "request_detail"}}
{{template "top" .}}
{{template "request_head" .}}

{{/* Not while the request is on hold. mockups/screens/request-on-hold.html
     carries exactly one banner in that state, and stacking this one above it
     tells the requester that "payment freezes while your approver decides" when
     the approver is deciding nothing — Accounts is holding it, and the banner
     below says so. */}}
{{if and (eq .Request2.RequesterID .User.ID) (eq .Request2.Status "approved") (not (activeHold .Request2))}}
<div class="banner locked">
  <span class="b-ico" aria-hidden="true">🔒</span>
  <div>
    <b>Approved requests are locked</b>
    <p>You can no longer edit this. If it should not be paid, ask for it to be cancelled — payment
      freezes while your approver decides.</p>
  </div>
</div>
{{end}}
{{/* L7 — mockups/screens/request-on-hold.html. The hold is a state of this
     screen rather than a screen of its own: the question Accounts asked belongs
     where the requester already reads everything about their request, and they
     answer it in the comment box the thread already carries. */}}
{{if activeHold .Request2}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">⏸</span>
  <div>
    <b>This request is on hold</b>
    <p>“{{.Request2.HoldReason}}”</p>
    <p>Payment is blocked until Accounts lifts the hold.</p>
    {{/* Q6: the answer may be a document as well as a comment. The edit path is
         closed once a request is approved, so this is the requester's only way to
         supply what Accounts asked for (hold-1). The route refuses anybody but the
         requester, which is why only they see it. */}}
    {{if and (eq .Request2.RequesterID .User.ID) (.Perms.Can "attachment" "create")}}
    <form class="cluster" method="post" enctype="multipart/form-data" action="/requests/{{.Request2.ID}}/attachments" style="margin-top:8px">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <label class="field" for="hold-attachment" style="margin:0"><span class="flabel">Add a document</span><input type="file" id="hold-attachment" name="attachment" required></label>
      <button class="btn small primary" type="submit">Attach it</button>
    </form>
    {{end}}
  </div>
</div>
{{end}}
{{/* settlement-8: the concern is shown where Accounts reads the request, and
     the holder is told in as many words that the answer is theirs to give. */}}
{{if .Request2.ConcernOpen}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">?</span>
  <div>
    <b>{{.Request2.ManagerName}} raised a concern about the partial payment</b>
    <p>{{if and .Request2.ProcessingBy (eq (deref .Request2.ProcessingBy) .User.ID)}}Answer it in the conversation below; the review returns to {{.Request2.ManagerName}} once you do.{{else}}Accounts is expected to answer it in the conversation; {{.Request2.ManagerName}} then decides.{{end}}</p>
  </div>
</div>
{{end}}
{{if eq .Request2.Status "cancellation_requested"}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">⏸</span>
  <div>
    <b>Payment is frozen</b>
    <p>No accountant can reserve or pay this request until {{.Request2.ManagerName}} decides.
      Reason given: {{.Request2.CancelReason}}</p>
  </div>
</div>
{{end}}
{{if and (eq .Request2.Status "returned") .Request2.DecisionReason}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">↩</span>
  <div><b>{{.Request2.ManagerName}} sent this back</b><p>“{{.Request2.DecisionReason}}”</p></div>
</div>
{{end}}
{{if and (eq .Request2.Status "rejected") .Request2.DecisionReason}}
<div class="banner bad">
  <span class="b-ico" aria-hidden="true">✕</span>
  <div><b>{{.Request2.ManagerName}} rejected this</b><p>“{{.Request2.DecisionReason}}”</p></div>
</div>
{{end}}
{{if .Request2.AttachmentExceptionReason}}
<div class="banner info">
  <span class="b-ico" aria-hidden="true">i</span>
  <div><b>No document was attached</b><p>{{.Request2.AttachmentExceptionReason}}</p></div>
</div>
{{end}}

<div class="card">
  <div class="card-head"><h2>Details</h2><span class="pill neutral no-dot">{{if eq .Request2.Treatment "recoverable"}}Recoverable{{if .Request2.RecoverableCategory}} · {{recoverable .Request2.RecoverableCategory}}{{end}}{{else}}Budget expense{{end}}</span></div>
  <dl class="dl">
    <div><dt>Amount</dt><dd class="big">{{money .Request2.Amount}}</dd></div>
    {{if .Request2.ApprovedAmount}}<div><dt>Approved</dt><dd class="big">{{money (deref .Request2.ApprovedAmount)}}</dd></div>{{end}}
    {{if .Request2.NeededBy}}<div><dt>Needed by</dt><dd>{{dateLong .Request2.NeededBy}}</dd></div>{{end}}
    {{if .Request2.Project}}<div><dt>Project</dt><dd>{{.Request2.Project}}</dd></div>{{end}}
    {{if .Request2.Head}}<div><dt>Head</dt><dd>{{.Request2.Head}}</dd></div>{{end}}
    <div><dt>{{if .Request2.VendorID}}Vendor{{else}}Paid to{{end}}</dt><dd>{{if and .Request2.VendorID (.Perms.Can "vendor" "view")}}<a href="/vendors/{{deref .Request2.VendorID}}">{{.Request2.Vendor}}</a>{{else}}{{.Request2.Vendor}}{{end}}</dd></div>
    {{if .Request2.VendorGSTIN}}<div><dt>GSTIN</dt><dd class="num">{{.Request2.VendorGSTIN}}</dd></div>{{end}}
    {{if .Request2.InvoiceNo}}<div><dt>Invoice number</dt><dd class="num">{{.Request2.InvoiceNo}}</dd></div>{{end}}
    {{if .Request2.InvoiceDate}}<div><dt>Invoice date</dt><dd>{{dateLong .Request2.InvoiceDate}}</dd></div>{{end}}
    {{if .Request2.ExpenseDate}}<div><dt>Expense date</dt><dd>{{dateLong .Request2.ExpenseDate}}</dd></div>{{end}}
    {{if .Request2.AdvanceReason}}<div><dt>Advance reason</dt><dd>{{.Request2.AdvanceReason}}</dd></div>{{end}}
    {{if .Request2.Counterparty}}<div><dt>Counterparty</dt><dd>{{.Request2.Counterparty}}</dd></div>{{end}}
    {{if .Request2.ExpectedReturnDate}}<div><dt>Expected return</dt><dd>{{dateLong .Request2.ExpectedReturnDate}}</dd></div>{{end}}
    {{if .Request2.RepaymentNotes}}<div><dt>Repayment terms</dt><dd>{{.Request2.RepaymentNotes}}</dd></div>{{end}}
    <div><dt>Requested by</dt><dd>{{.Request2.RequesterName}}</dd></div>
    <div><dt>Approver</dt><dd>{{.Request2.ManagerName}}</dd></div>
    <div><dt>Urgency</dt><dd>{{if .Request2.Urgent}}Urgent — {{.Request2.UrgencyReason}}{{else}}Normal{{end}}</dd></div>
    <div style="grid-column:1/-1"><dt>Purpose</dt><dd>{{.Request2.Purpose}}</dd></div>
  </dl>
</div>

{{/* Q4: the outcome, on the request, for the person who raised it. Approved
     beside paid, and the difference named — a settled shortfall is agreed, an
     unsettled one is still owed, and the two must never read the same. */}}
{{if .Payment.ID}}
<div class="section-head"><h2>Payment outcome</h2></div>
<div class="compare" style="margin-bottom:14px">
  <div class="cmp-row"><span class="l">Approved</span><span class="v">{{money (approvedOf .Request2)}}</span></div>
  <div class="cmp-row"><span class="l">Paid on {{dateLong .Payment.PaidOn}}</span><span class="v">{{money .Payment.Amount}}</span></div>
  {{/* The status decides the row, not the settlement alone: an accepted
       shortfall keeps settlement="partial", and "still owed" under
       "Completed — partial accepted · Nothing pending" contradicted the
       manual's own meaning of the row (partial-1). */}}
  {{if eq .Request2.Status "completed_partial"}}
  <div class="cmp-row"><span class="l">Balance written off · shortfall accepted by {{.Request2.ManagerName}}</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>
  {{else if eq .Payment.Settlement "partial"}}
  <div class="cmp-row diff"><span class="l">Still owed to the payee</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>
  {{else}}
  <div class="cmp-row match"><span class="l">Difference · confirmed settled by Accounts</span><span class="v">{{money (sub (approvedOf .Request2) .Payment.Amount)}}</span></div>
  {{end}}
</div>
{{if .Payment.PartialReason}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">i</span>
  <div><b>{{.Payment.EnteredByName}} marked this a genuine partial payment</b><p>{{.Payment.PartialReason}}</p></div>
</div>
{{end}}
{{end}}

<div class="section-head"><h2>Attachments</h2><span class="small muted">{{len .RequestAtts}} {{plural (len .RequestAtts) "file" "files"}}</span></div>
<div class="stack-8">
  {{/* The request's own route, not /attachments/{id}: that one reads
       payment_attachments and served a stranger's payment proof under this
       document's name (F-A-05/F-B-09). */}}
  {{range .RequestAtts}}
  <div class="file-row">
    <span class="f-ico" aria-hidden="true">{{fileKind .OriginalName}}</span>
    <span><b>{{.OriginalName}}</b><small>{{fileSize .SizeBytes}} · added {{date .CreatedAt}}</small></span>
    {{if $.Perms.Can "attachment" "view"}}<span class="f-actions"><a class="btn small outline" href="/requests/{{$.Request2.ID}}/attachments/{{.ID}}">Download</a></span>{{end}}
  </div>
  {{else}}<p class="empty">No documents attached.</p>{{end}}
</div>

{{template "request_thread" .}}

{{$mine := eq .Request2.RequesterID .User.ID}}{{$mineToDecide := eq .Request2.ManagerID .User.ID}}
{{$mayHold := .Perms.Can "payment" "hold"}}{{$held := activeHold .Request2}}
<div class="action-bar">
  {{/* The hold's own note. The mockup gives the hold state a bar of its own;
       there is only ever one bar on this screen, so the note joins this one. */}}
  {{if $held}}{{if $mayHold}}<span class="ab-note d-only">Releasing returns it to Approved — awaiting payment.</span>{{else}}<span class="ab-note d-only">Only Accounts can take this off hold.</span>{{end}}{{end}}
  <span class="row-end"></span>
  {{/* One action bar, not two: at phone width it is sticky to the bottom of the
       viewport, so a second one would sit on top of this. The payment link
       therefore joins the bar rather than bringing its own. */}}
  {{if and .Payment.ID (.Perms.Can "payment" "view")}}
    <a class="btn outline" href="/payments/{{.Payment.ID}}">View the payment</a>
  {{end}}
  {{if and $mine (or (eq .Request2.Status "pending") (eq .Request2.Status "returned")) (.Perms.Can "request" "edit")}}
    <a class="btn outline" href="/requests/{{.Request2.ID}}/edit">Edit request</a>
  {{end}}
  {{if and $mine (eq .Request2.Status "pending") (.Perms.Can "request" "withdraw")}}
    <form method="post" action="/requests/{{.Request2.ID}}/withdraw"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="btn outline" type="submit">Withdraw</button></form>
  {{end}}
  {{if and $mine (eq .Request2.Status "approved") (.Perms.Can "request" "cancel")}}
    <a class="btn outline" href="/requests/{{.Request2.ID}}/cancel">Request cancellation</a>
  {{end}}
  {{if and $mine (eq .Request2.Status "rejected") (.Perms.Can "request" "reraise")}}
    <form method="post" action="/requests/{{.Request2.ID}}/reraise"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="btn primary" type="submit">Raise it again</button></form>
  {{end}}
  {{if and $mineToDecide (eq .Request2.Status "pending")}}
    {{if .Perms.Can "approval" "reject"}}<button class="btn danger outline" type="button" data-open="reject-sheet">Reject</button>{{end}}
    {{if .Perms.Can "approval" "return"}}<button class="btn outline" type="button" data-open="return-sheet">Return for correction</button>{{end}}
    {{if .Perms.Can "approval" "approve"}}<button class="btn primary" type="button" data-open="approve-sheet">Approve {{money .Request2.Amount}}</button>{{end}}
  {{end}}
  {{if and $mineToDecide (eq .Request2.Status "approved") (.Perms.Can "approval" "cancel")}}
    <a class="btn danger outline" href="/requests/{{.Request2.ID}}/cancellation">Cancel with reason</a>
  {{end}}
  {{if and (eq .Request2.Status "cancellation_requested") $mineToDecide (.Perms.Can "approval" "cancel")}}
    <a class="btn primary" href="/requests/{{.Request2.ID}}/cancellation">Decide the cancellation</a>
  {{end}}
  {{/* S11: the partial review is the manager's screen, and the Accounts queue
       that also links to it is gated on payment:process — a verb a manager does
       not hold. Without this the decider has no way in. */}}
  {{if and (eq .Request2.Status "partial_review") $mineToDecide (.Perms.Can "approval" "accept_partial")}}
    <a class="btn primary" href="/requests/{{.Request2.ID}}/partial-review">Decide the partial payment</a>
  {{end}}
  {{/* L7: only Accounts holds and only Accounts lifts. "Keep on hold" is the
       screen itself — the accountant read the answer and decided it was not
       enough — so it links here rather than posting anything. */}}
  {{if and $held $mayHold}}
    <a class="btn outline" href="/requests/{{.Request2.ID}}">Keep on hold</a>
    <form method="post" action="/requests/{{.Request2.ID}}/unhold"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="btn primary" type="submit">Release hold</button></form>
  {{end}}
  {{if and (not $held) (eq .Request2.Status "approved") $mayHold}}
    <button class="btn outline" type="button" data-open="hold-sheet">Put on hold</button>
  {{end}}
  {{/* Phase-3 spec §6: "Record payment" on the request itself, when it is
       approved · unclaimed · not on hold, posting to the one reservation route
       the queue and the picker already use (S1/S2). The third entry point the
       spec asked for, never built (settlement-4). */}}
  {{if and (not $held) (eq .Request2.Status "approved") (not .Request2.ProcessingBy) (.Perms.Can "reservation" "reserve")}}
    <form method="post" action="/requests/{{.Request2.ID}}/record-payment"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="btn primary" type="submit">Record payment</button></form>
  {{end}}
  {{/* A7 — hand the approval to a different approver, with a reason and a history
       entry. store.ReassignRequest carried every rule and had no door until Wave 3
       built POST /requests/{id}/reassign-approver, and this is the control for it
       (F-A-06/F-C-02). It is the recovery path for a request routed to somebody who
       cannot decide it — a deactivated approver (F-G-025), or one who lost
       approval:approve (F-A-08). CanReassignApprover is mayReassignApprover's
       answer — the request's own approver, or a holder of user:edit rescuing it —
       in a status the store will move; the route asks the same question, so a
       manager who is not this request's approver is offered nothing (rbac-8). */}}
  {{if and .CanReassignApprover .Approvers}}
    <button class="btn outline" type="button" data-open="reassign-approver-sheet">Reassign approval</button>
  {{end}}
</div>

{{if and .CanReassignApprover .Approvers}}
<div class="overlay" id="reassign-approver-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/reassign-approver">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="sh-head"><div><h2>Reassign {{.Request2.Number}}</h2><p class="sh-sub">Currently with {{.Request2.ManagerName}}</p></div><button class="sh-close" type="button" data-close="reassign-approver-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="field">
        <label for="ra-manager">Send it to <span class="req" aria-hidden="true">*</span></label>
        {{/* The list is ListApprovers, so every name in it holds approval:approve
             and none of them is the requester — the same two rules the store
             re-checks, which is what keeps the control from offering a target the
             POST would refuse. */}}
        <select id="ra-manager" name="manager_id" required>
          <option value="">Choose an approver</option>
          {{/* The reader's own name is disabled as well as the current approver's:
               nobody hands a request to themselves, and the store refuses it. */}}
          {{range .Approvers}}<option value="{{.ID}}" {{if or (eq $.Request2.ManagerID .ID) (eq $.User.ID .ID)}}disabled{{end}}>{{.Name}}</option>{{end}}
        </select>
      </div>
      <div class="field"><label for="ra-reason">Why <span class="req" aria-hidden="true">*</span></label><textarea id="ra-reason" name="reason" required placeholder="Recorded in the history and visible to everyone who can see this request."></textarea></div>
      <p class="hint" style="margin:0">The request keeps its number and its history. The new approver is told, and the reminder clock starts again.</p>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="reassign-approver-sheet">Back</button><span class="row-end"></span><button class="btn primary" type="submit">Reassign</button></div>
  </form>
</div>
{{end}}

{{/* Placing a hold has no mockup of its own — the approved design jumps
     straight to the on-hold state — so the reason is captured in the same
     .overlay > .sheet every other reason on this screen uses. The store
     refuses an empty reason, which is what makes the asterisk true. */}}
{{if and (not $held) (eq .Request2.Status "approved") $mayHold}}
<div class="overlay" id="hold-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/hold">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="sh-head"><div><h2>Put {{.Request2.Number}} on hold</h2><p class="sh-sub">Payment is blocked until Accounts lifts it</p></div><button class="sh-close" type="button" data-close="hold-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="field"><label for="hold-reason">What do you need from the requester <span class="req" aria-hidden="true">*</span></label><textarea id="hold-reason" name="reason" required placeholder="They see this exactly as you write it."></textarea></div>
      <p class="hint" style="margin:0">{{.Request2.RequesterName}} is asked to answer. Nobody in Accounts can reserve or pay this until you release it.</p>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="hold-sheet">Back</button><span class="row-end"></span><button class="btn primary" type="submit">Put on hold</button></div>
  </form>
</div>
{{end}}

{{if and $mineToDecide (eq .Request2.Status "pending")}}{{template "request_sheets" .}}{{end}}
{{template "bottom" .}}
{{end}}

{{/* The vendor control, shared by the new-request form and the edit screen so
     the two cannot disagree about what a vendor is. The combobox is offered
     only to somebody who may actually reach GET /vendors/search; everyone else,
     and every browser with no JavaScript, gets the plain select. The visible
     text is never trusted — the hidden id is what the server reads. */}}
{{define "request_vendor_field"}}
<div class="field span-6">
  <label for="vendor">Vendor <span class="req" aria-hidden="true">*</span></label>
  {{if .Perms.Can "vendor" "view"}}
  <span class="combo">
    {{/* name="q", as the create form's identical control has. htmx sends the
         triggering element's own name/value pair with hx-get, so an unnamed input
         sent nothing at all: vendorSearch read q="", SearchVendors("") returns nil,
         and the fragment rendered only its placeholder row for every keystroke
         forever — leaving the vendor on a pending invoice unchangeable except by
         withdrawing and raising it again (F-B-13). The visible text is still never
         trusted; #vendor-id is what the server reads. */}}
    <input class="combo-input" id="vendor" name="q" aria-required="true" autocomplete="off" value="{{.Request2.Vendor}}"
           placeholder="Type a name, GSTIN or city"
           hx-get="/vendors/search" hx-trigger="input changed delay:250ms" hx-target="#vendor-options">
    <span class="combo-caret" aria-hidden="true">▾</span>
  </span>
  <div id="vendor-options"></div>
  <noscript>
    <label for="vendor-plain">Or pick from the list</label>
    <select id="vendor-plain" name="vendor_id">
      <option value="">Choose a vendor</option>
      {{range .Vendors}}<option value="{{.ID}}" {{if eq (deref $.Request2.VendorID) .ID}}selected{{end}}>{{.Name}}</option>{{end}}
    </select>
  </noscript>
  <input type="hidden" name="vendor_id" id="vendor-id" data-combo-value value="{{deref .Request2.VendorID}}">
  <span class="hint">Bank details stay in the vendor record — never on this form.</span>
  {{else}}
  <select id="vendor" name="vendor_id" aria-required="true">
    <option value="">Choose a vendor</option>
    {{range .Vendors}}<option value="{{.ID}}" {{if eq (deref $.Request2.VendorID) .ID}}selected{{end}}>{{.Name}}</option>{{end}}
  </select>
  <span class="hint">Bank details stay in the vendor record — never on this form.</span>
  {{end}}
</div>
{{end}}

{{/* The correction screen — mockups/screens/request-edit.html.

     Its whole job is to promise, before the person commits, the three things
     the store already does: record the change in the history, tell the approver
     again, and restart the reminder clock, whose length is the configured
     reminder_pending_days (F-F-03). Changing the approver
     moves the request to that person instead.

     The type is not editable. It is what the request *is*, and it travels as
     the same hidden input the new-request form carries (A16). */}}
{{/* form_warnings renders every caution a form carries, one banner each with
     its own heading, so a legacy-category notice is never titled with a
     retirement it does not have. The legacy notice links to the deposit form it
     points the reader at. */}}
{{define "form_warnings"}}
{{range .Warnings}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">!</span>
  <div><b>{{.Title}}</b><p>{{.Body}}</p></div>
</div>
{{end}}
{{if .LegacyCategory}}
<p class="hint">Raise a deposit or guarantee at <a href="/requests/new?type=recoverable">New request → Deposit or guarantee</a>; it is paid to the counterparty you name.</p>
{{end}}
{{end}}

{{define "request_edit"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">{{.Request2.Number}} · {{reqStatus .Request2.Status}}</div>
    <h1>Edit request</h1>
    <p class="sub">{{.Request2.ShortTitle}}</p>
  </div>
</section>

<div class="banner info">
  <span class="b-ico" aria-hidden="true">i</span>
  <div>
    {{/* Both labels that name the approver follow the #apr select (rbac-10):
         the request moves to whoever is chosen, so a label fixed at render
         time named somebody the save would not notify. */}}
    <b data-follows-select="apr" data-follows-text="Editing tells {name} again">Editing tells {{.Request2.ManagerName}} again</b>
    <p>Every change is recorded in the history, notifies your approver, and restarts the
      {{.Reminders.PendingAfterDays}}-{{plural .Reminders.PendingAfterDays "day" "day"}} reminder clock. Change the approver and it moves to that person instead.</p>
  </div>
</div>

{{/* Each warning carries its own heading. T12 (form-3): a project or head
     retired after the request was raised is offered back in the selects below,
     marked, and its warning says why it has to be chosen again — the store still
     refuses a retired head, and the refusal used to be "project and head are
     required", about a request that had both. form-1: a legacy deposit filed as
     an employee advance says what saving does before the press. */}}
{{template "form_warnings" .}}

<form method="post" enctype="multipart/form-data" action="/requests/{{.Request2.ID}}/edit">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <input type="hidden" name="type" value="{{.FormType}}">
  <input type="hidden" name="treatment" value="{{.Request2.Treatment}}">

  <fieldset>
    <legend>What is this for</legend>
    <div class="form-grid">
      <div class="field span-12">
        <label for="short-title">Short title <span class="req" aria-hidden="true">*</span></label>
        <input id="short-title" name="short_title" value="{{.Request2.ShortTitle}}" required>
      </div>
    </div>
  </fieldset>

  <div id="form-fields">{{template "request_form_fields" .}}</div>

  {{if eq .FormType "recoverable"}}
  {{template "request_payee_field" .}}
  {{end}}

  <fieldset>
    <legend>Amount and timing</legend>
    <div class="form-grid">
      <div class="field span-6 money-field">
        <label for="amount">Amount <span class="req" aria-hidden="true">*</span></label>
        <span class="money-wrap"><span class="cur" aria-hidden="true">₹</span><input id="amount" name="amount" inputmode="decimal" value="{{amountValue .Request2.Amount}}" required></span>
        <span class="in-words">{{if .Request2.Amount}}{{inWords .Request2.Amount}}{{else}}Enter the amount you are requesting{{end}}</span>
      </div>
      <div class="field span-6 m-half">
        <label for="needed">Needed by</label>
        <input id="needed" type="date" name="needed_by" value="{{.Request2.NeededBy}}">
      </div>
      {{if ne (index .Settings "urgency_mode") "disabled"}}
      <div class="field span-12">
        <label class="checkline"><input type="checkbox" name="urgent" {{check .Request2.Urgent}}> Marked urgent</label>
      </div>
      {{if eq (index .Settings "urgency_mode") "reason"}}
      <div class="field span-12" data-when="urgent:on" {{if not .Request2.Urgent}}hidden{{end}}>
        <label for="ureason">Why is it urgent <span class="req" aria-hidden="true">*</span></label>
        <input id="ureason" name="urgency_reason" aria-required="true" value="{{.Request2.UrgencyReason}}">
      </div>
      {{end}}
      {{end}}
    </div>
  </fieldset>

  {{if or (eq .FormType "vendor_invoice") (eq .FormType "vendor_advance")}}
  <fieldset>
    <legend>Vendor and {{if eq .FormType "vendor_invoice"}}invoice{{else}}advance{{end}}</legend>
    <div class="form-grid">
      {{template "request_vendor_field" .}}
      {{if eq .FormType "vendor_invoice"}}
      <div class="field span-3 m-half"><label for="inv">Invoice number <span class="req" aria-hidden="true">*</span></label><input id="inv" name="invoice_no" value="{{.Request2.InvoiceNo}}" required></div>
      <div class="field span-3 m-half"><label for="idate">Invoice date <span class="req" aria-hidden="true">*</span></label><input id="idate" type="date" name="invoice_date" value="{{.Request2.InvoiceDate}}" required></div>
      {{else}}
      <div class="field span-6"><label for="areason">Reason for the advance <span class="req" aria-hidden="true">*</span></label><input id="areason" name="advance_reason" value="{{.Request2.AdvanceReason}}" required></div>
      {{end}}
    </div>
  </fieldset>
  {{end}}

  {{if eq .FormType "reimbursement"}}
  <fieldset>
    <legend>Your expense</legend>
    <div class="form-grid">
      <div class="field span-6 m-half"><label for="paid">Paid to</label><input id="paid" value="{{.Request2.RequesterName}}" readonly><span class="hint">Reimbursements always pay the person raising them.</span></div>
      <div class="field span-6 m-half"><label for="edate">Expense date <span class="req" aria-hidden="true">*</span></label><input id="edate" type="date" name="expense_date" value="{{.Request2.ExpenseDate}}" required></div>
    </div>
  </fieldset>
  {{end}}

  {{if eq .FormType "employee_advance"}}
  <fieldset>
    <legend>Advance details</legend>
    <div class="form-grid">
      <div class="field span-6 m-half"><label for="adv-to">Paid to</label><input id="adv-to" value="{{.Request2.RequesterName}}" readonly></div>
      <div class="field span-12"><label for="areason2">What the money is for <span class="req" aria-hidden="true">*</span></label><input id="areason2" name="advance_reason" value="{{.Request2.AdvanceReason}}" required></div>
    </div>
  </fieldset>
  {{end}}

  <fieldset>
    <legend>Purpose and documents</legend>
    <div class="form-grid">
      <div class="field span-12"><label for="purp">Purpose <span class="req" aria-hidden="true">*</span></label><textarea id="purp" name="purpose" required>{{.Request2.Purpose}}</textarea></div>
      <div class="field span-12">
        <span class="flabel">Documents</span>
        <div class="stack-8">
          {{range .RequestAtts}}<div class="file-row"><span class="f-ico" aria-hidden="true">{{fileKind .OriginalName}}</span><span><b>{{.OriginalName}}</b><small>{{fileSize .SizeBytes}}</small></span>{{if $.Perms.Can "attachment" "view"}}<span class="f-actions"><a class="btn small outline" href="/requests/{{$.Request2.ID}}/attachments/{{.ID}}">Download</a></span>{{end}}</div>{{end}}
          <div class="uploader">
            <div class="up-ico" aria-hidden="true">⇪</div>
            <label for="attachment"><b>Add another document</b></label>
            <small>PDF, JPG or PNG up to {{index .Settings "attachment_max_mb"}} MB</small>
            <input type="file" id="attachment" name="attachment">
          </div>
        </div>
      </div>
      {{if eq (index .Settings "require_attachments") "1"}}
      <div class="field span-12">
        <label for="att-exception">If you cannot attach a document, say why <span class="req" aria-hidden="true">*</span></label>
        <input id="att-exception" name="attachment_exception_reason" aria-required="true" value="{{.Request2.AttachmentExceptionReason}}">
      </div>
      {{end}}
    </div>
  </fieldset>

  <fieldset>
    <legend>Who approves it</legend>
    <div class="form-grid">
      <div class="field span-6">
        <label for="apr">Approver <span class="req" aria-hidden="true">*</span></label>
        <select id="apr" name="manager_id" required>
          {{range .Approvers}}<option value="{{.ID}}" {{if eq $.Request2.ManagerID .ID}}selected{{end}}>{{.Name}}</option>{{end}}
        </select>
        <span class="hint">Changing this moves the request to the new approver and notifies them. Your own name is never in this list.</span>
      </div>
    </div>
  </fieldset>

  <div class="action-bar">
    <span class="ab-note d-only">Every change is recorded in the history.</span>
    <span class="row-end"></span>
    <a class="btn outline" href="/requests/{{.Request2.ID}}">Discard changes</a>
    {{/* On a returned request the promise is only true if this press also
         resubmits: a save alone leaves it returned and tells nobody
         (notifications-1). Saving without sending stays on the returned
         screen's own "Save corrections". */}}
    <button class="btn primary" type="submit" data-follows-select="apr" data-follows-text="Save and notify {name}"{{if eq .Request2.Status "returned"}} name="submit_action" value="resubmit"{{end}}>Save and notify {{.Request2.ManagerName}}</button>
  </div>
</form>
{{template "bottom" .}}
{{end}}

{{/* The correction screen — mockups/screens/request-returned.html.

     "Returned is not rejected. A returned request keeps its number and its
     history — you correct it and resubmit. A rejected request is final and
     read-only; the employee raises a new one." So this lives on the same URL as
     the detail, above the same thread, and its primary action sends the request
     back rather than saving it.

     Correcting and resubmitting are one press. Two buttons over two forms would
     be two sticky action bars on a phone, and saving-then-forgetting is how a
     returned request sits for a week with nobody waiting on it.

     Every field the store validates is here, visible where the approver is
     likely to have asked for a change and hidden where it is not: a field that
     is missing from the body is a field the store reads as cleared. */}}
{{define "request_returned"}}
{{template "top" .}}
{{template "request_head" .}}

<div class="banner warn">
  <span class="b-ico" aria-hidden="true">↩</span>
  <div>
    <b>{{.Request2.ManagerName}} sent this back</b>
    <p>“{{.Request2.DecisionReason}}”</p>
  </div>
</div>

<p class="hint">Returned is not rejected. This request keeps its number and its history — correct it
  and send it again.</p>

{{/* The same cautions the full edit form shows. A legacy deposit filed as an
     employee advance is reclassified by the one-press resubmit below exactly as
     it is by "Edit every field", so the reader is told here, before the press,
     and not only on the screen they might never open (form-1, review). */}}
{{template "form_warnings" .}}

<form method="post" enctype="multipart/form-data" action="/requests/{{.Request2.ID}}/edit">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <input type="hidden" name="type" value="{{.FormType}}">
  <input type="hidden" name="treatment" value="{{.Request2.Treatment}}">
  <input type="hidden" name="manager_id" value="{{.Request2.ManagerID}}">
  <input type="hidden" name="project_id" value="{{deref .Request2.ProjectID}}">
  <input type="hidden" name="head_id" value="{{deref .Request2.HeadID}}">
  <input type="hidden" name="needed_by" value="{{.Request2.NeededBy}}">
  {{if .Request2.Urgent}}<input type="hidden" name="urgent" value="on">
  <input type="hidden" name="urgency_reason" value="{{.Request2.UrgencyReason}}">{{end}}
  <input type="hidden" name="attachment_exception_reason" value="{{.Request2.AttachmentExceptionReason}}">
  {{if eq .Request2.Treatment "recoverable"}}
  <input type="hidden" name="recoverable_category" value="{{.Request2.RecoverableCategory}}">
  <input type="hidden" name="expected_return_date" value="{{.Request2.ExpectedReturnDate}}">
  <input type="hidden" name="repayment_notes" value="{{.Request2.RepaymentNotes}}">
  <input type="hidden" name="counterparty" value="{{.Request2.Counterparty}}">
  {{end}}
  {{if or (eq .FormType "vendor_invoice") (eq .FormType "vendor_advance")}}
  <input type="hidden" name="vendor_id" value="{{deref .Request2.VendorID}}">
  {{end}}
  {{/* A deposit's payee is a field the store reads as cleared when absent, and
       refuses cleared; it travels with the rest of what this screen does not
       show. "Edit every field" is where it is changed. */}}
  {{if eq .FormType "recoverable"}}
  <input type="hidden" name="vendor_payee" value="{{.Request2.VendorPayee}}">
  {{end}}

  <fieldset>
    <legend>Correct and resubmit</legend>
    <div class="form-grid">
      <div class="field span-12"><label for="rt-title">Short title <span class="req" aria-hidden="true">*</span></label><input id="rt-title" name="short_title" value="{{.Request2.ShortTitle}}" required></div>
      {{if eq .FormType "vendor_invoice"}}
      <div class="field span-4 m-half"><label for="rt-inv">Invoice number <span class="req" aria-hidden="true">*</span></label><input id="rt-inv" name="invoice_no" value="{{.Request2.InvoiceNo}}" required></div>
      <div class="field span-4 m-half"><label for="rt-idate">Invoice date <span class="req" aria-hidden="true">*</span></label><input id="rt-idate" type="date" name="invoice_date" value="{{.Request2.InvoiceDate}}" required></div>
      {{end}}
      {{if eq .FormType "reimbursement"}}
      <div class="field span-4 m-half"><label for="rt-edate">Expense date <span class="req" aria-hidden="true">*</span></label><input id="rt-edate" type="date" name="expense_date" value="{{.Request2.ExpenseDate}}" required></div>
      {{end}}
      {{if or (eq .FormType "vendor_advance") (eq .FormType "employee_advance")}}
      <div class="field span-8"><label for="rt-areason">Reason for the advance <span class="req" aria-hidden="true">*</span></label><input id="rt-areason" name="advance_reason" value="{{.Request2.AdvanceReason}}" required></div>
      {{end}}
      <div class="field span-4 money-field">
        <label for="rt-amt">Amount <span class="req" aria-hidden="true">*</span></label>
        <span class="money-wrap"><span class="cur" aria-hidden="true">₹</span><input id="rt-amt" name="amount" inputmode="decimal" value="{{amountValue .Request2.Amount}}" required></span>
        <span class="in-words">{{if .Request2.Amount}}{{inWords .Request2.Amount}}{{else}}Enter the amount you are requesting{{end}}</span>
      </div>
      <div class="field span-12"><label for="rt-purpose">Purpose <span class="req" aria-hidden="true">*</span></label><textarea id="rt-purpose" name="purpose" required>{{.Request2.Purpose}}</textarea></div>
      <div class="field span-12">
        <span class="flabel">Documents</span>
        <div class="stack-8">
          {{range .RequestAtts}}<div class="file-row"><span class="f-ico" aria-hidden="true">{{fileKind .OriginalName}}</span><span><b>{{.OriginalName}}</b><small>{{fileSize .SizeBytes}}</small></span>{{if $.Perms.Can "attachment" "view"}}<span class="f-actions"><a class="btn small outline" href="/requests/{{$.Request2.ID}}/attachments/{{.ID}}">Download</a></span>{{end}}</div>{{end}}
          <div class="uploader">
            <div class="up-ico" aria-hidden="true">⇪</div>
            <label for="rt-attachment"><b>Attach the corrected document</b></label>
            <small>PDF, JPG or PNG up to {{index .Settings "attachment_max_mb"}} MB</small>
            <input type="file" id="rt-attachment" name="attachment">
          </div>
        </div>
      </div>
    </div>
  </fieldset>

  <div class="action-bar">
    <span class="ab-note d-only">Saving keeps it with you; resubmitting sends it back to {{.Request2.ManagerName}}.</span>
    <span class="row-end"></span>
    {{/* This screen replaces request_detail for a returned request, and with it
         the Edit request control — so the full form was reachable only by typing
         its address, even though the route permits it. The inline fields above
         cover the common correction; this is the way to the rest of them. */}}
    {{if .Perms.Can "request" "edit"}}<a class="btn outline" href="/requests/{{.Request2.ID}}/edit">Edit every field</a>{{end}}
    {{/* On a legacy row both presses file it as an Employee advance (the store
         accepts nothing else for this type), so the buttons say so. */}}
    {{if .LegacyCategory}}
    <button class="btn outline" type="submit" name="submit_action" value="save">Save as an Employee advance</button>
    <button class="btn primary" type="submit" name="submit_action" value="resubmit">Resubmit as an Employee advance</button>
    {{else}}
    <button class="btn outline" type="submit" name="submit_action" value="save">Save corrections</button>
    <button class="btn primary" type="submit" name="submit_action" value="resubmit">Resubmit for approval</button>
    {{end}}
  </div>
</form>

{{template "request_thread" .}}
{{template "bottom" .}}
{{end}}

{{/* The employee's side of a cancellation — mockups/screens/request-cancel.html.

     An approved request cannot be withdrawn on your own, so this screen asks
     rather than acts, and it says what asking costs before you ask: payment
     freezes the moment the request is sent, whether or not the approver ever
     gets round to deciding. */}}
{{define "request_cancel"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">{{.Request2.Number}} · {{reqStatus .Request2.Status}}</div>
    <h1>Ask for this to be cancelled</h1>
    <p class="sub">Approved requests cannot be withdrawn on your own. Your approver decides.</p>
  </div>
</section>

<div class="banner warn">
  <span class="b-ico" aria-hidden="true">⏸</span>
  <div>
    <b>Payment freezes the moment you ask</b>
    <p>Accounts cannot reserve or pay this request while a cancellation is pending. If an accountant
      has already reserved it, they are told immediately.</p>
  </div>
</div>

<div class="req-head">
  <div class="rh-top"><span class="rh-no">{{.Request2.Number}}</span><span class="rh-amt">{{money .Request2.Amount}}</span></div>
  <p class="rh-meta"><b>{{.Request2.ShortTitle}}</b> · approved by {{.Request2.ManagerName}}{{if .Request2.ApprovedAt}} on {{datep .Request2.ApprovedAt}}{{end}}</p>
  <div class="rh-status"><span class="pill {{pillClass .Request2.Status}}">{{reqStatus .Request2.Status}}</span></div>
</div>

<form method="post" action="/requests/{{.Request2.ID}}/cancel-request">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <fieldset>
    <legend>Why should it be cancelled</legend>
    <div class="form-grid">
      <div class="field span-12">
        <label for="reason">Reason <span class="req" aria-hidden="true">*</span></label>
        <textarea id="reason" name="reason" required placeholder="Say what changed. {{.Request2.ManagerName}} sees exactly this."></textarea>
      </div>
      <div class="field span-12">
        <span class="flabel">What happens next</span>
        <ul class="hint" style="margin:4px 0 0; padding-left:18px">
          <li>{{.Request2.ManagerName}} is notified and the request shows <b>Cancellation requested</b>.</li>
          <li>If they accept, the request is cancelled and closed. Nothing can be paid against it.</li>
          <li>If they decline, it returns to <b>Approved — awaiting payment</b> and Accounts can proceed.</li>
        </ul>
      </div>
    </div>
  </fieldset>
  <div class="action-bar">
    <span class="row-end"></span>
    <a class="btn outline" href="/requests/{{.Request2.ID}}">Never mind</a>
    <button class="btn danger" type="submit">Send cancellation request</button>
  </div>
</form>
{{template "bottom" .}}
{{end}}

{{/* The approver's side — mockups/screens/manager-cancellation-decision.html.

     One screen for two questions, because they are the same question asked by
     two people: should this still be paid. When a cancellation is pending the
     answer is accept or decline; when none is, the approver may still cancel
     outright with a reason (G2). Either way the reason is written down, because
     the requester and Accounts both read it. */}}
{{define "request_cancellation"}}
{{template "top" .}}
{{template "request_head" .}}

{{if eq .Request2.Status "cancellation_requested"}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">⏸</span>
  <div>
    <b>Payment is frozen</b>
    <p>No accountant can reserve or pay this request until you decide.</p>
  </div>
</div>

<div class="card">
  <div class="card-head"><h2>Why {{.Request2.RequesterName}} wants it cancelled</h2></div>
  <div class="card-body"><p style="margin:0">“{{.Request2.CancelReason}}”</p></div>
</div>
{{end}}

<div class="card" style="margin-top:12px">
  <div class="card-head"><h2>What you approved</h2></div>
  <dl class="dl">
    <div><dt>Amount</dt><dd class="big">{{if .Request2.ApprovedAmount}}{{money (deref .Request2.ApprovedAmount)}}{{else}}{{money .Request2.Amount}}{{end}}</dd></div>
    {{if .Request2.ApprovedAt}}<div><dt>Approved on</dt><dd>{{datep .Request2.ApprovedAt}}</dd></div>{{end}}
    <div><dt>{{if .Request2.VendorID}}Vendor{{else}}Paid to{{end}}</dt><dd>{{.Request2.Vendor}}</dd></div>
    {{if .Request2.AdvanceReason}}<div><dt>Advance reason</dt><dd>{{.Request2.AdvanceReason}}</dd></div>{{end}}
    <div style="grid-column:1/-1"><dt>Purpose</dt><dd>{{.Request2.Purpose}}</dd></div>
  </dl>
</div>

{{template "request_thread" .}}

<div class="action-bar">
  <span class="ab-note d-only">{{if eq .Request2.Status "cancellation_requested"}}Declining sends it back to Accounts to pay as approved.{{else}}Cancelling closes the request permanently.{{end}}</span>
  <span class="row-end"></span>
  {{if eq .Request2.Status "cancellation_requested"}}
    <button class="btn outline" type="button" data-open="decline-sheet">Decline — keep it live</button>
    <button class="btn danger" type="button" data-open="accept-sheet">Cancel the request</button>
  {{else if eq .Request2.Status "approved"}}
    <button class="btn danger" type="button" data-open="outright-sheet">Cancel with reason</button>
  {{else}}
    <a class="btn outline" href="/requests/{{.Request2.ID}}">Back to the request</a>
  {{end}}
</div>

{{if eq .Request2.Status "cancellation_requested"}}
<div class="overlay" id="accept-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/cancellation">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <input type="hidden" name="decision" value="accept">
    <div class="sh-head"><div><h2>Cancel {{.Request2.Number}}?</h2><p class="sh-sub">{{money .Request2.Amount}} to {{.Request2.Vendor}}</p></div><button class="sh-close" type="button" data-close="accept-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="banner bad" style="margin:0"><span class="b-ico" aria-hidden="true">!</span><div><b>The request closes permanently</b><p>Nothing can be paid against it. A new request is needed if the order comes back.</p></div></div>
      <div class="field"><label for="cx-note">Note <span class="opt" aria-hidden="true">optional</span></label><textarea id="cx-note" name="note" placeholder="Recorded in the history."></textarea></div>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="accept-sheet">Back</button><span class="row-end"></span><button class="btn danger" type="submit">Cancel request</button></div>
  </form>
</div>

<div class="overlay" id="decline-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/cancellation">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <input type="hidden" name="decision" value="decline">
    <div class="sh-head"><div><h2>Decline the cancellation</h2><p class="sh-sub">The request returns to Approved — awaiting payment.</p></div><button class="sh-close" type="button" data-close="decline-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="field"><label for="dc-reason">Why it should still be paid <span class="req" aria-hidden="true">*</span></label><textarea id="dc-reason" name="note" required placeholder="{{.Request2.RequesterName}} and Accounts both see this."></textarea></div>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="decline-sheet">Back</button><span class="row-end"></span><button class="btn primary" type="submit">Decline and unfreeze</button></div>
  </form>
</div>
{{else if eq .Request2.Status "approved"}}
<div class="overlay" id="outright-sheet" hidden>
  <form class="sheet" method="post" action="/requests/{{.Request2.ID}}/cancel">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="sh-head"><div><h2>Cancel {{.Request2.Number}}?</h2><p class="sh-sub">{{.Request2.RequesterName}} did not ask for this.</p></div><button class="sh-close" type="button" data-close="outright-sheet" aria-label="Close">✕</button></div>
    <div class="sh-body stack-12">
      <div class="field"><label for="oc-reason">Reason <span class="req" aria-hidden="true">*</span></label><textarea id="oc-reason" name="reason" required placeholder="{{.Request2.RequesterName}} and Accounts both see this."></textarea></div>
    </div>
    <div class="sh-foot"><button class="btn outline" type="button" data-close="outright-sheet">Back</button><span class="row-end"></span><button class="btn danger" type="submit">Cancel request</button></div>
  </form>
</div>
{{end}}
{{template "bottom" .}}
{{end}}

{{/* The manager queue — mockups/screens/approvals-list.html.

     Its own route, because it answers a different question from the requests
     list: not "where are my requests" but "what is mine to decide". The tabs
     are statuses, and Cancellations is one of them because a frozen payment is
     a decision only this person can make (G1).

     There is no checkbox column and no "approve selected" button. Every
     approval is a decision made after opening the request (A6). */}}
{{define "approvals"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Manager queue</div>
    <h1>Waiting on you</h1>
    <p class="sub">{{index .Counts "to-approve"}} to approve · {{index .Counts "cancellations"}} {{plural (index .Counts "cancellations") "cancellation" "cancellations"}} to decide · {{index .Counts "partial-review"}} {{plural (index .Counts "partial-review") "partial payment" "partial payments"}} to review</p>
  </div>
  <div class="pb-actions">
    <a class="btn outline" href="/requests/export.csv?scope=assigned&amp;q={{.Query}}">⤓ Export CSV</a>
  </div>
</section>

<form class="toolbar" method="get" action="/approvals">
  <input type="hidden" name="bucket" value="{{.Bucket}}">
  <div class="field search"><label for="aq">Search</label><input id="aq" name="q" value="{{.Query}}" placeholder="Number, payee, invoice, requester…"></div>
  <span class="row-end"></span>
  <button class="btn">Apply</button>
</form>

<form class="m-filters" method="get" action="/approvals">
  <input type="hidden" name="bucket" value="{{.Bucket}}">
  <span class="m-search"><input name="q" value="{{.Query}}" placeholder="Search approvals…" aria-label="Search approvals"></span>
  <button class="btn filter-btn" type="submit">Search</button>
</form>

<div class="segmented" style="margin-bottom:12px">
  {{range approvalTabs}}<a class="{{if eq $.Bucket .Key}}is-active{{end}}"
    {{if eq $.Bucket .Key}}aria-current="page"{{end}}
    href="/approvals?bucket={{.Key}}&amp;q={{$.Query}}">{{.Label}} <span class="n">{{index $.Counts .Key}}</span></a>{{end}}
</div>

<div class="req-list">
  {{range .Requests}}{{template "request_card" (card . $.User.ID)}}{{else}}<p class="empty">Nothing is waiting on you here.</p>{{end}}
</div>

<p class="hint">Every approval is a decision made after opening the request. There is no bulk
  approval, by design.</p>
{{template "bottom" .}}
{{end}}

{{/* The advisory duplicate warning — mockups/screens/request-duplicate-warning.html.

     "A duplicate warning never blocks submission. Legitimate repeat payments
     exist — the same rent, the same monthly retainer. The system points, the
     person decides." The endpoint that renders this is a read; the submit does
     not consult it and cannot be failed by it. */}}
{{define "request_duplicates"}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">⚠</span>
  <div>
    <b>{{if eq (len .Similar) 1}}A similar request already exists{{else}}{{len .Similar}} similar requests already exist{{end}}</b>
    <p>Same payee and a close amount in the last 30 days. Check before you submit — you can still go ahead.</p>
    <div class="req-list" style="margin-top:8px">
      {{range .Similar}}
      <a class="req-card" href="/requests/{{.ID}}">
        <span class="rc-top"><span class="rc-no">{{.Number}}</span><span class="rc-amt">{{money .Amount}}</span></span>
        <span class="rc-title">{{.ShortTitle}}</span>
        <span class="rc-meta">{{.Vendor}}{{if .InvoiceNo}} · invoice {{.InvoiceNo}}{{end}} · raised {{date .CreatedAt}}</span>
        <span class="rc-foot">
          <span class="pill {{pillClass .Status}}">{{reqStatus .Status}}</span>
          <span class="waiting">{{if eq .Status "approved"}}Waiting on Accounts{{else}}Waiting on {{.ManagerName}}{{end}}</span>
        </span>
      </a>
      {{end}}
    </div>
  </div>
</div>
{{end}}

{{/* The confirmation — mockups/screens/request-submitted.html.

     It exists to answer the only question a person has after pressing Submit:
     what happens now, and who has it. The three-step .thread is a forecast, not
     a history; the request's real history lives on its detail screen. */}}
{{define "request_submitted"}}
{{template "top" .}}
<div class="banner good">
  <span class="b-ico" aria-hidden="true">✓</span>
  <div>
    <b>Submitted. {{.Request2.ManagerName}} has been notified.</b>
    <p>You can still edit this request until they act on it. Every edit tells them again and restarts
      the {{.Reminders.PendingAfterDays}}-day reminder clock.</p>
  </div>
</div>

<div class="req-head">
  <div class="rh-top">
    <span class="rh-no">{{.Request2.Number}}</span>
    <span class="rh-amt">{{money .Request2.Amount}}</span>
  </div>
  <h1>{{.Request2.ShortTitle}}</h1>
  <p class="rh-meta">{{typeLabel .Request2.Type}}{{if eq .Request2.Treatment "recoverable"}} · {{recoverable .Request2.RecoverableCategory}}{{end}}{{if .Request2.Project}} · {{.Request2.Project}}{{end}}{{if .Request2.Head}} · {{.Request2.Head}}{{end}}{{if .Request2.NeededBy}} · needed by {{dateLong .Request2.NeededBy}}{{end}}</p>
  <div class="rh-status">
    <span class="pill {{pillClass .Request2.Status}}">{{reqStatus .Request2.Status}}</span>
    <span class="waiting">Waiting on {{.Request2.ManagerName}}</span>
  </div>
</div>

<div class="section-head"><h2>What happens next</h2></div>
<ol class="thread">
  <li>
    <span class="tl-dot brand" aria-hidden="true">1</span>
    <div class="tl-head"><b>{{.Request2.ManagerName}} reviews it</b></div>
    <div class="tl-body">They can approve, return it to you for a correction, or reject it. A reminder
      goes out every {{if eq .Reminders.RepeatEveryDays 1}}day{{else}}{{.Reminders.RepeatEveryDays}} days{{end}} if nothing happens for {{.Reminders.PendingAfterDays}} {{plural .Reminders.PendingAfterDays "day" "days"}}.</div>
  </li>
  <li>
    <span class="tl-dot" aria-hidden="true">2</span>
    <div class="tl-head"><b>Accounts picks it up</b></div>
    <div class="tl-body">Once approved, an accountant reserves the request and records the payment
      against it. Nobody else can process it while it is reserved.</div>
  </li>
  <li>
    <span class="tl-dot" aria-hidden="true">3</span>
    <div class="tl-head"><b>It settles</b></div>
    <div class="tl-body">Accounts confirms the payment covers the obligation and the request closes.
      You are notified at every step.</div>
  </li>
</ol>

<div class="action-bar">
  <span class="row-end"></span>
  <a class="btn outline" href="/requests/new">Raise another</a>
  <a class="btn" href="/requests">My requests</a>
  <a class="btn primary" href="/requests/{{.Request2.ID}}">View this request</a>
</div>
{{template "bottom" .}}
{{end}}

{{/* The Configuration screen — mockups/screens/admin-configuration.html.

     One form of fieldsets closed by one action bar. The screen is a rendering
     of configSections, so a later phase adds a section by appending to that
     table and touches nothing here.

     Two controls are deliberately not settings. "Block self-approval" renders
     checked and disabled because G8 is structural, not configurable, and
     "Second approval above a threshold" renders disabled as a stated non-goal.
     Neither carries a name, so neither can be written. */}}
{{define "configuration"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Administration</div>
    <h1>Configuration</h1>
    <p class="sub">The rules the request module runs on. Everything here is data — no code change needed.</p>
  </div>
</section>

<form method="post" action="/configuration">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  {{range configSections}}
  <fieldset>
    <legend>{{.Title}}</legend>
    <div class="form-grid">
      {{range .Fields}}
      <div class="field span-{{.Span}}{{if lt .Span 12}} m-half{{end}}">
        {{if eq .Kind "toggle"}}
          <label class="checkline"><input type="checkbox" name="{{.Key}}" {{if eq (index $.Config .Key) "1"}}checked{{end}}> {{.Label}}</label>
        {{else if eq .Kind "select"}}
          <label for="cf-{{.Key}}">{{.Label}}</label>
          {{$current := index $.Config .Key}}
          <select id="cf-{{.Key}}" name="{{.Key}}">
            {{range .Options}}<option value="{{.Value}}" {{select $current .Value}}>{{.Label}}</option>{{end}}
          </select>
        {{else}}
          <label for="cf-{{.Key}}">{{.Label}}</label>
          <input id="cf-{{.Key}}" name="{{.Key}}" {{if eq .Kind "number"}}type="number" inputmode="numeric"{{end}} value="{{index $.Config .Key}}">
        {{end}}
        {{if .Hint}}<span class="hint">{{.Hint}}</span>{{end}}
      </div>
      {{end}}
      {{if eq .Title "Approvals"}}
      <div class="field span-12">
        <label class="checkline"><input type="checkbox" checked disabled> Block self-approval</label>
        <span class="hint">Always on. A person can never approve a request they raised, whatever roles they hold.</span>
      </div>
      <div class="field span-12">
        <label class="checkline"><input type="checkbox" disabled> Second approval above a threshold</label>
        <span class="hint">Reserved for a future version. The status model already has room for it.</span>
      </div>
      {{end}}
    </div>
    {{if .Note}}<p class="hint" style="margin:10px 0 0">{{.Note}}</p>{{end}}
  </fieldset>
  {{end}}

  <div class="action-bar">
    <span class="ab-note d-only">Every change here is written to the audit log.</span>
    <span class="row-end"></span>
    <a class="btn outline" href="/dashboard">Discard</a>
    <button class="btn primary" type="submit">Save configuration</button>
  </div>
</form>

{{/* Recoverable categories (Phase 4, D6). This fieldset sits OUTSIDE the
     settings form on purpose: its rows each need their own POST, and a <form>
     nested inside another <form> is invalid HTML that the parser silently
     drops — the row toggles would post nothing at all. A bare fieldset outside
     a form is valid, and it reads as one more section of the same screen.

     "Requires" is one derived descriptive column, never the two raw flags: an
     admin should be told what a category asks for, not asked to reason about
     two booleans.

     Each row is editable in place (recoverables-5). The name and the rule used
     to travel as hidden inputs, so the only way to change an in-use category's
     rule was delete-and-recreate — which the store refuses while any request
     names it. The name input and the requires select sit in their own cells
     and belong to the row's form through the form= attribute, the same shape
     the heads screen uses; Save is the row's submit, and the Active checkbox
     still saves on change, carrying the row's current name and rule with it.
     The code is not editable: it is the stable identity every request stores. */}}
<fieldset>
  <legend>Recoverable categories</legend>
  <div class="table-wrap" style="margin-bottom:10px"><table class="t-cards">
    <thead><tr><th>Category</th><th>Requires</th><th class="c">Active</th><th class="c">In use</th>{{if or (.Perms.Can "recoverable_category" "edit") (.Perms.Can "recoverable_category" "delete")}}<th>Actions</th>{{end}}</tr></thead>
    <tbody>{{range .CategoryUsage}}<tr>
      <td class="t-lead" data-label="Category">{{if $.Perms.Can "recoverable_category" "edit"}}<input form="rc-{{.ID}}" name="name" aria-label="Name for {{.Name}}" value="{{.Name}}" required>{{else}}{{.Name}}{{end}}</td>
      <td data-label="Requires">{{if $.Perms.Can "recoverable_category" "edit"}}{{$key := requiresKey .RecoverableCategory}}<select form="rc-{{.ID}}" name="requires" aria-label="Requires for {{.Name}}">
        <option value="none" {{select $key "none"}}>Nothing extra</option>
        <option value="project" {{select $key "project"}}>Related project</option>
        <option value="counterparty" {{select $key "counterparty"}}>Counterparty company</option>
        <option value="both" {{select $key "both"}}>Related project and counterparty company</option>
      </select>{{else}}{{.Requires}}{{end}}</td>
      <td class="c" data-label="Active">{{if $.Perms.Can "recoverable_category" "edit"}}
        <form id="rc-{{.ID}}" method="post" action="/configuration/recoverable-categories">
          <input type="hidden" name="csrf" value="{{$.CSRF}}">
          <input type="hidden" name="id" value="{{.ID}}">
          <input type="hidden" name="sort_order" value="{{.SortOrder}}">
          <label class="checkline"><input type="checkbox" name="active" {{check .Active}} onchange="this.form.submit()"> <span class="sr-only">Active</span></label>
        </form>
      {{else}}{{if .Active}}<span class="pill good no-dot">On</span>{{else}}<span class="pill neutral no-dot">Off</span>{{end}}{{end}}</td>
      <td class="c" data-label="In use">{{.InUse}}</td>
      {{/* F-E-06's control. Its own form, a sibling of the Active toggle's and
           never nested inside it: the parser drops a nested form silently, which
           is the same reason this whole fieldset sits outside the settings form.

           No confirm() dialog — the guard is the server's. store.DeleteRecoverableCategory
           counts the requests pointing at the category inside the DELETE's own
           transaction and refuses with that count, so the "In use" figure beside
           this button is a forecast of the answer, never the gate. The button is
           offered on every row for that reason: a reader who presses it on a
           category in use is told how many and that deactivating is the way to
           retire it, which is worth more than a control that quietly is not
           there. */}}
      {{if or ($.Perms.Can "recoverable_category" "edit") ($.Perms.Can "recoverable_category" "delete")}}
      <td class="actions-cell" data-label="Actions">
        {{if $.Perms.Can "recoverable_category" "edit"}}<button form="rc-{{.ID}}" class="btn small outline" type="submit">Save<span class="sr-only"> {{.Name}}</span></button>{{end}}
        {{if $.Perms.Can "recoverable_category" "delete"}}
        <form method="post" action="/configuration/recoverable-categories/{{.ID}}/delete">
          <input type="hidden" name="csrf" value="{{$.CSRF}}">
          <button class="btn small danger" type="submit">Delete<span class="sr-only"> {{.Name}}</span></button>
        </form>
        {{end}}
      </td>
      {{end}}
    </tr>{{else}}<tr><td colspan="{{if or (.Perms.Can "recoverable_category" "edit") (.Perms.Can "recoverable_category" "delete")}}5{{else}}4{{end}}" class="empty" data-label="">No recoverable categories yet.</td></tr>{{end}}</tbody>
  </table></div>
  {{if .Perms.Can "recoverable_category" "edit"}}
  <form method="post" action="/configuration/recoverable-categories">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <input type="hidden" name="active" value="on">
    <div class="form-grid">
      <div class="field span-5"><label for="nc-name">New category</label><input id="nc-name" name="name" placeholder="e.g. Retention deposit" required></div>
      <div class="field span-4 m-half"><label for="nc-req">Must also capture</label><select id="nc-req" name="requires">
        <option value="none">Nothing extra</option>
        <option value="project">Related project</option>
        <option value="counterparty">Counterparty company</option>
        <option value="both">Related project and counterparty company</option>
      </select></div>
      <div class="field span-3 m-half" style="align-self:end"><button class="btn" type="submit">Add category</button></div>
    </div>
  </form>
  {{end}}
  {{/* The payee, not the counterparty (F-E-07). forcesRequesterPayee fills
       vendor_payee — what /requests/{id} shows as "Paid to «name»" — for every
       employee-advance request whatever its treatment. The Counterparty field, the
       one RequiresCounterparty governs and the one this hint stands beside, is left
       exactly as the form left it and reads "Not recorded" for the seeded Employee
       advance category, which requires neither project nor counterparty. */}}
  <p class="hint" style="margin:10px 0 0">All recoverables always capture the reason, an expected return date and the refund terms. These rules only add what the category needs on top. An employee advance fills the <b>payee</b> in from the requester automatically — that is a request-type rule, not a category setting, and it does not fill in the counterparty.</p>
</fieldset>
{{template "bottom" .}}
{{end}}

{{/* The work dashboard — mockups/screens/dashboard.html.

     A metric strip plus work areas, not one or the other. The strip is the
     shape of the day at a glance; each area under it hands over the actual rows
     to act on and links to the queue behind them.

     Every area is gated on the permission its queue is gated on, so nobody is
     shown work behind a door they cannot open, and an area with nothing in it
     is not rendered at all — the strip already carries the zero. */}}
{{define "dashboard"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Home</div>
    <h1>Good day, {{.User.Name}}</h1>
    <p class="sub">Everything below is waiting on someone. The ones marked <em>you</em> are yours.</p>
  </div>
  <div class="pb-actions">
    {{if .Perms.Can "request" "create"}}<a class="btn primary" href="/requests/new">＋ New request</a>{{end}}
  </div>
</section>

<div class="metric-strip">
  {{if .Perms.Can "request" "create"}}
  <a class="metric{{if index .Counts "needs-action"}} warn{{end}}" href="/requests?bucket=needs-me">
    <span class="metric-label">Needs my action</span>
    <span class="metric-value">{{index .Counts "needs-action"}}</span>
  </a>
  <a class="metric" href="/requests?bucket=open">
    <span class="metric-label">My open requests</span>
    <span class="metric-value">{{index .Counts "in-progress"}}</span>
  </a>
  {{end}}
  {{if .Perms.Can "approval" "approve"}}
  <a class="metric{{if index .Counts "approvals"}} warn{{end}}" href="/approvals">
    <span class="metric-label">Awaiting my approval</span>
    <span class="metric-value">{{index .Counts "approvals"}}</span>
  </a>
  <a class="metric" href="/approvals?bucket=cancellations">
    <span class="metric-label">Cancellation requests</span>
    <span class="metric-value">{{index .Counts "decisions"}}</span>
  </a>
  {{end}}
  {{if .Perms.Can "approval" "accept_partial"}}
  <a class="metric{{if index .Counts "partials"}} warn{{end}}" href="/approvals?bucket=partial-review">
    <span class="metric-label">Partial payments to review</span>
    <span class="metric-value">{{index .Counts "partials"}}</span>
  </a>
  {{end}}
  {{/* The queue, which is the screen carrying the identical label and the
       identical number. It used to link to /requests?bucket=open, which is a
       different set entirely (F-G-007). */}}
  {{if .Perms.Can "payment" "process"}}
  <a class="metric" href="/accounts-queue?tab=approved">
    <span class="metric-label">Approved, unclaimed</span>
    <span class="metric-value">{{index .Counts "accounts"}}</span>
  </a>
  {{end}}
  {{if .Perms.Can "grid" "view"}}
  <a class="metric" href="/grid">
    <span class="metric-label">Budget</span>
    <span class="metric-value">Variance grid</span>
  </a>
  {{end}}
</div>

<div class="work-areas">
  {{range .Areas}}
  <section class="area">
    <div class="a-head"><span class="a-ico" aria-hidden="true">{{.Icon}}</span><h2>{{.Title}}</h2>{{if .Count}}<span class="n">{{.Count}}</span>{{end}}</div>
    <div class="a-list">
      {{range .Requests}}
      <a href="/requests/{{.ID}}">
        <span class="al-main"><b>{{.Number}} · {{.ShortTitle}}</b>
          <small>
            {{if .Urgent}}<span class="pill urgent">Urgent</span> {{end}}
            {{$p := statusPill . $.User.ID}}<span class="pill {{$p.Class}}">{{$p.Text}}</span>
            {{$w := waitingOn . $.User.ID}}<span class="waiting {{$w.Class}}">{{$w.Text}}</span>
          </small></span>
        <span class="al-amt">{{money .Amount}}</span>
      </a>
      {{end}}
      {{range .Links}}
      <a href="{{.Href}}"><span class="al-main"><b>{{.Label}}</b><small>{{.Sub}}</small></span><span class="al-amt" aria-hidden="true">→</span></a>
      {{end}}
    </div>
    {{if .FootHref}}<a class="a-foot" href="{{.FootHref}}">{{.FootText}}</a>{{end}}
  </section>
  {{else}}
  <section class="area">
    <div class="a-head"><span class="a-ico" aria-hidden="true">✓</span><h2>Nothing is waiting on you</h2></div>
    <div class="a-list"><p class="empty">When something needs you, it appears here.</p></div>
  </section>
  {{end}}
</div>
{{template "bottom" .}}
{{end}}

{{define "audit"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Evidence</div><h1>Audit Log</h1><p class="sub muted">Review who changed financial records, when, and why.</p></div></section>
{{/* Both option lists come from auditEntities/auditActions, which are built from
     the strings the store actually writes. The hand-maintained literals they
     replace had drifted so far that payment_request — the entity every request,
     approval, reservation and settlement is filed under — could not be asked for
     at all, and the entire Phase-2/Phase-3 action vocabulary was missing, so the
     screen whose stated purpose is reviewing who changed financial records could
     not be pointed at the workflow (F-G-004/F-C-06). */}}
<form class="toolbar" method="get"><label>Entity<select name="entity"><option value="">All</option>{{range auditEntities}}<option value="{{.Value}}" {{select $.AuditEntity .Value}}>{{.Label}}</option>{{end}}</select></label><label>Action<select name="action"><option value="">All</option>{{range auditActions}}<option value="{{.Value}}" {{select $.AuditAction .Value}}>{{.Label}}</option>{{end}}</select></label><label>Actor<input name="actor" value="{{.AuditActor}}" placeholder="Name"></label><button>Filter</button><a class="btn outline" href="/audit">Reset</a></form>
<div class="table-wrap"><table class="t-cards"><thead><tr><th>When</th><th>Actor</th><th>Entity</th><th>Action</th><th>Summary</th></tr></thead><tbody>{{range .Audit}}<tr><td class="t-lead" data-label="When">{{date .CreatedAt}}</td><td data-label="Actor">{{.ActorName}}</td><td data-label="Entity">{{entityText .EntityType}}</td><td data-label="Action"><span class="pill {{actionClass .Action}}">{{actionText .Action}}</span></td><td data-label="Summary"><span>{{.Summary}}{{if or (hasText .BeforeJSON) (hasText .AfterJSON)}}<details><summary>Before / after</summary>{{if hasText .BeforeJSON}}<pre>{{jsonPretty .BeforeJSON}}</pre>{{end}}{{if hasText .AfterJSON}}<pre>{{jsonPretty .AfterJSON}}</pre>{{end}}</details>{{end}}</span></td></tr>{{else}}<tr><td colspan="5" class="empty" data-label="">No audit entries match these filters.</td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}

{{define "reports"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Analysis</div><h1>Reports</h1><p class="sub muted">{{.From}} to {{.To}} · {{.Mode}} view</p></div><div class="head-actions">{{if .Perms.Can "budget" "view"}}<a class="btn outline" href="/months">Monthly plans</a>{{end}}{{if .Perms.Can "report" "export"}}<a class="btn outline" href="/reports/ytd.csv?from={{.From}}&to={{.To}}&level={{.Mode}}">Export CSV</a>{{end}}</div></section>
<div class="tabs"><a class="tab {{if eq .Mode "monthly"}}is-active{{end}}" href="/reports/monthly?from={{.From}}&to={{.To}}">Monthly</a><a class="tab {{if eq .Mode "projects"}}is-active{{end}}" href="/reports/projects?from={{.From}}&to={{.To}}">Projects</a><a class="tab {{if eq .Mode "heads"}}is-active{{end}}" href="/reports/heads?from={{.From}}&to={{.To}}">Heads</a></div>
<form class="toolbar" method="get"><label>From<input type="month" name="from" value="{{.From}}"></label><label>To<input type="month" name="to" value="{{.To}}"></label><button>Run</button><a class="btn outline" href="/reports/{{.Mode}}?from={{.Month}}&to={{.Month}}">Current month</a></form>
<div class="metric-strip"><div class="metric"><span class="metric-label">Budget</span><span class="metric-value">{{short .Summary.Budget}}</span></div><div class="metric"><span class="metric-label">Actual</span><span class="metric-value">{{short .Summary.Actual}}</span></div><div class="metric"><span class="metric-label">Remaining</span><span class="metric-value {{varClass .Summary.Variance}}">{{short .Summary.Variance}}</span></div><div class="metric"><span class="metric-label">Used</span><span class="metric-value">{{.Summary.UsedPercent}}</span></div></div>
<div class="table-wrap"><table class="t-cards"><thead><tr><th>Period</th>{{if ne .Mode "monthly"}}<th>Project</th>{{end}}{{if eq .Mode "heads"}}<th>Head</th>{{end}}<th class="num">Budget</th><th class="num">Actual</th><th class="num">Remaining</th><th class="num">Used</th></tr></thead><tbody>{{range .Reports}}<tr><td class="t-lead" data-label="Period">{{.Period}}</td>{{if ne $.Mode "monthly"}}<td data-label="Project">{{.Project}}</td>{{end}}{{if eq $.Mode "heads"}}<td data-label="Head">{{.Head}}</td>{{end}}<td class="num" data-label="Budget">{{money .Budget}}</td><td class="num" data-label="Actual">{{money .Actual}}</td><td class="num {{varClass .Variance}}" data-label="Remaining">{{money .Variance}}</td><td class="num" data-label="Used">{{usedText .Budget .Actual}}</td></tr>{{else}}<tr><td colspan="7" class="empty" data-label="">No report rows for this range. <a href="/budgets">Set a budget</a> to start comparing.</td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}

{{define "backups"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Operations</div><h1>Backups</h1><p class="sub muted">Create timestamped backups of the SQLite database and payment attachments.</p></div><form method="post" action="/backups" onsubmit="return confirm('Create a fresh backup now?')"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="primary">Create Backup</button></form></section>
<div class="table-wrap"><table class="t-cards"><thead><tr><th>Backup folder</th><th>Status</th></tr></thead><tbody>{{range .Backups}}<tr><td class="t-lead" data-label="Backup folder">{{.}}</td><td data-label="Status"><span class="pill good">Available</span></td></tr>{{else}}<tr><td colspan="2" class="empty" data-label="">No backups created yet. Use Create Backup above.</td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}

{{/* Admin notification rules — mockups/screens/admin-notifications.html.

     In-app delivery is a static "On" pill, not a control: it always fires, and
     a switch that did nothing would be a lie. Email is the only inline toggle.
     Everything else — the include flags, fixed To/CC, subject and message —
     lives in a per-row .overlay > .sheet editor.

     There is no password field anywhere on this screen by design; the SMTP
     password comes from the environment and is never stored. */}}
{{define "admin_notifications"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Administration</div>
    <h1>Notification rules</h1>
    <p class="sub">Who is told what, and how. In-app notifications always fire; email is opt-in per event.</p>
  </div>
</section>

{{/* No callout here: .Error and .Notice are the layout's one flash (ux-2). A
     refused rule reports inside its own reopened sheet instead. */}}
<fieldset>
  <legend>Email delivery</legend>
  <form method="post" action="/admin/notifications/smtp">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="form-grid">
      <div class="field span-6"><label for="smtp_host">SMTP host</label><input id="smtp_host" name="smtp_host" value="{{.MailCfg.SMTPHost}}" placeholder="smtp.example.com"></div>
      <div class="field span-2 m-half"><label for="smtp_port">Port</label><input id="smtp_port" name="smtp_port" type="number" inputmode="numeric" value="{{.MailCfg.SMTPPort}}"></div>
      <div class="field span-4 m-half"><label for="smtp_username">Username</label><input id="smtp_username" name="smtp_username" value="{{.MailCfg.SMTPUsername}}"></div>
      <div class="field span-4 m-half"><label for="smtp_from_name">From name</label><input id="smtp_from_name" name="smtp_from_name" value="{{.MailCfg.SMTPFromName}}"></div>
      <div class="field span-4 m-half"><label for="smtp_from_addr">From address</label><input id="smtp_from_addr" name="smtp_from_addr" value="{{.MailCfg.SMTPFromAddr}}"></div>
      <div class="field span-4 m-half"><label for="base_url">Base URL for links</label><input id="base_url" name="base_url" value="{{.MailCfg.BaseURL}}" placeholder="https://budget.example.com"></div>
      <div class="field span-12"><label for="management_recipients">Management copy list</label><input id="management_recipients" name="management_recipients" value="{{.MailCfg.ManagementRecipients}}" placeholder="one@example.com, two@example.com">
        <span class="hint">Copied on approvals and on urgent requests once they are approved.</span></div>
    </div>
    <p class="hint" style="margin:10px 0 0">The SMTP password is read from the <b>FERVID_SMTP_PASSWORD</b> environment variable and is never stored in the database. There is deliberately no field for it here.</p>
    <div class="row-end"></div>
    <div class="stack-8" style="margin-top:10px"><button class="btn primary" type="submit">Save email settings</button></div>
  </form>
  <form method="post" action="/admin/notifications/test" style="margin-top:10px">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <div class="form-grid">
      <div class="field span-8"><label for="test_to">Send a test email to</label><input id="test_to" name="test_to" value="{{.User.Email}}"></div>
      <div class="field span-4 m-half" style="align-self:end"><button class="btn outline" type="submit">Send a test email</button></div>
    </div>
  </form>
</fieldset>

<div class="table-wrap"><table class="t-cards">
  <thead><tr><th>Event</th><th class="c">In-app</th><th class="c">Email</th><th>Goes to</th><th>Fixed To / CC</th><th class="c">Edit</th></tr></thead>
  <tbody>{{range .NotifSettings}}<tr>
    <td class="t-lead" data-label="Event">{{.Label}}<span class="t-sub">{{.Event}}</span></td>
    <td class="c" data-label="In-app"><span class="pill good no-dot">On</span></td>
    <td class="c" data-label="Email">{{if .EmailEnabled}}<span class="pill good no-dot">On</span>{{else}}<span class="pill neutral no-dot">Off</span>{{end}}</td>
    <td data-label="Goes to">{{.Audience}}</td>
    <td data-label="Fixed To / CC">{{if or .ToRecipients .CcRecipients}}{{if .ToRecipients}}{{.ToRecipients}}{{end}}{{if .CcRecipients}} / {{.CcRecipients}}{{end}}{{else}}—{{end}}</td>
    <td class="c" data-label="Edit">{{if $.Perms.Can "notification" "edit"}}<button class="btn small outline" data-open="ev-{{.Event}}">Edit</button>{{else}}—{{end}}</td>
  </tr>{{else}}<tr><td colspan="6" class="empty" data-label="">No notification events are configured.</td></tr>{{end}}</tbody>
</table></div>

{{/* The hidden attribute, like every other .overlay in the product (approve-sheet,
     return-sheet, reject-sheet, hold-sheet). .overlay is
     position:fixed;inset:0;z-index:50;display:grid and there is no
     .overlay[hidden] rule — author-stylesheet origin beats the user agent's
     [hidden]{display:none} whatever the specificity — so without the attribute all
     twenty-one sheets laid out full-viewport at once, later siblings painted over
     earlier ones at equal z-index, and only the last one was reachable by a
     pointer. Every other event's Edit button and the SMTP form's own Save button
     sat underneath the stack (F-F-01). openDialog/closeDialog already toggle the
     attribute, so restoring it is the whole fix and needs no CSS. */}}
{{if .Perms.Can "notification" "edit"}}
{{range .NotifSettings}}
{{/* $d is what the sheet's fields show: the stored rule, or — for the one rule
     just refused — what the admin typed, so a typo costs them nothing (ux-2).
     data-reopen asks fervid-app.js to open that one sheet on load. */}}
{{$d := .}}{{$refused := and $.NotifDraft (eq $.NotifDraft.Event .Event)}}{{if $refused}}{{$d = $.NotifDraft}}{{end}}
<div class="overlay" id="ev-{{.Event}}" hidden{{if $refused}} data-reopen{{end}}>
  <form class="sheet" method="post" action="/admin/notifications/events/{{.Event}}">
    <input type="hidden" name="csrf" value="{{$.CSRF}}">
    <div class="sh-head">
      <div><b>{{.Label}}</b><span class="sh-sub">{{.Audience}}</span></div>
      <button class="sh-close" type="button" data-close="ev-{{.Event}}" aria-label="Close">✕</button>
    </div>
    <div class="sh-body stack-12">
      {{if $refused}}<div class="banner warn" role="alert"><span class="b-ico" aria-hidden="true">⚠</span><div><b>That did not save</b><p>{{$.NotifDraftError}}</p></div></div>{{end}}
      <label class="checkline"><input type="checkbox" name="email_enabled" {{check $d.EmailEnabled}}> Also send an email for this event</label>
      <div class="field"><span class="flabel">Who it goes to</span>
        <label class="checkline"><input type="checkbox" name="include_requester" {{check $d.IncludeRequester}}> The requester</label>
        <label class="checkline"><input type="checkbox" name="include_manager" {{check $d.IncludeManager}}> The approver</label>
        <label class="checkline"><input type="checkbox" name="include_accounts" {{check $d.IncludeAccounts}}> The Accounts group</label>
      </div>
      <div class="field"><label for="to-{{.Event}}" class="flabel">Always also send To</label><input id="to-{{.Event}}" name="to_recipients" value="{{$d.ToRecipients}}" placeholder="one@example.com, two@example.com"></div>
      <div class="field"><label for="cc-{{.Event}}" class="flabel">Always copy (Cc)</label><input id="cc-{{.Event}}" name="cc_recipients" value="{{$d.CcRecipients}}"></div>
      <div class="field"><label for="sub-{{.Event}}" class="flabel">Subject</label><input id="sub-{{.Event}}" name="subject_template" value="{{$d.SubjectTemplate}}"></div>
      <div class="field"><label for="body-{{.Event}}" class="flabel">Message</label><textarea id="body-{{.Event}}" name="body_template" rows="6">{{$d.BodyTemplate}}</textarea></div>
      <p class="hint">Available fields: {{range $i, $f := $.NotifFields}}{{if $i}}, {{end}}&#123;&#123;{{$f}}&#125;&#125;{{end}}. Anything else is rejected when you save, so a typo cannot reach an inbox.</p>
    </div>
    <div class="sh-foot">
      <span class="row-end"></span>
      <button class="btn outline" type="button" data-close="ev-{{.Event}}">Cancel</button>
      <button class="btn primary" type="submit">Save rule</button>
    </div>
  </form>
</div>
{{end}}
{{end}}
{{template "bottom" .}}
{{end}}

{{/* The user's notification centre — mockups/screens/notifications.html.

     Every row here is already addressed to the signed-in user, so the screen
     carries no permission gate. Each .notif is a real POST that marks the row
     read and then redirects to where it points, so the centre works with no
     JavaScript and a click can never lose the read state. */}}
{{define "notifications"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Your activity</div>
    <h1>Notifications</h1>
    {{/* The sub-line names both numbers when they differ. The row query stopped
         at 100 while the "All" count beside it had no cap, so a user past the cap
         read a number the list below could not account for (F-G-037). */}}
    <p class="sub">{{if or .NotifPage.Truncated .NotifOffset}}{{len .Notifs}} of {{.NotifPage.Total}} shown · {{end}}{{if .NotifCounts.Unread}}{{.NotifCounts.Unread}} unread of {{.NotifCounts.All}}{{else}}Everything here is read{{end}}</p>
  </div>
  <div class="pb-actions">{{if .NotifCounts.Unread}}
    <form method="post" action="/notifications/read"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="btn outline" type="submit">Mark all read</button></form>
  {{end}}</div>
</section>

<div class="segmented">
  <a class="{{if eq .NotifScope "all"}}is-active{{end}}" href="/notifications?scope=all">All <span class="n">{{.NotifCounts.All}}</span></a>
  <a class="{{if eq .NotifScope "unread"}}is-active{{end}}" href="/notifications?scope=unread">Unread <span class="n">{{.NotifCounts.Unread}}</span></a>
  <a class="{{if eq .NotifScope "mentions"}}is-active{{end}}" href="/notifications?scope=mentions">Mentions <span class="n">{{.NotifCounts.Mentions}}</span></a>
  <a class="{{if eq .NotifScope "reminders"}}is-active{{end}}" href="/notifications?scope=reminders">Reminders <span class="n">{{.NotifCounts.Reminders}}</span></a>
</div>

<div class="notif-list">
  {{range .Notifs}}
  <a class="notif{{if not .ReadAt}} unread{{end}}" href="/notifications/{{.ID}}/open">
    <span class="n-ico" aria-hidden="true">{{notifGlyph .Kind}}</span>
    <span class="n-main"><b>{{.Title}}</b>{{if .Body}}<p>{{notifBody .Body .Href}}</p>{{end}}</span>
    <time>{{date .CreatedAt}}</time>
  </a>
  {{else}}
  <div class="notif">
    <span class="n-ico" aria-hidden="true">·</span>
    <span class="n-main"><b>Nothing here yet</b><p>You will be told when something needs you.</p></span>
  </div>
  {{end}}
</div>

{{/* Paging, the same shape /requests uses: the cap is real, and it now says so
     and offers the rest rather than dropping rows in silence (F-G-037). Both
     links carry the scope, so crossing a page boundary never widens the filter
     the reader is standing in. */}}
{{if or .NotifPage.Truncated .NotifOffset}}
<div class="cluster" style="margin-top:14px">
  <span class="muted small">Showing {{if .Notifs}}{{add .NotifOffset 1}}–{{add .NotifOffset (len .Notifs)}}{{else}}0{{end}} of {{.NotifPage.Total}}</span>
  <span class="row-end"></span>
  {{if .NotifOffset}}<a class="btn outline" href="/notifications?scope={{.NotifScope}}&amp;offset={{sub0 .NotifOffset .NotifPage.Limit}}">← Newer</a>{{end}}
  {{if .NotifPage.Truncated}}<a class="btn outline" href="/notifications?scope={{.NotifScope}}&amp;offset={{add .NotifOffset (len .Notifs)}}">Older →</a>{{end}}
</div>
{{end}}
{{template "bottom" .}}
{{end}}

{{/* Recoverables dashboard — mockups/screens/recoverables-dashboard.html.

     The mockup's by-counterparty "Type" column ("Government body", "Client", …)
     has no field behind it anywhere in the schema, so it is replaced by the
     distinct categories that counterparty holds — real data that answers the
     same question. The page-banner is not d-only: that would leave a phone with
     no visible h1 and fail the UX sweep. */}}
{{define "recoverables_dashboard"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Money we expect back</div>
    <h1>Recoverable payments</h1>
    <p class="sub">{{money .RecMetrics.OutstandingAmount}} outstanding across {{.RecMetrics.OutstandingCount}} {{plural .RecMetrics.OutstandingCount "payment" "payments"}} · none of it counts as budget spend</p>
  </div>
  <div class="pb-actions">{{if .Perms.Can "recoverable_report" "export"}}<a class="btn outline" href="/recoverables/list.csv">⤓ Export CSV</a>{{end}}</div>
</section>

<div class="banner brand">
  <span class="b-ico" aria-hidden="true">↩</span>
  <div>
    <b>Kept out of budget actuals on purpose</b>
    <p>A deposit is not an expense. These payments never appear in the variance grid or in project spend — they live here until the money comes back.</p>
  </div>
</div>

<div class="metric-strip">
  <div class="metric"><span class="metric-label">Outstanding</span><span class="metric-value">{{short .RecMetrics.OutstandingAmount}}</span><span class="metric-foot">{{.RecMetrics.OutstandingCount}} {{plural .RecMetrics.OutstandingCount "payment" "payments"}}</span></div>
  <div class="metric warn"><span class="metric-label">Past expected return</span><span class="metric-value">{{short .RecMetrics.OverdueAmount}}</span><span class="metric-foot">{{.RecMetrics.OverdueCount}} overdue</span></div>
  <div class="metric"><span class="metric-label">Due in 30 days</span><span class="metric-value">{{short .RecMetrics.DueIn30Amount}}</span><span class="metric-foot">{{.RecMetrics.DueIn30Count}} {{plural .RecMetrics.DueIn30Count "payment" "payments"}}</span></div>
  <div class="metric"><span class="metric-label">Paid out this month</span><span class="metric-value">{{short .RecMetrics.PaidThisMonthAmount}}</span><span class="metric-foot">{{.RecMetrics.PaidThisMonthCount}} {{plural .RecMetrics.PaidThisMonthCount "payment" "payments"}}</span></div>
</div>

<div class="section-head"><h2>By category</h2><a class="more-link" href="/recoverables/list">Full list →</a></div>
<div class="table-wrap"><table class="t-cards">
  <thead><tr><th>Category</th><th class="c">Count</th><th class="num">Outstanding</th><th class="num">Overdue</th><th>Oldest</th></tr></thead>
  <tbody>{{range .ByCategory}}<tr>
    {{/* ?category={id}, which the list honours, not ?q={label}, which searches
         number, counterparty, project and requester — none of which ever contain a
         category name, so the row that said 1 landed on a different number
         (F-G-012). A row with no category ("Uncategorised") maps to no id and is
         correctly not a link. */}}
    <td class="t-lead" data-label="Category">{{$id := index $.CategoryIDs .Label}}{{if $id}}<a href="/recoverables/list?category={{$id}}">{{.Label}}</a>{{else}}{{.Label}}{{end}}</td>
    <td class="c" data-label="Count">{{.Count}}</td>
    <td class="num" data-label="Outstanding">{{money .Outstanding}}</td>
    <td class="num{{if .Overdue}} bad-num{{end}}" data-label="Overdue">{{if .Overdue}}{{money .Overdue}}{{else}}—{{end}}</td>
    <td data-label="Oldest">{{if .Oldest}}{{.Oldest}}{{else}}Not yet paid{{end}}</td>
  </tr>{{else}}<tr><td colspan="5" class="empty" data-label="">No recoverable payments yet.</td></tr>{{end}}</tbody>
  <tfoot><tr>
    <td data-label="">Total</td>
    <td class="c" data-label="Count">{{.RecMetrics.OutstandingCount}}</td>
    <td class="num" data-label="Outstanding">{{money .RecMetrics.OutstandingAmount}}</td>
    <td class="num" data-label="Overdue">{{money .RecMetrics.OverdueAmount}}</td>
    <td data-label="">—</td>
  </tr></tfoot>
</table></div>

<div class="section-head"><h2>By counterparty</h2></div>
<div class="table-wrap"><table class="t-cards">
  <thead><tr><th>Counterparty</th><th>Categories</th><th class="c">Items</th><th class="num">Outstanding</th><th>Expected back</th></tr></thead>
  <tbody>{{range .ByCounterparty}}<tr>
    <td class="t-lead" data-label="Counterparty"><a href="/recoverables/list?counterparty={{urlquery .Label}}">{{.Label}}</a></td>
    <td data-label="Categories">{{if .Detail}}{{.Detail}}{{else}}—{{end}}</td>
    <td class="c" data-label="Items">{{.Count}}</td>
    <td class="num" data-label="Outstanding">{{money .Outstanding}}</td>
    <td data-label="Expected back">{{if .ExpectedBack}}{{.ExpectedBack}}{{else}}No fixed date{{end}}</td>
  </tr>{{else}}<tr><td colspan="5" class="empty" data-label="">No counterparties yet.</td></tr>{{end}}</tbody>
</table></div>

<div class="banner locked">
  <span class="b-ico" aria-hidden="true">i</span>
  <div>
    <b>Tracking the money coming back is not in this version</b>
    <p>A recoverable closes when its payment is made. Repayments, forfeitures, and converting a lost deposit into an expense are deliberately out of scope — they need an accounting adjustment process, not an edit to a completed payment.</p>
  </div>
</div>
{{template "bottom" .}}
{{end}}

{{/* Recoverables list — mockups/screens/recoverables-list.html.

     The desktop .toolbar and the mobile .m-filters are both plain GET forms, so
     filtering works with no JavaScript and the phone is not left with a subset
     of the filters. */}}
{{define "recoverables_list"}}
{{template "top" .}}
<section class="page-banner">
  <div>
    <div class="eyebrow">Recoverable payments</div>
    <h1>All recoverables</h1>
    <p class="sub">Aged against the expected return date · overdue shown in red</p>
  </div>
  <div class="pb-actions">{{if .Perms.Can "recoverable_report" "export"}}<a class="btn outline" href="/recoverables/list.csv?category={{.CategoryID}}&amp;ageing={{.Ageing}}&amp;q={{urlquery .Query}}">⤓ Export CSV</a>{{end}}</div>
</section>

<form class="toolbar" method="get" action="/recoverables/list">
  <div class="field search"><label for="q">Search</label><input id="q" name="q" value="{{.Query}}" placeholder="Counterparty, project, request number…"></div>
  <div class="field"><label for="cat">Category</label><select id="cat" name="category">
    <option value="0">All categories</option>
    {{range .Categories}}<option value="{{.ID}}" {{if eq $.CategoryID .ID}}selected{{end}}>{{.Name}}</option>{{end}}
  </select></div>
  <div class="field"><label for="age">Ageing</label><select id="age" name="ageing">
    <option value="">All</option>
    <option value="overdue" {{select .Ageing "overdue"}}>Overdue</option>
    <option value="due30" {{select .Ageing "due30"}}>Due in 30 days</option>
    <option value="later" {{select .Ageing "later"}}>Due later</option>
    <option value="unpaid" {{select .Ageing "unpaid"}}>Not yet paid</option>
  </select></div>
  <span class="row-end"></span><button class="btn">Apply</button>
</form>

<form class="m-filters" method="get" action="/recoverables/list">
  <input type="hidden" name="category" value="{{.CategoryID}}">
  <input type="hidden" name="ageing" value="{{.Ageing}}">
  <span class="m-search"><input name="q" value="{{.Query}}" placeholder="Search recoverables…" aria-label="Search recoverables"></span>
  <button class="btn filter-btn" type="submit">Filters</button>
</form>

<div class="table-wrap"><table class="t-cards">
  {{/* Status, Requester and Repayment notes are here because the CSV beside this
       table has always carried them and the screen did not, so anybody who had only
       ever looked at the register had no way to know the download said more
       (F-E-04). The table scrolls inside its own .table-wrap, so eleven columns cost
       the page no horizontal scroll. */}}
  <thead><tr><th>Request</th><th>Category</th><th>Counterparty</th><th>Project</th><th class="num">Amount</th><th>Paid on</th><th>Expected back</th><th>Ageing</th><th>Status</th><th>Requester</th><th>Repayment notes</th></tr></thead>
  <tbody>{{range .Recoverables}}<tr>
    <td class="t-lead" data-label="Request"><a href="/recoverables/{{.RequestID}}">{{.Number}}</a></td>
    <td data-label="Category"><span class="pill recoverable">{{.Category}}</span></td>
    <td data-label="Counterparty">{{if .Counterparty}}{{.Counterparty}}{{else}}Not recorded{{end}}</td>
    <td data-label="Project">{{if .Project}}{{.Project}}{{else}}Not project linked{{end}}</td>
    <td class="num" data-label="Amount">{{money .Amount}}</td>
    <td data-label="Paid on">{{if .PaidOn}}{{.PaidOn}}{{else}}Not yet paid{{end}}</td>
    <td data-label="Expected back">{{if .HasReturnDate}}{{.ExpectedReturnDate}}{{else}}No fixed date{{end}}</td>
    <td data-label="Ageing"><span class="pill {{.AgeingTone}}">{{.AgeingLabel}}</span></td>
    <td data-label="Status"><span class="pill {{pillClass .Status}}">{{reqStatus .Status}}</span></td>
    <td data-label="Requester">{{.Requester}}</td>
    <td data-label="Repayment notes">{{if .RepaymentNotes}}{{.RepaymentNotes}}{{else}}Not recorded{{end}}</td>
  </tr>{{else}}<tr><td colspan="11" class="empty" data-label="">No recoverable payments match these filters.</td></tr>{{end}}</tbody>
  <tfoot><tr>
    <td data-label="">{{len .Recoverables}} shown</td>
    <td data-label=""></td><td data-label=""></td><td data-label=""></td>
    <td class="num" data-label="Amount">{{money .RecoverableTotal}}</td>
    <td data-label=""></td><td data-label=""></td><td data-label=""></td>
    <td data-label=""></td><td data-label=""></td><td data-label=""></td>
  </tr></tfoot>
</table></div>
{{template "bottom" .}}
{{end}}

{{/* Recoverable detail — mockups/screens/recoverable-detail.html.

     The history is Phase 2's merged RequestThread, not a second rendering of
     the same events: a recoverable is a payment request, and its story should
     read identically wherever it is opened. The comment box posts to Phase 2's
     existing route rather than introducing a parallel conversation. */}}
{{define "recoverable_detail"}}
{{template "top" .}}
<div class="req-head">
  <div class="rh-top"><span class="rh-no">{{.Request2.Number}}</span><span class="rh-amt">{{money .Recoverable.Amount}}</span></div>
  <h1>{{.Request2.Purpose}}</h1>
  <p class="rh-meta">Raised by {{.Request2.RequesterName}}{{if .Recoverable.PaidOn}} · paid {{.Recoverable.PaidOn}}{{end}} · <span class="pill recoverable">Recoverable · {{.Recoverable.Category}}</span></p>
  <div class="rh-status"><span class="pill {{pillClass .Request2.Status}}">{{reqStatus .Request2.Status}}</span><span class="pill {{.Recoverable.AgeingTone}}">{{.Recoverable.AgeingLabel}}</span></div>
</div>

{{if .Recoverable.Overdue}}
<div class="banner warn">
  <span class="b-ico" aria-hidden="true">◷</span>
  <div>
    <b>Past its expected return date</b>
    <p>Expected {{.Recoverable.ExpectedReturnDate}}. This is a reporting flag only — chasing the money and recording its return happen outside this version.</p>
  </div>
</div>
{{end}}

<div class="card">
  <div class="card-head"><h2>Recoverable details</h2><span class="pill recoverable no-dot">Not in budget actuals</span></div>
  <dl class="dl">
    <div><dt>Amount</dt><dd class="big">{{money .Recoverable.Amount}}</dd></div>
    <div><dt>Category</dt><dd>{{.Recoverable.Category}}</dd></div>
    <div><dt>Counterparty</dt><dd>{{if .Request2.Counterparty}}{{.Request2.Counterparty}}{{else}}Not recorded{{end}}</dd></div>
    <div><dt>Related project</dt><dd>{{if .Request2.Project}}{{.Request2.Project}}{{else}}Not project linked{{end}}</dd></div>
    <div><dt>Expected return</dt><dd>{{if .Recoverable.HasReturnDate}}{{.Recoverable.ExpectedReturnDate}}{{else}}No fixed date{{end}}</dd></div>
    <div><dt>Ageing</dt><dd>{{.Recoverable.AgeingLabel}}</dd></div>
    <div style="grid-column:1/-1"><dt>Refund terms</dt><dd>{{if .Request2.RepaymentNotes}}{{.Request2.RepaymentNotes}}{{else}}Not recorded{{end}}</dd></div>
  </dl>
</div>

<div class="section-head"><h2>Payment</h2></div>
<div class="card">
{{if .HasPayment}}
  <dl class="dl">
    <div><dt>Paid on</dt><dd>{{.RecPayment.PaidOn}}</dd></div>
    <div><dt>Amount paid</dt><dd class="big">{{money .RecPayment.Amount}}</dd></div>
    <div><dt>Mode</dt><dd>{{if .RecPayment.PaymentMode}}{{paymentMode .RecPayment.PaymentMode}}{{else}}Not recorded{{end}}</dd></div>
    <div><dt>Reference</dt><dd class="num">{{if .RecPayment.ReferenceNo}}{{.RecPayment.ReferenceNo}}{{else}}Not recorded{{end}}</dd></div>
    <div><dt>Recorded by</dt><dd>{{.RecPayment.EnteredByName}}</dd></div>
    <div><dt>Payee</dt><dd>{{.RecPayment.VendorPayee}}</dd></div>
  </dl>
{{else}}
  <p class="empty">Approved but not yet paid. The money has not left, so nothing is outstanding against a counterparty yet.</p>
{{end}}
</div>

<div class="section-head"><h2>History and conversation</h2></div>
<ol class="thread">
  {{range .Thread}}
  <li{{if eq .Kind "comment"}} class="is-comment{{if eq .ActorID $.User.ID}} is-me{{end}}"{{end}}>
    <span class="tl-dot {{threadDot .}}" aria-hidden="true">{{threadGlyph .}}</span>
    <div class="tl-head"><b>{{if eq .Kind "comment"}}{{.ActorName}}{{else}}{{.Title}}{{end}}</b><time>{{date .CreatedAt}}</time></div>
    <div class="tl-body">
      {{if eq .Kind "comment"}}<p>{{.Body}}</p>{{end}}
      {{if eq .Kind "attachment"}}<span class="tl-file">📎 {{.FileName}} · {{fileSize .FileSize}}</span>{{end}}
      {{if .Changes}}<div class="tl-change">{{range .Changes}}{{threadField .Field}} <span class="was">{{threadValue .Field .Was}}</span> → <span class="now">{{threadValue .Field .Now}}</span><br>{{end}}</div>{{end}}
    </div>
  </li>
  {{else}}<li><span class="tl-dot" aria-hidden="true">·</span><div class="tl-body muted">Nothing has happened yet.</div></li>{{end}}
</ol>

{{if .Perms.Can "request" "comment"}}
<form class="comment-box" method="post" action="/requests/{{.Request2.ID}}/comment">
  <input type="hidden" name="csrf" value="{{.CSRF}}">
  <label for="cmt" class="flabel">Add a comment</label>
  <textarea id="cmt" name="body" placeholder="Record what you heard from the counterparty." required></textarea>
  <div class="cb-actions"><span class="row-end"></span><button class="btn primary small" type="submit">Post comment</button></div>
</form>
{{end}}

<div class="action-bar">
  <span class="ab-note d-only">Recording the refund is out of scope for this version.</span>
  <span class="row-end"></span>
  <a class="btn outline" href="/recoverables/list">Back to list</a>
  {{if .Perms.Can "recoverable_report" "export"}}<a class="btn" href="/recoverables/list.csv?q={{urlquery .Request2.Number}}">⤓ Export this record</a>{{end}}
</div>
{{template "bottom" .}}
{{end}}
`
