// recovery_events.go records money coming back on a recoverable: a refund, an
// expense reconciled against an advance, or a balance adjustment. Events are
// append-only (database triggers refuse UPDATE and DELETE), can never exceed
// what is outstanding, and a resubmitted form cannot record the same event
// twice.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

// RecoveryInput is one recovery as entered on the form. It records a return or
// accounting reconciliation that has already happened; it never edits a
// payout or posts a budget expense. Token is the form's one-time key that
// makes a retry safe.
type RecoveryInput struct {
	Kind       string
	Amount     int64
	AmountText string
	OccurredOn string
	Reference  string
	Note       string
	Token      string
}

// RecoveryEvent is one recorded recovery on a request.
type RecoveryEvent struct {
	ID, RequestID, Amount, ActorID               int64
	Kind, OccurredOn, Reference, Note, ActorName string
	CreatedAt                                    time.Time
}

// Label is the event kind in words, as the history shows it.
func (e RecoveryEvent) Label() string {
	switch e.Kind {
	case "return":
		return "Money returned"
	case "expense":
		return "Expense reconciled"
	case "adjustment":
		return "Balance adjustment"
	}
	return e.Kind
}

// upRecoveryEvents is the migration that creates the append-only
// recovery_events table.
func upRecoveryEvents(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS recovery_events (
 id INTEGER PRIMARY KEY,
 request_id INTEGER NOT NULL REFERENCES payment_requests(id),
 kind TEXT NOT NULL CHECK(kind IN ('return','expense','adjustment')),
 amount INTEGER NOT NULL CHECK(amount > 0),
 occurred_on TEXT NOT NULL,
 reference TEXT NOT NULL,
 note TEXT NOT NULL,
 actor_id INTEGER NOT NULL REFERENCES users(id),
 actor_name TEXT NOT NULL,
 token TEXT NOT NULL UNIQUE,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
 );
 CREATE INDEX IF NOT EXISTS recovery_events_request ON recovery_events(request_id,id);
 CREATE TRIGGER IF NOT EXISTS recovery_events_no_update BEFORE UPDATE ON recovery_events BEGIN SELECT RAISE(ABORT,'Recovery history is append-only'); END;
 CREATE TRIGGER IF NOT EXISTS recovery_events_no_delete BEFORE DELETE ON recovery_events BEGIN SELECT RAISE(ABORT,'Recovery history is append-only'); END;`)
	return err
}

// recoveryAccessTx re-checks, inside the same transaction as the balance guard,
// that the actor may read (and, when write is set, add to) the recovery history
// of a recoverable request, so revoked privileges cannot race a record.
//
// The answer is ErrNotFound when the request is missing, outside the actor's
// request data scope, or not a recoverable at all: the register only knows
// recoverable requests, and a hidden row must look exactly like a missing one.
// It is ErrForbidden only when the actor can see the request but lacks the
// recoverable_report:view grant, or payment:create for a write.
func recoveryAccessTx(ctx context.Context, tx *sql.Tx, actorID, requestID int64, write bool) error {
	req, ps, err := requestVisibleTx(ctx, tx, actorID, requestID)
	if err != nil {
		return err
	}
	if req.Treatment != "recoverable" {
		return ErrNotFound
	}
	if !ps.Can("recoverable_report", "view") || (write && !ps.Can("payment", "create")) {
		return ErrForbidden
	}
	return nil
}

// validRecoveryDate reports whether value is a real YYYY-MM-DD date no later
// than now's calendar day. now is the caller's local time, so a day that has
// already started locally (ahead of UTC) is accepted, as on the payment form.
func validRecoveryDate(value string, now time.Time) bool {
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return false
	}
	return value <= now.Format("2006-01-02")
}

// RecordRecovery records a recovery on a recoverable request and returns the
// event id. The form's fields are checked first (kind, positive amount, a date
// not in the future, a reference and note within the form's length limits, and
// a valid token). Then, inside one write transaction: the actor must still be
// allowed to record recoveries on this request; a token already used returns
// the first event's id if it is the same entry and is refused if it differs;
// the month must not be locked; and the amount must fit the balance (see
// checkRecoveryBalanceTx). The event and its audit row are written together.
func (s *Store) RecordRecovery(ctx context.Context, actor User, requestID int64, in RecoveryInput) (int64, error) {
	in.Reference, in.Note, in.Token = strings.TrimSpace(in.Reference), strings.TrimSpace(in.Note), strings.TrimSpace(in.Token)
	if in.Kind != "return" && in.Kind != "expense" && in.Kind != "adjustment" {
		return 0, fmt.Errorf("%w: choose a recovery type", ErrValidation)
	}
	if in.Amount <= 0 {
		return 0, fmt.Errorf("%w: enter a positive recovery amount", ErrValidation)
	}
	if !validRecoveryDate(in.OccurredOn, time.Now()) {
		return 0, fmt.Errorf("%w: enter the date the recovery happened, no later than today", ErrValidation)
	}
	if in.Reference == "" || in.Note == "" {
		return 0, fmt.Errorf("%w: a bank or accounting reference and an explanatory note are required", ErrValidation)
	}
	// HTML maxlength counts UTF-16 code units, not UTF-8 bytes.
	if len(utf16.Encode([]rune(in.Reference))) > 240 {
		return 0, fmt.Errorf("%w: keep the bank or accounting reference within 240 characters", ErrValidation)
	}
	if len(utf16.Encode([]rune(in.Note))) > 4000 {
		return 0, fmt.Errorf("%w: keep the evidence and explanation within 4000 characters", ErrValidation)
	}
	if len(in.Token) < 16 || len(in.Token) > 128 {
		return 0, fmt.Errorf("%w: the form has expired; reload and try again", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := recoveryAccessTx(ctx, tx, actor.ID, requestID, true); err != nil {
		return 0, err
	}
	var e RecoveryEvent
	err = tx.QueryRowContext(ctx, `SELECT id,request_id,amount,actor_id,kind,occurred_on,reference,note FROM recovery_events WHERE token=?`, in.Token).Scan(&e.ID, &e.RequestID, &e.Amount, &e.ActorID, &e.Kind, &e.OccurredOn, &e.Reference, &e.Note)
	if err == nil {
		if e.RequestID != requestID {
			return 0, ErrForbidden
		}
		// A retry of the same entry is safe. A different entry under the same
		// token (for example after the browser's Back button) must not be
		// reported as saved while the earlier entry is what was kept.
		want := RecoveryEvent{ID: e.ID, RequestID: requestID, Amount: in.Amount, ActorID: actor.ID,
			Kind: in.Kind, OccurredOn: in.OccurredOn, Reference: in.Reference, Note: in.Note}
		if e != want {
			return 0, fmt.Errorf("%w: this confirmation already recorded a different recovery; reload this page to start a new entry. The existing event is unchanged", ErrValidation)
		}
		return e.ID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if locked, err := isLocked(ctx, tx, in.OccurredOn[:7]); err != nil {
		return 0, err
	} else if locked {
		return 0, ErrLockedMonth
	}
	outstanding, err := checkRecoveryBalanceTx(ctx, tx, requestID, in.Amount, in.OccurredOn)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO recovery_events(request_id,kind,amount,occurred_on,reference,note,actor_id,actor_name,token) VALUES(?,?,?,?,?,?,?,?,?)`, requestID, in.Kind, in.Amount, in.OccurredOn, in.Reference, in.Note, actor.ID, actor.Name, in.Token)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "recovery", EntityType: "payment_request", EntityID: &requestID, Summary: "Recorded " + (RecoveryEvent{Kind: in.Kind}).Label() + " · " + in.Reference, Before: map[string]any{"outstanding": outstanding}, After: map[string]any{"outstanding": outstanding - in.Amount, "amount": in.Amount, "kind": in.Kind, "occurred_on": in.OccurredOn, "reference": in.Reference, "note": in.Note}}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// checkRecoveryBalanceTx refuses a recovery of amount dated occurredOn that
