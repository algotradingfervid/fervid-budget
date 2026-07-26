package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
