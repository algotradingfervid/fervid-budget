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

// Saving a role grants exactly the ticked Advanced boxes: unticking a box under
// a still-ticked cell removes that permission. A ticked cell counts only when
// none of the boxes behind it is ticked (the no-JavaScript path, where ticking
// a cell cannot tick its boxes).
func TestRolesSaveHonoursAnUntickedAdvancedBoxUnderATickedCell(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()
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

	// The screen then draws the partial state: the cell is marked
	// partial, and the mobile badge counts the row's grants, not its cells.
	save = url.Values{"role_id": {roleID}, "name": {"Field Staff"}, "cell": {"requests:view"}, "perm": {"request:view", "approval:approve"}}
	requireStatus(t, s.postForm("/roles", save), http.StatusSeeOther)
	body := s.get("/roles?role=" + roleID)
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

// The delete sheet for a role people still hold says why the role cannot be
// deleted, points at the Users screen and offers no Delete button, rather than
// inviting a deletion the server would refuse.
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

	s.loginAdmin()
	body := s.get("/roles?role=" + roleID)
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
	admin := s.admin()
	if err := s.st.SetUserRoles(s.ctx, admin, holder.ID, nil); err != nil {
		t.Fatal(err)
	}
	body = s.get("/roles?role=" + roleID)
	sheet = deleteSheet(body)
	if !strings.Contains(sheet, `type="submit"`) || !strings.Contains(sheet, "Nobody holds this role") {
		t.Fatalf("an unheld role no longer offers Delete: %s", sheet[:600])
	}
}
