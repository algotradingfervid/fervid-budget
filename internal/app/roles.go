package app

// The roles admin screen: the permission matrix and creating, copying
// and deleting roles.

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// newRoleForm retains a refused create without changing the selected role.
type newRoleForm struct {
	Open        bool
	Error       string
	Name        string
	Description string
}

func (a *App) rolesPage(w http.ResponseWriter, r *http.Request) {
	data, err := a.rolesPageData(r, parseID(r.URL.Query().Get("role")))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "roles", data)
}

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
	counts := map[int64]int{}
	users, err := a.st.ListUsers(r.Context())
	if err != nil {
		return PageData{}, err
	}
	for _, u := range users {
		urs, err := a.st.UserRoles(r.Context(), u.ID)
		if err != nil {
			return PageData{}, err
		}
		for _, ur := range urs {
			counts[ur.ID]++
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

// rolesSave persists the matrix. The Advanced checkboxes are the grant set:
// what is ticked there is saved and what is unticked is revoked, even under a
// cell that is still ticked. The save used to start from every ticked cell
// expanded, so unticking one Advanced box under a ticked cell was silently
// re-granted (fixwave rbac-1). A ticked cell counts only when none of the
// boxes behind it is ticked — the JavaScript-less path, where ticking a cell
// cannot tick its boxes — and then expands to every canonical action behind
// it. UpdateRolePermissions then replaces the role's whole grant set, so
// anything absent here is revoked.
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
	// The two matrices carry two scope radio groups. They must not share a name:
	// same-name radios are one group across the whole form, so enabling the
	// mobile set would silently clear the desktop selection. The desktop group
	// wins when it is enabled; the mobile group is the fallback. An empty value
	// is the explicit "None" state and stores no scope row at all.
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
	// Screen the grants before writing anything. UpdateRole and
	// UpdateRolePermissions are two transactions, so a grant rejected by the
	// second would otherwise leave the first one's rename committed. Every grant
	// is re-validated inside UpdateRolePermissions as well — this is a guard
	// against a half-applied save, not the security boundary.
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

func (a *App) roleCopy(w http.ResponseWriter, r *http.Request) {
	id, err := a.st.CopyRole(r.Context(), auth.CurrentUser(r), pathID(r), r.FormValue("name"))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles?role="+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (a *App) roleDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.st.DeleteRole(r.Context(), auth.CurrentUser(r), pathID(r)); err != nil {
		// respondStoreError replaces every ErrForbidden with "You do not have
		// permission to perform this action.", which told the holder of all 66
		// grants that they held none (F-G-023). Both refusals this route can
		// produce — a system role, and a role somebody still holds — are rules
		// with reasons, so the reason is what the reader gets.
		if errors.Is(err, store.ErrForbidden) {
			a.respondError(w, r, storeErrorStatus(err), friendly(err), err)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/roles", http.StatusSeeOther)
}
