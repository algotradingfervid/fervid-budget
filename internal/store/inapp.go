package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The in-app notification centre (G19).
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

type NotificationCounts struct {
	All       int
	Unread    int
	Mentions  int
	Reminders int
}

// notificationKind maps an event to the filter bucket it belongs in. A mention
// is an event where a person wrote something addressed to you and is waiting on
// an answer; everything else is activity.
func notificationKind(event string) string {
	switch event {
	case "reminder_pending", "reminder_stale_reservation":
		return "reminder"
	case "request_returned", "request_on_hold", "request_rejected":
		return "mention"
	default:
		return "activity"
	}
}

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

const notificationSelect = `SELECT id,user_id,event,kind,request_id,title,body,href,read_at,created_at FROM notifications`

func (s *Store) ListNotifications(ctx context.Context, f NotificationFilter) ([]Notification, error) {
	q := notificationSelect + ` WHERE user_id=?`
	args := []any{f.UserID}
	switch f.Scope {
	case "unread":
		q += ` AND read_at IS NULL`
	case "mentions":
		q += ` AND kind='mention'`
	case "reminders":
		q += ` AND kind='reminder'`
	}
	// CURRENT_TIMESTAMP is second-resolution, so a burst written in one request
	// shares a timestamp; id is the tie-break that keeps the order stable.
	q += ` ORDER BY created_at DESC, id DESC`
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	q += ` LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Event, &n.Kind, &n.RequestID, &n.Title, &n.Body, &n.Href, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) NotificationCounts(ctx context.Context, userID int64) (NotificationCounts, error) {
	var c NotificationCounts
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN read_at IS NULL THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN kind='mention' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN kind='reminder' THEN 1 ELSE 0 END),0)
		FROM notifications WHERE user_id=?`, userID).
		Scan(&c.All, &c.Unread, &c.Mentions, &c.Reminders)
	return c, err
}

func (s *Store) UnreadNotificationCount(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id=? AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

// MarkNotificationRead is scoped by user_id, so another user's row matches
// nothing and returns ErrNotFound rather than being marked by a guessed id.
func (s *Store) MarkNotificationRead(ctx context.Context, userID, id int64, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE notifications SET read_at=? WHERE id=? AND user_id=? AND read_at IS NULL`,
		now.UTC(), id, userID)
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
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE id=? AND user_id=?`, id, userID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func (s *Store) MarkAllNotificationsRead(ctx context.Context, userID int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notifications SET read_at=? WHERE user_id=? AND read_at IS NULL`, now.UTC(), userID)
	return classify(err)
}
