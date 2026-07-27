package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// recoverableCategorySeed is the six default categories.
//
// code is the bridge to Phase 2. payment_requests.recoverable_category already
// stores exactly these codes, and validateRequestInput keyed its hardcoded
// rules map on them, so the code — not the display name — is the stable
// identity. name is the admin-editable label the screens show.
//
// The flags reproduce recoverableCategoryRules exactly. Moving validation from
// that map onto these rows must change no behaviour, and
// TestSeededCategoriesMatchPhase2Rules fails if the two ever drift.
var recoverableCategorySeed = []struct {
	code, name     string
	proj, cp, sort int
}{
	{"employee_advance", "Employee advance", 0, 0, 1},
	{"emd", "EMD", 1, 0, 2},
	{"pbg", "PBG", 1, 0, 3},
	{"icd", "ICD", 0, 1, 4},
	{"security_deposit", "Security deposit", 0, 1, 5},
	{"other", "Other", 0, 0, 6},
}

// upRecoverableCategories is migration v6: it creates the admin-configurable
// recoverable_categories table, seeds the six defaults and links the requests
// Phase 2 already wrote. It is idempotent — re-running on a migrated database
// inserts nothing new and re-links nothing.
func upRecoverableCategories(tx *sql.Tx) error {
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS recoverable_categories (
  id INTEGER PRIMARY KEY,
  code TEXT NOT NULL,
  name TEXT NOT NULL,
  requires_project INTEGER NOT NULL DEFAULT 0,
  requires_counterparty INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_recoverable_categories_code ON recoverable_categories(code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_recoverable_categories_name_nocase ON recoverable_categories(lower(name));`); err != nil {
		return err
	}
	for _, c := range recoverableCategorySeed {
		if _, err := tx.Exec(`INSERT INTO recoverable_categories(code,name,requires_project,requires_counterparty,active,sort_order)
			SELECT ?,?,?,?,1,? WHERE NOT EXISTS(SELECT 1 FROM recoverable_categories WHERE code=?)`,
			c.code, c.name, c.proj, c.cp, c.sort, c.code); err != nil {
			return err
		}
	}
	return backfillRecoverableCategoryIDs(tx)
}

// backfillRecoverableCategoryIDs links payment_requests rows to their category
// row by the Phase-2 code.
//
// Phase 2 wrote recoverable_category (the code) and hardcoded
// recoverable_category_id to NULL because recoverable_categories did not exist
// yet. Every Phase-4 register query joins on the id, so without this an
// installed database renders a blank category for every request ever raised.
// It only fills NULLs, so it never overwrites a deliberate later change.
func backfillRecoverableCategoryIDs(tx *sql.Tx) error {
	_, err := tx.Exec(`UPDATE payment_requests
		SET recoverable_category_id = (SELECT rc.id FROM recoverable_categories rc WHERE rc.code = payment_requests.recoverable_category)
		WHERE recoverable_category_id IS NULL
		  AND COALESCE(recoverable_category,'') <> ''
		  AND EXISTS(SELECT 1 FROM recoverable_categories rc WHERE rc.code = payment_requests.recoverable_category)`)
	return err
}

// Requires renders the two per-category field rules as one phrase for the
// Configuration screen. The presentation lives here, not in the template, so a
// category an admin adds later describes itself correctly with no template edit.
func (c RecoverableCategory) Requires() string {
	switch {
	case c.RequiresProject && c.RequiresCounterparty:
		return "Related project and counterparty company"
	case c.RequiresProject:
		return "Related project"
	case c.RequiresCounterparty:
		return "Counterparty company"
	default:
		return "Nothing extra"
	}
}

// categoryCode derives the stable code for a newly added category from its
// name. Existing rows keep the code they were created with, so renaming a
// category never orphans the requests pointing at it.
func categoryCode(name string) string {
	var b strings.Builder
	lastUnderscore := true // trims leading separators
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		case !lastUnderscore:
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

// recoverableRules is the rule set validateRequestInput enforces, read from the
// table rather than the built-in map so a category an admin adds is usable at
// once (V4). Only active categories are offered, so deactivating one stops new
// requests naming it — which is the whole point of the Active switch.
func (s *Store) recoverableRules(ctx context.Context) (map[string]recoverableRule, error) {
	cats, err := s.ListRecoverableCategories(ctx, true)
	if err != nil {
		return nil, err
	}
	rules := make(map[string]recoverableRule, len(cats))
	for _, c := range cats {
		rules[c.Code] = recoverableRule{RequiresProject: c.RequiresProject, RequiresCounterparty: c.RequiresCounterparty}
	}
	return rules, nil
}

// recoverableCategoryLink resolves a request's category code to the row id
// stored in payment_requests.recoverable_category_id, or nil when the request
// is not recoverable. Every Phase-4 register query joins on that id.
func (s *Store) recoverableCategoryLink(ctx context.Context, treatment, code string) (any, error) {
	if treatment != "recoverable" || strings.TrimSpace(code) == "" {
		return nil, nil
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM recoverable_categories WHERE code=?`, code).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil // validation already rejected unknown codes
	}
	if err != nil {
		return nil, err
	}
	return id, nil
}

