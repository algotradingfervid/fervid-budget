package store

import (
	"context"
	"testing"
)

// REQ-DEF-01: later decisions replace the live note, but each historical event
// must retain the note its actor actually entered, in decision order.
func TestRequestThreadRetainsDecisionNotesAfterCancellation(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	req, mgr, headID := seedRequestActors(t, s, ctx)
	id, err := s.CreateRequest(ctx, req, RequestInput{Treatment: "budget", Type: "reimbursement",
		ShortTitle: "Historical notes", ProjectID: 1, HeadID: headID, Amount: 12345,
		Purpose: "Verify notes", ManagerID: mgr.ID, ExpenseDate: "2026-09-26"})
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		s.ApproveRequest(ctx, mgr, id, 12345, "First approval <script>note</script>"),
		s.RequestCancellation(ctx, req, id, "Please cancel this request"),
		s.DecideCancellation(ctx, mgr, id, false, "Declined pending evidence"),
		s.RequestCancellation(ctx, req, id, "New evidence confirms cancellation"),
		s.DecideCancellation(ctx, mgr, id, true, "Final cancellation accepted"),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	current, err := s.Request(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if current.DecisionReason != "Final cancellation accepted" {
		t.Fatalf("live note = %q", current.DecisionReason)
	}
	thread, err := s.RequestThread(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var notes []string
	for _, entry := range thread {
		if entry.Body != "" {
			notes = append(notes, entry.Action+": "+entry.Body)
		}
	}
	want := []string{"approve: First approval <script>note</script>", "cancel: Final cancellation accepted"}
	if len(notes) != len(want) {
		t.Fatalf("historical notes = %#v, want %#v", notes, want)
	}
	for i := range want {
		if notes[i] != want[i] {
			t.Errorf("note %d = %q, want %q", i, notes[i], want[i])
		}
	}
}

func TestRequestAuditNoteDoesNotBorrowStaleDecisionReason(t *testing.T) {
	for _, tc := range []struct{ name, action, entity, before, after, want string }{
		{"approval", "approve", "payment_request", `{}`, `{"DecisionReason":"  approved evidence  "}`, "approved evidence"},
		{"accept cancellation", "cancel", "payment_request", `{"Status":"cancellation_requested"}`, `{"DecisionReason":"accept evidence"}`, "accept evidence"},
		{"direct cancel keeps old approval", "cancel", "payment_request", `{"Status":"approved"}`, `{"DecisionReason":"old approval","CancelReason":"direct cancellation"}`, ""},
		{"other event", "reserve", "payment_request", `{}`, `{"DecisionReason":"old approval"}`, ""},
		{"payment event", "approve", "payment", `{}`, `{"DecisionReason":"unrelated"}`, ""},
		{"legacy missing snapshot", "approve", "payment_request", `{}`, "", ""},
		{"empty note", "approve", "payment_request", `{}`, `{"DecisionReason":" "}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RequestAuditNote(AuditEntry{Action: tc.action, EntityType: tc.entity, BeforeJSON: tc.before, AfterJSON: tc.after, Summary: "Manager cancelled request at the requester's asking"})
			if got != tc.want {
				t.Errorf("note = %q, want %q", got, tc.want)
			}
		})
	}
}
