package money

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func ParsePaise(input string) (int64, error) {
	s := strings.TrimSpace(input)
	s = strings.TrimPrefix(s, "₹")
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0, fmt.Errorf("amount is required")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount")
	}
	paise := int64(math.Round(f * 100))
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

func Percent(variance, budget int64) string {
	if budget == 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.1f%%", (float64(variance)/float64(budget))*100)
}