const recoverableCategorySelect = `SELECT id,code,name,requires_project,requires_counterparty,active,sort_order,created_at
	FROM recoverable_categories`

func scanRecoverableCategory(sc interface{ Scan(...any) error }) (RecoverableCategory, error) {
	var c RecoverableCategory
	var rp, rc, active int
	if err := sc.Scan(&c.ID, &c.Code, &c.Name, &rp, &rc, &active, &c.SortOrder, &c.CreatedAt); err != nil {
		return c, err
	}
	c.RequiresProject, c.RequiresCounterparty, c.Active = rp == 1, rc == 1, active == 1
	return c, nil
}

func (s *Store) ListRecoverableCategories(ctx context.Context, activeOnly bool) ([]RecoverableCategory, error) {
	q := recoverableCategorySelect
	if activeOnly {
		q += ` WHERE active=1`
	}
	q += ` ORDER BY sort_order,name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecoverableCategory
	for rows.Next() {
		c, err := scanRecoverableCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) UpsertRecoverableCategory(ctx context.Context, actor User, id int64, name string, requiresProject, requiresCounterparty, active bool, sortOrder int) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("%w: recoverable category name is required", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var savedID int64
	action := "update"
	if id == 0 {
		code := categoryCode(name)
		if code == "" {
			return 0, fmt.Errorf("%w: recoverable category name must contain a letter or digit", ErrValidation)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO recoverable_categories(code,name,requires_project,requires_counterparty,active,sort_order) VALUES(?,?,?,?,?,?)`,
			code, name, boolInt(requiresProject), boolInt(requiresCounterparty), boolInt(active), sortOrder)
		if err != nil {
			return 0, classify(err)
		}
		if savedID, err = res.LastInsertId(); err != nil {
			return 0, err
		}
		action = "create"
	} else {
		// code is deliberately not in the SET list: it is the identity existing
		// requests resolve their field rules by.
		if _, err := tx.ExecContext(ctx, `UPDATE recoverable_categories SET name=?, requires_project=?, requires_counterparty=?, active=?, sort_order=? WHERE id=?`,
			name, boolInt(requiresProject), boolInt(requiresCounterparty), boolInt(active), sortOrder, id); err != nil {
			return 0, classify(err)
		}
		savedID = id
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: action,
		EntityType: "recoverable_category", EntityID: &savedID, Summary: "Saved recoverable category " + name}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return savedID, nil
}

