package app

// Sign-in and sign-out handlers.

import (
	"errors"
	"net/http"
	"strings"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

func (a *App) loginForm(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "login", PageData{Title: "Login", LoginNext: auth.SafeReturnPath(r.URL.Query().Get("next"))})
}

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
	// Per-source throttles protect guessing without allowing an attacker on a
	// different source to disable a victim's correct password globally.
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
	// Explain a denied or unavailable internal destination without exposing it.
	// Invalid external destinations are still discarded outright.
	if next == "/" && auth.SafeReturnPath(r.FormValue("next")) != "/" {
		next = "/?login_fallback=1"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

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
