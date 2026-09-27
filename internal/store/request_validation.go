package store

import (
	"context"
	"database/sql"
	"fervidbudget/internal/money"
	"fmt"
	"strings"
)

// requestTypes is what validateRequestInput accepts. There are five, and the
// product's type chooser (`requestTypeOptions`, internal/app/requests.go) offers
// all five since the 2026-09-25 fix wave (form-1 / recoverables-1).
//
// The fifth, `recoverable`, is the deposit or guarantee — EMD, PBG, ICD, a
// security deposit — and it exists as a type of its own for one reason: its
// payee. An employee advance always pays the person raising it
// (forcesRequesterPayee), so while it was the only card that could carry the
// recoverable treatment, a ₹10 lakh inter-corporate deposit was recorded as
// "Paid to Rhea Requester" and Accounts were told to pay the employee. A deposit
// is paid to the counterparty or the tender authority, so it is raised as its
// own type with a payee the requester names (phase-2 spec §3, design Figure 2).
var requestTypes = map[string]bool{
	"vendor_invoice": true, "vendor_advance": true, "reimbursement": true,
	"employee_advance": true, "recoverable": true,
}

type recoverableRule struct{ RequiresProject, RequiresCounterparty bool }

// recoverableCategoryRules is the built-in default rule set.
//
// Phase 4 moved the authority to the recoverable_categories table, which
// migration v6 seeds from exactly this map — so an admin can add a category and
// have its rules enforced without a code change (V4). The map survives as the
// seed and as the rule set the pure validator tests use;
// TestSeededCategoriesMatchPhase2Rules fails if the two ever drift.
var recoverableCategoryRules = map[string]recoverableRule{
	"emd":              {RequiresProject: true},
	"pbg":              {RequiresProject: true},
	"icd":              {RequiresCounterparty: true},
	"employee_advance": {},
	"security_deposit": {RequiresCounterparty: true},
	"other":            {},
}

