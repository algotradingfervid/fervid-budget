package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fervidbudget/internal/config"
	"fervidbudget/internal/store"

	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const userKey contextKey = "user"
const sessionCookie = "fervid_session"
const csrfCookie = "fervid_csrf"

type Manager struct {
	cfg     config.Config
	store   *store.Store
	onError func(http.ResponseWriter, *http.Request, int, string)
}

func New(cfg config.Config, st *store.Store) (*Manager, error) {
	return &Manager{cfg: cfg, store: st}, nil
}

func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

// ValidatePassword keeps the local bootstrap password compatible while
// preventing trivially short credentials in user-management workflows.
func ValidatePassword(password string) error {
	if len(password) > 72 {
		return fmt.Errorf("password must be at most 72 bytes; use fewer characters if it contains symbols or non-English letters")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	var hasLetter, hasDigit bool
	for _, r := range password {
		hasLetter = hasLetter || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z')
		hasDigit = hasDigit || ('0' <= r && r <= '9')
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("password must include a letter and a number")
	}
	return nil
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (m *Manager) Login(w http.ResponseWriter, r *http.Request, user store.User) {
	exp := time.Now().Add(12 * time.Hour).Unix()
	payload := fmt.Sprintf("%d:%d", user.ID, exp)
	sig := m.sign(payload + ":" + user.PasswordHash + ":" + strconv.FormatInt(user.SessionVersion, 10))
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + sig)), Path: "/", HttpOnly: true, Secure: m.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
	m.EnsureCSRF(w, r)
}

func (m *Manager) Logout(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: m.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
}

func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := m.userFromRequest(r); ok {
			r = r.WithContext(context.WithValue(r.Context(), userKey, u))
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Manager) RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentUser(r).ID == 0 {
			target := "/login"
			if r.Method == http.MethodGet && r.URL.RequestURI() != "/" {
				target += "?next=" + url.QueryEscape(SafeReturnPath(r.URL.RequestURI()))
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// permsFor loads the caller's effective permission set from the roles tables.
// Correctness-first: it queries per request (a short-TTL cache keyed by user id
// is a later option).
func (m *Manager) permsFor(u store.User) (store.PermissionSet, error) {
	if u.ID == 0 {
		return store.EmptyPermissions(), nil
	}
	return m.store.EffectivePermissions(context.Background(), u.ID)
}

// Permissions returns the caller's effective permission set for template use;
// on error it returns an empty (deny-all) set. store.PermissionSet is an
// interface, so the deny-all value comes from store.EmptyPermissions() rather
// than a struct literal.
func (m *Manager) Permissions(u store.User) store.PermissionSet {
	ps, err := m.permsFor(u)
	if err != nil {
		return store.EmptyPermissions()
	}
	return ps
}

// Can is the single authorisation question the whole application asks. Every
// gate — middleware, handler check or rendered control — goes through it so
// permissions are decided in exactly one place.
func (m *Manager) Can(u store.User, resource, action string) bool {
	ps, err := m.permsFor(u)
	if err != nil {
		return false
	}
	return ps.Can(resource, action)
}

// Scope reports the broadest data scope the caller holds over a scoped
// resource ("own" | "assigned" | "all"), or "" when it holds none.
func (m *Manager) Scope(u store.User, resource string) string {
	ps, err := m.permsFor(u)
	if err != nil {
		return ""
	}
	return ps.Scope(resource)
}

func (m *Manager) RequirePermission(resource, action string, next http.Handler) http.Handler {
	return m.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.Can(CurrentUser(r), resource, action) {
			m.writeError(w, r, http.StatusForbidden, "You do not have permission to perform this action.")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (m *Manager) SetErrorHandler(fn func(http.ResponseWriter, *http.Request, int, string)) {
	m.onError = fn
}

func (m *Manager) writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	if m.onError != nil {
		m.onError(w, r, status, message)
		return
	}
	http.Error(w, message, status)
}

func CurrentUser(r *http.Request) store.User {
	u, _ := r.Context().Value(userKey).(store.User)
	return u
}

func (m *Manager) CheckCSRF(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return true
	}
	c, err := r.Cookie(csrfCookie)
	if err != nil {
		return false
	}
	token := r.FormValue("csrf")
	if token == "" {
		token = r.Header.Get("X-CSRF-Token")
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) == 1
}

func (m *Manager) EnsureCSRF(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(csrfCookie); err == nil && c.Value != "" {
		return c.Value
	}
	token := m.sign(fmt.Sprintf("%d", time.Now().UnixNano()))[:32]
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: token, Path: "/", HttpOnly: false, Secure: m.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
	return token
}

func (m *Manager) CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.CheckCSRF(r) {
			m.writeError(w, r, http.StatusForbidden, "Your form session expired. Refresh the page and try again.")
			return
		}
		m.EnsureCSRF(w, r)
		next.ServeHTTP(w, r)
	})
}

func (m *Manager) userFromRequest(r *http.Request) (store.User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return store.User{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return store.User{}, false
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 3 {
		return store.User{}, false
	}
	payload := parts[0] + ":" + parts[1]
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return store.User{}, false
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return store.User{}, false
	}
	u, err := m.store.UserByID(r.Context(), id)
	if err != nil || !u.Active {
		return store.User{}, false
	}
	if !hmac.Equal([]byte(m.sign(payload+":"+u.PasswordHash+":"+strconv.FormatInt(u.SessionVersion, 10))), []byte(parts[2])) {
		return store.User{}, false
	}
	return u, true
}

func (m *Manager) sign(payload string) string {
	mac := hmac.New(sha256.New, []byte(m.cfg.SessionKey))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// SafeReturnPath only accepts same-origin absolute paths. Encoded separators,
// backslashes and control characters cannot turn the destination into a host.
func SafeReturnPath(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || strings.ContainsAny(u.Path, "\\\r\n") || strings.ContainsAny(value, "\r\n") {
		return "/"
	}
	if u.Path == "/login" || strings.HasPrefix(u.Path, "/login/") || u.Path == "/logout" {
		return "/"
	}
	return u.RequestURI()
}
