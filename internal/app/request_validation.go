package app

import (
	"fervidbudget/internal/store"
	"strings"
	"time"
)

// All currently applicable field errors are shown together; store validation
// remains the authority at write time.
func requestFieldErrors(in store.RequestInput) map[string]string {
	out := map[string]string{}
	require := func(id, value, message string) {
		if strings.TrimSpace(value) == "" {
			out[id] = message
		}
	}
	date := func(id, value, message string) {
		if _, err := time.Parse("2006-01-02", value); err != nil {
			out[id] = message
		}
	}
	require("short-title", in.ShortTitle, "Enter a short title.")
	require("purpose", in.Purpose, "Explain the purpose.")
	if in.Amount <= 0 {
		out["amount"] = "Enter an amount greater than zero."
	}
	if in.ManagerID <= 0 {
		out["approver"] = "Choose an approver."
	}
	if in.Treatment == "budget" {
		if in.ProjectID <= 0 {
			out["project"] = "Choose a project."
		}
		if in.HeadID <= 0 {
			out["head"] = "Choose a head."
		}
	}
	if in.Type == "vendor_invoice" || in.Type == "vendor_advance" {
		if in.VendorID <= 0 {
			out["vendor"] = "Choose a vendor."
		}
	}
	if in.Type == "vendor_invoice" {
		require("invoice-no", in.InvoiceNo, "Enter the invoice number.")
		date("invoice-date", in.InvoiceDate, "Enter a valid invoice date.")
	}
	if in.Type == "employee_advance" || in.Type == "vendor_advance" {
		require("advance-reason", in.AdvanceReason, "Explain what the advance is for.")
	}
	if in.Type == "reimbursement" {
		date("expense-date", in.ExpenseDate, "Enter a valid expense date.")
	}
	if in.Treatment == "recoverable" {
		date("expected-return", in.ExpectedReturnDate, "Enter the expected return date.")
		require("terms", in.RepaymentNotes, "Enter repayment or refund terms.")
	}
	return out
}
