package store

import (
	"context"
	"strings"
	"testing"
)

// docs-1 / test-1: the design's as-built notes described a JSON importer of
// 11 projects and 83 heads that never shipped. This pins what --seed actually
// creates, so the notes and the code cannot drift apart unnoticed again.
func TestSeedCreatesTheAdminAndThreeSampleProjects(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	result, err := s.Seed(ctx, SeedOptions{AdminEmail: "admin@fervid.local", AdminPassword: "TestAdmin12345"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.AdminCreated || !result.SampleCreated || result.Projects != 3 || result.Heads != 9 || result.Budgets != 9 {
		t.Fatalf("seed result = %+v, want admin + 3 projects / 9 heads / 9 budgets", result)
	}
	admin, err := s.UserByEmail(ctx, "admin@fervid.local")
	if err != nil || !admin.Active {
		t.Fatalf("seeded admin = %+v, %v", admin, err)
	}
	rows, err := s.DB().Query(`SELECT p.name, COUNT(h.id), COALESCE(SUM(b.amount),0) FROM projects p
		JOIN heads h ON h.project_id=p.id LEFT JOIN budgets b ON b.head_id=h.id AND b.month='2026-06'
		GROUP BY p.id ORDER BY p.sort_order`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		var heads int
		var total int64
		if err := rows.Scan(&name, &heads, &total); err != nil {
			t.Fatal(err)
		}
		if heads != 3 || total <= 0 {
			t.Errorf("%s: %d heads, June budget %d", name, heads, total)
		}
		got = append(got, name)
	}
	if strings.Join(got, ",") != "Operations,People,Growth" {
		t.Fatalf("seeded projects = %v", got)
	}

	// A second --seed on a populated database adds nothing.
	again, err := s.Seed(ctx, SeedOptions{AdminEmail: "admin@fervid.local", AdminPassword: "TestAdmin12345"})
	if err != nil || again.AdminCreated || !again.SampleSkipped {
		t.Fatalf("re-seed = %+v, %v; want a no-op", again, err)
	}
}
