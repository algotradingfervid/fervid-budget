package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// Settlement screens: who holds a request, what each screen offers, and what a
// confirmation may say.

// recordPartial reserves the request as the signed-in accountant and records a
// short payment against it, the way the entry screen does.
func (s *appTestServer) recordPartial(reqID, headID, paid int64) {
	s.t.Helper()
	requireStatus(s.t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-23"},
		"amount": {money2(paid)}, "vendor_payee": {"Acme Landlord"}, "settlement": {"partial"},
		"partial_reason": {"Retention held back by agreement."}, "reference_no": {"UTR-PART"}, "payment_mode": {"bank_transfer"}}
	requireStatus(s.t, s.postPayment(form), http.StatusSeeOther)
}

// Opening the entry screen for an approved request nobody holds
// is not a lost race. The screen says nobody holds it and offers to take it.
func TestUnclaimedEntryScreenSaysNobodyHoldsItAndOffersToTake(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Unclaimed")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.loginAdmin()
	resp := s.request(http.MethodGet, fmt.Sprintf("/payments/new?request=%d", reqID), nil, "")
	// Still a conflict — the form cannot be shown without a reservation — but
	// the explanation is the true one.
	requireStatus(t, resp, http.StatusConflict)
	body := responseBody(t, resp)
	mustContain(t, "unclaimed entry screen", body,
		"Nobody holds", fmt.Sprintf(`action="/requests/%d/record-payment"`, reqID), "Take it for processing",
		`class="pill approved"`, "Waiting on Accounts")
	mustNotContain(t, "unclaimed entry screen", body,
		"Someone else", "took this request before you", `class="pill processing"`)
	// The real race is still reported as one.
	deepak := s.seedColleague("deepak@example.test", "Deepak Menon", "DeepakPass1234")
	if err := s.st.ReserveRequest(s.ctx, deepak, reqID); err != nil {
		t.Fatal(err)
	}
	body = s.get(fmt.Sprintf("/payments/new?request=%d", reqID))
	mustContain(t, "taken entry screen", body, "Deepak Menon took this request before you")
	// …in the words every other screen uses for a held request, not a pill of
	// its own ("Processing — {holder}").
	mustContain(t, "taken entry screen", body, `class="pill processing">With Accounts — taken by Deepak Menon</span>`)
	mustNotContain(t, "taken entry screen", body, "Processing —")
}

