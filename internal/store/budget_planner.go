// budget_planner.go stores a month's budget as line items under each project
// head, for the budget planner screen. The lines of a head add up to that
// head's budgets.amount, which stays the single figure payments and reports
// use. A save runs in one transaction and is refused if the plan changed since
// the browser loaded it.
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

// Limits on one saved plan. maxPlanTotal keeps every amount and sum below
// JavaScript's exact-integer ceiling, so the planner screen never rounds.
const (
	maxPlanProjects        = 200
	maxPlanHeadsPerProject = 500
	maxPlanLinesPerHead    = 500
	maxPlanLines           = 5000
	maxPlanNameRunes       = 120
	maxPlanLineDescRunes   = 240
	maxPlanTotal           = int64(900000000000000)
)

// PlanLine is one budget line: a description and an amount in paise.
type PlanLine struct {
	Description string `json:"description"`
	Amount      int64  `json:"amount"` // paise
}

// PlanHead is one expense head in a plan with its budget lines. ReadOnly marks
// a retired head (or a head of a retired project), whose lines cannot change.
type PlanHead struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	ReadOnly bool       `json:"readOnly,omitempty"`
	Lines    []PlanLine `json:"lines"`
}

// PlanProject is one project in a plan with its heads. ReadOnly marks a
// retired project. An ID of 0 in a saved plan asks for a new project.
type PlanProject struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	ReadOnly bool       `json:"readOnly,omitempty"`
	Heads    []PlanHead `json:"heads"`
}

// BudgetPlan is a whole month's budget, grouped project -> head -> lines, as
// the planner screen edits it. SourceMonth is the month it was copied from, if
// any. Revision fingerprints the plan so a save from a stale browser tab can be
// refused. Exists and Locked describe the month when the plan was read.
type BudgetPlan struct {
	Month       string        `json:"month"`
	SourceMonth string        `json:"sourceMonth"`
	Revision    string        `json:"revision"`
	Exists      bool          `json:"exists"`
	Locked      bool          `json:"locked"`
	Projects    []PlanProject `json:"projects"`
}

