package app

// The users admin screen: listing users with their roles and default
// approver, and creating or editing one user.

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// newUserForm is the "Add user" drawer when a create is refused: shown open
// with the error and what was typed. The password is never sent back to the
// page, so it is the one field the admin types again.
type newUserForm struct {
	Open    bool
	Error   string
	Email   string
	Name    string
	Active  bool
	RoleIDs map[int64]bool
}

// users shows the users screen.
func (a *App) users(w http.ResponseWriter, r *http.Request) {
	data, err := a.usersPageData(r)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "users", data)
}

// refuseNewUser answers a refused create with the users screen and the Add
// user drawer still open (status 400), so the admin can correct the form
// instead of losing it on an error page.
func (a *App) refuseNewUser(w http.ResponseWriter, r *http.Request, message string, roleIDs []int64) {
	data, err := a.usersPageData(r)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	ticked := make(map[int64]bool, len(roleIDs))
	for _, id := range roleIDs {
		ticked[id] = true
	}
	data.NewUser = newUserForm{Open: true, Error: message, Email: r.FormValue("email"), Name: r.FormValue("name"),
		Active: r.FormValue("active") == "on", RoleIDs: ticked}
	a.renderStatus(w, r, http.StatusBadRequest, "users", data)
}

// usersPageData is the users screen's data: every user, every role, the
// roles each user holds, and the people who can be chosen as an approver.
func (a *App) usersPageData(r *http.Request) (PageData, error) {
	users, err := a.st.ListUsers(r.Context())
	if err != nil {
		return PageData{}, err
	}
	roles, err := a.st.AllRoles(r.Context())
	if err != nil {
		return PageData{}, err
	}
	approvers, err := a.st.ListApprovers(r.Context(), 0)
	if err != nil {
		return PageData{}, err
	}
	assigned, err := a.userRoleSets(r.Context(), users)
	if err != nil {
		return PageData{}, err
	}
	names := make(map[int64]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Name
	}
	return PageData{
		Title:         "Users",
		Users:         users,
		AllRoles:      roles,
		UserRoleIDs:   assigned,
		Approvers:     approvers,
		ApproverNames: names,
	}, nil
}

// userRoleSets maps each user's id to the set of role ids they hold. It reads
// the roles one user at a time.
func (a *App) userRoleSets(ctx context.Context, users []store.User) (map[int64]map[int64]bool, error) {
	sets := make(map[int64]map[int64]bool, len(users))
	for _, u := range users {
		roles, err := a.st.UserRoles(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		set := map[int64]bool{}
		for _, role := range roles {
			set[role.ID] = true
		}
		sets[u.ID] = set
	}
	return sets, nil
}

// userSave creates a user (no id) or updates one: name, email, active flag,
// password (optional on update), roles and, for an existing user, the default
// approver. Creating needs user:create and editing needs user:edit; the route
// only checks that the caller has one of the two. A refused create keeps the
// Add user drawer open with the reason. The save is audited.
func (a *App) userSave(w http.ResponseWriter, r *http.Request) {
	id := parseID(r.FormValue("id"))
	if !a.canSave(r, "user", id) {
		a.respondError(w, r, http.StatusForbidden, "You do not have permission to perform this action.", nil)
		return
	}
	roleIDs := make([]int64, 0, len(r.Form["role_ids"]))
	for _, raw := range r.Form["role_ids"] {
		if v := parseID(raw); v != 0 {
			roleIDs = append(roleIDs, v)
		}
	}
	// The password rules (12 to 72 characters, a letter and a digit) are
	// shared with the password-reset form; a refusal is a sentence for the form.
	hash := ""
	if pw := r.FormValue("password"); pw != "" {
		if err := auth.ValidatePassword(pw); err != nil {
			if id == 0 {
				a.refuseNewUser(w, r, err.Error(), roleIDs)
				return
			}
			a.respondError(w, r, http.StatusBadRequest, err.Error(), nil)
			return
		}
		var err error
		hash, err = auth.HashPassword(pw)
		if err != nil {
			a.respondError(w, r, http.StatusInternalServerError, "The password could not be secured.", err)
			return
		}
	}
	active := r.FormValue("active") == "on"
	var err error
	var savedID int64
	if id == 0 {
		if hash == "" {
			a.refuseNewUser(w, r, "A password is required.", roleIDs)
			return
		}
		if len(roleIDs) == 0 {
			a.refuseNewUser(w, r, "Choose at least one role for the new user.", roleIDs)
			return
		}
		savedID, err = a.st.CreateManagedUser(r.Context(), auth.CurrentUser(r), r.FormValue("email"), r.FormValue("name"), hash, active, roleIDs)
	} else {
		current := auth.CurrentUser(r)
		// Deactivating someone that open requests still wait on first shows a
		// confirmation screen.
		if asked, err := a.userDeactivationNeedsConfirm(w, r, id); err != nil {
			a.respondStoreError(w, r, err)
			return
		} else if asked {
			return
		}
		// Profile, roles and default approver are saved in one transaction, so
		// a refusal leaves nothing half-saved.
		err = a.st.SaveUser(r.Context(), current, store.UserSaveInput{
			ID:                id,
			Name:              r.FormValue("name"),
			Role:              r.FormValue("role"),
			Active:            active,
			PasswordHash:      hash,
			RoleIDs:           roleIDs,
			DefaultApproverID: parseID(r.FormValue("default_approver_id")),
		})
		savedID = id
	}
	if err != nil {
		if id == 0 && errors.Is(err, store.ErrDuplicate) {
			a.refuseNewUser(w, r, "A user with this email address already exists. Use a different email or edit the existing user.", roleIDs)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	u := auth.CurrentUser(r)
	action, summary := "update", "Updated user "
	if id == 0 {
		action, summary = "create", "Created user "
	}
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: action, EntityType: "user", EntityID: &savedID,
		Summary: summary + strings.ToLower(strings.TrimSpace(r.FormValue("email")))})
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

// approverAvailable reports whether the user with this id is in approvers,
// the list of people who may currently approve. The user and request forms
// call it from their templates to warn when a saved default approver can no
// longer approve.
func approverAvailable(id int64, approvers []store.User) bool {
	for _, user := range approvers {
		if user.ID == id {
			return true
		}
	}
	return false
}
