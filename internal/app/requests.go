package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/money"
	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

// Payment request screens: raising a request, editing and correcting it, the
// request list and detail page, the approver's decisions (approve, return,
// reject), withdrawal, cancellation and the approvals queue.
//
// Two rules shape this file:
//   - There are no drafts. Creating a request also submits it, in one POST, so
//     a request exists only once it has gone to an approver, and it gets its
//     number then.
//   - The request type comes from the URL (?type=), never from a form control.
//     The type decides which fields the form shows. Changing the treatment or
//     the recoverable category reloads the middle of the form from the server
//     (htmx), so the show/hide rules live only in Go. Hiding a field is not
//     validation: the store checks every field again on save.

// requestTypeOption is one card on the "new request" chooser.
type requestTypeOption struct {
	Key, Label, Icon, Blurb, Tag, TagClass string
}

// requestTypeOptions lists every request type in the order the chooser shows
// them. A type missing here cannot be raised from the UI.
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
	// A deposit or guarantee is paid to another company or authority (not a
	// vendor, not the requester) and is expected back.
	{Key: "recoverable", Label: "Deposit or guarantee", Icon: "⇄",
		Blurb: "EMD, PBG, ICD or a security deposit — money paid to another company or authority that comes back.",
		Tag:   "Paid to the counterparty · always recoverable", TagClass: "recoverable"},
}

// requestTypeLabels maps a type key to its label, built from
// requestTypeOptions. Its keys are the only types the form will render.
var requestTypeLabels = func() map[string]string {
	out := make(map[string]string, len(requestTypeOptions))
	for _, option := range requestTypeOptions {
		out[option.Key] = option.Label
	}
	return out
}()

// unofferedRequestType is the error shown on the chooser, with status 400, when
// /requests/new?type= names a type that has no card.
const unofferedRequestType = "That is not a request type this system raises. Pick one below — a deposit or guarantee has its own card, and an advance to yourself is an employee advance."

// requestNew serves /requests/new. With no ?type= it shows the type chooser;
// with a known type it shows the form for that type; with an unknown type it
// shows the chooser with an error (400).
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
	// An employee advance opens as recoverable, in its own category; the
	// requester may still switch it to a budget expense. A deposit or guarantee
	// is always recoverable and opens with no category chosen.
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

// requestFormFields returns the middle of the request form (the part that
// depends on treatment and category) as an htmx fragment. The browser asks for
// it whenever the treatment, category or project changes.
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
	// Keep what the requester already typed in these fields.
	data.Request2.ProjectID = optionalID(parseID(q.Get("project_id")))
	data.Request2.HeadID = optionalID(parseID(q.Get("head_id")))
	data.Request2.Counterparty = q.Get("counterparty")
	data.Request2.ExpectedReturnDate = q.Get("expected_return_date")
	data.Request2.RepaymentNotes = q.Get("repayment_notes")
	a.renderPartial(w, r, "request_form_fields", data)
}

// normalizeRecoverableCategory returns code if it is one of the active
// categories in cats. An unknown or inactive code becomes "" (nothing chosen),
// so the person has to pick one; it is never swapped for another category. An
// empty code takes the category named like the form type, if active (this is
// how an employee advance opens already categorised).
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

// categoriesForType returns the recoverable categories a request type may use.
// An employee advance pays the requester, so it may only use the "Employee
// advance" category; a deposit or guarantee pays a counterparty, so it may use
// any category except that one. The store enforces the same rule on save.
// Other types get the full list.
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

// resolveRecoverableCategory returns the category row for code, so the form can
// show the fields that category requires. An unknown code returns the zero
// value, which requires nothing, the same as "no category chosen".
func resolveRecoverableCategory(code string, cats []store.RecoverableCategory) store.RecoverableCategory {
	for _, c := range cats {
		if c.Code == code {
			return c
		}
	}
	return store.RecoverableCategory{}
}

// requestCreate raises and submits a new request in one step: it saves any
// uploaded file to a staging path, creates the request (already pending, with
// its number) together with the attachment, and redirects to the confirmation.
// If anything fails nothing is saved, the staged file is deleted, and the form
// is shown again with the message (or the error page for a server fault).
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

