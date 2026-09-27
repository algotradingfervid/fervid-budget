package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"fervidbudget/internal/money"

	_ "modernc.org/sqlite"
)

type Store struct {
	db            *sql.DB
	badgeRevision atomic.Uint64
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	// The pragmas ride on the DSN, not on a db.Exec: PRAGMA state is
	// per-connection, and database/sql opens more connections on demand, so a
	// single `db.Exec("PRAGMA …")` configures only whichever connection happened
	// to serve it. Every later connection would then run with foreign keys off
	// and no busy handler — the second is what makes concurrent reservations
	// (ReserveRequest) fail with SQLITE_BUSY instead of waiting their turn.
	// modernc.org/sqlite applies every _pragma query parameter to each new
	// connection; without a "file:" prefix it strips the query before opening,
	// so a path containing spaces still opens correctly.
	// journal_mode(WAL) is the third pragma for a reason of its own. In the
	// default rollback-journal mode a COMMIT needs EXCLUSIVE, so it waits on
	// every reader — and when it gives up with SQLITE_BUSY, SQLite leaves that
	// transaction open. database/sql has already marked the Tx done by then, so
	// `defer tx.Rollback()` returns ErrTxDone without rolling anything back, and
	// the connection returns to the pool still holding the write lock: every
	// later writer then failed with SQLITE_BUSY and every later BEGIN on that
	// connection with "cannot start a transaction within a transaction", until
	// the process was restarted. Two people recording payments at the same time
	// was enough. Under WAL, readers and the writer no longer exclude each other,
	// so the COMMIT that sprang the trap does not happen.
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// Defence in depth for the same trap: should a connection ever be poisoned
	// this way again, retiring it bounds the damage to this lifetime rather than
	// until the next restart. Closing the connection rolls its transaction back.
	db.SetConnMaxLifetime(5 * time.Minute)
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

// RoleIDsIncludeAdmin reports whether any of these role ids is a system role
// named Admin. It exists so the superseded users.role column can be kept in step
// with the roles that actually grant permissions, rather than contradicting them.
func (s *Store) RoleIDsIncludeAdmin(ctx context.Context, roleIDs []int64) bool {
	for _, id := range roleIDs {
		var name string
		err := s.db.QueryRowContext(ctx, `SELECT name FROM roles WHERE id=?`, id).Scan(&name)
		if err == nil && strings.EqualFold(name, "Admin") {
			return true
		}
	}
	return false
}

// CreateUser creates a user and lets the legacy role string decide their first
// role, as it always has. Seeding and the tests use this shape.
func (s *Store) CreateUser(ctx context.Context, email, name, hash, role string, active bool) (int64, error) {
	return s.CreateUserWithRoles(ctx, email, name, hash, role, active, nil)
}

// CreateUserWithRoles creates a user and gives them exactly the roles named.
//
// The people screen used to offer a two-value "Data entry or Admin" dropdown
// taken from the superseded users.role column, and assignDefaultRoleTx turned
// anything that was not "admin" into the *Accounts* role — which carries
// payment:settle and payment:void. So an administrator adding a colleague could
// not create a requester or an approver at all, and the innocuous-sounding
// option silently granted the ability to move money. The screen now asks for
// real roles and passes them here.
//
// With no roles named the legacy derivation still applies, because seeding and
// the tests create users that way and their first role has to come from
// somewhere.
func (s *Store) CreateUserWithRoles(ctx context.Context, email, name, hash, role string, active bool, roleIDs []int64) (int64, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if err := validateUserFields(email, name, role); err != nil {
		return 0, err
	}
	if role == "" {
		role = "data_entry"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO users(email,name,password_hash,role,active) VALUES(?,?,?,?,?)`,
		email, name, hash, role, boolInt(active))
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	wanted := make([]int64, 0, len(roleIDs))
	seen := map[int64]struct{}{}
	for _, rid := range roleIDs {
		if rid == 0 {
			continue
		}
		if _, dup := seen[rid]; dup {
			continue
		}
		seen[rid] = struct{}{}
		wanted = append(wanted, rid)
	}
	if len(wanted) == 0 {
		if err := assignDefaultRoleTx(ctx, tx, id, role); err != nil {
			return 0, err
		}
	} else {
		for _, rid := range wanted {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO user_roles(user_id,role_id) VALUES(?,?) ON CONFLICT(user_id,role_id) DO NOTHING`,
				id, rid); err != nil {
				return 0, classify(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) EnsureUser(ctx context.Context, email, name, hash, role string) error {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE email=?`, strings.ToLower(email)).Scan(&id)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = s.CreateUser(ctx, email, name, hash, role, true)
	return err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,email,name,password_hash,role,active,created_at,updated_at,default_approver_id,session_version FROM users WHERE email=?`, strings.ToLower(strings.TrimSpace(email)))
	return scanUser(row)
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,email,name,password_hash,role,active,created_at,updated_at,default_approver_id,session_version FROM users WHERE id=?`, id)
	return scanUser(row)
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,email,name,password_hash,role,active,created_at,updated_at,default_approver_id,session_version FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateUser rewrites a user's profile — name, legacy role, active flag and
// optionally the password — and touches nothing else.
//
// It is NOT the Users screen's path and must not become one again. F-G-034
// replaced that with SaveUser, which applies the profile, the role assignment
// and the default approver in a single transaction, because a save refused by
// the second of three separate calls used to leave the first one's rename
// committed. Any new writer that saves the edit sheet uses SaveUser.
//
// It is kept, rather than deleted with its callers rewritten, because SaveUser
// is not a drop-in replacement for a narrow write: UserSaveInput.RoleIDs is
// "the user's roles now", so a nil slice CLEARS the assignment. A caller that
// only wants to deactivate somebody would have to read their roles back and
// echo them, and getting that wrong is a silent permission change. That is what
// this function is for, and the callers that want exactly it today are the
// fixtures in internal/auth's session tests (deactivating a signed-in user) and
// store_test.go's field-validation pin.
//
// Its one weakness, recorded rather than hidden: the last-active-administrator
// guard reads on s.db and then writes on s.db, with no transaction around the
// pair. SaveUser re-asks the same question inside its own transaction for
// exactly that reason. This is not reachable from any route today; it becomes a
// real race the moment one calls it.
func (s *Store) UpdateUser(ctx context.Context, id int64, name, role string, active bool, passwordHash string) error {
	name = strings.TrimSpace(name)
	if err := validateUserFields("placeholder@example.invalid", name, role); err != nil {
		return err
	}
	if err := s.RequireAnotherActiveAdmin(ctx, id, role, active); err != nil {
		return err
	}
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if passwordHash != "" || !active {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET session_version=session_version+1 WHERE id=?`, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id=?`, id); err != nil {
			return err
		}
	}
	if passwordHash != "" {
		_, err = tx.ExecContext(ctx, `UPDATE users SET name=?, role=?, active=?, password_hash=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
			name, role, boolInt(active), passwordHash, id)
		if err != nil {
			return classify(err)
		}
		return tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `UPDATE users SET name=?, role=?, active=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		name, role, boolInt(active), id)
	if err != nil {
		return classify(err)
	}
	return tx.Commit()
}

// UserSaveInput is one submission of the Users screen's edit sheet: the profile,
// the role assignment and the default approver together. They arrive together
// because they are saved together — see SaveUser.
type UserSaveInput struct {
	ID     int64
	Name   string
	Role   string
	Active bool
	// PasswordHash is empty when the sheet was submitted without a new password.
	PasswordHash string
	// RoleIDs replaces the user's entire role assignment. A nil slice clears it,
	// which is what unticking every box means.
	RoleIDs []int64
	// DefaultApproverID is 0 to clear the field.
	DefaultApproverID int64
}

// SaveUser applies a whole user save in one transaction.
//
// It exists because the three writes behind one submit used to be three store
// calls with three transactions, so a save refused by the second left the first
// one's rename committed — and the audit row, written last of all by the
// handler, described neither state (F-G-034). R4 makes role assignment the thing
// that governs access, so a half-applied user save is a half-applied access
// change.
//
// Every rule the three old calls enforced is enforced here, before any write:
// the profile fields, the last-active-administrator guard, that every role id
// exists, and that nobody is their own default approver.
func (s *Store) SaveUser(ctx context.Context, actor User, in UserSaveInput) error {
	name := strings.TrimSpace(in.Name)
	if err := validateUserFields("placeholder@example.invalid", name, in.Role); err != nil {
		return err
	}
	if in.DefaultApproverID != 0 && in.DefaultApproverID == in.ID {
		return fmt.Errorf("%w: a user cannot be their own default approver", ErrValidation)
	}
	roleIDs := make([]int64, 0, len(in.RoleIDs))
	seen := map[int64]struct{}{}
	for _, id := range in.RoleIDs {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		roleIDs = append(roleIDs, id)
	}

	// beginWriteTx takes the write lock with the transaction's first statement,
	// which is what keeps a read-then-write like this one out of the SQLITE_BUSY
	// races the audit closed elsewhere.
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// The last-active-administrator guard, re-asked inside the transaction. The
	// exported RequireAnotherActiveAdmin reads through s.db, and a read on a
	// second connection while this transaction holds the write lock is exactly
	// the shape that deadlocks under SQLite.
	var currentRole, currentName string
	var currentActive int
	if err := tx.QueryRowContext(ctx, `SELECT role,active,name FROM users WHERE id=?`, in.ID).
		Scan(&currentRole, &currentActive, &currentName); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if currentRole == "admin" && currentActive == 1 && !(in.Role == "admin" && in.Active) {
		var others int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='admin' AND active=1 AND id<>?`, in.ID).Scan(&others); err != nil {
			return err
		}
		if others == 0 {
			return fmt.Errorf("%w: at least one active administrator is required", ErrValidation)
		}
	}
	for _, id := range roleIDs {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM roles WHERE id=?`, id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("%w: role %d does not exist", ErrValidation, id)
		}
	}
	var approver any
	if err := requireDefaultApproverTx(ctx, tx, in.ID, in.DefaultApproverID); err != nil {
		return err
	}
	if in.DefaultApproverID != 0 {
		approver = in.DefaultApproverID
	}

	if in.PasswordHash != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET name=?, role=?, active=?, password_hash=?, default_approver_id=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
			name, in.Role, boolInt(in.Active), in.PasswordHash, approver, in.ID); err != nil {
			return classify(err)
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE users SET name=?, role=?, active=?, default_approver_id=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		name, in.Role, boolInt(in.Active), approver, in.ID); err != nil {
		return classify(err)
	}
	// Administrator password changes and deactivation supersede email reset
	// links already issued. Re-activation must not resurrect an old credential.
	if in.PasswordHash != "" || !in.Active {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET session_version=session_version+1 WHERE id=?`, in.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM password_resets WHERE user_id=?`, in.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id=?`, in.ID); err != nil {
		return err
	}
	for _, id := range roleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id) VALUES(?,?)`, in.ID, id); err != nil {
			return classify(err)
		}
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name,
		Action: "update", EntityType: "user", EntityID: &in.ID,
		Summary: "Updated user " + name,
		Before:  map[string]any{"name": currentName, "role": currentRole, "active": currentActive == 1},
		After: map[string]any{"name": name, "role": in.Role, "active": in.Active,
			"role_ids": roleIDs, "default_approver_id": in.DefaultApproverID}}); err != nil {
		return err
	}
	return tx.Commit()
}

// RequireAnotherActiveAdmin prevents disabling or demoting the final active
// administrator. It is deliberately public so callers can surface a useful
// validation message before displaying a confirmation form.
func (s *Store) RequireAnotherActiveAdmin(ctx context.Context, userID int64, nextRole string, nextActive bool) error {
	var currentRole string
	var currentActive int
	if err := s.db.QueryRowContext(ctx, `SELECT role,active FROM users WHERE id=?`, userID).Scan(&currentRole, &currentActive); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if currentRole != "admin" || currentActive != 1 || (nextRole == "admin" && nextActive) {
		return nil
	}
	var others int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='admin' AND active=1 AND id<>?`, userID).Scan(&others); err != nil {
		return err
	}
	if others == 0 {
		return fmt.Errorf("%w: at least one active administrator is required", ErrValidation)
	}
	return nil
}

