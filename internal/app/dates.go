package app

// Months and dates. The app stores a month as "YYYY-MM" text and a day as
// "YYYY-MM-DD" text; these helpers check and step through those strings and
// format stored timestamps for display.

import (
	"time"
)

// monthLayout is the time layout of a stored month, "YYYY-MM". A stored day
// uses the standard library's time.DateOnly, "YYYY-MM-DD".
const monthLayout = "2006-01"

// currentMonth is this month as "YYYY-MM", in the server's time zone.
func currentMonth() string { return time.Now().Format(monthLayout) }

// dateText formats a stored timestamp in the server's local time zone. The
// database records timestamps in UTC, so they are converted before display.
func dateText(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }

// datepText is dateText for a timestamp that may be missing; nil shows as "".
func datepText(t *time.Time) string {
	if t == nil {
		return ""
	}
	return dateText(*t)
}

// validMonthInput reports whether s is a real month written as "YYYY-MM".
// The store checks months again with its own validMonth, which is the same
// test, before it writes or filters by one.
func validMonthInput(s string) bool {
	_, err := time.Parse(monthLayout, s)
	return err == nil
}

// validMonthOrCurrent returns s if it is a valid month, else this month.
func validMonthOrCurrent(s string) string {
	return validMonthOrFallback(s, currentMonth())
}

// validMonthOrFallback returns s if it is a valid month, else fallback if
// that is valid, else this month.
func validMonthOrFallback(s, fallback string) string {
	if validMonthInput(s) {
		return s
	}
	if !validMonthInput(fallback) {
		return currentMonth()
	}
	return fallback
}

// validDateInput reports whether s is a real day written as "YYYY-MM-DD".
func validDateInput(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// previousMonth is the month before month ("YYYY-MM").
func previousMonth(month string) string {
	return shiftMonth(month, -1)
}

// shiftMonth moves month ("YYYY-MM") by delta months. An invalid month is
// treated as this month.
func shiftMonth(month string, delta int) string {
	t, err := time.Parse(monthLayout, month)
	if err != nil {
		return time.Now().AddDate(0, delta, 0).Format(monthLayout)
	}
	return t.AddDate(0, delta, 0).Format(monthLayout)
}
