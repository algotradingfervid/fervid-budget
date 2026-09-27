package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/money"
	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

// Accounts' side of a request: the payment queue, reserving an approved request
// to pay it, the payment entry and settlement confirmation, the approver's
// partial-payment review, releasing or reassigning a reservation, and holds.
//
// Rules this file follows:
//   - A payment is recorded against one approved request, by the accountant
//     who has reserved it. Reserving is atomic in the store.
//   - Finding that someone else holds the request is not an error page: every
//     such path shows reservationConflict, which says who holds it and why.
//   - The settlement preview writes nothing. Only POST /payments saves.

// withHolderName fills in req.ProcessingByName (the name of the accountant who
// reserved it), which store.Request does not load. A failed lookup is logged
// and leaves the name empty.
func (a *App) withHolderName(r *http.Request, req store.Request) store.Request {
	if req.ProcessingBy == nil || req.ProcessingByName != "" {
		return req
	}
	holder, err := a.st.UserByID(r.Context(), *req.ProcessingBy)
	if err != nil {
		a.log.WarnContext(r.Context(), "reservation holder could not be named",
			"request_id", requestID(r), "request", req.ID, "error", err)
		return req
	}
	req.ProcessingByName = holder.Name
	return req
}

// Why a request could not be reserved or paid by the reader. The
// reservation_conflict template shows a different message for each.
const (
	conflictTaken       = "taken"        // somebody really does hold it
	conflictHold        = "hold"         // approved, but put on hold by Accounts
	conflictNotApproved = "not-approved" // any other status: completed, cancelled, frozen…
	conflictUnclaimed   = "unclaimed"    // approved and free: the reader simply does not hold it
)

// reservationConflict shows the 409 page for a request the reader cannot take
// or pay: who holds it (or "Someone else") and why. cause is the store's
// refusal, or nil when the conflict was found by reading the request. The
// queue, the picker and the payment form all use it.
func (a *App) reservationConflict(w http.ResponseWriter, r *http.Request, req store.Request, cause error) {
	req = a.withHolderName(r, req)
	holder := req.ProcessingByName
	if holder == "" {
		holder = "Someone else"
	}
	kind := conflictCause(req, cause)
	a.log.WarnContext(r.Context(), "reservation conflict",
		"request_id", requestID(r), "request", req.ID, "holder", holder,
		"cause", kind, "error", cause)
	title := "Already taken"
	switch kind {
	case conflictHold:
		title = "On hold"
	case conflictNotApproved:
		title = "Not available to process"
	case conflictUnclaimed:
		title = "Not reserved by you"
	}
	a.renderStatus(w, r, http.StatusConflict, "reservation_conflict", PageData{
		Title:         title,
		Request2:      req,
		Holder:        holder,
		ConflictCause: kind,
	})
}

// conflictCause decides which conflict to show, from the store's error and the
// request as re-read. The request's own status wins over the error: a paid
// request still has processing_by set, and the store reports it as "already
// reserved", but nobody is paying it any more. paymentEntry and
// settlementPreview pass a nil cause, so only the request is used there.
func conflictCause(req store.Request, cause error) string {
	switch {
	case activeHold(req) || errors.Is(cause, store.ErrRequestOnHold):
		return conflictHold
	case req.Status == "processing" && req.ProcessingBy != nil:
		return conflictTaken
	case req.Status == "approved" && cause != nil:
		// The reservation was refused but the request is free again: someone
		// else took it and has already let go.
		return conflictTaken
	case req.Status == "approved":
		// Nothing was refused: the reader opened the payment form for a
		// request nobody holds (after releasing it, or by URL). It is free.
		return conflictUnclaimed
	default:
		return conflictNotApproved
	}
}

// heldByCaller reports whether req is currently reserved (processing) by
// userID.
func heldByCaller(req store.Request, userID int64) bool {
	return req.Status == "processing" && req.ProcessingBy != nil && *req.ProcessingBy == userID
}

