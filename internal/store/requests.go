package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"fervidbudget/internal/money"
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

const requestSelect = `SELECT r.id,r.number,r.status,r.treatment,r.type,r.recoverable_category,r.recoverable_category_id,
 r.project_id,COALESCE(p.name,''),r.head_id,COALESCE(h.name,''),
 r.vendor_id,COALESCE(NULLIF(v.name,''),r.vendor_payee),COALESCE(v.gstin,''),r.vendor_payee,r.short_title,
 r.amount,r.purpose,COALESCE(r.needed_by,''),
 r.invoice_no,COALESCE(r.invoice_date,''),COALESCE(r.expense_date,''),r.advance_reason,
 r.counterparty,COALESCE(r.expected_return_date,''),r.repayment_notes,
 r.urgent,r.urgency_reason,r.attachment_exception_reason,
 r.requester_id,COALESCE(ru.name,''),r.manager_id,COALESCE(mu.name,''),
 r.approved_amount,r.approved_by,COALESCE(au.name,''),r.approved_at,
 r.decision_reason,r.cancel_reason,r.on_hold,r.hold_reason,r.processing_by,r.processing_at,
 r.reminder_last_sent,r.submitted_at,r.created_at,r.updated_at
FROM payment_requests r
LEFT JOIN projects p ON p.id=r.project_id
LEFT JOIN heads h ON h.id=r.head_id
LEFT JOIN vendors v ON v.id=r.vendor_id
JOIN users ru ON ru.id=r.requester_id
JOIN users mu ON mu.id=r.manager_id
LEFT JOIN users au ON au.id=r.approved_by`

