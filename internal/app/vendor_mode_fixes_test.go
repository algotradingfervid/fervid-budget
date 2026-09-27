package app

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

func TestNEFTIdentitySurvivesConfiguredEntryPreviewSaveAndReceipt(t *testing.T) {
	s := newAppTestServer(t)
	actor, head := s.seedHead("NEFT fidelity")
	reqID, vendorID := s.seedVendorRequest(987, actor.ID, actor.ID, head, 12345, "NEFT Supplier Legal")
	if _, err := s.st.DB().Exec(`UPDATE vendors SET default_payment_mode='NEFT' WHERE id=?`, vendorID); err != nil {
		t.Fatal(err)
	}
	if err := s.st.SetAppSetting(s.ctx, actor, "payment_modes", "NEFT, RTGS, Bank transfer, DD"); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm(strconvPath("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	entry := responseBody(t, s.request("GET", strconvPath("/payments/new?request=%d", reqID), nil, ""))
	for _, want := range []string{`value="neft" selected>NEFT`, `value="rtgs" >RTGS`, `value="bank_transfer" >Bank transfer`, `value="dd" >DD`} {
		if !strings.Contains(entry, want) {
			t.Fatalf("entry missing %q", want)
		}
	}
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(head)}, "amount": {"123.45"}, "paid_on": {"2026-09-26"}, "payment_mode": {"neft"}, "reference_no": {"NEFT-IDENTITY-123"}, "settlement": {"settled"}}
	preview := responseBody(t, s.postForm(strconvPath("/requests/%d/settlement-preview", reqID), form))
	if !strings.Contains(preview, ">NEFT<") || !strings.Contains(preview, `value="neft"`) {
		t.Fatal("preview lost NEFT identity")
	}
	saved := s.postForm("/payments", form)
	requireStatus(t, saved, http.StatusSeeOther)
	location := saved.Header.Get("Location")
	saved.Body.Close()
	var mode string
	var amount int64
	if err := s.st.DB().QueryRow(`SELECT payment_mode,amount FROM payments WHERE request_id=?`, reqID).Scan(&mode, &amount); err != nil {
		t.Fatal(err)
	}
	if mode != "neft" || amount != 12345 {
		t.Fatalf("saved mode=%q amount=%d", mode, amount)
	}
	receipt := responseBody(t, s.request("GET", location, nil, ""))
	if !strings.Contains(receipt, ">NEFT<") {
		t.Fatal("receipt lost NEFT identity")
	}
	// Existing generic transfers remain generic; no retroactive guess about their rail.
	if canonicalPaymentMode("bank_transfer") != "bank_transfer" || paymentModeText("bank_transfer") != "Bank transfer" {
		t.Fatal("legacy bank transfer changed")
	}
	for _, tc := range []struct{ input, key, label string }{{"NEFT", "neft", "NEFT"}, {"RTGS", "rtgs", "RTGS"}, {"Demand draft", "dd", "Demand draft"}} {
		if canonicalPaymentMode(tc.input) != tc.key || paymentModeText(tc.key) != tc.label {
			t.Fatalf("mode %q lost identity", tc.input)
		}
	}
}

func TestVendorShortNameInListSearchAndRequestPickers(t *testing.T) {
	s := newAppTestServer(t)
	id := s.seedVendor(store.VendorInput{Name: "Short Name Supplier Legal", DisplayName: "SN Supplier", VendorType: "company", Status: "active"})
	s.seedVendor(store.VendorInput{Name: "No Short Name Supplier", VendorType: "company", Status: "active"})
	s.login(s.cfg.AdminEmail, testAdminPassword)
	list := responseBody(t, s.request("GET", "/vendors", nil, ""))
	if !strings.Contains(list, strconvPath(`/vendors/%d">SN Supplier</a>`, id)) || !strings.Contains(list, `>Short Name Supplier Legal</span>`) || !strings.Contains(list, `>No Short Name Supplier</a>`) {
		t.Fatal("list must use short name, keep legal context, and fall back when empty")
	}
	search := responseBody(t, s.htmxGet("/vendors/search?q=SN+Supplier"))
	if !strings.Contains(search, `data-name="SN Supplier"`) || !strings.Contains(search, `<b>SN Supplier</b>`) || !strings.Contains(search, `Short Name Supplier Legal`) {
		t.Fatal("search must find and display short name with legal context")
	}
	for _, kind := range []string{"vendor_invoice", "vendor_advance"} {
		body := responseBody(t, s.request("GET", "/requests/new?type="+kind, nil, ""))
		if !strings.Contains(body, `>SN Supplier (Short Name Supplier Legal)</option>`) || !strings.Contains(body, `>No Short Name Supplier</option>`) {
			t.Fatalf("%s picker must show short name and fall back when empty", kind)
		}
	}
	detail := responseBody(t, s.request("GET", strconvPath("/vendors/%d", id), nil, ""))
	if !strings.Contains(detail, `value="Short Name Supplier Legal"`) || !strings.Contains(detail, `value="SN Supplier"`) {
		t.Fatal("legal and short names must remain distinct on detail")
	}
}
