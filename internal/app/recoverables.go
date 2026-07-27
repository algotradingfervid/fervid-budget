package app

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// The recoverables register: a dashboard of what is outstanding, the full list
// behind it, a CSV export and one detail screen per recoverable.
//
// Every ageing number on these screens comes from the store with an explicit
// AsOf. time.Now() is called here, at the HTTP boundary, and nowhere below it,
// so the same request always produces the same ageing and the tests can pin
// "25 days overdue" to a fixed clock.

func (a *App) recoverablesDashboard(w http.ResponseWriter, r *http.Request) {
	asOf := time.Now().UTC()
	// The summary is scoped for the same reason the register is (F-G-016/F-E-03).
	// Wave 4 scoped the rows and left these three aggregates company-wide, which
	// left the disclosure half-closed: the counterparty rollup names counterparties
	// and the tiles total their money, so a reader restricted to their own rows
	// still learned both from the screen the register's own tabs link to. Summaries
	// over a scoped table need the scope too.
	viewer := a.recoverableViewer(r)
	metrics, err := a.st.RecoverableMetrics(r.Context(), asOf, viewer)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	byCat, err := a.st.RecoverableRollups(r.Context(), "category", asOf, viewer)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	byCp, err := a.st.RecoverableRollups(r.Context(), "counterparty", asOf, viewer)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// The by-category drill-through used to link to ?q={label}, and the label
	// appears in none of the columns the free-text search covers — so clicking a
	// row that said "1" landed on a different number (F-G-012). ?category={id} is
	// the control the list actually honours, and RecoverableRollup carries no id,
	// so the label is mapped back to one here. The rollup's own label is the
	// category name, or "Uncategorised" for a row with no category, which matches
	// nothing and correctly keeps its link off.
	cats, err := a.st.ListRecoverableCategories(r.Context(), false)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	ids := make(map[string]int64, len(cats))
	for _, c := range cats {
		ids[c.Name] = c.ID
	}
	a.render(w, r, "recoverables_dashboard", PageData{
		Title: "Recoverable payments", RecMetrics: metrics, ByCategory: byCat, ByCounterparty: byCp,
		CategoryIDs: ids,
	})
}

// recoverableListOptions builds the shared query options for the list screen and
// its CSV export. From/To stay optional: with no range the register shows every
// live recoverable, including ones approved but not yet paid.
func (a *App) recoverableListOptions(r *http.Request) store.RecoverableReportOptions {
	q := r.URL.Query()
	ageing := q.Get("ageing")
	switch ageing {
	case "overdue", "due30", "later", "unpaid": // allow-list; anything else means "all"
	default:
		ageing = ""
	}
	order := q.Get("order")
	switch order {
	case "amount", "paid_on", "number":
	default:
		order = ""
	}
	return store.RecoverableReportOptions{
		From:         q.Get("from"),
		To:           q.Get("to"),
		CategoryID:   parseID(q.Get("category")),
		Counterparty: q.Get("counterparty"),
		Query:        q.Get("q"),
		Ageing:       ageing,
		Order:        order,
		AsOf:         time.Now().UTC(),
		// Set here, at the one place the list and its CSV both pass through, so the
		// download cannot drift from the screen the way F-G-016 found it had.
		Viewer: a.recoverableViewer(r),
	}
}

// recoverableViewer names the caller every recoverable read is answered for.
//
// F-G-016/F-E-03: the register is a second view over `payment_requests` and it
// never asked who was reading it. `recoverable_report:view` alone returned every
// category, counterparty, project, amount, requester and repayment note in the
// company — including rows the very same caller is refused on `/requests/{id}`.
// R3 requires a data scope per resource and R6 requires it enforced server-side.
//
// This replaced a handler-side filter that re-read each row's request to ask
// `canViewRequest`. The predicate now lives in the SQL (`recoverableScope`), for
// two reasons beyond the N+1: the aggregates on the summary screen cannot be
// filtered row-by-row after the fact at all, and a filter applied after the query
// silently breaks any LIMIT the query grows later.
//
// Nothing is exposed under the shipped roles — Accounts and Admin both hold
// `request=all`, which is exactly why this survived the original build unnoticed.
func (a *App) recoverableViewer(r *http.Request) store.RecoverableViewer {
	u := auth.CurrentUser(r)
	return store.RecoverableViewer{Scope: a.auth.Scope(u, "request"), ViewerID: u.ID}
}

