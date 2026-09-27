package app

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// Fixwave 2026-09-25, cluster A — roles and RBAC.

// rbac-1: an Advanced box unticked under a cell that is still ticked used to
// be silently re-granted, because the save expanded every ticked cell and only
// ever added the Advanced values. The Advanced boxes are the grant set; a
// ticked cell counts only when none of the boxes behind it is ticked (the
// JavaScript-less path, where ticking a cell cannot tick its boxes).
func TestRolesSaveHonoursAnUntickedAdvancedBoxUnderATickedCell(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)
	requireStatus(t, s.postForm("/roles/new", url.Values{"name": {"Field Staff"}}), http.StatusSeeOther)
	roleID := s.roleIDByName(t, "Field Staff")

	held := func() map[string]bool {
		t.Helper()
		grants, _, err := s.st.RolePermissions(s.ctx, parseID(roleID))
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, g := range grants {
			out[g.Resource+":"+g.Action] = true
		}
		return out
	}

	// The View cell is ticked; of the two boxes behind it only request:view is.
	save := url.Values{
		"role_id": {roleID}, "name": {"Field Staff"},
		"cell": {"requests:view"},
		"perm": {"request:view"},
	}
	requireStatus(t, s.postForm("/roles", save), http.StatusSeeOther)
	got := held()
	if !got["request:view"] || got["attachment:view"] || len(got) != 1 {
		t.Fatalf("grants after unticking attachment:view under a ticked View cell = %v, want request:view only", got)
	}

	// A ticked cell with none of its boxes ticked is the no-script tick of a
	// cell, and still expands to everything behind it.
	save = url.Values{"role_id": {roleID}, "name": {"Field Staff"}, "cell": {"requests:view"}}
	requireStatus(t, s.postForm("/roles", save), http.StatusSeeOther)
	got = held()
	if !got["request:view"] || !got["attachment:view"] || len(got) != 2 {
		t.Fatalf("grants after ticking the bare View cell = %v, want request:view + attachment:view", got)
	}

	// The screen then draws the partial state (rbac-5): the cell is marked
	// partial, and the mobile badge counts the row's grants, not its cells.
	save = url.Values{"role_id": {roleID}, "name": {"Field Staff"}, "cell": {"requests:view"}, "perm": {"request:view", "approval:approve"}}
	requireStatus(t, s.postForm("/roles", save), http.StatusSeeOther)
	body := responseBody(t, s.request(http.MethodGet, "/roles?role="+roleID, nil, ""))
	viewCell := regexp.MustCompile(`<input type="checkbox" name="cell" value="requests:view"[^>]*>`).FindString(body)
	if viewCell == "" || !strings.Contains(viewCell, `data-partial`) || strings.Contains(viewCell, "checked") {
		t.Fatalf("the half-held View cell renders as %q, want unchecked and marked data-partial", viewCell)
	}
	approveCell := regexp.MustCompile(`<input type="checkbox" name="cell" disabled value="requests:approve"[^>]*>`).FindString(body)
	if approveCell == "" || !strings.Contains(approveCell, `data-partial`) {
		t.Fatalf("the mobile Approve cell renders as %q, want marked data-partial", approveCell)
	}
	if !strings.Contains(body, `<b>Payment requests</b><span class="n">2 of 15</span>`) {
		t.Fatalf("the mobile badge does not count the row's two grants out of its fifteen: %s",
			regexp.MustCompile(`<b>Payment requests</b><span class="n">[^<]*</span>`).FindString(body))
	}
}

// rbac-2: a request scope of None (no role_data_scope row) must mean no rows,
// on the list and the CSV alike, which is what the detail page already says
// with its 404. The same rule applies to the payment ledger and its detail page.
func TestRequestScopeNoneShowsNoRowsAnywhere(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Scope")
	reqID := s.seedPendingRequest(1, admin.ID, admin.ID, headID, 1200)
	s.seedProbeUser("carl@example.test", "Carl", "CarlPass12345", "Viewer",
		[]store.Grant{{Resource: "request", Action: "view"}, {Resource: "grid", Action: "view"}}, nil)

	s.login("carl@example.test", "CarlPass12345")
	body := responseBody(t, s.request(http.MethodGet, "/requests?bucket=all", nil, ""))
	if strings.Contains(body, "PR-2026-000001") {
		t.Fatalf("a viewer with no request scope sees the list: %s", body)
	}
	csv := responseBody(t, s.request(http.MethodGet, "/requests/export.csv?bucket=all", nil, ""))
	if strings.Contains(csv, "PR-2026-000001") {
		t.Fatalf("a viewer with no request scope gets rows in the export: %s", csv)
	}
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/requests/%d", reqID), nil, ""), http.StatusNotFound)
	// The dashboard's own count of the scope agrees.
	requireStatus(t, s.request(http.MethodGet, "/", nil, ""), http.StatusOK)
}

