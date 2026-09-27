package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNotificationLifecycleAndScopedFilters(t *testing.T) { // G19
	ctx := context.Background()
	s := newTestStore(t)
	me, err := s.CreateUser(ctx, "me@test", "Me", "h", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateUser(ctx, "other@test", "Other", "h", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}

	seed := []Notification{
		{UserID: me, Event: "request_returned", Title: "Kavita Rao returned PR-2026-000131", Body: "Attach the July invoice", Href: "/requests/7"},
		{UserID: me, Event: "reminder_pending", Title: "PR-2026-000134 has waited 3 days", Href: "/requests/7"},
		{UserID: me, Event: "request_approved", Title: "Kavita Rao approved PR-2026-000128", Href: "/requests/7"},
		{UserID: other, Event: "request_approved", Title: "Not yours", Href: "/requests/9"},
	}
	var ids []int64
	for _, n := range seed {
		id, err := s.AddNotification(ctx, n)
		if err != nil {
			t.Fatalf("AddNotification(%s): %v", n.Event, err)
		}
		ids = append(ids, id)
	}

	all, err := s.ListNotifications(ctx, NotificationFilter{UserID: me})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("my notifications = %d, want 3 (never another user's)", len(all))
	}
	if all[0].Title != "Kavita Rao approved PR-2026-000128" {
		t.Fatalf("newest first expected, got %q", all[0].Title)
	}

	// kind is derived at write time from the event; the caller never sets it.
	kinds := map[string]string{}
	for _, n := range all {
		kinds[n.Event] = n.Kind
	}
	if kinds["request_returned"] != "mention" || kinds["reminder_pending"] != "reminder" || kinds["request_approved"] != "activity" {
		t.Fatalf("derived kinds = %+v", kinds)
	}

	counts, err := s.NotificationCounts(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	if counts.All != 3 || counts.Unread != 3 || counts.Mentions != 1 || counts.Reminders != 1 {
		t.Fatalf("counts = %+v, want All 3 / Unread 3 / Mentions 1 / Reminders 1", counts)
	}

	for _, scope := range []struct {
		name string
		want int
	}{{"unread", 3}, {"mentions", 1}, {"reminders", 1}, {"all", 3}} {
		rows, err := s.ListNotifications(ctx, NotificationFilter{UserID: me, Scope: scope.name})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != scope.want {
			t.Fatalf("scope %q = %d rows, want %d", scope.name, len(rows), scope.want)
		}
	}

	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	if err := s.MarkNotificationRead(ctx, me, ids[0], now); err != nil {
		t.Fatalf("MarkNotificationRead: %v", err)
	}
	unread, err := s.UnreadNotificationCount(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	if unread != 2 {
		t.Fatalf("unread after one read = %d, want 2", unread)
	}
	// Reading it twice is not an error: a double click is not a failure.
	if err := s.MarkNotificationRead(ctx, me, ids[0], now); err != nil {
		t.Fatalf("second MarkNotificationRead: %v", err)
	}

	if err := s.MarkAllNotificationsRead(ctx, me, now); err != nil {
		t.Fatal(err)
	}
	if unread, err = s.UnreadNotificationCount(ctx, me); err != nil {
		t.Fatal(err)
	} else if unread != 0 {
		t.Fatalf("unread after mark-all = %d, want 0", unread)
	}
	// The other user's notification is untouched by my mark-all.
	if unread, err = s.UnreadNotificationCount(ctx, other); err != nil {
		t.Fatal(err)
	} else if unread != 1 {
		t.Fatalf("another user's unread count = %d, want 1 — mark-all must be scoped", unread)
	}
}

// Ownership is enforced in the query, so guessing an id gets you nothing.
func TestMarkNotificationReadRefusesAnotherUsersRow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	me, err := s.CreateUser(ctx, "me2@test", "Me", "h", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateUser(ctx, "other2@test", "Other", "h", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := s.AddNotification(ctx, Notification{UserID: other, Event: "request_approved", Title: "Theirs", Href: "/requests/1"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	if err := s.MarkNotificationRead(ctx, me, theirs, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("marking another user's notification = %v, want ErrNotFound", err)
	}
	unread, err := s.UnreadNotificationCount(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if unread != 1 {
		t.Fatal("another user's notification was marked read")
	}
}

func TestAddNotificationValidates(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.AddNotification(ctx, Notification{Event: "request_approved", Title: "No recipient"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing recipient = %v, want ErrValidation", err)
	}
	me, err := s.CreateUser(ctx, "me3@test", "Me", "h", "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNotification(ctx, Notification{UserID: me, Event: "request_approved", Title: "   "}); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank title = %v, want ErrValidation", err)
	}
}
