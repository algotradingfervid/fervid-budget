package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
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

// notify-2: the body keeps its line breaks and shows the request link as a
// link, not as flattened text. The row itself is the <a> that opens the
// request, so the link is drawn as link text inside it (a nested <a> is not
// valid HTML), and everything else in the body stays escaped.
func TestNotificationCentreRendersBodyLinesAndLink(t *testing.T) { // N1
	s := newAppTestServer(t)
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.DB().ExecContext(s.ctx, `INSERT INTO notifications(user_id,event,kind,title,body,href) VALUES(?,?,?,?,?,?)`,
		admin.ID, "request_submitted", "activity", "PR-2026-000005 needs your approval",
		"Rhea raised PR-2026-000005 <b>now</b>.\n\nProject: Ops / Rent\n\nOpen it: https://budget.test/requests/5.", "/requests/5"); err != nil {
		t.Fatal(err)
	}
	s.login(s.cfg.AdminEmail, testAdminPassword)
	body := responseBody(t, s.request(http.MethodGet, "/notifications", nil, ""))
	want := `<p>Rhea raised PR-2026-000005 &lt;b&gt;now&lt;/b&gt;.<br><br>Project: Ops / Rent<br><br>Open it: <span class="n-link">https://budget.test/requests/5</span>.</p>`
	if !strings.Contains(body, want) {
		t.Fatalf("notification body not rendered with its lines and link; want %s", want)
	}
}

