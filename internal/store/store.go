package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fervidbudget/internal/money"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	// The pragmas ride on the DSN, not on a db.Exec: PRAGMA state is
	// per-connection, and database/sql opens more connections on demand, so a
	// single `db.Exec("PRAGMA …")` configures only whichever connection happened
	// to serve it. Every later connection would then run with foreign keys off
	// and no busy handler — the second is what makes concurrent reservations
	// (ReserveRequest) fail with SQLITE_BUSY instead of waiting their turn.
	// modernc.org/sqlite applies every _pragma query parameter to each new
	// connection; without a "file:" prefix it strips the query before opening,
	// so a path containing spaces still opens correctly.
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

func (s *Store) CreateUser(ctx context.Context, email, name, hash, role string, active bool) (int64, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if err := validateUserFields(email, name, role); err != nil {
		return 0, err
	}
	if role == "" {
		role = "data_entry"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO users(email,name,password_hash,role,active) VALUES(?,?,?,?,?)`,
		email, name, hash, role, boolInt(active))
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := assignDefaultRoleTx(ctx, tx, id, role); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) EnsureUser(ctx context.Context, email, name, hash, role string) error {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE email=?`, strings.ToLower(email)).Scan(&id)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = s.CreateUser(ctx, email, name, hash, role, true)
	return err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,email,name,password_hash,role,active,created_at,updated_at,default_approver_id FROM users WHERE email=?`, strings.ToLower(strings.TrimSpace(email)))
	return scanUser(row)
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,email,name,password_hash,role,active,created_at,updated_at,default_approver_id FROM users WHERE id=?`, id)
	return scanUser(row)
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,email,name,password_hash,role,active,created_at,updated_at,default_approver_id FROM users ORDER BY name`)
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

func (s *Store) UpdateUser(ctx context.Context, id int64, name, role string, active bool, passwordHash string) error {
	name = strings.TrimSpace(name)
	if err := validateUserFields("placeholder@example.invalid", name, role); err != nil {
		return err
	}
	if err := s.RequireAnotherActiveAdmin(ctx, id, role, active); err != nil {
		return err
	}
	if passwordHash != "" {
		_, err := s.db.ExecContext(ctx, `UPDATE users SET name=?, role=?, active=?, password_hash=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
			name, role, boolInt(active), passwordHash, id)
		return classify(err)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET name=?, role=?, active=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		name, role, boolInt(active), id)
	return classify(err)
}

// RequireAnotherActiveAdmin prevents disabling or demoting the final active
// administrator. It is deliberately public so callers can surface a useful
// validation message before displaying a confirmation form.
func (s *Store) RequireAnotherActiveAdmin(ctx context.Context, userID int64, nextRole string, nextActive bool) error {
	var currentRole string
	var currentActive int
	if err := s.db.QueryRowContext(ctx, `SELECT role,active FROM users WHERE id=?`, userID).Scan(&currentRole, &currentActive); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if currentRole != "admin" || currentActive != 1 || (nextRole == "admin" && nextActive) {
		return nil
	}
	var others int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='admin' AND active=1 AND id<>?`, userID).Scan(&others); err != nil {
		return err
	}
	if others == 0 {
		return fmt.Errorf("%w: at least one active administrator is required", ErrValidation)
	}
	return nil
}

// LoginLocked reports whether a known account remains temporarily locked.
func (s *Store) LoginLocked(ctx context.Context, email string) (bool, time.Time, error) {
	var locked sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT locked FROM users WHERE email=?`, strings.ToLower(strings.TrimSpace(email))).Scan(&locked)
	if err == sql.ErrNoRows {
		return false, time.Time{}, ErrNotFound
	}
	if err != nil {
		return false, time.Time{}, err
	}
	if !locked.Valid || !locked.Time.After(time.Now()) {
		return false, time.Time{}, nil
	}
	return true, locked.Time, nil
}

