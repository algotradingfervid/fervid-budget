package app

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

type requestIDContextKey struct{}

type observedResponse struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

func (w *observedResponse) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *observedResponse) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

// httpObservability is deliberately kept as a small package-private boundary:
// it gives every request a correlation ID, records one structured completion
// event, and converts panics into a safe response without exposing internals.
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

func requestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDContextKey{}).(string)
	if id == "" {
		return "unavailable"
	}
	return id
}

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
}

func remoteIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(addr)
}

// Shell counts may be memoised between reads, but must reflect a user's writes
// immediately on the redirected receipt or ledger rather than after a TTL.
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
