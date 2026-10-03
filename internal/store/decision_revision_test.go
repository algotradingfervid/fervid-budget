package store

import (
	"context"
	"errors"
	"testing"
)

// The ...AtRevision decision methods always check the revision: a blank one
// is refused and the request stays pending, and the current one lets the
// decision through.
func TestDecisionsAtRevisionRefuseABlankRevision(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	requester, manager, head := seedRequestActors(t, s)
	raise := func(title string) int64 {
		id, err := s.CreateRequest(ctx, requester, RequestInput{Treatment: "budget", Type: "reimbursement", ShortTitle: title, ProjectID: 1, HeadID: head, Amount: 10000, Purpose: "taxi", ManagerID: manager.ID, ExpenseDate: "2026-09-26"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	current := func(id int64) string {
		req, err := s.Request(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		rev, err := s.RequestEditRevision(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		return rev
	}
	decisions := []struct {
		name   string
		decide func(id int64, revision string) error
		want   string
	}{
		{"approve", func(id int64, rev string) error {
			return s.ApproveRequestAtRevision(ctx, manager, id, 10000, "", rev)
		}, "approved"},
		{"return", func(id int64, rev string) error {
			return s.ReturnRequestAtRevision(ctx, manager, id, "Fix it", rev)
		}, "returned"},
		{"reject", func(id int64, rev string) error {
			return s.RejectRequestAtRevision(ctx, manager, id, "No", rev)
		}, "rejected"},
	}
	for _, d := range decisions {
		id := raise(d.name)
		if err := d.decide(id, ""); !errors.Is(err, ErrValidation) {
			t.Fatalf("%s with a blank revision: err = %v, want ErrValidation", d.name, err)
		}
		if got, _ := s.Request(ctx, id); got.Status != "pending" {
			t.Fatalf("%s with a blank revision changed the status to %q", d.name, got.Status)
		}
		if err := d.decide(id, current(id)); err != nil {
			t.Fatalf("%s with the current revision: %v", d.name, err)
		}
		if got, _ := s.Request(ctx, id); got.Status != d.want {
			t.Fatalf("%s left status %q, want %q", d.name, got.Status, d.want)
		}
	}
}
