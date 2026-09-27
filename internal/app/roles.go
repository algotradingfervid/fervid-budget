package app

// The roles admin screen: the permission matrix for one role, and creating,
// copying, saving and deleting roles.

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// newRoleForm is the "New role" drawer when a create is refused: shown open
// with the error and what was typed, while the screen keeps the role that was
// selected.
type newRoleForm struct {
	Open        bool
	Error       string
	Name        string
	Description string
}

// rolesPage shows the roles screen with the role in ?role= selected (the
// first role when none is given).
func (a *App) rolesPage(w http.ResponseWriter, r *http.Request) {
	data, err := a.rolesPageData(r, parseID(r.URL.Query().Get("role")))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "roles", data)
}

// rolesPageData is the roles screen's data: every role, the selected role's
// permission matrix, and how many users hold each role.
func (a *App) rolesPageData(r *http.Request, selected int64) (PageData, error) {
	roles, err := a.st.AllRoles(r.Context())
	if err != nil {
		return PageData{}, err
	}
	if selected == 0 && len(roles) > 0 {
		selected = roles[0].ID
	}
	role, err := a.st.Role(r.Context(), selected)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return PageData{}, err
	}
	grants, scopes, err := a.st.RolePermissions(r.Context(), selected)
	if err != nil {
		return PageData{}, err
	}
	users, err := a.st.ListUsers(r.Context())
	if err != nil {
		return PageData{}, err
	}
	sets, err := a.userRoleSets(r.Context(), users)
	if err != nil {
		return PageData{}, err
	}
	counts := map[int64]int{}
	for _, set := range sets {
		for roleID := range set {
			counts[roleID]++
		}
	}
	return PageData{
		Title:          "Roles",
		Roles:          roles,
		Role:           role,
		PermColumns:    permColumns,
		PermMatrix:     buildPermMatrix(grants, scopes),
		RoleUserCounts: counts,
	}, nil
}

// rolesSave saves a role's name, description and permission matrix.
//
// The ticked "perm" boxes (resource:action, from the Advanced view) are the
// grants. A ticked matrix "cell" adds every action behind it, but only when
// none of its Advanced boxes was sent: that is the form without JavaScript,
// where ticking a cell cannot tick its boxes. Anything not ticked is revoked,
// because the store replaces the role's whole grant set.
func (a *App) rolesSave(w http.ResponseWriter, r *http.Request) {
	roleID := parseID(r.FormValue("role_id"))
	var grants []store.Grant
	seen := map[store.Grant]bool{}
	for _, raw := range r.Form["perm"] {
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			continue
		}
		g := store.Grant{Resource: parts[0], Action: parts[1]}
		if seen[g] {
			continue
		}
		seen[g] = true
		grants = append(grants, g)
	}
	for _, raw := range r.Form["cell"] {
		behind := cellGrants(raw)
		spoken := false
		for _, g := range behind {
			if seen[g] {
				spoken = true
				break
			}
		}
		if spoken {
			continue
		}
		for _, g := range behind {
			seen[g] = true
			grants = append(grants, g)
		}
	}
	// The desktop and mobile matrices each have their own scope radio group
	// ("scope_x" and "m_scope_x"); radios sharing a name would be one group
	// across the whole form. The desktop value wins when set. An empty value
	// means "None" and stores no scope.
	var scopes []store.ScopeGrant
	for _, res := range scopedMatrixResources() {
		v := strings.TrimSpace(r.FormValue("scope_" + res))
		if v == "" {
			v = strings.TrimSpace(r.FormValue("m_scope_" + res))
		}
		if v != "" {
			scopes = append(scopes, store.ScopeGrant{Resource: res, Scope: v})
		}
	}
	// Check every grant before writing anything: the rename and the grants
	// are two separate store writes, and an unknown grant must not leave the
	// rename saved on its own. The store checks the grants again itself.
	for _, g := range grants {
		if !store.ValidGrant(g.Resource, g.Action) {
			a.respondError(w, r, http.StatusBadRequest, "That permission does not exist.", nil)
			return
		}
	}
	if err := a.st.UpdateRole(r.Context(), auth.CurrentUser(r), roleID, r.FormValue("name"), r.FormValue("description")); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if err := a.st.UpdateRolePermissions(r.Context(), auth.CurrentUser(r), roleID, grants, scopes); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles?role="+strconv.FormatInt(roleID, 10), http.StatusSeeOther)
}

// roleCreate creates an empty role and selects it. A duplicate or invalid
// name shows the screen again with the New role drawer open (when the caller
// can view roles); any other failure is an error page.
func (a *App) roleCreate(w http.ResponseWriter, r *http.Request) {
	id, err := a.st.CreateRole(r.Context(), auth.CurrentUser(r), r.FormValue("name"), r.FormValue("description"))
	if err != nil {
		if (errors.Is(err, store.ErrDuplicate) || errors.Is(err, store.ErrValidation)) && a.auth.Can(auth.CurrentUser(r), "role", "view") {
			data, loadErr := a.rolesPageData(r, parseID(r.FormValue("selected_role_id")))
			if loadErr != nil {
				a.respondStoreError(w, r, loadErr)
				return
			}
			message := friendly(err)
			if errors.Is(err, store.ErrDuplicate) {
				message = "A role with this name already exists. Choose a different name."
			}
			data.NewRole = newRoleForm{Open: true, Error: message, Name: r.FormValue("name"), Description: r.FormValue("description")}
			a.renderStatus(w, r, http.StatusBadRequest, "roles", data)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles?role="+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// roleCopy creates a new role with the given name and the path role's
// permissions, and selects it.
func (a *App) roleCopy(w http.ResponseWriter, r *http.Request) {
	id, err := a.st.CopyRole(r.Context(), auth.CurrentUser(r), pathID(r), r.FormValue("name"))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles?role="+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// roleDelete deletes a role. The store refuses a system role and a role that
// someone still holds; respondStoreError shows the store's reason with 403.
func (a *App) roleDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.st.DeleteRole(r.Context(), auth.CurrentUser(r), pathID(r)); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles", http.StatusSeeOther)
}
