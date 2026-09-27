package app

// The users admin screen: listing users and creating or editing one.

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// newUserForm is the Add user drawer as the server hands it back after refusing
// a create: open, with an inline error and what was typed. The password is
// never echoed into the page; it is the one field the reader types again.
type newUserForm struct {
	Open    bool
	Error   string
	Email   string
	Name    string
	Active  bool
	RoleIDs map[int64]bool
}

func (a *App) users(w http.ResponseWriter, r *http.Request) {
	data, err := a.usersPageData(r)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "users", data)
}

// refuseNewUser answers a create the server will not make with the users page
// and the Add user drawer still open, rather than the full-page error screen:
// that screen's Go back closed the drawer and lost the password (ui-1).
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

func (a *App) usersPageData(r *http.Request) (PageData, error) {
	users, err := a.st.ListUsers(r.Context())
	if err != nil {
		return PageData{}, err
	}
	roles, err := a.st.AllRoles(r.Context())
	if err != nil {
		return PageData{}, err
	}
	assigned := map[int64]map[int64]bool{}
	names := map[int64]string{}
	approvers, err := a.st.ListApprovers(r.Context(), 0)
	if err != nil {
		return PageData{}, err
	}
	for _, u := range users {
		names[u.ID] = u.Name
		urs, err := a.st.UserRoles(r.Context(), u.ID)
		if err != nil {
			return PageData{}, err
		}
		set := map[int64]bool{}
		for _, ur := range urs {
			set[ur.ID] = true
		}
		assigned[u.ID] = set
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

func (a *App) userSave(w http.ResponseWriter, r *http.Request) {
	id := parseID(r.FormValue("id"))
	// F-A-07: creating and editing are two grants and this one handler does both.
	// The verb it demands is therefore the verb the control the caller pressed is
	// gated on, not whichever one happened to be on the route.
	verb := "edit"
	if id == 0 {
		verb = "create"
	}
	if !a.auth.Can(auth.CurrentUser(r), "user", verb) {
		a.respondError(w, r, http.StatusForbidden, "You do not have permission to perform this action.", nil)
		return
	}
	// Both branches read the same role checkboxes. The create form used to send a
	// two-value legacy string instead, which could not express "requester" or
	// "approver" at all and mapped its harmless-looking option to the Accounts
	// role — the one that can settle and void payments.
	roleIDs := make([]int64, 0, len(r.Form["role_ids"]))
	for _, raw := range r.Form["role_ids"] {
		if v := parseID(raw); v != 0 {
			roleIDs = append(roleIDs, v)
		}
	}
	hash := ""
	if pw := r.FormValue("password"); pw != "" {
		if err := validatePassword(pw); err != nil {
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
		// users.role is superseded but still written, so anything that reads it
		// keeps agreeing with the roles that actually decide permissions.
		savedID, err = a.st.CreateManagedUser(r.Context(), auth.CurrentUser(r), r.FormValue("email"), r.FormValue("name"), hash, active, roleIDs)
	} else {
		current := auth.CurrentUser(r)
		if asked, err := a.userDeactivationNeedsConfirm(w, r, id); err != nil {
			a.respondStoreError(w, r, err)
			return
		} else if asked {
			return
		}
		// One form, one submit, one transaction. The profile, the role assignment
		// and the default approver used to be three independent store calls, so a
		// save refused by the second left the first one's rename committed and
		// unaudited (F-G-034). The default approver applies only to an existing
		// user; the roles are read once above, because both branches now take
		// them from the same checkboxes.
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
	action := "update"
	if id == 0 {
		action = "create"
	}
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: action, EntityType: "user", EntityID: &savedID, Summary: strings.Title(action) + "d user " + strings.ToLower(strings.TrimSpace(r.FormValue("email")))})
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

// validatePassword owns the whole password rule, so every rejection is a 400
// naming what is missing. It used to check the length alone and leave the
// letter-and-digit rule to auth.HashPassword, which reports a refusal as a
// failure to hash — a 500 reading "The password could not be secured." for a
// perfectly ordinary validation event (F-A-11/F-G-036). The 12-character
// minimum is this layer's own, stricter than auth's bootstrap-compatible 8.
func validatePassword(password string) error {
	if len(password) < 12 {
		return fmt.Errorf("password must be at least 12 characters")
	}
	return auth.ValidatePassword(password)
}
