package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/money"
	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

// The payment-request workflow's screens (Phase 2).
//
// Two rules shape everything in this file.
//
// D1 — there are no drafts. Creating and submitting are one atomic POST, so a
// request exists only once it has been sent to an approver, and it takes its
// number at that moment. Nothing here saves a half-finished request.
//
// A16 — the request type is a route parameter, never a form control. The type
// fixes which fieldsets exist; treatment and recoverable category swap the
// middle of the form through an htmx fragment, so there is exactly one copy of
// the conditional-field rules and it lives in Go. `hidden` is not validation:
// every reveal is re-enforced by store.validateRequestInput, which is what a
// hand-rolled POST runs into.

// requestTypeOption is one card on request-new-type.html: the whole vocabulary
// of request types, in the order the approved screen lays them out. Adding a
// type here is the only way to add one to the product.
type requestTypeOption struct {
	Key, Label, Icon, Blurb, Tag, TagClass string
}

var requestTypeOptions = []requestTypeOption{
	{Key: "vendor_invoice", Label: "Vendor invoice payment", Icon: "▤",
		Blurb: "You have an invoice from a vendor and it needs paying.",
		Tag:   "Needs invoice number and date", TagClass: "neutral no-dot"},
	{Key: "vendor_advance", Label: "Vendor advance", Icon: "◷",
		Blurb: "Money to a vendor before any invoice exists — a deposit against an order.",
		Tag:   "Needs a reason for the advance", TagClass: "neutral no-dot"},
	{Key: "reimbursement", Label: "Reimbursement", Icon: "↺",
		Blurb: "You already spent your own money for the company and want it back.",
		Tag:   "Paid to you · needs the expense date", TagClass: "neutral no-dot"},
	{Key: "employee_advance", Label: "Employee advance", Icon: "₹",
		Blurb: "Money to you up front for organisation spending you are about to make.",
		Tag:   "Paid to you · usually recoverable", TagClass: "recoverable"},
	// The store's fifth type, and the only one whose payee is both free and not
	// a vendor: an EMD, PBG, ICD or security deposit is paid to a counterparty
	// or a tender authority and expected back. Until this card existed the only
	// way to raise one was the employee advance, which pays the requester — so a
	// ₹10 lakh inter-corporate deposit was recorded as owed to the employee
	// (form-1 / recoverables-1; phase-2 spec §3, design Figure 2).
	{Key: "recoverable", Label: "Deposit or guarantee", Icon: "⇄",
		Blurb: "EMD, PBG, ICD or a security deposit — money paid to another company or authority that comes back.",
		Tag:   "Paid to the counterparty · always recoverable", TagClass: "recoverable"},
}

// requestTypeLabels is derived from the ordered vocabulary above, so a type is
// named for a human in exactly one place. Its keys are also the only types the
// chooser and the form will render: anything else falls back to the chooser.
var requestTypeLabels = func() map[string]string {
	out := make(map[string]string, len(requestTypeOptions))
	for _, option := range requestTypeOptions {
		out[option.Key] = option.Label
	}
	return out
}()

// unofferedRequestType is what `/requests/new?type=` says when the type names no
// card. A URL naming something the system will not raise is a client error about
// that URL (F-D-14), so it answers 400 on the chooser rather than pretending the
// URL said nothing.
//
// F-D-14's original reconciliation kept the chooser at four cards on the premise
// that "recoverable" was only a treatment every type carried. That premise was
// false — only the employee advance carries the radio, and that type pays the
// requester — so the store's fifth type has its own card now (form-1). The
// message does not echo the requested type and does not name the vocabulary:
// re-typing that list here is how the two drift.
const unofferedRequestType = "That is not a request type this system raises. Pick one below — a deposit or guarantee has its own card, and an advance to yourself is an employee advance."

// requestNew is both steps of the new-request flow on one route. With no ?type=
// it is the chooser; with a known type it is the form for that type and nothing
// else (A16). A type with no card is refused on the chooser, above.
func (a *App) requestNew(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("type")
	label, known := requestTypeLabels[kind]
	if !known {
		if kind != "" {
			a.renderStatus(w, r, http.StatusBadRequest, "request_new_type", PageData{
				Title: "New request", Error: unofferedRequestType,
			})
			return
		}
		a.render(w, r, "request_new_type", PageData{Title: "New request"})
		return
	}
	data, err := a.requestFormData(r, label)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data.FormType = kind
	data.Request2.Type = kind
	data.Request2.Treatment = "budget"
	data.Categories = categoriesForType(kind, data.Categories)
	// An employee advance is money the organisation expects to see accounted
	// for, so it opens on the recoverable treatment already categorised. The
	// requester can still switch it to a budget expense. A deposit or guarantee
	// is recoverable by definition and opens with the category still to choose.
	if kind == "employee_advance" || kind == "recoverable" {
		data.Request2.Treatment = "recoverable"
		data.Request2.RecoverableCategory = normalizeRecoverableCategory("", kind, data.Categories)
	}
	data.RecCategory = resolveRecoverableCategory(data.Request2.RecoverableCategory, data.Categories)
	if data.Vendors, err = a.vendorChoices(r, kind); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "request_form", data)
}

// requestFormFields serves the treatment-dependent middle of the form. The
// browser asks for it on every treatment, category or project change, so there
// is exactly one copy of the conditional-field rules and it lives in Go — a
// fieldset the server did not render is one the requester cannot fill in, and
// validateRequestInput refuses it a second time if they forge it anyway.
func (a *App) requestFormFields(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	data, err := a.requestFormData(r, "")
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data.FormType = q.Get("type")
	data.Request2.Type = data.FormType
	data.Request2.Treatment = "budget"
	data.Categories = categoriesForType(data.FormType, data.Categories)
	if q.Get("treatment") == "recoverable" {
		data.Request2.Treatment = "recoverable"
		data.Request2.RecoverableCategory = normalizeRecoverableCategory(q.Get("recoverable_category"), data.FormType, data.Categories)
		data.RecCategory = resolveRecoverableCategory(data.Request2.RecoverableCategory, data.Categories)
	}
	// Whatever the requester had already typed survives the swap; losing it
	// would make changing a radio button a punishment.
	data.Request2.ProjectID = optionalID(parseID(q.Get("project_id")))
	data.Request2.HeadID = optionalID(parseID(q.Get("head_id")))
	data.Request2.Counterparty = q.Get("counterparty")
	data.Request2.ExpectedReturnDate = q.Get("expected_return_date")
	data.Request2.RepaymentNotes = q.Get("repayment_notes")
	a.renderPartial(w, r, "request_form_fields", data)
}

// normalizeRecoverableCategory keeps the server's idea of the category and the
// rendered <select> in step, against the *live* active categories rather than a
// hardcoded six.
//
// It no longer substitutes a code. An unrecognised or deactivated value used to
// resolve to "emd" — so a request the requester believed was a security deposit
// was silently reclassified as earnest money (F-E-02/F-B-17). It now resolves to
// "nothing chosen", the select renders its "Choose a category" option selected,
// and validateRequestInput refuses the submit until a person picks one. An empty
// value still takes the form type's own default when that category is active,
// which is what opens an employee advance already categorised.
func normalizeRecoverableCategory(code, formType string, cats []store.RecoverableCategory) string {
	for _, c := range cats {
		if c.Code == code {
			return code
		}
	}
	if code != "" {
		return ""
	}
	for _, c := range cats {
		if c.Code == formType {
			return c.Code
		}
	}
	return ""
}