// would take more back than was paid out: more than the balance outstanding
// now, dated before the first payment, more than was outstanding on its date
// (so a backdated event cannot use an installment paid later), or pushing the
// running balance of any later recorded event below zero. It returns the
// balance outstanding before this recovery.
func checkRecoveryBalanceTx(ctx context.Context, tx *sql.Tx, requestID, amount int64, occurredOn string) (int64, error) {
	var paid, recovered int64
	var firstPaid string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0),COALESCE(MIN(paid_on),'') FROM payments WHERE request_id=? AND voided_at IS NULL`, requestID).Scan(&paid, &firstPaid); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM recovery_events WHERE request_id=?`, requestID).Scan(&recovered); err != nil {
		return 0, err
	}
	if paid <= 0 || amount > paid-recovered {
		return 0, fmt.Errorf("%w: recovery cannot exceed the outstanding balance", ErrValidation)
	}
	if occurredOn < firstPaid {
		return 0, fmt.Errorf("%w: recovery date cannot be before the first payment", ErrValidation)
	}
	var paidByDate, recoveredByDate int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM payments WHERE request_id=? AND voided_at IS NULL AND paid_on<=?`, requestID, occurredOn).Scan(&paidByDate); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM recovery_events WHERE request_id=? AND occurred_on<=?`, requestID, occurredOn).Scan(&recoveredByDate); err != nil {
		return 0, err
	}
	if amount > paidByDate-recoveredByDate {
		return 0, fmt.Errorf("%w: recovery exceeds the balance available on that date", ErrValidation)
	}
	var invalid int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM recovery_events e WHERE e.request_id=? AND e.occurred_on>=? AND
 (SELECT COALESCE(SUM(p.amount),0) FROM payments p WHERE p.request_id=e.request_id AND p.voided_at IS NULL AND p.paid_on<=e.occurred_on) <
 (SELECT COALESCE(SUM(r.amount),0) FROM recovery_events r WHERE r.request_id=e.request_id AND r.occurred_on<=e.occurred_on)+?`, requestID, occurredOn, amount).Scan(&invalid); err != nil {
		return 0, err
	}
	if invalid > 0 {
		return 0, fmt.Errorf("%w: that backdated recovery would exceed a later recorded balance", ErrValidation)
	}
	return paid - recovered, nil
}

// RecoveryEvents lists a recoverable's recovery events in date order, after
// checking that the actor may read them (see recoveryAccessTx). The check and
// the list share one read-only snapshot.
func (s *Store) RecoveryEvents(ctx context.Context, actor User, requestID int64) ([]RecoveryEvent, error) {
	tx, err := s.beginReadTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := recoveryAccessTx(ctx, tx, actor.ID, requestID, false); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,request_id,kind,amount,occurred_on,reference,note,actor_id,actor_name,created_at FROM recovery_events WHERE request_id=? ORDER BY occurred_on,id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecoveryEvent
	for rows.Next() {
		var e RecoveryEvent
		if err := rows.Scan(&e.ID, &e.RequestID, &e.Kind, &e.Amount, &e.OccurredOn, &e.Reference, &e.Note, &e.ActorID, &e.ActorName, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
