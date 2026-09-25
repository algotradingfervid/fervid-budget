package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"fervidbudget/internal/money"
)

// beginWriteTx opens a transaction that is a writer from its first statement.
//
// This is not decoration. SQLite will not upgrade a transaction that already
// holds a read snapshot into a writer while another writer is active, and it
// deliberately does **not** consult the busy handler for that upgrade — retrying
// could only deadlock — so `_pragma=busy_timeout(5000)` on the DSN never applies
// to it and the loser fails instantly with SQLITE_BUSY. Every writer in this
// file used to be shaped BeginTx → read → write, which is exactly that case:
// two simultaneous submits, or two simultaneous decisions, handed the loser a
// 500 and threw their form away (F-B-10, F-C-05).
//
// Taking the write lock first is the way out, and it is the `BEGIN IMMEDIATE`
// the driver can only express per-DSN (modernc.org/sqlite's `_txlock`), not per
// call site. The statement below matches no row and changes nothing: SQLite
// acquires the database write lock when a write statement starts, before it
// filters rows, so this reserves the lock — and because the transaction holds no
// read snapshot yet, the busy handler *does* apply and a second writer waits its
// turn instead of erroring.
//
// Writers still guard their UPDATE with the state they expect and check
// RowsAffected, the shape ReserveRequest uses (store.go:751): the lock decides
// who goes first, the condition decides whether going second still makes sense.
func (s *Store) beginWriteTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE request_number_seq SET last=last WHERE 1=0`); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// ErrRequestRaced is the loser of a write race: the row moved between the read
// that authorised this call and the conditional UPDATE that would have applied
// it. It is a validation error, not a server error, so the reader is told the
// request was already decided instead of meeting an internal-error page
// (F-B-10, F-C-05).
var ErrRequestRaced = fmt.Errorf("%w: somebody else changed this request a moment ago — reload it and look again", ErrValidation)

// affectedOne turns a conditional UPDATE's row count into the race refusal.
func affectedOne(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrRequestRaced
	}
	return nil
}

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

// requestTypes is what validateRequestInput accepts. There are five, and the
// fifth is the divergence F-D-14 reports: the product's type chooser
// (`requestTypeOptions`, internal/app/requests.go) offers four cards, and
// `/requests/new?type=recoverable` therefore falls back to the chooser with no
// form behind it.
//
// The type is kept, and its validation branch with it, because it is NOT dead
// code: `POST /requests` reaches the store with whatever type the body carried,
// so a `recoverable`-typed request is creatable today by anything that is not
// the chooser — and the branch below is what makes such a submission be refused
// for its real reason (the category's project rule) rather than accepted
// unvalidated or refused for a type the store had just accepted. That is the
// behaviour F-E-08's fix depends on, and both `internal/app` and the audit
// suites drive it directly.
//
// So the reconciliation F-D-14 asks for belongs on the other side of the line:
// either the chooser grows a fifth card or the handler refuses the type before
// the store sees it. Neither is this file's to make. Reported, not fixed here.
var requestTypes = map[string]bool{
	"vendor_invoice": true, "vendor_advance": true, "reimbursement": true,
	"employee_advance": true, "recoverable": true,
}

// requestStatuses is the complete status enum — every value that can appear in
// payment_requests.status, in workflow order (F-C-08).
//
// It listed seven until the 2026-07-27 audit, on the strength of a comment
// saying "Phase 3 appends" the rest. Phase 3 shipped the four extra statuses and
// never came back for the comment, so a reader consulting the enum for the set
// of states this system has was told four of the eleven do not exist —
// including both terminal ones.
//
// Two things are deliberately NOT here:
//
//   - 'draft'. D1: a request exists only once submitted, and
//     migrations.go's CHECK (status <> 'draft') makes the value unrepresentable
//     for the life of the table, not merely unused.
//   - 'on_hold'. The old comment named it as a status Phase 3 would append. It
//     is not a status and never was: it is a boolean column that qualifies
//     'approved', which is exactly the confusion the activeHold predicate and
//     the "on_hold implies approved" invariant exist to prevent. A request on
//     hold has status 'approved'.
//
// docs/superpowers/specs/2026-07-25-payment-requests-overview.md still describes
// nine statuses and omits cancellation_requested, cancelled and
// completed_partial. This map is the authority; the document is the thing that
// is wrong, and correcting it is not this file's to do.
var requestStatuses = map[string]bool{
	"pending": true, "returned": true, "approved": true, "rejected": true,
	"withdrawn": true, "cancellation_requested": true, "cancelled": true,
	// Phase 3, the settlement half.
	"processing": true, "partial_review": true,
	"completed": true, "completed_partial": true,
}

var legalTransitions = map[string]map[string]bool{
	"returned": {"pending": true},
	"pending":  {"approved": true, "returned": true, "rejected": true, "withdrawn": true},
	// G1/G2: post-approval cancellation. An approved request may be frozen by
	// the requester (cancellation_requested) or cancelled outright by a manager.
	"approved": {"cancellation_requested": true, "cancelled": true},
	// The edge back to 'approved' belongs to DecideCancellation(accept=false)
	// alone — that path is gated on approval:cancel, demands a written reason and
	// records `cancel_decline`. ApproveRequest used to borrow it through
	// canTransition and so lifted the requester's freeze with none of the three
	// (F-C-03); it now tests for 'pending' itself.
	"cancellation_requested": {"cancelled": true, "approved": true},
	// Phase 3 (G14): a manager-accepted partial closes distinctly from a clean pay.
	// "completed_partial" has no outgoing edges — terminal, like "completed".
	//
	// F-C-08: the edge to "completed" used to be declared here and no code could
	// traverse it. Only two writers produce a completed state and neither starts
	// from partial_review — RecordPaymentForRequest writes 'completed' WHERE
	// status='processing', and AcceptPartial writes 'completed_partial' WHERE
	// status='partial_review'. A shortfall the manager accepts closes as
	// completed_partial precisely so that the difference between "paid in full"
	// and "balance written off" survives in the record, so an edge that would
	// erase it is not one to leave lying about for a future caller of
	// decideRequest to reach for. Removed rather than implemented.
	"partial_review": {"completed_partial": true},
}

func canTransition(from, to string) bool {
	return legalTransitions[from][to]
}

// statusPhrase names a request in a status the way a refusal reads it: with
// its article and in plain words. The transition refusals used to drop the raw
// status code after a fixed "a", which produced "a approved request cannot be
// withdrawn" and "a completed_partial request cannot be returned" — a code
// nobody outside this package speaks, and a grammar nobody speaks at all.
func statusPhrase(status string) string {
	switch status {
	case "approved":
		return "an approved request"
	case "cancellation_requested":
		return "a request awaiting a cancellation decision"
	case "processing":
		return "a request with Accounts"
	case "partial_review":
		return "a request in partial review"
	case "completed_partial":
		return "a completed (partial accepted) request"
	}
	return "a " + strings.ReplaceAll(status, "_", " ") + " request"
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
	in = scrubFieldsNotOwned(in)
	rules, err := s.recoverableRules(ctx)
	if err != nil {
		return 0, err
	}
	if err := validateRequestInput(in, rules); err != nil {
		return 0, err
	}
	if err := s.validateRequestRefs(ctx, in); err != nil {
		return 0, err
	}
	categoryID, err := s.recoverableCategoryLink(ctx, in.Treatment, in.RecoverableCategory)
	if err != nil {
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
	// A writer from its first statement: the numbering settings are read inside
	// this transaction, and a deferred transaction that reads before it writes
	// cannot be upgraded while another submit is in flight (F-B-10).
	tx, err := s.beginWriteTx(ctx)
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
 VALUES(?,'pending',?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		number, in.Treatment, in.Type, in.RecoverableCategory, categoryID,
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
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.submitRequestTx(ctx, tx, actor, id); err != nil {
		return err
	}
	return tx.Commit()
}

// submitRequestTx is SubmitRequest's body without the transaction, so that
// UpdateAndResubmit can run the edit and the submit as one unit (F-G-035). It
// re-reads the row through tx, which is what makes composition safe: called after
// updateRequestTx it validates the *edited* row, not the row as it was on entry.
func (s *Store) submitRequestTx(ctx context.Context, tx *sql.Tx, actor User, id int64) error {
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.RequesterID != actor.ID {
		return ErrForbidden
	}
	if !canTransition(before.Status, "pending") {
		return fmt.Errorf("%w: %s cannot be submitted", ErrValidation, statusPhrase(before.Status))
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
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='pending', submitted_at=CURRENT_TIMESTAMP, reminder_last_sent=NULL, updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND status=? AND requester_id=?`, id, before.Status, actor.ID)
	if err != nil {
		return err
	}
	if err := affectedOne(res); err != nil {
		return err
	}
	after := before
	after.Status = "pending"
	return recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "submit", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " resubmitted request " + before.Number, Before: before, After: after})
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
	in, categoryID, err := s.prepareRequestUpdate(ctx, actor, in)
	if err != nil {
		return err
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.updateRequestTx(ctx, tx, actor, id, in, categoryID); err != nil {
		return err
	}
	return tx.Commit()
}

