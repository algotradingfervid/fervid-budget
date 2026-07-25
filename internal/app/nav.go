package app

import (
	"net/http"
	"strings"

	"fervidbudget/internal/store"
)

// The shell is permission-driven. Nothing in this file may branch on a role
// name: every item declares the resource and action it needs and the
// PermissionSet decides. Items with no Resource are available to every signed
// in user; items marked Soon are announcements, not links, and are shown to
// everyone exactly as the approved mockup does.

type NavItem struct {
	Key, Label, Href, Icon string
	Resource, Action       string
	Badge                  string
	Soon                   bool
}

type NavGroup struct {
	Title string
	Items []NavItem
}

type TabItem struct {
	Key, Label, Href, Icon string
}

type TabBar struct {
	Left  []TabItem
	Fab   TabItem
	Right []TabItem
}

type Shell struct {
	Groups                                        []NavGroup
	Tabs                                          TabBar
	Active                                        string
	Badges                                        map[string]int
	Unread                                        int
	Chrome                                        string // "app" | "none"
	Title, Sub, BackHref, ActionLabel, ActionHref string
}

// navSpec is the full navigation of the product, in the order the approved
// mockup lays it out. Screens that are not built yet still carry the resource
// and action they will be gated on, so they light up the moment their route
// and policy exist.
var navSpec = []NavGroup{
	{Title: "", Items: []NavItem{
		{Key: "dashboard", Label: "Home", Href: "/", Icon: "⌂"},
	}},
	// Soon marks a screen whose route does not exist yet: it renders as an
	// announcement, not a link, so nobody can click through to a 404. The
	// phase that builds the screen drops the flag, and
	// TestEveryLinkedNavItemResolves fails until it does.
	{Title: "Requests", Items: []NavItem{
		{Key: "requests-list", Label: "My requests", Href: "/requests", Icon: "▤", Resource: "request", Action: "view"},
		{Key: "approvals", Label: "Approvals", Href: "/approvals", Icon: "✓", Resource: "approval", Action: "approve", Badge: "approvals"},
		{Key: "accounts-queue", Label: "Accounts queue", Href: "/accounts-queue", Icon: "₹", Resource: "payment", Action: "process", Badge: "accounts_queue"},
		{Key: "recoverables", Label: "Recoverables", Href: "/recoverables", Icon: "↩", Resource: "recoverable_report", Action: "view"},
	}},
	{Title: "Payments", Items: []NavItem{
		{Key: "payments", Label: "Payments ledger", Href: "/payments", Icon: "▦", Resource: "payment", Action: "view", Badge: "receipts_missing"},
	}},
	{Title: "Budget", Items: []NavItem{
		{Key: "variance-grid", Label: "Variance grid", Href: "/grid", Icon: "▥", Resource: "grid", Action: "view"},
		{Key: "budgets", Label: "Budgets", Href: "/budgets", Icon: "◴", Resource: "budget", Action: "view"},
		{Key: "monthly-plans", Label: "Monthly plans", Href: "/months", Icon: "◷", Resource: "month", Action: "view", Badge: "open_months"},
	}},
	{Title: "Reports", Items: []NavItem{
		{Key: "reports", Label: "Reports", Href: "/reports/monthly", Icon: "⤓", Resource: "report", Action: "view"},
	}},
	{Title: "Masters", Items: []NavItem{
		{Key: "vendors", Label: "Vendors", Href: "/vendors", Icon: "◫", Resource: "vendor", Action: "view"},
		{Key: "projects", Label: "Projects", Href: "/projects", Icon: "▣", Resource: "project", Action: "view"},
		{Key: "heads", Label: "Heads", Href: "/heads", Icon: "≡", Resource: "head", Action: "view"},
	}},
	{Title: "Admin", Items: []NavItem{
		{Key: "users", Label: "Users", Href: "/users", Icon: "◍", Resource: "user", Action: "view"},
		{Key: "roles", Label: "Roles & permissions", Href: "/roles", Icon: "⚿", Resource: "role", Action: "view"},
		{Key: "configuration", Label: "Configuration", Href: "/configuration", Icon: "⚙", Resource: "config", Action: "view"},
		// Two different screens, per adoption spec D7: /admin/notifications is
		// the rule editor, /notifications (the topbar bell) is the user's own
		// in-app centre. Phase 5 builds both.
		{Key: "notif-admin", Label: "Notification rules", Href: "/admin/notifications", Icon: "✉", Resource: "notification", Action: "view"},
		{Key: "audit", Label: "Audit log", Href: "/audit", Icon: "◎", Resource: "audit", Action: "view"},
		{Key: "backups", Label: "Backups", Href: "/backups", Icon: "⇪", Resource: "backup", Action: "view"},
	}},
	{Title: "Coming soon", Items: []NavItem{
		{Key: "invoices", Label: "Invoices", Icon: "▧", Soon: true},
		{Key: "receipts", Label: "Payments received", Icon: "↧", Soon: true},
		{Key: "inventory", Label: "Inventory", Icon: "▨", Soon: true},
		{Key: "po", Label: "Purchase orders", Icon: "▩", Soon: true},
	}},
}

// buildShell filters navSpec down to what the holder of perms may reach.
// Groups left with no items disappear entirely.
func buildShell(perms store.PermissionSet) []NavGroup {
	groups := make([]NavGroup, 0, len(navSpec))
	for _, group := range navSpec {
		items := make([]NavItem, 0, len(group.Items))
		for _, item := range group.Items {
			if !navItemVisible(item, perms) {
				continue
			}
			// An item whose route does not exist yet is announced, never
			// linked. Derived here rather than hand-flagged so the switch
			// stays in one place.
			item.Soon = item.Soon || !routeBuilt(item.Href)
			items = append(items, item)
		}
		if len(items) == 0 {
			continue
		}
		groups = append(groups, NavGroup{Title: group.Title, Items: items})
	}
	return groups
}

