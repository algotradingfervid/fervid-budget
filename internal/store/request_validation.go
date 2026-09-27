// request_validation.go checks a request form before it is written: the pure
// field rules (validateRequestInput), clearing fields the chosen type does not
// use, database checks on the ids the form names (approver, project, head,
// vendor), the urgency and attachment policies, and the duplicate-invoice
// guard. prepareRequestInput runs them in one fixed order for both a new
// request and an edit, so the two can never validate differently.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"fervidbudget/internal/money"
)

// requestTypes are the request types validateRequestInput accepts. The
// `recoverable` type is a deposit or guarantee (EMD, PBG, ICD, a security
// deposit). It is a type of its own because of its payee: an employee advance
// always pays the person raising it (forcesRequesterPayee), while a deposit is
// paid to the counterparty or tender authority the requester names.
var requestTypes = map[string]bool{
	"vendor_invoice": true, "vendor_advance": true, "reimbursement": true,
	"employee_advance": true, "recoverable": true,
}

// budgetOnlyTypes are the types that are always a budget expense against a
// project head. The value names the type in the refusal message.
var budgetOnlyTypes = map[string]string{
	"vendor_invoice": "a vendor invoice",
	"vendor_advance": "a vendor advance",
	"reimbursement":  "reimbursement",
}

// recoverableRule is what one recoverable category demands of a request.
type recoverableRule struct{ RequiresProject, RequiresCounterparty bool }

// recoverableCategoryRules is the built-in default rule set. The live rules
// come from the recoverable_categories table (see recoverableRules), which the
// migration seeds from this map; the map also serves the pure validator tests.
// TestSeededCategoriesMatchPhase2Rules fails if the two ever drift.
var recoverableCategoryRules = map[string]recoverableRule{
	"emd":              {RequiresProject: true},
	"pbg":              {RequiresProject: true},
	"icd":              {RequiresCounterparty: true},
	"employee_advance": {},
	"security_deposit": {RequiresCounterparty: true},
	"other":            {},
}

// validateRequestInput checks a request form's own fields: amount, title,
// purpose, approver (never the requester), treatment, type, date formats, and
// then what each type requires (project and head, vendor, invoice details,
// recoverable details). It reads no database; the caller passes the
// recoverable category rules so they can come from the table.
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
	// Nobody may approve their own request, whatever roles they hold. This is
	// a fixed rule, not a setting.
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
	if noun, ok := budgetOnlyTypes[in.Type]; ok {
		if in.Treatment != "budget" {
			return fmt.Errorf("%w: %s is a budget expense", ErrValidation, noun)
		}
		if err := needsProjectHead(); err != nil {
			return err
		}
	}
	switch in.Type {
	case "vendor_invoice":
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
		if err := needsVendor(); err != nil {
			return err
		}
		if strings.TrimSpace(in.AdvanceReason) == "" {
			return fmt.Errorf("%w: say what the advance is for", ErrValidation)
		}
	case "reimbursement":
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
		// An employee advance pays the person raising it, so its category is
		// fixed: an EMD, PBG or ICD filed under this type would be a deposit
		// recorded as owed to the employee. The seeded category code is the
		// stable identity, so renaming the category keeps this rule working.
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
		// The payee is free text but may not be blank: it is who Accounts pays.
		if strings.TrimSpace(in.VendorPayee) == "" {
			return fmt.Errorf("%w: say who receives the money — the counterparty company or the authority holding the deposit", ErrValidation)
		}
	}
	return nil
}

// employeeAdvanceCategory is the code of the seeded Employee advance category.
// Codes never change (see RecoverableCategory), so it survives a rename.
const employeeAdvanceCategory = "employee_advance"

// forcesRequesterPayee reports whether the payee must be the requester. These
// types never carry a vendor row; the payee name is the only payee. The
// `recoverable` type is not one of them: a deposit or guarantee is paid to the
// counterparty or tender authority the requester names.
func forcesRequesterPayee(t string) bool {
	return t == "reimbursement" || t == "employee_advance"
}

