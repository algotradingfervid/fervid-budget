package app

// The project and head master lists.

import (
	"context"
	"net/http"
	"strconv"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

func (a *App) projects(w http.ResponseWriter, r *http.Request) {
	projects, err := a.st.ListProjects(r.Context(), false)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "projects", PageData{Title: "Projects", Projects: projects})
}

func (a *App) projectSave(w http.ResponseWriter, r *http.Request) {
	id := parseID(r.FormValue("id"))
	action := "edit"
	if id == 0 {
		action = "create"
	}
	if !a.auth.Can(auth.CurrentUser(r), "project", action) {
		a.respondStoreError(w, r, store.ErrForbidden)
		return
	}
	sortOrder, _ := strconv.Atoi(r.FormValue("sort_order"))
	_, err := a.st.UpsertProject(r.Context(), id, r.FormValue("name"), r.FormValue("active") == "on", sortOrder, auth.CurrentUser(r))

	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

func (a *App) heads(w http.ResponseWriter, r *http.Request) {
	data, err := a.headsPageData(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "heads", data)
}

// headsPageData is the heads screen's whole state, split out so the contract it
// owes the markup is testable without a rendered page.
//
// Two project lists, because the screen asks two different questions. Projects
// is what a *new* head may be filed under: active only, per T12. AllProjects is
// what an *existing* row's select must offer, and it has to be every project —
// a head whose project was retired had no matching option, so the browser
// preselected the first one and that row's own Save moved the head there with no
// edit of any kind (F-G-033). Server-side there is no way to tell "the operator
// chose this project" from "the browser defaulted to it", which is why the fix is
// the option list and not a validation rule.
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

func (a *App) headSave(w http.ResponseWriter, r *http.Request) {
	id := parseID(r.FormValue("id"))
	action := "edit"
	if id == 0 {
		action = "create"
	}
	if !a.auth.Can(auth.CurrentUser(r), "head", action) {
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
	_, err := a.st.UpsertHead(r.Context(), id, projectID, r.FormValue("name"), r.FormValue("due_day"), r.FormValue("active") == "on", sortOrder, auth.CurrentUser(r))

	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/heads", http.StatusSeeOther)
}
