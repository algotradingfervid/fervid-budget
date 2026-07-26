package store

import (
	"context"
	"errors"
	"fmt"
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
	ctx := context.Background()
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

func TestRecoverableCategoryCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)

	id, err := s.UpsertRecoverableCategory(ctx, actor, 0, "Retention money", true, false, true, 7)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == 0 {
		t.Fatal("expected a new category id")
	}
	cats, err := s.ListRecoverableCategories(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var found *RecoverableCategory
	for i := range cats {
		if cats[i].ID == id {
			found = &cats[i]
		}
	}
	if found == nil || !found.RequiresProject || found.RequiresCounterparty || !found.Active {
		t.Fatalf("stored category = %+v", found)
	}
	// An admin-added category still needs a code: it is the identity the request
	// validator resolves rules by, so a category with no code could never be used.
	if found.Code != "retention_money" {
		t.Fatalf("derived code = %q, want retention_money", found.Code)
	}
	if _, err := s.UpsertRecoverableCategory(ctx, actor, 0, "retention money", false, false, true, 8); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate name = %v, want %v", err, ErrDuplicate)
	}
	if _, err := s.UpsertRecoverableCategory(ctx, actor, id, "Retention money", true, false, false, 7); err != nil {
		t.Fatalf("update: %v", err)
	}
	active, err := s.ListRecoverableCategories(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range active {
		if c.ID == id {
			t.Fatal("deactivated category still listed as active")
		}
	}
	if _, err := s.UpsertRecoverableCategory(ctx, actor, 0, "  ", false, false, true, 9); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank name = %v, want %v", err, ErrValidation)
	}
	// Renaming must not change the code: existing requests point at it.
	if _, err := s.UpsertRecoverableCategory(ctx, actor, id, "Retention deposit", true, false, true, 7); err != nil {
		t.Fatalf("rename: %v", err)
	}
	after, err := s.ListRecoverableCategories(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range after {
		if c.ID == id && c.Code != "retention_money" {
			t.Fatalf("rename changed the code to %q; existing requests would be orphaned", c.Code)
		}
	}
}

func TestRecoverableCategoryRequiresLabel(t *testing.T) {
	cases := []struct {
		name string
		cat  RecoverableCategory
		want string
	}{
		{"neither", RecoverableCategory{Name: "Other"}, "Nothing extra"},
		{"project", RecoverableCategory{Name: "EMD", RequiresProject: true}, "Related project"},
		{"counterparty", RecoverableCategory{Name: "ICD", RequiresCounterparty: true}, "Counterparty company"},
		{"both", RecoverableCategory{Name: "Retention", RequiresProject: true, RequiresCounterparty: true}, "Related project and counterparty company"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cat.Requires(); got != c.want {
				t.Fatalf("Requires() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestListRecoverableCategoriesWithUsageCountsRequests(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	var emdID, icdID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM recoverable_categories WHERE code='emd'`).Scan(&emdID); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM recoverable_categories WHERE code='icd'`).Scan(&icdID); err != nil {
		t.Fatal(err)
	}
	for i, cat := range []struct {
		id   int64
		code string
	}{{emdID, "emd"}, {emdID, "emd"}, {icdID, "icd"}} {
		if _, err := s.DB().ExecContext(ctx, `INSERT INTO payment_requests
			(number,status,treatment,type,recoverable_category,recoverable_category_id,head_id,amount,purpose,counterparty,expected_return_date,repayment_notes,requester_id,manager_id,submitted_at)
			VALUES(?, 'pending','recoverable','recoverable',?,?,?,?, 'Deposit','Counterparty','2027-03-31','Refund later',?,?,CURRENT_TIMESTAMP)`,
			fmt.Sprintf("PR-2026-%06d", 300+i), cat.code, cat.id, headID, int64(100000), actor.ID, actor.ID); err != nil {
			t.Fatal(err)
		}
	}
	usage, err := s.ListRecoverableCategoriesWithUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 6 {
		t.Fatalf("categories = %d, want the 6 seeded", len(usage))
	}
	counts := map[string]int{}
	for _, u := range usage {
		counts[u.Name] = u.InUse
	}
	if counts["EMD"] != 2 {
		t.Fatalf("EMD in use = %d, want 2", counts["EMD"])
	}
	if counts["ICD"] != 1 {
		t.Fatalf("ICD in use = %d, want 1", counts["ICD"])
	}
	if counts["Other"] != 0 {
		t.Fatalf("Other in use = %d, want 0", counts["Other"])
	}
	if usage[0].Name != "Employee advance" {
		t.Fatalf("first row = %q, want the lowest sort_order (Employee advance)", usage[0].Name)
	}
	if usage[1].Requires() != "Related project" {
		t.Fatalf("EMD Requires() = %q, want Related project", usage[1].Requires())
	}
}