// LoginLocked reports whether a known account remains temporarily locked.
func (s *Store) LoginLocked(ctx context.Context, email string) (bool, time.Time, error) {
	var locked sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT locked FROM users WHERE email=?`, strings.ToLower(strings.TrimSpace(email))).Scan(&locked)
	if err == sql.ErrNoRows {
		return false, time.Time{}, ErrNotFound
	}
	if err != nil {
		return false, time.Time{}, err
	}
	if !locked.Valid || !locked.Time.After(time.Now()) {
		return false, time.Time{}, nil
	}
	return true, locked.Time, nil
}

// RecordFailedLogin increments a known user's failure counter and locks the
// account for fifteen minutes after five consecutive failures. It intentionally
// returns no signal for unknown email addresses to avoid account enumeration.
func (s *Store) RecordFailedLogin(ctx context.Context, email string) (time.Time, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback()
	var attempts int
	var previousLock sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT attempt_count,locked FROM users WHERE email=?`, email).Scan(&attempts, &previousLock); err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	if previousLock.Valid && !previousLock.Time.After(time.Now()) {
		attempts = 0
	}
	attempts++
	var locked any
	var until time.Time
	if attempts >= 5 {
		until = time.Now().Add(15 * time.Minute).UTC()
		locked = until
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET attempt_count=?, last_attempt=CURRENT_TIMESTAMP, locked=?, updated_at=CURRENT_TIMESTAMP WHERE email=?`, attempts, locked, email); err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return time.Time{}, err
	}
	return until, nil
}

func (s *Store) ResetLoginAttempts(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET attempt_count=0,last_attempt=NULL,locked=NULL,updated_at=CURRENT_TIMESTAMP WHERE id=?`, userID)
	return classify(err)
}

