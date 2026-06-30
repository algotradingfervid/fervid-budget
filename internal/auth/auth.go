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
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
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

func (m *Manager) RequirePermission(obj, act string, next http.Handler) http.Handler {
	return m.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := CurrentUser(r)
		ok, err := m.enforcer.Enforce(u.Role, obj, act)
		if err != nil || !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
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
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
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
