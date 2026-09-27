package store

import (
	"context"
	"database/sql"
	"errors"
	"fervidbudget/internal/money"
	"fmt"
	"strings"
	"time"
)

func (s *Store) CreatePayment(ctx context.Context, actor User, in PaymentInput) (int64, error) {
	return s.createPayment(ctx, actor, in, nil)
}

// CreatePaymentWithAttachment makes the database payment and attachment rows
// one transaction. The caller remains responsible for removing an uploaded
// file if this method returns an error.
func (s *Store) CreatePaymentWithAttachment(ctx context.Context, actor User, in PaymentInput, attachment *AttachmentInput) (int64, error) {
	return s.createPayment(ctx, actor, in, attachment)
}

func (s *Store) createPayment(ctx context.Context, actor User, in PaymentInput, attachment *AttachmentInput) (int64, error) {
	if err := s.validatePayment(ctx, in, false); err != nil {
		return 0, err
	}
	if attachment != nil {
		if err := validateAttachment(*attachment); err != nil {
			return 0, err
		}
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,payment_mode,invoice_no,reference_no,remarks,entered_by) VALUES(?,?,?,?,?,?,?,?,?)`, in.HeadID, in.PaidOn, in.Amount, in.VendorPayee, in.PaymentMode, in.InvoiceNo, in.ReferenceNo, in.Remarks, actor.ID)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	after := paymentFromInput(id, actor, in)
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "create", EntityType: "payment", EntityID: &id, Summary: "Recorded payment " + money.FormatPaise(in.Amount), After: after}); err != nil {
		return 0, err
	}
	if attachment != nil {
		if _, err := addAttachmentTx(ctx, tx, actor, id, *attachment); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) UpdatePayment(ctx context.Context, actor User, id int64, in PaymentInput) error {
	return s.UpdatePaymentWithAttachment(ctx, actor, id, in, nil)
}

// UpdatePaymentWithAttachment updates payment fields, optionally adds a file,
// and records both audit events in one transaction.
func (s *Store) UpdatePaymentWithAttachment(ctx context.Context, actor User, id int64, in PaymentInput, attachment *AttachmentInput) error {
	if attachment != nil {
		if err := validateAttachment(*attachment); err != nil {
			return err
		}
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := validatePaymentTx(ctx, tx, in, false); err != nil {
		return err
	}
	before, err := paymentInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := paymentWriteAccess(ctx, tx, actor.ID, before); err != nil {
		return err
	}
	// S12: a payment that settles a request is the request's outcome. Editing it
	// would silently rewrite what the requester and approver were told was paid.
	if before.RequestID != nil {
		return fmt.Errorf("%w: a payment linked to a request cannot be edited", ErrValidation)
	}
	if before.VoidedAt != nil {
		return fmt.Errorf("%w: cannot edit voided payment", ErrValidation)
	}
	// Read the lock on this transaction, not the pool: a second connection asking
	// while this one holds the write lock is the shape that stacked busy waits.
	locked, err := isLocked(ctx, tx, before.PaidOn[:7])
	if err != nil {
		return err
	}
	if locked {
		return ErrLockedMonth
	}
	_, err = tx.ExecContext(ctx, `UPDATE payments SET head_id=?, paid_on=?, amount=?, vendor_payee=?, payment_mode=?, invoice_no=?, reference_no=?, remarks=?, updated_by=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		in.HeadID, in.PaidOn, in.Amount, in.VendorPayee, in.PaymentMode, in.InvoiceNo, in.ReferenceNo, in.Remarks, actor.ID, id)
	if err != nil {
		return classify(err)
	}
	after := paymentFromInput(id, actor, in)
	after.EnteredBy, after.EnteredByName = before.EnteredBy, before.EnteredByName
	after.UpdatedBy = &actor.ID
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "update", EntityType: "payment", EntityID: &id, Summary: "Edited payment " + money.FormatPaise(in.Amount), Before: before, After: after}); err != nil {
		return err
	}
	if attachment != nil {
		if _, err := addAttachmentTx(ctx, tx, actor, id, *attachment); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) VoidPayment(ctx context.Context, actor User, id int64, reason string) error {
	reason = strings.TrimSpace(reason)
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := paymentInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := paymentWriteAccess(ctx, tx, actor.ID, before); err != nil {
		return err
	}
	// S12: voiding would leave the request 'completed' with nothing paid.
	if before.RequestID != nil {
		return fmt.Errorf("%w: a payment linked to a request cannot be voided", ErrValidation)
	}
	if before.VoidedAt != nil {
		return nil
	}
	if reason == "" {
		return fmt.Errorf("%w: void reason is required", ErrValidation)
	}
	locked, err := isLocked(ctx, tx, before.PaidOn[:7])
	if err != nil {
		return err
	}
	if locked {
		return ErrLockedMonth
	}
	_, err = tx.ExecContext(ctx, `UPDATE payments SET voided_by=?, void_reason=?, voided_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP WHERE id=?`, actor.ID, reason, id)
	if err != nil {
		return err
	}
	after := before
	after.VoidedBy, after.VoidReason = &actor.ID, reason
	now := time.Now().UTC()
	after.VoidedAt = &now
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "void", EntityType: "payment", EntityID: &id, Summary: "Voided payment " + money.FormatPaise(before.Amount), Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

// paymentSelect reads ledger rows with everything a payment screen shows: the
// project and head names, the name of the person who entered it, and the
// linked request's treatment. The heads and projects joins are LEFT joins with
// COALESCE because a payment settling a recoverable request has no head; its
// screens must still resolve, with an empty project and head. Callers append
// their own WHERE / ORDER BY. Read the rows with scanPayment.
const paymentSelect = `SELECT py.id,COALESCE(py.head_id,0),COALESCE(h.project_id,0),COALESCE(p.name,''),COALESCE(h.name,''),py.paid_on,py.amount,
		COALESCE(py.vendor_payee,''),COALESCE(py.payment_mode,''),COALESCE(py.invoice_no,''),COALESCE(py.reference_no,''),COALESCE(py.remarks,''),
		py.entered_by,u.name,py.updated_by,py.voided_by,COALESCE(py.void_reason,''),py.voided_at,py.created_at,py.updated_at,
		py.request_id,COALESCE(py.settlement,''),COALESCE(py.partial_reason,''),COALESCE(pr.treatment,'')
		FROM payments py LEFT JOIN heads h ON h.id=py.head_id LEFT JOIN projects p ON p.id=h.project_id JOIN users u ON u.id=py.entered_by LEFT JOIN payment_requests pr ON pr.id=py.request_id`

// scanPayment reads one row selected with paymentSelect. A single-row lookup
// that finds nothing returns ErrNotFound.
func scanPayment(sc rowScanner) (Payment, error) {
	var p Payment
	err := sc.Scan(&p.ID, &p.HeadID, &p.ProjectID, &p.Project, &p.Head, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.EnteredByName, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt, &p.RequestID, &p.Settlement, &p.PartialReason, &p.Treatment)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// Payment reads one ledger row, or returns ErrNotFound. The request's treatment
// rides along so the detail screen can say "Recoverable" where a budget payment
// names its project and head.
func (s *Store) Payment(ctx context.Context, id int64) (Payment, error) {
	return scanPayment(s.db.QueryRowContext(ctx, paymentSelect+` WHERE py.id=?`, id))
}

// PaymentForRequest returns the latest payment linked to a request, or
// ErrNotFound when none exists yet. The requester's outcome view shows it.
func (s *Store) PaymentForRequest(ctx context.Context, requestID int64) (Payment, error) {
	return scanPayment(s.db.QueryRowContext(ctx, paymentSelect+` WHERE py.request_id=? ORDER BY py.id DESC LIMIT 1`, requestID))
}

// paymentInTx reads the payment's own columns inside the caller's transaction.
// It deliberately reads no joined names (project, head, entered by): edit and
// void record this snapshot as the audit "before", and names present only on
// that side would look like changes on the audit screen.
func paymentInTx(ctx context.Context, tx *sql.Tx, id int64) (Payment, error) {
	var p Payment
	err := tx.QueryRowContext(ctx, `SELECT id,COALESCE(head_id,0),paid_on,amount,COALESCE(vendor_payee,''),COALESCE(payment_mode,''),COALESCE(invoice_no,''),COALESCE(reference_no,''),COALESCE(remarks,''),entered_by,updated_by,voided_by,COALESCE(void_reason,''),voided_at,created_at,updated_at,request_id,COALESCE(settlement,''),COALESCE(partial_reason,'') FROM payments WHERE id=?`, id).
		Scan(&p.ID, &p.HeadID, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt, &p.RequestID, &p.Settlement, &p.PartialReason)
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	return p, err
}

func paymentFromInput(id int64, actor User, in PaymentInput) Payment {
	return Payment{ID: id, HeadID: in.HeadID, PaidOn: in.PaidOn, Amount: in.Amount, VendorPayee: in.VendorPayee, PaymentMode: in.PaymentMode, InvoiceNo: in.InvoiceNo, ReferenceNo: in.ReferenceNo, Remarks: in.Remarks, EnteredBy: actor.ID, EnteredByName: actor.Name}
}

func (s *Store) ListPayments(ctx context.Context, opts PaymentListOptions) ([]Payment, error) {
	if opts.Limit <= 0 {
		opts.Limit = 300
	}
	q := paymentSelect
	var where []string
	var args []any
	// F-A-04 / F-G-003: the payment data scope. "own" narrows the ledger to
	// rows the viewer entered; "assigned" has no routed-to meaning on payments
	// and narrows the same way rather than silently widening to everything —
	// an administrator who chose either narrowing gets a narrowing. Only "all"
	// is unrestricted: "" (the role editor's "None") and anything else read no
	// rows, exactly as requestWhere treats the request scope (fixwave rbac-2).
	switch opts.Scope {
	case ScopeAll:
	case "own", "assigned":
		where = append(where, `py.entered_by=?`)
		args = append(args, opts.ViewerID)
	default:
		where = append(where, `0`)
	}
	if opts.VendorID > 0 {
		where = append(where, `pr.vendor_id=?`)
		args = append(args, opts.VendorID)
	}
	if validMonth(opts.Month) {
		where = append(where, `substr(py.paid_on,1,7)=?`)
		args = append(args, opts.Month)
	}
	if opts.ProjectID > 0 {
		where = append(where, `h.project_id=?`)
		args = append(args, opts.ProjectID)
	}
	if opts.HeadID > 0 {
		where = append(where, `py.head_id=?`)
		args = append(args, opts.HeadID)
	}
	// The same COALESCE Grid uses, so a historical payment (no request at all)
	// stays in, exactly as it still counts as an actual there.
	if opts.ExcludeRecoverable {
		where = append(where, `COALESCE(pr.treatment,'') <> 'recoverable'`)
	}
	switch opts.Status {
	case "voided":
		where = append(where, `py.voided_at IS NOT NULL`)
	case "all":
	default:
		where = append(where, `py.voided_at IS NULL`)
	}
	search := strings.ToLower(strings.TrimSpace(opts.Query))
	if search != "" {
		where = append(where, `(lower(p.name) LIKE ? ESCAPE '\' OR lower(h.name) LIKE ? ESCAPE '\' OR lower(COALESCE(py.vendor_payee,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(py.invoice_no,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(py.reference_no,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(py.remarks,'')) LIKE ? ESCAPE '\')`)
		search = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
		needle := "%" + search + "%"
		args = append(args, needle, needle, needle, needle, needle, needle)
	}
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, ` AND `)
	}
	q += ` ORDER BY py.paid_on DESC, py.created_at DESC LIMIT ? OFFSET ?`
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	args = append(args, opts.Limit, opts.Offset)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		// request_id travels with the row because the ledger has to tell a
		// linked payment from a historical one: a linked payment cannot be
		// edited, and a screen that cannot see the difference would offer an
		// Edit button the store refuses.
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) AddAttachment(ctx context.Context, actor User, paymentID int64, original, stored, mime string, size int64) error {
	attachment := AttachmentInput{OriginalName: original, StoredPath: stored, MimeType: mime, SizeBytes: size}
	if err := validateAttachment(attachment); err != nil {
		return err
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := paymentInTx(ctx, tx, paymentID)
	if err != nil {
		return err
	}
	if err := paymentWriteAccess(ctx, tx, actor.ID, p); err != nil {
		return err
	}
	// S12 / F-D-08: a payment that settles a request is the request's outcome
	// and the screen calls it read-only. Edit and void already refuse a linked
	// row; a new document is the same mutation of that record and gets the
	// same answer. (The settlement's own proof travels inside
	// RecordPaymentForRequest's transaction, which never passes through here.)
	if p.RequestID != nil {
		return fmt.Errorf("%w: a payment linked to a request cannot receive a new attachment", ErrValidation)
	}
	if p.VoidedAt != nil {
		return fmt.Errorf("%w: cannot attach files to a voided payment", ErrValidation)
	}
	locked, err := isLocked(ctx, tx, p.PaidOn[:7])
	if err != nil {
		return err
	}
	if locked {
		return ErrLockedMonth
	}
	if _, err := addAttachmentTx(ctx, tx, actor, paymentID, attachment); err != nil {
		return err
	}
	return tx.Commit()
}

func addAttachmentTx(ctx context.Context, tx *sql.Tx, actor User, paymentID int64, attachment AttachmentInput) (int64, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO payment_attachments(payment_id,original_name,stored_path,mime_type,size_bytes,uploaded_by) VALUES(?,?,?,?,?,?)`, paymentID, attachment.OriginalName, attachment.StoredPath, attachment.MimeType, attachment.SizeBytes, actor.ID)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "attach", EntityType: "payment", EntityID: &paymentID, Summary: "Uploaded attachment " + attachment.OriginalName, After: map[string]any{"id": id, "name": attachment.OriginalName}}); err != nil {
		return 0, err
	}
	return id, nil
}

// AttachmentByID retrieves a single attachment for authorized download
// handlers. Authorization is intentionally left to the app's permission layer.
func (s *Store) AttachmentByID(ctx context.Context, id int64) (Attachment, error) {
	var a Attachment
	err := s.db.QueryRowContext(ctx, `SELECT id,payment_id,original_name,stored_path,COALESCE(mime_type,''),size_bytes,uploaded_by,created_at FROM payment_attachments WHERE id=?`, id).
		Scan(&a.ID, &a.PaymentID, &a.OriginalName, &a.StoredPath, &a.MimeType, &a.SizeBytes, &a.UploadedBy, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return a, ErrNotFound
	}
	return a, err
}

// AttachmentWithPayment resolves a payment attachment to the payment it
// belongs to, in one call, so a download or upload handler can walk from the
// attachment to its payment and — through Payment.RequestID — to the request
// whose data scope governs it (F-A-01/F-A-03). Authorization stays with the
// app layer; this only supplies the ownership chain it needs.
func (s *Store) AttachmentWithPayment(ctx context.Context, id int64) (Attachment, Payment, error) {
	a, err := s.AttachmentByID(ctx, id)
	if err != nil {
		return Attachment{}, Payment{}, err
	}
	p, err := s.Payment(ctx, a.PaymentID)
	if err != nil {
		return Attachment{}, Payment{}, err
	}
	return a, p, nil
}

// RequestAttachmentByID resolves a request document from its own table.
// Request documents live in request_attachments, not payment_attachments —
// the two id spaces are unrelated (F-A-05) — so the request-document route
// must resolve here and scope by the owning RequestID, never through
// AttachmentByID. Authorization is intentionally left to the app layer.
func (s *Store) RequestAttachmentByID(ctx context.Context, id int64) (RequestAttachment, error) {
	var a RequestAttachment
	err := s.db.QueryRowContext(ctx, `SELECT id,request_id,original_name,stored_path,COALESCE(mime_type,''),size_bytes,uploaded_by,created_at FROM request_attachments WHERE id=?`, id).
		Scan(&a.ID, &a.RequestID, &a.OriginalName, &a.StoredPath, &a.MimeType, &a.SizeBytes, &a.UploadedBy, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Store) Attachments(ctx context.Context, paymentID int64) ([]Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,payment_id,original_name,stored_path,COALESCE(mime_type,''),size_bytes,uploaded_by,created_at FROM payment_attachments WHERE payment_id=? ORDER BY created_at`, paymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.PaymentID, &a.OriginalName, &a.StoredPath, &a.MimeType, &a.SizeBytes, &a.UploadedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// paymentModes is the one vocabulary of payment modes, in the order a form
// offers them when configuration names none. Every payment form builds its
// options from it and validatePaymentTx refuses anything outside it.
var paymentModes = []string{"neft", "rtgs", "bank_transfer", "cheque", "upi", "cash", "card", "dd", "other"}

// PaymentModes returns a copy of the payment-mode vocabulary.
func PaymentModes() []string { return append([]string(nil), paymentModes...) }

// CanonicalPaymentMode maps what a person or the configuration may write
// ("NEFT", "Bank transfer", "Demand draft") to the stored key ("neft",
// "bank_transfer", "dd"). A value outside the vocabulary comes back trimmed but
// otherwise unchanged.
func CanonicalPaymentMode(value string) string {
	switch key := strings.ToLower(strings.TrimSpace(value)); key {
	case "bank transfer":
		return "bank_transfer"
	case "demand draft":
		return "dd"
	default:
		for _, mode := range paymentModes {
			if key == mode {
				return mode
			}
		}
		return strings.TrimSpace(value)
	}
}

// IsPaymentMode reports whether value names a mode in the vocabulary, in any
// of the spellings CanonicalPaymentMode accepts.
func IsPaymentMode(value string) bool {
	canonical := CanonicalPaymentMode(value)
	for _, mode := range paymentModes {
		if canonical == mode {
			return true
		}
	}
	return false
}

// validatePayment runs the check against the pool, for the callers that have not
// opened a transaction yet.
func (s *Store) validatePayment(ctx context.Context, in PaymentInput, headOptional bool) error {
	return validatePaymentTx(ctx, s.db, in, headOptional)
}

// validatePaymentTx checks a payment's own facts on a caller-supplied querier, so
// that a caller already holding a write transaction validates inside it rather
// than from a second pooled connection.
//
// headOptional is true only when the payment settles a recoverable request
// (F-D-11/F-E-01): a deposit or advance belongs to no budget head — the
// recoverable fieldset never collects one — and the grid and monthly report
// already exclude recoverables by treatment, so its head was never meaningful.
// Every other payment, including one with no request at all, still needs a head.
func validatePaymentTx(ctx context.Context, q rowQuerier, in PaymentInput, headOptional bool) error {
	if in.Amount <= 0 || in.Amount > money.MaxAmount || !validDate(in.PaidOn) || (in.HeadID == 0 && !headOptional) {
		return fmt.Errorf("%w: valid head, date, and positive amount are required", ErrValidation)
	}
	// The mode is optional, but when given it must be one the forms offer:
	// every payment form builds its options from PaymentModes, so anything else
	// arrived from outside the product.
	if strings.TrimSpace(in.PaymentMode) != "" && !IsPaymentMode(in.PaymentMode) {
		return fmt.Errorf("%w: choose a payment mode from the list", ErrValidation)
	}
	// F-D-06: paid_on records when money left the bank, so a date after today
	// records something that has not happened.
	//
	// "Today" is in.Now when a caller injects a clock, and the server's own clock
	// when it does not. That default is the fix, not a detail: the check was
	// written to skip on a zero Now, no production caller ever set the field,
	// and so the rule shipped inert — present in the code, pinned by a store
	// test, and enforced against nobody. Forgetting to inject a clock must mean
	// "enforce", never "skip"; injection stays so a test can pin the day.
	//
	// It is deliberately LOCAL time rather than UTC, because it has to agree with
	// the clock the rest of the product calls "now": validMonthOrCurrent defaults
	// the ledger and the grid to time.Now().Format("2006-01") in the server's own
	// zone (app.go:2051). While this read UTC the two disagreed for every zone
	// ahead of Greenwich — between local midnight and the offset the ledger had
	// rolled to the new day and this guard had not, so an accountant recording a
	// payment dated the day the screen was showing them was told "paid on cannot
	// be a future date". In IST that was every day from 00:00 to 05:30.
	//
	// paid_on and the formatted clock share the YYYY-MM-DD shape, so a plain
	// string comparison is a correct date comparison.
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	if in.PaidOn > now.Format("2006-01-02") {
		return fmt.Errorf("%w: paid on cannot be a future date — money cannot have left the bank after today", ErrValidation)
	}
	locked, err := isLocked(ctx, q, in.PaidOn[:7])
	if err != nil {
		return err
	}
	if locked {
		return ErrLockedMonth
	}
	if in.HeadID == 0 {
		return nil
	}
	var active int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM heads h JOIN projects p ON p.id=h.project_id WHERE h.id=? AND h.active=1 AND p.active=1`, in.HeadID).Scan(&active); err != nil {
		return err
	}
	if active == 0 {
		return ErrInactiveHead
	}
	return nil
}

func validateAttachment(in AttachmentInput) error {
	if strings.TrimSpace(in.OriginalName) == "" || strings.TrimSpace(in.StoredPath) == "" || in.SizeBytes < 0 {
		return fmt.Errorf("%w: valid attachment metadata is required", ErrValidation)
	}
	return nil
}
