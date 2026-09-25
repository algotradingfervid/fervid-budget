package app

import (
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

const resetNotice = "If an active account matches and email delivery is available, a reset link will arrive shortly. Check spam. If no email arrives, contact the administrator below."

func resetHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
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
func (a *App) resetPageData(r *http.Request) PageData {
	cfg, _ := a.st.GetMailSettings(r.Context())
	return PageData{Title: "Sign-in help", RecoveryName: a.cfg.AdminName, RecoveryContact: a.cfg.AdminEmail, ResetEmailAvailable: strings.TrimSpace(cfg.SMTPHost) != "" && strings.TrimSpace(cfg.SMTPFromAddr) != "" && trustedResetBase(cfg.BaseURL) != ""}
}
func (a *App) loginHelp(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a.render(w, r, "login_help", a.resetPageData(r))
}
func (a *App) passwordResetRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	data := a.resetPageData(r)
	data.Notice = resetNotice
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	allowed, err := a.st.AllowPasswordReset(r.Context(), []string{resetHash("email:" + email), resetHash("ip:" + host)}, time.Now())
	if err != nil {
		a.log.ErrorContext(r.Context(), "password reset throttle unavailable", "error", err)
	}
	if allowed && err == nil && data.ResetEmailAvailable && len(email) < 255 {
		u, err := a.st.UserByEmail(r.Context(), email)
		if err == nil && u.Active {
			token := make([]byte, 32)
			if _, err = rand.Read(token); err == nil {
				raw := hex.EncodeToString(token)
				if err = a.st.CreatePasswordReset(r.Context(), u.ID, resetHash(raw), time.Now()); err == nil {
					cfg, _ := a.st.GetMailSettings(r.Context())
					err = a.mailer.Send(r.Context(), notify.Message{From: cfg.SMTPFromAddr, To: []string{u.Email}, Subject: "Reset your Fervid Budget password", Body: "Use this one-time link within 30 minutes to set a new password:\n\n" + trustedResetBase(cfg.BaseURL) + "/login/reset?token=" + raw + "\n\nIf you did not request this, ignore this email. Never share this link or your password."})
				}
			}
			if err != nil {
				a.log.WarnContext(r.Context(), "password reset delivery unavailable")
			}
		}
	}
	a.render(w, r, "login_help", data)
}
func (a *App) passwordResetForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	data := a.resetPageData(r)
	data.Title = "Reset password"
	data.ResetToken = r.URL.Query().Get("token")
	a.render(w, r, "password_reset", data)
}
func (a *App) passwordResetPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	data := a.resetPageData(r)
	data.Title = "Reset password"
	data.ResetToken = r.FormValue("token")
	tokenBytes, tokenErr := hex.DecodeString(data.ResetToken)
	if tokenErr != nil || len(tokenBytes) != 32 {
		data.Error = "This reset link is invalid, expired or already used. Request a new link below."
		data.ResetToken = ""
		a.renderStatus(w, r, http.StatusBadRequest, "password_reset", data)
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
	hash, err := auth.HashPassword(password)
	if err == nil {
		err = a.st.ConsumePasswordReset(r.Context(), resetHash(data.ResetToken), hash, time.Now())
	}
	if err != nil {
		data.Error = "This reset link is invalid, expired or already used. Request a new link below."
		data.ResetToken = ""
		a.renderStatus(w, r, http.StatusBadRequest, "password_reset", data)
		return
	}
	a.auth.Logout(w)
	data.Title = "Login"
	data.Notice = "Your password has been reset. Sign in with your new password."
	data.LoginNext = "/"
	a.render(w, r, "login", data)
}
