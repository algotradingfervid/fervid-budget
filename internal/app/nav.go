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
		// G19's own centre, and the only entry point to it above 860 px. The
		// .m-topbar bell is display:none on every desktop, and navSpec had no item,
		// so a desktop user had no way to discover their notifications existed
		// (F-F-05). No Resource: every row the screen can return is already scoped
		// to the signed-in user by the store, which is why the route itself needs no
		// verb either. The badge is the unread count, injected in buildPageShell.
		{Key: "notifications", Label: "Notifications", Href: "/notifications", Icon: "✉", Badge: "notifications"},
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
	if badges == nil {
		badges = map[string]int{}
	}
	shell.Badges = badges
	// The bell is the one count that is not permission-derived: every row is
	// already addressed to this user, so there is nothing to gate. A failure
	// here must not cost the page its chrome — the badge just stays absent.
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

// workQueueBadges fills in the two badges navSpec has always declared and no
// query ever populated (F-G-013): a manager with pending approvals saw no badge
// on Approvals, and an accountant saw none on the Accounts queue — the two counts
// a person most needs at a glance were the two that were never built, which is the
// opposite of D4.
//
// They are computed here rather than as store badgeSpecs because neither is a
// scalar sub-select over one table the way the other three are: the approvals
// count needs the caller's `assigned` scope, and the queue count needs the
// takeable predicate (approved · unclaimed · not on hold) that
// LinkablePaymentRequests already owns and CountRequests cannot express. A
// batched read in the store is the better long-term home; a looser count is not.
//
// Each is gated on the same verb its nav item is, so an unauthorised count is
// never computed. A failure costs the badge and never the page.
func (a *App) workQueueBadges(r *http.Request, user store.User, perms store.PermissionSet, badges map[string]int) {
	if can(perms, "approval", "approve") {
		n, err := a.st.CountRequests(r.Context(), store.RequestListOptions{
			Scope: "assigned", ViewerID: user.ID, Statuses: []string{"pending"}})
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
	// Every tab is gated on the verb its own route is gated on. A tab that 403s is
	// worse than an absent one on a phone, where the bar is the only navigation
	// there is: /grid is now RequirePermission("grid","view") (F-A-02/F-G-032), so
	// the Budget fallback below used to hand a Requester — who holds no grid:view —
	// a tab that refused them, and a role-less account two of them.
	grid := TabItem{Key: "variance-grid", Label: "Budget", Href: "/grid", Icon: "▥"}
	if requests := (TabItem{Key: "requests-list", Label: "Requests", Href: "/requests", Icon: "▤"}); can(perms, "request", "view") && routeBuilt(requests.Href) {
		tabs.Left = append(tabs.Left, requests)
	} else if can(perms, "grid", "view") {
		tabs.Left = append(tabs.Left, grid)
	}
	for _, candidate := range centreActions {
		if can(perms, candidate.Resource, candidate.Action) && routeBuilt(candidate.Tab.Href) {
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

func can(perms store.PermissionSet, resource, action string) bool {
	return perms != nil && perms.Can(resource, action)
}

// unbuiltPrefixes lists the screens the navigation knows about but no route
// serves yet. It is the single switch: the sidebar renders these as Soon
// announcements and the mobile tab bar skips them, so a phase that builds a
// screen deletes one line here and the entry lights up in both places at once.
// TestEveryLinkedNavItemResolves and TestTabBarNeverLinksToAnUnbuiltRoute fail
// until the line goes, so it cannot be forgotten.
// Every screen the navigation knows about is now built, so this list is empty.
// It stays because it is the switch a future phase flips: add the prefix while
// the nav entry exists but the route does not, and delete it when it does.
var unbuiltPrefixes []string

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
