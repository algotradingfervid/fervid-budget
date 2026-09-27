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

type App struct {
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
	notify     *notify.Service
	mailer     notify.Mailer
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

	ctx := contextWithTimeout()
	defer ctx.cancel()
	hasUsers, err := st.HasUsers(ctx.ctx)
	if err != nil {
		return nil, nil, err
	}
	// Bootstrap configuration applies only to a new, empty installation.
	// Restores and ordinary restarts keep their existing accounts untouched,
	// even if the configured email or password differs from those accounts.
	if !hasUsers {
		adminHash, err := auth.HashPassword(cfg.AdminPassword)
		if err != nil {
			return nil, nil, err
		}
		if err := st.BootstrapAdminIfEmpty(ctx.ctx, cfg.AdminEmail, cfg.AdminName, adminHash); err != nil {
			return nil, nil, err
		}
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

func contextWithTimeout() struct {
	ctx    context.Context
	cancel func()
} {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	return struct {
		ctx    context.Context
		cancel func()
	}{ctx, cancel}
}