func TestPaymentScopeNoneShowsNoLedgerRows(t *testing.T) {
	s := newAppTestServer(t)
	admin, headID := s.seedHead("Ledger")
	payID, err := s.st.CreatePayment(s.ctx, admin, store.PaymentInput{HeadID: headID, PaidOn: "2026-06-03", Amount: 4200, VendorPayee: "Scope Probe Payee"})
	if err != nil {
		t.Fatal(err)
	}
	s.seedProbeUser("dana@example.test", "Dana", "DanaPass12345", "Ledger Viewer",
		[]store.Grant{{Resource: "payment", Action: "view"}}, nil)

	s.login("dana@example.test", "DanaPass12345")
	body := responseBody(t, s.request(http.MethodGet, "/payments?month=2026-06&status=all", nil, ""))
	if strings.Contains(body, "Scope Probe Payee") {
		t.Fatalf("a viewer with no payment scope sees the ledger: %s", body)
	}
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/payments/%d", payID), nil, ""), http.StatusNotFound)

	// And a scope of All still shows the row, so the ledger was not locked shut.
	s.seedProbeUser("erin@example.test", "Erin", "ErinPass12345", "Ledger Reader",
		[]store.Grant{{Resource: "payment", Action: "view"}},
		[]store.ScopeGrant{{Resource: "payment", Scope: store.ScopeAll}})
	s.login("erin@example.test", "ErinPass12345")
	body = responseBody(t, s.request(http.MethodGet, "/payments?month=2026-06&status=all", nil, ""))
	if !strings.Contains(body, "Scope Probe Payee") {
		t.Fatalf("a viewer with scope All lost the ledger: %s", body)
	}
	requireStatus(t, s.request(http.MethodGet, fmt.Sprintf("/payments/%d", payID), nil, ""), http.StatusOK)
}

// rbac-6: the delete sheet for a role people still hold used to invite the
// deletion and then land on the store's 403. It now says why the role cannot
// be deleted, points at the Users screen and offers no Delete button.
func TestRoleDeleteSheetExplainsAHeldRole(t *testing.T) {
	s := newAppTestServer(t)
	holder := s.seedProbeUser("holder@example.test", "Holder", "HolderPass123", "Viewer",
		[]store.Grant{{Resource: "request", Action: "view"}}, nil)
	roles, err := s.st.UserRoles(s.ctx, holder.ID)
	if err != nil || len(roles) != 1 {
		t.Fatalf("UserRoles = %+v, %v", roles, err)
	}
	roleID := fmt.Sprint(roles[0].ID)

	// deleteSheet is the #role-delete overlay alone, up to its closing tags,
	// so the page chrome's own forms cannot satisfy or fail the assertions.
	deleteSheet := func(body string) string {
		t.Helper()
		start := strings.Index(body, `id="role-delete"`)
		if start < 0 {
			t.Fatalf("no delete sheet on the roles screen: %s", body)
		}
		rest := body[start:]
		end := strings.Index(rest, "\n  </div>\n</div>")
		if end < 0 {
			t.Fatalf("unterminated delete sheet: %s", rest)
		}
		return rest[:end]
	}

	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/roles?role="+roleID, nil, ""))
	sheet := deleteSheet(body)
	if !strings.Contains(sheet, "1 user holds this role") || strings.Contains(sheet, "1 users") {
		t.Fatalf("the sheet miscounts or mis-pluralises the holders: %s", sheet[:600])
	}
	if strings.Contains(sheet, "will lose everything it grants") || !strings.Contains(sheet, "cannot be deleted") {
		t.Fatalf("the sheet still invites a deletion the store refuses: %s", sheet[:600])
	}
	if strings.Contains(sheet, `type="submit"`) || !strings.Contains(sheet, `href="/users"`) {
		t.Fatalf("the sheet must offer the Users screen, not a Delete button: %s", sheet[:600])
	}
	if strings.Contains(body, "1 users") {
		t.Fatalf("the roles screen says \"1 users\": %s", body)
	}

	// Once nobody holds it, the sheet offers the deletion again.
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.SetUserRoles(s.ctx, admin, holder.ID, nil); err != nil {
		t.Fatal(err)
	}
	body = responseBody(t, s.request(http.MethodGet, "/roles?role="+roleID, nil, ""))
	sheet = deleteSheet(body)
	if !strings.Contains(sheet, `type="submit"`) || !strings.Contains(sheet, "Nobody holds this role") {
		t.Fatalf("an unheld role no longer offers Delete: %s", sheet[:600])
	}
}
