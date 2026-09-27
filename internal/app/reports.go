package app

// Budget-versus-actual reports and the grid and report CSV exports.

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/money"
	"fervidbudget/internal/store"
)

// report shows budget against actual for a range of months (from/to, or a
// single ?month=), grouped by the last path segment: monthly, projects or
// heads. It can be narrowed to one project or head (by id, since names can
// repeat or change); choosing a head always groups by head. When one head is
// chosen and the reader has payment:view, that head's payments in the range
// are listed below.
func (a *App) report(w http.ResponseWriter, r *http.Request) {
	from := validMonthOrCurrent(queryDefault(r, "from", r.URL.Query().Get("month")))
	to := validMonthOrFallback(r.URL.Query().Get("to"), from)
	if from > to {
		from, to = to, from
	}
	mode := strings.TrimPrefix(r.URL.Path, "/reports/")
	projectID, headID := parseID(r.URL.Query().Get("project_id")), parseID(r.URL.Query().Get("head_id"))
	if headID > 0 {
		mode = "heads"
	} else if projectID > 0 && mode == "monthly" {
		mode = "projects"
	}
	rows, err := a.st.Report(r.Context(), from, to, mode)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	// Preserve an explicitly selected project/head even in a period with no
	// activity, so date shortcuts retain a visible scope and budget link.
	if mode != "monthly" && projectID == 0 && headID == 0 {
		rows = nonEmptyReportRows(rows)
	}
	filtered := rows[:0]
	contextLabel := ""
	for _, row := range rows {
		if projectID > 0 && row.ProjectID != projectID {
			continue
		}
		if headID > 0 && row.HeadID != headID {
			continue
		}
		filtered = append(filtered, row)
		if projectID > 0 {
			contextLabel = row.Project
		}
		if headID > 0 {
			contextLabel = row.Project + " / " + row.Head
		}
	}
	rows = filtered
	// A retired head may have no grid row in the new month. Still name the
	// selected scope, rather than making an empty result appear unfiltered.
	if contextLabel == "" && headID > 0 {
		heads, err := a.st.ListHeads(r.Context(), false)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		for _, head := range heads {
			if head.ID == headID && (projectID == 0 || head.ProjectID == projectID) {
				contextLabel = head.Project + " / " + head.Name
				break
			}
		}
	} else if contextLabel == "" && projectID > 0 {
		projects, err := a.st.ListProjects(r.Context(), false)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		for _, project := range projects {
			if project.ID == projectID {
				contextLabel = project.Name
				break
			}
		}
	}
	back := safeReportBack(r.URL.Query().Get("back"))
	query := url.Values{"from": {from}, "to": {to}}
	if projectID > 0 {
		query.Set("project_id", strconv.FormatInt(projectID, 10))
	}
	if headID > 0 {
		query.Set("head_id", strconv.FormatInt(headID, 10))
	}
	if back != "" {
		query.Set("back", back)
	}
	data := PageData{Title: "Reports", Reports: rows, Summary: summarizeReports(rows), From: from, To: to, Mode: mode,
		ReportProjectID: projectID, ReportHeadID: headID, ReportBack: back,
		ReportURL: "/reports/" + mode + "?" + query.Encode(), ReportContext: contextLabel}
	u := auth.CurrentUser(r)
	// The aggregate report permission does not grant permission to individual payments.
	if headID > 0 && a.auth.Can(u, "payment", "view") {
		for _, row := range rows {
			payments, err := a.st.ListPayments(r.Context(), store.PaymentListOptions{Month: row.Period,
				HeadID: headID, ProjectID: projectID, Status: "active", ExcludeRecoverable: true,
				Scope: a.auth.Scope(u, "payment"), ViewerID: u.ID, Limit: 300})
			if err != nil {
				a.respondStoreError(w, r, err)
				return
			}
			data.Payments = append(data.Payments, payments...)
		}
	}
	a.render(w, r, "reports", data)
}

// safeReportBack keeps a "back" link only when it points at one of the three
// report pages on this site; anything else becomes "".
func safeReportBack(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" {
		return ""
	}
	switch u.Path {
	case "/reports/monthly", "/reports/projects", "/reports/heads":
		return u.RequestURI()
	}
	return ""
}

