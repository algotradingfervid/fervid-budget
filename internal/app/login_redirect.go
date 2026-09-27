package app

// Where to send a user after sign-in. The login form carries the page the
// user was trying to open (?next=). They are sent there only when it is one
// of the known pages below and they may open it (including the specific
// record in the URL); otherwise they land on Home with a notice.

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// loginFallbackNotice is shown on Home when the requested page was refused.
// It does not name the page.
const loginFallbackNotice = "You’re signed in. The requested page isn’t available to your account, so we’ve opened Home."

// loginPage describes the pages under one top-level path segment that a
// sign-in may return to, and the permission they need.
type loginPage struct {
	resource, action string
	noIndex          bool     // the bare /segment is not a page (only its sub-pages are)
	subpages         []string // allowed second segments, e.g. "new" or "monthly"
	records          bool     // /segment/{id} is a page
}

// loginPages lists every page a sign-in may return to, by first path segment,
// with the permission its route requires (none for a page open to every
// signed-in user). "/segment/new" needs the create permission instead.
var loginPages = map[string]loginPage{
	"grid":           {resource: "grid", action: "view"},
	"months":         {resource: "month", action: "view"},
	"budgets":        {resource: "budget", action: "view"},
	"projects":       {resource: "project", action: "view"},
	"heads":          {resource: "head", action: "view"},
	"vendors":        {resource: "vendor", action: "view", subpages: []string{"new"}, records: true},
	"requests":       {resource: "request", action: "view", subpages: []string{"new"}, records: true},
	"payments":       {resource: "payment", action: "view", subpages: []string{"new"}, records: true},
	"reports":        {resource: "report", action: "view", noIndex: true, subpages: []string{"monthly", "projects", "heads"}},
	"recoverables":   {resource: "recoverable_report", action: "view", subpages: []string{"list"}, records: true},
	"users":          {resource: "user", action: "view"},
	"roles":          {resource: "role", action: "view"},
	"configuration":  {resource: "config", action: "view"},
	"audit":          {resource: "audit", action: "view"},
	"backups":        {resource: "backup", action: "view"},
	"notifications":  {}, // the user's own notification centre: any signed-in user
	"admin":          {resource: "notification", action: "view", noIndex: true, subpages: []string{"notifications"}},
	"approvals":      {resource: "approval", action: "approve"},
	"accounts-queue": {resource: "payment", action: "process"},
}

// vendorTabs are the tabs of a vendor page (/vendors/{id}/{tab}) and the
// resource each one needs view permission on.
var vendorTabs = map[string]string{"requests": "request", "payments": "payment", "history": "audit"}

// authorizedLoginNext returns where to send u after sign-in: the requested
// path raw (made safe by auth.SafeReturnPath) when it is a known page that u
// may open, otherwise "/".
func (a *App) authorizedLoginNext(r *http.Request, u store.User, raw string) string {
	target := auth.SafeReturnPath(raw)
	if target == "/" {
		return target
	}
	parsed, _ := url.Parse(target)
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if !knownLoginPage(parts) {
		return "/"
	}
	page := loginPages[parts[0]]
	action := page.action
	if len(parts) > 1 && parts[1] == "new" {
		action = "create"
	}
	if page.resource != "" && !a.auth.Can(u, page.resource, action) {
		return "/"
	}
	// /roles?role={id} opens one role, which must exist.
	if page.resource == "role" && parsed.Query().Get("role") != "" {
		id, err := strconv.ParseInt(parsed.Query().Get("role"), 10, 64)
		if err != nil || id <= 0 {
			return "/"
		}
		if _, err = a.st.Role(r.Context(), id); err != nil {
			return "/"
		}
	}
	if len(parts) > 1 {
		if id := positiveID(parts[1]); id > 0 && !a.canOpenLoginRecord(r.Context(), u, page.resource, id, parts) {
			return "/"
		}
	}
	return target
}

// knownLoginPage reports whether the path segments name a page in loginPages:
// /segment, /segment/{subpage}, /segment/{id}, or a vendor tab
// /vendors/{id}/{tab}.
func knownLoginPage(parts []string) bool {
	page, ok := loginPages[parts[0]]
	if !ok {
		return false
	}
	switch len(parts) {
	case 1:
		return !page.noIndex
	case 2:
		return slices.Contains(page.subpages, parts[1]) || (page.records && positiveID(parts[1]) > 0)
	case 3:
		_, tab := vendorTabs[parts[2]]
		return parts[0] == "vendors" && positiveID(parts[1]) > 0 && tab
	}
	return false
}

// canOpenLoginRecord reports whether u may open record id under resource,
// using the same rule as that record's own page. Other resources have no
// record pages here and are always allowed.
func (a *App) canOpenLoginRecord(ctx context.Context, u store.User, resource string, id int64, parts []string) bool {
	switch resource {
	case "vendor":
		if _, err := a.st.Vendor(ctx, id, a.auth.Permissions(u)); err != nil {
			return false
		}
		return len(parts) != 3 || a.auth.Can(u, vendorTabs[parts[2]], "view")
	case "recoverable_report":
		req, err := a.st.Request(ctx, id)
		return err == nil && req.Treatment == "recoverable" && a.userSeesRequest(u, req)
	case "request":
		req, err := a.st.Request(ctx, id)
		return err == nil && a.userSeesRequest(u, req)
	case "payment":
		pay, err := a.st.Payment(ctx, id)
		return err == nil && a.userReadsPayment(ctx, u, pay)
	}
	return true
}

// positiveID parses s as a record id, returning 0 when it is not a positive
// whole number.
func positiveID(s string) int64 {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// userSeesRequest is canSeeRequest for an explicit user. The sign-in request
// has no signed-in user in its context yet, so the post-login redirect must
// check the user who has just signed in.
func (a *App) userSeesRequest(u store.User, req store.Request) bool {
	return store.RequestInScope(a.auth.Scope(u, "request"), u.ID, req)
}

// userReadsPayment is canReadPayment for an explicit user, for the same
// reason: a payment linked to a request is readable when the user's request
// scope reaches that request; one without a request needs payment:view and a
// payment scope that reaches it.
func (a *App) userReadsPayment(ctx context.Context, u store.User, p store.Payment) bool {
	if p.RequestID != nil {
		req, err := a.st.Request(ctx, *p.RequestID)
		return err == nil && a.userSeesRequest(u, req)
	}
	return a.auth.Can(u, "payment", "view") && paymentScopeReaches(a.auth.Scope(u, "payment"), u.ID, p)
}
