package app

// The web-facing security layer: browser security headers, the cross-origin
// check on form posts, the sign-in rate limiter, request body size caps, the
// client address used for rate limits, the public static-file allowlist and
// CSV cell escaping.

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

// maxRequestBodyBytes is the body cap for file uploads: the 20 MiB attachment
// limit plus room for the other form fields.
const maxRequestBodyBytes int64 = 21 << 20

// dummyPasswordHash is checked against when no user matches the email typed
// at sign-in, so a wrong email takes as long as a wrong password and the
// response time does not reveal which accounts exist.
const dummyPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// authWindow counts the attempts one key has made in its current minute.
type authWindow struct {
	started  time.Time
	attempts int
}

// authLimiter is a fixed-window rate limiter: each key may make a set number
// of attempts per minute. It is safe for concurrent use.
type authLimiter struct {
	mu      sync.Mutex
	windows map[string]authWindow
}

// allow records one attempt for key and reports whether it is within limit
// for the current minute. Windows older than a minute are dropped on every
// call, and at most 10,000 keys are tracked (new keys are refused beyond
// that), so a flood of distinct keys cannot grow memory without bound.
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

// requestBodyLimit is how many bytes a request body may have: 8 KiB for the
// sign-in pages, 3 MiB for the budget planner, maxRequestBodyBytes for file
// uploads to requests and payments, and 64 KiB for every other form.
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

// Default per-minute caps on POSTs to /login and its sub-pages.
const (
	defaultLoginLimitPerIP  = 30
	defaultLoginLimitGlobal = 300
)

// loginLimits returns the per-client and global per-minute caps from the
// configuration. A zero or negative value (a Config built by hand, as tests
// do) means the production default, never "no limit".
func (a *App) loginLimits() (perIP, global int) {
	perIP, global = a.cfg.LoginLimitPerIP, a.cfg.LoginLimitGlobal
	if perIP <= 0 {
		perIP = defaultLoginLimitPerIP
	}
	if global <= 0 {
		global = defaultLoginLimitGlobal
	}
	return perIP, global
}

// clientIP is the address rate limits are keyed on. It is the connection's
// address, unless that address is a trusted proxy (FERVID_TRUSTED_PROXIES):
// then it is the right-most X-Forwarded-For entry that is not itself a
// trusted proxy. A malformed entry in that walk falls back to the connection
// address, so a forged header cannot pick an arbitrary key.
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

// securityBoundary sets the browser security headers on every response. For
// anything but GET, HEAD and OPTIONS it also refuses cross-site submissions
// (by Sec-Fetch-Site and Origin; a forwarded Host is never trusted) with 403,
// and rate-limits posts to /login and its sub-pages with 429: first per client
// address, then against a shared global allowance.
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
				perIP, global := a.loginLimits()
				// Per-client first: a client over its own limit is refused before its
				// attempts count against the shared allowance, so one noisy client
				// cannot lock everyone else out.
				if !a.authLimits.allow("ip:"+a.clientIP(r), perIP, now) || !a.authLimits.allow("global", global, now) {
					h.Set("Retry-After", "60")
					http.Error(w, "Too many attempts. Try again later.", http.StatusTooManyRequests)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// publicStatic serves only the named release assets from web/static, so a
// file copied there by accident cannot be downloaded and no directory listing
// is shown. The design-system reference page (_kitchensink.html) is a
// development aid served only when devPages is set (FERVID_DEV_PAGES), which
// the component test suite does and production never does.
// The directory is relative, so the server must run from the repository root.
func publicStatic(devPages bool) http.Handler {
	allowed := map[string]bool{"fervid-ds.css": true, "fervid-app.js": true, "fervid-logo.svg": true, "budget-planner.css": true, "budget-ux.css": true, "budget-planner.js": true, "htmx.min.js": true}
	if devPages {
		allowed["_kitchensink.html"] = true
	}
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

// csvText makes a value safe for a CSV cell that a spreadsheet will open. A
// value that a spreadsheet would treat as a formula (starting with = + - or @,
// after any leading blanks or byte-order mark), that starts with a byte-order
// mark, or that contains a tab or line break, is prefixed with an apostrophe
// so it is shown as plain text.
func csvText(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n\uFEFF")
	if strings.ContainsAny(value, "\t\r\n") || strings.HasPrefix(value, "\uFEFF") || (trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0]))) {
		return "'" + value
	}
	return value
}
