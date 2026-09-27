package app

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

const urgencyAlert = `<div class="alert error" role="alert" aria-live="assertive" data-server-error-for="urgency_reason">say why this is urgent</div>`

// The shell ties the missing-urgency-reason alert to its field. It used to do
// that by comparing the message text with the store's wording; it now follows
// the store's sentinel error, and the rendered alert is unchanged.
func TestUrgencyReasonAlertIsTiedToItsFieldBySentinel(t *testing.T) {
	if !errors.Is(store.ErrUrgencyReasonRequired, store.ErrValidation) || friendly(store.ErrUrgencyReasonRequired) != "say why this is urgent" {
		t.Fatalf("sentinel must stay a validation error with the same sentence, got %q", friendly(store.ErrUrgencyReasonRequired))
	}

	s := newAppTestServer(t)
	_, head := s.seedHead("Urgency field")
	manager := seedSecondApprover(t, s)
	requester := s.seedRequester("urgency-field@example.test", "Urgency Requester", "RequesterPass123")
	s.login(requester.Email, "RequesterPass123")
	fields := url.Values{"type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Urgent taxi"}, "project_id": {"1"}, "head_id": {strconvFormat(head)}, "amount": {"123.45"}, "purpose": {"Site visit"}, "expense_date": {"2026-09-26"}, "manager_id": {strconvFormat(manager)}, "urgent": {"on"}, "urgency_reason": {"  "}}

	resp := s.postForm("/requests", fields)
	requireStatus(t, resp, http.StatusBadRequest)
	if body := responseBody(t, resp); !strings.Contains(body, urgencyAlert) {
		t.Fatalf("new-request refusal lost the field-bound alert:\n%s", body)
	}

	// Any other refusal renders the plain alert.
	other := url.Values{}
	for k, v := range fields {
		other[k] = v
	}
	other.Del("urgent")
	other.Set("purpose", "")
	resp = s.postForm("/requests", other)
	requireStatus(t, resp, http.StatusBadRequest)
	if body := responseBody(t, resp); strings.Contains(body, "data-server-error-for") || !strings.Contains(body, `<div class="alert error" role="alert" aria-live="assertive">`) {
		t.Fatalf("an unrelated refusal was tied to the urgency field:\n%s", body)
	}

	// The correction screen follows the same rule.
	id, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{Treatment: "budget", Type: "reimbursement", ShortTitle: "Correct me", ProjectID: 1, HeadID: head, Amount: 12345, Purpose: "Original", ManagerID: manager, ExpenseDate: "2026-09-26"})
	if err != nil {
		t.Fatal(err)
	}
	resp = s.submitScreen(fmt.Sprintf("/requests/%d/edit", id), fields)
	requireStatus(t, resp, http.StatusBadRequest)
	if body := responseBody(t, resp); !strings.Contains(body, urgencyAlert) {
		t.Fatalf("edit refusal lost the field-bound alert:\n%s", body)
	}
}
