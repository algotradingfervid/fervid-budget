package app

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// F-G-006 / F-G-007 / F-G-018 — the dashboard's own three disagreements with the
// screens it links to: a tile that omitted `returned` from a bucket that includes
// it, an "Approved, unclaimed" count with no on_hold test beside a queue metric
// of the same name that has one, and a work area built with Scope:"all" written
// into the call.
func TestDashboardAreasAgreeWithTheQueuesTheyLinkTo(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Dashboard truth")
	requester := s.seedRequester("dash@example.test", "Dash Requester", "DashPass12345")

	// One pending and one returned request: "In progress" links to bucket=open,
	// which includes returned.
	pending := s.seedPendingRequest(1301, requester.ID, admin.ID, headID, 100000)
	returned := s.seedPendingRequest(1302, requester.ID, admin.ID, headID, 110000)
	s.loginAdmin()
	requireStatus(t, s.submitScreen(fmt.Sprintf("/requests/%d/return", returned),
		url.Values{"comment": {"Add the invoice"}}), http.StatusSeeOther)
	_ = pending

	s.login("dash@example.test", "DashPass12345")
	tile := dashboardTile(t, s, "My open requests")
	bucket, err := s.st.CountRequests(s.ctx, store.RequestListOptions{Scope: "own", ViewerID: requester.ID, Bucket: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if tile != bucket {
		t.Fatalf("the In-progress tile reads %d and /requests?bucket=open holds %d", tile, bucket)
	}

	// The accounts area: hardwired scope, and no on_hold test.
	other := s.seedRequester("dash-other@example.test", "Other Dash", "OtherDashPass12")
	visible := s.seedApprovedRequest(1303, other.ID, admin.ID, headID, 120000)
	s.seedProbeUser("processor@example.test", "Processor Rao", "ProcessorPass12", "Narrow processor",
		[]store.Grant{{Resource: "payment", Action: "process"}, {Resource: "request", Action: "view"}},
		[]store.ScopeGrant{{Resource: "request", Scope: "own"}})
	s.login("processor@example.test", "ProcessorPass12")
	body := s.get("/")
	if strings.Contains(body, fmt.Sprintf("PR-2026-%06d", 1303)) {
		t.Fatal("the dashboard lists an approved request the caller cannot open")
	}
	_ = visible

	// With a hold placed, the tile drops exactly as the queue's metric does.
	s.loginAdmin()
	before := dashboardTile(t, s, "Approved, unclaimed")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/hold", visible),
		url.Values{"reason": {"Waiting on a reply"}}), http.StatusSeeOther)
	after := dashboardTile(t, s, "Approved, unclaimed")
	if after != before-1 {
		t.Fatalf("the Approved-unclaimed tile went %d → %d across a hold; the queue's metric of the same name drops by one", before, after)
	}
}

// tilePattern matches one dashboard metric: its label and its value.
var tilePattern = regexp.MustCompile(`(?s)<span class="metric-label">([^<]*)</span>\s*<span class="metric-value">([^<]*)</span>`)

// dashboardTile reads one metric straight off the rendered dashboard, so the
// test asserts the number a reader is shown rather than the one the handler
// computed.
func dashboardTile(t *testing.T, s *appTestServer, label string) int {
	t.Helper()
	body := responseBody(t, s.request(http.MethodGet, "/", nil, ""))
	for _, match := range tilePattern.FindAllStringSubmatch(body, -1) {
		if strings.TrimSpace(match[1]) != label {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(match[2]))
		if err != nil {
			t.Fatalf("tile %q reads %q, which is not a number", label, match[2])
		}
		return n
	}
	t.Fatalf("the dashboard has no %q tile: %s", label, firstLines(body))
	return 0
}