// categoriesForType is the category vocabulary a request type may choose from.
//
// The category and the type agree on who is paid, so they cannot be chosen
// independently: an employee advance pays the requester and is always in the
// Employee advance category, and a deposit or guarantee pays a counterparty and
// is never in it. validateRequestInput refuses the other combinations; this is
// the same rule on the form, so a requester is not offered a choice that is
// refused after the whole form has been filled in (form-1 / recoverables-1).
// Every other type keeps the full active list — it is only rendered on the
// recoverable treatment, which those types do not carry.
func categoriesForType(kind string, cats []store.RecoverableCategory) []store.RecoverableCategory {
	if kind != "employee_advance" && kind != "recoverable" {
		return cats
	}
	out := make([]store.RecoverableCategory, 0, len(cats))
	for _, c := range cats {
		if (c.Code == "employee_advance") == (kind == "employee_advance") {
			out = append(out, c)
		}
	}
	return out
}

// resolveRecoverableCategory finds the row behind a code so the form can reveal
// the fields that category requires. A code with no active row resolves to the
// zero value, whose Requires flags are both false — the same state as "no
// category chosen", which is what the reader is looking at.
func resolveRecoverableCategory(code string, cats []store.RecoverableCategory) store.RecoverableCategory {
	for _, c := range cats {
		if c.Code == code {
			return c
		}
	}
	return store.RecoverableCategory{}
}

// requestCreate is the whole of D1 in one handler: stage the file, create the
// request already pending, redirect to the confirmation. There is no draft to
// save and no second submit step, so the row, its number, its submission
// timestamp and the requester's document all commit together — or none of them
// do, and the staged file is removed behind them.
func (a *App) requestCreate(w http.ResponseWriter, r *http.Request) {
	in, err := requestInput(r)
	var stagedPath string
	if err == nil {
		var attachment *store.AttachmentInput
		attachment, stagedPath, err = a.stageUploadedAttachment(r)
		if err == nil && attachment != nil {
			in.Attachments = []store.AttachmentInput{*attachment}
		}
	}
	var id int64
	if err == nil {
		id, err = a.st.CreateRequest(r.Context(), auth.CurrentUser(r), in)
	}
	if err != nil {
		// Nothing was written, so nothing may be left on disk either.
		removeStagedAttachment(a.log, r, stagedPath)
		if status, ok := userError(err); ok {
			a.renderRejectedRequestForm(w, r, status, in, err)
		} else {
			a.respondStoreError(w, r, err)
		}
		return
	}
	a.fire(r, notify.EventRequestSubmitted, id)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d/submitted", id), http.StatusSeeOther)
}

// userError reports whether err is a refusal the person can fix (any 4xx
// status), so the form is shown again with the message, and returns that
// status. Anything else is a server fault and gets the error page.
func userError(err error) (int, bool) {
	status := storeErrorStatus(err)
	return status, status < http.StatusInternalServerError
}

