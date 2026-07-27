package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMailSettingsRoundTripAndNeverStoreAPassword(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)
	in := MailSettings{SMTPHost: "smtp.test", SMTPPort: 2525, SMTPUsername: "u",
		SMTPFromName: "Fervid", SMTPFromAddr: "no@reply.test", ManagementRecipients: "boss@test",
		BaseURL: "https://budget.fervid.test"}
	if err := s.SetMailSettings(ctx, actor, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMailSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Fatalf("GetMailSettings = %#v, want %#v", got, in)
	}
	rows, err := s.DB().QueryContext(ctx, `SELECT key FROM app_settings`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(k), "password") {
			t.Fatalf("app_settings must never hold a password key, found %q", k)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// A trailing slash on the base URL would produce "https://host//requests/1".
func TestMailSettingsNormalisesBaseURL(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)
	if err := s.SetMailSettings(ctx, actor, MailSettings{BaseURL: "https://budget.test/ "}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMailSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != "https://budget.test" {
		t.Fatalf("BaseURL = %q, want the trailing slash trimmed", got.BaseURL)
	}
}

func TestNotificationSettingUpsertAndFetch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)
	in := NotificationSetting{Event: "request_approved", EmailEnabled: true, ToRecipients: "ops@test",
		IncludeRequester: true, IncludeAccounts: true, SubjectTemplate: "S {{number}}", BodyTemplate: "B {{amount}}"}
	if err := s.SetNotificationSetting(ctx, actor, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.NotificationSetting(ctx, "request_approved")
	if err != nil {
		t.Fatal(err)
	}
	// The editable columns round-trip exactly…
	if got.EmailEnabled != in.EmailEnabled || got.ToRecipients != in.ToRecipients || got.CcRecipients != in.CcRecipients ||
		got.IncludeRequester != in.IncludeRequester || got.IncludeManager != in.IncludeManager || got.IncludeAccounts != in.IncludeAccounts ||
		got.SubjectTemplate != in.SubjectTemplate || got.BodyTemplate != in.BodyTemplate {
		t.Fatalf("NotificationSetting editable fields = %#v, want %#v", got, in)
	}
	// …and the seeded presentation columns survive an edit that never sends them.
	if got.Label != "Approved" || got.Audience == "" {
		t.Fatalf("seeded label/audience lost on upsert: %q / %q", got.Label, got.Audience)
	}
	all, err := s.AllNotificationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Screen order is the seeded sort_order, and notification_settings is
	// append-only across migrations — v9 adds nine more events (F-F-06). So this
	// pins that v7's twelve come first and in their seeded order, which is what
	// the screen's order actually means, rather than a total that grows with each
	// migration that adds an event.
	if len(all) < len(defaultNotificationSettings) {
		t.Fatalf("events = %d, want at least the %d v7 seeded", len(all), len(defaultNotificationSettings))
	}
	for i, want := range defaultNotificationSettings {
		if all[i].Event != want.Event {
			t.Fatalf("AllNotificationSettings must return screen order: row %d = %q, want %q", i, all[i].Event, want.Event)
		}
	}
}

// An unknown event must be refused, not quietly created: a rule nothing ever
// fires is worse than an error, because the admin believes it is configured.
func TestSetNotificationSettingRejectsUnknownEvent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	actor, _ := seedActorAndHead(t, s, ctx)
	err := s.SetNotificationSetting(ctx, actor, NotificationSetting{Event: "invented_event", EmailEnabled: true})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown event = %v, want ErrNotFound", err)
	}
	if err := s.SetNotificationSetting(ctx, actor, NotificationSetting{Event: "  "}); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank event = %v, want ErrValidation", err)
	}
}

// Note on the fixture: CreateUser back-fills a user_roles row from the legacy
// role string, and every non-admin lands on the seeded Accounts role — which
// really does hold payment:process. So the negative case is asserted with
// role:edit, a grant only Admin holds; asserting it with payment:process would
// have been testing the seed, not the query.
func TestUsersWithPermissionResolvesGrantHolders(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	acct1, err := s.CreateUser(ctx, "acct1@test", "Acct One", "h", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "acct2@test", "Acct Two", "h", "data_entry", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "boss@test", "Boss", "h", "admin", true); err != nil {
		t.Fatal(err)
	}

	emails := func(resource, action string) map[string]bool {
		t.Helper()
		users, err := s.UsersWithPermission(ctx, resource, action)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, u := range users {
			got[u.Email] = true
		}
		return got
	}

	accounts := emails("payment", "process")
	if !accounts["acct1@test"] || !accounts["acct2@test"] {
		t.Fatalf("UsersWithPermission missed an Accounts grant holder: %+v", accounts)
	}

	admins := emails("role", "edit")
	if !admins["boss@test"] {
		t.Fatalf("the admin does not resolve for role:edit: %+v", admins)
	}
	if admins["acct1@test"] || admins["acct2@test"] {
		t.Fatalf("a user without role:edit was resolved as a recipient: %+v", admins)
	}

	// An inactive user is not a recipient: they cannot act on what they are told.
	if _, err := s.DB().ExecContext(ctx, `UPDATE users SET active=0 WHERE id=?`, acct1); err != nil {
		t.Fatal(err)
	}
	if emails("payment", "process")["acct1@test"] {
		t.Fatal("a deactivated user is still resolved as a recipient")
	}
}
