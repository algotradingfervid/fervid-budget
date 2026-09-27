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

// paymentForm starts recording a payment. Every payment belongs to an
// approved request, so without ?request= it shows the picker of requests to
// pay; with ?request= it shows the entry form for a request the caller has
// reserved.
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

// payments shows the payment ledger for a month, filtered by status (default
// "active") and search text, with the total of the payments that are not
// voided. The caller's payment scope decides which payments are listed: "own"
// and "assigned" show only payments they entered.
func (a *App) payments(w http.ResponseWriter, r *http.Request) {
	month := validMonthOrCurrent(r.URL.Query().Get("month"))
	status := queryDefault(r, "status", "active")
	q := r.URL.Query().Get("q")
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

// paymentCreate records a payment against a request the caller has reserved
// (this is the only way users create payments). "settlement" is "settled"
// (the request is paid in full and closes) or "partial" (the approver is asked
// to accept the shortfall, which also needs payment:mark_partial). An optional
// proof-of-payment file is saved with it.
//
// Pressing confirm twice (back button, double tap) does not fail: when this
// request already has the payment from the same form, the user goes to it,
// with a note that nothing new was saved.
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
		if pay, perr := a.st.PaymentBySubmission(r.Context(), linkedID, u.ID, in.SubmissionKey); perr == nil {
			a.redirectToPayment(w, r, pay.ID, "duplicate")
			return
		}
		a.settlementError(w, r, linkedID, in, r.FormValue("amount"), settlement, partialReason, err)
		return
	}
	// A settled payment tells the requester the request is closed; a partial
	// one asks the approver to accept the shortfall.
	if settlement == "settled" {
		a.fire(r, notify.EventPaymentSettled, linkedID)
	} else if settlement == "partial" {
		a.fire(r, notify.EventPaymentPartialReview, linkedID)
	}
	a.redirectToPayment(w, r, payID, "saved")
}

// paymentOutcomeCookie carries how the payment page was reached ("saved",
// "duplicate" or "immutable") across the redirect, tied to one payment id. A
// cookie rather than a query parameter keeps the URL /payments/{id}, so a
// refresh, bookmark or shared link never shows "Payment saved" again. It is
// read once and cleared.
const paymentOutcomeCookie = "fervid_payment_outcome"

// paymentOutcomes are the outcomes the payment page knows.
var paymentOutcomes = map[string]bool{"saved": true, "duplicate": true, "immutable": true}

