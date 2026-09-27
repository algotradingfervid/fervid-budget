package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRequestRevisionRejectsSameSecondStaleAttachmentResubmitAtomically(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	requester, manager, head := seedRequestActors(t, s, ctx)
	in := RequestInput{Treatment: "budget", Type: "reimbursement", ShortTitle: "Original", ProjectID: 1, HeadID: head, Amount: 12345, Purpose: "Original purpose", ManagerID: manager.ID, ExpenseDate: "2026-09-26"}
	id, err := s.CreateRequest(ctx, requester, in)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ReturnRequest(ctx, manager, id, "Correct the details"); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Request(ctx, id)
	revision, err := s.RequestEditRevision(ctx, before)
	if err != nil {
		t.Fatal(err)
	}
	winner := in
	winner.ShortTitle = "Newer correction"
	if err = s.EditRequest(ctx, requester, id, RequestEdit{Input: winner, Revision: revision}); err != nil {
		t.Fatal(err)
	}
	// Force equal timestamps: wall-clock second precision cannot protect this form.
	if _, err = s.DB().Exec("UPDATE payment_requests SET updated_at=? WHERE id=?", before.UpdatedAt.Format("2006-01-02 15:04:05"), id); err != nil {
		t.Fatal(err)
	}
	var auditBefore int
	s.DB().QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&auditBefore)
	stale := in
	stale.ShortTitle = "Stale overwrite"
	err = s.EditRequest(ctx, requester, id, RequestEdit{Input: stale, Revision: revision, Resubmit: true, Attachment: &AttachmentInput{OriginalName: "stale.pdf", StoredPath: "/tmp/stale.pdf", MimeType: "application/pdf", SizeBytes: 9}})
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("stale = %v", err)
	}
	after, _ := s.Request(ctx, id)
	atts, _ := s.RequestAttachments(ctx, id)
	var auditAfter int
	s.DB().QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&auditAfter)
	if after.ShortTitle != "Newer correction" || after.Status != "returned" || len(atts) != 0 || auditAfter != auditBefore {
		t.Fatalf("stale edit changed data: %+v attachments=%d audits=%d/%d", after, len(atts), auditBefore, auditAfter)
	}
	// Even a change-and-revert is a new generation, independent of timestamp/value equality.
	if err = s.UpdateRequest(ctx, requester, id, in); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec("UPDATE payment_requests SET updated_at=? WHERE id=?", before.UpdatedAt.Format("2006-01-02 15:04:05"), id); err != nil {
		t.Fatal(err)
	}
	if err = s.EditRequest(ctx, requester, id, RequestEdit{Input: stale, Revision: revision}); !errors.Is(err, ErrValidation) {
		t.Fatalf("reverted stale = %v", err)
	}
	current, _ := s.Request(ctx, id)
	fresh, _ := s.RequestEditRevision(ctx, current)
	if err = s.EditRequest(ctx, requester, id, RequestEdit{Input: stale, Revision: fresh, Resubmit: true}); err != nil {
		t.Fatalf("fresh revision: %v", err)
	}
}

func TestRecoveryUnicodeLimitsMatchHTMLMaxlength(t *testing.T) {
	s, u, _, id := recoveryFixture(t)
	ctx := context.Background()
	for i, tc := range []struct {
		name, ref, note string
		valid           bool
	}{
		{"Telugu", strings.Repeat("అ", 240), strings.Repeat("అ", 4000), true},
		{"Tamil", strings.Repeat("அ", 100), strings.Repeat("அ", 1500), true},
		{"Emoji boundary", strings.Repeat("😀", 120), strings.Repeat("😀", 2000), true},
		{"Reference over", strings.Repeat("అ", 241), "Evidence", false},
		{"Note over", "UTR", strings.Repeat("అ", 4001), false},
		{"Emoji over", "UTR", strings.Repeat("😀", 2001), false},
	} {
		in := recoveryInput(strings.Repeat("t", 20)+string(rune('a'+i)), 1)
		in.Reference = tc.ref
		in.Note = tc.note
		_, err := s.RecordRecovery(ctx, u, id, in)
		if tc.valid && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if !tc.valid && !errors.Is(err, ErrValidation) {
			t.Errorf("%s: expected validation, got %v", tc.name, err)
		}
	}
	var count int
	s.DB().QueryRow("SELECT COUNT(*) FROM recovery_events WHERE request_id=?", id).Scan(&count)
	if count != 3 {
		t.Fatalf("events=%d", count)
	}
}