// ResetLoginFailures is the email-oriented companion for login handlers that
// do not retain a user ID after authentication has succeeded.
func (s *Store) ResetLoginFailures(ctx context.Context, email string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET attempt_count=0,last_attempt=NULL,locked=NULL,updated_at=CURRENT_TIMESTAMP WHERE email=?`, strings.ToLower(strings.TrimSpace(email)))
	return classify(err)
}

func (s *Store) UpsertProject(ctx context.Context, id int64, name string, active bool, sortOrder int) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("%w: project name is required", ErrValidation)
	}
	if id == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO projects(name,active,sort_order) VALUES(?,?,?)`, name, boolInt(active), sortOrder)
		if err != nil {
			return 0, classify(err)
		}
		return res.LastInsertId()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE projects SET name=?, active=?, sort_order=? WHERE id=?`, name, boolInt(active), sortOrder, id)
	return id, classify(err)
}

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

func (s *Store) UpsertHead(ctx context.Context, id, projectID int64, name, dueDay string, active bool, sortOrder int) (int64, error) {
	name = strings.TrimSpace(name)
	dueDay = strings.TrimSpace(dueDay)
	if name == "" || projectID == 0 {
		return 0, fmt.Errorf("%w: project and head name are required", ErrValidation)
	}
	if !validDueDay(dueDay) {
		return 0, fmt.Errorf("%w: due day must be a day from 1 to 31", ErrValidation)
	}
	if id == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO heads(project_id,name,due_day,active,sort_order) VALUES(?,?,?,?,?)`,
			projectID, name, dueDay, boolInt(active), sortOrder)
		if err != nil {
			return 0, classify(err)
		}
		return res.LastInsertId()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE heads SET project_id=?, name=?, due_day=?, active=?, sort_order=? WHERE id=?`,
		projectID, name, dueDay, boolInt(active), sortOrder, id)
	return id, classify(err)
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
// active: every state that can still end in a payment (F-G-024).
const headOpenStatuses = `'pending','returned','approved','cancellation_requested','processing','partial_review'`

// approverOpenStatuses are the states in which a request waits on its approver,
// or will again once its requester resubmits it (F-G-025).
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

func (s *Store) SetBudget(ctx context.Context, actor User, headID int64, month string, amount int64) error {
	return s.SetBudgets(ctx, actor, month, []BudgetInput{{HeadID: headID, Amount: amount}})
}

// SetBudgets writes an entire budget form atomically. Validation occurs before
// any write, so a malformed row cannot leave a partially saved monthly plan.
func (s *Store) SetBudgets(ctx context.Context, actor User, month string, inputs []BudgetInput) error {
	if !validMonth(month) || len(inputs) == 0 {
		return fmt.Errorf("%w: valid month and at least one budget are required", ErrValidation)
	}
	seen := make(map[int64]struct{}, len(inputs))
	for _, input := range inputs {
		if input.HeadID <= 0 || input.Amount < 0 {
			return fmt.Errorf("%w: valid head and non-negative amount are required", ErrValidation)
		}
		if _, exists := seen[input.HeadID]; exists {
			return fmt.Errorf("%w: duplicate budget head", ErrValidation)
		}
		seen[input.HeadID] = struct{}{}
	}
	locked, err := isLocked(ctx, s.db, month)
	if err != nil {
		return err
	}
	if locked {
		return ErrLockedMonth
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE budget_months SET month=month WHERE month=?`, month); err != nil {
		return err
	}
	if locked, err := isLocked(ctx, tx, month); err != nil {
		return err
	} else if locked {
		return ErrLockedMonth
	}
	var actorID any
	if actor.ID != 0 {
		actorID = actor.ID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO budget_months(month,status,created_by) VALUES(?,'open',?) ON CONFLICT(month) DO UPDATE SET updated_at=CURRENT_TIMESTAMP`, month, actorID); err != nil {
		return classify(err)
	}
	for _, input := range inputs {
		var before *Budget
		var b Budget
		err := tx.QueryRowContext(ctx, `SELECT id,head_id,month,amount,created_at,updated_at FROM budgets WHERE head_id=? AND month=?`, input.HeadID, month).Scan(&b.ID, &b.HeadID, &b.Month, &b.Amount, &b.CreatedAt, &b.UpdatedAt)
		if err == nil {
			before = &b
		} else if err != sql.ErrNoRows {
			return err
		}
		// The head lookup comes before the skip below, so an unknown head still
		// fails the whole batch even when it arrives at ₹0 with no budget behind
		// it; the label it yields names the head in the audit summary.
		var label string
		if err := tx.QueryRowContext(ctx, `SELECT p.name || ' / ' || h.name FROM heads h JOIN projects p ON p.id=h.project_id WHERE h.id=?`, input.HeadID).Scan(&label); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("%w: unknown budget head", ErrValidation)
			}
			return err
		}
		// Saving the form re-submits every head on it. A budget whose amount did
		// not move is not a mutation: writing it anyway put an "update" row in
		// the audit log for every head on every save, which buried the real
		// changes and pushed history out of the audit view (audit-2).
		// The same holds for a head with no budget submitted at ₹0: an absent
		// budget already reads as ₹0 on every screen (the grid COALESCEs it), so
		// the first save of a month used to log "set to ₹0.00" for every head the
		// user never touched.
		if (before != nil && before.Amount == input.Amount) || (before == nil && input.Amount == 0) {
			continue
		}
		var detailed int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM budget_lines l JOIN budgets b ON b.id=l.budget_id WHERE b.head_id=? AND b.month=?`, input.HeadID, month).Scan(&detailed); err != nil {
			return err
		}
		if detailed > 0 {
			return fmt.Errorf("%w: %s has detailed budget lines; use Edit budget lines to change its amount", ErrValidation, label)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO budgets(head_id,month,amount) VALUES(?,?,?) ON CONFLICT(head_id,month) DO UPDATE SET amount=excluded.amount, updated_at=CURRENT_TIMESTAMP`, input.HeadID, month, input.Amount)
		if err != nil {
			return classify(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		after := Budget{ID: id, HeadID: input.HeadID, Month: month, Amount: input.Amount}
		// The summary names what changed — head, month and both amounts — because
		// it is the only column the audit screen shows without expanding the row.
		action := "create"
		summary := label + " " + month + ": set to " + money.FormatPaise(input.Amount)
		if before != nil {
			action, after.ID = "update", before.ID
			summary = label + " " + month + ": " + money.FormatPaise(before.Amount) + " → " + money.FormatPaise(input.Amount)
		}
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(after)
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log(actor_id,actor_name,action,entity_type,entity_id,summary,before_json,after_json) VALUES(?,?,?,?,?,?,?,?)`, &actor.ID, actor.Name, action, "budget", after.ID, summary, nullJSON(beforeJSON), nullJSON(afterJSON)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) setBudgetLegacy(ctx context.Context, actor User, headID int64, month string, amount int64) error {
	// Kept as a small, explicit implementation reference for older callers.
	var before *Budget
	if b, err := s.Budget(ctx, headID, month); err == nil {
		before = &b
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO budgets(head_id,month,amount) VALUES(?,?,?)
		ON CONFLICT(head_id,month) DO UPDATE SET amount=excluded.amount, updated_at=CURRENT_TIMESTAMP`, headID, month, amount)
	if err != nil {
		return classify(err)
	}
	after, _ := s.Budget(ctx, headID, month)
	action := "create"
	if before != nil {
		action = "update"
	}
	return s.RecordAudit(ctx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: action, EntityType: "budget", EntityID: &after.ID, Summary: "Saved budget " + money.FormatPaise(amount), Before: before, After: after})
}

func (s *Store) Budget(ctx context.Context, headID int64, month string) (Budget, error) {
	var b Budget
	err := s.db.QueryRowContext(ctx, `SELECT id,head_id,month,amount,created_at,updated_at FROM budgets WHERE head_id=? AND month=?`, headID, month).
		Scan(&b.ID, &b.HeadID, &b.Month, &b.Amount, &b.CreatedAt, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return b, ErrNotFound
	}
	return b, err
}

func (s *Store) CreateMonthPlan(ctx context.Context, actor User, targetMonth, sourceMonth string) error {
	targetMonth = strings.TrimSpace(targetMonth)
	sourceMonth = strings.TrimSpace(sourceMonth)
	if !validMonth(targetMonth) {
		return fmt.Errorf("%w: target month is required", ErrValidation)
	}
	if sourceMonth != "" && !validMonth(sourceMonth) {
		return fmt.Errorf("%w: source month is invalid", ErrValidation)
	}
	locked, err := isLocked(ctx, s.db, targetMonth)
	if err != nil {
		return err
	}
	if locked {
		return ErrLockedMonth
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE budget_months SET month=month WHERE month=?`, targetMonth); err != nil {
		return err
	}
	if locked, err := isLocked(ctx, tx, targetMonth); err != nil {
		return err
	} else if locked {
		return ErrLockedMonth
	}
	if sourceMonth != "" {
		var sourceBudgets int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM budgets WHERE month=? AND amount>0`, sourceMonth).Scan(&sourceBudgets); err != nil {
			return err
		}
		if sourceBudgets == 0 {
			return fmt.Errorf("%w: source month has no budgets to copy", ErrValidation)
		}
	}

	var source any
	if sourceMonth != "" {
		source = sourceMonth
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO budget_months(month,status,source_month,created_by) VALUES(?,'open',?,?)
		ON CONFLICT(month) DO UPDATE SET updated_at=CURRENT_TIMESTAMP, source_month=COALESCE(budget_months.source_month, excluded.source_month)`,
		targetMonth, source, actor.ID); err != nil {
		return classify(err)
	}

	var copied int64
	if sourceMonth != "" {
		rows, err := tx.QueryContext(ctx, `INSERT INTO budgets(head_id,month,amount)
		 SELECT h.id, ?, sb.amount FROM heads h JOIN projects p ON p.id=h.project_id
		 JOIN budgets sb ON sb.head_id=h.id AND sb.month=?
		 WHERE h.active=1 AND p.active=1 AND sb.amount>0
		 ON CONFLICT(head_id,month) DO NOTHING RETURNING id`, targetMonth, sourceMonth)
		if err != nil {
			return classify(err)
		}
		var created []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			created = append(created, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		copied = int64(len(created))
		for _, id := range created {
			if _, err := tx.ExecContext(ctx, `INSERT INTO budget_lines(budget_id,description,amount,sort_order)
		 SELECT target.id,l.description,l.amount,l.sort_order FROM budgets target
		 JOIN budgets source ON source.head_id=target.head_id AND source.month=?
		 JOIN budget_lines l ON l.budget_id=source.id WHERE target.id=?`, sourceMonth, id); err != nil {
				return err
			}
		}
	}

	summary := "Created monthly plan " + targetMonth
	if sourceMonth != "" {
		summary = fmt.Sprintf("%s from %s with %d copied budgets", summary, sourceMonth, copied)
	}
	after, err := json.Marshal(map[string]any{"month": targetMonth, "source_month": sourceMonth, "copied_budgets": copied})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log(actor_id,actor_name,action,entity_type,summary,after_json) VALUES(?,?,'create','budget_month',?,?)`, actor.ID, actor.Name, summary, string(after)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListMonthPlans(ctx context.Context) ([]MonthPlan, error) {
	rows, err := s.db.QueryContext(ctx, `WITH months(month) AS (
			SELECT month FROM budget_months
			UNION SELECT month FROM budgets
			UNION SELECT substr(paid_on,1,7) FROM payments
			UNION SELECT month FROM month_locks
		)
		SELECT m.month,COALESCE(bm.status,''),COALESCE(bm.source_month,''),COALESCE(u.name,''),bm.created_at,ml.locked_at,COALESCE(ml.reason,'')
		FROM months m
		LEFT JOIN budget_months bm ON bm.month=m.month
		LEFT JOIN users u ON u.id=bm.created_by
		LEFT JOIN month_locks ml ON ml.month=m.month
		ORDER BY m.month DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MonthPlan
	for rows.Next() {
		var plan MonthPlan
		var createdAt, lockedAt sql.NullTime
		if err := rows.Scan(&plan.Month, &plan.Status, &plan.SourceMonth, &plan.CreatedByName, &createdAt, &lockedAt, &plan.LockReason); err != nil {
			return nil, err
		}
		if plan.Status == "" {
			plan.Status = "open"
		}
		if createdAt.Valid {
			t := createdAt.Time
			plan.CreatedAt = &t
		}
		if lockedAt.Valid {
			t := lockedAt.Time
			plan.Locked = true
			plan.LockedAt = &t
			plan.Status = "locked"
		}
		grid, err := s.Grid(ctx, plan.Month, "", "")
		if err != nil {
			return nil, err
		}
		plan.Budget = grid.Total.Budget
		plan.Actual = grid.Total.Actual
		plan.Variance = grid.Total.Variance
		plan.HeadCount = len(grid.Rows)
		if plan.Budget <= 0 && plan.Actual > 0 {
			plan.UsedPercent = "No budget"
		} else if plan.Budget <= 0 || plan.Actual <= 0 {
			plan.UsedPercent = "0%"
		} else {
			plan.UsedPercent = fmt.Sprintf("%.0f%%", (float64(plan.Actual)/float64(plan.Budget))*100)
		}
		out = append(out, plan)
	}
	return out, rows.Err()
}

func (s *Store) CreatePayment(ctx context.Context, actor User, in PaymentInput) (int64, error) {
	return s.createPayment(ctx, actor, in, nil)
}

// CreatePaymentWithAttachment makes the database payment and attachment rows
// one transaction. The caller remains responsible for removing an uploaded
// file if this method returns an error.
func (s *Store) CreatePaymentWithAttachment(ctx context.Context, actor User, in PaymentInput, attachment *AttachmentInput) (int64, error) {
	return s.createPayment(ctx, actor, in, attachment)
}

func (s *Store) createPayment(ctx context.Context, actor User, in PaymentInput, attachment *AttachmentInput) (int64, error) {
	if err := s.validatePayment(ctx, in, false); err != nil {
		return 0, err
	}
	if attachment != nil {
		if err := validateAttachment(*attachment); err != nil {
			return 0, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO payments(head_id,paid_on,amount,vendor_payee,payment_mode,invoice_no,reference_no,remarks,entered_by) VALUES(?,?,?,?,?,?,?,?,?)`, in.HeadID, in.PaidOn, in.Amount, in.VendorPayee, in.PaymentMode, in.InvoiceNo, in.ReferenceNo, in.Remarks, actor.ID)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	after := paymentFromInput(id, actor, in)
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "create", EntityType: "payment", EntityID: &id, Summary: "Recorded payment " + money.FormatPaise(in.Amount), After: after}); err != nil {
		return 0, err
	}
	if attachment != nil {
		if _, err := addAttachmentTx(ctx, tx, actor, id, *attachment); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) UpdatePayment(ctx context.Context, actor User, id int64, in PaymentInput) error {
	return s.UpdatePaymentWithAttachment(ctx, actor, id, in, nil)
}

// UpdatePaymentWithAttachment updates payment fields, optionally adds a file,
// and records both audit events in one transaction.
func (s *Store) UpdatePaymentWithAttachment(ctx context.Context, actor User, id int64, in PaymentInput, attachment *AttachmentInput) error {
	if err := s.validatePayment(ctx, in, false); err != nil {
		return err
	}
	if attachment != nil {
		if err := validateAttachment(*attachment); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := paymentInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	// S12: a payment that settles a request is the request's outcome. Editing it
	// would silently rewrite what the requester and approver were told was paid.
	if before.RequestID != nil {
		return fmt.Errorf("%w: a payment linked to a request cannot be edited", ErrValidation)
	}
	if before.VoidedAt != nil {
		return fmt.Errorf("%w: cannot edit voided payment", ErrValidation)
	}
	// Read the lock on this transaction, not the pool: a second connection asking
	// while this one holds the write lock is the shape that stacked busy waits.
	locked, err := isLocked(ctx, tx, before.PaidOn[:7])
	if err != nil {
		return err
	}
	if locked {
		return ErrLockedMonth
	}
	_, err = tx.ExecContext(ctx, `UPDATE payments SET head_id=?, paid_on=?, amount=?, vendor_payee=?, payment_mode=?, invoice_no=?, reference_no=?, remarks=?, updated_by=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		in.HeadID, in.PaidOn, in.Amount, in.VendorPayee, in.PaymentMode, in.InvoiceNo, in.ReferenceNo, in.Remarks, actor.ID, id)
	if err != nil {
		return classify(err)
	}
	after := paymentFromInput(id, actor, in)
	after.EnteredBy, after.EnteredByName = before.EnteredBy, before.EnteredByName
	after.UpdatedBy = &actor.ID
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "update", EntityType: "payment", EntityID: &id, Summary: "Edited payment " + money.FormatPaise(in.Amount), Before: before, After: after}); err != nil {
		return err
	}
	if attachment != nil {
		if _, err := addAttachmentTx(ctx, tx, actor, id, *attachment); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) VoidPayment(ctx context.Context, actor User, id int64, reason string) error {
	reason = strings.TrimSpace(reason)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := paymentInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	// S12: voiding would leave the request 'completed' with nothing paid.
	if before.RequestID != nil {
		return fmt.Errorf("%w: a payment linked to a request cannot be voided", ErrValidation)
	}
	if before.VoidedAt != nil {
		return nil
	}
	if reason == "" {
		return fmt.Errorf("%w: void reason is required", ErrValidation)
	}
	locked, err := isLocked(ctx, tx, before.PaidOn[:7])
	if err != nil {
		return err
	}
	if locked {
		return ErrLockedMonth
	}
	_, err = tx.ExecContext(ctx, `UPDATE payments SET voided_by=?, void_reason=?, voided_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP WHERE id=?`, actor.ID, reason, id)
	if err != nil {
		return err
	}
	after := before
	after.VoidedBy, after.VoidReason = &actor.ID, reason
	now := time.Now().UTC()
	after.VoidedAt = &now
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "void", EntityType: "payment", EntityID: &id, Summary: "Voided payment " + money.FormatPaise(before.Amount), Before: before, After: after}); err != nil {
		return err
	}
	return tx.Commit()
}

// Payment reads one ledger row. The heads/projects joins are LEFT joins with
// COALESCE because a payment settling a recoverable request has head_id NULL
// (v8): its detail screen must still resolve, with an empty project and head.
// The request's treatment rides along so that screen can say "Recoverable"
// where a budget payment names its project and head.
func (s *Store) Payment(ctx context.Context, id int64) (Payment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT py.id,COALESCE(py.head_id,0),COALESCE(h.project_id,0),COALESCE(p.name,''),COALESCE(h.name,''),py.paid_on,py.amount,
		COALESCE(py.vendor_payee,''),COALESCE(py.payment_mode,''),COALESCE(py.invoice_no,''),COALESCE(py.reference_no,''),COALESCE(py.remarks,''),
		py.entered_by,u.name,py.updated_by,py.voided_by,COALESCE(py.void_reason,''),py.voided_at,py.created_at,py.updated_at,
		py.request_id,COALESCE(py.settlement,''),COALESCE(py.partial_reason,''),COALESCE(pr.treatment,'')
		FROM payments py LEFT JOIN heads h ON h.id=py.head_id LEFT JOIN projects p ON p.id=h.project_id JOIN users u ON u.id=py.entered_by LEFT JOIN payment_requests pr ON pr.id=py.request_id WHERE py.id=?`, id)
	var p Payment
	err := row.Scan(&p.ID, &p.HeadID, &p.ProjectID, &p.Project, &p.Head, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.EnteredByName, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt, &p.RequestID, &p.Settlement, &p.PartialReason, &p.Treatment)
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	return p, err
}

// PaymentForRequest returns the latest payment linked to a request, or
// ErrNotFound when none exists yet. It powers the requester's outcome view (Q4).
func (s *Store) PaymentForRequest(ctx context.Context, requestID int64) (Payment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT py.id,COALESCE(py.head_id,0),COALESCE(h.project_id,0),COALESCE(p.name,''),COALESCE(h.name,''),py.paid_on,py.amount,
		COALESCE(py.vendor_payee,''),COALESCE(py.payment_mode,''),COALESCE(py.invoice_no,''),COALESCE(py.reference_no,''),COALESCE(py.remarks,''),
		py.entered_by,u.name,py.updated_by,py.voided_by,COALESCE(py.void_reason,''),py.voided_at,py.created_at,py.updated_at,
		py.request_id,COALESCE(py.settlement,''),COALESCE(py.partial_reason,''),COALESCE(pr.treatment,'')
		FROM payments py LEFT JOIN heads h ON h.id=py.head_id LEFT JOIN projects p ON p.id=h.project_id JOIN users u ON u.id=py.entered_by LEFT JOIN payment_requests pr ON pr.id=py.request_id WHERE py.request_id=? ORDER BY py.id DESC LIMIT 1`, requestID)
	var p Payment
	err := row.Scan(&p.ID, &p.HeadID, &p.ProjectID, &p.Project, &p.Head, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.EnteredByName, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt, &p.RequestID, &p.Settlement, &p.PartialReason, &p.Treatment)
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	return p, err
}

