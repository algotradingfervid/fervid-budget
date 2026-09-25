package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	Addr           string
	DBPath         string
	AttachmentDir  string
	BackupDir      string
	SessionKey     string
	SecureCookies  bool
	AdminEmail     string
	AdminPassword  string
	AdminName      string
	BackupKeepDays int
	// BackupKeepMonths is how many calendar months before the current one keep
	// their newest backup after the daily window has passed (§10: 12 monthly).
	BackupKeepMonths int
	// BackupHour is the local hour (0-23) at or after which the server makes
	// its automated daily backup.
	BackupHour int
	// SMTPPassword comes only from FERVID_SMTP_PASSWORD. It is never stored in
	// app_settings and never logged: the rest of the SMTP configuration is
	// admin-editable data, but the secret is not.
	SMTPPassword string
}

func Load() Config {
	return Config{
		Addr:             env("FERVID_ADDR", ":8080"),
		DBPath:           env("FERVID_DB", filepath.Join("data", "fervid.db")),
		AttachmentDir:    env("FERVID_ATTACHMENT_DIR", filepath.Join("data", "attachments")),
		BackupDir:        env("FERVID_BACKUP_DIR", filepath.Join("data", "backups")),
		SessionKey:       env("FERVID_SESSION_KEY", randomHex(32)),
		SecureCookies:    envBool("FERVID_SECURE_COOKIES", false),
		AdminEmail:       env("FERVID_ADMIN_EMAIL", "admin@fervid.local"),
		AdminPassword:    env("FERVID_ADMIN_PASSWORD", "admin123"),
		AdminName:        env("FERVID_ADMIN_NAME", "Fervid Admin"),
		BackupKeepDays:   envInt("FERVID_BACKUP_KEEP_DAYS", 30),
		BackupKeepMonths: envInt("FERVID_BACKUP_KEEP_MONTHS", 12),
		BackupHour:       envHour("FERVID_BACKUP_HOUR", 2),
		SMTPPassword:     env("FERVID_SMTP_PASSWORD", ""),
	}
}

func (c Config) EnsureDirs() error {
	for _, dir := range []string{filepath.Dir(c.DBPath), c.AttachmentDir, c.BackupDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return fallback
}

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

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "dev-session-key-change-me"
	}
	return hex.EncodeToString(buf)
}
