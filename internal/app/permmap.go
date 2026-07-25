package app

import (
	"strings"

	"fervidbudget/internal/store"
)

// The roles screen draws 9 page rows × 7 action columns; the vocabulary
// underneath is 21 resources with ragged action lists (66 canonical
// (resource, action) pairs). This file is the only place the two meet
// (adoption-spec D2).
//
// Enforcement never consults it: role_permissions stays one row per
// (resource, action) and internal/store and internal/auth do not import this
// package. A cell is a SET of canonical grants — toggling it grants or revokes
// all of them — and every row carries an Advanced disclosure enumerating its
// individual canonical actions, which is what keeps R2's fine-grained control.
// A cell with no canonical action for its column is unavailable and renders an
// em dash rather than a checkbox.
//
// Three canonical resources are not named in the D2 row table, because the
// approved screen draws only nine rows. They are placed where the approved
// screens put their controls, and permmap_test.go asserts the placement is
// total and unambiguous:
//
//	attachment      -> Payment requests. The uploader appears on the request
//	                   form and on payment entry; one grant governs both.
//	project, head   -> Budgets & variance grid. Projects and heads are the two
//	                   axes of the grid and of every budget row — they are
//	                   masters of the budget, not of administration.

type permColumn struct {
	Key   string
	Label string
}

// permColumns is the fixed column set of the approved matrix, in order.
var permColumns = []permColumn{
	{Key: "view", Label: "View"},
	{Key: "create", Label: "Create"},
	{Key: "edit", Label: "Edit"},
	{Key: "approve", Label: "Approve"},
	{Key: "process", Label: "Process"},
	{Key: "cancel", Label: "Cancel"},
	{Key: "export", Label: "Export"},
}

type permGroupDef struct {
	Key       string
	Label     string
	Resources []string
	Cells     map[string][]store.Grant
}

