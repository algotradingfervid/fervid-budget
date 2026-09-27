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

// A user who administers users and roles through a delegated role cannot give
// anyone — themselves included — more than their own role holds, and cannot
// touch an administrator's account. An administrator cannot demote themselves
// through the role list either.
func TestSecurityDelegationCannotEscalateOrResetAdministrator(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := newRoleActor(t, s)
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
	t.Run("cannot add a grant it does not hold", func(t *testing.T) {
		if err = s.UpdateRolePermissions(ctx, actor, role, append(grants, Grant{"backup", "create"}), nil); !errors.Is(err, ErrForbidden) {
			t.Fatalf("grant escalation: %v", err)
		}
	})
	t.Run("cannot widen a data scope", func(t *testing.T) {
		if err = s.UpdateRolePermissions(ctx, actor, role, grants, []ScopeGrant{{"payment", "all"}}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("scope escalation: %v", err)
		}
	})
	t.Run("cannot create an administrator", func(t *testing.T) {
		if _, err = s.CreateManagedUser(ctx, actor, "escalated@test", "Escalated", "hash", true, []int64{adminRole}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("create admin: %v", err)
		}
	})
	t.Run("cannot assign itself the administrator role", func(t *testing.T) {
		if err = s.SetUserRoles(ctx, actor, actor.ID, []int64{adminRole}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("assign admin: %v", err)
		}
	})
	t.Run("cannot reset an administrator's password", func(t *testing.T) {
		if err = s.SaveUser(ctx, actor, UserSaveInput{ID: admin.ID, Name: admin.Name, Role: "data_entry", Active: true, PasswordHash: "changed", RoleIDs: []int64{adminRole}}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("reset higher account: %v", err)
		}
	})
	t.Run("an administrator cannot demote themselves through role ids", func(t *testing.T) {
		if err = s.SaveUser(ctx, admin, UserSaveInput{ID: admin.ID, Name: admin.Name, Role: "admin", Active: true, RoleIDs: []int64{role}}); !errors.Is(err, ErrValidation) {
			t.Fatalf("self-demotion via role ids: %v", err)
		}
	})
}

// Unlinked (direct) payments respect the payment data scope, amounts are capped
// in the store and in the schema, reports refuse an unbounded range, and a head
// with history cannot be moved to another project.
func TestSecurityLegacyPaymentScopeAndMoneyCeiling(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin, head := seedActorAndHead(t, s)
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
	t.Run("own scope cannot void or edit another user's payment", func(t *testing.T) {
		if err = s.VoidPayment(ctx, actor, id, "unauthorized"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("void out of scope: %v", err)
		}
		if err = s.UpdatePayment(ctx, actor, id, in); !errors.Is(err, ErrNotFound) {
			t.Fatalf("edit out of scope: %v", err)
		}
	})
	t.Run("amount above the maximum is refused by the store and the schema", func(t *testing.T) {
		in.Amount = money.MaxAmount + 1
		if _, err = s.CreatePayment(ctx, admin, in); !errors.Is(err, ErrValidation) {
			t.Fatalf("amount limit: %v", err)
		}
		if _, err = s.DB().Exec(`UPDATE payments SET amount=9223372036854775807 WHERE id=?`, id); err == nil {
			t.Fatal("database allowed overflow payload")
		}
	})
	t.Run("report refuses an unbounded month range", func(t *testing.T) {
		if _, err = s.Report(ctx, "0001-01", "9999-12", "monthly"); !errors.Is(err, ErrValidation) {
			t.Fatalf("unbounded report: %v", err)
		}
	})
	t.Run("a head with history cannot move to another project", func(t *testing.T) {
		pid, _ := s.UpsertProject(ctx, 0, "Other", true, 0)
		if _, err = s.UpsertHead(ctx, head, pid, "Rent", "5", true, 0, admin); !errors.Is(err, ErrValidation) {
			t.Fatalf("reparent historical head: %v", err)
		}
	})
}

// A decision taken on a stale view of the request (its revision changed since
// the approver opened it) is refused, and a payment cannot be dated before the
// approval.
func TestSecurityDecisionRevisionAndPreApprovalPayment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	account, requester, managerID, head := seedRequestParty(t, s)
	manager, _ := s.UserByID(ctx, managerID)
	id := seedApprovedRequest(t, s, 1, requester.ID, managerID, head, 10000, 10000)
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

// A recovery cannot be recorded in a locked month, and a user who has lost all
// roles can no longer open or count their earlier notifications.
func TestSecurityRecoveryLockedMonthAndRevokedNotification(t *testing.T) {
	s, actor, _, id := recoveryFixture(t)
	ctx := context.Background()
	t.Run("recovery in a locked month is refused", func(t *testing.T) {
		if err := s.LockMonth(ctx, actor, "2026-02", "closed"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.RecordRecovery(ctx, actor, id, recoveryInput("locked-security-recovery", 100)); !errors.Is(err, ErrLockedMonth) {
			t.Fatalf("locked recovery: %v", err)
		}
	})
	t.Run("notifications close when every role is removed", func(t *testing.T) {
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
	})
}

// The database file is private to its owner (0600), a backup refuses to follow
// a symlink out of the attachment folder, and seeding refuses to run without an
// explicit admin password.
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
