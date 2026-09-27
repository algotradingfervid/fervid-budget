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

// Payment linking, reservation and settlement, at the HTTP layer.
//
// Every test here drives the product the way an accountant does: take a request
// out of the queue, record what left the bank, say whether that settles it. The
// store already refuses the illegal moves; these prove the screens never offer
// them, and that the one endpoint which must not write, does not write.

// ---------------------------------------------------------------------------
// Reservation entry point, permission verbs, conflict screen
// ---------------------------------------------------------------------------

func TestReservationEntryPointReservesAndRedirects(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Reserve")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.loginAdmin()

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

// Losing a reservation race shows a conflict screen that names the winner and
// says nothing was saved, not the generic error page.
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
	mustContain(t, "conflict screen", body,
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
	)
	// The reservation is untouched by the loser.
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status = %q, want processing", got)
	}
}

// ---------------------------------------------------------------------------
// The accounts queue
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
	s.loginAdmin()

	body := s.get("/accounts-queue")
	mustContain(t, "queue", body,
		`class="metric-strip"`,
		"Approved, unclaimed",
		"Reserved by you",
		"Reserved by others",
		"On hold",
		`class="segmented"`,
		"Approved, unclaimed <span class=\"n\">1</span>",
		"Processing <span class=\"n\">1</span>",
		"On hold <span class=\"n\">1</span>",
		"Partial review <span class=\"n\">0</span>",
		"Paid <span class=\"n\">0</span>",
		`class="m-filters"`,
		`<table class="t-cards">`,
		`data-label="Payee"`,
		"Take for processing",
		"PR-2026-000001",
	)
	// A reserved-by-you row offers Resume, never a second Take.
	if strings.Count(body, "Take for processing") != 1 {
		t.Fatalf("Take offered for a non-takeable row:\n%s", body)
	}
	// Design system, not the legacy markup.
	if strings.Contains(body, `class="badge`) {
		t.Fatalf("queue still renders .badge (D5):\n%s", body)
	}

	// The Processing tab shows the reserved row and hides the open one.
	proc := s.get("/accounts-queue?tab=processing")
	if !strings.Contains(proc, "PR-2026-000002") || strings.Contains(proc, "PR-2026-000001") {
		t.Fatalf("processing tab wrong:\n%s", proc)
	}
	if !strings.Contains(proc, "Resume") {
		t.Fatalf("processing tab has no Resume action:\n%s", proc)
	}
	// The hold tab shows the held row, which is never takeable.
	hold := s.get("/accounts-queue?tab=hold")
	if !strings.Contains(hold, "PR-2026-000003") || strings.Contains(hold, "Take for processing") {
		t.Fatalf("hold tab wrong:\n%s", hold)
	}
	// An unknown tab is rejected, not silently treated as Approved.
	resp := s.request(http.MethodGet, "/accounts-queue?tab=nonsense", nil, "")
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)
	_ = open
}

// Once approved, an urgent request is Accounts' to pay: the queue, the Accounts
// area of the dashboard and the payment picker mark it urgent and list it ahead
// of older ordinary requests.
func TestAccountsQueueMarksAndSurfacesUrgentRequests(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("UrgentQueue")
	// Raised by someone else, so the only place the accountant's dashboard can
	// show these rows is its Accounts area.
	requester := s.seedRequester("urq@example.test", "Urgent Requester", "UrgentPass1234")
	s.seedColleague("uracct@example.test", "Urgent Accountant", "UrgentAcct1234")
	normal := s.seedApprovedRequest(1, requester.ID, admin.ID, headID, 500000)
	urgent := s.seedApprovedRequest(2, requester.ID, admin.ID, headID, 700000)
	if _, err := s.st.DB().ExecContext(s.ctx, `UPDATE payment_requests SET approved_at=datetime('now','-2 days') WHERE id=?`, normal); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.DB().ExecContext(s.ctx, `UPDATE payment_requests SET urgent=1 WHERE id=?`, urgent); err != nil {
		t.Fatal(err)
	}
	s.login("uracct@example.test", "UrgentAcct1234")

	// The queue, the Accounts area of the dashboard, and the payment form's
	// request picker are the three places Accounts chooses what to pay next.
	for _, path := range []string{"/accounts-queue", "/", "/payments/new"} {
		body := s.get(path)
		u, n := strings.Index(body, "PR-2026-000002"), strings.Index(body, "PR-2026-000001")
		if u < 0 || n < 0 {
			t.Fatalf("%s is missing a request row", path)
		}
		if u > n {
			t.Errorf("%s lists the urgent request after an older ordinary one", path)
		}
		if strings.Count(body, `<span class="pill urgent">Urgent</span>`) != 1 {
			t.Errorf("%s does not mark exactly the one urgent request Urgent", path)
		}
	}
}

