package app

import (
	"bytes"
	"encoding/csv"
	"errors"
	"net/http"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/money"
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
	metrics, err := a.st.RecoverableMetrics(r.Context(), asOf)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	byCat, err := a.st.RecoverableRollups(r.Context(), "category", asOf)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	byCp, err := a.st.RecoverableRollups(r.Context(), "counterparty", asOf)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "recoverables_dashboard", PageData{
		Title: "Recoverable payments", RecMetrics: metrics, ByCategory: byCat, ByCounterparty: byCp,
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
	}
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
		_ = cw.Write([]string{row.Number, row.Category, row.Counterparty, row.Project, money.FormatPaise(row.Amount),
			row.PaidOn, row.ExpectedReturnDate, row.AgeingLabel, row.Status, row.Requester, row.RepaymentNotes})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The export could not be generated.", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
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
	// Reuse the register query so ageing here and in the list can never
	// disagree; the number is unique, so it returns exactly this row.
	rows, err := a.st.RecoverableReport(r.Context(), store.RecoverableReportOptions{Query: req.Number, AsOf: time.Now().UTC()})
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
