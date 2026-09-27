package app

// The project and head master lists, and saving one project or head. The
// POST routes only require a signed-in user: whether the caller needs create
// or edit depends on the posted id, so each save handler checks it itself.

import (
	"context"
	"net/http"
	"strconv"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// canSave reports whether the signed-in user may save a record of resource:
// id 0 is a new record and needs "create", any other id needs "edit".
func (a *App) canSave(r *http.Request, resource string, id int64) bool {
	action := "edit"
	if id == 0 {
		action = "create"
	}
	return a.auth.Can(auth.CurrentUser(r), resource, action)
}

// projects lists every project, active and retired.
func (a *App) projects(w http.ResponseWriter, r *http.Request) {
	projects, err := a.st.ListProjects(r.Context(), false)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "projects", PageData{Title: "Projects", Projects: projects})
}

// projectSave creates (no id) or updates a project.
func (a *App) projectSave(w http.ResponseWriter, r *http.Request) {
	id := parseID(r.FormValue("id"))
	if !a.canSave(r, "project", id) {
		a.respondStoreError(w, r, store.ErrForbidden)
		return
	}
	sortOrder, _ := strconv.Atoi(r.FormValue("sort_order"))
	if _, err := a.st.UpsertProject(r.Context(), id, r.FormValue("name"), r.FormValue("active") == "on", sortOrder, auth.CurrentUser(r)); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

// heads lists every head with the projects it can be filed under.
func (a *App) heads(w http.ResponseWriter, r *http.Request) {
	data, err := a.headsPageData(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "heads", data)
}

// headsPageData is the heads screen's data. It carries two project lists:
// Projects (active only) is what a new head may be filed under, and
// AllProjects (retired ones included) is what an existing head's project
// select offers. Without the retired projects, a head filed under one would
// have no matching option, the browser would pick the first project, and
// saving that row would silently move the head.
func (a *App) headsPageData(ctx context.Context) (PageData, error) {
	heads, err := a.st.ListHeads(ctx, false)
	if err != nil {
		return PageData{}, err
	}
	projects, err := a.st.ListProjects(ctx, true)
	if err != nil {
		return PageData{}, err
	}
	allProjects, err := a.st.ListProjects(ctx, false)
	if err != nil {
		return PageData{}, err
	}
	return PageData{Title: "Heads", Heads: heads, Projects: projects, AllProjects: allProjects}, nil
}

// headSave creates (no id) or updates a head. Deactivating a head that open
// requests still use first shows a confirmation screen.
func (a *App) headSave(w http.ResponseWriter, r *http.Request) {
	id := parseID(r.FormValue("id"))
	if !a.canSave(r, "head", id) {
		a.respondStoreError(w, r, store.ErrForbidden)
		return
	}
	projectID := parseID(r.FormValue("project_id"))
	sortOrder, _ := strconv.Atoi(r.FormValue("sort_order"))
	if asked, err := a.headDeactivationNeedsConfirm(w, r, id); err != nil {
		a.respondStoreError(w, r, err)
		return
	} else if asked {
		return
	}
	if _, err := a.st.UpsertHead(r.Context(), id, projectID, r.FormValue("name"), r.FormValue("due_day"), r.FormValue("active") == "on", sortOrder, auth.CurrentUser(r)); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/heads", http.StatusSeeOther)
}
