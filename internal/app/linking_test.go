package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// Phase 3 — payment linking, reservation and settlement, at the HTTP layer.
//
// Every test here drives the product the way an accountant does: take a request
// out of the queue, record what left the bank, say whether that settles it. The
// store already refuses the illegal moves; these prove the screens never offer
// them, and that the one endpoint which must not write, does not write.

// seedApprovedRequest inserts an approved, unclaimed request directly. Going
// through the UI would need a second user for the approval (G8) on every single
// fixture, and none of these tests are about how a request gets approved.
func (s *appTestServer) seedApprovedRequest(seq int, requesterID, managerID, headID, amount int64) int64 {
	s.t.Helper()
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		s.t.Fatal(err)
	}
	res, err := s.st.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,project_id,head_id,amount,purpose,short_title,vendor_payee,requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?,'approved','budget','vendor_invoice',?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), projectID, headID, amount, "Office rent", "Office rent", "Acme Landlord",
		requesterID, managerID, amount, managerID)
	if err != nil {
		s.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedVendorRequest inserts an approved request the way the *real form* writes
// one: the payee is a vendor_id and the vendor_payee snapshot is empty. That
// column is only filled for reimbursement and employee advance, so every fixture
// that writes vendor_payee directly is the one shape the UI can never produce —
// which is exactly how the blank-payee defect survived a green suite.
func (s *appTestServer) seedVendorRequest(seq int, requesterID, managerID, headID, amount int64, vendorName string) (reqID, vendorID int64) {
	s.t.Helper()
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		s.t.Fatal(err)
	}
	vres, err := s.st.DB().Exec(`INSERT INTO vendors(name,status) VALUES(?,'active')`, vendorName)
	if err != nil {
		s.t.Fatal(err)
	}
	vendorID, _ = vres.LastInsertId()
	res, err := s.st.DB().Exec(`INSERT INTO payment_requests(number,status,treatment,type,project_id,head_id,amount,purpose,short_title,vendor_id,vendor_payee,requester_id,manager_id,approved_amount,approved_by,approved_at,submitted_at)
		VALUES(?,'approved','budget','vendor_invoice',?,?,?,?,?,?,'',?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		fmt.Sprintf("PR-2026-%06d", seq), projectID, headID, amount, "Annual licence", "Annual licence", vendorID,
		requesterID, managerID, amount, managerID)
	if err != nil {
		s.t.Fatal(err)
	}
	reqID, _ = res.LastInsertId()
	return reqID, vendorID
}

// strconvPath keeps the id-in-a-path formatting in one place, so the older test
// file does not have to grow an "fmt" import for it.
func strconvPath(format string, id int64) string { return fmt.Sprintf(format, id) }

func requestStatusApp(t *testing.T, s *appTestServer, id int64) string {
	t.Helper()
	var status string
	if err := s.st.DB().QueryRow(`SELECT status FROM payment_requests WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

// seedColleague creates a second signed-in-able user so a reservation can be
// held by somebody other than the caller.
func (s *appTestServer) seedColleague(email, name, password string) store.User {
	s.t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.t.Fatal(err)
	}
	id, err := s.st.CreateUser(s.ctx, email, name, hash, "admin", true)
	if err != nil {
		s.t.Fatal(err)
	}
	u, err := s.st.UserByID(s.ctx, id)
	if err != nil {
		s.t.Fatal(err)
	}
	return u
}

// ---------------------------------------------------------------------------
// Task 11 — reservation entry point, permission verbs, conflict screen
// ---------------------------------------------------------------------------

func TestReservationEntryPointReservesAndRedirects(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Reserve")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	resp := s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{})
	requireStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != fmt.Sprintf("/payments/new?request=%d", reqID) {
		t.Fatalf("reserve redirect = %q", loc)
	}
	_ = responseBody(t, resp)
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status after reserve = %q, want processing", got)
	}
}

// TestReservationConflictRendersScreenNotErrorPage is the G15 proof.
func TestReservationConflictRendersScreenNotErrorPage(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Taken")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 3350000)
	// Someone already reserved it.
	if err := s.st.ReserveRequest(s.ctx, admin, reqID); err != nil {
		t.Fatal(err)
	}
	// A second accountant tries via the button.
	s.seedColleague("second@example.test", "Second Acct", "SecondPass1234")
	s.login("second@example.test", "SecondPass1234")
	resp := s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{})
	requireStatus(t, resp, http.StatusConflict)
	body := responseBody(t, resp)

	// It is a screen, not the generic error page.
	if strings.Contains(body, `class="error-state"`) || strings.Contains(body, "Request ID:") {
		t.Fatalf("conflict rendered the generic error page: %s", body)
	}
	for _, want := range []string{
		`class="banner bad"`,               // names the winner, says nothing was saved
		"Test Admin took this request",     // who won
		"Nothing you typed has been saved", // and that no payment was created
		`class="req-head"`,                 // the request in context
		`class="pill processing"`,          // its state
		`class="waiting"`,                  // reserved-at line
		`class="a-list"`,                   // what you can do next
		"Go back to the queue",
		"Ask for it to be reassigned",
		"Open the request read-only",
		`class="action-bar"`,
		"PR-2026-000001",
		"33,500.00",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("conflict screen missing %q:\n%s", want, body)
		}
	}
	// The reservation is untouched by the loser.
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status = %q, want processing", got)
	}
}

// ---------------------------------------------------------------------------
// Task 12 — the accounts queue
// ---------------------------------------------------------------------------

func TestAccountsQueueRendersMetricsTabsAndRowActions(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Queue")
	open := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 23500000)
	mine := s.seedApprovedRequest(2, admin.ID, admin.ID, headID, 10000000)
	held := s.seedApprovedRequest(3, admin.ID, admin.ID, headID, 2500000)
	if err := s.st.ReserveRequest(s.ctx, admin, mine); err != nil {
		t.Fatal(err)
	}
	if err := s.st.HoldRequest(s.ctx, admin, held, "await vendor GST"); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/accounts-queue", nil, ""))
	for _, want := range []string{
		`class="metric-strip"`,
		"Approved, unclaimed",
		"Reserved by you",
		"Reserved by others",
		"On hold",
		`class="segmented"`,
		"Approved <span class=\"n\">1</span>",
		"Processing <span class=\"n\">1</span>",
		"On hold <span class=\"n\">1</span>",
		"Partial review <span class=\"n\">0</span>",
		"Paid <span class=\"n\">0</span>",
		`class="m-filters"`,
		`<table class="t-cards">`,
		`data-label="Payee"`,
		"Take for processing",
		"PR-2026-000001",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("queue missing %q:\n%s", want, body)
		}
	}
	// A reserved-by-you row offers Resume, never a second Take.
	if strings.Count(body, "Take for processing") != 1 {
		t.Fatalf("Take offered for a non-takeable row:\n%s", body)
	}
	// Design system, not the legacy markup.
	if strings.Contains(body, `class="badge`) {
		t.Fatalf("queue still renders .badge (D5):\n%s", body)
	}

	// The Processing tab shows the reserved row and hides the open one.
	proc := responseBody(t, s.request(http.MethodGet, "/accounts-queue?tab=processing", nil, ""))
	if !strings.Contains(proc, "PR-2026-000002") || strings.Contains(proc, "PR-2026-000001") {
		t.Fatalf("processing tab wrong:\n%s", proc)
	}
	if !strings.Contains(proc, "Resume") {
		t.Fatalf("processing tab has no Resume action:\n%s", proc)
	}
	// The hold tab shows the held row, which is never takeable.
	hold := responseBody(t, s.request(http.MethodGet, "/accounts-queue?tab=hold", nil, ""))
	if !strings.Contains(hold, "PR-2026-000003") || strings.Contains(hold, "Take for processing") {
		t.Fatalf("hold tab wrong:\n%s", hold)
	}
	// An unknown tab is rejected, not silently treated as Approved.
	resp := s.request(http.MethodGet, "/accounts-queue?tab=nonsense", nil, "")
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)
	_ = open
}

