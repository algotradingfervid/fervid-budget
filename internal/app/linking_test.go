package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

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