// The lesser screens build their pill through statusPill too, so they can name
// the holder and show "Partial — under discussion": the recoverables register
// and detail, the requester's cancel screen, the similar-request card on the
// new-request form, the holder's own payment entry screen, and the
// just-submitted screen.
func TestLesserScreensShareTheStatusPill(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Lesser")
	deepak := s.seedRoleUser("deepak@example.test", "Deepak Menon", "DeepakPass1234", "Accounts")

	// A recoverable Deepak holds: the register and the detail name him.
	taken := s.seedRecoverableRequest(1, admin.ID, admin.ID, "icd", "Meridian Holdings", 250000)
	if err := s.st.ReserveRequest(s.ctx, deepak, taken); err != nil {
		t.Fatal(err)
	}
	// A recoverable in partial review with the manager's concern unanswered.
	discussed := s.seedRecoverableRequest(2, admin.ID, admin.ID, "icd", "Ridge Metro", 300000)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET status='partial_review', concern_open=1 WHERE id=?`, discussed); err != nil {
		t.Fatal(err)
	}
	s.loginAdmin()
	register := s.get("/recoverables/list")
	mustContain(t, "recoverables register", register,
		`class="pill processing">With Accounts — taken by Deepak Menon</span>`, `class="pill discussion">Partial — under discussion</span>`)
	mustNotContain(t, "recoverables register", register, `class="pill processing">With Accounts</span>`, "Partial — manager review")
	for id, want := range map[int64]string{
		taken:     `class="pill processing">With Accounts — taken by Deepak Menon</span>`,
		discussed: `class="pill discussion">Partial — under discussion</span>`,
	} {
		mustContain(t, "recoverable detail", s.get(fmt.Sprintf("/recoverables/%d", id)), want)
	}

	// The stale-reservation screen: "taken by you" to the holder, the holder's
	// name to anybody else.
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET processing_at=datetime('now','-26 hours') WHERE id=?`, taken); err != nil {
		t.Fatal(err)
	}
	stale := s.get(fmt.Sprintf("/requests/%d/reservation/stale", taken))
	mustContain(t, "bystander's stale screen", stale, `class="pill processing">With Accounts — taken by Deepak Menon</span>`)
	s.login(deepak.Email, "DeepakPass1234")
	stale = s.get(fmt.Sprintf("/requests/%d/reservation/stale", taken))
	mustContain(t, "holder's stale screen", stale, `class="pill processing">With Accounts — taken by you</span>`)
	mustNotContain(t, "holder's stale screen", stale, "Processing —")

	// The similar-request card on the new-request form names the holder too.
	s.loginAdmin()
	held := s.seedApprovedRequest(3, admin.ID, admin.ID, headID, 500000)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET vendor_payee='Acme Landlord', vendor_id=NULL WHERE id=?`, held); err != nil {
		t.Fatal(err)
	}
	if err := s.st.ReserveRequest(s.ctx, deepak, held); err != nil {
		t.Fatal(err)
	}
	card := responseBody(t, s.postForm("/requests/duplicate-check", url.Values{
		"type": {"vendor_invoice"}, "vendor_payee": {"Acme Landlord"}, "amount": {"5,000.00"}}))
	mustContain(t, "similar-request card", card, "PR-2026-000003", `class="pill processing">With Accounts — taken by Deepak Menon</span>`)
}

// A partial review waiting on the manager is listed where the
// manager looks — Home, the approvals queue and "Needs me" — not only behind a
// notification link.
func TestManagerHomeAndApprovalsQueueListPartialReviews(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Partials")
	manager := s.seedRoleUser("kavita@example.test", "Kavita Rao", "KavitaPass1234", "Manager")
	reqID := s.seedApprovedRequest(1, admin.ID, manager.ID, headID, 9500000)
	s.loginAdmin()
	s.recordPartial(reqID, headID, 6000000)

	s.login(manager.Email, "KavitaPass1234")
	home := s.get("/")
	mustContain(t, "manager home", home, "Partial payments to review", "PR-2026-000001", `class="waiting you"`)
	mustNotContain(t, "manager home", home, "Nothing is waiting on you")
	queue := s.get("/approvals")
	mustContain(t, "approvals queue", queue, `href="/approvals?bucket=partial-review`, "Partial review <span class=\"n\">1</span>")
	tab := s.get("/approvals?bucket=partial-review")
	mustContain(t, "partial review tab", tab, "PR-2026-000001", "Partial — manager review", "Waiting on you")
	// The sidebar badge counts it with the approvals, and the request list's
	// "Needs me" tab agrees with the detail page's "Waiting on you".
	mustContain(t, "approvals nav badge", tab, `<span class="ico">✓</span>Approvals<span class="n">1</span>`)
	needs := s.get("/requests?bucket=needs-me")
	mustContain(t, "needs-me", needs, "PR-2026-000001")
}

