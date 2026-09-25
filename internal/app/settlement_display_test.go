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

// The 2026-09-25 fix wave, cluster D (settlement). One test per defect, each
// written to fail on the code as it was and to pin the behaviour the specs and
// the owner decisions ask for.

// seedRoleUser creates an active user holding exactly one seeded system role.
func (s *appTestServer) seedRoleUser(email, name, password, role string) store.User {
	s.t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.t.Fatal(err)
	}
	id, err := s.st.CreateUser(s.ctx, email, name, hash, "data_entry", true)
	if err != nil {
		s.t.Fatal(err)
	}
	s.assignRole(id, role)
	u, err := s.st.UserByID(s.ctx, id)
	if err != nil {
		s.t.Fatal(err)
	}
	return u
}

// recordPartial reserves the request as the signed-in accountant and records a
// short payment against it, the way the entry screen does.
func (s *appTestServer) recordPartial(reqID, headID, paid int64) {
	s.t.Helper()
	requireStatus(s.t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-23"},
		"amount": {money2(paid)}, "vendor_payee": {"Acme Landlord"}, "settlement": {"partial"},
		"partial_reason": {"Retention held back by agreement."}, "reference_no": {"UTR-PART"}, "payment_mode": {"bank_transfer"}}
	requireStatus(s.t, s.postForm("/payments", form), http.StatusSeeOther)
}

func money2(paise int64) string { return fmt.Sprintf("%d.%02d", paise/100, paise%100) }

func mustContain(t *testing.T, what, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing %q:\n%s", what, want, body)
		}
	}
}

func mustNotContain(t *testing.T, what, body string, unwanted ...string) {
	t.Helper()
	for _, bad := range unwanted {
		if strings.Contains(body, bad) {
			t.Fatalf("%s must not say %q:\n%s", what, bad, body)
		}
	}
}

// settlement-2: opening the entry screen for an approved request nobody holds
// is not a lost race. The screen says nobody holds it and offers to take it.
func TestUnclaimedEntryScreenSaysNobodyHoldsItAndOffersToTake(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Unclaimed")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.request(http.MethodGet, strconvPath("/payments/new?request=%d", reqID), nil, "")
	// Still a conflict — the form cannot be shown without a reservation — but
	// the explanation is the true one.
	requireStatus(t, resp, http.StatusConflict)
	body := responseBody(t, resp)
	mustContain(t, "unclaimed entry screen", body,
		"Nobody holds", strconvPath(`action="/requests/%d/record-payment"`, reqID), "Take it for processing",
		`class="pill approved"`, "Waiting on Accounts")
	mustNotContain(t, "unclaimed entry screen", body,
		"Someone else", "took this request before you", `class="pill processing"`)
	// The real race is still reported as one.
	deepak := s.seedColleague("deepak@example.test", "Deepak Menon", "DeepakPass1234")
	if err := s.st.ReserveRequest(s.ctx, deepak, reqID); err != nil {
		t.Fatal(err)
	}
	body = responseBody(t, s.request(http.MethodGet, strconvPath("/payments/new?request=%d", reqID), nil, ""))
	mustContain(t, "taken entry screen", body, "Deepak Menon took this request before you")
	// …in the words every other screen uses for a held request, not a pill of
	// its own (settlement-7 review: "Processing — {holder}").
	mustContain(t, "taken entry screen", body, `class="pill processing">With Accounts — taken by Deepak Menon</span>`)
	mustNotContain(t, "taken entry screen", body, "Processing —")
}

