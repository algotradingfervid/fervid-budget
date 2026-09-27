package store

import (
	"context"
	"database/sql"
	"fervidbudget/internal/money"
	"fmt"
	"strings"
	"time"
)

// ReserveRequest's three refusal causes, distinguished so the conflict screen
// can tell the accountant what actually happened instead of "Someone else took
// this request" for all three (F-D-02). Each wraps ErrForbidden, so every
// existing errors.Is(err, ErrForbidden) branch — including the handler that
// routes reservation refusals to the conflict screen — keeps working; a caller
// that wants the real cause branches with errors.Is on the specific sentinel.
var (
	// ErrAlreadyReserved: the request is in 'processing', held by somebody.
	ErrAlreadyReserved = fmt.Errorf("%w: someone else is already processing this request", ErrForbidden)
	// ErrRequestOnHold: the request is approved but paused; the hold reason is
	// on the request and only Accounts lifts it.
	ErrRequestOnHold = fmt.Errorf("%w: this request is on hold", ErrForbidden)
	// ErrRequestNotApproved: the request is in some other state — pending,
	// completed, cancelled — that offers nothing to reserve.
	ErrRequestNotApproved = fmt.Errorf("%w: only an approved request can be taken for processing", ErrForbidden)
)

// ReserveRequest atomically moves an approved, unclaimed, not-on-hold request to
// 'processing' reserved by actor. The single conditional UPDATE is the
// concurrency guarantee: only the first committer matches, so a losing caller
// sees RowsAffected()==0 (S2, S5, L8). Only then — the race is already lost —
// is the row re-read to name which of the three causes refused it (F-D-02).
func (s *Store) ReserveRequest(ctx context.Context, actor User, id int64) error {
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := paymentRequestScopeTx(ctx, tx, actor.ID, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests
		SET status='processing', processing_by=?, processing_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='approved' AND processing_by IS NULL AND on_hold=0`, actor.ID, id)
	if err != nil {
		return classify(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var status string
		var processingBy sql.NullInt64
		var onHold int
		if err := tx.QueryRowContext(ctx, `SELECT status, processing_by, on_hold FROM payment_requests WHERE id=?`, id).Scan(&status, &processingBy, &onHold); err != nil {
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			return err
		}
		switch {
		// on_hold=1 implies status='approved', so the order cannot misname a
		// held request as taken or vice versa.
		case onHold == 1:
			return ErrRequestOnHold
		case status == "processing" || processingBy.Valid:
			return ErrAlreadyReserved
		default:
			return ErrRequestNotApproved
		}
	}
	// Every payment_request summary written on this side starts with the actor,
	// as the request-module ones do: the detail thread shows the summary as the
	// line's whole title, so a summary that leaves the name out is a line that
	// says what happened and not who did it (L7–L10).
	if err := recordAuditTx(ctx, tx, auditBy(actor, "process", "payment_request", &id, actor.Name+" reserved the request for processing").withChange(nil, map[string]any{"processing_by": actor.ID})); err != nil {
		return err
	}
	return tx.Commit()
}

// ReleaseRequest returns a 'processing' request to 'approved'. confirmed must be
// true ("no payment initiated" — S7) and reason is required (G12): the release is
// visible to the requester and the approver, and the reservation .thread renders
// it. Only the assignee, or an authorized caller (handler resolved
// reservation:release beyond their own reservations), may release (S6). There is
// no auto-release; this is the only path back to approved.
func (s *Store) ReleaseRequest(ctx context.Context, actor User, id int64, reason string, confirmed, authorized bool) error {
	if !confirmed {
		return fmt.Errorf("%w: confirm that no payment was initiated before releasing", ErrValidation)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a reason is required so the requester and approver know why", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var processingBy sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT status, processing_by FROM payment_requests WHERE id=?`, id).Scan(&status, &processingBy); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if status != "processing" {
		return fmt.Errorf("%w: only a processing request can be released", ErrForbidden)
	}
	if !authorized && (!processingBy.Valid || processingBy.Int64 != actor.ID) {
		return fmt.Errorf("%w: only the assignee may release this request", ErrForbidden)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='approved', processing_by=NULL, processing_at=NULL, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='processing'`, id); err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, auditBy(actor, "release", "payment_request", &id, actor.Name+" released the reservation: "+reason).withChange(map[string]any{"processing_by": processingBy.Int64}, nil)); err != nil {
		return err
	}
	return tx.Commit()
}

// ReassignReservation hands a reservation to another user without ever returning
// the request to the open queue (G11): status stays 'processing' and only
// processing_by moves, so no third party can slip in between. authorized is the
// handler-resolved reservation:reassign grant; the store fails closed without it.
// reason is required and is what the reservation .thread renders.
func (s *Store) ReassignReservation(ctx context.Context, actor User, id, toUserID int64, reason string, authorized bool) error {
	if !authorized {
		return fmt.Errorf("%w: reassigning someone else's reservation needs the reassign permission", ErrForbidden)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a reason is required when a reservation changes hands", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var processingBy sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT status, processing_by FROM payment_requests WHERE id=?`, id).Scan(&status, &processingBy); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if status != "processing" || !processingBy.Valid {
		return fmt.Errorf("%w: only a reserved request can be reassigned", ErrForbidden)
	}
	if processingBy.Int64 == toUserID {
		return fmt.Errorf("%w: that person already holds this reservation", ErrValidation)
	}
	var toName string
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT name, active FROM users WHERE id=?`, toUserID).Scan(&toName, &active); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if active == 0 {
		return fmt.Errorf("%w: that user is deactivated", ErrValidation)
	}
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests
		SET processing_by=?, processing_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='processing' AND processing_by=?`, toUserID, id, processingBy.Int64)
	if err != nil {
		return classify(err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("%w: the reservation changed while you were deciding", ErrForbidden)
	}
	if err := recordAuditTx(ctx, tx, auditBy(actor, "reassign", "payment_request", &id,
		actor.Name+" reassigned the reservation to "+toName+": "+reason).withChange(
		map[string]any{"processing_by": processingBy.Int64},
		map[string]any{"processing_by": toUserID})); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordPaymentForRequest atomically appends a payment to the request the actor
// holds. Installments return to approved with the remaining balance payable;
// settled closes, while partial asks the manager to decide a proposed shortfall.
// The cumulative amount cannot exceed approval. Confirmation keys and balance
// snapshots reject replays and stale forms, including after a fresh reservation.
// Payments and their attachments commit together and remain immutable.
//
// F-D-01: the head, the payee and the invoice number are facts of the request —
// a manager approved an amount against a project and head, and the entry screen
// never offers a control to change any of them — so all three are derived from
// the request row this transaction already holds open, and whatever the form
// sent in those fields is ignored. A recoverable request has no head (F-D-11):
// its payment is written with head_id NULL, which the grid and the monthly
// report never match because they join payments on head_id.
func (s *Store) RecordPaymentForRequest(ctx context.Context, actor User, requestID int64, in PaymentInput, settlement, partialReason string, attachment *AttachmentInput) (int64, error) {
	// Electronic and instrument payments need a usable bank/payment reference.
	// Untyped historical store callers retain their compatibility behavior; the HTTP boundary requires a reference for every linked submission.
	switch strings.ToLower(strings.TrimSpace(in.PaymentMode)) {
	case "neft", "rtgs", "upi", "cheque", "card", "dd", "bank_transfer", "cash":
		if strings.TrimSpace(in.ReferenceNo) == "" {
			return 0, fmt.Errorf("%w: enter a transaction or payment reference; spaces alone are not a reference", ErrValidation)
		}
	}

	settlement = strings.TrimSpace(settlement)
	partialReason = strings.TrimSpace(partialReason)
	if settlement != "settled" && settlement != "partial" && settlement != "installment" {
		return 0, fmt.Errorf("%w: choose how the remaining balance should be handled", ErrValidation)
	}
	if settlement == "partial" && partialReason == "" {
		return 0, fmt.Errorf("%w: a reason is required for a partial settlement", ErrValidation)
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
	var status, treatment, displayPayee, invoiceNo string
	var processingBy, headID, vendorID sql.NullInt64
	var requested int64
	var approved sql.NullInt64
	// The display payee, exactly as Request() resolves it: a vendor_invoice
	// names its payee with vendor_id and leaves the snapshot empty.
	//
	// F-G-009: r.vendor_id is read for the same reason head_id is — it is a fact
	// of the request, not of the form — and is written onto the payment so the
	// vendor master can total spend by identity instead of by matching the payee
	// snapshot's text. Migration v10 back-filled the payments that already
	// existed; this is what keeps every payment made from now on linked.
	if err := tx.QueryRowContext(ctx, `SELECT r.status, r.processing_by, r.amount, r.approved_amount,
		r.treatment, r.head_id, r.vendor_id, COALESCE(NULLIF(v.name,''), r.vendor_payee, ''), COALESCE(r.invoice_no,'')
		FROM payment_requests r LEFT JOIN vendors v ON v.id=r.vendor_id
		WHERE r.id=?`, requestID).Scan(&status, &processingBy, &requested, &approved, &treatment, &headID, &vendorID, &displayPayee, &invoiceNo); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if err := paymentRequestScopeTx(ctx, tx, actor.ID, requestID); err != nil {
		return 0, err
	}
	if in.SubmissionKey != "" {
		var priorID int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM payments WHERE request_id=? AND submission_key=? AND entered_by=?`, requestID, in.SubmissionKey, actor.ID).Scan(&priorID)
		if err == nil {
			return 0, fmt.Errorf("%w: this payment confirmation was already recorded", ErrValidation)
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
	}
	if status != "processing" || !processingBy.Valid || processingBy.Int64 != actor.ID {
		return 0, fmt.Errorf("%w: reserve this request before recording its payment", ErrForbidden)
	}
	// F-D-01: overwrite, never compare — the form's copies of these fields are
	// display baggage from the entry screen, not input.
	in.HeadID = headID.Int64 // 0 when the request has no head
	in.VendorPayee = displayPayee
	in.InvoiceNo = invoiceNo
	// Validated on this transaction: reading the month lock and the head on a
	// second pooled connection while this one holds the write lock is how a
	// single settlement came to stack four separate five-second busy waits.
	if err := validatePaymentTx(ctx, tx, in, treatment == "recoverable"); err != nil {
		return 0, err
	}
	var approvedDate string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(substr(approved_at,1,10),'') FROM payment_requests WHERE id=?`, requestID).Scan(&approvedDate); err != nil {
		return 0, err
	}
	if approvedDate == "" || in.PaidOn < approvedDate {
		return 0, fmt.Errorf("%w: payment date cannot precede approval", ErrValidation)
	}
	// G13: the approved amount is a hard ceiling. Paying more is not a settlement
	// decision, it is a different obligation — cancel and raise a new request.
	ceiling := requested
	if approved.Valid {
		ceiling = approved.Int64
	}
	var previouslyPaid int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM payments WHERE request_id=? AND voided_at IS NULL`, requestID).Scan(&previouslyPaid); err != nil {
		return 0, err
	}
	if in.ExpectedPaid != nil && *in.ExpectedPaid != previouslyPaid {
		return 0, fmt.Errorf("%w: another payment was recorded after this form opened; reopen the payment form to use the current balance", ErrValidation)
	}
	remaining := ceiling - previouslyPaid
	if in.Amount < remaining && settlement == "settled" && partialReason == "" {
		return 0, fmt.Errorf("%w: explain the deduction or agreed adjustment before closing a short payment", ErrValidation)
	}
	if in.Amount > remaining {
		if previouslyPaid > 0 {
			return 0, fmt.Errorf("%w: %s exceeds the remaining approved balance of %s; additional spending needs a separately approved request", ErrValidation, money.FormatPaise(in.Amount), money.FormatPaise(remaining))
		}
		return 0, fmt.Errorf("%w: %s is more than the approved %s — to pay more, cancel this request and raise a new one",
			ErrValidation, money.FormatPaise(in.Amount), money.FormatPaise(remaining))
	}
	if in.Amount == remaining && settlement == "partial" {
		return 0, fmt.Errorf("%w: this payment clears the remaining balance; choose fully settled instead of shortfall review", ErrValidation)
	}
	// A zero head is stored as NULL, never 0 — heads(id) has no row 0, and the
	// v8 schema keeps the foreign key.
	var headArg any
	if in.HeadID != 0 {
		headArg = in.HeadID
	}
	// Same treatment for the vendor: NULL, never 0. A reimbursement or an
	// employee advance names no vendor row at all, and vendors(id) has no row 0.
	var vendorArg any
	if vendorID.Valid && vendorID.Int64 != 0 {
		vendorArg = vendorID.Int64
	}
	storedSettlement := settlement
	if settlement == "installment" {
		storedSettlement = "partial"
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO payments(head_id,vendor_id,paid_on,amount,vendor_payee,payment_mode,invoice_no,reference_no,remarks,entered_by,request_id,settlement,partial_reason,submission_key)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		headArg, vendorArg, in.PaidOn, in.Amount, in.VendorPayee, in.PaymentMode, in.InvoiceNo, in.ReferenceNo, in.Remarks, actor.ID, requestID, storedSettlement, partialReason, nullableText(in.SubmissionKey))
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	newStatus := "completed"
	if settlement == "partial" {
		newStatus = "partial_review"
	}
	if settlement == "installment" && in.Amount < remaining {
		newStatus = "approved"
	}
	upd, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status=?, processing_by=CASE WHEN ?='approved' THEN NULL ELSE processing_by END, processing_at=CASE WHEN ?='approved' THEN NULL ELSE processing_at END, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='processing' AND processing_by=?`, newStatus, newStatus, newStatus, requestID, actor.ID)
	if err != nil {
		return 0, err
	}
	if n, err := upd.RowsAffected(); err != nil {
		return 0, err
	} else if n == 0 {
		return 0, fmt.Errorf("%w: reservation was lost before settlement", ErrForbidden)
	}
	after := paymentFromInput(id, actor, in)
	after.RequestID = &requestID
	after.Settlement = storedSettlement
	after.PartialReason = partialReason
	if err := recordAuditTx(ctx, tx, auditBy(actor, "create", "payment", &id, "Recorded payment "+money.FormatPaise(in.Amount)).withChange(nil, after)); err != nil {
		return 0, err
	}
	reqAction, reqSummary := "settle", actor.Name+" settled the request as completed"
	if settlement == "partial" {
		reqAction, reqSummary = "mark_partial", actor.Name+" recorded a partial settlement: "+partialReason
	}
	if settlement == "installment" {
		reqAction, reqSummary = "installment", actor.Name+" recorded an installment; "+money.FormatPaise(remaining-in.Amount)+" remains approved for payment"
	}
	if err := recordAuditTx(ctx, tx, auditBy(actor, reqAction, "payment_request", &requestID, reqSummary).withChange(nil, map[string]any{"status": newStatus, "payment_id": id})); err != nil {
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

// AcceptPartial closes a 'partial_review' request as 'completed_partial' — a
// terminal state distinct from a clean 'completed' (S11, G14). The difference is
// permanent and visible: the ledger renders .pill.completed-partial, and anyone
// reading the request later can see that a balance was written off rather than
// paid. note is optional and joins the trail the requester reads.
func (s *Store) AcceptPartial(ctx context.Context, actor User, id int64, note string) error {
	note = strings.TrimSpace(note)
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Whose decision this is, asked here and not only by the template. Holding
	// approval:accept_partial says a person may accept a shortfall; it never
	// says whose. ApproveRequest and decideRequest both refuse an actor who is
	// not the request's own manager, and writing a balance off for good is the
	// last place to relax that.
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	// concern_open goes with it: the discussion ends with the decision.
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='completed_partial', concern_open=0, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='partial_review'`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("%w: only a partial-review request can be accepted", ErrForbidden)
	}
	summary := actor.Name + " accepted the partial settlement — completed, partial accepted"
	if note != "" {
		summary += ": " + note
	}
	if err := recordAuditTx(ctx, tx, auditBy(actor, "accept_partial", "payment_request", &id, summary).withChange(nil, map[string]any{"status": "completed_partial"})); err != nil {
		return err
	}
	return tx.Commit()
}

// RaiseConcern keeps a 'partial_review' request in review and appends a
// conversation comment (S11). comment is required.
func (s *Store) RaiseConcern(ctx context.Context, actor User, id int64, comment string) error {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return fmt.Errorf("%w: a concern comment is required", ErrValidation)
	}
	// Reads the request, then writes a comment — same shape, same fix.
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	// Disputing a shortfall is the other half of accepting it, so it answers to
	// the same owner: the manager the request was routed to, not everybody who
	// happens to hold the verb.
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	if before.Status != "partial_review" {
		return fmt.Errorf("%w: only a partial-review request can receive a concern", ErrForbidden)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO request_comments(request_id,author_id,body) VALUES(?,?,?)`, id, actor.ID, comment); err != nil {
		return classify(err)
	}
	// The concern is now open until the holder answers (AddRequestComment
	// clears it). The status is untouched: "under discussion" is how the
	// request is displayed, not a state it moves to.
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET concern_open=1, updated_at=CURRENT_TIMESTAMP WHERE id=?`, id); err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, auditBy(actor, "concern", "payment_request", &id, "Raised concern: "+comment)); err != nil {
		return err
	}
	return tx.Commit()
}

// HoldRequest pauses an approved, not-already-held request (L7). reason required.
func (s *Store) HoldRequest(ctx context.Context, actor User, id int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a hold reason is required", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET on_hold=1, hold_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='approved' AND on_hold=0`, reason, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("%w: only an approved, not-already-held request can be held", ErrForbidden)
	}
	if err := recordAuditTx(ctx, tx, auditBy(actor, "hold", "payment_request", &id, actor.Name+" put the request on hold: "+reason)); err != nil {
		return err
	}
	return tx.Commit()
}

// UnholdRequest lifts a hold. The route gate (payment.hold) restricts this to
// Accounts (L7).
func (s *Store) UnholdRequest(ctx context.Context, actor User, id int64) error {
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET on_hold=0, hold_reason='', updated_at=CURRENT_TIMESTAMP WHERE id=? AND on_hold=1`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("%w: request is not on hold", ErrForbidden)
	}
	if err := recordAuditTx(ctx, tx, auditBy(actor, "unhold", "payment_request", &id, actor.Name+" lifted the hold")); err != nil {
		return err
	}
	return tx.Commit()
}

// LinkablePaymentRequests powers the Accounts queue and the request picker.
// Available is the S3 set — approved · unclaimed · not on hold — the only rows
// that may be reserved. Unavailable is everything else the tab asked for that
// the caller may see but not take. Both are searchable by number / requester /
// payee / project / head / amount (S4). Counts always span the caller's scope.
func (s *Store) LinkablePaymentRequests(ctx context.Context, opts LinkableOptions) (LinkableSet, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	// Staleness is measured against an injected clock, never julianday('now'):
	// a test must be able to decide what "26 hours ago" means. CURRENT_TIMESTAMP
	// writes UTC in this format, so the cutoff compares as plain text.
	staleCutoff := now.UTC().Add(-StaleReservation).Format("2006-01-02 15:04:05")
	var out LinkableSet

	// The queue shows the caller exactly the requests their request data scope
	// lets them open, and nothing else. An empty or unrecognised scope reads
	// nothing.
	scopePredicate, scopeArgs := requestScopeSQL("r", opts.Scope, opts.ViewerID)
	scopeSQL := ` AND ` + scopePredicate

	// Counts first: one aggregate pass over the scope, independent of the tab,
	// the search and the limit. Stale reservations are counted here too, because
	// the banner is rendered on tabs whose rows do not include them.
	countQ := `SELECT
		COALESCE(SUM(CASE WHEN r.status='approved' AND r.processing_by IS NULL AND r.on_hold=0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='approved' AND r.processing_by IS NULL AND r.on_hold=0 THEN MAX(0,COALESCE(r.approved_amount, r.amount)-(SELECT COALESCE(SUM(ip.amount),0) FROM payments ip WHERE ip.request_id=r.id AND ip.voided_at IS NULL)) ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='processing' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.on_hold=1 AND r.status='approved' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='partial_review' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status IN ('completed','completed_partial') THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='processing' AND r.processing_by=? THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='processing' AND r.processing_by IS NOT NULL AND r.processing_by<>? THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='processing' AND r.processing_by=? AND r.processing_at IS NOT NULL AND r.processing_at<=? THEN 1 ELSE 0 END),0)
		FROM payment_requests r WHERE 1=1` + scopeSQL
	countArgs := append([]any{opts.ViewerID, opts.ViewerID, opts.ViewerID, staleCutoff}, scopeArgs...)
	if err := s.db.QueryRowContext(ctx, countQ, countArgs...).Scan(
		&out.Counts.Approved, &out.Counts.ApprovedAmount, &out.Counts.Processing,
		&out.Counts.Hold, &out.Counts.PartialReview, &out.Counts.Paid,
		&out.Counts.ReservedByMe, &out.Counts.ReservedByOthers, &out.Counts.StaleReservations); err != nil {
		return out, err
	}

	// The rows are full requests, read exactly as Request() reads them, so the
	// queue shows the same display payee, short title and paid amount as the
	// request's own screen.
	q := requestSelect + `
		WHERE `
	var args []any
	switch opts.Status {
	case "":
		// The approved pool: what may be taken, plus what has been taken out of
		// it. The picker needs the second half to render .co.is-taken rows rather
		// than silently hiding a request someone is already paying.
		q += `r.status IN ('approved','processing')`
	case "approved":
		// The queue tab promises the same unclaimed, actionable set as its
		// Approved count. The payment picker uses the broader default above.
		q += `r.status='approved' AND r.processing_by IS NULL AND r.on_hold=0`
	case "processing":
		q += `r.status='processing'`
	case "hold":
		// A hold pauses an approved request and every writer that moves a
		// request off 'approved' clears it, so the status test is belt and
		// braces: it stops a row written before that rule existed from sitting
		// in this tab for ever, being offered a reply that can change nothing.
		q += `r.on_hold=1 AND r.status='approved'`
	case "partial_review":
		q += `r.status='partial_review'`
	case "paid":
		q += `r.status IN ('completed','completed_partial')`
	default:
		return out, fmt.Errorf("%w: unknown queue tab %q", ErrValidation, opts.Status)
	}
	q += scopeSQL
	args = append(args, scopeArgs...)
	if search := strings.ToLower(strings.TrimSpace(opts.Query)); search != "" {
		// The payee is searched as it is displayed: typing a vendor's name must
		// find the row that shows that name.
		//
		// F-D-04: so is the amount. r.amount is int64 paise, so matching the
		// search text against CAST(r.amount AS TEXT) meant `7,431.00` — the only
		// form of the figure that appears on any screen, since every rendering
		// goes through money.FormatPaise — never found ₹7,431.00, while 743100
		// did. The text match is kept, because a substring of the paise integer
		// is still a useful needle and TC-D-013 pins that 743100 works; what is
		// added is an exact paise comparison whenever the needle parses as money.
		// money.ParsePaise already strips ₹, commas and whitespace, so it is the
		// right normaliser and the two cannot drift.
		amountClause := ` OR CAST(r.amount AS TEXT) LIKE ? ESCAPE '\'`
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
		needle := "%" + esc + "%"
		extra := []any{needle, needle, needle, needle, needle, needle}
		if paise, err := money.ParsePaise(search); err == nil {
			amountClause += ` OR r.amount = ?`
			extra = append(extra, paise)
		}
		q += ` AND (lower(r.number) LIKE ? ESCAPE '\' OR lower(ru.name) LIKE ? ESCAPE '\' OR lower(COALESCE(NULLIF(v.name,''),r.vendor_payee,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(p.name,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(h.name,'')) LIKE ? ESCAPE '\'` + amountClause + `)`
		args = append(args, extra...)
	}
	// CURRENT_TIMESTAMP is second-resolution, so a burst of approvals shares one
	// timestamp; id breaks the tie and keeps the order stable.
	//
	// Urgent first, as every other request list sorts: the design's approved
	// stage reads "In the Accounts queue. Urgent requests surface here", and
	// the dashboard's Accounts area shows only the first few of these rows, so
	// an urgent request behind older ordinary ones did not surface at all
	// (urgent-1). The Paid tab is history, not work, and keeps approval order.
	if opts.Status == "paid" {
		q += ` ORDER BY r.approved_at, r.id`
	} else {
		q += ` ORDER BY r.urgent DESC, r.approved_at, r.id`
	}
	if opts.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, opts.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return out, err
		}
		// Availability is re-derived from the row, never from the tab, so no tab
		// can ever hand the UI a "Take for processing" button it must not have.
		if r.Status == "approved" && r.ProcessingBy == nil && !r.OnHold {
			out.Available = append(out.Available, r)
		} else {
			out.Unavailable = append(out.Unavailable, r)
		}
	}
	return out, rows.Err()
}
