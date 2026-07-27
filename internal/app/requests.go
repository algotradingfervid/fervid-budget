package app

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
		Tag:   "Usually recoverable", TagClass: "recoverable"},
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

// requestNew is both steps of the new-request flow on one route. With no
// ?type= — or an unrecognised one — it is the chooser; with a known type it is
// the form for that type and nothing else (A16).
func (a *App) requestNew(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("type")
	label, known := requestTypeLabels[kind]
	if !known {
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
	// An employee advance is money the organisation expects to see accounted
	// for, so it opens on the recoverable treatment already categorised. The
	// requester can still switch it to a budget expense.
	if kind == "employee_advance" {
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
		status := storeErrorStatus(err)
		if status >= http.StatusInternalServerError {
			a.respondStoreError(w, r, err)
			return
		}
		a.renderRejectedRequestForm(w, r, status, in, friendly(err))
		return
	}
	a.fire(r, notify.EventRequestSubmitted, id)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d/submitted", id), http.StatusSeeOther)
}

// renderRejectedRequestForm puts the form back with the message and everything
// the requester had typed still in it. Losing a page of typing to one bad field
// is the cruellest thing a form can do.
func (a *App) renderRejectedRequestForm(w http.ResponseWriter, r *http.Request, status int, in store.RequestInput, message string) {
	label, known := requestTypeLabels[in.Type]
	if !known {
		// Two different situations, and they used to answer the same way.
		//
		// A type the *store* does not recognise is a client error about the type,
		// and "that is not a kind of request this system raises" is the right
		// sentence for it. But the store's fifth type, `recoverable`, has no
		// chooser card and therefore no label, while CreateRequest accepts it
		// happily — so a refused one lost both the real reason and the whole form,
		// and was told the system does not raise a kind of request it had raised
		// seconds earlier (F-E-08/F-B-02). That one keeps the rule that refused it
		// and comes back on the chooser, which is the nearest screen there is.
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
	data.FormType = in.Type
	data.Request2 = requestFromInput(in)
	data.RecCategory = resolveRecoverableCategory(in.RecoverableCategory, data.Categories)
	if data.Vendors, err = a.vendorChoices(r, in.Type); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.renderStatus(w, r, status, "request_form", data)
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

// loadViewableRequest resolves the {id} in the path and refuses it to anybody
// whose data scope does not reach it — holding request:view somewhere is not
// the same as being allowed to read this one (Q5/R6).
//
// The refusal is 404, not 403. A row that exists but is out of scope used to
// answer 403 while a row that does not exist answered 404, so the status code
// was an existence oracle: a requester could walk the id space and learn exactly
// which request ids exist, and by extension how many requests the company raises
// (F-G-002). The existence of a row is information about that row, so the two
// cases have to be indistinguishable. The attempt is still logged.
func (a *App) loadViewableRequest(w http.ResponseWriter, r *http.Request) (store.Request, bool) {
	u := auth.CurrentUser(r)
	req, err := a.st.Request(r.Context(), pathID(r))
	if err != nil {
		a.respondStoreError(w, r, err)
		return store.Request{}, false
	}
	if !canViewRequest(a.auth.Scope(u, "request"), u, req) {
		a.respondError(w, r, http.StatusNotFound, "The requested record was not found.",
			fmt.Errorf("request %d is outside the caller's data scope", req.ID))
		return store.Request{}, false
	}
	return req, true
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

// canViewRequest answers the URL-typing question: this caller holds request:view
// somewhere, but may they see *this* row? Q5/R6.
func canViewRequest(scope string, u store.User, req store.Request) bool {
	switch scope {
	case "all":
		return true
	case "assigned":
		return req.ManagerID == u.ID || req.RequesterID == u.ID
	case "own":
		return req.RequesterID == u.ID
	default:
		return false
	}
}

// effectiveScope lets a URL narrow the caller's scope and never widen it, so
// "?scope=own" is a filter an admin may use and "?scope=all" is not a way in.
func (a *App) effectiveScope(u store.User, requested string) string {
	scope := a.auth.Scope(u, "request")
	rank := map[string]int{"own": 1, "assigned": 2, "all": 3}
	if rank[requested] > 0 && rank[requested] < rank[scope] {
		return requested
	}
	return scope
}

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

// Waiting is the "waiting on" line: one plain sentence naming who owes the
// next action on a request. Class is the Phase 0 modifier — "you" in brand
// colour when it is the reader, "closed" when nobody owes anything any more.
// It is computed once, here, so the list, the approvals queue and the detail
// head cannot drift into three different answers.
type Waiting struct {
	Text  string
	Class string
}

func waitingOn(req store.Request, viewerID int64) Waiting {
	you := Waiting{Text: "Waiting on you", Class: "you"}
	// A hold is not a status — the request stays 'approved' so the queue's own
	// availability test takes it out of the takeable set (L7) — but it is the
	// only thing anybody is waiting on while it lasts. It therefore answers
	// before the status does, or a held request would read "Waiting on
	// Accounts" while Accounts is the one waiting.
	//
	// activeHold, not req.OnHold: answering before the status is only right
	// while 'approved' is the status. On anything else there is a real decision
	// pending, and hiding it behind the hold would take the approvals queue's
	// answer away from the one person who owes it.
	if activeHold(req) {
		if req.RequesterID == viewerID {
			return you
		}
		return Waiting{Text: "Waiting on " + req.RequesterName + " to clarify"}
	}
	switch req.Status {
	case "pending", "cancellation_requested":
		if req.ManagerID == viewerID {
			return you
		}
		return Waiting{Text: "Waiting on " + req.ManagerName}
	case "returned":
		if req.RequesterID == viewerID {
			return you
		}
		return Waiting{Text: "Waiting on " + req.RequesterName}
	case "approved":
		return Waiting{Text: "Waiting on Accounts"}
	case "rejected":
		return Waiting{Text: "Closed. Raise a new request if needed", Class: "closed"}
	case "withdrawn":
		return Waiting{Text: "Withdrawn by the requester", Class: "closed"}
	case "cancelled":
		return Waiting{Text: "Cancelled. Nothing can be paid against it", Class: "closed"}
	case "processing":
		if req.ProcessingBy != nil && *req.ProcessingBy == viewerID {
			return you
		}
		return Waiting{Text: "Waiting on Accounts"}
	case "partial_review":
		if req.ManagerID == viewerID {
			return you
		}
		return Waiting{Text: "Waiting on " + req.ManagerName}
	case "completed", "completed_partial":
		return Waiting{Text: "Nothing pending", Class: "done"}
	default:
		return Waiting{}
	}
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

func (a *App) requests(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	q := r.URL.Query()
	bucket := q.Get("bucket")
	if bucket == "" {
		bucket = "open"
	}
	// ?status= is honoured, as the CSV of the same URL already did. The screen used
	// to ignore it silently, so one URL described two different sets on the page
	// and in the file (F-G-014).
	//
	// It is passed as Statuses rather than Status because requestWhere reads Bucket
	// *before* Status and the bucket always has a default — so a named status set
	// on Status alone would go on being ignored. Statuses is the one field that
	// wins, which is the right precedence anyway: the tab is where the reader is,
	// the status is what they asked for.
	status := q.Get("status")
	opts := store.RequestListOptions{Scope: a.effectiveScope(u, q.Get("scope")), ViewerID: u.ID,
		Bucket: bucket, Type: q.Get("type"), Treatment: q.Get("treatment"),
		ProjectID: parseID(q.Get("project_id")), Query: q.Get("q")}
	if status != "" && status != "all" {
		opts.Statuses = []string{status}
	}
	// ListRequestsPage, not ListRequests: the row query was capped at 200 while
	// the tab count beside it had no cap, so the All tab promised 214 and the list
	// drew 200 with nothing on the page saying so (F-B-16). Total and Truncated are
	// what the screen shows, and Offset is what makes the rest reachable.
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
		Scope: opts.Scope, Bucket: bucket, Status: status,
		TypeFilter: opts.Type, Treatment: opts.Treatment, Query: opts.Query,
		Counts: counts, Projects: projects})
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
	// A7: the reassign-the-approval control needs somebody to reassign it to. The
	// list is ListApprovers — everyone holding approval:approve except the
	// requester — which is the same list the request form offers and the same rule
	// store.ReassignRequest enforces, so the select can never name a target the
	// POST would refuse. It is loaded only for a caller who may actually use the
	// control (F-A-06/F-C-02).
	if a.auth.Can(auth.CurrentUser(r), "approval", "reassign") {
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
	data.FormType = req.Type
	data.RecCategory = resolveRecoverableCategory(req.RecoverableCategory, data.Categories)
	if data.Thread, err = a.st.RequestThread(r.Context(), req.ID); err != nil {
		return PageData{}, err
	}
	if data.RequestAtts, err = a.st.RequestAttachments(r.Context(), req.ID); err != nil {
		return PageData{}, err
	}
	if data.Vendors, err = a.vendorChoices(r, req.Type); err != nil {
		return PageData{}, err
	}
	return data, nil
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
	var attachment *store.AttachmentInput
	var stagedPath string
	if err == nil {
		attachment, stagedPath, err = a.stageUploadedAttachment(r)
	}
	// Every precondition SubmitRequest will check, checked before UpdateRequest
	// writes anything (F-G-035/F-C-04). The three calls below are three
	// transactions, so a resubmit that failed on its own preconditions used to
	// leave the edit committed *and audited as done* while the requester was shown
	// a 400 — the history asserting a change the caller had been told did not
	// happen. Nothing here writes, so a refusal now costs nothing.
	if err == nil && resubmitting {
		err = a.canResubmit(r, req, in, attachment != nil)
	}
	if err == nil {
		err = a.st.UpdateRequest(r.Context(), u, req.ID, in)
	}
	if err == nil && attachment != nil {
		_, err = a.st.AddRequestAttachment(r.Context(), u, req.ID, *attachment)
	}
	if err == nil && resubmitting {
		err = a.st.SubmitRequest(r.Context(), u, req.ID)
	}
	if err != nil {
		removeStagedAttachment(a.log, r, stagedPath)
		status := storeErrorStatus(err)
		if status >= http.StatusInternalServerError {
			a.respondStoreError(w, r, err)
			return
		}
		a.renderRejectedEdit(w, r, req, in, status, friendly(err))
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

// canResubmit answers "would SubmitRequest refuse this?" without writing.
//
// It mirrors store.SubmitRequest's own three checks in the same order and with
// the same messages: the requester is the actor (loadEditableRequest has already
// established that), the status has an edge to 'pending', and the attachment
// policy holds. The attachment count includes the document this POST is carrying,
// because SubmitRequest would see it too.
func (a *App) canResubmit(r *http.Request, req store.Request, in store.RequestInput, addingAttachment bool) error {
	// legalTransitions has no pending→pending edge, so only a returned request
	// can be sent back. This is the reachable trigger the finding names.
	if req.Status != "returned" {
		return fmt.Errorf("%w: a %s request cannot be submitted", store.ErrValidation, req.Status)
	}
	required, err := a.st.AppSetting(r.Context(), "require_attachments")
	if err != nil {
		return err
	}
	if required != "1" {
		return nil
	}
	atts, err := a.st.RequestAttachments(r.Context(), req.ID)
	if err != nil {
		return err
	}
	if len(atts) > 0 || addingAttachment {
		return nil
	}
	if strings.TrimSpace(in.AttachmentExceptionReason) == "" {
		return fmt.Errorf("%w: attach a supporting document, or say why you cannot", store.ErrValidation)
	}
	return nil
}

// renderRejectedEdit puts the correction screen back with the message and
// everything that was typed still in it, re-read from the row only for the
// parts the form does not own.
func (a *App) renderRejectedEdit(w http.ResponseWriter, r *http.Request, stored store.Request, in store.RequestInput, status int, message string) {
	data, err := a.requestEditData(r, editedRequest(stored, in))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data.Error = message
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
	amount, err := money.ParsePaise(r.FormValue("approved_amount"))
	if err != nil {
		a.respondError(w, r, http.StatusBadRequest, "Enter the amount you are approving.", err)
		return
	}
	locked, err := a.lockedApprovalMonth(r)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if locked != "" {
		a.respondError(w, r, http.StatusConflict,
			locked+" is locked, so this request cannot be approved for payment in it. Reopen the month, or ask for the required-by date to be changed.", nil)
		return
	}
	if err := a.st.ApproveRequest(r.Context(), auth.CurrentUser(r), pathID(r), amount, r.FormValue("note")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.fire(r, notify.EventRequestApproved, pathID(r))
	http.Redirect(w, r, "/approvals", http.StatusSeeOther)
}

// lockedApprovalMonth names the locked month an approval would commit to, or ""
// when there is none (F-G-021).
//
// An approval is a promise that the request can be paid, and validatePayment
// refuses a paid_on inside a locked month — so approving into one manufactures an
// obligation nobody can discharge until the month is reopened, and the accountant
// only discovers it at the settlement, having filled in the whole form. The month
// tested is the one the requester asked for: needed_by is the only date a request
// carries, and a request with no needed_by names no period and is unaffected.
func (a *App) lockedApprovalMonth(r *http.Request) (string, error) {
	req, err := a.st.Request(r.Context(), pathID(r))
	if err != nil {
		return "", err
	}
	if len(req.NeededBy) < 7 {
		return "", nil
	}
	month := req.NeededBy[:7]
	if !a.st.IsLocked(r.Context(), month) {
		return "", nil
	}
	return month, nil
}

func (a *App) requestReturn(w http.ResponseWriter, r *http.Request) {
	if err := a.st.ReturnRequest(r.Context(), auth.CurrentUser(r), pathID(r), r.FormValue("comment")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.fire(r, notify.EventRequestReturned, pathID(r))
	http.Redirect(w, r, "/approvals", http.StatusSeeOther)
}

func (a *App) requestReject(w http.ResponseWriter, r *http.Request) {
	if err := a.st.RejectRequest(r.Context(), auth.CurrentUser(r), pathID(r), r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.fire(r, notify.EventRequestRejected, pathID(r))
	http.Redirect(w, r, "/approvals", http.StatusSeeOther)
}

func (a *App) requestWithdraw(w http.ResponseWriter, r *http.Request) {
	if err := a.st.WithdrawRequest(r.Context(), auth.CurrentUser(r), pathID(r)); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// The approver's queue item has just disappeared without a decision, so they
	// are told why (F-F-06).
	a.fire(r, notify.EventRequestWithdrawn, pathID(r))
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", pathID(r)), http.StatusSeeOther)
}

// requestReraise lands on the confirmation screen rather than the source, and
// it lands there for the *new* request: D1 makes a re-raise a fresh pending
// request with its own number, not a draft copy of the rejected one.
func (a *App) requestReraise(w http.ResponseWriter, r *http.Request) {
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

func (a *App) requestCancelAsk(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := a.st.RequestCancellation(r.Context(), auth.CurrentUser(r), id, r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.fire(r, notify.EventCancellationRequested, id)
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", id), http.StatusSeeOther)
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
	id := pathID(r)
	accept := r.FormValue("decision") == "accept"
	if err := a.st.DecideCancellation(r.Context(), auth.CurrentUser(r), id, accept, r.FormValue("note")); err != nil {
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

func (a *App) requestCancelOutright(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if err := a.st.CancelRequest(r.Context(), auth.CurrentUser(r), id, r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/requests/%d", id), http.StatusSeeOther)
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
	{"decided", "Decided", []string{"approved", "rejected", "cancelled"}},
}

// approvals is the manager queue. Scope is always "assigned": holding
// approval:approve says you may decide, and the manager_id on the row says
// which requests are yours to decide. There is no bulk approval by design (A6).
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
		opts := store.RequestListOptions{Scope: "assigned", ViewerID: u.ID,
			Statuses: tab.Statuses, Query: query}
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
	for _, tab := range approvalTabs {
		if tab.Key == key {
			return true
		}
	}
	return false
}

func (a *App) requestsExport(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	q := r.URL.Query()
	// The same bucket default the screen has. Without it a bare
	// /requests/export.csv exported every status while /requests showed only the
	// open ones, so the export silently meant something different from the list it
	// sits on (F-G-014).
	bucket := q.Get("bucket")
	if bucket == "" {
		bucket = "open"
	}
	// RequestsUnlimited: an export taken for reconciliation that drops fourteen
	// rows without saying so is worse than no export (F-B-16/D3).
	opts := store.RequestListOptions{
		Scope: a.effectiveScope(u, q.Get("scope")), ViewerID: u.ID,
		Bucket: bucket, Type: q.Get("type"),
		Treatment: q.Get("treatment"), ProjectID: parseID(q.Get("project_id")), Query: q.Get("q"),
		Limit: store.RequestsUnlimited}
	// Statuses, not Status, and for the same reason the screen uses it: the bucket
	// default would otherwise swallow a named status now that the export has one.
	if status := q.Get("status"); status != "" && status != "all" {
		opts.Statuses = []string{status}
	}
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
	_ = cw.Write([]string{"Number", "Status", "Type", "Title", "Amount", "Payee", "Requester", "Approver", "Created"})
	for _, req := range list {
		_ = cw.Write([]string{req.Number, requestStatusText(req.Status), typeLabel(req.Type), req.ShortTitle,
			csvAmount(req.Amount), req.Vendor, req.RequesterName, req.ManagerName,
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

// ---------------------------------------------------------------------------
// Display helpers. Every one of them is registered in the template FuncMap, so
// a status or a type is spelled for a person in exactly one place.
// ---------------------------------------------------------------------------

// pillClass maps a stored status onto the Phase 0 `.pill` modifier. Phase 0
// owns the CSS; this owns nothing but the mapping.
func pillClass(status string) string {
	switch status {
	case "pending":
		return "awaiting"
	case "cancellation_requested":
		return "cancelreq"
	case "withdrawn":
		return "cancelled"
	// The Phase-3 statuses whose enum name is not the design system's class
	// name. Everything else already matches, and a status with no rule of its
	// own would render an invisible pill.
	case "partial_review":
		return "partial"
	case "completed_partial":
		return "completed-partial"
	default:
		return status
	}
}

// requestStatusText is the sentence a person reads, not the enum value. It is
// deliberately not called statusText: that name already belongs to the variance
// grid's budget status, and one word meaning two things is how they drift.
func requestStatusText(status string) string {
	switch status {
	case "pending":
		return "Awaiting approval"
	case "returned":
		return "Returned for correction"
	case "approved":
		return "Approved — awaiting payment"
	case "rejected":
		return "Rejected — final"
	case "withdrawn":
		return "Withdrawn"
	case "cancellation_requested":
		return "Cancellation requested"
	case "cancelled":
		return "Cancelled"
	case "processing":
		return "With Accounts"
	case "partial_review":
		return "Partial — manager review"
	case "completed":
		return "Completed"
	case "completed_partial":
		return "Completed — partial accepted"
	default:
		return status
	}
}

func typeLabel(t string) string {
	if label, ok := requestTypeLabels[t]; ok {
		return label
	}
	return t
}

// recoverableCategoryLabels names the Phase-2 recoverable categories. Phase 4
// replaces the map with rows from `recoverable_categories`.
var recoverableCategoryLabels = map[string]string{
	"emd":              "EMD — earnest money deposit",
	"pbg":              "PBG — performance bank guarantee",
	"icd":              "ICD — inter-corporate deposit",
	"employee_advance": "Employee advance",
	"security_deposit": "Security deposit",
	"other":            "Other",
}

func recoverableLabel(code string) string {
	if label, ok := recoverableCategoryLabels[code]; ok {
		return label
	}
	return code
}

// formatLongDate turns a stored YYYY-MM-DD into the date a person would say.
func formatLongDate(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("2 January 2006")
}

// amountValue renders paise for a `.money-field` input, which draws its own ₹
// in the `.cur` prefix — so the value must not carry a second one.
func amountValue(paise int64) string {
	if paise <= 0 {
		return ""
	}
	return strings.TrimPrefix(money.FormatPaise(paise), "₹")
}

// threadDot and threadGlyph decorate one line of the merged thread. Kind wins
// over Action: a comment is shown as the person who wrote it, whatever the
// audit called the event behind it.
//
// The hold pair is here because the hold lives on this stream, not on a trail
// of its own: request-on-hold.html renders it as a warn ⏸, and "unhold" is a
// return to the takeable queue, which is what auditTone tones "brand". The two
// helpers describe the same two events and have to agree about them.
func threadDot(e store.ThreadEntry) string {
	switch e.Action {
	case "submit", "reraise", "unhold":
		return "brand"
	case "approve":
		return "ok"
	case "return", "cancel_request", "cancel_decline", "hold":
		return "warn"
	case "reject", "cancel", "withdraw":
		return "bad"
	default:
		return ""
	}
}

func threadGlyph(e store.ThreadEntry) string {
	switch e.Kind {
	case "comment":
		return e.Initials
	case "attachment":
		return "⇪"
	}
	switch e.Action {
	case "submit", "reraise":
		return "＋"
	case "approve":
		return "✓"
	case "return":
		return "↩"
	case "reject", "withdraw":
		return "✕"
	case "update":
		return "✎"
	case "cancel", "cancel_request", "cancel_decline", "hold":
		return "⏸"
	case "unhold":
		return "◷"
	default:
		return "·"
	}
}

// threadValue renders one side of a `.tl-change` the way a person reads it. The
// audit trail stores raw JSON, so an amount arrives as 2.35e+07 and a flag as
// true — neither is a sentence anybody wants to find in their own history.
func threadValue(field, raw string) string {
	switch raw {
	case "", "<nil>", "null":
		return "—"
	}
	switch field {
	case "amount", "approved_amount":
		if paise, err := strconv.ParseFloat(raw, 64); err == nil {
			return money.FormatPaise(int64(paise))
		}
	case "urgent":
		if raw == "true" || raw == "1" {
			return "Urgent"
		}
		return "Normal"
	}
	return raw
}

// threadField names a changed column for a reader. The audit records column
// names, which are the right key and the wrong words.
func threadField(column string) string {
	if label, ok := map[string]string{
		"amount": "Amount", "approved_amount": "Approved amount", "needed_by": "Needed by",
		"invoice_no": "Invoice number", "invoice_date": "Invoice date", "expense_date": "Expense date",
		"manager_id": "Approver", "short_title": "Short title", "purpose": "Purpose",
		"urgent": "Urgency",
	}[column]; ok {
		return label
	}
	return strings.ReplaceAll(column, "_", " ")
}

// fileKind is the three-or-four letter tag the `.f-ico` square shows.
func fileKind(name string) string {
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(name), "."))
	if ext == "" {
		return "FILE"
	}
	if len(ext) > 4 {
		ext = ext[:4]
	}
	return ext
}

func optionalID(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}
