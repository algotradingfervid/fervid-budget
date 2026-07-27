package app

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/money"
	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

// Payment linking, reservation and settlement (Phase 3).
//
// Three rules shape this file.
//
// S1/S2 — a payment is only ever created by linking it to one approved request
// the actor holds. Reservation is atomic in the store; the screens here never
// offer a control the store would refuse.
//
// G15 — losing the reservation race is not an error, it is a screen. Every path
// that discovers somebody else holds the request funnels into
// reservationConflict, so the queue, the picker and the entry form all say the
// same thing in the same words.
//
// D8 — the settlement preview is pure. It re-checks the reservation, computes
// the comparison and renders the sheet. Only POST /payments writes.

// ---------------------------------------------------------------------------
// Shared plumbing
// ---------------------------------------------------------------------------

// withHolderName fills in ProcessingByName. store.Request does not join the
// reserver's user row — only LinkablePaymentRequests does — so every screen that
// names the holder resolves it here rather than growing its own join.
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

// Conflict causes for the reservation screen. ReserveRequest refuses for three
// different reasons and used to return one ErrForbidden for all of them, so the
// screen said "Someone else took this request before you" about a request whose
// status was 'approved' and whose processing_by was NULL (F-D-02). Wave 1 gave
// each cause its own sentinel; these are what the template branches on, so the
// mapping from a store error to a sentence lives in exactly one place.
const (
	conflictTaken       = "taken"        // somebody really does hold it
	conflictHold        = "hold"         // approved, but paused by Accounts (L7)
	conflictNotApproved = "not-approved" // any other status: completed, cancelled, frozen…
)

// reservationConflict renders G15: the losing accountant gets a screen naming
// what actually happened, not an error page. It is the single place a refused
// reservation is presented, so the queue, the picker and the payment form all
// agree.
func (a *App) reservationConflict(w http.ResponseWriter, r *http.Request, req store.Request, cause error) {
	req = a.withHolderName(r, req)
	holder := req.ProcessingByName
	if holder == "" {
		holder = "Someone else"
	}
	a.log.WarnContext(r.Context(), "reservation conflict",
		"request_id", requestID(r), "request", req.ID, "holder", holder,
		"cause", conflictCause(req, cause), "error", cause)
	title := "Already taken"
	switch conflictCause(req, cause) {
	case conflictHold:
		title = "On hold"
	case conflictNotApproved:
		title = "Not available to process"
	}
	a.renderStatus(w, r, http.StatusConflict, "reservation_conflict", PageData{
		Title:         title,
		Request2:      req,
		Holder:        holder,
		ConflictCause: conflictCause(req, cause),
	})
}

// conflictCause reads the sentinel and the row together, because neither alone is
// enough.
//
// The sentinel says which test the conditional UPDATE failed; the row says what
// the request actually is. They can disagree, and the disagreement is the whole
// finding: RecordPaymentForRequest leaves processing_by set on a completed
// request, so ReserveRequest's cause check — which asks about processing_by
// before it asks about the status — answers ErrAlreadyReserved for a request that
// was paid last month. "Someone else is paying it" is not true of that request,
// so the row wins on the question of whether anybody holds it.
//
// The row is also all there is on the two paths that discover a conflict by
// reading rather than by being refused: paymentEntry and settlementPreview both
// arrive with a nil cause.
func conflictCause(req store.Request, cause error) string {
	switch {
	case activeHold(req) || errors.Is(cause, store.ErrRequestOnHold):
		return conflictHold
	case req.Status == "processing" && req.ProcessingBy != nil:
		return conflictTaken
	case req.Status == "approved":
		// Approved, unheld and unreserved by the time the row was re-read: the
		// caller lost the race and the winner has already let go, so the race is
		// still the honest answer.
		return conflictTaken
	default:
		return conflictNotApproved
	}
}