// renderRejectedRequestForm puts the form back with the message and everything
// the requester had typed still in it. Losing a page of typing to one bad field
// is the cruellest thing a form can do.
func (a *App) renderRejectedRequestForm(w http.ResponseWriter, r *http.Request, status int, in store.RequestInput, refusal error) {
	message := friendly(refusal)
	label, known := requestTypeLabels[in.Type]
	if !known {
		// A type the *store* does not recognise is a client error about the type,
		// and "that is not a kind of request this system raises" is the right
		// sentence for it. A type the store knows but this chooser does not carry
		// used to fall in here too and lose both its real reason and the whole
		// form (F-E-08/F-B-02); every store type has a card now, so that branch
		// only guards against the two vocabularies drifting apart again, and a
		// refusal there keeps the rule that refused it and lands on the chooser.
		//
		// The two are told apart by the store's own message rather than by a
		// second copy of the type vocabulary here: duplicating that list is how it
		// drifts, and drifting is the defect.
		if strings.Contains(message, "unknown request type") {
			a.respondError(w, r, http.StatusBadRequest, "That is not a kind of request this system raises.", nil)
			return
		}
		a.renderStatus(w, r, status, "request_new_type", PageData{
			Title: "New request",
			Error: message + " — start again from a request type.",
		})
		return
	}
	data, err := a.requestFormData(r, label)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data.Error = message
	data.UrgencyReasonError = errors.Is(refusal, store.ErrUrgencyReasonRequired)
	data.DuplicateReason = in.DuplicateReason
	data.RequestRawAmount = r.FormValue("amount")
	data.RequestFieldErrors = requestFieldErrors(in)
	if in.Type == "vendor_invoice" {
		data.Similar, err = a.st.DuplicateInvoices(r.Context(), in, store.RequestListOptions{Scope: a.auth.Scope(auth.CurrentUser(r), "request"), ViewerID: auth.CurrentUser(r).ID})
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
	data.Request2 = requestFromInput(in)
	if err := a.setFormType(r, &data, in.Type, in.RecoverableCategory); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.renderStatus(w, r, status, "request_form", data)
}

// setFormType narrows a request form to one request type: the recoverable
// categories that type may use, the row of the chosen category (so the form
// shows the fields it requires) and, for the vendor types, the vendor list.
func (a *App) setFormType(r *http.Request, data *PageData, kind, category string) error {
	data.FormType = kind
	data.Categories = categoriesForType(kind, data.Categories)
	data.RecCategory = resolveRecoverableCategory(category, data.Categories)
	var err error
	data.Vendors, err = a.vendorChoices(r, kind)
	return err
}

// requestFromInput turns what was posted back into the shape the form renders
// from, so a rejected submission re-renders as itself.
func requestFromInput(in store.RequestInput) store.Request {
	req := store.Request{
		Treatment: in.Treatment, Type: in.Type, RecoverableCategory: in.RecoverableCategory,
		VendorPayee: in.VendorPayee, ShortTitle: in.ShortTitle, Amount: in.Amount,
		Purpose: in.Purpose, NeededBy: in.NeededBy, InvoiceNo: in.InvoiceNo,
		InvoiceDate: in.InvoiceDate, ExpenseDate: in.ExpenseDate, AdvanceReason: in.AdvanceReason,
		Counterparty: in.Counterparty, ExpectedReturnDate: in.ExpectedReturnDate,
		RepaymentNotes: in.RepaymentNotes, Urgent: in.Urgent, UrgencyReason: in.UrgencyReason,
		AttachmentExceptionReason: in.AttachmentExceptionReason, ManagerID: in.ManagerID,
	}
	req.ProjectID = optionalID(in.ProjectID)
	req.HeadID = optionalID(in.HeadID)
	req.VendorID = optionalID(in.VendorID)
	return req
}

func (a *App) requestSubmitted(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	// "What happens next" quotes the reminder wait twice, so it reads the
	// configured threshold rather than a hardcoded three (F-F-03).
	thresholds, err := a.st.ReminderThresholds(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "request_submitted", PageData{Title: "Request submitted", Request2: req, Reminders: thresholds})
}

// canSeeRequest reports whether the signed-in user's request data scope reaches
// req. It is the row half of the access rule, shared with every store read
// through store.RequestInScope, so a list, a queue, a notification and a detail
// page can never disagree about who may see a request.
//
// It deliberately checks the scope alone and not the request:view grant. Which
// verb a screen needs depends on the screen — request:view for /requests,
// payment:view for a linked payment, recoverable_report:view for the register —
// and every route already demands its own verb before a handler runs. Adding
// request:view here would refuse an accountant with payment:view but no
// request:view the payment they are allowed to open, without hiding anything
// the routes do not already hide. Notification recipients, which have no route
// in front of them, use store.CanViewRequest, which does require the grant.
func (a *App) canSeeRequest(r *http.Request, req store.Request) bool {
	u := auth.CurrentUser(r)
	return store.RequestInScope(a.auth.Scope(u, "request"), u.ID, req)
}

// loadRequestByID reads request id and refuses it to anybody whose data scope
// does not reach it — holding request:view somewhere is not the same as being
// allowed to read this one.
//
// The refusal is 404, not 403, and the same sentence a missing row gets. If a
// row that exists but is out of scope answered 403 while a missing row answered
// 404, the status code would tell a requester exactly which request ids exist,
// and from that how many requests the company raises. The existence of a row is
// information about that row, so the two cases are indistinguishable. The
// attempt is still logged.
func (a *App) loadRequestByID(w http.ResponseWriter, r *http.Request, id int64) (store.Request, bool) {
	req, err := a.st.Request(r.Context(), id)
	if err != nil {
		a.respondStoreError(w, r, err)
		return store.Request{}, false
	}
	if !a.visibleOr404(w, r, req) {
		return store.Request{}, false
	}
	return req, true
}

// visibleOr404 reports whether the signed-in user may see req. When they may
// not, it answers 404 with the same sentence a missing request gets, and logs
// the attempt.
func (a *App) visibleOr404(w http.ResponseWriter, r *http.Request, req store.Request) bool {
	if a.canSeeRequest(r, req) {
		return true
	}
	a.respondError(w, r, http.StatusNotFound, "The requested record was not found.",
		fmt.Errorf("request %d is outside the caller's data scope", req.ID))
	return false
}

// loadViewableRequest is loadRequestByID for the {id} in the URL path.
func (a *App) loadViewableRequest(w http.ResponseWriter, r *http.Request) (store.Request, bool) {
	return a.loadRequestByID(w, r, pathID(r))
}

// requestDuplicateCheck renders the advisory duplicate warning. It is a read:
// it answers 200 whether or not anything matched, and POST /requests neither
// calls it nor consults its result. Legitimate repeat payments exist — the same
// rent, the same monthly retainer — so the system points and the person
// decides (G6). Even a failed check must not stand between somebody and their
// submit, which is why an error here is logged and swallowed.
func (a *App) requestDuplicateCheck(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	amount, _ := money.ParsePaise(r.FormValue("amount"))
	payee := r.FormValue("vendor_payee")
	// A reimbursement or an employee advance carries no vendor row: the store
	// forces its payee to the requester, so the check has to look for it there
	// or those two types would never be checked at all.
	if payeeIsRequester(r.FormValue("type")) {
		payee = u.Name
	}
	similar, err := a.st.SimilarRequests(r.Context(), store.SimilarRequestOptions{
		ExcludeID: parseID(r.FormValue("request_id")),
		VendorID:  parseID(r.FormValue("vendor_id")),
		Payee:     payee,
		Amount:    amount,
		InvoiceNo: r.FormValue("invoice_no"),
	})
	if err != nil {
		a.log.ErrorContext(r.Context(), "duplicate check failed", "request_id", requestID(r), "error", err)
		return
	}
	similar = slices.DeleteFunc(similar, func(match store.Request) bool { return !a.canSeeRequest(r, match) })
	if len(similar) == 0 {
		return
	}
	a.renderPartial(w, r, "request_duplicates", PageData{Similar: similar})
}

// payeeIsRequester mirrors the store's forcesRequesterPayee. These types never
// carry a vendor row, so the payee snapshot is the only payee they have.
func payeeIsRequester(t string) bool {
	return t == "reimbursement" || t == "employee_advance"
}

// vendorChoices backs the plain <select> the form falls back to when the
// combobox cannot run — no JavaScript, or a requester who does not hold
// vendor:view and so cannot reach GET /vendors/search. Types that never carry a
// vendor row load nothing.
func (a *App) vendorChoices(r *http.Request, formType string) ([]store.Vendor, error) {
	if formType != "vendor_invoice" && formType != "vendor_advance" {
		return nil, nil
	}
	return a.st.ListVendors(r.Context(), store.VendorListOptions{Status: "active", Limit: 500},
		a.auth.Permissions(auth.CurrentUser(r)))
}

// requestInput reads the whole form. Every field the adaptive form is capable
// of revealing is read unconditionally: the store decides what was allowed to
// be filled in, because a browser that hid a control is not a guarantee that
// the submitter's browser did.
func requestInput(r *http.Request) (store.RequestInput, error) {
	in := store.RequestInput{
		DuplicateReason:           r.FormValue("duplicate_reason"),
		Treatment:                 r.FormValue("treatment"),
		Type:                      r.FormValue("type"),
		RecoverableCategory:       r.FormValue("recoverable_category"),
		ProjectID:                 parseID(r.FormValue("project_id")),
		HeadID:                    parseID(r.FormValue("head_id")),
		VendorID:                  parseID(r.FormValue("vendor_id")),
		VendorPayee:               r.FormValue("vendor_payee"),
		ShortTitle:                r.FormValue("short_title"),
		Purpose:                   r.FormValue("purpose"),
		NeededBy:                  r.FormValue("needed_by"),
		InvoiceNo:                 r.FormValue("invoice_no"),
		InvoiceDate:               r.FormValue("invoice_date"),
		ExpenseDate:               r.FormValue("expense_date"),
		AdvanceReason:             r.FormValue("advance_reason"),
		Counterparty:              r.FormValue("counterparty"),
		ExpectedReturnDate:        r.FormValue("expected_return_date"),
		RepaymentNotes:            r.FormValue("repayment_notes"),
		Urgent:                    r.FormValue("urgent") == "on",
		UrgencyReason:             r.FormValue("urgency_reason"),
		AttachmentExceptionReason: r.FormValue("attachment_exception_reason"),
		ManagerID:                 parseID(r.FormValue("manager_id")),
	}
	amount, err := money.ParsePaise(r.FormValue("amount"))
	in.Amount = amount
	if err != nil {
		return in, fmt.Errorf("%w: enter the amount you are requesting", store.ErrValidation)
	}
	return in, nil
}

// effectiveScope lets a URL narrow the caller's scope and never widen it, so
// "?scope=own" is a filter an admin may use and "?scope=all" is not a way in.
func (a *App) effectiveScope(u store.User, requested string) string {
	scope := a.auth.Scope(u, "request")
	if scopeRank[requested] > 0 && scopeRank[requested] < scopeRank[scope] {
		return requested
	}
	return scope
}

// scopeRank orders the request data scopes from narrowest to widest.
var scopeRank = map[string]int{store.ScopeOwn: 1, store.ScopeAssigned: 2, store.ScopeAll: 3}

// requestFormData assembles everything the adaptive form needs. The approver
// list comes from ListApprovers, so the requester's own name is structurally
// absent (G8), and their default approver is pre-selected (G9).
func (a *App) requestFormData(r *http.Request, title string) (PageData, error) {
	u := auth.CurrentUser(r)
	ctx := r.Context()
	projects, err := a.st.ListProjects(ctx, true)
	if err != nil {
		return PageData{}, err
	}
	heads, err := a.st.ListHeads(ctx, true)
	if err != nil {
		return PageData{}, err
	}
	approvers, err := a.st.ListApprovers(ctx, u.ID)
	if err != nil {
		return PageData{}, err
	}
	settings, err := a.st.AppSettings(ctx)
	if err != nil {
		return PageData{}, err
	}
	// The recoverable category picker is the table, not a literal. An
	// admin-added category was enforced but unselectable and a deactivated one
	// was still offered, which is half of V4 (F-E-02/F-B-17).
	cats, err := a.st.ListRecoverableCategories(ctx, true)
	if err != nil {
		return PageData{}, err
	}
	// The reminder copy on this form quotes a number, so it reads the number.
	thresholds, err := a.st.ReminderThresholds(ctx)
	if err != nil {
		return PageData{}, err
	}
	data := PageData{Title: title, Projects: projects, Heads: heads, Approvers: approvers,
		Settings: settings, Categories: cats, Reminders: thresholds}
	// G9: 0 means "no default"; the control then opens on "Choose an approver".
	if u.DefaultApproverID > 0 {
		data.Request2.ManagerID = u.DefaultApproverID
	}
	return data, nil
}

// requestCardData is what "request_card" is called with. It exists because a
// {{template}} inside a {{range}} rebinds dot to the request, and $.User.ID
// inside the card would then resolve against the card rather than the page —
// silently, and with the wrong "waiting on" line to show for it.
type requestCardData struct {
	Req      store.Request
	ViewerID int64
}

type requestTab struct{ Key, Label string }

// requestTabs are the .segmented tabs. "needs-me" is the one the whole design
// turns on: it is the only view that answers "what is mine to do".
var requestTabs = []requestTab{
	{"open", "Open"}, {"needs-me", "Needs me"}, {"closed", "Closed"}, {"all", "All"},
}

// requestListOptions reads the request-list filters from the URL. The list
// screen and its CSV export both use it, so the file always holds what the
// screen shows. The bucket (tab) defaults to "open". A ?status= other than
// "all" goes in Statuses, which the store applies instead of the bucket's own
// statuses.
func (a *App) requestListOptions(r *http.Request) store.RequestListOptions {
	u := auth.CurrentUser(r)
	q := r.URL.Query()
	opts := store.RequestListOptions{Scope: a.effectiveScope(u, q.Get("scope")), ViewerID: u.ID,
		Bucket: queryDefault(r, "bucket", "open"), Type: q.Get("type"), Treatment: q.Get("treatment"),
		ProjectID: parseID(q.Get("project_id")), Query: q.Get("q")}
	if status := q.Get("status"); status != "" && status != "all" {
		opts.Statuses = []string{status}
	}
	return opts
}

// requests is the /requests list: one page of the reader's requests with a
// count on every tab. The page reports its total so the screen can say how
// many there are and link to the next page.
func (a *App) requests(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := a.requestListOptions(r)
	page, err := a.st.ListRequestsPage(r.Context(), store.RequestPageOptions{
		RequestListOptions: opts, Offset: int(parseID(q.Get("offset")))})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// Every tab count comes from the same SQL as the rows, with only the bucket
	// changed, so a tab can never promise a number the list does not show.
	counts := map[string]int{}
	for _, tab := range requestTabs {
		counted := opts
		counted.Bucket = tab.Key
		counted.Statuses = nil
		n, err := a.st.CountRequests(r.Context(), counted)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		counts[tab.Key] = n
	}
	projects, err := a.st.ListProjects(r.Context(), true)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "requests", PageData{Title: "Requests", Requests: page.Requests, Page: page,
		Scope: opts.Scope, Bucket: opts.Bucket, Status: q.Get("status"),
		TypeFilter: opts.Type, Treatment: opts.Treatment, Query: opts.Query,
		RequestClearHref: requestClearURL(r, opts), Counts: counts, Projects: projects, RequestProjectID: opts.ProjectID, RequestVendorID: opts.VendorID})
}

