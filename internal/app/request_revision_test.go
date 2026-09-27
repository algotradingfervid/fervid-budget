package app

import (
	"fervidbudget/internal/store"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func requestRevisionFromPage(t *testing.T, page string) string {
	t.Helper()
	m := regexp.MustCompile(`name="revision" value="([^"]+)"`).FindStringSubmatch(page)
	if len(m) != 2 {
		t.Fatal("edit form lacks revision")
	}
	return m[1]
}

func TestRequestEditRejectsStaleAndMissingRevisionAndKeepsOriginalOnRetry(t *testing.T) {
	s := newAppTestServer(t)
	_, head := s.seedHead("Revision QA")
	manager := seedSecondApprover(t, s)
	requester := s.seedRequester("revision@example.test", "Revision Requester", "RequesterPass123")
	in := store.RequestInput{Treatment: "budget", Type: "reimbursement", ShortTitle: "Original", ProjectID: 1, HeadID: head, Amount: 12345, Purpose: "Original purpose", ManagerID: manager, ExpenseDate: "2026-09-26"}
	id, err := s.st.CreateRequest(s.ctx, requester, in)
	if err != nil {
		t.Fatal(err)
	}
	s.login(requester.Email, "RequesterPass123")
	path := fmt.Sprintf("/requests/%d/edit", id)
	original := requestRevisionFromPage(t, responseBody(t, s.request(http.MethodGet, path, nil, "")))
	fields := url.Values{"csrf": {s.csrf()}, "revision": {original}, "type": {"reimbursement"}, "treatment": {"budget"}, "short_title": {"Winning correction"}, "project_id": {"1"}, "head_id": {strconvFormat(head)}, "amount": {"123.45"}, "purpose": {"Corrected purpose"}, "expense_date": {"2026-09-26"}, "manager_id": {strconvFormat(manager)}}
	post := func() *http.Response {
		return s.request(http.MethodPost, path, strings.NewReader(fields.Encode()), "application/x-www-form-urlencoded")
	}
	requireStatus(t, post(), http.StatusSeeOther)
	fields.Set("short_title", "Stale retained input")
	for attempt := 0; attempt < 2; attempt++ {
		resp := post()
		requireStatus(t, resp, http.StatusBadRequest)
		body := responseBody(t, resp)
		if !strings.Contains(body, "changed since") || !strings.Contains(body, "Stale retained input") {
			t.Fatal("stale form lacked retained corrections/error")
		}
		if got := requestRevisionFromPage(t, body); got != original {
			t.Fatalf("stale retry acquired fresh revision %q", got)
		}
	}
	fields.Del("revision")
	resp := post()
	requireStatus(t, resp, http.StatusBadRequest)
	if !strings.Contains(responseBody(t, resp), "form has expired") {
		t.Fatal("missing revision not explained")
	}
	current, _ := s.st.Request(s.ctx, id)
	if current.ShortTitle != "Winning correction" {
		t.Fatalf("stale overwrite: %s", current.ShortTitle)
	}
	fields.Set("revision", requestRevisionFromPage(t, responseBody(t, s.request(http.MethodGet, path, nil, ""))))
	fields.Set("short_title", "Reviewed current correction")
	requireStatus(t, post(), http.StatusSeeOther)
}

func TestRecoveryPrecisionRejectedWithoutLosingInput(t *testing.T) {
	s := newAppTestServer(t)
	_, head := s.seedHead("Recovery precision")
	id := seedRecoverable(t, s, head, "PR-PRECISION", "icd", "Precision Counterparty", "2026-06-30", "2026-02-01", 700000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	path := fmt.Sprintf("/recoverables/%d/events", id)
	fields := url.Values{"kind": {"return"}, "amount": {"1.001"}, "occurred_on": {"2026-02-02"}, "reference": {"PRECISION-REF"}, "note": {"Retain this evidence"}, "token": {"recovery-precision-retained-token"}}
	resp := s.postForm(path, fields)
	requireStatus(t, resp, http.StatusUnprocessableEntity)
	body := responseBody(t, resp)
	for _, want := range []string{`value="1.001"`, `value="PRECISION-REF"`, "Retain this evidence", "positive recovery amount with up to two decimal places"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
	var count int
	s.st.DB().QueryRow("SELECT COUNT(*) FROM recovery_events WHERE request_id=?", id).Scan(&count)
	if count != 0 {
		t.Fatalf("precision wrote %d events", count)
	}
	fields.Set("amount", "1.01")
	requireStatus(t, s.postForm(path, fields), http.StatusSeeOther)
	var amount int
	s.st.DB().QueryRow("SELECT amount FROM recovery_events WHERE request_id=?", id).Scan(&amount)
	if amount != 101 {
		t.Fatalf("corrected amount=%d", amount)
	}
}
