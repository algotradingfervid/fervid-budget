package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// The in-app notification centre.
//
// This is a real persisted channel, not a view over the work queues: every
// event writes one row per recipient and those rows always fire, whether or not
// email is enabled for the event.
//
// Two rules live here rather than in a handler:
//
//  1. Ownership is enforced in the query. Every read and every mutation is
//     scoped by user_id, so marking or listing another user's row is not a
//     permission check that could be forgotten — it simply matches nothing.
//  2. kind is derived once, at write time, from the event, so the filter strip
//     never re-derives it and the two can never disagree.

// Notification is one in-app notification row. RequestID is nil when the
// notification is not about a request; ReadAt is nil while unread.
type Notification struct {
	ID        int64
	UserID    int64
	Event     string
	Kind      string
	Title     string
	Body      string
	Href      string
	RequestID *int64
	ReadAt    *time.Time
	CreatedAt time.Time
}

// NotificationFilter scopes a read. Scope is "" / "all" | "unread" |
// "mentions" | "reminders", matching the .segmented strip on the centre screen.
type NotificationFilter struct {
	UserID int64
	Scope  string
	Limit  int
}

// NotificationCounts are the numbers on the centre's filter strip.
type NotificationCounts struct {
	All       int
	Unread    int
	Mentions  int
	Reminders int
}

// notificationKind maps an event to the filter it belongs in: "reminder" for
// the scheduler's nudges; "mention" when someone wrote to the reader and is
// waiting on an answer (returned, on hold, a partial-payment concern) or ended
// their request against their wishes (rejected, cancelled); "activity" for
// everything else, which reports something already settled.
func notificationKind(event string) string {
	switch event {
	case "reminder_pending", "reminder_stale_reservation":
		return "reminder"
	case "request_returned", "request_on_hold", "request_rejected", "payment_partial_concern", "request_cancelled":
		return "mention"
	default:
		return "activity"
	}
}

// AddNotification stores a notification for in.UserID and returns its id. Kind
// is derived from the event unless in.Kind is set.
func (s *Store) AddNotification(ctx context.Context, in Notification) (int64, error) {
	if in.UserID == 0 {
		return 0, fmt.Errorf("%w: a notification needs a recipient", ErrValidation)
	}
	if strings.TrimSpace(in.Title) == "" {
		return 0, fmt.Errorf("%w: a notification needs a title", ErrValidation)
	}
	kind := in.Kind
	if kind == "" {
		kind = notificationKind(in.Event)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO notifications(user_id,event,kind,request_id,title,body,href)
		VALUES(?,?,?,?,?,?,?)`, in.UserID, in.Event, kind, in.RequestID, in.Title, in.Body, in.Href)
	if err != nil {
		return 0, classify(err)
	}
	return res.LastInsertId()
}

// notificationSelect reads the columns scanNotification expects.
const notificationSelect = `SELECT id,user_id,event,kind,request_id,title,body,href,read_at,created_at FROM notifications`

// defaultNotificationLimit is the cap applied when a filter names no number.
const defaultNotificationLimit = 100

// NotificationPage is a capped list of notifications that says what it left
// out. It has the same Total/Truncated shape as RequestPage.
type NotificationPage struct {
	Notifications []Notification
	// Total rows matching the same filter, ignoring Limit. For each scope this
	// is exactly the matching NotificationCounts field, so the number on the
	// filter strip and the rows beneath it cannot disagree.
	Total int
	// Limit as it was applied.
	Limit int
	// Truncated reports that rows matching the filter are not in Notifications.
	Truncated bool
}

// notificationScopeWhere turns a filter scope into SQL. The rows query and the
// count share it, so they always select the same set.
func notificationScopeWhere(scope string) string {
	switch scope {
	case "unread":
		return ` AND read_at IS NULL`
	case "mentions":
		return ` AND kind='mention'`
	case "reminders":
		return ` AND kind='reminder'`
	}
	return ""
}

// ListNotificationsPage returns up to f.Limit notifications for the filter
// (defaultNotificationLimit when unset), newest first, plus the total that
// match, so the screen can say when the list is cut off.
func (s *Store) ListNotificationsPage(ctx context.Context, f NotificationFilter) (NotificationPage, error) {
	visible, visibleArgs, err := s.notificationVisibility(ctx, f.UserID)
	if err != nil {
		return NotificationPage{}, err
	}
	where := ` WHERE user_id=?` + visible + notificationScopeWhere(f.Scope)
	args := append([]any{f.UserID}, visibleArgs...)

	var page NotificationPage
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications`+where, args...).Scan(&page.Total); err != nil {
		return NotificationPage{}, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = defaultNotificationLimit
	}
	page.Limit = limit

	// CURRENT_TIMESTAMP is second-resolution, so a burst written in one request
	// shares a timestamp; id is the tie-break that keeps the order stable.
	rows, err := s.db.QueryContext(ctx, notificationSelect+where+` ORDER BY created_at DESC, id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return NotificationPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return NotificationPage{}, err
		}
		page.Notifications = append(page.Notifications, n)
	}
	if err := rows.Err(); err != nil {
		return NotificationPage{}, err
	}
	page.Truncated = len(page.Notifications) < page.Total
	return page, nil
}

// ListNotifications returns the matching rows and nothing about what it left
// out. It is the shape the notify package's tests read the channel with, where
// the cap is never in play; a screen renders ListNotificationsPage instead, so
// it can say what it is not showing.
func (s *Store) ListNotifications(ctx context.Context, f NotificationFilter) ([]Notification, error) {
	page, err := s.ListNotificationsPage(ctx, f)
	if err != nil {
		return nil, err
	}
	return page.Notifications, nil
}

// Notification reads one row, scoped to its owner. Another user's id returns
// ErrNotFound rather than the row, so the caller cannot leak it by guessing.
func (s *Store) Notification(ctx context.Context, userID, id int64) (Notification, error) {
	visible, visibleArgs, err := s.notificationVisibility(ctx, userID)
	if err != nil {
		return Notification{}, err
	}
	return scanNotification(s.db.QueryRowContext(ctx, notificationSelect+` WHERE id=? AND user_id=?`+visible, append([]any{id, userID}, visibleArgs...)...))
}

// scanNotification reads one row selected by notificationSelect; sql.ErrNoRows
// becomes ErrNotFound.
func scanNotification(sc rowScanner) (Notification, error) {
	var n Notification
	err := sc.Scan(&n.ID, &n.UserID, &n.Event, &n.Kind, &n.RequestID, &n.Title, &n.Body, &n.Href, &n.ReadAt, &n.CreatedAt)
	if err == sql.ErrNoRows {
		return n, ErrNotFound
	}
	return n, err
}

// NotificationCounts counts the user's visible notifications for each filter.
func (s *Store) NotificationCounts(ctx context.Context, userID int64) (NotificationCounts, error) {
	var c NotificationCounts
	visible, visibleArgs, err := s.notificationVisibility(ctx, userID)
	if err != nil {
		return c, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN read_at IS NULL THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN kind='mention' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN kind='reminder' THEN 1 ELSE 0 END),0)
		FROM notifications WHERE user_id=?`+visible, append([]any{userID}, visibleArgs...)...).
		Scan(&c.All, &c.Unread, &c.Mentions, &c.Reminders)
	return c, err
}

