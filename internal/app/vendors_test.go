package app

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

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
	s.loginAdmin()
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	entry := responseBody(t, s.request("GET", fmt.Sprintf("/payments/new?request=%d", reqID), nil, ""))
	mustContain(t, "entry", entry, `value="neft" selected>NEFT`, `value="rtgs" >RTGS`, `value="bank_transfer" >Bank transfer`, `value="dd" >DD`)
	form := url.Values{"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(head)}, "amount": {"123.45"}, "paid_on": {"2026-09-26"}, "payment_mode": {"neft"}, "reference_no": {"NEFT-IDENTITY-123"}, "settlement": {"settled"}}
	preview := responseBody(t, s.postForm(fmt.Sprintf("/requests/%d/settlement-preview", reqID), form))
	if !strings.Contains(preview, ">NEFT<") || !strings.Contains(preview, `value="neft"`) {
		t.Fatal("preview lost NEFT identity")
	}
	saved := s.postPayment(form)
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
	s.loginAdmin()
	list := responseBody(t, s.request("GET", "/vendors", nil, ""))
	if !strings.Contains(list, fmt.Sprintf(`/vendors/%d">SN Supplier</a>`, id)) || !strings.Contains(list, `>Short Name Supplier Legal</span>`) || !strings.Contains(list, `>No Short Name Supplier</a>`) {
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
	detail := responseBody(t, s.request("GET", fmt.Sprintf("/vendors/%d", id), nil, ""))
	if !strings.Contains(detail, `value="Short Name Supplier Legal"`) || !strings.Contains(detail, `value="SN Supplier"`) {
		t.Fatal("legal and short names must remain distinct on detail")
	}
}

// tableCell pulls one <td> out of the <tr> whose text contains rowKey. The
// vendor list renders its footer totals with the same data-label as the row
// cells, so a cell is only unambiguous when it is taken from a named row.
func tableCell(t *testing.T, body, rowKey, label string) string {
	t.Helper()
	cell := regexp.MustCompile(`data-label="` + regexp.QuoteMeta(label) + `">([^<]*)<`)
	for _, row := range strings.Split(body, "<tr") {
		if !strings.Contains(row, rowKey) {
			continue
		}
		if m := cell.FindStringSubmatch(row); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	t.Fatalf("no %q cell in a row containing %q: %s", label, rowKey, firstLines(body))
	return ""
}

// The vendor list's "Paid this year" and "Open requests" columns and their
// footer totals count real work: payments join to vendors by payments.vendor_id,
// not by payee text, and open requests are counted by the store. This drives the
// screen end to end.
//
// The rename is the decisive step: a join on the payee's name would lose the
// money as soon as the vendor is renamed.
func TestVendorListTotalsCountRealWorkAndSurviveARename(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("VendorTotals")
	const vendorName = "Sundaram Electricals Pvt Ltd"
	reqID, vendorID := s.seedVendorRequest(1, admin.ID, admin.ID, headID, 910000, vendorName)
	// A second request against the same vendor, left pending, so the open count
	// still has something to count once the first one is paid.
	s.insertRequest(t, map[string]any{
		"number": requestNumber(2), "status": "pending", "treatment": "budget", "type": "vendor_invoice",
		"project_id": projectOf(t, s, headID), "head_id": headID, "amount": 250000,
		"purpose": "Switchgear", "short_title": "Switchgear", "vendor_id": vendorID, "vendor_payee": "",
		"requester_id": admin.ID, "manager_id": admin.ID, "submitted_at": sqlNow,
	})
	s.loginAdmin()

	// Before any payment: two open requests, nothing paid.
	body := s.get("/vendors")
	if got := tableCell(t, body, vendorName, "Open requests"); got != "2" {
		t.Fatalf("Open requests = %q before settlement, want 2", got)
	}
	if got := tableCell(t, body, vendorName, "Paid this year"); got != "₹0.00" {
		t.Fatalf("Paid this year = %q before settlement, want ₹0.00", got)
	}

	// Settle one of them through the real screens, which is what writes
	// payments.vendor_id from the request.
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", reqID), url.Values{}), http.StatusSeeOther)
	settle := url.Values{
		"request_id": {strconvFormat(reqID)}, "head_id": {strconvFormat(headID)},
		"paid_on": {time.Now().Format("2006-01-02")}, "amount": {"9100.00"},
		"settlement": {"settled"}, "reference_no": {"VENDOR-TOTALS-REF"},
	}
	resp := s.postPayment(settle)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	body = s.get("/vendors")
	if got := tableCell(t, body, vendorName, "Paid this year"); got != "₹9,100.00" {
		t.Fatalf("Paid this year = %q after settlement, want ₹9,100.00", got)
	}
	// The settled request left the open bucket; the pending one is still in it.
	if got := tableCell(t, body, vendorName, "Open requests"); got != "1" {
		t.Fatalf("Open requests = %q after settlement, want 1", got)
	}
	// The footer totals the rows above it rather than reporting a hard zero.
	if got := tableCell(t, body, "shown of", "Paid this year"); got != "₹9,100.00" {
		t.Fatalf("footer Paid this year = %q, want ₹9,100.00", got)
	}
	if got := tableCell(t, body, "shown of", "Open"); got != "1" {
		t.Fatalf("footer Open = %q, want 1", got)
	}

	// Rename the vendor, touch no payment, and the money stays with it.
	const renamed = "Sundaram Electricals and Controls Pvt Ltd"
	resp = s.postForm("/vendors/"+strconvFormat(vendorID), url.Values{
		"name": {renamed}, "vendor_type": {"company"}, "status": {"active"},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	body = s.get("/vendors")
	if strings.Contains(body, vendorName) {
		t.Fatal("the rename did not apply")
	}
	if got := tableCell(t, body, renamed, "Paid this year"); got != "₹9,100.00" {
		t.Fatalf("Paid this year = %q after the rename, want ₹9,100.00", got)
	}
	if got := tableCell(t, body, renamed, "Open requests"); got != "1" {
		t.Fatalf("Open requests = %q after the rename, want 1", got)
	}

	// The detail screen renders neither figure, so there is no second place
	// quietly showing a zero: Vendor() does not compute them and nothing on the
	// record page claims to.
	detail := s.get("/vendors/" + strconvFormat(vendorID))
	for _, banned := range []string{"Paid this year", "Open requests"} {
		if strings.Contains(detail, banned) {
			t.Fatalf("the vendor detail screen renders %q, which Vendor() never fills in", banned)
		}
	}
}
