package app

import (
	"fmt"
	"strings"
	"testing"
)

// F-G-014 — /requests ignored ?status= while its own CSV honoured it, so one URL
// described two different sets on the page and in the file. The export also had
// no bucket default where the screen defaulted to "open".
func TestRequestsListAndItsExportDescribeTheSameSet(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Status filter")
	requester := s.seedRequester("statuses@example.test", "Status Requester", "StatusPass1234")
	pending := s.seedPendingRequest(981, requester.ID, admin.ID, headID, 100000)

	s.loginAdmin()
	number := fmt.Sprintf("PR-2026-%06d", 981)
	// No bucket at all, which is where the defect lived: the screen's own default
	// bucket used to swallow the status.
	for _, path := range []string{
		"/requests?status=rejected", "/requests?bucket=all&status=rejected",
		"/requests/export.csv?status=rejected", "/requests/export.csv?bucket=all&status=rejected",
	} {
		if strings.Contains(s.get(path), number) {
			t.Fatalf("%s lists a pending request, so ?status= is being ignored", path)
		}
	}
	// Bare, both default to the open bucket and both find the pending request.
	screen := s.get("/requests")
	bare := s.get("/requests/export.csv")
	if !strings.Contains(screen, number) || !strings.Contains(bare, number) {
		t.Fatal("the screen and its export no longer share a default bucket")
	}
	_ = pending
}

// F-B-16 — the row query capped itself at 200 while the tab count beside it had
// no cap, so the All tab promised 214 and the list drew 200 in silence. The
// screen now says what it is not showing and the export carries every row.
func TestTheRequestsListNeverTruncatesInSilence(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Truncation")
	requester := s.seedRequester("bulk@example.test", "Bulk Requester", "BulkPass12345")
	const total = 205
	for i := 0; i < total; i++ {
		s.seedPendingRequest(1000+i, requester.ID, admin.ID, headID, 100000)
	}

	s.login("bulk@example.test", "BulkPass12345")
	body := s.get("/requests?bucket=all")
	if !strings.Contains(body, fmt.Sprintf("200 of %d shown", total)) {
		t.Fatalf("the list does not admit what it is not showing: %s", firstLines(body))
	}
	if !strings.Contains(body, "offset=200") {
		t.Fatal("the remaining rows are unreachable: no next page link")
	}

	csv := s.get("/requests/export.csv?bucket=all")
	rows := strings.Count(strings.TrimSpace(csv), "\n") // one header + one per row
	if rows != total {
		t.Fatalf("the export carries %d data rows, want every one of %d", rows, total)
	}
}
