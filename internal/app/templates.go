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
<div class="table-wrap payments-table" tabindex="0" role="region" aria-label="Payments table, scrollable"><table><thead><tr><th>Date</th><th>Project / Head</th><th class="num">Amount</th><th>Payee</th><th>Mode</th><th>Reference</th><th>Entered by</th><th>Status</th><th>Actions</th></tr></thead><tbody>{{range .Payments}}<tr class="{{if voided .}}is-voided{{end}}"><td>{{.PaidOn}}</td><td><a href="/payments/{{.ID}}">{{.Project}} / {{.Head}}</a></td><td class="num">{{money .Amount}}</td><td>{{.VendorPayee}}</td><td>{{paymentMode .PaymentMode}}</td><td><span class="nowrap">{{.InvoiceNo}}</span>{{if .ReferenceNo}}<br><small>{{.ReferenceNo}}</small>{{end}}</td><td>{{.EnteredByName}}</td><td>{{if voided .}}<span class="pill bad">Removed</span>{{else}}<span class="pill good">Active</span>{{end}}</td><td class="actions-cell"><a class="btn small" href="/payments/{{.ID}}">View</a>{{if and ($.Perms.Can "payment" "edit") (not $.Locked) (not (voided .))}}<a class="btn small outline" href="/payments/{{.ID}}/edit">Edit</a>{{if $.Perms.Can "payment" "void"}}<details class="inline-danger"><summary>Remove</summary><form method="post" action="/payments/{{.ID}}/void" onsubmit="return confirm('Remove payment #{{.ID}} from actual totals?')"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="next" value="/payments?month={{$.Month}}&status={{$.Status}}&q={{urlquery $.Query}}"><label>Reason<input name="reason" required></label><button class="danger">Remove</button></form></details>{{end}}{{else if voided .}}<small class="muted">{{.VoidReason}}</small>{{else if $.Locked}}<small class="muted">Locked</small>{{end}}</td></tr>{{else}}<tr><td colspan="9" class="empty">No payments match these filters.</td></tr>{{end}}</tbody></table></div>
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
<form class="budget-editor" method="post" action="/budgets"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="month" value="{{.Month}}"><table><thead><tr><th>Project</th><th>Head</th><th>Status</th><th>Budget</th><th class="num">Actual</th><th class="num">Used</th></tr></thead><tbody>{{range .Grid.Rows}}<tr><td>{{.Project}}</td><td>{{.Head}}</td><td>{{if .Active}}<span class="pill good">Active</span>{{else}}<span class="pill neutral">Retired</span>{{end}}</td><td><input name="budget_{{.HeadID}}" aria-label="Budget for {{.Project}} / {{.Head}}" value="{{if $.BudgetInputs}}{{index $.BudgetInputs .HeadID}}{{else}}{{money .Budget}}{{end}}" {{if or $.Grid.Locked (not .Active)}}disabled{{end}}></td><td class="num">{{money .Actual}}</td><td class="num">{{usedText .Budget .Actual}}</td></tr>{{else}}<tr><td colspan="6" class="empty">No active heads.</td></tr>{{end}}</tbody></table><div class="form-actions"><button class="primary" {{if .Grid.Locked}}disabled{{end}}>Save Budgets</button></div></form>
{{template "bottom" .}}
{{end}}

