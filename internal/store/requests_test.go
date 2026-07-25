package store

import (
	"strings"
	"testing"
	"time"
)

func TestNextRequestNumberIsMonotonicPerYear(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	got := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		n, err := NextRequestNumber(tx, "2026")
		if err != nil {
			t.Fatalf("NextRequestNumber: %v", err)
		}
		got = append(got, n)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	want := []string{"PR-2026-000001", "PR-2026-000002", "PR-2026-000003"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("number[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	tx2, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback()
	n, err := NextRequestNumber(tx2, "2027")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(n, "PR-2027-000001") {
		t.Fatalf("new-year number = %q, want PR-2027-000001", n)
	}
}

// A20/D6: the Configuration numbering fieldset actually drives the format.
func TestNextRequestNumberHonoursConfiguredFormat(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DB().Exec(`UPDATE app_settings SET value='REQ' WHERE key='number_prefix'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE app_settings SET value='4' WHERE key='number_width'`); err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	n, err := NextRequestNumber(tx, "2026")
	if err != nil {
		t.Fatal(err)
	}
	if n != "REQ-2026-0001" {
		t.Fatalf("configured number = %q, want REQ-2026-0001", n)
	}
}

func TestRequestNumberYearSegmentModes(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	march := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	april := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if y, err := requestNumberYear(tx, march); err != nil || y != "2026" {
		t.Fatalf("calendar year = %q, %v; want 2026", y, err)
	}
	if _, err := tx.Exec(`UPDATE app_settings SET value='financial' WHERE key='number_year_mode'`); err != nil {
		t.Fatal(err)
	}
	if y, err := requestNumberYear(tx, march); err != nil || y != "2025-26" {
		t.Fatalf("financial year (March) = %q, %v; want 2025-26", y, err)
	}
	if y, err := requestNumberYear(tx, april); err != nil || y != "2026-27" {
		t.Fatalf("financial year (April) = %q, %v; want 2026-27", y, err)
	}
}

func TestValidateRequestInputPerType(t *testing.T) {
	base := func(mut func(*RequestInput)) RequestInput {
		in := RequestInput{
			Treatment: "budget", Type: "vendor_invoice", ShortTitle: "July switchgear",
			ProjectID: 1, HeadID: 2, Amount: 1000, Purpose: "buy", ManagerID: 7,
			VendorID: 3, InvoiceNo: "SE/26-27/1184", InvoiceDate: "2026-07-18",
		}
		if mut != nil {
			mut(&in)
		}
		return in
	}
	cases := []struct {
		name string
		in   RequestInput
		ok   bool
	}{
		{"vendor_invoice ok", base(nil), true},
		{"vendor_invoice needs project", base(func(i *RequestInput) { i.ProjectID = 0 }), false},
		{"vendor_invoice needs a vendor row, not free text", base(func(i *RequestInput) { i.VendorID = 0; i.VendorPayee = "Acme" }), false},
		{"vendor_invoice needs invoice number", base(func(i *RequestInput) { i.InvoiceNo = "" }), false},
		{"vendor_invoice needs invoice date", base(func(i *RequestInput) { i.InvoiceDate = "" }), false},
		{"vendor_invoice rejects a malformed invoice date", base(func(i *RequestInput) { i.InvoiceDate = "18-07-2026" }), false},
		{"short title is required", base(func(i *RequestInput) { i.ShortTitle = "  " }), false},
		{"vendor_advance ok", base(func(i *RequestInput) {
			i.Type, i.InvoiceNo, i.InvoiceDate = "vendor_advance", "", ""
			i.AdvanceReason = "40% booking against PO-2026-0417"
		}), true},
		{"vendor_advance needs a reason", base(func(i *RequestInput) {
			i.Type, i.InvoiceNo, i.InvoiceDate = "vendor_advance", "", ""
		}), false},
		{"vendor_advance needs a vendor", base(func(i *RequestInput) {
			i.Type, i.InvoiceNo, i.InvoiceDate, i.VendorID = "vendor_advance", "", "", 0
			i.AdvanceReason = "booking"
		}), false},
		{"reimbursement ok without a vendor", base(func(i *RequestInput) {
			i.Type, i.VendorID, i.InvoiceNo, i.InvoiceDate = "reimbursement", 0, "", ""
			i.ExpenseDate = "2026-07-21"
		}), true},
		{"reimbursement needs the expense date", base(func(i *RequestInput) {
			i.Type, i.VendorID, i.InvoiceNo, i.InvoiceDate = "reimbursement", 0, "", ""
		}), false},
		{"employee_advance budget needs head", base(func(i *RequestInput) {
			i.Type, i.VendorID, i.InvoiceNo, i.InvoiceDate, i.HeadID = "employee_advance", 0, "", "", 0
			i.AdvanceReason = "site mobilisation cash"
		}), false},
		{"employee_advance recoverable needs return date", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "employee_advance", ShortTitle: "Site cash",
				Amount: 1000, Purpose: "buy", ManagerID: 7, AdvanceReason: "site cash",
				RecoverableCategory: "employee_advance", RepaymentNotes: "monthly"}
		}), false},
		{"employee_advance recoverable ok", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "employee_advance", ShortTitle: "Site cash",
				Amount: 1000, Purpose: "buy", ManagerID: 7, AdvanceReason: "site cash",
				RecoverableCategory: "employee_advance", ExpectedReturnDate: "2026-12-01", RepaymentNotes: "monthly"}
		}), true},
		{"recoverable EMD ok with a project", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "Ridge Metro EMD",
				Amount: 1000, Purpose: "tender", ManagerID: 7, RecoverableCategory: "emd", ProjectID: 4,
				ExpectedReturnDate: "2026-12-01", RepaymentNotes: "on tender close"}
		}), true},
		{"recoverable EMD without a project is rejected", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "Ridge Metro EMD",
				Amount: 1000, Purpose: "tender", ManagerID: 7, RecoverableCategory: "emd",
				ExpectedReturnDate: "2026-12-01", RepaymentNotes: "on tender close"}
		}), false},
		{"recoverable ICD without a counterparty is rejected", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "ICD to Meridian",
				Amount: 1000, Purpose: "deposit", ManagerID: 7, RecoverableCategory: "icd",
				ExpectedReturnDate: "2026-12-01", RepaymentNotes: "on maturity"}
		}), false},
		{"recoverable ICD with a counterparty ok", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "ICD to Meridian",
				Amount: 1000, Purpose: "deposit", ManagerID: 7, RecoverableCategory: "icd",
				Counterparty: "Meridian Holdings Pvt Ltd", ExpectedReturnDate: "2026-12-01", RepaymentNotes: "on maturity"}
		}), true},
		{"unknown recoverable category", base(func(i *RequestInput) {
			*i = RequestInput{Treatment: "recoverable", Type: "recoverable", ShortTitle: "Mystery",
				Amount: 1000, Purpose: "x", ManagerID: 7, RecoverableCategory: "mystery",
				ExpectedReturnDate: "2026-12-01", RepaymentNotes: "n"}
		}), false},
		{"unknown type", base(func(i *RequestInput) { i.Type = "mystery" }), false},
		{"no manager", base(func(i *RequestInput) { i.ManagerID = 0 }), false},
		{"no amount", base(func(i *RequestInput) { i.Amount = 0 }), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRequestInput(c.in)
			if c.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("want validation error, got nil")
			}
		})
	}
}