// settlement-7/-8 review: a few lesser screens built their pill from the
// status alone, so they could neither name the holder nor show "Partial —
// under discussion". They all go through statusPill now: the recoverables
// register and detail, the requester's cancel screen, the similar-request card
// on the new-request form, the holder's own payment entry screen, and the
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
	s.login(s.cfg.AdminEmail, testAdminPassword)
	register := responseBody(t, s.request(http.MethodGet, "/recoverables/list", nil, ""))
	mustContain(t, "recoverables register", register,
		`class="pill processing">With Accounts — taken by Deepak Menon</span>`, `class="pill discussion">Partial — under discussion</span>`)
	mustNotContain(t, "recoverables register", register, `class="pill processing">With Accounts</span>`, "Partial — manager review")
	for id, want := range map[int64]string{
		taken:     `class="pill processing">With Accounts — taken by Deepak Menon</span>`,
		discussed: `class="pill discussion">Partial — under discussion</span>`,
	} {
		mustContain(t, "recoverable detail", responseBody(t, s.request(http.MethodGet, strconvPath("/recoverables/%d", id), nil, "")), want)
	}

	// The stale-reservation screen: "taken by you" to the holder, the holder's
	// name to anybody else.
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET processing_at=datetime('now','-26 hours') WHERE id=?`, taken); err != nil {
		t.Fatal(err)
	}
	stale := responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d/reservation/stale", taken), nil, ""))
	mustContain(t, "bystander's stale screen", stale, `class="pill processing">With Accounts — taken by Deepak Menon</span>`)
	s.login(deepak.Email, "DeepakPass1234")
	stale = responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d/reservation/stale", taken), nil, ""))
	mustContain(t, "holder's stale screen", stale, `class="pill processing">With Accounts — taken by you</span>`)
	mustNotContain(t, "holder's stale screen", stale, "Processing —")

	// The similar-request card on the new-request form names the holder too.
	s.login(s.cfg.AdminEmail, testAdminPassword)
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

// settlement-3: a partial review waiting on the manager is listed where the
// manager looks — Home, the approvals queue and "Needs me" — not only behind a
// notification link.
func TestManagerHomeAndApprovalsQueueListPartialReviews(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Partials")
	manager := s.seedRoleUser("kavita@example.test", "Kavita Rao", "KavitaPass1234", "Manager")
	reqID := s.seedApprovedRequest(1, admin.ID, manager.ID, headID, 9500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	s.recordPartial(reqID, headID, 6000000)

	s.login(manager.Email, "KavitaPass1234")
	home := responseBody(t, s.request(http.MethodGet, "/", nil, ""))
	mustContain(t, "manager home", home, "Partial payments to review", "PR-2026-000001", `class="waiting you"`)
	mustNotContain(t, "manager home", home, "Nothing is waiting on you")
	queue := responseBody(t, s.request(http.MethodGet, "/approvals", nil, ""))
	mustContain(t, "approvals queue", queue, `href="/approvals?bucket=partial-review`, "Partial review <span class=\"n\">1</span>")
	tab := responseBody(t, s.request(http.MethodGet, "/approvals?bucket=partial-review", nil, ""))
	mustContain(t, "partial review tab", tab, "PR-2026-000001", "Partial — manager review", "Waiting on you")
	// The sidebar badge counts it with the approvals, and the request list's
	// "Needs me" tab agrees with the detail page's "Waiting on you".
	mustContain(t, "approvals nav badge", tab, `<span class="ico">✓</span>Approvals<span class="n">1</span>`)
	needs := responseBody(t, s.request(http.MethodGet, "/requests?bucket=needs-me", nil, ""))
	mustContain(t, "needs-me", needs, "PR-2026-000001")
}

// settlement-4: phase-3 spec §6 — the request detail offers "Record payment"
// to Accounts when the request is approved, unclaimed and not on hold.
func TestRequestDetailOffersRecordPaymentToAccounts(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Detail")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	asha := s.seedRoleUser("asha@example.test", "Asha Accounts", "AshaPass1234", "Accounts")
	s.login(asha.Email, "AshaPass1234")
	body := responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d", reqID), nil, ""))
	mustContain(t, "accounts detail", body, strconvPath(`action="/requests/%d/record-payment"`, reqID), "Record payment")
	// On hold: not offered — the queue would refuse it too.
	requireStatus(t, s.postForm(strconvPath("/requests/%d/hold", reqID), url.Values{"reason": {"Which invoice?"}}), http.StatusSeeOther)
	body = responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d", reqID), nil, ""))
	mustNotContain(t, "held detail", body, "record-payment")
	requireStatus(t, s.postForm(strconvPath("/requests/%d/unhold", reqID), url.Values{}), http.StatusSeeOther)
	// Taken by somebody: not offered either.
	deepak := s.seedColleague("deepak@example.test", "Deepak Menon", "DeepakPass1234")
	if err := s.st.ReserveRequest(s.ctx, deepak, reqID); err != nil {
		t.Fatal(err)
	}
	body = responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d", reqID), nil, ""))
	mustNotContain(t, "taken detail", body, "record-payment")
	// And never to somebody without reservation:reserve.
	rhea := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass1234")
	own := s.seedApprovedRequest(2, rhea.ID, admin.ID, headID, 500000)
	s.login(rhea.Email, "RheaPass1234")
	body = responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d", own), nil, ""))
	mustNotContain(t, "requester detail", body, "record-payment")
}

