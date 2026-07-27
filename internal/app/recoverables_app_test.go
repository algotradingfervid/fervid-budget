package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/auth"
)

// seedRecoverable inserts a recoverable request and, when paidOn is given, its
// settled payment. It writes both the category code and the category id — the
// shape CreateRequest produces — so the register's joins are exercised the way
// the real form drives them.
func seedRecoverable(t *testing.T, s *appTestServer, headID int64, number, code, counterparty, expectedReturn, paidOn string, amount int64) int64 {
	t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	var catID int64
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT id FROM recoverable_categories WHERE code=?`, code).Scan(&catID); err != nil {
		t.Fatal(err)
	}
	res, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO payment_requests
		(number,status,treatment,type,recoverable_category,recoverable_category_id,head_id,amount,purpose,counterparty,
		 expected_return_date,repayment_notes,requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?, 'completed','recoverable','recoverable',?,?,?,?, 'Inter-corporate deposit',?,?, 'Recover on maturity',?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		number, code, catID, headID, amount, counterparty, expectedReturn, admin.ID, admin.ID, amount, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	reqID, _ := res.LastInsertId()
	if paidOn != "" {
		if _, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,entered_by,request_id,settlement) VALUES(?,?,?,?,?,?, 'settled')`,
			headID, paidOn, amount, counterparty, admin.ID, reqID); err != nil {
			t.Fatal(err)
		}
	}
	return reqID
}

// assertTCardsLabelled fails when any <td> inside a table.t-cards lacks a
// data-label attribute. Below 860 px the CSS restacks rows into cards and reads
// that attribute for the field name, so a missing one renders an unlabelled
// value on a phone. Asserted by test, because it is invisible on a desktop.
func assertTCardsLabelled(t *testing.T, body string) {
	t.Helper()
	for _, chunk := range strings.Split(body, `class="t-cards"`)[1:] {
		table := chunk
		if end := strings.Index(table, "</table>"); end >= 0 {
			table = table[:end]
		}
		for _, cell := range strings.Split(table, "<td")[1:] {
			head := cell
			if gt := strings.Index(head, ">"); gt >= 0 {
				head = head[:gt]
			}
			if !strings.Contains(head, "data-label=") {
				t.Fatalf("t-cards <td%s> has no data-label (mobile card restack contract)", head)
			}
		}
	}
}

func TestRecoverablesDashboardRendersMetricsAndRollups(t *testing.T) { // G18
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recoverable")
	seedRecoverable(t, s, headID, "PR-2026-000101", "icd", "Beacon Infra Ltd", "2027-09-30", "2026-08-15", 900000)
	seedRecoverable(t, s, headID, "PR-2026-000102", "emd", "Ridge Metro tender authority", "2026-01-31", "2026-01-05", 200000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/recoverables", nil, ""))
	for _, want := range []string{
		"Recoverable payments",
		"Kept out of budget actuals on purpose",                 // .banner.brand explainer
		"Tracking the money coming back is not in this version", // .banner.locked
		"Outstanding", "Past expected return", "Due in 30 days", "Paid out this month",
		"By category", "By counterparty",
		"Beacon Infra Ltd", "Ridge Metro tender authority",
		"ICD", "EMD", // the category names, which only render if the id link resolves
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("recoverables dashboard missing %q", want)
		}
	}
	for _, want := range []string{`class="metric-strip"`, `class="metric warn"`, `class="banner brand"`, `class="banner locked"`, `class="t-cards"`, "<tfoot>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("recoverables dashboard missing design-system markup %q", want)
		}
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("templates must use .pill, never .badge (D5)")
	}
	assertTCardsLabelled(t, body)
}