// RecordFailedLogin increments a known user's failure counter and locks the
// account for fifteen minutes after five consecutive failures. It intentionally
// returns no signal for unknown email addresses to avoid account enumeration.
func (s *Store) RecordFailedLogin(ctx context.Context, email string) (time.Time, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback()
	var attempts int
	var previousLock sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT attempt_count,locked FROM users WHERE email=?`, email).Scan(&attempts, &previousLock); err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	if previousLock.Valid && !previousLock.Time.After(time.Now()) {
		attempts = 0
	}
	attempts++
	var locked any
	var until time.Time
	if attempts >= 5 {
		until = time.Now().Add(15 * time.Minute).UTC()
		locked = until
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET attempt_count=?, last_attempt=CURRENT_TIMESTAMP, locked=?, updated_at=CURRENT_TIMESTAMP WHERE email=?`, attempts, locked, email); err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return time.Time{}, err
	}
	return until, nil
}

func (s *Store) ResetLoginAttempts(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET attempt_count=0,last_attempt=NULL,locked=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=?`, userID)
	return classify(err)
}

// ResetLoginFailures is the email-oriented companion for login handlers that
// do not retain a user ID after authentication has succeeded.
func (s *Store) ResetLoginFailures(ctx context.Context, email string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET attempt_count=0,last_attempt=NULL,locked=NULL,updated_at=CURRENT_TIMESTAMP WHERE email=?`, strings.ToLower(strings.TrimSpace(email)))
	return classify(err)
}

func (s *Store) UpsertProject(ctx context.Context, id int64, name string, active bool, sortOrder int) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("%w: project name is required", ErrValidation)
	}
	if id == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO projects(name,active,sort_order) VALUES(?,?,?)`, name, boolInt(active), sortOrder)
		if err != nil {
			return 0, classify(err)
		}
		return res.LastInsertId()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE projects SET name=?, active=?, sort_order=? WHERE id=?`, name, boolInt(active), sortOrder, id)
	return id, classify(err)
}

func (s *Store) ListProjects(ctx context.Context, activeOnly bool) ([]Project, error) {
	q := `SELECT id,name,active,sort_order,created_at FROM projects`
	if activeOnly {
		q += ` WHERE active=1`
	}
	q += ` ORDER BY sort_order,name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		var active int
		if err := rows.Scan(&p.ID, &p.Name, &active, &p.SortOrder, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Active = active == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) UpsertHead(ctx context.Context, id, projectID int64, name, dueDay string, active bool, sortOrder int) (int64, error) {
	name = strings.TrimSpace(name)
	dueDay = strings.TrimSpace(dueDay)
	if name == "" || projectID == 0 {
		return 0, fmt.Errorf("%w: project and head name are required", ErrValidation)
	}
	if !validDueDay(dueDay) {
		return 0, fmt.Errorf("%w: due day must be a day from 1 to 31", ErrValidation)
	}
	if id == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO heads(project_id,name,due_day,active,sort_order) VALUES(?,?,?,?,?)`,
			projectID, name, dueDay, boolInt(active), sortOrder)
		if err != nil {
			return 0, classify(err)
		}
		return res.LastInsertId()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE heads SET project_id=?, name=?, due_day=?, active=?, sort_order=? WHERE id=?`,
		projectID, name, dueDay, boolInt(active), sortOrder, id)
	return id, classify(err)
}

func (s *Store) ListHeads(ctx context.Context, activeOnly bool) ([]Head, error) {
	q := `SELECT h.id,h.project_id,p.name,h.name,COALESCE(h.due_day,''),h.active,h.sort_order,h.created_at
	      FROM heads h JOIN projects p ON p.id=h.project_id`
	if activeOnly {
		q += ` WHERE h.active=1 AND p.active=1`
	}
	q += ` ORDER BY p.sort_order,p.name,h.sort_order,h.name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Head
	for rows.Next() {
		var h Head
		var active int
		if err := rows.Scan(&h.ID, &h.ProjectID, &h.Project, &h.Name, &h.DueDay, &active, &h.SortOrder, &h.CreatedAt); err != nil {
			return nil, err
		}
		h.Active = active == 1
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) SetBudget(ctx context.Context, actor User, headID int64, month string, amount int64) error {
	return s.SetBudgets(ctx, actor, month, []BudgetInput{{HeadID: headID, Amount: amount}})
}

