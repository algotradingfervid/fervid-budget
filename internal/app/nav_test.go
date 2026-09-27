package app

import (
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// adminPerms is the seeded Admin role: every canonical (resource, action) pair.
// There is no wildcard: matching is exact.
func adminPerms() store.PermissionSet {
	return store.NewPermissionSet(store.AllGrants(), nil)
}

// dataEntryPerms mirrors the seeded Accounts role, which is what a legacy
// data_entry user resolves to.
func dataEntryPerms() store.PermissionSet {
	return store.NewPermissionSet([]store.Grant{
		{Resource: "request", Action: "view"},
		{Resource: "request", Action: "comment"},
		{Resource: "payment", Action: "view"},
		{Resource: "payment", Action: "create"},
		{Resource: "payment", Action: "edit"},
		{Resource: "payment", Action: "void"},
		{Resource: "payment", Action: "process"},
		{Resource: "payment", Action: "settle"},
		{Resource: "payment", Action: "mark_partial"},
		{Resource: "payment", Action: "hold"},
		{Resource: "reservation", Action: "reserve"},
		{Resource: "reservation", Action: "release"},
		{Resource: "attachment", Action: "view"},
		{Resource: "attachment", Action: "create"},
		{Resource: "grid", Action: "view"},
		{Resource: "report", Action: "view"},
		{Resource: "report", Action: "export"},
		{Resource: "recoverable_report", Action: "view"},
		{Resource: "recoverable_report", Action: "export"},
	}, nil)
}

func visibleLabels(groups []NavGroup, includeSoon bool) []string {
	labels := []string{}
	for _, group := range groups {
		for _, item := range group.Items {
			if item.Soon && !includeSoon {
				continue
			}
			labels = append(labels, item.Label)
		}
	}
	sort.Strings(labels)
	return labels
}

// specLabels mirrors buildShell's Soon derivation so the expectation tracks
// the unbuiltPrefixes switch instead of duplicating the list. When a screen is
// built and its line deleted there, this starts expecting it.
func specLabels(includeSoon bool) []string {
	spec := make([]NavGroup, 0, len(navSpec))
	for _, group := range navSpec {
		items := make([]NavItem, 0, len(group.Items))
		for _, item := range group.Items {
			item.Soon = item.Soon || !routeBuilt(item.Href)
			items = append(items, item)
		}
		spec = append(spec, NavGroup{Title: group.Title, Items: items})
	}
	return visibleLabels(spec, includeSoon)
}

func groupTitles(groups []NavGroup) []string {
	titles := []string{}
	for _, group := range groups {
		titles = append(titles, group.Title)
	}
	return titles
}

func TestBuildShellFiltersNavByPermission(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		perms store.PermissionSet
		want  []string
	}{
		{
			name:  "admin sees every non-soon item",
			perms: adminPerms(),
			want:  specLabels(false),
		},
		// These lists are the built screens each role may open; unbuilt ones
		// carry Soon and are counted in the coming-soon test instead.
		{
			name:  "data entry sees only its own screens",
			perms: dataEntryPerms(),
			want:  []string{"Accounts queue", "Home", "My requests", "Notifications", "Payments ledger", "Recoverables", "Reports", "Variance grid"},
		},
		{
			// Home and the notification centre: the two items with no Resource.
			// The centre is ungated on purpose — every row it can return is already
			// scoped to the signed-in user.
			name:  "no grants leaves only ungated items",
			perms: store.NewPermissionSet(nil, nil),
			want:  []string{"Home", "Notifications"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := visibleLabels(buildShell(testCase.perms), false)
			if strings.Join(got, "|") != strings.Join(testCase.want, "|") {
				t.Fatalf("visible items = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestBuildShellKeepsComingSoonAndDropsEmptyGroups(t *testing.T) {
	adminTitles := groupTitles(buildShell(adminPerms()))
	wantAdmin := []string{"", "Requests", "Payments", "Budget", "Reports", "Masters", "Admin", "Coming soon"}
	if strings.Join(adminTitles, "|") != strings.Join(wantAdmin, "|") {
		t.Fatalf("admin groups = %v, want %v", adminTitles, wantAdmin)
	}

	entryGroups := buildShell(dataEntryPerms())
	for _, title := range groupTitles(entryGroups) {
		if title == "Admin" || title == "Masters" {
			t.Fatalf("data_entry sees the %q group, which should be dropped when empty", title)
		}
	}
	// The four future products: Invoices, Payments received, Inventory and
	// Purchase orders. A Soon item may carry
	// the href its screen will occupy; the template renders it without one, so
	// it cannot become a clickable dead end.
	soon := 0
	for _, group := range entryGroups {
		for _, item := range group.Items {
			if item.Soon {
				soon++
			}
		}
	}
	if soon != 4 {
		t.Fatalf("coming-soon items visible to data_entry = %d, want 4", soon)
	}
}

func tabLabels(items []TabItem) []string {
	labels := []string{}
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels
}

func TestResolveTabsPicksTheCentreActionByPermission(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		perms     store.PermissionSet
		wantLabel string
		wantHref  string
		wantLeft  []string
		wantRight []string
	}{
		// The centre action falls through any candidate whose screen is not
		// built. /requests/new and /approvals both exist, so approval:approve
		// is the first candidate that resolves and an approver who can also pay
		// lands on Approve rather than Pay. TestTabBarNeverLinksToAnUnbuiltRoute
		// keeps the two in step: a screen leaves unbuiltPrefixes and its tab
		// lights up here.
		//
		// Every tab is gated on the verb its own route is gated on. wantLeft says
		// which of Requests / Budget / nothing the case is entitled to, because a
		// tab that answers 403 is a dead end on the only navigation a phone has:
		// /requests needs request:view and /grid needs grid:view, which the
		// role-less case and the bare requester hold neither of.
		{
			name:      "approver gets the approvals queue",
			perms:     store.NewPermissionSet([]store.Grant{{Resource: "approval", Action: "approve"}, {Resource: "payment", Action: "create"}, {Resource: "request", Action: "create"}, {Resource: "payment", Action: "view"}}, nil),
			wantLabel: "Approve",
			wantHref:  "/approvals",
			wantLeft:  []string{"Home"},
			wantRight: []string{"Payments", "More"},
		},
		{
			name:      "payer gets pay",
			perms:     store.NewPermissionSet([]store.Grant{{Resource: "payment", Action: "create"}, {Resource: "request", Action: "create"}, {Resource: "payment", Action: "view"}}, nil),
			wantLabel: "Pay",
			wantHref:  "/payments/new",
			wantLeft:  []string{"Home"},
			wantRight: []string{"Payments", "More"},
		},
		{
			name:      "requester gets the new-request action",
			perms:     store.NewPermissionSet([]store.Grant{{Resource: "request", Action: "create"}}, nil),
			wantLabel: "New",
			wantHref:  "/requests/new",
			wantLeft:  []string{"Home"},
			wantRight: []string{"More"},
		},
		{
			name:      "no grants falls back to home",
			perms:     store.NewPermissionSet(nil, nil),
			wantLabel: "Home",
			wantHref:  "/",
			wantLeft:  []string{"Home"},
			wantRight: []string{"More"},
		},
		{
			name:      "every grant takes the first built match",
			perms:     adminPerms(),
			wantLabel: "Approve",
			wantHref:  "/approvals",
			wantLeft:  []string{"Home", "Requests"},
			wantRight: []string{"Payments", "More"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tabs := resolveTabs(testCase.perms)
			if tabs.Fab.Label != testCase.wantLabel || tabs.Fab.Href != testCase.wantHref {
				t.Fatalf("fab = %q %q, want %q %q", tabs.Fab.Label, tabs.Fab.Href, testCase.wantLabel, testCase.wantHref)
			}
			if got := tabLabels(tabs.Right); strings.Join(got, "|") != strings.Join(testCase.wantRight, "|") {
				t.Fatalf("right tabs = %v, want %v", got, testCase.wantRight)
			}
			if got := tabLabels(tabs.Left); strings.Join(got, "|") != strings.Join(testCase.wantLeft, "|") {
				t.Fatalf("left tabs = %v, want %v", got, testCase.wantLeft)
			}
			if len(tabs.Left) > 2 || len(tabs.Right) > 2 {
				t.Fatalf("tab bar sides hold %d and %d items, want at most 2 each", len(tabs.Left), len(tabs.Right))
			}
			if last := tabs.Right[len(tabs.Right)-1]; last.Key != "more" {
				t.Fatalf("last right tab = %q, want the More sheet", last.Key)
			}
			for _, item := range append(append([]TabItem{}, tabs.Left...), tabs.Right...) {
				if item.Key == "" || item.Icon == "" || item.Href == "" {
					t.Fatalf("tab %#v is missing key, icon or href", item)
				}
			}
		})
	}
}

func TestNavContainsNoRoleNames(t *testing.T) {
	source, err := os.ReadFile("nav.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"admin"`, `"data_entry"`, `.Role`, `User.Role`} {
		if count := strings.Count(string(source), forbidden); count != 0 {
			t.Fatalf("nav.go contains %s %d times; the shell must be permission-driven", forbidden, count)
		}
	}
}

// Every nav and tab gate must name a permission in the canonical vocabulary:
// a gate on an unknown verb silently hides the item from everyone.
func TestNavAndTabGatesUseTheCanonicalVocabulary(t *testing.T) {
	for _, group := range navSpec {
		for _, item := range group.Items {
			if item.Resource == "" && item.Action == "" {
				continue // available to every signed-in user
			}
			if !store.ValidGrant(item.Resource, item.Action) {
				t.Fatalf("nav item %q gates on %s:%s, which is not in the canonical vocabulary", item.Key, item.Resource, item.Action)
			}
		}
	}
	for _, candidate := range centreActions {
		if !store.ValidGrant(candidate.Resource, candidate.Action) {
			t.Fatalf("tab bar centre action %q gates on %s:%s, which is not canonical", candidate.Tab.Key, candidate.Resource, candidate.Action)
		}
	}
}

func TestNavItemsHaveKeysIconsAndReachableHrefs(t *testing.T) {
	seen := map[string]bool{}
	for _, group := range navSpec {
		for _, item := range group.Items {
			if item.Key == "" || item.Label == "" || item.Icon == "" {
				t.Fatalf("nav item %#v is missing key, label or icon", item)
			}
			if seen[item.Key] {
				t.Fatalf("duplicate nav key %q", item.Key)
			}
			seen[item.Key] = true
			// A Soon item may declare the href its screen will occupy;
			// TestEveryLinkedNavItemResolves starts checking it the moment the
			// flag drops. The template renders Soon items without an href
			// regardless, so declaring one cannot produce a clickable dead end.
			if !item.Soon && item.Href == "" {
				t.Fatalf("nav item %q is a link with no href", item.Key)
			}
			if item.Href != "" && !strings.HasPrefix(item.Href, "/") {
				t.Fatalf("nav item %q href %q must be an absolute app path", item.Key, item.Href)
			}
			if item.Action != "" && item.Resource == "" {
				t.Fatalf("nav item %q declares an action without a resource", item.Key)
			}
		}
	}
}

// Every nav item that renders as a link must lead somewhere. Items whose
// screen is not built yet carry Soon and render as announcements instead, so
// no nav entry may 404, and building a screen means dropping its flag.
func TestEveryLinkedNavItemResolves(t *testing.T) {
	s := newAppTestServer(t)
	s.loginAdmin()

	// Iterate the built shell, not navSpec: buildShell is where Soon is derived
	// from unbuiltPrefixes, so this checks what a user is actually shown.
	for _, group := range buildShell(adminPerms()) {
		for _, item := range group.Items {
			if item.Soon || item.Href == "" {
				continue
			}
			resp := s.request(http.MethodGet, item.Href, nil, "")
			body := responseBody(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("nav item %q links to %s, which returns %d (mark it Soon until the screen exists)\n%s",
					item.Label, item.Href, resp.StatusCode, firstLines(body))
			}
		}
	}
}

// The tab bar is the only navigation a phone has, so a tab pointing at a
// screen that does not exist is a dead end with no way around it. A screen's
// unbuiltPrefixes entry must be removed once it is built.
func TestTabBarNeverLinksToAnUnbuiltRoute(t *testing.T) {
	for _, perms := range []store.PermissionSet{adminPerms(), dataEntryPerms(), store.NewPermissionSet(nil, nil)} {
		tabs := resolveTabs(perms)
		all := append([]TabItem{tabs.Fab}, tabs.Left...)
		all = append(all, tabs.Right...)
		for _, tab := range all {
			if !routeBuilt(tab.Href) {
				t.Errorf("tab %q links to %s, which no route serves", tab.Label, tab.Href)
			}
		}
	}
}
