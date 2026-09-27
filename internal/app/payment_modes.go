package app

import (
	"strings"

	"fervidbudget/internal/store"
)

// Payment modes (NEFT, UPI, cheque …) as the forms offer them. The stored codes
// and their validation live in the store; these helpers build the option lists
// for the payment forms and the vendor form from the "Payment modes offered"
// setting.

type PaymentModeChoice struct{ Value, Label string }

func canonicalPaymentMode(value string) string { return store.CanonicalPaymentMode(value) }

func vendorPaymentModes(settings map[string]string, current string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range strings.Split(settings["payment_modes"], ",") {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			out = append(out, value)
			seen[value] = true
		}
	}
	// Preserve an older/custom value on unrelated edits, even if configuration
	// no longer offers it for new vendors.
	if current != "" && !seen[current] {
		out = append(out, current)
	}
	return out
}

// paymentModes is the stored vocabulary of payment modes (store.PaymentModes,
// the one list the server also validates against). paymentMode() spells each
// one for a person.
func paymentModes() []string { return store.PaymentModes() }

// paymentModeChoices is the option list of every payment form — settlement
// entry and the historical payment edit alike — so the two can never offer
// different modes. It follows the "Payment modes offered" setting (order and
// labels), keeping only modes in the vocabulary, since the server refuses the
// rest. With nothing usable configured it offers the whole vocabulary. The
// payment's current mode is always kept, so an edit never silently drops it.
func paymentModeChoices(settings map[string]string, current string) []PaymentModeChoice {
	out := []PaymentModeChoice{}
	seen := map[string]bool{}
	add := func(value, label string) {
		if store.IsPaymentMode(value) && !seen[value] {
			seen[value] = true
			out = append(out, PaymentModeChoice{Value: value, Label: label})
		}
	}
	for _, label := range strings.Split(settings["payment_modes"], ",") {
		label = strings.TrimSpace(label)
		add(canonicalPaymentMode(label), label)
	}
	if len(out) == 0 {
		for _, mode := range paymentModes() {
			add(mode, paymentModeText(mode))
		}
	}
	if mode := canonicalPaymentMode(current); mode != "" {
		add(mode, paymentModeText(mode))
	}
	return out
}