func TestAccountsQueueSearchNarrowsRows(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("QueueSearch")
	s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.seedApprovedRequest(2, admin.ID, admin.ID, headID, 600000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/accounts-queue?q=PR-2026-000002", nil, ""))
	if !strings.Contains(body, "PR-2026-000002") || strings.Contains(body, "PR-2026-000001") {
		t.Fatalf("queue search did not narrow:\n%s", body)
	}
	// Counts are scope-wide, not search-scoped: the tab still says 2.
	if !strings.Contains(body, "Approved <span class=\"n\">2</span>") {
		t.Fatalf("search distorted the tab counts:\n%s", body)
	}
}

// ---------------------------------------------------------------------------
// Task 13 — the request picker
// ---------------------------------------------------------------------------

func TestPaymentPickerRendersComboAndTakenRows(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Picker")
	open := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 10000000)
	taken := s.seedApprovedRequest(2, admin.ID, admin.ID, headID, 3350000)
	deepak := s.seedColleague("deepak@example.test", "Deepak Menon", "DeepakPass1234")
	if err := s.st.ReserveRequest(s.ctx, deepak, taken); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/payments/new", nil, ""))
	for _, want := range []string{
		`class="banner info"`,
		"Free payment entry has been removed",
		`class="combo"`,
		`class="combo-input"`,
		`class="combo-list"`,
		`class="co"`,
		"PR-2026-000001",
		`class="co-amt"`,
		`class="co is-taken"`,
		"Reserved by Deepak Menon",
		"Recently paid by you",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("picker missing %q:\n%s", want, body)
		}
	}
	// The picker promises no control this phase does not build: the mockup's
	// "an administrator can re-enable direct entry in Configuration" is absent.
	if strings.Contains(body, "re-enable direct entry") {
		t.Fatalf("picker promises a Configuration toggle that does not exist:\n%s", body)
	}
	// A taken row must not be postable: no reservation action inside it.
	takenRow := body[strings.Index(body, `class="co is-taken"`):]
	if i := strings.Index(takenRow, "</a>"); i > 0 && strings.Contains(takenRow[:i], "/record-payment") {
		t.Fatalf("taken row offers a reservation action:\n%s", takenRow[:i])
	}
	// htmx fragment: the list only, no shell.
	frag := responseBody(t, s.htmxGet("/payments/new/options?q=PR-2026-000001"))
	if !strings.Contains(frag, `class="combo-list"`) {
		t.Fatalf("options fragment missing the list:\n%s", frag)
	}
	if strings.Contains(frag, "<aside") || strings.Contains(frag, "<!doctype") {
		t.Fatalf("options fragment rendered the whole shell:\n%s", frag)
	}
	if strings.Contains(frag, "PR-2026-000002") {
		t.Fatalf("options fragment ignored the search:\n%s", frag)
	}
	_ = open
}

func TestPaymentPickerListsOnlyYourOwnRecentPayments(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Recent")
	deepak := s.seedColleague("deepak@example.test", "Deepak Menon", "DeepakPass1234")
	// Deepak settles one; the admin must never see it under "paid by you".
	theirs := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	if err := s.st.ReserveRequest(s.ctx, deepak, theirs); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.RecordPaymentForRequest(s.ctx, deepak, theirs,
		store.PaymentInput{HeadID: headID, PaidOn: "2026-07-24", Amount: 500000, VendorPayee: "Nova Print Works"},
		"settled", "", nil); err != nil {
		t.Fatal(err)
	}
	// The admin settles their own.
	mine := s.seedApprovedRequest(2, admin.ID, admin.ID, headID, 400000)
	if err := s.st.ReserveRequest(s.ctx, admin, mine); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.RecordPaymentForRequest(s.ctx, admin, mine,
		store.PaymentInput{HeadID: headID, PaidOn: "2026-07-25", Amount: 300000, VendorPayee: "Acme Landlord"},
		"partial", "balance later", nil); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/payments/new", nil, ""))
	recent := body[strings.Index(body, "Recently paid by you"):]
	if !strings.Contains(recent, "PR-2026-000002") {
		t.Fatalf("the caller's own settlement is missing:\n%s", recent)
	}
	if strings.Contains(recent, "PR-2026-000001") {
		t.Fatalf("another accountant's settlement leaked into 'paid by you':\n%s", recent)
	}
	if !strings.Contains(recent, `class="pill partial"`) {
		t.Fatalf("a partial settlement is not marked as one:\n%s", recent)
	}
}

// ---------------------------------------------------------------------------
// Task 14 — the payment entry screen
// ---------------------------------------------------------------------------

func TestPaymentEntryScreenShowsReservationApprovalAndRule(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Entry")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 10000000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	body := responseBody(t, s.request(http.MethodGet, strconvPath("/payments/new?request=%d", reqID), nil, ""))
	for _, want := range []string{
		`class="reserve-bar"`,
		"Reserved by you",
		`class="rb-actions"`,
		"What was approved",
		`class="dl"`,
		"A payment can never exceed this",
		`class="field span-6 money-field"`,
		`class="in-words"`,
		`id="diff-banner"`,
		`class="uploader"`,
		`class="action-bar"`,
		"Nothing is saved until you confirm",
		`name="request_id" value="`,
		"1,00,000.00",
		// S14: the request's own figures arrive prefilled.
		"Acme Landlord",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("payment entry missing %q:\n%s", want, body)
		}
	}
	// A <details> is not an acceptable settlement control (design system rule).
	if strings.Contains(body, "<details") {
		t.Fatalf("payment entry still uses a <details> disclosure:\n%s", body)
	}
	// The form uploads once, on the final POST.
	if !strings.Contains(body, `enctype="multipart/form-data"`) || !strings.Contains(body, `action="/payments"`) {
		t.Fatalf("payment form is not the single multipart POST to /payments:\n%s", body)
	}
}

func TestPaymentEntryRefusesSomeoneElsesReservation(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("NotMine")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	deepak := s.seedColleague("deepak@example.test", "Deepak Menon", "DeepakPass1234")
	if err := s.st.ReserveRequest(s.ctx, deepak, reqID); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.request(http.MethodGet, strconvPath("/payments/new?request=%d", reqID), nil, "")
	requireStatus(t, resp, http.StatusConflict)
	body := responseBody(t, resp)
	if !strings.Contains(body, "Deepak Menon took this request") || !strings.Contains(body, `class="a-list"`) {
		t.Fatalf("opening someone else's reservation must land on the conflict screen (G15):\n%s", body)
	}
}

// Historical, request-less payments stay editable (X6) — the linked entry
// screen replaced the create form, not the ledger's edit screen.
func TestHistoricalPaymentEditFormStillRenders(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Historical")
	payID, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{HeadID: headID, PaidOn: "2026-04-12", Amount: 12345, VendorPayee: "Legacy Vendor", PaymentMode: "cash"})
	if err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, strconvPath("/payments/%d/edit", payID), nil, ""))
	for _, want := range []string{"Legacy Vendor", `name="head_id"`, "Save Payment"} {
		if !strings.Contains(body, want) {
			t.Fatalf("historical payment edit form missing %q:\n%s", want, body)
		}
	}
}

func TestPaymentCreateRequiresReservedRequest(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("X5")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	// No request_id at all → refused.
	form := url.Values{"head_id": {strconvFormat(headID)}, "paid_on": {"2026-06-15"}, "amount": {"100.00"}, "settlement": {"settled"}}
	resp := s.postForm("/payments", form)
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)
	// A request the caller has NOT reserved → refused.
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	form.Set("request_id", strconvFormat(reqID))
	resp = s.postForm("/payments", form)
	if resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("unreserved link accepted: %d", resp.StatusCode)
	}
	_ = responseBody(t, resp)
	var n int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("request-less/unreserved payment wrote %d rows (X5 broken)", n)
	}
}

// ---------------------------------------------------------------------------
// Task 15 — the settlement preview. D8/G16: it renders the decision and writes
// nothing. The only writer in the whole flow is POST /payments.
// ---------------------------------------------------------------------------

func TestSettlementPreviewWritesNothingAndRendersTheSheet(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Preview")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 10000000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	form := url.Values{"amount": {"98000.00"}, "paid_on": {"2026-07-25"}, "payment_mode": {"bank_transfer"}, "reference_no": {"N221260725004417"}}
	resp := s.postForm(strconvPath("/requests/%d/settlement-preview", reqID), form)
	requireStatus(t, resp, http.StatusOK)
	body := responseBody(t, resp)
	for _, want := range []string{
		`class="overlay"`, `class="sheet"`, `class="sh-head"`, `class="sh-body`, `class="sh-foot"`,
		`class="compare"`, `class="cmp-row"`, `class="cmp-row diff"`,
		"1,00,000.00", "98,000.00", "2,000.00",
		`class="choice"`,
		`class="outcome good"`, "Completed",
		`class="outcome warn"`, "Manager review",
		`data-when="settlement:partial"`,
		`class="banner info"`, "cannot be edited or cancelled",
		`value="settled"`, `value="partial"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("settlement sheet missing %q:\n%s", want, body)
		}
	}
	// D8/G16: nothing was persisted, and the request is still merely reserved.
	var payments int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&payments); err != nil {
		t.Fatal(err)
	}
	if payments != 0 {
		t.Fatalf("the preview wrote %d payment rows (D8 broken)", payments)
	}
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status after preview = %q, want processing", got)
	}
	// The plan names this table "attachments"; the schema calls it
	// payment_attachments. Same assertion, real table.
	var atts int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM payment_attachments`).Scan(&atts); err != nil {
		t.Fatal(err)
	}
	if atts != 0 {
		t.Fatalf("the preview staged %d attachments (D8 broken)", atts)
	}
}