// requestRecordPayment reserves the request for the reader and opens the
// payment form. The route needs reservation:reserve, not payment:create:
// taking work from the queue is a separate permission from recording money.
// A refused reservation shows the conflict page.
func (a *App) requestRecordPayment(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	u := auth.CurrentUser(r)
	if _, ok := a.loadRequestByID(w, r, id); !ok {
		return
	}
	if err := a.st.ReserveRequest(r.Context(), u, id); err != nil {
		if errors.Is(err, store.ErrForbidden) {
			req, rerr := a.st.Request(r.Context(), id)
			if rerr != nil {
				a.respondStoreError(w, r, rerr)
				return
			}
			a.reservationConflict(w, r, req, err)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/new?request=%d", id), http.StatusSeeOther)
}

// queueTab is one tab on the Accounts payment queue. Key is the status filter
// the store understands (LinkableOptions.Status).
type queueTab struct{ Key, Label string }

// queueTabs are the payment queue's tabs, in display order. Each tab shows the
// same set its count badge counts; "approved" means approved, unreserved and
// not on hold.
var queueTabs = []queueTab{
	{"approved", "Approved, unclaimed"}, {"processing", "Processing"}, {"hold", "On hold"},
	{"partial_review", "Partial review"}, {"paid", "Paid"},
}

// SettlementPreview is what the settlement confirmation shows: the amount still
// approved, the amount being paid and the difference. It is never saved.
type SettlementPreview struct {
	Approved      int64
	Paid          int64
	Difference    int64 // approved - paid; > 0 means under-paid
	Match         bool
	Settlement    string
	PartialReason string
	Fields        map[string]string // every entry-form field, carried to the confirm POST
	// Fragment is true when the sheet is shown inside the entry form (htmx).
	// Closing it then keeps what was typed; on the no-JavaScript page the close
	// buttons are links back to the entry screen.
	Fragment bool
}

// PaidRequestRow is one row of "Recently paid by you" on the payment picker.
type PaidRequestRow struct {
	PaymentID  int64
	RequestID  int64
	Number     string
	Payee      string
	Amount     int64
	PaidOn     string
	Settlement string
	Status     string // the request's status: completed | completed_partial | partial_review
}

// accountsQueue is the Accounts payment queue (/accounts-queue?tab=). An
// unknown tab answers 400. The search box narrows the rows but not the tab
// counts, which always cover the reader's whole scope.
func (a *App) accountsQueue(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = queueTabs[0].Key
	}
	if !slices.ContainsFunc(queueTabs, func(t queueTab) bool { return t.Key == tab }) {
		a.respondError(w, r, http.StatusBadRequest, "That queue tab does not exist.", nil)
		return
	}
	query := r.URL.Query().Get("q")
	set, err := a.st.LinkablePaymentRequests(r.Context(), store.LinkableOptions{
		Scope: a.auth.Scope(u, "request"), ViewerID: u.ID, Status: tab, Query: query,
	})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "accounts_queue", PageData{Title: "Payment queue", Linkable: set, Tab: tab, Query: query})
}

// pickerData builds the payment picker (GET /payments/new with no request, and
// its search fragment): up to 20 requests that can be paid, and the reader's
// five most recent payments.
func (a *App) pickerData(r *http.Request) (PageData, error) {
	u := auth.CurrentUser(r)
	q := r.URL.Query().Get("q")
	set, err := a.st.LinkablePaymentRequests(r.Context(), store.LinkableOptions{
		Scope: a.auth.Scope(u, "request"), ViewerID: u.ID, Query: q, Limit: 20,
	})
	if err != nil {
		return PageData{}, err
	}
	recent, err := a.recentPaidByActor(r, u.ID, 5)
	if err != nil {
		return PageData{}, err
	}
	return PageData{Title: "Record a payment", Linkable: set, RecentPaid: recent, Query: q}, nil
}