// SetBudgets writes an entire budget form atomically. Validation occurs before
// any write, so a malformed row cannot leave a partially saved monthly plan.
func (s *Store) SetBudgets(ctx context.Context, actor User, month string, inputs []BudgetInput) error {
	if !validMonth(month) || len(inputs) == 0 {
		return fmt.Errorf("%w: valid month and at least one budget are required", ErrValidation)
	}
	seen := make(map[int64]struct{}, len(inputs))
	for _, input := range inputs {
		if input.HeadID <= 0 || input.Amount < 0 {
			return fmt.Errorf("%w: valid head and non-negative amount are required", ErrValidation)
		}
		if _, exists := seen[input.HeadID]; exists {
			return fmt.Errorf("%w: duplicate budget head", ErrValidation)
		}
		seen[input.HeadID] = struct{}{}
	}
	if s.IsLocked(ctx, month) {
		return ErrLockedMonth
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
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
		} else if err != sql.ErrNoRows {
			return err
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
		action := "create"
		if before != nil {
			action, after.ID = "update", before.ID
		}
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(after)
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log(actor_id,actor_name,action,entity_type,entity_id,summary,before_json,after_json) VALUES(?,?,?,?,?,?,?,?)`, &actor.ID, actor.Name, action, "budget", after.ID, "Saved budget "+money.FormatPaise(input.Amount), nullJSON(beforeJSON), nullJSON(afterJSON)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) setBudgetLegacy(ctx context.Context, actor User, headID int64, month string, amount int64) error {
	// Kept as a small, explicit implementation reference for older callers.
	var before *Budget
	if b, err := s.Budget(ctx, headID, month); err == nil {
		before = &b
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO budgets(head_id,month,amount) VALUES(?,?,?)
		ON CONFLICT(head_id,month) DO UPDATE SET amount=excluded.amount, updated_at=CURRENT_TIMESTAMP`, headID, month, amount)
	if err != nil {
		return classify(err)
	}
	after, _ := s.Budget(ctx, headID, month)
	action := "create"
	if before != nil {
		action = "update"
	}
	return s.RecordAudit(ctx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: action, EntityType: "budget", EntityID: &after.ID, Summary: "Saved budget " + money.FormatPaise(amount), Before: before, After: after})
}

func (s *Store) Budget(ctx context.Context, headID int64, month string) (Budget, error) {
	var b Budget
	err := s.db.QueryRowContext(ctx, `SELECT id,head_id,month,amount,created_at,updated_at FROM budgets WHERE head_id=? AND month=?`, headID, month).
		Scan(&b.ID, &b.HeadID, &b.Month, &b.Amount, &b.CreatedAt, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return b, ErrNotFound
	}
	return b, err
}

