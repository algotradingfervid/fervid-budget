package app

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

// Phase 6: the eleven pre-existing screens come onto the design system.
//
// A wide <table> is unusable on a phone — it either overflows the viewport or
// gets squeezed unreadably. .t-cards restacks each row into a labelled card
// below 860px, reading the field name from data-label. This asserts both halves
// of that contract on every list screen the app has, because a missing
// data-label renders an unlabelled value that no desktop test would ever see.
func TestEveryListScreenRestacksOnMobile(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Phase6")
	admin := s.admin()
	if _, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{
		HeadID: headID, PaidOn: "2026-07-05", Amount: 250000, VendorPayee: "Phase Six Vendor",
	}); err != nil {
		t.Fatal(err)
	}
	s.loginAdmin()

	for _, route := range []string{
		"/payments", "/months", "/reports/monthly", "/audit", "/backups",
		"/projects", "/heads", "/budgets",
	} {
		t.Run(route, func(t *testing.T) {
			resp := s.request(http.MethodGet, route, nil, "")
			requireStatus(t, resp, http.StatusOK)
			body := responseBody(t, resp)
			if !strings.Contains(body, `class="t-cards"`) && !strings.Contains(body, `class="t-cards `) {
				t.Fatalf("%s still renders a table that cannot restack on a phone", route)
			}
			assertTCardsLabelled(t, body)
		})
	}
}

// The variance grid is the widest screen in the app and cannot restack into
// cards; it gets a project accordion instead, driven by the same data.
func TestVarianceGridHasAMobileAccordion(t *testing.T) {
	s := newAppTestServer(t)
	s.seedHead("Phase6Grid")
	s.loginAdmin()
	body := s.get("/grid")
	mustContain(t, "variance grid mobile accordion markup", body, `class="acc m-only"`, "acc-item", "acc-head", "acc-body", "ah-main", "ah-amt", "hr-figs")
	// One markup, both devices: the matrix is desktop-only, the accordion mobile-only.
	if !strings.Contains(body, "m-only") || !strings.Contains(body, "d-only") {
		t.Fatal("the grid must carry both the desktop matrix and the mobile accordion")
	}
}

// An error page with a sidebar invites the reader to click deeper into an app
// that just failed. It renders chrome-less, like login.
func TestErrorPageRendersWithoutChrome(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
	resp := s.request(http.MethodGet, "/definitely-not-a-route", nil, "")
	requireStatus(t, resp, http.StatusNotFound)
	body := responseBody(t, resp)
	for _, forbidden := range []string{"<aside", `class="tabbar"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("the error page renders %s; it must be chrome-less", forbidden)
		}
	}
	if !strings.Contains(body, `class="error-state"`) {
		t.Fatal("the error page does not use the design-system error state")
	}
}

// An empty screen is an invitation to act, not a dead end. Every list screen
// that can be empty offers the next step.
func TestEmptyStatesOfferAnAction(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
	for _, tc := range []struct{ route, want string }{
		{"/payments?month=2099-01", "/payments/new"},
		{"/reports/heads?from=2099-01&to=2099-01", "/budgets"},
		{"/budgets?month=2099-01", "empty"},
	} {
		body := s.get(tc.route)
		if !strings.Contains(body, `class="empty"`) {
			t.Fatalf("%s has no empty state at all", tc.route)
		}
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s empty state offers no way forward (looked for %q)", tc.route, tc.want)
		}
	}
}

// F-F-01 — the per-event sheets on /admin/notifications carried no `hidden`,
// unlike every other .overlay, so all of them laid out full-viewport at once and
// only the last was reachable by a pointer.
// F-G-004 / F-C-06 — the audit filters could not reach payment_request.
// F-D-07 — the Accounts queue shipped only the mobile filter form.
// F-F-05 — the notification centre had no desktop entry point.
func TestScreensThatWereUnusableAsRendered(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()

	rules := s.get("/admin/notifications")
	sheets := strings.Count(rules, `<div class="overlay" id="ev-`)
	hidden := strings.Count(rules, `<div class="overlay" id="ev-`) - strings.Count(rules, `<div class="overlay" id="ev-{{`)
	if sheets == 0 {
		t.Fatal("no per-event sheets are rendered at all")
	}
	if strings.Count(rules, ` hidden>`) < hidden {
		t.Fatalf("%d event sheets and fewer hidden attributes — the stack covers every control beneath it", sheets)
	}
	for _, event := range notify.AllEvents {
		if !strings.Contains(rules, fmt.Sprintf(`<div class="overlay" id="ev-%s" hidden>`, event)) {
			t.Fatalf("the %s sheet is not hidden", event)
		}
	}

	audit := s.get("/audit")
	for _, want := range []string{`value="payment_request"`, `value="approve"`, `value="settle"`, `value="reassign"`} {
		if !strings.Contains(audit, want) {
			t.Errorf("the audit filters cannot ask for %s", want)
		}
	}

	queue := s.get("/accounts-queue")
	if !strings.Contains(queue, `<form class="toolbar" method="get" action="/accounts-queue">`) {
		t.Error("the Accounts queue still ships only the mobile filter form")
	}
	if !strings.Contains(queue, `id="queue-q"`) {
		t.Error("the desktop search input is gone")
	}

	dash := s.get("/")
	if !strings.Contains(dash, `class="sidebar"`) || !strings.Contains(dash, `href="/notifications"`) {
		t.Error("the sidebar has no entry point to the notification centre")
	}
}

// The `search` half of `class="field search"` had no rule anywhere in
// fervid-ds.css — the only match was `.gridhead .toolbar .search-label`, a
// different class — so five toolbars carried a token that rendered as nothing.
// PROGRESS.md records this failure mode as a standing trap, and it is invisible
// to every other test in this repository: markup with a dead class renders
// perfectly, just not as designed. The rule is the approved stylesheet's own
// (mockups/mockup.css), ported.
func TestToolbarSearchTokenHasARuleBehindIt(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "fervid-ds.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(templateSource(t)), `class="field search"`) {
		t.Skip("no template uses the search token any more")
	}
	// Comments are stripped first, or the comment ABOVE the rule — which quotes
	// the selector it is explaining — satisfies the search all by itself. That
	// false pass was observed while writing this test, not guessed at.
	live := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAll(css, nil)
	if !regexp.MustCompile(`\.toolbar\s+\.search\s*\{`).Match(live) {
		t.Fatal("templates carry `class=\"field search\"` and the stylesheet has no `.toolbar .search` rule")
	}
}

// recoverables-6. At 390px every empty spacer cell in a t-cards tfoot became a
// blank stripe in the total card. The spacers are desktop-only.
func TestTCardsTotalRowsCarryNoEmptyMobileCells(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
	for _, path := range []string{"/recoverables/list", "/vendors"} {
		body := s.get(path)
		tfoot := cut(body, "<tfoot>", "</tfoot>")
		if tfoot == "" {
			t.Fatalf("%s has no total row", path)
		}
		if strings.Contains(tfoot, `<td data-label=""></td>`) {
			t.Fatalf("%s's total row still has empty cells that restack into blank stripes on a phone: %s", path, tfoot)
		}
	}
}
