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

// requestTypeLabels is the vocabulary of request-new-type.html. It is the only
// place a request type is named for a human, and its keys are the only types
// the chooser and the form will render.
var requestTypeLabels = map[string]string{
	"vendor_invoice":   "Vendor invoice payment",
	"vendor_advance":   "Vendor advance",
	"reimbursement":    "Reimbursement",
	"employee_advance": "Employee advance",
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
