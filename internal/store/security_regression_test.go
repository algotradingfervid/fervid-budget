package store

import (
	"context"
	"errors"
	"fervidbudget/internal/money"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSecurityDelegationCannotEscalateOrResetAdministrator(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := newRoleActor(t, s, ctx)
	role, err := s.CreateRole(ctx, admin, "Delegated operator", "")
	if err != nil {
		t.Fatal(err)
	}
	grants := []Grant{{"user", "create"}, {"user", "edit"}, {"role", "edit"}, {"payment", "edit"}, {"payment", "view"}}
	if err = s.UpdateRolePermissions(ctx, admin, role, grants, []ScopeGrant{{"payment", "own"}}); err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateUserWithRoles(ctx, "delegated@test", "Delegated", "hash", "data_entry", true, []int64{role})
	if err != nil {
		t.Fatal(err)
	}
	actor, _ := s.UserByID(ctx, id)
	adminRoles, _ := s.UserRoles(ctx, admin.ID)
	adminRole := adminRoles[0].ID
	if err = s.UpdateRolePermissions(ctx, actor, role, append(grants, Grant{"backup", "create"}), nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("grant escalation: %v", err)
	}
	if err = s.UpdateRolePermissions(ctx, actor, role, grants, []ScopeGrant{{"payment", "all"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("scope escalation: %v", err)
	}
	if _, err = s.CreateManagedUser(ctx, actor, "escalated@test", "Escalated", "hash", true, []int64{adminRole}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("create admin: %v", err)
	}
	if err = s.SetUserRoles(ctx, actor, actor.ID, []int64{adminRole}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("assign admin: %v", err)
	}
	if err = s.SaveUser(ctx, actor, UserSaveInput{ID: admin.ID, Name: admin.Name, Role: "data_entry", Active: true, PasswordHash: "changed", RoleIDs: []int64{adminRole}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("reset higher account: %v", err)
	}
	if err = s.SaveUser(ctx, admin, UserSaveInput{ID: admin.ID, Name: admin.Name, Role: "admin", Active: true, RoleIDs: []int64{role}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("self-demotion via role ids: %v", err)
	}
}

func TestSecurityLegacyPaymentScopeAndMoneyCeiling(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin, head := seedActorAndHead(t, s, ctx)
	role, _ := s.CreateRole(ctx, admin, "Own payments", "")
	if err := s.UpdateRolePermissions(ctx, admin, role, []Grant{{"payment", "view"}, {"payment", "edit"}, {"payment", "void"}}, []ScopeGrant{{"payment", "own"}}); err != nil {
		t.Fatal(err)
	}
	uid, err := s.CreateUserWithRoles(ctx, "own@test", "Own", "hash", "data_entry", true, []int64{role})
	if err != nil {
		t.Fatal(err)
	}
	actor, _ := s.UserByID(ctx, uid)
	in := PaymentInput{HeadID: head, PaidOn: time.Now().Format("2006-01-02"), Amount: 10000, VendorPayee: "Private payee", PaymentMode: "cash"}
	id, err := s.CreatePayment(ctx, admin, in)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.VoidPayment(ctx, actor, id, "unauthorized"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("void out of scope: %v", err)
	}
	if err = s.UpdatePayment(ctx, actor, id, in); !errors.Is(err, ErrNotFound) {
		t.Fatalf("edit out of scope: %v", err)
	}
	in.Amount = money.MaxAmount + 1
	if _, err = s.CreatePayment(ctx, admin, in); !errors.Is(err, ErrValidation) {
		t.Fatalf("amount limit: %v", err)
	}
	if _, err = s.DB().Exec(`UPDATE payments SET amount=9223372036854775807 WHERE id=?`, id); err == nil {
		t.Fatal("database allowed overflow payload")
	}
	if _, err = s.Report(ctx, "0001-01", "9999-12", "monthly"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unbounded report: %v", err)
	}
	pid, _ := s.UpsertProject(ctx, 0, "Other", true, 0)
	if _, err = s.UpsertHead(ctx, head, pid, "Rent", "5", true, 0, admin); !errors.Is(err, ErrValidation) {
		t.Fatalf("reparent historical head: %v", err)
	}
}

func TestSecurityDecisionRevisionAndPreApprovalPayment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	account, requester, managerID, head := seedRequestParty(t, s, ctx)
	manager, _ := s.UserByID(ctx, managerID)
	id := seedApprovedRequest(t, s, ctx, 1, requester.ID, managerID, head, 10000, 10000)
	if _, err := s.DB().Exec(`UPDATE payment_requests SET status='pending',approved_at=NULL,approved_amount=NULL WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	req, _ := s.Request(ctx, id)
	revision, err := s.RequestEditRevision(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`UPDATE payment_requests SET purpose='Changed beneficiary instructions' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	for _, decision := range []func() error{func() error { return s.ApproveRequest(ctx, manager, id, 10000, "", revision) }, func() error { return s.ReturnRequest(ctx, manager, id, "correct it", revision) }, func() error { return s.RejectRequest(ctx, manager, id, "refused", revision) }} {
		if err = decision(); !errors.Is(err, ErrValidation) {
			t.Fatalf("stale decision: %v", err)
		}
	}
	req, _ = s.Request(ctx, id)
	revision, _ = s.RequestEditRevision(ctx, req)
	if err = s.ApproveRequest(ctx, manager, id, 10000, "", revision); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveRequest(ctx, account, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecordPaymentForRequest(ctx, account, id, PaymentInput{Amount: 10000, PaidOn: "1999-01-01", PaymentMode: "cash"}, "settled", "", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("payment predates approval: %v", err)
	}
}

func TestSecurityRecoveryLockedMonthAndRevokedNotification(t *testing.T) {
	s, actor, _, id := recoveryFixture(t)
	ctx := context.Background()
	if err := s.LockMonth(ctx, actor, "2026-02", "closed"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordRecovery(ctx, actor, id, recoveryInput("locked-security-recovery", 100)); !errors.Is(err, ErrLockedMonth) {
		t.Fatalf("locked recovery: %v", err)
	}
	nid, err := s.AddNotification(ctx, Notification{UserID: actor.ID, RequestID: &id, Title: "Sensitive amount"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Notification(ctx, actor.ID, nid); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`DELETE FROM user_roles WHERE user_id=?`, actor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Notification(ctx, actor.ID, nid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked notification: %v", err)
	}
	if count, err := s.UnreadNotificationCount(ctx, actor.ID); err != nil || count != 0 {
		t.Fatalf("revoked badge: %d %v", count, err)
	}
}

func TestSecurityPrivateDatabaseBackupAndSymlinkRejection(t *testing.T) {
	root := t.TempDir()
	dbpath := filepath.Join(root, "private.db")
	s, err := Open(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	info, err := os.Stat(dbpath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("database mode: %v %v", info, err)
	}
	source := filepath.Join(root, "attachments")
	os.Mkdir(source, 0700)
	secret := filepath.Join(root, "secret")
	os.WriteFile(secret, []byte("private"), 0600)
	os.Symlink(secret, filepath.Join(source, "link"))
	if err = copyTree(context.Background(), source, filepath.Join(root, "copy")); !errors.Is(err, ErrValidation) {
		t.Fatalf("backup copied symlink: %v", err)
	}
	if _, err = s.Seed(context.Background(), SeedOptions{}); err == nil {
		t.Fatal("default bootstrap password accepted")
	}
}
