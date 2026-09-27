package app

// Sign-in and sign-out. The sign-in rate limit is applied earlier, in
// securityBoundary; the choice of page to land on after sign-in is in
// login_redirect.go.

import (
	"errors"
	"net/http"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// loginForm shows the sign-in page, keeping a safe ?next= to return to.
func (a *App) loginForm(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "login", PageData{Title: "Login", LoginNext: auth.SafeReturnPath(r.URL.Query().Get("next"))})
}

// loginPost signs a user in. A wrong email, wrong password or inactive
// account all get the same "Invalid email or password", and every failure is
// counted and audited. On success the user lands on ?next= when they may
// open it; otherwise on Home with a note that the page was not available.
func (a *App) loginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			a.respondError(w, r, http.StatusRequestEntityTooLarge, "The submitted form is too large.", nil)
			return
		}
		a.renderStatus(w, r, http.StatusBadRequest, "login", PageData{Title: "Login", LoginNext: auth.SafeReturnPath(r.FormValue("next")), Error: "Invalid form"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	if len(email) > 254 {
		email = "invalid-address"
	}
	u, err := a.st.UserByEmail(r.Context(), email)
	// Always run a bcrypt comparison, against a dummy hash for an unknown or
	// inactive user, so the response time does not reveal whether the email
	// has an account.
	hash := u.PasswordHash
	if err != nil || !u.Active {
		hash = dummyPasswordHash
	}
	passwordOK := auth.CheckPassword(hash, r.FormValue("password"))
	if err != nil || !u.Active || !passwordOK {
		if _, failureErr := a.st.RecordFailedLogin(r.Context(), email); failureErr != nil && !errors.Is(failureErr, store.ErrNotFound) {
			a.log.ErrorContext(r.Context(), "failed to record login attempt", "request_id", requestID(r), "error", failureErr)
		}
		var actorID *int64
		actorName := email
		if err == nil {
			actorID = &u.ID
			actorName = u.Name
		}
		a.recordAudit(r, store.AuditInput{ActorID: actorID, ActorName: actorName, Action: "login_failed", EntityType: "user", EntityID: actorID, Summary: "Failed login attempt", IP: r.RemoteAddr})
		a.render(w, r, "login", PageData{Title: "Login", LoginNext: auth.SafeReturnPath(r.FormValue("next")), Error: "Invalid email or password"})
		return
	}
	if err := a.st.ResetLoginFailures(r.Context(), email); err != nil {
		a.log.ErrorContext(r.Context(), "failed to reset login attempts", "request_id", requestID(r), "error", err)
	}
	a.auth.Login(w, r, u)
	a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "login", EntityType: "user", EntityID: &u.ID, Summary: "Logged in", IP: r.RemoteAddr})
	next := a.authorizedLoginNext(r, u, r.FormValue("next"))
	// A page inside the app that this user may not open lands on Home with a
	// note (without naming the page). A "next" outside the app is dropped
	// without a note.
	if next == "/" && auth.SafeReturnPath(r.FormValue("next")) != "/" {
		next = "/?login_fallback=1"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// logoutPost signs the user out everywhere (all their sessions are revoked),
// audits it and returns to the sign-in page.
func (a *App) logoutPost(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	if u.ID != 0 {
		if err := a.st.RevokeSessions(r.Context(), u.ID); err != nil {
			a.respondStoreError(w, r, err)
			return
		}
		a.recordAudit(r, store.AuditInput{ActorID: &u.ID, ActorName: u.Name, Action: "logout", EntityType: "user", EntityID: &u.ID, Summary: "Logged out", IP: r.RemoteAddr})
	}
	a.auth.Logout(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
