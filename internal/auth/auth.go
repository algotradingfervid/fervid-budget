package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fervidbudget/internal/config"
	"fervidbudget/internal/store"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const userKey contextKey = "user"
const sessionCookie = "fervid_session"
const csrfCookie = "fervid_csrf"

type Manager struct {
	cfg      config.Config
	store    *store.Store
	enforcer *casbin.Enforcer
	onError  func(http.ResponseWriter, *http.Request, int, string)
}

func New(cfg config.Config, st *store.Store) (*Manager, error) {
	m, err := model.NewModelFromString(`
[request_definition]
r = sub, obj, act
[policy_definition]
p = sub, obj, act
[role_definition]
g = _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = g(r.sub, p.sub) && (p.obj == "*" || p.obj == r.obj) && (p.act == "*" || p.act == r.act)
`)
	if err != nil {
		return nil, err
	}
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, err
	}
	for _, p := range [][]string{
		{"admin", "*", "*"},
		{"data_entry", "payment", "create"},
		{"data_entry", "payment", "read"},
		{"data_entry", "payment_attachment", "create"},
		{"data_entry", "payment_attachment", "read"},
		{"data_entry", "report", "read"},
		{"data_entry", "report", "export"},
		{"data_entry", "grid", "read"},
	} {
		_, _ = e.AddPolicy(p)
	}
	_, _ = e.AddGroupingPolicy("admin", "admin")
	_, _ = e.AddGroupingPolicy("data_entry", "data_entry")
	return &Manager{cfg: cfg, store: st, enforcer: e}, nil
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
	sig := m.sign(payload)
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
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Can is the single authorisation question the whole application asks. Every
// gate — middleware, handler check or rendered control — goes through it so
// permissions are decided in exactly one place.
func (m *Manager) Can(u store.User, resource, action string) bool {
	if u.Role == "" || resource == "" || action == "" {
		return false
	}
	ok, err := m.enforcer.Enforce(u.Role, resource, action)
	return err == nil && ok
}

// Permissions snapshots everything a user may do so a page render can ask
// hundreds of questions without re-entering the policy engine each time.
// Phase 1 swaps the source of these grants for the roles table; the returned
// interface does not change.
func (m *Manager) Permissions(u store.User) store.PermissionSet {
	if u.Role == "" {
		return store.NewPermissionSet(nil)
	}
	policies, err := m.enforcer.GetImplicitPermissionsForUser(u.Role)
	if err != nil {
		return store.NewPermissionSet(nil)
	}
	grants := make([]store.Grant, 0, len(policies))
	for _, policy := range policies {
		if len(policy) < 3 {
			continue
		}
		grants = append(grants, store.Grant{Resource: policy[1], Action: policy[2]})
	}
	return store.NewPermissionSet(grants)
}

func (m *Manager) RequirePermission(obj, act string, next http.Handler) http.Handler {
	return m.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.Can(CurrentUser(r), obj, act) {
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
	if !hmac.Equal([]byte(m.sign(payload)), []byte(parts[2])) {
		return store.User{}, false
	}
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
	return u, true
}

func (m *Manager) sign(payload string) string {
	mac := hmac.New(sha256.New, []byte(m.cfg.SessionKey))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