// recentPaidByActor lists the actor's most recently entered payments that are
// linked to a request, newest entry first, at most limit of them. It reads the
// actor's own ledger rows (voided ones included), re-sorts them by entry time
// (the ledger comes back ordered by payment date), and names each row from the
// request it paid.
func (a *App) recentPaidByActor(r *http.Request, actorID int64, limit int) ([]PaidRequestRow, error) {
	payments, err := a.st.ListPayments(r.Context(), store.PaymentListOptions{Status: "all", Limit: 200, Scope: "own", ViewerID: actorID})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(payments, func(i, j int) bool { return payments[i].CreatedAt.After(payments[j].CreatedAt) })
	out := make([]PaidRequestRow, 0, limit)
	for _, p := range payments {
		if len(out) == limit {
			break
		}
		if p.RequestID == nil {
			continue
		}
		req, err := a.st.Request(r.Context(), *p.RequestID)
		if err != nil {
			return nil, err
		}
		out = append(out, PaidRequestRow{
			PaymentID: p.ID, RequestID: req.ID, Number: req.Number,
			Payee: p.VendorPayee, Amount: p.Amount, PaidOn: p.PaidOn,
			Settlement: p.Settlement, Status: req.Status,
		})
	}
	return out, nil
}

// paymentEntry is the payment form for one reserved request (GET
// /payments/new?request=). It saves nothing; its button opens the settlement
// confirmation. A request the reader has not reserved shows the conflict page.
// The form opens on today's date and says up front if that month is locked.
// When a rejected confirmation posts back here (POST), the typed values are
// shown again; they travel in the body so references and notes stay out of
// the URL.
func (a *App) paymentEntry(w http.ResponseWriter, r *http.Request, linkedID int64) {
	u := auth.CurrentUser(r)
	req, ok := a.loadRequestByID(w, r, linkedID)
	if !ok {
		return
	}
	if !heldByCaller(req, u.ID) {
		a.reservationConflict(w, r, req, nil)
		return
	}
	headID := deref(req.HeadID)
	paidOn := time.Now().Format(time.DateOnly)
	token, err := newFormToken()
	if err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "Could not prepare payment confirmation.", err)
		return
	}
	settings, err := a.st.AppSettings(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	choices := paymentModeChoices(settings, "")
	vendor := store.Vendor{}
	defaultMode := ""
	if req.VendorID != nil {
		vendor, err = a.st.Vendor(r.Context(), *req.VendorID, a.auth.Permissions(u))
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		// The vendor's usual mode is preselected when the form offers it.
		if vendor.Bank != nil {
			candidate := canonicalPaymentMode(vendor.Bank.DefaultPaymentMode)
			if slices.ContainsFunc(choices, func(c PaymentModeChoice) bool { return c.Value == candidate }) {
				defaultMode = candidate
			}
		}
	}
	data := PageData{
		PaymentModeChoices: choices, Vendor: vendor,
		Title:          "Record payment",
		SubmissionKey:  token,
		Request2:       req,
		SelectedHeadID: headID,
		ReserveMine:    true,
		Month:          paidOn[:7],
		Locked:         a.st.IsLocked(r.Context(), paidOn[:7]),
		Payment: store.Payment{
			PaymentMode: defaultMode,
			HeadID:      headID,
			PaidOn:      paidOn,
			Amount:      approvedOf(req) - req.PaidAmount,
			// The displayed payee: a vendor invoice names its vendor by id and
			// leaves the vendor_payee column empty.
			VendorPayee: req.Vendor,
			InvoiceNo:   req.InvoiceNo,
		},
	}
	if r.Method == http.MethodPost {
		data.PaymentAmount = r.FormValue("amount")
		data.Payment.Amount, _ = money.ParsePaise(data.PaymentAmount)
		data.Payment.PaidOn = r.FormValue("paid_on")
		data.Payment.PaymentMode = r.FormValue("payment_mode")
		data.Payment.ReferenceNo = r.FormValue("reference_no")
		data.Payment.Remarks = r.FormValue("remarks")
		if validDateInput(data.Payment.PaidOn) {
			data.Month = data.Payment.PaidOn[:7]
			data.Locked = a.st.IsLocked(r.Context(), data.Month)
		}
	}
	a.render(w, r, "payment_form", data)
}

// newFormToken returns a random 48-character hex token. A form carries it so
// that the same submission sent twice is recorded only once.
func newFormToken() (string, error) {
	var token [24]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}

