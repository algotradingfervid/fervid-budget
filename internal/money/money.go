package money

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParsePaise converts a user-supplied rupee amount into paise. The browser
// writes Indian-grouped digits back into the money field, so grouped input
// ("1,00,000"), a leading rupee symbol with or without a space ("₹ 1,00,000.50")
// and surrounding whitespace are all accepted. Non-numeric, empty and
// non-positive amounts are rejected.
func ParsePaise(input string) (int64, error) {
	s := strings.TrimSpace(input)
	s = strings.TrimSpace(strings.TrimPrefix(s, "₹"))
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0, fmt.Errorf("amount is required")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount")
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("invalid amount")
	}
	// Guard before the int64 conversion: Go's float→int conversion saturates,
	// so an amount whose paise exceed int64 would silently become MaxInt64 —
	// a different number from the one submitted — and the <= 0 guard below
	// would never fire (F-B-01). float64(math.MaxInt64) is exactly 2^63, and
	// every representable float below it converts to a valid int64, so >= is
	// the precise boundary. Too-negative values saturate to MinInt64 and are
	// caught by the positivity check.
	rounded := math.Round(f * 100)
	if rounded >= float64(math.MaxInt64) {
		return 0, fmt.Errorf("amount is too large")
	}
	paise := int64(rounded)
	if paise <= 0 {
		return 0, fmt.Errorf("amount must be positive")
	}
	return paise, nil
}

func FormatPaise(paise int64) string {
	sign := ""
	if paise < 0 {
		sign = "-"
		paise = -paise
	}
	rupees := paise / 100
	cents := paise % 100
	return fmt.Sprintf("%s₹%s.%02d", sign, indianGroup(rupees), cents)
}

func FormatShort(paise int64) string {
	abs := math.Abs(float64(paise) / 100)
	sign := ""
	if paise < 0 {
		sign = "-"
	}
	switch {
	case abs >= 10000000:
		return fmt.Sprintf("%s₹%.2f Cr", sign, abs/10000000)
	case abs >= 100000:
		return fmt.Sprintf("%s₹%.2f L", sign, abs/100000)
	default:
		return FormatPaise(paise)
	}
}

func indianGroup(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	last := s[len(s)-3:]
	prefix := s[:len(s)-3]
	var parts []string
	for len(prefix) > 2 {
		parts = append([]string{prefix[len(prefix)-2:]}, parts...)
		prefix = prefix[:len(prefix)-2]
	}
	if prefix != "" {
		parts = append([]string{prefix}, parts...)
	}
	return strings.Join(append(parts, last), ",")
}

var onesWords = [...]string{
	"", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
	"eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen",
}

var tensWords = [...]string{
	"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety",
}

// InWords renders an amount as Indian English words using the lakh/crore
// system, mirroring the client-side inWords helper in the mockup so the server
// and the browser always agree.
//
// Rounding: it operates on whole rupees only. Any paise remainder is truncated,
// never rounded, so 199 paise reads "One" and 99 paise reads "Zero". Callers
// that need the paise must render them separately.
//
// Negative amounts are prefixed with "Minus". Only the first letter is
// capitalised and no unit is appended, so callers add their own suffix (the
// mockup uses "<words> rupees only").
func InWords(paise int64) string {
	rupees := paise / 100
	negative := rupees < 0
	if negative {
		rupees = -rupees
	}
	if rupees == 0 {
		return "Zero"
	}

	var words []string
	if negative {
		words = append(words, "minus")
	}
	words = append(words, groupWords(rupees)...)

	s := strings.Join(words, " ")
	return strings.ToUpper(s[:1]) + s[1:]
}

// groupWords splits n (always positive) into crore / lakh / thousand / unit
// groups. Counts of a crore or more recurse, which yields the Indian
// "lakh crore" idiom instead of overflowing the word tables.
func groupWords(n int64) []string {
	crore := n / 10000000
	n %= 10000000
	lakh := n / 100000
	n %= 100000
	thousand := n / 1000
	n %= 1000

	var out []string
	if crore > 0 {
		out = append(out, groupWords(crore)...)
		out = append(out, "crore")
	}
	if lakh > 0 {
		out = append(out, threeWords(lakh), "lakh")
	}
	if thousand > 0 {
		out = append(out, threeWords(thousand), "thousand")
	}
	if n > 0 {
		out = append(out, threeWords(n))
	}
	return out
}

// threeWords spells a group of 1-999.
func threeWords(n int64) string {
	if n >= 100 {
		s := onesWords[n/100] + " hundred"
		if n%100 != 0 {
			s += " " + twoWords(n%100)
		}
		return s
	}
	return twoWords(n)
}

// twoWords spells a group of 1-99.
func twoWords(n int64) string {
	if n < 20 {
		return onesWords[n]
	}
	s := tensWords[n/10]
	if n%10 != 0 {
		s += " " + onesWords[n%10]
	}
	return s
}

func Percent(variance, budget int64) string {
	if budget == 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.1f%%", (float64(variance)/float64(budget))*100)
}