func TestSettlementPreviewMatchesRowAndNoJSFullPage(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("PreviewExact")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"amount": {"5000.00"}, "paid_on": {"2026-07-25"}, "payment_mode": {"bank_transfer"}, "reference_no": {"N1"}}

	// No-JS: a whole page, shell and all, wrapping the same sheet.
	page := responseBody(t, s.postForm(strconvPath("/requests/%d/settlement-preview", reqID), form))
	if !strings.Contains(page, `class="cmp-row match"`) || strings.Contains(page, `class="cmp-row diff"`) {
		t.Fatalf("equal amounts must render .match, not .diff:\n%s", page)
	}
	if !strings.Contains(page, "<aside") {
		t.Fatalf("the no-JS path must render the full page:\n%s", page)
	}
	// The no-JS confirmation carries every field forward so the confirm posts once.
	for _, want := range []string{`name="amount"`, `name="paid_on"`, `name="payment_mode"`, `name="reference_no"`, `name="request_id"`, `enctype="multipart/form-data"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("no-JS confirmation missing %q:\n%s", want, page)
		}
	}

	// htmx: the fragment only.
	frag := responseBody(t, s.postFormHX(strconvPath("/requests/%d/settlement-preview", reqID), form))
	if !strings.Contains(frag, `class="overlay"`) {
		t.Fatalf("htmx fragment missing the sheet:\n%s", frag)
	}
	if strings.Contains(frag, "<aside") || strings.Contains(frag, "<!doctype") {
		t.Fatalf("htmx fragment rendered the shell:\n%s", frag)
	}
}

func TestSettlementPreviewRefusesWhenTheReservationIsGone(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("PreviewLost")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	// An authorised colleague takes it away mid-flow.
	if err := s.st.ReleaseRequest(s.ctx, admin, reqID, "needed elsewhere", true, true); err != nil {
		t.Fatal(err)
	}
	resp := s.postForm(strconvPath("/requests/%d/settlement-preview", reqID), url.Values{"amount": {"5000.00"}, "paid_on": {"2026-07-25"}})
	requireStatus(t, resp, http.StatusConflict)
	if body := responseBody(t, resp); !strings.Contains(body, `class="a-list"`) {
		t.Fatalf("a lost reservation must land on the conflict screen:\n%s", body)
	}
}

// Task 16 — the settlement lands on the payment, and the payment carries the
// whole story: what was approved, what left the bank, who did each step, and
// nothing that would let anybody change it afterwards (S10, S12, S13, Q4).

func TestSettlementFlowCompletesAndShowsPaymentDetail(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Settle")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 10000000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	// Settle for less than approved (S10).
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-25"}, "amount": {"98000.00"}, "vendor_payee": {"Acme Landlord"}, "settlement": {"settled"}, "reference_no": {"N221260725004417"}}
	resp := s.postForm("/payments", form)
	requireStatus(t, resp, http.StatusSeeOther)
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/payments/") {
		t.Fatalf("settlement redirect = %q, want the payment detail screen", loc)
	}
	_ = responseBody(t, resp)
	if got := requestStatusApp(t, s, reqID); got != "completed" {
		t.Fatalf("status = %q, want completed", got)
	}

	body := responseBody(t, s.request(http.MethodGet, loc, nil, ""))
	for _, want := range []string{
		`class="banner good"`, "The request is completed",
		`class="req-head"`, "from PR-2026-000001",
		`class="pill completed"`, `class="waiting done"`,
		`class="compare"`, `class="cmp-row match"`, "confirmed settled by Accounts",
		"1,00,000.00", "98,000.00", "2,000.00",
		`class="pill neutral no-dot"`, "Read-only",
		`<ol class="thread">`, `class="tl-dot`, "reserved it for processing", "recorded a payment",
		"Full trail, request to payment",
		`class="action-bar"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("payment detail missing %q:\n%s", want, body)
		}
	}
	// S12: no edit or void control on a linked payment, and the route refuses too.
	if strings.Contains(body, "/edit") || strings.Contains(body, "/void") {
		t.Fatalf("linked payment offers a mutation control:\n%s", body)
	}
	var payID int64
	if err := s.st.DB().QueryRow(`SELECT id FROM payments WHERE request_id=?`, reqID).Scan(&payID); err != nil {
		t.Fatal(err)
	}
	if resp := s.postForm(fmt.Sprintf("/payments/%d/void", payID), url.Values{"reason": {"nope"}}); resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("linked payment void accepted: %d", resp.StatusCode)
	}

	// Q4: the request page shows the outcome and links to the payment.
	reqBody := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""))
	if !strings.Contains(reqBody, `class="compare"`) || !strings.Contains(reqBody, "98,000.00") || !strings.Contains(reqBody, loc) {
		t.Fatalf("request outcome missing the comparison or the payment link:\n%s", reqBody)
	}
}

func TestDoubleConfirmLandsOnTheExistingPayment(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Double")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-25"}, "amount": {"5000.00"}, "vendor_payee": {"Acme"}, "settlement": {"settled"}, "reference_no": {"N1"}}
	first := s.postForm("/payments", form)
	requireStatus(t, first, http.StatusSeeOther)
	_ = responseBody(t, first)
	// The accountant taps Confirm twice. The second must not look like a failure.
	second := s.postForm("/payments", form)
	requireStatus(t, second, http.StatusSeeOther)
	if second.Header.Get("Location") != first.Header.Get("Location") {
		t.Fatalf("double confirm went to %q, want the existing payment %q", second.Header.Get("Location"), first.Header.Get("Location"))
	}
	_ = responseBody(t, second)
	var n int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM payments WHERE request_id=?`, reqID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("double confirm created %d payments (S9 broken)", n)
	}
}

func TestPartialSettlementRoutesToReview(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Partial")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-06-15"}, "amount": {"3000.00"}, "vendor_payee": {"Acme Landlord"}, "settlement": {"partial"}, "partial_reason": {"balance later"}, "reference_no": {"N2"}}
	requireStatus(t, s.postForm("/payments", form), http.StatusSeeOther)
	if got := requestStatusApp(t, s, reqID); got != "partial_review" {
		t.Fatalf("status = %q, want partial_review", got)
	}
	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""))
	if !strings.Contains(body, "balance later") || !strings.Contains(body, `class="pill partial"`) {
		t.Fatalf("partial outcome missing from the request:\n%s", body)
	}
}

// settleOneRequest drives the whole flow once — reserve, then confirm — and
// returns the request and the payment it produced. Half the assertions below
// are about what a settled payment must never offer, and none of them are about
// how it got settled.
func (s *appTestServer) settleOneRequest(seq int, headID, amount int64, paid, settlement, paidOn string) (int64, int64) {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
	reqID := s.seedApprovedRequest(seq, admin.ID, admin.ID, headID, amount)
	requireStatus(s.t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{
		"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)},
		"paid_on": {paidOn}, "amount": {paid}, "vendor_payee": {"Acme Landlord"},
		"settlement": {settlement}, "reference_no": {"N221260725004417"},
	}
	if settlement == "partial" {
		form.Set("partial_reason", "balance next month")
	}
	requireStatus(s.t, s.postForm("/payments", form), http.StatusSeeOther)
	var payID int64
	if err := s.st.DB().QueryRow(`SELECT id FROM payments WHERE request_id=?`, reqID).Scan(&payID); err != nil {
		s.t.Fatal(err)
	}
	return reqID, payID
}

// S12 again, from the other two directions the reviewer found open: the ledger
// row and the edit URL. The detail screen already hides both controls because a
// control the route would reject is a lie — but so is a form the store would
// reject, and so is an Edit button three columns from it.
func TestLinkedPaymentIsImmutableInTheLedgerAndOnItsEditRoute(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Immutable")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	_, linkedID := s.settleOneRequest(1, headID, 500000, "5000.00", "settled", "2026-07-25")

	// A payment recorded before this module has no request behind it and stays
	// editable (X6), so the ledger has to tell the two apart rather than lock
	// the whole screen.
	histID, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{HeadID: headID, PaidOn: "2026-07-10", Amount: 12345, VendorPayee: "Legacy Vendor", PaymentMode: "cash"})
	if err != nil {
		t.Fatal(err)
	}

	ledger := responseBody(t, s.request(http.MethodGet, "/payments?month=2026-07", nil, ""))
	for _, forbidden := range []string{fmt.Sprintf("/payments/%d/edit", linkedID), fmt.Sprintf("/payments/%d/void", linkedID)} {
		if strings.Contains(ledger, forbidden) {
			t.Fatalf("the ledger offers %q on a request-linked payment:\n%s", forbidden, ledger)
		}
	}
	if !strings.Contains(ledger, fmt.Sprintf("/payments/%d/edit", histID)) {
		t.Fatalf("the ledger stopped offering Edit on a historical payment:\n%s", ledger)
	}

	resp := s.request(http.MethodGet, fmt.Sprintf("/payments/%d/edit", linkedID), nil, "")
	requireStatus(t, resp, http.StatusSeeOther)
	if got, want := resp.Header.Get("Location"), fmt.Sprintf("/payments/%d", linkedID); got != want {
		t.Fatalf("edit form for a linked payment redirected to %q, want %q", got, want)
	}
	_ = responseBody(t, resp)
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/payments/%d/edit", histID), nil, ""), http.StatusOK)
}

