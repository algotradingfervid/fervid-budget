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
			if !strings.Contains(body, `class="t-cards"`) {
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
