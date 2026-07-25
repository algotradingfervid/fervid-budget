package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

const maxRequestBodyBytes int64 = 21 << 20
const maxAttachmentBytes int64 = 20 << 20

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

func (a *App) renderStatus(w http.ResponseWriter, r *http.Request, status int, name string, data PageData) {
	data.User = auth.CurrentUser(r)
	data.CSRF = a.auth.EnsureCSRF(w, r)
	data.RequestID = requestID(r)
	if data.Month == "" {
		data.Month = time.Now().Format("2006-01")
	}
	// renderStatus is the single place where the signed-in user is known, so
	// it is the single place the shell is built. htmx asks for a fragment, not
	// a page: skip the nav, the tab bar and the badge query entirely.
	if isFragmentRequest(r) {
		data.Shell = Shell{Chrome: chromeNone}
	} else {
		data.Shell = a.buildPageShell(r, data.User, data.Title)
	}

	var body bytes.Buffer
	if err := a.tpl.ExecuteTemplate(&body, name, data); err != nil {
		a.log.ErrorContext(r.Context(), "template rendering failed",
			"request_id", data.RequestID,
			"template", name,
			"error", err,
		)
		http.Error(w, "Something went wrong while preparing this page. Request ID: "+data.RequestID, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(body.Bytes()); err != nil {
		a.log.ErrorContext(r.Context(), "response write failed",
			"request_id", data.RequestID,
			"template", name,
			"error", err,
		)
	}
}

// isFragmentRequest reports whether htmx is swapping part of a page rather
// than loading a whole one. htmx sets HX-Request on every request it makes.
func isFragmentRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") != ""
}

func (a *App) respondError(w http.ResponseWriter, r *http.Request, status int, message string, cause error) {
	if cause != nil {
		level := slog.LevelWarn
		if status >= 500 {
			level = slog.LevelError
		}
		a.log.Log(r.Context(), level, "request failed",
			"request_id", requestID(r),
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"error", cause,
		)
	}
	a.renderStatus(w, r, status, "error_page", PageData{
		Title:     http.StatusText(status),
		Error:     message,
		ErrorCode: status,
	})
}

func (a *App) respondStoreError(w http.ResponseWriter, r *http.Request, err error) {
	status := storeErrorStatus(err)
	message := "Something went wrong while processing your request."
	switch {
	case errors.Is(err, store.ErrNotFound):
		status, message = http.StatusNotFound, "The requested record was not found."
	case errors.Is(err, store.ErrLockedMonth):
		status, message = http.StatusConflict, "This month is locked and cannot be changed."
	case errors.Is(err, store.ErrForbidden):
		status, message = http.StatusForbidden, "You do not have permission to perform this action."
	case errors.Is(err, store.ErrValidation), errors.Is(err, store.ErrDuplicate), errors.Is(err, store.ErrInactiveHead):
		status, message = http.StatusBadRequest, friendly(err)
	}
	a.respondError(w, r, status, message, err)
}

func storeErrorStatus(err error) int {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, store.ErrLockedMonth):
		return http.StatusConflict
	case errors.Is(err, store.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, store.ErrValidation), errors.Is(err, store.ErrDuplicate), errors.Is(err, store.ErrInactiveHead):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
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

func removeStagedAttachment(logger *slog.Logger, r *http.Request, path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.ErrorContext(r.Context(), "attachment cleanup failed",
			"request_id", requestID(r),
			"path", path,
			"error", err,
		)
	}
}

func (a *App) recordAudit(r *http.Request, in store.AuditInput) {
	if err := a.st.RecordAudit(r.Context(), in); err != nil {
		a.log.ErrorContext(r.Context(), "audit write failed",
			"request_id", requestID(r),
			"action", in.Action,
			"entity_type", in.EntityType,
			"error", err,
		)
	}
}
