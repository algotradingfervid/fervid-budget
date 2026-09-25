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

// slice returns the text from the first `from` to the next `to` after it.
func cut(body, from, to string) string {
	i := strings.Index(body, from)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	if j := strings.Index(rest[len(from):], to); j >= 0 {
		return rest[:len(from)+j+len(to)]
	}
	return rest
}

// seedHeadlessRecoverable is an approved recoverable shaped the way the request
// form leaves one since v8: linked to a project, with no head.
func (s *appTestServer) seedHeadlessRecoverable(seq int, requesterID, managerID, projectID, amount int64) int64 {
	s.t.Helper()
	res, err := s.st.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,recoverable_category,recoverable_category_id,project_id,amount,purpose,short_title,counterparty,expected_return_date,repayment_notes,requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?,'approved','recoverable','employee_advance','emd',(SELECT id FROM recoverable_categories WHERE code='emd'),?,?,'Tender deposit','EMD','Ridge Metro','2027-03-31','Refundable on award',?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), projectID, amount, requesterID, managerID, amount, managerID)
	if err != nil {
		s.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// recoverables-2 / grid-2. A recoverable is a deposit, not budget spend: the
// grid's totals already left it out, but its Recent Payments panel listed it,
// and every payment screen printed its missing project and head as a bare "/".
func TestRecoverablePaymentStaysOffTheGridAndIsNamedNotSlashed(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("RecGrid")
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	month := currentMonthForTest()
	_, budgetPay := s.settleOneRequest(1, headID, 400000, "4000.00", "settled", month+"-02")

	reqID := s.seedHeadlessRecoverable(2, admin.ID, admin.ID, projectID, 25000000)
	picker := responseBody(t, s.request(http.MethodGet, "/payments/new", nil, ""))
	row := cut(cut(picker, "PR-2026-000002", "</small>"), "<small>", "</small>")
	if row == "" || danglingSlash.MatchString(row) || !strings.Contains(row, "Recoverable") {
		t.Fatalf("the payment picker row for a recoverable reads %q", row)
	}
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	// The reservation screen, before the payment exists: a recoverable is not
	// charged to any budget, so "Charge to" must not read "Operations /".
	entry := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/payments/new?request=%d", reqID), nil, ""))
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
	requireStatus(t, s.postForm("/payments", form), http.StatusSeeOther)
	var recPay int64
	if err := s.st.DB().QueryRow(`SELECT id FROM payments WHERE request_id=?`, reqID).Scan(&recPay); err != nil {
		t.Fatal(err)
	}

	grid := responseBody(t, s.request(http.MethodGet, "/grid?month="+month, nil, ""))
	panel := cut(grid, "Recent Payments for", "</table>")
	if strings.Contains(panel, fmt.Sprintf(`href="/payments/%d"`, recPay)) || strings.Contains(panel, "2,50,000.00") {
		t.Fatalf("the grid's Recent Payments lists the recoverable payment: %s", panel)
	}
	if !strings.Contains(panel, fmt.Sprintf(`href="/payments/%d"`, budgetPay)) {
		t.Fatalf("the budget payment fell out of Recent Payments too: %s", panel)
	}

	ledger := responseBody(t, s.request(http.MethodGet, "/payments?month="+month, nil, ""))
	cell := cut(ledger, fmt.Sprintf(`<a href="/payments/%d">`, recPay), "</a>")
	if cell == "" || danglingSlash.MatchString(cell) || !strings.Contains(cell, "Recoverable") {
		t.Fatalf("the ledger's Project / Head cell for a recoverable = %q, want it named Recoverable", cell)
	}

	detail := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/payments/%d", recPay), nil, ""))
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
	s.login(s.cfg.AdminEmail, testAdminPassword)
	month := currentMonthForTest()
	s.settleOneRequest(1, headID, 400000, "4000.00", "settled", month+"-02")

	s.seedProbeUser("gridonly@example.test", "Grid Only", "GridOnlyPass1", "grid-only",
		[]store.Grant{{Resource: "grid", Action: "view"}}, nil)
	s.login("gridonly@example.test", "GridOnlyPass1")
	body := responseBody(t, s.request(http.MethodGet, "/grid?month="+month, nil, ""))
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
	s.seedHead("Swap")
	s.login(s.cfg.AdminEmail, testAdminPassword)

	page := responseBody(t, s.request(http.MethodGet, "/grid", nil, ""))
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
	s.seedHead("Solo")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	month := currentMonthForTest()
	grid := responseBody(t, s.request(http.MethodGet, "/grid?month="+month, nil, ""))
	if !strings.Contains(grid, `<span class="project-count">1 head</span>`) {
		t.Fatalf("a one-head project is not counted as \"1 head\": %s", cut(grid, `class="project-count"`, "</span>"))
	}
	if !strings.Contains(grid, `<span class="countpill">1 project</span>`) || !strings.Contains(grid, " · 1 head · remaining") {
		t.Fatalf("the grid header does not pluralise its counts: %s", cut(grid, `<div class="between"`, "</div></div>"))
	}
	if !strings.Contains(grid, "1 head is unpaid, 0 have unbudgeted spend, and 0 are over budget.") {
		t.Fatalf("the month close sentence is not pluralised: %s", cut(grid, "<h2>Month Close</h2>", "</p>"))
	}
	requireStatus(t, s.postForm("/months", url.Values{"target_month": {month}}), http.StatusSeeOther)
	months := responseBody(t, s.request(http.MethodGet, "/months", nil, ""))
	if !strings.Contains(months, "<small>1 head</small>") {
		t.Fatalf("/months does not say \"1 head\": %s", cut(months, `data-label="Month"`, "</td>"))
	}
}

// ui-1 (c). Adding a user with no role used to answer with the full-page error
// screen; Back closed the drawer and lost what was typed. The users page comes
// back with the drawer open, an inline error, and the typed name, email and
// roles still in place. The password is deliberately never echoed into HTML.
func TestAddUserValidationKeepsTheDrawerAndTheInput(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.postForm("/users", url.Values{"email": {"nora@example.test"}, "name": {"Nora Test"}, "password": {"NoraPassword123"}, "active": {"on"}})
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	drawer := cut(body, `id="user-new"`, "</form>")
	if drawer == "" || strings.HasPrefix(strings.TrimSpace(strings.SplitN(drawer, ">", 2)[0]), `id="user-new" hidden`) || strings.Contains(strings.SplitN(drawer, ">", 2)[0], "hidden") {
		t.Fatalf("the Add user drawer is not open on the validation answer: %s", strings.SplitN(drawer, ">", 2)[0])
	}
	for _, want := range []string{"Choose at least one role for the new user.", `value="nora@example.test"`, `value="Nora Test"`} {
		if !strings.Contains(drawer, want) {
			t.Fatalf("the reopened drawer lacks %q", want)
		}
	}
	if strings.Contains(body, "NoraPassword123") {
		t.Fatal("the password was echoed back into the page")
	}
	if strings.Contains(body, "error-code") {
		t.Fatal("the full-page error screen answered a form validation error")
	}

	// A role that was ticked stays ticked when the password is what is wrong.
	roleID := s.roleIDByName(t, "Requester")
	resp = s.postForm("/users", url.Values{"email": {"nora@example.test"}, "name": {"Nora Test"}, "password": {"short"}, "role_ids": {roleID}})
	requireStatus(t, resp, http.StatusBadRequest)
	drawer = cut(responseBody(t, resp), `id="user-new"`, "</form>")
	if !strings.Contains(drawer, fmt.Sprintf(`value="%s" checked`, roleID)) {
		t.Fatal("the ticked role was lost on a password error")
	}
}

// recoverables-6. At 390px every empty spacer cell in a t-cards tfoot became a
// blank stripe in the total card. The spacers are desktop-only.
func TestTCardsTotalRowsCarryNoEmptyMobileCells(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	for _, path := range []string{"/recoverables/list", "/vendors"} {
		body := responseBody(t, s.request(http.MethodGet, path, nil, ""))
		tfoot := cut(body, "<tfoot>", "</tfoot>")
		if tfoot == "" {
			t.Fatalf("%s has no total row", path)
		}
		if strings.Contains(tfoot, `<td data-label=""></td>`) {
			t.Fatalf("%s's total row still has empty cells that restack into blank stripes on a phone: %s", path, tfoot)
		}
	}
}