// The seeded Accounts role is the only non-admin role designed to process a
// payment. If it cannot reserve, nothing in this phase is reachable without
// being an administrator.
func TestSeededAccountsRoleTakesARequestForProcessing(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("AccountsRole")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	clerk := s.seedColleague("clerk@example.test", "Priya Nair", "ClerkPassword123")
	s.assignRole(clerk.ID, "Accounts")

	s.login("clerk@example.test", "ClerkPassword123")
	resp := s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{})
	requireStatus(t, resp, http.StatusSeeOther)
	loc := resp.Header.Get("Location")
	_ = responseBody(t, resp)
	if !strings.HasPrefix(loc, "/payments/new") {
		t.Fatalf("Accounts reservation went to %q, want the payment entry screen", loc)
	}
	body := responseBody(t, s.request(http.MethodGet, loc, nil, ""))
	if !strings.Contains(body, "Record the payment") {
		t.Fatalf("Accounts cannot reach the entry screen:\n%s", body)
	}
}

// The queue gates its take button on reservation:reserve; the picker offered
// one to anybody who could open it, and the POST behind it answers 403.
func TestPickerOffersNoTakeControlWithoutTheReservationGrant(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("PickerGate")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.seedUserWithGrants("ledger@example.test", "LedgerPassword123", "Ledger only", []store.Grant{
		{Resource: "payment", Action: "view"}, {Resource: "payment", Action: "create"},
	})

	s.login("ledger@example.test", "LedgerPassword123")
	body := responseBody(t, s.request(http.MethodGet, "/payments/new", nil, ""))
	if strings.Contains(body, fmt.Sprintf(`action="/requests/%d/record-payment"`, reqID)) {
		t.Fatalf("the picker offers a take control the route answers 403 to:\n%s", body)
	}
	if !strings.Contains(body, `class="co is-taken"`) {
		t.Fatalf("the picker dropped the request instead of showing it read-only:\n%s", body)
	}
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusForbidden)
}

// Q4 read by the audience it is for. The requester holds no payment grant at
// all, so the outcome has to be on the request itself — and the link beside it
// must not be offered to somebody the payment route would turn away.
func TestRequesterReadsThePaymentOutcomeOnTheirOwnRequest(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Outcome")
	requester := s.seedRequester("ravi@example.test", "Ravi Kumar", "RequesterPass123")
	reqID := s.seedApprovedRequest(1, requester.ID, admin.ID, headID, 10000000)

	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-25"}, "amount": {"98000.00"}, "vendor_payee": {"Acme Landlord"}, "settlement": {"settled"}, "reference_no": {"N1"}}
	requireStatus(t, s.postForm("/payments", form), http.StatusSeeOther)

	s.login("ravi@example.test", "RequesterPass123")
	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""))
	for _, want := range []string{"Payment outcome", `class="compare"`, "98,000.00", "confirmed settled by Accounts"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the requester's own request is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `href="/payments/`) {
		t.Fatalf("the requester is offered a payment link the route answers 403 to:\n%s", body)
	}
}

// The payment screen is half a request screen. Reading it must therefore obey
// the same row scope /requests/{id} obeys, or payment:view becomes a way round
// it.
func TestPaymentDetailRefusesTheRequestBehindItOutOfScope(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Scope")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	_, linkedID := s.settleOneRequest(1, headID, 500000, "5000.00", "settled", "2026-07-25")
	histID, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{HeadID: headID, PaidOn: "2026-07-10", Amount: 12345, VendorPayee: "Legacy Vendor", PaymentMode: "cash"})
	if err != nil {
		t.Fatal(err)
	}
	s.seedUserWithGrants("auditor@example.test", "AuditorPassword123", "Ledger reader", []store.Grant{
		{Resource: "payment", Action: "view"},
	})

	s.login("auditor@example.test", "AuditorPassword123")
	resp := s.request(http.MethodGet, fmt.Sprintf("/payments/%d", linkedID), nil, "")
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
	// The request-less ledger row is untouched: this closes a way into request
	// data, not a way into the ledger.
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/payments/%d", histID), nil, ""), http.StatusOK)
}

// The trail's head already names the actor. Most request-side audit summaries
// are written to start with the same name, so the body repeated it.
func TestTrailBodyDropsTheActorNameTheHeadAlreadyCarries(t *testing.T) {
	for _, tc := range []struct{ name, actor, summary, want string }{
		{"request side", "Priya Nair", "Priya Nair approved request PR-2026-000001 for ₹1,00,000.00", "Approved request PR-2026-000001 for ₹1,00,000.00"},
		{"payment side", "Priya Nair", "Reserved request for processing", "Reserved request for processing"},
		{"no actor", "", "Recorded payment ₹98,000.00", "Recorded payment ₹98,000.00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := trailBody(store.AuditEntry{ActorName: tc.actor, Summary: tc.summary}); got != tc.want {
				t.Fatalf("trailBody = %q, want %q", got, tc.want)
			}
		})
	}
}

// Every other dot in a `.thread` is a monochrome text symbol the CSS tints with
// `color:`. An emoji ignores that and lands as a colour sticker.
func TestAuditGlyphsStayMonochrome(t *testing.T) {
	for _, action := range []string{"submit", "approve", "process", "hold", "record_payment", "release", "reject", "update", "attach", "attach_payment", "anything"} {
		if glyph := auditGlyph(action); strings.ContainsAny(glyph, "📎📄📌") {
			t.Fatalf("auditGlyph(%q) = %q, want a monochrome text glyph", action, glyph)
		}
	}
	if got := auditGlyph("attach"); got != "⇪" {
		t.Fatalf("auditGlyph(attach) = %q, want the thread's own upload glyph", got)
	}
}