func TestRecoverablesListScreenAndExport(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recoverable")
	seedRecoverable(t, s, headID, "PR-2026-000101", "icd", "Beacon Infra Ltd", "2027-09-30", "2026-08-15", 900000)
	seedRecoverable(t, s, headID, "PR-2026-000102", "emd", "Ridge Metro tender authority", "2026-01-31", "2026-01-05", 200000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/recoverables/list", nil, ""))
	for _, want := range []string{"All recoverables", "Beacon Infra Ltd", "PR-2026-000101", "Ridge Metro tender authority"} {
		if !strings.Contains(body, want) {
			t.Fatalf("recoverables list missing %q", want)
		}
	}
	for _, want := range []string{`class="t-cards"`, `class="m-filters"`, `class="pill recoverable"`, "<tfoot>", `name="ageing"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("recoverables list missing design-system markup %q", want)
		}
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("templates must use .pill, never .badge (D5)")
	}
	if !strings.Contains(body, `class="pill bad"`) {
		t.Fatal("an overdue row must render its ageing as a .pill.bad")
	}
	assertTCardsLabelled(t, body)

	// The ageing filter narrows the register.
	overdue := responseBody(t, s.request(http.MethodGet, "/recoverables/list?ageing=overdue", nil, ""))
	if !strings.Contains(overdue, "PR-2026-000102") || strings.Contains(overdue, "PR-2026-000101") {
		t.Fatal("ageing=overdue must keep only the row past its expected return date")
	}

	resp := s.request(http.MethodGet, "/recoverables/list.csv?from=2026-08&to=2026-08", nil, "")
	requireStatus(t, resp, http.StatusOK)
	csv := responseBody(t, resp)
	if !strings.HasPrefix(csv, "Number,Category,Counterparty,Project,Amount,Paid On,Expected Return,Ageing,Status,Requester,Repayment Notes") {
		t.Fatalf("recoverable CSV header unexpected: %s", csv)
	}
	if !strings.Contains(csv, "Beacon Infra Ltd") {
		t.Fatalf("recoverable CSV missing data row: %s", csv)
	}
}

func TestRecoverablesListViewableByAccountsRole(t *testing.T) {
	s := newAppTestServer(t)
	hash, err := auth.HashPassword("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	// data_entry maps to the seeded Accounts role, which holds recoverable_report{view,export}
	if _, err := s.st.CreateUser(s.ctx, "accounts@example.test", "Accounts User", hash, "data_entry", true); err != nil {
		t.Fatal(err)
	}
	s.login("accounts@example.test", "EntryPassword123")
	for _, path := range []string{"/recoverables", "/recoverables/list"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusOK)
		_ = responseBody(t, resp)
	}
}

func TestRecoverableDetailScreen(t *testing.T) { // G17
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recoverable")
	reqID := seedRecoverable(t, s, headID, "PR-2026-000097", "emd", "Coastal Power Utilities Ltd", "2026-06-30", "2026-02-12", 200000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/recoverables/%d", reqID), nil, ""))
	for _, want := range []string{
		"PR-2026-000097", "Coastal Power Utilities Ltd", "EMD",
		"Past its expected return date", // the overdue banner copy
		"Recoverable details", "Payment", "History and conversation",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("recoverable detail missing %q", want)
		}
	}
	for _, want := range []string{`class="req-head"`, `class="banner warn"`, `class="dl"`, `class="thread"`, `class="comment-box"`, `class="action-bar"`, `class="pill recoverable"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("recoverable detail missing design-system markup %q", want)
		}
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("templates must use .pill, never .badge (D5)")
	}
	// The comment box posts to Phase 2's existing conversation route, not a new one.
	if !strings.Contains(body, fmt.Sprintf(`action="/requests/%d/comment"`, reqID)) {
		t.Fatal("comment box must post to the Phase-2 request comment route")
	}
	// A non-recoverable request is not reachable through this screen.
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO payment_requests(number,status,treatment,type,head_id,amount,purpose,requester_id,manager_id,submitted_at)
		VALUES('PR-2026-000098','pending','budget','vendor_invoice',?,?, 'Rent',?,?,CURRENT_TIMESTAMP)`, headID, int64(100000), admin.ID, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	budgetID, _ := res.LastInsertId()
	resp := s.request(http.MethodGet, fmt.Sprintf("/recoverables/%d", budgetID), nil, "")
	requireStatus(t, resp, http.StatusNotFound)
	_ = responseBody(t, resp)
}

func TestRecoverableDetailShowsUnpaidState(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recoverable")
	reqID := seedRecoverable(t, s, headID, "PR-2026-000099", "emd", "Ridge Metro", "2027-06-30", "", 300000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/recoverables/%d", reqID), nil, ""))
	if !strings.Contains(body, "Approved but not yet paid") {
		t.Fatal("an unpaid recoverable must say so rather than render an empty payment block")
	}
	if strings.Contains(body, "Past its expected return date") {
		t.Fatal("an unpaid recoverable is not overdue: no money has left")
	}
}

func TestRecoverableListCSVRouteBeatsDetailWildcard(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	// /recoverables/list.csv is a literal pattern and must win over /recoverables/{id}.
	resp := s.request(http.MethodGet, "/recoverables/list.csv", nil, "")
	requireStatus(t, resp, http.StatusOK)
	csv := responseBody(t, resp)
	if !strings.HasPrefix(csv, "Number,Category,") {
		t.Fatalf("/recoverables/list.csv was routed to the detail handler: %s", csv)
	}
}

func TestConfigurationRecoverableCategoriesFieldset(t *testing.T) { // D6, V4
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recoverable")
	seedRecoverable(t, s, headID, "PR-2026-000701", "emd", "Ridge Metro tender authority", "2026-11-30", "", 200000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/configuration", nil, ""))
	for _, want := range []string{"Recoverable categories", "EMD", "ICD", "Related project", "Counterparty company", "Nothing extra"} {
		if !strings.Contains(body, want) {
			t.Fatalf("configuration screen missing %q", want)
		}
	}
	// One descriptive Requires column, never the raw flags.
	for _, forbidden := range []string{`name="requires_project"`, `name="requires_counterparty"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("configuration must not expose the raw flag input %s (D6)", forbidden)
		}
	}
	// The "In use" count is rendered from the store, not guessed.
	if !strings.Contains(body, `data-label="In use"`) {
		t.Fatal("configuration fieldset must render an In use column")
	}
	if !strings.Contains(body, ">1<") {
		t.Fatal("the seeded EMD recoverable should show an In use count of 1")
	}
	assertTCardsLabelled(t, body)

	form := url.Values{"name": {"Retention money"}, "requires": {"project"}, "active": {"on"}, "sort_order": {"7"}}
	resp := s.postForm("/configuration/recoverable-categories", form)
	requireStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != "/configuration" {
		t.Fatalf("save redirect = %q, want /configuration", loc)
	}
	_ = responseBody(t, resp)
	cats, err := s.st.ListRecoverableCategories(s.ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range cats {
		if c.Name == "Retention money" && c.RequiresProject && !c.RequiresCounterparty {
			found = true
		}
	}
	if !found {
		t.Fatal("new recoverable category was not persisted with the flags implied by requires=project")
	}
}

