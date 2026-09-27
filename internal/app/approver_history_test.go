package app

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Approver reassignment and request history, each rule driven through the
// real routes.

// reassignForm posts the approver reassignment the sheet posts.
func (s *appTestServer) reassignForm(reqID, to int64, reason string) *http.Response {
	s.t.Helper()
	return s.postForm(fmt.Sprintf("/requests/%d/reassign-approver", reqID), url.Values{
		"manager_id": {strconvFormat(to)}, "reason": {reason},
	})
}

func (s *appTestServer) managerOf(reqID int64) int64 {
	s.t.Helper()
	req, err := s.st.Request(s.ctx, reqID)
	if err != nil {
		s.t.Fatal(err)
	}
	return req.ManagerID
}

// Reassignment is for the request's own approver handing it on, or for an
// administrator rescuing it; nobody hands a request to themselves. Otherwise any
// Manager could take another manager's request and approve it.
func TestOnlyTheCurrentApproverOrAnAdministratorMayReassignAnApproval(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Hijack")
	rita := s.seedRequester("rita@example.test", "Rita Requester", "RitaPass12345")
	mona := s.seedManager("mona@example.test", "Mona Manager", "MonaPass12345")
	max := s.seedManager("max@example.test", "Max Manager", "MaxPass12345")
	reqID := s.seedPendingRequest(6, rita.ID, mona.ID, headID, 90000)

	// Max is a Manager and not the chosen approver. The control is not offered
	// to him and the route refuses him — to himself or to anybody else.
	s.login("max@example.test", "MaxPass12345")
	body := s.get(fmt.Sprintf("/requests/%d", reqID))
	if strings.Contains(body, "reassign-approver-sheet") {
		t.Fatalf("a manager who is not the approver is offered the reassign control:\n%s", body)
	}
	resp := s.reassignForm(reqID, max.ID, "I will take it")
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
	resp = s.reassignForm(reqID, admin.ID, "Send it up")
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
	if got := s.managerOf(reqID); got != mona.ID {
		t.Fatalf("a refused reassignment moved the approver to %d", got)
	}
	// And so he still cannot decide it.
	resp = s.submitScreen(fmt.Sprintf("/requests/%d/approve", reqID), url.Values{"approved_amount": {"900.00"}})
	requireStatus(t, resp, http.StatusNotFound)
	_ = responseBody(t, resp)

	// Mona, the request's own approver, sees the control and may hand it on —
	// to somebody else. Her own name is not a destination.
	s.login("mona@example.test", "MonaPass12345")
	body = s.get(fmt.Sprintf("/requests/%d", reqID))
	if !strings.Contains(body, "reassign-approver-sheet") {
		t.Fatalf("the request's own approver is not offered the reassign control:\n%s", body)
	}
	resp = s.reassignForm(reqID, mona.ID, "Keeping it")
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)
	requireStatus(t, s.reassignForm(reqID, max.ID, "Away next week"), http.StatusSeeOther)
	if got := s.managerOf(reqID); got != max.ID {
		t.Fatalf("manager after the approver's own handover = %d, want %d", got, max.ID)
	}

	// An administrator (user:edit — the grant that deactivates a person is the
	// grant that rescues what they leave behind) may reassign any request, and
	// is held to the same rule about themselves.
	s.loginAdmin()
	resp = s.reassignForm(reqID, admin.ID, "I will decide it")
	requireStatus(t, resp, http.StatusBadRequest)
	if body := responseBody(t, resp); !strings.Contains(body, "yourself") {
		t.Fatalf("the refusal does not say the target is the actor: %s", body)
	}
	requireStatus(t, s.reassignForm(reqID, mona.ID, "Max is on leave"), http.StatusSeeOther)
	if got := s.managerOf(reqID); got != mona.ID {
		t.Fatalf("manager after the administrator's reassignment = %d, want %d", got, mona.ID)
	}
}