// The head, the trail and the proof list, read against the mockup: one answer
// to "who is this waiting on" shared with every other screen, no name printed
// twice, and the request's own invoice sitting beside the bank advice.
func TestPaymentDetailHeadTrailAndProofReadLikeTheMockup(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Reads")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	if _, err := s.st.DB().Exec(`INSERT INTO request_attachments(request_id,original_name,stored_path,mime_type,size_bytes,uploaded_by) VALUES(?,?,?,?,?,?)`,
		reqID, "SE-26-27-1184.pdf", "req/SE.pdf", "application/pdf", 219136, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.DB().Exec(`INSERT INTO audit_log(actor_id,actor_name,action,entity_type,entity_id,summary) VALUES(?,?,?,?,?,?)`,
		admin.ID, admin.Name, "approve", "payment_request", reqID, admin.Name+" approved request PR-2026-000001 for ₹5,000.00"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-25"}, "amount": {"3000.00"}, "vendor_payee": {"Acme Landlord"}, "settlement": {"partial"}, "partial_reason": {"balance next month"}, "reference_no": {"N2"}}
	requireStatus(t, s.postForm("/payments", form), http.StatusSeeOther)
	var payID int64
	if err := s.st.DB().QueryRow(`SELECT id FROM payments WHERE request_id=?`, reqID).Scan(&payID); err != nil {
		t.Fatal(err)
	}

	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/payments/%d", payID), nil, ""))
	for _, want := range []string{
		// The waiting line is waitingOn's, so it says "you" to the manager it
		// is waiting on rather than reading them their own name.
		`class="pill partial"`, "Partial — manager review", `class="waiting you"`, "Waiting on you",
		// The trail's body no longer repeats the name in the head above it.
		`<div class="tl-body">Approved request PR-2026-000001`,
		// The mockup's second proof row: the request's own invoice.
		"SE-26-27-1184.pdf", "Invoice from the request",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("payment detail missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `<div class="tl-body">`+admin.Name+" approved") {
		t.Fatalf("the trail prints the actor's name twice:\n%s", body)
	}
}

// The manager's side of a partial settlement (S11, G14). Two decisions, each in
// its own .overlay > .sheet, and the accept lands in a terminal state that is
// visibly not a clean "completed".
func TestPartialReviewScreenAndManagerDecision(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Review")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 9500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-23"}, "amount": {"60000.00"}, "vendor_payee": {"Nova Print Works"}, "settlement": {"partial"}, "partial_reason": {"Vendor delivered 700 of the 1,000 copies."}, "reference_no": {"N9912"}}
	requireStatus(t, s.postForm("/payments", form), http.StatusSeeOther)

	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/partial-review", reqID), nil, ""))
	for _, want := range []string{
		`class="req-head"`, `class="pill partial"`, `class="waiting you"`,
		`class="compare"`, "Still owed to the vendor", "35,000.00",
		`class="banner warn"`, "Vendor delivered 700 of the 1,000 copies.",
		`class="pill neutral no-dot"`, "Cannot be edited",
		`<ol class="thread">`, `class="comment-box"`,
		`id="close-sheet"`, "Accept and close", "Completed — partial accepted",
		`id="concern-sheet"`, "Raise a concern", "This does not reverse anything",
		`class="overlay"`, `class="sheet"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("partial review missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<details") {
		t.Fatalf("partial review uses a <details> instead of a sheet:\n%s", body)
	}
	if strings.Count(body, `class="overlay"`) != 2 {
		t.Fatalf("expected two sheets (accept and concern):\n%s", body)
	}

	// Raising a concern keeps it open and records the comment.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/raise-concern", reqID), url.Values{"comment": {"Confirm the balance timeline"}}), http.StatusSeeOther)
	if got := requestStatusApp(t, s, reqID); got != "partial_review" {
		t.Fatalf("status after concern = %q, want partial_review", got)
	}
	// Accepting closes it into the distinct terminal state (G14).
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/accept-partial", reqID), url.Values{"note": {"Balance invoiced separately."}}), http.StatusSeeOther)
	if got := requestStatusApp(t, s, reqID); got != "completed_partial" {
		t.Fatalf("status after accept = %q, want completed_partial (G14)", got)
	}
	after := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""))
	if !strings.Contains(after, `class="pill completed-partial"`) || !strings.Contains(after, "Completed — partial accepted") {
		t.Fatalf("accepted request does not render .pill.completed-partial:\n%s", after)
	}

	// Once the decision is made there is nothing left to decide, and the screen
	// says so by not existing: it would otherwise offer "Accept ₹60,000 and
	// close?" against a request that closed, over a balance of nothing.
	gone := s.request(http.MethodGet, fmt.Sprintf("/requests/%d/partial-review", reqID), nil, "")
	requireStatus(t, gone, http.StatusSeeOther)
	if loc := gone.Header.Get("Location"); loc != fmt.Sprintf("/requests/%d", reqID) {
		t.Fatalf("partial review of a closed request redirected to %q", loc)
	}
	_ = responseBody(t, gone)
}

// seedPartialReview takes one request all the way to partial_review through the
// product, so every test below starts where the manager starts. The caller must
// already be signed in as somebody who can reserve and settle.
func (s *appTestServer) seedPartialReview(reqID, headID int64, reason string) {
	s.t.Helper()
	requireStatus(s.t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)},
		"paid_on": {"2026-07-23"}, "amount": {"60000.00"}, "vendor_payee": {"Nova Print Works"},
		"settlement": {"partial"}, "partial_reason": {reason}, "reference_no": {"N9912"}}
	requireStatus(s.t, s.postForm("/payments", form), http.StatusSeeOther)
}

// TestPartialReviewDecisionsBelongToTheRequestsOwnManager is the ownership
// proof. approval:accept_partial says a person may accept a shortfall; it never
// says whose. Every other manager verb in the system — ApproveRequest,
// ReturnRequest, DecideCancellation — refuses an actor who is not the request's
// own manager, and writing off money permanently is not the exception.
func TestPartialReviewDecisionsBelongToTheRequestsOwnManager(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Owner")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 9500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	s.seedPartialReview(reqID, headID, "Vendor delivered 700 of the 1,000 copies.")

	// A second person holding every verb and seeing every request, who simply
	// is not this request's manager.
	s.seedColleague("other-manager@example.test", "Other Manager", "OtherManagerPass1")
	s.login("other-manager@example.test", "OtherManagerPass1")

	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/partial-review", reqID), nil, ""))
	if strings.Contains(body, `class="overlay"`) || strings.Contains(body, `data-open="close-sheet"`) {
		t.Fatalf("a decision that is not theirs was offered:\n%s", body)
	}
	if !strings.Contains(body, admin.Name+" decides.") {
		t.Fatalf("the onlooker's action bar does not name the decider:\n%s", body)
	}
	// The server refuses it too, not only the template.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/raise-concern", reqID),
		url.Values{"comment": {"Not mine to judge"}}), http.StatusForbidden)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/accept-partial", reqID),
		url.Values{"note": {"Closing someone else's request"}}), http.StatusForbidden)
	if got := requestStatusApp(t, s, reqID); got != "partial_review" {
		t.Fatalf("status after an outsider's decision = %q, want partial_review", got)
	}
}

// TestPartialReviewThreadIsOneChronologicalStream covers the two ways the trail
// can lie: printing the story out of order, and printing the same sentence
// twice because raising a concern writes both a comment and an audit row.
func TestPartialReviewThreadIsOneChronologicalStream(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Stream")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 9500000)
	// A comment written long before the payment. Its own timestamp is the only
	// thing that may decide where in the story it belongs.
	if _, err := s.st.DB().Exec(`INSERT INTO request_comments(request_id,author_id,body,created_at) VALUES(?,?,?,?)`,
		reqID, admin.ID, "EARLYWORD the invoice is already with the vendor", "2026-07-01 09:00:00"); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	s.seedPartialReview(reqID, headID, "Vendor delivered 700 of the 1,000 copies.")

	path := fmt.Sprintf("/requests/%d/partial-review", reqID)
	body := responseBody(t, s.request(http.MethodGet, path, nil, ""))
	early, paid := strings.Index(body, "EARLYWORD"), strings.Index(body, "recorded a payment")
	if early < 0 || paid < 0 {
		t.Fatalf("the thread is missing the comment or the payment event:\n%s", body)
	}
	if early > paid {
		t.Fatalf("a comment written before the payment renders after it:\n%s", body)
	}

	// Raising a concern writes a comment and an audit row carrying the same
	// words. The screen the concern lands on prints it once.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/raise-concern", reqID),
		url.Values{"comment": {"CONCERNWORD confirm the balance timeline"}}), http.StatusSeeOther)
	after := responseBody(t, s.request(http.MethodGet, path, nil, ""))
	if n := strings.Count(after, "CONCERNWORD"); n != 1 {
		t.Fatalf("the concern is printed %d times, want 1:\n%s", n, after)
	}

	// Replying mid-decision returns to the decision, rather than ejecting the
	// manager to the request they were reading it from.
	reply := s.postForm(fmt.Sprintf("/requests/%d/comment", reqID),
		url.Values{"body": {"Noted, thank you."}, "return_to": {"partial-review"}})
	requireStatus(t, reply, http.StatusSeeOther)
	if loc := reply.Header.Get("Location"); loc != path {
		t.Fatalf("reply from the partial review redirected to %q, want %q", loc, path)
	}
	_ = responseBody(t, reply)
}

// ---------------------------------------------------------------------------
// Task 18 — the release / reassign screen (S6, S7, G11, G12)
// ---------------------------------------------------------------------------

func TestReservationScreenReleaseAndReassign(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Release")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 10000000)
	hash, _ := auth.HashPassword("DeepakPass1234")
	deepakID, err := s.st.CreateUser(s.ctx, "deepak@example.test", "Deepak Menon", hash, "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/reservation", reqID), nil, ""))
	for _, want := range []string{
		`class="reserve-bar"`, "Reserved by you",
		`class="banner bad"`, "Confirm no payment has been started",
		`class="choice"`, `value="release"`, `value="reassign"`,
		`data-when="action:reassign"`, "Deepak Menon",
		`name="reason"`, `class="checkline"`,
		`class="action-bar"`, `<ol class="thread">`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("reservation screen missing %q:\n%s", want, body)
		}
	}

	// G12: release without a reason is refused and the reservation survives.
	resp := s.postForm(fmt.Sprintf("/requests/%d/release", reqID), url.Values{"action": {"release"}, "confirm": {"on"}})
	if resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("release without a reason accepted: %d", resp.StatusCode)
	}
	_ = responseBody(t, resp)
	// S7: confirmation is still required.
	resp = s.postForm(fmt.Sprintf("/requests/%d/release", reqID), url.Values{"action": {"release"}, "reason": {"Bank details unconfirmed"}})
	if resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("release without confirmation accepted: %d", resp.StatusCode)
	}
	_ = responseBody(t, resp)
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status after refused releases = %q, want processing", got)
	}

	// G11: reassign keeps it in processing and moves the holder.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/reassign", reqID), url.Values{
		"action": {"reassign"}, "to_user_id": {strconvFormat(deepakID)}, "reason": {"Going on leave"}, "confirm": {"on"},
	}), http.StatusSeeOther)
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status after reassign = %q, want processing", got)
	}
	var holder int64
	if err := s.st.DB().QueryRow(`SELECT processing_by FROM payment_requests WHERE id=?`, reqID).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if holder != deepakID {
		t.Fatalf("processing_by = %d, want %d", holder, deepakID)
	}
}

// The reassign target is revealed by data-when; hidden is not validation.
func TestReassignRefusesWithoutATargetServerSide(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("ReassignGuard")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	resp := s.postForm(fmt.Sprintf("/requests/%d/reassign", reqID), url.Values{"action": {"reassign"}, "reason": {"someone else"}, "confirm": {"on"}})
	if resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("reassign with no target accepted: %d", resp.StatusCode)
	}
	_ = responseBody(t, resp)
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status = %q, want processing", got)
	}
}

// S6 — the plan's tests prove every way a release is refused; this proves the
// one way it is granted. A release that carries a reason and the confirmation
// puts the request back in the open queue, drops the holder, and lands the
// accountant on the queue rather than on a request they no longer hold.
func TestReleaseReturnsTheRequestToTheQueue(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Released")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 4200000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	resp := s.postForm(fmt.Sprintf("/requests/%d/release", reqID), url.Values{
		"action": {"release"}, "reason": {"Vendor bank details need confirming"}, "confirm": {"on"},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != "/accounts-queue" {
		t.Fatalf("release redirect = %q, want /accounts-queue", loc)
	}
	_ = responseBody(t, resp)
	if got := requestStatusApp(t, s, reqID); got != "approved" {
		t.Fatalf("status after release = %q, want approved", got)
	}
	var holder *int64
	if err := s.st.DB().QueryRow(`SELECT processing_by FROM payment_requests WHERE id=?`, reqID).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if holder != nil {
		t.Fatalf("processing_by = %d after release, want NULL", *holder)
	}
	// The reason is in the history, so the requester and the approver can read
	// why their money stopped moving.
	if got := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, "")); !strings.Contains(got, "Vendor bank details need confirming") {
		t.Fatalf("the release reason is not in the request history:\n%s", got)
	}
	// And the screen is gone with the reservation: there is nothing to release.
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/reservation", reqID), nil, ""), http.StatusConflict)
}

// seedProbeUser creates an active user whose single role carries exactly the
// grants and scopes given. The seeded roles are bundles; proving that one cell
// of the permission matrix is enough — or not enough — needs a user holding
// that cell and nothing next to it.
func (s *appTestServer) seedProbeUser(email, name, password, roleName string, grants []store.Grant, scopes []store.ScopeGrant) store.User {
	s.t.Helper()
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		s.t.Fatal(err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.t.Fatal(err)
	}
	uid, err := s.st.CreateUser(s.ctx, email, name, hash, "data_entry", true)
	if err != nil {
		s.t.Fatal(err)
	}
	roleID, err := s.st.CreateRole(s.ctx, admin, roleName, "")
	if err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.UpdateRolePermissions(s.ctx, admin, roleID, grants, scopes); err != nil {
		s.t.Fatal(err)
	}
	if err := s.st.SetUserRoles(s.ctx, admin, uid, []int64{roleID}); err != nil {
		s.t.Fatal(err)
	}
	u, err := s.st.UserByID(s.ctx, uid)
	if err != nil {
		s.t.Fatal(err)
	}
	return u
}

func reservationHolder(t *testing.T, s *appTestServer, reqID int64) int64 {
	t.Helper()
	var holder int64
	if err := s.st.DB().QueryRow(`SELECT COALESCE(processing_by,0) FROM payment_requests WHERE id=?`, reqID).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	return holder
}

// The accounts queue and the reservation-conflict screen both link here behind
// reservation:reassign alone, and reassign and release are two independent cells
// of the permission matrix. A route gated on release answered 403 to exactly the
// reader those two links were written for.
func TestReservationScreenOpensForAReassignOnlyRole(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("ReassignOnly")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 900000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	s.seedProbeUser("reassign-only@example.test", "Reassign Only", "ReassignPass1234", "Reassign only",
		[]store.Grant{
			{Resource: "request", Action: "view"},
			{Resource: "payment", Action: "view"},
			{Resource: "payment", Action: "process"},
			{Resource: "reservation", Action: "reassign"},
		},
		[]store.ScopeGrant{{Resource: "request", Scope: "all"}})
	s.login("reassign-only@example.test", "ReassignPass1234")

	link := fmt.Sprintf("/requests/%d/reservation", reqID)
	queue := responseBody(t, s.request(http.MethodGet, "/accounts-queue?tab=processing", nil, ""))
	if !strings.Contains(queue, link) {
		t.Fatalf("the queue does not offer the reassign link this test is about:\n%s", queue)
	}
	resp := s.request(http.MethodGet, link, nil, "")
	requireStatus(t, resp, http.StatusOK)
	body := responseBody(t, resp)

	// Letting them in must not offer them the other half of the screen: every
	// release control would answer 403, and the form's own action must be the
	// verb they hold, so pressing Enter posts something they may actually do.
	if !strings.Contains(body, `value="reassign"`) {
		t.Fatalf("the reassign branch is missing from the screen:\n%s", body)
	}
	for _, leak := range []string{`value="release"`, ">Release reservation</button>", fmt.Sprintf(`action="/requests/%d/release"`, reqID)} {
		if strings.Contains(body, leak) {
			t.Fatalf("a reader who cannot release is offered %q:\n%s", leak, body)
		}
	}
}

// Holding a reservation verb somewhere is not permission to read this request
// (Q5/R6). The screen and both of its POSTs answer to the same data scope the
// request's own detail page does.
func TestReservationScreenRefusesARequestTheCallerMayNotRead(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Scoped")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 700000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	deepak := s.seedColleague("deepak@example.test", "Deepak Menon", "DeepakPass1234")

	s.seedProbeUser("outsider@example.test", "Outsider Rao", "OutsiderPass1234", "Reservation outsider",
		[]store.Grant{
			{Resource: "request", Action: "view"},
			{Resource: "payment", Action: "process"},
			{Resource: "reservation", Action: "release"},
			{Resource: "reservation", Action: "reassign"},
		},
		[]store.ScopeGrant{{Resource: "request", Scope: "own"}})
	s.login("outsider@example.test", "OutsiderPass1234")

	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""), http.StatusForbidden)
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/reservation", reqID), nil, ""), http.StatusForbidden)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/reassign", reqID), url.Values{
		"action": {"reassign"}, "to_user_id": {strconvFormat(deepak.ID)}, "reason": {"I want it"}, "confirm": {"on"},
	}), http.StatusForbidden)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/release", reqID), url.Values{
		"action": {"release"}, "reason": {"Not mine to hold"}, "confirm": {"on"},
	}), http.StatusForbidden)

	if got := reservationHolder(t, s, reqID); got != admin.ID {
		t.Fatalf("processing_by = %d after four refusals, want %d", got, admin.ID)
	}
}

// A reservation handed to somebody who cannot open the Accounts queue is a
// request nobody can move: the new holder gets 403 on the queue and on this very
// screen, and anybody else re-reserving gets 409.
func TestReassignOffersOnlyPeopleWhoCanWorkTheAccountsQueue(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Candidates")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 600000)
	bystander := s.seedRequester("bystander@example.test", "Bystander Bhat", "BystanderPass1234")
	colleague := s.seedColleague("deepak@example.test", "Deepak Menon", "DeepakPass1234")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/reservation", reqID), nil, ""))
	if !strings.Contains(body, colleague.Name) {
		t.Fatalf("the reassign select does not offer anyone who can work the queue:\n%s", body)
	}
	if strings.Contains(body, bystander.Name) {
		t.Fatalf("the reassign select offers %q, who cannot open the Accounts queue:\n%s", bystander.Name, body)
	}

	resp := s.postForm(fmt.Sprintf("/requests/%d/reassign", reqID), url.Values{
		"action": {"reassign"}, "to_user_id": {strconvFormat(bystander.ID)}, "reason": {"Passing it on"}, "confirm": {"on"},
	})
	if resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("reassign to somebody who cannot work the queue accepted: %d", resp.StatusCode)
	}
	_ = responseBody(t, resp)
	if got := reservationHolder(t, s, reqID); got != admin.ID {
		t.Fatalf("processing_by = %d after a refused reassign, want %d", got, admin.ID)
	}
}

// HTML makes the first submit button in tree order the form's default button,
// and hidden — unlike disabled — does not exempt it. With Reassign first,
// pressing Enter on the checked "Release it" radio posted to /reassign with no
// target and lost the typed reason on a 400.
func TestReleaseIsTheReservationFormsDefaultButton(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("DefaultButton")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 300000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/reservation", reqID), nil, ""))
	release := strings.Index(body, fmt.Sprintf(`formaction="/requests/%d/release"`, reqID))
	reassign := strings.Index(body, fmt.Sprintf(`formaction="/requests/%d/reassign"`, reqID))
	if release < 0 || reassign < 0 {
		t.Fatalf("both submit buttons must carry a formaction:\n%s", body)
	}
	if release > reassign {
		t.Fatalf("the reassign button precedes the release button, making it the form's default")
	}
}

// The reserve bar localises its clock and the trail underneath it did not, so
// one screen printed two times five and a half hours apart for one event.
func TestTrailTimestampsAgreeWithTheReserveBar(t *testing.T) {
	when := time.Date(2026, 7, 26, 16, 44, 0, 0, time.UTC)
	if got, want := dateText(when), when.Local().Format("2006-01-02 15:04"); got != want {
		t.Fatalf("dateText = %q, want %q", got, want)
	}
	if !strings.HasSuffix(dateText(when), hhmm(&when)) {
		t.Fatalf("the trail says %q while the reserve bar says %q", dateText(when), hhmm(&when))
	}
	if got := datepText(&when); got != dateText(when) {
		t.Fatalf("datepText = %q, dateText = %q: one screen, two clocks", got, dateText(when))
	}
	if got := datepText(nil); got != "" {
		t.Fatalf("datepText(nil) = %q, want the empty string", got)
	}
}

// Approval reassignment (swapping the approver of a pending request) and
// reservation reassignment (moving who is paying it) wrote the same audit action
// on the same entity, so an approver swap would have read as "X reassigned the
// reservation" in this screen's history.
func TestApprovalReassignmentIsNotAReservationEvent(t *testing.T) {
	kept := reservationTrail([]store.AuditEntry{
		{Action: "process"}, {Action: "approve"}, {Action: "approval_reassign"},
		{Action: "release"}, {Action: "reassign"},
	})
	var actions []string
	for _, e := range kept {
		actions = append(actions, e.Action)
	}
	if got, want := strings.Join(actions, ","), "process,release,reassign"; got != want {
		t.Fatalf("reservationTrail kept %q, want %q", got, want)
	}
	if got, want := auditPhrase("approval_reassign"), "reassigned the approval"; got != want {
		t.Fatalf("auditPhrase(approval_reassign) = %q, want %q", got, want)
	}
	if auditPhrase("approval_reassign") == auditPhrase("reassign") {
		t.Fatalf("both reassignments read %q; the trail cannot tell them apart", auditPhrase("reassign"))
	}
}

// ---------------------------------------------------------------------------
// Task 19 — hold, unhold and the stale reservation screen (L7, Q6)
// ---------------------------------------------------------------------------

// The plan wrote these two against "/requests/to-pay". That route was never
// built: the accounts queue shipped as GET /accounts-queue with ?tab=, so the
// queue reads below are the same two assertions against the route that exists.
func TestHoldAndUnholdMoveTheRequestThroughTheQueue(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Hold")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 2500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/accounts-queue", nil, ""))
	if !strings.Contains(body, "PR-2026-000001") {
		t.Fatalf("approved request missing from the queue:\n%s", body)
	}
	// Hold takes it out of the takeable tab (L7) and onto the hold tab.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", reqID), url.Values{"reason": {"Which site is this for?"}}), http.StatusSeeOther)
	body = responseBody(t, s.request(http.MethodGet, "/accounts-queue", nil, ""))
	if strings.Contains(body, "Take for processing") {
		t.Fatalf("held request is still takeable:\n%s", body)
	}
	held := responseBody(t, s.request(http.MethodGet, "/accounts-queue?tab=hold", nil, ""))
	if !strings.Contains(held, "PR-2026-000001") {
		t.Fatalf("held request missing from the hold tab:\n%s", held)
	}
	// The request screen explains the hold and offers the Accounts-only release.
	detail := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""))
	for _, want := range []string{`class="pill hold"`, `class="banner warn"`, "Which site is this for?", "Release hold", `class="comment-box"`} {
		if !strings.Contains(detail, want) {
			t.Fatalf("on-hold request detail missing %q:\n%s", want, detail)
		}
	}
	// Unhold restores it.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/unhold", reqID), url.Values{}), http.StatusSeeOther)
	body = responseBody(t, s.request(http.MethodGet, "/accounts-queue", nil, ""))
	if !strings.Contains(body, "Take for processing") {
		t.Fatalf("unheld request is not takeable again:\n%s", body)
	}
}

func TestStaleReservationScreenOffersTheFourChoices(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Stale")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 7800000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET processing_at=datetime('now','-26 hours') WHERE id=?`, reqID); err != nil {
		t.Fatal(err)
	}
	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/reservation/stale", reqID), nil, ""))
	for _, want := range []string{
		`class="banner warn"`, "reserved by you for", "26 h",
		`class="req-head"`, `class="pill processing"`, `class="waiting"`,
		`class="a-list"`, "Carry on and record the payment", "Release it", "Hand it to a colleague", "Put it on hold",
		"Who has been told", `<ol class="thread">`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stale reservation screen missing %q:\n%s", want, body)
		}
	}
}