// DeleteRecoverableCategory removes a category outright, refusing while any
// request still points at it (F-E-06).
//
// recoverable_category:delete has been a grantable permission since Phase 4 with
// nothing behind it: the only mutating route calls UpsertRecoverableCategory,
// which only ever INSERTs or UPDATEs, so an administrator could grant "delete"
// on this resource and buy exactly nothing. This is the store half of consuming
// it.
//
// The refusal, not the delete, is the point. payment_requests.recoverable_category_id
// deliberately carries no REFERENCES clause — the v3 schema comment explains why
// (a forward foreign key to a table Phase 4 had not created yet would have made
// every request write fail) — so the database will NOT stop a delete from
// orphaning history. Nothing would break loudly: the register would render a
// blank category for every affected request and the rules that category enforced
// would quietly stop applying. So the pre-check is the only guard there is, and
// it runs inside the same transaction as the DELETE, which is what stops a
// request raised between the count and the write from being orphaned anyway.
//
// It mirrors DeleteRole's holder pre-check exactly: an ErrForbidden carrying the
// count, so the handler can say how many rather than "you do not have
// permission". The count asks the same question ListRecoverableCategoriesWithUsage
// answers as InUse — the number of requests whose recoverable_category_id is this
// row — so the Delete button's refusal and the "in use" figure printed beside it
// can never disagree. TestDeleteRecoverableCategoryRefusalAgreesWithInUse pins that.
//
// Deactivating (active=0) remains the ordinary way to retire a category, and is
// the only thing that works once it has been used. That is the Phase-4 design
// ("no hard delete... a category may be referenced by historical requests"), and
// this makes the permission honest without softening it: delete is for a
// category added by mistake, before anything has named it.
func (s *Store) DeleteRecoverableCategory(ctx context.Context, actor User, id int64) error {
	// beginWriteTx takes the write lock with the transaction's first statement:
	// this is a read-then-write, the shape that hands the loser SQLITE_BUSY when
	// the transaction starts as a reader.
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var name string
	err = tx.QueryRowContext(ctx, `SELECT name FROM recoverable_categories WHERE id=?`, id).Scan(&name)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var inUse int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_requests WHERE recoverable_category_id=?`, id).Scan(&inUse); err != nil {
		return err
	}
	if inUse > 0 {
		return fmt.Errorf("%w: %s is used by %d %s — deactivate it instead, so those requests keep their category",
			ErrForbidden, name, inUse, pluralRequests(inUse))
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM recoverable_categories WHERE id=?`, id); err != nil {
		return classify(err)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "delete",
		EntityType: "recoverable_category", EntityID: &id, Summary: "Deleted recoverable category " + name,
		Before: map[string]any{"id": id, "name": name}}); err != nil {
		return err
	}
	return tx.Commit()
}

func pluralRequests(n int) string {
	if n == 1 {
		return "request"
	}
	return "requests"
}

func (s *Store) ListRecoverableCategoriesWithUsage(ctx context.Context) ([]RecoverableCategoryUsage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.code,c.name,c.requires_project,c.requires_counterparty,c.active,c.sort_order,c.created_at,
		(SELECT COUNT(*) FROM payment_requests pr WHERE pr.recoverable_category_id=c.id)
		FROM recoverable_categories c ORDER BY c.sort_order,c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecoverableCategoryUsage
	for rows.Next() {
		var u RecoverableCategoryUsage
		var rp, rc, active int
		if err := rows.Scan(&u.ID, &u.Code, &u.Name, &rp, &rc, &active, &u.SortOrder, &u.CreatedAt, &u.InUse); err != nil {
			return nil, err
		}
		u.RequiresProject, u.RequiresCounterparty, u.Active = rp == 1, rc == 1, active == 1
		out = append(out, u)
	}
	return out, rows.Err()
}

// recoverableBaseWhere is the single definition of "a live recoverable", shared
// by RecoverableReport, RecoverableMetrics and RecoverableRollups so the
// dashboard totals can never drift from the list underneath them. A request
// that died before any money left never held money, so it is not outstanding.
const recoverableBaseWhere = `pr.treatment='recoverable' AND pr.status NOT IN ('rejected','cancelled','withdrawn')`