// planQuerier is what readBudgetPlan needs: the database or a transaction.
type planQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// upBudgetLines is the migration that creates the budget_lines table.
func upBudgetLines(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS budget_lines (
 id INTEGER PRIMARY KEY, budget_id INTEGER NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
 description TEXT NOT NULL, amount INTEGER NOT NULL CHECK(amount>=0), sort_order INTEGER NOT NULL,
 UNIQUE(budget_id,sort_order));`)
	return err
}

// BudgetPlan reads a month's plan. The month details, the lines and the
// revision come from one read-only snapshot, so they always agree.
func (s *Store) BudgetPlan(ctx context.Context, month string) (BudgetPlan, error) {
	tx, err := s.beginReadTx(ctx)
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

// readBudgetPlan loads a month's plan through q. A head budgeted without line
// items shows its budget as one "Monthly allocation" line. Revision is the
// SHA-256 of the plan's JSON, so any change to the plan changes it.
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

// SaveBudgetPlan creates (edit=false) or replaces (edit=true) a month's plan
// in one transaction, so a refusal leaves no new project, head, month or
// partial line set behind. The actor's grants are re-read inside the
// transaction, so a role revoked while the browser was open cannot save. An
// edit must carry the revision it was loaded with. New projects and heads (ID
// 0) are created when the actor may create them. Retired heads must come back
// unchanged, and no existing head may be left out.
func (s *Store) SaveBudgetPlan(ctx context.Context, actor User, in BudgetPlan, edit bool) error {
	if !validMonth(in.Month) || len(in.Projects) == 0 || len(in.Projects) > maxPlanProjects {
		return fmt.Errorf("%w: choose a month and add at least one project (maximum 200)", ErrValidation)
	}
	// The write lock is held from BEGIN, which serialises this save against
	// month locking, other budget edits and a duplicate submission.
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// An inactive or missing user gets an empty permission set.
	perms, err := effectivePermissionsQ(ctx, tx, actor.ID)
	if err != nil {
		return err
	}
	if !perms.Can("budget", "edit") || !perms.Can("budget", "view") || (!edit && !perms.Can("month", "create")) {
		return ErrForbidden
	}
	before, err := checkPlanPreconditions(ctx, tx, in, edit)
	if err != nil {
		return err
	}
	source := in.SourceMonth
	if edit {
		source = before.SourceMonth
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO budget_months(month,status,source_month,created_by) VALUES(?,'open',NULLIF(?,''),?) ON CONFLICT(month) DO NOTHING`, in.Month, source, actor.ID); err != nil {
		return classify(err)
	}
	w := newPlanWriter(tx, perms, in.Month, before, edit)
	for _, project := range in.Projects {
		if err := w.saveProject(ctx, project); err != nil {
			return err
		}
	}
	for hid := range w.oldHeads {
		if !w.seenHeads[hid] {
			return fmt.Errorf("%w: an existing expense head is missing; retain its lines or set their amounts to zero", ErrValidation)
		}
	}
	after, err := readBudgetPlan(ctx, tx, in.Month)
	if err != nil {
		return err
	}
	action := "create"
	if edit {
		action = "update"
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: action, EntityType: "budget_month",
		Summary: fmt.Sprintf("Saved budget plan %s with %d projects and %d budget lines", in.Month, len(in.Projects), w.lineCount),
		Before:  before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

// checkPlanPreconditions reads the month's current plan inside tx and checks
// that it may be saved: the month is not locked; an edit targets an existing
// plan at the revision it was loaded with; a new plan does not already exist,
// and its source month (if any) is a different month that has a plan. It
// returns the current plan, which is the audit row's "before".
func checkPlanPreconditions(ctx context.Context, tx *sql.Tx, in BudgetPlan, edit bool) (BudgetPlan, error) {
	before, err := readBudgetPlan(ctx, tx, in.Month)
	if err != nil {
		return before, err
	}
	if before.Locked {
		return before, ErrLockedMonth
	}
	if edit {
		if !before.Exists {
			return before, fmt.Errorf("%w: this budget no longer exists", ErrValidation)
		}
		if in.Revision == "" || in.Revision != before.Revision {
			return before, fmt.Errorf("%w: this budget changed since you opened it; reload the latest budget before saving", ErrValidation)
		}
	} else if before.Exists {
		return before, fmt.Errorf("%w: a budget already exists for %s; open it to edit instead", ErrValidation, in.Month)
	}
	if !edit && in.SourceMonth != "" {
		if !validMonth(in.SourceMonth) || in.SourceMonth == in.Month {
			return before, fmt.Errorf("%w: choose a different valid source month", ErrValidation)
		}
		source, err := readBudgetPlan(ctx, tx, in.SourceMonth)
		if err != nil {
			return before, err
		}
		if !source.Exists {
			return before, fmt.Errorf("%w: source budget no longer exists", ErrValidation)
		}
	}
	return before, nil
}

// planWriter carries the state of one SaveBudgetPlan call across its projects
// and heads: which projects, heads and new project names were already seen,
// the running line count and total, and the heads of the plan being replaced.
type planWriter struct {
	tx           *sql.Tx
	perms        PermissionSet
	month        string
	edit         bool
	oldHeads     map[int64]PlanHead
	seenProjects map[int64]bool
	seenHeads    map[int64]bool
	names        map[string]bool
	lineCount    int
	total        int64
}

// newPlanWriter prepares a planWriter for saving over the plan before.
func newPlanWriter(tx *sql.Tx, perms PermissionSet, month string, before BudgetPlan, edit bool) *planWriter {
	w := &planWriter{tx: tx, perms: perms, month: month, edit: edit,
		oldHeads: map[int64]PlanHead{}, seenProjects: map[int64]bool{},
		seenHeads: map[int64]bool{}, names: map[string]bool{}}
	for _, p := range before.Projects {
		for _, h := range p.Heads {
			w.oldHeads[h.ID] = h
		}
	}
	return w
}

// saveProject resolves one project of the plan (creating it when its ID is 0)
// and saves each of its heads.
func (w *planWriter) saveProject(ctx context.Context, project PlanProject) error {
	project.Name = strings.TrimSpace(project.Name)
	if project.ID < 0 || len(project.Heads) == 0 || len(project.Heads) > maxPlanHeadsPerProject {
		return fmt.Errorf("%w: each project needs expense heads (maximum 500)", ErrValidation)
	}
	pid, projectActive, err := w.resolveProject(ctx, project)
	if err != nil {
		return err
	}
	if w.seenProjects[pid] {
		return fmt.Errorf("%w: project is included more than once", ErrValidation)
	}
	w.seenProjects[pid] = true
	for _, head := range project.Heads {
		if err := w.saveHead(ctx, pid, projectActive, head); err != nil {
			return err
		}
	}
	return nil
}

// resolveProject returns the id and active flag of the plan's project. ID 0
// creates a new project, which needs project:create and a name that is 1-120
// characters and not already taken (in the database or earlier in this plan).
// Any other ID must be an existing project.
func (w *planWriter) resolveProject(ctx context.Context, project PlanProject) (int64, bool, error) {
	if project.ID != 0 {
		var active bool
		if err := w.tx.QueryRowContext(ctx, `SELECT name,active FROM projects WHERE id=?`, project.ID).Scan(&project.Name, &active); err != nil {
			return 0, false, fmt.Errorf("%w: choose an existing project", ErrValidation)
		}
		return project.ID, active, nil
	}
	if !w.perms.Can("project", "create") {
		return 0, false, ErrForbidden
	}
	if project.Name == "" || len([]rune(project.Name)) > maxPlanNameRunes {
		return 0, false, fmt.Errorf("%w: project names must contain 1–120 characters", ErrValidation)
	}
	var exists int
	if err := w.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE lower(trim(name))=lower(?)`, project.Name).Scan(&exists); err != nil {
		return 0, false, err
	}
	if exists > 0 {
		return 0, false, fmt.Errorf("%w: project %q already exists; choose it from existing projects", ErrValidation, project.Name)
	}
	key := strings.ToLower(project.Name)
	if w.names[key] {
		return 0, false, fmt.Errorf("%w: duplicate project name", ErrValidation)
	}
	w.names[key] = true
	res, err := w.tx.ExecContext(ctx, `INSERT INTO projects(name) VALUES(?)`, project.Name)
	if err != nil {
		return 0, false, classify(err)
	}
	pid, err := res.LastInsertId()
	if err != nil {
		return 0, false, err
	}
	return pid, true, nil
}

// saveHead resolves one head of project pid (creating it when its ID is 0),
// validates and totals its lines, and, when the head and project are open,
// writes the head's budget amount and replaces its lines. A retired head is
// only accepted in an edit that sends its lines back unchanged.
func (w *planWriter) saveHead(ctx context.Context, pid int64, projectActive bool, head PlanHead) error {
	head.Name = strings.TrimSpace(head.Name)
	if head.ID < 0 || len(head.Lines) == 0 || len(head.Lines) > maxPlanLinesPerHead {
		return fmt.Errorf("%w: every expense head needs at least one budget line (maximum 500)", ErrValidation)
	}
	hid, active, err := w.resolveHead(ctx, pid, projectActive, head)
	if err != nil {
		return err
	}
	if w.seenHeads[hid] {
		return fmt.Errorf("%w: expense head is included more than once", ErrValidation)
	}
	w.seenHeads[hid] = true
	if !active || !projectActive {
		old, ok := w.oldHeads[hid]
		if !w.edit || !ok || !samePlanLines(old.Lines, head.Lines) {
			return fmt.Errorf("%w: retired heads are read-only", ErrValidation)
		}
	}
	// Work on a private copy: the caller may still be using its slice.
	head.Lines = append([]PlanLine(nil), head.Lines...)
	var amount int64
	for i := range head.Lines {
		l := &head.Lines[i]
		l.Description = strings.TrimSpace(l.Description)
		w.lineCount++
		if l.Description == "" || len([]rune(l.Description)) > maxPlanLineDescRunes || l.Amount < 0 || l.Amount > maxPlanTotal || amount > maxPlanTotal-l.Amount || w.total > maxPlanTotal-l.Amount || w.lineCount > maxPlanLines {
			return fmt.Errorf("%w: each line needs a description (up to 240 characters) and a non-negative amount; the plan may contain up to 5,000 lines", ErrValidation)
		}
		amount += l.Amount
		w.total += l.Amount
	}
	if !active || !projectActive {
		return nil
	}
	return w.writeHeadBudget(ctx, hid, amount, head.Lines)
}

// resolveHead returns the id and active flag of a head under project pid. ID 0
// creates a new head, which needs head:create, an open project, and a name that
// is 1-120 characters and not already used in the project. Any other ID must
// be an existing head of project pid.
func (w *planWriter) resolveHead(ctx context.Context, pid int64, projectActive bool, head PlanHead) (int64, bool, error) {
	if head.ID != 0 {
		var parent int64
		var active bool
		if err := w.tx.QueryRowContext(ctx, `SELECT project_id,name,active FROM heads WHERE id=?`, head.ID).Scan(&parent, &head.Name, &active); err != nil || parent != pid {
			return 0, false, fmt.Errorf("%w: head does not belong to this project", ErrValidation)
		}
		return head.ID, active, nil
	}
	if !w.perms.Can("head", "create") {
		return 0, false, ErrForbidden
	}
	if !projectActive {
		return 0, false, fmt.Errorf("%w: retired projects cannot receive new heads", ErrValidation)
	}
	if head.Name == "" || len([]rune(head.Name)) > maxPlanNameRunes {
		return 0, false, fmt.Errorf("%w: expense head names must contain 1–120 characters", ErrValidation)
	}
	var exists int
	if err := w.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM heads WHERE project_id=? AND lower(trim(name))=lower(?)`, pid, head.Name).Scan(&exists); err != nil {
		return 0, false, err
	}
	if exists > 0 {
		return 0, false, fmt.Errorf("%w: head %q already exists; choose it from existing heads", ErrValidation, head.Name)
	}
	res, err := w.tx.ExecContext(ctx, `INSERT INTO heads(project_id,name) VALUES(?,?)`, pid, head.Name)
	if err != nil {
		return 0, false, classify(err)
	}
	hid, err := res.LastInsertId()
	if err != nil {
		return 0, false, err
	}
	return hid, true, nil
}

