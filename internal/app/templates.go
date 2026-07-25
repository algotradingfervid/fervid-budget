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
  <div class="side-user"><span class="avatar">{{.Initials}}</span><span><strong>{{.User.Name}}</strong><small>{{roleText .User.Role}}</small></span></div>
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
<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Login - Fervid Budget</title><link rel="stylesheet" href="/static/fervid-ds.css"></head>
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
  <form class="toolbar" method="get" action="/" aria-label="Grid filters">
    <label>Month<input class="input" type="month" name="month" value="{{.Month}}"></label>
    <label>Status<select class="select" name="status"><option value="all">All</option><option value="unbudgeted" {{select .Status "unbudgeted"}}>Unbudgeted spend</option><option value="over" {{select .Status "over"}}>Over budget</option><option value="under" {{select .Status "under"}}>Under budget</option><option value="on-track" {{select .Status "on-track"}}>On track</option><option value="not-paid" {{select .Status "not-paid"}}>Not paid</option></select></label>
    <label class="search-label">Search<input class="input" name="q" value="{{.Query}}" placeholder="Search head or project..."></label>
    <div class="spacer"></div>
    <button>Apply</button><a class="btn outline" href="/export.csv?month={{.Month}}&status={{.Status}}&q={{.Query}}">⤓ Export</a><a class="btn primary" href="/payments/new?month={{.Month}}">+ Add payment</a>
  </form>
  <div class="legend"><span class="lk"><span class="sw none"></span>Not paid {{.Grid.NotPaid}}</span><span class="lk"><span class="sw warn"></span>Unbudgeted {{.Grid.Unbudgeted}}</span><span class="lk"><span class="sw ok"></span>Under {{.Grid.Under}}</span><span class="lk"><span class="sw track"></span>On track {{.Grid.OnTrack}}</span><span class="lk"><span class="sw bad"></span>Over {{.Grid.Over}}</span></div>
