package app

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

// projectOf is the project a head belongs to, for the forms that must re-post it.
func projectOf(t *testing.T, s *appTestServer, headID int64) int64 {
	t.Helper()
	var projectID int64
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	return projectID
}

// Wave 4 of the 2026-07-27 QA repair: the app layer.
//
// One test per finding, or per family where the findings share a mechanism. Each
// one names the id it pins so a future reader can go back to
// docs/qa/results/findings-*.md and see what was reproduced.

// notificationTitles is every in-app row addressed to one user, newest first.
// The events this wave wired are checked through the store rather than through
// the centre's markup: what matters is that the row exists for the right person,
// not how it renders.
func notificationTitles(t *testing.T, s *appTestServer, userID int64) []string {
	t.Helper()
	rows, err := s.st.ListNotifications(s.ctx, store.NotificationFilter{UserID: userID, Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Event+"|"+row.Title)
	}
	return out
}

func hasEvent(titles []string, event string) bool {
	for _, entry := range titles {
		if strings.HasPrefix(entry, event+"|") {
			return true
		}
	}
	return false
}

// F-F-06 / F-D-12 — six real workflow actions notified nobody, and the release
// screen promised in as many words that they did. Every one is driven through its
// own route here, and the row is read back for the person the seeded rule
// addresses.
func TestWorkflowActionsThatUsedToNotifyNobodyNowFire(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Notify")
	requester := s.seedRequester("firer@example.test", "Fire Requester", "FirePass12345")

	// Withdraw: the approver's queue item disappears without a decision.
	pending := s.seedPendingRequest(901, requester.ID, admin.ID, headID, 120000)
	s.login("firer@example.test", "FirePass12345")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/withdraw", pending), url.Values{}), http.StatusSeeOther)
	if !hasEvent(notificationTitles(t, s, admin.ID), notify.EventRequestWithdrawn) {
		t.Error("withdrawing a pending request tells the approver nothing")
	}

	// Re-raise: fired for the NEW request, which is the one that needs deciding.
	rejected := s.seedPendingRequest(902, requester.ID, admin.ID, headID, 130000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/reject", rejected),
		url.Values{"reason": {"Wrong head"}}), http.StatusSeeOther)
	s.login("firer@example.test", "FirePass12345")
	resp := s.postForm(fmt.Sprintf("/requests/%d/reraise", rejected), url.Values{})
	requireStatus(t, resp, http.StatusSeeOther)
	if !hasEvent(notificationTitles(t, s, admin.ID), notify.EventRequestReraised) {
		t.Error("re-raising a rejected request tells the approver nothing")
	}

	// Hold then unhold: request_on_hold already told the requester the pause
	// began; request_unheld is the half that says it is over.
	held := s.seedApprovedRequest(903, requester.ID, admin.ID, headID, 140000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", held),
		url.Values{"reason": {"Which cost centre?"}}), http.StatusSeeOther)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/unhold", held), url.Values{}), http.StatusSeeOther)
	if !hasEvent(notificationTitles(t, s, requester.ID), notify.EventRequestUnheld) {
		t.Error("lifting a hold tells the requester nothing")
	}

	// Release: the screen states "The requester and the approver are both
	// notified", which is now true.
	released := s.seedApprovedRequest(904, requester.ID, admin.ID, headID, 150000)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", released), url.Values{}), http.StatusSeeOther)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/release", released),
		url.Values{"reason": {"Waiting on a corrected invoice"}, "confirm": {"on"}}), http.StatusSeeOther)
	if !hasEvent(notificationTitles(t, s, requester.ID), notify.EventReservationReleased) {
		t.Error("releasing a reservation tells the requester nothing")
	}

	// Reassign the reservation to a colleague who can work the queue.
	colleague := s.seedColleague("taker@example.test", "Taker Rao", "TakerPass12345")
	reassigned := s.seedApprovedRequest(905, requester.ID, admin.ID, headID, 160000)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reassigned), url.Values{}), http.StatusSeeOther)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/reassign", reassigned),
		url.Values{"to_user_id": {itoa64(colleague.ID)}, "reason": {"Off sick"}, "confirm": {"on"}}), http.StatusSeeOther)
	if !hasEvent(notificationTitles(t, s, requester.ID), notify.EventReservationReassigned) {
		t.Error("reassigning a reservation tells the requester nothing")
	}
}