func TestAccountsQueueSearchNarrowsRows(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("QueueSearch")
	s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.seedApprovedRequest(2, admin.ID, admin.ID, headID, 600000)
	s.loginAdmin()
	body := s.get("/accounts-queue?q=PR-2026-000002")
	if !strings.Contains(body, "PR-2026-000002") || strings.Contains(body, "PR-2026-000001") {
		t.Fatalf("queue search did not narrow:\n%s", body)
	}
	// Counts are scope-wide, not search-scoped: the tab still says 2.
	if !strings.Contains(body, "Approved, unclaimed <span class=\"n\">2</span>") {
		t.Fatalf("search distorted the tab counts:\n%s", body)
	}
}

// ---------------------------------------------------------------------------
// The request picker
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
	s.loginAdmin()

	body := s.get("/payments/new")
	mustContain(t, "picker", body,
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
	)
	// The picker promises no control that does not exist: the mockup's
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
	if _, err := historicalSettlement(s.ctx, s.st, deepak, theirs,
		store.PaymentInput{HeadID: headID, PaidOn: "2026-07-24", Amount: 500000, VendorPayee: "Nova Print Works"},
		"settled", "", nil); err != nil {
		t.Fatal(err)
	}
	// The admin settles their own.
	mine := s.seedApprovedRequest(2, admin.ID, admin.ID, headID, 400000)
	if err := s.st.ReserveRequest(s.ctx, admin, mine); err != nil {
		t.Fatal(err)
	}
	if _, err := historicalSettlement(s.ctx, s.st, admin, mine,
		store.PaymentInput{HeadID: headID, PaidOn: "2026-07-25", Amount: 300000, VendorPayee: "Acme Landlord"},
		"partial", "balance later", nil); err != nil {
		t.Fatal(err)
	}
	s.loginAdmin()
	body := s.get("/payments/new")
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
// The payment entry screen
// ---------------------------------------------------------------------------

func TestPaymentEntryScreenShowsReservationApprovalAndRule(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Entry")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 10000000)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	body := s.get(fmt.Sprintf("/payments/new?request=%d", reqID))
	mustContain(t, "payment entry", body,
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
		// The request's own figures arrive prefilled.
		"Acme Landlord",
	)
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
	s.loginAdmin()
	resp := s.request(http.MethodGet, fmt.Sprintf("/payments/new?request=%d", reqID), nil, "")
	requireStatus(t, resp, http.StatusConflict)
	body := responseBody(t, resp)
	if !strings.Contains(body, "Deepak Menon took this request") || !strings.Contains(body, `class="a-list"`) {
		t.Fatalf("opening someone else's reservation must land on the conflict screen (G15):\n%s", body)
	}
}

// Historical, request-less payments stay editable: the ledger's edit screen
// still renders for them.
func TestHistoricalPaymentEditFormStillRenders(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Historical")
	payID, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{HeadID: headID, PaidOn: "2026-04-12", Amount: 12345, VendorPayee: "Legacy Vendor", PaymentMode: "cash"})
	if err != nil {
		t.Fatal(err)
	}
	s.loginAdmin()
	body := s.get(fmt.Sprintf("/payments/%d/edit", payID))
	mustContain(t, "historical payment edit form", body, "Legacy Vendor", `name="head_id"`, "Save Payment")
}

func TestPaymentCreateRequiresReservedRequest(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("X5")
	s.loginAdmin()
	// No request_id at all → refused.
	form := url.Values{"head_id": {strconvFormat(headID)}, "paid_on": {"2026-06-15"}, "amount": {"100.00"}, "settlement": {"settled"}}
	resp := s.postPayment(form)
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)
	// A request the caller has NOT reserved → refused.
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	form.Set("request_id", strconvFormat(reqID))
	resp = s.postPayment(form)
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
// The settlement preview renders the decision and writes nothing. The only
// writer in the whole flow is POST /payments.
// ---------------------------------------------------------------------------

