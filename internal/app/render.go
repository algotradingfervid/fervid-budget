package app

// Page rendering and error responses. Every HTML answer the app sends goes
// through renderStatus, which fills in what every page needs (user, CSRF
// token, permissions, navigation shell) before executing the template. Error
// pages are ordinary renders of the "error_page" template.

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// renderStatus renders the named template with the given HTTP status. It adds
// the fields every page shares, builds the navigation shell for full pages,
// and renders into a buffer first so a template error becomes a clean 500
// instead of half a page.
func (a *App) renderStatus(w http.ResponseWriter, r *http.Request, status int, name string, data PageData) {
	if data.Settings != nil {
		data.AttachmentMaxMB = normalizedAttachmentLimit(data.Settings["attachment_max_mb"])
	} else if limit, err := a.attachmentLimitMB(r); err == nil {
		data.AttachmentMaxMB = limit
	} else {
		data.AttachmentMaxMB = normalizedAttachmentLimit("")
	}
	data.User = auth.CurrentUser(r)
	data.CSRF = a.auth.EnsureCSRF(w, r)
	data.RequestID = requestID(r)
	if data.Month == "" {
		data.Month = time.Now().Format("2006-01")
	}
	// Permissions are resolved once here, and both the page body and the nav
	// use this same set, so a control is never shown on a screen whose own nav
	// entry is hidden.
	data.Perms = a.auth.Permissions(data.User)
	data.ContextBackHref, data.ContextBackLabel = contextReturn(r, data.Perms)
	// An htmx fragment only replaces part of a page, so it gets no nav, tab bar
	// or badge queries (its controls are still gated by data.Perms). The error
	// page also has no chrome: after a failure it offers its own two ways out
	// rather than a sidebar, and skips badge queries that could fail too.
	if isFragmentRequest(r) || name == "error_page" {
		data.Shell = Shell{Chrome: chromeNone}
	} else {
		data.Shell = a.buildPageShell(r, data.User, data.Perms, data.Title)
		// The caption under the user's name lists the roles they hold. It is
		// filled here, not in nav.go, because the navigation must depend on
		// permissions only (a test checks nav.go never mentions roles). If the
		// roles cannot be read the caption is simply left empty.
		if roles, err := a.st.UserRoles(r.Context(), data.User.ID); err == nil {
			names := make([]string, 0, len(roles))
			for _, role := range roles {
				names = append(names, role.Name)
			}
			data.Shell.RoleNames = strings.Join(names, " · ")
		}
	}

	var body bytes.Buffer
	if err := a.tpl.ExecuteTemplate(&body, name, data); err != nil {
		a.log.ErrorContext(r.Context(), "template rendering failed",
			"request_id", data.RequestID,
			"template", name,
			"error", err,
		)
		http.Error(w, "Something went wrong while preparing this page. Request ID: "+data.RequestID, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(body.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "response write failed",
			"request_id", data.RequestID,
			"template", name,
			"error", err,
		)
	}
}

// isFragmentRequest reports whether htmx is swapping part of a page rather
// than loading a whole one. htmx sets HX-Request on every request it makes,
// but a history restore (Back to a page htmx no longer has cached) replaces
// the whole body, so it needs the full page and is not a fragment.
func isFragmentRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") != "" && r.Header.Get("HX-History-Restore-Request") == ""
}

// notFound answers every URL no other route claims. It is registered on the
// catch-all "GET /" pattern (the dashboard has "GET /{$}"), so an unknown
// path shows a 404 page instead of the dashboard.
func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	a.respondError(w, r, http.StatusNotFound, "That page does not exist. It may have moved, or it may not be built yet.", nil)
}

// respondError renders the error page with the given status and message.
// When cause is set it is logged (as a warning, or an error for 5xx) with the
// request id, so the id shown on the page leads to the log line.
func (a *App) respondError(w http.ResponseWriter, r *http.Request, status int, message string, cause error) {
	if cause != nil {
		level := slog.LevelWarn
		if status >= 500 {
			level = slog.LevelError
		}
		a.log.Log(r.Context(), level, "request failed",
			"request_id", requestID(r),
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"error", cause,
		)
	}
	a.renderStatus(w, r, status, "error_page", PageData{
		Title:     http.StatusText(status),
		Error:     message,
		ErrorCode: status,
	})
}

// respondStoreError answers a store error with the status storeErrorStatus
// picks and the sentence friendly gives it. A missing record always gets the
// same fixed sentence, whatever the store said.
func (a *App) respondStoreError(w http.ResponseWriter, r *http.Request, err error) {
	status, message := storeErrorStatus(err), friendly(err)
	if status == http.StatusNotFound {
		message = "The requested record was not found."
	}
	a.respondError(w, r, status, message, err)
}

// storeErrorStatus maps a store error to its HTTP status: not found 404,
// locked month 409, refusal 403, invalid input 400, anything else 500.
func storeErrorStatus(err error) int {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, store.ErrLockedMonth):
		return http.StatusConflict
	case errors.Is(err, store.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, store.ErrValidation), errors.Is(err, store.ErrDuplicate), errors.Is(err, store.ErrInactiveHead):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
