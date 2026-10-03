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

// friendly turns a store error into the sentence a person reads.
//
// A validation error shows the store's own sentence without the "validation
// failed: " prefix, with its first letter left as the store wrote it (screens
// also embed these sentences mid-line). A refusal (ErrForbidden) that carries
// a reason, such as "this request is on hold", shows that reason with a
// capital first letter; a bare refusal gets the generic permission sentence.
// Anything unrecognised gets a generic sentence.
func friendly(err error) string {
	switch {
	case errors.Is(err, store.ErrLockedMonth):
		return "This month is locked. Unlock it with a reason before changing budgets or payments."
	case errors.Is(err, store.ErrDuplicate):
		return "A record with this name already exists."
	case errors.Is(err, store.ErrInactiveHead):
		return "This project/head is inactive."
	case errors.Is(err, store.ErrValidation):
		return strings.TrimPrefix(err.Error(), store.ErrValidation.Error()+": ")
	case errors.Is(err, store.ErrForbidden):
		if msg := strings.TrimPrefix(err.Error(), store.ErrForbidden.Error()+": "); msg != err.Error() {
			return upperFirst(msg)
		}
		return "You do not have permission to perform this action."
	default:
		return "Something went wrong while processing your request."
	}
}

// upperFirst capitalises the first byte of s (the store's messages are ASCII).
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// usedPct is the width of a budget bar: actual as a percentage of budget,
// capped at 118% so an overspent bar still fits its track.
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

// usedText is actual as a whole percentage of budget, or "No budget" when
// money was spent against no budget.
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

// remainingText is what is left of the budget as a whole percentage (negative
// when overspent), or "N/A" without a budget.
func remainingText(budget, actual int64) string {
	if budget <= 0 {
		return "N/A"
	}
	remaining := budget - actual
	pct := (float64(remaining) / float64(budget)) * 100
	return fmt.Sprintf("%.0f%%", pct)
}

// barClasses is the CSS class of a budget bar for each grid status.
var barClasses = map[string]string{
	"over":       "is-over",
	"under":      "is-under",
	"on-track":   "is-ontrack",
	"unbudgeted": "is-unbudgeted",
}

// barClass is the CSS class of a budget bar ("" for other statuses).
func barClass(status string) string { return barClasses[status] }

// varClass colours a variance: positive (under budget), negative or zero.
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

// labelOr returns labels[value], or value itself when it has no label.
func labelOr(labels map[string]string, value string) string {
	if label, ok := labels[value]; ok {
		return label
	}
	return value
}

// gridStatusLabels are the words for each grid status (see statusForView).
var gridStatusLabels = map[string]string{
	"no-activity": "No activity",
	"over":        "Over budget",
	"under":       "Under budget",
	"on-track":    "On track",
	"not-paid":    "Not paid",
	"unbudgeted":  "Unbudgeted spend",
}

// statusText is the label of a grid status.
func statusText(status string) string { return labelOr(gridStatusLabels, status) }

// planStatusText is the label of a month plan's status.
func planStatusText(status string) string {
	return labelOr(map[string]string{"locked": "Locked", "open": "Open"}, status)
}

// statusForView is a head's grid status from its budget and actual spend.
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

// dueClass says where a head's usual payment day stands in the grid's month:
// "overdue", "today" or "upcoming" while its budget is still unpaid,
// "recorded" once spend is recorded, and "none" when there is nothing to say
// (no activity, or no usable day or month).
func dueClass(dueDay, month, status string) string {
	if status == "no-activity" {
		return "none"
	}
	// The grid status is about budget use, not about the requests' settlement.
	if status != "not-paid" {
		return "recorded"
	}
	day, ok := dueDayNumber(dueDay)
	start, err := time.Parse(monthLayout, month)
	if !ok || err != nil {
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

// dueText is the words for dueClass.
func dueText(dueDay, month, status string) string {
	if status == "no-activity" {
		if strings.TrimSpace(dueDay) != "" {
			return "Usual day " + dueDay
		}
		return "No planned spend"
	}
	switch dueClass(dueDay, month, status) {
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

// dueDayNumber reads the first number in a free-text due day ("5th",
// "day 12") and reports whether it is a day of the month (1 to 31).
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

// paymentModeLabels are the words for each payment mode key.
var paymentModeLabels = map[string]string{
	"neft":          "NEFT",
	"rtgs":          "RTGS",
	"dd":            "Demand draft",
	"bank_transfer": "Bank transfer",
	"cash":          "Cash",
	"cheque":        "Cheque",
	"card":          "Card",
	"upi":           "UPI",
	"other":         "Other",
}

// paymentModeText is the label of a payment mode. Older rows may store a
// label ("Bank transfer") rather than a key, so the mode is first turned into
// its key; a mode with no label is shown exactly as stored.
func paymentModeText(mode string) string {
	if label, ok := paymentModeLabels[canonicalPaymentMode(mode)]; ok {
		return label
	}
	return mode
}

// vendorTypeText is the label of a vendor type.
func vendorTypeText(t string) string {
	return labelOr(map[string]string{"company": "Company", "proprietor": "Proprietor", "individual": "Individual"}, t)
}

// vendorStatusText is the label of a vendor status.
func vendorStatusText(status string) string {
	return labelOr(map[string]string{"active": "Active", "inactive": "Inactive"}, status)
}

// categoryChain shows stored comma-separated categories as a
// "·" chain, split the same way the category filter splits them.
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

// boolText shows an active flag as "Active" or "Inactive".
func boolText(v bool) string {
	if v {
		return "Active"
	}
	return "Inactive"
}

// hasText reports whether s has content: not blank and not the JSON "null".
func hasText(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && s != "null"
}

// jsonPretty indents a JSON document for display; text that is not JSON is
// returned unchanged, and an empty or "null" value becomes "".
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

// fileSize shows a byte count as B, KB or MB (1024-based, one decimal).
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
