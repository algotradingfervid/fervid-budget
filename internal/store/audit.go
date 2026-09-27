package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// execer is what *sql.DB and *sql.Tx have in common for writing, so the audit
// writer works both inside a caller's transaction and on its own.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// writeAudit appends one row to the audit log. Before and After are stored as
// JSON; a nil value is stored as SQL NULL rather than the text "null".
func writeAudit(ctx context.Context, db execer, in AuditInput) error {
	before, _ := json.Marshal(in.Before)
	after, _ := json.Marshal(in.After)
	_, err := db.ExecContext(ctx, `INSERT INTO audit_log(actor_id,actor_name,action,entity_type,entity_id,summary,before_json,after_json,ip) VALUES(?,?,?,?,?,?,?,?,?)`,
		in.ActorID, in.ActorName, in.Action, in.EntityType, in.EntityID, in.Summary, nullJSON(before), nullJSON(after), in.IP)
	return err
}

// recordAuditTx writes an audit row inside tx, so it commits or rolls back with
// the change it describes. Every store writer uses this.
func recordAuditTx(ctx context.Context, tx *sql.Tx, in AuditInput) error {
	return writeAudit(ctx, tx, in)
}

// RecordAudit writes an audit row on its own, for callers outside the store
// whose change is not part of a store transaction.
func (s *Store) RecordAudit(ctx context.Context, in AuditInput) error {
	return writeAudit(ctx, s.db, in)
}

// auditBy starts an audit entry made by actor. The actor id is always recorded,
// even when it is 0: audit_log.actor_id references users, so a write without a
// real actor fails instead of silently recording nobody. entityID may be nil
// for entries that name no single row.
func auditBy(actor User, action, entityType string, entityID *int64, summary string) AuditInput {
	return AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: action, EntityType: entityType, EntityID: entityID, Summary: summary}
}

// withChange attaches the before and after snapshots to an audit entry.
func (in AuditInput) withChange(before, after any) AuditInput {
	in.Before, in.After = before, after
	return in
}

func (s *Store) Audit(ctx context.Context, entityType string, entityID int64, limit int) ([]AuditEntry, error) {
	q := `SELECT id,actor_id,COALESCE(actor_name,''),action,COALESCE(entity_type,''),entity_id,COALESCE(summary,''),COALESCE(before_json,''),COALESCE(after_json,''),COALESCE(ip,''),created_at FROM audit_log`
	var args []any
	if entityType != "" {
		q += ` WHERE entity_type=?`
		args = append(args, entityType)
		if entityID > 0 {
			q += ` AND entity_id=?`
			args = append(args, entityID)
		}
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.ID, &a.ActorID, &a.ActorName, &a.Action, &a.EntityType, &a.EntityID, &a.Summary, &a.BeforeJSON, &a.AfterJSON, &a.IP, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AuditQuery is every /audit filter, all applied in SQL over the whole log.
// From is inclusive and To exclusive; the zero time leaves that side open.
// Limit 0 returns every matching row.
type AuditQuery struct {
	EntityType string
	EntityID   int64
	Action     string
	// Actor matches a case-insensitive substring of actor_name.
	Actor  string
	From   time.Time
	To     time.Time
	Limit  int
	Offset int
}

// AuditPage is a window onto the matching audit rows with the total the same
// filter matches, so the screen can say what it is not showing.
type AuditPage struct {
	Entries   []AuditEntry
	Total     int
	Offset    int
	Limit     int
	Truncated bool
}

// AuditPage replaces reading the newest N rows and filtering them in Go: that
// window hid every older row from the action and actor filters (audit-1).
func (s *Store) AuditPage(ctx context.Context, q AuditQuery) (AuditPage, error) {
	where := []string{"1=1"}
	var args []any
	if q.EntityType != "" {
		where = append(where, "entity_type=?")
		args = append(args, q.EntityType)
		if q.EntityID > 0 {
			where = append(where, "entity_id=?")
			args = append(args, q.EntityID)
		}
	}
	if q.Action != "" {
		where = append(where, "action=?")
		args = append(args, q.Action)
	}
	if actor := strings.ToLower(strings.TrimSpace(q.Actor)); actor != "" {
		where = append(where, "instr(lower(COALESCE(actor_name,'')),?)>0")
		args = append(args, actor)
	}
	// created_at is CURRENT_TIMESTAMP, i.e. UTC text; datetime() normalises it
	// so the comparison does not depend on how a row's timestamp was spelled.
	if !q.From.IsZero() {
		where = append(where, "datetime(created_at)>=datetime(?)")
		args = append(args, q.From.UTC().Format("2006-01-02 15:04:05"))
	}
	if !q.To.IsZero() {
		where = append(where, "datetime(created_at)<datetime(?)")
		args = append(args, q.To.UTC().Format("2006-01-02 15:04:05"))
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE `+cond, args...).Scan(&total); err != nil {
		return AuditPage{}, err
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	query := `SELECT id,actor_id,COALESCE(actor_name,''),action,COALESCE(entity_type,''),entity_id,COALESCE(summary,''),COALESCE(before_json,''),COALESCE(after_json,''),COALESCE(ip,''),created_at FROM audit_log WHERE ` +
		cond + ` ORDER BY created_at DESC, id DESC`
	rowArgs := append([]any{}, args...)
	if q.Limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		rowArgs = append(rowArgs, q.Limit, q.Offset)
	} else {
		q.Offset = 0
	}
	rows, err := s.db.QueryContext(ctx, query, rowArgs...)
	if err != nil {
		return AuditPage{}, err
	}
	defer rows.Close()
	page := AuditPage{Total: total, Offset: q.Offset, Limit: q.Limit}
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.ID, &a.ActorID, &a.ActorName, &a.Action, &a.EntityType, &a.EntityID, &a.Summary, &a.BeforeJSON, &a.AfterJSON, &a.IP, &a.CreatedAt); err != nil {
			return AuditPage{}, err
		}
		page.Entries = append(page.Entries, a)
	}
	page.Truncated = page.Offset+len(page.Entries) < total
	return page, rows.Err()
}
