package store

import (
	"path/filepath"
	"testing"
)

// The seeded categories must reproduce the Phase-2 recoverableCategoryRules map
// exactly. Phase 2 shipped those rules hardcoded and its tests pin them; moving
// the rules onto table rows is only safe if the rows say the same thing. Note
// security_deposit requires a counterparty — the Phase-4 plan text says it
// requires nothing, which would have silently weakened shipped validation.
func TestMigrationV6SeedsRecoverableCategories(t *testing.T) {
	s := newTestStore(t)
	var version int
	if err := s.DB().QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version < 6 {
		t.Fatalf("user_version = %d, want >= 6", version)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM recoverable_categories`).Scan(&count); err != nil {
		t.Fatalf("recoverable_categories table missing: %v", err)
	}
	if count != 6 {
		t.Fatalf("seeded categories = %d, want 6", count)
	}
	checks := []struct {
		code, name string
		proj, cp   bool
	}{
		{"employee_advance", "Employee advance", false, false},
		{"emd", "EMD", true, false},
		{"pbg", "PBG", true, false},
		{"icd", "ICD", false, true},
		{"security_deposit", "Security deposit", false, true},
		{"other", "Other", false, false},
	}
	for _, c := range checks {
		var name string
		var proj, cp int
		err := s.DB().QueryRow(`SELECT name,requires_project,requires_counterparty FROM recoverable_categories WHERE code=?`, c.code).
			Scan(&name, &proj, &cp)
		if err != nil {
			t.Fatalf("category %q missing: %v", c.code, err)
		}
		if name != c.name {
			t.Fatalf("%s name = %q, want %q", c.code, name, c.name)
		}
		if (proj == 1) != c.proj || (cp == 1) != c.cp {
			t.Fatalf("%s flags = proj:%d cp:%d, want proj:%v cp:%v", c.code, proj, cp, c.proj, c.cp)
		}
	}
}

// The seeded rows are the authority the request validator reads. If this ever
// disagrees with recoverableCategoryRules, Phase 2's shipped behaviour changed.
func TestSeededCategoriesMatchPhase2Rules(t *testing.T) {
	s := newTestStore(t)
	rows, err := s.DB().Query(`SELECT code,requires_project,requires_counterparty FROM recoverable_categories`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var code string
		var proj, cp int
		if err := rows.Scan(&code, &proj, &cp); err != nil {
			t.Fatal(err)
		}
		want, ok := recoverableCategoryRules[code]
		if !ok {
			t.Fatalf("seeded category %q is not a Phase-2 code", code)
		}
		if (proj == 1) != want.RequiresProject || (cp == 1) != want.RequiresCounterparty {
			t.Fatalf("%s = proj:%v cp:%v, want proj:%v cp:%v",
				code, proj == 1, cp == 1, want.RequiresProject, want.RequiresCounterparty)
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != len(recoverableCategoryRules) {
		t.Fatalf("seeded %d categories, but Phase 2 declares %d rules", seen, len(recoverableCategoryRules))
	}
}

func TestMigrationV6IsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotent.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := Open(path) // re-open an already-migrated database
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	var count int
	if err := s2.DB().QueryRow(`SELECT COUNT(*) FROM recoverable_categories`).Scan(&count); err != nil {
		t.Fatalf("count after re-open: %v", err)
	}
	if count != 6 {
		t.Fatalf("after re-open categories = %d, want 6 (no duplicates)", count)
	}
}

// Phase 2 wrote payment_requests.recoverable_category (a code) and hardcoded
// recoverable_category_id to NULL. Every register query in Phase 4 joins on the
// id, so an installed database full of Phase-2 requests must be back-filled or
// the whole register renders a blank category for real rows.
func TestBackfillLinksExistingRequestsToCategories(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)

	res, err := s.DB().ExecContext(ctx, `INSERT INTO payment_requests
		(number,status,treatment,type,recoverable_category,recoverable_category_id,head_id,amount,purpose,
		 counterparty,expected_return_date,repayment_notes,requester_id,manager_id,submitted_at)
		VALUES('PR-2026-000900','pending','recoverable','recoverable','emd',NULL,?,?, 'Tender EMD',
		 'Ridge Metro','2027-03-31','Refund on award',?,?,CURRENT_TIMESTAMP)`,
		headID, int64(100000), actor.ID, actor.ID)
	if err != nil {
		t.Fatalf("seed legacy request: %v", err)
	}
	reqID, _ := res.LastInsertId()

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := backfillRecoverableCategoryIDs(tx); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var gotName string
	if err := s.DB().QueryRowContext(ctx, `SELECT rc.name FROM payment_requests pr
		JOIN recoverable_categories rc ON rc.id=pr.recoverable_category_id WHERE pr.id=?`, reqID).Scan(&gotName); err != nil {
		t.Fatalf("request was not linked to its category: %v", err)
	}
	if gotName != "EMD" {
		t.Fatalf("linked category = %q, want EMD", gotName)
	}
}
