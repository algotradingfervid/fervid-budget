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

// requireAnyOf is RequirePermission for a route one of whose actions depends on
// the submitted form. It refuses a caller holding none of the named actions with
// the same status and the same sentence auth.RequirePermission uses, so the
// refusal is indistinguishable, and it runs where every other permission gate
// runs: outside withCSRF, before anything is parsed. The handler behind it must
// still demand the specific action the request turns out to need — this only
// establishes that the caller has business here at all.
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

// withBodyCap is the body limit half of withCSRF, on its own, for the one POST
// that is deliberately outside the token check. An unauthenticated endpoint
// that calls ParseForm on an unbounded body is a free memory sink; every other
// POST refuses at maxRequestBodyBytes and so does this one (F-A-10).
func (a *App) withBodyCap(fn func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, requestBodyLimit(r))
		fn(w, r)
	}
}

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
