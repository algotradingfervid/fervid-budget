package app

// Helpers for CSV downloads.

import (
	"fmt"
)

// csvAmount is how money leaves the application in a file. Every export used to
// write money.FormatPaise, so an Actual cell read "₹1,50,000.00" — quoted,
// because the Indian grouping commas would otherwise break the field, and
// Number() of it is NaN (F-G-030). An export is the hop where the data stops
// being a rendered report and starts being data, so it carries rupees to two
// decimals with no symbol, no grouping and no quoting, and the reader formats.
func csvAmount(paise int64) string {
	sign := ""
	if paise < 0 {
		sign, paise = "-", -paise
	}
	return fmt.Sprintf("%s%d.%02d", sign, paise/100, paise%100)
}
