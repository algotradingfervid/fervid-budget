package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
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

func TestCaseInsensitiveIdentifiersAndDueDayValidation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.CreateUser(ctx, "ADMIN@example.com", "Admin", "hash", "admin", true); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := s.CreateUser(ctx, "admin@example.com", "Second", "hash", "admin", true); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("case-insensitive email duplicate = %v, want %v", err, ErrDuplicate)
	}
	projectID, err := s.UpsertProject(ctx, 0, "Operations", true, 1)
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	if _, err := s.UpsertProject(ctx, 0, "operations", true, 2); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("case-insensitive project duplicate = %v, want %v", err, ErrDuplicate)
	}
	if _, err := s.UpsertHead(ctx, 0, projectID, "Rent", "31", true, 1); err != nil {
		t.Fatalf("UpsertHead: %v", err)
	}
	if _, err := s.UpsertHead(ctx, 0, projectID, "rent", "1", true, 2); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("case-insensitive head duplicate = %v, want %v", err, ErrDuplicate)
	}
	if _, err := s.UpsertHead(ctx, 0, projectID, "Utilities", "32", true, 2); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid due day = %v, want %v", err, ErrValidation)
	}
}

func TestSetBudgetsIsAtomic(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	err := s.SetBudgets(ctx, actor, "2026-08", []BudgetInput{{HeadID: headID, Amount: 10000}, {HeadID: 999999, Amount: 20000}})
	if err == nil {
		t.Fatal("SetBudgets succeeded with a nonexistent head")
	}
	if _, err := s.Budget(ctx, headID, "2026-08"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("budget persisted despite failed batch: %v", err)
	}
	var plans int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM budget_months WHERE month='2026-08'`).Scan(&plans); err != nil {
		t.Fatal(err)
	}
	if plans != 0 {
		t.Fatalf("month plan persisted despite failed batch: %d", plans)
	}
}

func TestLoginAttemptLockAndReset(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	userID, err := s.CreateUser(ctx, "login@example.com", "Login User", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		until, err := s.RecordFailedLogin(ctx, "login@example.com")
		if err != nil || !until.IsZero() {
			t.Fatalf("failure %d = %v, %v", i+1, until, err)
		}
	}
	until, err := s.RecordFailedLogin(ctx, "login@example.com")
	if err != nil || until.Before(time.Now()) {
		t.Fatalf("fifth failed login lock = %v, %v", until, err)
	}
	locked, gotUntil, err := s.LoginLocked(ctx, "LOGIN@example.com")
	if err != nil || !locked || gotUntil.IsZero() {
		t.Fatalf("LoginLocked = %v, %v, %v", locked, gotUntil, err)
	}
	if err := s.ResetLoginAttempts(ctx, userID); err != nil {
		t.Fatal(err)
	}
	locked, _, err = s.LoginLocked(ctx, "login@example.com")
	if err != nil || locked {
		t.Fatalf("LoginLocked after reset = %v, %v", locked, err)
	}
}

func TestLastActiveAdminAndAttachmentInvariants(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	if err := s.UpdateUser(ctx, actor.ID, actor.Name, "data_entry", true, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("demoting final admin = %v, want %v", err, ErrValidation)
	}
	paymentID, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-03-02", Amount: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAttachment(ctx, actor, paymentID, "receipt.txt", "/tmp/receipt.txt", "text/plain", 12); err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}
	attachments, err := s.Attachments(ctx, paymentID)
	if err != nil || len(attachments) != 1 {
		t.Fatalf("Attachments = %+v, %v", attachments, err)
	}
	if got, err := s.AttachmentByID(ctx, attachments[0].ID); err != nil || got.PaymentID != paymentID {
		t.Fatalf("AttachmentByID = %+v, %v", got, err)
	}
	if err := s.LockMonth(ctx, actor, "2026-03", "close"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAttachment(ctx, actor, paymentID, "second.txt", "/tmp/second.txt", "text/plain", 1); !errors.Is(err, ErrLockedMonth) {
		t.Fatalf("locked AddAttachment = %v, want %v", err, ErrLockedMonth)
	}
	if err := s.UnlockMonth(ctx, actor, "2026-03", "correction"); err != nil {
		t.Fatal(err)
	}
	if err := s.VoidPayment(ctx, actor, paymentID, "duplicate"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAttachment(ctx, actor, paymentID, "third.txt", "/tmp/third.txt", "text/plain", 1); !errors.Is(err, ErrValidation) {
		t.Fatalf("voided AddAttachment = %v, want %v", err, ErrValidation)
	}
	audit, err := s.Audit(ctx, "payment", paymentID, 10)
	if err != nil {
		t.Fatal(err)
	}
	var attached bool
	for _, entry := range audit {
		attached = attached || entry.Action == "attach"
	}
	if !attached {
		t.Fatalf("attachment was not recorded on the payment timeline: %+v", audit)
	}
}

func TestCreatePaymentWithAttachmentIsAtomic(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	_, err := s.CreatePaymentWithAttachment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-04-02", Amount: 5000}, &AttachmentInput{OriginalName: "", StoredPath: "/tmp/nope", SizeBytes: 1})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid attachment = %v, want %v", err, ErrValidation)
	}
	payments, err := s.ListPayments(ctx, PaymentListOptions{Month: "2026-04", Limit: 10, Scope: ScopeAll})
	if err != nil {
		t.Fatal(err)
	}
	if len(payments) != 0 {
		t.Fatalf("payment was written despite rejected attachment: %+v", payments)
	}
	id, err := s.CreatePaymentWithAttachment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-04-02", Amount: 5000}, &AttachmentInput{OriginalName: "receipt.pdf", StoredPath: "/tmp/receipt.pdf", MimeType: "application/pdf", SizeBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	attachments, err := s.Attachments(ctx, id)
	if err != nil || len(attachments) != 1 {
		t.Fatalf("atomic attachment = %+v, %v", attachments, err)
	}
}

func TestPaymentMutationsRollBackWhenAuditFails(t *testing.T) {
	ctx := context.Background()
	t.Run("create", func(t *testing.T) {
		s := newTestStore(t)
		actor, headID := seedActorAndHead(t, s, ctx)
		abortAuditAction(t, s, "create")
		if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-05-02", Amount: 1000}); err == nil {
			t.Fatal("CreatePayment succeeded despite audit failure")
		}
		if got := paymentCount(t, s); got != 0 {
			t.Fatalf("payment persisted after failed audit: %d", got)
		}
	})
	t.Run("update_with_attachment", func(t *testing.T) {
		s := newTestStore(t)
		actor, headID := seedActorAndHead(t, s, ctx)
		paymentID, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-05-02", Amount: 1000, VendorPayee: "Original"})
		if err != nil {
			t.Fatal(err)
		}
		abortAuditAction(t, s, "update")
		err = s.UpdatePaymentWithAttachment(ctx, actor, paymentID, PaymentInput{HeadID: headID, PaidOn: "2026-05-03", Amount: 2000, VendorPayee: "Changed"}, &AttachmentInput{OriginalName: "receipt.pdf", StoredPath: "/tmp/receipt.pdf", SizeBytes: 1})
		if err == nil {
			t.Fatal("UpdatePaymentWithAttachment succeeded despite audit failure")
		}
		payment, err := s.Payment(ctx, paymentID)
		if err != nil || payment.Amount != 1000 || payment.VendorPayee != "Original" {
			t.Fatalf("payment changed after failed audit: %+v, %v", payment, err)
		}
		attachments, err := s.Attachments(ctx, paymentID)
		if err != nil || len(attachments) != 0 {
			t.Fatalf("attachment persisted after failed audit: %+v, %v", attachments, err)
		}
	})
	t.Run("void", func(t *testing.T) {
		s := newTestStore(t)
		actor, headID := seedActorAndHead(t, s, ctx)
		paymentID, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-05-02", Amount: 1000})
		if err != nil {
			t.Fatal(err)
		}
		abortAuditAction(t, s, "void")
		if err := s.VoidPayment(ctx, actor, paymentID, "duplicate"); err == nil {
			t.Fatal("VoidPayment succeeded despite audit failure")
		}
		payment, err := s.Payment(ctx, paymentID)
		if err != nil || payment.VoidedAt != nil {
			t.Fatalf("payment was voided after failed audit: %+v, %v", payment, err)
		}
	})
	t.Run("attachment", func(t *testing.T) {
		s := newTestStore(t)
		actor, headID := seedActorAndHead(t, s, ctx)
		paymentID, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-05-02", Amount: 1000})
		if err != nil {
			t.Fatal(err)
		}
		abortAuditAction(t, s, "attach")
		if err := s.AddAttachment(ctx, actor, paymentID, "receipt.pdf", "/tmp/receipt.pdf", "application/pdf", 1); err == nil {
			t.Fatal("AddAttachment succeeded despite audit failure")
		}
		attachments, err := s.Attachments(ctx, paymentID)
		if err != nil || len(attachments) != 0 {
			t.Fatalf("attachment persisted after failed audit: %+v, %v", attachments, err)
		}
	})
}

func TestUpdatePaymentWithAttachmentCommitsTogether(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	paymentID, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-06-01", Amount: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePaymentWithAttachment(ctx, actor, paymentID, PaymentInput{HeadID: headID, PaidOn: "2026-06-02", Amount: 2000}, &AttachmentInput{OriginalName: "receipt.pdf", StoredPath: "/tmp/receipt.pdf", SizeBytes: 1}); err != nil {
		t.Fatal(err)
	}
	payment, err := s.Payment(ctx, paymentID)
	if err != nil || payment.Amount != 2000 {
		t.Fatalf("updated payment = %+v, %v", payment, err)
	}
	attachments, err := s.Attachments(ctx, paymentID)
	if err != nil || len(attachments) != 1 {
		t.Fatalf("updated attachment = %+v, %v", attachments, err)
	}
}

func abortAuditAction(t *testing.T, s *Store, action string) {
	t.Helper()
	if _, err := s.DB().Exec(`CREATE TRIGGER abort_audit BEFORE INSERT ON audit_log WHEN NEW.action = '` + action + `' BEGIN SELECT RAISE(ABORT, 'audit blocked'); END`); err != nil {
		t.Fatalf("create audit trigger: %v", err)
	}
}

func paymentCount(t *testing.T, s *Store) int {
	t.Helper()
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
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

// TestRecordPaymentDerivesHeadPayeeAndInvoiceFromRequest is the F-D-01 proof:
// a manager approves an amount against a project and head, and the entry
// screen offers no control to change the head, the payee or the invoice — so
// the store derives all three from the request row and ignores the form's
// copies. Before the fix a forged head_id charged an approved payment to a
// head nobody approved, with the audit blob recording the forgery as truth.
func TestRecordPaymentDerivesHeadPayeeAndInvoiceFromRequest(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	otherProjectID, err := s.UpsertProject(ctx, 0, "People", true, 2)
	if err != nil {
		t.Fatal(err)
	}
	otherHeadID, err := s.UpsertHead(ctx, 0, otherProjectID, "Payroll", "1", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 440000, 440000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	payID, err := historicalSettlement(s, ctx, acc, reqID, PaymentInput{
		HeadID:      otherHeadID, // forged: another project's head
		PaidOn:      "2026-06-15",
		Amount:      440000,
		VendorPayee: "NOT THE APPROVED PAYEE",
		InvoiceNo:   "FORGED-INV-1",
	}, "settled", "", nil)
	if err != nil {
		t.Fatalf("record with forged snapshot fields: %v", err)
	}
	p, err := s.Payment(ctx, payID)
	if err != nil {
		t.Fatal(err)
	}
	if p.HeadID != headID || p.Project != "Operations" || p.Head != "Rent" {
		t.Fatalf("payment charged to %d (%s / %s), want the request's head %d (Operations / Rent)", p.HeadID, p.Project, p.Head, headID)
	}
	if p.VendorPayee != "Acme Landlord" {
		t.Fatalf("payee = %q, want the request's payee Acme Landlord", p.VendorPayee)
	}
	if p.InvoiceNo != "" {
		t.Fatalf("invoice = %q, want the request's (empty) invoice", p.InvoiceNo)
	}
	// The audit's after blob records what was written, not what was posted.
	audits, err := s.Audit(ctx, "payment", payID, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range audits {
		if strings.Contains(a.AfterJSON, "NOT THE APPROVED PAYEE") || strings.Contains(a.AfterJSON, "FORGED-INV-1") {
			t.Fatalf("audit blob carries the forged values: %s", a.AfterJSON)
		}
	}
}

// TestRecoverableSettlementNeedsNoHead is the F-D-11 / F-E-01 / F-G-001 proof,
// through the real write path end to end: CreateRequest (the shape the form
// produces — no head for a recoverable), ApproveRequest, ReserveRequest, then
// RecordPaymentForRequest posting head_id=0 exactly as the entry screen's
// hidden input does. Before v8 this died on payments.head_id NOT NULL for
// every one of the six categories.
func TestRecoverableSettlementNeedsNoHead(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx) // budget head for the grid contrast
	mgrRow, err := s.CreateUser(ctx, "approver@example.com", "Approver", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	mgr, _ := s.UserByID(ctx, mgrRow)
	reqID, err := s.CreateRequest(ctx, actor, RequestInput{
		Treatment:           "recoverable",
		Type:                "employee_advance",
		RecoverableCategory: "employee_advance",
		ShortTitle:          "Site travel advance",
		Amount:              500000,
		Purpose:             "Advance for site travel",
		AdvanceReason:       "Travel to the Pune site",
		ExpectedReturnDate:  "2027-03-31",
		RepaymentNotes:      "Deducted from salary",
		ManagerID:           mgr.ID,
	})
	if err != nil {
		t.Fatalf("CreateRequest through the real path: %v", err)
	}
	if err := s.ApproveRequest(ctx, mgr, reqID, 500000, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := s.ReserveRequest(ctx, mgr, reqID); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	// A budget payment in the same month, so the grid assertion below has a
	// number to keep.
	if err := s.SetBudget(ctx, actor, headID, "2026-07", 1000000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-07-10", Amount: 100000, VendorPayee: "Budget Vendor"}); err != nil {
		t.Fatal(err)
	}
	// head_id=0 is exactly what the entry screen's hidden input posts.
	payID, err := historicalSettlement(s, ctx, mgr, reqID, PaymentInput{
		HeadID: 0, PaidOn: "2026-07-20", Amount: 500000, VendorPayee: "whatever the form carried",
	}, "settled", "", nil)
	if err != nil {
		t.Fatalf("settling a recoverable must not need a head: %v", err)
	}
	// The row stores NULL, not 0 — heads has no row 0 and the FK survives v8.
	var storedHead sql.NullInt64
	if err := s.DB().QueryRowContext(ctx, `SELECT head_id FROM payments WHERE id=?`, payID).Scan(&storedHead); err != nil {
		t.Fatal(err)
	}
	if storedHead.Valid {
		t.Fatalf("recoverable payment stored head_id=%d, want NULL", storedHead.Int64)
	}
	// Every reader still resolves the row.
	p, err := s.Payment(ctx, payID)
	if err != nil {
		t.Fatalf("Payment() must resolve a NULL-head row: %v", err)
	}
	if p.HeadID != 0 || p.Project != "" || p.Head != "" {
		t.Fatalf("NULL-head payment read back as %+v", p)
	}
	// An employee advance pays the requester (forcesRequesterPayee), and the
	// derived payee proves the form's value was ignored here too.
	if p.VendorPayee != actor.Name {
		t.Fatalf("payee = %q, want the requester %q", p.VendorPayee, actor.Name)
	}
	if _, err := s.PaymentForRequest(ctx, reqID); err != nil {
		t.Fatalf("PaymentForRequest() must resolve a NULL-head row: %v", err)
	}
	list, err := s.ListPayments(ctx, PaymentListOptions{Month: "2026-07", Scope: ScopeAll})
	if err != nil {
		t.Fatal(err)
	}
	var listed bool
	for _, row := range list {
		listed = listed || row.ID == payID
	}
	if !listed {
		t.Fatalf("NULL-head payment missing from the ledger: %+v", list)
	}
	recent, err := s.RecentPayments(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var inRecent bool
	for _, row := range recent {
		inRecent = inRecent || row.ID == payID
	}
	if !inRecent {
		t.Fatalf("NULL-head payment missing from recent payments")
	}
	// The request closed and kept its classification.
	var status, treatment string
	if err := s.DB().QueryRowContext(ctx, `SELECT status, treatment FROM payment_requests WHERE id=?`, reqID).Scan(&status, &treatment); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || treatment != "recoverable" {
		t.Fatalf("request after settle = %s/%s, want completed/recoverable", status, treatment)
	}
	// And the grid and monthly report never see it: a NULL head matches no
	// head row, so only the budget payment counts.
	grid, err := s.Grid(ctx, "2026-07", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if grid.Total.Actual != 100000 {
		t.Fatalf("grid actual = %d, want 100000 (recoverable with NULL head excluded)", grid.Total.Actual)
	}
	rows, err := s.Report(ctx, "2026-07", "2026-07", "heads")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Actual != 100000 {
		t.Fatalf("report rows = %+v, want one row with actual 100000", rows)
	}
}

// TestReserveRequestNamesEachRefusalCause is the F-D-02 store half: the three
// reasons a reservation can be refused arrive as three distinct sentinels, each
// still an ErrForbidden so every existing handler branch keeps working.
func TestReserveRequestNamesEachRefusalCause(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	otherID, err := s.CreateUser(ctx, "other-acc@example.com", "Other Accountant", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.UserByID(ctx, otherID)

	held := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 100000, 100000)
	if err := s.HoldRequest(ctx, acc, held, "waiting on the requester"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, acc, held); !errors.Is(err, ErrRequestOnHold) || !errors.Is(err, ErrForbidden) {
		t.Fatalf("reserve held = %v, want ErrRequestOnHold (wrapping ErrForbidden)", err)
	}

	taken := seedApprovedRequest(t, s, ctx, 2, req.ID, mgrID, headID, 200000, 200000)
	if err := s.ReserveRequest(ctx, other, taken); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, acc, taken); !errors.Is(err, ErrAlreadyReserved) || !errors.Is(err, ErrForbidden) {
		t.Fatalf("reserve taken = %v, want ErrAlreadyReserved (wrapping ErrForbidden)", err)
	}

	done := seedApprovedRequest(t, s, ctx, 3, req.ID, mgrID, headID, 300000, 300000)
	if _, err := s.DB().ExecContext(ctx, `UPDATE payment_requests SET status='completed' WHERE id=?`, done); err != nil {
		t.Fatal(err)
	}
	if err := s.ReserveRequest(ctx, acc, done); !errors.Is(err, ErrRequestNotApproved) || !errors.Is(err, ErrForbidden) {
		t.Fatalf("reserve completed = %v, want ErrRequestNotApproved (wrapping ErrForbidden)", err)
	}

	if err := s.ReserveRequest(ctx, acc, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reserve missing = %v, want ErrNotFound", err)
	}
}

// TestClassifyTurnsForeignKeyViolationsIntoValidation covers F-B-06 / F-G-027:
// a forged foreign id is a client error that must surface as a 400 keeping the
// typed form, not a 500 losing it — and the driver text never travels.
func TestClassifyTurnsForeignKeyViolationsIntoValidation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	raw := fmt.Errorf("constraint failed: FOREIGN KEY constraint failed (787)")
	got := classify(raw)
	if !errors.Is(got, ErrValidation) {
		t.Fatalf("classify(FK) = %v, want ErrValidation", got)
	}
	if strings.Contains(got.Error(), "787") || strings.Contains(got.Error(), "constraint failed") {
		t.Fatalf("classify(FK) echoes driver text: %q", got.Error())
	}

	// And through a real write: a request naming a vendor that does not exist.
	actor, headID := seedActorAndHead(t, s, ctx)
	mgrRow, err := s.CreateUser(ctx, "fk-approver@example.com", "FK Approver", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	var projectID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateRequest(ctx, actor, RequestInput{
		Treatment: "budget", Type: "vendor_invoice", ProjectID: projectID, HeadID: headID,
		VendorID: 999999, ShortTitle: "Forged vendor", Amount: 5000, Purpose: "probe",
		InvoiceNo: "INV-9", InvoiceDate: "2026-07-01", ManagerID: mgrRow,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("forged vendor_id = %v, want ErrValidation", err)
	}
}

// TestListPaymentsScopeNarrowsToEnteredBy is the F-A-04 / F-G-003 store half:
// the `payment` data scope finally reads. "own" (and "assigned", which has no
// routed-to meaning on the ledger) narrow to rows the viewer entered; "all" is
// unrestricted and "" — the role editor's "None" — reads nothing, like
// requestWhere (fixwave rbac-2; the empty scope used to be unrestricted here).
func TestListPaymentsScopeNarrowsToEnteredBy(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	otherID, err := s.CreateUser(ctx, "second-entry@example.com", "Second Entry", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.UserByID(ctx, otherID)
	mine, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-05-05", Amount: 1100, VendorPayee: "Mine"})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := s.CreatePayment(ctx, other, PaymentInput{HeadID: headID, PaidOn: "2026-05-06", Amount: 2200, VendorPayee: "Theirs"})
	if err != nil {
		t.Fatal(err)
	}
	ids := func(rows []Payment) map[int64]bool {
		out := map[int64]bool{}
		for _, p := range rows {
			out[p.ID] = true
		}
		return out
	}
	for _, scope := range []string{"own", "assigned"} {
		rows, err := s.ListPayments(ctx, PaymentListOptions{Month: "2026-05", Scope: scope, ViewerID: actor.ID})
		if err != nil {
			t.Fatal(err)
		}
		got := ids(rows)
		if !got[mine] || got[theirs] || len(rows) != 1 {
			t.Fatalf("scope %q for actor = %+v, want only their own payment", scope, got)
		}
	}
	rows, err := s.ListPayments(ctx, PaymentListOptions{Month: "2026-05", Scope: ScopeAll, ViewerID: actor.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(rows); !got[mine] || !got[theirs] {
		t.Fatalf("scope all = %+v, want both payments", got)
	}
	rows, err = s.ListPayments(ctx, PaymentListOptions{Month: "2026-05", Scope: "", ViewerID: actor.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("empty scope = %+v, want no payments at all", ids(rows))
	}
}

// TestValidatePaymentRefusesFutureDateAgainstInjectedClock is F-D-06.
//
// The rule reads the injected clock when the caller supplies one, so a test can
// pin the day — and falls back to the server's local day when it does not, so the
// rule is enforced whether or not anybody remembered to wire a clock. The
// second half is the fix: the check shipped skipping on a zero Now, no
// production caller ever set the field, and the guard therefore refused nothing
// for a whole release. The last block below is what pins that, and it is the
// exact assertion this test used to make in reverse.
func TestValidatePaymentRefusesFutureDateAgainstInjectedClock(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, headID := seedActorAndHead(t, s, ctx)
	clock := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)

	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-07-28", Amount: 1000, Now: clock}); !errors.Is(err, ErrValidation) {
		t.Fatalf("tomorrow's payment = %v, want ErrValidation", err)
	}
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2029-12-31", Amount: 1000, Now: clock}); !errors.Is(err, ErrValidation) {
		t.Fatalf("far-future payment = %v, want ErrValidation", err)
	}
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-07-27", Amount: 1000, Now: clock}); err != nil {
		t.Fatalf("today's payment: %v", err)
	}
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: "2026-07-26", Amount: 1000, Now: clock}); err != nil {
		t.Fatalf("yesterday's payment: %v", err)
	}
	// The settlement path enforces the same rule and leaves the reservation
	// standing so the accountant can correct the date.
	mgrID, err := s.CreateUser(ctx, "clock-mgr@example.com", "Clock Manager", "hash", "admin", true)
	if err != nil {
		t.Fatal(err)
	}
	reqRow, err := s.CreateUser(ctx, "clock-req@example.com", "Clock Requester", "hash", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	reqID := seedApprovedRequest(t, s, ctx, 41, reqRow, mgrID, headID, 5000, 5000)
	if err := s.ReserveRequest(ctx, actor, reqID); err != nil {
		t.Fatal(err)
	}
	if _, err := historicalSettlement(s, ctx, actor, reqID, PaymentInput{HeadID: headID, PaidOn: "2027-01-01", Amount: 5000, Now: clock}, "settled", "", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("future-dated settlement = %v, want ErrValidation", err)
	}
	// No clock at all — the shape every production caller uses. The rule must
	// still bite, because a guard that switches itself off when the caller
	// forgets to inject a clock guards nothing, which is what F-D-06 found.
	// Both dates are relative to the real day, so this cannot rot into a test
	// that passes only until the calendar catches up with a literal.
	localToday := time.Now()
	tomorrow := localToday.AddDate(0, 0, 1).Format("2006-01-02")
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: tomorrow, Amount: 1000}); !errors.Is(err, ErrValidation) {
		t.Fatalf("clockless payment dated %s = %v, want ErrValidation — the default must be enforcement, not a skip", tomorrow, err)
	}
	yesterday := localToday.AddDate(0, 0, -1).Format("2006-01-02")
	if _, err := s.CreatePayment(ctx, actor, PaymentInput{HeadID: headID, PaidOn: yesterday, Amount: 1000}); err != nil {
		t.Fatalf("clockless payment dated %s = %v; the past is not what this rule refuses", yesterday, err)
	}
}

// TestAddAttachmentRefusesLinkedPayment closes F-D-08: a linked payment is the
// request's outcome and the screen calls it read-only; edit and void already
// refuse it, and a new document now gets the same answer.
func TestAddAttachmentRefusesLinkedPayment(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)
	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	payID, err := historicalSettlement(s, ctx, acc, reqID, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 500000}, "settled", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddAttachment(ctx, acc, payID, "late-advice.pdf", "/tmp/late-advice.pdf", "application/pdf", 9)
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "linked") {
		t.Fatalf("attachment on linked payment = %v, want ErrValidation naming the linkage", err)
	}
	atts, err := s.Attachments(ctx, payID)
	if err != nil || len(atts) != 0 {
		t.Fatalf("attachment written despite refusal: %+v, %v", atts, err)
	}
}

// TestRequestAttachmentByIDAndAttachmentWithPayment pins the store surface
// Wave 3 builds the F-A-01 / F-A-05 ownership checks on: request documents
// resolve from their own table, and a payment attachment resolves to its
// payment (and through Payment.RequestID to its request) in one call.
func TestRequestAttachmentByIDAndAttachmentWithPayment(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acc, req, mgrID, headID := seedRequestParty(t, s, ctx)
	reqID := seedApprovedRequest(t, s, ctx, 1, req.ID, mgrID, headID, 500000, 500000)

	docID, err := s.AddRequestAttachment(ctx, req, reqID, AttachmentInput{OriginalName: "invoice-mine.txt", StoredPath: "/tmp/invoice-mine.txt", MimeType: "text/plain", SizeBytes: 12})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := s.RequestAttachmentByID(ctx, docID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.ID != docID || doc.RequestID != reqID || doc.OriginalName != "invoice-mine.txt" || doc.StoredPath != "/tmp/invoice-mine.txt" {
		t.Fatalf("request attachment = %+v", doc)
	}
	if _, err := s.RequestAttachmentByID(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing request attachment = %v, want ErrNotFound", err)
	}

	if err := s.ReserveRequest(ctx, acc, reqID); err != nil {
		t.Fatal(err)
	}
	payID, err := historicalSettlement(s, ctx, acc, reqID, PaymentInput{HeadID: headID, PaidOn: "2026-06-15", Amount: 500000},
		"settled", "", &AttachmentInput{OriginalName: "advice.pdf", StoredPath: "/tmp/advice.pdf", MimeType: "application/pdf", SizeBytes: 9})
	if err != nil {
		t.Fatal(err)
	}
	atts, err := s.Attachments(ctx, payID)
	if err != nil || len(atts) != 1 {
		t.Fatalf("attachments = %+v, %v", atts, err)
	}
	att, pay, err := s.AttachmentWithPayment(ctx, atts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if att.ID != atts[0].ID || att.PaymentID != payID || pay.ID != payID {
		t.Fatalf("AttachmentWithPayment = %+v / %+v", att, pay)
	}
	if pay.RequestID == nil || *pay.RequestID != reqID {
		t.Fatalf("payment behind the attachment lost its request: %+v", pay)
	}
	if _, _, err := s.AttachmentWithPayment(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing payment attachment = %v, want ErrNotFound", err)
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

// F-G-034 — the profile, the role assignment and the default approver used to be
// three store calls with three transactions, so a save refused by the second
// left the first one's rename committed.
func TestSaveUserIsAtomicAcrossProfileRolesAndApprover(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor := newRoleActor(t, s, ctx)
	subject, err := s.CreateUser(ctx, "envelope@example.com", "Original Name", "hash", "data_entry", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	roleID, err := s.CreateRole(ctx, actor, "Envelope", "")
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	before, err := s.UserRoles(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}

	// A role id that does not exist must roll the whole save back.
	err = s.SaveUser(ctx, actor, UserSaveInput{ID: subject, Name: "Renamed By A Failure",
		Role: "data_entry", Active: true, RoleIDs: []int64{roleID, 999999}})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("SaveUser with an unknown role = %v, want ErrValidation", err)
	}
	u, err := s.UserByID(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Original Name" {
		t.Fatalf("a refused save committed the rename: %q", u.Name)
	}
	after, err := s.UserRoles(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("a refused save changed the role assignment: %d roles, was %d", len(after), len(before))
	}

	// Self-approval is refused before anything is written, as it was by
	// SetUserDefaultApprover.
	err = s.SaveUser(ctx, actor, UserSaveInput{ID: subject, Name: "Also Not Renamed",
		Role: "data_entry", Active: true, DefaultApproverID: subject})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("SaveUser with a self-approver = %v, want ErrValidation", err)
	}
	if u, _ := s.UserByID(ctx, subject); u.Name != "Original Name" {
		t.Fatalf("a refused self-approver save committed the rename: %q", u.Name)
	}

	// And the accepted save applies all three parts together.
	if err := s.SaveUser(ctx, actor, UserSaveInput{ID: subject, Name: "Renamed On Purpose",
		Role: "data_entry", Active: true, RoleIDs: []int64{roleID}, DefaultApproverID: actor.ID}); err != nil {
		t.Fatalf("SaveUser: %v", err)
	}
	u, err = s.UserByID(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Renamed On Purpose" || u.DefaultApproverID != actor.ID {
		t.Fatalf("SaveUser did not apply the profile and approver: %+v", u)
	}
	roles, err := s.UserRoles(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].ID != roleID {
		t.Fatalf("SaveUser did not replace the role assignment: %+v", roles)
	}
	// One audit row for one submit, and it names both sides.
	trail, err := s.Audit(ctx, "user", subject, 20)
	if err != nil {
		t.Fatal(err)
	}
	var saved int
	for _, entry := range trail {
		if entry.Action == "update" && strings.Contains(entry.Summary, "Renamed On Purpose") {
			saved++
		}
	}
	if saved != 1 {
		t.Fatalf("audit rows for one user save = %d, want 1: %+v", saved, trail)
	}
}

// SaveUser must keep the guard UpdateUser had: the last active administrator
// cannot demote or deactivate themselves through the new envelope either.
func TestSaveUserKeepsTheLastAdministrator(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor := newRoleActor(t, s, ctx)
	err := s.SaveUser(ctx, actor, UserSaveInput{ID: actor.ID, Name: actor.Name, Role: "data_entry", Active: true})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("demoting the last administrator = %v, want ErrValidation", err)
	}
	if u, _ := s.UserByID(ctx, actor.ID); u.Role != "admin" {
		t.Fatalf("the last administrator was demoted anyway: %+v", u)
	}
}
