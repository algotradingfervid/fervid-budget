package app

// The Home dashboard. Under the count strip it shows one work area per queue
// the user may act on, each listing the first few rows they can open straight
// away and linking to the full queue. Every area is gated on the same
// permission as the queue it links to, and an area with no rows is left out
// (the strip still shows its zero).

import (
	"net/http"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// WorkArea is one block on the dashboard: a heading, the rows a person can act
// on now, and a link to the full queue behind them.
type WorkArea struct {
	Key      string
	Icon     string
	Title    string
	Count    int
	Requests []store.Request
	FootText string
	FootHref string
	// Links is used by the Administration area instead of Requests: it lists
	// destinations rather than rows, rendered by the same template.
	Links []NavLink
}

// NavLink is one destination in the Administration area.
type NavLink struct{ Label, Sub, Href string }

// dashboardRows is how many rows an area lists before pointing to its queue;
// four fit above the fold on a phone.
const dashboardRows = 4

// dashboardArea describes one request-list work area and the permission that
// shows it. An empty opts.Scope means the caller's own request data scope.
type dashboardArea struct {
	resource, action                     string
	key, icon, title, footText, footHref string
	opts                                 store.RequestListOptions
}

// dashboardAreas are the request-list work areas in on-screen order. Each
// uses the same filter as the queue it links to, so the count and rows match
// that queue. The approver and accountant queues are filters inside the
// caller's request scope (routed to them, or held by them), never a scope of
// their own.
var dashboardAreas = []dashboardArea{
	{"request", "create", "needs-action", "!", "Needs your action", "Open my requests →", "/requests?bucket=needs-me",
		store.RequestListOptions{Scope: "own", Bucket: "needs-me"}},
	{"request", "create", "in-progress", "▤", "In progress", "See all my requests →", "/requests?bucket=open",
		store.RequestListOptions{Scope: "own", Bucket: "open"}},
	{"approval", "approve", "approvals", "✓", "Awaiting your approval", "Open the approvals queue →", "/approvals",
		store.RequestListOptions{RoutedToViewer: true, Statuses: []string{"pending"}}},
	{"approval", "approve", "decisions", "⏸", "Decisions only you can make", "Review all →", "/approvals?bucket=cancellations",
		store.RequestListOptions{RoutedToViewer: true, Statuses: []string{"cancellation_requested"}}},
	{"approval", "accept_partial", "partials", "₹", "Partial payments to review", "Open the partial reviews →", "/approvals?bucket=partial-review",
		store.RequestListOptions{RoutedToViewer: true, Statuses: []string{"partial_review"}}},
	// A concern the approver raised on a partial payment goes back to the
	// accountant who recorded it and still holds the reservation.
	{"payment", "process", "concerns", "?", "Concerns to answer", "Open the partial review tab →", "/accounts-queue?tab=partial_review",
		store.RequestListOptions{HeldByViewer: true, Statuses: []string{"partial_review"}, ConcernOpen: true}},
}

// dashboard renders Home: the count strip, the work areas the user may see,
// then the accounts queue area and the Administration links.
func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	perms := a.auth.Permissions(u)
	scope := perms.Scope("request")
	counts := map[string]int{}
	var areas []WorkArea

	for _, d := range dashboardAreas {
		if !perms.Can(d.resource, d.action) {
			continue
		}
		opts := d.opts
		if opts.Scope == "" {
			opts.Scope = scope
		}
		opts.ViewerID = u.ID
		// The count is the whole queue's size; only its first rows are listed.
		n, err := a.st.CountRequests(r.Context(), opts)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		counts[d.key] = n
		if n == 0 {
			continue
		}
		opts.Limit = dashboardRows
		list, err := a.st.ListRequests(r.Context(), opts)
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		areas = append(areas, WorkArea{Key: d.key, Icon: d.icon, Title: d.title, Count: n,
			Requests: list, FootText: d.footText, FootHref: d.footHref})
	}

	if perms.Can("payment", "process") {
		// The accounts queue's own query, so the count and rows match the queue
		// exactly: approved, unclaimed, not on hold, within the caller's scope.
		set, err := a.st.LinkablePaymentRequests(r.Context(), store.LinkableOptions{
			Scope: scope, ViewerID: u.ID, Status: "approved",
		})
		if err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		counts["accounts"] = set.Counts.Approved
		if set.Counts.Approved > 0 {
			rows := set.Available
			if len(rows) > dashboardRows {
				rows = rows[:dashboardRows]
			}
			areas = append(areas, WorkArea{Key: "accounts", Icon: "₹", Title: "Approved and unclaimed",
				Count: set.Counts.Approved, Requests: rows,
				FootText: "Open the payment queue →", FootHref: "/accounts-queue?tab=approved"})
		}
	}
	if links := a.adminLinks(u); len(links) > 0 {
		areas = append(areas, WorkArea{Key: "admin", Icon: "⚙", Title: "Administration", Links: links})
	}

	notice := ""
	if r.URL.Query().Get("login_fallback") == "1" {
		notice = loginFallbackNotice
	}
	a.render(w, r, "dashboard", PageData{Title: "Home", Areas: areas, Counts: counts, Notice: notice})
}

// adminLinks lists the Administration area's destinations the user may open,
// each gated on its own screen's permission. It is empty (and the area is not
// shown) when they may open none.
func (a *App) adminLinks(u store.User) []NavLink {
	var links []NavLink
	for _, candidate := range []struct {
		Resource, Action string
		Link             NavLink
	}{
		{"config", "view", NavLink{"Configuration", "Numbering, attachments, urgency, approvals", "/configuration"}},
		{"role", "view", NavLink{"Roles & permissions", "Who can do what", "/roles"}},
		{"user", "view", NavLink{"Users", "People and their default approvers", "/users"}},
		{"vendor", "view", NavLink{"Vendors", "The vendor master", "/vendors"}},
	} {
		if a.auth.Can(u, candidate.Resource, candidate.Action) {
			links = append(links, candidate.Link)
		}
	}
	return links
}
