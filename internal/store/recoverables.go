package store

import (
	"database/sql"
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