// redirectToPayment sends the user to the payment's page, carrying outcome
// in a five-minute cookie (see paymentOutcomeCookie).
func (a *App) redirectToPayment(w http.ResponseWriter, r *http.Request, payID int64, outcome string) {
	http.SetCookie(w, &http.Cookie{Name: paymentOutcomeCookie, Value: fmt.Sprintf("%s:%d", outcome, payID),
		Path: "/payments", MaxAge: 300, HttpOnly: true, Secure: a.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", payID), http.StatusSeeOther)
}

// takePaymentOutcome reads and clears the one-time outcome. A cookie for a
// different payment, or with an unknown outcome, is ignored.
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

// paymentDetail shows one payment. A payment linked to a request shows the
// request too (what was approved beside what was paid, both documents lists,
// and one history from submission to settlement) and offers no changes. An
// older payment without a request shows its own history and whether its month
// is locked.
//
// Whoever may not read the payment gets the same 404 as for a missing one: for
// a linked payment that is anyone whose request scope does not reach the
// request, otherwise anyone whose payment scope does not reach the payment.
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
		if !a.canSeeRequest(r, req) {
			a.respondError(w, r, http.StatusNotFound, "The requested record was not found.",
				fmt.Errorf("payment %d belongs to request %d, outside the caller's data scope", p.ID, req.ID))
			return
		}
		trail, terr := a.mergedTrail(r, req.ID)
		if terr != nil {
			a.respondStoreError(w, r, terr)
			return
		}
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

// loadReadablePayment loads the payment named in the URL path. A payment the
// caller may not read gets the same 404 as a missing one. ok is false when an
// error response has already been written.
func (a *App) loadReadablePayment(w http.ResponseWriter, r *http.Request) (p store.Payment, ok bool) {
	p, err := a.st.Payment(r.Context(), pathID(r))
	if err == nil && !a.canReadPayment(r, p) {
		err = store.ErrNotFound
	}
	if err != nil {
		a.respondStoreError(w, r, err)
		return store.Payment{}, false
	}
	return p, true
}

// paymentEditPageData is what the edit form needs besides the payment itself:
// the heads to choose from and the payment-mode options (which always include
// the payment's own mode).
func (a *App) paymentEditPageData(r *http.Request, p store.Payment) (PageData, error) {
	heads, err := a.st.ListHeads(r.Context(), true)
	if err != nil {
		return PageData{}, err
	}
	choices, err := a.paymentEditChoices(r, p.PaymentMode)
	if err != nil {
		return PageData{}, err
	}
	return PageData{Title: "Edit Payment", Payment: p, Heads: heads, PaymentModeChoices: choices}, nil
}

// paymentEditForm shows the edit form for a payment that is not linked to a
// request. A payment that settled a request can never be edited (the store
// refuses it), so that URL redirects to the payment's page instead.
func (a *App) paymentEditForm(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadReadablePayment(w, r)
	if !ok {
		return
	}
	if p.RequestID != nil {
		a.redirectToPayment(w, r, p.ID, "immutable")
		return
	}
	// Older rows may spell the mode "Bank transfer" or "NEFT"; the form's
	// options carry the stored key, so the saved mode stays selected.
	p.PaymentMode = canonicalPaymentMode(p.PaymentMode)
	data, err := a.paymentEditPageData(r, p)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// Month is the payment's own, so "Back to grid" returns to that month.
	month := p.PaidOn[:7]
	data.Month, data.Locked = month, a.st.IsLocked(r.Context(), month) || p.VoidedAt != nil
	a.render(w, r, "payment_edit_form", data)
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

// paymentEdit saves the edit form, with an optional new attachment. A refusal
// the user can fix shows the form again with what they typed and the reason.
func (a *App) paymentEdit(w http.ResponseWriter, r *http.Request) {
	payment, ok := a.loadReadablePayment(w, r)
	if !ok {
		return
	}
	id := payment.ID
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
		status, fixable := userError(err)
		if !fixable {
			a.respondStoreError(w, r, err)
			return
		}
		p := paymentFormValues(in)
		p.ID = id
		data, dataErr := a.paymentEditPageData(r, p)
		if dataErr != nil {
			a.respondStoreError(w, r, dataErr)
			return
		}
		data.PaymentAmount, data.Error = r.FormValue("amount"), friendly(err)
		data.Locked = validDateInput(in.PaidOn) && a.st.IsLocked(r.Context(), in.PaidOn[:7])
		a.renderStatus(w, r, status, "payment_edit_form", data)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", id), http.StatusSeeOther)
}

// paymentVoid voids a payment with the given reason, then returns to the
// payments page named in "next" or to the payment itself.
func (a *App) paymentVoid(w http.ResponseWriter, r *http.Request) {
	payment, ok := a.loadReadablePayment(w, r)
	if !ok {
		return
	}
	id := payment.ID
	if err := a.st.VoidPayment(r.Context(), auth.CurrentUser(r), id, r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if next := r.FormValue("next"); strings.HasPrefix(next, "/payments") {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/payments/%d", id), http.StatusSeeOther)
}

// canReadPayment reports whether the signed-in user may read p, which decides
// the payment edit, void and attachment routes. A payment linked to a request
// is readable when the user's request scope reaches that request, because its
// page and files show the request too. A payment without a request needs
// payment:view and a payment scope that reaches it.
func (a *App) canReadPayment(r *http.Request, p store.Payment) bool {
	return a.userReadsPayment(r.Context(), auth.CurrentUser(r), p)
}

// paymentScopeReaches applies the ledger's payment-scope rule to one payment,
// so a payment hidden from the ledger cannot be opened by id either: "all"
// reaches every payment, "own" and "assigned" only payments the viewer
// entered, and anything else (including no scope, "None" in the role
// editor) reaches nothing.
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

// paymentInput reads the payment form. It refuses (as a validation error) a
// malformed balance check value, an amount that does not parse, and, for a
// payment against a request, a blank transaction reference.
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
	return in, nil
}

// paymentFormValues turns what was typed into the edit form back into a
// Payment, so a refused edit can show the form again as the user left it.
func paymentFormValues(in store.PaymentInput) store.Payment {
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
