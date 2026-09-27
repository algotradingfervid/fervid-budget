package app

// The user's own notification centre (/notifications). It needs a signed-in
// user and no permission: the store only ever returns the signed-in user's
// own rows, so a guessed URL cannot reach anyone else's.

import (
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"fervidbudget/internal/auth"
	"fervidbudget/internal/store"
)

// notificationScopes are the filter tabs the centre accepts; anything else
// shows "all".
var notificationScopes = map[string]bool{"all": true, "unread": true, "mentions": true, "reminders": true}

// notifGlyph is the icon shown for a notification of the given kind.
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

// notifBody renders a notification's stored plain-text body as HTML. The text
// is escaped, line breaks become <br>, and a URL equal to the row's own link
// (relative, or absolute ending in it) is styled as link text. It cannot be a
// real <a>: the whole row is already the link that opens it, and links cannot
// nest. Any other URL stays plain text.
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

// notificationPageSize is how many rows the centre shows per page (the
// store's default limit).
const notificationPageSize = 100

// maxNotificationOffset caps the ?offset= the pager accepts. Each page reads
// offset+notificationPageSize rows, so without a cap one request could be
// made arbitrarily expensive. A hundred pages is far past any real inbox.
const maxNotificationOffset = 10000

// notificationCentre lists the user's notifications for the chosen filter,
// one page at a time, with the counts for each filter tab.
func (a *App) notificationCentre(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	scope := r.URL.Query().Get("scope")
	if !notificationScopes[scope] {
		scope = "all"
	}
	offset := min(max(int(parseID(r.URL.Query().Get("offset"))), 0), maxNotificationOffset)
	// The store filter has a limit but no offset, so read offset plus one page
	// and drop the rows before offset. page.Truncated then means exactly that
	// older rows exist beyond this page, and Total is the full count.
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

// notificationsMarkAllRead marks all of the user's notifications read and
// returns to the centre.
func (a *App) notificationsMarkAllRead(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	if err := a.st.MarkAllNotificationsRead(r.Context(), user.ID, time.Now().UTC()); err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/notifications", http.StatusSeeOther)
}

// notificationOpen is where each notification row links: it marks the row
// read and redirects to the page the row points at (or the centre when that
// is not a local path). It is a GET because each row is a plain link; marking
// your own row read is harmless, so it needs no CSRF token. The destination
// comes from the stored row, never the query string, so it cannot be used as
// an open redirect. Someone else's row, or a missing one, answers 404.
func (a *App) notificationOpen(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	id := parseID(r.PathValue("id"))
	n, err := a.st.Notification(r.Context(), user.ID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
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
