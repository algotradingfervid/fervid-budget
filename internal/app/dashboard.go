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
		// Bucket, not a hand-written status list. The tile links to
		// /requests?bucket=open and counted a different set — it omitted
		// `returned`, which that bucket includes — so a requester with one pending
		// and one returned request was told 1 and shown 2 (F-G-006). One predicate,
		// named once, in the place the link already names.
		if err := area("in-progress", "▤", "In progress", "See all my requests →", "/requests?bucket=open",
			store.RequestListOptions{Scope: "own", Bucket: "open"}); err != nil {
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
	// S11: a shortfall waits on the same manager, and the queue tab, the
	// "Needs me" bucket and this area all read the same predicate — before this
	// the request said "Waiting on you" while Home said nothing was
	// (settlement-3). Gated on the verb the decision routes are.
	if a.auth.Can(u, "approval", "accept_partial") {
		if err := area("partials", "₹", "Partial payments to review", "Open the partial reviews →", "/approvals?bucket=partial-review",
			store.RequestListOptions{Scope: "assigned", Statuses: []string{"partial_review"}}); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
	// The other side of the same review: a concern the manager raised is a
	// question to the accountant who recorded the shortfall, so it is listed
	// for that accountant (settlement-8). Their own reservations, the one
	// status, the flag.
	if a.auth.Can(u, "payment", "process") {
		if err := area("concerns", "?", "Concerns to answer", "Open the partial review tab →", "/accounts-queue?tab=partial_review",
			store.RequestListOptions{Scope: "held", Statuses: []string{"partial_review"}, ConcernOpen: true}); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
	}
	if a.auth.Can(u, "payment", "process") {
		// The queue's own query, not a second one over the same data.
		//
		// This area used to be built with the literal Scope: "all" written into the
		// call, so its tile and its four rows were company-wide for anybody holding
		// payment:process whatever their request scope said — and each row linked to
		// a detail page the same caller is refused (F-G-018). It also counted
		// `status='approved'` with no on_hold or processing_by test, so it disagreed
		// with the accounts queue's metric of the identical name: place a hold and
		// the queue's number dropped while this one did not (F-G-007).
		//
		// LinkablePaymentRequests answers both: it takes the caller's scope, and its
		// Counts.Approved *is* the number the queue renders — approved, unclaimed and
		// not on hold. Available is that same set as rows, so the count and the list
		// come from one query and cannot drift.
		set, err := a.st.LinkablePaymentRequests(r.Context(), store.LinkableOptions{
			Scope: a.effectiveScope(u, ""), ViewerID: u.ID, Status: "approved",
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
