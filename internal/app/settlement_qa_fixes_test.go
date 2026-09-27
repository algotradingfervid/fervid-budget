package app

import (
	"net/http"
	"net/url"
	"testing"

	"fervidbudget/internal/notify"
)

// PAY-D02: configured audiences are authoritative. A successful receipt may
// confirm the write without inventing notification recipients.
func TestPaymentReceiptDoesNotInventNotificationRecipients(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("ReceiptAudience")
	requester := s.seedRequester("receipt-requester@example.test", "Receipt Requester", "ReceiptPass1234")
	manager := s.seedRoleUser("receipt-manager@example.test", "Receipt Manager", "ManagerPass1234", "Manager")
	rule, err := s.st.NotificationSetting(s.ctx, notify.EventPaymentPartialReview)
	if err != nil {
		t.Fatal(err)
	}
	rule.IncludeRequester = false
	rule.IncludeManager = true
	rule.EmailEnabled = false
	if err := s.st.SetNotificationSetting(s.ctx, admin, rule); err != nil {
		t.Fatal(err)
	}
	reqID := s.seedApprovedRequest(1, requester.ID, manager.ID, headID, 550000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	resp := s.postForm("/payments", url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-23"}, "amount": {"4000.13"}, "settlement": {"partial"}, "partial_reason": {"Agreed retention"}, "reference_no": {"AUDIENCE-13"}, "payment_mode": {"bank_transfer"}})
	requireStatus(t, resp, http.StatusSeeOther)
	body := responseBody(t, s.request(http.MethodGet, resp.Header.Get("Location"), nil, ""))
	mustContain(t, "partial receipt", body, "Payment saved. The request is with the manager", "The payment is recorded against this request.")
	mustNotContain(t, "partial receipt", body, "have been notified")
	if hasEvent(notificationTitles(t, s, requester.ID), notify.EventPaymentPartialReview) {
		t.Fatal("receipt fix changed requester audience")
	}
	if !hasEvent(notificationTitles(t, s, manager.ID), notify.EventPaymentPartialReview) {
		t.Fatal("manager did not receive configured notification")
	}
}

// PAY-D03: processing_by is retained as history after payment. It must not
// offer reservation mutation on either terminal status.
func TestPaidQueueOffersViewAndOnlyActiveReservationsOfferReassign(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("QueueTerminal")
	colleague := s.seedRoleUser("queue-owner@example.test", "Queue Owner", "OwnerPass1234", "Accounts")
	for seq, status := range []string{"completed", "completed_partial"} {
		id := s.seedApprovedRequest(seq+1, admin.ID, admin.ID, headID, 12345)
		if _, err := s.st.DB().Exec(`UPDATE payment_requests SET status=?,processing_by=?,processing_at=CURRENT_TIMESTAMP WHERE id=?`, status, colleague.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	active := s.seedApprovedRequest(3, admin.ID, admin.ID, headID, 45678)
	if err := s.st.ReserveRequest(s.ctx, colleague, active); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	paid := responseBody(t, s.request(http.MethodGet, "/accounts-queue?tab=paid", nil, ""))
	mustContain(t, "paid queue", paid, "PR-2026-000001", "PR-2026-000002", ">View</a>")
	mustNotContain(t, "paid queue", paid, ">Reassign</a>", ">Resume</a>")
	processing := responseBody(t, s.request(http.MethodGet, "/accounts-queue?tab=processing", nil, ""))
	mustContain(t, "processing queue", processing, strconvPath(`href="/requests/%d/reservation">Reassign</a>`, active))
}

// PAY-D04: recreate a stale confirmation across a real reassignment. The
// rejected POST must show the current holder, retain the accountant's inputs,
// remove the mutation affordance, and leave both money and ownership intact.
func TestStaleSettlementConfirmationShowsCurrentHolderAndCannotResubmit(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("StaleConfirm")
	next := s.seedRoleUser("new-owner@example.test", "New Accounts Owner", "NextPass1234", "Accounts")
	id := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 12345)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", id), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(id)}, "head_id": {strconvFormat(headID)}, "amount": {"123.45"}, "paid_on": {"2026-07-23"}, "payment_mode": {"bank_transfer"}, "reference_no": {"STALE-REF-123"}, "remarks": {"Keep this processing note"}, "settlement": {"settled"}}
	preview := responseBody(t, s.postForm(strconvPath("/requests/%d/settlement-preview", id), form))
	mustContain(t, "original confirmation", preview, "Reserved by you", "Confirm and save payment")
	if err := s.st.ReassignReservation(s.ctx, admin, id, next.ID, "Shift handover", true); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []bool{false, true} {
		var body string
		if fragment {
			resp := s.postFormHX("/payments", form)
			requireStatus(t, resp, http.StatusConflict)
			body = responseBody(t, resp)
		} else {
			resp := s.postForm("/payments", form)
			requireStatus(t, resp, http.StatusConflict)
			body = responseBody(t, resp)
			mustContain(t, "retained fields", body, "STALE-REF-123", "Keep this processing note")
		}
		mustContain(t, "stale confirmation", body, "Reserved by New Accounts Owner", "Your payment was not saved", "123.45", "View request", "Back to the queue")
		mustNotContain(t, "stale confirmation", body, "Reserved by you", "Confirm and save payment", "Correct it and confirm again")
	}
	var count int
	if err := s.st.DB().QueryRow(`SELECT count(*) FROM payments WHERE request_id=?`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("stale confirmation wrote %d payments", count)
	}
	req, err := s.st.Request(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if req.ProcessingBy == nil || *req.ProcessingBy != next.ID || req.Status != "processing" {
		t.Fatalf("reservation changed: %+v", req)
	}
	s.login(next.Email, "NextPass1234")
	requireStatus(t, s.postForm("/payments", form), http.StatusSeeOther)
	if err := s.st.DB().QueryRow(`SELECT count(*) FROM payments WHERE request_id=? AND amount=12345`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("new holder could not settle exactly once: %d", count)
	}
}
