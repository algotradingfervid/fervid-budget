package money

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// MaxAmount bounds an individual financial entry to 100 crore rupees.
const MaxAmount int64 = 100_000_000_000

var groupedAmount = regexp.MustCompile(`^[+]?(?:[0-9]{1,3}(?:,[0-9]{3})+|[0-9]{1,2}(?:,[0-9]{2})*,[0-9]{3})(?:\.[0-9]{1,2})?$`)

// ParsePaise converts a user-supplied rupee amount into exact integer paise.
// Grouped digits, a leading rupee symbol and surrounding whitespace are accepted.
// Decimal scientific notation remains supported when its value has at most two
// decimal places. Invalid precision is rejected, never rounded or truncated.
func ParsePaise(input string) (int64, error) {
	s := strings.TrimSpace(input)
	s = strings.TrimSpace(strings.TrimPrefix(s, "₹"))
	if strings.Contains(s, ",") && !groupedAmount.MatchString(s) {
		return 0, fmt.Errorf("invalid digit grouping")
	}
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0, fmt.Errorf("amount is required")
	}
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "−") {
		return 0, fmt.Errorf("amount must be positive")
	}
	s = strings.TrimPrefix(s, "+")
	var exponent int64
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		var err error
		exponent, err = strconv.ParseInt(s[i+1:], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid amount")
		}
		s = s[:i]
	}
	whole, fraction, _ := strings.Cut(s, ".")
	digits := whole + fraction
	if digits == "" {
		return 0, fmt.Errorf("invalid amount")
	}
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return 0, fmt.Errorf("invalid amount")
		}
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return 0, fmt.Errorf("amount must be positive")
	}

	// Bound the exponent before subtracting or allocating. At most nineteen
	// digits fit in positive int64 paise, including the two decimal places.
	fractionDigits := int64(len(fraction))
	if exponent < fractionDigits-2 {
		return 0, fmt.Errorf("amount must have at most two decimal places")
	}
	if exponent > fractionDigits+19 {
		return 0, fmt.Errorf("amount is too large")
	}
	zeros := 2 - (fractionDigits - exponent)
	if int64(len(digits))+zeros > 19 {
		return 0, fmt.Errorf("amount is too large")
	}
	paise, err := strconv.ParseInt(digits+strings.Repeat("0", int(zeros)), 10, 64)
	if err != nil || paise > MaxAmount {
		return 0, fmt.Errorf("amount is too large (maximum 100 crore rupees)")
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
