package store

import (
	"context"
	"database/sql"
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
//
// Of the nine events migration v9 added (F-F-06), exactly one is a mention:
// payment_partial_concern carries the approver's written words to the
// accountant who recorded the shortfall and waits on their reply, which is the
// same shape as request_returned. The other eight report something that has
// already been settled — a withdrawal, a release, a hold lifted, a decision
// taken — and ask nothing of the reader, so they are activity.
func notificationKind(event string) string {
	switch event {
	case "reminder_pending", "reminder_stale_reservation":
		return "reminder"
	// request_cancelled sits with rejected rather than with the activity feed:
	// both end the request against the requester's wishes, and neither leaves
	// them anything to do — it is the finality that earns the stronger filter,
	// not an outstanding action.
	case "request_returned", "request_on_hold", "request_rejected", "payment_partial_concern", "request_cancelled":
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

// defaultNotificationLimit is the cap applied when a filter names no number.
const defaultNotificationLimit = 100

// NotificationPage is a window onto the centre that knows what it is not
// showing. It is deliberately the same shape as RequestPage — Total and
// Truncated, meaning the same things — so the app layer meets one idiom for
// "this list is capped" rather than two (F-G-037, F-B-16).
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

// notificationScopeWhere is the single translation of a filter scope into SQL,
// shared by the rows query and the count so the two can never select different
// sets — which is the whole defect this page type exists to close.
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

// ListNotificationsPage returns the notifications a filter matches together with
// how many it matched in total.
//
// F-G-037: the centre listed at most 100 rows while NotificationCounts, rendered
// on the filter strip immediately above them, counted with no cap at all — so a
// user past the cap read "All 137" over 100 rows and nothing said which 37 were
// missing, or that any were. That is not hypothetical: an area-D test passes
// alone and fails in a full run purely because the shared admin has crossed the
// cap by the time it looks. Silence was the defect; the cap is fine.
func (s *Store) ListNotificationsPage(ctx context.Context, f NotificationFilter) (NotificationPage, error) {
	where := ` WHERE user_id=?` + notificationVisibility + notificationScopeWhere(f.Scope)

	var page NotificationPage
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications`+where, f.UserID).Scan(&page.Total); err != nil {
		return NotificationPage{}, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = defaultNotificationLimit
	}
	page.Limit = limit

	// CURRENT_TIMESTAMP is second-resolution, so a burst written in one request
	// shares a timestamp; id is the tie-break that keeps the order stable.
	rows, err := s.db.QueryContext(ctx, notificationSelect+where+` ORDER BY created_at DESC, id DESC LIMIT ?`, f.UserID, limit)
	if err != nil {
		return NotificationPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Event, &n.Kind, &n.RequestID, &n.Title, &n.Body, &n.Href, &n.ReadAt, &n.CreatedAt); err != nil {
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
	var n Notification
	err := s.db.QueryRowContext(ctx, notificationSelect+` WHERE id=? AND user_id=?`+notificationVisibility, id, userID).
		Scan(&n.ID, &n.UserID, &n.Event, &n.Kind, &n.RequestID, &n.Title, &n.Body, &n.Href, &n.ReadAt, &n.CreatedAt)
	if err == sql.ErrNoRows {
		return n, ErrNotFound
	}
	return n, err
}

func (s *Store) NotificationCounts(ctx context.Context, userID int64) (NotificationCounts, error) {
	var c NotificationCounts
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN read_at IS NULL THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN kind='mention' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN kind='reminder' THEN 1 ELSE 0 END),0)
		FROM notifications WHERE user_id=?`+notificationVisibility, userID).
		Scan(&c.All, &c.Unread, &c.Mentions, &c.Reminders)
	return c, err
}

func (s *Store) UnreadNotificationCount(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id=? AND read_at IS NULL`+notificationVisibility, userID).Scan(&n)
	return n, err
}

// MarkNotificationRead is scoped by user_id, so another user's row matches
// nothing and returns ErrNotFound rather than being marked by a guessed id.
func (s *Store) MarkNotificationRead(ctx context.Context, userID, id int64, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE notifications SET read_at=? WHERE id=? AND user_id=? AND read_at IS NULL`+notificationVisibility,
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
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE id=? AND user_id=?`+notificationVisibility, id, userID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func (s *Store) MarkAllNotificationsRead(ctx context.Context, userID int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notifications SET read_at=? WHERE user_id=? AND read_at IS NULL`+notificationVisibility, now.UTC(), userID)
	return classify(err)
}

// Re-evaluate current membership for old notifications after reassignment or revocation.
const notificationVisibility = ` AND (request_id IS NULL OR EXISTS (
 SELECT 1 FROM payment_requests nr JOIN users nu ON nu.id=notifications.user_id AND nu.active=1
 WHERE nr.id=notifications.request_id
 AND EXISTS (SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id=ur.role_id WHERE ur.user_id=nu.id AND rp.resource='request' AND rp.action='view')
 AND EXISTS (SELECT 1 FROM user_roles ur JOIN role_data_scope ds ON ds.role_id=ur.role_id WHERE ur.user_id=nu.id AND ds.resource='request' AND
 (ds.scope='all' OR (ds.scope='own' AND nr.requester_id=nu.id) OR (ds.scope='assigned' AND (nr.requester_id=nu.id OR nr.manager_id=nu.id))))))`