</div>
{{if .Grid.Locked}}<div class="locked">Month {{.Month}} is locked by {{.Grid.Lock.ActorName}}. Reason: {{.Grid.Lock.Reason}}</div>{{end}}
<div class="gridwrap"><div class="card scroll"><table class="matrix grid"><caption class="sr-only">Budget versus actuals by project and head</caption><thead><tr><th class="sticky project-col">Project</th><th class="sticky head-col">Head</th><th class="c">Due</th><th class="num">Budget</th><th class="num">Actual</th><th class="num">Remaining ₹</th><th class="num">Remaining %</th><th class="c">Used</th><th class="c">Status</th><th class="c">Action</th></tr></thead><tbody>{{range .Grid.Groups}}<tr class="project-row" data-project-id="{{.ProjectID}}"><td class="sticky project-col" colspan="2"><button class="project-toggle" type="button" data-project-toggle="{{.ProjectID}}" aria-expanded="true"><span class="chev" aria-hidden="true">▾</span><span class="project-title">{{.Project}}</span><span class="project-count">{{len .Rows}} heads</span></button></td><td></td><td class="num">{{money .Total.Budget}}</td><td class="num">{{money .Total.Actual}}</td><td class="num {{varClass .Total.Variance}}">{{money .Total.Variance}}</td><td class="num">{{remainingText .Total.Budget .Total.Actual}}</td><td><div class="usedcell"><div class="vbar {{barClass (statusFor .Total.Budget .Total.Actual)}}"><i style="--pct:{{usedPct .Total.Budget .Total.Actual}}"></i><span class="mark"></span></div><span class="pctxt">{{usedText .Total.Budget .Total.Actual}}</span></div></td><td></td><td></td></tr>{{range .Rows}}<tr class="head {{if eq .Status "not-paid"}}unpaid{{end}}" data-project-row="{{.ProjectID}}"><td class="sticky project-col project-spacer" aria-hidden="true"></td><td class="sticky head-col"><span class="hname">{{.Head}}</span></td><td class="c due"><span class="duepill {{dueClass .DueDay $.Month .Status}}">{{dueText .DueDay $.Month .Status}}</span><small>{{.DueDay}}</small></td><td class="num">{{money .Budget}}</td><td class="num">{{money .Actual}}</td><td class="num {{varClass .Variance}}">{{money .Variance}}</td><td class="num">{{remainingText .Budget .Actual}}</td><td><div class="usedcell"><div class="vbar {{barClass .Status}}"><i style="--pct:{{usedPct .Budget .Actual}}"></i><span class="mark"></span></div><span class="pctxt">{{usedText .Budget .Actual}}</span></div></td><td class="c"><span class="pill {{.Status}}">{{statusText .Status}}</span></td><td class="c">{{if and .Active (not $.Grid.Locked)}}<a class="btn small" href="/payments/new?month={{$.Month}}&head_id={{.HeadID}}">Add</a>{{else if $.Grid.Locked}}<small class="muted">Locked</small>{{else}}<small class="muted">Retired</small>{{end}}</td></tr>{{end}}{{else}}<tr><td colspan="10" class="empty">No heads found. Add projects and heads to begin.</td></tr>{{end}}</tbody><tfoot><tr class="total"><td colspan="2"><span class="tlabel">Company total</span></td><td class="c">—</td><td class="num">{{money .Grid.Total.Budget}}</td><td class="num">{{money .Grid.Total.Actual}}</td><td class="num {{varClass .Grid.Total.Variance}}">{{money .Grid.Total.Variance}}</td><td class="num">{{remainingText .Grid.Total.Budget .Grid.Total.Actual}}</td><td><div class="usedcell"><div class="vbar {{barClass (statusFor .Grid.Total.Budget .Grid.Total.Actual)}}"><i style="--pct:{{usedPct .Grid.Total.Budget .Grid.Total.Actual}}"></i><span class="mark"></span></div><span class="pctxt">{{usedText .Grid.Total.Budget .Grid.Total.Actual}}</span></div></td><td></td><td></td></tr></tfoot></table></div></div>
<section class="split"><div><h2>Recent Payments for {{.Month}}</h2><div class="table-wrap"><table><thead><tr><th>Date</th><th>Project / Head</th><th>Amount</th><th>Payee</th></tr></thead><tbody>{{range .Payments}}<tr><td>{{.PaidOn}}</td><td><a href="/payments/{{.ID}}">{{.Project}} / {{.Head}}</a></td><td class="num">{{money .Amount}}</td><td>{{.VendorPayee}}</td></tr>{{else}}<tr><td colspan="4" class="empty">No payments in this month.</td></tr>{{end}}</tbody></table></div></div>{{if .Perms.Can "month" "lock"}}<div><h2>Month Close</h2>{{if .Grid.Locked}}<p class="muted">Unlocking reopens this month for budget and payment changes.</p><form method="post" action="/months/{{.Month}}/unlock" onsubmit="return confirm('Unlock {{.Month}} and allow changes again?')"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Unlock reason<input name="reason" required></label><button>Unlock Month</button></form>{{else}}<p class="muted">{{.CloseGrid.NotPaid}} heads are unpaid, {{.CloseGrid.Unbudgeted}} have unbudgeted spend, and {{.CloseGrid.Over}} are over budget. Review before closing.</p><form method="post" action="/months/{{.Month}}/lock" onsubmit="return confirm('Lock {{.Month}}? Budgets and payments will become read-only.')"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Lock reason<input name="reason" required></label><button>Lock Month</button></form>{{end}}</div>{{end}}</section>
{{template "bottom" .}}
{{end}}

