package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/notify"
)

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
	s.loginAdmin()
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/reject", rejected),
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
	s.loginAdmin()
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
		url.Values{"to_user_id": {strconvFormat(colleague.ID)}, "reason": {"Off sick"}, "confirm": {"on"}}), http.StatusSeeOther)
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
	s.loginAdmin()
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
		s.loginAdmin()
		requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/cancellation", id),
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

	s.loginAdmin()
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/return", id),
		url.Values{"comment": {"Add the GST breakup"}}), http.StatusSeeOther)

	edit := url.Values{"type": {"reimbursement"}, "treatment": {"budget"},
		"short_title": {"Site visit"}, "project_id": {strconvFormat(projectOf(t, s, headID))},
		"head_id": {strconvFormat(headID)}, "amount": {"1,900.00"},
		"purpose":      {"flights and cabs, with the GST split out"},
		"expense_date": {"2026-07-01"}, "manager_id": {strconvFormat(admin.ID)}}

	s.login("saver@example.test", "SavePass12345")
	saveOnly := edit
	saveOnly.Set("submit_action", "save")
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/edit", id), saveOnly), http.StatusSeeOther)
	if hasEvent(notificationTitles(t, s, admin.ID), notify.EventRequestEdited) {
		t.Fatal("Save corrections told the approver the request was re-sent for approval")
	}

	// The resubmit path still fires, because there the claim is true.
	resubmit := edit
	resubmit.Set("submit_action", "resubmit")
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/edit", id), resubmit), http.StatusSeeOther)
	if !hasEvent(notificationTitles(t, s, admin.ID), notify.EventRequestEdited) {
		t.Fatal("Resubmit for approval no longer tells the approver at all")
	}
}

// notifications-1 — "Edit every field" on a returned request opens the full
// edit form, whose banner says editing notifies the approver and whose button
// reads "Save and notify <approver>". That button sent no submit_action, so it
// saved without resubmitting: the request stayed returned and nobody was told.
// It now resubmits, as its label promises, and the approver is told.
func TestFullEditOfAReturnedRequestResubmitsAndNotifies(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Full edit returned")
	requester := s.seedRequester("fulledit@example.test", "Full Editor", "FullEdit12345")
	approver := s.seedRequester("fullapprover@example.test", "Apra Approver", "FullAppr12345")
	s.assignRole(approver.ID, "Manager")
	id := s.seedPendingRequest(923, requester.ID, approver.ID, headID, 180000)

	s.login("fullapprover@example.test", "FullAppr12345")
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/return", id),
		url.Values{"comment": {"Add GST split"}}), http.StatusSeeOther)

	s.login("fulledit@example.test", "FullEdit12345")
	page := s.get(fmt.Sprintf("/requests/%d/edit", id))
	// The same button also follows the approver select (rbac-10), so the
	// markup carries both: the resubmit action and the follow-select hooks.
	button := `<button class="btn primary" type="submit" data-follows-select="apr" data-follows-text="Save and notify {name}" name="submit_action" value="resubmit">Save and notify Apra Approver</button>`
	if !strings.Contains(page, button) {
		t.Fatalf("the full edit form's button does not resubmit; want %s", button)
	}
	// Post exactly what that form sends: its fields plus the button's value.
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/edit", id), url.Values{
		"type": {"reimbursement"}, "treatment": {"budget"},
		"short_title": {"Site visit"}, "project_id": {strconvFormat(projectOf(t, s, headID))},
		"head_id": {strconvFormat(headID)}, "amount": {"1,800.00"},
		"purpose":      {"flights and cabs, with the GST split out"},
		"expense_date": {"2026-07-01"}, "manager_id": {strconvFormat(approver.ID)},
		"submit_action": {"resubmit"},
	}), http.StatusSeeOther)
	var status string
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT status FROM payment_requests WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("status after Save and notify = %q, want pending (back with the approver)", status)
	}
	if !hasEvent(notificationTitles(t, s, approver.ID), notify.EventRequestEdited) {
		t.Fatal("Save and notify did not notify the approver")
	}

	// Editing a pending request still saves in place: no submit_action there,
	// and it notifies as TestEditingAPendingRequestReNotifiesTheApprover pins.
	page = s.get(fmt.Sprintf("/requests/%d/edit", id))
	if !strings.Contains(page, `<button class="btn primary" type="submit" data-follows-select="apr" data-follows-text="Save and notify {name}">Save and notify Apra Approver</button>`) {
		t.Fatal("the pending edit form's button changed")
	}
}

