package app

// The variance grid, monthly plans, the budgets screen, and locking or
// unlocking a month.

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/money"
	"fervidbudget/internal/store"
)

func (a *App) grid(w http.ResponseWriter, r *http.Request) {
	month := validMonthOrCurrent(r.URL.Query().Get("month"))
	status := r.URL.Query().Get("status")
	q := r.URL.Query().Get("q")
	grid, err := a.st.Grid(r.Context(), month, status, q)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	closeGrid, err := a.st.Grid(r.Context(), month, "", "")
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// grid:view opens the budget matrix. The Recent Payments panel is a second
	// question: it carries amounts, payees and live /payments/{id} links, so it
	// is gated on payment:view separately (F-A-02/F-G-032) and the caller who
	// lacks that verb is simply handed no rows. The gate is here rather than in
	// the template because a screen must not be trusted to hide data the handler
	// loaded — /payments answers 403 to exactly this caller.
	u := auth.CurrentUser(r)
	var payments []store.Payment
	if a.auth.Can(u, "payment", "view") {
		// ExcludeRecoverable: a recoverable is a deposit, not budget spend. The
		// totals above already leave it out (V2), and so must this panel, or the
		// grid lists a payment its own figures do not count (recoverables-2).
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

func (a *App) months(w http.ResponseWriter, r *http.Request) {
	plans, err := a.st.ListMonthPlans(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	target := defaultTargetMonth(plans)
	source := previousMonth(target)
	a.render(w, r, "months", PageData{Title: "Monthly Plans", MonthPlans: plans, TargetMonth: target, SourceMonth: source})
}

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
		plans, listErr := a.st.ListMonthPlans(r.Context())
		if listErr != nil {
			a.respondStoreError(w, r, listErr)
			return
		}
		// The status is the store's, not a hardcoded 400: a locked month is a 409
		// everywhere else and must be a 409 here too (F-G-026).
		a.renderStatus(w, r, reRenderStatus(err), "months", PageData{Title: "Monthly Plans", MonthPlans: plans, TargetMonth: target, SourceMonth: source, Error: friendly(err)})
		return
	}
	http.Redirect(w, r, "/budgets?month="+target, http.StatusSeeOther)
}

func (a *App) budgets(w http.ResponseWriter, r *http.Request) {
	month := validMonthOrCurrent(r.URL.Query().Get("month"))
	grid, err := a.st.Grid(r.Context(), month, "", "")
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	heads, err := a.st.ListHeads(r.Context(), true)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	lineHeads, err := a.st.DetailedBudgetHeads(r.Context(), month)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "budgets", PageData{Title: "Budgets", Month: month, Grid: grid, Heads: heads, BudgetLineHeads: lineHeads})
}

func (a *App) budgetSave(w http.ResponseWriter, r *http.Request) {
	month := strings.TrimSpace(r.FormValue("month"))
	if !validMonthInput(month) {
		a.respondError(w, r, http.StatusBadRequest, "A valid month is required.", nil)
		return
	}
	u := auth.CurrentUser(r)
	inputs := make(map[int64]string)
	fieldErrors := make(map[int64]string)
	var updates []store.BudgetInput
	// One unreadable field is one unreadable field. The whole batch used to be
	// abandoned for any single failure, and because the screen renders ₹0.00 for
	// every unbudgeted head — a value ParsePaise refuses — a month that had never
	// been budgeted could not be budgeted at all (F-G-028). Every field that
	// parses is now applied; the ones that do not are named back to the operator.
	for k, v := range r.Form {
		if !strings.HasPrefix(k, "budget_") {
			continue
		}
		headID, idErr := strconv.ParseInt(strings.TrimPrefix(k, "budget_"), 10, 64)
		raw := ""
		if len(v) > 0 {
			raw = v[0]
		}
		if idErr != nil || headID <= 0 {
			continue
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
		saveErr = a.st.SetBudgets(r.Context(), u, month, updates)
	}
	if saveErr != nil || len(fieldErrors) > 0 {
		grid, gridErr := a.st.Grid(r.Context(), month, "", "")
		heads, headsErr := a.st.ListHeads(r.Context(), true)
		if gridErr != nil || headsErr != nil {
			a.respondStoreError(w, r, errors.Join(gridErr, headsErr))
			return
		}
		lineHeads, lineErr := a.st.DetailedBudgetHeads(r.Context(), month)
		if lineErr != nil {
			a.respondStoreError(w, r, lineErr)
			return
		}
		message := friendly(saveErr)
		if saveErr == nil {
			// The wording keeps "invalid budget amount" from the message this
			// replaces, because that phrase is what the reader — and the shipped
			// regression guard — recognises. What it no longer says is that
			// everything was refused, because it no longer is.
			message = fmt.Sprintf("%d budget %s saved. %d invalid budget %s not saved; correct the marked fields below.",
				len(updates), plural(len(updates), "amount", "amounts"), len(fieldErrors), plural(len(fieldErrors), "amount was", "amounts were"))
		}
		// reRenderStatus keeps a locked month a 409 here too (F-G-026).
		a.renderStatus(w, r, reRenderStatus(saveErr), "budgets", PageData{Title: "Budgets", Month: month, Grid: grid, Heads: heads, BudgetInputs: inputs, BudgetErrors: fieldErrors, BudgetSaved: len(updates), BudgetSaveFailed: saveErr != nil, Error: message, BudgetLineHeads: lineHeads})
		return
	}
	http.Redirect(w, r, "/budgets?month="+month, http.StatusSeeOther)
}

func budgetInputValue(inputs map[int64]string, headID, amount int64) string {
	if raw, submitted := inputs[headID]; submitted {
		return raw
	}
	return money.FormatPaise(amount)
}

// parseBudgetPaise reads a budget figure. A budget of zero is a real and
// meaningful number — "this head has no budget this month" is the state the
// screen renders by default, as ₹0.00 — so an empty or zero field is accepted
// and money.ParsePaise, which rightly refuses a non-positive *payment*, is not
// the parser for it. A negative budget is still refused; SetBudgets refuses it
// again.
func parseBudgetPaise(raw string) (int64, error) {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "₹"))
	if s == "" || s == "0" || s == "0.0" || s == "0.00" {
		return 0, nil
	}
	return money.ParsePaise(raw)
}

// reRenderStatus is the status a handler that re-renders its own screen answers
// with. Routing through storeErrorStatus is what keeps one condition to one
// code: a locked month was a 409 through respondStoreError and a 400 on the two
// screens that draw their own error (F-G-026).
func reRenderStatus(err error) int {
	if err == nil {
		return http.StatusBadRequest
	}
	return storeErrorStatus(err)
}

// lockMonth and unlockMonth land on /grid?month=, which is the one screen that
// renders the lock and its reason. They used to redirect to /?month=, which was
// the variance grid before Phase 4 made / the dashboard — so an operator locked
// a month and was dropped on a page that read the month parameter not at all and
// confirmed nothing (F-G-019, and the same root cause as F-G-029).
func (a *App) lockMonth(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	if err := a.st.LockMonth(r.Context(), auth.CurrentUser(r), month, r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/grid?month="+month, http.StatusSeeOther)
}

func (a *App) unlockMonth(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	if err := a.st.UnlockMonth(r.Context(), auth.CurrentUser(r), month, r.FormValue("reason")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/grid?month="+month, http.StatusSeeOther)
}

func defaultTargetMonth(plans []store.MonthPlan) string {
	now := time.Now().Format("2006-01")
	if len(plans) == 0 || plans[0].Month < now {
		return now
	}
	return shiftMonth(plans[0].Month, 1)
}