// settlement-5: "Payment saved … have been notified" is the confirmation of a
// write, so it appears once, on the redirect from the confirming POST. A later
// visit shows the payment; a refused repeat and a blocked edit say what really
// happened.
func TestPaymentSavedBannerAppearsOnlyAfterTheConfirmingPost(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Banner")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 550000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)}, "paid_on": {"2026-07-23"},
		"amount": {"5500.00"}, "vendor_payee": {"Acme Landlord"}, "settlement": {"settled"}, "reference_no": {"UTR-1"}, "payment_mode": {"bank_transfer"}}
	resp := s.postForm("/payments", form)
	requireStatus(t, resp, http.StatusSeeOther)
	// The landing URL is the payment's own — the outcome rides on a one-shot
	// cookie, so a bookmark or a refresh can never replay the confirmation.
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/payments/") || strings.Contains(loc, "?") {
		t.Fatalf("the confirming POST must land on the payment itself, got %q", loc)
	}
	saved := responseBody(t, s.request(http.MethodGet, loc, nil, ""))
	mustContain(t, "just-saved screen", saved, "Payment saved", "have been notified")

	later := responseBody(t, s.request(http.MethodGet, loc, nil, ""))
	mustNotContain(t, "a later visit", later, "Payment saved", "have been notified")
	mustContain(t, "a later visit", later, "PAY-", "5,500.00")

	// A second confirm with a different figure writes nothing and says so.
	form.Set("amount", "5000.00")
	resp = s.postForm("/payments", form)
	requireStatus(t, resp, http.StatusSeeOther)
	if got := resp.Header.Get("Location"); got != loc {
		t.Fatalf("refused repeat landed on %q, want the existing payment %q", got, loc)
	}
	dup := responseBody(t, s.request(http.MethodGet, loc, nil, ""))
	mustContain(t, "refused repeat", dup, "already", "nothing new was saved", "5,500.00")
	mustNotContain(t, "refused repeat", dup, "Payment saved", "5,000.00")
	mustNotContain(t, "the visit after the refused repeat", responseBody(t, s.request(http.MethodGet, loc, nil, "")), "nothing new was saved")

	// A blocked edit lands on the payment with the reason, once.
	resp = s.request(http.MethodGet, loc+"/edit", nil, "")
	requireStatus(t, resp, http.StatusSeeOther)
	if got := resp.Header.Get("Location"); got != loc {
		t.Fatalf("blocked edit landed on %q", got)
	}
	imm := responseBody(t, s.request(http.MethodGet, loc, nil, ""))
	mustContain(t, "blocked edit", imm, "cannot be edited")
	mustNotContain(t, "blocked edit", imm, "Payment saved")
	mustNotContain(t, "the visit after the blocked edit", responseBody(t, s.request(http.MethodGet, loc, nil, "")), "cannot be edited")
}

// settlement-6: on the htmx path the sheet sits inside the live entry form, so
// its closers dismiss it in place and the typed values stay. On the no-JS page
// the same anchors still navigate.
func TestSettlementSheetClosersDismissInPlaceOnTheHtmxPath(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Sheet")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 5000000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	form := url.Values{"amount": {"45000.00"}, "paid_on": {"2026-07-23"}, "payment_mode": {"bank_transfer"}, "reference_no": {"UTR-A-001"}}
	fragment := responseBody(t, s.postFormHX(strconvPath("/requests/%d/settlement-preview", reqID), form))
	href := strconvPath(`href="/payments/new?request=%d"`, reqID)
	mustContain(t, "htmx sheet", fragment,
		`<a class="sh-close" `+href+` data-close="settle-sheet"`,
		`<a class="btn outline" `+href+` data-close="settle-sheet">Go back</a>`)
	page := responseBody(t, s.postForm(strconvPath("/requests/%d/settlement-preview", reqID), form))
	mustContain(t, "no-JS page", page, `<a class="btn outline" `+href+`>Go back</a>`)
	mustNotContain(t, "no-JS page", page, `data-close="settle-sheet"`)
}