// requestDetail is one screen for every audience. The two detail mockups are
// the same page: only the `.action-bar` differs, and it differs on permission —
// never on a role name, and never on which URL the reader arrived from.
func (a *App) requestDetail(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	// request-returned.html is this request in a different state, seen by the
	// one person who can act on it. It is a different template rather than a
	// pile of conditionals: what a returned request needs is a correction form,
	// and everybody else still needs the detail.
	if a.showsReturnedCorrection(r, req) {
		data, err := a.requestEditData(r, req)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		a.render(w, r, "request_returned", data)
		return
	}
	data, err := a.requestDetailData(r, req, req.Number)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "request_detail", data)
}

func (a *App) showsReturnedCorrection(r *http.Request, req store.Request) bool {
	u := auth.CurrentUser(r)
	return req.Status == "returned" && req.RequesterID == u.ID && a.auth.Can(u, "request", "edit")
}

// requestDetailData loads the merged thread and the documents every screen that
// shows one request needs, so the detail page, the returned-correction page and
// the cancellation decision cannot drift into three different readings of the
// same request.
func (a *App) requestDetailData(r *http.Request, req store.Request, title string) (PageData, error) {
	thread, err := a.st.RequestThread(r.Context(), req.ID)
	if err != nil {
		return PageData{}, err
	}
	atts, err := a.st.RequestAttachments(r.Context(), req.ID)
	if err != nil {
		return PageData{}, err
	}
	data := PageData{Title: title, Request2: req, Thread: thread, RequestAtts: atts}
	data.RequestRevision, err = a.st.RequestEditRevision(r.Context(), req)
	if err != nil {
		return PageData{}, err
	}
	data.RequestPayments, err = a.st.RequestPayments(r.Context(), req.ID)
	if err != nil {
		return PageData{}, err
	}
	// A7: the reassign-the-approval control needs somebody to reassign it to. The
	// list is ListApprovers — everyone holding approval:approve except the
	// requester — which is the same list the request form offers and the same rule
	// store.ReassignRequest enforces, so the select can never name a target the
	// POST would refuse. It is loaded only for a caller who may actually use the
	// control (F-A-06/F-C-02): the same answer the route gives, so nobody is
	// offered a sheet whose submit answers 403 (rbac-8).
	if data.CanReassignApprover = a.mayReassignApprover(auth.CurrentUser(r), req); data.CanReassignApprover {
		approvers, aerr := a.st.ListApprovers(r.Context(), req.RequesterID)
		if aerr != nil {
			return PageData{}, aerr
		}
		data.Approvers = approvers
	}
	// Q4: the requester reads the outcome on the request, not in the ledger. No
	// payment yet is the ordinary case for most of a request's life, so
	// ErrNotFound is an answer here rather than a failure.
	pay, perr := a.st.PaymentForRequest(r.Context(), req.ID)
	switch {
	case perr == nil:
		data.Payment = pay
	case !errors.Is(perr, store.ErrNotFound):
		return PageData{}, perr
	}
	return data, nil
}

