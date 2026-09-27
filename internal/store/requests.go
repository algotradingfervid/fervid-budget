// requests.go holds the core of a payment request: numbering, reading a request
// row, raising a new request, editing it (with the optional resubmit and
// document upload in one transaction), comments and attachments.
//
// Related files: request_validation.go checks the form, request_transitions.go
// moves a request between statuses, request_list.go answers list and count
// queries, and request_thread.go builds the history shown on the request page.
// Every write runs in one write transaction and records an audit row.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
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
	if errors.Is(err, sql.ErrNoRows) || (err == nil && strings.TrimSpace(v) == "") {
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

// requestSelect reads one full Request, with the names of its project, head,
// vendor and people joined in and the total paid so far (voided payments
// excluded). Callers append a WHERE clause; scanRequest reads each row.
const requestSelect = `SELECT r.id,r.number,r.status,r.treatment,r.type,r.recoverable_category,r.recoverable_category_id,
 r.project_id,COALESCE(p.name,''),r.head_id,COALESCE(h.name,''),
 r.vendor_id,COALESCE(NULLIF(v.name,''),r.vendor_payee),COALESCE(v.gstin,''),r.vendor_payee,r.short_title,
 r.amount,r.purpose,COALESCE(r.needed_by,''),
 r.invoice_no,COALESCE(r.invoice_date,''),COALESCE(r.expense_date,''),r.advance_reason,
 r.counterparty,COALESCE(r.expected_return_date,''),r.repayment_notes,
 r.urgent,r.urgency_reason,r.attachment_exception_reason,
 r.requester_id,COALESCE(ru.name,''),r.manager_id,COALESCE(mu.name,''),
 r.approved_amount,r.approved_by,COALESCE(au.name,''),r.approved_at,
 r.decision_reason,r.cancel_reason,r.on_hold,r.hold_reason,r.processing_by,COALESCE(pu.name,''),r.processing_at,r.concern_open,
 r.reminder_last_sent,r.submitted_at,r.created_at,r.updated_at,
 (SELECT COALESCE(SUM(py.amount),0) FROM payments py WHERE py.request_id=r.id AND py.voided_at IS NULL)
FROM payment_requests r
LEFT JOIN projects p ON p.id=r.project_id
LEFT JOIN heads h ON h.id=r.head_id
LEFT JOIN vendors v ON v.id=r.vendor_id
JOIN users ru ON ru.id=r.requester_id
JOIN users mu ON mu.id=r.manager_id
LEFT JOIN users au ON au.id=r.approved_by
LEFT JOIN users pu ON pu.id=r.processing_by`

// scanRequest reads one requestSelect row into a Request. Nullable columns
// become nil pointers, and a missing row becomes ErrNotFound.
func scanRequest(sc rowScanner) (Request, error) {
	var r Request
	var catID, projID, headID, vendorID, approvedAmt, approvedBy, processingBy sql.NullInt64
	var approvedAt, reminder, submitted, processingAt sql.NullTime
	var urgent, onHold, concernOpen int
	err := sc.Scan(&r.ID, &r.Number, &r.Status, &r.Treatment, &r.Type, &r.RecoverableCategory, &catID,
		&projID, &r.Project, &headID, &r.Head,
		&vendorID, &r.Vendor, &r.VendorGSTIN, &r.VendorPayee, &r.ShortTitle,
		&r.Amount, &r.Purpose, &r.NeededBy,
		&r.InvoiceNo, &r.InvoiceDate, &r.ExpenseDate, &r.AdvanceReason,
		&r.Counterparty, &r.ExpectedReturnDate, &r.RepaymentNotes,
		&urgent, &r.UrgencyReason, &r.AttachmentExceptionReason,
		&r.RequesterID, &r.RequesterName, &r.ManagerID, &r.ManagerName,
		&approvedAmt, &approvedBy, &r.ApprovedByName, &approvedAt,
		&r.DecisionReason, &r.CancelReason, &onHold, &r.HoldReason, &processingBy, &r.ProcessingByName, &processingAt, &concernOpen,
		&reminder, &submitted, &r.CreatedAt, &r.UpdatedAt, &r.PaidAmount)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.Urgent = urgent == 1
	r.OnHold = onHold == 1
	r.ConcernOpen = concernOpen == 1
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

// Request loads one request by id, or ErrNotFound. It does not check who may
// see it; callers apply the request scope.
func (s *Store) Request(ctx context.Context, id int64) (Request, error) {
	return scanRequest(s.db.QueryRowContext(ctx, requestSelect+` WHERE r.id=?`, id))
}

// requestInTx loads one request inside tx, so the checks that follow see the
// row as it is under the transaction's write lock.
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

// nullableID stores a missing (zero or negative) id as SQL NULL.
func nullableID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

// nullableText stores blank text as SQL NULL.
func nullableText(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// CreateRequest raises a new request, which is submitted ('pending') from the
// start: there are no drafts. The row, its number and any documents staged on
// the form are written in one transaction, and the new id is returned.
func (s *Store) CreateRequest(ctx context.Context, actor User, in RequestInput) (int64, error) {
	in, categoryID, err := s.prepareRequestInput(ctx, actor, in)
	if err != nil {
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
	// The numbering settings are read inside this write transaction, so two
	// submits in flight at once take their numbers one after the other.
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := requireDuplicateReasonTx(ctx, tx, in, 0); err != nil {
		return 0, err
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
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "submit", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " submitted request " + number + " for " + money.FormatPaise(in.Amount) + duplicateNote(in),
		After:   map[string]any{"number": number, "type": in.Type, "amount": in.Amount, "manager_id": in.ManagerID, "duplicate_override_reason": strings.TrimSpace(in.DuplicateReason)}}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// ListApprovers returns the active users who may approve a request, never
// including excludeUserID. The request form's approver control is built from
// exactly this list, so a requester's own name is never selectable.
func (s *Store) ListApprovers(ctx context.Context, excludeUserID int64) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT u.id,u.email,u.name,u.password_hash,u.role,u.active,u.created_at,u.updated_at,u.default_approver_id,u.session_version
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

// editableStatuses are the statuses in which the requester may still correct a
// request: while it is pending, or after the approver returned it.
var editableStatuses = map[string]bool{"returned": true, "pending": true}

// UpdateRequest saves the requester's corrections to a pending or returned
// request, validated exactly as a new request is.
func (s *Store) UpdateRequest(ctx context.Context, actor User, id int64, in RequestInput) error {
	in, categoryID, err := s.prepareRequestInput(ctx, actor, in)
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
	Revision   string // optional for trusted store callers; required by the HTTP correction form
	Input      RequestInput
	Attachment *AttachmentInput // nil when the form carried no file
	Resubmit   bool
}

// RequestEditRevision fingerprints a request as a screen showed it, so a later
// form post can be refused if the request changed in between. The fingerprint
// is a hash of the whole row plus the id of the request's newest audit row; the
// audit id catches a change and revert inside the same second, which the row
// hash alone would miss.
func (s *Store) RequestEditRevision(ctx context.Context, req Request) (string, error) {
	return requestEditRevision(ctx, s.db, req)
}

// requestEditRevision computes RequestEditRevision through q (the database or
// a transaction).
func requestEditRevision(ctx context.Context, q rowQuerier, req Request) (string, error) {
	var generation int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM audit_log WHERE entity_type='payment_request' AND entity_id=?`, req.ID).Scan(&generation); err != nil {
		return "", err
	}
	snapshot, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x-%d", sha256.Sum256(snapshot), generation), nil
}

// requireRevision refuses, with msg, a form whose revision no longer matches
// req as it is inside tx. A blank revision never matches.
func requireRevision(ctx context.Context, tx *sql.Tx, req Request, revision, msg string) error {
	current, err := requestEditRevision(ctx, tx, req)
	if err != nil {
		return err
	}
	if revision != current {
		return fmt.Errorf("%w: %s", ErrValidation, msg)
	}
	return nil
}

// EditRequest applies one press of the correction form (edit, optional
// document, optional resubmit) as a single transaction, so either all of it
// and its audit rows are saved or none is. A resubmit that fails its checks
// therefore leaves no half-saved edit behind.
//
// The document is inserted before the resubmit is checked, because the
// attachment policy counts the request's documents: a request whose only
// document is the one being uploaded must pass.
func (s *Store) EditRequest(ctx context.Context, actor User, id int64, e RequestEdit) error {
	in, categoryID, err := s.prepareRequestInput(ctx, actor, e.Input)
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
	if e.Revision != "" {
		current, err := requestInTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := requireRevision(ctx, tx, current, e.Revision, "this request changed since you opened it. Your corrections have not been saved. Open the latest request and review its changes before editing again"); err != nil {
			return err
		}
	}
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

// updateRequestTx writes already-validated corrections inside tx. Only the
// requester may edit, only while the request is pending or returned, and a
// changed vendor invoice is checked for duplicates again. The UPDATE repeats
// the status and requester it read, so a concurrent change refuses the edit.
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
	if before.VendorID == nil || *before.VendorID != in.VendorID || before.InvoiceNo != in.InvoiceNo || before.InvoiceDate != in.InvoiceDate {
		if err := requireDuplicateReasonTx(ctx, tx, in, id); err != nil {
			return err
		}
	}
	// Pending edits reset the reminder timer and reroute to the chosen approver.
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
		Summary: actor.Name + " edited request " + before.Number + duplicateNote(in),
		Before:  before, After: after})
}

// duplicateNote is the audit-summary suffix recorded when the requester
// overrode the duplicate-invoice warning, or "" when there was no override.
func duplicateNote(in RequestInput) string {
	if r := strings.TrimSpace(in.DuplicateReason); r != "" {
		return " — duplicate override: " + r
	}
	return ""
}

// AddRequestComment adds a comment to a request's conversation and returns its
// id. When the accountant holding a partial review replies while a concern is
// open, the reply closes the concern.
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
	before, err := requestInTx(ctx, tx, requestID)
	if err != nil {
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
	// A concern is a question put to the accountant who recorded the shortfall,
	// so their reply is what answers it — not the manager's follow-up and not the
	// requester's. Nothing but the display changes: the request stays in
	// partial_review and the manager still owes the decision.
	if before.Status == "partial_review" && before.ConcernOpen && before.ProcessingBy != nil && *before.ProcessingBy == actor.ID {
		if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET concern_open=0, updated_at=CURRENT_TIMESTAMP WHERE id=?`, requestID); err != nil {
			return 0, err
		}
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

// RequestComments lists a request's comments with their authors, oldest first.
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

// AddRequestAttachment records a document already saved to disk against a
// request and returns its id. Who may upload is the caller's check.
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

// AddHeldRequestAttachment lets the requester answer an Accounts hold ("Need
// GST receipt") with a document. It is the only way to add a document after the
// edit form has closed, so it is allowed only while the request is the actor's
// own and on hold, both checked on the row inside the write transaction
// (Accounts may have lifted the hold since the page was drawn). Nothing else on
// the request changes.
func (s *Store) AddHeldRequestAttachment(ctx context.Context, actor User, requestID int64, in AttachmentInput) (int64, error) {
	if err := validateAttachment(in); err != nil {
		return 0, err
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	req, err := requestInTx(ctx, tx, requestID)
	if err != nil {
		return 0, err
	}
	if req.RequesterID != actor.ID {
		return 0, fmt.Errorf("%w: only the person who raised a request may add documents to it", ErrForbidden)
	}
	if !req.OnHold {
		return 0, fmt.Errorf("%w: this request is not on hold, so no document can be added to it", ErrValidation)
	}
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