func scanRequest(sc interface{ Scan(...any) error }) (Request, error) {
	var r Request
	var catID, projID, headID, vendorID, approvedAmt, approvedBy, processingBy sql.NullInt64
	var approvedAt, reminder, submitted, processingAt sql.NullTime
	var urgent, onHold int
	err := sc.Scan(&r.ID, &r.Number, &r.Status, &r.Treatment, &r.Type, &r.RecoverableCategory, &catID,
		&projID, &r.Project, &headID, &r.Head,
		&vendorID, &r.Vendor, &r.VendorGSTIN, &r.VendorPayee, &r.ShortTitle,
		&r.Amount, &r.Purpose, &r.NeededBy,
		&r.InvoiceNo, &r.InvoiceDate, &r.ExpenseDate, &r.AdvanceReason,
		&r.Counterparty, &r.ExpectedReturnDate, &r.RepaymentNotes,
		&urgent, &r.UrgencyReason, &r.AttachmentExceptionReason,
		&r.RequesterID, &r.RequesterName, &r.ManagerID, &r.ManagerName,
		&approvedAmt, &approvedBy, &r.ApprovedByName, &approvedAt,
		&r.DecisionReason, &r.CancelReason, &onHold, &r.HoldReason, &processingBy, &processingAt,
		&reminder, &submitted, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.Urgent = urgent == 1
	r.OnHold = onHold == 1
	for _, p := range []struct {
		src sql.NullInt64
		dst **int64
	}{{catID, &r.RecoverableCategoryID}, {projID, &r.ProjectID}, {headID, &r.HeadID},
		{vendorID, &r.VendorID}, {approvedAmt, &r.ApprovedAmount}, {approvedBy, &r.ApprovedBy},
		{processingBy, &r.ProcessingBy}} {
		if p.src.Valid {
			v := p.src.Int64
			*p.dst = &v
		}
	}
	for _, p := range []struct {
		src sql.NullTime
		dst **time.Time
	}{{approvedAt, &r.ApprovedAt}, {processingAt, &r.ProcessingAt},
		{reminder, &r.ReminderLastSent}, {submitted, &r.SubmittedAt}} {
		if p.src.Valid {
			v := p.src.Time
			*p.dst = &v
		}
	}
	return r, nil
}

func (s *Store) Request(ctx context.Context, id int64) (Request, error) {
	return scanRequest(s.db.QueryRowContext(ctx, requestSelect+` WHERE r.id=?`, id))
}

func requestInTx(ctx context.Context, tx *sql.Tx, id int64) (Request, error) {
	return scanRequest(tx.QueryRowContext(ctx, requestSelect+` WHERE r.id=?`, id))
}

// RequestAttachments lists the documents stored against a request, oldest first.
func (s *Store) RequestAttachments(ctx context.Context, requestID int64) ([]RequestAttachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,request_id,original_name,stored_path,COALESCE(mime_type,''),size_bytes,uploaded_by,created_at FROM request_attachments WHERE request_id=? ORDER BY created_at, id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RequestAttachment
	for rows.Next() {
		var a RequestAttachment
		if err := rows.Scan(&a.ID, &a.RequestID, &a.OriginalName, &a.StoredPath, &a.MimeType, &a.SizeBytes, &a.UploadedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func nullableID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

func nullableText(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// CreateRequest creates a request that is already submitted. D1: there are no
// drafts — the row, its number and its submission timestamp are written in one
// transaction, together with any file the requester staged on the form.
func (s *Store) CreateRequest(ctx context.Context, actor User, in RequestInput) (int64, error) {
	in.RequesterID = actor.ID
	if forcesRequesterPayee(in.Type) {
		in.VendorID = 0
		in.VendorPayee = actor.Name
	}
	if err := validateRequestInput(in); err != nil {
		return 0, err
	}
	mode, err := s.urgencyMode(ctx)
	if err != nil {
		return 0, err
	}
	if err := validateUrgency(in, mode); err != nil {
		return 0, err
	}
	required, err := s.attachmentsRequired(ctx)
	if err != nil {
		return 0, err
	}
	if err := validateAttachmentPolicy(required, len(in.Attachments), in.AttachmentExceptionReason); err != nil {
		return 0, err
	}
	for _, att := range in.Attachments {
		if err := validateAttachment(att); err != nil {
			return 0, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	year, err := requestNumberYear(tx, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	number, err := NextRequestNumber(tx, year)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO payment_requests
 (number,status,treatment,type,recoverable_category,recoverable_category_id,project_id,head_id,
  vendor_id,vendor_payee,short_title,amount,purpose,needed_by,invoice_no,invoice_date,expense_date,
  advance_reason,counterparty,expected_return_date,repayment_notes,urgent,urgency_reason,
  attachment_exception_reason,requester_id,manager_id,submitted_at)
 VALUES(?,'pending',?,?,?,NULL,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		number, in.Treatment, in.Type, in.RecoverableCategory,
		nullableID(in.ProjectID), nullableID(in.HeadID),
		nullableID(in.VendorID), in.VendorPayee, strings.TrimSpace(in.ShortTitle),
		in.Amount, in.Purpose, nullableText(in.NeededBy), in.InvoiceNo,
		nullableText(in.InvoiceDate), nullableText(in.ExpenseDate), in.AdvanceReason,
		in.Counterparty, nullableText(in.ExpectedReturnDate), in.RepaymentNotes,
		boolInt(in.Urgent), in.UrgencyReason, in.AttachmentExceptionReason,
		actor.ID, in.ManagerID)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, att := range in.Attachments {
		if _, err := tx.ExecContext(ctx, `INSERT INTO request_attachments(request_id,original_name,stored_path,mime_type,size_bytes,uploaded_by) VALUES(?,?,?,?,?,?)`,
			id, att.OriginalName, att.StoredPath, att.MimeType, att.SizeBytes, actor.ID); err != nil {
			return 0, classify(err)
		}
	}
	// P5 hook: notify in.ManagerID here.
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "submit", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " submitted request " + number + " for " + money.FormatPaise(in.Amount),
		After:   map[string]any{"number": number, "type": in.Type, "amount": in.Amount, "manager_id": in.ManagerID}}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// SubmitRequest resubmits a request the approver returned for correction. It is
// the only remaining transition into `pending` after creation (D1).
func (s *Store) SubmitRequest(ctx context.Context, actor User, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.RequesterID != actor.ID {
		return ErrForbidden
	}
	if !canTransition(before.Status, "pending") {
		return fmt.Errorf("%w: a %s request cannot be submitted", ErrValidation, before.Status)
	}
	required, err := s.attachmentsRequired(ctx)
	if err != nil {
		return err
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_attachments WHERE request_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if err := validateAttachmentPolicy(required, n, before.AttachmentExceptionReason); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='pending', submitted_at=CURRENT_TIMESTAMP, reminder_last_sent=NULL, updated_at=CURRENT_TIMESTAMP WHERE id=?`, id); err != nil {
		return err
	}
	after := before
	after.Status = "pending"
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "submit", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " resubmitted request " + before.Number, Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
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
			return fmt.Errorf("%w: say why this is urgent", ErrValidation)
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

// ListApprovers returns the active users who may approve a request, never
// including excludeUserID. The request form's approver control is built from
// exactly this list, so a requester's own name is never selectable (G8).
func (s *Store) ListApprovers(ctx context.Context, excludeUserID int64) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT u.id,u.email,u.name,u.password_hash,u.role,u.active,u.created_at,u.updated_at,u.default_approver_id
 FROM users u
 JOIN user_roles ur ON ur.user_id=u.id
 JOIN role_permissions rp ON rp.role_id=ur.role_id
 WHERE u.active=1 AND rp.resource='approval' AND rp.action='approve' AND u.id<>?
 ORDER BY u.name`, excludeUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// editableStatuses: D1 removed 'draft'. A request may be corrected while it is
// still pending, or after the approver returned it.
var editableStatuses = map[string]bool{"returned": true, "pending": true}

func (s *Store) UpdateRequest(ctx context.Context, actor User, id int64, in RequestInput) error {
	in.RequesterID = actor.ID
	if forcesRequesterPayee(in.Type) {
		in.VendorID = 0
		in.VendorPayee = actor.Name
	}
	if err := validateRequestInput(in); err != nil {
		return err
	}
	mode, err := s.urgencyMode(ctx)
	if err != nil {
		return err
	}
	if err := validateUrgency(in, mode); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.RequesterID != actor.ID {
		return ErrForbidden
	}
	if !editableStatuses[before.Status] {
		return fmt.Errorf("%w: a %s request cannot be edited", ErrValidation, before.Status)
	}
	// Pending edits reset the reminder timer and reroute to the chosen approver.
	// P5 hook: re-notify the (possibly new) approver here.
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET
 treatment=?, type=?, recoverable_category=?, project_id=?, head_id=?, vendor_id=?, vendor_payee=?,
 short_title=?, amount=?, purpose=?, needed_by=?, invoice_no=?, invoice_date=?, expense_date=?,
 advance_reason=?, counterparty=?, expected_return_date=?, repayment_notes=?, urgent=?,
 urgency_reason=?, attachment_exception_reason=?, manager_id=?, reminder_last_sent=NULL,
 updated_at=CURRENT_TIMESTAMP
 WHERE id=?`,
		in.Treatment, in.Type, in.RecoverableCategory, nullableID(in.ProjectID), nullableID(in.HeadID),
		nullableID(in.VendorID), in.VendorPayee, strings.TrimSpace(in.ShortTitle),
		in.Amount, in.Purpose, nullableText(in.NeededBy), in.InvoiceNo,
		nullableText(in.InvoiceDate), nullableText(in.ExpenseDate), in.AdvanceReason,
		in.Counterparty, nullableText(in.ExpectedReturnDate), in.RepaymentNotes,
		boolInt(in.Urgent), in.UrgencyReason, in.AttachmentExceptionReason, in.ManagerID, id); err != nil {
		return classify(err)
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "update", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " edited request " + before.Number, Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) WithdrawRequest(ctx context.Context, actor User, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.RequesterID != actor.ID {
		return ErrForbidden
	}
	if !canTransition(before.Status, "withdrawn") {
		return fmt.Errorf("%w: a %s request cannot be withdrawn", ErrValidation, before.Status)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='withdrawn', updated_at=CURRENT_TIMESTAMP WHERE id=?`, id); err != nil {
		return err
	}
	after := before
	after.Status = "withdrawn"
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "withdraw", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " withdrew request " + before.Number, Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ApproveRequest(ctx context.Context, actor User, id, approvedAmount int64, note string) error {
	if approvedAmount <= 0 {
		return fmt.Errorf("%w: approved amount must be positive", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	// G8: last line of defence. The form never offers it, validateRequestInput
	// rejects it, and this refuses it even if a row reached that state.
	if before.RequesterID == actor.ID {
		return ErrForbidden
	}
	if !canTransition(before.Status, "approved") {
		return fmt.Errorf("%w: a %s request cannot be approved", ErrValidation, before.Status)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='approved', approved_amount=?, approved_by=?, approved_at=CURRENT_TIMESTAMP, decision_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, approvedAmount, actor.ID, strings.TrimSpace(note), id); err != nil {
		return err
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "approve", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " approved request " + before.Number + " for " + money.FormatPaise(approvedAmount),
		Before:  before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) decideRequest(ctx context.Context, actor User, id int64, to, action, reason, missingMsg string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: %s", ErrValidation, missingMsg)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	if !canTransition(before.Status, to) {
		return fmt.Errorf("%w: a %s request cannot be %s", ErrValidation, before.Status, to)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status=?, decision_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, to, reason, id); err != nil {
		return err
	}
	after := before
	after.Status, after.DecisionReason = to, reason
	summary := map[string]string{
		"returned": " returned request ",
		"rejected": " rejected request ",
	}[to]
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: action, EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + summary + before.Number + ": " + reason, Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReturnRequest(ctx context.Context, actor User, id int64, comment string) error {
	return s.decideRequest(ctx, actor, id, "returned", "return", comment, "a comment is required to return a request")
}

func (s *Store) RejectRequest(ctx context.Context, actor User, id int64, reason string) error {
	return s.decideRequest(ctx, actor, id, "rejected", "reject", reason, "a reason is required to reject a request")
}

func (s *Store) ReassignRequest(ctx context.Context, actor User, id, newManagerID int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a reason is required to reassign", ErrValidation)
	}
	if newManagerID <= 0 {
		return fmt.Errorf("%w: choose an approver to reassign to", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.Status != "pending" {
		return fmt.Errorf("%w: only a pending request can be reassigned", ErrValidation)
	}
	// G8 holds for reassignment too.
	if newManagerID == before.RequesterID {
		return fmt.Errorf("%w: a request cannot be reassigned to its own requester", ErrValidation)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET manager_id=?, decision_reason=?, reminder_last_sent=NULL, updated_at=CURRENT_TIMESTAMP WHERE id=?`, newManagerID, reason, id); err != nil {
		return classify(err)
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "reassign", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " reassigned request " + before.Number + " to " + after.ManagerName + ": " + reason,
		Before:  before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

// ReraiseRequest copies a rejected request into a new one. D1: the copy is
// created already pending with its own number — there is no draft to land in.
func (s *Store) ReraiseRequest(ctx context.Context, actor User, id int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	src, err := requestInTx(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	if src.RequesterID != actor.ID {
		return 0, ErrForbidden
	}
	if src.Status != "rejected" {
		return 0, fmt.Errorf("%w: only a rejected request can be re-raised", ErrValidation)
	}
	year, err := requestNumberYear(tx, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	number, err := NextRequestNumber(tx, year)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO payment_requests
 (number,status,treatment,type,recoverable_category,recoverable_category_id,project_id,head_id,
  vendor_id,vendor_payee,short_title,amount,purpose,needed_by,invoice_no,invoice_date,expense_date,
  advance_reason,counterparty,expected_return_date,repayment_notes,urgent,urgency_reason,
  attachment_exception_reason,requester_id,manager_id,submitted_at)
 SELECT ?, 'pending', treatment, type, recoverable_category, recoverable_category_id, project_id, head_id,
  vendor_id, vendor_payee, short_title, amount, purpose, needed_by, invoice_no, invoice_date, expense_date,
  advance_reason, counterparty, expected_return_date, repayment_notes, urgent, urgency_reason,
  attachment_exception_reason, requester_id, manager_id, CURRENT_TIMESTAMP
 FROM payment_requests WHERE id=?`, number, id)
	if err != nil {
		return 0, classify(err)
	}
	newID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "reraise", EntityType: "payment_request", EntityID: &newID,
		Summary: actor.Name + " re-raised " + src.Number + " as " + number,
		After:   map[string]any{"source": src.Number, "number": number}}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newID, nil
}

// RequestCancellation is G1: the requester asks for an approved request to be
// cancelled. Payment freezes the moment this succeeds, because the request is
// no longer in the `approved` state that Accounts reserves from.
func (s *Store) RequestCancellation(ctx context.Context, actor User, id int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: say why it should be cancelled", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.RequesterID != actor.ID {
		return ErrForbidden
	}
	if !canTransition(before.Status, "cancellation_requested") {
		return fmt.Errorf("%w: a %s request cannot be sent for cancellation", ErrValidation, before.Status)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='cancellation_requested', cancel_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, reason, id); err != nil {
		return err
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	// P5 hook: notify the approver, and any accountant holding a reservation.
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "cancel_request", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " asked for cancellation of " + before.Number + ": " + reason + ". Payment frozen.",
		Before:  before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

// DecideCancellation is the approver's answer to G1. Accepting closes the
// request permanently; declining unfreezes it back to approved and requires a
// written reason, because Accounts and the requester both read it.
func (s *Store) DecideCancellation(ctx context.Context, actor User, id int64, accept bool, note string) error {
	note = strings.TrimSpace(note)
	if !accept && note == "" {
		return fmt.Errorf("%w: say why it should still be paid", ErrValidation)
	}
	// Accepting a cancellation *is* cancelling the request, so it lands on the
	// thread under the same `cancel` action as an outright cancel — only the
	// summary says who asked for it. A separate `cancel_accept` action would
	// split one event across two names for no reader's benefit.
	to, action := "cancelled", "cancel"
	if !accept {
		to, action = "approved", "cancel_decline"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	if before.Status != "cancellation_requested" || !canTransition(before.Status, to) {
		return fmt.Errorf("%w: there is no cancellation to decide on a %s request", ErrValidation, before.Status)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status=?, decision_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, to, note, id); err != nil {
		return err
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	summary := actor.Name + " cancelled " + before.Number + " at the requester's asking"
	if !accept {
		summary = actor.Name + " declined the cancellation of " + before.Number + ": " + note + ". Payment unfrozen."
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: action, EntityType: "payment_request", EntityID: &id,
		Summary: summary, Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

// CancelRequest is G2: the approver cancels an approved request outright,
// without the requester having asked. A reason is always required.
func (s *Store) CancelRequest(ctx context.Context, actor User, id int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a reason is required to cancel a request", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	if !canTransition(before.Status, "cancelled") {
		return fmt.Errorf("%w: a %s request cannot be cancelled", ErrValidation, before.Status)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='cancelled', cancel_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, reason, id); err != nil {
		return err
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "cancel", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " cancelled request " + before.Number + ": " + reason,
		Before:  before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}
