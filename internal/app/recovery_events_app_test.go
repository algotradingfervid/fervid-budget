package app

import (
	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestRecoveryHTTPWorkflowAndRefusedInputPreserved(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Recovery lifecycle")
	id := seedRecoverable(t, s, headID, "PR-REC-HTTP", "icd", "Beacon Infra", "2026-06-30", "2026-02-01", 700000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	path := fmt.Sprintf("/recoverables/%d", id)
	body := responseBody(t, s.request(http.MethodGet, path, nil, ""))
	if !strings.Contains(body, "Record recovery") {
		t.Fatal("missing recovery form")
	}
	fields := url.Values{"kind": {"return"}, "amount": {"2000"}, "occurred_on": {"2026-02-02"}, "reference": {"UTR-2000"}, "note": {"Bank statement February row 42"}, "token": {"recovery-http-event-token"}}
	resp := s.postForm(path+"/events", fields)
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	body = responseBody(t, s.request(http.MethodGet, path, nil, ""))
	for _, want := range []string{"₹5,000", "₹2,000", "UTR-2000", "2026-02-02", "Bank statement February row 42"} {
		if !strings.Contains(body, want) {
			t.Fatalf("detail missing %q", want)
		}
	}
	fields.Set("token", "recovery-http-overage-token")
	fields.Set("amount", "6000")
	resp = s.postForm(path+"/events", fields)
	requireStatus(t, resp, http.StatusUnprocessableEntity)
	body = responseBody(t, resp)
	for _, want := range []string{"Recovery was not saved", "outstanding balance", `value="6000"`, `value="UTR-2000"`, "Bank statement February row 42"} {
		if !strings.Contains(body, want) {
			t.Fatalf("refused form missing %q", want)
		}
	}
	csv := responseBody(t, s.request(http.MethodGet, "/recoverables/list.csv", nil, ""))
	if !strings.Contains(csv, "5000.00,7000.00,2000.00") {
		t.Fatalf("CSV balance columns %s", csv)
	}
	var count int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM recovery_events WHERE request_id=?`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("events %d", count)
	}
}

func TestRecoveryHTTPPermissionScopeAndCSRF(t *testing.T) {
	s := newAppTestServer(t)
	u, h := s.seedHead("Scoped recovery")
	id := seedRecoverable(t, s, h, "PR-REC-SCOPE", "icd", "Scoped Party", "2026-06-30", "2026-02-01", 700000)
	hash, err := auth.HashPassword("ScopedPassword123")
	if err != nil {
		t.Fatal(err)
	}
	role, err := s.st.CreateRole(s.ctx, u, "Recovery read only", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.UpdateRolePermissions(s.ctx, u, role, []store.Grant{{Resource: "recoverable_report", Action: "view"}}, []store.ScopeGrant{{Resource: "request", Scope: "all"}}); err != nil {
		t.Fatal(err)
	}
	uid, err := s.st.CreateUserWithRoles(s.ctx, "recover-read@example.test", "Recovery reader", hash, "data_entry", true, []int64{role})
	if err != nil {
		t.Fatal(err)
	}
	s.login("recover-read@example.test", "ScopedPassword123")
	path := fmt.Sprintf("/recoverables/%d", id)
	body := responseBody(t, s.request(http.MethodGet, path, nil, ""))
	if strings.Contains(body, `id="record-recovery"`) {
		t.Fatal("read-only user offered record form")
	}
	fields := url.Values{"kind": {"return"}, "amount": {"2000"}, "occurred_on": {"2026-02-02"}, "reference": {"UTR-2000"}, "note": {"Proof"}, "token": {"recovery-scope-event-token"}}
	resp := s.postForm(path+"/events", fields)
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
	// Grant the action but narrow the request scope: a valid CSRF POST still cannot
	// mutate another requester's balance.
	if err := s.st.UpdateRolePermissions(s.ctx, u, role, []store.Grant{{Resource: "recoverable_report", Action: "view"}, {Resource: "payment", Action: "create"}}, []store.ScopeGrant{{Resource: "request", Scope: "own"}}); err != nil {
		t.Fatal(err)
	}
	s.login("recover-read@example.test", "ScopedPassword123")
	resp = s.postForm(path+"/events", fields)
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
	// Own request but missing CSRF is forbidden at the route boundary.
	if _, err := s.st.DB().Exec(`UPDATE payment_requests SET requester_id=? WHERE id=?`, uid, id); err != nil {
		t.Fatal(err)
	}
	fields.Del("csrf")
	resp = s.request(http.MethodPost, path+"/events", strings.NewReader(fields.Encode()), "application/x-www-form-urlencoded")
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
}

func TestRecoverableMobileFiltersExposeApplyResetAndSelectedState(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	cats, err := s.st.ListRecoverableCategories(s.ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	body := responseBody(t, s.request(http.MethodGet, "/recoverables/list?category="+strconv.FormatInt(cats[0].ID, 10)+"&ageing=overdue", nil, ""))
	start := strings.Index(body, `class="m-only recovery-filters"`)
	if start < 0 {
		t.Fatal("missing mobile filters")
	}
	chunk := body[start:]
	if end := strings.Index(chunk, "</details>"); end >= 0 {
		chunk = chunk[:end]
	}
	for _, want := range []string{`name="category"`, `name="ageing"`, `value="overdue" selected`, "Apply filters", `href="/recoverables/list">Reset`} {
		if !strings.Contains(chunk, want) {
			t.Fatalf("mobile filters missing %q", want)
		}
	}
}
