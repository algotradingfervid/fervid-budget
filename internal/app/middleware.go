package app

// HTTP middleware that wraps every request: request ids and structured
// request logging with panic recovery, and clearing the cached nav badge
// counts after any write. The security middleware (headers, cross-origin
// check, login rate limit) lives in security.go.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// requestIDContextKey is the context key the request id is stored under.
type requestIDContextKey struct{}

// observedResponse wraps a ResponseWriter to remember the status code and
// the number of body bytes written, for the completion log line.
type observedResponse struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

// WriteHeader records the status and passes it on. Only the first call
// counts, as with net/http itself.
func (w *observedResponse) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

// Write sends body bytes, writing a 200 header first if none was written.
func (w *observedResponse) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

// httpObservability gives every request an id (returned in X-Request-ID and
// shown on error pages), logs one "http request completed" line per request,
// and turns a panic into the generic error page instead of a dropped
// connection, logging the stack but never showing it to the user.
func (a *App) httpObservability(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := newRequestID()
		w.Header().Set("X-Request-ID", requestID)
		r = r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, requestID))
		observed := &observedResponse{ResponseWriter: w}

		defer func() {
			if recovered := recover(); recovered != nil {
				a.log.ErrorContext(r.Context(), "http panic recovered",
					"request_id", requestID,
					"method", r.Method,
					"path", r.URL.Path,
					"error", fmt.Sprint(recovered),
					"stack", string(debug.Stack()),
				)
				if !observed.wrote {
					a.respondError(observed, r, http.StatusInternalServerError, "Something went wrong while processing your request.", nil)
				}
			}
			status := observed.status
			if status == 0 {
				status = http.StatusOK
			}
			level := slog.LevelInfo
			if status >= 500 {
				level = slog.LevelError
			} else if status >= 400 {
				level = slog.LevelWarn
			}
			a.log.Log(r.Context(), level, "http request completed",
				"request_id", requestID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"bytes", observed.bytes,
				"duration_ms", time.Since(started).Milliseconds(),
				"remote_ip", remoteIP(r.RemoteAddr),
			)
		}()

		next.ServeHTTP(observed, r)
	})
}

// requestID returns the id httpObservability gave this request, or
// "unavailable" outside that middleware (for example in unit tests).
func requestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDContextKey{}).(string)
	if id == "" {
		return "unavailable"
	}
	return id
}

// newRequestID returns 32 random hex characters, or a time-based id in the
// unlikely case the random source fails.
func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
}

// remoteIP strips the port from a "host:port" connection address. An address
// without a port is returned trimmed.
func remoteIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(addr)
}

// refreshBadgesAfterMutation clears the cached nav badge counts after every
// request that may change data (anything but GET, HEAD and OPTIONS). Counts
// may be cached between reads, but the page a user lands on after their own
// change must show the new numbers straight away.
func (a *App) refreshBadgesAfterMutation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			defer a.st.InvalidateBadgeCounts()
		}
		next.ServeHTTP(w, r)
	})
}
