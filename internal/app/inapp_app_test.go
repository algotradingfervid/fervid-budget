package app

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"fervidbudget/internal/auth"
)

func seedNotification(t *testing.T, s *appTestServer, userID int64, event, kind, title, href string) int64 {
	t.Helper()
	res, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO notifications(user_id,event,kind,title,body,href) VALUES(?,?,?,?,?,?)`,
		userID, event, kind, title, "detail line", href)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestNotificationCentreRendersFilterStripAndMarksRead(t *testing.T) { // G19
	s := newAppTestServer(t)
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	returned := seedNotification(t, s, admin.ID, "request_returned", "mention", "Kavita Rao returned PR-2026-000131", "/requests/1")
	seedNotification(t, s, admin.ID, "reminder_pending", "reminder", "PR-2026-000134 has waited 3 days", "/requests/2")
	seedNotification(t, s, admin.ID, "request_approved", "activity", "Kavita Rao approved PR-2026-000128", "/requests/3")
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/notifications", nil, ""))
	for _, want := range []string{
		"Kavita Rao returned PR-2026-000131", "PR-2026-000134 has waited 3 days",
		`class="segmented"`, `class="notif-list"`, `class="notif unread"`, `class="n-ico"`, `class="n-main"`,
		"All", "Unread", "Mentions", "Reminders",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("notification centre missing %q", want)
		}
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("templates must use .pill, never .badge (D5)")
	}

	// The bell badge carries the unread count.
	if !strings.Contains(body, `<span class="dot">3</span>`) {
		t.Fatal("the shell bell does not show the unread count")
	}

	// Scope filters narrow the list.
	mentions := responseBody(t, s.request(http.MethodGet, "/notifications?scope=mentions", nil, ""))
	if !strings.Contains(mentions, "Kavita Rao returned") || strings.Contains(mentions, "has waited 3 days") {
		t.Fatal("scope=mentions did not narrow the list")
	}

	// Marking one read redirects to where it points and drops the unread count.
	resp := s.postForm("/notifications/"+itoa64(returned)+"/read", url.Values{"href": {"/requests/1"}})
	requireStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != "/requests/1" {
		t.Fatalf("redirect = %q, want the notification's own href", loc)
	}
	_ = responseBody(t, resp)
	unread, err := s.st.UnreadNotificationCount(s.ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unread != 2 {
		t.Fatalf("unread after marking one = %d, want 2", unread)
	}

	// Mark all read.
	resp = s.postForm("/notifications/read", url.Values{})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	if unread, err = s.st.UnreadNotificationCount(s.ctx, admin.ID); err != nil {
		t.Fatal(err)
	} else if unread != 0 {
		t.Fatalf("unread after mark-all = %d, want 0", unread)
	}
}

// The centre needs a session and nothing more, but it must not leak another
// user's rows — the store scopes every read to the caller.
func TestNotificationCentreNeverShowsAnotherUsersRows(t *testing.T) {
	s := newAppTestServer(t)
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	adminsRow := seedNotification(t, s, admin.ID, "request_approved", "activity", "Admin only secret", "/requests/1")

	hash, err := auth.HashPassword("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.CreateUser(s.ctx, "nosy@example.test", "Nosy", hash, "data_entry", true); err != nil {
		t.Fatal(err)
	}
	s.login("nosy@example.test", "EntryPassword123")

	body := responseBody(t, s.request(http.MethodGet, "/notifications", nil, ""))
	if strings.Contains(body, "Admin only secret") {
		t.Fatal("the centre leaked another user's notification")
	}
	// And it cannot be marked read by guessing its id.
	resp := s.postForm("/notifications/"+itoa64(adminsRow)+"/read", url.Values{})
	requireStatus(t, resp, http.StatusNotFound)
	_ = responseBody(t, resp)
	unread, err := s.st.UnreadNotificationCount(s.ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unread != 1 {
		t.Fatal("another user's notification was marked read through the handler")
	}
}

// A posted href is attacker-controlled: it must never redirect off-site.
func TestNotificationMarkReadRefusesAnOffsiteRedirect(t *testing.T) {
	s := newAppTestServer(t)
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	id := seedNotification(t, s, admin.ID, "request_approved", "activity", "Row", "/requests/1")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.postForm("/notifications/"+itoa64(id)+"/read", url.Values{"href": {"https://evil.test/steal"}})
	requireStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != "/notifications" {
		t.Fatalf("redirect = %q, want the local fallback", loc)
	}
	_ = responseBody(t, resp)
}

func TestNotificationCentreRequiresASession(t *testing.T) {
	s := newAppTestServer(t)
	resp := s.request(http.MethodGet, "/notifications", nil, "")
	if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusFound {
		t.Fatalf("signed-out status = %d, want a redirect to login", resp.StatusCode)
	}
	_ = responseBody(t, resp)
}

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }
