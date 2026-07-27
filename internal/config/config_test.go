package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndEnvironmentOverrides(t *testing.T) {
	for _, key := range []string{
		"FERVID_ADDR", "FERVID_DB", "FERVID_ATTACHMENT_DIR", "FERVID_BACKUP_DIR",
		"FERVID_SESSION_KEY", "FERVID_SECURE_COOKIES", "FERVID_ADMIN_EMAIL",
		"FERVID_ADMIN_PASSWORD", "FERVID_ADMIN_NAME", "FERVID_BACKUP_KEEP_DAYS",
	} {
		t.Setenv(key, "")
	}
	defaults := Load()
	if defaults.Addr != ":8080" || defaults.AdminEmail != "admin@fervid.local" || defaults.BackupKeepDays != 30 {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}
	if len(defaults.SessionKey) != 64 {
		t.Fatalf("generated session key length = %d, want 64", len(defaults.SessionKey))
	}

	t.Setenv("FERVID_ADDR", "127.0.0.1:9999")
	t.Setenv("FERVID_SESSION_KEY", "fixed-session-key")
	t.Setenv("FERVID_SECURE_COOKIES", "true")
	t.Setenv("FERVID_BACKUP_KEEP_DAYS", "14")
	overrides := Load()
	if overrides.Addr != "127.0.0.1:9999" || overrides.SessionKey != "fixed-session-key" ||
		!overrides.SecureCookies || overrides.BackupKeepDays != 14 {
		t.Fatalf("environment overrides were not loaded: %#v", overrides)
	}

	t.Setenv("FERVID_SECURE_COOKIES", "not-a-bool")
	t.Setenv("FERVID_BACKUP_KEEP_DAYS", "not-a-number")
	invalid := Load()
	if invalid.SecureCookies || invalid.BackupKeepDays != 30 {
		t.Fatalf("invalid environment values did not fall back: %#v", invalid)
	}
}

func TestEnsureDirs(t *testing.T) {
	root := t.TempDir()
	cfg := Config{
		DBPath:        filepath.Join(root, "db", "fervid.db"),
		AttachmentDir: filepath.Join(root, "uploads", "nested"),
		BackupDir:     filepath.Join(root, "backups"),
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Dir(cfg.DBPath), cfg.AttachmentDir, cfg.BackupDir} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Fatalf("%s was not created as a directory: %v", path, err)
		}
	}
}

func TestEnvironmentHelpers(t *testing.T) {
	t.Setenv("FERVID_TEST_VALUE", "present")
	if got := env("FERVID_TEST_VALUE", "fallback"); got != "present" {
		t.Fatalf("env = %q, want present", got)
	}
	t.Setenv("FERVID_TEST_VALUE", "")
	if got := env("FERVID_TEST_VALUE", "fallback"); got != "fallback" {
		t.Fatalf("empty env = %q, want fallback", got)
	}
}

func TestSMTPPasswordLoadsFromEnvOnly(t *testing.T) {
	t.Setenv("FERVID_SMTP_PASSWORD", "")
	if got := Load().SMTPPassword; got != "" {
		t.Fatalf("default SMTPPassword = %q, want empty", got)
	}
	t.Setenv("FERVID_SMTP_PASSWORD", "env-secret-123")
	if got := Load().SMTPPassword; got != "env-secret-123" {
		t.Fatalf("SMTPPassword = %q, want env-secret-123", got)
	}
}
