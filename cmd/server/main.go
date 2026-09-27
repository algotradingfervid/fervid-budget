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
	"fervidbudget/internal/notify"
	"fervidbudget/internal/store"
)

var version = "development"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	logger.Info("starting Fervid Budget", "version", version)

	seed := flag.Bool("seed", false, "create admin and sample setup data if empty")
	backup := flag.Bool("backup", false, "create a database + attachments backup and exit")
	restore := flag.String("restore", "", "restore from a backup directory and exit; stop the app first")
	flag.Parse()

	cfg := config.Load()
	if err := cfg.EnsureDirs(); err != nil {
		logger.Error("failed to prepare application directories", "error", err)
		os.Exit(1)
	}

	if *restore != "" {
		if err := store.Restore(context.Background(), *restore, cfg.DBPath, cfg.AttachmentDir); err != nil {
			logger.Error("restore failed", "backup_path", *restore, "error", err)
			os.Exit(1)
		}
		fmt.Printf("Restored backup from %s\n", *restore)
		return
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		logger.Error("database open failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("database close failed", "error", err)
		}
	}()

	if *seed {
		result, err := db.Seed(context.Background(), store.SeedOptions{
			AdminEmail:    cfg.AdminEmail,
			AdminName:     cfg.AdminName,
			AdminPassword: cfg.AdminPassword,
		})
		if err != nil {
			logger.Error("seed failed", "error", err)
			os.Exit(1)
		}
		fmt.Printf("Seed complete: admin_created=%v sample_created=%v sample_skipped=%v projects=%d heads=%d budgets=%d\n",
			result.AdminCreated, result.SampleCreated, result.SampleSkipped, result.Projects, result.Heads, result.Budgets)
	}

	if *backup {
		info, err := store.Backup(context.Background(), cfg.DBPath, cfg.AttachmentDir, cfg.BackupDir)
		if err != nil {
			logger.Error("backup failed", "error", err)
			os.Exit(1)
		}
		if err := store.PruneBackups(cfg.BackupDir, cfg.BackupKeepDays, cfg.BackupKeepMonths, time.Now()); err != nil {
			logger.Warn("backup created but pruning failed", "backup_path", info.Path, "error", err)
		}
		fmt.Printf("Backup created at %s\n", info.Path)
		return
	}

	srv, err := app.New(cfg, db)
	if err != nil {
		logger.Error("server initialization failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The reminder scheduler shares the shutdown context, so Ctrl-C stops it
	// with the server. It ticks hourly rather than daily because the admin can
	// set the pending and stale waits to any number of days: an hourly tick
	// makes the first reminder land within an hour of becoming due, and the
	// per-request reminder_last_sent stamp is what stops it repeating.
	notifier := notify.NewService(db, notify.NewSMTPMailer(db, cfg.SMTPPassword))
	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		notifier.Scheduler(ctx, time.Hour, func() time.Time { return time.Now().UTC() })
	}()

	// The automated daily backup (§10) shares the same shutdown context. It
	// checks every ten minutes and makes one backup a day once the local clock
	// passes FERVID_BACKUP_HOUR; RunIfDue is idempotent per day, so a restart or
	// an extra tick never makes a second one.
	dailyBackup := store.DailyBackup{
		DBPath: cfg.DBPath, AttachmentDir: cfg.AttachmentDir, BackupDir: cfg.BackupDir,
		Hour: cfg.BackupHour, KeepDays: cfg.BackupKeepDays, KeepMonths: cfg.BackupKeepMonths, Log: logger,
	}
	backupDone := make(chan struct{})
	go func() {
		defer close(backupDone)
		dailyBackup.Scheduler(ctx, 10*time.Minute, time.Now)
	}()

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server started", "address", cfg.Addr)
		serverErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("shutdown requested")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
			if closeErr := srv.Close(); closeErr != nil {
				logger.Error("forced server close failed", "error", closeErr)
			}
			os.Exit(1)
		}
		err := <-serverErr
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server shutdown returned an error", "error", err)
			os.Exit(1)
		}
		// Wait for the scheduler so a reminder in flight finishes writing
		// before the database is closed by the deferred handler above.
		select {
		case <-schedulerDone:
		case <-time.After(15 * time.Second):
			logger.Warn("reminder scheduler did not stop in time")
		}
		select {
		case <-backupDone:
		case <-time.After(15 * time.Second):
			logger.Warn("daily backup scheduler did not stop in time")
		}
		logger.Info("server stopped")
	}
}
