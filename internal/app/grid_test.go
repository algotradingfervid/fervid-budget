package app

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// danglingSlash matches a "Project / Head" rendering with one side missing:
// "> / <", "/ · paid", "Operations /</dd>". A real pair always has text on both
// sides of the separator.
var danglingSlash = regexp.MustCompile(`(>\s*/\s*[^\s<])|(>\s*/\s*<)|([^\s>]\s/\s*(<|·))`)

// recoverables-2 / grid-2. A recoverable is a deposit, not budget spend: the
// grid's totals already left it out, but its Recent Payments panel listed it,
// and every payment screen printed its missing project and head as a bare "/".
func TestRecoverablePaymentStaysOffTheGridAndIsNamedNotSlashed(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("RecGrid")
	projectID := projectOf(t, s, headID)
	s.loginAdmin()
	month := currentMonthForTest()
	_, budgetPay := s.settleOneRequest(1, headID, 400000, "4000.00", "settled", month+"-02")

	reqID := s.seedHeadlessRecoverable(2, admin.ID, admin.ID, projectID, 25000000)
	picker := s.get("/payments/new")
	row := cut(cut(picker, "PR-2026-000002", "</small>"), "<small>", "</small>")
	if row == "" || danglingSlash.MatchString(row) || !strings.Contains(row, "Recoverable") {
		t.Fatalf("the payment picker row for a recoverable reads %q", row)
	}
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	// The reservation screen, before the payment exists: a recoverable is not
	// charged to any budget, so "Charge to" must not read "Operations /".
	entry := s.get(fmt.Sprintf("/payments/new?request=%d", reqID))
	if cut(entry, "<dt>Charge to</dt>", "</dd>") == "" {
		t.Fatal("the payment entry screen has no Charge to line")
	}
	for _, frag := range []string{cut(entry, "<dt>Charge to</dt>", "</dd>"), cut(entry, `<p class="rh-meta">`, "</p>")} {
		if danglingSlash.MatchString(frag) {
			t.Fatalf("the payment entry screen renders a dangling project/head: %q", frag)
		}
	}
	if !strings.Contains(cut(entry, "<dt>Charge to</dt>", "</dd>"), "Recoverable") {
		t.Fatalf("the entry screen does not say the payment is recoverable: %q", cut(entry, "<dt>Charge to</dt>", "</dd>"))
	}

	form := url.Values{
		"request_id": {strconvFormat(reqID)}, "paid_on": {month + "-03"}, "amount": {"250000.00"},
		"vendor_payee": {"Ridge Metro"}, "settlement": {"settled"}, "reference_no": {"UTR123"},
	}
	requireStatus(t, s.postPayment(form), http.StatusSeeOther)
	var recPay int64
	if err := s.st.DB().QueryRow(`SELECT id FROM payments WHERE request_id=?`, reqID).Scan(&recPay); err != nil {
		t.Fatal(err)
	}

	grid := s.get("/grid?month=" + month)
	panel := cut(grid, "Recent Payments for", "</table>")
	if strings.Contains(panel, fmt.Sprintf(`href="/payments/%d"`, recPay)) || strings.Contains(panel, "2,50,000.00") {
		t.Fatalf("the grid's Recent Payments lists the recoverable payment: %s", panel)
	}
	if !strings.Contains(panel, fmt.Sprintf(`href="/payments/%d"`, budgetPay)) {
		t.Fatalf("the budget payment fell out of Recent Payments too: %s", panel)
	}

	ledger := s.get("/payments?month=" + month)
	cell := cut(ledger, fmt.Sprintf(`<a href="/payments/%d">`, recPay), "</a>")
	if cell == "" || danglingSlash.MatchString(cell) || !strings.Contains(cell, "Recoverable") {
		t.Fatalf("the ledger's Project / Head cell for a recoverable = %q, want it named Recoverable", cell)
	}

	detail := s.get(fmt.Sprintf("/payments/%d", recPay))
	meta := cut(detail, `<p class="rh-meta">`, "</p>")
	if meta == "" || danglingSlash.MatchString(meta) || !strings.Contains(meta, "Recoverable") {
		t.Fatalf("the payment detail head reads %q, want it named Recoverable", meta)
	}
}

// grid-1 (F-G-032 wording). A grid:view caller without payment:view was told
// "No payments in this month." beside a head the same page showed as paid. The
// panel is not theirs, so it is not rendered at all (PE-03: "expect 200 without
// `Recent Payments for`").
func TestGridHidesThePaymentsPanelRatherThanClaimingItIsEmpty(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("GridPanel")
	s.loginAdmin()
	month := currentMonthForTest()
	s.settleOneRequest(1, headID, 400000, "4000.00", "settled", month+"-02")

	s.seedProbeUser("gridonly@example.test", "Grid Only", "GridOnlyPass1", "grid-only",
		[]store.Grant{{Resource: "grid", Action: "view"}}, nil)
	s.login("gridonly@example.test", "GridOnlyPass1")
	body := s.get("/grid?month=" + month)
	if !strings.Contains(body, "4,000.00") {
		t.Fatal("the grid lost the head's actual")
	}
	if strings.Contains(body, "Recent Payments for") || strings.Contains(body, "No payments in this month.") {
		t.Fatal("a caller without payment:view is shown a Recent Payments panel that claims the month has no payments")
	}
}

