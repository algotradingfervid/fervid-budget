package store

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// settingInTx reads a runtime setting on the caller's transaction, falling back
// to def when the key is absent. Numbering must see the format that is in force
// at the instant the number is reserved, so it reads inside the same tx.
func settingInTx(tx *sql.Tx, key, def string) (string, error) {
	var v string
	err := tx.QueryRow(`SELECT value FROM app_settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows || (err == nil && strings.TrimSpace(v) == "") {
		return def, nil
	}
	return v, err
}

// requestNumberYear resolves the year segment of a request number from the
// number_year_mode setting: "calendar" (2026), "financial" (2025-26, April
// start) or "none" (empty segment).
func requestNumberYear(tx *sql.Tx, now time.Time) (string, error) {
	mode, err := settingInTx(tx, "number_year_mode", "calendar")
	if err != nil {
		return "", err
	}
	switch mode {
	case "none":
		return "", nil
	case "financial":
		start := now.Year()
		if now.Month() < time.April {
			start--
		}
		return fmt.Sprintf("%d-%02d", start, (start+1)%100), nil
	default:
		return now.Format("2006"), nil
	}
}

// NextRequestNumber reserves the next monotonic request number for a year
// inside the caller's transaction and returns it as <prefix>-<year>-NNNNNN.
func NextRequestNumber(tx *sql.Tx, year string) (string, error) {
	prefix, err := settingInTx(tx, "number_prefix", "PR")
	if err != nil {
		return "", err
	}
	widthText, err := settingInTx(tx, "number_width", "6")
	if err != nil {
		return "", err
	}
	width, err := strconv.Atoi(strings.TrimSpace(widthText))
	if err != nil || width < 1 || width > 12 {
		width = 6
	}
	if _, err := tx.Exec(`INSERT INTO request_number_seq(year,last) VALUES(?,0) ON CONFLICT(year) DO NOTHING`, year); err != nil {
		return "", err
	}
	var last int64
	if err := tx.QueryRow(`UPDATE request_number_seq SET last=last+1 WHERE year=? RETURNING last`, year).Scan(&last); err != nil {
		return "", err
	}
	if year == "" {
		return fmt.Sprintf("%s-%0*d", prefix, width, last), nil
	}
	return fmt.Sprintf("%s-%s-%0*d", prefix, year, width, last), nil
}

var requestTypes = map[string]bool{
	"vendor_invoice": true, "vendor_advance": true, "reimbursement": true,
	"employee_advance": true, "recoverable": true,
}

// requestStatuses is the Phase-2 status enum. 'draft' is deliberately absent —
// D1: a request exists only once submitted. Phase 3 appends on_hold,
// processing, completed and completed_partial.
var requestStatuses = map[string]bool{
	"pending": true, "returned": true, "approved": true, "rejected": true,
	"withdrawn": true, "cancellation_requested": true, "cancelled": true,
}

var legalTransitions = map[string]map[string]bool{
	"returned": {"pending": true},
	"pending":  {"approved": true, "returned": true, "rejected": true, "withdrawn": true},
	// G1/G2: post-approval cancellation. An approved request may be frozen by
	// the requester (cancellation_requested) or cancelled outright by a manager.
	"approved":               {"cancellation_requested": true, "cancelled": true},
	"cancellation_requested": {"cancelled": true, "approved": true},
}

func canTransition(from, to string) bool {
	return legalTransitions[from][to]
}

// recoverableCategoryRules is the Phase-2 view of the recoverable categories.
// Phase 4 creates `recoverable_categories` and replaces this map with rows from
// it (requires_project / requires_counterparty) without changing call sites.
var recoverableCategoryRules = map[string]struct{ RequiresProject, RequiresCounterparty bool }{
	"emd":              {RequiresProject: true},
	"pbg":              {RequiresProject: true},
	"icd":              {RequiresCounterparty: true},
	"employee_advance": {},
	"security_deposit": {RequiresCounterparty: true},
	"other":            {},
}

func validateRequestInput(in RequestInput) error {
	if in.Amount <= 0 {
		return fmt.Errorf("%w: a positive amount is required", ErrValidation)
	}
	if strings.TrimSpace(in.ShortTitle) == "" {
		return fmt.Errorf("%w: a short title is required — it is what your approver sees in their list", ErrValidation)
	}
	if strings.TrimSpace(in.Purpose) == "" {
		return fmt.Errorf("%w: purpose is required", ErrValidation)
	}
	if in.ManagerID <= 0 {
		return fmt.Errorf("%w: choose an approver", ErrValidation)
	}
	if in.Treatment != "budget" && in.Treatment != "recoverable" {
		return fmt.Errorf("%w: treatment must be budget or recoverable", ErrValidation)
	}
	if !requestTypes[in.Type] {
		return fmt.Errorf("%w: unknown request type", ErrValidation)
	}
	for _, d := range []struct{ label, value string }{
		{"required-by date", in.NeededBy},
		{"expected return date", in.ExpectedReturnDate},
		{"invoice date", in.InvoiceDate},
		{"expense date", in.ExpenseDate},
	} {
		if d.value != "" && !validDate(d.value) {
			return fmt.Errorf("%w: %s is invalid", ErrValidation, d.label)
		}
	}
	needsProjectHead := func() error {
		if in.ProjectID <= 0 || in.HeadID <= 0 {
			return fmt.Errorf("%w: project and head are required for this type", ErrValidation)
		}
		return nil
	}
	needsVendor := func() error {
		if in.VendorID <= 0 {
			return fmt.Errorf("%w: choose a vendor from the vendor master", ErrValidation)
		}
		return nil
	}
	needsRecoverable := func() error {
		rule, ok := recoverableCategoryRules[in.RecoverableCategory]
		if !ok {
			return fmt.Errorf("%w: choose a recoverable category", ErrValidation)
		}
		if !validDate(in.ExpectedReturnDate) {
			return fmt.Errorf("%w: expected return date is required for recoverables", ErrValidation)
		}
		if strings.TrimSpace(in.RepaymentNotes) == "" {
			return fmt.Errorf("%w: repayment or refund terms are required for recoverables", ErrValidation)
		}
		if rule.RequiresProject && in.ProjectID <= 0 {
			return fmt.Errorf("%w: this recoverable category always belongs to a project", ErrValidation)
		}
		if rule.RequiresCounterparty && strings.TrimSpace(in.Counterparty) == "" {
			return fmt.Errorf("%w: this recoverable category needs a counterparty company", ErrValidation)
		}
		return nil
	}
	switch in.Type {
	case "vendor_invoice":
		if in.Treatment != "budget" {
			return fmt.Errorf("%w: a vendor invoice is a budget expense", ErrValidation)
		}
		if err := needsProjectHead(); err != nil {
			return err
		}
		if err := needsVendor(); err != nil {
			return err
		}
		if strings.TrimSpace(in.InvoiceNo) == "" {
			return fmt.Errorf("%w: the invoice number is required", ErrValidation)
		}
		if !validDate(in.InvoiceDate) {
			return fmt.Errorf("%w: the invoice date is required", ErrValidation)
		}
	case "vendor_advance":
		if in.Treatment != "budget" {
			return fmt.Errorf("%w: a vendor advance is a budget expense", ErrValidation)
		}
		if err := needsProjectHead(); err != nil {
			return err
		}
		if err := needsVendor(); err != nil {
			return err
		}
		if strings.TrimSpace(in.AdvanceReason) == "" {
			return fmt.Errorf("%w: say what the advance is for", ErrValidation)
		}
	case "reimbursement":
		if in.Treatment != "budget" {
			return fmt.Errorf("%w: reimbursement is a budget expense", ErrValidation)
		}
		if err := needsProjectHead(); err != nil {
			return err
		}
		if !validDate(in.ExpenseDate) {
			return fmt.Errorf("%w: the expense date is required", ErrValidation)
		}
	case "employee_advance":
		if strings.TrimSpace(in.AdvanceReason) == "" {
			return fmt.Errorf("%w: say what the money is for", ErrValidation)
		}
		if in.Treatment == "budget" {
			return needsProjectHead()
		}
		return needsRecoverable()
	case "recoverable":
		if in.Treatment != "recoverable" {
			return fmt.Errorf("%w: recoverable type requires recoverable treatment", ErrValidation)
		}
		return needsRecoverable()
	}
	return nil
}

// forcesRequesterPayee reports whether the payee must equal the requester.
// These types never carry a vendor row — the payee snapshot is the only payee.
func forcesRequesterPayee(t string) bool {
	return t == "reimbursement" || t == "employee_advance"
}
