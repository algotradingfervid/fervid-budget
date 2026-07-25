package app

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/money"
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
		data.Request2.RecoverableCategory = "employee_advance"
	}
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
		data.Request2.RecoverableCategory = normalizeRecoverableCategory(q.Get("recoverable_category"), data.FormType)
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
// rendered <select> in step: a browser posting nothing still shows the first
// option, so an unrecognised value must resolve to whatever that would be.
func normalizeRecoverableCategory(code, formType string) string {
	if _, ok := recoverableCategoryLabels[code]; ok {
		return code
	}
	if formType == "employee_advance" {
		return "employee_advance"
	}
	return "emd"
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
	http.Redirect(w, r, fmt.Sprintf("/requests/%d/submitted", id), http.StatusSeeOther)
}

// renderRejectedRequestForm puts the form back with the message and everything
// the requester had typed still in it. Losing a page of typing to one bad field
// is the cruellest thing a form can do.
func (a *App) renderRejectedRequestForm(w http.ResponseWriter, r *http.Request, status int, in store.RequestInput, message string) {
	label, known := requestTypeLabels[in.Type]
	if !known {
		a.respondError(w, r, http.StatusBadRequest, "That is not a kind of request this system raises.", nil)
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
	a.render(w, r, "request_submitted", PageData{Title: "Request submitted", Request2: req})
}

// loadViewableRequest resolves the {id} in the path and refuses it to anybody
// whose data scope does not reach it — holding request:view somewhere is not
// the same as being allowed to read this one (Q5/R6).
func (a *App) loadViewableRequest(w http.ResponseWriter, r *http.Request) (store.Request, bool) {
	u := auth.CurrentUser(r)
	req, err := a.st.Request(r.Context(), pathID(r))
	if err != nil {
		a.respondStoreError(w, r, err)
		return store.Request{}, false
	}
	if !canViewRequest(a.auth.Scope(u, "request"), u, req) {
		a.respondError(w, r, http.StatusForbidden, "You do not have permission to view this request.", nil)
		return store.Request{}, false
	}
	return req, true
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
	data := PageData{Title: title, Projects: projects, Heads: heads, Approvers: approvers, Settings: settings}
	// G9: 0 means "no default"; the control then opens on "Choose an approver".
	if u.DefaultApproverID > 0 {
		data.Request2.ManagerID = u.DefaultApproverID
	}
	return data, nil
}

func (a *App) requestsExport(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	q := r.URL.Query()
	list, err := a.st.ListRequests(r.Context(), store.RequestListOptions{
		Scope: a.effectiveScope(u, q.Get("scope")), ViewerID: u.ID,
		Status: q.Get("status"), Bucket: q.Get("bucket"), Type: q.Get("type"),
		Treatment: q.Get("treatment"), ProjectID: parseID(q.Get("project_id")), Query: q.Get("q")})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "export",
		EntityType: "payment_request", Summary: "Exported request list"})
	var body bytes.Buffer
	cw := csv.NewWriter(&body)
	_ = cw.Write([]string{"Number", "Status", "Type", "Title", "Amount", "Payee", "Requester", "Approver", "Created"})
	for _, req := range list {
		_ = cw.Write([]string{req.Number, requestStatusText(req.Status), typeLabel(req.Type), req.ShortTitle,
			money.FormatPaise(req.Amount), req.Vendor, req.RequesterName, req.ManagerName,
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

func optionalID(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}
