// budgets.go saves monthly budgets, creates and lists month plans, and locks
// and unlocks months. A locked month refuses every budget and payment change.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fervidbudget/internal/money"
	"fmt"
	"strings"
)

// SetBudget saves one head's budget for a month; see SetBudgets.
func (s *Store) SetBudget(ctx context.Context, actor User, headID int64, month string, amount int64) error {
	return s.SetBudgets(ctx, actor, month, []BudgetInput{{HeadID: headID, Amount: amount}})
}

// SetBudgets saves a whole budget form in one transaction: every row is checked
// before anything is written, so a bad row saves nothing. The month must not be
// locked, and a head whose budget has detailed lines is changed through the
// budget planner, not here. Each changed head gets an audit row; unchanged
// ones are skipped.
func (s *Store) SetBudgets(ctx context.Context, actor User, month string, inputs []BudgetInput) error {
	if !validMonth(month) || len(inputs) == 0 {
		return fmt.Errorf("%w: valid month and at least one budget are required", ErrValidation)
	}
	seen := make(map[int64]struct{}, len(inputs))
	for _, input := range inputs {
		if input.HeadID <= 0 || input.Amount < 0 || input.Amount > money.MaxAmount {
			return fmt.Errorf("%w: valid head and non-negative amount are required", ErrValidation)
		}
		if _, exists := seen[input.HeadID]; exists {
			return fmt.Errorf("%w: duplicate budget head", ErrValidation)
		}
		seen[input.HeadID] = struct{}{}
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if locked, err := isLocked(ctx, tx, month); err != nil {
		return err
	} else if locked {
		return ErrLockedMonth
	}
	var actorID any
	if actor.ID != 0 {
		actorID = actor.ID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO budget_months(month,status,created_by) VALUES(?,'open',?) ON CONFLICT(month) DO UPDATE SET updated_at=CURRENT_TIMESTAMP`, month, actorID); err != nil {
		return classify(err)
	}
	for _, input := range inputs {
		var before *Budget
		var b Budget
		err := tx.QueryRowContext(ctx, `SELECT id,head_id,month,amount,created_at,updated_at FROM budgets WHERE head_id=? AND month=?`, input.HeadID, month).Scan(&b.ID, &b.HeadID, &b.Month, &b.Amount, &b.CreatedAt, &b.UpdatedAt)
		if err == nil {
			before = &b
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// The head lookup comes before the skip below, so an unknown head still
		// fails the whole batch even when it arrives at ₹0 with no budget behind
		// it. The label names the head in the audit summary; detailed counts the
		// budget lines behind this month's budget.
		var label string
		var detailed int
		if err := tx.QueryRowContext(ctx, `SELECT p.name || ' / ' || h.name,
			(SELECT COUNT(*) FROM budget_lines l JOIN budgets b ON b.id=l.budget_id WHERE b.head_id=h.id AND b.month=?)
			FROM heads h JOIN projects p ON p.id=h.project_id WHERE h.id=?`, month, input.HeadID).Scan(&label, &detailed); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: unknown budget head", ErrValidation)
			}
			return err
		}
		// Saving the form re-submits every head on it. A budget whose amount did
		// not move is not a change and gets no audit row, or every save would
		// bury the real changes. The same holds for a head with no budget
		// submitted at ₹0: an absent budget already reads as ₹0 on every screen.
		if (before != nil && before.Amount == input.Amount) || (before == nil && input.Amount == 0) {
			continue
		}
		if detailed > 0 {
			return fmt.Errorf("%w: %s has detailed budget lines; use Edit budget lines to change its amount", ErrValidation, label)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO budgets(head_id,month,amount) VALUES(?,?,?) ON CONFLICT(head_id,month) DO UPDATE SET amount=excluded.amount, updated_at=CURRENT_TIMESTAMP`, input.HeadID, month, input.Amount)
		if err != nil {
			return classify(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		after := Budget{ID: id, HeadID: input.HeadID, Month: month, Amount: input.Amount}
		// The summary names what changed — head, month and both amounts — because
		// it is the only column the audit screen shows without expanding the row.
		action := "create"
		summary := label + " " + month + ": set to " + money.FormatPaise(input.Amount)
		if before != nil {
			action, after.ID = "update", before.ID
			summary = label + " " + month + ": " + money.FormatPaise(before.Amount) + " → " + money.FormatPaise(input.Amount)
		}
		if err := recordAuditTx(ctx, tx, auditBy(actor, action, "budget", &after.ID, summary).withChange(before, after)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Budget returns a head's budget for a month, or ErrNotFound.
func (s *Store) Budget(ctx context.Context, headID int64, month string) (Budget, error) {
	var b Budget
	err := s.db.QueryRowContext(ctx, `SELECT id,head_id,month,amount,created_at,updated_at FROM budgets WHERE head_id=? AND month=?`, headID, month).
		Scan(&b.ID, &b.HeadID, &b.Month, &b.Amount, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

// CreateMonthPlan opens targetMonth for planning. With a sourceMonth it also
// copies that month's non-zero budgets (and their budget lines) for heads that
// are still active; heads that already have a budget in targetMonth keep it.
// The month must not be locked.
func (s *Store) CreateMonthPlan(ctx context.Context, actor User, targetMonth, sourceMonth string) error {
	targetMonth = strings.TrimSpace(targetMonth)
	sourceMonth = strings.TrimSpace(sourceMonth)
	if !validMonth(targetMonth) {
		return fmt.Errorf("%w: target month is required", ErrValidation)
	}
	if sourceMonth != "" && !validMonth(sourceMonth) {
		return fmt.Errorf("%w: source month is invalid", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if locked, err := isLocked(ctx, tx, targetMonth); err != nil {
		return err
	} else if locked {
		return ErrLockedMonth
	}
	if sourceMonth != "" {
		var sourceBudgets int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM budgets WHERE month=? AND amount>0`, sourceMonth).Scan(&sourceBudgets); err != nil {
			return err
		}
		if sourceBudgets == 0 {
			return fmt.Errorf("%w: source month has no budgets to copy", ErrValidation)
		}
	}

	var source any
	if sourceMonth != "" {
		source = sourceMonth
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO budget_months(month,status,source_month,created_by) VALUES(?,'open',?,?)
		ON CONFLICT(month) DO UPDATE SET updated_at=CURRENT_TIMESTAMP, source_month=COALESCE(budget_months.source_month, excluded.source_month)`,
		targetMonth, source, actor.ID); err != nil {
		return classify(err)
	}

	var copied int64
	if sourceMonth != "" {
		rows, err := tx.QueryContext(ctx, `INSERT INTO budgets(head_id,month,amount)
		 SELECT h.id, ?, sb.amount FROM heads h JOIN projects p ON p.id=h.project_id
		 JOIN budgets sb ON sb.head_id=h.id AND sb.month=?
		 WHERE h.active=1 AND p.active=1 AND sb.amount>0
		 ON CONFLICT(head_id,month) DO NOTHING RETURNING id`, targetMonth, sourceMonth)
		if err != nil {
			return classify(err)
		}
		var created []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			created = append(created, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		copied = int64(len(created))
		for _, id := range created {
			if _, err := tx.ExecContext(ctx, `INSERT INTO budget_lines(budget_id,description,amount,sort_order)
		 SELECT target.id,l.description,l.amount,l.sort_order FROM budgets target
		 JOIN budgets source ON source.head_id=target.head_id AND source.month=?
		 JOIN budget_lines l ON l.budget_id=source.id WHERE target.id=?`, sourceMonth, id); err != nil {
				return err
			}
		}
	}

	summary := "Created monthly plan " + targetMonth
	if sourceMonth != "" {
		summary = fmt.Sprintf("%s from %s with %d copied budgets", summary, sourceMonth, copied)
	}
	after := map[string]any{"month": targetMonth, "source_month": sourceMonth, "copied_budgets": copied}
	if err := recordAuditTx(ctx, tx, auditBy(actor, "create", "budget_month", nil, summary).withChange(nil, after)); err != nil {
		return err
	}
	return tx.Commit()
}

// ListMonthPlans lists every month that has a plan, a budget, a payment or a
// lock, newest first, with its budget-versus-actual totals from Grid.
func (s *Store) ListMonthPlans(ctx context.Context) ([]MonthPlan, error) {
	rows, err := s.db.QueryContext(ctx, `WITH months(month) AS (
			SELECT month FROM budget_months
			UNION SELECT month FROM budgets
			UNION SELECT substr(paid_on,1,7) FROM payments
			UNION SELECT month FROM month_locks
		)
		SELECT m.month,COALESCE(bm.status,''),COALESCE(bm.source_month,''),COALESCE(u.name,''),bm.created_at,ml.locked_at,COALESCE(ml.reason,'')
		FROM months m
		LEFT JOIN budget_months bm ON bm.month=m.month
		LEFT JOIN users u ON u.id=bm.created_by
		LEFT JOIN month_locks ml ON ml.month=m.month
		ORDER BY m.month DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MonthPlan
	for rows.Next() {
		var plan MonthPlan
		var createdAt, lockedAt sql.NullTime
		if err := rows.Scan(&plan.Month, &plan.Status, &plan.SourceMonth, &plan.CreatedByName, &createdAt, &lockedAt, &plan.LockReason); err != nil {
			return nil, err
		}
		if plan.Status == "" {
			plan.Status = "open"
		}
		if createdAt.Valid {
			t := createdAt.Time
			plan.CreatedAt = &t
		}
		if lockedAt.Valid {
			t := lockedAt.Time
			plan.Locked = true
			plan.LockedAt = &t
			plan.Status = "locked"
		}
		grid, err := s.Grid(ctx, plan.Month, "", "")
		if err != nil {
			return nil, err
		}
		plan.Budget = grid.Total.Budget
		plan.Actual = grid.Total.Actual
		plan.Variance = grid.Total.Variance
		plan.HeadCount = len(grid.Rows)
		if plan.Budget <= 0 && plan.Actual > 0 {
			plan.UsedPercent = "No budget"
		} else if plan.Budget <= 0 || plan.Actual <= 0 {
			plan.UsedPercent = "0%"
		} else {
			plan.UsedPercent = fmt.Sprintf("%.0f%%", (float64(plan.Actual)/float64(plan.Budget))*100)
		}
		out = append(out, plan)
	}
	return out, rows.Err()
}

// LockMonth locks a month so no budget or payment in it can change. A reason
// is required.
func (s *Store) LockMonth(ctx context.Context, actor User, month, reason string) error {
	if !validMonth(month) || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: month and reason are required", ErrValidation)
	}
	// One transaction: the lock row, the month status and the audit entry are
	// written together or not at all.
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ensureMonthPlan(ctx, tx, actor, month, ""); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO month_locks(month,locked_by,reason) VALUES(?,?,?)
		ON CONFLICT(month) DO UPDATE SET locked_by=excluded.locked_by, locked_at=CURRENT_TIMESTAMP, reason=excluded.reason`, month, actor.ID, reason); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE budget_months SET status='locked', updated_at=CURRENT_TIMESTAMP WHERE month=?`, month); err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, auditBy(actor, "lock", "month_lock", nil, "Locked "+month+": "+reason).withChange(nil, map[string]string{"month": month, "reason": reason})); err != nil {
		return err
	}
	return tx.Commit()
}

// UnlockMonth reopens a locked month. A reason is required.
func (s *Store) UnlockMonth(ctx context.Context, actor User, month, reason string) error {
	if !validMonth(month) || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: month and reason are required", ErrValidation)
	}
	before := map[string]string{"month": month}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM month_locks WHERE month=?`, month); err != nil {
		return err
	}
	if err := ensureMonthPlan(ctx, tx, actor, month, ""); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE budget_months SET status='open', updated_at=CURRENT_TIMESTAMP WHERE month=?`, month); err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, auditBy(actor, "unlock", "month_lock", nil, "Unlocked "+month+": "+reason).withChange(before, map[string]string{"reason": reason})); err != nil {
		return err
	}
	return tx.Commit()
}

// isLocked reports whether month is locked, or why it could not tell. Every
// write path uses this rather than IsLocked: a lock that fails to read is not
// an unlocked month.
func isLocked(ctx context.Context, q rowQuerier, month string) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM month_locks WHERE month=?`, month).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// IsLocked is the display-side answer, where there is nowhere to report an error
// to and a wrong answer only mis-renders a badge. Anything gating a write uses
// isLocked inside the package, or MonthIsLocked from outside it.
func (s *Store) IsLocked(ctx context.Context, month string) bool {
	locked, err := isLocked(ctx, s.db, month)
	if err != nil {
		return false
	}
	return locked
}

// MonthIsLocked is IsLocked for callers that gate a write on the answer and can
// therefore not treat "I could not find out" as "open".
func (s *Store) MonthIsLocked(ctx context.Context, month string) (bool, error) {
	return isLocked(ctx, s.db, month)
}

// MonthLock returns who locked month and why, or ErrNotFound when it is open.
func (s *Store) MonthLock(ctx context.Context, month string) (MonthLock, error) {
	var lock MonthLock
	err := s.db.QueryRowContext(ctx, `SELECT ml.month,ml.locked_by,COALESCE(u.name,''),ml.locked_at,COALESCE(ml.reason,'')
		FROM month_locks ml LEFT JOIN users u ON u.id=ml.locked_by WHERE ml.month=?`, month).
		Scan(&lock.Month, &lock.LockedBy, &lock.ActorName, &lock.LockedAt, &lock.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return lock, ErrNotFound
	}
	return lock, err
}

// ensureMonthPlan makes sure the month has a budget_months row, inside the
// caller's write transaction.
func ensureMonthPlan(ctx context.Context, tx *sql.Tx, actor User, month, sourceMonth string) error {
	if !validMonth(month) {
		return fmt.Errorf("%w: valid month is required", ErrValidation)
	}
	var source any
	if validMonth(sourceMonth) {
		source = sourceMonth
	}
	var actorID any
	if actor.ID != 0 {
		actorID = actor.ID
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO budget_months(month,status,source_month,created_by) VALUES(?,'open',?,?)
		ON CONFLICT(month) DO UPDATE SET updated_at=CURRENT_TIMESTAMP`, month, source, actorID)
	return classify(err)
}
