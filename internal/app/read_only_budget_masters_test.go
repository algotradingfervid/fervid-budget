package app

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// A viewer must be able to read the exact existing values without being invited
// to make edits the server will reject. Direct POSTs must still be forbidden.
func TestBudgetAndMasterViewersHaveNoWriteControls(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("ReadOnly")
	s.seedProbeUser("viewer@example.test", "Read-only viewer", "ReadOnly123", "read-only", []store.Grant{
		{Resource: "budget", Action: "view"}, {Resource: "project", Action: "view"}, {Resource: "head", Action: "view"},
	}, nil)
	s.login("viewer@example.test", "ReadOnly123")
	for _, tc := range []struct{ path, button, field string }{
		{"/budgets", "Save Budgets", `id="budget_`},
		{"/projects", "Add Project", `form="project-`},
		{"/heads", "Add Head", `form="head-`},
	} {
		resp := s.request(http.MethodGet, tc.path, nil, "")
		requireStatus(t, resp, http.StatusOK)
		body := responseBody(t, resp)
		if strings.Contains(body, tc.button) || strings.Contains(body, ">Save</button>") {
			t.Errorf("%s offers write controls", tc.path)
		}
		if !strings.Contains(body, "Operations ReadOnly") {
			t.Errorf("%s hides existing values", tc.path)
		}
		fields := regexp.MustCompile(`<(?:input|select)\b[^>]*`+tc.field+`[^>]*>`).FindAllString(body, -1)
		if len(fields) == 0 {
			t.Fatalf("%s missing readable row fields", tc.path)
		}
		for _, field := range fields {
			if !strings.Contains(field, "disabled") {
				t.Errorf("%s permits editing: %s", tc.path, field)
			}
		}
		resp = s.postForm(tc.path, url.Values{"name": {"Must not be saved"}, "month": {"2026-06"}})
		requireStatus(t, resp, http.StatusForbidden)
		resp.Body.Close()
	}
	var headName string
	err := s.st.DB().QueryRow(`SELECT name FROM heads WHERE id=?`, headID).Scan(&headName)
	if err != nil || headName != "Rent ReadOnly" {
		t.Fatalf("viewer changed head: %q %v", headName, err)
	}
	// Full editors retain the controls, ensuring this isn't a blanket disable.
	s.login(s.cfg.AdminEmail, testAdminPassword)
	for _, tc := range []struct{ path, button string }{{"/budgets", "Save Budgets"}, {"/projects", "Add Project"}, {"/heads", "Add Head"}} {
		body := responseBody(t, s.request(http.MethodGet, tc.path, nil, ""))
		if !strings.Contains(body, tc.button) {
			t.Errorf("editor lost %s", tc.button)
		}
	}
}

func TestMasterCreateAndEditGrantsAreIndependent(t *testing.T) {
	s := newAppTestServer(t)
	_, headID := s.seedHead("Separate grants")
	var projectID int64
	if err := s.st.DB().QueryRow(`SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"create", "edit"} {
		email := action + "@example.test"
		s.seedProbeUser(email, action, "SeparateGrants123", action+"-only", []store.Grant{
			{Resource: "project", Action: "view"}, {Resource: "head", Action: "view"},
			{Resource: "project", Action: action}, {Resource: "head", Action: action},
		}, nil)
		s.login(email, "SeparateGrants123")
		for _, tc := range []struct {
			path, button string
			id           int64
		}{{"/projects", "Add Project", projectID}, {"/heads", "Add Head", headID}} {
			body := responseBody(t, s.request(http.MethodGet, tc.path, nil, ""))
			if strings.Contains(body, tc.button) != (action == "create") {
				t.Errorf("%s-only wrong create control on %s", action, tc.path)
			}
			if strings.Contains(body, ">Save</button>") != (action == "edit") {
				t.Errorf("%s-only wrong edit control on %s", action, tc.path)
			}
			for _, editing := range []bool{false, true} {
				form := url.Values{"name": {"Independent " + action + tc.path}, "project_id": {strconv.FormatInt(projectID, 10)}, "active": {"on"}}
				if editing {
					form.Set("id", strconv.FormatInt(tc.id, 10))
				}
				want := http.StatusForbidden
				if editing == (action == "edit") {
					want = http.StatusSeeOther
				}
				resp := s.postForm(tc.path, form)
				requireStatus(t, resp, want)
				resp.Body.Close()
			}
		}
	}
}
