package app

// Payment screens: the ledger, recording a payment against a request, the
// payment detail page, editing and voiding, and who may read a payment.

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/money"
	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

// paymentForm has two branches and no third. Without ?request= it is the
// picker: every payment belongs to exactly one approved request, so choosing
// one is the first step rather than an optional extra. With ?request= it is the
// entry screen for a reservation the caller holds.
func (a *App) paymentForm(w http.ResponseWriter, r *http.Request) {
	linkedID := parseID(r.URL.Query().Get("request"))
	if linkedID == 0 {
		data, err := a.pickerData(r)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		a.render(w, r, "payment_pick_request", data)
		return
	}
	a.paymentEntry(w, r, linkedID)
}

func (a *App) payments(w http.ResponseWriter, r *http.Request) {
	month := validMonthOrCurrent(r.URL.Query().Get("month"))
	status := queryDefault(r, "status", "active")
	q := r.URL.Query().Get("q")
	// The `payment` data scope is one of the two scoped resources, and until the
	// viewer reached ListPayments a role built with Payments · Own received the
	// whole ledger (F-A-04/F-G-003). The store mirrors requestWhere: only "own"
	// and "assigned" narrow.
	u := auth.CurrentUser(r)
	payments, err := a.st.ListPayments(r.Context(), store.PaymentListOptions{
		Month: month, Status: status, Query: q,
		Scope: a.auth.Scope(u, "payment"), ViewerID: u.ID,
	})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	var total int64
	for _, payment := range payments {
		if payment.VoidedAt == nil {
			total += payment.Amount
		}
	}
	a.render(w, r, "payments", PageData{Title: "Payments", Month: month, Status: status, Query: q, Payments: payments, PaymentTotal: total, Locked: a.st.IsLocked(r.Context(), month)})
}

// paymentCreate is the only user-reachable way a payment is created, and it
// creates nothing that is not linked to a request the caller holds (X5). The
// free-standing create survives in the store as a seed/import helper only.
func (a *App) paymentCreate(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	linkedID := parseID(r.FormValue("request_id"))
	if linkedID == 0 {
		a.respondError(w, r, http.StatusBadRequest, "Payments must be linked to an approved request.", nil)
		return
	}
	if _, ok := a.loadRequestByID(w, r, linkedID); !ok {
		return
	}
	settlement := r.FormValue("settlement")
	// Writing off a shortfall to the approver's queue is its own decision, and
	// payment:mark_partial is the verb the vocabulary declares for it. It cannot
	// be a route gate because it applies to one value of one field (F-D-10).
	if settlement == "partial" && !a.auth.Can(u, "payment", "mark_partial") {
		a.respondError(w, r, http.StatusForbidden,
			"You do not have permission to record a partial payment.", nil)
		return
	}
	in, err := paymentInput(r)
	partialReason := r.FormValue("partial_reason")
	var attachment *store.AttachmentInput
	var attachmentPath string
	if err == nil {
		attachment, attachmentPath, err = a.stageUploadedAttachment(r)
	}
	var payID int64
	if err == nil {
		payID, err = a.st.RecordPaymentForRequest(r.Context(), u, linkedID, in, settlement, partialReason, attachment)
	}
	if err != nil {
		removeStagedAttachment(a.log, r, attachmentPath)
		// A double-confirm (back button, double tap) must not look like a
		// failure: the payment this request needed already exists, so go to it —
		// saying that nothing new was written, because a repeat carrying a
		// different figure otherwise landed under "Payment saved" (settlement-5).
		if pay, perr := a.st.PaymentBySubmission(r.Context(), linkedID, u.ID, in.SubmissionKey); perr == nil {
			a.redirectToPayment(w, r, pay.ID, "duplicate")
			return
		}
		a.settlementError(w, r, linkedID, in, r.FormValue("amount"), settlement, partialReason, err)
		return
	}
	// A settled payment closes the request; a partial one asks the approver to
	// accept the shortfall. They are different events because they ask
	// different people for different things.
	if settlement == "settled" {
		a.fire(r, notify.EventPaymentSettled, linkedID)
	} else if settlement == "partial" {
		a.fire(r, notify.EventPaymentPartialReview, linkedID)
	}
	a.redirectToPayment(w, r, payID, "saved")
}