// recoverableAmount is the money considered at risk: what actually left when the
// payment exists, otherwise what was approved, otherwise what was asked for.
const recoverableAmount = `COALESCE(py.amount, COALESCE(pr.approved_amount, pr.amount))`

// recoverableScope is the row-visibility half of recoverableBaseWhere: the same
// predicate the app's canViewRequest applies, expressed for the `pr` alias so
// every recoverable read enforces it in SQL rather than after the fact.
//
// It mirrors canViewRequest and NOT requestWhere, deliberately. The two already
// differ — requestWhere's "assigned" is `manager_id` alone, while canViewRequest
// admits `manager_id OR requester_id` — and canViewRequest is the authority on
// whether a caller may see a given row, because it is what /requests/{id} answers
// with. A register that showed a row whose detail page 404s, or hid one whose
// detail page opens, would be the same class of disagreement F-G-016 was.
//
// An unrecognised or empty scope returns `AND 0`: no rows. See RecoverableViewer.
func recoverableScope(v RecoverableViewer) (string, []any) {
	switch v.Scope {
	case ScopeAll:
		return "", nil
	case "assigned":
		return ` AND (pr.manager_id=? OR pr.requester_id=?)`, []any{v.ViewerID, v.ViewerID}
	case "own":
		return ` AND pr.requester_id=?`, []any{v.ViewerID}
	default:
		return ` AND 0`, nil
	}
}

// recoverableAgeing turns a row's payment state and its distance from the
// expected return date into the pill text and pill modifier the register
// renders. days is whole calendar days from "now" to the expected return date
// (negative = overdue).
func recoverableAgeing(paidOn, expectedReturn string, onHold bool, days int) (label, tone string, overdue bool) {
	switch {
	case onHold:
		return "On hold", "hold", false
	case paidOn == "":
		return "Awaiting payment", "approved", false
	case expectedReturn == "":
		return "No fixed date", "neutral", false
	case days < 0:
		return fmt.Sprintf("%d days overdue", -days), "bad", true
	case days == 0:
		return "Due today", "neutral", false
	default:
		return fmt.Sprintf("%d days to go", days), "neutral", false
	}
}