func paymentInTx(ctx context.Context, tx *sql.Tx, id int64) (Payment, error) {
	var p Payment
	err := tx.QueryRowContext(ctx, `SELECT id,COALESCE(head_id,0),paid_on,amount,COALESCE(vendor_payee,''),COALESCE(payment_mode,''),COALESCE(invoice_no,''),COALESCE(reference_no,''),COALESCE(remarks,''),entered_by,updated_by,voided_by,COALESCE(void_reason,''),voided_at,created_at,updated_at,request_id,COALESCE(settlement,''),COALESCE(partial_reason,'') FROM payments WHERE id=?`, id).
		Scan(&p.ID, &p.HeadID, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt, &p.RequestID, &p.Settlement, &p.PartialReason)
	if err == sql.ErrNoRows {
		return p, ErrNotFound
	}
	return p, err
}

func paymentFromInput(id int64, actor User, in PaymentInput) Payment {
	return Payment{ID: id, HeadID: in.HeadID, PaidOn: in.PaidOn, Amount: in.Amount, VendorPayee: in.VendorPayee, PaymentMode: in.PaymentMode, InvoiceNo: in.InvoiceNo, ReferenceNo: in.ReferenceNo, Remarks: in.Remarks, EnteredBy: actor.ID, EnteredByName: actor.Name}
}

func recordAuditTx(ctx context.Context, tx *sql.Tx, in AuditInput) error {
	before, _ := json.Marshal(in.Before)
	after, _ := json.Marshal(in.After)
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_log(actor_id,actor_name,action,entity_type,entity_id,summary,before_json,after_json,ip) VALUES(?,?,?,?,?,?,?,?,?)`, in.ActorID, in.ActorName, in.Action, in.EntityType, in.EntityID, in.Summary, nullJSON(before), nullJSON(after), in.IP)
	return err
}

// ReserveRequest's three refusal causes, distinguished so the conflict screen
// can tell the accountant what actually happened instead of "Someone else took
// this request" for all three (F-D-02). Each wraps ErrForbidden, so every
// existing errors.Is(err, ErrForbidden) branch — including the handler that
// routes reservation refusals to the conflict screen — keeps working; a caller
// that wants the real cause branches with errors.Is on the specific sentinel.
var (
	// ErrAlreadyReserved: the request is in 'processing', held by somebody.
	ErrAlreadyReserved = fmt.Errorf("%w: someone else is already processing this request", ErrForbidden)
	// ErrRequestOnHold: the request is approved but paused; the hold reason is
	// on the request and only Accounts lifts it.
	ErrRequestOnHold = fmt.Errorf("%w: this request is on hold", ErrForbidden)
	// ErrRequestNotApproved: the request is in some other state — pending,
	// completed, cancelled — that offers nothing to reserve.
	ErrRequestNotApproved = fmt.Errorf("%w: only an approved request can be taken for processing", ErrForbidden)
)

// ReserveRequest atomically moves an approved, unclaimed, not-on-hold request to
// 'processing' reserved by actor. The single conditional UPDATE is the
// concurrency guarantee: only the first committer matches, so a losing caller
// sees RowsAffected()==0 (S2, S5, L8). Only then — the race is already lost —
// is the row re-read to name which of the three causes refused it (F-D-02).
func (s *Store) ReserveRequest(ctx context.Context, actor User, id int64) error {
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := paymentRequestScopeTx(ctx, tx, actor.ID, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests
		SET status='processing', processing_by=?, processing_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='approved' AND processing_by IS NULL AND on_hold=0`, actor.ID, id)
	if err != nil {
		return classify(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var status string
		var processingBy sql.NullInt64
		var onHold int
		if err := tx.QueryRowContext(ctx, `SELECT status, processing_by, on_hold FROM payment_requests WHERE id=?`, id).Scan(&status, &processingBy, &onHold); err != nil {
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			return err
		}
		switch {
		// on_hold=1 implies status='approved', so the order cannot misname a
		// held request as taken or vice versa.
		case onHold == 1:
			return ErrRequestOnHold
		case status == "processing" || processingBy.Valid:
			return ErrAlreadyReserved
		default:
			return ErrRequestNotApproved
		}
	}
	// Every payment_request summary written on this side starts with the actor,
	// as the request-module ones do: the detail thread shows the summary as the
	// line's whole title, so a summary that leaves the name out is a line that
	// says what happened and not who did it (L7–L10).
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "process", EntityType: "payment_request", EntityID: &id, Summary: actor.Name + " reserved the request for processing", After: map[string]any{"processing_by": actor.ID}}); err != nil {
		return err
	}
	return tx.Commit()
}

// ReleaseRequest returns a 'processing' request to 'approved'. confirmed must be
// true ("no payment initiated" — S7) and reason is required (G12): the release is
// visible to the requester and the approver, and the reservation .thread renders
// it. Only the assignee, or an authorized caller (handler resolved
// reservation:release beyond their own reservations), may release (S6). There is
// no auto-release; this is the only path back to approved.
func (s *Store) ReleaseRequest(ctx context.Context, actor User, id int64, reason string, confirmed, authorized bool) error {
	if !confirmed {
		return fmt.Errorf("%w: confirm that no payment was initiated before releasing", ErrValidation)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a reason is required so the requester and approver know why", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var processingBy sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT status, processing_by FROM payment_requests WHERE id=?`, id).Scan(&status, &processingBy); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if status != "processing" {
		return fmt.Errorf("%w: only a processing request can be released", ErrForbidden)
	}
	if !authorized && (!processingBy.Valid || processingBy.Int64 != actor.ID) {
		return fmt.Errorf("%w: only the assignee may release this request", ErrForbidden)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='approved', processing_by=NULL, processing_at=NULL, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='processing'`, id); err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "release", EntityType: "payment_request", EntityID: &id, Summary: actor.Name + " released the reservation: " + reason, Before: map[string]any{"processing_by": processingBy.Int64}}); err != nil {
		return err
	}
	return tx.Commit()
}

// ReassignReservation hands a reservation to another user without ever returning
// the request to the open queue (G11): status stays 'processing' and only
// processing_by moves, so no third party can slip in between. authorized is the
// handler-resolved reservation:reassign grant; the store fails closed without it.
// reason is required and is what the reservation .thread renders.
func (s *Store) ReassignReservation(ctx context.Context, actor User, id, toUserID int64, reason string, authorized bool) error {
	if !authorized {
		return fmt.Errorf("%w: reassigning someone else's reservation needs the reassign permission", ErrForbidden)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a reason is required when a reservation changes hands", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var processingBy sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT status, processing_by FROM payment_requests WHERE id=?`, id).Scan(&status, &processingBy); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if status != "processing" || !processingBy.Valid {
		return fmt.Errorf("%w: only a reserved request can be reassigned", ErrForbidden)
	}
	if processingBy.Int64 == toUserID {
		return fmt.Errorf("%w: that person already holds this reservation", ErrValidation)
	}
	var toName string
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT name, active FROM users WHERE id=?`, toUserID).Scan(&toName, &active); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if active == 0 {
		return fmt.Errorf("%w: that user is deactivated", ErrValidation)
	}
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests
		SET processing_by=?, processing_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='processing' AND processing_by=?`, toUserID, id, processingBy.Int64)
	if err != nil {
		return classify(err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("%w: the reservation changed while you were deciding", ErrForbidden)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "reassign", EntityType: "payment_request", EntityID: &id,
		Summary: actor.Name + " reassigned the reservation to " + toName + ": " + reason,
		Before:  map[string]any{"processing_by": processingBy.Int64},
		After:   map[string]any{"processing_by": toUserID}}); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordPaymentForRequest atomically appends a payment to the request the actor
