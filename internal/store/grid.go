// grid.go builds the budget-versus-actual grid for a month and the budget
// report over a range of months.

package store

import (
	"context"
	"fervidbudget/internal/money"
	"fmt"
	"strings"
	"time"
)

// Grid builds the budget-versus-actual table for one month ("YYYY-MM"; an
// invalid month means this month): one row per head, grouped by project, with
// totals. Inactive heads appear only when they have a budget or spend that
// month. status and q (a project/head name search) filter the rows and the
// totals. Payments that settle recoverable requests never count as spend.
func (s *Store) Grid(ctx context.Context, month, status, q string) (GridData, error) {
	if !validMonth(month) {
		month = time.Now().Format("2006-01")
	}
	// A recoverable payment is a deposit, not an expense: it never counts as
	// budget actuals. The COALESCE keeps payments with no linked request
	// counting as spend.
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.name,h.id,h.name,h.active,p.active,COALESCE(h.due_day,''),COALESCE(b.amount,0),
		COALESCE(SUM(CASE WHEN py.voided_at IS NULL AND COALESCE(pr.treatment,'') <> 'recoverable' THEN py.amount ELSE 0 END),0)
		FROM heads h JOIN projects p ON p.id=h.project_id
		LEFT JOIN budgets b ON b.head_id=h.id AND b.month=?
		LEFT JOIN payments py ON py.head_id=h.id AND substr(py.paid_on,1,7)=?
		LEFT JOIN payment_requests pr ON pr.id=py.request_id
		WHERE (h.active=1 AND p.active=1)
			OR b.id IS NOT NULL
			OR EXISTS(SELECT 1 FROM payments px
				LEFT JOIN payment_requests pxr ON pxr.id=px.request_id
				WHERE px.head_id=h.id AND substr(px.paid_on,1,7)=?
					AND COALESCE(pxr.treatment,'') <> 'recoverable')
		GROUP BY p.id,p.name,h.id,h.name,h.active,p.active,h.due_day,b.amount
		ORDER BY p.sort_order,p.name,h.sort_order,h.name`, month, month, month)
	if err != nil {
		return GridData{}, err
	}
	defer rows.Close()
	g := GridData{Month: month, Filtered: strings.TrimSpace(q) != "" || (status != "" && status != "all"), ScopeLabel: "Company total"}
	if g.Filtered {
		g.ScopeLabel = "Filtered subtotal"
	}
	if lock, err := s.MonthLock(ctx, month); err == nil {
		g.Locked = true
		g.Lock = lock
	}
	groupAt := map[int64]int{} // project id -> index in g.Groups
	q = strings.ToLower(strings.TrimSpace(q))
	for rows.Next() {
		var r GridRow
		var headActive, projectActive int
		if err := rows.Scan(&r.ProjectID, &r.Project, &r.HeadID, &r.Head, &headActive, &projectActive, &r.DueDay, &r.Budget, &r.Actual); err != nil {
			return g, err
		}
		r.Active = headActive == 1 && projectActive == 1
		r.Variance = r.Budget - r.Actual
		r.VariancePercent = money.Percent(r.Variance, r.Budget)
		r.Status = statusFor(r.Budget, r.Actual)
		if status != "" && status != "all" && r.Status != status {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.Project+" "+r.Head), q) {
			continue
		}
		g.Rows = append(g.Rows, r)
		switch r.Status {
		case "over":
			g.Over++
		case "unbudgeted":
			g.Unbudgeted++
		case "under":
			g.Under++
		case "on-track":
			g.OnTrack++
		case "not-paid":
			g.NotPaid++
		case "no-activity":
			g.NoActivity++
		}
		i, ok := groupAt[r.ProjectID]
		if !ok {
			i = len(g.Groups)
			groupAt[r.ProjectID] = i
			g.Groups = append(g.Groups, GridGroup{ProjectID: r.ProjectID, Project: r.Project, Total: ProjectTotal{ProjectID: r.ProjectID, Project: r.Project}})
		}
		g.Groups[i].Rows = append(g.Groups[i].Rows, r)
		g.Groups[i].Total.add(r.Budget, r.Actual)
		g.Total.add(r.Budget, r.Actual)
	}
	// Projects lists each group's total, in the same order, for the report.
	for _, grp := range g.Groups {
		g.Projects = append(g.Projects, grp.Total)
	}
	g.Total.Project = g.ScopeLabel
	g.Total.add(0, 0) // fills Variance for an empty grid too
	return g, rows.Err()
}

// Report returns the budget report from fromMonth to toMonth (at most 120
// months). mode "projects" gives one row per project per month, "monthly" one
// row per month, and anything else one row per head per month.
func (s *Store) Report(ctx context.Context, fromMonth, toMonth, mode string) ([]ReportRow, error) {
	if !validMonth(fromMonth) {
		fromMonth = time.Now().Format("2006-01")
	}
	if !validMonth(toMonth) {
		toMonth = fromMonth
	}
	months, err := monthRange(fromMonth, toMonth)
	if err != nil {
		return nil, err
	}
	var out []ReportRow
	for _, month := range months {
		grid, err := s.Grid(ctx, month, "", "")
		if err != nil {
			return nil, err
		}
		switch mode {
		case "projects":
			for _, pt := range grid.Projects {
				out = append(out, ReportRow{Period: month, ProjectID: pt.ProjectID, Project: pt.Project, Budget: pt.Budget, Actual: pt.Actual, Variance: pt.Variance, VariancePercent: pt.VariancePercent})
			}
		case "monthly":
			out = append(out, ReportRow{Period: month, Budget: grid.Total.Budget, Actual: grid.Total.Actual, Variance: grid.Total.Variance, VariancePercent: grid.Total.VariancePercent})
		default:
			for _, row := range grid.Rows {
				out = append(out, ReportRow{Period: month, ProjectID: row.ProjectID, HeadID: row.HeadID, Project: row.Project, Head: row.Head, Budget: row.Budget, Actual: row.Actual, Variance: row.Variance, VariancePercent: row.VariancePercent})
			}
		}
	}
	return out, nil
}

// monthRange lists the months from one "YYYY-MM" to another, inclusive, in
// either order.
func monthRange(from, to string) ([]string, error) {
	start, err := time.Parse("2006-01", from)
	if err != nil {
		return nil, err
	}
	end, err := time.Parse("2006-01", to)
	if err != nil {
		return nil, err
	}
	if end.Before(start) {
		start, end = end, start
	}
	if (end.Year()-start.Year())*12+int(end.Month()-start.Month()) >= 120 {
		return nil, fmt.Errorf("%w: report range must be 120 months or fewer", ErrValidation)
	}
	var months []string
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 1, 0) {
		months = append(months, cur.Format("2006-01"))
	}
	return months, nil
}

// add counts one more row's budget and actual into the total and keeps the
// variance in step.
func (t *ProjectTotal) add(budget, actual int64) {
	t.Budget += budget
	t.Actual += actual
	t.Variance = t.Budget - t.Actual
	t.VariancePercent = money.Percent(t.Variance, t.Budget)
}

// statusFor names a grid row's state from its budget and actual.
func statusFor(budget, actual int64) string {
	switch {
	case budget == 0 && actual == 0:
		return "no-activity"
	case actual == 0:
		return "not-paid"
	case budget == 0 && actual > 0:
		return "unbudgeted"
	case budget > 0 && actual > budget:
		return "over"
	case budget > 0 && actual == budget:
		return "on-track"
	default:
		return "under"
	}
}
