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

type contextKey string

const userKey contextKey = "user"
const sessionCookie = "fervid_session"
const csrfCookie = "fervid_csrf"

type Manager struct {
	cfg     config.Config
	store   *store.Store
	onError func(http.ResponseWriter, *http.Request, int, string)
	// loadPerms reads one user's permission set from the database. It is a field
	// only so a test can count how often the database is actually asked.
	loadPerms func(ctx context.Context, userID int64) (store.PermissionSet, error)
}

func New(cfg config.Config, st *store.Store) (*Manager, error) {
	return &Manager{cfg: cfg, store: st, loadPerms: st.EffectivePermissions}, nil
}

func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

// ValidatePassword is shared by bootstrap, user management and email reset.
func ValidatePassword(value string) error { return password.Validate(value) }

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (m *Manager) Login(w http.ResponseWriter, r *http.Request, user store.User) {
	exp := time.Now().Add(12 * time.Hour).Unix()
	payload := fmt.Sprintf("%d:%d", user.ID, exp)
	sig := m.sign(payload + ":" + user.PasswordHash + ":" + strconv.FormatInt(user.SessionVersion, 10))
	value := base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + sig))
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: "/", HttpOnly: true, Secure: m.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
	m.setCSRF(w, value)
}

func (m *Manager) Logout(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: m.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
}

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
// buttons), and each answer used to cost two SQL queries.
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

func (m *Manager) csrfBinding(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}
func (m *Manager) validCSRF(token, binding string) bool {
	nonce, signature, ok := strings.Cut(token, ".")
	raw, err := base64.RawURLEncoding.DecodeString(nonce)
	return ok && err == nil && len(raw) == 32 && hmac.Equal([]byte(signature), []byte(m.sign("csrf:"+binding+":"+nonce)))
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
	return m.validCSRF(c.Value, m.csrfBinding(r)) && subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) == 1
}
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
func (m *Manager) EnsureCSRF(w http.ResponseWriter, r *http.Request) string {
	binding := m.csrfBinding(r)
	if c, err := r.Cookie(csrfCookie); err == nil && m.validCSRF(c.Value, binding) {
		return c.Value
	}
	return m.setCSRF(w, binding)
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