// holds. Installments return to approved with the remaining balance payable;
// settled closes, while partial asks the manager to decide a proposed shortfall.
// The cumulative amount cannot exceed approval. Confirmation keys and balance
// snapshots reject replays and stale forms, including after a fresh reservation.
// Payments and their attachments commit together and remain immutable.
//
// F-D-01: the head, the payee and the invoice number are facts of the request —
// a manager approved an amount against a project and head, and the entry screen
// never offers a control to change any of them — so all three are derived from
// the request row this transaction already holds open, and whatever the form
// sent in those fields is ignored. A recoverable request has no head (F-D-11):
// its payment is written with head_id NULL, which the grid and the monthly
// report never match because they join payments on head_id.
func (s *Store) RecordPaymentForRequest(ctx context.Context, actor User, requestID int64, in PaymentInput, settlement, partialReason string, attachment *AttachmentInput) (int64, error) {
	// Electronic and instrument payments need a usable bank/payment reference.
	// Untyped historical store callers retain their compatibility behavior; the HTTP boundary requires a reference for every linked submission.
	switch strings.ToLower(strings.TrimSpace(in.PaymentMode)) {
	case "neft", "rtgs", "upi", "cheque", "card", "dd", "bank_transfer", "cash":
		if strings.TrimSpace(in.ReferenceNo) == "" {
			return 0, fmt.Errorf("%w: enter a transaction or payment reference; spaces alone are not a reference", ErrValidation)
		}
	}

	settlement = strings.TrimSpace(settlement)
	partialReason = strings.TrimSpace(partialReason)
	if settlement != "settled" && settlement != "partial" && settlement != "installment" {
		return 0, fmt.Errorf("%w: choose how the remaining balance should be handled", ErrValidation)
	}
	if settlement == "partial" && partialReason == "" {
		return 0, fmt.Errorf("%w: a reason is required for a partial settlement", ErrValidation)
	}
	if attachment != nil {
		if err := validateAttachment(*attachment); err != nil {
			return 0, err
		}
	}
	// beginWriteTx, not BeginTx: this reads the request and then writes it, the
	// exact read-then-upgrade shape SQLite refuses to promote while another
	// writer is active — and refuses without consulting the busy handler, so the
	// loser of two simultaneous settlements failed instantly. Every writer in
	// requests.go was converted for this reason; the payment writers were missed.
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var status, treatment, displayPayee, invoiceNo string
	var processingBy, headID, vendorID sql.NullInt64
	var requested int64
	var approved sql.NullInt64
	// The display payee, exactly as Request() resolves it: a vendor_invoice
	// names its payee with vendor_id and leaves the snapshot empty.
	//
	// F-G-009: r.vendor_id is read for the same reason head_id is — it is a fact
	// of the request, not of the form — and is written onto the payment so the
	// vendor master can total spend by identity instead of by matching the payee
	// snapshot's text. Migration v10 back-filled the payments that already
	// existed; this is what keeps every payment made from now on linked.
	if err := tx.QueryRowContext(ctx, `SELECT r.status, r.processing_by, r.amount, r.approved_amount,
		r.treatment, r.head_id, r.vendor_id, COALESCE(NULLIF(v.name,''), r.vendor_payee, ''), COALESCE(r.invoice_no,'')
		FROM payment_requests r LEFT JOIN vendors v ON v.id=r.vendor_id
		WHERE r.id=?`, requestID).Scan(&status, &processingBy, &requested, &approved, &treatment, &headID, &vendorID, &displayPayee, &invoiceNo); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if err := paymentRequestScopeTx(ctx, tx, actor.ID, requestID); err != nil {
		return 0, err
	}
	if in.SubmissionKey != "" {
		var priorID int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM payments WHERE request_id=? AND submission_key=? AND entered_by=?`, requestID, in.SubmissionKey, actor.ID).Scan(&priorID)
		if err == nil {
			return 0, fmt.Errorf("%w: this payment confirmation was already recorded", ErrValidation)
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
	}
	if status != "processing" || !processingBy.Valid || processingBy.Int64 != actor.ID {
		return 0, fmt.Errorf("%w: reserve this request before recording its payment", ErrForbidden)
	}
	// F-D-01: overwrite, never compare — the form's copies of these fields are
	// display baggage from the entry screen, not input.
	in.HeadID = headID.Int64 // 0 when the request has no head
	in.VendorPayee = displayPayee
	in.InvoiceNo = invoiceNo
	// Validated on this transaction: reading the month lock and the head on a
	// second pooled connection while this one holds the write lock is how a
	// single settlement came to stack four separate five-second busy waits.
	if err := validatePaymentTx(ctx, tx, in, treatment == "recoverable"); err != nil {
		return 0, err
	}
	// G13: the approved amount is a hard ceiling. Paying more is not a settlement
	// decision, it is a different obligation — cancel and raise a new request.
	ceiling := requested
	if approved.Valid {
		ceiling = approved.Int64
	}
	var previouslyPaid int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM payments WHERE request_id=? AND voided_at IS NULL`, requestID).Scan(&previouslyPaid); err != nil {
		return 0, err
	}
	if in.ExpectedPaid != nil && *in.ExpectedPaid != previouslyPaid {
		return 0, fmt.Errorf("%w: another payment was recorded after this form opened; reopen the payment form to use the current balance", ErrValidation)
	}
	remaining := ceiling - previouslyPaid
	if in.Amount < remaining && settlement == "settled" && partialReason == "" {
		return 0, fmt.Errorf("%w: explain the deduction or agreed adjustment before closing a short payment", ErrValidation)
	}
	if in.Amount > remaining {
		if previouslyPaid > 0 {
			return 0, fmt.Errorf("%w: %s exceeds the remaining approved balance of %s; additional spending needs a separately approved request", ErrValidation, money.FormatPaise(in.Amount), money.FormatPaise(remaining))
		}
		return 0, fmt.Errorf("%w: %s is more than the approved %s — to pay more, cancel this request and raise a new one",
			ErrValidation, money.FormatPaise(in.Amount), money.FormatPaise(remaining))
	}
	if in.Amount == remaining && settlement == "partial" {
		return 0, fmt.Errorf("%w: this payment clears the remaining balance; choose fully settled instead of shortfall review", ErrValidation)
	}
	// A zero head is stored as NULL, never 0 — heads(id) has no row 0, and the
	// v8 schema keeps the foreign key.
	var headArg any
	if in.HeadID != 0 {
		headArg = in.HeadID
	}
	// Same treatment for the vendor: NULL, never 0. A reimbursement or an
	// employee advance names no vendor row at all, and vendors(id) has no row 0.
	var vendorArg any
	if vendorID.Valid && vendorID.Int64 != 0 {
		vendorArg = vendorID.Int64
	}
	storedSettlement := settlement
	if settlement == "installment" {
		storedSettlement = "partial"
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO payments(head_id,vendor_id,paid_on,amount,vendor_payee,payment_mode,invoice_no,reference_no,remarks,entered_by,request_id,settlement,partial_reason,submission_key)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		headArg, vendorArg, in.PaidOn, in.Amount, in.VendorPayee, in.PaymentMode, in.InvoiceNo, in.ReferenceNo, in.Remarks, actor.ID, requestID, storedSettlement, partialReason, nullableText(in.SubmissionKey))
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	newStatus := "completed"
	if settlement == "partial" {
		newStatus = "partial_review"
	}
	if settlement == "installment" && in.Amount < remaining {
		newStatus = "approved"
	}
	upd, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status=?, processing_by=CASE WHEN ?='approved' THEN NULL ELSE processing_by END, processing_at=CASE WHEN ?='approved' THEN NULL ELSE processing_at END, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='processing' AND processing_by=?`, newStatus, newStatus, newStatus, requestID, actor.ID)
	if err != nil {
		return 0, err
	}
	if n, err := upd.RowsAffected(); err != nil {
		return 0, err
	} else if n == 0 {
		return 0, fmt.Errorf("%w: reservation was lost before settlement", ErrForbidden)
	}
	after := paymentFromInput(id, actor, in)
	after.RequestID = &requestID
	after.Settlement = storedSettlement
	after.PartialReason = partialReason
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "create", EntityType: "payment", EntityID: &id, Summary: "Recorded payment " + money.FormatPaise(in.Amount), After: after}); err != nil {
		return 0, err
	}
	reqAction, reqSummary := "settle", actor.Name+" settled the request as completed"
	if settlement == "partial" {
		reqAction, reqSummary = "mark_partial", actor.Name+" recorded a partial settlement: "+partialReason
	}
	if settlement == "installment" {
		reqAction, reqSummary = "installment", actor.Name+" recorded an installment; "+money.FormatPaise(remaining-in.Amount)+" remains approved for payment"
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: reqAction, EntityType: "payment_request", EntityID: &requestID, Summary: reqSummary, After: map[string]any{"status": newStatus, "payment_id": id}}); err != nil {
		return 0, err
	}
	if attachment != nil {
		if _, err := addAttachmentTx(ctx, tx, actor, id, *attachment); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// AcceptPartial closes a 'partial_review' request as 'completed_partial' — a
// terminal state distinct from a clean 'completed' (S11, G14). The difference is
// permanent and visible: the ledger renders .pill.completed-partial, and anyone
// reading the request later can see that a balance was written off rather than
// paid. note is optional and joins the trail the requester reads.
func (s *Store) AcceptPartial(ctx context.Context, actor User, id int64, note string) error {
	note = strings.TrimSpace(note)
	// Reads the request, then writes it — beginWriteTx for the same reason
	// RecordPaymentForRequest needs it.
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Whose decision this is, asked here and not only by the template. Holding
	// approval:accept_partial says a person may accept a shortfall; it never
	// says whose. ApproveRequest and decideRequest both refuse an actor who is
	// not the request's own manager, and writing a balance off for good is the
	// last place to relax that.
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	// concern_open goes with it: the discussion ends with the decision.
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET status='completed_partial', concern_open=0, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='partial_review'`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("%w: only a partial-review request can be accepted", ErrForbidden)
	}
	summary := actor.Name + " accepted the partial settlement — completed, partial accepted"
	if note != "" {
		summary += ": " + note
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "accept_partial", EntityType: "payment_request", EntityID: &id, Summary: summary, After: map[string]any{"status": "completed_partial"}}); err != nil {
		return err
	}
	return tx.Commit()
}

// RaiseConcern keeps a 'partial_review' request in review and appends a
// conversation comment (S11). comment is required.
func (s *Store) RaiseConcern(ctx context.Context, actor User, id int64, comment string) error {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return fmt.Errorf("%w: a concern comment is required", ErrValidation)
	}
	// Reads the request, then writes a comment — same shape, same fix.
	tx, err := s.beginWriteTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, err := requestInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	// Disputing a shortfall is the other half of accepting it, so it answers to
	// the same owner: the manager the request was routed to, not everybody who
	// happens to hold the verb.
	if before.ManagerID != actor.ID {
		return ErrForbidden
	}
	if before.Status != "partial_review" {
		return fmt.Errorf("%w: only a partial-review request can receive a concern", ErrForbidden)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO request_comments(request_id,author_id,body) VALUES(?,?,?)`, id, actor.ID, comment); err != nil {
		return classify(err)
	}
	// The concern is now open until the holder answers (AddRequestComment
	// clears it). The status is untouched: "under discussion" is how the
	// request is displayed, not a state it moves to.
	if _, err := tx.ExecContext(ctx, `UPDATE payment_requests SET concern_open=1, updated_at=CURRENT_TIMESTAMP WHERE id=?`, id); err != nil {
		return err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "concern", EntityType: "payment_request", EntityID: &id, Summary: "Raised concern: " + comment}); err != nil {
		return err
	}
	return tx.Commit()
}

// HoldRequest pauses an approved, not-already-held request (L7). reason required.
func (s *Store) HoldRequest(ctx context.Context, actor User, id int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%w: a hold reason is required", ErrValidation)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET on_hold=1, hold_reason=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='approved' AND on_hold=0`, reason, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("%w: only an approved, not-already-held request can be held", ErrForbidden)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "hold", EntityType: "payment_request", EntityID: &id, Summary: actor.Name + " put the request on hold: " + reason}); err != nil {
		return err
	}
	return tx.Commit()
}

