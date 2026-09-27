package app

import (
	"fervidbudget/internal/store"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPaymentReservationDoesNotSurviveRevokedRequestScope(t *testing.T) {
	s := newAppTestServer(t)
	admin, head := s.seedHead("Revoked payout scope")
	id := s.seedApprovedRequest(1, admin.ID, admin.ID, head, 1200000)
	grants := []store.Grant{{Resource: "payment", Action: "settle"}, {Resource: "reservation", Action: "reserve"}, {Resource: "request", Action: "view"}, {Resource: "payment", Action: "create"}, {Resource: "payment", Action: "process"}, {Resource: "payment", Action: "view"}}
	actor := s.seedProbeUser("scope-payout@example.test", "Scoped Accounts", "ScopePayout123", "Scoped Accounts", grants, []store.ScopeGrant{{Resource: "request", Scope: "all"}, {Resource: "payment", Scope: "all"}})
	roles, err := s.st.UserRoles(s.ctx, actor.ID)
	if err != nil || len(roles) != 1 {
		t.Fatalf("roles=%v error=%v", roles, err)
	}
	s.login(actor.Email, "ScopePayout123")
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", id), url.Values{}), http.StatusSeeOther)
	if err := s.st.UpdateRolePermissions(s.ctx, admin, roles[0].ID, grants, []store.ScopeGrant{{Resource: "request", Scope: "own"}, {Resource: "payment", Scope: "all"}}); err != nil {
		t.Fatal(err)
	}
	// Keep the same session: role changes must take effect on the next request.
	for _, path := range []string{fmt.Sprintf("/requests/%d", id), fmt.Sprintf("/payments/new?request=%d", id)} {
		resp := s.request(http.MethodGet, path, nil, "")
		body := responseBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("revoked scope GET %s status=%d body bytes=%d, want404", path, resp.StatusCode, len(body))
		}
	}
	form := url.Values{"request_id": {fmt.Sprint(id)}, "paid_on": {"2026-06-15"}, "amount": {"7000"}, "settlement": {"installment"}, "submission_key": {"revoked-scope-form"}, "reference_no": {"RESTORED-SCOPE-REF"}, "expected_paid": {"0"}}
	preview := s.postForm(fmt.Sprintf("/requests/%d/settlement-preview", id), form)
	requireStatus(t, preview, http.StatusNotFound)
	requireStatus(t, s.postForm(fmt.Sprintf("/requests/%d/record-payment", id), url.Values{}), http.StatusNotFound)
	resp := s.postForm("/payments", form)
	_ = responseBody(t, resp)
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusForbidden {
		t.Errorf("payout after scope revocation status=%d, want403/404", resp.StatusCode)
	}
	var count int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM payments WHERE request_id=?`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("revoked scope still created %d payout", count)
	}
	// Restoring visibility makes the original reservation usable again; the fix
	// must not turn all held payments into a permanent refusal.
	if err := s.st.UpdateRolePermissions(s.ctx, admin, roles[0].ID, grants, []store.ScopeGrant{{Resource: "request", Scope: "all"}, {Resource: "payment", Scope: "all"}}); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, s.postForm("/payments", form), http.StatusSeeOther)

}

func TestEditedRecoveryReplayDoesNotShowSuccess(t *testing.T) {
	s := newAppTestServer(t)
	_, head := s.seedHead("Recovery replay")
	id := seedRecoverable(t, s, head, "PR-REPLAY-HTTP", "icd", "Replay Party", "2026-06-30", "2026-02-01", 700000)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	path := fmt.Sprintf("/recoverables/%d/events", id)
	form := url.Values{"kind": {"return"}, "amount": {"2000"}, "occurred_on": {"2026-02-02"}, "reference": {"ORIGINAL-RECEIPT"}, "note": {"Bank evidence"}, "token": {"http-recovery-replay-confirmation"}}
	requireStatus(t, s.postForm(path, form), http.StatusSeeOther)
	form.Set("amount", "3000")
	form.Set("reference", "CHANGED-RECEIPT")
	resp := s.postForm(path, form)
	requireStatus(t, resp, http.StatusUnprocessableEntity)
	body := responseBody(t, resp)
	if !strings.Contains(body, "already recorded a different recovery") || !strings.Contains(body, `value="3000"`) {
		t.Fatal("changed replay lacks clear refusal or loses typed amount")
	}
}
