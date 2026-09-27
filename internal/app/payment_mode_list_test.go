package app

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"testing"

	"fervidbudget/internal/store"
)

// selectOptionValues returns the non-empty option values of the payment_mode
// select that selectPattern finds in body.
func selectOptionValues(t *testing.T, body string, selectPattern *regexp.Regexp) []string {
	t.Helper()
	block := selectPattern.FindString(body)
	if block == "" {
		t.Fatalf("payment mode select not found:\n%s", body)
	}
	var values []string
	for _, m := range regexp.MustCompile(`<option value="([^"]*)"`).FindAllStringSubmatch(block, -1) {
		if m[1] != "" {
			values = append(values, m[1])
		}
	}
	return values
}

var (
	editModeSelect       = regexp.MustCompile(`(?s)<select name="payment_mode">.*?</select>`)
	settlementModeSelect = regexp.MustCompile(`(?s)<select id="payment_mode" name="payment_mode" required>.*?</select>`)
)

// The historical edit form used to type its own six modes while settlement
// offered the configured list; both now come from one Go list.
func TestPaymentEditFormOffersTheSettlementModeList(t *testing.T) {
	s := newAppTestServer(t)
	actor, head := s.seedHead("Mode list")
	reqID, _ := s.seedVendorRequest(881, actor.ID, actor.ID, head, 10000, "Mode List Supplier")
	// "Bank transfer" is how older rows spell it, and it is not in the seeded
	// "Payment modes offered" setting.
	payID, err := s.st.CreatePayment(s.ctx, actor, store.PaymentInput{HeadID: head, PaidOn: "2026-06-15", Amount: 5000, VendorPayee: "Legacy", PaymentMode: "Bank transfer"})
	if err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)

	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	settlement := selectOptionValues(t, responseBody(t, s.request(http.MethodGet, strconvPath("/payments/new?request=%d", reqID), nil, "")), settlementModeSelect)

	editBody := responseBody(t, s.request(http.MethodGet, strconvPath("/payments/%d/edit", payID), nil, ""))
	edit := selectOptionValues(t, editBody, editModeSelect)

	want := append(slices.Clone(settlement), "bank_transfer")
	if !slices.Equal(edit, want) {
		t.Fatalf("edit form modes = %v, want the settlement list plus the payment's own mode %v", edit, want)
	}
	for _, mode := range edit {
		if !store.IsPaymentMode(mode) {
			t.Fatalf("edit form offers %q, which the server refuses", mode)
		}
	}
	if !slices.Contains(edit, "neft") || !slices.Contains(edit, "dd") {
		t.Fatalf("edit form lacks modes settlement offers: %v", edit)
	}
	if !regexp.MustCompile(`<option value="bank_transfer" selected>Bank transfer</option>`).MatchString(editBody) {
		t.Fatal("the payment's saved mode is not selected on the edit form")
	}

	// A mode the forms offer saves; one outside the list is refused and nothing
	// changes.
	form := url.Values{"head_id": {fmt.Sprint(head)}, "paid_on": {"2026-06-15"}, "amount": {"50.00"}, "vendor_payee": {"Legacy"}, "payment_mode": {"neft"}}
	requireStatus(t, s.postForm(strconvPath("/payments/%d/edit", payID), form), http.StatusSeeOther)
	form.Set("payment_mode", "wire")
	refused := s.postForm(strconvPath("/payments/%d/edit", payID), form)
	requireStatus(t, refused, http.StatusBadRequest)
	refused.Body.Close()
	p, err := s.st.Payment(s.ctx, payID)
	if err != nil {
		t.Fatal(err)
	}
	if p.PaymentMode != "neft" {
		t.Fatalf("stored mode = %q after a refused edit, want neft", p.PaymentMode)
	}
}

func TestPaymentModeChoicesFallBackToTheWholeList(t *testing.T) {
	got := paymentModeChoices(map[string]string{"payment_modes": " , Wire "}, "")
	var values []string
	for _, c := range got {
		values = append(values, c.Value)
	}
	if !slices.Equal(values, store.PaymentModes()) {
		t.Fatalf("with nothing usable configured, choices = %v, want %v", values, store.PaymentModes())
	}
	configured := paymentModeChoices(map[string]string{"payment_modes": "UPI, Wire, Demand draft"}, "cash")
	if len(configured) != 3 || configured[0] != (PaymentModeChoice{"upi", "UPI"}) || configured[1] != (PaymentModeChoice{"dd", "Demand draft"}) || configured[2] != (PaymentModeChoice{"cash", "Cash"}) {
		t.Fatalf("configured choices = %v", configured)
	}
}

func TestStoreRefusesPaymentModesOutsideTheList(t *testing.T) {
	s := newAppTestServer(t)
	actor, head := s.seedHead("Mode validation")
	if _, err := s.st.CreatePayment(s.ctx, actor, store.PaymentInput{HeadID: head, PaidOn: "2026-06-15", Amount: 100, PaymentMode: "wire"}); !errors.Is(err, store.ErrValidation) {
		t.Fatalf("unknown mode accepted: %v", err)
	}
	for _, mode := range append(store.PaymentModes(), "NEFT", "Bank transfer", "Demand draft", "") {
		if _, err := s.st.CreatePayment(s.ctx, actor, store.PaymentInput{HeadID: head, PaidOn: "2026-06-15", Amount: 100, PaymentMode: mode}); err != nil {
			t.Fatalf("mode %q refused: %v", mode, err)
		}
	}
}
