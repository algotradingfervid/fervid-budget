// request_transitions.go moves a payment request between statuses: resubmit,
// withdraw, approve, return, reject, reassign, re-raise, the cancellation
// paths, and continuing a partly paid request.
//
// Every transition follows the same pattern. It opens a write transaction
// (which holds SQLite's write lock from BEGIN, see tx.go), re-reads the request,
// checks who is acting and whether the move is allowed from the current status,
// then runs an UPDATE whose WHERE repeats the status (and owner) it just read
// and checks that exactly one row changed (affectedOne). The lock decides who
// goes first; the repeated condition decides whether going second still makes
// sense. Each transition writes an audit row in the same transaction.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"fervidbudget/internal/money"
)

// ErrRequestRaced is the refusal for losing a write race: the row changed
// between the read that authorised this call and the conditional UPDATE. It is
// a validation error, so the person is told to reload instead of seeing an
// internal-error page.
var ErrRequestRaced = fmt.Errorf("%w: somebody else changed this request a moment ago — reload it and look again", ErrValidation)

// affectedOne turns a conditional UPDATE that changed no row into
// ErrRequestRaced.
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

// requestStatuses lists every value payment_requests.status can hold, in
// workflow order. There is no 'draft': a request exists only once submitted,
// and the table's CHECK constraint forbids the value. 'On hold' is not a status
// either: it is the on_hold flag on an approved request.
var requestStatuses = map[string]bool{
	"pending": true, "returned": true, "approved": true, "rejected": true,
	"withdrawn": true, "cancellation_requested": true, "cancelled": true,
	"processing": true, "partial_review": true,
	"completed": true, "completed_partial": true,
}

// legalTransitions says which status changes canTransition allows. Final
// states (rejected, withdrawn, cancelled, completed, completed_partial) have
// no entry. 'processing' has no entry either, but it is not final: Accounts'
// reservation and settlement code moves requests in and out of it with its own
// guarded UPDATEs rather than through this table.
var legalTransitions = map[string]map[string]bool{
	"returned": {"pending": true},
	"pending":  {"approved": true, "returned": true, "rejected": true, "withdrawn": true},
	// After approval the requester may ask for cancellation (freezing payment),
	// or the approver may cancel outright.
	"approved": {"cancellation_requested": true, "cancelled": true},
	// cancellation_requested -> approved is used only when the approver declines
	// the cancellation (DecideCancellation), never by ApproveRequest.
	"cancellation_requested": {"cancelled": true, "approved": true},
	// A shortfall the approver accepts closes as completed_partial, so the
	// record keeps "paid in full" and "balance written off" apart.
	"partial_review": {"completed_partial": true},
}

// canTransition reports whether legalTransitions allows moving from one status
// to another.
func canTransition(from, to string) bool {
	return legalTransitions[from][to]
}

// statusPhrase names a request in a status the way a refusal reads it, with
// the right article and plain words ("an approved request", "a request with
// Accounts") instead of the raw status code.
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

// refuseIfPaid stops a cancellation once any money has been paid. After an
// installment a request can be back in 'approved', and cancelling it would hide
// the paid amount and any balance still to recover.
func refuseIfPaid(r Request) error {
	if r.PaidAmount > 0 {
		return fmt.Errorf("%w: a payment has already been recorded; continue the remaining balance or resolve it through partial review instead of cancelling", ErrValidation)
	}
	return nil
}

// checkDecisionRevision binds an approver's decision to the request as their
// screen showed it. When a revision is passed, a blank or out-of-date one is
// refused. Passing none skips the check; only trusted callers (seed data and
// tests) do that. HTTP handlers use the ...AtRevision methods below, whose
// revision argument cannot be left out.
func checkDecisionRevision(ctx context.Context, tx *sql.Tx, req Request, revisions []string) error {
	if len(revisions) == 0 {
		return nil
	}
	return requireRevision(ctx, tx, req, revisions[0], "this request changed; reload and review it before deciding")
}