// A hold describes an approved request that Accounts has paused. Every writer
// that moves a request off 'approved' has to clear it, or the request dies with
// on_hold still set: the queue's hold tab goes on listing a cancelled request
// and offering a reply that can change nothing, and its own detail screen goes
// on calling it "On hold, waiting on the requester".
func TestCancellingAHeldRequestClearsTheHold(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("HoldCancel")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 2500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", reqID), url.Values{"reason": {"Which site is this for?"}}), http.StatusSeeOther)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/cancel", reqID), url.Values{"reason": {"Site dropped"}}), http.StatusSeeOther)

	if requestStatusApp(t, s, reqID) != "cancelled" {
		t.Fatalf("status = %q, want cancelled", requestStatusApp(t, s, reqID))
	}
	var onHold int
	if err := s.st.DB().QueryRow(`SELECT on_hold FROM payment_requests WHERE id=?`, reqID).Scan(&onHold); err != nil {
		t.Fatal(err)
	}
	if onHold != 0 {
		t.Fatalf("cancelled request still carries on_hold=%d", onHold)
	}
	held := responseBody(t, s.request(http.MethodGet, "/accounts-queue?tab=hold", nil, ""))
	if strings.Contains(held, "PR-2026-000001") {
		t.Fatalf("cancelled request is still in the hold tab:\n%s", held)
	}
	detail := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""))
	for _, unwanted := range []string{`class="pill hold"`, "Release hold", "This request is on hold"} {
		if strings.Contains(detail, unwanted) {
			t.Fatalf("cancelled request still shows %q:\n%s", unwanted, detail)
		}
	}
	if !strings.Contains(detail, "Cancelled") {
		t.Fatalf("cancelled request does not say so:\n%s", detail)
	}
}

