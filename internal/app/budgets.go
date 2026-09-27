package app

// The variance grid, monthly plans, the budgets screen, and locking or
// unlocking a month.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/money"
	"fervidbudget/internal/store"
)

// grid shows the variance grid: budget against actual spend for every head in
// a month, filtered by status and search text, plus a "Recent payments" panel.
func (a *App) grid(w http.ResponseWriter, r *http.Request) {
	month := validMonthOrCurrent(r.URL.Query().Get("month"))
	status := r.URL.Query().Get("status")
	q := r.URL.Query().Get("q")
	grid, err := a.st.Grid(r.Context(), month, status, q)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// The full-month totals and the month-close panel always use the whole
	// month, whatever the filters.
	closeGrid, err := a.st.Grid(r.Context(), month, "", "")
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// The payments panel shows amounts, payees and links to payments, so it is
	// loaded only for a reader with payment:view (grid:view alone is not
	// enough). Deciding here rather than in the template means the data is
	// never loaded for someone who may not see it.
	u := auth.CurrentUser(r)
	var payments []store.Payment
	if a.auth.Can(u, "payment", "view") {
		// Recoverables are deposits, not budget spend: the grid's totals leave
		// them out, so this panel does too.
		payments, err = a.st.ListPayments(r.Context(), store.PaymentListOptions{
			Month: month, Status: "active", Limit: 10,
			Scope: a.auth.Scope(u, "payment"), ViewerID: u.ID,
			ExcludeRecoverable: true,
		})
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
	// The filters swap the grid in over htmx and the same URL serves the full
	// page, so a cache must key on the header that tells them apart.
	w.Header().Add("Vary", "HX-Request")
	a.render(w, r, "grid", PageData{Title: "Variance Grid", Grid: grid, CloseGrid: closeGrid, Month: month, Status: status, Query: q, Payments: payments})
}

// monthsPageData is the monthly plans screen: every plan, newest first.
func (a *App) monthsPageData(ctx context.Context) (PageData, error) {
	plans, err := a.st.ListMonthPlans(ctx)
	if err != nil {
		return PageData{}, err
	}
	return PageData{Title: "Monthly Plans", MonthPlans: plans}, nil
}

// months shows the monthly plans screen, with the "new month" form
// suggesting the month after the newest plan, copied from the month before.
func (a *App) months(w http.ResponseWriter, r *http.Request) {
	data, err := a.monthsPageData(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data.TargetMonth = defaultTargetMonth(data.MonthPlans)
	data.SourceMonth = previousMonth(data.TargetMonth)
	a.render(w, r, "months", data)
}

// monthCreate opens a new month plan, either empty or copied from a source
// month. Copying budgets needs budget view and edit as well as month:create.
// A refusal shows the screen again with the error and what was typed.
func (a *App) monthCreate(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.FormValue("target_month"))
	source := ""
	if r.FormValue("source_mode") == "copy" {
		perms := a.auth.Permissions(auth.CurrentUser(r))
		if !perms.Can("budget", "view") || !perms.Can("budget", "edit") {
			a.respondStoreError(w, r, store.ErrForbidden)
			return
		}
		source = strings.TrimSpace(r.FormValue("source_month"))
		if source == "" {
			a.respondError(w, r, http.StatusBadRequest, "Choose a source month to copy.", nil)
			return
		}
	}
	if err := a.st.CreateMonthPlan(r.Context(), auth.CurrentUser(r), target, source); err != nil {
		data, listErr := a.monthsPageData(r.Context())
		if listErr != nil {
			a.respondStoreError(w, r, listErr)
			return
		}
		data.TargetMonth, data.SourceMonth, data.Error = target, source, friendly(err)
		a.renderStatus(w, r, reRenderStatus(err), "months", data)
		return
	}
	http.Redirect(w, r, "/budgets?month="+target, http.StatusSeeOther)
}

// budgetsPageData is the budgets screen for one month: the grid, every
// active head, and which heads have line-item (detailed) budgets.
func (a *App) budgetsPageData(ctx context.Context, month string) (PageData, error) {
	grid, gridErr := a.st.Grid(ctx, month, "", "")
	heads, headsErr := a.st.ListHeads(ctx, true)
	if gridErr != nil || headsErr != nil {
		return PageData{}, errors.Join(gridErr, headsErr)
	}
	lineHeads, err := a.st.DetailedBudgetHeads(ctx, month)
	if err != nil {
		return PageData{}, err
	}
	return PageData{Title: "Budgets", Month: month, Grid: grid, Heads: heads, BudgetLineHeads: lineHeads}, nil
}

// budgets shows the budgets screen for the month in ?month= (default: now).
func (a *App) budgets(w http.ResponseWriter, r *http.Request) {
	data, err := a.budgetsPageData(r.Context(), validMonthOrCurrent(r.URL.Query().Get("month")))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "budgets", data)
}

