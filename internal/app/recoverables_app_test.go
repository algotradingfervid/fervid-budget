package app

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// assertTCardsLabelled fails when any <td> inside a table.t-cards lacks a
// data-label attribute. Below 860 px the CSS restacks rows into cards and reads
// that attribute for the field name, so a missing one renders an unlabelled
// value on a phone. Asserted by test, because it is invisible on a desktop.
func assertTCardsLabelled(t *testing.T, body string) {
	t.Helper()
	for _, chunk := range strings.Split(body, `class="t-cards`)[1:] {
		if !strings.HasPrefix(chunk, `"`) && !strings.HasPrefix(chunk, " ") {
			continue
		}
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

func TestRecoverablesDashboardRendersMetricsAndRollups(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recoverable")
	seedRecoverable(t, s, headID, "PR-2026-000101", "icd", "Beacon Infra Ltd", "2027-09-30", "2026-08-15", 900000)
	seedRecoverable(t, s, headID, "PR-2026-000102", "emd", "Ridge Metro tender authority", "2026-01-31", "2026-01-05", 200000)
	s.loginAdmin()

	body := s.get("/recoverables")
	mustContain(t, "recoverables dashboard", body,
		"Recoverable payments",
		"Kept out of budget actuals on purpose", // .banner.brand explainer
		"Track each return and reconciliation",  // .banner.locked
		"Outstanding", "Past expected return", "Due in 30 days", "Paid out this month",
		"By category", "By counterparty",
		"Beacon Infra Ltd", "Ridge Metro tender authority",
		"ICD", "EMD", // the category names, which only render if the id link resolves
	)
	mustContain(t, "recoverables dashboard design-system markup", body, `class="metric-strip"`, `class="metric warn"`, `class="banner brand"`, `class="banner locked"`, `class="t-cards"`, "<tfoot>")
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
	s.loginAdmin()

	body := s.get("/recoverables/list")
	mustContain(t, "recoverables list", body, "All recoverables", "Beacon Infra Ltd", "PR-2026-000101", "Ridge Metro tender authority")
	mustContain(t, "recoverables list design-system markup", body, `class="t-cards"`, `class="m-only recovery-filters"`, `class="pill recoverable"`, "<tfoot>", `name="ageing"`)
	if strings.Contains(body, `class="badge`) {
		t.Fatal("templates must use .pill, never .badge (D5)")
	}
	if !strings.Contains(body, `class="pill bad"`) {
		t.Fatal("an overdue row must render its ageing as a .pill.bad")
	}
	assertTCardsLabelled(t, body)

	// The ageing filter narrows the register.
	overdue := s.get("/recoverables/list?ageing=overdue")
	if !strings.Contains(overdue, "PR-2026-000102") || strings.Contains(overdue, "PR-2026-000101") {
		t.Fatal("ageing=overdue must keep only the row past its expected return date")
	}

	resp := s.request(http.MethodGet, "/recoverables/list.csv?from=2026-08&to=2026-08", nil, "")
	requireStatus(t, resp, http.StatusOK)
	csv := responseBody(t, resp)
	if !strings.HasPrefix(csv, "Number,Category,Counterparty,Project,Outstanding,Paid out,Recovered or reconciled,Paid On,Expected Return,Ageing,Status,Requester,Repayment Notes") {
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

func TestRecoverableDetailScreen(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recoverable")
	reqID := seedRecoverable(t, s, headID, "PR-2026-000097", "emd", "Coastal Power Utilities Ltd", "2026-06-30", "2026-02-12", 200000)
	s.loginAdmin()

	body := s.get(fmt.Sprintf("/recoverables/%d", reqID))
	mustContain(t, "recoverable detail", body,
		"PR-2026-000097", "Coastal Power Utilities Ltd", "EMD",
		"Past its expected return date", // the overdue banner copy
		"Recoverable details", "Latest payment", "History and conversation",
	)
	mustContain(t, "recoverable detail design-system markup", body, `class="req-head"`, `class="banner warn"`, `class="dl"`, `class="thread"`, `class="comment-box"`, `class="action-bar"`, `class="pill recoverable"`)
	if strings.Contains(body, `class="badge`) {
		t.Fatal("templates must use .pill, never .badge (D5)")
	}
	// The comment box posts to the request's existing conversation route, not a new one.
	if !strings.Contains(body, fmt.Sprintf(`action="/requests/%d/comment"`, reqID)) {
		t.Fatal("comment box must post to the Phase-2 request comment route")
	}
	// A non-recoverable request is not reachable through this screen.
	admin := s.admin()
	budgetID := s.insertRequest(t, map[string]any{
		"number": requestNumber(98), "status": "pending", "treatment": "budget", "type": "vendor_invoice",
		"head_id": headID, "amount": int64(100000), "purpose": "Rent",
		"requester_id": admin.ID, "manager_id": admin.ID, "submitted_at": sqlNow,
	})
	resp := s.request(http.MethodGet, fmt.Sprintf("/recoverables/%d", budgetID), nil, "")
	requireStatus(t, resp, http.StatusNotFound)
	_ = responseBody(t, resp)
}

func TestRecoverableDetailShowsUnpaidState(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recoverable")
	reqID := seedRecoverable(t, s, headID, "PR-2026-000099", "emd", "Ridge Metro", "2027-06-30", "", 300000)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET status='approved' WHERE id=?`, reqID); err != nil {
		t.Fatal(err)
	}
	s.loginAdmin()

	body := s.get(fmt.Sprintf("/recoverables/%d", reqID))
	if !strings.Contains(body, "Approved but not yet paid") {
		t.Fatal("an unpaid recoverable must say so rather than render an empty payment block")
	}
	if strings.Contains(body, "Past its expected return date") {
		t.Fatal("an unpaid recoverable is not overdue: no money has left")
	}
}

func TestRecoverableListCSVRouteBeatsDetailWildcard(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
	// /recoverables/list.csv is a literal pattern and must win over /recoverables/{id}.
	resp := s.request(http.MethodGet, "/recoverables/list.csv", nil, "")
	requireStatus(t, resp, http.StatusOK)
	csv := responseBody(t, resp)
	if !strings.HasPrefix(csv, "Number,Category,") {
		t.Fatalf("/recoverables/list.csv was routed to the detail handler: %s", csv)
	}
}

// Recoverable categories are managed on the Configuration screen.
func TestConfigurationRecoverableCategoriesFieldset(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recoverable")
	seedRecoverable(t, s, headID, "PR-2026-000701", "emd", "Ridge Metro tender authority", "2026-11-30", "", 200000)
	s.loginAdmin()

	body := s.get("/configuration")
	mustContain(t, "configuration screen", body, "Recoverable categories", "EMD", "ICD", "Related project", "Counterparty company", "Nothing extra")
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

// There is no standalone recoverable-categories screen; Configuration is the
// only place they are managed.
func TestStandaloneRecoverableCategoriesScreenIsGone(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
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

func TestNoRecoverableRepaymentTracking(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
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

func TestNoForfeitureRoute(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
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
	// And no store method either. Same shape as TestNoRefundRoute: s.st is
	// *store.Store, so reflect enumerates every pointer-receiver method.
	st := reflect.TypeOf(s.st)
	for i := 0; i < st.NumMethod(); i++ {
		name := strings.ToLower(st.Method(i).Name)
		for _, banned := range []string{"forfeit", "writeoff", "write_off"} {
			if strings.Contains(name, banned) {
				t.Fatalf("unexpected store method %q (X4): forfeiture and write-off are out of scope", st.Method(i).Name)
			}
		}
	}
}

// The reminder thresholds are admin-set data on the
// Configuration screen, not constants in the scheduler.
func TestConfigurationRemindersFieldsetPersistsThresholds(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()

	body := s.get("/configuration")
	mustContain(t, "configuration screen", body, "Reminders and ageing", `name="reminder_pending_days"`, `name="reminder_repeat_days"`, `name="reminder_stale_days"`)

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

// The recoverables register is a second view over payment_requests, so it
// applies the caller's request data scope; otherwise recoverable_report:view
// would hand over every counterparty, amount, requester and repayment note in
// the company.
func TestRecoverablesRegisterAppliesTheRequestDataScope(t *testing.T) {
	s := newAppTestServer(t)
	admin, _ := s.seedHead("Recoverable scope")
	other := s.seedRequester("other-rec@example.test", "Other Requester", "OtherRecPass123")
	id := s.seedRecoverableRequest(951, other.ID, admin.ID, "icd", "Kalyani Steels", 500000)

	// A role that pairs the register with a narrower request scope, as an
	// administrator may build in the role editor.
	s.seedProbeUser("narrow@example.test", "Narrow Reader", "NarrowPass1234", "Register reader",
		[]store.Grant{
			{Resource: "recoverable_report", Action: "view"},
			{Resource: "recoverable_report", Action: "export"},
			{Resource: "request", Action: "view"},
		},
		[]store.ScopeGrant{{Resource: "request", Scope: "own"}})
	s.login("narrow@example.test", "NarrowPass1234")

	// The control: the same caller is refused the request itself.
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", id), nil, ""), http.StatusNotFound)

	list := s.get("/recoverables/list")
	if strings.Contains(list, "Kalyani Steels") {
		t.Fatal("the register lists a recoverable this caller is refused on /requests/{id}")
	}
	csv := s.get("/recoverables/list.csv")
	for _, leak := range []string{"Kalyani Steels", "Other Requester"} {
		if strings.Contains(csv, leak) {
			t.Fatalf("the CSV discloses %q to a caller outside its data scope", leak)
		}
	}
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/recoverables/%d", id), nil, ""), http.StatusNotFound)

	// And an administrator, who holds request=all, still sees everything.
	s.loginAdmin()
	if body := s.get("/recoverables/list"); !strings.Contains(body, "Kalyani Steels") {
		t.Fatal("the scope filter hides a row from a caller who holds request=all")
	}
}

// The register shows every column its CSV exports, including Status, Requester
// and Repayment Notes, so the download never says more than the screen.
func TestRecoverablesRegisterShowsEveryColumnItsCSVExports(t *testing.T) {
	s := newAppTestServer(t)
	admin, _ := s.seedHead("Column parity")
	requester := s.seedRequester("columns@example.test", "Column Requester", "ColumnPass1234")
	s.seedRecoverableRequest(961, requester.ID, admin.ID, "icd", "Parity Counterparty", 400000)

	s.loginAdmin()
	body := s.get("/recoverables/list")
	for _, want := range []string{">Status<", ">Requester<", ">Repayment notes<", "Column Requester"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the register does not carry %q, which its own CSV exports", want)
		}
	}
}
