package app

// The app shell: the sidebar navigation, the mobile tab bar and the badge
// counts. It is driven by permissions only and never branches on a role name
// (a test checks this file for role names). Every nav item declares the
// resource and action it needs; items with no resource are shown to every
// signed-in user, and items marked Soon are announcements, not links.

import (
	"net/http"
	"strings"

	"fervidbudget/internal/store"
)

// NavItem is one sidebar entry. Badge names the count shown beside it.
type NavItem struct {
	Key, Label, Href, Icon string
	Resource, Action       string
	Badge                  string
	Soon                   bool
}

// NavGroup is a titled group of sidebar entries.
type NavGroup struct {
	Title string
	Items []NavItem
}

// TabItem is one entry of the mobile tab bar.
type TabItem struct {
	Key, Label, Href, Icon string
}

// TabBar is the mobile tab bar: up to two tabs each side of a centre action.
type TabBar struct {
	Left  []TabItem
	Fab   TabItem
	Right []TabItem
}

// Shell is everything the page chrome needs for one render.
type Shell struct {
	Groups                                        []NavGroup
	Tabs                                          TabBar
	Active                                        string
	Badges                                        map[string]int
	Unread                                        int
	Chrome                                        string // "app" | "none"
	Title, Sub, BackHref, ActionLabel, ActionHref string
	// RoleNames is the caption under the signed-in person's name: the names of
	// the roles they hold, filled in by renderStatus.
	RoleNames string
}

// navSpec is the full sidebar navigation, in display order. Each item is
// gated on the same permission as its route.
var navSpec = []NavGroup{
	{Title: "", Items: []NavItem{
		{Key: "dashboard", Label: "Home", Href: "/", Icon: "⌂"},
		// The user's own notification centre. On desktop this is its only entry
		// (the top-bar bell is phone-only). No permission: the page only shows
		// the user's own rows. The badge is the unread count.
		{Key: "notifications", Label: "Notifications", Href: "/notifications", Icon: "✉", Badge: "notifications"},
	}},
	// A Soon item is a screen that does not exist yet: it renders as an
	// announcement, not a link. TestEveryLinkedNavItemResolves checks that
	// every linked item has a route.
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
		// The notification rules editor, not the user's own centre above.
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
			if item.Resource != "" && !can(perms, item.Resource, item.Action) {
				continue
			}
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
	}
	if err != nil || badges == nil {
		badges = map[string]int{}
	}
	shell.Badges = badges
	// The unread count needs no permission (the rows are the user's own). If
	// it cannot be read the badge is simply left off.
	if unread, err := a.st.UnreadNotificationCount(r.Context(), user.ID); err != nil {
		a.log.WarnContext(r.Context(), "unread notification count unavailable",
			"request_id", requestID(r),
			"error", err,
		)
	} else {
		shell.Unread = unread
		shell.Badges["notifications"] = unread
	}
	a.workQueueBadges(r, user, perms, shell.Badges)
	return shell
}

// workQueueBadges adds the Approvals and Accounts queue counts. They are
// computed here, not in store.BadgeCounts, because each needs the caller's
// request scope and the same query as its queue: requests routed to this
// approver, and approved requests nobody has claimed that are not on hold.
// Each is computed only for a holder of its nav item's permission, and a
// failure only hides that badge.
func (a *App) workQueueBadges(r *http.Request, user store.User, perms store.PermissionSet, badges map[string]int) {
	if can(perms, "approval", "approve") {
		// Everything the approvals queue lists as waiting on this approver,
		// including partial-payment reviews for those who may decide them.
		statuses := []string{"pending"}
		if can(perms, "approval", "accept_partial") {
			statuses = append(statuses, "partial_review")
		}
		n, err := a.st.CountRequests(r.Context(), store.RequestListOptions{
			Scope: perms.Scope("request"), ViewerID: user.ID, RoutedToViewer: true, Statuses: statuses})
		if err != nil {
			a.log.WarnContext(r.Context(), "approvals badge unavailable",
				"request_id", requestID(r), "error", err)
		} else {
			badges["approvals"] = n
		}
	}
	if can(perms, "payment", "process") {
		set, err := a.st.LinkablePaymentRequests(r.Context(), store.LinkableOptions{
			Scope: a.auth.Scope(user, "request"), ViewerID: user.ID, Status: "approved", Limit: 1})
		if err != nil {
			a.log.WarnContext(r.Context(), "accounts queue badge unavailable",
				"request_id", requestID(r), "error", err)
		} else {
			badges["accounts_queue"] = set.Counts.Approved
		}
	}
}

// chromeApp and chromeNone are the two Shell.Chrome values: the full app
// chrome, or none (signed-out pages, htmx fragments, the error page).
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

// homeTab and moreTab are the fixed tabs: Home first, the More sheet last.
var homeTab = TabItem{Key: "dashboard", Label: "Home", Href: "/", Icon: "⌂"}
var moreTab = TabItem{Key: "more", Label: "More", Href: "#more", Icon: "⋯"}

// resolveTabs builds the mobile tab bar from permissions alone. Left and right
// hold at most two items each and More is always the last one.
func resolveTabs(perms store.PermissionSet) TabBar {
	tabs := TabBar{Left: []TabItem{homeTab}, Fab: homeTab}
	// The tab bar is a phone's only navigation, so every tab is gated on the
	// same permission as its route: a tab that leads to a refusal is worse
	// than no tab.
	grid := TabItem{Key: "variance-grid", Label: "Budget", Href: "/grid", Icon: "▥"}
	if requests := (TabItem{Key: "requests-list", Label: "Requests", Href: "/requests", Icon: "▤"}); can(perms, "request", "view") {
		tabs.Left = append(tabs.Left, requests)
	} else if can(perms, "grid", "view") {
		tabs.Left = append(tabs.Left, grid)
	}
	for _, candidate := range centreActions {
		if can(perms, candidate.Resource, candidate.Action) {
			tabs.Fab = candidate.Tab
			break
		}
	}
	switch {
	case can(perms, "payment", "view"):
		tabs.Right = append(tabs.Right, TabItem{Key: "payments", Label: "Payments", Href: "/payments", Icon: "▦"})
	case can(perms, "grid", "view"):
		tabs.Right = append(tabs.Right, grid)
	}
	tabs.Right = append(tabs.Right, moreTab)
	return tabs
}

// can reports whether perms grants action on resource; a nil set grants
// nothing.
func can(perms store.PermissionSet, resource, action string) bool {
	return perms != nil && perms.Can(resource, action)
}
