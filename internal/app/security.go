package app

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

const dummyPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

type authWindow struct {
	started  time.Time
	attempts int
}
type authLimiter struct {
	mu      sync.Mutex
	windows map[string]authWindow
}

// Bounded, short-lived state prevents unknown usernames becoming a memory sink.
func (l *authLimiter) allow(key string, limit int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.windows == nil {
		l.windows = make(map[string]authWindow)
	}
	for k, v := range l.windows {
		if now.Sub(v.started) >= time.Minute {
			delete(l.windows, k)
		}
	}
	v, exists := l.windows[key]
	if !exists {
		if len(l.windows) >= 10000 {
			return false
		}
		v.started = now
	}
	if v.attempts >= limit {
		return false
	}
	v.attempts++
	l.windows[key] = v
	return true
}

func requestBodyLimit(r *http.Request) int64 {
	if r.URL.Path == "/login" || strings.HasPrefix(r.URL.Path, "/login/") {
		return 8 << 10
	}
	if r.URL.Path == "/budgets/plan" {
		return 3 << 20
	}
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") &&
		(strings.HasPrefix(r.URL.Path, "/requests") || strings.HasPrefix(r.URL.Path, "/payments")) {
		return maxRequestBodyBytes
	}
	return 64 << 10
}

func (a *App) clientIP(r *http.Request) string {
	host := remoteIP(r.RemoteAddr)
	trusted := func(value string) bool {
		ip, err := netip.ParseAddr(value)
		if err != nil {
			return false
		}
		for _, raw := range a.cfg.TrustedProxies {
			raw = strings.TrimSpace(raw)
			if prefix, e := netip.ParsePrefix(raw); e == nil && prefix.Contains(ip) {
				return true
			}
			if allowed, e := netip.ParseAddr(raw); e == nil && allowed == ip {
				return true
			}
		}
		return false
	}
	if !trusted(host) {
		return host
	}
	chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(chain) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(chain[i])
		if net.ParseIP(candidate) == nil {
			return host
		}
		if !trusted(candidate) {
			return candidate
		}
	}
	return host
}

func (a *App) securityBoundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cache-Control", "private, no-store")
		if a.cfg.SecureCookies {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			// Same-site sibling origins are not trusted. Do not trust forwarded Host.
			site := r.Header.Get("Sec-Fetch-Site")
			origin := r.Header.Get("Origin")
			scheme := "http"
			if a.cfg.SecureCookies || r.TLS != nil {
				scheme = "https"
			}
			invalid := site == "cross-site" || site == "same-site"
			if origin != "" {
				u, err := url.Parse(origin)
				invalid = invalid || err != nil || u.Scheme != scheme || !strings.EqualFold(u.Host, r.Host) || u.User != nil
			}
			if invalid {
				http.Error(w, "Cross-origin form submission refused.", http.StatusForbidden)
				return
			}
			if r.URL.Path == "/login" || strings.HasPrefix(r.URL.Path, "/login/") {
				now := time.Now()
				if !a.authLimits.allow("global", 300, now) || !a.authLimits.allow("ip:"+a.clientIP(r), 30, now) {
					h.Set("Retry-After", "60")
					http.Error(w, "Too many attempts. Try again later.", http.StatusTooManyRequests)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Only release assets are public. Files accidentally copied under web/static
// cannot become a download or a directory listing.
func publicStatic() http.Handler {
	allowed := map[string]bool{"fervid-ds.css": true, "fervid-app.js": true, "fervid-logo.svg": true, "budget-planner.css": true, "budget-ux.css": true, "budget-planner.js": true, "htmx.min.js": true}
	files := http.FileServer(http.Dir("web/static"))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if !allowed[name] {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}

func csvText(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n\uFEFF")
	if strings.ContainsAny(value, "\t\r\n") || strings.HasPrefix(value, "\uFEFF") || (trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0]))) {
		return "'" + value
	}
	return value
}