// mayReassignApprover is the one answer to who may hand this request's approval
// on, asked by the detail screen and by the route. The request's own approver
// may, because it is theirs to decide (A5). So may a holder of user:edit — the
// grant that deactivates a person is the grant that rescues what they leave
// behind (F-G-025) — because an administrator is the only door out of a request
// whose approver cannot act. Nobody else: the seeded Manager role holds
// approval:reassign and sees every request, and with no more than that any
// manager could make another manager's request theirs and approve it (rbac-8).
// Only in a status the store will move, so the control is never a dead end.
func (a *App) mayReassignApprover(u store.User, req store.Request) bool {
	if !a.auth.Can(u, "approval", "reassign") || !store.Reassignable(req.Status) {
		return false
	}
	return req.ManagerID == u.ID || a.auth.Can(u, "user", "edit")
}

// requestEditForm is the correction screen. Only the person who raised a
// request may open it: holding request:edit says you may correct your own work,
// not somebody else's, and an approver who wants a change returns the request
// instead — which is a different verb with a different audit line.
func (a *App) requestEditForm(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadEditableRequest(w, r)
	if !ok {
		return
	}
	data, err := a.requestEditData(r, req)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "request_edit", data)
}

// loadEditableRequest resolves the request and refuses anybody who is not its
// requester, or any status the store would refuse to write anyway. Answering
// 403 here rather than at the store keeps the screen honest: a control the
// reader may not use is never rendered, and the URL says the same thing.
func (a *App) loadEditableRequest(w http.ResponseWriter, r *http.Request) (store.Request, bool) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return store.Request{}, false
	}
	if req.RequesterID != auth.CurrentUser(r).ID {
		a.respondError(w, r, http.StatusForbidden, "Only the person who raised a request may edit it.", nil)
		return store.Request{}, false
	}
	if req.Status != "pending" && req.Status != "returned" {
		a.respondError(w, r, http.StatusBadRequest,
			"This request can no longer be edited. "+reqStatusExplain(req.Status), nil)
		return store.Request{}, false
	}
	return req, true
}

func reqStatusExplain(status string) string {
	switch status {
	case "approved":
		return "It is approved and locked; ask for it to be cancelled instead."
	case "cancellation_requested":
		return "A cancellation is already pending on it."
	case "rejected":
		return "A rejected request is final — raise a new one."
	case "pending":
		return "It is still awaiting approval; withdraw it instead."
	default:
		return "It is " + strings.ToLower(requestStatusText(status)) + "."
	}
}

// requestEditData is the edit form's whole state: the same projects, heads,
// approvers and settings the new-request form is built from, plus the request
// as it stands and the documents already on it.
func (a *App) requestEditData(r *http.Request, req store.Request) (PageData, error) {
	data, err := a.requestFormData(r, "Edit "+req.Number)
	if err != nil {
		return PageData{}, err
	}
	data.Request2 = req
	data.RequestRevision, err = a.st.RequestEditRevision(r.Context(), req)
	if err != nil {
		return PageData{}, err
	}
	if err := a.setFormType(r, &data, req.Type, req.RecoverableCategory); err != nil {
		return PageData{}, err
	}
	noteLegacyDeposit(&data, req)
	if err := a.offerRetiredRefs(r, &data, req); err != nil {
		return PageData{}, err
	}
	if data.Thread, err = a.st.RequestThread(r.Context(), req.ID); err != nil {
		return PageData{}, err
	}
	if data.RequestAtts, err = a.st.RequestAttachments(r.Context(), req.ID); err != nil {
		return PageData{}, err
	}
	return data, nil
}

// legacyDepositCategory is the deposit category an employee advance was
// recorded under before deposits and guarantees were their own request type
// (form-1), or "" for any other row. The test is the stored code itself, not
// whether it resolves in the active list: retiring the Employee advance
// category does not turn every employee advance into a legacy deposit.
func legacyDepositCategory(req store.Request) string {
	if req.Type == "employee_advance" && req.Treatment == "recoverable" &&
		req.RecoverableCategory != "" && req.RecoverableCategory != "employee_advance" {
		return req.RecoverableCategory
	}
	return ""
}

// noteLegacyDeposit is the edit and returned screens' handling of a row from
// before deposits were their own type: an employee advance filed under EMD, ICD
// or another deposit category. It still reads as it was recorded, but both
// screens fix the category, so it is offered back as an Employee advance, and
// the banner says what saving will do *before* the press (form-1, kept
// readable). The returned screen's one-press resubmit used to do this without
// a word, because the banner was rendered on the full edit form only.
//
// The way out for money that goes to a counterparty depends on the status: a
// pending request can be withdrawn now, while a returned one can only be
// resubmitted first (returned → pending is its one legal transition), so the
// sentence says so rather than pointing at a control the screen does not have.
func noteLegacyDeposit(data *PageData, req store.Request) {
	code := legacyDepositCategory(req)
	if code == "" || data.LegacyCategory != "" {
		return
	}
	data.LegacyCategory = code
	data.Request2.RecoverableCategory = normalizeRecoverableCategory("", req.Type, data.Categories)
	data.RecCategory = resolveRecoverableCategory(data.Request2.RecoverableCategory, data.Categories)
	body := "This employee advance was recorded under the " + recoverableLabel(code) +
		" category before deposits and guarantees became their own request type. Saving files it as an Employee advance, which pays the person raising it — " + req.RequesterName + ", not "
	if strings.TrimSpace(req.Counterparty) != "" {
		body += req.Counterparty + ". "
	} else {
		body += "a counterparty. "
	}
	if req.Status == "returned" {
		body += "If the money goes to a counterparty, resubmit it, withdraw it, and raise a deposit or guarantee instead."
	} else {
		body += "If the money goes to a counterparty, withdraw it and raise a deposit or guarantee instead."
	}
	data.Warnings = append(data.Warnings, Warning{
		Title: "Recorded before deposits and guarantees were their own request type",
		Body:  body,
	})
}

