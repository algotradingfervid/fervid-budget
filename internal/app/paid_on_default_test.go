package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// test-1 (BV24): the only check that "Paid on" opens on today was an e2e regex
// for any date. This pins the value itself on the record-payment screen.
func TestRecordPaymentPaidOnDefaultsToToday(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Paid on default")
	requester := s.seedRequester("paidon@example.test", "Paid On Requester", "PaidOnPass1234")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	id := s.seedApprovedRequest(996, requester.ID, admin.ID, headID, 100000)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", id), url.Values{}), http.StatusSeeOther)
	before := time.Now().Format("2006-01-02")
	entry := responseBody(t, s.request(http.MethodGet, fmt.Sprintf("/payments/new?request=%d", id), nil, ""))
	after := time.Now().Format("2006-01-02")
	if !strings.Contains(entry, `name="paid_on" type="date" value="`+before+`"`) &&
		!strings.Contains(entry, `name="paid_on" type="date" value="`+after+`"`) {
		t.Fatalf("Paid on does not default to today (%s): %s", before, firstLines(entry))
	}
}