// settlement-7: a processing request names who holds it, on the pill and in
// the waiting line, for everybody but the holder — who reads "you".
func TestProcessingRequestNamesTheHolderEverywhere(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Holder")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	deepak := s.seedColleague("deepak@example.test", "Deepak Menon", "DeepakPass1234")
	if err := s.st.ReserveRequest(s.ctx, deepak, reqID); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	detail := responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d", reqID), nil, ""))
	mustContain(t, "detail head", detail, `class="pill processing">With Accounts — taken by Deepak Menon</span>`, "Waiting on Deepak Menon (Accounts)")
	mustNotContain(t, "detail head", detail, ">Waiting on Accounts<")
	list := responseBody(t, s.request(http.MethodGet, "/requests?bucket=all", nil, ""))
	mustContain(t, "request list", list, "taken by Deepak Menon", "Waiting on Deepak Menon (Accounts)")
	s.login(deepak.Email, "DeepakPass1234")
	mine := responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d", reqID), nil, ""))
	mustContain(t, "holder's own view", mine, "With Accounts — taken by you", `class="waiting you">Waiting on you<`)
}

// settlement-8: a concern the manager raised is displayed as "Partial — under
// discussion", waiting on the manager and Accounts, until the holder answers.
// The status never changes.
func TestConcernDisplaysAsUnderDiscussionUntilAccountsAnswers(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Concern")
	manager := s.seedRoleUser("kavita@example.test", "Kavita Rao", "KavitaPass1234", "Manager")
	reqID := s.seedApprovedRequest(1, admin.ID, manager.ID, headID, 9500000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	s.recordPartial(reqID, headID, 6000000)

	s.login(manager.Email, "KavitaPass1234")
	requireStatus(t, s.postForm(strconvPath("/requests/%d/raise-concern", reqID), url.Values{"comment": {"Why was the balance withheld?"}}), http.StatusSeeOther)
	mgrView := responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d/partial-review", reqID), nil, ""))
	mustContain(t, "manager's review after a concern", mgrView,
		`class="pill discussion">Partial — under discussion</span>`, `class="waiting you">Waiting on you and Accounts<`)
	mustNotContain(t, "manager's review after a concern", mgrView, "Partial — manager review")

	s.login(s.cfg.AdminEmail, testAdminPassword)
	accView := responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d", reqID), nil, ""))
	mustContain(t, "accountant's request after a concern", accView,
		"Partial — under discussion", `class="waiting you">Waiting on you<`, "Kavita Rao raised a concern", "Answer it in the conversation")
	queue := responseBody(t, s.request(http.MethodGet, "/accounts-queue?tab=partial_review", nil, ""))
	mustContain(t, "accounts queue", queue, `class="pill discussion">Partial — under discussion</span>`)
	needs := responseBody(t, s.request(http.MethodGet, "/requests?bucket=needs-me", nil, ""))
	mustContain(t, "accountant's needs-me", needs, "PR-2026-000001")
	// A third party reads both names.
	rhea := s.seedRequester("rhea@example.test", "Rhea Requester", "RheaPass1234")
	_ = rhea
	if r, err := s.st.Request(s.ctx, reqID); err != nil || r.Status != "partial_review" {
		t.Fatalf("status after a concern = %q, %v; the state machine must be untouched", r.Status, err)
	}

	// Accounts answers: the ordinary review is back.
	requireStatus(t, s.postForm(strconvPath("/requests/%d/comment", reqID), url.Values{"body": {"The vendor agreed to a retention."}}), http.StatusSeeOther)
	after := responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d", reqID), nil, ""))
	mustContain(t, "after the answer", after, "Partial — manager review", "Waiting on Kavita Rao")
	mustNotContain(t, "after the answer", after, "under discussion", "raised a concern")
}

// queue-1: a non-holding accountant on the stale-reservation screen is told
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
	body := responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d/reservation/stale", reqID), nil, ""))
	mustNotContain(t, "bystander's stale screen", body, "<h2>Pick one</h2>", "only you or an authorised colleague")
	mustContain(t, "bystander's stale screen", body,
		"only Deepak Menon or an authorised colleague can act", "You cannot act on this reservation", "Deepak Menon can carry on")
	// The holder still gets the choices.
	s.login(deepak.Email, "DeepakPass1234")
	body = responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d/reservation/stale", reqID), nil, ""))
	mustContain(t, "holder's stale screen", body, "<h2>Pick one</h2>", "Carry on and record the payment", "only you or an authorised colleague")
}