{{define "payment_form"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Daily entry</div><h1>{{.Title}}</h1><p class="sub muted">Record the head, date, amount, reference details, and proof in one pass.</p></div><a class="btn outline" href="/">Back to grid</a></section>
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
<div class="table-wrap payments-table"><table><thead><tr><th>Date</th><th>Project / Head</th><th class="num">Amount</th><th>Payee</th><th>Mode</th><th>Reference</th><th>Entered by</th><th>Status</th><th>Actions</th></tr></thead><tbody>{{range .Payments}}<tr class="{{if voided .}}is-voided{{end}}"><td>{{.PaidOn}}</td><td><a href="/payments/{{.ID}}">{{.Project}} / {{.Head}}</a></td><td class="num">{{money .Amount}}</td><td>{{.VendorPayee}}</td><td>{{paymentMode .PaymentMode}}</td><td><span class="nowrap">{{.InvoiceNo}}</span>{{if .ReferenceNo}}<br><small>{{.ReferenceNo}}</small>{{end}}</td><td>{{.EnteredByName}}</td><td>{{if voided .}}<span class="pill bad">Removed</span>{{else}}<span class="pill good">Active</span>{{end}}</td><td class="actions-cell"><a class="btn small" href="/payments/{{.ID}}">View</a>{{if and ($.Perms.Can "payment" "edit") (not $.Locked) (not (voided .))}}<a class="btn small outline" href="/payments/{{.ID}}/edit">Edit</a>{{if $.Perms.Can "payment" "void"}}<details class="inline-danger"><summary>Remove</summary><form method="post" action="/payments/{{.ID}}/void" onsubmit="return confirm('Remove payment #{{.ID}} from actual totals?')"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="next" value="/payments?month={{$.Month}}&status={{$.Status}}&q={{urlquery $.Query}}"><label>Reason<input name="reason" required></label><button class="danger">Remove</button></form></details>{{end}}{{else if voided .}}<small class="muted">{{.VoidReason}}</small>{{else if $.Locked}}<small class="muted">Locked</small>{{end}}</td></tr>{{else}}<tr><td colspan="9" class="empty">No payments match these filters.</td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}

{{define "payment_detail"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Payment record</div><h1>Payment #{{.Payment.ID}}</h1><p class="sub muted">{{.Payment.Project}} / {{.Payment.Head}} · {{money .Payment.Amount}}</p></div>{{if voided .Payment}}<span class="pill bad">Voided</span>{{else if .Locked}}<span class="pill warn">Locked</span>{{else if .Perms.Can "payment" "edit"}}<a class="btn outline" href="/payments/{{.Payment.ID}}/edit">Edit</a>{{end}}</section>
{{if voided .Payment}}<div class="locked">Voided payment. Reason: {{.Payment.VoidReason}}</div>{{else if .Locked}}<div class="locked">This payment belongs to a locked month and is read-only.</div>{{end}}
<dl class="details detail-grid"><div><dt>Project / Head</dt><dd>{{.Payment.Project}} / {{.Payment.Head}}</dd></div><div><dt>Date</dt><dd>{{.Payment.PaidOn}}</dd></div><div><dt>Amount</dt><dd>{{money .Payment.Amount}}</dd></div><div><dt>Payee</dt><dd>{{.Payment.VendorPayee}}</dd></div><div><dt>Mode</dt><dd>{{paymentMode .Payment.PaymentMode}}</dd></div><div><dt>Invoice</dt><dd>{{.Payment.InvoiceNo}}</dd></div><div><dt>Reference</dt><dd>{{.Payment.ReferenceNo}}</dd></div><div><dt>Entered by</dt><dd>{{.Payment.EnteredByName}} at {{date .Payment.CreatedAt}}</dd></div><div><dt>Remarks</dt><dd>{{.Payment.Remarks}}</dd></div></dl>
<section class="split"><div><h2>Attachments</h2>{{if and (not .Locked) (not (voided .Payment))}}<form class="cluster" method="post" enctype="multipart/form-data" action="/payments/{{.Payment.ID}}/attachments"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="file" name="attachment" aria-label="Attachment" required><button>Upload</button></form>{{end}}<ul class="file-list">{{range .Attachments}}<li><a href="/attachments/{{.ID}}">{{.OriginalName}}</a><small>{{fileSize .SizeBytes}}</small></li>{{else}}<li class="muted">No attachments.</li>{{end}}</ul></div>{{if and (.Perms.Can "payment" "void") (not .Locked) (not (voided .Payment))}}<div class="danger-zone"><h2>Void Payment</h2><p class="muted">Voiding keeps the audit trail but excludes this payment from actual totals.</p><form method="post" action="/payments/{{.Payment.ID}}/void" onsubmit="return confirm('Void payment #{{.Payment.ID}}? This cannot be undone without a new correction.')"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Reason<input name="reason" required></label><button class="danger">Void</button></form></div>{{end}}</section>
<h2>Transaction Audit Trail</h2><ol class="timeline">{{range .Audit}}<li class="timeline-item {{actionClass .Action}}"><div class="audit-meta"><strong>{{actionText .Action}}</strong><span>{{date .CreatedAt}}</span><span>{{.ActorName}}</span></div><p>{{.Summary}}</p>{{if or (hasText .BeforeJSON) (hasText .AfterJSON)}}<details><summary>Before / after</summary>{{if hasText .BeforeJSON}}<pre>{{jsonPretty .BeforeJSON}}</pre>{{end}}{{if hasText .AfterJSON}}<pre>{{jsonPretty .AfterJSON}}</pre>{{end}}</details>{{end}}</li>{{else}}<li class="muted">No audit entries.</li>{{end}}</ol>
{{template "bottom" .}}
{{end}}

{{define "months"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Month control</div><h1>Monthly Plans</h1><p class="sub muted">Create each month, review prior months, and open locked history whenever needed.</p></div><a class="btn outline" href="/reports/monthly">Reports</a></section>
<form class="toolbar setup-form" method="post" action="/months"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>New month<input type="month" name="target_month" value="{{.TargetMonth}}" required></label><label>Plan type<select name="source_mode"><option value="copy">Copy from month</option><option value="blank">Start blank</option></select></label><label>Source month<input type="month" name="source_month" value="{{.SourceMonth}}"></label><button class="primary">Create Month</button></form>
<div class="table-wrap"><table><thead><tr><th>Month</th><th>Status</th><th>Source</th><th class="num">Budget</th><th class="num">Actual</th><th class="num">Remaining</th><th class="num">Used</th><th>Created</th><th>Actions</th></tr></thead><tbody>{{range .MonthPlans}}<tr><td><strong>{{.Month}}</strong><br><small>{{.HeadCount}} heads</small></td><td><span class="pill {{.Status}}">{{planStatusText .Status}}</span>{{if .LockedAt}}<br><small>{{datep .LockedAt}}</small>{{end}}</td><td>{{if .SourceMonth}}{{.SourceMonth}}{{else}}<span class="muted">Manual</span>{{end}}</td><td class="num">{{money .Budget}}</td><td class="num">{{money .Actual}}</td><td class="num {{varClass .Variance}}">{{money .Variance}}</td><td class="num">{{.UsedPercent}}</td><td>{{if .CreatedAt}}{{datep .CreatedAt}}{{else}}<span class="muted">Imported history</span>{{end}}</td><td class="actions-cell"><a class="btn small" href="/?month={{.Month}}">Grid</a><a class="btn small outline" href="/budgets?month={{.Month}}">Budget</a><a class="btn small outline" href="/payments?month={{.Month}}">Payments</a><a class="btn small outline" href="/reports/heads?from={{.Month}}&to={{.Month}}">Report</a></td></tr>{{else}}<tr><td colspan="9" class="empty">No monthly plans yet.</td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}

{{define "budgets"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Monthly plan</div><h1>Budgets</h1><p class="sub muted">Edit planned spend per head for the selected month.</p></div>{{if .Grid.Locked}}<span class="pill warn">Locked</span>{{end}}</section>
<form class="toolbar" method="get"><label>Month<input type="month" name="month" value="{{.Month}}"></label><button>Open</button><a class="btn outline" href="/months">Month history</a><a class="btn outline" href="/?month={{.Month}}">View grid</a></form>{{if .Grid.Locked}}<div class="locked">This month is locked. Unlock it before changing budgets.</div>{{end}}
<form class="budget-editor" method="post" action="/budgets"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="month" value="{{.Month}}"><table><thead><tr><th>Project</th><th>Head</th><th>Status</th><th>Budget</th><th class="num">Actual</th><th class="num">Used</th></tr></thead><tbody>{{range .Grid.Rows}}<tr><td>{{.Project}}</td><td>{{.Head}}</td><td>{{if .Active}}<span class="pill good">Active</span>{{else}}<span class="pill neutral">Retired</span>{{end}}</td><td><input name="budget_{{.HeadID}}" value="{{if $.BudgetInputs}}{{index $.BudgetInputs .HeadID}}{{else}}{{money .Budget}}{{end}}" {{if or $.Grid.Locked (not .Active)}}disabled{{end}}></td><td class="num">{{money .Actual}}</td><td class="num">{{usedText .Budget .Actual}}</td></tr>{{else}}<tr><td colspan="6" class="empty">No active heads.</td></tr>{{end}}</tbody></table><div class="form-actions"><button class="primary" {{if .Grid.Locked}}disabled{{end}}>Save Budgets</button></div></form>
{{template "bottom" .}}
{{end}}

{{define "projects"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Setup</div><h1>Projects</h1><p class="sub muted">Manage project buckets used by the grid and payment entry.</p></div></section>
<form class="toolbar setup-form" method="post" action="/projects"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Project name<input name="name" required></label><label>Order<input name="sort_order" type="number" value="0"></label><label class="checkline"><input type="checkbox" name="active" checked> Active</label><button class="primary">Add Project</button></form>
<div class="table-wrap"><table><thead><tr><th>Name</th><th>Status</th><th>Order</th><th>Update</th></tr></thead><tbody>{{range .Projects}}<tr><td><input form="project-{{.ID}}" name="name" value="{{.Name}}" required></td><td><label class="checkline"><input form="project-{{.ID}}" type="checkbox" name="active" {{check .Active}}> {{boolText .Active}}</label></td><td><input form="project-{{.ID}}" name="sort_order" type="number" value="{{.SortOrder}}"></td><td><form id="project-{{.ID}}" method="post" action="/projects"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"></form><button form="project-{{.ID}}">Save</button></td></tr>{{else}}<tr><td colspan="4" class="empty">No projects yet.</td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}

{{define "heads"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Setup</div><h1>Heads</h1><p class="sub muted">Maintain spend heads, due days, and sort order.</p></div></section>
<form class="toolbar setup-form" method="post" action="/heads"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Project<select name="project_id" required>{{range .Projects}}<option value="{{.ID}}">{{.Name}}</option>{{end}}</select></label><label>Head name<input name="name" required></label><label>Due day<input name="due_day" type="number" min="1" max="31" placeholder="5"></label><label>Order<input name="sort_order" type="number" value="0"></label><label class="checkline"><input type="checkbox" name="active" checked> Active</label><button class="primary">Add Head</button></form>
<div class="table-wrap"><table><thead><tr><th>Project</th><th>Head</th><th>Due</th><th>Status</th><th>Order</th><th>Update</th></tr></thead><tbody>{{range .Heads}}{{$head := .}}<tr><td><select form="head-{{.ID}}" name="project_id">{{range $.Projects}}<option value="{{.ID}}" {{if eq $head.ProjectID .ID}}selected{{end}}>{{.Name}}</option>{{end}}</select></td><td><input form="head-{{.ID}}" name="name" value="{{.Name}}" required></td><td><input form="head-{{.ID}}" name="due_day" type="number" min="1" max="31" value="{{.DueDay}}"></td><td><label class="checkline"><input form="head-{{.ID}}" type="checkbox" name="active" {{check .Active}}> {{boolText .Active}}</label></td><td><input form="head-{{.ID}}" name="sort_order" type="number" value="{{.SortOrder}}"></td><td><form id="head-{{.ID}}" method="post" action="/heads"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"></form><button form="head-{{.ID}}">Save</button></td></tr>{{else}}<tr><td colspan="6" class="empty">No heads yet.</td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}

{{/* Every <td> carries data-label, including the trailing action cell, because
     table.t-cards uses that attribute to caption each field once the table
     restacks into cards below 860px. Editing lives in one sheet per user; the
     legacy users.role rides along as a hidden input because UpdateUser still
     validates it and RequireAnotherActiveAdmin still guards it. The approver
     <select> omits the user themselves and SetUserDefaultApprover rejects them
     again server-side — a hidden option is not validation. */}}
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
        <td class="c" data-label=""><button class="btn small outline" type="button" data-open="user-{{.ID}}">Edit</button></td>
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
          <select id="u-apr-{{.ID}}" name="default_approver_id">
            <option value="0">None</option>
            {{$self := .ID}}{{$chosen := .DefaultApproverID}}{{range $.Approvers}}{{if ne .ID $self}}<option value="{{.ID}}" data-approver-for="{{$self}}" {{if eq .ID $chosen}}selected{{end}}>{{.Name}}</option>{{end}}{{end}}
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

<div class="overlay" id="user-new" hidden>
  <div class="sheet">
    <form class="setup-form" method="post" action="/users">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="sh-head"><div><h2>Add user</h2><p class="sh-sub">They can be given more roles once they exist.</p></div><button class="sh-close" type="button" data-close="user-new">✕</button></div>
      <div class="sh-body stack-12">
        <div class="field"><label for="nu-email">Email</label><input id="nu-email" name="email" type="email" required></div>
        <div class="field"><label for="nu-name">Name</label><input id="nu-name" name="name" required></div>
        <div class="field"><label for="nu-role">Role</label><select id="nu-role" name="role"><option value="data_entry">Data entry</option><option value="admin">Admin</option></select></div>
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
      <span class="pill good">{{index .RoleUserCounts .Role.ID}} users</span>
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
          {{range .Cells}}<td class="c">{{if .Available}}<input type="checkbox" name="cell" value="{{.Value}}" data-cell="{{.Value}}" aria-label="{{.Label}}" {{if .Granted}}checked{{end}}>{{else}}<span class="muted">—</span>{{end}}</td>{{end}}
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
        {{range .Cells}}{{if .Available}}<label class="checkline"><input type="checkbox" name="cell" disabled value="{{.Value}}" data-cell="{{.Value}}" {{if .Granted}}checked{{end}}> {{.Label}}</label>{{end}}{{end}}
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
    <span class="ab-note d-only">Changes apply to all {{index .RoleUserCounts .Role.ID}} users holding this role.</span>
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

{{if not .Role.IsSystem}}
<div class="overlay" id="role-delete" hidden>
  <div class="sheet">
    <form method="post" action="/roles/{{.Role.ID}}/delete">
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <div class="sh-head"><div><h2>Delete {{.Role.Name}}?</h2><p class="sh-sub">{{index .RoleUserCounts .Role.ID}} users hold this role and will lose everything it grants.</p></div><button class="sh-close" type="button" data-close="role-delete">✕</button></div>
      <div class="sh-foot"><button class="btn outline" type="button" data-close="role-delete">Cancel</button><span class="row-end"></span><button class="btn danger" type="submit">Delete role</button></div>
    </form>
  </div>
</div>
{{end}}
{{template "bottom" .}}
{{end}}

{{define "audit"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Evidence</div><h1>Audit Log</h1><p class="sub muted">Review who changed financial records, when, and why.</p></div></section>
<form class="toolbar" method="get"><label>Entity<select name="entity"><option value="">All</option><option value="payment" {{select .AuditEntity "payment"}}>Payment</option><option value="budget" {{select .AuditEntity "budget"}}>Budget</option><option value="budget_month" {{select .AuditEntity "budget_month"}}>Monthly plan</option><option value="month_lock" {{select .AuditEntity "month_lock"}}>Month lock</option><option value="project" {{select .AuditEntity "project"}}>Project</option><option value="head" {{select .AuditEntity "head"}}>Head</option><option value="user" {{select .AuditEntity "user"}}>User</option><option value="report" {{select .AuditEntity "report"}}>Report</option><option value="backup" {{select .AuditEntity "backup"}}>Backup</option></select></label><label>Action<select name="action"><option value="">All</option><option value="create" {{select .AuditAction "create"}}>Created</option><option value="attach" {{select .AuditAction "attach"}}>Attached</option><option value="update" {{select .AuditAction "update"}}>Updated</option><option value="void" {{select .AuditAction "void"}}>Voided</option><option value="lock" {{select .AuditAction "lock"}}>Locked</option><option value="unlock" {{select .AuditAction "unlock"}}>Unlocked</option><option value="export" {{select .AuditAction "export"}}>Exported</option><option value="login" {{select .AuditAction "login"}}>Login</option><option value="login_failed" {{select .AuditAction "login_failed"}}>Failed login</option><option value="logout" {{select .AuditAction "logout"}}>Logout</option></select></label><label>Actor<input name="actor" value="{{.AuditActor}}" placeholder="Name"></label><button>Filter</button><a class="btn outline" href="/audit">Reset</a></form>
<div class="table-wrap"><table><thead><tr><th>When</th><th>Actor</th><th>Entity</th><th>Action</th><th>Summary</th></tr></thead><tbody>{{range .Audit}}<tr><td>{{date .CreatedAt}}</td><td>{{.ActorName}}</td><td>{{entityText .EntityType}}</td><td><span class="pill {{actionClass .Action}}">{{actionText .Action}}</span></td><td>{{.Summary}}{{if or (hasText .BeforeJSON) (hasText .AfterJSON)}}<details><summary>Before / after</summary>{{if hasText .BeforeJSON}}<pre>{{jsonPretty .BeforeJSON}}</pre>{{end}}{{if hasText .AfterJSON}}<pre>{{jsonPretty .AfterJSON}}</pre>{{end}}</details>{{end}}</td></tr>{{else}}<tr><td colspan="5" class="empty">No audit entries match these filters.</td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}

{{define "reports"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Analysis</div><h1>Reports</h1><p class="sub muted">{{.From}} to {{.To}} · {{.Mode}} view</p></div><div class="head-actions">{{if .Perms.Can "budget" "view"}}<a class="btn outline" href="/months">Monthly plans</a>{{end}}<a class="btn outline" href="/reports/ytd.csv?from={{.From}}&to={{.To}}">Export CSV</a></div></section>
<div class="tabs"><a class="tab {{if eq .Mode "monthly"}}is-active{{end}}" href="/reports/monthly?from={{.From}}&to={{.To}}">Monthly</a><a class="tab {{if eq .Mode "projects"}}is-active{{end}}" href="/reports/projects?from={{.From}}&to={{.To}}">Projects</a><a class="tab {{if eq .Mode "heads"}}is-active{{end}}" href="/reports/heads?from={{.From}}&to={{.To}}">Heads</a></div>
<form class="toolbar" method="get"><label>From<input type="month" name="from" value="{{.From}}"></label><label>To<input type="month" name="to" value="{{.To}}"></label><button>Run</button><a class="btn outline" href="/reports/{{.Mode}}?from={{.Month}}&to={{.Month}}">Current month</a></form>
<div class="metric-strip"><div class="metric"><span class="metric-label">Budget</span><span class="metric-value">{{short .Summary.Budget}}</span></div><div class="metric"><span class="metric-label">Actual</span><span class="metric-value">{{short .Summary.Actual}}</span></div><div class="metric"><span class="metric-label">Remaining</span><span class="metric-value {{varClass .Summary.Variance}}">{{short .Summary.Variance}}</span></div><div class="metric"><span class="metric-label">Used</span><span class="metric-value">{{.Summary.UsedPercent}}</span></div></div>
<div class="table-wrap"><table><thead><tr><th>Period</th>{{if ne .Mode "monthly"}}<th>Project</th>{{end}}{{if eq .Mode "heads"}}<th>Head</th>{{end}}<th class="num">Budget</th><th class="num">Actual</th><th class="num">Remaining</th><th class="num">Used</th></tr></thead><tbody>{{range .Reports}}<tr><td>{{.Period}}</td>{{if ne $.Mode "monthly"}}<td>{{.Project}}</td>{{end}}{{if eq $.Mode "heads"}}<td>{{.Head}}</td>{{end}}<td class="num">{{money .Budget}}</td><td class="num">{{money .Actual}}</td><td class="num {{varClass .Variance}}">{{money .Variance}}</td><td class="num">{{usedText .Budget .Actual}}</td></tr>{{else}}<tr><td colspan="7" class="empty">No report rows.</td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}

{{define "backups"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Operations</div><h1>Backups</h1><p class="sub muted">Create timestamped backups of the SQLite database and payment attachments.</p></div><form method="post" action="/backups" onsubmit="return confirm('Create a fresh backup now?')"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="primary">Create Backup</button></form></section>
<div class="table-wrap"><table><thead><tr><th>Backup folder</th><th>Status</th></tr></thead><tbody>{{range .Backups}}<tr><td>{{.}}</td><td><span class="pill good">Available</span></td></tr>{{else}}<tr><td colspan="2" class="empty">No backups created yet.</td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}
`