// UnholdRequest lifts a hold. The route gate (payment.hold) restricts this to
// Accounts (L7).
func (s *Store) UnholdRequest(ctx context.Context, actor User, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE payment_requests SET on_hold=0, hold_reason='', updated_at=CURRENT_TIMESTAMP WHERE id=? AND on_hold=1`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("%w: request is not on hold", ErrForbidden)
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "unhold", EntityType: "payment_request", EntityID: &id, Summary: actor.Name + " lifted the hold"}); err != nil {
		return err
	}
	return tx.Commit()
}

// LinkablePaymentRequests powers the Accounts queue and the request picker.
// Available is the S3 set — approved · unclaimed · not on hold — the only rows
// that may be reserved. Unavailable is everything else the tab asked for that
// the caller may see but not take. Both are searchable by number / requester /
// payee / project / head / amount (S4). Counts always span the caller's scope.
func (s *Store) LinkablePaymentRequests(ctx context.Context, opts LinkableOptions) (LinkableSet, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	// Staleness is measured against an injected clock, never julianday('now'):
	// a test must be able to decide what "26 hours ago" means. CURRENT_TIMESTAMP
	// writes UTC in this format, so the cutoff compares as plain text.
	staleCutoff := now.UTC().Add(-StaleReservation).Format("2006-01-02 15:04:05")
	var out LinkableSet

	// The same fail-closed rule as requestWhere: only "all" is unrestricted, and
	// an empty or unrecognised scope reads nothing (fixwave rbac-2).
	scopeSQL, scopeArgs := "", []any(nil)
	switch opts.Scope {
	case ScopeAll:
	case "own":
		scopeSQL, scopeArgs = ` AND r.requester_id=?`, []any{opts.ViewerID}
	case "assigned":
		scopeSQL, scopeArgs = ` AND r.manager_id=?`, []any{opts.ViewerID}
	default:
		scopeSQL = ` AND 0`
	}

	// Counts first: one aggregate pass over the scope, independent of the tab,
	// the search and the limit. Stale reservations are counted here too, because
	// the banner is rendered on tabs whose rows do not include them.
	countQ := `SELECT
		COALESCE(SUM(CASE WHEN r.status='approved' AND r.processing_by IS NULL AND r.on_hold=0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='approved' AND r.processing_by IS NULL AND r.on_hold=0 THEN MAX(0,COALESCE(r.approved_amount, r.amount)-(SELECT COALESCE(SUM(ip.amount),0) FROM payments ip WHERE ip.request_id=r.id AND ip.voided_at IS NULL)) ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='processing' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.on_hold=1 AND r.status='approved' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='partial_review' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status IN ('completed','completed_partial') THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='processing' AND r.processing_by=? THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='processing' AND r.processing_by IS NOT NULL AND r.processing_by<>? THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN r.status='processing' AND r.processing_by=? AND r.processing_at IS NOT NULL AND r.processing_at<=? THEN 1 ELSE 0 END),0)
		FROM payment_requests r WHERE 1=1` + scopeSQL
	countArgs := append([]any{opts.ViewerID, opts.ViewerID, opts.ViewerID, staleCutoff}, scopeArgs...)
	if err := s.db.QueryRowContext(ctx, countQ, countArgs...).Scan(
		&out.Counts.Approved, &out.Counts.ApprovedAmount, &out.Counts.Processing,
		&out.Counts.Hold, &out.Counts.PartialReview, &out.Counts.Paid,
		&out.Counts.ReservedByMe, &out.Counts.ReservedByOthers, &out.Counts.StaleReservations); err != nil {
		return out, err
	}

	// Two payee columns, exactly as Request() reads them: vendor_payee is the raw
	// snapshot, filled only for reimbursement and employee advance, and Vendor is
	// the display payee a vendor_invoice actually has. A screen that renders the
	// snapshot asks the accountant to pay a blank.
	// short_title is selected because the dashboard's work-area rows render
	// "{{.Number}} · {{.ShortTitle}}". Without it every row on the Accounts and
	// administrator home screens ended in a separator with nothing after it.
	q := `SELECT r.id, r.number, COALESCE(r.short_title,''), r.status, r.amount, r.approved_amount, COALESCE(r.vendor_payee,''),
		COALESCE(NULLIF(v.name,''),r.vendor_payee,''),
		COALESCE(p.name,''), COALESCE(h.name,''), r.requester_id, COALESCE(u.name,''), r.manager_id,
		r.on_hold, COALESCE(r.hold_reason,''), r.processing_by, COALESCE(pu.name,''), r.processing_at,
		r.head_id, COALESCE(r.needed_by,''), r.treatment, r.type, r.approved_at, r.concern_open, r.urgent,
 (SELECT COALESCE(SUM(ip.amount),0) FROM payments ip WHERE ip.request_id=r.id AND ip.voided_at IS NULL)
		FROM payment_requests r
		LEFT JOIN projects p ON p.id=r.project_id
		LEFT JOIN heads h ON h.id=r.head_id
		LEFT JOIN users pu ON pu.id=r.processing_by
		LEFT JOIN vendors v ON v.id=r.vendor_id
		JOIN users u ON u.id=r.requester_id
		WHERE `
	var args []any
	switch opts.Status {
	case "", "approved":
		// The approved pool: what may be taken, plus what has been taken out of
		// it. The picker needs the second half to render .co.is-taken rows rather
		// than silently hiding a request someone is already paying.
		q += `r.status IN ('approved','processing')`
	case "processing":
		q += `r.status='processing'`
	case "hold":
		// A hold pauses an approved request and every writer that moves a
		// request off 'approved' clears it, so the status test is belt and
		// braces: it stops a row written before that rule existed from sitting
		// in this tab for ever, being offered a reply that can change nothing.
		q += `r.on_hold=1 AND r.status='approved'`
	case "partial_review":
		q += `r.status='partial_review'`
	case "paid":
		q += `r.status IN ('completed','completed_partial')`
	default:
		return out, fmt.Errorf("%w: unknown queue tab %q", ErrValidation, opts.Status)
	}
	q += scopeSQL
	args = append(args, scopeArgs...)
	if search := strings.ToLower(strings.TrimSpace(opts.Query)); search != "" {
		// The payee is searched as it is displayed: typing a vendor's name must
		// find the row that shows that name.
		//
		// F-D-04: so is the amount. r.amount is int64 paise, so matching the
		// search text against CAST(r.amount AS TEXT) meant `7,431.00` — the only
		// form of the figure that appears on any screen, since every rendering
		// goes through money.FormatPaise — never found ₹7,431.00, while 743100
		// did. The text match is kept, because a substring of the paise integer
		// is still a useful needle and TC-D-013 pins that 743100 works; what is
		// added is an exact paise comparison whenever the needle parses as money.
		// money.ParsePaise already strips ₹, commas and whitespace, so it is the
		// right normaliser and the two cannot drift.
		amountClause := ` OR CAST(r.amount AS TEXT) LIKE ? ESCAPE '\'`
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
		needle := "%" + esc + "%"
		extra := []any{needle, needle, needle, needle, needle, needle}
		if paise, err := money.ParsePaise(search); err == nil {
			amountClause += ` OR r.amount = ?`
			extra = append(extra, paise)
		}
		q += ` AND (lower(r.number) LIKE ? ESCAPE '\' OR lower(u.name) LIKE ? ESCAPE '\' OR lower(COALESCE(NULLIF(v.name,''),r.vendor_payee,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(p.name,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(h.name,'')) LIKE ? ESCAPE '\'` + amountClause + `)`
		args = append(args, extra...)
	}
	// CURRENT_TIMESTAMP is second-resolution, so a burst of approvals shares one
	// timestamp; id breaks the tie and keeps the order stable.
	//
	// Urgent first, as every other request list sorts: the design's approved
	// stage reads "In the Accounts queue. Urgent requests surface here", and
	// the dashboard's Accounts area shows only the first few of these rows, so
	// an urgent request behind older ordinary ones did not surface at all
	// (urgent-1). The Paid tab is history, not work, and keeps approval order.
	if opts.Status == "paid" {
		q += ` ORDER BY r.approved_at, r.id`
	} else {
		q += ` ORDER BY r.urgent DESC, r.approved_at, r.id`
	}
	if opts.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, opts.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Request
		var approved, processingBy, headID sql.NullInt64
		var processingAt, approvedAt sql.NullTime
		var onHold, concernOpen, urgent int
		if err := rows.Scan(&r.ID, &r.Number, &r.ShortTitle, &r.Status, &r.Amount, &approved, &r.VendorPayee, &r.Vendor,
			&r.Project, &r.Head, &r.RequesterID, &r.RequesterName, &r.ManagerID,
			&onHold, &r.HoldReason, &processingBy, &r.ProcessingByName, &processingAt,
			&headID, &r.NeededBy, &r.Treatment, &r.Type, &approvedAt, &concernOpen, &urgent, &r.PaidAmount); err != nil {
			return out, err
		}
		r.ConcernOpen = concernOpen == 1
		if approved.Valid {
			v := approved.Int64
			r.ApprovedAmount = &v
		}
		if processingBy.Valid {
			v := processingBy.Int64
			r.ProcessingBy = &v
		}
		if headID.Valid {
			v := headID.Int64
			r.HeadID = &v
		}
		if processingAt.Valid {
			v := processingAt.Time
			r.ProcessingAt = &v
		}
		if approvedAt.Valid {
			v := approvedAt.Time
			r.ApprovedAt = &v
		}
		r.OnHold = onHold == 1
		r.Urgent = urgent == 1
		// Availability is re-derived from the row, never from the tab, so no tab
		// can ever hand the UI a "Take for processing" button it must not have.
		if r.Status == "approved" && !processingBy.Valid && !r.OnHold {
			out.Available = append(out.Available, r)
		} else {
			out.Unavailable = append(out.Unavailable, r)
		}
	}
	return out, rows.Err()
}

func (s *Store) RecentPayments(ctx context.Context, limit int) ([]Payment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT py.id,COALESCE(py.head_id,0),COALESCE(h.project_id,0),COALESCE(p.name,''),COALESCE(h.name,''),py.paid_on,py.amount,
		COALESCE(py.vendor_payee,''),COALESCE(py.payment_mode,''),COALESCE(py.invoice_no,''),COALESCE(py.reference_no,''),COALESCE(py.remarks,''),
		py.entered_by,u.name,py.updated_by,py.voided_by,COALESCE(py.void_reason,''),py.voided_at,py.created_at,py.updated_at
		FROM payments py LEFT JOIN heads h ON h.id=py.head_id LEFT JOIN projects p ON p.id=h.project_id JOIN users u ON u.id=py.entered_by
		WHERE py.voided_at IS NULL
		ORDER BY py.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.ID, &p.HeadID, &p.ProjectID, &p.Project, &p.Head, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.EnteredByName, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ListPayments(ctx context.Context, opts PaymentListOptions) ([]Payment, error) {
	if opts.Limit <= 0 {
		opts.Limit = 300
	}
	q := `SELECT py.id,COALESCE(py.head_id,0),COALESCE(h.project_id,0),COALESCE(p.name,''),COALESCE(h.name,''),py.paid_on,py.amount,
		COALESCE(py.vendor_payee,''),COALESCE(py.payment_mode,''),COALESCE(py.invoice_no,''),COALESCE(py.reference_no,''),COALESCE(py.remarks,''),
		py.entered_by,u.name,py.updated_by,py.voided_by,COALESCE(py.void_reason,''),py.voided_at,py.created_at,py.updated_at,
		py.request_id,COALESCE(py.settlement,''),COALESCE(py.partial_reason,''),COALESCE(pr.treatment,'')
		FROM payments py LEFT JOIN heads h ON h.id=py.head_id LEFT JOIN projects p ON p.id=h.project_id JOIN users u ON u.id=py.entered_by LEFT JOIN payment_requests pr ON pr.id=py.request_id`
	var where []string
	var args []any
	// F-A-04 / F-G-003: the payment data scope. "own" narrows the ledger to
	// rows the viewer entered; "assigned" has no routed-to meaning on payments
	// and narrows the same way rather than silently widening to everything —
	// an administrator who chose either narrowing gets a narrowing. Only "all"
	// is unrestricted: "" (the role editor's "None") and anything else read no
	// rows, exactly as requestWhere treats the request scope (fixwave rbac-2).
	switch opts.Scope {
	case ScopeAll:
	case "own", "assigned":
		where = append(where, `py.entered_by=?`)
		args = append(args, opts.ViewerID)
	default:
		where = append(where, `0`)
	}
	if opts.VendorID > 0 {
		where = append(where, `pr.vendor_id=?`)
		args = append(args, opts.VendorID)
	}
	if validMonth(opts.Month) {
		where = append(where, `substr(py.paid_on,1,7)=?`)
		args = append(args, opts.Month)
	}
	if opts.ProjectID > 0 {
		where = append(where, `h.project_id=?`)
		args = append(args, opts.ProjectID)
	}
	if opts.HeadID > 0 {
		where = append(where, `py.head_id=?`)
		args = append(args, opts.HeadID)
	}
	// The same COALESCE Grid uses, so a historical payment (no request at all)
	// stays in, exactly as it still counts as an actual there.
	if opts.ExcludeRecoverable {
		where = append(where, `COALESCE(pr.treatment,'') <> 'recoverable'`)
	}
	switch opts.Status {
	case "voided":
		where = append(where, `py.voided_at IS NOT NULL`)
	case "all":
	default:
		where = append(where, `py.voided_at IS NULL`)
	}
	search := strings.ToLower(strings.TrimSpace(opts.Query))
	if search != "" {
		where = append(where, `(lower(p.name) LIKE ? ESCAPE '\' OR lower(h.name) LIKE ? ESCAPE '\' OR lower(COALESCE(py.vendor_payee,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(py.invoice_no,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(py.reference_no,'')) LIKE ? ESCAPE '\' OR lower(COALESCE(py.remarks,'')) LIKE ? ESCAPE '\')`)
		search = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
		needle := "%" + search + "%"
		args = append(args, needle, needle, needle, needle, needle, needle)
	}
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, ` AND `)
	}
	q += ` ORDER BY py.paid_on DESC, py.created_at DESC LIMIT ? OFFSET ?`
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	args = append(args, opts.Limit, opts.Offset)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		var p Payment
		// request_id travels with the row because the ledger has to tell a
		// linked payment from a historical one: the linked one is immutable
		// (S12), and a screen that cannot see the difference offers an Edit
		// button the store would refuse.
		if err := rows.Scan(&p.ID, &p.HeadID, &p.ProjectID, &p.Project, &p.Head, &p.PaidOn, &p.Amount, &p.VendorPayee, &p.PaymentMode, &p.InvoiceNo, &p.ReferenceNo, &p.Remarks, &p.EnteredBy, &p.EnteredByName, &p.UpdatedBy, &p.VoidedBy, &p.VoidReason, &p.VoidedAt, &p.CreatedAt, &p.UpdatedAt, &p.RequestID, &p.Settlement, &p.PartialReason, &p.Treatment); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) AddAttachment(ctx context.Context, actor User, paymentID int64, original, stored, mime string, size int64) error {
	attachment := AttachmentInput{OriginalName: original, StoredPath: stored, MimeType: mime, SizeBytes: size}
	if err := validateAttachment(attachment); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := paymentInTx(ctx, tx, paymentID)
	if err != nil {
		return err
	}
	// S12 / F-D-08: a payment that settles a request is the request's outcome
	// and the screen calls it read-only. Edit and void already refuse a linked
	// row; a new document is the same mutation of that record and gets the
	// same answer. (The settlement's own proof travels inside
	// RecordPaymentForRequest's transaction, which never passes through here.)
	if p.RequestID != nil {
		return fmt.Errorf("%w: a payment linked to a request cannot receive a new attachment", ErrValidation)
	}
	if p.VoidedAt != nil {
		return fmt.Errorf("%w: cannot attach files to a voided payment", ErrValidation)
	}
	locked, err := isLocked(ctx, tx, p.PaidOn[:7])
	if err != nil {
		return err
	}
	if locked {
		return ErrLockedMonth
	}
	if _, err := addAttachmentTx(ctx, tx, actor, paymentID, attachment); err != nil {
		return err
	}
	return tx.Commit()
}

