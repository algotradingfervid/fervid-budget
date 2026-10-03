// Command server runs Fervid Budget.
//
// Settings come from FERVID_* environment variables (see internal/config).
// The command does one of three things:
//
//	server --restore <dir>   restore the database and attachments from a backup, then exit
//	server --backup          make one backup now, prune old ones, then exit
//	server [--seed]          serve the web app until SIGINT or SIGTERM
//
// --seed first creates the admin user and sample data when the database is
// empty. While serving, two background jobs share the shutdown signal: the
// hourly reminder job and the automated daily backup.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fervidbudget/internal/app"
	"fervidbudget/internal/config"
	"fervidbudget/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "development"

// shutdownTimeout is how long shutdown waits for open requests, and then for
// each background job, before giving up on it.
const shutdownTimeout = 15 * time.Second

// main sets up JSON logging, runs the server and exits with status 1 after
// logging the reason if anything failed.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	logger.Info("starting Fervid Budget", "version", version)

	if err := run(logger); err != nil {
		var f *failure
		switch {
		case errors.As(err, &f):
			logger.Error(f.msg, append(f.attrs, "error", f.err)...)
		case !errors.Is(err, errAlreadyLogged):
			logger.Error("fervid budget stopped", "error", err)
		}
		os.Exit(1)
	}
}

// failure is a start-up or shutdown error together with the log message and
// extra log attributes it should be reported with, so main can log it once in
// the same shape each step always used.
type failure struct {
	msg   string
	attrs []any
	err   error
}

func (f *failure) Error() string { return f.msg + ": " + f.err.Error() }
func (f *failure) Unwrap() error { return f.err }

// fail wraps err with the log message (and optional key/value attributes)
// main reports it with.
func fail(msg string, err error, attrs ...any) error {
	return &failure{msg: msg, attrs: attrs, err: err}
}

// errAlreadyLogged tells main the failure has been logged where it happened
// and only the exit status is left to set.
var errAlreadyLogged = errors.New("already logged")

// run parses the flags and carries out the chosen mode (see the package
// comment). Returning, rather than exiting, lets the database close cleanly
// on every path.
func run(logger *slog.Logger) error {
	seed := flag.Bool("seed", false, "create admin and sample setup data if empty")
	backup := flag.Bool("backup", false, "create a database + attachments backup and exit")
	restore := flag.String("restore", "", "restore from a backup directory and exit; stop the app first")
	flag.Parse()

	cfg := config.Load()
	if err := cfg.EnsureDirs(); err != nil {
		return fail("failed to prepare application directories", err)
	}

	if *restore != "" {
		if err := store.Restore(context.Background(), *restore, cfg.DBPath, cfg.AttachmentDir); err != nil {
			return fail("restore failed", err, "backup_path", *restore)
		}
		fmt.Printf("Restored backup from %s\n", *restore)
		return nil
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fail("database open failed", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("database close failed", "error", err)
		}
	}()

	if *seed {
		if err := seedDatabase(cfg, db); err != nil {
			return err
		}
	}
	if *backup {
		return backupNow(logger, cfg)
	}
	return serve(logger, cfg, db)
}

// seedDatabase creates the first admin and the sample set-up data when the
// database has none, and prints what it created.
func seedDatabase(cfg config.Config, db *store.Store) error {
	result, err := db.Seed(context.Background(), store.SeedOptions{
		AdminEmail:    cfg.AdminEmail,
		AdminName:     cfg.AdminName,
		AdminPassword: cfg.AdminPassword,
	})
	if err != nil {
		return fail("seed failed", err)
	}
	fmt.Printf("Seed complete: admin_created=%v sample_created=%v sample_skipped=%v projects=%d heads=%d budgets=%d\n",
		result.AdminCreated, result.SampleCreated, result.SampleSkipped, result.Projects, result.Heads, result.Budgets)
	return nil
}

