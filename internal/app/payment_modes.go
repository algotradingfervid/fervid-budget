package app

import (
	"strings"

	"fervidbudget/internal/store"
)

// Payment modes (NEFT, UPI, cheque …) as the forms offer them. The stored codes
// and their validation live in the store; these helpers build the option lists
// for the payment forms and the vendor form from the "Payment modes offered"
// setting.

// PaymentModeChoice is one option in a payment form's mode select: the stored
// code (Value) and the label shown (Label).
type PaymentModeChoice struct{ Value, Label string }

// canonicalPaymentMode turns a configured label ("Bank transfer") into its
// stored code ("bank_transfer"); see store.CanonicalPaymentMode.
func canonicalPaymentMode(value string) string { return store.CanonicalPaymentMode(value) }

// vendorPaymentModes lists the vendor form's "default payment mode" choices:
// the configured labels, trimmed and without repeats, plus the vendor's
// current value if it is no longer configured (so an unrelated edit keeps it).
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
	if current != "" && !seen[current] {
		out = append(out, current)
	}
	return out
}

// paymentModes lists every payment mode code the store accepts. The
// "paymentMode" template helper turns a code into its label.
func paymentModes() []string { return store.PaymentModes() }

// paymentModeChoices is the mode list for every payment form (new payments and
// edits). It follows the "Payment modes offered" setting, in its order and with
// its labels, keeping only modes the store accepts. If none are usable it
// offers every mode. The payment's current mode is always included, so an edit
// never drops it.
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
