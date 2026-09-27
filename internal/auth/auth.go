// Package auth handles who is signed in and what they may do: the signed
// session cookie, password hashing, CSRF tokens for forms, and the
// login-required and permission-required wrappers the HTTP handlers use.
//
// The session cookie holds base64("userID:expiry:signature"). The signature is
// an HMAC over the user id, the expiry, the user's password hash and their
// session version, keyed with the server's session key. Changing a password or
// bumping a user's session version therefore signs that user out everywhere,
// and changing the session key signs everybody out.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"fervidbudget/internal/config"
	"fervidbudget/internal/password"
	"fervidbudget/internal/store"

	"golang.org/x/crypto/bcrypt"
)

// contextKey keeps this package's request-context keys apart from any other
// package's keys.
type contextKey string

const (
	// userKey is where Middleware stores the signed-in store.User.
	userKey contextKey = "user"
	// sessionCookie is the name of the signed session cookie.
	sessionCookie = "fervid_session"
	// csrfCookie is the name of the cookie holding the form token. Unlike the
	// session cookie it is readable by the page's scripts.
	csrfCookie = "fervid_csrf"
	// sessionLifetime is how long a sign-in lasts before the user must sign in
	// again.
	sessionLifetime = 12 * time.Hour
)

// Manager signs and checks session cookies and CSRF tokens and answers
// permission questions. The application builds one at start-up with New and
// shares it across all requests.
type Manager struct {
	cfg     config.Config
	store   *store.Store
	onError func(http.ResponseWriter, *http.Request, int, string)
	// loadPerms reads one user's permission set from the database. It is a field
	// only so a test can count how often the database is actually asked.
	loadPerms func(ctx context.Context, userID int64) (store.PermissionSet, error)
}

// New builds a Manager that signs cookies with cfg.SessionKey and reads users
// and permissions from st. It never fails today; the error return is kept for
// the callers that already check it.
func New(cfg config.Config, st *store.Store) (*Manager, error) {
	return &Manager{cfg: cfg, store: st, loadPerms: st.EffectivePermissions}, nil
}

// HashPassword checks the password against the password policy and, if it
// passes, returns its bcrypt hash for storage.
func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

// ValidatePassword applies the password policy. It is shared by the first
// admin set-up, user management and the email password reset.
func ValidatePassword(value string) error { return password.Validate(value) }

// CheckPassword reports whether password matches the stored bcrypt hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Login signs user in for twelve hours: it sets the session cookie (see the
// package comment for its format) and a fresh CSRF token bound to it. The
// cookie itself carries no expiry date, so the browser also drops it when it
// closes.
func (m *Manager) Login(w http.ResponseWriter, r *http.Request, user store.User) {
	exp := time.Now().Add(sessionLifetime).Unix()
	payload := fmt.Sprintf("%d:%d", user.ID, exp)
	sig := m.sign(sessionSignedText(payload, user))
	value := base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + sig))
	http.SetCookie(w, m.sessionCookie(value, 0))
	m.setCSRF(w, value)
}

// Logout tells the browser to delete the session cookie.
func (m *Manager) Logout(w http.ResponseWriter) {
	http.SetCookie(w, m.sessionCookie("", -1))
}

// sessionCookie builds the session cookie with the options Login and Logout
// share. maxAge 0 means "until the browser closes"; -1 means "delete now".
func (m *Manager) sessionCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: sessionCookie, Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: m.cfg.SecureCookies, SameSite: http.SameSiteLaxMode}
}

// sessionSignedText is the text a session signature covers: "userID:expiry"
// followed by the user's password hash and session version. Including the
// last two is what makes a password change or a session-version bump end
// every existing session of that user.
func sessionSignedText(payload string, u store.User) string {
	return payload + ":" + u.PasswordHash + ":" + strconv.FormatInt(u.SessionVersion, 10)
}

// Middleware reads the session cookie on every request. When it names an
// active user and its signature is still valid, that user is put on the
// request context (read it back with CurrentUser) together with a fresh
// per-request permission cache. Otherwise the request carries no user. It
// never rejects a request itself; RequireLogin and RequirePermission do that.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := m.userFromRequest(r); ok {
			// The signed-in user carries this request's permission cache, so
			// every Can/Scope/Permissions call made with CurrentUser(r) shares it.
			u = u.WithRequestCache(newPermissionCache(r.Context()))
			r = r.WithContext(context.WithValue(r.Context(), userKey, u))
		}
		next.ServeHTTP(w, r)
	})
}

// RequireLogin lets signed-in users through. Anyone else is redirected to
// /login; for a GET of any page but the home page, that page is passed along
// as ?next= so the user comes back to it after signing in.
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

// permissionCache remembers the permission sets already read during one
// request, keyed by user id. A single page asks Can/Scope/Permissions many
// times (the route gate, the navigation, the dashboard cards, the rendered
// buttons), and without the cache each answer would cost two SQL queries.
//
// Middleware creates one per request and attaches it to the signed-in user it
// puts in the request context, so it dies with the request: a role or grant an
// admin changes applies on the very next request (the role and user screens
// redirect after a successful save). A user loaded from the database — some
// handlers ask about people other than the signed-in one — carries no cache
// and is always read fresh.
type permissionCache struct {
	ctx  context.Context // the request's context, used for the queries
	mu   sync.Mutex
	sets map[int64]store.PermissionSet
}