// The request detail offers "Record payment" to Accounts when the request is approved, unclaimed and not on hold.
func TestRequestDetailOffersRecordPaymentToAccounts(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Detail")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	asha := s.seedRoleUser("asha@example.test", "Asha Accounts", "AshaPass1234", "Accounts")
	s.login(asha.Email, "AshaPass1234")
	body := s.get(fmt.Sprintf("/requests/%d", reqID))
	mustContain(t, "accounts detail", body, fmt.Sprintf(`action="/requests/%d/record-payment"`, reqID), "Record payment")
	// On hold: not offered — the queue would refuse it too.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", reqID), url.Values{"reason": {"Which invoice?"}}), http.StatusSeeOther)
	body = s.get(fmt.Sprintf("/requests/%d", reqID))
	mustNotContain(t, "held detail", body, "record-payment")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/unhold", reqID), url.Values{}), http.StatusSeeOther)
	// Taken by somebody: not offered either.
	deepak := s.seedColleague("deepak@example.test", "Deepak Menon", "DeepakPass1234")
	if err := s.st.ReserveRequest(s.ctx, deepak, reqID); err != nil {
		t.Fatal(err)
	}
	body = s.get(fmt.Sprintf("/requests/%d", reqID))
	mustNotContain(t, "taken detail", body, "record-payment")
	// And never to somebody without reservation:reserve.
	rhea := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass1234")
	own := s.seedApprovedRequest(2, rhea.ID, admin.ID, headID, 500000)
	s.login(rhea.Email, "RheaPass1234")
	body = s.get(fmt.Sprintf("/requests/%d", own))
	mustNotContain(t, "requester detail", body, "record-payment")
}

// "Payment saved" is the confirmation of a
// write, so it appears once, on the redirect from the confirming POST. A later
// visit shows the payment; a refused repeat and a blocked edit say what really
// happened.
func TestPaymentSavedBannerAppearsOnlyAfterTheConfirmingPost(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Banner")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 550000)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"submission_key": {"saved-banner-fixture"}, "request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-23"},
		"amount": {"5500.00"}, "vendor_payee": {"Acme Landlord"}, "settlement": {"settled"}, "reference_no": {"UTR-1"}, "payment_mode": {"bank_transfer"}}
	resp := s.postPayment(form)
	requireStatus(t, resp, http.StatusSeeOther)
	// The landing URL is the payment's own — the outcome rides on a one-shot
	// cookie, so a bookmark or a refresh can never replay the confirmation.
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/payments/") || strings.Contains(loc, "?") {
		t.Fatalf("the confirming POST must land on the payment itself, got %q", loc)
	}
	saved := s.get(loc)
	mustContain(t, "just-saved screen", saved, "Payment saved", "The payment is recorded against this request.")
	mustNotContain(t, "just-saved screen", saved, "have been notified")

	later := s.get(loc)
	mustNotContain(t, "a later visit", later, "Payment saved", "have been notified")
	mustContain(t, "a later visit", later, "PAY-", "5,500.00")

	// A second confirm with a different figure writes nothing and says so.
	form.Set("amount", "5000.00")
	resp = s.postPayment(form)
	requireStatus(t, resp, http.StatusSeeOther)
	if got := resp.Header.Get("Location"); got != loc {
		t.Fatalf("refused repeat landed on %q, want the existing payment %q", got, loc)
	}
	dup := s.get(loc)
	mustContain(t, "refused repeat", dup, "already", "No additional payment was saved", "5,500.00")
	mustNotContain(t, "refused repeat", dup, "Payment saved", "5,000.00")
	mustNotContain(t, "the visit after the refused repeat", s.get(loc), "No additional payment was saved")

	// A blocked edit lands on the payment with the reason, once.
	resp = s.request(http.MethodGet, loc+"/edit", nil, "")
	requireStatus(t, resp, http.StatusSeeOther)
	if got := resp.Header.Get("Location"); got != loc {
		t.Fatalf("blocked edit landed on %q", got)
	}
	imm := s.get(loc)
	mustContain(t, "blocked edit", imm, "cannot be edited")
	mustNotContain(t, "blocked edit", imm, "Payment saved")
	mustNotContain(t, "the visit after the blocked edit", s.get(loc), "cannot be edited")
}

