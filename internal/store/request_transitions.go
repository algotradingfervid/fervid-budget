package store

import (
	"context"
	"database/sql"
	"fervidbudget/internal/money"
	"fmt"
	"strings"
	"time"
)

// Writers guard their UPDATE with the state they expect and check RowsAffected,
// the shape ReserveRequest uses: the write lock (taken at BEGIN, see tx.go)
// decides who goes first, the condition decides whether going second still
// makes sense.

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

func (s *Store) ReturnRequest(ctx context.Context, actor User, id int64, comment string, revisions ...string) error {
	return s.decideRequest(ctx, actor, id, "returned", "return", comment, "a comment is required to return a request", revisions...)
}

func (s *Store) RejectRequest(ctx context.Context, actor User, id int64, reason string, revisions ...string) error {
	return s.decideRequest(ctx, actor, id, "rejected", "reject", reason, "a reason is required to reject a request", revisions...)
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

// ReassignRequest hands a request to a different approver. Whether the actor
// is entitled to is the caller's question, and its answer arrives as
// asAdministrator: false means the actor is the request's own approver handing
// it on, true means a holder of the admin-level grant rescuing one whose
// approver cannot act. The store asks the first case's question again on the
// row it is about to change — the caller decided it on a read made before this
// transaction, and between the two an administrator may already have moved the
// request, in which case the actor is a former approver and has no say (rbac-8
// review). The UPDATE matches the approver it read for the same reason.
//
// One rule is the store's whoever the actor is: nobody hands a request to
// themselves. A manager who could not decide a request must not be able to
// make it theirs and then decide it (A5).
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
	if !asAdministrator && before.ManagerID != actor.ID {
		return fmt.Errorf("%w: this request is with %s now, so it is not yours to reassign", ErrForbidden, before.ManagerName)
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
	// Installments can return to approved after money has left. Cancellation
	// must never hide that paid obligation or its recoverable balance.
	if before.PaidAmount > 0 {
		return fmt.Errorf("%w: a payment has already been recorded; continue the remaining balance or resolve it through partial review instead of cancelling", ErrValidation)
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
func (s *Store) DecideCancellation(ctx context.Context, actor User, id int64, accept bool, note string, revisions ...string) error {
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
	// Installments can return to approved after money has left. Cancellation
	// must never hide that paid obligation or its recoverable balance.
	if accept && before.PaidAmount > 0 {
		return fmt.Errorf("%w: a payment has already been recorded; continue the remaining balance or resolve it through partial review instead of cancelling", ErrValidation)
	}
	// Like approve/return/reject, the decision holds only for the request the
	// approver was looking at: an edit, comment or other change since the
	// screen was drawn refuses it.
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

// CancelRequest is G2: the approver cancels an approved request outright,
// without the requester having asked. A reason is always required.
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
	// Installments can return to approved after money has left. Cancellation
	// must never hide that paid obligation or its recoverable balance.
	if before.PaidAmount > 0 {
		return fmt.Errorf("%w: a payment has already been recorded; continue the remaining balance or resolve it through partial review instead of cancelling", ErrValidation)
	}
	// Bound to the revision the approver saw, as approve/return/reject are.
	if err := checkDecisionRevision(ctx, tx, before, revisions); err != nil {
		return err
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

// HTTP decisions always supply the revision displayed on the reviewed form.
func checkDecisionRevision(ctx context.Context, tx *sql.Tx, req Request, revisions []string) error {
	if len(revisions) == 0 {
		return nil
	} // Trusted internal callers have no browser snapshot.
	current, err := requestEditRevision(ctx, tx, req)
	if err != nil {
		return err
	}
	if revisions[0] == "" || revisions[0] != current {
		return fmt.Errorf("%w: this request changed; reload and review it before deciding", ErrValidation)
	}
	return nil
}

// ContinuePartial returns a manager-reviewed shortfall to the existing approval.
// It never raises the approved ceiling or alters any recorded payment.
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
