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
// There is no wildcard to lean on any more — matching is exact.
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
// the unbuiltPrefixes switch instead of duplicating the list. When a phase
// builds a screen and deletes its line there, this starts expecting it.
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
		// These lists are the screens that exist *today*. "My requests",
		// "Accounts queue" and "Recoverables" are permitted but not built yet,
		// so they carry Soon and appear in the coming-soon count below instead.
		// The phase that builds each one drops its flag and moves it here.
		{
			name:  "data entry sees only its own screens",
			perms: dataEntryPerms(),
			want:  []string{"Home", "Payments ledger", "Reports", "Variance grid"},
		},
		{
			name:  "no grants leaves only ungated items",
			perms: store.NewPermissionSet(nil, nil),
			want:  []string{"Home"},
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
	// Four future products (Invoices, Payments received, Inventory, Purchase
	// orders) plus the three permitted-but-unbuilt request screens. A Soon item
	// may carry the href its screen will occupy; the template renders it
	// without one, so it cannot become a clickable dead end.
	soon := 0
	for _, group := range entryGroups {
		for _, item := range group.Items {
			if item.Soon {
				soon++
			}
		}
	}
	if soon != 7 {
		t.Fatalf("coming-soon items visible to data_entry = %d, want 7", soon)
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
		wantRight []string
	}{
		// The centre action falls through any candidate whose screen is not
		// built: /approvals and /requests/new are still in unbuiltPrefixes, so
		// an approver currently lands on Pay. Deleting those lines when Phase 2
		// ships flips these expectations back to Approve and New, and
		// TestTabBarNeverLinksToAnUnbuiltRoute is what keeps the two in step.
		{
			name:      "approver falls through to pay until /approvals exists",
			perms:     store.NewPermissionSet([]store.Grant{{Resource: "approval", Action: "approve"}, {Resource: "payment", Action: "create"}, {Resource: "request", Action: "create"}, {Resource: "payment", Action: "view"}}, nil),
			wantLabel: "Pay",
			wantHref:  "/payments/new",
			wantRight: []string{"Payments", "More"},
		},
		{
			name:      "payer gets pay",
			perms:     store.NewPermissionSet([]store.Grant{{Resource: "payment", Action: "create"}, {Resource: "request", Action: "create"}, {Resource: "payment", Action: "view"}}, nil),
			wantLabel: "Pay",
			wantHref:  "/payments/new",
			wantRight: []string{"Payments", "More"},
		},
		{
			name:      "requester falls back to home until /requests/new exists",
			perms:     store.NewPermissionSet([]store.Grant{{Resource: "request", Action: "create"}}, nil),
			wantLabel: "Home",
			wantHref:  "/",
			wantRight: []string{"Budget", "More"},
		},
		{
			name:      "no grants falls back to home",
			perms:     store.NewPermissionSet(nil, nil),
			wantLabel: "Home",
			wantHref:  "/",
			wantRight: []string{"Budget", "More"},
		},
		{
			name:      "every grant takes the first built match",
			perms:     adminPerms(),
			wantLabel: "Pay",
			wantHref:  "/payments/new",
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
			// The Requests tab is replaced by Budget while /requests is
			// unbuilt, and dropped entirely for a user without grid:view.
			wantLeft := "Home"
			if routeBuilt("/requests") {
				wantLeft = "Home|Requests"
			} else if testCase.perms != nil && testCase.perms.Can("grid", "view") {
				wantLeft = "Home|Budget"
			}
			if got := tabLabels(tabs.Left); strings.Join(got, "|") != wantLeft {
				t.Fatalf("left tabs = %v, want %s", got, wantLeft)
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

// The gates and the engine have to speak one language. Phase 0 wrote these
// against interim Casbin verbs; point the DB engine at a verb the vocabulary
// does not carry and the item silently disappears for everyone, forever.
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
			// A Soon item may declare the href its screen will occupy — that is
			// how the phase which builds it knows where to land, and
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
// this is what stops a phase shipping a nav entry that 404s -- and what forces
// the phase that does build the screen to drop the flag.
func TestEveryLinkedNavItemResolves(t *testing.T) {
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

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
					item.Label, item.Href, resp.StatusCode, truncateForLog(body))
			}
		}
	}
}

func truncateForLog(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// The tab bar is the only navigation a phone has, so a tab pointing at a
// screen that does not exist is a dead end with no way around it. This fails
// until the phase that builds the screen removes its unbuiltPrefixes entry.
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