// The same rule on the other exit from 'approved'. A hold that survived into
// cancellation_requested would answer before the status did, and the manager
// who owes the decision would read "Waiting on the requester to clarify" on the
// one screen that is supposed to tell them it is theirs.
func TestAskingForCancellationOnAHeldRequestClearsTheHold(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("HoldAsk")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 2500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", reqID), url.Values{"reason": {"Which site is this for?"}}), http.StatusSeeOther)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/cancel-request", reqID), url.Values{"reason": {"No longer needed"}}), http.StatusSeeOther)

	var onHold int
	if err := s.st.DB().QueryRow(`SELECT on_hold FROM payment_requests WHERE id=?`, reqID).Scan(&onHold); err != nil {
		t.Fatal(err)
	}
	if onHold != 0 {
		t.Fatalf("cancellation_requested request still carries on_hold=%d", onHold)
	}
	detail := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""))
	if strings.Contains(detail, `class="pill hold"`) {
		t.Fatalf("pill still says On hold while a cancellation is pending:\n%s", detail)
	}
	for _, want := range []string{"Cancellation requested", `class="waiting you"`} {
		if !strings.Contains(detail, want) {
			t.Fatalf("pending cancellation missing %q:\n%s", want, detail)
		}
	}
}

// The on-hold state carries exactly one banner in
// mockups/screens/request-on-hold.html. The approved lock stacked above it told
// the requester that "payment freezes while your approver decides" when the
// approver is deciding nothing — Accounts is holding it.
func TestTheHoldBannerIsTheOnlyBannerOnAHeldRequest(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("HoldBanner")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 2500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	detail := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""))
	if !strings.Contains(detail, "Approved requests are locked") {
		t.Fatalf("approved request is missing its lock banner:\n%s", detail)
	}
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", reqID), url.Values{"reason": {"Which site is this for?"}}), http.StatusSeeOther)

	detail = responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""))
	if strings.Contains(detail, "Approved requests are locked") {
		t.Fatalf("the approved lock stacks above the hold banner:\n%s", detail)
	}
	if !strings.Contains(detail, "This request is on hold") {
		t.Fatalf("hold banner missing:\n%s", detail)
	}
	// The hold is an event in the same stream, and threadDot/threadGlyph are
	// what colour it: request-on-hold.html renders it as a warn ⏸, not the
	// untinted grey dot every unknown action falls through to.
	if !strings.Contains(detail, `<span class="tl-dot warn" aria-hidden="true">⏸</span>`) {
		t.Fatalf("the hold line in the thread is undecorated:\n%s", detail)
	}
}

