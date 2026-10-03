package app

// Self-service password reset: the "Sign-in help" page, the request that
// emails a one-time link, and the form that sets a new password from that
// link. Only a SHA-256 hash of each token is stored.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/notify"
)

// resetNotice is shown after every reset request, whether or not an email was
// sent, so the page never reveals which addresses have accounts.
const resetNotice = "If an active account matches and email delivery is available, a reset link will arrive shortly. Check spam. If no email arrives, contact the administrator below."

// badResetLink is the one message for a reset token that is malformed,
// unknown, expired or already used.
const badResetLink = "This reset link is invalid, expired or already used. Request a new link below."

// resetHash returns the hex SHA-256 of value. Tokens and throttle keys are
// stored only in this form.
func resetHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// trustedResetBase returns the configured base URL without a trailing slash
// when it is safe to put in a reset email: https (or http to localhost or a
// loopback address), with a host and no credentials, query or fragment.
// Otherwise it returns "" and reset emails are not offered.
func trustedResetBase(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback())) {
		return ""
	}
	return strings.TrimRight(u.String(), "/")
}

// resetPageData is the data both sign-in help pages start from: who to
// contact, and whether emailed reset links are available (SMTP host, sender
// address and a trusted base URL are all configured).
func (a *App) resetPageData(r *http.Request) PageData {
	cfg, _ := a.st.GetMailSettings(r.Context())
	return PageData{Title: "Sign-in help", RecoveryName: a.cfg.AdminName, RecoveryContact: a.cfg.AdminEmail, ResetEmailAvailable: strings.TrimSpace(cfg.SMTPHost) != "" && strings.TrimSpace(cfg.SMTPFromAddr) != "" && trustedResetBase(cfg.BaseURL) != ""}
}

// loginHelp shows the "Sign-in help" page.
func (a *App) loginHelp(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a.render(w, r, "login_help", a.resetPageData(r))
}

// passwordResetRequest handles the "email me a reset link" form. It always
// shows the same notice, so it never reveals whether an account exists. A link
// is sent only when the throttle allows (per email and per client address),
// reset email is configured and the address is a plausible length.
func (a *App) passwordResetRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	data := a.resetPageData(r)
	data.Notice = resetNotice
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	// The throttle keys on the real client address, resolved the same way as
	// the login limiter. Behind a reverse proxy the connection address is the
	// proxy's, which would put every visitor in one shared bucket.
	host := a.clientIP(r)
	allowed, err := a.st.AllowPasswordReset(r.Context(), []string{resetHash("email:" + email), resetHash("ip:" + host)}, time.Now())
	if err != nil {
		a.log.ErrorContext(r.Context(), "password reset throttle unavailable", "error", err)
	}
	if allowed && err == nil && data.ResetEmailAvailable && len(email) < 255 {
		if err := a.sendResetLink(r.Context(), email); err != nil {
			a.log.WarnContext(r.Context(), "password reset delivery unavailable")
		}
	}
	a.render(w, r, "login_help", data)
}

// sendResetLink emails a fresh one-time reset link to the active user with
// this email. An unknown or inactive address is not an error: nothing is sent
// and the caller shows the same notice either way.
func (a *App) sendResetLink(ctx context.Context, email string) error {
	u, err := a.st.UserByEmail(ctx, email)
	if err != nil || !u.Active {
		return nil
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	raw := hex.EncodeToString(token)
	if err := a.st.CreatePasswordReset(ctx, u.ID, resetHash(raw), time.Now()); err != nil {
		return err
	}
	cfg, _ := a.st.GetMailSettings(ctx)
	return a.mailer.Send(ctx, notify.Message{From: cfg.SMTPFromAddr, To: []string{u.Email}, Subject: "Reset your Fervid Budget password", Body: "Use this one-time link within 30 minutes to set a new password:\n\n" + trustedResetBase(cfg.BaseURL) + "/login/reset?token=" + raw + "\n\nIf you did not request this, ignore this email. Never share this link or your password."})
}

// passwordResetForm shows the "choose a new password" form for the token in
// the link. The token is checked only when the form is posted. No-referrer
// keeps the token out of Referer headers.
func (a *App) passwordResetForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	data := a.resetPageData(r)
	data.Title = "Reset password"
	data.ResetToken = r.URL.Query().Get("token")
	a.render(w, r, "password_reset", data)
}

// passwordResetPost sets a new password from a one-time reset link. A bad or
// used link answers 400 and drops the token; a password that does not match
// its confirmation or fails the password rules answers 422 and keeps it. On
// success the token is consumed, any session is signed out and the sign-in
// page is shown with a confirmation.
func (a *App) passwordResetPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	data := a.resetPageData(r)
	data.Title = "Reset password"
	data.ResetToken = r.FormValue("token")
	badLink := func() {
		data.Error, data.ResetToken = badResetLink, ""
		a.renderStatus(w, r, http.StatusBadRequest, "password_reset", data)
	}
	if tokenBytes, err := hex.DecodeString(data.ResetToken); err != nil || len(tokenBytes) != 32 {
		badLink()
		return
	}
	password := r.FormValue("password")
	if password != r.FormValue("confirm_password") {
		data.Error = "Passwords do not match."
	} else if err := auth.ValidatePassword(password); err != nil {
		data.Error = err.Error()
	}
	if data.Error != "" {
		a.renderStatus(w, r, http.StatusUnprocessableEntity, "password_reset", data)
		return
	}
	if valid, err := a.st.ValidPasswordReset(r.Context(), resetHash(data.ResetToken), time.Now()); err != nil || !valid {
		badLink()
		return
	}
	hash, err := auth.HashPassword(password)
	if err == nil {
		err = a.st.ConsumePasswordReset(r.Context(), resetHash(data.ResetToken), hash, time.Now())
	}
	if err != nil {
		badLink()
		return
	}
	a.auth.Logout(w)
	data.Title = "Login"
	data.Notice = "Your password has been reset. Sign in with your new password."
	data.LoginNext = "/"
	a.render(w, r, "login", data)
}
