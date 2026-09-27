package app

// Display helpers the templates call to turn stored values into the words
// and CSS classes a person sees, plus friendly(), which turns a store error
// into a sentence.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"fervidbudget/internal/store"
)

// friendly turns a store error into the sentence a person reads. The
// `validation failed: ` sentinel is stripped: it is how the code recognises the
// class of error, never something anybody wants to find at the top of a form
// (F-B-08).
func friendly(err error) string {
	switch {
	case errors.Is(err, store.ErrLockedMonth):
		return "This month is locked. Unlock it with a reason before changing budgets or payments."
	case errors.Is(err, store.ErrDuplicate):
		return "A record with this name already exists."
	case errors.Is(err, store.ErrInactiveHead):
		return "This project/head is inactive."
	case errors.Is(err, store.ErrValidation):
		// The sentence only, never the sentinel. The case of the first letter is
		// left exactly as the store wrote it: these strings are sentence
		// fragments the screens embed as well as show ("Correct it and confirm
		// again"), and a great many of them are asserted verbatim.
		return strings.TrimPrefix(err.Error(), store.ErrValidation.Error()+": ")
	case errors.Is(err, store.ErrForbidden):
		// A refusal that carries a reason keeps it. The state conflicts on the
		// settlement path — "reserve this request before recording its payment",
		// "this request is on hold" — are rules, not faults, and fell through to
		// the generic sentence that also covers a genuine server error (F-A-09).
		// The sentinel prefix is trimmed because "forbidden: …" is not a sentence
		// a person reads; ErrForbidden alone, with nothing wrapped, still gets the
		// permission wording.
		if msg := strings.TrimPrefix(err.Error(), store.ErrForbidden.Error()+": "); msg != err.Error() {
			return upperFirst(msg)
		}
		return "You do not have permission to perform this action."
	default:
		return "Something went wrong while processing your request."
	}
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func usedPct(budget, actual int64) string {
	if budget <= 0 || actual <= 0 {
		return "0%"
	}
	pct := (float64(actual) / float64(budget)) * 100
	if pct > 118 {
		pct = 118
	}
	return fmt.Sprintf("%.1f%%", pct)
}

func usedText(budget, actual int64) string {
	if budget <= 0 && actual > 0 {
		return "No budget"
	}
	if budget <= 0 || actual <= 0 {
		return "0%"
	}
	pct := (float64(actual) / float64(budget)) * 100
	return fmt.Sprintf("%.0f%%", pct)
}

func remainingText(budget, actual int64) string {
	if budget <= 0 {
		return "N/A"
	}
	remaining := budget - actual
	pct := (float64(remaining) / float64(budget)) * 100
	return fmt.Sprintf("%.0f%%", pct)
}

func barClass(status string) string {
	switch status {
	case "over":
		return "is-over"
	case "under":
		return "is-under"
	case "on-track":
		return "is-ontrack"
	case "unbudgeted":
		return "is-unbudgeted"
	default:
		return ""
	}
}

func varClass(variance int64) string {
	switch {
	case variance > 0:
		return "varpos"
	case variance < 0:
		return "varneg"
	default:
		return "varzero"
	}
}

func statusText(status string) string {
	switch status {
	case "no-activity":
		return "No activity"
	case "over":
		return "Over budget"
	case "under":
		return "Under budget"
	case "on-track":
		return "On track"
	case "not-paid":
		return "Not paid"
	case "unbudgeted":
		return "Unbudgeted spend"
	default:
		return status
	}
}

func planStatusText(status string) string {
	switch status {
	case "locked":
		return "Locked"
	case "open":
		return "Open"
	default:
		return status
	}
}

func statusForView(budget, actual int64) string {
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

func dueClass(dueDay, month, status string) string {
	if status == "no-activity" {
		return "none"
	}
	// Grid status is budget utilization, not the settlement state of its requests.
	if status != "not-paid" {
		return "recorded"
	}
	day, ok := dueDayNumber(dueDay)
	if !ok || !validMonthInput(month) {
		return "none"
	}
	start, err := time.Parse(monthLayout, month)
	if err != nil {
		return "none"
	}
	last := start.AddDate(0, 1, -1).Day()
	if day > last {
		day = last
	}
	now := time.Now()
	if now.Format(monthLayout) != month {
		target := time.Date(start.Year(), start.Month(), day, 0, 0, 0, 0, now.Location())
		if target.Before(now) {
			return "overdue"
		}
		return "upcoming"
	}
	switch {
	case now.Day() > day:
		return "overdue"
	case now.Day() == day:
		return "today"
	default:
		return "upcoming"
	}
}

func dueText(dueDay, month, status string) string {
	if status == "no-activity" {
		if strings.TrimSpace(dueDay) != "" {
			return "Usual day " + dueDay
		}
		return "No planned spend"
	}
	class := dueClass(dueDay, month, status)
	switch class {
	case "recorded":
		return "Payment recorded"
	case "overdue":
		return "Overdue"
	case "today":
		return "Due today"
	case "upcoming":
		return "Upcoming"
	default:
		if strings.TrimSpace(dueDay) == "" {
			return "No due day"
		}
		return "Due " + dueDay
	}
}

func dueDayNumber(dueDay string) (int, bool) {
	digits := ""
	for _, r := range dueDay {
		if r >= '0' && r <= '9' {
			digits += string(r)
		} else if digits != "" {
			break
		}
	}
	if digits == "" {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	return n, err == nil && n >= 1 && n <= 31
}

func paymentModeText(mode string) string {
	switch canonicalPaymentMode(mode) {
	case "neft":
		return "NEFT"
	case "rtgs":
		return "RTGS"
	case "dd":
		return "Demand draft"
	case "bank_transfer":
		return "Bank transfer"
	case "cash":
		return "Cash"
	case "cheque":
		return "Cheque"
	case "card":
		return "Card"
	case "upi":
		return "UPI"
	case "other":
		return "Other"
	default:
		return mode
	}
}

func vendorTypeText(t string) string {
	switch t {
	case "company":
		return "Company"
	case "proprietor":
		return "Proprietor"
	case "individual":
		return "Individual"
	default:
		return t
	}
}

func vendorStatusText(status string) string {
	switch status {
	case "active":
		return "Active"
	case "inactive":
		return "Inactive"
	default:
		return status
	}
}

// categoryChain renders the stored comma-separated categories the way the
// approved list screen does, as a "·" chain. store.SplitCategories is what
// decides the boundaries, so the filter's option list and this reading can
// never disagree about what a category is.
func categoryChain(raw string) string {
	return strings.Join(store.SplitCategories(raw), " · ")
}

// plural picks between two whole phrases rather than appending an "s", because
// "1 vendor has" and "2 vendors have" differ in more than one place.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func boolText(v bool) string {
	if v {
		return "Active"
	}
	return "Inactive"
}

func hasText(s string) bool {
	return strings.TrimSpace(s) != "" && strings.TrimSpace(s) != "null"
}

func jsonPretty(s string) string {
	if !hasText(s) {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(out)
}

func fileSize(size int64) string {
	switch {
	case size >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	case size >= 1024:
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	default:
		return fmt.Sprintf("%d B", size)
	}
}