{{define "projects"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Setup</div><h1>Projects</h1><p class="sub muted">Manage project buckets used by the grid and payment entry.</p></div></section>
<form class="toolbar setup-form" method="post" action="/projects"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Project name<input name="name" required></label><label>Order<input name="sort_order" type="number" value="0"></label><label class="checkline"><input type="checkbox" name="active" checked> Active</label><button class="primary">Add Project</button></form>
<div class="table-wrap"><table><thead><tr><th>Name</th><th>Status</th><th>Order</th><th>Update</th></tr></thead><tbody>{{range .Projects}}<tr><td><input form="project-{{.ID}}" name="name" aria-label="Name for {{.Name}}" value="{{.Name}}" required></td><td><label class="checkline"><input form="project-{{.ID}}" type="checkbox" name="active" {{check .Active}}> {{boolText .Active}}</label></td><td><input form="project-{{.ID}}" name="sort_order" type="number" aria-label="Sort order for {{.Name}}" value="{{.SortOrder}}"></td><td><form id="project-{{.ID}}" method="post" action="/projects"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"></form><button form="project-{{.ID}}">Save</button></td></tr>{{else}}<tr><td colspan="4" class="empty">No projects yet.</td></tr>{{end}}</tbody></table></div>
{{template "bottom" .}}
{{end}}

{{define "heads"}}
{{template "top" .}}
<section class="page-banner"><div><div class="eyebrow">Setup</div><h1>Heads</h1><p class="sub muted">Maintain spend heads, due days, and sort order.</p></div></section>
<form class="toolbar setup-form" method="post" action="/heads"><input type="hidden" name="csrf" value="{{.CSRF}}"><label>Project<select name="project_id" required>{{range .Projects}}<option value="{{.ID}}">{{.Name}}</option>{{end}}</select></label><label>Head name<input name="name" required></label><label>Due day<input name="due_day" type="number" min="1" max="31" placeholder="5"></label><label>Order<input name="sort_order" type="number" value="0"></label><label class="checkline"><input type="checkbox" name="active" checked> Active</label><button class="primary">Add Head</button></form>
<div class="table-wrap"><table><thead><tr><th>Project</th><th>Head</th><th>Due</th><th>Status</th><th>Order</th><th>Update</th></tr></thead><tbody>{{range .Heads}}{{$head := .}}<tr><td><select form="head-{{.ID}}" name="project_id" aria-label="Project for {{.Name}}">{{range $.Projects}}<option value="{{.ID}}" {{if eq $head.ProjectID .ID}}selected{{end}}>{{.Name}}</option>{{end}}</select></td><td><input form="head-{{.ID}}" name="name" aria-label="Head name for {{.Name}}" value="{{.Name}}" required></td><td><input form="head-{{.ID}}" name="due_day" type="number" min="1" max="31" aria-label="Due day for {{.Name}}" value="{{.DueDay}}"></td><td><label class="checkline"><input form="head-{{.ID}}" type="checkbox" name="active" {{check .Active}}> {{boolText .Active}}</label></td><td><input form="head-{{.ID}}" name="sort_order" type="number" aria-label="Sort order for {{.Name}}" value="{{.SortOrder}}"></td><td><form id="head-{{.ID}}" method="post" action="/heads"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="id" value="{{.ID}}"></form><button form="head-{{.ID}}">Save</button></td></tr>{{else}}<tr><td colspan="6" class="empty">No heads yet.</td></tr>{{end}}</tbody></table></div>
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
    <p>If the money leaves the company and never comes back, it is an expense. If it is a deposit, a
      guarantee, or something you will repay, pick the type that fits and mark it recoverable on the
      next screen.</p>
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
      <label for="rcategory">Category <span class="req" aria-hidden="true">*</span></label>
      <select id="rcategory" name="recoverable_category" aria-required="true"
              hx-get="/requests/new/fields" hx-include="closest form" hx-target="#form-fields" hx-trigger="change">
        <option value="emd" {{select .Request2.RecoverableCategory "emd"}}>EMD — earnest money deposit</option>
        <option value="pbg" {{select .Request2.RecoverableCategory "pbg"}}>PBG — performance bank guarantee</option>
        <option value="icd" {{select .Request2.RecoverableCategory "icd"}}>ICD — inter-corporate deposit</option>
        <option value="employee_advance" {{select .Request2.RecoverableCategory "employee_advance"}}>Employee advance</option>
        <option value="security_deposit" {{select .Request2.RecoverableCategory "security_deposit"}}>Security deposit</option>
        <option value="other" {{select .Request2.RecoverableCategory "other"}}>Other</option>
      </select>
      <span class="hint">Categories are maintained by your administrator.</span>
    </div>
    <div class="field span-6 m-half">
      <label for="expected-return">Expected return date <span class="req" aria-hidden="true">*</span></label>
      <input id="expected-return" type="date" name="expected_return_date" aria-required="true" value="{{.Request2.ExpectedReturnDate}}">
    </div>
    {{if or (eq .Request2.RecoverableCategory "emd") (eq .Request2.RecoverableCategory "pbg")}}
    <div class="field span-6 m-half" data-when="recoverable_category:emd|pbg">
      <label for="rproject">Related project <span class="req" aria-hidden="true">*</span></label>
      <select id="rproject" name="project_id" aria-required="true">
        <option value="">Choose a project</option>
        {{range .Projects}}<option value="{{.ID}}" {{if eq (deref $.Request2.ProjectID) .ID}}selected{{end}}>{{.Name}}</option>{{end}}
      </select>
      <span class="hint">EMD and PBG always belong to a project.</span>
    </div>
    {{end}}
    {{if or (eq .Request2.RecoverableCategory "icd") (eq .Request2.RecoverableCategory "security_deposit")}}
    <div class="field span-6 m-half" data-when="recoverable_category:icd|security_deposit">
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
        {{range .Projects}}<option value="{{.ID}}" {{if eq (deref $.Request2.ProjectID) .ID}}selected{{end}}>{{.Name}}</option>{{end}}
      </select>
      <span class="hint">Picking a project narrows the heads below to that project's own.</span>
    </div>
    <div class="field span-6 m-half">
      <label for="head">Head <span class="req" aria-hidden="true">*</span></label>
      <select id="head" name="head_id" aria-required="true">
        <option value="">Choose a head</option>
        {{range .Heads}}{{if or (not (deref $.Request2.ProjectID)) (eq .ProjectID (deref $.Request2.ProjectID))}}<option value="{{.ID}}" {{if eq (deref $.Request2.HeadID) .ID}}selected{{end}}>{{.Project}} / {{.Name}}</option>{{end}}{{end}}
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
      <div class="field span-12">
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
      </div>
    </div>
  </fieldset>

  <div id="form-fields">{{template "request_form_fields" .}}</div>

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
        <span class="flabel">Supporting document {{if eq (index .Settings "require_attachments") "1"}}<span class="req" aria-hidden="true">*</span>{{else}}<span class="opt">optional</span>{{end}}</span>
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
        <p class="hint" style="margin:4px 0 0">If nothing happens for three calendar days, this request starts sending a daily reminder to whoever it is waiting on.</p>
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
    <span class="pill {{pillClass $r.Status}}">{{reqStatus $r.Status}}</span>
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
    <p class="sub">{{len .Requests}} shown · sorted by who is holding them up</p>
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
      <div class="field"><label for="ap-note">Note <span class="opt">optional</span></label><textarea id="ap-note" name="note" placeholder="Recorded in the history and visible to everyone."></textarea></div>
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
    <span class="pill {{pillClass .Request2.Status}}">{{reqStatus .Request2.Status}}</span>
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

{{if and (eq .Request2.RequesterID .User.ID) (eq .Request2.Status "approved")}}
<div class="banner locked">
  <span class="b-ico" aria-hidden="true">🔒</span>
  <div>
    <b>Approved requests are locked</b>
    <p>You can no longer edit this. If it should not be paid, ask for it to be cancelled — payment
      freezes while your approver decides.</p>
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

<div class="section-head"><h2>Attachments</h2><span class="small muted">{{len .RequestAtts}} {{plural (len .RequestAtts) "file" "files"}}</span></div>
<div class="stack-8">
  {{range .RequestAtts}}
  <div class="file-row">
    <span class="f-ico" aria-hidden="true">{{fileKind .OriginalName}}</span>
    <span><b>{{.OriginalName}}</b><small>{{fileSize .SizeBytes}} · added {{date .CreatedAt}}</small></span>
    {{if $.Perms.Can "attachment" "view"}}<span class="f-actions"><a class="btn small outline" href="/attachments/{{.ID}}">Download</a></span>{{end}}
  </div>
  {{else}}<p class="empty">No documents attached.</p>{{end}}
</div>

{{template "request_thread" .}}

{{$mine := eq .Request2.RequesterID .User.ID}}{{$mineToDecide := eq .Request2.ManagerID .User.ID}}
<div class="action-bar">
  <span class="row-end"></span>
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
</div>

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
    <input class="combo-input" id="vendor" aria-required="true" autocomplete="off" value="{{.Request2.Vendor}}"
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
     again, and restart the three-day reminder clock. Changing the approver
     moves the request to that person instead.

     The type is not editable. It is what the request *is*, and it travels as
     the same hidden input the new-request form carries (A16). */}}
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
    <b>Editing tells {{.Request2.ManagerName}} again</b>
    <p>Every change is recorded in the history, notifies your approver, and restarts the three-day
      reminder clock. Change the approver and it moves to that person instead.</p>
  </div>
</div>

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
          {{range .RequestAtts}}<div class="file-row"><span class="f-ico" aria-hidden="true">{{fileKind .OriginalName}}</span><span><b>{{.OriginalName}}</b><small>{{fileSize .SizeBytes}}</small></span></div>{{end}}
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
    <button class="btn primary" type="submit">Save and notify {{.Request2.ManagerName}}</button>
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
          {{range .RequestAtts}}<div class="file-row"><span class="f-ico" aria-hidden="true">{{fileKind .OriginalName}}</span><span><b>{{.OriginalName}}</b><small>{{fileSize .SizeBytes}}</small></span></div>{{end}}
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
    <button class="btn outline" type="submit" name="submit_action" value="save">Save corrections</button>
    <button class="btn primary" type="submit" name="submit_action" value="resubmit">Resubmit for approval</button>
  </div>
</form>

{{template "request_thread" .}}
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
    <p class="sub">{{index .Counts "to-approve"}} to approve · {{index .Counts "cancellations"}} {{plural (index .Counts "cancellations") "cancellation" "cancellations"}} to decide</p>
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
      the three-day reminder clock.</p>
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
      goes out daily if nothing happens after three calendar days.</div>
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