func (s *Store) CreateMonthPlan(ctx context.Context, actor User, targetMonth, sourceMonth string) error {
	targetMonth = strings.TrimSpace(targetMonth)
	sourceMonth = strings.TrimSpace(sourceMonth)
	if !validMonth(targetMonth) {
		return fmt.Errorf("%w: target month is required", ErrValidation)
	}
	if sourceMonth != "" && !validMonth(sourceMonth) {
		return fmt.Errorf("%w: source month is invalid", ErrValidation)
	}
	if s.IsLocked(ctx, targetMonth) {
		return ErrLockedMonth
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
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
		res, err := tx.ExecContext(ctx, `INSERT INTO budgets(head_id,month,amount)
			SELECT h.id, ?, sb.amount
			FROM heads h
			JOIN projects p ON p.id=h.project_id
			JOIN budgets sb ON sb.head_id=h.id AND sb.month=?
			WHERE h.active=1 AND p.active=1 AND sb.amount > 0
			ON CONFLICT(head_id,month) DO NOTHING`, targetMonth, sourceMonth)
		if err != nil {
			return classify(err)
		}
		copied, _ = res.RowsAffected()
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	summary := "Created monthly plan " + targetMonth
	if sourceMonth != "" {
		summary = fmt.Sprintf("%s from %s with %d copied budgets", summary, sourceMonth, copied)
	}
	return s.RecordAudit(ctx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "create", EntityType: "budget_month", Summary: summary, After: map[string]any{"month": targetMonth, "source_month": sourceMonth, "copied_budgets": copied}})
}

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
		if plan.Budget <= 0 || plan.Actual <= 0 {
			plan.UsedPercent = "0%"
		} else {
			plan.UsedPercent = fmt.Sprintf("%.0f%%", (float64(plan.Actual)/float64(plan.Budget))*100)
		}
		out = append(out, plan)
	}
	return out, rows.Err()
}

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
	if err := s.validatePayment(ctx, in); err != nil {
		return 0, err
	}
	if attachment != nil {
		if err := validateAttachment(*attachment); err != nil {
			return 0, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
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
	if err := s.validatePayment(ctx, in); err != nil {
		return err
	}
	if attachment != nil {
		if err := validateAttachment(*attachment); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := paymentInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.VoidedAt != nil {
		return fmt.Errorf("%w: cannot edit voided payment", ErrValidation)
	}
	if s.IsLocked(ctx, before.PaidOn[:7]) {
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := paymentInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.VoidedAt != nil {
		return nil
	}
	if reason == "" {
		return fmt.Errorf("%w: void reason is required", ErrValidation)
	}
	if s.IsLocked(ctx, before.PaidOn[:7]) {
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

func (s *Store) Payment(ctx context.Context, id int64) (Payment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT py.id,py.head_id,h.project_id,p.name,h.name,py.paid_on,py.amount,
		COALESCE(py.vendor_payee,''),COALESCE(py.payment_mode,''),COALESCE(py.invoice_no,''),COALESCE(py.reference_no,''),COALESCE(py.remarks,''),
		py.entered_by,u.name,py.updated_by,py.voided_by,COALESCE(py.void_reason,''),py.voided_at,py.created_at,py.updated_at,
		py.request_id,COALESCE(py.settlement,''),COALESCE(py.partial_reason,'')
		FROM payments py JOIN heads h ON h.id=py.head_id JOIN projects p ON p.id=h.project_id JOIN users u ON u.id=py.entered_by WHERE py.id=?`, id)
	var p Payment
	err := row.Scan(&p.ID, &p.HeadID, &p.ProjectID, &p.Project, &p.Head, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.EnteredByName, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt, &p.RequestID, &p.Settlement, &p.PartialReason)
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	return p, err
}

// PaymentForRequest returns the single payment linked to a request, or
// ErrNotFound when none exists yet. It powers the requester's outcome view (Q4).
func (s *Store) PaymentForRequest(ctx context.Context, requestID int64) (Payment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT py.id,py.head_id,h.project_id,p.name,h.name,py.paid_on,py.amount,
		COALESCE(py.vendor_payee,''),COALESCE(py.payment_mode,''),COALESCE(py.invoice_no,''),COALESCE(py.reference_no,''),COALESCE(py.remarks,''),
		py.entered_by,u.name,py.updated_by,py.voided_by,COALESCE(py.void_reason,''),py.voided_at,py.created_at,py.updated_at,
		py.request_id,COALESCE(py.settlement,''),COALESCE(py.partial_reason,'')
		FROM payments py JOIN heads h ON h.id=py.head_id JOIN projects p ON p.id=h.project_id JOIN users u ON u.id=py.entered_by WHERE py.request_id=?`, requestID)
	var p Payment
	err := row.Scan(&p.ID, &p.HeadID, &p.ProjectID, &p.Project, &p.Head, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.EnteredByName, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt, &p.RequestID, &p.Settlement, &p.PartialReason)
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	return p, err
}

func paymentInTx(ctx context.Context, tx *sql.Tx, id int64) (Payment, error) {
	var p Payment
	err := tx.QueryRowContext(ctx, `SELECT id,head_id,paid_on,amount,COALESCE(vendor_payee,''),COALESCE(payment_mode,''),COALESCE(invoice_no,''),COALESCE(reference_no,''),COALESCE(remarks,''),entered_by,updated_by,voided_by,COALESCE(void_reason,''),voided_at,created_at,updated_at,request_id,COALESCE(settlement,''),COALESCE(partial_reason,'') FROM payments WHERE id=?`, id).
		Scan(&p.ID, &p.HeadID, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt, &p.RequestID, &p.Settlement, &p.PartialReason)
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	return p, err
}

func paymentFromInput(id int64, actor User, in PaymentInput) Payment {
	return Payment{ID: id, HeadID: in.HeadID, PaidOn: in.PaidOn, Amount: in.Amount, VendorPayee: in.VendorPayee, PaymentMode: in.PaymentMode, InvoiceNo: in.InvoiceNo, ReferenceNo: in.ReferenceNo, Remarks: in.Remarks, EnteredBy: actor.ID, EnteredByName: actor.Name}
}

func recordAuditTx(ctx context.Context, tx *sql.Tx, in AuditInput) error {
	before, _ := json.Marshal(in.Before)
	after, _ := json.Marshal(in.After)
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_log(actor_id,actor_name,action,entity_type,entity_id,summary,before_json,after_json,ip) VALUES(?,?,?,?,?,?,?,?,?)`, in.ActorID, in.ActorName, in.Action, in.EntityType, in.EntityID, in.Summary, nullJSON(before), nullJSON(after), in.IP)
	return err
}

// ReserveRequest atomically moves an approved, unclaimed, not-on-hold request to
// 'processing' reserved by actor. The single conditional UPDATE is the
// concurrency guarantee: only the first committer matches, so a losing caller
// sees RowsAffected()==0 and is told the request is unavailable (S2, S5, L8).
func (s *Store) ReserveRequest(ctx context.Context, actor User, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
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
		return fmt.Errorf("%w: request is not available to process", ErrForbidden)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "process", EntityType: "payment_request", EntityID: &id, Summary: "Reserved request for processing", After: map[string]any{"processing_by": actor.ID}}); err != nil {
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
	tx, err := s.db.BeginTx(ctx, nil)
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
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "release", EntityType: "payment_request", EntityID: &id, Summary: "Released reservation: " + reason, Before: map[string]any{"processing_by": processingBy.Int64}}); err != nil {
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
	tx, err := s.db.BeginTx(ctx, nil)
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
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "reassign", EntityType: "payment_request", EntityID: &id,
		Summary: "Reassigned reservation to " + toName + ": " + reason,
		Before:  map[string]any{"processing_by": processingBy.Int64},
		After:   map[string]any{"processing_by": toUserID}}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecentPayments(ctx context.Context, limit int) ([]Payment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT py.id,py.head_id,h.project_id,p.name,h.name,py.paid_on,py.amount,
		COALESCE(py.vendor_payee,''),COALESCE(py.payment_mode,''),COALESCE(py.invoice_no,''),COALESCE(py.reference_no,''),COALESCE(py.remarks,''),
		py.entered_by,u.name,py.updated_by,py.voided_by,COALESCE(py.void_reason,''),py.voided_at,py.created_at,py.updated_at
		FROM payments py JOIN heads h ON h.id=py.head_id JOIN projects p ON p.id=h.project_id JOIN users u ON u.id=py.entered_by
		WHERE py.voided_at IS NULL
		ORDER BY py.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.ID, &p.HeadID, &p.ProjectID, &p.Project, &p.Head, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.EnteredByName, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ListPayments(ctx context.Context, opts PaymentListOptions) ([]Payment, error) {
	if opts.Limit <= 0 {
		opts.Limit = 300
	}
	q := `SELECT py.id,py.head_id,h.project_id,p.name,h.name,py.paid_on,py.amount,
		COALESCE(py.vendor_payee,''),COALESCE(py.payment_mode,''),COALESCE(py.invoice_no,''),COALESCE(py.reference_no,''),COALESCE(py.remarks,''),
		py.entered_by,u.name,py.updated_by,py.voided_by,COALESCE(py.void_reason,''),py.voided_at,py.created_at,py.updated_at
		FROM payments py JOIN heads h ON h.id=py.head_id JOIN projects p ON p.id=h.project_id JOIN users u ON u.id=py.entered_by`
	var where []string
	var args []any
	if validMonth(opts.Month) {
		where = append(where, `substr(py.paid_on,1,7)=?`)
		args = append(args, opts.Month)
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
	q += ` ORDER BY py.paid_on DESC, py.created_at DESC LIMIT ?`
	args = append(args, opts.Limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.ID, &p.HeadID, &p.ProjectID, &p.Project, &p.Head, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.EnteredByName, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := paymentInTx(ctx, tx, paymentID)
	if err != nil {
		return err
	}
	if p.VoidedAt != nil {
		return fmt.Errorf("%w: cannot attach files to a voided payment", ErrValidation)
	}
	if s.IsLocked(ctx, p.PaidOn[:7]) {
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

func (s *Store) Grid(ctx context.Context, month, status, q string) (GridData, error) {
	if !validMonth(month) {
		month = time.Now().Format("2006-01")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.name,h.id,h.name,h.active,p.active,COALESCE(h.due_day,''),COALESCE(b.amount,0),
		COALESCE(SUM(CASE WHEN py.voided_at IS NULL THEN py.amount ELSE 0 END),0)
		FROM heads h JOIN projects p ON p.id=h.project_id
		LEFT JOIN budgets b ON b.head_id=h.id AND b.month=?
		LEFT JOIN payments py ON py.head_id=h.id AND substr(py.paid_on,1,7)=?
		WHERE (h.active=1 AND p.active=1)
			OR b.id IS NOT NULL
			OR EXISTS(SELECT 1 FROM payments px WHERE px.head_id=h.id AND substr(px.paid_on,1,7)=?)
		GROUP BY p.id,p.name,h.id,h.name,h.active,p.active,h.due_day,b.amount
		ORDER BY p.sort_order,p.name,h.sort_order,h.name`, month, month, month)
	if err != nil {
		return GridData{}, err
	}
	defer rows.Close()
	g := GridData{Month: month}
	if lock, err := s.MonthLock(ctx, month); err == nil {
		g.Locked = true
		g.Lock = lock
	}
	projectMap := map[int64]*ProjectTotal{}
	groupMap := map[int64]*GridGroup{}
	q = strings.ToLower(strings.TrimSpace(q))
	for rows.Next() {
		var r GridRow
		var headActive, projectActive int
		if err := rows.Scan(&r.ProjectID, &r.Project, &r.HeadID, &r.Head, &headActive, &projectActive, &r.DueDay, &r.Budget, &r.Actual); err != nil {
			return g, err
		}
		r.Active = headActive == 1 && projectActive == 1
		r.Variance = r.Budget - r.Actual
		r.VariancePercent = money.Percent(r.Variance, r.Budget)
		r.Status = statusFor(r.Budget, r.Actual)
		if status != "" && status != "all" && r.Status != status {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.Project+" "+r.Head), q) {
			continue
		}
		g.Rows = append(g.Rows, r)
		switch r.Status {
		case "over":
			g.Over++
		case "unbudgeted":
			g.Unbudgeted++
		case "under":
			g.Under++
		case "on-track":
			g.OnTrack++
		case "not-paid":
			g.NotPaid++
		}
		pt := projectMap[r.ProjectID]
		if pt == nil {
			pt = &ProjectTotal{ProjectID: r.ProjectID, Project: r.Project}
			projectMap[r.ProjectID] = pt
			g.Projects = append(g.Projects, *pt)
		}
		grp := groupMap[r.ProjectID]
		if grp == nil {
			grp = &GridGroup{ProjectID: r.ProjectID, Project: r.Project, Total: ProjectTotal{ProjectID: r.ProjectID, Project: r.Project}}
			groupMap[r.ProjectID] = grp
			g.Groups = append(g.Groups, *grp)
		}
		grp.Rows = append(grp.Rows, r)
		pt.Budget += r.Budget
		pt.Actual += r.Actual
		grp.Total.Budget += r.Budget
		grp.Total.Actual += r.Actual
		g.Total.Budget += r.Budget
		g.Total.Actual += r.Actual
	}
	for i := range g.Projects {
		pt := projectMap[g.Projects[i].ProjectID]
		pt.Variance = pt.Budget - pt.Actual
		pt.VariancePercent = money.Percent(pt.Variance, pt.Budget)
		g.Projects[i] = *pt
	}
	for i := range g.Groups {
		grp := groupMap[g.Groups[i].ProjectID]
		grp.Total.Variance = grp.Total.Budget - grp.Total.Actual
		grp.Total.VariancePercent = money.Percent(grp.Total.Variance, grp.Total.Budget)
		g.Groups[i] = *grp
	}
	g.Total.Project = "Company Total"
	g.Total.Variance = g.Total.Budget - g.Total.Actual
	g.Total.VariancePercent = money.Percent(g.Total.Variance, g.Total.Budget)
	return g, rows.Err()
}

func (s *Store) Report(ctx context.Context, fromMonth, toMonth, mode string) ([]ReportRow, error) {
	if !validMonth(fromMonth) {
		fromMonth = time.Now().Format("2006-01")
	}
	if !validMonth(toMonth) {
		toMonth = fromMonth
	}
	months, err := monthRange(fromMonth, toMonth)
	if err != nil {
		return nil, err
	}
	var out []ReportRow
	for _, month := range months {
		grid, err := s.Grid(ctx, month, "", "")
		if err != nil {
			return nil, err
		}
		switch mode {
		case "projects":
			for _, pt := range grid.Projects {
				out = append(out, ReportRow{Period: month, Project: pt.Project, Budget: pt.Budget, Actual: pt.Actual, Variance: pt.Variance, VariancePercent: pt.VariancePercent})
			}
		case "monthly":
			out = append(out, ReportRow{Period: month, Budget: grid.Total.Budget, Actual: grid.Total.Actual, Variance: grid.Total.Variance, VariancePercent: grid.Total.VariancePercent})
		default:
			for _, row := range grid.Rows {
				out = append(out, ReportRow{Period: month, Project: row.Project, Head: row.Head, Budget: row.Budget, Actual: row.Actual, Variance: row.Variance, VariancePercent: row.VariancePercent})
			}
		}
	}
	return out, nil
}

func (s *Store) LockMonth(ctx context.Context, actor User, month, reason string) error {
	if !validMonth(month) || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: month and reason are required", ErrValidation)
	}
	if err := s.ensureMonthPlan(ctx, actor, month, ""); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO month_locks(month,locked_by,reason) VALUES(?,?,?)
		ON CONFLICT(month) DO UPDATE SET locked_by=excluded.locked_by, locked_at=CURRENT_TIMESTAMP, reason=excluded.reason`, month, actor.ID, reason)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE budget_months SET status='locked', updated_at=CURRENT_TIMESTAMP WHERE month=?`, month); err != nil {
		return err
	}
	return s.RecordAudit(ctx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "lock", EntityType: "month_lock", Summary: "Locked " + month + ": " + reason, After: map[string]string{"month": month, "reason": reason}})
}