// The deactivation warning lists every request that waits on the leaver —
// pending, returned, cancellation_requested and partial_review — and tells the
// administrator to reassign each one, so reassignment must work for all four
// statuses or those requests stay stuck.
func TestReassignmentRescuesEveryStatusTheDeactivationWarningLists(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Stuck")
	requester := s.seedRequester("stuck.req@example.test", "Stuck Requester", "StuckPass12345")
	leaver := s.seedManager("leaver@example.test", "Leaving Approver", "LeaverPass12345")
	rescuer := s.seedManager("rescuer@example.test", "Rescuing Approver", "RescuerPass12345")

	cancelID := s.seedApprovedRequest(971, requester.ID, leaver.ID, headID, 90000)
	s.login("stuck.req@example.test", "StuckPass12345")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/cancel-request", cancelID), url.Values{"reason": {"Ordered twice"}}), http.StatusSeeOther)

	partialID := s.seedApprovedRequest(972, requester.ID, leaver.ID, headID, 9500000)
	s.loginAdmin()
	s.seedPartialReview(partialID, headID, "Vendor short-shipped")

	returnedID := s.seedPendingRequest(973, requester.ID, leaver.ID, headID, 90000)
	s.login("leaver@example.test", "LeaverPass12345")
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/return", returnedID), url.Values{"comment": {"Attach the bill"}}), http.StatusSeeOther)

	// The warning names all three, and the administrator confirms anyway.
	s.loginAdmin()
	form := url.Values{
		"id": {fmt.Sprint(leaver.ID)}, "email": {leaver.Email}, "name": {leaver.Name},
		"role": {"data_entry"}, "default_approver_id": {"0"},
		"role_ids": {s.roleIDByName(t, "Requester"), s.roleIDByName(t, "Manager")},
	}
	body := responseBody(t, s.postForm("/users", form))
	for _, want := range []string{"PR-2026-000971", "PR-2026-000972", "PR-2026-000973", "Reassign approval"} {
		if !strings.Contains(body, want) {
			t.Errorf("the deactivation warning lacks %q", want)
		}
	}
	form.Set("confirm", "on")
	requireStatus(t, s.postForm("/users", form), http.StatusSeeOther)

	// Every listed request offers the way out the warning promised, and it works.
	for _, id := range []int64{cancelID, partialID, returnedID} {
		body := s.get(fmt.Sprintf("/requests/%d", id))
		if !strings.Contains(body, "reassign-approver-sheet") {
			t.Errorf("request %d offers no Reassign approval control to the administrator", id)
		}
		resp := s.reassignForm(id, rescuer.ID, "The approver was deactivated")
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("reassigning request %d = %d, body %s", id, resp.StatusCode, responseBody(t, resp))
		}
		if got := s.managerOf(id); got != rescuer.ID {
			t.Fatalf("request %d manager = %d, want the rescuer %d", id, got, rescuer.ID)
		}
	}
	// The returned request keeps the correction the requester is reading.
	returned, err := s.st.Request(s.ctx, returnedID)
	if err != nil {
		t.Fatal(err)
	}
	if returned.Status != "returned" || returned.DecisionReason != "Attach the bill" {
		t.Fatalf("returned request after reassignment: status=%q reason=%q", returned.Status, returned.DecisionReason)
	}

	// And the rescuer can actually decide what the leaver was holding.
	s.login("rescuer@example.test", "RescuerPass12345")
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/cancellation", cancelID), url.Values{"decision": {"accept"}}), http.StatusSeeOther)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/accept-partial", partialID), url.Values{"note": {"Fine"}}), http.StatusSeeOther)
	for id, want := range map[int64]string{cancelID: "cancelled", partialID: "completed_partial"} {
		req, err := s.st.Request(s.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if req.Status != want {
			t.Fatalf("request %d status = %q, want %q", id, req.Status, want)
		}
	}
}

