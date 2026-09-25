package app

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// floodAudit writes one old "Created project Facilities" row and then n newer
// failed-login rows, the shape the audit-1 repro used.
func floodAudit(t *testing.T, s *appTestServer, n int) {
	t.Helper()
	if err := s.st.RecordAudit(s.ctx, store.AuditInput{ActorName: "Test Admin", Action: "create", EntityType: "project", Summary: "Created project Facilities"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.DB().Exec(`UPDATE audit_log SET created_at='2026-01-15 06:30:00' WHERE summary='Created project Facilities'`); err != nil {
		t.Fatal(err)
	}
	tx, err := s.st.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if _, err := tx.Exec(`INSERT INTO audit_log(actor_name,action,entity_type,summary) VALUES('Flood Bot','login_failed','user',?)`, fmt.Sprintf("Failed login attempt %04d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// audit-1: /audit read the newest 1000 rows, filtered action and actor in Go,
// then cut the list at 200 without saying so, and had no date filter at all.
// Every filter now runs in SQL over the whole log, and the rest is paged.
func TestAuditLogFiltersEveryRowAndPages(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	floodAudit(t, s, 1100)

	// Action and actor reach a row older than any 1000-row window.
	for _, path := range []string{"/audit?action=create", "/audit?actor=test+admin&action=create"} {
		body := responseBody(t, s.request(http.MethodGet, path, nil, ""))
		if !strings.Contains(body, "Created project Facilities") {
			t.Fatalf("%s cannot reach the old create row", path)
		}
	}

	// The unfiltered view says what it is not showing and offers the next page.
	first := responseBody(t, s.request(http.MethodGet, "/audit", nil, ""))
	if got := strings.Count(first, "Failed login attempt"); got != auditPageSize {
		t.Fatalf("first page rows = %d, want %d", got, auditPageSize)
	}
	total := 1100 + 2 // the flood, the Facilities row, and the admin's login
	if !strings.Contains(first, fmt.Sprintf("of %d", total)) {
		t.Fatalf("first page does not say how many rows there are (want \"of %d\")", total)
	}
	if !strings.Contains(first, fmt.Sprintf("offset=%d", auditPageSize)) || !strings.Contains(first, "Older") {
		t.Fatal("first page offers no way to the older rows")
	}
	// Walking the pages reaches every row, the oldest included.
	last := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/audit?offset=%d", (total-1)/auditPageSize*auditPageSize), nil, ""))
	if !strings.Contains(last, "Created project Facilities") {
		t.Fatal("the last page does not carry the oldest row")
	}
	if !strings.Contains(last, "Newer") {
		t.Fatal("the last page offers no way back")
	}
	// An offset past the end — a stale or hand-edited URL — lands on that same
	// last page, not on "Showing 0 of N" with a Newer link to another empty
	// page (audit-1 review).
	past := responseBody(t, s.request(http.MethodGet, "/audit?offset=5000", nil, ""))
	lastOffset := (total - 1) / auditPageSize * auditPageSize
	if !strings.Contains(past, "Created project Facilities") ||
		!strings.Contains(past, fmt.Sprintf("Showing %d–%d of %d", lastOffset+1, total, total)) {
		t.Fatalf("an offset past the end does not land on the last page (want rows %d–%d of %d)", lastOffset+1, total, total)
	}
	if strings.Contains(past, "Showing 0 of") {
		t.Fatal("an offset past the end still shows an empty page")
	}

	// Page links keep every filter, so a page boundary never widens the set.
	filtered := responseBody(t, s.request(http.MethodGet, "/audit?action=login_failed&actor=flood&from=2020-01-01&to=2100-12-31", nil, ""))
	for _, want := range []string{"action=login_failed", "actor=flood", "from=2020-01-01", "to=2100-12-31"} {
		if !strings.Contains(filtered, want+"&amp;offset=") && !strings.Contains(filtered, want+"&amp;") {
			t.Errorf("page link drops %s", want)
		}
	}
}

// The scoped reader's path pages in Go; it clamps an offset past the end the
// same way the SQL path does.
func TestPageAuditEntriesClampsAnOffsetPastTheEnd(t *testing.T) {
	entries := make([]store.AuditEntry, 2*auditPageSize+50)
	page := pageAuditEntries(entries, 9000)
	if page.Offset != 2*auditPageSize || len(page.Entries) != 50 || page.Truncated || page.Total != len(entries) {
		t.Fatalf("offset past the end = offset %d, %d rows, truncated %v, total %d; want the last page of 50",
			page.Offset, len(page.Entries), page.Truncated, page.Total)
	}
	if page := pageAuditEntries(entries, len(entries)); page.Offset != 2*auditPageSize || len(page.Entries) != 50 {
		t.Fatalf("offset exactly past the end = offset %d, %d rows", page.Offset, len(page.Entries))
	}
	if page := pageAuditEntries(nil, 9000); page.Offset != 0 || len(page.Entries) != 0 || page.Truncated {
		t.Fatalf("no rows at all = offset %d, %d rows, truncated %v", page.Offset, len(page.Entries), page.Truncated)
	}
	if page := pageAuditEntries(entries, -5); page.Offset != 0 || len(page.Entries) != auditPageSize || !page.Truncated {
		t.Fatalf("negative offset = offset %d, %d rows, truncated %v", page.Offset, len(page.Entries), page.Truncated)
	}
}

func TestAuditLogFiltersByDate(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	floodAudit(t, s, 3)

	form := responseBody(t, s.request(http.MethodGet, "/audit", nil, ""))
	for _, want := range []string{`type="date" name="from"`, `type="date" name="to"`} {
		if !strings.Contains(form, want) {
			t.Fatalf("the filter form has no %s control", want)
		}
	}

	// Timestamps are stored in UTC and shown in local time, so the day bounds
	// are local days: pick the day the Facilities row falls on locally.
	day := responseBody(t, s.request(http.MethodGet, "/audit?from="+facilitiesLocalDay(t, s)+"&to="+facilitiesLocalDay(t, s), nil, ""))
	if !strings.Contains(day, "Created project Facilities") || strings.Contains(day, "Failed login attempt") {
		t.Fatal("a one-day range does not select exactly that day's rows")
	}
	if !strings.Contains(day, `value="`+facilitiesLocalDay(t, s)+`"`) {
		t.Fatal("the chosen dates are not kept in the form")
	}
	after := responseBody(t, s.request(http.MethodGet, "/audit?from=2026-02-01", nil, ""))
	if strings.Contains(after, "Created project Facilities") || !strings.Contains(after, "Failed login attempt") {
		t.Fatal("from= does not exclude earlier rows")
	}
	before := responseBody(t, s.request(http.MethodGet, "/audit?to=2026-01-31", nil, ""))
	if !strings.Contains(before, "Created project Facilities") || strings.Contains(before, "Failed login attempt") {
		t.Fatal("to= does not exclude later rows")
	}
}

func facilitiesLocalDay(t *testing.T, s *appTestServer) string {
	t.Helper()
	entries, err := s.st.Audit(s.ctx, "project", 0, 1)
	if err != nil || len(entries) != 1 {
		t.Fatalf("Facilities row: %v %v", entries, err)
	}
	return entries[0].CreatedAt.Local().Format("2006-01-02")
}