// scrubFieldsNotOwned clears the fields that the request's treatment and type
// do not use, before anything is validated or written.
//
// The form shows these fields as alternatives, but hiding a field is not
// validation: a crafted POST could otherwise store both halves, and the detail
// page prints every non-empty column, so one request could read as recoverable
// while every query treats it as a budget expense. Clearing first also means the
// validator sees exactly what will be stored, and a value about to be dropped
// cannot be refused for being malformed.
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

// requireApprover refuses an approver who could not decide the request: the
// person must be active and hold approval:approve. The form only offers such
// people (ListApprovers), but a hand-made POST could name anybody, and a
// request routed to someone who cannot approve it would be stuck for good.
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

// validateRequestRefs re-checks against the database the ids that the form
// only narrows with a <select>: the approver must be able to approve, the head
// must be open and belong to the project the request names (or, with no head,
// the project must be open), and the vendor must still be active. The head test
// uses the same condition as ListHeads(ctx, true), which builds the form's list.
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
		// A recoverable may link a project without a head, so the project is
		// checked on its own: the register groups recoverables by it.
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
			// An inactive vendor is one the business has decided not to pay.
			return fmt.Errorf("%w: that vendor is no longer active — choose an active vendor", ErrValidation)
		}
	}
	return nil
}

// prepareRequestInput normalises and validates a request form for both a new
// request and an edit, in one fixed order: the requester is the actor, types
// that pay the requester get the actor as payee, unused fields are cleared,
// then the field rules, the database references and the urgency policy are
// checked. It returns the cleaned input and the recoverable category's row id
// (nil when the request is not recoverable). It needs no transaction.
func (s *Store) prepareRequestInput(ctx context.Context, actor User, in RequestInput) (RequestInput, any, error) {
	in.RequesterID = actor.ID
	if forcesRequesterPayee(in.Type) {
		in.VendorID = 0
		in.VendorPayee = actor.Name
	}
	in = scrubFieldsNotOwned(in)
	rules, err := s.recoverableRules(ctx)
	if err != nil {
		return in, 0, err
	}
	if err := validateRequestInput(in, rules); err != nil {
		return in, 0, err
	}
	if err := s.validateRequestRefs(ctx, in); err != nil {
		return in, 0, err
	}
	categoryID, err := s.recoverableCategoryLink(ctx, in.Treatment, in.RecoverableCategory)
	if err != nil {
		return in, 0, err
	}
	mode, err := s.urgencyMode(ctx)
	if err != nil {
		return in, 0, err
	}
	if err := validateUrgency(in, mode); err != nil {
		return in, 0, err
	}
	return in, categoryID, nil
}

// urgencyMode returns the configured urgency policy: "reason" (the default:
// an urgent request must say why), "free" (no reason needed) or "disabled"
// (nothing may be marked urgent).
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

// validateUrgency applies the urgency policy to an urgent request. It is kept
// apart from validateRequestInput because the policy is a setting, and
// validateRequestInput reads no database.
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

// validateAttachmentPolicy applies the "attachments required" setting. It never
// blocks outright: with no document the requester must give a written reason,
// because a real document is sometimes genuinely unavailable.
func validateAttachmentPolicy(required bool, attachmentCount int, exceptionReason string) error {
	if !required || attachmentCount > 0 {
		return nil
	}
	if strings.TrimSpace(exceptionReason) == "" {
		return fmt.Errorf("%w: attach a supporting document, or say why you cannot", ErrValidation)
	}
	return nil
}

// attachmentsRequired reports whether the "attachments required" setting is on.
func (s *Store) attachmentsRequired(ctx context.Context) (bool, error) {
	v, err := s.AppSetting(ctx, "require_attachments")
	return v == "1", err
}

// requireDuplicateReasonTx refuses a vendor invoice that matches another live
// request (same vendor, same invoice number ignoring case and surrounding spaces, same
// invoice year) unless the requester gives an override reason of at least 10
// characters. A duplicate is a warning with a recorded reason, never a ban.
// excludeID is the request being edited (0 for a new one). The check counts
// every request, not only those the requester may see, but reveals none of them.
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
