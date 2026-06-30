package store

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestOpenCreatesSchemaBasics(t *testing.T) {
	s := newTestStore(t)

	wantTables := []string{
		"users",
		"projects",
		"heads",
		"budgets",
		"budget_months",
		"payments",
		"payment_attachments",
		"month_locks",
		"audit_log",
	}
	for _, table := range wantTables {
		t.Run(table, func(t *testing.T) {
			var name string
			err := s.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
			if err != nil {
				t.Fatalf("table %q missing: %v", table, err)
			}
		})
	}

	var foreignKeys int
	if err := s.DB().QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
}

func TestProjectAndHeadUniqueness(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	projectID, err := s.UpsertProject(ctx, 0, "Operations", true, 1)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.UpsertProject(ctx, 0, "Operations", true, 2); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate project error = %v, want %v", err, ErrDuplicate)
	}

	if _, err := s.UpsertHead(ctx, 0, projectID, "Rent", "5", true, 1); err != nil {
		t.Fatalf("create head: %v", err)
	}
	if _, err := s.UpsertHead(ctx, 0, projectID, "Rent", "10", true, 2); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate head in same project error = %v, want %v", err, ErrDuplicate)
	}

	otherProjectID, err := s.UpsertProject(ctx, 0, "Field", true, 2)
	if err != nil {
		t.Fatalf("create other project: %v", err)
	}
	if _, err := s.UpsertHead(ctx, 0, otherProjectID, "Rent", "15", true, 1); err != nil {
		t.Fatalf("same head name in different project should be allowed: %v", err)
	}
}

func TestCreateMonthPlanCopiesOnlyActiveHeads(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	userID, err := s.CreateUser(ctx, "planner@example.com", "Planner", "hash", "admin", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	actor, err := s.UserByID(ctx, userID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	projectID, err := s.UpsertProject(ctx, 0, "Operations", true, 1)
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	activeHeadID, err := s.UpsertHead(ctx, 0, projectID, "Rent", "5", true, 1)
	if err != nil {
		t.Fatalf("create active head: %v", err)
	}
	retiredHeadID, err := s.UpsertHead(ctx, 0, projectID, "Old Lease", "10", true, 2)
	if err != nil {
		t.Fatalf("create retiring head: %v", err)
	}
	if err := s.SetBudget(ctx, actor, activeHeadID, "2026-06", 100000); err != nil {
		t.Fatalf("SetBudget active source: %v", err)
	}
	if err := s.SetBudget(ctx, actor, retiredHeadID, "2026-06", 50000); err != nil {
		t.Fatalf("SetBudget retired source: %v", err)
	}
	if _, err := s.UpsertHead(ctx, retiredHeadID, projectID, "Old Lease", "10", false, 2); err != nil {
		t.Fatalf("retire head: %v", err)
	}

	if err := s.CreateMonthPlan(ctx, actor, "2026-07", "2026-06"); err != nil {
		t.Fatalf("CreateMonthPlan: %v", err)
	}
	if b, err := s.Budget(ctx, activeHeadID, "2026-07"); err != nil || b.Amount != 100000 {
		t.Fatalf("copied active budget = %+v, err=%v", b, err)
	}
	if _, err := s.Budget(ctx, retiredHeadID, "2026-07"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retired head copied into new month, err=%v", err)
	}

	plans, err := s.ListMonthPlans(ctx)
	if err != nil {
		t.Fatalf("ListMonthPlans: %v", err)
	}
	var found bool
	for _, plan := range plans {
		if plan.Month == "2026-07" {
			found = true
			if plan.SourceMonth != "2026-06" || plan.Budget != 100000 || plan.Status != "open" {
				t.Fatalf("new month plan = %+v", plan)
			}
		}
	}
	if !found {
		t.Fatalf("2026-07 plan not listed: %+v", plans)
	}
}

func TestHistoricalGridAndReportsIncludeRetiredHeads(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)

	if err := s.SetBudget(ctx, actor, headID, "2026-06", 100000); err != nil {
		t.Fatalf("SetBudget: %v", err)
	}
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-06-12", Amount: 25000, VendorPayee: "Legacy Vendor"}); err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	if _, err := s.UpsertHead(ctx, headID, 1, "Rent", "5", false, 1); err != nil {
		t.Fatalf("retire head: %v", err)
	}

	june, err := s.Grid(ctx, "2026-06", "", "")
	if err != nil {
		t.Fatalf("Grid June: %v", err)
	}
	if len(june.Rows) != 1 {
		t.Fatalf("June rows = %+v, want retired historical row", june.Rows)
	}
	if june.Rows[0].Active || june.Rows[0].Budget != 100000 || june.Rows[0].Actual != 25000 {
		t.Fatalf("June historical row = %+v", june.Rows[0])
	}

	july, err := s.Grid(ctx, "2026-07", "", "")
	if err != nil {
		t.Fatalf("Grid July: %v", err)
	}
	if len(july.Rows) != 0 {
		t.Fatalf("July rows = %+v, want retired unused head hidden", july.Rows)
	}

	rows, err := s.Report(ctx, "2026-06", "2026-06", "heads")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(rows) != 1 || rows[0].Budget != 100000 || rows[0].Actual != 25000 {
		t.Fatalf("historical report rows = %+v", rows)
	}
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-07-01", Amount: 10000, VendorPayee: "Should Fail"}); !errors.Is(err, ErrInactiveHead) {
		t.Fatalf("CreatePayment inactive head error = %v, want %v", err, ErrInactiveHead)
	}
}