// reportDetailURL is the link from a report row to the next level down for
// that row's month: a project's heads, or one head.
func reportDetailURL(row store.ReportRow, back string) string {
	mode := "projects"
	q := url.Values{"from": {row.Period}, "to": {row.Period}}
	if row.ProjectID > 0 {
		mode = "heads"
		q.Set("project_id", strconv.FormatInt(row.ProjectID, 10))
	}
	if row.HeadID > 0 {
		q.Set("head_id", strconv.FormatInt(row.HeadID, 10))
	}
	if back = safeReportBack(back); back != "" {
		q.Set("back", back)
	}
	return "/reports/" + mode + "?" + q.Encode()
}

// exportGrid downloads the variance grid for a month (with the grid's
// status and search filters) as variance-YYYY-MM.csv. The export is audited.
func (a *App) exportGrid(w http.ResponseWriter, r *http.Request) {
	month := validMonthOrCurrent(r.URL.Query().Get("month"))
	grid, err := a.st.Grid(r.Context(), month, r.URL.Query().Get("status"), r.URL.Query().Get("q"))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	u := auth.CurrentUser(r)
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "export", EntityType: "report", Summary: "Exported grid " + month})
	rows := make([][]string, 0, len(grid.Rows))
	for _, row := range grid.Rows {
		rows = append(rows, []string{csvText(row.Project), csvText(row.Head), csvAmount(row.Budget), csvAmount(row.Actual), csvAmount(row.Variance), row.VariancePercent, csvText(row.Status)})
	}
	a.sendCSV(w, r, "variance-"+month+".csv",
		[]string{"Project", "Head", "Budget", "Actual", "Variance", "Variance %", "Status"}, rows)
}

// reportLevels are the three groupings store.Report knows, which are also the
// three /reports/* paths.
var reportLevels = map[string]bool{"monthly": true, "projects": true, "heads": true}

// exportYTD downloads a report as CSV for a month range (default: the whole
// of ?year=, or this year) at the ?level= of the tab it was pressed from
// (default "heads"). The export is audited.
func (a *App) exportYTD(w http.ResponseWriter, r *http.Request) {
	year := queryDefault(r, "year", time.Now().Format("2006"))
	from := validMonthOrFallback(r.URL.Query().Get("from"), year+"-01")
	to := validMonthOrFallback(r.URL.Query().Get("to"), year+"-12")
	if from > to {
		from, to = to, from
	}
	level := strings.TrimSpace(r.URL.Query().Get("level"))
	if !reportLevels[level] {
		level = "heads"
	}
	rows, err := a.st.Report(r.Context(), from, to, level)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	u := auth.CurrentUser(r)
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "export", EntityType: "report", Summary: "Exported " + level + " report " + from + " to " + to})
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells = append(cells, []string{csvText(row.Period), csvText(row.Project), csvText(row.Head), csvAmount(row.Budget), csvAmount(row.Actual), csvAmount(row.Variance), row.VariancePercent})
	}
	a.sendCSV(w, r, "report-"+from+"-to-"+to+".csv",
		[]string{"Period", "Project", "Head", "Budget", "Actual", "Variance", "Variance %"}, cells)
}

// nonEmptyReportRows drops rows with neither budget nor spend.
func nonEmptyReportRows(rows []store.ReportRow) []store.ReportRow {
	out := rows[:0]
	for _, row := range rows {
		if row.Budget != 0 || row.Actual != 0 {
			out = append(out, row)
		}
	}
	return out
}

// summarizeReports adds up the report rows into the totals row.
func summarizeReports(rows []store.ReportRow) ReportSummary {
	var s ReportSummary
	s.Rows = len(rows)
	for _, row := range rows {
		s.Budget += row.Budget
		s.Actual += row.Actual
		switch statusForView(row.Budget, row.Actual) {
		case "over":
			s.Over++
		case "under":
			s.Under++
		case "on-track":
			s.OnTrack++
		case "not-paid":
			s.NotPaid++
		}
	}
	s.Variance = s.Budget - s.Actual
	s.VariancePercent = money.Percent(s.Variance, s.Budget)
	s.UsedPercent = usedText(s.Budget, s.Actual)
	return s
}