func (s *Store) UnlockMonth(ctx context.Context, actor User, month, reason string) error {
	if !validMonth(month) || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: month and reason are required", ErrValidation)
	}
	before := map[string]string{"month": month}
	_, err := s.db.ExecContext(ctx, `DELETE FROM month_locks WHERE month=?`, month)
	if err != nil {
		return err
	}
	if err := s.ensureMonthPlan(ctx, actor, month, ""); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE budget_months SET status='open', updated_at=CURRENT_TIMESTAMP WHERE month=?`, month); err != nil {
		return err
	}
	return s.RecordAudit(ctx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "unlock", EntityType: "month_lock", Summary: "Unlocked " + month + ": " + reason, Before: before, After: map[string]string{"reason": reason}})
}

func (s *Store) IsLocked(ctx context.Context, month string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM month_locks WHERE month=?`, month).Scan(&n)
	return n > 0
}

func (s *Store) MonthLock(ctx context.Context, month string) (MonthLock, error) {
	var lock MonthLock
	err := s.db.QueryRowContext(ctx, `SELECT ml.month,ml.locked_by,COALESCE(u.name,''),ml.locked_at,COALESCE(ml.reason,'')
		FROM month_locks ml LEFT JOIN users u ON u.id=ml.locked_by WHERE ml.month=?`, month).
		Scan(&lock.Month, &lock.LockedBy, &lock.ActorName, &lock.LockedAt, &lock.Reason)
	if err == sql.ErrNoRows {
		return lock, ErrNotFound
	}
	return lock, err
}