// The approver's decisions as the HTTP handlers make them. Each one takes the
// revision the approver's screen showed as a required argument and always
// checks it, so a handler cannot drop it by accident: a blank or out-of-date
// revision is refused with ErrValidation.

// ApproveRequestAtRevision is ApproveRequest bound to the reviewed revision.
func (s *Store) ApproveRequestAtRevision(ctx context.Context, actor User, id, approvedAmount int64, note, revision string) error {
	return s.ApproveRequest(ctx, actor, id, approvedAmount, note, revision)
}

// ReturnRequestAtRevision is ReturnRequest bound to the reviewed revision.
func (s *Store) ReturnRequestAtRevision(ctx context.Context, actor User, id int64, comment, revision string) error {
	return s.ReturnRequest(ctx, actor, id, comment, revision)
}

// RejectRequestAtRevision is RejectRequest bound to the reviewed revision.
func (s *Store) RejectRequestAtRevision(ctx context.Context, actor User, id int64, reason, revision string) error {
	return s.RejectRequest(ctx, actor, id, reason, revision)
}

// DecideCancellationAtRevision is DecideCancellation bound to the reviewed
// revision.
func (s *Store) DecideCancellationAtRevision(ctx context.Context, actor User, id int64, accept bool, note, revision string) error {
	return s.DecideCancellation(ctx, actor, id, accept, note, revision)
}

// CancelRequestAtRevision is CancelRequest bound to the reviewed revision.
func (s *Store) CancelRequestAtRevision(ctx context.Context, actor User, id int64, reason, revision string) error {
	return s.CancelRequest(ctx, actor, id, reason, revision)
}

// SubmitRequest resubmits a request the approver returned for correction. It
// is the only way back into 'pending' after a request is raised.
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

// submitRequestTx is SubmitRequest inside the caller's transaction, so
// EditRequest can save corrections and resubmit as one unit. It re-reads the
// row through tx, so after an edit it checks the edited row. Only the requester
// may resubmit, and the attachment policy is checked against the documents the
// request has now.
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

// WithdrawRequest lets the requester take back a request that is still
// pending. A withdrawn request is final.
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