func TestPaymentCreateEditVoidAndAudit(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)

	paymentID, err := s.CreatePayment(ctx, actor, PaymentInput{
		HeadID:      headID,
		PaidOn:      "2026-01-15",
		Amount:      125000,
		VendorPayee: "Acme Supplies",
		PaymentMode: "NEFT",
		InvoiceNo:   "INV-1",
		ReferenceNo: "REF-1",
		Remarks:     "first payment",
	})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	p, err := s.Payment(ctx, paymentID)
	if err != nil {
		t.Fatalf("Payment after create: %v", err)
	}
	if p.Amount != 125000 || p.VendorPayee != "Acme Supplies" || p.EnteredBy != actor.ID || p.VoidedAt != nil {
		t.Fatalf("created payment = %+v", p)
	}

	err = s.UpdatePayment(ctx, actor, paymentID, PaymentInput{
		HeadID:      headID,
		PaidOn:      "2026-01-20",
		Amount:      150000,
		VendorPayee: "Acme Supplies Pvt Ltd",
		PaymentMode: "UPI",
		InvoiceNo:   "INV-2",
		ReferenceNo: "REF-2",
		Remarks:     "edited payment",
	})
	if err != nil {
		t.Fatalf("UpdatePayment: %v", err)
	}
	p, err = s.Payment(ctx, paymentID)
	if err != nil {
		t.Fatalf("Payment after update: %v", err)
	}
	if p.Amount != 150000 || p.PaymentMode != "UPI" || p.UpdatedBy == nil || *p.UpdatedBy != actor.ID {
		t.Fatalf("updated payment = %+v", p)
	}

	if err := s.VoidPayment(ctx, actor, paymentID, "duplicate entry"); err != nil {
		t.Fatalf("VoidPayment: %v", err)
	}
	p, err = s.Payment(ctx, paymentID)
	if err != nil {
		t.Fatalf("Payment after void: %v", err)
	}
	if p.VoidedAt == nil || p.VoidedBy == nil || *p.VoidedBy != actor.ID || p.VoidReason != "duplicate entry" {
		t.Fatalf("voided payment = %+v", p)
	}

	audits, err := s.Audit(ctx, "payment", paymentID, 10)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	assertAuditActions(t, audits, []string{"create", "update", "void"})
	assertAuditPayloads(t, audits, paymentID)

	recent, err := s.RecentPayments(ctx, 10)
	if err != nil {
		t.Fatalf("RecentPayments: %v", err)
	}
	for _, payment := range recent {
		if payment.ID == paymentID {
			t.Fatalf("voided payment %d appeared in recent payments: %+v", paymentID, recent)
		}
	}
}

func TestLockedMonthRejectsBudgetAndPaymentMutations(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)

	oldMonthPaymentID, err := s.CreatePayment(ctx, actor, PaymentInput{
		HeadID:      headID,
		PaidOn:      "2026-02-10",
		Amount:      100000,
		VendorPayee: "Before Lock",
	})
	if err != nil {
		t.Fatalf("CreatePayment before lock: %v", err)
	}
	voidPaymentID, err := s.CreatePayment(ctx, actor, PaymentInput{
		HeadID:      headID,
		PaidOn:      "2026-02-11",
		Amount:      200000,
		VendorPayee: "Void Candidate",
	})
	if err != nil {
		t.Fatalf("CreatePayment void candidate before lock: %v", err)
	}

	if err := s.LockMonth(ctx, actor, "2026-02", "month close"); err != nil {
		t.Fatalf("LockMonth: %v", err)
	}
	if !s.IsLocked(ctx, "2026-02") {
		t.Fatalf("IsLocked(2026-02) = false, want true")
	}

	if err := s.SetBudget(ctx, actor, headID, "2026-02", 300000); !errors.Is(err, ErrLockedMonth) {
		t.Fatalf("SetBudget locked month error = %v, want %v", err, ErrLockedMonth)
	}
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{
		HeadID:      headID,
		PaidOn:      "2026-02-12",
		Amount:      400000,
		VendorPayee: "After Lock",
	}); !errors.Is(err, ErrLockedMonth) {
		t.Fatalf("CreatePayment locked month error = %v, want %v", err, ErrLockedMonth)
	}
	if err := s.UpdatePayment(ctx, actor, oldMonthPaymentID, PaymentInput{
		HeadID:      headID,
		PaidOn:      "2026-03-01",
		Amount:      110000,
		VendorPayee: "Move Out",
	}); !errors.Is(err, ErrLockedMonth) {
		t.Fatalf("UpdatePayment locked original month error = %v, want %v", err, ErrLockedMonth)
	}
	if err := s.VoidPayment(ctx, actor, voidPaymentID, "late void"); !errors.Is(err, ErrLockedMonth) {
		t.Fatalf("VoidPayment locked month error = %v, want %v", err, ErrLockedMonth)
	}

	p, err := s.Payment(ctx, voidPaymentID)
	if err != nil {
		t.Fatalf("Payment after rejected void: %v", err)
	}
	if p.VoidedAt != nil {
		t.Fatalf("payment was voided despite locked month: %+v", p)
	}
}

