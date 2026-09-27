// Package config reads the server's settings from FERVID_* environment
// variables, with a safe default for each, and prepares the private data
// directories. Settings an admin can change in the running app (SMTP host,
// reminder waits and so on) live in the database, not here.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds every operator setting the server reads at start-up. Each
// field's comment names the environment variable it comes from and its
// default.
type Config struct {
	// Addr is the host:port the web server listens on (FERVID_ADDR,
	// default 127.0.0.1:8080).
	Addr string
	// DBPath is the SQLite database file (FERVID_DB, default data/fervid.db).
	DBPath string
	// AttachmentDir holds uploaded invoices and receipts
	// (FERVID_ATTACHMENT_DIR, default data/attachments).
	AttachmentDir string
	// BackupDir holds manual and automated backups (FERVID_BACKUP_DIR,
	// default data/backups).
	BackupDir string
	// SessionKey signs session cookies and CSRF tokens (FERVID_SESSION_KEY).
	// When it is unset a random key is made at start-up, so every restart
	// signs all users out; production must set it.
	SessionKey string
	// SecureCookies marks cookies HTTPS-only (FERVID_SECURE_COOKIES, default
	// true). Only a plain-HTTP local run should turn it off.
	SecureCookies bool
	// AdminEmail, AdminPassword and AdminName describe the first administrator
	// that --seed creates when the database has none (FERVID_ADMIN_EMAIL,
	// FERVID_ADMIN_PASSWORD, FERVID_ADMIN_NAME).
	AdminEmail    string
	AdminPassword string
	AdminName     string
	// BackupKeepDays is how many days of daily backups are kept
	// (FERVID_BACKUP_KEEP_DAYS, default 30).
	BackupKeepDays int
	// BackupKeepMonths is how many calendar months before the current one keep
	// their newest backup after the daily window has passed
	// (FERVID_BACKUP_KEEP_MONTHS, default 12).
	BackupKeepMonths int
	// BackupHour is the local hour (0-23) at or after which the server makes
	// its automated daily backup (FERVID_BACKUP_HOUR, default 2).
	BackupHour int
	// SMTPPassword comes only from FERVID_SMTP_PASSWORD. It is never stored in
	// app_settings and never logged: the rest of the SMTP configuration is
	// admin-editable data, but the secret is not.
	SMTPPassword string
	// TrustedProxies lists the reverse-proxy addresses or CIDR ranges whose
	// X-Forwarded-For header is believed when working out the client address
	// that login rate limits count against (FERVID_TRUSTED_PROXIES,
	// comma-separated, default none).
	TrustedProxies []string
	// SMTPAllowedHosts lists the only SMTP hosts the server will send mail
	// (and the SMTP password) to (FERVID_SMTP_ALLOWED_HOSTS, comma-separated,
	// default none, which means no email is sent).
	SMTPAllowedHosts []string
	// AttachmentQuotaBytes caps the total size of stored attachments
	// (FERVID_ATTACHMENT_QUOTA_MB, in megabytes, default 1024).
	AttachmentQuotaBytes int64
	// LoginLimitPerIP and LoginLimitGlobal cap POST requests to /login and its
	// sub-pages (sign-in, reset request, reset) per minute: per client address
	// and across all clients (FERVID_LOGIN_LIMIT_PER_IP and
	// FERVID_LOGIN_LIMIT_GLOBAL). Production keeps the defaults (30 and 300);
	// test suites that sign in many times from one address raise them.
	LoginLimitPerIP  int
	LoginLimitGlobal int
}

// Load reads the configuration from the environment. A missing or unreadable
// value falls back to its default; Load never fails.
func Load() Config {
	return Config{
		Addr:                 env("FERVID_ADDR", "127.0.0.1:8080"),
		DBPath:               env("FERVID_DB", filepath.Join("data", "fervid.db")),
		AttachmentDir:        env("FERVID_ATTACHMENT_DIR", filepath.Join("data", "attachments")),
		BackupDir:            env("FERVID_BACKUP_DIR", filepath.Join("data", "backups")),
		SessionKey:           env("FERVID_SESSION_KEY", randomHex(32)),
		SecureCookies:        envBool("FERVID_SECURE_COOKIES", true),
		AdminEmail:           env("FERVID_ADMIN_EMAIL", "admin@fervid.local"),
		AdminPassword:        env("FERVID_ADMIN_PASSWORD", ""),
		AdminName:            env("FERVID_ADMIN_NAME", "Fervid Admin"),
		BackupKeepDays:       envInt("FERVID_BACKUP_KEEP_DAYS", 30),
		BackupKeepMonths:     envInt("FERVID_BACKUP_KEEP_MONTHS", 12),
		BackupHour:           envHour("FERVID_BACKUP_HOUR", 2),
		SMTPPassword:         env("FERVID_SMTP_PASSWORD", ""),
		TrustedProxies:       envList("FERVID_TRUSTED_PROXIES"),
		SMTPAllowedHosts:     envList("FERVID_SMTP_ALLOWED_HOSTS"),
		AttachmentQuotaBytes: int64(envInt("FERVID_ATTACHMENT_QUOTA_MB", 1024)) * 1024 * 1024,
		LoginLimitPerIP:      envPositiveInt("FERVID_LOGIN_LIMIT_PER_IP", 30),
		LoginLimitGlobal:     envPositiveInt("FERVID_LOGIN_LIMIT_GLOBAL", 300),
	}
}

// EnsureDirs creates the database, attachment and backup directories if they
// are missing and makes them readable by the server's user only (mode 0700).
// It refuses to use the current or root directory for attachments or backups,
// since that would change the permissions of an unrelated directory.
func (c Config) EnsureDirs() error {
	for _, dir := range []string{filepath.Dir(c.DBPath), c.AttachmentDir, c.BackupDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	for _, dir := range []string{c.AttachmentDir, c.BackupDir} {
		if filepath.Clean(dir) == "." || filepath.Clean(dir) == string(os.PathSeparator) {
			return fmt.Errorf("private storage must use a dedicated directory")
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}

// env returns the variable's value, or fallback when it is unset or empty.
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envBool reads true/false (any form strconv.ParseBool accepts); anything
// else falls back.
func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return fallback
}

// envInt reads a whole number; text that is not a number falls back.
func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}

// envHour reads an hour of the day; anything outside 0-23 falls back, as an
// unparseable number does.
func envHour(key string, fallback int) int {
	if h := envInt(key, fallback); h >= 0 && h <= 23 {
		return h
	}
	return fallback
}

// envPositiveInt reads a whole number above zero. Zero, a negative number or
// text falls back to the default, so a typo can never switch a limit off.
func envPositiveInt(key string, fallback int) int {
	if n := envInt(key, fallback); n > 0 {
		return n
	}
	return fallback
}

// envList splits a comma-separated variable into its non-empty items. Items
// are kept exactly as written (spaces are not trimmed); an unset variable
// gives an empty list.
func envList(key string) []string {
	return strings.FieldsFunc(env(key, ""), func(r rune) bool { return r == ',' })
}

// randomHex returns n random bytes as hex. It is the fallback session key.
func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Errorf("generate secure session key: %w", err))
	}
	return hex.EncodeToString(buf)
}
