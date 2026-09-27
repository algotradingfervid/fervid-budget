package app

import (
	"fervidbudget/internal/store"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestInstallmentHTTPFlowExplicitChoiceAndRemainingBalance(t *testing.T) {
	s := newAppTestServer(t)
	admin, head := s.seedHead("Installments")
	id := s.seedApprovedRequest(1, admin.ID, admin.ID, head, 1200000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", id), url.Values{}), http.StatusSeeOther)
	entry := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/payments/new?request=%d", id), nil, ""))
	token := regexp.MustCompile(`name="submission_key" value="([^"]+)"`).FindStringSubmatch(entry)
	if len(token) != 2 {
		t.Fatal("missing confirmation idempotency key")
	}
	form := url.Values{"request_id": {strconvFormat(id)}, "amount": {"7000"}, "paid_on": {"2026-06-15"}, "reference_no": {"INSTALLMENT-REF"}, "submission_key": {token[1]}}
	preview := responseBody(t, s.postForm(fmt.Sprintf("/requests/%d/settlement-preview", id), form))
	if regexp.MustCompile(`name="settlement"[^>]*checked`).MatchString(preview) {
		t.Fatal("short payment has a default disposition")
	}
	if !strings.Contains(preview, "Installment — pay the balance later") {
		t.Fatal("missing continuation option")
	}
	requireStatus(t, s.postForm("/payments", form), http.StatusBadRequest)
	form.Set("settlement", "installment")
	first := s.postForm("/payments", form)
	requireStatus(t, first, http.StatusSeeOther)
	firstLocation := first.Header.Get("Location")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", id), url.Values{}), http.StatusSeeOther)
	retry := s.postForm("/payments", form)
	requireStatus(t, retry, http.StatusSeeOther)
	if retry.Header.Get("Location") != firstLocation {
		t.Fatal("retry did not resolve original payment")
	}
	entry = responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/payments/new?request=%d", id), nil, ""))
	if !strings.Contains(entry, `data-approved="500000"`) {
		t.Fatal("entry ceiling is not remaining 5000")
	}
	token = regexp.MustCompile(`name="submission_key" value="([^"]+)"`).FindStringSubmatch(entry)
	form.Set("submission_key", token[1])
	form.Set("amount", "6000")
	form.Set("settlement", "settled")
	overResponse := s.postForm("/payments", form)
	requireStatus(t, overResponse, http.StatusBadRequest)
	overBody := responseBody(t, overResponse)
	if !strings.Contains(overBody, "remaining approved balance") || !strings.Contains(overBody, "separately approved request") || strings.Contains(overBody, "cancel this request") {
		t.Fatal("overremaining refusal suggests unavailable cancellation or omits recovery")
	}
	form.Set("amount", "5000")
	requireStatus(t, s.postForm("/payments", form), http.StatusSeeOther)
	body := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", id), nil, ""))
	for _, want := range []string{"2 linked payments", "Total paid to date", "12,000.00", "7,000.00", "5,000.00"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
}

// These are HTTP workflows: the outcome must follow the live request balance,
// while an older immutable payment continues to show its own recorded amount.
func TestPaymentOutcomeFollowsLifecycleWithoutRewritingHistory(t *testing.T) {
	s := newAppTestServer(t)
	admin, head := s.seedHead("Outcome lifecycle")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	var issuedForm url.Values
	pay := func(id int64, amount, disposition, reason string) string {
		t.Helper()
		requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", id), url.Values{}), http.StatusSeeOther)
		entry := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/payments/new?request=%d", id), nil, ""))
		form := url.Values{"request_id": {strconvFormat(id)}, "amount": {amount}, "paid_on": {"2026-06-15"}, "reference_no": {"OUTCOME-REF"}, "settlement": {disposition}, "partial_reason": {reason}}
		for _, name := range []string{"submission_key", "expected_paid"} {
			field := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindStringSubmatch(entry)
			if len(field) != 2 {
				t.Fatalf("missing issued %s", name)
			}
			form.Set(name, field[1])
		}
		issuedForm = form
		resp := s.postForm("/payments", form)
		requireStatus(t, resp, http.StatusSeeOther)
		return resp.Header.Get("Location")
	}
	check := func(label, path string, wants, excludes []string) {
		t.Helper()
		response := s.request(http.MethodGet, path, nil, "")
		requireStatus(t, response, http.StatusOK)
		body := responseBody(t, response)
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %q", label, want)
			}
		}
		for _, bad := range excludes {
			if strings.Contains(body, bad) {
				t.Errorf("%s incorrectly contains %q", label, bad)
			}
		}
	}
	id := s.seedApprovedRequest(1, admin.ID, admin.ID, head, 1200000)
	first := pay(id, "7000", "installment", "")
	for _, path := range []string{fmt.Sprintf("/requests/%d", id), first} {
		check("open installment", path, []string{"Still owed to the payee", "5,000.00"}, []string{"confirmed settled by Accounts", "Fully settled", "This payment settled"})
	}
	// A stale confirm must replay the same issued token, preserve the ledger,
	// and describe the recorded installment without claiming the request ended.
	issuedForm.Set("amount", "1000")
	replayed := s.postForm("/payments", issuedForm)
	requireStatus(t, replayed, http.StatusSeeOther)
	if replayed.Header.Get("Location") != first {
		t.Fatal("issued-token replay did not resolve the original payment")
	}
	check("replayed installment", first,
		[]string{"This payment confirmation was already recorded", "No additional payment was saved", "7,000.00", "Still owed to the payee", "5,000.00"},
		[]string{"was settled by PAY-"})
	blockedEdit := s.request(http.MethodGet, first+"/edit", nil, "")
	requireStatus(t, blockedEdit, http.StatusSeeOther)
	if blockedEdit.Header.Get("Location") != first {
		t.Fatal("immutable edit did not resolve the original payment")
	}
	check("blocked installment edit", first,
		[]string{"This payment cannot be edited", "Nothing was changed", "7,000.00", "Still owed to the payee", "5,000.00"},
		[]string{"It settled "})
	var paymentCount int
	var paidTotal int64
	if err := s.st.DB().QueryRow(`SELECT COUNT(*),SUM(amount) FROM payments WHERE request_id=?`, id).Scan(&paymentCount, &paidTotal); err != nil {
		t.Fatal(err)
	}
	if paymentCount != 1 || paidTotal != 700000 {
		t.Fatalf("replay/edit changed immutable installment: count=%d paid=%d", paymentCount, paidTotal)
	}
	pay(id, "5000", "settled", "")
	check("historical installment after completion", first,
		[]string{"confirmed settled by Accounts", "7,000.00", "12,000.00"}, []string{"Still owed to the payee", "a balance is still owed"})

	continued := s.seedApprovedRequest(2, admin.ID, admin.ID, head, 1200000)
	partial := pay(continued, "7000", "partial", "Awaiting the remaining delivery.")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/accept-partial", continued), url.Values{"decision": {"continue"}, "note": {"Pay remaining balance after delivery."}}), http.StatusSeeOther)
	for _, path := range []string{fmt.Sprintf("/requests/%d", continued), partial} {
		check("manager kept balance payable", path, []string{"Still owed to the payee", "5,000.00"}, []string{"confirmed settled by Accounts", "Balance written off", "This payment settled"})
	}

	deducted := s.seedApprovedRequest(3, admin.ID, admin.ID, head, 1200000)
	deduction := pay(deducted, "7000", "settled", "Contractual adjustment agreed with payee.")
	for _, path := range []string{fmt.Sprintf("/requests/%d", deducted), deduction} {
		check("agreed deduction", path, []string{"confirmed settled by Accounts", "5,000.00", "Contractual adjustment agreed with payee."}, []string{"Balance written off", "Still owed to the payee", "genuine partial payment"})
	}

	writtenOff := s.seedApprovedRequest(4, admin.ID, admin.ID, head, 1200000)
	accepted := pay(writtenOff, "7000", "partial", "Supplier cannot fulfill remaining delivery.")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/accept-partial", writtenOff), url.Values{"note": {"Manager accepts the unpaid shortfall."}}), http.StatusSeeOther)
	for _, path := range []string{fmt.Sprintf("/requests/%d", writtenOff), accepted} {
		check("manager accepted shortfall", path, []string{"Balance written off", "5,000.00"}, []string{"confirmed settled by Accounts", "Still owed to the payee", "a balance is still owed"})
	}
}

func TestRequestFieldErrorsCollectAllApplicableFields(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	form := url.Values{"type": {"employee_advance"}, "treatment": {"recoverable"}, "recoverable_category": {"employee_advance"}, "short_title": {"Return missing"}, "amount": {"123.00"}, "purpose": {"Travel"}, "advance_reason": {"Travel advance"}}
	resp := s.postForm("/requests", form)
	requireStatus(t, resp, http.StatusBadRequest)
	body := responseBody(t, resp)
	for _, want := range []string{`href="#expected-return"`, `href="#terms"`, `id="error-expected-return"`, `aria-describedby="error-expected-return"`, `Return missing`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestRequestFiltersSurviveBucketsAndExport(t *testing.T) {
	s := newAppTestServer(t)
	admin, head := s.seedHead("Filtered requests")
	one := s.seedApprovedRequest(1, admin.ID, admin.ID, head, 100000)
	two := s.seedApprovedRequest(2, admin.ID, admin.ID, head, 200000)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET status='cancelled' WHERE id=?`, one); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET type='reimbursement',status='cancelled' WHERE id=?`, two); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/requests?scope=own&bucket=all&type=vendor_invoice&treatment=budget", nil, ""))
	if !strings.Contains(body, `bucket=closed&amp;type=vendor_invoice&amp;project_id=0&amp;vendor_id=0&amp;treatment=budget`) {
		t.Fatal("bucket drops filter")
	}
	if !strings.Contains(body, `/requests/export.csv?scope=own&amp;bucket=all&amp;type=vendor_invoice&amp;project_id=0&amp;vendor_id=0&amp;treatment=budget`) {
		t.Fatal("export drops filter")
	}
	closed := responseBody(t, s.request(http.MethodGet, "/requests?scope=own&bucket=closed&type=vendor_invoice&treatment=budget", nil, ""))
	if !strings.Contains(closed, "PR-2026-000001") || strings.Contains(closed, "PR-2026-000002") {
		t.Fatal("closed result widens filter")
	}
	csv := responseBody(t, s.request(http.MethodGet, "/requests/export.csv?scope=own&bucket=closed&type=vendor_invoice&treatment=budget", nil, ""))
	if !strings.Contains(csv, "PR-2026-000001") || strings.Contains(csv, "PR-2026-000002") {
		t.Fatal("export differs from filtered list")
	}
}

func TestRejectedShortSettlementReturnsTypedEntryValues(t *testing.T) {
	s := newAppTestServer(t)
	admin, head := s.seedHead("Preserved entry")
	id := s.seedApprovedRequest(1, admin.ID, admin.ID, head, 1200000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", id), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(id)}, "amount": {"7000.00"}, "paid_on": {"2026-06-15"}, "payment_mode": {"upi"}, "reference_no": {"UXR-PAY-7000"}, "remarks": {"Retain this processing note"}, "settlement": {"settled"}}
	failure := s.postForm("/payments", form)
	requireStatus(t, failure, http.StatusBadRequest)
	body := responseBody(t, failure)
	if !strings.Contains(body, `formaction="/payments/new?request=`) {
		t.Fatal("no return form")
	}
	returned := s.postForm(fmt.Sprintf("/payments/new?request=%d", id), form)
	requireStatus(t, returned, http.StatusOK)
	body = responseBody(t, returned)
	for _, want := range []string{`value="7000.00"`, `value="2026-06-15"`, `value="upi" selected`, `value="UXR-PAY-7000"`, `Retain this processing note`} {
		if !strings.Contains(body, want) {
			t.Errorf("lost %s", want)
		}
	}
	var count int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM payments WHERE request_id=?`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("return workflow wrote %d payments", count)
	}
}

func TestClosedIncludesCompletedAndClearFiltersRetainsContext(t *testing.T) {
	s := newAppTestServer(t)
	admin, head := s.seedHead("Closed requests")
	id := s.seedApprovedRequest(1, admin.ID, admin.ID, head, 1200000)
	if err := s.st.ReserveRequest(s.ctx, admin, id); err != nil {
		t.Fatal(err)
	}
	if _, err := historicalSettlement(s.st, s.ctx, admin, id, store.PaymentInput{PaidOn: "2026-06-15", Amount: 1200000}, "settled", "", nil); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	closed := responseBody(t, s.request(http.MethodGet, "/requests?scope=own&bucket=closed&type=vendor_invoice&treatment=budget", nil, ""))
	if !strings.Contains(closed, "PR-2026-000001") {
		t.Fatal("completed payment absent from Closed")
	}
	filtered := responseBody(t, s.request(http.MethodGet, "/requests?scope=own&bucket=closed&type=vendor_invoice&treatment=budget&q=no-such-invoice", nil, ""))
	if !strings.Contains(filtered, `href="/requests?bucket=closed&amp;scope=own">Clear filters</a>`) {
		t.Fatal("no visible functional reset")
	}
	reset := responseBody(t, s.request(http.MethodGet, "/requests?bucket=closed&scope=own", nil, ""))
	if !strings.Contains(reset, "PR-2026-000001") {
		t.Fatal("clear filters does not recover completed record")
	}
	req := httptest.NewRequest(http.MethodGet, "/requests?return_to=/vendors/42/requests&back=/reports/monthly", nil)
	href := requestClearURL(req, store.RequestListOptions{Scope: "own", Bucket: "closed", Type: "vendor_invoice", Treatment: "budget", Query: "absent", ProjectID: 9, VendorID: 42})
	target, err := url.Parse(href)
	if err != nil {
		t.Fatal(err)
	}
	q := target.Query()
	if q.Get("type") != "" || q.Get("treatment") != "" || q.Get("q") != "" || q.Get("bucket") != "closed" || q.Get("scope") != "own" || q.Get("vendor_id") != "42" || q.Get("return_to") != "/vendors/42/requests" || q.Get("back") != "/reports/monthly" {
		t.Fatalf("reset loses context or retains filters: %s", href)
	}
}