// grid-htmx-1 (BV20, §6). The grid's filters swap the grid in over htmx and
// push the filtered URL; the same form still submits as a plain GET without
// JavaScript. An htmx request is answered without the app shell, so the swap
// carries the grid and not a second sidebar.
func TestGridFiltersSwapOverHTMXAndStillWorkWithoutIt(t *testing.T) {
	s := newAppTestServer(t)
	actor, swapHead := s.seedHead("Swap")
	if err := s.st.SetBudget(s.ctx, actor, swapHead, currentMonthForTest(), 10000); err != nil {
		t.Fatal(err)
	}
	s.loginAdmin()

	page := s.get("/grid")
	form := cut(page, `<form class="toolbar"`, ">")
	for _, want := range []string{`method="get"`, `action="/grid"`, `hx-get="/grid"`, `hx-push-url="true"`, `hx-target="#grid-body"`, `hx-select="#grid-body"`} {
		if !strings.Contains(form, want) {
			t.Fatalf("the grid filter form lacks %s: %s", want, form)
		}
	}
	for _, id := range []string{`id="grid-body"`, `id="grid-summary"`, `id="grid-legend"`, `id="grid-export"`, `id="grid-add"`, `id="grid-metrics"`} {
		if !strings.Contains(page, id) {
			t.Fatalf("the full grid page has no %s to swap", id)
		}
	}

	resp := s.htmxGet("/grid?status=not-paid&q=Swap")
	requireStatus(t, resp, http.StatusOK)
	if vary := resp.Header.Get("Vary"); !strings.Contains(vary, "HX-Request") {
		t.Fatalf("Vary = %q; a cache must not hand the fragment to a full-page load", vary)
	}
	frag := responseBody(t, resp)
	if strings.Contains(frag, `class="sidebar`) {
		t.Fatal("the htmx answer carries the app shell")
	}
	if !strings.Contains(frag, `id="grid-body"`) || !strings.Contains(frag, "Rent Swap") {
		t.Fatalf("the htmx answer lacks the filtered grid: %s", firstLines(frag))
	}

	// A history-restore request (htmx 2 sends HX-Request with it) is a full
	// page load and must get the whole page back.
	req, err := http.NewRequest(http.MethodGet, s.server.URL+"/grid?status=not-paid", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-History-Restore-Request", "true")
	restore, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if body := responseBody(t, restore); !strings.Contains(body, `class="sidebar`) {
		t.Fatal("a history restore was answered with a fragment, so Back would render a page with no shell")
	}
}

// ui-1 (a). Counts next to nouns are pluralised, so a one-head project reads
// "1 head" and a one-head month close reads "1 head is unpaid".
func TestGridAndMonthsPluraliseHeadCounts(t *testing.T) {
	s := newAppTestServer(t)
	actor, soloHead := s.seedHead("Solo")
	if err := s.st.SetBudget(s.ctx, actor, soloHead, currentMonthForTest(), 10000); err != nil {
		t.Fatal(err)
	}
	s.loginAdmin()
	month := currentMonthForTest()
	grid := s.get("/grid?month=" + month)
	if !strings.Contains(grid, `<span class="project-count">1 head</span>`) {
		t.Fatalf("a one-head project is not counted as \"1 head\": %s", cut(grid, `class="project-count"`, "</span>"))
	}
	if !strings.Contains(grid, `<span class="countpill">1 project</span>`) || !strings.Contains(grid, " · 1 head · remaining") {
		t.Fatalf("the grid header does not pluralise its counts: %s", cut(grid, `<div class="between"`, "</div></div>"))
	}
	if !strings.Contains(grid, "1 budgeted head is unpaid, 0 have unbudgeted spend, and 0 are over budget.") {
		t.Fatalf("the month close sentence is not pluralised: %s", cut(grid, "<h2>Month Close</h2>", "</p>"))
	}
	requireStatus(t, s.postForm("/months", url.Values{"target_month": {month}}), http.StatusSeeOther)
	months := s.get("/months")
	if !strings.Contains(months, "<small>1 head</small>") {
		t.Fatalf("/months does not say \"1 head\": %s", cut(months, `data-label="Month"`, "</td>"))
	}
}

func TestGridPaymentActivityDoesNotDeclareOpenBalancesSettled(t *testing.T) {
	s := newAppTestServer(t)
	actor, head := s.seedHead("OutstandingBalances")
	month := currentMonthForTest()
	if err := s.st.SetBudget(s.ctx, actor, head, month, 1000000); err != nil {
		t.Fatal(err)
	}
	s.loginAdmin()
	installment, _ := s.settleOneRequest(501, head, 1200000, "7000.00", "installment", month+"-01")
	kept, _ := s.settleOneRequest(502, head, 60000, "100.00", "partial", month+"-01")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/accept-partial", kept), url.Values{"decision": {"continue"}, "note": {"Pay the remaining 500 on next transfer"}}), http.StatusSeeOther)
	s.settleOneRequest(503, head, 200000, "2000.00", "settled", month+"-01")
	for id, wantRemaining := range map[int64]int64{installment: 500000, kept: 50000} {
		req, err := s.st.Request(s.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if req.Status != "approved" || approvedOf(req)-req.PaidAmount != wantRemaining {
			t.Fatalf("request %d status=%s remaining=%d", id, req.Status, approvedOf(req)-req.PaidAmount)
		}
	}
	body := s.get("/grid?month=" + month)
	row := cut(body, `<tr class="head`, `</tr>`)
	t.Logf("Grid head row with ₹5,000 + ₹500 still payable: %s", row)
	for _, want := range []string{"₹10,000.00", "₹9,100.00", "₹900.00", "Under budget", "Payment recorded"} {
		if !strings.Contains(row, want) {
			t.Errorf("grid row missing %q", want)
		}
	}
	if strings.Contains(row, ">Settled<") || strings.Contains(row, `duepill settled`) {
		t.Fatal("payment activity falsely declares open request balances settled")
	}
	for _, status := range []string{"under", "on-track", "over", "unbudgeted"} {
		if dueText("5", month, status) != "Payment recorded" {
			t.Errorf("%s claims settlement from budget utilization", status)
		}
	}
}