// buildPageShell assembles everything the chrome needs for one render: the nav
// the user may reach, their tab bar, the active item and the badge counts they
// are entitled to see. A signed-out request gets no chrome at all. The caller
// resolves the permission set, because the page body gates on the same one.
func (a *App) buildPageShell(r *http.Request, user store.User, perms store.PermissionSet, title string) Shell {
	if user.ID == 0 {
		return Shell{Chrome: chromeNone}
	}
	shell := Shell{
		Groups: buildShell(perms),
		Tabs:   resolveTabs(perms),
		Active: activeNavKey(r.URL.Path),
		Chrome: chromeApp,
		Title:  title,
	}
	badges, err := a.st.BadgeCounts(r.Context(), user.ID, perms)
	if err != nil {
		a.log.WarnContext(r.Context(), "badge counts unavailable",
			"request_id", requestID(r),
			"error", err,
		)
		badges = map[string]int{}
	}
	shell.Badges = badges
	return shell
}

const (
	chromeApp  = "app"
	chromeNone = "none"
)

// activeNavKey marks the nav item that owns the current path. The longest
// matching href wins, so /payments/42/edit still highlights the ledger.
func activeNavKey(path string) string {
	active := ""
	matched := 0
	for _, group := range navSpec {
		for _, item := range group.Items {
			if item.Href == "" {
				continue
			}
			if item.Href == "/" {
				if path == "/" && matched == 0 {
					active, matched = item.Key, 1
				}
				continue
			}
			if path != item.Href && !strings.HasPrefix(path, item.Href+"/") {
				continue
			}
			if len(item.Href) > matched {
				active, matched = item.Key, len(item.Href)
			}
		}
	}
	return active
}

// centreActions is the priority order of the mobile tab bar's centre action.
// The first action the holder is permitted to take wins; Home is the floor, so
// the bar always has a centre.
var centreActions = []struct {
	Resource, Action string
	Tab              TabItem
}{
	{Resource: "approval", Action: "approve", Tab: TabItem{Key: "approvals", Label: "Approve", Href: "/approvals", Icon: "✓"}},
	{Resource: "payment", Action: "create", Tab: TabItem{Key: "pay", Label: "Pay", Href: "/payments/new", Icon: "₹"}},
	{Resource: "request", Action: "create", Tab: TabItem{Key: "new", Label: "New", Href: "/requests/new", Icon: "＋"}},
}

var homeTab = TabItem{Key: "dashboard", Label: "Home", Href: "/", Icon: "⌂"}
var moreTab = TabItem{Key: "more", Label: "More", Href: "#more", Icon: "⋯"}

// resolveTabs builds the mobile tab bar from permissions alone. Left and right
// hold at most two items each and More is always the last one.
func resolveTabs(perms store.PermissionSet) TabBar {
	tabs := TabBar{Left: []TabItem{homeTab}, Fab: homeTab}
	// The tab bar is the only navigation a phone has, so a tab pointing at an
	// unbuilt screen is a dead end with no way around it. Skip those until
	// their route exists.
	if requests := (TabItem{Key: "requests-list", Label: "Requests", Href: "/requests", Icon: "▤"}); routeBuilt(requests.Href) {
		tabs.Left = append(tabs.Left, requests)
	} else if can(perms, "grid", "view") {
		tabs.Left = append(tabs.Left, TabItem{Key: "variance-grid", Label: "Budget", Href: "/grid", Icon: "▥"})
	}
	for _, candidate := range centreActions {
		if can(perms, candidate.Resource, candidate.Action) && routeBuilt(candidate.Tab.Href) {
			tabs.Fab = candidate.Tab
			break
		}
	}
	if can(perms, "payment", "view") {
		tabs.Right = append(tabs.Right, TabItem{Key: "payments", Label: "Payments", Href: "/payments", Icon: "▦"})
	} else {
		tabs.Right = append(tabs.Right, TabItem{Key: "variance-grid", Label: "Budget", Href: "/grid", Icon: "▥"})
	}
	tabs.Right = append(tabs.Right, moreTab)
	return tabs
}

func can(perms store.PermissionSet, resource, action string) bool {
	return perms != nil && perms.Can(resource, action)
}

// unbuiltPrefixes lists the screens the navigation knows about but no route
// serves yet. It is the single switch: the sidebar renders these as Soon
// announcements and the mobile tab bar skips them, so a phase that builds a
// screen deletes one line here and the entry lights up in both places at once.
// TestEveryLinkedNavItemResolves and TestTabBarNeverLinksToAnUnbuiltRoute fail
// until the line goes, so it cannot be forgotten.
var unbuiltPrefixes = []string{
	"/accounts-queue",      // Phase 2
	"/recoverables",        // Phase 4
	"/admin/notifications", // Phase 5
}

func routeBuilt(href string) bool {
	if href == "" || strings.HasPrefix(href, "#") {
		return true // not a route: an announcement, or the More sheet toggle
	}
	for _, prefix := range unbuiltPrefixes {
		if href == prefix || strings.HasPrefix(href, prefix+"/") {
			return false
		}
	}
	return true
}

// navItemVisible gates on permission alone. Soon says a screen is not built
// yet, not that it is ungated: announcing a screen the user will never be
// allowed to open is noise, and it would resurrect group headings that should
// stay hidden. Items with no resource (Home, and the product announcements in
// "Coming soon") are visible to everyone.
func navItemVisible(item NavItem, perms store.PermissionSet) bool {
	if item.Resource == "" {
		return true
	}
	if perms == nil {
		return false
	}
	return perms.Can(item.Resource, item.Action)
}
