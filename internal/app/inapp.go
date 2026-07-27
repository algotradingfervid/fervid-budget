package app

import (
	"errors"
	"net/http"
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

func (a *App) notificationCentre(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	scope := r.URL.Query().Get("scope")
	if !notificationScopes[scope] {
		scope = "all"
	}
	rows, err := a.st.ListNotifications(r.Context(), store.NotificationFilter{UserID: user.ID, Scope: scope})
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	counts, err := a.st.NotificationCounts(r.Context(), user.ID)
	if err != nil {
		a.respondStoreError(w, r, err)
		return
	}
	a.render(w, r, "notifications", PageData{
		Title: "Notifications", Notifs: rows, NotifCounts: counts, NotifScope: scope,
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