// permGroups is the 9-row presentation map. Every canonical resource is owned
// by exactly one row and every canonical action by exactly one cell.
var permGroups = []permGroupDef{
	{
		Key:       "requests",
		Label:     "Payment requests",
		Resources: []string{"request", "approval", "attachment"},
		Cells: map[string][]store.Grant{
			"view":   {{Resource: "request", Action: "view"}, {Resource: "attachment", Action: "view"}},
			"create": {{Resource: "request", Action: "create"}, {Resource: "request", Action: "reraise"}, {Resource: "attachment", Action: "create"}},
			"edit":   {{Resource: "request", Action: "edit"}, {Resource: "request", Action: "comment"}},
			"approve": {
				{Resource: "approval", Action: "approve"}, {Resource: "approval", Action: "reject"},
				{Resource: "approval", Action: "return"}, {Resource: "approval", Action: "reassign"},
				{Resource: "approval", Action: "accept_partial"},
			},
			// Withdrawal by the raiser, cancellation of a live request and the
			// approver's decision on that cancellation are one control on the
			// screen: "may take a request out of the pipeline".
			"cancel": {
				{Resource: "request", Action: "withdraw"}, {Resource: "request", Action: "cancel"},
				{Resource: "approval", Action: "cancel"},
			},
		},
	},
	{
		Key:       "payments",
		Label:     "Payments",
		Resources: []string{"payment"},
		Cells: map[string][]store.Grant{
			"view":   {{Resource: "payment", Action: "view"}},
			"create": {{Resource: "payment", Action: "create"}},
			"edit":   {{Resource: "payment", Action: "edit"}},
			"process": {
				{Resource: "payment", Action: "process"}, {Resource: "payment", Action: "settle"},
				{Resource: "payment", Action: "mark_partial"}, {Resource: "payment", Action: "hold"},
			},
			"cancel": {{Resource: "payment", Action: "void"}},
		},
	},
	{
		Key:       "reservations",
		Label:     "Reservations",
		Resources: []string{"reservation"},
		Cells: map[string][]store.Grant{
			"create": {{Resource: "reservation", Action: "reserve"}},
			"edit":   {{Resource: "reservation", Action: "reassign"}},
			"cancel": {{Resource: "reservation", Action: "release"}},
		},
	},
	{
		Key:       "recoverables",
		Label:     "Recoverables",
		Resources: []string{"recoverable_report", "recoverable_category"},
		Cells: map[string][]store.Grant{
			"view":   {{Resource: "recoverable_report", Action: "view"}, {Resource: "recoverable_category", Action: "view"}},
			"create": {{Resource: "recoverable_category", Action: "create"}},
			"edit":   {{Resource: "recoverable_category", Action: "edit"}},
			"cancel": {{Resource: "recoverable_category", Action: "delete"}},
			"export": {{Resource: "recoverable_report", Action: "export"}},
		},
	},
	{
		Key:       "vendors",
		Label:     "Vendors",
		Resources: []string{"vendor"},
		Cells: map[string][]store.Grant{
			"view":   {{Resource: "vendor", Action: "view"}},
			"create": {{Resource: "vendor", Action: "create"}},
			"edit":   {{Resource: "vendor", Action: "edit"}},
		},
	},
	{
		Key:       "vendor-bank",
		Label:     "Vendor bank details",
		Resources: []string{"vendor_bank"},
		Cells: map[string][]store.Grant{
			"view": {{Resource: "vendor_bank", Action: "view"}},
			"edit": {{Resource: "vendor_bank", Action: "edit"}},
		},
	},
	{
		Key:       "budgets",
		Label:     "Budgets & variance grid",
		Resources: []string{"budget", "grid", "month", "project", "head"},
		Cells: map[string][]store.Grant{
			"view": {
				{Resource: "budget", Action: "view"}, {Resource: "grid", Action: "view"},
				{Resource: "month", Action: "view"}, {Resource: "project", Action: "view"},
				{Resource: "head", Action: "view"},
			},
			"create":  {{Resource: "month", Action: "create"}, {Resource: "project", Action: "create"}, {Resource: "head", Action: "create"}},
			"edit":    {{Resource: "budget", Action: "edit"}, {Resource: "project", Action: "edit"}, {Resource: "head", Action: "edit"}},
			"process": {{Resource: "month", Action: "lock"}},
			"export":  {{Resource: "grid", Action: "export"}},
		},
	},
	{
		Key:       "reports",
		Label:     "Reports",
		Resources: []string{"report"},
		Cells: map[string][]store.Grant{
			"view":   {{Resource: "report", Action: "view"}},
			"export": {{Resource: "report", Action: "export"}},
		},
	},
	{
		Key:       "administration",
		Label:     "Administration",
		Resources: []string{"user", "role", "notification", "config", "audit", "backup"},
		Cells: map[string][]store.Grant{
			"view": {
				{Resource: "user", Action: "view"}, {Resource: "role", Action: "view"},
				{Resource: "notification", Action: "view"}, {Resource: "config", Action: "view"},
				{Resource: "audit", Action: "view"}, {Resource: "backup", Action: "view"},
			},
			"create": {{Resource: "user", Action: "create"}, {Resource: "role", Action: "create"}, {Resource: "backup", Action: "create"}},
			"edit": {
				{Resource: "user", Action: "edit"}, {Resource: "role", Action: "edit"},
				{Resource: "notification", Action: "edit"}, {Resource: "config", Action: "edit"},
			},
			"cancel": {{Resource: "role", Action: "delete"}},
		},
	},
}

// permAdvanced is one canonical action inside a row's Advanced disclosure.
// Cell is the "<group>:<column>" value of the cell that also covers it, so the
// screen can keep the two in step; it is empty for an action no cell reaches.
type permAdvanced struct {
	Resource string
	Action   string
	Label    string
	Cell     string
	Granted  bool
}

type permCell struct {
	Column    string
	Label     string
	Value     string // "<group>:<column>" — the submitted form value
	Available bool   // false renders an em dash instead of a checkbox
	Granted   bool   // every canonical action behind the cell is held
	Partial   bool   // some, but not all, are held
}

