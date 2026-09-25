package app

import (
	"testing"

	"fervidbudget/internal/store"
)

// canonicalResourceCount and canonicalGrantCount mirror the shape of the
// canonical vocabulary in internal/store/permissions.go, which
// store.TestPermissionVocabularyIsCanonical pins from the inside (21 resources,
// 66 (resource, action) pairs).
//
// They are literals here because store keeps resourceOrder/resourceActions
// unexported and offers no enumerator — only store.ValidGrant and
// store.ValidScope cross the package boundary. That is still enough for an
// exact proof: TestPermMapCoversCanonicalVocabularyExactlyOnce checks every
// grant the matrix maps against store.ValidGrant (so the map is a *subset* of
// the vocabulary) and that the map holds exactly canonicalGrantCount distinct
// grants. A subset of the same size is the whole set, so nothing canonical can
// be orphaned. If the vocabulary ever grows, store's own test fails on its
// matching literal at the same moment this one does.
const (
	canonicalResourceCount = 21
	canonicalGrantCount    = 66
)

// This is the contract between the drawn matrix and the enforced vocabulary.
// If a canonical pair were reachable from nowhere, an admin could never grant
// it; if it were reachable from two cells, ticking one and clearing the other
// would silently disagree about the same permission. Neither is allowed.
func TestPermMapCoversCanonicalVocabularyExactlyOnce(t *testing.T) {
	if len(permGroups) != 9 {
		t.Fatalf("permGroups = %d rows, want the 9 the approved design draws", len(permGroups))
	}
	wantColumns := []string{"view", "create", "edit", "approve", "process", "cancel", "export"}
	if len(permColumns) != len(wantColumns) {
		t.Fatalf("permColumns = %d, want %d", len(permColumns), len(wantColumns))
	}
	for i, want := range wantColumns {
		if permColumns[i].Key != want {
			t.Fatalf("permColumns[%d] = %q, want %q", i, permColumns[i].Key, want)
		}
	}

	// 1. The rows partition the canonical resources.
	owner := map[string]string{}
	for _, group := range permGroups {
		for _, resource := range group.Resources {
			if prev, dup := owner[resource]; dup {
				t.Fatalf("resource %q is owned by both row %q and row %q", resource, prev, group.Key)
			}
			owner[resource] = group.Key
		}
	}
	if len(owner) != canonicalResourceCount {
		t.Fatalf("the 9 rows own %d resources, the canonical vocabulary has %d", len(owner), canonicalResourceCount)
	}

	// 2. Every cell grant is canonical, belongs to its own row, and is mapped by
	//    exactly one cell in the whole matrix.
	known := map[string]bool{}
	for _, column := range permColumns {
		known[column.Key] = true
	}
	cellOf := map[store.Grant]string{}
	reached := map[string]bool{}
	for _, group := range permGroups {
		rowResources := map[string]bool{}
		for _, resource := range group.Resources {
			rowResources[resource] = true
		}
		for key := range group.Cells {
			if !known[key] {
				t.Fatalf("row %q declares unknown column %q", group.Key, key)
			}
		}
		for _, column := range permColumns {
			for _, grant := range group.Cells[column.Key] {
				if !store.ValidGrant(grant.Resource, grant.Action) {
					t.Fatalf("cell %s:%s maps %s:%s, which is not canonical", group.Key, column.Key, grant.Resource, grant.Action)
				}
				if !rowResources[grant.Resource] {
					t.Fatalf("cell %s:%s maps %s:%s from a resource the row does not own", group.Key, column.Key, grant.Resource, grant.Action)
				}
				if prev, dup := cellOf[grant]; dup {
					t.Fatalf("%s:%s is mapped twice: by %s and by %s:%s", grant.Resource, grant.Action, prev, group.Key, column.Key)
				}
				cellOf[grant] = group.Key + ":" + column.Key
				reached[grant.Resource] = true
			}
		}
	}
	// A row may not claim a resource no cell reaches: it would count towards the
	// partition while being unreachable on screen.
	for resource, row := range owner {
		if !reached[resource] {
			t.Fatalf("row %q claims %q, but no cell maps any of its actions", row, resource)
		}
	}

	// 3. Nothing orphaned: every canonical pair sits in exactly one row's
	//    Advanced list, which is the disclosure that preserves R2.
	advanced := map[store.Grant]int{}
	for _, row := range buildPermMatrix(nil, nil) {
		for _, entry := range row.Advanced {
			grant := store.Grant{Resource: entry.Resource, Action: entry.Action}
			advanced[grant]++
			if advanced[grant] != 1 {
				t.Fatalf("%s:%s appears %d times across the Advanced lists, want exactly 1", entry.Resource, entry.Action, advanced[grant])
			}
			if !store.ValidGrant(entry.Resource, entry.Action) {
				t.Fatalf("Advanced lists %s:%s, which is not canonical", entry.Resource, entry.Action)
			}
			if _, ok := cellOf[grant]; !ok {
				t.Fatalf("%s:%s is reachable from no cell", entry.Resource, entry.Action)
			}
		}
	}
	if len(cellOf) != canonicalGrantCount {
		t.Fatalf("cells map %d grants, the canonical vocabulary has %d", len(cellOf), canonicalGrantCount)
	}
	if len(advanced) != canonicalGrantCount {
		t.Fatalf("Advanced lists cover %d grants, the canonical vocabulary has %d", len(advanced), canonicalGrantCount)
	}

	// 4. The rows that offer a scope control are exactly the resources store
	//    treats as data-scoped — no more, no fewer.
	scoped := map[string]bool{}
	for _, row := range buildPermMatrix(nil, nil) {
		if row.Scoped {
			scoped[row.ScopeResource] = true
		}
	}
	for resource := range owner {
		if want := store.ValidScope(resource, store.ScopeAll); scoped[resource] != want {
			t.Fatalf("resource %q offers a scope control = %v, want %v", resource, scoped[resource], want)
		}
	}
}