// backupNow makes one backup of the database and attachments, then prunes old
// backups. A pruning failure is only a warning: the new backup exists.
func backupNow(logger *slog.Logger, cfg config.Config) error {
	info, err := store.Backup(context.Background(), cfg.DBPath, cfg.AttachmentDir, cfg.BackupDir)
	if err != nil {
		return fail("backup failed", err)
	}
	if err := store.PruneBackups(cfg.BackupDir, cfg.BackupKeepDays, cfg.BackupKeepMonths, time.Now()); err != nil {
		logger.Warn("backup created but pruning failed", "backup_path", info.Path, "error", err)
	}
	fmt.Printf("Backup created at %s\n", info.Path)
	return nil
}

// serve runs the web server and the two background jobs until SIGINT or
// SIGTERM, or until the server fails. Either way it then stops the jobs and
// waits for any reminder or backup in flight to finish, so the database is
// not closed underneath it.
func serve(logger *slog.Logger, cfg config.Config, db *store.Store) error {
	srv, notifier, err := app.NewWithNotifier(cfg, db)
	if err != nil {
		return fail("server initialization failed", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The reminder job ticks hourly rather than daily because the admin can
	// set the pending and stale waits to any number of days: an hourly tick
	// makes the first reminder land within an hour of becoming due, and the
	// per-request "last reminded" stamp is what stops it repeating. It uses
	// the app's own notification service, so reminder emails go through the
	// same mailer (and SMTP host allowlist) as every other email.
	remindersDone := inBackground(func() {
		notifier.Scheduler(ctx, time.Hour, func() time.Time { return time.Now().UTC() })
	})

	// The automated daily backup checks every ten minutes and makes one backup
	// a day once the local clock passes FERVID_BACKUP_HOUR. It makes at most
	// one per day, so a restart or an extra tick never makes a second one.
	dailyBackup := store.DailyBackup{
		DBPath: cfg.DBPath, AttachmentDir: cfg.AttachmentDir, BackupDir: cfg.BackupDir,
		Hour: cfg.BackupHour, KeepDays: cfg.BackupKeepDays, KeepMonths: cfg.BackupKeepMonths, Log: logger,
	}
	backupsDone := inBackground(func() {
		dailyBackup.Scheduler(ctx, 10*time.Minute, time.Now)
	})

	serveErr := listenUntilStopped(ctx, logger, srv, cfg.Addr)

	stop()
	waitOrWarn(logger, remindersDone, "reminder scheduler did not stop in time")
	waitOrWarn(logger, backupsDone, "daily backup scheduler did not stop in time")
	if serveErr != nil {
		return serveErr
	}
	logger.Info("server stopped")
	return nil
}

// listenUntilStopped serves HTTP until ctx is cancelled (a shutdown signal),
// then shuts the server down, giving open requests shutdownTimeout to finish.
// It returns an error if the server fails to start or stops by itself, or if
// shutdown does not complete cleanly.
func listenUntilStopped(ctx context.Context, logger *slog.Logger, srv *http.Server, addr string) error {
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server started", "address", addr)
		serverErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fail("server stopped unexpectedly", err)
		}
		return nil
	case <-ctx.Done():
	}

	logger.Info("shutdown requested")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		if closeErr := srv.Close(); closeErr != nil {
			logger.Error("forced server close failed", "error", closeErr)
		}
		return errAlreadyLogged
	}
	if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fail("server shutdown returned an error", err)
	}
	return nil
}

// inBackground runs job on its own goroutine and returns a channel that is
// closed when job returns.
func inBackground(job func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		job()
	}()
	return done
}

// waitOrWarn waits up to shutdownTimeout for done to close, and logs warning
// if it does not.
func waitOrWarn(logger *slog.Logger, done <-chan struct{}, warning string) {
	select {
	case <-done:
	case <-time.After(shutdownTimeout):
		logger.Warn(warning)
	}
}
