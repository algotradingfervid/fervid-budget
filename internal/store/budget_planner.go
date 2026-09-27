package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Budget lines belong to a month and roll up into the existing head budget.
// Payments and reports continue to use budgets.amount, never a second total.
type PlanLine struct {
	Description string `json:"description"`
	Amount      int64  `json:"amount"` // paise
}
type PlanHead struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	ReadOnly bool       `json:"readOnly,omitempty"`
	Lines    []PlanLine `json:"lines"`
}
type PlanProject struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	ReadOnly bool       `json:"readOnly,omitempty"`
	Heads    []PlanHead `json:"heads"`
}
type BudgetPlan struct {
	Month       string        `json:"month"`
	SourceMonth string        `json:"sourceMonth"`
	Revision    string        `json:"revision"`
	Exists      bool          `json:"exists"`
	Locked      bool          `json:"locked"`
	Projects    []PlanProject `json:"projects"`
}
type planQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func upBudgetLines(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS budget_lines (
 id INTEGER PRIMARY KEY, budget_id INTEGER NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
 description TEXT NOT NULL, amount INTEGER NOT NULL CHECK(amount>=0), sort_order INTEGER NOT NULL,
 UNIQUE(budget_id,sort_order));`)
	return err
}
func (s *Store) BudgetPlan(ctx context.Context, month string) (BudgetPlan, error) {
	// Keep month metadata, line values and the revision in one read snapshot.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return BudgetPlan{}, err
	}
	defer tx.Rollback()
	p, err := readBudgetPlan(ctx, tx, month)
	if err != nil {
		return p, err
	}
	return p, tx.Commit()
}
func readBudgetPlan(ctx context.Context, q planQuerier, month string) (BudgetPlan, error) {
	p := BudgetPlan{Month: month, Projects: []PlanProject{}}
	if !validMonth(month) {
		return p, fmt.Errorf("%w: choose a valid budget month", ErrValidation)
	}
	var n int
	if err := q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM budget_months WHERE month=?)+(SELECT COUNT(*) FROM budgets WHERE month=?)`, month, month).Scan(&n); err != nil {
		return p, err
	}
	p.Exists = n > 0
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM month_locks WHERE month=?`, month).Scan(&n); err != nil {
		return p, err
	}
	p.Locked = n > 0
	err := q.QueryRowContext(ctx, `SELECT COALESCE(source_month,'') FROM budget_months WHERE month=?`, month).Scan(&p.SourceMonth)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	rows, err := q.QueryContext(ctx, `SELECT p.id,p.name,p.active,h.id,h.name,h.active,b.id,b.amount,
 COALESCE(l.description,'Monthly allocation'),COALESCE(l.amount,b.amount)
 FROM budgets b JOIN heads h ON h.id=b.head_id JOIN projects p ON p.id=h.project_id
 LEFT JOIN budget_lines l ON l.budget_id=b.id WHERE b.month=?
 ORDER BY p.sort_order,p.name,p.id,h.sort_order,h.name,h.id,l.sort_order,l.id`, month)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	var lastProject, lastHead int64
	for rows.Next() {
		var pid, hid, bid, amount, lineAmount int64
		var pn, hn, description string
		var pa, ha bool
		if err := rows.Scan(&pid, &pn, &pa, &hid, &hn, &ha, &bid, &amount, &description, &lineAmount); err != nil {
			return p, err
		}
		if pid != lastProject {
			p.Projects = append(p.Projects, PlanProject{ID: pid, Name: pn, ReadOnly: !pa, Heads: []PlanHead{}})
			lastProject = pid
			lastHead = 0
		}
		project := &p.Projects[len(p.Projects)-1]
		if hid != lastHead {
			project.Heads = append(project.Heads, PlanHead{ID: hid, Name: hn, ReadOnly: !ha || !pa, Lines: []PlanLine{}})
			lastHead = hid
		}
		head := &project.Heads[len(project.Heads)-1]
		head.Lines = append(head.Lines, PlanLine{Description: description, Amount: lineAmount})
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	raw, _ := json.Marshal(p)
	hash := sha256.Sum256(raw)
	p.Revision = hex.EncodeToString(hash[:])
	return p, nil
}

// SaveBudgetPlan is a single transaction: failed validation must not leave a
// new project, head, month, or partial line set behind. Revision checking stops
// a stale browser from overwriting a plan edited by someone else.
func (s *Store) SaveBudgetPlan(ctx context.Context, actor User, in BudgetPlan, edit bool) error {
	if !validMonth(in.Month) || len(in.Projects) == 0 || len(in.Projects) > 200 {
		return fmt.Errorf("%w: choose a month and add at least one project (maximum 200)", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Acquire the write reservation before the snapshot/read checks. This also
	// serializes locking, legacy budget edits, and duplicate submissions.
	if _, err = tx.ExecContext(ctx, `UPDATE budget_months SET month=month WHERE month=?`, in.Month); err != nil {
		return err
	}
	// Read authorization after reserving the write transaction, so an account
	// or role revoked while a browser was open cannot save using stale grants.
	var actorActive bool
	if err = tx.QueryRowContext(ctx, `SELECT active FROM users WHERE id=?`, actor.ID).Scan(&actorActive); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForbidden
		}
		return err
	}
	if !actorActive {
		return ErrForbidden
	}
	perms := &dbPermissionSet{grants: map[string]map[string]struct{}{}, scopes: map[string]string{}}
	grants, err := tx.QueryContext(ctx, `SELECT rp.resource,rp.action FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id WHERE ur.user_id=?`, actor.ID)
	if err != nil {
		return err
	}
	for grants.Next() {
		var resource, action string
		if err := grants.Scan(&resource, &action); err != nil {
			grants.Close()
			return err
		}
		perms.add(resource, action)
	}
	err = grants.Err()
	grants.Close()
	if err != nil {
		return err
	}
	if !perms.Can("budget", "edit") || !perms.Can("budget", "view") || (!edit && !perms.Can("month", "create")) {
		return ErrForbidden
	}
	before, err := readBudgetPlan(ctx, tx, in.Month)
	if err != nil {
		return err
	}
	if before.Locked {
		return ErrLockedMonth
	}
	if edit {
		if !before.Exists {
			return fmt.Errorf("%w: this budget no longer exists", ErrValidation)
		}
		if in.Revision == "" || in.Revision != before.Revision {
			return fmt.Errorf("%w: this budget changed since you opened it; reload the latest budget before saving", ErrValidation)
		}
	} else if before.Exists {
		return fmt.Errorf("%w: a budget already exists for %s; open it to edit instead", ErrValidation, in.Month)
	}
	if !edit && in.SourceMonth != "" {
		if !validMonth(in.SourceMonth) || in.SourceMonth == in.Month {
			return fmt.Errorf("%w: choose a different valid source month", ErrValidation)
		}
		source, err := readBudgetPlan(ctx, tx, in.SourceMonth)
		if err != nil {
			return err
		}
		if !source.Exists {
			return fmt.Errorf("%w: source budget no longer exists", ErrValidation)
		}
	}
	source := in.SourceMonth
	if edit {
		source = before.SourceMonth
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO budget_months(month,status,source_month,created_by) VALUES(?,'open',NULLIF(?,''),?) ON CONFLICT(month) DO NOTHING`, in.Month, source, actor.ID); err != nil {
		return classify(err)
	}
	oldHeads := map[int64]PlanHead{}
	for _, p := range before.Projects {
		for _, h := range p.Heads {
			oldHeads[h.ID] = h
		}
	}
	seenProjects := map[int64]bool{}
	seenHeads := map[int64]bool{}
	names := map[string]bool{}
	lineCount := 0
	var total int64
	const maxTotal int64 = 900000000000000 // below JavaScript's exact-integer ceiling
	for _, project := range in.Projects {
		project.Name = strings.TrimSpace(project.Name)
		if project.ID < 0 || len(project.Heads) == 0 || len(project.Heads) > 500 {
			return fmt.Errorf("%w: each project needs expense heads (maximum 500)", ErrValidation)
		}
		pid := project.ID
		var projectActive bool
		if pid == 0 {
			if !perms.Can("project", "create") {
				return ErrForbidden
			}
			if project.Name == "" || len([]rune(project.Name)) > 120 {
				return fmt.Errorf("%w: project names must contain 1–120 characters", ErrValidation)
			}
			var exists int
			if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE lower(trim(name))=lower(?)`, project.Name).Scan(&exists); err != nil {
				return err
			}
			if exists > 0 {
				return fmt.Errorf("%w: project %q already exists; choose it from existing projects", ErrValidation, project.Name)
			}
			key := strings.ToLower(project.Name)
			if names[key] {
				return fmt.Errorf("%w: duplicate project name", ErrValidation)
			}
			names[key] = true
			res, e := tx.ExecContext(ctx, `INSERT INTO projects(name) VALUES(?)`, project.Name)
			if e != nil {
				return classify(e)
			}
			pid, e = res.LastInsertId()
			if e != nil {
				return e
			}
			projectActive = true
		} else {
			if err = tx.QueryRowContext(ctx, `SELECT name,active FROM projects WHERE id=?`, pid).Scan(&project.Name, &projectActive); err != nil {
				return fmt.Errorf("%w: choose an existing project", ErrValidation)
			}
		}
		if seenProjects[pid] {
			return fmt.Errorf("%w: project is included more than once", ErrValidation)
		}
		seenProjects[pid] = true
		for _, head := range project.Heads {
			head.Name = strings.TrimSpace(head.Name)
			hid := head.ID
			var active bool
			if hid < 0 || len(head.Lines) == 0 || len(head.Lines) > 500 {
				return fmt.Errorf("%w: every expense head needs at least one budget line (maximum 500)", ErrValidation)
			}
			if hid == 0 {
				if !perms.Can("head", "create") {
					return ErrForbidden
				}
				if !projectActive {
					return fmt.Errorf("%w: retired projects cannot receive new heads", ErrValidation)
				}
				if head.Name == "" || len([]rune(head.Name)) > 120 {
					return fmt.Errorf("%w: expense head names must contain 1–120 characters", ErrValidation)
				}
				var exists int
				if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM heads WHERE project_id=? AND lower(trim(name))=lower(?)`, pid, head.Name).Scan(&exists); err != nil {
					return err
				}
				if exists > 0 {
					return fmt.Errorf("%w: head %q already exists; choose it from existing heads", ErrValidation, head.Name)
				}
				res, e := tx.ExecContext(ctx, `INSERT INTO heads(project_id,name) VALUES(?,?)`, pid, head.Name)
				if e != nil {
					return classify(e)
				}
				hid, e = res.LastInsertId()
				if e != nil {
					return e
				}
				active = true
			} else {
				var parent int64
				if err = tx.QueryRowContext(ctx, `SELECT project_id,name,active FROM heads WHERE id=?`, hid).Scan(&parent, &head.Name, &active); err != nil || parent != pid {
					return fmt.Errorf("%w: head does not belong to this project", ErrValidation)
				}
			}
			if seenHeads[hid] {
				return fmt.Errorf("%w: expense head is included more than once", ErrValidation)
			}
			seenHeads[hid] = true
			if !active || !projectActive {
				old, ok := oldHeads[hid]
				a, _ := json.Marshal(old.Lines)
				b, _ := json.Marshal(head.Lines)
				if !edit || !ok || string(a) != string(b) {
					return fmt.Errorf("%w: retired heads are read-only", ErrValidation)
				}
			}
			// Normalize a private slice; callers may reuse their draft concurrently.
			head.Lines = append([]PlanLine(nil), head.Lines...)
			var amount int64
			for i := range head.Lines {
				l := &head.Lines[i]
				l.Description = strings.TrimSpace(l.Description)
				lineCount++
				if l.Description == "" || len([]rune(l.Description)) > 240 || l.Amount < 0 || l.Amount > maxTotal || amount > maxTotal-l.Amount || total > maxTotal-l.Amount || lineCount > 5000 {
					return fmt.Errorf("%w: each line needs a description (up to 240 characters) and a non-negative amount; the plan may contain up to 5,000 lines", ErrValidation)
				}
				amount += l.Amount
				total += l.Amount
			}
			if !active || !projectActive {
				continue
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO budgets(head_id,month,amount) VALUES(?,?,?) ON CONFLICT(head_id,month) DO UPDATE SET amount=excluded.amount,updated_at=CURRENT_TIMESTAMP`, hid, in.Month, amount)
			if err != nil {
				return classify(err)
			}
			var bid int64
			if err = tx.QueryRowContext(ctx, `SELECT id FROM budgets WHERE head_id=? AND month=?`, hid, in.Month).Scan(&bid); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM budget_lines WHERE budget_id=?`, bid); err != nil {
				return err
			}
			for i, l := range head.Lines {
				if _, err = tx.ExecContext(ctx, `INSERT INTO budget_lines(budget_id,description,amount,sort_order) VALUES(?,?,?,?)`, bid, l.Description, l.Amount, i); err != nil {
					return err
				}
			}
		}
	}
	for hid := range oldHeads {
		if !seenHeads[hid] {
			return fmt.Errorf("%w: an existing expense head is missing; retain its lines or set their amounts to zero", ErrValidation)
		}
	}
	after, err := readBudgetPlan(ctx, tx, in.Month)
	if err != nil {
		return err
	}
	oldJSON, _ := json.Marshal(before)
	newJSON, _ := json.Marshal(after)
	action := "create"
	if edit {
		action = "update"
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_log(actor_id,actor_name,action,entity_type,summary,before_json,after_json) VALUES(?,?,?,?,?,?,?)`, actor.ID, actor.Name, action, "budget_month", fmt.Sprintf("Saved budget plan %s with %d projects and %d budget lines", in.Month, len(in.Projects), lineCount), string(oldJSON), string(newJSON)); err != nil {
		return err
	}
	return tx.Commit()
}
