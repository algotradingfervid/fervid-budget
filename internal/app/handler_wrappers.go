package app

// Wrappers that sit between a route's permission gate and its handler:
// the form parser with the CSRF check, the plain body-size cap, and the
// "any one of these permissions" gate.

import (
	"errors"
	"net/http"
	"strings"

	"fervidbudget/internal/auth"
)

// requireAnyOf is RequirePermission for a route whose action depends on the
// submitted form (such as POST /users: create or edit). It lets through a
// caller holding at least one of actions on resource and refuses everyone
// else with the same 403 and sentence RequirePermission uses, before the form
// is read. The handler must still check the exact action it needs.
func (a *App) requireAnyOf(resource string, actions []string, next http.Handler) http.Handler {
	return a.auth.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := auth.CurrentUser(r)
		for _, action := range actions {
			if a.auth.Can(u, resource, action) {
				next.ServeHTTP(w, r)
				return
			}
		}
		a.respondError(w, r, http.StatusForbidden, "You do not have permission to perform this action.", nil)
	}))
}

// withBodyCap caps the request body at requestBodyLimit, like withCSRF, but
// checks no token. It is for POST /login, which has no session and so no
// token, but must not read an unlimited body.
func (a *App) withBodyCap(fn func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, requestBodyLimit(r))
		fn(w, r)
	}
}

// withCSRF wraps every form POST except sign-in itself. In order, it:
//
//  1. caps the body at requestBodyLimit;
//  2. parses the form, multipart or urlencoded (413 when too large, 400 when
//     unreadable);
//  3. checks the CSRF token (403 "Your form session expired" when wrong);
//  4. for a path under /requests/{id}, answers 404 unless the caller may see
//     that request, so request handlers can rely on it being visible;
//  5. calls fn, and afterwards deletes any uploaded temporary files.
func (a *App) withCSRF(fn func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, requestBodyLimit(r))
		var err error
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
			err = r.ParseMultipartForm(1 << 20)
		} else {
			err = r.ParseForm()
		}
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				a.respondError(w, r, http.StatusRequestEntityTooLarge, "The submitted form is too large.", err)
			} else {
				a.respondError(w, r, http.StatusBadRequest, "The submitted form could not be read.", err)
			}
			return
		}
		if !a.auth.CheckCSRF(r) {
			a.respondError(w, r, http.StatusForbidden, "Your form session expired. Refresh the page and try again.", nil)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/requests/") && pathID(r) > 0 {
			if _, ok := a.loadViewableRequest(w, r); !ok {
				return
			}
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		fn(w, r)
	}
}