func (a *App) recoverablesList(w http.ResponseWriter, r *http.Request) {
	opts := a.recoverableListOptions(r)
	rows, err := a.st.RecoverableReport(r.Context(), opts)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	cats, err := a.st.ListRecoverableCategories(r.Context(), false)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	var total int64
	for _, row := range rows {
		total += row.Amount
	}
	a.render(w, r, "recoverables_list", PageData{
		Title: "All recoverables", Recoverables: rows, RecoverableTotal: total,
		Categories: cats, CategoryID: opts.CategoryID, Ageing: opts.Ageing,
		From: opts.From, To: opts.To, Query: opts.Query,
	})
}

func (a *App) exportRecoverable(w http.ResponseWriter, r *http.Request) {
	opts := a.recoverableListOptions(r)
	rows, err := a.st.RecoverableReport(r.Context(), opts)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// The download obeys the same row scope the screen does, because both build
	// their options through recoverableListOptions (F-G-016/F-E-03).
	u := auth.CurrentUser(r)
	scope := "all live recoverables"
	if opts.From != "" || opts.To != "" {
		scope = opts.From + " to " + opts.To
	}
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "export",
		EntityType: "recoverable_report", Summary: "Exported recoverable payments · " + scope})
	var body bytes.Buffer
	cw := csv.NewWriter(&body)
	_ = cw.Write([]string{"Number", "Category", "Counterparty", "Project", "Amount", "Paid On", "Expected Return", "Ageing", "Status", "Requester", "Repayment Notes"})
	for _, row := range rows {
		_ = cw.Write([]string{row.Number, row.Category, row.Counterparty, row.Project, csvAmount(row.Amount),
			row.PaidOn, row.ExpectedReturnDate, row.AgeingLabel, row.Status, row.Requester, row.RepaymentNotes})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The export could not be generated.", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="recoverables.csv"`)
	if _, err := w.Write(body.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "csv response write failed", "request_id", requestID(r), "error", err)
	}
}

func (a *App) recoverableDetail(w http.ResponseWriter, r *http.Request) {
	id := parseID(r.PathValue("id"))
	req, err := a.st.Request(r.Context(), id)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// This screen is the recoverables register, not a general request viewer.
	if req.Treatment != "recoverable" {
		a.respondError(w, r, http.StatusNotFound, "That request is not a recoverable payment.", nil)
		return
	}
	// The same row scope /requests/{id} applies, answered the same way it answers
	// it — 404, so the register is not an existence oracle either (F-G-016/F-G-002).
	u := auth.CurrentUser(r)
	if !canViewRequest(a.auth.Scope(u, "request"), u, req) {
		a.respondError(w, r, http.StatusNotFound, "The requested record was not found.",
			fmt.Errorf("recoverable %d is outside the caller's data scope", req.ID))
		return
	}
	// Reuse the register query so ageing here and in the list can never
	// disagree; the number is unique, so it returns exactly this row.
	rows, err := a.st.RecoverableReport(r.Context(), store.RecoverableReportOptions{
		Query: req.Number, AsOf: time.Now().UTC(), Viewer: a.recoverableViewer(r)})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	var row store.RecoverableRow
	for _, candidate := range rows {
		if candidate.RequestID == req.ID {
			row = candidate
		}
	}
	pay, payErr := a.st.PaymentForRequest(r.Context(), req.ID)
	if payErr != nil && !errors.Is(payErr, store.ErrNotFound) {
		a.respondStoreError(w, r, payErr)
		return
	}
	// The plan asked for RequestComments + Audit rendered as two loops. Phase 2
	// already merges both into one chronological stream, and using it keeps this
	// screen's history identical to the request's own rather than a second,
	// subtly different rendering of the same events.
	thread, err := a.st.RequestThread(r.Context(), req.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "recoverable_detail", PageData{
		Title: "Recoverable " + req.Number, Request2: req, Recoverable: row,
		RecPayment: pay, HasPayment: payErr == nil, Thread: thread,
	})
}
