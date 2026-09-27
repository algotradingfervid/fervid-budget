package app

// Budget-versus-actual reports and the grid and report CSV exports.

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/money"
	"fervidbudget/internal/store"
)

func (a *App) report(w http.ResponseWriter, r *http.Request) {
	from := validMonthOrCurrent(queryDefault(r, "from", r.URL.Query().Get("month")))
	to := validMonthOrFallback(r.URL.Query().Get("to"), from)
	if from > to {
		from, to = to, from
	}
	mode := strings.TrimPrefix(r.URL.Path, "/reports/")
	projectID, headID := parseID(r.URL.Query().Get("project_id")), parseID(r.URL.Query().Get("head_id"))
	// A drilldown keeps stable IDs: names may overlap or be edited.
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

func (a *App) exportGrid(w http.ResponseWriter, r *http.Request) {
	month := validMonthOrCurrent(r.URL.Query().Get("month"))
	grid, err := a.st.Grid(r.Context(), month, r.URL.Query().Get("status"), r.URL.Query().Get("q"))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	u := auth.CurrentUser(r)
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "export", EntityType: "report", Summary: "Exported grid " + month})
	var body bytes.Buffer
	cw := csv.NewWriter(&body)
	_ = cw.Write([]string{"Project", "Head", "Budget", "Actual", "Variance", "Variance %", "Status"})
	for _, row := range grid.Rows {
		_ = cw.Write([]string{csvText(row.Project), csvText(row.Head), csvAmount(row.Budget), csvAmount(row.Actual), csvAmount(row.Variance), row.VariancePercent, csvText(row.Status)})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The export could not be generated.", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="variance-`+month+`.csv"`)
	if _, err := w.Write(body.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "csv response write failed", "request_id", requestID(r), "error", err)
	}
}

// reportLevels are the three groupings store.Report knows, which are also the
// three /reports/* paths. The CSV takes the level as a parameter so a download
// pressed from the Projects or Monthly tab exports that tab (F-G-031); it used to
// hardcode "heads" whichever tab it was pressed from.
var reportLevels = map[string]bool{"monthly": true, "projects": true, "heads": true}

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
	var body bytes.Buffer
	cw := csv.NewWriter(&body)
	_ = cw.Write([]string{"Period", "Project", "Head", "Budget", "Actual", "Variance", "Variance %"})
	for _, row := range rows {
		_ = cw.Write([]string{csvText(row.Period), csvText(row.Project), csvText(row.Head), csvAmount(row.Budget), csvAmount(row.Actual), csvAmount(row.Variance), row.VariancePercent})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The export could not be generated.", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="report-`+from+`-to-`+to+`.csv"`)
	if _, err := w.Write(body.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "csv response write failed", "request_id", requestID(r), "error", err)
	}
}

func nonEmptyReportRows(rows []store.ReportRow) []store.ReportRow {
	out := rows[:0]
	for _, row := range rows {
		if row.Budget != 0 || row.Actual != 0 {
			out = append(out, row)
		}
	}
	return out
}

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