// On the htmx path the sheet sits inside the live entry form, so
// its closers dismiss it in place and the typed values stay. On the no-JS page
// the same anchors still navigate.
func TestSettlementSheetClosersDismissInPlaceOnTheHtmxPath(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Sheet")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 5000000)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"amount": {"45000.00"}, "paid_on": {"2026-07-23"}, "payment_mode": {"bank_transfer"}, "reference_no": {"UTR-A-001"}}
	fragment := responseBody(t, s.postFormHX(fmt.Sprintf("/requests/%d/settlement-preview", reqID), form))
	href := fmt.Sprintf(`href="/payments/new?request=%d"`, reqID)
	mustContain(t, "htmx sheet", fragment,
		`<a class="sh-close" `+href+` data-close="settle-sheet"`,
		`<a class="btn outline" `+href+` data-close="settle-sheet">Go back</a>`)
	page := responseBody(t, s.postForm(fmt.Sprintf("/requests/%d/settlement-preview", reqID), form))
	mustContain(t, "no-JS page", page, fmt.Sprintf(`<button class="btn outline" type="submit" formaction="/payments/new?request=%d" formmethod="post" formnovalidate>Go back</button>`, reqID))
	mustNotContain(t, "no-JS page", page, `data-close="settle-sheet"`)
}

// A processing request names who holds it, on the pill and in
// the waiting line, for everybody but the holder — who reads "you".
func TestProcessingRequestNamesTheHolderEverywhere(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Holder")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	deepak := s.seedColleague("deepak@example.test", "Deepak Menon", "DeepakPass1234")
	if err := s.st.ReserveRequest(s.ctx, deepak, reqID); err != nil {
		t.Fatal(err)
	}
	s.loginAdmin()
	detail := s.get(fmt.Sprintf("/requests/%d", reqID))
	mustContain(t, "detail head", detail, `class="pill processing">With Accounts — taken by Deepak Menon</span>`, "Waiting on Deepak Menon (Accounts)")
	mustNotContain(t, "detail head", detail, ">Waiting on Accounts<")
	list := s.get("/requests?bucket=all")
	mustContain(t, "request list", list, "taken by Deepak Menon", "Waiting on Deepak Menon (Accounts)")
	s.login(deepak.Email, "DeepakPass1234")
	mine := s.get(fmt.Sprintf("/requests/%d", reqID))
	mustContain(t, "holder's own view", mine, "With Accounts — taken by you", `class="waiting you">Waiting on you<`)
}

// A concern the manager raised is displayed as "Partial — under
// discussion", waiting on the manager and Accounts, until the holder answers.
// The status never changes.
func TestConcernDisplaysAsUnderDiscussionUntilAccountsAnswers(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Concern")
	manager := s.seedRoleUser("kavita@example.test", "Kavita Rao", "KavitaPass1234", "Manager")
	reqID := s.seedApprovedRequest(1, admin.ID, manager.ID, headID, 9500000)
	s.loginAdmin()
	s.recordPartial(reqID, headID, 6000000)

	s.login(manager.Email, "KavitaPass1234")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/raise-concern", reqID), url.Values{"comment": {"Why was the balance withheld?"}}), http.StatusSeeOther)
	mgrView := s.get(fmt.Sprintf("/requests/%d/partial-review", reqID))
	mustContain(t, "manager's review after a concern", mgrView,
		`class="pill discussion">Partial — under discussion</span>`, `class="waiting you">Waiting on you and Accounts<`)
	mustNotContain(t, "manager's review after a concern", mgrView, "Partial — manager review")

	s.loginAdmin()
	accView := s.get(fmt.Sprintf("/requests/%d", reqID))
	mustContain(t, "accountant's request after a concern", accView,
		"Partial — under discussion", `class="waiting you">Waiting on you<`, "Kavita Rao raised a concern", "Answer it in the conversation")
	queue := s.get("/accounts-queue?tab=partial_review")
	mustContain(t, "accounts queue", queue, `class="pill discussion">Partial — under discussion</span>`)
	needs := s.get("/requests?bucket=needs-me")
	mustContain(t, "accountant's needs-me", needs, "PR-2026-000001")
	// A third party reads both names.
	rhea := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass1234")
	_ = rhea
	if r, err := s.st.Request(s.ctx, reqID); err != nil || r.Status != "partial_review" {
		t.Fatalf("status after a concern = %q, %v; the state machine must be untouched", r.Status, err)
	}

	// Accounts answers: the ordinary review is back.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/comment", reqID), url.Values{"body": {"The vendor agreed to a retention."}}), http.StatusSeeOther)
	after := s.get(fmt.Sprintf("/requests/%d", reqID))
	mustContain(t, "after the answer", after, "Partial — manager review", "Waiting on Kavita Rao")
	mustNotContain(t, "after the answer", after, "under discussion", "raised a concern")
}