// budgetSave saves the budgets screen. Each "budget_<headID>" field is read
// on its own: every amount that parses is saved, and the ones that do not are
// marked on the screen with what was typed, so one typo does not throw away
// the rest of the month.
func (a *App) budgetSave(w http.ResponseWriter, r *http.Request) {
	month := strings.TrimSpace(r.FormValue("month"))
	if !validMonthInput(month) {
		a.respondError(w, r, http.StatusBadRequest, "A valid month is required.", nil)
		return
	}
	inputs := make(map[int64]string)
	fieldErrors := make(map[int64]string)
	var updates []store.BudgetInput
	for k, v := range r.Form {
		if !strings.HasPrefix(k, "budget_") {
			continue
		}
		headID, idErr := strconv.ParseInt(strings.TrimPrefix(k, "budget_"), 10, 64)
		if idErr != nil || headID <= 0 {
			continue
		}
		raw := ""
		if len(v) > 0 {
			raw = v[0]
		}
		inputs[headID] = raw
		amt, amountErr := parseBudgetPaise(raw)
		if amountErr != nil {
			fieldErrors[headID] = amountErr.Error()
			continue
		}
		updates = append(updates, store.BudgetInput{HeadID: headID, Amount: amt})
	}
	var saveErr error
	if len(updates) > 0 {
		saveErr = a.st.SetBudgets(r.Context(), auth.CurrentUser(r), month, updates)
	}
	if saveErr == nil && len(fieldErrors) == 0 {
		http.Redirect(w, r, "/budgets?month="+month, http.StatusSeeOther)
		return
	}
	data, err := a.budgetsPageData(r.Context(), month)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	data.Error = friendly(saveErr)
	if saveErr == nil {
		// The valid amounts were saved; say how many, and how many were not.
		data.Error = fmt.Sprintf("%d budget %s saved. %d invalid budget %s not saved; correct the marked fields below.",
			len(updates), plural(len(updates), "amount", "amounts"), len(fieldErrors), plural(len(fieldErrors), "amount was", "amounts were"))
	}
	data.BudgetInputs, data.BudgetErrors = inputs, fieldErrors
	data.BudgetSaved, data.BudgetSaveFailed = len(updates), saveErr != nil
	a.renderStatus(w, r, reRenderStatus(saveErr), "budgets", data)
}

// budgetInputValue is what a budget field shows: what the user typed, when
// the screen is shown again after a refused save, otherwise the saved amount.
func budgetInputValue(inputs map[int64]string, headID, amount int64) string {
	if raw, submitted := inputs[headID]; submitted {
		return raw
	}
	return money.FormatPaise(amount)
}

// parseBudgetPaise reads a budget amount. Unlike a payment, a budget may be
// zero (the screen shows ₹0.00 for a head with no budget), so an empty or
// zero field means 0. Anything else goes through money.ParsePaise, which also
// refuses a negative amount.
func parseBudgetPaise(raw string) (int64, error) {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "₹"))
	if s == "" || s == "0" || s == "0.0" || s == "0.00" {
		return 0, nil
	}
	return money.ParsePaise(raw)
}

// reRenderStatus is the status for a screen shown again with an error: the
// store error's own status (so a locked month is 409 here as everywhere
// else), or 400 when the only problem was the form's own validation.
func reRenderStatus(err error) int {
	if err == nil {
		return http.StatusBadRequest
	}
	return storeErrorStatus(err)
}

// setMonthLock returns the handler that locks or unlocks (depending on
// change) the month in the path, with the reason from the form, and then
// shows that month's grid, the screen that displays the lock.
func (a *App) setMonthLock(change func(ctx context.Context, u store.User, month, reason string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		month := r.PathValue("month")
		if err := change(r.Context(), auth.CurrentUser(r), month, r.FormValue("reason")); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		http.Redirect(w, r, "/grid?month="+month, http.StatusSeeOther)
	}
}

// defaultTargetMonth is the month the "new month" form suggests: the month
// after the newest plan, or the current month if that is later.
func defaultTargetMonth(plans []store.MonthPlan) string {
	now := currentMonth()
	if len(plans) == 0 || plans[0].Month < now {
		return now
	}
	return shiftMonth(plans[0].Month, 1)
}
