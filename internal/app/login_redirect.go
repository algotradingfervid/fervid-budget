package app

import (
	"context"
	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (a *App) authorizedLoginNext(r *http.Request, u store.User, raw string) string {
	target := auth.SafeReturnPath(raw)
	parsed, _ := url.Parse(target)
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if target == "/" {
		return target
	}
	resources := map[string]string{"grid": "grid", "months": "month", "budgets": "budget", "projects": "project", "heads": "head", "vendors": "vendor", "requests": "request", "payments": "payment", "reports": "report", "recoverables": "recoverable_report", "users": "user", "roles": "role", "configuration": "config", "audit": "audit", "backups": "backup", "notifications": "notification", "admin": "notification"}
	if !knownLoginPage(parts) {
		return "/"
	}
	resource := resources[parts[0]]
	action := "view"
	if parts[0] == "approvals" {
		resource, action = "approval", "approve"
	}
	if parts[0] == "accounts-queue" {
		resource, action = "payment", "process"
	}
	if len(parts) > 1 && parts[1] == "new" {
		action = "create"
	}
	if resource == "" || !a.auth.Can(u, resource, action) {
		return "/"
	}
	if resource == "role" && parsed.Query().Get("role") != "" {
		id, err := strconv.ParseInt(parsed.Query().Get("role"), 10, 64)
		if err != nil || id <= 0 {
			return "/"
		}
		if _, err = a.st.Role(r.Context(), id); err != nil {
			return "/"
		}
	}
	if len(parts) > 1 {
		if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil && id > 0 {
			if resource == "vendor" {
				if _, err := a.st.Vendor(r.Context(), id, a.auth.Permissions(u)); err != nil {
					return "/"
				}
				if len(parts) == 3 {
					resourceForTab := map[string]string{"requests": "request", "payments": "payment", "history": "audit"}[parts[2]]
					if !a.auth.Can(u, resourceForTab, "view") {
						return "/"
					}
				}
			}
			if resource == "recoverable_report" {
				req, err := a.st.Request(r.Context(), id)
				if err != nil || req.Treatment != "recoverable" || !a.userSeesRequest(u, req) {
					return "/"
				}
			}
			if resource == "request" {
				req, err := a.st.Request(r.Context(), id)
				if err != nil || !a.userSeesRequest(u, req) {
					return "/"
				}
			}
			if resource == "payment" {
				// The redirect may land only where the payment page itself would
				// open, so it asks the page's own rule. A payment linked to a
				// request is readable when the request data scope reaches that
				// request (payment:view was already required above); a payment
				// with no request is readable when the payment data scope reaches
				// it — "own" and "assigned" mean payments this person entered,
				// "all" means every payment, and no scope means none.
				pay, err := a.st.Payment(r.Context(), id)
				if err != nil || !a.userReadsPayment(r.Context(), u, pay) {
					return "/"
				}
			}
		}
	}
	return target
}

func knownLoginPage(parts []string) bool {
	if len(parts) == 1 {
		switch parts[0] {
		case "grid", "months", "budgets", "projects", "heads", "vendors", "requests", "payments", "recoverables", "users", "roles", "configuration", "audit", "backups", "notifications", "approvals", "accounts-queue":
			return true
		}
		return false
	}
	if len(parts) == 2 {
		switch parts[0] {
		case "reports":
			return parts[1] == "monthly" || parts[1] == "projects" || parts[1] == "heads"
		case "admin":
			return parts[1] == "notifications"
		case "recoverables":
			if parts[1] == "list" {
				return true
			}
		case "vendors", "requests", "payments":
			if parts[1] == "new" {
				return true
			}
		default:
			return false
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		return err == nil && id > 0
	}
	if len(parts) == 3 && parts[0] == "vendors" {
		id, err := strconv.ParseInt(parts[1], 10, 64)
		return err == nil && id > 0 && (parts[2] == "requests" || parts[2] == "payments" || parts[2] == "history")
	}
	return false
}

const loginFallbackNotice = "You’re signed in. The requested page isn’t available to your account, so we’ve opened Home."

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
