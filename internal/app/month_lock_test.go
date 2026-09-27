package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// F-G-021 — a request could be raised and approved into a locked month, becoming
// an approved obligation nobody could pay until the month reopened, and the
// accountant only discovered it at the settlement.
func TestARequestCannotBeApprovedIntoALockedMonth(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Locked approval")
	requester := s.seedRequester("locked@example.test", "Locked Requester", "LockedPass1234")
	id := s.seedPendingRequest(996, requester.ID, admin.ID, headID, 100000)
	if _, err := s.st.DB().ExecContext(s.ctx, `UPDATE payment_requests SET needed_by='2027-11-20' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.st.LockMonth(s.ctx, admin, "2027-11", "Year end"); err != nil {
		t.Fatal(err)
	}

	s.loginAdmin()
	resp := s.postForm(fmt.Sprintf("/requests/%d/approve", id), url.Values{"approved_amount": {"1,000.00"}})
	requireStatus(t, resp, http.StatusConflict)
	if body := responseBody(t, resp); !strings.Contains(body, "2027-11 is locked") {
		t.Fatalf("the refusal does not name the locked month: %s", firstLines(body))
	}
	req, err := s.st.Request(s.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if req.Status != "pending" {
		t.Fatalf("status = %q after a refused approval, want pending", req.Status)
	}

	// Reopen it and the identical approval succeeds, which proves the refusal was
	// the lock and nothing else.
	if err := s.st.UnlockMonth(s.ctx, admin, "2027-11", "Reopened"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/approve", id),
		url.Values{"approved_amount": {"1,000.00"}}), http.StatusSeeOther)
}

// F-G-019 / F-G-020 — locking a month redirected to /?month=, which has been the
// dashboard since Phase 4 and reads no month at all; and the payment entry
// screen carried no lock affordance, so the whole form was filled in first.
func TestTheLockedMonthFamilyConfirmsItselfAndWarnsInAdvance(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Lock family")
	requester := s.seedRequester("lockui@example.test", "Lock Requester", "LockUIPass1234")
	month := currentMonthForTest()

	s.loginAdmin()
	resp := s.postForm("/months/"+month+"/lock", url.Values{"reason": {"Audit lock"}})
	requireStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != "/grid?month="+month {
		t.Fatalf("lock redirect = %q, want /grid?month=%s — the dashboard confirms nothing", loc, month)
	}
	landing := s.get(resp.Header.Get("Location"))
	if !strings.Contains(landing, "Audit lock") {
		t.Fatal("the operator is still not shown the lock they just applied")
	}

	// The entry screen says so before the accountant types anything.
	id := s.seedApprovedRequest(997, requester.ID, admin.ID, headID, 100000)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", id), url.Values{}), http.StatusSeeOther)
	entry := s.get(fmt.Sprintf("/payments/new?request=%d", id))
	if !strings.Contains(entry, `class="locked"`) {
		t.Fatalf("the payment entry screen carries no lock warning: %s", firstLines(entry))
	}

	unlock := s.postForm("/months/"+month+"/unlock", url.Values{"reason": {"Reopened"}})
	requireStatus(t, unlock, http.StatusSeeOther)
	if loc := unlock.Header.Get("Location"); loc != "/grid?month="+month {
		t.Fatalf("unlock redirect = %q, want /grid?month=%s", loc, month)
	}
}

// F-G-020, remaining edge — the entry screen warned about the month it opened
// on and nothing else, so a locked month typed into "Paid on" afterwards was
// only met at submit. The date field re-asks GET /payments/lock-status on
// change, and that fragment is the banner for the month the date names.
func TestPaymentLockStatusFollowsTheDateField(t *testing.T) {
	s := newAppTestServer(t)
	admin, _ := s.seedHead("Lock status")
	requester := s.seedRequester("lockstatus@example.test", "Lock Status Requester", "LockStatusPass1234")
	if err := s.st.LockMonth(s.ctx, admin, "2027-11", "Year end"); err != nil {
		t.Fatal(err)
	}

	s.loginAdmin()
	fragment := func(query string) string {
		t.Helper()
		resp := s.request(http.MethodGet, "/payments/lock-status"+query, nil, "")
		requireStatus(t, resp, http.StatusOK)
		body := responseBody(t, resp)
		if !strings.Contains(body, `id="lock-banner"`) {
			t.Fatalf("lock-status %s did not return the swap target: %s", query, firstLines(body))
		}
		if strings.Contains(body, "<html") || strings.Contains(body, "side-nav") {
			t.Fatalf("lock-status %s rendered the whole page shell: %s", query, firstLines(body))
		}
		return body
	}

	locked := fragment("?paid_on=2027-11-20")
	for _, want := range []string{`class="locked"`, "2027-11 is locked"} {
		if !strings.Contains(locked, want) {
			t.Fatalf("a date in a locked month carries no %q: %s", want, firstLines(locked))
		}
	}
	for _, query := range []string{"?paid_on=2027-12-01", "?paid_on=not-a-date", "?paid_on=", ""} {
		if body := fragment(query); strings.Contains(body, `class="locked"`) || strings.Contains(body, "is locked") {
			t.Fatalf("lock-status %s shows a banner it should not: %s", query, firstLines(body))
		}
	}

	// Gated exactly like the form it serves: payment:create.
	outsider := newAppTestClient(t, s)
	outsider.login(requester.Email, "LockStatusPass1234")
	denied := outsider.request(http.MethodGet, "/payments/lock-status?paid_on=2027-11-20", nil, "")
	requireStatus(t, denied, http.StatusForbidden)
	_ = responseBody(t, denied)
}
