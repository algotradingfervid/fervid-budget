package app

// CSV downloads. Every export builds its header and rows, records its own
// "export" audit row, and hands the rows to sendCSV. Text cells go through
// csvText, which stops a cell being read as a spreadsheet
// formula; money cells go through csvAmount.

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
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

// csvText makes a value safe for a CSV cell that a spreadsheet will open. A
// value that a spreadsheet would treat as a formula (starting with = + - or @,
// after any leading blanks or byte-order mark), that starts with a byte-order
// mark, or that contains a tab or line break, is prefixed with an apostrophe
// so it is shown as plain text.
func csvText(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n\uFEFF")
	if strings.ContainsAny(value, "\t\r\n") || strings.HasPrefix(value, "\uFEFF") || (trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0]))) {
		return "'" + value
	}
	return value
}