// offerRetiredRefs puts a project or head that was retired after the request was
// raised back into the edit form's selects, marked, and says why it has to be
// chosen again (T12, form-3).
//
// The selects are built from the active set, so a retired project or head had no
// option to select: the control fell back to "Choose a project", the browser
// posted nothing, and a requester who changed only the purpose was refused with
// "project and head are required for this type" — a sentence about a request
// that had both. The historical value is offered back so the form shows what the
// request charges today, and the banner names the retirement, because the store
// still refuses a retired head (validateRequestRefs) and the reader has to know
// that re-choosing is the way through. The head is flagged inactive here even
// when only its project was retired: ListHeads(ctx, true) treats the two alike,
// and "retired" is what the option has to say either way.
func (a *App) offerRetiredRefs(r *http.Request, data *PageData, req store.Request) error {
	var retired []string
	projectID := deref(req.ProjectID)
	isChargedProject := func(p store.Project) bool { return p.ID == projectID }
	if projectID > 0 && !slices.ContainsFunc(data.Projects, isChargedProject) {
		all, err := a.st.ListProjects(r.Context(), false)
		if err != nil {
			return err
		}
		if i := slices.IndexFunc(all, isChargedProject); i >= 0 {
			all[i].Active = false
			data.Projects = append(data.Projects, all[i])
			retired = append(retired, "project "+all[i].Name)
		}
	}
	headID := deref(req.HeadID)
	isChargedHead := func(h store.Head) bool { return h.ID == headID }
	if headID > 0 && !slices.ContainsFunc(data.Heads, isChargedHead) {
		all, err := a.st.ListHeads(r.Context(), false)
		if err != nil {
			return err
		}
		if i := slices.IndexFunc(all, isChargedHead); i >= 0 {
			all[i].Active = false
			data.Heads = append(data.Heads, all[i])
			retired = append(retired, "head "+all[i].Name)
		}
	}
	if len(retired) == 0 {
		return nil
	}
	verb := "were"
	if len(retired) == 1 {
		verb = "was"
	}
	data.Warnings = append(data.Warnings, Warning{
		Title: "This request charges something that has since been retired",
		Body: "The " + strings.Join(retired, " and ") + " this request charges " + verb +
			" retired after it was raised. Choose a project and head that are still open before saving.",
	})
	return nil
}

