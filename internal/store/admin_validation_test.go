package store

import (
	"context"
	"errors"
	"testing"
)

func TestDefaultApproverRequiresLiveApprovalGrantInBothWriters(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	admin := newRoleActor(t, s, ctx)
	subject, _ := s.CreateUser(ctx, "subject@qa.test", "Subject", "hash", "data_entry", true)
	candidate, _ := s.CreateUser(ctx, "candidate@qa.test", "Candidate", "hash", "data_entry", true)
	role, err := s.CreateRole(ctx, admin, "Custom Decider", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateRolePermissions(ctx, admin, role, []Grant{{Resource: "approval", Action: "approve"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.SetUserRoles(ctx, admin, candidate, []int64{role}); err != nil {
		t.Fatal(err)
	}
	if err = s.SetUserDefaultApprover(ctx, admin, subject, candidate); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveUser(ctx, admin, UserSaveInput{ID: subject, Name: "Valid rename", Role: "data_entry", Active: true, DefaultApproverID: candidate}); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateRolePermissions(ctx, admin, role, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.SetUserDefaultApprover(ctx, admin, subject, candidate); !errors.Is(err, ErrValidation) {
		t.Fatalf("revoked grant accepted: %v", err)
	}
	if err = s.SaveUser(ctx, admin, UserSaveInput{ID: subject, Name: "Must not commit", Role: "data_entry", Active: true, DefaultApproverID: candidate}); !errors.Is(err, ErrValidation) {
		t.Fatalf("SaveUser accepted revoked grant: %v", err)
	}
	after, _ := s.UserByID(ctx, subject)
	if after.Name != "Valid rename" || after.DefaultApproverID != candidate {
		t.Fatalf("rejected save partially wrote: %+v", after)
	}
	if err = s.SetUserDefaultApprover(ctx, admin, subject, 0); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationRecipientValidationIsAtomic(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	admin := newRoleActor(t, s, ctx)
	before, err := s.NotificationSetting(ctx, "request_submitted")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ to, cc string }{{"not-an-email", ""}, {"", "valid@example.test, missing-at-sign"}, {"a@x.test,,b@y.test", ""}, {"Person <p@x.test>", ""}, {"a@x.test\r\nBcc: b@y.test", ""}} {
		in := before
		in.ToRecipients = tc.to
		in.CcRecipients = tc.cc
		in.SubjectTemplate = "must not commit"
		if err := s.SetNotificationSetting(ctx, admin, in); !errors.Is(err, ErrValidation) {
			t.Fatalf("invalid recipients accepted: %+v %v", tc, err)
		}
		after, _ := s.NotificationSetting(ctx, in.Event)
		if after.ToRecipients != before.ToRecipients || after.CcRecipients != before.CcRecipients || after.SubjectTemplate != before.SubjectTemplate {
			t.Fatal("partial rule write")
		}
	}
	in := before
	in.ToRecipients = "first+ap@example.test, second@test"
	in.CcRecipients = "cc@example.test"
	if err := s.SetNotificationSetting(ctx, admin, in); err != nil {
		t.Fatal(err)
	}
	after, _ := s.NotificationSetting(ctx, in.Event)
	if after.ToRecipients != in.ToRecipients || after.CcRecipients != in.CcRecipients {
		t.Fatal("valid recipients changed")
	}
}