// A5/D1: 'draft' is gone; cancellation_requested and cancelled are in.
func TestCanTransition(t *testing.T) {
	legal := map[[2]string]bool{
		{"returned", "pending"}: true,
		{"pending", "approved"}: true, {"pending", "returned"}: true,
		{"pending", "rejected"}: true, {"pending", "withdrawn"}: true,
		{"approved", "cancellation_requested"}: true, {"approved", "cancelled"}: true,
		{"cancellation_requested", "cancelled"}: true, {"cancellation_requested", "approved"}: true,
	}
	all := []string{"draft", "pending", "returned", "rejected", "withdrawn", "approved",
		"cancellation_requested", "cancelled"}
	for _, from := range all {
		for _, to := range all {
			want := legal[[2]string{from, to}]
			if got := canTransition(from, to); got != want {
				t.Fatalf("canTransition(%q,%q)=%v want %v", from, to, got, want)
			}
		}
	}
	// L1 (inverted): 'draft' is not a status this system knows.
	if requestStatuses["draft"] {
		t.Fatal("draft is present in the status enum; D1 removed it")
	}
	for _, s := range []string{"pending", "returned", "rejected", "withdrawn", "approved",
		"cancellation_requested", "cancelled"} {
		if !requestStatuses[s] {
			t.Fatalf("status %q missing from the enum", s)
		}
	}
}