// renderRejectedRequestForm shows the new-request form again with the error
// message, per-field errors and everything the requester typed. If the posted
// type has no card, it answers 400 when the store did not know the type at all,
// and otherwise shows the chooser with the store's message.
func (a *App) renderRejectedRequestForm(w http.ResponseWriter, r *http.Request, status int, in store.RequestInput, refusal error) {
	message := friendly(refusal)
	label, known := requestTypeLabels[in.Type]
	if !known {
		// The store's own message tells an unknown type apart from a type the
		// store knows but the chooser lacks, so the type list is not repeated here.
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

// requestFromInput copies the posted fields into a store.Request, the shape
// the form renders from, so a rejected submission shows what was typed.
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

// requestSubmitted is the confirmation page after a request is raised. Its
// "what happens next" text quotes the configured reminder wait.
func (a *App) requestSubmitted(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	thresholds, err := a.st.ReminderThresholds(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "request_submitted", PageData{Title: "Request submitted", Request2: req, Reminders: thresholds})
}

// canSeeRequest reports whether the signed-in user's request data scope
// ("own", "assigned" or "all") covers req. It uses store.RequestInScope, the
// same rule the store's list queries use, so lists and detail pages agree.
//
// It checks only the scope, not the request:view permission: each route
// already requires its own permission (request:view, payment:view,
// recoverable_report:view …) before the handler runs. Notifications, which
// have no route in front of them, use store.CanViewRequest, which checks both.
func (a *App) canSeeRequest(r *http.Request, req store.Request) bool {
	return a.userSeesRequest(auth.CurrentUser(r), req)
}

// loadRequestByID reads request id and returns it if the signed-in user's data
// scope covers it. Otherwise it has already answered and returns false. A
// request outside the scope gets the same 404 as a missing one, so status codes
// never reveal which request ids exist.
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

// requestDuplicateCheck returns the "possible duplicate" warning fragment for
// the form being filled in. It is only advice: it always answers 200, saving a
// request never depends on it, and a failed check is logged and shows nothing.
// Only matches the reader may see are listed.
func (a *App) requestDuplicateCheck(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	amount, _ := money.ParsePaise(r.FormValue("amount"))
	payee := r.FormValue("vendor_payee")
	// Reimbursements and employee advances are paid to the requester, so that
	// is the payee to look for.
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

// payeeIsRequester reports whether requests of type t are always paid to the
// requester (the store applies the same rule when saving).
func payeeIsRequester(t string) bool {
	return t == "reimbursement" || t == "employee_advance"
}

// vendorChoices lists the active vendors for the form's plain vendor <select>,
// used when the search box cannot run (no JavaScript, or no vendor:view
// permission for /vendors/search). Types without a vendor get nil.
func (a *App) vendorChoices(r *http.Request, formType string) ([]store.Vendor, error) {
	if formType != "vendor_invoice" && formType != "vendor_advance" {
		return nil, nil
	}
	return a.st.ListVendors(r.Context(), store.VendorListOptions{Status: "active", Limit: 500},
		a.auth.Permissions(auth.CurrentUser(r)))
}

// requestInput reads every field the request form can show, whether or not
// the form showed it; the store decides which fields a request may carry. An
// amount that does not parse is returned as a validation error.
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

// requestFormData loads what every request form needs: active projects and
// heads, the approvers (never the requester), settings, active recoverable
// categories and reminder thresholds. The user's default approver is
// preselected.
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
	cats, err := a.st.ListRecoverableCategories(ctx, true)
	if err != nil {
		return PageData{}, err
	}
	thresholds, err := a.st.ReminderThresholds(ctx)
	if err != nil {
		return PageData{}, err
	}
	data := PageData{Title: title, Projects: projects, Heads: heads, Approvers: approvers,
		Settings: settings, Categories: cats, Reminders: thresholds}
	// 0 means no default approver; the select then opens on "Choose an approver".
	if u.DefaultApproverID > 0 {
		data.Request2.ManagerID = u.DefaultApproverID
	}
	return data, nil
}

// requestCardData is the data for the "request_card" template: the request and
// the id of the person viewing it (needed for the "waiting on" line, and not
// reachable from inside a {{range}} over requests).
type requestCardData struct {
	Req      store.Request
	ViewerID int64
}

// requestTab is one tab on the /requests list. Key is the store's bucket name.
type requestTab struct{ Key, Label string }

// requestTabs are the tabs on the /requests list, in display order.
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
	// Each tab's count uses the same filters as the rows, with only the bucket
	// changed (and no status filter).
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

// requestDetail is the request page, the same for everyone who may see the
// request; which action buttons appear depends on the reader's permissions.
// The requester of a returned request (with request:edit) gets the correction
// form instead.
func (a *App) requestDetail(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
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

// showsReturnedCorrection reports whether the reader should get the correction
// form for req: it is returned, they raised it, and they hold request:edit.
func (a *App) showsReturnedCorrection(r *http.Request, req store.Request) bool {
	u := auth.CurrentUser(r)
	return req.Status == "returned" && req.RequesterID == u.ID && a.auth.Can(u, "request", "edit")
}

// requestDetailData loads what every single-request screen shows: the history
// thread, documents, edit revision, payments, the latest payment (if any) and,
// for a reader who may reassign the approval, the list of possible approvers.
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
	// Possible new approvers: everyone who may approve except the requester,
	// the same rule store.ReassignRequest enforces.
	if data.CanReassignApprover = a.mayReassignApprover(auth.CurrentUser(r), req); data.CanReassignApprover {
		approvers, aerr := a.st.ListApprovers(r.Context(), req.RequesterID)
		if aerr != nil {
			return PageData{}, aerr
		}
		data.Approvers = approvers
	}
	// Most requests have no payment yet; that is not an error.
	pay, perr := a.st.PaymentForRequest(r.Context(), req.ID)
	switch {
	case perr == nil:
		data.Payment = pay
	case !errors.Is(perr, store.ErrNotFound):
		return PageData{}, perr
	}
	return data, nil
}

// mayReassignApprover reports whether u may hand req's approval to another
// approver. It needs approval:reassign, a status the store allows reassigning
// from, and either being req's current approver or holding user:edit (an
// administrator, who can rescue requests left with an approver who cannot
// act). Other managers may not, or they could take and approve each other's
// requests. The detail page and the route both use this.
func (a *App) mayReassignApprover(u store.User, req store.Request) bool {
	if !a.auth.Can(u, "approval", "reassign") || !store.Reassignable(req.Status) {
		return false
	}
	return req.ManagerID == u.ID || a.auth.Can(u, "user", "edit")
}

// requestEditForm shows the edit form for a pending or returned request. Only
// the requester may open it; an approver who wants changes returns the request.
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

// loadEditableRequest loads the request in the URL for editing. It answers 403
// unless the reader raised it, and 400 (with the reason) unless it is pending
// or returned, the only statuses the store lets a requester edit.
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

// reqStatusExplain is a short sentence saying why a request in status cannot
// be edited or cancelled, and what to do instead.
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

// requestEditData builds the edit form for req: the same lists the new-request
// form uses, plus the request as it stands, its history and its documents. It
// also adds the warnings about legacy deposits and retired projects or heads.
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

// legacyDepositCategory returns the deposit category of an older employee
// advance that was filed under a deposit category (EMD, ICD …) before deposits
// and guarantees had their own request type, or "" for any other request. It
// checks the stored code, not whether that category is still active.
func legacyDepositCategory(req store.Request) string {
	if req.Type == "employee_advance" && req.Treatment == "recoverable" &&
		req.RecoverableCategory != "" && req.RecoverableCategory != "employee_advance" {
		return req.RecoverableCategory
	}
	return ""
}

// noteLegacyDeposit handles such a legacy employee advance on the edit and
// returned screens: the form offers it as an Employee advance (saving will
// refile it that way), and a warning explains this before the user saves. If
// the money really goes to a counterparty, the warning says how to raise a
// deposit instead: a pending request can be withdrawn now, while a returned
// one must be resubmitted first, since that is its only allowed next step.
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

// offerRetiredRefs handles a request whose project or head was retired after
// it was raised. The form's selects list only active items, so the retired one
// is added back (marked inactive) and a warning asks the user to choose an
// open project and head, because the store will not save a retired one. A head
// counts as retired here when it or its project is inactive.
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

// requestEdit saves the requester's corrections. With submit_action=resubmit
// (the returned screen's main button) it also sends a returned request back
// for approval. The edit, a new attachment and the resubmit are saved in one
// store transaction: all of them or none. A refusal shows the form again.
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
	// The approver is told the request "was edited and re-sent for approval".
	// That is true for a resubmit and for an edit of a pending request, but not
	// for "Save corrections" on a returned request, which stays returned.
	if resubmitting || req.Status == "pending" {
		a.fire(r, notify.EventRequestEdited, req.ID)
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
}

// renderRejectedEdit shows the edit (or returned) screen again with the error
// message and what was typed; fields the form does not edit come from the
// stored request.
func (a *App) renderRejectedEdit(w http.ResponseWriter, r *http.Request, stored store.Request, in store.RequestInput, status int, refusal error) {
	data, err := a.requestEditData(r, editedRequest(stored, in))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// The posted values already say Employee advance, so the legacy-deposit
	// warning is based on the stored request.
	noteLegacyDeposit(&data, stored)
	data.Error = friendly(refusal)
	data.UrgencyReasonError = errors.Is(refusal, store.ErrUrgencyReasonRequired)
	data.RequestRevision = r.FormValue("revision") // keep the posted revision, so a stale form stays stale
	data.RequestRawAmount = r.FormValue("amount")
	data.RequestFieldErrors = requestFieldErrors(in)
	data.DuplicateReason = in.DuplicateReason
	name := "request_edit"
	if stored.Status == "returned" {
		name = "request_returned"
	}
	a.renderStatus(w, r, status, name, data)
}

// editedRequest returns the posted values laid over the stored request. The
// number, status, names, decisions and dates are not form fields, so they come
// from stored.
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

// requestApprove approves a pending request, possibly for less than was
// asked. Only the request's approver may; anyone else gets 404. Approval is
// refused (409) when the needed-by month is locked. An amount the store
// refuses shows the approve sheet again with the message. The store also
// refuses a requester approving their own request.
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
	if err := a.st.ApproveRequest(r.Context(), u, req.ID, amount, r.FormValue("note"), r.FormValue("revision")); err != nil {
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

// refuseRequestApproval shows the request page again with the approve sheet
// open and message on it (400). The request is re-read first, so if someone
// else decided it meanwhile the sheet is not offered again.
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
	data.RequestRevision = r.FormValue("revision") // keep the posted revision, so a stale form stays stale
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

// requestReraise raises a rejected request again as a new pending request
// with its own number, notifies the approver about the new one, and shows its
// confirmation page.
func (a *App) requestReraise(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.loadViewableRequest(w, r); !ok {
		return
	}
	id, err := a.st.ReraiseRequest(r.Context(), auth.CurrentUser(r), pathID(r))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.fire(r, notify.EventRequestReraised, id)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d/submitted", id), http.StatusSeeOther)
}

// requestComment adds a comment to the request's conversation.
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
	// A comment made from the partial-review screen returns there. return_to
	// is a fixed token, not a URL, so the form cannot redirect anywhere else.
	if r.FormValue("return_to") == "partial-review" && req.Status == "partial_review" {
		dest += "/partial-review"
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// requestAttachmentUpload adds one document to a request that Accounts has put
// on hold (for example "Need GST receipt"), without changing anything else.
// Only the requester may, and only while it is on hold. These checks run
// before the file is saved; the store repeats them inside its transaction.
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
		removeStagedAttachment(a.log, r, stagedPath)
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", req.ID), http.StatusSeeOther)
}

// Cancellation. An approved request cannot simply be withdrawn: the requester
// asks for cancellation, payment is frozen, and the approver accepts or
// declines. The approver may also cancel a request outright, with a reason.

// requestCancelForm shows the requester the "ask to cancel" form. Only the
// requester (403 for anyone else) and only for an approved request (400).
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

// requestCancellationForm shows the approver's cancellation screen, used both
// to decide a requested cancellation and to cancel outright. Only the
// request's approver may open it (403 otherwise).
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

// requestCancellationDecide records the approver's decision on a requested
// cancellation (decision=accept, or decline) and tells the requester which it
// was: accepted means nothing will be paid, declined means payment resumes.
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

// approvalTab is one tab on the approvals queue, selecting requests by status.
type approvalTab struct {
	Key, Label string
	Statuses   []string
}

// approvalTabs are the approvals queue's tabs, in display order.
var approvalTabs = []approvalTab{
	{"to-approve", "To approve", []string{"pending"}},
	{"cancellations", "Cancellations", []string{"cancellation_requested"}},
	// A partial payment waits on the same approver to accept the shortfall.
	{"partial-review", "Partial review", []string{"partial_review"}},
	{"decided", "Decided", []string{"approved", "rejected", "cancelled"}},
}

// approvals is the approver's queue: requests sent to the reader as approver,
// within their request data scope, one tab per status group with a count on
// each. Requests the reader raised themselves never appear here. Each request
// is decided on its own page; there is no bulk approval.
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

// knownApprovalTab reports whether key names one of approvalTabs.
func knownApprovalTab(key string) bool {
	return slices.ContainsFunc(approvalTabs, func(tab approvalTab) bool { return tab.Key == key })
}

// requestsExport downloads the /requests list as requests.csv, using the same
// filters as the screen but with no row limit. The export is audited.
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
	rows := make([][]string, 0, len(list))
	for _, req := range list {
		// The approved amount is empty until the request is decided.
		approved := ""
		if req.ApprovedAmount != nil {
			approved = csvAmount(*req.ApprovedAmount)
		}
		rows = append(rows, []string{csvText(req.Number), requestStatusText(csvText(req.Status)), typeLabel(req.Type), csvText(req.ShortTitle),
			csvAmount(req.Amount), approved, csvText(req.Vendor), csvText(req.RequesterName), csvText(req.ManagerName),
			req.CreatedAt.Format(time.DateOnly)})
	}
	a.sendCSV(w, r, "requests.csv",
		[]string{"Number", "Status", "Type", "Title", "Amount", "Approved amount", "Payee", "Requester", "Approver", "Created"}, rows)
}

// optionalID returns nil for an id of 0 or less, else a pointer to id.
func optionalID(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

// requestClearURL is the "clear filters" link on /requests. It drops the
// user's filters but keeps the scope, the current tab, and the vendor or
// report the reader came from.
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