// A8 (coverage matrix) — editing a request that is still *pending* re-notifies
// the approver: the request is already in their queue and the figures they are
// about to decide on have changed. The approver here is a plain Manager, not
// the bootstrap admin, so no other role's rule can satisfy the check, and the
// edit goes through the real /edit route with no resubmit button pressed.
func TestEditingAPendingRequestReNotifiesTheApprover(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Pending edit")
	requester := s.seedRequester("pendingeditor@example.test", "Pending Editor", "PendingEdit12345")
	approver := s.seedRequester("pendingapprover@example.test", "Pending Approver", "PendingAppr12345")
	s.assignRole(approver.ID, "Manager")
	id := s.seedPendingRequest(922, requester.ID, approver.ID, headID, 180000)

	if hasEvent(notificationTitles(t, s, approver.ID), notify.EventRequestEdited) {
		t.Fatal("precondition: the approver already has a request_edited row before any edit")
	}

	s.login("pendingeditor@example.test", "PendingEdit12345")
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/edit", id), url.Values{
		"type": {"reimbursement"}, "treatment": {"budget"},
		"short_title": {"Site visit"}, "project_id": {strconvFormat(projectOf(t, s, headID))},
		"head_id": {strconvFormat(headID)}, "amount": {"2,400.00"},
		"purpose":      {"flights, cabs and a second night's hotel"},
		"expense_date": {"2026-07-01"}, "manager_id": {strconvFormat(approver.ID)},
	}), http.StatusSeeOther)

	var status string
	var amount int64
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT status, amount FROM payment_requests WHERE id=?`, id).Scan(&status, &amount); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || amount != 240000 {
		t.Fatalf("the edit did not land as a pending edit: status=%q amount=%d", status, amount)
	}
	if !hasEvent(notificationTitles(t, s, approver.ID), notify.EventRequestEdited) {
		t.Fatal("a pending request was edited and its approver was never re-notified")
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

	s.loginAdmin()
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/return", pending),
		url.Values{"comment": {"Add the invoice"}}), http.StatusSeeOther)
	reserved := s.seedApprovedRequest(992, requester.ID, admin.ID, headID, 100000)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reserved), url.Values{}), http.StatusSeeOther)
	stale := s.get(fmt.Sprintf("/requests/%d/reservation/stale", reserved))
	if !strings.Contains(stale, "open 3 days") {
		t.Errorf("the stale banner does not read reminder_stale_days: %s", firstLines(stale))
	}
	if strings.Contains(stale, "one-day mark") || strings.Contains(stale, "calendar day") {
		t.Error("the stale banner still asserts a hardcoded, calendar-day threshold")
	}
	// copy-1: the Configuration note under these same three fields still said
	// "calendar days" after decision 2 made the thresholds elapsed days.
	config := s.get("/configuration")
	if strings.Contains(config, "calendar day") {
		t.Error("the Configuration reminders note still says calendar days")
	}
	if !strings.Contains(config, "Reminders are counted in days") {
		t.Errorf("the Configuration reminders note is missing: %s", firstLines(config))
	}

	s.login("copy@example.test", "CopyPass12345")
	form := s.get("/requests/new?type=vendor_invoice")
	if !strings.Contains(form, "nothing happens for 7 days") {
		t.Errorf("the new-request hint does not read reminder_pending_days: %s", firstLines(form))
	}
	returned := s.get(fmt.Sprintf("/requests/%d/edit", pending))
	if !strings.Contains(returned, "7-day reminder clock") {
		t.Errorf("the correction screen does not read reminder_pending_days: %s", firstLines(returned))
	}
	for _, body := range []string{form, returned, stale} {
		if strings.Contains(body, "three calendar days") || strings.Contains(body, "three-day") {
			t.Error("a screen still hardcodes three calendar days")
		}
	}
}

// The two events the repair's own documentation pass found still firing nothing.
//
// Both are the fault F-F-06 was about — a transition that changes what a specific
// other person must do next, telling nobody — and the first is worse than silence:
// the reassignment sheet states that "The new approver is told", so the route Wave
// 3 built to make reassignment possible at all was shipping a false promise.
func TestReassignmentAndOutrightCancellationNotifyTheirSubject(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Reassign")
	requester := s.seedRequester("reassignee@example.test", "Reassign Requester", "ReassignPass12345")

	// A second approver to hand the request to. Manager alone, not Manager +
	// Accounts, so "who was told" cannot be satisfied by some other role's rule.
	second := s.seedRequester("secondapprover@example.test", "Second Approver", "SecondPass12345")
	s.assignRole(second.ID, "Manager")

	id := s.seedPendingRequest(931, requester.ID, admin.ID, headID, 140000)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/reassign-approver", id), url.Values{
		"manager_id": {fmt.Sprint(second.ID)},
		"reason":     {"Original approver is on leave"},
	}), http.StatusSeeOther)

	if !hasEvent(notificationTitles(t, s, second.ID), notify.EventApproverReassigned) {
		t.Error("the sheet promises the new approver is told, and they were not")
	}
	// The person it must NOT go to: the approver it was taken away from has
	// nothing left to do, and IncludeManager reads the row after the store moved
	// it, so a row here would mean the event fired against the stale manager_id.
	if hasEvent(notificationTitles(t, s, admin.ID), notify.EventApproverReassigned) {
		t.Error("the outgoing approver was told to approve a request that is no longer theirs")
	}

	// Cancelling outright is the fourth cancellation path and the only one nobody
	// asked for, which makes it the one the requester is least able to guess at.
	// It is legal only from `approved` (legalTransitions, internal/store/requests.go:191),
	// so the request is approved first — this is the manager killing money they
	// had already agreed to, which is exactly why the requester needs telling.
	cancelled := s.seedPendingRequest(932, requester.ID, admin.ID, headID, 150000)
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/approve", cancelled),
		url.Values{"approved_amount": {"1500.00"}}), http.StatusSeeOther)
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/cancel", cancelled), url.Values{
		"reason": {"Duplicate of PR-2026-000931"},
	}), http.StatusSeeOther)

	if !hasEvent(notificationTitles(t, s, requester.ID), notify.EventRequestCancelled) {
		t.Error("the approver cancelled the request outright and the requester was never told")
	}
}

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
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	resp := s.postPayment(url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-23"}, "amount": {"4000.13"}, "settlement": {"partial"}, "partial_reason": {"Agreed retention"}, "reference_no": {"AUDIENCE-13"}, "payment_mode": {"bank_transfer"}})
	requireStatus(t, resp, http.StatusSeeOther)
	body := s.get(resp.Header.Get("Location"))
	mustContain(t, "partial receipt", body, "Payment saved. The request is with the manager", "The payment is recorded against this request.")
	mustNotContain(t, "partial receipt", body, "have been notified")
	if hasEvent(notificationTitles(t, s, requester.ID), notify.EventPaymentPartialReview) {
		t.Fatal("receipt fix changed requester audience")
	}
	if !hasEvent(notificationTitles(t, s, manager.ID), notify.EventPaymentPartialReview) {
		t.Fatal("manager did not receive configured notification")
	}
}