func TestSettlementPreviewWritesNothingAndRendersTheSheet(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Preview")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 10000000)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	form := url.Values{"amount": {"98000.00"}, "paid_on": {"2026-07-25"}, "payment_mode": {"bank_transfer"}, "reference_no": {"N221260725004417"}}
	resp := s.postForm(fmt.Sprintf("/requests/%d/settlement-preview", reqID), form)
	requireStatus(t, resp, http.StatusOK)
	body := responseBody(t, resp)
	mustContain(t, "settlement sheet", body,
		`class="overlay"`, `class="sheet"`, `class="sh-head"`, `class="sh-body`, `class="sh-foot"`,
		`class="compare"`, `class="cmp-row"`, `class="cmp-row diff"`,
		"1,00,000.00", "98,000.00", "2,000.00",
		`class="choice"`,
		`class="outcome good"`, "Completed",
		`class="outcome warn"`, "Manager review",
		`name="partial_reason"`, `value="installment"`,
		`class="banner info"`, "cannot be edited or cancelled",
		`value="settled"`, `value="partial"`,
	)
	// Nothing was persisted, and the request is still merely reserved.
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
	// No attachment was stored either.
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
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"amount": {"5000.00"}, "paid_on": {"2026-07-25"}, "payment_mode": {"bank_transfer"}, "reference_no": {"N1"}}

	// No-JS: a whole page, shell and all, wrapping the same sheet.
	page := responseBody(t, s.postForm(fmt.Sprintf("/requests/%d/settlement-preview", reqID), form))
	if !strings.Contains(page, `class="cmp-row match"`) || strings.Contains(page, `class="cmp-row diff"`) {
		t.Fatalf("equal amounts must render .match, not .diff:\n%s", page)
	}
	if !strings.Contains(page, "<aside") {
		t.Fatalf("the no-JS path must render the full page:\n%s", page)
	}
	// The no-JS confirmation carries every field forward so the confirm posts once.
	mustContain(t, "no-JS confirmation", page, `name="amount"`, `name="paid_on"`, `name="payment_mode"`, `name="reference_no"`, `name="request_id"`, `enctype="multipart/form-data"`)

	// htmx: the fragment only.
	frag := responseBody(t, s.postFormHX(fmt.Sprintf("/requests/%d/settlement-preview", reqID), form))
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
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	// An authorised colleague takes it away mid-flow.
	if err := s.st.ReleaseRequest(s.ctx, admin, reqID, "needed elsewhere", true, true); err != nil {
		t.Fatal(err)
	}
	resp := s.postForm(fmt.Sprintf("/requests/%d/settlement-preview", reqID), url.Values{"amount": {"5000.00"}, "paid_on": {"2026-07-25"}})
	requireStatus(t, resp, http.StatusConflict)
	if body := responseBody(t, resp); !strings.Contains(body, `class="a-list"`) {
		t.Fatalf("a lost reservation must land on the conflict screen:\n%s", body)
	}
}

// The settlement lands on the payment, and the payment carries the whole
// story: what was approved, what left the bank, who did each step, and nothing
// that would let anybody change it afterwards.

func TestSettlementFlowCompletesAndShowsPaymentDetail(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Settle")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 10000000)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	// Settle for less than approved.
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-25"}, "partial_reason": {"Agreed TDS deduction"}, "amount": {"98000.00"}, "vendor_payee": {"Acme Landlord"}, "settlement": {"settled"}, "reference_no": {"N221260725004417"}}
	resp := s.postPayment(form)
	requireStatus(t, resp, http.StatusSeeOther)
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/payments/") {
		t.Fatalf("settlement redirect = %q, want the payment detail screen", loc)
	}
	_ = responseBody(t, resp)
	if got := requestStatusApp(t, s, reqID); got != "completed" {
		t.Fatalf("status = %q, want completed", got)
	}

	body := s.get(loc)
	mustContain(t, "payment detail", body,
		`class="banner good"`, "The request is completed",
		`class="req-head"`, "from PR-2026-000001",
		`class="pill completed"`, `class="waiting done"`,
		`class="compare"`, `class="cmp-row match"`, "confirmed settled by Accounts",
		"1,00,000.00", "98,000.00", "2,000.00",
		`class="pill neutral no-dot"`, "Read-only",
		`<ol class="thread">`, `class="tl-dot`, "reserved it for processing", "recorded a payment",
		"Full trail, request to payment",
		`class="action-bar"`,
	)
	// A linked payment has no edit or void control, and the route refuses too.
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

	// The request page shows the outcome and links to the payment.
	reqBody := s.get(fmt.Sprintf("/requests/%d", reqID))
	if !strings.Contains(reqBody, `class="compare"`) || !strings.Contains(reqBody, "98,000.00") || !strings.Contains(reqBody, loc) {
		t.Fatalf("request outcome missing the comparison or the payment link:\n%s", reqBody)
	}
}