func (s *Store) RecoverableReport(ctx context.Context, opts RecoverableReportOptions) ([]RecoverableRow, error) {
	asOf := opts.AsOf
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	today := asOf.UTC().Format("2006-01-02")

	q := `SELECT pr.id, pr.number, COALESCE(rc.name,''), COALESCE(pr.counterparty,''), COALESCE(p.name,''),
		` + recoverableAmount + ` AS amt,
		COALESCE(py.paid_on,''), COALESCE(pr.expected_return_date,''), COALESCE(pr.repayment_notes,''),
		pr.status, COALESCE(u.name,''), COALESCE(pr.on_hold,0),
		CAST(julianday(COALESCE(NULLIF(pr.expected_return_date,''),?)) - julianday(?) AS INTEGER) AS days
		FROM payment_requests pr
		LEFT JOIN payments py ON py.request_id=pr.id AND py.voided_at IS NULL
		LEFT JOIN recoverable_categories rc ON rc.id=pr.recoverable_category_id
		LEFT JOIN projects p ON p.id=pr.project_id
		LEFT JOIN users u ON u.id=pr.requester_id
		WHERE ` + recoverableBaseWhere
	args := []any{today, today}

	scopeClause, scopeArgs := recoverableScope(opts.Viewer)
	q += scopeClause
	args = append(args, scopeArgs...)

	if validMonth(opts.From) || validMonth(opts.To) {
		from, to := opts.From, opts.To
		if !validMonth(from) {
			from = to
		}
		if !validMonth(to) {
			to = from
		}
		if to < from {
			from, to = to, from
		}
		q += ` AND substr(COALESCE(py.paid_on,''),1,7) BETWEEN ? AND ?`
		args = append(args, from, to)
	}
	if opts.CategoryID > 0 {
		q += ` AND pr.recoverable_category_id=?`
		args = append(args, opts.CategoryID)
	}
	if cp := strings.TrimSpace(opts.Counterparty); cp != "" {
		q += ` AND lower(COALESCE(pr.counterparty,''))=lower(?)`
		args = append(args, cp)
	}
	switch opts.Ageing {
	case "overdue":
		q += ` AND COALESCE(pr.expected_return_date,'') <> '' AND pr.expected_return_date < ?`
		args = append(args, today)
	case "due30":
		q += ` AND COALESCE(pr.expected_return_date,'') <> '' AND pr.expected_return_date >= ?
			AND julianday(pr.expected_return_date) - julianday(?) <= 30`
		args = append(args, today, today)
	case "later":
		q += ` AND (COALESCE(pr.expected_return_date,'') = '' OR julianday(pr.expected_return_date) - julianday(?) > 30)`
		args = append(args, today)
	case "unpaid":
		q += ` AND py.id IS NULL`
	}
	if search := strings.ToLower(strings.TrimSpace(opts.Query)); search != "" {
		q += ` AND (lower(pr.number) LIKE ? ESCAPE '\' OR lower(COALESCE(pr.counterparty,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(p.name,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(u.name,'')) LIKE ? ESCAPE '\')`
		search = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
		needle := "%" + search + "%"
		args = append(args, needle, needle, needle, needle)
	}
	switch opts.Order {
	case "amount":
		q += ` ORDER BY amt DESC, pr.number`
	case "paid_on":
		q += ` ORDER BY COALESCE(py.paid_on,'') DESC, pr.number`
	case "number":
		q += ` ORDER BY pr.number`
	default:
		// Overdue first, then the soonest expected return; undated rows sort last.
		q += ` ORDER BY CASE WHEN COALESCE(pr.expected_return_date,'') <> '' AND pr.expected_return_date < ? THEN 0 ELSE 1 END,
			COALESCE(NULLIF(pr.expected_return_date,''),'9999-12-31'), pr.number`
		args = append(args, today)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecoverableRow
	for rows.Next() {
		var r RecoverableRow
		var onHold int
		if err := rows.Scan(&r.RequestID, &r.Number, &r.Category, &r.Counterparty, &r.Project, &r.Amount,
			&r.PaidOn, &r.ExpectedReturnDate, &r.RepaymentNotes, &r.Status, &r.Requester, &onHold, &r.DaysToReturn); err != nil {
			return nil, err
		}
		r.OnHold = onHold == 1
		r.HasReturnDate = r.ExpectedReturnDate != ""
		if !r.HasReturnDate {
			// The days expression substitutes today for an undated row so
			// julianday never scans NULL into an int; the label says
			// "No fixed date" rather than "Due today".
			r.DaysToReturn = 0
		}
		r.AgeingLabel, r.AgeingTone, r.Overdue = recoverableAgeing(r.PaidOn, r.ExpectedReturnDate, r.OnHold, r.DaysToReturn)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RecoverableMetrics(ctx context.Context, asOf time.Time, viewer RecoverableViewer) (RecoverableMetrics, error) {
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	today := asOf.UTC().Format("2006-01-02")
	month := asOf.UTC().Format("2006-01")
	var m RecoverableMetrics
	// F-E-05: the overdue pair carries `paid <> ''` because recoverableAgeing
	// applies exactly that guard per row — it answers "Awaiting payment", not
	// overdue, before it ever compares dates. Money that never left cannot be
	// overdue. Without it the dashboard tile counted unpaid, approved requests as
	// overdue and then sent the reader to a register that showed fewer red rows
	// than the summary promised.
	//
	// Deliberately not applied to the due-in-30 pair: that tile is the calendar of
	// expected returns coming up, and the register's own `due30` filter does not
	// filter on payment either, so the two still describe the same population.
	// The scope rides inside the subquery, so its placeholders bind after the outer
	// SELECT's — which is why scopeArgs is appended last (F-G-016: the tiles and the
	// counterparty rollup summarise the same rows the register lists, so if the
	// register is scoped and these are not, the totals disclose what the list hides).
	scopeClause, scopeArgs := recoverableScope(viewer)
	args := append([]any{today, today, today, today, today, today, month, month}, scopeArgs...)
	err := s.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(amt),0), COUNT(*),
		COALESCE(SUM(CASE WHEN paid <> '' AND exp <> '' AND exp < ? THEN amt ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN paid <> '' AND exp <> '' AND exp < ? THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN exp <> '' AND exp >= ? AND julianday(exp)-julianday(?) <= 30 THEN amt ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN exp <> '' AND exp >= ? AND julianday(exp)-julianday(?) <= 30 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN substr(paid,1,7)=? THEN amt ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN substr(paid,1,7)=? THEN 1 ELSE 0 END),0)
		FROM (SELECT `+recoverableAmount+` AS amt,
			COALESCE(pr.expected_return_date,'') AS exp, COALESCE(py.paid_on,'') AS paid
			FROM payment_requests pr
			LEFT JOIN payments py ON py.request_id=pr.id AND py.voided_at IS NULL
			WHERE `+recoverableBaseWhere+scopeClause+`)`, args...).
		Scan(&m.OutstandingAmount, &m.OutstandingCount, &m.OverdueAmount, &m.OverdueCount,
			&m.DueIn30Amount, &m.DueIn30Count, &m.PaidThisMonthAmount, &m.PaidThisMonthCount)
	if err != nil {
		return RecoverableMetrics{}, err
	}
	return m, nil
}

func (s *Store) RecoverableRollups(ctx context.Context, by string, asOf time.Time, viewer RecoverableViewer) ([]RecoverableRollup, error) {
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	today := asOf.UTC().Format("2006-01-02")
	var label, detail string
	switch by {
	case "category":
		label = `COALESCE(NULLIF(rc.name,''),'Uncategorised')`
		detail = `''`
	case "counterparty":
		label = `COALESCE(NULLIF(pr.counterparty,''),'Not recorded')`
		detail = `COALESCE(GROUP_CONCAT(DISTINCT rc.name),'')`
	default:
		return nil, fmt.Errorf("%w: unknown recoverable rollup dimension %q", ErrValidation, by)
	}
	scopeClause, scopeArgs := recoverableScope(viewer)
	args := append([]any{today}, scopeArgs...)
	rows, err := s.db.QueryContext(ctx, `SELECT `+label+` AS grp, `+detail+`,
		COUNT(*), COALESCE(SUM(`+recoverableAmount+`),0),
		COALESCE(SUM(CASE WHEN COALESCE(pr.expected_return_date,'') <> '' AND pr.expected_return_date < ?
			THEN `+recoverableAmount+` ELSE 0 END),0),
		COALESCE(MIN(NULLIF(COALESCE(py.paid_on,''),'')),''),
		COALESCE(MIN(NULLIF(COALESCE(pr.expected_return_date,''),'')),'')
		FROM payment_requests pr
		LEFT JOIN payments py ON py.request_id=pr.id AND py.voided_at IS NULL
		LEFT JOIN recoverable_categories rc ON rc.id=pr.recoverable_category_id
		WHERE `+recoverableBaseWhere+scopeClause+`
		GROUP BY grp
		ORDER BY 4 DESC, grp`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecoverableRollup
	for rows.Next() {
		var r RecoverableRollup
		if err := rows.Scan(&r.Label, &r.Detail, &r.Count, &r.Outstanding, &r.Overdue, &r.Oldest, &r.ExpectedBack); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
