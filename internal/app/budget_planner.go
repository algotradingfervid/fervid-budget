package app

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

type plannerSource struct {
	Month  string `json:"month"`
	Budget int64  `json:"budget"`
}
type plannerCatalogHead struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type plannerCatalogProject struct {
	ID    int64                `json:"id"`
	Name  string               `json:"name"`
	Heads []plannerCatalogHead `json:"heads"`
}
type plannerConfig struct {
	Draft            store.BudgetPlan        `json:"draft"`
	Edit             bool                    `json:"edit"`
	Sources          []plannerSource         `json:"sources"`
	Catalog          []plannerCatalogProject `json:"catalog"`
	CanCreateProject bool                    `json:"canCreateProject"`
	CanCreateHead    bool                    `json:"canCreateHead"`
	SavedProjectIDs  []int64                 `json:"savedProjectIds"`
	SavedHeadIDs     []int64                 `json:"savedHeadIds"`
	Saved            bool                    `json:"saved"`
	Error            string                  `json:"error,omitempty"`
}

func (a *App) plannerRender(w http.ResponseWriter, r *http.Request, status int, draft store.BudgetPlan, edit bool, message string) {
	perms := a.auth.Permissions(auth.CurrentUser(r))
	if !perms.Can("budget", "view") || !perms.Can("budget", "edit") || (!edit && !perms.Can("month", "create")) {
		a.respondStoreError(w, r, store.ErrForbidden)
		return
	}
	plans, err := a.st.ListMonthPlans(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	projects, err := a.st.ListProjects(r.Context(), false)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	heads, err := a.st.ListHeads(r.Context(), false)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if message != "" {
		// Keep entered line values and the original revision, but never let a
		// rejected draft redefine persisted names or retirement state in the UI.
		projectByID := map[int64]store.Project{}
		headByID := map[int64]store.Head{}
		for _, project := range projects {
			projectByID[project.ID] = project
		}
		for _, head := range heads {
			headByID[head.ID] = head
		}
		for i := range draft.Projects {
			project := &draft.Projects[i]
			if saved, ok := projectByID[project.ID]; ok {
				project.Name, project.ReadOnly = saved.Name, !saved.Active
			}
			for j := range project.Heads {
				head := &project.Heads[j]
				if saved, ok := headByID[head.ID]; ok {
					head.Name, head.ReadOnly = saved.Name, !saved.Active || project.ReadOnly || saved.ProjectID != project.ID
				}
			}
		}
	}
	cfg := plannerConfig{Draft: draft, Edit: edit, Sources: []plannerSource{}, Catalog: []plannerCatalogProject{}, CanCreateProject: perms.Can("project", "create"), CanCreateHead: perms.Can("head", "create"), Saved: r.URL.Query().Get("saved") == "1", Error: message}
	cfg.SavedProjectIDs, cfg.SavedHeadIDs = []int64{}, []int64{}
	if edit {
		saved, err := a.st.BudgetPlan(r.Context(), draft.Month)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		for _, project := range saved.Projects {
			cfg.SavedProjectIDs = append(cfg.SavedProjectIDs, project.ID)
			for _, head := range project.Heads {
				cfg.SavedHeadIDs = append(cfg.SavedHeadIDs, head.ID)
			}
		}
	}
	for _, p := range plans {
		cfg.Sources = append(cfg.Sources, plannerSource{p.Month, p.Budget})
	}
	for _, p := range projects {
		if !p.Active {
			continue
		}
		item := plannerCatalogProject{ID: p.ID, Name: p.Name, Heads: []plannerCatalogHead{}}
		for _, h := range heads {
			if h.ProjectID == p.ID && h.Active {
				item.Heads = append(item.Heads, plannerCatalogHead{h.ID, h.Name})
			}
		}
		cfg.Catalog = append(cfg.Catalog, item)
	}
	if cfg.Draft.Projects == nil {
		cfg.Draft.Projects = []store.PlanProject{}
	}
	// json.Marshal escapes HTML characters. This is data, never executable input.
	raw, err := json.Marshal(cfg)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	title := "Create a monthly budget"
	if edit {
		title = "Edit monthly budget"
	}
	notice := ""
	if cfg.Saved {
		notice = "Budget saved. Your budget lines and totals are now available in the grid and reports."
	}
	a.renderStatus(w, r, status, "budget-planner", PageData{Title: title, Month: draft.Month, Error: message, Notice: notice, BudgetPlannerJSON: template.JS(raw)})
}
func (a *App) budgetPlannerNew(w http.ResponseWriter, r *http.Request) {
	plans, err := a.st.ListMonthPlans(r.Context())
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	month := validMonthOrFallback(r.URL.Query().Get("month"), defaultTargetMonth(plans))
	a.plannerRender(w, r, http.StatusOK, store.BudgetPlan{Month: month, Projects: []store.PlanProject{}}, false, "")
}
func (a *App) budgetPlannerEdit(w http.ResponseWriter, r *http.Request) {
	month := validMonthOrCurrent(r.URL.Query().Get("month"))
	p, err := a.st.BudgetPlan(r.Context(), month)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if !p.Exists {
		http.Redirect(w, r, "/budgets/new?month="+month, http.StatusSeeOther)
		return
	}
	a.plannerRender(w, r, http.StatusOK, p, true, "")
}
func (a *App) budgetPlannerData(w http.ResponseWriter, r *http.Request) {
	p, err := a.st.BudgetPlan(r.Context(), r.URL.Query().Get("month"))
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	if !p.Exists {
		a.respondError(w, r, http.StatusNotFound, "No budget exists for that source month.", nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}
func (a *App) budgetPlannerSave(w http.ResponseWriter, r *http.Request) {
	raw := r.FormValue("draft")
	if len(raw) > 2<<20 {
		a.respondError(w, r, http.StatusRequestEntityTooLarge, "This budget is too large. Use fewer lines.", nil)
		return
	}
	var draft store.BudgetPlan
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&draft); err != nil {
		a.respondError(w, r, http.StatusBadRequest, "The budget could not be read. Return to the planner and try again.", nil)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		a.respondError(w, r, http.StatusBadRequest, "The budget contains unexpected data.", nil)
		return
	}
	mode := r.FormValue("mode")
	if mode != "create" && mode != "edit" {
		a.respondError(w, r, http.StatusBadRequest, "Choose whether to create or edit this budget.", nil)
		return
	}
	edit := mode == "edit"
	if err := a.st.SaveBudgetPlan(r.Context(), auth.CurrentUser(r), draft, edit); err != nil {
		message := friendly(err)
		// Refresh persisted metadata, keeping submitted lines and the original
		// revision for editable drafts. A locked response is a saved, read-only
		// view, so its values must come from storage rather than the rejected form.
		if fresh, e := a.st.BudgetPlan(r.Context(), draft.Month); e == nil {
			if fresh.Locked {
				draft = fresh
				message = "Your changes were not saved because this month is locked. The saved budget is shown below. Unlock the month with a reason before editing."
			} else {
				draft.Locked = fresh.Locked
				draft.Exists = fresh.Exists
				if edit {
					draft.SourceMonth = fresh.SourceMonth
				}
			}
		}
		a.plannerRender(w, r, reRenderStatus(err), draft, edit, message)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/budgets/plan?month=%s&saved=1", draft.Month), http.StatusSeeOther)
}