func TestDoubleConfirmLandsOnTheExistingPayment(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Double")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"submission_key": {"double-confirm-fixture"}, "request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-25"}, "amount": {"5000.00"}, "vendor_payee": {"Acme"}, "settlement": {"settled"}, "reference_no": {"N1"}}
	first := s.postPayment(form)
	requireStatus(t, first, http.StatusSeeOther)
	_ = responseBody(t, first)
	// The accountant taps Confirm twice. The second must not look like a failure.
	second := s.postPayment(form)
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
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-06-15"}, "amount": {"3000.00"}, "vendor_payee": {"Acme Landlord"}, "settlement": {"partial"}, "partial_reason": {"balance later"}, "reference_no": {"N2"}}
	requireStatus(t, s.postPayment(form), http.StatusSeeOther)
	if got := requestStatusApp(t, s, reqID); got != "partial_review" {
		t.Fatalf("status = %q, want partial_review", got)
	}
	body := s.get(fmt.Sprintf("/requests/%d", reqID))
	if !strings.Contains(body, "balance later") || !strings.Contains(body, `class="pill partial"`) {
		t.Fatalf("partial outcome missing from the request:\n%s", body)
	}
}

// A payment linked to a request is immutable everywhere, not only on its detail
// screen: the ledger row offers no Edit button and the edit URL is refused. A
// control the store would reject is a lie wherever it appears.
func TestLinkedPaymentIsImmutableInTheLedgerAndOnItsEditRoute(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Immutable")
	s.loginAdmin()
	_, linkedID := s.settleOneRequest(1, headID, 500000, "5000.00", "settled", "2026-07-25")

	// A payment recorded before this module has no request behind it and stays
	// editable, so the ledger has to tell the two apart rather than lock
	// the whole screen.
	histID, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{HeadID: headID, PaidOn: "2026-07-10", Amount: 12345, VendorPayee: "Legacy Vendor", PaymentMode: "cash"})
	if err != nil {
		t.Fatal(err)
	}

	ledger := s.get("/payments?month=2026-07")
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
// payment. If it cannot reserve, nothing in the payment flow is reachable without
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
	body := s.get(loc)
	if !strings.Contains(body, "Record the payment") {
		t.Fatalf("Accounts cannot reach the entry screen:\n%s", body)
	}
}

// Like the queue, the picker offers a Take button only to callers holding
// reservation:reserve; anyone else would get a 403 from the POST behind it.
func TestPickerOffersNoTakeControlWithoutTheReservationGrant(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("PickerGate")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	// A request scope of All: the picker reads the caller's request scope, and
	// an empty scope (the editor's "None") lists nothing. The case under test
	// is the missing reservation grant, not the missing scope.
	s.seedProbeUser("ledger@example.test", "Ledger", "LedgerPassword123", "Ledger only", []store.Grant{
		{Resource: "payment", Action: "view"}, {Resource: "payment", Action: "create"},
	}, []store.ScopeGrant{{Resource: "request", Scope: store.ScopeAll}})

	s.login("ledger@example.test", "LedgerPassword123")
	body := s.get("/payments/new")
	if strings.Contains(body, fmt.Sprintf(`action="/requests/%d/record-payment"`, reqID)) {
		t.Fatalf("the picker offers a take control the route answers 403 to:\n%s", body)
	}
	if !strings.Contains(body, `class="co is-taken"`) {
		t.Fatalf("the picker dropped the request instead of showing it read-only:\n%s", body)
	}
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusForbidden)
}

// The payment outcome, read by the audience it is for. The requester holds no payment grant at
// all, so the outcome has to be on the request itself — and the link beside it
// must not be offered to somebody the payment route would turn away.
func TestRequesterReadsThePaymentOutcomeOnTheirOwnRequest(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Outcome")
	requester := s.seedRequester("ravi@example.test", "Ravi Kumar", "RequesterPass123")
	reqID := s.seedApprovedRequest(1, requester.ID, admin.ID, headID, 10000000)

	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-25"}, "partial_reason": {"Agreed TDS deduction"}, "amount": {"98000.00"}, "vendor_payee": {"Acme Landlord"}, "settlement": {"settled"}, "reference_no": {"N1"}}
	requireStatus(t, s.postPayment(form), http.StatusSeeOther)

	s.login("ravi@example.test", "RequesterPass123")
	body := s.get(fmt.Sprintf("/requests/%d", reqID))
	mustContain(t, "the requester's own request", body, "Payment outcome", `class="compare"`, "98,000.00", "confirmed settled by Accounts")
	if strings.Contains(body, `href="/payments/`) {
		t.Fatalf("the requester is offered a payment link the route answers 403 to:\n%s", body)
	}
}