// paymentLockStatus returns the "month is locked" banner fragment for the
// ?paid_on= date; the payment form asks for it whenever the date changes. An
// empty or invalid date gets an empty banner. A failed lock read is an error,
// never reported as an open month.
func (a *App) paymentLockStatus(w http.ResponseWriter, r *http.Request) {
	paidOn := r.URL.Query().Get("paid_on")
	data := PageData{}
	if validDateInput(paidOn) {
		locked, err := a.st.MonthIsLocked(r.Context(), paidOn[:7])
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		data.Month, data.Locked = paidOn[:7], locked
	}
	a.renderPartial(w, r, "payment_lock_banner", data)
}

// paymentPickerOptions returns the picker's result list as a fragment, for
// the search box.
func (a *App) paymentPickerOptions(w http.ResponseWriter, r *http.Request) {
	data, err := a.pickerData(r)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.renderPartial(w, r, "payment_pick_options", data)
}

// settlementPreview shows the settlement confirmation: the amount being paid
// against what is still approved. It writes nothing (the payment is saved
// only by POST /payments). It checks the reader still holds the reservation,
// and shows the conflict page if not.
func (a *App) settlementPreview(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	id := pathID(r)
	req, ok := a.loadRequestByID(w, r, id)
	if !ok {
		return
	}
	if !heldByCaller(req, u.ID) {
		a.reservationConflict(w, r, req, nil)
		return
	}
	paid, perr := money.ParsePaise(r.FormValue("amount"))
	if perr != nil {
		a.settlementError(w, r, id, store.PaymentInput{}, r.FormValue("amount"), "", "",
			fmt.Errorf("%w: enter a valid amount", store.ErrValidation))
		return
	}
	preview := newSettlementPreview(r, req, paid)
	if preview.Match {
		preview.Settlement = "settled"
	}
	a.renderSettlement(w, r, http.StatusOK, req, preview, "")
}

// newSettlementPreview compares what is still approved on req (the approved
// amount less what has already been paid) with the amount being paid now, and
// carries the entry form's fields along for the confirm step.
func newSettlementPreview(r *http.Request, req store.Request, paid int64) SettlementPreview {
	approved := approvedOf(req) - req.PaidAmount
	return SettlementPreview{Approved: approved, Paid: paid, Difference: approved - paid,
		Match: approved == paid, Fields: settlementFields(r)}
}

// settlementFields copies the entry form's non-empty fields so the
// confirmation can post them all at once. The file is not included: it stays
// in the live form (htmx), and the no-JavaScript page asks for it again.
func settlementFields(r *http.Request) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"amount", "paid_on", "payment_mode", "reference_no", "invoice_no", "remarks", "vendor_payee", "head_id", "submission_key", "expected_paid"} {
		if v := r.FormValue(k); v != "" {
			out[k] = v
		}
	}
	return out
}

// renderSettlement shows the settlement sheet: as a fragment for htmx, or as a
// full page otherwise. Both use the same markup.
func (a *App) renderSettlement(w http.ResponseWriter, r *http.Request, status int, req store.Request, p SettlementPreview, errMsg string) {
	name := "settlement_confirm"
	if isFragmentRequest(r) {
		name = "settlement_sheet"
		p.Fragment = true
	}
	a.renderStatus(w, r, status, name, PageData{
		Title: "Confirm the payment", Request2: a.withHolderName(r, req), Settlement: p, Error: errMsg,
		ReserveMine: heldByCaller(req, auth.CurrentUser(r).ID),
	})
}

// settlementError shows the confirmation sheet again with the error message
// and the figures that were typed. If the reader no longer holds the
// reservation it answers 409 saying the payment was not saved. Server faults
// get the error page instead.
func (a *App) settlementError(w http.ResponseWriter, r *http.Request, linkedID int64, in store.PaymentInput, rawAmount, settlement, partialReason string, cause error) {
	status, ok := userError(cause)
	if !ok {
		a.respondStoreError(w, r, cause)
		return
	}
	req, ok := a.loadRequestByID(w, r, linkedID)
	if !ok {
		return
	}
	if !heldByCaller(req, auth.CurrentUser(r).ID) {
		status = http.StatusConflict
		cause = fmt.Errorf("%w: this request is no longer reserved by you. Your payment was not saved", store.ErrForbidden)
	}
	// An amount that does not parse counts as zero here; the form field still
	// shows exactly what was typed.
	paid, _ := money.ParsePaise(rawAmount)
	if in.Amount != 0 {
		paid = in.Amount
	}
	preview := newSettlementPreview(r, req, paid)
	preview.Settlement, preview.PartialReason = settlement, partialReason
	a.renderSettlement(w, r, status, req, preview, friendly(cause))
}