// requestEdit saves a correction. "resubmit" is the returned screen's primary
// action: the corrections and the resubmission are one press, because saving
// and then forgetting to send it back is how a returned request sits for a week
// with nobody waiting on it.
func (a *App) requestEdit(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadEditableRequest(w, r)
	if !ok {
		return
	}
	u := auth.CurrentUser(r)
	resubmitting := r.FormValue("submit_action") == "resubmit"
	in, err := requestInput(r)
	revision := strings.TrimSpace(r.FormValue("revision"))
	if err == nil && revision == "" {
		err = fmt.Errorf("%w: this edit form has expired. Reload the request before saving corrections", store.ErrValidation)
	}
	var attachment *store.AttachmentInput
	var stagedPath string
	if err == nil {
		attachment, stagedPath, err = a.stageUploadedAttachment(r)
	}
	// One press, one transaction (F-G-035/F-C-04). This used to be three calls in a
	// row — update, attach, submit — so a resubmit that failed its own preconditions
	// left the edit committed *and audited as done* while the requester was shown a
	// 400, the history asserting a change the caller had been told did not happen.
	// store.EditRequest commits all three or none, and validates the submit against
	// the edited row rather than the row as it was on entry.
	if err == nil {
		err = a.st.EditRequest(r.Context(), u, req.ID, store.RequestEdit{
			Input: in, Attachment: attachment, Resubmit: resubmitting, Revision: revision})
	}
	if err != nil {
		removeStagedAttachment(a.log, r, stagedPath)
		if status, ok := userError(err); ok {
			a.renderRejectedEdit(w, r, req, in, status, err)
		} else {
			a.respondStoreError(w, r, err)
		}
		return
	}
	// Not unconditionally. The seeded template for this event says the request
	// "was edited and re-sent for approval", and there is exactly one case where
	// that is false: "Save corrections" on a *returned* request, the button whose
	// whole purpose is staying put without re-sending. Firing there told the
	// approver something was back in their queue when the status was still
	// `returned` (F-F-07).
	//
	// Editing a *pending* request does re-notify, and the edit screen's banner
	// promises it does: the request is already in the approver's queue and the
	// figures they are about to decide on have changed, so the sentence holds.
	if resubmitting || req.Status == "pending" {
		a.fire(r, notify.EventRequestEdited, req.ID)
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
}

// renderRejectedEdit puts the correction screen back with the message and
// everything that was typed still in it, re-read from the row only for the
// parts the form does not own.
func (a *App) renderRejectedEdit(w http.ResponseWriter, r *http.Request, stored store.Request, in store.RequestInput, status int, refusal error) {
	data, err := a.requestEditData(r, editedRequest(stored, in))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// The posted values already say Employee advance, so the edited row is not
	// legacy — but the stored one still is, and the notice about what saving
	// does must not vanish because one field was refused.
	noteLegacyDeposit(&data, stored)
	data.Error = friendly(refusal)
	data.UrgencyReasonError = errors.Is(refusal, store.ErrUrgencyReasonRequired)
	data.RequestRevision = r.FormValue("revision") // Never bless a stale retry with the latest revision.
	data.RequestRawAmount = r.FormValue("amount")
	data.RequestFieldErrors = requestFieldErrors(in)
	data.DuplicateReason = in.DuplicateReason
	name := "request_edit"
	if stored.Status == "returned" {
		name = "request_returned"
	}
	a.renderStatus(w, r, status, name, data)
}

// editedRequest overlays what was posted onto the stored row. Losing a page of
// corrections to one bad field is the cruellest thing a form can do, and the
// number, the status and the names were never the form's to change.
func editedRequest(stored store.Request, in store.RequestInput) store.Request {
	edited := requestFromInput(in)
	edited.ID, edited.Number, edited.Status = stored.ID, stored.Number, stored.Status
	edited.RequesterID, edited.RequesterName = stored.RequesterID, stored.RequesterName
	edited.ManagerName = stored.ManagerName
	edited.Vendor, edited.VendorGSTIN = stored.Vendor, stored.VendorGSTIN
	edited.Project, edited.Head = stored.Project, stored.Head
	edited.DecisionReason, edited.CancelReason = stored.DecisionReason, stored.CancelReason
	edited.ApprovedAmount, edited.ApprovedAt = stored.ApprovedAmount, stored.ApprovedAt
	edited.CreatedAt, edited.UpdatedAt, edited.SubmittedAt = stored.CreatedAt, stored.UpdatedAt, stored.SubmittedAt
	return edited
}

// requestApprove is the approve sheet. The amount is editable there because an
// approver may approve less than was asked for; the store is what refuses a
// requester approving themselves, whatever this handler is sent.
func (a *App) requestApprove(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	req, err := a.st.Request(r.Context(), pathID(r))
	if err != nil || req.ManagerID != u.ID {
		a.respondStoreError(w, r, store.ErrNotFound)
		return
	}
	if !a.visibleOr404(w, r, req) {
		return
	}
	amount, err := money.ParsePaise(r.FormValue("approved_amount"))
	if err != nil {
		a.refuseRequestApproval(w, r, "Enter the amount you are approving.")
		return
	}
	locked, err := a.lockedApprovalMonth(r.Context(), req.NeededBy)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if locked != "" {
		a.respondError(w, r, http.StatusConflict,
			locked+" is locked, so this request cannot be approved for payment in it. Reopen the month, or ask for the required-by date to be changed.", nil)
		return
	}
	if err := a.st.ApproveRequest(r.Context(), u, req.ID, amount, r.FormValue("note")); err != nil {
		if errors.Is(err, store.ErrValidation) {
			a.refuseRequestApproval(w, r, friendly(err))
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	a.fire(r, notify.EventRequestApproved, req.ID)
	http.Redirect(w, r, "/approvals", http.StatusSeeOther)
}

// Refused amounts stay in the same approval sheet. Reload permission and state
// before rendering: a concurrent decision must not reopen an actionable sheet.
func (a *App) refuseRequestApproval(w http.ResponseWriter, r *http.Request, message string) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	u := auth.CurrentUser(r)
	if req.ManagerID != u.ID || req.RequesterID == u.ID {
		a.respondStoreError(w, r, store.ErrForbidden)
		return
	}
	if req.Status != "pending" {
		a.respondError(w, r, http.StatusBadRequest, message, nil)
		return
	}
	data, err := a.requestDetailData(r, req, req.Number)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data.RequestRevision = r.FormValue("revision") // A refused submission never authorizes an unseen revision.
	data.ApprovalError = message
	data.ApprovalRawAmount = r.FormValue("approved_amount")
	data.ApprovalNote = r.FormValue("note")
	a.renderStatus(w, r, http.StatusBadRequest, "request_detail", data)
}

// lockedApprovalMonth returns the YYYY-MM month of neededBy when that month is
// locked, or "" when it is open or neededBy is empty.
//
// Approving is a promise that the request can be paid, and no payment can be
// recorded in a locked month, so an approval into a locked month is refused up
// front instead of surprising the accountant later. The month checked is the
// request's needed-by date, the only date a request carries. A failed lock read
// is returned as an error (MonthIsLocked, not IsLocked), so the approval is
// refused rather than let through.
func (a *App) lockedApprovalMonth(ctx context.Context, neededBy string) (string, error) {
	if len(neededBy) < 7 {
		return "", nil
	}
	month := neededBy[:7]
	locked, err := a.st.MonthIsLocked(ctx, month)
	if err != nil || !locked {
		return "", err
	}
	return month, nil
}

// requestChange is one store call that moves a request to a new state.
type requestChange func(ctx context.Context, u store.User, id int64) error

// changeRequest is the shared body of the simple state-change handlers
// (return, reject, withdraw, cancel, hold, release …). It loads the request in
// the URL (404 if the reader's data scope does not reach it), runs change,
// sends the event's notifications and redirects to dest. "{id}" in dest is
// replaced by the request id. A refusal from the store is answered by
// respondStoreError and nothing is sent.
func (a *App) changeRequest(w http.ResponseWriter, r *http.Request, event, dest string, change requestChange) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	if err := change(r.Context(), auth.CurrentUser(r), req.ID); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.fire(r, event, req.ID)
	http.Redirect(w, r, strings.ReplaceAll(dest, "{id}", strconv.FormatInt(req.ID, 10)), http.StatusSeeOther)
}

// requestReturn sends a pending request back to its requester for correction.
func (a *App) requestReturn(w http.ResponseWriter, r *http.Request) {
	a.changeRequest(w, r, notify.EventRequestReturned, "/approvals", func(ctx context.Context, u store.User, id int64) error {
		return a.st.ReturnRequest(ctx, u, id, r.FormValue("comment"), r.FormValue("revision"))
	})
}

// requestReject rejects a pending request. A rejection is final.
func (a *App) requestReject(w http.ResponseWriter, r *http.Request) {
	a.changeRequest(w, r, notify.EventRequestRejected, "/approvals", func(ctx context.Context, u store.User, id int64) error {
		return a.st.RejectRequest(ctx, u, id, r.FormValue("reason"), r.FormValue("revision"))
	})
}

// requestWithdraw lets the requester take back a request that is not yet
// approved. The approver is notified, because the item leaves their queue
// without a decision.
func (a *App) requestWithdraw(w http.ResponseWriter, r *http.Request) {
	a.changeRequest(w, r, notify.EventRequestWithdrawn, "/requests/{id}", func(ctx context.Context, u store.User, id int64) error {
		return a.st.WithdrawRequest(ctx, u, id)
	})
}

// requestReraise lands on the confirmation screen rather than the source, and
// it lands there for the *new* request: D1 makes a re-raise a fresh pending
// request with its own number, not a draft copy of the rejected one.
func (a *App) requestReraise(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.loadViewableRequest(w, r); !ok {
		return
	}
	id, err := a.st.ReraiseRequest(r.Context(), auth.CurrentUser(r), pathID(r))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// Fired for the *new* id, not the rejected one: the second attempt is what
	// needs deciding, and without this the approver never learned it existed
	// (F-F-06).
	a.fire(r, notify.EventRequestReraised, id)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d/submitted", id), http.StatusSeeOther)
}

func (a *App) requestComment(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	if _, err := a.st.AddRequestComment(r.Context(), auth.CurrentUser(r), req.ID, r.FormValue("body")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	dest := fmt.Sprintf("/requests/%d", req.ID)
	// The partial review is a decision screen, and a manager who asks Accounts a
	// question mid-decision should land back on the decision. The field carries
	// a token, never a URL: the destination is built from the id this handler
	// already loaded, so nothing a form says can redirect anybody off-site.
	if r.FormValue("return_to") == "partial-review" && req.Status == "partial_review" {
		dest += "/partial-review"
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// requestAttachmentUpload adds one document to a request without touching a
// field on it. It is the requester's answer to a hold — "Need GST receipt" —
// which the design says they may give as a comment or an attachment (Q6/L7),
// and which the edit path cannot carry once the request is approved. Only the
// person who raised the request may add to it, and only while it is on hold:
// that is the one moment the design grants, the only one the detail screen
// offers, and the server allows no more than it shows (hold-1 review). The
// checks here answer before a file is staged for nothing; the store makes them
// again on the row inside its transaction, which is where they count.
func (a *App) requestAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	u := auth.CurrentUser(r)
	if req.RequesterID != u.ID {
		a.respondError(w, r, http.StatusForbidden, "Only the person who raised a request may add documents to it.", nil)
		return
	}
	if !req.OnHold {
		how := "Add a comment if there is something to say about it."
		if req.Status == "pending" || req.Status == "returned" {
			how = "Edit the request to attach it instead."
		}
		a.respondError(w, r, http.StatusBadRequest, "This request is not on hold, so no document can be added to it. "+how, nil)
		return
	}
	attachment, stagedPath, err := a.stageUploadedAttachment(r)
	if err == nil && attachment == nil {
		err = fmt.Errorf("%w: choose a file to attach", store.ErrValidation)
	}
	if err == nil {
		_, err = a.st.AddHeldRequestAttachment(r.Context(), u, req.ID, *attachment)
	}
	if err != nil {
		// Nothing was written, so nothing may be left on disk either.
		removeStagedAttachment(a.log, r, stagedPath)
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
}

// The cancellation flow (G1, G2, G3). An approved request cannot be withdrawn
// on the requester's own say-so: they ask, payment freezes at that moment, and
// the approver accepts or declines. The approver may also cancel outright with
// a reason, without having been asked.

// requestCancelForm is the employee's side. Asking is the requester's act, so
// anybody else — including an administrator holding every verb — is refused
// here rather than at the store, and the screen is never rendered to somebody
// whose submit would bounce.
func (a *App) requestCancelForm(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	if req.RequesterID != auth.CurrentUser(r).ID {
		a.respondError(w, r, http.StatusForbidden,
			"Only the person who raised a request may ask for it to be cancelled.", nil)
		return
	}
	// Only an approved request is frozen by asking. A pending one is withdrawn
	// and a closed one is already closed, so rendering the form for either would
	// be offering a control whose submit the store is going to refuse.
	if req.Status != "approved" {
		a.respondError(w, r, http.StatusBadRequest,
			"Only an approved request is cancelled this way. "+reqStatusExplain(req.Status), nil)
		return
	}
	a.render(w, r, "request_cancel", PageData{Title: "Request cancellation", Request2: req})
}

// requestCancelAsk records the requester's request to cancel an approved
// request. Payment is frozen until the approver decides.
func (a *App) requestCancelAsk(w http.ResponseWriter, r *http.Request) {
	a.changeRequest(w, r, notify.EventCancellationRequested, "/requests/{id}", func(ctx context.Context, u store.User, id int64) error {
		return a.st.RequestCancellation(ctx, u, id, r.FormValue("reason"))
	})
}

// requestCancellationForm is the approver's side: the decision on a pending
// cancellation, or the outright cancellation of a request nobody asked about.
// Both are the same screen because both are the same question — should this
// still be paid — and the answer is recorded the same way either way.
func (a *App) requestCancellationForm(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	if req.ManagerID != auth.CurrentUser(r).ID {
		a.respondError(w, r, http.StatusForbidden,
			"Only the approver this request was sent to can decide its cancellation.", nil)
		return
	}
	data, err := a.requestDetailData(r, req, "Cancellation · "+req.Number)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "request_cancellation", data)
}

func (a *App) requestCancellationDecide(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.loadViewableRequest(w, r); !ok {
		return
	}
	id := pathID(r)
	accept := r.FormValue("decision") == "accept"
	if err := a.st.DecideCancellation(r.Context(), auth.CurrentUser(r), id, accept, r.FormValue("note"), r.FormValue("revision")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// Two events, not one: "nothing will be paid" and "payment is unfrozen" are
	// opposite sentences, and the requester who asked is owed whichever one is
	// true (F-F-06).
	if accept {
		a.fire(r, notify.EventCancellationAccepted, id)
	} else {
		a.fire(r, notify.EventCancellationDeclined, id)
	}
	http.Redirect(w, r, "/approvals?bucket=cancellations", http.StatusSeeOther)
}

// requestCancelOutright is the approver cancelling a request nobody asked to
// cancel. The requester is notified, since they did not see it coming.
func (a *App) requestCancelOutright(w http.ResponseWriter, r *http.Request) {
	a.changeRequest(w, r, notify.EventRequestCancelled, "/requests/{id}", func(ctx context.Context, u store.User, id int64) error {
		return a.st.CancelRequest(ctx, u, id, r.FormValue("reason"), r.FormValue("revision"))
	})
}

// approvalTab is one `.segmented` tab on the manager queue. The queue is its
// own screen rather than a scope of /requests because it answers a different
// question — "what is mine to decide" — and its tabs are statuses, not buckets.
type approvalTab struct {
	Key, Label string
	Statuses   []string
}

var approvalTabs = []approvalTab{
	{"to-approve", "To approve", []string{"pending"}},
	{"cancellations", "Cancellations", []string{"cancellation_requested"}},
	// S11: a shortfall waits on the same manager, so it is listed in the same
	// queue (phase-2 spec §N1, mockups/screens/approvals-list.html). Without
	// this tab a partial review was reachable only through a notification link
	// while the request itself read "Waiting on you" (settlement-3).
	{"partial-review", "Partial review", []string{"partial_review"}},
	{"decided", "Decided", []string{"approved", "rejected", "cancelled"}},
}

// approvals is the manager queue: the requests routed to this person as
// approver (RoutedToViewer), within whatever request data scope they hold.
// Holding approval:approve says you may decide, and the manager_id on the row
// says which requests are yours to decide; the person's own raised requests are
// never in this queue, whatever their scope. There is no bulk approval by
// design (A6).
func (a *App) approvals(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	query := r.URL.Query().Get("q")
	bucket := r.URL.Query().Get("bucket")
	if !knownApprovalTab(bucket) {
		bucket = approvalTabs[0].Key
	}
	counts := map[string]int{}
	var list []store.Request
	for _, tab := range approvalTabs {
		opts := store.RequestListOptions{Scope: a.auth.Scope(u, "request"), ViewerID: u.ID,
			RoutedToViewer: true, Statuses: tab.Statuses, Query: query}
		n, err := a.st.CountRequests(r.Context(), opts)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		counts[tab.Key] = n
		if tab.Key != bucket {
			continue
		}
		if list, err = a.st.ListRequests(r.Context(), opts); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
	a.render(w, r, "approvals", PageData{Title: "Approvals", Requests: list, Bucket: bucket,
		Counts: counts, Query: query})
}

func knownApprovalTab(key string) bool {
	return slices.ContainsFunc(approvalTabs, func(tab approvalTab) bool { return tab.Key == key })
}

func (a *App) requestsExport(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	opts := a.requestListOptions(r)
	opts.Limit = store.RequestsUnlimited // an export for reconciliation must not drop rows
	page, err := a.st.ListRequestsPage(r.Context(), store.RequestPageOptions{RequestListOptions: opts})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	list := page.Requests
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "export",
		EntityType: "payment_request", Summary: "Exported request list"})
	var body bytes.Buffer
	cw := csv.NewWriter(&body)
	_ = cw.Write([]string{"Number", "Status", "Type", "Title", "Amount", "Approved amount", "Payee", "Requester", "Approver", "Created"})
	for _, req := range list {
		// F-G-011: the approved figure beside the requested one; empty until decided.
		approved := ""
		if req.ApprovedAmount != nil {
			approved = csvAmount(*req.ApprovedAmount)
		}
		_ = cw.Write([]string{csvText(req.Number), requestStatusText(csvText(req.Status)), typeLabel(req.Type), csvText(req.ShortTitle),
			csvAmount(req.Amount), approved, csvText(req.Vendor), csvText(req.RequesterName), csvText(req.ManagerName),
			req.CreatedAt.Format("2006-01-02")})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The export could not be generated.", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="requests.csv"`)
	if _, err := w.Write(body.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "csv response write failed", "request_id", requestID(r), "error", err)
	}
}

func optionalID(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

// requestClearURL clears user filters while retaining the access scope, current
// bucket and fixed vendor/report context that brought the reader to the list.
func requestClearURL(r *http.Request, opts store.RequestListOptions) string {
	q := url.Values{"scope": {opts.Scope}, "bucket": {opts.Bucket}}
	if opts.VendorID > 0 {
		q.Set("vendor_id", fmt.Sprint(opts.VendorID))
	}
	if back := safeReportBack(r.URL.Query().Get("back")); back != "" {
		q.Set("back", back)
		if opts.ProjectID > 0 {
			q.Set("project_id", fmt.Sprint(opts.ProjectID))
		}
	}
	if target := auth.SafeReturnPath(r.URL.Query().Get("return_to")); target != "" && target != "/" {
		q.Set("return_to", target)
	}
	return "/requests?" + q.Encode()
}