// The payment screen is half a request screen. Reading it must therefore obey
// the same row scope /requests/{id} obeys, or payment:view becomes a way round
// it.
//
// The refusal is 404 and not 403: a 403 here and a 404 for a missing id would
// let a caller walk payments.id and learn which payments exist from the
// difference. /requests/{id} and the attachment routes follow the same rule.
func TestPaymentDetailRefusesTheRequestBehindItOutOfScope(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Scope")
	s.loginAdmin()
	_, linkedID := s.settleOneRequest(1, headID, 500000, "5000.00", "settled", "2026-07-25")
	histID, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{HeadID: headID, PaidOn: "2026-07-10", Amount: 12345, VendorPayee: "Legacy Vendor", PaymentMode: "cash"})
	if err != nil {
		t.Fatal(err)
	}
	// Payment scope All and no request scope: the ledger is fully theirs, the
	// request behind a linked row is not. (An empty payment scope reads no
	// ledger row at all, so the scope is explicit.)
	s.seedProbeUser("auditor@example.test", "Auditor", "AuditorPassword123", "Ledger reader", []store.Grant{
		{Resource: "payment", Action: "view"},
	}, []store.ScopeGrant{{Resource: "payment", Scope: store.ScopeAll}})

	s.login("auditor@example.test", "AuditorPassword123")
	resp := s.request(http.MethodGet, fmt.Sprintf("/payments/%d", linkedID), nil, "")
	requireStatus(t, resp, http.StatusNotFound)
	refused := responseBody(t, resp)
	// Indistinguishable from an id nobody ever used: same status, and the refusal
	// must not leak the number, payee or amount of the payment it is hiding.
	missing := s.request(http.MethodGet, "/payments/99999", nil, "")
	requireStatus(t, missing, http.StatusNotFound)
	if strings.Contains(refused, "Legacy Vendor") || strings.Contains(refused, "5,000") {
		t.Fatalf("the refusal describes the payment it is withholding:\n%s", refused)
	}
	// The request-less ledger row is untouched: this closes a way into request
	// data, not a way into the ledger.
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/payments/%d", histID), nil, ""), http.StatusOK)
}

// The trail's head already names the actor, and every payment_request audit
// summary starts with that same name, so the body drops it rather than repeat
// it.
func TestTrailBodyDropsTheActorNameTheHeadAlreadyCarries(t *testing.T) {
	for _, tc := range []struct{ name, actor, summary, want string }{
		{"request side", "Priya Nair", "Priya Nair approved request PR-2026-000001 for ₹1,00,000.00", "Approved request PR-2026-000001 for ₹1,00,000.00"},
		{"payment side", "Priya Nair", "Priya Nair reserved the request for processing", "Reserved the request for processing"},
		{"summary without the actor", "Priya Nair", "Reserved request for processing", "Reserved request for processing"},
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
	s.loginAdmin()
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
	requireStatus(t, s.postPayment(form), http.StatusSeeOther)
	var payID int64
	if err := s.st.DB().QueryRow(`SELECT id FROM payments WHERE request_id=?`, reqID).Scan(&payID); err != nil {
		t.Fatal(err)
	}

	body := s.get(fmt.Sprintf("/payments/%d", payID))
	mustContain(t, "payment detail", body,
		// The waiting line is waitingOn's, so it says "you" to the manager it
		// is waiting on rather than reading them their own name.
		`class="pill partial"`, "Partial — manager review", `class="waiting you"`, "Waiting on you",
		// The trail's body no longer repeats the name in the head above it.
		`<div class="tl-body">Approved request PR-2026-000001`,
		// The mockup's second proof row: the request's own invoice.
		"SE-26-27-1184.pdf", "Invoice from the request",
	)
	if strings.Contains(body, `<div class="tl-body">`+admin.Name+" approved") {
		t.Fatalf("the trail prints the actor's name twice:\n%s", body)
	}
}

// The manager's side of a partial settlement. Two decisions, each in
// its own .overlay > .sheet, and the accept lands in a terminal state that is
// visibly not a clean "completed".
func TestPartialReviewScreenAndManagerDecision(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Review")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 9500000)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-23"}, "amount": {"60000.00"}, "vendor_payee": {"Nova Print Works"}, "settlement": {"partial"}, "partial_reason": {"Vendor delivered 700 of the 1,000 copies."}, "reference_no": {"N9912"}}
	requireStatus(t, s.postPayment(form), http.StatusSeeOther)

	body := s.get(fmt.Sprintf("/requests/%d/partial-review", reqID))
	mustContain(t, "partial review", body,
		`class="req-head"`, `class="pill partial"`, `class="waiting you"`,
		// "payee", as every other screen says: the request may be a
		// reimbursement owed to an employee.
		`class="compare"`, "Still owed to the payee", "35,000.00",
		`class="banner warn"`, "Vendor delivered 700 of the 1,000 copies.",
		`class="pill neutral no-dot"`, "Cannot be edited",
		`<ol class="thread">`, `class="comment-box"`,
		`id="close-sheet"`, "Accept and close", "Completed — partial accepted",
		`id="concern-sheet"`, "Raise a concern", "This does not reverse anything",
		`class="overlay"`, `class="sheet"`,
	)
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
	// Accepting closes it into the distinct terminal state.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/accept-partial", reqID), url.Values{"note": {"Balance invoiced separately."}}), http.StatusSeeOther)
	if got := requestStatusApp(t, s, reqID); got != "completed_partial" {
		t.Fatalf("status after accept = %q, want completed_partial (G14)", got)
	}
	after := s.get(fmt.Sprintf("/requests/%d", reqID))
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

