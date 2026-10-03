// Package app is the web layer of Fervid Budget. It turns HTTP requests into
// store calls and answers with html/template pages (and htmx fragments of
// them).
//
// This file builds the server: the App value every handler hangs off, the
// templates, the first admin account and the middleware chain. The route
// table is in routes.go and each screen has its own file (payments.go,
// requests.go, budgets.go, users.go, …). Shared page plumbing is in
// page_data.go, render.go and handler_wrappers.go.
package app

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/config"
	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

// App holds what every handler needs: the configuration, the SQLite store,
// the session and permission manager, the parsed templates and the
// notification service. Handlers are methods on *App.
type App struct {
	// uploadMu makes attachment uploads take turns, so two uploads cannot
	// both pass the storage quota check; uploadQuotaWarned makes the
	// "storage above 80 percent" warning log only once.
	uploadMu          sync.Mutex
	uploadQuotaWarned bool
	cfg               config.Config
	st                *store.Store
	auth              *auth.Manager
	tpl               *template.Template
	log               *slog.Logger
	// notify turns workflow events into in-app rows and email; mailer is the
	// transport it uses, kept separately so the rules screen can send a test
	// message without inventing an event.
	notify *notify.Service
	mailer notify.Mailer
	// authLimits counts posts to /login and its sub-pages, per client and
	// overall, for the rate limit in securityBoundary.
	authLimits authLimiter
}

// New builds the HTTP server.
func New(cfg config.Config, st *store.Store) (*http.Server, error) {
	srv, _, err := NewWithNotifier(cfg, st)
	return srv, err
}

// NewWithNotifier builds the HTTP server and also returns the notification
// service its handlers use. The reminder scheduler must run on this same
// service, so there is exactly one mailer and one SMTP configuration (password
// and host allowlist) in the process.
func NewWithNotifier(cfg config.Config, st *store.Store) (*http.Server, *notify.Service, error) {
	a, srv, err := build(cfg, st)
	if err != nil {
		return nil, nil, err
	}
	return srv, a.notify, nil
}

// build wires the App (auth, mailer, notifications, templates), creates the
// first admin on an empty database, registers every route and returns the
// http.Server with its middleware and timeouts. The middleware runs, from the
// outside in: request logging, security headers and checks, session loading,
// and the nav-badge refresh after writes.
func build(cfg config.Config, st *store.Store) (*App, *http.Server, error) {
	am, err := auth.New(cfg, st)
	if err != nil {
		return nil, nil, err
	}
	a := &App{cfg: cfg, st: st, auth: am, log: slog.Default()}
	a.mailer = notify.NewSMTPMailer(st, cfg.SMTPPassword, cfg.SMTPAllowedHosts)
	a.notify = notify.NewService(st, a.mailer)
	am.SetErrorHandler(func(w http.ResponseWriter, r *http.Request, status int, message string) {
		a.respondError(w, r, status, message, nil)
	})
	a.tpl = parseTemplates()
	if err := bootstrapAdmin(cfg, st); err != nil {
		return nil, nil, err
	}

	mux := http.NewServeMux()
	a.routes(mux)
	return a, &http.Server{
		Addr:              cfg.Addr,
		Handler:           a.httpObservability(a.securityBoundary(am.Middleware(a.refreshBadgesAfterMutation(mux)))),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(a.log.Handler(), slog.LevelError),
	}, nil
}

// bootstrapAdmin creates the configured admin account, but only when the
// database has no users at all. Restores and ordinary restarts keep their
// existing accounts untouched, even if the configured email or password
// differs from them.
func bootstrapAdmin(cfg config.Config, st *store.Store) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hasUsers, err := st.HasUsers(ctx)
	if err != nil || hasUsers {
		return err
	}
	adminHash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}
	return st.BootstrapAdminIfEmpty(ctx, cfg.AdminEmail, cfg.AdminName, adminHash)
}
