// catalog.go maintains projects and their budget heads, and lists the open
// requests that deactivating a head or an approver would strand.

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// UpsertProject creates a project (id 0) or updates one, with an audit row.
// actors is optional: seeding passes none and the audit row then has no actor.
// It returns the project id.
func (s *Store) UpsertProject(ctx context.Context, id int64, name string, active bool, sortOrder int, actors ...User) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("%w: project name is required", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var before map[string]any
	if id != 0 {
		var raw string
		err = tx.QueryRowContext(ctx, `SELECT json_object('name',name,'active',active,'sort_order',sort_order) FROM projects WHERE id=?`, id).Scan(&raw)
		if err != nil {
			return 0, classify(err)
		}
		if err = json.Unmarshal([]byte(raw), &before); err != nil {
			return 0, err
		}
	}
	action := "update"
	if id == 0 {
		res, e := tx.ExecContext(ctx, `INSERT INTO projects(name,active,sort_order) VALUES(?,?,?)`, name, boolInt(active), sortOrder)
		if e != nil {
			return 0, classify(e)
		}
		id, e = res.LastInsertId()
		if e != nil {
			return 0, e
		}
		action = "create"
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE projects SET name=?,active=?,sort_order=? WHERE id=?`, name, boolInt(active), sortOrder, id); err != nil {
			return 0, classify(err)
		}
	}
	actor := User{}
	if len(actors) > 0 {
		actor = actors[0]
	}
	var actorID *int64
	if actor.ID != 0 {
		actorID = &actor.ID
	}
	after := map[string]any{"name": name, "active": boolInt(active), "sort_order": sortOrder}
	if err = recordAuditTx(ctx, tx, AuditInput{ActorID: actorID, ActorName: actor.Name, Action: action, EntityType: "project", EntityID: &id, Summary: auditVerb(action) + " project " + name, Before: before, After: after}); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// ListProjects returns projects in display order, optionally only active ones.
func (s *Store) ListProjects(ctx context.Context, activeOnly bool) ([]Project, error) {
	q := `SELECT id,name,active,sort_order,created_at FROM projects`
	if activeOnly {
		q += ` WHERE active=1`
	}
	q += ` ORDER BY sort_order,name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		var active int
		if err := rows.Scan(&p.ID, &p.Name, &active, &p.SortOrder, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Active = active == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertHead creates a head (id 0) or updates one, with an audit row. A head
// that already has budgets, payments or requests cannot move to another
// project. actors is optional, as for UpsertProject. It returns the head id.
func (s *Store) UpsertHead(ctx context.Context, id, projectID int64, name, dueDay string, active bool, sortOrder int, actors ...User) (int64, error) {
	name = strings.TrimSpace(name)
	dueDay = strings.TrimSpace(dueDay)
	if name == "" || projectID == 0 {
		return 0, fmt.Errorf("%w: project and head name are required", ErrValidation)
	}
	if !validDueDay(dueDay) {
		return 0, fmt.Errorf("%w: due day must be a day from 1 to 31", ErrValidation)
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var before map[string]any
	if id != 0 {
		var raw string
		err = tx.QueryRowContext(ctx, `SELECT json_object('project_id',project_id,'name',name,'due_day',due_day,'active',active,'sort_order',sort_order) FROM heads WHERE id=?`, id).Scan(&raw)
		if err != nil {
			return 0, classify(err)
		}
		if err = json.Unmarshal([]byte(raw), &before); err != nil {
			return 0, err
		}
	}
	if id != 0 {
		var oldParent int64
		if err = tx.QueryRowContext(ctx, `SELECT project_id FROM heads WHERE id=?`, id).Scan(&oldParent); err != nil {
			return 0, err
		}
		if oldParent != projectID {
			var used bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM budgets WHERE head_id=? UNION ALL SELECT 1 FROM payments WHERE head_id=? UNION ALL SELECT 1 FROM payment_requests WHERE head_id=?)`, id, id, id).Scan(&used)
			if err != nil {
				return 0, err
			}
			if used {
				return 0, fmt.Errorf("%w: a head with financial history cannot move between projects; create a new head", ErrValidation)
			}
		}
	}
	action := "update"
	if id == 0 {
		res, e := tx.ExecContext(ctx, `INSERT INTO heads(project_id,name,due_day,active,sort_order) VALUES(?,?,?,?,?)`, projectID, name, dueDay, boolInt(active), sortOrder)
		if e != nil {
			return 0, classify(e)
		}
		id, e = res.LastInsertId()
		if e != nil {
			return 0, e
		}
		action = "create"
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE heads SET project_id=?,name=?,due_day=?,active=?,sort_order=? WHERE id=?`, projectID, name, dueDay, boolInt(active), sortOrder, id); err != nil {
			return 0, classify(err)
		}
	}
	actor := User{}
	if len(actors) > 0 {
		actor = actors[0]
	}
	var actorID *int64
	if actor.ID != 0 {
		actorID = &actor.ID
	}
	after := map[string]any{"project_id": projectID, "name": name, "due_day": dueDay, "active": boolInt(active), "sort_order": sortOrder}
	if err = recordAuditTx(ctx, tx, AuditInput{ActorID: actorID, ActorName: actor.Name, Action: action, EntityType: "head", EntityID: &id, Summary: auditVerb(action) + " head " + name, Before: before, After: after}); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// OpenRequest is one unfinished request, named on a deactivation warning.
type OpenRequest struct {
	ID     int64
	Number string
	Title  string
	Status string
	Amount int64
}

// headOpenStatuses are the states in which a request still needs its head to be
// active: every state that can still end in a payment.
const headOpenStatuses = `'pending','returned','approved','cancellation_requested','processing','partial_review'`

// approverOpenStatuses are the states in which a request waits on its approver,
// or will again once its requester resubmits it.
const approverOpenStatuses = `'pending','returned','cancellation_requested','partial_review'`

// HeadIsActive reports whether a head is currently active.
func (s *Store) HeadIsActive(ctx context.Context, id int64) (bool, error) {
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT active FROM heads WHERE id=?`, id).Scan(&active)
	return active == 1, classify(err)
}

// OpenRequestsForHead lists the requests that deactivating the head would leave
// unpayable, because a payment requires an active head.
func (s *Store) OpenRequestsForHead(ctx context.Context, headID int64) ([]OpenRequest, error) {
	return s.openRequests(ctx, `head_id=? AND status IN (`+headOpenStatuses+`)`, headID)
}

// RequestsAwaitingApprover lists the requests that deactivating the user would
// leave with nobody able to decide them.
func (s *Store) RequestsAwaitingApprover(ctx context.Context, userID int64) ([]OpenRequest, error) {
	return s.openRequests(ctx, `manager_id=? AND status IN (`+approverOpenStatuses+`)`, userID)
}

// openRequests lists requests matching where (one ? for arg), oldest first.
func (s *Store) openRequests(ctx context.Context, where string, arg int64) ([]OpenRequest, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, number, COALESCE(NULLIF(short_title,''), purpose), status, COALESCE(approved_amount, amount)
		FROM payment_requests WHERE `+where+` ORDER BY id`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OpenRequest
	for rows.Next() {
		var o OpenRequest
		if err := rows.Scan(&o.ID, &o.Number, &o.Title, &o.Status, &o.Amount); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ListHeads returns heads with their project names in display order;
// activeOnly keeps heads that are active in an active project.
func (s *Store) ListHeads(ctx context.Context, activeOnly bool) ([]Head, error) {
	q := `SELECT h.id,h.project_id,p.name,h.name,COALESCE(h.due_day,''),h.active,h.sort_order,h.created_at
	      FROM heads h JOIN projects p ON p.id=h.project_id`
	if activeOnly {
		q += ` WHERE h.active=1 AND p.active=1`
	}
	q += ` ORDER BY p.sort_order,p.name,h.sort_order,h.name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Head
	for rows.Next() {
		var h Head
		var active int
		if err := rows.Scan(&h.ID, &h.ProjectID, &h.Project, &h.Name, &h.DueDay, &active, &h.SortOrder, &h.CreatedAt); err != nil {
			return nil, err
		}
		h.Active = active == 1
		out = append(out, h)
	}
	return out, rows.Err()
}

// validDueDay reports whether s is empty or a day of the month from 1 to 31.
func validDueDay(s string) bool {
	if s == "" {
		return true
	}
	day, err := strconv.Atoi(s)
	return err == nil && day >= 1 && day <= 31
}

// auditVerb is the word an audit summary opens with for a create or update
// action, so the log reads "Created project X" for a new record and "Updated
// project X" for a changed one.
func auditVerb(action string) string {
	if action == "create" {
		return "Created"
	}
	return "Updated"
}
