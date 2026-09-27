package app

// CSV downloads. Every export builds its header and rows, records its own
// "export" audit row, and hands the rows to sendCSV. Text cells go through
// csvText (security.go), which stops a cell being read as a spreadsheet
// formula; money cells go through csvAmount.

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
)

// sendCSV answers with header and rows as a downloadable CSV file called
// filename. The file is built in memory first, so a failure to build it can
// still answer with an error page instead of half a file.
func (a *App) sendCSV(w http.ResponseWriter, r *http.Request, filename string, header []string, rows [][]string) {
	var body bytes.Buffer
	if err := csv.NewWriter(&body).WriteAll(append([][]string{header}, rows...)); err != nil {
		a.respondError(w, r, http.StatusInternalServerError, "The export could not be generated.", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if _, err := w.Write(body.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "csv response write failed", "request_id", requestID(r), "error", err)
	}
}

// csvAmount writes paise as plain rupees with two decimals, such as 150000.00
// or -12.50: no ₹ sign and no digit grouping, so a spreadsheet reads the cell
// as a number.
func csvAmount(paise int64) string {
	sign := ""
	if paise < 0 {
		sign, paise = "-", -paise
	}
	return fmt.Sprintf("%s%d.%02d", sign, paise/100, paise%100)
}
