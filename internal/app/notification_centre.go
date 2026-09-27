package app

import (
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// The user's own notification centre (G19).
//
// It needs an authenticated session and no permission verb: every row it can
// return is scoped to the signed-in user by the store, so there is nothing a
// permission could add and nothing a guessed URL can reach.

var notificationScopes = map[string]bool{"all": true, "unread": true, "mentions": true, "reminders": true}

// notifGlyph is the .n-ico mark for a row's kind. Display only — the kind
// itself is derived once, at write time, in the store.
func notifGlyph(kind string) string {
	switch kind {
	case "mention":
		return "✎"
	case "reminder":
		return "◷"
	default:
		return "✓"
	}
}

// notifBody renders a row's stored plain-text body for the centre (notify-2).
// The text is escaped first; then its line breaks become <br>, and a URL that
// points where the row itself goes — the rendered {{link}}, absolute or
// relative — is marked as link text. It cannot be its own <a>: the whole row
// is already the <a> that opens it, and anchors do not nest. A URL pointing
// anywhere else is left as text, because a click on it would follow the row.
func notifBody(body, href string) template.HTML {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for i, line := range lines {
		words := strings.Split(line, " ")
		for j, w := range words {
			core := strings.TrimRight(w, ".,;:)!?")
			if href != "" && (core == href || (strings.HasSuffix(core, href) &&
				(strings.HasPrefix(core, "https://") || strings.HasPrefix(core, "http://")))) {
				words[j] = `<span class="n-link">` + template.HTMLEscapeString(core) + `</span>` +
					template.HTMLEscapeString(w[len(core):])
				continue
			}
			words[j] = template.HTMLEscapeString(w)
		}
		lines[i] = strings.Join(words, " ")
	}
	return template.HTML(strings.Join(lines, "<br>"))
}

// notificationPageSize is how many rows the centre draws at once. It is the
// store's own default limit, so the page boundary and the cap are one number.
const notificationPageSize = 100

// maxNotificationOffset bounds how deep the pager will walk. The window is
// fetched as offset+notificationPageSize rows (see below), so an unbounded
// offset from the query string would be an unbounded read — of the caller's own
// notifications, so not a disclosure, but a way to make one request expensive.
// A hundred pages is far past any real inbox.
const maxNotificationOffset = 10000

func (a *App) notificationCentre(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	scope := r.URL.Query().Get("scope")
	if !notificationScopes[scope] {
		scope = "all"
	}
	offset := int(parseID(r.URL.Query().Get("offset")))
	if offset < 0 {
		offset = 0
	}
	if offset > maxNotificationOffset {
		offset = maxNotificationOffset
	}
	// ListNotificationsPage, not ListNotifications: the rows were capped at 100
	// and the counts on the filter strip were not, so the strip promised more
	// than the list drew and nothing said so (F-G-037). Total and Truncated are
	// what let the screen admit it; the window is what makes the rest reachable.
	//
	// NotificationFilter carries a Limit and no Offset, so the window is taken by
	// asking for offset+one page and dropping the rows already shown. The read is
	// bounded by maxNotificationOffset, and page.Truncated still means exactly
	// "the fetch did not reach the end", which is exactly the condition for an
	// older page existing.
	page, err := a.st.ListNotificationsPage(r.Context(), store.NotificationFilter{
		UserID: user.ID, Scope: scope, Limit: offset + notificationPageSize,
	})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	rows := page.Notifications
	if offset < len(rows) {
		rows = rows[offset:]
	} else {
		rows = nil
	}
	counts, err := a.st.NotificationCounts(r.Context(), user.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "notifications", PageData{
		Title: "Notifications", Notifs: rows, NotifCounts: counts, NotifScope: scope,
		NotifPage: store.NotificationPage{
			Notifications: rows, Total: page.Total,
			Limit: notificationPageSize, Truncated: page.Truncated,
		},
		NotifOffset: offset,
	})
}

func (a *App) notificationsMarkAllRead(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	if err := a.st.MarkAllNotificationsRead(r.Context(), user.ID, time.Now().UTC()); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/notifications", http.StatusSeeOther)
}

// notificationOpen is what a .notif links to: it marks the row read and then
// forwards to wherever the row points.
//
// It is a GET because the approved design makes each row a plain anchor, and an
// anchor cannot POST. The side effect is a per-user read receipt on the reader's
// own row — the same thing every mail client does when you open a message — not
// a destructive action, so nothing here is worth a CSRF token.
//
// The destination comes from the stored row, never from the query string, so
// there is no redirect for an attacker to aim.
func (a *App) notificationOpen(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	id := parseID(r.PathValue("id"))
	n, err := a.st.Notification(r.Context(), user.ID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Not yours, or gone. Either way there is nothing to show.
			a.respondError(w, r, http.StatusNotFound, "That notification is not available.", nil)
			return
		}
		a.respondStoreError(w, r, err)
		return
	}
	if err := a.st.MarkNotificationRead(r.Context(), user.ID, id, time.Now().UTC()); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	target := n.Href
	if target == "" || target[0] != '/' {
		target = "/notifications"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
