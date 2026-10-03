package store

import (
	"context"
	"testing"
)

// A new project or head is audited as "Created …" and a later change as
// "Updated …", so the audit log's wording agrees with its action filter.
func TestUpsertProjectAndHeadAuditCreatedThenUpdated(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s)

	projectID, err := s.UpsertProject(ctx, 0, "Audit Project", true, 2, actor)
	if err != nil {
		t.Fatalf("UpsertProject create: %v", err)
	}
	if _, err := s.UpsertProject(ctx, projectID, "Audit Project Renamed", true, 2, actor); err != nil {
		t.Fatalf("UpsertProject update: %v", err)
	}
	headID, err := s.UpsertHead(ctx, 0, projectID, "Audit Head", "7", true, 1, actor)
	if err != nil {
		t.Fatalf("UpsertHead create: %v", err)
	}
	if _, err := s.UpsertHead(ctx, headID, projectID, "Audit Head Renamed", "7", true, 1, actor); err != nil {
		t.Fatalf("UpsertHead update: %v", err)
	}

	// Both rows land within the same second, so their order is not asserted:
	// each action must simply carry the summary that names it.
	for _, tc := range []struct {
		entity string
		id     int64
		want   map[string]string // action -> summary
	}{
		{"project", projectID, map[string]string{"create": "Created project Audit Project", "update": "Updated project Audit Project Renamed"}},
		{"head", headID, map[string]string{"create": "Created head Audit Head", "update": "Updated head Audit Head Renamed"}},
	} {
		audits, err := s.Audit(ctx, tc.entity, tc.id, 10)
		if err != nil {
			t.Fatalf("Audit %s: %v", tc.entity, err)
		}
		if len(audits) != len(tc.want) {
			t.Fatalf("%s audit count = %d, want %d: %+v", tc.entity, len(audits), len(tc.want), audits)
		}
		got := map[string]string{}
		for _, a := range audits {
			got[a.Action] = a.Summary
		}
		for action, want := range tc.want {
			if got[action] != want {
				t.Errorf("%s %s summary = %q, want %q", tc.entity, action, got[action], want)
			}
		}
	}
}