// validateRequestInput stays pure: the caller supplies the recoverable rule set
// so the rules can come from the database without this function reaching for it.
func validateRequestInput(in RequestInput, rules map[string]recoverableRule) error {
	if in.Amount <= 0 || in.Amount > money.MaxAmount {
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
	// G8: self-approval is never acceptable, whatever roles the requester holds.
	// admin-configuration.html renders this as checked-and-disabled: not a setting.
	if in.RequesterID > 0 && in.ManagerID == in.RequesterID {
		return fmt.Errorf("%w: you cannot approve your own request — choose another approver", ErrValidation)
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
		rule, ok := rules[in.RecoverableCategory]
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
		// The category is the type's, not the requester's to choose: an employee
		// advance pays the person raising it, so an EMD, PBG or ICD filed under
		// this type would be a deposit recorded as owed to the employee (form-1).
		// The seeded code is the stable identity — renaming the category keeps it.
		if in.RecoverableCategory != employeeAdvanceCategory {
			return fmt.Errorf("%w: an employee advance is always in the Employee advance category — a deposit, guarantee or loan is raised as its own request type", ErrValidation)
		}
		return needsRecoverable()
	case "recoverable":
		if in.Treatment != "recoverable" {
			return fmt.Errorf("%w: recoverable type requires recoverable treatment", ErrValidation)
		}
		if in.RecoverableCategory == employeeAdvanceCategory {
			return fmt.Errorf("%w: an advance to an employee is raised as an Employee advance, which pays the person raising it", ErrValidation)
		}
		if err := needsRecoverable(); err != nil {
			return err
		}
		// Free, but not blank: this is who Accounts pays, and a request that
		// names nobody cannot be paid.
		if strings.TrimSpace(in.VendorPayee) == "" {
			return fmt.Errorf("%w: say who receives the money — the counterparty company or the authority holding the deposit", ErrValidation)
		}
	}
	return nil
}

// employeeAdvanceCategory is the code of the seeded Employee advance category.
// Codes never change (see RecoverableCategory), so it survives a rename.
const employeeAdvanceCategory = "employee_advance"

// forcesRequesterPayee reports whether the payee must equal the requester.
// These types never carry a vendor row — the payee snapshot is the only payee.
//
// `recoverable` is deliberately not here: a deposit or guarantee is paid to the
// counterparty or the tender authority the requester names (form-1).
func forcesRequesterPayee(t string) bool {
	return t == "reimbursement" || t == "employee_advance"
}

// scrubFieldsNotOwned clears the columns a request's treatment and its type do
// not own, before anything is written or validated.
//
// The form renders these fieldsets as alternatives, so no browser can send both.
// But `hidden` is not validation and neither is a `data-when` reveal: a crafted
// POST used to store the concealed half, and request_detail prints every column
// that is non-empty — so one request could read as recoverable to a person and
// behave as a budget expense to every query, or carry an invoice number on a
// reimbursement that has no invoice (F-B-15, F-B-04). Clearing here is the same
// move forcesRequesterPayee already makes for vendor_id, applied to the rest of
// the shape.
//
// It runs before validateRequestInput so the validator sees exactly what will be
// stored, and so a value that is about to be dropped cannot be refused for being
// malformed.
func scrubFieldsNotOwned(in RequestInput) RequestInput {
	// Recoverable columns belong to the treatment, not the type: an employee
	// advance is recoverable or budget depending on how it was raised.
	if in.Treatment != "recoverable" {
		in.RecoverableCategory = ""
		in.Counterparty = ""
		in.ExpectedReturnDate = ""
		in.RepaymentNotes = ""
	}
	// The rest belong to the type, and mirror validateRequestInput's per-type
	// requirements exactly — whatever a type is asked for, it keeps.
	if in.Type != "vendor_invoice" {
		in.InvoiceNo = ""
		in.InvoiceDate = ""
	}
	if in.Type != "vendor_advance" && in.Type != "employee_advance" {
		in.AdvanceReason = ""
	}
	if in.Type != "reimbursement" {
		in.ExpenseDate = ""
	}
	// A deposit or guarantee names its payee in text. The form offers no vendor
	// control for it, so a vendor_id in the body is a forged link to the vendor
	// master that would put that vendor's name over the payee the requester typed.
	if in.Type == "recoverable" {
		in.VendorID = 0
	}
	return in
}

// requireApprover refuses a manager_id that cannot decide the request.
//
// The form's approver control is built from ListApprovers, so it only ever
// offers active users holding approval:approve — and that was the only
// enforcement. Every decision route is gated on the verb **and** on
// manager_id == actor, so a request routed to anybody else can be decided by
// nobody: the named person is refused by the route gate and every real manager
// by the ownership check, leaving withdrawal as the only exit (F-A-08). The
// query is ListApprovers' own, minus the ordering and the self-exclusion
// validateRequestInput already applies.
func (s *Store) requireApprover(ctx context.Context, managerID int64) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u
 JOIN user_roles ur ON ur.user_id=u.id
 JOIN role_permissions rp ON rp.role_id=ur.role_id
 WHERE u.id=? AND u.active=1 AND rp.resource='approval' AND rp.action='approve'`, managerID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: that person cannot approve requests — choose one of the approvers offered", ErrValidation)
	}
	return nil
}

// validateRequestRefs re-checks, against the database, the ids whose only other
// guard is a narrowed <select>: the approver must be able to approve (F-A-08),
// the head must be live and must belong to the project the request names
// (F-B-03, F-B-05), and the vendor must still be active (F-B-07).
//
// The head test is deliberately ListHeads(ctx, true)'s own condition —
// `h.active=1 AND p.active=1` — because the form's list is built from exactly
// that query, and a rule enforced only by the list it populates is a rule a
// hand-rolled POST does not have to obey.
func (s *Store) validateRequestRefs(ctx context.Context, in RequestInput) error {
	if err := s.requireApprover(ctx, in.ManagerID); err != nil {
		return err
	}
	if in.HeadID > 0 {
		var projectID int64
		var headActive, projectActive int
		err := s.db.QueryRowContext(ctx, `SELECT h.project_id, h.active, p.active
 FROM heads h JOIN projects p ON p.id=h.project_id WHERE h.id=?`, in.HeadID).
			Scan(&projectID, &headActive, &projectActive)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%w: that budget head does not exist", ErrValidation)
		}
		if err != nil {
			return err
		}
		if in.ProjectID > 0 && projectID != in.ProjectID {
			// Every budget and variance figure is keyed on the head, so a request
			// that names one project and charges another project's head
			// misattributes the spend the moment it is paid.
			return fmt.Errorf("%w: that budget head belongs to a different project — choose a head under the project you named", ErrValidation)
		}
		if headActive != 1 || projectActive != 1 {
			return fmt.Errorf("%w: that budget head has been retired — choose a head that is still open", ErrValidation)
		}
	} else if in.ProjectID > 0 {
		// A recoverable's project link carries no head (recoverables-7), so the
		// head check above never sees it and the project is checked on its own:
		// the register groups recoverables by this id.
		var active int
		err := s.db.QueryRowContext(ctx, `SELECT active FROM projects WHERE id=?`, in.ProjectID).Scan(&active)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%w: that project does not exist", ErrValidation)
		}
		if err != nil {
			return err
		}
		if active != 1 {
			return fmt.Errorf("%w: that project has been retired — choose a project that is still open", ErrValidation)
		}
	}
	if in.VendorID > 0 {
		var status string
		err := s.db.QueryRowContext(ctx, `SELECT status FROM vendors WHERE id=?`, in.VendorID).Scan(&status)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%w: that vendor does not exist", ErrValidation)
		}
		if err != nil {
			return err
		}
		if status != "active" {
			// 'inactive' on a vendor is how this product says "do not pay these
			// people any more", and the vendor master is the authority for a payee.
			return fmt.Errorf("%w: that vendor is no longer active — choose an active vendor", ErrValidation)
		}
	}
	return nil
}

// urgencyMode returns the configured urgency policy: "reason" (default),
// "free" or "disabled" (admin-configuration.html · Payments · Urgency).
func (s *Store) urgencyMode(ctx context.Context) (string, error) {
	mode, err := s.AppSetting(ctx, "urgency_mode")
	if err != nil {
		return "", err
	}
	switch mode {
	case "free", "disabled", "reason":
		return mode, nil
	default:
		return "reason", nil
	}
}

// ErrUrgencyReasonRequired refuses an urgent request with no reason when the
// configuration asks for one. It is a sentinel so the request form can tie the
// message to the reason field with errors.Is instead of matching its wording.
var ErrUrgencyReasonRequired = fmt.Errorf("%w: say why this is urgent", ErrValidation)

// validateUrgency enforces G7. It is separate from validateRequestInput because
// the rule is configuration-driven and validateRequestInput stays pure.
func validateUrgency(in RequestInput, mode string) error {
	if !in.Urgent {
		return nil
	}
	switch mode {
	case "disabled":
		return fmt.Errorf("%w: urgent requests are switched off", ErrValidation)
	case "free":
		return nil
	default:
		if strings.TrimSpace(in.UrgencyReason) == "" {
			return ErrUrgencyReasonRequired
		}
	}
	return nil
}

// validateAttachmentPolicy implements G10. When attachments are compulsory the
// system never hard-blocks: it asks for a written reason, because legitimate
// documents are sometimes genuinely unavailable.
func validateAttachmentPolicy(required bool, attachmentCount int, exceptionReason string) error {
	if !required || attachmentCount > 0 {
		return nil
	}
	if strings.TrimSpace(exceptionReason) == "" {
		return fmt.Errorf("%w: attach a supporting document, or say why you cannot", ErrValidation)
	}
	return nil
}

func (s *Store) attachmentsRequired(ctx context.Context) (bool, error) {
	v, err := s.AppSetting(ctx, "require_attachments")
	return v == "1", err
}

// Duplicate invoice matches are vendor + normalized invoice + invoice year.
// They are warnings with a recorded exception, never an unoverridable ban.
func requireDuplicateReasonTx(ctx context.Context, tx *sql.Tx, in RequestInput, excludeID int64) error {
	if in.Type != "vendor_invoice" || in.VendorID <= 0 || strings.TrimSpace(in.InvoiceNo) == "" {
		return nil
	}
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_requests WHERE id<>? AND vendor_id=? AND lower(trim(invoice_no))=lower(trim(?)) AND substr(invoice_date,1,4)=substr(?,1,4) AND status NOT IN ('withdrawn','rejected','cancelled')`, excludeID, in.VendorID, in.InvoiceNo, in.InvoiceDate).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 && len(strings.TrimSpace(in.DuplicateReason)) < 10 {
		return fmt.Errorf("%w: this vendor invoice already exists; review the matching request and give an override reason of at least 10 characters", ErrValidation)
	}
	return nil
}