// Partial review. When Accounts pays less than was approved, the request waits
// in partial_review for its approver. The approver either accepts the partial
// payment (the balance is written off) or raises a concern (the request stays
// open). Neither moves money, and the payment itself cannot be changed here.

// requestPartialReview shows the partial-review screen: the payment, its
// documents and one history that mixes the request's and the payment's audit
// with the conversation. A request not in partial_review is redirected to its
// own page.
func (a *App) requestPartialReview(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	if req.Status != "partial_review" {
		http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
		return
	}
	pay, err := a.st.PaymentForRequest(r.Context(), req.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.respondError(w, r, http.StatusNotFound,
				"No payment has been recorded against this request yet, so there is nothing to review.", err)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	comments, err := a.st.RequestComments(r.Context(), req.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	trail, err := a.mergedTrail(r, req.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// The payment's documents (such as the bank advice) are shown too.
	atts, err := a.st.Attachments(r.Context(), pay.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "partial_review", PageData{
		Title:       "Partial payment " + req.Number,
		Request2:    req,
		Payment:     pay,
		Attachments: atts,
		Trail:       partialTrail(trail, comments),
	})
}

// TrailLine is one line of the partial-review history, which mixes the
// request's audit, the payment's audit and the conversation in time order.
type TrailLine struct {
	Comment   bool // a comment someone wrote, rather than a recorded event
	Action    string
	ActorID   int64
	ActorName string
	Body      string
	CreatedAt time.Time
}

// partialTrail merges the audit entries with the comments, oldest first. The
// request's own "comment", "attach" and "concern" audit rows are dropped,
// because the conversation already shows them (store.RequestThread does the
// same).
func partialTrail(entries []store.AuditEntry, comments []store.RequestComment) []TrailLine {
	out := make([]TrailLine, 0, len(entries)+len(comments))
	for _, e := range entries {
		if e.EntityType == "payment_request" && (e.Action == "comment" || e.Action == "attach" || e.Action == "concern") {
			continue
		}
		var actorID int64
		if e.ActorID != nil {
			actorID = *e.ActorID
		}
		out = append(out, TrailLine{Action: trailAction(e), ActorID: actorID,
			ActorName: e.ActorName, Body: trailBody(e), CreatedAt: e.CreatedAt})
	}
	for _, c := range comments {
		out = append(out, TrailLine{Comment: true, ActorID: c.AuthorID,
			ActorName: c.AuthorName, Body: c.Body, CreatedAt: c.CreatedAt})
	}
	// A stable sort keeps same-second entries in merge order (audit rows are
	// already ordered by id).
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// requestAcceptPartial records the approver's decision on a partial payment.
// decision=continue keeps the request open for more payment (ContinuePartial);
// otherwise the partial payment is accepted and the request closes as
// completed_partial (not completed, so the written-off balance stays visible),
// and the requester and accountant are notified.
func (a *App) requestAcceptPartial(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if _, ok := a.loadRequestByID(w, r, id); !ok {
		return
	}
	if r.FormValue("decision") == "continue" {
		if err := a.st.ContinuePartial(r.Context(), auth.CurrentUser(r), id, r.FormValue("note")); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/requests/%d", id), http.StatusSeeOther)
		return
	}
	if err := a.st.AcceptPartial(r.Context(), auth.CurrentUser(r), id, r.FormValue("note")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.fire(r, notify.EventPaymentPartialAccepted, id)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", id), http.StatusSeeOther)
}

// requestRaiseConcern adds the approver's concern about a partial payment to
// the conversation and notifies Accounts. The request stays in review; nothing
// is reversed. (The request's scope is checked by the POST middleware.)
func (a *App) requestRaiseConcern(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := a.st.RaiseConcern(r.Context(), auth.CurrentUser(r), id, r.FormValue("comment")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.fire(r, notify.EventPaymentPartialConcern, id)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d/partial-review", id), http.StatusSeeOther)
}

// Giving up a reservation. One screen offers two actions: release the request
// back to the queue, or hand it to a named colleague. Both need a reason (the
// requester and approver are told why) and the same confirmation that no
// payment has been started. The server checks every field itself; the form
// only shows or hides them.

// reservationForm shows the release/reassign screen for a reserved request. The
// reader need not hold the reservation (the conflict page links here to ask
// for a reassignment), but must be able to do one of the two actions: release
// their own reservation, or reassign any (403 otherwise). The route only
// requires a signed-in user, because it cannot demand "either permission".
func (a *App) reservationForm(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	if req.Status != "processing" || req.ProcessingBy == nil {
		a.respondError(w, r, http.StatusConflict, "This request is not reserved by anyone.", nil)
		return
	}
	mine := heldByCaller(req, u.ID)
	mayRelease := a.auth.Can(u, "reservation", "release")
	mayReassign := a.auth.Can(u, "reservation", "reassign")
	if !(mine && mayRelease) && !mayReassign {
		a.respondError(w, r, http.StatusForbidden,
			"Only the person holding this reservation can release it.", nil)
		return
	}
	var users []store.User
	if mayReassign {
		all, uerr := a.st.ListUsers(r.Context())
		if uerr != nil {
			a.respondStoreError(w, r, uerr)
			return
		}
		users = a.reassignCandidates(all, *req.ProcessingBy)
	}
	trail, terr := a.mergedTrail(r, req.ID)
	if terr != nil {
		a.respondStoreError(w, r, terr)
		return
	}
	// The title names what this reader can actually do.
	title := "Release reservation"
	if !mayRelease {
		title = "Reassign reservation"
	}
	a.render(w, r, "reservation_form", PageData{
		Title:       title,
		Request2:    a.withHolderName(r, req),
		Users:       users,
		Audit:       reservationTrail(trail),
		ReserveMine: mine,
	})
}

// reassignCandidates lists who a reservation may be handed to: active users,
// other than the current holder, who can work the Accounts queue. The store
// refuses inactive users and the current holder too. The reader is not
// excluded, so an administrator may take over someone else's reservation.
// Handing it to someone without payment:process would leave the request stuck,
// since they could not open the queue to pay it.
func (a *App) reassignCandidates(users []store.User, holderID int64) []store.User {
	out := make([]store.User, 0, len(users))
	for _, u := range users {
		if !u.Active || u.ID == holderID || !canWorkTheQueue(a.auth, u) {
			continue
		}
		out = append(out, u)
	}
	return out
}

// canWorkTheQueue reports whether u is active and holds payment:process, the
// permission needed to open the Accounts queue.
func canWorkTheQueue(am *auth.Manager, u store.User) bool {
	return u.Active && am.Can(u, "payment", "process")
}

// reservationTrail keeps only the reserve, release and reassign entries. It
// filters here, not in the template, so an empty result shows the template's
// "No reservation history yet." ({{range}}…{{else}} needs an empty slice).
func reservationTrail(entries []store.AuditEntry) []store.AuditEntry {
	out := make([]store.AuditEntry, 0, len(entries))
	for _, e := range entries {
		switch e.Action {
		case "process", "release", "reassign":
			out = append(out, e)
		}
	}
	return out
}

// requestRelease puts a reserved request back in the open queue and notifies
// the requester and approver. The store requires a reason and the
// confirmation. Releasing someone else's reservation needs reservation:reassign.
func (a *App) requestRelease(w http.ResponseWriter, r *http.Request) {
	a.changeRequest(w, r, notify.EventReservationReleased, "/accounts-queue", func(ctx context.Context, u store.User, id int64) error {
		return a.st.ReleaseRequest(ctx, u, id, r.FormValue("reason"),
			r.FormValue("confirm") == "on", a.auth.Can(u, "reservation", "reassign"))
	})
}

// requestReassign hands the reservation to another person who can work the
// Accounts queue, and notifies the requester, approver and new holder. It
// requires a target and the "no payment initiated" confirmation (checked here,
// as the store's ReassignReservation does not take it).
func (a *App) requestReassign(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	u := auth.CurrentUser(r)
	to := parseID(r.FormValue("to_user_id"))
	if to == 0 {
		a.respondError(w, r, http.StatusBadRequest, "Choose who should take this reservation.", nil)
		return
	}
	if r.FormValue("confirm") != "on" {
		a.respondError(w, r, http.StatusBadRequest, "Confirm that no payment has been initiated.", nil)
		return
	}
	target, terr := a.st.UserByID(r.Context(), to)
	if terr != nil {
		a.respondStoreError(w, r, terr)
		return
	}
	if !canWorkTheQueue(a.auth, target) {
		a.respondError(w, r, http.StatusBadRequest, "That person cannot work the Accounts queue.", nil)
		return
	}
	if err := a.st.ReassignReservation(r.Context(), u, req.ID, to, r.FormValue("reason"),
		a.auth.Can(u, "reservation", "reassign")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.fire(r, notify.EventReservationReassigned, req.ID)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
}

// Holds and stale reservations. A hold pauses payment of an approved request
// until the requester answers Accounts' question (in the request's comments,
// or by adding a document). The status stays "approved"; the on-hold flag is
// what keeps it out of the payable queue.

// requestHold puts an approved request on hold with a reason (the store
// requires one) and notifies the requester.
func (a *App) requestHold(w http.ResponseWriter, r *http.Request) {
	a.changeRequest(w, r, notify.EventRequestOnHold, "/requests/{id}", func(ctx context.Context, u store.User, id int64) error {
		return a.st.HoldRequest(ctx, u, id, r.FormValue("reason"))
	})
}

// requestUnhold lifts a hold, putting the request back in the payable queue,
// and notifies the requester. No reason is needed.
func (a *App) requestUnhold(w http.ResponseWriter, r *http.Request) {
	a.changeRequest(w, r, notify.EventRequestUnheld, "/requests/{id}", func(ctx context.Context, u store.User, id int64) error {
		return a.st.UnholdRequest(ctx, u, id)
	})
}

// requestStale is the "reserved too long" screen for a reservation open past
// the configured threshold. It only explains the options; nothing is released
// automatically, since the payment may already be in progress at the bank.
// A request that is not reserved answers 409.
func (a *App) requestStale(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	if req.Status != "processing" || req.ProcessingBy == nil {
		a.respondError(w, r, http.StatusConflict, "This request is not reserved by anyone.", nil)
		return
	}
	trail, terr := a.mergedTrail(r, req.ID)
	if terr != nil {
		a.respondStoreError(w, r, terr)
		return
	}
	// The banner quotes the configured staleness threshold.
	thresholds, therr := a.st.ReminderThresholds(r.Context())
	if therr != nil {
		a.respondStoreError(w, r, therr)
		return
	}
	a.render(w, r, "reservation_stale", PageData{
		Title:       "Reserved too long",
		Request2:    a.withHolderName(r, req),
		Audit:       trail,
		Reminders:   thresholds,
		ReserveMine: heldByCaller(req, auth.CurrentUser(r).ID),
	})
}

// mergedTrail returns the audit history of a request and of every payment
// recorded against it, oldest first (ties broken by id).
func (a *App) mergedTrail(r *http.Request, requestID int64) ([]store.AuditEntry, error) {
	entries, err := a.st.Audit(r.Context(), "payment_request", requestID, 200)
	if err != nil {
		return nil, err
	}
	payments, err := a.st.RequestPayments(r.Context(), requestID)
	if err != nil {
		return nil, err
	}
	for _, payment := range payments {
		pay, perr := a.st.Audit(r.Context(), "payment", payment.ID, 200)
		if perr != nil {
			return nil, perr
		}
		entries = append(entries, pay...)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})
	return entries, nil
}
