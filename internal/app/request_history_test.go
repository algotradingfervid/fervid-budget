package app

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

func TestDecisionHistoryVisibleEscapedAndChronologicalForAuthorizedReaders(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("History QA")
	managerID := seedSecondApprover(t, s)
	manager, err := s.st.UserByID(s.ctx, managerID)
	if err != nil {
		t.Fatal(err)
	}
	requester := s.seedRequester("history@example.test", "History Requester", "RequesterPass123")
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "History QA", ProjectID: 1, HeadID: headID, Amount: 12345,
		Purpose: "Verify notes", ManagerID: managerID, ExpenseDate: "2026-09-26"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.ApproveRequest(s.ctx, manager, id, 12345, "First approval <script>alert(1)</script>"); err != nil {
		t.Fatal(err)
	}
	if err := s.st.RequestCancellation(s.ctx, requester, id, "No longer required"); err != nil {
		t.Fatal(err)
	}
	if err := s.st.DecideCancellation(s.ctx, manager, id, true, "Final cancellation note"); err != nil {
		t.Fatal(err)
	}
	for _, login := range []struct{ email, password string }{
		{requester.Email, "RequesterPass123"}, {manager.Email, "ApproverPass123"}, {s.cfg.AdminEmail, testAdminPassword},
	} {
		s.login(login.email, login.password)
		resp := s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, "")
		requireStatus(t, resp, http.StatusOK)
		body := responseBody(t, resp)
		first, last := strings.Index(body, "First approval &lt;script&gt;alert(1)&lt;/script&gt;"), strings.Index(body, "Final cancellation note")
		if first < 0 || last < first {
			t.Errorf("%s: missing or out-of-order decision notes", login.email)
		}
		if strings.Contains(body, "<script>alert(1)</script>") {
			t.Errorf("%s: decision note was not escaped", login.email)
		}
	}
	other := s.seedRequester("unrelated-history@example.test", "Unrelated", "RequesterPass123")
	s.login(other.Email, "RequesterPass123")
	resp := s.request(http.MethodGet, "/requests/"+strconvFormat(id), nil, "")
	if resp.StatusCode == http.StatusOK {
		t.Fatal("unrelated requester can read decision history")
	}
	body := responseBody(t, resp)
	if strings.Contains(body, "First approval") || strings.Contains(body, "Final cancellation note") {
		t.Fatal("unauthorized response leaked notes")
	}
}

func TestDuplicateCardsUseCanonicalNextActionForTerminalAndPendingRequests(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Duplicate status QA")
	managerID := seedSecondApprover(t, s)
	requester := s.seedRequester("duplicate-status@example.test", "Duplicate Requester", "RequesterPass123")
	vendorID := s.seedVendor(store.VendorInput{Name: "Duplicate Status Vendor", VendorType: "company", Status: "active"})
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget", Type: "vendor_invoice",
		ShortTitle: "Existing invoice", ProjectID: 1, HeadID: headID, Amount: 12345,
		Purpose: "Verify duplicate status", ManagerID: managerID, VendorID: vendorID, InvoiceNo: "HISTORY-QA-1", InvoiceDate: "2026-09-26"})
	if err != nil {
		t.Fatal(err)
	}
	s.login(requester.Email, "RequesterPass123")
	for _, tc := range []struct{ status, want string }{
		{"completed", "Nothing pending"}, {"completed_partial", "Nothing pending"},
		{"cancelled", ""}, {"rejected", ""}, {"withdrawn", ""},
		{"approved", "Waiting on Accounts"}, {"pending", "Waiting on Kavita Rao"}, {"returned", "Waiting on you"},
	} {
		// Isolate rendering from transition guards, which have their own store tests.
		if _, err := s.st.DB().Exec(`UPDATE payment_requests SET status=? WHERE id=?`, tc.status, id); err != nil {
			t.Fatal(err)
		}
		body := responseBody(t, s.postFormHX("/requests/duplicate-check", url.Values{
			"type": {"vendor_invoice"}, "vendor_id": {strconvFormat(vendorID)}, "invoice_no": {"HISTORY-QA-1"}, "amount": {"123.45"},
		}))
		if tc.want == "" && strings.TrimSpace(body) != "" {
			t.Errorf("%s should remain excluded from duplicate matching", tc.status)
		}
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: missing %q in %s", tc.status, tc.want, body)
		}
		if tc.status != "pending" && strings.Contains(body, "Waiting on Kavita Rao") {
			t.Errorf("%s falsely waits on manager", tc.status)
		}
	}
}

func TestDecisionNotesAppearInMergedPaymentTrails(t *testing.T) {
	events := []store.AuditEntry{
		{EntityType: "payment_request", Action: "approve", ActorName: "Manager", Summary: "Manager approved request for ₹123.45", AfterJSON: `{"DecisionReason":"Approval evidence"}`},
		{EntityType: "payment_request", Action: "cancel", ActorName: "Manager", Summary: "Manager cancelled request at the requester's asking", BeforeJSON: `{"Status":"cancellation_requested"}`, AfterJSON: `{"DecisionReason":"Cancellation evidence"}`},
	}
	trail := partialTrail(events, nil)
	for i, want := range []string{"Approval evidence", "Cancellation evidence"} {
		if !strings.Contains(trail[i].Body, want) {
			t.Errorf("partial trail %d omits %q", i, want)
		}
		if !strings.Contains(trailBody(events[i]), want) {
			t.Errorf("receipt trail %d omits %q", i, want)
		}
	}
}