// ApproveRequest records the approver's approval and the amount approved.
// Only the request's approver may approve, never the requester, and only from
// 'pending'. The approved amount is the most Accounts may pay, so it may be
// lower than the amount asked for but never higher. A request whose required-by
// month is locked cannot be approved. revisions binds the decision to the
// request as the approver saw it (see checkDecisionRevision).
func (s *Store) ApproveRequest(ctx context.Context, actor User, id, approvedAmount int64, note string, revisions ...string) error {
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
	// Last line of defence against self-approval: the form never offers it and
	// validateRequestInput refuses it, but a row could still reach this state.
	if before.RequesterID == actor.ID {
		return ErrForbidden
	}
	// Not canTransition: the table also allows cancellation_requested ->
	// approved, but that move belongs to DecideCancellation, which needs its own
	// grant, a written reason and its own audit entry.
	if before.Status == "cancellation_requested" {
		return fmt.Errorf("%w: this request is waiting on your cancellation decision — decide that first, and declining it is what returns it to approved", ErrValidation)
	}
	if before.Status != "pending" {
		return fmt.Errorf("%w: %s cannot be approved", ErrValidation, statusPhrase(before.Status))
	}
	// The approved amount can be lowered but never raised: approving more than
	// was asked for would need a new request.
	if approvedAmount > before.Amount {
		return fmt.Errorf("%w: you cannot approve more than the %s that was requested — approve up to that amount, or cancel this request and ask for a new one", ErrValidation, money.FormatPaise(before.Amount))
	}
	if err := checkDecisionRevision(ctx, tx, before, revisions); err != nil {
		return err
	}
	if len(before.NeededBy) >= 7 {
		locked, e := isLocked(ctx, tx, before.NeededBy[:7])
		if e != nil {
			return e
		}
		if locked {
			return ErrLockedMonth
		}
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

// decideRequest applies an approver's return or reject decision (to is
// "returned" or "rejected"). A written reason is required; missingMsg is the
// refusal when it is blank. Only the request's approver may decide.
func (s *Store) decideRequest(ctx context.Context, actor User, id int64, to, action, reason, missingMsg string, revisions ...string) error {
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
	if err := checkDecisionRevision(ctx, tx, before, revisions); err != nil {
		return err
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

// ReturnRequest sends a pending request back to the requester for correction,
// with a comment saying what to fix.
func (s *Store) ReturnRequest(ctx context.Context, actor User, id int64, comment string, revisions ...string) error {
	return s.decideRequest(ctx, actor, id, "returned", "return", comment, "a comment is required to return a request", revisions...)
}

// RejectRequest closes a pending request as rejected, with a reason. The
// requester can re-raise it as a new request (ReraiseRequest).
func (s *Store) RejectRequest(ctx context.Context, actor User, id int64, reason string, revisions ...string) error {
	return s.decideRequest(ctx, actor, id, "rejected", "reject", reason, "a reason is required to reject a request", revisions...)
}

// reassignableStatuses are the states in which a request is waiting on its
// approver, and so can be handed to another. They match the states the
// deactivate-user warning lists (approverOpenStatuses), so every request that
// warning tells the administrator to reassign can in fact be reassigned.
var reassignableStatuses = map[string]bool{
	"pending": true, "returned": true, "cancellation_requested": true, "partial_review": true,
}

// Reassignable reports whether ReassignRequest would move a request in this
// status, so a screen can withhold the control where the POST would refuse.
func Reassignable(status string) bool { return reassignableStatuses[status] }

// ReassignRequest hands a request to a different approver, with a reason.
//
// Whether the actor may do so is decided by the caller and passed as
// asAdministrator: false means the actor is the request's own approver handing
// it on, true means an administrator rescuing a request whose approver cannot
// act. For the first case the store checks again, on the row inside the
// transaction, that the actor is still the approver, because an administrator
// may have moved the request since the caller looked. Whoever the actor is,
// nobody may reassign a request to themselves or to its requester, and the new
// approver must be able to approve.
func (s *Store) ReassignRequest(ctx context.Context, actor User, id, newManagerID int64, reason string, asAdministrator bool) error {
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
	if !asAdministrator && before.ManagerID != actor.ID {
		return fmt.Errorf("%w: this request is with %s now, so it is not yours to reassign", ErrForbidden, before.ManagerName)
	}
	if newManagerID == before.RequesterID {
		return fmt.Errorf("%w: a request cannot be reassigned to its own requester", ErrValidation)
	}
	// The reason is written to decision_reason only while the request is
	// pending. On a returned request that column holds the correction the
	// requester is reading, and on a frozen or partial one the approval note;
	// the reason is in the audit row either way.
	reasonSQL := `decision_reason=?, `
	args := []any{newManagerID, reason, id, before.Status, before.ManagerID}
	if before.Status != "pending" {
		reasonSQL = ``
		args = []any{newManagerID, id, before.Status, before.ManagerID}
	}
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET manager_id=?, `+reasonSQL+`reminder_last_sent=NULL, updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND status=? AND manager_id=?`, args...)
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
		// Not "reassign": that action name means handing over an Accounts
		// reservation on the same entity type.
		Action: "approval_reassign", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " reassigned request " + before.Number + " to " + after.ManagerName + ": " + reason,
		Before:  before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

// ReraiseRequest copies the requester's own rejected request into a new
// request, which is pending from the start with its own number. It returns the
// new request's id.
func (s *Store) ReraiseRequest(ctx context.Context, actor User, id int64) (int64, error) {
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

// RequestCancellation lets the requester ask for an approved request to be
// cancelled, with a reason. Payment freezes at once, because Accounts only
// reserve requests that are 'approved'. The approver then accepts or declines
// (DecideCancellation). A request with money already paid cannot be cancelled.
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
	if err := refuseIfPaid(before); err != nil {
		return err
	}
	// An Accounts hold is suspended, not removed. on_hold=1 must imply
	// status='approved' (the hold tab, the hold pill and ReserveRequest rely on
	// it), so the flag is cleared. hold_reason is kept: it is the accountant's
	// unanswered question, and DecideCancellation restores the hold from it if
	// the cancellation is declined. Screens show a hold only when on_hold=1, so
	// the kept reason is not displayed meanwhile.
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
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "cancel_request", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " asked for cancellation of " + before.Number + ": " + reason + ". Payment frozen.",
		Before:  before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

// DecideCancellation is the approver's answer to a cancellation request.
// Accepting cancels the request for good (and is refused once money has been
// paid). Declining returns it to 'approved' and needs a written reason, because
// Accounts and the requester both read it. The decision is bound to the
// revision the approver saw, like approve, return and reject.
func (s *Store) DecideCancellation(ctx context.Context, actor User, id int64, accept bool, note string, revisions ...string) error {
	note = strings.TrimSpace(note)
	if !accept && note == "" {
		return fmt.Errorf("%w: say why it should still be paid", ErrValidation)
	}
	// Accepting a cancellation is cancelling the request, so it is recorded
	// under the same "cancel" action as an outright cancel; the summary says
	// who asked for it.
	to, action := "cancelled", "cancel"
	if !accept {
		to, action = "approved", "cancel_decline"
	}
	// Accepting clears any suspended Accounts hold, since a cancelled request
	// cannot be on hold. Declining restores the hold that RequestCancellation
	// suspended, if there was one: only Accounts lifts a hold.
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
	if accept {
		if err := refuseIfPaid(before); err != nil {
			return err
		}
	}
	if err := checkDecisionRevision(ctx, tx, before, revisions); err != nil {
		return err
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

// CancelRequest lets the approver cancel an approved request outright, without
// the requester asking. A reason is required, a request with money already paid
// cannot be cancelled, and the decision is bound to the revision the approver
// saw. Any Accounts hold is cleared, since a cancelled request cannot be on hold.
func (s *Store) CancelRequest(ctx context.Context, actor User, id int64, reason string, revisions ...string) error {
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
	if err := refuseIfPaid(before); err != nil {
		return err
	}
	if err := checkDecisionRevision(ctx, tx, before, revisions); err != nil {
		return err
	}

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

// ContinuePartial is the approver's answer to a partial review when the rest
// of the approved amount should still be paid: the request goes back to
// 'approved' and Accounts can pay the balance. It never raises the approved
// amount or changes a recorded payment. The actor must still be able to see the
// request, be its approver, and there must be a balance left to pay.
func (s *Store) ContinuePartial(ctx context.Context, actor User, id int64, note string) error {
	if strings.TrimSpace(note) == "" {
		return fmt.Errorf("%w: explain when the remaining balance should be paid", ErrValidation)
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
	if err := paymentRequestScopeTx(ctx, tx, actor.ID, id); err != nil {
		return err
	}
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	if before.Status != "partial_review" {
		return fmt.Errorf("%w: only a partial review can return to payment", ErrValidation)
	}
	ceiling := before.Amount
	if before.ApprovedAmount != nil {
		ceiling = *before.ApprovedAmount
	}
	if before.PaidAmount >= ceiling {
		return fmt.Errorf("%w: there is no approved balance left to pay", ErrValidation)
	}
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='approved',processing_by=NULL,processing_at=NULL,concern_open=0,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='partial_review'`, id)
	if err != nil {
		return err
	}
	if err := affectedOne(res); err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "continue_payment", EntityType: "payment_request", EntityID: &id, Summary: actor.Name + " kept the approved balance payable: " + strings.TrimSpace(note)}); err != nil {
		return err
	}
	return tx.Commit()
}
