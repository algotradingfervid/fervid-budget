// recoverables.go covers money that is expected to come back: deposits,
// guarantees and advances raised with the "recoverable" treatment. It holds the
// admin-managed recoverable categories (with their migration and seed) and the
// recoverables register: the list, the dashboard tiles and the grouped totals.
// Every register query applies the viewer's request scope in SQL.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// recoverableCategorySeed is the six default categories. code is the stable
// identity that payment_requests.recoverable_category stores; name is the
// label an admin may rename. The flags match recoverableCategoryRules exactly,
// and TestSeededCategoriesMatchPhase2Rules fails if the two ever drift.
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
// recoverable_categories table, seeds the six defaults and links existing
// requests to them. It is idempotent: re-running it on a migrated database
// inserts and re-links nothing.
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

// backfillRecoverableCategoryIDs sets recoverable_category_id on requests
// written before the categories table existed, matching on the stored category
// code. The register joins on the id, so without this older requests would show
// a blank category. It only fills NULLs, so it never overwrites a later change.
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
// table so a category an admin adds is usable at once. Only active categories
// are included, so deactivating one stops new requests from naming it.
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
// is not recoverable. The register queries join on that id.
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

// recoverableCategorySelect reads recoverable_categories rows for
// scanRecoverableCategory.
const recoverableCategorySelect = `SELECT id,code,name,requires_project,requires_counterparty,active,sort_order,created_at
	FROM recoverable_categories`

// scanRecoverableCategory reads one recoverableCategorySelect row.
func scanRecoverableCategory(sc interface{ Scan(...any) error }) (RecoverableCategory, error) {
	var c RecoverableCategory
	var rp, rc, active int
	if err := sc.Scan(&c.ID, &c.Code, &c.Name, &rp, &rc, &active, &c.SortOrder, &c.CreatedAt); err != nil {
		return c, err
	}
	c.RequiresProject, c.RequiresCounterparty, c.Active = rp == 1, rc == 1, active == 1
	return c, nil
}

// ListRecoverableCategories lists the categories in display order, only the
// active ones when activeOnly is set.
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