// F-F-06 — the partial-review decisions and the two halves of a cancellation.
// They are separated from the test above because both need a request in a state
// only the manager can decide, and the manager here is the admin.
func TestPartialAndCancellationDecisionsFire(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Decide notify")
	requester := s.seedRequester("decided@example.test", "Decided Requester", "DecidePass1234")

	// Accept a partial: the requester learns the balance is never coming.
	s.login(s.cfg.AdminEmail, testAdminPassword)
	accepted := s.seedApprovedRequest(911, requester.ID, admin.ID, headID, 10000000)
	s.seedPartialReview(accepted, headID, "Bank deducted charges")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/accept-partial", accepted),
		url.Values{"note": {"Not worth chasing"}}), http.StatusSeeOther)
	if !hasEvent(notificationTitles(t, s, requester.ID), notify.EventPaymentPartialAccepted) {
		t.Error("accepting a partial payment tells the requester nothing")
	}

	// Raise a concern: it waits on Accounts, so Accounts is told.
	concerned := s.seedApprovedRequest(912, requester.ID, admin.ID, headID, 10000000)
	s.seedPartialReview(concerned, headID, "Short by a lakh")
	before := len(notificationTitles(t, s, admin.ID))
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/raise-concern", concerned),
		url.Values{"comment": {"Why is this short?"}}), http.StatusSeeOther)
	if after := len(notificationTitles(t, s, admin.ID)); after == before {
		t.Error("raising a concern about a shortfall tells nobody")
	}

	// A cancellation accepted and a cancellation declined are two events,
	// because the sentences are opposites.
	for _, tc := range []struct {
		seq      int
		decision string
		event    string
	}{
		{913, "accept", notify.EventCancellationAccepted},
		{914, "decline", notify.EventCancellationDeclined},
	} {
		id := s.seedApprovedRequest(tc.seq, requester.ID, admin.ID, headID, 220000)
		s.login("decided@example.test", "DecidePass1234")
		requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/cancel-request", id),
			url.Values{"reason": {"Order withdrawn"}}), http.StatusSeeOther)
		s.login(s.cfg.AdminEmail, testAdminPassword)
		requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/cancellation", id),
			url.Values{"decision": {tc.decision}, "note": {"Decided"}}), http.StatusSeeOther)
		if !hasEvent(notificationTitles(t, s, requester.ID), tc.event) {
			t.Errorf("a %sd cancellation does not fire %s", tc.decision, tc.event)
		}
	}
}

// F-F-07 — "Save corrections" leaves the request `returned` on purpose, and the
// seeded template for request_edited says it "was edited and re-sent for
// approval". Firing on both buttons told the approver something was back in
// their queue when nothing was.
func TestSaveCorrectionsDoesNotClaimTheRequestWasResent(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Save only")
	requester := s.seedRequester("saver@example.test", "Save Requester", "SavePass12345")
	id := s.seedPendingRequest(921, requester.ID, admin.ID, headID, 180000)

	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/return", id),
		url.Values{"comment": {"Add the GST breakup"}}), http.StatusSeeOther)

	edit := url.Values{"type": {"reimbursement"}, "treatment": {"budget"},
		"short_title": {"Site visit"}, "project_id": {itoa64(projectOf(t, s, headID))},
		"head_id": {itoa64(headID)}, "amount": {"1,900.00"},
		"purpose":      {"flights and cabs, with the GST split out"},
		"expense_date": {"2026-07-01"}, "manager_id": {itoa64(admin.ID)}}

	s.login("saver@example.test", "SavePass12345")
	saveOnly := edit
	saveOnly.Set("submit_action", "save")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/edit", id), saveOnly), http.StatusSeeOther)
	if hasEvent(notificationTitles(t, s, admin.ID), notify.EventRequestEdited) {
		t.Fatal("Save corrections told the approver the request was re-sent for approval")
	}

	// The resubmit path still fires, because there the claim is true.
	resubmit := edit
	resubmit.Set("submit_action", "resubmit")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/edit", id), resubmit), http.StatusSeeOther)
	if !hasEvent(notificationTitles(t, s, admin.ID), notify.EventRequestEdited) {
		t.Fatal("Resubmit for approval no longer tells the approver at all")
	}
}

// F-G-035 / F-C-04 — the edit and the resubmission are three transactions, so a
// refused resubmit used to commit the correction and audit it as done while the
// requester was shown a 400. Every precondition SubmitRequest checks is now
// checked before UpdateRequest writes.
func TestARefusedResubmitLeavesNothingWritten(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Atomic edit")
	requester := s.seedRequester("atomic@example.test", "Atomic Requester", "AtomicPass1234")
	// Pending, not returned: legalTransitions has no pending→pending edge, so
	// SubmitRequest refuses — which is the finding's own reproduction.
	id := s.seedPendingRequest(931, requester.ID, admin.ID, headID, 200000)

	s.login("atomic@example.test", "AtomicPass1234")
	resp := s.postForm(fmt.Sprintf("/requests/%d/edit", id), url.Values{
		"type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Site visit"},
		"project_id": {itoa64(projectOf(t, s, headID))}, "head_id": {itoa64(headID)},
		"amount": {"31,000.00"}, "purpose": {"flights and cabs"},
		"expense_date": {"2026-07-01"}, "manager_id": {itoa64(admin.ID)},
		"submit_action": {"resubmit"},
	})
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	if !strings.Contains(body, "cannot be submitted") {
		t.Fatalf("the refusal does not name the rule: %s", body)
	}

	req, err := s.st.Request(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if req.Amount != 200000 {
		t.Fatalf("amount = %d after a refused resubmit, want the original 200000", req.Amount)
	}
	trail, err := s.st.Audit(s.ctx, "payment_request", id, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range trail {
		if entry.Action == "update" {
			t.Fatal("the audit records an edit the caller was told had failed")
		}
	}
}

// F-G-002 — an out-of-scope request answered 403 while a missing one answered
// 404, so the status code enumerated the id space. The two are now identical.
func TestOutOfScopeRequestIsIndistinguishableFromAMissingOne(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Oracle")
	owner := s.seedRequester("owner@example.test", "Owner Rao", "OwnerPass12345")
	s.seedRequester("stranger@example.test", "Stranger Rao", "StrangerPass1234")
	id := s.seedApprovedRequest(941, owner.ID, admin.ID, headID, 100000)

	s.login("stranger@example.test", "StrangerPass1234")
	existing := s.request(http.MethodGet, fmt.Sprintf("/requests/%d", id), nil, "")
	missing := s.request(http.MethodGet, "/requests/99999999", nil, "")
	if existing.StatusCode != missing.StatusCode {
		t.Fatalf("an existing out-of-scope request answers %d and a missing one %d — the status code is an existence oracle",
			existing.StatusCode, missing.StatusCode)
	}
	if existing.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", existing.StatusCode)
	}
	if body := responseBody(t, existing); strings.Contains(body, "Office rent") {
		t.Fatal("the refusal echoes a field of the request it refused")
	}
	_ = responseBody(t, missing)
}

