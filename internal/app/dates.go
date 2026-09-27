package app

// Month (YYYY-MM) and date (YYYY-MM-DD) parsing, month arithmetic, and
// timestamp formatting.

import (
	"time"
)

// dateText and datepText are the FuncMap's "date" and "datep". Every timestamp
// the store writes is CURRENT_TIMESTAMP, which SQLite records in UTC, so a
// stamp printed as it comes back is five and a half hours behind the person
// reading it. hhmm has localised since Task 14; these two did not, and the
// reservation screen is the first that prints both for the same event.
func dateText(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }

func datepText(t *time.Time) string {
	if t == nil {
		return ""
	}
	return dateText(*t)
}

func validMonthInput(s string) bool {
	_, err := time.Parse("2006-01", s)
	return err == nil
}

func validMonthOrCurrent(s string) string {
	return validMonthOrFallback(s, time.Now().Format("2006-01"))
}

func validMonthOrFallback(s, fallback string) string {
	if validMonthInput(s) {
		return s
	}
	if !validMonthInput(fallback) {
		return time.Now().Format("2006-01")
	}
	return fallback
}

func validDateInput(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func previousMonth(month string) string {
	return shiftMonth(month, -1)
}

func shiftMonth(month string, delta int) string {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return time.Now().AddDate(0, delta, 0).Format("2006-01")
	}
	return t.AddDate(0, delta, 0).Format("2006-01")
}