func TestExpandCellsGrantsEveryCanonicalActionBehindTheCell(t *testing.T) {
	grants := expandCells([]string{"requests:approve"})
	want := map[string]bool{"approve": true, "reject": true, "return": true, "reassign": true, "accept_partial": true}
	if len(grants) != len(want) {
		t.Fatalf("expandCells(requests:approve) = %v, want all %d approval actions", grants, len(want))
	}
	for _, g := range grants {
		if g.Resource != "approval" || !want[g.Action] {
			t.Fatalf("expandCells produced %s:%s, which is outside the cell", g.Resource, g.Action)
		}
	}
	// Duplicates collapse; an unavailable cell, an unknown row and a malformed
	// value all expand to nothing rather than to a bogus grant.
	if got := expandCells([]string{"vendors:view", "vendors:view"}); len(got) != 1 {
		t.Fatalf("expandCells deduplication = %v, want one grant", got)
	}
	if got := expandCells([]string{"requests:process", "reservations:approve", "nonsense", "nope:view"}); len(got) != 0 {
		t.Fatalf("expandCells on empty/unknown cells = %v, want none", got)
	}
	// Both cancellation verbs live behind the Payment requests Cancel cell, next
	// to request:withdraw — the vocabulary carries them (amendment A10) and the
	// matrix has to be able to grant them.
	cancel := map[store.Grant]bool{}
	for _, g := range expandCells([]string{"requests:cancel"}) {
		cancel[g] = true
	}
	for _, g := range []store.Grant{
		{Resource: "request", Action: "withdraw"},
		{Resource: "request", Action: "cancel"},
		{Resource: "approval", Action: "cancel"},
	} {
		if !cancel[g] {
			t.Fatalf("expandCells(requests:cancel) does not reach %s:%s", g.Resource, g.Action)
		}
	}
	if len(cancel) != 3 {
		t.Fatalf("expandCells(requests:cancel) = %d grants, want 3", len(cancel))
	}
}

func TestBuildPermMatrixMarksGrantedPartialAndUnavailableCells(t *testing.T) {
	held := []store.Grant{
		{Resource: "request", Action: "view"},
		{Resource: "attachment", Action: "view"},
		{Resource: "approval", Action: "approve"},
	}
	var requests permRow
	for _, row := range buildPermMatrix(held, []store.ScopeGrant{{Resource: "request", Scope: "all"}}) {
		if row.Key == "requests" {
			requests = row
		}
	}
	cells := map[string]permCell{}
	for _, cell := range requests.Cells {
		cells[cell.Column] = cell
	}
	if len(requests.Cells) != len(permColumns) {
		t.Fatalf("row rendered %d cells, want one per column (%d)", len(requests.Cells), len(permColumns))
	}
	if !cells["view"].Granted || cells["view"].Partial {
		t.Fatalf("view cell = %+v, want fully granted (request:view + attachment:view)", cells["view"])
	}
	if cells["approve"].Granted || !cells["approve"].Partial {
		t.Fatalf("approve cell = %+v, want partial (1 of the 5 approval actions)", cells["approve"])
	}
	if cells["process"].Available {
		t.Fatalf("process cell = %+v, want unavailable so the template renders an em dash", cells["process"])
	}
	if cells["create"].Granted || cells["create"].Partial {
		t.Fatalf("create cell = %+v, want untouched", cells["create"])
	}
	// The badge counts grants, not fully granted cells: three of the row's
	// fifteen canonical actions are held (rbac-5 — it used to read "1 of 7").
	if requests.Held != 3 || requests.Total != 15 {
		t.Fatalf("row badge = %d of %d, want 3 of 15", requests.Held, requests.Total)
	}
	if !requests.Scoped || requests.ScopeResource != "request" || requests.Scope != "all" {
		t.Fatalf("row scope = %q on %q (scoped=%v), want all on request", requests.Scope, requests.ScopeResource, requests.Scoped)
	}
	if got := len(requests.Advanced); got != 15 {
		t.Fatalf("Advanced list = %d entries, want 15 (request 7 + approval 6 + attachment 2)", got)
	}
	// Advanced entries carry the cell that also covers them, so the template can
	// keep the two in sync and the reader can see why a cell is partial.
	for _, entry := range requests.Advanced {
		if entry.Cell == "" {
			t.Fatalf("advanced entry %s:%s reports no cell", entry.Resource, entry.Action)
		}
		if entry.Resource == "approval" && entry.Action != "cancel" && entry.Cell != "requests:approve" {
			t.Fatalf("advanced entry %s:%s reports cell %q, want requests:approve", entry.Resource, entry.Action, entry.Cell)
		}
		if entry.Resource == "approval" && entry.Action == "cancel" && entry.Cell != "requests:cancel" {
			t.Fatalf("advanced entry approval:cancel reports cell %q, want requests:cancel", entry.Cell)
		}
		if entry.Resource == "request" && entry.Action == "view" && !entry.Granted {
			t.Fatal("advanced entry request:view should be granted")
		}
	}
	// An unscoped row offers no scope control at all: the template renders the
	// static "None" pill the mockup shows on Administration.
	for _, row := range buildPermMatrix(nil, nil) {
		if row.Key == "administration" && row.Scoped {
			t.Fatal("Administration must not be data-scoped")
		}
	}
}