// F-G-016 / F-E-03 — the recoverables register is a second view over
// payment_requests and applied no data scope at all, so recoverable_report:view
// handed over every counterparty, amount, requester and repayment note in the
// company regardless of the caller's request scope.
func TestRecoverablesRegisterAppliesTheRequestDataScope(t *testing.T) {
	s := newAppTestServer(t)
	admin, _ := s.seedHead("Recoverable scope")
	other := s.seedRequester("other-rec@example.test", "Other Requester", "OtherRecPass123")
	id := s.seedRecoverableRequest(951, other.ID, admin.ID, "icd", "Kalyani Steels", 500000)

	// A role that pairs the register with a narrower request scope — exactly the
	// role R1 exists to let an administrator build.
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

	list := responseBody(t, s.request(http.MethodGet, "/recoverables/list", nil, ""))
	if strings.Contains(list, "Kalyani Steels") {
		t.Fatal("the register lists a recoverable this caller is refused on /requests/{id}")
	}
	csv := responseBody(t, s.request(http.MethodGet, "/recoverables/list.csv", nil, ""))
	for _, leak := range []string{"Kalyani Steels", "Other Requester"} {
		if strings.Contains(csv, leak) {
			t.Fatalf("the CSV discloses %q to a caller outside its data scope", leak)
		}
	}
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/recoverables/%d", id), nil, ""), http.StatusNotFound)

	// And an administrator, who holds request=all, still sees everything.
	s.login(s.cfg.AdminEmail, testAdminPassword)
	if body := responseBody(t, s.request(http.MethodGet, "/recoverables/list", nil, "")); !strings.Contains(body, "Kalyani Steels") {
		t.Fatal("the scope filter hides a row from a caller who holds request=all")
	}
}

// F-E-04 — the CSV carried Status, Requester and Repayment Notes and the screen
// carried none of them, so nobody who had only ever read the register knew the
// download said more.
func TestRecoverablesRegisterShowsEveryColumnItsCSVExports(t *testing.T) {
	s := newAppTestServer(t)
	admin, _ := s.seedHead("Column parity")
	requester := s.seedRequester("columns@example.test", "Column Requester", "ColumnPass1234")
	s.seedRecoverableRequest(961, requester.ID, admin.ID, "icd", "Parity Counterparty", 400000)

	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/recoverables/list", nil, ""))
	for _, want := range []string{">Status<", ">Requester<", ">Repayment notes<", "Column Requester"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the register does not carry %q, which its own CSV exports", want)
		}
	}
}

// F-G-030 — every CSV amount was a quoted, symbol-bearing, comma-grouped string,
// so Number() of it is NaN and a spreadsheet has to clean the column first.
// F-G-031 — /reports/ytd.csv always exported the head level, whichever tab the
// download was pressed from.
func TestCSVExportsCarryNumbersAndTheLevelTheyWereAskedFor(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Numeric CSV")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	s.settleOneRequest(971, headID, 150000, "1,500.00", "settled", "2026-04-15")

	for _, path := range []string{"/export.csv?month=2026-04", "/reports/ytd.csv?from=2026-04&to=2026-04", "/requests/export.csv?bucket=all"} {
		resp := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, resp, http.StatusOK)
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "charset=utf-8") {
			t.Errorf("%s Content-Type = %q, and ₹ is multi-byte", path, ct)
		}
		body := responseBody(t, resp)
		if strings.Contains(body, "₹") {
			t.Errorf("%s still writes a currency symbol into a data column", path)
		}
		if !strings.Contains(body, "1500.00") {
			t.Errorf("%s does not carry the amount as a plain number: %s", path, body)
		}
	}

	// The level is a parameter, and an unknown one falls back to heads rather
	// than reaching store.Report with a mode it does not know.
	monthly := responseBody(t, s.request(http.MethodGet, "/reports/ytd.csv?from=2026-04&to=2026-04&level=monthly", nil, ""))
	heads := responseBody(t, s.request(http.MethodGet, "/reports/ytd.csv?from=2026-04&to=2026-04&level=heads", nil, ""))
	if monthly == heads {
		t.Fatal("?level= changes nothing, so the export cannot be the tab it was pressed from")
	}
	requireStatus(t, s.request(http.MethodGet, "/reports/ytd.csv?level=nonsense", nil, ""), http.StatusOK)
}

