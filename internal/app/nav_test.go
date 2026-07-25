package app

import (
	"os"
	"sort"
	"strings"
	"testing"

	"fervidbudget/internal/store"
)

// adminPerms mirrors the wildcard policy the admin role carries today.
func adminPerms() store.PermissionSet {
	return store.NewPermissionSet([]store.Grant{{Resource: "*", Action: "*"}})
}

// dataEntryPerms mirrors the data_entry policies registered in auth.New.
func dataEntryPerms() store.PermissionSet {
	return store.NewPermissionSet([]store.Grant{
		{Resource: "payment", Action: "create"},
		{Resource: "payment", Action: "read"},
		{Resource: "payment_attachment", Action: "create"},
		{Resource: "payment_attachment", Action: "read"},
		{Resource: "report", Action: "read"},
		{Resource: "report", Action: "export"},
		{Resource: "grid", Action: "read"},
	})
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

func specLabels(includeSoon bool) []string {
	return visibleLabels(navSpec, includeSoon)
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
		{
			name:  "data entry sees only its own screens",
			perms: dataEntryPerms(),
			want:  []string{"Home", "My requests", "Payments ledger", "Reports", "Variance grid"},
		},
		{
			name:  "no grants leaves only ungated items",
			perms: store.NewPermissionSet(nil),
			want:  []string{"Home", "My requests"},
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
	soon := 0
	for _, group := range entryGroups {
		for _, item := range group.Items {
			if item.Soon {
				soon++
				if item.Href != "" {
					t.Fatalf("coming-soon item %q must not link anywhere, got %q", item.Label, item.Href)
				}
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
		wantRight []string
	}{
		{
			name:      "approver gets approve",
			perms:     store.NewPermissionSet([]store.Grant{{Resource: "request", Action: "approve"}, {Resource: "payment", Action: "create"}, {Resource: "request", Action: "create"}, {Resource: "payment", Action: "read"}}),
			wantLabel: "Approve",
			wantHref:  "/approvals",
			wantRight: []string{"Payments", "More"},
		},
		{
			name:      "payer gets pay",
			perms:     store.NewPermissionSet([]store.Grant{{Resource: "payment", Action: "create"}, {Resource: "request", Action: "create"}, {Resource: "payment", Action: "read"}}),
			wantLabel: "Pay",
			wantHref:  "/payments/new",
			wantRight: []string{"Payments", "More"},
		},
		{
			name:      "requester gets new",
			perms:     store.NewPermissionSet([]store.Grant{{Resource: "request", Action: "create"}}),
			wantLabel: "New",
			wantHref:  "/requests/new",
			wantRight: []string{"Budget", "More"},
		},
		{
			name:      "no grants falls back to home",
			perms:     store.NewPermissionSet(nil),
			wantLabel: "Home",
			wantHref:  "/",
			wantRight: []string{"Budget", "More"},
		},
		{
			name:      "wildcard takes the first match",
			perms:     adminPerms(),
			wantLabel: "Approve",
			wantHref:  "/approvals",
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
			if got := tabLabels(tabs.Left); strings.Join(got, "|") != "Home|Requests" {
				t.Fatalf("left tabs = %v, want [Home Requests]", got)
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
			if item.Soon == (item.Href != "") {
				t.Fatalf("nav item %q: soon=%v with href %q", item.Key, item.Soon, item.Href)
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
