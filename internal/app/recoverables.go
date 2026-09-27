package app

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/money"
	"fervidbudget/internal/store"
)

// The recoverables register: a dashboard of what is outstanding, the full list
// behind it, a CSV export and one detail screen per recoverable.
//
// A recoverable is money paid out that is expected back (a deposit, a
// guarantee, an employee advance). Every read is limited to the requests the
// reader's data scope covers. Ageing ("25 days overdue") is computed by the
// store from an explicit AsOf date; time.Now() is taken here, in the handlers,
// so tests can fix the clock.

// recoverablesDashboard shows totals and ageing of money still to come back,
// with roll-ups by category and by counterparty. The totals cover only the
// requests in the reader's scope.
func (a *App) recoverablesDashboard(w http.ResponseWriter, r *http.Request) {
	asOf := time.Now()
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
	// Each category row links to the list filtered by ?category={id}. The
	// roll-up only carries the category name, so names are mapped to ids here;
	// "Uncategorised" matches no category and gets no link.
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

// recoverableListOptions reads the list filters from the URL, for both the list
// screen and its CSV export, so the file always matches the screen. Unknown
// ageing or order values are ignored. With no from/to range the list shows
// every live recoverable, including approved ones not yet paid.
func (a *App) recoverableListOptions(r *http.Request) store.RecoverableReportOptions {
	q := r.URL.Query()
	ageing := q.Get("ageing")
	switch ageing {
	case "outstanding", "overdue", "due30", "later", "unpaid", "recovered": // allow-list; anything else means "all"
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
		AsOf:         time.Now(),
		Viewer:       a.recoverableViewer(r),
	}
}

// recoverableViewer returns who is reading, so every recoverables query only
// returns rows that person could also open on /requests/{id}. The scope is
// applied inside the SQL (store's recoverableScope), which keeps the summary
// totals correct and any LIMIT on the query accurate.
func (a *App) recoverableViewer(r *http.Request) store.RecoverableViewer {
	u := auth.CurrentUser(r)
	return store.RecoverableViewer{Scope: a.auth.Scope(u, "request"), ViewerID: u.ID}
}

// recoverablesList shows the full recoverables list with its filters and the
// total amount outstanding.
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
		From: opts.From, To: opts.To, Query: opts.Query, RecoveryCounterparty: opts.Counterparty, RecoveryOrder: opts.Order,
	})
}

// exportRecoverable downloads the filtered recoverables list as
// recoverables.csv. The export is audited.
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
	_ = cw.Write([]string{"Number", "Category", "Counterparty", "Project", "Outstanding", "Paid out", "Recovered or reconciled", "Paid On", "Expected Return", "Ageing", "Status", "Requester", "Repayment Notes"})
	for _, row := range rows {
		_ = cw.Write([]string{csvText(row.Number), csvText(row.Category), csvText(row.Counterparty), csvText(row.Project), csvAmount(row.Amount), csvAmount(row.PaidAmount), csvAmount(row.RecoveredAmount),
			row.PaidOn, row.ExpectedReturnDate, row.AgeingLabel, csvText(row.Status), csvText(row.Requester), csvText(row.RepaymentNotes)})
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

// recoverableDetail shows one recoverable.
func (a *App) recoverableDetail(w http.ResponseWriter, r *http.Request) {
	a.renderRecoverableDetail(w, r, store.RecoveryInput{}, "", http.StatusOK)
}

// recordRecovery records money coming back (or otherwise reconciled) against a
// recoverable. A refused entry shows the detail page again with the message
// and status 422; success returns to the history section with a notice.
func (a *App) recordRecovery(w http.ResponseWriter, r *http.Request) {
	in := store.RecoveryInput{Kind: r.FormValue("kind"), AmountText: r.FormValue("amount"), OccurredOn: r.FormValue("occurred_on"), Reference: r.FormValue("reference"), Note: r.FormValue("note"), Token: r.FormValue("token")}
	id := pathID(r)
	amount, err := money.ParsePaise(in.AmountText)
	if err != nil {
		err = fmt.Errorf("%w: enter a positive recovery amount with up to two decimal places", store.ErrValidation)
	} else {
		in.Amount = amount
		_, err = a.st.RecordRecovery(r.Context(), auth.CurrentUser(r), id, in)
	}
	switch {
	case errors.Is(err, store.ErrValidation):
		message := strings.TrimPrefix(err.Error(), store.ErrValidation.Error()+": ")
		a.renderRecoverableDetail(w, r, in, message, http.StatusUnprocessableEntity)
	case err != nil:
		a.respondStoreError(w, r, err)
	default:
		http.Redirect(w, r, fmt.Sprintf("/recoverables/%d?saved=1#recovery-history", id), http.StatusSeeOther)
	}
}

// renderRecoverableDetail draws one recoverable: its register row, payment,
// history, recoveries so far and the "record a recovery" form (pre-filled with
// input and showing recoveryError, if any). Requests outside the reader's
// scope, and requests that are not recoverable, answer 404.
func (a *App) renderRecoverableDetail(w http.ResponseWriter, r *http.Request, input store.RecoveryInput, recoveryError string, status int) {
	req, ok := a.loadViewableRequest(w, r)
	if !ok {
		return
	}
	u := auth.CurrentUser(r)
	if req.Treatment != "recoverable" {
		a.respondError(w, r, http.StatusNotFound, "The requested record was not found.", nil)
		return
	}
	// Use the list's own query (searching by the unique number) so the ageing
	// here matches the list.
	rows, err := a.st.RecoverableReport(r.Context(), store.RecoverableReportOptions{
		Query: req.Number, AsOf: time.Now(), Viewer: a.recoverableViewer(r)})
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
	// The same history thread as the request page.
	thread, err := a.st.RequestThread(r.Context(), req.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	events, err := a.st.RecoveryEvents(r.Context(), u, req.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if input.Token == "" {
		token, err := newFormToken()
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		input.Token = token
	}
	if input.OccurredOn == "" {
		input.OccurredOn = time.Now().Format("2006-01-02")
	}
	notice := ""
	if r.URL.Query().Get("saved") == "1" {
		notice = "Recovery recorded. The outstanding balance and dated history have been updated."
	}
	a.renderStatus(w, r, status, "recoverable_detail", PageData{
		RecoveryEvents: events, RecoveryInput: input, RecoveryError: recoveryError, CanRecordRecovery: a.auth.Can(u, "payment", "create") && row.Amount > 0, Notice: notice,
		Title: "Recoverable " + req.Number, Request2: req, Recoverable: row,
		RecPayment: pay, HasPayment: payErr == nil, Thread: thread,
	})
}
