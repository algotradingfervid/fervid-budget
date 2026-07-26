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

// reservationConflict renders G15: the losing accountant gets a screen naming
// the winner, not an error page. It is the single place a lost race is
// presented, so the queue, the picker and the payment form all agree.
func (a *App) reservationConflict(w http.ResponseWriter, r *http.Request, req store.Request, cause error) {
	req = a.withHolderName(r, req)
	holder := req.ProcessingByName
	if holder == "" {
		holder = "Someone else"
	}
	a.log.WarnContext(r.Context(), "reservation conflict",
		"request_id", requestID(r), "request", req.ID, "holder", holder, "error", cause)
	a.renderStatus(w, r, http.StatusConflict, "reservation_conflict", PageData{
		Title:    "Already taken",
		Request2: req,
		Holder:   holder,
	})
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

var queueTabs = []queueTab{
	{"approved", "Approved"}, {"processing", "Processing"}, {"hold", "On hold"},
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
	a.render(w, r, "payment_form", PageData{
		Title:          "Record payment",
		Request2:       req,
		SelectedHeadID: headID,
		ReserveMine:    true,
		Payment: store.Payment{
			HeadID:      headID,
			PaidOn:      time.Now().Format("2006-01-02"),
			Amount:      approvedOf(req),
			VendorPayee: req.VendorPayee,
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
	case "submit", "reraise", "process", "reassign":
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
	case "update", "amend_payment":
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