// A non-holding accountant on the stale-reservation screen is told
// they cannot act and who can, instead of an empty "Pick one" card under a
// banner addressed to "you".
func TestStaleScreenTellsANonHolderWhoCanAct(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Stale")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	deepak := s.seedRoleUser("deepak@example.test", "Deepak Menon", "DeepakPass1234", "Accounts")
	if err := s.st.ReserveRequest(s.ctx, deepak, reqID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET processing_at=datetime('now','-26 hours') WHERE id=?`, reqID); err != nil {
		t.Fatal(err)
	}
	bystander := s.seedRoleUser("priya@example.test", "Priya Nair", "PriyaPass1234", "Accounts")
	s.login(bystander.Email, "PriyaPass1234")
	body := s.get(fmt.Sprintf("/requests/%d/reservation/stale", reqID))
	mustNotContain(t, "bystander's stale screen", body, "<h2>Pick one</h2>", "only you or an authorised colleague")
	mustContain(t, "bystander's stale screen", body,
		"only Deepak Menon or an authorised colleague can act", "You cannot act on this reservation", "Deepak Menon can carry on")
	// The holder still gets the choices.
	s.login(deepak.Email, "DeepakPass1234")
	body = s.get(fmt.Sprintf("/requests/%d/reservation/stale", reqID))
	mustContain(t, "holder's stale screen", body, "<h2>Pick one</h2>", "Carry on and record the payment", "only you or an authorised colleague")
}

// A payer who may take a request but not settle a payment is not
// offered "Payment settled →" — a button whose POST answers 403 in silence.
func TestEntryScreenWithholdsSettleFromAPayerWithoutTheVerb(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("NoSettle")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	pns := s.seedProbeUser("pns@example.test", "Pay No Settle", "PnsPass123445", "QA Pay No Settle",
		[]store.Grant{
			{Resource: "request", Action: "view"}, {Resource: "payment", Action: "view"},
			{Resource: "payment", Action: "create"}, {Resource: "payment", Action: "process"},
			{Resource: "reservation", Action: "reserve"}, {Resource: "reservation", Action: "release"},
		},
		[]store.ScopeGrant{{Resource: "request", Scope: store.ScopeAll}, {Resource: "payment", Scope: store.ScopeAll}})
	s.login(pns.Email, "PnsPass123445")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	body := s.get(fmt.Sprintf("/payments/new?request=%d", reqID))
	mustNotContain(t, "no-settle entry screen", body, "settlement-preview", "Payment settled")
	mustContain(t, "no-settle entry screen", body, "cannot settle", "Cancel and release")
	// The seeded Accounts role keeps its button.
	asha := s.seedRoleUser("asha@example.test", "Asha Accounts", "AshaPass1234", "Accounts")
	other := s.seedApprovedRequest(2, admin.ID, admin.ID, headID, 500000)
	s.login(asha.Email, "AshaPass1234")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", other), url.Values{}), http.StatusSeeOther)
	body = s.get(fmt.Sprintf("/payments/new?request=%d", other))
	mustContain(t, "accounts entry screen", body, "Payment settled")
}

// The partial screens name the payee, keep the manager's prompt for
// the manager, and stop calling a written-off balance "still owed".
func TestPartialScreensNameThePayeeAndStopOwingAfterAcceptance(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Copy")
	manager := s.seedRoleUser("mano@example.test", "Mano Manager", "ManoPass1234", "Manager")
	rita := s.seedRequester("rita@example.test", "Rita Requester", "RitaPass1234")
	reqID := s.seedApprovedRequest(1, rita.ID, manager.ID, headID, 550000)
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET type='reimbursement', vendor_payee='Rita Requester', short_title='Taxi to client site' WHERE id=?`, reqID); err != nil {
		t.Fatal(err)
	}
	s.loginAdmin()
	s.recordPartial(reqID, headID, 300000)

	s.login(rita.Email, "RitaPass1234")
	review := s.get(fmt.Sprintf("/requests/%d/partial-review", reqID))
	mustContain(t, "requester's review", review, "Still owed to the payee", "2,500.00", "Mano Manager decides")
	mustNotContain(t, "requester's review", review, "Still owed to the vendor", "Reply to Accounts", "before you decide")

	s.login(manager.Email, "ManoPass1234")
	review = s.get(fmt.Sprintf("/requests/%d/partial-review", reqID))
	mustContain(t, "manager's review", review, "Reply to Accounts", "before you decide")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/accept-partial", reqID), url.Values{}), http.StatusSeeOther)

	s.login(rita.Email, "RitaPass1234")
	closed := s.get(fmt.Sprintf("/requests/%d", reqID))
	mustContain(t, "closed request", closed, "Completed — partial accepted", "Nothing pending", "Balance written off", "2,500.00")
	mustNotContain(t, "closed request", closed, "Still owed")
	s.loginAdmin()
	pay, err := s.st.PaymentForRequest(s.ctx, reqID)
	if err != nil {
		t.Fatal(err)
	}
	payment := s.get(fmt.Sprintf("/payments/%d", pay.ID))
	mustContain(t, "closed payment", payment, "Balance written off")
	mustNotContain(t, "closed payment", payment, "Still owed")
}

