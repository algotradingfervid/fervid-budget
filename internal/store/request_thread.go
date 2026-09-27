// request_thread.go builds the "History and conversation" panel of a request:
// audit events, comments and uploads merged in time order, with the fields an
// edit changed and people's names in place of user ids.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// initials renders "Arun Mehta" as "AM" for the avatar dot in the history.
func initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "?"
	}
	out := strings.ToUpper(parts[0][:1])
	if len(parts) > 1 {
		out += strings.ToUpper(parts[len(parts)-1][:1])
	}
	return out
}

// threadDiffFields are the fields an edit reports as changes in the history:
// col is the column name shown to the reader (ThreadChange.Field) and key the
// Request field name under which the audit JSON stores it. Other fields change
// too rarely, or too noisily, to be worth a line.
var threadDiffFields = []struct{ col, key string }{
	{"amount", "Amount"}, {"approved_amount", "ApprovedAmount"}, {"needed_by", "NeededBy"},
	{"invoice_no", "InvoiceNo"}, {"invoice_date", "InvoiceDate"}, {"expense_date", "ExpenseDate"},
	{"manager_id", "ManagerID"}, {"short_title", "ShortTitle"}, {"purpose", "Purpose"}, {"urgent", "Urgent"},
}

// diffRequestAudit compares the before and after JSON of one audit row and
// returns the threadDiffFields that changed. It returns nil when either side is
// not a JSON object (for example an event that recorded no snapshot).
func diffRequestAudit(beforeJSON, afterJSON string) []ThreadChange {
	var before, after map[string]any
	if json.Unmarshal([]byte(beforeJSON), &before) != nil || json.Unmarshal([]byte(afterJSON), &after) != nil {
		return nil
	}
	var out []ThreadChange
	for _, f := range threadDiffFields {
		was, now := fmt.Sprint(before[f.key]), fmt.Sprint(after[f.key])
		if before[f.key] == nil && after[f.key] == nil {
			continue
		}
		if was != now {
			out = append(out, ThreadChange{Field: f.col, Was: was, Now: now})
		}
	}
	return out
}

// nameUsersInChanges replaces the user ids in a manager_id change with the
// people's names, so the history reads "Asha → Ravi" rather than "3 → 4". A
// value that cannot be resolved (not an id, or an id nobody has) is left as it
// was, so the change is still shown.
func (s *Store) nameUsersInChanges(ctx context.Context, changes []ThreadChange) ([]ThreadChange, error) {
	for i, c := range changes {
		if c.Field != "manager_id" {
			continue
		}
		for _, side := range []*string{&changes[i].Was, &changes[i].Now} {
			// JSON numbers unmarshal as float64, so the id printed as "3" or as
			// "3e+00" depending on its size; ParseFloat reads both.
			id, err := strconv.ParseFloat(*side, 64)
			if err != nil || id <= 0 {
				continue
			}
			var name string
			err = s.db.QueryRowContext(ctx, `SELECT name FROM users WHERE id=?`, int64(id)).Scan(&name)
			if err == sql.ErrNoRows {
				continue
			}
			if err != nil {
				return nil, err
			}
			*side = name
		}
	}
	return changes, nil
}

// RequestAuditNote reads the note recorded by this decision, never the request's
// current decision_reason, which later decisions can overwrite. Other actions
// already include their reason in the audit summary. A direct cancellation uses
// cancel_reason and must not accidentally repeat an earlier approval note.
func RequestAuditNote(a AuditEntry) string {
	if a.EntityType != "payment_request" || (a.Action != "approve" && a.Action != "cancel") {
		return ""
	}
	var after struct{ DecisionReason string }
	if json.Unmarshal([]byte(a.AfterJSON), &after) != nil {
		return ""
	}
	if a.Action == "cancel" {
		// Both cancellation paths share an action. The acceptance writer has
		// always used this summary suffix; a direct cancellation may start from
		// the same status but leaves decision_reason untouched.
		if !strings.HasSuffix(a.Summary, " at the requester's asking") {
			return ""
		}
		var before struct{ Status string }
		if json.Unmarshal([]byte(a.BeforeJSON), &before) != nil || before.Status != "cancellation_requested" {
			return ""
		}
	}
	return strings.TrimSpace(after.DecisionReason)
}

// RequestThread merges a request's audit trail, comments and uploaded
// documents into one list, oldest first: the "History and conversation" panel
// on the request page. Audit rows for comments, uploads and concerns are
// skipped because those appear through their own, richer entries.
func (s *Store) RequestThread(ctx context.Context, requestID int64) ([]ThreadEntry, error) {
	audit, err := s.Audit(ctx, "payment_request", requestID, 200)
	if err != nil {
		return nil, err
	}
	comments, err := s.RequestComments(ctx, requestID)
	if err != nil {
		return nil, err
	}
	atts, err := s.RequestAttachments(ctx, requestID)
	if err != nil {
		return nil, err
	}
	// s.Audit returns newest first, and SQLite's CURRENT_TIMESTAMP resolves only
	// to the second, so a whole burst of events shares one timestamp. Re-sort the
	// trail by (created_at, id) ascending before merging: the audit id is the only
	// strictly monotonic record of what happened first, and leaving the order to
	// SQLite's tie-breaking would make the first line of the story arbitrary.
	sort.SliceStable(audit, func(i, j int) bool {
		if !audit[i].CreatedAt.Equal(audit[j].CreatedAt) {
			return audit[i].CreatedAt.Before(audit[j].CreatedAt)
		}
		return audit[i].ID < audit[j].ID
	})
	out := make([]ThreadEntry, 0, len(audit)+len(comments)+len(atts))
	for _, a := range audit {
		// The comment and attach events are rendered by their own richer
		// entries below; keeping both would double every line. A concern is the
		// same case wearing a different verb: RaiseConcern writes a comment row
		// and an audit row whose summary is that comment, so the manager's words
		// would otherwise appear twice in one stream.
		if a.Action == "comment" || a.Action == "attach" || a.Action == "concern" {
			continue
		}
		var actorID int64
		if a.ActorID != nil {
			actorID = *a.ActorID
		}
		changes, err := s.nameUsersInChanges(ctx, diffRequestAudit(a.BeforeJSON, a.AfterJSON))
		if err != nil {
			return nil, err
		}
		out = append(out, ThreadEntry{Kind: "event", Action: a.Action, ActorID: actorID,
			ActorName: a.ActorName, Initials: initials(a.ActorName), Title: a.Summary,
			Body: RequestAuditNote(a), Changes: changes, CreatedAt: a.CreatedAt})
	}
	for _, c := range comments {
		out = append(out, ThreadEntry{Kind: "comment", ActorID: c.AuthorID, ActorName: c.AuthorName,
			Initials: initials(c.AuthorName), Body: c.Body, CreatedAt: c.CreatedAt})
	}
	for _, a := range atts {
		out = append(out, ThreadEntry{Kind: "attachment", ActorID: a.UploadedBy, Title: "Attachment added",
			FileName: a.OriginalName, FileSize: a.SizeBytes, CreatedAt: a.CreatedAt})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