// newPermissionCache makes an empty cache whose queries run under ctx.
func newPermissionCache(ctx context.Context) *permissionCache {
	return &permissionCache{ctx: ctx, sets: map[int64]store.PermissionSet{}}
}

// permsFor returns a user's effective permission set: from the request's cache
// when this request has already read it, otherwise from the roles tables. A
// failed read is never cached, so the next call tries the database again.
func (m *Manager) permsFor(u store.User) (store.PermissionSet, error) {
	if u.ID == 0 {
		return store.EmptyPermissions(), nil
	}
	cache, _ := u.RequestCache().(*permissionCache)
	// A cache whose request has finished is ignored rather than trusted: the
	// user value has outlived its request, so it reads fresh as before.
	if cache == nil || cache.ctx.Err() != nil {
		return m.loadPerms(context.Background(), u.ID)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if ps, ok := cache.sets[u.ID]; ok {
		return ps, nil
	}
	ps, err := m.loadPerms(cache.ctx, u.ID)
	if err != nil {
		return ps, err
	}
	cache.sets[u.ID] = ps
	return ps, nil
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

// Can reports whether u holds the permission action on resource (for example
// "approval", "approve"). Route gates, handler checks and rendered buttons in
// the web layer ask it. A user with no id, or a failed permission read, gets
// false.
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

// RequirePermission wraps next so that it runs only for a signed-in user who
// can do action on resource. Anonymous visitors are sent to /login (see
// RequireLogin); signed-in users without the permission get a 403 page.
func (m *Manager) RequirePermission(resource, action string, next http.Handler) http.Handler {
	return m.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.Can(CurrentUser(r), resource, action) {
			m.writeError(w, r, http.StatusForbidden, "You do not have permission to perform this action.")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// SetErrorHandler sets the function that draws error pages, such as the 403
// from RequirePermission. Without one, a plain-text http.Error is written.
func (m *Manager) SetErrorHandler(fn func(http.ResponseWriter, *http.Request, int, string)) {
	m.onError = fn
}

// writeError shows an error page through the handler set with SetErrorHandler.
func (m *Manager) writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	if m.onError != nil {
		m.onError(w, r, status, message)
		return
	}
	http.Error(w, message, status)
}

// CurrentUser returns the signed-in user Middleware put on the request, or the
// zero User (ID 0) when nobody is signed in.
func CurrentUser(r *http.Request) store.User {
	u, _ := r.Context().Value(userKey).(store.User)
	return u
}

// CSRF tokens have the form "nonce.signature": a random 32-byte nonce and an
// HMAC over the nonce and the current session cookie value (the "binding").
// The token lives in the fervid_csrf cookie, and every form posts it back in a
// "csrf" field or an X-CSRF-Token header. Because the signature covers the
// session cookie, a token taken from one session is useless in another.

// csrfBinding returns the value a CSRF token is tied to: the request's
// session cookie, or "" for a visitor who is not signed in.
func csrfBinding(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// validCSRF reports whether token is well formed and was signed by this
// server for binding.
func (m *Manager) validCSRF(token, binding string) bool {
	nonce, signature, ok := strings.Cut(token, ".")
	raw, err := base64.RawURLEncoding.DecodeString(nonce)
	return ok && err == nil && len(raw) == 32 && hmac.Equal([]byte(signature), []byte(m.sign("csrf:"+binding+":"+nonce)))
}

// CheckCSRF reports whether a request may change state. GET, HEAD and
// OPTIONS always pass. Any other method passes only when the fervid_csrf
// cookie holds a valid token for this session and the "csrf" form field (or
// the X-CSRF-Token header) carries the same token.
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
	return m.validCSRF(c.Value, csrfBinding(r)) && subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) == 1
}

// setCSRF makes a new token bound to binding, stores it in the fervid_csrf
// cookie and returns it.
func (m *Manager) setCSRF(w http.ResponseWriter, binding string) string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	nonce := base64.RawURLEncoding.EncodeToString(raw)
	token := nonce + "." + m.sign("csrf:"+binding+":"+nonce)
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: token, Path: "/", Secure: m.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
	return token
}

// EnsureCSRF returns the token a page should put in its forms: the one
// already in the request's cookie if it is still valid for this session,
// otherwise a newly issued one.
func (m *Manager) EnsureCSRF(w http.ResponseWriter, r *http.Request) string {
	binding := csrfBinding(r)
	if c, err := r.Cookie(csrfCookie); err == nil && m.validCSRF(c.Value, binding) {
		return c.Value
	}
	return m.setCSRF(w, binding)
}

// userFromRequest decodes and checks the session cookie. It returns the user
// only when the cookie is well formed, not expired, names an active user, and
// its signature matches that user's current password hash and session
// version.
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
	if !hmac.Equal([]byte(m.sign(sessionSignedText(payload, u))), []byte(parts[2])) {
		return store.User{}, false
	}
	return u, true
}

// sign returns the base64 HMAC-SHA256 of payload under the session key.
func (m *Manager) sign(payload string) string {
	mac := hmac.New(sha256.New, []byte(m.cfg.SessionKey))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// SafeReturnPath turns a user-supplied "go back to" value into a path on this
// site. Only same-origin absolute paths are accepted; anything else, and the
// login and logout pages themselves, becomes "/". Encoded separators,
// backslashes and control characters cannot turn the destination into
// another host.
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