// paymentOutcomeCookie carries how the payment screen was reached across the
// redirect from the write — "saved", "duplicate" or "immutable", bound to one
// payment id. A cookie rather than a query token, so the landing URL stays
// /payments/{id} and a refresh, a bookmark or a shared link never replays
// "Payment saved" (settlement-5). It is read once and cleared.
const paymentOutcomeCookie = "fervid_payment_outcome"

var paymentOutcomes = map[string]bool{"saved": true, "duplicate": true, "immutable": true}

func (a *App) redirectToPayment(w http.ResponseWriter, r *http.Request, payID int64, outcome string) {
	http.SetCookie(w, &http.Cookie{Name: paymentOutcomeCookie, Value: fmt.Sprintf("%s:%d", outcome, payID),
		Path: "/payments", MaxAge: 300, HttpOnly: true, Secure: a.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", payID), http.StatusSeeOther)
}

// takePaymentOutcome reads and clears the one-shot outcome. Anything not
// issued by redirectToPayment for this very payment is ignored, never echoed.
func (a *App) takePaymentOutcome(w http.ResponseWriter, r *http.Request, payID int64) string {
	c, err := r.Cookie(paymentOutcomeCookie)
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{Name: paymentOutcomeCookie, Value: "", Path: "/payments", MaxAge: -1,
		HttpOnly: true, Secure: a.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
	outcome, id, ok := strings.Cut(c.Value, ":")
	if !ok || id != strconv.FormatInt(payID, 10) || !paymentOutcomes[outcome] {
		return ""
	}
	return outcome
}

// paymentDetail is one screen with two readings. A payment linked to a request
// is the end of that request's story, so it shows what was approved beside what
// was paid and the whole trail from submission to settlement — and offers
// nothing that would change it (S12). A historical, request-less payment is
// still a ledger row and keeps the pre-Phase-3 screen until Phase 6 redraws the
// ledger (X6).
func (a *App) paymentDetail(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	p, err := a.st.Payment(r.Context(), id)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	atts, err := a.st.Attachments(r.Context(), id)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if p.RequestID != nil {
		req, rerr := a.st.Request(r.Context(), *p.RequestID)
		if rerr != nil {
			a.respondStoreError(w, r, rerr)
			return
		}
		// Half this screen is the request: its number, its people, its whole
		// history. Reading it therefore obeys the same row scope /requests/{id}
		// obeys, or payment:view becomes a way around Q5/R6.
		if !a.canSeeRequest(r, req) {
			// 404, not 403, and the same sentence a missing payment gets. This was
			// the last route where the pair still distinguished "exists but is not
			// yours" from "does not exist", which is the enumeration oracle F-G-002
			// closed on /requests/{id} and Wave 3 closed on both attachment routes.
			// It was never in that finding's scope, so nobody looked at it until two
			// spec agents arrived at it independently from opposite directions.
			a.respondError(w, r, http.StatusNotFound, "The requested record was not found.",
				fmt.Errorf("payment %d belongs to request %d, outside the caller's data scope", p.ID, req.ID))
			return
		}
		// The trail the screen shows spans both entities: the request from
		// submission to approval, then the payment from reservation to
		// settlement. There is no store read across entity types, so the two
		// audit listings are merged and ordered oldest-first.
		trail, terr := a.mergedTrail(r, req.ID, p.ID)
		if terr != nil {
			a.respondStoreError(w, r, terr)
			return
		}
		// The proof list is both halves too: the bank advice Accounts uploaded
		// and the invoice the request came in with (payment-detail.html).
		reqAtts, aerr := a.st.RequestAttachments(r.Context(), req.ID)
		if aerr != nil {
			a.respondStoreError(w, r, aerr)
			return
		}
		a.render(w, r, "payment_detail", PageData{
			Title: "Payment · " + req.Number, Payment: p, Request2: req,
			Attachments: atts, RequestAtts: reqAtts, Audit: trail, Outcome: a.takePaymentOutcome(w, r, p.ID),
		})
		return
	}
	// An unlinked payment obeys the payment scope the ledger obeys, as its
	// attachment routes already do through canReadPayment: a row the ledger
	// hides is not reachable by id either (fixwave rbac-2).
	u := auth.CurrentUser(r)
	if !paymentScopeReaches(a.auth.Scope(u, "payment"), u.ID, p) {
		a.respondError(w, r, http.StatusNotFound, "The requested record was not found.",
			fmt.Errorf("payment %d is outside the caller's payment scope", p.ID))
		return
	}
	audit, err := a.st.Audit(r.Context(), "payment", id, 50)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "payment_detail", PageData{Title: "Payment Detail", Payment: p, Attachments: atts, Audit: audit, Locked: a.st.IsLocked(r.Context(), p.PaidOn[:7])})
}

func (a *App) paymentEditForm(w http.ResponseWriter, r *http.Request) {
	if payment, err := a.st.Payment(r.Context(), pathID(r)); err != nil {
		a.respondStoreError(w, r, err)
		return
	} else if !a.canReadPayment(r, payment) {
		a.respondStoreError(w, r, store.ErrNotFound)
		return
	}
	p, err := a.st.Payment(r.Context(), pathID(r))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// S12: a payment that settled a request cannot be edited, and the store
	// refuses the write. Serving the form anyway would let somebody fill in a
	// screen whose Save can only ever fail, so the URL goes where the payment
	// actually lives.
	if p.RequestID != nil {
		a.redirectToPayment(w, r, p.ID, "immutable")
		return
	}
	heads, err := a.st.ListHeads(r.Context(), true)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// Older rows may spell the mode "Bank transfer" or "NEFT"; the form's
	// options carry the stored key, so the saved mode stays selected.
	p.PaymentMode = canonicalPaymentMode(p.PaymentMode)
	choices, err := a.paymentEditChoices(r, p.PaymentMode)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// Month is the payment's own, so "Back to grid" returns to the month this
	// payment belongs to rather than to / — which was the grid before Phase 4 and
	// is the dashboard now (F-G-029).
	a.render(w, r, "payment_edit_form", PageData{Title: "Edit Payment", Payment: p, Heads: heads, PaymentModeChoices: choices,
		Month: p.PaidOn[:7], Locked: a.st.IsLocked(r.Context(), p.PaidOn[:7]) || p.VoidedAt != nil})
}

// paymentEditChoices is the edit form's payment-mode options: the same list the
// settlement form offers, plus the payment's own mode.
func (a *App) paymentEditChoices(r *http.Request, current string) ([]PaymentModeChoice, error) {
	settings, err := a.st.AppSettings(r.Context())
	if err != nil {
		return nil, err
	}
	return paymentModeChoices(settings, current), nil
}

func (a *App) paymentEdit(w http.ResponseWriter, r *http.Request) {
	if payment, err := a.st.Payment(r.Context(), pathID(r)); err != nil {
		a.respondStoreError(w, r, err)
		return
	} else if !a.canReadPayment(r, payment) {
		a.respondStoreError(w, r, store.ErrNotFound)
		return
	}
	id := pathID(r)
	in, err := paymentInput(r)
	var attachment *store.AttachmentInput
	var attachmentPath string
	if err == nil {
		attachment, attachmentPath, err = a.stageUploadedAttachment(r)
	}
	if err == nil {
		err = a.st.UpdatePaymentWithAttachment(r.Context(), auth.CurrentUser(r), id, in, attachment)
	}
	if err != nil {
		removeStagedAttachment(a.log, r, attachmentPath)
		status := storeErrorStatus(err)
		if status >= http.StatusInternalServerError {
			a.respondStoreError(w, r, err)
			return
		}
		heads, headsErr := a.st.ListHeads(r.Context(), true)
		if headsErr != nil {
			a.respondStoreError(w, r, headsErr)
			return
		}
		p := paymentFromInput(in)
		p.ID = id
		choices, choicesErr := a.paymentEditChoices(r, p.PaymentMode)
		if choicesErr != nil {
			a.respondStoreError(w, r, choicesErr)
			return
		}
		locked := validDateInput(in.PaidOn) && a.st.IsLocked(r.Context(), in.PaidOn[:7])
		a.renderStatus(w, r, status, "payment_edit_form", PageData{Title: "Edit Payment", Payment: p, PaymentAmount: r.FormValue("amount"), Heads: heads, PaymentModeChoices: choices, Locked: locked, Error: friendly(err)})
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", id), http.StatusSeeOther)
}

func (a *App) paymentVoid(w http.ResponseWriter, r *http.Request) {
	if payment, err := a.st.Payment(r.Context(), pathID(r)); err != nil {
		a.respondStoreError(w, r, err)
		return
	} else if !a.canReadPayment(r, payment) {
		a.respondStoreError(w, r, store.ErrNotFound)
		return
	}
	id := pathID(r)
	err := a.st.VoidPayment(r.Context(), auth.CurrentUser(r), id, r.FormValue("reason"))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if next := r.FormValue("next"); strings.HasPrefix(next, "/payments") {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", id), http.StatusSeeOther)
}

// canReadPayment is the ownership rule behind every attachment route, and it is
// the same rule paymentDetail applies before rendering a linked payment: half of
// what an attachment discloses belongs to the request, so reading it obeys the
// request's row scope "or payment:view becomes a way around Q5/R6"
// (paymentDetail's own comment, and this is the route it was written about —
// F-A-01/F-A-03).
//
// A request-less historical payment has no row scope to consult, so it falls
// back to the ledger's own verb plus the payment data scope.
func (a *App) canReadPayment(r *http.Request, p store.Payment) bool {
	u := auth.CurrentUser(r)
	if p.RequestID != nil {
		req, err := a.st.Request(r.Context(), *p.RequestID)
		if err != nil {
			return false
		}
		return a.canSeeRequest(r, req)
	}
	return a.auth.Can(u, "payment", "view") &&
		paymentScopeReaches(a.auth.Scope(u, "payment"), u.ID, p)
}

// paymentScopeReaches mirrors ListPayments' scope filter exactly, so a caller
// who cannot see a payment in the ledger cannot see it by id either: "own" and
// "assigned" narrow to what the caller entered, "all" reaches everything, and
// anything else — including the empty scope, the role editor's "None" — reaches
// nothing (fixwave rbac-2).
func paymentScopeReaches(scope string, viewerID int64, p store.Payment) bool {
	switch scope {
	case store.ScopeAll:
		return true
	case "own", "assigned":
		return p.EnteredBy == viewerID
	default:
		return false
	}
}

func paymentInput(r *http.Request) (store.PaymentInput, error) {
	in := store.PaymentInput{
		SubmissionKey: r.FormValue("submission_key"),
		HeadID:        parseID(r.FormValue("head_id")),
		PaidOn:        r.FormValue("paid_on"),
		VendorPayee:   r.FormValue("vendor_payee"),
		PaymentMode:   r.FormValue("payment_mode"),
		InvoiceNo:     r.FormValue("invoice_no"),
		ReferenceNo:   strings.TrimSpace(r.FormValue("reference_no")),
		Remarks:       r.FormValue("remarks"),
	}
	if raw := r.FormValue("expected_paid"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			return in, fmt.Errorf("%w: invalid payment balance; reopen the form", store.ErrValidation)
		}
		in.ExpectedPaid = &value
	}
	amount, err := money.ParsePaise(r.FormValue("amount"))
	in.Amount = amount
	if err != nil {
		return in, fmt.Errorf("%w: invalid amount; enter a valid payment amount", store.ErrValidation)
	}
	if parseID(r.FormValue("request_id")) > 0 && in.ReferenceNo == "" {
		return in, fmt.Errorf("%w: enter a transaction or payment reference; spaces alone are not a reference", store.ErrValidation)
	}
	return in, err
}

func paymentFromInput(in store.PaymentInput) store.Payment {
	return store.Payment{
		HeadID:      in.HeadID,
		PaidOn:      in.PaidOn,
		Amount:      in.Amount,
		VendorPayee: in.VendorPayee,
		PaymentMode: in.PaymentMode,
		InvoiceNo:   in.InvoiceNo,
		ReferenceNo: in.ReferenceNo,
		Remarks:     in.Remarks,
	}
}