func addAttachmentTx(ctx context.Context, tx *sql.Tx, actor User, paymentID int64, attachment AttachmentInput) (int64, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO payment_attachments(payment_id,original_name,stored_path,mime_type,size_bytes,uploaded_by) VALUES(?,?,?,?,?,?)`, paymentID, attachment.OriginalName, attachment.StoredPath, attachment.MimeType, attachment.SizeBytes, actor.ID)
	if err != nil {
		return 0, classify(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := recordAuditTx(ctx, tx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "attach", EntityType: "payment", EntityID: &paymentID, Summary: "Uploaded attachment " + attachment.OriginalName, After: map[string]any{"id": id, "name": attachment.OriginalName}}); err != nil {
		return 0, err
	}
	return id, nil
}

// AttachmentByID retrieves a single attachment for authorized download
// handlers. Authorization is intentionally left to the app's permission layer.
func (s *Store) AttachmentByID(ctx context.Context, id int64) (Attachment, error) {
	var a Attachment
	err := s.db.QueryRowContext(ctx, `SELECT id,payment_id,original_name,stored_path,COALESCE(mime_type,''),size_bytes,uploaded_by,created_at FROM payment_attachments WHERE id=?`, id).
		Scan(&a.ID, &a.PaymentID, &a.OriginalName, &a.StoredPath, &a.MimeType, &a.SizeBytes, &a.UploadedBy, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return a, ErrNotFound
	}
	return a, err
}

// AttachmentWithPayment resolves a payment attachment to the payment it
// belongs to, in one call, so a download or upload handler can walk from the
// attachment to its payment and — through Payment.RequestID — to the request
// whose data scope governs it (F-A-01/F-A-03). Authorization stays with the
// app layer; this only supplies the ownership chain it needs.
func (s *Store) AttachmentWithPayment(ctx context.Context, id int64) (Attachment, Payment, error) {
	a, err := s.AttachmentByID(ctx, id)
	if err != nil {
		return Attachment{}, Payment{}, err
	}
	p, err := s.Payment(ctx, a.PaymentID)
	if err != nil {
		return Attachment{}, Payment{}, err
	}
	return a, p, nil
}

// RequestAttachmentByID resolves a request document from its own table.
// Request documents live in request_attachments, not payment_attachments —
// the two id spaces are unrelated (F-A-05) — so the request-document route
// must resolve here and scope by the owning RequestID, never through
// AttachmentByID. Authorization is intentionally left to the app layer.
func (s *Store) RequestAttachmentByID(ctx context.Context, id int64) (RequestAttachment, error) {
	var a RequestAttachment
	err := s.db.QueryRowContext(ctx, `SELECT id,request_id,original_name,stored_path,COALESCE(mime_type,''),size_bytes,uploaded_by,created_at FROM request_attachments WHERE id=?`, id).
		Scan(&a.ID, &a.RequestID, &a.OriginalName, &a.StoredPath, &a.MimeType, &a.SizeBytes, &a.UploadedBy, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Store) Attachments(ctx context.Context, paymentID int64) ([]Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,payment_id,original_name,stored_path,COALESCE(mime_type,''),size_bytes,uploaded_by,created_at FROM payment_attachments WHERE payment_id=? ORDER BY created_at`, paymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.PaymentID, &a.OriginalName, &a.StoredPath, &a.MimeType, &a.SizeBytes, &a.UploadedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Grid(ctx context.Context, month, status, q string) (GridData, error) {
	if !validMonth(month) {
		month = time.Now().Format("2006-01")
	}
	// A recoverable payment is a deposit, not an expense: it never counts as
	// budget actuals (V2). The COALESCE keeps historical payments — the ones
	// with no linked request at all — counting exactly as they always have.
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.name,h.id,h.name,h.active,p.active,COALESCE(h.due_day,''),COALESCE(b.amount,0),
		COALESCE(SUM(CASE WHEN py.voided_at IS NULL AND COALESCE(pr.treatment,'') <> 'recoverable' THEN py.amount ELSE 0 END),0)
		FROM heads h JOIN projects p ON p.id=h.project_id
		LEFT JOIN budgets b ON b.head_id=h.id AND b.month=?
		LEFT JOIN payments py ON py.head_id=h.id AND substr(py.paid_on,1,7)=?
		LEFT JOIN payment_requests pr ON pr.id=py.request_id
		WHERE (h.active=1 AND p.active=1)
			OR b.id IS NOT NULL
			OR EXISTS(SELECT 1 FROM payments px
				LEFT JOIN payment_requests pxr ON pxr.id=px.request_id
				WHERE px.head_id=h.id AND substr(px.paid_on,1,7)=?
					AND COALESCE(pxr.treatment,'') <> 'recoverable')
		GROUP BY p.id,p.name,h.id,h.name,h.active,p.active,h.due_day,b.amount
		ORDER BY p.sort_order,p.name,h.sort_order,h.name`, month, month, month)
	if err != nil {
		return GridData{}, err
	}
	defer rows.Close()
	g := GridData{Month: month, Filtered: strings.TrimSpace(q) != "" || (status != "" && status != "all"), ScopeLabel: "Company total"}
	if g.Filtered {
		g.ScopeLabel = "Filtered subtotal"
	}
	if lock, err := s.MonthLock(ctx, month); err == nil {
		g.Locked = true
		g.Lock = lock
	}
	projectMap := map[int64]*ProjectTotal{}
	groupMap := map[int64]*GridGroup{}
	q = strings.ToLower(strings.TrimSpace(q))
	for rows.Next() {
		var r GridRow
		var headActive, projectActive int
		if err := rows.Scan(&r.ProjectID, &r.Project, &r.HeadID, &r.Head, &headActive, &projectActive, &r.DueDay, &r.Budget, &r.Actual); err != nil {
			return g, err
		}
		r.Active = headActive == 1 && projectActive == 1
		r.Variance = r.Budget - r.Actual
		r.VariancePercent = money.Percent(r.Variance, r.Budget)
		r.Status = statusFor(r.Budget, r.Actual)
		if status != "" && status != "all" && r.Status != status {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.Project+" "+r.Head), q) {
			continue
		}
		g.Rows = append(g.Rows, r)
		switch r.Status {
		case "over":
			g.Over++
		case "unbudgeted":
			g.Unbudgeted++
		case "under":
			g.Under++
		case "on-track":
			g.OnTrack++
		case "not-paid":
			g.NotPaid++
		case "no-activity":
			g.NoActivity++
		}
		pt := projectMap[r.ProjectID]
		if pt == nil {
			pt = &ProjectTotal{ProjectID: r.ProjectID, Project: r.Project}
			projectMap[r.ProjectID] = pt
			g.Projects = append(g.Projects, *pt)
		}
		grp := groupMap[r.ProjectID]
		if grp == nil {
			grp = &GridGroup{ProjectID: r.ProjectID, Project: r.Project, Total: ProjectTotal{ProjectID: r.ProjectID, Project: r.Project}}
			groupMap[r.ProjectID] = grp
			g.Groups = append(g.Groups, *grp)
		}
		grp.Rows = append(grp.Rows, r)
		pt.Budget += r.Budget
		pt.Actual += r.Actual
		grp.Total.Budget += r.Budget
		grp.Total.Actual += r.Actual
		g.Total.Budget += r.Budget
		g.Total.Actual += r.Actual
	}
	for i := range g.Projects {
		pt := projectMap[g.Projects[i].ProjectID]
		pt.Variance = pt.Budget - pt.Actual
		pt.VariancePercent = money.Percent(pt.Variance, pt.Budget)
		g.Projects[i] = *pt
	}
	for i := range g.Groups {
		grp := groupMap[g.Groups[i].ProjectID]
		grp.Total.Variance = grp.Total.Budget - grp.Total.Actual
		grp.Total.VariancePercent = money.Percent(grp.Total.Variance, grp.Total.Budget)
		g.Groups[i] = *grp
	}
	g.Total.Project = g.ScopeLabel
	g.Total.Variance = g.Total.Budget - g.Total.Actual
	g.Total.VariancePercent = money.Percent(g.Total.Variance, g.Total.Budget)
	return g, rows.Err()
}

func (s *Store) Report(ctx context.Context, fromMonth, toMonth, mode string) ([]ReportRow, error) {
	if !validMonth(fromMonth) {
		fromMonth = time.Now().Format("2006-01")
	}
	if !validMonth(toMonth) {
		toMonth = fromMonth
	}
	months, err := monthRange(fromMonth, toMonth)
	if err != nil {
		return nil, err
	}
	var out []ReportRow
	for _, month := range months {
		grid, err := s.Grid(ctx, month, "", "")
		if err != nil {
			return nil, err
		}
		switch mode {
		case "projects":
			for _, pt := range grid.Projects {
				out = append(out, ReportRow{Period: month, ProjectID: pt.ProjectID, Project: pt.Project, Budget: pt.Budget, Actual: pt.Actual, Variance: pt.Variance, VariancePercent: pt.VariancePercent})
			}
		case "monthly":
			out = append(out, ReportRow{Period: month, Budget: grid.Total.Budget, Actual: grid.Total.Actual, Variance: grid.Total.Variance, VariancePercent: grid.Total.VariancePercent})
		default:
			for _, row := range grid.Rows {
				out = append(out, ReportRow{Period: month, ProjectID: row.ProjectID, HeadID: row.HeadID, Project: row.Project, Head: row.Head, Budget: row.Budget, Actual: row.Actual, Variance: row.Variance, VariancePercent: row.VariancePercent})
			}
		}
	}
	return out, nil
}

func (s *Store) LockMonth(ctx context.Context, actor User, month, reason string) error {
	if !validMonth(month) || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: month and reason are required", ErrValidation)
	}
	if err := s.ensureMonthPlan(ctx, actor, month, ""); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO month_locks(month,locked_by,reason) VALUES(?,?,?)
		ON CONFLICT(month) DO UPDATE SET locked_by=excluded.locked_by, locked_at=CURRENT_TIMESTAMP, reason=excluded.reason`, month, actor.ID, reason)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE budget_months SET status='locked', updated_at=CURRENT_TIMESTAMP WHERE month=?`, month); err != nil {
		return err
	}
	return s.RecordAudit(ctx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "lock", EntityType: "month_lock", Summary: "Locked " + month + ": " + reason, After: map[string]string{"month": month, "reason": reason}})
}

func (s *Store) UnlockMonth(ctx context.Context, actor User, month, reason string) error {
	if !validMonth(month) || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: month and reason are required", ErrValidation)
	}
	before := map[string]string{"month": month}
	_, err := s.db.ExecContext(ctx, `DELETE FROM month_locks WHERE month=?`, month)
	if err != nil {
		return err
	}
	if err := s.ensureMonthPlan(ctx, actor, month, ""); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE budget_months SET status='open', updated_at=CURRENT_TIMESTAMP WHERE month=?`, month); err != nil {
		return err
	}
	return s.RecordAudit(ctx, AuditInput{ActorID: &actor.ID, ActorName: actor.Name, Action: "unlock", EntityType: "month_lock", Summary: "Unlocked " + month + ": " + reason, Before: before, After: map[string]string{"reason": reason}})
}