type permRow struct {
	Key           string
	Label         string
	Cells         []permCell
	Advanced      []permAdvanced
	Held          int    // fully granted cells — the mobile "N of 7" badge
	Total         int    // len(permColumns)
	Scoped        bool   // the row owns a data-scoped resource
	ScopeResource string // which one; the form field is "scope_" + this
	Scope         string // "", "own", "assigned" or "all"
}

func permGroup(key string) (permGroupDef, bool) {
	for _, group := range permGroups {
		if group.Key == key {
			return group, true
		}
	}
	return permGroupDef{}, false
}

// actionsFor lists the canonical actions the row maps for one of its resources,
// in column order and deduplicated. It is what the Advanced disclosure
// enumerates. Deriving it from the cells rather than from a second copy of the
// vocabulary keeps permGroups the only place a grant is written down;
// permmap_test.go is what proves the derivation misses nothing canonical.
func (g permGroupDef) actionsFor(resource string) []string {
	var out []string
	seen := map[string]bool{}
	for _, column := range permColumns {
		for _, grant := range g.Cells[column.Key] {
			if grant.Resource != resource || seen[grant.Action] {
				continue
			}
			seen[grant.Action] = true
			out = append(out, grant.Action)
		}
	}
	return out
}

// isScopedResource reports whether a resource carries a data scope. store keeps
// its scopedResources set unexported and exposes only ValidScope, which is true
// for exactly the scoped resources when handed a valid scope value.
func isScopedResource(resource string) bool {
	return store.ValidScope(resource, store.ScopeAll)
}

// expandCells turns submitted "<group>:<column>" cell values into the full set
// of canonical grants behind them, deduplicated. Unknown rows, unknown columns
// and cells with no canonical action expand to nothing, so a hand-crafted POST
// can never invent a grant.
func expandCells(cells []string) []store.Grant {
	var out []store.Grant
	seen := map[store.Grant]bool{}
	for _, raw := range cells {
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			continue
		}
		group, ok := permGroup(parts[0])
		if !ok {
			continue
		}
		for _, grant := range group.Cells[parts[1]] {
			if seen[grant] {
				continue
			}
			seen[grant] = true
			out = append(out, grant)
		}
	}
	return out
}

// buildPermMatrix renders one role's stored grants and scopes as the drawn
// matrix. Both the desktop table and the mobile accordion are built from this
// one result, so they can never disagree.
func buildPermMatrix(grants []store.Grant, scopes []store.ScopeGrant) []permRow {
	held := map[store.Grant]bool{}
	for _, grant := range grants {
		held[grant] = true
	}
	scope := map[string]string{}
	for _, sc := range scopes {
		scope[sc.Resource] = sc.Scope
	}

	rows := make([]permRow, 0, len(permGroups))
	for _, group := range permGroups {
		row := permRow{Key: group.Key, Label: group.Label, Total: len(permColumns)}
		columnOf := map[store.Grant]string{}
		for _, column := range permColumns {
			behind := group.Cells[column.Key]
			cell := permCell{
				Column:    column.Key,
				Label:     column.Label,
				Value:     group.Key + ":" + column.Key,
				Available: len(behind) > 0,
			}
			got := 0
			for _, grant := range behind {
				columnOf[grant] = column.Key
				if held[grant] {
					got++
				}
			}
			cell.Granted = cell.Available && got == len(behind)
			cell.Partial = got > 0 && !cell.Granted
			if cell.Granted {
				row.Held++
			}
			row.Cells = append(row.Cells, cell)
		}
		for _, resource := range group.Resources {
			for _, action := range group.actionsFor(resource) {
				grant := store.Grant{Resource: resource, Action: action}
				cell := ""
				if column, ok := columnOf[grant]; ok {
					cell = group.Key + ":" + column
				}
				row.Advanced = append(row.Advanced, permAdvanced{
					Resource: resource,
					Action:   action,
					Label:    entityText(resource) + " · " + strings.ReplaceAll(action, "_", " "),
					Cell:     cell,
					Granted:  held[grant],
				})
			}
			if isScopedResource(resource) {
				row.Scoped = true
				row.ScopeResource = resource
				row.Scope = scope[resource]
			}
		}
		rows = append(rows, row)
	}
	return rows
}