// TestPartialReviewDecisionsBelongToTheRequestsOwnManager is the ownership
// proof. approval:accept_partial says a person may accept a shortfall; it never
// says whose. Every other manager verb in the system — ApproveRequest,
// ReturnRequest, DecideCancellation — refuses an actor who is not the request's
// own manager, and writing off money permanently is not the exception.
func TestPartialReviewDecisionsBelongToTheRequestsOwnManager(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Owner")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 9500000)
	s.loginAdmin()
	s.seedPartialReview(reqID, headID, "Vendor delivered 700 of the 1,000 copies.")

	// A second person holding every verb and seeing every request, who simply
	// is not this request's manager.
	s.seedColleague("other-manager@example.test", "Other Manager", "OtherManagerPass1")
	s.login("other-manager@example.test", "OtherManagerPass1")

	body := s.get(fmt.Sprintf("/requests/%d/partial-review", reqID))
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
	s.loginAdmin()
	s.seedPartialReview(reqID, headID, "Vendor delivered 700 of the 1,000 copies.")

	path := fmt.Sprintf("/requests/%d/partial-review", reqID)
	body := s.get(path)
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
	after := s.get(path)
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
// The release / reassign screen
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
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	body := s.get(fmt.Sprintf("/requests/%d/reservation", reqID))
	mustContain(t, "reservation screen", body,
		`class="reserve-bar"`, "Reserved by you",
		`class="banner bad"`, "Confirm no payment has been started",
		`class="choice"`, `value="release"`, `value="reassign"`,
		`data-when="action:reassign"`, "Deepak Menon",
		`name="reason"`, `class="checkline"`,
		`class="action-bar"`, `<ol class="thread">`,
	)

	// Release without a reason is refused and the reservation survives.
	resp := s.postForm(fmt.Sprintf("/requests/%d/release", reqID), url.Values{"action": {"release"}, "confirm": {"on"}})
	if resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("release without a reason accepted: %d", resp.StatusCode)
	}
	_ = responseBody(t, resp)
	// Confirmation is still required.
	resp = s.postForm(fmt.Sprintf("/requests/%d/release", reqID), url.Values{"action": {"release"}, "reason": {"Bank details unconfirmed"}})
	if resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("release without confirmation accepted: %d", resp.StatusCode)
	}
	_ = responseBody(t, resp)
	if got := requestStatusApp(t, s, reqID); got != "processing" {
		t.Fatalf("status after refused releases = %q, want processing", got)
	}

	// Reassign keeps it in processing and moves the holder.
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
	s.loginAdmin()
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

// A release that carries a reason and the confirmation puts the request back in
// the open queue, drops the holder, and lands the accountant on the queue rather
// than on a request they no longer hold.
func TestReleaseReturnsTheRequestToTheQueue(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Released")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 4200000)
	s.loginAdmin()
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
	if got := s.get(fmt.Sprintf("/requests/%d", reqID)); !strings.Contains(got, "Vendor bank details need confirming") {
		t.Fatalf("the release reason is not in the request history:\n%s", got)
	}
	// And the screen is gone with the reservation: there is nothing to release.
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/reservation", reqID), nil, ""), http.StatusConflict)
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
// reservation:reassign alone, and reassign and release are independent
// permissions, so the screen must open for a role that holds only reassign.
func TestReservationScreenOpensForAReassignOnlyRole(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("ReassignOnly")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 900000)
	s.loginAdmin()
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
	queue := s.get("/accounts-queue?tab=processing")
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

// Holding a reservation verb somewhere is not permission to read this request.
// The screen and both of its POSTs answer to the same data scope the
// request's own detail page does.
func TestReservationScreenRefusesARequestTheCallerMayNotRead(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Scoped")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 700000)
	s.loginAdmin()
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

	// All four answer 404 rather than 403: every one routes through
	// loadViewableRequest, and an out-of-scope row has to be indistinguishable
	// from a row that does not exist or the status code enumerates the id space.
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""), http.StatusNotFound)
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d/reservation", reqID), nil, ""), http.StatusNotFound)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/reassign", reqID), url.Values{
		"action": {"reassign"}, "to_user_id": {strconvFormat(deepak.ID)}, "reason": {"I want it"}, "confirm": {"on"},
	}), http.StatusNotFound)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/release", reqID), url.Values{
		"action": {"release"}, "reason": {"Not mine to hold"}, "confirm": {"on"},
	}), http.StatusNotFound)

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
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	body := s.get(fmt.Sprintf("/requests/%d/reservation", reqID))
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
// and hidden — unlike disabled — does not exempt it. Release must come first,
// so pressing Enter on the checked "Release it" radio releases rather than
// posting to /reassign with no target and losing the typed reason.
func TestReleaseIsTheReservationFormsDefaultButton(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("DefaultButton")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 300000)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)

	body := s.get(fmt.Sprintf("/requests/%d/reservation", reqID))
	release := strings.Index(body, fmt.Sprintf(`formaction="/requests/%d/release"`, reqID))
	reassign := strings.Index(body, fmt.Sprintf(`formaction="/requests/%d/reassign"`, reqID))
	if release < 0 || reassign < 0 {
		t.Fatalf("both submit buttons must carry a formaction:\n%s", body)
	}
	if release > reassign {
		t.Fatalf("the reassign button precedes the release button, making it the form's default")
	}
}