func TestStandaloneRecoverableCategoriesScreenIsGone(t *testing.T) { // D6 proof of absence
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.request(http.MethodGet, "/recoverable-categories", nil, "")
	requireStatus(t, resp, http.StatusNotFound)
	_ = responseBody(t, resp)
	// 404 or 405 — see the note in TestNoRecoverableRepaymentTracking.
	post := s.postForm("/recoverable-categories", url.Values{"name": {"Nope"}})
	if post.StatusCode != http.StatusNotFound && post.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /recoverable-categories = %d, want 404/405 (categories live on /configuration)", post.StatusCode)
	}
	_ = responseBody(t, post)
}

func TestRecoverableCategorySaveRequiresPermission(t *testing.T) {
	s := newAppTestServer(t)
	hash, err := auth.HashPassword("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.CreateUser(s.ctx, "accounts2@example.test", "Accounts User", hash, "data_entry", true); err != nil {
		t.Fatal(err)
	}
	s.login("accounts2@example.test", "EntryPassword123")
	// Accounts holds no recoverable_category permission → 403 by URL
	resp := s.postForm("/configuration/recoverable-categories", url.Values{"name": {"Nope"}})
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
}

func TestNoRecoverableRepaymentTracking(t *testing.T) { // X2
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	// No repayment surface exists: plausible repayment routes must be unrouted (404).
	for _, path := range []string{"/recoverables/1/repay", "/recoverables/list/repay", "/configuration/recoverable-categories/1/repay"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusNotFound)
		_ = responseBody(t, resp)
	}
	// 404 or 405, exactly as the sibling guard TestNoRefundRoute documents:
	// routes() registers a catch-all "GET /", so an unrouted POST matches that
	// pattern on path but not on method and ServeMux answers 405. Both mean
	// "no such endpoint". Do not narrow this to 404 — the router cannot
	// produce it here.
	resp := s.postForm("/recoverables/1/repay", url.Values{"amount": {"100000"}})
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		body := responseBody(t, resp)
		t.Fatalf("repayment POST status = %d, want 404/405 (no repayment path exists); body: %s", resp.StatusCode, body)
	}
	_ = responseBody(t, resp)
}

func TestNoForfeitureRoute(t *testing.T) { // X4
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	// Forfeiture / write-off is out of scope: no route or store method converts a
	// recoverable into a budget expense, so plausible forfeiture paths are unrouted.
	for _, path := range []string{"/recoverables/1/forfeit", "/recoverables/1/write-off", "/recoverables/list/forfeit"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusNotFound)
		_ = responseBody(t, resp)
	}
	// 404 or 405 — see the note in TestNoRecoverableRepaymentTracking.
	resp := s.postForm("/recoverables/1/forfeit", url.Values{"reason": {"defaulted"}})
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		body := responseBody(t, resp)
		t.Fatalf("forfeiture POST status = %d, want 404/405 (no forfeiture/write-off path exists); body: %s", resp.StatusCode, body)
	}
	_ = responseBody(t, resp)
}

// Phase 5, Task 6: the reminder thresholds are admin-set data on the
// Configuration screen, not constants in the scheduler.
func TestConfigurationRemindersFieldsetPersistsThresholds(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/configuration", nil, ""))
	for _, want := range []string{"Reminders and ageing", `name="reminder_pending_days"`, `name="reminder_repeat_days"`, `name="reminder_stale_days"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("configuration screen missing %q", want)
		}
	}

	form := url.Values{
		"reminder_pending_days": {"5"}, "reminder_repeat_days": {"2"}, "reminder_stale_days": {"4"},
	}
	resp := s.postForm("/configuration", form)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	th, err := s.st.ReminderThresholds(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if th.PendingAfterDays != 5 || th.RepeatEveryDays != 2 || th.StaleAfterDays != 4 {
		t.Fatalf("thresholds after save = %+v, want 5/2/4", th)
	}
}
