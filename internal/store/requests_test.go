package store

import (
	"strings"
	"testing"
	"time"
)

func TestNextRequestNumberIsMonotonicPerYear(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	got := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		n, err := NextRequestNumber(tx, "2026")
		if err != nil {
			t.Fatalf("NextRequestNumber: %v", err)
		}
		got = append(got, n)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	want := []string{"PR-2026-000001", "PR-2026-000002", "PR-2026-000003"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("number[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	tx2, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback()
	n, err := NextRequestNumber(tx2, "2027")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(n, "PR-2027-000001") {
		t.Fatalf("new-year number = %q, want PR-2027-000001", n)
	}
}

// A20/D6: the Configuration numbering fieldset actually drives the format.
func TestNextRequestNumberHonoursConfiguredFormat(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DB().Exec(`UPDATE app_settings SET value='REQ' WHERE key='number_prefix'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE app_settings SET value='4' WHERE key='number_width'`); err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	n, err := NextRequestNumber(tx, "2026")
	if err != nil {
		t.Fatal(err)
	}
	if n != "REQ-2026-0001" {
		t.Fatalf("configured number = %q, want REQ-2026-0001", n)
	}
}

func TestRequestNumberYearSegmentModes(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	march := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	april := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if y, err := requestNumberYear(tx, march); err != nil || y != "2026" {
		t.Fatalf("calendar year = %q, %v; want 2026", y, err)
	}
	if _, err := tx.Exec(`UPDATE app_settings SET value='financial' WHERE key='number_year_mode'`); err != nil {
		t.Fatal(err)
	}
	if y, err := requestNumberYear(tx, march); err != nil || y != "2025-26" {
		t.Fatalf("financial year (March) = %q, %v; want 2025-26", y, err)
	}
	if y, err := requestNumberYear(tx, april); err != nil || y != "2026-27" {
		t.Fatalf("financial year (April) = %q, %v; want 2026-27", y, err)
	}
}