// The trail's timestamps are localised like the reserve bar's, so one screen
// never prints two different times for one event.
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
// reservation reassignment (moving who is paying it) write different audit
// actions, so an approver swap never reads as "X reassigned the reservation" in
// this screen's history.
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
// Hold, unhold and the stale reservation screen
// ---------------------------------------------------------------------------

// Holding an approved request moves it from the takeable tab of the accounts
// queue to the hold tab, and unholding moves it back.
func TestHoldAndUnholdMoveTheRequestThroughTheQueue(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Hold")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 2500000)
	s.loginAdmin()

	body := s.get("/accounts-queue")
	if !strings.Contains(body, "PR-2026-000001") {
		t.Fatalf("approved request missing from the queue:\n%s", body)
	}
	// Hold takes it out of the takeable tab and onto the hold tab.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", reqID), url.Values{"reason": {"Which site is this for?"}}), http.StatusSeeOther)
	body = s.get("/accounts-queue")
	if strings.Contains(body, "Take for processing") {
		t.Fatalf("held request is still takeable:\n%s", body)
	}
	held := s.get("/accounts-queue?tab=hold")
	if !strings.Contains(held, "PR-2026-000001") {
		t.Fatalf("held request missing from the hold tab:\n%s", held)
	}
	// The request screen explains the hold and offers the Accounts-only release.
	detail := s.get(fmt.Sprintf("/requests/%d", reqID))
	mustContain(t, "on-hold request detail", detail, `class="pill hold"`, `class="banner warn"`, "Which site is this for?", "Release hold", `class="comment-box"`)
	// Unhold restores it.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/unhold", reqID), url.Values{}), http.StatusSeeOther)
	body = s.get("/accounts-queue")
	if !strings.Contains(body, "Take for processing") {
		t.Fatalf("unheld request is not takeable again:\n%s", body)
	}
}

func TestStaleReservationScreenOffersTheFourChoices(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Stale")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 7800000)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET processing_at=datetime('now','-26 hours') WHERE id=?`, reqID); err != nil {
		t.Fatal(err)
	}
	body := s.get(fmt.Sprintf("/requests/%d/reservation/stale", reqID))
	mustContain(t, "stale reservation screen", body,
		`class="banner warn"`, "reserved by you for", "26 h",
		`class="req-head"`, `class="pill processing"`, `class="waiting"`,
		`class="a-list"`, "Carry on and record the payment", "Release it", "Hand it to a colleague", "Put it on hold",
		"Who has been told", `<ol class="thread">`,
	)
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
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", reqID), url.Values{"reason": {"Which site is this for?"}}), http.StatusSeeOther)
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/cancel", reqID), url.Values{"reason": {"Site dropped"}}), http.StatusSeeOther)

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
	held := s.get("/accounts-queue?tab=hold")
	if strings.Contains(held, "PR-2026-000001") {
		t.Fatalf("cancelled request is still in the hold tab:\n%s", held)
	}
	detail := s.get(fmt.Sprintf("/requests/%d", reqID))
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
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", reqID), url.Values{"reason": {"Which site is this for?"}}), http.StatusSeeOther)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/cancel-request", reqID), url.Values{"reason": {"No longer needed"}}), http.StatusSeeOther)

	var onHold int
	if err := s.st.DB().QueryRow(`SELECT on_hold FROM payment_requests WHERE id=?`, reqID).Scan(&onHold); err != nil {
		t.Fatal(err)
	}
	if onHold != 0 {
		t.Fatalf("cancellation_requested request still carries on_hold=%d", onHold)
	}
	detail := s.get(fmt.Sprintf("/requests/%d", reqID))
	if strings.Contains(detail, `class="pill hold"`) {
		t.Fatalf("pill still says On hold while a cancellation is pending:\n%s", detail)
	}
	mustContain(t, "pending cancellation", detail, "Cancellation requested", `class="waiting you"`)
}

