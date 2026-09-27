package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The "Paid on" field on the record-payment screen opens on today's date.
func TestRecordPaymentPaidOnDefaultsToToday(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Paid on default")
	requester := s.seedRequester("paidon@example.test", "Paid On Requester", "PaidOnPass1234")
	s.loginAdmin()
	id := s.seedApprovedRequest(996, requester.ID, admin.ID, headID, 100000)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", id), url.Values{}), http.StatusSeeOther)
	before := time.Now().Format("2006-01-02")
	entry := s.get(fmt.Sprintf("/payments/new?request=%d", id))
	after := time.Now().Format("2006-01-02")
	if !strings.Contains(entry, `name="paid_on" type="date" value="`+before+`"`) &&
		!strings.Contains(entry, `name="paid_on" type="date" value="`+after+`"`) {
		t.Fatalf("Paid on does not default to today (%s): %s", before, firstLines(entry))
	}
}