// The history's change chip for an approver change names people, not the user
// ids the audit log stores.
func TestHistoryNamesTheApproversAChangeMovedBetween(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Names")
	rita := s.seedRequester("rita@example.test", "Rita Requester", "RitaPass12345")
	mona := s.seedManager("mona@example.test", "Mona Manager", "MonaPass12345")
	max := s.seedManager("max@example.test", "Max Manager", "MaxPass12345")
	reqID := s.seedPendingRequest(4, rita.ID, mona.ID, headID, 90000)

	s.loginAdmin()
	requireStatus(t, s.reassignForm(reqID, max.ID, "Mona on leave"), http.StatusSeeOther)

	s.login("rita@example.test", "RitaPass12345")
	body := s.get(fmt.Sprintf("/requests/%d", reqID))
	want := `Approver <span class="was">Mona Manager</span> → <span class="now">Max Manager</span>`
	if !strings.Contains(body, want) {
		chip := between(t, body, `<div class="tl-change">`, `</div>`)
		t.Fatalf("the change chip does not name the approvers: %s", chip)
	}
}

// The edit form's banner and submit button name the approver currently chosen
// in the select, not the one the request had when the page rendered.
func TestEditFormLabelsFollowTheChosenApprover(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Follow")
	rita := s.seedRequester("rita@example.test", "Rita Requester", "RitaPass12345")
	mona := s.seedManager("mona@example.test", "Mona Manager", "MonaPass12345")
	reqID := s.seedPendingRequest(5, rita.ID, mona.ID, headID, 90000)

	s.login("rita@example.test", "RitaPass12345")
	body := s.get(fmt.Sprintf("/requests/%d/edit", reqID))
	for _, want := range []string{
		`data-follows-select="apr" data-follows-text="Save and notify {name}">Save and notify Mona Manager</button>`,
		`data-follows-select="apr" data-follows-text="Editing tells {name} again">Editing tells Mona Manager again</b>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form lacks a label that follows the approver select: %s", want)
		}
	}
}

// The detail thread's event titles are the audit summaries, and every one names
// its actor, the Accounts-side events ("Hold lifted", "Reserved request for
// processing") included.
func TestDetailThreadNamesTheActorForAccountsEvents(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Actor")
	rita := s.seedRequester("rita@example.test", "Rita Requester", "RitaPass12345")
	aarav := s.seedRequester("aarav@example.test", "Aarav Accounts", "AaravPass12345")
	s.assignRole(aarav.ID, "Accounts")
	settledID := s.seedApprovedRequest(1, rita.ID, admin.ID, headID, 100000)
	partialID := s.seedApprovedRequest(2, rita.ID, admin.ID, headID, 9500000)

	s.login("aarav@example.test", "AaravPass12345")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", settledID), url.Values{"reason": {"Checking GST"}}), http.StatusSeeOther)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/unhold", settledID), url.Values{}), http.StatusSeeOther)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", settledID), url.Values{}), http.StatusSeeOther)
	requireStatus(t, s.postPayment(url.Values{"request_id": {strconvFormat(settledID)}, "head_id": {strconvFormat(headID)},
		"paid_on": {"2026-07-23"}, "amount": {"1000.00"}, "vendor_payee": {"Acme Landlord"}, "settlement": {"settled"}, "reference_no": {"ACTOR-PAYMENT-REF"}}), http.StatusSeeOther)
	s.seedPartialReview(partialID, headID, "Vendor short-shipped")
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/accept-partial", partialID), url.Values{"note": {"fine"}}), http.StatusSeeOther)

	s.login("rita@example.test", "RitaPass12345")
	settled := s.get(fmt.Sprintf("/requests/%d", settledID))
	for _, want := range []string{
		"<b>Aarav Accounts put the request on hold: Checking GST</b>",
		"<b>Aarav Accounts lifted the hold</b>",
		"<b>Aarav Accounts reserved the request for processing</b>",
		"<b>Aarav Accounts settled the request as completed</b>",
	} {
		if !strings.Contains(settled, want) {
			t.Errorf("settled request's thread lacks %q", want)
		}
	}
	partial := s.get(fmt.Sprintf("/requests/%d", partialID))
	for _, want := range []string{
		"<b>Aarav Accounts recorded a partial settlement: Vendor short-shipped</b>",
		"<b>" + admin.Name + " accepted the partial settlement — completed, partial accepted: fine</b>",
	} {
		if !strings.Contains(partial, want) {
			t.Errorf("partial request's thread lacks %q", want)
		}
	}
	for _, stale := range []string{"<b>Hold lifted</b>", "<b>Reserved request for processing</b>", "<b>Settled request as completed</b>"} {
		if strings.Contains(settled, stale) {
			t.Errorf("thread still carries the actorless line %q", stale)
		}
	}
}

// Illegal-transition refusals read as sentences: no raw status codes and no
// fixed "a" (never "a approved request cannot be withdrawn").
func TestIllegalTransitionRefusalsReadAsSentences(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Copy")
	rita := s.seedRequester("rita@example.test", "Rita Requester", "RitaPass12345")
	approvedID := s.seedApprovedRequest(1, rita.ID, admin.ID, headID, 100000)
	partialID := s.seedApprovedRequest(2, rita.ID, admin.ID, headID, 9500000)
	s.loginAdmin()
	s.seedPartialReview(partialID, headID, "Vendor short-shipped")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/accept-partial", partialID), url.Values{}), http.StatusSeeOther)

	refusal := func(path string, form url.Values) string {
		t.Helper()
		resp := s.submitScreen(path, form)
		requireStatus(t, resp, http.StatusBadRequest)
		return responseBody(t, resp)
	}
	cases := []struct{ path, want string }{
		{fmt.Sprintf("/requests/%d/approve", approvedID), "an approved request cannot be approved"},
		{fmt.Sprintf("/requests/%d/return", approvedID), "an approved request cannot be returned"},
		{fmt.Sprintf("/requests/%d/return", partialID), "a completed (partial accepted) request cannot be returned"},
		{fmt.Sprintf("/requests/%d/cancel", partialID), "a completed (partial accepted) request cannot be cancelled"},
	}
	for _, tc := range cases {
		body := refusal(tc.path, url.Values{"approved_amount": {"1000.00"}, "comment": {"x"}, "reason": {"x"}})
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: refusal lacks %q:\n%s", tc.path, tc.want, between(t, body, "<main", "</main>"))
		}
	}
	s.login("rita@example.test", "RitaPass12345")
	body := refusal(fmt.Sprintf("/requests/%d/withdraw", approvedID), url.Values{})
	if !strings.Contains(body, "an approved request cannot be withdrawn") {
		t.Errorf("withdraw refusal: %s", between(t, body, "<main", "</main>"))
	}
	body = refusal(fmt.Sprintf("/requests/%d/cancel-request", partialID), url.Values{"reason": {"x"}})
	if !strings.Contains(body, "a completed (partial accepted) request cannot be sent for cancellation") {
		t.Errorf("cancel-request refusal: %s", between(t, body, "<main", "</main>"))
	}
}

// While a request is on hold the requester may add a comment or a document,
// to answer the question the hold asks.
func TestRequesterCanAddADocumentWhileTheRequestIsOnHold(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Held")
	rita := s.seedRequester("rita@example.test", "Rita Requester", "RitaPass12345")
	s.seedRequester("other@example.test", "Other Requester", "OtherPass12345")
	aarav := s.seedRequester("aarav@example.test", "Aarav Accounts", "AaravPass12345")
	s.assignRole(aarav.ID, "Accounts")
	reqID := s.seedApprovedRequest(4, rita.ID, admin.ID, headID, 100000)
	closedID := s.seedApprovedRequest(5, rita.ID, admin.ID, headID, 100000)
	openID := s.seedApprovedRequest(6, rita.ID, admin.ID, headID, 100000)
	pendingID := s.seedApprovedRequest(7, rita.ID, admin.ID, headID, 100000)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET status='pending', approved_amount=NULL WHERE id=?`, pendingID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET status='cancelled' WHERE id=?`, closedID); err != nil {
		t.Fatal(err)
	}
	s.login("aarav@example.test", "AaravPass12345")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", reqID), url.Values{"reason": {"Need GST receipt"}}), http.StatusSeeOther)

	upload := func(id int64, name string) *http.Response {
		t.Helper()
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if err := mw.WriteField("csrf", s.csrf()); err != nil {
			t.Fatal(err)
		}
		part, err := mw.CreateFormFile("attachment", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(validTestPDF)); err != nil {
			t.Fatal(err)
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		return s.request(http.MethodPost, fmt.Sprintf("/requests/%d/attachments", id), &buf, mw.FormDataContentType())
	}

	// The requester sees the upload beside the hold and it works.
	s.login("rita@example.test", "RitaPass12345")
	body := s.get(fmt.Sprintf("/requests/%d", reqID))
	if !strings.Contains(body, fmt.Sprintf(`action="/requests/%d/attachments"`, reqID)) || !strings.Contains(body, `type="file"`) {
		t.Fatalf("the held request offers the requester no way to add a document:\n%s", between(t, body, "<main", "</main>"))
	}
	resp := upload(reqID, "gst-receipt.pdf")
	requireStatus(t, resp, http.StatusSeeOther)
	atts, err := s.st.RequestAttachments(s.ctx, reqID)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 1 || atts[0].OriginalName != "gst-receipt.pdf" || atts[0].UploadedBy != rita.ID {
		t.Fatalf("attachments after the upload: %+v", atts)
	}
	body = s.get(fmt.Sprintf("/requests/%d", reqID))
	if !strings.Contains(body, "gst-receipt.pdf") {
		t.Fatalf("the uploaded document is not shown on the request")
	}
	// Nothing about the approved fields moved.
	req, err := s.st.Request(s.ctx, reqID)
	if err != nil {
		t.Fatal(err)
	}
	if req.Status != "approved" || !req.OnHold || req.Amount != 100000 {
		t.Fatalf("the upload changed the request: %+v", req)
	}
	// A closed request takes no more documents.
	resp = upload(closedID, "late.pdf")
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)
	// Nor does one that is not on hold: the door exists for the hold's question
	// and for nothing else, so an approved request Accounts has not paused, and
	// a pending one the edit form still serves, refuse it — and neither offers
	// the control.
	for _, id := range []int64{openID, pendingID} {
		body := s.get(fmt.Sprintf("/requests/%d", id))
		if strings.Contains(body, fmt.Sprintf(`action="/requests/%d/attachments"`, id)) {
			t.Fatalf("request %d is not on hold but offers the upload:\n%s", id, between(t, body, "<main", "</main>"))
		}
		resp = upload(id, "early.pdf")
		requireStatus(t, resp, http.StatusBadRequest)
		if body := responseBody(t, resp); !strings.Contains(body, "not on hold") {
			t.Fatalf("the refusal does not say the request is not on hold:\n%s", between(t, body, "<main", "</main>"))
		}
		if atts, err := s.st.RequestAttachments(s.ctx, id); err != nil || len(atts) != 0 {
			t.Fatalf("request %d took a document while not on hold: %+v %v", id, atts, err)
		}
	}

	// Somebody who is not the requester is refused: outside their scope it is
	// 404, and a colleague who can read it still may not plant a document on it.
	s.login("other@example.test", "OtherPass12345")
	resp = upload(reqID, "planted.pdf")
	requireStatus(t, resp, http.StatusNotFound)
	_ = responseBody(t, resp)
	s.login("aarav@example.test", "AaravPass12345")
	resp = upload(reqID, "planted.pdf")
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
	atts, err = s.st.RequestAttachments(s.ctx, reqID)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 1 {
		t.Fatalf("a stranger planted a document: %+v", atts)
	}
}