func TestRecordAuditCreatesQueryableEntry(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)
	entityID := int64(42)

	err := s.RecordAudit(ctx, AuditInput{
		ActorID:    &actor.ID,
		ActorName:  actor.Name,
		Action:     "create",
		EntityType: "project",
		EntityID:   &entityID,
		Summary:    "Created project",
		Before:     nil,
		After:      map[string]any{"id": entityID, "name": "Operations"},
		IP:         "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("RecordAudit: %v", err)
	}

	audits, err := s.Audit(ctx, "project", entityID, 1)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(audits) != 1 {
		t.Fatalf("audit count = %d, want 1", len(audits))
	}
	a := audits[0]
	if a.ActorID == nil || *a.ActorID != actor.ID || a.ActorName != actor.Name || a.Action != "create" || a.EntityType != "project" || a.EntityID == nil || *a.EntityID != entityID {
		t.Fatalf("audit entry = %+v", a)
	}
	if a.BeforeJSON != "" {
		t.Fatalf("BeforeJSON = %q, want empty for nil before", a.BeforeJSON)
	}
	if !strings.Contains(a.AfterJSON, `"name":"Operations"`) || a.IP != "127.0.0.1" {
		t.Fatalf("audit payload = %+v", a)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "fervid-budget-test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	return s
}

func seedActorAndHead(t *testing.T, s *Store, ctx context.Context) (User, int64) {
	t.Helper()
	userID, err := s.CreateUser(ctx, "qa@example.com", "QA User", "hash", "admin", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	actor, err := s.UserByID(ctx, userID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	projectID, err := s.UpsertProject(ctx, 0, "Operations", true, 1)
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	headID, err := s.UpsertHead(ctx, 0, projectID, "Rent", "5", true, 1)
	if err != nil {
		t.Fatalf("UpsertHead: %v", err)
	}
	return actor, headID
}

func assertAuditActions(t *testing.T, audits []AuditEntry, want []string) {
	t.Helper()
	if len(audits) != len(want) {
		t.Fatalf("audit count = %d, want %d: %+v", len(audits), len(want), audits)
	}
	got := make([]string, 0, len(audits))
	for _, audit := range audits {
		got = append(got, audit.Action)
	}
	sort.Strings(got)
	sort.Strings(want)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("audit actions = %v, want %v", got, want)
		}
	}
}

func assertAuditPayloads(t *testing.T, audits []AuditEntry, paymentID int64) {
	t.Helper()
	for _, audit := range audits {
		if audit.EntityID == nil || *audit.EntityID != paymentID {
			t.Fatalf("audit entity = %+v, want payment %d", audit, paymentID)
		}
		switch audit.Action {
		case "create":
			if audit.BeforeJSON != "" || !strings.Contains(audit.AfterJSON, `"VendorPayee":"Acme Supplies"`) {
				t.Fatalf("create audit payload = %+v", audit)
			}
		case "update":
			if !strings.Contains(audit.BeforeJSON, `"Amount":125000`) || !strings.Contains(audit.AfterJSON, `"Amount":150000`) {
				t.Fatalf("update audit payload = %+v", audit)
			}
		case "void":
			if !strings.Contains(audit.BeforeJSON, `"VoidReason":""`) || !strings.Contains(audit.AfterJSON, `"VoidReason":"duplicate entry"`) {
				t.Fatalf("void audit payload = %+v", audit)
			}
		default:
			t.Fatalf("unexpected audit action %q", audit.Action)
		}
	}
}