// rowQuerier is the read surface *sql.DB and *sql.Tx have in common, so a
// validation helper can run inside its caller's transaction instead of going out
// to a second pooled connection. Reading on a second connection while the first
// holds a write transaction is what turned one slow payment into four stacked
// five-second busy waits.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// isLocked answers the month-lock question and reports why it could not. Every
// write path must use this rather than IsLocked: a lock that fails to read is
// not an unlocked month, and treating it as one lets a write into a closed
// month.
func isLocked(ctx context.Context, q rowQuerier, month string) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM month_locks WHERE month=?`, month).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// IsLocked is the display-side answer, where there is nowhere to report an error
// to and a wrong answer only mis-renders a badge. Anything gating a write uses
// isLocked inside the package, or MonthIsLocked from outside it.
func (s *Store) IsLocked(ctx context.Context, month string) bool {
	locked, err := isLocked(ctx, s.db, month)
	if err != nil {
		return false
	}
	return locked
}

// MonthIsLocked is IsLocked for callers that gate a write on the answer and can
// therefore not treat "I could not find out" as "open".
func (s *Store) MonthIsLocked(ctx context.Context, month string) (bool, error) {
	return isLocked(ctx, s.db, month)
}

func (s *Store) MonthLock(ctx context.Context, month string) (MonthLock, error) {
	var lock MonthLock
	err := s.db.QueryRowContext(ctx, `SELECT ml.month,ml.locked_by,COALESCE(u.name,''),ml.locked_at,COALESCE(ml.reason,'')
		FROM month_locks ml LEFT JOIN users u ON u.id=ml.locked_by WHERE ml.month=?`, month).
		Scan(&lock.Month, &lock.LockedBy, &lock.ActorName, &lock.LockedAt, &lock.Reason)
	if err == sql.ErrNoRows {
		return lock, ErrNotFound
	}
	return lock, err
}

func (s *Store) ensureMonthPlan(ctx context.Context, actor User, month, sourceMonth string) error {
	if !validMonth(month) {
		return fmt.Errorf("%w: valid month is required", ErrValidation)
	}
	var source any
	if validMonth(sourceMonth) {
		source = sourceMonth
	}
	var actorID any
	if actor.ID != 0 {
		actorID = actor.ID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO budget_months(month,status,source_month,created_by) VALUES(?,'open',?,?)
		ON CONFLICT(month) DO UPDATE SET updated_at=CURRENT_TIMESTAMP`, month, source, actorID)
	return classify(err)
}

func (s *Store) RecordAudit(ctx context.Context, in AuditInput) error {
	before, _ := json.Marshal(in.Before)
	after, _ := json.Marshal(in.After)
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log(actor_id,actor_name,action,entity_type,entity_id,summary,before_json,after_json,ip) VALUES(?,?,?,?,?,?,?,?,?)`,
		in.ActorID, in.ActorName, in.Action, in.EntityType, in.EntityID, in.Summary, nullJSON(before), nullJSON(after), in.IP)
	return err
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

// validatePayment runs the check against the pool, for the callers that have not
// opened a transaction yet.
func (s *Store) validatePayment(ctx context.Context, in PaymentInput, headOptional bool) error {
	return validatePaymentTx(ctx, s.db, in, headOptional)
}

// validatePaymentTx checks a payment's own facts on a caller-supplied querier, so
// that a caller already holding a write transaction validates inside it rather
// than from a second pooled connection.
//
// headOptional is true only when the payment settles a recoverable request
// (F-D-11/F-E-01): a deposit or advance belongs to no budget head — the
// recoverable fieldset never collects one — and the grid and monthly report
// already exclude recoverables by treatment, so its head was never meaningful.
// Every other payment, including one with no request at all, still needs a head.
func validatePaymentTx(ctx context.Context, q rowQuerier, in PaymentInput, headOptional bool) error {
	if in.Amount <= 0 || !validDate(in.PaidOn) || (in.HeadID == 0 && !headOptional) {
		return fmt.Errorf("%w: valid head, date, and positive amount are required", ErrValidation)
	}
	// F-D-06: paid_on records when money left the bank, so a date after today
	// records something that has not happened.
	//
	// "Today" is in.Now when a caller injects a clock, and the server's own clock
	// when it does not. That default is the fix, not a detail: the check was
	// written to skip on a zero Now, no production caller ever set the field,
	// and so the rule shipped inert — present in the code, pinned by a store
	// test, and enforced against nobody. Forgetting to inject a clock must mean
	// "enforce", never "skip"; injection stays so a test can pin the day.
	//
	// It is deliberately LOCAL time rather than UTC, because it has to agree with
	// the clock the rest of the product calls "now": validMonthOrCurrent defaults
	// the ledger and the grid to time.Now().Format("2006-01") in the server's own
	// zone (app.go:2051). While this read UTC the two disagreed for every zone
	// ahead of Greenwich — between local midnight and the offset the ledger had
	// rolled to the new day and this guard had not, so an accountant recording a
	// payment dated the day the screen was showing them was told "paid on cannot
	// be a future date". In IST that was every day from 00:00 to 05:30.
	//
	// paid_on and the formatted clock share the YYYY-MM-DD shape, so a plain
	// string comparison is a correct date comparison.
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	if in.PaidOn > now.Format("2006-01-02") {
		return fmt.Errorf("%w: paid on cannot be a future date — money cannot have left the bank after today", ErrValidation)
	}
	locked, err := isLocked(ctx, q, in.PaidOn[:7])
	if err != nil {
		return err
	}
	if locked {
		return ErrLockedMonth
	}
	if in.HeadID == 0 {
		return nil
	}
	var active int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM heads h JOIN projects p ON p.id=h.project_id WHERE h.id=? AND h.active=1 AND p.active=1`, in.HeadID).Scan(&active); err != nil {
		return err
	}
	if active == 0 {
		return ErrInactiveHead
	}
	return nil
}

func scanUser(scanner interface{ Scan(...any) error }) (User, error) {
	var u User
	var active int
	var approver sql.NullInt64
	err := scanner.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &active, &u.CreatedAt, &u.UpdatedAt, &approver, &u.SessionVersion)
	if err == sql.ErrNoRows {
		return u, ErrNotFound
	}
	u.Active = active == 1
	u.DefaultApproverID = approver.Int64
	return u, err
}

func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func validDueDay(s string) bool {
	if s == "" {
		return true
	}
	day, err := strconv.Atoi(s)
	return err == nil && day >= 1 && day <= 31
}

func validateUserFields(email, name, role string) error {
	if strings.TrimSpace(email) == "" || !strings.Contains(email, "@") || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: email and name are required", ErrValidation)
	}
	if role != "" && role != "admin" && role != "data_entry" {
		return fmt.Errorf("%w: invalid user role", ErrValidation)
	}
	return nil
}

func validateAttachment(in AttachmentInput) error {
	if strings.TrimSpace(in.OriginalName) == "" || strings.TrimSpace(in.StoredPath) == "" || in.SizeBytes < 0 {
		return fmt.Errorf("%w: valid attachment metadata is required", ErrValidation)
	}
	return nil
}

func validMonth(s string) bool {
	_, err := time.Parse("2006-01", s)
	return err == nil
}

func monthRange(from, to string) ([]string, error) {
	start, err := time.Parse("2006-01", from)
	if err != nil {
		return nil, err
	}
	end, err := time.Parse("2006-01", to)
	if err != nil {
		return nil, err
	}
	if end.Before(start) {
		start, end = end, start
	}
	var months []string
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 1, 0) {
		months = append(months, cur.Format("2006-01"))
	}
	return months, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func classify(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("%w: %v", ErrDuplicate, err)
	}
	// F-B-06 / F-G-027: a FOREIGN KEY violation means a submitted id names a
	// row that does not exist — a client error, not a server fault, so it must
	// reach the user as a 400 that keeps their typed form, never a 500. The
	// driver text is deliberately not echoed. SQLite's message is stable:
	// "constraint failed: FOREIGN KEY constraint failed (787)".
	if strings.Contains(err.Error(), "FOREIGN KEY") {
		return fmt.Errorf("%w: a referenced record does not exist", ErrValidation)
	}
	return err
}

func statusFor(budget, actual int64) string {
	switch {
	case budget == 0 && actual == 0:
		return "no-activity"
	case actual == 0:
		return "not-paid"
	case budget == 0 && actual > 0:
		return "unbudgeted"
	case budget > 0 && actual > budget:
		return "over"
	case budget > 0 && actual == budget:
		return "on-track"
	default:
		return "under"
	}
}

func nullJSON(b []byte) any {
	if string(b) == "null" {
		return nil
	}
	return string(b)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
