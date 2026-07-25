package store

import (
	"context"
	"testing"
)

func TestAppSettingRoundTripAndDefault(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _, _ := seedRequestActors(t, s, ctx)

	// Migration v3 seeds require_attachments='0'.
	v, err := s.AppSetting(ctx, "require_attachments")
	if err != nil {
		t.Fatalf("AppSetting: %v", err)
	}
	if v != "0" {
		t.Fatalf("default require_attachments = %q, want 0", v)
	}
	// An absent key returns "" (no error).
	if v, err := s.AppSetting(ctx, "does_not_exist"); err != nil || v != "" {
		t.Fatalf("absent key = %q, %v; want \"\", nil", v, err)
	}
	// SetAppSetting upserts the value.
	if err := s.SetAppSetting(ctx, actor, "require_attachments", "1"); err != nil {
		t.Fatalf("SetAppSetting: %v", err)
	}
	if v, _ := s.AppSetting(ctx, "require_attachments"); v != "1" {
		t.Fatalf("after set = %q, want 1", v)
	}
}

// D6: the Configuration screen saves a whole form atomically.
func TestSetAppSettingsIsAtomicAndAudited(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _, _ := seedRequestActors(t, s, ctx)

	if err := s.SetAppSettings(ctx, actor, map[string]string{
		"number_prefix": "REQ", "urgency_mode": "free", "require_attachments": "1",
	}); err != nil {
		t.Fatalf("SetAppSettings: %v", err)
	}
	all, err := s.AppSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"number_prefix": "REQ", "urgency_mode": "free", "require_attachments": "1"} {
		if all[k] != want {
			t.Fatalf("AppSettings[%q] = %q, want %q", k, all[k], want)
		}
	}
	audit, err := s.Audit(ctx, "app_setting", 0, 5)
	if err != nil || len(audit) == 0 {
		t.Fatalf("audit = %#v, %v; want a configuration entry", audit, err)
	}
}