// UnreadNotificationCount counts the user's visible unread notifications (the
// number on the bell).
func (s *Store) UnreadNotificationCount(ctx context.Context, userID int64) (int, error) {
	var n int
	visible, visibleArgs, err := s.notificationVisibility(ctx, userID)
	if err != nil {
		return 0, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id=? AND read_at IS NULL`+visible, append([]any{userID}, visibleArgs...)...).Scan(&n)
	return n, err
}

// MarkNotificationRead is scoped by user_id, so another user's row matches
// nothing and returns ErrNotFound rather than being marked by a guessed id.
func (s *Store) MarkNotificationRead(ctx context.Context, userID, id int64, now time.Time) error {
	visible, visibleArgs, err := s.notificationVisibility(ctx, userID)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE notifications SET read_at=? WHERE id=? AND user_id=? AND read_at IS NULL`+visible,
		append([]any{now.UTC(), id, userID}, visibleArgs...)...)
	if err != nil {
		return classify(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Either it is not yours, it does not exist, or it was already read.
		// Distinguish the last case so a double click is not an error.
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE id=? AND user_id=?`+visible, append([]any{id, userID}, visibleArgs...)...).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
	}
	return nil
}

// MarkAllNotificationsRead marks every visible unread notification of the user
// as read at now.
func (s *Store) MarkAllNotificationsRead(ctx context.Context, userID int64, now time.Time) error {
	visible, visibleArgs, err := s.notificationVisibility(ctx, userID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE notifications SET read_at=? WHERE user_id=? AND read_at IS NULL`+visible, append([]any{now.UTC(), userID}, visibleArgs...)...)
	return classify(err)
}

// notificationVisibility is the WHERE fragment, with its arguments, that hides
// a notification about a request its owner may no longer see. A notification
// was addressed to the person when the event happened; a reassignment, a role
// change or a deactivation since then must not leave them reading about a
// request whose page now answers 404. Notifications that name no request are
// always visible to their owner.
//
// The owner's current permissions are read once here and the row rule is the
// same requestScopeSQL every list uses, so the inbox can never disagree with
// the request screens about who may see what. An inactive owner holds no
// permissions at all, so they see only their request-less notifications.
func (s *Store) notificationVisibility(ctx context.Context, userID int64) (string, []any, error) {
	ps, err := effectivePermissionsQ(ctx, s.db, userID)
	if err != nil {
		return "", nil, err
	}
	if !ps.Can("request", "view") {
		return ` AND request_id IS NULL`, nil, nil
	}
	scopeSQL, args := requestScopeSQL("nr", ps.Scope("request"), userID)
	return ` AND (request_id IS NULL OR EXISTS (SELECT 1 FROM payment_requests nr WHERE nr.id=notifications.request_id AND ` + scopeSQL + `))`, args, nil
}