// RequestEdit is everything one press of the correction form does: the edited
// fields, an optional document that arrived with them, and whether the press was
// "Save corrections" or "Save and resubmit".
type RequestEdit struct {
	Input      RequestInput
	Attachment *AttachmentInput // nil when the form carried no file
	Resubmit   bool
}

// EditRequest applies one press of the correction form as a single transaction
// (F-G-035, F-C-04).
//
// The handler used to call UpdateRequest, then AddRequestAttachment, then
// SubmitRequest — three transactions for one button. A resubmit that failed its
// own preconditions therefore left the edit written *and audited as "edited and
// re-sent"* while the request sat in `returned`: the history asserted a change the
// requester had just been told did not happen. Wave 4 hoisted the submit
// preconditions ahead of the write, which closed the observable defect but left a
// window — the row can change between calls, and only one call held a transaction
// at a time.
//
// Ordering matters and is the reason the attachment belongs in here rather than
// around the outside: the attachment policy counts rows in `request_attachments`,
// so the document must be inserted before the submit is validated, or a request
// whose only document is the one being uploaded is refused. Every audit row — edit,
// attach, submit — commits or rolls back together.
func (s *Store) EditRequest(ctx context.Context, actor User, id int64, e RequestEdit) error {
	in, categoryID, err := s.prepareRequestUpdate(ctx, actor, e.Input)
	if err != nil {
		return err
	}
	if e.Attachment != nil {
		if err := validateAttachment(*e.Attachment); err != nil {
			return err
		}
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.updateRequestTx(ctx, tx, actor, id, in, categoryID); err != nil {
		return err
	}
	if e.Attachment != nil {
		if _, err := addRequestAttachmentTx(ctx, tx, actor, id, *e.Attachment); err != nil {
			return err
		}
	}
	if e.Resubmit {
		// Reads the edited row, so the transition and the attachment policy are
		// checked against what was just saved rather than what was there before.
		if err := s.submitRequestTx(ctx, tx, actor, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// prepareRequestUpdate runs every check that needs no transaction and returns the
// scrubbed input plus the resolved recoverable category id. Shared so the edit and
// the edit-and-resubmit paths cannot validate differently.
func (s *Store) prepareRequestUpdate(ctx context.Context, actor User, in RequestInput) (RequestInput, any, error) {
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
	// Same gate on the edit path: rerouting a request to a non-approver by
	// editing it strands it exactly as raising it that way does (F-A-08).
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

func (s *Store) updateRequestTx(ctx context.Context, tx *sql.Tx, actor User, id int64, in RequestInput, categoryID any) error {
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.RequesterID != actor.ID {
		return ErrForbidden
	}
	if !editableStatuses[before.Status] {
		return fmt.Errorf("%w: %s cannot be edited", ErrValidation, statusPhrase(before.Status))
	}
	// Pending edits reset the reminder timer and reroute to the chosen approver.
	// P5 hook: re-notify the (possibly new) approver here.
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET
 treatment=?, type=?, recoverable_category=?, recoverable_category_id=?, project_id=?, head_id=?, vendor_id=?, vendor_payee=?,
 short_title=?, amount=?, purpose=?, needed_by=?, invoice_no=?, invoice_date=?, expense_date=?,
 advance_reason=?, counterparty=?, expected_return_date=?, repayment_notes=?, urgent=?,
 urgency_reason=?, attachment_exception_reason=?, manager_id=?, reminder_last_sent=NULL,
 updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND status=? AND requester_id=?`,
		in.Treatment, in.Type, in.RecoverableCategory, categoryID, nullableID(in.ProjectID), nullableID(in.HeadID),
		nullableID(in.VendorID), in.VendorPayee, strings.TrimSpace(in.ShortTitle),
		in.Amount, in.Purpose, nullableText(in.NeededBy), in.InvoiceNo,
		nullableText(in.InvoiceDate), nullableText(in.ExpenseDate), in.AdvanceReason,
		in.Counterparty, nullableText(in.ExpectedReturnDate), in.RepaymentNotes,
		boolInt(in.Urgent), in.UrgencyReason, in.AttachmentExceptionReason, in.ManagerID,
		id, before.Status, actor.ID)
	if err != nil {
		return classify(err)
	}
	if err := affectedOne(res); err != nil {
		return err
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	return recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "update", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " edited request " + before.Number, Before: before, After: after})
}

func (s *Store) WithdrawRequest(ctx context.Context, actor User, id int64) error {
	tx, err := s.beginWriteTx(ctx)
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
		return fmt.Errorf("%w: %s cannot be withdrawn", ErrValidation, statusPhrase(before.Status))
	}
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='withdrawn', updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND status=? AND requester_id=?`, id, before.Status, actor.ID)
	if err != nil {
		return err
	}
	if err := affectedOne(res); err != nil {
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

// ApproveRequest records the approver's decision. Two rules make it narrower
// than the transition table alone: the decision is legal from 'pending' and from
// nowhere else (F-C-03), and the approved amount — which is the ceiling Accounts
// may pay to (G13) — may be reduced and never raised (F-C-01).
func (s *Store) ApproveRequest(ctx context.Context, actor User, id, approvedAmount int64, note string) error {
	if approvedAmount <= 0 {
		return fmt.Errorf("%w: approved amount must be positive", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
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
	// F-C-03: not canTransition. legalTransitions also carries
	// cancellation_requested → approved, and that edge belongs to
	// DecideCancellation(accept=false) alone — the path that is gated on
	// approval:cancel, requires a written reason and writes `cancel_decline`,
	// because the requester and Accounts both read it. Approving from the frozen
	// state lifted the freeze with none of those, rewrote approved_amount,
	// re-stamped approved_at and left a stale cancel_reason on an approved row
	// that no screen renders — so the override was invisible to the person who
	// asked for the cancellation.
	if before.Status == "cancellation_requested" {
		return fmt.Errorf("%w: this request is waiting on your cancellation decision — decide that first, and declining it is what returns it to approved", ErrValidation)
	}
	if before.Status != "pending" {
		return fmt.Errorf("%w: %s cannot be approved", ErrValidation, statusPhrase(before.Status))
	}
	// F-C-01: the approved amount is not a note, it is the authority to pay —
	// G13 caps a payment at it (store.go:900–910). Approving above the request
	// therefore raises that ceiling above what anybody asked for, on one person's
	// signature, and the audit trail cannot tell it from an ordinary approval.
	// Downwards is the adjustment the approve sheet offers ("You may approve a
	// smaller amount than was asked for"); upwards is a different obligation, and
	// the answer to that is a new request.
	if approvedAmount > before.Amount {
		return fmt.Errorf("%w: you cannot approve more than the %s that was requested — approve up to that amount, or cancel this request and ask for a new one", ErrValidation, money.FormatPaise(before.Amount))
	}
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='approved', approved_amount=?, approved_by=?, approved_at=CURRENT_TIMESTAMP, decision_reason=?, updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND status='pending' AND manager_id=?`, approvedAmount, actor.ID, strings.TrimSpace(note), id, actor.ID)
	if err != nil {
		return err
	}
	if err := affectedOne(res); err != nil {
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
	tx, err := s.beginWriteTx(ctx)
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
		return fmt.Errorf("%w: %s cannot be %s", ErrValidation, statusPhrase(before.Status), to)
	}
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status=?, decision_reason=?, updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND status=? AND manager_id=?`, to, reason, id, before.Status, actor.ID)
	if err != nil {
		return err
	}
	if err := affectedOne(res); err != nil {
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

// reassignableStatuses are the states in which a request is somebody's to
// decide, and so can be handed to somebody else. They are exactly the states
// RequestsAwaitingApprover warns about when that person is deactivated
// (approverOpenStatuses): the warning tells the administrator to reassign each
// one, so each one has to be reassignable, or the way out it promises is a
// dead end for a frozen cancellation or a partial review (F-G-025).
var reassignableStatuses = map[string]bool{
	"pending": true, "returned": true, "cancellation_requested": true, "partial_review": true,
}

// Reassignable reports whether ReassignRequest would move a request in this
// status, so a screen can withhold the control where the POST would refuse.
func Reassignable(status string) bool { return reassignableStatuses[status] }

// ReassignRequest hands a request to a different approver. Who may call it is
// the caller's question — the request's own approver, or an administrator
// rescuing one whose approver cannot act — but one rule is the store's, whoever
// the actor is: nobody hands a request to themselves. A manager who could not
// decide a request must not be able to make it theirs and then decide it (A5).
func (s *Store) ReassignRequest(ctx context.Context, actor User, id, newManagerID int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a reason is required to reassign", ErrValidation)
	}
	if newManagerID <= 0 {
		return fmt.Errorf("%w: choose an approver to reassign to", ErrValidation)
	}
	if newManagerID == actor.ID {
		return fmt.Errorf("%w: you cannot reassign a request to yourself", ErrValidation)
	}
	// Reassignment is F-A-08's recovery path, so it must not be a way back into
	// the same hole: the new approver has to be able to approve.
	if err := s.requireApprover(ctx, newManagerID); err != nil {
		return err
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if !reassignableStatuses[before.Status] {
		return fmt.Errorf("%w: %s cannot be reassigned", ErrValidation, statusPhrase(before.Status))
	}
	// G8 holds for reassignment too.
	if newManagerID == before.RequesterID {
		return fmt.Errorf("%w: a request cannot be reassigned to its own requester", ErrValidation)
	}
	// decision_reason is the reassignment's note only while the request is
	// pending. On a returned request it is the correction the requester is
	// reading, and on a frozen or partial one it is the approval note — the
	// reason is in the audit row either way, so nothing is lost by leaving them.
	reasonSQL := `decision_reason=?, `
	args := []any{newManagerID, reason, id, before.Status}
	if before.Status != "pending" {
		reasonSQL = ``
		args = []any{newManagerID, id, before.Status}
	}
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET manager_id=?, `+reasonSQL+`reminder_last_sent=NULL, updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND status=?`, args...)
	if err != nil {
		return classify(err)
	}
	if err := affectedOne(res); err != nil {
		return err
	}
	after, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		// Not "reassign": reservation reassignment writes that action on this
		// same entity type, and one name for two different events makes an
		// approver swap read as somebody taking over the payment in every
		// trail that filters on it.
		Action: "approval_reassign", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " reassigned request " + before.Number + " to " + after.ManagerName + ": " + reason,
		Before:  before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

// ReraiseRequest copies a rejected request into a new one. D1: the copy is
// created already pending with its own number — there is no draft to land in.
func (s *Store) ReraiseRequest(ctx context.Context, actor User, id int64) (int64, error) {
	// Reads the source row and the numbering settings before it writes, so it
	// needs the write lock up front for the same reason CreateRequest does.
	tx, err := s.beginWriteTx(ctx)
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
 FROM payment_requests WHERE id=? AND status='rejected' AND requester_id=?`, number, id, actor.ID)
	if err != nil {
		return 0, classify(err)
	}
	if err := affectedOne(res); err != nil {
		return 0, err
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
	tx, err := s.beginWriteTx(ctx)
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
		return fmt.Errorf("%w: %s cannot be sent for cancellation", ErrValidation, statusPhrase(before.Status))
	}
	// The hold is suspended by the freeze, not destroyed by it (F-C-07).
	//
	// `on_hold=1` still implies `status='approved'` — that invariant is what lets
	// the hold tab, the hold pill and ReserveRequest all trust one column — so the
	// flag is cleared here. `hold_reason` is deliberately **not**: it is the
	// accountant's unanswered question, and it is what DecideCancellation reads to
	// restore the pause when the cancellation is declined. Clearing both used to
	// cost requirement L7, "On hold (only Accounts lifts)": a requester asking for
	// cancellation and an approver declining it lifted an accountant's hold
	// between them, and the request came back genuinely re-reservable with the
	// question still unanswered.
	//
	// A non-empty hold_reason with on_hold=0 is never rendered as a hold: every
	// reader of HoldReason is gated on OnHold or on activeHold, so the frozen
	// request shows the cancellation banner and only that. UnholdRequest requires
	// on_hold=1, so nobody can quietly discard the suspended question either — it
	// comes back with the request, and Accounts lifts it or does not.
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='cancellation_requested', cancel_reason=?, on_hold=0, updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND status='approved' AND requester_id=?`, reason, id, actor.ID)
	if err != nil {
		return err
	}
	if err := affectedOne(res); err != nil {
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
	// The hold's fate follows the decision (F-C-07). Accepting kills the request,
	// and nothing may still be "on hold" on a dead row — the hold tab would keep
	// listing it and offering "Read reply" on a request no reply can change, so
	// both columns go. Declining returns the request to 'approved', which is the
	// one status a hold may describe, so the pause RequestCancellation suspended
	// comes back: only Accounts lifts a hold (L7), and neither the requester who
	// asked for the cancellation nor the approver who refused it has answered the
	// accountant's question. hold_reason is the record that there was one.
	holdSQL := `, on_hold=0, hold_reason=''`
	if !accept {
		holdSQL = `, on_hold=CASE WHEN COALESCE(hold_reason,'') <> '' THEN 1 ELSE 0 END`
	}
	tx, err := s.beginWriteTx(ctx)
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
		return fmt.Errorf("%w: there is no cancellation to decide on %s", ErrValidation, statusPhrase(before.Status))
	}
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status=?, decision_reason=?`+holdSQL+`, updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND status='cancellation_requested' AND manager_id=?`, to, note, id, actor.ID)
	if err != nil {
		return err
	}
	if err := affectedOne(res); err != nil {
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
	tx, err := s.beginWriteTx(ctx)
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
		return fmt.Errorf("%w: %s cannot be cancelled", ErrValidation, statusPhrase(before.Status))
	}
	// A cancelled request is dead, so nothing may still be "on hold" on it: the
	// hold tab would keep listing it and offering "Read reply" on a request no
	// reply can change.
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='cancelled', cancel_reason=?, on_hold=0, hold_reason='', updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND status=? AND manager_id=?`, reason, id, before.Status, actor.ID)
	if err != nil {
		return err
	}
	if err := affectedOne(res); err != nil {
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

func (s *Store) AddRequestComment(ctx context.Context, actor User, requestID int64, body string) (int64, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return 0, fmt.Errorf("%w: comment cannot be empty", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := requestInTx(ctx, tx, requestID); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO request_comments(request_id,author_id,body) VALUES(?,?,?)`, requestID, actor.ID, body)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "comment", EntityType: "payment_request", EntityID: &requestID,
		Summary: "Commented on request", After: map[string]any{"comment_id": id}}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) RequestComments(ctx context.Context, requestID int64) ([]RequestComment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.request_id,c.author_id,COALESCE(u.name,''),c.body,c.created_at
 FROM request_comments c JOIN users u ON u.id=c.author_id WHERE c.request_id=? ORDER BY c.created_at, c.id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RequestComment
	for rows.Next() {
		var c RequestComment
		if err := rows.Scan(&c.ID, &c.RequestID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) AddRequestAttachment(ctx context.Context, actor User, requestID int64, in AttachmentInput) (int64, error) {
	if err := validateAttachment(in); err != nil {
		return 0, err
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := addRequestAttachmentTx(ctx, tx, actor, requestID, in)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// addRequestAttachmentTx is the write half, so EditRequest can put the document in
// the same transaction as the corrections it arrived with.
func addRequestAttachmentTx(ctx context.Context, tx *sql.Tx, actor User, requestID int64, in AttachmentInput) (int64, error) {
	if _, err := requestInTx(ctx, tx, requestID); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO request_attachments(request_id,original_name,stored_path,mime_type,size_bytes,uploaded_by) VALUES(?,?,?,?,?,?)`, requestID, in.OriginalName, in.StoredPath, in.MimeType, in.SizeBytes, actor.ID)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "attach", EntityType: "payment_request", EntityID: &requestID,
		Summary: "Uploaded attachment " + in.OriginalName, After: map[string]any{"id": id, "name": in.OriginalName}}); err != nil {
		return 0, err
	}
	return id, nil
}

// initials renders "Arun Mehta" as "AM" for the `.tl-dot` avatar.
func initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "?"
	}
	out := strings.ToUpper(parts[0][:1])
	if len(parts) > 1 {
		out += strings.ToUpper(parts[len(parts)-1][:1])
	}
	return out
}

// threadDiffFields are the fields an edit reports in `.tl-change`. Everything
// else changes too rarely, or too noisily, to be worth a line in the story.
var threadDiffFields = []string{"amount", "approved_amount", "needed_by", "invoice_no",
	"invoice_date", "expense_date", "manager_id", "short_title", "purpose", "urgent"}

func diffRequestAudit(beforeJSON, afterJSON string) []ThreadChange {
	var before, after map[string]any
	if json.Unmarshal([]byte(beforeJSON), &before) != nil || json.Unmarshal([]byte(afterJSON), &after) != nil {
		return nil
	}
	var out []ThreadChange
	for _, f := range threadDiffFields {
		key := auditFieldKey(f)
		was, now := fmt.Sprint(before[key]), fmt.Sprint(after[key])
		if before[key] == nil && after[key] == nil {
			continue
		}
		if was != now {
			out = append(out, ThreadChange{Field: f, Was: was, Now: now})
		}
	}
	return out
}

// nameUsersInChanges turns the user ids a `manager_id` change carries into the
// people they are. The audit serialises Request.ManagerID, so a reassignment or
// an edit that moved the approver diffed as "Approver 3 → 4" — internal ids on
// the one screen every reader of a request opens. A name that cannot be
// resolved (a value that is not an id, or an id nobody has) is left as it was
// rather than dropped, so the change is still recorded as a change.
func (s *Store) nameUsersInChanges(ctx context.Context, changes []ThreadChange) ([]ThreadChange, error) {
	for i, c := range changes {
		if c.Field != "manager_id" {
			continue
		}
		for _, side := range []*string{&changes[i].Was, &changes[i].Now} {
			// JSON numbers unmarshal as float64, so the id printed as "3" or as
			// "3e+00" depending on its size; ParseFloat reads both.
			id, err := strconv.ParseFloat(*side, 64)
			if err != nil || id <= 0 {
				continue
			}
			var name string
			err = s.db.QueryRowContext(ctx, `SELECT name FROM users WHERE id=?`, int64(id)).Scan(&name)
			if err == sql.ErrNoRows {
				continue
			}
			if err != nil {
				return nil, err
			}
			*side = name
		}
	}
	return changes, nil
}

// auditFieldKey maps a column name to the key recordAuditTx serialised, which
// is the Go field name on Request (Amount, NeededBy, …).
func auditFieldKey(column string) string {
	parts := strings.Split(column, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	key := strings.Join(parts, "")
	if key == "InvoiceNo" {
		return "InvoiceNo"
	}
	if key == "ManagerId" {
		return "ManagerID"
	}
	if key == "ApprovedAmount" {
		return "ApprovedAmount"
	}
	return key
}

// RequestThread merges the audit trail, the conversation and the file uploads
// into one chronological stream — the single "History and conversation" list
// the design renders as `.thread` (UI/UX §9).
func (s *Store) RequestThread(ctx context.Context, requestID int64) ([]ThreadEntry, error) {
	audit, err := s.Audit(ctx, "payment_request", requestID, 200)
	if err != nil {
		return nil, err
	}
	comments, err := s.RequestComments(ctx, requestID)
	if err != nil {
		return nil, err
	}
	atts, err := s.RequestAttachments(ctx, requestID)
	if err != nil {
		return nil, err
	}
	// s.Audit returns newest first, and SQLite's CURRENT_TIMESTAMP resolves only
	// to the second, so a whole burst of events shares one timestamp. Re-sort the
	// trail by (created_at, id) ascending before merging: the audit id is the only
	// strictly monotonic record of what happened first, and leaving the order to
	// SQLite's tie-breaking would make the first line of the story arbitrary.
	sort.SliceStable(audit, func(i, j int) bool {
		if !audit[i].CreatedAt.Equal(audit[j].CreatedAt) {
			return audit[i].CreatedAt.Before(audit[j].CreatedAt)
		}
		return audit[i].ID < audit[j].ID
	})
	out := make([]ThreadEntry, 0, len(audit)+len(comments)+len(atts))
	for _, a := range audit {
		// The comment and attach events are rendered by their own richer
		// entries below; keeping both would double every line. A concern is the
		// same case wearing a different verb: RaiseConcern writes a comment row
		// and an audit row whose summary is that comment, so the manager's words
		// would otherwise appear twice in one stream.
		if a.Action == "comment" || a.Action == "attach" || a.Action == "concern" {
			continue
		}
		var actorID int64
		if a.ActorID != nil {
			actorID = *a.ActorID
		}
		changes, err := s.nameUsersInChanges(ctx, diffRequestAudit(a.BeforeJSON, a.AfterJSON))
		if err != nil {
			return nil, err
		}
		out = append(out, ThreadEntry{Kind: "event", Action: a.Action, ActorID: actorID,
			ActorName: a.ActorName, Initials: initials(a.ActorName), Title: a.Summary,
			Changes: changes, CreatedAt: a.CreatedAt})
	}
	for _, c := range comments {
		out = append(out, ThreadEntry{Kind: "comment", ActorID: c.AuthorID, ActorName: c.AuthorName,
			Initials: initials(c.AuthorName), Body: c.Body, CreatedAt: c.CreatedAt})
	}
	for _, a := range atts {
		out = append(out, ThreadEntry{Kind: "attachment", ActorID: a.UploadedBy, Title: "Attachment added",
			FileName: a.OriginalName, FileSize: a.SizeBytes, CreatedAt: a.CreatedAt})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// requestBuckets back the `.segmented` tabs. "needs-me" is scope-dependent and
// handled separately in requestWhere.
var requestBuckets = map[string][]string{
	"open":   {"pending", "returned", "approved", "cancellation_requested"},
	"closed": {"rejected", "withdrawn", "cancelled"},
}

func requestWhere(opts RequestListOptions) (string, []any) {
	var where []string
	var args []any
	switch opts.Scope {
	case "own":
		where = append(where, `r.requester_id=?`)
		args = append(args, opts.ViewerID)
	case "assigned":
		where = append(where, `r.manager_id=?`)
		args = append(args, opts.ViewerID)
	}
	placeholders := func(n int) string {
		return strings.TrimSuffix(strings.Repeat("?,", n), ",")
	}
	switch {
	case len(opts.Statuses) > 0:
		where = append(where, `r.status IN (`+placeholders(len(opts.Statuses))+`)`)
		for _, st := range opts.Statuses {
			args = append(args, st)
		}
	case opts.Bucket == "needs-me":
		// The one line the whole design turns on: who owes the next action.
		where = append(where, `((r.requester_id=? AND r.status='returned')
 OR (r.manager_id=? AND r.status IN ('pending','cancellation_requested')))`)
		args = append(args, opts.ViewerID, opts.ViewerID)
	case opts.Bucket != "" && opts.Bucket != "all":
		statuses := requestBuckets[opts.Bucket]
		if len(statuses) == 0 {
			statuses = []string{opts.Bucket}
		}
		where = append(where, `r.status IN (`+placeholders(len(statuses))+`)`)
		for _, st := range statuses {
			args = append(args, st)
		}
	case opts.Status != "" && opts.Status != "all":
		where = append(where, `r.status=?`)
		args = append(args, opts.Status)
	}
	if opts.Type != "" {
		where = append(where, `r.type=?`)
		args = append(args, opts.Type)
	}
	if opts.Treatment != "" {
		where = append(where, `r.treatment=?`)
		args = append(args, opts.Treatment)
	}
	if opts.ProjectID > 0 {
		where = append(where, `r.project_id=?`)
		args = append(args, opts.ProjectID)
	}
	if q := strings.ToLower(strings.TrimSpace(opts.Query)); q != "" {
		where = append(where, `(lower(r.number) LIKE ? ESCAPE '\' OR lower(r.purpose) LIKE ? ESCAPE '\'
 OR lower(r.short_title) LIKE ? ESCAPE '\' OR lower(r.invoice_no) LIKE ? ESCAPE '\'
 OR lower(COALESCE(v.name,r.vendor_payee)) LIKE ? ESCAPE '\' OR lower(ru.name) LIKE ? ESCAPE '\')`)
		q = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
		needle := "%" + q + "%"
		args = append(args, needle, needle, needle, needle, needle, needle)
	}
	clause := ""
	if len(where) > 0 {
		clause = ` WHERE ` + strings.Join(where, ` AND `)
	}
	return clause, args
}

// RequestsUnlimited asks for every row the filter matches, with no cap at all.
// The CSV export is what needs it: an export taken for reconciliation that drops
// rows without saying so is worse than no export (D3, F-B-16).
const RequestsUnlimited = -1

// defaultRequestLimit is the cap applied when a caller names no number. It is a
// guard against a panel accidentally reading the whole table, not a display
// decision — a screen that lists requests uses ListRequestsPage, which reports
// the total and whether anything was left out.
const defaultRequestLimit = 200

// RequestPage is a list of requests that knows what it is not showing.
//
// F-B-16: ListRequests capped itself at 200 rows while CountRequests — the tab
// count rendered right beside it — had no cap at all, so a queue promised 214 and
// drew 200 with nothing on the page saying so, and the CSV export dropped the
// same fourteen rows just as quietly. Silence was the defect. Total and Truncated
// are what a screen needs in order to stop lying: page on Offset/Limit and say
// which page this is, or ask for RequestsUnlimited and carry every row.
type RequestPage struct {
	Requests []Request
	// Total rows matching the same filter, ignoring Limit and Offset. This is
	// exactly CountRequests, so a tab built from Total can never disagree with
	// the rows beneath it.
	Total int
	// Offset and Limit as they were applied. Limit is 0 when no cap was applied.
	Offset int
	Limit  int
	// Truncated reports that rows matching the filter are not in Requests —
	// either later pages, or rows the cap dropped.
	Truncated bool
}

// RequestPageOptions is RequestListOptions plus the offset a paged screen needs.
// Offset lives here rather than on RequestListOptions because every existing
// caller of that struct means "the first N", and its zero value must go on
// meaning exactly that.
type RequestPageOptions struct {
	RequestListOptions
	Offset int
}

// ListRequestsPage returns a window onto the matching requests together with the
// total the same filter matches, so the screen can say what it is not showing.
func (s *Store) ListRequestsPage(ctx context.Context, opts RequestPageOptions) (RequestPage, error) {
	total, err := s.CountRequests(ctx, opts.RequestListOptions)
	if err != nil {
		return RequestPage{}, err
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	rows, err := s.listRequestRows(ctx, opts.RequestListOptions, opts.Offset)
	if err != nil {
		return RequestPage{}, err
	}
	page := RequestPage{Requests: rows, Total: total, Offset: opts.Offset}
	if opts.Limit != RequestsUnlimited {
		page.Limit = effectiveRequestLimit(opts.Limit)
	}
	page.Truncated = opts.Offset+len(rows) < total
	return page, nil
}

// ListRequests returns the matching rows and nothing about what it left out. A
// caller that renders a list, or exports one, wants ListRequestsPage instead;
// this stays for the panels that deliberately show only a head (the dashboard's
// four rows) and for the queues that pass an explicit Limit.
func (s *Store) ListRequests(ctx context.Context, opts RequestListOptions) ([]Request, error) {
	return s.listRequestRows(ctx, opts, 0)
}

func effectiveRequestLimit(limit int) int {
	if limit <= 0 {
		return defaultRequestLimit
	}
	return limit
}

func (s *Store) listRequestRows(ctx context.Context, opts RequestListOptions, offset int) ([]Request, error) {
	clause, args := requestWhere(opts)
	// F-B-18: urgent first, then **oldest** first. The comment here and the
	// sub-line on requests-list.html both say the list is sorted by who has been
	// kept waiting longest, and that is created_at ASC — it used to be DESC, so
	// the most neglected request was the last one on the page, or on no page at
	// all once the 200-row cap bit. CURRENT_TIMESTAMP resolves only to the second,
	// so a burst of submissions shares one value; id breaks the tie and keeps them
	// in the order they arrived.
	q := requestSelect + clause + ` ORDER BY r.urgent DESC, r.created_at, r.id`
	switch {
	case opts.Limit == RequestsUnlimited && offset > 0:
		// SQLite has no bare OFFSET: -1 is its "no limit" sentinel.
		q += ` LIMIT -1 OFFSET ?`
		args = append(args, offset)
	case opts.Limit == RequestsUnlimited:
	case offset > 0:
		q += ` LIMIT ? OFFSET ?`
		args = append(args, effectiveRequestLimit(opts.Limit), offset)
	default:
		q += ` LIMIT ?`
		args = append(args, effectiveRequestLimit(opts.Limit))
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CountRequests(ctx context.Context, opts RequestListOptions) (int, error) {
	clause, args := requestWhere(opts)
	q := `SELECT COUNT(*) FROM payment_requests r
 JOIN users ru ON ru.id=r.requester_id
 LEFT JOIN vendors v ON v.id=r.vendor_id` + clause
	var n int
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

// SimilarRequests reports recent requests that look like the one being raised:
// the same payee, and either a near-identical amount or the very same invoice
// reference, inside the configured window. It is a warning, not a gate — callers
// must never refuse a submit on the strength of a non-empty result (G6).
func (s *Store) SimilarRequests(ctx context.Context, opt SimilarRequestOptions) ([]Request, error) {
	if opt.Days <= 0 {
		opt.Days = 30
	}
	if opt.Limit <= 0 {
		opt.Limit = 5
	}
	payee := strings.ToLower(strings.TrimSpace(opt.Payee))
	if opt.VendorID <= 0 && payee == "" {
		return nil, nil
	}
	// ±1% of the amount, so "₹1,00,000 again" is caught but "₹2,500" is not.
	tolerance := opt.Amount / 100
	if tolerance < 100 {
		tolerance = 100
	}
	invoice := strings.ToLower(strings.TrimSpace(opt.InvoiceNo))
	q := requestSelect + ` WHERE r.id<>?
 AND r.status NOT IN ('withdrawn','rejected','cancelled')
 AND r.created_at >= datetime('now', ?)
 AND (( ? > 0 AND r.vendor_id = ? ) OR ( ? <> '' AND lower(COALESCE(v.name, r.vendor_payee)) = ? ))
 AND (( ? > 0 AND abs(r.amount - ?) <= ? ) OR ( ? <> '' AND lower(r.invoice_no) = ? ))
 ORDER BY r.created_at DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q,
		opt.ExcludeID,
		fmt.Sprintf("-%d days", opt.Days),
		opt.VendorID, opt.VendorID,
		payee, payee,
		opt.Amount, opt.Amount, tolerance,
		invoice, invoice,
		opt.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