// The on-hold state carries exactly one banner in
// mockups/screens/request-on-hold.html. The approved-lock banner must not stack
// above it: it would say "payment freezes while your approver decides" when the
// approver is deciding nothing — Accounts is holding it.
func TestTheHoldBannerIsTheOnlyBannerOnAHeldRequest(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("HoldBanner")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 2500000)
	s.loginAdmin()
	detail := s.get(fmt.Sprintf("/requests/%d", reqID))
	if !strings.Contains(detail, "Approved requests are locked") {
		t.Fatalf("approved request is missing its lock banner:\n%s", detail)
	}
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", reqID), url.Values{"reason": {"Which site is this for?"}}), http.StatusSeeOther)

	detail = s.get(fmt.Sprintf("/requests/%d", reqID))
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

// The stale-reservation screen is only reachable if something links to it. The queue is the one place that
// knows which reservation has crossed the one-day mark, so the mockup's own
// Resume on that row goes to the nudge, not back to the payment form.
func TestTheQueueLinksAStaleReservationToTheNudgeScreen(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("StaleLink")
	fresh := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 4400000)
	old := s.seedApprovedRequest(2, admin.ID, admin.ID, headID, 7800000)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", fresh), url.Values{}), http.StatusSeeOther)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", old), url.Values{}), http.StatusSeeOther)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET processing_at=datetime('now','-26 hours') WHERE id=?`, old); err != nil {
		t.Fatal(err)
	}

	body := s.get("/accounts-queue?tab=processing")
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
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET processing_at=datetime('now','-26 hours') WHERE id=?`, reqID); err != nil {
		t.Fatal(err)
	}
	body := s.get(fmt.Sprintf("/requests/%d/reservation/stale", reqID))

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
	s.loginAdmin()

	// The queue names the vendor, and searching for it finds the row.
	queue := s.get("/accounts-queue")
	if !strings.Contains(queue, vendorName) {
		t.Fatalf("the queue does not name the vendor:\n%s", queue)
	}
	found := s.get("/accounts-queue?q=Sundaram")
	if !strings.Contains(found, "PR-2026-000001") {
		t.Fatalf("searching the queue by vendor name found nothing:\n%s", found)
	}

	// The entry screen names it, and so does the confirmation sheet.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	entry := s.get(fmt.Sprintf("/payments/new?request=%d", reqID))
	if !strings.Contains(entry, vendorName) {
		t.Fatalf("the payment entry screen does not name the vendor:\n%s", entry)
	}
	form := url.Values{"amount": {"5000.00"}, "paid_on": {"2026-07-25"}, "payment_mode": {"bank_transfer"}, "reference_no": {"N1"}}
	sheet := responseBody(t, s.postFormHX(fmt.Sprintf("/requests/%d/settlement-preview", reqID), form))
	if !strings.Contains(sheet, vendorName) {
		t.Fatalf("the settlement sheet does not name the vendor:\n%s", sheet)
	}

	// Settling carries the payee onto the payment row itself.
	settle := url.Values{
		"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)},
		"paid_on": {"2026-07-25"}, "amount": {"5000.00"}, "payment_mode": {"bank_transfer"},
		"reference_no": {"N1"}, "vendor_payee": {vendorName}, "settlement": {"settled"},
	}
	resp := s.postPayment(settle)
	requireStatus(t, resp, http.StatusSeeOther)
	loc := resp.Header.Get("Location")
	var storedPayee string
	if err := s.st.DB().QueryRow(`SELECT COALESCE(vendor_payee,'') FROM payments WHERE request_id=?`, reqID).Scan(&storedPayee); err != nil {
		t.Fatal(err)
	}
	if storedPayee != vendorName {
		t.Fatalf("payments.vendor_payee = %q, want %q — the payment was written with no payee", storedPayee, vendorName)
	}
	detail := s.get(loc)
	if !strings.Contains(detail, "<h1>"+vendorName+"</h1>") {
		t.Fatalf("the payment detail h1 does not name the vendor:\n%s", detail)
	}
}

// ReserveRequest refuses for three reasons, and the conflict screen names the
// real one rather than always saying "Someone else took this request before
// you".
func TestTheConflictScreenNamesTheRealRefusal(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Conflict causes")
	requester := s.seedRequester("conflict@example.test", "Conflict Requester", "ConflictPass123")

	s.loginAdmin()
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
