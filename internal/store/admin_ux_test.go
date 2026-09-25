package store

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRoleAuditContainsNamedBeforeAfterGrantsAndScopes(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	actor := newRoleActor(t, s, ctx)
	id, err := s.CreateRole(ctx, actor, "Audit UX", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateRolePermissions(ctx, actor, id, []Grant{{"request", "view"}}, []ScopeGrant{{"request", "own"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateRolePermissions(ctx, actor, id, []Grant{{"request", "view"}, {"request", "create"}}, []ScopeGrant{{"request", "all"}}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Audit(ctx, "role", id, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if strings.Contains(entry.BeforeJSON, `"request":"own"`) && strings.Contains(entry.AfterJSON, `request:create`) && strings.Contains(entry.AfterJSON, `"request":"all"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing named diff: %+v", entries)
	}
}
func TestPasswordResetExpiresSingleUseAndUnlocks(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	u := newRoleActor(t, s, ctx)
	now := time.Now()
	if _, err := s.DB().Exec(`UPDATE users SET attempt_count=5,locked=? WHERE id=?`, now.Add(time.Hour), u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CreatePasswordReset(ctx, u.ID, "expired", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.ConsumePasswordReset(ctx, "expired", "new-hash", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired token: %v", err)
	}
	if err := s.CreatePasswordReset(ctx, u.ID, "new-token", now); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.ConsumePasswordReset(ctx, "new-token", "new-hash", now) }()
	}
	wg.Wait()
	close(results)
	successes, used := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrNotFound) {
			used++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || used != 1 {
		t.Fatalf("consumption successes=%d used=%d", successes, used)
	}
	got, err := s.UserByID(ctx, u.ID)
	if err != nil || got.PasswordHash != "new-hash" {
		t.Fatalf("password: %+v %v", got, err)
	}
	locked, _, err := s.LoginLocked(ctx, u.Email)
	if err != nil || locked {
		t.Fatalf("reset did not unlock: %v %v", locked, err)
	}
}
func TestPasswordResetThrottleLimitsAndRecovers(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	for i := 1; i <= 6; i++ {
		ok, err := s.AllowPasswordReset(t.Context(), []string{"email-hash", "ip-hash"}, now)
		if err != nil || ok != (i <= 5) {
			t.Fatalf("attempt%d: %v %v", i, ok, err)
		}
	}
	ok, err := s.AllowPasswordReset(t.Context(), []string{"email-hash", "ip-hash"}, now.Add(time.Hour+time.Second))
	if err != nil || !ok {
		t.Fatalf("after window: %v %v", ok, err)
	}
}

func TestVendorAuditRecordsContactChangesWithoutFalseBankChanges(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	actor := newRoleActor(t, s, ctx)
	input := validVendorInput()
	input.ContactPerson = "Old contact"
	input.Bank = &VendorBank{AccountNumber: "secret-account-77", UPIID: "secret@upi", IFSC: "SBIN0001234"}
	id, err := s.CreateVendor(ctx, actor, input)
	if err != nil {
		t.Fatal(err)
	}
	input.ContactPerson = "New contact"
	// Same bank values after the ordinary write normalization are not a bank edit.
	input.Bank.AccountNumber = " secret-account-77 "
	input.Bank.IFSC = "sbin0001234"
	perms := NewPermissionSet([]Grant{{"vendor", "edit"}, {"vendor_bank", "edit"}, {"vendor_bank", "view"}}, nil)
	if err = s.UpdateVendor(ctx, actor, id, input, perms); err != nil {
		t.Fatal(err)
	}
	var before, after string
	if err = s.DB().QueryRow(`SELECT before_json,after_json FROM audit_log WHERE entity_type='vendor' AND entity_id=? ORDER BY id DESC LIMIT 1`, id).Scan(&before, &after); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"contact_person":"Old contact"`, `"audit_version":2`} {
		if !strings.Contains(before, want) {
			t.Fatal("before missing", want)
		}
	}
	for _, want := range []string{`"contact_person":"New contact"`, `"bank_changed":false`} {
		if !strings.Contains(after, want) {
			t.Fatal("after missing", want)
		}
	}
	for _, secret := range []string{"secret-account-77", "secret@upi", "SBIN0001234"} {
		if strings.Contains(before+after, secret) {
			t.Fatal("bank audit leaked", secret)
		}
	}
	input.Bank.UPIID = "changed@upi"
	if err = s.UpdateVendor(ctx, actor, id, input, perms); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRow(`SELECT after_json FROM audit_log WHERE entity_type='vendor' AND entity_id=? ORDER BY id DESC LIMIT 1`, id).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after, `"bank_changed":true`) || strings.Contains(after, "changed@upi") {
		t.Fatal("bank change privacy/accuracy", after)
	}
}