// heldByCaller answers the one question every settlement screen asks first:
// is this request reserved, right now, by the person looking at it?
func heldByCaller(req store.Request, userID int64) bool {
	return req.Status == "processing" && req.ProcessingBy != nil && *req.ProcessingBy == userID
}

// requestRecordPayment is the reservation entry point. It is gated on
// reservation:reserve rather than payment:create because taking work out of the
// queue is a different act from recording money leaving the bank.
func (a *App) requestRecordPayment(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	u := auth.CurrentUser(r)
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

// queueTab is one `.segmented` tab on the accounts queue. Its Key is the store's
// LinkableOptions.Status, so a tab can never ask for a set the store does not
// know how to build.
type queueTab struct{ Key, Label string }

// The Approved tab is labelled the way the metric strip above it labels the same
// number, because the number and the rows answer different questions and always
// will: the badge is the strictly takeable set (approved · unclaimed · not on
// hold) while the rows also include the ones somebody is already paying, so the
// picker can render them as .co.is-taken instead of silently hiding a request
// from the person about to duplicate it. Labelled "Approved", the badge looked
// like an undercount of its own list (F-G-008); labelled this way it is visibly
// counting something narrower.
var queueTabs = []queueTab{
	{"approved", "Approved, unclaimed"}, {"processing", "Processing"}, {"hold", "On hold"},
	{"partial_review", "Partial review"}, {"paid", "Paid"},
}

// SettlementPreview is a view model only. It is computed, rendered and thrown
// away; nothing here is persisted (D8).
type SettlementPreview struct {
	Approved      int64
	Paid          int64
	Difference    int64 // approved - paid; > 0 means under-paid
	Match         bool
	Settlement    string
	PartialReason string
	Fields        map[string]string // every entry-form field, carried to the confirm POST
}

// PaidRequestRow is one line of "Recently paid by you": enough to render the row
// and link to the payment. It lives here rather than in the store because the
// store layer is closed for this phase's UI work; it is assembled from
// ListPayments plus the request each payment points at.
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

// accountsQueue is the Accounts work queue. Every tab is a store status filter
// and every count spans the caller's whole scope, so typing in the search box
// narrows the rows and never moves the numbers above them.
func (a *App) accountsQueue(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = queueTabs[0].Key
	}
	known := false
	for _, t := range queueTabs {
		if t.Key == tab {
			known = true
		}
	}
	if !known {
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

// pickerData is the no-request branch of GET /payments/new and the fragment
// behind it, built once so the two deliveries cannot disagree about what is
// takeable.
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

// recentPaidByActor assembles "Recently paid by you" from the ledger. There is
// no store method joining payments to their requests, and the store layer is
// closed for this phase's UI work, so the join happens here: read the ledger
// newest-first, keep the caller's own linked rows, and name each one from the
// request it settled.
func (a *App) recentPaidByActor(r *http.Request, actorID int64, limit int) ([]PaidRequestRow, error) {
	payments, err := a.st.ListPayments(r.Context(), store.PaymentListOptions{Status: "all", Limit: 200})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(payments, func(i, j int) bool { return payments[i].CreatedAt.After(payments[j].CreatedAt) })
	out := make([]PaidRequestRow, 0, limit)
	for _, p := range payments {
		if len(out) == limit {
			break
		}
		if p.EnteredBy != actorID {
			continue
		}
		// ListPayments does not select the linkage columns — RequestID is
		// always nil on its rows — so the linkage is re-read per candidate.
		linked, perr := a.st.Payment(r.Context(), p.ID)
		if perr != nil {
			return nil, perr
		}
		if linked.RequestID == nil {
			continue
		}
		req, rerr := a.st.Request(r.Context(), *linked.RequestID)
		if rerr != nil {
			return nil, rerr
		}
		out = append(out, PaidRequestRow{
			PaymentID: linked.ID, RequestID: req.ID, Number: req.Number,
			Payee: linked.VendorPayee, Amount: linked.Amount, PaidOn: linked.PaidOn,
			Settlement: linked.Settlement, Status: req.Status,
		})
	}
	return out, nil
}

// paymentEntry is the with-request branch of GET /payments/new: the reserve
// bar, what was approved, and the one money field the accountant fills in.
// Nothing on this screen writes — the primary button opens the settlement step.
// Opening somebody else's reservation is the conflict screen, not a 403: the
// reader needs to know who has it and what they can do instead (G15).
func (a *App) paymentEntry(w http.ResponseWriter, r *http.Request, linkedID int64) {
	u := auth.CurrentUser(r)
	req, err := a.st.Request(r.Context(), linkedID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if !heldByCaller(req, u.ID) {
		a.reservationConflict(w, r, req, nil)
		return
	}
	headID := int64(0)
	if req.HeadID != nil {
		headID = *req.HeadID
	}
	// The lock, before the form rather than after it. The grid renders "Locked"
	// in place of its Add buttons and /budgets carries a locked banner; this
	// screen carried nothing, so an accountant filled in the amount, the date,
	// the mode, the reference and the note and only learned the period was closed
	// on the confirmation (F-G-020). The month tested is the one the date field
	// opens on, which is the month a settlement recorded now would land in.
	paidOn := time.Now().Format("2006-01-02")
	a.render(w, r, "payment_form", PageData{
		Title:          "Record payment",
		Request2:       req,
		SelectedHeadID: headID,
		ReserveMine:    true,
		Month:          paidOn[:7],
		Locked:         a.st.IsLocked(r.Context(), paidOn[:7]),
		Payment: store.Payment{
			HeadID: headID,
			PaidOn: paidOn,
			Amount: approvedOf(req),
			// The display payee, not the snapshot column: a vendor_invoice names
			// its payee with vendor_id and leaves vendor_payee empty, so the
			// snapshot would write a payment with nobody to pay.
			VendorPayee: req.Vendor,
			InvoiceNo:   req.InvoiceNo,
		},
	})
}

func (a *App) paymentPickerOptions(w http.ResponseWriter, r *http.Request) {
	data, err := a.pickerData(r)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.renderPartial(w, r, "payment_pick_options", data)
}

// settlementPreview is pure (D8). No BeginTx, no attachment staging, no writes
// of any kind: it re-checks the reservation, computes approved-against-paid and
// renders the sheet. The payment genuinely does not exist while the sheet is on
// screen, which is what lets it say "Not saved yet" honestly. The only writer in
// this flow is POST /payments.
func (a *App) settlementPreview(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	id := pathID(r)
	req, err := a.st.Request(r.Context(), id)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// Losing the reservation between entry and confirmation is a screen, not an
	// error page (G15) — the accountant needs to know who holds it now.
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
	approved := approvedOf(req)
	a.renderSettlement(w, r, http.StatusOK, req, SettlementPreview{
		Approved:   approved,
		Paid:       paid,
		Difference: approved - paid,
		Match:      approved == paid,
		Settlement: "settled",
		Fields:     settlementFields(r),
	}, "")
}

// settlementFields snapshots the entry form so the confirmation can post every
// value in one request. The file input is deliberately absent — it lives in the
// live form on the htmx path, and is re-offered on the no-JS confirmation.
func settlementFields(r *http.Request) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"amount", "paid_on", "payment_mode", "reference_no", "invoice_no", "remarks", "vendor_payee", "head_id"} {
		if v := r.FormValue(k); v != "" {
			out[k] = v
		}
	}
	return out
}

// renderSettlement is the one place the sheet is delivered, so the htmx fragment
// and the no-JS page can never disagree about what the confirmation says. Same
// markup, two wrappers.
func (a *App) renderSettlement(w http.ResponseWriter, r *http.Request, status int, req store.Request, p SettlementPreview, errMsg string) {
	name := "settlement_confirm"
	if isFragmentRequest(r) {
		name = "settlement_sheet"
	}
	a.renderStatus(w, r, status, name, PageData{
		Title: "Confirm the payment", Request2: a.withHolderName(r, req), Settlement: p, Error: errMsg,
	})
}

// settlementError re-renders the confirmation sheet with the message in place,
// so a rejected settlement never throws the accountant onto an error page and
// never loses the figures they typed. Server faults still take the error page —
// re-rendering a sheet over a broken database would be a lie.
func (a *App) settlementError(w http.ResponseWriter, r *http.Request, linkedID int64, in store.PaymentInput, rawAmount, settlement, partialReason string, cause error) {
	status := storeErrorStatus(cause)
	if status >= http.StatusInternalServerError {
		a.respondStoreError(w, r, cause)
		return
	}
	req, rerr := a.st.Request(r.Context(), linkedID)
	if rerr != nil {
		a.respondStoreError(w, r, rerr)
		return
	}
	approved := approvedOf(req)
	// The typed amount is echoed back even when it is what was rejected: a
	// malformed figure parses to zero here and the field below still shows the
	// characters the accountant actually entered.
	paid, _ := money.ParsePaise(rawAmount)
	if in.Amount != 0 {
		paid = in.Amount
	}
	a.renderSettlement(w, r, status, req, SettlementPreview{
		Approved: approved, Paid: paid, Difference: approved - paid, Match: approved == paid,
		Settlement: settlement, PartialReason: partialReason, Fields: settlementFields(r),
	}, friendly(cause))
}

// ---------------------------------------------------------------------------
// The manager's partial review (S11, G14). Accounts can close a request that
// settles; the one outcome it cannot close is money still owed, so that request
// stops here and a person decides. Both decisions are permanent and neither
// moves any money: accepting writes off the balance, raising a concern keeps
// the request open. Nothing on this screen can amend the payment (S12).
// ---------------------------------------------------------------------------

// requestPartialReview renders the screen. The trail spans two entities — the
// request and the payment recorded against it — which is why it is built from
// mergedTrail rather than the request's own thread.
func (a *App) requestPartialReview(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	// The screen exists only while money is genuinely still owed. Off that one
	// status every sentence on it is false — "Still owed to the vendor ₹0.00",
	// a quoted reason nobody gave, and two sheets offering to decide something
	// already decided — so the request itself answers instead.
	if req.Status != "partial_review" {
		http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
		return
	}
	pay, err := a.st.PaymentForRequest(r.Context(), req.ID)
	if err != nil {
		// No payment means nothing to review. Say so in words rather than
		// rendering a screen whose whole subject is missing.
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
	trail, err := a.mergedTrail(r, req.ID, pay.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// The bank advice sits in the read-only card, because "is this shortfall
	// legitimate" is a question about the proof as much as the figures.
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

// TrailLine is one line of the partial review's single stream. The screen is a
// decision, and a decision is read as a story: three sources — the request's
// audit, the payment's audit and the conversation — have to be interleaved by
// time, because rendering them as two blocks sinks every comment below every
// event and puts a message written on Monday under a payment made on Friday.
type TrailLine struct {
	// Comment distinguishes the words somebody wrote from the events the system
	// recorded; they are the same stream but not the same markup.
	Comment   bool
	Action    string
	ActorID   int64
	ActorName string
	Body      string
	CreatedAt time.Time
}

// partialTrail merges the two-entity audit trail with the conversation, oldest
// first. It drops the audit rows the conversation already renders in full:
// commenting, attaching and raising a concern each write an audit line as well
// as the thing itself — RaiseConcern copies the whole comment into its summary —
// so keeping both would print those sentences twice. It is the same rule
// store.RequestThread applies to the request's own stream.
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
	// Stable, so an event and a comment sharing one second keep the order they
	// were merged in — the audit rows arrive already tie-broken on id, which is
	// the only strictly monotonic record of what happened first.
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// requestAcceptPartial closes the request as completed_partial — a terminal
// state deliberately distinct from a clean completed (G14), so a balance that
// was written off stays visible for the life of the record.
func (a *App) requestAcceptPartial(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := a.st.AcceptPartial(r.Context(), auth.CurrentUser(r), id, r.FormValue("note")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// The requester learns the balance is never coming and the accountant learns
	// to stop chasing it. Nothing told either of them before (F-F-06).
	a.fire(r, notify.EventPaymentPartialAccepted, id)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", id), http.StatusSeeOther)
}

// requestRaiseConcern keeps the request in review and puts the manager's words
// in the conversation. It reverses nothing: the money has already left.
func (a *App) requestRaiseConcern(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := a.st.RaiseConcern(r.Context(), auth.CurrentUser(r), id, r.FormValue("comment")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// The concern waits on Accounts, so Accounts has to be told (F-F-06).
	a.fire(r, notify.EventPaymentPartialConcern, id)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d/partial-review", id), http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Giving a reservation up (S6, S7, G11, G12). One screen, one choice, two
// POSTs: release it back to the queue, or hand it to a named colleague. Both
// demand a reason, because the requester and the approver are told why their
// money stopped moving, and both demand the same confirmation, because the one
// thing a reservation protects against is two people paying the same invoice.
//
// The browser reveals the reassign target with [data-when]; nothing about that
// is a rule. Every field the reveal implies is re-checked here, so a hand-rolled
// POST cannot reassign without a target any more than it can release without a
// reason.
// ---------------------------------------------------------------------------

// reservationForm renders the screen. It is deliberately not gated on holding
// the reservation — the conflict screen sends the loser here to ask for it to be
// reassigned — but it is gated on being able to do one of the two things it
// offers, so nobody is shown a page whose every button would answer 403. The
// route itself only requires a session: release and reassign are independent
// grants, and a middleware gate can ask for one verb, not for either.
//
// The request is loaded through loadViewableRequest rather than the store
// directly, because holding a reservation verb somewhere is not permission to
// read this request (Q5/R6) — and the screen prints its number, its payee and
// its whole history.
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
	trail, terr := a.mergedTrail(r, req.ID, 0)
	if terr != nil {
		a.respondStoreError(w, r, terr)
		return
	}
	// The tab names the decision this reader can actually take: somebody who
	// only holds reassign is not here to release anything.
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

// reassignCandidates is who a reservation may be handed to: the active users,
// other than whoever holds it now, who can actually work the Accounts queue.
// The first two rules are the store's — it refuses a deactivated target, and it
// refuses handing a reservation to the person already holding it — so the select
// never offers a name the POST would bounce. The exclusion is the holder rather
// than the caller, because an administrator clearing somebody else's stale
// reservation may legitimately take it over.
//
// canWorkTheQueue is this layer's own rule, and it is not cosmetic. A
// reservation parked on somebody without payment:process is a request nobody can
// move: the new holder gets 403 on /accounts-queue and on this screen, and
// anybody else re-reserving gets 409. Only a second reassignment recovers it.
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

// canWorkTheQueue is the gate on /accounts-queue, asked about somebody else.
// payment:process is the grant that puts a person in front of the reserved rows,
// which is the one thing a new holder must be able to reach.
func canWorkTheQueue(am *auth.Manager, u store.User) bool {
	return u.Active && am.Can(u, "payment", "process")
}

// reservationTrail keeps the three actions this screen is a history of. The
// filter lives here rather than in the template because {{range}}…{{else}} fires
// on an empty slice, not on a filter that matched nothing: filtering inline
// would mean "No reservation history yet." never printed, because every request
// that reaches this screen already carries a submit and an approval.
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

// requestRelease puts the request back in the open queue. Both refusals it can
// meet — no reason (G12) and no confirmation (S7) — are the store's, so the same
// rule holds whether the release arrives from this screen or from a script.
// authorized is reservation:reassign, because releasing work that is not yours
// is taking it off somebody, which is the verb that grants exactly that.
//
// The request is resolved through loadViewableRequest for the same reason the
// screen is: a data scope that does not reach a request is not widened by
// holding a reservation verb.
func (a *App) requestRelease(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	u := auth.CurrentUser(r)
	if err := a.st.ReleaseRequest(r.Context(), u, req.ID, r.FormValue("reason"),
		r.FormValue("confirm") == "on", a.auth.Can(u, "reservation", "reassign")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// The screen's action bar states that the requester and the approver are both
	// notified. It is now true (F-D-12/F-F-06): the seeded rule for this event
	// includes both, and the invoice being unclaimed again is exactly the kind of
	// interruption a hold already told them about.
	a.fire(r, notify.EventReservationReleased, req.ID)
	http.Redirect(w, r, "/accounts-queue", http.StatusSeeOther)
}

// requestReassign hands the reservation to somebody else. The target select is
// revealed by data-when; this handler is what enforces it, and the confirmation
// is demanded here rather than in the store because ReassignReservation has no
// confirmed parameter — the screen asks the same question for both branches, so
// the same answer is required for both.
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
	// The select only offers people who can work the queue; this is what makes
	// that a rule. Handing a reservation to somebody who cannot open the
	// Accounts queue strands the request in processing with no in-app way out.
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
	// Same promise as release, same audience, plus the colleague who is now
	// expected to pay it (F-D-12/F-F-06).
	a.fire(r, notify.EventReservationReassigned, req.ID)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Holding a request, and the reservation that has been open too long (L7, Q6).
//
// A hold is the one pause in the whole flow that is not a decision: nothing is
// rejected, nothing is released, the request simply stops being payable until
// the question Accounts asked is answered. It is therefore a state of the
// request screen rather than a screen of its own — the requester reads the
// question where they read everything else, and answers in the same comment box.
//
// L7 is the rule underneath: the request stays 'approved', so the queue's own
// availability test (approved · unclaimed · not on hold) takes it out of the
// takeable set without inventing a status for it. Only payment:hold lifts it.
// ---------------------------------------------------------------------------

// requestHold pauses payment with the question that caused it. The reason is
// mandatory in the store, so an empty one is refused whether it arrives from
// the sheet or from a script.
func (a *App) requestHold(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	if err := a.st.HoldRequest(r.Context(), auth.CurrentUser(r), req.ID, r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.fire(r, notify.EventRequestOnHold, req.ID)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
}

// requestUnhold puts it back in the queue. No reason is required: the hold
// itself is the thing that needed explaining, and lifting it is answering the
// question rather than asking a new one.
func (a *App) requestUnhold(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	if err := a.st.UnholdRequest(r.Context(), auth.CurrentUser(r), req.ID); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// request_on_hold told the requester the pause began; this is the other half,
	// and without it nobody was ever told the pause was over (F-D-12/F-F-06).
	a.fire(r, notify.EventRequestUnheld, req.ID)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
}

// requestStale is the 26-hour nudge (Q6). Nothing here releases anything and
// nothing on the screen is automatic: a reservation that has been open a day
// may well be a transfer already moving through a bank portal, so the screen
// only names the four things a person may decide to do next.
//
// Loaded through loadViewableRequest for the same reason the release screen is:
// holding payment:process is permission to work the queue, not permission to
// read a request outside the caller's data scope (Q5/R6).
func (a *App) requestStale(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	// Off 'processing' every sentence on this screen is false — there is no
	// holder to name, no elapsed time to count and no reservation to give up.
	if req.Status != "processing" || req.ProcessingBy == nil {
		a.respondError(w, r, http.StatusConflict, "This request is not reserved by anyone.", nil)
		return
	}
	trail, terr := a.mergedTrail(r, req.ID, 0)
	if terr != nil {
		a.respondStoreError(w, r, terr)
		return
	}
	// The banner quotes the staleness threshold, so it reads the configured one
	// rather than asserting "the one-day mark" an admin may have changed (F-F-03).
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

// ---------------------------------------------------------------------------
// Display helpers. Registered in the FuncMap, so a time or an amount is spelled
// for a person in exactly one place.
// ---------------------------------------------------------------------------

func hhmm(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Local().Format("15:04")
}

func since(t *time.Time) string {
	if t == nil {
		return ""
	}
	d := time.Since(*t)
	if d < time.Hour {
		if m := int(d.Minutes()); m > 0 {
			return fmt.Sprintf("%d m", m)
		}
		return "just now"
	}
	return fmt.Sprintf("%d h", int(d.Hours()))
}

// reservedLabel is the clock time while the reservation is same-day and the
// elapsed time once it is older. "· 14:02" answers "when did I start?"; "· 26 h"
// answers "how long has this been sitting?".
func reservedLabel(t *time.Time) string {
	if t == nil {
		return ""
	}
	if time.Since(*t) < store.StaleReservation {
		return hhmm(t)
	}
	return since(t)
}

// stale is the predicate reservedLabel switches on, exposed on its own so a row
// that has crossed the one-day mark can be linked to the nudge screen (Q6)
// rather than merely labelled with the elapsed hours. The count in
// LinkableSet.Counts is an aggregate over the whole scope and cannot say which
// row it means; this can.
func stale(t *time.Time) bool {
	return t != nil && time.Since(*t) >= store.StaleReservation
}

// activeHold is the hold as a reader experiences it, rather than the column.
// Every writer that moves a request off 'approved' clears the hold, so the two
// are the same thing on a well-formed row — but the pill, the waiting line and
// the release control all lie the moment they disagree, and a row written
// before that rule existed is exactly when they would. Asking one question in
// one place is what keeps the answer from drifting between them.
func activeHold(req store.Request) bool {
	return req.OnHold && req.Status == "approved"
}

func subPaise(a, b int64) int64 { return a - b }

// approvedOf is the ceiling a payment is measured against: what the approver
// signed off, falling back to what was asked when nothing was recorded.
func approvedOf(req store.Request) int64 {
	if req.ApprovedAmount != nil {
		return *req.ApprovedAmount
	}
	return req.Amount
}

// trailAction disambiguates an audit row before it is decorated or spelled. The
// full trail merges two entity types, and "create" or "update" on a payment is a
// different sentence from the same word on a request — so the entity is folded
// into the action once, here, rather than in every switch downstream. Every name
// it invents is one no store writer uses, so nothing else changes meaning.
func trailAction(e store.AuditEntry) string {
	if e.EntityType != "payment" {
		return e.Action
	}
	switch e.Action {
	case "create":
		return "record_payment"
	case "update":
		return "amend_payment"
	case "attach":
		return "attach_payment"
	default:
		return e.Action
	}
}

// auditTone and auditGlyph decorate one `.tl-dot` of a trail built from audit
// rows. They are deliberately not threadDot/threadGlyph: those two take a
// store.ThreadEntry and belong to the request conversation, and one name meaning
// two things is how they drift.
func auditTone(action string) string {
	switch action {
	// "unhold" sits with the reservation movements: lifting a hold puts the
	// request back in the takeable queue, which is the same kind of event.
	case "submit", "reraise", "process", "reassign", "approval_reassign", "unhold":
		return "brand"
	case "approve", "settle", "accept_partial", "record_payment":
		return "ok"
	case "hold", "mark_partial", "release", "concern", "return":
		return "warn"
	case "reject", "cancel", "withdraw", "void":
		return "bad"
	default:
		return ""
	}
}

func auditGlyph(action string) string {
	switch action {
	case "submit", "reraise", "create":
		return "＋"
	case "approve", "settle", "accept_partial":
		return "✓"
	case "process", "reassign", "unhold":
		return "◷"
	case "hold":
		return "⏸"
	case "mark_partial", "concern", "record_payment":
		return "₹"
	case "release", "return":
		return "↩"
	case "reject", "cancel", "withdraw", "void":
		return "✕"
	case "update", "amend_payment", "approval_reassign":
		return "✎"
	case "attach", "attach_payment":
		// The thread's own upload glyph. Every dot in a `.thread` is a
		// monochrome text symbol the CSS tints with `color:`; an emoji ignores
		// that and lands as a colour sticker in a column of grey marks.
		return "⇪"
	default:
		return "·"
	}
}

// auditPhrase is the trail's headline, written to follow the actor's name:
// "Priya Nair reserved it for processing". The audit summary underneath carries
// the figures; this line carries the verb, so a reader skimming the left column
// of the thread gets the story without reading a single amount.
func auditPhrase(action string) string {
	switch action {
	case "submit":
		return "submitted the request"
	case "update":
		return "edited the request"
	case "attach":
		return "attached a document"
	case "comment":
		return "commented"
	case "approve":
		return "approved the request"
	case "return":
		return "returned the request for correction"
	case "reject":
		return "rejected the request"
	case "withdraw":
		return "withdrew the request"
	case "reraise":
		return "raised the request again"
	case "cancel_request":
		return "asked for the request to be cancelled"
	case "cancel":
		return "cancelled the request"
	case "process":
		return "reserved it for processing"
	case "release":
		return "released the reservation"
	case "reassign":
		return "reassigned the reservation"
	case "approval_reassign":
		return "reassigned the approval"
	case "hold":
		return "put the request on hold"
	case "unhold":
		return "took the request off hold"
	case "concern":
		return "raised a concern"
	case "settle":
		return "settled the request"
	case "mark_partial":
		return "marked it a partial payment"
	case "accept_partial":
		return "accepted the partial payment"
	case "record_payment":
		return "recorded a payment"
	case "amend_payment":
		return "corrected the payment"
	case "attach_payment":
		return "attached proof of payment"
	case "void":
		return "voided the payment"
	default:
		return actionText(action)
	}
}

// trailBody is the audit summary as the trail prints it, under a head that has
// already named the actor. Every request-side writer composes its summary as
// "<actor> did this", so rendering both put the name on the screen twice; the
// mockup's bodies carry the figures and nothing else. The name is removed only
// when the summary genuinely opens with it — payment-side summaries ("Recorded
// payment ₹98,000.00") never do, and come through untouched.
func trailBody(e store.AuditEntry) string {
	if e.ActorName == "" {
		return e.Summary
	}
	rest := strings.TrimPrefix(e.Summary, e.ActorName+" ")
	if rest == e.Summary || rest == "" {
		return e.Summary
	}
	letters := []rune(rest)
	letters[0] = unicode.ToUpper(letters[0])
	return string(letters)
}

// initials is the two-letter avatar a comment's `.tl-dot` shows.
func initials(name string) string {
	out := make([]rune, 0, 2)
	for _, word := range strings.Fields(name) {
		for _, letter := range word {
			out = append(out, unicode.ToUpper(letter))
			break
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "··"
	}
	return string(out)
}

// paymentModes is the stored vocabulary of payment modes, in the order the form
// offers them. paymentMode() spells each one for a person.
func paymentModes() []string {
	return []string{"bank_transfer", "cheque", "upi", "cash", "card", "other"}
}

// mergedTrail is the request-to-payment history the settlement screens render.
// There is no store method spanning two entity types, so the two audit reads are
// merged here and ordered oldest-first — a trail is a story, and a story is told
// forwards.
func (a *App) mergedTrail(r *http.Request, requestID int64, paymentID int64) ([]store.AuditEntry, error) {
	entries, err := a.st.Audit(r.Context(), "payment_request", requestID, 200)
	if err != nil {
		return nil, err
	}
	if paymentID > 0 {
		pay, perr := a.st.Audit(r.Context(), "payment", paymentID, 200)
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