func (s *Store) ensureMonthPlan(ctx context.Context, actor User, month, sourceMonth string) error {
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO budget_months(month,status,source_month,created_by) VALUES(?,'open',?,?)
		ON CONFLICT(month) DO UPDATE SET updated_at=CURRENT_TIMESTAMP`, month, source, actorID)
	return classify(err)
}

func (s *Store) RecordAudit(ctx context.Context, in AuditInput) error {
	before, _ := json.Marshal(in.Before)
	after, _ := json.Marshal(in.After)
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log(actor_id,actor_name,action,entity_type,entity_id,summary,before_json,after_json,ip) VALUES(?,?,?,?,?,?,?,?,?)`,
		in.ActorID, in.ActorName, in.Action, in.EntityType, in.EntityID, in.Summary, nullJSON(before), nullJSON(after), in.IP)
	return err
}

func (s *Store) Audit(ctx context.Context, entityType string, entityID int64, limit int) ([]AuditEntry, error) {
	q := `SELECT id,actor_id,COALESCE(actor_name,''),action,COALESCE(entity_type,''),entity_id,COALESCE(summary,''),COALESCE(before_json,''),COALESCE(after_json,''),COALESCE(ip,''),created_at FROM audit_log`
	var args []any
	if entityType != "" {
		q += ` WHERE entity_type=?`
		args = append(args, entityType)
		if entityID > 0 {
			q += ` AND entity_id=?`
			args = append(args, entityID)
		}
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.ID, &a.ActorID, &a.ActorName, &a.Action, &a.EntityType, &a.EntityID, &a.Summary, &a.BeforeJSON, &a.AfterJSON, &a.IP, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) validatePayment(ctx context.Context, in PaymentInput) error {
	if in.Amount <= 0 || !validDate(in.PaidOn) || in.HeadID == 0 {
		return fmt.Errorf("%w: valid head, date, and positive amount are required", ErrValidation)
	}
	if s.IsLocked(ctx, in.PaidOn[:7]) {
		return ErrLockedMonth
	}
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM heads h JOIN projects p ON p.id=h.project_id WHERE h.id=? AND h.active=1 AND p.active=1`, in.HeadID).Scan(&active)
	if err != nil {
		return err
	}
	if active == 0 {
		return ErrInactiveHead
	}
	return nil
}

func scanUser(scanner interface{ Scan(...any) error }) (User, error) {
	var u User
	var active int
	var approver sql.NullInt64
	err := scanner.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &active, &u.CreatedAt, &u.UpdatedAt, &approver)
	if err == sql.ErrNoRows {
		return u, ErrNotFound
	}
	u.Active = active == 1
	u.DefaultApproverID = approver.Int64
	return u, err
}

func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func validDueDay(s string) bool {
	if s == "" {
		return true
	}
	day, err := strconv.Atoi(s)
	return err == nil && day >= 1 && day <= 31
}

func validateUserFields(email, name, role string) error {
	if strings.TrimSpace(email) == "" || !strings.Contains(email, "@") || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: email and name are required", ErrValidation)
	}
	if role != "" && role != "admin" && role != "data_entry" {
		return fmt.Errorf("%w: invalid user role", ErrValidation)
	}
	return nil
}

func validateAttachment(in AttachmentInput) error {
	if strings.TrimSpace(in.OriginalName) == "" || strings.TrimSpace(in.StoredPath) == "" || in.SizeBytes < 0 {
		return fmt.Errorf("%w: valid attachment metadata is required", ErrValidation)
	}
	return nil
}

func validMonth(s string) bool {
	_, err := time.Parse("2006-01", s)
	return err == nil
}

func monthRange(from, to string) ([]string, error) {
	start, err := time.Parse("2006-01", from)
	if err != nil {
		return nil, err
	}
	end, err := time.Parse("2006-01", to)
	if err != nil {
		return nil, err
	}
	if end.Before(start) {
		start, end = end, start
	}
	var months []string
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 1, 0) {
		months = append(months, cur.Format("2006-01"))
	}
	return months, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func classify(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("%w: %v", ErrDuplicate, err)
	}
	return err
}

func statusFor(budget, actual int64) string {
	switch {
	case actual == 0:
		return "not-paid"
	case budget == 0 && actual > 0:
		return "unbudgeted"
	case budget > 0 && actual > budget:
		return "over"
	case budget > 0 && actual == budget:
		return "on-track"
	default:
		return "under"
	}
}

func nullJSON(b []byte) any {
	if string(b) == "null" {
		return nil
	}
	return string(b)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
