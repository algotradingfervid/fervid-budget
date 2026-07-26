package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// The register joins payment_requests to recoverable_categories on the id, and
// Phase 2 hardcoded that id to NULL. If CreateRequest does not fill it, every
// screen in Phase 4 shows a blank category for every request the real form ever
// produced — while tests that INSERT the id by hand still pass.
func TestCreateRequestLinksRecoverableCategoryID(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, _ := seedRequestActors(t, s, ctx)

	id, err := s.CreateRequest(ctx, req, RequestInput{
		Treatment: "recoverable", Type: "recoverable", ShortTitle: "Ridge Metro EMD",
		RecoverableCategory: "emd", ProjectID: 1, Amount: 500000, Purpose: "Tender EMD",
		ExpectedReturnDate: "2027-03-31", RepaymentNotes: "Refund on award", ManagerID: mgr.ID,
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	var gotID *int64
	var gotCode string
	if err := s.DB().QueryRowContext(ctx, `SELECT recoverable_category_id,recoverable_category FROM payment_requests WHERE id=?`, id).
		Scan(&gotID, &gotCode); err != nil {
		t.Fatal(err)
	}
	if gotCode != "emd" {
		t.Fatalf("recoverable_category = %q, want emd", gotCode)
	}
	if gotID == nil {
		t.Fatal("recoverable_category_id is NULL: the register would show a blank category for this request")
	}
	var name string
	if err := s.DB().QueryRowContext(ctx, `SELECT name FROM recoverable_categories WHERE id=?`, *gotID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "EMD" {
		t.Fatalf("linked category = %q, want EMD", name)
	}
}

// A budget request has no category, so the link column must stay NULL rather
// than pointing at whatever a stale code happened to say.
func TestBudgetRequestHasNoCategoryLink(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)

	id, err := s.CreateRequest(ctx, req, RequestInput{
		Treatment: "budget", Type: "reimbursement", ShortTitle: "Team lunch",
		ProjectID: 1, HeadID: headID, Amount: 50000, Purpose: "team lunch",
		ExpenseDate: "2026-07-21", ManagerID: mgr.ID,
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	var gotID *int64
	if err := s.DB().QueryRowContext(ctx, `SELECT recoverable_category_id FROM payment_requests WHERE id=?`, id).Scan(&gotID); err != nil {
		t.Fatal(err)
	}
	if gotID != nil {
		t.Fatalf("budget request linked to category %d", *gotID)
	}
}

// V4: categories are admin-configurable, so the rules must come from the table.
// With the Phase-2 map still in charge, a category an admin adds is simply an
// unknown code and every request naming it is rejected.
func TestAdminAddedCategoryRulesAreEnforced(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, _ := seedRequestActors(t, s, ctx)
	admin, _ := s.UserByID(ctx, mgr.ID)
	if _, err := s.UpsertRecoverableCategory(ctx, admin, 0, "Retention money", false, true, true, 7); err != nil {
		t.Fatalf("add category: %v", err)
	}

	base := func() RequestInput {
		return RequestInput{
			Treatment: "recoverable", Type: "recoverable", ShortTitle: "Retention",
			RecoverableCategory: "retention_money", Amount: 250000, Purpose: "Retention held",
			ExpectedReturnDate: "2027-06-30", RepaymentNotes: "Release at defect liability end",
			ManagerID: mgr.ID,
		}
	}
	// The new category requires a counterparty, and that rule is enforced.
	if _, err := s.CreateRequest(ctx, req, base()); !errors.Is(err, ErrValidation) {
		t.Fatalf("admin category rule not enforced: %v", err)
	}
	in := base()
	in.Counterparty = "Ridge Metro"
	id, err := s.CreateRequest(ctx, req, in)
	if err != nil {
		t.Fatalf("admin-added category rejected: %v", err)
	}
	var linked *int64
	if err := s.DB().QueryRowContext(ctx, `SELECT recoverable_category_id FROM payment_requests WHERE id=?`, id).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked == nil {
		t.Fatal("admin-added category did not link")
	}
}

// Deactivating a category is how an admin retires it. It must stop being usable
// on new requests, otherwise the setting does nothing.
func TestDeactivatedCategoryIsRejectedOnNewRequests(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, _ := seedRequestActors(t, s, ctx)
	admin, _ := s.UserByID(ctx, mgr.ID)
	var pbgID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM recoverable_categories WHERE code='pbg'`).Scan(&pbgID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertRecoverableCategory(ctx, admin, pbgID, "PBG", true, false, false, 3); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	_, err := s.CreateRequest(ctx, req, RequestInput{
		Treatment: "recoverable", Type: "recoverable", ShortTitle: "PBG",
		RecoverableCategory: "pbg", ProjectID: 1, Amount: 100000, Purpose: "Bank guarantee",
		ExpectedReturnDate: "2027-03-31", RepaymentNotes: "On completion", ManagerID: mgr.ID,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("deactivated category accepted: %v", err)
	}
}

// seedRecoverablePayment inserts a completed recoverable request and its single
// linked (settled, non-voided) payment, exercising the Grid/Report and
// RecoverableReport joins. It sets both the category code and the category id,
// the shape CreateRequest actually writes. projectID of 0 leaves the
// recoverable unlinked from any project.
func seedRecoverablePayment(t *testing.T, s *Store, ctx context.Context, actor User, headID, projectID int64, number, paidOn string, amount int64) (int64, int64) {
	t.Helper()
	reqID := seedRecoverableRequestOnly(t, s, ctx, actor, headID, projectID, number, "completed", "security_deposit", "Acme Landlord", "2027-12-31", amount, false)
	payID := payRecoverable(t, s, ctx, actor, headID, reqID, paidOn, amount)
	return reqID, payID
}

// seedRecoverableRequestOnly inserts a recoverable request with no linked
// payment, so the register can be tested for rows approved but not yet paid.
func seedRecoverableRequestOnly(t *testing.T, s *Store, ctx context.Context, actor User, headID, projectID int64, number, status, code, counterparty, expectedReturn string, amount int64, onHold bool) int64 {
	t.Helper()
	var catID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM recoverable_categories WHERE code=?`, code).Scan(&catID); err != nil {
		t.Fatalf("lookup category %q: %v", code, err)
	}
	var proj, expReturn any
	if projectID != 0 {
		proj = projectID
	}
	if expectedReturn != "" {
		expReturn = expectedReturn
	}
	res, err := s.DB().ExecContext(ctx, `INSERT INTO payment_requests
		(number,status,treatment,type,recoverable_category,recoverable_category_id,project_id,head_id,amount,approved_amount,
		 purpose,counterparty,expected_return_date,repayment_notes,requester_id,manager_id,on_hold,submitted_at)
		VALUES(?,?, 'recoverable','recoverable',?,?,?,?,?,?, 'Deposit',?,?, 'Refund on close',?,?,?,CURRENT_TIMESTAMP)`,
		number, status, code, catID, proj, headID, amount, amount, counterparty, expReturn, actor.ID, actor.ID, boolInt(onHold))
	if err != nil {
		t.Fatalf("insert recoverable request %s: %v", number, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// payRecoverable links a settled, non-voided payment to a recoverable request.
func payRecoverable(t *testing.T, s *Store, ctx context.Context, actor User, headID, requestID int64, paidOn string, amount int64) int64 {
	t.Helper()
	res, err := s.DB().ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,entered_by,request_id,settlement) VALUES(?,?,?,?,?,?, 'settled')`,
		headID, paidOn, amount, "Acme Landlord", actor.ID, requestID)
	if err != nil {
		t.Fatalf("insert payment for request %d: %v", requestID, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestGridAndReportExcludeRecoverablePayments(t *testing.T) { // V2
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	if err := s.SetBudget(ctx, actor, headID, "2026-08", 1000000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-08-05", Amount: 300000, VendorPayee: "Budget Vendor"}); err != nil {
		t.Fatal(err)
	}
	seedRecoverablePayment(t, s, ctx, actor, headID, 0, "PR-2026-000001", "2026-08-06", 700000)

	grid, err := s.Grid(ctx, "2026-08", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if grid.Total.Actual != 300000 {
		t.Fatalf("grid actual = %d, want 300000 (recoverable excluded)", grid.Total.Actual)
	}
	rows, err := s.Report(ctx, "2026-08", "2026-08", "heads")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Actual != 300000 {
		t.Fatalf("report rows = %+v, want single row with actual 300000", rows)
	}
}

// A payment with no linked request is historical data from before the request
// module existed. It is a real budget expense and must keep counting.
func TestHistoricalPaymentsStillCountAsActuals(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-08-05", Amount: 450000, VendorPayee: "Legacy Vendor"}); err != nil {
		t.Fatal(err)
	}
	grid, err := s.Grid(ctx, "2026-08", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if grid.Total.Actual != 450000 {
		t.Fatalf("grid actual = %d, want 450000 (request_id IS NULL still counts)", grid.Total.Actual)
	}
}

func TestRecoverablePaymentExcludedFromActualsButInRecoverableReport(t *testing.T) { // V3, V6
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	if err := s.SetBudget(ctx, actor, headID, "2026-09", 1000000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-09-03", Amount: 250000, VendorPayee: "Budget Vendor"}); err != nil {
		t.Fatal(err)
	}
	seedRecoverablePayment(t, s, ctx, actor, headID, 0, "PR-2026-000009", "2026-09-04", 800000)

	grid, err := s.Grid(ctx, "2026-09", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if grid.Total.Actual != 250000 {
		t.Fatalf("grid actual = %d, want 250000 (recoverable excluded)", grid.Total.Actual)
	}
	rec, err := s.RecoverableReport(ctx, RecoverableReportOptions{From: "2026-09", To: "2026-09"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec) != 1 {
		t.Fatalf("recoverable report rows = %d, want 1", len(rec))
	}
	if rec[0].Amount != 800000 || rec[0].Category != "Security deposit" || rec[0].Counterparty != "Acme Landlord" || rec[0].Status != "completed" {
		t.Fatalf("recoverable row = %+v", rec[0])
	}
}

func TestRecoverableReportShowsLinkedProject(t *testing.T) { // V8
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	var projectID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	seedRecoverablePayment(t, s, ctx, actor, headID, projectID, "PR-2026-000021", "2026-10-01", 500000)
	rec, err := s.RecoverableReport(ctx, RecoverableReportOptions{From: "2026-10", To: "2026-10"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec) != 1 || rec[0].Project == "" {
		t.Fatalf("recoverable row missing linked project: %+v", rec)
	}
}

func TestRecoverableAgeingLabels(t *testing.T) {
	cases := []struct {
		name                string
		paidOn, expected    string
		onHold              bool
		days                int
		wantLabel, wantTone string
		wantOverdue         bool
	}{
		{"on_hold_wins", "2026-02-12", "2026-06-30", true, -25, "On hold", "hold", false},
		{"unpaid", "", "2026-11-30", false, 128, "Awaiting payment", "approved", false},
		{"no_fixed_date", "2026-01-30", "", false, 0, "No fixed date", "neutral", false},
		{"overdue", "2026-02-12", "2026-06-30", false, -25, "25 days overdue", "bad", true},
		{"due_today", "2026-02-12", "2026-07-25", false, 0, "Due today", "neutral", false},
		{"future", "2026-04-03", "2027-01-15", false, 174, "174 days to go", "neutral", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			label, tone, overdue := recoverableAgeing(c.paidOn, c.expected, c.onHold, c.days)
			if label != c.wantLabel || tone != c.wantTone || overdue != c.wantOverdue {
				t.Fatalf("recoverableAgeing = (%q,%q,%v), want (%q,%q,%v)", label, tone, overdue, c.wantLabel, c.wantTone, c.wantOverdue)
			}
		})
	}
}

func TestRecoverableReportIncludesUnpaidAndExcludesDeadRequests(t *testing.T) { // G18
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	seedRecoverablePayment(t, s, ctx, actor, headID, 0, "PR-2026-000200", "2026-05-10", 400000)
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000201", "approved", "emd", "Ridge Metro tender authority", "2026-11-30", 200000, false)
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000202", "rejected", "emd", "Nobody", "2026-11-30", 999999, false)
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000203", "cancelled", "icd", "Nobody", "2026-11-30", 999999, false)

	asOf := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	rows, err := s.RecoverableReport(ctx, RecoverableReportOptions{AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("register rows = %d, want 2 (paid + unpaid; rejected and cancelled excluded)", len(rows))
	}
	byNumber := map[string]RecoverableRow{}
	for _, r := range rows {
		byNumber[r.Number] = r
	}
	unpaid, ok := byNumber["PR-2026-000201"]
	if !ok {
		t.Fatalf("unpaid recoverable missing from the register: %+v", rows)
	}
	if unpaid.PaidOn != "" || unpaid.Amount != 200000 || unpaid.AgeingTone != "approved" {
		t.Fatalf("unpaid row = %+v, want empty PaidOn, approved amount, approved tone", unpaid)
	}
	if unpaid.RequestID == 0 {
		t.Fatal("RecoverableRow.RequestID must be populated so the list can link to the detail screen")
	}
	// A date range still scopes to paid rows only.
	scoped, err := s.RecoverableReport(ctx, RecoverableReportOptions{From: "2026-05", To: "2026-05", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 1 || scoped[0].Number != "PR-2026-000200" {
		t.Fatalf("range-scoped rows = %+v, want only the May payment", scoped)
	}
}

func TestRecoverableReportAgeingFilterAndOrdering(t *testing.T) { // G18
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	asOf := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	// Each row is paid: money that never left cannot be overdue, so an unpaid
	// row ages as "Awaiting payment" no matter how old its return date is.
	r1 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000301", "completed", "emd", "Coastal Power", "2026-06-30", 200000, false) // 25 days overdue
	payRecoverable(t, s, ctx, actor, headID, r1, "2026-02-12", 200000)
	r2 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000302", "completed", "pbg", "Ridge Metro", "2026-08-10", 300000, false) // due in 16 days
	payRecoverable(t, s, ctx, actor, headID, r2, "2026-03-01", 300000)
	r3 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000303", "completed", "icd", "Harith Infra", "2027-01-15", 100000, false) // later
	payRecoverable(t, s, ctx, actor, headID, r3, "2026-04-03", 100000)
	r4 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000304", "completed", "security_deposit", "Whitefield", "", 65000, false) // no fixed date
	payRecoverable(t, s, ctx, actor, headID, r4, "2026-01-30", 65000)

	all, err := s.RecoverableReport(ctx, RecoverableReportOptions{AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("rows = %d, want 4", len(all))
	}
	if all[0].Number != "PR-2026-000301" || !all[0].Overdue || all[0].DaysToReturn != -25 {
		t.Fatalf("default order must put the overdue row first with DaysToReturn -25: %+v", all[0])
	}
	if all[1].Number != "PR-2026-000302" || all[3].Number != "PR-2026-000304" {
		t.Fatalf("default order = %s,%s,%s,%s; want soonest expected return first and no-date last",
			all[0].Number, all[1].Number, all[2].Number, all[3].Number)
	}

	overdue, err := s.RecoverableReport(ctx, RecoverableReportOptions{Ageing: "overdue", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(overdue) != 1 || overdue[0].Number != "PR-2026-000301" {
		t.Fatalf("ageing=overdue rows = %+v, want only PR-2026-000301", overdue)
	}
	due30, err := s.RecoverableReport(ctx, RecoverableReportOptions{Ageing: "due30", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(due30) != 1 || due30[0].Number != "PR-2026-000302" {
		t.Fatalf("ageing=due30 rows = %+v, want only PR-2026-000302", due30)
	}
	later, err := s.RecoverableReport(ctx, RecoverableReportOptions{Ageing: "later", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(later) != 2 {
		t.Fatalf("ageing=later rows = %d, want 2 (the 2027 date and the undated one)", len(later))
	}

	byAmount, err := s.RecoverableReport(ctx, RecoverableReportOptions{Order: "amount", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if byAmount[0].Number != "PR-2026-000302" || byAmount[0].Amount != 300000 {
		t.Fatalf("order=amount first row = %+v, want the largest row", byAmount[0])
	}
	byCounterparty, err := s.RecoverableReport(ctx, RecoverableReportOptions{Counterparty: "Harith Infra", AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(byCounterparty) != 1 || byCounterparty[0].Number != "PR-2026-000303" {
		t.Fatalf("counterparty filter = %+v, want only PR-2026-000303", byCounterparty)
	}
}

func TestRecoverableMetricsFourDashboardNumbers(t *testing.T) { // G18
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	asOf := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)

	// Paid this month, due far out.
	req1 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000401", "completed", "pbg", "Ridge Metro", "2027-01-15", 300000, false)
	payRecoverable(t, s, ctx, actor, headID, req1, "2026-07-05", 300000)
	// Paid earlier, now overdue.
	req2 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000402", "completed", "emd", "Coastal Power", "2026-06-30", 200000, false)
	payRecoverable(t, s, ctx, actor, headID, req2, "2026-02-12", 200000)
	// Paid earlier, due inside 30 days.
	req3 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000403", "completed", "icd", "Harith Infra", "2026-08-10", 100000, false)
	payRecoverable(t, s, ctx, actor, headID, req3, "2026-03-01", 100000)
	// Approved, unpaid: outstanding but nothing paid out.
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000404", "approved", "emd", "Ridge Metro", "2026-11-30", 50000, false)
	// Rejected: invisible everywhere.
	seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000405", "rejected", "emd", "Nobody", "2026-01-01", 999999, false)

	m, err := s.RecoverableMetrics(ctx, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if m.OutstandingAmount != 650000 || m.OutstandingCount != 4 {
		t.Fatalf("outstanding = %d over %d rows, want 650000 over 4", m.OutstandingAmount, m.OutstandingCount)
	}
	if m.OverdueAmount != 200000 || m.OverdueCount != 1 {
		t.Fatalf("past expected return = %d over %d, want 200000 over 1", m.OverdueAmount, m.OverdueCount)
	}
	if m.DueIn30Amount != 100000 || m.DueIn30Count != 1 {
		t.Fatalf("due in 30 days = %d over %d, want 100000 over 1", m.DueIn30Amount, m.DueIn30Count)
	}
	if m.PaidThisMonthAmount != 300000 || m.PaidThisMonthCount != 1 {
		t.Fatalf("paid out this month = %d over %d, want 300000 over 1", m.PaidThisMonthAmount, m.PaidThisMonthCount)
	}
}

func TestRecoverableRollupsByCategoryAndCounterparty(t *testing.T) { // G18
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	asOf := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	req1 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000501", "completed", "emd", "Ridge Metro", "2026-06-30", 200000, false)
	payRecoverable(t, s, ctx, actor, headID, req1, "2026-02-12", 200000)
	req2 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000502", "completed", "emd", "Coastal Power", "2026-12-31", 400000, false)
	payRecoverable(t, s, ctx, actor, headID, req2, "2026-04-03", 400000)
	req3 := seedRecoverableRequestOnly(t, s, ctx, actor, headID, 0, "PR-2026-000503", "completed", "pbg", "Ridge Metro", "2026-11-30", 300000, false)
	payRecoverable(t, s, ctx, actor, headID, req3, "2026-05-20", 300000)

	cats, err := s.RecoverableRollups(ctx, "category", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 2 {
		t.Fatalf("category rollups = %d, want 2 (EMD, PBG)", len(cats))
	}
	if cats[0].Label != "EMD" || cats[0].Count != 2 || cats[0].Outstanding != 600000 {
		t.Fatalf("EMD rollup = %+v, want 2 items totalling 600000, largest first", cats[0])
	}
	if cats[0].Overdue != 200000 {
		t.Fatalf("EMD overdue = %d, want 200000", cats[0].Overdue)
	}
	if cats[0].Oldest != "2026-02-12" {
		t.Fatalf("EMD oldest paid_on = %q, want 2026-02-12", cats[0].Oldest)
	}

	cps, err := s.RecoverableRollups(ctx, "counterparty", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(cps) != 2 {
		t.Fatalf("counterparty rollups = %d, want 2", len(cps))
	}
	if cps[0].Label != "Ridge Metro" || cps[0].Count != 2 || cps[0].Outstanding != 500000 {
		t.Fatalf("Ridge Metro rollup = %+v, want 2 items totalling 500000", cps[0])
	}
	if !strings.Contains(cps[0].Detail, "EMD") || !strings.Contains(cps[0].Detail, "PBG") {
		t.Fatalf("counterparty Detail = %q, want the distinct categories", cps[0].Detail)
	}
	if cps[0].ExpectedBack != "2026-06-30" {
		t.Fatalf("Ridge Metro expected back = %q, want the earliest date 2026-06-30", cps[0].ExpectedBack)
	}

	if _, err := s.RecoverableRollups(ctx, "project", asOf); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown rollup dimension = %v, want ErrValidation", err)
	}
}