// UpsertRecoverableCategory creates a category (id 0) or updates one, and
// returns its id. A new category's code is derived from its name; an existing
// category keeps its code, because requests resolve their rules by it.
func (s *Store) UpsertRecoverableCategory(ctx context.Context, actor User, id int64, name string, requiresProject, requiresCounterparty, active bool, sortOrder int) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("%w: recoverable category name is required", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
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

// DeleteRecoverableCategory removes a category that no request uses yet, such
// as one added by mistake. A category in use is refused with ErrForbidden and
// the number of requests using it; deactivating it is the way to retire it. The
// check runs in the same transaction as the delete, because the database has no
// foreign key that would stop a delete from orphaning requests. The count is
// the same one ListRecoverableCategoriesWithUsage shows as InUse.
func (s *Store) DeleteRecoverableCategory(ctx context.Context, actor User, id int64) error {
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

// pluralRequests is "request" or "requests" for n.
func pluralRequests(n int) string {
	if n == 1 {
		return "request"
	}
	return "requests"
}

// ListRecoverableCategoriesWithUsage lists every category with the number of
// requests that use it, for the configuration screen.
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
const recoverableBaseWhere = `pr.treatment='recoverable' AND (pr.status NOT IN ('rejected','cancelled','withdrawn') OR EXISTS(SELECT 1 FROM payments live_pay WHERE live_pay.request_id=pr.id AND live_pay.voided_at IS NULL))`

// recoverableAmount is a recoverable's outstanding balance: what was paid out
// (voided payments excluded) less what came back (recovery events). It uses the
// py and re aliases that recoverableJoins adds.
const recoverableAmount = `COALESCE(py.amount,0)-COALESCE(re.amount,0)`

// recoverableJoins adds, per request, the paid total, first payment id and
// date (py) and the recovered total (re). Payments are summed before the join,
// so a request paid in installments still appears once.
const recoverableJoins = ` LEFT JOIN (SELECT request_id,MIN(id) AS id,SUM(amount) AS amount,MIN(paid_on) AS paid_on FROM payments WHERE voided_at IS NULL GROUP BY request_id) py ON py.request_id=pr.id
 LEFT JOIN (SELECT request_id,SUM(amount) AS amount FROM recovery_events GROUP BY request_id) re ON re.request_id=pr.id `

// recoverableCounterparty is who owes the money back: the employee for an
// employee advance, otherwise the counterparty, else the payee.
const recoverableCounterparty = `CASE WHEN pr.type='employee_advance' AND pr.recoverable_category='employee_advance' THEN COALESCE(NULLIF(u.name,''),'Employee') ELSE COALESCE(NULLIF(pr.counterparty,''),NULLIF(pr.vendor_payee,''),'Not recorded') END`

// recoverableScope is the row-visibility half of recoverableBaseWhere: the same
// request data scope the detail page applies, expressed in SQL for the `pr`
// alias so every recoverable read enforces it in the query rather than after
// the fact. The register is a second view over payment_requests, and a row it
// shows whose detail page answers 404, or one it hides whose detail page opens,
// would be a disagreement about who may see what. An unrecognised or empty
// scope reads no rows. See RecoverableViewer.
func recoverableScope(v RecoverableViewer) (string, []any) {
	predicate, args := requestScopeSQL("pr", v.Scope, v.ViewerID)
	return ` AND ` + predicate, args
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

// RecoverableReport lists the recoverables the viewer may see, with what was
// paid out, what has come back, what is outstanding and how the expected return
// date compares to AsOf (today when zero). Filters combine with AND: a payment
// month range (From/To), category, counterparty, an ageing bucket and a
// free-text search. Order picks the sort; the default puts overdue rows first,
// then the soonest expected return.
func (s *Store) RecoverableReport(ctx context.Context, opts RecoverableReportOptions) ([]RecoverableRow, error) {
	asOf := opts.AsOf
	if asOf.IsZero() {
		asOf = time.Now()
	}
	today := asOf.Format("2006-01-02")

	q := `SELECT pr.id, pr.number, COALESCE(rc.name,''), ` + recoverableCounterparty + `, COALESCE(p.name,''),
		` + recoverableAmount + ` AS amt, COALESCE(py.amount,0), COALESCE(re.amount,0),
		COALESCE(py.paid_on,''), COALESCE(pr.expected_return_date,''), COALESCE(pr.repayment_notes,''),
		pr.status, COALESCE(u.name,''), COALESCE(pr.on_hold,0),
		pr.processing_by, COALESCE(pu.name,''), COALESCE(pr.concern_open,0),
		CAST(julianday(COALESCE(NULLIF(pr.expected_return_date,''),?)) - julianday(?) AS INTEGER) AS days
		FROM payment_requests pr
		` + recoverableJoins + `
		LEFT JOIN recoverable_categories rc ON rc.id=pr.recoverable_category_id
		LEFT JOIN projects p ON p.id=pr.project_id
		LEFT JOIN users u ON u.id=pr.requester_id
		LEFT JOIN users pu ON pu.id=pr.processing_by
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
		q += ` AND EXISTS(SELECT 1 FROM payments fp WHERE fp.request_id=pr.id AND fp.voided_at IS NULL AND substr(fp.paid_on,1,7) BETWEEN ? AND ?)`
		args = append(args, from, to)
	}
	if opts.CategoryID > 0 {
		q += ` AND pr.recoverable_category_id=?`
		args = append(args, opts.CategoryID)
	}
	if cp := strings.TrimSpace(opts.Counterparty); cp != "" {
		q += ` AND lower(` + recoverableCounterparty + `)=lower(?)`
		args = append(args, cp)
	}
	switch opts.Ageing {
	case "outstanding", "overdue", "due30", "later":
		q += ` AND (` + recoverableAmount + `)>0`
	}
	switch opts.Ageing {
	case "overdue":
		// Paid rows only, as recoverableAgeing and RecoverableMetrics decide it:
		// the tile links here, so the filter must list what the tile counts.
		q += ` AND COALESCE(py.paid_on,'') <> '' AND COALESCE(pr.expected_return_date,'') <> '' AND pr.expected_return_date < ?`
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
	case "recovered":
		q += ` AND py.id IS NOT NULL AND (` + recoverableAmount + `)=0`
	}
	if search := escapeLike(opts.Query); search != "" {
		q += ` AND (lower(pr.number) LIKE ? ESCAPE '\' OR lower(COALESCE(pr.counterparty,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(p.name,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(u.name,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(pr.vendor_payee,'')) LIKE ? ESCAPE '\')`
		needle := "%" + search + "%"
		args = append(args, needle, needle, needle, needle, needle)
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
		var onHold, concernOpen int
		var processingBy sql.NullInt64
		if err := rows.Scan(&r.RequestID, &r.Number, &r.Category, &r.Counterparty, &r.Project, &r.Amount, &r.PaidAmount, &r.RecoveredAmount,
			&r.PaidOn, &r.ExpectedReturnDate, &r.RepaymentNotes, &r.Status, &r.Requester, &onHold,
			&processingBy, &r.ProcessingByName, &concernOpen, &r.DaysToReturn); err != nil {
			return nil, err
		}
		r.OnHold = onHold == 1
		r.ConcernOpen = concernOpen == 1
		if processingBy.Valid {
			v := processingBy.Int64
			r.ProcessingBy = &v
		}
		r.HasReturnDate = r.ExpectedReturnDate != ""
		if !r.HasReturnDate {
			// The days expression substitutes today for an undated row so
			// julianday never scans NULL into an int; the label says
			// "No fixed date" rather than "Due today".
			r.DaysToReturn = 0
		}
		r.AgeingLabel, r.AgeingTone, r.Overdue = recoverableAgeing(r.PaidOn, r.ExpectedReturnDate, r.OnHold, r.DaysToReturn)
		if r.PaidAmount > 0 && r.Amount == 0 {
			r.AgeingLabel, r.AgeingTone, r.Overdue = "Reconciled", "good", false
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecoverableMetrics totals the register for the dashboard tiles: the
// outstanding balance, the part overdue, the part due within 30 days of asOf,
// and what was paid out on recoverables in asOf's month. It sums the same rows
// RecoverableReport lists, so the tiles and the list always agree.
func (s *Store) RecoverableMetrics(ctx context.Context, asOf time.Time, viewer RecoverableViewer) (RecoverableMetrics, error) {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	rows, err := s.RecoverableReport(ctx, RecoverableReportOptions{AsOf: asOf, Viewer: viewer})
	if err != nil {
		return RecoverableMetrics{}, err
	}
	var m RecoverableMetrics
	for _, r := range rows {
		if r.Amount <= 0 {
			continue
		}
		m.OutstandingAmount += r.Amount
		m.OutstandingCount++
		// Calendar ageing stays independent of an operational payment hold.
		if r.HasReturnDate && r.DaysToReturn < 0 {
			m.OverdueAmount += r.Amount
			m.OverdueCount++
		}
		if r.HasReturnDate && r.DaysToReturn >= 0 && r.DaysToReturn <= 30 {
			m.DueIn30Amount += r.Amount
			m.DueIn30Count++
		}
	}
	scope, args := recoverableScope(viewer)
	args = append([]any{asOf.Format("2006-01")}, args...)
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(py.amount),0),COUNT(*) FROM payments py JOIN payment_requests pr ON pr.id=py.request_id WHERE py.voided_at IS NULL AND substr(py.paid_on,1,7)=? AND `+recoverableBaseWhere+scope, args...).Scan(&m.PaidThisMonthAmount, &m.PaidThisMonthCount)
	return m, err
}

// RecoverableRollups groups the outstanding recoverables by "category" or
// "counterparty", with each group's count, outstanding and overdue amounts, and
// oldest payment and expected-return dates, largest balance first.
func (s *Store) RecoverableRollups(ctx context.Context, by string, asOf time.Time, viewer RecoverableViewer) ([]RecoverableRollup, error) {
	if asOf.IsZero() {
		asOf = time.Now()
	}
	today := asOf.Format("2006-01-02")
	var label, detail string
	switch by {
	case "category":
		label = `COALESCE(NULLIF(rc.name,''),'Uncategorised')`
		detail = `''`
	case "counterparty":
		label = recoverableCounterparty
		detail = `COALESCE(GROUP_CONCAT(DISTINCT rc.name),'')`
	default:
		return nil, fmt.Errorf("%w: unknown recoverable rollup dimension %q", ErrValidation, by)
	}
	// The overdue column counts only paid rows, like the overdue tile: these rows
	// sit above a Total row read from RecoverableMetrics and must add up to it.
	scopeClause, scopeArgs := recoverableScope(viewer)
	args := append([]any{today}, scopeArgs...)
	rows, err := s.db.QueryContext(ctx, `SELECT `+label+` AS grp, `+detail+`,
		COUNT(*), COALESCE(SUM(`+recoverableAmount+`),0),
		COALESCE(SUM(CASE WHEN COALESCE(py.paid_on,'') <> '' AND COALESCE(pr.expected_return_date,'') <> '' AND pr.expected_return_date < ?
			THEN `+recoverableAmount+` ELSE 0 END),0),
		COALESCE(MIN(NULLIF(COALESCE(py.paid_on,''),'')),''),
		COALESCE(MIN(NULLIF(COALESCE(pr.expected_return_date,''),'')),'')
		FROM payment_requests pr
		`+recoverableJoins+`
		LEFT JOIN recoverable_categories rc ON rc.id=pr.recoverable_category_id
        LEFT JOIN users u ON u.id=pr.requester_id
		WHERE `+recoverableBaseWhere+scopeClause+` AND (`+recoverableAmount+`)>0
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