// Q6 is only a screen if something links to it. The queue is the one place that
// knows which reservation has crossed the one-day mark, so the mockup's own
// Resume on that row goes to the nudge, not back to the payment form.
func TestTheQueueLinksAStaleReservationToTheNudgeScreen(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("StaleLink")
	fresh := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 4400000)
	old := s.seedApprovedRequest(2, admin.ID, admin.ID, headID, 7800000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", fresh), url.Values{}), http.StatusSeeOther)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", old), url.Values{}), http.StatusSeeOther)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET processing_at=datetime('now','-26 hours') WHERE id=?`, old); err != nil {
		t.Fatal(err)
	}

	body := responseBody(t, s.request(http.MethodGet, "/accounts-queue?tab=processing", nil, ""))
	if !strings.Contains(body, fmt.Sprintf("/requests/%d/reservation/stale", old)) {
		t.Fatalf("the stale row does not link to the nudge screen:\n%s", body)
	}
	// The reservation taken a moment ago is not stale, and resuming it must
	// still open the payment form.
	if strings.Contains(body, fmt.Sprintf("/requests/%d/reservation/stale", fresh)) {
		t.Fatalf("a fresh reservation is being called stale:\n%s", body)
	}
	if !strings.Contains(body, fmt.Sprintf("/payments/new?request=%d", fresh)) {
		t.Fatalf("the fresh row lost its Resume link:\n%s", body)
	}
}

// The screen is one .a-list of exactly the four choices
// mockups/screens/accounts-stale-processing.html names, and every link on it
// has to land somewhere the reader can act. "Put it on hold" cannot go to the
// request: the hold control there is gated on 'approved', and this screen only
// exists while the request is reserved.
func TestStaleScreenOffersFourChoicesThatAllLandSomewhereUsable(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("StaleFour")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 7800000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET processing_at=datetime('now','-26 hours') WHERE id=?`, reqID); err != nil {
		t.Fatal(err)
	}
	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/reservation/stale", reqID), nil, ""))

	list := body[strings.Index(body, `class="a-list"`):]
	list = list[:strings.Index(list, "</div>")]
	if n := strings.Count(list, "<a href="); n != 4 {
		t.Fatalf("the a-list offers %d choices, want the mockup's four:\n%s", n, list)
	}
	if strings.Contains(body, "Leave it and go back to the queue") {
		t.Fatalf("a fifth choice the mockup does not have:\n%s", list)
	}
	// Every href on the screen has to answer for a signed-in accountant.
	for _, href := range []string{
		fmt.Sprintf("/payments/new?request=%d", reqID),
		fmt.Sprintf("/requests/%d/reservation", reqID),
	} {
		if !strings.Contains(list, `href="`+href+`"`) {
			t.Fatalf("choice %q missing:\n%s", href, list)
		}
		resp := s.request(http.MethodGet, href, nil, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200: a choice on the nudge screen is a dead end", href, resp.StatusCode)
		}
		responseBody(t, resp)
	}
	// The bare request is not one of them: at 'processing' it has no hold
	// control and no release control either.
	if strings.Contains(list, fmt.Sprintf(`href="/requests/%d"`, reqID)) {
		t.Fatalf("a choice still points at the request itself:\n%s", list)
	}
}

// A vendor-invoice request names its payee with vendor_id; the vendor_payee
// snapshot column is empty. store.Request resolves the display payee into
// Request.Vendor, and every screen in the settlement flow must read THAT — not
// the raw snapshot — or the accountant is asked to pay a blank, the payment row
// is written with no payee at all, and the payment detail renders an empty h1.
func TestTheVendorNameSurvivesTheWholeSettlementFlow(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Payee")
	const vendorName = "Sundaram Electricals Pvt Ltd"
	reqID, _ := s.seedVendorRequest(1, admin.ID, admin.ID, headID, 500000, vendorName)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	// The queue names the vendor, and searching for it finds the row.
	queue := responseBody(t, s.request(http.MethodGet, "/accounts-queue", nil, ""))
	if !strings.Contains(queue, vendorName) {
		t.Fatalf("the queue does not name the vendor:\n%s", queue)
	}
	found := responseBody(t, s.request(http.MethodGet, "/accounts-queue?q=Sundaram", nil, ""))
	if !strings.Contains(found, "PR-2026-000001") {
		t.Fatalf("searching the queue by vendor name found nothing:\n%s", found)
	}

	// The entry screen names it, and so does the confirmation sheet.
	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	entry := responseBody(t, s.request(http.MethodGet, strconvPath("/payments/new?request=%d", reqID), nil, ""))
	if !strings.Contains(entry, vendorName) {
		t.Fatalf("the payment entry screen does not name the vendor:\n%s", entry)
	}
	form := url.Values{"amount": {"5000.00"}, "paid_on": {"2026-07-25"}, "payment_mode": {"bank_transfer"}, "reference_no": {"N1"}}
	sheet := responseBody(t, s.postFormHX(strconvPath("/requests/%d/settlement-preview", reqID), form))
	if !strings.Contains(sheet, vendorName) {
		t.Fatalf("the settlement sheet does not name the vendor:\n%s", sheet)
	}

	// Settling carries the payee onto the payment row itself.
	settle := url.Values{
		"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)},
		"paid_on": {"2026-07-25"}, "amount": {"5000.00"}, "payment_mode": {"bank_transfer"},
		"reference_no": {"N1"}, "vendor_payee": {vendorName}, "settlement": {"settled"},
	}
	resp := s.postForm("/payments", settle)
	requireStatus(t, resp, http.StatusSeeOther)
	loc := resp.Header.Get("Location")
	var storedPayee string
	if err := s.st.DB().QueryRow(`SELECT COALESCE(vendor_payee,'') FROM payments WHERE request_id=?`, reqID).Scan(&storedPayee); err != nil {
		t.Fatal(err)
	}
	if storedPayee != vendorName {
		t.Fatalf("payments.vendor_payee = %q, want %q — the payment was written with no payee", storedPayee, vendorName)
	}
	detail := responseBody(t, s.request(http.MethodGet, loc, nil, ""))
	if !strings.Contains(detail, "<h1>"+vendorName+"</h1>") {
		t.Fatalf("the payment detail h1 does not name the vendor:\n%s", detail)
	}
}