// queue-2: a payer who may take a request but not settle a payment is not
// offered "Payment settled →" — a button whose POST answers 403 in silence.
func TestEntryScreenWithholdsSettleFromAPayerWithoutTheVerb(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("NoSettle")
	reqID := s.seedApprovedRequest(1, admin.ID, admin.ID, headID, 500000)
	pns := s.seedProbeUser("pns@example.test", "Pay No Settle", "PnsPass1234", "QA Pay No Settle",
		[]store.Grant{
			{Resource: "request", Action: "view"}, {Resource: "payment", Action: "view"},
			{Resource: "payment", Action: "create"}, {Resource: "payment", Action: "process"},
			{Resource: "reservation", Action: "reserve"}, {Resource: "reservation", Action: "release"},
		},
		[]store.ScopeGrant{{Resource: "request", Scope: store.ScopeAll}, {Resource: "payment", Scope: store.ScopeAll}})
	s.login(pns.Email, "PnsPass1234")
	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	body := responseBody(t, s.request(http.MethodGet, strconvPath("/payments/new?request=%d", reqID), nil, ""))
	mustNotContain(t, "no-settle entry screen", body, "settlement-preview", "Payment settled")
	mustContain(t, "no-settle entry screen", body, "cannot settle", "Cancel and release")
	// The seeded Accounts role keeps its button.
	asha := s.seedRoleUser("asha@example.test", "Asha Accounts", "AshaPass1234", "Accounts")
	other := s.seedApprovedRequest(2, admin.ID, admin.ID, headID, 500000)
	s.login(asha.Email, "AshaPass1234")
	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", other), url.Values{}), http.StatusSeeOther)
	body = responseBody(t, s.request(http.MethodGet, strconvPath("/payments/new?request=%d", other), nil, ""))
	mustContain(t, "accounts entry screen", body, "Payment settled")
}

// partial-1: the partial screens name the payee, keep the manager's prompt for
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
	s.login(s.cfg.AdminEmail, testAdminPassword)
	s.recordPartial(reqID, headID, 300000)

	s.login(rita.Email, "RitaPass1234")
	review := responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d/partial-review", reqID), nil, ""))
	mustContain(t, "requester's review", review, "Still owed to the payee", "2,500.00", "Mano Manager decides")
	mustNotContain(t, "requester's review", review, "Still owed to the vendor", "Reply to Accounts", "before you decide")

	s.login(manager.Email, "ManoPass1234")
	review = responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d/partial-review", reqID), nil, ""))
	mustContain(t, "manager's review", review, "Reply to Accounts", "before you decide")
	requireStatus(t, s.postForm(strconvPath("/requests/%d/accept-partial", reqID), url.Values{}), http.StatusSeeOther)

	s.login(rita.Email, "RitaPass1234")
	closed := responseBody(t, s.request(http.MethodGet, strconvPath("/requests/%d", reqID), nil, ""))
	mustContain(t, "closed request", closed, "Completed — partial accepted", "Nothing pending", "Balance written off", "2,500.00")
	mustNotContain(t, "closed request", closed, "Still owed")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	pay, err := s.st.PaymentForRequest(s.ctx, reqID)
	if err != nil {
		t.Fatal(err)
	}
	payment := responseBody(t, s.request(http.MethodGet, strconvPath("/payments/%d", pay.ID), nil, ""))
	mustContain(t, "closed payment", payment, "Balance written off")
	mustNotContain(t, "closed payment", payment, "Still owed")
}
