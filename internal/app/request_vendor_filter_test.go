package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// The request list and its CSV export honour ?vendor_id=, which every tab,
// pager and export link on the list carries forward: only that vendor's
// requests are listed and exported.
func TestRequestListAndExportFilterByVendor(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Vendor filter")
	managerID := seedSecondApprover(t, s)
	requester := s.seedRequester("vendor-filter@example.test", "Vendor Filter Requester", "RequesterPass123")
	raise := func(vendor, title, invoice string) int64 {
		vendorID := s.seedVendor(store.VendorInput{Name: vendor, VendorType: "company", Status: "active"})
		if _, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget", Type: "vendor_invoice",
			ShortTitle: title, ProjectID: 1, HeadID: headID, Amount: 12345, Purpose: "Vendor filter",
			ManagerID: managerID, VendorID: vendorID, InvoiceNo: invoice, InvoiceDate: "2026-09-26"}); err != nil {
			t.Fatal(err)
		}
		return vendorID
	}
	wanted := raise("Filter Vendor Kept", "Kept vendor invoice", "VF-1")
	raise("Filter Vendor Other", "Other vendor invoice", "VF-2")
	s.loginAdmin()
	for _, path := range []string{
		fmt.Sprintf("/requests?scope=all&bucket=all&vendor_id=%d", wanted),
		fmt.Sprintf("/requests/export.csv?scope=all&bucket=all&vendor_id=%d", wanted),
	} {
		body := responseBody(t, s.request(http.MethodGet, path, nil, ""))
		if !strings.Contains(body, "Kept vendor invoice") {
			t.Fatalf("%s: the chosen vendor's request is missing", path)
		}
		if strings.Contains(body, "Other vendor invoice") {
			t.Fatalf("%s: another vendor's request is listed", path)
		}
	}
}
