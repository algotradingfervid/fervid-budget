package app

import (
	"net/http"
	"strings"
	"testing"

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
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{
		HeadID: headID, PaidOn: "2026-07-05", Amount: 250000, VendorPayee: "Phase Six Vendor",
	}); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)

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
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/grid", nil, ""))
	for _, want := range []string{`class="acc m-only"`, "acc-item", "acc-head", "acc-body", "ah-main", "ah-amt", "hr-figs"} {
		if !strings.Contains(body, want) {
			t.Fatalf("variance grid missing mobile accordion markup %q", want)
		}
	}
	// One markup, both devices: the matrix is desktop-only, the accordion mobile-only.
	if !strings.Contains(body, "m-only") || !strings.Contains(body, "d-only") {
		t.Fatal("the grid must carry both the desktop matrix and the mobile accordion")
	}
}

// An error page with a sidebar invites the reader to click deeper into an app
// that just failed. It renders chrome-less, like login.
func TestErrorPageRendersWithoutChrome(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
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
	s.login(s.cfg.AdminEmail, testAdminPassword)
	for _, tc := range []struct{ route, want string }{
		{"/payments?month=2099-01", "/payments/new"},
		{"/reports/heads?from=2099-01&to=2099-01", "/budgets"},
		{"/budgets?month=2099-01", "empty"},
	} {
		body := responseBody(t, s.request(http.MethodGet, tc.route, nil, ""))
		if !strings.Contains(body, `class="empty"`) {
			t.Fatalf("%s has no empty state at all", tc.route)
		}
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s empty state offers no way forward (looked for %q)", tc.route, tc.want)
		}
	}
}