// F-G-014 — /requests ignored ?status= while its own CSV honoured it, so one URL
// described two different sets on the page and in the file. The export also had
// no bucket default where the screen defaulted to "open".
func TestRequestsListAndItsExportDescribeTheSameSet(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Status filter")
	requester := s.seedRequester("statuses@example.test", "Status Requester", "StatusPass1234")
	pending := s.seedPendingRequest(981, requester.ID, admin.ID, headID, 100000)

	s.login(s.cfg.AdminEmail, testAdminPassword)
	number := fmt.Sprintf("PR-2026-%06d", 981)
	// No bucket at all, which is where the defect lived: the screen's own default
	// bucket used to swallow the status.
	for _, path := range []string{
		"/requests?status=rejected", "/requests?bucket=all&status=rejected",
		"/requests/export.csv?status=rejected", "/requests/export.csv?bucket=all&status=rejected",
	} {
		if strings.Contains(responseBody(t, s.request(http.MethodGet, path, nil, "")), number) {
			t.Fatalf("%s lists a pending request, so ?status= is being ignored", path)
		}
	}
	// Bare, both default to the open bucket and both find the pending request.
	screen := responseBody(t, s.request(http.MethodGet, "/requests", nil, ""))
	bare := responseBody(t, s.request(http.MethodGet, "/requests/export.csv", nil, ""))
	if !strings.Contains(screen, number) || !strings.Contains(bare, number) {
		t.Fatal("the screen and its export no longer share a default bucket")
	}
	_ = pending
}

// F-B-16 — the row query capped itself at 200 while the tab count beside it had
// no cap, so the All tab promised 214 and the list drew 200 in silence. The
// screen now says what it is not showing and the export carries every row.
func TestTheRequestsListNeverTruncatesInSilence(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Truncation")
	requester := s.seedRequester("bulk@example.test", "Bulk Requester", "BulkPass12345")
	const total = 205
	for i := 0; i < total; i++ {
		s.seedPendingRequest(1000+i, requester.ID, admin.ID, headID, 100000)
	}

	s.login("bulk@example.test", "BulkPass12345")
	body := responseBody(t, s.request(http.MethodGet, "/requests?bucket=all", nil, ""))
	if !strings.Contains(body, fmt.Sprintf("200 of %d shown", total)) {
		t.Fatalf("the list does not admit what it is not showing: %s", firstLines(body))
	}
	if !strings.Contains(body, "offset=200") {
		t.Fatal("the remaining rows are unreachable: no next page link")
	}

	csv := responseBody(t, s.request(http.MethodGet, "/requests/export.csv?bucket=all", nil, ""))
	rows := strings.Count(strings.TrimSpace(csv), "\n") // one header + one per row
	if rows != total {
		t.Fatalf("the export carries %d data rows, want every one of %d", rows, total)
	}
}

func firstLines(body string) string {
	if len(body) > 400 {
		return body[:400]
	}
	return body
}

// F-E-02 / F-B-17 — the category picker was six literal options unconnected to
// the table, so an admin-added category could never be selected and a
// deactivated one was still offered and only refused after the form was filled
// in. F-E-02's data-integrity half is here too: an unknown code is no longer
// silently rewritten to "emd".
func TestRecoverableCategoryPickerRendersTheLiveCategories(t *testing.T) {
	s := newAppTestServer(t)
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.UpsertRecoverableCategory(s.ctx, admin, 0, "Retention money", true, false, true, 9); err != nil {
		t.Fatal(err)
	}
	// Deactivate a seeded one; the picker must stop offering it.
	cats, err := s.st.ListRecoverableCategories(s.ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var otherID int64
	for _, c := range cats {
		if c.Code == "other" {
			otherID = c.ID
		}
	}
	if otherID == 0 {
		t.Fatal("the seed no longer carries an 'other' category")
	}
	if _, err := s.st.UpsertRecoverableCategory(s.ctx, admin, otherID, "Other", false, false, false, 6); err != nil {
		t.Fatal(err)
	}

	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/requests/new?type=employee_advance", nil, ""))
	if !strings.Contains(body, `value="retention_money"`) {
		t.Fatalf("an admin-added category is not selectable on the real form: %s", firstLines(body))
	}
	if strings.Contains(body, `value="other"`) {
		t.Fatal("a deactivated category is still offered, and will be refused after the form is filled in")
	}
	// The new category requires a project, so the form asks for one — the reveal
	// is driven by the category's own flags, not by a hardcoded emd|pbg pair.
	fields := responseBody(t, s.htmxGet("/requests/new/fields?type=employee_advance&treatment=recoverable&recoverable_category=retention_money"))
	if !strings.Contains(fields, `id="rproject"`) {
		t.Fatalf("the project field an admin-added category requires is not rendered: %s", firstLines(fields))
	}

	// No silent substitution. An unrecognised code resolves to "nothing chosen"
	// rather than to earnest money deposit.
	unknown := responseBody(t, s.htmxGet("/requests/new/fields?type=employee_advance&treatment=recoverable&recoverable_category=made_up"))
	if strings.Contains(unknown, `value="emd" selected`) || strings.Contains(unknown, `value="emd"  selected`) {
		t.Fatal("an unrecognised category is silently rewritten to emd")
	}
	if !strings.Contains(unknown, `<option value="" selected>Choose a category</option>`) {
		t.Fatalf("an unrecognised category does not resolve to 'nothing chosen': %s", firstLines(unknown))
	}
}

func TestNormalizeRecoverableCategoryNeverSubstitutesACode(t *testing.T) {
	cats := []store.RecoverableCategory{
		{Code: "emd", Name: "EMD"}, {Code: "employee_advance", Name: "Employee advance"},
	}
	for _, tc := range []struct{ code, formType, want string }{
		{"emd", "vendor_invoice", "emd"},
		{"", "employee_advance", "employee_advance"},
		{"", "vendor_invoice", ""},
		{"made_up", "vendor_invoice", ""},
		// A deactivated code is not active, so it is not offered and not kept.
		{"other", "employee_advance", ""},
	} {
		if got := normalizeRecoverableCategory(tc.code, tc.formType, cats); got != tc.want {
			t.Errorf("normalizeRecoverableCategory(%q, %q) = %q, want %q", tc.code, tc.formType, got, tc.want)
		}
	}
}

// F-F-03 — four sentences described a wait Phase 5 had already turned into
// app_settings.reminder_pending_days, and the stale banner asserted "the one-day
// mark" against a separately configurable threshold. They read the settings now,
// and they say elapsed days: Wave 2b deleted calendarDaysBetween deliberately.
func TestReminderCopyReadsTheConfiguredThresholds(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Reminder copy")
	requester := s.seedRequester("copy@example.test", "Copy Requester", "CopyPass12345")
	pending := s.seedPendingRequest(991, requester.ID, admin.ID, headID, 100000)
	if err := s.st.SetAppSettings(s.ctx, admin, map[string]string{
		"reminder_pending_days": "7", "reminder_repeat_days": "2", "reminder_stale_days": "3"}); err != nil {
		t.Fatal(err)
	}

	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/return", pending),
		url.Values{"comment": {"Add the invoice"}}), http.StatusSeeOther)
	reserved := s.seedApprovedRequest(992, requester.ID, admin.ID, headID, 100000)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reserved), url.Values{}), http.StatusSeeOther)
	stale := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/reservation/stale", reserved), nil, ""))
	if !strings.Contains(stale, "open 3 days") {
		t.Errorf("the stale banner does not read reminder_stale_days: %s", firstLines(stale))
	}
	if strings.Contains(stale, "one-day mark") || strings.Contains(stale, "calendar day") {
		t.Error("the stale banner still asserts a hardcoded, calendar-day threshold")
	}

	s.login("copy@example.test", "CopyPass12345")
	form := responseBody(t, s.request(http.MethodGet, "/requests/new?type=vendor_invoice", nil, ""))
	if !strings.Contains(form, "nothing happens for 7 days") {
		t.Errorf("the new-request hint does not read reminder_pending_days: %s", firstLines(form))
	}
	returned := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/edit", pending), nil, ""))
	if !strings.Contains(returned, "7-day reminder clock") {
		t.Errorf("the correction screen does not read reminder_pending_days: %s", firstLines(returned))
	}
	for _, body := range []string{form, returned, stale} {
		if strings.Contains(body, "three calendar days") || strings.Contains(body, "three-day") {
			t.Error("a screen still hardcodes three calendar days")
		}
	}
}

