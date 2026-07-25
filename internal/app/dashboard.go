package app

import (
	"net/http"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// The dashboard (A17).
//
// The old one was a strip of four counts. A count tells you a number; a work
// area hands you the thing to do. So the strip stays — it is the shape of the
// day at a glance — and under it sits one `.area` per queue the reader may act
// on, each listing the actual rows they can click straight into.
//
// Every area is gated on the same permission its queue is, so nobody is ever
// shown work behind a door they cannot open. An area with nothing in it is not
// rendered at all: an empty list is noise, and the strip already carries the
// zero.

// WorkArea is one `.area` block: a heading, the rows a person can act on now,
// and a link to the full queue behind them.
type WorkArea struct {
	Key      string
	Icon     string
	Title    string
	Count    int
	Requests []store.Request
	FootText string
	FootHref string
	// Links back the administration area, which has destinations rather than
	// requests. One shape, because one template renders both.
	Links []NavLink
}

type NavLink struct{ Label, Sub, Href string }

// dashboardRows is how many rows an area shows before it defers to its queue.
// Four is what fits above the fold on a phone without becoming a list.
const dashboardRows = 4

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	counts := map[string]int{}
	var areas []WorkArea

	// area counts the whole queue but lists only its head, so the count on the
	// heading is the queue's real size and never the length of what is shown.
	area := func(key, icon, title, footText, footHref string, opts store.RequestListOptions) error {
		opts.ViewerID = u.ID
		n, err := a.st.CountRequests(r.Context(), opts)
		if err != nil {
			return err
		}
		counts[key] = n
		if n == 0 {
			return nil
		}
		opts.Limit = dashboardRows
		list, err := a.st.ListRequests(r.Context(), opts)
		if err != nil {
			return err
		}
		areas = append(areas, WorkArea{Key: key, Icon: icon, Title: title, Count: n,
			Requests: list, FootText: footText, FootHref: footHref})
		return nil
	}

	if a.auth.Can(u, "request", "create") {
		if err := area("needs-action", "!", "Needs your action", "Open my requests →", "/requests?bucket=needs-me",
			store.RequestListOptions{Scope: "own", Bucket: "needs-me"}); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		if err := area("in-progress", "▤", "In progress", "See all my requests →", "/requests?bucket=open",
			store.RequestListOptions{Scope: "own", Statuses: []string{"pending", "approved", "cancellation_requested"}}); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
	if a.auth.Can(u, "approval", "approve") {
		if err := area("approvals", "✓", "Awaiting your approval", "Open the approvals queue →", "/approvals",
			store.RequestListOptions{Scope: "assigned", Statuses: []string{"pending"}}); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		if err := area("decisions", "⏸", "Decisions only you can make", "Review all →", "/approvals?bucket=cancellations",
			store.RequestListOptions{Scope: "assigned", Statuses: []string{"cancellation_requested"}}); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
	if a.auth.Can(u, "payment", "process") {
		if err := area("accounts", "₹", "Approved and unclaimed", "Open all requests →", "/requests?bucket=open",
			store.RequestListOptions{Scope: "all", Statuses: []string{"approved"}}); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
	if links := a.adminLinks(u); len(links) > 0 {
		areas = append(areas, WorkArea{Key: "admin", Icon: "⚙", Title: "Administration", Links: links})
	}

	a.render(w, r, "dashboard", PageData{Title: "Home", Areas: areas, Counts: counts})
}

// adminLinks are the administration area's destinations, each gated on the
// permission its own screen is. The area disappears entirely for somebody who
// may reach none of them.
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