func TestNotifBodyOnlyLinksTheRowsOwnTarget(t *testing.T) { // N1
	cases := []struct{ body, href, want string }{
		{"Open it: /requests/1", "/requests/1", `Open it: <span class="n-link">/requests/1</span>`},
		// /requests/10 is a different request: no partial match.
		{"See /requests/10", "/requests/1", `See /requests/10`},
		// A URL that is not where the row goes stays text, because the row's
		// own <a> would take a click on it somewhere else.
		{"Policy: https://intranet.test/p", "/requests/1", `Policy: https://intranet.test/p`},
		{"a\r\nb", "/requests/1", `a<br>b`},
	}
	for _, tc := range cases {
		if got := string(notifBody(tc.body, tc.href)); got != tc.want {
			t.Fatalf("notifBody(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
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

	// Opening one marks it read and forwards to where it points.
	resp := s.request(http.MethodGet, "/notifications/"+itoa64(returned)+"/open", nil, "")
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
	// And it cannot be opened or marked read by guessing its id.
	resp := s.request(http.MethodGet, "/notifications/"+itoa64(adminsRow)+"/open", nil, "")
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

// The destination comes from the stored row, never from the request, so a
// query string cannot aim the redirect anywhere.
func TestNotificationOpenRedirectsToTheStoredHrefOnly(t *testing.T) {
	s := newAppTestServer(t)
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	id := seedNotification(t, s, admin.ID, "request_approved", "activity", "Row", "/requests/1")
	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.request(http.MethodGet, "/notifications/"+itoa64(id)+"/open?href=https://evil.test/steal", nil, "")
	requireStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != "/requests/1" {
		t.Fatalf("redirect = %q, want the stored href", loc)
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

func TestAdminNotificationsScreenSavesAndBlocksUnprivileged(t *testing.T) { // D7, G20
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	body := responseBody(t, s.request(http.MethodGet, "/admin/notifications", nil, ""))
	if !strings.Contains(body, "FERVID_SMTP_PASSWORD") {
		t.Fatal("the rules screen must document the env-only SMTP password")
	}
	if strings.Contains(body, `type="password"`) {
		t.Fatal("the rules screen must not render a password input")
	}
	for _, event := range []string{
		"request_submitted", "request_edited", "request_returned", "request_rejected",
		"request_approved", "request_urgent", "request_on_hold", "request_cancellation_requested",
		"payment_settled", "payment_partial_review", "reminder_pending", "reminder_stale_reservation",
	} {
		if !strings.Contains(body, event) {
			t.Fatalf("rules screen missing event %q", event)
		}
	}
	for _, want := range []string{
		`class="t-cards"`, `class="pill good no-dot"`, `data-label="In-app"`, `data-label="Goes to"`,
		`data-label="Fixed To / CC"`, `class="overlay"`, `class="sheet"`, `class="sh-head"`,
		`class="sh-body stack-12"`, `class="sh-foot"`, `class="checkline"`, `class="hint"`,
		"Send a test email",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rules screen missing design-system markup %q", want)
		}
	}
	if strings.Contains(body, `class="badge`) {
		t.Fatal("templates must use .pill, never .badge (D5)")
	}
	// require_attachments and the reminder thresholds live on Configuration (D6).
	for _, gone := range []string{`name="require_attachments"`, `name="reminder_pending_days"`} {
		if strings.Contains(body, gone) {
			t.Fatalf("%s belongs to the Configuration screen, not the rules screen", gone)
		}
	}
	assertTCardsLabelled(t, body)

	// Saving a rule round-trips.
	resp := s.postForm("/admin/notifications/events/request_approved", url.Values{
		"email_enabled": {"on"}, "include_requester": {"on"}, "to_recipients": {"ops@example.test"},
		"subject_template": {"{{number}} approved"}, "body_template": {"{{approved_amount}} to {{payee}}"},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	got, err := s.st.NotificationSetting(s.ctx, "request_approved")
	if err != nil {
		t.Fatal(err)
	}
	if !got.EmailEnabled || !got.IncludeRequester || got.ToRecipients != "ops@example.test" {
		t.Fatalf("saved rule = %#v", got)
	}

	// A typo in a template is refused at save time, not stored.
	resp = s.postForm("/admin/notifications/events/request_approved", url.Values{
		"subject_template": {"{{nope}}"}, "body_template": {"b"},
	})
	requireStatus(t, resp, http.StatusBadRequest)
	_ = responseBody(t, resp)
	after, err := s.st.NotificationSetting(s.ctx, "request_approved")
	if err != nil {
		t.Fatal(err)
	}
	if after.SubjectTemplate != "{{number}} approved" {
		t.Fatalf("a rejected template was stored anyway: %q", after.SubjectTemplate)
	}

	// SMTP settings save, and no password key is ever written.
	resp = s.postForm("/admin/notifications/smtp", url.Values{
		"smtp_host": {"smtp.example.test"}, "smtp_port": {"2525"}, "base_url": {"https://budget.example.test"},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)
	mail, err := s.st.GetMailSettings(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if mail.SMTPHost != "smtp.example.test" || mail.SMTPPort != 2525 {
		t.Fatalf("smtp settings = %#v", mail)
	}
}

// ux-2: every result on the rules screen used to show twice — the layout's
// flash and the page's own callout (a pink "Done", or "That did not save" even
// for a failed test email) — and a rule refused for a bad field closed its
// sheet and threw away what the admin had typed.
func TestAdminNotificationsShowsEachResultOnceAndKeepsARefusedRule(t *testing.T) { // N2, N8
	s := newAppTestServer(t)
	s.login(s.cfg.AdminEmail, testAdminPassword)

	// No SMTP host is configured, so the test send fails: one error, no
	// "did not save" heading for something that was never a save.
	resp := s.postForm("/admin/notifications/test", url.Values{"test_to": {"x@example.test"}})
	body := responseBody(t, resp)
	if n := strings.Count(body, "The test email could not be sent"); n != 1 {
		t.Fatalf("the test-email failure is shown %d times, want once", n)
	}
	if strings.Contains(body, "That did not save") {
		t.Fatal("a failed test email is reported as a failed save")
	}

	// A refused rule: one message, inside the sheet, which comes back open with
	// the admin's own text in it; the stored rule and the other sheets are
	// untouched.
	resp = s.postForm("/admin/notifications/events/request_approved", url.Values{
		"email_enabled": {"on"}, "to_recipients": {"ops@example.test"},
		"subject_template": {"Hello {{numbr}}"}, "body_template": {"typed body {{number}}"},
	})
	requireStatus(t, resp, http.StatusBadRequest)
	body = responseBody(t, resp)
	if n := strings.Count(body, "unknown template field(s): numbr"); n != 1 {
		t.Fatalf("the refused-rule error is shown %d times, want once", n)
	}
	if !strings.Contains(body, `<div class="overlay" id="ev-request_approved" hidden data-reopen>`) {
		t.Fatal("the refused rule's sheet is not marked to reopen")
	}
	if !strings.Contains(body, `<div class="overlay" id="ev-request_submitted" hidden>`) {
		t.Fatal("another event's sheet was reopened too")
	}
	for _, want := range []string{`value="Hello {{numbr}}"`, `typed body {{number}}</textarea>`, `value="ops@example.test"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("the reopened sheet lost what the admin typed: missing %s", want)
		}
	}
	stored, err := s.st.NotificationSetting(s.ctx, "request_approved")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.SubjectTemplate, "numbr") || stored.ToRecipients == "ops@example.test" {
		t.Fatalf("a refused rule was stored: %#v", stored)
	}
}

func TestAdminNotificationsBlocksUnprivileged(t *testing.T) {
	s := newAppTestServer(t)
	hash, err := auth.HashPassword("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.st.CreateUser(s.ctx, "noadmin@example.test", "No Admin", hash, "data_entry", true); err != nil {
		t.Fatal(err)
	}
	s.login("noadmin@example.test", "EntryPassword123")
	resp := s.request(http.MethodGet, "/admin/notifications", nil, "")
	requireStatus(t, resp, http.StatusForbidden)
	_ = responseBody(t, resp)
	post := s.postForm("/admin/notifications/events/request_approved", url.Values{"email_enabled": {"on"}})
	requireStatus(t, post, http.StatusForbidden)
	_ = responseBody(t, post)
}

// The hooks are the part most likely to rot: a handler can be refactored and
// quietly stop notifying anyone. This drives the real approve route and asserts
// the requester ends up with an in-app row.
func TestApprovingARequestNotifiesTheRequester(t *testing.T) {
	s := newAppTestServer(t)
	admin, err := s.st.UserByEmail(s.ctx, s.cfg.AdminEmail)
	if err != nil {
		t.Fatal(err)
	}
	_, headID := s.seedHead("Notify")
	var projectID int64
	if err := s.st.DB().QueryRowContext(s.ctx, `SELECT project_id FROM heads WHERE id=?`, headID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("EntryPassword123")
	if err != nil {
		t.Fatal(err)
	}
	requesterID, err := s.st.CreateUser(s.ctx, "rq@example.test", "Rhea", hash, "data_entry", true)
	if err != nil {
		t.Fatal(err)
	}
	requester, err := s.st.UserByID(s.ctx, requesterID)
	if err != nil {
		t.Fatal(err)
	}
	reqID, err := s.st.CreateRequest(s.ctx, requester, store.RequestInput{
		Treatment: "budget", Type: "reimbursement", ShortTitle: "Team lunch",
		ProjectID: projectID, HeadID: headID, Amount: 50000, Purpose: "team lunch",
		ExpenseDate: "2026-07-21", ManagerID: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The seeded rule addresses the requester; email stays off so this proves
	// the in-app channel on its own.
	if err := s.st.SetNotificationSetting(s.ctx, admin, store.NotificationSetting{
		Event: "request_approved", IncludeRequester: true,
		SubjectTemplate: "{{number}} approved", BodyTemplate: "{{approved_amount}}",
	}); err != nil {
		t.Fatal(err)
	}

	s.login(s.cfg.AdminEmail, testAdminPassword)
	resp := s.postForm(fmt.Sprintf("/requests/%d/approve", reqID), url.Values{
		"approved_amount": {"500.00"}, "note": {"ok"},
	})
	requireStatus(t, resp, http.StatusSeeOther)
	_ = responseBody(t, resp)

	rows, err := s.st.ListNotifications(s.ctx, store.NotificationFilter{UserID: requesterID})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("requester in-app rows after approval = %d, want 1 — the approve hook is not firing", len(rows))
	}
	if !strings.Contains(rows[0].Title, "approved") {
		t.Fatalf("in-app title = %q", rows[0].Title)
	}
}
