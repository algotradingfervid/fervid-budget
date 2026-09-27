package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

// RecoveryInput records an already-completed return or documented accounting
// reconciliation. It never edits a payout or silently posts a budget expense.
type RecoveryInput struct {
	Kind       string
	Amount     int64
	AmountText string
	OccurredOn string
	Reference  string
	Note       string
	Token      string
}

type RecoveryEvent struct {
	ID, RequestID, Amount, ActorID               int64
	Kind, OccurredOn, Reference, Note, ActorName string
	CreatedAt                                    time.Time
}

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

// recoveryAccessTx rechecks grants, activity and row scope within the same write
// transaction as the balance guard, so revoked privileges cannot race a record.
func recoveryAccessTx(ctx context.Context, tx *sql.Tx, actorID, requestID int64, write bool) error {
	var allowed int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_requests pr JOIN users u ON u.id=? AND u.active=1
 WHERE pr.id=? AND pr.treatment='recoverable'
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id WHERE ur.user_id=u.id AND rp.resource='recoverable_report' AND rp.action='view')
 AND (?=0 OR EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id WHERE ur.user_id=u.id AND rp.resource='payment' AND rp.action='create'))
 AND EXISTS(SELECT 1 FROM user_roles ur JOIN role_data_scope ds ON ds.role_id=ur.role_id WHERE ur.user_id=u.id AND ds.resource='request'
 AND (ds.scope='all' OR (ds.scope='own' AND pr.requester_id=u.id) OR (ds.scope='assigned' AND (pr.manager_id=u.id OR pr.requester_id=u.id))))`, actorID, requestID, boolInt(write)).Scan(&allowed)
	if err != nil {
		return err
	}
	if allowed == 0 {
		return ErrForbidden
	}
	return nil
}

// Match the application's payment calendar, including local days ahead of UTC.
func validRecoveryDate(value string, now time.Time) bool {
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return false
	}
	return value <= now.Format("2006-01-02")
}

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
	var existingID, existingRequest, existingAmount, existingActor int64
	var existingKind, existingDate, existingReference, existingNote string
	err = tx.QueryRowContext(ctx, `SELECT id,request_id,amount,actor_id,kind,occurred_on,reference,note FROM recovery_events WHERE token=?`, in.Token).Scan(&existingID, &existingRequest, &existingAmount, &existingActor, &existingKind, &existingDate, &existingReference, &existingNote)
	if err == nil {
		if existingRequest != requestID {
			return 0, ErrForbidden
		}
		// A retry of the same entry is safe. A browser-back edit must not be
		// reported as saved while silently resolving the previous, different entry.
		if existingAmount != in.Amount || existingActor != actor.ID || existingKind != in.Kind || existingDate != in.OccurredOn || existingReference != in.Reference || existingNote != in.Note {
			return 0, fmt.Errorf("%w: this confirmation already recorded a different recovery; reload this page to start a new entry. The existing event is unchanged", ErrValidation)
		}
		return existingID, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	var paid, recovered int64
	var firstPaid string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0),COALESCE(MIN(paid_on),'') FROM payments WHERE request_id=? AND voided_at IS NULL`, requestID).Scan(&paid, &firstPaid); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM recovery_events WHERE request_id=?`, requestID).Scan(&recovered); err != nil {
		return 0, err
	}
	if paid <= 0 || in.Amount > paid-recovered {
		return 0, fmt.Errorf("%w: recovery cannot exceed the outstanding balance", ErrValidation)
	}
	if in.OccurredOn < firstPaid {
		return 0, fmt.Errorf("%w: recovery date cannot be before the first payment", ErrValidation)
	}
	// Date-specific guards also prevent a backdated event consuming an installment
	// that had not been paid yet.
	var paidByDate, recoveredByDate int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM payments WHERE request_id=? AND voided_at IS NULL AND paid_on<=?`, requestID, in.OccurredOn).Scan(&paidByDate); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM recovery_events WHERE request_id=? AND occurred_on<=?`, requestID, in.OccurredOn).Scan(&recoveredByDate); err != nil {
		return 0, err
	}
	if in.Amount > paidByDate-recoveredByDate {
		return 0, fmt.Errorf("%w: recovery exceeds the balance available on that date", ErrValidation)
	}
	// Check all later event dates too: inserting before a previous reconciliation
	// must not make that historical running balance negative.
	var invalid int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM recovery_events e WHERE e.request_id=? AND e.occurred_on>=? AND
 (SELECT COALESCE(SUM(p.amount),0) FROM payments p WHERE p.request_id=e.request_id AND p.voided_at IS NULL AND p.paid_on<=e.occurred_on) <
 (SELECT COALESCE(SUM(r.amount),0) FROM recovery_events r WHERE r.request_id=e.request_id AND r.occurred_on<=e.occurred_on)+?`, requestID, in.OccurredOn, in.Amount).Scan(&invalid); err != nil {
		return 0, err
	}
	if invalid > 0 {
		return 0, fmt.Errorf("%w: that backdated recovery would exceed a later recorded balance", ErrValidation)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO recovery_events(request_id,kind,amount,occurred_on,reference,note,actor_id,actor_name,token) VALUES(?,?,?,?,?,?,?,?,?)`, requestID, in.Kind, in.Amount, in.OccurredOn, in.Reference, in.Note, actor.ID, actor.Name, in.Token)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "recovery", EntityType: "payment_request", EntityID: &requestID, Summary: "Recorded " + (RecoveryEvent{Kind: in.Kind}).Label() + " · " + in.Reference, Before: map[string]any{"outstanding": paid - recovered}, After: map[string]any{"outstanding": paid - recovered - in.Amount, "amount": in.Amount, "kind": in.Kind, "occurred_on": in.OccurredOn, "reference": in.Reference, "note": in.Note}}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) RecoveryEvents(ctx context.Context, actor User, requestID int64) ([]RecoveryEvent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
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