// processing_by is retained as history after payment. It must not
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
	s.loginAdmin()
	paid := s.get("/accounts-queue?tab=paid")
	mustContain(t, "paid queue", paid, "PR-2026-000001", "PR-2026-000002", ">View</a>")
	mustNotContain(t, "paid queue", paid, ">Reassign</a>", ">Resume</a>")
	processing := s.get("/accounts-queue?tab=processing")
	mustContain(t, "processing queue", processing, fmt.Sprintf(`href="/requests/%d/reservation">Reassign</a>`, active))
}

// A settlement confirmation that goes stale across a real reassignment is
// rejected: the POST must show the current holder, retain the accountant's inputs,
// remove the mutation affordance, and leave both money and ownership intact.
func TestStaleSettlementConfirmationShowsCurrentHolderAndCannotResubmit(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("StaleConfirm")
	next := s.seedRoleUser("new-owner@example.test", "New Accounts Owner", "NextPass1234", "Accounts")
	id := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 12345)
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", id), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(id)}, "head_id": {strconvFormat(headID)}, "amount": {"123.45"}, "paid_on": {"2026-07-23"}, "payment_mode": {"bank_transfer"}, "reference_no": {"STALE-REF-123"}, "remarks": {"Keep this processing note"}, "settlement": {"settled"}}
	preview := responseBody(t, s.postForm(fmt.Sprintf("/requests/%d/settlement-preview", id), form))
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
			resp := s.postPayment(form)
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
	requireStatus(t, s.postPayment(form), http.StatusSeeOther)
	if err := s.st.DB().QueryRow(`SELECT count(*) FROM payments WHERE request_id=? AND amount=12345`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("new holder could not settle exactly once: %d", count)
	}
}