// writeHeadBudget sets head hid's budget for the month to amount and replaces
// its budget lines with lines, in order.
func (w *planWriter) writeHeadBudget(ctx context.Context, hid, amount int64, lines []PlanLine) error {
	if _, err := w.tx.ExecContext(ctx, `INSERT INTO budgets(head_id,month,amount) VALUES(?,?,?) ON CONFLICT(head_id,month) DO UPDATE SET amount=excluded.amount,updated_at=CURRENT_TIMESTAMP`, hid, w.month, amount); err != nil {
		return classify(err)
	}
	var bid int64
	if err := w.tx.QueryRowContext(ctx, `SELECT id FROM budgets WHERE head_id=? AND month=?`, hid, w.month).Scan(&bid); err != nil {
		return err
	}
	if _, err := w.tx.ExecContext(ctx, `DELETE FROM budget_lines WHERE budget_id=?`, bid); err != nil {
		return err
	}
	for i, l := range lines {
		if _, err := w.tx.ExecContext(ctx, `INSERT INTO budget_lines(budget_id,description,amount,sort_order) VALUES(?,?,?,?)`, bid, l.Description, l.Amount, i); err != nil {
			return err
		}
	}
	return nil
}

// samePlanLines reports whether two line lists are identical as JSON. A nil
// list ("null") and an empty one ("[]") count as different, as the planner
// screen always sends a list.
func samePlanLines(a, b []PlanLine) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

// DetailedBudgetHeads returns the heads that have line items in month. A head
// not in the map was budgeted as a single amount, without line items.
func (s *Store) DetailedBudgetHeads(ctx context.Context, month string) (map[int64]bool, error) {
	out := map[int64]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT b.head_id FROM budget_lines l JOIN budgets b ON b.id=l.budget_id WHERE b.month=?`, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