// F-G-021 — a request could be raised and approved into a locked month, becoming
// an approved obligation nobody could pay until the month reopened, and the
// accountant only discovered it at the settlement.
func TestARequestCannotBeApprovedIntoALockedMonth(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Locked approval")
	requester := s.seedRequester("locked@example.test", "Locked Requester", "LockedPass1234")
	id := s.seedPendingRequest(996, requester.ID, admin.ID, headID, 100000)
	if _, err := s.st.DB().ExecContext(s.ctx, `UPDATE payment_requests SET needed_by='2027-11-20' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.st.LockMonth(s.ctx, admin, "2027-11", "Year end"); err != nil {
		t.Fatal(err)
	}

	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.postForm(fmt.Sprintf("/requests/%d/approve", id), url.Values{"approved_amount": {"1,000.00"}})
	requireStatus(t, resp, http.StatusConflict)
	if body := responseBody(t, resp); !strings.Contains(body, "2027-11 is locked") {
		t.Fatalf("the refusal does not name the locked month: %s", firstLines(body))
	}
	req, err := s.st.Request(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if req.Status != "pending" {
		t.Fatalf("status = %q after a refused approval, want pending", req.Status)
	}

	// Reopen it and the identical approval succeeds, which proves the refusal was
	// the lock and nothing else.
	if err := s.st.UnlockMonth(s.ctx, admin, "2027-11", "Reopened"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/approve", id),
		url.Values{"approved_amount": {"1,000.00"}}), http.StatusSeeOther)
}

// F-G-019 / F-G-020 — locking a month redirected to /?month=, which has been the
// dashboard since Phase 4 and reads no month at all; and the payment entry
// screen carried no lock affordance, so the whole form was filled in first.
func TestTheLockedMonthFamilyConfirmsItselfAndWarnsInAdvance(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Lock family")
	requester := s.seedRequester("lockui@example.test", "Lock Requester", "LockUIPass1234")
	month := currentMonthForTest()

	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.postForm("/months/"+month+"/lock", url.Values{"reason": {"Audit lock"}})
	requireStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != "/grid?month="+month {
		t.Fatalf("lock redirect = %q, want /grid?month=%s — the dashboard confirms nothing", loc, month)
	}
	landing := responseBody(t, s.request(http.MethodGet, resp.Header.Get("Location"), nil, ""))
	if !strings.Contains(landing, "Audit lock") {
		t.Fatal("the operator is still not shown the lock they just applied")
	}

	// The entry screen says so before the accountant types anything.
	id := s.seedApprovedRequest(997, requester.ID, admin.ID, headID, 100000)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", id), url.Values{}), http.StatusSeeOther)
	entry := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/payments/new?request=%d", id), nil, ""))
	if !strings.Contains(entry, `class="locked"`) {
		t.Fatalf("the payment entry screen carries no lock warning: %s", firstLines(entry))
	}

	unlock := s.postForm("/months/"+month+"/unlock", url.Values{"reason": {"Reopened"}})
	requireStatus(t, unlock, http.StatusSeeOther)
	if loc := unlock.Header.Get("Location"); loc != "/grid?month="+month {
		t.Fatalf("unlock redirect = %q, want /grid?month=%s", loc, month)
	}
}

// F-D-02 — ReserveRequest refuses for three reasons and the conflict screen
// reported all three as "Someone else took this request before you", about a
// request nobody held.
func TestTheConflictScreenNamesTheRealRefusal(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Conflict causes")
	requester := s.seedRequester("conflict@example.test", "Conflict Requester", "ConflictPass123")

	s.login(s.cfg.AdminEmail, testAdminPassword)
	held := s.seedApprovedRequest(998, requester.ID, admin.ID, headID, 100000)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", held),
		url.Values{"reason": {"Which cost centre is this?"}}), http.StatusSeeOther)
	resp := s.postForm(fmt.Sprintf("/requests/%d/record-payment", held), url.Values{})
	requireStatus(t, resp, http.StatusConflict)
	body := responseBody(t, resp)
	if !strings.Contains(body, "is on hold") {
		t.Fatalf("an on-hold refusal does not say so: %s", firstLines(body))
	}
	for _, lie := range []string{"took this request before you", "Someone else"} {
		if strings.Contains(body, lie) {
			t.Fatalf("the screen still claims %q about a request nobody holds", lie)
		}
	}
	if !strings.Contains(body, "Which cost centre is this?") {
		t.Fatal("the hold reason — the answer the accountant needs — is not on the screen")
	}

	// A completed request is neither taken nor on hold.
	done, _ := s.settleOneRequest(999, headID, 100000, "1,000.00", "settled", "2026-05-15")
	resp = s.postForm(fmt.Sprintf("/requests/%d/record-payment", done), url.Values{})
	requireStatus(t, resp, http.StatusConflict)
	body = responseBody(t, resp)
	if !strings.Contains(body, "not available to process") {
		t.Fatalf("a completed request is not reported as unavailable: %s", firstLines(body))
	}
	if strings.Contains(body, "took this request before you") {
		t.Fatal("a completed request is still reported as somebody else's reservation")
	}
}

// F-G-006 / F-G-007 / F-G-018 — the dashboard's own three disagreements with the
// screens it links to: a tile that omitted `returned` from a bucket that includes
// it, an "Approved, unclaimed" count with no on_hold test beside a queue metric
// of the same name that has one, and a work area built with Scope:"all" written
// into the call.
func TestDashboardAreasAgreeWithTheQueuesTheyLinkTo(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Dashboard truth")
	requester := s.seedRequester("dash@example.test", "Dash Requester", "DashPass12345")

	// One pending and one returned request: "In progress" links to bucket=open,
	// which includes returned.
	pending := s.seedPendingRequest(1301, requester.ID, admin.ID, headID, 100000)
	returned := s.seedPendingRequest(1302, requester.ID, admin.ID, headID, 110000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/return", returned),
		url.Values{"comment": {"Add the invoice"}}), http.StatusSeeOther)
	_ = pending

	s.login("dash@example.test", "DashPass12345")
	tile := dashboardTile(t, s, "My open requests")
	bucket, err := s.st.CountRequests(s.ctx, store.RequestListOptions{Scope: "own", ViewerID: requester.ID, Bucket: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if tile != bucket {
		t.Fatalf("the In-progress tile reads %d and /requests?bucket=open holds %d", tile, bucket)
	}

	// The accounts area: hardwired scope, and no on_hold test.
	other := s.seedRequester("dash-other@example.test", "Other Dash", "OtherDashPass12")
	visible := s.seedApprovedRequest(1303, other.ID, admin.ID, headID, 120000)
	s.seedProbeUser("processor@example.test", "Processor Rao", "ProcessorPass12", "Narrow processor",
		[]store.Grant{{Resource: "payment", Action: "process"}, {Resource: "request", Action: "view"}},
		[]store.ScopeGrant{{Resource: "request", Scope: "own"}})
	s.login("processor@example.test", "ProcessorPass12")
	body := responseBody(t, s.request(http.MethodGet, "/", nil, ""))
	if strings.Contains(body, fmt.Sprintf("PR-2026-%06d", 1303)) {
		t.Fatal("the dashboard lists an approved request the caller cannot open")
	}
	_ = visible

	// With a hold placed, the tile drops exactly as the queue's metric does.
	s.login(s.cfg.AdminEmail, testAdminPassword)
	before := dashboardTile(t, s, "Approved, unclaimed")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", visible),
		url.Values{"reason": {"Waiting on a reply"}}), http.StatusSeeOther)
	after := dashboardTile(t, s, "Approved, unclaimed")
	if after != before-1 {
		t.Fatalf("the Approved-unclaimed tile went %d → %d across a hold; the queue's metric of the same name drops by one", before, after)
	}
}

// dashboardTile reads one metric straight off the rendered dashboard, so the
// test asserts the number a reader is shown rather than the one the handler
// computed.
var tilePattern = regexp.MustCompile(`(?s)<span class="metric-label">([^<]*)</span>\s*<span class="metric-value">([^<]*)</span>`)

func dashboardTile(t *testing.T, s *appTestServer, label string) int {
	t.Helper()
	body := responseBody(t, s.request(http.MethodGet, "/", nil, ""))
	for _, match := range tilePattern.FindAllStringSubmatch(body, -1) {
		if strings.TrimSpace(match[1]) != label {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(match[2]))
		if err != nil {
			t.Fatalf("tile %q reads %q, which is not a number", label, match[2])
		}
		return n
	}
	t.Fatalf("the dashboard has no %q tile: %s", label, firstLines(body))
	return 0
}

// seedPendingRequest is a request awaiting its approver, shaped the way the
// reimbursement form makes one: project and head, an expense date, and a payee
// the store forces to the requester. Reimbursement rather than vendor_invoice
// because it needs no vendor row, so a test that re-posts the whole form does not
// have to seed the vendor master too.
func (s *appTestServer) seedPendingRequest(seq int, requesterID, managerID, headID, amount int64) int64 {
	s.t.Helper()
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		s.t.Fatal(err)
	}
	res, err := s.st.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,project_id,head_id,amount,purpose,short_title,expense_date,vendor_payee,requester_id,manager_id,submitted_at)
		VALUES(?,'pending','budget','reimbursement',?,?,?,?,?,'2026-07-01',?,?,?,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), projectID, headID, amount, "Site visit", "Site visit",
		"Requester Payee", requesterID, managerID)
	if err != nil {
		s.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedRecoverableRequest is an approved recoverable in the register, with its
// category id linked the way both write paths populate it since migration v6 —
// every register query joins on it, so a row without it shows a blank category.
func (s *appTestServer) seedRecoverableRequest(seq int, requesterID, managerID int64, code, counterparty string, amount int64) int64 {
	s.t.Helper()
	var catID int64
	if err := s.st.DB().QueryRow(`SELECT id FROM recoverable_categories WHERE code=?`, code).Scan(&catID); err != nil {
		s.t.Fatal(err)
	}
	res, err := s.st.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,recoverable_category,recoverable_category_id,amount,purpose,short_title,counterparty,expected_return_date,repayment_notes,requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?,'approved','recoverable','employee_advance',?,?,?,?,?,?,'2027-03-31',?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), code, catID, amount, "Inter-corporate deposit", "ICD",
		counterparty, "Refundable on maturity", requesterID, managerID, amount, managerID)
	if err != nil {
		s.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// currentMonthForTest is the month a settlement recorded now lands in, which is
// the month the payment entry screen tests its lock against.
func currentMonthForTest() string { return time.Now().Format("2006-01") }

// F-A-05 / F-B-09 — request documents were linked to /attachments/{id}, which
// reads payment_attachments. The link 404ed, or served an unrelated payment's
// file under the request document's name.
func TestRequestDocumentsLinkToTheirOwnRoute(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Doc links")
	requester := s.seedRequester("docs@example.test", "Doc Requester", "DocPass12345")
	id := s.seedPendingRequest(1401, requester.ID, admin.ID, headID, 100000)
	attID, err := s.st.AddRequestAttachment(s.ctx, requester, id, s.seedAttachmentFile("invoice-mine.txt", "mine"))
	if err != nil {
		t.Fatal(err)
	}

	s.login("docs@example.test", "DocPass12345")
	want := fmt.Sprintf(`href="/requests/%d/attachments/%d"`, id, attID)
	detail := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", id), nil, ""))
	if !strings.Contains(detail, want) {
		t.Fatalf("the request detail does not link a document to its own route: %s", firstLines(detail))
	}
	if strings.Contains(detail, fmt.Sprintf(`href="/attachments/%d"`, attID)) {
		t.Fatal("the request detail still points a request document at the payment attachment route")
	}
	edit := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/edit", id), nil, ""))
	if !strings.Contains(edit, want) {
		t.Fatal("the correction screen lists a document with no way to open it")
	}
	// And the link works, which is the whole point.
	resp := s.request(http.MethodGet, fmt.Sprintf("/requests/%d/attachments/%d", id, attID), nil, "")
	requireStatus(t, resp, http.StatusOK)
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "invoice-mine.txt") {
		t.Fatalf("Content-Disposition = %q, want the requester's own document", cd)
	}
	_ = responseBody(t, resp)
}

// F-G-033 — the heads screen listed every head and populated each row's project
// select from the active projects only, so a head on a retired project had no
// option of its own and that row's Save moved it.
func TestHeadRowsOfferTheirOwnRetiredProject(t *testing.T) {
	s := newAppTestServer(t)
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	_ = admin
	retiredID, err := s.st.UpsertProject(s.ctx, 0, "Retired Programme", true, 5)
	if err != nil {
		t.Fatal(err)
	}
	headID, err := s.st.UpsertHead(s.ctx, 0, retiredID, "Stranded head", "5", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.UpsertProject(s.ctx, retiredID, "Retired Programme", false, 5); err != nil {
		t.Fatal(err)
	}

	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/heads", nil, ""))
	want := fmt.Sprintf(`<option value="%d" selected>Retired Programme (retired)</option>`, retiredID)
	if !strings.Contains(body, want) {
		t.Fatalf("the row offers no option for the head's own retired project: %s", firstLines(body))
	}

	// Saving the row unchanged leaves the head where it is.
	requireStatus(t, s.postForm("/heads", url.Values{
		"id": {itoa64(headID)}, "project_id": {itoa64(retiredID)},
		"name": {"Stranded head"}, "due_day": {"5"}, "active": {"on"}, "sort_order": {"1"},
	}), http.StatusSeeOther)
	var got int64
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT project_id FROM heads WHERE id=?`, headID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != retiredID {
		t.Fatalf("project_id = %d after an unchanged Save, want %d", got, retiredID)
	}
}

// F-F-01 — the per-event sheets on /admin/notifications carried no `hidden`,
// unlike every other .overlay, so all of them laid out full-viewport at once and
// only the last was reachable by a pointer.
// F-G-004 / F-C-06 — the audit filters could not reach payment_request.
// F-D-07 — the Accounts queue shipped only the mobile filter form.
// F-F-05 — the notification centre had no desktop entry point.
func TestScreensThatWereUnusableAsRendered(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	rules := responseBody(t, s.request(http.MethodGet, "/admin/notifications", nil, ""))
	sheets := strings.Count(rules, `<div class="overlay" id="ev-`)
	hidden := strings.Count(rules, `<div class="overlay" id="ev-`) - strings.Count(rules, `<div class="overlay" id="ev-{{`)
	if sheets == 0 {
		t.Fatal("no per-event sheets are rendered at all")
	}
	if strings.Count(rules, ` hidden>`) < hidden {
		t.Fatalf("%d event sheets and fewer hidden attributes — the stack covers every control beneath it", sheets)
	}
	for _, event := range notify.AllEvents {
		if !strings.Contains(rules, fmt.Sprintf(`<div class="overlay" id="ev-%s" hidden>`, event)) {
			t.Fatalf("the %s sheet is not hidden", event)
		}
	}

	audit := responseBody(t, s.request(http.MethodGet, "/audit", nil, ""))
	for _, want := range []string{`value="payment_request"`, `value="approve"`, `value="settle"`, `value="reassign"`} {
		if !strings.Contains(audit, want) {
			t.Errorf("the audit filters cannot ask for %s", want)
		}
	}

	queue := responseBody(t, s.request(http.MethodGet, "/accounts-queue", nil, ""))
	if !strings.Contains(queue, `<form class="toolbar" method="get" action="/accounts-queue">`) {
		t.Error("the Accounts queue still ships only the mobile filter form")
	}
	if !strings.Contains(queue, `id="queue-q"`) {
		t.Error("the desktop search input is gone")
	}

	dash := responseBody(t, s.request(http.MethodGet, "/", nil, ""))
	if !strings.Contains(dash, `class="sidebar"`) || !strings.Contains(dash, `href="/notifications"`) {
		t.Error("the sidebar has no entry point to the notification centre")
	}
}

// F-G-013 — navSpec declared Badge:"approvals" and Badge:"accounts_queue" and no
// query ever populated them, so the two counts a person most needs at a glance
// were the two that were never built.
func TestTheTwoDeclaredWorkQueueBadgesArePopulated(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Badges")
	requester := s.seedRequester("badge@example.test", "Badge Requester", "BadgePass12345")
	s.seedPendingRequest(1501, requester.ID, admin.ID, headID, 100000)
	s.seedApprovedRequest(1502, requester.ID, admin.ID, headID, 110000)

	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/", nil, ""))
	for _, want := range []string{`href="/approvals"`, `href="/accounts-queue"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("the nav item %s is missing", want)
		}
	}
	// One pending approval and one takeable request, so both badges read 1.
	if strings.Count(body, `<span class="n">1</span>`) < 2 {
		t.Fatalf("the approvals and accounts-queue badges are not populated: %s", firstLines(body))
	}
}

// F-B-08 — every validation banner opened with the internal sentinel
// "validation failed: ", which is not a sentence for a person.
func TestValidationMessagesLoseTheSentinelPrefix(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.postForm("/heads", url.Values{"project_id": {"0"}, "name": {""}, "due_day": {"5"}})
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	if strings.Contains(body, "validation failed") {
		t.Fatalf("the banner still carries the sentinel: %s", firstLines(body))
	}
	if !strings.Contains(body, "project and head name are required") {
		t.Fatalf("the banner lost the rule with the sentinel: %s", firstLines(body))
	}
}

// F-E-08 / F-B-02 — a refused `type=recoverable` submission looked up a UI label
// it has none of and answered "That is not a kind of request this system raises."
// about a type the store had accepted seconds earlier, throwing the form away.
func TestARefusedUnlabelledTypeKeepsItsRealReason(t *testing.T) {
	s := newAppTestServer(t)
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	approver := seedSecondApprover(t, s)
	_ = admin
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.postForm("/requests", url.Values{
		"type": {"recoverable"}, "treatment": {"recoverable"},
		"recoverable_category": {"pbg"}, "short_title": {"Bank guarantee"},
		"amount": {"1,00,000.00"}, "purpose": {"guarantee against the tender"},
		"expected_return_date": {"2027-01-01"}, "repayment_notes": {"Released on completion"},
		"manager_id": {itoa64(approver)},
	})
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	if strings.Contains(body, "not a kind of request this system raises") {
		t.Fatalf("the refusal is about the wrong problem: %s", firstLines(body))
	}
	if !strings.Contains(body, "belongs to a project") {
		t.Fatalf("the rule that actually refused it is not reported: %s", firstLines(body))
	}
}

func TestCSVAmountIsANumber(t *testing.T) {
	for _, tc := range []struct {
		paise int64
		want  string
	}{{0, "0.00"}, {5, "0.05"}, {150000, "1500.00"}, {-2350, "-23.50"}, {100, "1.00"}} {
		if got := csvAmount(tc.paise); got != tc.want {
			t.Errorf("csvAmount(%d) = %q, want %q", tc.paise, got, tc.want)
		}
	}
}
